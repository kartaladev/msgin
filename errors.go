package msgin

import "errors"

var (
	// ErrPayloadType is returned when a Message[any] payload cannot be asserted to T.
	ErrPayloadType = errors.New("msgin: payload is not of the expected type")
	// ErrPayloadDecode is returned when a wire payload ([]byte) cannot be decoded into T.
	ErrPayloadDecode = errors.New("msgin: payload decode failed")
	// ErrNilAdapter is returned by constructors when a required adapter is nil.
	ErrNilAdapter = errors.New("msgin: adapter is nil")
	// ErrPayloadTooLarge is returned when a wire payload ([]byte) exceeds the
	// WithMaxPayloadBytes cap. It is PERMANENT (an over-size payload will not
	// shrink on redelivery), so the message is diverted to the invalid sink like
	// a decode failure rather than retried (ADR 0009 D5).
	ErrPayloadTooLarge = errors.New("msgin: payload exceeds the configured maximum size")
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
	// ErrInvalidAttemptTTL is returned when WithAttemptTTL is given a non-positive
	// duration (ADR 0009 D3).
	ErrInvalidAttemptTTL = errors.New("msgin: attempt TTL must be > 0")
	// ErrOverflowDropped is the cause carried to OnRetry/logs when an overflow
	// policy sheds a message; it is not returned from any exported function.
	ErrOverflowDropped = errors.New("msgin: message dropped by overflow policy")
	// ErrInvalidPollInterval is returned when WithPollInterval is given a
	// non-positive duration.
	ErrInvalidPollInterval = errors.New("msgin: poll interval must be > 0")
	// ErrInvalidPollMaxBatch is returned when WithPollMaxBatch is given n < 1.
	ErrInvalidPollMaxBatch = errors.New("msgin: poll max batch must be >= 1")
	// ErrChannelSubscribed is returned by a point-to-point channel's Subscribe
	// when a handler is already registered (single-consumer invariant).
	ErrChannelSubscribed = errors.New("msgin: channel already has a subscriber")
	// ErrNoSubscriber is returned by a point-to-point channel's Send when no
	// handler is subscribed — a message is never silently dropped.
	ErrNoSubscriber = errors.New("msgin: channel has no subscriber")
	// ErrNilHandler is returned when a nil MessageHandler is subscribed.
	ErrNilHandler = errors.New("msgin: nil message handler")
	// ErrNilSink is returned by To when its OutboundAdapter sink is nil.
	ErrNilSink = errors.New("msgin: nil outbound sink")
	// ErrNilFunc is returned by an endpoint (Transform/Filter/Activate/Consume/
	// Router) constructed with a nil function, instead of panicking at dispatch.
	ErrNilFunc = errors.New("msgin: nil endpoint function")
	// ErrNoRoute is returned by a Router when pick resolves no destination and no
	// WithDefaultChannel is configured (Spring resolutionRequired=true).
	ErrNoRoute = errors.New("msgin: no route for message")
	// ErrScheduledSendUnsupported is returned by Producer.SendAfter/SendAt when the
	// underlying OutboundAdapter does not implement ScheduledSender (it cannot defer
	// delivery). It is never a silent immediate send — the caller is told the sink
	// cannot schedule. It is errors.Is-able.
	ErrScheduledSendUnsupported = errors.New("msgin: outbound adapter does not support scheduled send")
)
