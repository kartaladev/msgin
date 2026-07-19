# Spec 006 — Cron / recurring message source + distributed coordination (Elector/Locker)

- **Status:** Draft (2026-07-19) — brainstormed with the user. Pending the mandatory adversarial audit of the
  full **spec + ADR 0016 + ADR 0017 + plan** bundle, then an explicit go-ahead, before any implementation
  (CLAUDE.md design-time gate).
- **Governing product spec:** [Spec 001 — Messaging core](001-messaging-core.md) §7 (the runtime / `StreamingSource`
  path) and the inbound adapter SPI ([ADR 0002](../adrs/0002-adapter-spi.md)).
- **Records decisions in:** [ADR 0016 — robfig/cron core dependency](../adrs/0016-robfig-cron-dependency.md)
  (the new core dependency) and [ADR 0017 — Cron source + distributed coordination](../adrs/0017-cron-source.md)
  (the design).
- **Realized by:** [Plan 011 — Cron source + coordination](../plans/011-cron-source.md).

## 1. Motivation

msgin can consume from external systems and produce/schedule *outbound* messages (Spec 005 delayed send), but it
has **no way to originate a message on a recurring schedule** — "emit an event every hour", "on `0 9 * * MON-FRI`
kick off a flow". This is the Enterprise Integration **Polling Consumer / Scheduled Producer** shape: a *source*
that fires on a wall-clock schedule and emits a message into a flow, which the existing runtime
(retry/DLQ/flow-control/shutdown) then carries.

The runtime already has the seam: `StreamingSource.Stream(ctx, out chan<- Delivery)` is the event-driven inbound
contract (the `memory` adapter is the reference). A cron source is a `StreamingSource` whose "events" are schedule
fires. No core runtime change is needed.

Running such a source across **N replicas** naively fires N times per tick. Production use needs **single-fire
across instances**, so this increment also ships msgin-native **distributed coordination** — an `Elector`
(leader) and a `Locker` (per-fire) seam, plus a **dependency-free SQL-backed implementation of each** (reusing
the "you already run a database" philosophy that made `visible_after` the right call for Spec 005).

## 2. Goals / Non-goals

### Goals

- G1. A recurring/cron **`Source[T]`** (`adapter/cron`, root module) implementing `StreamingSource` that emits a
  caller-defined message on each schedule fire; driven by the existing runtime via `NewConsumer[T]`.
- G2. Rich trigger kinds: standard 5-field cron, `@every <duration>` intervals, and `@daily/@hourly/...`
  descriptors — via `robfig/cron/v3` (accepted as a core dependency, ADR 0016).
- G3. **goleak-clean by construction** — no background goroutine; a single `ctx`+`clockwork` loop (the
  `memory.Broker.Stream` template). Fully fake-clock testable.
- G4. **Distributed single-fire across N instances** via two msgin-native seams — `Elector` (leader) and `Locker`
  (per-fire) — with a **dependency-free SQL-backed concrete implementation of each** (driver injected, PG/MySQL/
  SQLite dialects).
- G5. Sensible, safe defaults (at-most-once semantics documented; skip-missed on overrun; UTC default; injectable
  clock) — all overridable via `WithX`.

### Non-goals

- N1. **No core runtime change.** The source is a `StreamingSource`; `consumer.go`/`poller.go`/`flowcontrol.go`
  are untouched.
- N2. **No pluggable Poller trigger.** Extending the pull-path Poller with a schedule abstraction is explicitly
  rejected (large blast radius, no existing seam) — the source is push-side (`StreamingSource`).
- N3. **No Redis/etcd-backed coordinators** this increment. Only the dependency-free SQL-backed Elector + Locker
  ship; Redis/etcd impls are future optional modules (O6-1).
- N4. **No seconds-field cron (6-field)** in v1 — standard 5-field + `@every` + descriptors only; `WithSeconds()`
  deferred (O6-2).
- N5. No durable *replay* of missed fires — a fire is an ephemeral trigger (at-most-once); the durable
  delayed-*send* primitive is Spec 005's `ScheduledSender`, a separate, orthogonal concern.
- N6. No general-purpose distributed-lock library surface — the Elector/Locker are scoped to gating cron fires
  (though the SQL impl is written cleanly enough to reuse later).

## 3. Decisions (settled with the user, 2026-07-19)

