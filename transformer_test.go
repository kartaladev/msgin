package msgin_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kartaladev/msgin"
)

func TestTransform(t *testing.T) {
	capture := func(got *msgin.Message[any]) msgin.MessageHandler {
		return msgin.HandlerFunc(func(_ context.Context, m msgin.Message[any]) error { *got = m; return nil })
	}
	tests := []struct {
		name   string
		fn     func(context.Context, msgin.Message[int]) (msgin.Message[string], error)
		input  any
		next   func(got *msgin.Message[any]) msgin.MessageHandler
		assert func(t *testing.T, err error, forwarded msgin.Message[any])
	}{
		{
			name: "maps A to B, forwards, preserves headers via WithPayload",
			fn: func(_ context.Context, m msgin.Message[int]) (msgin.Message[string], error) {
				return msgin.WithPayload(m, "n"), nil
			},
			input: 5,
			next:  capture,
			assert: func(t *testing.T, err error, forwarded msgin.Message[any]) {
				require.NoError(t, err)
				assert.Equal(t, "n", forwarded.Payload())
			},
		},
		{
			name: "wrong input payload type is ErrPayloadType",
			fn: func(_ context.Context, m msgin.Message[int]) (msgin.Message[string], error) {
				return msgin.WithPayload(m, ""), nil
			},
			input:  "not-int",
			next:   capture,
			assert: func(t *testing.T, err error, _ msgin.Message[any]) { assert.ErrorIs(t, err, msgin.ErrPayloadType) },
		},
		{
			name: "fn error propagates and nothing is forwarded",
			fn: func(context.Context, msgin.Message[int]) (msgin.Message[string], error) {
				return msgin.Message[string]{}, errors.New("boom")
			},
			input: 1,
			next:  capture,
			assert: func(t *testing.T, err error, forwarded msgin.Message[any]) {
				assert.ErrorContains(t, err, "boom")
				assert.Nil(t, forwarded.Payload())
			},
		},
		{
			name: "downstream (next) error propagates",
			fn: func(_ context.Context, m msgin.Message[int]) (msgin.Message[string], error) {
				return msgin.WithPayload(m, "ok"), nil
			},
			input: 1,
			next: func(*msgin.Message[any]) msgin.MessageHandler {
				return msgin.HandlerFunc(func(context.Context, msgin.Message[any]) error { return errors.New("downstream") })
			},
			assert: func(t *testing.T, err error, _ msgin.Message[any]) { assert.ErrorContains(t, err, "downstream") },
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var forwarded msgin.Message[any]
			step := msgin.Transform(tc.fn)
			tc.assert(t, step(tc.next(&forwarded)).Handle(t.Context(), msgin.New[any](tc.input)), forwarded)
		})
	}
}

func TestTransform_NilFn(t *testing.T) {
	step := msgin.Transform[int, int](nil)
	next := msgin.HandlerFunc(func(context.Context, msgin.Message[any]) error { return nil })
	assert.ErrorIs(t, step(next).Handle(t.Context(), msgin.New[any](1)), msgin.ErrNilFunc)
}
