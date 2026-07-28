package channel_test

import (
	"testing"

	"go.uber.org/goleak"
)

// TestMain runs the package's goroutine-leak check. It matters most for the
// end-to-end tests here (queuechannel_e2e_test.go, pubsub_integration_test.go),
// which start a full endpoint.Consumer — poller, worker pool and sweep loop —
// and must join every one of those goroutines on ctx cancel.
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}
