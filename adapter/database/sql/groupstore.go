package sql

import (
	"context"
	stdsql "database/sql"
	"errors"
	"fmt"
	"log/slog"
	"time"

	msgin "github.com/kartaladev/msgin"
)

// defaultGroupLeaseTTL is the lease duration applied when WithGroupLeaseTTL is
// unset. It matches the Source's defaultLeaseTTL (5m — see options.go) rather
// than an independently-chosen, tighter value: aggregation's release is
// HEAVIER than a Source handler (it runs the caller's aggregate function AND
// output.Send, which on a DirectChannel drives a whole synchronous downstream
// sub-flow), so a stolen *live* lease double-emits to output on every recovery
// tick — the safe-default "generous margin" gate (CLAUDE.md) therefore demands
// an equal-or-more-generous default than the Source gets, not a tighter one
// (ADR 0021 §4, audit R3 M1).
const defaultGroupLeaseTTL = 5 * time.Minute

// defaultExpiredGroupsLimit bounds how many candidate groups a single Expired
// call fetches from the dialect per reaper sweep tick, so one sweep cannot pull
// an unbounded result set when many groups are simultaneously crash-expired or
// age-expired. The reaper (Aggregator.Run) re-sweeps on its own interval, so a
// backlog beyond this cap is simply picked up over subsequent ticks — nothing
// is skipped, only spread across more sweeps.
const defaultExpiredGroupsLimit = 100

// defaultMaxGroupMembers is the number of members WithMaxGroupMembers admits
// into ONE correlation group when the option is not set: 65,536 (Spec 017
// §3.2). The value is not a fresh judgement — it REUSES routing's
// completionSizeCeiling (Spec 016 §3.4, ADR 0032 D-Z), which fixed 65,536 as
// "far beyond any plausible aggregation" over the identical unit, members of
// one correlation group, and it matches memory.defaultMaxGroupMembers so the
// two first-party stores bound the same quantity at the same number.
//
// The cost basis is TIME and I/O, not bytes: GroupDialect.AddMember round-trips
// and decodes the group's whole live member set on EVERY add (Spec 017 §1.3),
// so a group of m members costs Θ(m²) rows fetched over its lifetime. That is
// why no test grows a group to this value.
//
// INVARIANT: this default must stay >= routing's completionSizeCeiling. A
// caller may legally configure routing.WithCompletionSize up to that ceiling,
// and a smaller default cap here would make such a group reject its own
// completing member before the release predicate could ever fire — a silent
// deadlock in place of a bound (Spec 017 §3.5).
const defaultMaxGroupMembers = 1 << 16

// maxGroupMembersCeiling is the upper bound WithMaxGroupMembers accepts
// (Spec 017 §3.2). It matches memory.maxGroupMembersCeiling and
// memory.maxGroupsCeiling, so one number reads as "the largest in-flight
// aggregation quantity this library will accept" across both stores. The
// ceiling matters even though the check itself is a scalar comparison: it is
// what stops a caller from configuring a cap so large that the bound is
// nominal, and — per defaultMaxGroupMembers' cost note — the quadratic fetch
// cost makes even the ceiling unreachable in practice.
const maxGroupMembersCeiling = 1 << 20

// groupBase holds the fields and operations shared by GroupStore's methods:
// the db handle, target table, the caller-supplied GroupDialect, and the
// injected logger, plus the fail-fast readiness check, the opt-in schema
// bootstrap, and the query-error classifier. Mirrors adapterBase (base.go),
// but over the segregated GroupDialect SPI (ADR 0021 §3) rather than
// LeaseDialect — the two dialects are not structurally interchangeable
// (EnsureGroupSchema vs EnsureSchema), so GroupStore gets its own small base
// rather than forcing adapterBase's dialect field to a common interface.
type groupBase struct {
	db      *stdsql.DB
	table   string
	dialect GroupDialect
	logger  *slog.Logger
}

