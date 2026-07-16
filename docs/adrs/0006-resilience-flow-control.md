# ADR 0006 — Resilience & flow control

- **Status:** Accepted (2026-07-16)
- **Context source:** [Spec 001 — Messaging core](../specs/001-messaging-core.md) §7.4; user requirement (a message flood must not hammer the system or a downstream); `CLAUDE.md` → Production robustness, Dependency policy
- **Related:** [ADR 0002 — Adapter SPI](0002-adapter-spi.md), [ADR 0004 — clockwork](0004-clockwork-dependency.md), [ADR 0005 — backoff](0005-cenkalti-backoff-dependency.md)

## Context

`msgin` targets production robustness. A concrete requirement: a **flood** of inbound messages —
most acutely a large backlog suddenly appearing in the `sql` adapter's message table — must not
overrun the runtime's memory, saturate the worker pool uncontrollably, or hammer a downstream
dependency. Resilience must ship in the **first phase**, not be retrofitted.

Key observation: for a `PollingSource` (sql/pgx) the **runtime controls the pull rate** — nothing
pushes into msgin — so a flood cannot overrun it if the runtime simply pulls less. The source (the
DB table) is a durable buffer; surplus waits there safely. Backpressure is intrinsic to pull. Push
sources (redis/nats/http) can be pushed at, so they need buffer bounding and an overflow policy.

Two competing forces on *how* to implement the throttles: the **minimal-dependencies** mandate (don't
force `x/time/rate` / `sony/gobreaker` on every consumer) and the **clockwork-testability** principle
(governor timing must be deterministic in tests — third-party rate limiters use the real clock).

## Decision

Ship a layered flow-control model in the first phase. The **credit gate is mandatory and always
active**; the rest are optional runtime options. All are `clockwork`-driven; the pluggable ones are
**interfaces with dependency-free default implementations** — no new forced dependency.

1. **Credit-based flow control — `WithMaxInFlight(n)` (mandatory, always bounded).** The runtime caps
   **claimed-but-unsettled** messages to `n`. **Credit is acquired at fetch/claim** (a `PollingSource`
   fetching `k` does `inFlight += k`) — *not* at dispatch — so messages already sitting in the internal
   buffer are counted and the poller cannot over-pull past `n` (audit NF-4; the earlier "dispatched"
   wording let a fetch see `inFlight≈0` and over-pull into the buffer). A `PollingSource` fetches
   `min(maxBatch, n − inFlight)` and skips polling at zero credit → the source buffers the surplus.
   **Every terminal settle releases exactly one credit — including `Nack`** (audit NF-5): a Nacked
   message leaves the credit pool (returns to the source, or enters a separately-bounded in-process
   delay-park) and re-acquires on re-fetch, so messages sleeping their backoff cannot pin all `n`
   credits and starve fresh work. Release is idempotent (guarded against double-release). A
   `StreamingSource` uses the bounded internal buffer of size `n` as the credit pool (no double-count).
   This is the load-bearing flood defense.
2. **Rate limiting — `WithRateLimit(RateLimiter)` (optional).** `RateLimiter interface { Wait(ctx) error }`;
   default a small `clockwork`-driven token bucket (rps + burst); `golang.org/x/time/rate` is a
   drop-in plug-in, not a dependency.
3. **Handler timeout — `WithHandlerTimeout(d)`.** Each handler runs under a derived context deadline;
   a stuck handler is cancelled → transient failure, not a pinned worker.
4. **Circuit breaker — `WithCircuitBreaker(CircuitBreaker)` (optional).** On repeated
   handler/outbound failure the breaker opens and **gates dispatch, not just ingress** (audit NF-10):
   the poller/stream pauses **and** workers stop draining the buffer (buffered messages are `Nack`ed,
   releasing credit), so an open breaker does not keep hammering the failing downstream from the buffer.
   The `clockwork` cool-down timer half-opens and **explicitly signals the parked ingress goroutine**
   (no missed-wakeup stuck-open). Interface with a small `clockwork`-driven default; `sony/gobreaker` is
   a plug-in.
5. **Overflow policy — `WithOverflow(policy)` (push sources).** `Block` (default), `DropNewest`,
   `DropOldest`, or `Reject` (HTTP `503`/`429`). A dropped message is **`Nack`ed and its credit
   released** (audit NF-14) so an at-least-once source can redeliver; the at-most-once loss case is
   documented per adapter.

Concurrency limiting (`WithConcurrency`, ADR 0002) composes with these. Ingress order: rate-limit →
credit gate → bounded buffer → concurrency-limited workers under handler timeout, breaker able to
pause ingress.

## Consequences

**Positive**
- A message flood cannot hammer the system: pull adapters self-limit to handler capacity and leave
  the backlog durably in the source; push adapters bound their buffer and apply a policy.
- Downstream protection: rate limit caps throughput, the circuit breaker stops pulling from a failing
  dependency, handler timeout prevents worker starvation.
- Minimal-deps mandate preserved — no forced `x/time/rate` / `sony/gobreaker`; both remain optional
  plug-ins behind interfaces.
- Fully deterministic tests — every governor is `clockwork`-driven, so flood/limit/breaker behavior is
  asserted under a fake clock (no sleeps, no flakes).

**Negative / costs**
- More runtime options and internal state (credit counter, limiter, breaker) to implement and test —
  justified by the production-robustness requirement.
- Default implementations of the token bucket and circuit breaker are first-party code (small); they
  must be correct and tested rather than delegated. Mitigated by keeping them minimal and offering the
  well-known libraries as plug-ins for users who prefer them.
- Credit accounting must be exact across Ack/Nack/DLQ/invalid terminal paths (a leaked credit
  throttles throughput; an over-released credit breaks the bound) — a specific test target.

**Rejected alternatives**
- **No flow control / rely on the bounded channel alone** — bounds memory but still lets a
  `PollingSource` over-fetch and hammer downstreams; does not satisfy the requirement.
- **Force `golang.org/x/time/rate` + `sony/gobreaker` as core deps** — well-known and capable, but two
  more forced consumer dependencies and real-clock timing that breaks deterministic tests. Kept as
  optional plug-ins instead.
