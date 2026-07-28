package resilience_test

import (
	"testing"

	"go.uber.org/goleak"
)

// TestMain runs the package's goroutine-leak check. The rate limiter and
// circuit breaker block on a clock and on ctx, so a wait that is not woken or
// not cancelled surfaces here as a leaked goroutine.
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}
