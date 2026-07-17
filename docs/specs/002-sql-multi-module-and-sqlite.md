# Spec 002 — `sql` multi-module split + SQLite dialect

- **Status:** Draft (2026-07-17) — **audit rounds 1 AND 2 folded.** Round 1 → Structure Z / leaf-test modules;
  `harness` + `dbtest`; exported `BeginLockTx`/`SettleLockTx`; TestKit; `GOWORK=off` CI; created CI/release.
  Round 2 (SOUND WITH FIXES, no topology change → no round 3) → R2-1 (BeginLockTx/SettleLockTx covered in root
  via stub-driver), R2-2 (two-profile coverage methodology), R2-3 (Task-1 test-helper relocation), R2-4/5/6/7.
  User-approved the direction + Structure Z. Audit records: `.superpowers/sdd/plan-006-audit-round-{1,2}.md`.
- **Supersedes/amends:** [ADR 0003 — Multi-module layout](../adrs/0003-multi-module-repository-layout.md)
  (the "`database/sql` lives entirely in core" detail) and [ADR 0010 — Poller + SQL adapter](../adrs/0010-poller-sql-adapter.md)
  (constructor signatures + the driver auto-detect, D3). New ADRs will record the decisions:
  **ADR 0011** (sql engine/dialect module split) for increment A; **ADR 0012** (SQLite dialect) for increment B.
- **Governing product spec:** [Spec 001 — Messaging core](001-messaging-core.md) §9 (`sql` adapter). This spec is a
  structural refactor of the already-shipped `sql` adapter (Plan 005, Tasks 4–10) plus a third dialect.
- **Relation to Plan 005:** Plan 005 Task 11 (docs/examples) is **PENDED** behind this work and resumes after.

## 1. Motivation (evidence-based)

The `sql` adapter's production code is **already driver-free** — a caller injects their own `*sql.DB`, and
msgin imports no SQL driver in non-test code (ADR 0003/0010). But the adapter's **integration tests** (real
Postgres/MySQL/MariaDB via `testcontainers-go`) live in the **root module**, so the root `go.mod` carries
`jackc/pgx/v5`, `go-sql-driver/mysql`, `testcontainers-go` + 3 modules, and a **~50-entry indirect block**
(Docker/moby, OpenTelemetry, containerd, gopsutil…).

**Empirically measured** (a throwaway consumer importing `adapter/database/sql`, `go mod tidy`, Go 1.25):

| In the consumer's… | Drivers / testcontainers present? |
| --- | --- |
| compiled **binary** | ❌ No (production is driver-free) |
| **go.mod** (direct) | ❌ No |
| **go.sum** | ✅ **Yes** — pgx, mysql-driver, testcontainers (8 lines), **docker/moby (24)**, otel (10); most of the 128 go.sum lines |
| `go mod graph` | ✅ Yes |

Go 1.17+ module-graph pruning shields the consumer's *binary* but **not** their `go.sum` / module graph /
`govulncheck` surface. For a library whose headline is "minimal dependencies," every consumer inheriting
Docker/OTel checksums is a real defect — and it will worsen as dialects are added (SQLite now, more later).

## 2. Goals / Non-goals

**Goals:** (a) the **root module** and any **core-only consumer** carry **zero** driver/testcontainers deps;
(b) a consumer opting into a dialect inherits **only that dialect's** test-deps (a Postgres consumer gets
`pgx`, not MySQL/MariaDB/SQLite; a SQLite consumer gets only pure-Go `modernc.org/sqlite`, no Docker);
(c) **open for extension** — a new dialect is a new module, no core change; (d) add a **SQLite** dialect;
(e) behavior-preserving for the existing Postgres/MySQL engines (increment A is a refactor, not a rewrite).

**Non-goals:** changing the messaging semantics, the reliability/flow-control layer, or the settlement
contract; the standalone heavy-client adapters (`pgx` LISTEN/NOTIFY, `redis`, `nats`) remain separate future
modules and are **distinct** from the `sql/postgres` dialect module (generic `database/sql` + a Postgres
dialect).

## 3. Decisions (settled with the user)

- **D-A. One module per dialect.** `adapter/database/sql/{postgres,mysql,sqlite}`, each its own `go.mod`
  carrying only its driver/testcontainers test-deps. Matches ADR 0003's per-adapter-module precedent.
