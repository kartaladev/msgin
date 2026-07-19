package msgin

import "context"

// QueueChannel is a pollable, point-to-point queue channel (EIP Point-to-Point
// Channel; Spring Integration QueueChannel). It is backed by a swappable
// ChannelStore: in-memory for async decoupling, or a persistent store for
// Guaranteed Delivery across restarts.
//
// It is NOT a Subscribe-based MessageChannel (that is DirectChannel's synchronous
// push model). A QueueChannel is pollable: it implements OutboundAdapter (Send
// enqueues) and PollingSource (Poll claims), so you produce into it with
// NewProducer and drain it with NewConsumer — the existing Poller, worker pool,
// retry, dead-letter, and backpressure carry it unchanged. The channel starts no
// goroutine of its own.
type QueueChannel struct {
	store ChannelStore
}

var (
	_ OutboundAdapter   = (*QueueChannel)(nil)
	_ PollingSource     = (*QueueChannel)(nil)
	_ LiveValueSource   = (*QueueChannel)(nil)
	_ NativeReliability = (*QueueChannel)(nil)
)

// NewQueueChannel wraps store as a pollable queue channel. A nil store is
// ErrNilStore. Capacity and overflow policy are properties of the store (only a
// bounded in-memory store has them); configure them on the store's constructor.
func NewQueueChannel(store ChannelStore) (*QueueChannel, error) {
	if store == nil {
		return nil, ErrNilStore
	}
	return &QueueChannel{store: store}, nil
}

// Send enqueues msg (OutboundAdapter). Backpressure/overflow is the store's
// concern: a bounded store's Enqueue blocks, drops, or rejects per its policy.
func (q *QueueChannel) Send(ctx context.Context, msg Message[any]) error {
	return q.store.Enqueue(ctx, msg)
}

// Poll claims up to max ready deliveries (PollingSource). The store owns the
// three Poll invariants (<=max; no deliveries with a non-nil error; own cleanup
// on the error/cancel path); QueueChannel forwards verbatim.
func (q *QueueChannel) Poll(ctx context.Context, max int) ([]Delivery, error) {
	return q.store.Claim(ctx, max)
}

// EmitsLiveValue delegates to the store (LiveValueSource), so NewConsumer pairs a
// codec iff the store carries []byte.
func (q *QueueChannel) EmitsLiveValue() bool { return q.store.EmitsLiveValue() }

// NativeRedelivery / NativeDeadLetter forward the store's NativeReliability when
// it implements it (e.g. the SQL store, whose backing Source redelivers via the
// DB row), else report false. Forwarding keeps a durable-store-backed
// QueueChannel behaviorally identical to its underlying Source and prevents a
// future store whose NativeDeadLetter is true from being silently double
// dead-lettered by the runtime (audit M-2).
func (q *QueueChannel) NativeRedelivery() bool {
	if nr, ok := q.store.(NativeReliability); ok {
		return nr.NativeRedelivery()
	}
	return false
}

// NativeDeadLetter forwards the store's NativeReliability when it implements
// it, else reports false. See NativeRedelivery.
func (q *QueueChannel) NativeDeadLetter() bool {
	if nr, ok := q.store.(NativeReliability); ok {
		return nr.NativeDeadLetter()
	}
	return false
}
