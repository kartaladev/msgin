package sql_test

// groupstore_unit_test.go drives sql.GroupStore against the in-memory
// fakeGroupDialect (groupdialect_fake_test.go) — no real database (Plan 017
// Task 2). It covers construction validation, Add's framing/id/payload
// guards, ClaimGroup/SettleGroup/AbandonGroup's mapping and parameter
// pass-through, Expired's decode-mapping, Ready/EnsureSchema delegation,
// RecoverInterval/EmitsLiveValue, and the classifyQueryErr wrap-vs-passthrough
// branch. Real per-engine SQL (group-row locking, claimed-epoch
// re-absorption, deadlock ordering) is proven against real engines in Task 3.

import (
	"database/sql"
	"errors"
	"fmt"
	"testing"
	"time"

	msgin "github.com/kartaladev/msgin"
	msginsql "github.com/kartaladev/msgin/adapter/database/sql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewGroupStore_Construction covers every construction-validation branch
// (CLAUDE.md hot-path gate): nil db, nil dialect, an invalid table
// identifier, and an out-of-range WithGroupLeaseTTL — plus the happy path
// confirming the 5m default lease TTL and EmitsLiveValue()==false.
func TestNewGroupStore_Construction(t *testing.T) {
	t.Parallel()

	type testCase struct {
		name   string
		build  func(t *testing.T) (*msginsql.GroupStore, error)
		assert func(t *testing.T, store *msginsql.GroupStore, err error)
	}

	cases := []testCase{
		{
			name: "nil db is ErrNilAdapter",
			build: func(t *testing.T) (*msginsql.GroupStore, error) {
				return msginsql.NewGroupStore(nil, "groups", newFakeGroupDialect())
			},
			assert: func(t *testing.T, store *msginsql.GroupStore, err error) {
				require.ErrorIs(t, err, msgin.ErrNilAdapter)
				assert.Nil(t, store)
			},
		},
		{
			name: "invalid table name is ErrInvalidTableName",
			build: func(t *testing.T) (*msginsql.GroupStore, error) {
				return msginsql.NewGroupStore(openDB(t, fakeDriverName), "bad name!", newFakeGroupDialect())
			},
			assert: func(t *testing.T, store *msginsql.GroupStore, err error) {
				require.ErrorIs(t, err, msginsql.ErrInvalidTableName)
				assert.Nil(t, store)
			},
		},
		{
			name: "nil dialect is ErrNilDialect",
			build: func(t *testing.T) (*msginsql.GroupStore, error) {
				return msginsql.NewGroupStore(openDB(t, fakeDriverName), "groups", nil)
			},
			assert: func(t *testing.T, store *msginsql.GroupStore, err error) {
				require.ErrorIs(t, err, msginsql.ErrNilDialect)
				assert.Nil(t, store)
			},
		},
		{
			name: "WithGroupLeaseTTL(0) is ErrInvalidLeaseTTL",
			build: func(t *testing.T) (*msginsql.GroupStore, error) {
				return msginsql.NewGroupStore(openDB(t, fakeDriverName), "groups", newFakeGroupDialect(),
					msginsql.WithGroupLeaseTTL(0))
			},
			assert: func(t *testing.T, store *msginsql.GroupStore, err error) {
				require.ErrorIs(t, err, msginsql.ErrInvalidLeaseTTL)
				assert.Nil(t, store)
			},
		},
		{
			name: "WithGroupLeaseTTL(negative) is ErrInvalidLeaseTTL",
			build: func(t *testing.T) (*msginsql.GroupStore, error) {
				return msginsql.NewGroupStore(openDB(t, fakeDriverName), "groups", newFakeGroupDialect(),
					msginsql.WithGroupLeaseTTL(-time.Second))
			},
			assert: func(t *testing.T, store *msginsql.GroupStore, err error) {
				require.ErrorIs(t, err, msginsql.ErrInvalidLeaseTTL)
				assert.Nil(t, store)
			},
		},
		{
			name: "valid construction: RecoverInterval defaults to 5m, EmitsLiveValue is false",
			build: func(t *testing.T) (*msginsql.GroupStore, error) {
				return msginsql.NewGroupStore(openDB(t, fakeDriverName), "groups", newFakeGroupDialect())
			},
			assert: func(t *testing.T, store *msginsql.GroupStore, err error) {
				require.NoError(t, err)
				require.NotNil(t, store)
				assert.Equal(t, 5*time.Minute, store.RecoverInterval())
				assert.False(t, store.EmitsLiveValue())
			},
		},
		{
			name: "WithGroupLeaseTTL(10m) overrides RecoverInterval",
			build: func(t *testing.T) (*msginsql.GroupStore, error) {
				return msginsql.NewGroupStore(openDB(t, fakeDriverName), "groups", newFakeGroupDialect(),
					msginsql.WithGroupLeaseTTL(10*time.Minute))
			},
			assert: func(t *testing.T, store *msginsql.GroupStore, err error) {
				require.NoError(t, err)
				assert.Equal(t, 10*time.Minute, store.RecoverInterval())
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			store, err := tc.build(t)
			tc.assert(t, store, err)
		})
	}
}

// TestGroupStore_Add covers Add's guard branches (empty msgin.message-id ->
// ErrMissingMsgID BEFORE any dialect call; a non-[]byte payload ->
// ErrInvalidPayload) and the happy path: headers+payload framed via
// EncodeHeaders and delegated to AddMember, returning a MessageGroup decoded
// from GroupRows with members in arrival order and idempotent re-add.
func TestGroupStore_Add(t *testing.T) {
	t.Parallel()

	t.Run("empty message id is ErrMissingMsgID; no dialect call is made", func(t *testing.T) {
		t.Parallel()
		fd := newFakeGroupDialect()
		store, err := msginsql.NewGroupStore(openDB(t, fakeDriverName), "groups", fd)
		require.NoError(t, err)

		// NewMessage (not New) so no msgin.message-id is stamped — the guard under test.
		msg := msgin.NewMessage[any]([]byte("p"), msgin.NewHeaders(nil))
		_, err = store.Add(t.Context(), "corr-1", msg)
		require.ErrorIs(t, err, msginsql.ErrMissingMsgID)
		assert.False(t, fd.groupExists("corr-1"))
	})

	t.Run("non-[]byte payload is ErrInvalidPayload", func(t *testing.T) {
		t.Parallel()
		fd := newFakeGroupDialect()
		store, err := msginsql.NewGroupStore(openDB(t, fakeDriverName), "groups", fd)
		require.NoError(t, err)

		msg := msgin.New[any](12345, msgin.WithID("m-1"))
		_, err = store.Add(t.Context(), "corr-1", msg)
		require.ErrorIs(t, err, msginsql.ErrInvalidPayload)
		assert.False(t, fd.groupExists("corr-1"))
	})

	t.Run("valid adds frame+delegate and decode the live snapshot in arrival order, idempotent by id", func(t *testing.T) {
		t.Parallel()
		fd := newFakeGroupDialect()
		store, err := msginsql.NewGroupStore(openDB(t, fakeDriverName), "groups", fd)
		require.NoError(t, err)

		m1 := msgin.New[any]([]byte("p1"), msgin.WithID("m-1")).WithHeader(msgin.HeaderSequenceNumber, 1)
		m2 := msgin.New[any]([]byte("p2"), msgin.WithID("m-2")).WithHeader(msgin.HeaderSequenceNumber, 2)

		g1, err := store.Add(t.Context(), "corr-1", m1)
		require.NoError(t, err)
		assert.Equal(t, "corr-1", g1.Key())
		require.Len(t, g1.Messages(), 1)
		assert.Equal(t, "m-1", g1.Messages()[0].ID())
		assert.Equal(t, []byte("p1"), g1.Messages()[0].Payload())
		seq, ok := g1.Messages()[0].Headers().Int(msgin.HeaderSequenceNumber)
		require.True(t, ok)
		assert.Equal(t, 1, seq)

		g2, err := store.Add(t.Context(), "corr-1", m2)
		require.NoError(t, err)
		require.Len(t, g2.Messages(), 2)
		assert.Equal(t, "m-1", g2.Messages()[0].ID())
		assert.Equal(t, "m-2", g2.Messages()[1].ID())

		// Idempotent re-add of an already-stored member id: a no-op, still 2.
		g3, err := store.Add(t.Context(), "corr-1", m1)
		require.NoError(t, err)
		assert.Len(t, g3.Messages(), 2)
	})

	t.Run("a header value that cannot be JSON-encoded surfaces the EncodeHeaders error; no dialect call is made", func(t *testing.T) {
		t.Parallel()
		fd := newFakeGroupDialect()
		store, err := msginsql.NewGroupStore(openDB(t, fakeDriverName), "groups", fd)
		require.NoError(t, err)

		msg := msgin.New[any]([]byte("p"), msgin.WithID("m-1")).WithHeader("bad", make(chan int))
		_, err = store.Add(t.Context(), "corr-1", msg)
		require.Error(t, err)
		assert.False(t, fd.groupExists("corr-1"))
	})

	t.Run("a dialect AddMember error, with schema present, propagates raw", func(t *testing.T) {
		t.Parallel()
		fd := newFakeGroupDialect()
		fd.markGroupReady("groups")
		fd.addMemberErr = errors.New("add boom")
		store, err := msginsql.NewGroupStore(openDB(t, fakeDriverName), "groups", fd)
		require.NoError(t, err)

		_, err = store.Add(t.Context(), "corr-1", msgin.New[any]([]byte("p"), msgin.WithID("m-1")))
		require.Error(t, err)
		assert.NotErrorIs(t, err, msginsql.ErrSchemaNotReady)
		assert.Contains(t, err.Error(), "add boom")
	})
}

// TestGroupStore_ClaimGroup covers the (nil,nil) absent/leased mapping, the
// non-nil ClaimedGroup -> MessageGroupClaim mapping (Epoch wired), the
// leaseTTL/lockedBy pass-through, and dialect error propagation.
func TestGroupStore_ClaimGroup(t *testing.T) {
	t.Parallel()

	t.Run("absent group: dialect nil maps to (nil, nil)", func(t *testing.T) {
		t.Parallel()
		fd := newFakeGroupDialect()
		store, err := msginsql.NewGroupStore(openDB(t, fakeDriverName), "groups", fd)
		require.NoError(t, err)

		claim, err := store.ClaimGroup(t.Context(), "corr-missing")
		require.NoError(t, err)
		assert.Nil(t, claim)
	})

	t.Run("a non-nil claim decodes with Epoch wired, and leaseTTL/lockedBy pass through", func(t *testing.T) {
		t.Parallel()
		fd := newFakeGroupDialect()
		store, err := msginsql.NewGroupStore(openDB(t, fakeDriverName), "groups", fd,
			msginsql.WithGroupLeaseTTL(2*time.Minute), msginsql.WithGroupLockedBy("worker-1"))
		require.NoError(t, err)

		m1 := msgin.New[any]([]byte("p1"), msgin.WithID("m-1"))
		_, err = store.Add(t.Context(), "corr-1", m1)
		require.NoError(t, err)

		claim, err := store.ClaimGroup(t.Context(), "corr-1")
		require.NoError(t, err)
		require.NotNil(t, claim)
		assert.Equal(t, "corr-1", claim.Key())
		require.Len(t, claim.Messages(), 1)
		assert.Equal(t, "m-1", claim.Messages()[0].ID())
		assert.Equal(t, int64(1), claim.Epoch())

		assert.Equal(t, "worker-1", fd.lastClaimLockedBy)
		assert.Equal(t, 2*time.Minute, fd.lastClaimLeaseTTL)
	})

	t.Run("a dialect ClaimGroup error, with schema missing, is wrapped ErrSchemaNotReady naming the table", func(t *testing.T) {
		t.Parallel()
		fd := newFakeGroupDialect()
		fd.claimGroupErr = errors.New("claim boom")
		store, err := msginsql.NewGroupStore(openDB(t, fakeDriverName), "groups", fd)
		require.NoError(t, err)

		_, err = store.ClaimGroup(t.Context(), "corr-1")
		require.ErrorIs(t, err, msginsql.ErrSchemaNotReady)
		assert.Contains(t, err.Error(), `"groups"`)
	})

	t.Run("a claimed member with malformed framed headers surfaces a decode error", func(t *testing.T) {
		t.Parallel()
		fd := newFakeGroupDialect()
		fd.markGroupReady("groups")
		// Seed a group with a member carrying malformed framed headers, unleased
		// (lockedBy="") and live (claimedEpoch=0), so ClaimGroup can claim it.
		badRow := &fakeGroupRow{msgID: "m-1", headers: []byte("{not json"), payload: []byte("p")}
		fd.seedClaimed("corr-bad", "", 0, badRow)

		store, err := msginsql.NewGroupStore(openDB(t, fakeDriverName), "groups", fd)
		require.NoError(t, err)

		_, err = store.ClaimGroup(t.Context(), "corr-bad")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "m-1")
	})
}

