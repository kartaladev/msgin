package sql

import (
	"context"
	stdsql "database/sql"
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
	leaseTTL    time.Duration
	leaseTTLSet bool // distinguishes explicit WithGroupLeaseTTL(0) (rejected) from unset (default)
	lockedBy    string
	logger      *slog.Logger
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
// Message ids are REQUIRED: Add rejects a message whose msgin.id is empty with
// ErrMissingMsgID (members are keyed (group_key, msg_id) for idempotent,
// redelivery-safe add — audit R1 H3). Source deliveries always carry
// msgin.HeaderID and the Splitter stamps a deterministic child id, so this is
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
	leaseTTL time.Duration
	lockedBy string
}

var _ msgin.MessageGroupStore = (*GroupStore)(nil)

// NewGroupStore builds a durable GroupStore over table on db, using dialect to
// generate the exact group-aggregation SQL (ADR 0021 §3 — dialect is a
// required, explicit constructor argument; there is no driver auto-detect,
// matching NewPollingSource/NewOutboundAdapter). table is the BASE identifier
// GroupDialect derives its two-table schema from (see GroupDialect's doc). A
// nil db is msgin.ErrNilAdapter; a bad table identifier is
// ErrInvalidTableName; a nil dialect is ErrNilDialect; an explicit non-positive
// WithGroupLeaseTTL is ErrInvalidLeaseTTL. Call Ready/EnsureSchema once at
// boot, exactly like the Source (ADR 0010 D2) — msgin never runs DDL
// implicitly on the production path.
func NewGroupStore(db *stdsql.DB, table string, dialect GroupDialect, opts ...GroupStoreOption) (*GroupStore, error) {
	cfg := groupStoreConfig{logger: discardLogger()}
	for _, o := range opts {
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

	lockedBy := cfg.lockedBy
	if lockedBy == "" {
		lockedBy = randomLockedBy()
	}

	return &GroupStore{groupBase: base, leaseTTL: leaseTTL, lockedBy: lockedBy}, nil
}

// Add durably appends msg to group key: it frames msg's headers
// (EncodeHeaders) and requires a []byte payload (ErrInvalidPayload otherwise —
// GroupStore is a wire adapter, mirroring Outbound.Send/SendAfter; the paired
// runtime always encodes T to []byte before Add is reached), rejects an empty
// msgin.id with ErrMissingMsgID BEFORE any query runs (H3, belt-and-suspenders
// with GroupDialect.AddMember's own check), and delegates to
// GroupDialect.AddMember on the pool. It returns the resulting group snapshot
// of the LIVE (unclaimed) members, decoded from the dialect's raw framed
// bytes.
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

	rows, err := s.dialect.AddMember(ctx, s.db, s.table, key, msgID, seq, headers, payload)
	if err != nil {
		return nil, s.classifyQueryErr(ctx, err)
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
