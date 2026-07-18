# ADR 0013 — In-process composition: `MessageHandler`/`MessageChannel`, monomorphic core + typed sugar, synchronous-direct error model

- **Status:** Proposed (2026-07-18) — **adversarial audit round 1 folded** (`.superpowers/sdd/plan-008-audit-round-1.md`: F1 reuse existing `ErrPayloadType`; F2 `To(OutboundAdapter)`; F3 live-value vs wire scoping; F4 nil-func guards; F5 `WithPayload` header propagation; F6 `Chain`-terminal contract; F8 Activate/Consume split). Pending round-2 re-audit + user go-ahead (design-time gate, CLAUDE.md).
- **Context source:** [Spec 003 — Composition endpoints](../specs/003-composition-endpoints.md) §3 (D1–D6, D8), §4.
- **Related:** builds on [ADR 0001 — Payload typing](0001-message-payload-typing.md) (typed caller API over a
  non-generic `Message[any]` core) and [ADR 0002 — Adapter SPI](0002-adapter-spi.md) (settlement, invalid-message
  channel, worker pool). Companion [ADR 0014 — Channel settlement](0014-channel-settlement.md) covers the
  buffered + pub-sub channel kinds; this ADR covers the composition model and the synchronous `DirectChannel`.

## Context

The core is adapter-centric: `Handler[T]` is a **terminal** sink, so a flow cannot be **composed inside the
process** (transform → filter → route → activate). The four most-common composition endpoints — Transformer /
Message Translator, Filter, Content-Based Router, Service Activator — need an in-process `Channel` +
`MessageHandler` chaining that does not exist, and pub-sub was deferred (ADR 0002 §4). Spec 001 §1 designed
these as non-breaking minor additions. Three sub-decisions shape the whole API: (1) how endpoints chain,
(2) how payloads are typed across a heterogeneous chain, (3) how errors are handled without re-implementing the
reliability runtime per endpoint.

Two Go facts constrain the design: **(a) methods cannot have type parameters** (only free functions can be
generic — so a fully-typed fluent method chain is impossible); **(b)** the pipeline is heterogeneous (`A→B→…`),
so payload types cannot flow across the chain at compile time under any scheme.

## Decision

**1. Two composition primitives, channel-wired (not return-based).**
```go
type MessageHandler interface { Handle(ctx context.Context, msg Message[any]) error }
type MessageChannel  interface {
    Send(ctx context.Context, msg Message[any]) error   // identical shape to OutboundAdapter.Send
    Subscribe(h MessageHandler) error
}
```
- `MessageHandler.Handle` is **structurally a `Handler[any]`**, so a composed flow drives off the existing
  `NewConsumer(src, flow.Handle, …)` with no glue and inherits the whole runtime.
- `MessageChannel.Send` is **structurally `OutboundAdapter.Send`**, so any `OutboundAdapter` is a valid terminal
  sink with no glue.
- Endpoints **forward by sending onward themselves** (to the next handler/channel), not by returning a "next"
  message. A return-based `Handle → (Message, error)` cannot express Filter (0-or-1 out), Router (1-of-N to
  different destinations), or the deferred Splitter (N out) / Aggregator (emit-on-complete). The channel-wired
  shape models the whole endpoint group and the future uniformly.

**2. Monomorphic `Message[any]` core + optional typed sugar ("C-core, A-sugar").** The load-bearing layer is
monomorphic over `Message[any]` (mirroring the non-generic SPI, ADR 0001). Typed convenience is **additive,
non-breaking sugar**:
- `PayloadOf[T](m Message[any]) (Message[T], error)` — a safe assertion helper; mismatch → `ErrPayloadType`.
- **Generic free-function constructors** — `Transform[A,B]`, `Filter[A]`, `Activate[A,B]` — give the consumer a
  **typed function body** while boxing into the `any` core at their edges. Because Go forbids generic methods,
  free functions are the *only* typed form; these are it.

