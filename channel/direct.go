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
//
// The single-subscriber restriction is deliberate and differs from Spring
// Integration's DirectChannel, which load-balances across several subscribers.
// Here, competing consumers come from the Consumer's worker pool
// (WithConcurrency), not from multiple subscriptions — one dispatch policy
// instead of two overlapping ones. Use a PublishSubscribeChannel when every
// subscriber should see every message.
type DirectChannel struct {
	mu      sync.RWMutex
	handler msgin.MessageHandler
	sub     *directSubscription
}

var (
	_ msgin.MessageChannel      = (*DirectChannel)(nil)
	_ msgin.SubscribableChannel = (*DirectChannel)(nil)
)

// directSubscription is the handle returned by DirectChannel.Subscribe.
type directSubscription struct {
	ch   *DirectChannel
	once sync.Once
}

// Cancel releases the channel's single subscriber slot. It is idempotent (a
// second Cancel is a no-op) and never panics. A Send already dispatching when
// Cancel runs completes normally — Cancel prevents only SUBSEQUENT dispatch,
// matching PublishSubscribeChannel's snapshot semantics.
func (s *directSubscription) Cancel() { s.once.Do(func() { s.ch.release(s) }) }

// NewDirectChannel returns an empty DirectChannel; Subscribe one handler before Send.
func NewDirectChannel() *DirectChannel { return &DirectChannel{} }

// Subscribe registers the single point-to-point handler and returns the handle
// that releases it. A nil handler is (nil, ErrNilHandler) — the error path
// returns no handle, so a caller never nil-checks one it did not obtain. A
// second Subscribe while a subscriber is registered is (nil, ErrChannelSubscribed);
// after that subscriber's Cancel the slot is free and Subscribe succeeds again.
func (c *DirectChannel) Subscribe(h msgin.MessageHandler) (msgin.Subscription, error) {
	if h == nil {
		return nil, msgin.ErrNilHandler
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.handler != nil {
		return nil, msgin.ErrChannelSubscribed
	}
	s := &directSubscription{ch: c}
	c.handler, c.sub = h, s
	return s, nil
}

// release clears the subscriber slot iff s is still the channel's CURRENT
// subscription. The identity check matters: a stale handle cancelled after the
// slot was released and re-subscribed must not evict the new subscriber.
func (c *DirectChannel) release(s *directSubscription) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.sub == s {
		c.handler, c.sub = nil, nil
	}
}

// Send invokes the subscribed handler synchronously. With no subscriber — before
// the first Subscribe, or after the current subscription's Cancel — it is
// ErrNoSubscriber (never a silent drop). The handler is read under the lock and
// invoked outside it, so a Cancel racing an in-flight Send cannot interrupt the
// dispatch already under way.
func (c *DirectChannel) Send(ctx context.Context, msg msgin.Message[any]) error {
	c.mu.RLock()
	h := c.handler
	c.mu.RUnlock()
	if h == nil {
		return msgin.ErrNoSubscriber
	}
	return h.Handle(ctx, msg)
}