// newGroupBase validates db, table, and dialect, in that order — a nil
// dialect never masks an invalid db/table mistake — mirroring
// newAdapterBase's validation order exactly (base.go).
func newGroupBase(db *stdsql.DB, table string, dialect GroupDialect, logger *slog.Logger) (groupBase, error) {
	if db == nil {
		return groupBase{}, msgin.ErrNilAdapter
	}
	if err := ValidateIdent(table); err != nil {
		return groupBase{}, err
	}
	if dialect == nil {
		return groupBase{}, ErrNilDialect
	}
	return groupBase{db: db, table: table, dialect: dialect, logger: logger}, nil
}

// EnsureSchema idempotently creates the two-table group schema. It is optional
// and opt-in (dev/test/opt-in callers); production callers provision the
// schema via the reference DDL (postgres.GroupDDL / mysql.GroupDDL /
// sqlite.GroupDDL) instead — msgin never runs DDL implicitly (ADR 0010 D2,
// reused here).
func (b groupBase) EnsureSchema(ctx context.Context) error {
	return b.dialect.EnsureGroupSchema(ctx, b.db, b.table)
}

// Ready is the fail-fast boot check (ADR 0010 D2, reused here): it returns
// ErrSchemaNotReady (naming the table) when the schema is not initialized, so
// a forgotten migration fails the deploy immediately instead of the caller
// hitting it on the first Add/ClaimGroup. Call it once at startup.
func (b groupBase) Ready(ctx context.Context) error {
	exists, err := b.dialect.SchemaExists(ctx, b.db, b.table)
	if err != nil {
		return err
	}
	if !exists {
		return b.schemaNotReady()
	}
	return nil
}

// classifyQueryErr wraps a dialect query failure as ErrSchemaNotReady iff a
// follow-up portable probe reports the table missing (diagnosing a table
// dropped mid-run without a driver import); otherwise the raw error
// propagates, naming the table (mirrors adapterBase.classifyQueryErr).
func (b groupBase) classifyQueryErr(ctx context.Context, err error) error {
	if exists, probeErr := b.dialect.SchemaExists(ctx, b.db, b.table); probeErr == nil && !exists {
		return b.schemaNotReady()
	}
	return err
}

// schemaNotReady builds the ErrSchemaNotReady error naming the table.
func (b groupBase) schemaNotReady() error {
	return fmt.Errorf("%w: table %q not initialized; run EnsureSchema or apply the DDL",
		ErrSchemaNotReady, b.table)
}

// groupStoreConfig accumulates GroupStoreOption settings before NewGroupStore
// builds a GroupStore.
type groupStoreConfig struct {
	leaseTTL        time.Duration
	leaseTTLSet     bool // distinguishes explicit WithGroupLeaseTTL(0) (rejected) from unset (default)
	lockedBy        string
	maxGroupMembers int
	logger          *slog.Logger
}

// GroupStoreOption configures a GroupStore built by NewGroupStore.
type GroupStoreOption func(*groupStoreConfig)

// WithGroupLeaseTTL sets how long a ClaimGroup lease is held before it is
// treated as expired and the group becomes claimable again by another worker
// (the Aggregator's recovery-sweep reaper, driven at RecoverInterval). Unset,
// it defaults to 5m — the SAME safe default as the Source's WithLeaseTTL, and
// deliberately NOT a tighter value: see defaultGroupLeaseTTL's doc for why
// aggregation's release needs an equal-or-more-generous margin than a Source
// handler's (ADR 0021 §4, audit R3 M1).
//
// # Invariant (read before overriding)
//
// leaseTTL MUST exceed the worst-case release round-trip: the aggregate
// function PLUS output.Send (which, on a DirectChannel, drives a whole
// synchronous downstream sub-flow) PLUS SettleGroup latency — a margin. If
// release can take longer than leaseTTL, another worker re-claims a STILL-LIVE
// group mid-release and the aggregate is sent to output TWICE (not merely
// duplicated-on-crash) — a double emit that recurs every recovery-sweep tick
// under persistent slowness. Because RecoverInterval() reports this same TTL,
// a longer TTL also means a crashed group is recovered roughly one TTL later —
// a caller needing snappier crash-recovery, whose release is reliably fast,
// may lower this value, accepting the tighter steal window.
//
// A non-positive d is a construction error (ErrInvalidLeaseTTL) rather than a
// silent default: an explicit zero/negative is a caller mistake, not a request
// for the default.
func WithGroupLeaseTTL(d time.Duration) GroupStoreOption {
	return func(c *groupStoreConfig) {
		c.leaseTTL = d
		c.leaseTTLSet = true
	}
}

