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
// CONTRACT FOR IMPLEMENTERS. Subscribe must return either a non-nil Subscription
// and a nil error, or a nil Subscription and a non-nil error — never a nil-nil
// pair, which hands the caller a subscription it cannot Cancel. Callers that
// retain the Subscription reject a nil-nil return with [ErrNilSubscription].
//
// OPTIONAL CAPABILITY — [ExclusiveSubscribable]. An implementation that can
// report whether every message sent to it reaches at most one recipient should
// also declare SingleSubscriber. endpoint.NewChannelExchange probes for it and
// rejects a channel that reports non-exclusive with [ErrSharedReplyChannel]; a
// channel that does not implement it is accepted unprobed, so a fan-out channel
// that stays silent here is silently accepted as an exchange reply channel.
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

// ExclusiveSubscribable is the optional capability a SubscribableChannel
// implements to report whether every message sent to it reaches at most one
// recipient.
//
// It is OPTIONAL in the established sense of [NativeReliability] /
// [ScheduledSender] / [LiveValueSource]: endpoint.NewChannelExchange
// type-asserts for it and accepts a channel that does not implement it. Both
// in-tree channels do — channel.DirectChannel always reports true, and
// channel.PublishSubscribeChannel reports whether channel.WithSingleSubscriber()
// was passed, which is how a pub-sub reply channel becomes usable as an
// exchange's dedicated reply channel (ADR 0030).
type ExclusiveSubscribable interface {
	SubscribableChannel
	// SingleSubscriber reports whether every message sent to this channel
	// reaches at most one recipient, counted across every process. It is a
	// statement about the channel's POLICY, not its current subscriber count:
	// an implementation MUST NOT compute it from a live subscriber count.
	//
	// A channel MUST return false whenever a message sent to it can be received by
	// any recipient other than the single subscriber registered on it — INCLUDING a
	// recipient in another process. A broadcast broker subject, a Redis pub/sub
	// channel, or an SSE stream fanned out to N instances MUST therefore return
	// false even when its local handle admits one subscriber. A broker-backed
	// channel MAY return true only when the broker guarantees the destination is
	// private to this process's subscription — a per-instance NATS _INBOX reply
	// subject, an exclusive auto-delete AMQP reply queue. That is the Return
	// Address pattern, and it is what an honest true means here.
	//
	// SingleSubscriber MUST NOT block and MUST NOT panic. msgin calls it inside
	// NewChannelExchange, on the caller's goroutine, with no context and no
	// timeout; it must be a constant-time accessor over state fixed at
	// construction. A panic is recovered and read as false (fail closed), and a
	// blocking implementation hangs the constructor with nothing to cancel.
	//
	// The value MUST be constant for the lifetime of the channel. msgin calls it
	// once, at construction, and treats the answer as an invariant; a value that can
	// change afterwards makes the check a TOCTOU race the core cannot detect.
	// Implementations must also be safe for concurrent use.
	//
	// EMBEDDING CUTS BOTH WAYS. A type that embeds a *channel.DirectChannel or a
	// *channel.PublishSubscribeChannel inherits SingleSubscriber by method
	// promotion, so it reports on the EMBEDDED channel even when it overrides
	// Subscribe with its own multi-subscriber dispatch. A wrapper that changes
	// subscription behavior MUST declare its own SingleSubscriber.
	SingleSubscriber() bool
}

// Subscription is a handle to an active subscription. Cancel removes the
// subscriber; it is idempotent (a second Cancel is a safe no-op).
//
// It lives here, not in the channel package, because it is the return type of
// SubscribableChannel.Subscribe and of the root-level TopicSubscriber SPI:
// moving it would make root import channel (Spec 014 decision D-C).
type Subscription interface{ Cancel() }
