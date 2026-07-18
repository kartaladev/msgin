package msgin

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
)

// FanOutPolicy selects how a PublishSubscribeChannel settles a fan-out when a
// subscriber's handler returns an error.
type FanOutPolicy int

const (
	// FanOutAllSucceed (the default) invokes every subscriber and, if any returns
	// an error, Send returns a joined error — so a Consumer-driven publish
	// Nacks/retries (at-least-once for the whole fan-out). Because a retry
	// re-invokes ALL subscribers, subscribers should be idempotent.
	//
	// The fan-out settles as ONE unit: the joined error's classification follows
	// the runtime's rules, so if any subscriber returns a permanent error (e.g.
	// ErrPayloadType — errors.Join propagates it), a Consumer-driven publish routes
	// the WHOLE message to the invalid-message sink (observable, not retried);
	// otherwise it is transient and the whole fan-out retries. A subscriber whose
	// permanent failure must NOT affect the others' redelivery needs per-subscriber
	// independent settlement — a durable adapter concern, out of scope here.
	FanOutAllSucceed FanOutPolicy = iota
	// FanOutBestEffort invokes every subscriber, logs each error, and Send always
	// returns nil (Ack). A failed delivery is NOT retried — use only when a missed
	// subscriber is acceptable.
	FanOutBestEffort
)

// Subscription is a handle to an active subscription. Cancel removes the
// subscriber; it is idempotent (a second Cancel is a safe no-op).
type Subscription interface{ Cancel() }

type pubSubConfig struct {
	policy FanOutPolicy
	logger *slog.Logger
}

func defaultPubSubConfig() pubSubConfig {
	return pubSubConfig{policy: FanOutAllSucceed, logger: slog.New(slog.DiscardHandler)}
}

// PubSubOption configures a PublishSubscribeChannel or a PubSub registry.
type PubSubOption func(*pubSubConfig)

// WithFanOut sets the fan-out settlement policy. The default, FanOutAllSucceed,
// is the safe choice: a subscriber error surfaces (joined) so the message is
// retried rather than silently missed. Choose FanOutBestEffort only when a
// dropped delivery to one subscriber is acceptable.
func WithFanOut(p FanOutPolicy) PubSubOption { return func(c *pubSubConfig) { c.policy = p } }

// WithPubSubLogger injects the logger used to report subscriber errors under
// FanOutBestEffort. Defaults to a discarding logger (no output).
func WithPubSubLogger(l *slog.Logger) PubSubOption {
	return func(c *pubSubConfig) {
		if l != nil {
			c.logger = l
		}
	}
}

// withConfig seeds a channel with an already-built config (used by PubSub so all
// topic channels inherit the registry's fan-out policy and logger).
func withConfig(cfg pubSubConfig) PubSubOption { return func(c *pubSubConfig) { *c = cfg } }

// subscription is one registered handler on a PublishSubscribeChannel.
type subscription struct {
	ch      *PublishSubscribeChannel
	handler MessageHandler
	once    sync.Once
}

// Cancel removes the subscriber from its channel (idempotent).
func (s *subscription) Cancel() { s.once.Do(func() { s.ch.remove(s) }) }

// PublishSubscribeChannel is an in-process EIP Publish-Subscribe Channel: Send
// fans a message out to EVERY subscriber synchronously, on the caller's
// goroutine, in registration order (no goroutine is started). Subscribe returns
// a Subscription whose Cancel unsubscribes. It is an OutboundAdapter (Send), so
// a flow can terminate in To(psChannel) to broadcast.
type PublishSubscribeChannel struct {
	mu   sync.RWMutex
	subs []*subscription
	cfg  pubSubConfig
}

var _ OutboundAdapter = (*PublishSubscribeChannel)(nil)

// NewPublishSubscribeChannel returns an empty channel; Subscribe handlers, then Send.
func NewPublishSubscribeChannel(opts ...PubSubOption) *PublishSubscribeChannel {
	c := &PublishSubscribeChannel{cfg: defaultPubSubConfig()}
	for _, opt := range opts {
		opt(&c.cfg)
	}
	return c
}

// Subscribe registers h and returns a Subscription. A nil handler is ErrNilHandler.
func (c *PublishSubscribeChannel) Subscribe(h MessageHandler) (Subscription, error) {
	if h == nil {
		return nil, ErrNilHandler
	}
	s := &subscription{ch: c, handler: h}
	c.mu.Lock()
	c.subs = append(c.subs, s)
	c.mu.Unlock()
	return s, nil
}

// remove deletes s from the subscriber slice (called by subscription.Cancel).
func (c *PublishSubscribeChannel) remove(s *subscription) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for i, x := range c.subs {
		if x == s {
			c.subs = append(c.subs[:i], c.subs[i+1:]...)
			return
		}
	}
}

// isEmpty reports whether the channel has no subscribers (used by PubSub for
// drop-on-empty topic hygiene).
func (c *PublishSubscribeChannel) isEmpty() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.subs) == 0
}

// Send fans msg out to every current subscriber. It snapshots the subscriber set
// under a read lock and dispatches OUTSIDE the lock (so a handler may
// Subscribe/Cancel without deadlock, and concurrent Sends do not serialize on
// handler execution). Under FanOutAllSucceed a subscriber error is collected and
// the joined error returned after every subscriber has run; under
// FanOutBestEffort errors are logged and Send returns nil.
//
// Concurrency semantics: a subscriber cancelled AFTER Send snapshots still
// receives this in-flight message (same as DirectChannel). A Send that races the
// last Cancel may fan out to zero subscribers and return nil (delivered-to-none).
// A panicking subscriber is recovered per-subscriber (ErrHandlerPanic, transient)
// so it never aborts the fan-out — the loop always reaches every subscriber.
func (c *PublishSubscribeChannel) Send(ctx context.Context, msg Message[any]) error {
	c.mu.RLock()
	snapshot := make([]*subscription, len(c.subs))
	copy(snapshot, c.subs)
	c.mu.RUnlock()

	var errs []error
	for _, s := range snapshot {
		if err := safeFanOut(ctx, s.handler, msg); err != nil {
			if c.cfg.policy == FanOutBestEffort {
				c.cfg.logger.WarnContext(ctx, "msgin: pub-sub subscriber failed (best-effort)", "err", err)
				continue
			}
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...) // nil when errs is empty
}

// safeFanOut invokes one subscriber, recovering a panic into a transient
// ErrHandlerPanic so a panicking subscriber cannot abort the fan-out (fault
// isolation, CLAUDE.md) — the caller's loop continues to the remaining
// subscribers. ErrHandlerPanic is classified transient (reliability.go), so under
// FanOutAllSucceed a panicked subscriber makes the fan-out retry rather than divert.
func safeFanOut(ctx context.Context, h MessageHandler, msg Message[any]) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("%w: %v", ErrHandlerPanic, r)
		}
	}()
	return h.Handle(ctx, msg)
}