// WithGroupLockedBy sets the lease-owner id stamped on ClaimGroup and matched
// by the fenced SettleGroup/AbandonGroup. Unset, it defaults to a random
// 128-bit hex id (the Source's randomLockedBy generator) — the safe choice,
// since each GroupStore instance then owns a distinct id and two instances (or
// two processes) never mistake each other's leases. Override it only when you
// need a stable, human-readable owner for observability and you guarantee
// uniqueness per running GroupStore. An empty string is treated as unset (the
// random default is used).
func WithGroupLockedBy(id string) GroupStoreOption {
	return func(c *groupStoreConfig) { c.lockedBy = id }
}

// WithMaxGroupMembers bounds the number of members ONE correlation group may
// hold to n, which must be in [1, maxGroupMembersCeiling] (1,048,576);
// default 65,536 (see defaultMaxGroupMembers for why that number). n outside
// the range is a construction-time error (msgin.ErrInvalidCapacity), not a
// silent clamp. It is the durable twin of memory.WithMaxGroupMembers and
// carries the same name deliberately — one SPI concept, one name.
//
// WHAT IT COUNTS: every member ROW the database still holds for the key —
// LIVE plus CLAIMED. ClaimGroup stamps claimed_epoch on every live member
// without deleting anything, so claimed members keep counting until
// SettleGroup deletes them; a group at exactly n therefore rejects new
// arrivals for the duration of a claim, even though its live set is empty.
// That is what makes the bound a bound: counting live members only would let
// every claim cycle admit n more rows, forever (Spec 017 §3.4). The count
// rendered in the error is "members retained at the moment of the check", and
// this store checks AFTER the idempotent member upsert — required, so that a
// re-add of an existing id at exactly the cap stays a no-op — so at n = 4 it
// renders "holds 5 members, limit 4", one more than memory's twin, which
// checks before its append.
//
// WHERE IT IS ENFORCED: inside the dialect's own transaction, before the
// commit, so the over-cap row is rolled back rather than merely reported
// (Spec 017 §3.6). For a store built by NewGroupStore this bound is
// UNCONDITIONALLY DURABLE — the store always owns the transaction the dialect
// runs in, because NewGroupStore takes a concrete *sql.DB and Add always
// passes it. (A DIRECT caller of GroupDialect.AddMember may supply its own
// *sql.Tx; that caller owns the rollback. See GroupDialect.AddMember's godoc
// — the caveat cannot apply to a caller of this option.) The check costs one
// extra SELECT count(*) per Add, on every add rather than only on overflow;
// on sqlite that scan runs inside BEGIN IMMEDIATE's database-wide write lock.
//
// AT THE BOUNDARY: an Add that would take the group past n returns
// msgin.ErrOverflowDropped, wrapped with the engine-naming site, the group
// key, the count and the limit — e.g. "msgin/sql/postgres: AddMember: group
// \"k\" holds 5 members, limit 4". The member row is NOT committed. The live
// snapshot is returned ALONGSIDE the error so routing.Aggregator.Handle can
// still re-evaluate the release: the member is rejected, the release is not.
//
// CLASSIFICATION — the rejection is classified by CAUSE, from the group row's
// locked_by (Spec 017 §3.3.1):
//
//   - locked_by IS NULL, the group is NOT leased: nothing drains an unleased
//     group without an expiry cutoff (with no routing.WithGroupTimeout the
//     reaper's cutoff is zero and ExpiredGroups returns crashed-lease groups
//     only). The error is msgin.Permanent-wrapped, which the runtime settles
//     TERMINALLY — one attempt at the invalid-message sink, or the dead-letter
//     sink as a fallback, never a Nack. This is deliberate: a plain transient
//     rejection on the SHIPPED zero-value msgin.RetryPolicy (no MaxAttempts,
//     no Backoff) is an unlogged, zero-delay redelivery loop, and here each
//     iteration is a full rolled-back write transaction taking the group-row
//     lock plus a SchemaExists probe, multiplied by endpoint.WithConcurrency
//     — all contending on the very row a recovery would have to lock. With
//     NEITHER sink configured the runtime WARNs, naming both missing options,
//     and then ACKs — so the source DROPS the message. Configure
//     endpoint.WithInvalidMessageSink (or RetryPolicy.DeadLetter) to turn that
//     loss into a capture; the library cannot supply one.
//   - locked_by IS NOT NULL, the group IS leased: a claim is in flight and
//     Settle/Abandon runs on every release path, so the retry genuinely
//     succeeds afterwards. The error stays transient (unwrapped). Under the
//     zero-value RetryPolicy that retry is a busy-wait for the width of the
//     claim window; set RetryPolicy.Backoff if that matters.
//
// HOW LONG THE TRANSIENT ARM CAN LAST: normally one release round-trip. Its
// tail is a CRASHED releaser's lease, and that tail is UP TO 2 x leaseTTL —
// about 10 minutes at the shipped defaults, not 5. Two terms compose:
// ELIGIBILITY, at t0 + leaseTTL, when ExpiredGroups' locked_at <= now -
// leaseTTL arm first matches; and DISCOVERY, at the first reaper tick AT OR
// AFTER that moment. Aggregator.Run builds its ticker at Run's own start time
// on an interval that, with no WithGroupTimeout, equals RecoverInterval() —
// this store's lease TTL — so eligibility landing just after a tick waits a
// further full interval. Both terms presume go agg.Run(ctx) is running at
// all; without it the window has no upper bound (see GroupStore's "go
// agg.Run(ctx) is REQUIRED" section).
//
// A group that is full and unreleasable stays full: this option bounds
// growth, it does not provide liveness. Set routing.WithGroupTimeout to have
// the reaper expire such a group.
func WithMaxGroupMembers(n int) GroupStoreOption {
	return func(c *groupStoreConfig) { c.maxGroupMembers = n }
}

