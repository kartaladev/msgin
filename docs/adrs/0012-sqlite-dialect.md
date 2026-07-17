# ADR 0012 — SQLite dialect for the `sql` adapter (lease-only, INTEGER-µs clock)

- **Status:** Proposed (2026-07-18) — brainstorm folded; user-approved the design (INTEGER unix-micros
  timestamps, driver-free `sqlite.DSN` builder, `RunTestSQLite` with WAL-file default + shared-memory option,
  conditional-lock harness fix). Pending the **adversarial audit** of the spec+ADR+plan bundle before
  implementation (CLAUDE.md design-time gate).
- **Context source:** [Spec 002 — sql multi-module split + SQLite](../specs/002-sql-multi-module-and-sqlite.md)
  §7 (SQLite dialect) and §9 (sequencing, increment B).
- **Predecessor:** [ADR 0011 — sql engine/dialect module split](0011-sql-engine-dialect-module-split.md)
  (increment A) established Structure Z: a driver-free engine + per-dialect leaf-test modules + the `harness`
  and `dbtest` modules. This ADR adds a third dialect module against that structure, unchanged.
- **Builds on:** [ADR 0010 — Poller + SQL adapter](0010-poller-sql-adapter.md) (the lease/claim + lock
  strategies, the DB-clock invariant D3/D4, the dedup inbox D10) and [ADR 0003 — Multi-module layout](0003-multi-module-repository-layout.md).
- **Realized by:** Plan 007 (SQLite dialect). Traceability: Spec 002 → ADR 0012 → Plan 007 → code.

## Context

Increment A split `adapter/database/sql` into a driver-free engine plus per-dialect modules (`postgres`,
`mysql`), a reusable conformance `harness`, and a `dbtest` leaf runner (ADR 0011). Increment B adds a **SQLite**
dialect against that same structure. SQLite is attractive for msgin's scope: it is **embedded** (no server, no
Docker — tests run in-process), and `modernc.org/sqlite` is a **pure-Go, cgo-free** driver, so it fits the
library's no-cgo gate and needs no container.

SQLite differs from Postgres/MySQL in three ways that shape the dialect:

1. **Single-writer concurrency, no row-level locks.** SQLite has no `SELECT … FOR UPDATE` and no `SKIP
   LOCKED`; it serializes writers by holding a database-wide write lock for the duration of each write
   statement/transaction. So the lock/`FOR UPDATE` strategy is not implementable, and the lease/claim strategy
   must be adapted to SQLite's serialization model.
2. **No native datetime type.** SQLite stores dates as `TEXT`, `REAL` (julianday), or `INTEGER` (unix epoch)
   by convention — the schema author picks. The DB-clock invariant (ADR 0010 D3/D4: all row timestamps come
   from the DB server clock, never the app clock) must be preserved regardless.
3. **`SQLITE_BUSY` under contention.** Concurrent writers (multiple pollers, or a worker pool acking/nacking
   in parallel) collide on the single write lock and fail with `SQLITE_BUSY` unless WAL journal mode and a
   `busy_timeout` are configured so writers serialize (block-and-retry) instead of erroring. msgin never owns
   the caller's `*sql.DB` (the driver and DSN are the caller's choice, exactly as for PG/MySQL), so this is
   caller configuration — but the library must guide it, and the conformance tests must configure it.

## Decision

Ship a **`adapter/database/sql/sqlite`** production module (requires the engine ONLY — driver-free, dev-time
`replace` like `postgres`/`mysql`) implementing the engine's `LeaseDialect` and `InboxDialect`, plus a
driver-free DSN builder. SQLite conformance runs in the existing `dbtest` runner with an embedded (Docker-free)
database. Concretely:

### D1 — Lease-only; no lock/`FOR UPDATE` strategy

`sqlite.LeaseDialect()` implements `LeaseDialect` + `InboxDialect` but **deliberately NOT `LockDialect`**.
Selecting `WithStrategy(StrategyLockForUpdate)` with it therefore returns the engine's existing
`ErrLockStrategyUnsupported` at construction — no new error, no new engine code. There is no compile-time
assertion (Go cannot assert *non*-implementation); a runtime `_, ok := sqlite.LeaseDialect().(msginsql.LockDialect)`
test asserts `ok == false` in `dbtest`.

### D2 — Claim is a one-shot atomic `UPDATE … RETURNING` (no `SKIP LOCKED`)

