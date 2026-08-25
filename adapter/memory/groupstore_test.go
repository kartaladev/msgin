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
			// B1-7 / Spec 017 §3.3: the group-COUNT arm, upgraded from a bare
			// sentinel to the wrapped render in the same commit as the member
			// arm (fix the class, not the instance). It stays TRANSIENT — the
			// group map drains whenever ANY group settles.
			name: "the group-count overflow render names the site, the key and the limit",
			assert: func(t *testing.T) {
				s, err := memory.NewGroupStore(memory.WithMaxGroups(1))
				require.NoError(t, err)

				_, err = s.Add(t.Context(), "k1", msgin.New[any]("a", msgin.WithID("a")))
				require.NoError(t, err)

				g, err := s.Add(t.Context(), "k2", msgin.New[any]("b", msgin.WithID("b")))
				require.ErrorIs(t, err, msgin.ErrOverflowDropped)
				assert.False(t, msgin.IsPermanent(err),
					"the group-count arm keeps its TRANSIENT classification (Spec 017 §5)")
				assert.EqualError(t, err,
					`msgin: message dropped by overflow policy: memory.GroupStore.Add: new group "k2" rejected: store holds 1 groups, limit 1`)
				// Plan 031 review §6 item 5: the group-COUNT arm is the one
				// place Add MUST NOT hand back a snapshot. The key was never
				// admitted, so there is no group to report and any snapshot
				// would be a fabrication — Add's godoc promises (nil, err)
				// here, and the member-cap arm below is the only arm that
				// pairs a group with an error. Left unasserted, a mutant
				// returning `snapshot{key: key}` beside the error survived
				// every package in the workspace.
				assert.Nil(t, g,
					"the group-count arm has no group to report and must return (nil, err) — see Add's godoc")
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

		// ---- Spec 017 / Plan 031 Task 1: the per-group MEMBER bound ----
		{
			// B1-4 + AC-2b (upper end): the FULL construction-time render,
			// not merely errors.Is.
			name: "WithMaxGroupMembers ceiling+1 is rejected",
			assert: func(t *testing.T) {
				_, err := memory.NewGroupStore(memory.WithMaxGroupMembers(1<<20 + 1))
				require.ErrorIs(t, err, msgin.ErrInvalidCapacity)
				assert.False(t, msgin.IsPermanent(err), "R1 constructor error must stay bare (ADR 0029 D-M)")
				assert.EqualError(t, err,
					"msgin: capacity out of range: memory.WithMaxGroupMembers: 1048577 not in [1, 1048576]")
			},
		},
		{
			// B1-5 + AC-2b (lower end): the case that would catch a format
			// rendering "0 exceeds 1048576" — false, since 0 exceeds nothing.
			name: "WithMaxGroupMembers 0 is rejected",
			assert: func(t *testing.T) {
				_, err := memory.NewGroupStore(memory.WithMaxGroupMembers(0))
				require.ErrorIs(t, err, msgin.ErrInvalidCapacity)
				assert.False(t, msgin.IsPermanent(err), "R1 constructor error must stay bare (ADR 0029 D-M)")
				assert.EqualError(t, err,
					"msgin: capacity out of range: memory.WithMaxGroupMembers: 0 not in [1, 1048576]")
			},
		},
		{
			// B1-6 (in-range arm), constructor only — Spec 017 AC-6 forbids
			// growing a group to the ceiling (8.6 s / 48.3 GiB, Spec 016 §1.4).
			name: "WithMaxGroupMembers ceiling is accepted",
			assert: func(t *testing.T) {
				s, err := memory.NewGroupStore(memory.WithMaxGroupMembers(1 << 20))
				require.NoError(t, err)
				require.NotNil(t, s)
			},
		},
		{
			// B1-6 (default arm): the shipped default (65,536) admits members
			// without any option being set.
			name: "the default member cap is usable",
			assert: func(t *testing.T) {
				s, err := memory.NewGroupStore()
				require.NoError(t, err)

				g, err := s.Add(t.Context(), "k", msgin.New[any]("a", msgin.WithID("a")))
				require.NoError(t, err)
				assert.Len(t, g.Messages(), 1)
			},
		},
		{
			// B1-2: the cap is ATTAINABLE — the check is `>=` before the
			// append, so exactly cap members are admitted. A mutant inverting
			// it to `>` admits cap+1 and no other case sees that.
			name: "Add admits members up to the cap",
			assert: func(t *testing.T) {
				s, err := memory.NewGroupStore(memory.WithMaxGroupMembers(4))
				require.NoError(t, err)

				var g msgin.MessageGroup
				for _, id := range []string{"a", "b", "c", "d"} {
					g, err = s.Add(t.Context(), "k", msgin.New[any](id, msgin.WithID(id)))
					require.NoError(t, err, "member %q must be admitted at cap 4", id)
				}
				assert.Len(t, g.Messages(), 4, "the cap is exactly attainable")
			},
		},
		{
			// B1-1: the cap+1-th member is rejected.
			name: "Add rejects the cap-plus-one member",
			assert: func(t *testing.T) {
				s := filledGroup(t, 4, "k", "a", "b", "c", "d")

				_, err := s.Add(t.Context(), "k", msgin.New[any]("e", msgin.WithID("e")))
				require.ErrorIs(t, err, msgin.ErrOverflowDropped)
			},
		},
		{
			// B1-8 + AC-2c: an UNLEASED group cannot drain itself, so the
			// rejection is Permanent — terminal on the shipped zero-value
			// RetryPolicy instead of an unlogged zero-delay hot spin (§3.3.1).
			name: "an over-cap rejection on an unleased group is Permanent",
			assert: func(t *testing.T) {
				s := filledGroup(t, 4, "k", "a", "b", "c", "d")

				_, err := s.Add(t.Context(), "k", msgin.New[any]("e", msgin.WithID("e")))
				require.ErrorIs(t, err, msgin.ErrOverflowDropped)
				assert.True(t, msgin.IsPermanent(err))
				assert.EqualError(t, err,
					`msgin: permanent: msgin: message dropped by overflow policy: memory.GroupStore.Add: group "k" holds 4 members, limit 4`)
			},
		},
		{
			// B1-9: a LEASED group has a claim in flight, so Settle/Abandon
			// will drain it — the rejection stays transient and must NOT be
			// Permanent-wrapped.
			//
			// This is the WHOLLY-CLAIMED shape: the claim froze all 4 members,
			// so the live residual is EMPTY. That is legitimate, not the inert
			// mechanism it looks like — see the case below, which is the same
			// arm with a non-empty residual, and Aggregator.Handle's
			// zero-member gate, which returns the store's transient error
			// unchanged rather than running a release strategy on 0 members.
			name: "an over-cap rejection while leased is transient",
			assert: func(t *testing.T) {
				s := filledGroup(t, 4, "k", "a", "b", "c", "d")

				claim, err := s.ClaimGroup(t.Context(), "k")
				require.NoError(t, err)
				require.NotNil(t, claim)

				g, err := s.Add(t.Context(), "k", msgin.New[any]("e", msgin.WithID("e")))
				require.ErrorIs(t, err, msgin.ErrOverflowDropped)
				assert.False(t, msgin.IsPermanent(err),
					"a claim is in flight; Settle/Abandon drains the group, so the retry genuinely succeeds")
				assert.EqualError(t, err,
					`msgin: message dropped by overflow policy: memory.GroupStore.Add: group "k" holds 4 members, limit 4`)
				require.NotNil(t, g,
					"the member-cap arm ALWAYS pairs a snapshot with the error, even when the residual is empty")
				assert.Empty(t, g.Messages(),
					"the claim froze all 4 members, so nothing is live — Handle's zero-member gate takes it from here")
			},
		},
		{
			// Plan 031 review R-5. The finding held that this arm's snapshot
			// is "always empty (msgs[claimedLen:] with claimedLen ==
			// len(msgs))", making the snapshot-alongside-error mechanism inert
			// exactly where a concurrent claim makes it matter. It is NOT:
			// claimedLen == len(msgs) holds only at the instant ClaimGroup
			// returns. Add appends BEYOND claimedLen for the width of the
			// lease, so a group claimed before it filled reports a real,
			// non-empty live residual on the over-cap arm — and that residual
			// is precisely what Aggregator.Handle re-evaluates the release
			// against.
			//
			// Nothing in this package asserted the arm: `return live, err` →
			// `return nil, err` survived `go test ./adapter/memory/...`. (It
			// was caught at repo scope, by two routing cases added for D-AW —
			// but a cross-package accident is not a contract, and the store
			// that makes the promise is the one that must pin it.)
			name: "an over-cap rejection while leased returns the residual added DURING the lease",
			assert: func(t *testing.T) {
				// Claim at 3 of a cap of 4, so claimedLen (3) < len(msgs).
				s := filledGroup(t, 4, "k", "a", "b", "c")

				claim, err := s.ClaimGroup(t.Context(), "k")
				require.NoError(t, err)
				require.NotNil(t, claim)
				require.Len(t, claim.Messages(), 3, "the claim froze exactly the 3 members present")

				// Admitted during the lease: it lands beyond claimedLen and is
				// LIVE, taking the group to the cap.
				g, err := s.Add(t.Context(), "k", msgin.New[any]("d", msgin.WithID("d")))
				require.NoError(t, err)
				require.Len(t, g.Messages(), 1, "the in-lease arrival is the whole live residual")

				g, err = s.Add(t.Context(), "k", msgin.New[any]("e", msgin.WithID("e")))
				require.ErrorIs(t, err, msgin.ErrOverflowDropped)
				assert.False(t, msgin.IsPermanent(err), "a claim is in flight, so the rejection stays transient")
				require.NotNil(t, g, "the live snapshot must travel with the error on the LEASED arm too (§3.3a)")
				require.Len(t, g.Messages(), 1,
					"the residual is NOT empty: 'd' arrived after the claim froze msgs[:3], so it is live")
				assert.Equal(t, "d", g.Messages()[0].ID(),
					"the snapshot is the live residual — the claimed prefix is excluded and the refused member is absent")
				assert.Equal(t, "k", g.Key())
			},
		},
		{
			// B1-10 / §3.3a: the LIVE snapshot rides out WITH the error, so
			// Aggregator.Handle can re-evaluate the release. Reject the
			// member, not the release.
			name: "an over-cap rejection returns the live snapshot",
			assert: func(t *testing.T) {
				s := filledGroup(t, 4, "k", "a", "b", "c", "d")

				g, err := s.Add(t.Context(), "k", msgin.New[any]("e", msgin.WithID("e")))
				require.Error(t, err)
				require.NotNil(t, g, "the live snapshot must travel with the error (§3.3a)")
				assert.Equal(t, "k", g.Key())
				assert.Len(t, g.Messages(), 4)
			},
		},
		{
			// B1-3 / §3.4a: the cap check sits BELOW the dedup lookup, so an
			// idempotent re-add at exactly the cap is still a no-op, never an
			// overflow. Mutant: hoist the check above the lookup ⇒ fails.
			name: "re-adding an existing id at the cap is a no-op",
			assert: func(t *testing.T) {
				s := filledGroup(t, 4, "k", "a", "b", "c", "d")

				g, err := s.Add(t.Context(), "k", msgin.New[any]("d", msgin.WithID("d")))
				require.NoError(t, err)
				assert.Len(t, g.Messages(), 4, "the redelivered member is deduped, not counted again")
			},
		},
		{
			// B1-3b / AC-1c / §3.4a: the cap check sits ABOVE the dedup
			// INSERT, so a rejected member leaves NO trace in g.ids. Mutant:
			// move the check below the insert ⇒ the redelivery returns the
			// dedup no-op with a NIL error and the source Acks a message that
			// was never appended — silent loss.
			name: "a rejected member is admitted after the group drains",
			assert: func(t *testing.T) {
				// The drain must leave a RESIDUAL. SettleGroup deletes the
				// whole group (g.ids included) when the residual is empty, so
				// a claim-everything/settle fixture erases the dedup set no
				// matter where the cap check sits — and the mutant this case
				// exists to kill SURVIVES it. Claim 3 of 4 instead, so "d"
				// survives the settle and carries g.ids with it.
				s := filledGroup(t, 4, "k", "a", "b", "c")

				claim, err := s.ClaimGroup(t.Context(), "k")
				require.NoError(t, err)
				require.NotNil(t, claim)

				_, err = s.Add(t.Context(), "k", msgin.New[any]("d", msgin.WithID("d")))
				require.NoError(t, err)

				_, err = s.Add(t.Context(), "k", msgin.New[any]("e", msgin.WithID("e")))
				require.ErrorIs(t, err, msgin.ErrOverflowDropped)

				require.NoError(t, s.SettleGroup(t.Context(), claim))

				g, err := s.Add(t.Context(), "k", msgin.New[any]("e", msgin.WithID("e")))
				require.NoError(t, err, `"e" was rejected, never stored — it must not be swallowed as a duplicate`)
				require.Len(t, g.Messages(), 2, `the residual "d" plus the now-admitted "e"`)
				assert.Equal(t, "e", g.Messages()[1].ID())
			},
		},
		{
			// B1-3c / §3.4a: the id is HOISTED so the check also runs on the
			// id-less path. Mutant: fold it back inside `if id != ""` ⇒ the
			// unbounded append returns for id-less members.
			name: "the cap bounds an id-less member",
			assert: func(t *testing.T) {
				s, err := memory.NewGroupStore(memory.WithMaxGroupMembers(2))
				require.NoError(t, err)

				idless := msgin.NewMessage[any]("p", msgin.Headers{})
				require.Empty(t, idless.ID(), "the fixture must be id-LESS: msgin.New always stamps an id")

				for range 2 {
					_, err = s.Add(t.Context(), "k", idless)
					require.NoError(t, err)
				}
				_, err = s.Add(t.Context(), "k", idless)
				require.ErrorIs(t, err, msgin.ErrOverflowDropped)
			},
		},
		{
			// AC-7 / Spec 017 §3.7, the MUST-REPORT clause ONLY: an Add that
			// would exceed the bound is reported as msgin.ErrOverflowDropped.
			// The other three clauses of §3.7 are covered elsewhere (the
			// MUST-bound by "Add rejects the cap-plus-one member", the SHOULD
			// by the two classification cases, the MAY by "an over-cap
			// rejection returns the live snapshot").
			//
			// What this case uniquely buys is the INTERFACE-TYPED drive: the
			// store is held in a msgin.MessageGroupStore, so the body below is
			// copyable verbatim by a third-party store author as the executable
			// form of the SPI clause. A mutant returning a bare, non-wrapping
			// error from Add's overflow arm fails the ErrorIs.
			name: "the MUST-report clause holds through the msgin.MessageGroupStore interface",
			assert: func(t *testing.T) {
				s, err := memory.NewGroupStore(memory.WithMaxGroupMembers(1))
				require.NoError(t, err)

				var store msgin.MessageGroupStore = s

				_, err = store.Add(t.Context(), "k", msgin.New[any]("a", msgin.WithID("a")))
				require.NoError(t, err)

				_, err = store.Add(t.Context(), "k", msgin.New[any]("b", msgin.WithID("b")))
				require.ErrorIs(t, err, msgin.ErrOverflowDropped)
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

// filledGroup builds a GroupStore capped at maxMembers and fills group key
// with one id-ful member per id, asserting each was admitted. It is the
// shared arrangement for every at-the-cap case above.
func filledGroup(t *testing.T, maxMembers int, key string, ids ...string) *memory.GroupStore {
	t.Helper()
	s, err := memory.NewGroupStore(memory.WithMaxGroupMembers(maxMembers))
	require.NoError(t, err)
	for _, id := range ids {
		_, err = s.Add(t.Context(), key, msgin.New[any](id, msgin.WithID(id)))
		require.NoError(t, err)
	}
	return s
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
