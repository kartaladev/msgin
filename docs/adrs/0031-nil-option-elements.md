# ADR 0031 — A nil option element is a reported fault, not a panic

- **Status:** **PROPOSED (2026-08-17), revision 5 — AUDIT-CLEARED** — written before any code, per CLAUDE.md's design-time gate.
  Decisions **D-P**, **D-Q**, **D-R** were settled with the user during the Spec 015 brainstorm; **D-P was then
  materially rewritten** and **D-S/D-T added** after the round-1 adversarial audit
  ([`028-audit-round-1.md`](../plans/028-audit-round-1.md)) proved revision 1's mechanism contradicts
  `handler.go:44-51`. **Round 2** ([`028-audit-round-2.md`](../plans/028-audit-round-2.md)) re-attacked **D-S**
  hardest and it **held** — its justification below is strengthened with two hazards round 2 supplied and its "cost
  is cosmetic" claim corrected; D-T's count and membership tiebreak were corrected. **Round 3**
  ([`028-audit-round-3.md`](../plans/028-audit-round-3.md)) added **D-U** and **D-V**, both settled with the user.
  **Round 4** ([`028-audit-round-4.md`](../plans/028-audit-round-4.md))
  **compile-proved D-U and D-V** and cleared the bundle: SAFE TO IMPLEMENT, no round 5.
- **Prompted by:** [Spec 015](../specs/015-nil-option-elements.md); the backlog item in
  [`docs/HANDOVER.md`](../HANDOVER.md) §6.
- **Realized by:** [Plan 028](../plans/028-nil-option-elements.md).
- **Instance of** the `ErrNilFunc` governing invariant (`errors.go:212-221`), stated in
  [ADR 0029 §5.0b](0029-eip-lexical-alignment.md) as decision **D-M** — **except D-S**, which knowingly extends its
  third clause (§D-S).

## Context

All 32 exported functional-option constructors in the workspace apply their options with an unguarded loop, so a nil
option **element** panics. CLAUDE.md forbids a library panic on caller input without qualification.

The core already answers this exact question for the sibling variadic. `msgin.Chain`'s godoc (`handler.go:44-51`)
states **both halves** of the decision, and the second half is the one that governs this ADR:

> A nil ELEMENT is not a panic … and **is NOT skipped** … **Skipping was rejected** — a step built conditionally
> (`steps = append(steps, maybeStep())`) that came back nil is a **WIRING BUG**, and deleting it would silently change
> what the flow does.

The 32 constructors are **not one shape**. What a constructor can do about the fault is decided by what it can
surface one through: 24 return `(T, error)`; five return a product with an error-returning method; two return a
product with no error surface at all (`Message[T]`, `string`); and one — `resilience.NewCircuitBreaker` — returns an
interface with **no** error-returning method (`flowcontrol.go:42-46`) while its sibling `NewTokenBucket` already
returns `(msgin.RateLimiter, error)`. The full inventory and its regenerating commands are
[Spec 015 §2](../specs/015-nil-option-elements.md).

## Decision

### D-P (REVISED) — report through the constructor's return, else through the product's first use; skip only where neither exists

> No msgin constructor panics on a nil option element. It reports the fault through its own return if it has one,
> otherwise through the first use of the object it produced; only where neither surface exists does it skip the
> element, and then its godoc names what was lost.

| Family | Count | Mechanism | Result |
|---|---|---|---|
| **R1 — Reject at construction** | 25 | `return zero, nilOptionAt(ctor, i)` | **bare** `ErrNilFunc`; construction fails |
| **R2 — Degrade at first use** | 5 | latch the fault; every error-returning method returns it | **`Permanent(ErrNilFunc)`** naming the index |
| **R3 — Skip, documented** | 2 | `continue` + a godoc sentence naming the concrete loss | element ignored |

Position string: `"pkg.Ctor: nil option at index %d"` — the shape of `Chain`'s `"msgin.Chain: nil step at index %d"`.

**R2 is not a new invention; it is what `Chain` does.** `Chain` has no error return either, and its answer is to
replace the nil with a step that fails at dispatch. R2 applies the same answer to `routing.NewRouter` (via `Handle`),
both `channel` constructors (via `Send`/`Subscribe`/`Publish`) and `memory.New` (via `Broker.Send`/`Stream`).
`routing.Filter` already had it, through the package's `nilFuncStep`.