**3. Linear composition via `Step`/`Chain` (the Go middleware idiom).**
```go
type Step = func(next MessageHandler) MessageHandler
func Chain(steps ...Step) MessageHandler   // folds right-to-left
func To(sink OutboundAdapter) Step         // terminal: send to any OutboundAdapter (F2)
```
The generic constructors return a `Step`, so linear flows are **typed in their bodies yet compose cleanly**.
Branching (Router) is a `MessageHandler` that dispatches to `MessageChannel`s — where a linear chain ends and
fans out. Types do **not** flow across chain seams at compile time (they are `Message[any]`); a cross-endpoint
type mismatch surfaces at runtime as `ErrPayloadType` (decision 4), exactly as decode failures already do.

- **`To` takes `OutboundAdapter`, not `MessageChannel` (audit F2).** Terminating a flow needs only `Send`; a
  `*memory.Broker` (and every wire outbound adapter) satisfies `OutboundAdapter` but not `MessageChannel`
  (no `Subscribe`). So `To(sink OutboundAdapter)` makes the "any outbound adapter is a valid terminal sink"
  claim actually true; `*DirectChannel` (which has `Send`) also satisfies it. A nil sink → `ErrNilSink`.
- **`Chain` terminal contract (audit F6).** `Chain`'s innermost `next` is a no-op consume, so a **producing**
  flow (ending in a Transform/Activate/passing-Filter) with **no** `To`/`Consume` terminal **silently discards
  its final message**. This is a documented contract on `Chain`'s godoc — always end a producing flow with
  `To(sink)` or `Consume`; the examples always terminate.
- **Header propagation is the fn's responsibility (audit F5).** `New` stamps a fresh id and drops incoming
  headers, so a Transformer/Activator body using bare `New` loses `msgin.correlation-id` and the message id
  mid-flow (breaking traceability). The library provides `WithPayload[A,B](m, payload) Message[B]` — a new
  payload with the **same headers** (id/correlation preserved) — and the examples/godoc steer to it.
