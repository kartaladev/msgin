package harness

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	msginsql "github.com/kartaladev/msgin/adapter/database/sql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// RunInbox exercises the idempotent-consumer dedup inbox
// (msginsql.InboxDeduper, ADR 0010 D10) against kit.Inbox on the already-open
// db, reproducing every assertion the pre-split InboxSuite made (Plan 006
// Task 3): idempotency across redelivery, the atomic-with-business-tx
// silent-loss guard (a rolled-back attempt is genuinely retryable, never
// falsely deduped), the MySQL-family INSERT-IGNORE false-positive guard
// (skipped on a non-MySQL-family kit — audit R2-2's dialect-error-branch
// exercise), manual Purge retention, Ready/EnsureSchema (including applying
// the reference InboxDDL), the missing-unique-constraint fail-fast, and the
// sensible "msgin_inbox" default table name.
func RunInbox(t *testing.T, kit TestKit, db *sql.DB) {
	t.Helper()
	var counter int64
	nextName := func(prefix string) string {
		counter++
		return fmt.Sprintf("%s_%d", prefix, counter)
	}

	freshDeduper := func(t *testing.T, ctx context.Context, opts ...msginsql.InboxOption) (*msginsql.InboxDeduper, string) {
		t.Helper()
		name := nextName("msgin_inbox")
		opts = append([]msginsql.InboxOption{msginsql.WithInboxTable(name)}, opts...)
		d, err := msginsql.NewInboxDeduper(db, kit.Inbox, opts...)
		require.NoError(t, err)
		require.NoError(t, d.EnsureSchema(ctx), "EnsureSchema must provision the inbox table")
		return d, name
	}

	freshBiz := func(t *testing.T, ctx context.Context) string {
		t.Helper()
		name := nextName("msgin_biz")
		_, err := db.ExecContext(ctx, fmt.Sprintf("CREATE TABLE %s (seq INTEGER)", kit.Quote(name)))
		require.NoError(t, err)
		return name
	}

	inboxHas := func(t *testing.T, ctx context.Context, table, msgID string) bool {
		t.Helper()
		var n int
		q := fmt.Sprintf("SELECT count(*) FROM %s WHERE msg_id = %s", kit.Quote(table), kit.Placeholder(1))
		require.NoError(t, db.QueryRowContext(ctx, q, msgID).Scan(&n))
		return n > 0
	}

	markInTx := func(t *testing.T, ctx context.Context, d *msginsql.InboxDeduper, msgID string, work func(tx *sql.Tx) error, finalize func(tx *sql.Tx) error) (already bool, err error) {
		t.Helper()
		tx, err := db.BeginTx(ctx, nil)
		require.NoError(t, err)

		already, err = d.MarkProcessed(ctx, tx, msgID)
		if err != nil {
			_ = tx.Rollback()
			return already, err
		}
		if !already && work != nil {
			require.NoError(t, work(tx))
		}
		require.NoError(t, finalize(tx))
		return already, nil
	}

	commitTx := func(tx *sql.Tx) error { return tx.Commit() }
	rollbackTx := func(tx *sql.Tx) error { return tx.Rollback() }

	t.Run("Idempotency", func(t *testing.T) {
		ctx := t.Context()

		d, inbox := freshDeduper(t, ctx)
		biz := freshBiz(t, ctx)

		insertBizRow := func(tx *sql.Tx) error {
			_, werr := tx.ExecContext(ctx, fmt.Sprintf("INSERT INTO %s (seq) VALUES (1)", kit.Quote(biz)))
			return werr
		}

		already, err := markInTx(t, ctx, d, "id-A", insertBizRow, commitTx)
		require.NoError(t, err)
		assert.False(t, already, "first MarkProcessed of an id must report not-a-duplicate")

		already, err = markInTx(t, ctx, d, "id-A", func(tx *sql.Tx) error {
			t.Fatal("business work must NOT run for an already-processed id")
			return nil
		}, rollbackTx)
		require.NoError(t, err)
		assert.True(t, already, "a redelivered id must report a duplicate")

		already, err = markInTx(t, ctx, d, "id-B", insertBizRow, commitTx)
		require.NoError(t, err)
		assert.False(t, already, "a distinct id is not a duplicate")

		assert.Equal(t, 2, rowCount(t, ctx, kit, db, biz), "business effect applied once per distinct id (id-A once, id-B once)")
		assert.True(t, inboxHas(t, ctx, inbox, "id-A"), "id-A is recorded")
		assert.True(t, inboxHas(t, ctx, inbox, "id-B"), "id-B is recorded")
	})

	t.Run("AtomicRollbackIsRetryable", func(t *testing.T) {
		ctx := t.Context()

		d, inbox := freshDeduper(t, ctx)
		biz := freshBiz(t, ctx)

		already, err := markInTx(t, ctx, d, "id-X", func(tx *sql.Tx) error {
			_, werr := tx.ExecContext(ctx, fmt.Sprintf("INSERT INTO %s (seq) VALUES (1)", kit.Quote(biz)))
			return werr
		}, rollbackTx)
		require.NoError(t, err)
		assert.False(t, already, "the first attempt is not a duplicate")

		assert.False(t, inboxHas(t, ctx, inbox, "id-X"), "a rolled-back dedup row must not persist")
		assert.Equal(t, 0, rowCount(t, ctx, kit, db, biz), "a rolled-back business row must not persist")

		already, err = markInTx(t, ctx, d, "id-X", func(tx *sql.Tx) error {
			_, werr := tx.ExecContext(ctx, fmt.Sprintf("INSERT INTO %s (seq) VALUES (1)", kit.Quote(biz)))
			return werr
		}, commitTx)
		require.NoError(t, err)
		assert.False(t, already, "a rolled-back attempt must be retryable, not deduplicated")

		assert.True(t, inboxHas(t, ctx, inbox, "id-X"), "the committed retry persists the dedup row")
		assert.Equal(t, 1, rowCount(t, ctx, kit, db, biz), "the business effect is applied exactly once by the committed retry")
	})

	t.Run("MySQLFalsePositiveGuard", func(t *testing.T) {
		if !kit.MySQLFamily {
			t.Skip("INSERT IGNORE error-demotion is MySQL-specific; Postgres ON CONFLICT RETURNING is exact")
		}
		ctx := t.Context()

		table := nextName("msgin_inbox_chk")
		_, err := db.ExecContext(ctx, fmt.Sprintf(
			"CREATE TABLE %s (msg_id VARCHAR(255) PRIMARY KEY, processed_at DATETIME(6) NOT NULL DEFAULT (UTC_TIMESTAMP(6)), CONSTRAINT chk_forbidden CHECK (msg_id <> 'FORBIDDEN'))",
			kit.Quote(table)))
		require.NoError(t, err)

		tx, err := db.BeginTx(ctx, nil)
		require.NoError(t, err)
		defer func() { _ = tx.Rollback() }()

		already, err := kit.Inbox.InsertInboxIfAbsent(ctx, tx, table, "FORBIDDEN")
		assert.False(t, already, "a demoted (non-duplicate) error must NOT be reported as a duplicate")
		require.Error(t, err, "a demoted error must surface so the message is retried, not silently dropped")
		require.ErrorIs(t, err, msginsql.ErrInboxInsertFailed)
	})

	t.Run("Purge", func(t *testing.T) {
		ctx := t.Context()

		d, inbox := freshDeduper(t, ctx)

		_, err := markInTx(t, ctx, d, "id-old", nil, commitTx)
		require.NoError(t, err)
		time.Sleep(700 * time.Millisecond)
		_, err = markInTx(t, ctx, d, "id-recent", nil, commitTx)
		require.NoError(t, err)

		n, err := d.Purge(ctx, 250*time.Millisecond)
		require.NoError(t, err)
		assert.Equal(t, int64(1), n, "exactly the one old row is purged")
		assert.False(t, inboxHas(t, ctx, inbox, "id-old"), "the old row is purged")
		assert.True(t, inboxHas(t, ctx, inbox, "id-recent"), "the recent row survives")

		n, err = d.Purge(ctx, time.Hour)
		require.NoError(t, err)
		assert.Equal(t, int64(0), n, "nothing is older than an hour")
		assert.True(t, inboxHas(t, ctx, inbox, "id-recent"), "the recent row still survives")
	})

	t.Run("ReadyAndEnsureSchema", func(t *testing.T) {
		ctx := t.Context()

		table := nextName("msgin_inbox_ready")
		d, err := msginsql.NewInboxDeduper(db, kit.Inbox, msginsql.WithInboxTable(table))
		require.NoError(t, err)

		require.ErrorIs(t, d.Ready(ctx), msginsql.ErrSchemaNotReady)

		require.NoError(t, d.EnsureSchema(ctx))
		require.NoError(t, d.EnsureSchema(ctx))
		require.NoError(t, d.Ready(ctx), "Ready must pass once the table exists")

		ddlTable := nextName("msgin_inbox_ddl")
		ddl, err := kit.InboxDDL(ddlTable)
		require.NoError(t, err)
		for _, stmt := range splitStatements(ddl) {
			_, err := db.ExecContext(ctx, stmt)
			require.NoError(t, err, "apply reference inbox DDL statement")
		}

		d2, err := msginsql.NewInboxDeduper(db, kit.Inbox, msginsql.WithInboxTable(ddlTable))
		require.NoError(t, err)
		require.NoError(t, d2.Ready(ctx), "Ready must pass after applying the reference DDL")
	})

	t.Run("ReadyRejectsMissingUniqueConstraint", func(t *testing.T) {
		ctx := t.Context()

		table := nextName("msgin_inbox_nopk")
		_, err := db.ExecContext(ctx, fmt.Sprintf("CREATE TABLE %s (msg_id VARCHAR(255))", kit.Quote(table)))
		require.NoError(t, err)

		d, err := msginsql.NewInboxDeduper(db, kit.Inbox, msginsql.WithInboxTable(table))
		require.NoError(t, err)

		err = d.Ready(ctx)
		require.ErrorIs(t, err, msginsql.ErrInboxNoUniqueConstraint,
			"a msg_id column without a unique constraint must fail Ready")
		assert.NotErrorIs(t, err, msginsql.ErrSchemaNotReady,
			"the table exists, so this is a constraint failure, not a missing-schema one")
	})

	t.Run("DefaultTable", func(t *testing.T) {
		ctx := t.Context()

		d, err := msginsql.NewInboxDeduper(db, kit.Inbox) // no WithInboxTable → default
		require.NoError(t, err)
		require.NoError(t, d.EnsureSchema(ctx))
		t.Cleanup(func() {
			_, _ = db.ExecContext(context.Background(), fmt.Sprintf("DROP TABLE IF EXISTS %s", kit.Quote("msgin_inbox")))
		})

		_, err = markInTx(t, ctx, d, "id-default", nil, commitTx)
		require.NoError(t, err)
		assert.True(t, inboxHas(t, ctx, "msgin_inbox", "id-default"), "the default table is named msgin_inbox")
	})
}
