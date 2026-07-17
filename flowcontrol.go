package msgin

import (
	"context"
	"time"
)

// RateLimiter paces ingress. Wait blocks until the caller may proceed or ctx is
// done. The default is a dependency-free clockwork token bucket (NewTokenBucket);
// golang.org/x/time/rate.Limiter satisfies this interface directly.
//
// Implementations MUST NOT panic: the runtime invokes Wait directly on the
// ingress goroutine and does not recover a panic here (unlike handler/hooks,
// which are recovered by safeHandle/safeFire), so a panicking implementation
// crashes the process. The shipped default (NewTokenBucket) never panics.
type RateLimiter interface {
	Wait(ctx context.Context) error
}

// CircuitBreaker gates dispatch on downstream health (spec §7.4.5, NF-10).
//
//   - Allow reports whether work may proceed now (closed or half-open).
//   - Record feeds the outcome of an allowed dispatch back to the breaker.
//   - HalfOpen returns a channel the runtime parks on while Allow reports
//     false; it is closed (or replaced with a fresh open channel per
//     open-cycle) only when the breaker transitions to half-open.
//     Implementations MUST NOT return an already-closed channel while Allow
//     continues to report false — doing so causes the runtime's ingress loop
//     (admitBreaker) to busy-spin (it re-subscribes, re-checks Allow, and
//     immediately falls through the already-closed channel in a tight loop,
//     with no cooldown to rate-limit the spin). The default NewCircuitBreaker
//     mints a fresh channel per open-cycle and only closes it from
//     toHalfOpen, so this contract holds by construction.
//
// The default is a dependency-free clockwork breaker (NewCircuitBreaker);
// sony/gobreaker can be adapted via its OnStateChange callback.
//
// Implementations MUST NOT panic: the runtime invokes Allow/Record/HalfOpen
// directly on the ingress and worker goroutines and does not recover a panic
// here (unlike handler/hooks, which are recovered by safeHandle/safeFire), so
// a panicking implementation crashes the process. The shipped default
// (NewCircuitBreaker) never panics.
type CircuitBreaker interface {
	Allow() bool
	Record(success bool)
	HalfOpen() <-chan struct{}
}

// ProbeGate is an OPTIONAL CircuitBreaker capability (ADR 0009 D2). A breaker that
// also implements it lets the runtime admit a bounded number of half-open probes
// at the DISPATCH gate rather than the whole half-open state — fixing the probe
// storm under WithConcurrency(N>1), where otherwise every worker reaching the gate
// while half-open is admitted instead of a single canary.
//
// TryProbe is a CONSUMING acquire (it may set internal probe state) called ONLY at
// the dispatch gate and ALWAYS paired with a following Record; the runtime's
// ingress open-check keeps using the idempotent Allow. It reports whether THIS
// dispatch may proceed: closed → true (unlimited); open → false; half-open → true
// for one probe, false for the rest until a Record settles it. A returned false
// consumes nothing (the message is Nacked without a Record).
//
// A CircuitBreaker that does NOT implement ProbeGate keeps the prior behavior (the
// dispatch gate falls back to Allow, admitting the whole half-open state). The
// default NewCircuitBreaker implements ProbeGate; a sony/gobreaker wrapper SHOULD
// implement TryProbe (mirroring gobreaker's own half-open MaxRequests) to get
// single-probe under N>1 — otherwise the runtime logs a one-time warning at Run.
//
// Implementations MUST NOT panic (same contract as CircuitBreaker); the runtime
// fail-opens (admits) on a panic but cannot rescue a plug-in that wedges its own
// probe state (ADR 0009 D1).
type ProbeGate interface {
	TryProbe() bool
}

// OverflowPolicy selects what a push source's ingress does when the credit pool
// is exhausted (spec §7.4.6). Block backpressures; the Drop/Reject policies shed.
type OverflowPolicy int

