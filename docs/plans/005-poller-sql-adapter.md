# Plan 005 — Poller + `database/sql` adapter

> **For agentic workers:** REQUIRED SUB-SKILL — implement task-by-task via
> `superpowers:subagent-driven-development` (fresh implementer + reviewer per task) or
> `superpowers:executing-plans`. Every task is an independently green unit (`GOTOOLCHAIN=go1.25.0
> go test ./... -race` passes), red→green TDD, assert-closure tables (`table-test`), blackbox
> (`package …_test`), goleak-clean. **Read `CLAUDE.md`, spec 001 (§7.4.1, §7, §8, §9), and ADR 0010
> first; trust those over any memory.**

- **Spec:** [001 — Messaging core](../specs/001-messaging-core.md) §7.4.1 (credit-at-fetch), §7
  (settlement), §8 (framing), §9 (`sql`)
- **ADR:** [0010 — Poller + SQL adapter](../adrs/0010-poller-sql-adapter.md) (D1–D8, D10; **D9 deferred**);
  builds on [0002](../adrs/0002-adapter-spi.md), [0008](../adrs/0008-resilience-flow-control-api.md),
  [0009](../adrs/0009-resilience-hardening.md)
- **Predecessor:** Plans 001–004 (landed on `main`). **Successor:** a follow-up plan for **transactional
  consume** (ADR 0010 D9, the audit-forced redesign) and `adapter/http`; sequencing/numbering TBD.
- **Branch:** `feat/poller-sql-adapter`.

**Goal:** Drive any `PollingSource` from the runtime with credit-at-fetch flow control, and ship the
`adapter/database/sql` wire adapter (PostgreSQL + MySQL, lease/claim + lock strategies, at-least-once).

**Architecture:** `Run` gains a first-class **pull path** (poll loop) alongside today's streaming path,
sharing the worker pool / drain / breaker gate; credit is acquired **at fetch** reusing the existing
`creditGate`. The `sql` adapter frames `(Headers, payload)`↔columns, with a `Dialect` seam owning **all**
per-database SQL; row-time logic uses the DB clock, scheduling uses the injected `clockwork` clock.

**Tech Stack:** Go 1.25, stdlib `database/sql` (driver caller-injected — no driver import in non-test
code), `jonboulle/clockwork`. Test-only: `testcontainers-go` (real Postgres + MySQL), a Postgres driver
+ `go-sql-driver/mysql`, `testify`, `goleak`, `uber-go/mock` (SPI doubles).

## Global Constraints

- **Go 1.25**, `GOTOOLCHAIN=go1.25.0`; no language/stdlib features > 1.25; `CGO_ENABLED=0` builds.
- **Core deps unchanged in non-test code:** stdlib + `clockwork` only. `sql` production code imports
  `database/sql` (stdlib) — **never a driver**. Test-only deps (testcontainers, drivers) are allowed
  (not imported by non-test code); justify none beyond these.
- **`Dialect` owns all SQL — no cross-database string ever executes.** DB clock for all row timestamps;
  app (`clockwork`) clock only for scheduling.
- **Testing:** blackbox `_test` packages; `table-test` assert-closure; `use-testcontainers`
  (`RunTestDatabase(t, …)` / `RunTestMySQL(t, …)` helpers, real engines, never fakes); `use-mockgen` for
  SPI doubles; `goleak` `TestMain` per goroutine-starting package. Target ≥ 85% changed-pkg coverage;
  **every hot-path/typed-error branch covered**.
- **Never commit/push without approval** except the per-task commits this approved plan enumerates
  (`git commit` only; each a green unit). Whole-branch gate before the final increment.

---

## Task 1 — Poll pacing options, sentinels, config plumbing

**Files:** Modify `flowcontrol.go` (options + defaults), `errors.go` (sentinels), `consumer.go`
(`consumerConfig` fields + `NewConsumer` validation). Test: `flowcontrol_test.go`, `errors_test.go`.

**Produces:** `WithPollInterval[T](d)`, `WithPollMaxBatch[T](n)`; `ErrInvalidPollInterval`,
`ErrInvalidPollMaxBatch`; consts `defaultPollInterval = 1*time.Second`, `defaultPollMaxBatch = 100`,
`maxPollErrorBackoff = 30*time.Second`; config fields `pollInterval`, `pollIntervalSet`, `pollMaxBatch`,
`pollMaxBatchSet`.

- Options set the value + `…Set` flag (mirroring `maxInFlightSet`/`attemptTTLSet`, C2). `NewConsumer`:
  unset → default; `pollIntervalSet && d ≤ 0` → `ErrInvalidPollInterval`; `pollMaxBatchSet && n < 1` →
  `ErrInvalidPollMaxBatch`. Godoc each option (semantics: interval = idle wait after an *empty* poll;
  maxBatch = rows per poll). Add the two sentinels to `errors_test.go`'s wrap/compare table.
- **Hot-path branches:** interval unset→default / set-valid→used / set-`≤0`→err; maxBatch
  unset→default / set-valid→used / set-`<1`→err.
- **Tests (table):** `WithPollInterval(0)`/`(-1)`→`ErrInvalidPollInterval`; `WithPollMaxBatch(0)`→
  `ErrInvalidPollMaxBatch`; valid values round-trip (assert via a subsequent successful `NewConsumer`
  over a fake `PollingSource` from Task 2 — or defer the round-trip assert to Task 2 and assert only the
  error branches here). Streaming construction still ignores poll options (no behavior change).

## Task 2 — The pull path: poll loop with credit-at-fetch (core Poller)

**Files:** Create `poller.go` (poll loop + `releaseN`/`sleepCtx`/`pollErrorBackoff` helpers); Modify
`consumer.go` (`consumer` gains `streamSrc`/`pollSrc`; `NewConsumer` source resolution; `Run` branches
the producer). Test: `poller_test.go`, plus a fake `PollingSource` double (hand-written or mockgen).

