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
// [msgin.RequestReplyExchange] SPI. ChannelExchange correlates replies IN
// PROCESS: its pending-reply registry is an in-memory map, so a reply that
// arrives at a different instance is never matched. A distributed deployment
// needs the Return Address pattern implemented by an external
// RequestReplyExchange adapter (ADR 0022).
//
// Nothing here is imported by the root package; the dependency points inward
// only (endpoint imports msgin, never the reverse).
package endpoint
