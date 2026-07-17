package sql_test

// Despite the _pg_test.go suffix (matching the naming convention of the other
// dialect-parameterized suites in this package), InboxSuite runs once per engine
// via the `inboxEngines` table, so it exercises the idempotent-consumer dedup
// inbox (ADR 0010 D10: InboxDeduper) against a real PostgreSQL, MySQL, AND MariaDB
// container (the inbox path uses MySQL-family SQL that MariaDB must also accept).
// TestMain (goroutine-leak check) is declared once for the whole package in
// source_pg_test.go.

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	msginsql "github.com/kartaladev/msgin/adapter/database/sql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

// InboxSuite provisions one container per engine for the whole suite and
// exercises InboxDeduper end-to-end: idempotency across redelivery, the
// atomic-with-business-tx silent-loss guard, the MySQL SELECT-verify
// false-positive guard, Purge retention, and Ready/EnsureSchema. TestInboxSuite
// runs it once per engine (postgres, mysql) so the SAME assertions prove the
// deduper works over both dialects.
type InboxSuite struct {
	suite.Suite
	engine  engine
	db      *sql.DB
	counter int64
}

func TestInboxSuite(t *testing.T) {
	for _, e := range inboxEngines {
		t.Run(e.name, func(t *testing.T) {
			suite.Run(t, &InboxSuite{engine: e})
		})
	}
}

func (s *InboxSuite) SetupSuite() {
	s.db = s.engine.openDB(s.T())
}

// freshDeduper returns an InboxDeduper over a unique, schema-applied inbox table
// (auto-detecting the dialect from the per-engine driver) plus the table name.
func (s *InboxSuite) freshDeduper(ctx context.Context, opts ...msginsql.InboxOption) (*msginsql.InboxDeduper, string) {
	t := s.T()
	s.counter++
	name := fmt.Sprintf("msgin_inbox_%d", s.counter)
	opts = append([]msginsql.InboxOption{msginsql.WithInboxTable(name)}, opts...)
	d, err := msginsql.NewInboxDeduper(s.db, opts...)
	require.NoError(t, err)
	require.NoError(t, d.EnsureSchema(ctx), "EnsureSchema must provision the inbox table")
	return d, name
}

// freshBizTable creates a unique, minimal "business" table (a stand-in for a
// caller's own domain table) that a business write lands in alongside the dedup
// row, both under the same caller transaction.
func (s *InboxSuite) freshBizTable(ctx context.Context) string {
	t := s.T()
	s.counter++
	name := fmt.Sprintf("msgin_biz_%d", s.counter)
	_, err := s.db.ExecContext(ctx, fmt.Sprintf("CREATE TABLE %s (seq INTEGER)", s.engine.quote(name)))
	require.NoError(t, err)
	return name
}

// rowCount returns the number of rows in table.
func (s *InboxSuite) rowCount(ctx context.Context, table string) int {
	var n int
	require.NoError(s.T(), s.db.QueryRowContext(ctx, fmt.Sprintf("SELECT count(*) FROM %s", s.engine.quote(table))).Scan(&n))
	return n
}

// inboxHas reports whether the inbox table has a row for msgID.
func (s *InboxSuite) inboxHas(ctx context.Context, table, msgID string) bool {
	var n int
	q := fmt.Sprintf("SELECT count(*) FROM %s WHERE msg_id = %s", s.engine.quote(table), s.engine.ph(1))
	require.NoError(s.T(), s.db.QueryRowContext(ctx, q, msgID).Scan(&n))
	return n > 0
}

