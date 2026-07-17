# Spec 003 — In-process composition layer + linear endpoints

- **Status:** Draft (2026-07-18) — brainstormed with the user; **adversarial audit round 1 folded**
  (`.superpowers/sdd/plan-008-audit-round-1.md`; ADR 0013 + Plan 008 revised: `To(OutboundAdapter)`,
  live-value vs wire scoping, nil-func guards, `WithPayload`, `Chain`-terminal contract, Activate/Consume
  split). Pending round-2 re-audit + user go-ahead before implementation (CLAUDE.md design-time gate).
- **Governing product spec:** [Spec 001 — Messaging core](001-messaging-core.md) §1 (the deferred list:
  "routing endpoints (Router, Filter, Splitter, Aggregator); Message Translator; Messaging Gateway") and
  the "Message endpoint / handler" abstractions in the Architecture blueprint. This spec realizes the
  first tranche of that deferred work.
- **Relation to prior ADRs:** builds on [ADR 0001 — Payload typing](../adrs/0001-message-payload-typing.md)
  (typed caller API over a non-generic `Message[any]` core) and [ADR 0002 — Adapter SPI](../adrs/0002-adapter-spi.md)
  §4 (which **deferred publish-subscribe**). This spec un-defers pub-sub as a scoped, phased addition and
  applies ADR 0001's layering **one level up** — to the endpoint chain.
- **New ADRs (to be authored):** **ADR 0013** (composition model: `MessageHandler`/`MessageChannel`,
  the C-core/A-sugar typing choice, the synchronous-direct error model) and **ADR 0014** (channel kinds
  + in-process settlement semantics for the buffered and pub-sub phases).

## 1. Motivation

The core today is **adapter-centric**: a flow is `Source → Consumer[T](Handler) ` on the inbound side and
`Producer[T] → OutboundAdapter` on the outbound side, where `Handler[T]` is a **terminal sink** — it
consumes a message but cannot forward it to a *next* processing step. That is enough to attach application
code to an external system, but it is not enough to **compose a message flow inside the process**: a source
whose messages are transformed, filtered, then content-routed to different sinks — the EIP "pipes and
filters" model that is the whole point of the Messaging Endpoints / Routing / Transformation groups.

Concretely, msgin has **no in-process `Channel` / `MessageHandler` chaining**, and **pub-sub is deferred**
(ADR 0002 §4). So the four most-common composition endpoints — **Service Activator, Transformer/Message
Translator, Filter, Content-Based Router** — cannot be expressed. They were deliberately designed (Spec 001
§1) as **non-breaking, SemVer-minor additions the SPI already supports**; this spec delivers them plus the
thin composition backbone they sit on.

**Canonical reference:** Spring Integration. Its architecture is two-layer — explicit `MessageChannel` /
`MessageHandler` beans underneath, and a fluent `IntegrationFlow` **DSL** as sugar on top that auto-wires
the channels between steps. We mirror that layering, adapted to idiomatic Go (see D2/D9).

## 2. Goals / Non-goals

**Goals:**
- (a) A **first-class in-process composition layer** in the core (root `package msgin`, **stdlib-only**, no
  new dependency): a `MessageHandler` (a processing step) and a `MessageChannel` (the conduit it sends into
  and subscribes to).
- (b) The **four linear endpoints** — Transformer/Message Translator, Filter, Content-Based Router, Service
  Activator — each with an **opinionated, safe, overridable default** (CLAUDE.md sensible-defaults gate) and
  each a real-world messaging staple.
- (c) **Typed ergonomics without a generic core**: a monomorphic `Message[any]` core with **optional typed
  convenience** (`PayloadOf[T]` + generic constructors), consistent with ADR 0001.
- (d) **Reuse the existing reliability runtime** — a composed flow is a `Handler[any]` driven by the existing
  `Consumer`, so retry / dead-letter / invalid-message / flow-control / worker-pool all apply unchanged, with
  **no per-endpoint error handling duplicated**.
- (e) **All three EIP channel kinds** delivered this increment, but **phased** (direct → buffered → pub-sub)
  because settlement complexity increases across them.
- (f) **Open for extension**: the same primitives carry the deferred Splitter/Aggregator/Gateway/Recipient
  List/Wire Tap without a core change.

