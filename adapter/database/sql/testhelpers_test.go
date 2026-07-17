package sql_test

// Shared root test helpers (fast, no container): a registered fake SQL driver
// and a lazy-open *sql.DB builder, used across this package's construction/unit
// tests (outbound_test.go, inbox_dedup_test.go, strategy_test.go,
// testutils_test.go). Relocated here from the now-deleted detect_test.go (Plan
// 006 Task 1, audit R2-3): the explicit-dialect engine API (ADR 0011) dropped
// driver auto-detect, but these helpers are still the shared way this
// package's tests open a *sql.DB without a real connection.

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

// fakeQuerier is a msginsql.Querier that is neither *sql.DB nor *sql.Tx, so the
// engine's BeginLockTx transaction resolution must reject it — the lock
// strategy needs a real transaction owner. Its methods are never reached
// (BeginLockTx fails on the type first). Used by locktx_unit_test.go's
// unsupported-Querier case.
type fakeQuerier struct{}

func (fakeQuerier) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return nil, nil
}
func (fakeQuerier) QueryContext(context.Context, string, ...any) (*sql.Rows, error) {
	return nil, nil
}
func (fakeQuerier) QueryRowContext(context.Context, string, ...any) *sql.Row { return nil }

// fakeDriver is an unregistered-shape SQL driver, used by tests that need a
// *sql.DB whose driver is irrelevant (the dialect is now always passed
// explicitly, so no driver-shape marker matters). It never actually connects.
type fakeDriver struct{}

func (fakeDriver) Open(string) (driver.Conn, error) {
	return nil, errors.New("fake driver: no connections")
}

const fakeDriverName = "msgin-detect-fake"

func init() { sql.Register(fakeDriverName, fakeDriver{}) }

// openDB opens a *sql.DB on driverName WITHOUT connecting (sql.Open is lazy for
// the connection), closing it via t.Cleanup so the connectionOpener goroutine
// does not outlive the test (goleak). Root no longer registers any real SQL
// driver (Plan 006 Task 5 — the engine is driver-free), so every root test
// opens the fakeDriverName driver above; the DSN is never parsed or dialed.
func openDB(t *testing.T, driverName string) *sql.DB {
	t.Helper()
	db, err := sql.Open(driverName, "unused-dsn")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return db
}
