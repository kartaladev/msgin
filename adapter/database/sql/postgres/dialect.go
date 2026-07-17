package postgres

import (
	"context"
	stdsql "database/sql"
	"errors"
	"fmt"
	"time"

	msginsql "github.com/kartaladev/msgin/adapter/database/sql"
)

// postgresDialect is the built-in LeaseDialect for PostgreSQL and wire-compatible
// derivatives. It is stateless; a single value is shared by all callers.
type postgresDialect struct{}

// Compile-time assertions that the single stateless value satisfies the full
// SPI: the lease/claim source dialect, the segregated lock SPI (so
// WithStrategy(StrategyLockForUpdate) accepts it), and the segregated
// dedup-inbox SPI (so it may be passed as NewInboxDeduper's required dialect).
var (
	_ msginsql.LeaseDialect = postgresDialect{}
	_ msginsql.LockDialect  = postgresDialect{}
	_ msginsql.InboxDialect = postgresDialect{}
)

// LeaseDialect returns the built-in PostgreSQL LeaseDialect (lease/claim
// strategy). It owns the exact PostgreSQL SQL for every operation; pass it as
// the required dialect argument to NewPollingSource/NewOutboundAdapter. The
// single stateless value also satisfies msginsql.LockDialect (for
// WithStrategy(StrategyLockForUpdate)) and msginsql.InboxDialect.
func LeaseDialect() msginsql.LeaseDialect { return postgresDialect{} }

// InboxDialect returns the built-in PostgreSQL InboxDialect — the same
// stateless value as LeaseDialect(), narrowed to the dedup-inbox SPI (ADR
// 0010 D10). Pass it as the required dialect argument to NewInboxDeduper.
func InboxDialect() msginsql.InboxDialect { return postgresDialect{} }

