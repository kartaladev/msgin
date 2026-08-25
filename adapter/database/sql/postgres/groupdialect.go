package postgres

import (
	"context"
	stdsql "database/sql"
	"errors"
	"fmt"
	"time"

	msgin "github.com/kartaladev/msgin"
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

func (postgresGroupDialect) AddMember(ctx context.Context, q msginsql.Querier, table, groupKey, msgID string, seq int64, headers, payload []byte, maxMembers int, leaseTTL time.Duration) (msginsql.GroupRows, error) {
	if msgID == "" {
		return msginsql.GroupRows{}, msginsql.ErrMissingMsgID
	}
	// D-AV: the bound itself is validated BEFORE any statement runs, in the
	// one shared helper all three dialects call, so maxMembers is provably
	// UnboundedGroupMembers or in [1, 1<<20] everywhere below — which is what
	// makes selectLimit's maxMembers+1 total.
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
		//
		// Correctness-neutral cost: the no-op DO UPDATE still writes a new (dead)
		// heap tuple on every re-add to an existing group, like any PostgreSQL
		// MVCC UPDATE -- a hot correlation key accumulates dead tuples between
		// autovacuum runs. Autovacuum reclaims them; this is not a leak.
		//
		// The LEASE-LIVENESS flag rides out of this same statement at zero
		// extra cost: it is what discriminates the member-cap rejection's
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
		// Scanned as NullBool, not bool: SQL three-valued logic yields NULL if
		// a row ever held locked_by non-NULL with locked_at NULL (no shipped
		// statement writes that pair — both are set and cleared together), and
		// NullBool's zero value maps that to "not live", the conservative arm.
		var createdMicros int64
		var leaseLive stdsql.NullBool
		if err := tx.QueryRowContext(ctx,
			fmt.Sprintf(`INSERT INTO %s (group_key, created_at, epoch) VALUES ($1, %s, 0)
ON CONFLICT (group_key) DO UPDATE SET group_key = EXCLUDED.group_key
RETURNING created_at, (locked_by IS NOT NULL AND locked_at > %s - $2)`, gt, pgNowMicros, pgNowMicros),
			groupKey, leaseTTL.Microseconds()).Scan(&createdMicros, &leaseLive); err != nil {
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
		// The member cap, enforced AFTER the upsert so a re-add of an existing
		// id at exactly the cap stays a no-op (Spec 017 §3.6.1). The count is
		// EVERY row for the key — live AND claimed — via the shipped
		// pgCountMembers: ClaimGroup stamps every live member, so a live-only
		// count would readmit maxMembers more rows per claim cycle, forever.
		if maxMembers != msginsql.UnboundedGroupMembers {
			n, err := pgCountMembers(ctx, tx, mt, groupKey)
			if err != nil {
				return err
			}
			if n > int64(maxMembers) {
				overflowErr = groupOverflow(groupKey, n, maxMembers, leaseLive.Bool)
			}
		}
		// The live fetch is bounded only here — the helper's limit is 0
		// (unlimited) for ClaimGroup and ExpiredGroups, whose sets must never
		// be truncated (ADR 0033 D-AS).
		members, err := pgSelectMembers(ctx, tx, mt, groupKey, "claimed_epoch IS NULL", selectLimit(maxMembers))
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
		members, err := pgSelectMembers(ctx, tx, mt, groupKey, fmt.Sprintf("claimed_epoch = %d", newEpoch), 0)
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
		members, err := pgSelectMembers(ctx, q, mt, c.key, "claimed_epoch IS NULL", 0)
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

// groupOverflowSite is the %s this dialect substitutes into a member-cap
// rejection. It names the ENGINE, not sql.GroupStore.Add: the error is minted
// inside the dialect, which cannot know whether it was reached through the
// store or through a direct AddMember call, and only this form tells an
// operator WHICH engine rejected. It follows the shipped msgin/sql/<engine>:
// convention every other error in this file uses (Spec 017 §3.6.3).
const groupOverflowSite = "msgin/sql/postgres: AddMember"

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

// selectLimit maps AddMember's maxMembers onto pgSelectMembers' limit:
// maxMembers+1 rows are enough to serve the post-rollback snapshot at the cap,
// and msginsql.UnboundedGroupMembers keeps the unlimited fetch.
//
// It is TOTAL because of its PRECONDITION, not because of a clamp:
// msginsql.ValidateMaxMembers has already refused everything outside
// {UnboundedGroupMembers} u [1, 1<<20] before AddMember reaches this, so
// maxMembers+1 cannot overflow. Without that check, selectLimit(math.MaxInt)
// returned math.MinInt, the `limit > 0` guard then emitted no LIMIT, and
// `n > int64(maxMembers)` could never fire — the largest expressible bound
// silently meant NO bound (ADR 0033 D-AV, review finding R-15). The <= 0 arm
// stays, now reachable only via UnboundedGroupMembers.
//
// 🔴 THAT ARM IS NOW ARITHMETICALLY REDUNDANT, AND IT IS KEPT ANYWAY. Since
// UnboundedGroupMembers is -1, the fallthrough would compute -1+1 = 0, which is
// already this function's "no LIMIT" value — so deleting the guard is a mutant
// that SURVIVES the whole conformance suite, correctly. It is retained as
// executable documentation of the mapping and as insurance against the sentinel
// ever being renumbered; do not delete it because a coverage tool calls it
// unreached.
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

// pgSelectMembers returns the members of groupKey matching claimedWhere
// (a caller-built, injection-safe fragment: "claimed_epoch IS NULL" or
// "claimed_epoch = <int64>"), ordered by seq then msg_id.
//
// limit caps the number of rows fetched; 0 means UNLIMITED and emits no LIMIT
// clause. Only AddMember passes a non-zero value (maxMembers+1, enough for the
// over-cap snapshot). ClaimGroup and ExpiredGroups MUST pass 0: a LIMIT on the
// claimed set would release an incomplete aggregate, and a LIMIT on the
// recovery set would make the reaper drop members — neither is visible without
// the harness cases that assert the full sets (ADR 0033 D-AS).
func pgSelectMembers(ctx context.Context, q msginsql.Querier, mt, groupKey, claimedWhere string, limit int) ([]msginsql.MemberRow, error) {
	limitClause := ""
	if limit > 0 {
		limitClause = fmt.Sprintf(" LIMIT %d", limit)
	}
	rows, err := q.QueryContext(ctx,
		fmt.Sprintf(`SELECT msg_id, seq, headers, payload FROM %s
WHERE group_key = $1 AND %s ORDER BY seq, msg_id%s`, mt, claimedWhere, limitClause),
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
