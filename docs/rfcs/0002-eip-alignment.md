# RFC-0002 — EIP semantic & lexical alignment

- **Author:** kartaladev/msgin maintainers
- **Date:** 2026-07-22
- **Status:** Accepted (open questions settled 2026-07-27 — see §7)
- **Promoted to:** [Spec 014](../specs/014-core-package-layout.md) ·
  [ADR 0028](../adrs/0028-channel-interface-segregation.md) (channel segregation) ·
  [ADR 0029](../adrs/0029-eip-lexical-alignment.md) (renames) · [Plan 027](../plans/027-core-package-layout.md)
- **Reviewers:** TBD

## 1. Summary

An audit of all 35 implemented components against Hohpe & Woolf's *Enterprise Integration Patterns* and Spring
Integration found high fidelity with drift concentrated in ~4 names. This RFC proposes the renames and godoc
fixes that keep the library on the established vocabulary, to land in the same breaking window as RFC-0001/0003.

## 2. Background & Motivation

The stated goal is not drifting from well-known consensus. Names are a contract: a Spring-trained reader
should recognise our types. Fidelity is already strong — the Aggregator mirrors Spring's
`MessageGroupStore`/`MessageGroup`/`CorrelationStrategy`/`ReleaseStrategy` almost 1:1; the Filter's
`WithDiscardChannel` mirrors `discard-channel`; the Splitter stamps the canonical sequence headers; and
Invalid-Message vs Dead-Letter are correctly kept as distinct sinks. The audit (Appendix A) isolates the few
divergences worth fixing.

## 3. Proposal

### Overview

Fix the drift register (ranked); leave the exemplary parts untouched.

### Detailed Design

**Breaking renames (do in the window):**
- `StreamingSource`/`Stream` → **`EventDrivenSource`** — canonical EIP term is *Event-Driven Consumer*
  (Spring `EventDrivenConsumer`). Clearest drift.
