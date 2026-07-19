package cron

import (
	"context"
	stdsql "database/sql"
	"errors"
	"fmt"
	"time"
)

// LockerDialect owns the per-engine SQL for the per-fire dedup Locker. It is the
// exported SPI a caller supplies to NewSQLLocker (built-ins PostgresLocker() /
// MySQLLocker() / SQLiteLocker()). Every method validates+quotes its table
// first, uses the DB server clock for claimed_at, and passes scope/fire as bound
// parameters. fire is the schedule's fire time — the dedup key alongside scope,
// and it is instance-invariant ONLY for grid-aligned schedules (standard cron /
// descriptors) under bounded clock skew (skew ≪ smallest inter-fire gap); it is
// NOT invariant for "@every" (NewSource refuses that combination outright,
// ErrLockerRequiresGridSchedule — see the Locker interface godoc).
//
// PRECONDITION: q must be an autocommitting handle (*sql.DB), never a *sql.Tx —
// each method's atomicity reasoning relies on a fresh per-statement snapshot.
type LockerDialect interface {
	// ClaimFire idempotently inserts (scope, fire_ts) and reports whether THIS
	// call inserted the row (won). A conflict (row already present) is
	// won=false. There is no recorded claimant identity — the winner is
	// decided solely by whose INSERT succeeds.
	ClaimFire(ctx context.Context, q Querier, table, scope string, fire time.Time) (won bool, err error)
	// PurgeFired deletes fired-keys rows whose claimed_at (DB clock) is older than
	// olderThan and returns the count removed.
	PurgeFired(ctx context.Context, q Querier, table string, olderThan time.Duration) (int64, error)
	// EnsureFiredSchema idempotently creates the fired-keys table (CREATE ... IF
	// NOT EXISTS). Opt-in; production provisions via the *LockerDDL builder.
	EnsureFiredSchema(ctx context.Context, q Querier, table string) error
}

// --- PostgreSQL --------------------------------------------------------------

type postgresLocker struct{}

// PostgresLocker returns the PostgreSQL LockerDialect (also serves wire-compatible
// derivatives). Stateless; safe to share.
func PostgresLocker() LockerDialect { return postgresLocker{} }

func (postgresLocker) ClaimFire(ctx context.Context, q Querier, table, scope string, fire time.Time) (bool, error) {
	qt, err := quoteTable(pgQuote, table)
	if err != nil {
		return false, err
	}
	var returned string
	err = q.QueryRowContext(ctx,
		fmt.Sprintf(`INSERT INTO %s (scope, fire_ts, claimed_at) VALUES ($1, $2, now())
ON CONFLICT (scope, fire_ts) DO NOTHING
RETURNING scope`, qt),
		scope, fire.UTC(),
	).Scan(&returned)
	if errors.Is(err, stdsql.ErrNoRows) {
		return false, nil // conflict: another instance already claimed this fire
	}
	if err != nil {
		return false, err
	}
	return true, nil // inserted: this instance won the fire
}

