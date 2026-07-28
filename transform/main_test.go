package transform_test

import (
	"testing"

	"go.uber.org/goleak"
)

// TestMain runs the package's goroutine-leak check. Transformers are
// synchronous and start no goroutine of their own, so this is a backstop that
// catches a leak introduced by a future change rather than one present today.
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}