**Consumes:** `creditGate` (`acquire`/`tryAcquire`/`release`, `credit.go`), `manage`/`managedDelivery`,
the worker pool + drain in `Run`. **Produces:** a wired `PollingSource` consumer.

- **Source resolution (`NewConsumer`):** resolve `StreamingSource` first (precedence, ADR 0010 D1), else
  `PollingSource`, else `ErrUnsupportedSource`. Store `streamSrc`/`pollSrc` (one non-nil); resolve
  `native` from whichever is set; wire `codec`/`live` via `resolveCodec` as today.
- **`Run` refactor:** factor the shared drain (`done`/timeout/`cancelSweep`/`ingressWG.Wait`) so both
  producers reuse it. Producer = streaming (`Stream`+`ingest`, unchanged) **or** `pollLoop`. `pollLoop`
  runs on `ingressWG`, **owns `close(workerCh)`** on exit, and takes `ctx` (shutdown) + `settleCtx`
  (drain-surviving Nack).
- **`Poll` SPI godoc (`spi.go`) hardened (HIGH audit):** add MUST clauses — `Poll` returns **≤ `max`**
  deliveries, **no deliveries alongside a non-nil error**, and **owns rollback of partial/claimed work**
  on the error/cancel path.
- **`pollLoop` (ADR 0010 D1 pseudocode):** acquire ≥1 (blocking; ctx-done→return); top-up
  `tryAcquire` to `pollMaxBatch`; `pollSrc.Poll(ctx, held)`; on error → **`logger.Error("poll failed")`**
  (D2/audit — the promised loud log) + `releaseN(held)` + `errN++` + `sleepCtx(pollErrorBackoff(errN))`;
  on success → **clamp `len(rows) > held`** (defensive, ERROR-log + Nack the excess unwrapped — HIGH
  audit), `errN=0`, `releaseN(held−len(rows))`, wrap each row `manage(d, sync.OnceFunc(gate.release))` →
  `select{ out<-md; <-ctx.Done()→finish(md.Nack(settleCtx,true,0)) + releaseN(len(rows)−i−1) + return }`
  (**EXACT** un-handed count — CRITICAL audit; never over-release); empty poll → `sleepCtx(pollInterval)`.
- **Helpers:** `releaseN(g,n)` = `n×<-g.tokens` (plain, not OnceFunc); **panics on n<0** (bug tripwire —
  CRITICAL audit); `sleepCtx(ctx,clock,d)` = `clock.After(d)` vs `ctx.Done()`, returns false on cancel
  (d≤0 → immediate true); `pollErrorBackoff(n)` =
  `ExponentialBackoff{Initial:pollInterval,Max:maxPollErrorBackoff,Mult:2}.Delay(n-1)`.
- **Hot-path branches:** acquire ctx-done→shutdown; top-up hits maxBatch / exhausts free credit; Poll
  error→log+release-all+backoff; `len(rows)>held`→clamp; Poll success surplus>0 release / surplus==0;
  empty poll→idle; hand-off ctx-done→Nack+exact-release; non-empty→immediate re-loop.
- **Tests (fake `PollingSource` + `clockwork.NewFakeClock`, goleak):**
  - **Flood/never-over-pull:** a fake source with a huge backlog + a handler that blocks on a signal;
    assert concurrent in-flight (rows handed, unsettled) never exceeds `maxInFlight` across the run
    (`WithMaxInFlight(n)` + `WithPollMaxBatch(m>n)`); the fake records the max `Poll` `k` it ever sees ≤
    remaining credit.
  - **Surplus release:** source returns fewer than requested → subsequent polls still reach full credit
    (no leaked/pinned credits); assert by draining to completion with a fast handler.
  - **Empty poll idle + no pin:** empty source → `pollInterval` waited on the fake clock (advance to
    release), zero credits held during idle (a concurrently-injected message is claimable immediately).
  - **Error backoff + release:** a source erroring N times then succeeding → backoff grows per the
    formula (fake-clock elapsed asserts, `RandomizationFactor=0`), all credits released between attempts,
    recovers on success (`errN` reset).
  - **Drain/goleak + ctx-done handoff (CRITICAL audit):** cancel mid-run → `pollLoop` exits, `workerCh`
    closes, workers join, in-flight row Nacked; goleak clean; bounded by `WithShutdownTimeout`. **Force
    the near-unreachable ctx-done-during-handoff arm** with a deliberately stalled worker (workerCh full)
    and assert the exact `len(rows)−i−1` release — Run must still return within the deadline (no hang).
  - **`Poll(max)` contract (HIGH audit):** a fake returning `len(rows) > held` is clamped (excess
    Nacked/logged, gate not corrupted); a fake returning `(rows, err)` together → rows discarded, no
    credit leak (releaseN never negative).
  - **Streaming unchanged:** an existing memory-adapter consumer test still passes (regression).
  - Note the fake-clock **waiter count**: the sweep ticker is a **stable** permanent waiter, but the
    **poll timer (`sleepCtx`) is INTERMITTENT** — present only during empty-poll idle / error backoff,
    absent while draining a backlog (audit correction to the plan's earlier "+1" wording). Sequence
    `BlockUntilContext` around whether the loop is mid-sleep; size advances so the poll timer (≤30s)
    never accidentally fires the 5m sweep.

## Task 3 — Dispatch-boundary panic recovery (ADR 0010 D6, fold-in #4)

**Files:** Modify `consumer.go` (`safeDecode`/`safeSend`/`safeAck`/`safeNack`; rewire `dispatch`/`divert`
/settle sites). Test: `consumer_test.go` (panicking SPI doubles over the memory adapter).

> **Note:** the `Delivery.BindContext` core hook (originally here as a D9 prereq) is **DEFERRED to Plan
> 006 with transactional consume** (ADR 0010 D9 STATUS: DEFERRED). It is NOT added in Plan 005 — no core
> `spi.go`/`Delivery` change lands this increment.

- `safeDecode(b []byte) (T, error)` wraps the `!liveValue` `codec.Decode` (the live assert cannot panic):
  panic → `fmt.Errorf("%w: %v", ErrPayloadDecode, r)` (permanent → invalid sink). `safeSend`,
  `safeAck`, `safeNack` wrap `sink.Send`/`d.Ack`/`d.Nack`: panic → synthetic error routed as the raw
  call's error would be (`safeSend`→sink-failed→Nack+retry; `safeAck`/`safeNack`→`finish` ERROR). Each
  logs ERROR (id only, no payload). Credit is released *before* the wrapped Ack/Nack (via `manage`), so a
  panicking settle never pins credit; the worker's deferred `md.release` nets it.
- Replace the raw `c.decode`→`codec.Decode`, `sink.Send`, `d.Ack`, `d.Nack` call sites in
  `dispatch`/`divert` with the `safe*` wrappers.
- **Hot-path branches:** each wrapper's panic path (decode→permanent; send→retry; ack→log; nack→log) and
  its no-panic passthrough.
- **Tests (table, over memory):** doubles that panic in `Decode` / `Send` / `Ack` / `Nack` driven
  through a real `Run`: process does not crash; message settles per the mapped path (decode-panic →
  `OnInvalidMessage`+`errors.Is ErrPayloadDecode`; send-panic in a nil-vs-configured invalid sink →
  retried; ack/nack-panic → ERROR logged, credit released, no goleak, run drains). A codec double that
  panics is paired with a `[]byte` `StreamingSource` (not `LiveValueSource`).
- **Extensions folded in during the Task 2/Task 3 whole-branch reviews (ADR 0010 D6 updated to match):**
  (1) **`safePoll`** guards the D1 Poller's new `PollingSource.Poll` call site (panic → `pollLoop`'s
  existing error-backoff; no new sentinel, no double-log). (2) The `safeNack` guard is applied to **every**
  adapter-`Nack` call site, not only `dispatch`/`divert` — the `admit` shutdown/overflow Nacks, the
  `process` drain/breaker Nacks, and the poll loop's ctx-done-handoff + `clampExcess` Nacks — because a
  panicking wire-adapter `Nack` on ordinary graceful shutdown would otherwise crash the process. A
  deterministic overflow-shed panic test proves the newly-wrapped shed site; the shutdown-race sites are
  covered by the same (100%-covered) wrapper.