- **D-B. Explicit dialect; drop auto-detect.** The dialect is a **required constructor argument**; the core
  engine no longer references any concrete dialect. `detect.go`/`detect_test.go` and `ErrDialectUndetected`
  are removed. This also eliminates the documented heuristic mis-detection footgun (a Postgres-wire
  derivative silently mis-detected) — the guaranteed path becomes the only path.
- **D-C. `go.work` workspace** ties the modules for local dev; release tags are module-path-prefixed SemVer.
- **D-D. Shared conformance harness.** The Source/Outbound/InboxDeduper/dialect behavioral suites are defined
  **once** as an importable harness; each dialect module runs it against its own real engine. Every current
  and future dialect must pass the same contract.

## 4. Target layout (revised after audit round 1 — leaf-test modules, Structure Z)

**Audit finding F1 (CRITICAL, empirically proven):** a Go module's `go.mod` require-closure is inherited by
anyone importing ANY package from that module — so if a dialect's production code and its testcontainers tests
share a module, a consumer importing the dialect's *production* package still inherits the whole
testcontainers→docker/moby/otel closure (measured: 102 polluted `go.sum` lines vs **0** with the leaf-test
layout below). Therefore **each dialect's production module carries ZERO test-deps**; the container tests live
in a separate **runner** module a consumer never imports.

```
github.com/kartaladev/msgin                         (root module — driver-free, testify/goleak test-only)
  (core msgin runtime + adapter/memory, unchanged)
  adapter/database/sql/                              THE ENGINE (stays in root):
    - SPIs: LeaseDialect, LockDialect, InboxDialect, Querier, ClaimedRow, LockedRow
    - Source, Outbound, InboxDeduper, adapterBase, framing (Encode/DecodeHeaders),
      options, errors, Strategy, TransactionResolver, RecommendedMaxPayloadBytes
    - ValidateIdent, BeginLockTx, SettleLockTx (NEWLY EXPORTED — the lock/identifier SPI for dialect authors)
    - unit tests: in-memory fake dialect + fake Querier (no driver); root test-deps = testify + goleak
  adapter/database/sql/harness/      (go.mod)        THE HARNESS MODULE (pkg `harness`; requires the engine
    + testify — reusable by ANY dialect author, incl. third parties). Exposes:
      RunSource(t, TestKit), RunLock, RunOutbound, RunOutbox, RunInbox, RunDialect — driving the engine's
      EXPORTED API + a per-dialect TestKit. Imports NO driver/testcontainers.
  adapter/database/sql/postgres/     (go.mod)        prod dialect — requires the ENGINE ONLY (clean consumer go.sum):
      postgres.LeaseDialect() / DDL(table) / InboxDDL(table) (one stateless value satisfies all three SPIs;
      pgQuote + the relocated beginLockTx/settleLockTx usage live here). NO testcontainers/pgx in its go.mod.
  adapter/database/sql/mysql/        (go.mod)        prod dialect — requires the ENGINE ONLY: mysql.LeaseDialect()/…
  adapter/database/sql/sqlite/       (go.mod)        prod dialect — requires the ENGINE ONLY: sqlite.LeaseDialect()/…  [increment B]
  adapter/database/sql/dbtest/       (go.mod)        THE RUNNER (leaf; nobody imports it): requires postgres + mysql
      (+ sqlite in B) + harness + pgx + go-sql-driver/mysql (+ modernc.org/sqlite in B) + testcontainers.
      Provides RunTestDatabase/RunTestMySQL/RunTestMariaDB (+ SQLite in B) and runs the harness per engine
      against real containers. This is where ALL the heavy test-deps live; CI runs it.
go.work                                              use ./ + harness + postgres + mysql + dbtest (committed)
```

Increment A = 5 modules (engine + harness + postgres + mysql + dbtest). Increment B adds the `sqlite`
prod module and extends `dbtest`. A consumer importing `…/postgres` inherits **only the engine** (proven-clean
`go.sum`); a dialect author validates their dialect by importing `…/harness` (engine + testify) — never
the runner.

## 5. Engine API changes (increment A)

