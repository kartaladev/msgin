package msgin

import (
	"context"
	"time"
)

// Delivery is one received message plus the means to settle it. Msg.Payload is
// []byte for wire adapters or a live value for the in-memory adapter.
type Delivery struct {
	Msg  Message[any]
	Ack  func(ctx context.Context) error
	Nack func(ctx context.Context, requeue bool, delay time.Duration) error
}

// PollingSource is a pulled inbound adapter, driven by the runtime's Poller.
//
// Poll fetches up to max claimable deliveries. The runtime acquires max credits
// BEFORE calling Poll (credit-at-fetch, spec §7.4.1), so it never over-pulls past
// WithMaxInFlight. An implementation MUST honor three invariants — the runtime
// enforces the first defensively and relies on the other two:
//
//  1. Return AT MOST max deliveries. Returning more corrupts the credit
//     accounting; the runtime defensively clamps and Nacks the excess, but a
//     correct source never returns more than asked.
//  2. Return NO deliveries alongside a non-nil error. A partial (rows, err) is
//     forbidden: the runtime discards rows on error, which for a lock/tx-carrying
//     Delivery would leak the transaction/connection it holds.
//  3. Own the cleanup (rollback) of any partial or claimed work on the error or
//     ctx-cancel path, returning (nil, err) — a claimed-but-not-returned row must
//     be released back to the source so it is redelivered, not lost.
type PollingSource interface {
	Poll(ctx context.Context, max int) ([]Delivery, error)
}

// StreamingSource is a pushed inbound adapter that owns a blocking, cancellable loop.
type StreamingSource interface {
	Stream(ctx context.Context, out chan<- Delivery) error
}

// OutboundAdapter writes a message to the external system.
type OutboundAdapter interface {
	Send(ctx context.Context, msg Message[any]) error
}

// NativeReliability is an optional capability: two independent booleans (ADR 0002).
type NativeReliability interface {
	NativeRedelivery() bool
	NativeDeadLetter() bool
}

// LiveValueSource is an optional capability: a source emitting live Go values
// (in-memory) rather than []byte, so NewConsumer can enforce codec pairing.
type LiveValueSource interface {
	EmitsLiveValue() bool
}

// ScheduledSender is an optional capability of an OutboundAdapter: it delivers a
// message so that it becomes consumable only after a delay elapses (durable
// delayed send). An adapter that can defer delivery — e.g. the database/sql
// adapter via its visible_after column — implements it; the producer discovers
// it by type-assertion and returns ErrScheduledSendUnsupported when the sink
// does not.
//
// The capability carries the RELATIVE primitive only: the delivery time is
// computed by the adapter's own store as now+delay, so it is free of app-vs-store
// clock skew. A delay <= 0 delivers immediately (equivalent to Send). Absolute-time
// scheduling is a producer-side concern (Producer.SendAt), never pushed into the
// adapter.
type ScheduledSender interface {
	SendAfter(ctx context.Context, msg Message[any], delay time.Duration) error
}
