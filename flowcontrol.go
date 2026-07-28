package msgin

import (
	"context"
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

// ProbeGate is an OPTIONAL CircuitBreaker capability (ADR 0009 D2). A breaker that
// also implements it lets the runtime admit a bounded number of half-open probes
// at the DISPATCH gate rather than the whole half-open state — fixing the probe
// storm under WithConcurrency(N>1), where otherwise every worker reaching the gate
// while half-open is admitted instead of a single canary.
//
// TryProbe is a CONSUMING acquire (it may set internal probe state) called ONLY at
// the dispatch gate and ALWAYS paired with a following Record; the runtime's
// ingress open-check keeps using the idempotent Allow. It reports whether THIS
// dispatch may proceed: closed → true (unlimited); open → false; half-open → true
// for one probe, false for the rest until a Record settles it. A returned false
// consumes nothing (the message is Nacked without a Record).
//
// A CircuitBreaker that does NOT implement ProbeGate keeps the prior behavior (the
// dispatch gate falls back to Allow, admitting the whole half-open state). The
// default NewCircuitBreaker implements ProbeGate; a sony/gobreaker wrapper SHOULD
// implement TryProbe (mirroring gobreaker's own half-open MaxRequests) to get
// single-probe under N>1 — otherwise the runtime logs a one-time warning at Run.
//
// Implementations MUST NOT panic (same contract as CircuitBreaker); the runtime
// fail-opens (admits) on a panic but cannot rescue a plug-in that wedges its own
// probe state (ADR 0009 D1).
type ProbeGate interface {
	TryProbe() bool
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
