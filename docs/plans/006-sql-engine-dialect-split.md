# Plan 006 — `sql` engine/dialect module split (increment A)

> **⚠️ POST-AUDIT REVISION REQUIRED before execution.** The tasks below are the PRE-audit draft. Audit
> round 1 (`.superpowers/sdd/plan-006-audit-round-1.md`) found a CRITICAL structural flaw + 4 HIGH must-fixes;
> the authoritative corrected design is **spec 002 §4–§8** (Structure Z). Revise the tasks accordingly BEFORE
> running any of them, then **round-2 re-audit** the reconciled spec+ADR+plan, then get user approval. Concrete
> task-level changes required:
> - **Layout is now 5 modules** (engine + `harness` + `postgres` + `mysql` + `dbtest` runner), NOT 3. Dialect
>   prod modules require the ENGINE ONLY (no testcontainers/driver — audit F1); the container tests live in the
>   `dbtest` runner module (nobody imports it → clean consumer go.sum).
> - **Task 2** → build the `harness` MODULE (`adapter/database/sql/harness`, pkg `harness`, may use testify)
>   exposing `RunSource`/`RunLock`/`RunOutbound`/`RunOutbox`/`RunInbox`/`RunDialect` taking a **`TestKit`**
>   (dialect + `Quote`/`Placeholder`/`NowExpr` — audit F4). NOT a stdlib-`testing` root package (F7).
> - **Tasks 4–5** → split each into (a) a driver-free dialect prod module + (b) its tests in the `dbtest`
>   runner. Export `BeginLockTx`/`SettleLockTx` and move them + the `var _ …Dialect = …` assertions + all of
>   `ddl.go` (incl. DELETING the `InboxDDL(d,table)` type-switch dispatcher) out of root; keep root building
>   green after each (audit F2/F3/F6). Add a `dbtest`-module task.
> - **Task 6** → CREATE CI + release workflows (none exist) with `GOWORK=off` per-module jobs + the
>   replace→pin release bootstrap (root tagged first); commit `go.work` (audit F5). Its own task.
> - Author `RunSource` (lease) with correctness-only concurrency assertions so SQLite passes later (F8);
>   fix `doc.go`'s stale symbol references + goleak TestMain per module (F9).


