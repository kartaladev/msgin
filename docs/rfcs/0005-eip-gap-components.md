# RFC-0005 — Fill the EIP scope gaps (missing components)

- **Author:** kartaladev/msgin maintainers
- **Date:** 2026-07-22
- **Status:** Accepted (open questions settled 2026-07-27 — see §7; the OQ6 blocker is resolved)
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

| Pattern | Spring | Cardinality | Package | Existing seam | Effort |
|---|---|---|---|---|---|
| Idempotent Receiver | `IdempotentReceiver` | 1→0/1 | `endpoint` | `HeaderMessageID` + `adapter/database/sql.InboxDeduper` | **M** (§7.6: new `DedupStore` SPI + `InboxDeduper` method + DDL) |
| Resequencer | `Resequencer` | N→N (reordered) | `routing` | Splitter's sequence headers + `MessageGroupStore` | **M** (§7.3: needs `SettleMembers`) |
| Recipient List | `RecipientListRouter` | 1→N | `routing` | RFC-0003 `routing.RouteFunc` + pub-sub fan-out settlement | S |
| Content Enricher | `Enricher` | 1→1 | `transform` | `Transform` + Gateway/Exchange for external lookup | **M** (§7.1: full endpoint, own options — was S) |
| Message Expiration | (header) | 1→0/1 | `endpoint` (dispatch path) | `HeaderTimestamp` + dedicated expired sink | S |

- **Idempotent Receiver.** A `DedupStore` SPI in core keyed by a dedup key (default `HeaderMessageID`,
  overridable to a business key), and a receiver endpoint that drops a duplicate. Impls: memory +
  `adapter/database/sql` (reuse the existing `InboxDeduper`).

  > **Audit (2026-07-24) — `InboxDeduper` has no claim/settle protocol; the naive decorator reintroduces the
  > very race this bullet warns about.** The draft said to "reuse `InboxDeduper`'s **claim/settle** semantics."
  > It has none: its API is `MarkProcessed(ctx, tx *sql.Tx, msgID) (already bool, err)` (`inbox_dedup.go:126`)
  > — a **single, transactional** mark-processed. Its retry-vs-duplicate correctness comes entirely from
  > running **inside the caller's business transaction**, atomic with the handler's work: if the handler's tx
  > rolls back, the dedup mark rolls back with it, so a redelivery is correctly re-processed. A `DedupStore`
  > decorator that runs **before** the handler and "Acks-and-drops a duplicate" (as the original example
  > implied) is **not** transaction-coupled — a crash after the mark commits but before the handler completes
  > loses the message: exactly the failure this pattern must prevent. **Design consequence:** the `DedupStore`
  > SPI must be shaped around transactional-inbox reality — either (i) the store's claim participates in the
  > same transaction/settlement as the handler (the endpoint dedups *and* settles atomically), or (ii) a
  > genuine two-phase `Claim`→`Settle`/`Release` contract is defined and **`InboxDeduper` gets a new method to
  > satisfy it** (it cannot today). Do **not** model this as a pre-handler mark-seen decorator over the current
  > `MarkProcessed`. Settle this in Open Question 6 before promotion. Turns at-least-once into effectively-once
  > *only if* this coupling is correct.
  >
  > **Resolved 2026-07-27 — see §7.6.** Path (ii) was chosen: a real two-phase `Claim`/`Settle`/`Release`
  > contract with a lease, and `InboxDeduper` gains the method it cannot satisfy today. The tx-coupled
  > `MarkProcessed` path is kept and documented for handlers that own a transaction, so the two coexist rather
  > than compete.
