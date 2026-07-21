# Spec 013 — Producer-side outbound retry (`WithProducerRetry`, `RetryAfter`)

- **Status:** **Implemented (2026-07-22)** — the core retry is delivered by [Plan 023](../plans/023-producer-outbound-retry.md).
  The mandatory **two-round adversarial Opus audit** of the complete bundle (this spec + the
  [Spec 011 §3.4 delta](011-http-adapter.md) + [ADR 0025](../adrs/0025-producer-outbound-retry.md) + Plan 023) ran
  before any code and changed the design materially — §§3–5 and 8 below have been **amended to describe what shipped**,
  not what was first drafted. Spec 011 Phase 2 (§3.3) remains outstanding, in [Plan 024](../plans/024-http-outbound.md).
- **Realized by:** [Plan 023](../plans/023-producer-outbound-retry.md) — core producer retry — and
  [Plan 024](../plans/024-http-outbound.md) for the Spec 011 Phase 2 delta. §7 records why the originally chosen single
  increment was **split** after the round-1 audit.
- **Decided by:** [ADR 0025](../adrs/0025-producer-outbound-retry.md), which **supersedes the outbound-HTTP clause of**
  [ADR 0005](../adrs/0005-cenkalti-backoff-dependency.md).
- **Builds on:** [ADR 0002 — Adapter SPI](../adrs/0002-adapter-spi.md) (runtime-owned reliability: adapters classify,
  the runtime decides), [ADR 0004 — clockwork](../adrs/0004-clockwork-dependency.md) (injectable time).

## 1. The gap

**There is no retry on the outbound path.** `Producer.Send` (`producer.go`) is a straight passthrough:

```go
func (p *producer[T]) Send(ctx context.Context, msg Message[T]) error {
    boxed, err := p.box(msg)
    if err != nil { return err }
    return p.out.Send(ctx, boxed)   // ← no retry, no backoff, no dead-letter
}
```

`RetryPolicy` appears **only** in `consumer.go`, where it governs settlement of an *inbound* delivery (Nack →
redelivery → dead-letter). Nothing applies it to an `OutboundAdapter`.

**This makes a claim in [Spec 011 §3.4](011-http-adapter.md) false as written.** That section specifies the O1 webhook's
response→error classification as "`5xx`, `408`, `429`, network error, timeout → plain (transient) error — the runtime
**retries** per `RetryPolicy`", and explicitly deferred confirmation to this increment ("Open point (resolve in
Plan 023)"). The open point is hereby **resolved: it does not.** Spec 011 §3.1 likewise lists O1's reused core seam as
"`Producer` / `RetryPolicy`" — aspirational, not actual. This increment makes it true.

### 1.1 What *does* retry today, and why that is not enough

An outbound send reached through a **flow step** — `Chain(…, To(sink))` (`handler.go`) — returns its error to the
enclosing `MessageHandler`. Inside a **Consumer-driven** flow that error is settled by the Consumer's `RetryPolicy`:
the message is Nacked and **redelivered**, replaying the whole flow. So `Permanent` vs transient *is* honored there —
but the retry is a full message redelivery from the inbound source, not an outbound-local retry, and it re-runs every
prior step's side effects.

A **`Producer.Send`** call has no such enclosure. There is no inbound delivery to Nack and no runtime to settle it, so a
transient failure is simply returned to the caller. That is the gap this spec closes.

## 2. Decisions settled with the user (2026-07-21)

1. **Fix it at the producer, in core — not in the adapter.** Every outbound adapter (SQL, Redis, NATS, HTTP) benefits
   from one change at the right layer, per ADR 0002's runtime-owned-reliability boundary. Rejected: an adapter-side
   backoff loop inside `NewOutbound` (see §6.1).
2. **Expose it as `WithProducerRetry(RetryPolicy)` on `NewProducer`** — *not* as a general `RetryingOutbound(out, policy)`
   decorator. The alternative was surfaced with its trade-offs and deliberately declined (§6.2).
3. **Honor `Retry-After` via a `RetryAfter(err, d)` marker** mirroring the existing `Permanent(err)`, rather than
   ignoring it or importing a library for it (§3.2).
