# RFC-0004 — Trigger-driven scheduling: Poller & scheduled sources

- **Author:** kartaladev/msgin maintainers
- **Date:** 2026-07-22
- **Status:** Accepted (open questions settled 2026-07-27 — see §7)
- **Promoted to:** *not yet promoted* — **increment 2**, after Spec 014 / Plan 027 lands. Deliberately deferred:
  ADR 0027's C-full decision means the exported `Poller` and the `Trigger` SPI are born into `endpoint`, a
  package that does not exist until the restructure ships. One decision of this RFC is nonetheless **consumed
  by** the Spec 014 window: `robfig/cron` **stays** in the root module (§7), which ADR 0029 §6 cites as the
  other half of its dependency-weight rule.
- **Reviewers:** TBD

## 1. Summary

Introduce one `Trigger` SPI (fixed-delay, fixed-rate, once/date, cron) and use it to (a) extract a dedicated,
**publicly exported** `Poller` from `Consumer`, and (b) dissolve `adapter/cron` — its generic scheduling into
`endpoint`, its `Elector`/`Locker` interfaces into root, its SQL Locker/Elector into the
`adapter/database/sql` **per-dialect submodules**, and the `robfig`-bound `CronTrigger` into a `trigger/cron`
subpackage of the root module.

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
the coordination SPI in **root**; SQL coordination in the `adapter/database/sql` per-dialect submodules;
`CronTrigger` isolated in `trigger/cron`.

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
- **Coordination SPI** (`Elector`/`Locker`) moves to **root** (they are interfaces — §7.5); it gates
  *message-generating* triggers only.
  The **grid-schedule↔Locker constraint** (`ErrLockerRequiresGridSchedule`) must be expressed at the
  Trigger↔coordinator boundary (a `Trigger` "grid-aligned fire keys?" capability), not buried in cron.
- **SQL coordination** (`SQLElector`/`SQLLocker`/`LockerDialect`/dialects) moves to `adapter/database/sql`,
  reusing its `Querier` (removes the duplication) and dialect seam.