- **Constructors take the dialect explicitly (required, positional):**
  - `NewPollingSource(db *sql.DB, table string, dialect LeaseDialect, opts ...Option) (*Source, error)`
    (lock strategy still additionally requires the dialect to implement `LockDialect` →
    `ErrLockStrategyUnsupported`, unchanged).
  - `NewOutboundAdapter(db *sql.DB, table string, dialect LeaseDialect, opts ...Option) (*Outbound, error)`
    (Outbound uses the `Insert`/schema subset of `LeaseDialect`; kept as `LeaseDialect` for simplicity —
    **open item O-1** below).
  - `NewInboxDeduper(businessDB *sql.DB, dialect InboxDialect, opts ...InboxOption) (*InboxDeduper, error)`.
  - A nil dialect is a construction error (**`ErrNilDialect`**, new) — never a deferred nil-method panic.
- **Removed:** `WithDialect` option, `detect.go`, `resolveDialect`, `ErrDialectUndetected`. (Breaking, pre-1.0.)
- **`ValidateIdent(name string) error` exported** from the engine (was unexported `validateIdent`) so a dialect
  module can validate a table identifier before quoting; each dialect owns its own quoting (`pgQuote`/…moves
  to its module). The identifier-injection discipline (ADR 0010 D3 finding I1) is preserved: reference-DDL
  builders live per-dialect (`postgres.DDL(table)`, `postgres.InboxDDL(table)`) and validate first.
- **Export the lock helpers `BeginLockTx`/`SettleLockTx`** (audit F2/F6): the dialect `ClaimLock`/`AckLock`/
  `NackLock` impls currently call unexported engine helpers `beginLockTx`/`settleLockTx` (`lock.go`). Rather
  than duplicate them per dialect, export them from the engine as part of the `LockDialect`-author SPI —
  `BeginLockTx(ctx, q Querier) (*sql.Tx, error)`, `SettleLockTx(ctx, tx *sql.Tx, query string, args ...any)
  error` — with a godoc contract (always-commit-on-success, rollback-on-error). NOTE (audit F6 → revised by
  R2-1): the tx arms are covered **in root** by stdlib-only fake-`database/sql/driver` unit tests — all three
  (`BeginLockTx` unsupported-`Querier` error; `SettleLockTx` Exec-error and Commit-error rollbacks) — because
  the `dbtest` conformance run hits only the happy path and the fake `LockDialect` never routes through them.
  The plan asserts this so no branch is silently uncovered.
- **`ddl.go` leaves the root entirely** (audit F3): `PostgresDDL`/`MySQLDDL`, the `(postgresDialect).ddl`
  methods, AND the `InboxDDL(d InboxDialect, table)` **type-switch dispatcher** all move out — the dispatcher
  is **deleted** (it cannot switch on types no longer in root), replaced by per-dialect `postgres.InboxDDL`/
  `mysql.InboxDDL`. The compile-time SPI assertions (`var _ LockDialect = postgresDialect{}`,
  `var _ InboxDialect = …` in `lock.go`/`inbox_dedup.go`) move **into each dialect module**.
- **Dialect built-ins move + get package-qualified names** (resolving the naming asymmetry): `postgres.LeaseDialect()`
  (the same value also satisfies `LockDialect` + `InboxDialect`); likewise `mysql.*`, `sqlite.*`. The old
  `PostgresDialect()/MySQLDialect()/PostgresInboxDialect()/…` are removed from core. **Every task that removes
  a dialect from root must leave the root building green** (audit F3) — the moves above are done together.

## 6. Test strategy (increment A — revised after audit F4/F6/F7)

- **Core engine unit tests (root module): in-memory fake dialect + fake `Querier`, no real DB.** A fake
  `LeaseDialect`/`LockDialect`/`InboxDialect` in the engine's `_test.go` exercises the engine's Go logic —
  Poll→Delivery framing, Ack/Nack closure wiring + `ErrStaleLease`, the lock-wiring (which never dereferences
  `LockedRow.Tx`, so a fake returning `{Tx:nil}` covers it — audit F6 clean bill), `Outbound.resolveQuerier`
  branches, `InboxDeduper.MarkProcessed`/`Ready`/`Purge` branches, option validation, credit-path via a real
  `Run` — with **no driver/testcontainers**. Root `go.mod` test-deps = `testify` + `goleak`.
