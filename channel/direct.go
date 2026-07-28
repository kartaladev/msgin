package channel

import (
	"context"
	"sync"

	"github.com/kartaladev/msgin"
)

// DirectChannel is a synchronous, point-to-point channel with exactly one
// subscriber: Send invokes the subscribed handler on the caller's goroutine and
// returns its error. It starts no goroutine, and running in the caller's
// settlement scope preserves end-to-end at-least-once when driven by a Consumer.
type DirectChannel struct {
	mu      sync.RWMutex
	handler msgin.MessageHandler
}

var _ msgin.MessageChannel = (*DirectChannel)(nil)

// NewDirectChannel returns an empty DirectChannel; Subscribe one handler before Send.
func NewDirectChannel() *DirectChannel { return &DirectChannel{} }

// Subscribe registers the single point-to-point handler. A nil handler is
// ErrNilHandler; a second Subscribe is ErrChannelSubscribed.
func (c *DirectChannel) Subscribe(h msgin.MessageHandler) error {
	if h == nil {
		return msgin.ErrNilHandler
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.handler != nil {
		return msgin.ErrChannelSubscribed
	}
	c.handler = h
	return nil
}

// Send invokes the subscribed handler synchronously. With no subscriber it is
// ErrNoSubscriber (never a silent drop).
func (c *DirectChannel) Send(ctx context.Context, msg msgin.Message[any]) error {
	c.mu.RLock()
	h := c.handler
	c.mu.RUnlock()
	if h == nil {
		return msgin.ErrNoSubscriber
	}
	return h.Handle(ctx, msg)
}
