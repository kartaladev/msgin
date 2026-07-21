package msgin_test

import (
	"math"
	"testing"
	"time"

	"github.com/kartaladev/msgin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExponentialBackoff_Delay(t *testing.T) {
	base := msgin.ExponentialBackoff{Initial: 100 * time.Millisecond, Max: time.Second, Mult: 2}

	tests := []struct {
		name    string
		backoff msgin.ExponentialBackoff
		attempt int
		assert  func(t *testing.T, d time.Duration)
	}{
		{"first retry is Initial", base, 0, func(t *testing.T, d time.Duration) {
			assert.Equal(t, 100*time.Millisecond, d)
		}},
		{"grows by Mult", base, 1, func(t *testing.T, d time.Duration) {
			assert.Equal(t, 200*time.Millisecond, d)
		}},
		{"caps at Max", base, 10, func(t *testing.T, d time.Duration) {
			assert.Equal(t, time.Second, d) // 100ms*2^10 = 102.4s -> capped
		}},
		{"negative attempt clamps to first", base, -5, func(t *testing.T, d time.Duration) {
			assert.Equal(t, 100*time.Millisecond, d)
		}},
		{"zero Initial yields zero", msgin.ExponentialBackoff{Initial: 0, Max: time.Second, Mult: 2}, 3,
			func(t *testing.T, d time.Duration) {
				assert.Equal(t, time.Duration(0), d)
			}},
		{"non-growing Mult stays at Initial", msgin.ExponentialBackoff{Initial: 50 * time.Millisecond, Max: time.Second, Mult: 0}, 4,
			func(t *testing.T, d time.Duration) {
				assert.Equal(t, 50*time.Millisecond, d)
			}},
		{"no Max means no cap", msgin.ExponentialBackoff{Initial: time.Second, Max: 0, Mult: 2}, 2,
			func(t *testing.T, d time.Duration) {
				assert.Equal(t, 4*time.Second, d)
			}},
		{"jitter stays within bounds", msgin.ExponentialBackoff{Initial: 100 * time.Millisecond, Max: time.Second, Mult: 2, RandomizationFactor: 0.5}, 1,
			func(t *testing.T, d time.Duration) {
				// base for attempt 1 = 200ms; jitter in [100ms, 300ms]
				assert.GreaterOrEqual(t, d, 100*time.Millisecond)
				assert.LessOrEqual(t, d, 300*time.Millisecond)
			}},
		{"overflow with Max caps at Max", base, 2000, func(t *testing.T, d time.Duration) {
			// 100ms*2^2000 overflows float64 to +Inf -> IsInf guard, Max>0 -> Max.
			assert.Equal(t, time.Second, d)
		}},
		{"overflow without Max returns Initial", msgin.ExponentialBackoff{Initial: time.Second, Max: 0, Mult: 2}, 2000,
			func(t *testing.T, d time.Duration) {
				// +Inf with no cap -> IsInf guard, Max<=0 -> Initial.
				assert.Equal(t, time.Second, d)
			}},
		{"astronomic-but-finite growth is capped at Max", base, 100, func(t *testing.T, d time.Duration) {
			// 100ms*2^100 ~ 1.27e38ns: finite, far above MaxInt64, so the old
			// IsInf-only guard let it convert out of range (MinInt64 on amd64,
			// saturated MaxInt64 on arm64) and, when negative, slip past the cap.
			assert.Equal(t, time.Second, d)
		}},
		{"astronomic-but-finite growth uncapped falls back to Initial",
			msgin.ExponentialBackoff{Initial: time.Second, Max: 0, Mult: 2}, 100,
			func(t *testing.T, d time.Duration) {
				assert.Positive(t, d, "an out-of-range float->int conversion must not yield a negative delay")
				assert.Equal(t, time.Second, d)
			}},
		{"jitter cannot push an uncapped backoff negative",
			msgin.ExponentialBackoff{Initial: time.Second, Max: 0, Mult: 2, RandomizationFactor: 0.5}, 33,
			func(t *testing.T, _ time.Duration) {
				// jitter samples uniformly in [d/2, 3d/2]; at attempt 33 the
				// centre is ~8.6e18ns, so only the upper ~28% of the range
				// exceeds MaxInt64 and reaches the new guard. ONE draw would hit
				// it ~28% of the time, so coverage on this arm would flap
				// run-to-run. 200 draws make a total miss ~(0.72)^200 ~ 1e-28,
				// and every draw independently pins the property that matters.
				b := msgin.ExponentialBackoff{Initial: time.Second, Max: 0, Mult: 2, RandomizationFactor: 0.5}
				for i := range 200 {
					d := b.Delay(33)
					assert.Positive(t, d,
						"draw %d: jitter must never produce a negative delay", i)
				}
			}},
		{"an infinite randomization factor cannot produce a negative delay",
			msgin.ExponentialBackoff{Initial: time.Second, Max: 0, Mult: 2, RandomizationFactor: math.Inf(1)}, 1,
			func(t *testing.T, d time.Duration) {
				// An infinite factor makes delta = +Inf, so lo = -Inf and
				// j = -Inf + rand*(+Inf) = NaN. NaN compares false against BOTH
				// `j < 0` and `j >= MaxInt64`, so before the IsNaN guard it
				// reached the conversion and yielded MinInt64 on amd64 — a
				// negative delay that the Max re-clamp cannot catch either.
				assert.Positive(t, d, "a NaN jitter draw must not become a negative duration")
			}},
		{"a NaN randomization factor skips jitter entirely",
			msgin.ExponentialBackoff{Initial: time.Second, Max: 0, Mult: 2, RandomizationFactor: math.NaN()}, 1,
			func(t *testing.T, d time.Duration) {
				// NaN fails the `RandomizationFactor > 0` test, so jitter is
				// never called and the computed backoff stands.
				assert.Equal(t, 2*time.Second, d)
			}},
		{"jitter never exceeds Max after cap", msgin.ExponentialBackoff{Initial: 100 * time.Millisecond, Max: time.Second, Mult: 2, RandomizationFactor: 0.5}, 10,
			func(t *testing.T, d time.Duration) {
				// pre-jitter value is already capped to Max (100ms*2^10 = 102.4s -> 1s);
				// jitter([0.5s, 1.5s]) must be re-clamped so Max stays a hard cap.
				assert.LessOrEqual(t, d, time.Second, "jitter must not push the delay above Max")
			}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tc.assert(t, tc.backoff.Delay(tc.attempt))
		})
	}
}

// TestExponentialBackoff_JitterLowSideClamp covers the low-side clamp in jitter
// (`if j < 0 { j = 0 }`), reachable only at RandomizationFactor > 1 — a valid,
// un-rejected config. With f = 2.0 the jittered lower bound d·(1-f) = -d is
// negative, so a draw can land below zero and MUST be clamped to a non-negative
// duration. jitter uses math/rand/v2, so we assert the invariant (never
// negative) across many draws rather than an exact value; enough iterations make
// the negative-draw branch essentially certain to execute.
func TestExponentialBackoff_JitterLowSideClamp(t *testing.T) {
	b := msgin.ExponentialBackoff{Initial: 100 * time.Millisecond, Max: time.Second, Mult: 2, RandomizationFactor: 2.0}
	for i := 0; i < 1000; i++ {
		d := b.Delay(i % 8)
		require.GreaterOrEqual(t, d, time.Duration(0),
			"jitter must never yield a negative delay, even at RandomizationFactor > 1")
	}
}

func TestExponentialBackoff_SatisfiesInterface(t *testing.T) {
	var _ msgin.BackoffStrategy = msgin.ExponentialBackoff{}
}
