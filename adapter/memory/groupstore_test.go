package memory_test

import (
	"testing"

	"github.com/kartaladev/msgin"
	"github.com/kartaladev/msgin/adapter/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
)

// TestGroupStore covers adapter/memory.GroupStore specifics not exercised by
// the core store-contract conformance table: capacity validation and the
// group-count overflow policy.
func TestGroupStore(t *testing.T) {
	tests := []struct {
		name   string
		assert func(t *testing.T)
	}{
		{
			name: "non-positive WithMaxGroups is ErrInvalidCapacity",
			assert: func(t *testing.T) {
				_, err := memory.NewGroupStore(memory.WithMaxGroups(0))
				require.ErrorIs(t, err, msgin.ErrInvalidCapacity)
			},
		},
		{
			name: "a new key beyond WithMaxGroups is ErrOverflowDropped",
			assert: func(t *testing.T) {
				s, err := memory.NewGroupStore(memory.WithMaxGroups(1))
				require.NoError(t, err)

				g, err := s.Add(t.Context(), "k1", msgin.New[any]("a", msgin.WithID("a")))
				require.NoError(t, err)
				assert.Len(t, g.Messages(), 1)

				_, err = s.Add(t.Context(), "k2", msgin.New[any]("b", msgin.WithID("b")))
				require.ErrorIs(t, err, msgin.ErrOverflowDropped)
			},
		},
		{
			name: "a second message to an existing key still succeeds at the cap",
			assert: func(t *testing.T) {
				s, err := memory.NewGroupStore(memory.WithMaxGroups(1))
				require.NoError(t, err)

				_, err = s.Add(t.Context(), "k1", msgin.New[any]("a", msgin.WithID("a")))
				require.NoError(t, err)

				g, err := s.Add(t.Context(), "k1", msgin.New[any]("b", msgin.WithID("b")))
				require.NoError(t, err)
				assert.Len(t, g.Messages(), 2)
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			defer goleak.VerifyNone(t)
			tc.assert(t)
		})
	}
}
