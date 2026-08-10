package channel_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kartaladev/msgin"
	"github.com/kartaladev/msgin/channel"
)

// recordingHandler returns a handler that appends every message it sees to got
// and returns handlerErr.
func recordingHandler(got *[]msgin.Message[any], handlerErr error) msgin.MessageHandler {
	return msgin.HandlerFunc(func(_ context.Context, m msgin.Message[any]) error {
		*got = append(*got, m)
		return handlerErr
	})
}

// noopHandler is a handler that does nothing successfully.
func noopHandler() msgin.MessageHandler {
	return msgin.HandlerFunc(func(context.Context, msgin.Message[any]) error { return nil })
}

func TestDirectChannel_SendInvokesSubscriber(t *testing.T) {
	tests := []struct {
		name       string
		handlerErr error
		assert     func(t *testing.T, sendErr error, got []msgin.Message[any])
	}{
		{
			name:       "send invokes the subscribed handler synchronously",
			handlerErr: nil,
			assert: func(t *testing.T, sendErr error, got []msgin.Message[any]) {
				require.NoError(t, sendErr)
				require.Len(t, got, 1)
				assert.Equal(t, "hello", got[0].Payload())
			},
		},
		{
			name:       "send propagates the handler error",
			handlerErr: errors.New("boom"),
			assert: func(t *testing.T, sendErr error, _ []msgin.Message[any]) {
				assert.ErrorContains(t, sendErr, "boom")
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dc := channel.NewDirectChannel()
			var got []msgin.Message[any]
			sub, err := dc.Subscribe(recordingHandler(&got, tc.handlerErr))
			require.NoError(t, err)
			require.NotNil(t, sub)
			tc.assert(t, dc.Send(t.Context(), msgin.New[any]("hello")), got)
		})
	}
}

func TestDirectChannel_Errors(t *testing.T) {
	tests := []struct {
		name   string
		run    func(t *testing.T) (msgin.Subscription, error)
		assert func(t *testing.T, sub msgin.Subscription, err error)
	}{
		{
			name: "subscribe twice is ErrChannelSubscribed",
			run: func(t *testing.T) (msgin.Subscription, error) {
				dc := channel.NewDirectChannel()
				_, err := dc.Subscribe(noopHandler())
				require.NoError(t, err)
				return dc.Subscribe(noopHandler())
			},
			assert: func(t *testing.T, sub msgin.Subscription, err error) {
				assert.ErrorIs(t, err, msgin.ErrChannelSubscribed)
				assert.Nil(t, sub, "the error path must return no handle")
			},
		},
		{
			name: "subscribe nil handler is ErrNilHandler and no handle",
			run: func(t *testing.T) (msgin.Subscription, error) {
				return channel.NewDirectChannel().Subscribe(nil)
			},
			assert: func(t *testing.T, sub msgin.Subscription, err error) {
				assert.ErrorIs(t, err, msgin.ErrNilHandler)
				assert.Nil(t, sub, "the error path must return no handle")
			},
		},
		{
			name: "send with no subscriber is ErrNoSubscriber",
			run: func(t *testing.T) (msgin.Subscription, error) {
				return nil, channel.NewDirectChannel().Send(t.Context(), msgin.New[any](1))
			},
			assert: func(t *testing.T, _ msgin.Subscription, err error) {
				assert.ErrorIs(t, err, msgin.ErrNoSubscriber)
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sub, err := tc.run(t)
			tc.assert(t, sub, err)
		})
	}
}

