package resilience_test

import (
	"testing"
	"time"

	"github.com/jonboulle/clockwork"
	"github.com/kartaladev/msgin"
	"github.com/kartaladev/msgin/resilience"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCircuitBreaker_OpensClosesAndHalfOpenWakeup(t *testing.T) {
	clk := clockwork.NewFakeClock()
	b, err := resilience.NewCircuitBreaker(
		resilience.WithBreakerClock(clk),
		resilience.WithBreakerThreshold(2),
		resilience.WithBreakerCooldown(3*time.Second),
	)
	require.NoError(t, err)

	assert.True(t, b.Allow(), "starts closed")
	b.Record(false)
	assert.True(t, b.Allow(), "one failure below threshold: still closed")
	b.Record(false)
	assert.False(t, b.Allow(), "threshold reached: open")

	// A parked waiter must be woken EXPLICITLY when the cooldown elapses.
	wake := b.HalfOpen()
	woken := make(chan struct{})
	go func() {
		<-wake
		close(woken)
	}()
	require.NoError(t, clk.BlockUntilContext(t.Context(), 1)) // the AfterFunc cooldown timer is registered
	clk.Advance(3 * time.Second)                              // fire cooldown → open→half-open → close(wake)
	select {
	case <-woken:
	case <-time.After(time.Second):
		t.Fatal("half-open transition did not wake the parked waiter (missed wakeup)")
	}
	assert.True(t, b.Allow(), "half-open allows a probe")

	b.Record(true)
	assert.True(t, b.Allow(), "probe success closes the breaker")
}

func TestCircuitBreaker_HalfOpenProbeFailureReopens(t *testing.T) {
	clk := clockwork.NewFakeClock()
	b, err := resilience.NewCircuitBreaker(resilience.WithBreakerClock(clk), resilience.WithBreakerThreshold(1), resilience.WithBreakerCooldown(time.Second))
	require.NoError(t, err)
	b.Record(false)
	require.False(t, b.Allow())

	// Synchronize on the explicit half-open wakeup (clockwork fires AfterFunc in
	// its own goroutine) so the state read below is not racing the transition.
	wake := b.HalfOpen()
	require.NoError(t, clk.BlockUntilContext(t.Context(), 1))
	clk.Advance(time.Second) // fire cooldown → open→half-open
	select {
	case <-wake:
	case <-time.After(time.Second):
		t.Fatal("cooldown did not half-open the breaker")
	}

	require.True(t, b.Allow(), "half-open allows a probe")
	b.Record(false) // probe fails
	assert.False(t, b.Allow(), "half-open probe failure reopens")
}

// TestCircuitBreaker_RecordSuccessFromOpen_DoesNotReclose pins the reachable
// straggler case (ADR 0008 D7 fix pass): under WithConcurrency(N>1), a worker
// dispatch admitted before another worker's failure trips the breaker can
// still call Record(true) after the breaker is already open. breaker.Record's
// `if state == breakerHalfOpen` guard means this only zeroes the failure
// count — it must NOT re-close (or otherwise leave open) an already-open
// breaker, which stays open until its own cooldown/probe cycle decides
// otherwise. Driven directly through the public breaker API (Allow/Record) so
// the interleaving is deterministic, rather than racing real workers.
func TestCircuitBreaker_RecordSuccessFromOpen_DoesNotReclose(t *testing.T) {
	clk := clockwork.NewFakeClock()
	b, err := resilience.NewCircuitBreaker(
		resilience.WithBreakerClock(clk),
		resilience.WithBreakerThreshold(1),
		resilience.WithBreakerCooldown(time.Minute), // long enough that the cooldown never fires in this test
	)
	require.NoError(t, err)

	b.Record(false) // trips closed->open on threshold 1
	require.False(t, b.Allow(), "open after tripping")

	b.Record(true) // straggler success, admitted before the breaker opened
	assert.False(t, b.Allow(), "a straggler success must not re-close an open breaker")
}

// halfOpen drives b (a fresh default breaker on fake clock clk with the given
// cooldown) from open into half-open deterministically, returning once the
// transition has fired. Precondition: b is already open.
func halfOpen(t *testing.T, clk *clockwork.FakeClock, b msgin.CircuitBreaker, cooldown time.Duration) {
	t.Helper()
	wake := b.HalfOpen()
	require.NoError(t, clk.BlockUntilContext(t.Context(), 1)) // cooldown AfterFunc registered
	clk.Advance(cooldown)
	select {
	case <-wake:
	case <-time.After(time.Second):
		t.Fatal("cooldown did not half-open the breaker")
	}
}

// TestCircuitBreaker_ProbeGate_SingleProbeInHalfOpen proves the default breaker
// implements ProbeGate (ADR 0009 D2): TryProbe is unlimited when closed, denied
// when open, and admits exactly ONE probe in half-open (the rest denied until a
// Record settles it) — the fix for the N>1 half-open probe storm.
func TestCircuitBreaker_ProbeGate_SingleProbeInHalfOpen(t *testing.T) {
	clk := clockwork.NewFakeClock()
	b, err := resilience.NewCircuitBreaker(resilience.WithBreakerClock(clk), resilience.WithBreakerThreshold(1), resilience.WithBreakerCooldown(time.Second))
	require.NoError(t, err)
	pg, ok := b.(msgin.ProbeGate)
	require.True(t, ok, "NewCircuitBreaker must implement ProbeGate")

	assert.True(t, pg.TryProbe(), "closed: TryProbe unlimited (1)")
	assert.True(t, pg.TryProbe(), "closed: TryProbe unlimited (2)")

	b.Record(false) // threshold 1 → open
	require.False(t, b.Allow(), "open")
	assert.False(t, pg.TryProbe(), "open: TryProbe denied")

	halfOpen(t, clk, b, time.Second)
	assert.True(t, pg.TryProbe(), "half-open: first probe admitted")
	assert.False(t, pg.TryProbe(), "half-open: second probe denied while the first is in flight")
	assert.False(t, pg.TryProbe(), "half-open: still denied")

	b.Record(true) // probe succeeds → close
	assert.True(t, pg.TryProbe(), "closed again after a successful probe: unlimited")
}

// TestCircuitBreaker_ProbeGate_ReopenAdmitsFreshProbeNextCycle is the mandatory
// wedge test (ADR 0009 D2): after a half-open probe FAILS and the breaker
// reopens, the NEXT half-open cycle must admit a fresh probe. A buggy impl that
// clears probeInFlight only on the success path (or not in toHalfOpen) leaves the
// flag stuck true → TryProbe denies every probe forever → permanent half-open
// wedge. This asserts the flag is reset across cycles.
func TestCircuitBreaker_ProbeGate_ReopenAdmitsFreshProbeNextCycle(t *testing.T) {
	clk := clockwork.NewFakeClock()
	b, err := resilience.NewCircuitBreaker(resilience.WithBreakerClock(clk), resilience.WithBreakerThreshold(1), resilience.WithBreakerCooldown(time.Second))
	require.NoError(t, err)
	pg := b.(msgin.ProbeGate)

	b.Record(false) // → open (cycle 1)
	halfOpen(t, clk, b, time.Second)
	require.True(t, pg.TryProbe(), "cycle 1: probe admitted")
	require.False(t, pg.TryProbe(), "cycle 1: single-probe holds")

	b.Record(false) // probe fails → reopen (probeInFlight must be cleared)
	require.False(t, b.Allow(), "reopened after the failed probe")

	halfOpen(t, clk, b, time.Second) // cycle 2
	assert.True(t, pg.TryProbe(), "cycle 2: a FRESH probe is admitted (toHalfOpen reset the flag — no wedge)")
	assert.False(t, pg.TryProbe(), "cycle 2: single-probe still holds")
}

// TestNewCircuitBreaker_NilOptionElement proves a nil ELEMENT of opts (as
// opposed to a nil options SLICE, which is a normal zero-option call) is a
// BARE ErrNilFunc naming the computed 0-based index, not a panic. The
// CircuitBreaker interface has no error-returning method, so the fault cannot
// ride the product to first use (family R2 is structurally impossible) —
// NewCircuitBreaker instead gained an error return, moving it to family R1
// (Spec 015 §3.6, ADR 0031 D-T). It is LOOP-FIRST (Spec 015 §3.5): it has no
// argument checks at all, so nothing precedes the option loop.
func TestNewCircuitBreaker_NilOptionElement(t *testing.T) {
	tests := []struct {
		name   string
		opts   []resilience.CircuitBreakerOption
		assert func(t *testing.T, err error)
	}{
		{
			name: "nil element alone",
			opts: []resilience.CircuitBreakerOption{nil},
			assert: func(t *testing.T, err error) {
				require.ErrorIs(t, err, msgin.ErrNilFunc)
				assert.False(t, msgin.IsPermanent(err), "R1 nil-option error must stay bare")
				assert.Contains(t, err.Error(), "resilience.NewCircuitBreaker: nil option at index 0")
			},
		},
		{
			name: "nil element after a valid option asserts the COMPUTED index and the FULL position",
			opts: []resilience.CircuitBreakerOption{resilience.WithBreakerThreshold(2), nil},
			assert: func(t *testing.T, err error) {
				require.ErrorIs(t, err, msgin.ErrNilFunc)
				assert.Contains(t, err.Error(), "resilience.NewCircuitBreaker: nil option at index 1")
			},
		},
		{
			name: "first of two nils wins",
			opts: []resilience.CircuitBreakerOption{nil, nil},
			assert: func(t *testing.T, err error) {
				require.ErrorIs(t, err, msgin.ErrNilFunc)
				assert.Contains(t, err.Error(), "resilience.NewCircuitBreaker: nil option at index 0")
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			b, err := resilience.NewCircuitBreaker(tc.opts...)
			assert.Nil(t, b)
			tc.assert(t, err)
		})
	}
}
