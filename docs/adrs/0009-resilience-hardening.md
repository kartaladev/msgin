# ADR 0009 — Resilience hardening (clearing the ADR 0008 D10 backlog)

- **Status:** Accepted (2026-07-17)
- **Context source:** [Spec 001 — Messaging core](../specs/001-messaging-core.md) §7.4;
  [Plan 004 — Resilience hardening](../plans/004-resilience-hardening.md); the tracked follow-ups in
  [ADR 0008 D10](0008-resilience-flow-control-api.md) and `docs/HANDOVER.md` §4.
- **Related:** [ADR 0002 — Adapter SPI](0002-adapter-spi.md) (optional-capability interfaces —
  `NativeReliability`, `LiveValueSource` — the pattern D2 reuses), [ADR 0007 — Reliability &
  settlement API](0007-reliability-settlement-api.md) (`safeFire`/`safeHandle` recovery, the
  attempt-tracker whose TTL D3 makes configurable), [ADR 0008 — Resilience & flow-control
  API](0008-resilience-flow-control-api.md) (the layer this ADR hardens; D10 enumerates the findings
  cleared here), [ADR 0001 — Message payload typing](0001-message-payload-typing.md) (the codec
  seam D4 ratifies).

## Context

Plan 003 shipped the flow-control layer and its whole-branch review gate (ADR 0008 D10) deliberately
**triaged** several findings to a backlog rather than rushing fixes into the plan's final task. Before
Plan 005 (Poller + `sql` — the first *wire* adapter) starts, this increment **clears that backlog** so
the next plan builds on a hardened base. The follow-ups, from HANDOVER §4:

1. SPI-panic isolation — `RateLimiter`/`CircuitBreaker` plug-in calls are not `recover`-wrapped.
2. Circuit-breaker half-open probe storm under `WithConcurrency(N>1)`.
3. `Backoff.Max < defaultAttemptTTL` invariant is documented but not enforceable/escapable.
4. `ErrNoPayloadCodec` is an exported-but-never-returned sentinel (open keep/error/drop question).
5. Untrusted wire-decode limits — a payload **byte-size cap** on externally-sourced bytes.
6. Overflow-drop at-least-once redeliver semantics — **inherently an at-least-once-source concern**.

The **adversarial audit corrected the initial scoping of finding 5** (ADR 0009 D5): the decode path
(`consumer.decode → codec.Decode`) **already exists in the runtime**, and per [ADR 0001](0001-message-payload-typing.md)
decoding is **runtime-owned** (the adapter never sees `T`). A raw-byte size cap therefore belongs at the
runtime decode boundary and is testable **now** via a synthetic `StreamingSource` that emits `[]byte`
payloads with a codec — so finding 5's DoS teeth are cleared here (D5), not deferred. Finding 6 remains
genuinely un-clearable (it needs a real at-least-once source to model redeliver-on-drop faithfully) and
stays scoped to Plan 006+ (D6).

Findings 1–5 are the substance of this ADR (D1–D5); D6 records the one deliberate deferral. As with ADR
0007/0008, this is one ADR with per-decision sections; each fleshes out in the same commit that
implements it. **The design below incorporates the fixes from two independent adversarial Opus audits**
(concurrency-correctness and API/policy), whose material findings are folded into each decision and
called out where they changed the design.

## Decision

### D1 — SPI-plug-in panic isolation with a fail-open policy

