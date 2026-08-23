package mysql

import (
	"context"
	stdsql "database/sql"
	"errors"
	"fmt"
	"time"

	msgin "github.com/kartaladev/msgin"
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

func (mysqlGroupDialect) AddMember(ctx context.Context, q msginsql.Querier, table, groupKey, msgID string, seq int64, headers, payload []byte, maxMembers int) (msginsql.GroupRows, error) {
	if msgID == "" {
		return msginsql.GroupRows{}, msginsql.ErrMissingMsgID
	}
	gt, mt, err := groupTables(table)
	if err != nil {
		return msginsql.GroupRows{}, err
	}
	out := msginsql.GroupRows{GroupKey: groupKey}
	// overflowErr is set inside the transaction when the member cap is
	// exceeded, and is what tells the post-rollback code below to return the
	// live snapshot alongside the error rather than an empty GroupRows.
	var overflowErr error
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
		// Read created_at (the group row is already X-locked by the upsert
		// above). locked_by rides out of the SAME statement at zero extra
		// cost: it is what discriminates the member-cap rejection's
		// classification below (NULL = unleased = permanent; non-NULL = a
		// claim is in flight = transient — Spec 017 §3.3.1). It is
		// deliberately NOT added to GroupRows; it is local to this method.
		var createdMicros int64
		var lockedBy stdsql.NullString
		if err := tx.QueryRowContext(ctx,
			fmt.Sprintf("SELECT created_at, locked_by FROM %s WHERE group_key = ?", gt),
			groupKey).Scan(&createdMicros, &lockedBy); err != nil {
			return err
		}
		out.CreatedAt = time.UnixMicro(createdMicros)
		if _, err := tx.ExecContext(ctx,
			fmt.Sprintf(`INSERT IGNORE INTO %s (group_key, msg_id, seq, headers, payload, claimed_epoch)
VALUES (?, ?, ?, ?, ?, NULL)`, mt),
			groupKey, msgID, seq, headers, payload); err != nil {
			return err
		}
		// The member cap, enforced AFTER the upsert so a re-add of an existing
		// id at exactly the cap stays a no-op (Spec 017 §3.6.1). The count is
		// EVERY row for the key — live AND claimed — via the shipped
		// mysqlCountMembers: ClaimGroup stamps every live member, so a
		// live-only count would readmit maxMembers more rows per claim cycle,
		// forever.
		if maxMembers > 0 {
			n, err := mysqlCountMembers(ctx, tx, mt, groupKey)
			if err != nil {
				return err
			}
			if n > int64(maxMembers) {
				overflowErr = groupOverflow(groupKey, n, maxMembers, lockedBy.Valid)
			}
		}
		// The live fetch is bounded only here — the helper's limit is 0
		// (unlimited) for ClaimGroup and ExpiredGroups, whose sets must never
		// be truncated (ADR 0033 D-AS).
		members, err := mysqlSelectMembers(ctx, tx, mt, groupKey, "claimed_epoch IS NULL", selectLimit(maxMembers))
		if err != nil {
			return err
		}
		if overflowErr != nil {
			// The post-rollback live set: everything except the member just
			// refused. No extra query (Spec 017 §3.6.3).
			out.Members = withoutMember(members, msgID)
			return overflowErr
		}
		out.Members = members
		return nil
	})
	if err != nil {
		if overflowErr != nil && errors.Is(err, overflowErr) {
			return out, err // the snapshot rides out WITH the overflow rejection
		}
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
		members, err := mysqlSelectMembers(ctx, tx, mt, groupKey, fmt.Sprintf("claimed_epoch = %d", newEpoch), 0)
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
		members, err := mysqlSelectMembers(ctx, q, mt, c.key, "claimed_epoch IS NULL", 0)
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

// groupOverflowSite is the %s this dialect substitutes into a member-cap
// rejection. It names the ENGINE, not sql.GroupStore.Add: the error is minted
// inside the dialect, which cannot know whether it was reached through the
// store or through a direct AddMember call, and only this form tells an
// operator WHICH engine rejected. It follows the shipped msgin/sql/<engine>:
// convention every other error in this file uses (Spec 017 §3.6.3).
const groupOverflowSite = "msgin/sql/mysql: AddMember"

// groupOverflow builds AddMember's member-cap rejection: the shared
// msgin.ErrOverflowDropped shape (Spec 017 §3.3), msgin.Permanent-wrapped iff
// the group is NOT leased. An unleased group at cap will not drain itself, so
// a transient rejection would hot-spin under the shipped zero-value
// msgin.RetryPolicy; a LEASED one is about to be drained by the in-flight
// claim's Settle/Abandon, so its retry genuinely succeeds and it stays
// transient (Spec 017 §3.3.1, ADR 0033 D-AM).
//
// n is the count AT THE MOMENT OF THE CHECK — after the member upsert, so it
// reads one above the limit. Do not "normalise" it to n-1: that renders a
// count no statement in the transaction ever observed.
func groupOverflow(groupKey string, n int64, maxMembers int, leased bool) error {
	err := fmt.Errorf("%w: %s: group %q holds %d members, limit %d",
		msgin.ErrOverflowDropped, groupOverflowSite, groupKey, n, maxMembers)
	if leased {
		return err
	}
	return msgin.Permanent(err)
}

// selectLimit maps AddMember's maxMembers onto mysqlSelectMembers' limit:
// maxMembers+1 rows are enough to serve the post-rollback snapshot at the cap,
// and an unbounded (maxMembers <= 0) call keeps the unlimited fetch.
func selectLimit(maxMembers int) int {
	if maxMembers <= 0 {
		return 0
	}
	return maxMembers + 1
}

// withoutMember returns members with msgID removed — the post-rollback live
// set, computed in Go from rows already fetched rather than by re-querying.
func withoutMember(members []msginsql.MemberRow, msgID string) []msginsql.MemberRow {
	out := make([]msginsql.MemberRow, 0, len(members))
	for _, m := range members {
		if m.MsgID != msgID {
			out = append(out, m)
		}
	}
	return out
}

// mysqlSelectMembers returns groupKey's members matching claimedWhere (an
// injection-safe fragment built by the caller: "claimed_epoch IS NULL" or
// "claimed_epoch = <int64>"), ordered by seq then msg_id.
//
// limit caps the number of rows fetched; 0 means UNLIMITED and emits no LIMIT
// clause. Only AddMember passes a non-zero value (maxMembers+1, enough for the
// over-cap snapshot). ClaimGroup and ExpiredGroups MUST pass 0: a LIMIT on the
// claimed set would release an incomplete aggregate, and a LIMIT on the
// recovery set would make the reaper drop members — neither is visible without
// the harness cases that assert the full sets (ADR 0033 D-AS).
func mysqlSelectMembers(ctx context.Context, q msginsql.Querier, mt, groupKey, claimedWhere string, limit int) ([]msginsql.MemberRow, error) {
	limitClause := ""
	if limit > 0 {
		limitClause = fmt.Sprintf(" LIMIT %d", limit)
	}
	rows, err := q.QueryContext(ctx,
		fmt.Sprintf(`SELECT msg_id, seq, headers, payload FROM %s
WHERE group_key = ? AND %s ORDER BY seq, msg_id%s`, mt, claimedWhere, limitClause),
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
