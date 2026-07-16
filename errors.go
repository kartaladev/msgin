package msgin

import "errors"

var (
	// ErrPayloadType is returned when a Message[any] payload cannot be asserted to T.
	ErrPayloadType = errors.New("msgin: payload is not of the expected type")
	// ErrPayloadDecode is returned when a wire payload ([]byte) cannot be decoded into T.
	ErrPayloadDecode = errors.New("msgin: payload decode failed")
	// ErrNilAdapter is returned by constructors when a required adapter is nil.
	ErrNilAdapter = errors.New("msgin: adapter is nil")
	// ErrNoPayloadCodec is returned when a wire source is used without a PayloadCodec.
	ErrNoPayloadCodec = errors.New("msgin: wire source requires a payload codec")
	// ErrUnexpectedCodec is returned when a live-value source (memory) is given a codec.
	ErrUnexpectedCodec = errors.New("msgin: live-value source must not have a payload codec")
	// ErrInvalidConcurrency is returned when WithConcurrency is < 1.
	ErrInvalidConcurrency = errors.New("msgin: concurrency must be >= 1")
	// ErrUnsupportedSource is returned when a Source is neither Polling nor Streaming.
	ErrUnsupportedSource = errors.New("msgin: source implements neither PollingSource nor StreamingSource")
	// ErrHandlerPanic wraps a value recovered from a panicking handler. It is a
	// TRANSIENT failure (retried per the RetryPolicy), not permanent.
	ErrHandlerPanic = errors.New("msgin: handler panicked")
	// ErrNoDeadLetter is returned when a RetryPolicy has a finite MaxAttempts
	// (> 0) but no DeadLetter sink to receive exhausted messages.
	ErrNoDeadLetter = errors.New("msgin: finite MaxAttempts requires a DeadLetter sink")
	// ErrInvalidMaxAttempts is returned when a RetryPolicy's MaxAttempts is negative.
	ErrInvalidMaxAttempts = errors.New("msgin: MaxAttempts must be >= 0")
)