**Non-goals (this increment):**
- **Splitter, Aggregator, Resequencer** (stateful, correlation/completion/expiry, message store) — next
  increment.
- **Messaging Gateway** (synchronous request-reply bridge; needs correlation + reply-channel registry) —
  the increment after.
- **Recipient List, Wire Tap, Message History, Control Bus** (System Management / multi-cast) — later; they
  ride on the pub-sub phase.
- **A committed fluent `Flow` DSL** — it is **planned but gated** (D9, Phase 4): a go/no-go decision the user
  re-assesses after the primitives stabilize. Not a commitment.
- No change to the adapter SPI, the settlement contract for *external* adapters, or message immutability.

## 3. Decisions (settled with the user, 2026-07-18)

- **D1 — Placement.** The composition layer lives in the **core module, root `package msgin`** (same home as
  `Message`/`Consumer`/`Producer`), **stdlib-only** (no new direct dependency; clockwork/backoff already
  in-core suffice). New files (`channel.go`, `handler.go`, `endpoint.go`, `router.go`, …); no new module.
- **D2 — Typing: monomorphic core + typed sugar ("C-core, A-sugar").** The load-bearing layer is
  **monomorphic over `Message[any]`** (mirroring the non-generic SPI). Optional typed convenience is layered
  on top and is **additive/non-breaking**: `PayloadOf[T](m) (Message[T], error)` for safe asserts, plus
  **generic free-function endpoint constructors** (`Transform[A,B]`, `Filter[A]`, `Activate[A,B]`) that box
  into the `any` core. Rationale: (i) the pipeline is heterogeneous by nature (`A→B→…`) so types cannot flow
  across the chain at compile time regardless; (ii) **Go forbids type parameters on methods**, so a fully
  typed fluent chain is impossible — generic *free functions* are the only typed form; (iii) a `Message[any]`
  core simplifies down to nothing and lets a typed layer be added later, whereas a generic core cannot be
  simplified without a breaking change; (iv) it is exactly ADR 0001's ratified layering, one level up.
  **Rejected:** a fully-generic `Handler[In,Out]` + `Chain[A,B,C]` core — it collapses to `any` at every
  interesting endpoint (Router 1-of-N, Splitter 1→N, Aggregator N→1, dynamic routing) while costing the most
  generic machinery: heaviest code, *false* end-to-end safety.
- **D3 — Composition primitives (channel-wired, not return-based).**
  - `MessageHandler interface { Handle(ctx context.Context, msg Message[any]) error }` — a processing step.
    Note it **is** a `Handler[any]`, so a composed flow drives off the existing `NewConsumer(src, flow.Handle,
    …)` with zero glue.
  - `MessageChannel interface { Send(ctx context.Context, msg Message[any]) error; Subscribe(h
    MessageHandler) error }` — the conduit. Its `Send` **is** `OutboundAdapter.Send`, so any `OutboundAdapter`
    is a valid terminal sink for a flow with no new glue.
  - Endpoints forward by **sending onward themselves** (channel-wired), NOT by returning a next message.
    Rationale: a return-based `Handle → (Message, error)` cannot express Filter (0-or-1 out), Router (1-of-N
    to different destinations), or the deferred Splitter (N out) / Aggregator (emit-on-complete) — the
    channel-wired shape models the whole group and the future uniformly.
- **D4 — Linear composition sugar (`Step`/`Chain`).** For the common linear case, endpoints are also exposed
  as **decorators** — `type Step = func(next MessageHandler) MessageHandler` — composed by
  `Chain(steps ...Step) MessageHandler` (the Go middleware idiom). The generic constructors (D2) return a
  `Step`, so linear flows are **typed in their bodies** yet compose cleanly. Terminal delivery to any
  outbound adapter is a `To(sink OutboundAdapter) Step` (F2). Branching (Router) is a `MessageHandler` that dispatches
  to channels/sub-flows, i.e. where a linear chain ends and fans out. (Exact constructor names finalized in
  ADR 0013 / the plan; the surface is intentionally minimal.)
