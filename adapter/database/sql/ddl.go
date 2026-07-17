package sql

import "fmt"

// PostgresDDL returns the reference CREATE TABLE (+ claim index) statement for
// the lease/claim schema on PostgreSQL, for callers to fold into their own
// migration tool. It validates table (ErrInvalidTableName on a bad identifier)
// before building the statement; msgin never runs this on the production path
// (ADR 0010 D2 — the caller provisions the schema).
func PostgresDDL(table string) (string, error) {
	if err := validateIdent(table); err != nil {
		return "", err
	}
	return postgresDialect{}.ddl(table), nil
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
// untrusted identifier. The only public entry point is PostgresDDL, which
// validateIdents first (review finding I1).
func (postgresDialect) ddl(table string) string {
	qt := pgQuote(table)
	qidx := pgQuote(table + "_claim_idx")
	return postgresCreateTable(qt) + ";\n" + postgresCreateIndex(qt, qidx) + ";"
}

// MySQLDDL returns the reference CREATE TABLE statement for the lease/claim
// schema on MySQL, for callers to fold into their own migration tool. It
// validates table (ErrInvalidTableName on a bad identifier) before building the
// statement; msgin never runs this on the production path (ADR 0010 D2 — the
// caller provisions the schema). The claim index is declared INLINE (MySQL has
// no CREATE INDEX IF NOT EXISTS), so the whole schema is a single statement.
func MySQLDDL(table string) (string, error) {
	if err := validateIdent(table); err != nil {
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
// MySQLDDL, which validateIdents first.
func (mysqlDialect) ddl(table string) string {
	qt := mysqlQuote(table)
	qidx := mysqlQuote(table + "_claim_idx")
	return mysqlCreateTable(qt, qidx) + ";"
}
