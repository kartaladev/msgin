package sql_test

import (
	"context"
	"testing"
	"time"

	msgin "github.com/kartaladev/msgin"
)

// runConsumerUntil runs consumer until done is signalled (or the deadline),
// then cancels and returns Run's error. It joins the Run goroutine so no
// goroutine outlives the test. Shared by SourceSuite and OutboundSuite so the
// produce/consume round-trip test can drive the same real-runtime consumer
// loop the Source suite already exercises.
func runConsumerUntil(t *testing.T, ctx context.Context, consumer msgin.Consumer[string], done <-chan struct{}, deadline time.Duration) error {
	t.Helper()
	runCtx, cancel := context.WithCancel(ctx)
	errCh := make(chan error, 1)
	go func() { errCh <- consumer.Run(runCtx) }()

	select {
	case <-done:
	case <-time.After(deadline):
		cancel()
		<-errCh
		t.Fatal("timed out waiting for the consumer to process the expected messages")
		return nil
	}
	cancel()
	return <-errCh
}
