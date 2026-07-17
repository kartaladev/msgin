# ADR 0010 — Poller + `database/sql` adapter (credit-at-fetch pull path, dialect-owned SQL)

- **Status:** Proposed (2026-07-17) — pending the two adversarial audits + reconciliation
- **Context source:** [Spec 001 — Messaging core](../specs/001-messaging-core.md) §7.4.1 (credit-at-fetch),
  §7 (runtime settlement), §8 (framing), §9 (the `sql` adapter);
  [Plan 005 — Poller + SQL adapter](../plans/005-poller-sql-adapter.md); `docs/HANDOVER.md` §1/§4/§5.
- **Related:** [ADR 0002 — Adapter SPI](0002-adapter-spi.md) (`PollingSource`, `Delivery`,
  `NativeReliability`, the shared Poller this ADR finally implements), [ADR 0006 — Resilience &
  flow-control](0006-resilience-flow-control.md) + [ADR 0008](0008-resilience-flow-control-api.md) (the
  credit gate this ADR extends to the pull side), [ADR 0009 — Resilience hardening](0009-resilience-hardening.md)
  (D5 byte-cap resolved here; the `safe*` recovery pattern D4-below extends), [ADR 0001 — Message payload
  typing](0001-message-payload-typing.md) (runtime-owned decode; adapter does envelope framing only),
  [ADR 0003 — Multi-module layout](0003-multi-module-repository-layout.md) (`sql` is in the **core**
  module — `database/sql` is stdlib, the driver is caller-injected).

## Context

Plans 001–004 built the streaming runtime, reliability, and flow-control layer — but the runtime today
is **streaming-only**: `Run` calls `src.Stream(ctx, rawCh)`, and the credit gate acquires a credit
**per delivery already received** inside `ingest` (ADR 0008 D3: "the bounded buffer of size `n` *is* the
credit pool"). `NewConsumer` explicitly rejects a `PollingSource` (`consumer.go` `ErrUnsupportedSource`,
with a `TODO(Plan 004)`). Spec §7.4.1 makes the **pull model the primary flood defense**, and it
requires the opposite credit ordering from streaming: **credit-at-FETCH** — the poller must consult free
credit *before* it asks the source for rows, fetching `k = min(maxBatch, n − inFlight)` and acquiring
`k` credits **at claim**, so a huge backlog stays **durably in the source** (the SQL table) instead of
flooding an in-process buffer.

This ADR settles the design for **Plan 005**, which bundles two coupled deliverables (per the user's
scoping decision — the Poller is untestable end-to-end without a real pulled source, and the `sql`
adapter is untestable without the Poller):

1. The shared **Poller** driving any `PollingSource` with credit-at-fetch — pure core runtime, testable
   with a fake `PollingSource` + `clockwork.NewFakeClock()`.
2. The **`sql` adapter** (`adapter/database/sql`, core module) — generic `database/sql`, `SELECT … FOR
   UPDATE SKIP LOCKED` inbound + `INSERT` outbound, at-least-once, **v1 = the full spec matrix: two
   strategies (lease/claim default + lock/`FOR UPDATE`) × two dialects (PostgreSQL + MySQL)**.

Two backlog fold-ins become reachable now that the first *wire* adapter lands and are cleared here:
**#4** dispatch-boundary panic recovery (an adapter `Decode`/`Send`/`Ack`/`Nack` panic currently crashes
the process) and **#5-D5** the untrusted-payload byte cap's role at a real wire source.

As with ADR 0007/0008/0009, this is one ADR with per-decision sections; each fleshes out in the same
commit that implements it. **The design below is to be hardened by two independent adversarial Opus
audits** (a concurrency-correctness pass — credit-at-fetch over-pull / lease-expiry the stress target —
and an API/policy pass); their material findings will be folded into each decision before SDD.

## Decision

### D1 — The Poller: a first-class pull path in `Run` with credit-at-FETCH

**`Run` branches on the resolved source kind.** `NewConsumer` resolves the source once: a value
implementing `StreamingSource` takes today's streaming path (acquire-in-`ingest`, unchanged); a value
implementing `PollingSource` (and not `StreamingSource`) takes the new **poll loop**. Both feed the
**same** worker pool, drain machinery, attempt-tracker sweep, and breaker-dispatch gate — only the
*producer* stage differs. Precedence when a value implements **both**: `StreamingSource` wins (matches
the existing resolution order and prefers the lower-latency event-driven path); no shipped adapter
implements both (`pgx` deliberately exposes them as *separate constructor views* — spec §9 NF-7), so
this precedence is a documented tie-break, not a hot path. `consumer` gains `streamSrc StreamingSource`
and `pollSrc PollingSource` (exactly one non-nil).

**Credit-at-fetch via the existing gate primitives — "acquire ≥1, top-up, fetch, release surplus".**
The load-bearing invariant (spec §7.4.1) is *claimed-but-unsettled ≤ `maxInFlight`*, counted at **fetch**
so buffered-but-unprocessed rows are already counted and the poller can never over-pull past `n`. The
poll loop realizes this **reusing the current `creditGate` unchanged** (no new field, no atomic
counter):

```
pollLoop(ctx, settleCtx, gate, out):
  errN := 0
  for {
    // 1. Acquire at least one credit. This BLOCKS on the gate channel until a
    //    worker's release frees a slot — the natural "wait for capacity" primitive,
    //    missed-wakeup-free (the send unblocks exactly when a release receives). At
    //    zero free credit the poller does NOT poll (spec §7.4.1: "does not poll at
    //    zero credit").
    if gate.acquire(ctx) != nil { return }          // ctx done → shutdown
    held := 1
    // 2. Top up: grab additional FREE credits, non-blocking, up to maxBatch.
    for held < maxBatch && gate.tryAcquire() { held++ }
    // 3. Fetch up to `held` rows. Poll's contract (SPI godoc, below): returns at
    //    most `held` deliveries, NONE alongside a non-nil error, and owns rollback
    //    of any partial work on the error/cancel path.
    rows, err := pollSrc.Poll(ctx, held)
    if err != nil {
      c.logger.Error("msgin: poll failed", "err", err)  // D2: the promised loud log
      releaseN(gate, held)                               // release ALL held credits
      errN++
      if !sleepCtx(ctx, clock, pollErrorBackoff(errN)) { return }
      continue
    }
    // 3a. DEFENSIVE clamp (HIGH audit): a buggy dialect returning len(rows) > held
    //     would corrupt the gate. Never wrap more rows than credits held: excess
    //     rows are dropped (Nack, unwrapped — they hold no credit) and logged.
    if len(rows) > held { rows = clampExcess(ctx, settleCtx, rows, held) }  // ERROR-logs the violation
    errN = 0
    // 4. Release the surplus credits we acquired but no row filled (held ≥ len(rows)
    //    now guaranteed, so held-len(rows) ≥ 0). releaseN panics on a negative n.
    releaseN(gate, held-len(rows))
    // 5. Hand each row to a worker, release-first-wrapped (manage). One acquired
    //    credit rides with each row and is released by its terminal settle.
    for i, d := range rows {
      md := manage(d, sync.OnceFunc(gate.release))
      select {
      case out <- md:
      case <-ctx.Done():
        c.finish(md.Nack(settleCtx, true, 0)) // releases THIS row's (rows[i]) credit
        releaseN(gate, len(rows)-i-1)          // EXACTLY the un-handed rows[i+1:]; never rows[i]
        return
      }
    }
    // 6. Pace. Non-empty → loop immediately (drain the backlog while credit + rows
    //    last; acquire in step 1 backpressures). Empty → idle-wait pollInterval.
    if len(rows) == 0 { if !sleepCtx(ctx, clock, pollInterval) { return } }
  }
```

Key correctness properties (each a Plan 005 test target):

- **Never over-pulls — enforced, not assumed (HIGH audit).** `Poll` is asked for at most `held` rows;
  the loop **defensively clamps** `len(rows) > held` (a dialect bug) rather than trusting the SPI
  contract, so at most `held ≤ maxInFlight` new rows enter flight, each carrying a credit. A backlog of a
  million rows yields at most `n` in flight; the rest wait in the table. The `Poll(max)` ≤ `max`
  guarantee is **also documented on the SPI godoc** (below) so the contract is stated at both ends.