Because SQLite serializes writers, a single write statement is atomic against all other writers — so the claim
needs neither `FOR UPDATE SKIP LOCKED` (Postgres) nor a two-step SELECT-then-UPDATE transaction (MySQL). It is
a **one-shot** statement, closest in shape to Postgres:

```sql
UPDATE "t" SET
  locked_by = ?, locked_at = <now>,
  lease_epoch = lease_epoch + 1, delivery_count = delivery_count + 1
WHERE id IN (
  SELECT id FROM "t"
  WHERE visible_after <= <now>
    AND (locked_at IS NULL OR locked_at <= <now> - ?)   -- inlined reaper: leaseTTL µs
  ORDER BY visible_after
  LIMIT ?
)
RETURNING id, msg_id, headers, payload, delivery_count, lease_epoch
```

`<now>` is the DB-clock expression (D4). `RETURNING` returns the post-update `delivery_count`/`lease_epoch`
(no Go-side +1 as MySQL needs). Requires SQLite ≥3.35 (`RETURNING`, 2021). The claim runs on the injected
`Querier` directly (the pool `*sql.DB` in auto-commit is a single write statement holding the write lock; an
already-open `*sql.Tx` also works) — no `txBeginner` branch. `Ack` (fenced `DELETE`), `Nack` (fenced
clear-lock + `visible_after = <now> + ?`), and `Insert` (`visible_after = <now> + ?`) mirror the Postgres
shapes with `?` placeholders and integer-µs arithmetic.

**Empirically verified** (no-Docker spike against `modernc.org/sqlite` v1.54.0 / SQLite 3.53.3, audit round 1):
the one-shot `UPDATE … WHERE id IN (SELECT … LIMIT ?) RETURNING …` executes and bumps the fence/counter
correctly, and 8 concurrent pool claimers over 200 rows produce **zero double-claims** (disjoint id sets) —
so the at-least-once + single-claim guarantee holds under SQLite's write serialization.

### D3 — Inbox dedup is exact (`ON CONFLICT … DO NOTHING RETURNING`)

SQLite supports `INSERT … ON CONFLICT(msg_id) DO NOTHING RETURNING msg_id` (upsert since 3.24, `RETURNING`
since 3.35), so `InsertInboxIfAbsent` is **exact** like Postgres — a returned row means inserted
(`already=false`); `sql.ErrNoRows` means a genuine duplicate (`already=true`). There is no MySQL `INSERT
IGNORE` error-demotion problem and thus no `ErrInboxInsertFailed` path. Consequently the SQLite `TestKit` sets
`MySQLFamily=false`, so the harness skips the INSERT-IGNORE-specific assertion. `MsgIDUniqueIndexExists` uses
`pragma_index_list`/`pragma_index_info` (msg_id is `TEXT PRIMARY KEY` → a `"unique"=1` autoindex);
`SchemaExists` probes `sqlite_master` (`type='table' AND name=?`) — SQLite has no `information_schema`.

### D4 — Timestamps: `INTEGER` unix microseconds, DB-clock only

All row-time columns (`visible_after`, `locked_at`, `created_at`, `processed_at`) are `INTEGER` epoch
microseconds. The DB clock is `CAST(unixepoch('now','subsec')*1000000 AS INTEGER)`; delays and lease-TTL are
passed as plain integer microseconds and combined with `+`/`-` — **exact parity** with the `.Microseconds()`
interval arithmetic the Postgres and MySQL dialects already use. Resolution is milliseconds (SQLite `'subsec'`
yields 3 fractional digits) expressed in microseconds — ample for a message queue's visibility/lease timing.

The DB-clock invariant (ADR 0010 D3/D4) is preserved: every persisted timestamp comes from SQLite's own
`unixepoch('now',…)`, never the app clock, so there is no app↔DB skew. `'subsec'` requires SQLite ≥3.42
(2023); `modernc.org/sqlite` bundles ≥3.45, so the requirement is met. Rejected alternatives: **TEXT ISO-8601**
(`strftime('%Y-%m-%d %H:%M:%f','now')`) — human-readable but only millisecond precision, lexical-format
fragility, and string-modifier delay arithmetic that diverges from the other dialects' integer-µs math;
**REAL julianday** — float rounding near comparison boundaries. The `harness.TestKit.NowExpr` returns the
INTEGER-µs expression; `HeadersTextExpr` is the identity (headers is a plain `TEXT` column — no jsonb cast).

