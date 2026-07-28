package msgin

import (
	"context"
)

// MessageChannel is the conduit endpoints send into and subscribe to. Its Send
// is structurally identical to OutboundAdapter.Send.
type MessageChannel interface {
	Send(ctx context.Context, msg Message[any]) error
	Subscribe(h MessageHandler) error
}

// Subscription is a handle to an active subscription. Cancel removes the
// subscriber; it is idempotent (a second Cancel is a safe no-op).
//
// It lives here, not in the channel package, because it is the return type of
// the root-level TopicSubscriber SPI: moving it would make root import channel
// (Spec 014 decision D-C).
type Subscription interface{ Cancel() }
