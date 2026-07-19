package cron_test

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	msgin "github.com/kartaladev/msgin"
	"github.com/kartaladev/msgin/adapter/cron"
)

// fakeCronDriver is an unregistered-shape SQL driver, used by tests that need a
// *sql.DB whose driver never actually connects — construction-time SQLLocker
// tests never issue a query. Mirrors adapter/database/sql/testhelpers_test.go's
// fakeDriver.
type fakeCronDriver struct{}

func (fakeCronDriver) Open(string) (driver.Conn, error) {
	return nil, errors.New("fake driver: no connections")
}

const fakeCronDriverName = "msgin-cron-fake"

// Registered exactly once, in this single init(), for the whole cron_test
// binary (Round-2 audit NEW-NIT-1): sql.Register panics on a duplicate name,
// so no other _test.go file in this package may register a driver.
func init() { sql.Register(fakeCronDriverName, fakeCronDriver{}) }

// stubDB opens a *sql.DB on the fake driver WITHOUT connecting (sql.Open is
// lazy), closing it via t.Cleanup so the connectionOpener goroutine does not
// outlive the test (goleak).
func stubDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open(fakeCronDriverName, "unused-dsn")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// msginErrNilAdapter returns msgin.ErrNilAdapter, named so tests reference the
// sentinel through a helper rather than importing the core package by name at
// every call site.
func msginErrNilAdapter() error { return msgin.ErrNilAdapter }

// fakeLockerDialect records ClaimFire/Purge calls and returns a scripted verdict,
// so the SQLLocker's non-SQL logic (validation, delegation, retention guard) is
// tested with no database.
type fakeLockerDialect struct {
	won        bool
	claimErr   error
	claimCalls int
	lastScope  string
	lastFire   time.Time
	purged     int64
	purgeErr   error
	lastOlder  time.Duration
	ensureErr  error
}

func (f *fakeLockerDialect) ClaimFire(_ context.Context, _ cron.Querier, _ string, scope string, fire time.Time) (bool, error) {
	f.claimCalls++
	f.lastScope, f.lastFire = scope, fire
	return f.won, f.claimErr
}
func (f *fakeLockerDialect) PurgeFired(_ context.Context, _ cron.Querier, _ string, older time.Duration) (int64, error) {
	f.lastOlder = older
	return f.purged, f.purgeErr
}
func (f *fakeLockerDialect) EnsureFiredSchema(context.Context, cron.Querier, string) error {
	return f.ensureErr
}

// TestSQLLocker_Construction covers NewSQLLocker's validation order — db==nil
// (msgin.ErrNilAdapter) → dialect==nil (ErrNilDialect) → invalid table
// (ErrInvalidTableName).
func TestSQLLocker_Construction(t *testing.T) {
	type testCase struct {
		name string
		// build constructs the locker against fd (nil for the error-path cases
		// that never reach a working Locker).
		build  func(fd *fakeLockerDialect) (*cron.SQLLocker, error)
		assert func(t *testing.T, l *cron.SQLLocker, fd *fakeLockerDialect, err error)
	}
	cases := []testCase{
		{
			name: "nil db is ErrNilAdapter",
			build: func(*fakeLockerDialect) (*cron.SQLLocker, error) {
				return cron.NewSQLLocker(nil, cron.PostgresLocker())
			},
			assert: func(t *testing.T, _ *cron.SQLLocker, _ *fakeLockerDialect, err error) {
				assert.ErrorIs(t, err, msginErrNilAdapter())
			},
		},
		{
			name:  "nil dialect is ErrNilDialect",
			build: func(*fakeLockerDialect) (*cron.SQLLocker, error) { return cron.NewSQLLocker(stubDB(t), nil) },
			assert: func(t *testing.T, _ *cron.SQLLocker, _ *fakeLockerDialect, err error) {
				assert.ErrorIs(t, err, cron.ErrNilDialect)
			},
		},
		{
			name: "invalid table is ErrInvalidTableName",
			build: func(*fakeLockerDialect) (*cron.SQLLocker, error) {
				return cron.NewSQLLocker(stubDB(t), cron.PostgresLocker(), cron.WithLockerTable("bad; drop"))
			},
			assert: func(t *testing.T, _ *cron.SQLLocker, _ *fakeLockerDialect, err error) {
				assert.ErrorIs(t, err, cron.ErrInvalidTableName)
			},
		},
		{
			// Locks the construction-error priority: db==nil is checked first
			// (covered above), then dialect==nil, then table validity — so a
			// nil dialect co-occurring with an invalid table still reports
			// ErrNilDialect, never ErrInvalidTableName.
			name: "nil dialect AND invalid table: dialect check wins (ErrNilDialect)",
			build: func(*fakeLockerDialect) (*cron.SQLLocker, error) {
				return cron.NewSQLLocker(stubDB(t), nil, cron.WithLockerTable("bad; drop"))
			},
			assert: func(t *testing.T, _ *cron.SQLLocker, _ *fakeLockerDialect, err error) {
				assert.ErrorIs(t, err, cron.ErrNilDialect)
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fd := &fakeLockerDialect{won: true}
			l, err := tc.build(fd)
			tc.assert(t, l, fd, err)
		})
	}
}

func TestSQLLocker_ClaimDelegates(t *testing.T) {
	fd := &fakeLockerDialect{won: true}
	l, err := cron.NewSQLLocker(stubDB(t), fd)
	require.NoError(t, err)

	fire := time.Date(2026, 1, 1, 1, 0, 0, 0, time.UTC)
	won, err := l.Claim(t.Context(), "job-x", fire)
	require.NoError(t, err)
	assert.True(t, won)
	assert.Equal(t, "job-x", fd.lastScope)
	assert.Equal(t, fire, fd.lastFire)
	assert.Equal(t, 1, fd.claimCalls)
}

