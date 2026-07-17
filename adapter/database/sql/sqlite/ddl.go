package sqlite

import (
	"fmt"

	msginsql "github.com/kartaladev/msgin/adapter/database/sql"
)

// nowMicros is the DB-clock expression: current UTC time as epoch microseconds
// (INTEGER). SQLite's 'subsec' modifier yields sub-second (millisecond)
// resolution; *1000000 expresses it in microseconds so all interval arithmetic
// matches the .Microseconds() convention the postgres/mysql dialects use
// (ADR 0012 D4). Requires SQLite >=3.42.
const nowMicros = `CAST(unixepoch('now','subsec')*1000000 AS INTEGER)`

// DDL returns the reference CREATE TABLE (+ claim index) for the lease/claim
// schema on SQLite, for callers to fold into their migration tool. It validates
// table before building; msgin never runs it on the production path (ADR 0010 D2).
func DDL(table string) (string, error) {
	if err := msginsql.ValidateIdent(table); err != nil {
		return "", err
	}
	return sqliteDialect{}.ddl(table), nil
}

// InboxDDL returns the reference CREATE TABLE (+ retention index) for the
// dedup-inbox schema on SQLite (ADR 0010 D10). It validates table as the sole
// entry point (no string-returning DDL method on the SPI).
func InboxDDL(table string) (string, error) {
	if err := msginsql.ValidateIdent(table); err != nil {
		return "", err
	}
	return sqliteInboxDDL(table), nil
}

// sqliteCreateTable builds the idempotent CREATE TABLE for the lease/claim
// schema. qt must be an already-quoted identifier. Timestamps are INTEGER epoch
// microseconds defaulted from the DB clock; id is a rowid alias (auto-increments).
func sqliteCreateTable(qt string) string {
	return fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
  id             INTEGER PRIMARY KEY,
  msg_id         TEXT    NOT NULL,
  headers        TEXT    NOT NULL,
  payload        BLOB    NOT NULL,
  locked_by      TEXT,
  locked_at      INTEGER,
  visible_after  INTEGER NOT NULL DEFAULT (%s),
  delivery_count INTEGER NOT NULL DEFAULT 0,
  lease_epoch    INTEGER NOT NULL DEFAULT 0,
  created_at     INTEGER NOT NULL DEFAULT (%s)
)`, qt, nowMicros, nowMicros)
}

// sqliteCreateIndex builds the partial claim index (SQLite supports partial
// indexes >=3.8.0). qt is the already-quoted table; qidx the quoted index name.
func sqliteCreateIndex(qt, qidx string) string {
	return fmt.Sprintf(`CREATE INDEX IF NOT EXISTS %s ON %s (visible_after) WHERE locked_at IS NULL`, qidx, qt)
}

// sqliteCreateInboxTable builds the idempotent CREATE TABLE for the dedup inbox.
// msg_id is the TEXT PRIMARY KEY (the dedup key → a unique autoindex);
// processed_at (DB clock, µs) drives Purge retention.
func sqliteCreateInboxTable(qt string) string {
	return fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
  msg_id       TEXT    PRIMARY KEY,
  processed_at INTEGER NOT NULL DEFAULT (%s)
)`, qt, nowMicros)
}

// sqliteCreateInboxIndex builds the retention index on processed_at.
func sqliteCreateInboxIndex(qt, qidx string) string {
	return fmt.Sprintf(`CREATE INDEX IF NOT EXISTS %s ON %s (processed_at)`, qidx, qt)
}

// ddl builds the combined reference DDL (table + index) for an already-validated
// table name. Unexported (a string return cannot revalidate); the only public
// entry point is DDL, which ValidateIdent first.
func (sqliteDialect) ddl(table string) string {
	qt := sqliteQuote(table)
	qidx := sqliteQuote(table + "_claim_idx")
	return sqliteCreateTable(qt) + ";\n" + sqliteCreateIndex(qt, qidx) + ";"
}

// sqliteInboxDDL builds the combined reference inbox DDL for an already-validated
// table name. Unexported; the only public entry point is InboxDDL.
func sqliteInboxDDL(table string) string {
	qt := sqliteQuote(table)
	qidx := sqliteQuote(table + "_processed_idx")
	return sqliteCreateInboxTable(qt) + ";\n" + sqliteCreateInboxIndex(qt, qidx) + ";"
}
