# RFC-0005 — Fill the EIP scope gaps (missing components)

- **Author:** kartaladev/msgin maintainers
- **Date:** 2026-07-22
- **Status:** Draft
- **Reviewers:** TBD

## 1. Summary

RFC-0002's audit catalogued five canonical EIP patterns we deliberately don't implement — Idempotent Receiver,
Resequencer, Recipient List, Content Enricher, Message Expiration. This RFC proposes adding them. Most are
**additive and non-breaking**, and several already have the machinery (sequence headers, `MessageGroupStore`,
`adapter/database/sql`'s `InboxDeduper`) — so this is largely "expose seams that exist," not greenfield.

## 2. Background & Motivation

The library ships a focused EIP subset (Spec 001), and the gaps are intentional *for now* — but each is a
common integration need with a seam already in place, and closing them raises the library from "core patterns"
to "the patterns a real flow needs." Unlike RFC-0001–0004 (a breaking refactor), this is a **feature roadmap**:
additive endpoints/SPIs that don't share the breaking window and can land incrementally.

## 3. Proposal

### Overview

Add the five patterns, each as a small endpoint or SPI + adapter impl, adopting RFC-0003's named behavior types
and RFC-0004's SPI-in-core / impl-in-adapter shape. Prioritise by value-per-effort: **Idempotent Receiver and
Recipient List first**, then Resequencer, then Content Enricher and Message Expiration.

### Detailed Design

| Pattern | Spring | Cardinality | Existing seam | Effort |
|---|---|---|---|---|
| Idempotent Receiver | `IdempotentReceiver` | 1→0/1 | `HeaderMessageID` + `adapter/database/sql.InboxDeduper` | S–M |
| Resequencer | `Resequencer` | N→N (reordered) | Splitter's sequence headers + `MessageGroupStore` | M |
| Recipient List | `RecipientListRouter` | 1→N | RFC-0003 `RoutingFunction` + pub-sub fan-out settlement | S |
| Content Enricher | `Enricher` | 1→1 | `Transform` + Gateway/Exchange for external lookup | S |
| Message Expiration | (header) | 1→0/1 | `HeaderTimestamp` + invalid/expired sink | S |

- **Idempotent Receiver.** A `DedupStore` SPI in core (`SeenBefore`/claim-settle keyed by a dedup key, default
  `HeaderMessageID`, overridable to a business key); a receiver endpoint/decorator that Acks-and-drops a
  duplicate. Impls: memory + `adapter/database/sql` (**expose the existing `InboxDeduper` as a `DedupStore`**).
  Turns at-least-once into effectively-once. *Subtlety:* a legitimate retry/redelivery must not be seen as a
  duplicate — reuse `InboxDeduper`'s claim/settle semantics, don't roll a naive "mark seen on receipt."
- **Resequencer.** The Aggregator's sibling: buffer correlated messages, release them in ascending
  `HeaderSequenceNumber` as a contiguous prefix completes; hold gaps; on timeout release-partial-in-order or
  route to an expired sink. **Reuse `MessageGroupStore`** + an ordered release strategy. Single-process by
  default with the same durable-store seam as the Aggregator.
- **Recipient List.** `RecipientsFunction func(ctx, Message[any]) ([]MessageChannel, error)` +
  `NewRecipientList(pick, opts...)`; fan-out to each recipient with all-succeed settlement (joined error →
  retry) mirroring `PublishSubscribeChannel`'s `FanOutAllSucceed` — recipients should be idempotent. Distinct
  from Router (1-of-N vs 1→N). An expr provider (RFC-0003) can build the recipients function.
- **Content Enricher.** Augments (adds data) vs the Translator (reshapes). Model as `Enrich` — a Transform
  whose fn performs a lookup (optionally via `OutboundGateway`/`Exchange`) and merges via `WithPayload`.
  Possibly a thin helper + documentation rather than a heavy endpoint.
- **Message Expiration.** `WithExpiration(d)`/`WithExpireAt(t)` message options stamp `HeaderExpiration`
  (absolute, clock-derived); the Consumer dispatch path diverts an expired message to an expired/invalid sink
  before the handler. Per-message and process-independent — no coordination.

### Examples

```go
recv := endpoint.IdempotentReceiver(sqlDedup)                 // drops duplicates by message-id
rl   := endpoint.NewRecipientList(pickRecipients)             // 1 → N channels
seq  := endpoint.NewResequencer(groupStore, endpoint.WithOrderTimeout(30*time.Second))
```

## 4. Trade-offs & Alternatives

### Alternatives Considered

- **Leave as scope gaps** — keep the surface minimal. Cost: users re-implement dedup/resequencing themselves,
  often incorrectly (the retry-vs-duplicate subtlety).
- **Content Enricher as docs-only** — it's close to Transform; a dedicated endpoint may not earn its keep.
  Decide per §7.

### Trade-offs

Additive surface must still be justified per CLAUDE.md. The two highest-value adds (Idempotent Receiver,
Resequencer) are stateful/multi-instance-sensitive — their value is precisely that they get the hard parts
right using the existing durable stores, so users don't.

## 5. Implementation Plan

### Phases

Each pattern is an independent additive increment (spec + ADR + plan + SDD), ideally **after RFC-0001** so new
code is born into the `endpoint` package and the new SPI homes:

1. Idempotent Receiver (`DedupStore` SPI + memory/sql impls; expose `InboxDeduper`). — M
2. Recipient List. — S
3. Resequencer (reuse `MessageGroupStore`). — M
4. Content Enricher; Message Expiration. — S each

### Timeline

Non-breaking; land incrementally, independent of the breaking window. Prefer after RFC-0001's package layout.

### Success Metrics

Each pattern: exported endpoint/SPI with godoc + Example test; ≥85% coverage and every hot-path/typed-error
branch tested; multi-instance behaviour documented (in-process vs durable seam) per CLAUDE.md.

## 6. Risks & Mitigations

| Risk | Impact | Mitigation |
|---|---|---|
| Idempotent Receiver dedups a legitimate retry | Message loss | Reuse `InboxDeduper` claim/settle; don't mark-seen-on-receipt |
| Resequencer unbounded buffering on a missing sequence member | Memory/DB growth | Order-timeout → partial release / expired sink; bound the store |
| Surface bloat from marginal patterns | Maintenance cost | Gate Content Enricher on whether it beats plain `Transform` (§7) |
| New names re-introduce lexical drift | Consensus drift | Use Spring names (`RecipientListRouter`, `Resequencer`, `IdempotentReceiver`); coordinate with RFC-0002 |

## 7. Open Questions

1. Content Enricher — dedicated endpoint, or documentation + a thin `Transform` helper?
2. Idempotent Receiver dedup key — `HeaderMessageID` default only, or a first-class business-key strategy?
3. Resequencer — its own `ResequencerStore`, or reuse `MessageGroupStore` with an ordered release strategy?
4. Message Expiration — divert to the invalid-message sink, or a dedicated expired sink?
5. Priority/scope for v1 — all five, or just Idempotent Receiver + Recipient List + Resequencer?

## 8. Appendix

**Appendix A — patterns still deferred beyond this RFC** (System Management group): Wire Tap, Message History,
Control Bus, Message Store. Out of scope here; revisit once the routing/endpoint gaps are closed.
