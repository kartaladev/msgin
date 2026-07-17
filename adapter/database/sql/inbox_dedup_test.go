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
			dialect: newFakeDialect(),
			assert: func(t *testing.T, d *msginsql.InboxDeduper, err error) {
				require.ErrorIs(t, err, msgin.ErrNilAdapter)
				assert.Nil(t, d)
			},
		},
		{
			name:    "invalid inbox table name is rejected before touching the db",
			db:      func(t *testing.T) *sql.DB { return openDB(t, fakeDriverName) },
			dialect: newFakeDialect(),
			opts:    []msginsql.InboxOption{msginsql.WithInboxTable("bad table; DROP")},
			assert: func(t *testing.T, d *msginsql.InboxDeduper, err error) {
				require.ErrorIs(t, err, msginsql.ErrInvalidTableName)
				assert.Nil(t, d)
			},
		},
		{
			name:    "empty inbox table name is rejected (not silently defaulted)",
			db:      func(t *testing.T) *sql.DB { return openDB(t, fakeDriverName) },
			dialect: newFakeDialect(),
			opts:    []msginsql.InboxOption{msginsql.WithInboxTable("")},
			assert: func(t *testing.T, d *msginsql.InboxDeduper, err error) {
				require.ErrorIs(t, err, msginsql.ErrInvalidTableName)
				assert.Nil(t, d)
			},
		},
		{
			name:    "nil dialect is ErrNilDialect",
			db:      func(t *testing.T) *sql.DB { return openDB(t, fakeDriverName) },
			dialect: nil,
			assert: func(t *testing.T, d *msginsql.InboxDeduper, err error) {
				require.ErrorIs(t, err, msginsql.ErrNilDialect)
				assert.Nil(t, d)
			},
		},
		{
			name:    "a valid businessDB/dialect (fake) constructs",
			db:      func(t *testing.T) *sql.DB { return openDB(t, fakeDriverName) },
			dialect: newFakeDialect(),
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

	d, err := msginsql.NewInboxDeduper(openDB(t, fakeDriverName), newFakeDialect())
	require.NoError(t, err)

	already, err := d.MarkProcessed(t.Context(), nil, "msg-1")
	require.ErrorIs(t, err, msginsql.ErrNilTx)
	assert.False(t, already, "a refused MarkProcessed must not report a duplicate")
}

// TestInboxDeduper_PurgeRejectsNonPositiveRetention pins the retention guard (ADR
// 0010 D10, Task 10 review #2): a non-positive olderThan would purge the ENTIRE
// inbox (cutoff = now()-0 or the future) → every id double-processed, so Purge
// refuses it with ErrInvalidRetention BEFORE any DB call. No container needed —
// the guard returns before touching the pool.
func TestInboxDeduper_PurgeRejectsNonPositiveRetention(t *testing.T) {
	t.Parallel()

	d, err := msginsql.NewInboxDeduper(openDB(t, fakeDriverName), newFakeDialect())
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
