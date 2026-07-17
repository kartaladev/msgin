package sql_test

import (
	"testing"

	"go.uber.org/goleak"
)

// TestMain runs the package's goroutine-leak check. Locally the testcontainers,
// Docker-client, and HTTP-pool background goroutines (from the MySQL/MariaDB
// real-DB suites still in root — Plan 006 Task 4) settle within goleak's retry
// window, but those are timing-dependent and can linger past the window on a
// slower/busier CI host, which would flake the whole package. The ignore list
// below is a DEFENSIVE guard for exactly those known container-plumbing
// top-of-stack functions, so a real leaked msgin poll/worker/sweep goroutine is
// still caught while container plumbing is not (use-testcontainers / ADR 0010).
//
// It was relocated here when the Postgres suites (which previously hosted it)
// moved to the dbtest runner module. The dbtest module carries its own
// equivalent goleak TestMain for the harness runs.
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