func (postgresDialect) Claim(ctx context.Context, q msginsql.Querier, table string, limit int, lockedBy string, leaseTTL time.Duration) ([]msginsql.ClaimedRow, error) {
	qt, err := pgQuoteTable(table)
	if err != nil {
		return nil, err
	}
	// One-shot claim: lease the least-recently-visible claimable rows (unlocked
	// or lease expired), bumping the fence token and delivery count. The lease
	// cutoff and visibility both use the DB clock; leaseTTL is passed as a
	// microsecond interval. headers is cast to text so the framed JSON returns
	// as clean bytes across drivers (no jsonb binary-format prefix).
	query := fmt.Sprintf(`UPDATE %[1]s SET
  locked_by = $2, locked_at = now(),
  lease_epoch = lease_epoch + 1, delivery_count = delivery_count + 1
WHERE id IN (
  SELECT id FROM %[1]s
  WHERE visible_after <= now()
    AND (locked_at IS NULL OR locked_at <= now() - ($3 * interval '1 microsecond'))
  ORDER BY visible_after
  FOR UPDATE SKIP LOCKED
  LIMIT $1
)
RETURNING id, msg_id, headers::text, payload, delivery_count, lease_epoch`, qt)

	rows, err := q.QueryContext(ctx, query, limit, lockedBy, leaseTTL.Microseconds())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []msginsql.ClaimedRow
	for rows.Next() {
		var r msginsql.ClaimedRow
		if err := rows.Scan(&r.ID, &r.MsgID, &r.Headers, &r.Payload, &r.DeliveryCount, &r.LeaseEpoch); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (postgresDialect) Ack(ctx context.Context, q msginsql.Querier, table string, id int64, lockedBy string, epoch int64) (bool, error) {
	qt, err := pgQuoteTable(table)
	if err != nil {
		return false, err
	}
	res, err := q.ExecContext(ctx,
		fmt.Sprintf(`DELETE FROM %s WHERE id = $1 AND locked_by = $2 AND lease_epoch = $3`, qt),
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

func (postgresDialect) Nack(ctx context.Context, q msginsql.Querier, table string, id int64, lockedBy string, epoch int64, delay time.Duration) (bool, error) {
	qt, err := pgQuoteTable(table)
	if err != nil {
		return false, err
	}
	res, err := q.ExecContext(ctx,
		fmt.Sprintf(`UPDATE %s SET locked_by = NULL, locked_at = NULL,
  visible_after = now() + ($4 * interval '1 microsecond')
WHERE id = $1 AND locked_by = $2 AND lease_epoch = $3`, qt),
		id, lockedBy, epoch, delay.Microseconds(),
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

func (postgresDialect) Insert(ctx context.Context, q msginsql.Querier, table, msgID string, headers, payload []byte, delay time.Duration) error {
	qt, err := pgQuoteTable(table)
	if err != nil {
		return err
	}
	// headers is bound as a string so PostgreSQL applies the jsonb input
	// function (column context), portable across pgx and lib/pq; payload is
	// bound as []byte for the bytea column.
	_, err = q.ExecContext(ctx,
		fmt.Sprintf(`INSERT INTO %s (msg_id, headers, payload, visible_after)
VALUES ($1, $2, $3, now() + ($4 * interval '1 microsecond'))`, qt),
		msgID, string(headers), payload, delay.Microseconds(),
	)
	return err
}

func (d postgresDialect) EnsureSchema(ctx context.Context, q msginsql.Querier, table string) error {
	qt, err := pgQuoteTable(table)
	if err != nil {
		return err
	}
	// Execute the table and index statements separately: some drivers (pgx's
	// extended protocol) reject multiple statements in one Exec. Both are
	// IF NOT EXISTS, so EnsureSchema is idempotent.
	if _, err := q.ExecContext(ctx, postgresCreateTable(qt)); err != nil {
		return err
	}
	qidx := pgQuote(table + "_claim_idx")
	if _, err := q.ExecContext(ctx, postgresCreateIndex(qt, qidx)); err != nil {
		return err
	}
	return nil
}

// ClaimLock implements LockDialect for PostgreSQL (ADR 0010 D5): it begins a
// transaction on ctx, claims one visible/unlocked row FOR UPDATE SKIP LOCKED,
// bumps its delivery_count in that transaction, and returns the row with the
// OPEN transaction carried in LockedRow.Tx. There is NO lease-expiry disjunct —
// the open transaction is the exclusive lock (SKIP LOCKED makes a concurrent
// claimer skip a row this transaction holds), released only by commit or crash.
// It returns (nil, nil) when nothing is claimable, and rolls the transaction back
// on that path and on ANY error (no connection leak).
func (postgresDialect) ClaimLock(ctx context.Context, q msginsql.Querier, table, lockedBy string) (*msginsql.LockedRow, error) {
	qt, err := pgQuoteTable(table)
	if err != nil {
		return nil, err
	}
	tx, err := msginsql.BeginLockTx(ctx, q)
	if err != nil {
		return nil, err
	}

	// headers is cast to text so the framed JSON returns as clean bytes across
	// drivers (no jsonb binary-format prefix), matching Claim.
	selectSQL := fmt.Sprintf(`SELECT id, msg_id, headers::text, payload, delivery_count
FROM %[1]s
WHERE visible_after <= now() AND locked_at IS NULL
ORDER BY visible_after
FOR UPDATE SKIP LOCKED
LIMIT 1`, qt)

	var lr msginsql.LockedRow
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
		fmt.Sprintf(`UPDATE %s SET delivery_count = delivery_count + 1 WHERE id = $1`, qt), lr.ID); err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	lr.DeliveryCount++ // post-increment, mirroring the lease Claim's RETURNING
	lr.Tx = tx
	return &lr, nil
}

// AckLock settles lr by deleting its row on the carried transaction and
// committing (ADR 0010 D5). On any error it rolls back, releasing the connection.
func (postgresDialect) AckLock(ctx context.Context, lr *msginsql.LockedRow, table string) error {
	qt, err := pgQuoteTable(table)
	if err != nil {
		_ = lr.Tx.Rollback() // never leave the carried tx open, even on this near-impossible path
		return err
	}
	return msginsql.SettleLockTx(ctx, lr.Tx, fmt.Sprintf(`DELETE FROM %s WHERE id = $1`, qt), lr.ID)
}

// NackLock returns lr to the queue by clearing the lock and pushing
// visible_after out by delay, then ALWAYS commits (ADR 0010 D5): the commit
// persists the delivery_count++ made at claim and releases the FOR UPDATE lock.
// On an UPDATE/commit error it rolls back, releasing the connection.
func (postgresDialect) NackLock(ctx context.Context, lr *msginsql.LockedRow, table string, delay time.Duration) error {
	qt, err := pgQuoteTable(table)
	if err != nil {
		_ = lr.Tx.Rollback()
		return err
	}
	return msginsql.SettleLockTx(ctx, lr.Tx,
		fmt.Sprintf(`UPDATE %s SET locked_by = NULL, locked_at = NULL,
  visible_after = now() + ($2 * interval '1 microsecond')
WHERE id = $1`, qt),
		lr.ID, delay.Microseconds())
}

func (postgresDialect) SchemaExists(ctx context.Context, q msginsql.Querier, table string) (bool, error) {
	// table is a bound parameter here, but validate it anyway so the exported
	// SPI never runs on an unvalidated identifier.
	if err := msginsql.ValidateIdent(table); err != nil {
		return false, err
	}
	var one int
	err := q.QueryRowContext(ctx,
		`SELECT 1 FROM information_schema.tables WHERE table_name = $1 AND table_schema = current_schema()`,
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

// InsertInboxIfAbsent implements InboxDialect for PostgreSQL (ADR 0010 D10). It
// records msgID via INSERT ... ON CONFLICT (msg_id) DO NOTHING RETURNING, which
// distinguishes an insert (a row is RETURNed → already=false) from a duplicate
// (no row → sql.ErrNoRows → already=true) EXACTLY, never trusting rowsAffected.
// It runs on q — the caller's business transaction — so the dedup row commits
// atomically with the business writes.
func (postgresDialect) InsertInboxIfAbsent(ctx context.Context, q msginsql.Querier, table, msgID string) (bool, error) {
	qt, err := pgQuoteTable(table)
	if err != nil {
		return false, err
	}
	var returned string
	err = q.QueryRowContext(ctx,
		fmt.Sprintf(`INSERT INTO %s (msg_id, processed_at) VALUES ($1, now())
ON CONFLICT (msg_id) DO NOTHING
RETURNING msg_id`, qt),
		msgID,
	).Scan(&returned)
	if errors.Is(err, stdsql.ErrNoRows) {
		return true, nil // conflict: msgID already recorded (a genuine duplicate)
	}
	if err != nil {
		return false, err
	}
	return false, nil // inserted: first time this id is seen
}

// PurgeInbox implements InboxDialect for PostgreSQL: it deletes rows older than
// olderThan (DB clock) and returns the count removed. olderThan is passed as a
// microsecond interval, matching the rest of the dialect's DB-clock arithmetic.
func (postgresDialect) PurgeInbox(ctx context.Context, q msginsql.Querier, table string, olderThan time.Duration) (int64, error) {
	qt, err := pgQuoteTable(table)
	if err != nil {
		return 0, err
	}
	res, err := q.ExecContext(ctx,
		fmt.Sprintf(`DELETE FROM %s WHERE processed_at < now() - ($1 * interval '1 microsecond')`, qt),
		olderThan.Microseconds(),
	)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// EnsureInboxSchema implements InboxDialect for PostgreSQL: it idempotently
// creates the dedup table and its processed_at retention index. The two
// statements run separately (pgx's extended protocol rejects multi-statement
// Exec); both are IF NOT EXISTS, so EnsureInboxSchema is idempotent.
func (postgresDialect) EnsureInboxSchema(ctx context.Context, q msginsql.Querier, table string) error {
	qt, err := pgQuoteTable(table)
	if err != nil {
		return err
	}
	if _, err := q.ExecContext(ctx, postgresCreateInboxTable(qt)); err != nil {
		return err
	}
	qidx := pgQuote(table + "_processed_idx")
	if _, err := q.ExecContext(ctx, postgresCreateInboxIndex(qt, qidx)); err != nil {
		return err
	}
	return nil
}

// MsgIDUniqueIndexExists implements InboxDialect for PostgreSQL: it reports
// whether msg_id participates in a PRIMARY KEY or UNIQUE constraint on table, via
// a portable information_schema probe (no driver import, matching SchemaExists).
// table is a bound parameter, but validated anyway so the exported SPI never runs
// on an unvalidated identifier.
func (postgresDialect) MsgIDUniqueIndexExists(ctx context.Context, q msginsql.Querier, table string) (bool, error) {
	if err := msginsql.ValidateIdent(table); err != nil {
		return false, err
	}
	var one int
	err := q.QueryRowContext(ctx,
		`SELECT 1
FROM information_schema.table_constraints tc
JOIN information_schema.key_column_usage kcu
  ON tc.constraint_name = kcu.constraint_name
 AND tc.table_schema = kcu.table_schema
 AND tc.table_name = kcu.table_name
WHERE tc.constraint_type IN ('PRIMARY KEY', 'UNIQUE')
  AND kcu.column_name = 'msg_id'
  AND tc.table_name = $1
  AND tc.table_schema = current_schema()
LIMIT 1`,
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
