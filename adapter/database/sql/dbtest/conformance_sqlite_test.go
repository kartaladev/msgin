package dbtest_test

import (
	"testing"

	msginsql "github.com/kartaladev/msgin/adapter/database/sql"
	"github.com/kartaladev/msgin/adapter/database/sql/harness"
	"github.com/kartaladev/msgin/adapter/database/sql/sqlite"
	"github.com/stretchr/testify/require"
)

// sqliteKit builds the harness TestKit for the built-in SQLite dialect. SQLite
// uses double-quoted identifiers, "?" placeholders, INTEGER epoch-microsecond
// timestamps (the DB-clock NowExpr), and a plain TEXT headers column (identity
// HeadersTextExpr — no jsonb cast). MySQLFamily=false: SQLite's ON CONFLICT ...
// RETURNING inbox is exact (no INSERT-IGNORE demotion), so the harness skips the
// MySQL-specific assertion. There is NO OpenDB (RunLock is not run — sqlite has
// no LockDialect).
func sqliteKit() harness.TestKit {
	return harness.TestKit{
		Name:            "sqlite",
		MySQLFamily:     false,
		SingleWriter:    true, // skips RunOutbox/CommitGatesVisibility (ADR 0012 D8, audit F1)
		Lease:           sqlite.LeaseDialect(),
		Inbox:           sqlite.InboxDialect(),
		Quote:           func(ident string) string { return `"` + ident + `"` },
		Placeholder:     func(int) string { return "?" },
		NowExpr:         func() string { return `CAST(unixepoch('now','subsec')*1000000 AS INTEGER)` },
		HeadersTextExpr: func(col string) string { return col },
		DDL:             sqlite.DDL,
		InboxDDL:        sqlite.InboxDDL,
		// OpenDB intentionally nil: RunLock is not run for SQLite.
	}
}

// TestSQLiteConformance runs the msgin sql-adapter conformance harness against
// an embedded SQLite database (no Docker). RunLock is omitted — sqlite is
// lease-only (ADR 0012 D1). One DB backs every suite; each harness Run*
// provisions its own fresh tables.
func TestSQLiteConformance(t *testing.T) {
	db := RunTestSQLite(t)
	kit := sqliteKit()

	t.Run("Source", func(t *testing.T) { harness.RunSource(t, kit, db) })
	t.Run("Outbound", func(t *testing.T) { harness.RunOutbound(t, kit, db) })
	t.Run("Outbox", func(t *testing.T) { harness.RunOutbox(t, kit, db) })
	t.Run("Inbox", func(t *testing.T) { harness.RunInbox(t, kit, db) })
	t.Run("Dialect", func(t *testing.T) { harness.RunDialect(t, kit, db) })
	t.Run("QueueStore", func(t *testing.T) { harness.RunQueueStore(t, kit, db) })
}

// TestSQLiteLockStrategyUnsupported asserts the lease-only sqlite dialect does
// NOT satisfy LockDialect and that selecting the lock strategy fails at
// construction with ErrLockStrategyUnsupported (ADR 0012 D1).
func TestSQLiteLockStrategyUnsupported(t *testing.T) {
	_, isLock := sqlite.LeaseDialect().(msginsql.LockDialect)
	require.False(t, isLock, "sqlite.LeaseDialect() must NOT implement LockDialect")

	db := RunTestSQLite(t)
	_, err := msginsql.NewPollingSource(db, "msgin_msgs", sqlite.LeaseDialect(),
		msginsql.WithStrategy(msginsql.StrategyLockForUpdate))
	require.ErrorIs(t, err, msginsql.ErrLockStrategyUnsupported)
}