- **D1 — `robfig/cron/v3` becomes a core dependency (ADR 0016).** The cron `Source` lives in the **root module**
  (`adapter/cron` package, like `adapter/memory`), NOT a separate module — the user's explicit choice. This adds
  `robfig/cron/v3` to the root `go.mod`, so every consumer gets it transitively. Justified because it is a tiny,
  **dependency-free, pure-Go, MIT** parser (burden comparable to `clockwork`/ADR 0004). It is the **third** blessed
  core exception. The alternative (own module isolating the dep, or an in-house parser) was considered and declined.

- **D2 — `StreamingSource`, not a Poller trigger.** `Source[T]` implements
  `Stream(ctx, out chan<- Delivery) error`; runs on the runtime's `Run` goroutine; consumed by the runtime's
  ingest loop. No core change (N1/N2).

- **D3 — Caller-supplied message factory.** `cron.NewSource[T](spec string, factory func(fire time.Time) T,
  opts ...Option)`. On each fire the source builds `msgin.New[any](factory(fireTime), msgin.WithClock(clock))`
  (stamps `msgin.id` + `msgin.timestamp`) and emits it. A fixed payload and a pure "tick" (`T = time.Time`) are
  the trivial factories.

- **D4 — Live values, no codec.** `Source[T]` implements `LiveValueSource` (`EmitsLiveValue() → true`, like
  `memory`): it emits live Go values, so `NewConsumer[T]` pairs it with no codec. The factory's `T` is the
  handler's payload type.

- **D5 — goleak-clean loop; skip-missed on overrun.** The `Stream` loop computes `next := schedule.Next(clock.Now())`
  each iteration and `select`s on `clock.After(next.Sub(now))` vs `ctx.Done()` (and on the `out <- delivery` send
  vs `ctx.Done()`). **No background goroutine.** Because `next` is recomputed from `clock.Now()`, if a slow handler
  (backpressure on `out`) stalls the loop past a scheduled fire, that fire is **skipped, not queued** (standard
  cron overrun behavior — no stampede after a pause). Documented.

- **D6 — At-most-once; no-op Ack/Nack.** A fire is an ephemeral trigger, not a durable row — `Ack`/`Nack` are
  no-ops. Transient handler failures are still retried **in-process** by the runtime's `RetryPolicy` (same
  delivery); a permanent failure routes to the invalid/DLQ sink. Documented as the delivery guarantee.

- **D7 — Injectable clock + timezone.** `WithClock(clockwork.Clock)` (nil = real, no-op — mirrors `memory`);
  `WithLocation(*time.Location)` (default **UTC** — cron specs are TZ-sensitive; UTC is the safe, explicit default).
  All schedule math uses the injected clock's now, in the configured location.

- **D8 — Construction-time validation.** `NewSource` parses `spec` once (via a `robfig/cron` parser supporting
  cron + `@every` + descriptors); an invalid spec returns a typed `ErrInvalidSchedule` at construction (the
  debuggability surface), never a deferred failure. A nil factory returns `ErrNilFactory`.

- **D9 — Distributed coordination seams (`Elector` + `Locker`), checked on-demand.** Two interfaces in
  `adapter/cron`, both **checked synchronously per fire** (no heartbeat/background goroutine, so goleak-clean):
  ```go
  // Elector gates ALL fires: only the elected leader emits. IsLeader runs an
  // atomic acquire-or-renew each call; leadership is sticky while the lease is
  // valid, re-elected when it expires.
  type Elector interface { IsLeader(ctx context.Context) (bool, error) }
  // Locker gates ONE fire: the instance that claims (scope, fireTime) emits; the
  // rest skip. Deterministic per-fire dedup.
  type Locker interface { Claim(ctx context.Context, scope string, fire time.Time) (won bool, err error) }
  ```
  Injected via `WithElector(e)` / `WithLocker(l)`. When set, the `Stream` loop consults the gate at each fire and
  emits only if it wins; at most one gate may be configured (both set → `ErrConflictingCoordinator`). A coordinator
  error is surfaced (logged via an injected `*slog.Logger`, default discard) and the fire is skipped fail-safe (no
  emit) — a coordination outage must never cause N-fold firing.

