package sql_test

import (
	"database/sql"
	"testing"
	"time"

	msgin "github.com/kartaladev/msgin"
	msginsql "github.com/kartaladev/msgin/adapter/database/sql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewPollingSource_Construction exercises the constructor's validation over
// the explicit-dialect API (ADR 0011 — dialect is now a required, positional
// constructor argument; there is no driver auto-detect fallback): nil db is
// ErrNilAdapter, an invalid table is ErrInvalidTableName, a nil dialect is
// ErrNilDialect, and a valid call constructs. No database connection is made —
// sql.Open is lazy and construction never dials.
func TestNewPollingSource_Construction(t *testing.T) {
	t.Parallel()

	type testCase struct {
		name    string
		db      func(t *testing.T) *sql.DB // nil => pass a nil *sql.DB
		table   string
		dialect msginsql.LeaseDialect
		opts    []msginsql.Option
		assert  func(t *testing.T, src *msginsql.Source, err error)
	}

	cases := []testCase{
		{
			name:    "nil db is ErrNilAdapter",
			db:      nil,
			table:   "msgs",
			dialect: msginsql.PostgresDialect(),
			assert: func(t *testing.T, src *msginsql.Source, err error) {
				require.ErrorIs(t, err, msgin.ErrNilAdapter)
				assert.Nil(t, src)
			},
		},
		{
			name:    "invalid table name is rejected before touching the db",
			db:      func(t *testing.T) *sql.DB { return openDB(t, "pgx") },
			table:   "bad table; DROP",
			dialect: msginsql.PostgresDialect(),
			assert: func(t *testing.T, src *msginsql.Source, err error) {
				require.ErrorIs(t, err, msginsql.ErrInvalidTableName)
				assert.Nil(t, src)
			},
		},
		{
			name:    "nil dialect is ErrNilDialect",
			db:      func(t *testing.T) *sql.DB { return openDB(t, "pgx") },
			table:   "msgs",
			dialect: nil,
			assert: func(t *testing.T, src *msginsql.Source, err error) {
				require.ErrorIs(t, err, msginsql.ErrNilDialect)
				assert.Nil(t, src)
			},
		},
		{
			name:    "a valid db/table/dialect constructs",
			db:      func(t *testing.T) *sql.DB { return openDB(t, "pgx") },
			table:   "msgs",
			dialect: msginsql.PostgresDialect(),
			assert: func(t *testing.T, src *msginsql.Source, err error) {
				require.NoError(t, err)
				assert.NotNil(t, src)
			},
		},
		{
			name:    "WithLeaseTTL(0) is ErrInvalidLeaseTTL",
			db:      func(t *testing.T) *sql.DB { return openDB(t, "pgx") },
			table:   "msgs",
			dialect: msginsql.PostgresDialect(),
			opts:    []msginsql.Option{msginsql.WithLeaseTTL(0)},
			assert: func(t *testing.T, src *msginsql.Source, err error) {
				require.ErrorIs(t, err, msginsql.ErrInvalidLeaseTTL)
				assert.Nil(t, src)
			},
		},
		{
			name:    "WithLeaseTTL(negative) is ErrInvalidLeaseTTL",
			db:      func(t *testing.T) *sql.DB { return openDB(t, "pgx") },
			table:   "msgs",
			dialect: msginsql.PostgresDialect(),
			opts:    []msginsql.Option{msginsql.WithLeaseTTL(-time.Second)},
			assert: func(t *testing.T, src *msginsql.Source, err error) {
				require.ErrorIs(t, err, msginsql.ErrInvalidLeaseTTL)
				assert.Nil(t, src)
			},
		},
		{
			name:    "WithLeaseTTL(positive) is accepted",
			db:      func(t *testing.T) *sql.DB { return openDB(t, "pgx") },
			table:   "msgs",
			dialect: msginsql.PostgresDialect(),
			opts:    []msginsql.Option{msginsql.WithLeaseTTL(30 * time.Second)},
			assert: func(t *testing.T, src *msginsql.Source, err error) {
				require.NoError(t, err)
				assert.NotNil(t, src)
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
			src, err := msginsql.NewPollingSource(db, tc.table, tc.dialect, tc.opts...)
			tc.assert(t, src, err)
		})
	}
}

// TestSource_NativeReliabilityContract pins the delivery-guarantee contract the
// runtime resolves from the Source: native redelivery (the row persists and is
// re-leased), no native dead-letter (the runtime's RetryPolicy governs it). No
// connection is made.
func TestSource_NativeReliabilityContract(t *testing.T) {
	t.Parallel()

	src, err := msginsql.NewPollingSource(openDB(t, "pgx"), "msgs", msginsql.PostgresDialect())
	require.NoError(t, err)

	assert.True(t, src.NativeRedelivery(), "sql source redelivers natively (nacked/expired rows persist)")
	assert.False(t, src.NativeDeadLetter(), "sql source has no native dead-letter; the runtime policy governs it")
}
