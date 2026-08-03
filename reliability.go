package msgin

import (
	"errors"
	"time"
)

// permanentError marks a handler error as non-retryable: the runtime diverts it
// to the invalid-message sink instead of retrying — see [Permanent] for that
// destination's three arms. Wrapping is transparent to errors.Is/As via Unwrap.
type permanentError struct{ err error }

func (e *permanentError) Error() string { return "msgin: permanent: " + e.err.Error() }
func (e *permanentError) Unwrap() error { return e.err }

// Permanent marks err as permanent (non-retryable). A handler that returns
// Permanent(err) diverts the message without consuming retry attempts.
// Permanent(nil) returns nil.
//
// THE DESTINATION, canonically — every other "routed to the invalid-message
// sink" statement in msgin is shorthand for these three arms (ADR 0007 D7 as
// amended by decisions D-N and D-P):
//
//  1. the consumer's invalid-message sink, when one is configured
//     (endpoint.WithInvalidMessageSink);
//  2. otherwise the RetryPolicy's DeadLetter sink, when one is configured — so
//     a fault this library used to capture durably is never downgraded to a
//     discard;
//  3. otherwise it is logged at WARN and discarded.
//
// The divert is SINGLE-SHOT on ALL of them: one attempt at one sink. If that
// Send fails — arm 1's configured sink and arm 2's fallback alike — the message
// is logged at WARN (naming the classification cause and the sink error) and
// discarded, never requeued, because requeueing a permanent message is an
// unbounded loop no retry budget, backoff or circuit breaker can observe. The
// accepted cost is that a transient outage of a configured invalid-message sink
// discards that window's invalid messages; see endpoint.WithInvalidMessageSink.
// The DEAD-LETTER path is the opposite: a retry-exhausted message IS transient,
// so a failed dead-letter Send is Nacked with backoff, not discarded. The one
// exception to "never requeued" is SHUTDOWN: a Send that fails only because the
// consumer's settle context was already cancelled by the shutdown deadline
// proved nothing about the sink, so the message is Nacked for redelivery and no
// terminal event is reported.
//
// KEEP PAYLOAD AND PII OUT OF err. That WARN renders the cause VERBATIM, so
// Permanent(fmt.Errorf("invalid email %q", m.Payload().Email)) — an ordinary
// validation shape — writes the email to the log. msgin redacts exactly one
// class, the payload-decode one (ErrPayloadDecode, whose text comes from a
// caller-supplied codec quoting untrusted wire bytes); every other cause,
// including this one, is logged as written. Sensitive detail belongs behind the
// OnInvalidMessage hook, which always receives the UNREDACTED cause under the
// caller's own disclosure policy. See endpoint.WithInvalidMessageSink.
//
// OnInvalidMessage fires on all three (it reports the CLASSIFICATION, not the
// destination); OnRetry and OnDeadLetter fire on none of them. An operator who
// must tell retries-exhausted from permanently-invalid in a shared dead-letter
// store has to configure endpoint.WithInvalidMessageSink: msgin stamps no
// settlement-reason header.
//
// msgin uses its own marker rather than a third-party backoff library's
// Permanent so the core runtime stays stdlib + clockwork (ADR 0007). ADR 0005
// once reserved cenkalti/backoff/v4 for an adapter-side outbound-HTTP retry
// loop; that clause is SUPERSEDED by ADR 0025 — outbound retry is producer-side
// and reuses RetryPolicy (see WithProducerRetry), and cenkalti/backoff is not a
// dependency of this module.
func Permanent(err error) error {
	if err == nil {
		return nil
	}
	return &permanentError{err: err}
}

// IsPermanent reports whether err must skip retry: an explicit Permanent marker,
// a decode/type mismatch (ErrPayloadDecode / ErrPayloadType), or an over-size
// payload (ErrPayloadTooLarge — an over-size message will not shrink on
// redelivery). A recovered handler panic (ErrHandlerPanic) is NOT permanent — it
// is retried.
func IsPermanent(err error) bool {
	if err == nil {
		return false
	}
	var pe *permanentError
	if errors.As(err, &pe) {
		return true
	}
	return errors.Is(err, ErrPayloadType) ||
		errors.Is(err, ErrPayloadDecode) ||
		errors.Is(err, ErrPayloadTooLarge)
}

// retryAfterError marks a transient error with a server-instructed minimum
// delay. Wrapping is transparent to errors.Is/As via Unwrap.
type retryAfterError struct {
	err error
	d   time.Duration
}

func (e *retryAfterError) Error() string {
	return "msgin: retry after " + e.d.String() + ": " + e.err.Error()
}

func (e *retryAfterError) Unwrap() error { return e.err }

// RetryAfter marks err as transient with a server-provided MINIMUM delay: a
// producer configured with WithProducerRetry waits at least d before the next
// attempt. It is how an adapter relays an explicit server instruction (an HTTP
// Retry-After header on a 429 or 503) that a BackoffStrategy, being stateless
// and closed-form, cannot express.
//
// d is a FLOOR, not an override (RFC 9110 §10.2.3: Retry-After is the minimum
// time the client should wait). The effective wait is
//
//	max(policyBackoff, min(d, WithProducerRetryAfterCap))
//
// and is always additionally bounded by ctx and by WithProducerRetryBudget. A
// server therefore cannot SHORTEN the client's own backoff — including to zero,
// which would be a remote-triggerable hot spin — it can only lengthen it, up to
// the cap.
//
// It mirrors Permanent: same wrapper shape, same Unwrap transparency, same nil
// handling. RetryAfter(nil, d) returns nil. A negative d is normalized to 0
// (meaning "no server-instructed floor") rather than rejected, so a skewed or
// already-elapsed server deadline degrades to the computed backoff instead of
// an error.
//
// Permanent WINS over RetryAfter when both markers are present, in either
// nesting order: permanent means do not retry, so a delay is meaningless.
//
// A RetryAfter marker on an error returned to a Producer WITHOUT
// WithProducerRetry is inert — there is no retry loop to honour it.
func RetryAfter(err error, d time.Duration) error {
	if err == nil {
		return nil
	}
	if d < 0 {
		d = 0
	}
	return &retryAfterError{err: err, d: d}
}

// RetryAfterOf reports the server-instructed minimum delay carried by err, if
// any, matching IsPermanent's structure (errors.As over the wrap chain).
//
// Deliberately NO `if err == nil` guard: errors.As(nil, &re) already returns
// false, and the only caller (nextDelay) never passes nil, so the guard would be
// both redundant and blackbox-unreachable.
func RetryAfterOf(err error) (time.Duration, bool) {
	var re *retryAfterError
	if errors.As(err, &re) {
		return re.d, true
	}
	return 0, false
}
