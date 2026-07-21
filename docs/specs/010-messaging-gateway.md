# Spec 010 — Messaging Gateway (in-process request-reply: inbound + outbound)

- **Status:** Draft (2026-07-21) — brainstormed with the user; scope and the dominant design forks settled
  interactively (see "Decisions settled with the user"). ADR 0022 + Plan 019 authored. **Adversarial design audit
  round 1 folded** (Opus, NEEDS-REVISION → revised): **G1** correlator enforces correlation-id uniqueness
  (`ErrDuplicateCorrelation`) + rejects empty ids (`ErrNoCorrelation`) — §3.2; **G2** Plan 019 adds the previously
  missing async cross-goroutine + N-concurrent `-race` + timeout-races-delivery tests; **G3** the async-wiring language
  corrected (reply channel is `DirectChannel`-only; async = who calls `reply.Send`) — §3.3; **G4** the give-up path
  drains a delivered-but-abandoned reply to the unmatched sink instead of dropping it silently — §3.2/§3.5; **G5**
  outbound save/restore uses raw header presence — §3.4; G6–G8 folded into Plan 019. **Round-2 re-audit: SOUND-WITH-NITS,
  no must-fix** — all five G-fixes CONFIRMED-FIXED (the `giveUp` blocking-drain proven leak-free across every
  interleaving); three LOW/nit items folded: N1 (correlation-id uniqueness is a lifetime, not just concurrent,
  contract — §3.2), N2 (a `closeRacesGiveUp` test covers the `giveUp` `ok==false` defensive arm — Plan 019 Task 1),
  N3 (godoc notes the empty/duplicate guards are direct-caller guards the façades never trip). **Design gate cleared;
  ready to implement (Plan 019) pending user go-ahead.**
- **Decisions settled with the user (2026-07-21):**
  - **Scope** = the **request-reply** Messaging Gateway only. **Both directions**: an **inbound** (application-facing)
    gateway and an **outbound** (in-flow) gateway. **One-way is out** — in-process one-way is already `Producer.Send`
    onto a channel, and external one-way is a channel adapter; a one-way gateway would be pure sugar (§2 non-goals).
  - **Outbound target** = an **in-process channel pair + a pluggable SPI** (`RequestReplyExchange`). The core ships one
    in-process implementation (`ChannelExchange`); a future HTTP/NATS adapter implements the same SPI for real external
    call-out **without a core change** (Open/Closed). This collapses inbound + outbound onto **one shared primitive**.
  - **Reply correlation header** = **reuse `HeaderCorrelationID`** (not a dedicated header). Chosen deliberately after
    the collision trade-off was surfaced: it is EIP-canonical for scatter-gather (gateway → splitter → aggregator all
    key off the same id). The cost — **the outbound gateway must save the incoming `HeaderCorrelationID`, mint a fresh
    id for its own exchange, and restore the saved id on the reply** before forwarding downstream — is accepted (§3.4).
  - **Reply timeout** = **30s default**, overridable via `WithReplyTimeout`; the effective deadline is
    `min(ctx deadline, reply timeout)`. Guards against an unbounded hang / registry leak when the caller passes a
    deadline-less ctx (CLAUDE.md safe-default gate: fail-safe, generous margin above any plausible round-trip).
  - **Unmatched / late reply** = **warn-log + drop by default**; opt-in `WithUnmatchedReplySink(OutboundAdapter)` to
    route them. Never propagated as an error back to the reply-channel sender (that would couple a reply producer's
    success to gateway-waiter liveness — usually wrong; §3.5).
