# Plan 006 — `sql` engine/dialect module split (increment A, Structure Z)

> **For agentic workers:** REQUIRED SUB-SKILL — implement task-by-task via
> `superpowers:subagent-driven-development` (fresh implementer + reviewer per task). Every task is an
> independently green unit (the affected module's `GOTOOLCHAIN=go1.25.0 go test ./... -race` passes),
> blackbox (`package …_test`) where it applies, goleak-clean. **Read `CLAUDE.md`, spec 002, and ADR 0011
> first; trust those over any memory.** Start Go work from `cc-skills-golang:golang-how-to`; use `gopls`/`LSP`
> for navigation and post-edit diagnostics. This plan is a **behavior-preserving refactor** of the existing
> Postgres/MySQL `sql` adapter — no messaging-semantics change.

- **Spec:** [002 — sql multi-module split + SQLite](../specs/002-sql-multi-module-and-sqlite.md) (increment A;
  §4–§8 are authoritative, post-audit-round-1, **Structure Z**).
- **ADR:** [0011 — sql engine/dialect module split](../adrs/0011-sql-engine-dialect-module-split.md); amends
  [0003](0003-multi-module-repository-layout.md) + [0010](0010-poller-sql-adapter.md).
- **Audit:** round 1 `.superpowers/sdd/plan-006-audit-round-1.md` (F1–F9, folded) + round 2
  `.superpowers/sdd/plan-006-audit-round-2.md` (R2-1…R2-7, folded — SOUND WITH FIXES, no topology change → no
  round 3). Both folded into this bundle before execution.
- **Predecessor:** Plan 005 (sql adapter, Tasks 1–10, landed on this branch). **Successors:** Plan 007 (SQLite
  dialect, increment B, after ADR 0012) then Plan 005 **Task 11** (docs/examples, resumed).
- **Branch:** continues on `feat/poller-sql-adapter` (this increment lands here; the branch is pushed for MR,
  not merged to `main`, per the session directive).

**Goal:** Split `adapter/database/sql` into a driver-free **engine** (root module) + **leaf-test dialect
modules** (`postgres`, `mysql` production-only, requiring the engine ONLY) + a shared **`harness`** conformance
module + a **`dbtest`** runner (all heavy test-deps; nobody imports it) tied by a committed `go.work`, with an
explicit-dialect API — so that the root module **and every dialect *production* consumer** carry zero
driver/testcontainers dependencies (audit F1: the co-located layout leaks 102 `go.sum` lines; leaf-test = 0).

**Architecture (Structure Z — spec 002 §4):**
```
github.com/kartaladev/msgin                    ENGINE (root): SPIs, Source/Outbound/InboxDeduper, framing,
                                               options, errors, ValidateIdent + BeginLockTx + SettleLockTx
                                               (newly exported); fake-dialect unit tests only. test-deps: testify+goleak
  adapter/database/sql/harness/   (go.mod)     pkg harness: RunSource/RunLock/RunOutbound/RunOutbox/RunInbox/
                                               RunDialect(t, TestKit). requires engine + testify. NO driver/containers.
  adapter/database/sql/postgres/  (go.mod)     prod dialect, requires ENGINE ONLY: postgres.LeaseDialect()/DDL/InboxDDL
  adapter/database/sql/mysql/     (go.mod)     prod dialect, requires ENGINE ONLY: mysql.LeaseDialect()/DDL/InboxDDL
  adapter/database/sql/dbtest/    (go.mod)     RUNNER (leaf; nobody imports): requires postgres+mysql+harness+drivers+
                                               testcontainers. RunTestDatabase/RunTestMySQL/RunTestMariaDB + harness runs.
go.work                                        use ./ + harness + postgres + mysql + dbtest (committed in Task 6)
```

**Tech Stack:** Go 1.25, stdlib `database/sql`, `jonboulle/clockwork`. Root test-deps: `testify` + `goleak`.
`harness` module: `testify` (+ engine). `dbtest` module: `testcontainers-go` + `jackc/pgx/v5` +
`go-sql-driver/mysql` + `testify` + `goleak`. Dialect prod modules: **no test-deps** (require the engine only).

## Global Constraints

- **Go 1.25**, `GOTOOLCHAIN=go1.25.0`; no language/stdlib features > 1.25; `CGO_ENABLED=0` builds per module.
- **Behavior-preserving** for Postgres + MySQL/MariaDB: messaging semantics, settlement contract, fence/lease
  logic, and every SQL statement are unchanged — only module boundaries, the dialect-wiring API, and test
  placement change. The dialect impls MOVE VERBATIM.
- **The root module ends driver-free** (after Task 5): `go mod tidy` drops `pgx`, `go-sql-driver/mysql`,
  `testcontainers-go` (+ Docker/moby/OTel indirects). Root production deps = stdlib + `clockwork`; root test
  deps = `testify` + `goleak` only. **Every dialect production module requires the engine ONLY** — zero
  test-deps in its `go.mod` (audit F1). Verify both with `go list -deps` / `go mod graph`.
- **Dependency points inward:** the engine imports no dialect module and no driver; dialect modules import the
  engine; `harness` imports the engine + testify; `dbtest` imports the dialect modules + `harness` + drivers.
  No cycles.
- **Identifier-injection discipline preserved** (ADR 0010 D3 finding I1): `ValidateIdent` is the shared
  exported validator; each dialect quotes its own identifiers and validates first; reference-DDL builders are
  per-dialect (`postgres.DDL`/`postgres.InboxDDL`, `mysql.DDL`/`mysql.InboxDDL`) and validate before building.
  There is NO string-returning DDL method on any SPI interface.
- **Testing:** blackbox `_test` packages; `table-test` assert-closure for engine fake unit tests; the `harness`
  module uses `testify` freely (it is its own module — testify never enters root). `use-testcontainers` in the
  `dbtest` module only, via the existing `RunTestDatabase(t, opts...)`-style helpers (moved there).
- **Coverage methodology under leaf-test modules (audit R2-2 — the ≥85% gate is NOT per-prod-module).** The
  dialect production modules (`postgres`/`mysql`) have **no co-located tests** (require the engine only), so
  `go test -cover` on them reports ~0% — that is expected, not a failure. Coverage for the dialect production
  code **and** the engine's dialect-driven paths is measured from the **`dbtest` run** with
  **`-coverpkg=github.com/kartaladev/msgin/adapter/database/sql/...`** (the merged engine+dialect package set),
  evaluated on that merged profile. Engine-intrinsic code (framing, options, Source/Outbound/InboxDeduper
  wiring, `BeginLockTx`/`SettleLockTx` arms) is measured from the **root** fake+stub-driver tests (Task 2). The
  ≥85% target and the hot-path/typed-error-branch requirement apply to these **two measured profiles** (root
  fakes; `dbtest -coverpkg`), not to the empty per-prod-module runs. The harness MUST also exercise the dialect
  **error branches** (e.g. `postgres.DDL("bad; name")` → `ErrInvalidTableName`, the `MsgIDUniqueIndexExists`
  false path) so they are not silently uncovered. Each task states its fake-covered vs dbtest-covered branch
  split (audit F6/R2-1).
- **On-branch module resolution:** no published root tag carries these engine changes, so each dialect/harness/
  dbtest `go.mod` resolves `github.com/kartaladev/msgin` via `go.work` (local) + a temporary
  `replace github.com/kartaladev/msgin => ../../../..` for `GOWORK=off` builds. The `replace` is swapped for a
  pinned `require` only at release (root tagged first — Task 6 / spec §8). `go.work` is gitignored until Task 6
  un-ignores and commits it; earlier tasks create it locally for dev.
- **Commits:** per-task green units; `Spec: 002` / `Plan: 006` / `ADR: 0011` trailers. Whole-branch gate before
  delivery. Per-task commits are pre-authorized (approved plan); `git push` still needs explicit approval.

---

## Task 1 — Explicit-dialect engine API; export `ValidateIdent`/`BeginLockTx`/`SettleLockTx`; delete auto-detect (in-place, still one module)

**Files:**
- `base.go`: `newAdapterBase` takes an explicit `dialect LeaseDialect`; delete the `resolveDialect` fallback
  and the `cfg.dialect` read; validate `dialect != nil` → `ErrNilDialect` (after the `db != nil` /
  `ValidateIdent(table)` checks). Update the doc comment (drop the auto-detect language).
- `source.go` / `outbound.go`: `NewPollingSource(db, table, dialect LeaseDialect, opts...)`,
  `NewOutboundAdapter(db, table, dialect LeaseDialect, opts...)` — dialect is the new required positional arg,
  passed into `newAdapterBase`.
- `inbox_dedup.go`: `NewInboxDeduper(businessDB, dialect InboxDialect, opts...)`; drop the auto-detect /
  `WithInboxDialect` resolution; nil dialect → `ErrNilDialect`.
- `options.go`: remove `WithDialect`, `WithInboxDialect`, `config.dialect`, `inboxConfig.dialect`.
- `errors.go`: add `ErrNilDialect` (exported sentinel); remove `ErrDialectUndetected`.
- `dialect.go`: export `ValidateIdent(name string) error` (rename `validateIdent`; update all call sites in
  `postgres.go`/`mysql.go`/`ddl.go`).
- `lock.go`: rename `beginLockTx`→`BeginLockTx`, `settleLockTx`→`SettleLockTx` (exported, with godoc stating
  the always-commit-on-success / rollback-on-error contract for dialect authors). **Relocate the `txBeginner`
  interface from `mysql.go` into the engine** (`lock.go`) so `BeginLockTx` is self-contained in root; the
  dialect modules will each declare their OWN local `txBeginner` (Go interfaces are structural) in Task 4/5.
- `postgres.go`/`mysql.go`/`ddl.go`: update the (postgresDialect)/(mysqlDialect) `ClaimLock`/`AckLock`/
  `NackLock` bodies to call the exported `BeginLockTx`/`SettleLockTx`; the mysql `Claim` keeps using
  `txBeginner` (now engine-declared, still reachable in-module this task).
- `doc.go`: remove the auto-detect paragraph and the `PostgresDDL`/`PostgresDialect` references; describe the
  explicit-dialect API (a placeholder pointing at the forthcoming `postgres.*`/`mysql.*` packages is fine — the
  package-qualified names land in Tasks 4–5). (Audit F9.)
- **Delete** `detect.go`, and the auto-detect **test cases** in `detect_test.go`, BUT (audit R2-3) `detect_test.go`
  currently **defines the shared root test helpers `openDB(t, driverName)`, `fakeDriver`, `fakeDriverName`**
  used by `outbound_test.go`, `inbox_dedup_test.go`, `strategy_test.go`, and `testutils_test.go`. Do NOT orphan
  them: **relocate `openDB`/`fakeDriver`/`fakeDriverName` (or a minimal `*sql.DB`-stub provider) into a
  surviving root `_test.go`** — e.g. a new `testhelpers_test.go` — **not** `testutils_test.go` (that file's
  container helpers move to `dbtest` in Task 4). Then delete `detect_test.go`.
- **Rewrite the detection-specific cases** that reference the removed symbols in **`outbound_test.go`,
  `inbox_dedup_test.go`, `strategy_test.go`, and `source_lock_test.go`** (grep-confirmed: all reference
  `WithDialect`/`ErrDialectUndetected`) to the explicit-dialect signature. The built-ins
  `PostgresDialect()`/`MySQLDialect()` still exist in root at Task 1, so these tests pass them positionally (no
  Task-2 fake needed yet). Audit R2-3.

**Produces:**
- `NewPollingSource(db *sql.DB, table string, dialect LeaseDialect, opts ...Option) (*Source, error)`
- `NewOutboundAdapter(db *sql.DB, table string, dialect LeaseDialect, opts ...Option) (*Outbound, error)`
- `NewInboxDeduper(businessDB *sql.DB, dialect InboxDialect, opts ...InboxOption) (*InboxDeduper, error)`
- Exported `ValidateIdent`, `BeginLockTx`, `SettleLockTx`; new `ErrNilDialect`.
- **Removed:** `WithDialect`, `WithInboxDialect`, `resolveDialect`, `driverTokens`, `driverTypeName`,
  `ErrDialectUndetected`.

**Hot-path branches:** `newAdapterBase` — nil db → `ErrNilAdapter`; nil dialect → `ErrNilDialect`; invalid
table → `ErrInvalidTableName`; valid → ok. `NewInboxDeduper` — nil db, nil dialect → `ErrNilDialect`, invalid
table, valid. Lock strategy + a dialect not implementing `LockDialect` → `ErrLockStrategyUnsupported`
(unchanged). `BeginLockTx` — `txBeginner` path, `*sql.Tx` path, unsupported-Querier error.

**Tests (root, still dual-engine real-DB, unchanged behavior — one module this task):** rewrite the surviving
`detect_test.go` construction cases as `source`/`outbound`/`inbox` construction tests asserting the new
signatures + the nil-dialect branch (fast, no container — use a trivial inline stub dialect or wait for the
Task-2 fake). The existing dual-engine suites already carry `e.dialect`/`e.inboxDialect`; update them to pass
it explicitly. Confirm `GOTOOLCHAIN=go1.25.0 go test ./adapter/database/sql/ -race` green with
pgx/mysql/testcontainers still present in root this task. `go vet`, `gofmt` clean.

---

## Task 2 — Engine fake-dialect unit tests (root coverage safety net) — runs in parallel with Task 3

**Rationale:** Tasks 4–5 move the real-DB tests out of root; this task adds a **fake dialect + fake `Querier`**
so the engine's Go-logic hot-path branches stay covered in root afterward. **Independent of Task 3** (both
depend only on Task 1) — dispatch them concurrently.

