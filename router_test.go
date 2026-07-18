package msgin_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kartaladev/msgin"
)

func TestRouter(t *testing.T) {
	tests := []struct {
		name   string
		pick   func(target, def msgin.MessageChannel) func(context.Context, msgin.Message[any]) (msgin.MessageChannel, error)
		useDef bool
		assert func(t *testing.T, err error, routed, def bool)
	}{
		{
			name: "resolved channel receives the message",
			pick: func(target, _ msgin.MessageChannel) func(context.Context, msgin.Message[any]) (msgin.MessageChannel, error) {
				return func(context.Context, msgin.Message[any]) (msgin.MessageChannel, error) { return target, nil }
			},
			assert: func(t *testing.T, err error, routed, def bool) {
				require.NoError(t, err)
				assert.True(t, routed)
				assert.False(t, def)
			},
		},
		{
			name: "nil channel with no default is ErrNoRoute",
			pick: func(msgin.MessageChannel, msgin.MessageChannel) func(context.Context, msgin.Message[any]) (msgin.MessageChannel, error) {
				return func(context.Context, msgin.Message[any]) (msgin.MessageChannel, error) { return nil, nil }
			},
			assert: func(t *testing.T, err error, _, _ bool) { assert.ErrorIs(t, err, msgin.ErrNoRoute) },
		},
		{
			name: "nil channel with default routes to default",
			pick: func(msgin.MessageChannel, msgin.MessageChannel) func(context.Context, msgin.Message[any]) (msgin.MessageChannel, error) {
				return func(context.Context, msgin.Message[any]) (msgin.MessageChannel, error) { return nil, nil }
			},
			useDef: true,
			assert: func(t *testing.T, err error, _, def bool) { require.NoError(t, err); assert.True(t, def) },
		},
		{
			name: "pick returning (chan, err) propagates err and ignores chan",
			pick: func(target, _ msgin.MessageChannel) func(context.Context, msgin.Message[any]) (msgin.MessageChannel, error) {
				return func(context.Context, msgin.Message[any]) (msgin.MessageChannel, error) {
					return target, errors.New("boom")
				}
			},
			assert: func(t *testing.T, err error, routed, _ bool) {
				assert.ErrorContains(t, err, "boom")
				assert.False(t, routed)
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var routed, def bool
			target := msgin.NewDirectChannel()
			_ = target.Subscribe(msgin.HandlerFunc(func(context.Context, msgin.Message[any]) error { routed = true; return nil }))
			defCh := msgin.NewDirectChannel()
			_ = defCh.Subscribe(msgin.HandlerFunc(func(context.Context, msgin.Message[any]) error { def = true; return nil }))
			var opts []msgin.RouterOption
			if tc.useDef {
				opts = append(opts, msgin.WithDefaultChannel(defCh))
			}
			r := msgin.NewRouter(tc.pick(target, defCh), opts...)
			tc.assert(t, r.Handle(t.Context(), msgin.New[any](1)), routed, def)
		})
	}
}

func TestRouter_NilPick(t *testing.T) {
	r := msgin.NewRouter(nil)
	assert.ErrorIs(t, r.Handle(t.Context(), msgin.New[any](1)), msgin.ErrNilFunc)
}
