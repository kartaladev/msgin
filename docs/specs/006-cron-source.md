# Spec 006 — Cron / recurring message source + distributed coordination (Elector/Locker)

- **Status:** Audited — ready for implementation (2026-07-19). Brainstormed with the user; **both** adversarial
  audit rounds complete (the 2-round norm), all findings folded: the Locker's grid-alignment requirement
  (`ErrLockerRequiresGridSchedule`), the unsatisfiable-schedule construction/loop guard, the Elector's scope
  parameter, the demoted-error test provocation (Round-2 corrected NOT-NULL → CHECK constraint), and the
  Medium/Low items. Records: `.superpowers/sdd/plan-011-audit-round-{1,2}.md`. Gated only on an explicit user
  go-ahead before any implementation (CLAUDE.md design-time gate).
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
- N4. *(CLOSED by Plan 012, 2026-07-19 — was: no seconds-field cron in v1.)* Seconds-field cron (6-field) is now
  supported **opt-in** via `WithSeconds()` — see **D13**. The **default is unchanged**: standard 5-field + `@every`
  + descriptors.
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

- **D5 — goleak-clean loop; skip-missed on overrun.** The `Stream` loop **grid-tracks** the schedule: it holds a
  single `next` pointer, seeded once from `schedule.Next(clock.Now())`, and advances it by exactly one step
  (`next = schedule.Next(next)`) as each instant is reached — it does NOT recompute `next` from `clock.Now()` every
  iteration. It `select`s on `clock.After(next.Sub(now))` vs `ctx.Done()` (and on the `out <- delivery` send vs
  `ctx.Done()`). **No background goroutine.** If a slow handler (backpressure on `out`) stalls the loop past one or
  more scheduled instants, the loop advances `next` past every already-elapsed instant WITHOUT emitting, then waits
  on the first still-future one — so missed fires are **skipped, not queued** (standard cron overrun behavior — no
  stampede after a pause). Grid-tracking (vs recompute-from-now) is required for correctness: for a non-grid
  `@every <duration>` schedule, robfig's `ConstantDelaySchedule.Next(t) = t + duration` is relative, so reseeding
  from an arbitrary post-overrun `now` would silently shift the interval's phase; grid-tracking from the prior
  `next` keeps the phase fixed. (Refined during Plan 011 Task 1 — the recompute-from-now form both drifts `@every`'s
  phase and hangs the skip-missed test; the skip-missed *guarantee* is unchanged.) Documented.

- **D6 — At-most-once; no-op Ack/Nack.** A fire is an ephemeral trigger, not a durable row — `Ack`/`Nack` are
  no-ops. Transient handler failures are still retried **in-process** by the runtime's `RetryPolicy` (same
  delivery); a permanent failure routes to the invalid/DLQ sink. Documented as the delivery guarantee.

- **D7 — Injectable clock + timezone.** `WithClock(clockwork.Clock)` (nil = real, no-op — mirrors `memory`);
  `WithLocation(*time.Location)` (default **UTC** — cron specs are TZ-sensitive; UTC is the safe, explicit default).
  All schedule math uses the injected clock's now, in the configured location. A spec-embedded `CRON_TZ=`/`TZ=`
  prefix (robfig-supported) **overrides** `WithLocation` for that spec; `@every` intervals ignore location
  entirely (they are relative durations, not wall-clock instants). Documented on the option's godoc.

- **D8 — Construction-time validation.** `NewSource` parses `spec` once (via a `robfig/cron` parser supporting
  cron + `@every` + descriptors); an invalid spec returns a typed `ErrInvalidSchedule` at construction (the
  debuggability surface), never a deferred failure. A nil factory returns `ErrNilFactory`. Construction also
  validates **satisfiability**, not just parseability: a syntactically valid spec with no future occurrence
  (e.g. `"0 0 30 2 *"`, Feb 30) is refused with `ErrInvalidSchedule` rather than left to hot-loop the firing loop
  at runtime.

