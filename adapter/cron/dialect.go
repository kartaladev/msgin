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

// ElectorDialect owns the per-engine atomic acquire-or-renew for the leader-lease
// Elector. Built-ins: PostgresElector()/MySQLElector()/SQLiteElector(). All time
// math uses the DB server clock (skew-free).
//
// PRECONDITION: q must be an autocommitting handle (*sql.DB), never a *sql.Tx —
// the MySQL implementation's three-step sequence (Round-1 audit M-2) relies on
// each statement getting a fresh, independently-committed snapshot; under a
// single REPEATABLE-READ transaction the verifying read would see a stale
// snapshot and could misreport leadership. Violating this precondition breaks
// two things, not one: (a) snapshot freshness — a single REPEATABLE-READ tx pins
// the Step-1 snapshot so Step-2/3's verify reads stale, misfiring the verdict —
// AND (b) it courts an InnoDB gap-lock deadlock: on an absent row the
// SELECT ... LOCK IN SHARE MODE takes a shared gap lock, and two such
// transactions then both INSERT, whose insert-intention locks conflict with the
// other's gap lock, producing error 1213. Autocommit dissolves both hazards
// (each statement's locks release at statement end).
type ElectorDialect interface {
	// AcquireOrRenew atomically sets holder=holder, expires_at=db_now+leaseTTL on
	// the lease row for scope IFF the row is absent, already held by holder, or
	// expired; it returns true iff holder now holds a valid lease.
	AcquireOrRenew(ctx context.Context, q Querier, table, scope, holder string, leaseTTL time.Duration) (isLeader bool, err error)
	// EnsureLeaseSchema idempotently creates the lease table (opt-in).
	EnsureLeaseSchema(ctx context.Context, q Querier, table string) error
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

type postgresElector struct{}

// PostgresElector returns the PostgreSQL ElectorDialect (also serves
// wire-compatible derivatives). Stateless; safe to share.
func PostgresElector() ElectorDialect { return postgresElector{} }

// AcquireOrRenew implements ElectorDialect for PostgreSQL: an
// INSERT ... ON CONFLICT (scope) DO UPDATE ... WHERE ... RETURNING atomically
// sets holder/expires_at iff the row is absent, held by holder, or expired.
// now() is evaluated once per statement in Postgres, so EXCLUDED.expires_at
// (from VALUES) and the WHERE's now() are consistent. The DO UPDATE fires only
// when the row is held by self or expired; otherwise no row is returned.
func (postgresElector) AcquireOrRenew(ctx context.Context, q Querier, table, scope, holder string, ttl time.Duration) (bool, error) {
	qt, err := quoteTable(pgQuote, table)
	if err != nil {
		return false, err
	}
	var gotHolder string
	err = q.QueryRowContext(ctx, fmt.Sprintf(`INSERT INTO %[1]s (scope, holder, expires_at)
VALUES ($1, $2, now() + ($3 * interval '1 microsecond'))
ON CONFLICT (scope) DO UPDATE SET holder = EXCLUDED.holder, expires_at = EXCLUDED.expires_at
WHERE %[1]s.holder = $2 OR %[1]s.expires_at < now()
RETURNING holder`, qt),
		scope, holder, ttl.Microseconds(),
	).Scan(&gotHolder)
	if errors.Is(err, stdsql.ErrNoRows) {
		return false, nil // held by another, not expired → not leader
	}
	if err != nil {
		return false, err
	}
	return gotHolder == holder, nil // RETURNING guarantees this, but assert defensively
}

// EnsureLeaseSchema implements ElectorDialect for PostgreSQL: it idempotently
// creates the lease table.
func (postgresElector) EnsureLeaseSchema(ctx context.Context, q Querier, table string) error {
	qt, err := quoteTable(pgQuote, table)
	if err != nil {
		return err
	}
	_, err = q.ExecContext(ctx, postgresCreateLeaseTable(qt))
	return err
}

// postgresCreateLeaseTable builds the idempotent CREATE TABLE for the PG
// leader-lease schema. qt must already be quoted.
func postgresCreateLeaseTable(qt string) string {
	return fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
  scope      VARCHAR(255) PRIMARY KEY,
  holder     VARCHAR(255) NOT NULL,
  expires_at TIMESTAMPTZ  NOT NULL
)`, qt)
}

// PostgresElectorDDL returns the reference CREATE TABLE for the PG
// leader-lease table, for a migration tool to run directly (msgin itself
// always issues this via EnsureLeaseSchema). It validates table first. No
// secondary index is needed — all access is by the scope PK.
func PostgresElectorDDL(table string) (string, error) {
	qt, err := quoteTable(pgQuote, table)
	if err != nil {
		return "", err
	}
	return postgresCreateLeaseTable(qt) + ";", nil
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

type mysqlElector struct{}

// MySQLElector returns the MySQL/MariaDB ElectorDialect. Stateless; safe to
// share.
func MySQLElector() ElectorDialect { return mysqlElector{} }

// AcquireOrRenew implements ElectorDialect for MySQL/MariaDB. MySQL lacks
// ON CONFLICT ... WHERE ... RETURNING, so acquire-or-renew is a three-step
// atomic-enough sequence; the row's scope PK serializes concurrent writers.
//
// Why this is correct under concurrency — PRECONDITION: autocommit (Round-1
// audit M-2). This reasoning holds ONLY when q is an autocommitting handle
// (*sql.DB), never a single *sql.Tx: each step must observe the latest
// COMMITTED state, not a transaction's fixed snapshot. SQLElector.IsLeader
// always passes e.db (the pool), so the precondition holds in-tree; it is
// documented on ElectorDialect.AcquireOrRenew because Querier is exported
// dialect-author SPI and a custom dialect/caller could otherwise pass a
// *sql.Tx and silently break the argument below.
//
// Two hazards, not one (Round-2 audit NEW-LOW-1): violating the precondition
// breaks (a) snapshot freshness — a single REPEATABLE-READ tx pins the Step-1
// snapshot so Step-2/3's verify reads stale, misfiring the verdict — AND (b) it
// courts an InnoDB gap-lock deadlock: on an absent row the
// SELECT ... LOCK IN SHARE MODE takes a shared gap lock, and two such
// transactions then both INSERT, whose insert-intention locks conflict with
// the other's gap lock -> error 1213. Autocommit dissolves both (each
// statement's locks release at statement end).
//
// Given autocommit: the lease row's scope PK serializes the UPDATE and the
// INSERT across instances. Step 1 wins for the incumbent leader (holder=self)
// and for any instance when the lease is expired -- and because the
// WHERE ... expires_at < UTC_TIMESTAMP(6) predicate is evaluated under the row
// lock the UPDATE takes, at most one expired-takeover UPDATE succeeds (a
// second concurrent UPDATE re-reads the now-renewed row, sees holder!=self and
// not expired, matches nothing, n==0). Step 3's INSERT IGNORE resolves the
// absent-row race to exactly one winner. Both verifying SELECTs use
// LOCK IN SHARE MODE (matching mysqlLocker.ClaimFire, Task 3; NOT the
// MySQL-8-only FOR SHARE, so the read also runs on MariaDB) so the verdict is
// robust even under an accidental transaction, on top of the documented
// autocommit precondition. The verifying reads distinguish a raced-loss
// (fail-safe -> not leader) from a demoted data error (ErrElectorAcquireFailed
// -> the Source skips the fire fail-safe and logs).
func (mysqlElector) AcquireOrRenew(ctx context.Context, q Querier, table, scope, holder string, ttl time.Duration) (bool, error) {
	qt, err := quoteTable(mysqlQuote, table)
	if err != nil {
		return false, err
	}
	micros := ttl.Microseconds()

	// Step 1: conditional renew/takeover of an existing row.
	res, err := q.ExecContext(ctx, fmt.Sprintf(
		`UPDATE %s SET holder = ?, expires_at = DATE_ADD(UTC_TIMESTAMP(6), INTERVAL ? MICROSECOND)
WHERE scope = ? AND (holder = ? OR expires_at < UTC_TIMESTAMP(6))`, qt),
		holder, micros, scope, holder)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	if n >= 1 {
		return true, nil // renewed or took over an expired/self row → leader
	}

	// Step 2: n==0 — either the row is held by another (not expired), or absent,
	// or it is ours but the UPDATE was a no-op (identical values, same microsecond).
	// Verify the current committed state with a LOCKING read (LOCK IN SHARE MODE,
	// not FOR UPDATE — MariaDB-compatible, mirrors mysqlLocker.ClaimFire, Task 3)
	// so the verdict is robust even if q is ever handed a transaction, not only
	// the autocommit *sql.DB the ElectorDialect precondition requires (Round-1
	// audit M-2 — belt-and-suspenders on top of the documented precondition).
	var curHolder string
	var expired bool
	err = q.QueryRowContext(ctx, fmt.Sprintf(
		`SELECT holder, (expires_at < UTC_TIMESTAMP(6)) FROM %s WHERE scope = ? LOCK IN SHARE MODE`, qt), scope).
		Scan(&curHolder, &expired)
	switch {
	case errors.Is(err, stdsql.ErrNoRows):
		// Step 3: absent → try to acquire. INSERT IGNORE avoids driver-specific
		// dup-key detection: 1 row ⇒ we acquired; 0 rows ⇒ a concurrent instance
		// inserted first (or a demoted data error — distinguished below).
		res, err := q.ExecContext(ctx, fmt.Sprintf(
			`INSERT IGNORE INTO %s (scope, holder, expires_at)
VALUES (?, ?, DATE_ADD(UTC_TIMESTAMP(6), INTERVAL ? MICROSECOND))`, qt),
			scope, holder, micros)
		if err != nil {
			return false, err
		}
		ins, err := res.RowsAffected()
		if err != nil {
			return false, err
		}
		if ins == 1 {
			return true, nil // we won the insert → leader
		}
		// ins==0: a concurrent insert won, OR INSERT IGNORE demoted a real error.
		// Re-check (locking read, same rationale as Step 2's verify): a row now
		// present with another holder ⇒ raced-loss (fail-safe, not leader); still
		// absent ⇒ a demoted data error ⇒ surface it.
		err = q.QueryRowContext(ctx, fmt.Sprintf(`SELECT holder FROM %s WHERE scope = ? LOCK IN SHARE MODE`, qt), scope).
			Scan(&curHolder)
		if errors.Is(err, stdsql.ErrNoRows) {
			return false, fmt.Errorf("%w: scope %q (INSERT IGNORE affected no row and none exists)",
				ErrElectorAcquireFailed, scope)
		}
		if err != nil {
			return false, err
		}
		return curHolder == holder, nil // extremely unlikely holder==self here; false otherwise
	case err != nil:
		return false, err
	default:
		// Row present. It is ours and valid ⇒ leader (the UPDATE was a no-op);
		// otherwise held by another and not expired ⇒ not leader.
		return curHolder == holder && !expired, nil
	}
}

// EnsureLeaseSchema implements ElectorDialect for MySQL/MariaDB: it issues
// MySQLElectorDDL as a single ExecContext call.
func (mysqlElector) EnsureLeaseSchema(ctx context.Context, q Querier, table string) error {
	ddl, err := MySQLElectorDDL(table)
	if err != nil {
		return err
	}
	_, err = q.ExecContext(ctx, ddl)
	return err
}

// MySQLElectorDDL returns the reference CREATE TABLE for the MySQL/MariaDB
// leader-lease table, for a migration tool. It validates table first.
func MySQLElectorDDL(table string) (string, error) {
	qt, err := quoteTable(mysqlQuote, table)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
  scope      VARCHAR(255) PRIMARY KEY,
  holder     VARCHAR(255) NOT NULL,
  expires_at DATETIME(6)  NOT NULL
)`, qt), nil
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

