package transform_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kartaladev/msgin"
	"github.com/kartaladev/msgin/transform"
)

// TestTransformNilFnIsPermanent covers Spec 014 §2.1 row 6 (decision D-M):
// Transform(nil) captures the nil at construction, so the fault cannot change
// for the message's lifetime and is Permanent — diverted to the
// invalid-message channel rather than burning the retry budget.
func TestTransformNilFnIsPermanent(t *testing.T) {
	t.Parallel()

	discard := msgin.HandlerFunc(func(context.Context, msgin.Message[any]) error { return nil })
	err := transform.Transform[int, int](nil)(discard).Handle(t.Context(), msgin.New[any](1))

	require.ErrorIs(t, err, msgin.ErrNilFunc)
	assert.True(t, msgin.IsPermanent(err), "must be permanent")
	assert.Contains(t, err.Error(), "transform.Transform: nil fn")
}
