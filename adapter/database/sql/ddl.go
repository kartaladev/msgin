package sql

import "fmt"

// MySQLDDL returns the reference CREATE TABLE statement for the lease/claim
// schema on MySQL, for callers to fold into their own migration tool. It
// validates table (ErrInvalidTableName on a bad identifier) before building the
// statement; msgin never runs this on the production path (ADR 0010 D2 — the
// caller provisions the schema). The claim index is declared INLINE (MySQL has
// no CREATE INDEX IF NOT EXISTS), so the whole schema is a single statement.
func MySQLDDL(table string) (string, error) {
	if err := ValidateIdent(table); err != nil {
		return "", err
	}
	return mysqlDialect{}.ddl(table), nil
}

// mysqlCreateTable builds the idempotent CREATE TABLE for the lease/claim schema
// on MySQL (ADR 0010 D3/D4). qt and qidx must be already-quoted identifiers.
// Types mirror the Postgres schema's semantics: BIGINT AUTO_INCREMENT for the id,
// JSON headers, LONGBLOB payload, DATETIME(6) timestamps (all written as UTC via
// UTC_TIMESTAMP(6)). The claim index (locked_at, visible_after) is declared
// inline so EnsureSchema is a single idempotent statement.
func mysqlCreateTable(qt, qidx string) string {
	return fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %[1]s (
  id             BIGINT        AUTO_INCREMENT PRIMARY KEY,
  msg_id         VARCHAR(255)  NOT NULL,
  headers        JSON          NOT NULL,
  payload        LONGBLOB      NOT NULL,
  locked_by      VARCHAR(255),
  locked_at      DATETIME(6),
  visible_after  DATETIME(6)   NOT NULL DEFAULT (UTC_TIMESTAMP(6)),
  delivery_count INTEGER       NOT NULL DEFAULT 0,
  lease_epoch    BIGINT        NOT NULL DEFAULT 0,
  created_at     DATETIME(6)   NOT NULL DEFAULT (UTC_TIMESTAMP(6)),
  INDEX %[2]s (locked_at, visible_after)
)`, qt, qidx)
}

// ddl builds the reference DDL (single CREATE TABLE with inline index) for an
// already-validated table name. Unexported for the same reason as the Postgres
// builder: a string return cannot revalidate, so the only public entry point is
// MySQLDDL, which ValidateIdent first.
func (mysqlDialect) ddl(table string) string {
	qt := mysqlQuote(table)
	qidx := mysqlQuote(table + "_claim_idx")
	return mysqlCreateTable(qt, qidx) + ";"
}

// InboxDDL returns the reference CREATE TABLE (+ retention index) statement for
// the idempotent-consumer dedup inbox on d's engine, for callers to fold into
// their own migration tool (ADR 0010 D10). It validates table
// (ErrInvalidTableName on a bad identifier) as the sole entry point, then
// dispatches to the built-in dialect for its exact SQL — applying the same
// identifier-injection discipline as the source LeaseDialect (reference finding I1):
// there is no string-returning DDL method on the InboxDialect interface to
// bypass validation. A dialect that is not one of the built-ins has no reference
// DDL (a caller supplying a custom InboxDialect provisions its inbox schema
// directly); that is a clear error naming the offending type.
func InboxDDL(d InboxDialect, table string) (string, error) {
	if err := ValidateIdent(table); err != nil {
		return "", err
	}
	switch d.(type) {
	case mysqlDialect:
		return mysqlInboxDDL(table), nil
	default:
		return "", fmt.Errorf(
			"msgin/sql: no reference inbox DDL for dialect type %T; provision its inbox schema directly", d)
	}
}

// mysqlCreateInboxTable builds the idempotent CREATE TABLE for the dedup inbox
// schema on MySQL (ADR 0010 D10). qt and qidx must be already-quoted
// identifiers. The retention index on processed_at is declared INLINE (MySQL has
// no CREATE INDEX IF NOT EXISTS), so the whole schema is a single statement.
// VARCHAR(255) msg_id as PRIMARY KEY stays within InnoDB's key-length limit.
func mysqlCreateInboxTable(qt, qidx string) string {
	return fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %[1]s (
  msg_id       VARCHAR(255)  NOT NULL,
  processed_at DATETIME(6)   NOT NULL DEFAULT (UTC_TIMESTAMP(6)),
  PRIMARY KEY (msg_id),
  INDEX %[2]s (processed_at)
)`, qt, qidx)
}

// mysqlInboxDDL builds the reference inbox DDL (single CREATE TABLE with inline
// index) for an already-validated table name. Unexported for the same reason as
// the other builders; the only public entry point is InboxDDL.
func mysqlInboxDDL(table string) string {
	qt := mysqlQuote(table)
	qidx := mysqlQuote(table + "_processed_idx")
	return mysqlCreateInboxTable(qt, qidx) + ";"
}
