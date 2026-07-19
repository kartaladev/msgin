package crontest_test

// crontest is the leaf runner module for adapter/cron (Plan 011 Task 5,
// mirroring adapter/database/sql/dbtest, ADR 0011/0017): nobody imports it, so
// it is the one place the heavy test dependencies live — the SQL drivers and
// testcontainers-go. It EXECUTES a shared conformance body
// (runLockerConformance / runElectorConformance, in locker_conformance_test.go
// / elector_conformance_test.go) against real PostgreSQL, MySQL, MariaDB, and
// SQLite for both SQL-backed coordinators (adapter/cron.SQLLocker /
// SQLElector), proving the per-engine SQL in adapter/cron/dialect.go —
// including the exactly-one-winner / single-leader concurrency properties a
// driver-free fake cannot prove — under real concurrency. This file provisions
// the four engines (container helpers copied from
// adapter/database/sql/dbtest/testutils_test.go, Plan 006 Task 4/5) and pairs
// each with its matching LockerDialect/ElectorDialect built-in.

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/kartaladev/msgin/adapter/cron"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	tcmariadb "github.com/testcontainers/testcontainers-go/modules/mariadb"
	tcmysql "github.com/testcontainers/testcontainers-go/modules/mysql"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
	"go.uber.org/goleak"

	// Registers the "pgx" database/sql driver. pgx (jackc/pgx/v5) is chosen
	// over lib/pq because it is the modern, actively-maintained PostgreSQL
	// driver — the same driver a production caller would inject alongside the
	// explicit cron.PostgresLocker()/PostgresElector().
	_ "github.com/jackc/pgx/v5/stdlib"
	// Registers the "mysql" database/sql driver (go-sql-driver/mysql) — the
	// same driver a production caller would inject alongside the explicit
	// cron.MySQLLocker()/MySQLElector(). MariaDB is wire-compatible and uses
	// this same driver.
	_ "github.com/go-sql-driver/mysql"
	_ "modernc.org/sqlite" // registers the "sqlite" database/sql driver (pure-Go, cgo-free)
)

// TestMain runs the module's goroutine-leak check. The testcontainers,
// Docker-client, and HTTP-pool background goroutines settle within goleak's
// retry window locally, but can linger past it on a slower/busier CI host,
// which would flake the whole module. The ignore list below is a DEFENSIVE
// guard for exactly those known container-plumbing top-of-stack functions, so
// a real leaked msgin poll/coordinator goroutine is still caught while
// container plumbing is not (use-testcontainers skill, mirrors dbtest's
// TestMain verbatim).
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m,
		// testcontainers Ryuk reaper connection keep-alive.
		goleak.IgnoreAnyFunction("github.com/testcontainers/testcontainers-go.(*Reaper).connect.func1"),
		// Docker/HTTP client idle connection pool (kept warm across calls).
		goleak.IgnoreAnyFunction("net/http.(*persistConn).readLoop"),
		goleak.IgnoreAnyFunction("net/http.(*persistConn).writeLoop"),
		// Underlying network poller blocking read for the above conns.
		goleak.IgnoreTopFunction("internal/poll.runtime_pollWait"),
	)
}

// testConfig holds RunTestDatabase/RunTestMySQL/RunTestMariaDB/RunTestSQLite
// options.
type testConfig struct {
	image              string
	sqliteSharedMemory bool
}

// TestOption customizes a RunTest* helper.
type TestOption func(*testConfig)

// WithImage overrides the pinned container image.
func WithImage(image string) TestOption {
	return func(c *testConfig) { c.image = image }
}

// WithSharedMemory selects an in-memory shared-cache SQLite DB instead of the
// default WAL temp-file, for RunTestSQLite.
func WithSharedMemory() TestOption {
	return func(c *testConfig) { c.sqliteSharedMemory = true }
}

