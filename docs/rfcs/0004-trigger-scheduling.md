# RFC-0004 — Trigger-driven scheduling: Poller & scheduled sources

- **Author:** kartaladev/msgin maintainers
- **Date:** 2026-07-22
- **Status:** Draft
- **Reviewers:** TBD

## 1. Summary

Introduce one `Trigger` SPI (fixed-delay, fixed-rate, once/date, cron) and use it to (a) extract a dedicated
`Poller` from `Consumer`, and (b) dissolve `adapter/cron` — its generic scheduling into runtime, its SQL
Locker/Elector into `adapter/database/sql`, and the `robfig`-bound `CronTrigger` into an isolated package.

## 2. Background & Motivation

Two problems share one root abstraction:

- **`Consumer` clutter.** `poller.go`'s `pollLoop` is a method on `consumer[T]` with hard-coded timing
  (credit-at-fetch → drain-fast on non-empty → idle `pollInterval` on empty). There is no fixed-rate, cron, or
  once trigger for polling.
- **`adapter/cron` is misplaced.** Cron is *timing*, not a bridge to an external system. Its `Source[T]`
  firing loop is generic; only the `robfig.Schedule` is cron-specific; its `Elector`/`Locker` are a general
  coordination SPI; and its `SQLElector`/`SQLLocker` are database adapters (self-contained on stdlib
  `database/sql`, deliberately duplicating a `Querier` to avoid coupling).

Both are the same shape: *a schedule/trigger driving fetch-or-fire.* Design them once (Spring's poller takes
`fixed-delay`/`fixed-rate`/`cron`/`trigger` + `max-messages-per-poll` — this also reduces lexical drift).

## 3. Proposal

### Overview

A pull-form `Trigger` SPI; a first-class `Poller`; a generic `ScheduledSource(trigger, factory, coordinator)`;
the coordination SPI in runtime; SQL coordination in `adapter/database/sql`; `CronTrigger` isolated.

### Detailed Design

```go
type Trigger interface { NextFire(ctx TriggerContext) time.Time } // zero time = stop
type TriggerContext struct { Now, LastScheduled, LastCompleted time.Time; LastPolledCount int; LastErr error }
```

Shipped triggers (core, stdlib+clockwork): **`FixedDelay(d)`** — adaptive: immediate when `LastPolledCount>0`,
else `LastCompleted+d`, **reproducing today's drain-fast/idle-slow exactly**; **`FixedRate(period, initial)`**;
**`Once(at)`**. **`CronTrigger`** (robfig) ships isolated (§4).

- **`Poller`** owns `src`/`trigger`/`maxBatch`/the shared `creditGate`/error-backoff and hands managed
  deliveries to the worker pool. `Consumer` keeps worker pool + settlement. Loop:
  `next := trigger.NextFire(...)`; if zero → stop; `sleepUntil(next)`; acquire credit (≤ maxBatch); poll;
  hand off; on error keep the exponential backoff (overriding the trigger). **Credit-at-fetch is preserved.**
- **`ScheduledSource`** = the generic cron `Source[T]` firing loop (overrun-skip, coordinator gating,
  `EventDrivenSource`) minus the schedule and coordination impl. The old cron `Source` becomes
  `ScheduledSource(CronTrigger, factory, coordinator)`.
- **Coordination SPI** (`Elector`/`Locker`) moves to runtime; it gates *message-generating* triggers only.
  The **grid-schedule↔Locker constraint** (`ErrLockerRequiresGridSchedule`) must be expressed at the
  Trigger↔coordinator boundary (a `Trigger` "grid-aligned fire keys?" capability), not buried in cron.
- **SQL coordination** (`SQLElector`/`SQLLocker`/`LockerDialect`/dialects) moves to `adapter/database/sql`,
  reusing its `Querier` (removes the duplication) and dialect seam.

Dependency graph (inward, acyclic): `CronTrigger`(robfig) → runtime SPI ← `adapter/database/sql`(SQL coord).

### Examples

