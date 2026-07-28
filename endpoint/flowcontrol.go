package endpoint

import (
	"time"

	"github.com/kartaladev/msgin"
)

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
func WithRateLimit[T any](r msgin.RateLimiter) ConsumerOption[T] {
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
func WithCircuitBreaker[T any](b msgin.CircuitBreaker) ConsumerOption[T] {
	return func(o *consumerConfig[T]) {
		if b != nil {
			o.breaker = b
		}
	}
}

// WithOverflow selects the ingress overflow policy for a push source (spec §7.4.6);
// default OverflowBlock.
func WithOverflow[T any](p msgin.OverflowPolicy) ConsumerOption[T] {
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