// TestGroupStore_SettleGroup covers claim.Epoch()+lockedBy pass-through, the
// applied=false-is-not-an-error contract, and dialect error propagation.
func TestGroupStore_SettleGroup(t *testing.T) {
	t.Parallel()

	t.Run("passes claim.Epoch() and the configured lockedBy through; applied=true settles cleanly", func(t *testing.T) {
		t.Parallel()
		fd := newFakeGroupDialect()
		store, err := msginsql.NewGroupStore(openDB(t, fakeDriverName), "groups", fd,
			msginsql.WithGroupLockedBy("worker-1"))
		require.NoError(t, err)

		_, err = store.Add(t.Context(), "corr-1", msgin.New[any]([]byte("p"), msgin.WithID("m-1")))
		require.NoError(t, err)
		claim, err := store.ClaimGroup(t.Context(), "corr-1")
		require.NoError(t, err)
		require.NotNil(t, claim)

		require.NoError(t, store.SettleGroup(t.Context(), claim))
		assert.Equal(t, "worker-1", fd.lastSettleLockedBy)
		assert.False(t, fd.groupExists("corr-1"), "settling the only claimed member deletes the group")
	})

	t.Run("dialect applied=false (fence miss) is NOT an error", func(t *testing.T) {
		t.Parallel()
		fd := newFakeGroupDialect()
		store, err := msginsql.NewGroupStore(openDB(t, fakeDriverName), "groups", fd, msginsql.WithGroupLockedBy("worker-1"))
		require.NoError(t, err)

		_, err = store.Add(t.Context(), "corr-1", msgin.New[any]([]byte("p"), msgin.WithID("m-1")))
		require.NoError(t, err)
		claim, err := store.ClaimGroup(t.Context(), "corr-1")
		require.NoError(t, err)
		require.NotNil(t, claim)

		fd.settleApplied = false // simulate a fence miss (lease stolen/already settled)
		require.NoError(t, store.SettleGroup(t.Context(), claim))
	})

	t.Run("a dialect SettleGroup error, with schema present, propagates raw", func(t *testing.T) {
		t.Parallel()
		fd := newFakeGroupDialect()
		fd.markGroupReady("groups")
		fd.settleGroupErr = errors.New("settle boom")
		store, err := msginsql.NewGroupStore(openDB(t, fakeDriverName), "groups", fd)
		require.NoError(t, err)

		_, err = store.Add(t.Context(), "corr-1", msgin.New[any]([]byte("p"), msgin.WithID("m-1")))
		require.NoError(t, err)
		claim, err := store.ClaimGroup(t.Context(), "corr-1")
		require.NoError(t, err)
		require.NotNil(t, claim)

		err = store.SettleGroup(t.Context(), claim)
		require.Error(t, err)
		assert.NotErrorIs(t, err, msginsql.ErrSchemaNotReady)
		assert.Contains(t, err.Error(), "settle boom")
	})
}

