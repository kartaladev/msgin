package routing_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kartaladev/msgin"
	"github.com/kartaladev/msgin/routing"
)

// TestDeterministicEndpointFaultsAreClassified covers Spec 014 §2.1 row 6
// (decision D-M): a routing endpoint constructed with a nil function returns
// Permanent(ErrNilFunc) named with its position, while ErrNoRoute — evaluated
// per message against a caller-supplied pick — deliberately stays transient.
func TestDeterministicEndpointFaultsAreClassified(t *testing.T) {
	t.Parallel()

	discard := msgin.HandlerFunc(func(context.Context, msgin.Message[any]) error { return nil })

	tests := []struct {
		name    string
		handler func() msgin.MessageHandler
		assert  func(t *testing.T, err error)
	}{
		{
			name: "Filter(nil) is a permanent ErrNilFunc naming its position",
			handler: func() msgin.MessageHandler {
				return routing.Filter[int](nil)(discard)
			},
			assert: func(t *testing.T, err error) {
				require.ErrorIs(t, err, msgin.ErrNilFunc)
				assert.True(t, msgin.IsPermanent(err), "must be permanent")
				assert.Contains(t, err.Error(), "routing.Filter: nil pred")
			},
		},
		{
			name: "Split(nil) is a permanent ErrNilFunc naming its position",
			handler: func() msgin.MessageHandler {
				return routing.Split[int, int](nil)(discard)
			},
			assert: func(t *testing.T, err error) {
				require.ErrorIs(t, err, msgin.ErrNilFunc)
				assert.True(t, msgin.IsPermanent(err), "must be permanent")
				assert.Contains(t, err.Error(), "routing.Split: nil fn")
			},
		},
		{
			name: "NewRouter(nil).Handle is a permanent ErrNilFunc naming its position",
			handler: func() msgin.MessageHandler {
				return routing.NewRouter(nil)
			},
			assert: func(t *testing.T, err error) {
				require.ErrorIs(t, err, msgin.ErrNilFunc)
				assert.True(t, msgin.IsPermanent(err), "must be permanent")
				assert.Contains(t, err.Error(), "routing.Router.Handle: nil pick")
			},
		},
		{
			// The load-bearing negative: pick is caller-supplied and evaluated per
			// message, so an unroutable message may be routable after a config
			// reload. ErrNoRoute must stay bare and transient (ADR 0029 §5.0b).
			name: "ErrNoRoute stays TRANSIENT — deliberate, not an omission",
			handler: func() msgin.MessageHandler {
				return routing.NewRouter(func(context.Context, msgin.Message[any]) (msgin.MessageChannel, error) {
					return nil, nil
				})
			},
			assert: func(t *testing.T, err error) {
				require.ErrorIs(t, err, msgin.ErrNoRoute)
				assert.False(t, msgin.IsPermanent(err), "ErrNoRoute must NOT be permanent")
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tc.assert(t, tc.handler().Handle(t.Context(), msgin.New[any](1)))
		})
	}
}
