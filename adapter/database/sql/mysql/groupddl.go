package mysql

import (
	"fmt"

	msginsql "github.com/kartaladev/msgin/adapter/database/sql"
)

// GroupDDL returns the reference CREATE TABLE statements (group-lease table +
// member table, each with inline indexes) for the durable Aggregator group
// store on MySQL/MariaDB, for callers to fold into their own migration tool. It
// validates table (ErrInvalidTableName on a bad identifier) as the sole entry
// point — there is no string-returning DDL method on the GroupDialect interface
// to bypass validation (ADR 0021 §3). msgin never runs this on the production
// path (the caller provisions the schema).
func GroupDDL(table string) (string, error) {
	if err := msginsql.ValidateIdent(table); err != nil {
		return "", err
	}
	return mysqlGroupDialect{}.groupDDL(table), nil
}

// mysqlCreateGroupTable builds the idempotent CREATE TABLE for the group-lease
// table. qt/qidx must be already-quoted. created_at/locked_at are epoch
// microseconds (BIGINT, DB clock); the expiry index (created_at) is inline
// (MySQL has no CREATE INDEX IF NOT EXISTS).
func mysqlCreateGroupTable(qt, qidx string) string {
	return fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %[1]s (
  group_key   VARCHAR(255) NOT NULL,
  created_at  BIGINT       NOT NULL,
  epoch       BIGINT       NOT NULL DEFAULT 0,
  locked_by   VARCHAR(255),
  locked_at   BIGINT,
  PRIMARY KEY (group_key),
  INDEX %[2]s (created_at)
)`, qt, qidx)
}

// mysqlCreateMemberTable builds the idempotent CREATE TABLE for the append-only
// member table. qt/qidx must be already-quoted. PRIMARY KEY (group_key, msg_id)
// is the idempotency key; the (group_key, claimed_epoch) index is inline.
// VARCHAR(255) keys keep the composite PK within InnoDB's key-length limit.
func mysqlCreateMemberTable(qt, qidx string) string {
	return fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %[1]s (
  group_key     VARCHAR(255) NOT NULL,
  msg_id        VARCHAR(255) NOT NULL,
  seq           BIGINT,
  headers       LONGBLOB     NOT NULL,
  payload       LONGBLOB     NOT NULL,
  claimed_epoch BIGINT,
  PRIMARY KEY (group_key, msg_id),
  INDEX %[2]s (group_key, claimed_epoch)
)`, qt, qidx)
}

// groupDDL builds the combined reference DDL (both tables) for an already-
// validated table name. Unexported (a string return cannot revalidate); the
// only public entry point is GroupDDL, which ValidateIdent first.
func (mysqlGroupDialect) groupDDL(table string) string {
	gt := mysqlQuote(table)
	mt := mysqlQuote(table + "_member")
	return mysqlCreateGroupTable(gt, mysqlQuote(table+"_expiry_idx")) + ";\n" +
		mysqlCreateMemberTable(mt, mysqlQuote(table+"_member_claim_idx")) + ";"
}
