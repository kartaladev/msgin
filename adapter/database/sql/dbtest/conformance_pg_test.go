package dbtest_test

import (
	"database/sql"
	"fmt"
	"testing"

	"github.com/kartaladev/msgin/adapter/database/sql/harness"
	"github.com/kartaladev/msgin/adapter/database/sql/postgres"
	"go.uber.org/goleak"
)

// TestMain runs the module's goroutine-leak check. The testcontainers,
// Docker-client, and HTTP-pool background goroutines settle within goleak's
// retry window locally, but can linger past it on a slower/busier CI host,
// which would flake the whole module. The ignore list below is a DEFENSIVE
// guard for exactly those known container-plumbing top-of-stack functions, so a
// real leaked msgin poll/worker/sweep goroutine is still caught while container
// plumbing is not (use-testcontainers / ADR 0010).
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

// postgresKit builds the harness TestKit for the built-in PostgreSQL dialect:
// the dialect under test (postgres.LeaseDialect/InboxDialect) plus the
// per-dialect SQL primitives the harness's own verification queries need but
// that are deliberately NOT on the SPI — identifier quoting (double quotes),
// placeholders ($n), the server-now expression (now()), the headers→text cast
// (::text), and the reference-DDL builders (postgres.DDL/InboxDDL).
func postgresKit() harness.TestKit {
	return harness.TestKit{
		Name:            "postgres",
		MySQLFamily:     false,
		Lease:           postgres.LeaseDialect(),
		Inbox:           postgres.InboxDialect(),
		Quote:           func(ident string) string { return `"` + ident + `"` },
		Placeholder:     func(n int) string { return fmt.Sprintf("$%d", n) },
		NowExpr:         func() string { return "now()" },
		HeadersTextExpr: func(col string) string { return col + "::text" },
		DDL:             postgres.DDL,
		InboxDDL:        postgres.InboxDDL,
		Group:           postgres.GroupDialect(),
		GroupDDL:        postgres.GroupDDL,
		// OpenDB opens an ADDITIONAL, independently-pooled *sql.DB for RunLock's
		// separate-pool DLQ assertion. The property under test is pool
		// separation, not physical-container separation (harness TestKit doc),
		// so a fresh throwaway container satisfies it.
		OpenDB: func(t *testing.T) *sql.DB { return RunTestDatabase(t) },
	}
}

// TestPostgresConformance runs the full msgin sql-adapter conformance harness
// against a real PostgreSQL container (Plan 006 Task 4 — the harness's first
// real execution). One container backs every suite; each harness Run* provisions
// its own fresh tables, so the suites do not interfere. This certifies the
// built-in postgres dialect drives the engine's EXPORTED API correctly
// end-to-end: lease/claim source, lock/FOR UPDATE strategy, outbound INSERT,
// transactional outbox, dedup inbox, and the dialect-level SQL.
func TestPostgresConformance(t *testing.T) {
	db := RunTestDatabase(t)
	kit := postgresKit()

	t.Run("Source", func(t *testing.T) { harness.RunSource(t, kit, db) })
	t.Run("Lock", func(t *testing.T) { harness.RunLock(t, kit, db) })
	t.Run("Outbound", func(t *testing.T) { harness.RunOutbound(t, kit, db) })
	t.Run("Outbox", func(t *testing.T) { harness.RunOutbox(t, kit, db) })
	t.Run("Inbox", func(t *testing.T) { harness.RunInbox(t, kit, db) })
	t.Run("Dialect", func(t *testing.T) { harness.RunDialect(t, kit, db) })
	t.Run("QueueStore", func(t *testing.T) { harness.RunQueueStore(t, kit, db) })
	t.Run("GroupStore", func(t *testing.T) { harness.RunGroupStore(t, kit, db) })
}
