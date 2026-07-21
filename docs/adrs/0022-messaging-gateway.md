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
  (Plan 019).** **Amended 2026-07-21 by [Addendum A](#addendum-a--panic-safe-cleanup-2026-07-21)** (Spec 012 /
  Plan 021) — give-up cleanup moves to a single deferred, `settled`-guarded reconciler so a panicking flow handler can
  no longer leak a correlator slot (A1), `deregister` becomes identity-checked to close a silent-drop + permanent-hang
  window the reuse-is-now-safe claim would otherwise expose (A2), and the no-leak-on-unwind obligation is written into
  the `RequestReplyExchange` SPI contract (A3). Not superseded; every decision below still stands.
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

**Known defect against this decision (filed 2026-07-21, FIXED by Addendum A below).** `ChannelExchange.Exchange`
registered its reply waiter **before** it sent the request and called its `giveUp` cleanup **non-`defer`red**, so a
**panicking** request-channel subscriber (a `DirectChannel` runs it synchronously on the caller's goroutine) unwound
past every cleanup arm and leaked the correlator entry + slot channel until `Close()`. Surfaced — and contained, not
fixed — by Spec 011's HTTP inbound adapter ([ADR 0023 Addendum A5](0023-http-channel-adapter.md#a5--panic-recovery-at-both-handler-cores-fault-isolation-and-the-residual-it-cannot-fix)).
Impact, limits, and the design obligations for the fix are captured in
[Spec 012 — Panic-safe `ChannelExchange` cleanup](../specs/012-exchange-panic-safe-cleanup.md). It is a **core** change,
so it ran its own design cycle; the decision it reached is recorded in **Addendum A**.

---

## Addendum A — panic-safe cleanup (2026-07-21)

- **Status:** Accepted (2026-07-21). Amends §2 (`replyCorrelator` / give-up reconciliation) and §1
  (`RequestReplyExchange` contract) of this ADR. **Does not supersede it** — every decision above stands; A1 changes
  *where* the existing `giveUp` is invoked, and A2 adds an obligation to the SPI contract.
- **Driven by:** [Spec 012](../specs/012-exchange-panic-safe-cleanup.md) — realized by
  [Plan 021](../plans/021-exchange-panic-safe-cleanup.md).
- **Design gate:** two-round adversarial Opus audit of the complete bundle (spec + this addendum + plan).
  **Round 1 NEEDS-REVISION** — A2 exists *because* of round-1 finding H-1: A1 alone would have traded a memory leak for
  a permanently blocked goroutine. **Round 2 SOUND-WITH-NITS**, no must-fix in the design; identity-checked `deregister`
  was independently re-traced against `deliver`/`closeAll`/`register`/`giveUp` with no remaining hang, silent drop or
  orphan. Full finding list in Spec 012's status block.

### A1 — cleanup moves to a single deferred, `settled`-guarded reconciler (correctness fix)

**Context.** §2 above decided *what* give-up must do (the G4 reconciliation: `deregister()==false` ⇒ a `deliver` is
committed ⇒ drain the slot and route the reply to the unmatched path). It did **not** decide *how* that cleanup is
invoked, and the implementation placed it at three explicit call sites — one per abandonment arm. That placement is
unsound: a panic unwinding out of `request.Send` bypasses all three (Spec 012 §1).

**Decision.** `Exchange` declares `settled := false` and registers **one** deferred reconciler immediately after a
successful `register`; the three explicit `e.giveUp(…)` calls are **deleted**. `settled = true` is set **only** in the
`case reply, open := <-slot:` arm. Every other exit — send error, `ctx.Done`, reply timeout, **and a panic** — leaves
`settled == false` and is reconciled exactly once.

**Why the flag is load-bearing, and an unconditional `defer` is wrong.** On the success arm the slot was legitimately
consumed by a `deliver`, so `deregister()` returns `false` and `giveUp` would then block on `<-slot` — an emptied,
never-closed channel — **forever**. The flag is what distinguishes "settled" from "abandoned"; it is the answer to the
open question Spec 012's first revision raised.

**What is deliberately *not* changed.** `giveUp` itself, and the `register`/`deliver`/`closeAll` protocol. This
addendum **widens `giveUp`'s trigger set by one arm; it does not alter `giveUp`'s logic**, so the interleaving this
ADR's round-2 audit hand-traced as leak-free is preserved. The flag also keeps `giveUp` to **at most one invocation per
call** — `deregister` is not idempotent against a concurrent `deliver` and must not become so, which is why a
belt-and-braces "keep the explicit calls *and* add the defer" variant was rejected.

**Panic transparency (unchanged contract).** No `recover()` is introduced. The deferred function only reconciles state,
so a consumer panic keeps unwinding with its original value and stack. `Exchange` must never convert a consumer panic
into an error return; §7's documented error set is unchanged. Registering the waiter *after* `Send` — the other way to
close the window — was rejected: the request channel is a `DirectChannel` that delivers the reply synchronously
*during* `Send`, so a late registration would make every in-process reply unmatched.

**Raced-in reply on the panic arm — uniform treatment.** A reply committed before the panic is drained and routed to
`routeUnmatched`, identically to the timeout/cancel arms (§5 above), because the only way to special-case a panic
unwind is the rejected `recover()`+re-`panic`. The drain is bounded — a committed `deliver` sends non-blocking into a
`cap 1` slot — **but only once A2 makes `deregister` identity-checked**; without A2 that claim is false. **Residual, accepted and documented:** anything caller-supplied that runs inside the
reconciler can panic while the consumer's panic is already unwinding, masking it. That is **two** hooks, not one
(audit M-1): the opt-in unmatched sink, and — on **both** branches of `routeUnmatched`, including the default
warn-log-and-drop branch — the injected `*slog.Logger`. Both `WithUnmatchedReplySink` and `WithExchangeLogger` gain a
**must-not-panic-and-must-not-block** clause: after this change a blocking hook stalls a panic *unwind*, which in
`adapter/http` parks a request goroutine `recoverHandler` can never reach.

### A2 — `deregister` becomes identity-checked (correctness prerequisite for A1)

**Context.** A1's reconciler — and §2 above's G4 guarantee that a delivered-but-abandoned reply is never dropped —
both rest on the premise that `deregister()==false` means *our* slot was taken by a `deliver` (committed to a
non-blocking `cap 1` send) or closed by `closeAll`, so `giveUp`'s `<-slot` is bounded. **The shipped `register` does not
honour that premise**: its `deregister` closure deletes **by id**, matching whatever slot currently sits under that key.

**The hazard** (hand-traced in [Spec 012 §5.1](../specs/012-exchange-panic-safe-cleanup.md); audit H-1). With two
callers reusing one correlation id — the window audit N1 above documented for *sequential* reuse — a `deliver` that has
deleted the map entry but not yet sent can be interleaved with a second `register`, after which the first caller's
`deregister` deletes the **second** caller's slot and returns `true`. Two things go wrong at once: the first caller
returns without draining its own slot, so the committed reply is **silently dropped** (a direct G4 violation), and the
second caller's slot is left **orphaned** — absent from the map, so no `deliver` can reach it and `closeAll` cannot
close it. The second caller's `giveUp` then blocks on a bare `<-slot` **forever**, not selectable on `ctx` and not
rescuable by `Close()`. A permanently wedged goroutine is strictly worse than the leak A1 fixes.

**Decision.** `deregister` matches on slot identity, not just key:

```go
if s, ok := c.waiters[id]; ok && s == slot { delete(c.waiters, id); return true }
```

**Consequences.** `deregister()==false` now genuinely implies deliver-or-`closeAll`, which is what makes A1's bounded
drain true rather than aspirational, and closes both the silent drop and the hang. `deliver`, `closeAll` and `giveUp`'s
body are **unchanged**. This widens the increment beyond `Exchange` into the correlator protocol — accepted
deliberately, because **A4**'s caller-visible change (an id abandoned by a panic is no longer permanently poisoned, so
reuse now succeeds where it used to fail closed) is precisely what makes the window reachable. Shipping A1 without A2
would trade a memory leak for a liveness bug.

