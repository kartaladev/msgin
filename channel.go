package msgin

import (
	"context"
)

// MessageChannel is the EIP **Pipe**: the conduit an endpoint sends into. It is
// send-only — subscribing is the separate [SubscribableChannel] contract — so
// every channel implementation in the library satisfies it: a DirectChannel, a
// pollable QueueChannel, a PublishSubscribeChannel, and any OutboundAdapter.
//
// METHOD-IDENTICAL TO [OutboundAdapter] BY DESIGN. The two interfaces declare
// the same single method and are freely interchangeable under Go's structural
// typing, yet both are kept because they name two different EIP patterns:
// MessageChannel is the Pipe of Pipes and Filters (EIP ch.3), the concept this
// library's composition model is built on, while OutboundAdapter is a Channel
// Adapter at a system boundary (EIP ch.4). Collapsing them would erase the Pipe
// from the type system of a pipes-and-filters library. Use MessageChannel for a
// destination inside a flow, OutboundAdapter for the edge of one (ADR 0028 §5).
type MessageChannel interface {
	Send(ctx context.Context, msg Message[any]) error
}

// SubscribableChannel is a MessageChannel that also accepts a push subscriber,
// returning a [Subscription] whose Cancel unsubscribes. DirectChannel (one
// subscriber) and PublishSubscribeChannel (fan-out) implement it; a pollable
// QueueChannel deliberately does not — it is drained through PollingSource.
//
// TOPOLOGY — IN-PROCESS ONLY. A subscriber is a Go func in this process's
// memory, so nothing reached through this interface crosses a process boundary:
// under N horizontally-scaled instances, a message published on instance A is
// never seen by a subscriber registered on instance B. That is a deliberate
// scope, not an oversight. The distributed answers are the Return Address
// pattern for request-reply (carry the reply destination in the message, so the
// reply is routed rather than assumed local — see [RequestReplyExchange], whose
// seam is kept in root precisely so an external adapter can implement it) and a
// broker's own topics/consumer groups for fan-out (see [TopicSubscriber]).
type SubscribableChannel interface {
	MessageChannel
	Subscribe(h MessageHandler) (Subscription, error)
}

// Subscription is a handle to an active subscription. Cancel removes the
// subscriber; it is idempotent (a second Cancel is a safe no-op).
//
// It lives here, not in the channel package, because it is the return type of
// SubscribableChannel.Subscribe and of the root-level TopicSubscriber SPI:
// moving it would make root import channel (Spec 014 decision D-C).
type Subscription interface{ Cancel() }