**Files:** `fakedialect_test.go` (an in-memory `LeaseDialect` + `LockDialect` + `InboxDialect` + a fake
`Querier`), `source_unit_test.go`, `outbound_unit_test.go`, `inbox_unit_test.go`, and **`locktx_unit_test.go`**
(blackbox `package sql_test`, `table-test` assert-closure), driving `Source`/`Outbound`/`InboxDeduper` and the
exported `BeginLockTx`/`SettleLockTx` against fakes — **no real DB**.

**Consumes:** the explicit-dialect constructors (Task 1).

- The fake `LeaseDialect` stores rows in an in-memory map and implements `Claim`/`Ack`/`Nack`/`Insert`/
  `EnsureSchema`/`SchemaExists` deterministically; the fake `LockDialect` returns `LockedRow{Tx: nil}` — the
  engine wiring NEVER dereferences `LockedRow.Tx` (audit F6 clean bill), so a nil-Tx fake covers the lock
  wiring; the fake `InboxDialect` covers `InsertInboxIfAbsent`/`PurgeInbox`/`MsgIDUniqueIndexExists`.
- Exercised engine branches: `Source.Poll` empty/rows batching; framing → `Delivery` construction; Ack/Nack
  closures applied vs stale (→ `ErrStaleLease`); `Outbound.Send` payload/framing + `resolveQuerier`
  (no-resolver / resolver-err / shared-tx / strict-no-tx / opportunistic-no-tx); `InboxDeduper.MarkProcessed`
  nil-tx/first/dup, `Ready` ok/not-ready/no-unique-constraint, `Purge` N/0/`ErrInvalidRetention`; construction
  validation (all Task-1 branches). Include a `goleak` `TestMain`.