> **For agentic workers:** REQUIRED SUB-SKILL — implement task-by-task via
> `superpowers:subagent-driven-development` (fresh implementer + reviewer per task). Every task is an
> independently green unit (the affected module's `GOTOOLCHAIN=go1.25.0 go test ./... -race` passes),
> blackbox (`package …_test`) where it applies, goleak-clean. **Read `CLAUDE.md`, spec 002, and ADR 0011
> first; trust those over any memory.** This plan is a **behavior-preserving refactor** of the existing
> Postgres/MySQL `sql` adapter — no messaging-semantics change.

- **Spec:** [002 — sql multi-module split + SQLite](../specs/002-sql-multi-module-and-sqlite.md) (increment A).
- **ADR:** [0011 — sql engine/dialect module split](../adrs/0011-sql-engine-dialect-module-split.md); amends
  [0003](0003-multi-module-repository-layout.md) + [0010](0010-poller-sql-adapter.md).
- **Predecessor:** Plan 005 (sql adapter, Tasks 1–10, landed on this branch). **Successors:** Plan 007 (SQLite
  dialect, increment B, after ADR 0012) then Plan 005 **Task 11** (docs/examples, resumed).
- **Branch:** a fresh branch off the current `feat/poller-sql-adapter` HEAD (or off `main` after it merges) —
  decided at execution time with the user.

**Goal:** Split `adapter/database/sql` into a driver-free engine (root module) + per-dialect modules
(`postgres`, `mysql`) tied by `go.work`, with an explicit-dialect API and a shared conformance harness, so the
root module and core-only consumers carry zero driver/testcontainers dependencies.

**Architecture:** The engine (SPIs, `Source`/`Outbound`/`InboxDeduper`, framing, options) stays in the root
module and references no concrete dialect. Dialect impls + DDL + quoting + real-DB tests move to per-dialect
modules. A stdlib-`testing`-only conformance harness in the root defines the behavioral contract once; each
dialect module runs it against its real engine. The engine's own logic is unit-tested with an in-memory fake
dialect (no driver).

**Tech Stack:** Go 1.25, stdlib `database/sql`, `jonboulle/clockwork`. Test-only, **per dialect module**:
`testcontainers-go` + the engine driver (`jackc/pgx/v5` for postgres; `go-sql-driver/mysql` for mysql/mariadb),
`testify`, `goleak`. Root test-only: `testify`, `goleak`.

## Global Constraints

- **Go 1.25**, `GOTOOLCHAIN=go1.25.0`; no language/stdlib features > 1.25; `CGO_ENABLED=0` builds per module.
- **Behavior-preserving** for Postgres + MySQL/MariaDB: the messaging semantics, settlement contract, fence/
  lease logic, and SQL are unchanged — only the module boundaries, the dialect-wiring API, and test placement
  change.
- **The root module must end driver-free:** after Task 5, `go mod tidy` in the root drops `pgx`,
  `go-sql-driver/mysql`, `testcontainers-go` (+ Docker/OTel indirects). Root production deps = stdlib +
  `clockwork`; root test deps = `testify` + `goleak` only.
- **Dependency points inward:** the root engine imports no dialect module; dialect modules import the engine.
  The conformance harness (root) imports only the engine + stdlib `testing`; it is imported by dialect-module
  tests. No cycles.
- **Identifier-injection discipline preserved** (ADR 0010 D3 I1): `ValidateIdent` is the shared validator;
  each dialect quotes its own identifiers and validates first; reference-DDL builders are per-dialect and
  validate before building.
- **Testing:** blackbox `_test` packages; `table-test` assert-closure for engine unit tests; the conformance
  harness uses stdlib `testing` + tiny in-harness assert helpers (NO testify, to keep the root dependency-
  clean — ADR 0011 D4 / O-2). `use-testcontainers` per dialect module. Target ≥ 85% changed-pkg coverage;
  every hot-path/typed-error branch covered **per module**.
- **Commits:** per-task green units; `Spec: 002` / `Plan: 006` / `ADR: 0011` trailers. Whole-branch gate
  before delivery. Never commit/push without approval except the plan's per-task commits.

---

## Task 1 — Explicit-dialect engine API; delete driver auto-detect (in-place)

**Files:** Modify `base.go` (`newAdapterBase` takes an explicit `dialect`, drops the `resolveDialect`
fallback), `source.go`/`outbound.go`/`inbox_dedup.go` (constructor signatures gain the dialect parameter),
`options.go` (remove `WithDialect`, `WithInboxDialect`; remove `config.dialect`/`inboxConfig.dialect`),
`errors.go` (add `ErrNilDialect`; remove `ErrDialectUndetected`), `dialect.go` (export `ValidateIdent`; keep
`validateIdent` as an unexported alias or replace call sites). **Delete** `detect.go`, `detect_test.go`.
Update every root `_test.go` that constructed via auto-detect to pass the dialect explicitly (the `engines`/
`inboxEngines` tables already carry the dialect value). Still ONE module; dialect impls stay in root for now.

**Produces:**
- `NewPollingSource(db *stdsql.DB, table string, dialect LeaseDialect, opts ...Option) (*Source, error)`
- `NewOutboundAdapter(db *stdsql.DB, table string, dialect LeaseDialect, opts ...Option) (*Outbound, error)`
- `NewInboxDeduper(businessDB *stdsql.DB, dialect InboxDialect, opts ...InboxOption) (*InboxDeduper, error)`
- `ValidateIdent(name string) error` (exported); `ErrNilDialect` (construction error on a nil dialect).
- **Removed:** `WithDialect`, `WithInboxDialect`, `resolveDialect`, `ErrDialectUndetected`.

- A nil dialect → `ErrNilDialect` at construction (never a deferred nil-method panic). The lock strategy still
  additionally requires the dialect to implement `LockDialect` → `ErrLockStrategyUnsupported` (unchanged).
- `newAdapterBase(db, table, dialect, cfg)` validates `db != nil` (`msgin.ErrNilAdapter`), `dialect != nil`
  (`ErrNilDialect`), and `ValidateIdent(table)`; assigns the dialect directly (no detection).

**Hot-path branches:** nil db → `ErrNilAdapter`; nil dialect → `ErrNilDialect`; invalid table →
`ErrInvalidTableName`; valid construction → ok; lock strategy + non-`LockDialect` → `ErrLockStrategyUnsupported`.

**Tests (root, still dual-engine real-DB, unchanged behavior):** update `detect_test.go`'s surviving
construction cases into `source`/`outbound`/`inbox` construction tests asserting the new signatures + the
nil-dialect branch (fast, no container, via the fake dialect from Task 3 or a trivial inline stub). The
existing dual-engine suites pass `e.dialect` explicitly and stay green. Confirm `go test ./adapter/database/sql/
-race` green with pgx/mysql/testcontainers still present.

## Task 2 — Conformance harness (stdlib-`testing`-only) + convert root real-DB suites to run it

**Files:** Create `adapter/database/sql/sqlconformance/{harness.go,assert.go,source.go,lock.go,outbound.go,
outbox.go,inbox.go,dialect.go}` (a NON-test, importable package in the root module, importing only the engine
+ stdlib `testing`). Convert the root real-DB suites (`source_pg_test.go`, `source_lock_test.go`,
`outbound_pg_test.go`, `outbox_pg_test.go`, `inbox_dedup_pg_test.go`, `dialect_pg_test.go`, `mysql_test.go`,
`strategy_test.go`) into thin `_test.go` wrappers that, per engine in the `engines`/`inboxEngines` table, call
the harness.

**Produces (package `sqlconformance`):**
- `RunSourceConformance(t *testing.T, dialect msginsql.LeaseDialect, db *sql.DB, opts ...Option)` and peers
  `RunLockConformance`, `RunOutboundConformance`, `RunOutboxConformance`, `RunInboxConformance`,
  `RunDialectConformance` — each encapsulating the behavior currently asserted by the corresponding suite.
- Tiny stdlib-based assert helpers in `assert.go` (`mustNoErr(t, err, msg)`, `mustEqual[T comparable](t, got,
  want, msg)`, `mustTrue`, `mustErrorIs`, …) so the harness reads clearly with **no testify** (keeping the
  root module free of a testify production dependency — ADR 0011 D4).

- The harness takes an already-open `*sql.DB` + the dialect from the caller (the dialect module provides the
  container); it never imports a driver or testcontainers. It provisions its own fresh tables (via
  `dialect.EnsureSchema`/`EnsureInboxSchema`) per sub-case, mirroring the current suites' `freshTable`.
- Behavior parity is the bar: every assertion the current suites make (claim bumps counts/locks, fenced Ack/
  Nack, stale-lease `ErrStaleLease`, `visible_after` redelivery, lock-strategy always-commit-Nack + detached
  tx + separate-pool DLQ, outbox atomicity + commit-gated visibility + strict/opportunistic branches, inbox
  idempotency + atomic-rollback-retryable + MySQL false-positive guard + Purge + Ready unique-constraint
  fail-fast) is reproduced as a harness assertion.

**Hot-path branches:** unchanged from Plan 005 — the harness RE-EXPRESSES the same coverage against a real DB.

**Tests:** root runs the harness against Postgres + MySQL (+ MariaDB for inbox) exactly as today (testcontainers
still in root at this task). Confirm `go test ./adapter/database/sql/... -race` green and coverage on the
engine + harness is at least the pre-refactor level. **Audit target:** confirm no testify import in
`sqlconformance` (`go list -deps`), and no engine-internal (unexported) access the harness can't reach from
outside the package (the harness is a separate package `sqlconformance`, so it drives the engine's EXPORTED
API only — verify the exported surface is sufficient).

## Task 3 — Engine unit tests with an in-memory fake dialect (retain engine coverage post-move)

**Files:** Create `adapter/database/sql/fakedialect_test.go` (an in-memory `LeaseDialect` + `LockDialect` +
`InboxDialect` + a fake `Querier`), and `source_unit_test.go`/`outbound_unit_test.go`/`inbox_unit_test.go`
(engine-logic unit tests driving `Source`/`Outbound`/`InboxDeduper` against the fake — NO real DB). These
COVER the engine's Go-logic hot-path branches that will lose real-DB coverage once the dialects move (Tasks
4–5), so the root module's coverage holds afterward.