4. **One increment, core + HTTP outbound together** (§7).

### 2.1 The rationale for the narrow scope — recorded, not merely the choice

Attaching retry to `Producer` only, and not to `To(sink)`, is coherent rather than a compromise: **inside a flow the
runtime already owns retry via redelivery**, so an inner loop there would *multiply* attempts (inner × outer) and
re-run prior steps on each outer retry. Filling only the `Producer` gap keeps **exactly one retry authority per path**.

**The consequence, which must be documented on the option and in the HTTP adapter's godoc:** an O1 webhook driven as
`To(outbound)` inside a flow relies on Consumer redelivery, not producer retry — and a flow with **no inbound source**
(e.g. a hand-driven `Chain`) gets **no retry at all**. A caller who wants outbound retry there uses a `Producer`.

## 3. Design

### 3.1 `WithProducerRetry(p RetryPolicy) ProducerOption[T]`

Reuses the **existing, tested** `RetryPolicy` / `BackoffStrategy` / `ExponentialBackoff` machinery verbatim
(`retry.go`, `backoff.go`) — no new retry concepts, **no new dependency**.

> **Amended 2026-07-22 to match what shipped.** The loop below is the **implemented** one
> (`producer.go:424 sendRetrying`), not the design this spec was drafted with. Two adversarial audit rounds over the
> Plan 023 bundle changed it materially — see [Plan 023](../plans/023-producer-outbound-retry.md)'s round-1/round-2
> audit sections for the findings, and `docs/adrs/0025` §1 for the ratified bounds.

- **Validation at construction.** `NewProducer` calls the existing `p.Validate()`, so `MaxAttempts > 0` without a
  `DeadLetter` returns `ErrNoDeadLetter`, and a negative `MaxAttempts` returns `ErrInvalidMaxAttempts` — at build time,
  not at send time. **Additionally** (audit-forced), a policy unbounded in *both* dimensions — `MaxAttempts == 0` with a
  nil `Backoff`, i.e. the `RetryPolicy` **zero value** — is rejected with the new `ErrUnboundedRetry`. The check is
  deliberately **structural**, never a probe of `delayFor`: `BackoffStrategy` is an exported public interface, so
  evaluating it inside a constructor would invoke caller code (which may panic or block) and is non-deterministic under
  jitter. The residual hazard it does *not* catch — `ExponentialBackoff{}`, a non-nil strategy whose `Delay` is always
  `0` — is handled at runtime by the floor and the budget below.
- **Default: unchanged behaviour.** Without the option, `Send` remains a single passthrough attempt. This is additive;
  no existing caller changes semantics.