// markInTx opens a fresh transaction, calls MarkProcessed on it, runs the
// business work ONLY when the id is not already processed (the check-then-skip
// pattern MarkProcessed's contract prescribes), then finalizes (commit/rollback).
// It mirrors how a real handler records the id inside its own business
// transaction and skips the business effect on a duplicate redelivery.
func (s *InboxSuite) markInTx(ctx context.Context, d *msginsql.InboxDeduper, msgID string, work func(tx *sql.Tx) error, finalize func(tx *sql.Tx) error) (already bool, err error) {
	t := s.T()
	tx, err := s.db.BeginTx(ctx, nil)
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

func commitTx(tx *sql.Tx) error   { return tx.Commit() }
func rollbackTx(tx *sql.Tx) error { return tx.Rollback() }

// ---- tests ----------------------------------------------------------------

// TestIdempotency proves the core promise (ADR 0010 D10): the first
// MarkProcessed of an id records it (already=false); a redelivery of the same id
// sees already=true, so a cooperative handler skips the business effect and it is
// applied exactly once even though delivery is at-least-once.
func (s *InboxSuite) TestIdempotency() {
	ctx := s.T().Context()
	t := s.T()

	d, inbox := s.freshDeduper(ctx)
	biz := s.freshBizTable(ctx)

	insertBizRow := func(tx *sql.Tx) error {
		_, werr := tx.ExecContext(ctx, fmt.Sprintf("INSERT INTO %s (seq) VALUES (1)", s.engine.quote(biz)))
		return werr
	}

	// First delivery: not a duplicate, business effect applied.
	already, err := s.markInTx(ctx, d, "id-A", insertBizRow, commitTx)
	require.NoError(t, err)
	assert.False(t, already, "first MarkProcessed of an id must report not-a-duplicate")

	// Redelivery: duplicate; the handler must skip the business write.
	already, err = s.markInTx(ctx, d, "id-A", func(tx *sql.Tx) error {
		t.Fatal("business work must NOT run for an already-processed id")
		return nil
	}, rollbackTx)
	require.NoError(t, err)
	assert.True(t, already, "a redelivered id must report a duplicate")

	// A different id still processes.
	already, err = s.markInTx(ctx, d, "id-B", insertBizRow, commitTx)
	require.NoError(t, err)
	assert.False(t, already, "a distinct id is not a duplicate")

	assert.Equal(t, 2, s.rowCount(ctx, biz), "business effect applied once per distinct id (id-A once, id-B once)")
	assert.True(t, s.inboxHas(ctx, inbox, "id-A"), "id-A is recorded")
	assert.True(t, s.inboxHas(ctx, inbox, "id-B"), "id-B is recorded")
}

// TestAtomicRollbackIsRetryable is the silent-loss guard (ADR 0010 D10 HIGH 5):
// because MarkProcessed records inside the caller's *sql.Tx, a rolled-back
// attempt persists NEITHER the dedup row nor the business row, so the id is
// genuinely retried (a later MarkProcessed reports already=false) rather than
// falsely deduplicated and dropped forever. The *sql.Tx signature makes passing
// the auto-committing pool a compile error, closing the misuse this guards.
func (s *InboxSuite) TestAtomicRollbackIsRetryable() {
	ctx := s.T().Context()
	t := s.T()

	d, inbox := s.freshDeduper(ctx)
	biz := s.freshBizTable(ctx)

	// Attempt 1: record + business write, then ROLL BACK (simulating a crash
	// before commit).
	already, err := s.markInTx(ctx, d, "id-X", func(tx *sql.Tx) error {
		_, werr := tx.ExecContext(ctx, fmt.Sprintf("INSERT INTO %s (seq) VALUES (1)", s.engine.quote(biz)))
		return werr
	}, rollbackTx)
	require.NoError(t, err)
	assert.False(t, already, "the first attempt is not a duplicate")

	assert.False(t, s.inboxHas(ctx, inbox, "id-X"), "a rolled-back dedup row must not persist")
	assert.Equal(t, 0, s.rowCount(ctx, biz), "a rolled-back business row must not persist")

	// Attempt 2 (redelivery after the crash): the id must be retryable, NOT
	// falsely reported as a duplicate.
	already, err = s.markInTx(ctx, d, "id-X", func(tx *sql.Tx) error {
		_, werr := tx.ExecContext(ctx, fmt.Sprintf("INSERT INTO %s (seq) VALUES (1)", s.engine.quote(biz)))
		return werr
	}, commitTx)
	require.NoError(t, err)
	assert.False(t, already, "a rolled-back attempt must be retryable, not deduplicated")

	assert.True(t, s.inboxHas(ctx, inbox, "id-X"), "the committed retry persists the dedup row")
	assert.Equal(t, 1, s.rowCount(ctx, biz), "the business effect is applied exactly once by the committed retry")
}

// TestMySQLFalsePositiveGuard proves the MySQL SELECT-verify path (ADR 0010 D10
// MEDIUM 6): when INSERT IGNORE demotes a genuine (non-duplicate) error to
// rowsAffected==0, InsertInboxIfAbsent does NOT report already=true (which would
// drop the message) — it verifies with a SELECT, finds no matching row, and
// returns an error so the message is retried. Postgres has no equivalent demotion
// (ON CONFLICT ... RETURNING is exact), so this guard is MySQL-specific.
func (s *InboxSuite) TestMySQLFalsePositiveGuard() {
	if !s.engine.isMySQL() {
		s.T().Skip("INSERT IGNORE error-demotion is MySQL-specific; Postgres ON CONFLICT RETURNING is exact")
	}
	ctx := s.T().Context()
	t := s.T()

	// A table shaped like the inbox but with a CHECK constraint that INSERT
	// IGNORE will skip (rowsAffected==0, NO row inserted) for a forbidden id —
	// the deterministic stand-in for a demoted data error.
	s.counter++
	table := fmt.Sprintf("msgin_inbox_chk_%d", s.counter)
	_, err := s.db.ExecContext(ctx, fmt.Sprintf(
		"CREATE TABLE %s (msg_id VARCHAR(255) PRIMARY KEY, processed_at DATETIME(6) NOT NULL DEFAULT (UTC_TIMESTAMP(6)), CONSTRAINT chk_forbidden CHECK (msg_id <> 'FORBIDDEN'))",
		s.engine.quote(table)))
	require.NoError(t, err)

	tx, err := s.db.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()

	already, err := msginsql.MySQLInboxDialect().InsertInboxIfAbsent(ctx, tx, table, "FORBIDDEN")
	assert.False(t, already, "a demoted (non-duplicate) error must NOT be reported as a duplicate")
	require.Error(t, err, "a demoted error must surface so the message is retried, not silently dropped")
	require.ErrorIs(t, err, msginsql.ErrInboxInsertFailed)
}

// TestPurge proves manual retention (ADR 0010 D10): Purge deletes only rows
// whose processed_at is older than the cutoff (DB clock), and reports 0 when
// nothing qualifies. It uses tiny real durations with generous margins (a DB-clock
// behavior cannot use a fake clock).
func (s *InboxSuite) TestPurge() {
	ctx := s.T().Context()
	t := s.T()

	d, inbox := s.freshDeduper(ctx)

	// Record an "old" id, wait past the purge window, then a "recent" id. The
	// margins are wide (700ms gap, 250ms window) to stay robust against CI clock
	// jitter (a DB-clock behavior cannot use a fake clock).
	_, err := s.markInTx(ctx, d, "id-old", nil, commitTx)
	require.NoError(t, err)
	time.Sleep(700 * time.Millisecond)
	_, err = s.markInTx(ctx, d, "id-recent", nil, commitTx)
	require.NoError(t, err)

	// Purge everything older than 250ms: the old row (age >700ms) goes, the
	// recent row (age <250ms) stays.
	n, err := d.Purge(ctx, 250*time.Millisecond)
	require.NoError(t, err)
	assert.Equal(t, int64(1), n, "exactly the one old row is purged")
	assert.False(t, s.inboxHas(ctx, inbox, "id-old"), "the old row is purged")
	assert.True(t, s.inboxHas(ctx, inbox, "id-recent"), "the recent row survives")

	// A second purge with a huge window removes nothing (deletes-0 branch).
	n, err = d.Purge(ctx, time.Hour)
	require.NoError(t, err)
	assert.Equal(t, int64(0), n, "nothing is older than an hour")
	assert.True(t, s.inboxHas(ctx, inbox, "id-recent"), "the recent row still survives")
}

// TestReadyAndEnsureSchema mirrors the source's schema story (ADR 0010 D2): Ready
// fails fast with ErrSchemaNotReady on an un-provisioned inbox table, EnsureSchema
// creates it idempotently, and the reference InboxDDL applies cleanly too.
func (s *InboxSuite) TestReadyAndEnsureSchema() {
	ctx := s.T().Context()
	t := s.T()

	s.counter++
	table := fmt.Sprintf("msgin_inbox_ready_%d", s.counter)
	d, err := msginsql.NewInboxDeduper(s.db, msginsql.WithInboxTable(table))
	require.NoError(t, err)

	// Not provisioned yet → fail fast.
	require.ErrorIs(t, d.Ready(ctx), msginsql.ErrSchemaNotReady)

	// EnsureSchema is idempotent (safe to call twice).
	require.NoError(t, d.EnsureSchema(ctx))
	require.NoError(t, d.EnsureSchema(ctx))
	require.NoError(t, d.Ready(ctx), "Ready must pass once the table exists")

	// The reference DDL applies to a fresh table too. Apply each statement on
	// its own (pgx's extended protocol rejects multi-statement Exec), mirroring
	// TestReferenceDDLApplies.
	s.counter++
	ddlTable := fmt.Sprintf("msgin_inbox_ddl_%d", s.counter)
	ddl, err := msginsql.InboxDDL(s.engine.inboxDialect(), ddlTable)
	require.NoError(t, err)
	for _, stmt := range splitStatements(ddl) {
		_, err := s.db.ExecContext(ctx, stmt)
		require.NoError(t, err, "apply reference inbox DDL statement")
	}

	d2, err := msginsql.NewInboxDeduper(s.db, msginsql.WithInboxTable(ddlTable))
	require.NoError(t, err)
	require.NoError(t, d2.Ready(ctx), "Ready must pass after applying the reference DDL")
}

// TestReadyRejectsMissingUniqueConstraint proves the constraint fail-fast (ADR
// 0010 D10, Task 10 review #3, the user's robust choice): an inbox table that
// exists but whose msg_id column has NO unique/PK constraint makes MySQL/MariaDB
// INSERT IGNORE never conflict → dedup silently never works, so Ready fails fast
// with ErrInboxNoUniqueConstraint (naming the table) rather than passing a broken
// schema. The proper EnsureSchema/InboxDDL table (with the PK) passes — covered by
// TestReadyAndEnsureSchema. Runs on every inbox engine (incl. MariaDB).
func (s *InboxSuite) TestReadyRejectsMissingUniqueConstraint() {
	ctx := s.T().Context()
	t := s.T()

	// A table shaped like the inbox but with NO unique/PK on msg_id.
	s.counter++
	table := fmt.Sprintf("msgin_inbox_nopk_%d", s.counter)
	_, err := s.db.ExecContext(ctx, fmt.Sprintf("CREATE TABLE %s (msg_id VARCHAR(255))", s.engine.quote(table)))
	require.NoError(t, err)

	d, err := msginsql.NewInboxDeduper(s.db, msginsql.WithInboxTable(table))
	require.NoError(t, err)

	// The table exists (so it is not ErrSchemaNotReady) but lacks the constraint.
	err = d.Ready(ctx)
	require.ErrorIs(t, err, msginsql.ErrInboxNoUniqueConstraint,
		"a msg_id column without a unique constraint must fail Ready")
	assert.NotErrorIs(t, err, msginsql.ErrSchemaNotReady,
		"the table exists, so this is a constraint failure, not a missing-schema one")
}

// TestDefaultTable proves the sensible default (ADR 0010 D10 / CLAUDE.md): with
// no WithInboxTable, the deduper uses "msgin_inbox".
func (s *InboxSuite) TestDefaultTable() {
	ctx := s.T().Context()
	t := s.T()

	d, err := msginsql.NewInboxDeduper(s.db) // no WithInboxTable → default
	require.NoError(t, err)
	require.NoError(t, d.EnsureSchema(ctx))
	t.Cleanup(func() {
		_, _ = s.db.ExecContext(context.Background(), fmt.Sprintf("DROP TABLE IF EXISTS %s", s.engine.quote("msgin_inbox")))
	})

	_, err = s.markInTx(ctx, d, "id-default", nil, commitTx)
	require.NoError(t, err)
	assert.True(t, s.inboxHas(ctx, "msgin_inbox", "id-default"), "the default table is named msgin_inbox")
}