**Consumes:** the explicit-dialect constructors (Task 1).

- The fake `LeaseDialect` stores rows in an in-memory map and implements `Claim`/`Ack`/`Nack`/`Insert`/
  `EnsureSchema`/`SchemaExists` (+ `ClaimLock`/`AckLock`/`NackLock` for the lock path, using a fake `*sql.Tx`
  substitute is NOT possible — see note) deterministically, so the engine's framing → `Delivery` construction,
  the `Ack`/`Nack` closures + `ErrStaleLease` mapping, `Source.Poll` batching, `Outbound.Send`/`resolveQuerier`
  branches, and `InboxDeduper.MarkProcessed`/`Ready`/`Purge` branches are exercised without a driver.
- **Note (audit target):** the lock strategy carries a real `*sql.Tx` (`LockedRow.Tx`), which a pure fake
  cannot fabricate. Enumerate precisely which lock-strategy branches are engine-logic (fake-testable) vs
  SQL/tx-semantics (conformance-only, run in the dialect modules). Any branch that genuinely needs a real
  `*sql.Tx` stays covered by the conformance harness, NOT the fake — document this split so the coverage gate
  is honest and no branch is silently uncovered.

**Hot-path branches (fake-covered):** enumerate each engine branch here at execution time — Poll empty/rows,
Ack applied/stale→`ErrStaleLease`, Nack applied/stale, `Outbound.Send` payload-type/framing/resolveQuerier
(no-resolver/resolver-err/shared-tx/strict-no-tx/opportunistic-no-tx), `MarkProcessed` nil-tx/first/dup,
`Ready` ok/not-ready/no-unique-constraint, `Purge` N/0/`ErrInvalidRetention`, construction validation.

