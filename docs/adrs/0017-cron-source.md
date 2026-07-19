# ADR 0017 — Cron / recurring source + distributed coordination (Elector/Locker)

- **Status:** Proposed (2026-07-19) — pending the mandatory adversarial audit of the spec + ADR + plan bundle and
  an explicit go-ahead before implementation (CLAUDE.md design-time gate).
- **Spec:** [Spec 006 — Cron source + coordination](../specs/006-cron-source.md).
- **Plan:** [Plan 011 — Cron source + coordination](../plans/011-cron-source.md).
- **Depends on:** [ADR 0016 — robfig/cron core dependency](0016-robfig-cron-dependency.md) (the parser),
  [ADR 0002](0002-adapter-spi.md) (inbound SPI: `StreamingSource`/`LiveValueSource`/`Delivery`),
  [ADR 0004](0004-clockwork-dependency.md) (`clockwork`), [ADR 0010](0010-poller-sql-adapter.md) (the
  `InboxDeduper` dedup pattern the SQL `Locker` reuses).

## Context

msgin has no way to originate a message on a recurring/cron schedule (Spec 006 §1). The runtime already exposes
the push-side inbound seam `StreamingSource.Stream(ctx, out chan<- Delivery) error` (the `memory` adapter is the
reference), so a cron source fits with **no core runtime change**. Two shaping problems: (1) how the source fires
and emits without a background goroutine (goleak) or app↔clock nondeterminism; (2) how to make it fire **once
across N replicas** in production without forcing external infrastructure on the caller.

## Decision

Ship an `adapter/cron` package (root module, ADR 0016) with three parts.

### 1. `Source[T]` — a `StreamingSource` emitting caller-defined messages on schedule

- `cron.NewSource[T](spec string, factory func(fire time.Time) T, opts ...Option) (*Source[T], error)`.
- Implements `StreamingSource` **and** `LiveValueSource` (`EmitsLiveValue() → true`) — it emits live Go values, so
  `NewConsumer[T]` pairs it with no codec (like `memory`). (Spec D2, D4.)
- **Firing loop** (Spec D5), the `memory.Broker.Stream` template — **no background goroutine**:
  ```
  for {
      next := schedule.Next(clock.Now())            // robfig Schedule, in WithLocation tz
      select {
      case <-clock.After(next.Sub(clock.Now())):    // fire
          if gate != nil && !win(ctx, gate, next) { continue }   // coordination (part 3)
          msg := msgin.New[any](factory(next), msgin.WithClock(clock))
          select {
          case out <- Delivery{Msg: msg, Ack: noop, Nack: noop}:  // at-most-once (D6)
          case <-ctx.Done(): return ctx.Err()
          }
      case <-ctx.Done(): return ctx.Err()
      }
  }
  ```
  Recomputing `next` from `clock.Now()` each iteration means an overrun (slow handler → backpressure on `out`)
  **skips** the missed fire rather than queuing it (Spec D5). `Ack`/`Nack` are no-ops (at-most-once, Spec D6);
  transient handler failure is retried in-process by the runtime `RetryPolicy`.
- **Options:** `WithClock(clockwork.Clock)` (nil = real; ADR 0004), `WithLocation(*time.Location)` (default UTC,
  Spec D7), `WithElector`/`WithLocker` (part 3), `WithCronLogger(*slog.Logger)` (default discard),
  `WithInstanceID(string)` (part 3). **Construction validates** (Spec D8): invalid `spec` → `ErrInvalidSchedule`;
  nil factory → `ErrNilFactory`; both coordinators set → `ErrConflictingCoordinator`.
- Parser: robfig configured for cron 5-field + `@every` + descriptors (ADR 0016 scope). No seconds field in v1
  (Spec N4).

### 2. Coordination SPI — `Elector` + `Locker`, checked on-demand per fire (Spec D9)

```go
type Elector interface { IsLeader(ctx context.Context) (bool, error) }               // gates ALL fires
type Locker  interface { Claim(ctx context.Context, scope string, fire time.Time) (won bool, err error) } // gates ONE fire
```

Both are consulted **synchronously inside the firing loop** at each fire — **no heartbeat / background goroutine**
(goleak-clean). At most one may be configured. **Fail-safe:** a coordinator error is logged and the fire is
**skipped** (never emitted) — a coordination outage must degrade to *no* fire, never to N-fold firing (Spec D9).
Without a coordinator, the source fires on every instance (documented footgun, Spec R4).