- **The poll goroutine is the *only* acquirer on the pull path** (workers only *release*, via the
  release-first `manage` wrapper). So a `tryAcquire` in step 2, and the `Nack`/`Ack` releases, never
  race a second acquirer — the accounting is single-writer-acquire / many-writer-release, which the
  channel semaphore already handles.
- **Surplus is always released, with an EXACT count (CRITICAL audit).** Step 4 releases `held−len(rows)`
  (≥ 0 after the clamp; `releaseN` **panics on a negative n** as a bug-tripwire); the error path
  releases all `held`; and the ctx-done handoff on row `i` releases **exactly** the un-handed
  `rows[i+1:]` = `len(rows)−i−1` (the wrapped `md.Nack` releases `rows[i]`'s own credit; `rows[:i]` rode
  off with earlier hand-offs). An over-count here is catastrophic: `releaseN` is a **non-cancellable**
  `<-tokens` receive and the poll goroutine is the sole sender, so one receive too many blocks it
  **before `close(workerCh)`** → workers never finish `range` → `wg.Wait` never returns → `Run` hangs
  past the shutdown deadline (violates C1). The exact count + the negative-n panic + a **forced test
  with a deliberately stalled worker** (this ctx-done arm is near-unreachable — `workerCh` is buffered to
  `maxInFlight` — so it must be exercised deliberately, not left to the coverage gate) close this.
- **Conservative during the DB round-trip.** Between step 1/2 and step 4 the loop holds `held` credits
  while `Poll` runs, momentarily counting credits with no message. This under-utilizes (never
  *over*-utilizes) capacity — the safe direction for a flood defense — and self-corrects on the next
  line. Documented, not a leak.
- **Shutdown is the shared machinery.** The poll loop replaces the `Stream`+`ingest` producer
  goroutines; like `ingest` it **owns closing `out` (`workerCh`)** on exit, so the worker pool drains
  and joins exactly as today (goleak-clean). A held-but-unhanded delivery at ctx-cancel is `Nack`ed
  (requeue) on `settleCtx`; surplus/held credits are released (exact count, above) so the gate cannot
  wedge the drain.