- **The `harness` MODULE (`adapter/database/sql/harness`, pkg `harness`, requires engine + testify):** the
  current real-DB behavior suites become reusable functions `RunSource`/`RunLock`/`RunOutbound`/`RunOutbox`/
  `RunInbox`/`RunDialect`. **Audit F4:** each takes a **`TestKit`** — the dialect plus the per-dialect test
  primitives the verification SQL needs (`{Lease LeaseDialect; Quote func(string) string; Placeholder
  func(int) string; NowExpr func() string; …}`) — because the suites build raw verification SQL (backtick vs
  `"`, `?` vs `$n`, `now()` vs `UTC_TIMESTAMP(6)`) that is NOT on the SPI. **Audit F7:** the harness is its own
  module so it may use `testify` freely (no testify in the root) — this REPLACES the stdlib-`testing`-only-in-
  root idea (O-2 resolved → separate module), and avoids the ~1000-line testify→stdlib port.
- **The `dbtest` RUNNER MODULE** (requires the dialect modules + `harness` + the drivers + testcontainers)
  provisions real containers (`RunTestDatabase`/`RunTestMySQL`/`RunTestMariaDB`, moved here) and runs the
  harness per engine: Postgres, MySQL, MariaDB (+ SQLite in increment B, no container). **All heavy test-deps
  live here; nothing imports this module**, so no consumer inherits them.
- **Audit F8 — author the lease conformance (`RunSource`) with correctness-only concurrency assertions**
  (disjoint claims, no double-claim), NOT non-blocking/throughput assumptions, so a single-writer engine
  (SQLite, increment B) passes the SAME contract without a harness redesign.
- **Coverage accounting (revised after audit round 2):** engine-intrinsic coverage = the root fake-based unit
  tests; the concrete-dialect real-SQL coverage = the `dbtest` conformance runs. **Audit R2-1 (corrects F6):**
  `BeginLockTx`/`SettleLockTx` stay exported in the engine, so their tx arms — the two `SettleLockTx` rollback
  branches + `BeginLockTx`'s unsupported-`Querier` error — are covered in **root** via a stdlib-only fake
  `database/sql/driver` (the `dbtest` happy path never hits the rollback arms; the fake `LockDialect` never
  routes through them). **Audit R2-2 (coverage methodology under leaf-test modules):** the dialect production
  modules have no co-located tests (they `require` the engine only → `go test -cover` on them is ~0% by
  design), so the ≥85%/hot-path gate is measured on **two profiles** — (a) the root fake+stub-driver tests for
  engine-intrinsic code, and (b) the `dbtest` run with `-coverpkg=…/adapter/database/sql/...` over the merged
  engine+dialect packages for the dialect SQL — **not** per empty prod-module run. The harness must exercise
  the dialect **error branches** (`DDL("bad; name")`→`ErrInvalidTableName`, `MsgIDUniqueIndexExists` false)
  too. The plan enumerates the fake-covered vs dbtest-covered branch split so none is silently uncovered.

## 7. SQLite dialect (increment B — its own ADR 0012 + plan)

_All decisions below are RESOLVED (brainstorm 2026-07-18); the details are ratified in **ADR 0012**._

- **Driver (test-only):** **`modernc.org/sqlite`** — pure-Go, **cgo-free** (aligns with the no-cgo gate);
  no `testcontainers`/Docker (SQLite is embedded — tests use a temp-file or shared-cache in-memory DB). It
  lives in the **`dbtest` leaf runner ONLY**; the `sqlite` prod module stays engine-only (O-3 resolved).