**`BeginLockTx`/`SettleLockTx` coverage — cover them HERE in root (audit R2-1, correcting F6).** Because the
engine keeps these exported helpers (not relocated per-dialect), and the fake `LockDialect` returns
`LockedRow{Tx:nil}` (never routing through them), and the `dbtest` conformance run exercises only the HAPPY
path — the two `SettleLockTx` rollback arms (Exec-error → rollback; Commit-error → rollback) and
`BeginLockTx`'s unsupported-`Querier` error arm would be **uncovered anywhere**. These are settlement/reliability
hot-path + typed-error branches (a CLAUDE.md delivery blocker if uncovered). In `locktx_unit_test.go`, cover
all arms **directly in root** with a **stdlib-only fake `database/sql/driver`** (a `driver.Conn` with
`BeginTx`, a `driver.Tx` with forceable `Commit`/`Rollback`, a `Stmt`/`Exec` that can be forced to error) — a
registered fake driver opened via `sql.Open`, exactly the shape the current `strategy_test.go` `fakeQuerier`
relies on. **Zero new deps.** Covers: `BeginLockTx` txBeginner-path / `*sql.Tx`-path / unsupported-Querier
error; `SettleLockTx` success / Exec-error-rollback / Commit-error-rollback.

**Coverage split (stated honestly):** every engine branch is covered in root by this task — the fake dialect
for the wiring, the stub-driver tests for `BeginLockTx`/`SettleLockTx`. The `dbtest` conformance runs
(Tasks 4–5) cover the concrete-dialect SQL (which lives in the dialect modules), measured via `-coverpkg`
(see Global Constraints / spec §6). Enumerate the exact branch list in the task report so none is silently
uncovered.

