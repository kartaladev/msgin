# Spec 004 — Publish-Subscribe channel + topic pub/sub SPI (composition Phase 3)

- **Status:** Draft (2026-07-19) — brainstormed with the user. Pending the mandatory adversarial audit of the
  full **spec + ADR 0014 + plan** bundle, then an explicit go-ahead, before any implementation (CLAUDE.md
  design-time gate).
- **Governing product spec:** [Spec 003 — Composition endpoints](003-composition-endpoints.md) **§3 D7 Phase 3**
  (the fan-out channel). Companion to [ADR 0013 — Composition model](../adrs/0013-composition-endpoints.md)
  (primitives, `DirectChannel`, synchronous-direct error model — Phase 1, MERGED). **Un-defers**
  [ADR 0002 §4](../adrs/0002-adapter-spi.md) (publish-subscribe, deferred there).
- **Records decisions in:** [ADR 0014 — Publish-Subscribe channel + topic pub/sub SPI](../adrs/0014-publish-subscribe.md).

## 1. Motivation

Phase 1 shipped `DirectChannel` — **point-to-point**, exactly one subscriber. The remaining EIP channel kind is
the **Publish-Subscribe Channel**: one message delivered to **every** subscriber (fan-out). It is the pattern
behind broadcast, event-notification, and audit-tap flows — a produced event fanned out to many independent
handlers, each reacting on its own. msgin cannot express fan-out today (ADR 0002 §4 deferred it); this spec
un-defers it as a scoped, phased addition, **EIP-native** so that future **native-topic broker adapters**
(Kafka topics, NATS subjects, Redis pub/sub) plug into the *same* abstraction rather than a second topic model.

## 2. Goals / Non-goals

**Goals**
- A **first-class Publish-Subscribe Channel** in the core (root `package msgin`, **stdlib-only**): one message
  → all subscribers, with **dynamic subscribe/unsubscribe** (subscribers come and go at runtime).
- **EIP-native topics**: a topic is a *named channel*; a dynamic registry resolves/creates a channel per topic
  name. Modeled behind a **topic pub/sub SPI** so a native-topic adapter implements topics **generically**.
- **Safe-by-default fan-out settlement** (all-subscribers-succeed) with an overridable best-effort mode.
- **Leak-free** (synchronous dispatch, no goroutine) and **race-clean** under concurrent subscribe/unsubscribe.

**Non-goals (this increment)**
- **Wire Tap, Recipient List, Message History** — ride on pub-sub, but a **later** increment (Spec 003 §2).
- **Async/executor fan-out** (subscribers on separate goroutines) — a later option; Phase 3 is synchronous.
- **Per-subscriber durable independent retry** (subscriber 3 fails → 1 & 2 stay settled, 3 retried alone) —
  inherently a durable per-subscriber queue = a **broker/adapter** concern (Redis Streams groups, NATS durable
  consumers), out of scope here.
- **Consumer groups** — adapter-provided (ADR 0002); the sql adapter already gives durable competing consumers.
- **Broker pub-sub adapters** (Kafka/NATS/Redis) — separate adapter-build track; this ships the SPI they'll
  implement + the in-process reference impl.

## 3. Decisions (settled with the user, 2026-07-19)

