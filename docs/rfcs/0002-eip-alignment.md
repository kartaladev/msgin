# RFC-0002 — EIP semantic & lexical alignment

- **Author:** kartaladev/msgin maintainers
- **Date:** 2026-07-22
- **Status:** Draft
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

`FilterPredicate` vs Spring's `MessageSelector`: prefer `FilterPredicate` (clearer to Go readers) unless strict
Spring parity is wanted — decided in RFC-0003 where the type is introduced.

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
| Rename churn across adapters/examples | Wide diff | Bundle with RFC-0001's moves; `gopls` rename |
| Interface segregation breaks implementers | Compile breaks downstream | Pre-v1; document in `MIGRATION.md` |
| Over-aligning to Spring hurts Go ergonomics | Awkward API | Keep Go-idiomatic names where clearer (e.g. `FilterPredicate`) |

## 7. Open Questions

1. `Exchange` — keep (qualified) or rename?
2. `FilterPredicate` vs `MessageSelector` (decided in RFC-0003).
3. Do the `MessageChannel` split now, or defer the interface-segregation to C-full?

## 8. Appendix

**Appendix A — fidelity scorecard (abridged).** ✅ faithful unless noted. Message/Headers ✅; Correlation Id ✅;
Sequence headers ✅; DirectChannel 🟡 (single-subscriber); QueueChannel ✅; PublishSubscribeChannel ✅;
Dead-Letter ✅; Invalid-Message ✅; Channel Adapter ✅; Polling Consumer ✅; **Event-Driven Consumer 🟠
(`StreamingSource`)**; Competing Consumers ✅; Service Activator ✅ (`Activate`/`Consume`); Messaging Gateway ✅;
Content-Based Router ✅; Message Filter ✅; Splitter ✅; Aggregator ✅✅; Message Translator 🟡 (`Transform`);
Request-Reply 🟡 (`Exchange`); **`MessageChannel` bundles Send+Subscribe 🟠**. Scope gaps (deliberate, seams
present): Resequencer, Recipient List, Content Enricher, Message Expiration, Idempotent Receiver.

**Sources:** EIP catalog (enterpriseintegrationpatterns.com/patterns/messaging/); Spring Integration channel
reference (docs.spring.io/spring-integration/reference/channel/implementations.html).
