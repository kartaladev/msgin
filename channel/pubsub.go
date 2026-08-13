package channel

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"github.com/kartaladev/msgin"
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
	// ErrPayloadType — errors.Join propagates it), a Consumer-driven publish diverts
	// the WHOLE message to the invalid-message sink — or the dead-letter sink when
	// none is configured ([msgin.Permanent] states all three arms) — observable,
	// not retried;
	// otherwise it is transient and the whole fan-out retries. A subscriber whose
	// permanent failure must NOT affect the others' redelivery needs per-subscriber
	// independent settlement — a durable adapter concern, out of scope here.
	FanOutAllSucceed FanOutPolicy = iota
	// FanOutBestEffort invokes every subscriber, logs each error, and Send always
	// returns nil (Ack). A failed delivery is NOT retried — use only when a missed
	// subscriber is acceptable.
	FanOutBestEffort
)

type pubSubConfig struct {
	policy FanOutPolicy
	logger *slog.Logger
	single bool
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

// WithSingleSubscriber restricts the channel to one subscriber at a time: while
// a subscriber is registered, a second Subscribe is ErrChannelSubscribed —
// the same typed error DirectChannel returns — and the slot frees again on that
// subscriber's Cancel.
//
// OFF BY DEFAULT, and deliberately so: fan-out to every subscriber IS the
// Publish-Subscribe Channel pattern, so restricting it must be the caller's
// explicit choice and existing flows are unaffected. Opt in when the channel is
// used where exclusivity is part of the contract — most importantly as an
// endpoint.NewChannelExchange REPLY channel, where a second subscriber does not
// fail loudly but silently hands a copy of every correlated reply to the other
// subscriber (typically another exchange's unmatched-reply sink). This turns
// that mis-wiring into a typed error at Subscribe time instead of a godoc
// warning nobody reads (ADR 0028 §6.2).
//
// THIS IS A SINGLE-PROCESS GUARD, exactly like [DirectChannel]'s, and it MUST
// NOT be read as a distributed exclusivity guarantee. The subscriber slot is a
// field on this Go value: N instances behind a load balancer each hold their
// OWN PublishSubscribeChannel, so each one independently accepts its own
// subscriber and none of them can see — let alone reject — the other N−1. What
// the option guarantees is "at most one subscriber on THIS channel value in
// THIS process", never "at most one subscriber across the deployment".
//
// The distributed answer is the Return Address pattern (EIP ch.5): carry the
// reply destination in the request and make that destination private to the
// subscribing process — a per-instance NATS _INBOX subject, an exclusive
// auto-delete AMQP reply queue — so exclusivity comes from the broker's own
// topology rather than from a local flag. See [msgin.ExclusiveSubscribable],
// whose SingleSubscriber probe is the end-to-end predicate a broker-backed
// channel must answer honestly (ADR 0028 §6.2; Spec 014 §10).
//
// Passed to NewPubSub it applies to every topic channel the registry creates,
// making each topic single-subscriber.
func WithSingleSubscriber() PubSubOption { return func(c *pubSubConfig) { c.single = true } }

// withConfig seeds a channel with an already-built config (used by PubSub so all
// topic channels inherit the registry's fan-out policy and logger).
func withConfig(cfg pubSubConfig) PubSubOption { return func(c *pubSubConfig) { *c = cfg } }

// subscription is one registered handler on a PublishSubscribeChannel.
type subscription struct {
	ch      *PublishSubscribeChannel
	handler msgin.MessageHandler
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

var (
	_ msgin.OutboundAdapter       = (*PublishSubscribeChannel)(nil)
	_ msgin.SubscribableChannel   = (*PublishSubscribeChannel)(nil)
	_ msgin.ExclusiveSubscribable = (*PublishSubscribeChannel)(nil)
)

// SingleSubscriber reports whether WithSingleSubscriber was passed to this
// channel — the channel's POLICY, fixed at construction, never its live
// subscriber count. Without that option the channel fans every message out to
// every subscriber, so it reports false and endpoint.NewChannelExchange rejects
// it as a reply channel with msgin.ErrSharedReplyChannel unless the caller opts
// into the fan-out. The value is set once by NewPublishSubscribeChannel and
// never written again, so it is constant for the channel's lifetime and safe
// for concurrent use, as msgin.ExclusiveSubscribable requires (ADR 0030 §2).
func (c *PublishSubscribeChannel) SingleSubscriber() bool { return c.cfg.single }

// NewPublishSubscribeChannel returns an empty channel; Subscribe handlers, then Send.
func NewPublishSubscribeChannel(opts ...PubSubOption) *PublishSubscribeChannel {
	c := &PublishSubscribeChannel{cfg: defaultPubSubConfig()}
	for _, opt := range opts {
		opt(&c.cfg)
	}
	return c
}

// Subscribe registers h and returns a Subscription. A nil handler is
// ErrNilHandler. Under WithSingleSubscriber a second concurrent subscriber is
// ErrChannelSubscribed; without it (the default) any number of subscribers is
// accepted and every one receives every message.
func (c *PublishSubscribeChannel) Subscribe(h msgin.MessageHandler) (msgin.Subscription, error) {
	if h == nil {
		return nil, msgin.ErrNilHandler
	}
	s := &subscription{ch: c, handler: h}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cfg.single && len(c.subs) > 0 {
		return nil, msgin.ErrChannelSubscribed
	}
	c.subs = append(c.subs, s)
	return s, nil
}

// remove deletes s from the subscriber slice (called by subscription.Cancel).
func (c *PublishSubscribeChannel) remove(s *subscription) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for i, x := range c.subs {
		if x == s {
			copy(c.subs[i:], c.subs[i+1:])
			c.subs[len(c.subs)-1] = nil
			c.subs = c.subs[:len(c.subs)-1]
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
func (c *PublishSubscribeChannel) Send(ctx context.Context, msg msgin.Message[any]) error {
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
func safeFanOut(ctx context.Context, h msgin.MessageHandler, msg msgin.Message[any]) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("%w: %v", msgin.ErrHandlerPanic, r)
		}
	}()
	return h.Handle(ctx, msg)
}
