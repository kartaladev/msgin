package msgin_test

import (
	"testing"
	"time"

	"github.com/jonboulle/clockwork"
	"github.com/kartaladev/msgin"
	"github.com/kartaladev/msgin/adapter/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
)

// TestGroupStore_Conformance is a store-contract table any msgin.MessageGroupStore
// implementation must pass. Run here against adapter/memory.GroupStore, the
// in-memory reference implementation.
func TestGroupStore_Conformance(t *testing.T) {
	clock := clockwork.NewFakeClock()
	newStore := func() msgin.MessageGroupStore {
		s, err := memory.NewGroupStore(memory.WithGroupClock(clock))
		require.NoError(t, err)
		return s
	}
	msg := func(id string) msgin.Message[any] { return msgin.New[any](id, msgin.WithID(id)) }

	tests := []struct {
		name   string
		assert func(t *testing.T, s msgin.MessageGroupStore)
	}{
		{
			name: "Add grows the group in arrival order",
			assert: func(t *testing.T, s msgin.MessageGroupStore) {
				_, _ = s.Add(t.Context(), "k", msg("a"))
				g, err := s.Add(t.Context(), "k", msg("b"))
				require.NoError(t, err)
				assert.Equal(t, "k", g.Key())
				assert.Len(t, g.Messages(), 2)
				assert.Equal(t, "a", g.Messages()[0].ID())
				assert.False(t, g.CreatedAt().IsZero())
			},
		},
		{
			name: "Add is idempotent by id",
			assert: func(t *testing.T, s msgin.MessageGroupStore) {
				_, _ = s.Add(t.Context(), "k", msg("a"))
				g, err := s.Add(t.Context(), "k", msg("a")) // same id
				require.NoError(t, err)
				assert.Len(t, g.Messages(), 1)
			},
		},
		{
			name: "keys are independent",
			assert: func(t *testing.T, s msgin.MessageGroupStore) {
				_, _ = s.Add(t.Context(), "k1", msg("a"))
				g, _ := s.Add(t.Context(), "k2", msg("b"))
				assert.Len(t, g.Messages(), 1)
			},
		},
		{
			name: "Remove drops the group and returns a snapshot of it",
			assert: func(t *testing.T, s msgin.MessageGroupStore) {
				_, _ = s.Add(t.Context(), "k", msg("a"))
				removed, err := s.Remove(t.Context(), "k")
				require.NoError(t, err)
				require.NotNil(t, removed)
				assert.Equal(t, "k", removed.Key())
				assert.Len(t, removed.Messages(), 1)
				assert.Equal(t, "a", removed.Messages()[0].ID())

				g, _ := s.Add(t.Context(), "k", msg("b"))
				assert.Len(t, g.Messages(), 1) // fresh group
			},
		},
		{
			name: "Remove on an absent key returns (nil, nil)",
			assert: func(t *testing.T, s msgin.MessageGroupStore) {
				removed, err := s.Remove(t.Context(), "absent")
				require.NoError(t, err)
				assert.Nil(t, removed)
			},
		},
		{
			name: "Expired returns only groups older than the cutoff",
			assert: func(t *testing.T, s msgin.MessageGroupStore) {
				_, _ = s.Add(t.Context(), "old", msg("a"))
				clock.Advance(10 * time.Second)
				cutoff := clock.Now()
				_, _ = s.Add(t.Context(), "new", msg("b"))
				exp, err := s.Expired(t.Context(), cutoff)
				require.NoError(t, err)
				require.Len(t, exp, 1)
				assert.Equal(t, "old", exp[0].Key())
			},
		},
		{
			name: "EmitsLiveValue is true",
			assert: func(t *testing.T, s msgin.MessageGroupStore) {
				assert.True(t, s.EmitsLiveValue())
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			defer goleak.VerifyNone(t)
			tc.assert(t, newStore())
		})
	}
}