// TestGroupStore_AbandonGroup covers claim.Epoch()+lockedBy pass-through, the
// applied=false-is-not-an-error contract, and dialect error propagation —
// mirroring TestGroupStore_SettleGroup.
func TestGroupStore_AbandonGroup(t *testing.T) {
	t.Parallel()

	t.Run("passes claim.Epoch() and the configured lockedBy through; applied=true restores the group to live", func(t *testing.T) {
		t.Parallel()
		fd := newFakeGroupDialect()
		store, err := msginsql.NewGroupStore(openDB(t, fakeDriverName), "groups", fd, msginsql.WithGroupLockedBy("worker-1"))
		require.NoError(t, err)

		_, err = store.Add(t.Context(), "corr-1", msgin.New[any]([]byte("p"), msgin.WithID("m-1")))
		require.NoError(t, err)
		claim, err := store.ClaimGroup(t.Context(), "corr-1")
		require.NoError(t, err)
		require.NotNil(t, claim)

		require.NoError(t, store.AbandonGroup(t.Context(), claim))
		assert.Equal(t, "worker-1", fd.lastAbandonLockedBy)
		assert.True(t, fd.groupExists("corr-1"), "abandon restores members to live, does not delete")

		// The group is claimable again after an abandon.
		reclaim, err := store.ClaimGroup(t.Context(), "corr-1")
		require.NoError(t, err)
		require.NotNil(t, reclaim)
		assert.Len(t, reclaim.Messages(), 1)
	})

	t.Run("dialect applied=false (fence miss) is NOT an error", func(t *testing.T) {
		t.Parallel()
		fd := newFakeGroupDialect()
		store, err := msginsql.NewGroupStore(openDB(t, fakeDriverName), "groups", fd, msginsql.WithGroupLockedBy("worker-1"))
		require.NoError(t, err)

		_, err = store.Add(t.Context(), "corr-1", msgin.New[any]([]byte("p"), msgin.WithID("m-1")))
		require.NoError(t, err)
		claim, err := store.ClaimGroup(t.Context(), "corr-1")
		require.NoError(t, err)
		require.NotNil(t, claim)

		fd.abandonApplied = false
		require.NoError(t, store.AbandonGroup(t.Context(), claim))
	})

	t.Run("a dialect AbandonGroup error, with schema present, propagates raw", func(t *testing.T) {
		t.Parallel()
		fd := newFakeGroupDialect()
		fd.markGroupReady("groups")
		fd.abandonGroupErr = errors.New("abandon boom")
		store, err := msginsql.NewGroupStore(openDB(t, fakeDriverName), "groups", fd)
		require.NoError(t, err)

		_, err = store.Add(t.Context(), "corr-1", msgin.New[any]([]byte("p"), msgin.WithID("m-1")))
		require.NoError(t, err)
		claim, err := store.ClaimGroup(t.Context(), "corr-1")
		require.NoError(t, err)
		require.NotNil(t, claim)

		err = store.AbandonGroup(t.Context(), claim)
		require.Error(t, err)
		assert.NotErrorIs(t, err, msginsql.ErrSchemaNotReady)
		assert.Contains(t, err.Error(), "abandon boom")
	})
}

