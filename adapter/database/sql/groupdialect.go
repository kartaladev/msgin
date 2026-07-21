package sql

import (
	"context"
	"time"
)

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
	// the DB server clock — never a caller-supplied now), takes the GROUP
	// ROW LOCK (SELECT ... FOR UPDATE or equivalent) BEFORE reading or
	// writing any member row — so concurrent same-key AddMember calls, even
	// from different processes, SERIALIZE on this lock (audit R1 H1: under
	// READ COMMITTED, two processes each adding one member of a size-N group
	// would otherwise each snapshot only their own uncommitted-elsewhere
	// member and neither would observe completion) — then upserts the member
	// row (ON CONFLICT(group_key,msg_id) DO NOTHING / INSERT IGNORE, so a
	// redelivered member is a no-op), and finally SELECTs the group's
	// current CreatedAt plus its LIVE members (claimed_epoch IS NULL),
	// ordered by seq then msg_id. Commits atomically. An empty msgID is
	// rejected with a typed error (ErrMissingMsgID) BEFORE any statement
	// runs — audit R1 H3, durable aggregation requires message ids. seq is
	// msgin.HeaderSequenceNumber (0 if the member's headers carried none);
	// headers/payload are the already-framed bytes (EncodeHeaders output /
	// runtime-codec wire body) to persist verbatim.
	AddMember(ctx context.Context, q Querier, table, groupKey, msgID string, seq int64, headers, payload []byte) (GroupRows, error)

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