**`PollingSource.Poll` contract (SPI godoc, hardened per the HIGH audit).** `spi.go`'s `Poll(ctx, max)
([]Delivery, error)` godoc gains three MUST clauses, and the runtime enforces the first defensively:
(1) **return at most `max` deliveries**; (2) **return no deliveries alongside a non-nil error** — a
partial `(rows, err)` is forbidden because the loop discards `rows` on error, which for the lock strategy
would leak the tx/connection carried in a discarded `Delivery`; (3) **own cleanup (rollback) of any
partial/claimed work on the error or ctx-cancel path**. The `sql` `Poll` upholds (2)/(3) by rolling back
its claim tx on any error before returning `(nil, err)`.

**Poll pacing options** (new `ConsumerOption`s, mirroring the established `set`-flag pattern so an
explicit invalid value is a typed construction error, not a silent default — C2 from ADR 0008):

- `WithPollInterval(d)` — the idle wait after an **empty** poll. Unset → `defaultPollInterval` (1s); set
  and `d ≤ 0` → **`ErrInvalidPollInterval`**. (A zero interval would busy-poll the DB.)
- `WithPollMaxBatch(n)` — the `held`/`Poll` cap per iteration. Unset → `defaultPollMaxBatch` (100); set
  and `n < 1` → **`ErrInvalidPollMaxBatch`**.
- **Error backoff is built-in, not an option** (YAGNI): consecutive `Poll` errors back off
  `min(maxPollErrorBackoff, pollInterval × 2^(errN−1))` reusing the existing stateless
  `ExponentialBackoff{Initial: pollInterval, Max: maxPollErrorBackoff, Mult: 2}` (attempt-indexed, O(1),
  clockwork-testable), reset to zero on the first successful poll. `maxPollErrorBackoff` is a const (30s).

**Pacing semantics — "drain fast, idle slow".** After a non-empty poll the loop immediately re-enters
step 1 (whose blocking `acquire` is the only pacing under load — it backpressures to handler capacity),
so a backlog drains as fast as credit frees; `pollInterval` applies **only** when the source was empty.
This is the efficient default; a fixed-delay-after-every-poll mode is deferred (YAGNI) — no adapter needs
it in v1.

**Poison-recycle caveat (MEDIUM audit).** The credit gate bounds *concurrency*, not *rate*. A small set
of poison rows with `RetryPolicy.Backoff == nil` (immediate redelivery) is `Nack`ed with `visible_after
= now()` → immediately claimable → re-polled at once (a non-empty poll never idles), a **tight DB-poll
loop** for as long as they keep failing (forever under `MaxAttempts == 0`). This is inherent to
"drain fast", not a bug, but the `sql` package doc **recommends a non-zero `RetryPolicy.Backoff` with
`sql`** so a failing row's `visible_after` pushes it into the future and the poll idles — the opinionated
guidance that keeps the default safe (per the CLAUDE.md sensible-defaults principle). A structural
minimum-inter-poll pace on all-immediately-Nacked polls is noted as a future option, not taken in v1.

### D2 — `sql` adapter: schema ownership, reference DDL, `Ready` fail-fast, and `ErrSchemaNotReady`

**The caller provisions the schema; msgin never runs DDL on the production path.** A library that
silently `CREATE TABLE`s on startup hides privilege, migration-ordering, and multi-tenant concerns and
is an anti-pattern. Instead the adapter:

- **exports a per-dialect reference DDL builder** — `PostgresDDL(table string) string` /
  `MySQLDDL(table string) string` — returning the exact `CREATE TABLE` (+ index) statement for a
  validated table name, for callers to fold into their own migration tool;
- **provides an OPTIONAL idempotent** `EnsureSchema(ctx) error` on the source/outbound (runs
  `CREATE TABLE IF NOT EXISTS` via the dialect) that dev/test/opt-in callers may call — never called
  implicitly;
- **detects a not-initialized table with a PORTABLE probe, not driver-error sniffing (HIGH audit).** The
  original design classified via SQLSTATE `42P01` / errno `1146`, but Go's `database/sql` exposes no
  portable error-code API: pgx exposes `SQLState()` (a method), but `lib/pq` exposes `Code` as a *field*
  and `go-sql-driver/mysql` exposes `Number` as a *field* — reading errno `1146` requires
  `errors.As(&*mysql.MySQLError)`, which would **import the MySQL driver into non-test production code**,
  breaching the dependency policy (ADR 0003) and pushing a MySQL driver into *every* consumer's binary.
  Instead the `Dialect` exposes `SchemaExists(ctx, q, table) (bool, error)` — a portable
  `information_schema.tables` existence query (each dialect owns its exact form: Postgres
  `table_schema = current_schema()`, MySQL `table_schema = DATABASE()`) that imports **no** driver.
- **fails fast at boot via `Ready(ctx) error`** on the source/outbound: it runs `SchemaExists` and returns
  **`ErrSchemaNotReady`** (naming the table) when the table is absent, so a forgotten migration fails the
  deploy **immediately** — instead of an ostensibly-healthy consumer sitting in a silent 30s poll-error
  loop forever (MEDIUM audit: `ErrSchemaNotReady` is a *readiness* error, so it must surface at
  readiness, not only per-poll). At runtime, `Poll`/`Send` still wrap a query failure as
  `ErrSchemaNotReady` **iff a follow-up `SchemaExists` returns false** (so a table dropped mid-run is
  still diagnosed clearly, portably, no driver import); otherwise the raw error propagates through the
  poll-loop error backoff (with the ERROR log D1 now emits). `ErrSchemaNotReady` is exported (callers
  `errors.Is` it), `fmt.Errorf("%w: table %q not initialized; run EnsureSchema or apply the DDL",
  ErrSchemaNotReady, table)`.

**Package-name ergonomics — keep `sql`, document the alias (the user's call, MEDIUM audit).** The package
lives at `adapter/database/sql` (spec/ADR 0003) and is named `sql`, which collides with stdlib
`database/sql` in every consumer file (each caller needs `*sql.DB` to pass in). Per the user's decision
we keep the name `sql` and **document the alias convention** — the package doc and every runnable example
import it aliased (e.g. `import msginsql "github.com/kartaladev/msgin/adapter/database/sql"`), so a
copy-paste starting point compiles. (Renaming to `sqladapter`/`msginsql` was considered and declined.)

### D3 — `sql` adapter: exported `Dialect` SPI owning **all** SQL; generic constructors with driver auto-detect + `WithDialect` opt-in

**No cross-dialect SQL string ever executes** (the user's hard constraint). The `Dialect` interface —
**exported as a public SPI** (the caller-facing extension point for a database *and its derivatives /
quirks*: CockroachDB / YugabyteDB on the Postgres wire, MariaDB on the MySQL wire, TimescaleDB, …) —
owns the *complete* statement **and orchestration** for every operation, not merely placeholder style,
because Postgres and MySQL diverge structurally, not just lexically:

| Operation | PostgreSQL | MySQL |
| --- | --- | --- |
| claim (lease) | one-shot `UPDATE … FROM (SELECT … FOR UPDATE SKIP LOCKED LIMIT $1) … RETURNING` | **no `RETURNING`** → tx: `SELECT … FOR UPDATE SKIP LOCKED LIMIT ?` then `UPDATE … WHERE id IN (…)`; row data taken from the `SELECT` |
| time | `now()`, `now() + $n * interval '1 microsecond'` | `UTC_TIMESTAMP(6)`, `DATE_ADD(UTC_TIMESTAMP(6), INTERVAL ? MICROSECOND)` |
| insert id | `RETURNING id` | `LAST_INSERT_ID()` |
| autoinc/type | `BIGSERIAL`, `TIMESTAMPTZ`, `BYTEA`, `JSONB` | `BIGINT AUTO_INCREMENT`, `DATETIME(6)`, `LONGBLOB`, `JSON` |
| schema probe | `information_schema.tables … table_schema = current_schema()` | `… table_schema = DATABASE()` |

The **exported** `Dialect` therefore exposes **methods** (taking a `Querier` = the `*sql.DB`/`*sql.Tx`
subset: `ExecContext`/`QueryContext`/`QueryRowContext`), each fully owning its SQL and any
multi-statement tx orchestration. Because it is public, `ClaimedRow`/`LockedRow` (and any other value
types it returns) are exported too, and the SPI is a **pre-1.0 (v0) contract** that may still evolve:

```go
// Dialect is the exported SPI a caller supplies (via WithDialect) to teach the
// sql adapter a database's exact SQL — the extension point for wire-compatible
// derivatives and per-engine quirks. The two built-ins are sql.PostgresDialect()
// and sql.MySQLDialect(). Every method fully owns its statement(s); no cross-dialect SQL runs.
type Dialect interface {
    Claim(ctx, q, table string, limit int, lockedBy string, leaseTTL time.Duration) ([]ClaimedRow, error)
    ClaimLock(ctx, q Querier, table, lockedBy string) (*LockedRow, error) // lock strategy: ≤1 row + its owned tx
    Ack(ctx, q, table string, id int64, lockedBy string, epoch int64) (applied bool, err error)
    Nack(ctx, q, table string, id int64, lockedBy string, epoch int64, delay time.Duration) (applied bool, err error)
    Insert(ctx, q, table, msgID string, headers, payload []byte, delay time.Duration) error
    EnsureSchema(ctx, q, table string) error
    DDL(table string) string
    SchemaExists(ctx, q Querier, table string) (bool, error) // portable information_schema probe; no driver import
}
```

- **All row-time logic uses the DB server clock, never the app clock.** `locked_at`, `visible_after`,
  and the lease-expiry cutoff are computed **in SQL** (`now()` / `UTC_TIMESTAMP(6)`). Mixing DB time and
  the injected `clockwork` clock for row comparisons would introduce app↔DB skew bugs; the app clock
  drives only **scheduling** (the Poller's `pollInterval`/error-backoff, tested in the core with a fake
  clock), while the DB clock drives all persisted timestamps (tested against real Postgres/MySQL via
  testcontainers). This clean split is a deliberate invariant.
- **Identifier safety (spec §9 audit M5).** The `table` name cannot be a bound parameter, so it is
  validated against `^[A-Za-z_][A-Za-z0-9_]*$` and dialect-quoted (`"t"` / `` `t` ``) by the dialect
  before interpolation — closing the only injection surface. An invalid identifier is a construction
  error (**`ErrInvalidTableName`**).
- **Generic constructors with driver auto-detect + a `WithDialect` opt-in (deliberately trading
  guaranteed-correctness for flexibility — the user's call).** A single pair of constructors —
  `NewPollingSource(db *sql.DB, table string, opts ...Option) (*Source, error)` (→ `msgin.PollingSource`
  + `msgin.NativeReliability`) and `NewOutboundAdapter(db, table, opts...) (*Outbound, error)`
  (→ `msgin.OutboundAdapter`) — the explicit, self-describing names (clarity over Go-brevity, the user's
  call) resolve the `Dialect` in this precedence:
  1. **`WithDialect(d Dialect)` given → use `d`** (manual opt-in; the *guaranteed* path and the escape
     hatch for a derivative's quirks — e.g. `WithDialect(sql.PostgresDialect())` or a caller's own
     `Dialect`);
  2. **else auto-detect from the driver** — `reflect.TypeOf(db.Driver()).PkgPath()` (with a `String()`
     fallback) substring-matched: `pq` / `pgx` / `postgres` → `sql.PostgresDialect()`; `mysql` /
     `mariadb` → `sql.MySQLDialect()`. **This imports no driver** — it reflects on the value returned by
     the stdlib `driver.Driver` accessor — so the "core imports no SQL driver" rule (ADR 0003) holds;
  3. **neither → `ErrDialectUndetected`** (a typed construction error naming the driver type, telling the
     caller to pass `WithDialect`).
  The built-in dialects are **exported** as `sql.PostgresDialect() Dialect` / `sql.MySQLDialect() Dialect`.
  **Explicit trade-off (the user's decision):** auto-detect is **heuristic/best-effort** — a
  Postgres-wire derivative with different `SKIP LOCKED` / `RETURNING` semantics (CockroachDB) is
  mis-detected as vanilla Postgres and may run subtly wrong SQL; `WithDialect` is the only *guaranteed*
  path. This is documented loudly on `NewPollingSource`/`NewOutboundAdapter` and in the package doc:
  **auto-detect is a convenience; `WithDialect` is correctness.** The prior "per-dialect constructors so a mismatch is
  impossible" design is **rejected in favor of this** (see Rejected alternatives) because the user
  prioritizes handling derivatives/quirks over the mismatch-proof rigidity.
- **Strategy stays an `Option` (`WithStrategy`), not a constructor**, now consistent with dialect
  selection also being option-driven (`WithDialect`) — one generic constructor pair configured by
  options, rather than a constructor-per-(dialect × strategy) combinatorial explosion.

### D4 — Lease/claim strategy (default): fenced settle, inline reaping, `delivery_count`

The **default** strategy. Schema (reference DDL; Postgres types shown):

```sql
CREATE TABLE <table> (
  id             BIGSERIAL     PRIMARY KEY,
  msg_id         VARCHAR(255)  NOT NULL,          -- msgin.id (idempotency/observability)
  headers        JSONB         NOT NULL,          -- envelope framing: Headers → JSON
  payload        BYTEA         NOT NULL,          -- the wire body ([]byte)
  locked_by      VARCHAR(255),                    -- lease owner (consumer instance id); NULL = unlocked
  locked_at      TIMESTAMPTZ,                     -- lease start (DB clock); NULL = unlocked
  visible_after  TIMESTAMPTZ   NOT NULL DEFAULT now(),  -- server-side Nack delay / initial delay
  delivery_count INTEGER       NOT NULL DEFAULT 0,      -- ++ on each claim → msgin.delivery-count
  lease_epoch    BIGINT        NOT NULL DEFAULT 0,      -- fence token, ++ on each claim
  created_at     TIMESTAMPTZ   NOT NULL DEFAULT now()
);
CREATE INDEX <table>_claim_idx ON <table> (visible_after) WHERE locked_at IS NULL;  -- pg partial; MySQL: (locked_at, visible_after)
```

There is **no explicit `status` column** (a deliberate refinement of spec §9's column list): readiness
is encoded by `locked_at` + `visible_after` + lease expiry — one source of truth, no status/lock drift.
Ack **deletes** the row.

- **Claim** (short tx, committed before fan-out): select claimable rows
  `WHERE visible_after <= now() AND (locked_at IS NULL OR locked_at <= now() − leaseTTL)` — note the
  second disjunct **is the reaper, inlined**: an expired lease is simply claimable again, so there is
  **no separate reaper goroutine** (a whole class of lifecycle/goleak complexity avoided; expired leases
  are reclaimed by the next credited poll, which always runs at ≤ `pollInterval` cadence). The claim
  `… FOR UPDATE SKIP LOCKED LIMIT k`, then `UPDATE … SET locked_by, locked_at = now(),
  lease_epoch = lease_epoch + 1, delivery_count = delivery_count + 1 … RETURNING` (Postgres) / two-step
  (MySQL). `delivery_count` (post-increment) populates the **`msgin.delivery-count`** header so the
  runtime's attempt count never relies on the ephemeral tracker (NF-2 — sql messages persist across
  process restarts).
- **Fenced settle** (separate short tx each; audit NF-8): `Ack` = `DELETE … WHERE id=? AND locked_by=?
  AND lease_epoch=?`; `Nack` = `UPDATE … SET locked_by=NULL, locked_at=NULL,
  visible_after = now() + delay WHERE id=? AND locked_by=? AND lease_epoch=?`. If the row was already
  re-leased (the handler outran `leaseTTL` and another worker reclaimed it), the fence predicate matches
  **zero rows** → `applied=false`. **A fenced-out settle returns a non-nil sentinel (`ErrStaleLease`),
  NOT `nil` (MEDIUM audit).** Returning `nil` would make the runtime's success path
  (`consumer.go` `ackErr == nil`) fire a **phantom `OnAck`** and `tracker.evict(id)` for a message the
  stale worker never actually settled — an observability lie, and a latent `MaxAttempts`-reset for any
  future native adapter lacking a `delivery-count` header. Returning `ErrStaleLease` routes through
  `finish` (logged), suppresses the phantom `OnAck`/evict, and is harmless (the row is owned by the
  worker that re-leased it, which will settle it). The `sql` `Source` also logs the stale settle at WARN.
  `requeue=false` behaves as `requeue=true` with `delay=0` (an at-least-once source cannot silently drop;
  the runtime only ever sends the pull path `requeue=true` anyway — the overflow-shed paths are
  streaming-only). **This collapse is godoc'd on the exported `Source`** (MEDIUM audit — a guarantee in
  an ADR is not part of the API contract a caller reads), dovetailing with deferred follow-up #6.
- **Lease-TTL invariant + safe default (NF-8; MEDIUM audit + CLAUDE.md sensible-defaults).** `WithLeaseTTL(d)`
  requires `d > 0` (**`ErrInvalidLeaseTTL`**); **unset → a safe default of 5m** (matching
  `defaultAttemptTTL`, comfortably above any plausible handler round-trip — NOT a tight 30s that would
  silently double-process a slow handler when `WithHandlerTimeout` is disabled by default). The godoc
  states the **full** invariant (audit sharpened it): `leaseTTL` must exceed **the worst-case redelivery
  round-trip — handler execution + settle/Ack latency + margin — not merely `WithHandlerTimeout`**, and
  notes the app-clock (`handlerTimeout`, `clockwork`) vs DB-clock (lease expiry) skew shifts the margin.
  Cross-object static enforcement is not feasible (the `Source` and `Consumer` are independent objects),
  so it is documented — mirroring the ADR 0009 D3 "documented, not statically enforced" residual.
- **`NativeReliability`:** `NativeRedelivery()=true` (the row persists and is re-leased after
  Nack/expiry), `NativeDeadLetter()=false` (the runtime DLQ policy applies — e.g. to another `sql`
  outbound as the DeadLetter sink).

### D5 — Lock/`FOR UPDATE` strategy (opt-in): tx-carried delivery, no reaper, pool-coupled

Selected via `WithStrategy(LockForUpdate)`. A single `SELECT … FOR UPDATE SKIP LOCKED LIMIT 1`
transaction is **carried in the `Delivery` and owned by the worker** from claim to settle: `Ack` =
`DELETE` + `tx.Commit`; **`Nack` = `UPDATE … SET locked cleared, visible_after = now()+delay` +
`tx.Commit` — it ALWAYS commits, never rolls back (CRITICAL 2 audit).** A process crash (only) auto-releases
via tx rollback — **no reaper, no fence token**. Same-schema as lease (it reuses the columns). Constraints
(spec §9 audit NF-9; the two HIGH audit findings + the second-round CRITICAL/HIGH findings folded in):

- **`Nack` always commits so `delivery_count` persists (CRITICAL 2 audit — the earlier "restart-durable"
  claim was FALSE and is retracted).** The plan increments `delivery_count` **inside the carried
  `FOR UPDATE` tx**, so it is durable **only if that tx commits**. The first draft had `Nack(delay=0) →
  tx.Rollback`, which reverts the `++` — and the **default** `RetryPolicy` (`Backoff == nil` → `delay=0`)
  hits exactly that path, so `attempts()` read 1 forever and `MaxAttempts` never advanced (**infinite
  poison retry, no DLQ**). Fix: **the plain lock strategy has no business writes in the tx, so `Nack`
  ALWAYS commits** — an `UPDATE` that clears the lock, sets `visible_after = now()+delay` (delay may be
  0), and thereby persists the `delivery_count++`. Committing releases the `FOR UPDATE` lock just as a
  rollback would, so nothing is lost. `MaxAttempts` is now durable across **normal** redeliveries; **only
  a genuine crash** (tx auto-rollback) reverts a single in-flight attempt — rare and documented, vastly
  better than reverting every retry. Populates the `msgin.delivery-count` header (spec §7.4.1). *(This
  "always commit on Nack" resolution is available to the plain lock strategy precisely because there are
  no business writes to undo; transactional consume — D9 — cannot use it and is deferred, below.)*
- **Detached claim-tx context so shutdown doesn't yank in-flight work (HIGH 3 audit).** The claim tx must
  NOT be begun on the poll/parent ctx: `database/sql` rolls a tx back the instant its begin-ctx cancels,
  so on graceful shutdown parent-cancel would auto-rollback **every in-flight lock tx before the drain
  deadline** — redelivering all in-flight messages and defeating the C1 detached-drain. Fix: `ClaimLock`
  begins its tx on **`context.WithoutCancel(ctx)`** (detached, peer of the runtime's `settleCtx`), so the
  tx lives until the worker settles (Ack/Nack on `settleCtx`) or the drain deadline cancels `settleCtx`
  and the deadline-Nack closes it. In-flight work then finishes within the deadline as C1 promises.
- **Same-pool DLQ stall — mitigated (HIGH audit).** A worker pins its claim `tx` (one conn) for the
  message lifetime; on a permanent failure the runtime's `divert` calls the DLQ/invalid **`sink.Send`
  BEFORE** `Ack` (NF-3), so a `sql` sink on the **same `*sql.DB`** needs an *additional* conn while all
  claim conns are pinned → the `INSERT` blocks → **total stall until the shutdown deadline** cancels
  `settleCtx`. Reordering settle to free the conn first is not available to the adapter (the runtime
  controls settle order). Mitigation, loudly documented on `WithStrategy(LockForUpdate)`: **a
  lock-strategy consumer whose DLQ/invalid sink is a `sql` adapter MUST give that sink a SEPARATE
  connection pool (a separate `*sql.DB`)**, or size one shared pool with headroom `poolSize ≥
  maxInFlight + divert-concurrency`. The runnable example uses a separate pool. Not statically enforceable
  (the `Source` cannot see the sink's pool), consistent with the other cross-object invariants.
- **Single-row, no batch** — `ClaimLock` returns ≤1 regardless of the requested `k` (each in-flight
  message pins one pooled connection). The poll loop's surplus-release (D1 step 4) frees the over-acquired
  credits; callers SHOULD pair it with `WithPollMaxBatch(1)` for efficiency (documented, not enforced).
  On any claim error `ClaimLock` **rolls back its tx before returning `(nil, err)`** (upholds the Poll
  no-partial-result / owns-cleanup contract — HIGH audit — so a discarded delivery never leaks a conn).
- **Pool-coupling invariant:** effective in-flight = `min(WithMaxInFlight, WithConcurrency,
  poolSize − headroom)` (with the DLQ headroom above); if it exceeds the DB pool the poller stalls on an
  exhausted pool. **Documented** with a strong godoc warning + an example setting `db.SetMaxOpenConns`,
  `WithPollMaxBatch(1)`, and a separate DLQ pool together. (`ErrInvalidLeaseTTL`/`WithLeaseTTL` are
  ignored under `LockForUpdate` — godoc'd.)

Lock strategy is the *stronger-crash-safety* option (crash → tx rollback, no lease-expiry window) at the
cost of a held connection per message + the pool-topology care above; lease/claim is the default because
it scales to batches without pinning connections.

### D6 — Dispatch-boundary panic recovery (fold-in #4): extend the `safe*` guard to the adapter SPI

Today only the **handler** (`safeHandle`), **hooks** (`safeFire`), and the **resilience governors**
(`safe*`, ADR 0009 D1) are `recover`-wrapped. The settlement path calls four **adapter-supplied** funcs
raw — `codec.Decode` (runtime-owned but caller-pluggable), `sink.Send`, `Delivery.Ack`, `Delivery.Nack`
— so a panic in a wire adapter (realistic now that `sql` marshals/unmarshals real bytes and runs real
DB I/O) unwinds the worker goroutine and **crashes the process**. This is the pre-existing
observation from the Plan 004 whole-branch review; it becomes reachable with the first wire adapter.

**Decision — wrap each adapter SPI call in a `recover` guard**, classifying the recovered panic so the
existing settlement switch handles it, never a crash (CLAUDE.md fault-isolation):

- `safeDecode(b) (T, error)` → a panic in `codec.Decode` maps to `ErrPayloadDecode` (wrapped) →
  **permanent** → invalid sink. A codec that panics on an input will panic again on redelivery, so
  treating it as poison (not a retry loop) is correct.
- `safeSend(ctx, sink, msg) error` → a panic in `sink.Send` maps to a synthetic error → the existing
  `divert` "sink failed → Nack + retry" path (the message is not lost).
- `safeAck(ctx, d) error` / `safeNack(ctx, d, requeue, delay) error` → a panic maps to a synthetic
  settle error → `finish` logs it (ERROR). The credit is **already released** before the panic (the
  `manage` wrapper releases *before* calling the underlying Ack/Nack), so a panicking settle never pins
  a credit; the worker's deferred `md.release` OnceFunc is the panic-safe net.

Each logs at **ERROR** (an adapter panic is a serious bug — louder than a hook's WARN), message id only,
never the payload — consistent with `governorPanic`. This is a small, mechanical, high-value hardening;
it does **not** add per-method dedup (an adapter panic is not the deterministic-per-message governor
case), but the poll-loop/error-backoff and the settlement switch already bound any repeat.

### D7 — The D5 byte-cap at a real wire source: runtime-owned, `sql`-documented (no forced default)

The handover flagged "the D5 byte-cap should get a sensible **adapter default**". On analysis the cap is
**runtime-owned** and already shipped (ADR 0009 D5, `WithMaxPayloadBytes`, applied in `consumer.decode`
before `codec.Decode`) — the adapter never sees `T` or runs the decode (ADR 0001), so it *cannot* set a
consumer option, and capping the payload **column** adapter-side would silently strand poison rows
(the adapter has no invalid-sink to divert to; that is runtime machinery). Therefore:

- **The byte cap stays a consumer concern** (`WithMaxPayloadBytes`). The `sql` adapter **documents it
  prominently** and its runnable examples set it; it does **not** force a default. A DB message table is
  populated by *trusted* producers (typically the paired `sql` outbound / the caller's own writes), so
  it is materially less exposed than an untrusted HTTP firehose — where a recommended/forced default
  matters most, and where it will be revisited (`adapter/http`, next plan). Per CLAUDE.md's
  sensible-defaults principle (the "no safe universal default → make it explicit + guide" branch), the
  package **exports a recommended constant** `RecommendedMaxPayloadBytes` (e.g. 1 MiB) that the runnable
  `Example` passes to `WithMaxPayloadBytes`, so the guidance is a copy-pasteable value, not just prose
  (LOW audit).
- The D6 `safeDecode` guard (above) is the substantive Plan-005 decode-boundary hardening; D7 is the
  documentation resolution that closes the fold-in honestly rather than bolting a cap onto the wrong
  layer.

### D8 — Transactional outbox: `WithSharedTransaction` (strict) / `WithOpportunisticSharedTransaction`

**Problem — the default `sql` outbound is a dual write.** `Outbound.Send` INSERTs on the pool `*sql.DB`
(its own auto-commit tx). A caller performing a business change **and** publishing an event ("debit
account, then emit `AccountDebited`") therefore commits two independent transactions; a crash between
them loses the message (or publishes without the state change). The **transactional outbox** pattern —
a must-have for reliable DB-backed messaging — closes this by making the message INSERT participate in
the **caller's business transaction**, so state-change + enqueue commit or roll back **atomically**. A
separate relay then forwards committed rows to the real broker; in msgin the `sql` inbound
`PollingSource` (D1/D4) **is** that relay, so this decision completes the outbox (write side) whose relay
side already ships in Plan 005.

**Decision — resolve the caller's tx from `context.Context` and INSERT on it.** The core `OutboundAdapter`
SPI is **unchanged** (`Send(ctx, msg)` already carries the context); the tx travels *in* that context and
the adapter borrows it:

```go
// TransactionResolver resolves the caller's active transaction from ctx as a Querier
// (a *sql.Tx satisfies Querier). (nil, nil) ⇒ no shared tx present in ctx.
type TransactionResolver func(ctx context.Context) (Querier, error)

