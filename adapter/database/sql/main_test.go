package sql_test

import (
	"testing"

	"go.uber.org/goleak"
)

// TestMain runs the package's goroutine-leak check. Root is driver-free and
// container-free as of Plan 006 Task 5 (the Postgres and MySQL/MariaDB
// real-DB suites both moved to the dbtest runner module, which carries its
// own equivalent goleak TestMain for the harness runs) — every test here runs
// against the fake dialect or a stub in-process database/sql/driver, so no
// defensive container-plumbing ignore list is needed; a plain check is
// sufficient to catch a real leaked msgin poll/worker/sweep goroutine.
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}
