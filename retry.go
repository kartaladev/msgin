package msgin

import (
	"context"
)

// RetryPolicy governs how the runtime settles a failed delivery (spec §7).
//
//   - MaxAttempts == 0 : retry forever (no dead-letter).
//   - MaxAttempts  > 0 : after that many delivery attempts a still-failing
//     message is diverted to DeadLetter (required); DeadLetter may be any
//     OutboundAdapter, including another msgin adapter.
//   - Backoff nil       : immediate redelivery (zero delay).
//
// The zero value is valid and means "retry forever, immediately, no DLQ".
//
// THE SAME POLICY READS DIFFERENTLY ON THE TWO PATHS. The description above is
// the CONSUMER path (NewConsumer), where a retry is a broker REDELIVERY: the
// message goes back to the source and the broker paces it, so "forever,
// immediately" costs no local resource. On the PRODUCER path
// (WithProducerRetry) a retry is a live re-send on the CALLER'S OWN goroutine
// with no broker in between, so the same fields are bounded further:
//
//   - MaxAttempts == 0 does NOT mean forever. Every producer retry is bounded by
//     WithProducerRetryBudget (2 minutes by default, always on) — including a
//     finite MaxAttempts, which the budget can cut short. A stop caused by the
//     budget rather than by spent attempts is marked ErrRetryBudgetExhausted so
//     the two remain distinguishable.
//   - Backoff nil, or any strategy yielding a non-positive delay, is floored to
//     100ms per wait rather than spinning.
//   - The ZERO VALUE IS REJECTED by NewProducer with ErrUnboundedRetry:
//     MaxAttempts == 0 with a nil Backoff is a zero-delay infinite loop on the
//     caller's goroutine, which is a caller mistake worth failing loudly on.
//     It remains valid for a Consumer.
//
// See WithProducerRetry, WithProducerRetryBudget and ADR 0025 §1.1.
type RetryPolicy struct {
	// MaxAttempts is COUNTED PER INSTANCE, not across the deployment. What the
	// consumer actually tests is THE PRESENCE OF THE HeaderDeliveryCount HEADER
	// on the delivery: carry one and that native count wins. Carry none and the
	// count comes from an in-memory attempt tracker owned by the Consumer —
	// one tracker per NewConsumer call, so even two Consumers draining the same
	// source in the SAME process keep independent counts. (A source stamping
	// that header typically also reports NativeReliability.NativeRedelivery,
	// but that capability is two booleans and obliges nobody to stamp
	// anything: a source claiming native redelivery while omitting the header
	// still falls back to the tracker.)
	//
	// A message redelivered to a DIFFERENT node therefore starts its count at
	// zero, so behind a load balancer with N nodes the effective global bound
	// is N × MaxAttempts, and a caller sizing a poison-message threshold
	// silently gets N times what they asked for. Read that as an UPPER-BOUND
	// APPROXIMATION rather than an exact bound: the tracker also evicts ids
	// idle for longer than endpoint.WithAttemptTTL, so a message bouncing among
	// the other nodes can age out of a given node's map and restart its count
	// there. A source that DOES stamp HeaderDeliveryCount is unaffected: the
	// broker counts, and every node reads the same number.
	//
	// The distributed answer is that native redelivery count, or a shared
	// idempotency/dedup store keyed by message id — not a smaller MaxAttempts,
	// which only moves the error (Spec 014 §10).
	MaxAttempts int
	Backoff     BackoffStrategy
	DeadLetter  OutboundAdapter
}

// Validate reports whether the policy is internally consistent. A finite
// MaxAttempts requires a DeadLetter; a negative MaxAttempts is invalid. Called
// by NewConsumer so a bad policy fails at construction (spec §5).
func (p RetryPolicy) Validate() error {
	if p.MaxAttempts < 0 {
		return ErrInvalidMaxAttempts
	}
	if p.MaxAttempts > 0 && p.DeadLetter == nil {
		return ErrNoDeadLetter
	}
	return nil
}

// Hooks are optional, nil-safe callbacks fired on the operationally important
// settlement events (spec §7 observability). The error argument carries the
// triggering error (nil on a successful Ack).
type Hooks struct {
	OnRetry          func(ctx context.Context, msg Message[any], err error)
	OnDeadLetter     func(ctx context.Context, msg Message[any], err error)
	OnInvalidMessage func(ctx context.Context, msg Message[any], err error)
	OnAck            func(ctx context.Context, msg Message[any], err error)
}
