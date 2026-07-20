package sqlite

import (
	"fmt"

	msginsql "github.com/kartaladev/msgin/adapter/database/sql"
)

// GroupDDL returns the reference CREATE TABLE statements (group-lease table +
// member table + supporting indexes) for the durable Aggregator group store on
// SQLite, for callers to fold into their own migration tool. It validates table
// (ErrInvalidTableName on a bad identifier) as the sole entry point — there is
// no string-returning DDL method on the GroupDialect interface to bypass
// validation (ADR 0021 §3). msgin never runs this on the production path.
func GroupDDL(table string) (string, error) {
	if err := msginsql.ValidateIdent(table); err != nil {
		return "", err
	}
	return sqliteGroupDialect{}.groupDDL(table), nil
}

// sqliteCreateGroupTable builds the idempotent CREATE TABLE for the group-lease
// table. qt must be already-quoted. created_at/locked_at are INTEGER epoch
// microseconds (DB clock, the nowMicros expression); epoch is the fence token.
func sqliteCreateGroupTable(qt string) string {
	return fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
  group_key   TEXT    PRIMARY KEY,
  created_at  INTEGER NOT NULL,
  epoch       INTEGER NOT NULL DEFAULT 0,
  locked_by   TEXT,
  locked_at   INTEGER
)`, qt)
}

// sqliteCreateMemberTable builds the idempotent CREATE TABLE for the append-only
// member table. qt must be already-quoted. PRIMARY KEY (group_key, msg_id) is
// the idempotency key; claimed_epoch NULL = live; headers/payload are raw framed
// bytes stored verbatim.
func sqliteCreateMemberTable(qt string) string {
	return fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
  group_key     TEXT    NOT NULL,
  msg_id        TEXT    NOT NULL,
  seq           INTEGER,
  headers       BLOB    NOT NULL,
  payload       BLOB    NOT NULL,
  claimed_epoch INTEGER,
  PRIMARY KEY (group_key, msg_id)
)`, qt)
}

// sqliteCreateGroupIndex builds the expiry index (created_at). qt/qidx are
// already-quoted.
func sqliteCreateGroupIndex(qt, qidx string) string {
	return fmt.Sprintf(`CREATE INDEX IF NOT EXISTS %s ON %s (created_at)`, qidx, qt)
}

// sqliteCreateMemberIndex builds the (group_key, claimed_epoch) index used by
// the live/claimed member selects and the re-absorb UPDATE.
func sqliteCreateMemberIndex(qt, qidx string) string {
	return fmt.Sprintf(`CREATE INDEX IF NOT EXISTS %s ON %s (group_key, claimed_epoch)`, qidx, qt)
}

// groupDDL builds the combined reference DDL (both tables + indexes) for an
// already-validated table name. Unexported (a string return cannot revalidate);
// the only public entry point is GroupDDL, which ValidateIdent first.
func (sqliteGroupDialect) groupDDL(table string) string {
	gt := sqliteQuote(table)
	mt := sqliteQuote(table + "_member")
	return sqliteCreateGroupTable(gt) + ";\n" +
		sqliteCreateMemberTable(mt) + ";\n" +
		sqliteCreateGroupIndex(gt, sqliteQuote(table+"_expiry_idx")) + ";\n" +
		sqliteCreateMemberIndex(mt, sqliteQuote(table+"_member_claim_idx")) + ";"
}
