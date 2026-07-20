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
			name: "ClaimGroup then SettleGroup removes the claimed group; a later Add starts fresh",
			assert: func(t *testing.T, s msgin.MessageGroupStore) {
				_, _ = s.Add(t.Context(), "k", msg("a"))
				claim, err := s.ClaimGroup(t.Context(), "k")
				require.NoError(t, err)
				require.NotNil(t, claim)
				assert.Equal(t, "k", claim.Key())
				assert.Len(t, claim.Messages(), 1)
				assert.Equal(t, "a", claim.Messages()[0].ID())

				require.NoError(t, s.SettleGroup(t.Context(), claim))

				g, _ := s.Add(t.Context(), "k", msg("b"))
				assert.Len(t, g.Messages(), 1, "fresh group after settlement")
			},
		},
		{
			name: "ClaimGroup then AbandonGroup returns the members to live",
			assert: func(t *testing.T, s msgin.MessageGroupStore) {
				_, _ = s.Add(t.Context(), "k", msg("a"))
				claim, err := s.ClaimGroup(t.Context(), "k")
				require.NoError(t, err)
				require.NotNil(t, claim)

				require.NoError(t, s.AbandonGroup(t.Context(), claim))

				g, err := s.Add(t.Context(), "k", msg("b"))
				require.NoError(t, err)
				assert.Len(t, g.Messages(), 2, "abandoned members are live again, plus the new one")
			},
		},
		{
			name: "ClaimGroup on an absent key returns (nil, nil)",
			assert: func(t *testing.T, s msgin.MessageGroupStore) {
				claim, err := s.ClaimGroup(t.Context(), "absent")
				require.NoError(t, err)
				assert.Nil(t, claim)
			},
		},
		{
			name: "ClaimGroup on an already-leased key returns (nil, nil): held by the live holder",
			assert: func(t *testing.T, s msgin.MessageGroupStore) {
				_, _ = s.Add(t.Context(), "k", msg("a"))
				first, err := s.ClaimGroup(t.Context(), "k")
				require.NoError(t, err)
				require.NotNil(t, first)

				second, err := s.ClaimGroup(t.Context(), "k")
				require.NoError(t, err)
				assert.Nil(t, second, "a second concurrent claim on an in-flight lease must be held")
			},
		},
		{
			name: "a member arriving during a lease survives SettleGroup as a fresh residual group",
			assert: func(t *testing.T, s msgin.MessageGroupStore) {
				_, _ = s.Add(t.Context(), "k", msg("a"))
				claim, err := s.ClaimGroup(t.Context(), "k")
				require.NoError(t, err)
				require.NotNil(t, claim)

				// A late member arrives while "a" is under lease.
				live, err := s.Add(t.Context(), "k", msg("b"))
				require.NoError(t, err)
				assert.Len(t, live.Messages(), 1, "Add returns LIVE (unclaimed) members only")
				assert.Equal(t, "b", live.Messages()[0].ID())

				require.NoError(t, s.SettleGroup(t.Context(), claim))

				g, err := s.Add(t.Context(), "k", msg("c"))
				require.NoError(t, err)
				require.Len(t, g.Messages(), 2, "the late member b survives, plus the new c")
				assert.Equal(t, "b", g.Messages()[0].ID())
				assert.Equal(t, "c", g.Messages()[1].ID())
			},
		},
		{
			name: "SettleGroup with a stale epoch (the lease was re-claimed) is a no-op — the fence protects the current lease",
			assert: func(t *testing.T, s msgin.MessageGroupStore) {
				_, _ = s.Add(t.Context(), "k", msg("a"))
				staleClaim, err := s.ClaimGroup(t.Context(), "k")
				require.NoError(t, err)
				require.NotNil(t, staleClaim)

				require.NoError(t, s.AbandonGroup(t.Context(), staleClaim)) // released; the epoch is NOT rewound

				freshClaim, err := s.ClaimGroup(t.Context(), "k") // bumps the epoch past staleClaim's
				require.NoError(t, err)
				require.NotNil(t, freshClaim)

				// The stale claim's Settle must be a no-op: it must not delete
				// the member now held (again) by the fresh claim.
				require.NoError(t, s.SettleGroup(t.Context(), staleClaim))

				require.NoError(t, s.SettleGroup(t.Context(), freshClaim)) // the real settle
				g, err := s.Add(t.Context(), "k", msg("b"))
				require.NoError(t, err)
				assert.Len(t, g.Messages(), 1, "settled cleanly via the fresh claim; the stale settle was a no-op")
			},
		},
		{
			name: "AbandonGroup with a stale epoch (the lease was re-claimed) is a no-op — the fence protects the current lease",
			assert: func(t *testing.T, s msgin.MessageGroupStore) {
				_, _ = s.Add(t.Context(), "k", msg("a"))
				staleClaim, err := s.ClaimGroup(t.Context(), "k")
				require.NoError(t, err)
				require.NotNil(t, staleClaim)

				// A late member arrives during the lease, forming a residual so
				// the group survives settlement (rather than being deleted) —
				// keeping the SAME group alive for the epoch to be re-bumped on.
				_, err = s.Add(t.Context(), "k", msg("b"))
				require.NoError(t, err)
				require.NoError(t, s.SettleGroup(t.Context(), staleClaim)) // settles "a"; "b" survives live

				freshClaim, err := s.ClaimGroup(t.Context(), "k") // claims "b"; bumps the epoch past staleClaim's
				require.NoError(t, err)
				require.NotNil(t, freshClaim)

				// The stale (already-settled) claim's Abandon must be a no-op:
				// it must not un-lease the fresh claim.
				require.NoError(t, s.AbandonGroup(t.Context(), staleClaim))

				second, err := s.ClaimGroup(t.Context(), "k")
				require.NoError(t, err)
				assert.Nil(t, second, "the fresh claim must still be exclusively held; the stale abandon must not have un-leased it")
			},
		},
		{
			name: "Expired excludes a leased group",
			assert: func(t *testing.T, s msgin.MessageGroupStore) {
				_, _ = s.Add(t.Context(), "k", msg("a"))
				claim, err := s.ClaimGroup(t.Context(), "k")
				require.NoError(t, err)
				require.NotNil(t, claim)

				clock.Advance(10 * time.Second)
				cutoff := clock.Now()

				exp, err := s.Expired(t.Context(), cutoff)
				require.NoError(t, err)
				assert.Empty(t, exp, "a leased group must not be surfaced as expired")
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
