# ADR 0022 — Messaging Gateway & the `RequestReplyExchange` model (in-process request-reply, shared reply correlator)

- **Status:** Proposed (2026-07-21) — records the architectural decisions of
  [Spec 010](../specs/010-messaging-gateway.md), settled with the user in brainstorming (request-reply only; inbound +
  outbound; in-process channel-pair + pluggable SPI; reuse `HeaderCorrelationID` + outbound save/restore; 30s reply
  timeout; warn-log+drop / opt-in sink for unmatched replies). **Adversarial design audit round 1 folded** (Opus,
  NEEDS-REVISION → revised): G1 correlator uniqueness guard (`ErrDuplicateCorrelation`) + empty-id `ErrNoCorrelation`
  (§2/§7); G2 async cross-goroutine + concurrent `-race` tests added to Plan 019 (§2); G3 async-wiring language
  corrected — reply channel is `DirectChannel`-only (§3); G4 give-up drains a delivered-but-abandoned reply to the
  unmatched path (§2); G5 outbound uses raw header presence (§4). **Round-2 re-audit: SOUND-WITH-NITS, no must-fix** —
  all five fixes CONFIRMED-FIXED (the `giveUp` blocking-drain proven leak-free across every deliver/closeAll/timeout
  interleaving); N1 (correlation-id uniqueness is a lifetime contract — §2), N2 (a defensive `giveUp` arm gets a
  covering test — Plan 019), N3 (guards are direct-caller only) folded. **Design gate cleared; ready to implement
  (Plan 019).**
