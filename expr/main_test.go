package expr_test

import (
	"testing"

	"go.uber.org/goleak"
)

// TestMain runs the package's goroutine-leak check. The acceptance fixture
// starts a Consumer on its own goroutine and joins it before returning; this is
// the package-wide backstop proving it, and every future provider test, leaves
// nothing running.
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}
