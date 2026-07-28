// Package msgin implements the messaging patterns of Gregor Hohpe and Bobby
// Woolf's Enterprise Integration Patterns (EIP) in idiomatic, minimal-dependency
// Go.
//
// # Pipes and Filters
//
// A flow is assembled as pipes and filters. A [MessageChannel] is the Pipe — the
// conduit a message travels along. A [Step] is the filter — one unit of
// processing. [Chain] assembles Steps into a pipeline. A [MessageHandler] is the
// endpoint where application code meets the messaging system.
//
// The linear endpoints — Transform (Message Translator), Filter, and
// Activate/Consume (Service Activator) — are Steps composed by Chain; a
// content-based Router branches to a MessageChannel. End a producing Chain with
// To(sink) or Consume, or its final message is discarded.
//
// A composed flow is a Handler[any], so NewConsumer[any](src, flow.Handle, …)
// drives it and it inherits retry, dead-letter, invalid-message, flow-control,
// and the worker pool. Typed endpoints assume the payload is the live Go value:
// this holds for live-value sources (memory); a WIRE source at T=any decodes to
// map[string]any, so decode to the concrete type in the first endpoint (a
// bytes-passthrough WithConsumerCodec[any] + Transform[[]byte, T]). Endpoint
// errors propagate into the runtime; a payload-type mismatch is [ErrPayloadType],
// routed to the invalid-message channel.
//
// # Package layout
//
// The root package holds the vocabulary and the SPI: messages, headers, the
// channel and handler interfaces, the adapter seams, and the error sentinels
// callers match with errors.Is. Implementations live in packages named for the
// EIP chapter that defines them:
//
//   - endpoint — Consumer, Producer, Gateway, ChannelExchange (EIP ch.10)
//   - routing — Filter, Router, Split, Aggregator (EIP ch.7)
//   - transform — Transform (EIP ch.8)
//   - channel — DirectChannel, QueueChannel, PublishSubscribeChannel, PubSub
//   - resilience — ExponentialBackoff, circuit breaker, token bucket
//
// Nothing in the root package imports one of these; no one of these imports
// another. Adapters and callers import them directly.
//
// # Publish-Subscribe
//
// Beyond point-to-point channels, a PublishSubscribeChannel fans a message out
// to every subscriber; Subscribe returns a [Subscription] whose Cancel
// unsubscribes. A PubSub registry maps a topic name to such a channel (created
// on first Subscribe, dropped when empty) and satisfies the
// [TopicPublisher]/[TopicSubscriber] SPI that native-topic broker adapters
// implement. Dispatch is synchronous (no goroutine); the default settlement is
// all-subscribers-succeed (a subscriber error is joined and the message
// retried), with WithFanOut(FanOutBestEffort) to log-and-continue.
//
// # Scheduled and delayed send
//
// An [OutboundAdapter] that can defer delivery implements the optional
// [ScheduledSender] capability; the sql adapter does so via its visible_after
// column. Producer.SendAfter(delay) is the skew-free relative primitive (the
// store computes now+delay) and SendAt(t) is absolute sugar over an injected
// clock; a sink that cannot schedule returns [ErrScheduledSendUnsupported]
// (never a silent immediate send). A negative or past delay delivers
// immediately. No goroutine is started — the delay lives in the durable row, so
// a scheduled send survives restarts and fires once across N competing
// consumers.
//
// # Messaging Gateway
//
// Gateway and OutboundGateway are the request-reply members of the composition
// set, both built over the shared [RequestReplyExchange] SPI. The inbound
// Gateway[Req, Rep] is the application-facing entry point: Request turns a Req
// into a message, drives it through the exchange, and returns the correlated
// Rep. OutboundGateway is the in-flow counterpart — a Step that performs the
// exchange mid-flow and forwards the reply to next, saving and restoring the
// message's HeaderCorrelationID around a fresh per-exchange id so an upstream
// split/aggregate group key survives the round-trip.
//
// # Runtime-defined rules
//
// Predicates and routing keys that must be defined at runtime — operator- or
// config-authored rules that change without a rebuild — are provided by the
// separate msgin/expr module, which supplies expression-backed counterparts to
// the Go-func forms. The Go-func forms remain the compile-time default: prefer
// them whenever the rule is known at build time, for their type safety and
// debuggability.
package msgin
