# Spec 012 — Panic-safe `ChannelExchange` cleanup (correlator-slot leak on a panicking flow handler)

- **Status:** **Backlog / follow-up increment** (2026-07-21) — filed, not scheduled. **No design cycle has run yet**:
  this document records a *known defect* precisely enough to pick up cold, and deliberately stops short of choosing a
  fix. Per CLAUDE.md this is a **core** change to a shipped, reviewed primitive, so it needs its own full cycle —
  `superpowers:brainstorming` → spec revision → ADR (amending or superseding [ADR 0022](../adrs/0022-messaging-gateway.md))
  → plan → **adversarial Opus audit of the complete bundle** → SDD implementation — before any code.
- **Surfaced by:** [Spec 011](011-http-adapter.md) Phase 1 (the HTTP inbound adapter, [Plan 020](../plans/020-http-adapter-inbound.md)),
  whose handler cores added a panic-recovery boundary and found that the recover **contains the panic but cannot
  reclaim the exchange's reply-waiter slot**. Recorded there as
  [ADR 0023 Addendum A5](../adrs/0023-http-channel-adapter.md#a5--panic-recovery-at-both-handler-cores-fault-isolation-and-the-residual-it-cannot-fix).
- **Owns:** the core file `exchange.go` (`ChannelExchange`), shipped by [Spec 010](010-messaging-gateway.md) /
  [ADR 0022](../adrs/0022-messaging-gateway.md) / [Plan 019](../plans/019-messaging-gateway.md).

## 1. The defect

`ChannelExchange.Exchange` (`exchange.go`) registers its reply waiter **before** it sends the request, and its
`giveUp` cleanup is invoked **non-`defer`red**, once per abandonment arm (send error, `ctx` done, reply timeout):

```go
slot, deregister, err := e.corr.register(id)   // waiter registered FIRST
if err != nil { … }
if err := e.request.Send(ctx, req); err != nil {
    e.giveUp(ctx, slot, deregister)            // cleanup only on the ERROR return
    return Message[any]{}, err
}
```

If `e.request.Send` **panics**, the panic unwinds straight past every `giveUp` call site. The correlator keeps the map
entry `HeaderCorrelationID → slot` and its `cap 1` channel **forever** — nothing else ever removes it, since the only
removal paths are a delivered reply, one of the three `giveUp` arms, or `Close()`.

This is reachable in ordinary use: `ChannelExchange`'s request channel is a `DirectChannel`, whose `Send` runs the
subscriber chain **synchronously on the caller's goroutine**. A panicking application handler is therefore a panicking
`Send`. Recovering downstream (as `adapter/http`'s `ServeAsync`/`ServeGateway` now do) does not help — the slot was
already abandoned by the time the recover runs, and the recovering frame holds neither `slot` nor `deregister`.

## 2. Proven reproduction

Delivered as part of Spec 011 Phase 1 (`adapter/http/inbound_test.go`,
`TestServeGateway_panickingFlowIsContainedEndToEnd` and the `handlerPanicValue` table arms): wire a real
`msgin.ChannelExchange` over a request/reply `DirectChannel` pair whose subscriber panics, drive it through
`msghttp.ServeGateway`, and observe (a) the adapter answers a clean `500` and the server keeps serving — the panic is
contained — while (b) the exchange's correlator retains the request's entry. Repeating the request grows the
correlator monotonically until `Close()`.

The core-side minimal repro needs no adapter at all: subscribe a panicking handler to the request channel, call
`Exchange`, recover in the test, and assert the correlator still holds the id (today: it does).

## 3. Impact — and its limits

Stated honestly in both directions; this is a **real leak** but a narrow one:

- **Trigger requires a panicking message handler** — a bug in the *consumer's* own flow code, not something a remote
  client can induce directly.
- **Default path: memory only.** Every `msghttp` request mints a fresh 128-bit-CSPRNG correlation key
  ([ADR 0023 Addendum A2](../adrs/0023-http-channel-adapter.md#a2--correlation-id-always-server-minted-advisory-and-trusted-split-security-reversal)),
  so a leaked slot is never re-keyed and can never collide with a later request. Cost = one map entry + one `cap 1`
  channel per panic, until the exchange is `Close`d.
- **Poisoning variant needs an opt-in.** A leaked slot only breaks *later* requests if a subsequent request reuses the
  same correlation key — which requires `msghttp.WithTrustedCorrelationID` (or a direct `Exchange` caller that reuses
  ids). Then each reuse fails `ErrDuplicateCorrelation` → `409`, permanently, for that key.
- **In-process only.** `ChannelExchange` is single-process by construction (Spec 010 §8.1); there is no cross-instance
  amplification.
- **Not a data-loss bug:** no message is lost or double-delivered; a reply that arrives for a leaked slot is claimed by
  the slot and simply never read (it is not routed to the unmatched sink either, since no one is giving up).

## 4. Why it was scoped out of the HTTP branch

The user deliberately kept the HTTP adapter increment (Spec 011 Phase 1) free of core changes:

- Spec 011's whole premise is that **the core does not change** — every mode wires to an existing SPI (Open/Closed).
  Amending `exchange.go` inside that branch would have broken the increment's stated invariant and widened its
  `/code-review` + `/security-review` surface to a shipped core primitive.
- The defect is **pre-existing** (introduced with `ChannelExchange` in Plan 019, merged long before this branch), not a
  regression the HTTP work caused. Phase 1 only *surfaced* it.
- The adapter can and does **contain** the blast radius (clean `500`, server survives), so nothing shipped in an
  unsafe state — only in a leaky one, documented at the recovery site and in Spec 011 §3.3.
- A correctness fix to the correlator touches the exact `giveUp`/`deliver`/`closeAll` interleaving that Plan 019's
  round-2 Opus audit hand-traced as leak-free. It deserves that same scrutiny, not a drive-by patch.

## 5. What the increment must cover (open — for the design cycle, not decided here)

- **Where the fix belongs: the core (`exchange.go`).** No adapter-side mitigation can work — the abandonment state
  (`slot`, `deregister`) is `Exchange`-local. Any future external `RequestReplyExchange` implementation would hit the
  same class of bug independently, so the *contract* ("`Exchange` must not leak a waiter on any unwind") may belong in
  the SPI godoc, not only in this implementation.
- **The obvious candidate — `defer` the cleanup — is not obviously correct.** `giveUp` today runs on exactly three
  arms and, on the success arm, must **not** run (the slot was legitimately consumed). A deferred variant must
  distinguish "settled" from "abandoned" without reintroducing the double-claim/lost-reply races
  [ADR 0022](../adrs/0022-messaging-gateway.md) G4/N2 closed, and must decide what a *panicking* abandonment does with
  a reply that raced in (route to the unmatched sink? drop?) — a policy question, not a mechanical one.
- **Panic transparency.** Whatever cleanup runs must **re-panic**: `Exchange` must never swallow a consumer panic into
  an error return (that would hide the consumer's bug and change `Exchange`'s contract).
- **Test obligation.** A panicking-handler leak test at the core level (asserting the correlator is empty after a
  recovered panic), plus the existing interleaving tests re-run under `-race`; per CLAUDE.md every branch the fix adds
  needs a covering case.
- **API/SemVer.** Expected to be behavior-only (no exported symbol changes) → patch/minor; confirm during the cycle.

## 6. Traceability

- **Defect in:** `exchange.go` (`ChannelExchange.Exchange` / `giveUp`) — [Spec 010](010-messaging-gateway.md),
  [ADR 0022](../adrs/0022-messaging-gateway.md), [Plan 019](../plans/019-messaging-gateway.md).
- **Surfaced by:** [Spec 011 §3.3](011-http-adapter.md) / [ADR 0023 Addendum A5](../adrs/0023-http-channel-adapter.md#a5--panic-recovery-at-both-handler-cores-fault-isolation-and-the-residual-it-cannot-fix)
  / [Plan 020 "Delivered — outcome and deviations"](../plans/020-http-adapter-inbound.md#delivered--outcome-and-deviations).
- **Blocks nothing** — Spec 011 Phases 2–5 may proceed independently. The `adapter/http` recover boundary must **not**
  be removed until this lands.
