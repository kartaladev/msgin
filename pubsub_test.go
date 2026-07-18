package msgin_test

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kartaladev/msgin"
)

func TestPublishSubscribeChannel_FanOut(t *testing.T) {
	tests := []struct {
		name   string
		policy msgin.FanOutPolicy
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
			policy: msgin.FanOutBestEffort,
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
			ps := msgin.NewPublishSubscribeChannel(msgin.WithFanOut(tc.policy))
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
		_, err := msgin.NewPublishSubscribeChannel().Subscribe(nil)
		assert.ErrorIs(t, err, msgin.ErrNilHandler)
	})
	t.Run("cancel removes the subscriber and is idempotent", func(t *testing.T) {
		ps := msgin.NewPublishSubscribeChannel()
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

func TestPublishSubscribeChannel_SubscriberPanicIsIsolated(t *testing.T) {
	tests := []struct {
		name   string
		policy msgin.FanOutPolicy
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
			policy: msgin.FanOutBestEffort,
			assert: func(t *testing.T, sendErr error, laterRan bool) {
				require.NoError(t, sendErr)
				assert.True(t, laterRan)
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ps := msgin.NewPublishSubscribeChannel(msgin.WithFanOut(tc.policy))
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
	ps := msgin.NewPublishSubscribeChannel()
	_, err := ps.Subscribe(msgin.HandlerFunc(func(context.Context, msgin.Message[any]) error { return msgin.ErrPayloadType }))
	require.NoError(t, err)
	_, err = ps.Subscribe(msgin.HandlerFunc(func(context.Context, msgin.Message[any]) error { return errors.New("transient") }))
	require.NoError(t, err)
	assert.ErrorIs(t, ps.Send(t.Context(), msgin.New[any](1)), msgin.ErrPayloadType)
}

func TestPublishSubscribeChannel_BestEffortLogsToInjectedLogger(t *testing.T) {
	var buf strings.Builder
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))
	ps := msgin.NewPublishSubscribeChannel(msgin.WithFanOut(msgin.FanOutBestEffort), msgin.WithPubSubLogger(logger))
	_, err := ps.Subscribe(msgin.HandlerFunc(func(context.Context, msgin.Message[any]) error { return errors.New("sub-fail") }))
	require.NoError(t, err)
	require.NoError(t, ps.Send(t.Context(), msgin.New[any](1)))
	assert.Contains(t, buf.String(), "sub-fail")
	_ = msgin.NewPublishSubscribeChannel(msgin.WithPubSubLogger(nil)) // nil is a no-op (keeps default discard logger)
}

func TestPublishSubscribeChannel_IsOutboundAdapter(t *testing.T) {
	var _ msgin.OutboundAdapter = msgin.NewPublishSubscribeChannel() // compiles => Send satisfies the SPI
}