- **Governing product spec:** [Spec 001 — Messaging core](001-messaging-core.md) §1, which names the **Messaging
  Gateway** among the deferred endpoints. Directly continues [Spec 003 — Composition endpoints](003-composition-endpoints.md),
  which built the linear endpoints and **explicitly deferred the Messaging Gateway** ("synchronous request-reply
  bridge; needs correlation + reply-channel registry — the increment after", Spec 003 §2 non-goals). This spec is
  that increment.
- **Builds on:** [ADR 0013 — Composition endpoints](../adrs/0013-composition-endpoints.md) (the
  `MessageHandler`/`MessageChannel`/`Step`/`Chain` backbone the gateways compose into; `To(sink)` terminal),
  [ADR 0001 — Payload typing](../adrs/0001-message-payload-typing.md) (`Message[T]` on the caller API, non-generic
  `Message[any]` on the SPI; `boxMessage`/`WithPayload`/`Payload().(Rep)` reused), and the
  `HeaderCorrelationID` convention the **Aggregator** already established (Spec 009 §3.3). Reuses `clockwork`
  ([ADR 0004](../adrs/0004-clockwork-dependency.md)) for the reply timeout.
- **New ADR (to be authored):** **ADR 0022** — the gateway + request-reply model: the `RequestReplyExchange` SPI, the
  shared zero-goroutine reply correlator, the correlation-header **reuse + save/restore** decision and its
  consequences, and the reply-timeout / unmatched-reply defaults.

## 1. Motivation

Every endpoint msgin ships so far is **one-way**: a message enters a flow and is consumed, transformed, routed, split,
or aggregated, but the caller never gets a value *back*. The **Messaging Gateway** is the pattern that hides the
messaging system behind an ordinary synchronous call — application code calls a typed method, msgin turns the call
into a `Message`, drives it through a flow, and **blocks for the correlated reply**, returning it as a plain value (or
an error / timeout). It is how request-reply application code talks to a message flow without knowing there is one.

EIP splits this into two mirror-image endpoints, and this spec builds both onto one primitive:

- **Inbound gateway** — the application-facing entry. `Gateway[Req,Rep].Request(ctx, req) (Rep, error)`: the request
  comes *into* the flow from outside (the app), the flow processes it, the reply returns to the blocked caller.
- **Outbound gateway** — an endpoint placed *inside* a flow. It takes the in-flight message, sends it to a request
  channel, blocks for the correlated reply, and emits that reply downstream to the next step. The flow's own
  "call out and wait" primitive.

The genuinely new machinery — the reason Spec 003 deferred this to "the increment after" — is the **reply-channel
registry + correlation**: a way for exactly the reply that belongs to a blocked caller's goroutine to wake exactly
that goroutine, with a timeout and clean-up, safe under concurrency. Both gateways are thin façades over that one
primitive, and a `RequestReplyExchange` SPI makes the same façades work over a future external transport without a
core change.

## 2. Goals / Non-goals

### Goals

- (a) **Inbound gateway** — a typed `Gateway[Req,Rep]` with `Request(ctx, Req) (Rep, error)` that boxes `Req`, drives
  the request through a `RequestReplyExchange`, and unboxes the reply to `Rep` (type-assert → `ErrPayloadType`).
- (b) **Outbound gateway** — an in-flow `Step` (`OutboundGateway`) that performs a request-reply exchange on the
  message and forwards the reply to `next`, applying the correlation **save/restore** rule (§3.4).
- (c) **`RequestReplyExchange` SPI** — the narrow public seam `Exchange(ctx, Message[any]) (Message[any], error)`, with
  one shipped in-process implementation (`ChannelExchange`) and room for adapters to implement external call-out.
- (d) **Zero-goroutine reply correlator** — a concurrency-safe registry that correlates replies to blocked waiters with
  no long-lived owned goroutine (leak-free by construction).
- (e) **Safe defaults** — 30s reply timeout (`min(ctx, timeout)`), warn-log-and-drop for unmatched replies, both
  overridable via `WithX` options.
- (f) **Graceful shutdown** — `Close()` on the exchange rejects new requests (`ErrGatewayClosed`) and fails pending
  waiters, draining cleanly.
- (g) **Open for extension** — the same façades carry a future external request-reply adapter (HTTP/NATS) with no core
  change, purely by implementing `RequestReplyExchange`.

### Non-goals (this increment)

- **One-way / send-only gateway.** In-process one-way is `Producer[T].Send`; external one-way is a channel adapter. A
  void gateway would be pure ergonomic sugar over `Producer` — deferred; non-breaking to add later.
- **Async / future-returning gateway** (`RequestAsync` → future). Extra correlation-lifecycle surface for no settled
  need; deferred.
- **External request-reply transports** (HTTP inbound/outbound gateway, RPC-over-broker). Those are **adapters** that
  implement the `RequestReplyExchange` SPI this spec defines; they ship in their own modules later, not here.
- **Header-carrying request variant** (`RequestMessage(ctx, Message[Req])` for caller-set request headers). v1 exposes
  `Request(ctx, Req)` only; a message-level variant is a non-breaking later add.
- **Reply aggregation / scatter-gather as a single endpoint.** Scatter-gather is *composed* from this gateway + the
  existing Splitter/Aggregator (they share `HeaderCorrelationID`); no new endpoint is introduced.

## 3. Decisions

### 3.1 `RequestReplyExchange` SPI (core)

The single seam both gateways delegate to:

```go
type RequestReplyExchange interface {
    Exchange(ctx context.Context, req Message[any]) (Message[any], error)
}
```

Non-generic (over `Message[any]`), consistent with the adapter-SPI typing rule (ADR 0001/0002). The inbound gateway
owns the `Req`/`Rep` generics at its boundary; the exchange and outbound gateway are type-agnostic. An adapter (HTTP,
NATS) implements `Exchange` to do a real external round-trip; both façades then get an external gateway for free.

### 3.2 Reply correlator (internal, shared)

An unexported `replyCorrelator`:

- A `sync.Mutex`-guarded `map[string]chan Message[any]`, each value a **one-shot, cap-1** reply slot keyed by the
  request's `HeaderCorrelationID`.
- `register(id) (slot, deregister, err)` — inserts a fresh slot. **Uniqueness is enforced here (audit G1):** a
  duplicate in-flight `id` returns `ErrDuplicateCorrelation` and an already-closed correlator returns
  `ErrGatewayClosed`, so two concurrent requests can never overwrite each other's slot or cross-delete on deregister.
  `Exchange` additionally rejects an **empty** `id` with `ErrNoCorrelation` (mirroring the Aggregator's
  no-correlation invariant) before registering. Correlation ids must be unique across the exchange **lifetime**, not
  only concurrently (audit N1): a sequentially-reused id can receive a prior abandoned request's late reply — the
  façades mint fresh 128-bit ids so they never hit this; direct `ChannelExchange` callers must use unique ids. Both
  guards are **direct-caller** guards the façades never trip (audit N3). `deregister` is the give-up reconciler (§below).
- A **`MessageHandler`** (the reply receiver) that the exchange `Subscribe`s onto the **reply channel**. On each reply:
  read `HeaderCorrelationID`; if a waiter slot exists, deliver (remove-under-lock then non-blocking send into the cap-1
  slot) and the waiter wins; else it is an **unmatched reply** (§3.5).
- **Zero owned goroutines**: the receiver runs on whatever goroutine drives the reply channel (the caller's own
  goroutine for a synchronous `DirectChannel`; a driving Consumer's worker when `reply.Send` is invoked from another
  goroutine); each waiter blocks on its own goroutine. Nothing to leak; `goleak` asserts it on **both** the synchronous
  and the cross-goroutine (async) path.

The waiter blocks in `select { case rep := <-slot; case <-ctx.Done(); case <-timer.Chan() }` (timer from the injected
`clockwork.Clock`), returning the reply, `ctx.Err()`, or `ErrReplyTimeout`. On the ctx/timeout (and send-error) exits it
**reconciles with a possibly-concurrent deliver (audit G4):** `deregister` reports whether it still owned the slot; if
a deliver already claimed it, the waiter drains the slot and routes the delivered-but-abandoned reply to the unmatched
path (§3.5) rather than dropping it silently — important for a future at-least-once external exchange.

### 3.3 `ChannelExchange` (in-process `RequestReplyExchange`)

```go
func NewChannelExchange(request, reply MessageChannel, opts ...ExchangeOption) (*ChannelExchange, error)
func (e *ChannelExchange) Exchange(ctx context.Context, req Message[any]) (Message[any], error)
func (e *ChannelExchange) Close() error
```

- Construction validates: nil `request` or `reply` → `ErrNilChannel`; an explicit `WithReplyTimeout(d<=0)` →
  `ErrInvalidReplyTimeout` (set-flag pattern distinguishes "unset → 30s default" from "explicit invalid"). It
  `Subscribe`s the correlator receiver onto `reply` (so `reply` is a channel dedicated to the gateway; a second
  subscriber is the channel's own `ErrChannelSubscribed`).
- `Exchange`: `register` the request's `HeaderCorrelationID` → `request.Send(ctx, req)` (a send error deregisters and
  propagates) → wait for the correlated reply / ctx / timeout.
- `Close`: marks closed (new `Exchange` → `ErrGatewayClosed`) and fails every pending waiter with `ErrGatewayClosed`.
  Idempotent.
- `ExchangeOption`: `WithReplyTimeout(d)`, `WithUnmatchedReplySink(out OutboundAdapter)`, `WithExchangeClock(clock)`,
  `WithExchangeLogger(*slog.Logger)`.

**Wiring (documented; corrected per audit G3):** the request channel's subscriber is the caller's flow; the flow ends
in `To(replyChannel)`; the exchange owns `replyChannel`. **The reply channel is a `Subscribe`-based `MessageChannel` —
in the current core that is `DirectChannel` only** (`QueueChannel` is poll-based and `PublishSubscribeChannel.Subscribe`
has a different signature; neither satisfies `MessageChannel`). "Synchronous" vs "asynchronous" is **not** about the
reply channel's type but about **which goroutine calls `reply.Send`**: in a fully synchronous flow the whole round-trip
runs on the caller's goroutine (the cap-1 slot is already full when the waiter reads it — register-before-send makes
this safe); in an asynchronous flow the request path bridges into a `QueueChannel`+`Consumer` (or any worker) whose
flow terminates in `To(replyDirectChannel)`, so `reply.Send` — and thus `deliver` — runs on that worker's goroutine and
the waiter blocks until the reply is driven back. The correlator handles both; an `Example` demonstrates the async
wiring.

