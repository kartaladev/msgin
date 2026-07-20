package sqlite

import (
	"context"
	stdsql "database/sql"
	"errors"
	"fmt"
	"time"

	msginsql "github.com/kartaladev/msgin/adapter/database/sql"
)

// sqliteGroupDialect is the built-in msginsql.GroupDialect for SQLite. It is
// stateless; a single value is shared by all callers. It owns the group-
// aggregation SQL for the two-table schema (member table derived as
// table+"_member", ADR 0021 §2), storing created_at/locked_at as epoch-
// microsecond INTEGERs (the nowMicros DB-clock expression) so lease/expiry math
// is portable integer arithmetic — identical semantics to the postgres/mysql
// GroupDialects.
//
// # Completion-serialization without FOR UPDATE (M-D)
//
// SQLite has no SELECT ... FOR UPDATE, and database/sql's BeginTx opens a
// DEFERRED transaction whose read snapshot is taken at first read — two
// concurrent AddMembers could each miss the other's committed member,
// reintroducing the H1 race. So every multi-statement operation runs on a
// DEDICATED *sql.Conn via a raw BEGIN IMMEDIATE / COMMIT, acquiring the database
// write lock up front (the single-writer analog of the group-row lock). A raw
// BEGIN over a pooled conn is invisible to database/sql, so every error path
// ROLLBACKs before returning the conn to the pool (audit R3).
type sqliteGroupDialect struct{}

var _ msginsql.GroupDialect = sqliteGroupDialect{}

// GroupDialect returns the built-in SQLite GroupDialect. Pass it as the required
// dialect argument to msginsql.NewGroupStore. Its multi-statement operations run
// on a dedicated *sql.Conn with BEGIN IMMEDIATE, so a *sql.DB (not a *sql.Tx)
// must back the store (ADR 0021 §3, M-D).
func GroupDialect() msginsql.GroupDialect { return sqliteGroupDialect{} }

// connOpener is the capability withImmediateConn needs: *sql.DB satisfies it via
// Conn. Type-asserting it (rather than requiring *sql.DB) mirrors the engine's
// BeginLockTx txBeginner pattern (lock.go).
type connOpener interface {
	Conn(ctx context.Context) (*stdsql.Conn, error)
}

// withImmediateConn runs fn on a dedicated connection inside a raw
// BEGIN IMMEDIATE / COMMIT, rolling back (and always returning the conn to the
// pool) on any error path. fn receives the *sql.Conn as a Querier.
func withImmediateConn(ctx context.Context, q msginsql.Querier, fn func(conn msginsql.Querier) error) error {
	opener, ok := q.(connOpener)
	if !ok {
		return fmt.Errorf("msgin/sql/sqlite: group ops require a *sql.DB Querier (dedicated BEGIN IMMEDIATE conn), got %T", q)
	}
	conn, err := opener.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return err
	}
	if err := fn(conn); err != nil {
		_, _ = conn.ExecContext(ctx, "ROLLBACK")
		return err
	}
	if _, err := conn.ExecContext(ctx, "COMMIT"); err != nil {
		_, _ = conn.ExecContext(ctx, "ROLLBACK")
		return err
	}
	return nil
}

// groupTables validates table once and returns the quoted group-lease table and
// its derived member table (table+"_member").
func groupTables(table string) (groupTable, memberTable string, err error) {
	if err := msginsql.ValidateIdent(table); err != nil {
		return "", "", err
	}
	return sqliteQuote(table), sqliteQuote(table + "_member"), nil
}

