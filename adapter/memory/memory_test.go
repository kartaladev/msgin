package memory_test

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	"github.com/kartaladev/msgin"
	"github.com/kartaladev/msgin/adapter/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
)

func TestMain(m *testing.M) { goleak.VerifyTestMain(m) }

func TestBroker_SendThenStreamDelivers(t *testing.T) {
	b := memory.New(memory.WithBuffer(4))

	require.NoError(t, b.Send(t.Context(), msgin.New[any]("hello", msgin.WithID("m1"))))

	out := make(chan msgin.Delivery, 1)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- b.Stream(ctx, out) }()

	d := <-out
	assert.Equal(t, "hello", d.Msg.Payload())
	assert.Equal(t, "m1", d.Msg.ID())
	require.NoError(t, d.Ack(t.Context()))

	cancel()
	<-done // ensure the Stream goroutine has exited before the test returns (goleak safety).
}

// TestBroker_WithBufferNegativeClamped closes the no-panic-on-caller-input
// gap: WithBuffer(n) with n < 0 must not panic (make would panic on a
// negative capacity) and must behave as an unbuffered (synchronous) broker
// instead.
func TestBroker_WithBufferNegativeClamped(t *testing.T) {
	t.Parallel()

	assert.NotPanics(t, func() {
		b := memory.New(memory.WithBuffer(-1))

		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()

		out := make(chan msgin.Delivery, 1)
		done := make(chan error, 1)
		go func() { done <- b.Stream(ctx, out) }()

		require.NoError(t, b.Send(t.Context(), msgin.New[any]("hello", msgin.WithID("m1"))))

		d := <-out
		assert.Equal(t, "hello", d.Msg.Payload())
		require.NoError(t, d.Ack(t.Context()))

		cancel()
		<-done // ensure the Stream goroutine has exited before the test returns (goleak safety).
	})
}

func TestBroker_EmitsLiveValue(t *testing.T) {
	var _ msgin.LiveValueSource = memory.New()
	assert.True(t, memory.New().EmitsLiveValue())
}

// TestBroker_SendCtxDone closes a hot-path coverage gap: Send must return
// ctx.Err() when the context is already done and no receiver is draining the
// (unbuffered, default) channel, so the send can never proceed. This is
// deterministic: with no reader on b.ch, the channel-send case of Send's
// select is never ready, so only the ctx.Done() case can fire.
func TestBroker_SendCtxDone(t *testing.T) {
	t.Parallel()

	b := memory.New() // unbuffered, no reader
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	err := b.Send(ctx, msgin.New[any]("unreached"))

	require.ErrorIs(t, err, context.Canceled)
}

// TestBroker_StreamCtxDoneWhileDelivering closes a hot-path coverage gap:
// Stream's inner select must return ctx.Err() when ctx is cancelled while
// blocked trying to hand a pulled message to out. The message is sent first
// (buffered), so Stream pulls it from b.ch and then blocks on the unbuffered,
// unread out channel; cancelling ctx at that point must unblock Stream via
// the ctx.Done() case rather than hang.
//
// This runs inside a synctest bubble so reaching the blocked-delivery state
// is deterministic: synctest.Wait returns only once every goroutine in the
// bubble is durably blocked, which — with out unbuffered and unread — can
// only be the Stream goroutine parked on the inner `out <- d` select. That
// replaces a wall-clock sleep, which could race the outer select instead of
// exercising the inner ctx.Done() branch under test.
func TestBroker_StreamCtxDoneWhileDelivering(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		b := memory.New(memory.WithBuffer(1))
		require.NoError(t, b.Send(t.Context(), msgin.New[any]("blocked", msgin.WithID("m-block"))))

		out := make(chan msgin.Delivery) // unbuffered, no reader — forces Stream to block delivering.
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()

		errCh := make(chan error, 1)
		go func() { errCh <- b.Stream(ctx, out) }()

		// Wait until the Stream goroutine has pulled "blocked" from b.ch and
		// is durably blocked on the unbuffered, unread out channel — i.e. on
		// the inner select's `out <- d` case.
		synctest.Wait()

		cancel()

		err := <-errCh
		assert.ErrorIs(t, err, context.Canceled)

		// Let the now-exited Stream goroutine settle before the bubble ends
		// (synctest requires every bubble goroutine to finish, and this keeps
		// goleak clean too).
		synctest.Wait()
	})
}

// TestBroker_Nack exercises both branches of the Nack requeue path: requeue
// re-enqueues the message via Send so it is redelivered on the next Stream
// pull, while a non-requeue Nack is a no-op (at-most-once — the message is
// dropped).
func TestBroker_Nack(t *testing.T) {
	t.Parallel()

	type testCase struct {
		name    string
		requeue bool
		assert  func(t *testing.T, out <-chan msgin.Delivery, nackErr error)
	}

	cases := []testCase{
		{
			name:    "requeue true redelivers the message",
			requeue: true,
			assert: func(t *testing.T, out <-chan msgin.Delivery, nackErr error) {
				require.NoError(t, nackErr)
				select {
				case redelivered := <-out:
					assert.Equal(t, "payload", redelivered.Msg.Payload())
				case <-time.After(time.Second):
					t.Fatal("expected redelivered message, got none")
				}
			},
		},
		{
			name:    "requeue false is a no-op",
			requeue: false,
			assert: func(t *testing.T, out <-chan msgin.Delivery, nackErr error) {
				require.NoError(t, nackErr)
				select {
				case d := <-out:
					t.Fatalf("expected no redelivery, got %v", d.Msg.Payload())
				case <-time.After(100 * time.Millisecond):
					// expected: no redelivery.
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			b := memory.New(memory.WithBuffer(1))
			require.NoError(t, b.Send(t.Context(), msgin.New[any]("payload", msgin.WithID(tc.name))))

			out := make(chan msgin.Delivery, 1)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()

			done := make(chan struct{})
			go func() {
				_ = b.Stream(ctx, out)
				close(done)
			}()

			d := <-out
			nackErr := d.Nack(t.Context(), tc.requeue, 0)

			tc.assert(t, out, nackErr)

			cancel()
			<-done // ensure the Stream goroutine has exited before the subtest returns (goleak safety).
		})
	}
}