- **D5 — Error model: one model, reused (the payoff of synchronous-direct).** In Phase 1 the whole flow runs
  **synchronously on the source delivery's goroutine**, inside its settlement scope. Therefore:
  - An endpoint returning `error` **propagates up the chain** to the driving `Consumer`, which already owns
    retry / dead-letter / invalid-message / backoff — **no duplicated error handling**, and the source `Ack`
    fires only after the entire chain succeeds (end-to-end at-least-once preserved).
  - A typed-sugar assertion failure (`any→T` mismatch) is a **permanent** error, `ErrPayloadType`, routed to
    the **invalid-message channel** — mirroring the existing decode-failure handling (ADR 0002 §3).
  - Handler panics inside endpoints are recovered at the existing worker boundary and treated as transient
    (unchanged runtime behavior).
- **D6 — Endpoint set + sensible defaults (real-world / Spring-aligned, all overridable).**
  | Endpoint | Core signature (over `Message[any]`) | Opinionated default | Override |
  | --- | --- | --- | --- |
  | **Transformer** / Message Translator | `(ctx, Message[any]) (Message[any], error)` | — (always maps; returns a **new** message, immutable) | — |
  | **Filter** | `(ctx, Message[any]) (bool, error)` | **silent drop** on `false` (Spring `throwExceptionOnRejection=false`) | `WithDiscardChannel(ch)` |
  | **Router** (content-based, single dest) | `(ctx, Message[any]) (MessageChannel, error)` | **error `ErrNoRoute`** when unresolved (Spring `resolutionRequired=true`) → retry/DLQ | `WithDefaultChannel(ch)` |
  | **Service Activator** | `Activate[A,B]` (request-reply, forwards reply) + `Consume[A]` (one-way, terminal) | reply forwarded / side-effect only | — |
  Each default is **safe-by-default + one `WithX` to change**, documented with its rationale on the option's
  godoc (sensible-defaults gate). Transformer and Service Activator share machinery (both `A→B` in Go) but
  keep distinct names for EIP fidelity + intent-signaling (Transformer = pure message mapping; Service
  Activator = the boundary that invokes your domain service, optionally replying). Content-Based Router ships
  single-destination; **Recipient List** (fan-out to many) is deferred to the pub-sub phase.
- **D7 — Channel kinds, phased (all land this increment).**
  - **Phase 1 — `DirectChannel`**: synchronous, point-to-point, exactly one subscriber. Runs on the sender's
    goroutine → settlement-safe (D5). The workhorse.
  - **Phase 2 — `QueueChannel`**: buffered, point-to-point, async hand-off. Requires an **in-process
    settlement decision** (ADR 0014): a message accepted into the buffer looks "sent," so *who Acks and when*
    — Ack-on-enqueue (fast, loss window if the process dies with a full buffer) vs Ack-on-drain (safe, the
    buffer must carry the settlement handle). Naturally reuses the existing `Poller`/`Consumer` (a
    `QueueChannel` is a `PollingSource`). Overflow policy reuses the existing `OverflowPolicy` knobs.
  - **Phase 3 — `PublishSubscribeChannel`**: fan-out to N subscribers. Requires a **fan-out settlement
    policy** (ADR 0014): all-subscribers-succeed-before-Ack vs best-effort vs per-subscriber isolation.
    Un-defers ADR 0002 §4's pub-sub; unlocks Wire Tap / Recipient List (later).
  Phasing is deliberate: settlement complexity increases direct → buffered → pub-sub, and Phase 1 (usable
  typed flows) ships first per "deliver faster."
- **D8 — Edges (scoped after audit F3/F2/F9).** Head: a flow is a `Handler[any]`, so
  `NewConsumer[any](src, flow.Handle, …)` (explicit `[any]`) drives it, inheriting the whole
  reliability/flow-control/worker-pool runtime. **Typed endpoints assume live-value payloads:** a live-value
  source (`memory`) delivers the original Go value so `PayloadOf[Order]` works directly; a **wire** source at
  `T=any` decodes to `map[string]any`, so it must decode to the concrete type in the first endpoint (a
  bytes-passthrough `WithConsumerCodec[any]` + `Transform[[]byte, T]`). Tail: `To(sink OutboundAdapter)` — any
  `OutboundAdapter` (a `*memory.Broker`, a wire outbound, or a `*DirectChannel`) is a terminal sink; `To` takes
  the narrow `OutboundAdapter` (only `Send`), because `*memory.Broker` has no `Subscribe` and so is not a
  `MessageChannel`. No new bridge code. A Phase-1 integration test proves the `NewConsumer`-driven path.
