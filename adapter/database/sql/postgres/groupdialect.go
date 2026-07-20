package postgres

import (
	"context"
	stdsql "database/sql"
	"errors"
	"fmt"
	"time"

	msginsql "github.com/kartaladev/msgin/adapter/database/sql"
)

// pgNowMicros is the DB-clock expression: the current transaction time as epoch
// microseconds (BIGINT). EXTRACT(EPOCH FROM now()) is seconds since the Unix
// epoch in UTC (timezone-independent), *1000000 expresses it in microseconds so
// every lease/expiry comparison is pure integer arithmetic on the DB clock — no
// app<->DB skew and no per-engine INTERVAL syntax (ADR 0021 §3/§4). created_at
// and locked_at are persisted in this same unit, so a Go-side CreatedAt is
// time.UnixMicro of the stored value.
const pgNowMicros = `CAST(EXTRACT(EPOCH FROM now()) * 1000000 AS BIGINT)`

// postgresGroupDialect is the built-in msginsql.GroupDialect for PostgreSQL and
// wire-compatible derivatives. It is stateless; a single value is shared by all
// callers. It owns the exact group-aggregation SQL for the two-table schema
// (group-lease table + append-only member table), deriving the member table
// name as table+"_member" (ADR 0021 §2).
type postgresGroupDialect struct{}

var _ msginsql.GroupDialect = postgresGroupDialect{}

// GroupDialect returns the built-in PostgreSQL GroupDialect. Pass it as the
// required dialect argument to msginsql.NewGroupStore. It owns the exact
// PostgreSQL group-aggregation SQL (group-row FOR UPDATE locking, epoch-fenced
// claim with RETURNING, claimed-set re-absorption) for the durable, multi-
// process Aggregator store (ADR 0021).
func GroupDialect() msginsql.GroupDialect { return postgresGroupDialect{} }

// txBeginner is the capability a Querier must have to BEGIN a new transaction:
// *sql.DB satisfies it (its BeginTx); *sql.Tx does not. It is a module-local
// declaration (Go interfaces are structural) used only by pgRunInTx below to
// tell the pool (begin a tx) apart from an already-open *sql.Tx (run on it
// directly, caller owns commit).
type txBeginner interface {
	BeginTx(ctx context.Context, opts *stdsql.TxOptions) (*stdsql.Tx, error)
}

// pgRunInTx runs fn inside a transaction resolved from q: the pool (*sql.DB) is
// wrapped in a fresh tx (committed on success, rolled back on any error), while
// an already-open *sql.Tx is run on directly (the caller owns commit). Every
// multi-statement group operation runs through this so its statements are
// atomic (ADR 0021 §4).
func pgRunInTx(ctx context.Context, q msginsql.Querier, fn func(tx msginsql.Querier) error) error {
	if b, ok := q.(txBeginner); ok {
		tx, err := b.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if err := fn(tx); err != nil {
			_ = tx.Rollback()
			return err
		}
		return tx.Commit()
	}
	if tx, ok := q.(*stdsql.Tx); ok {
		return fn(tx)
	}
	return fmt.Errorf("msgin/sql/postgres: group ops require a *sql.DB or *sql.Tx Querier, got %T", q)
}

// groupTables validates table once and returns the quoted group-lease table and
// its derived member table (table+"_member"). ValidateIdent(table) is the sole
// injection guard; the derived member name appends only safe characters.
func groupTables(table string) (groupTable, memberTable string, err error) {
	if err := msginsql.ValidateIdent(table); err != nil {
		return "", "", err
	}
	return pgQuote(table), pgQuote(table + "_member"), nil
}

