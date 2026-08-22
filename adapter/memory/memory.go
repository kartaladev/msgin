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
	// err latches the FIRST construction fault New cannot return (it has no
	// error return), from either of two sources: a nil ELEMENT of opts, or a
	// WithBuffer size outside [0, maxBufferCeiling]. Set once in New,
	// read-only thereafter — so it needs no lock — and reported by Send and
	// Stream.
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

// maxBufferCeiling is the upper bound WithBuffer accepts (Spec 016 §3.4).
// Unlike this package's other two ceilings, the allocation IS the hazard here:
// make(chan msgin.Message[any], n) has a 24-byte element, so a large n either
// panics ("makechan: size out of range") or — worse, in the band the runtime's
// own check lets through — dies with an unrecoverable "fatal error: out of
// memory" that no recover can intercept (Spec 016 §1.1). The ceiling is stated
// in the knob's own unit (queued messages), matching WithCapacity and
// endpoint.WithMaxInFlight so all three agree, and costs 25,166,072 B (24.0 MiB)
// at the ceiling — four orders of magnitude below the fatal band (ADR 0032
// D-Z).
const maxBufferCeiling = 1 << 20

// WithBuffer sets the channel buffer size, which must be in
// [0, maxBufferCeiling] (0 to 1,048,576); default 0 — synchronous handoff, so
// an unbuffered Broker is the legal lower end, not an error.
//
// An n outside that range — negative, or above the ceiling — is neither a panic
// nor a silent clamp: it is a msgin.ErrInvalidCapacity, wrapped in
// msgin.Permanent and naming the site, the value and the range. See
// maxBufferCeiling's godoc for why the upper bound exists.
//
// The fault is PERMANENT for the Broker's lifetime, and a later valid option
// does NOT clear it: New(WithBuffer(-1), WithBuffer(8)) returns a Broker that
// reports the -1 fault from Send and Stream even though the effective buffer
// would have been 8. That is the first-fault-wins latch (ADR 0031 D-U/D-V)
// applied consistently — the same is true of a nil option element followed by
// valid ones — and it is deliberate: a constructor that silently forgives a
// rejected argument because a later argument happened to be fine would hide
// the caller's mistake. Fix the offending call rather than appending another.
//
// The fault surfaces at [Broker.Send] and [Broker.Stream], NOT at [New],
// because New returns *Broker with no error: the fault is latched on the Broker
// and reported by both error-returning methods before their own logic, exactly
// as a nil option element is (Spec 016 §3.2, family R2; ADR 0031 D-V). It is
// permanent because n is fixed here and cannot become valid later, so retrying
// can never succeed.
func WithBuffer(n int) Option {
	return func(b *Broker) {
		// The sentinel is Permanent-wrapped BEFORE it reaches checkRange, so the
		// wrap lands inside the %w exactly as Spec 016 §3.1's shape requires and
		// the rendered string keeps its "msgin: permanent: " prefix. Wrapping the
		// returned error instead would print identically but build a different
		// error tree, and §3.2/D-Y's shape is what Task 5's review verified.
		// Building it eagerly costs one allocation per WithBuffer application at
		// construction; New is not a hot path.
		if err := checkRange(msgin.Permanent(msgin.ErrInvalidCapacity), "memory.WithBuffer",
			n, 0, maxBufferCeiling); err != nil {
			if b.err == nil { // first-fault-wins, same latch as the nil-element arm in New
				b.err = err
			}
			// LOAD-BEARING, and it must stay OUTSIDE the latch's if (ADR 0032
			// D-Y): New's apply loop continues past a nil option (ADR 0031
			// D-U), so this option still runs when an earlier nil already took
			// the first-fault-wins latch. Gating the return on `b.err == nil`
			// would then fall through to the make and panic — the latch decides
			// only WHICH error the caller reads, never whether the allocation is
			// prevented.
			return
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
//
// [WithBuffer] latches the same field when its size is out of range, so the
// two faults share one first-fault-wins latch: whichever comes FIRST in opts is
// the one Send and Stream report, and the other is not recorded. A later
// out-of-range WithBuffer still refuses to allocate even when it loses the
// latch (ADR 0032 D-Y).
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

// Send enqueues a message (outbound adapter). A fault latched by New — a nil
// option element, or an out-of-range [WithBuffer] size — is reported FIRST
// (ADR 0031 D-V), before anything is enqueued.
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
// are no-ops for at-most-once; Nack with requeue re-enqueues. A fault latched
// by New — a nil option element, or an out-of-range [WithBuffer] size — is
// reported FIRST (ADR 0031 D-V), before any delivery loop runs.
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
