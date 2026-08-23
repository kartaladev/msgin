package sqlite

import (
	"context"
	stdsql "database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"time"

	msgin "github.com/kartaladev/msgin"
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
		if _, rbErr := conn.ExecContext(ctx, "ROLLBACK"); rbErr != nil {
			discardConn(conn)
		}
		return err
	}
	if _, err := conn.ExecContext(ctx, "COMMIT"); err != nil {
		if _, rbErr := conn.ExecContext(ctx, "ROLLBACK"); rbErr != nil {
			discardConn(conn)
		}
		return err
	}
	return nil
}

// discardConn forces database/sql to evict conn from the pool instead of
// returning it on the caller's subsequent Close: called only when a
// defensive ROLLBACK itself failed, leaving the connection's transaction
// state unknown, so a still-mid-transaction connection is never handed back
// for reuse. Conn.Raw is the documented mechanism — a raw f that returns
// driver.ErrBadConn makes database/sql discard the connection rather than
// pool it (see database/sql's (*Conn).Raw / putConn). The error Raw itself
// returns is always driver.ErrBadConn here and carries no new information,
// so it is deliberately discarded.
func discardConn(conn *stdsql.Conn) {
	_ = conn.Raw(func(any) error { return driver.ErrBadConn })
}

// groupTables validates table once and returns the quoted group-lease table and
// its derived member table (table+"_member").
func groupTables(table string) (groupTable, memberTable string, err error) {
	if err := msginsql.ValidateIdent(table); err != nil {
		return "", "", err
	}
	return sqliteQuote(table), sqliteQuote(table + "_member"), nil
}

func (sqliteGroupDialect) AddMember(ctx context.Context, q msginsql.Querier, table, groupKey, msgID string, seq int64, headers, payload []byte, maxMembers int) (msginsql.GroupRows, error) {
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
	err = withImmediateConn(ctx, q, func(conn msginsql.Querier) error {
		if _, err := conn.ExecContext(ctx,
			fmt.Sprintf(`INSERT INTO %s (group_key, created_at, epoch) VALUES (?, %s, 0)
ON CONFLICT (group_key) DO NOTHING`, gt, nowMicros),
			groupKey); err != nil {
			return err
		}
		// locked_by rides out of the SAME statement that reads created_at, at
		// zero extra cost: it is what discriminates the member-cap rejection's
		// classification below (NULL = unleased = permanent; non-NULL = a claim
		// is in flight = transient — Spec 017 §3.3.1). It is deliberately NOT
		// added to GroupRows; it is local to this method.
		var createdMicros int64
		var lockedBy stdsql.NullString
		if err := conn.QueryRowContext(ctx,
			fmt.Sprintf("SELECT created_at, locked_by FROM %s WHERE group_key = ?", gt),
			groupKey).Scan(&createdMicros, &lockedBy); err != nil {
			return err
		}
		out.CreatedAt = time.UnixMicro(createdMicros)
		if _, err := conn.ExecContext(ctx,
			fmt.Sprintf(`INSERT INTO %s (group_key, msg_id, seq, headers, payload, claimed_epoch)
VALUES (?, ?, ?, ?, ?, NULL) ON CONFLICT (group_key, msg_id) DO NOTHING`, mt),
			groupKey, msgID, seq, headers, payload); err != nil {
			return err
		}
		// The member cap, enforced AFTER the upsert so a re-add of an existing
		// id at exactly the cap stays a no-op (Spec 017 §3.6.1). The count is
		// EVERY row for the key — live AND claimed — via the shipped
		// sqliteCountMembers: ClaimGroup stamps every live member, so a
		// live-only count would readmit maxMembers more rows per claim cycle,
		// forever.
		//
		// Placement relative to the group upsert is irrelevant here, unlike on
		// postgres/mysql: BEGIN IMMEDIATE already holds a DATABASE-WIDE write
		// lock, which is also why this extra O(members) scan lengthens a GLOBAL
		// critical section rather than a per-key one (Spec 017 §3.6).
		if maxMembers > 0 {
			n, err := sqliteCountMembers(ctx, conn, mt, groupKey)
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
		members, err := sqliteSelectMembers(ctx, conn, mt, groupKey, "claimed_epoch IS NULL", selectLimit(maxMembers))
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
		members, err := sqliteSelectMembers(ctx, conn, mt, groupKey, fmt.Sprintf("claimed_epoch = %d", newEpoch), 0)
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
		members, err := sqliteSelectMembers(ctx, q, mt, c.key, "claimed_epoch IS NULL", 0)
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

// groupOverflowSite is the %s this dialect substitutes into a member-cap
// rejection. It names the ENGINE, not sql.GroupStore.Add: the error is minted
// inside the dialect, which cannot know whether it was reached through the
// store or through a direct AddMember call, and only this form tells an
// operator WHICH engine rejected. It follows the shipped msgin/sql/<engine>:
// convention every other error in this file uses (Spec 017 §3.6.3).
const groupOverflowSite = "msgin/sql/sqlite: AddMember"

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

// selectLimit maps AddMember's maxMembers onto sqliteSelectMembers' limit:
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

// sqliteSelectMembers returns groupKey's members matching claimedWhere (an
// injection-safe fragment: "claimed_epoch IS NULL" or "claimed_epoch = <int64>"),
// ordered by seq then msg_id.
//
// limit caps the number of rows fetched; 0 means UNLIMITED and emits no LIMIT
// clause. Only AddMember passes a non-zero value (maxMembers+1, enough for the
// over-cap snapshot). ClaimGroup and ExpiredGroups MUST pass 0: a LIMIT on the
// claimed set would release an incomplete aggregate, and a LIMIT on the
// recovery set would make the reaper drop members — neither is visible without
// the harness cases that assert the full sets (ADR 0033 D-AS).
func sqliteSelectMembers(ctx context.Context, q msginsql.Querier, mt, groupKey, claimedWhere string, limit int) ([]msginsql.MemberRow, error) {
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