- **D9 — Fluent `Flow` DSL: planned, gated (go/no-go later).** A Spring-style fluent builder
  (`NewFlow().Filter(…).Transform(…).Route(…).Handle(…).Build()`) is **sugar over Phases 1–3** and is
  recorded as **Phase 4**, whose go/no-go the user **re-assesses after the primitives stabilize** — because
  Go forbids generic methods, its inline methods would be untyped (typed steps injected via `.Step(…)`), so
  its value-add over the typed `Chain` (D4) is ergonomic only. Not committed in this spec.

## 4. Architecture — three layers of the same composability trade-off

```
Layer 3 (Phase 4, GATED):  Flow fluent DSL   — NewFlow().Filter().Transform().Route().Handle().Build()
                            │ sugar over ↓ (untyped inline methods; typed via .Step)
Layer 2 (Phase 1):         Chain / Step       — typed generic constructors: Transform[A,B], Filter[A], Activate[A,B]
                            │ compose into ↓                                  Chain(steps...) MessageHandler
Layer 1 (Phase 1–3):       MessageHandler + MessageChannel (the primitives)
                              MessageHandler.Handle(ctx, Message[any]) error        (== Handler[any])
                              MessageChannel.Send(ctx, Message[any]) error          (== OutboundAdapter.Send)
                              MessageChannel.Subscribe(MessageHandler) error
                              DirectChannel (P1) · QueueChannel (P2) · PublishSubscribeChannel (P3)
                              Router (branch), WithDiscardChannel / WithDefaultChannel, PayloadOf[T], ErrPayloadType/ErrNoRoute
```

**Data flow (Phase-1, what a consumer writes):**

```go
receiptSink := memory.New()                                   // OutboundAdapter == terminal channel
flow := msgin.Chain(
    msgin.Filter(func(ctx context.Context, m msgin.Message[Order]) (bool, error) { // typed body
        return m.Payload().Paid, nil                          // unpaid: silently dropped (default)
    }),
    msgin.Transform(func(ctx context.Context, m msgin.Message[Order]) (msgin.Message[Invoice], error) {
        return msgin.WithPayload(m, m.Payload().ToInvoice()), nil // new payload, headers preserved (F5)
    }),
    msgin.Activate(func(ctx context.Context, m msgin.Message[Invoice]) (msgin.Message[Receipt], error) {
        r, err := payments.Charge(ctx, m.Payload())           // domain service boundary
        if err != nil { return msgin.Message[Receipt]{}, err } // error -> existing retry/DLQ
        return msgin.WithPayload(m, r), nil                   // correlation-id survives the flow
    }),
    msgin.To(receiptSink),                                    // terminal send to any OutboundAdapter
)
consumer, _ := msgin.NewConsumer[any](orderSource, flow.Handle, // reuse the runtime, unchanged
    msgin.WithConcurrency[any](4),                           // competing consumers
    msgin.WithRetryPolicy[any](retry),                       // retry/DLQ
    msgin.WithInvalidMessageSink[any](invalidCh),            // ErrPayloadType / permanent lands here
)
_ = consumer.Run(ctx)
```

Every existing capability (worker pool, retry, DLQ, invalid-message, flow control, circuit breaker,
graceful drain) is inherited **because the flow is a `Handler[any]` running in the source's settlement
scope** — the payoff of the synchronous-direct core (D5).

## 5. Testing posture (per CLAUDE.md + custom skills)

- **Blackbox `_test` packages**, exported surface only; **assert-closure tables** (`table-test`);
  `t.Context()`; `goleak` on any channel/endpoint that starts a goroutine (QueueChannel/PubSub in P2/P3 —
  DirectChannel starts none). `Example…` tests double as godoc for each endpoint and for `Chain`.
