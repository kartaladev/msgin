package msgin_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kartaladev/msgin"
	"github.com/kartaladev/msgin/adapter/memory"
)

// A consumer whose handler publishes each message to a topic fans it out to all
// subscribers, inheriting the runtime (retry/DLQ/flow-control/shutdown).
func TestPubSub_DrivesOffConsumer(t *testing.T) {
	src := memory.New(memory.WithBuffer(4))
	ps := msgin.NewPubSub()

	var seenA, seenB atomic.Int64
	_, err := ps.Subscribe("events", msgin.HandlerFunc(func(context.Context, msgin.Message[any]) error { seenA.Add(1); return nil }))
	require.NoError(t, err)
	_, err = ps.Subscribe("events", msgin.HandlerFunc(func(context.Context, msgin.Message[any]) error { seenB.Add(1); return nil }))
	require.NoError(t, err)

	consumer, err := msgin.NewConsumer[any](src,
		func(ctx context.Context, m msgin.Message[any]) error { return ps.Publish(ctx, "events", m) },
		msgin.WithShutdownTimeout[any](2*time.Second),
	)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- consumer.Run(ctx) }()

	require.NoError(t, src.Send(ctx, msgin.New[any]("e-1")))

	require.Eventually(t, func() bool { return seenA.Load() == 1 && seenB.Load() == 1 }, 2*time.Second, 5*time.Millisecond)

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("consumer did not shut down")
	}
	// goleak (root TestMain) verifies no goroutine leak.
}

// Subscribe/Cancel racing a Publish must be race-clean and never panic.
func TestPubSub_ConcurrentSubscribeCancelDuringPublish(t *testing.T) {
	ps := msgin.NewPublishSubscribeChannel()
	var wg sync.WaitGroup
	stop := make(chan struct{})

	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				_ = ps.Send(t.Context(), msgin.New[any](1))
			}
		}
	}()
	for i := 0; i < 50; i++ {
		sub, err := ps.Subscribe(msgin.HandlerFunc(func(context.Context, msgin.Message[any]) error { return nil }))
		require.NoError(t, err)
		sub.Cancel()
	}
	close(stop)
	wg.Wait()
}

// F1 regression: a Subscribe to a topic racing the last-subscriber Cancel of the
// SAME topic must not orphan the new subscriber (the registry-level TOCTOU the
// bare-channel test above cannot reach). Deterministic-pass under the fix
// (Subscribe holds p.mu across ch.Subscribe); flaky-fails without it.
func TestPubSub_SubscribeRacesLastCancel(t *testing.T) {
	for i := 0; i < 200; i++ {
		ps := msgin.NewPubSub()
		sub1, err := ps.Subscribe("t", msgin.HandlerFunc(func(context.Context, msgin.Message[any]) error { return nil }))
		require.NoError(t, err)

		var got atomic.Int64
		var wg sync.WaitGroup
		wg.Add(2)
		go func() { defer wg.Done(); sub1.Cancel() }()
		go func() {
			defer wg.Done()
			_, err := ps.Subscribe("t", msgin.HandlerFunc(func(context.Context, msgin.Message[any]) error { got.Add(1); return nil }))
			assert.NoError(t, err)
		}()
		wg.Wait()

		require.NoError(t, ps.Publish(t.Context(), "t", msgin.New[any](1)))
		assert.Equal(t, int64(1), got.Load(), "iteration %d: surviving subscriber missed the publish (F1 TOCTOU)", i)
		assert.Equal(t, 1, ps.TopicCount())
	}
}
