package memory

import (
	"context"
	"fmt"
	"time"

	"github.com/kartaladev/msgin"
)

// Broker is an in-process point-to-point transport backed by a Go channel. It
// carries live Go values (no codec, zero-copy) and is the reference adapter and
// test double. Delivery guarantee: at-most-once.
type Broker struct {
	ch chan msgin.Message[any]
	// err latches a construction fault New cannot return (it has no error
	// return): the first nil ELEMENT of opts. Set once in New, read-only
	// thereafter — so it needs no lock — and reported by Send and Stream.
	// Unlike the other R2 latches, this holds only as long as no
	// caller-supplied Option lets the live *Broker escape New: Option is
	// func(*Broker), exported, so a hostile option could stash b and read
	// err concurrently with New's write. Spec 015 §3.2 (family R2).
	err error
}

var (
	_ msgin.EventDrivenSource = (*Broker)(nil)
	_ msgin.OutboundAdapter   = (*Broker)(nil)
	_ msgin.LiveValueSource   = (*Broker)(nil)
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
//
// A nil ELEMENT of opts is not a panic (no panic on caller input). New has no
// error return, so the FIRST nil element is LATCHED and both error-returning
// methods — [Broker.Send] and [Broker.Stream] — report it as a PERMANENT
// msgin.ErrNilFunc naming the element's 0-based index ("memory.New: nil
// option at index 1"). The degradation is permanent because an option
// element is fixed here and cannot become non-nil later, so retrying can
// never succeed. Every non-nil option still applies, including any after the
// nil (ADR 0031 D-U), and the latch is checked at the TOP of each method,
// before its own logic (D-V).
func New(opts ...Option) *Broker {
	b := &Broker{ch: make(chan msgin.Message[any])}
	for i, opt := range opts {
		if opt == nil {
			if b.err == nil { // first-nil-wins (D-U: latch only when unlatched)
				b.err = fmt.Errorf("%w: %s: nil option at index %d",
					msgin.Permanent(msgin.ErrNilFunc), "memory.New", i)
			}
			continue // D-U: the surviving options are the caller's stated intent
		}
		opt(b)
	}
	return b
}

// Send enqueues a message (outbound adapter). A nil option element latched by
// New is reported FIRST (ADR 0031 D-V), before anything is enqueued.
func (b *Broker) Send(ctx context.Context, m msgin.Message[any]) error {
	if b.err != nil {
		return b.err
	}
	select {
	case b.ch <- m:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Stream delivers messages until ctx is cancelled (streaming source). Ack/Nack
// are no-ops for at-most-once; Nack with requeue re-enqueues. A nil option
// element latched by New is reported FIRST (ADR 0031 D-V), before any
// delivery loop runs.
func (b *Broker) Stream(ctx context.Context, out chan<- msgin.Delivery) error {
	if b.err != nil {
		return b.err
	}
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