// WithSharedTransaction — STRICT (the recommended, safe default): Send enlists the outbox
// INSERT in the caller's transaction (resolved from ctx); if no tx is present it returns
// ErrNoSharedTransaction — it NEVER silently dual-writes. A nil resolver is a construction
// error (ErrNilResolver).
func WithSharedTransaction(r TransactionResolver) Option

// WithOpportunisticSharedTransaction — the explicitly-named unsafe variant: uses the tx when
// present, else falls back to a standalone (auto-commit) INSERT and logs the fallback at WARN.
func WithOpportunisticSharedTransaction(r TransactionResolver) Option
```

**Two named options, not a `required bool` (HIGH audit — sensible-defaults + boolean-param anti-pattern).**
A `WithSharedTransaction(r, false)` call site is unreadable and `false` is the *dangerous* value; worse,
opportunistic-fallback logged at DEBUG silently reintroduces the exact dual-write the outbox exists to
prevent (DEBUG is off in production). So the safe mode is the **plainly-named default** and the unsafe
mode is **explicitly opted into by name**. `Send(ctx, msg)`:
1. `q, err := r(ctx)` — a resolver **error** is wrapped and returned (either mode).
2. `q != nil` → INSERT on `q` (the shared tx). msgin **borrows** it — it **never `Commit`s or `Rollback`s**
   the caller's tx; the caller owns the lifecycle.
3. `q == nil` (no tx in ctx): **strict** → **`ErrNoSharedTransaction`** (refuse the dual-write);
   **opportunistic** → fall back to the pool (`db`, auto-commit) **and log at WARN** ("no shared
   transaction in context; standalone insert — atomicity NOT achieved", id only). No resolver → today's
   standalone-`db` behavior.

**Nil-input validation (HIGH audit):** a nil `TransactionResolver` is a construction-time
**`ErrNilResolver`** from `NewOutboundAdapter` (never a deferred nil-func panic on first `Send`).

To carry the WARN log without a package global, the `sql` adapter gains an injected **`WithLogger(
*slog.Logger)`** option (default a discard logger — same pattern as the consumer; observability hooks,
not globals — CLAUDE.md).

**Do not use a shared-tx (strict) Outbound as a runtime DLQ/invalid sink (LOW audit).** The runtime's
`divert` calls `sink.Send(settleCtx, msg)` and `settleCtx` never carries a caller tx, so a strict
shared-tx Outbound as a `RetryPolicy.DeadLetter`/invalid sink would return `ErrNoSharedTransaction` on
every poison message → the divert treats it as "sink down → retry" → the message never dead-letters.
Godoc'd: a DLQ/invalid sink must be a plain (non-shared-tx) Outbound.

**Why resolve-from-context (not a `SendTx(ctx, tx, msg)` method).** A `SendTx` would change the
`OutboundAdapter` SPI and be **unreachable through the runtime** (the runtime's `divert`/DLQ path calls
the generic `Send(ctx, msg)`), whereas resolve-from-context works transparently everywhere `Send` is
called and keeps msgin **agnostic about how the caller stores its tx** (raw `database/sql`, a
unit-of-work/repository layer, `sqlx`, ent…) — the resolver is the only seam, and it adapts *their*
convention to msgin's `Querier` (which `Dialect.Insert` already accepts, so the implementation is a
one-line "use `q` instead of `db`").

**Commit gates visibility — atomicity for free.** Because the outbox row is INSERTed inside the caller's
tx, it is **invisible to the relay's `SELECT … FOR UPDATE SKIP LOCKED` until the caller commits**
(transaction isolation), and **never exists if they roll back**. No `visible_after` coordination is
needed; the relay only ever sees committed rows. End to end: `WithSharedTransaction` outbound (atomic
write) → `PollingSource` relay (forward to Kafka/NATS/HTTP) → at-least-once with `msgin.id` for
downstream idempotency.

**Documented invariant (unenforceable).** The resolved tx MUST be on the **same database** as the outbox
table — otherwise there is no shared transaction to be atomic with. A `*sql.Tx` does not expose its `DB`,
so msgin cannot verify this; it is a loud godoc warning, consistent with the other cross-object
invariants (lease-TTL, pool-coupling).

**Cross-database durable publishing — decision: outbox only (research-backed).** The shared tx needs the
outbox table co-located with the **business data**, NOT with the message's **destination** — the relay
(`PollingSource`) forwards a committed outbox row to any foreign DB/broker at-least-once, so "events go
to a different system" is a non-issue (point the sql outbound at the *business* DB; relay onward). Atomic
publishing to a store that genuinely *cannot* share the business tx is impossible without **2PC/XA**
(unsupported by most brokers, blocking, coordinator-SPOF) — **rejected** (violates the
minimal-deps/pure-Go/no-cgo ethos). msgin therefore ships **the outbox as its sole durable-publishing
mechanism**; the rare "cannot co-locate any table" case is addressed by **documented guidance only** —
co-locate, else use external CDC (Debezium) or a listen-to-yourself architecture (publish-first; eventual
DB consistency). A future **CDC / Postgres logical-replication inbound adapter** (dovetailing with the
pgx `LISTEN`/`NOTIFY` work, Plan 009) is a **backlog** item, not built here. Note the asymmetry with D10:
cross-DB *consuming* can retroactively dedup a duplicate (D10), but cross-DB *publishing* cannot
reconstruct a *lost* event — durable publishing is fundamentally harder, so there is no symmetric cross-DB
outbox. (Sources: Confluent dual-write, microservices.io transactional-outbox, Red Hat distributed-tx.)

**Scope — outbound (write) side.** The symmetric *inbound* variants are the durable-consume strategies:
**strategy 2 (idempotent / dedup inbox, D10) ships in Plan 005**; **strategy 1 (transactional consume,
D9) is DEFERRED to Plan 006** (see D9).

### D9 — Durable consume, strategy 1 of 2: transactional consume — DEFERRED to Plan 006 (audit-forced redesign)

> **STATUS: DEFERRED to Plan 006.** The second-round adversarial audit proved the design below is **not
> implementable as drafted** (see "Why deferred"). Transactional consume is a **flagship, commonly-used
> feature of DB-backed queues** (River, Oban, graphile-worker, Solid Queue — process-and-complete in one
> `SKIP LOCKED` transaction), so it is **kept on the roadmap**, not dropped (research-backed) — but it
> needs a proper tx-model redesign that does not belong in Plan 005. The `Delivery.BindContext` core-SPI
> hook it required is **deferred with it** (removed from Plan 005 Task 3). Strategy 2 (D10) — the
> different-DB idempotent consumer — ships in Plan 005 and covers the cross-DB case now.

**Why deferred (the audit's CRITICAL findings).** Transactional consume runs the handler *inside* the
lock-strategy's single carried tx so handler-writes + the settling `DELETE` commit atomically. But that
one transaction is over-loaded — it cannot simultaneously satisfy three conflicting needs:
1. **CRITICAL 1 — delayed Nack commits failed writes.** The runtime's transient `Nack` uses `delay>0 →
   UPDATE visible_after + tx.Commit`. With a non-nil `Backoff` (which D1 *recommends* to avoid
   poison-polling), a **failed** handler's business writes get **committed** → redelivery re-applies them
   → exactly-once broken. Business writes want *rollback*; delayed redelivery needs *commit*; same tx.
2. **CRITICAL 2 — `delivery_count` reverts on rollback** (the same root as D5's fix, but D9 *must* roll
   back to undo business writes, so it cannot use D5's "always commit on Nack" escape).
3. **HIGH — `Delivery.BindContext` fail-open** runs the handler *without* its tx on an injector panic →
   silent dual-write (transactional consume needs fail-**closed**, opposite of a generic hook).

**Intended Plan-006 redesign (research-backed — the River/Oban model).** Separate the **business tx**
from the **settlement/retry metadata**: on handler success, commit business-writes + `DELETE` in the one
tx (exactly-once); on failure, **roll back** (immediate re-eligibility, undoes business writes) and
manage `delivery_count`/backoff in a **separate** short statement — *not* by committing the poisoned tx.
Plus a settle-scoped context on the Poll SPI and a **fail-closed** bind for the transactional path. This
is a known, proven design; it just needs its own increment + audit round.

_The original (deferred) design follows for Plan-006 reference; it is NOT part of Plan 005._

**Durable (exactly-once) consume — two opt-in strategies, sensible default (CLAUDE.md).** A production
consumer wants a message's effect applied **exactly once** despite at-least-once redelivery. Which
guarantee is achievable depends on whether the business data shares a database with the message source,
so msgin offers **two opt-in strategies** and a safe zero-config default:

| | Strategy 1 — transactional consume (D9, **DEFERRED → Plan 006**) | Strategy 2 — idempotent consume / dedup inbox (D10, **ships in 005**) |
| --- | --- | --- |
| When | business data in the **same DB** as the source | business data in a **different DB** (can't share a tx) |
| Guarantee | **exactly-once *delivery*** (handler writes + `Ack DELETE` in one local tx) | **exactly-once *effect*** (dedup by `msgin.id`; delivery stays at-least-once) |
| How | transparent: adapter injects its tx into the handler ctx | handler-cooperative: handler calls a dedup helper in its business tx |
| Strategy | lock/`FOR UPDATE` only | any strategy / any source |

**Sensible default:** neither is on by default — the zero-config baseline is plain at-least-once with the
caller responsible for idempotency (documented). Each strategy is an explicit opt-in with sensible
internal defaults (D10's dedup table name, dialect auto-detect). Guidance in the package doc: same-DB →
prefer strategy 1 (stronger, transparent); different-DB → strategy 2.

**Problem — the symmetric read side.** D8 makes *publishing* atomic with the caller's business tx. The
mirror is making *consuming* atomic: run the handler's business writes and the message's **settlement**
in one local DB transaction, so a crash cannot leave "processed but not acked" and a redelivery cannot
re-apply committed work. Because msgin's `sql` source is a table in (typically) the **same database** as
the business data, this needs **no XA / distributed transaction** — a single local tx suffices, giving
**exactly-once local effect** (the DB-as-queue property of River / Oban / graphile-worker). It also
**subsumes a dedup/inbox table** for the same-DB case: the settling `DELETE` committed with the business
writes *is* the idempotency.

**Fit — the lock/`FOR UPDATE` strategy only (D5).** There the `Delivery` already owns a tx spanning
claim→settle, so the handler can write into that very tx and `Ack` (= `DELETE` + `tx.Commit`) commits
business-writes + settlement atomically. The **lease strategy cannot** provide it (D4 commits the claim
tx before dispatch and settles in a separate tx — the handler never runs inside a settle-able tx).
`WithTransactionalConsume` on a non-lock source is a **construction error**
(`ErrTransactionalConsumeRequiresLock`).

**The one core change — an additive, opt-in `Delivery.BindContext` (non-breaking).** The sql source holds
the tx, but the *runtime* invokes the handler, so the source needs a way to contribute request-scoped
context to that call. `spi.go`'s `Delivery` gains an **optional** field:

```go
type Delivery struct {
    Msg  Message[any]
    Ack  func(ctx context.Context) error
    Nack func(ctx context.Context, requeue bool, delay time.Duration) error
    // BindContext, if non-nil, derives the context under which the runtime invokes
    // the handler for THIS delivery — letting an adapter contribute request-scoped
    // values (e.g. a transaction the Ack will commit). nil = identity (unchanged).
    BindContext func(ctx context.Context) context.Context
}
```

The runtime's `dispatch` applies it **after** `handlerContext` (so `WithHandlerTimeout` still bounds the
handler) and **before** `safeHandle`: `hctx = c.safeBindContext(d, hctx)`. It is wrapped in a `recover`
guard (`safeBindContext`, mirroring D6) that fails open to the *unbound* context on a panic (ERROR-logged)
— never a crash. Adding a zero-value-`nil` field is source-compatible: existing adapters (memory) that
use struct literals `Delivery{Msg, Ack, Nack}` leave it nil and are unaffected. This is an additive
public-core-SPI change (SemVer minor); it is general beyond this feature (an adapter could inject a trace
span or a message-scoped value the same way).

**The sql option — symmetric to the outbox.**
```go
// TransactionInjector places the message-lifetime tx into ctx under the CALLER's
// convention, so the handler's existing repository/uow code (which reads a tx from
// ctx) runs on it. The mirror of the outbox TransactionResolver.
type TransactionInjector func(ctx context.Context, tx Querier) context.Context
func WithTransactionalConsume(inject TransactionInjector) Option // requires WithStrategy(LockForUpdate)
```
In lock mode the source sets each `Delivery.BindContext = func(ctx) context.Context { return inject(ctx,
tx) }` (the message's claim tx as a `Querier`). The handler resolves the tx from ctx (its own key
convention), does business writes on it; `Ack` commits both + the `DELETE`; a handler error / `Nack`
rolls back → the row's lock releases → redelivered. **The same context-key convention powers both
directions**: for the outbox the caller *writes* the tx and gives a `resolver` that reads it; for the
inbox msgin *writes* its tx (via the `injector`) under that same key and the handler reads it the same
way — so a caller's "get tx from ctx" repository layer works transparently for both. Concurrency-safe: one
worker owns the tx for the message's lifetime (one conn, D5), never shared across goroutines.

**Exactly-once, precisely.** The settling `DELETE` is *inside* the committed tx, so: commit → row gone,
never redelivered; crash before commit → tx rolls back, business writes undone, redelivered and re-run
cleanly; commit fails → same as crash. No dedup table needed for the same-DB case. The **same-database
invariant** (D8) applies and is documented (unenforceable): the injected tx and the outbox table are the
same DB by construction here (it *is* the source's tx), so this direction is safe by construction.

### D10 — Durable consume, strategy 2 of 2: idempotent consume / dedup inbox (different-database case)

**Problem.** When the business data lives in a **different database** than the message source, strategy 1
is impossible — there is no shared transaction. The achievable guarantee is then **exactly-once
*effect*** via the **idempotent-consumer (inbox) pattern**: record each processed `msgin.id` in a dedup
table **in the business DB, inside the handler's business tx**, and skip a message already recorded. The
dedup record commits atomically *with the business writes*, so a crash between commit and `Ack` (→
redelivery) finds the id already present and skips — the business effect applies exactly once even though
delivery is at-least-once.

**Why it is handler-cooperative, not a transparent consumer option.** The dedup record MUST commit in the
**same transaction as the business writes** to be correct — and that tx is owned and created by the
handler (against the business DB msgin does not own). msgin cannot transparently insert a check-and-record
into the middle of a tx the handler controls (the ordering — check *before* work, record *with* work —
lives inside the handler). So strategy 2 is delivered as a **msgin-provided, dialect-aware component the
handler invokes**, not a runtime interceptor. (Recording in a *separate* tx would reopen the very
crash-window it exists to close.)

**Decision — an opt-in `InboxDeduper` with sensible defaults.**
```go
// InboxDeduper makes an at-least-once consumer idempotent when business data is in a
// DIFFERENT database than the message source. Construct against the BUSINESS *sql.DB.
type InboxDeduper struct{ /* db, dialect, table */ }