// GroupStore is a durable, multi-process-safe msgin.MessageGroupStore backed
// by a database/sql table pair (ADR 0021): correlation-keyed groups of held
// messages, with a store-level atomic lease-claim (GroupDialect.ClaimGroup)
// that makes an Aggregator's release exactly-once WITHIN and ACROSS processes.
// It is a WIRE store (EmitsLiveValue()==false): headers are JSON-framed
// (EncodeHeaders/DecodeHeaders) and payloads are the runtime-codec []byte
// body, identical to sql.Outbound / sql.QueueStore — the paired typed runtime
// encodes/decodes (ADR 0001).
//
// Delivery guarantee: at-least-once across restarts AND across processes. A
// crash between ClaimGroup and SettleGroup is recovered by lease expiry ->
// re-claim -> re-release (a duplicate emit, never a loss — ADR 0020 §8).
// Message ids are REQUIRED: Add rejects a message whose msgin.message-id is empty with
// ErrMissingMsgID (members are keyed (group_key, msg_id) for idempotent,
// redelivery-safe add — audit R1 H3). Source deliveries always carry
// msgin.HeaderMessageID and the Splitter stamps a deterministic child id, so this is
// not a real-world restriction.
//
// # go agg.Run(ctx) is REQUIRED for crash-recovery with a durable store
//
// RecoverInterval reports this store's configured lease TTL (not 0, unlike
// memory.GroupStore — audit R2 H-A), so an Aggregator built over a GroupStore
// only ever crash-recovers a stuck complete group (re-emitting it to the
// OUTPUT channel) if its reaper is actually running. A caller using
// sql.GroupStore for multi-process/crash safety MUST run go agg.Run(ctx),
// even with no WithGroupTimeout configured.
type GroupStore struct {
	groupBase
	leaseTTL        time.Duration
	lockedBy        string
	maxGroupMembers int
}