- **The loop** (`producer.sendRetrying`, entered from `Send` after `box` only when the option is set):
  1. Compute the budget deadline once: `clock.Now() + retryBudget`. The budget **always** applies (§3.2) — there is no
     unbudgeted mode and no flag.
  2. Attempt `p.out.Send(ctx, boxed)`. `nil` → return `nil`.
  3. **Permanent short-circuits.** If the existing `isPermanent(err)` reports true — an explicit `Permanent` marker,
     `ErrPayloadType`, `ErrPayloadDecode`, `ErrPayloadTooLarge` — return it immediately, consuming no attempt budget
     and **never** dead-lettering on the retry path. A permanent outbound failure is the caller's to see. `Permanent`
     **wins over** a `RetryAfter` marker: a delay is meaningless when the answer is "do not retry".
  4. **Attempt exhaustion.** With `MaxAttempts > 0`, once `attempt >= MaxAttempts`: divert (step 8) and return.
  5. **Compute the wait** — `nextDelay(attempt, err)`: `policy.delayFor(attempt)`, widened by any `RetryAfter` minimum
     (§3.2), then **floored to `minRetryDelay` = 100 ms**. The floor is what stops a degenerate strategy
     (`ExponentialBackoff{}`, a non-positive `Initial`) re-attempting with no delay at all and hammering the target
     from the caller's goroutine. It is deliberately **not configurable**: a caller wanting a *longer* wait sets
     `Backoff`, and there is no legitimate reason to want a shorter one on a loop holding the caller's own goroutine.
  6. **Budget check.** If `now + wait` would pass the deadline, stop *before* parking rather than truncating the wait —
     the producer never makes an attempt the budget cannot back. Divert (step 8), with the cause wrapped as
     `fmt.Errorf("%w: %w", ErrRetryBudgetExhausted, err)` so a budget stop is distinguishable from genuine attempt
     exhaustion.
  7. **Wait**, on the producer's **injected `clockwork.Clock`**, after firing `Hooks.OnRetry` (§3.2). **`ctx`
     cancellation aborts the wait** — everything is cancellable (CLAUDE.md robustness gate) — but it is **terminal for
     the message**, not a bare return: the loop **diverts on cancel** and returns
     `errors.Join(deadLetter(...), ctx.Err())`. The audit's finding was that returning without diverting loses the
     message on the *common* cancellation path, which is exactly the loss this design exists to prevent.
  8. **The divert** (`producer.deadLetter`). With no `DeadLetter` configured the cause is returned unchanged and
     **without** `ErrDeadLettered` — nothing was dead-lettered. Otherwise the sink is called on a ctx that is:
     - **detached** from the caller's (`context.WithoutCancel`, the `exchange.go:347` precedent) — the usual reason
       the loop is ending is that `ctx` died, and diverting on that same ctx would fail too, leaving the message in
       neither the target nor the DLQ; and
     - **timed** (`WithProducerDeadLetterTimeout`, 30 s default) — detaching *without* bounding would let a hung sink
       block the caller's goroutine forever, immune to their own cancel, which is strictly **worse** than the
       un-retried passthrough.

     The sink call is panic-recovered (`safeDeadLetter`) so a faulty DLQ cannot crash the caller's goroutine; the
     producer's own `out.Send` is deliberately *not* wrapped, because its panic belongs to the caller. A successful
     divert returns `fmt.Errorf("%w: %w", ErrDeadLettered, cause)` — **never `nil`**, since the caller must learn the
     send did not reach its target — and a failed one returns `errors.Join(cause, dlErr)`. `Hooks.OnDeadLetter` fires
     on **both** arms with exactly the error `Send` is about to return, so a hook can tell "safely in the DLQ" from
     "lost" the same way the caller can.
- **Hooks.** `WithProducerHooks` installs the nil-safe, panic-recovered `OnRetry` (before each wait, with the causing
  error) and `OnDeadLetter` callbacks. They run synchronously on the caller's goroutine; a panicking hook is recovered
  and logged via `WithProducerLogger` (default: a discard logger), mirroring `consumer.safeFire`. Telemetry must never
  break the send, so hooks are not usable for control flow.
- **`SendAfter`/`SendAt` are out of scope** for retry in v1: they delegate to a `ScheduledSender`, whose delivery is
  the adapter's store, not a live network call. Stated explicitly so the omission is a decision, not an oversight.

### 3.2 `RetryAfter(err error, d time.Duration) error`

A server that answers `429` or `503` with a `Retry-After` header is *instructing* the client. `BackoffStrategy.Delay`
is **stateless and closed-form by design** (`backoff.go`: "pure arithmetic, safe to share across goroutines") and
cannot express "wait the 30s the server asked for". A marker carries it out-of-band instead:

```go
// RetryAfter marks err as transient with a server-provided delay: the producer's
// retry loop waits at least d before the next attempt. Transparent to
// errors.Is/As via Unwrap. RetryAfter(nil, d) returns nil.
func RetryAfter(err error, d time.Duration) error
```

This deliberately mirrors `Permanent(err)` (`reliability.go`) — same wrapper shape, same `Unwrap` transparency, same
nil handling — so the codebase gains no new idiom. An unexported `retryAfterOf(err) (time.Duration, bool)` reads it via
`errors.As`, matching `isPermanent`'s structure. **A `Permanent` marker wins over a `RetryAfter` marker** if both are
present (permanent means *do not retry*, so a delay is meaningless).

