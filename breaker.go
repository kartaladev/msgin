package msgin

import (
	"sync"
	"time"

	"github.com/jonboulle/clockwork"
)

type breakerState int

const (
	breakerClosed breakerState = iota
	breakerOpen
	breakerHalfOpen
)

// breaker is the dependency-free default CircuitBreaker. It is clockwork-driven
// so the cooldown is deterministic in tests, and it signals the open→half-open
// transition by closing the HalfOpen channel (explicit wakeup, no missed-wakeup).
type breaker struct {
	clock     clockwork.Clock
	threshold int
	cooldown  time.Duration

	mu            sync.Mutex
	state         breakerState
	fails         int
	probeInFlight bool          // half-open single-probe gate (ProbeGate; ADR 0009 D2)
	wake          chan struct{} // closed on open→half-open; re-minted each open cycle
	timer         clockwork.Timer
}

// CircuitBreakerOption configures NewCircuitBreaker.
type CircuitBreakerOption func(*breaker)

// WithBreakerClock injects the clock used for the cooldown (default: real). A nil
// clock is ignored. Tests pass a clockwork fake clock and drive the cooldown by
// advancing it, so the open→half-open transition is deterministic.
func WithBreakerClock(c clockwork.Clock) CircuitBreakerOption {
	return func(b *breaker) {
		if c != nil {
			b.clock = c
		}
	}
}

// WithBreakerThreshold sets the number of consecutive failures that trip the
// breaker closed→open (default 5, minimum 1). A value below 1 is ignored.
func WithBreakerThreshold(n int) CircuitBreakerOption {
	return func(b *breaker) {
		if n >= 1 {
			b.threshold = n
		}
	}
}

// WithBreakerCooldown sets the open→half-open delay (default 30s, minimum > 0). A
// non-positive value is ignored.
func WithBreakerCooldown(d time.Duration) CircuitBreakerOption {
	return func(b *breaker) {
		if d > 0 {
			b.cooldown = d
		}
	}
}

// NewCircuitBreaker builds the default dependency-free CircuitBreaker: a
// clockwork-driven state machine that trips closed→open after threshold
// consecutive failures, schedules an open→half-open transition after the
// cooldown, then closes again on a successful probe or reopens on a failed one.
// It gates both ingress and dispatch when wired via WithCircuitBreaker (NF-10);
// HalfOpen signals the open→half-open transition with an explicit channel close
// so a parked ingress goroutine cannot miss the wakeup (spec §7.4.5, ADR 0008 D7).
func NewCircuitBreaker(opts ...CircuitBreakerOption) CircuitBreaker {
	b := &breaker{
		clock:     clockwork.NewRealClock(),
		threshold: 5,
		cooldown:  30 * time.Second,
		state:     breakerClosed,
		wake:      make(chan struct{}),
	}
	for _, opt := range opts {
		opt(b)
	}
	return b
}

// Allow reports whether work may proceed now: true when closed or half-open,
// false only when open. It is the IDEMPOTENT open-check used by the runtime's
// ingress park (it never consumes a probe); the dispatch gate uses TryProbe.
func (b *breaker) Allow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.state != breakerOpen
}

// TryProbe implements ProbeGate (ADR 0009 D2): the CONSUMING dispatch-gate
// acquire, paired with a following Record. Closed → true (unlimited, like Allow);
// open → false; half-open → true for exactly ONE caller (it sets probeInFlight),
// false for the rest until a Record settles the probe. This bounds half-open to a
// single canary under WithConcurrency(N>1), instead of admitting every worker.
func (b *breaker) TryProbe() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	switch b.state {
	case breakerOpen:
		return false
	case breakerHalfOpen:
		if b.probeInFlight {
			return false
		}
		b.probeInFlight = true
		return true
	default: // breakerClosed
		return true
	}
}

// Record feeds the outcome of an allowed dispatch back to the breaker. A success
// resets the failure count and, from half-open, closes the breaker. A failure
// increments the count and, at or above the threshold (closed) or on any
// half-open probe, trips the breaker open and (re)arms the cooldown.
func (b *breaker) Record(success bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if success {
		b.fails = 0
		if b.state == breakerHalfOpen {
			b.state = breakerClosed
			b.probeInFlight = false // probe succeeded → close; free the probe slot
		}
		// else: state == breakerOpen is reachable under WithConcurrency(N>1) —
		// a straggler dispatch admitted before another worker's failure opened
		// the breaker can still Record(true) here. It must NOT re-close an
		// open breaker (that would undo a just-tripped trip); zeroing fails is
		// harmless since openLocked/toHalfOpen own the open→half-open→closed
		// transitions from here on. The probe slot is owned by toHalfOpen's reset.
		return
	}
	b.fails++
	switch b.state {
	case breakerClosed:
		if b.fails >= b.threshold {
			b.openLocked()
		}
	case breakerHalfOpen:
		b.probeInFlight = false // probe failed → reopen; free the slot for the next cycle
		b.openLocked()          // probe failed → reopen (restarts the cooldown)
	}
}

// HalfOpen returns the channel that is closed when the breaker next transitions
// open→half-open. A caller parks on it AFTER re-checking Allow so a transition
// between the check and the park cannot be missed; a fresh channel is minted for
// each open cycle.
func (b *breaker) HalfOpen() <-chan struct{} {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.wake
}

// openLocked transitions to open and schedules the half-open. Caller holds mu.
func (b *breaker) openLocked() {
	b.state = breakerOpen
	if b.timer != nil {
		b.timer.Stop()
	}
	b.timer = b.clock.AfterFunc(b.cooldown, b.toHalfOpen)
}

// toHalfOpen fires on the cooldown timer: if still open, it transitions to
// half-open and closes the current wake channel (the explicit wakeup of any
// parked waiter), then mints a fresh channel for the next open cycle.
func (b *breaker) toHalfOpen() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.state != breakerOpen {
		return
	}
	b.state = breakerHalfOpen
	// UNCONDITIONALLY reset the single-probe gate on every genuine open→half-open
	// transition (ADR 0009 D2, the wedge fix). Even though the default Record
	// paths already clear probeInFlight on both half-open exits, resetting here is
	// the load-bearing guarantee: it makes "probeInFlight true ⟹ state half-open"
	// hold by construction, so no straggler/interleaving can carry a stuck flag
	// into a new half-open cycle and deny every probe forever.
	b.probeInFlight = false
	close(b.wake)                // explicit wakeup of parked waiters
	b.wake = make(chan struct{}) // fresh channel for the next open cycle
}