**Tests:** `go test ./adapter/database/sql/ -race` green; the fake-based suite plus the still-present
harness-over-real-DB suite together keep coverage ≥ the pre-refactor number. This task ADDS tests; it removes
none.

## Task 4 — Create the `postgres` module; move the Postgres dialect + tests out of root

**Files:** Create `adapter/database/sql/postgres/{go.mod,dialect.go,ddl.go,quote.go,doc.go}` (module
`github.com/kartaladev/msgin/adapter/database/sql/postgres`, `require`ing the root module + `pgx` +
`testcontainers-go`/`modules/postgres` as **test-only**), and `postgres/{conformance_pg_test.go,testutils_test.go}`
(runs the Task-2 harness against a real Postgres container via `RunTestDatabase`, moved here). Create/append
`go.work` at repo root (`use ./` + `./adapter/database/sql/postgres`). **Delete** from root: `postgres.go`,
the Postgres DDL builders + `pgQuote*` in `ddl.go`/`postgres.go`, and the Postgres-specific test wiring.

**Produces (package `postgres`):** `postgres.LeaseDialect() msginsql.LeaseDialect` (the value also satisfies
`msginsql.LockDialect` and `msginsql.InboxDialect`), `postgres.DDL(table) (string, error)`,
`postgres.InboxDDL(table) (string, error)`. (The single stateless value satisfies all three engine SPIs.)

- The dialect impl is MOVED verbatim (behavior-preserving); `pgQuote`/`pgQuoteTable` move with it and call
  the now-exported `msginsql.ValidateIdent`.
- Root no longer references any Postgres symbol; the MySQL dialect still in root (removed in Task 5) keeps
  root compiling until then.

**Hot-path branches:** unchanged (the dialect SQL is identical); coverage now accrues in the `postgres` module
via the harness run.

**Tests:** `cd adapter/database/sql/postgres && GOTOOLCHAIN=go1.25.0 go test ./... -race` green (full
conformance vs real Postgres, goleak-clean). Root still green (`go test ./adapter/database/sql/ -race` — now
Postgres-free; MySQL suites + fake suites remain). `go.work` lets local `go test ./...` see both.

## Task 5 — Create the `mysql` module; move the MySQL/MariaDB dialect + tests out of root → root goes lean

**Files:** Create `adapter/database/sql/mysql/{go.mod,dialect.go,ddl.go,quote.go,doc.go}` (module …`/mysql`,
`require`ing root + `go-sql-driver/mysql` + `testcontainers-go`/`modules/mysql`+`/modules/mariadb` test-only)
and `mysql/{conformance_test.go,testutils_test.go}` (runs the harness against MySQL **and** MariaDB via
`RunTestMySQL`/`RunTestMariaDB`, moved here). Extend `go.work` with `./adapter/database/sql/mysql`. **Delete**
from root: `mysql.go`, the MySQL DDL builders + `mysqlQuote*`, and the MySQL/MariaDB test wiring +
`testutils_test.go`'s container helpers. Then `go mod tidy` the root.

**Produces (package `mysql`):** `mysql.LeaseDialect()`, `mysql.DDL(table)`, `mysql.InboxDDL(table)` (one
stateless value satisfying `LeaseDialect` + `LockDialect` + `InboxDialect`), covering MySQL and MariaDB
(`LOCK IN SHARE MODE`, `UTC_TIMESTAMP(6)`, two-step claim, etc. — moved verbatim).