const (
	// OverflowBlock backpressures the stream loop until a credit frees (default).
	OverflowBlock OverflowPolicy = iota
	// OverflowDropNewest drops the just-arrived message (Nack, no requeue).
	OverflowDropNewest
	// OverflowDropOldest is accepted for API stability; on a streaming source
	// with no discrete buffer stage it behaves as OverflowDropNewest until the
	// Poller buffer lands (Plan 004) — see ADR 0008 D9.
	OverflowDropOldest
	// OverflowReject is HTTP-semantics (503/429); on a non-HTTP streaming source
	// it behaves as OverflowDropNewest until adapter/http lands — see ADR 0008 D9.
	OverflowReject
)

// String renders the policy; unknown values render as OverflowBlock (the safe default).
func (p OverflowPolicy) String() string {
	switch p {
	case OverflowDropNewest:
		return "OverflowDropNewest"
	case OverflowDropOldest:
		return "OverflowDropOldest"
	case OverflowReject:
		return "OverflowReject"
	default:
		return "OverflowBlock"
	}
}

// defaultMaxInFlight bounds claimed-but-unsettled messages when WithMaxInFlight
// is unset. It is finite (never unbounded — spec §7.4.1): the credit gate is
// always active.
const defaultMaxInFlight = 1024

// defaultAttemptTTL is how long an idle attempt-tracker entry survives before the
// clockwork sweep reclaims it (ADR 0008 D8). It vastly exceeds any in-process
// redelivery cadence, so an actively-redelivering id (re-observed each attempt)
// is never swept mid-flight (NF-2).
const defaultAttemptTTL = 5 * time.Minute

// defaultPollInterval is the idle wait between polls when a PollingSource's
// poll returns no messages, and WithPollInterval is unset. It paces the
// Poller so an empty backlog does not busy-loop the source.
const defaultPollInterval = 1 * time.Second

// defaultPollMaxBatch bounds the number of rows/messages fetched per poll
// when WithPollMaxBatch is unset. It keeps a single poll bounded so a large
// backlog is drained incrementally rather than in one unbounded fetch.
const defaultPollMaxBatch = 100

// maxPollErrorBackoff caps the Poller's backoff delay after consecutive
// polling errors, so a persistently failing source is retried at a bounded
// cadence rather than backing off unboundedly.
const maxPollErrorBackoff = 30 * time.Second

// WithMaxInFlight bounds claimed-but-unsettled messages to n (n >= 1). This is
// the load-bearing flood defense (spec §7.4.1); default defaultMaxInFlight.
// Setting the flag lets NewConsumer tell an explicit WithMaxInFlight(0) (a
// caller error → ErrInvalidMaxInFlight) apart from "unset" (→ default) (C2).
func WithMaxInFlight[T any](n int) ConsumerOption[T] {
	return func(o *consumerConfig[T]) { o.maxInFlight = n; o.maxInFlightSet = true }
}

// WithRateLimit paces ingress with r (spec §7.4.2). A nil r is a no-op.
func WithRateLimit[T any](r RateLimiter) ConsumerOption[T] {
	return func(o *consumerConfig[T]) {
		if r != nil {
			o.rateLimiter = r
		}
	}
}

// WithHandlerTimeout runs each handler under a clockwork-derived deadline d
// (spec §7.4.4). A stuck cooperative handler is cancelled → transient failure
// (retried), not a pinned worker. d <= 0 disables the timeout.
func WithHandlerTimeout[T any](d time.Duration) ConsumerOption[T] {
	return func(o *consumerConfig[T]) { o.handlerTimeout = d }
}

// WithCircuitBreaker gates ingress AND dispatch on b (spec §7.4.5). A nil b is a no-op.
func WithCircuitBreaker[T any](b CircuitBreaker) ConsumerOption[T] {
	return func(o *consumerConfig[T]) {
		if b != nil {
			o.breaker = b
		}
	}
}

