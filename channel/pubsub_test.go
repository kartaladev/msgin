package channel_test

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kartaladev/msgin"
	"github.com/kartaladev/msgin/channel"
)

func TestPublishSubscribeChannel_FanOut(t *testing.T) {
	tests := []struct {
		name   string
		policy channel.FanOutPolicy
		errs   []error // one per subscriber, nil = success
		assert func(t *testing.T, sendErr error, got []string)
	}{
		{
			name: "fans out to all subscribers in registration order",
			errs: []error{nil, nil, nil},
			assert: func(t *testing.T, sendErr error, got []string) {
				require.NoError(t, sendErr)
				assert.Equal(t, []string{"a", "b", "c"}, got)
			},
		},
		{
			name: "all-succeed: a subscriber error is joined, others still invoked",
			errs: []error{nil, errors.New("boom"), nil},
			assert: func(t *testing.T, sendErr error, got []string) {
				assert.ErrorContains(t, sendErr, "boom")
				assert.Equal(t, []string{"a", "b", "c"}, got) // every subscriber still ran
			},
		},
		{
			name:   "best-effort: a subscriber error is swallowed, Send returns nil",
			policy: channel.FanOutBestEffort,
			errs:   []error{nil, errors.New("boom"), nil},
			assert: func(t *testing.T, sendErr error, got []string) {
				require.NoError(t, sendErr)
				assert.Equal(t, []string{"a", "b", "c"}, got)
			},
		},
		{
			name: "no subscribers is a no-op",
			errs: nil,
			assert: func(t *testing.T, sendErr error, got []string) {
				require.NoError(t, sendErr)
				assert.Empty(t, got)
			},
		},
	}
	tags := []string{"a", "b", "c"}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ps := channel.NewPublishSubscribeChannel(channel.WithFanOut(tc.policy))
			var got []string
			for i, e := range tc.errs {
				tag, e := tags[i], e
				_, err := ps.Subscribe(msgin.HandlerFunc(func(context.Context, msgin.Message[any]) error {
					got = append(got, tag)
					return e
				}))
				require.NoError(t, err)
			}
			tc.assert(t, ps.Send(t.Context(), msgin.New[any]("x")), got)
		})
	}
}

func TestPublishSubscribeChannel_SubscribeAndCancel(t *testing.T) {
	t.Run("nil handler is ErrNilHandler", func(t *testing.T) {
		_, err := channel.NewPublishSubscribeChannel().Subscribe(nil)
		assert.ErrorIs(t, err, msgin.ErrNilHandler)
	})
	t.Run("cancel removes the subscriber and is idempotent", func(t *testing.T) {
		ps := channel.NewPublishSubscribeChannel()
		var count int
		sub, err := ps.Subscribe(msgin.HandlerFunc(func(context.Context, msgin.Message[any]) error { count++; return nil }))
		require.NoError(t, err)
		require.NoError(t, ps.Send(t.Context(), msgin.New[any](1)))
		assert.Equal(t, 1, count)
		sub.Cancel()
		sub.Cancel() // idempotent: no panic, no double-remove
		require.NoError(t, ps.Send(t.Context(), msgin.New[any](2)))
		assert.Equal(t, 1, count) // cancelled: not invoked again
	})
}

// TestPublishSubscribeChannel_SingleSubscriber pins decision D-F: the opt-in
// exclusivity guard, which is OFF by default so fan-out stays the pattern's
// default behavior.
func TestPublishSubscribeChannel_SingleSubscriber(t *testing.T) {
	tests := []struct {
		name   string
		opts   []channel.PubSubOption
		run    func(t *testing.T, ps *channel.PublishSubscribeChannel) (msgin.Subscription, error)
		assert func(t *testing.T, sub msgin.Subscription, err error)
	}{
		{
			name: "option off: a second subscriber is accepted (fan-out is the default)",
			run: func(t *testing.T, ps *channel.PublishSubscribeChannel) (msgin.Subscription, error) {
				_, err := ps.Subscribe(noopHandler())
				require.NoError(t, err)
				return ps.Subscribe(noopHandler())
			},
			assert: func(t *testing.T, sub msgin.Subscription, err error) {
				require.NoError(t, err)
				assert.NotNil(t, sub)
			},
		},
		{
			name: "option on: the first subscriber is accepted",
			opts: []channel.PubSubOption{channel.WithSingleSubscriber()},
			run: func(t *testing.T, ps *channel.PublishSubscribeChannel) (msgin.Subscription, error) {
				return ps.Subscribe(noopHandler())
			},
			assert: func(t *testing.T, sub msgin.Subscription, err error) {
				require.NoError(t, err)
				assert.NotNil(t, sub)
			},
		},
		{
			name: "option on: a second subscriber is ErrChannelSubscribed",
			opts: []channel.PubSubOption{channel.WithSingleSubscriber()},
			run: func(t *testing.T, ps *channel.PublishSubscribeChannel) (msgin.Subscription, error) {
				_, err := ps.Subscribe(noopHandler())
				require.NoError(t, err)
				return ps.Subscribe(noopHandler())
			},
			assert: func(t *testing.T, sub msgin.Subscription, err error) {
				assert.ErrorIs(t, err, msgin.ErrChannelSubscribed)
				assert.Nil(t, sub, "the error path must return no handle")
			},
		},
		{
			name: "option on: cancelling frees the slot for a new subscriber",
			opts: []channel.PubSubOption{channel.WithSingleSubscriber()},
			run: func(t *testing.T, ps *channel.PublishSubscribeChannel) (msgin.Subscription, error) {
				first, err := ps.Subscribe(noopHandler())
				require.NoError(t, err)
				first.Cancel()
				return ps.Subscribe(noopHandler())
			},
			assert: func(t *testing.T, sub msgin.Subscription, err error) {
				require.NoError(t, err)
				assert.NotNil(t, sub)
			},
		},
		{
			name: "option on: a nil handler is still ErrNilHandler",
			opts: []channel.PubSubOption{channel.WithSingleSubscriber()},
			run: func(t *testing.T, ps *channel.PublishSubscribeChannel) (msgin.Subscription, error) {
				return ps.Subscribe(nil)
			},
			assert: func(t *testing.T, sub msgin.Subscription, err error) {
				assert.ErrorIs(t, err, msgin.ErrNilHandler)
				assert.Nil(t, sub)
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sub, err := tc.run(t, channel.NewPublishSubscribeChannel(tc.opts...))
			tc.assert(t, sub, err)
		})
	}
}