- **Resequencer.** The Aggregator's sibling: buffer correlated messages, release them in ascending
  `HeaderSequenceNumber` as a contiguous prefix completes; hold gaps; on timeout release-partial-in-order or
  route to an expired sink. **Reuse `MessageGroupStore`** + an ordered release strategy. Single-process by
  default with the same durable-store seam as the Aggregator.

  > **Audit (2026-07-27) — `MessageGroupStore` cannot express partial release today; the SPI needs a new
  > method.** "Reuse `MessageGroupStore`" was stated without checking that its settlement is all-or-nothing:
  > `SettleGroup` **deletes exactly the claimed member set**, and `ClaimGroup` leases *all* members present. A
  > Resequencer releasing a **contiguous prefix** must settle some claimed members and keep the rest live —
  > which this SPI has no way to say.
  >
  > The obvious workaround **fails silently**: claim, release the prefix, re-`Add` the tail during the lease,
  > then `SettleGroup`. `Add` is documented idempotent by msg id, so re-adding a still-leased member is a
  > **no-op**, and `SettleGroup` then deletes it — **message loss**, with no error anywhere.
  >
  > **Resolution (§7.3): add `SettleMembers(claim MessageGroupClaim, msgIDs []string) error`** to
  > `MessageGroupStore`, fenced on `claim.Epoch` exactly like `SettleGroup`. This reuses the lease/fence/reaper
  > design that two audit rounds already hardened, rather than building a second durable store. It is a
  > **breaking addition to a shipped SPI** — acceptable pre-v1, and the only implementations are ours (memory,
  > sql, plus the three dialect modules).
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

Package homes follow RFC-0001's EIP-chapter split — the receiver is a ch.10 endpoint, the other two are ch.7
routing patterns:

