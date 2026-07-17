package sqlite

import (
	"context"
	stdsql "database/sql"
	"errors"
	"fmt"
	"time"

	msginsql "github.com/kartaladev/msgin/adapter/database/sql"
)

// sqliteDialect is the built-in LeaseDialect + InboxDialect for SQLite. It is
// stateless; a single value is shared by all callers. It implements the
// lease/claim strategy only: SQLite serializes writers and has no FOR UPDATE, so
// it deliberately does NOT implement msginsql.LockDialect (ADR 0012 D1) —
// WithStrategy(StrategyLockForUpdate) with it returns ErrLockStrategyUnsupported.
// All persisted timestamps use the DB clock (unixepoch), never the app clock.
type sqliteDialect struct{}

// Compile-time assertions. Note there is intentionally NO
// `_ msginsql.LockDialect = sqliteDialect{}` — sqlite is lease-only.
var (
	_ msginsql.LeaseDialect = sqliteDialect{}
	_ msginsql.InboxDialect = sqliteDialect{}
)

// LeaseDialect returns the built-in SQLite LeaseDialect (lease/claim strategy).
// Pass it as the required dialect argument to NewPollingSource/
// NewOutboundAdapter. It does NOT implement LockDialect (sqlite is lease-only).
func LeaseDialect() msginsql.LeaseDialect { return sqliteDialect{} }

// InboxDialect returns the built-in SQLite InboxDialect — the same stateless
// value as LeaseDialect(), narrowed to the dedup-inbox SPI. Pass it to
// NewInboxDeduper.
func InboxDialect() msginsql.InboxDialect { return sqliteDialect{} }