- **Capability differences (SQLite is single-writer, no row-level locks):**
  - **No `SELECT … FOR UPDATE SKIP LOCKED`** and **no `FOR UPDATE`.** The **lease/claim** strategy is adapted:
    because SQLite serializes writers (a write statement holds the DB write lock for its whole duration), the
    claim is a **one-shot** `UPDATE "t" SET … WHERE id IN (SELECT id … ORDER BY visible_after LIMIT ?)
    RETURNING …` — atomic without `SKIP LOCKED`, mirroring Postgres's one-shot RETURNING (no MySQL-style
    two-step tx needed). `RETURNING` needs SQLite ≥3.35; the fence/`delivery_count`/`visible_after` columns
    are unchanged. The **lock/`FOR UPDATE` strategy is not implementable** → `sqlite.LeaseDialect()` does
    **not** implement `LockDialect`, so `WithStrategy(StrategyLockForUpdate)` returns the existing
    `ErrLockStrategyUnsupported`. (This requires **two small, behavior-preserving `harness` changes** — see
    the test-provisioning bullet.)
  - **Inbox dedup works:** `INSERT … ON CONFLICT(msg_id) DO NOTHING RETURNING` (SQLite supports it) — exact,
    like Postgres; no `INSERT IGNORE`-style demotion problem (so `TestKit.MySQLFamily=false`). The
    `MsgIDUniqueIndexExists` probe uses SQLite's `pragma_index_list`/`pragma_index_info` (msg_id is
    `TEXT PRIMARY KEY` → an autoindex with `"unique"=1`); `SchemaExists` probes `sqlite_master`.
  - **Time → INTEGER unix micros.** SQLite has no native datetime type. `visible_after`/`locked_at`/
    `created_at`/`processed_at` are `INTEGER` epoch-microsecond columns; the DB clock is
    `CAST(unixepoch('now','subsec')*1000000 AS INTEGER)`, and delays/lease-TTL are plain integer ±µs — exact
    parity with the PG/MySQL dialects' `.Microseconds()` interval math (resolution is milliseconds expressed
    in µs, ample for a queue). `'subsec'` needs SQLite ≥3.42; `modernc.org/sqlite` bundles ≥3.45. **DB-clock
    invariant preserved** (no app-clock for row times).
- **Production DSN guidance (opinionated, overridable).** SQLite serializes writers, so concurrent
  consumers require WAL + a `busy_timeout` (else `SQLITE_BUSY`). msgin never owns the caller's `*sql.DB`
  (driver/DSN are the caller's, like PG/MySQL), so the `sqlite` prod module ships a **driver-free** DSN
  builder `sqlite.DSN(path string, opts ...DSNOption) string` — default
  `file:<path>?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)`, overridable via `WithJournalMode`/
  `WithBusyTimeout`/`WithSharedMemory` — plus `doc.go` explaining why. It emits `modernc.org/sqlite`-flavored
  pragmas (the recommended driver); a different driver → the caller builds their own DSN.
- **Test provisioning (dbtest, no Docker).** `RunTestSQLite(t, opts ...)` defaults to a **WAL temp-file**
  (`t.TempDir()`, torn down by `t.Cleanup`), with `WithSharedMemory()` selecting `file::memory:?cache=shared`
  — both modes, opinionated default; it builds its DSN via `sqlite.DSN(...)` (dogfooding the builder).
  Conformance runs `RunSource/RunOutbound/RunOutbox/RunInbox/RunDialect` (**omitting `RunLock`**, no
  `LockDialect`) plus a lock-strategy-unsupported assertion.
- **Two behavior-preserving `harness` changes (both gated so PG/MySQL are unaffected; audit round 1):**
  1. `RunDialect`'s per-SPI-method invalid-identifier coverage hard-requires `LockDialect` for its
     `ClaimLock` case → make **only that case** conditional on the type-assertion (runs for PG/MySQL, skipped
     for a lease-only dialect).
  2. A new `TestKit.SingleWriter bool` flag gates **only** `RunOutbox`'s `CommitGatesVisibility` subtest.
     That subtest holds an **open, uncommitted write transaction** and then issues a concurrent
     `Source.Poll` on the pool — on PG/MySQL the uncommitted row is MVCC/`SKIP LOCKED`-invisible so the poll
     returns empty cleanly, but on **single-writer SQLite** the pool's claim `UPDATE` blocks on the held
     writer lock for the full `busy_timeout` and then fails `SQLITE_BUSY` (**empirically confirmed**, audit
     F1). It is NOT an F8 correctness-only case; a single-writer engine cannot satisfy it. `sqliteKit` sets
     `SingleWriter=true`, skipping it; the rest of `RunOutbox` (Atomicity, policy branches, borrowed-tx) and
     all of `RunSource`'s genuinely-concurrent cases **do** pass on SQLite (autocommit claims serialize
     safely — empirically confirmed: 8 workers × 200 rows, zero double-claims).
- **Guarantee:** at-least-once, same as the other engines (single-writer concurrency, but the queue semantics
  hold). Documented per-dialect.

## 8. Workspace, tagging, CI (revised after audit F5)

- **`go.work` is committed** at repo root (`use ./ ./adapter/database/sql/harness
  ./adapter/database/sql/postgres ./adapter/database/sql/mysql ./adapter/database/sql/dbtest`; + `sqlite` in
  B). It gives contributors a consistent local workspace. (O-4 resolved: commit.)