var _ msgin.MessageGroupStore = (*GroupStore)(nil)

// NewGroupStore builds a durable GroupStore over table on db, using dialect to
// generate the exact group-aggregation SQL (ADR 0021 §3 — dialect is a
// required, explicit constructor argument; there is no driver auto-detect,
// matching NewPollingSource/NewOutboundAdapter). table is the BASE identifier
// GroupDialect derives its two-table schema from (see GroupDialect's doc). A
// nil db is msgin.ErrNilAdapter; a bad table identifier is
// ErrInvalidTableName; a nil dialect is ErrNilDialect; an explicit non-positive
// WithGroupLeaseTTL is ErrInvalidLeaseTTL; a WithMaxGroupMembers outside
// [1, 1048576] is msgin.ErrInvalidCapacity. Call Ready/EnsureSchema once at
// boot, exactly like the Source (ADR 0010 D2) — msgin never runs DDL
// implicitly on the production path.
//
// A nil ELEMENT of opts is a bare [msgin.ErrNilFunc] naming the element's
// 0-based index ("sql.NewGroupStore: nil option at index 1"), not a panic —
// checked as opts is applied. In the order enumerated above the nil-option
// check comes FIRST: the apply loop is this constructor's first
// statement that can fail, preceded only by the config-defaults
// initializer, which cannot fail, so it runs before the nil-db,
// table-identifier, nil-dialect, WithGroupLeaseTTL and
// WithMaxGroupMembers checks, every one of which loses to it.
func NewGroupStore(db *stdsql.DB, table string, dialect GroupDialect, opts ...GroupStoreOption) (*GroupStore, error) {
	cfg := groupStoreConfig{logger: discardLogger(), maxGroupMembers: defaultMaxGroupMembers}
	for i, o := range opts {
		if o == nil {
			return nil, nilOptionAt("sql.NewGroupStore", i)
		}
		o(&cfg)
	}

	base, err := newGroupBase(db, table, dialect, cfg.logger)
	if err != nil {
		return nil, err
	}

	leaseTTL := defaultGroupLeaseTTL
	if cfg.leaseTTLSet {
		if cfg.leaseTTL <= 0 {
			return nil, fmt.Errorf("%w: %v", ErrInvalidLeaseTTL, cfg.leaseTTL)
		}
		leaseTTL = cfg.leaseTTL
	}

	if err := checkRange(msgin.ErrInvalidCapacity, "sql.WithMaxGroupMembers",
		cfg.maxGroupMembers, 1, maxGroupMembersCeiling); err != nil {
		return nil, err
	}

	lockedBy := cfg.lockedBy
	if lockedBy == "" {
		lockedBy = randomLockedBy()
	}

	return &GroupStore{
		groupBase:       base,
		leaseTTL:        leaseTTL,
		lockedBy:        lockedBy,
		maxGroupMembers: cfg.maxGroupMembers,
	}, nil
}