```go
recv := endpoint.IdempotentReceiver(sqlDedup)                // claim/settle; drops duplicates
rl   := routing.NewRecipientList(pickRecipients)             // 1 → N channels
seq  := routing.NewResequencer(groupStore, routing.WithOrderTimeout(30*time.Second))
enr  := transform.NewEnricher(lookup, transform.WithLookupTimeout(2*time.Second))
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

Each pattern is an independent additive increment (spec + ADR + plan + SDD), **after RFC-0001** so new code is
born into the `endpoint`/`routing`/`transform` packages and the new SPI homes:

0. **Inside the breaking window, not here:** add `MessageGroupStore.SettleMembers` (see the note below). — S
1. Idempotent Receiver — `DedupStore` SPI (root) + memory/sql impls, `InboxDeduper` claim method, lease columns
   in the reference DDL. Lands in `endpoint`. — M
2. Recipient List → `routing`. — S
3. Resequencer → `routing` (consumes `SettleMembers` from step 0). — M
4. Content Enricher → `transform` (now M, §7.1); Message Expiration → the `endpoint` dispatch path (S).

> **Correction (2026-07-27) — this RFC is no longer purely non-breaking.** §2 bills the whole set as "additive
> and non-breaking", which the `SettleMembers` decision (§7.3) contradicts: adding a method to the shipped
> `MessageGroupStore` interface breaks every implementer. Resolution: **land `SettleMembers` during RFC-0001's
> breaking window** (step 0), ahead of the Resequencer that needs it. The method is defined and implemented for
> memory + sql + the three dialect modules while the API is already moving; the Resequencer then lands later as
> genuinely additive work. This keeps RFC-0005's "additive, independent of the window" framing true for
> everything except one method deliberately pulled forward.

### Timeline

Non-breaking; land incrementally, independent of the breaking window. Prefer after RFC-0001's package layout.

### Success Metrics

Each pattern: exported endpoint/SPI with godoc + Example test; ≥85% coverage and every hot-path/typed-error
branch tested; multi-instance behaviour documented (in-process vs durable seam) per CLAUDE.md.

## 6. Risks & Mitigations

| Risk | Impact | Mitigation |
|---|---|---|
| Idempotent Receiver dedups a legitimate retry / drops after a crash | Message loss | **Audit (2026-07-24):** `InboxDeduper` is transactional `MarkProcessed`, not claim/settle — never a pre-handler mark-seen decorator (see §3 audit note). **Resolved 2026-07-27 (§7.6):** two-phase `Claim`/`Settle`/`Release` with a lease, so a crash mid-handler ages the claim out and the message is reprocessed; the tx-coupled `MarkProcessed` path is kept for handlers that own a transaction. Test the crash window explicitly — claim, kill before settle, redeliver, assert reprocessed |
| Resequencer unbounded buffering on a missing sequence member | Memory/DB growth | Order-timeout → partial release / expired sink; bound the store |
| Surface bloat from marginal patterns | Maintenance cost | **Gate opened deliberately 2026-07-27 (§7.1):** Content Enricher ships as a full endpoint with its own options, not a thin `Transform` helper. The mitigation moves down a level — each option must be justified in the per-increment spec or dropped |
| Resequencer reuses an SPI that cannot express partial release | Silent message loss | **Audit-resolved (§3, §7.3):** `SettleGroup` is all-or-nothing and the re-`Add` workaround loses messages via `Add`'s idempotency; add `SettleMembers` (epoch-fenced) and conformance-test partial settlement against both memory and sql stores |
| New names re-introduce lexical drift | Consensus drift | Use Spring names (`RecipientListRouter`, `Resequencer`, `IdempotentReceiver`); coordinate with RFC-0002 |

## 7. Decisions (settled 2026-07-27)

6. **(Blocker, resolved first because the others depend on it) Idempotent Receiver → a genuine two-phase
   `Claim`/`Settle` contract with a lease.**

   ```go
   type DedupStore interface {
       Claim(ctx context.Context, key string) (claimed bool, err error)
       Settle(ctx context.Context, key string) error   // permanent — handler succeeded
       Release(ctx context.Context, key string) error  // retryable — handler failed
   }
   ```

   Endpoint flow: not claimed → duplicate, Ack and drop; claimed → run the handler; success → `Settle`; failure
   → `Release` (redelivered); **crash between `Claim` and `Settle` → the lease ages out and the message is
   correctly reprocessed.** At-least-once is preserved; nothing is lost.

   This **complements rather than replaces** `InboxDeduper`. Two documented paths, chosen by whether the handler
   owns a transaction:

   | Handler shape | Mechanism | Guarantee |
   |---|---|---|
   | Owns a business `*sql.Tx` | `InboxDeduper.MarkProcessed` inside that tx | Dedup mark and business effect commit atomically — the strongest available |
   | Has no transaction (HTTP call, Redis write, …) | `DedupStore` claim/settle | Duplicate suppression bounded by the lease |

   **A pre-handler mark-seen decorator is explicitly forbidden**, and not merely as this RFC's opinion: ADR 0010
   D10 HIGH 5 already made it a **compile error** by typing `MarkProcessed`'s parameter as the concrete
   `*sql.Tx`, precisely so the dedup row cannot commit independently of the business writes. Shipping Spring's
   advisory `IdempotentReceiverInterceptor` shape as a core endpoint would contradict a settled decision of this
   codebase. `InboxDeduper` gains the new method and the reference DDL gains lease columns — free, pre-v1.

   Lease default follows CLAUDE.md's sensible-defaults rule: **comfortably above the source's visibility
   timeout**, not tuned tight.

1. **Content Enricher → a full endpoint with its own options** (lookup timeout, failure policy, merge
   strategy), not a thin `Transform` helper and not documentation only. External-lookup enrichment has failure
   modes `Transform` does not model, and the `transform` package gives it a canonical home.

   > **This deliberately opens a gate §6 had set.** The risk table's "surface bloat / gate Content Enricher on
   > whether it beats plain `Transform`" was **considered and the gate opened**, not left unaddressed. It also
   > moves the item from **S to M** — the options surface has to be designed, specified, and covered. The
   > per-increment spec must justify each option or drop it.

2. **Dedup key → a first-class strategy, defaulting to `HeaderMessageID`.** `Claim(ctx, key)` needs a key
   extractor regardless, so `WithDedupKey(func(Message[any]) (string, error))` costs almost nothing and covers
   the common real case of a business key (order id, payment id) rather than a transport-assigned message id.

3. **Resequencer → reuse `MessageGroupStore`, extended with `SettleMembers`.** See the 2026-07-27 audit note in
   §3 for why plain reuse is unsafe and why a separate `ResequencerStore` was rejected (it would duplicate the
   lease, fencing, and reaper machinery `MessageGroupStore` only got right after two audit rounds).

4. **Message Expiration → a dedicated expired sink** (`WithExpiredChannel`), mirroring the Aggregator's shipped
   `WithExpiredGroupChannel`, including a paired typed error when an expiration is configured without a sink
   (cf. `ErrExpiryChannelRequired`). Keeps "expired" (a timing outcome) distinct from "invalid" (a malformed
   message) — a distinction RFC-0002's audit credited the library for getting right, and one an operator
   reading the channel needs in order to tell a clock problem from a serialization problem.

5. **v1 scope → all five**, landed in the §5 priority order as independent additive increments. Each still gets
   its own spec + ADR + plan + adversarial audit, so scope can be cut per increment without re-deciding here.

## 8. Appendix

**Appendix A — patterns still deferred beyond this RFC** (System Management group): Wire Tap, Message History,
Control Bus, Message Store. Out of scope here; revisit once the routing/endpoint gaps are closed.
