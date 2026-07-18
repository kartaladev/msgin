package msgin_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kartaladev/msgin"
)

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
			dc := msgin.NewDirectChannel()
			var got []msgin.Message[any]
			require.NoError(t, dc.Subscribe(msgin.HandlerFunc(func(_ context.Context, m msgin.Message[any]) error {
				got = append(got, m)
				return tc.handlerErr
			})))
			tc.assert(t, dc.Send(t.Context(), msgin.New[any]("hello")), got)
		})
	}
}

func TestDirectChannel_Errors(t *testing.T) {
	tests := []struct {
		name   string
		run    func(t *testing.T) error
		assert func(t *testing.T, err error)
	}{
		{
			name: "subscribe twice is ErrChannelSubscribed",
			run: func(t *testing.T) error {
				dc := msgin.NewDirectChannel()
				_ = dc.Subscribe(msgin.HandlerFunc(func(context.Context, msgin.Message[any]) error { return nil }))
				return dc.Subscribe(msgin.HandlerFunc(func(context.Context, msgin.Message[any]) error { return nil }))
			},
			assert: func(t *testing.T, err error) { assert.ErrorIs(t, err, msgin.ErrChannelSubscribed) },
		},
		{
			name:   "subscribe nil handler is ErrNilHandler",
			run:    func(t *testing.T) error { return msgin.NewDirectChannel().Subscribe(nil) },
			assert: func(t *testing.T, err error) { assert.ErrorIs(t, err, msgin.ErrNilHandler) },
		},
		{
			name:   "send with no subscriber is ErrNoSubscriber",
			run:    func(t *testing.T) error { return msgin.NewDirectChannel().Send(t.Context(), msgin.New[any](1)) },
			assert: func(t *testing.T, err error) { assert.ErrorIs(t, err, msgin.ErrNoSubscriber) },
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) { tc.assert(t, tc.run(t)) })
	}
}
