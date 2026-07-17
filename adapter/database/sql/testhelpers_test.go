package sql_test

// Shared root test helpers (fast, no container): a registered fake SQL driver
// and a lazy-open *sql.DB builder, used across this package's construction/unit
// tests (outbound_test.go, inbox_dedup_test.go, strategy_test.go,
// testutils_test.go). Relocated here from the now-deleted detect_test.go (Plan
// 006 Task 1, audit R2-3): the explicit-dialect engine API (ADR 0011) dropped
// driver auto-detect, but these helpers are still the shared way this
// package's tests open a *sql.DB without a real connection.

import (
	"database/sql"
	"database/sql/driver"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

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
// does not outlive the test (goleak). The DSN is only parsed, never dialed:
// go-sql-driver/mysql validates DSN shape eagerly in Open, so the "mysql" driver
// needs a well-formed (but unreachable) DSN, whereas pgx/fake accept any string.
func openDB(t *testing.T, driverName string) *sql.DB {
	t.Helper()
	dsn := "unused-dsn"
	if driverName == "mysql" {
		dsn = "user:pass@tcp(127.0.0.1:3306)/db"
	}
	db, err := sql.Open(driverName, dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return db
}
