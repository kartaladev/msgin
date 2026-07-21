# Spec 012 — Panic-safe `ChannelExchange` cleanup (correlator-slot leak on a panicking flow handler)

- **Status:** **Designed (2026-07-21)** — the design cycle has run (`superpowers:brainstorming`); the fix is decided and
  recorded below. Superseded state: this document was filed on 2026-07-21 as a *backlog defect report* that deliberately
  stopped short of choosing a fix; §5 of that version listed the open questions, all of which §5–§7 below now answer.
  Governing decisions: [ADR 0022 Addendum A](../adrs/0022-messaging-gateway.md#addendum-a--panic-safe-cleanup-2026-07-21).
  Implementation: [Plan 021](../plans/021-exchange-panic-safe-cleanup.md). Per CLAUDE.md this is a **core** change to a
  shipped, reviewed primitive, so the complete bundle (this spec + ADR 0022 Addendum A + Plan 021) went through a
  **two-round adversarial Opus audit** before any code was written. **Design gate CLEARED.**
  - **Round 1 — NEEDS-REVISION.** **H-1** `deregister` deletes by id without slot identity, so a reused id lets one
    caller evict another's slot: a committed reply is **silently dropped** (a G4 violation) and the victim's slot is
    **orphaned**, blocking its owner forever in `giveUp` — worse than the leak being fixed, and reachable *because*
    this increment makes id reuse succeed. Fixed as §5.1 / ADR Addendum A2, promoted to Plan Task 1.
    **H-2** no concurrent test of the panic-unwind drain (→ §6 case 5). **M-1** the injected `*slog.Logger` is on the
    deferred path in the *default* config, not just the opt-in sink (→ §5.3). **M-2** the core amplifies the leak with
    no adapter involved — `safeHandle` → `ErrHandlerPanic` → *transient* → **retry**, a fresh id per attempt (→ §3).
    **M-3** `WithPayload` is package-level, not a method. **L-1**–**L-5** folded.
  - **Round 2 — SOUND-WITH-NITS, no must-fix in the design.** All ten round-1 findings CONFIRMED-FIXED; identity-checked
    `deregister` independently re-traced against `deliver`/`closeAll`/`register`/`giveUp` with **no** remaining hang,
    silent drop, or orphan, and no new leak. Every round-2 finding was in the plan's *test snippets*:
    **H-1n** a per-iteration barrier closed twice (a `close of closed channel` masquerading as the flow's panic),
    **M-1n** `require`/`t.Fatal` called from spawned goroutines (invalid — `Goexit` masks the real failure behind a
    `goleak` storm), **M-2n** the silent-drop half of H-1 was untested (→ reply accounting), **M-3n** the
    `s != slot` branch is stress-covered only by construction, plus **L-1n**–**L-8n**. All folded.
- **Surfaced by:** [Spec 011](011-http-adapter.md) Phase 1 (the HTTP inbound adapter, [Plan 020](../plans/020-http-adapter-inbound.md)),
  whose handler cores added a panic-recovery boundary and found that the recover **contains the panic but cannot
  reclaim the exchange's reply-waiter slot**. Recorded there as
  [ADR 0023 Addendum A5](../adrs/0023-http-channel-adapter.md#a5--panic-recovery-at-both-handler-cores-fault-isolation-and-the-residual-it-cannot-fix).
- **Owns:** the core file `exchange.go` (`ChannelExchange` and the `RequestReplyExchange` SPI godoc), shipped by
  [Spec 010](010-messaging-gateway.md) / [ADR 0022](../adrs/0022-messaging-gateway.md) / [Plan 019](../plans/019-messaging-gateway.md).

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

**No façade change is required.** `Gateway.Request` and `OutboundGateway` delegate to `Exchange` and hold no cleanup
state of their own — `slot` and `deregister` never escape `Exchange`'s frame. Verified against `gateway.go` during the
design cycle. (The fix does reach one function beyond `Exchange`: `register`'s `deregister` closure — see §5.1.)

## 2. Proven reproduction

Delivered as part of Spec 011 Phase 1 (`adapter/http/inbound_test.go`,
`TestServeGateway_panickingFlowIsContainedEndToEnd` and the `handlerPanicValue` table arms): wire a real
`msgin.ChannelExchange` over a request/reply `DirectChannel` pair whose subscriber panics, drive it through
`msghttp.ServeGateway`, and observe (a) the adapter answers a clean `500` and the server keeps serving — the panic is
contained — while (b) the exchange's correlator retains the request's entry. Repeating the request grows the
correlator monotonically until `Close()`.

The core-side minimal repro needs no adapter at all: subscribe a panicking handler to the request channel, call
`Exchange`, recover in the test, and assert the correlator still holds the id (today: it does). §6 defines how that
assertion is made **through the exported API**, since tests are blackbox.

## 3. Impact — and its limits

Stated honestly in both directions; this is a **real leak** but a narrow one:

- **Trigger requires a panicking message handler** — a bug in the *consumer's* own flow code, not something a remote
  client can induce directly.
- **But it is reachable from the core alone, and the core *amplifies* it — no HTTP adapter required** (audit M-2).
  `consumer.go`'s `safeHandle` recovers a panicking handler into `ErrHandlerPanic`, which the settlement switch
  classifies as **transient** — so the message is Nacked and **retried**. An `OutboundGateway` inside such a handler
  mints a **fresh** `randomID()` per attempt (`gateway.go`), so a deterministically-panicking downstream flow leaks a
  **new slot on every retry**, unbounded, rather than the single entry the HTTP case produces. `pubsub.go`'s
  `safeFanOut` is a second such containment boundary. The HTTP adapter is where the defect was *noticed*, not the only
  place it bites.
- **Default path: memory only.** Every `msghttp` request mints a fresh 128-bit-CSPRNG correlation key
  ([ADR 0023 Addendum A2](../adrs/0023-http-channel-adapter.md#a2--correlation-id-always-server-minted-advisory-and-trusted-split-security-reversal)),
  so a leaked slot is never re-keyed and can never collide with a later request. Cost = one map entry + one `cap 1`
  channel per panic, until the exchange is `Close`d.
- **Poisoning variant needs an opt-in (PRE-FIX behaviour — see the note below).** A leaked slot only breaks *later*
  requests if a subsequent request reuses the same correlation key — which requires
  `msghttp.WithTrustedCorrelationID` (or a direct `Exchange` caller that reuses ids). Then each reuse fails
  `ErrDuplicateCorrelation` → `409`, permanently, for that key.

  > **After this increment this bullet no longer describes runtime behaviour.** §1–§3 are a *defect report* of the
  > pre-fix state, retained as the record of what was wrong. Post-fix the id is reclaimed, so reuse **succeeds** — which
  > removes the permanent 409 but places the id in [ADR 0022](../adrs/0022-messaging-gateway.md) audit N1's
  > sequential-reuse window. A whole-branch security review proved the resulting reply-hijack on a client-keyed
  > exchange, and proved the *same* hijack already reachable pre-fix via the **timeout** arm — so this is a fourth
  > trigger for a pre-existing opt-in-gated hazard, accepted deliberately against the unbounded leak. See
  > [ADR 0022 Addendum A4](../adrs/0022-messaging-gateway.md#a4--consequences).
- **In-process only.** `ChannelExchange` is single-process by construction (Spec 010 §8.1); there is no cross-instance
  amplification.
- **Not a data-loss bug *in this leak itself*:** no message is lost or double-delivered; a reply that arrives for a
  leaked slot is claimed by the slot and simply never read (it is not routed to the unmatched sink either, since no one
  is giving up). **§5.1's separate delete-by-id defect *is* a silent-drop bug** — a distinct, pre-existing hazard this
  increment also closes, since the fix's own "id reuse is safe now" claim is what makes it reachable.

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

## 5. The fix — one deferred, flag-guarded reconciler

`Exchange` today has **six** exit arms; a panic out of `e.request.Send` is an unhandled **seventh**:

| # | Arm | `giveUp` today | Required |
|---|-----|----------------|----------|
| 0 | empty correlation id → `ErrNoCorrelation` (returns **before** `register`) | no — no slot exists | no (audit L-5) |
| 1 | `register` error (`ErrGatewayClosed` / `ErrDuplicateCorrelation`) | no — no slot was created | no |
| 2 | `request.Send` returns an error | yes (explicit) | yes |
| 3 | `<-slot` yields a reply (`open`) | **no — and must not** | **no** |
| 4 | `<-slot` observes a closed slot (`!open`, `closeAll` ran) | no | no (slot already removed **and** closed) |
| 5 | `ctx.Done()` / `timer.Chan()` | yes (explicit) | yes |
| 6 | **panic unwinding out of `request.Send`** | **no — the defect** | **yes** |

Arm 3 is why a naive unconditional `defer e.giveUp(…)` is wrong, and why §5 of the previous revision called the obvious
fix "not obviously correct": on arm 3 the slot was legitimately consumed, so `deregister()` returns `false` (a `deliver`
already removed it) and `giveUp` would then block on `<-slot` — an emptied, never-closed channel — **forever**.

The fix is therefore a deferred reconciler **guarded by a settled flag**, replacing all three explicit call sites:

```go
slot, deregister, err := e.corr.register(id)
if err != nil {
    return Message[any]{}, err
}
settled := false
defer func() {
    if !settled {
        e.giveUp(ctx, slot, deregister)
    }
}()
if err := e.request.Send(ctx, req); err != nil {
    return Message[any]{}, err            // arm 2 — the defer reconciles
}
timer := e.clock.NewTimer(e.timeout)
defer timer.Stop()
select {
case reply, open := <-slot:
    settled = true                        // arms 3 & 4 — the slot is no longer ours
    if !open {
        return Message[any]{}, ErrGatewayClosed
    }
    return reply, nil
case <-ctx.Done():
    return Message[any]{}, ctx.Err()      // arm 5 — the defer reconciles
case <-timer.Chan():
    return Message[any]{}, ErrReplyTimeout
}
```

Five properties make this the decided design:

1. **One cleanup site, not three.** The reconciler is registered on the only path that can reach it (immediately after a
   *successful* `register`) and fires on every unwind out of that frame, ordinary or panicking. Arm 1 returns before it
   is registered, so it cannot run without a slot.
2. **`settled` is set only where the slot is provably no longer ours.** Arm 3 (`deliver` consumed it) and arm 4
   (`closeAll` removed *and* closed it) are the same `case`, and both are exactly the states in which `giveUp` must not
   run. Every other arm leaves `settled == false` and is reconciled.
3. **`giveUp`'s body is unchanged.** Its ADR 0022 G4 logic — `deregister()==true` ⇒ we still owned the slot, done;
   `==false` ⇒ a `deliver` is committed to a non-blocking `cap 1` send ⇒ drain the slot and route the reply to
   `routeUnmatched` — is already correct for arm 6. This increment **widens `giveUp`'s trigger set by one arm; it does
   not alter `giveUp`'s logic**, which is what preserves the interleaving Plan 019's round-2 audit hand-traced.
4. **`giveUp` runs at most once per call.** The flag makes the three former call sites mutually exclusive with the
   defer, so `deregister` is never invoked twice — it is *not* idempotent against a concurrent `deliver` and must not be.
5. **Panic transparency by construction.** No `recover()` is introduced anywhere in `Exchange`. The deferred function
   only reconciles state, so the panic continues unwinding with its **original value and stack**. `Exchange` never
   converts a consumer panic into an error return; its documented error set is unchanged.

### 5.1 Prerequisite — `deregister` must be identity-checked (audit H-1)

The reconciler above is only safe if `deregister()==false` genuinely implies *our* slot was taken by a `deliver` or
closed by `closeAll`. **Today it does not**, and the gap is a **permanent goroutine hang** — strictly worse than the
leak this increment set out to fix. It must be closed *in this increment*, because §7's decision to advertise
correlation-id reuse as safe is what makes the window reachable.

`register`'s `deregister` closure deletes **by id, without checking slot identity**:

```go
deregister = func() bool {
    if _, ok := c.waiters[id]; ok {   // ← matches ANY slot under this id, not necessarily OURS
        delete(c.waiters, id)
        return true
    }
    return false
}
```

**The interleaving** (two callers reusing one correlation id — the window ADR 0022 audit N1 documented for *sequential*
reuse, here reached concurrently):

1. G1 `register("X")` → slot `S1`; `waiters["X"] = S1`.
2. A reply arrives: `deliver("X", r)` takes the lock, reads `S1`, deletes `waiters["X"]`, releases the lock — and is
   **preempted before `S1 <- reply`**.
3. G2 `register("X")` finds no entry (step 2 deleted it), succeeds → `waiters["X"] = S2`.
4. G1 abandons (ctx / timeout / **panic**) → `giveUp` → `deregister()` finds `waiters["X"]` present — but it holds
   **`S2`** — deletes it and returns **`true`**. G1 returns **without draining `S1`**, so the committed reply lands in
   an unread `S1` and **vanishes silently**, violating the G4 "a delivered-but-abandoned reply is never dropped"
   guarantee. Worse, G1 has just evicted **G2's** entry.
5. G2 later abandons → `deregister()` finds no entry → `false` → `giveUp` blocks on `<-S2`. `S2` is **orphaned**: absent
   from the map, so no `deliver` can ever find it, and `closeAll` iterates the map so it can never `close` it. The
   receive is a bare `<-slot`, not a `select` on `ctx`. **G2 blocks forever, and `Close()` cannot rescue it.**

**Decision.** Make `deregister` identity-checked — a one-line tightening entirely in the spirit of the existing design:

```go
deregister = func() bool {
    if s, ok := c.waiters[id]; ok && s == slot {   // only OUR slot
        delete(c.waiters, id)
        return true
    }
    return false
}
```

**Why this closes it.** Our entry can leave the map by exactly two routes — `deliver` (which is then committed to a
non-blocking `cap 1` send) or `closeAll` (which closes the slot). With the identity check, `deregister()==false` implies
one of those two, so `<-slot` is **bounded in every case**: it either receives the committed reply or returns
immediately on the closed channel. Re-tracing the steps above: at step 4 G1 sees `S2 != S1` → `false` → drains `S1` →
routes the reply to the unmatched path (no loss); G2's entry is untouched and remains reachable by `deliver`/`closeAll`
(no hang).

**This is what makes §5.3's "bounded" claim true rather than aspirational**, and it is why this increment amends
`register` even though the rest of the correlator protocol (`deliver`, `closeAll`, `giveUp`'s body) is untouched. The
window becomes reachable precisely because this increment makes correlation-id reuse succeed where it used to fail
closed with `ErrDuplicateCorrelation` (§3's poisoning variant), and because §6's probe reuses ids deliberately.

### 5.2 Rejected alternatives

- **Register the waiter *after* `Send`** — eliminates the window by construction, but breaks the primitive's core use
  case: the request channel is a `DirectChannel` that runs the subscriber chain **synchronously on the caller's
  goroutine**, so the reply is delivered *during* `Send`. Registering afterwards would make every in-process reply
  unmatched. Rejected.
- **`recover()` inside `Exchange`, converted to a typed error** — would swallow the consumer's own bug and silently
  change `Exchange`'s contract from "propagates whatever your handler did" to "returns an error". Rejected; panic
  transparency is a stated requirement.
- **Keep the three explicit calls *and* add the deferred guard as a belt-and-braces net** — would require `deregister`
  to be safe to call twice against a concurrent `deliver`, which it is not. Rejected in favour of a single site.

### 5.3 A raced-in reply on the panic arm — uniform treatment

If the flow handler sends a reply and *then* panics, a `deliver` may already be committed to the slot when the unwind
reaches the reconciler. That reply is **drained and routed to `routeUnmatched`** — the configured
`WithUnmatchedReplySink`, else a warn-log — exactly as on the timeout/cancel arms.

Uniform treatment is chosen because the *only* way to special-case a panic unwind in Go is `recover()` + re-`panic`,
which is the rejected option above and additionally muddies the panic trace. Uniform treatment needs no panic detection
at all, and the drain is **bounded**: a committed `deliver` performs a non-blocking send into a `cap 1` slot, so
`<-slot` cannot stall the unwind.

**Documented residual — two caller-supplied hooks, not one** (audit M-1). Anything caller-supplied that runs inside
this deferred reconciler can panic *while the consumer's original panic is already unwinding*, replacing and therefore
masking it. Two such hooks are reachable from `routeUnmatched`:

- the **unmatched sink** (`WithUnmatchedReplySink`) — only when opted in; and
- the injected **`*slog.Logger`** (`WithExchangeLogger`) — on **both** branches of `routeUnmatched`, including the
  warn-log-and-drop branch that is the **default** configuration. A caller's `slog.Handler` is caller code.

Both godocs are extended to state that the hook must **neither panic nor block**, and why (audit L-5n). The blocking
half is newly consequential: `WithUnmatchedReplySink` already warned that a slow sink "delays that Exchange's return",
but after this change a blocking sink — or a blocking `slog.Handler` — stalls a **panic unwind**, which in
`adapter/http` parks a request goroutine that `recoverHandler` can never reach. Same clause on `WithExchangeLogger`.

## 6. Test obligation — proving a leak through a blackbox API

Per CLAUDE.md all tests are blackbox `package msgin_test` and exercise only the exported surface. `replyCorrelator` is
unexported, so "the slot leaked" cannot be asserted directly. It has a precise **public-surface probe**:
`ErrDuplicateCorrelation`.

> After a recovered panic on correlation id `X`, a second `Exchange` with the same `X` returns
> `ErrDuplicateCorrelation` **today**; after the fix it must proceed normally.

That single assertion is a non-whitebox proof of slot reclamation, and it is a genuine caller-visible behaviour change
(the poisoning variant of §3), not a test-only contrivance.

**On the [ADR 0022 audit N1](../adrs/0022-messaging-gateway.md) sequential-reuse caveat** (audit L-7n): reusing an id is
safe in these cases, but for two different reasons, and the distinction matters when adapting them. In **case 2** the
panicking handler never sends a reply at all, so no late reply exists to be misrouted. In **cases 3 and 5** a reply *is*
sent — they are safe because the prior reply is fully **settled** (delivered, or drained to the unmatched sink) before
the id is reused: case 3 is entirely synchronous, and case 5 joins its worker goroutines before the second drive. A new
case that reuses an id without settling the prior reply first would be genuinely unsafe.

Required cases (folded into the existing `exchange_test.go` assert-closure tables per the `table-test` skill):

1. **Panic propagates unchanged** — a panicking request-channel subscriber; the test `recover()`s and asserts the
   recovered value is **identical** to the one panicked (proves no `recover`/re-`panic` laundering).
2. **Slot reclaimed after a panic** — same setup, then re-`Exchange` with the *same* correlation id and assert it is
   **not** `ErrDuplicateCorrelation` (the §6 probe). This is the case that fails today.
3. **Raced reply on the panic arm, synchronous** — a handler that sends a reply and *then* panics; assert the reply
   reaches a `WithUnmatchedReplySink` **and** the panic still propagates (covers §5.3's drain arm under unwind).
4. **Regression cover for the deleted explicit calls** — the send-error, `ctx`-cancel and reply-timeout arms still
   reclaim their slot (same probe as case 2), and the success arm still returns its reply without stalling (arm 3 must
   never reach `giveUp`).
5. **Raced reply on the panic arm, CONCURRENT** (audit H-2) — cases 1–4 are all single-goroutine and deterministic, so
   none of them actually exercises the claim the fix rests on: that the drain cannot stall when a `deliver` on **another**
   goroutine races the unwind. A handler that hands the message to an async worker and *then* panics, looped ~30
   iterations from a shared barrier — the same shape as the existing `TestChannelExchange_timeoutRacesDelivery`, which
   is the timeout arm's counterpart and the precedent ADR 0022's audit G2 set for exactly this reason. Assert per
   iteration: the panic value is unchanged, the reply is accounted for on whichever arm won, the id is reusable, and
   **the test terminates** — a hang here is the §5.1 signal.
6. **Identity-checked `deregister`** (audit H-1) — the §5.1 hazard, driven through the exported API: two concurrent
   `Exchange` calls reusing one correlation id against a flow whose reply is delivered from another goroutine. Assert
   no reply is silently dropped (it reaches the unmatched sink) and **neither call hangs**. This is the case that
   distinguishes the identity-checked `deregister` from today's delete-by-id, and it must be run under `-race` and
   with a test timeout so a regression fails loudly instead of wedging CI.
7. **Identity-checked `deregister`, deterministic** (Task 1 review) — the deterministic counterpart to case 6:
   `TestChannelExchange_reusedIDAbandon_drainsOwnReply` scripts the request channel so caller A's send-error `giveUp`
   deterministically finds caller B's slot registered under A's reused id, with no reliance on a scheduler
   preemption. Asserts the same two things as case 6 — A's committed reply reaches the unmatched sink, and B's slot
   is never orphaned (B returns normally once released) — but forces the exact ordering instead of stressing for it.

The whole existing `exchange.go` / `gateway.go` suite re-runs under `-race`, and both `goleak` `VerifyTestMain` guards
stay in force. Coverage on the changed package must hold at its current level (Plan 019 shipped 99.1%, 100% on the
exchange hot paths); every arm in the §5 table has a covering case above.

**Correction (Task 1 review).** An earlier revision of this section claimed `deregister`'s `ok && s != slot` arm
could not be forced through the exported API and was stress-covered only. **That was wrong.** The arm does not need
a preemption inside `deliver` — it needs only that `deliver` complete, a second `register` land under the same id,
and the first caller then reach `giveUp`. `Exchange`'s **send-error arm** reaches `giveUp` with no `select` race at
all, and `MessageChannel` is an exported interface, so a scripted request channel drives the entire ordering
deterministically. `TestChannelExchange_reusedIDAbandon_drainsOwnReply` does exactly that: it fails on
delete-by-id and passes on the identity check, catching **both** halves of §5.1 — the silent drop (assertion) and
the orphaned slot (`goleak`). The stress test is retained as complementary coverage of the concurrent hang path.
No test-only internal seam was needed.

## 7. Contract, API surface and downstream reconciliation

- **SPI contract.** The obligation is written into the **`RequestReplyExchange` godoc**, not left as a `ChannelExchange`
  implementation detail: an implementation must release all request-scoped state on **every** exit path, including a
  panic unwind. This binds [Spec 011 Phase 2's `NewExchange` (O2)](011-http-adapter.md#34-phase-2--outbound-client-o1-o2--the-return-address-boundary)
  — the second implementation of this SPI, which will hold its own request-scoped state (an in-flight `*http.Request`,
  a response body to close) and can hit the same class of bug independently — and any future NATS/external one.
  A shared exported conformance-test helper was considered and **deferred**: it would add exported API surface and a new
  package decision for a second implementation that does not exist yet.
- **API/SemVer: patch.** Behaviour and godoc only; **no exported symbol is added, removed or changed**. Confirm with
  `gorelease`/`apidiff` during the gate, as the library quality gates require.
- **Dependencies: none added.** Pure stdlib control flow inside existing functions.
- **Scope note — `register` is amended too.** §5.1 (audit H-1) makes `deregister` identity-checked. That is a change to
  the correlator protocol, not only to `Exchange`, and it is **in scope for this increment** because §5.3's bounded-drain
  guarantee and §7's "id reuse is now safe" claim are both false without it. `deliver`, `closeAll` and `giveUp`'s body
  remain untouched.
- **Downstream doc reconciliation.** `adapter/http/inbound.go` — there is **one** shared `recoverHandler`, with **two
  residual notes**: the `NOTE:` block in `recoverHandler`'s own godoc and the paragraph in `ServeGateway`'s godoc
  (audit L-3) — plus the `inbound_test.go` comment that records the leak as deliberately un-asserted, and
  [Spec 011 §3.3](011-http-adapter.md) / [ADR 0023 Addendum A5](../adrs/0023-http-channel-adapter.md#a5--panic-recovery-at-both-handler-cores-fault-isolation-and-the-residual-it-cannot-fix)
  all currently describe the residual as **unfixed**; they are corrected to point at this spec as its resolution.
- **The `adapter/http` `recover()` boundary stays.** It is still required fault isolation — a panicking flow handler must
  not kill the HTTP server, and that is orthogonal to slot reclamation. Only its "…and cannot reclaim the slot" caveat
  is removed.

## 8. Traceability

- **Defect in:** `exchange.go` (`ChannelExchange.Exchange` / `giveUp`) — [Spec 010](010-messaging-gateway.md),
  [ADR 0022](../adrs/0022-messaging-gateway.md), [Plan 019](../plans/019-messaging-gateway.md).
- **Surfaced by:** [Spec 011 §3.3](011-http-adapter.md) / [ADR 0023 Addendum A5](../adrs/0023-http-channel-adapter.md#a5--panic-recovery-at-both-handler-cores-fault-isolation-and-the-residual-it-cannot-fix)
  / [Plan 020 "Delivered — outcome and deviations"](../plans/020-http-adapter-inbound.md#delivered--outcome-and-deviations).
- **Decided by:** [ADR 0022 Addendum A](../adrs/0022-messaging-gateway.md#addendum-a--panic-safe-cleanup-2026-07-21).
- **Realized by:** [Plan 021](../plans/021-exchange-panic-safe-cleanup.md).
- **Blocks nothing** — Spec 011 Phases 2–5 may proceed independently, and Phase 2 inherits the §7 SPI contract.
