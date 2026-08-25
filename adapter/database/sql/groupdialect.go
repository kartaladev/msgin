package sql

import (
	"context"
	"fmt"
	"time"

	msgin "github.com/kartaladev/msgin"
)

// UnboundedGroupMembers is the ONLY value of GroupDialect.AddMember's
// maxMembers that requests an UNBOUNDED group — one whose member table may grow
// without limit. Every other non-positive value, INCLUDING 0, is rejected by
// ValidateMaxMembers with msgin.ErrInvalidCapacity.
//
// # Who would pass it, and what they are accepting
//
// A DIRECT caller of the GroupDialect SPI that has not adopted Spec 017's
// per-group bound — a caller driving AddMember on its own *sql.Tx, or a
// migration running against groups that already exceed any cap it would choose.
// sql.GroupStore never passes it: NewGroupStore range-checks WithMaxGroupMembers
// to [1, 1048576] at construction, so a store-driven flow cannot reach this
// value at all.
//
// The risk is the whole of what Spec 017 exists to close. With no bound, one
// correlation key whose release never fires accumulates member rows forever;
// each AddMember re-SELECTs the group's entire live set, so the per-arrival cost
// is linear in the group and the total cost quadratic. There is no back pressure
// and no diagnostic — the table simply grows.
//
// # Why -1 and not 0 (ADR 0033 D-AV)
//
// The zero value must not be the dangerous value. maxMembers is a POSITIONAL
// SPI parameter, so an omitted or unset-config-field argument arrives as 0 with
// no signal that anything was omitted; making 0 mean "unbounded" turned that
// mistake into a silent fail-open. -1 has to be typed on purpose. (Contrast
// endpoint.WithMaxPayloadBytes, whose `n <= 0 disables` is defensible because it
// is reached through an OPTION, where omission genuinely means "unset".)
const UnboundedGroupMembers = -1

// ValidateMaxMembers reports whether maxMembers is a legal
// GroupDialect.AddMember bound: it must be UnboundedGroupMembers, or in
// [1, 1048576] (maxGroupMembersCeiling — the same ceiling
// WithMaxGroupMembers enforces at construction). Anything else is a typed
// msgin.ErrInvalidCapacity naming site, the offending value, the accepted range
// and the sentinel.
//
// Every GroupDialect implementation MUST call it BEFORE any statement runs —
// the same validate-before-I/O placement ErrMissingMsgID has. It lives here,
// once, rather than being copy-pasted into postgres/mysql/sqlite, so the
// parameter's contract is executable in ONE place instead of prose in three
// (ADR 0033 D-AV; Spec 017 §3.6a.2). site is the calling dialect's own
// "msgin/sql/<engine>: AddMember" string, so an operator learns which engine
// was driven with the bad bound.
//
// # It rejects 0, and that is the point
//
// maxMembers <= 0 once meant UNBOUNDED, which made the ZERO VALUE the dangerous
// value on an exported SPI. It now means "invalid"; opting out is spelled
// UnboundedGroupMembers. This is a behavioral break for a direct dialect caller
// that passed 0 — loud, one line to fix, and free at pre-v1.
//
// # It also makes selectLimit total
//
// The three dialects derive their live-member fetch limit as maxMembers+1.
// Before this check, AddMember(…, math.MaxInt, …) overflowed that sum to
// math.MinInt: the `limit > 0` guard then emitted no LIMIT clause AND
// `n > int64(maxMembers)` could never fire, so the largest expressible bound
// silently delivered NO bound (whole-branch review finding R-15). With
// maxMembers provably in [1, 1048576] by the time a dialect reaches the sum,
// +1 cannot overflow.
//
// # The rejection is msgin.Permanent, unlike checkRange's constructor arms
//
// The other msgin.ErrInvalidCapacity producers are OPTION validators, whose
// error is handed back at construction and never carried through a
// MessageHandler, so they stay bare (ADR 0029 D-M). This one is returned from
// AddMember — a per-message hot-path surface. An invalid constant argument
// fails identically on every redelivery, so a transient classification would
// Nack-loop with no delay and no log under the shipped zero-value
// msgin.RetryPolicy (the B-1 hot spin D-AM and D-AU both exist to prevent).
// Permanent settles it terminally instead. errors.Is(err,
// msgin.ErrInvalidCapacity) is unaffected — permanentError unwraps
// transparently. This mirrors ADR 0031 D-V, which likewise wraps a sizing fault
// in Permanent when it surfaces at USE time rather than at construction.
func ValidateMaxMembers(site string, maxMembers int) error {
	if maxMembers == UnboundedGroupMembers {
		return nil
	}
	if maxMembers >= 1 && maxMembers <= maxGroupMembersCeiling {
		return nil
	}
	return msgin.Permanent(fmt.Errorf("%w: %s: %d not in [%d, %d] and not UnboundedGroupMembers (%d)",
		msgin.ErrInvalidCapacity, site, maxMembers, 1, maxGroupMembersCeiling, UnboundedGroupMembers))
}

