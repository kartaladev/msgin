package msgin

import (
	"errors"
	"time"
)

// permanentError marks a handler error as non-retryable: the runtime routes it
// straight to the invalid-message sink instead of retrying. Wrapping is
// transparent to errors.Is/As via Unwrap.
type permanentError struct{ err error }

func (e *permanentError) Error() string { return "msgin: permanent: " + e.err.Error() }
func (e *permanentError) Unwrap() error { return e.err }

// Permanent marks err as permanent (non-retryable). A handler that returns
// Permanent(err) sends the message to the invalid-message sink without
// consuming retry attempts. Permanent(nil) returns nil.
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

// isPermanent reports whether err must skip retry: an explicit Permanent marker,
// a decode/type mismatch (ErrPayloadDecode / ErrPayloadType), or an over-size
// payload (ErrPayloadTooLarge — an over-size message will not shrink on
// redelivery). A recovered handler panic (ErrHandlerPanic) is NOT permanent — it
// is retried.
func isPermanent(err error) bool {
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

// noNativeReliability is the NativeReliability default for sources that do not
// implement the optional capability (e.g. memory): neither native redelivery
// nor native dead-letter. Using a value (never nil) upholds NF-11 — the runtime
// never nil-calls the capability.
type noNativeReliability struct{}

func (noNativeReliability) NativeRedelivery() bool { return false }
func (noNativeReliability) NativeDeadLetter() bool { return false }

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

// retryAfterOf reports the server-instructed minimum delay carried by err, if
// any, matching isPermanent's structure (errors.As over the wrap chain).
//
// Deliberately NO `if err == nil` guard: errors.As(nil, &re) already returns
// false, and the only caller (nextDelay) never passes nil, so the guard would be
// both redundant and blackbox-unreachable.
func retryAfterOf(err error) (time.Duration, bool) {
	var re *retryAfterError
	if errors.As(err, &re) {
		return re.d, true
	}
	return 0, false
}
