package sql

import (
	"context"
	stdsql "database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// mysqlDialect is the built-in Dialect for MySQL and wire-compatible
// derivatives (MariaDB). It is stateless; a single value is shared by all
// callers. It is behavior-identical to postgresDialect — same claim predicate,
// fence semantics, delivery_count/lease_epoch bumps, and visible_after delays —
// expressed in MySQL SQL (ADR 0010 D3/D4).
//
// # The two-step, atomic claim (correctness-critical)
//
// MySQL has no RETURNING, and its SELECT ... FOR UPDATE SKIP LOCKED row locks
// only hold for the life of a transaction, so Claim CANNOT be a one-shot
// statement like Postgres. It MUST run the SELECT (locking the claimable rows)
// and the follow-up UPDATE (bumping the lease) in ONE explicit transaction — if
// the two ran in separate auto-commit statements, a concurrent poller could
// claim the same row between them (a double-claim, the exact defect the fence
// exists to prevent). The claimed rows' delivery_count/lease_epoch are the
// SELECT's values + 1, computed in Go to mirror Postgres's RETURNING.
type mysqlDialect struct{}

// MySQLDialect returns the built-in MySQL Dialect (lease/claim strategy),
// behavior-identical to PostgresDialect() over MySQL SQL. Pass it to the adapter
// constructors via WithDialect for the guaranteed-correct path; a MySQL/MariaDB
// driver also auto-detects it.
func MySQLDialect() Dialect { return mysqlDialect{} }

// mysqlQuote back-quotes a MySQL identifier. The name must already be validated
// (validateIdent admits no backtick), so wrapping is safe; doubling any embedded
// backtick is defense-in-depth in case this is ever reached without prior
// validation (mirrors pgQuote).
func mysqlQuote(name string) string {
	return "`" + strings.ReplaceAll(name, "`", "``") + "`"
}

// mysqlQuoteTable validates then quotes a table identifier for interpolation.
func mysqlQuoteTable(table string) (string, error) {
	if err := validateIdent(table); err != nil {
		return "", err
	}
	return mysqlQuote(table), nil
}

// txBeginner is the capability a Querier must have for the MySQL two-step claim:
// the ability to open a transaction. *sql.DB satisfies it (its BeginTx); *sql.Tx
// does not (a tx cannot nest one), which is exactly how Claim tells the pool
// apart from an already-open transaction below.
type txBeginner interface {
	BeginTx(ctx context.Context, opts *stdsql.TxOptions) (*stdsql.Tx, error)
}

// Claim leases up to limit claimable rows for lockedBy, treating any lease older
// than leaseTTL as expired. Because MySQL needs the SELECT + UPDATE to be atomic
// (see mysqlDialect), it resolves a transaction from q:
//
//   - q is the pool (*sql.DB, the txBeginner path the Source always takes): begin
//     a tx, run the two-step, commit — rolling back on any error before returning
//     (nil, err), upholding Poll's no-partial-result / owns-cleanup contract.
//   - q is already a *sql.Tx (defensive/future — e.g. a caller driving Claim
//     inside their own tx): run the two-step directly on it, leaving commit to the
//     caller who owns the tx.
//   - q is neither: a clear error (Claim cannot be atomic without a transaction).
func (mysqlDialect) Claim(ctx context.Context, q Querier, table string, limit int, lockedBy string, leaseTTL time.Duration) ([]ClaimedRow, error) {
	qt, err := mysqlQuoteTable(table)
	if err != nil {
		return nil, err
	}

	if b, ok := q.(txBeginner); ok {
		tx, err := b.BeginTx(ctx, nil)
		if err != nil {
			return nil, err
		}
		rows, err := mysqlClaimInTx(ctx, tx, qt, limit, lockedBy, leaseTTL)
		if err != nil {
			_ = tx.Rollback() // own cleanup on the error path (Poll contract)
			return nil, err
		}
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		return rows, nil
	}
	if tx, ok := q.(*stdsql.Tx); ok {
		// Already inside a transaction: run the two-step on it directly; the caller
		// owns commit/rollback.
		return mysqlClaimInTx(ctx, tx, qt, limit, lockedBy, leaseTTL)
	}
	return nil, fmt.Errorf(
		"msgin/sql: mysql Claim requires a *sql.DB or *sql.Tx Querier (its two-step claim must be atomic), got %T", q)
}

// mysqlClaimInTx runs the atomic two-step claim on q (a transaction): SELECT the
// claimable rows FOR UPDATE SKIP LOCKED, then UPDATE them to bump the lease. The
// SELECT's rows are fully read and closed BEFORE the UPDATE (a tx holds a single
// connection, so the result set must be drained first). Returned rows carry the
// post-increment delivery_count/lease_epoch computed in Go, matching Postgres.
func mysqlClaimInTx(ctx context.Context, q Querier, qt string, limit int, lockedBy string, leaseTTL time.Duration) ([]ClaimedRow, error) {
	// Claim predicate identical to Postgres: visible now, and unlocked or lease
	// expired (the inlined reaper). leaseTTL is passed as a microsecond interval.
	// Note the clause order: MySQL requires LIMIT BEFORE the locking clause
	// (FOR UPDATE SKIP LOCKED), the opposite of Postgres which allows FOR UPDATE
	// before LIMIT.
	selectSQL := fmt.Sprintf(`SELECT id, msg_id, headers, payload, delivery_count, lease_epoch
FROM %[1]s
WHERE visible_after <= UTC_TIMESTAMP(6)
  AND (locked_at IS NULL OR locked_at <= DATE_SUB(UTC_TIMESTAMP(6), INTERVAL ? MICROSECOND))
ORDER BY visible_after
LIMIT ?
FOR UPDATE SKIP LOCKED`, qt)

	out, ids, err := mysqlScanClaim(ctx, q, selectSQL, leaseTTL.Microseconds(), limit)
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		// Empty select → nothing to update (hot-path branch).
		return nil, nil
	}

	updateSQL := fmt.Sprintf(`UPDATE %[1]s SET
  locked_by = ?, locked_at = UTC_TIMESTAMP(6),
  lease_epoch = lease_epoch + 1, delivery_count = delivery_count + 1
WHERE id IN (%s)`, qt, placeholders(len(ids)))

	args := make([]any, 0, len(ids)+1)
	args = append(args, lockedBy)
	for _, id := range ids {
		args = append(args, id)
	}
	if _, err := q.ExecContext(ctx, updateSQL, args...); err != nil {
		return nil, err
	}
	return out, nil
}

