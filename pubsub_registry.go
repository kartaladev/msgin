package msgin

import (
	"context"
	"sync"
)

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

// PubSub is the in-process topic registry: it maps a topic name to a
// PublishSubscribeChannel, created on first Subscribe and dropped when its last
// subscriber cancels. Publish fans out to that topic's subscribers only.
type PubSub struct {
	mu     sync.Mutex
	topics map[string]*PublishSubscribeChannel
	cfg    pubSubConfig
}

var (
	_ TopicPublisher  = (*PubSub)(nil)
	_ TopicSubscriber = (*PubSub)(nil)
)

// NewPubSub returns an empty registry. Options apply to every topic channel it creates.
func NewPubSub(opts ...PubSubOption) *PubSub {
	p := &PubSub{topics: make(map[string]*PublishSubscribeChannel), cfg: defaultPubSubConfig()}
	for _, opt := range opts {
		opt(&p.cfg)
	}
	return p
}

// Publish fans msg out to the topic's subscribers. A topic with no subscribers is
// a no-op (never an error): publishing before anyone subscribes is normal for
// broadcast. It returns the topic channel's joined fan-out error (see FanOutPolicy).
func (p *PubSub) Publish(ctx context.Context, topic string, msg Message[any]) error {
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
func (p *PubSub) Subscribe(topic string, h MessageHandler) (Subscription, error) {
	if h == nil {
		return nil, ErrNilHandler
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
	if err != nil { // defensive: ch.Subscribe only errors on a nil handler, already guarded above
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
	inner Subscription
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