**R3 has exactly two members, on structural grounds, not preference.** `Message[T]`'s six methods return no error and
`DSN` returns a `string`, so there is nowhere to put a fault. Both godocs therefore name the concrete consequence —
for `msgin.New`, that a dropped `WithID` means a **fresh random id** is stamped where the caller supplied an
idempotency key.

**What revision 1 got wrong, recorded so it is not re-proposed.** It put all seven no-error-return constructors in
R3 and cited `handler.go:52-63` — the position format — while omitting lines 44-51, which reject the mechanism it
adopted, for a reason that transfers exactly. The cost was concrete: a dropped `WithID` silently randomising a
message identity that the SQL inbox deduper keys on, and a dropped `WithSingleSubscriber` voiding the
`ExclusiveSubscribable` contract so every message is processed twice. Round-1 audit, BLOCKER-2.

**Rejected alternatives** are tabulated in [Spec 015 §5](../specs/015-nil-option-elements.md#5-alternatives-rejected).
The one worth restating here: **error returns for all seven** is the simplest design to explain — two mechanisms — but
costs **103 call sites** and the inline composability Spec 014:1879 values for base constructors, to buy a signal R2
already provides at zero churn. §D-T takes the error return only where R2 is structurally impossible.

**The residual cost, stated plainly:** two constructors still swallow a caller's mistake. That is not mitigable, only
documented, and the documentation must name the consequence rather than merely note the silence.

### D-Q — reuse `msgin.ErrNilFunc`; mint no sentinel

All 22 option types are `func` types (Spec 015 §2), so "nil function" is literally accurate for every element, and
`ErrNilFunc`'s stated scope already reaches a nil *element* of a variadic function slice. Root's sentinel set is
**unchanged**. Adapters reuse a root sentinel exactly as `memory.NewQueueStore` already returns
`msgin.ErrInvalidCapacity` (`adapter/memory/queuestore.go:91`).

**Cost:** `errors.Is(err, ErrNilFunc)` no longer distinguishes a nil *behavior* (`pred`, `fn`, `Handler`) from a nil
*config option*. They remain distinguishable in the **message**, via the position, but not programmatically. A
dedicated `ErrNilOption` would restore that at the price of a 44th root sentinel for what is arguably the same fault
class; this project has consistently preferred widening an existing sentinel's documented invariant.

### D-R — every caller-facing constructor guards its own slice

Eight of the 32 do not apply their own options: `sql.NewQueueStore` forwards to `NewOutboundAdapter` **and**
`NewPollingSource`; five `msghttp` constructors and both `adapter/http/stdlib` constructors forward to
`msghttp.NewConfig`. Guarding only at the 24 apply loops would make `stdlib.NewInbound(ch, nil)` report
`msghttp.NewConfig: nil option at index 0` — a position naming a function the caller never called, which defeats
carrying a position at all.

All **32** therefore validate their own `opts`: 24 fold the check into the existing loop, 8 pre-check and forward.
**The duplicate pass is accepted** — O(len(opts)) over a slice essentially always under ten elements, off the message
path entirely, bought for a truthful position at every entry point. A future delegator that forgets its guard degrades
to the delegate's less-precise position rather than to a panic.

**Mechanism — a 3-line unexported helper per package, duplicated, not shared:**

```go
func nilOptionAt(ctor string, i int) error {
	return fmt.Errorf("%w: %s: nil option at index %d", msgin.ErrNilFunc, ctor, i)
}
```

in the **eight** packages with an R1 constructor: `endpoint`, `routing`, `resilience`, `adapter/memory`,
`adapter/http`, **`adapter/http/stdlib`**, `adapter/cron`, `adapter/database/sql`. `stdlib` needs its own because it
cannot reach `msghttp`'s unexported helper.

[Spec 014 §3.3](../specs/014-core-package-layout.md) resolved exactly this way for eight shared unexported identifiers
(`boxMessage`, `nilFuncStep`, …). **It is cited as precedent, not as a stated general rule** — it is a resolution
table for named symbols, and calling it "the inline-don't-share rule" was interpretation presented as quotation
(round-1 audit, m-12). The precedent is what rules out the alternative: a generic exported `msgin.ApplyOptions` would
need to be reachable from eight modules, making it **new exported surface in root** to solve an internal detail.

### D-S (NEW) — the latched R2 fault is `Permanent`-wrapped on every surface, extending the invariant's third clause

The `ErrNilFunc` invariant's third clause holds that an error "handed to a caller from a non-constructor API" stays
**bare**. Applied literally, `channel.Send`, `PubSub.Publish` and `Broker.Send` would return the latched fault bare.

**This ADR wraps them in `Permanent` anyway.** The round-2 audit attacked this decision hardest and it survived; the
justification below is **strengthened by that audit**, which found the original argument understated.

**Hazard 1 — unbounded consumer redelivery.** `msgin.To(sink)` calls `OutboundAdapter.Send` from a handler body it
constructs. Bare, `endpoint/consumer.go:820`'s `if msgin.IsPermanent(err)` is false → the transient path → and the
**default** `RetryPolicy{}` has `MaxAttempts == 0`, which `retry.go` documents as *"retry forever (no
dead-letter)"*. That is the unbounded hot-retry loop `endpoint/consumer.go:235-239` measures at **~46k retries on a
single message in 200 ms**.

