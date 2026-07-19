# Spec 005 — Scheduled / delayed send (ScheduledSender capability + producer sugar)

- **Status:** Draft (2026-07-19) — brainstormed with the user. Pending the mandatory adversarial audit of the
  full **spec + ADR 0015 + plan** bundle, then an explicit go-ahead, before any implementation (CLAUDE.md
  design-time gate).
- **Governing product spec:** the messaging core, [Spec 001 — Messaging core](001-messaging-core.md) §9 (the
  `sql` adapter and its `visible_after`/delayed-visibility storage) and §7 (the runtime produce path).
- **Builds on:** [ADR 0002 — Adapter SPI](../adrs/0002-adapter-spi.md) (the `OutboundAdapter` SPI and the
  optional-capability-interface pattern) and [ADR 0010 — Poller + `database/sql` adapter](../adrs/0010-poller-sql-adapter.md)
  (the `LeaseDialect.Insert(..., delay)` / `visible_after` machinery this feature exposes).
- **Records decisions in:** [ADR 0015 — Scheduled / delayed send](../adrs/0015-scheduled-send.md).
- **Realized by:** [Plan 010 — Scheduled / delayed send](../plans/010-scheduled-send.md).

## 1. Motivation

The `sql` adapter already stores a `visible_after` timestamp on every message and its claim query skips rows
whose `visible_after` is still in the future — this is a complete, durable **delayed-visibility** mechanism,
proven across PostgreSQL, MySQL, and SQLite. `LeaseDialect.Insert(ctx, q, table, msgID, headers, payload,
delay)` writes `visible_after = <db-now> + delay`; the Poller's `Claim`/`ClaimLock` filter `visible_after <=
<db-now>`. **The only place this is discarded is `Outbound.Send`, which hardcodes `delay = 0`** (immediately
visible).

So the *storage* primitive for scheduled delivery already ships; the *only* gap is a **public, typed producer-side
surface** to set a delay. This spec adds that surface — a message can be published to become consumable only
after a delay (or at a wall-clock target) — as a **purely additive** capability, with **no DDL and no
dialect-SQL change**.

This realizes the Enterprise Integration **Delayer** pattern's *intent* (store a message and forward it later),
backed by the durable store msgin already has. The EIP-*named* Delayer composition endpoint is an explicit
future increment (§2 Non-goals, §8 O5-1), layered over the capability this spec defines.

## 2. Goals / Non-goals

### Goals

- G1. A durable **relative** delayed send — deliver a message no earlier than `now + delay` — exposed as a
  first-class typed producer operation, skew-free (the DB computes `now + delay`).
- G2. An **absolute** scheduled send — deliver at wall-clock time `t` — as ergonomic producer-side sugar over G1.
- G3. **Fail loud** when the sink cannot schedule: a typed, `errors.Is`-able error, never a silent
  immediate-delivery. (Sensible-defaults / no-silent-footgun rule.)
- G4. Follow the **existing optional-capability-interface pattern** (`NativeReliability`, `LiveValueSource`,
  `ProbeGate`, `LockDialect`) so the shape is idiomatic to this codebase and future adapters extend it uniformly.
- G5. Purely additive and backward compatible: absent any scheduling call, every existing produce/consume path
  behaves exactly as today.

### Non-goals

- N1. **No DDL or dialect-SQL change.** The `visible_after` column, index, and DB-clock arithmetic already
  exist; this feature only threads a caller-supplied `delay` into the existing `Insert`.
- N2. **No EIP-named `Delayer` composition endpoint** in this increment (deferred — O5-1). The capability is the
  mechanism; a thin `Delayer` step over it is a later increment.
- N3. **No memory-adapter delayed send** in this increment (deferred — O5-2). The in-memory `Broker` is the
  minimal reference/test double; timed release would add a clock, a goroutine lifecycle, and a `Close()` to it.
  Scheduling to memory **fails loud** (`ErrScheduledSendUnsupported`), not silently-immediate.
- N4. **No absolute time in the SPI.** The adapter capability is relative-only (`SendAfter(delay)`); absolute
  time is a core/producer concern (needs an app clock), never pushed into the DB-clock-pure adapter.
- N5. No change to the retry/Nack delayed-redelivery path (that already threads a delay through
  `LeaseDialect.Nack`); this spec is producer-side only.
- N6. No cross-adapter scheduling for pgx/redis/nats/http (those adapters are still pended); each implements the
  capability when it lands, or documents that it cannot.
- N7. **No recurring / cron-triggered message *production*** in this increment. That is a distinct, *source-side*
  capability ("emit a message every hour / on a cron expression"), not producer-side one-shot delayed send. It is
  deferred to a future optional adapter (O5-5). Rationale (researched 2026-07-19): a durable one-shot delayed send
  needs restart-durability + cross-instance exactly-once, which the DB `visible_after` + `SKIP LOCKED` primitive
  already provides for free; an in-process job scheduler (e.g. `go-co-op/gocron`) persists nothing and would lose a
  pending send on restart, and needs an external Redis/etcd/SQL locker for exactly-once — the wrong tool for *this*
  feature. Recurring triggers, by contrast, are ephemeral (resume ticking on restart) and are a good fit for such a
  library, but only as an **optional isolated module** (O5-5).

## 3. Decisions (settled with the user, 2026-07-19)

- **D1 — Shape: optional capability SPI, not a header.** A new core interface `ScheduledSender` in `spi.go`,
  discovered by type-assertion, mirroring `NativeReliability`/`LockDialect`. Chosen over a behavior-changing
  header after researching existing-code precedent: the capability-type-assertion pattern is used in ~4
  load-bearing sites (`consumer.go`, `producer.go`, `source.go`) with a documented "add-your-own" recipe
  (`producer.go`), whereas caller-set behavior-changing headers have **no precedent** and would **leak the delay
  header onto the delivered message** (sql framing round-trips all headers with no strip mechanism). The
  capability shape is the stronger analogy and avoids the leak.

  ```go
  // spi.go — new optional capability
  type ScheduledSender interface {
      SendAfter(ctx context.Context, msg Message[any], delay time.Duration) error
  }
  ```

- **D2 — SPI carries the relative primitive only.** `SendAfter(delay)` is the skew-free operation an adapter can
  honor against its own store (the DB computes `now + delay`). Absolute time is **not** in the SPI (N4).

- **D3 — `sql.Outbound` implements `ScheduledSender`.** `SendAfter` threads `delay` into the existing
  `dialect.Insert(..., delay)`; the current `Send` is refactored to `SendAfter(ctx, msg, 0)` (behavior-preserving).
  A compile-time assertion `var _ msgin.ScheduledSender = (*Outbound)(nil)` documents the capability, alongside
  the existing `var _ msgin.OutboundAdapter`.

- **D4 — Typed producer entry points + absolute sugar.** The core typed producer gains two methods:
  - `SendAfter(ctx, msg Message[T], delay time.Duration) error` — encodes `T→[]byte` exactly as `Send` does,
    **type-asserts** the wrapped sink to `ScheduledSender`, and calls it. If the sink does not implement it →
    `ErrScheduledSendUnsupported` (D6).
  - `SendAt(ctx, msg Message[T], t time.Time) error` — **core-side** sugar: `delay = t.Sub(clock.Now())`, then
    `SendAfter`. `SendAt` is not in the SPI (N4).

- **D5 — Clock injection for `SendAt`.** A new producer option `WithProducerClock(clockwork.Clock)` (mirroring
  `WithConsumerClock`, ADR 0007 D10, to avoid colliding with the message-level `WithClock`) makes `SendAt`
  testable with a fake clock. Default: `clockwork.NewRealClock()`. The **adapter stays DB-clock-pure** — no clock
  is added to `sql` (confirmed: the adapter injects none; all timestamp math is DB-server-side).

- **D6 — Unsupported sink → typed error.** New core sentinel `ErrScheduledSendUnsupported`
  (`errors.New("msgin: ...")`, `errors.Is`-able), produced by the producer's type-assert miss — exactly the
  shape of the sql adapter's `ErrLockStrategyUnsupported` (a type-assertion failure → typed error). No silent
  immediate delivery (G3).

- **D7 — Negative / past delay clamps to 0 (deliver now), documented.** A `SendAfter` with `delay <= 0`, or a
  `SendAt(t)` where `t` is already in the past, delivers immediately (delay 0). "Scheduled in the past = send
  now" is the least-surprising semantic and avoids footgunning a `SendAt` a few milliseconds late into a typed
  error. Clamping is documented on both godocs. (The alternative — a typed `ErrNegativeDelay` — was considered
  and rejected as a footgun for the common slightly-late `SendAt`.)

- **D8 — Skew semantics documented as a contract.** `SendAfter(delay)` is **fully skew-free** (relative; DB
  computes `now_db + delay`). `SendAt(t)` converts absolute→relative with the app clock, so it is realized on the
  DB clock as `t + (db_clock − app_clock)` — i.e. it inherits app↔DB skew on the absolute anchor. This caveat is
  documented on `SendAt`'s godoc; callers needing exactness use `SendAfter`.

## 4. Architecture

Three small, well-bounded units, dependency pointing inward (core defines the SPI; the adapter implements it):

1. **Core SPI (`spi.go`)** — `ScheduledSender` interface (D1). Depends on nothing new.
2. **Core producer (`producer.go` + `errors.go`)** — `SendAfter`/`SendAt` methods, `WithProducerClock` option,
   `ErrScheduledSendUnsupported` sentinel, an injected `clockwork.Clock` (default real). Consumes `ScheduledSender`
   by type-assertion, exactly as it already consumes `LiveValueSource` via `isLiveValue`.
3. **sql adapter (`adapter/database/sql/outbound.go`)** — `Outbound.SendAfter` threads `delay` into
   `dialect.Insert`; `Send` becomes `SendAfter(..., 0)`. No dialect, DDL, or framing change.

Data flow (durable relative send): `producer[T].SendAfter(ctx, Message[T], delay)` → encode `T→[]byte` →
`sink.(ScheduledSender).SendAfter(ctx, Message[any], delay)` → `Outbound.SendAfter` →
`dialect.Insert(..., delay)` → row with `visible_after = db_now + delay` → Poller's `Claim` skips it until due →
delivered to the consumer at/after `visible_after`.

Absolute send: `producer[T].SendAt(ctx, msg, t)` → `delay = max(0, t − clock.Now())` → the relative path above.

## 5. Testing posture (per CLAUDE.md + custom skills)

- **Blackbox `_test` packages**; assert-closure tables (`table-test`); `t.Context()`; `gopls`; start each task
  from `cc-skills-golang:golang-how-to`; TDD red→green.
- **Core producer unit tests** with a **fake `ScheduledSender`** (records the delay it received) and a
  `clockwork.NewFakeClock()`:
  - `SendAfter` forwards the exact delay; encodes the payload; propagates the sink's error.
  - `SendAt` math: `delay = t − now`; **past `t` clamps to 0** (D7); fake-clock advance changes the computed delay.
  - **`ErrScheduledSendUnsupported`**: a plain `OutboundAdapter` (no capability) → `SendAfter`/`SendAt` return it
    (both hot-path error branches covered).
  - `SendAfter` with `delay <= 0` clamps to 0 (D7) and still delivers.
- **sql conformance** via the existing `harness` + `dbtest` (real PostgreSQL / MySQL / SQLite, testcontainers /
  embedded per the existing split): a delayed row is **invisible before** `visible_after` and **visible after**;
  drive one case end-to-end through `NewConsumer` to prove the produce→claim→deliver timing. `delay = 0` parity
  with today's `Send`. Reuse `harness` helpers; do not spin up ad-hoc containers (`use-testcontainers`).
- **Coverage**: ≥85% on every changed package; **every hot-path/typed-error branch** covered — the
  type-assert-miss (`ErrScheduledSendUnsupported`), the `SendAt` past-clamp, the `delay<=0` clamp, the
  encode-error propagation, the sink-error propagation.
- **No goroutine leaks** (`goleak`, already wired) — this feature starts **no goroutine** (the delay lives in the
  DB row, not an in-process timer).

## 6. Sequencing

Small, ordered, each a green unit (details in Plan 010):

1. Core SPI + producer surface: `ScheduledSender`, `ErrScheduledSendUnsupported`, `WithProducerClock`, injected
   clock, `producer.SendAfter`/`SendAt` (+ fake-`ScheduledSender` unit tests). No adapter dependency.
2. sql `Outbound.SendAfter` (+ `Send` delegates to it; compile-time capability assertion). Unit/round-trip.
3. sql conformance via `harness`/`dbtest` (delayed-visibility timing across engines) + a `NewConsumer`
   end-to-end example, package-doc paragraph, whole-branch gate.

## 7. Risks

- **R1 — Absolute-time skew (D8).** `SendAt` is DB-clock-skewed on the anchor. Mitigation: documented contract;
  `SendAfter` offered as the exact primitive. Not a correctness bug.
- **R2 — Silent immediate delivery.** Fully avoided by D6 (`ErrScheduledSendUnsupported`) — an unsupported sink
  is loud, never a silent delay-drop.
- **R3 — Producer API surface growth.** Two methods + one option added to the producer. Small, additive, mirrors
  existing option/method patterns; no breaking change to `Send`/`NewProducer`.
- **R4 — `Send`→`SendAfter(...,0)` refactor in sql.** Behavior-preserving; guarded by the existing `Send`
  round-trip tests plus a `delay=0` parity assertion.
- **R5 — Memory fails loud (N3).** A caller expecting memory to delay gets `ErrScheduledSendUnsupported`. This is
  intended and documented; memory delayed-send is a named future increment (O5-2).

## 8. Open items (to close in ADR 0015 + the plan)

- **O5-1 — EIP-named `Delayer` composition step.** A thin composition-layer endpoint (`Delayer`/`Delay`) over
  the capability, for the EIP-named surface. **Deferred**; the capability is sufficient this increment.
- **O5-2 — Memory-adapter delayed send.** Give the `Broker` timed release (clock + goroutine + `Close()`).
  **Deferred**; sql-only now.
- **O5-3 — Where exactly `SendAfter`/`SendAt` live — RESOLVED (ADR 0015 Decision 3).** They widen the exported
  `Producer[T]` **interface** (implemented on `*producer[T]`), not a separate `ScheduledProducer[T]` type, because
  scheduling support is a runtime property of the sink — a separate compile-time type would give false assurance.
  This widens an exported interface (breaking only for external *implementers*; acceptable pre-1.0, no known
  implementers, minor bump; SemVer note in ADR 0015). A standalone `ScheduledSender`-typed sink call for
  pre-encoded `[]byte` is unnecessary: a caller holding the concrete `*sql.Outbound` (which implements
  `ScheduledSender`) can call `SendAfter` directly.
- **O5-4 — Clamp vs error final wording (D7).** Confirm clamp-to-0 in the ADR; document precisely on both godocs.

- **O5-5 — Recurring / cron-triggered source adapter (optional module).** A future increment: a *source-side*
  scheduled/recurring message producer with rich trigger kinds (cron expressions, fixed/random interval,
  daily/weekly/monthly), as an **optional isolated module** (own `go.mod`, per ADR 0003) so its dependencies burden
  only importers, never the core. Candidate backend `github.com/go-co-op/gocron/v2` (MIT; runtime deps
  `google/uuid` + `robfig/cron/v3` + `jonboulle/clockwork` — the last already ours; researched 2026-07-19). Its
  non-durable in-memory model is acceptable there because recurring triggers are ephemeral (resume on restart). To
  be specced/ADR'd/audited on its own; explicitly **out of scope** for Spec 005. May instead extend msgin's
  existing **Poller** trigger abstraction — a decision for that spec.

## 9. Traceability

- **Realized by:** Plan 010 → commits carry `Spec: 005` / `Plan: 010` / `ADR: 0015` trailers.
- **Records decisions in:** ADR 0015.
- **Builds on:** ADR 0002 (OutboundAdapter SPI + optional-capability pattern), ADR 0010 (`visible_after` /
  `LeaseDialect.Insert(delay)`), ADR 0007 D10 (`WithConsumerClock` naming precedent), ADR 0004 (`clockwork`).
- **Governing product spec:** Spec 001 §7, §9.
