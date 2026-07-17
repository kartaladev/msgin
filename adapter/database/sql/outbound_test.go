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

// TestNewOutboundAdapter_ConstructionAndDetection mirrors
// TestNewPollingSource_ConstructionAndDetection: NewOutboundAdapter shares the
// same construction-time validation and dialect-resolution precedence (ADR
// 0010 D3) via the shared adapterBase. No database connection is made — sql.Open
// is lazy and resolveDialect only reflects on db.Driver().
func TestNewOutboundAdapter_ConstructionAndDetection(t *testing.T) {
	t.Parallel()

	type testCase struct {
		name   string
		db     func(t *testing.T) *sql.DB // nil => pass a nil *sql.DB
		table  string
		opts   []msginsql.Option
		assert func(t *testing.T, out *msginsql.Outbound, err error)
	}

	cases := []testCase{
		{
			name:  "nil db is ErrNilAdapter",
			db:    nil,
			table: "msgs",
			assert: func(t *testing.T, out *msginsql.Outbound, err error) {
				require.ErrorIs(t, err, msgin.ErrNilAdapter)
				assert.Nil(t, out)
			},
		},
		{
			name:  "invalid table name is rejected before touching the db",
			db:    func(t *testing.T) *sql.DB { return openDB(t, "pgx") },
			table: "bad table; DROP",
			assert: func(t *testing.T, out *msginsql.Outbound, err error) {
				require.ErrorIs(t, err, msginsql.ErrInvalidTableName)
				assert.Nil(t, out)
			},
		},
		{
			name:  "pgx driver auto-detects a dialect",
			db:    func(t *testing.T) *sql.DB { return openDB(t, "pgx") },
			table: "msgs",
			assert: func(t *testing.T, out *msginsql.Outbound, err error) {
				require.NoError(t, err)
				assert.NotNil(t, out)
			},
		},
		{
			name:  "unrecognized driver with no WithDialect is ErrDialectUndetected naming the type",
			db:    func(t *testing.T) *sql.DB { return openDB(t, fakeDriverName) },
			table: "msgs",
			assert: func(t *testing.T, out *msginsql.Outbound, err error) {
				require.ErrorIs(t, err, msginsql.ErrDialectUndetected)
				assert.Nil(t, out)
				assert.Contains(t, err.Error(), "fakeDriver", "error names the driver type")
			},
		},
		{
			name:  "WithDialect wins over an unrecognized driver (no auto-detect needed)",
			db:    func(t *testing.T) *sql.DB { return openDB(t, fakeDriverName) },
			table: "msgs",
			opts:  []msginsql.Option{msginsql.WithDialect(msginsql.PostgresDialect())},
			assert: func(t *testing.T, out *msginsql.Outbound, err error) {
				require.NoError(t, err, "explicit WithDialect must bypass auto-detect")
				assert.NotNil(t, out)
			},
		},
		{
			name:  "WithLeaseTTL/WithLockedBy (lease-Source-specific) are inert but do not error",
			db:    func(t *testing.T) *sql.DB { return openDB(t, "pgx") },
			table: "msgs",
			opts:  []msginsql.Option{msginsql.WithLeaseTTL(30 * time.Second), msginsql.WithLockedBy("owner")},
			assert: func(t *testing.T, out *msginsql.Outbound, err error) {
				require.NoError(t, err)
				assert.NotNil(t, out)
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
			out, err := msginsql.NewOutboundAdapter(db, tc.table, tc.opts...)
			tc.assert(t, out, err)
		})
	}
}

// TestOutbound_NotLiveValueSource pins the wire-adapter contract: Outbound must
// NOT implement msgin.LiveValueSource, so msgin.NewProducer always
// JSON-encodes the payload to []byte before calling Send (ADR 0010 D8).
func TestOutbound_NotLiveValueSource(t *testing.T) {
	t.Parallel()

	out, err := msginsql.NewOutboundAdapter(openDB(t, "pgx"), "msgs")
	require.NoError(t, err)

	_, ok := any(out).(msgin.LiveValueSource)
	assert.False(t, ok, "Outbound must not implement LiveValueSource")
}