**`Retry-After` is a MINIMUM, never a replacement** (amended — the original text said "overriding the computed
backoff", which is not what shipped). RFC 9110 §10.2.3 makes `Retry-After` a lower bound on when the client may retry,
not an instruction to retry *sooner*. So the effective wait is

```
max( computedBackoff , min(serverDelay, retryAfterCap) )      then floored to minRetryDelay (100ms)
```

A server may therefore only **lengthen** the client's own backoff. Treating it as an override would hand a remote
endpoint a lever to shorten — ultimately to zero — the client's backoff: a hot-spin trigger controlled by the party the
client is already failing against.

**`WithProducerRetryAfterCap(d time.Duration) ProducerOption[T]`** clamps the honored delay. An unbounded server-supplied
delay lets a hostile or misconfigured endpoint park the caller's goroutine indefinitely; the caller's `ctx` bounds it
only if they passed a deadline, and `context.Background()` is common. **Default: 60 s** — at the top of the plausible
legitimate range (HTTP rate-limit windows are typically ≤ 60 s), so a hostile `Retry-After: 86400` is clamped. The
default deliberately does *not* cover the longest legitimate case (a maintenance `503` asking for 120 s): defaulting
*above* the worst legitimate value would optimise for obeying a remote instruction over bounding the caller, which is
the wrong side of CLAUDE.md's safe-defaults gate. An explicit `d <= 0` is `ErrInvalidRetryAfterCap`.

### 3.2.1 The other options the audit added

**`WithProducerRetryBudget(d time.Duration)`** bounds the **cumulative wall-clock**, on the injected clock, that one
`Send` may spend retrying. **Default: 2 minutes**, and it **always** applies. It exists because `MaxAttempts == 0`
means "retry forever", bounded otherwise only by `ctx` — and `Producer.Send` is routinely called with
`context.Background()`. The two inequalities that constrain the number are the load-bearing part, not the number
itself:

- `cap (60 s) < budget (2 m)` — a budget below the cap would silently defeat the `Retry-After` compliance the cap
  exists to permit.
- `budget (2 m) < the shortest plausible upstream lease` (`adapter/database/sql` defaults `leaseTTL` to 5 minutes) — a
  `Send` outliving the lease covering the message being handled lets the source reclaim and redeliver it mid-send,
  turning one logical message into duplicate outbound calls fanned out across instances. **This relation holds for the
  defaults as shipped; it is not a guarantee** — the budget bounds retry waits only.
- **What it does not bound:** the duration of any single `OutboundAdapter.Send`. The loop checks the budget before
  parking, never during an adapter call, so worst-case `Send` wall clock is
  `retryBudget + one unbounded adapter call + deadLetterTimeout`. Bound the adapter's own I/O yourself.

  An explicit `d <= 0` is `ErrInvalidRetryBudget`. There is deliberately **no "unlimited" value**.

**`WithProducerDeadLetterTimeout(d time.Duration)`** bounds the detached divert (§3.1 step 8). **Default: 30 s**;
explicit `d <= 0` is `ErrInvalidDeadLetterTimeout`; no "unlimited" value. Deliberate deviation, recorded here: this is
a **`context` deadline, not a clockwork timer**, so a fake clock cannot drive its expiry — `context.WithTimeout` has
no injectable-clock form and reimplementing one to keep the rule would be worse than documenting the exception.

**`WithProducerHooks(Hooks)`** and **`WithProducerLogger(*slog.Logger)`** — §3.1. `WithProducerLogger` is named
distinctly from the Consumer's `WithLogger` to avoid a collision on the shared option vocabulary (ADR 0007 D10), as
`WithProducerClock` already is.

### 3.3 Spec 011 §3.4 delta (O1/O2) — what changes in the HTTP design

The HTTP outbound design is otherwise as [Spec 011 §3.4](011-http-adapter.md) already specifies. This increment:

- **Resolves the open point** recorded there (§1 above) and rewrites the O1 classification text so it no longer claims
  a runtime retry that does not exist on the `Producer`-less path.
- **Adds `Retry-After` parsing** to O1's classification: a `429` or `503` carrying `Retry-After` (both the
  delay-seconds and the HTTP-date forms) is returned as `RetryAfter(err, d)`; without the header it is a plain
  transient error.