> **Audit (2026-07-24) — the dialect seam is realized as separate MODULES; decide where the Locker dialects
> land.** `adapter/cron` today keeps its concrete `PostgresLocker`/`MySQLLocker`/`SQLiteLocker` (and matching
> Electors) **monolithic in one root-module file** (`adapter/cron/dialect.go`), and both `adapter/cron` and
> `adapter/database/sql` are part of the **root module**, so the *core* move is within-module (good — `Querier`
> dedup is clean). BUT `adapter/database/sql` realizes its dialect seam as **per-dialect submodules**
> (`postgres`/`mysql`/`sqlite`, each its own `go.mod` — ADR 0011/0012). The plan MUST decide: do the Locker/
> Elector dialect impls follow that convention into the per-dialect submodules (consistent with ADR 0011/0012,
> but changes each consumer's import path), or stay monolithic in the sql core (simpler, but diverges from the
> adapter it's joining)? This is unaddressed in the draft and changes the public import surface either way.
>
> **Resolved 2026-07-27 (§7.6): the per-dialect submodules.** Consistency with ADR 0011/0012 wins — the
> alternative reintroduces engine-specific SQL into the sql core, re-opening a settled decision by the back
> door. The import-path change for current cron-coordination consumers is accepted and goes in `MIGRATION.md`.

Dependency graph (inward, acyclic): `trigger/cron`(robfig) → root SPI ← `adapter/database/sql`(SQL coord);
`endpoint`(Poller, ScheduledSource) → root SPI.

### Examples

`NewConsumer(src, h, WithTrigger(FixedRate(time.Minute)))`; `WithPollInterval(d)` becomes sugar for
`WithTrigger(FixedDelay(d))` — default preserved, non-breaking for polling.

## 4. Trade-offs & Alternatives

### Alternatives Considered

- **Drain-fast vs schedule:** each fire = one poll of ≤ `maxBatch`; schedule triggers drain at maxBatch/fire
  (correct for cron/rate), drain-fast retained inside adaptive `FixedDelay`.
- **Credit exhausted at a scheduled fire:** *block* (late but never over-pull; default) vs opt-in
  *try-and-skip* (punctual, overrun-skip).
- **`robfig` boundary:** `CronTrigger` in its own module, or a same-module `trigger/cron` subpackage
  (**chosen**). Alternative (rejected): accept robfig into core and re-ratify ADR 0016 — unnecessary, since
  ADR 0016 already accepts it and stands unchanged.

> **Audit (2026-07-24) — "own vs same-module" is load-bearing, not stylistic (see Open Question 1).** The core
> *package* is **already** robfig-free: `go list -deps .` today shows no `robfig` — only `adapter/cron` (a
> separate package in the same root module) imports it. The genuine, unrealized goal is removing `robfig` from
> the **root module's `go.mod`** (`go.mod` line 8), which is forced on every root-module consumer. That is
> achieved **only by moving `CronTrigger` to its own module**; a same-module `trigger/cron` subpackage leaves
> `robfig` in the root require and delivers **none** of the dependency-policy benefit. Also: (a) the removed
> "like pgx/redis/nats" analogy pointed at modules that **do not exist yet** — only `memory`, `database/sql`
> (+dialect submodules), `cron`, and `http` are built; (b) this move **reverses ADR 0016**, which deliberately
> accepted `robfig` as a root-module exception, so the window must supersede ADR 0016 **and** amend CLAUDE.md's
> Dependency policy (which still enumerates `robfig` as accepted).

> **Resolution (2026-07-27) — the audit above is accepted as *fact* and rejected as *recommendation*.** Its
> mechanics are correct: only an own-module `CronTrigger` removes `robfig` from the root `go.mod`. The decision
> is nonetheless the **same-module `trigger/cron` subpackage**, because the audit priced the benefit but not the
> cost. Measured: `robfig/cron/v3` has **zero onward edges** in `go mod graph` and occupies **144 KB** — a
> dependency-free, pure-Go leaf. Against that, an own module costs an eighth `go.mod`, a `go.work` entry, a CI
> job, and release-tag choreography, permanently. The dependency-policy rule adopted in **RFC-0003 §7** governs:
> *a zero-transitive dependency is pushed to its own module when its weight is material to consumers who do not
> use it* — 7.1 MB of `expr-lang` is, 144 KB of `robfig` is not.
>
> Consequences, all of which this RFC now reflects: **ADR 0016 stands** (no supersession), **CLAUDE.md's
> dependency policy keeps `robfig` as an accepted core exception** (only the `expr-lang` entry is removed, by
> RFC-0003), and this RFC's "root `go.mod` no longer requires robfig" **success metric is deleted rather than
> restated** — it is now unmeetable by design, and keeping it would leave the RFC failing its own gate.

### Trade-offs

`FixedRate`/`Cron` deliberately opt out of drain-fast for a predictable cadence — a feature, stated loudly.
Multi-instance: a Poller over a durable competing-consumer source needs **no** coordination (the source
self-serializes); coordination attaches only to the generating path — the SPI seam lives in
**root**, the durable impl in the DB adapter's per-dialect submodules.

## 5. Implementation Plan

### Phases

1. Joint `Trigger` SPI spec + ADR (supersedes 0010 poller notes; amends 0016/0017). Adversarial audit.
2. Extract `Poller` (behaviour-identical: adaptive `FixedDelay` = today), preserve poll-loop tests. — M
3. Add `FixedRate`/`Once` + `WithTrigger`. — S
4. Move the `Elector`/`Locker` SPI to **root**; move SQL coordination into the **per-dialect submodules**
   (`postgres`/`mysql`/`sqlite`), reusing `adapter/database/sql`'s `Querier` (removes the duplication);
   express the grid constraint at the Trigger↔coordinator boundary. — M
5. Extract `CronTrigger` (robfig) to the `trigger/cron` **subpackage**; re-express cron `Source` as
   `endpoint.ScheduledSource`; dissolve `adapter/cron`; supersede **ADR 0017** (ADR 0016 stands — §7.1). — M

### Timeline

Sequenced by the shared breaking window (see [index](README.md)); phases 2–3 can be additive/non-breaking,
phases 4–5 move import paths (breaking).

### Success Metrics

> **Metric deleted 2026-07-27.** The draft's operative metric was "root module `go.mod` no longer requires
> `robfig`". The §7.1 decision keeps `robfig` in the root module deliberately, so that metric is now
> unmeetable **by design** and is removed rather than quietly reworded — see the 2026-07-27 resolution in §4 for
> why the benefit did not justify an eighth module.

Default polling behaviour **byte-identical** (the existing poll-loop tests pass unchanged — this is the
load-bearing metric, since adaptive `FixedDelay` must reproduce today's drain-fast/idle-slow exactly); cron
overrun-skip / timezone / seconds / unsatisfiable-schedule semantics preserved by the relocated tests;
`adapter/cron` fully dissolved with nothing left under that path but `trigger/cron`'s `CronTrigger`;
`endpoint.Poller` carries godoc, an `Example` test, and hot-path branch coverage as a newly public type.

## 6. Risks & Mitigations

| Risk | Impact | Mitigation |
|---|---|---|
| Regress default drain-fast | Throughput loss | Adaptive `FixedDelay` reproduces today; existing poll-loop tests guard it |
| Break credit-at-fetch invariant | Flood / over-pull | Poller shares the *same* `creditGate`; keep credit-at-fetch untouched |
| Lose grid↔Locker coupling in the move | Silent no-dedup | Express constraint at the Trigger boundary; conformance test |
| robfig leaks into the core *package* | Dep-policy breach | `CronTrigger` isolated in the `trigger/cron` subpackage; `go list -deps .` on the core package must stay robfig-free. It remains in the root **module** by decision (§7.1), which ADR 0016 already ratifies |
| `Poller` exported before its shape settles | Locked-in API churn | **Accepted risk (§7.2):** `Poller` is public, so its shape enters the SemVer contract at exactly the moment this RFC is reshaping it. Mitigation: land phase 2 (behaviour-identical extraction) and phase 3 (triggers) **before** exporting, so the type is exported in its final shape, not its transitional one |
| Relocated SQL/cron semantics drift | Correctness | Relocate behaviour-identical; move tests, don't rewrite |

## 7. Decisions (settled 2026-07-27)

1. **`CronTrigger` → same-module `trigger/cron` subpackage.** `robfig` stays in the root `go.mod`; ADR 0016
   stands; the robfig success metric is deleted. Full rationale and the deleted metric in §4 and §5.
2. **`Poller` → exported publicly**, alongside `WithTrigger` and the `Trigger` SPI. Callers and adapter authors
   can drive their own polling loops rather than only reaching one through a `Consumer`. Accepted cost and its
   mitigation (export last, in final shape) are in the §6 risk table.
3. **Trigger set → all four: `FixedDelay`, `FixedRate`, `Once`, `Cron`.** `Once(at)` ships despite having no
   Spring equivalent; it is trivial (return the time, then the zero time) and covers one-shot scheduling without
   forcing callers to write a `Trigger`. Its godoc should say plainly that it is a msgin addition, not a
   Spring-parity name, so it does not read as drift.
4. **Credit exhausted at a scheduled fire → block by default, opt-in skip.** The fire waits for credit, then
   polls: late, but never over-pulls and never silently drops a cycle — the conservative default CLAUDE.md's
   sensible-defaults rule requires. `WithSkipOnBackpressure()` opts into punctual cadence. **The trade-off must
   be loud in the godoc:** under sustained backpressure a `FixedRate` or `Cron` poller drifts off the very
   cadence its name promises, which is the property those triggers were chosen for.
5. **Coordination SPI home → root**, not the engine package. RFC-0001's organising principle decides it:
   `Elector` and `Locker` are interfaces, and root holds the vocabulary and SPI. (The draft's "lands with the
   engine" phrasing predates C-full, under which the engine is no longer in root.)
6. **SQL Locker/Elector dialects → the per-dialect submodules** (`adapter/database/sql/{postgres,mysql,sqlite}`),
   following ADR 0011/0012 rather than staying monolithic. A consumer importing the `postgres` submodule gets
   the `Dialect` and the `LockerDialect` from one place. Keeping them in the sql core would have put
   engine-specific SQL back into the package ADR 0011 and 0012 exist to keep free of it. Cost: the import path
   changes for every current cron-coordination consumer — record it in `MIGRATION.md`.

## 8. Appendix

**Appendix A — `adapter/cron` four-way split** (destinations updated 2026-07-27):

| Piece today | Destination |
|---|---|
| Generic firing loop (overrun-skip, coordinator gating) | `endpoint.ScheduledSource` |
| `robfig.Schedule` binding | `trigger/cron.CronTrigger` (root module, §7.1) |
| `Elector` / `Locker` interfaces | **root** (they are SPI — §7.5) |
| `SQLElector` / `SQLLocker` / `LockerDialect` | `adapter/database/sql` core (the generic half), reusing its `Querier` |
| `PostgresLocker` / `MySQLLocker` / `SQLiteLocker` + Electors | the **per-dialect submodules** (§7.6) |

What remains named "cron" is only the robfig `CronTrigger`.