**Hazard 2 — silent dead-lettering (found in round 2, not in the original argument).**
`endpoint.NewProducer(latchedBroker, WithProducerRetry(…))` retries at `producer.go:447-453`, where
`if msgin.IsPermanent(err) { return err }` is the only early exit. Bare, a wiring fault is retried to attempt
exhaustion and the message is **dead-lettered** — data diverted by a nil option.

**Hazard 3 — a wiring bug that looks like an outage.** `consumer.go:716` records
`c.safeRecord(md.Msg.ID(), err == nil || msgin.IsPermanent(err))`. Bare, the latched fault records the circuit
breaker **unhealthy** and trips it. Permanent records healthy.

**The cost, stated honestly:** a caller invoking `Send` directly, outside any flow, sees `IsPermanent(err) == true`
on an error handed back synchronously. *(Revision 2 called this "cosmetic". Hazard 3 shows the asymmetry runs the
other way — leaving it bare has a real behavioral cost, so the wrap is not merely the tidier of two cosmetic
choices.)* `errors.Is(err, ErrNilFunc)` matches either way.

The rejected alternative — wrap on `Router.Handle` (unambiguously a `MessageHandler`) and leave the channel/broker
surfaces bare — is more literal about the invariant but leaves **all three** hazards open on `Send`, the surface most
likely to sit inside a flow. **The invariant paragraph in `errors.go:212-221` is NOT edited**; this extension lives
here, so the divergence is visible rather than smuggled into the sentinel's godoc.

### D-T (NEW) — `resilience.NewCircuitBreaker` gains an error return

`msgin.CircuitBreaker` is `Allow() bool`, `Record(bool)`, `HalfOpen() <-chan struct{}` (`flowcontrol.go:42-46`) —
**no error-returning method**, so R2 is structurally impossible. Its sibling `resilience.NewTokenBucket` already
returns `(msgin.RateLimiter, error)`, making the current asymmetry arbitrary, and a breaker is constructed once at
wiring time, never per message. It therefore moves to **R1** with the signature
`NewCircuitBreaker(opts ...CircuitBreakerOption) (msgin.CircuitBreaker, error)`. **10 call sites** move to the
two-value form, all in `_test.go` across four files *(revision 2 said 11 — it counted the declaration)*.

**Membership tiebreak, so D-P's "structural, not preference" survives this decision.** By D-P's letter, a
constructor with no product error surface belongs in R3 — which is where `NewCircuitBreaker` would sit. The rule that
places it in R1 instead, stated explicitly rather than applied ad hoc: *a constructor with no product error surface
takes an error return when it is wiring-time-only **and** its sibling already returns one; otherwise R3.* `msgin.New`
(per-message) and `sqlite.DSN` (no sibling, returns a primitive) both fail that test and stay R3.

**The cost this decision owns:** two of the ten call sites are inline-composed —
`endpoint.WithCircuitBreaker[string](resilience.NewCircuitBreaker())` — and one is a **published godoc Example**
(`endpoint/example_flowcontrol_test.go:24`), which gains error-handling noise. That is the same inline-composability
cost used to reject error returns for R2's five. It is paid here only because R2 is structurally impossible.

**Rejected: fail closed** (`Allow()` returns false forever). It needs no signature change, but a breaker denying every
call is indistinguishable from a real outage — it converts a wiring bug into a silent total denial, worse than the
panic it replaces.

This is the increment's **only** signature change. Nothing is tagged and there are no consumers, so the break is free
— but it **is** a break, and if that ever stops being true it falls under the SemVer gate.

### D-U (NEW) — the R2 loop `continue`s; every non-nil option applies

