package postgres

import (
	"fmt"

	msginsql "github.com/kartaladev/msgin/adapter/database/sql"
)

// GroupDDL returns the reference CREATE TABLE statements (group-lease table +
// member table + supporting indexes) for the durable Aggregator group store on
// PostgreSQL, for callers to fold into their own migration tool. It validates
// table (ErrInvalidTableName on a bad identifier) before building — the sole
// entry point, applying the same identifier-injection discipline as DDL: there
// is no string-returning DDL method on the GroupDialect interface to bypass
// validation (ADR 0021 §3). msgin never runs this on the production path (the
// caller provisions the schema).
func GroupDDL(table string) (string, error) {
	if err := msginsql.ValidateIdent(table); err != nil {
		return "", err
	}
	return postgresGroupDialect{}.groupDDL(table), nil
}

// postgresCreateGroupTable builds the idempotent CREATE TABLE for the group-lease
// table. qt must be an already-quoted identifier. created_at/locked_at are epoch
// microseconds (BIGINT, DB clock) so lease/expiry math is portable integer
// arithmetic; epoch is the fence token bumped on each claim.
func postgresCreateGroupTable(qt string) string {
	return fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
  group_key   VARCHAR(255) PRIMARY KEY,
  created_at  BIGINT       NOT NULL,
  epoch       BIGINT       NOT NULL DEFAULT 0,
  locked_by   VARCHAR(255),
  locked_at   BIGINT
)`, qt)
}

// postgresCreateMemberTable builds the idempotent CREATE TABLE for the
// append-only member table. qt must be already-quoted. PRIMARY KEY
// (group_key, msg_id) is the idempotency key for AddMember; claimed_epoch NULL =
// live, non-NULL = frozen into an in-flight claim; headers/payload are raw
// framed bytes stored verbatim.
func postgresCreateMemberTable(qt string) string {
	return fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
  group_key     VARCHAR(255) NOT NULL,
  msg_id        VARCHAR(255) NOT NULL,
  seq           BIGINT,
  headers       BYTEA        NOT NULL,
  payload       BYTEA        NOT NULL,
  claimed_epoch BIGINT,
  PRIMARY KEY (group_key, msg_id)
)`, qt)
}

// postgresCreateGroupIndex builds the expiry index (created_at) so the reaper's
// age-based scan is not a full table scan. qt/qidx are already-quoted.
func postgresCreateGroupIndex(qt, qidx string) string {
	return fmt.Sprintf(`CREATE INDEX IF NOT EXISTS %s ON %s (created_at)`, qidx, qt)
}

// postgresCreateMemberIndex builds the (group_key, claimed_epoch) index used by
// the live-member and claimed-member selects and the re-absorb UPDATE.
func postgresCreateMemberIndex(qt, qidx string) string {
	return fmt.Sprintf(`CREATE INDEX IF NOT EXISTS %s ON %s (group_key, claimed_epoch)`, qidx, qt)
}

// groupDDL builds the combined reference DDL (both tables + indexes) for an
// already-validated table name. Unexported (a string return cannot revalidate);
// the only public entry point is GroupDDL, which ValidateIdent first.
func (postgresGroupDialect) groupDDL(table string) string {
	gt := pgQuote(table)
	mt := pgQuote(table + "_member")
	return postgresCreateGroupTable(gt) + ";\n" +
		postgresCreateMemberTable(mt) + ";\n" +
		postgresCreateGroupIndex(gt, pgQuote(table+"_expiry_idx")) + ";\n" +
		postgresCreateMemberIndex(mt, pgQuote(table+"_member_claim_idx")) + ";"
}
