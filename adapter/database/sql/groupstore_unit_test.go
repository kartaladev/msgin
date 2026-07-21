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
	"errors"
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
