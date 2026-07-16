package memory

import (
	"context"
	"time"

	"github.com/kartaladev/msgin"
)

// Broker is an in-process point-to-point transport backed by a Go channel. It
// carries live Go values (no codec, zero-copy) and is the reference adapter and
// test double. Delivery guarantee: at-most-once.
type Broker struct {
	ch chan msgin.Message[any]
}

var (
	_ msgin.StreamingSource = (*Broker)(nil)
	_ msgin.OutboundAdapter = (*Broker)(nil)
	_ msgin.LiveValueSource = (*Broker)(nil)
)

// Option configures a Broker.
type Option func(*Broker)

// WithBuffer sets the channel buffer size (default 0 — synchronous handoff).
// A negative n is clamped to 0 rather than panicking, honoring the library's
// no-panic-on-caller-input contract.
func WithBuffer(n int) Option {
	return func(b *Broker) {
		if n < 0 {
			n = 0
		}
		b.ch = make(chan msgin.Message[any], n)
	}
}

// New builds an in-memory Broker.
func New(opts ...Option) *Broker {
	b := &Broker{ch: make(chan msgin.Message[any])}
	for _, opt := range opts {
		opt(b)
	}
	return b
}

// Send enqueues a message (outbound adapter).
func (b *Broker) Send(ctx context.Context, m msgin.Message[any]) error {
	select {
	case b.ch <- m:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Stream delivers messages until ctx is cancelled (streaming source). Ack/Nack
// are no-ops for at-most-once; Nack with requeue re-enqueues.
func (b *Broker) Stream(ctx context.Context, out chan<- msgin.Delivery) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case m := <-b.ch:
			d := msgin.Delivery{
				Msg:  m,
				Ack:  func(context.Context) error { return nil },
				Nack: b.nackFunc(m),
			}
			select {
			case out <- d:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
}

// nackFunc returns the Nack closure for a delivered message m: a no-op unless
// requeue is set, in which case m is re-enqueued via Send.
func (b *Broker) nackFunc(m msgin.Message[any]) func(context.Context, bool, time.Duration) error {
	return func(ctx context.Context, requeue bool, _ time.Duration) error {
		if !requeue {
			return nil
		}
		return b.Send(ctx, m)
	}
}

// EmitsLiveValue reports that this source carries live Go values (no codec).
func (b *Broker) EmitsLiveValue() bool { return true }