func (postgresGroupDialect) AddMember(ctx context.Context, q msginsql.Querier, table, groupKey, msgID string, seq int64, headers, payload []byte) (msginsql.GroupRows, error) {
	if msgID == "" {
		return msginsql.GroupRows{}, msginsql.ErrMissingMsgID
	}
	gt, mt, err := groupTables(table)
	if err != nil {
		return msginsql.GroupRows{}, err
	}
	out := msginsql.GroupRows{GroupKey: groupKey}
	err = pgRunInTx(ctx, q, func(tx msginsql.Querier) error {
		// Upsert-AND-LOCK the group row atomically, reading created_at in the same
		// statement. ON CONFLICT DO UPDATE (a no-op re-set of group_key to its
		// existing value) LOCKS the conflicting row and RETURNING yields its
		// created_at — unlike DO NOTHING, which on a conflict neither locks the
		// row nor returns one, leaving a window for a concurrent SettleGroup to
		// DELETE the last group row between a separate upsert and a FOR UPDATE
		// (which would then see no row). This single statement serializes same-key
		// adds (H1) and closes that delete race. created_at is set once: the no-op
		// update never touches it, so it keeps the first-arrival value.
		var createdMicros int64
		if err := tx.QueryRowContext(ctx,
			fmt.Sprintf(`INSERT INTO %s (group_key, created_at, epoch) VALUES ($1, %s, 0)
ON CONFLICT (group_key) DO UPDATE SET group_key = EXCLUDED.group_key
RETURNING created_at`, gt, pgNowMicros),
			groupKey).Scan(&createdMicros); err != nil {
			return err
		}
		out.CreatedAt = time.UnixMicro(createdMicros)
		// Idempotent member upsert by (group_key, msg_id): a redelivered member
		// is a no-op. claimed_epoch NULL = live.
		if _, err := tx.ExecContext(ctx,
			fmt.Sprintf(`INSERT INTO %s (group_key, msg_id, seq, headers, payload, claimed_epoch)
VALUES ($1, $2, $3, $4, $5, NULL) ON CONFLICT (group_key, msg_id) DO NOTHING`, mt),
			groupKey, msgID, seq, headers, payload); err != nil {
			return err
		}
		members, err := pgSelectMembers(ctx, tx, mt, groupKey, "claimed_epoch IS NULL")
		if err != nil {
			return err
		}
		out.Members = members
		return nil
	})
	if err != nil {
		return msginsql.GroupRows{}, err
	}
	return out, nil
}