### 3.4 Inbound + outbound gateways (the two façades)

**Inbound** — typed, application-facing:

```go
func NewGateway[Req, Rep any](x RequestReplyExchange, opts ...GatewayOption[Req,Rep]) (*Gateway[Req,Rep], error)
func (g *Gateway[Req,Rep]) Request(ctx context.Context, req Req) (Rep, error)
```

`NewGateway` with a nil exchange → `ErrNilExchange`. `Request`: mint a **fresh unique** id (`randomID`), build
`New[Req](req)` with `HeaderCorrelationID = id`, `boxMessage` → `Exchange` → reply `.Payload().(Rep)` (→ `ErrPayloadType`
on mismatch). The app passes a raw `Req` with no pre-existing headers, so the inbound path **always** mints fresh —
no collision risk.

**Outbound** — an in-flow `Step`:

```go
func OutboundGateway(x RequestReplyExchange) Step
```

`Handle(ctx, msg)`: **save** `msg`'s incoming `HeaderCorrelationID` **and whether it was present** → set a
**fresh unique** id on the message for its own exchange → `Exchange(ctx, msg)` → **restore the original correlation
state** onto the reply → forward the reply to `next`. Restore has two cases: if the incoming message *had* a
correlation id `G`, the reply is forwarded carrying `G` (`WithHeader`); if it had **none**, the fresh exchange id
**must not leak downstream** — the reply is forwarded with the correlation id removed. Because `Headers` is immutable
and additive (`WithHeader` cannot delete), this needs a header-removal capability (a `Message.WithoutHeader` /
`Headers.without`, added as part of this task — see §8). This is the accepted cost of reusing `HeaderCorrelationID`:
it keeps an upstream splitter's/aggregator's correlation id intact across the round-trip and gives every concurrent
in-flight exchange a **unique** registry key even when the messages entering the outbound gateway share a correlation
id (e.g. all children of one split). An `Exchange` error propagates back to the driving Consumer (retry/dead-letter
owns it), unchanged.

