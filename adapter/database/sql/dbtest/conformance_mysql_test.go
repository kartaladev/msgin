package dbtest_test

import (
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/kartaladev/msgin/adapter/database/sql/harness"
	"github.com/kartaladev/msgin/adapter/database/sql/mysql"
)

// mysqlKit builds the harness TestKit for the built-in MySQL/MariaDB dialect:
// the dialect under test (mysql.LeaseDialect/InboxDialect) plus the
// per-dialect SQL primitives the harness's own verification queries need but
// that are deliberately NOT on the SPI — identifier quoting (backticks),
// placeholders (?), the server-now expression (UTC_TIMESTAMP(6)), the
// headers→text cast (MySQL's JSON column already returns text bytes as-is,
// unlike Postgres's jsonb), and the reference-DDL builders
// (mysql.DDL/InboxDDL). MySQLFamily is true so the harness also runs the
// INSERT-IGNORE false-positive guard (RunInbox), which has no Postgres
// equivalent. openDB opens an ADDITIONAL *sql.DB for RunLock's separate-pool
// DLQ assertion, against the SAME engine kind as db (a fresh MySQL container
// for the mysql kit, a fresh MariaDB container for the mariadb kit) — the
// property under test is pool separation, not physical-container separation.
func mysqlKit(openDB func(t *testing.T) *sql.DB) harness.TestKit {
	return harness.TestKit{
		Name:            "mysql",
		MySQLFamily:     true,
		Lease:           mysql.LeaseDialect(),
		Inbox:           mysql.InboxDialect(),
		Quote:           func(ident string) string { return "`" + ident + "`" },
		Placeholder:     func(int) string { return "?" },
		NowExpr:         func() string { return "UTC_TIMESTAMP(6)" },
		HeadersTextExpr: func(col string) string { return col },
		DDL:             mysql.DDL,
		InboxDDL:        mysql.InboxDDL,
		Group:           mysql.GroupDialect(),
		GroupDDL:        mysql.GroupDDL,
		OpenDB:          openDB,
	}
}

// TestMySQLConformance runs the full msgin sql-adapter conformance harness
// against a real MySQL container (Plan 006 Task 5). One container backs every
// suite; each harness Run* provisions its own fresh tables, so the suites do
// not interfere. This certifies the built-in mysql dialect drives the
// engine's EXPORTED API correctly end-to-end: lease/claim source, lock/FOR
// UPDATE strategy, outbound INSERT, transactional outbox, dedup inbox, and
// the dialect-level SQL.
func TestMySQLConformance(t *testing.T) {
	db := RunTestMySQL(t)
	kit := mysqlKit(func(t *testing.T) *sql.DB { return RunTestMySQL(t) })

	t.Run("Source", func(t *testing.T) { harness.RunSource(t, kit, db) })
	t.Run("Lock", func(t *testing.T) { harness.RunLock(t, kit, db) })
	t.Run("Outbound", func(t *testing.T) { harness.RunOutbound(t, kit, db) })
	t.Run("Outbox", func(t *testing.T) { harness.RunOutbox(t, kit, db) })
	t.Run("Inbox", func(t *testing.T) { harness.RunInbox(t, kit, db) })
	t.Run("Dialect", func(t *testing.T) { harness.RunDialect(t, kit, db) })
	t.Run("QueueStore", func(t *testing.T) { harness.RunQueueStore(t, kit, db) })
	t.Run("GroupStore", func(t *testing.T) { harness.RunGroupStore(t, kit, db) })
}

// TestMariaDBConformance is the MariaDB peer of TestMySQLConformance: the same
// mysql dialect and harness suite run against a real MariaDB container
// (wire-compatible with MySQL, ADR 0010 D3/D4), certifying the built-in covers
// both engines it claims to.
func TestMariaDBConformance(t *testing.T) {
	db := RunTestMariaDB(t)
	kit := mysqlKit(func(t *testing.T) *sql.DB { return RunTestMariaDB(t) })
	kit.Name = "mariadb"

	t.Run("Source", func(t *testing.T) { harness.RunSource(t, kit, db) })
	t.Run("Lock", func(t *testing.T) { harness.RunLock(t, kit, db) })
	t.Run("Outbound", func(t *testing.T) { harness.RunOutbound(t, kit, db) })
	t.Run("Outbox", func(t *testing.T) { harness.RunOutbox(t, kit, db) })
	t.Run("Inbox", func(t *testing.T) { harness.RunInbox(t, kit, db) })
	t.Run("Dialect", func(t *testing.T) { harness.RunDialect(t, kit, db) })
	t.Run("QueueStore", func(t *testing.T) { harness.RunQueueStore(t, kit, db) })
	t.Run("GroupStore", func(t *testing.T) { harness.RunGroupStore(t, kit, db) })
}

// TestMySQLClaimInExistingTransaction is Plan 006 Task 5 carry-forward item
// (b): the MySQL two-step claim's defensive *sql.Tx branch (the Querier is
// already a transaction, so Claim runs the two-step directly on it, leaving
// commit to the caller who owns the tx) — reproduced here against a real
// MySQL container since the branch runs actual SQL. This is distinct from the
// harness's own ClaimBumpsCountsAndLocks assertion (RunDialect), which always
// passes the pool (*sql.DB), never a pre-opened *sql.Tx.
func TestMySQLClaimInExistingTransaction(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	db := RunTestMySQL(t)
	dialect := mysql.LeaseDialect()

	const table = "msgin_tx_claim"
	if err := dialect.EnsureSchema(ctx, db, table); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}

	if err := dialect.Insert(ctx, db, table, "tx-1", []byte(`{}`), []byte("payload"), 0); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	t.Cleanup(func() { _ = tx.Rollback() })

	// Claim with the *sql.Tx directly: the two-step runs on the caller's tx.
	rows, err := dialect.Claim(ctx, tx, table, 10, "worker-tx", time.Minute)
	if err != nil {
		t.Fatalf("Claim in existing tx: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 claimed row, got %d", len(rows))
	}
	if rows[0].MsgID != "tx-1" {
		t.Errorf("expected msg_id tx-1, got %q", rows[0].MsgID)
	}
	if rows[0].DeliveryCount != 1 {
		t.Errorf("expected delivery_count 1 (post-increment computed in Go), got %d", rows[0].DeliveryCount)
	}
	if rows[0].LeaseEpoch != 1 {
		t.Errorf("expected lease_epoch 1 (post-increment computed in Go), got %d", rows[0].LeaseEpoch)
	}

	// The caller owns the lifecycle: the claim is only durable once the caller
	// commits. After commit, the row is leased and not re-claimable on the pool.
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	again, err := dialect.Claim(ctx, db, table, 10, "worker-tx", time.Minute)
	if err != nil {
		t.Fatalf("re-claim: %v", err)
	}
	if len(again) != 0 {
		t.Errorf("a live-leased row must not be re-claimable after the caller's tx commits, got %d rows", len(again))
	}

	var lockedBy string
	if err := db.QueryRowContext(ctx,
		fmt.Sprintf("SELECT locked_by FROM `%s` WHERE msg_id = ?", table), "tx-1").Scan(&lockedBy); err != nil {
		t.Fatalf("verify locked_by: %v", err)
	}
	if lockedBy != "worker-tx" {
		t.Errorf("expected the committed claim to stamp locked_by=worker-tx, got %q", lockedBy)
	}
}
