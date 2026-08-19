package routing_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kartaladev/msgin"
	"github.com/kartaladev/msgin/channel"
	"github.com/kartaladev/msgin/routing"
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
			target := channel.NewDirectChannel()
			_, subErr := target.Subscribe(msgin.HandlerFunc(func(context.Context, msgin.Message[any]) error { routed = true; return nil }))
			require.NoError(t, subErr)
			defCh := channel.NewDirectChannel()
			_, subErr = defCh.Subscribe(msgin.HandlerFunc(func(context.Context, msgin.Message[any]) error { def = true; return nil }))
			require.NoError(t, subErr)
			var opts []routing.RouterOption
			if tc.useDef {
				opts = append(opts, routing.WithDefaultChannel(defCh))
			}
			r := routing.NewRouter(tc.pick(target, defCh), opts...)
			tc.assert(t, r.Handle(t.Context(), msgin.New[any](1)), routed, def)
		})
	}
}

func TestRouter_NilPick(t *testing.T) {
	r := routing.NewRouter(nil)
	assert.ErrorIs(t, r.Handle(t.Context(), msgin.New[any](1)), msgin.ErrNilFunc)
}

// TestNewRouter_NilOptionElement proves a nil ELEMENT of opts is LATCHED at
// construction (NewRouter has no error return, so it is family R2) and
// reported by Handle as a PERMANENT ErrNilFunc naming the computed 0-based
// index (Spec 015 §3.2, ADR 0031 D-P/D-S/D-U). The last row pins D-V: the
// latch is checked at the TOP of Handle, above its own pick == nil check.
func TestNewRouter_NilOptionElement(t *testing.T) {
	pick := func(context.Context, msgin.Message[any]) (msgin.MessageChannel, error) {
		return channel.NewDirectChannel(), nil
	}

	tests := []struct {
		name   string
		router func() *routing.Router
		assert func(t *testing.T, err error)
	}{
		{
			name:   "nil element alone",
			router: func() *routing.Router { return routing.NewRouter(pick, nil) },
			assert: func(t *testing.T, err error) {
				require.ErrorIs(t, err, msgin.ErrNilFunc)
				assert.True(t, msgin.IsPermanent(err), "R2 nil-option error must be Permanent-wrapped")
				assert.Contains(t, err.Error(), "routing.NewRouter: nil option at index 0")
			},
		},
		{
			name: "nil element after a valid option asserts the COMPUTED index and the FULL position",
			router: func() *routing.Router {
				return routing.NewRouter(pick, routing.WithDefaultChannel(channel.NewDirectChannel()), nil)
			},
			assert: func(t *testing.T, err error) {
				require.ErrorIs(t, err, msgin.ErrNilFunc)
				assert.True(t, msgin.IsPermanent(err))
				assert.Contains(t, err.Error(), "routing.NewRouter: nil option at index 1")
			},
		},
		{
			// AC-3's R2 half: D-U's "latch only when unlatched" is what keeps
			// first-nil-wins; an implementation latching the LAST nil passes
			// every other assertion here.
			name:   "first of two nils wins",
			router: func() *routing.Router { return routing.NewRouter(pick, nil, nil) },
			assert: func(t *testing.T, err error) {
				require.ErrorIs(t, err, msgin.ErrNilFunc)
				assert.Contains(t, err.Error(), "routing.NewRouter: nil option at index 0")
			},
		},
		{
			// D-V: the option fault happened at construction, so it is
			// chronologically earlier than the nil pick and is reported first.
			name:   "D-V: a nil option is reported BEFORE the nil pick",
			router: func() *routing.Router { return routing.NewRouter(nil, nil) },
			assert: func(t *testing.T, err error) {
				require.ErrorIs(t, err, msgin.ErrNilFunc)
				assert.True(t, msgin.IsPermanent(err))
				assert.Contains(t, err.Error(), "routing.NewRouter: nil option at index 0")
				assert.NotContains(t, err.Error(), "nil pick")
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tc.assert(t, tc.router().Handle(t.Context(), msgin.New[any](1)))
		})
	}
}
