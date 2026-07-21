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
	// ErrOverflowDropped is the cause carried to OnRetry/logs when a streaming
	// source's overflow policy sheds a message, AND is returned by a bounded
	// ChannelStore.Enqueue (e.g. memory.QueueStore) when OverflowReject rejects a
	// full-buffer Send. It is NOT returned for the silent Drop* policies.
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
	// ErrNilStore is returned by NewQueueChannel when the ChannelStore is nil.
	ErrNilStore = errors.New("msgin: channel store is nil")
	// ErrInvalidCapacity is returned by a bounded store constructor (e.g.
	// memory.NewQueueStore) when an explicit capacity is <= 0.
	ErrInvalidCapacity = errors.New("msgin: capacity must be > 0")
	// ErrInvalidExpression is returned by FilterExpr/RouterExpr when an
	// expression is empty, unparseable, or fails type-checking against the
	// payload type at construction. The wrapped error names the offending
	// expression. Runtime evaluation errors are NOT this — they propagate as the
	// endpoint's handler error into the runtime's retry/DLQ path.
	ErrInvalidExpression = errors.New("msgin: invalid expression")
	// ErrNoCorrelation is returned when an Aggregator's correlation strategy
	// yields no key for a message. It is always wrapped with Permanent (a
	// missing correlation id will not appear on redelivery), so the runtime
	// routes it to the invalid-message channel rather than retrying.
	ErrNoCorrelation = errors.New("msgin: message has no correlation key")
	// ErrNilOutput is returned by NewAggregator when no output channel is set
	// (WithOutputChannel is required).
	ErrNilOutput = errors.New("msgin: aggregator output channel is nil")
	// ErrExpiryChannelRequired is returned by NewAggregator when
	// WithGroupTimeout is set without WithExpiredGroupChannel — an expired
	// partial group must have a visible sink, never a silent drop.
	ErrExpiryChannelRequired = errors.New("msgin: group timeout requires an expired-group channel")
	// ErrExprResultType is returned (as the endpoint's handler error) by
	// TransformExpr/SplitExpr when a compiled expression evaluates to a value that
	// is not the asserted output type — a non-B TransformExpr result, or a
	// non-slice SplitExpr result / non-B SplitExpr element. It is an EVALUATION
	// (not construction) error: ErrInvalidExpression covers compile-time faults,
	// this covers a well-typed-at-compile expression whose runtime value is the
	// wrong Go type (possible when A/B is an interface, so expr cannot type-check
	// the result). It propagates into the runtime's retry/DLQ path like any eval
	// error.
	ErrExprResultType = errors.New("msgin: expression result type mismatch")

	// ErrGatewayClosed is returned by ChannelExchange.Exchange (and any Gateway
	// built on it) once Close has been called: new exchanges are rejected and
	// any waiter pending at Close time fails with it. Part of graceful shutdown.
	ErrGatewayClosed = errors.New("msgin: request-reply exchange is closed")

	// ErrReplyTimeout is returned by Exchange when no correlated reply arrives
	// before the effective deadline (min of the caller ctx deadline and the
	// configured reply timeout, default 30s). Distinct from ctx cancellation,
	// which returns the ctx error.
	ErrReplyTimeout = errors.New("msgin: timed out awaiting reply")

	// ErrNilExchange is returned by NewGateway when the RequestReplyExchange is
	// nil. adapter/http/stdlib.NewInboundGateway (the I2 sync inbound gateway
	// constructor) is a second legitimate caller: it returns this same
	// sentinel when its exchange argument is nil.
	ErrNilExchange = errors.New("msgin: request-reply exchange is nil")

	// ErrNilChannel is returned by NewChannelExchange when the request or reply
	// MessageChannel is nil.
	ErrNilChannel = errors.New("msgin: request or reply channel is nil")

	// ErrInvalidReplyTimeout is returned by NewChannelExchange when an explicit
	// WithReplyTimeout is set to a non-positive duration. An unset timeout takes
	// the 30s default and never yields this error.
	ErrInvalidReplyTimeout = errors.New("msgin: reply timeout must be > 0")

	// ErrDuplicateCorrelation is returned by Exchange when the request's
	// HeaderCorrelationID already has an in-flight request registered on the same
	// exchange (audit G1). Correlation ids must be unique per concurrent
	// in-flight request; the gateway façades mint a fresh id so they never trigger
	// this — it guards direct ChannelExchange users who set the header by hand.
	ErrDuplicateCorrelation = errors.New("msgin: correlation id already has an in-flight request")
)
