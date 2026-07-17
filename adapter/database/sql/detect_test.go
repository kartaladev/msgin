package sql_test

import (
	"database/sql"
	"database/sql/driver"
	"errors"
	"testing"
	"time"

	msgin "github.com/kartaladev/msgin"
	msginsql "github.com/kartaladev/msgin/adapter/database/sql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeDriver is an unregistered-shape SQL driver whose type name contains none
// of the auto-detect markers (pgx/postgres/pq), so a *sql.DB opened on it must
// fail dialect auto-detection. It never actually connects.
type fakeDriver struct{}

func (fakeDriver) Open(string) (driver.Conn, error) {
	return nil, errors.New("fake driver: no connections")
}

const fakeDriverName = "msgin-detect-fake"

func init() { sql.Register(fakeDriverName, fakeDriver{}) }

// openDB opens a *sql.DB on driverName WITHOUT connecting (sql.Open is lazy),
// closing it via t.Cleanup so the connectionOpener goroutine does not outlive
// the test (goleak).
func openDB(t *testing.T, driverName string) *sql.DB {
	t.Helper()
	db, err := sql.Open(driverName, "unused-dsn")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// TestNewPollingSource_ConstructionAndDetection exercises the constructor's
// validation and the dialect-resolution precedence (ADR 0010 D3): WithDialect
// wins over auto-detect; a pgx driver auto-detects PostgresDialect; an
// unrecognized driver with no WithDialect is ErrDialectUndetected naming the
// type. No database connection is made — sql.Open is lazy and resolveDialect
// only reflects on db.Driver().
func TestNewPollingSource_ConstructionAndDetection(t *testing.T) {
	t.Parallel()

	type testCase struct {
		name   string
		db     func(t *testing.T) *sql.DB // nil => pass a nil *sql.DB
		table  string
		opts   []msginsql.Option
		assert func(t *testing.T, src *msginsql.Source, err error)
	}

	cases := []testCase{
		{
			name:  "nil db is ErrNilAdapter",
			db:    nil,
			table: "msgs",
			assert: func(t *testing.T, src *msginsql.Source, err error) {
				require.ErrorIs(t, err, msgin.ErrNilAdapter)
				assert.Nil(t, src)
			},
		},
		{
			name:  "invalid table name is rejected before touching the db",
			db:    func(t *testing.T) *sql.DB { return openDB(t, "pgx") },
			table: "bad table; DROP",
			assert: func(t *testing.T, src *msginsql.Source, err error) {
				require.ErrorIs(t, err, msginsql.ErrInvalidTableName)
				assert.Nil(t, src)
			},
		},
		{
			name:  "pgx driver auto-detects a dialect",
			db:    func(t *testing.T) *sql.DB { return openDB(t, "pgx") },
			table: "msgs",
			assert: func(t *testing.T, src *msginsql.Source, err error) {
				require.NoError(t, err)
				assert.NotNil(t, src)
			},
		},
		{
			name:  "unrecognized driver with no WithDialect is ErrDialectUndetected naming the type",
			db:    func(t *testing.T) *sql.DB { return openDB(t, fakeDriverName) },
			table: "msgs",
			assert: func(t *testing.T, src *msginsql.Source, err error) {
				require.ErrorIs(t, err, msginsql.ErrDialectUndetected)
				assert.Nil(t, src)
				assert.Contains(t, err.Error(), "fakeDriver", "error names the driver type")
			},
		},
		{
			name:  "WithDialect wins over an unrecognized driver (no auto-detect needed)",
			db:    func(t *testing.T) *sql.DB { return openDB(t, fakeDriverName) },
			table: "msgs",
			opts:  []msginsql.Option{msginsql.WithDialect(msginsql.PostgresDialect())},
			assert: func(t *testing.T, src *msginsql.Source, err error) {
				require.NoError(t, err, "explicit WithDialect must bypass auto-detect")
				assert.NotNil(t, src)
			},
		},
		{
			name:  "WithLeaseTTL(0) is ErrInvalidLeaseTTL",
			db:    func(t *testing.T) *sql.DB { return openDB(t, "pgx") },
			table: "msgs",
			opts:  []msginsql.Option{msginsql.WithLeaseTTL(0)},
			assert: func(t *testing.T, src *msginsql.Source, err error) {
				require.ErrorIs(t, err, msginsql.ErrInvalidLeaseTTL)
				assert.Nil(t, src)
			},
		},
		{
			name:  "WithLeaseTTL(negative) is ErrInvalidLeaseTTL",
			db:    func(t *testing.T) *sql.DB { return openDB(t, "pgx") },
			table: "msgs",
			opts:  []msginsql.Option{msginsql.WithLeaseTTL(-time.Second)},
			assert: func(t *testing.T, src *msginsql.Source, err error) {
				require.ErrorIs(t, err, msginsql.ErrInvalidLeaseTTL)
				assert.Nil(t, src)
			},
		},
		{
			name:  "WithLeaseTTL(positive) is accepted",
			db:    func(t *testing.T) *sql.DB { return openDB(t, "pgx") },
			table: "msgs",
			opts:  []msginsql.Option{msginsql.WithLeaseTTL(30 * time.Second)},
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
			src, err := msginsql.NewPollingSource(db, tc.table, tc.opts...)
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

	src, err := msginsql.NewPollingSource(openDB(t, "pgx"), "msgs")
	require.NoError(t, err)

	assert.True(t, src.NativeRedelivery(), "sql source redelivers natively (nacked/expired rows persist)")
	assert.False(t, src.NativeDeadLetter(), "sql source has no native dead-letter; the runtime policy governs it")
}