// GroupRows is one correlation group plus its LIVE (unclaimed) members, as raw
// framed bytes — the carrier a GroupDialect method returns and sql.GroupStore
// decodes into a msgin.MessageGroup. Mirrors ClaimedRow's raw-bytes contract:
// Headers/Payload are the exact framed bytes written by AddMember (EncodeHeaders
// output / the runtime-codec wire body); the store, not the dialect, decodes
// them. Members are ordered by seq then msg_id (ADR 0021 §2 "Ordering").
type GroupRows struct {
	// GroupKey is the correlation key (msgin_group.group_key).
	GroupKey string
	// CreatedAt is the group's arrival-time clock, used for expiry: the first
	// member's arrival, or (after a SettleGroup residual reset) the residual's
	// reset time. Always the DB server clock — never the app clock.
	CreatedAt time.Time
	// Members are the group's live (claimed_epoch IS NULL) members, ordered by
	// seq then msg_id.
	Members []MemberRow
}

// MemberRow is one persisted group member, as raw framed bytes.
type MemberRow struct {
	// MsgID is the member's message id (msgin_group_member.msg_id) — the
	// idempotency key for AddMember, and never empty (H3: AddMember rejects an
	// empty msgID).
	MsgID string
	// Seq is the persisted msgin.HeaderSequenceNumber, or 0 when the member's
	// headers carried none. It exists as a column purely so the group's
	// members can be fetched pre-ordered by the DB (ORDER BY seq, msg_id)
	// without decoding every row's headers first; the authoritative sequence
	// value still lives in Headers (ADR 0021 §2).
	Seq int64
	// Headers is the framed JSON headers blob written by EncodeHeaders.
	Headers []byte
	// Payload is the raw wire body (the runtime-codec-encoded []byte); the
	// paired typed runtime decodes it downstream — the store never sees T
	// (EmitsLiveValue()==false, ADR 0001).
	Payload []byte
}

// ClaimedGroup is a GroupRows frozen at claim time, plus the fence epoch
// ClaimGroup assigned it. Members is the CLAIMED set (claimed_epoch == Epoch),
// not the live set — a member that arrives after the claim is NOT included and
// survives as a fresh residual group (ADR 0021 §2 "Why claimed_epoch on
// members").
type ClaimedGroup struct {
	GroupRows
	// Epoch is the fence token stamped on the claimed member set and the group
	// row's lease. SettleGroup/AbandonGroup take effect only while the group's
	// current epoch still matches; a lease that expired and was re-claimed
	// (epoch bumped) makes a stale holder's settle/abandon a no-op.
	Epoch int64
}