- **D9 — Distributed coordination seams (`Elector` + `Locker`), checked on-demand.** Two interfaces in
  `adapter/cron`, both **checked synchronously per fire** (no heartbeat/background goroutine, so goleak-clean):
  ```go
  // Elector gates fires sharing a scope: only the elected leader for that scope
  // emits. IsLeader runs an atomic acquire-or-renew each call, scoped by the
  // caller-supplied scope (symmetric with Locker.Claim — one SQLElector can gate
  // many independent schedules). With on-demand renewal, leadership holds for the
  // duration of the lease TTL but re-elects on every call once the TTL has
  // elapsed since the last call for that scope — NOT unconditionally "sticky":
  // if WithLeaseTTL is shorter than the fire interval, every fire is a fresh
  // election.
  type Elector interface { IsLeader(ctx context.Context, scope string) (bool, error) }
  // Locker gates ONE fire: the instance that claims (scope, fireTime) emits; the
  // rest skip. Deterministic per-fire dedup — REQUIRES a grid-aligned schedule
  // (see D10); refused for @every.
  type Locker interface { Claim(ctx context.Context, scope string, fire time.Time) (won bool, err error) }
  ```
  Injected via `WithElector(e)` / `WithLocker(l)`. When set, the `Stream` loop consults the gate at each fire
  (the Elector branch calls `IsLeader(ctx, s.scope)`, using the Source's own scope) and emits only if it wins; at
  most one gate may be configured (both set → `ErrConflictingCoordinator`). A `Locker` configured against an
  `@every` schedule is refused at construction (`ErrLockerRequiresGridSchedule`, see D10). A coordinator error is
  surfaced (logged via an injected `*slog.Logger`, default discard) and the fire is skipped fail-safe (no
  emit) — a coordination outage must never cause N-fold firing.

- **D10 — SQL-backed concrete `Locker` (dedup-INSERT), grid-aligned schedules only.** `cron.NewSQLLocker(db
  *sql.DB, dialect, opts...)`: on each fire, `INSERT (scope, fire_ts) ... ON CONFLICT DO NOTHING` (PG/SQLite) /
  `INSERT IGNORE` (MySQL) into a fired-keys table; **whoever inserts first wins** and emits. Exactly the proven
  `InboxDeduper` mechanism applied to `(scope, fireTime)`. Dependency-free (`database/sql`, caller-injected
  driver); a small `LockerDialect` seam (PG/MySQL/SQLite) mirrors the existing `InboxDialect` split. A
  `Purge(olderThan)` reaps old rows. DB-clock for `fire_ts` normalization; `fire_ts` is the schedule's fire time
  (deterministic across instances), the dedup key. **This dedup key is instance-invariant ONLY for grid-aligned
  schedules** — standard 5-field cron and `@daily`/`@hourly`/... descriptors, which robfig maps to absolute grid
  instants every instance converges on under bounded clock skew (skew ≪ smallest inter-fire gap). It is **broken
  for `@every <duration>`**: robfig's `ConstantDelaySchedule.Next(t) = t + Delay` is relative to each instance's
  own last-fire/start time, so independent instances compute disjoint, non-colliding fire grids and every `Claim`
  inserts a fresh row — zero dedup, silent N-fold firing. `NewSource` therefore refuses a `Locker` paired with an
  `@every` schedule at construction (`ErrLockerRequiresGridSchedule`) — use the `Elector` (D11) for `@every`.
  **Recommended primitive for grid-aligned (cron/descriptor) schedules — no failover gap.**

- **D11 — SQL-backed concrete `Elector` (leader-lease), scope-parameterized.** `cron.NewSQLElector(db, dialect,
  opts...)`: `IsLeader(ctx, scope)` runs an atomic **acquire-or-renew** against a single leader-lease row
  `(scope PK, holder, expires_at)`: `holder := self, expires_at := db_now + WithLeaseTTL` **iff** the row is
  absent, already held by self, or expired (`holder = self OR expires_at < db_now`) — an atomic upsert
  (PG/SQLite `ON CONFLICT DO UPDATE ... WHERE`; MySQL a conditional `UPDATE` then `INSERT` fallback, verified with
  a **locking** read under the autocommit precondition below). Returns true iff self now holds the lease.
  **On-demand, no heartbeat goroutine** — the per-fire call is the renewal. **Failover latency is bounded by the
  lease TTL** (fires in `[leader-crash, lease-expiry]` are missed; a smaller TTL = faster failover, more
  re-election churn) — documented. **Recommended for `@every` schedules** (no grid-alignment requirement,
  unlike D10); for grid-aligned schedules the `Locker` remains the simpler, gap-free primitive. DB-clock
  (`db_now`) throughout — skew-free. **Precondition:** both `LockerDialect.ClaimFire` and
  `ElectorDialect.AcquireOrRenew` require an autocommitting `Querier` (`*sql.DB`, not a `*sql.Tx`) — the
  per-statement fresh snapshot each atomicity argument relies on (esp. the MySQL three-step acquire-or-renew) is
  load-bearing; documented on both dialect interfaces.

- **D12 — Holder identity is Elector-only.** Only the `Elector` takes a caller instance-id option —
  `WithElectorInstanceID` — for the lease's `holder` identity; default a per-process random id. Two Elector
  instances must not share an id (documented). The `Locker` does **not** record a claimant: a `WithInstanceID`
  option and a `claimed_by` column were implemented in Task 3 and then removed as YAGNI post-implementation —
  the Locker's winner is decided solely by whose `INSERT` succeeds, nothing in the design reads a per-fire
  claimant, and the Elector's `holder` already covers the one case (failover diagnosis) where identity is
  correctness-bearing.

- **D13 — Optional seconds field via `WithSeconds()` (closes N4/O6-2; Plan 012, 2026-07-19).** By default `NewSource`
  parses `spec` with the 5-field standard parser (`Minute|Hour|Dom|Month|Dow|Descriptor`). `WithSeconds()` — a
  no-arg `Option`, consistent with the other `WithX` — switches to a **required 6-field** parser
  (`Second|Minute|Hour|Dom|Month|Dow|Descriptor`), matching robfig's own `cron.WithSeconds()` semantics: the spec
  MUST carry a leading seconds field (e.g. `"*/5 * * * * *"`), and a 5-field spec then returns `ErrInvalidSchedule`
  at construction. **Required, not optional** (rejected `SecondOptional`): a field-count-dependent meaning for the
  same-looking spec is a footgun — one spec, one meaning. `@every` and descriptors still work under `WithSeconds`
  (the `Descriptor` flag is set in both parsers). To pick the parser, `NewSource` applies options **before** parsing
  (a behavior-preserving reorder — invalid-spec is still reported before nil-factory). **All other invariants are
  unchanged and require no code beyond the option + parser selection:** the D7 timezone rules, the D5 grid-tracking
  loop and D8 satisfiability guard, and the D10 `@every`+Locker refusal (a 6-field cron is still grid-aligned, so a
  `Locker` is accepted; `@every` is still refused) all apply as-is; the SQL coordinators (D10/D11) are
  schedule-granularity-agnostic, so **no `dialect.go`/`crontest` change**. **Footgun (documented on `WithSeconds`/
  `doc.go`, not blocking):** a sub-minute schedule paired with a SQL coordinator does one DB round-trip per fire, and
  with the Elector's default 30s `WithLeaseTTL` a sub-30s schedule holds leadership across several fires (still
  single-fire-correct — lower the TTL for faster failover).

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
  while the emit is blocked → the second is skipped); `ctx`-cancel returns promptly (goleak); `ErrInvalidSchedule`
  for both an unparseable spec and a syntactically valid but unsatisfiable one (e.g. `"0 0 30 2 *"`)/
  `ErrNilFactory`; `@every` + descriptor + 5-field specs each fire on schedule; `WithLocation` shifts fire times.
  Gate tests with fake `Elector`/`Locker` (win → emit, lose → skip, error → skip fail-safe);
  `ErrConflictingCoordinator` when both set; `ErrLockerRequiresGridSchedule` when a `Locker` is paired with an
  `@every` schedule. An integration test drives the cron `Source` **through `msgin.NewConsumer[T]` + `Run`**
  (fake clock, a handler recording fires, goleak-clean shutdown) to prove the factory→`New[any]`→runtime→handler
  `T` round-trip, not just direct `Stream` draining. `goleak` (adapter `TestMain`).
