package sql

import (
	"context"
	stdsql "database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// postgresDialect is the built-in Dialect for PostgreSQL and wire-compatible
// derivatives. It is stateless; a single value is shared by all callers.
type postgresDialect struct{}

// PostgresDialect returns the built-in PostgreSQL Dialect (lease/claim
// strategy). It owns the exact PostgreSQL SQL for every operation; pass it to
// the adapter constructors via WithDialect for the guaranteed-correct path.
func PostgresDialect() Dialect { return postgresDialect{} }

// pgQuote double-quotes a PostgreSQL identifier. The name must already be
// validated (validateIdent admits no double-quote), so wrapping is safe;
// doubling any embedded `"` is defense-in-depth (belt-and-suspenders) in case
// this is ever reached without prior validation.
func pgQuote(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

// pgQuoteTable validates then quotes a table identifier for interpolation.
func pgQuoteTable(table string) (string, error) {
	if err := validateIdent(table); err != nil {
		return "", err
	}
	return pgQuote(table), nil
}

func (postgresDialect) Claim(ctx context.Context, q Querier, table string, limit int, lockedBy string, leaseTTL time.Duration) ([]ClaimedRow, error) {
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

	var out []ClaimedRow
	for rows.Next() {
		var r ClaimedRow
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

func (postgresDialect) Ack(ctx context.Context, q Querier, table string, id int64, lockedBy string, epoch int64) (bool, error) {
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

func (postgresDialect) Nack(ctx context.Context, q Querier, table string, id int64, lockedBy string, epoch int64, delay time.Duration) (bool, error) {
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

func (postgresDialect) Insert(ctx context.Context, q Querier, table, msgID string, headers, payload []byte, delay time.Duration) error {
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

func (d postgresDialect) EnsureSchema(ctx context.Context, q Querier, table string) error {
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

func (postgresDialect) SchemaExists(ctx context.Context, q Querier, table string) (bool, error) {
	// table is a bound parameter here, but validate it anyway so the exported
	// SPI never runs on an unvalidated identifier.
	if err := validateIdent(table); err != nil {
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
