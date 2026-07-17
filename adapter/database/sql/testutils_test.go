package sql_test

// These helpers live in a _test.go file (not the plain testutils.go the brief
// names) on purpose: they import SQL drivers and testcontainers-go, which the
// msgin dependency policy (ADR 0003) forbids in non-test production code. Go
// does not force _test.go-only imports onto consumers, so keeping the helpers
// here keeps those heavy deps out of every consumer's build while still making
// RunTestDatabase / RunTestMySQL reachable from sibling _test packages.

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	// Registers the "pgx" database/sql driver. pgx (jackc/pgx/v5) is chosen
	// over lib/pq because it is the modern, actively-maintained PostgreSQL
	// driver, and its driver type PkgPath contains "pgx" — the token the
	// dialect auto-detect matches — so tests exercise the same detection path
	// production callers will hit.
	_ "github.com/jackc/pgx/v5/stdlib"
	// Registers the "mysql" database/sql driver (go-sql-driver/mysql). Its
	// driver type PkgPath contains the "mysql" token, so the MySQL auto-detect
	// path is exercised the same way a production caller hits it.
	_ "github.com/go-sql-driver/mysql"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	tcmariadb "github.com/testcontainers/testcontainers-go/modules/mariadb"
	tcmysql "github.com/testcontainers/testcontainers-go/modules/mysql"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	msginsql "github.com/kartaladev/msgin/adapter/database/sql"
)

// testConfig holds RunTestDatabase / RunTestMySQL options.
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
// package's tests (per the use-testcontainers skill); callers apply their own
// schema via EnsureSchema or the reference DDL.
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
// package's tests (per the use-testcontainers skill), the MySQL peer of
// RunTestDatabase.
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
// probe needs it). It is the MariaDB peer of RunTestMySQL, provisioned ONLY for
// the inbox suite (inboxEngines) to certify the MySQL-family InboxDialect on
// MariaDB too — the ADR claims MariaDB as an auto-detected engine, and its
// INSERT-IGNORE verify read (LOCK IN SHARE MODE) and unique-index probe must hold
// there. The lease/lock/outbox suites are NOT run on MariaDB here (a separate
// follow-up).
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

// engine is one database engine the behavior suites run against: its built-in
// LeaseDialect and the helper that provisions a throwaway container for it. The suites
// iterate over `engines` so the SAME behavior assertions run on both PostgreSQL
// and MySQL, proving the LeaseDialect abstraction holds across engines.
type engine struct {
	name    string
	dialect msginsql.LeaseDialect
	openDB  func(t *testing.T, opts ...TestOption) *sql.DB
}

// engines is the dialect-parameterized table the lease/lock/outbox/dialect
// behavior suites run over (PostgreSQL + MySQL). MariaDB is deliberately NOT here
// — it is scoped to inboxEngines (see below).
var engines = []engine{
	{name: "postgres", dialect: msginsql.PostgresDialect(), openDB: RunTestDatabase},
	{name: "mysql", dialect: msginsql.MySQLDialect(), openDB: RunTestMySQL},
}

// inboxEngines is the engine table the InboxSuite runs over: the shared engines
// PLUS MariaDB. MariaDB is a MySQL-family engine (same driver, MySQLDialect), and
// the inbox path uses MySQL-specific SQL that MariaDB must also accept (the
// INSERT-IGNORE + LOCK IN SHARE MODE verify read, the unique-index probe), so the
// dedup suite certifies it there. Certifying the lease/lock/outbox suites on
// MariaDB is a separate follow-up, so those keep using engines.
var inboxEngines = append(append([]engine{}, engines...),
	engine{name: "mariadb", dialect: msginsql.MySQLDialect(), openDB: RunTestMariaDB},
)

// isMySQL reports whether this engine speaks MySQL-family SQL (MySQL or the
// wire-compatible MariaDB), so a test helper can pick the right identifier
// quoting / placeholder / time expression. MariaDB uses the same driver and
// MySQLDialect, so it is treated identically here.
func (e engine) isMySQL() bool { return e.name == "mysql" || e.name == "mariadb" }

// quote wraps a raw identifier for the engine (backticks for MySQL, double
// quotes for Postgres) — used by the raw SQL the test helpers issue directly
// (row counts, probes), NOT by the dialect (which quotes its own).
func (e engine) quote(ident string) string {
	if e.isMySQL() {
		return "`" + ident + "`"
	}
	return `"` + ident + `"`
}

// ph renders the nth bind placeholder for the engine ("?" for MySQL, "$n" for
// Postgres).
func (e engine) ph(n int) string {
	if e.isMySQL() {
		return "?"
	}
	return fmt.Sprintf("$%d", n)
}

// nowExpr is the engine's current-UTC-time SQL expression. MySQL persists all
// timestamps as UTC via UTC_TIMESTAMP(6), so a test-side comparison MUST use the
// same expression (not NOW(), which is session-timezone dependent) to match the
// dialect's own predicates. Postgres uses now() (a timestamptz, absolute).
func (e engine) nowExpr() string {
	if e.isMySQL() {
		return "UTC_TIMESTAMP(6)"
	}
	return "now()"
}

// headersTextExpr is the SELECT expression that returns the framed headers as
// clean text bytes for the engine. Postgres casts jsonb to text (avoids the
// binary-format prefix); MySQL's JSON column already returns text bytes.
func (e engine) headersTextExpr(col string) string {
	if e.isMySQL() {
		return col
	}
	return col + "::text"
}

// referenceDDL returns the engine's reference DDL for table (PostgresDDL /
// MySQLDDL), so TestReferenceDDLApplies drives the right builder.
func (e engine) referenceDDL(table string) (string, error) {
	if e.isMySQL() {
		return msginsql.MySQLDDL(table)
	}
	return msginsql.PostgresDDL(table)
}

// inboxDialect returns the engine's built-in InboxDialect (the narrow dedup-inbox
// SPI), so the inbox suite can drive InboxDDL and dialect methods for the right
// engine.
func (e engine) inboxDialect() msginsql.InboxDialect {
	if e.isMySQL() {
		return msginsql.MySQLInboxDialect()
	}
	return msginsql.PostgresInboxDialect()
}
