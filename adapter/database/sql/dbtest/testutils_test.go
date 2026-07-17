package dbtest_test

// dbtest is the leaf runner module (Plan 006 Task 4, ADR 0011): nobody imports
// it, so it is the one place the heavy test dependencies live — the SQL
// drivers and testcontainers-go. It EXECUTES the reusable harness conformance
// suite (github.com/kartaladev/msgin/adapter/database/sql/harness) against a
// real database container, per built-in dialect. This file provisions the
// Postgres, MySQL, and MariaDB containers (RunTestMySQL/RunTestMariaDB moved
// here from root's testutils_test.go, Plan 006 Task 5); conformance_pg_test.go
// and conformance_mysql_test.go build each engine's TestKit and run the
// harness against it.

import (
	"context"
	"database/sql"
	"testing"
	"time"

	// Registers the "pgx" database/sql driver. pgx (jackc/pgx/v5) is chosen
	// over lib/pq because it is the modern, actively-maintained PostgreSQL
	// driver — the same driver a production caller would inject alongside the
	// explicit postgres.LeaseDialect().
	_ "github.com/jackc/pgx/v5/stdlib"
	// Registers the "mysql" database/sql driver (go-sql-driver/mysql) — the
	// same driver a production caller would inject alongside the explicit
	// mysql.LeaseDialect(). MariaDB is wire-compatible and uses this same
	// driver.
	_ "github.com/go-sql-driver/mysql"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	tcmariadb "github.com/testcontainers/testcontainers-go/modules/mariadb"
	tcmysql "github.com/testcontainers/testcontainers-go/modules/mysql"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

// testConfig holds RunTestDatabase options.
type testConfig struct {
	image string
}

// TestOption customizes a RunTest* helper.
type TestOption func(*testConfig)

// WithImage overrides the pinned container image.
func WithImage(image string) TestOption {
	return func(c *testConfig) { c.image = image }
}

// RunTestDatabase starts a throwaway PostgreSQL container and returns an open
// *sql.DB (pgx driver) pointed at it. The container and connection are torn
// down via t.Cleanup. It is the single Postgres provisioning helper for this
// module's conformance runs (per the use-testcontainers skill); the harness
// applies its own schema via EnsureSchema or the reference DDL.
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
// down via t.Cleanup. parseTime=true is set so DATETIME(6) columns scan back
// into time.Time / sql.NullTime (a test probe needs it); the dialect itself
// never scans a timestamp. It is the single MySQL provisioning helper for this
// module's conformance runs (per the use-testcontainers skill).
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
// probe needs it). It is the MariaDB peer of RunTestMySQL.
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
