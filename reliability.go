package msgin

import "errors"

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
// msgin uses its own marker rather than cenkalti/backoff.Permanent so the core
// runtime stays stdlib + clockwork (see ADR 0007); cenkalti enters only via the
// outbound-HTTP adapter (ADR 0005).
func Permanent(err error) error {
	if err == nil {
		return nil
	}
	return &permanentError{err: err}
}

// isPermanent reports whether err must skip retry: an explicit Permanent marker,
// or a decode/type mismatch (ErrPayloadDecode / ErrPayloadType). A recovered
// handler panic (ErrHandlerPanic) is NOT permanent — it is retried.
func isPermanent(err error) bool {
	if err == nil {
		return false
	}
	var pe *permanentError
	if errors.As(err, &pe) {
		return true
	}
	return errors.Is(err, ErrPayloadType) || errors.Is(err, ErrPayloadDecode)
}

// noNativeReliability is the NativeReliability default for sources that do not
// implement the optional capability (e.g. memory): neither native redelivery
// nor native dead-letter. Using a value (never nil) upholds NF-11 — the runtime
// never nil-calls the capability.
type noNativeReliability struct{}

func (noNativeReliability) NativeRedelivery() bool { return false }
func (noNativeReliability) NativeDeadLetter() bool { return false }