// Add durably appends msg to group key: it frames msg's headers
// (EncodeHeaders) and requires a []byte payload (ErrInvalidPayload otherwise —
// GroupStore is a wire adapter, mirroring Outbound.Send/SendAfter; the paired
// runtime always encodes T to []byte before Add is reached), rejects an empty
// msgin.message-id with ErrMissingMsgID BEFORE any query runs (H3, belt-and-suspenders
// with GroupDialect.AddMember's own check), and delegates to
// GroupDialect.AddMember on the pool, threading the configured
// WithMaxGroupMembers cap. It returns the resulting group snapshot of the LIVE
// (unclaimed) members, decoded from the dialect's raw framed bytes.
//
// # The member-cap rejection carries a snapshot
//
// When the dialect refuses the member because the group is at its cap it
// returns msgin.ErrOverflowDropped — msgin.Permanent-wrapped when the group is
// not leased, bare while a claim is in flight (WithMaxGroupMembers) — TOGETHER
// with the group's post-rollback live members. Add propagates BOTH, so
// routing.Aggregator.Handle can re-evaluate the release strategy against a
// group that is complete but was never re-triggered (Spec 017 §3.3a). Only the
// overflow rejection carries a snapshot; every other dialect failure keeps the
// (nil, err) shape, as does an overflow whose members cannot be decoded.
//
// The error still routes through classifyQueryErr, which costs one extra
// SchemaExists round-trip. Both the sentinel and the Permanent marker survive
// it while the table exists; a genuinely dropped table wins and surfaces as
// ErrSchemaNotReady, which is the correct diagnosis.
func (s *GroupStore) Add(ctx context.Context, key string, msg msgin.Message[any]) (msgin.MessageGroup, error) {
	msgID := msg.ID()
	if msgID == "" {
		return nil, ErrMissingMsgID
	}

	headers, err := EncodeHeaders(msg.Headers())
	if err != nil {
		return nil, err
	}

	payload, ok := msg.Payload().([]byte)
	if !ok {
		return nil, fmt.Errorf("%w: got %T", ErrInvalidPayload, msg.Payload())
	}

	var seq int64
	if n, ok := msg.Headers().Int(msgin.HeaderSequenceNumber); ok {
		seq = int64(n)
	}

	rows, err := s.dialect.AddMember(ctx, s.db, s.table, key, msgID, seq, headers, payload, s.maxGroupMembers)
	if err != nil {
		classified := s.classifyQueryErr(ctx, err)
		if !errors.Is(err, msgin.ErrOverflowDropped) {
			return nil, classified
		}
		snap, decodeErr := s.decodeGroupRows(rows)
		if decodeErr != nil {
			return nil, classified // a corrupt stored header must not mask the rejection
		}
		return snap, classified
	}
	return s.decodeGroupRows(rows)
}

// ClaimGroup atomically leases the group's current members via
// GroupDialect.ClaimGroup, passing this store's configured lockedBy and
// leaseTTL. It returns (nil, nil), with no error, when the dialect reports the
// group absent or actively (unexpired) leased by another holder — the caller
// then treats the group as held. A non-nil claim is decoded from the
// dialect's raw framed bytes, with Epoch wired from ClaimedGroup.Epoch.
func (s *GroupStore) ClaimGroup(ctx context.Context, key string) (msgin.MessageGroupClaim, error) {
	cg, err := s.dialect.ClaimGroup(ctx, s.db, s.table, key, s.lockedBy, s.leaseTTL)
	if err != nil {
		return nil, s.classifyQueryErr(ctx, err)
	}
	if cg == nil {
		return nil, nil
	}
	snap, err := s.decodeGroupRows(cg.GroupRows)
	if err != nil {
		return nil, err
	}
	return groupClaim{groupSnapshot: snap, epoch: cg.Epoch}, nil
}

// SettleGroup finalizes a successful release: it passes claim.Key(),
// s.lockedBy, and claim.Epoch() to GroupDialect.SettleGroup. A dialect
// applied=false (the fence missed — the lease was stolen or already settled)
// is NOT an error, matching the core msgin.MessageGroupStore contract
// (SettleGroup's fence-miss is a no-op).
func (s *GroupStore) SettleGroup(ctx context.Context, claim msgin.MessageGroupClaim) error {
	_, err := s.dialect.SettleGroup(ctx, s.db, s.table, claim.Key(), s.lockedBy, claim.Epoch())
	if err != nil {
		return s.classifyQueryErr(ctx, err)
	}
	return nil
}

// AbandonGroup releases claim's lease without deleting: it passes
// claim.Key(), s.lockedBy, and claim.Epoch() to GroupDialect.AbandonGroup. A
// dialect applied=false (fence miss) is NOT an error, matching the core
// contract.
func (s *GroupStore) AbandonGroup(ctx context.Context, claim msgin.MessageGroupClaim) error {
	_, err := s.dialect.AbandonGroup(ctx, s.db, s.table, claim.Key(), s.lockedBy, claim.Epoch())
	if err != nil {
		return s.classifyQueryErr(ctx, err)
	}
	return nil
}

