package cron_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/jonboulle/clockwork"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kartaladev/msgin"
	"github.com/kartaladev/msgin/adapter/cron"
)

// handlerRecorder records every fire the consumer's handler observes, so the
// test can assert the payload arrived as the handler's own T (proving the
// factory(fire time.Time) T -> msgin.New[any] -> runtime -> Handler[T] round
// trip, not just that Source.Stream emits something).
type handlerRecorder struct {
	mu      sync.Mutex
	payload []string
}

func (r *handlerRecorder) record(p string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.payload = append(r.payload, p)
}

func (r *handlerRecorder) snapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.payload...)
}

// TestSource_ThroughNewConsumer drives a fake-clock cron Source through
// msgin.NewConsumer[T] + Run — the shape every real caller uses — instead of
// draining Source.Stream by hand. Proves: NewConsumer resolves the Source via
// LiveValueSource (no codec needed), the handler receives the fired payload
// typed as T, and shutdown is goleak-clean.
func TestSource_ThroughNewConsumer(t *testing.T) {
	epoch := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	clk := clockwork.NewFakeClockAt(epoch)

	src, err := cron.NewSource("@every 1h",
		func(fire time.Time) string { return "tick@" + fire.UTC().Format("15:04") },
		cron.WithClock(clk))
	require.NoError(t, err)

	rec := &handlerRecorder{}
	h := func(_ context.Context, m msgin.Message[string]) error {
		rec.record(m.Payload())
		return nil
	}

	c, err := msgin.NewConsumer[string](src, h) // cron Source ⇒ LiveValueSource ⇒ no codec
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- c.Run(ctx) }()

	require.NoError(t, clk.BlockUntilContext(ctx, 1))
	clk.Advance(time.Hour)

	require.Eventually(t, func() bool { return len(rec.snapshot()) == 1 }, time.Second, 5*time.Millisecond)
	assert.Equal(t, []string{"tick@01:00"}, rec.snapshot())

	cancel()
	assert.ErrorIs(t, <-done, context.Canceled) // goleak: Run's goroutine joined before TestMain checks
}