// RunTestDatabase starts a throwaway PostgreSQL container and returns an open
// *sql.DB (pgx driver) pointed at it. The container and connection are torn
// down via t.Cleanup. It is the single Postgres provisioning helper for this
// module's conformance runs (per the use-testcontainers skill); the
// coordinator's EnsureSchema applies its own schema.
func RunTestDatabase(t *testing.T, opts ...TestOption) *sql.DB {
	t.Helper()

	cfg := &testConfig{image: "postgres:16.10-alpine"}
	for _, o := range opts {
		o(cfg)
	}

	ctx := t.Context()

	container, err := tcpostgres.Run(ctx, cfg.image,
		tcpostgres.WithDatabase("msgin"),
		tcpostgres.WithUsername("msgin"),
		tcpostgres.WithPassword("msgin"),
		testcontainers.WithWaitStrategy(
			// The readiness log appears once during init and once after the
			// internal restart; only the second occurrence means truly ready.
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(60*time.Second),
		),
	)
	require.NoError(t, err, "start postgres test container")

	t.Cleanup(func() {
		// Fresh context: t.Context() is already canceled during cleanup.
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := container.Terminate(cleanupCtx); err != nil {
			t.Errorf("terminate postgres test container: %v", err)
		}
	})

	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err, "postgres connection string")

	db, err := sql.Open("pgx", dsn)
	require.NoError(t, err, "open postgres database")
	t.Cleanup(func() { _ = db.Close() })

	pingCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	require.NoError(t, db.PingContext(pingCtx), "ping postgres database")

	return db
}

// RunTestMySQL starts a throwaway MySQL container and returns an open *sql.DB
// (go-sql-driver/mysql) pointed at it. The container and connection are torn
// down via t.Cleanup. parseTime=true is REQUIRED so DATETIME(6) columns scan
// back into time.Time — the Locker stores fire_ts as a time.Time and a test
// probe needs it back. It is the single MySQL provisioning helper for this
// module's conformance runs (per the use-testcontainers skill).
func RunTestMySQL(t *testing.T, opts ...TestOption) *sql.DB {
	t.Helper()

	// MySQL 8.0.40: SELECT ... FOR UPDATE SKIP LOCKED / LOCK IN SHARE MODE
	// (8.0.1+) and expression column DEFAULT (UTC_TIMESTAMP(6)) (8.0.13+) and
	// row CHECK constraints (8.0.16+, needed by the demoted-error tests) are
	// all available here.
	cfg := &testConfig{image: "mysql:8.0.40"}
	for _, o := range opts {
		o(cfg)
	}

	ctx := t.Context()

	container, err := tcmysql.Run(ctx, cfg.image,
		tcmysql.WithDatabase("msgin"),
		tcmysql.WithUsername("msgin"),
		tcmysql.WithPassword("msgin"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("port: 3306  MySQL Community Server").
				WithStartupTimeout(120*time.Second),
		),
	)
	require.NoError(t, err, "start mysql test container")

	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := container.Terminate(cleanupCtx); err != nil {
			t.Errorf("terminate mysql test container: %v", err)
		}
	})

	dsn, err := container.ConnectionString(ctx, "parseTime=true")
	require.NoError(t, err, "mysql connection string")

	db, err := sql.Open("mysql", dsn)
	require.NoError(t, err, "open mysql database")
	t.Cleanup(func() { _ = db.Close() })

	// The wait-for-log strategy can fire a moment before the server accepts
	// connections, so ping with a short retry loop rather than a single shot.
	pingCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	require.Eventually(t, func() bool {
		return db.PingContext(pingCtx) == nil
	}, 60*time.Second, 500*time.Millisecond, "ping mysql database")

	return db
}