func (sqliteGroupDialect) AddMember(ctx context.Context, q msginsql.Querier, table, groupKey, msgID string, seq int64, headers, payload []byte) (msginsql.GroupRows, error) {
	if msgID == "" {
		return msginsql.GroupRows{}, msginsql.ErrMissingMsgID
	}
	gt, mt, err := groupTables(table)
	if err != nil {
		return msginsql.GroupRows{}, err
	}
	out := msginsql.GroupRows{GroupKey: groupKey}
	err = withImmediateConn(ctx, q, func(conn msginsql.Querier) error {
		if _, err := conn.ExecContext(ctx,
			fmt.Sprintf(`INSERT INTO %s (group_key, created_at, epoch) VALUES (?, %s, 0)
ON CONFLICT (group_key) DO NOTHING`, gt, nowMicros),
			groupKey); err != nil {
			return err
		}
		var createdMicros int64
		if err := conn.QueryRowContext(ctx,
			fmt.Sprintf("SELECT created_at FROM %s WHERE group_key = ?", gt),
			groupKey).Scan(&createdMicros); err != nil {
			return err
		}
		out.CreatedAt = time.UnixMicro(createdMicros)
		if _, err := conn.ExecContext(ctx,
			fmt.Sprintf(`INSERT INTO %s (group_key, msg_id, seq, headers, payload, claimed_epoch)
VALUES (?, ?, ?, ?, ?, NULL) ON CONFLICT (group_key, msg_id) DO NOTHING`, mt),
			groupKey, msgID, seq, headers, payload); err != nil {
			return err
		}
		members, err := sqliteSelectMembers(ctx, conn, mt, groupKey, "claimed_epoch IS NULL")
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

func (sqliteGroupDialect) ClaimGroup(ctx context.Context, q msginsql.Querier, table, groupKey, lockedBy string, leaseTTL time.Duration) (*msginsql.ClaimedGroup, error) {
	gt, mt, err := groupTables(table)
	if err != nil {
		return nil, err
	}
	var result *msginsql.ClaimedGroup
	err = withImmediateConn(ctx, q, func(conn msginsql.Querier) error {
		res, err := conn.ExecContext(ctx,
			fmt.Sprintf(`UPDATE %s SET epoch = epoch + 1, locked_by = ?, locked_at = %s
WHERE group_key = ? AND (locked_by IS NULL OR locked_at <= %s - ?)`, gt, nowMicros, nowMicros),
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
		var newEpoch, createdMicros int64
		if err := conn.QueryRowContext(ctx,
			fmt.Sprintf("SELECT epoch, created_at FROM %s WHERE group_key = ?", gt),
			groupKey).Scan(&newEpoch, &createdMicros); err != nil {
			return err
		}
		if _, err := conn.ExecContext(ctx,
			fmt.Sprintf(`UPDATE %s SET claimed_epoch = ?
WHERE group_key = ? AND (claimed_epoch IS NULL OR claimed_epoch < ?)`, mt),
			newEpoch, groupKey, newEpoch); err != nil {
			return err
		}
		members, err := sqliteSelectMembers(ctx, conn, mt, groupKey, fmt.Sprintf("claimed_epoch = %d", newEpoch))
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

func (sqliteGroupDialect) SettleGroup(ctx context.Context, q msginsql.Querier, table, groupKey, lockedBy string, epoch int64) (bool, error) {
	gt, mt, err := groupTables(table)
	if err != nil {
		return false, err
	}
	var applied bool
	err = withImmediateConn(ctx, q, func(conn msginsql.Querier) error {
		if exists, err := sqliteGroupExists(ctx, conn, gt, groupKey); err != nil || !exists {
			return err
		}
		if _, err := conn.ExecContext(ctx,
			fmt.Sprintf("DELETE FROM %s WHERE group_key = ? AND claimed_epoch = ?", mt),
			groupKey, epoch); err != nil {
			return err
		}
		remaining, err := sqliteCountMembers(ctx, conn, mt, groupKey)
		if err != nil {
			return err
		}
		var res stdsql.Result
		if remaining > 0 {
			res, err = conn.ExecContext(ctx,
				fmt.Sprintf(`UPDATE %s SET locked_by = NULL, locked_at = NULL, created_at = %s
WHERE group_key = ? AND locked_by = ? AND epoch = ?`, gt, nowMicros),
				groupKey, lockedBy, epoch)
		} else {
			res, err = conn.ExecContext(ctx,
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

func (sqliteGroupDialect) AbandonGroup(ctx context.Context, q msginsql.Querier, table, groupKey, lockedBy string, epoch int64) (bool, error) {
	gt, mt, err := groupTables(table)
	if err != nil {
		return false, err
	}
	var applied bool
	err = withImmediateConn(ctx, q, func(conn msginsql.Querier) error {
		if exists, err := sqliteGroupExists(ctx, conn, gt, groupKey); err != nil || !exists {
			return err
		}
		if _, err := conn.ExecContext(ctx,
			fmt.Sprintf("UPDATE %s SET claimed_epoch = NULL WHERE group_key = ? AND claimed_epoch = ?", mt),
			groupKey, epoch); err != nil {
			return err
		}
		res, err := conn.ExecContext(ctx,
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

func (sqliteGroupDialect) ExpiredGroups(ctx context.Context, q msginsql.Querier, table string, before time.Time, leaseTTL time.Duration, limit int) ([]msginsql.GroupRows, error) {
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
LIMIT ?`, gt, nowMicros),
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
		members, err := sqliteSelectMembers(ctx, q, mt, c.key, "claimed_epoch IS NULL")
		if err != nil {
			return nil, err
		}
		out = append(out, msginsql.GroupRows{GroupKey: c.key, CreatedAt: time.UnixMicro(c.createdMicros), Members: members})
	}
	return out, nil
}

func (sqliteGroupDialect) EnsureGroupSchema(ctx context.Context, q msginsql.Querier, table string) error {
	gt, mt, err := groupTables(table)
	if err != nil {
		return err
	}
	stmts := []string{
		sqliteCreateGroupTable(gt),
		sqliteCreateMemberTable(mt),
		sqliteCreateGroupIndex(gt, sqliteQuote(table+"_expiry_idx")),
		sqliteCreateMemberIndex(mt, sqliteQuote(table+"_member_claim_idx")),
	}
	for _, s := range stmts {
		if _, err := q.ExecContext(ctx, s); err != nil {
			return err
		}
	}
	return nil
}

func (sqliteGroupDialect) SchemaExists(ctx context.Context, q msginsql.Querier, table string) (bool, error) {
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

// sqliteGroupExists reports whether the group row is present (fence-miss guard
// for settle/abandon; the BEGIN IMMEDIATE write lock is the group->member order
// analog, so no FOR UPDATE is needed).
func sqliteGroupExists(ctx context.Context, q msginsql.Querier, gt, groupKey string) (bool, error) {
	var one int
	err := q.QueryRowContext(ctx,
		fmt.Sprintf("SELECT 1 FROM %s WHERE group_key = ?", gt), groupKey).Scan(&one)
	if errors.Is(err, stdsql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// sqliteCountMembers returns how many member rows remain for groupKey.
func sqliteCountMembers(ctx context.Context, q msginsql.Querier, mt, groupKey string) (int64, error) {
	var n int64
	err := q.QueryRowContext(ctx,
		fmt.Sprintf("SELECT count(*) FROM %s WHERE group_key = ?", mt), groupKey).Scan(&n)
	return n, err
}

// sqliteSelectMembers returns groupKey's members matching claimedWhere (an
// injection-safe fragment: "claimed_epoch IS NULL" or "claimed_epoch = <int64>"),
// ordered by seq then msg_id.
func sqliteSelectMembers(ctx context.Context, q msginsql.Querier, mt, groupKey, claimedWhere string) ([]msginsql.MemberRow, error) {
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
