package msgin

import (
	"math"
	"math/rand/v2"
	"time"
)

// BackoffStrategy computes the redelivery delay for a given 0-based retry index
// (0 = first retry). It is stateless and clock-agnostic: pure arithmetic, safe
// to share across goroutines. This is RetryPolicy.Backoff (spec §8).
type BackoffStrategy interface {
	Delay(attempt int) time.Duration
}

// ExponentialBackoff is the default BackoffStrategy: min(Max, Initial × Mult^attempt),
// with optional jitter. All fields are guarded so no combination panics or
// returns a negative duration (a nil/zero-value ExponentialBackoff yields 0).
type ExponentialBackoff struct {
	Initial             time.Duration // delay for the first retry (attempt 0)
	Max                 time.Duration // cap; <= 0 means uncapped
	Mult                float64       // growth factor per attempt; <= 0 treated as 1 (no growth)
	RandomizationFactor float64       // 0 = deterministic; f>0 spreads over [d·(1-f), d·(1+f)]
}

// Delay returns the backoff for the given 0-based retry index.
func (b ExponentialBackoff) Delay(attempt int) time.Duration {
	if attempt < 0 {
		attempt = 0
	}
	if b.Initial <= 0 {
		return 0
	}
	mult := b.Mult
	if mult <= 0 {
		mult = 1
	}
	d := float64(b.Initial) * math.Pow(mult, float64(attempt))
	if math.IsInf(d, 1) || math.IsNaN(d) {
		if b.Max > 0 {
			return b.Max
		}
		return b.Initial
	}
	out := time.Duration(d)
	if b.Max > 0 && out > b.Max {
		out = b.Max
	}
	if b.RandomizationFactor > 0 {
		out = jitter(out, b.RandomizationFactor)
		// Re-clamp: jitter spreads symmetrically around out, so a value already
		// at the Max cap can jitter back above it. Max is a hard cap (the
		// struct doc's "min(Max, ...)"), so re-apply it after jitter too.
		if b.Max > 0 && out > b.Max {
			out = b.Max
		}
	}
	return out
}

// jitter spreads d uniformly over [d·(1-f), d·(1+f)], clamped to >= 0.
func jitter(d time.Duration, f float64) time.Duration {
	delta := float64(d) * f
	lo := float64(d) - delta
	j := lo + rand.Float64()*(2*delta)
	if j < 0 {
		j = 0
	}
	return time.Duration(j)
}