- **Spec:** [Spec 010 — Messaging Gateway](../specs/010-messaging-gateway.md).
- **Depends on / builds on:** [ADR 0013 — Composition endpoints](0013-composition-endpoints.md) (the
  `MessageHandler`/`MessageChannel`/`Step`/`Chain` backbone + the synchronous-`DirectChannel` error model both
  gateways compose into; the `To(sink)` terminal), [ADR 0001 — Payload typing](0001-message-payload-typing.md)
  (`Message[T]` on the caller boundary, non-generic `Message[any]` on the SPI; `boxMessage`/`WithPayload`/
  `Payload().(Rep)` reused; `ErrPayloadType`), [ADR 0002 — Adapter SPI](0002-adapter-spi.md) (the "narrow non-generic
  public SPI, runtime owns typing" precedent this ADR follows for `RequestReplyExchange`), and
  [ADR 0004 — clockwork](0004-clockwork-dependency.md) (injectable time for the reply timeout). The reply-correlation
  header is the same `HeaderCorrelationID` the Aggregator established ([ADR 0020](0020-splitter-aggregator-group-store.md)).

## Context

Every endpoint msgin ships (Transformer, Filter, Router, Splitter, Aggregator, Service Activator) is **one-way**: a
message flows through and is consumed; the caller gets nothing back. The **Messaging Gateway** (EIP) is the pattern
that hides the messaging system behind an ordinary synchronous call — application code calls a typed method, msgin
turns it into a `Message`, drives it through a flow, and **blocks for the correlated reply**. Spec 003 built the
linear endpoints and explicitly deferred the gateway to "the increment after" because it needs machinery none of the
one-way endpoints do: a **reply-channel registry + correlation** that wakes exactly the blocked caller's goroutine
with exactly its reply, under concurrency, with a timeout and clean-up.

EIP names two mirror-image endpoints — an **inbound** gateway (application → flow → reply to caller) and an
**outbound** gateway (in-flow: send a request out, block for the reply, forward it downstream). The user chose to build
**both**, and to target the outbound one at an **in-process channel pair behind a pluggable SPI** so a future external
transport (HTTP/NATS request-reply) drops in without a core change. That choice is what lets both gateways collapse
onto a single primitive.

## Decision

### 1. `RequestReplyExchange` — the SPI both gateways delegate to (Spec §3.1)

```go
type RequestReplyExchange interface {
    Exchange(ctx context.Context, req Message[any]) (Message[any], error)
}
```

Non-generic over `Message[any]`, following the ADR 0002 rule (the caller boundary owns generics; the SPI is
type-agnostic). One shipped implementation, `ChannelExchange` (§3). An HTTP/NATS adapter implements `Exchange` for a
real external round-trip; **both** façades (§4) then obtain an external gateway with no core change (Open/Closed). This
is the seam that makes the whole increment adapter-ready.

### 2. `replyCorrelator` — a shared, zero-goroutine reply registry (Spec §3.2)

An unexported primitive both façades reach through the exchange:

- A `sync.Mutex`-guarded `map[string]chan Message[any]`, each value a **one-shot, cap-1** slot keyed by the request's
  `HeaderCorrelationID`. `register(id) → (slot, deregister, err)` **enforces uniqueness (audit G1):** a duplicate
  in-flight id returns `ErrDuplicateCorrelation`, a closed correlator returns `ErrGatewayClosed`; `Exchange` rejects an
  empty id with `ErrNoCorrelation` first. Without this guard two concurrent requests sharing an id would overwrite each
  other's slot and cross-delete on deregister (orphaning waiters) — a silent-corruption footgun in a *public* SPI that
  the spec's own scatter-gather framing makes reachable. `deregister` returns whether the waiter still owned the slot.
  Uniqueness is a **lifetime** contract, not merely concurrent (audit N1): a sequentially-reused id can receive a prior
  abandoned request's late reply; the façades mint fresh ids so only hand-rolled direct callers can hit it.
- A `MessageHandler` (the reply receiver) `Subscribe`d onto the **reply channel**. Per reply: read
  `HeaderCorrelationID`; a matching slot → remove-under-lock then non-blocking send into the cap-1 slot; no slot →
  **unmatched** (§5). It returns `nil` from `Handle` even for an unmatched reply, so a reply producer is never failed by
  an absent waiter.
- **Zero owned goroutines.** The receiver runs on whatever goroutine drives the reply channel (the caller's own
  goroutine for a synchronous `DirectChannel`; a driving Consumer worker when `reply.Send` runs on another goroutine);
  each waiter blocks on its own goroutine. There is no background loop to leak — a strong outcome for the `goleak` gate
  and graceful shutdown. (The async path's concurrency is verified by dedicated cross-goroutine + `-race` tests per
  audit G2 — the primitive's headline claim is not left to the synchronous path alone.)

The waiter is `select { case rep := <-slot; case <-ctx.Done(); case <-timer.Chan() }` where `timer` comes from the
injected `clockwork.Clock`, returning the reply / `ctx.Err()` / `ErrReplyTimeout`. On the ctx/timeout/send-error exits
it **reconciles with a possibly-concurrent deliver (audit G4):** if `deregister` reports the slot was already claimed by
a deliver, the waiter drains it and routes the delivered-but-abandoned reply to the unmatched path (§5) instead of
letting it vanish — correctness insurance for a future at-least-once external exchange.

**Why reuse `HeaderCorrelationID` (not a dedicated header).** Surfaced the collision trade-off to the user; they chose
reuse because it is EIP-canonical for scatter-gather (gateway → splitter → aggregator all key off one id). The cost is
the outbound save/restore rule (§4); it is contained and tested. A dedicated header was the safer default but the user
deliberately took the canonical route.

### 3. `ChannelExchange` — the in-process `RequestReplyExchange` (Spec §3.3)

```go
func NewChannelExchange(request, reply MessageChannel, opts ...ExchangeOption) (*ChannelExchange, error)
func (e *ChannelExchange) Exchange(ctx context.Context, req Message[any]) (Message[any], error)
func (e *ChannelExchange) Close() error
```

- **Construction validates** (typed errors — the debuggability surface): nil `request`/`reply` → `ErrNilChannel`; an
  explicit `WithReplyTimeout(d<=0)` → `ErrInvalidReplyTimeout` (the established set-flag pattern distinguishes
  "unset → 30s default" from "explicit invalid", per `WithMaxInFlight`/`WithAttemptTTL`). It `Subscribe`s the
  correlator receiver onto `reply`; a second subscriber is the channel's own `ErrChannelSubscribed`.
- **`Exchange`**: `register` the request's `HeaderCorrelationID` → `request.Send(ctx, req)` (a send error deregisters
  and propagates) → wait for the correlated reply / ctx / timeout.
- **`Close`**: idempotent; marks closed (new `Exchange` → `ErrGatewayClosed`) and fails every pending waiter with
  `ErrGatewayClosed`. This is graceful shutdown — no in-flight waiter hangs past `Close`.
- **Options**: `WithReplyTimeout`, `WithUnmatchedReplySink`, `WithExchangeClock`, `WithExchangeLogger`.
- **Wiring contract** (documented; corrected per audit G3): request-channel subscriber = the flow; the flow ends
  `To(replyChannel)`; the exchange owns `replyChannel`. **The reply channel is a `Subscribe`-based `MessageChannel` —
  `DirectChannel` only in the current core** (`QueueChannel` is poll-based; `PublishSubscribeChannel.Subscribe` has a
  different signature — neither satisfies `MessageChannel`). Synchronous vs asynchronous is about **which goroutine
  calls `reply.Send`**, not the reply channel's type: a synchronous flow runs the whole round-trip on the caller's
  goroutine (the cap-1 slot is already full when the waiter reads it — register-before-send makes this safe); an
  asynchronous flow bridges the request path through a `QueueChannel`+`Consumer` (or any worker) that terminates in
  `To(replyDirectChannel)`, so `deliver` runs on that worker.

### 4. The two façades — inbound `Gateway[Req,Rep]` + outbound `OutboundGateway` (Spec §3.4)

**Inbound** (typed, app-facing): `NewGateway[Req,Rep](x RequestReplyExchange, …) (*Gateway[Req,Rep], error)` (nil
exchange → `ErrNilExchange`); `Request(ctx, req) (Rep, error)` mints a **fresh** id, `New[Req](req)` with
`HeaderCorrelationID`, `boxMessage` → `Exchange` → `reply.Payload().(Rep)` (→ `ErrPayloadType`). The app passes a raw
`Req` with no headers, so the inbound path always mints fresh — no collision.

**Outbound** (in-flow `Step`): `OutboundGateway(x RequestReplyExchange) Step`. `Handle(ctx, msg)`: **save** the
incoming `HeaderCorrelationID` value **and whether it was present, via the raw `Header` accessor — not `Headers.String`,
which conflates absent with present-but-non-string and would wrongly strip a present non-string id (audit G5)** → set a
**fresh** id for the exchange → `Exchange` → **restore the original correlation state** on the reply → forward to
`next`. Restore is two cases: present value `G` (any type) → forward carrying `G`; **absent** → the fresh exchange id
must **not** leak downstream, so the reply is forwarded with the correlation id **removed**. Since `Headers` is immutable/additive, this needs a small additive
`Message.WithoutHeader(key)` / `Headers.without(key)` (lands with the outbound task). The fresh-id mint gives every
concurrent in-flight exchange a **unique** registry key even when the entering messages share a correlation id (all
children of one split), while save/restore preserves an upstream aggregator's group key across the round-trip.

### 5. Unmatched / late replies — warn-log + drop by default, opt-in sink (Spec §3.5)

A reply whose `HeaderCorrelationID` has no registered waiter (already timed-out/cancelled, or unknown) is
**warn-logged** via the injected `*slog.Logger` and **dropped** (`Handle` returns `nil` — the reply producer is never
failed by waiter liveness). `WithUnmatchedReplySink(out)` instead `Send`s it to a caller `OutboundAdapter` (best-effort;
a sink error is logged, not propagated). Mirrors `WithInvalidMessageSink` — safe, observable, extensible. Returning an
error to the reply sender was rejected: it would couple a reply producer's success to a slow caller's liveness. The
same unmatched path also receives a reply that raced a timeout/cancel/close and was delivered into a slot the waiter
had already abandoned (audit G4) — and, on the synchronous path, a reply produced by a flow that then returns an error
(audit G7) — so neither vanishes silently.

### 6. Reply timeout default — 30s, `min(ctx, timeout)` (Spec §3, safe-default gate)

The reply timeout defaults to **30s**, overridable via `WithReplyTimeout`; the effective deadline is
`min(ctx deadline, reply timeout)`. Rationale (CLAUDE.md safe-default gate): a wrong default here could **hang** — a
deadline-less ctx would block a waiter (and hold a registry slot) forever — so the default fails safe with a value
comfortably above any plausible in-process round-trip, and the caller's own ctx deadline always wins when tighter.

### 7. Errors, typing, dependencies (Spec §3.6)

New exported sentinels: `ErrGatewayClosed`, `ErrReplyTimeout`, `ErrNilExchange`, `ErrNilChannel`,
`ErrInvalidReplyTimeout`, `ErrDuplicateCorrelation` (audit G1); reuse `ErrPayloadType` and `ErrNoCorrelation` (empty
correlation id, G1). ctx cancellation returns `ctx.Err()`, distinguishable from `ErrReplyTimeout`. **No new dependency**
(stdlib + `clockwork`). The additions — `RequestReplyExchange`,
`ChannelExchange`, `NewGateway`/`Gateway`, `OutboundGateway`, the options, the sentinels, `Message.WithoutHeader` — are
**purely additive** → **minor** SemVer bump; `apidiff` shows additions only.

## Consequences

**Positive.**

- **One primitive, two façades, one SPI.** Inbound and outbound gateways are thin façades over `replyCorrelator` +
  `RequestReplyExchange`; a future HTTP/NATS adapter makes both external with no core change.
- **Leak-free by construction.** Zero owned goroutines → the `goleak` gate is satisfied structurally, and graceful
  shutdown is just "fail pending waiters."
- **Scatter-gather composes for free.** Reusing `HeaderCorrelationID` means gateway + Splitter + Aggregator interoperate
  with no new endpoint.
- **Safe, overridable defaults.** 30s `min(ctx, timeout)` and warn-log+drop follow the established defaults + option
  patterns; every knob has a `WithX`.

**Negative / trade-offs.**

- **Outbound save/restore is real hot-path state.** Reusing `HeaderCorrelationID` (vs a dedicated header) buys EIP
  fidelity at the cost of save/restore logic + the `WithoutHeader` addition + their tests. Accepted by the user with
  the trade-off surfaced.
- **Reply-channel wiring is a caller contract.** A flow that forgets `To(replyChannel)` or drops `HeaderCorrelationID`
  times out every request; mitigated by an `Example` and the warn-log on unmatched/absent replies, not by a compile-time
  guarantee.
- **Reply channel is single-purpose.** The exchange owns its reply channel (one subscriber); documented, not enforced
  beyond `ErrChannelSubscribed`.

**Neutral / deferred.** One-way gateway, async/future gateway, a header-carrying `RequestMessage` variant, external
request-reply adapters, and a `NewChannelGateway` convenience constructor are all out of this increment and
non-breaking to add later (Spec §2, §8).

**Constraint carried forward — the correlator is single-process by construction; multi-instance reply routing is the
external adapter's job (Spec §8.1).** The reply correlator (§2) matches a reply through a **process-local Go channel**,
so a reply only reaches the waiter registered on the **same `ChannelExchange` instance**. This is *complete* for the
in-process topology under horizontal scaling — N instances behind a proxy each serve a request end-to-end in their own
memory, so requests/replies never cross instances and there is nothing to coordinate. It is *insufficient* the moment a
reply returns via an **external transport** to *any* instance: correlation-id-only matching lets a reply land on an
instance with no waiter (dropped) while the origin waiter times out. A future external `RequestReplyExchange` adapter
therefore **MUST implement the EIP Return Address pattern** (each instance owns a unique reply destination stamped on
the request, so the reply returns to the originating instance; correlation id matches within it). The core does not and
**must not** attempt this — a Go channel cannot cross processes; the synchronous, self-contained `Exchange` seam exists
precisely so the adapter encapsulates return-address + reply-demux. This constraint is recorded now so the
external-adapter increment designs it in from the start (see CLAUDE.md → *Production robustness → Multi-instance /
distributed-deployment awareness*).

**Known defect against this decision (filed 2026-07-21, not yet fixed).** `ChannelExchange.Exchange` registers its
reply waiter **before** it sends the request and calls its `giveUp` cleanup **non-`defer`red**, so a **panicking**
request-channel subscriber (a `DirectChannel` runs it synchronously on the caller's goroutine) unwinds past every
cleanup arm and leaks the correlator entry + slot channel until `Close()`. Surfaced — and contained, not fixed — by
Spec 011's HTTP inbound adapter ([ADR 0023 Addendum A5](0023-http-channel-adapter.md#a5--panic-recovery-at-both-handler-cores-fault-isolation-and-the-residual-it-cannot-fix)).
Impact, limits, and the design obligations for the fix are captured in
[Spec 012 — Panic-safe `ChannelExchange` cleanup](../specs/012-exchange-panic-safe-cleanup.md); it is a **core** change
requiring its own design cycle, and this ADR will be amended or superseded by that increment.