// TestGroupStore_Expired covers the []GroupRows -> []MessageGroup decode
// mapping, the store's leaseTTL pass-through, and dialect error propagation.
func TestGroupStore_Expired(t *testing.T) {
	t.Parallel()

	t.Run("maps decoded groups and passes the store's leaseTTL through", func(t *testing.T) {
		t.Parallel()
		fd := newFakeGroupDialect()
		fixedNow := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		fd.now = func() time.Time { return fixedNow }
		store, err := msginsql.NewGroupStore(openDB(t, fakeDriverName), "groups", fd,
			msginsql.WithGroupLeaseTTL(90*time.Second))
		require.NoError(t, err)

		_, err = store.Add(t.Context(), "corr-1", msgin.New[any]([]byte("p"), msgin.WithID("m-1")))
		require.NoError(t, err)

		groups, err := store.Expired(t.Context(), fixedNow.Add(time.Minute))
		require.NoError(t, err)
		require.Len(t, groups, 1)
		assert.Equal(t, "corr-1", groups[0].Key())
		assert.Equal(t, fixedNow, groups[0].CreatedAt())
		require.Len(t, groups[0].Messages(), 1)
		assert.Equal(t, "m-1", groups[0].Messages()[0].ID())

		assert.Equal(t, 90*time.Second, fd.lastExpiredLeaseTTL)
	})

	t.Run("an unexpired group is excluded", func(t *testing.T) {
		t.Parallel()
		fd := newFakeGroupDialect()
		fixedNow := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		fd.now = func() time.Time { return fixedNow }
		store, err := msginsql.NewGroupStore(openDB(t, fakeDriverName), "groups", fd)
		require.NoError(t, err)

		_, err = store.Add(t.Context(), "corr-1", msgin.New[any]([]byte("p"), msgin.WithID("m-1")))
		require.NoError(t, err)

		groups, err := store.Expired(t.Context(), fixedNow.Add(-time.Minute))
		require.NoError(t, err)
		assert.Empty(t, groups)
	})

	t.Run("a dialect ExpiredGroups error, with schema present, propagates raw", func(t *testing.T) {
		t.Parallel()
		fd := newFakeGroupDialect()
		fd.markGroupReady("groups")
		fd.expiredErr = errors.New("expired boom")
		store, err := msginsql.NewGroupStore(openDB(t, fakeDriverName), "groups", fd)
		require.NoError(t, err)

		_, err = store.Expired(t.Context(), time.Now())
		require.Error(t, err)
		assert.NotErrorIs(t, err, msginsql.ErrSchemaNotReady)
		assert.Contains(t, err.Error(), "expired boom")
	})

	t.Run("an expired group with a malformed framed member surfaces a decode error", func(t *testing.T) {
		t.Parallel()
		fd := newFakeGroupDialect()
		fd.markGroupReady("groups")
		badRow := &fakeGroupRow{msgID: "m-1", headers: []byte("{not json"), payload: []byte("p")}
		fd.seedClaimed("corr-bad", "", 0, badRow) // unleased, so it is a candidate

		store, err := msginsql.NewGroupStore(openDB(t, fakeDriverName), "groups", fd)
		require.NoError(t, err)

		_, err = store.Expired(t.Context(), time.Now().Add(time.Hour))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "m-1")
	})
}