// WithOverflow selects the ingress overflow policy for a push source (spec §7.4.6);
// default OverflowBlock.
func WithOverflow[T any](p OverflowPolicy) ConsumerOption[T] {
	return func(o *consumerConfig[T]) { o.overflow = p }
}

// WithAttemptTTL overrides how long an idle delivery-attempt-tracker entry
// survives before the periodic sweep reclaims it (default defaultAttemptTTL, 5m;
// ADR 0009 D3). The tracker counts redelivery attempts per message id for sources
// WITHOUT a native msgin.delivery-count header (e.g. memory); the sweep bounds the
// map so a stream of distinct one-shot ids cannot grow it without limit.
//
// d must be > 0, else NewConsumer returns ErrInvalidAttemptTTL.
//
// INVARIANT — set d comfortably ABOVE the worst-case redelivery round-trip,
// INCLUDING handler execution time — not merely your Backoff.Max. lastSeen is
// refreshed only after the handler returns, so the gap between two observes of a
// redelivering id spans re-inject + decode + handler + settle. A too-small TTL
// sweeps an id that is still being retried and restarts it at attempt 1, silently
// defeating RetryPolicy.MaxAttempts / dead-lettering. This is reachable TODAY on
// the memory adapter, which ignores the Nack backoff delay and redelivers at once:
// a Backoff of nil ("0") does NOT make a tiny TTL safe if the handler runs longer
// than that TTL. When in doubt, leave it at the default.
func WithAttemptTTL[T any](d time.Duration) ConsumerOption[T] {
	return func(o *consumerConfig[T]) { o.attemptTTL = d; o.attemptTTLSet = true }
}

// WithMaxPayloadBytes caps the size of an externally-sourced wire payload
// ([]byte) BEFORE it is handed to the PayloadCodec for decoding (ADR 0009 D5,
// spec §7 untrusted-input defense). A payload whose length exceeds n is settled
// as a PERMANENT invalid message (ErrPayloadTooLarge) — diverted to the
// invalid-message sink like a decode failure, never retried — since an over-size
// payload will not shrink on redelivery.
//
// n <= 0 disables the cap (the default): a library cannot guess a caller's
// legitimate maximum, so the cap is opt-in. Wire adapters consuming UNTRUSTED
// sources SHOULD set it to bound decode-time memory. The live-value (memory) path
// never carries []byte and is unaffected. Payload structural complexity (deep
// nesting) is bounded by the codec, not here — encoding/json returns an error on
// pathologically nested input rather than overflowing the stack.
func WithMaxPayloadBytes[T any](n int) ConsumerOption[T] {
	return func(o *consumerConfig[T]) { o.maxPayloadBytes = n }
}

// WithPollInterval sets how long the Poller idles after an EMPTY poll of a
// PollingSource before polling again (default defaultPollInterval, 1s). It
// has no effect on a StreamingSource. Setting the flag lets NewConsumer tell
// an explicit WithPollInterval(0) (a caller error → ErrInvalidPollInterval)
// apart from "unset" (→ default), mirroring WithAttemptTTL (C2).
//
// d must be > 0, else NewConsumer returns ErrInvalidPollInterval.
func WithPollInterval[T any](d time.Duration) ConsumerOption[T] {
	return func(o *consumerConfig[T]) { o.pollInterval = d; o.pollIntervalSet = true }
}

// WithPollMaxBatch caps the number of rows/messages the Poller fetches per
// poll of a PollingSource (default defaultPollMaxBatch, 100). It has no
// effect on a StreamingSource. Setting the flag lets NewConsumer tell an
// explicit WithPollMaxBatch(0) (a caller error → ErrInvalidPollMaxBatch)
// apart from "unset" (→ default), mirroring WithAttemptTTL (C2).
//
// n must be >= 1, else NewConsumer returns ErrInvalidPollMaxBatch.
func WithPollMaxBatch[T any](n int) ConsumerOption[T] {
	return func(o *consumerConfig[T]) { o.pollMaxBatch = n; o.pollMaxBatchSet = true }
}