- **Binds O2 to the SPI contract written by [Spec 012](012-exchange-panic-safe-cleanup.md) §7.** `NewExchange` is the
  **first external implementation** of `RequestReplyExchange` and holds its own request-scoped state — an in-flight
  `*http.Request`, a response body that must be closed. That is the same class of state whose leak Spec 012 fixed in
  `ChannelExchange`, so O2 must release it on **every** exit path including a panic unwind (deferred cleanup), and must
  **not** recover a caller panic into an error return.

## 4. Robustness & security constraints

- **SSRF invariant (inherited, Phase 1 §4):** the target URL is caller-configured and **never** derived from message
  payload or headers.
- **Response bodies are closed on every path**, including error and panic returns; the reply read is **bounded** so a
  hostile server cannot exhaust memory.
- **No goroutine leaks** (`goleak`), everything `ctx`-cancellable, graceful shutdown honored.
- **The retry loop holds the caller's goroutine.** That is inherent to a synchronous `Send`, and is documented on the
  option: a caller wanting fire-and-forget composes retry with their own concurrency.
- **Nested-retry multiplication** (§2.1) is documented on both `WithProducerRetry` and the HTTP adapter godoc.

### 4.1 Delivery guarantee — retrying CHANGES it (mandatory statement)

CLAUDE.md forbids leaving a delivery guarantee implied, and this option **changes one**. A retry after a timeout or a
connection reset re-sends a request the peer **may already have received and committed**; the producer cannot
distinguish "never arrived" from "arrived, reply lost". Therefore:

> **`WithProducerRetry` converts `Producer.Send` from the underlying adapter's guarantee to AT-LEAST-ONCE, with
> duplicates visible to the receiver.**

The **receiver must be idempotent**. Either carry a stable **idempotency key** in a header — the message ID is one —
and deduplicate on it (EIP *Idempotent Receiver*), or make the operation naturally idempotent. Without
`WithProducerRetry`, `Send` makes exactly one attempt and the adapter's own guarantee is unchanged. This is stated on
`WithProducerRetry`'s godoc as well, because the option's own doc is where a caller meets the trade.

### 4.2 Multi-instance topology (CLAUDE.md mandatory rule)

**This retry is PER-PROCESS by construction.** All of its state — the attempt counter, the budget deadline, the
`Retry-After` a server supplied — lives in one goroutine's stack frame in one process. Across **N horizontally scaled
instances behind a load balancer**, the ordinary deployment this library targets:

- each instance retries **independently**, so an endpoint that answered `429` to shed load receives **N × the retry
  load** it asked to shed;
- `Retry-After` compliance is **per-instance, not fleet-wide** — every instance honours the delay, and all N then
  retry together, which is also a synchronised thundering herd;
- the budget bounds one `Send` on one instance; it says nothing about aggregate pressure on the target.

The core **cannot** solve this: a fleet-wide budget or token bucket needs shared state (Redis, a DB) that a
dependency-minimal core must not assume. **The seam is already shaped for it** —
[ADR 0006](../adrs/0006-resilience-flow-control.md)'s **rate-limit and circuit-breaker interfaces** are clockwork-driven
interfaces with dependency-free defaults, so a distributed (Redis- or DB-backed) limiter plugs in **without a core
change**. That is the documented path for a caller who needs fleet-wide throttling; it is stated on the option's godoc
so an in-process construct never silently leaks into a contract a distributed deployment would break.

### 4.3 Why `ErrUnboundedRetry` exists (and why the asymmetry is deliberate)

`RetryPolicy{}` — the **zero value** — is **valid for a Consumer** and **invalid for a Producer**. That is a genuine
wart (one struct meaning different things at two call sites) and it is deliberate:

- On the **Consumer**, `MaxAttempts == 0` with a nil `Backoff` means "retry forever, immediately" via **broker
  redelivery** — the message goes back to the source, the broker paces it, and nothing spins.
- On the **Producer**, the identical policy is a **zero-delay infinite loop on the caller's own goroutine**. There is
  no broker in the path to pace it.