### A3 — the no-leak-on-unwind obligation belongs to the SPI, not to `ChannelExchange`

**Decision.** The `RequestReplyExchange` godoc (§1) states that an implementation must release **all** request-scoped
state on **every** exit path, including a panic unwind.

**Why at the SPI.** `ChannelExchange` is no longer the only implementation on the roadmap: Spec 011 Phase 2's
`NewExchange` (O2) is the second, and holds its own request-scoped state (an in-flight `*http.Request`, a response body
to close) that can leak by the same mechanism. Leaving the obligation as an implementation note would make every future
external exchange rediscover it. A shared **exported conformance-test helper** to enforce it mechanically was considered
and **deferred** — it would add exported API surface and a new package decision for a second implementation that does
not yet exist.

### A4 — consequences

- **API/SemVer: patch.** Behaviour and godoc only; no exported symbol is added, removed or changed.
- **No new dependency**; pure stdlib control flow inside existing functions.
- **Scope reaches `register`, not only `Exchange`** (A2). `deliver`, `closeAll` and `giveUp`'s body stay untouched, so
  the interleaving this ADR's round-2 audit hand-traced is preserved; A2 *narrows* when `deregister` returns `true`,
  which strictly tightens that trace rather than reopening it.
- **The `adapter/http` `recover()` boundary stays.** It remains required fault isolation (a panic must not kill the
  HTTP server), which is orthogonal to slot reclamation; only its "…and cannot reclaim the slot" caveat is removed.
  [ADR 0023 Addendum A5](0023-http-channel-adapter.md#a5--panic-recovery-at-both-handler-cores-fault-isolation-and-the-residual-it-cannot-fix)
  is annotated to point here as its resolution.
- **Caller-visible behaviour change (intended).** A correlation id abandoned by a panic is no longer permanently
  poisoned with `ErrDuplicateCorrelation`; reusing it succeeds. This is also the blackbox probe the tests use, since
  `replyCorrelator` is unexported (Spec 012 §6).
