package msgin_test

import (
	"testing"
	"time"

	"github.com/jonboulle/clockwork"
	"github.com/kartaladev/msgin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCircuitBreaker_OpensClosesAndHalfOpenWakeup(t *testing.T) {
	clk := clockwork.NewFakeClock()
	b := msgin.NewCircuitBreaker(
		msgin.WithBreakerClock(clk),
		msgin.WithBreakerThreshold(2),
		msgin.WithBreakerCooldown(3*time.Second),
	)

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
	b := msgin.NewCircuitBreaker(msgin.WithBreakerClock(clk), msgin.WithBreakerThreshold(1), msgin.WithBreakerCooldown(time.Second))
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
	b := msgin.NewCircuitBreaker(
		msgin.WithBreakerClock(clk),
		msgin.WithBreakerThreshold(1),
		msgin.WithBreakerCooldown(time.Minute), // long enough that the cooldown never fires in this test
	)

	b.Record(false) // trips closed->open on threshold 1
	require.False(t, b.Allow(), "open after tripping")

	b.Record(true) // straggler success, admitted before the breaker opened
	assert.False(t, b.Allow(), "a straggler success must not re-close an open breaker")
}