- **Hot-path branch coverage (mandatory).** Enumerate per endpoint: Filter true/false/error + discard-channel
  present/absent; Router resolved/unresolved(+default present/absent = `ErrNoRoute`)/error; Transformer
  ok/error; Service Activator reply/one-way/error; `PayloadOf[T]` ok/mismatch (`ErrPayloadType`); `Chain`
  empty/single/many + a mid-chain error propagating; `DirectChannel` Subscribe-twice error, Send-before-
  Subscribe. Target ≥85% on the changed core package; every typed-error branch gets a case.
- **Settlement tests (P2/P3).** Ack-timing under buffer drain + shutdown (QueueChannel); fan-out
  all-succeed / partial-failure policy (PubSub) — driven through the existing Consumer so the reused
  reliability path is exercised end-to-end.
- **`-race` clean**, `CGO_ENABLED=0`, `go vet`/`golangci-lint`/`gofmt` clean, `govulncheck` clean, `go mod
  tidy` stable (no new dep — the root module graph is unchanged).

## 6. Sequencing

1. **Phase 1 — backbone + linear endpoints** (ADR 0013 + plan): `MessageHandler`/`MessageChannel`,
   `DirectChannel`, the four endpoints (C-core + typed constructors), `Chain`/`Step`/`To`, `PayloadOf[T]`,
   `ErrPayloadType`/`ErrNoRoute`, `WithDiscardChannel`/`WithDefaultChannel`. **Usable typed flows ship here.**
2. **Phase 2 — `QueueChannel`** (ADR 0014, in-process settlement): buffered channel over the Poller;
   overflow via existing policy.
3. **Phase 3 — `PublishSubscribeChannel`** (ADR 0014, fan-out settlement): un-defers pub-sub.
4. **Phase 4 — fluent `Flow` DSL** (GATED — go/no-go re-assessed with the user after Phases 1–3).

Each phase: brainstorm-informed spec (this) → ADR → **adversarial Opus audit of the full spec+ADR+plan
bundle** → plan → SDD (fresh implementer per task + adversarial reviewer), per CLAUDE.md. **No implementation
before the audited plan is approved and the user gives an explicit go-ahead.**

## 7. Risks

- **In-process settlement (P2/P3)** — the buffered Ack-timing and fan-out partial-failure policies are the
  genuinely hard design points; isolating them in their own phases + ADR 0014 (not Phase 1) is the mitigation.
- **Transformer/Service-Activator overlap** — mitigated by shared machinery + distinct intent-documented
  names; no behavioral divergence.
- **Surface creep** — held down by the "commonly-used-real-world-only" filter (D6) and by gating the DSL
  (D9); Recipient List / Splitter / Aggregator / Gateway explicitly out (§2).
- **Typed-sugar runtime type errors** — `any→T` mismatches surface as `ErrPayloadType` to the invalid-message
  channel rather than panicking; every constructor's assert path gets a covering test (§5).

## 8. Open items (to close in ADR 0013/0014 + the plan)

- **O-1** — Exact constructor names/shapes for the Step vs channel-wired forms (minimize surface: are both
  `NewX`+`X[A,B]` forms needed per endpoint, or is the `Step` form primary with channels reached via
  `Subscribe`/`To`?). Lean: `Step` primary; channels for branch/fan/terminal.
- **O-2** — Phase-2 settlement default: Ack-on-enqueue vs Ack-on-drain (safe-default gate points to
  Ack-on-drain; confirm in ADR 0014).
- **O-3** — Phase-3 fan-out default: all-succeed-before-Ack vs best-effort (confirm in ADR 0014).
- **O-4** — Whether Router should also offer a header-value convenience (`RouteByHeader(key, mapping)`) as
  sugar over the predicate form, or defer as YAGNI.

## 9. Traceability

Spec 003 realizes [Spec 001](001-messaging-core.md) §1 (deferred routing/transformation/gateway endpoints)
and un-defers [ADR 0002](../adrs/0002-adapter-spi.md) §4 (publish-subscribe). New **ADR 0013** (composition
model + typing + error model) and **ADR 0014** (channel kinds + in-process settlement) record the decisions;
they cite this spec. Plans (Phase 1 = the next plan number; subsequent phases their own) reference Spec 003 +
the ADRs; code/commits carry `Spec: 003` / `Plan:` / `ADR:` trailers. Builds on ADR 0001 (payload typing
layering, applied to the endpoint chain).