### 3. Dependency-free SQL-backed concrete coordinators (`database/sql`, driver injected)

- **`SQLLocker` (per-fire dedup, Spec D10)** — `cron.NewSQLLocker(db *sql.DB, dialect LockerDialect, opts...)`.
  `Claim(scope, fire)` does an idempotent `INSERT (scope, fire_ts)` with `ON CONFLICT DO NOTHING` (PG/SQLite) /
  `INSERT IGNORE` (MySQL) into a fired-keys table; **won = this instance inserted the row**. This is the proven
  `InboxDeduper` mechanism keyed on the deterministic `(scope, fireTime)` — every instance computes the same key,
  exactly one wins. `Purge(ctx, olderThan)` reaps old rows. Recommended primitive: **no failover gap.**
- **`SQLElector` (leader-lease, Spec D11)** — `cron.NewSQLElector(db, dialect ElectorDialect, opts...)`.
  `IsLeader(ctx)` runs an **atomic acquire-or-renew** on a single lease row `(scope PK, holder, expires_at)`:
  set `holder := instanceID, expires_at := db_now + WithLeaseTTL` **iff** absent OR `holder = instanceID` OR
  `expires_at < db_now`; return true iff self now holds it. On-demand (the per-fire call is the renewal) — **no
  heartbeat goroutine.** **Failover latency ≤ lease TTL** (documented; prefer the `Locker` when that gap matters).
- Both use the **DB server clock** (`db_now`) for all time math — skew-free, exactly as `visible_after`/lease
  (ADR 0010). A small `LockerDialect`/`ElectorDialect` seam carries the per-engine SQL (PG/MySQL/SQLite), mirroring
  the existing `InboxDialect`/`LeaseDialect` split; `EnsureSchema`/DDL are opt-in (never implicit DDL on the hot
  path). Distinct `holder`/claimant identity via `WithInstanceID` (default per-process random; Spec D12).

## Consequences

**Positive.**
- A production-grade recurring source with **zero core runtime change** (pure `StreamingSource`), goleak-clean by
  construction, fully fake-clock testable.
- Real **single-fire across N replicas** out of the box, dependency-free — reusing the DB the app already runs, no
  Redis/etcd required. The `Locker` reuses a pattern the codebase already trusts (`InboxDeduper`).
- Rich trigger kinds (cron + `@every` + descriptors) from the accepted parser (ADR 0016).

**Negative / trade-offs.**
- **Leader-election correctness (`SQLElector`) is the hardest surface in the library so far** — atomic
  acquire-or-renew across PG/MySQL/SQLite (MySQL lacks `ON CONFLICT ... WHERE`, needing a conditional `UPDATE` +
  `INSERT` fallback), plus a TTL-bounded failover gap. Mitigated: DB-clock-only, atomic conditional upsert,
  concurrency conformance tests, and a dedicated adversarial-audit focus; the `Locker` is offered as the simpler,
  gap-free primitive.
- **Overrun skips fires** (Spec D5) — a slow handler drops ticks. Documented semantic; the handler must keep up.
- **At-most-once** — a fire is ephemeral; there is no durable replay (durable delayed *send* is Spec 005's
  orthogonal `ScheduledSender`).
- Large increment (source + SPI + two SQL coordinators × 3 dialects + testcontainers) — delivered as one phased
  Plan 011 (user's choice), each phase a green unit.

**Neutral.**
- Coordination interfaces live in `adapter/cron` for now (Spec O6-3); a later extraction to a neutral package is
  possible if they prove generally useful.
- Redis/etcd coordinators and a seconds-field cron are deferred (Spec O6-1/O6-2).

## Alternatives considered

- **Pluggable trigger on the pull-path Poller.** Rejected (Spec N2): no seam exists; would touch
  `consumer.go`/`poller.go`/`flowcontrol.go` (large blast radius) for no gain over a `StreamingSource`.
- **`go-co-op/gocron` scheduler.** Rejected as the engine (ADR 0016 Alternatives): 3 deps + a scheduler goroutine
  fighting goleak/`clockwork`; msgin drives its own loop over robfig's parser.
- **Elector-only (leader) coordination.** Rejected as the *sole* primitive: the per-fire `Locker` is simpler,
  gap-free, and reuses `InboxDeduper`; both ship, `Locker` recommended.
- **Fire-per-instance only (no coordination).** Rejected for v1: the user wants first-class multi-instance
  single-fire shipped now.
