# ADR 0025 — Producer-side outbound retry (supersedes ADR 0005's outbound-HTTP clause)

- **Status:** **Accepted (2026-07-22)** — records the decisions of
  [Spec 013](../specs/013-producer-outbound-retry.md), settled with the user in brainstorming and **amended by the
  two-round adversarial Opus audit** of the complete bundle, which ran before any code and changed §1 and §3
  materially. Implemented by [Plan 023](../plans/023-producer-outbound-retry.md).
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

#### 1.1 Four bounds, added by the audit — the loop is bounded from every direction

The design as first drafted was bounded only by `MaxAttempts` and `ctx`. Both adversarial rounds attacked that, and the
ratified decision is that **a retry loop running on the caller's own goroutine must be bounded structurally, not by
caller diligence** — `Producer.Send` is routinely called with `context.Background()`, which supplies no bound at all.
Four bounds ship, each closing a distinct escape:

1. **`ErrUnboundedRetry` — a construction-time structural rejection.** `MaxAttempts == 0` (forever) with a nil
   `Backoff` (no delay) — the `RetryPolicy` **zero value** — is rejected by `NewProducer`. The predicate is
   deliberately structural and **not** a probe of `delayFor`: round 1 widened it to `delayFor(1) <= 0` and **round 2
   reverted that**, because `BackoffStrategy` is an *exported public interface*, so evaluating it inside a constructor
   invokes caller code that may panic (CLAUDE.md forbids panicking on caller input) or block, is non-deterministic
   whenever `RandomizationFactor > 0` (jitter can truncate a sampled delay to 0 and intermittently reject a valid
   config), and sampling one index false-rejects the legitimate "first retry immediate, then back off" shape. **This
   makes `RetryPolicy{}` valid for a Consumer and invalid for a Producer** — a deliberate asymmetry, because on the
   Consumer the same policy means *broker* redelivery, which is paced, whereas here it is a zero-delay spin on the
   caller's goroutine. Spec 013 §4.3 records the wart and why failing loudly beats a silent stall.
2. **A 100 ms floor (`minRetryDelay`) on every computed wait.** This is where the hazard the reverted probe was
   reaching for is actually handled: `ExponentialBackoff{}` is a **non-nil** strategy whose `Delay` is always `0`, so
   it slips past bound 1 structurally. The floor is deliberately **not configurable** — "never hard-code a policy the
   caller cannot change" governs behaviour a caller might legitimately need, and a caller wanting a *longer* wait sets
   `Backoff`; there is no legitimate reason to want a shorter one here.
3. **An always-on retry budget** (`WithProducerRetryBudget`, 2 m default) bounding cumulative retry wall-clock. Round 1
   scoped it to `MaxAttempts == 0` to avoid "silently truncating an explicit attempt count"; **round 2 reverted that**,
   because it left `MaxAttempts > 0` completely unbudgeted —
   `RetryPolicy{MaxAttempts: 1_000_000, Backoff: ExponentialBackoff{}}` is `10⁶ × 100 ms` ≈ **27.7 hours** on the
   caller's goroutine, the identical flood bound 1 rejects, respelled with a large `MaxAttempts` instead of `0`. The
   budget therefore **always** applies, with no flag and no unbudgeted mode, and the truncation objection is answered
   with **information rather than by removing the bound**: `ErrRetryBudgetExhausted` makes a budget stop
   distinguishable from genuine attempt exhaustion. *Answer an "it hides information" objection with information.*
4. **A timed, detached dead-letter divert** (`WithProducerDeadLetterTimeout`, 30 s default). The divert must be
   **detached** (`context.WithoutCancel`) because the usual reason the loop is ending is that `ctx` died, and diverting
   on that same ctx would fail too — leaving the message in neither the target nor the DLQ. But detaching **without**
   bounding hands a hung sink (blackholed TCP, wedged DB) the ability to block the caller's goroutine **forever, immune
   to their own cancel** — strictly worse than the un-retried passthrough, and a violation of the
   everything-cancellable and graceful-shutdown constraints. Detached **and** timed is the only combination that is
   neither lossy nor unbounded.

#### 1.2 Divert on cancellation — a cancelled `Send` is terminal for the message

**Decided:** a `ctx` cancellation *during a backoff wait* routes the message to the `DeadLetter` sink and returns
`errors.Join(divertResult, ctx.Err())`, rather than returning the cancellation bare.

The round-1 security audit found that returning bare **loses the message on the common cancellation path** — the design
had a divert only for the narrow "already cancelled at exhaustion" case, which is not how cancellation actually
arrives. Since the divert is detached and timed (bound 4), it still completes after the caller's ctx is dead.