// TestGroupStore_ReadyAndEnsureSchema covers Ready delegating to
// dialect.SchemaExists (both the ready and not-ready branches) and
// EnsureSchema delegating to dialect.EnsureGroupSchema.
func TestGroupStore_ReadyAndEnsureSchema(t *testing.T) {
	t.Parallel()

	type testCase struct {
		name       string
		callEnsure bool  // whether the test calls store.EnsureSchema before Ready
		schemaErr  error // when set, fd.schemaExistsErr is injected before Ready
		assert     func(t *testing.T, fd *fakeGroupDialect, err error)
	}

	cases := []testCase{
		{
			name:       "Ready returns ErrSchemaNotReady when the dialect reports the table missing",
			callEnsure: false,
			assert: func(t *testing.T, fd *fakeGroupDialect, err error) {
				require.ErrorIs(t, err, msginsql.ErrSchemaNotReady)
			},
		},
		{
			name:       "EnsureSchema delegates to EnsureGroupSchema so Ready then succeeds",
			callEnsure: true,
			assert: func(t *testing.T, fd *fakeGroupDialect, err error) {
				require.NoError(t, err)
			},
		},
		{
			name:      "Ready passes a genuine SchemaExists infrastructure error through unwrapped",
			schemaErr: errors.New("probe boom"),
			assert: func(t *testing.T, fd *fakeGroupDialect, err error) {
				require.Error(t, err)
				assert.NotErrorIs(t, err, msginsql.ErrSchemaNotReady)
				assert.Contains(t, err.Error(), "probe boom")
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fd := newFakeGroupDialect()
			store, err := msginsql.NewGroupStore(openDB(t, fakeDriverName), "groups", fd)
			require.NoError(t, err)

			if tc.callEnsure {
				require.NoError(t, store.EnsureSchema(t.Context()))
			}
			fd.schemaExistsErr = tc.schemaErr
			tc.assert(t, fd, store.Ready(t.Context()))
		})
	}
}

// TestNewGroupStore_NilOptionElement proves a nil ELEMENT of opts is a bare
// ErrNilFunc naming the computed 0-based index (Spec 015 §3.1, family R1) rather
// than a panic. NewGroupStore is LOOP-FIRST (Spec 015 §3.5): the apply loop is
// the constructor's first statement that can fail, preceded only by the
// config-defaults initializer, which cannot fail, so a nil option beats newGroupBase's
// db/table/dialect validation AND the WithGroupLeaseTTL range check.
func TestNewGroupStore_NilOptionElement(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		nilDB   bool
		dialect msginsql.GroupDialect
		opts    []msginsql.GroupStoreOption
		assert  func(t *testing.T, err error)
	}{
		{
			// AC-1 (no panic) + AC-5 (the R1 wrap is BARE).
			name:    "nil element alone",
			dialect: newFakeGroupDialect(),
			opts:    []msginsql.GroupStoreOption{nil},
			assert: func(t *testing.T, err error) {
				require.ErrorIs(t, err, msgin.ErrNilFunc)
				assert.False(t, msgin.IsPermanent(err),
					"R1 nil-option error must be BARE, not Permanent-wrapped")
				assert.Contains(t, err.Error(), "sql.NewGroupStore: nil option at index 0")
			},
		},
		{
			// AC-2: computed index, asserted as the FULL position string.
			name:    "nil element after a valid option reports the computed index",
			dialect: newFakeGroupDialect(),
			opts:    []msginsql.GroupStoreOption{msginsql.WithGroupLockedBy("worker-1"), nil},
			assert: func(t *testing.T, err error) {
				require.ErrorIs(t, err, msgin.ErrNilFunc)
				assert.Contains(t, err.Error(), "sql.NewGroupStore: nil option at index 1")
			},
		},
		{
			// AC-3: first-nil-wins.
			name:    "first of two nils wins",
			dialect: newFakeGroupDialect(),
			opts:    []msginsql.GroupStoreOption{nil, nil},
			assert: func(t *testing.T, err error) {
				require.ErrorIs(t, err, msgin.ErrNilFunc)
				assert.Contains(t, err.Error(), "sql.NewGroupStore: nil option at index 0")
			},
		},
		{
			// Precedence (Spec 015 §3.5): LOOP-FIRST, so the nil option beats
			// the nil-db check inside newGroupBase.
			name:    "nil option precedes the nil-db validation",
			nilDB:   true,
			dialect: newFakeGroupDialect(),
			opts:    []msginsql.GroupStoreOption{nil},
			assert: func(t *testing.T, err error) {
				require.ErrorIs(t, err, msgin.ErrNilFunc)
				assert.NotErrorIs(t, err, msgin.ErrNilAdapter)
				assert.Contains(t, err.Error(), "sql.NewGroupStore: nil option at index 0")
			},
		},
		{
			// Precedence: also beats the explicit-WithGroupLeaseTTL range
			// check, which sits after the loop.
			name:    "nil option precedes the WithGroupLeaseTTL validation",
			dialect: newFakeGroupDialect(),
			opts:    []msginsql.GroupStoreOption{msginsql.WithGroupLeaseTTL(0), nil},
			assert: func(t *testing.T, err error) {
				require.ErrorIs(t, err, msgin.ErrNilFunc)
				assert.NotErrorIs(t, err, msginsql.ErrInvalidLeaseTTL)
				assert.Contains(t, err.Error(), "sql.NewGroupStore: nil option at index 1")
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var db *sql.DB
			if !tc.nilDB {
				db = openDB(t, fakeDriverName)
			}
			_, err := msginsql.NewGroupStore(db, "groups", tc.dialect, tc.opts...)
			tc.assert(t, err)
		})
	}
}

