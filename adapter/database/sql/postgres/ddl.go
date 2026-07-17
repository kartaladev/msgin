package postgres

import (
	"fmt"

	msginsql "github.com/kartaladev/msgin/adapter/database/sql"
)

// DDL returns the reference CREATE TABLE (+ claim index) statement for the
// lease/claim schema on PostgreSQL, for callers to fold into their own
// migration tool. It validates table (ErrInvalidTableName on a bad identifier)
// before building the statement; msgin never runs this on the production path
// (ADR 0010 D2 — the caller provisions the schema).
func DDL(table string) (string, error) {
	if err := msginsql.ValidateIdent(table); err != nil {
		return "", err
	}
	return postgresDialect{}.ddl(table), nil
}

// InboxDDL returns the reference CREATE TABLE (+ retention index) statement for
// the idempotent-consumer dedup inbox on PostgreSQL, for callers to fold into
// their own migration tool (ADR 0010 D10). It validates table
// (ErrInvalidTableName on a bad identifier) as the sole entry point, applying
// the same identifier-injection discipline as DDL (reference finding I1): there
// is no string-returning DDL method on the InboxDialect interface to bypass
// validation.
func InboxDDL(table string) (string, error) {
	if err := msginsql.ValidateIdent(table); err != nil {
		return "", err
	}
	return postgresInboxDDL(table), nil
}

// postgresCreateTable builds the idempotent CREATE TABLE for the lease/claim
// schema (ADR 0010 D4). qt must be an already-quoted identifier. There is no
// explicit status column: readiness is encoded by locked_at + visible_after +
// lease expiry, one source of truth.
func postgresCreateTable(qt string) string {
	return fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
  id             BIGSERIAL     PRIMARY KEY,
  msg_id         VARCHAR(255)  NOT NULL,
  headers        JSONB         NOT NULL,
  payload        BYTEA         NOT NULL,
  locked_by      VARCHAR(255),
  locked_at      TIMESTAMPTZ,
  visible_after  TIMESTAMPTZ   NOT NULL DEFAULT now(),
  delivery_count INTEGER       NOT NULL DEFAULT 0,
  lease_epoch    BIGINT        NOT NULL DEFAULT 0,
  created_at     TIMESTAMPTZ   NOT NULL DEFAULT now()
)`, qt)
}

// postgresCreateIndex builds the partial claim index. qt is the already-quoted
// table; qidx is the already-quoted index name.
func postgresCreateIndex(qt, qidx string) string {
	return fmt.Sprintf(
		`CREATE INDEX IF NOT EXISTS %s ON %s (visible_after) WHERE locked_at IS NULL`,
		qidx, qt,
	)
}

// ddl builds the combined reference DDL (table + index) for an already-
// validated table name. It is unexported by design: it does not (and a
// string return cannot) revalidate, so it must never be reachable with an
// untrusted identifier. The only public entry point is DDL, which
// ValidateIdent first (review finding I1).
func (postgresDialect) ddl(table string) string {
	qt := pgQuote(table)
	qidx := pgQuote(table + "_claim_idx")
	return postgresCreateTable(qt) + ";\n" + postgresCreateIndex(qt, qidx) + ";"
}

// postgresCreateInboxTable builds the idempotent CREATE TABLE for the dedup
// inbox schema on PostgreSQL (ADR 0010 D10). qt must be an already-quoted
// identifier. msg_id is the PRIMARY KEY (the dedup key); processed_at (DB clock)
// drives Purge retention.
func postgresCreateInboxTable(qt string) string {
	return fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
  msg_id       VARCHAR(255)  PRIMARY KEY,
  processed_at TIMESTAMPTZ   NOT NULL DEFAULT now()
)`, qt)
}

// postgresCreateInboxIndex builds the retention index on processed_at (so Purge
// does not full-scan). qt is the already-quoted table; qidx the quoted index.
func postgresCreateInboxIndex(qt, qidx string) string {
	return fmt.Sprintf(`CREATE INDEX IF NOT EXISTS %s ON %s (processed_at)`, qidx, qt)
}

// postgresInboxDDL builds the combined reference inbox DDL (table + index) for an
// already-validated table name. Unexported (a string return cannot revalidate);
// the only public entry point is InboxDDL, which ValidateIdent first.
func postgresInboxDDL(table string) string {
	qt := pgQuote(table)
	qidx := pgQuote(table + "_processed_idx")
	return postgresCreateInboxTable(qt) + ";\n" + postgresCreateInboxIndex(qt, qidx) + ";"
}