- **Nil functions never panic (audit F4).** `Transform`/`Filter`/`Activate`/`Consume`/`NewRouter` given a nil
  function return `ErrNilFunc` at dispatch (via a shared `nilFuncStep`, and `Router.Handle`'s nil-pick guard),
  not a recovered nil-call panic that the runtime would misclassify as transient and retry forever.

**4. Synchronous-direct error model — one model, reused.** In Phase 1 the whole flow runs **synchronously on
the source delivery's goroutine**, inside its settlement scope. Therefore:
- An endpoint returning `error` **propagates up the chain** to the driving `Consumer`, which already owns
  retry / dead-letter / invalid-message / backoff. **No per-endpoint error handling.** The source `Ack` fires
  only after the entire chain succeeds → end-to-end at-least-once preserved.
- A typed-sugar assertion failure (`any→T` mismatch) is a **permanent** error `ErrPayloadType`, routed to the
  **invalid-message channel** (mirrors ADR 0002 §3 decode-failure handling; not retried).
- A no-route from the Router is `ErrNoRoute` (permanent) unless a default channel is configured.
- Endpoint panics are recovered at the existing worker boundary and treated as transient (unchanged).

**5. `DirectChannel` (Phase 1) — synchronous, point-to-point, single subscriber.** `Send` invokes the one
subscribed handler on the caller's goroutine and returns its error. `Subscribe` a second handler → typed error
(point-to-point is single-consumer); `Send` before `Subscribe` → typed error. It starts **no goroutine** (no
leak surface). Buffered (`QueueChannel`) and fan-out (`PublishSubscribeChannel`) channels — which *do* start
goroutines and need their own settlement semantics — are ADR 0014.

**6. Endpoint defaults are opinionated, safe, and overridable** (CLAUDE.md sensible-defaults gate; aligned with
Spring Integration's real-world defaults):
- **Filter** → silent drop on `false` (Spring `throwExceptionOnRejection=false`); opt-in `WithDiscardChannel`.
- **Router** → `ErrNoRoute` when unresolved (Spring `resolutionRequired=true`); opt-in `WithDefaultChannel`.
- **Service Activator** → split into two constructors (audit F8: a value-type `Message[B]{}` cannot represent
  "no reply", so a single nil-reply signal is unimplementable): **`Activate[A,B]`** always forwards `svc`'s
  reply downstream (request-reply), and **`Consume[A]`** runs a side-effecting service and forwards nothing
  (one-way terminal).
- **Transformer** returns a **new** `Message` (immutability preserved), propagating headers via `WithPayload`.
Each is documented on the option godoc with its rationale.

**7. Placement.** Core module, root `package msgin`, **stdlib-only** — no new direct dependency; the root module
graph is unchanged.

**8. Payload representation at the flow boundary (audit F3/F9).** A flow is driven as
`NewConsumer[any](src, flow.Handle, …)` (explicit `[any]` — inference off a method value is not guaranteed).
At `T=any` the payload the first endpoint sees is whatever the runtime's decode produced: for a **live-value**
source (`memory`, `EmitsLiveValue()==true`) it is the original Go value, so typed endpoints (`PayloadOf[Order]`)
work directly; for a **wire** source the default `JSONPayloadCodec[any]` decodes to `map[string]any`, so a typed
endpoint would `ErrPayloadType` on every message. Therefore: **typed composition endpoints assume live-value
payloads**; a wire source must decode to the concrete type in the **first endpoint** (a bytes-passthrough
`WithConsumerCodec[any]` + `Transform[[]byte, T]`). This is documented, not engineered away in Phase 1. The
"drives off any inbound source" claim is scoped accordingly, and an integration test drives a `Chain` through
`NewConsumer` over `memory.New()` to prove the runtime-inheritance path.

## Consequences

**Positive**
- Lowest composition cost: an endpoint is one `MessageHandler`; a linear flow is one `Chain`; the whole
  reliability runtime (retry/DLQ/invalid/flow-control/worker-pool/graceful-drain) is inherited **because a flow
  is a `Handler[any]` in the source's settlement scope** — zero duplication.
- The structural equivalences (`MessageHandler`≈`Handler[any]`, `MessageChannel.Send`≈`OutboundAdapter.Send`)
  mean adapters plug in as sinks and flows plug into consumers with no bridge code.
- Typed bodies without a generic core; the `any` core keeps Router/Splitter/Aggregator/Gateway (future)
  friction-free and lets the typed layer grow additively.
- Consistent with ADR 0001's ratified layering — the same typed-edge / `any`-wire split, one level up.

**Negative / costs**
- Type safety across chain seams is **runtime, not compile-time** (`ErrPayloadType` to the invalid channel).
  Accepted: it is unavoidable for a heterogeneous pipeline in Go, and it reuses an existing failure path.
- Two entry styles (explicit `MessageChannel` wiring vs `Chain`/`Step`) is a slightly larger surface; held
  minimal by making `Step` the primary linear form and channels the branch/fan/terminal form (Spec 003 O-1).
- Transformer and Service Activator share machinery (both `A→B`); kept as distinct names for EIP fidelity +
  intent-signaling, a documented, deliberate redundancy.

**Rejected alternatives**
- **Fully-generic `Handler[In,Out]` + `Chain[A,B,C]` core** — collapses to `any` at every interesting endpoint
  (Router 1-of-N, Splitter 1→N, Aggregator N→1, dynamic routing) while costing the most generic machinery:
  heaviest code, false end-to-end safety, and it cannot be simplified later without a breaking change.
- **Return-based composition (`Handle → (Message, error)` + functional `Chain`)** — clean only for Transformer;
  cannot express Filter/Router/Splitter. Fails the endpoint group on its own.
- **A separate `flow`/`endpoint` subpackage** — would split `msgin.Transformer` into `flow.Transformer`,
  fracturing the core API; the composition endpoints are core EIP and belong beside `Message`/`Consumer`.
- **A committed fluent DSL now** — Go's no-generic-methods rule makes its inline surface untyped, so its
  value-add over typed `Chain` is ergonomic only; gated as Spec 003 Phase 4 (go/no-go later), not decided here.