func TestSQLLocker_ClaimErrorPropagates(t *testing.T) {
	fd := &fakeLockerDialect{claimErr: cron.ErrLockerClaimFailed}
	l, err := cron.NewSQLLocker(stubDB(t), fd)
	require.NoError(t, err)
	_, err = l.Claim(t.Context(), "s", time.Now())
	assert.ErrorIs(t, err, cron.ErrLockerClaimFailed)
}

func TestSQLLocker_PurgeRejectsNonPositive(t *testing.T) {
	fd := &fakeLockerDialect{}
	l, err := cron.NewSQLLocker(stubDB(t), fd)
	require.NoError(t, err)
	for _, d := range []time.Duration{0, -time.Second} {
		_, err := l.Purge(t.Context(), d)
		assert.ErrorIs(t, err, cron.ErrInvalidRetention)
	}
	assert.Zero(t, fd.lastOlder, "Purge must reject before any dialect call")
}

// TestSQLLocker_Purge covers the delegation path Purge takes once olderThan
// passes the ErrInvalidRetention guard (a positive olderThan): the count and
// any dialect error both propagate to the caller. The guard itself is covered
// separately by TestSQLLocker_PurgeRejectsNonPositive.
func TestSQLLocker_Purge(t *testing.T) {
	type testCase struct {
		name   string
		fd     *fakeLockerDialect
		assert func(t *testing.T, fd *fakeLockerDialect, n int64, err error)
	}
	cases := []testCase{
		{
			name: "positive olderThan delegates to dialect and returns its count",
			fd:   &fakeLockerDialect{purged: 3},
			assert: func(t *testing.T, fd *fakeLockerDialect, n int64, err error) {
				require.NoError(t, err)
				assert.Equal(t, int64(3), n)
				assert.Equal(t, time.Hour, fd.lastOlder)
			},
		},
		{
			name: "dialect error propagates",
			fd:   &fakeLockerDialect{purgeErr: assert.AnError},
			assert: func(t *testing.T, fd *fakeLockerDialect, n int64, err error) {
				assert.ErrorIs(t, err, assert.AnError)
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			l, err := cron.NewSQLLocker(stubDB(t), tc.fd)
			require.NoError(t, err)
			n, err := l.Purge(t.Context(), time.Hour)
			tc.assert(t, tc.fd, n, err)
		})
	}
}

// TestSQLLocker_EnsureSchema covers EnsureSchema's sole branch: it delegates
// to the dialect's EnsureFiredSchema and passes through whatever it returns.
func TestSQLLocker_EnsureSchema(t *testing.T) {
	type testCase struct {
		name   string
		fd     *fakeLockerDialect
		assert func(t *testing.T, err error)
	}
	cases := []testCase{
		{
			name:   "delegates to dialect and returns nil on success",
			fd:     &fakeLockerDialect{},
			assert: func(t *testing.T, err error) { assert.NoError(t, err) },
		},
		{
			name:   "dialect error propagates",
			fd:     &fakeLockerDialect{ensureErr: assert.AnError},
			assert: func(t *testing.T, err error) { assert.ErrorIs(t, err, assert.AnError) },
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			l, err := cron.NewSQLLocker(stubDB(t), tc.fd)
			require.NoError(t, err)
			err = l.EnsureSchema(t.Context())
			tc.assert(t, err)
		})
	}
}

func TestLockerDDL(t *testing.T) {
	type testCase struct {
		name   string
		build  func(table string) (string, error)
		assert func(t *testing.T, ddl string, err error)
	}
	cases := []testCase{
		{
			name:  "postgres composite PK + fired table",
			build: cron.PostgresLockerDDL,
			assert: func(t *testing.T, ddl string, err error) {
				require.NoError(t, err)
				assert.Contains(t, ddl, "CREATE TABLE IF NOT EXISTS")
				assert.Contains(t, ddl, "PRIMARY KEY (scope, fire_ts)")
				assert.Contains(t, ddl, "claimed_at")
				// Two separate statements (pgx's extended protocol rejects
				// multi-statement Exec) — the combined DDL is for migration
				// tooling only, but must still be two IF NOT EXISTS statements.
				assert.Contains(t, ddl, "CREATE INDEX IF NOT EXISTS")
			},
		},
		{
			name:  "mysql inline index",
			build: cron.MySQLLockerDDL,
			assert: func(t *testing.T, ddl string, err error) {
				require.NoError(t, err)
				assert.Contains(t, ddl, "CREATE TABLE IF NOT EXISTS")
				assert.Contains(t, ddl, "PRIMARY KEY (scope, fire_ts)")
			},
		},
		{
			name:  "sqlite composite PK",
			build: cron.SQLiteLockerDDL,
			assert: func(t *testing.T, ddl string, err error) {
				require.NoError(t, err)
				assert.Contains(t, ddl, "CREATE TABLE IF NOT EXISTS")
				assert.Contains(t, ddl, "PRIMARY KEY (scope, fire_ts)")
				assert.Contains(t, ddl, "CREATE INDEX IF NOT EXISTS")
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ddl, err := tc.build("msgin_cron_fired")
			tc.assert(t, ddl, err)

			// invalid identifier is rejected before any SQL is built.
			_, err = tc.build("bad; drop")
			assert.ErrorIs(t, err, cron.ErrInvalidTableName)
		})
	}
}