// TestNewGroupStore_MaxGroupMembersRange covers WithMaxGroupMembers' checkRange
// arms (Spec 017 §6 AC-2 / AC-2b; Plan 031 Task 5 branches B5-1..B5-3): both
// ends of [1, 1<<20] asserted with the FULL render, not merely errors.Is, so a
// message that lies at either end ("0 exceeds 1048576") cannot pass.
//
// The ceiling is exercised by the CONSTRUCTOR only — no group is ever grown to
// it (Spec 017 §6 AC-6: 65,536 members costs 8.6 s and 48.3 GiB of churn).
func TestNewGroupStore_MaxGroupMembersRange(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		n      int
		assert func(t *testing.T, store *msginsql.GroupStore, err error)
	}{
		{
			name: "zero is rejected at the lower arm",
			n:    0,
			assert: func(t *testing.T, store *msginsql.GroupStore, err error) {
				require.ErrorIs(t, err, msgin.ErrInvalidCapacity)
				assert.False(t, msgin.IsPermanent(err), "R1 constructor error stays bare (ADR 0029 D-M)")
				assert.EqualError(t, err,
					"msgin: capacity out of range: sql.WithMaxGroupMembers: 0 not in [1, 1048576]")
				assert.Nil(t, store)
			},
		},
		{
			name: "the ceiling itself is accepted",
			n:    1 << 20,
			assert: func(t *testing.T, store *msginsql.GroupStore, err error) {
				require.NoError(t, err)
				assert.NotNil(t, store)
			},
		},
		{
			name: "ceiling+1 is rejected at the upper arm",
			n:    1<<20 + 1,
			assert: func(t *testing.T, store *msginsql.GroupStore, err error) {
				require.ErrorIs(t, err, msgin.ErrInvalidCapacity)
				assert.EqualError(t, err,
					"msgin: capacity out of range: sql.WithMaxGroupMembers: 1048577 not in [1, 1048576]")
				assert.Nil(t, store)
			},
		},
		{
			name: "one is accepted at the lower boundary",
			n:    1,
			assert: func(t *testing.T, store *msginsql.GroupStore, err error) {
				require.NoError(t, err)
				assert.NotNil(t, store)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			store, err := msginsql.NewGroupStore(openDB(t, fakeDriverName), "groups", newFakeGroupDialect(),
				msginsql.WithMaxGroupMembers(tc.n))
			tc.assert(t, store, err)
		})
	}
}

