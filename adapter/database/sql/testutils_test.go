package sql_test

// These helpers live in a _test.go file (not a plain testutils.go) on purpose:
// they import a SQL driver and testcontainers-go, which the msgin dependency
// policy (ADR 0003) forbids in non-test production code. Go does not force
// _test.go-only imports onto consumers, so keeping the helpers here keeps those
// heavy deps out of every consumer's build while still making RunTestMySQL /
// RunTestMariaDB reachable from sibling _test packages.
//
// The Postgres helper (RunTestDatabase) and the dialect-parameterized real-DB
// suites moved to the dbtest runner module (Plan 006 Task 4); only the
// MySQL/MariaDB real-DB coverage remains in root until Task 5.

import (
	"context"
	"database/sql"
	"testing"
	"time"

	// Registers the "mysql" database/sql driver (go-sql-driver/mysql) — the
	// same driver a production caller would inject alongside the explicit
	// MySQLDialect().
	_ "github.com/go-sql-driver/mysql"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	tcmariadb "github.com/testcontainers/testcontainers-go/modules/mariadb"
	tcmysql "github.com/testcontainers/testcontainers-go/modules/mysql"
	"github.com/testcontainers/testcontainers-go/wait"
)

// testConfig holds RunTestMySQL / RunTestMariaDB options.
type testConfig struct {
	image string
}

// TestOption customizes a RunTest* helper.
type TestOption func(*testConfig)

// WithImage overrides the pinned container image.
func WithImage(image string) TestOption {
	return func(c *testConfig) { c.image = image }
}

// RunTestMySQL starts a throwaway MySQL container and returns an open *sql.DB
// (go-sql-driver/mysql) pointed at it. The container and connection are torn
// down via t.Cleanup. parseTime=true is set so DATETIME(6) columns scan back
// into time.Time / sql.NullTime (a test probe needs it); the dialect itself
// never scans a timestamp. It is the single MySQL provisioning helper for this
// package's tests (per the use-testcontainers skill).
func RunTestMySQL(t *testing.T, opts ...TestOption) *sql.DB {
	t.Helper()

	// MySQL 8.0.40: SELECT ... FOR UPDATE SKIP LOCKED (8.0.1+) and expression
	// column DEFAULT (UTC_TIMESTAMP(6)) (8.0.13+) are both required by the
	// dialect, and are available here.
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

// RunTestMariaDB starts a throwaway MariaDB container and returns an open *sql.DB
// (go-sql-driver/mysql — MariaDB is wire-compatible and uses the same driver)
// pointed at it. The container and connection are torn down via t.Cleanup.
// parseTime=true is set so DATETIME(6) columns scan back into time.Time (a test
// probe needs it). It is the MariaDB peer of RunTestMySQL. MariaDB conformance
// currently runs from the dbtest module's harness runs (Plan 006 Task 5); this
// helper stays in root until then.
func RunTestMariaDB(t *testing.T, opts ...TestOption) *sql.DB {
	t.Helper()

	// MariaDB 11.4 (LTS): SELECT ... FOR UPDATE SKIP LOCKED, LOCK IN SHARE MODE,
	// and expression column DEFAULT (UTC_TIMESTAMP(6)) are all supported.
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