func (postgresGroupDialect) ClaimGroup(ctx context.Context, q msginsql.Querier, table, groupKey, lockedBy string, leaseTTL time.Duration) (*msginsql.ClaimedGroup, error) {
	gt, mt, err := groupTables(table)
	if err != nil {
		return nil, err
	}
	var result *msginsql.ClaimedGroup
	err = pgRunInTx(ctx, q, func(tx msginsql.Querier) error {
		// Fenced lease: bump epoch + stamp lease iff unleased or the lease aged
		// past leaseTTL. RETURNING gives the bumped epoch + created_at atomically.
		var newEpoch, createdMicros int64
		err := tx.QueryRowContext(ctx,
			fmt.Sprintf(`UPDATE %s SET epoch = epoch + 1, locked_by = $2, locked_at = %s
WHERE group_key = $1 AND (locked_by IS NULL OR locked_at <= %s - $3)
RETURNING epoch, created_at`, gt, pgNowMicros, pgNowMicros),
			groupKey, lockedBy, leaseTTL.Microseconds()).Scan(&newEpoch, &createdMicros)
		if errors.Is(err, stdsql.ErrNoRows) {
			return nil // absent or actively leased: (nil, nil)
		}
		if err != nil {
			return err
		}
		// Re-absorb a dead claim's members (H2): stamp the new epoch on every
		// member the lease fence proves is no longer actively claimed.
		if _, err := tx.ExecContext(ctx,
			fmt.Sprintf(`UPDATE %s SET claimed_epoch = $2
WHERE group_key = $1 AND (claimed_epoch IS NULL OR claimed_epoch < $2)`, mt),
			groupKey, newEpoch); err != nil {
			return err
		}
		members, err := pgSelectMembers(ctx, tx, mt, groupKey, fmt.Sprintf("claimed_epoch = %d", newEpoch))
		if err != nil {
			return err
		}
		result = &msginsql.ClaimedGroup{
			GroupRows: msginsql.GroupRows{GroupKey: groupKey, CreatedAt: time.UnixMicro(createdMicros), Members: members},
			Epoch:     newEpoch,
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (postgresGroupDialect) SettleGroup(ctx context.Context, q msginsql.Querier, table, groupKey, lockedBy string, epoch int64) (bool, error) {
	gt, mt, err := groupTables(table)
	if err != nil {
		return false, err
	}
	var applied bool
	err = pgRunInTx(ctx, q, func(tx msginsql.Querier) error {
		if locked, err := pgLockGroup(ctx, tx, gt, groupKey); err != nil || !locked {
			return err // group gone: applied stays false
		}
		// Delete only the claimed set; a member added during the lease
		// (claimed_epoch NULL) survives.
		if _, err := tx.ExecContext(ctx,
			fmt.Sprintf(`DELETE FROM %s WHERE group_key = $1 AND claimed_epoch = $2`, mt),
			groupKey, epoch); err != nil {
			return err
		}
		remaining, err := pgCountMembers(ctx, tx, mt, groupKey)
		if err != nil {
			return err
		}
		var res stdsql.Result
		if remaining > 0 {
			// Residual: clear the lease AND reset created_at so it is a fresh
			// group for expiry (M2). Fenced on (locked_by, epoch).
			res, err = tx.ExecContext(ctx,
				fmt.Sprintf(`UPDATE %s SET locked_by = NULL, locked_at = NULL, created_at = %s
WHERE group_key = $1 AND locked_by = $2 AND epoch = $3`, gt, pgNowMicros),
				groupKey, lockedBy, epoch)
		} else {
			res, err = tx.ExecContext(ctx,
				fmt.Sprintf(`DELETE FROM %s WHERE group_key = $1 AND locked_by = $2 AND epoch = $3`, gt),
				groupKey, lockedBy, epoch)
		}
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return err
		}
		applied = n > 0
		return nil
	})
	if err != nil {
		return false, err
	}
	return applied, nil
}

func (postgresGroupDialect) AbandonGroup(ctx context.Context, q msginsql.Querier, table, groupKey, lockedBy string, epoch int64) (bool, error) {
	gt, mt, err := groupTables(table)
	if err != nil {
		return false, err
	}
	var applied bool
	err = pgRunInTx(ctx, q, func(tx msginsql.Querier) error {
		if locked, err := pgLockGroup(ctx, tx, gt, groupKey); err != nil || !locked {
			return err
		}
		// Return the claimed members to live; the epoch stays bumped so a stale
		// holder's later settle/abandon still fences as a no-op.
		if _, err := tx.ExecContext(ctx,
			fmt.Sprintf(`UPDATE %s SET claimed_epoch = NULL WHERE group_key = $1 AND claimed_epoch = $2`, mt),
			groupKey, epoch); err != nil {
			return err
		}
		res, err := tx.ExecContext(ctx,
			fmt.Sprintf(`UPDATE %s SET locked_by = NULL, locked_at = NULL
WHERE group_key = $1 AND locked_by = $2 AND epoch = $3`, gt),
			groupKey, lockedBy, epoch)
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return err
		}
		applied = n > 0
		return nil
	})
	if err != nil {
		return false, err
	}
	return applied, nil
}

func (postgresGroupDialect) ExpiredGroups(ctx context.Context, q msginsql.Querier, table string, before time.Time, leaseTTL time.Duration, limit int) ([]msginsql.GroupRows, error) {
	gt, mt, err := groupTables(table)
	if err != nil {
		return nil, err
	}
	// $1 = age-path enabled (before non-zero); $2 = leaseTTL micros; $3 = before
	// as epoch micros; $4 = limit. A crashed lease (locked, aged past TTL) is
	// returned regardless of age; an unleased group older than before is returned
	// only when the age path is enabled; live-leased groups are excluded.
	beforeSet := !before.IsZero()
	rows, err := q.QueryContext(ctx,
		fmt.Sprintf(`SELECT group_key, created_at FROM %s
WHERE (locked_by IS NOT NULL AND locked_at <= %s - $2)
   OR ($1 AND locked_by IS NULL AND created_at < $3)
ORDER BY created_at
LIMIT $4`, gt, pgNowMicros),
		beforeSet, leaseTTL.Microseconds(), before.UnixMicro(), limit)
	if err != nil {
		return nil, err
	}
	type cand struct {
		key           string
		createdMicros int64
	}
	var cands []cand
	for rows.Next() {
		var c cand
		if err := rows.Scan(&c.key, &c.createdMicros); err != nil {
			rows.Close()
			return nil, err
		}
		cands = append(cands, c)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	out := make([]msginsql.GroupRows, 0, len(cands))
	for _, c := range cands {
		members, err := pgSelectMembers(ctx, q, mt, c.key, "claimed_epoch IS NULL")
		if err != nil {
			return nil, err
		}
		out = append(out, msginsql.GroupRows{GroupKey: c.key, CreatedAt: time.UnixMicro(c.createdMicros), Members: members})
	}
	return out, nil
}

func (postgresGroupDialect) EnsureGroupSchema(ctx context.Context, q msginsql.Querier, table string) error {
	gt, mt, err := groupTables(table)
	if err != nil {
		return err
	}
	// Separate statements: pgx's extended protocol rejects multi-statement Exec.
	// All are IF NOT EXISTS, so EnsureGroupSchema is idempotent.
	stmts := []string{
		postgresCreateGroupTable(gt),
		postgresCreateMemberTable(mt),
		postgresCreateGroupIndex(gt, pgQuote(table+"_expiry_idx")),
		postgresCreateMemberIndex(mt, pgQuote(table+"_member_claim_idx")),
	}
	for _, s := range stmts {
		if _, err := q.ExecContext(ctx, s); err != nil {
			return err
		}
	}
	return nil
}

func (postgresGroupDialect) SchemaExists(ctx context.Context, q msginsql.Querier, table string) (bool, error) {
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

// pgLockGroup takes the group-row lock (SELECT ... FOR UPDATE) at the top of a
// settle/abandon so the lock order is uniformly group -> member (H-B). It
// reports false (no error) when the group row is absent — a fence miss, since a
// settle/abandon of a vanished group is a no-op.
func pgLockGroup(ctx context.Context, q msginsql.Querier, gt, groupKey string) (bool, error) {
	var one int
	err := q.QueryRowContext(ctx,
		fmt.Sprintf(`SELECT 1 FROM %s WHERE group_key = $1 FOR UPDATE`, gt), groupKey).Scan(&one)
	if errors.Is(err, stdsql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// pgCountMembers returns how many member rows remain for groupKey.
func pgCountMembers(ctx context.Context, q msginsql.Querier, mt, groupKey string) (int64, error) {
	var n int64
	err := q.QueryRowContext(ctx,
		fmt.Sprintf(`SELECT count(*) FROM %s WHERE group_key = $1`, mt), groupKey).Scan(&n)
	return n, err
}

// pgSelectMembers returns the members of groupKey matching claimedWhere
// (a caller-built, injection-safe fragment: "claimed_epoch IS NULL" or
// "claimed_epoch = <int64>"), ordered by seq then msg_id.
func pgSelectMembers(ctx context.Context, q msginsql.Querier, mt, groupKey, claimedWhere string) ([]msginsql.MemberRow, error) {
	rows, err := q.QueryContext(ctx,
		fmt.Sprintf(`SELECT msg_id, seq, headers, payload FROM %s
WHERE group_key = $1 AND %s ORDER BY seq, msg_id`, mt, claimedWhere),
		groupKey)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []msginsql.MemberRow
	for rows.Next() {
		var m msginsql.MemberRow
		var seq stdsql.NullInt64
		if err := rows.Scan(&m.MsgID, &seq, &m.Headers, &m.Payload); err != nil {
			return nil, err
		}
		m.Seq = seq.Int64
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