## Task 4 — `sql` scaffold: exported `Dialect` SPI, framing, identifiers, schema, PostgreSQL dialect

**Files:** Create `adapter/database/sql/{doc.go,dialect.go,framing.go,errors.go,postgres.go,ddl.go}`;
`adapter/database/sql/testutils.go` (`RunTestDatabase` — Postgres testcontainer, per `use-testcontainers`).
Test: `adapter/database/sql/dialect_pg_test.go` (dialect against real Postgres). Add test-only deps to
`go.mod` (`testcontainers-go`, a Postgres driver).

**Produces (package `sql`):** `Querier` (exported — the `*sql.DB`/`*sql.Tx` subset), `ClaimedRow{ID
int64; MsgID string; Headers, Payload []byte; DeliveryCount int; LeaseEpoch int64}` (exported), the
**exported `Dialect`** SPI (ADR 0010 D3 signature), `PostgresDialect() Dialect` (the built-in),
`PostgresDDL(table) (string, error)`, `ErrSchemaNotReady`, `ErrInvalidTableName`; framing `encodeHeaders(msgin.Headers)
([]byte,error)` / `decodeHeaders([]byte) (msgin.Headers,error)`; `validateIdent(name) error`.

- **Framing (§8):** `Headers`→JSON (iterate `Headers.All()`; store the reserved `msgin.*` + custom keys;
  round-trip `msgin.timestamp` as RFC3339, `msgin.delivery-count` as int). Payload column = the wire
  `[]byte` verbatim. `decodeHeaders` reconstructs via `msgin.NewHeaders`.
- **Identifiers:** `validateIdent` = `^[A-Za-z_][A-Za-z0-9_]*$` else `ErrInvalidTableName`; the dialect
  quotes (`"t"`). Constructors validate `table` up front.
- **Exported SPI:** `Dialect`, `Querier`, and `ClaimedRow` are public (ADR 0010 D3) so a caller can pass
  a custom `Dialect` for a derivative/quirk via `WithDialect` (Task 5). `PostgresDialect()` returns the built-in.
- **PostgreSQL dialect:** `Claim` (one-shot `UPDATE … FROM (SELECT … FOR UPDATE SKIP LOCKED LIMIT $1)
  … RETURNING`, claim predicate `visible_after <= now() AND (locked_at IS NULL OR locked_at <= now() −
  $leaseTTL)`, `lease_epoch`/`delivery_count`++); `Ack` (fenced `DELETE`, returns `applied`); `Nack`
  (fenced `UPDATE … visible_after = now() + $delay`, returns `applied`); `Insert`
  (`INSERT … RETURNING id`, `visible_after = now() + $delay`); `EnsureSchema` (`CREATE TABLE IF NOT
  EXISTS` + index); `DDL`; **`SchemaExists`** (portable `SELECT 1 FROM information_schema.tables WHERE
  table_name=$1 AND table_schema=current_schema()` — **no driver import**, HIGH audit; replaces the
  original driver-error `IsUndefinedTable`). All times DB-clock (`now()`), durations passed as
  `interval`-typed params.
