package msgin

import (
	"time"
)

// BackoffStrategy computes the redelivery delay for a given 0-based retry index
// (0 = first retry). It is stateless and clock-agnostic: pure arithmetic, safe
// to share across goroutines. This is RetryPolicy.Backoff (spec §8).
type BackoffStrategy interface {
	Delay(attempt int) time.Duration
}