The rejected alternative — relying on the 100 ms floor plus the budget alone — turns an obvious caller mistake into a
silent stall (~1200 pointless attempts over the 2-minute default budget, then a dead-letter) instead of a construction
error naming the problem. Failing loudly at `NewProducer` is the debuggability-first choice (CLAUDE.md's core quality
criterion).

The **structural** predicate (`MaxAttempts == 0 && Backoff == nil`) is what ships; §3.1 records why probing
`delayFor` was tried and reverted, and where the residual `ExponentialBackoff{}` hazard is handled instead.

## 5. Test obligations

> **Amended 2026-07-22:** the obligation list this section originally carried (nine bullet cases) has been replaced by
> the coverage **actually achieved**. The delivered table is materially larger, because the audit-forced bounds each
> brought their own branches.

**Standing rules, unchanged:** blackbox only (`package msgin_test`), exported API only, **assert-closure** tables
(never `want`/`wantErr` fields), `t.Context()`, `goleak` guards, and **retry timing driven by
`clockwork.NewFakeClock()`** — the only real sleep in the increment is `ExampleWithProducerRetry`'s 1 ms `Initial`,
which an `Example` cannot avoid and which asserts nothing about elapsed time.

**Achieved coverage** (`GOTOOLCHAIN=go1.25.12 go test -coverprofile=… .`): **99.2 % package total**, with **every
function this increment adds or touches at 100 %** — all 25 in `producer.go`, both `BytesPayloadCodec` methods in
`codec.go`, `Delay` and `jitter` in `backoff.go`, and `RetryAfter`/`retryAfterOf` in `reliability.go`. The two
sub-100 % functions in `reliability.go` (`isPermanent` 83.3 %, `NativeRedelivery` 0.0 %) are **pre-existing,
blackbox-unreachable arms**, verified as such before the increment and untouched by it.

**37 branch cases across 11 test functions:**

