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

// TestNewGroupStore_NilOptionElement proves a nil ELEMENT of opts is a bare
// ErrNilFunc naming the computed 0-based index (Spec 015 §3.1, family R1: the
// loop runs BEFORE the WithMaxGroups validation, so a nil option wins over an
// invalid explicit maxGroups).
func TestNewGroupStore_NilOptionElement(t *testing.T) {
	tests := []struct {
		name   string
		opts   []memory.GroupStoreOption
		assert func(t *testing.T, err error)
	}{
		{
			name: "nil element alone",
			opts: []memory.GroupStoreOption{nil},
			assert: func(t *testing.T, err error) {
				require.ErrorIs(t, err, msgin.ErrNilFunc)
				assert.False(t, msgin.IsPermanent(err), "R1 nil-option error must be BARE, not Permanent-wrapped")
				assert.Contains(t, err.Error(), "memory.NewGroupStore: nil option at index 0")
			},
		},
		{
			name: "nil element after a valid option asserts the COMPUTED index and the FULL position",
			opts: []memory.GroupStoreOption{memory.WithMaxGroups(5), nil},
			assert: func(t *testing.T, err error) {
				require.ErrorIs(t, err, msgin.ErrNilFunc)
				assert.Contains(t, err.Error(), "memory.NewGroupStore: nil option at index 1")
			},
		},
		{
			// AC-3: D-U's "latch only when unlatched" analog for R1's
			// first-nil-wins — an implementation reporting the LAST nil
			// passes every other assertion here.
			name: "first of two nils wins",
			opts: []memory.GroupStoreOption{nil, nil},
			assert: func(t *testing.T, err error) {
				require.ErrorIs(t, err, msgin.ErrNilFunc)
				assert.Contains(t, err.Error(), "memory.NewGroupStore: nil option at index 0")
			},
		},
		{
			// Precedence (Spec 015 §3.5): NewGroupStore is LOOP-FIRST — the
			// apply loop runs before the WithMaxGroups <= 0 validation, so a
			// nil option wins over an invalid explicit maxGroups.
			name: "nil option precedes the WithMaxGroups validation",
			opts: []memory.GroupStoreOption{memory.WithMaxGroups(0), nil},
			assert: func(t *testing.T, err error) {
				require.ErrorIs(t, err, msgin.ErrNilFunc)
				assert.NotErrorIs(t, err, msgin.ErrInvalidCapacity)
				assert.Contains(t, err.Error(), "memory.NewGroupStore: nil option at index 1")
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := memory.NewGroupStore(tc.opts...)
			tc.assert(t, err)
		})
	}
}