// TestPubSub_SingleSubscriberPropagatesToTopics proves the registry inherits the
// option, so its per-topic channels reject a second subscriber — and that the
// now-reachable error return leaves the live topic untouched.
func TestPubSub_SingleSubscriberPropagatesToTopics(t *testing.T) {
	ps := channel.NewPubSub(channel.WithSingleSubscriber())

	first, err := ps.Subscribe("orders", noopHandler())
	require.NoError(t, err)
	require.NotNil(t, first)
	assert.Equal(t, 1, ps.TopicCount())

	second, err := ps.Subscribe("orders", noopHandler())
	assert.ErrorIs(t, err, msgin.ErrChannelSubscribed)
	assert.Nil(t, second)
	assert.Equal(t, 1, ps.TopicCount(), "the rejected subscribe must not disturb the live topic")

	first.Cancel()
	assert.Equal(t, 0, ps.TopicCount(), "drop-on-empty still applies")
}

func TestPublishSubscribeChannel_SubscriberPanicIsIsolated(t *testing.T) {
	tests := []struct {
		name   string
		policy channel.FanOutPolicy
		assert func(t *testing.T, sendErr error, laterRan bool)
	}{
		{
			name: "all-succeed: panic is a transient error, later subscribers still run",
			assert: func(t *testing.T, sendErr error, laterRan bool) {
				assert.ErrorIs(t, sendErr, msgin.ErrHandlerPanic)
				assert.True(t, laterRan)
			},
		},
		{
			name:   "best-effort: panic is logged, Send returns nil, later subscribers still run",
			policy: channel.FanOutBestEffort,
			assert: func(t *testing.T, sendErr error, laterRan bool) {
				require.NoError(t, sendErr)
				assert.True(t, laterRan)
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ps := channel.NewPublishSubscribeChannel(channel.WithFanOut(tc.policy))
			_, err := ps.Subscribe(msgin.HandlerFunc(func(context.Context, msgin.Message[any]) error { panic("boom") }))
			require.NoError(t, err)
			var laterRan bool
			_, err = ps.Subscribe(msgin.HandlerFunc(func(context.Context, msgin.Message[any]) error { laterRan = true; return nil }))
			require.NoError(t, err)
			tc.assert(t, ps.Send(t.Context(), msgin.New[any](1)), laterRan)
		})
	}
}

func TestPublishSubscribeChannel_PermanentErrorPropagates(t *testing.T) {
	// Unit-settlement (F2): a subscriber's permanent error makes the joined fan-out
	// permanent (errors.Join propagates it), even mixed with a transient failure —
	// so a Consumer-driven publish diverts the whole message to the invalid sink.
	ps := channel.NewPublishSubscribeChannel()
	_, err := ps.Subscribe(msgin.HandlerFunc(func(context.Context, msgin.Message[any]) error { return msgin.ErrPayloadType }))
	require.NoError(t, err)
	_, err = ps.Subscribe(msgin.HandlerFunc(func(context.Context, msgin.Message[any]) error { return errors.New("transient") }))
	require.NoError(t, err)
	assert.ErrorIs(t, ps.Send(t.Context(), msgin.New[any](1)), msgin.ErrPayloadType)
}

func TestPublishSubscribeChannel_BestEffortLogsToInjectedLogger(t *testing.T) {
	var buf strings.Builder
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))
	ps := channel.NewPublishSubscribeChannel(channel.WithFanOut(channel.FanOutBestEffort), channel.WithPubSubLogger(logger))
	_, err := ps.Subscribe(msgin.HandlerFunc(func(context.Context, msgin.Message[any]) error { return errors.New("sub-fail") }))
	require.NoError(t, err)
	require.NoError(t, ps.Send(t.Context(), msgin.New[any](1)))
	assert.Contains(t, buf.String(), "sub-fail")
	_ = channel.NewPublishSubscribeChannel(channel.WithPubSubLogger(nil)) // nil is a no-op (keeps default discard logger)
}

func TestPublishSubscribeChannel_IsOutboundAdapter(t *testing.T) {
	var _ msgin.OutboundAdapter = channel.NewPublishSubscribeChannel() // compiles => Send satisfies the SPI
}
