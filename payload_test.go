package msgin_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kartaladev/msgin"
)

func TestPayloadOf(t *testing.T) {
	tests := []struct {
		name   string
		run    func(t *testing.T) (any, error)
		assert func(t *testing.T, payload any, err error)
	}{
		{
			name: "matching type unboxes and preserves headers",
			run: func(t *testing.T) (any, error) {
				return msgin.PayloadOf[string](msgin.New[any]("hi", msgin.WithID("id-1")))
			},
			assert: func(t *testing.T, payload any, err error) {
				require.NoError(t, err)
				tm := payload.(msgin.Message[string])
				assert.Equal(t, "hi", tm.Payload())
				assert.Equal(t, "id-1", tm.ID())
			},
		},
		{
			name: "mismatched type wraps the existing ErrPayloadType",
			run: func(t *testing.T) (any, error) {
				return msgin.PayloadOf[int](msgin.New[any]("not-an-int"))
			},
			assert: func(t *testing.T, _ any, err error) {
				assert.ErrorIs(t, err, msgin.ErrPayloadType)
				assert.ErrorContains(t, err, "int")
			},
		},
		{
			name: "any target succeeds for a non-nil payload",
			run:  func(t *testing.T) (any, error) { return msgin.PayloadOf[any](msgin.New[any](42)) },
			assert: func(t *testing.T, payload any, err error) {
				require.NoError(t, err)
				assert.Equal(t, 42, payload.(msgin.Message[any]).Payload())
			},
		},
		{
			name: "untyped-nil payload yields ErrPayloadType even for any",
			run:  func(t *testing.T) (any, error) { return msgin.PayloadOf[any](msgin.New[any](nil)) },
			assert: func(t *testing.T, _ any, err error) {
				assert.ErrorIs(t, err, msgin.ErrPayloadType)
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p, err := tc.run(t)
			tc.assert(t, p, err)
		})
	}
}

func TestWithPayload_PreservesHeaders(t *testing.T) {
	in, _ := msgin.PayloadOf[string](msgin.New[any]("x", msgin.WithID("id-9"), msgin.WithHeaders(map[string]any{"k": "v"})))
	out := msgin.WithPayload(in, 100) // string -> int
	assert.Equal(t, 100, out.Payload())
	assert.Equal(t, "id-9", out.ID()) // id + custom headers survive the transform
	v, ok := out.Headers().String("k")
	require.True(t, ok)
	assert.Equal(t, "v", v)
}
