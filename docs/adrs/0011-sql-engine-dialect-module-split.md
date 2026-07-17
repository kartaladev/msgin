# ADR 0011 — Split the `sql` adapter into a driver-free engine + per-dialect modules

- **Status:** Proposed (2026-07-17) — **audit round 1 done; NEEDS RECONCILIATION** into the decisions below
  before round-2 re-audit. The authoritative post-audit design is **spec 002 §4–§8** + the audit record
  `.superpowers/sdd/plan-006-audit-round-1.md`. Reconcile into this ADR: **Decision 2** → leaf-test-module
  layout (Structure Z — dialect prod modules require the engine ONLY; a `dbtest` runner module holds the
  container tests; empirically the ONLY way to give dialect consumers a clean go.sum — audit F1 CRITICAL);
  add **exported `BeginLockTx`/`SettleLockTx`** + relocate the compile-time SPI assertions and all of `ddl.go`
  out of root (F2/F3/F6); **Decision 4** → the harness is its **own module** `adapter/database/sql/harness`
  (pkg `harness`, may use testify) taking a per-dialect **TestKit** (F4/F7), not a stdlib-only root package;
  **Decision 5** → `GOWORK=off` CI + release bootstrap (replace→pin, root-tagged-first) + CI created from
  scratch (F5). Naming (user): harness module `harness`, runner `dbtest`.
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

Split `adapter/database/sql` into a **driver-free engine** (kept in the root module) and **one module per
dialect** (`postgres`, `mysql`, `sqlite`), tied together by a `go.work` workspace. Concretely:

1. **The engine stays in the root module** (`adapter/database/sql`): the SPIs (`LeaseDialect`, `LockDialect`,
   `InboxDialect`, `Querier`, `ClaimedRow`, `LockedRow`), `Source`, `Outbound`, `InboxDeduper`, `adapterBase`,
   framing (`Encode/DecodeHeaders`), options, errors, `Strategy`, `TransactionResolver`,
   `RecommendedMaxPayloadBytes`, and a newly-exported **`ValidateIdent`** (the identifier-safety contract for
   dialect authors). It references **no** concrete dialect.

2. **Each dialect is its own module** (`adapter/database/sql/{postgres,mysql,sqlite}`) with its own `go.mod`
   carrying only its driver/testcontainers test-deps. Dialect built-ins get **package-qualified names**
   (`postgres.LeaseDialect()`, `postgres.InboxDialect()`, `mysql.*`, `sqlite.*`), which also resolves the
   `PostgresLeaseDialect` naming asymmetry flagged in Plan 005. The per-dialect reference-DDL builders
   (`postgres.DDL(table)`, `postgres.InboxDDL(table)`) and identifier quoting move to the dialect modules and
   validate via `ValidateIdent` first (preserving ADR 0010 D3 finding I1 — no unvalidated string-returning
   DDL escapes on an interface).

3. **The dialect is a required, explicit constructor argument; driver auto-detect is removed.**
   `NewPollingSource(db, table, dialect LeaseDialect, opts...)`,
   `NewOutboundAdapter(db, table, dialect LeaseDialect, opts...)`,
   `NewInboxDeduper(businessDB, dialect InboxDialect, opts...)`. A nil dialect is a construction error
   (**`ErrNilDialect`**). `detect.go`/`resolveDialect`/`WithDialect`/`ErrDialectUndetected` are **deleted**.
   This makes the previously "guaranteed" path (explicit dialect) the *only* path, eliminating the documented
   heuristic mis-detection footgun (a Postgres-wire derivative silently mis-detected as vanilla Postgres).
   (O-1 resolved: `Outbound` keeps `LeaseDialect` — no separate writer interface; YAGNI.)

4. **A shared conformance harness, stdlib-`testing`-only, in the root module**
   (`adapter/database/sql/sqlconformance`): the Source/Outbound/InboxDeduper/dialect behavioral suites become
   importable functions taking `(t *testing.T, dialect, db *sql.DB)`. Using only the stdlib `testing` package
   (no `testify`) keeps it dependency-clean, so any dialect module can import it without pulling assertion
   deps. Each dialect module runs the harness against its real engine (`postgres` vs a Postgres container;
   `mysql` vs MySQL **and** MariaDB; `sqlite` vs an embedded SQLite — increment B). The core engine's own
   unit tests use an in-memory **fake dialect** + fake `Querier` (no driver/testcontainers), so the root
   module's test-deps shrink to `testify` + `goleak`. (O-2 resolved: harness in root, stdlib-only.)

5. **`go.work` is committed** at the repo root (`use ./` + the three dialect modules) for a consistent
   contributor workspace; **release tags are module-path-prefixed** SemVer (e.g.
   `adapter/database/sql/postgres/v0.0.1`), independent of the root module's tags (standard Go multi-module
   tagging; matches ADR 0003). CI quality gates run per module, and the release workflow handles the
   module-prefixed tags (folded into the increment-A plan). (O-4, O-5 resolved.)

The **SQLite dialect** (its capability differences — no `SKIP LOCKED`/`FOR UPDATE`, so lease-only + inbox via
`ON CONFLICT … RETURNING`; pure-Go `modernc.org/sqlite`, no Docker) is decided in **ADR 0012** and built in
increment B, against this same engine + conformance contract.

## Consequences

**Positive**
- The root module and any core-only consumer become **driver/testcontainers-free** (go.sum, module graph,
  `govulncheck` all clean); a dialect consumer inherits only that dialect's test-deps (SQLite: only pure-Go
  `modernc.org/sqlite`, no Docker). The "minimal dependencies" promise is finally true at the module-graph
  level, not just the binary level.
- **Open for extension without core change:** a new dialect is a new module implementing the SPIs + passing
  the conformance harness; the engine never learns about it. This extends ADR 0003's per-adapter-module
  philosophy to dialects and cleanly admits SQLite (and future CockroachDB/Timescale/… derivatives).
- **Explicit dialect** removes the heuristic mis-detection risk and the global auto-detect surface; the API is
  fully type-safe. Package-qualified dialect names read cleanly and resolve the prior naming asymmetry.
- **One conformance contract** every dialect must satisfy — a single source of truth for queue behavior across
  engines, and a ready-made acceptance test for contributors' dialects.

**Negative / costs**
- **Breaking API change** (pre-1.0, permitted): constructors gain a required dialect argument; `WithDialect`
  and the `Postgres*/MySQL*` core built-ins are removed in favor of `postgres.*`/`mysql.*`.
- **Large mechanical test migration** (~1000 lines of real-DB behavior tests → harness form) and a **coverage
  re-accounting**: the engine's hot-path branches previously covered only by real-DB tests must be re-covered
  by fake-dialect unit tests (enumerated in the plan) so the ≥85%/hot-path gate holds per module.
- **Multi-module friction**: `go.work`, module-prefixed tags, per-module CI — a one-time cost ADR 0003 already
  accepted for pgx/redis/nats.
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
- **Dialect impls stay in core, only tests move to a separate module** — rejected: Go requires `_test.go`
  beside its package, so the impl must move with its tests; and keeping core coupled to every dialect defeats
  open-for-extension.
