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
package msgin
