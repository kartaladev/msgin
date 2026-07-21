# Spec 013 — Producer-side outbound retry (`WithProducerRetry`, `RetryAfter`)

- **Status:** **Designed (2026-07-21)** — settled with the user in `superpowers:brainstorming`. Pending the mandatory
  **two-round adversarial Opus audit** of the complete bundle (this spec + the [Spec 011 §3.4 delta](011-http-adapter.md)
  + [ADR 0025](../adrs/0025-producer-outbound-retry.md) + [Plan 023](../plans/023-producer-outbound-retry.md))
  before any code is written.
- **Realized by:** [Plan 023](../plans/023-producer-outbound-retry.md) — **jointly with Spec 011 Phase 2**
  (HTTP outbound O1/O2). The user chose one increment covering both; §7 records the risk that decision carries and how
  the plan mitigates it.
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

- **Validation at construction.** `NewProducer` calls the existing `p.Validate()`, so `MaxAttempts > 0` without a
  `DeadLetter` returns `ErrNoDeadLetter`, and a negative `MaxAttempts` returns `ErrInvalidMaxAttempts` — at build time,
  not at send time. No new error sentinel is required.
- **Default: unchanged behaviour.** Without the option, `Send` remains a single passthrough attempt. This is additive;
  no existing caller changes semantics.
- **The loop** (in `producer.Send`, after `box`):
  1. Attempt `p.out.Send(ctx, boxed)`. `nil` → return `nil`.
  2. **Permanent short-circuits.** If the existing `isPermanent(err)` reports true — an explicit `Permanent` marker,
     `ErrPayloadType`, `ErrPayloadDecode`, `ErrPayloadTooLarge` — return it immediately, consuming no attempt budget
     and **never** dead-lettering on the retry path. A permanent outbound failure is the caller's to see.
  3. **Exhaustion.** With `MaxAttempts > 0`, once attempts are spent: route the message to `policy.DeadLetter` and
     return. A dead-letter send that *itself* fails is wrapped and returned alongside the original error
     (`errors.Join`), never swallowed.
  4. **Wait.** Otherwise sleep `policy.delayFor(attempt)` — or the server-provided delay (§3.2) — on the **producer's
     existing injected `clockwork.Clock`** (`producerConfig` already carries one, so tests drive retry with a fake
     clock and no real sleeps).
  5. **`ctx` cancellation aborts the wait** and returns `ctx.Err()` (wrapping the last attempt error), so a cancelled
     caller is never parked. Everything is cancellable (CLAUDE.md robustness gate).
- **`SendAfter`/`SendAt` are out of scope** for retry in v1: they delegate to a `ScheduledSender`, whose delivery is
  the adapter's store, not a live network call. Stated explicitly so the omission is a decision, not an oversight.

### 3.2 `RetryAfter(err error, d time.Duration) error`

A server that answers `429` or `503` with a `Retry-After` header is *instructing* the client. `BackoffStrategy.Delay`
is **stateless and closed-form by design** (`backoff.go`: "pure arithmetic, safe to share across goroutines") and
cannot express "wait the 30s the server asked for". A marker carries it out-of-band instead:

```go
// RetryAfter marks err as transient with a server-provided delay: the producer's
// retry loop waits at least d before the next attempt, overriding the computed
// backoff. Transparent to errors.Is/As via Unwrap. RetryAfter(nil, d) returns nil.
func RetryAfter(err error, d time.Duration) error
```

This deliberately mirrors `Permanent(err)` (`reliability.go`) — same wrapper shape, same `Unwrap` transparency, same
nil handling — so the codebase gains no new idiom. An unexported `retryAfterOf(err) (time.Duration, bool)` reads it via
`errors.As`, matching `isPermanent`'s structure. **A `Permanent` marker wins over a `RetryAfter` marker** if both are
present (permanent means *do not retry*, so a delay is meaningless).

**`WithProducerRetryAfterCap(d time.Duration) ProducerOption[T]`** clamps the honored delay. An unbounded server-supplied
delay lets a hostile or misconfigured endpoint park the caller's goroutine indefinitely; the caller's `ctx` bounds it
only if they passed a deadline, and `context.Background()` is common. Per CLAUDE.md's safe-defaults gate this ships a
**documented, conservative default that is generous relative to any plausible legitimate `Retry-After`** and is
overridable. The effective wait is `min(serverDelay, cap)`, and always additionally bounded by `ctx`.

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

## 5. Test obligations

- **Blackbox only** (`package msgin_test`, `package stdlib_test`), exported API only; assert-closure tables;
  `t.Context()`; `goleak` guards.
- **Retry timing is driven by `clockwork.NewFakeClock()`** — no real sleeps anywhere in the suite.
- Hot-path branches each need a case: success-first-attempt; transient→retry→success; permanent→no retry (assert the
  attempt count, not just the error); exhaustion→dead-letter; dead-letter failure→joined error; `ctx` cancel during the
  wait; `RetryAfter` honored over computed backoff; `RetryAfter` clamped by the cap; `Permanent` beating `RetryAfter`.
- **HTTP:** `httptest`-hermetic, **no testcontainers**. Status classification per arm; `Retry-After` in both header
  forms; body-close verified on error and panic paths; O2 correlation propagation; O2 cleanup under a panic unwind.
- **Any test asserting an interleaving must be measured, not assumed** — the Plan 021 lesson: a concurrent test there
  passed, was `-race` clean and line-covered while hitting its target arm **0 times in 200 iterations**.

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

**Additive only → minor bump.** New exported symbols: `WithProducerRetry`, `WithProducerRetryAfterCap`, `RetryAfter`
(core); `NewOutbound`/`Outbound`, `NewExchange`/`Exchange` and their options (`adapter/http`, `adapter/http/stdlib`).
No existing signature or behaviour changes — a `Producer` built without the new options behaves exactly as today.

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
