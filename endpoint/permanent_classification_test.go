package endpoint_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kartaladev/msgin"
	"github.com/kartaladev/msgin/endpoint"
)

// TestNilServiceFaultsArePermanent covers Spec 014 §2.1 row 6 (decision D-M)
// for the two endpoint-package entry points: a Service Activator constructed
// with a nil service captures the nil at construction, so the fault cannot
// change for the message's lifetime and is Permanent, named with its position.
func TestNilServiceFaultsArePermanent(t *testing.T) {
	t.Parallel()

	discard := msgin.HandlerFunc(func(context.Context, msgin.Message[any]) error { return nil })

	tests := []struct {
		name   string
		step   msgin.Step
		assert func(t *testing.T, err error)
	}{
		{
			name: "Activate(nil) is a permanent ErrNilFunc naming its position",
			step: endpoint.Activate[int, int](nil),
			assert: func(t *testing.T, err error) {
				require.ErrorIs(t, err, msgin.ErrNilFunc)
				assert.True(t, msgin.IsPermanent(err), "must be permanent")
				assert.Contains(t, err.Error(), "endpoint.Activate: nil svc")
			},
		},
		{
			name: "Consume(nil) is a permanent ErrNilFunc naming its position",
			step: endpoint.Consume[int](nil),
			assert: func(t *testing.T, err error) {
				require.ErrorIs(t, err, msgin.ErrNilFunc)
				assert.True(t, msgin.IsPermanent(err), "must be permanent")
				assert.Contains(t, err.Error(), "endpoint.Consume: nil svc")
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tc.assert(t, tc.step(discard).Handle(t.Context(), msgin.New[any](1)))
		})
	}
}