- `MessageChannel` interface segregation — today it bundles `Send` + `Subscribe`; Spring splits
  `MessageChannel` (send) / `SubscribableChannel` (subscribe) / `PollableChannel` (receive). Split ours
  accordingly so the name means send-only and `PublishSubscribeChannel` can satisfy the subscribe contract
  (it currently cannot — its `Subscribe` returns a `Subscription`).

  > **Audit (2026-07-27) — this is a capability defect, not just a naming one.** Measured against the code:
  > `MessageChannel` is satisfied by **exactly one type in the repo** — `DirectChannel` (the only
  > `var _ MessageChannel` assertion outside a test fake). `QueueChannel` has `Send`+`Poll` (it is a
  > `PollingSource`, not a `MessageChannel`); `PublishSubscribeChannel` has `Send`+`Subscribe`→`(Subscription,
  > error)`. So `MessageChannel` is not a channel abstraction — it is `DirectChannel`'s shape wearing the
  > general name. Every consumer of the type (`WithDiscardChannel`, `WithDefaultChannel`, the router's `pick`
  > return, `NewChannelExchange`'s `request`) calls **only `Send`** — verified, no call site subscribes — yet
  > the interface demands `Subscribe`. Consequence today: **you cannot discard to, route to, or exchange over a
  > `QueueChannel` or a `PublishSubscribeChannel`.** The segregation therefore *fixes a functional limitation*,
  > which raises its priority above a lexical fix.
  >
  > The one genuine subscriber is `exchange.go:247`, which calls `reply.Subscribe(...)` — so `ChannelExchange`
  > needs both shapes and is the concrete customer proving `SubscribableChannel` earns its keep.
- `Exchange`/`ChannelExchange` — decide: keep (qualified as `RequestReply*`) or rename away from the
  AMQP-overloaded "exchange". If kept, add a disclaiming godoc line.

**Non-breaking godoc fixes (anytime):**
- Name the in-process request-reply pattern *Correlation Identifier*, with *Return Address* as the distributed
  seam (aligns with the multi-instance rule; ADR 0022).
- Document `DirectChannel`'s deliberate single-subscriber restriction vs Spring's load-balanced
  multi-subscriber; competing consumers come via the worker pool.

**Keep (consistent, low-risk):** `Transform` (godoc already says "Message Translator"), `Consume`, `PubSub`,
`Router`, `Filter`, `Split`, `Aggregator`.

### Examples

`FilterPredicate` vs Spring's `MessageSelector` — **settled 2026-07-27 as neither**: the type is
`routing.Predicate[A]`. Once RFC-0001 puts it in a package named `routing`, both candidates repeat what the
package already says. The general rule adopted for all six behavior types is *let the package carry the
qualifier*, with each godoc naming its Spring equivalent so a Spring-trained reader still finds it. Full name
set in RFC-0003 §7.

## 4. Trade-offs & Alternatives

### Alternatives Considered

- **Rename** (chosen for items 1–2): highest consensus value, breaking.
- **Keep + document the mapping in godoc**: non-breaking, but the name still misleads at call sites.

### Trade-offs

Renames are breaking → they must ride the shared window; done piecemeal they cost extra majors. The godoc-only
fixes are free and can precede the window.

## 5. Implementation Plan

### Phases

1. ADR recording the naming decisions.
2. Land breaking renames alongside RFC-0001's package moves (one `apidiff` review covers both).
3. Land godoc fixes independently.

### Timeline

Sequenced by the shared breaking window (see [index](README.md)); godoc fixes anytime.

### Success Metrics

Every 🟠 item in the drift register resolved or explicitly kept-with-rationale; no new drift introduced by
RFC-0003/0004's new names.

## 6. Risks & Mitigations

| Risk | Impact | Mitigation |
|---|---|---|
| Rename churn across adapters/examples | Wide diff | Bundle with RFC-0001's moves; `gopls` rename. **Audit (2026-07-24):** churn scales with the corrected **87** adapter-referenced `msgin.*` symbols (see RFC-0001), not ~40 — size the apidiff/review pass for that. |
| Renamed symbols documented in CLAUDE.md | Stale docs / traceability breach | The window must also update CLAUDE.md's Architecture-blueprint naming (e.g. `StreamingSource`→`EventDrivenSource`) in the same commit, per the traceability rule |
| Interface segregation breaks implementers | Compile breaks downstream | Pre-v1; document in `MIGRATION.md` |
| Over-aligning to Spring hurts Go ergonomics | Awkward API | Keep Go-idiomatic names where clearer. **Applied 2026-07-27:** the six behavior types drop their `Message*`/`*Predicate` qualifiers because RFC-0001's packages already carry them (`routing.Predicate`, not `routing.MessageSelector`); each godoc names the Spring equivalent so recognition survives the divergence |
| Package name itself misfiles patterns | Drift introduced by the alignment RFC | **Audit (2026-07-27):** RFC-0001's draft `endpoint` package held filter/router/splitter/aggregator/transformer, which EIP files under ch.7 Routing and ch.8 Transformation — fixed by adopting EIP-chapter package names (`endpoint`/`routing`/`transform`), see RFC-0001 §4 |

## 7. Decisions (settled 2026-07-27)

1. **`Exchange` → keep, qualified.** Root keeps `RequestReplyExchange`; the in-process implementation is
   `endpoint.ChannelExchange`, with a godoc line disclaiming the AMQP meaning. Rationale: Spring Integration
   names its own equivalent interface `RequestReplyExchanger`, so "exchange" here is the consensus term rather
   than drift. **The ADR must cite that source explicitly** rather than assert it — if the citation does not
   hold up, this decision reverts to a rename.
2. **`FilterPredicate` vs `MessageSelector` → neither; `routing.Predicate[A]`.** See §3 Examples above and
   RFC-0003 §7 for the full six-type set.
3. **`MessageChannel` split → now, in this window.** RFC-0001 chose C-full, which removes the reason to defer
   it. Decided shape — **two interfaces, not Spring's three:**

   ```go
   type MessageChannel interface {
       Send(ctx context.Context, msg Message[any]) error
   }

   type SubscribableChannel interface {
       MessageChannel
       Subscribe(h MessageHandler) (Subscription, error)
   }
   ```

   Unifying `Subscribe` on `(Subscription, error)` makes `DirectChannel` **and** `PublishSubscribeChannel` both
   satisfy `SubscribableChannel` (today neither satisfies `MessageChannel`), so an exchange reply channel may be
   either. `DirectChannel.Subscribe` changes signature — breaking, in-window.

   **`PollableChannel` is deliberately omitted.** It would duplicate the existing `PollingSource` SPI's exact
   method set (`Poll(ctx, max) ([]Delivery, error)`, already implemented by `QueueChannel`) and **no signature in
   the library would take one**. Per CLAUDE.md's small-surface rule it must earn its keep; it can be added later
   non-breakingly if a caller ever appears. This is a deliberate, documented divergence from Spring's three-way
   split — recorded here so it reads as a decision, not an oversight.

   Call sites narrow honestly to send-only: `WithDiscardChannel`, `WithDefaultChannel`, the router's `pick`
   return, and `NewChannelExchange`'s `request` take `MessageChannel`; its `reply` takes `SubscribableChannel`.

## 8. Appendix

**Appendix A — fidelity scorecard (abridged).** ✅ faithful unless noted. Message/Headers ✅; Correlation Id ✅;
Sequence headers ✅; DirectChannel 🟡 (single-subscriber); QueueChannel ✅; PublishSubscribeChannel ✅;
Dead-Letter ✅; Invalid-Message ✅; Channel Adapter ✅; Polling Consumer ✅; **Event-Driven Consumer 🟠
(`StreamingSource`)**; Competing Consumers ✅; Service Activator ✅ (`Activate`/`Consume`); Messaging Gateway ✅;
Content-Based Router ✅; Message Filter ✅; Splitter ✅; Aggregator ✅✅; Message Translator 🟡 (`Transform`);
Request-Reply 🟡 (`Exchange`); **`MessageChannel` bundles Send+Subscribe 🟠**. Scope gaps (deliberate, seams
present): Resequencer, Recipient List, Content Enricher, Message Expiration, Idempotent Receiver.

> **Drift register — disposition (2026-07-27).** Every 🟠/🟡 item is now resolved or explicitly kept, which is
> this RFC's success metric:
>
> | Item | Disposition |
> |---|---|
> | Event-Driven Consumer 🟠 (`StreamingSource`) | **Renamed** → `EventDrivenSource` (in-window) |
> | `MessageChannel` bundles Send+Subscribe 🟠 | **Split** → `MessageChannel`/`SubscribableChannel` (§7.3); also fixes a capability defect |
> | Request-Reply 🟡 (`Exchange`) | **Kept, qualified** + disclaiming godoc (§7.1) — Spring uses `RequestReplyExchanger` |
> | Message Translator 🟡 (`Transform`) | **Kept** — godoc already says "Message Translator"; now lives in the `transform` package, which carries the chapter name |
> | DirectChannel 🟡 (single-subscriber) | **Kept** + godoc documenting the deliberate restriction vs Spring's load-balanced multi-subscriber; competing consumers come via the worker pool |
>
> All five scope gaps are addressed by **RFC-0005** (accepted 2026-07-27, all five in v1), so the "deliberate
> gaps" line below is now a roadmap, not a permanent exclusion.

**Sources:** EIP catalog (enterpriseintegrationpatterns.com/patterns/messaging/); Spring Integration channel
reference (docs.spring.io/spring-integration/reference/channel/implementations.html).