### D5 — Reference DDL

`sqlite.DDL(table)` / `sqlite.InboxDDL(table)` validate the identifier (`ValidateIdent`) then build:

```sql
-- lease/claim schema
CREATE TABLE IF NOT EXISTS "t" (
  id             INTEGER PRIMARY KEY,          -- rowid alias, auto-increments
  msg_id         TEXT    NOT NULL,
  headers        TEXT    NOT NULL,
  payload        BLOB    NOT NULL,
  locked_by      TEXT,
  locked_at      INTEGER,
  visible_after  INTEGER NOT NULL DEFAULT (CAST(unixepoch('now','subsec')*1000000 AS INTEGER)),
  delivery_count INTEGER NOT NULL DEFAULT 0,
  lease_epoch    INTEGER NOT NULL DEFAULT 0,
  created_at     INTEGER NOT NULL DEFAULT (CAST(unixepoch('now','subsec')*1000000 AS INTEGER))
);
CREATE INDEX IF NOT EXISTS "t_claim_idx" ON "t" (visible_after) WHERE locked_at IS NULL;

-- dedup inbox schema
CREATE TABLE IF NOT EXISTS "t" (
  msg_id       TEXT    PRIMARY KEY,
  processed_at INTEGER NOT NULL DEFAULT (CAST(unixepoch('now','subsec')*1000000 AS INTEGER))
);
CREATE INDEX IF NOT EXISTS "t_processed_idx" ON "t" (processed_at);
```

Identifiers are double-quoted (`sqliteQuote`); the partial claim index (`WHERE locked_at IS NULL`) is supported
since 3.8.0. `EnsureSchema`/`EnsureInboxSchema` execute the `CREATE TABLE` and `CREATE INDEX` as **separate**
`IF NOT EXISTS` statements (idempotent, and not every driver path accepts multi-statement `Exec`). As with the
other dialects, DDL is never run implicitly on the production path (ADR 0010 D2) and the string-returning
builders are the only public DDL entry points (they revalidate the identifier; there is no DDL method on the
SPI — the identifier-injection discipline of ADR 0010 D3 / ADR 0011).

### D6 — Driver-free production DSN builder (`sqlite.DSN`)

The `sqlite` module ships an opinionated-but-overridable **pure-string** DSN builder (no driver import, keeping
the module engine-only):

```go
func DSN(path string, opts ...DSNOption) string
// default:  file:<path>?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)
// options:  WithJournalMode(mode), WithBusyTimeout(d), WithSharedMemory()
```

It emits `modernc.org/sqlite`-flavored pragmas (`_pragma=…`), the recommended cgo-free driver; a caller using a
different driver builds their own DSN (documented). `doc.go` states the delivery guarantee (at-least-once) and
explains **why** WAL + a non-zero `busy_timeout` are required for concurrent consumers (without them,
concurrent writers get `SQLITE_BUSY`). This is the "sensible default, fully overridable" pattern (CLAUDE.md)
applied to connection guidance the library cannot own directly.

### D7 — Test provisioning in `dbtest` (embedded, no Docker)

`modernc.org/sqlite` is added to the **`dbtest` runner only** (the leaf module nobody imports); the `sqlite`
prod module's `go.sum` stays engine-only. `dbtest` gains `RunTestSQLite(t, opts ...)`:

- **Default:** a WAL temp-file (`file:<t.TempDir()>/msgin.db?_pragma=journal_mode(WAL)&_pragma=busy_timeout(…)`),
  torn down by `t.Cleanup` — production-representative; multiple pool connections see one database.
- **`WithSharedMemory()`:** `file::memory:?cache=shared` — an ephemeral in-memory alternative.

`RunTestSQLite` builds its DSN via `sqlite.DSN(...)` (dogfooding D6). `conformance_sqlite_test.go` builds
`sqliteKit()` (`MySQLFamily=false`, `SingleWriter=true`, INTEGER-µs `NowExpr`, identity `HeadersTextExpr`,
double-quote `Quote`, `?` `Placeholder`) and runs `RunSource`, `RunOutbound`, `RunOutbox`, `RunInbox`,
`RunDialect` — **omitting `RunLock`** (no `LockDialect`) — plus the D1 lock-strategy-unsupported assertion.