### 3.5 Unmatched / late replies

A reply whose `HeaderCorrelationID` has no registered waiter (the waiter already timed out / cancelled, or the id is
unknown) is handled by the receiver as: **warn-log** via the injected `*slog.Logger` and **drop**, returning `nil` from
`Handle` (so the reply-channel sender is never failed by a slow/absent waiter). If `WithUnmatchedReplySink(out)` is
set, the reply is instead `Send`-ed to `out` (best-effort; a sink error is logged, not propagated). Mirrors the
existing `WithInvalidMessageSink` posture — safe, observable, extensible.

### 3.6 Cross-cutting

- **Errors** (new sentinels in `errors.go`, exported for `errors.Is`): `ErrGatewayClosed`, `ErrReplyTimeout`,
  `ErrNilExchange`, `ErrNilChannel`, `ErrInvalidReplyTimeout`, `ErrDuplicateCorrelation` (G1). Reuse `ErrPayloadType`
  and `ErrNoCorrelation` (empty correlation id — G1). ctx cancellation returns `ctx.Err()` (distinguishable from
  `ErrReplyTimeout`).
- **Concurrency-safe by construction:** one exchange/correlator serves many concurrent `Request`s; the registry is
  mutex-guarded; `Message` immutability means no shared mutable payload. `-race` clean.