type sqliteElector struct{}

// SQLiteElector returns the SQLite ElectorDialect. Stateless; safe to share.
func SQLiteElector() ElectorDialect { return sqliteElector{} }

// AcquireOrRenew implements ElectorDialect for SQLite: identical shape to
// postgresElector.AcquireOrRenew, using SQLite quoting and the nowMicrosSQLite
// DB-clock expression; 'now' is constant within a single statement, so the
// VALUES expires_at and the WHERE clock agree. SQLite >= 3.35 supports
// RETURNING (modernc bundles a newer version).
func (sqliteElector) AcquireOrRenew(ctx context.Context, q Querier, table, scope, holder string, ttl time.Duration) (bool, error) {
	qt, err := quoteTable(sqliteQuote, table)
	if err != nil {
		return false, err
	}
	var gotHolder string
	err = q.QueryRowContext(ctx, fmt.Sprintf(`INSERT INTO %[1]s (scope, holder, expires_at)
VALUES (?, ?, %[2]s + ?)
ON CONFLICT (scope) DO UPDATE SET holder = excluded.holder, expires_at = excluded.expires_at
WHERE %[1]s.holder = ? OR %[1]s.expires_at < %[2]s
RETURNING holder`, qt, nowMicrosSQLite),
		scope, holder, ttl.Microseconds(), holder,
	).Scan(&gotHolder)
	if errors.Is(err, stdsql.ErrNoRows) {
		return false, nil // held by another, not expired → not leader
	}
	if err != nil {
		return false, err
	}
	return gotHolder == holder, nil // RETURNING guarantees this, but assert defensively
}

// EnsureLeaseSchema implements ElectorDialect for SQLite: it idempotently
// creates the lease table.
func (sqliteElector) EnsureLeaseSchema(ctx context.Context, q Querier, table string) error {
	qt, err := quoteTable(sqliteQuote, table)
	if err != nil {
		return err
	}
	_, err = q.ExecContext(ctx, sqliteCreateLeaseTable(qt))
	return err
}

// sqliteCreateLeaseTable builds the idempotent CREATE TABLE for the SQLite
// leader-lease schema. qt must already be quoted.
func sqliteCreateLeaseTable(qt string) string {
	return fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
  scope      TEXT    PRIMARY KEY,
  holder     TEXT    NOT NULL,
  expires_at INTEGER NOT NULL
)`, qt)
}

// SQLiteElectorDDL returns the reference CREATE TABLE for the SQLite
// leader-lease table, for a migration tool to run directly (msgin itself
// always issues this via EnsureLeaseSchema). It validates table first.
func SQLiteElectorDDL(table string) (string, error) {
	qt, err := quoteTable(sqliteQuote, table)
	if err != nil {
		return "", err
	}
	return sqliteCreateLeaseTable(qt) + ";", nil
}