- **Package name + alias:** the package is `sql` (path `adapter/database/sql`); `doc.go` and every example
  import it **aliased** (`import msginsql "…/adapter/database/sql"`) so callers don't collide with stdlib
  `database/sql`. Also export `RecommendedMaxPayloadBytes` (≈1 MiB, ADR 0010 D7) here.
- **`RunTestDatabase(t, opts...)`:** starts a Postgres container, returns `*sql.DB` (+ applies the DDL or
  leaves it to the test), `t.Cleanup` terminates. Single helper (skill rule).
- **Hot-path/error branches:** `validateIdent` reject; `SchemaExists` true/false; `EnsureSchema`
  create-then-noop; framing round-trip incl. delivery-count/timestamp; claim empty vs rows; fenced
  Ack/Nack applied vs not.
- **Tests (real Postgres):** DDL applies + `EnsureSchema` idempotent; identifier rejection; claim a row
  bumps `delivery_count`/`lease_epoch` + sets lock; fenced `Ack` deletes / stale-epoch `Ack`
  `applied=false`; `Nack` sets `visible_after` (not visible until elapsed); expired-lease row re-claims
  (advance real time past a tiny `leaseTTL`); `SchemaExists` false on a missing table / true after DDL;
  framing round-trips a message with custom + reserved headers. **Note (audit):** DB-clock behaviors
  (lease expiry, `visible_after`) can't use a fake clock — use tiny real TTLs/delays + generous margins.

## Task 5 — Lease/claim `Source` + generic constructor with dialect auto-detect / `WithDialect`

> **Prerequisite (ADR 0010 D11, added during Task 5):** the core gains a public
> `msgin.NewMessage[T](payload T, headers Headers) Message[T]` (build-from-parts, no id/timestamp
> stamping) — a wire inbound adapter cannot build `Message[any]` from decoded `(payload, Headers)` outside
> package `msgin` (the fields are unexported, and `New` re-stamps id/timestamp). `Source.Poll` reconstructs
> each delivery's `Msg` with it. This is an additive core change (message.go) that lands in the Task 5
> commit alongside the code + ADR D11.

**Files:** Create `adapter/database/sql/source.go` (shared `Source` body, dialect-agnostic),
`adapter/database/sql/options.go` (`WithDialect`, `WithLeaseTTL`, `WithStrategy`, `WithLockedBy`),
`adapter/database/sql/detect.go` (`resolveDialect(db) (Dialect, error)`). Modify
`dialect.go`/`errors.go` (`NewPollingSource`, `ErrDialectUndetected`, `ErrStaleLease`). Test:
`adapter/database/sql/source_pg_test.go`, `detect_test.go`.

**Consumes:** Task 4 `Dialect`/`PostgresDialect()`/`SchemaExists`/framing. **Produces:**
`NewPollingSource(db *sql.DB, table string, opts ...Option) (*Source, error)` (ADR 0010 D3: `WithDialect`
→ else driver auto-detect → else `ErrDialectUndetected`); `WithDialect(Dialect) Option`; `Strategy`
(`LeaseClaim` default, `LockForUpdate`); `ErrInvalidLeaseTTL`, `ErrDialectUndetected`, `ErrStaleLease`;
`Source.EnsureSchema(ctx) error`; **`Source.Ready(ctx) error`** (fail-fast, ADR 0010 D2); `Source`
implements `msgin.PollingSource` + `msgin.NativeReliability` (`NativeRedelivery()=true`,
`NativeDeadLetter()=false`).

- **Dialect resolution** (`resolveDialect`, ADR 0010 D3): if `WithDialect(d)` set → `d`; else
  `reflect.TypeOf(db.Driver()).PkgPath()`/`String()` substring → `PostgresDialect()`/`MySQLDialect()`;
  else `ErrDialectUndetected` (naming the driver type). **No driver import.**
- **`Ready(ctx)`** runs `dialect.SchemaExists` → `ErrSchemaNotReady` (naming the table) if absent — the
  fail-fast boot check (ADR 0010 D2).
- `Poll(ctx, max)`: `dialect.Claim(ctx, db, table, min(max,batchCap), lockedBy, leaseTTL)` → for each
  `ClaimedRow` build `msgin.Delivery{Msg: framed message with `HeaderDeliveryCount`=count,
  Ack/Nack: fenced closures capturing `id`/`lockedBy`/`epoch`}`. `Ack` closure → `dialect.Ack(...)`;
  `applied=false` → **return `ErrStaleLease` (non-nil), NOT nil** (MEDIUM audit — suppress the runtime's
  phantom `OnAck`/evict), Source logs WARN. `Nack(requeue,delay)` → `dialect.Nack(...)` (requeue=false ≡
  delay 0; at-least-once cannot drop — **godoc this on the exported `Source`**). On any `Poll` query
  error, wrap as `ErrSchemaNotReady` iff a follow-up `SchemaExists` returns false (portable), else raw.