`NewConsumer(src, h, WithTrigger(FixedRate(time.Minute)))`; `WithPollInterval(d)` becomes sugar for
`WithTrigger(FixedDelay(d))` — default preserved, non-breaking for polling.

## 4. Trade-offs & Alternatives

### Alternatives Considered

- **Drain-fast vs schedule:** each fire = one poll of ≤ `maxBatch`; schedule triggers drain at maxBatch/fire
  (correct for cron/rate), drain-fast retained inside adaptive `FixedDelay`.
- **Credit exhausted at a scheduled fire:** *block* (late but never over-pull; default) vs opt-in
  *try-and-skip* (punctual, overrun-skip).
- **`robfig` boundary:** keep the core robfig-free — `CronTrigger` in its **own module** (like pgx/redis/nats,
  recommended) or a same-module `trigger/cron` subpackage. Alternative (rejected): accept robfig into core and
  re-ratify ADR 0016 — opposite to RFC-0003's direction.

### Trade-offs

`FixedRate`/`Cron` deliberately opt out of drain-fast for a predictable cadence — a feature, stated loudly.
Multi-instance: a Poller over a durable competing-consumer source needs **no** coordination (the source
self-serializes); coordination attaches only to the generating path — the in-process SPI seam lives in
runtime, the durable impl in the DB adapter.

## 5. Implementation Plan

### Phases

1. Joint `Trigger` SPI spec + ADR (supersedes 0010 poller notes; amends 0016/0017). Adversarial audit.
2. Extract `Poller` (behaviour-identical: adaptive `FixedDelay` = today), preserve poll-loop tests. — M
3. Add `FixedRate`/`Once` + `WithTrigger`. — S
4. Move `Elector`/`Locker` SPI to runtime; move SQL coordination to `adapter/database/sql` (dedup `Querier`);
   express the grid constraint at the boundary. — M
5. Extract `CronTrigger` (robfig) to package/module; re-express cron `Source` as `ScheduledSource`; deprecate
   `adapter/cron`; supersede ADR 0016/0017. — M

### Timeline

Sequenced by the shared breaking window (see [index](README.md)); phases 2–3 can be additive/non-breaking,
phases 4–5 move import paths (breaking).

### Success Metrics

Core package deps exclude `robfig`; default polling behaviour byte-identical (poll-loop tests unchanged);
cron overrun-skip / timezone / seconds / unsatisfiable-schedule semantics preserved by the relocated tests.

## 6. Risks & Mitigations

| Risk | Impact | Mitigation |
|---|---|---|
| Regress default drain-fast | Throughput loss | Adaptive `FixedDelay` reproduces today; existing poll-loop tests guard it |
| Break credit-at-fetch invariant | Flood / over-pull | Poller shares the *same* `creditGate`; keep credit-at-fetch untouched |
| Lose grid↔Locker coupling in the move | Silent no-dedup | Express constraint at the Trigger boundary; conformance test |
| robfig leaks into core | Dep-policy breach | `CronTrigger` isolated (own module/subpackage) |
| Relocated SQL/cron semantics drift | Correctness | Relocate behaviour-identical; move tests, don't rewrite |

## 7. Open Questions

1. `CronTrigger` — own module vs same-module subpackage.
2. Export `Poller` publicly, or only expose `WithTrigger`?
3. Trigger set for v1 — `FixedDelay`+`FixedRate`+`Once`+`Cron`, or a subset?
4. Block vs opt-in skip on credit exhaustion at a scheduled fire.
5. Coordination SPI home (confirm it lands with the engine per RFC-0001).

## 8. Appendix

**Appendix A — `adapter/cron` four-way split.** Generic firing loop → runtime (`ScheduledSource`);
`robfig.Schedule` → `CronTrigger` (isolated); `Elector`/`Locker` interfaces → runtime;
`SQLElector`/`SQLLocker`/dialects/`Querier` → `adapter/database/sql`. What remains named "cron" is only the
robfig `CronTrigger`.
