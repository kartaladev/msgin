package msgin_test

import (
	"testing"

	"go.uber.org/goleak"
)

// TestMain runs the root package's goroutine-leak check. The pattern core
// starts no goroutine on its own — the endpoint runtime does — so this is the
// backstop that keeps a future core change from leaking one unnoticed, and it
// complements the per-test goleak.VerifyNone(t) calls in
// groupstore_conformance_test.go.
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}
