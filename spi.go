package msgin

import (
	"context"
	"time"
)

// Delivery is one received message plus the means to settle it. Msg.Payload is
// []byte for wire adapters or a live value for the in-memory adapter.
type Delivery struct {
	Msg  Message[any]
	Ack  func(ctx context.Context) error
	Nack func(ctx context.Context, requeue bool, delay time.Duration) error
}

// PollingSource is a pulled inbound adapter, driven by the runtime's Poller.
//
// Poll fetches up to max claimable deliveries. The runtime acquires max credits
// BEFORE calling Poll (credit-at-fetch, spec §7.4.1), so it never over-pulls past
// WithMaxInFlight. An implementation MUST honor three invariants — the runtime
// enforces the first defensively and relies on the other two:
//
//  1. Return AT MOST max deliveries. Returning more corrupts the credit
//     accounting; the runtime defensively clamps and Nacks the excess, but a
//     correct source never returns more than asked.
//  2. Return NO deliveries alongside a non-nil error. A partial (rows, err) is
//     forbidden: the runtime discards rows on error, which for a lock/tx-carrying
//     Delivery would leak the transaction/connection it holds.
//  3. Own the cleanup (rollback) of any partial or claimed work on the error or
//     ctx-cancel path, returning (nil, err) — a claimed-but-not-returned row must
//     be released back to the source so it is redelivered, not lost.
type PollingSource interface {
	Poll(ctx context.Context, max int) ([]Delivery, error)
}

// EventDrivenSource is a pushed inbound adapter that owns a blocking,
// cancellable loop. The name follows EIP's Event-Driven Consumer (Spring
// Integration: EventDrivenConsumer); the Stream method keeps its name because it
// describes the mechanism and has no Spring counterpart to align with
// (ADR 0029 §1).
type EventDrivenSource interface {
	Stream(ctx context.Context, out chan<- Delivery) error
}

// OutboundAdapter writes a message to the external system — the EIP Channel
// Adapter at a system boundary (EIP ch.4).
//
// METHOD-IDENTICAL TO [MessageChannel] BY DESIGN. Since ADR 0028 narrowed
// MessageChannel to Send, the two interfaces declare the same single method and
// are interchangeable under Go's structural typing. Both are kept because they
// name two different patterns: MessageChannel is the Pipe of Pipes and Filters
// (EIP ch.3), OutboundAdapter is the Channel Adapter here. A consequence worth
// stating plainly: every shipped OutboundAdapter is now also a legal filter
// discard target, router default channel, router destination, and exchange
// request channel.
type OutboundAdapter interface {
	Send(ctx context.Context, msg Message[any]) error
}

// NativeReliability is an optional capability: two independent booleans (ADR 0002).
type NativeReliability interface {
	NativeRedelivery() bool
	NativeDeadLetter() bool
}

// LiveValueSource is an optional capability: a source emitting live Go values
// (in-memory) rather than []byte, so NewConsumer can enforce codec pairing.
type LiveValueSource interface {
	EmitsLiveValue() bool
}

// ScheduledSender is an optional capability of an OutboundAdapter: it delivers a
// message so that it becomes consumable only after a delay elapses (durable
// delayed send). An adapter that can defer delivery — e.g. the database/sql
// adapter via its visible_after column — implements it; the producer discovers
// it by type-assertion and returns ErrScheduledSendUnsupported when the sink
// does not.
//
// The capability carries the RELATIVE primitive only: the delivery time is
// computed by the adapter's own store as now+delay, so it is free of app-vs-store
// clock skew. A delay <= 0 delivers immediately (equivalent to Send). Absolute-time
// scheduling is a producer-side concern (Producer.SendAt), never pushed into the
// adapter.
type ScheduledSender interface {
	SendAfter(ctx context.Context, msg Message[any], delay time.Duration) error
}

// TopicPublisher publishes a message to a named topic. Native-topic broker
// adapters (Kafka, NATS, Redis) implement this using their own topics, so topic
// support is handled generically through one SPI.
type TopicPublisher interface {
	Publish(ctx context.Context, topic string, msg Message[any]) error
}

// TopicSubscriber subscribes a handler to a named topic, returning a Subscription
// whose Cancel unsubscribes. The counterpart SPI to TopicPublisher (split per the
// interface-segregation principle: a publish-only or subscribe-only adapter is
// legitimate).
type TopicSubscriber interface {
	Subscribe(topic string, h MessageHandler) (Subscription, error)
}

// RequestReplyExchange is the narrow SPI a gateway delegates to: it sends a
// request and returns the correlated reply (or an error). ChannelExchange is the
// in-process implementation; a future HTTP/NATS adapter implements Exchange for
// a real external round-trip, so both gateway façades work over it unchanged.
//
// Contract: an implementation MUST release every piece of request-scoped state
// it acquires — a correlator entry, an in-flight connection, a response body —
// on EVERY exit path, including a panic unwinding out of a downstream call.
// Deferred cleanup is the only reliable way to honour this; an implementation
// that cleans up at each return site alone will leak whenever a caller-supplied
// handler panics. An implementation MUST NOT recover such a panic into an error
// return: the panic belongs to the caller's code and must propagate with its
// original value and stack (ADR 0022 Addendum A3; Spec 012) — unless a
// CALLER-SUPPLIED hook on the cleanup path itself panics, which replaces the
// original (see WithUnmatchedReplySink and WithExchangeLogger).
type RequestReplyExchange interface {
	Exchange(ctx context.Context, req Message[any]) (Message[any], error)
}
