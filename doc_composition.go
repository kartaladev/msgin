// Package msgin — in-process composition (Spec 003 / ADR 0013).
//
// Beyond adapters, msgin composes an in-process message flow from small
// endpoints wired as pipes and filters. A MessageHandler is one step; a
// MessageChannel is the conduit. The linear endpoints — Transform (Message
// Translator), Filter, and Activate/Consume (Service Activator) — are Steps
// composed by Chain; a content-based Router branches to a MessageChannel. End a
// producing Chain with To(sink) or Consume, or its final message is discarded.
//
// A composed flow is a Handler[any], so NewConsumer[any](src, flow.Handle, …)
// drives it and it inherits retry, dead-letter, invalid-message, flow-control,
// and the worker pool. Typed endpoints assume the payload is the live Go value:
// this holds for live-value sources (memory); a WIRE source at T=any decodes to
// map[string]any, so decode to the concrete type in the first endpoint (a
// bytes-passthrough WithConsumerCodec[any] + Transform[[]byte, T]). Endpoint
// errors propagate into the runtime; a payload-type mismatch is ErrPayloadType,
// routed to the invalid-message channel.
//
// Publish-Subscribe (Spec 004 / ADR 0014). Beyond point-to-point channels, a
// PublishSubscribeChannel fans a message out to every subscriber; Subscribe
// returns a Subscription whose Cancel unsubscribes. A PubSub registry maps a
// topic name to such a channel (created on first Subscribe, dropped when empty)
// and satisfies the TopicPublisher/TopicSubscriber SPI that native-topic broker
// adapters implement. Dispatch is synchronous (no goroutine); the default
// settlement is all-subscribers-succeed (a subscriber error is joined and the
// message retried), with WithFanOut(FanOutBestEffort) to log-and-continue.
package msgin