- Options (**sensible-default per CLAUDE.md**): `WithDialect(d)` (guaranteed path / quirk escape);
  `WithLeaseTTL(d)` (`d>0` else `ErrInvalidLeaseTTL`; **unset → 5m default** (matches `defaultAttemptTTL`,
  safe above a slow handler); godoc the full invariant "must exceed handler round-trip incl. settle
  latency + clock-skew margin, not merely `WithHandlerTimeout`"); `WithStrategy(Strategy)`;
  `WithLockedBy(string)` (default a random id). **`WithClock` dropped** (MEDIUM audit — inert in v1; add
  it in the plan that consumes it).
- **Hot-path branches:** dialect resolved via `WithDialect` / auto-detect pg / auto-detect mysql /
  undetected→`ErrDialectUndetected`; `Ready` ok / not-ready; claim empty→no deliveries; claim
  rows→deliveries; Ack applied / stale→`ErrStaleLease`; Nack applied/stale; schema-not-ready;
  `WithLeaseTTL(0)`→err; `WithLeaseTTL` unset→5m default.
- **Detection tests:** `WithDialect` wins over auto-detect; a real Postgres driver (`pq`/`pgx`)
  auto-detects `PostgresDialect()`, MySQL driver auto-detects `MySQLDialect()` (exercised by the
  integration suites); an unrecognized driver (a registered dummy `driver.Driver` with a non-matching
  type name) → `ErrDialectUndetected` naming the type.
- **Tests (real Postgres, via the runtime `Run` where useful):** produce N rows → consume via a
  `msgin.NewConsumer(source, handler)` real `Run`: all Acked (rows deleted), at-least-once; a handler
  error → `Nack` → row re-visible after delay → redelivered with `delivery-count` incremented; a handler
  that outruns a tiny `leaseTTL` → row reclaimed by another poll, the stale `Ack` no-ops (fence), no
  double-delete; `NativeRedelivery` drives the runtime to populate `delivery-count` from the header (not
  the tracker); missing table → `ErrSchemaNotReady` surfaces (poll-loop error backoff). goleak clean.

## Task 6 — `Outbound` adapter (`INSERT`) + round-trip

**Files:** Create `adapter/database/sql/outbound.go` (shared body + `NewOutboundAdapter`). Test:
`adapter/database/sql/outbound_pg_test.go`.

**Produces:** `NewOutboundAdapter(db, table, opts...) (*Outbound, error)` (same dialect
auto-detect/`WithDialect` resolution as `NewPollingSource`); `Outbound` implements
`msgin.OutboundAdapter`; `Outbound.EnsureSchema(ctx) error`; `Outbound.Ready(ctx) error`.

- `Send(ctx, msg)`: frame `(headers,payload)`; `dialect.Insert(ctx, db, table, msgID, headers, payload,
  delay=0)`. Wrap a query error as `ErrSchemaNotReady` iff `SchemaExists` is false (portable). Not a
  `LiveValueSource` → the producer JSON-encodes the payload (default codec).
- **Hot-path branches:** insert ok; schema-not-ready (`Ready` + `Send`-wrap); framing.
- **Tests (real Postgres):** `msgin.NewProducer(outbound)` sends → row present with correct
  headers/payload; **round-trip**: producer→outbound INSERT, then a `Source` consumer Acks it (produce
  then consume the same table); `Outbound` usable as a `RetryPolicy.DeadLetter` / `WithInvalidMessageSink`
  target (a poison message lands in the DLQ table); missing table → `ErrSchemaNotReady`.

## Task 7 — MySQL dialect (`MySQLDialect()`, driver auto-detect) + dual-engine suites

**Files:** Create `adapter/database/sql/mysql.go` (`mysqlDialect`, `MySQLDialect() Dialect`, `MySQLDDL`);
modify `detect.go` (mysql/mariadb → `MySQLDialect()`), `testutils.go` (`RunTestMySQL(t, opts...)` — MySQL
testcontainer). Add `go-sql-driver/mysql` (**test-only**). Test: convert `*_pg_test.go` behavior suites
to run over **both** engines via a dialect-parameterized table (`RunTestDatabase` / `RunTestMySQL`).

- No separate MySQL constructors — the generic `NewPollingSource`/`NewOutboundAdapter` auto-detect a
  MySQL driver (or take `WithDialect(MySQLDialect())`).
- **MySQL dialect:** `Claim` = tx { `SELECT … FOR UPDATE SKIP LOCKED LIMIT ?` (claim predicate with
  `UTC_TIMESTAMP(6)` + `DATE_SUB` for lease expiry) → collect rows → `UPDATE … SET …, lease_epoch=
  lease_epoch+1, delivery_count=delivery_count+1 WHERE id IN (…)` → return rows with counts computed in
  Go } committed. `Insert` = `INSERT …` then `LAST_INSERT_ID()`. `Ack`/`Nack` fenced (single UPDATE/
  DELETE). `DATE_ADD(UTC_TIMESTAMP(6), INTERVAL ? MICROSECOND)` for delays. `SchemaExists` =
  `information_schema.tables … table_schema=DATABASE()` (portable, no driver import). `MySQLDDL`
  (`BIGINT AUTO_INCREMENT`, `DATETIME(6)`, `LONGBLOB`, `JSON`, `(locked_at, visible_after)` index).
- **Hot-path branches:** the two-step claim (empty select → no update; rows → update+return); MySQL time
  fns; `SchemaExists` true/false; mysql/mariadb driver auto-detect.
- **Tests:** the full Task 4/5/6 behavior suite green on MySQL (dialect table cases): claim/ack/nack/
  fence/expiry/delivery-count/round-trip/schema-not-ready — proving the `Dialect` abstraction holds
  across both engines.

## Task 8 — Lock/`FOR UPDATE` strategy (both dialects)

**Files:** Modify `source.go` (strategy branch: lock-mode `Poll`), `postgres.go`/`mysql.go` (lock-mode
dialect methods: `ClaimLock`/tx-carried), `options.go` docs. Test: `source_lock_test.go` (both engines).

- `WithStrategy(LockForUpdate)`: `Poll` calls `dialect.ClaimLock` → begins a tx on
  **`context.WithoutCancel(ctx)`** (HIGH 3 audit — detached so shutdown parent-cancel doesn't auto-rollback
  in-flight work before the drain deadline), `SELECT … FOR UPDATE SKIP LOCKED LIMIT 1`,
  `UPDATE … SET delivery_count = delivery_count+1` in that tx, returns ≤1 `Delivery` carrying the tx.
  `Ack` = `DELETE` + `tx.Commit`. **`Nack(requeue,delay)` = `UPDATE … clear lock, visible_after=now()+delay`
  + `tx.Commit` — ALWAYS commits, never rolls back (CRITICAL 2 audit)**: committing persists the
  `delivery_count++` (so `HeaderDeliveryCount`/`MaxAttempts` progress across normal redeliveries) and
  releases the `FOR UPDATE` lock just as a rollback would — the plain lock strategy has no business writes
  to undo, so there is nothing to roll back. Only a genuine **crash** (tx auto-rollback) reverts one
  in-flight attempt (rare, documented). **On any claim error, `ClaimLock` rolls back its tx before
  returning `(nil, err)`** (no conn leak; Poll no-partial-result contract). One pooled conn per in-flight
  message.
- **Retract the earlier overclaim:** the round-1 "delivery_count persisted in the tx → restart-durable
  MaxAttempts" was **false** as drafted (the default `Backoff==nil`→`delay=0`→rollback reverted the ++).
  The always-commit Nack (above) is the actual fix; MaxAttempts is durable across **committed** Nack
  redeliveries, not across crashes.
- **Same-pool DLQ stall (round-1 HIGH):** godoc on `WithStrategy(LockForUpdate)` **mandates** a `sql`
  DLQ/invalid sink use a **separate `*sql.DB` (separate pool)**, or a shared pool sized `poolSize ≥
  maxInFlight + divert-concurrency` — else the divert `INSERT` (before `Ack`, NF-3) blocks on an exhausted
  pool while claim conns are pinned → stall until shutdown. Example uses a separate pool.
- **Pool-coupling (NF-9):** godoc warns effective in-flight = `min(WithMaxInFlight, WithConcurrency,
  poolSize−headroom)`; example sets `db.SetMaxOpenConns` + `WithPollMaxBatch(1)` + separate DLQ pool.
  Not statically enforced. `WithLeaseTTL` ignored in lock mode (godoc).
- **Hot-path branches:** lock claim empty (tx rolled back, ≤0 rows) / one row; claim error → rollback;
  `delivery_count`++; Ack commit; **Nack always commits (delay>0 and delay=0)**; single-row cap
  (`ClaimLock` ignores k>1); tx begun detached (WithoutCancel).
- **Tests (both engines):** claim+Ack removes + commits; **`delivery_count` progresses across a
  Nack-redeliver loop** (fail N times → header count reaches N → `MaxAttempts` dead-letters — proves the
  always-commit fix, and that the DEFAULT `Backoff==nil` policy still advances the count); a **crash**
  (drop the Delivery unsettled → tx auto-rollback) reverts the *in-flight* attempt only (documented);
  `ClaimLock` error rolls back (no conn leak — `db.Stats().InUse` returns to 0); **shutdown does not
  redeliver all in-flight** (detached-tx / HIGH 3 regression: cancel mid-flight, assert in-flight messages
  Ack within the drain deadline rather than every one redelivering); `Poll(10)` returns ≤1;
  **same-pool-DLQ stall regression** (separate-pool DLQ drains without deadlock). goleak clean.

## Task 9 — Transactional outbox: `WithSharedTransaction` (ADR 0010 D8)

**Files:** Modify `adapter/database/sql/outbound.go` (resolver plumbing in `Send`), `options.go`
(`WithSharedTransaction`, `WithOpportunisticSharedTransaction`, `WithLogger`), `errors.go`
(`ErrNoSharedTransaction`, `ErrNilResolver`). Test: `adapter/database/sql/outbox_pg_test.go` (real
Postgres) + a dialect-parameterized mirror on MySQL.

**Consumes:** Task 6 `Outbound`, Task 4 `Querier`/`Dialect.Insert`. **Produces:** `type
TransactionResolver func(ctx context.Context) (Querier, error)`; **`WithSharedTransaction(r
TransactionResolver) Option`** (STRICT — the safe default) and **`WithOpportunisticSharedTransaction(r
TransactionResolver) Option`** (fallback+WARN) — two named options, NOT `required bool` (audit);
`WithLogger(*slog.Logger) Option` (default discard); `ErrNoSharedTransaction`, `ErrNilResolver`.

- **Nil validation:** a nil `TransactionResolver` → construction-time **`ErrNilResolver`** from
  `NewOutboundAdapter` (audit — never a deferred nil-func panic).
- `Send(ctx, msg)` with a resolver set: `q, err := r(ctx)`; **`err != nil` → wrap+return**; **`q != nil` →
  `dialect.Insert(ctx, q, …)`** on the caller's tx (borrowed — msgin **never** `Commit`s/`Rollback`s it);
  **`q == nil`** → **strict** (`WithSharedTransaction`) → **`ErrNoSharedTransaction`**; **opportunistic**
  (`WithOpportunisticSharedTransaction`) → **WARN log** ("no shared transaction in context; standalone
  insert — atomicity NOT achieved", id only) + `dialect.Insert(ctx, db, …)` fallback. No resolver →
  today's `db` insert (Task 6, unchanged).
- **DLQ-sink misuse doc (LOW audit):** godoc that a strict shared-tx Outbound must NOT be a
  `RetryPolicy.DeadLetter`/invalid sink (the runtime's divert `settleCtx` carries no tx → every poison
  message → `ErrNoSharedTransaction` → never dead-lettered).
- **Hot-path branches:** nil resolver→`ErrNilResolver`; resolver unset (standalone); resolver error→wrap;
  `q!=nil`→shared insert; `q==nil` & strict→`ErrNoSharedTransaction`; `q==nil` & opportunistic→WARN+fallback.
- **Tests (real Postgres, then MySQL mirror):**
  - **Atomicity:** open a caller tx, write a business row + `Producer.Send` (resolver returns the tx) →
    **roll back** → assert neither the business row nor the outbox row exists; repeat with **commit** →
    both exist. Uses a real `*sql.Tx` put into `ctx` via a test context key; the resolver extracts it.
  - **Commit gates visibility:** with the caller tx still open (uncommitted), a concurrent `Source.Poll`
    does **not** see the outbox row (SKIP LOCKED on committed rows only); after commit, the next poll
    claims it → end-to-end outbox relay.
  - **Strict vs opportunistic:** strict + no tx in ctx → `Send` returns `ErrNoSharedTransaction` (nothing
    inserted); opportunistic + no tx → row inserted standalone **and** one **WARN** line captured
    (injected `slog` handler). `WithSharedTransaction(nil)` → `ErrNilResolver` at construction.
  - **Borrowed-tx safety:** after `Send` on a shared tx, the caller's tx is still usable (msgin did not
    commit/roll it back) — assert by committing afterward and finding the row.
  - Resolver returning `(nil, err)` → `Send` propagates it (no insert).

> **Task 10 (Transactional consume / `WithTransactionalConsume`) is DEFERRED to Plan 006** (ADR 0010 D9,
> audit-forced tx-model redesign). It is NOT part of Plan 005. See ADR 0010 D9 "Why deferred" + "Intended
> Plan-006 redesign".

## Task 10 — Idempotent consume / dedup inbox: `InboxDeduper` (ADR 0010 D10)

**Files:** Create `adapter/database/sql/inbox_dedup.go` (`InboxDeduper`, `NewInboxDeduper`,
`MarkProcessed`, `EnsureSchema`, `Ready`, `Purge`, `InboxDDL`, `InboxOption`, `WithInboxTable`,
`WithInboxDialect`); modify `dialect.go` (add the narrow **`InboxDialect`** interface —
`InsertInboxIfAbsent`/`PurgeInbox`/`SchemaExists`/inbox `DDL` — segregated from the fat source `Dialect`,
audit ISP), `postgres.go`/`mysql.go` (implement it). Test: `adapter/database/sql/inbox_dedup_pg_test.go`
+ MySQL mirror.

**Consumes:** Task 4 `SchemaExists`/framing, Task 7 MySQL dialect. **Produces (strategy 2, D10 —
different-DB idempotent consumer):** `NewInboxDeduper(businessDB *sql.DB, opts ...InboxOption)
(*InboxDeduper, error)` (dialect auto-detect / `WithInboxDialect`); **`InboxOption`** (segregated type,
audit); `WithInboxTable(string)` (**default `"msgin_inbox"`** — sensible default); `WithInboxDialect(InboxDialect)`;
**`InboxDeduper.MarkProcessed(ctx, tx *sql.Tx, msgID) (already bool, err error)`** (concrete `*sql.Tx`,
NOT `Querier` — audit HIGH 5, so the pool can't be passed → no silent-loss); `.EnsureSchema(ctx)`;
`.Ready(ctx)`; `.Purge(ctx, olderThan) (int64, error)`; `InboxDDL(d InboxDialect, table) (string, error)`;
`ErrNilAdapter` on nil `businessDB`.

- **Segregated `InboxDialect` (audit ISP):** the narrow interface — `InsertInboxIfAbsent`, `PurgeInbox`,
  `SchemaExists`, inbox `DDL` — NOT the fat source `Dialect`. Built-ins satisfy both.
- **`MarkProcessed`:** `dialect.InsertInboxIfAbsent` on the caller's business `tx`. **Verdict derived
  precisely, NOT from `rowsAffected` (audit MEDIUM 6):** Postgres `INSERT … ON CONFLICT (msg_id) DO
  NOTHING RETURNING` (exact); **MySQL inserts then VERIFIES with a `SELECT`** (`INSERT IGNORE` demotes
  truncation/range errors to `rowsAffected==0` → false `already=true` → dropped message), so `already`
  means *genuinely a duplicate*. `processed_at` column (DB clock) for retention.
- **`Purge`:** `DELETE … WHERE processed_at < now() − olderThan` — manual (no goroutine, D4 precedent).
  **Safety godoc (audit MEDIUM 10):** only safe with a **finite `MaxAttempts`**; `olderThan` MUST exceed
  the source's max redelivery window, which is unbounded under the default `MaxAttempts==0` — purging a
  still-redeliverable id → a late redelivery reads `already=false` → double-process.
- **Nil validation:** `NewInboxDeduper(nil, …)` → `ErrNilAdapter` at construction (audit).
- **Sensible defaults (CLAUDE.md):** opt-in (handler calls it); table defaults `msgin_inbox`; dialect
  auto-detects from `businessDB`; `EnsureSchema`/`Ready` mirror the source (D2). Consumer stays plain
  at-least-once unless the handler adopts it.
- **Hot-path branches:** dialect resolve/auto-detect/undetected; `MarkProcessed` first-time
  (`already=false`) / duplicate (`already=true`); MySQL non-duplicate ignorable-error is NOT read as
  duplicate; `Ready` ok/not-ready; `Purge` deletes N / 0; nil businessDB → `ErrNilAdapter`.
- **Tests (real Postgres, then MySQL mirror):**
  - **Idempotency:** call `MarkProcessed(tx, id)` twice → first `already=false`, second `already=true`; a
    business handler that does work + `MarkProcessed` in one tx, redelivered → the second sees
    `already=true` and skips → business effect applied **once**.
  - **Atomic with business tx (the silent-loss guard):** `MarkProcessed` + business write in one tx,
    **roll back** → neither the dedup row nor the business row persists (so a rolled-back attempt is
    genuinely retried, not falsely deduped). The `*sql.Tx` signature makes passing the pool a compile error.
  - **MySQL false-positive guard:** an `INSERT` that a non-duplicate error would ignore is NOT reported as
    `already=true` (the `SELECT`-verify path).
  - `Purge(olderThan)` removes only rows older than the cutoff (DB-clock; tiny real durations);
    `Ready`/`EnsureSchema`/`InboxDDL` as in Task 4. goleak clean.

## Task 11 — Docs, runnable examples, D7 resolution, whole-branch gate

**Files:** `adapter/database/sql/doc.go` (package overview: **the import-alias convention**, strategies,
guarantees, schema ownership + `Ready`/`ErrSchemaNotReady`, the lock-strategy separate-DLQ-pool rule, the
non-zero-`RetryPolicy.Backoff` recommendation, the `WithMaxPayloadBytes`/`RecommendedMaxPayloadBytes`
recommendation — ADR 0010 D7, **and the transactional-outbox pattern — write via `WithSharedTransaction`
(strict) / `WithOpportunisticSharedTransaction` + relay via the `Source`, incl. the same-database
invariant and the DLQ-sink caveat** — ADR 0010 D8; **and the different-DB durable-consume strategy —
`InboxDeduper` idempotent consume, with the sensible default = plain at-least-once, and a pointer that
same-DB transactional consume (D9) is coming in Plan 006** — ADR 0010 D10/D9); `example_test.go` (runnable
`Example…` over Postgres, **importing the package aliased**: DDL/`EnsureSchema`, `Ready`, produce→consume,
**setting `WithMaxPayloadBytes(RecommendedMaxPayloadBytes)`**, **an `ExampleWithSharedTransaction`** (atomic
business-write + publish), **and an `ExampleInboxDeduper`** (different-DB idempotent consume)); godoc on
every exported symbol. **Update spec 001 §9** to fold the `status` column into the ADR 0010 D4
`locked_at`/`visible_after` encoding (mark superseded — traceability).

- D7: doc.go + example show `WithMaxPayloadBytes(RecommendedMaxPayloadBytes)` on the consumer; no forced
  adapter default. Godoc the `requeue=false→true` collapse on the exported `Source`. D8/D10: the
  outbox + dedup examples show the resolver/deduper, the strict-default, the same-DB invariant, and the
  durable-consume guidance (same-DB → D9 later; different-DB → `InboxDeduper` now).
- **Whole-branch delivery gate (CLAUDE.md §5):** `/code-review` + `/security-review` over `main..HEAD`;
  resolve/triage every finding; `GOTOOLCHAIN=go1.25.0 go test ./... -race` green (shake with `-count`);
  `go vet`, `gofmt`/`gofumpt`, `CGO_ENABLED=0 build`, `go mod tidy` (stable), `go mod verify`,
  `govulncheck`, `golangci-lint` clean; coverage ≥ 85% on changed files, every listed hot-path branch
  covered. Consolidate to logical-feature commits (amend in-branch). Update `docs/HANDOVER.md` + memory.
  Fast-forward + push `main` (user-authorized), delete the branch.

## Traceability

Commits carry `Spec: 001`, `Plan: 005`, `ADR: 0010` trailers. This plan implements spec §7.4.1 (Poller
credit-at-fetch) + §9 (`sql`, lease + lock strategies × Postgres + MySQL), adds the transactional
**outbox** (write, ADR 0010 D8) and the **different-DB durable-consume** strategy (idempotent
`InboxDeduper`, D10). It clears fold-in #4 (D6) and resolves #5-D5's wire-source role (D7). **Deferred to
a follow-up plan:** transactional consume (D9, audit-forced tx-model redesign), the `Delivery.BindContext`
core hook (with D9), and a future **CDC / logical-replication inbound adapter** (durable publishing is
outbox-only, D8) — alongside backlog #6 at-least-once overflow, #3-residual TTL enforcement, #5-complexity
codec-nesting, per `docs/HANDOVER.md` §4.

## Self-review notes (post-audit, 2 rounds)

- **Spec coverage:** §7.4.1 credit-at-fetch → Task 2; §9 `sql` two strategies × two dialects → Tasks
  4–8; §8 framing → Task 4; §7 settlement reused unchanged; schema ownership/`Ready`/`ErrSchemaNotReady`
  → Tasks 2,4,5,6; transactional outbox (D8) → Task 9; different-DB idempotent consume (D10) → Task 10;
  fold-in #4 → Task 3; D7 → Task 11. Same-DB transactional consume (D9) **deferred to Plan 006**. No spec
  §9 `sql` clause left unassigned.
- **Round 1 audits (Poller/sql core) folded in:** CRITICAL exact ctx-done credit-release count +
  negative-`n` panic + stalled-worker test (Task 2); HIGH `Poll(max)` contract + clamp + no-partial-result
  (Task 2, `spi.go`); HIGH portable `SchemaExists` probe + `Ready` fail-fast, no driver import (Tasks
  4–6); same-pool-DLQ separate-pool rule (Task 8); MEDIUM 5m lease-TTL default, `ErrStaleLease` (no
  phantom `OnAck`), dropped `WithClock`, poll-error ERROR log, spec §9 `status` reconciliation,
  `RecommendedMaxPayloadBytes`, kept-`sql`-name alias.
- **Round 2 audits (transactional features) folded in:** lock-strategy **Nack-always-commits** (CRITICAL 2
  — retracted the false "restart-durable delivery_count" claim; Task 8) + **detached claim-tx** (HIGH 3;
  Task 8); outbox **strict-by-default two options** + WARN + `ErrNilResolver` + DLQ-sink caveat (Task 9);
  deduper **`*sql.Tx` signature** (HIGH 5 silent-loss), **MySQL `SELECT`-verify** (MEDIUM 6), `Purge`
  safety, segregated **`InboxDialect`/`InboxOption`** (Task 10); **D9 deferred** (the single-carried-tx
  cannot reconcile business-rollback + durable-count + delayed-redelivery — research confirms transactional
  consume is worth doing *right* in its own increment).
- **Note:** D8 (outbox) + D10 (deduper) + the Task-8 lock fixes were added/changed after round 1 and
  audited in round 2; the round-2 CRITICALs did **not** destabilize the round-1 core conclusions (credit
  accounting, drain, fake-clock waiters) — only the lock-strategy `delivery_count` claim was retracted.
