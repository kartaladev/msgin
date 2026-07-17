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

// TestNewOutboundAdapter_Construction mirrors TestNewPollingSource_Construction:
// NewOutboundAdapter shares the same construction-time validation via the
// shared adapterBase, over the explicit-dialect API (ADR 0011). No database
// connection is made — sql.Open is lazy and construction never dials.
func TestNewOutboundAdapter_Construction(t *testing.T) {
	t.Parallel()

	type testCase struct {
		name    string
		db      func(t *testing.T) *sql.DB // nil => pass a nil *sql.DB
		table   string
		dialect msginsql.LeaseDialect
		opts    []msginsql.Option
		assert  func(t *testing.T, out *msginsql.Outbound, err error)
	}

	cases := []testCase{
		{
			name:    "nil db is ErrNilAdapter",
			db:      nil,
			table:   "msgs",
			dialect: msginsql.PostgresDialect(),
			assert: func(t *testing.T, out *msginsql.Outbound, err error) {
				require.ErrorIs(t, err, msgin.ErrNilAdapter)
				assert.Nil(t, out)
			},
		},
		{
			name:    "invalid table name is rejected before touching the db",
			db:      func(t *testing.T) *sql.DB { return openDB(t, "pgx") },
			table:   "bad table; DROP",
			dialect: msginsql.PostgresDialect(),
			assert: func(t *testing.T, out *msginsql.Outbound, err error) {
				require.ErrorIs(t, err, msginsql.ErrInvalidTableName)
				assert.Nil(t, out)
			},
		},
		{
			name:    "nil dialect is ErrNilDialect",
			db:      func(t *testing.T) *sql.DB { return openDB(t, "pgx") },
			table:   "msgs",
			dialect: nil,
			assert: func(t *testing.T, out *msginsql.Outbound, err error) {
				require.ErrorIs(t, err, msginsql.ErrNilDialect)
				assert.Nil(t, out)
			},
		},
		{
			name:    "a valid db/table/dialect constructs",
			db:      func(t *testing.T) *sql.DB { return openDB(t, "pgx") },
			table:   "msgs",
			dialect: msginsql.PostgresDialect(),
			assert: func(t *testing.T, out *msginsql.Outbound, err error) {
				require.NoError(t, err)
				assert.NotNil(t, out)
			},
		},
		{
			name:    "WithLeaseTTL/WithLockedBy (lease-Source-specific) are inert but do not error",
			db:      func(t *testing.T) *sql.DB { return openDB(t, "pgx") },
			table:   "msgs",
			dialect: msginsql.PostgresDialect(),
			opts:    []msginsql.Option{msginsql.WithLeaseTTL(30 * time.Second), msginsql.WithLockedBy("owner")},
			assert: func(t *testing.T, out *msginsql.Outbound, err error) {
				require.NoError(t, err)
				assert.NotNil(t, out)
			},
		},
		{
			name:    "WithSharedTransaction(nil) is a construction-time ErrNilResolver, not a deferred panic",
			db:      func(t *testing.T) *sql.DB { return openDB(t, "pgx") },
			table:   "msgs",
			dialect: msginsql.PostgresDialect(),
			opts:    []msginsql.Option{msginsql.WithSharedTransaction(nil)},
			assert: func(t *testing.T, out *msginsql.Outbound, err error) {
				require.ErrorIs(t, err, msginsql.ErrNilResolver)
				assert.Nil(t, out)
			},
		},
		{
			name:    "WithOpportunisticSharedTransaction(nil) is also a construction-time ErrNilResolver",
			db:      func(t *testing.T) *sql.DB { return openDB(t, "pgx") },
			table:   "msgs",
			dialect: msginsql.PostgresDialect(),
			opts:    []msginsql.Option{msginsql.WithOpportunisticSharedTransaction(nil)},
			assert: func(t *testing.T, out *msginsql.Outbound, err error) {
				require.ErrorIs(t, err, msginsql.ErrNilResolver)
				assert.Nil(t, out)
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
			out, err := msginsql.NewOutboundAdapter(db, tc.table, tc.dialect, tc.opts...)
			tc.assert(t, out, err)
		})
	}
}

// TestOutbound_NotLiveValueSource pins the wire-adapter contract: Outbound must
// NOT implement msgin.LiveValueSource, so msgin.NewProducer always
// JSON-encodes the payload to []byte before calling Send (ADR 0010 D8).
func TestOutbound_NotLiveValueSource(t *testing.T) {
	t.Parallel()

	out, err := msginsql.NewOutboundAdapter(openDB(t, "pgx"), "msgs", msginsql.PostgresDialect())
	require.NoError(t, err)

	_, ok := any(out).(msgin.LiveValueSource)
	assert.False(t, ok, "Outbound must not implement LiveValueSource")
}