// InboxOption is a SEGREGATED option type (NOT the source's Option — HIGH audit: a shared
// type would either collide on WithDialect or silently accept meaningless WithStrategy/WithLeaseTTL).
func NewInboxDeduper(businessDB *sql.DB, opts ...InboxOption) (*InboxDeduper, error)
//   opts: WithInboxDialect(InboxDialect) (else auto-detect from businessDB — D3),
//         WithInboxTable(string) (default "msgin_inbox" — sensible default)

// MarkProcessed atomically records msgID within the caller's business tx. It takes a concrete
// *sql.Tx (NOT Querier) BY DESIGN (HIGH audit): accepting Querier would let a caller pass the
// pool *sql.DB, which auto-commits the dedup row out-of-band — then if the business tx rolls
// back, redelivery sees already=true and SKIPS forever → SILENT PERMANENT MESSAGE LOSS. The
// concrete tx type makes that misuse a compile error. Returns already=true if msgID was already
// recorded (a duplicate) → the caller SKIPS the business effect and returns nil (message is Acked).
func (d *InboxDeduper) MarkProcessed(ctx context.Context, tx *sql.Tx, msgID string) (already bool, err error)

func (d *InboxDeduper) EnsureSchema(ctx context.Context) error // caller provisions; opt-in helper (D2)
func (d *InboxDeduper) Ready(ctx context.Context) error        // SchemaExists fail-fast (D2)
func (d *InboxDeduper) Purge(ctx context.Context, olderThan time.Duration) (int64, error) // retention
func InboxDDL(d InboxDialect, table string) (string, error)    // reference DDL
```

- **Segregated `InboxDialect` (HIGH/MEDIUM audit — ISP):** the deduper needs only
  `InsertInboxIfAbsent` + `SchemaExists` + `PurgeInbox` + inbox `DDL`, so it takes a **narrow
  `InboxDialect`**, NOT the fat source `Dialect` (~10 methods) — a derivative author fixing one `Claim`
  quirk must not be forced to implement inbox-dedup SQL. The built-ins (`PostgresDialect()`/`MySQLDialect()`)
  satisfy both interfaces; the option type is `InboxOption` and the dialect option is `WithInboxDialect`.
- **Nil-input validation (HIGH audit):** `NewInboxDeduper(nil, …)` → construction-time **`ErrNilAdapter`**
  (never a deferred nil-DB panic on first `MarkProcessed`).
- **`MarkProcessed`** is one dialect-owned statement per engine, but the **verdict is derived
  precisely, not from `rowsAffected`** (MEDIUM audit): Postgres `INSERT … ON CONFLICT (msg_id) DO
  NOTHING RETURNING` distinguishes insert-vs-conflict exactly; **MySQL `INSERT IGNORE` must NOT trust
  `rowsAffected==0`** (it demotes truncation/range/other errors to warnings → false `already=true` →
  dropped message) — the MySQL `InsertInboxIfAbsent` inserts then **verifies with a `SELECT`** (or checks
  warning count) so `already` means *genuinely a duplicate*, nothing else. The two engines' dedup
  guarantees are thereby equivalent.
- **Retention** is a manual `Purge(ctx, olderThan)` (`DELETE … WHERE processed_at < now() − olderThan`),
  **no background goroutine** (D4's no-reaper precedent — caller schedules it). **Safety (MEDIUM audit):
  `Purge` is only safe with a finite `MaxAttempts`** — `olderThan` MUST exceed the source's max
  redelivery window, which is *unbounded* under the default `MaxAttempts==0` (retry forever), so no
  finite retention is safe there. Godoc'd loudly (purging a still-redeliverable id → a late redelivery
  reads `already=false` → double-process).
- **Opt-in + sensible defaults (CLAUDE.md):** using the deduper is opt-in; its table name defaults to
  `msgin_inbox`, its dialect auto-detects from the business DB (D3), and `EnsureSchema`/`Ready` mirror the
  source's schema story (D2). The consumer stays plain at-least-once unless the handler adopts the deduper.
- **Composability:** strategy 2 works with **any** source strategy (lease or lock) and any at-least-once
  source; it is the fallback when strategy 1's same-DB precondition does not hold.

**The two strategies are mutually exclusive per consumer** (a given consumer is either same-DB
transactional or different-DB idempotent), but both may appear in one application for different flows.

## Consequences

**Positive**
- The Poller finally makes the **pull model** (spec's primary flood defense) real: a million-row `sql`
  backlog yields ≤ `maxInFlight` in flight, the rest durable in the table — with credit-at-fetch reusing
  the existing gate primitives (no gate redesign) and the entire streaming path, worker pool, drain, and
  breaker gate unchanged.
- `sql` ships the **full spec matrix** (2 strategies × 2 dialects) with a clean dialect seam where no
  cross-database SQL executes, DB-clock-only row logic (no app/DB skew), fenced settlement, and inline
  reaping (no reaper goroutine — less lifecycle surface, trivially goleak-clean).
- Schema stays the caller's (idiomatic for a library); a not-initialized table fails with a clear,
  `errors.Is`-able `ErrSchemaNotReady` naming the table, not a raw driver error.
- The **exported `Dialect` SPI + generic constructors** make the adapter open for extension (a
  wire-compatible derivative or a quirky engine = supply a `Dialect`, no fork), and driver auto-detect
  keeps the common Postgres/MySQL path zero-config — the flexibility the user prioritized.
- D6 closes the last raw (unrecovered) SPI call sites, so a buggy wire adapter degrades to a
  logged transient/invalid settlement instead of a process crash — extending the fault-isolation
  guarantee to the adapter boundary just as the first wire adapter arrives.
- **D8 completes the transactional outbox** — `WithSharedTransaction` (strict, the safe default) /
  `WithOpportunisticSharedTransaction` makes state-change + publish atomic with **zero core-SPI change**
  (the tx rides in `ctx`, the relay half already ships in Plan 005), and "commit gates visibility" gives
  the atomicity for free. Strict-by-default + WARN-on-fallback refuses a silent dual-write.
- **D10 ships the different-DB durable-consume strategy now.** The `InboxDeduper` (idempotent consume) →
  exactly-once *effect* for the different-DB case (a narrow-`InboxDialect`-aware helper the handler calls
  in its business tx, `*sql.Tx`-typed to bar the pool-misuse silent-loss, MySQL verdict verified). D9
  (transactional consume, same-DB exactly-once *delivery*) is **deferred to Plan 006** for the
  audit-forced tx-model redesign — kept on the roadmap (a flagship DB-queue feature) but not rushed.
  Default stays plain at-least-once (documented).
- **Two rounds of adversarial audits hardened the design before code:** round 1 — the shutdown-hang
  (exact credit-release count + negative-`n` panic tripwire + forced stalled-worker test), the unenforced
  `Poll(max)` contract (defensive clamp + SPI godoc + no-partial-result rule), the driver-leak in schema
  detection (portable `information_schema` probe + `Ready` fail-fast, no driver import), the same-pool DLQ
  stall, the phantom `OnAck`, the lease-TTL footgun default. Round 2 (on the transactional features) —
  the lock-strategy **`Nack`-always-commits** fix (CRITICAL 2: the earlier "restart-durable
  `delivery_count`" claim was retracted; durable across normal Nack, only a crash reverts one attempt),
  the **detached claim-tx** (HIGH 3: shutdown no longer auto-rolls-back in-flight work), outbox
  **strict-by-default** + nil-resolver validation, the deduper **`*sql.Tx` type** + MySQL verdict fix +
  `Purge` safety + segregated `InboxDialect`/`InboxOption`, and — decisively — **deferring D9** because
  its single carried tx cannot reconcile business-rollback + durable-count + delayed-redelivery. All
  folded here rather than discovered in production.

**Negative / costs**
- The poll loop holds `held` credits during the `Poll` DB round-trip (conservative under-utilization,
  the safe direction) — documented, tested, not a leak.
- The lease-TTL-vs-round-trip invariant and the lock-strategy pool-topology rules (pool-coupling +
  separate-DLQ-pool) are **documented, not statically enforced** (the `Source` and `Consumer`/sink are
  independent objects). Mirrors the ADR 0009 D3 precedent; a `Stats`/config cross-check is noted for a
  later plan. The lock strategy is the more expert-configuration option (its caveats are why lease/claim
  is the default).
- `sql` integration tests add **test-only** deps to the core `go.mod` (`testcontainers-go`, a Postgres
  driver, `go-sql-driver/mysql`) — allowed by the dependency policy (not imported by non-test code, no
  burden on consumers) but they enlarge `go.sum` and the `govulncheck`/`go mod tidy` surface. Accepted;
  `use-testcontainers` mandates real engines over fakes.
- v1 poll pacing is "drain fast, idle slow" only; a fixed-delay-after-every-poll mode is deferred (YAGNI).
- **D9 adds one field to the core `Delivery` SPI** (`BindContext`). Additive and source-compatible (nil =
  identity; existing struct-literal adapters unaffected), but it *is* a public-core-API surface change
  (SemVer minor) and a general dispatch hook, so it carries a small ongoing contract. Transactional
  consume is **lock-strategy-only** (a `ErrTransactionalConsumeRequiresLock` construction error otherwise),
  inheriting the lock strategy's connection-per-message and pool-topology caveats (D5).
- **Driver auto-detect is heuristic, and the exported `Dialect` is a larger pre-1.0 public surface.**
  A mis-detected derivative can run subtly wrong SQL (the deliberate correctness-for-flexibility
  trade-off); mitigated by the loud "auto-detect is convenience, `WithDialect` is correctness" doc and
  the `ErrDialectUndetected` fail-closed when nothing matches. Exporting `Dialect`/`ClaimedRow` commits
  more API to SemVer; accepted as a v0 SPI the user explicitly wants.

**Rejected alternatives**
- **Per-dialect constructors (`NewPostgresSource`/`NewMySQLSource`/…) with an internal, unexported
  `Dialect`** — the prior draft; **rejected on the user's decision** in favor of generic constructors +
  driver auto-detect + an exported `WithDialect` SPI. Mismatch-proof rigidity loses to the ability to
  serve wire-compatible derivatives and per-engine quirks (the caller supplies a `Dialect`); the residual
  mismatch risk is bounded by `ErrDialectUndetected` + explicit docs.
- **`PollingSource` wrapped as an internal `StreamingSource` shim** — rejected: the shim would have to
  consult the credit gate *before* `Poll` (else it over-pulls), splitting credit acquisition across the
  shim *and* `ingest` — exactly where an over-pull/double-count bug hides. A first-class pull path
  acquires at a single site per source kind.
- **A new atomic free-credit counter / mutex-cond gate for peek-then-poll** — rejected as unnecessary:
  the "acquire ≥1 (blocking) then non-blocking top-up, release surplus" loop reuses the existing channel
  semaphore unchanged and is single-acquirer-safe; a peek counter adds surface for no correctness gain.
- **Auto-`CREATE TABLE` on startup** — rejected (D2): a library running implicit DDL hides
  privilege/migration concerns; reference DDL + opt-in `EnsureSchema` keeps the caller in control.
- **A cross-dialect parameterized SQL template** — rejected (D3, the user's constraint): Postgres and
  MySQL diverge structurally (`RETURNING`, time functions, two-step claim), so one template would either
  execute wrong SQL on one engine or degrade to lowest-common-denominator. Each dialect owns its full
  statements.
- **A standalone reaper goroutine** — rejected (D4): folding lease expiry into the claim predicate
  reclaims expired rows on the next credited poll with zero extra lifecycle/goleak surface; a separate
  goroutine reclaims nothing the next poll wouldn't.
- **Capping the payload column adapter-side / forcing a `sql` byte-cap default (D7)** — rejected:
  decoding is runtime-owned (ADR 0001); an adapter-side cap can only strand poison rows, not divert them.
  The cap stays the consumer's `WithMaxPayloadBytes`, documented by the adapter.