// GroupDialect is the exported SPI a caller supplies, as the required dialect
// argument to NewGroupStore, to teach sql.GroupStore a database's exact
// group-aggregation SQL — a NEW, segregated interface (interface-segregation,
// like InboxDialect vs LeaseDialect): group-keyed idempotent add, a per-group
// lease, claimed-set fencing, and an expiry scan are not row-oriented queue
// operations, so they do not belong on LeaseDialect, and a LeaseDialect author
// must not be forced to implement them (ADR 0021 §3). The built-ins are
// postgres.GroupDialect(), mysql.GroupDialect() (covers MariaDB), and
// sqlite.GroupDialect(), each in its own module.
//
// Every method fully owns its statement(s) and any multi-statement transaction
// orchestration; no cross-dialect SQL ever runs. Every method validates table
// with ValidateIdent BEFORE dialect-quoting and interpolating it into SQL — the
// table identifier cannot be a bound parameter, so this is the sole injection
// guard (mirrors LeaseDialect/InboxDialect). table is the BASE identifier from
// which a dialect derives BOTH the two-table schema's names (ADR 0021 §2) — the
// group-lease table and its append-only member table (e.g. table and a
// dialect-owned derived name such as table+"_member"); the exact derivation is
// a dialect implementation detail, documented by each built-in. All persisted
// timestamps and lease/expiry comparisons use the DB server clock (now()),
// never the app clock, and leaseTTL is passed as an interval-typed duration
// parameter — no app<->DB skew (ADR 0010 D3/D4, reused here).
//
// # Lock order (deadlock avoidance)
//
// Every multi-statement method that touches both tables acquires the GROUP row
// lock (SELECT ... FOR UPDATE, or the engine's equivalent) BEFORE any member
// row is read or written, uniformly across AddMember, ClaimGroup, SettleGroup,
// and AbandonGroup. A member-first lock in any one method would let it
// ABBA-deadlock against a group-first method under concurrent load (ADR 0021
// §4 "Settle / Abandon are fenced AND lock the group row FIRST", audit R2
// H-B) — implementers MUST preserve this order.
//
// # Message ids are required
//
// Members are keyed (group_key, msg_id): AddMember rejects an empty msgID with
// a typed error (audit R1 H3) rather than silently colliding id-less members
// into one row. Durable aggregation therefore requires message ids — source
// deliveries carry msgin.HeaderMessageID and the Splitter stamps a deterministic
// child id, so this is not a real-world restriction (ADR 0021 §2).
//
// A dialect author implementing GroupDialect for a new engine or
// wire-compatible derivative should add a compile-time assertion:
//
//	var _ msginsql.GroupDialect = (*yourDialect)(nil)
//
// This is a pre-1.0 (v0) contract that may still evolve.
type GroupDialect interface {
	// AddMember durably, idempotently appends one member to the group table,
	// in ONE transaction: it upserts the group row (created_at set once, via
	// the DB server clock — never a caller-supplied now) with a statement
	// that SERIALIZES concurrent same-key adds BEFORE any member row is read
	// or written — by a group-row lock on postgres and mysql, by BEGIN
	// IMMEDIATE's database-wide write lock on sqlite (ADR 0033 D-AP; there
	// is no one mechanism, and an implementer for a new engine must supply
	// one of their own). Serialization is required, not stylistic (audit R1
	// H1: under READ COMMITTED, two processes each adding one member of a
	// size-N group would otherwise each snapshot only their own
	// uncommitted-elsewhere member and neither would observe completion). It
	// then upserts the member row (ON CONFLICT(group_key,msg_id) DO NOTHING
	// / INSERT IGNORE, so a redelivered member is a no-op), and finally
	// SELECTs the group's current CreatedAt plus its LIVE members
	// (claimed_epoch IS NULL), ordered by seq then msg_id. Commits
	// atomically. An empty msgID is rejected with a typed error
	// (ErrMissingMsgID) BEFORE any statement runs — audit R1 H3, durable
	// aggregation requires message ids. seq is msgin.HeaderSequenceNumber (0
	// if the member's headers carried none); headers/payload are the
	// already-framed bytes (EncodeHeaders output / runtime-codec wire body)
	// to persist verbatim.
	//
	// # The member cap is enforced IN THIS TRANSACTION (Spec 017 §3.6)
	//
	// maxMembers is the bound on how many members ONE group may hold —
	// sql.GroupStore threads its WithMaxGroupMembers here. AFTER the member
	// upsert (so an idempotent re-add of an existing id at exactly the cap
	// stays a no-op), the implementation MUST count EVERY member row for the
	// key — LIVE AND CLAIMED, i.e. SELECT count(*) with no claimed_epoch
	// predicate — and, if that count exceeds maxMembers, MUST abort the
	// transaction and return msgin.ErrOverflowDropped wrapped as
	// "%w: msgin/sql/<engine>: AddMember: group %q holds %d members, limit %d".
	// Counting only the live set does not bound anything: ClaimGroup stamps
	// every live member, so the next maxMembers arrivals see an empty live
	// set and the table grows by maxMembers per claim cycle, forever.
	//
	// The rejection is classified by whether the group's lease is LIVE, using
	// the SAME predicate the implementation's own ClaimGroup steals on —
	// locked_by IS NULL OR locked_at <= now() - leaseTTL, evaluated on the DB
	// SERVER CLOCK, never the app clock. An expired-or-absent lease means
	// nothing will drain the group on its own, so the error MUST be wrapped
	// in msgin.Permanent and the runtime settles it terminally instead of
	// hot-spinning on the shipped zero-value msgin.RetryPolicy; a LIVE lease
	// means a claim is in flight that will drain it, so the error stays bare
	// and transient.
	//
	// 🔴 TESTING locked_by ALONE IS NOT ENOUGH, and that is why leaseTTL is a
	// parameter (ADR 0033 D-AU, whole-branch review finding R-7). A non-NULL
	// locked_by proves a row was STAMPED, not that a holder is alive: a
	// crashed or stranded releaser leaves the group leased with nothing
	// draining it, and every subsequent over-cap add is then classified
	// transient FOREVER — the exact infinite zero-delay Nack loop this
	// classification exists to prevent.
	//
	// The group's remaining LIVE members SHOULD be returned in the GroupRows
	// ALONGSIDE that error, with the just-upserted member filtered out, so
	// the Aggregator can re-evaluate its release strategy against a group
	// that is full but complete (Spec 017 §3.3a).
	//
	// maxMembers MUST be validated with ValidateMaxMembers BEFORE any
	// statement runs — the same validate-before-I/O placement ErrMissingMsgID
	// has. The accepted set is UnboundedGroupMembers or [1, 1048576]; 0 is an
	// ERROR, not a synonym for unbounded (ADR 0033 D-AV). Only
	// UnboundedGroupMembers skips the count, and it cannot arrive from
	// sql.GroupStore, whose constructor rejects anything outside
	// [1, 1048576]; it exists so a direct dialect caller that has not opted
	// into a bound can keep the pre-Spec-017 behavior — explicitly, and at
	// the risk UnboundedGroupMembers documents.
	//
	// # PRECONDITION: the rollback is only ours when the transaction is
	//
	// The cap is enforced BY ROLLBACK, and a dialect only rolls back a
	// transaction it OWNS — the one it begins when q is a *sql.DB. When a
	// DIRECT caller passes its own *sql.Tx as q, the dialect runs on that
	// transaction and neither commits nor rolls it back: the over-cap member
	// row is already inserted into the caller's open transaction when the
	// error is returned. Such a caller MUST treat msgin.ErrOverflowDropped as
	// a rollback trigger; committing anyway exceeds the cap durably. A
	// library must not roll back a transaction it cannot see the rest of, so
	// this is stated rather than engineered away. It does not apply to
	// sql.GroupStore, which always owns its transaction (see
	// WithMaxGroupMembers).
	AddMember(ctx context.Context, q Querier, table, groupKey, msgID string, seq int64, headers, payload []byte, maxMembers int, leaseTTL time.Duration) (GroupRows, error)

	// ClaimGroup atomically leases the group's current members, in ONE
	// transaction: it bumps the group row's epoch and stamps
	// locked_by/locked_at (the DB server clock), FENCED so the UPDATE
	// matches only when the row is unleased or its existing lease has aged
	// past leaseTTL — exactly one concurrent claimant's UPDATE affects a row
	// (the winner). The winner is detected by rowsAffected==1 (a dialect MAY
	// additionally use RETURNING where the engine supports it; a dialect
	// without RETURNING reads the bumped epoch via a SELECT inside the SAME
	// transaction, while the row lock from the UPDATE is still held — never
	// a separate-transaction read another claimant could race, audit R1
	// M3). It returns (nil, nil), with NO error and NO transaction side
	// effects, when the group is absent or is actively (unexpired) leased by
	// another holder.
	//
	// Having won, ClaimGroup RE-ABSORBS a possibly-superseded prior claim's
	// members (audit R1 H2): it stamps claimed_epoch = the NEW epoch on
	// every member row where claimed_epoch IS NULL OR claimed_epoch <
	// newEpoch — NOT merely IS NULL. Tagging only IS NULL would leave a
	// CRASHED holder's already-tagged-but-never-settled members permanently
	// orphaned (the re-claim would see zero members, emit nothing, and the
	// real members are neither emitted nor deleted — a silent, permanent
	// loss). Because the group-row lease fence guarantees at most one ACTIVE
	// claim at a time, every claimed_epoch < newEpoch is provably a dead
	// (crashed or already-settled-elsewhere) claim and is always safe to
	// re-absorb — this re-absorption is what makes "a crashed holder's
	// lease ages out, another worker re-claims" resolve to a duplicate
	// (safe, at-least-once), never a loss. It then SELECTs the newly-claimed
	// member set (claimed_epoch == newEpoch), ordered by seq then msg_id,
	// and returns it with the new epoch as *ClaimedGroup. Commits
	// atomically.
	ClaimGroup(ctx context.Context, q Querier, table, groupKey, lockedBy string, leaseTTL time.Duration) (*ClaimedGroup, error)

	// SettleGroup finalizes a successful release, in ONE transaction: it
	// LOCKS THE GROUP ROW FIRST (SELECT ... FOR UPDATE or equivalent —
	// uniform group-then-member lock order, audit R2 H-B, see the interface
	// doc's "Lock order" section), then DELETEs only the CLAIMED member set
	// (claimed_epoch == epoch) — never a blind delete by group_key, so a
	// member that arrived during the lease (claimed_epoch still NULL)
	// survives untouched. If members remain after the delete (a residual —
	// exactly that late-arrival case), it clears the group row's lease
	// (locked_by/locked_at = NULL) AND RESETS created_at to the DB server
	// clock — audit R1 M2, so the residual is treated as a FRESH group for
	// expiry purposes, matching memory.GroupStore's residual semantics.
	// Otherwise (no members remain) it deletes the group row entirely. The
	// whole operation is FENCED on (group_key, locked_by, epoch): a lease
	// that expired and was re-claimed (a bumped epoch, or a different
	// locked_by) makes this call a no-op. applied reports whether the fence
	// matched (true) or missed (false, nil error — a fence miss is not an
	// error; it means another holder already owns or settled this group).
	SettleGroup(ctx context.Context, q Querier, table, groupKey, lockedBy string, epoch int64) (applied bool, err error)

	// AbandonGroup releases a claim WITHOUT deleting anything, in ONE
	// transaction: it LOCKS THE GROUP ROW FIRST (same order as SettleGroup,
	// audit R2 H-B), un-tags the claimed member set (claimed_epoch = NULL
	// for rows where claimed_epoch == epoch, so they return to LIVE — a
	// retry or the next AddMember/reaper tick sees them again), and clears
	// the group row's lease (locked_by/locked_at = NULL) — the epoch itself
	// is left bumped, so a stale holder's later Settle/Abandon on the OLD
	// epoch still fences correctly as a no-op. FENCED on
	// (group_key, locked_by, epoch), exactly like SettleGroup: applied is
	// false (nil error) on a fence miss.
	AbandonGroup(ctx context.Context, q Querier, table, groupKey, lockedBy string, epoch int64) (applied bool, err error)

	// ExpiredGroups returns the groups the Aggregator's reaper sweep must
	// re-examine (audit R2 H-A — the crash-recovery mechanism): every group
	// whose LEASE has expired — locked_by IS NOT NULL AND the DB server
	// clock now() - locked_at > leaseTTL (a crashed holder), REGARDLESS OF
	// AGE — PLUS, only when before is non-zero, every UNLEASED group whose
	// created_at is strictly before before (the age-based expiry path).
	// Currently-actively-leased groups (an unexpired lease) are EXCLUDED in
	// both cases — they are being actively worked. Each returned GroupRows
	// carries the group's current LIVE members (claimed_epoch IS NULL),
	// ordered by seq then msg_id; results are ordered oldest-created_at
	// first and capped at limit. A zero before value means "crash-recovery
	// sweep only" (no age-based candidates) — callers running a store with
	// no configured expiry timeout still get crash recovery this way.
	ExpiredGroups(ctx context.Context, q Querier, table string, before time.Time, leaseTTL time.Duration, limit int) ([]GroupRows, error)

	// EnsureGroupSchema idempotently creates the two-table schema (CREATE ...
	// IF NOT EXISTS — both the group-lease table and its member table) and
	// any supporting index. It is optional and opt-in; msgin never runs DDL
	// implicitly on the production path (mirrors LeaseDialect.EnsureSchema).
	EnsureGroupSchema(ctx context.Context, q Querier, table string) error

	// SchemaExists reports whether the group-lease table exists, via a
	// portable information_schema probe that imports no SQL driver (mirrors
	// LeaseDialect.SchemaExists / InboxDialect.SchemaExists).
	SchemaExists(ctx context.Context, q Querier, table string) (bool, error)
}

// Note: reference-DDL generation is deliberately NOT a GroupDialect interface
// method, for the same identifier-injection reason documented on LeaseDialect
// (above): a string-returning method structurally cannot return
// ErrInvalidTableName, so exposing DDL(table) on the interface would be an
// unvalidated SQL-injection path (the identifier cannot be a bound parameter).
// The only public reference-DDL entry points are the per-dialect package
// builders (postgres.GroupDDL / mysql.GroupDDL / sqlite.GroupDDL, added in a
// later task), which validate the table first; each dialect keeps its builder
// unexported.
