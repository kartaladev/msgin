package endpoint

import (
	"time"

	"github.com/kartaladev/msgin"
)

// defaultMaxInFlight bounds claimed-but-unsettled messages when WithMaxInFlight
// is unset. It is finite (never unbounded — spec §7.4.1): the credit gate is
// always active.
const defaultMaxInFlight = 1024

// maxInFlightCeiling is the upper bound WithMaxInFlight accepts (Spec 016 §3.4).
// It is 1024x defaultMaxInFlight: at the ceiling, sizing workerCh's backing
// array (managedDelivery is 48 B) costs 1<<20 * 48 = 50,331,648 B (48 MiB) —
// four orders of magnitude below the platform's unrecoverable-OOM band, so the
// ceiling demonstrably prevents the crash a caller-supplied huge n would
// otherwise cause (ADR 0032).
const maxInFlightCeiling = 1 << 20

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

// WithMaxInFlight bounds claimed-but-unsettled messages to n, which must be in
// [1, maxInFlightCeiling] (1,048,576) — an n outside that range is a
// construction-time error (msgin.ErrInvalidMaxInFlight), not a silent clamp.
// This is the load-bearing flood defense (spec §7.4.1); default
// defaultMaxInFlight (1024). The ceiling exists because n directly sizes the
// worker channel's backing array: at 1<<20 that costs ~48 MiB (Spec 016 §3.4)
// — comfortably affordable, and 1024x the default, but far below the point at
// which a mistyped huge n would exhaust process memory. Setting the flag lets
// NewConsumer tell an explicit WithMaxInFlight(0) (a caller error →
// ErrInvalidMaxInFlight) apart from "unset" (→ default) (C2).
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
// as a PERMANENT invalid message (ErrPayloadTooLarge) — diverted like a decode
// failure, never retried — since an over-size payload will not shrink on
// redelivery.
//
// THIS CLASS IS THE ONE EXEMPTION TO THE D-N DEAD-LETTER FALLBACK, and the
// exemption is the whole reason the cap is safe to turn on:
//
//   - WithInvalidMessageSink SET → the message goes there, like every other
//     invalid message. The operator asked for the bytes; they get them.
//   - UNSET → the message is DISCARDED, loudly (a WARN naming the message id and
//     the class, plus OnInvalidMessage, then an Ack). It does NOT fall back to
//     RetryPolicy.DeadLetter the way other invalid messages do.
//
// Without that exemption the cap would defeat itself: "DeadLetter set, invalid
// sink unset" is the default shape of every finite-retry consumer, so an
// attacker posting over-size bodies would have each rejected payload written
// VERBATIM into the operator's durable dead-letter store — the defence becoming
// the vector. Discarding is not a regression against the fallback either: this
// class was already permanent before D-N and was already discarded under ADR
// 0007 D7, so the exemption RESTORES that behavior rather than dropping
// something D-N had captured.
//
// If you need the rejected bytes for forensics, set WithInvalidMessageSink to
// something cheap and bounded, or reject over-size input at the adapter before
// it becomes a Delivery.
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
// has no effect on a EventDrivenSource. Setting the flag lets NewConsumer tell
// an explicit WithPollInterval(0) (a caller error → ErrInvalidPollInterval)
// apart from "unset" (→ default), mirroring WithAttemptTTL (C2).
//
// d must be > 0, else NewConsumer returns ErrInvalidPollInterval.
func WithPollInterval[T any](d time.Duration) ConsumerOption[T] {
	return func(o *consumerConfig[T]) { o.pollInterval = d; o.pollIntervalSet = true }
}

// WithPollMaxBatch caps the number of rows/messages the Poller fetches per
// poll of a PollingSource (default defaultPollMaxBatch, 100). It has no
// effect on a EventDrivenSource. Setting the flag lets NewConsumer tell an
// explicit WithPollMaxBatch(0) (a caller error → ErrInvalidPollMaxBatch)
// apart from "unset" (→ default), mirroring WithAttemptTTL (C2).
//
// n must be >= 1, else NewConsumer returns ErrInvalidPollMaxBatch.
func WithPollMaxBatch[T any](n int) ConsumerOption[T] {
	return func(o *consumerConfig[T]) { o.pollMaxBatch = n; o.pollMaxBatchSet = true }
}
