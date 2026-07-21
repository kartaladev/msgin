# ADR 0025 — Producer-side outbound retry (supersedes ADR 0005's outbound-HTTP clause)

- **Status:** Proposed (2026-07-21) — records the decisions of
  [Spec 013](../specs/013-producer-outbound-retry.md), settled with the user in brainstorming. Pending the
  **two-round adversarial Opus audit** of the complete bundle before any code.
- **Supersedes:** the **outbound-HTTP clause** of [ADR 0005 — cenkalti/backoff](0005-cenkalti-backoff-dependency.md)
  (its `RetryPolicy.Backoff` closed-form decision stands; only the "the outbound HTTP adapter uses `cenkalti/backoff`
  directly" part is superseded). ADR 0005's Status is annotated accordingly.
- **Honors:** [ADR 0002 — Adapter SPI](0002-adapter-spi.md) (runtime-owned reliability),
  [ADR 0004 — clockwork](0004-clockwork-dependency.md).
- **Spec:** [013](../specs/013-producer-outbound-retry.md) · **Plan:** [023](../plans/023-producer-outbound-retry.md)

## Context

`RetryPolicy` governs only the **inbound** settlement path (`consumer.go`). `Producer.Send` (`producer.go`) is a bare
passthrough to `OutboundAdapter.Send` — **no retry, no backoff, no dead-letter**. Two consequences:

1. [Spec 011 §3.4](../specs/011-http-adapter.md)'s O1 webhook classification claims "the runtime **retries** per
   `RetryPolicy`" for transient responses. On the `Producer` path that is **false**. Spec 011 flagged this as an open
   point to resolve in this increment; it is resolved here.
2. ADR 0005 planned the fix in the wrong place — inside the HTTP adapter, using `cenkalti/backoff/v4` — and that
   dependency, though **Accepted** since 2026-07-16 and listed in CLAUDE.md as one of four accepted core dependencies,
   **is absent from `go.mod` and imported by nothing**. It was ratified for a use that never shipped.

An outbound send reached via a flow step (`To(sink)`) inside a **Consumer-driven** flow *is* retried today — by full
message redelivery. A `Producer.Send` has no such enclosure. That asymmetry is the real gap.

## Decision

### 1. Retry belongs at the producer, in core — not in the adapter

`WithProducerRetry(p RetryPolicy) ProducerOption[T]` applies the **existing** `RetryPolicy` / `BackoffStrategy` /
`ExponentialBackoff` machinery to `producer.Send`. Every outbound adapter — SQL, Redis, NATS, HTTP, and any future one —
gains retry from a single change at the layer ADR 0002 assigns reliability to. Adapters keep doing only what ADR 0002
says they do: **classify** (`Permanent` vs transient) and let the runtime decide.

Validation reuses `RetryPolicy.Validate()`, so a finite `MaxAttempts` without a `DeadLetter` fails in `NewProducer`
(`ErrNoDeadLetter`) rather than at send time. No new sentinel. Waits run on the producer's **already-injected**
`clockwork.Clock`, so tests need no real sleeps. `ctx` cancellation aborts a wait. Without the option, behaviour is
byte-for-byte unchanged.

**`Permanent` short-circuits before consuming an attempt or dead-lettering** — a permanent outbound failure is the
caller's to see, not something to bury in a DLQ after N pointless attempts.

### 2. Scope is `Producer` only — deliberately, not by omission

The alternative, a general `RetryingOutbound(out, policy)` decorator covering `To(sink)` and dead-letter sinks too, was
surfaced with its trade-offs and **declined by the user**.

**Why the narrow scope is coherent:** inside a flow the runtime **already** owns retry via redelivery, so an inner loop
would multiply attempts (inner × outer) and re-run every prior step's side effects on each outer retry. Filling only the
`Producer` gap keeps **exactly one retry authority per path**.

**Consequence, which must be documented on the option and in the HTTP adapter godoc:** an O1 webhook driven as
`To(outbound)` in a flow relies on Consumer redelivery; a flow with **no inbound source** gets **no retry at all**.

### 3. `Retry-After` via a marker, mirroring `Permanent`

`BackoffStrategy.Delay(attempt)` is stateless and closed-form by design and cannot express a server-instructed delay.
`RetryAfter(err, d)` wraps a transient error with one, mirroring `Permanent(err)`'s shape, `Unwrap` transparency and
nil handling — no new idiom. `Permanent` **wins** over `RetryAfter` when both are present.

`WithProducerRetryAfterCap(d)` clamps the honored delay to a documented, conservative-but-generous default (overridable),
because an unbounded server-supplied delay lets a hostile or misconfigured endpoint park the caller's goroutine
indefinitely and `context.Background()` callers have no other bound. Effective wait = `min(serverDelay, cap)`, always
further bounded by `ctx`. This satisfies CLAUDE.md's safe-defaults gate.

### 4. `cenkalti/backoff/v4` is NOT adopted

The producer-side design reuses machinery that already exists and is tested. The only capability cenkalti would have
added is `Retry-After`, which §3 solves in a few stdlib lines. Therefore:

- `cenkalti/backoff/v4` stays **out of `go.mod`**.
- ADR 0005's outbound-HTTP clause is **superseded**; its closed-form `RetryPolicy.Backoff` decision **stands**.
- **CLAUDE.md's Dependency policy must be corrected** — it currently lists cenkalti among the accepted core
  dependencies, which does not match the module. The core's real third-party set is `clockwork`, `robfig/cron`,
  `expr-lang/expr`.
- The stale comment at `reliability.go` ("cenkalti enters only via the outbound-HTTP adapter") is corrected.

### 5. O2 is bound by the `RequestReplyExchange` contract from Spec 012

`NewExchange` is the **first external implementation** of that SPI. It holds request-scoped state — an in-flight
`*http.Request` and a response body that must be closed — of exactly the class whose leak
[Spec 012](../specs/012-exchange-panic-safe-cleanup.md) fixed in `ChannelExchange`. Per
[ADR 0022 Addendum A3](0022-messaging-gateway.md), it MUST release that state on **every** exit path including a panic
unwind (deferred cleanup), and MUST NOT recover a caller panic into an error return.

## Consequences

**Positive**
- The gap is closed at the correct layer; **all** outbound adapters benefit, not just HTTP.
- **No new dependency** — and one phantom dependency is retired from the documentation instead of being added to the
  module.
- Reuses tested code (`RetryPolicy`, `ExponentialBackoff`, `Validate`, the producer's clock) rather than duplicating it.
- Spec 011's O1 classification becomes true rather than aspirational.

**Negative / accepted**
- `To(sink)` flow steps get no producer retry (§2) — deliberate, documented.
- The retry loop holds the caller's goroutine; inherent to a synchronous `Send`, documented on the option.
- Nested retry (producer retry inside a Consumer-driven flow) multiplies attempts — documented; opt-in by construction.
- One review gate covers a core change plus a new adapter plus a new SPI implementation (the user's sequencing choice);
  Plan 023 mitigates by landing and reviewing the core tasks first.

**API/SemVer:** additive only → **minor** bump. `gorelease` cannot verify it (repo has no tags); compatibility is
established by inspection.