func (postgresLocker) PurgeFired(ctx context.Context, q Querier, table string, olderThan time.Duration) (int64, error) {
	qt, err := quoteTable(pgQuote, table)
	if err != nil {
		return 0, err
	}
	res, err := q.ExecContext(ctx,
		fmt.Sprintf(`DELETE FROM %s WHERE claimed_at < now() - ($1 * interval '1 microsecond')`, qt),
		olderThan.Microseconds())
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// EnsureFiredSchema issues the CREATE TABLE then the CREATE INDEX as two
// separate ExecContext calls — never a single combined multi-statement Exec.
// pgx's extended protocol (used by the pgx-backed callers of this dialect)
// rejects multi-statement Exec, so a combined statement would error on real
// Postgres (mirrors adapter/database/sql/postgres's EnsureSchema). Both
// statements are IF NOT EXISTS, so the sequence stays idempotent even if the
// first Exec already ran on a prior call.
func (postgresLocker) EnsureFiredSchema(ctx context.Context, q Querier, table string) error {
	qt, err := quoteTable(pgQuote, table)
	if err != nil {
		return err
	}
	if _, err := q.ExecContext(ctx, postgresCreateFiredTable(qt)); err != nil {
		return err
	}
	qidx, _ := quoteTable(pgQuote, table+"_claimed_idx")
	_, err = q.ExecContext(ctx, postgresCreateFiredIndex(qt, qidx))
	return err
}

// postgresCreateFiredTable builds the idempotent CREATE TABLE for the PG
// fired-keys schema. qt must already be quoted.
func postgresCreateFiredTable(qt string) string {
	return fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
  scope      VARCHAR(255) NOT NULL,
  fire_ts    TIMESTAMPTZ  NOT NULL,
  claimed_at TIMESTAMPTZ  NOT NULL DEFAULT now(),
  PRIMARY KEY (scope, fire_ts)
)`, qt)
}

// postgresCreateFiredIndex builds the retention index on claimed_at. qt/qidx
// must already be quoted.
func postgresCreateFiredIndex(qt, qidx string) string {
	return fmt.Sprintf(`CREATE INDEX IF NOT EXISTS %s ON %s (claimed_at)`, qidx, qt)
}

// PostgresLockerDDL returns the reference CREATE TABLE (+ retention index) for
// the PG fired-keys table, as a single combined statement, for a migration
// tool to run directly (msgin itself always issues these as two separate Execs
// — see EnsureFiredSchema). It validates table first.
func PostgresLockerDDL(table string) (string, error) {
	qt, err := quoteTable(pgQuote, table)
	if err != nil {
		return "", err
	}
	qidx, _ := quoteTable(pgQuote, table+"_claimed_idx")
	return postgresCreateFiredTable(qt) + ";\n" + postgresCreateFiredIndex(qt, qidx) + ";", nil
}

// --- MySQL / MariaDB ----------------------------------------------------------

type mysqlLocker struct{}

// MySQLLocker returns the MySQL/MariaDB LockerDialect. Stateless; safe to share.
func MySQLLocker() LockerDialect { return mysqlLocker{} }

// ClaimFire implements LockerDialect for MySQL, mirroring
// mysql.InsertInboxIfAbsent (ADR 0010 D10) exactly: MySQL has no
// ON CONFLICT ... RETURNING, so it INSERT IGNOREs then VERIFIES the outcome
// with a locking SELECT, never trusting rowsAffected alone — INSERT IGNORE
// turns a duplicate-key into a no-op (rowsAffected==0) but ALSO demotes genuine
// data errors (CHECK/FK/truncation/range) to warnings with rowsAffected==0. A
// freshly inserted row (rowsAffected==1) is this instance winning the fire;
// otherwise a SELECT ... LOCK IN SHARE MODE (NOT the MySQL-8-only FOR SHARE —
// so the read also runs on MariaDB, which rejects FOR SHARE as a parse error)
// decides: a row present means another instance already claimed this fire
// (won=false, err=nil); a row absent means INSERT IGNORE swallowed a real error
// and recorded nothing — that is surfaced as ErrLockerClaimFailed rather than
// silently treated as "claimed by another" (which would skip the fire on every
// instance).
func (mysqlLocker) ClaimFire(ctx context.Context, q Querier, table, scope string, fire time.Time) (bool, error) {
	qt, err := quoteTable(mysqlQuote, table)
	if err != nil {
		return false, err
	}
	res, err := q.ExecContext(ctx,
		fmt.Sprintf("INSERT IGNORE INTO %s (scope, fire_ts, claimed_at) VALUES (?, ?, UTC_TIMESTAMP(6))", qt),
		scope, fire.UTC(),
	)
	if err != nil {
		return false, err // a non-demoted error still surfaces normally
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	if n == 1 {
		return true, nil // freshly inserted: this instance won the fire
	}
	// n==0: another instance already claimed this fire OR a demoted data error —
	// verify with a locking read of the current committed state.
	var found string
	err = q.QueryRowContext(ctx,
		fmt.Sprintf("SELECT scope FROM %s WHERE scope=? AND fire_ts=? LOCK IN SHARE MODE", qt),
		scope, fire.UTC(),
	).Scan(&found)
	if errors.Is(err, stdsql.ErrNoRows) {
		return false, fmt.Errorf("%w: scope %q fire %s (INSERT IGNORE affected no row and none exists)",
			ErrLockerClaimFailed, scope, fire.UTC())
	}
	if err != nil {
		return false, err
	}
	return false, nil // verified: another instance already claimed this fire
}

// PurgeFired implements LockerDialect for MySQL: it deletes fired-keys rows
// older than olderThan (DB clock) and returns the count removed.
func (mysqlLocker) PurgeFired(ctx context.Context, q Querier, table string, olderThan time.Duration) (int64, error) {
	qt, err := quoteTable(mysqlQuote, table)
	if err != nil {
		return 0, err
	}
	res, err := q.ExecContext(ctx,
		fmt.Sprintf("DELETE FROM %s WHERE claimed_at < DATE_SUB(UTC_TIMESTAMP(6), INTERVAL ? MICROSECOND)", qt),
		olderThan.Microseconds())
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// EnsureFiredSchema issues MySQLLockerDDL as a SINGLE ExecContext call: unlike
// Postgres/SQLite, the MySQL/MariaDB fired-keys schema declares its retention
// index INLINE in the CREATE TABLE (MySQL has no CREATE INDEX IF NOT EXISTS),
// so MySQLLockerDDL is genuinely one statement — there is nothing to split.
func (mysqlLocker) EnsureFiredSchema(ctx context.Context, q Querier, table string) error {
	ddl, err := MySQLLockerDDL(table)
	if err != nil {
		return err
	}
	_, err = q.ExecContext(ctx, ddl)
	return err
}

// MySQLLockerDDL returns the reference CREATE TABLE for the MySQL/MariaDB
// fired-keys table, for a migration tool. The retention index on claimed_at is
// declared INLINE (MySQL has no CREATE INDEX IF NOT EXISTS), so the whole
// schema is a single statement. It validates table first.
func MySQLLockerDDL(table string) (string, error) {
	qt, err := quoteTable(mysqlQuote, table)
	if err != nil {
		return "", err
	}
	qidx, _ := quoteTable(mysqlQuote, table+"_claimed_idx")
	return fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %[1]s (
  scope      VARCHAR(255) NOT NULL,
  fire_ts    DATETIME(6)  NOT NULL,
  claimed_at DATETIME(6)  NOT NULL DEFAULT (UTC_TIMESTAMP(6)),
  PRIMARY KEY (scope, fire_ts),
  INDEX %[2]s (claimed_at)
)`, qt, qidx), nil
}