**The trade is real and accepted:** cancelling a `Send` no longer returns promptly — it returns only once the detached
divert finishes, so a caller waits up to the dead-letter timeout per in-flight `Send`, and a mass shutdown dumps
**every** in-flight message into the DLQ rather than dropping it. **A message in the DLQ is recoverable; a lost one is
not.** Callers size `WithProducerDeadLetterTimeout` against their shutdown deadline and should expect DLQ volume
proportional to in-flight sends at shutdown; both facts are on the two options' godoc.

**Defaults are constrained from both sides, not chosen freely:** `cap (60 s) < budget (2 m) < the shortest plausible
upstream lease (adapter/database/sql's 5 m default)`. The lower inequality stops the budget defeating the
`Retry-After` compliance the cap exists to permit; the upper stops a `Send` outliving the lease covering the message
being handled, which would let the source reclaim and redeliver it mid-send. **The numbers are arguable; the two
inequalities are what must not change.**

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

**Corrected (2026-07-22): the marker is a MINIMUM, never an override.** This ADR originally described `RetryAfter` as
"overriding the computed backoff". That is not what shipped, and it was the wrong decision. **RFC 9110 §10.2.3** makes
`Retry-After` a *lower bound* on when a client may retry — not an instruction to retry sooner. The effective wait is

```
max( computedBackoff , min(serverDelay, retryAfterCap) )      then floored to minRetryDelay (100 ms)
```

so a server may only **lengthen** the client's own backoff. Under the override reading, a server answering
`Retry-After: 0` would **shorten** the client's backoff to zero: a hot-spin lever handed to the very party the client
is already failing against. Same header, opposite security posture.

`WithProducerRetryAfterCap(d)` clamps the honored delay (default **60 s**, overridable), because an unbounded
server-supplied delay lets a hostile or misconfigured endpoint park the caller's goroutine indefinitely and
`context.Background()` callers have no other bound. The default deliberately does **not** cover the longest legitimate
case (a maintenance `503` asking for 120 s): defaulting *above* the worst legitimate value would optimise for obeying a
remote instruction over bounding the caller, which is the wrong side of CLAUDE.md's safe-defaults gate. A caller who
needs it raises the cap explicitly. The wait is always further bounded by `ctx` **and** by the retry budget (§1.1).

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

**Delivered** as `msghttp.Exchange` / `NewExchange` in `adapter/http` (package `msghttp`) — the **first external
`RequestReplyExchange`** — by [Plan 024](../plans/024-http-outbound.md) (Spec 011 Phase 2); its INV-7 pins the
single-`defer` no-leak-on-unwind cleanup against a panicking response body.

### 6. `BytesPayloadCodec` — explicit, never an automatic default

**Decided:** ship an identity `PayloadCodec[[]byte]` that passes bytes through unchanged, and require callers to
**pair it explicitly**:

```go
msgin.NewProducer[[]byte](out, msgin.WithProducerCodec[[]byte](msgin.BytesPayloadCodec{}))
```

**Why it is needed.** `resolveCodec` defaults a wire adapter with no codec to `JSONPayloadCodec`, and `json.Marshal` of
a `[]byte` is a **quoted base64 string** — a raw body meant to go out as `payload` goes out as `"cGF5bG9hZA=="`. That
is almost never what a caller sending raw bytes intends, and it is not hypothetical: the round-1 audit found Task 4's
own DLQ assertion could not pass for exactly this reason, which is what made this a prerequisite rather than a
nice-to-have.

**Why not make it the automatic default when `T == []byte`.** That was considered and rejected. Adapters that already
persist base64-encoded envelopes — `database/sql`, `redis`, `nats` — would **silently change their on-wire format** for
every existing `[]byte` caller. A silent wire-format change is precisely the class of breakage a library must not
inflict; the pairing is the caller's explicit, greppable choice. `ExampleWithProducerRetry` doubles as its
documentation.

**Two residuals, both consequences of the pass-through being exact,** recorded on the type's godoc:

- It **removes an accidental escaping layer.** `JSONPayloadCodec`'s quoting and escaping neutralised some hostile bytes
  as a side effect — never as a security control, but in practice. This codec emits the caller's bytes verbatim, so
  sanitising or validating the payload for whatever the sink interprets it as (SQL, a shell, HTML, a log) is wholly the
  caller's and the adapter's responsibility.
