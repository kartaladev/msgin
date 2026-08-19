package channel

import (
	"context"
	"fmt"
	"sync"

	"github.com/kartaladev/msgin"
)

// PubSub is the in-process topic registry: it maps a topic name to a
// PublishSubscribeChannel, created on first Subscribe and dropped when its last
// subscriber cancels. Publish fans out to that topic's subscribers only.
type PubSub struct {
	mu     sync.Mutex
	topics map[string]*PublishSubscribeChannel
	cfg    pubSubConfig
	// err latches a construction fault NewPubSub cannot return (it has no error
	// return): the first nil ELEMENT of opts. Set once in the constructor,
	// read-only thereafter — so it needs no lock — and reported by Publish and
	// Subscribe. Spec 015 §3.2 (family R2).
	//
	// It lives on the STRUCT and deliberately NOT in cfg: Subscribe seeds every
	// topic channel with withConfig(p.cfg), so a latch held in the config would
	// propagate this registry's fault into every topic channel it creates.
	err error
}

var (
	_ msgin.TopicPublisher  = (*PubSub)(nil)
	_ msgin.TopicSubscriber = (*PubSub)(nil)
)

// NewPubSub returns an empty registry. Options apply to every topic channel it creates.
//
// A nil ELEMENT of opts is not a panic (no panic on caller input). This
// constructor has no error return, so the FIRST nil element is LATCHED and both
// error-returning methods — [PubSub.Publish] and [PubSub.Subscribe] — report it
// as a PERMANENT msgin.ErrNilFunc naming the element's 0-based index
// ("channel.NewPubSub: nil option at index 1"). The degradation is permanent
// because an option element is fixed here and cannot become non-nil later, so
// retrying can never succeed. Every non-nil option still applies, including any
// after the nil (ADR 0031 D-U), and the latch is checked at the TOP of each
// method, before its own argument checks (D-V), so a latched
// Subscribe(topic, nil) reports the nil OPTION rather than msgin.ErrNilHandler.
// The latch is the registry's own: no topic channel is created while it is set,
// and none inherits it.
func NewPubSub(opts ...PubSubOption) *PubSub {
	p := &PubSub{topics: make(map[string]*PublishSubscribeChannel), cfg: defaultPubSubConfig()}
	for i, opt := range opts {
		if opt == nil {
			if p.err == nil { // first-nil-wins (D-U: latch only when unlatched)
				p.err = fmt.Errorf("%w: %s: nil option at index %d",
					msgin.Permanent(msgin.ErrNilFunc), "channel.NewPubSub", i)
			}
			continue // D-U: the surviving options are the caller's stated intent
		}
		opt(&p.cfg)
	}
	return p
}

// Publish fans msg out to the topic's subscribers. A topic with no subscribers is
// a no-op (never an error): publishing before anyone subscribes is normal for
// broadcast. It returns the topic channel's joined fan-out error (see FanOutPolicy).
//
// A nil option element latched by NewPubSub is reported before the topic is
// looked up: nothing is dispatched and the permanent msgin.ErrNilFunc is
// returned instead of the no-op nil.
func (p *PubSub) Publish(ctx context.Context, topic string, msg msgin.Message[any]) error {
	if p.err != nil {
		return p.err
	}
	p.mu.Lock()
	ch := p.topics[topic]
	p.mu.Unlock()
	if ch == nil {
		return nil
	}
	return ch.Send(ctx, msg)
}

// Subscribe registers h on topic, lazily creating the topic channel. The returned
// Subscription's Cancel unsubscribes AND drops the topic if it becomes empty. A
// nil handler is ErrNilHandler (no topic is created).
//
// A nil option element latched by NewPubSub is reported FIRST, above the
// nil-handler check (ADR 0031 D-V) — the option fault happened earlier, at
// construction — and no topic is created for it either.
func (p *PubSub) Subscribe(topic string, h msgin.MessageHandler) (msgin.Subscription, error) {
	if p.err != nil {
		return nil, p.err
	}
	if h == nil {
		return nil, msgin.ErrNilHandler
	}
	p.mu.Lock()
	ch := p.topics[topic]
	if ch == nil {
		ch = NewPublishSubscribeChannel(withConfig(p.cfg))
		p.topics[topic] = ch
	}
	// F1: subscribe UNDER p.mu, so a concurrent last-subscriber Cancel cannot drop
	// the topic in the window between the map insert and the subscribe (a TOCTOU
	// that would orphan this subscriber on a channel no longer in the registry).
	// Lock order stays p.mu -> ch.mu — the SAME nesting topicSubscription.Cancel
	// uses when it calls isEmpty() under p.mu — so no deadlock; and ch.Subscribe
	// runs no handler code, so holding p.mu across it cannot re-enter the registry.
	inner, err := ch.Subscribe(h)
	p.mu.Unlock()
	if err != nil {
		// Reachable since decision D-F: under WithSingleSubscriber a second
		// subscriber to an EXISTING topic is ErrChannelSubscribed. (The nil
		// handler case is guarded above; a topic this call just created is empty,
		// so the guard cannot reject the subscriber that created it — no empty
		// topic can be stranded here.)
		return nil, err
	}
	return &topicSubscription{ps: p, topic: topic, ch: ch, inner: inner}, nil
}

// TopicCount reports the number of live topics (topics with ≥1 subscriber). Zero
// after every subscriber of every topic has cancelled — proves drop-on-empty.
func (p *PubSub) TopicCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.topics)
}

// topicSubscription wraps a channel Subscription so Cancel also GCs an empty topic.
type topicSubscription struct {
	ps    *PubSub
	topic string
	ch    *PublishSubscribeChannel
	inner msgin.Subscription
}

// Cancel unsubscribes, then drops the topic if it has no remaining subscribers.
func (s *topicSubscription) Cancel() {
	s.inner.Cancel()
	s.ps.mu.Lock()
	defer s.ps.mu.Unlock()
	// Only drop the exact channel we hold, and only if still empty — a concurrent
	// Subscribe to the same topic may have re-populated or replaced it.
	if cur, ok := s.ps.topics[s.topic]; ok && cur == s.ch && cur.isEmpty() {
		delete(s.ps.topics, s.topic)
	}
}