// --- SQLite --------------------------------------------------------------------

type sqliteLocker struct{}

// SQLiteLocker returns the SQLite LockerDialect. Stateless; safe to share.
func SQLiteLocker() LockerDialect { return sqliteLocker{} }

// ClaimFire implements LockerDialect for SQLite, mirroring
// sqlite.InsertInboxIfAbsent exactly: an INSERT ... ON CONFLICT DO NOTHING
// RETURNING distinguishes an insert (a row is RETURNed → won=true) from a
// conflict (sql.ErrNoRows → won=false) exactly, never trusting rowsAffected.
// SQLite has no INSERT-IGNORE demotion problem (unlike MySQL), so there is no
// ErrLockerClaimFailed path here. fire_ts is stored as INTEGER epoch
// microseconds (fire.UTC().UnixMicro()) — the fire time is an app-computed
// deterministic key, unlike claimed_at (the DB clock), so it is NOT rounded
// through the DB clock expression.
func (sqliteLocker) ClaimFire(ctx context.Context, q Querier, table, scope string, fire time.Time) (bool, error) {
	qt, err := quoteTable(sqliteQuote, table)
	if err != nil {
		return false, err
	}
	var returned string
	err = q.QueryRowContext(ctx,
		fmt.Sprintf(`INSERT INTO %s (scope, fire_ts, claimed_at) VALUES (?, ?, %s)
ON CONFLICT (scope, fire_ts) DO NOTHING
RETURNING scope`, qt, nowMicrosSQLite),
		scope, fire.UTC().UnixMicro(),
	).Scan(&returned)
	if errors.Is(err, stdsql.ErrNoRows) {
		return false, nil // conflict: another instance already claimed this fire
	}
	if err != nil {
		return false, err
	}
	return true, nil // inserted: this instance won the fire
}