- **`Encode(nil)` returns `nil`**, where `JSONPayloadCodec` returned the four bytes `null`. A store with a `NOT NULL`
  payload column that accepted a nil payload before will now reject it.

Both methods **copy** (`bytes.Clone`), so neither the caller's slice nor the returned one can be mutated through the
other — messages are immutable by contract and may be shared across a pub-sub channel, so aliasing would break that.

## Consequences

**Positive**
- The gap is closed at the correct layer; **all** outbound adapters benefit, not just HTTP.
- **No new dependency** — and one phantom dependency is retired from the documentation instead of being added to the
  module.
- Reuses tested code (`RetryPolicy`, `ExponentialBackoff`, `Validate`, the producer's clock) rather than duplicating it.
- Spec 011's O1 classification becomes true rather than aspirational.
- **A genuine observability gain, not just a retry.** The terminal outcomes are **distinguishable in the error value**,
  which the pre-audit design could not do: `ErrDeadLettered` separates "safely in the DLQ" from "failed outright" (a
  successful divert deliberately does **not** become a `nil` error — the caller must still learn the send never reached
  its target), and `ErrRetryBudgetExhausted` separates "the budget ran out" from "we tried N times", which are
  different operational signals. `Hooks.OnRetry`/`OnDeadLetter` expose both without the caller parsing errors, and
  `OnDeadLetter` fires on **both** divert arms carrying exactly the error `Send` is about to return — "the DLQ is down
  and the message is lost" is the single most operationally important event this loop can produce, and the pre-audit
  design emitted no telemetry for it at all.

**Negative / accepted**
- **`WithProducerRetry` changes the delivery guarantee to AT-LEAST-ONCE with caller-visible duplicates.** A retry after
  a timeout or reset re-sends a request the peer may already have committed; the producer cannot tell "never arrived"
  from "arrived, reply lost". The **receiver must be idempotent** — a stable idempotency key in a header (the message
  ID is one) plus dedup, or a naturally idempotent operation. Stated on the option's godoc and in Spec 013 §4.1;
  CLAUDE.md forbids leaving a delivery guarantee implied.
- **A cancelled `Send` may cost up to the dead-letter timeout and writes a DLQ entry** (§1.2). Cancelling no longer
  returns promptly — up to 30 s by default per in-flight `Send` — and a mass shutdown fills the DLQ with every
  in-flight message rather than dropping them. Accepted: recoverable beats lost.
- **The always-on default budget makes `MaxAttempts == 0` finite on a Producer** — "until the budget" (2 m default)
  rather than "forever" — and bounds an explicit `MaxAttempts` too. This diverges from `RetryPolicy`'s own godoc
  reading of the same field on the Consumer path, where `MaxAttempts == 0` still means forever via broker redelivery.
  It is additive (the option is new, so nobody can have relied on it) and made **non-silent** by
  `ErrRetryBudgetExhausted`; `retry.go`'s godoc is amended to state the divergence per-path.
- **`RetryPolicy{}` is valid for a Consumer and invalid for a Producer** (`ErrUnboundedRetry`, §1.1) — one struct
  meaning two things at two call sites. A wart, accepted because the alternative turns an obvious caller mistake into a
  silent multi-minute stall.
- **The dead-letter timeout is a `context` deadline, not clockwork-driven** — a documented deviation from the
  injectable-time rule. `context.WithTimeout` has no injectable-clock form, so a fake clock cannot drive its expiry
  arm; the test reaches it with a 1 ns timeout instead.
- `To(sink)` flow steps get no producer retry (§2) — deliberate, documented.
- The retry loop holds the caller's goroutine; inherent to a synchronous `Send`, documented on the option.
- **The retry is per-process.** Across N scaled instances each retries independently, so a throttling endpoint sees
  N × the load it asked to shed and `Retry-After` compliance is per-instance, not fleet-wide. The core cannot fix this
  without shared state; the seam is ADR 0006's rate-limit/breaker interfaces (Spec 013 §4.2).
- Nested retry (producer retry inside a Consumer-driven flow) multiplies attempts — documented; opt-in by construction.
- ~~One review gate covers a core change plus a new adapter plus a new SPI implementation~~ — **resolved**: the
  round-1 audit's findings caused the increment to be **split** (Spec 013 §7), so this ADR's core decision landed
  under its own `/code-review` + `/security-review` gate in Plan 023, and the HTTP outbound gets a dedicated one in
  Plan 024.

**API/SemVer:** additive only → **minor** bump. `gorelease` cannot verify it (repo has no tags); compatibility is
established by inspection.
