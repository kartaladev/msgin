# ADR 0014 — Publish-Subscribe channel + topic pub/sub SPI

- **Status:** Proposed (2026-07-19) — pending adversarial audit + user go-ahead. Realizes Spec 003 **§3 D7
  Phase 3**.
- **Context source:** [Spec 004 — Publish-Subscribe](../specs/004-publish-subscribe.md); companion to
  [ADR 0013 — Composition model](0013-composition-endpoints.md); un-defers publish-subscribe from
  [ADR 0002 §4](0002-adapter-spi.md).

## Context

msgin's composition core (ADR 0013) ships `DirectChannel` — synchronous, **point-to-point**, one subscriber.
The remaining EIP channel kind is the **Publish-Subscribe Channel**: one message delivered to **every**
subscriber (fan-out), the pattern behind broadcast / event-notification / audit-tap flows. ADR 0002 §4
deferred pub-sub; this ADR un-defers it.

Fan-out raises design questions `DirectChannel` does not:

1. **Topics.** Real fan-out is usually *topic-scoped* — publish to a named topic, only its subscribers receive
   it. How should msgin model a topic, given that future broker adapters (Kafka/NATS/Redis) have **native**
   topics? A topic model baked into the core as a broker-style dynamic string would **fork** the moment such an
   adapter arrives (two topic mechanisms).
2. **Subscription lifecycle.** Subscribers come and go at runtime, so pub-sub needs **unsubscribe** — which
   Phase-1's `MessageChannel.Subscribe(h) error` (single-subscriber, no unsubscribe) does not provide.
3. **Settlement.** One message, N subscribers: when a subscriber's handler errors, does the whole publish fail
   (safe) or continue best-effort (fast)? A silent per-subscriber drop is a data-loss default.
4. **Concurrency.** The subscriber set is mutated (subscribe/cancel) concurrently with dispatch.

## Decision

**1. Topics are EIP-native: a topic is a named `PublishSubscribeChannel`; a registry maps name → channel.**
Publishing to a topic **is** sending to that channel. A dynamic in-process registry lazily creates a channel
per topic name. This is modeled behind a **topic pub/sub SPI** so that a native-topic broker adapter simply
*implements the SPI* using its own topics — the core never learns a second topic model; topic support is
handled **generically** (dependency points inward). **Rejected:** a broker-style dynamic-string topic in the
core — forks the model when a native-topic adapter appears.

**2. Three layers.**
- **Topic pub/sub SPI** — the seam adapters implement (split per ISP):
  ```go
  type TopicPublisher  interface { Publish(ctx context.Context, topic string, msg Message[any]) error }
  type TopicSubscriber interface { Subscribe(topic string, h MessageHandler) (Subscription, error) }
  type Subscription    interface { Cancel() }
  ```
- **`PublishSubscribeChannel`** — the single-topic fan-out building block (the textbook EIP channel; also what
  a native-topic adapter models per topic). `Send(ctx, msg)` fans out (it is an `OutboundAdapter`);
  `Subscribe(h) (Subscription, error)`.
- **`PubSub`** — the in-process topic registry satisfying the SPI: `Publish(ctx, topic, msg)` /
  `Subscribe(topic, h) (Subscription, error)`.

**3. Subscription-handle lifecycle.** `Subscribe` returns a `Subscription` whose `Cancel()` unsubscribes —
Go-idiomatic, no fragile handler-identity comparison, extensible. Pub-sub is a **distinct, richer channel**: it
does **not** implement Phase-1's `MessageChannel` (whose `Subscribe(h) error` is single-subscriber), but it
**does** implement `OutboundAdapter` (`Send`) so a `Router`/`To` can target it. **Rejected:** `Unsubscribe(h)`
by handler identity — funcs/wrapped handlers compare poorly, a footgun for dynamic subscribe/unsubscribe.

**4. Synchronous dispatch, registration order, no goroutine.** Subscribers run on the caller's goroutine in
registration order — deterministic, leak-free (nothing to join at shutdown), simple settlement. Async/executor
fan-out is a later option. The `Message[any]` **envelope** is immutable, so all subscribers share the one value
concurrently — no copy; per `Headers`' shallow-copy caveat, reference-typed payload/header *contents* remain the
caller's responsibility (store immutable values).

**5. Settlement default = all-subscribers-succeed-before-Ack; best-effort opt-in.** `Send`/`Publish` invokes
every subscriber and returns a **joined error** (`errors.Join`) if any fails, so a Consumer-driven publish
Nacks/retries (at-least-once for the fan-out). Because a whole-fan-out retry re-invokes **all** subscribers,
**subscribers should be idempotent** (documented). `WithFanOut(BestEffort)` = log-and-continue, always Ack.
**Rejected default:** best-effort — swallows a subscriber error and Acks (silent loss).
- **Unit-settlement:** the fan-out settles as one unit — a subscriber's **permanent** error (`errors.Join`
  propagates it) makes the whole message permanent → the observable invalid-message sink; per-subscriber
  independent settlement is out of scope. **Panic isolation:** each subscriber is recovered (panic → transient
  `ErrHandlerPanic`), so a panic never aborts the fan-out and never escapes a standalone `Publish`.

**6. Concurrency.** The subscriber set is `RWMutex`-guarded; `Send` snapshots the subscriber slice under
`RLock` and dispatches **outside** the lock (the `DirectChannel.Send` pattern — no lock held across handler
execution; safe if a handler subscribes/cancels). `Cancel()` is idempotent and safe mid-publish; the registry
drops a topic's channel when its last subscriber cancels (memory hygiene for dynamic topic names).

**7. Out of scope (adapter / deferred):** consumer groups (adapter-provided), per-subscriber durable
independent retry (a durable per-subscriber queue = broker/adapter), broker pub-sub adapters, async dispatch,
Wire Tap / Recipient List (ride on this later). Pub-sub **delivers** the fan-out; it does not **persist**
subscribers.

## Consequences

**Positive**
- One topic model, EIP-native — native-topic adapters implement the same SPI, no core fork.
- Safe-by-default settlement (all-succeed) while overridable (best-effort) — the sensible-defaults posture.
- Leak-free by construction (synchronous, no goroutine); race-safe by the snapshot-under-RLock dispatch reused
  from `DirectChannel`.
- Subscription handles give clean dynamic subscribe/unsubscribe without identity comparison.

**Negative / costs**
- All-succeed retry re-invokes already-succeeded subscribers → a documented idempotency expectation.
- Synchronous dispatch bounds fan-out latency to the slowest subscriber (acceptable for Phase 3; async later).
- The topic SPI is introduced before a broker adapter exists — justified as a stated requirement with a minimal
  surface, exercised immediately by the in-process registry.

**Rejected alternatives** (see Decision): broker-style dynamic-string topics in the core; `Unsubscribe(h)` by
identity; best-effort as the default; async dispatch now.