// RunTestMariaDB starts a throwaway MariaDB container and returns an open
// *sql.DB (go-sql-driver/mysql — MariaDB is wire-compatible and uses the same
// driver) pointed at it. The container and connection are torn down via
// t.Cleanup. parseTime=true is set so DATETIME(6) columns scan back into
// time.Time. It is the MariaDB peer of RunTestMySQL.
func RunTestMariaDB(t *testing.T, opts ...TestOption) *sql.DB {
	t.Helper()

	// MariaDB 11.4 (LTS): SELECT ... FOR UPDATE SKIP LOCKED, LOCK IN SHARE
	// MODE, expression column DEFAULT (UTC_TIMESTAMP(6)), and row CHECK
	// constraints (10.2.1+, needed by the demoted-error tests) are all
	// supported.
	cfg := &testConfig{image: "mariadb:11.4"}
	for _, o := range opts {
		o(cfg)
	}

	ctx := t.Context()

	container, err := tcmariadb.Run(ctx, cfg.image,
		tcmariadb.WithDatabase("msgin"),
		tcmariadb.WithUsername("msgin"),
		tcmariadb.WithPassword("msgin"),
	)
	require.NoError(t, err, "start mariadb test container")

	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := container.Terminate(cleanupCtx); err != nil {
			t.Errorf("terminate mariadb test container: %v", err)
		}
	})

	dsn, err := container.ConnectionString(ctx, "parseTime=true")
	require.NoError(t, err, "mariadb connection string")

	db, err := sql.Open("mysql", dsn)
	require.NoError(t, err, "open mariadb database")
	t.Cleanup(func() { _ = db.Close() })

	// The wait-for-log strategy can fire a moment before the server accepts
	// connections, so ping with a short retry loop rather than a single shot.
	pingCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	require.Eventually(t, func() bool {
		return db.PingContext(pingCtx) == nil
	}, 60*time.Second, 500*time.Millisecond, "ping mariadb database")

	return db
}

// sqliteDSN mirrors adapter/database/sql/sqlite.DSN's default connection
// string (WAL journal mode + 5s busy_timeout) for a file path, or the shared
// in-memory form when shared is true. It is replicated inline — rather than
// importing sqlite.DSN — because crontest carries no dependency on the sqlite
// leaf module (only the root module's adapter/cron dialects, per the plan's
// no-leaf-dialect-deps constraint). Without both pragmas, SQLite serializes
// writers by rejecting concurrent ones outright: the N=8 concurrent-writer
// conformance cases (Locker exactly-one-winner, Elector single-leader) would
// flake with SQLITE_BUSY instead of blocking and retrying.
func sqliteDSN(path string, shared bool) string {
	if shared {
		return "file::memory:?cache=shared"
	}
	return fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)", path)
}

// RunTestSQLite opens an embedded SQLite *sql.DB (modernc.org/sqlite, pure-Go
// — no Docker) for the conformance run, via sqliteDSN. The default is a WAL
// temp-file (removed with the test's TempDir by the framework) with a
// busy_timeout so the harness's concurrency cases serialize rather than
// hitting SQLITE_BUSY; WithSharedMemory selects file::memory:?cache=shared
// instead. The connection is closed via t.Cleanup.
func RunTestSQLite(t *testing.T, opts ...TestOption) *sql.DB {
	t.Helper()

	cfg := &testConfig{}
	for _, o := range opts {
		o(cfg)
	}

	dsn := sqliteDSN(filepath.Join(t.TempDir(), "msgin.db"), false)
	if cfg.sqliteSharedMemory {
		dsn = sqliteDSN("", true)
	}

	db, err := sql.Open("sqlite", dsn)
	require.NoError(t, err, "open sqlite database")
	t.Cleanup(func() { _ = db.Close() })

	pingCtx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	require.NoError(t, db.PingContext(pingCtx), "ping sqlite database")

	return db
}

// engineDialect pairs an engine's RunTestX provisioning helper with the
// matching built-in LockerDialect/ElectorDialect from adapter/cron. MariaDB
// pairs with the MySQL dialect (wire-compatible), exactly as dbtest pairs
// MariaDB with the mysql package's dialects.
type engineDialect struct {
	name    string
	runDB   func(t *testing.T, opts ...TestOption) *sql.DB
	locker  cron.LockerDialect
	elector cron.ElectorDialect
}

// dialectFor returns the four engines this module certifies, each paired
// with its coordinator dialects, so locker_conformance_test.go and
// elector_conformance_test.go can run the SAME shared conformance body
// (runLockerConformance / runElectorConformance) against every engine.
func dialectFor() []engineDialect {
	return []engineDialect{
		{name: "postgres", runDB: RunTestDatabase, locker: cron.PostgresLocker(), elector: cron.PostgresElector()},
		{name: "mysql", runDB: RunTestMySQL, locker: cron.MySQLLocker(), elector: cron.MySQLElector()},
		{name: "mariadb", runDB: RunTestMariaDB, locker: cron.MySQLLocker(), elector: cron.MySQLElector()},
		{name: "sqlite", runDB: RunTestSQLite, locker: cron.SQLiteLocker(), elector: cron.SQLiteElector()},
	}
}