The R2 loop latches the **first** nil (latching only when unlatched, which preserves first-nil-wins) and **continues**,
so options before *and after* the nil are all applied.

**Why, and why this is not a taste call.** Under `break`, the round-3 audit compile-proved that
`NewPublishSubscribeChannel(nil, WithSingleSubscriber())` left `single == false`, so the reply-channel exclusivity
probe (`endpoint/exchange.go:318-326`) rejected it with **`ErrSharedReplyChannel`** — `errors.Is(err, ErrNilFunc)`
false, the nil option never mentioned. The *same* call with the nil second reported correctly. A guarantee that holds
for one option ordering and not the other is the **third** recurrence of the class rounds 1 and 2 each caught once
(M-5, M-D: a property stated more broadly than it holds), so it is fixed **structurally** rather than documented
around.

**`Chain`'s precedent does not transfer here, and the difference is principled.** `Chain` says *"the chain stops at
it"* because a `Step` is **sequential behavior** — a skipped step changes what runs, so continuing past a nil would
silently execute a different flow. An option is **configuration**: applying the caller's other explicitly stated
intent is strictly better than discarding it, and the fault is still reported in full. D-P's mechanism is unchanged;
only the loop's continuation is.

**Consequence for the spec:** §3.2's applied-prefix property collapses to the simpler *"every non-nil option
applies"*, and non-error methods no longer depend on where the nil sat.

### D-V (NEW) — a latched fault is reported before the method's own argument checks, uniformly

Three of the five R2 products already validate at method entry — `PublishSubscribeChannel.Subscribe` and
`PubSub.Subscribe` check `h == nil → ErrNilHandler`; `Router.Handle` checks `pick == nil`. The latch check goes
**above** all of them: the option fault happened at construction, so it is chronologically earlier, and one rule
across the family beats a per-method accident.

**`routing.Filter` is realigned to match** — its option loop moves above its `pred == nil` check, so `Filter(nil, nil)`
reports the nil option. Without that, `Filter` and `NewRouter` would answer the *same* fault pair the opposite way
**inside the same package** (round-3 audit, M-N). `Filter(nil)` with no options still reports `nil pred`.

**Cost:** a caller who passes both a nil option and a nil handler learns about the option first and must fix it before
seeing the second fault. Accepted — that is true of every ordered validation in the library, and the alternative is
an inconsistency a reader cannot predict.

## Consequences

**Good**

- A documented library-wide invariant replaces 32 undocumented panics; the class becomes stateable and therefore
  gateable (Spec 015 AC-7).
- **Zero call-site churn for R2's five**, against 103 for the all-error-returns alternative.
- The two remaining silent cases are structurally forced and *named* in godoc, not hand-waved.
- One new exported symbol (`error` on `NewCircuitBreaker`), no new sentinel; `apidiff` on the root **package** is
  0/0.
- `errors.Is(err, ErrNilFunc)` becomes the single test for "msgin was handed a nil function" — behavior arguments,
  `Chain` elements and option elements alike.

**Bad / accepted**

- **Three mechanisms.** A reader must know what the constructor returns to predict the behavior. Mitigated by
  stating the invariant over reportability and by a per-constructor godoc sentence.
- **R2 adds a latched-fault field to four types** that had no such concept. `Chain` is the precedent, but `Chain`
  latches into a step it already builds; these latch into a struct.
- **D-S diverges from the stated invariant's third clause.** Deliberate, argued, and confined to this ADR.
- **`nilOptionAt` is duplicated eight times.** Per the Spec 014 §3.3 precedent.
- **Two constructors stay silent.** The real, unmitigable cost of D-P.
- **The delegators check twice.** Judged worth a truthful position.
- The AST class gate (Spec 015 AC-7) carries genuine implementation risk against four proven hazards. Its fallback is
  now a per-package table that calls every constructor with `(nil)` — weaker than the gate but it tests the
  **invariant**, unlike revision 1's withdrawn census fallback, which would have passed green against a tree with
  every guard deleted.

**Neutral**

- Plan 028 was pencilled in for the `gin` adapter; that work moves to a later plan number. There is still no ADR 0024,
  and the `gin` ADR remains a forward reference to an unwritten artifact.
- `memory.WithBuffer(1<<62)` panics inside `make(chan)` — a msgin-provided option that panics on caller input, found
  by the round-1 audit. Same family, deliberately deferred: it needs an increment covering **all** options.
