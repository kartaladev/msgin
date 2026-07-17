package dbtest_test

// dbtest is the leaf runner module (Plan 006 Task 4, ADR 0011): nobody imports
// it, so it is the one place the heavy test dependencies live — the SQL driver
// and testcontainers-go. It EXECUTES the reusable harness conformance suite
// (github.com/kartaladev/msgin/adapter/database/sql/harness) against a real
// database container, per built-in dialect. This file provisions the Postgres
// container; conformance_pg_test.go builds the Postgres TestKit and runs the
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
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
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
