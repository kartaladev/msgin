package resilience_test

import (
	"context"
	"testing"
	"time"

	"github.com/jonboulle/clockwork"
	"github.com/kartaladev/msgin"
	"github.com/kartaladev/msgin/resilience"
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
			_, err := resilience.NewTokenBucket(tc.rps, tc.burst)
			tc.assert(t, err)
		})
	}
}

// Deterministic pacing: burst 1, 10 rps => second Wait must block ~100ms on the
// fake clock. First Wait returns immediately (initial token); the second parks
// until the clock advances one refill interval.
func TestTokenBucket_Wait_PacesOnFakeClock(t *testing.T) {
	clk := clockwork.NewFakeClock()
	rl, err := resilience.NewTokenBucket(10, 1, resilience.WithTokenBucketClock(clk))
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
	rl, err := resilience.NewTokenBucket(1, 1, resilience.WithTokenBucketClock(clk))
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
	rl, err := resilience.NewTokenBucket(1, burst, resilience.WithTokenBucketClock(clk))
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

// TestNewTokenBucket_NilOptionElement proves a nil ELEMENT of opts (as opposed
// to a nil options SLICE, which is a normal zero-option call) is a BARE
// ErrNilFunc naming the computed 0-based index, not a panic (Spec 015 §3.1,
// family R1; ADR 0031 D-P/D-Q/D-R). rps and burst are both valid here — this
// is about the THIRD argument, opts, never about the two already covered by
// TestNewTokenBucket_Validation.
func TestNewTokenBucket_NilOptionElement(t *testing.T) {
	tests := []struct {
		name   string
		opts   []resilience.TokenBucketOption
		assert func(t *testing.T, err error)
	}{
		{
			name: "nil element alone",
			opts: []resilience.TokenBucketOption{nil},
			assert: func(t *testing.T, err error) {
				require.ErrorIs(t, err, msgin.ErrNilFunc)
				assert.False(t, msgin.IsPermanent(err), "R1 nil-option error must stay bare")
				assert.Contains(t, err.Error(), "resilience.NewTokenBucket: nil option at index 0")
			},
		},
		{
			name: "nil element after a valid option asserts the COMPUTED index and the FULL position",
			opts: []resilience.TokenBucketOption{resilience.WithTokenBucketClock(clockwork.NewFakeClock()), nil},
			assert: func(t *testing.T, err error) {
				require.ErrorIs(t, err, msgin.ErrNilFunc)
				assert.Contains(t, err.Error(), "resilience.NewTokenBucket: nil option at index 1")
			},
		},
		{
			name: "first of two nils wins",
			opts: []resilience.TokenBucketOption{nil, nil},
			assert: func(t *testing.T, err error) {
				require.ErrorIs(t, err, msgin.ErrNilFunc)
				assert.Contains(t, err.Error(), "resilience.NewTokenBucket: nil option at index 0")
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rl, err := resilience.NewTokenBucket(10, 5, tc.opts...)
			assert.Nil(t, rl)
			tc.assert(t, err)
		})
	}
}

// TestNewTokenBucket_NilOptionElement_ValidateFirst proves NewTokenBucket is
// VALIDATE-FIRST (Spec 015 §3.5). Its pre-loop guard is NUMERIC
// (rps <= 0 || burst < 1), not a nil/empty check — an earlier derivation
// script pattern-matched it into the wrong family (round-3 audit, M-L) — and
// it still wins over a nil option sitting at the SAME index 0. Standalone
// rather than folded into the table above because its setup (invalid rps and
// burst) is a different shape than "rps/burst valid, an option is nil" — the
// table-test skill's documented divergence exception.
func TestNewTokenBucket_NilOptionElement_ValidateFirst(t *testing.T) {
	_, err := resilience.NewTokenBucket(0, 0, nil)

	require.ErrorIs(t, err, msgin.ErrInvalidRateLimit)
	assert.NotErrorIs(t, err, msgin.ErrNilFunc)
}