- **D1 — EIP-native topics.** A topic **is** a named `PublishSubscribeChannel`; publishing to a topic **is**
  sending to that channel. A dynamic **registry** maps topic name → channel (lazily created). Rationale (the
  user's): when a broker adapter with **native** topic support arrives, that native mechanism just *implements
  the channel/SPI* — the core never learns a second topic model, so topic support is handled **generically**
  through one SPI (msgin's dependency-points-inward posture, applied to topics). **Rejected:** a broker-style
  dynamic-string topic baked into the core — it forks the model the moment a native-topic adapter appears.
- **D2 — Three layers.**
  1. **Topic pub/sub SPI** (the seam adapters implement):
     ```go
     type TopicPublisher  interface { Publish(ctx context.Context, topic string, msg Message[any]) error }
     type TopicSubscriber interface { Subscribe(topic string, h MessageHandler) (Subscription, error) }
     type Subscription    interface { Cancel() }
     ```
  2. **`PublishSubscribeChannel`** — the single-topic fan-out building block (the textbook EIP channel; also
     what a native-topic adapter models *per topic*):
     ```go
     func NewPublishSubscribeChannel(opts ...PubSubOption) *PublishSubscribeChannel
     func (c *PublishSubscribeChannel) Send(ctx context.Context, msg Message[any]) error // fan out (OutboundAdapter)
     func (c *PublishSubscribeChannel) Subscribe(h MessageHandler) (Subscription, error)
     ```
  3. **`PubSub`** — the in-process topic registry satisfying the SPI: `NewPubSub(opts ...PubSubOption) *PubSub`;
     `Publish(ctx, topic, msg)`; `Subscribe(topic, h) (Subscription, error)`.
- **D3 — Subscription-handle lifecycle.** `Subscribe` returns a `Subscription` whose `Cancel()` unsubscribes.
  Go-idiomatic (like `context.CancelFunc`/`time.Ticker.Stop`), avoids fragile `MessageHandler`-identity
  comparison, and leaves room to attach topic/metrics/health later. This **intentionally diverges** from
  Phase-1's `MessageChannel.Subscribe(h) error` (single-subscriber, no unsubscribe) — pub-sub is a distinct,
  richer channel and does **not** implement `MessageChannel`; it implements `OutboundAdapter` (`Send`) so a
  `Router`/`To` can target it.
- **D4 — Synchronous dispatch, registration order, no goroutine.** `Send`/`Publish` runs each subscriber on the
  caller's goroutine, in registration order. Deterministic, leak-free (nothing to join at shutdown, unlike a
  buffered channel), and gives a simple settlement story. Async/executor dispatch is a later option.
- **D5 — Fan-out settlement: all-succeed-before-Ack (default), best-effort (opt-in).** Default:
  `Send`/`Publish` invokes every subscriber and, if **any** returns an error, returns a **joined error**
  (`errors.Join`) — so a **Consumer-driven** publish Nacks/retries per its `RetryPolicy` (at-least-once for the
  fan-out as a whole); no subscriber silently misses a message the flow considered delivered. Because a
  whole-fan-out retry re-invokes **all** subscribers, **subscribers should be idempotent** (documented in the
  godoc + the delivery-guarantee doc — consistent with the library's at-least-once posture). `WithFanOut(BestEffort)`
  = log-and-continue on a subscriber error, always succeed (Ack). **Rejected default:** best-effort — it
  swallows a subscriber error and Acks, a silent-loss default the sensible-defaults gate forbids.
  - **Unit-settlement (audit F2):** the fan-out settles as **one unit** — the joined error's classification follows the
    runtime's rules, so a subscriber returning a **permanent** error (e.g. `ErrPayloadType`; `errors.Join` propagates it)
    makes the WHOLE message permanent → a Consumer-driven publish routes it to the **invalid-message sink** (observable, not
    silently dropped); otherwise the fan-out is transient and retried. A subscriber whose permanent failure must not affect the
    others' redelivery needs **per-subscriber independent settlement** — a durable adapter concern, out of scope (D7).
  - **Panic isolation (audit F3):** each subscriber invocation is recovered (a panic → transient `ErrHandlerPanic`), so a
    panicking subscriber never aborts the fan-out (best-effort continues; all-succeed retries) — upholding CLAUDE.md's
    recover-at-endpoint-boundaries rule, including for a **standalone** `Publish`.
- **D6 — Concurrency + immutability.** The subscriber set is guarded by an `RWMutex`; `Send` **snapshots** the
  subscriber slice under `RLock` and dispatches **outside** the lock (the `DirectChannel.Send` pattern —
  avoids holding the lock across handler execution and self-deadlock if a handler subscribes/cancels).
  `Cancel()` removes the subscriber; the registry drops a topic's channel when its **last** subscriber cancels
  (memory hygiene for dynamic topic names). The `Message[any]` **envelope is immutable** (payload interface + immutable
  `Headers`), so all subscribers safely share the one value concurrently — no defensive copy. Per `Headers`' own caveat the
  copy is shallow: a **reference-typed payload or header value** (`*T`, slice, map) can still be mutated in place by one
  subscriber and observed by another, so callers should carry immutable payload/header values (audit F5).
- **D7 — What stays out (adapter/deferred).** Consumer groups, per-subscriber durable retry, and broker
  pub-sub are adapter concerns (§2). Pub-sub **delivers** the fan-out; it does not **persist** subscribers — a
  durable subscriber registry (surviving restart) is the consumer's concern, rebuilt on recovery.

## 4. Architecture

```
Layer 1 — SPI (adapters implement this):   TopicPublisher.Publish(ctx, topic, msg)
                                            TopicSubscriber.Subscribe(topic, h) (Subscription, error)
                                            Subscription.Cancel()
Layer 2 — building block:                   PublishSubscribeChannel (single topic)
                                              Send(ctx, msg) -> fan out to all subs (all-succeed | best-effort)
                                              Subscribe(h) (Subscription, error)     [snapshot-under-RLock dispatch]
Layer 3 — in-process registry (satisfies    PubSub{ topics map[string]*PublishSubscribeChannel }  [RWMutex, lazy create,
           the SPI):                           Publish(ctx, topic, msg) / Subscribe(topic, h) (Subscription, error)   drop-on-empty]

Wiring:
  standalone:      ps.Publish(ctx, "OrderApproved", msg)            // fan out; joined err if a subscriber fails
  Consumer-driven: NewConsumer(src, func(ctx, m){ return ps.Publish(ctx, topicOf(m), m) })  // Nack/retry => at-least-once fan-out
  future adapter:  natsPubSub{} implements TopicPublisher/TopicSubscriber via native subjects  // generic, no core change
```

## 5. Testing posture (per CLAUDE.md + custom skills)

- **Blackbox `_test` packages**, exported surface only; **assert-closure tables** (`table-test`); `t.Context()`.
- **`goleak` clean on every path** — synchronous dispatch starts no goroutine, so a leak would be a real
  regression to catch; **`-race` clean** with concurrent subscribe/cancel *during* a publish.
- **Hot-path/typed-error branches:** fan-out to 0 / 1 / N subscribers; all-succeed with one/many failures →
  joined error; best-effort → nil + all others still invoked; `Subscribe(nil)` → `ErrNilHandler`; `Cancel()`
  idempotent (double-cancel safe); publish after last cancel (empty topic); registry lazy-create + drop-on-empty.
- **Fan-out envelope sharing:** every subscriber sees the same payload/headers from the one shared immutable envelope (the
  guarantee is envelope-level; reference-typed payload contents are the caller's responsibility — F5).
- **Fan-out integration test:** register N handlers on a topic, publish, assert all invoked in registration
  order; cancel some, re-publish, assert only the rest fire; concurrent subscribe/cancel under `-race`.
- **Whole-branch gate:** `-race`, `CGO_ENABLED=0`, `go vet`/`golangci-lint`/`gofmt`/`govulncheck` clean,
  `go mod tidy` stable (stdlib-only, no new dep), coverage ≥85% + every typed-error branch covered.

## 6. Sequencing

1. **Plan (Phase 3)** — the SPI + `PublishSubscribeChannel` + `PubSub` registry + options, per this spec and
   ADR 0014: brainstorm-informed spec (this) → ADR 0014 → **adversarial audit of spec+ADR+plan** → SDD (fresh
   implementer per task + adversarial reviewer) → whole-branch gate → merge.
2. **Later** (own specs): Wire Tap / Recipient List (ride on pub-sub); broker pub-sub adapters (implement the
   SPI natively).

No implementation before the audited plan is approved and the user gives an explicit go-ahead.

## 7. Risks

- **Concurrent mutation during dispatch** — subscribe/cancel racing a publish. Mitigation: snapshot-under-RLock
  dispatch (D6), `-race` tests with concurrent subscribe/cancel, `Cancel()` idempotent + safe mid-publish.
- **Retry re-invokes all subscribers** (all-succeed) → an idempotency expectation. Mitigation: documented in
  godoc + delivery-guarantee doc; best-effort available as opt-in.
- **Dynamic topic accumulation** — unbounded topic names could grow the registry. Mitigation: drop-on-empty
  (D6); document that topics are cheap and GC'd when idle.
- **SPI premature abstraction** — defining `TopicPublisher`/`TopicSubscriber` before an adapter exists.
  Mitigation: it is a **stated requirement** (generic native-topic adapters), the surface is minimal (2+1
  interfaces), and the in-process impl exercises it immediately (not speculative).

## 8. Open items (to close in ADR 0014 + the plan)

- **O4-1** — Does `PublishSubscribeChannel`/`PubSub` need a `Close()` (matching watermill's Publisher/Subscriber
  and future adapters), given synchronous dispatch has no goroutine to stop? Lean: a `Close()` that cancels all
  subscriptions and rejects further `Send`/`Subscribe` (an `ErrPubSubClosed`) — useful for lifecycle/tests and
  SPI parity — but confirm it earns its place vs YAGNI.
- **O4-2** — Should `Router`/`Filter` (Phase-1) be widened from `MessageChannel` to `OutboundAdapter` so a
  `PublishSubscribeChannel` can be a `Router` destination (it has `Send`, not `MessageChannel.Subscribe`)?
  **Non-breaking** for the option params (`MessageChannel` *declares* `Send`, so a `MessageChannel` value satisfies
  `OutboundAdapter`); but widening `Router.pick`'s `(MessageChannel, error)` **return** to `OutboundAdapter` **is breaking**
  (Go function-return types are invariant) — so `pick`-widening needs its own ADR and stays deferred. Audit confirms against
  shipped signatures.
- **O4-3** — Dispatch order: registration order (natural) vs a deterministic sort. Lean: registration order;
  document it; a caller needing a total order sorts its own handlers.
- **O4-4** — Should the SPI be one interface (`TopicBus` combining Publish+Subscribe) or the two split
  (`TopicPublisher`/`TopicSubscriber`, ISP)? Lean: split (segregated — a publisher-only or subscriber-only
  adapter is legitimate), matching the common Publisher/Subscriber split.

## 9. Traceability

Spec 004 details [Spec 003](003-composition-endpoints.md) §3 D7 **Phase 3** and un-defers
[ADR 0002 §4](../adrs/0002-adapter-spi.md). [ADR 0014](../adrs/0014-publish-subscribe.md) records the decisions
(EIP-native topics, the topic pub/sub SPI, Subscription-handle lifecycle, synchronous all-succeed settlement),
citing this spec and its companion [ADR 0013](../adrs/0013-composition-endpoints.md). The Phase-3 plan
references Spec 004 + ADR 0014; code/commits carry `Spec: 004` / `Plan:` / `ADR: 0014` trailers.
