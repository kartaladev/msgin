package msgin

import (
	"context"
	"sync"
)

// MessageChannel is the conduit endpoints send into and subscribe to. Its Send
// is structurally identical to OutboundAdapter.Send.
type MessageChannel interface {
	Send(ctx context.Context, msg Message[any]) error
	Subscribe(h MessageHandler) error
}

// DirectChannel is a synchronous, point-to-point channel with exactly one
// subscriber: Send invokes the subscribed handler on the caller's goroutine and
// returns its error. It starts no goroutine, and running in the caller's
// settlement scope preserves end-to-end at-least-once when driven by a Consumer.
type DirectChannel struct {
	mu      sync.RWMutex
	handler MessageHandler
}

var _ MessageChannel = (*DirectChannel)(nil)

// NewDirectChannel returns an empty DirectChannel; Subscribe one handler before Send.
func NewDirectChannel() *DirectChannel { return &DirectChannel{} }

// Subscribe registers the single point-to-point handler. A nil handler is
// ErrNilHandler; a second Subscribe is ErrChannelSubscribed.
func (c *DirectChannel) Subscribe(h MessageHandler) error {
	if h == nil {
		return ErrNilHandler
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.handler != nil {
		return ErrChannelSubscribed
	}
	c.handler = h
	return nil
}

// Send invokes the subscribed handler synchronously. With no subscriber it is
// ErrNoSubscriber (never a silent drop).
func (c *DirectChannel) Send(ctx context.Context, msg Message[any]) error {
	c.mu.RLock()
	h := c.handler
	c.mu.RUnlock()
	if h == nil {
		return ErrNoSubscriber
	}
	return h.Handle(ctx, msg)
}