// TestGroupStore_AddThreadsAndPropagatesTheMemberCap covers the store's half of
// the in-transaction bound (Spec 017 §3.6 / §3.6.3; Plan 031 Task 5 branches
// B5-4..B5-7): Add threads the CONFIGURED cap into GroupDialect.AddMember, and
// propagates the dialect's rejection — sentinel, Permanent marker AND the live
// snapshot that rides out with it (D-AN) — through classifyQueryErr unchanged.
//
// The dialect's own enforcement is proven against real engines in the harness
// conformance suite; a render assertion through this fake would be vacuous,
// since the fake mints whatever the test hands it (Spec 017 §6 AC-2c).
func TestGroupStore_AddThreadsAndPropagatesTheMemberCap(t *testing.T) {
	t.Parallel()

	// overflow renders what a real dialect renders, so the propagation
	// assertions below are about the STORE, not about this string.
	overflow := func(key string, n, max int) error {
		return fmt.Errorf("%w: msgin/sql/fake: AddMember: group %q holds %d members, limit %d",
			msgin.ErrOverflowDropped, key, n, max)
	}

	t.Run("Add threads the configured cap into AddMember", func(t *testing.T) {
		t.Parallel()
		fd := newFakeGroupDialect()
		store, err := msginsql.NewGroupStore(openDB(t, fakeDriverName), "groups", fd,
			msginsql.WithMaxGroupMembers(7))
		require.NoError(t, err)

		_, err = store.Add(t.Context(), "corr-1", msgin.New[any]([]byte("p"), msgin.WithID("m-1")))
		require.NoError(t, err)
		assert.Equal(t, 7, fd.lastAddMaxMembers,
			"the store must pass its CONFIGURED cap, not a literal")
	})

	t.Run("Add threads the 65,536 default when the option is unset", func(t *testing.T) {
		t.Parallel()
		fd := newFakeGroupDialect()
		store, err := msginsql.NewGroupStore(openDB(t, fakeDriverName), "groups", fd)
		require.NoError(t, err)

		_, err = store.Add(t.Context(), "corr-1", msgin.New[any]([]byte("p"), msgin.WithID("m-1")))
		require.NoError(t, err)
		assert.Equal(t, 1<<16, fd.lastAddMaxMembers)
	})

	// D-AU (Spec 017 §3.6a.1, review finding R-7): AddMember's overflow
	// classification tests lease EXPIRY, and it can only do that if the store
	// threads its CONFIGURED lease TTL — exactly as it already does for
	// ClaimGroup and ExpiredGroups. A zero value here would make every stamped
	// lease read as expired.
	t.Run("Add threads the configured lease TTL into AddMember", func(t *testing.T) {
		t.Parallel()
		fd := newFakeGroupDialect()
		store, err := msginsql.NewGroupStore(openDB(t, fakeDriverName), "groups", fd,
			msginsql.WithGroupLeaseTTL(90*time.Second))
		require.NoError(t, err)

		_, err = store.Add(t.Context(), "corr-1", msgin.New[any]([]byte("p"), msgin.WithID("m-1")))
		require.NoError(t, err)
		assert.Equal(t, 90*time.Second, fd.lastAddLeaseTTL,
			"the store must pass its CONFIGURED lease TTL, not a literal or the zero value")
	})

	t.Run("Add threads the 5m default lease TTL when the option is unset", func(t *testing.T) {
		t.Parallel()
		fd := newFakeGroupDialect()
		store, err := msginsql.NewGroupStore(openDB(t, fakeDriverName), "groups", fd)
		require.NoError(t, err)

		_, err = store.Add(t.Context(), "corr-1", msgin.New[any]([]byte("p"), msgin.WithID("m-1")))
		require.NoError(t, err)
		assert.Equal(t, 5*time.Minute, fd.lastAddLeaseTTL,
			"the same default ClaimGroup gets; AddMember must not see a zero TTL")
	})

	t.Run("a permanent dialect overflow propagates with its marker and sentinel intact", func(t *testing.T) {
		t.Parallel()
		fd := newFakeGroupDialect()
		fd.markGroupReady("groups")
		fd.addMemberErr = msgin.Permanent(overflow("corr-1", 5, 4))
		store, err := msginsql.NewGroupStore(openDB(t, fakeDriverName), "groups", fd,
			msginsql.WithMaxGroupMembers(4))
		require.NoError(t, err)

		_, err = store.Add(t.Context(), "corr-1", msgin.New[any]([]byte("p"), msgin.WithID("m-5")))
		require.ErrorIs(t, err, msgin.ErrOverflowDropped)
		assert.True(t, msgin.IsPermanent(err),
			"the dialect's not-leased classification must survive the store (D-AM)")
		assert.NotErrorIs(t, err, msginsql.ErrSchemaNotReady)
	})

	t.Run("a leased (transient) dialect overflow stays transient", func(t *testing.T) {
		t.Parallel()
		fd := newFakeGroupDialect()
		fd.markGroupReady("groups")
		fd.addMemberErr = overflow("corr-1", 5, 4)
		store, err := msginsql.NewGroupStore(openDB(t, fakeDriverName), "groups", fd,
			msginsql.WithMaxGroupMembers(4))
		require.NoError(t, err)

		_, err = store.Add(t.Context(), "corr-1", msgin.New[any]([]byte("p"), msgin.WithID("m-5")))
		require.ErrorIs(t, err, msgin.ErrOverflowDropped)
		assert.False(t, msgin.IsPermanent(err),
			"a claim is in flight; the store must not upgrade the classification")
	})

	t.Run("the dialect's live snapshot rides out ALONGSIDE the overflow error", func(t *testing.T) {
		t.Parallel()
		headers, err := msginsql.EncodeHeaders(
			msgin.NewHeaders(map[string]any{msgin.HeaderMessageID: "m-1"}))
		require.NoError(t, err)

		fd := newFakeGroupDialect()
		fd.markGroupReady("groups")
		fd.addMemberErr = msgin.Permanent(overflow("corr-1", 2, 1))
		fd.addMemberRows = msginsql.GroupRows{
			GroupKey: "corr-1",
			Members:  []msginsql.MemberRow{{MsgID: "m-1", Headers: headers, Payload: []byte("p1")}},
		}
		store, err := msginsql.NewGroupStore(openDB(t, fakeDriverName), "groups", fd,
			msginsql.WithMaxGroupMembers(1))
		require.NoError(t, err)

		group, err := store.Add(t.Context(), "corr-1", msgin.New[any]([]byte("p2"), msgin.WithID("m-2")))
		require.ErrorIs(t, err, msgin.ErrOverflowDropped)
		require.NotNil(t, group,
			"Add must propagate the dialect's snapshot, or routing.Aggregator.Handle's "+
				"release re-evaluation is unreachable for sql (Spec 017 §3.3a)")
		assert.Equal(t, "corr-1", group.Key())
		require.Len(t, group.Messages(), 1)
		assert.Equal(t, "m-1", group.Messages()[0].ID())
	})

	// R-9 (Plan 031 Task 11 Step 3), the CLAUDE.md hot-path gate: the overflow
	// path's own decode-failure early return. Add's godoc promises the (nil, err)
	// shape for "an overflow whose members cannot be decoded" — a corrupt stored
	// header must not mask the rejection, and must not hand Handle a
	// half-decoded group to run a release strategy against.
	//
	// The table must EXIST for this branch to be reachable at all: with the
	// table gone, classifyQueryErr replaces the error and the discriminator
	// above exits before any decode is attempted (R-6).
	t.Run("an overflow whose snapshot cannot be decoded keeps the rejection and drops the group", func(t *testing.T) {
		t.Parallel()
		fd := newFakeGroupDialect()
		fd.markGroupReady("groups")
		fd.addMemberErr = msgin.Permanent(overflow("corr-1", 2, 1))
		fd.addMemberRows = msginsql.GroupRows{
			GroupKey: "corr-1",
			Members: []msginsql.MemberRow{
				{MsgID: "m-1", Headers: []byte("{not json"), Payload: []byte("p1")},
			},
		}
		store, err := msginsql.NewGroupStore(openDB(t, fakeDriverName), "groups", fd,
			msginsql.WithMaxGroupMembers(1))
		require.NoError(t, err)

		group, err := store.Add(t.Context(), "corr-1", msgin.New[any]([]byte("p2"), msgin.WithID("m-2")))
		assert.Nil(t, group,
			"a member whose framed headers are corrupt must not yield a partial group (R-9)")
		require.ErrorIs(t, err, msgin.ErrOverflowDropped,
			"the decode fault must not MASK the rejection the caller has to act on")
		assert.True(t, msgin.IsPermanent(err),
			"the dialect's not-leased classification survives the decode failure too")
	})

	t.Run("a NON-overflow dialect error still returns a nil group", func(t *testing.T) {
		t.Parallel()
		fd := newFakeGroupDialect()
		fd.markGroupReady("groups")
		fd.addMemberErr = errors.New("add boom")
		fd.addMemberRows = msginsql.GroupRows{GroupKey: "corr-1"}
		store, err := msginsql.NewGroupStore(openDB(t, fakeDriverName), "groups", fd)
		require.NoError(t, err)

		group, err := store.Add(t.Context(), "corr-1", msgin.New[any]([]byte("p"), msgin.WithID("m-1")))
		require.Error(t, err)
		assert.Nil(t, group, "only an overflow rejection carries a snapshot (Spec 017 §3.7)")
	})

	t.Run("the overflow marker and sentinel survive classifyQueryErr's schema probe", func(t *testing.T) {
		t.Parallel()
		fd := newFakeGroupDialect()
		fd.markGroupReady("groups") // the table EXISTS, so the probe passes the error through
		fd.addMemberErr = msgin.Permanent(overflow("corr-1", 5, 4))
		store, err := msginsql.NewGroupStore(openDB(t, fakeDriverName), "groups", fd,
			msginsql.WithMaxGroupMembers(4))
		require.NoError(t, err)

		_, err = store.Add(t.Context(), "corr-1", msgin.New[any]([]byte("p"), msgin.WithID("m-5")))
		require.ErrorIs(t, err, msgin.ErrOverflowDropped)
		assert.True(t, msgin.IsPermanent(err))
		assert.NotErrorIs(t, err, msginsql.ErrSchemaNotReady,
			"Spec 017 §6 AC-4c: with the table present the error passes through unchanged")
	})

	// R-6 (Plan 031 Task 11 Step 3): the overflow branch is discriminated on the
	// CLASSIFIED error, not on the raw dialect one, so a classifyQueryErr rewrite
	// takes the snapshot with it. A member table dropped by a bad migration
	// CONCURRENTLY with an over-cap Add reclassifies to ErrSchemaNotReady — which
	// carries neither msgin.ErrOverflowDropped nor the Permanent marker — and a
	// snapshot must not ride out beside an error that no longer states the
	// overflow contract (Add's godoc: "every other dialect failure keeps the
	// (nil, err) shape").
	t.Run("a MISSING table reclassifies the overflow as ErrSchemaNotReady AND drops the snapshot", func(t *testing.T) {
		t.Parallel()
		headers, err := msginsql.EncodeHeaders(
			msgin.NewHeaders(map[string]any{msgin.HeaderMessageID: "m-1"}))
		require.NoError(t, err)

		fd := newFakeGroupDialect() // markGroupReady NOT called: the probe reports the table absent
		fd.addMemberErr = msgin.Permanent(overflow("corr-1", 5, 4))
		// A NON-empty snapshot, so "nil" below cannot be satisfied by accident:
		// the zero GroupRows decodes to a valid zero-member group, which is a
		// non-nil msgin.MessageGroup.
		fd.addMemberRows = msginsql.GroupRows{
			GroupKey: "corr-1",
			Members:  []msginsql.MemberRow{{MsgID: "m-1", Headers: headers, Payload: []byte("p1")}},
		}
		store, err := msginsql.NewGroupStore(openDB(t, fakeDriverName), "groups", fd,
			msginsql.WithMaxGroupMembers(4))
		require.NoError(t, err)

		group, err := store.Add(t.Context(), "corr-1", msgin.New[any]([]byte("p"), msgin.WithID("m-5")))
		require.ErrorIs(t, err, msginsql.ErrSchemaNotReady,
			"classifyQueryErr's diagnosis wins when the table is genuinely gone")
		assert.NotErrorIs(t, err, msgin.ErrOverflowDropped,
			"the schema diagnosis REPLACES the rejection; it does not wrap it")
		assert.Nil(t, group,
			"the snapshot rides out only with an error that still carries the overflow "+
				"sentinel — discriminate on the CLASSIFIED error, not the raw one (R-6)")
	})

	// AC-7 / Spec 017 §3.7, the MUST-REPORT clause ONLY: an Add that would
	// exceed the bound is reported as msgin.ErrOverflowDropped. The other three
	// clauses of §3.7 are covered by the subtests above (the MUST-bound by the
	// two cap-threading cases, the SHOULD by the permanent/transient pair, the
	// MAY by the snapshot case).
	//
	// What this case uniquely buys is the INTERFACE-TYPED drive: the store is
	// held in a msgin.MessageGroupStore, so the body below is copyable verbatim
	// by a third-party store author as the executable form of the SPI clause.
	// A mutant returning a bare, non-wrapping error from Add's dialect-error
	// arm fails the ErrorIs. This asserts the STORE's propagation contract
	// against the fake dialect; the real-engine enforcement proof is the
	// harness conformance suite's.
	t.Run("the MUST-report clause holds through the msgin.MessageGroupStore interface", func(t *testing.T) {
		t.Parallel()
		fd := newFakeGroupDialect()
		fd.markGroupReady("groups")
		fd.addMemberErr = msgin.Permanent(overflow("corr-1", 2, 1))
		s, err := msginsql.NewGroupStore(openDB(t, fakeDriverName), "groups", fd,
			msginsql.WithMaxGroupMembers(1))
		require.NoError(t, err)

		var store msgin.MessageGroupStore = s

		_, err = store.Add(t.Context(), "corr-1", msgin.New[any]([]byte("p"), msgin.WithID("m-2")))
		require.ErrorIs(t, err, msgin.ErrOverflowDropped)
	})
}
