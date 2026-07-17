# ADR 0011 — Split the `sql` adapter into a driver-free engine + per-dialect modules

- **Status:** Proposed (2026-07-17) — **audit rounds 1 AND 2 folded; reconciled to Structure Z; ready to
  implement.** The authoritative post-audit design is **spec 002 §4–§8** + the audit records
  `.superpowers/sdd/plan-006-audit-round-{1,2}.md`; this ADR matches it (leaf-test modules; exported
  `BeginLockTx`/`SettleLockTx`/`ValidateIdent`; the `harness` module + per-dialect `TestKit`; `GOWORK=off` CI +
  release bootstrap created from scratch; round-2 coverage corrections R2-1/R2-2). Round 2 = SOUND WITH FIXES,
  no module-topology change → no round 3. Naming (user): harness module `harness`, container-test runner
  `dbtest`; the reusable-conformance layout is **Structure Z** (spec 002 §4).
- **Context source:** [Spec 002 — sql multi-module split + SQLite](../specs/002-sql-multi-module-and-sqlite.md).
- **Amends:** [ADR 0003 — Multi-module layout](0003-multi-module-repository-layout.md) (the "`database/sql`
  lives entirely in the core module" detail) and [ADR 0010 — Poller + SQL adapter](0010-poller-sql-adapter.md)
  (D3 driver auto-detect + the constructor signatures). **Successor:** ADR 0012 (the SQLite dialect, increment B).

## Context

The `sql` adapter (ADR 0010, Plan 005 Tasks 4–10) is production-driver-free — the caller injects a `*sql.DB`,
msgin imports no driver in non-test code — but its **integration tests** (real Postgres/MySQL/MariaDB via
`testcontainers-go`) live in the **root module**. So the root `go.mod` carries `pgx`, `go-sql-driver/mysql`,
`testcontainers-go` + 3 modules, and a ~50-entry indirect block (Docker/moby, OpenTelemetry, containerd…).

Measured (a throwaway consumer importing `adapter/database/sql`, `go mod tidy`, Go 1.25): the consumer's
**binary and go.mod are driver-free**, but their **`go.sum` inherits pgx + mysql-driver + testcontainers +
docker/moby (24 lines) + otel (10)** — Go 1.17+ module-graph pruning shields the binary, not the `go.sum` /
module graph / `govulncheck` surface. For a "minimal dependencies is a hard requirement" library this is a
real defect, and it worsens as dialects are added (SQLite now, more later). The fix must (a) make the root
module and any core-only consumer carry zero driver/testcontainers deps, (b) scope a dialect's test-deps to
consumers who opt into that dialect, and (c) keep the adapter open for extension (a new dialect = a new
module, no core change) — while remaining behavior-preserving for the existing engines.

## Decision

Split `adapter/database/sql` into a **driver-free engine** (kept in the root module) and **leaf-test modules**
per dialect — each dialect's *production* code in a module that requires the engine ONLY, and ALL container/
driver tests in a single `dbtest` **runner** module that nobody imports — tied together by a committed
`go.work` workspace. This is **Structure Z** (spec 002 §4). Concretely:

1. **The engine stays in the root module** (`adapter/database/sql`): the SPIs (`LeaseDialect`, `LockDialect`,
   `InboxDialect`, `Querier`, `ClaimedRow`, `LockedRow`), `Source`, `Outbound`, `InboxDeduper`, `adapterBase`,
   framing (`Encode/DecodeHeaders`), options, errors, `Strategy`, `TransactionResolver`,
   `RecommendedMaxPayloadBytes`, and three **newly-exported** dialect-author SPIs: **`ValidateIdent`** (the
   identifier-safety contract), and the lock helpers **`BeginLockTx`** / **`SettleLockTx`** (audit F2/F6 — the
   dialect `ClaimLock`/`AckLock`/`NackLock` impls call these engine helpers; rather than duplicate the tx
   logic per dialect they are exported from the engine with a godoc always-commit-on-success/rollback-on-error
   contract). The engine references **no** concrete dialect. Its own tests use an in-memory **fake dialect** +
   fake `Querier` only (no driver), so root test-deps shrink to `testify` + `goleak`.

2. **Leaf-test modules per dialect (audit F1, CRITICAL — empirically proven).** A Go module's `go.mod`
   require-closure is inherited by *anyone importing any package from it*, so co-locating a dialect's
   production code with its testcontainers tests would leave every dialect *production* consumer inheriting the
   whole testcontainers→docker/moby/otel closure (measured: **102 polluted `go.sum` lines** vs **0** with the
   leaf-test layout). Therefore:
   - **Each dialect production module** (`adapter/database/sql/{postgres,mysql,sqlite}`) carries **zero
     test-deps** — its `go.mod` requires **only the engine**. Dialect built-ins get **package-qualified names**
     — `postgres.LeaseDialect()` (the single stateless value also satisfies `LockDialect` + `InboxDialect`),
     `mysql.*`, `sqlite.*` — resolving the `PostgresLeaseDialect` naming asymmetry flagged in Plan 005. The
     per-dialect reference-DDL builders (`postgres.DDL(table)`, `postgres.InboxDDL(table)`) and identifier
     quoting move to the dialect module and validate via `ValidateIdent` first (preserving ADR 0010 D3 finding
     I1 — no unvalidated string-returning DDL escapes on an interface). The compile-time SPI assertions
     (`var _ LockDialect = …{}`, `var _ InboxDialect = …{}`, currently in root `lock.go`/`inbox_dedup.go`) move
     **into each dialect module** (audit F3).
   - **All of `ddl.go` leaves the root** (audit F3): `PostgresDDL`/`MySQLDDL`, the per-dialect `.ddl` methods,
     **and** the `InboxDDL(d InboxDialect, table)` **type-switch dispatcher** — which is **deleted** (it cannot
     switch on types that no longer live in root), replaced by per-dialect `postgres.InboxDDL(table)` /
     `mysql.InboxDDL(table)`. Every task that removes a dialect from root **must leave root building green**.
   - **One `dbtest` runner module** (`adapter/database/sql/dbtest`, leaf — *nobody* imports it) holds ALL heavy
     test-deps: the drivers (`pgx`, `go-sql-driver/mysql`, later `modernc.org/sqlite`) + `testcontainers-go`,
     `require`s the dialect modules + `harness`, provides the container helpers (`RunTestDatabase`/
     `RunTestMySQL`/`RunTestMariaDB`, moved here from root) and runs the harness per engine against real
     containers. Because no consumer imports `dbtest`, no consumer inherits its closure.

3. **The dialect is a required, explicit constructor argument; driver auto-detect is removed.**
   `NewPollingSource(db, table, dialect LeaseDialect, opts...)`,
   `NewOutboundAdapter(db, table, dialect LeaseDialect, opts...)`,
   `NewInboxDeduper(businessDB, dialect InboxDialect, opts...)`. A nil dialect is a construction error
   (**`ErrNilDialect`**). `detect.go`/`resolveDialect`/`WithDialect`/`ErrDialectUndetected` are **deleted**.
   This makes the previously "guaranteed" path (explicit dialect) the *only* path, eliminating the documented
   heuristic mis-detection footgun (a Postgres-wire derivative silently mis-detected as vanilla Postgres).
   (O-1 resolved: `Outbound` keeps `LeaseDialect` — no separate writer interface; YAGNI.)

4. **A shared conformance harness in its OWN module** (`adapter/database/sql/harness`, package `harness`;
   audit F7, O-2 revised). The Source/Outbound/InboxDeduper/dialect behavioral suites become importable
   functions `RunSource` / `RunLock` / `RunOutbound` / `RunOutbox` / `RunInbox` / `RunDialect`. Because the
   harness is its own module (not a root package), it **may depend on `testify`** freely — testify stays in the
   dialect/`dbtest` test graph and never enters the root module — which **avoids the ~1000-line testify→stdlib
   port** the earlier stdlib-only-in-root idea would have forced. **Audit F4:** each function takes a
   per-dialect **`TestKit`** — the dialect PLUS the test primitives its verification SQL needs and that are NOT
   on the SPI: `{Lease LeaseDialect; Quote func(string) string; Placeholder func(int) string; NowExpr func()
   string; …}` — because the suites build raw verification SQL (backtick vs `"`, `?` vs `$n`, `now()` vs
   `UTC_TIMESTAMP(6)`) each dialect must supply. The harness imports the engine + `testify` + stdlib
   `database/sql`/`testing` only — **no driver, no testcontainers** — and drives the engine's EXPORTED API.
   The `dbtest` runner module (Decision 2) provisions the real container, builds the dialect's `TestKit`, and
   calls the harness per engine (`postgres` vs a Postgres container; `mysql` vs MySQL **and** MariaDB; `sqlite`
   vs an embedded SQLite — increment B). **Audit F8:** the lease conformance (`RunSource`) is authored with
   correctness-only concurrency assertions (disjoint claims, no double-claim), NOT non-blocking/throughput
   assumptions, so a single-writer engine (SQLite) passes the SAME contract without a harness redesign.

5. **`go.work` is committed**, **CI runs `GOWORK=off`, and the CI/release workflows are created from scratch**
   (audit F5; `.github/workflows/` does not yet exist). `go.work` (`use ./` + `harness` + `postgres` +
   `mysql` + `dbtest`; + `sqlite` in B) gives contributors a consistent local workspace, but **CI MUST run
   `GOWORK=off`** for the per-module build/test/tidy jobs so each module is exercised against its **`require`d**
   engine version — otherwise a stale/mis-pinned `require` in a dialect `go.mod` passes CI under the workspace
   yet breaks a real `go get` consumer (keep one aggregate workspace-build job alongside). **Release
   bootstrap (the chicken-and-egg):** a dialect `go.mod` must `require github.com/kartaladev/msgin` at a
   *published* version, but on-branch no such root tag exists; so on-branch the dialect modules resolve the
   engine via `go.work` (local) + a temporary `replace` (for `GOWORK=off` CI), and **at release the root is
   tagged FIRST**, then each dialect module swaps `replace` → a pinned `require` and is tagged with its
   **module-path-prefixed** SemVer (`adapter/database/sql/postgres/v0.0.1`), independent of the root tag
   (standard Go multi-module tagging; matches ADR 0003). The release workflow handles the module-prefixed tags;
   the tag-order is documented in a release doc. (O-4, O-5 resolved; created as a dedicated increment-A task.)

The **SQLite dialect** (its capability differences — no `SKIP LOCKED`/`FOR UPDATE`, so lease-only + inbox via
`ON CONFLICT … RETURNING`; pure-Go `modernc.org/sqlite`, no Docker) is decided in **ADR 0012** and built in
increment B, against this same engine + conformance contract.

## Consequences

**Positive**
- The root module, any core-only consumer, AND **any dialect *production* consumer** become
  **driver/testcontainers-free** — the leaf-test layout means a `…/postgres` consumer's `go.sum` inherits only
  the engine (measured **0** polluted lines vs 102 for the co-located layout); go.sum, module graph, and
  `govulncheck` are all clean. The "minimal dependencies" promise is finally true at the module-graph level,
  not just the binary level. The empirical go.sum probe is the **acceptance gate**.
- **Open for extension without core change:** a new dialect is a new production module implementing the SPIs +
  a `dbtest` entry running the shared `harness`; the engine never learns about it. This extends ADR 0003's
  per-adapter-module philosophy to dialects and cleanly admits SQLite (and future CockroachDB/Timescale/…
  derivatives).
- **Explicit dialect** removes the heuristic mis-detection risk and the global auto-detect surface; the API is
  fully type-safe. Package-qualified dialect names read cleanly and resolve the prior naming asymmetry.
- **One conformance contract** (the `harness` module) every dialect must satisfy — a single source of truth for
  queue behavior across engines, reusable by third-party dialect authors, and a ready-made acceptance test for
  contributors' dialects. Authored without multi-writer/non-blocking assumptions (F8) so SQLite passes it too.

**Negative / costs**
- **Breaking API change** (pre-1.0, permitted): constructors gain a required dialect argument; `WithDialect`,
  `WithInboxDialect`, driver auto-detect, and the `Postgres*/MySQL*` core built-ins are removed in favor of
  `postgres.*`/`mysql.*`.
- **Module count**: increment A is **5 modules** (engine + `harness` + `postgres` + `mysql` + `dbtest`), and
  the leaf-test split adds a layer of indirection — the container tests live in `dbtest`, one hop from the
  dialect they exercise, driving it through its exported API + the harness (blackbox). This is the price of a
  clean dialect-consumer `go.sum`; it is deliberate.
- **Large mechanical test migration** (~1000 lines of real-DB behavior tests → `harness` functions taking a
  `TestKit`) and a **coverage re-accounting**: the engine's hot-path branches previously covered only by
  real-DB tests must be re-covered by fake-dialect unit tests in root (enumerated in the plan). The
  concrete-dialect SQL is covered by the `dbtest` conformance runs; the exported `BeginLockTx`/`SettleLockTx`
  tx arms (both rollback branches + the unsupported-`Querier` error) are covered **in root** by a stdlib-only
  fake-`database/sql/driver` (audit R2-1, correcting F6 — the `dbtest` happy path never hits the rollback
  arms). **The ≥85%/hot-path gate is NOT per-prod-module** (audit R2-2): dialect production modules require the
  engine only and have no co-located tests, so coverage is measured on two profiles — the root fake+stub-driver
  tests, and the `dbtest` run with `-coverpkg=…/adapter/database/sql/...` over the merged engine+dialect
  packages. The plan states the split so no branch is silently uncovered.
- **Multi-module friction**: committed `go.work`, `GOWORK=off` CI, module-prefixed tags, release bootstrap
  (replace→pin, root-tagged-first), per-module CI — a one-time cost ADR 0003 already accepted for
  pgx/redis/nats, now with CI/release created from scratch.
- Behavior-preserving for Postgres/MySQL is a **hard requirement** of increment A; the adversarial audit's
  concurrency/coverage pass is the gate before implementation.

## Alternatives considered (rejected)

- **Do nothing / rely on module pruning** — rejected: measured go.sum pollution is real; the maintainer/CI/
  security surface stays bloated and grows with each dialect.
- **One shared `dialects` module** (all engines together) — rejected: a Postgres consumer would still inherit
  MySQL/MariaDB/SQLite test-deps; weaker isolation than per-dialect modules.
- **Keep auto-detect via a driver registry** (database/sql-style blank-import + `init()` registration) —
  rejected in favor of explicit passing: post-split the consumer already imports the dialect module, so
  naming the dialect costs one argument, and explicit passing avoids global registry state, `init()` side
  effects, and the reflection-based mis-detection risk.
- **Co-locate each dialect's production code AND its testcontainers tests in one module** (the original
  3-module draft) — rejected by audit F1: a consumer importing the dialect's *production* package still
  inherits the module's whole test closure (Go doesn't prune a required module's requirements), leaving 102
  polluted `go.sum` lines. The leaf-test split (dialect prod module requires the engine only; `dbtest` runner
  holds the tests) is the ONLY measured way to a clean dialect-consumer `go.sum`.
- **Dialect impls stay in core, only tests move to a separate module** — rejected: keeping core coupled to
  every concrete dialect defeats open-for-extension (the engine would `import` each dialect), and the
  `InboxDDL` type-switch dispatcher could never admit a third-party dialect.
- **Stdlib-`testing`-only harness in the root module** (the pre-audit O-2 choice) — rejected by audit F7: it
  forces a ~1000-line testify→stdlib port (incl. testify/suite lifecycle with no stdlib equivalent),
  error-prone; hosting the harness in its own module lets it keep `testify` without polluting root.