// PurgeFired implements LockerDialect for SQLite: it deletes fired-keys rows
// whose claimed_at (DB clock, µs) is older than olderThan and returns the count.
func (sqliteLocker) PurgeFired(ctx context.Context, q Querier, table string, olderThan time.Duration) (int64, error) {
	qt, err := quoteTable(sqliteQuote, table)
	if err != nil {
		return 0, err
	}
	res, err := q.ExecContext(ctx,
		fmt.Sprintf(`DELETE FROM %s WHERE claimed_at < %s - ?`, qt, nowMicrosSQLite),
		olderThan.Microseconds())
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// EnsureFiredSchema issues the CREATE TABLE then the CREATE INDEX as two
// separate ExecContext calls — mirrors postgresLocker.EnsureFiredSchema (never
// a single combined multi-statement Exec, for parity across dialects and with
// drivers that reject multi-statement Exec). Both statements are IF NOT
// EXISTS, so the sequence stays idempotent even if the first Exec already ran
// on a prior call.
func (sqliteLocker) EnsureFiredSchema(ctx context.Context, q Querier, table string) error {
	qt, err := quoteTable(sqliteQuote, table)
	if err != nil {
		return err
	}
	if _, err := q.ExecContext(ctx, sqliteCreateFiredTable(qt)); err != nil {
		return err
	}
	qidx, _ := quoteTable(sqliteQuote, table+"_claimed_idx")
	_, err = q.ExecContext(ctx, sqliteCreateFiredIndex(qt, qidx))
	return err
}

// sqliteCreateFiredTable builds the idempotent CREATE TABLE for the SQLite
// fired-keys schema. qt must already be quoted.
func sqliteCreateFiredTable(qt string) string {
	return fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
  scope      TEXT    NOT NULL,
  fire_ts    INTEGER NOT NULL,
  claimed_at INTEGER NOT NULL DEFAULT (%s),
  PRIMARY KEY (scope, fire_ts)
)`, qt, nowMicrosSQLite)
}

// sqliteCreateFiredIndex builds the retention index on claimed_at. qt/qidx
// must already be quoted.
func sqliteCreateFiredIndex(qt, qidx string) string {
	return fmt.Sprintf(`CREATE INDEX IF NOT EXISTS %s ON %s (claimed_at)`, qidx, qt)
}

// SQLiteLockerDDL returns the reference CREATE TABLE (+ retention index) for
// the SQLite fired-keys table, as a single combined statement, for a migration
// tool to run directly (msgin itself always issues these as two separate Execs
// — see EnsureFiredSchema). It validates table first.
func SQLiteLockerDDL(table string) (string, error) {
	qt, err := quoteTable(sqliteQuote, table)
	if err != nil {
		return "", err
	}
	qidx, _ := quoteTable(sqliteQuote, table+"_claimed_idx")
	return sqliteCreateFiredTable(qt) + ";\n" + sqliteCreateFiredIndex(qt, qidx) + ";", nil
}