**Problem.** `safeHandle`/`safeFire` (`consumer.go`) recover a panicking *handler*/*hook* so a fault in
caller code cannot unwind a worker goroutine and crash the process (CLAUDE.md fault-isolation
constraint). The Plan-003 SPI plug-ins — `RateLimiter.Wait` and `CircuitBreaker.Allow`/`Record`/
`HalfOpen` — are equally caller-supplied (the `x/time/rate` / `sony/gobreaker` adapters are the
documented path) but are invoked **directly**, so a panic in a custom implementation crashes the whole
consumer. ADR 0008 D10 left this un-fixed because recovering a *control-flow* call (unlike a
fire-and-forget hook) needs a deliberate **fail-open vs fail-closed** fallback: what does a panicking
`Allow()` resolve to?

**Decision — recover every SPI call; on panic, FAIL OPEN (proceed), log once at WARN.** New consumer
methods wrap each plug-in call, mirroring `safeFire`:

- `safeLimiterWait(ctx) error` → on panic returns **nil** (proceed as if unpaced).
- `safeAllow() bool` (ingress open-check) → on panic returns **true** (treat as not-open).
- `safeAllowProbe() bool` (dispatch gate, D2) → on panic returns **true** (admit).
- `safeRecord(success bool)` → on panic swallow (no fallback value needed).
- `safeHalfOpen() (<-chan struct{}, bool)` → on panic returns `(nil, false)`; `admitBreaker` reads the
  `false` as "breaker unusable" and **returns true (fail-open, does not park)** rather than parking on
  a channel it could not obtain.

**Why fail-open, not fail-closed** (rationale sharpened by the API/policy audit):

1. **The decisive reason — fail-closed under a *deterministic* panic is a zero-progress livelock.** A
   real plug-in bug panics on *every* call. Fail-closed at the dispatch gate (Nack every message) then
   makes **zero** forward progress, and on an immediate-redelivery source (the memory adapter Nacks and
   re-enqueues at once, discarding the delay) that becomes a **hot Nack/redeliver CPU livelock**.
   Fail-open at least degrades to processing. This is the strongest argument and the one the runtime is
   built around.
2. **The panic is the plug-in's fault, not a downstream signal.** A breaker reports *downstream* health;
   a panic in `Allow()` is a bug in the breaker, carrying no evidence the downstream is unhealthy.
   Fail-closed would infer an outage from a code bug — the wrong inference.
3. **Fail-open degrades to the *no-breaker baseline*, which is still bounded.** With the governor
   effectively absent, downstream hits are bounded by **`WithConcurrency`** (default 1) and the
   `RetryPolicy` backoff — exactly the state a caller who never configured a breaker is in. (Note: it is
   `WithConcurrency`, **not** the credit gate, that bounds *concurrent downstream hits*; the credit gate
   bounds claimed-but-unsettled *count*. Fail-open removes the downstream-protection the user opted into,
   degrading to that baseline — not to "no safety at all".) Fail-*closed* at the **ingress** park is
   strictly worse: `Allow()==false` parks on `HalfOpen()`, and if that call *also* panics there is no
   wake channel — the consumer wedges. "A panicking handler must not take down the flow" ⇒ "a panicking
   governor must not wedge the flow."

**Observability — ERROR, deduplicated (not a per-message WARN flood).** A panicking *governor* is
categorically worse than a panicking *hook* (`safeFire`, WARN): a hook is fire-and-forget, but a
governor panic **silently removes a safety layer the caller explicitly configured** — a latent bug. So,
unlike `safeFire`, a governor panic is logged at **ERROR**, and **deduplicated per method** (log the
first occurrence loudly with id + recovered value + method, then suppress repeats via a per-consumer
`sync.Map` gate) so a deterministic panic under fail-open cannot flood the log at one line per message.
Message id only, never the payload. The interface godocs already state a hard no-panic contract (Plan
003 doc-only mitigation); this ADR makes the runtime *enforce* graceful degradation rather than trust it.

**Implementation landmines (audit — sound as specified, must not regress in the rewrite).**

- `safeHalfOpen` returns `(nil, false)` on panic and `admitBreaker` must then **return `true` (proceed
  without parking)** — it must NOT fall through to `select { case <-wake }` on a `nil` channel, which
  blocks forever (a permanent wedge). This is the one place an off-by-one in the rewrite is catastrophic.
- `safeLimiterWait` maps a **panic** to `nil` (proceed) but must **propagate a returned `ctx.Err()`**
  unchanged, so the existing ctx-done Nack path still fires at shutdown. Only panic→nil, never error→nil.
- **Mutex-under-panic is safe for the default breaker** (its methods `defer b.mu.Unlock()`, which runs
  during panic unwinding *before* the panic reaches the consumer's `recover`). A residual, documented,
  not fixed: a *custom* breaker that unlocks manually and panics mid-critical-section leaks its own lock
  and self-deadlocks on its next call (a block, not a panic — `recover` cannot help). D1 keeps the
  process alive; it cannot rescue a plug-in that corrupts its own state. Same for a custom `ProbeGate`
  that panics in `Record` after setting its probe flag: `safeRecord` swallows the panic, the flag stays
  set, and the next `TryProbe` returns a *clean* `false` (not a panic) → no fail-open trigger → that
  breaker wedges its own half-open. The shipped defaults never do either.

### D2 — Single-probe half-open via an OPTIONAL `ProbeGate` capability (non-breaking)

**Problem.** `CircuitBreaker.Allow()` returns true for the *entire* half-open state, so under
`WithConcurrency(N>1)` every worker reaching the dispatch gate while half-open is admitted — a probe
**storm**, where a textbook breaker admits a single canary. (Default `N=1` is already single-probe by
construction — one worker — so this affects only the `N>1` + `WithCircuitBreaker` opt-in combination.)

**Why not just make `Allow()` stateful.** `Allow()` is called at **two** sites: the ingress park
(`admitBreaker`, possibly twice per loop, with **no** paired `Record`) and the dispatch gate
(`process`, always followed by `Record`). A stateful `Allow()` that consumed a probe would leak at the
ingress site (consume, never release) and dead-lock the dispatch site (ingress consumes the only probe,
the worker then sees none and Nacks without ever Recording — the probe flag never clears). Single-probe
therefore needs a **dispatch-only, `Record`-paired** acquire that is *distinct* from the ingress
open-check.

**Decision — add an optional capability interface, not a new required method.** Mirroring the existing
`NativeReliability` / `LiveValueSource` pattern (ADR 0002):

```go
// ProbeGate is an OPTIONAL CircuitBreaker capability. A breaker that implements it
// lets the runtime admit a bounded number of half-open probes at the dispatch gate
// instead of the whole half-open state. TryProbe is a CONSUMING acquire (it may set
// internal probe state), called ONLY at the dispatch gate and ALWAYS paired with a
// following Record; the ingress open-check keeps using the idempotent Allow. A
// breaker that does NOT implement ProbeGate keeps the prior behavior (dispatch
// gates on Allow).
type ProbeGate interface {
    TryProbe() bool
}
```

**`TryProbe`, not `AllowProbe`** (audit rename): unlike the idempotent `Allow`, this is a *consuming*
acquire with a side effect, paired with `Record` — `Try*` conveys that (mirroring the credit gate's
`tryAcquire`), where `AllowProbe` wrongly read as another idempotent check.

- The core `CircuitBreaker` interface is **unchanged** (still `Allow`/`Record`/`HalfOpen`) — a purely
  **additive, non-breaking** SPI change. Existing implementers and the two test doubles
  (`stubCircuitBreaker`, `scriptedBreaker`) keep compiling and their current dispatch behavior.
- `NewConsumer` resolves the capability once (`breaker.(ProbeGate)`), storing a non-nil `probeGate` when
  present. The dispatch gate calls `safeTryProbe()` (→ `probeGate.TryProbe`) when set, else falls back
  to `safeAllow()` (→ `Allow`). The ingress park (`admitBreaker`) **always** uses `Allow`, never
  `TryProbe`.

**Closing the silent gobreaker cliff (audit HIGH).** The documented `sony/gobreaker` wrapper (ADR 0008
D7) mirrors `State()` and will **not** implement `ProbeGate`, so under `WithConcurrency(N>1)` it would
silently fall back to `Allow` and keep the probe storm — for exactly the user who followed the docs,
with nothing warning them. The optional-capability pattern is still right (keeps the core SPI minimal;
a breaker opts into single-probe), but the gap must not be **silent**: `Run` logs a one-time **WARN**
when `workers > 1` **and** a non-nil breaker does **not** implement `ProbeGate` ("breaker does not
implement ProbeGate; half-open admits concurrent probes under WithConcurrency>1"). The plug-in
docs/README gain a note that a gobreaker wrapper should implement `TryProbe` to get single-probe. This
is blackbox-testable (inject a logger; assert the warning fires for `N>1`+non-`ProbeGate`, and does
**not** fire for the default breaker or `N=1`). It also resolves the apparent D2-vs-D4 pre-1.0 tension:
`ProbeGate` stays **optional** (external `CircuitBreaker` implementers exist — gobreaker wrappers — so a
*required* new method would break them), while D4 removes a genuinely *dead* symbol with **zero**
implementers; the two are not in tension once the cliff is made loud.

**Default breaker implements `ProbeGate`.** `TryProbe()`: closed → true (unlimited, like `Allow`); open
→ false; half-open → true for exactly **one** caller (sets `probeInFlight`), false for the rest until a
`Record` settles it. Because the dispatch gate calls `TryProbe` then **always** `Record`s, the
acquire/release pairing is exact; a returned-`false` `TryProbe` consumes nothing (the message Nacks
without a `Record`). At `N=1` `TryProbe` ≡ `Allow` (one worker), so no existing default-breaker test
changes.

**The wedge landmine (audit CRITICAL-if-misimplemented).** The whole design rests on the invariant
`probeInFlight == true ⟹ state == halfOpen`. It holds only if **`toHalfOpen` UNCONDITIONALLY resets
`probeInFlight = false`** on the genuine open→half-open transition (after its `state != open` guard) —
not merely "the half-open Record branches clear it". The killer path: under `N>1`, worker P acquires the
probe (flag=true) in half-open; a *closed-era straggler* then `Record`s a failure, reopening the breaker
via `openLocked`; P later `Record`s while `state == open`, and a "clear only in the half-open Record
branches" shortcut leaves the flag **stuck true**. The next cooldown then lands in half-open with
`probeInFlight` permanently set → `TryProbe` returns false for every worker forever → **permanent
half-open wedge + Nack churn livelock**. Clearing the flag in `toHalfOpen` closes it. **Task 4 MUST
include the explicit N>1 straggler-Records-from-open test** that re-enters half-open and asserts a fresh
probe is admitted — the happy-path open→half-open→probe→close test does not exercise this.

**Residuals (documented, accepted).**

- **Single-probe is best-effort under `N>1` stragglers.** A closed-era straggler's late `Record(true)`
  during half-open can close the breaker (and clear `probeInFlight`) before the real probe's result is
  known, momentarily admitting more than one probe. Bounded, no wedge/leak; inherent to the shared-breaker
  "Record from any dispatch" model ADR 0008 already accepts.
- **Half-open churn is a hot spin on immediate-redelivery sources (audit correction — the earlier
  "waits durably in the source" wording was WRONG for memory).** In half-open under `N>1`, ingress
  (`Allow`=true) keeps admitting; every non-probe worker gets `TryProbe`=false and Nacks with `delay=0`.
  **The memory adapter discards the Nack delay** and re-enqueues immediately, so those messages hot-loop
  through Stream→ingest→worker→reject→Nack at ~100% CPU across `min(workers, maxInFlight)−1` goroutines
  for the *entire duration of the single probe's handler* — and the worst case (a slow/timing-out probe
  against a still-recovering downstream) is the breaker's *normal* operating condition. It is bounded
  (terminates when the probe `Record`s) and credit-safe (release-first, no leak), but it is a genuine
  spin, not durable waiting. Delay-honoring / native-redelivery wire sources (Plan 005+) do not exhibit
  it; the memory adapter is a reference/test adapter. Documented on `TryProbe` and covered by a
  regression test asserting termination + credit-safety (all messages eventually processed, no leak) —
  **not** paced with a consumer-clock timer, which would perturb the ADR 0008 D8 C1 fake-clock
  waiter-count invariant. A structural fix (throttling ingress in half-open, which needs a
  half-open→closed wakeup signal the breaker does not currently emit) is noted as a future option, not
  taken here.

### D3 — `WithAttemptTTL` escape hatch + invariant documentation (reverses ADR 0008 D8's YAGNI)

**Problem.** The attempt-tracker TTL sweep (ADR 0008 D8) is safe *because* an actively-redelivering id
is re-observed within `≤ Backoff.Max ≪ defaultAttemptTTL (5m)`. Nothing lets a caller who deliberately
pairs a large/uncapped `ExponentialBackoff` with a future delay-honoring source **raise** the TTL, and
D8 hard-coded it with an explicit "no opt-out knob (YAGNI)".

**Decision — add `WithAttemptTTL(d)`.** A `ConsumerOption` that overrides `defaultAttemptTTL`. `d <= 0`
is a caller error → **`ErrInvalidAttemptTTL`** (construction-time typed error, matching
`ErrInvalidMaxInFlight`). Now that a caller *can* pass a non-positive TTL, the guard D8 omitted as
"uncoverable dead code" becomes reachable and is added with a covering test. The tracker already takes
its `ttl` by injection (`newAttemptTracker(clock, ttl)`), so this only threads the config through
`consumerConfig`/`NewConsumer`.

**Invariant enforcement — documentation, not a construction-time hard error.** A fully general
`Backoff`-vs-TTL check is not tractable (an arbitrary `BackoffStrategy`'s growth curve is unbounded), and
a concrete-`ExponentialBackoff`-only error would leave custom strategies unchecked (an asymmetric
surface) and false-positive on the default-TTL memory path. The escape hatch is the actionable fix (the
follow-up's own "*or* add a `WithAttemptTTL` escape hatch"); the invariant is **documented prominently**
on `WithAttemptTTL` and the tracker. When the first delay-honoring source lands (Plan 006+), revisit
whether a best-effort concrete-type guard is then warranted (tracked, D6).

**Invariant reframed (concurrency audit — the `d > 0` guard is NOT a safety guarantee, and NF-2 is
violable on memory *today*).** The sweep evicts when `now.Sub(lastSeen) >= ttl`, and `lastSeen` is
refreshed only at `observe`, called on the transient path **after `safeHandle` returns**. So the gap
between consecutive observes of a redelivering id spans the whole next-attempt round-trip — re-inject +
decode + **handler execution** + settle — **not just `Backoff.Max`**. The correct invariant is therefore
"**TTL must comfortably exceed the worst-case redelivery round-trip, *including handler execution
time***", which the `WithAttemptTTL` godoc states explicitly. The earlier "`Backoff.Max ≪ TTL`" framing
is a footgun: the memory adapter **ignores the Nack delay entirely**, so a caller with `Backoff = nil`
reads "0 ≪ any positive TTL" and sets, say, `WithAttemptTTL(1ms)` — then a handler that runs longer than
1ms opens an observe-to-observe gap the sweep lands in, evicting the in-flight entry and restarting it at
attempt 1, **silently defeating `MaxAttempts` on the shipped adapter, now**. `d > 0` only rejects the
degenerate zero; it cannot make an arbitrary small positive TTL safe. The mitigation is the explicit
godoc warning plus a Task-1 NF-2 test whose handler execution straddles a tiny TTL under controlled
observe/sweep ordering (a fake-clock test with the gap modeled explicitly), proving both that a
correctly-sized TTL preserves the in-flight count and that the sweep only reclaims genuinely-idle ids.

### D4 — Ratify JSON-default for wire+no-codec; drop the unused `ErrNoPayloadCodec`

**Problem.** `resolveCodec` (`producer.go`) defaults a wire adapter with no explicit codec to
`JSONPayloadCodec[T]`. `ErrNoPayloadCodec` is **exported but never returned** by any path — a
misleading part of the public error contract (a caller `errors.Is`-ing against it can never match).

**Decision — ratify the JSON default; drop the sentinel.** The convenience default (wire source + no
codec → JSON) is the deliberate, documented behavior: it matches `WithConsumerCodec`'s stated default
("default JSON") and is the ergonomic norm for the first wire adapter (`sql`, which stores JSON-friendly
bytes). Because that policy makes `ErrNoPayloadCodec` permanently dead, it is **removed** — a small,
honest public surface (library quality gate: "prefer a small, stable surface") beats an exported error
nothing returns. This is a pre-1.0, no-release-tag change; if a future adapter (e.g. a binary-only
protocol) wants **require-codec** semantics, it reintroduces the sentinel *with* a returning path at that
point. Note (audit): a returning path also needs a **discriminator** — `resolveCodec` unconditionally
supplies JSON when `codec == nil`, so the future adapter must signal require-codec via a capability
(e.g. `RequiresCodec()`, mirroring `LiveValueSource`), not merely re-add the error. The decision to add
either belongs with the adapter that needs it, not a speculative reservation.

### D5 — Untrusted wire-decode byte cap: `WithMaxPayloadBytes` (finding 5, implemented — audit re-scope)

**Audit correction.** The initial plan deferred finding 5 as "no decode path exists". That was wrong:
`consumer.decode` already calls `c.codec.Decode(b)` on `[]byte` payloads, and per [ADR 0001](0001-message-payload-typing.md)
decoding is **runtime-owned** — so a byte-size cap belongs at that runtime boundary and is testable now
via a synthetic `StreamingSource` emitting `[]byte`. Finding 5's primary DoS lever (an attacker sends a
multi-gigabyte payload → OOM on decode) is closed here, not deferred.

**Decision — `WithMaxPayloadBytes(n)`.** A `ConsumerOption` capping the size of an externally-sourced
`[]byte` payload **before** it is handed to `codec.Decode`. In `consumer.decode`, on the wire path only
(`!c.liveValue`), if `n > 0 && len(b) > n` the message is settled as a **permanent invalid message**
(new sentinel **`ErrPayloadTooLarge`**, added to `isPermanent`), diverted to the invalid sink like a
decode failure — an over-size payload is poison (it will not shrink on redelivery), never a transient
retry. The live-value (memory) path never carries `[]byte` and is unaffected.

- **Default = unlimited (`n <= 0` disables the cap).** A library cannot guess a caller's legitimate max;
  a forced default would break large-payload users. Wire adapters for untrusted sources **should** set
  it — the `WithMaxPayloadBytes` godoc says so, and Plan 005's `sql` adapter can pick a sensible default
  when it wires the source. The option is the *mechanism*; opting in is the caller's/adapter's call.
- **Complexity/nesting** (the other half of finding 5) is bounded by the codec, not msgin:
  `encoding/json` has a hard recursion limit and returns an error on pathologically nested input rather
  than overflowing the stack, so `JSONPayloadCodec` is already safe; a custom codec owns its own
  complexity limits. Documented; no separate knob.
- **Hot-path branches:** `n <= 0` (disabled → no check); `n > 0 && len(b) <= n` (pass);
  `n > 0 && len(b) > n` (→ `ErrPayloadTooLarge` → invalid sink). All coverable via a `[]byte`
  `StreamingSource` + `WithMaxPayloadBytes`.

### D6 — The one genuine deferral: finding 6 (at-least-once overflow)

Finding 6 **cannot** be cleared here: faithfully modeling overflow-drop redeliver-on-drop for an
at-least-once source needs the native-redelivery machinery (Redis PEL / JetStream `Nak`/`Term`), and the
open *design* question (does overflow-drop want a native-redeliver flag distinct from `requeue`?)
genuinely needs the real adapter to inform it. A test stub would contradict the very `requeue=false`
semantics under test. Re-scoped to **Plan 006+** (`redis`/`nats`); ADR 0008 D9 carries the detailed note.
Likewise the best-effort `ExponentialBackoff`-vs-TTL *enforcement* (beyond D3's escape hatch + docs) is
revisited when the first delay-honoring source lands. This is recorded so both deferrals are deliberate,
not forgotten; all six HANDOVER §4 follow-ups are thereby accounted for (1→D1, 2→D2, 3→D3, 4→D4, 5→D5,
6→D6).

## Consequences

**Positive**
- Five of the six D10 follow-ups are cleared before the first wire adapter builds on this layer; the
  sixth (D6) is deliberately, traceably deferred to where it becomes testable.
- D2's optional-capability approach fixes the probe storm with **zero** breaking SPI change and **zero**
  churn to existing breaker tests/doubles; the one-time `Run` WARN closes the silent-gobreaker-cliff so
  the `N>1`+non-`ProbeGate` gap is loud, not hidden.
- D1 turns the interface godoc's "MUST NOT panic" from a trusted contract into an *enforced* graceful
  degradation logged at ERROR (deduplicated), so a misbehaving plug-in degrades to the
  `WithConcurrency`-bounded no-breaker baseline — never a process crash, a wedge, or a silent
  safety-removal.
- D5 closes finding 5's DoS lever (over-size untrusted payload → OOM) at the runtime decode boundary,
  where ADR 0001 says decoding belongs.
- D4 removes a misleading exported symbol, shrinking the public error contract to what the code returns.

**Negative / costs**
- D1's fail-open is a deliberate policy a downstream-protection purist could contest; the sharpened
  rationale (deterministic-panic fail-closed = zero-progress livelock; degrades to the no-breaker
  baseline; fail-closed-at-ingress wedges) is documented, and a caller wanting fail-closed must supply a
  non-panicking breaker (the contract). `safeHalfOpen` fail-open on a truly-open breaker with a panicking
  `HalfOpen` degrades to a *hot* admit→Nack spin, not smooth "governor absent" — bounded and credit-safe,
  but noted.
- D2 adds a second (optional, one-method) breaker interface; single-probe is best-effort under `N>1`
  stragglers, and the half-open reject-churn is a hot spin on immediate-redelivery sources (memory) —
  both documented, the latter regression-tested for termination/credit-safety rather than paced (a pace
  timer would perturb the C1 waiter-count).
- D3's invariant stays *documented*, not *enforced*; a too-small positive `WithAttemptTTL` can defeat
  `MaxAttempts` on memory today, mitigated by a prominent godoc warning. Best-effort enforcement tracked
  for Plan 006+.
- D4 removes an exported symbol (acceptable pre-1.0, no release tag; recorded per the API-compatibility
  gate); re-adding require-codec later needs a discriminator capability, not just the sentinel.
- D5 defaults to unlimited (a library cannot guess a safe cap), so the DoS defense is opt-in; Plan 005's
  `sql` adapter is expected to set a sensible default.

**Rejected alternatives**
- **Fail-closed on a panicking SPI call (D1)** — rejected: a deterministic panic makes fail-closed a
  zero-progress livelock (hot Nack/redeliver on memory), infers a downstream outage from a plug-in bug,
  and wedges the consumer if `HalfOpen` also panics. Fail-open degrades to the `WithConcurrency`-bounded
  no-breaker baseline.
- **`TryProbe` as a REQUIRED `CircuitBreaker` method / a stateful `Allow` (D2)** — rejected: a stateful
  `Allow` leaks at the unpaired ingress site and deadlocks the paired dispatch site; a required new
  method breaks external implementers (gobreaker wrappers) and both test doubles. The optional
  `ProbeGate` + the one-time cliff WARN achieves single-probe with neither cost.
- **Naive per-message WARN for a panicking governor (D1)** — rejected: a deterministic panic under
  fail-open would flood one WARN per message at a filtered level while silently disabling a configured
  safety. Logged at ERROR, deduplicated per method.
- **Construction-time hard error for `Backoff.Max ≥ TTL` (D3)** — rejected now: cannot cover custom
  `BackoffStrategy` implementations and the real gap includes handler time (not just `Backoff.Max`);
  deferred to when a delay-honoring source makes a check both reachable and meaningful.
- **Keep `ErrNoPayloadCodec` as a "reserved" sentinel (D4)** — rejected: an exported error no code
  returns misrepresents the contract; reintroduce it *with* its path + a discriminator when an adapter
  needs it.
- **Defer finding 5 to the sql adapter (D5)** — rejected after audit: the decode path is runtime-owned
  (ADR 0001) and exists today; the byte cap is testable now and belongs at the runtime boundary, not the
  adapter.