// mysqlScanClaim runs the claim SELECT and returns the rows (with
// delivery_count/lease_epoch already incremented in Go) plus their ids for the
// follow-up UPDATE. It closes the result set before returning, so the caller may
// immediately run the UPDATE on the same single-connection transaction.
func mysqlScanClaim(ctx context.Context, q Querier, query string, args ...any) ([]ClaimedRow, []int64, error) {
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	var (
		out []ClaimedRow
		ids []int64
	)
	for rows.Next() {
		var r ClaimedRow
		if err := rows.Scan(&r.ID, &r.MsgID, &r.Headers, &r.Payload, &r.DeliveryCount, &r.LeaseEpoch); err != nil {
			return nil, nil, err
		}
		// Post-increment values computed in Go (the UPDATE below bumps the DB by
		// the same +1), mirroring Postgres's RETURNING of the updated row.
		r.DeliveryCount++
		r.LeaseEpoch++
		out = append(out, r)
		ids = append(ids, r.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	return out, ids, nil
}

func (mysqlDialect) Ack(ctx context.Context, q Querier, table string, id int64, lockedBy string, epoch int64) (bool, error) {
	qt, err := mysqlQuoteTable(table)
	if err != nil {
		return false, err
	}
	res, err := q.ExecContext(ctx,
		fmt.Sprintf("DELETE FROM %s WHERE id = ? AND locked_by = ? AND lease_epoch = ?", qt),
		id, lockedBy, epoch,
	)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

func (mysqlDialect) Nack(ctx context.Context, q Querier, table string, id int64, lockedBy string, epoch int64, delay time.Duration) (bool, error) {
	qt, err := mysqlQuoteTable(table)
	if err != nil {
		return false, err
	}
	res, err := q.ExecContext(ctx,
		fmt.Sprintf(`UPDATE %s SET locked_by = NULL, locked_at = NULL,
  visible_after = DATE_ADD(UTC_TIMESTAMP(6), INTERVAL ? MICROSECOND)
WHERE id = ? AND locked_by = ? AND lease_epoch = ?`, qt),
		delay.Microseconds(), id, lockedBy, epoch,
	)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

func (mysqlDialect) Insert(ctx context.Context, q Querier, table, msgID string, headers, payload []byte, delay time.Duration) error {
	qt, err := mysqlQuoteTable(table)
	if err != nil {
		return err
	}
	// headers is bound as a string so MySQL validates it into the JSON column;
	// payload is bound as []byte for the LONGBLOB column. visible_after is the DB
	// clock (UTC) plus the delay, so there is no app↔DB skew (delay 0 = now).
	_, err = q.ExecContext(ctx,
		fmt.Sprintf(`INSERT INTO %s (msg_id, headers, payload, visible_after)
VALUES (?, ?, ?, DATE_ADD(UTC_TIMESTAMP(6), INTERVAL ? MICROSECOND))`, qt),
		msgID, string(headers), payload, delay.Microseconds(),
	)
	return err
}

func (mysqlDialect) EnsureSchema(ctx context.Context, q Querier, table string) error {
	qt, err := mysqlQuoteTable(table)
	if err != nil {
		return err
	}
	// The claim index is declared INLINE in the CREATE TABLE (MySQL has no
	// CREATE INDEX IF NOT EXISTS), so the whole schema is created idempotently in
	// one statement.
	qidx := mysqlQuote(table + "_claim_idx")
	if _, err := q.ExecContext(ctx, mysqlCreateTable(qt, qidx)); err != nil {
		return err
	}
	return nil
}

// ClaimLock implements LockDialect for MySQL (ADR 0010 D5), the MySQL peer of
// postgresDialect.ClaimLock. Because MySQL's FOR UPDATE SKIP LOCKED row lock only
// holds for the life of a transaction, the claim transaction is the SAME one
// carried to settle: it begins a transaction on ctx, SELECTs one claimable row
// (LIMIT before FOR UPDATE, the MySQL clause order) and scans it, bumps
// delivery_count in that transaction, and returns the row with the OPEN
// transaction carried in LockedRow.Tx. There is no lease-expiry disjunct — the
// open transaction is the exclusive lock. It returns (nil, nil) when nothing is
// claimable, rolling the transaction back on that path and on ANY error (no
// connection leak).
func (mysqlDialect) ClaimLock(ctx context.Context, q Querier, table, lockedBy string) (*LockedRow, error) {
	qt, err := mysqlQuoteTable(table)
	if err != nil {
		return nil, err
	}
	tx, err := beginLockTx(ctx, q)
	if err != nil {
		return nil, err
	}

	selectSQL := fmt.Sprintf(`SELECT id, msg_id, headers, payload, delivery_count
FROM %[1]s
WHERE visible_after <= UTC_TIMESTAMP(6) AND locked_at IS NULL
ORDER BY visible_after
LIMIT 1
FOR UPDATE SKIP LOCKED`, qt)

	var lr LockedRow
	err = tx.QueryRowContext(ctx, selectSQL).Scan(&lr.ID, &lr.MsgID, &lr.Headers, &lr.Payload, &lr.DeliveryCount)
	if errors.Is(err, stdsql.ErrNoRows) {
		_ = tx.Rollback() // nothing claimable: release the connection
		return nil, nil
	}
	if err != nil {
		_ = tx.Rollback()
		return nil, err
	}

	if _, err := tx.ExecContext(ctx,
		fmt.Sprintf("UPDATE %s SET delivery_count = delivery_count + 1 WHERE id = ?", qt), lr.ID); err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	lr.DeliveryCount++ // post-increment, mirroring Postgres
	lr.Tx = tx
	return &lr, nil
}

// AckLock settles lr by deleting its row on the carried transaction and
// committing (ADR 0010 D5). On any error it rolls back, releasing the connection.
func (mysqlDialect) AckLock(ctx context.Context, lr *LockedRow, table string) error {
	qt, err := mysqlQuoteTable(table)
	if err != nil {
		_ = lr.Tx.Rollback()
		return err
	}
	return settleLockTx(ctx, lr.Tx, fmt.Sprintf("DELETE FROM %s WHERE id = ?", qt), lr.ID)
}

// NackLock returns lr to the queue by clearing the lock and pushing
// visible_after out by delay, then ALWAYS commits (ADR 0010 D5). On an
// UPDATE/commit error it rolls back, releasing the connection.
func (mysqlDialect) NackLock(ctx context.Context, lr *LockedRow, table string, delay time.Duration) error {
	qt, err := mysqlQuoteTable(table)
	if err != nil {
		_ = lr.Tx.Rollback()
		return err
	}
	return settleLockTx(ctx, lr.Tx,
		fmt.Sprintf(`UPDATE %s SET locked_by = NULL, locked_at = NULL,
  visible_after = DATE_ADD(UTC_TIMESTAMP(6), INTERVAL ? MICROSECOND)
WHERE id = ?`, qt),
		delay.Microseconds(), lr.ID)
}

func (mysqlDialect) SchemaExists(ctx context.Context, q Querier, table string) (bool, error) {
	// table is a bound parameter here, but validate it anyway so the exported SPI
	// never runs on an unvalidated identifier.
	if err := validateIdent(table); err != nil {
		return false, err
	}
	var one int
	err := q.QueryRowContext(ctx,
		"SELECT 1 FROM information_schema.tables WHERE table_name = ? AND table_schema = DATABASE()",
		table,
	).Scan(&one)
	if errors.Is(err, stdsql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// placeholders returns "?, ?, ..." with n placeholders, for a MySQL IN (...)
// clause built from a known-length id slice.
func placeholders(n int) string {
	if n <= 0 {
		return ""
	}
	return strings.Repeat("?, ", n-1) + "?"
}
