package sql_test

// This helper lives in a _test.go file (not the plain testutils.go the brief
// names) on purpose: it imports a SQL driver and testcontainers-go, which the
// msgin dependency policy (ADR 0003) forbids in non-test production code. Go
// does not force _test.go-only imports onto consumers, so keeping the helper
// here keeps those heavy deps out of every consumer's build while still making
// RunTestDatabase reachable from sibling _test packages.

import (
	"context"
	"database/sql"
	"testing"
	"time"

	// Registers the "pgx" database/sql driver. pgx (jackc/pgx/v5) is chosen
	// over lib/pq because it is the modern, actively-maintained PostgreSQL
	// driver, and its driver type PkgPath contains "pgx" — the substring
	// Task 5's dialect auto-detect matches — so tests exercise the same
	// detection path production callers will hit.
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

// testConfig holds RunTestDatabase options.
type testConfig struct {
	image string
}

// TestOption customizes RunTestDatabase.
type TestOption func(*testConfig)

// WithImage overrides the pinned PostgreSQL image.
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
