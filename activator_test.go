package msgin_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kartaladev/msgin"
)

func TestActivate(t *testing.T) {
	tests := []struct {
		name   string
		svc    func(context.Context, msgin.Message[int]) (msgin.Message[string], error)
		input  any
		next   func(reply *msgin.Message[any]) msgin.MessageHandler
		assert func(t *testing.T, err error, reply msgin.Message[any])
	}{
		{
			name: "invokes the service and forwards the reply",
			svc: func(_ context.Context, m msgin.Message[int]) (msgin.Message[string], error) {
				return msgin.WithPayload(m, "ok"), nil
			},
			input: 1,
			next: func(r *msgin.Message[any]) msgin.MessageHandler {
				return msgin.HandlerFunc(func(_ context.Context, m msgin.Message[any]) error { *r = m; return nil })
			},
			assert: func(t *testing.T, err error, reply msgin.Message[any]) {
				require.NoError(t, err)
				assert.Equal(t, "ok", reply.Payload())
			},
		},
		{
			name: "service error propagates without forwarding",
			svc: func(context.Context, msgin.Message[int]) (msgin.Message[string], error) {
				return msgin.Message[string]{}, errors.New("boom")
			},
			input: 1,
			next: func(r *msgin.Message[any]) msgin.MessageHandler {
				return msgin.HandlerFunc(func(_ context.Context, m msgin.Message[any]) error { *r = m; return nil })
			},
			assert: func(t *testing.T, err error, reply msgin.Message[any]) {
				assert.ErrorContains(t, err, "boom")
				assert.Nil(t, reply.Payload())
			},
		},
		{
			name: "wrong payload type is ErrPayloadType",
			svc: func(_ context.Context, m msgin.Message[int]) (msgin.Message[string], error) {
				return msgin.WithPayload(m, ""), nil
			},
			input: "nope",
			next: func(*msgin.Message[any]) msgin.MessageHandler {
				return msgin.HandlerFunc(func(context.Context, msgin.Message[any]) error { return nil })
			},
			assert: func(t *testing.T, err error, _ msgin.Message[any]) { assert.ErrorIs(t, err, msgin.ErrPayloadType) },
		},
		{
			name: "downstream error propagates",
			svc: func(_ context.Context, m msgin.Message[int]) (msgin.Message[string], error) {
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
			var reply msgin.Message[any]
			step := msgin.Activate(tc.svc)
			tc.assert(t, step(tc.next(&reply)).Handle(t.Context(), msgin.New[any](tc.input)), reply)
		})
	}
}

func TestConsume(t *testing.T) {
	tests := []struct {
		name   string
		svc    func(seen *int) func(context.Context, msgin.Message[int]) error
		input  any
		assert func(t *testing.T, err error, seen int, forwarded bool)
	}{
		{
			name: "runs the side effect and does not forward",
			svc: func(seen *int) func(context.Context, msgin.Message[int]) error {
				return func(_ context.Context, m msgin.Message[int]) error { *seen = m.Payload(); return nil }
			},
			input: 7,
			assert: func(t *testing.T, err error, seen int, forwarded bool) {
				require.NoError(t, err)
				assert.Equal(t, 7, seen)
				assert.False(t, forwarded)
			},
		},
		{
			name: "service error propagates",
			svc: func(*int) func(context.Context, msgin.Message[int]) error {
				return func(context.Context, msgin.Message[int]) error { return errors.New("boom") }
			},
			input:  7,
			assert: func(t *testing.T, err error, _ int, _ bool) { assert.ErrorContains(t, err, "boom") },
		},
		{
			name: "wrong payload type is ErrPayloadType and does not forward",
			svc: func(seen *int) func(context.Context, msgin.Message[int]) error {
				return func(_ context.Context, m msgin.Message[int]) error { *seen = m.Payload(); return nil }
			},
			input: "nope",
			assert: func(t *testing.T, err error, seen int, forwarded bool) {
				assert.ErrorIs(t, err, msgin.ErrPayloadType)
				assert.Equal(t, 0, seen)
				assert.False(t, forwarded)
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var seen int
			var forwarded bool
			next := msgin.HandlerFunc(func(context.Context, msgin.Message[any]) error { forwarded = true; return nil })
			tc.assert(t, msgin.Consume(tc.svc(&seen))(next).Handle(t.Context(), msgin.New[any](tc.input)), seen, forwarded)
		})
	}
}

func TestActivator_NilFn(t *testing.T) {
	next := msgin.HandlerFunc(func(context.Context, msgin.Message[any]) error { return nil })
	assert.ErrorIs(t, msgin.Activate[int, int](nil)(next).Handle(t.Context(), msgin.New[any](1)), msgin.ErrNilFunc)
	assert.ErrorIs(t, msgin.Consume[int](nil)(next).Handle(t.Context(), msgin.New[any](1)), msgin.ErrNilFunc)
}