// Claim leases up to limit claimable rows for lockedBy, treating any lease older
// than leaseTTL as expired. It is a ONE-SHOT atomic UPDATE ... RETURNING: SQLite
// serializes writers (the statement holds the DB write lock for its duration),
// so no SKIP LOCKED or explicit two-step transaction is needed — mirroring
// Postgres's one-shot claim. RETURNING yields the post-update delivery_count/
// lease_epoch directly (ADR 0012 D2).
func (sqliteDialect) Claim(ctx context.Context, q msginsql.Querier, table string, limit int, lockedBy string, leaseTTL time.Duration) ([]msginsql.ClaimedRow, error) {
	qt, err := sqliteQuoteTable(table)
	if err != nil {
		return nil, err
	}
	query := fmt.Sprintf(`UPDATE %[1]s SET
  locked_by = ?, locked_at = %[2]s,
  lease_epoch = lease_epoch + 1, delivery_count = delivery_count + 1
WHERE id IN (
  SELECT id FROM %[1]s
  WHERE visible_after <= %[2]s
    AND (locked_at IS NULL OR locked_at <= %[2]s - ?)
  ORDER BY visible_after
  LIMIT ?
)
RETURNING id, msg_id, headers, payload, delivery_count, lease_epoch`, qt, nowMicros)

	rows, err := q.QueryContext(ctx, query, lockedBy, leaseTTL.Microseconds(), limit)
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

// Ack deletes a fenced row (id + locked_by + lease_epoch); applied=false on a
// fence miss (the lease expired and another worker re-claimed it).
func (sqliteDialect) Ack(ctx context.Context, q msginsql.Querier, table string, id int64, lockedBy string, epoch int64) (bool, error) {
	qt, err := sqliteQuoteTable(table)
	if err != nil {
		return false, err
	}
	res, err := q.ExecContext(ctx,
		fmt.Sprintf(`DELETE FROM %s WHERE id = ? AND locked_by = ? AND lease_epoch = ?`, qt),
		id, lockedBy, epoch)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// Nack returns a fenced row to the queue: clear the lock and push visible_after
// out by delay (µs). applied=false on a fence miss.
func (sqliteDialect) Nack(ctx context.Context, q msginsql.Querier, table string, id int64, lockedBy string, epoch int64, delay time.Duration) (bool, error) {
	qt, err := sqliteQuoteTable(table)
	if err != nil {
		return false, err
	}
	res, err := q.ExecContext(ctx,
		fmt.Sprintf(`UPDATE %s SET locked_by = NULL, locked_at = NULL,
  visible_after = %s + ?
WHERE id = ? AND locked_by = ? AND lease_epoch = ?`, qt, nowMicros),
		delay.Microseconds(), id, lockedBy, epoch)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// Insert writes a new message; visible_after = now + delay (µs). headers is the
// framed JSON bound as TEXT; payload as a BLOB.
func (sqliteDialect) Insert(ctx context.Context, q msginsql.Querier, table, msgID string, headers, payload []byte, delay time.Duration) error {
	qt, err := sqliteQuoteTable(table)
	if err != nil {
		return err
	}
	_, err = q.ExecContext(ctx,
		fmt.Sprintf(`INSERT INTO %s (msg_id, headers, payload, visible_after)
VALUES (?, ?, ?, %s + ?)`, qt, nowMicros),
		msgID, string(headers), payload, delay.Microseconds())
	return err
}

// EnsureSchema idempotently creates the table then its partial claim index
// (separate IF NOT EXISTS statements).
func (sqliteDialect) EnsureSchema(ctx context.Context, q msginsql.Querier, table string) error {
	qt, err := sqliteQuoteTable(table)
	if err != nil {
		return err
	}
	if _, err := q.ExecContext(ctx, sqliteCreateTable(qt)); err != nil {
		return err
	}
	qidx := sqliteQuote(table + "_claim_idx")
	if _, err := q.ExecContext(ctx, sqliteCreateIndex(qt, qidx)); err != nil {
		return err
	}
	return nil
}

// SchemaExists probes sqlite_master (SQLite has no information_schema). table is
// a bound parameter, but validated anyway so the exported SPI never runs on an
// unvalidated identifier.
func (sqliteDialect) SchemaExists(ctx context.Context, q msginsql.Querier, table string) (bool, error) {
	if err := msginsql.ValidateIdent(table); err != nil {
		return false, err
	}
	var one int
	err := q.QueryRowContext(ctx,
		`SELECT 1 FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&one)
	if errors.Is(err, stdsql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// InsertInboxIfAbsent records msgID via INSERT ... ON CONFLICT DO NOTHING
// RETURNING — EXACT like Postgres (a returned row → inserted; ErrNoRows →
// genuine duplicate). Runs on q (the caller's business tx). SQLite has no
// INSERT-IGNORE demotion problem, so there is no ErrInboxInsertFailed path.
func (sqliteDialect) InsertInboxIfAbsent(ctx context.Context, q msginsql.Querier, table, msgID string) (bool, error) {
	qt, err := sqliteQuoteTable(table)
	if err != nil {
		return false, err
	}
	var returned string
	err = q.QueryRowContext(ctx,
		fmt.Sprintf(`INSERT INTO %s (msg_id, processed_at) VALUES (?, %s)
ON CONFLICT (msg_id) DO NOTHING
RETURNING msg_id`, qt, nowMicros),
		msgID).Scan(&returned)
	if errors.Is(err, stdsql.ErrNoRows) {
		return true, nil // conflict: already recorded (a genuine duplicate)
	}
	if err != nil {
		return false, err
	}
	return false, nil // inserted: first sighting
}

// PurgeInbox deletes dedup rows older than olderThan (DB clock, µs).
func (sqliteDialect) PurgeInbox(ctx context.Context, q msginsql.Querier, table string, olderThan time.Duration) (int64, error) {
	qt, err := sqliteQuoteTable(table)
	if err != nil {
		return 0, err
	}
	res, err := q.ExecContext(ctx,
		fmt.Sprintf(`DELETE FROM %s WHERE processed_at < %s - ?`, qt, nowMicros),
		olderThan.Microseconds())
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// EnsureInboxSchema idempotently creates the dedup table then its processed_at
// retention index (separate IF NOT EXISTS statements).
func (sqliteDialect) EnsureInboxSchema(ctx context.Context, q msginsql.Querier, table string) error {
	qt, err := sqliteQuoteTable(table)
	if err != nil {
		return err
	}
	if _, err := q.ExecContext(ctx, sqliteCreateInboxTable(qt)); err != nil {
		return err
	}
	qidx := sqliteQuote(table + "_processed_idx")
	if _, err := q.ExecContext(ctx, sqliteCreateInboxIndex(qt, qidx)); err != nil {
		return err
	}
	return nil
}

// MsgIDUniqueIndexExists reports whether msg_id participates in a unique index
// (a TEXT PRIMARY KEY or an explicit UNIQUE both create a "unique"=1 index that
// appears in pragma_index_list). table is validated even though it is bound.
func (sqliteDialect) MsgIDUniqueIndexExists(ctx context.Context, q msginsql.Querier, table string) (bool, error) {
	if err := msginsql.ValidateIdent(table); err != nil {
		return false, err
	}
	var one int
	err := q.QueryRowContext(ctx,
		`SELECT 1
FROM pragma_index_list(?) il
JOIN pragma_index_info(il.name) ii
WHERE il."unique" = 1 AND ii.name = 'msg_id'
LIMIT 1`, table).Scan(&one)
	if errors.Is(err, stdsql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}
