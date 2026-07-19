package msgin_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/kartaladev/msgin"
	"github.com/kartaladev/msgin/adapter/memory"
	"github.com/stretchr/testify/require"
)

// TestQueueChannel_EndToEnd proves the full round-trip: a Producer sends typed
// messages into a memory.QueueStore-backed QueueChannel, and a Consumer (via the
// runtime's Poller and worker pool) drains and handles every one, with
// goleak-clean shutdown on ctx cancel.
func TestQueueChannel_EndToEnd(t *testing.T) {
	store, err := memory.NewQueueStore()
	require.NoError(t, err)
	qc, err := msgin.NewQueueChannel(store)
	require.NoError(t, err)

	prod, err := msgin.NewProducer[string](qc)
	require.NoError(t, err)
	for _, s := range []string{"x", "y", "z"} {
		require.NoError(t, prod.Send(t.Context(), msgin.New(s))) // Send takes Message[string]
	}

	var mu sync.Mutex
	var got []string
	h := func(_ context.Context, m msgin.Message[string]) error { // converts to Handler[string]
		mu.Lock()
		got = append(got, m.Payload())
		mu.Unlock()
		return nil
	}
	cons, err := msgin.NewConsumer[string](qc, h, msgin.WithPollInterval[string](2*time.Millisecond))
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- cons.Run(ctx) }()
	require.Eventually(t, func() bool { mu.Lock(); defer mu.Unlock(); return len(got) == 3 }, time.Second, 5*time.Millisecond)
	cancel()
	require.ErrorIs(t, <-done, context.Canceled)
	require.ElementsMatch(t, []string{"x", "y", "z"}, got)
}