| Test | Cases | Branches pinned |
|---|---|---|
| `TestProducerRetry` | 7 | unset policy sends exactly once; first-attempt success; transient→retry→success; attempt exhaustion diverts and returns the cause; `Permanent` marker not retried; sentinel-permanent (`ErrPayloadType` &c.) not retried; **a zero computed backoff is floored, not spun** |
| `TestProducerPermanentBeatsRetryAfter` | 2 | both marker nestings — `Permanent` wins from either direction |
| `TestProducerDeadLetterFailure` | 2 | sink error → joined; sink **panic** → recovered and joined |
| `TestProducerDeadLetterDetachedContext` | 1 | the sink observes `ctx.Err() == nil` **as it saw it** (`lastCtxErr`), i.e. the divert really is detached |
| `TestProducerDeadLetterTimeout` | 1 | a hung sink is cut off by the timeout rather than blocking forever |
| `TestProducerRetryContextCancel` | 2 | cancel **during backoff** diverts (the common path); a **pre-cancelled** ctx stops after one attempt |
| `TestProducerHooks` | 2 | `OnRetry` + `OnDeadLetter` observed on both arms; a **panicking hook** is contained and logged |
| `TestProducerRetryAfter` | 6 | no marker → computed backoff; marker **floors above** the computed backoff; marker **cannot shorten** it; a **zero** `Retry-After` cannot shorten it either; clamped by an explicit cap; clamped by the **60 s default** cap |
| `TestProducerRetryBudget` | 2 | budget exhaustion **without** a `DeadLetter` returns the cause alone (no `ErrDeadLettered`); **with** one, diverts carrying `ErrRetryBudgetExhausted` |
| `TestNewProducerRetryValidation` | 11 | every construction-time typed-error branch: `ErrNoDeadLetter`, `ErrInvalidMaxAttempts`, `ErrUnboundedRetry` (zero value) vs. `MaxAttempts == 0` **with** a `Backoff` accepted, `ErrInvalidRetryAfterCap` (zero/negative), `ErrInvalidRetryBudget` (zero/negative), `ErrInvalidDeadLetterTimeout` (zero/negative) vs. positive accepted, nil-logger-keeps-default, non-nil logger, and unset cap/budget taking their defaults |
| `TestProducerScheduledSendIsNotRetried` | 1 | `SendAfter`/`SendAt` bypass the loop entirely (§3.1's stated scope decision, pinned rather than assumed) |

Two methodology rules the audit made binding, applied throughout:

- **Measured, two-phase waits.** A driver advances the fake clock to `want-1ns`, asserts the send has **not** returned,
  then advances the final nanosecond. A one-shot `Advance(want)` cannot detect **under**-waiting, so it would pass
  against a floor that never fired.
- **Any test asserting an interleaving must be measured, not assumed** — the Plan 021 lesson: a concurrent test there
  passed, was `-race` clean and line-covered while hitting its target arm **0 times in 200 iterations**. `jitter`'s new
  overflow arm is hit only ~28 % per draw, so its case loops 200 draws rather than trusting one.

**HTTP obligations move to [Plan 024](../plans/024-http-outbound.md)** with the rest of Phase 2 (§7): `httptest`-hermetic
and **no testcontainers**; status classification per arm; `Retry-After` in both header forms; body-close verified on
error and panic paths; O2 correlation propagation; O2 cleanup under a panic unwind.

## 6. Rejected alternatives

### 6.1 Adapter-side backoff loop inside `NewOutbound` (what ADR 0005 specified)

[ADR 0005](../adrs/0005-cenkalti-backoff-dependency.md) states the outbound HTTP adapter's POST-retry loop "is a real
tight loop and uses `cenkalti/backoff/v4` directly", and that this is **the** justification for cenkalti being an
accepted core dependency. **Rejected**, and ADR 0025 supersedes that clause:

- It puts reliability *in the adapter*, against ADR 0002's runtime-owned boundary, and helps **only** HTTP.
- It requires adding `cenkalti/backoff/v4` to `go.mod` — a real dependency every consumer pays for.
- **The dependency does not currently exist.** Despite ADR 0005 being Accepted since 2026-07-16 and CLAUDE.md listing
  it among four accepted core dependencies, `cenkalti/backoff/v4` is **absent from `go.mod`** and imported by no code
  (only referenced in a `reliability.go` comment). It was ratified for a use that never shipped. **CLAUDE.md's
  dependency policy must be corrected** as part of this increment.
- The producer-side design reuses `RetryPolicy`/`BackoffStrategy`/`ExponentialBackoff`, which already provide
  exponential growth, jitter, and a cap — so the library buys nothing that is not already present, except `Retry-After`,
  which §3.2 solves in ~15 stdlib lines.

### 6.2 A general `RetryingOutbound(out, policy)` decorator

Would additionally cover `To(sink)` flow steps and dead-letter sinks. **Declined by the user** in favour of the
narrower Producer option. The trade is recorded in §2.1: the decorator's extra reach is precisely where a second retry
authority would multiply attempts against the Consumer's existing redelivery.

### 6.3 Ignoring `Retry-After`

Rejected: it discards an explicit server instruction and risks hammering a rate-limited endpoint — the exact failure
ADR 0005 named as a requirement.

## 7. Increment shape — SPLIT (revised 2026-07-22)

**Superseded decision.** This section originally recorded the user's choice of **one increment** covering the core
retry and HTTP outbound O1/O2 together, over the recommended split, and named the risk it carried: a single review gate
spanning a **core change**, a **new adapter**, and a **new external SPI implementation** — the same shape that let the
`ChannelExchange` leak reach production in Spec 011 Phase 1.

**That risk materialized at design time, before any code.** The round-1 adversarial audit (three independent Opus
auditors over the complete bundle) returned **3 critical and ~20 major findings** spanning all three areas at once,
including two the combined plan would plausibly have shipped:

- **Redirect-following SSRF** — the outbound `*http.Client` follows redirects by default, so the URL actually fetched
  is whatever the remote endpoint names in `Location`. A webhook target answering `302 → 169.254.169.254` makes O2
  return cloud instance-metadata credentials into the flow as the reply payload; a 307/308 replays the POST body and
  custom allow-listed headers to the attacker's host. The "caller-configured URL" SSRF invariant was not the property
  it claimed to be.
- **Reflected XSS reopened from the outbound side** — the reply builder wrote the *remote server's* `Content-Type` onto
  the reserved `msgin.content-type`, the exact key [ADR 0023](../adrs/0023-http-channel-adapter.md) Addendum A1 forbids
  an untrusted party from setting, because `EncodeResponse` trusts it as the response media type.

**Revised decision: three increments, in dependency order.**

| Plan | Scope | Gate |
|---|---|---|
| [022](../plans/022-header-message-id-rename.md) | the `HeaderMessageID` rename (unrelated; zero coupling) | `/code-review` |
| [023](../plans/023-producer-outbound-retry.md) | **this spec** — core producer retry | `/code-review` + `/security-review` |
| [024](../plans/024-http-outbound.md) | Spec 011 Phase 2 — HTTP outbound O1/O2 | `/code-review` + a **dedicated** `/security-review`, this being the increment that introduces an outbound network client and a new untrusted-input boundary |

The core-before-dependent ordering the original mitigation described is now enforced by branch boundaries rather than
by task sequencing within one branch — the same principle, made structural. The cost is two extra merge cycles; the
audit's findings are the argument that they are cheap.

## 8. API surface & SemVer

**Additive only → minor bump.** No existing signature changes, and no behaviour changes for existing callers — a
`Producer` built without the new options behaves exactly as today, single-attempt passthrough. The full set delivered
by Plan 023 (amended 2026-07-22 — the original list named three symbols; the audit-forced bounds each brought their
own option and sentinel):

**Producer options (6), all `ProducerOption[T]`:**

| Symbol | Default | Explicit-invalid error |
|---|---|---|
| `WithProducerRetry(RetryPolicy)` | unset → single attempt | `ErrNoDeadLetter`, `ErrInvalidMaxAttempts`, `ErrUnboundedRetry` |
| `WithProducerRetryAfterCap(time.Duration)` | 60 s | `ErrInvalidRetryAfterCap` |
| `WithProducerRetryBudget(time.Duration)` | 2 m | `ErrInvalidRetryBudget` |
| `WithProducerDeadLetterTimeout(time.Duration)` | 30 s | `ErrInvalidDeadLetterTimeout` |
| `WithProducerHooks(Hooks)` | no hooks | — (nil-safe) |
| `WithProducerLogger(*slog.Logger)` | discard logger | — (nil ignored) |

**Other new core symbols:**

- `RetryAfter(err error, d time.Duration) error` — the transient-with-server-delay marker (§3.2).
- `BytesPayloadCodec` — the identity `PayloadCodec[[]byte]` (ADR 0025 §6).

**Error sentinels (6):** `ErrUnboundedRetry`, `ErrInvalidRetryAfterCap`, `ErrInvalidRetryBudget`,
`ErrInvalidDeadLetterTimeout`, `ErrRetryBudgetExhausted`, `ErrDeadLettered`. The last two are the **runtime**
sentinels — everything else is construction-time — and they are the increment's debuggability surface: they let a
caller distinguish "safely in the DLQ" from "lost outright", and "the budget ran out" from "we tried N times".

**`adapter/http`'s `NewOutbound`/`Outbound`, `NewExchange`/`Exchange` and their options move to
[Plan 024](../plans/024-http-outbound.md)** with the rest of Phase 2 (§7); they are not part of this increment's
surface.

> ⚠️ **`gorelease` cannot verify this.** The repo has **zero git tags**, so it reports "inferred base version: none"
> instead of a compatibility diff. Compatibility is established by inspection, as in every prior increment. Cutting a
> first tag would close this standing blind spot.

## 9. Traceability

- **Spec:** this document, plus the [Spec 011 §3.4](011-http-adapter.md) delta it drives.
- **ADR:** [0025 — Producer-side outbound retry](../adrs/0025-producer-outbound-retry.md); supersedes the
  outbound-HTTP clause of [ADR 0005](../adrs/0005-cenkalti-backoff-dependency.md); honors
  [ADR 0002](../adrs/0002-adapter-spi.md).
- **Plan:** [023](../plans/023-producer-outbound-retry.md).
- **Depends on:** [Spec 012](012-exchange-panic-safe-cleanup.md) §7 — the `RequestReplyExchange` no-leak-on-unwind
  contract that O2 is the first external implementation of.