- **SQL coordinator conformance** via **testcontainers** (real PG/MySQL/MariaDB/SQLite, using or mirroring the
  existing `harness`/`dbtest` split — `use-testcontainers`): Locker — two concurrent `Claim(scope, sameFire)` →
  exactly one `won=true`; different fire → both win; `Purge` reaps. Elector — concurrent `IsLeader` → exactly one
  leader; leader renews (stays leader within TTL); after simulated expiry (advance/`WithLeaseTTL` short) a second
  instance takes over; DB-clock used (no app-clock). **Every dialect's every branch covered, including the MySQL/
  MariaDB demoted-error branches (`ErrLockerClaimFailed`/`ErrElectorAcquireFailed`), exercised deterministically
  against a malformed test table — not merely asserted via a driver-free fake, which cannot reach the real SQL
  bodies in `dialect.go`.** The dialect SQL is covered ONLY by this real-DB conformance run; a Docker-less run is
  not gate-satisfying for `dialect.go`, so CI MUST run it (mirrors how `adapter/database/sql`'s dialect SQL is
  covered only by `dbtest`).
- **Coverage** ≥85% on every changed package; every hot-path/typed-error branch covered (the coordinator win/lose/
  error paths, the atomic acquire-or-renew truthiness per dialect, the skip-missed branch, the unsatisfiable-
  schedule guard, all sentinels).
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
  SQLite (esp. MySQL's lack of `ON CONFLICT ... WHERE`), split-brain, failover latency = TTL, autocommit-only
  correctness (the `Querier` handed to `AcquireOrRenew`/`ClaimFire` must be a `*sql.DB`, never a `*sql.Tx`).
  Mitigation: DB-clock-only, atomic conditional upsert, a locking verifying read on the MySQL path, concurrency
  conformance tests, adversarial audit focus; the `Locker` (D10) remains the simpler, gap-free primitive — but
  only for grid-aligned schedules (see R6).
- **R3 — Overrun/backpressure (D5).** A slow handler stalls the loop; skip-missed avoids backlog but drops fires.
  Documented as the semantic; the handler must keep up or the schedule must be coarser.
- **R4 — Multi-instance footgun without a coordinator.** With no `Elector`/`Locker`, N replicas fire N times.
  Documented prominently; coordination is opt-in.
- **R5 — Increment size.** Four subsystems (source + SPI + two SQL coordinators × 3 dialects). Mitigation: the
  plan phases it (§6) and may split into Plan 011a/011b; each phase is a green unit.
- **R6 — Locker dedup-key invariant is schedule-dependent (Round-1 audit B-1).** The `(scope, fire_ts)` key is
  instance-invariant only for grid-aligned schedules under bounded clock skew; it is fundamentally broken for
  `@every` (disjoint per-instance grids → zero dedup, silently). Mitigation: `NewSource` refuses a `Locker` +
  `@every` combination at construction (`ErrLockerRequiresGridSchedule`); the `Elector` is documented as the
  `@every` primitive; a construction test covers the refusal.
- **R7 — Unsatisfiable-but-valid schedule (Round-1 audit B-2).** `robfig.ParseStandard` accepts specs with no
  future occurrence (e.g. `"0 0 30 2 *"`); an unguarded firing loop would hot-spin on the resulting zero `Next`.
  Mitigation: `NewSource` probes satisfiability at construction (`ErrInvalidSchedule`) and the `Stream` loop
  additionally parks on `ctx.Done()` if `Next` ever returns zero at runtime (belt-and-suspenders).

## 8. Open items (to close in ADRs + the plan)

- **O6-1 — Redis/etcd-backed coordinators.** Future optional modules implementing `Elector`/`Locker`. Deferred.
- **O6-2 — Seconds-field cron (`WithSeconds`).** ✅ CLOSED by **Plan 012** (2026-07-19) — see **D13** (required
  6-field, opt-in; default unchanged). Records decision in ADR 0017 (addendum).
- **O6-3 — Where the coordination interfaces live.** In `adapter/cron` (consumed there) this increment; if they
  prove generally useful, a later extraction to a neutral package is possible (confirm in ADR 0017).
- **O6-4 — Plan split.** Confirm at planning whether §6 ships as one Plan 011 or splits (011a: source+SPI+Locker;
  011b: Elector). Recommend one plan, phased.
- **O6-5 — SQL coordinator dialect reuse.** Confirm whether the `LockerDialect`/`ElectorDialect` seams live in
  `adapter/cron` standalone, or reuse pieces of `adapter/database/sql` (ADR 0017).

## 9. Traceability

- **Realized by:** Plan 011 (core source + coordination) → `Spec: 006` / `Plan: 011` / `ADR: 0016,0017` trailers;
  **Plan 012** (D13 — `WithSeconds`, closes O6-2/N4) → `Spec: 006` / `Plan: 012` / `ADR: 0017` trailers.
- **Records decisions in:** ADR 0016 (dependency), ADR 0017 (design; + `WithSeconds` addendum for D13).
- **Builds on:** ADR 0002 (inbound SPI: `StreamingSource`/`LiveValueSource`/`Delivery`), ADR 0004 (`clockwork`
  precedent for a core-dep exception), ADR 0010 (`InboxDeduper` dedup pattern reused by the SQL `Locker`).
- **Governing product spec:** Spec 001 §7.