**Tests:** `go test ./adapter/database/sql/ -race` green; engine coverage at least the pre-refactor level,
including the `BeginLockTx`/`SettleLockTx` arms. This task ADDS tests; removes none. Include a `goleak`
`TestMain`.

---

## Task 3 — The `harness` module: `RunSource`/`RunLock`/`RunOutbound`/`RunOutbox`/`RunInbox`/`RunDialect` + `TestKit` — runs in parallel with Task 2

**Files:** new module `adapter/database/sql/harness/{go.mod,go.sum}` (module
`github.com/kartaladev/msgin/adapter/database/sql/harness`, `require github.com/kartaladev/msgin` via
`go.work` + a dev `replace ../../../..`, `require github.com/stretchr/testify`), and
`harness/{harness.go,testkit.go,source.go,lock.go,outbound.go,outbox.go,inbox.go,dialect.go}` (package
`harness`). Port the assertion logic from the current root real-DB suites (`source_pg_test.go`,
`source_lock_test.go`, `outbound_pg_test.go`, `outbox_pg_test.go`, `inbox_dedup_pg_test.go`,
`dialect_pg_test.go`, `mysql_test.go`, `strategy_test.go`) into reusable functions.

**Produces (package `harness`):**
- `type TestKit struct { Lease msginsql.LeaseDialect; Inbox msginsql.InboxDialect; Quote func(string) string;
  Placeholder func(int) string; NowExpr func() string; HeadersTextExpr func(col string) string; DDL func(table
  string) (string, error); InboxDDL func(table string) (string, error); … }` — the dialect PLUS the per-dialect
  test primitives the verification SQL needs but that are NOT on the SPI (audit F4 + R2-6): identifier quoting
  (backtick vs `"`), placeholders (`?` vs `$n`), server-now (`now()` vs `UTC_TIMESTAMP(6)`), the headers→text
  cast the current suites use (`engine.headersTextExpr`: `::text` vs bare), and the per-dialect reference-DDL
  builders (`postgres.DDL`/`postgres.InboxDDL`). Read the current suites' test-side helpers
  (`engine.quote`/`engine.ph`/`nowExpr`/`headersTextExpr`/`referenceDDL`/`inboxDialect()`) and lift EVERY one
  into a TestKit field — do not under-scope. The `dbtest` runner fills these per engine.
