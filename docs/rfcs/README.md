# msgin RFCs — pre-v1 refactor & gap-closure program

A coordinated set of RFCs for one pre-v1 effort: reduce reader/maintainer cognitive load, align with the
EIP / Spring Integration consensus, and close the catalogued pattern gaps. The **refactor** RFCs (0001–0004)
share a **single breaking window** (`v0.x`, SemVer-major intent); the **gap-closure** RFC (0005) is additive
and lands independently — **nothing from it rides the window** (revised 2026-07-27). All share one design language:
**vocabulary + interfaces in the core; implementations and providers in packages named for their EIP chapter;
dependencies point inward.**

Each RFC follows the [RFC template](https://gist.github.com/rowlando/416f41e34fe32840c5634a660df790e1). All five
are **Accepted** — every open question was settled 2026-07-27 and recorded in each RFC's §7 *Decisions*. They
precede the CLAUDE.md artifact chain (spec → plan → ADR); an accepted RFC spawns those.

## Index

| RFC | Title | Scope | Breaking? |
|---|---|---|---|
| [0001](0001-core-package-restructure.md) | Core package restructure | **C-full**: root keeps vocabulary + SPI only; `endpoint`/`routing`/`transform`/`channel`/`resilience` hold every implementation | Yes (**amends nothing** — the flat core was never a recorded decision; see ADR 0027 Context) |
| [0002](0002-eip-alignment.md) | EIP semantic & lexical alignment | Fidelity-audit renames + the `MessageChannel`/`SubscribableChannel` split (a capability fix, not just lexical) + godoc | Yes |
| [0003](0003-endpoint-behavior-types.md) | Endpoint behavior types & provider model | Named func types (`routing.Predicate`, …) with combinator methods; expr becomes a provider **module**; expr dropped from core | Yes (amends ADR 0019) |
| [0004](0004-trigger-scheduling.md) | Trigger-driven scheduling: Poller & scheduled sources | One `Trigger` SPI; **public** `Poller`; dissolve `adapter/cron`; `robfig` stays in the root module | Yes (amends ADR 0017; **ADR 0016 stands**) |
| [0005](0005-eip-gap-components.md) | Fill the EIP scope gaps | Add Idempotent Receiver, Resequencer, Recipient List, Content Enricher, Message Expiration (all five in v1) | Additive, **except** the Resequencer increment, which carries `MessageGroupStore.SettleMembers` |

## The decided package layout

Every RFC places its code here, so read this before any of them. **`[1]` marks what increment 1 (Spec 014 /
Plan 027) actually creates; everything else is a later increment's contribution to the same tree** — annotated
2026-07-27 after audit round 1 (finding L4), because an unannotated end-state layout reads as a move-list and
is not one.

```
msgin/       root (vocabulary + SPI ONLY)
       [1]     Message/Headers, errors, Delivery, Subscription, PayloadCodec,
       [1]     MessageChannel/SubscribableChannel/MessageHandler/Step,
       [1]     PollingSource/EventDrivenSource/OutboundAdapter/RequestReplyExchange,
       [1]     ChannelStore/MessageGroupStore, RetryPolicy/Hooks/BackoffStrategy,
       [1]     RateLimiter/CircuitBreaker/ProbeGate/OverflowPolicy,
       [1]     Chain/To/PayloadOf/Permanent/IsPermanent/RetryAfter/RetryAfterOf/NewID
        4      Trigger + TriggerContext, Elector/Locker
        5      DedupStore
  endpoint/  (EIP ch.10 Messaging Endpoints)
       [1]     Consumer, Producer, Gateway, ChannelExchange, Activate/Consume
        4      Poller (exported), ScheduledSource
        5      IdempotentReceiver, Message Expiration
  routing/   (EIP ch.7 Message Routing)
       [1]     Filter, Router, Split, Aggregator + the behavior types
        5      Resequencer, RecipientList
  transform/ (EIP ch.8 Message Transformation)
       [1]     Transform + Transformer[A,B]
        5      Enrich
  channel/
       [1]     DirectChannel, QueueChannel, PublishSubscribeChannel, PubSub
  resilience/  IMPLEMENTATIONS ONLY — the interfaces above stay in root
       [1]     ExponentialBackoff, NewCircuitBreaker, NewTokenBucket
  trigger/cron/
        4      CronTrigger (robfig — root module)
expr/  ← its own module
       [1]     Predicate / RouteFunc / SplitFunc / Transformer / Correlation / Release providers
```

> **`resilience` holds no interfaces** — corrected 2026-07-27 (audit A5). An earlier version of this block put
> `RateLimiter`/`CircuitBreaker`/`OverflowPolicy` there. That contradicted the program's own "interfaces in the
> core" rule and broke `adapter/memory/queuestore.go:74`, which takes `msgin.OverflowPolicy` in an exported
> signature. See [Spec 014 §3.2](../specs/014-core-package-layout.md) / [ADR 0027 §5](../adrs/0027-core-package-restructure.md).

## How they relate

```
0001 (packages) ──┬── defines the engine's home (endpoint/) used by ────▶ 0004 (Poller, ScheduledSource)
                  ├── defines routing/ + transform/ used by ────────────▶ 0003 (behavior types)
                  └── defines all three homes used by ──────────────────▶ 0005 (the five components)
0002 (naming) ── renames + the channel split land in the same apidiff pass as 0001
0003/0004/0005 ── adopt 0001's package names, so they introduce no new drift
```

- **0001 is the substrate** — its C-full + EIP-chapter decision determines where every other RFC's code is born,
  and its clean-break decision (no facade) governs all of them.
- **0002 is cross-cutting** — its renames ride 0001's package moves in one breaking review. Its
  `MessageChannel` split fixes a *functional* limitation, not only a name.
- **0003 and 0004 are downstream of 0001's packages** and share one dependency-policy rule (below).
- **0005 is additive and last** — **entirely**. `MessageGroupStore.SettleMembers` was briefly routed into the
  window and is now back where it belongs, inside the Resequencer increment that consumes it (revised
  2026-07-27, audit §H1: nothing is tagged and every implementer is in this repo, so the window buys nothing,
  while a real caller is what pins the method's four undecided semantics).

## One rule the RFCs share

**A zero-transitive dependency is pushed to its own module when its weight is material to consumers who do not
use it.** `expr-lang` (7.1 MB, propagates to all seven modules) is — so RFC-0003 gives it a module. `robfig/cron`
(144 KB) is not — so RFC-0004 keeps it in the root module and ADR 0016 stands. Without this rule the two
decisions read as arbitrary; it is recorded in [RFC-0003 §7](0003-endpoint-behavior-types.md).

## Promotion status

| RFC | Promoted to | State |
|---|---|---|
| 0001, 0002, 0003 | [Spec 014](../specs/014-core-package-layout.md), [ADR 0027](../adrs/0027-core-package-restructure.md) / [0028](../adrs/0028-channel-interface-segregation.md) / [0029](../adrs/0029-eip-lexical-alignment.md) / [0030](../adrs/0030-reply-channel-exclusivity-probe.md), [Plan 027](../plans/027-core-package-layout.md) | **Bundle committed; code partially landed.** Three code commits are on `claude/repo-structure-refactor-jt79t1`: `c83dde9` (extract the flat core into `endpoint`/`routing`/`transform`/`channel`/`resilience`), `b6ce7bb` (segregate `MessageChannel`, add `WithSingleSubscriber`, rename `StreamingSource`), `1d7fc80` (goleak net, poll-backoff coverage, `ErrNilSubscription`); the design bundle itself is at **`c4582ba`** (`aae6160` promoted it, `c4582ba` applied round 6). **Remaining: Plan 027 Tasks 9 → 9.5 → 9.6 → 9.7 → 10 → 11 → 12** (9.7 added by round 6's decision D-M). **Seven** adversarial audit rounds have run — records: [round 1](../plans/027-audit-round-1.md), [round 2](../plans/027-audit-round-2.md), [round 6](../plans/027-audit-round-6.md), [round 7](../plans/027-audit-round-7.md); rounds 3–5 are recorded in [`docs/HANDOVER.md`](../HANDOVER.md). **This row names a `HEAD`-adjacent SHA — re-derive it (`git log --oneline`), never copy it** |
| 0004 | — | Increment 2, not yet promoted. One decision (`robfig` stays) is consumed by the increment-1 window |
| 0005 | — | After the window, entirely. `SettleMembers` now lands **with** the Resequencer, not in the window |

**RFC-0003 was folded into the first increment** rather than following it. Its six `*Expr` constructors return
`Step`/`*Router`/`AggregatorOption` — types that move to `routing`/`transform` — so `expr.go` cannot remain in
root once those move, and splitting it across two new packages only to delete it afterwards is throwaway work.
Plan 027 therefore sequences the `*Expr` deletion **first**, before any extraction.

## Sequencing

> **Revised 2026-07-27 — the "non-breaking slices first" strategy is gone.** The earlier plan was to land
> RFC-0003 phase 1 and RFC-0004 phases 2–3 ahead of the window, because they were non-breaking. That was true
> only while those types were going to live in a flat `msgin.` namespace. **RFC-0001 chose C-full**, so they are
> born in `routing`/`transform`/`endpoint` — packages that do not exist until the restructure lands. The program
> is now **one window**, and the mitigation the deferral-risk note below relied on no longer exists.

**The compensating decision: run the window FIRST, ahead of the feature roadmap.** Blast radius is at its
smallest today (87 `msgin.*` symbols referenced by `adapter/`), and every adapter landed first enlarges it.
**Plan 028** (the HTTP SSE gin binding — renumbered from 027, which the window now occupies) and any future
`pgx`/`redis`/`nats` adapter get written directly into the new layout rather than migrated into it.

1. **Split from `main` now.** RFC-0001's moves + RFC-0002's renames and channel split + RFC-0003's behavior
   types and `expr` module, as one apidiff pass and one ADR set. **This is [Plan 027](../plans/027-core-package-layout.md).**
2. **Then increment 2:** RFC-0004 (Trigger, exported `Poller`, dissolve `adapter/cron`), born into `endpoint`.
3. **Then the feature roadmap:** Plan 028 (gin), then RFC-0005's five components incrementally — Idempotent
   Receiver and Recipient List first; the Resequencer carries `MessageGroupStore.SettleMembers` with it.

> **Audit (2026-07-24) — deferral risk compounds.** The roadmap is still *growing* (the gin binding — now
> Plan 028 — next, and
> CLAUDE.md's blueprint still lists **unbuilt** `redis`/`nats`/`pgx` adapters). Every adapter landed before the
> window **enlarges RFC-0001's blast radius** and RFC-0002's rename churn, and the "quiet `main`" to split from
> keeps receding. **Resolved 2026-07-27:** the original mitigation (pull the non-breaking work forward) died
> with the C-full decision, so the risk is addressed the other way — by running the window **first** instead of
> waiting for a quiet `main` that keeps receding.

> **Process note (2026-07-24) — RFCs are a new artifact type. RESOLVED.** CLAUDE.md's *Documentation artifacts*
> section now records `docs/rfcs/`, its 4-digit numbering, the `spec → rfc → adr → plan` promotion path (and the
> `rfc → (spec + adr + plan)` variant this refactor program uses), and the `RFC:` commit trailer. No further
> amendment is needed for RFCs as an artifact class.

## Using this for a refactor session

Read CLAUDE.md, then this index (especially **the decided package layout**), then the RFCs in number order. Each
RFC's **§7 *Decisions*** records what was settled and why — read it before §3, since several §3 passages predate
the decisions and carry dated resolution notes rather than being rewritten. Per CLAUDE.md, promote each RFC to a
spec + ADR(s) + plan, run the adversarial design audit on the assembled bundle, and execute via SDD.