- **D10 — SQL-backed concrete `Locker` (dedup-INSERT).** `cron.NewSQLLocker(db *sql.DB, dialect, opts...)`: on
  each fire, `INSERT (scope, fire_ts) ... ON CONFLICT DO NOTHING` (PG/SQLite) / `INSERT IGNORE` (MySQL) into a
  fired-keys table; **whoever inserts first wins** and emits. Exactly the proven `InboxDeduper` mechanism applied
  to `(scope, fireTime)`. Dependency-free (`database/sql`, caller-injected driver); a small `LockerDialect` seam
  (PG/MySQL/SQLite) mirrors the existing `InboxDialect` split. A `Purge(olderThan)` reaps old rows. DB-clock for
  `fire_ts` normalization; `fire_ts` is the schedule's fire time (deterministic across instances), the dedup key.

- **D11 — SQL-backed concrete `Elector` (leader-lease).** `cron.NewSQLElector(db, dialect, opts...)`:
  `IsLeader(ctx)` runs an atomic **acquire-or-renew** against a single leader-lease row
  `(scope PK, holder, expires_at)`: `holder := self, expires_at := db_now + WithLeaseTTL` **iff** the row is
  absent, already held by self, or expired (`holder = self OR expires_at < db_now`) — an atomic upsert
  (PG/SQLite `ON CONFLICT DO UPDATE ... WHERE`; MySQL a conditional `UPDATE` then `INSERT` fallback). Returns true
  iff self now holds the lease. **On-demand, no heartbeat goroutine** — the per-fire call is the renewal.
  **Failover latency is bounded by the lease TTL** (fires in `[leader-crash, lease-expiry]` are missed; a smaller
  TTL = faster failover, more re-election churn) — documented; the per-fire `Locker` (D10) has no such gap and is
  the recommended primitive when failover latency matters. DB-clock (`db_now`) throughout — skew-free.

- **D12 — Distinct holder identity.** Both SQL coordinators take a caller `WithInstanceID(string)` (the `holder` /
  claimant identity); default a per-process random id. Two instances must not share an id (documented).

## 4. Architecture

Units, dependency pointing inward (core runtime unchanged; the adapter implements the inbound SPI):

1. **`adapter/cron` — `Source[T]`** (`source.go`): the `StreamingSource`/`LiveValueSource`; robfig-parsed schedule;
   the goleak-clean loop; `WithClock`/`WithLocation`/`WithElector`/`WithLocker`/`WithCronLogger`; `ErrInvalidSchedule`/
   `ErrNilFactory`/`ErrConflictingCoordinator`.
2. **`adapter/cron` — coordination SPI** (`coordinator.go`): `Elector`, `Locker` interfaces; the emit-gating logic
   consumed by `Source`.
3. **`adapter/cron` — SQL coordinators** (`sqllock.go`, `sqlelector.go` + a `dialect.go` seam): `NewSQLLocker` /
   `NewSQLElector`, `LockerDialect` / `ElectorDialect` interfaces, PG/MySQL/SQLite dialect impls, DDL/`EnsureSchema`,
   `Purge`. `database/sql` only (driver injected).

Data flow: runtime `Run` → `Source.Stream(ctx, out)` → each fire: (optional) `gate` check → build
`msgin.New[any](factory(fire))` → `out <- Delivery{Msg, Ack:noop, Nack:noop}` → runtime ingest →
handler/retry/DLQ. Shutdown: `ctx` cancel → `Stream` returns `ctx.Err()` promptly → runtime joins.

## 5. Testing posture (per CLAUDE.md + custom skills)

- **Blackbox `_test` packages**; assert-closure tables; `t.Context()`; `gopls`; start each task from
  `cc-skills-golang:golang-how-to`; TDD.
- **`Source[T]` unit tests** with `clockwork.NewFakeClock()`: a fire emits the factory's message with stamped
  id/timestamp; `clock.Advance` past a fire triggers exactly one emit; **skip-missed** (advance past two fires
  while the emit is blocked → the second is skipped); `ctx`-cancel returns promptly (goleak); `ErrInvalidSchedule`/
  `ErrNilFactory`; `@every` + descriptor + 5-field specs each fire on schedule; `WithLocation` shifts fire times.
  Gate tests with fake `Elector`/`Locker` (win → emit, lose → skip, error → skip fail-safe);
  `ErrConflictingCoordinator` when both set. `goleak` (adapter `TestMain`).
