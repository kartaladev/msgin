package sql_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	msgin "github.com/kartaladev/msgin"
	msginsql "github.com/kartaladev/msgin/adapter/database/sql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubInboxDialect is a minimal InboxDialect that is NOT one of the built-ins,
// used to prove InboxDDL rejects a dialect it has no reference DDL for. It never
// touches a database.
type stubInboxDialect struct{}

func (stubInboxDialect) InsertInboxIfAbsent(context.Context, msginsql.Querier, string, string) (bool, error) {
	return false, nil
}

func (stubInboxDialect) PurgeInbox(context.Context, msginsql.Querier, string, time.Duration) (int64, error) {
	return 0, nil
}

func (stubInboxDialect) EnsureInboxSchema(context.Context, msginsql.Querier, string) error {
	return nil
}

func (stubInboxDialect) SchemaExists(context.Context, msginsql.Querier, string) (bool, error) {
	return false, nil
}

func (stubInboxDialect) MsgIDUniqueIndexExists(context.Context, msginsql.Querier, string) (bool, error) {
	return false, nil
}

// TestNewInboxDeduper_Construction exercises the deduper constructor's
// validation over the explicit-dialect API (ADR 0011 — dialect is now a
// required, positional constructor argument; there is no driver auto-detect
// fallback): a nil businessDB is ErrNilAdapter, an invalid/empty table is
// ErrInvalidTableName, a nil dialect is ErrNilDialect, and a caller's own
// InboxDialect is accepted like the built-ins. No database connection is
// made — sql.Open is lazy and construction never dials.
func TestNewInboxDeduper_Construction(t *testing.T) {
	t.Parallel()

	type testCase struct {
		name    string
		db      func(t *testing.T) *sql.DB // nil => pass a nil *sql.DB
		dialect msginsql.InboxDialect
		opts    []msginsql.InboxOption
		assert  func(t *testing.T, d *msginsql.InboxDeduper, err error)
	}

	cases := []testCase{
		{
			name:    "nil businessDB is ErrNilAdapter",
			db:      nil,
			dialect: msginsql.PostgresInboxDialect(),
			assert: func(t *testing.T, d *msginsql.InboxDeduper, err error) {
				require.ErrorIs(t, err, msgin.ErrNilAdapter)
				assert.Nil(t, d)
			},
		},
		{
			name:    "invalid inbox table name is rejected before touching the db",
			db:      func(t *testing.T) *sql.DB { return openDB(t, "pgx") },
			dialect: msginsql.PostgresInboxDialect(),
			opts:    []msginsql.InboxOption{msginsql.WithInboxTable("bad table; DROP")},
			assert: func(t *testing.T, d *msginsql.InboxDeduper, err error) {
				require.ErrorIs(t, err, msginsql.ErrInvalidTableName)
				assert.Nil(t, d)
			},
		},
		{
			name:    "empty inbox table name is rejected (not silently defaulted)",
			db:      func(t *testing.T) *sql.DB { return openDB(t, "pgx") },
			dialect: msginsql.PostgresInboxDialect(),
			opts:    []msginsql.InboxOption{msginsql.WithInboxTable("")},
			assert: func(t *testing.T, d *msginsql.InboxDeduper, err error) {
				require.ErrorIs(t, err, msginsql.ErrInvalidTableName)
				assert.Nil(t, d)
			},
		},
		{
			name:    "nil dialect is ErrNilDialect",
			db:      func(t *testing.T) *sql.DB { return openDB(t, "pgx") },
			dialect: nil,
			assert: func(t *testing.T, d *msginsql.InboxDeduper, err error) {
				require.ErrorIs(t, err, msginsql.ErrNilDialect)
				assert.Nil(t, d)
			},
		},
		{
			name:    "a valid businessDB/dialect (postgres) constructs",
			db:      func(t *testing.T) *sql.DB { return openDB(t, "pgx") },
			dialect: msginsql.PostgresInboxDialect(),
			assert: func(t *testing.T, d *msginsql.InboxDeduper, err error) {
				require.NoError(t, err)
				assert.NotNil(t, d)
			},
		},
		{
			name:    "a valid businessDB/dialect (mysql) constructs",
			db:      func(t *testing.T) *sql.DB { return openDB(t, "mysql") },
			dialect: msginsql.MySQLInboxDialect(),
			assert: func(t *testing.T, d *msginsql.InboxDeduper, err error) {
				require.NoError(t, err)
				assert.NotNil(t, d)
			},
		},
		{
			name:    "a custom InboxDialect is accepted",
			db:      func(t *testing.T) *sql.DB { return openDB(t, fakeDriverName) },
			dialect: stubInboxDialect{},
			assert: func(t *testing.T, d *msginsql.InboxDeduper, err error) {
				require.NoError(t, err, "a caller's own InboxDialect must be accepted")
				assert.NotNil(t, d)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var db *sql.DB
			if tc.db != nil {
				db = tc.db(t)
			}
			d, err := msginsql.NewInboxDeduper(db, tc.dialect, tc.opts...)
			tc.assert(t, d, err)
		})
	}
}

