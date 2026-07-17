package msgin_test

import (
	"context"
	"testing"
	"time"

	"github.com/jonboulle/clockwork"
	"github.com/kartaladev/msgin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewTokenBucket_Validation(t *testing.T) {
	tests := []struct {
		name   string
		rps    float64
		burst  int
		assert func(t *testing.T, err error)
	}{
		{"non-positive rps rejected", 0, 1, func(t *testing.T, err error) { assert.ErrorIs(t, err, msgin.ErrInvalidRateLimit) }},
		{"burst below 1 rejected", 10, 0, func(t *testing.T, err error) { assert.ErrorIs(t, err, msgin.ErrInvalidRateLimit) }},
		{"valid config", 10, 5, func(t *testing.T, err error) { assert.NoError(t, err) }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := msgin.NewTokenBucket(tc.rps, tc.burst)
			tc.assert(t, err)
		})
	}
}

// Deterministic pacing: burst 1, 10 rps => second Wait must block ~100ms on the
// fake clock. First Wait returns immediately (initial token); the second parks
// until the clock advances one refill interval.
func TestTokenBucket_Wait_PacesOnFakeClock(t *testing.T) {
	clk := clockwork.NewFakeClock()
	rl, err := msgin.NewTokenBucket(10, 1, msgin.WithTokenBucketClock(clk))
	require.NoError(t, err)

	require.NoError(t, rl.Wait(t.Context())) // initial token, no wait

	waited := make(chan error, 1)
	go func() { waited <- rl.Wait(t.Context()) }()

	require.NoError(t, clk.BlockUntilContext(t.Context(), 1)) // the second Wait is parked on clock.After
	clk.Advance(100 * time.Millisecond)                       // one refill interval
	select {
	case err := <-waited:
		assert.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("second Wait did not unblock after a refill interval")
	}
}

func TestTokenBucket_Wait_CancelledContext(t *testing.T) {
	clk := clockwork.NewFakeClock()
	rl, err := msgin.NewTokenBucket(1, 1, msgin.WithTokenBucketClock(clk))
	require.NoError(t, err)
	require.NoError(t, rl.Wait(t.Context())) // consume the initial token

	ctx, cancel := context.WithCancel(t.Context())
	waited := make(chan error, 1)
	go func() { waited <- rl.Wait(ctx) }()
	require.NoError(t, clk.BlockUntilContext(t.Context(), 1))
	cancel()
	assert.ErrorIs(t, <-waited, context.Canceled)
}

// A long idle period must accrue at most `burst` tokens, never unboundedly:
// after a huge clock advance, exactly burst immediate Waits succeed before the
// next one has to park.
func TestTokenBucket_Wait_RefillCappedAtBurst(t *testing.T) {
	clk := clockwork.NewFakeClock()
	const burst = 3
	rl, err := msgin.NewTokenBucket(1, burst, msgin.WithTokenBucketClock(clk))
	require.NoError(t, err)

	clk.Advance(1000 * time.Second) // would accrue ~1000 tokens if uncapped

	for i := 0; i < burst; i++ {
		require.NoError(t, rl.Wait(t.Context()), "immediate token %d of %d", i+1, burst)
	}

	waited := make(chan error, 1)
	go func() { waited <- rl.Wait(t.Context()) }()
	require.NoError(t, clk.BlockUntilContext(t.Context(), 1)) // the (burst+1)th call must park — refill was capped, not unbounded
	clk.Advance(time.Second)
	select {
	case err := <-waited:
		assert.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("token after burst exhaustion did not unblock after a refill interval")
	}
}
