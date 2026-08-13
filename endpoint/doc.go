// Package endpoint implements the Message Endpoint patterns of Enterprise
// Integration Patterns chapter 10 — the boundary where application code meets
// the messaging system. It is the msgin counterpart of Spring Integration's
// org.springframework.integration.endpoint.
//
// [Consumer] is the driving runtime: it pulls from a [msgin.PollingSource] or
// [msgin.EventDrivenSource], decodes the payload, dispatches to a [Handler], and
// settles each [msgin.Delivery] — carrying retry, dead-lettering,
// invalid-message diversion, flow control, and the worker pool. [Producer] is
// the outbound side, including the Scheduled/Delayed send capability.
// [Activate] and [Consume] are the Service Activator (EIP ch.10) endpoints,
// expressed as [msgin.Step] so [msgin.Chain] can compose them.
//
// [Gateway], [OutboundGateway], and [ChannelExchange] are the Messaging Gateway
// (EIP ch.10) request-reply members, all built over the root
// [msgin.RequestReplyExchange] SPI. The pattern they implement in process is
// Correlation Identifier (EIP ch.5): [Gateway] and [OutboundGateway] stamp a
// fresh [msgin.HeaderCorrelationID] on every outgoing request, and
// ChannelExchange matches each inbound reply back to its waiting caller by that
// id alone — never by arrival order, and never by the reply's own message id.
// That fresh id keys the round-trip; it is not a value that leaks onward.
// [OutboundGateway] restores the caller's pre-existing correlation id on the
// reply it forwards, and strips the minted one entirely when the request
// arrived carrying none.
// ChannelExchange correlates replies IN
// PROCESS: its pending-reply registry is an in-memory map, so a reply that
// arrives at a different instance is never matched. A distributed deployment
// needs the Return Address pattern implemented by an external
// RequestReplyExchange adapter (ADR 0022) — Correlation Identifier still
// identifies WHICH request a reply answers; Return Address is what tells a
// remote responder WHERE to send it.
//
// Nothing here is imported by the root package; the dependency points inward
// only (endpoint imports msgin, never the reverse).
package endpoint