`RunSource`'s genuinely-concurrent cases pass on SQLite because each claim/ack/nack is a discrete auto-commit
statement that serializes on the writer lock (never held across another connection's acquisition) — verified
by the spike. The ONE exception is `RunOutbox/CommitGatesVisibility`, which is skipped for single-writer
engines — see D8.

### D8 — Two behavior-preserving changes to the `harness` module

Both are gated so Postgres/MySQL behavior is unchanged (still fully covered via `dbtest`); each only affects a
lease-only, single-writer dialect.

1. **Conditional `ClaimLock` invalid-identifier coverage.** `harness.RunDialect`'s
   `InvalidIdentifierRejectedPerSPIMethod` subtest hard-requires `kit.Lease.(LockDialect)` (via
   `require.True`) to cover the `ClaimLock` bad-identifier branch. For a lease-only dialect that assertion
   fails. Make **only that one `ClaimLock` case conditional** on the type-assertion: run it when the dialect
   implements `LockDialect` (Postgres/MySQL), skip it otherwise (SQLite).

2. **`TestKit.SingleWriter` gate on `RunOutbox/CommitGatesVisibility`** (audit F1, empirically confirmed). That
   subtest opens a caller `*sql.Tx`, does the outbox `Insert` on it (**holding SQLite's single writer lock,
   uncommitted**), then issues a concurrent `Source.Poll` whose lease `Claim` is a write `UPDATE` on the pool
   (a second connection). On Postgres/MySQL the uncommitted row is MVCC/`SKIP LOCKED`-invisible, so the poll
   returns empty with no error. On single-writer SQLite the pool's `UPDATE` must take the writer lock at
   statement start (before its `WHERE` is evaluated, so "zero rows match" does not save it), blocks for the
   full `busy_timeout`, then returns `SQLITE_BUSY` — the spike measured a 5.05 s stall then `database is
   locked (5)`. This is NOT an F8 correctness-only concurrency case; it asserts an MVCC non-blocking-read
   property SQLite structurally lacks. Add a `TestKit.SingleWriter bool` field (zero value `false` → the
   subtest runs, so PG/MySQL are unaffected) and skip **only** `CommitGatesVisibility` when it is `true`.
   `sqliteKit` sets `SingleWriter=true`. The transactional-outbox atomicity guarantee remains covered by
   `RunOutbox/Atomicity` (commit + rollback branches), which does not hold a write tx open across a competing
   writer.

## Consequences

**Positive**

- A third shipped engine with **no server and no Docker** — the fastest, lowest-friction adapter to run, ideal
  for local development, embedded deployments, and CI. Reinforces "open for extension" (ADR 0011): a new
  dialect is a new module, zero engine change (the only shared-code touch is the two gated D8 harness tweaks,
  both behavior-preserving for the existing engines).
- The engine's exported API is unchanged; `ErrLockStrategyUnsupported` already existed for exactly this case.
  The DB-clock invariant and at-least-once guarantee hold uniformly across all three engines.
- INTEGER-µs timestamps keep SQLite's interval math identical to PG/MySQL, minimizing cognitive and test
  divergence, and give unambiguous ordering with no text-format or float-rounding hazards.
- `modernc.org/sqlite` keeps the no-cgo gate (`CGO_ENABLED=0` still builds) and stays confined to `dbtest`, so
  no consumer — not even a SQLite consumer — inherits it transitively via the prod module.

**Negative / trade-offs**

- **Single-writer throughput.** SQLite does not do competing-consumer parallelism at the DB level; a worker
  pool serializes on the write lock. This is documented (at-least-once holds; throughput is bounded). Callers
  needing high write concurrency choose Postgres/MySQL. The conformance concurrency cases assert correctness,
  not throughput (audit F8).
- **No lock/`FOR UPDATE` strategy** — lease/claim only. Acceptable: the lease strategy is the default and
  fully sufficient; the lock strategy is a PG/MySQL-specific option.
- **Version floor.** Requires SQLite ≥3.42 (`unixepoch('now','subsec')`); satisfied by `modernc.org/sqlite`'s
  bundled engine and documented. A caller pointing the dialect at an ancient SQLite build would fail — an
  explicit, documented constraint rather than a silent mis-behavior.
- **Millisecond effective resolution** for visibility/lease timing (µs unit, ms granularity). Immaterial for a
  queue; noted for completeness.
- **DSN builder targets one driver's pragma syntax** (`modernc`). Mitigated by documentation and by the
  builder being optional (callers may ignore it and construct any DSN).