- `RunSource(t *testing.T, kit TestKit, db *sql.DB)` and peers `RunLock`, `RunOutbound`, `RunOutbox`,
  `RunInbox`, `RunDialect` — each taking an already-open `*sql.DB` (the runner supplies the container) + the
  `TestKit`; each provisions its own fresh tables (via `kit.Lease.EnsureSchema`/`kit.Inbox.EnsureInboxSchema`,
  mirroring the current `freshTable`) and reproduces EVERY assertion the corresponding suite makes.

- **Behavior parity is the bar:** claim bumps counts/locks; fenced Ack/Nack; stale-lease `ErrStaleLease`;
  `visible_after` redelivery; lock-strategy always-commit-Nack + detached tx + separate-pool DLQ; outbox
  atomicity + commit-gated visibility + strict/opportunistic branches; inbox idempotency +
  atomic-rollback-retryable + MySQL false-positive guard + Purge + Ready unique-constraint fail-fast — each
  reproduced as a harness assertion. The harness drives the engine's **EXPORTED** API only (it is a separate
  module) — verify the exported surface is sufficient; if a suite reached an unexported helper, re-express it
  through the public API (never re-export engine internals just for the harness).
- **Audit F8:** author `RunSource` (and any lock/lease concurrency assertion) with **correctness-only**
  concurrency checks (disjoint claims, no double-claim, no lost message), NOT non-blocking/throughput
  assumptions, so a single-writer engine (SQLite, increment B) passes the SAME contract unchanged.
- The harness imports the engine + `testify` + stdlib `database/sql`/`testing` ONLY — **no driver, no
  testcontainers**. Verify with `go list -deps`.

**Hot-path branches:** the harness RE-EXPRESSES the engine's real-DB coverage; it introduces no new engine
branches. Its own logic (table provisioning, assertion helpers) is exercised when Tasks 4–5 run it.

