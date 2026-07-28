package routing_test

import (
	"testing"

	"go.uber.org/goleak"
)

// TestMain runs the package's goroutine-leak check. The Aggregator owns a
// background release/timeout loop, so every test that calls Run must join it
// before returning; this is the package-wide backstop for the per-test
// goleak.VerifyNone(t) calls in aggregator_test.go.
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}