**Hot-path branches:** unchanged.

**Tests:** `cd adapter/database/sql/mysql && go test ./... -race` green (conformance vs MySQL + MariaDB,
goleak-clean). **Root is now driver-free:** `go mod tidy` drops `pgx`, `go-sql-driver/mysql`,
`testcontainers-go` (+ Docker/OTel indirects); `go test ./adapter/database/sql/ -race` green on the fake +
harness-definition only; root `go.mod` production deps = stdlib + `clockwork`, test deps = `testify` +
`goleak`. Verify with `go list -deps` that root imports no driver/testcontainers.

## Task 6 — `go.work`, CI/release per module, and the whole-branch verification gate

**Files:** Commit `go.work` (repo root). Update CI (`.github/workflows/*`) to build/test each module (root +
postgres + mysql) with `GOTOOLCHAIN=go1.25.0` (`-race`, `vet`, `gofmt`, `govulncheck`, `golangci-lint`,
`go mod tidy` stability, `CGO_ENABLED=0` build) — a matrix over modules. Update the release workflow to accept
**module-path-prefixed** tags (`adapter/database/sql/postgres/vX.Y.Z`, `…/mysql/vX.Y.Z`) alongside the root
`vX.Y.Z`. **Audit target O-5:** confirm the release choreography — a dialect module's `go.mod` requires a
**published** root version, so the root must be tagged before a dialect module can require a non-`replace`d
version; document the tag-order in the release doc.

- **Empirical isolation check (the whole point):** re-run the throwaway-consumer probe from spec 002 §1
  against the refactored tree — a consumer importing only `adapter/database/sql` must have a `go.sum` free of
  pgx/mysql/testcontainers/docker/otel; a consumer importing `…/postgres` gets only pgx (+ testcontainers in
  their test graph, not build); record the numbers in the plan's self-review.
- **Whole-branch delivery gate (CLAUDE.md §5)** over the branch diff: `/code-review` + `/security-review`,
  resolve/triage every finding; each module `go test ./... -race` green (shake with `-count`); `go vet`,
  `gofmt`/`gofumpt`, `CGO_ENABLED=0` build, `go mod tidy` (stable), `go mod verify`, `govulncheck`,
  `golangci-lint`, coverage ≥ 85% per changed module with every listed hot-path branch covered. Update
  `docs/HANDOVER.md` + memory. Push for the user's GitHub review (do NOT ff-`main` without approval).

## Traceability

Commits carry `Spec: 002`, `Plan: 006`, `ADR: 0011` trailers. This plan implements spec 002 increment A;
ADR 0011 amends ADR 0003 (engine/dialect split) + ADR 0010 (explicit-dialect constructors, auto-detect
removal). **Successors:** Plan 007 (SQLite dialect, ADR 0012) then Plan 005 Task 11 (docs/examples), both
against the module layout this plan establishes.

## Self-review notes (pre-audit)

- **Spec coverage:** engine driver-free (Tasks 1–5) → §2 goal (a); per-dialect test-dep isolation (Tasks 4–5)
  → goal (b); open-for-extension via the SPI + harness (Tasks 2–5) → goal (c); behavior-preserving (Global
  Constraints, all tasks) → goal (e). SQLite (goal d) is Plan 007, not here.
- **Ordering rationale:** Task 3 (fake unit tests) precedes Tasks 4–5 (dialect removal) so root retains engine
  coverage after the real-DB suites leave. Task 2 (harness) precedes 4–5 so the dialect modules have a suite
  to run. Root stays green after every task; each dialect module is green when created.
- **Known audit targets (flagged inline):** (1) the fake-dialect vs conformance coverage split for the
  lock-strategy `*sql.Tx` branches (Task 3) — risk of a silently-uncovered branch; (2) the stdlib-`testing`-
  only harness vs a separate `sqltest` module trade-off (Task 2, O-2) — is the testify→stdlib port worth it,
  or does a separate harness module read better? (3) release choreography / root-version pinning for dialect
  modules (Task 6, O-5); (4) whether `go.work` should be committed (O-4) given it can mask version-skew in CI.
- **Deliberately deferred:** SQLite (Plan 007), Task 11 docs/examples, and any consumer-facing migration guide
  for the breaking API change (fold into Task 11).
