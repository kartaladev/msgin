package msgin

import (
	"context"
	"sync"
	"time"

	"github.com/jonboulle/clockwork"
)

// tokenBucket is the dependency-free default RateLimiter: a clockwork-driven
// bucket of `burst` tokens refilling at `rps` per second.
type tokenBucket struct {
	clock    clockwork.Clock
	interval time.Duration // time to accrue one token
	burst    float64
	mu       sync.Mutex
	tokens   float64
	last     time.Time
}

// TokenBucketOption configures NewTokenBucket.
type TokenBucketOption func(*tokenBucket)

// WithTokenBucketClock injects the clock (default: real). Tests pass a fake clock.
func WithTokenBucketClock(c clockwork.Clock) TokenBucketOption {
	return func(b *tokenBucket) {
		if c != nil {
			b.clock = c
		}
	}
}

// NewTokenBucket builds the default RateLimiter. rps must be > 0 and burst >= 1,
// else ErrInvalidRateLimit. It starts full (burst tokens).
func NewTokenBucket(rps float64, burst int, opts ...TokenBucketOption) (RateLimiter, error) {
	if rps <= 0 || burst < 1 {
		return nil, ErrInvalidRateLimit
	}
	b := &tokenBucket{
		clock:    clockwork.NewRealClock(),
		interval: time.Duration(float64(time.Second) / rps),
		burst:    float64(burst),
		tokens:   float64(burst),
	}
	for _, opt := range opts {
		opt(b)
	}
	b.last = b.clock.Now()
	return b, nil
}

// Wait blocks until a token is available or ctx is done, then consumes exactly
// one. It LOOPS re-checking after each wait rather than admitting unconditionally
// once the timer fires (M1): if a concurrent caller consumed the freed token
// while we slept, tokens can be < 1 again, and a blind post-wait admit would
// over-admit (return without decrementing, i.e. hand out a token that isn't
// there). Looping consumes a token only when one is genuinely present, so the
// rps bound holds even if the limiter is shared by several callers. (In msgin
// the limiter is driven only by the single serial ingest goroutine, so the loop
// runs at most once in practice — but correctness must not rely on that.)
func (b *tokenBucket) Wait(ctx context.Context) error {
	for {
		b.mu.Lock()
		b.refill()
		if b.tokens >= 1 {
			b.tokens--
			b.mu.Unlock()
			return nil
		}
		deficit := 1 - b.tokens
		wait := time.Duration(deficit * float64(b.interval))
		b.mu.Unlock()

		select {
		case <-b.clock.After(wait):
			// loop: re-check; the token may have been taken by another caller.
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// refill accrues tokens for the elapsed time, capped at burst. Caller holds mu.
func (b *tokenBucket) refill() {
	now := b.clock.Now()
	elapsed := now.Sub(b.last)
	if elapsed <= 0 {
		return
	}
	b.last = now
	b.tokens += float64(elapsed) / float64(b.interval)
	if b.tokens > b.burst {
		b.tokens = b.burst
	}
}
