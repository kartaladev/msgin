package harness

import (
	"context"
	"database/sql"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	msgin "github.com/kartaladev/msgin"
	msginsql "github.com/kartaladev/msgin/adapter/database/sql"
	"github.com/stretchr/testify/require"
)

const (
	dialectTestLockedBy = "worker-1"
	// dialectLongLeaseTTL is comfortably longer than any test operation, so a
	// freshly claimed row is NOT re-claimable while the test still holds its
	// lease.
	dialectLongLeaseTTL = 5 * time.Minute
)

// RunDialect exercises kit.Lease's SQL directly (bypassing Source/Outbound),
// reproducing every assertion the pre-split DialectSuite made (Plan 006 Task
// 3): SchemaExists/EnsureSchema idempotency, the reference DDL (kit.DDL)
// applying cleanly, Claim bumping delivery_count/lease_epoch and locking the
// row, the empty-claim case, Ack's fence branches (correct/stale-epoch/
// wrong-owner), Nack delaying visibility, expired-lease reclaim, and a
// delayed Insert staying hidden until visible. It additionally exercises the
// dialect-author error branch audit R2-2 calls out: kit.DDL and kit.InboxDDL
// both reject an invalid table identifier with ErrInvalidTableName, so that
// branch is not silently uncovered when coverage is measured via the
// concrete dialect's dbtest -coverpkg run.
func RunDialect(t *testing.T, kit TestKit, db *sql.DB) {
	t.Helper()
	var counter atomic.Int64
	fresh := func(ctx context.Context) string { return freshTable(t, ctx, kit, db, &counter, "msgin_msgs") }

	insert := func(t *testing.T, ctx context.Context, table, msgID string, payload []byte) {
		t.Helper()
		headers, err := msginsql.EncodeHeaders(msgin.NewHeaders(map[string]any{msgin.HeaderMessageID: msgID}))
		require.NoError(t, err)
		require.NoError(t, kit.Lease.Insert(ctx, db, table, msgID, headers, payload, 0))
	}

	t.Run("SchemaExistsAndEnsureSchemaIdempotent", func(t *testing.T) {
		ctx := t.Context()
		name := fmt.Sprintf("msgin_ensure_%d", counter.Add(1))

		exists, err := kit.Lease.SchemaExists(ctx, db, name)
		require.NoError(t, err)
		require.False(t, exists, "table must not exist before EnsureSchema")

		require.NoError(t, kit.Lease.EnsureSchema(ctx, db, name))

		exists, err = kit.Lease.SchemaExists(ctx, db, name)
		require.NoError(t, err)
		require.True(t, exists, "table must exist after EnsureSchema")

		require.NoError(t, kit.Lease.EnsureSchema(ctx, db, name), "EnsureSchema must be idempotent")
	})

	t.Run("ReferenceDDLApplies", func(t *testing.T) {
		ctx := t.Context()
		name := fmt.Sprintf("msgin_refddl_%d", counter.Add(1))
		ddl, err := kit.DDL(name)
		require.NoError(t, err)

		for _, stmt := range splitStatements(ddl) {
			_, err := db.ExecContext(ctx, stmt)
			require.NoError(t, err, "apply reference DDL statement")
		}

		exists, err := kit.Lease.SchemaExists(ctx, db, name)
		require.NoError(t, err)
		require.True(t, exists)
	})

	t.Run("InvalidIdentifierRejected", func(t *testing.T) {
		const bad = "bad; name"

		_, err := kit.DDL(bad)
		require.ErrorIs(t, err, msginsql.ErrInvalidTableName, "kit.DDL must validate before building any SQL")

		_, err = kit.InboxDDL(bad)
		require.ErrorIs(t, err, msginsql.ErrInvalidTableName, "kit.InboxDDL must validate before building any SQL")
	})

	// InvalidIdentifierRejectedPerSPIMethod exercises the per-method
	// ValidateIdent/quoteTable early-return guard on EVERY LeaseDialect/
	// LockDialect/InboxDialect method that takes a table string, not just
	// kit.DDL/kit.InboxDDL above (review finding: after the dialect-module
	// split, only the reference-DDL builders were covered, leaving the guard
	// inside Claim/Ack/Nack/Insert/EnsureSchema/SchemaExists/ClaimLock and the
	// inbox methods untested ANYWHERE). This is a PUBLIC-SPI typed-error
	// hot-path branch a dialect-direct caller relies on (CLAUDE.md's
	// hot-path/typed-error rule). The guard validates and returns before
	// touching the database, so a bad identifier is sufficient — the other
	// arguments are zero values. Run against Postgres/MySQL/MariaDB via
	// dbtest, this single addition covers every shipped dialect.
	//
	// AckLock/NackLock are deliberately NOT covered here: both take a
	// *LockedRow, not a bare table string, and a real LockedRow requires a
	// successful ClaimLock first — at which point the table is already
	// valid, so there is no way to drive them with a bad identifier without
	// fabricating a fake *LockedRow, which the fix's brief explicitly rules
	// out.
	t.Run("InvalidIdentifierRejectedPerSPIMethod", func(t *testing.T) {
		const bad = "bad; name"

		lockDialect, ok := kit.Lease.(msginsql.LockDialect)

		assertInvalid := func(t *testing.T, err error) {
			require.ErrorIs(t, err, msginsql.ErrInvalidTableName)
		}

		type validationCase struct {
			name   string
			call   func(t *testing.T, ctx context.Context) error
			assert func(t *testing.T, err error)
		}

		cases := []validationCase{
			{
				name: "Claim",
				call: func(t *testing.T, ctx context.Context) error {
					_, err := kit.Lease.Claim(ctx, db, bad, 10, dialectTestLockedBy, dialectLongLeaseTTL)
					return err
				},
				assert: assertInvalid,
			},
			{
				name: "Ack",
				call: func(t *testing.T, ctx context.Context) error {
					_, err := kit.Lease.Ack(ctx, db, bad, 0, dialectTestLockedBy, 0)
					return err
				},
				assert: assertInvalid,
			},
			{
				name: "Nack",
				call: func(t *testing.T, ctx context.Context) error {
					_, err := kit.Lease.Nack(ctx, db, bad, 0, dialectTestLockedBy, 0, time.Second)
					return err
				},
				assert: assertInvalid,
			},
			{
				name: "Insert",
				call: func(t *testing.T, ctx context.Context) error {
					return kit.Lease.Insert(ctx, db, bad, "m", nil, nil, 0)
				},
				assert: assertInvalid,
			},
			{
				name: "EnsureSchema",
				call: func(t *testing.T, ctx context.Context) error {
					return kit.Lease.EnsureSchema(ctx, db, bad)
				},
				assert: assertInvalid,
			},
			{
				name: "SchemaExists",
				call: func(t *testing.T, ctx context.Context) error {
					_, err := kit.Lease.SchemaExists(ctx, db, bad)
					return err
				},
				assert: assertInvalid,
			},
			{
				name: "InsertInboxIfAbsent",
				call: func(t *testing.T, ctx context.Context) error {
					_, err := kit.Inbox.InsertInboxIfAbsent(ctx, db, bad, "m")
					return err
				},
				assert: assertInvalid,
			},
			{
				name: "PurgeInbox",
				call: func(t *testing.T, ctx context.Context) error {
					_, err := kit.Inbox.PurgeInbox(ctx, db, bad, time.Hour)
					return err
				},
				assert: assertInvalid,
			},
			{
				name: "EnsureInboxSchema",
				call: func(t *testing.T, ctx context.Context) error {
					return kit.Inbox.EnsureInboxSchema(ctx, db, bad)
				},
				assert: assertInvalid,
			},
			{
				name: "MsgIDUniqueIndexExists",
				call: func(t *testing.T, ctx context.Context) error {
					_, err := kit.Inbox.MsgIDUniqueIndexExists(ctx, db, bad)
					return err
				},
				assert: assertInvalid,
			},
		}

		// ClaimLock is the only LockDialect method with a bare-table-string signature;
		// cover its invalid-identifier guard only when the dialect implements the
		// segregated lock SPI. A lease-only dialect (e.g. sqlite) has no ClaimLock, so
		// this case is skipped rather than hard-required (ADR 0012 D8). AckLock/NackLock
		// take a *LockedRow, not a table string, so they are not covered here (as before).
		if ok {
			cases = append(cases, validationCase{
				name: "ClaimLock",
				call: func(t *testing.T, ctx context.Context) error {
					_, err := lockDialect.ClaimLock(ctx, db, bad, dialectTestLockedBy)
					return err
				},
				assert: assertInvalid,
			})
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				err := tc.call(t, t.Context())
				tc.assert(t, err)
			})
		}
	})

	t.Run("ClaimBumpsCountsAndLocks", func(t *testing.T) {
		ctx := t.Context()
		table := fresh(ctx)
		insert(t, ctx, table, "m-1", []byte("hello"))

		rows, err := kit.Lease.Claim(ctx, db, table, 10, dialectTestLockedBy, dialectLongLeaseTTL)
		require.NoError(t, err)
		require.Len(t, rows, 1)
		require.Equal(t, "m-1", rows[0].MsgID)
		require.Equal(t, []byte("hello"), rows[0].Payload)
		require.Equal(t, 1, rows[0].DeliveryCount, "delivery_count post-increment")
		require.Equal(t, int64(1), rows[0].LeaseEpoch, "lease_epoch post-increment")

		hdrs, err := msginsql.DecodeHeaders(rows[0].Headers)
		require.NoError(t, err)
		id, ok := hdrs.String(msgin.HeaderMessageID)
		require.True(t, ok)
		require.Equal(t, "m-1", id)

		rows, err = kit.Lease.Claim(ctx, db, table, 10, dialectTestLockedBy, dialectLongLeaseTTL)
		require.NoError(t, err)
		require.Empty(t, rows, "a live-leased row is not re-claimable")
	})

	t.Run("ClaimEmptyWhenNoRows", func(t *testing.T) {
		ctx := t.Context()
		table := fresh(ctx)
		rows, err := kit.Lease.Claim(ctx, db, table, 10, dialectTestLockedBy, dialectLongLeaseTTL)
		require.NoError(t, err)
		require.Empty(t, rows)
	})

	t.Run("AckFenced", func(t *testing.T) {
		ctx := t.Context()

		cases := []struct {
			name   string
			mutate func(id int64, epoch int64) (int64, string, int64)
			assert func(t *testing.T, applied bool, err error)
		}{
			{
				name: "correct fence deletes the row",
				mutate: func(id, epoch int64) (int64, string, int64) {
					return id, dialectTestLockedBy, epoch
				},
				assert: func(t *testing.T, applied bool, err error) {
					require.NoError(t, err)
					require.True(t, applied)
				},
			},
			{
				name: "stale epoch is a no-op",
				mutate: func(id, epoch int64) (int64, string, int64) {
					return id, dialectTestLockedBy, epoch + 99
				},
				assert: func(t *testing.T, applied bool, err error) {
					require.NoError(t, err)
					require.False(t, applied)
				},
			},
			{
				name: "wrong owner is a no-op",
				mutate: func(id, epoch int64) (int64, string, int64) {
					return id, "someone-else", epoch
				},
				assert: func(t *testing.T, applied bool, err error) {
					require.NoError(t, err)
					require.False(t, applied)
				},
			},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				table := fresh(ctx)
				insert(t, ctx, table, "ack-msg", []byte("x"))

				claimed, err := kit.Lease.Claim(ctx, db, table, 1, dialectTestLockedBy, dialectLongLeaseTTL)
				require.NoError(t, err)
				require.Len(t, claimed, 1)

				id, owner, epoch := tc.mutate(claimed[0].ID, claimed[0].LeaseEpoch)
				applied, err := kit.Lease.Ack(ctx, db, table, id, owner, epoch)
				tc.assert(t, applied, err)
			})
		}
	})

	t.Run("NackDelaysVisibility", func(t *testing.T) {
		ctx := t.Context()
		table := fresh(ctx)
		insert(t, ctx, table, "nack-msg", []byte("y"))

		claimed, err := kit.Lease.Claim(ctx, db, table, 1, dialectTestLockedBy, dialectLongLeaseTTL)
		require.NoError(t, err)
		require.Len(t, claimed, 1)

		const delay = 1500 * time.Millisecond
		applied, err := kit.Lease.Nack(ctx, db, table, claimed[0].ID, dialectTestLockedBy, claimed[0].LeaseEpoch, delay)
		require.NoError(t, err)
		require.True(t, applied)

		rows, err := kit.Lease.Claim(ctx, db, table, 1, dialectTestLockedBy, dialectLongLeaseTTL)
		require.NoError(t, err)
		require.Empty(t, rows, "nacked row must stay invisible until the delay elapses")

		time.Sleep(delay + 1*time.Second)

		rows, err = kit.Lease.Claim(ctx, db, table, 1, dialectTestLockedBy, dialectLongLeaseTTL)
		require.NoError(t, err)
		require.Len(t, rows, 1, "nacked row becomes claimable after the delay")
		require.Equal(t, 2, rows[0].DeliveryCount, "re-claim bumps delivery_count again")
		require.Equal(t, int64(2), rows[0].LeaseEpoch)
	})

	t.Run("ExpiredLeaseReclaim", func(t *testing.T) {
		ctx := t.Context()
		table := fresh(ctx)
		insert(t, ctx, table, "lease-msg", []byte("z"))

		const shortLeaseTTL = 1 * time.Second

		claimed, err := kit.Lease.Claim(ctx, db, table, 1, dialectTestLockedBy, shortLeaseTTL)
		require.NoError(t, err)
		require.Len(t, claimed, 1)
		require.Equal(t, 1, claimed[0].DeliveryCount)

		rows, err := kit.Lease.Claim(ctx, db, table, 1, dialectTestLockedBy, shortLeaseTTL)
		require.NoError(t, err)
		require.Empty(t, rows, "lease not yet expired")

		time.Sleep(shortLeaseTTL + 1500*time.Millisecond)

		rows, err = kit.Lease.Claim(ctx, db, table, 1, dialectTestLockedBy, shortLeaseTTL)
		require.NoError(t, err)
		require.Len(t, rows, 1, "expired lease is reclaimable")
		require.Equal(t, 2, rows[0].DeliveryCount, "reclaim bumps delivery_count")
		require.Equal(t, int64(2), rows[0].LeaseEpoch, "reclaim bumps lease_epoch")
	})

	t.Run("InsertDelayHidesUntilVisible", func(t *testing.T) {
		ctx := t.Context()
		table := fresh(ctx)
		headers, err := msginsql.EncodeHeaders(msgin.NewHeaders(nil))
		require.NoError(t, err)

		const delay = 1500 * time.Millisecond
		require.NoError(t, kit.Lease.Insert(ctx, db, table, "delayed", headers, []byte("d"), delay))

		rows, err := kit.Lease.Claim(ctx, db, table, 1, dialectTestLockedBy, dialectLongLeaseTTL)
		require.NoError(t, err)
		require.Empty(t, rows, "delayed insert is invisible until visible_after")

		time.Sleep(delay + 1*time.Second)

		rows, err = kit.Lease.Claim(ctx, db, table, 1, dialectTestLockedBy, dialectLongLeaseTTL)
		require.NoError(t, err)
		require.Len(t, rows, 1)
	})
}