**Tests / verification (this task):** the harness is a library of test-driver functions; it is first EXECUTED
against a real DB in Task 4. This task's green bar is: `cd adapter/database/sql/harness && GOTOOLCHAIN=go1.25.0
GOFLAGS=-mod=mod go build ./... && go vet ./...` clean under `go.work`; `go list -deps` shows no
driver/testcontainers import. **If Task 4 surfaces a harness defect, the Task-4 implementer fixes it in the
harness module** (same branch) — acceptable, since Task 4 is the harness's first real execution.

---

## Task 4 — `postgres` prod module + `dbtest` runner module: move Postgres out of root; run the harness vs a real Postgres

**Files:**
- New module `adapter/database/sql/postgres/{go.mod,dialect.go,ddl.go,quote.go,doc.go}` (module …`/postgres`,
  `require github.com/kartaladev/msgin` via `go.work` + dev `replace`; **no test-deps**). MOVE from root
  (verbatim): `postgresDialect` + all its methods (`postgres.go`), the Postgres DDL builders + `pgQuote*`
  (`ddl.go` + `postgres.go`), and the `var _ LockDialect = postgresDialect{}` / `var _ InboxDialect = …`
  compile-time assertions (into this module). Declare a module-local `txBeginner` if the moved `Claim`/lock
  code needs it (structural). Update DDL builders to call `msginsql.ValidateIdent`.
- New module `adapter/database/sql/dbtest/{go.mod,testutils_test.go,conformance_pg_test.go}` (module …`/dbtest`,
  `require`s `…/postgres` + `…/harness` + `jackc/pgx/v5` + `testcontainers-go/modules/postgres` + testify +
  goleak, via `go.work` + dev `replace` for the engine). MOVE `RunTestDatabase` (+ the Postgres testcontainers
  wiring) from root's `testutils_test.go` here. `conformance_pg_test.go` builds the Postgres `TestKit`
  (`postgres.LeaseDialect()`, `postgres.InboxDialect()`, pg `Quote`/`Placeholder`/`NowExpr`) and runs
  `harness.RunSource/RunLock/RunOutbound/RunOutbox/RunInbox/RunDialect` against a real Postgres container.
  Include a `goleak` `TestMain`.
- Create `go.work` locally (`use ./ ./adapter/database/sql/harness ./adapter/database/sql/postgres
  ./adapter/database/sql/dbtest`) — still gitignored (committed in Task 6).
- **Delete from root:** `postgres.go`; the Postgres halves of `ddl.go`; the pg compile-time assertions in
  `lock.go`/`inbox_dedup.go`; and the Postgres-specific root suites now hosted in `dbtest`
  (`source_pg_test.go`, `outbound_pg_test.go`, `outbox_pg_test.go`, `inbox_dedup_pg_test.go`,
  `dialect_pg_test.go`, and the pg-only parts of `source_lock_test.go`/`strategy_test.go`). **Root must stay
  green** — the MySQL dialect + its real-DB suites remain in root this task, so root still `require`s the mysql
  driver + testcontainers (mysql/mariadb modules); `pgx` + `testcontainers/modules/postgres` drop from root via
  `go mod tidy`.

**Produces (package `postgres`):** `postgres.LeaseDialect() msginsql.LeaseDialect` (the single stateless value
also satisfies `msginsql.LockDialect` + `msginsql.InboxDialect`), `postgres.InboxDialect() msginsql.InboxDialect`,
`postgres.DDL(table) (string, error)`, `postgres.InboxDDL(table) (string, error)`.

- **R2-4 — strategy construction test hand-off:** `strategy_test.go`'s `StrategyLockForUpdate` *construction*
  cases reference `msginsql.PostgresDialect()`; once the built-in leaves root, swap them to the **Task-2 root
  fake `LockDialect`** (which satisfies `LockDialect`), keeping them as root unit tests. (`TestClaimLock_
  RequiresDBOrTxQuerier`, the mixed pg+mysql guard test, is already **superseded** by Task 2's direct
  `BeginLockTx` unsupported-`Querier` test — remove its dialect-driven form rather than migrate it.)
- **R2-5 — checkpoint (this is the branch's largest task).** Prove the vertical incrementally: wire + run
  `harness.RunSource` against the Postgres container FIRST (validates the TestKit + the exported-API-only
  contract end-to-end), reach green, THEN wire the remaining `RunLock`/`RunOutbound`/`RunOutbox`/`RunInbox`/
  `RunDialect`. If the harness needs a fix, fix it in the `harness` module (same branch). The coordinator
  verifies at the RunSource checkpoint before the rest.

**Hot-path branches:** unchanged (the dialect SQL is identical); Postgres coverage now accrues in the `dbtest`
module via the harness run, including the real-`*sql.Tx` `BeginLockTx`/`SettleLockTx` *happy-path* branches
(the rollback/error arms are covered in root by Task 2 — audit R2-1).

**Tests:** `cd adapter/database/sql/dbtest && GOTOOLCHAIN=go1.25.0 go test ./... -race` green (full Postgres
conformance vs a real container, goleak-clean — **first real execution of the harness**; fix any harness defect
in the `harness` module). Root still green: `cd <root> && go test ./adapter/database/sql/ -race` (now
Postgres-free; MySQL + fake suites remain). `postgres` module: `go build ./... && go vet ./...` clean; its
**go.mod require block** has no test-deps (require the engine only). **Note (R2-7):** the `postgres` module's
**go.sum** is still polluted at this task (root still carries pgx/mysql/testcontainers until Task 5), so the
clean-`go.sum` isolation probe is deferred to Task 6 — do not evaluate go.sum cleanliness here, only the
go.mod require block. `go.work` lets local `go test ./...` see all modules.

---

## Task 5 — `mysql` prod module + extend `dbtest` (MySQL + MariaDB); root goes lean (driver-free)

**Files:**
- New module `adapter/database/sql/mysql/{go.mod,dialect.go,ddl.go,quote.go,doc.go}` (module …`/mysql`,
  `require` the engine via `go.work` + dev `replace`; **no test-deps**). MOVE from root (verbatim):
  `mysqlDialect` + all methods (`mysql.go`), the MySQL DDL builders + `mysqlQuote*` + `placeholders`, the MySQL
  compile-time assertions, and the module-local `txBeginner` for the two-step claim. Update DDL builders to
  call `msginsql.ValidateIdent`.
- Extend `dbtest`: `conformance_mysql_test.go` builds the MySQL `TestKit` and runs the harness against **MySQL
  and MariaDB**; MOVE `RunTestMySQL`/`RunTestMariaDB` (+ their testcontainers wiring) from root into `dbtest`'s
  `testutils_test.go`. Add `go-sql-driver/mysql` + `testcontainers/modules/mysql`+`/mariadb` to `dbtest`'s
  `go.mod`.
- Extend `go.work` with `./adapter/database/sql/mysql`.
- **Delete from root:** `mysql.go`; the now-empty `ddl.go` in full — **including the `InboxDDL(d, table)`
  type-switch dispatcher, which is DELETED** (replaced by per-dialect `postgres.InboxDDL`/`mysql.InboxDDL`;
  audit F3); the remaining MySQL/MariaDB real-DB root suites (`mysql_test.go`, the MySQL parts of
  `strategy_test.go`/`source_lock_test.go`) → their conformance moves to `dbtest`; and the container helpers
  left in root's `testutils_test.go`. Then `go mod tidy` the root.
- **R2-4:** any `strategy_test.go` `StrategyLockForUpdate` construction case still referencing
  `msginsql.MySQLDialect()` swaps to the Task-2 root fake `LockDialect` (stays a root unit test); no
  construction case should reference a built-in after this task.

**Produces (package `mysql`):** `mysql.LeaseDialect()`, `mysql.InboxDialect()`, `mysql.DDL(table)`,
`mysql.InboxDDL(table)` (one stateless value satisfying `LeaseDialect` + `LockDialect` + `InboxDialect`),
covering MySQL and MariaDB (`LOCK IN SHARE MODE`, `UTC_TIMESTAMP(6)`, two-step claim — moved verbatim).

**Hot-path branches:** unchanged.

**Tests:**
- `cd adapter/database/sql/dbtest && go test ./... -race` green (Postgres + MySQL + MariaDB conformance,
  goleak-clean).
- **Root is now driver-free:** `go mod tidy` drops `pgx`, `go-sql-driver/mysql`, `testcontainers-go` (+ all
  Docker/moby/OTel indirects); `go test ./adapter/database/sql/ -race` green on the fake + framing tests only;
  root `go.mod` production deps = stdlib + `clockwork`, test deps = `testify` + `goleak`. **Verify with
  `go list -deps ./... | grep -E 'pgx|go-sql-driver|testcontainers'` → empty** for the root module.
- `mysql` module: `go build ./... && go vet ./...` clean, **no test-deps in its go.mod**.

---

## Task 6 — Commit `go.work`; create CI + release workflows; empirical `go.sum` isolation probe

**Files:**
- `.gitignore`: remove the `go.work` / `go.work.sum` ignore lines (they are now committed).
- Commit `go.work` (repo root: `use ./` + `harness` + `postgres` + `mysql` + `dbtest`).
- Create `.github/workflows/ci.yml` (does not exist — audit F5): a **matrix over modules** (root, harness,
  postgres, mysql, dbtest) running with `GOTOOLCHAIN=go1.25.0` **and `GOWORK=off`** (`-mod=mod`) — `go build`,
  `go test ./... -race` (dbtest needs Docker services), `go vet`, `gofmt -l`, `govulncheck`, `golangci-lint`,
  `go mod tidy` stability check, `CGO_ENABLED=0 go build`. Plus **one aggregate workspace-build job** (GOWORK
  on) so the workspace stays coherent. `GOWORK=off` is mandatory so each module is exercised against its
  `require`d/`replace`d engine, not the workspace (audit F5).
- Create `.github/workflows/release.yml`: accept **module-path-prefixed** SemVer tags
  (`adapter/database/sql/postgres/vX.Y.Z`, `…/mysql/vX.Y.Z`, `…/harness/vX.Y.Z`) alongside the root `vX.Y.Z`;
  publish a GitHub Release with auto-generated notes (matches the CLAUDE.md release model).
- Create `docs/RELEASE.md` (or a section): document the **release choreography** — root is tagged FIRST, then
  each dialect/harness module swaps its dev `replace github.com/kartaladev/msgin` → a pinned `require
  vX.Y.Z` and is tagged with its module-prefixed SemVer (audit F5 / spec §8). Note the dev-time `replace`
  directives that CI relies on under `GOWORK=off`.

**Empirical isolation check (the whole point — the acceptance gate):** re-run the throwaway-consumer probe from
spec 002 §1 against the refactored tree (in the scratchpad, `GOWORK=off`):
- a consumer importing only `adapter/database/sql` (the engine) → `go.sum` free of pgx/mysql/testcontainers/
  docker/otel;
- a consumer importing `…/postgres` → `go.sum` free of testcontainers/docker/otel (only the engine closure);
- record the before/after line counts (target: 102 → 0 polluted lines) in the task report.

**Hot-path branches:** none (workflow/config task).

**Tests / verification:** the workflows parse (`actionlint` if available); `go.work` + `GOWORK=off` per-module
`go build`/`go vet` all pass locally; the isolation probe numbers recorded. This task's diff is config +
docs + `go.work`.

---

## Delivery gate (coordinator, after Task 6 — not an SDD implementer task)

Per CLAUDE.md §5 over the whole branch (`main..HEAD`): `/code-review` + `/security-review`, resolve/triage
every finding; each of the 5 modules `go test ./... -race` green (shake with `-count=2` where feasible);
`go vet`, `gofmt`/`gofumpt`, `CGO_ENABLED=0` build, `go mod tidy` stability, `go mod verify`, `govulncheck`,
`golangci-lint`; coverage ≥ 85% on the **two measured profiles** (root fakes+stub-driver; `dbtest -coverpkg`
over the merged engine+dialect packages — see Global Constraints, audit R2-2), every listed hot-path/
typed-error branch covered; the `go.sum` isolation probe passing (102 → 0). Update `docs/HANDOVER.md` + memory.
**Push the branch for the user's GitHub MR — do NOT fast-forward `main`** (session directive; `git push` needs
explicit approval per CLAUDE.md).

## Traceability

Commits carry `Spec: 002`, `Plan: 006`, `ADR: 0011` trailers. This plan implements spec 002 increment A;
ADR 0011 amends ADR 0003 (engine/dialect split) + ADR 0010 (explicit-dialect constructors, auto-detect
removal). **Successors:** Plan 007 (SQLite dialect, ADR 0012) then Plan 005 Task 11 (docs/examples), both
against the module layout this plan establishes.

## Execution notes (parallelism & sequencing)

- **Sequencing:** 1 → (2 ∥ 3) → 4 → 5 → 6. Task 1 (explicit API) unblocks everything. **Task 2 (engine fake
  unit tests) and Task 3 (harness authoring) are independent** — both consume only Task 1 — so dispatch their
  implementer subagents **concurrently** (the coordinator verifies + commits each as it lands; commit order
  between them is free). Task 4 needs the harness (3) + explicit API (1); Task 5 needs `dbtest` (4); Task 6
  needs all modules (5).
- **Root-green invariant:** verify `go test ./adapter/database/sql/ -race` stays green after Tasks 1, 4, and 5
  (the tasks that touch root), and the moved-to `dbtest` suites green after 4 and 5.
- **SDD loop per task:** fresh implementer subagent (TDD red→green, consults `golang-how-to` + the custom
  skills, uses `gopls`) → coordinator verifies `-race` green + commits (per-task commit pre-authorized) →
  adversarial reviewer subagent before moving on; fold review findings before the next task.

## Self-review notes

- **Spec coverage:** engine driver-free (Tasks 1,4,5) → §2 goal (a); dialect-consumer `go.sum` clean via
  leaf-test modules (Tasks 4,5,6 + probe) → goal (a)/(b) + audit F1; open-for-extension via SPI + `harness`
  (Tasks 3–5) → goal (c); behavior-preserving (Global Constraints, all tasks) → goal (e). SQLite (goal d) is
  Plan 007, not here.
- **Audit fold-in (round 1):** F1 (leaf-test modules) → Tasks 4–6 + probe; F2 (BeginLockTx/SettleLockTx
  exported, txBeginner relocated) → Tasks 1–5; F3 (ddl.go departs, InboxDDL dispatcher deleted, assertions
  relocated, root-green checks) → Tasks 4–5; F4 (TestKit) → Task 3; F5 (GOWORK=off CI + release bootstrap,
  created from scratch) → Task 6; F7 (harness its own module, testify) → Task 3; F8 (correctness-only lease
  concurrency) → Task 3; F9 (doc.go + goleak TestMain per module) → Tasks 1–5.
- **Audit fold-in (round 2):** R2-1 (BeginLockTx/SettleLockTx covered in root via stub-driver) → Task 2;
  R2-2 (coverage measured on two profiles, not per empty prod-module) → Global Constraints + delivery gate +
  spec §6; R2-3 (relocate `openDB`/`fakeDriver` helpers, enumerate detection-case rewrites) → Task 1;
  R2-4 (TestClaimLock→BeginLockTx direct test; strategy construction → fake LockDialect) → Tasks 2/4/5;
  R2-5 (Task 4 RunSource-first checkpoint) → Task 4; R2-6 (full TestKit knob list) → Task 3; R2-7 (Task 4
  go.mod-scoped clean claim, go.sum probe deferred to Task 6) → Task 4.
- **Coverage honesty (R2-1, correcting F6):** `BeginLockTx`/`SettleLockTx` tx arms (both rollbacks + the
  unsupported-`Querier` error) are covered IN ROOT via a stdlib fake driver (Task 2) — the `dbtest` happy path
  never hits the rollback arms — so no branch is silently uncovered.
- **Deliberately deferred:** SQLite (Plan 007), Task 11 docs/examples + the consumer migration guide for the
  breaking API change (fold into Task 11).
</content>
</invoke>
