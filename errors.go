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
	// ErrInvalidMaxInFlight is returned when WithMaxInFlight is given n < 1.
	ErrInvalidMaxInFlight = errors.New("msgin: max in-flight must be >= 1")
	// ErrInvalidRateLimit is returned when a rate-limit configuration is invalid
	// (non-positive rps or burst on the default token bucket).
	ErrInvalidRateLimit = errors.New("msgin: rate limit must have positive rps and burst")
	// ErrOverflowDropped is the cause carried to OnRetry/logs when an overflow
	// policy sheds a message; it is not returned from any exported function.
	ErrOverflowDropped = errors.New("msgin: message dropped by overflow policy")
)