// Expired returns the groups the Aggregator's reaper sweep must re-examine:
// GroupDialect.ExpiredGroups(before, s.leaseTTL, defaultExpiredGroupsLimit),
// decoded from raw framed bytes into []msgin.MessageGroup.
func (s *GroupStore) Expired(ctx context.Context, before time.Time) ([]msgin.MessageGroup, error) {
	rows, err := s.dialect.ExpiredGroups(ctx, s.db, s.table, before, s.leaseTTL, defaultExpiredGroupsLimit)
	if err != nil {
		return nil, s.classifyQueryErr(ctx, err)
	}
	out := make([]msgin.MessageGroup, 0, len(rows))
	for _, r := range rows {
		snap, err := s.decodeGroupRows(r)
		if err != nil {
			return nil, err
		}
		out = append(out, snap)
	}
	return out, nil
}

// RecoverInterval returns this store's configured lease TTL (default 5m —
// WithGroupLeaseTTL), NOT 0: unlike memory.GroupStore's unconditional
// in-process lease, a sql lease is TTL-bounded and can be stranded by a crash,
// so the Aggregator's reaper must sweep at this cadence to recover it (audit
// R2 H-A). See GroupStore's doc "go agg.Run(ctx) is REQUIRED" section.
func (s *GroupStore) RecoverInterval() time.Duration { return s.leaseTTL }

// EmitsLiveValue reports false: GroupStore is a wire store ([]byte payloads,
// JSON-framed headers) — the paired typed runtime encodes/decodes (ADR 0001).
func (s *GroupStore) EmitsLiveValue() bool { return false }

// decodeGroupRows decodes a GroupRows' raw framed member bytes into a
// groupSnapshot, mirroring how Source.pollLease builds a Message[any] from a
// ClaimedRow: DecodeHeaders reconstructs Headers, and the raw payload bytes
// become the Message[any]'s payload verbatim (the typed runtime decodes it
// downstream — EmitsLiveValue()==false). A member whose framed headers cannot
// be decoded surfaces as a wrapped error naming the offending message id.
//
// Decode is deliberately all-or-nothing per call: one corrupt stored header
// fails the whole Add/ClaimGroup/Expired operation rather than silently
// dropping just that member from the returned group — surfacing storage
// corruption beats hiding it behind an incomplete-but-successful result.
func (s *GroupStore) decodeGroupRows(rows GroupRows) (groupSnapshot, error) {
	msgs := make([]msgin.Message[any], 0, len(rows.Members))
	for _, m := range rows.Members {
		headers, err := DecodeHeaders(m.Headers)
		if err != nil {
			return groupSnapshot{}, fmt.Errorf("msgin/sql: decode group member %q: %w", m.MsgID, err)
		}
		msgs = append(msgs, msgin.NewMessage[any](m.Payload, headers))
	}
	return groupSnapshot{key: rows.GroupKey, msgs: msgs, createdAt: rows.CreatedAt}, nil
}

// groupSnapshot is an immutable msgin.MessageGroup view returned by
// Add/ClaimGroup/Expired, mirroring memory.GroupStore's snapshot type.
type groupSnapshot struct {
	key       string
	msgs      []msgin.Message[any]
	createdAt time.Time
}

func (s groupSnapshot) Key() string                    { return s.key }
func (s groupSnapshot) Messages() []msgin.Message[any] { return s.msgs }
func (s groupSnapshot) CreatedAt() time.Time           { return s.createdAt }

// groupClaim is a groupSnapshot plus a fence epoch, implementing
// msgin.MessageGroupClaim (mirrors memory.GroupStore's claimGroup type).
type groupClaim struct {
	groupSnapshot
	epoch int64
}

func (c groupClaim) Epoch() int64 { return c.epoch }
