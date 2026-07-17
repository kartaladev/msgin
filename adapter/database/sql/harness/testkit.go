// Package harness is a reusable conformance suite for the msgin sql adapter
// engine (github.com/kartaladev/msgin/adapter/database/sql). It is authored as
// its own module (Plan 006 Task 3, ADR 0011) so any dialect author — the
// built-in postgres/mysql leaf-test modules, a future sqlite dialect, or a
// third party — can run the SAME behavior contract against their own
// LeaseDialect/InboxDialect implementation and a real database, without this
// module (or its testify dependency) ever entering the driver-free engine's
// module graph.
//
// The harness drives the engine's EXPORTED API only: RunSource, RunLock,
// RunOutbound, RunOutbox, RunInbox, and RunDialect each take an already-open
// *sql.DB (the caller/runner provisions the container or database) and a
// TestKit describing the dialect under test plus the per-dialect SQL
// primitives the verification queries need but that are deliberately NOT part
// of the LeaseDialect/InboxDialect SPI (identifier quoting, placeholders, the
// server-now expression, ...). See TestKit's field docs for why each knob
// exists.
//
// The harness imports the engine + testify + stdlib database/sql/testing
// ONLY — no driver, no testcontainers (verified by `go list -deps`). It is a
// library of test-driver functions; a dialect's own module (or the dbtest
// runner, for the built-ins) supplies the real database connection and is the
// first place these functions actually execute against one.
package harness

import (
	"database/sql"
	"testing"

	msginsql "github.com/kartaladev/msgin/adapter/database/sql"
)

// TestKit is everything a RunXxx conformance function needs to exercise one
// dialect: the dialect under test (satisfying the engine's exported SPI) plus
// the per-dialect SQL primitives the harness's OWN verification queries need
// but that are deliberately not on LeaseDialect/InboxDialect (identifier
// quoting, placeholders, the server-now expression, the headers→text cast,
// and the reference-DDL builders — a string-returning DDL method cannot
// safely revalidate an identifier, so it is never an SPI method; ADR 0010 D3,
// ADR 0011). A dialect author supplies one TestKit value per engine (e.g. one
// for Postgres, one for MySQL/MariaDB) and passes it to every RunXxx the
// dialect wants certified.
type TestKit struct {
	// Name identifies the engine under test (e.g. "postgres", "mysql",
	// "mariadb") for subtest names and diagnostic messages. Required.
	Name string

	// MySQLFamily reports whether this engine speaks MySQL-family SQL (MySQL
	// itself, or a wire-compatible derivative such as MariaDB). It gates the
	// harness assertions that are genuinely engine-specific — currently the
	// INSERT-IGNORE false-positive guard (RunInbox), which has no equivalent
	// on Postgres (ON CONFLICT ... RETURNING is exact, never demotes an
	// error) — so a non-MySQL-family TestKit skips exactly that assertion
	// rather than the harness silently mis-asserting engine-specific SQL.
	MySQLFamily bool

	// Lease is the LeaseDialect under test — the dialect a real
	// NewPollingSource/NewOutboundAdapter would be constructed with. Required
	// for RunSource, RunOutbound, RunOutbox, RunDialect, and (via a
	// LockDialect type-assertion) RunLock.
	Lease msginsql.LeaseDialect

	// Inbox is the InboxDialect under test — the dialect a real
	// NewInboxDeduper would be constructed with. It is deliberately a
	// separate field (not derived from Lease) because InboxDialect is its
	// own segregated SPI (ADR 0010 D10): a dialect author may supply an
	// InboxDialect value distinct from their LeaseDialect. Required for
	// RunInbox.
	Inbox msginsql.InboxDialect

	// Quote wraps a raw identifier in the engine's quoting style (backticks
	// for MySQL-family, double quotes for Postgres) for the RAW verification
	// SQL the harness issues directly (row counts, column probes) — NOT used
	// by the dialect under test, which quotes its own identifiers
	// internally. Required.
	Quote func(ident string) string

	// Placeholder renders the nth (1-based) bind placeholder for the engine
	// ("?" for MySQL-family, "$n" for Postgres), for the harness's own raw
	// verification SQL. Required.
	Placeholder func(n int) string

	// NowExpr is the engine's current-UTC-time SQL expression, used by the
	// harness's raw verification SQL so a comparison against a
	// dialect-written timestamp uses the SAME clock/expression the dialect
	// itself compares against (e.g. MySQL persists via UTC_TIMESTAMP(6), a
	// session-timezone-independent expression; NOW() would not match).
	// Required.
	NowExpr func() string

	// HeadersTextExpr is the SELECT expression that returns the framed
	// headers column as clean text bytes for the engine (e.g. Postgres casts
	// jsonb to text to avoid the binary-format prefix; MySQL's JSON column
	// already returns text bytes as-is). Required for RunOutbound.
	HeadersTextExpr func(col string) string

	// DDL returns the engine's reference CREATE TABLE (+ claim index)
	// statement for the lease/claim schema — the harness peer of the
	// per-dialect postgres.DDL/mysql.DDL package functions, and itself
	// exercises those functions' ValidateIdent guard (RunDialect calls it
	// with both a valid and a deliberately invalid identifier). Required for
	// RunDialect.
	DDL func(table string) (string, error)

	// InboxDDL returns the engine's reference CREATE TABLE (+ retention
	// index) statement for the dedup-inbox schema — the harness peer of the
	// per-dialect postgres.InboxDDL/mysql.InboxDDL package functions.
	// Required for RunInbox.
	InboxDDL func(table string) (string, error)

	// OpenDB opens an ADDITIONAL, independently-pooled *sql.DB connection to
	// the same test database as the *sql.DB RunLock is called with. It
	// exists for exactly one assertion (RunLock's separate-pool DLQ test,
	// ADR 0010 D5): proving a lock-strategy consumer whose source pool is
	// capped at one connection can still dead-letter through a DIFFERENT
	// pool without deadlock. The property under test is pool separation
	// (an independent *sql.DB's connection limit and connection set), not
	// physical-container separation, so a fresh connection to the SAME
	// database via a new *sql.DB satisfies it. Required for RunLock only;
	// the other RunXxx functions never call it.
	OpenDB func(t *testing.T) *sql.DB
}