// TestInboxDeduper_MarkProcessedNilTx pins the no-panic-on-caller-input rule
// (CLAUDE.md): a nil *sql.Tx is refused with ErrNilTx before any database call,
// never dereferenced into a panic. The *sql.Tx type already makes passing the
// pool a compile error (ADR 0010 D10 HIGH 5); this guards the remaining nil case.
func TestInboxDeduper_MarkProcessedNilTx(t *testing.T) {
	t.Parallel()

	d, err := msginsql.NewInboxDeduper(openDB(t, "pgx"), msginsql.PostgresInboxDialect())
	require.NoError(t, err)

	already, err := d.MarkProcessed(t.Context(), nil, "msg-1")
	require.ErrorIs(t, err, msginsql.ErrNilTx)
	assert.False(t, already, "a refused MarkProcessed must not report a duplicate")
}

// TestInboxDeduper_ReadyPassesThroughProbeError pins the error-passthrough branch
// of InboxDeduper.Ready (ADR 0010 D2): when SchemaExists itself ERRORS (as
// opposed to reporting the table simply absent), Ready returns that raw error
// unchanged — it does NOT mask a real infrastructure failure as
// ErrSchemaNotReady. Closing the pool before the probe makes SchemaExists's query
// fail deterministically, no container needed.
func TestInboxDeduper_ReadyPassesThroughProbeError(t *testing.T) {
	t.Parallel()

	db := openDB(t, "pgx")
	require.NoError(t, db.Close()) // a closed pool makes SchemaExists's query error

	d, err := msginsql.NewInboxDeduper(db, msginsql.PostgresInboxDialect())
	require.NoError(t, err)

	err = d.Ready(t.Context())
	require.Error(t, err, "a probe error must surface, not be swallowed")
	assert.NotErrorIs(t, err, msginsql.ErrSchemaNotReady,
		"a genuine probe error must NOT be reported as a not-ready schema")
}

// TestInboxDeduper_PurgeRejectsNonPositiveRetention pins the retention guard (ADR
// 0010 D10, Task 10 review #2): a non-positive olderThan would purge the ENTIRE
// inbox (cutoff = now()-0 or the future) → every id double-processed, so Purge
// refuses it with ErrInvalidRetention BEFORE any DB call. No container needed —
// the guard returns before touching the pool.
func TestInboxDeduper_PurgeRejectsNonPositiveRetention(t *testing.T) {
	t.Parallel()

	d, err := msginsql.NewInboxDeduper(openDB(t, "pgx"), msginsql.PostgresInboxDialect())
	require.NoError(t, err)

	type testCase struct {
		name      string
		olderThan time.Duration
	}
	cases := []testCase{
		{"zero", 0},
		{"negative", -time.Second},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			n, err := d.Purge(t.Context(), tc.olderThan)
			require.ErrorIs(t, err, msginsql.ErrInvalidRetention)
			assert.Zero(t, n, "a refused Purge must delete nothing")
		})
	}
}

// TestInboxDDL exercises the reference-DDL builder's validation and
// dialect-dispatch (ADR 0010 D10, applying the Task-4 identifier-injection
// discipline): the table is validated first (the sole entry point), then the
// built-ins produce their exact CREATE TABLE; an unrecognized dialect yields a
// clear error rather than empty or wrong SQL.
func TestInboxDDL(t *testing.T) {
	t.Parallel()

	type testCase struct {
		name    string
		dialect msginsql.InboxDialect
		table   string
		assert  func(t *testing.T, ddl string, err error)
	}

	cases := []testCase{
		{
			name:    "postgres built-in produces a validated CREATE TABLE",
			dialect: msginsql.PostgresInboxDialect(),
			table:   "msgin_inbox",
			assert: func(t *testing.T, ddl string, err error) {
				require.NoError(t, err)
				assert.Contains(t, ddl, "CREATE TABLE")
				assert.Contains(t, ddl, "msg_id")
				assert.Contains(t, ddl, "processed_at")
				assert.Contains(t, ddl, `"msgin_inbox"`, "the table identifier is dialect-quoted")
			},
		},
		{
			name:    "mysql built-in produces a validated CREATE TABLE",
			dialect: msginsql.MySQLInboxDialect(),
			table:   "msgin_inbox",
			assert: func(t *testing.T, ddl string, err error) {
				require.NoError(t, err)
				assert.Contains(t, ddl, "CREATE TABLE")
				assert.Contains(t, ddl, "msg_id")
				assert.Contains(t, ddl, "processed_at")
				assert.Contains(t, ddl, "`msgin_inbox`", "the table identifier is back-quoted for MySQL")
			},
		},
		{
			name:    "invalid identifier is rejected before building any SQL",
			dialect: msginsql.PostgresInboxDialect(),
			table:   "bad table; DROP",
			assert: func(t *testing.T, ddl string, err error) {
				require.ErrorIs(t, err, msginsql.ErrInvalidTableName)
				assert.Empty(t, ddl)
			},
		},
		{
			name:    "an unrecognized dialect has no reference DDL",
			dialect: stubInboxDialect{},
			table:   "msgin_inbox",
			assert: func(t *testing.T, ddl string, err error) {
				require.Error(t, err)
				assert.Empty(t, ddl)
				assert.Contains(t, err.Error(), "stubInboxDialect", "the error names the offending dialect type")
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ddl, err := msginsql.InboxDDL(tc.dialect, tc.table)
			tc.assert(t, ddl, err)
		})
	}
}
