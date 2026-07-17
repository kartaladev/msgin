package msgin

import (
	"context"
	"time"
)

// RateLimiter paces ingress. Wait blocks until the caller may proceed or ctx is
// done. The default is a dependency-free clockwork token bucket (NewTokenBucket);
// golang.org/x/time/rate.Limiter satisfies this interface directly.
//
// Implementations MUST NOT panic: the runtime invokes Wait directly on the
// ingress goroutine and does not recover a panic here (unlike handler/hooks,
// which are recovered by safeHandle/safeFire), so a panicking implementation
// crashes the process. The shipped default (NewTokenBucket) never panics.
type RateLimiter interface {
	Wait(ctx context.Context) error
}

// CircuitBreaker gates dispatch on downstream health (spec §7.4.5, NF-10).
//
//   - Allow reports whether work may proceed now (closed or half-open).
//   - Record feeds the outcome of an allowed dispatch back to the breaker.
//   - HalfOpen returns a channel the runtime parks on while Allow reports
//     false; it is closed (or replaced with a fresh open channel per
//     open-cycle) only when the breaker transitions to half-open.
//     Implementations MUST NOT return an already-closed channel while Allow
//     continues to report false — doing so causes the runtime's ingress loop
//     (admitBreaker) to busy-spin (it re-subscribes, re-checks Allow, and
//     immediately falls through the already-closed channel in a tight loop,
//     with no cooldown to rate-limit the spin). The default NewCircuitBreaker
//     mints a fresh channel per open-cycle and only closes it from
//     toHalfOpen, so this contract holds by construction.
//
// The default is a dependency-free clockwork breaker (NewCircuitBreaker);
// sony/gobreaker can be adapted via its OnStateChange callback.
//
// Implementations MUST NOT panic: the runtime invokes Allow/Record/HalfOpen
// directly on the ingress and worker goroutines and does not recover a panic
// here (unlike handler/hooks, which are recovered by safeHandle/safeFire), so
// a panicking implementation crashes the process. The shipped default
// (NewCircuitBreaker) never panics.
type CircuitBreaker interface {
	Allow() bool
	Record(success bool)
	HalfOpen() <-chan struct{}
}

// OverflowPolicy selects what a push source's ingress does when the credit pool
// is exhausted (spec §7.4.6). Block backpressures; the Drop/Reject policies shed.
type OverflowPolicy int

const (
	// OverflowBlock backpressures the stream loop until a credit frees (default).
	OverflowBlock OverflowPolicy = iota
	// OverflowDropNewest drops the just-arrived message (Nack, no requeue).
	OverflowDropNewest
	// OverflowDropOldest is accepted for API stability; on a streaming source
	// with no discrete buffer stage it behaves as OverflowDropNewest until the
	// Poller buffer lands (Plan 004) — see ADR 0008 D9.
	OverflowDropOldest
	// OverflowReject is HTTP-semantics (503/429); on a non-HTTP streaming source
	// it behaves as OverflowDropNewest until adapter/http lands — see ADR 0008 D9.
	OverflowReject
)

// String renders the policy; unknown values render as OverflowBlock (the safe default).
func (p OverflowPolicy) String() string {
	switch p {
	case OverflowDropNewest:
		return "OverflowDropNewest"
	case OverflowDropOldest:
		return "OverflowDropOldest"
	case OverflowReject:
		return "OverflowReject"
	default:
		return "OverflowBlock"
	}
}

// defaultMaxInFlight bounds claimed-but-unsettled messages when WithMaxInFlight
// is unset. It is finite (never unbounded — spec §7.4.1): the credit gate is
// always active.
const defaultMaxInFlight = 1024

// defaultAttemptTTL is how long an idle attempt-tracker entry survives before the
// clockwork sweep reclaims it (ADR 0008 D8). It vastly exceeds any in-process
// redelivery cadence, so an actively-redelivering id (re-observed each attempt)
// is never swept mid-flight (NF-2).
const defaultAttemptTTL = 5 * time.Minute

// WithMaxInFlight bounds claimed-but-unsettled messages to n (n >= 1). This is
// the load-bearing flood defense (spec §7.4.1); default defaultMaxInFlight.
// Setting the flag lets NewConsumer tell an explicit WithMaxInFlight(0) (a
// caller error → ErrInvalidMaxInFlight) apart from "unset" (→ default) (C2).
func WithMaxInFlight[T any](n int) ConsumerOption[T] {
	return func(o *consumerConfig[T]) { o.maxInFlight = n; o.maxInFlightSet = true }
}

// WithRateLimit paces ingress with r (spec §7.4.2). A nil r is a no-op.
func WithRateLimit[T any](r RateLimiter) ConsumerOption[T] {
	return func(o *consumerConfig[T]) {
		if r != nil {
			o.rateLimiter = r
		}
	}
}

// WithHandlerTimeout runs each handler under a clockwork-derived deadline d
// (spec §7.4.4). A stuck cooperative handler is cancelled → transient failure
// (retried), not a pinned worker. d <= 0 disables the timeout.
func WithHandlerTimeout[T any](d time.Duration) ConsumerOption[T] {
	return func(o *consumerConfig[T]) { o.handlerTimeout = d }
}

// WithCircuitBreaker gates ingress AND dispatch on b (spec §7.4.5). A nil b is a no-op.
func WithCircuitBreaker[T any](b CircuitBreaker) ConsumerOption[T] {
	return func(o *consumerConfig[T]) {
		if b != nil {
			o.breaker = b
		}
	}
}

// WithOverflow selects the ingress overflow policy for a push source (spec §7.4.6);
// default OverflowBlock.
func WithOverflow[T any](p OverflowPolicy) ConsumerOption[T] {
	return func(o *consumerConfig[T]) { o.overflow = p }
}