- **No new dependency.** Pure core (stdlib + `clockwork`). Additive API → **minor** SemVer bump; `apidiff` shows only
  additions.
- **Pure Go / debuggable:** no cgo, typed wrapping errors naming the failure; a caller can breakpoint through
  `Request` → `Exchange` → the correlator and read a plain stack.

## 4. Architecture

### 4.1 Component view

```
   app code ─► Gateway[Req,Rep].Request(ctx, req) ─┐        (inbound façade, typed)
                                                    ├─► RequestReplyExchange.Exchange(ctx, Message[any])
   in-flow  ─► OutboundGateway(x) Step.Handle ──────┘        (outbound façade, in-flow; save/restore corr-id)
                                                              │
                                          ┌───────────────────▼─────────────────────┐
                                          │ ChannelExchange (in-process impl)         │
                                          │  register(corrID) → one-shot slot         │
                                          │  request.Send(ctx, req)                   │
                                          │  wait: slot / ctx.Done() / replyTimeout   │
                                          └───────────────────────────────────────────┘
   reply flow ─► To(replyChannel) ─► [correlator receiver subscribed here] ─► demux by HeaderCorrelationID → slot
```

### 4.2 Inbound round-trip (synchronous `DirectChannel` example)

1. `Request(ctx, req)` mints `corrID`, boxes `req` with `HeaderCorrelationID=corrID`, registers the slot.
2. `request.Send` runs the flow synchronously on the caller's goroutine → flow ends `To(replyChannel)`.
3. The correlator receiver (the reply channel's subscriber) runs synchronously, matches `corrID`, fills the slot.
4. Control returns up the stack to the waiter's `select`; the slot is already full → unbox → return `Rep`.

For an asynchronous flow, steps 2–3 happen on another goroutine and the waiter's `select` blocks until the reply is
driven back (or ctx/timeout fires).

### 4.3 Outbound save/restore (correlation invariant)

A message with an existing `HeaderCorrelationID = G` (e.g. a split child) entering `OutboundGateway`:
`save G` → `set corrID = fresh` → `Exchange` (registry keyed by the unique `fresh`, collision-free) → `restore G` on
the reply → forward downstream. Downstream sees the reply carrying `G`, so an aggregator regrouping on `G` still works.

## 5. Testing posture (per CLAUDE.md + custom skills)

- **Blackbox `_test` package**, exported surface only; **assert-closure tables** (`table-test`); `t.Context()`;
  `Example` tests for both `Gateway.Request` and `OutboundGateway` (they double as godoc).
- **`use-mockgen`** for a `RequestReplyExchange` test double where a façade is tested in isolation from channels.
- **goleak** at package scope proves zero goroutine leaks (the design owns none, but the async-channel path is asserted).
- **`-race`** under concurrent `Request`s sharing one exchange.
- **Hot-path branch coverage (mandatory gate)** — a covering case for each: reply arrives (sync + async); ctx cancelled
  pre-send and mid-wait; reply timeout fires; wrong reply payload type → `ErrPayloadType`; gateway closed → new request
  and pending waiter both `ErrGatewayClosed`; `request.Send` error propagates + deregisters; unmatched reply (drop+log)
  and unmatched reply → sink; construction errors (`ErrNilChannel`, `ErrNilExchange`, `ErrInvalidReplyTimeout`);
  outbound save/restore **with** a pre-existing correlation id **and without** one; outbound-after-splitter
  (shared-corr-id children get unique registry keys). Target ≥ 85% on the changed core package.

## 6. Sequencing (Plan 019, phased tasks, ADR-linked + audited)

One plan (`docs/plans/019-messaging-gateway.md`), TDD red→green per task, each a green committable unit:

1. **Errors + `replyCorrelator`** — sentinels; the registry + reply-receiver `MessageHandler`; register/deregister;
   unmatched handling. Unit-tested directly through the exchange in later tasks.
2. **`RequestReplyExchange` SPI + `ChannelExchange`** — construction/validation, `Exchange`, `Close`, options,
   subscribe-on-reply-channel wiring.
3. **Inbound `Gateway[Req,Rep]`** — box/unbox, fresh-id mint, `ErrPayloadType`, options.
4. **`OutboundGateway` Step** — save/restore, forward-to-next, error propagation.
5. **Examples + package doc + whole-branch gate** — `Example` tests, godoc on every exported symbol, `/code-review` +
   `/security-review` over `main..HEAD`, `-race`/lint/`gofmt`/`CGO_ENABLED=0`/`go mod tidy`/`govulncheck`, coverage.

## 7. Risks

- **Reply-channel mis-wiring** (flow doesn't terminate `To(replyChannel)`, or the reply loses `HeaderCorrelationID`) →
  every request times out. Mitigation: documented wiring contract + an `Example` showing the canonical flow; the
  warn-log on unmatched/absent replies makes the failure observable rather than silent.
- **Correlation-id reuse collision** (an unrelated aggregator on the same flow keying off `HeaderCorrelationID`).
  Mitigation: the save/restore rule + fresh-id mint keep the gateway's id local to its exchange; documented that a flow
  needing an independent aggregation key uses `WithCorrelationStrategy` on that aggregator.
- **Slow/absent reply producer** holds a waiter until the 30s default. Mitigation: `min(ctx, timeout)` bounds it;
  `Close()` fails pending waiters at shutdown.

## 8. Open items (to close in ADR 0022 + Plan 019)

- **Convenience constructor?** Whether to add `NewChannelGateway[Req,Rep](request, reply, opts...)` sugar over
  `NewChannelExchange` + `NewGateway`, or keep the explicit two-step composition. Lean: keep explicit for v1 (one obvious
  way); revisit if the two-step proves noisy. Non-breaking either way.
- **Outbound Step naming** — `OutboundGateway` (chosen, EIP-faithful) vs `RequestReply`. Confirm in the plan.
- **`Close` on the inbound `Gateway`?** The exchange owns lifecycle/`Close`; the gateway is a stateless façade over it,
  so no separate `Gateway.Close` is planned. Confirm no shutdown gap.
- **Header removal on `Message`/`Headers`.** The outbound save/restore (§3.4, no-incoming-id case) requires a way to
  return a message with a header removed (`Message.WithoutHeader(key)` / `Headers.without(key)`). This is a small,
  additive public-API extension to `message.go` that lands with Plan 019 Task 4; confirm the name/shape in the plan
  (additive → minor SemVer, no break).

## 9. Traceability

Spec 010 realizes [Spec 001](001-messaging-core.md) §1 (the deferred **Messaging Gateway**) and un-defers
[Spec 003](003-composition-endpoints.md) §2's "next increment" non-goal. New **ADR 0022** (gateway +
`RequestReplyExchange` model, correlation reuse + save/restore, defaults) will cite this spec; **Plan 019** implements
it and links back to Spec 010 + ADR 0022 + the composition ADRs (0013/0001). Code/commits carry `Spec: 010`,
`Plan: 019`, `ADR: 0022` trailers.