- **SQL coordinator conformance** via **testcontainers** (real PG/MySQL/MariaDB/SQLite, using or mirroring the
  existing `harness`/`dbtest` split — `use-testcontainers`): Locker — two concurrent `Claim(scope, sameFire)` →
  exactly one `won=true`; different fire → both win; `Purge` reaps. Elector — concurrent `IsLeader` → exactly one
  leader; leader renews (stays leader within TTL); after simulated expiry (advance/`WithLeaseTTL` short) a second
  instance takes over; DB-clock used (no app-clock). **Every dialect's every branch covered.**
- **Coverage** ≥85% on every changed package; every hot-path/typed-error branch covered (the coordinator win/lose/
  error paths, the atomic acquire-or-renew truthiness per dialect, the skip-missed branch, all sentinels).
- **No goroutine leaks** — the source starts none; the SQL coordinators start none (synchronous queries).

## 6. Sequencing (the plan may split this — it is large)

1. **ADR 0016** (robfig/cron core dep) + `adapter/cron` scaffold + `Source[T]` (schedule loop, factory,
   clock/location, sentinels) — fake-clock unit tests. No coordination yet.
2. **Coordination SPI** (`Elector`/`Locker` interfaces + `WithElector`/`WithLocker` gating + fail-safe skip) —
   fake-coordinator unit tests.
3. **SQL `Locker`** (dedup-INSERT, `LockerDialect` PG/MySQL/SQLite, DDL, `Purge`) — testcontainers conformance.
4. **SQL `Elector`** (leader-lease acquire-or-renew, `ElectorDialect` PG/MySQL/SQLite, TTL/failover) —
   testcontainers conformance. (Hardest correctness surface — the audit scrutinizes this.)
5. End-to-end example (`NewConsumer` over a cron `Source`) + package doc + whole-branch gate.

## 7. Risks

- **R1 — New universal core dependency (D1).** `robfig/cron/v3` burdens all consumers. Mitigation: it is
  dependency-free/pure-Go/MIT (verify `go.sum` shows no transitive deps at implementation); justified in ADR 0016;
  user-accepted.
- **R2 — Leader-election correctness (D11).** The hardest part: atomicity of acquire-or-renew across PG/MySQL/
  SQLite (esp. MySQL's lack of `ON CONFLICT ... WHERE`), split-brain, failover latency = TTL. Mitigation:
  DB-clock-only, atomic conditional upsert, concurrency conformance tests, adversarial audit focus; the `Locker`
  (D10) is offered as the simpler primitive with no failover gap.
- **R3 — Overrun/backpressure (D5).** A slow handler stalls the loop; skip-missed avoids backlog but drops fires.
  Documented as the semantic; the handler must keep up or the schedule must be coarser.
- **R4 — Multi-instance footgun without a coordinator.** With no `Elector`/`Locker`, N replicas fire N times.
  Documented prominently; coordination is opt-in.
- **R5 — Increment size.** Four subsystems (source + SPI + two SQL coordinators × 3 dialects). Mitigation: the
  plan phases it (§6) and may split into Plan 011a/011b; each phase is a green unit.

## 8. Open items (to close in ADRs + the plan)

- **O6-1 — Redis/etcd-backed coordinators.** Future optional modules implementing `Elector`/`Locker`. Deferred.
- **O6-2 — Seconds-field cron (`WithSeconds`).** Deferred (N4).
- **O6-3 — Where the coordination interfaces live.** In `adapter/cron` (consumed there) this increment; if they
  prove generally useful, a later extraction to a neutral package is possible (confirm in ADR 0017).
- **O6-4 — Plan split.** Confirm at planning whether §6 ships as one Plan 011 or splits (011a: source+SPI+Locker;
  011b: Elector). Recommend one plan, phased.
- **O6-5 — SQL coordinator dialect reuse.** Confirm whether the `LockerDialect`/`ElectorDialect` seams live in
  `adapter/cron` standalone, or reuse pieces of `adapter/database/sql` (ADR 0017).

## 9. Traceability

- **Realized by:** Plan 011 → commits carry `Spec: 006` / `Plan: 011` / `ADR: 0016,0017` trailers.
- **Records decisions in:** ADR 0016 (dependency), ADR 0017 (design).
- **Builds on:** ADR 0002 (inbound SPI: `StreamingSource`/`LiveValueSource`/`Delivery`), ADR 0004 (`clockwork`
  precedent for a core-dep exception), ADR 0010 (`InboxDeduper` dedup pattern reused by the SQL `Locker`).
- **Governing product spec:** Spec 001 §7.