// TestDirectChannel_SubscriptionLifecycle pins the ADR 0028 §7 semantics table
// for the Subscription handle DirectChannel.Subscribe now returns.
func TestDirectChannel_SubscriptionLifecycle(t *testing.T) {
	tests := []struct {
		name   string
		run    func(t *testing.T, dc *channel.DirectChannel, first msgin.Subscription) (delivered int, err error)
		assert func(t *testing.T, err error, delivered int)
	}{
		{
			name: "subscribe after cancel succeeds and the new handler receives",
			run: func(t *testing.T, dc *channel.DirectChannel, first msgin.Subscription) (int, error) {
				first.Cancel()
				delivered := 0
				sub, err := dc.Subscribe(msgin.HandlerFunc(func(context.Context, msgin.Message[any]) error {
					delivered++
					return nil
				}))
				require.NoError(t, err)
				require.NotNil(t, sub)
				sendErr := dc.Send(t.Context(), msgin.New[any]("after-cancel"))
				return delivered, sendErr
			},
			assert: func(t *testing.T, err error, delivered int) {
				require.NoError(t, err)
				assert.Equal(t, 1, delivered, "the resubscribed handler must receive the message")
			},
		},
		{
			name: "send between cancel and the next subscribe is ErrNoSubscriber",
			run: func(t *testing.T, dc *channel.DirectChannel, first msgin.Subscription) (int, error) {
				first.Cancel()
				return 0, dc.Send(t.Context(), msgin.New[any]("orphan"))
			},
			assert: func(t *testing.T, err error, _ int) {
				assert.ErrorIs(t, err, msgin.ErrNoSubscriber)
			},
		},
		{
			name: "cancel twice is idempotent and never panics",
			run: func(t *testing.T, dc *channel.DirectChannel, first msgin.Subscription) (int, error) {
				assert.NotPanics(t, func() {
					first.Cancel()
					first.Cancel()
				})
				return 0, dc.Send(t.Context(), msgin.New[any]("orphan"))
			},
			assert: func(t *testing.T, err error, _ int) {
				assert.ErrorIs(t, err, msgin.ErrNoSubscriber, "the slot stays released after a repeated Cancel")
			},
		},
		{
			name: "a stale handle's cancel does not evict the current subscriber",
			run: func(t *testing.T, dc *channel.DirectChannel, first msgin.Subscription) (int, error) {
				first.Cancel()
				delivered := 0
				_, err := dc.Subscribe(msgin.HandlerFunc(func(context.Context, msgin.Message[any]) error {
					delivered++
					return nil
				}))
				require.NoError(t, err)
				first.Cancel() // stale: must not touch the new subscription
				sendErr := dc.Send(t.Context(), msgin.New[any]("still-live"))
				return delivered, sendErr
			},
			assert: func(t *testing.T, err error, delivered int) {
				require.NoError(t, err)
				assert.Equal(t, 1, delivered)
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dc := channel.NewDirectChannel()
			var got []msgin.Message[any]
			first, err := dc.Subscribe(recordingHandler(&got, nil))
			require.NoError(t, err)
			delivered, sendErr := tc.run(t, dc, first)
			tc.assert(t, sendErr, delivered)
		})
	}
}

// TestDirectChannel_CancelDuringInFlightSend proves the ADR 0028 §7 race row: a
// Cancel issued while a handler is executing does not interrupt it — the
// in-flight Handle runs to completion — and only SUBSEQUENT dispatch is stopped.
func TestDirectChannel_CancelDuringInFlightSend(t *testing.T) {
	dc := channel.NewDirectChannel()
	entered := make(chan struct{})
	release := make(chan struct{})
	var completed bool

	sub, err := dc.Subscribe(msgin.HandlerFunc(func(context.Context, msgin.Message[any]) error {
		close(entered)
		<-release
		completed = true
		return nil
	}))
	require.NoError(t, err)

	var wg sync.WaitGroup
	wg.Add(1)
	var sendErr error
	go func() {
		defer wg.Done()
		sendErr = dc.Send(t.Context(), msgin.New[any]("in-flight"))
	}()

	<-entered
	sub.Cancel() // races the handler that is already running
	close(release)
	wg.Wait()

	require.NoError(t, sendErr)
	assert.True(t, completed, "the in-flight Handle must run to completion")
	assert.ErrorIs(t, dc.Send(t.Context(), msgin.New[any]("next")), msgin.ErrNoSubscriber,
		"Cancel must stop only subsequent dispatch")
}

// TestSingleSubscriber pins the ExclusiveSubscribable probe (ADR 0030 §2) for
// both in-tree channels directly, in the package that declares them: the
// endpoint-side truth table exercises the guard, not these two methods, so
// without this table nothing here covers them. The msgin.ExclusiveSubscribable
// field type is itself an assertion that both types satisfy the capability.
//
// DirectChannel is exclusive by TYPE (its Subscribe rejects a second
// subscriber); PublishSubscribeChannel reports exactly whether
// WithSingleSubscriber was passed — the equality that links decision D-F to
// decision D-J.
func TestSingleSubscriber(t *testing.T) {
	tests := []struct {
		name   string
		ch     msgin.ExclusiveSubscribable
		assert func(t *testing.T, single bool)
	}{
		{
			name: "DirectChannel is exclusive by type",
			ch:   channel.NewDirectChannel(),
			assert: func(t *testing.T, single bool) {
				assert.True(t, single,
					"DirectChannel.Subscribe returns ErrChannelSubscribed to a second subscriber")
			},
		},
		{
			name: "PublishSubscribeChannel with WithSingleSubscriber is exclusive",
			ch:   channel.NewPublishSubscribeChannel(channel.WithSingleSubscriber()),
			assert: func(t *testing.T, single bool) {
				assert.True(t, single, "WithSingleSubscriber is exactly what the probe reports")
			},
		},
		{
			name: "plain PublishSubscribeChannel is not exclusive",
			ch:   channel.NewPublishSubscribeChannel(),
			assert: func(t *testing.T, single bool) {
				assert.False(t, single,
					"fan-out to every subscriber is the default, so the policy permits other recipients")
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tc.assert(t, tc.ch.SingleSubscriber())
		})
	}
}