- **CI MUST run with `GOWORK=off`** (audit F5) for the per-module build/test/tidy jobs, so each module is built
  against the **`require`d** engine version, not the local workspace — otherwise a stale/mis-pinned `require`
  in a dialect `go.mod` passes CI under the workspace and breaks real `go get` consumers. Keep one separate
  workspace-build job for the aggregate.
- **Release bootstrap / version pinning** (audit F5, the chicken-and-egg): a dialect module's `go.mod` must
  `require github.com/kartaladev/msgin vX.Y.Z` at a **published** version. On-branch (before any root tag),
  the dialect modules resolve the engine via **`go.work` for local dev and a temporary `replace` for
  `GOWORK=off` CI**; at release, the root is tagged FIRST, then each dialect module swaps `replace` → a pinned
  `require` and is tagged with its module-path-prefixed SemVer (`adapter/database/sql/postgres/vX.Y.Z`).
  Document this tag-order in a release doc.
- **CI + release workflows are CREATED, not updated** (audit F5 — `.github/workflows/` does not exist): a
  per-module matrix (build/`-race`/vet/gofmt/`govulncheck`/golangci/`tidy`/`CGO_ENABLED=0`, `GOWORK=off`) plus
  a module-prefixed-tag release workflow. This is its own increment-A task, not a tail-end bullet (O-5
  resolved: in increment A, as a dedicated task).

## 9. Sequencing

1. **Increment A — the split refactor** (ADR 0011 + a plan): create the three dialect modules + `go.work`;
   move the Postgres and MySQL/MariaDB dialect impls, DDL builders, quoting, and real-DB tests into
   `postgres`/`mysql`; extract the `harness` module; add engine fake-dialect unit tests; switch constructors to
   explicit dialect + export `ValidateIdent` + delete auto-detect. **Behavior-preserving** for PG/MySQL.
2. **Increment B — SQLite dialect** (ADR 0012 + a plan): implement `sqlite` against the same conformance
   contract (minus lock-strategy), pure-Go driver, no Docker.
3. **Resume Plan 005 Task 11** (docs/examples), now spanning the multi-module layout + the SQLite story.

Each increment: brainstorm→(this) spec→ADR→**adversarial Opus audit**→plan→SDD (implementer + reviewer),
per CLAUDE.md and the project workflow. No implementation before the audited plan is approved.

## 10. Risks

- **Large mechanical test migration** (~1000 lines of behavior tests → harness form). Mitigated by the
  conformance-suite structure and per-module `-race` verification.
- **Coverage regressions during the move** — the fake-dialect engine tests must genuinely cover the hot-path
  branches previously covered only by real-DB tests; enumerate them in the plan.
- **Multi-module friction** (tagging, CI, `go.work`) — one-time cost; ADR 0003 already committed to it.
- **SQLite semantic divergence** — the no-SKIP-LOCKED lease adaptation must preserve the at-least-once +
  fence guarantees; the audit's concurrency pass is the gate.

## 11. Open items — RESOLVED (user + audit round 1)

- **O-1 — RESOLVED:** Outbound keeps `LeaseDialect` (YAGNI; no separate writer interface).
- **O-2 — RESOLVED (audit F7):** the harness is its **own module** (`adapter/database/sql/harness`) using
  `testify` — NOT a stdlib-`testing`-only package in root. Avoids the testify→stdlib port; keeps root clean.
- **O-3 — RESOLVED:** SQLite driver = `modernc.org/sqlite` (pure-Go, cgo-free) — increment B.
- **O-4 — RESOLVED:** commit `go.work`; CI runs `GOWORK=off` (audit F5).
- **O-5 — RESOLVED:** CI + release workflows are created in increment A as a dedicated task (audit F5).

**Module naming (user):** the harness package/module is `harness` (`adapter/database/sql/harness`); the
container-test runner is `dbtest`. The reusable-conformance layout is **Structure Z** (§4).

## 12. Traceability

Spec 002 → ADR 0011 (increment A) + ADR 0012 (increment B, SQLite) → plans (006 refactor, 007 sqlite) →
code/commits (module-prefixed trailers). ADR 0011 amends ADR 0003 (sql engine/dialect split) and ADR 0010
(explicit-dialect constructors, auto-detect removal). Plan 005 Task 11 resumes after increment B.
