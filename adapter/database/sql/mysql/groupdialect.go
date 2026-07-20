package mysql

import (
	"context"
	stdsql "database/sql"
	"errors"
	"fmt"
	"time"

	msginsql "github.com/kartaladev/msgin/adapter/database/sql"
)

// mysqlNowMicros is the DB-clock expression: the current UTC time as epoch
// microseconds (BIGINT). TIMESTAMPDIFF(MICROSECOND, epoch-literal,
// UTC_TIMESTAMP(6)) is pure datetime arithmetic between two UTC values, so it is
// session-timezone-independent (unlike UNIX_TIMESTAMP, which interprets its
// argument in the session zone). created_at/locked_at persist in this unit so
// every lease/expiry comparison is portable integer arithmetic on the DB clock —
// no app<->DB skew, no engine-specific INTERVAL syntax (ADR 0021 §3/§4).
const mysqlNowMicros = `TIMESTAMPDIFF(MICROSECOND, '1970-01-01 00:00:00', UTC_TIMESTAMP(6))`

// mysqlGroupDialect is the built-in msginsql.GroupDialect for MySQL and
// wire-compatible derivatives (MariaDB). It is stateless; a single value is
// shared by all callers. Behavior-identical to the postgres GroupDialect
// (group-row locking, epoch fence, claimed-set re-absorption) expressed in
// MySQL SQL; the member table is derived as table+"_member" (ADR 0021 §2).
//
// # The two-step, atomic claim (no RETURNING)
//
// MySQL has no RETURNING, so ClaimGroup runs the fenced UPDATE and then, inside
// the SAME transaction (the UPDATE's row lock still held), reads the bumped
// epoch via SELECT — never a separate-transaction read another claimant could
// bump between (audit R1 M3). The winner is rowsAffected==1.
type mysqlGroupDialect struct{}

var _ msginsql.GroupDialect = mysqlGroupDialect{}

// GroupDialect returns the built-in MySQL/MariaDB GroupDialect. Pass it as the
// required dialect argument to msginsql.NewGroupStore. It owns the exact
// MySQL-family group-aggregation SQL for the durable, multi-process Aggregator
// store; MariaDB rides on this dialect (wire-compatible) (ADR 0021).
func GroupDialect() msginsql.GroupDialect { return mysqlGroupDialect{} }

// mysqlRunInTx runs fn inside a transaction resolved from q (the pool begins a
// fresh tx committed on success / rolled back on error; an already-open *sql.Tx
// runs directly, caller owns commit). It reuses the module-local txBeginner
// declared in dialect.go.
func mysqlRunInTx(ctx context.Context, q msginsql.Querier, fn func(tx msginsql.Querier) error) error {
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
	return fmt.Errorf("msgin/sql/mysql: group ops require a *sql.DB or *sql.Tx Querier, got %T", q)
}

// groupTables validates table once and returns the quoted group-lease table and
// its derived member table (table+"_member").
func groupTables(table string) (groupTable, memberTable string, err error) {
	if err := msginsql.ValidateIdent(table); err != nil {
		return "", "", err
	}
	return mysqlQuote(table), mysqlQuote(table + "_member"), nil
}

