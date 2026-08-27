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

func (mysqlGroupDialect) AddMember(ctx context.Context, q msginsql.Querier, table, groupKey, msgID string, seq int64, headers, payload []byte, maxMembers int, leaseTTL time.Duration) (msginsql.GroupRows, error) {
	if msgID == "" {
		return msginsql.GroupRows{}, msginsql.ErrMissingMsgID
	}
	// D-AV: the bound itself is validated BEFORE any statement runs, in the
	// one shared helper all three dialects call, so maxMembers is provably
	// UnboundedGroupMembers or in [1, 1<<20] everywhere below — which is what
	// keeps the `n > int64(maxMembers)` comparison meaningful. At math.MaxInt
	// no count can ever exceed it, so the largest expressible bound would
	// silently be NO bound (review finding R-15).
	if err := msginsql.ValidateMaxMembers(groupOverflowSite, maxMembers); err != nil {
		return msginsql.GroupRows{}, err
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
		// above). The LEASE-LIVENESS flag rides out of the SAME statement at
		// zero extra cost: it is what discriminates the member-cap rejection's
		// classification below (no live lease = nothing drains this group =
		// permanent; a live lease = a claim is in flight = transient —
		// Spec 017 §3.3.1 as corrected by §3.6a.1). It is deliberately NOT
		// added to GroupRows; it is local to this method.
		//
		// The predicate is the NEGATION of the one ClaimGroup steals on, in
		// the same file and on the same DB SERVER CLOCK: ClaimGroup takes the
		// row when `locked_by IS NULL OR locked_at <= now - leaseTTL`, so a
		// lease is LIVE exactly when neither holds. Testing locked_by alone
		// would call a CRASHED holder's stranded lease "live" forever and
		// classify every subsequent over-cap add transient — B-1's unlogged
		// zero-delay Nack loop (ADR 0033 D-AU, review finding R-7).
		//
		// The leaseTTL placeholder sits in the SELECT LIST, so it binds BEFORE
		// the WHERE clause's groupKey — hence the argument order below.
		// Scanned as NullBool, not bool: SQL three-valued logic yields NULL if
		// a row ever held locked_by non-NULL with locked_at NULL (no shipped
		// statement writes that pair — both are set and cleared together), and
		// NullBool's zero value maps that to "not live", the conservative arm.
		var createdMicros int64
		var leaseLive stdsql.NullBool
		if err := tx.QueryRowContext(ctx,
			fmt.Sprintf("SELECT created_at, (locked_by IS NOT NULL AND locked_at > %s - ?) FROM %s WHERE group_key = ?",
				mysqlNowMicros, gt),
			leaseTTL.Microseconds(), groupKey).Scan(&createdMicros, &leaseLive); err != nil {
			return err
		}
		out.CreatedAt = time.UnixMicro(createdMicros)
		res, err := tx.ExecContext(ctx,
			fmt.Sprintf(`INSERT IGNORE INTO %s (group_key, msg_id, seq, headers, payload, claimed_epoch)
VALUES (?, ?, ?, ?, ?, NULL)`, mt),
			groupKey, msgID, seq, headers, payload)
		if err != nil {
			return err
		}
		// Did THIS statement store the row? Affected-rows is 1 for an insert and
		// 0 for a row IGNORE skipped.
		//
		// INSERT IGNORE demotes a broader class of faults to warnings than
		// postgres' and sqlite's targeted ON CONFLICT DO NOTHING, so "0 means
		// duplicate" is a claim about THIS TABLE, not about the statement in
		// general — and the shipped member DDL is what makes it true: the only
		// constraint IGNORE can skip a row on is a key violation, and
		// mysqlCreateMemberTable declares exactly one key, PRIMARY KEY
		// (group_key, msg_id), with no foreign key and no secondary UNIQUE index
		// (the one INDEX it adds is non-unique). Value-level faults IGNORE
		// demotes — truncation, coercion — still INSERT the row and report 1, so
		// they cannot be mistaken for a duplicate. A future migration adding a
		// UNIQUE constraint to this table must revisit this line.
		//
		// The mechanism is derived PER ENGINE (ADR 0033 D-AP — "one mechanism
		// asserted for three engines" is this bundle's recorded, repeated
		// defect). MySQL has no RETURNING on INSERT (MariaDB does; this dialect
		// serves both), so postgres' read-it-back form is unavailable here and
		// affected-rows — what this dialect's own ClaimGroup already relies on in
		// this file — is the portable discriminator.
		inserted, err := res.RowsAffected()
		if err != nil {
			return err
		}
		// The member cap, enforced AFTER the upsert AND only against a member
		// this statement actually INSERTED, so a re-add of an existing id is a
		// no-op at the cap and equally above it (Spec 017 §3.6.1, review finding
		// R-11). A no-op stores nothing, so it cannot grow the group past any
		// bound; refusing it would terminally discard a redelivery the SPI
		// guarantees is idempotent, and which adapter/memory's GroupStore
		// answers from its dedup branch with (full snapshot, nil).
		//
		// The count is EVERY row for the key — live AND claimed — via the
		// shipped mysqlCountMembers: ClaimGroup stamps every live member, so a
		// live-only count would readmit maxMembers more rows per claim cycle,
		// forever.
		if inserted > 0 && maxMembers != msginsql.UnboundedGroupMembers {
			n, err := mysqlCountMembers(ctx, tx, mt, groupKey)
			if err != nil {
				return err
			}
			if n > int64(maxMembers) {
				overflowErr = groupOverflow(groupKey, n, maxMembers, leaseLive.Bool)
			}
		}
		// UNLIMITED, exactly like ClaimGroup's and ExpiredGroups' fetches: a
		// member set the caller ACTS ON must never be truncated (ADR 0033 D-AS).
		// This call used to bound the fetch to maxMembers+1, which is sound only
		// while the group holds at most that many rows — false the moment the
		// cap is LOWERED beneath a group's existing size, by a rolling deploy or
		// by two instances configured differently. It then returned a truncated
		// snapshot that the release predicate reads as an incomplete group, so
		// the member is dead-lettered AND the complete group never releases —
		// the deadlock this snapshot exists to prevent (review finding R-1).
		// mysqlSelectMembers no longer takes a limit at all, so the bound cannot
		// be reintroduced at this call site; see its godoc.
		//
		// It costs no new ceiling: ClaimGroup already pulls this same group's
		// full member set with no LIMIT from this same table, and
		// UnboundedGroupMembers already made this very fetch unlimited.
		members, err := mysqlSelectMembers(ctx, tx, mt, groupKey, "claimed_epoch IS NULL")
		if err != nil {
			return err
		}
		if overflowErr != nil {
			// The post-rollback live set: everything except the member just
			// refused. No extra query (Spec 017 §3.6.3). Reachable only when
			// inserted > 0, which is what makes removing msgID correct: a member
			// this statement did NOT store was never refused.
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

// groupOverflowSite is the %s this dialect substitutes into a member-cap
// rejection. It names the ENGINE, not sql.GroupStore.Add: the error is minted
// inside the dialect, which cannot know whether it was reached through the
// store or through a direct AddMember call, and only this form tells an
// operator WHICH engine rejected. It follows the shipped msgin/sql/<engine>:
// convention every other error in this file uses (Spec 017 §3.6.3).
const groupOverflowSite = "msgin/sql/mysql: AddMember"

// groupOverflow builds AddMember's member-cap rejection: the shared
// msgin.ErrOverflowDropped shape (Spec 017 §3.3), msgin.Permanent-wrapped iff
// no LIVE lease is draining the group. A group with no lease — or with one that
// has already aged past leaseTTL, i.e. a CRASHED holder's stranded stamp — will
// not drain itself, so a transient rejection would hot-spin under the shipped
// zero-value msgin.RetryPolicy; a group under a LIVE lease is about to be
// drained by the in-flight claim's Settle/Abandon, so its retry genuinely
// succeeds and it stays transient (Spec 017 §3.3.1/§3.6a.1, ADR 0033 D-AM as
// amended by D-AU).
//
// leaseLive is computed IN SQL on the DB server clock by the caller, as the
// negation of ClaimGroup's own steal predicate — never from the app clock, and
// never from locked_by alone (review finding R-7).
//
// n is the count AT THE MOMENT OF THE CHECK — after the member upsert, so it
// reads one above the limit. Do not "normalise" it to n-1: that renders a
// count no statement in the transaction ever observed.
func groupOverflow(groupKey string, n int64, maxMembers int, leaseLive bool) error {
	err := fmt.Errorf("%w: %s: group %q holds %d members, limit %d",
		msgin.ErrOverflowDropped, groupOverflowSite, groupKey, n, maxMembers)
	if leaseLive {
		return err
	}
	return msgin.Permanent(err)
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
// The fetch is UNLIMITED, and no caller can ask for anything else. All three —
// AddMember, ClaimGroup and ExpiredGroups — ACT ON the set they get back, and
// ADR 0033 D-AS's rule is that such a set is never truncated: a LIMIT on the
// claimed set releases an incomplete aggregate, and a LIMIT on the recovery set
// makes the reaper drop members.
//
// The helper used to take a private limit int (0 = unlimited) so AddMember
// could pass maxMembers+1. Review finding R-1 removed that bound — a cap
// LOWERED beneath a group's stored size returned a truncated snapshot, which
// the release predicate reads as an incomplete group, so the member was
// dead-lettered AND the complete group never released — leaving all three
// callers passing 0. The parameter is deleted rather than left dead so D-AS's
// rule is enforced structurally: there is no longer any way to express the
// truncation (D-AS's MECHANISM is superseded, its RULE is not; Spec 017
// §3.6.3, §3.6a.3).
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