func (mysqlGroupDialect) AddMember(ctx context.Context, q msginsql.Querier, table, groupKey, msgID string, seq int64, headers, payload []byte) (msginsql.GroupRows, error) {
	if msgID == "" {
		return msginsql.GroupRows{}, msginsql.ErrMissingMsgID
	}
	gt, mt, err := groupTables(table)
	if err != nil {
		return msginsql.GroupRows{}, err
	}
	out := msginsql.GroupRows{GroupKey: groupKey}
	err = mysqlRunInTx(ctx, q, func(tx msginsql.Querier) error {
		// Upsert-and-X-LOCK the group row FIRST (H1): INSERT ... ON DUPLICATE KEY
		// UPDATE takes an EXCLUSIVE lock on the (new or existing) group row
		// directly, so concurrent same-key adds serialize on it. A plain
		// INSERT IGNORE + SELECT ... FOR UPDATE would instead take a SHARED lock
		// on an existing row (duplicate check) and then try to upgrade S->X — two
		// interleaved adds each holding S and each wanting X self-deadlock (InnoDB
		// 1213). The no-op `group_key = group_key` update never touches created_at,
		// so it is still set exactly once, on first arrival.
		if _, err := tx.ExecContext(ctx,
			fmt.Sprintf(`INSERT INTO %s (group_key, created_at, epoch) VALUES (?, %s, 0)
ON DUPLICATE KEY UPDATE group_key = group_key`, gt, mysqlNowMicros),
			groupKey); err != nil {
			return err
		}
		// Read created_at (the group row is already X-locked by the upsert above).
		var createdMicros int64
		if err := tx.QueryRowContext(ctx,
			fmt.Sprintf("SELECT created_at FROM %s WHERE group_key = ?", gt),
			groupKey).Scan(&createdMicros); err != nil {
			return err
		}
		out.CreatedAt = time.UnixMicro(createdMicros)
		if _, err := tx.ExecContext(ctx,
			fmt.Sprintf(`INSERT IGNORE INTO %s (group_key, msg_id, seq, headers, payload, claimed_epoch)
VALUES (?, ?, ?, ?, ?, NULL)`, mt),
			groupKey, msgID, seq, headers, payload); err != nil {
			return err
		}
		members, err := mysqlSelectMembers(ctx, tx, mt, groupKey, "claimed_epoch IS NULL")
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

func (mysqlGroupDialect) ClaimGroup(ctx context.Context, q msginsql.Querier, table, groupKey, lockedBy string, leaseTTL time.Duration) (*msginsql.ClaimedGroup, error) {
	gt, mt, err := groupTables(table)
	if err != nil {
		return nil, err
	}
	var result *msginsql.ClaimedGroup
	err = mysqlRunInTx(ctx, q, func(tx msginsql.Querier) error {
		res, err := tx.ExecContext(ctx,
			fmt.Sprintf(`UPDATE %s SET epoch = epoch + 1, locked_by = ?, locked_at = %s
WHERE group_key = ? AND (locked_by IS NULL OR locked_at <= %s - ?)`, gt, mysqlNowMicros, mysqlNowMicros),
			lockedBy, groupKey, leaseTTL.Microseconds())
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if n == 0 {
			return nil // absent or actively leased: (nil, nil)
		}
		// No RETURNING: read the just-bumped epoch + created_at inside the same
		// tx, the UPDATE's row lock still held (audit R1 M3).
		var newEpoch, createdMicros int64
		if err := tx.QueryRowContext(ctx,
			fmt.Sprintf("SELECT epoch, created_at FROM %s WHERE group_key = ?", gt),
			groupKey).Scan(&newEpoch, &createdMicros); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx,
			fmt.Sprintf(`UPDATE %s SET claimed_epoch = ?
WHERE group_key = ? AND (claimed_epoch IS NULL OR claimed_epoch < ?)`, mt),
			newEpoch, groupKey, newEpoch); err != nil {
			return err
		}
		members, err := mysqlSelectMembers(ctx, tx, mt, groupKey, fmt.Sprintf("claimed_epoch = %d", newEpoch))
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

func (mysqlGroupDialect) SettleGroup(ctx context.Context, q msginsql.Querier, table, groupKey, lockedBy string, epoch int64) (bool, error) {
	gt, mt, err := groupTables(table)
	if err != nil {
		return false, err
	}
	var applied bool
	err = mysqlRunInTx(ctx, q, func(tx msginsql.Querier) error {
		if locked, err := mysqlLockGroup(ctx, tx, gt, groupKey); err != nil || !locked {
			return err
		}
		if _, err := tx.ExecContext(ctx,
			fmt.Sprintf("DELETE FROM %s WHERE group_key = ? AND claimed_epoch = ?", mt),
			groupKey, epoch); err != nil {
			return err
		}
		remaining, err := mysqlCountMembers(ctx, tx, mt, groupKey)
		if err != nil {
			return err
		}
		var res stdsql.Result
		if remaining > 0 {
			res, err = tx.ExecContext(ctx,
				fmt.Sprintf(`UPDATE %s SET locked_by = NULL, locked_at = NULL, created_at = %s
WHERE group_key = ? AND locked_by = ? AND epoch = ?`, gt, mysqlNowMicros),
				groupKey, lockedBy, epoch)
		} else {
			res, err = tx.ExecContext(ctx,
				fmt.Sprintf("DELETE FROM %s WHERE group_key = ? AND locked_by = ? AND epoch = ?", gt),
				groupKey, lockedBy, epoch)
		}
		if err != nil {
			return err
		}
		nn, err := res.RowsAffected()
		if err != nil {
			return err
		}
		applied = nn > 0
		return nil
	})
	if err != nil {
		return false, err
	}
	return applied, nil
}

func (mysqlGroupDialect) AbandonGroup(ctx context.Context, q msginsql.Querier, table, groupKey, lockedBy string, epoch int64) (bool, error) {
	gt, mt, err := groupTables(table)
	if err != nil {
		return false, err
	}
	var applied bool
	err = mysqlRunInTx(ctx, q, func(tx msginsql.Querier) error {
		if locked, err := mysqlLockGroup(ctx, tx, gt, groupKey); err != nil || !locked {
			return err
		}
		if _, err := tx.ExecContext(ctx,
			fmt.Sprintf("UPDATE %s SET claimed_epoch = NULL WHERE group_key = ? AND claimed_epoch = ?", mt),
			groupKey, epoch); err != nil {
			return err
		}
		res, err := tx.ExecContext(ctx,
			fmt.Sprintf(`UPDATE %s SET locked_by = NULL, locked_at = NULL
WHERE group_key = ? AND locked_by = ? AND epoch = ?`, gt),
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

func (mysqlGroupDialect) ExpiredGroups(ctx context.Context, q msginsql.Querier, table string, before time.Time, leaseTTL time.Duration, limit int) ([]msginsql.GroupRows, error) {
	gt, mt, err := groupTables(table)
	if err != nil {
		return nil, err
	}
	beforeSet := 0
	if !before.IsZero() {
		beforeSet = 1
	}
	rows, err := q.QueryContext(ctx,
		fmt.Sprintf(`SELECT group_key, created_at FROM %s
WHERE (locked_by IS NOT NULL AND locked_at <= %s - ?)
   OR (? = 1 AND locked_by IS NULL AND created_at < ?)
ORDER BY created_at
LIMIT ?`, gt, mysqlNowMicros),
		leaseTTL.Microseconds(), beforeSet, before.UnixMicro(), limit)
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
		members, err := mysqlSelectMembers(ctx, q, mt, c.key, "claimed_epoch IS NULL")
		if err != nil {
			return nil, err
		}
		out = append(out, msginsql.GroupRows{GroupKey: c.key, CreatedAt: time.UnixMicro(c.createdMicros), Members: members})
	}
	return out, nil
}

func (mysqlGroupDialect) EnsureGroupSchema(ctx context.Context, q msginsql.Querier, table string) error {
	gt, mt, err := groupTables(table)
	if err != nil {
		return err
	}
	// Each CREATE TABLE (with inline indexes — MySQL has no CREATE INDEX IF NOT
	// EXISTS) is idempotent; run them as separate statements.
	if _, err := q.ExecContext(ctx, mysqlCreateGroupTable(gt, mysqlQuote(table+"_expiry_idx"))); err != nil {
		return err
	}
	if _, err := q.ExecContext(ctx, mysqlCreateMemberTable(mt, mysqlQuote(table+"_member_claim_idx"))); err != nil {
		return err
	}
	return nil
}

func (mysqlGroupDialect) SchemaExists(ctx context.Context, q msginsql.Querier, table string) (bool, error) {
	if err := msginsql.ValidateIdent(table); err != nil {
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

// mysqlLockGroup takes the group-row lock (SELECT ... FOR UPDATE) at the top of
// a settle/abandon (uniform group -> member order, H-B). It reports false (no
// error) when the group row is absent (a fence miss).
func mysqlLockGroup(ctx context.Context, q msginsql.Querier, gt, groupKey string) (bool, error) {
	var one int
	err := q.QueryRowContext(ctx,
		fmt.Sprintf("SELECT 1 FROM %s WHERE group_key = ? FOR UPDATE", gt), groupKey).Scan(&one)
	if errors.Is(err, stdsql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// mysqlCountMembers returns how many member rows remain for groupKey.
func mysqlCountMembers(ctx context.Context, q msginsql.Querier, mt, groupKey string) (int64, error) {
	var n int64
	err := q.QueryRowContext(ctx,
		fmt.Sprintf("SELECT count(*) FROM %s WHERE group_key = ?", mt), groupKey).Scan(&n)
	return n, err
}

// mysqlSelectMembers returns groupKey's members matching claimedWhere (an
// injection-safe fragment built by the caller: "claimed_epoch IS NULL" or
// "claimed_epoch = <int64>"), ordered by seq then msg_id.
func mysqlSelectMembers(ctx context.Context, q msginsql.Querier, mt, groupKey, claimedWhere string) ([]msginsql.MemberRow, error) {
	rows, err := q.QueryContext(ctx,
		fmt.Sprintf(`SELECT msg_id, seq, headers, payload FROM %s
WHERE group_key = ? AND %s ORDER BY seq, msg_id`, mt, claimedWhere),
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
