package sql

import (
	"context"
	stdsql "database/sql"
	"fmt"
	"time"

	msgin "github.com/kartaladev/msgin"
)

// Corrupt-row penalty backoff (ADR 0010 D4, coordinator review). A row whose
// framed headers cannot be decoded (a corrupt or foreign row) is never dropped
// — at-least-once forbids it — but it also never reaches the runtime's
// invalid-sink, because Poll drops it before building a Delivery. So the Source
// must self-throttle it: instead of a delay-0 Nack (immediately re-claimable →
// churns the DB and spams the ERROR log every poll interval, forever), it Nacks
// with an ESCALATING penalty keyed off the row's own delivery_count (a separate
// column, still readable when the headers blob is corrupt). Each failed claim
// bumps delivery_count, so the penalty grows 30s → 1m → 2m → … capped at 1h,
// pushing the stuck row out of the hot claim window while good rows keep
// flowing (ORDER BY visible_after). This is SPECIFIC to the corrupt-row path;
// the normal nackClosure requeue=false→delay-0 collapse is a different, correct
// contract and stays unchanged.
const (
	corruptRowInitialBackoff = 30 * time.Second
	corruptRowMaxBackoff     = 1 * time.Hour
)

// Compile-time assertions: a Source is a pull inbound adapter with native
// redelivery (the row persists and is re-leased) but no native dead-letter (the
// runtime's RetryPolicy governs dead-lettering).
var (
	_ msgin.PollingSource     = (*Source)(nil)
	_ msgin.NativeReliability = (*Source)(nil)
)

// Source is the sql lease/claim inbound channel adapter: a durable,
// at-least-once message queue backed by a table, driven by the runtime's
// Poller. Each Poll leases a batch of claimable rows (bumping delivery_count
// and a fence token), and each delivery's Ack/Nack settles its row fenced on
// that token, so a lease that expired and was re-claimed by another worker
// cannot be settled twice.
//
// # requeue=false collapses to a delayed requeue
//
// An at-least-once source cannot silently drop a message, so a delivery's
// Nack(ctx, requeue=false, delay) is treated as Nack(ctx, requeue=true, 0): the
// row is made immediately claimable again rather than discarded. The runtime
// only ever sends the pull path requeue=true, so this collapse is a safety net,
// not a hot path (ADR 0010 D4).
//
// # Payload size cap
//
// The Source does not cap payload size; decoding is a runtime concern (the
// adapter never sees the typed payload). Pass WithMaxPayloadBytes to
// NewConsumer — RecommendedMaxPayloadBytes is a copy-pasteable starting value
// (ADR 0010 D7).
type Source struct {
	adapterBase
	strategy Strategy
	leaseTTL time.Duration
	lockedBy string
	// lockDialect is the resolved LockDialect, non-nil only under
	// StrategyLockForUpdate (asserted at construction). The lease path never
	// touches it.
	lockDialect LockDialect
}

// NewPollingSource builds a lease/claim Source over table on db, using dialect
// to generate the exact SQL (ADR 0011 — the dialect is a required, explicit
// constructor argument; there is no driver auto-detect). Pass
// postgres.LeaseDialect() (adapter/database/sql/postgres), mysql.LeaseDialect()
// (adapter/database/sql/mysql), or your own LeaseDialect implementation. It
// validates the table identifier (ErrInvalidTableName), and
// applies the sensible defaults documented on each option (5m lease TTL, a
// random lease owner, a discard logger). A nil db is msgin.ErrNilAdapter; a nil
// dialect is ErrNilDialect.
//
// The returned Source implements msgin.PollingSource and
// msgin.NativeReliability; pass it to msgin.NewConsumer. Call Ready once at boot
// to fail fast on an un-provisioned schema (ADR 0010 D2), and prefer a non-zero
// RetryPolicy.Backoff so a repeatedly-failing row idles the poll loop instead of
// hot-looping the DB (ADR 0010 D1 poison-recycle caveat).
func NewPollingSource(db *stdsql.DB, table string, dialect LeaseDialect, opts ...Option) (*Source, error) {
	cfg := config{logger: discardLogger()}
	for _, o := range opts {
		o(&cfg)
	}

	base, err := newAdapterBase(db, table, dialect, cfg)
	if err != nil {
		return nil, err
	}

	if cfg.strategy != StrategyLeaseClaim && cfg.strategy != StrategyLockForUpdate {
		return nil, fmt.Errorf("%w: %d", ErrInvalidStrategy, cfg.strategy)
	}

	// StrategyLockForUpdate requires the resolved LeaseDialect to also satisfy the
	// segregated LockDialect SPI (ADR 0010 D5). Resolve it once at construction so
	// pollLock never type-asserts on the hot path and a lease-only dialect fails
	// fast rather than at first poll.
	var lockDialect LockDialect
	if cfg.strategy == StrategyLockForUpdate {
		ld, ok := base.dialect.(LockDialect)
		if !ok {
			return nil, fmt.Errorf("%w: dialect %T", ErrLockStrategyUnsupported, base.dialect)
		}
		lockDialect = ld
	}

	leaseTTL := defaultLeaseTTL
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

	return &Source{
		adapterBase: base,
		strategy:    cfg.strategy,
		leaseTTL:    leaseTTL,
		lockedBy:    lockedBy,
		lockDialect: lockDialect,
	}, nil
}

// Poll fetches up to max claimable rows as settleable deliveries
// (msgin.PollingSource), dispatching on the configured Strategy: the default
// lease/claim path (pollLease) or the lock/FOR UPDATE path (pollLock). Either way
// it upholds the Poll contract: at most max deliveries, and none alongside a
// non-nil error (each strategy owns rollback of its claim work on the error
// path). A claim query error is surfaced as ErrSchemaNotReady iff a follow-up
// portable probe finds the table missing, otherwise the raw error propagates to
// the poll-loop backoff.
func (s *Source) Poll(ctx context.Context, max int) ([]msgin.Delivery, error) {
	if s.strategy == StrategyLockForUpdate {
		return s.pollLock(ctx, max)
	}
	return s.pollLease(ctx, max)
}

// pollLease is the lease/claim strategy Poll (ADR 0010 D4): it leases up to max
// claimable rows in a short committed tx and returns them as fenced deliveries.
//
// A row whose framed headers cannot be decoded (a corrupt or foreign row — a
// defensive case trusted producers do not hit) is logged, has its lease
// released so at-least-once does not lose it, and is skipped, rather than
// failing the whole batch.
func (s *Source) pollLease(ctx context.Context, max int) ([]msgin.Delivery, error) {
	rows, err := s.dialect.Claim(ctx, s.db, s.table, max, s.lockedBy, s.leaseTTL)
	if err != nil {
		return nil, s.classifyQueryErr(ctx, err)
	}

	out := make([]msgin.Delivery, 0, len(rows))
	for _, row := range rows {
		headers, derr := DecodeHeaders(row.Headers)
		if derr != nil {
			// Corrupt/foreign row (defensive): release its lease so at-least-once
			// does not strand it, and exclude it from the batch. Do NOT fail the
			// whole Poll — one bad row must not stall every good one. The Nack
			// carries an ESCALATING penalty (not delay 0) keyed off the row's own
			// delivery_count, so a row that can never be decoded is pushed out of
			// the hot claim window instead of churning the DB / spamming this log
			// every poll interval (the log is thereby naturally throttled to the
			// penalty cadence). See the corruptRow*Backoff consts.
			penalty := msgin.ExponentialBackoff{
				Initial: corruptRowInitialBackoff,
				Max:     corruptRowMaxBackoff,
				Mult:    2,
			}.Delay(row.DeliveryCount - 1) // 0-based: first corrupt claim → Initial
			s.logger.Error("msgin/sql: skipping row with undecodable headers; penalizing its lease",
				"table", s.table, "id", row.ID, "delivery_count", row.DeliveryCount, "penalty", penalty, "err", derr)
			if _, nerr := s.dialect.Nack(ctx, s.db, s.table, row.ID, s.lockedBy, row.LeaseEpoch, penalty); nerr != nil {
				s.logger.Error("msgin/sql: failed to penalize lease on undecodable row",
					"table", s.table, "id", row.ID, "err", nerr)
			}
			continue
		}

		// Reconstruct the persisted envelope verbatim (no id/timestamp re-stamp),
		// then overlay the LIVE claim's delivery_count so the runtime's native
		// attempt count is driven by the durable row, not the ephemeral tracker.
		msg := msgin.NewMessage[any](row.Payload, headers).
			WithHeader(msgin.HeaderDeliveryCount, row.DeliveryCount)

		out = append(out, msgin.Delivery{
			Msg:  msg,
			Ack:  s.ackClosure(row.ID, row.LeaseEpoch),
			Nack: s.nackClosure(row.ID, row.LeaseEpoch),
		})
	}
	return out, nil
}

// pollLock is the lock/FOR UPDATE strategy Poll (ADR 0010 D5). It claims at most
// ONE row (each in-flight lock message pins one pooled connection), regardless of
// max — the poll loop releases the surplus credits. The claim tx is begun on a
// cancellation-DETACHED context (context.WithoutCancel) so graceful shutdown
// (parent-cancel) does NOT auto-rollback in-flight lock txs before the drain
// deadline (HIGH 3): the tx lives until the worker settles (Ack/Nack on the
// runtime's settleCtx) or the drain-deadline Nack closes it.
//
// A corrupt/foreign row (undecodable headers) is NackLocked — which commits and
// closes its carried tx (no connection leak) with an escalating penalty keyed off
// delivery_count, the same throttle the lease path uses — then skipped (returns
// empty), never delivered.
func (s *Source) pollLock(ctx context.Context, max int) ([]msgin.Delivery, error) {
	// Detached context: the claim tx must survive parent-cancel so in-flight work
	// finishes within the drain deadline (ADR 0010 D5, HIGH 3). ClaimLock owns
	// rolling this tx back on any error/empty path (no partial result, no leak).
	lr, err := s.lockDialect.ClaimLock(context.WithoutCancel(ctx), s.db, s.table, s.lockedBy)
	if err != nil {
		return nil, s.classifyQueryErr(ctx, err)
	}
	if lr == nil {
		return nil, nil // nothing claimable (tx already rolled back by ClaimLock)
	}

	headers, derr := DecodeHeaders(lr.Headers)
	if derr != nil {
		// Corrupt/foreign row (defensive): NackLock it to COMMIT/close the carried
		// tx (never leak the connection) with an escalating penalty so a row that
		// can never decode is pushed out of the hot claim window instead of churning
		// the DB / spamming this log. Then skip it (empty batch). Same policy as the
		// lease path's corrupt-row Nack.
		penalty := msgin.ExponentialBackoff{
			Initial: corruptRowInitialBackoff,
			Max:     corruptRowMaxBackoff,
			Mult:    2,
		}.Delay(lr.DeliveryCount - 1) // 0-based: first corrupt claim → Initial
		s.logger.Error("msgin/sql: skipping row with undecodable headers; penalizing and releasing its lock",
			"table", s.table, "id", lr.ID, "delivery_count", lr.DeliveryCount, "penalty", penalty, "err", derr)
		// NackLock runs on the parent poll ctx (not detached/settleCtx) by design:
		// on shutdown mid-poll the penalty UPDATE rolls back rather than commits,
		// which is SAFE — the FOR UPDATE lock is released, the delivery_count++ is
		// reverted, and the row is re-claimable (and re-penalized) next run. No
		// connection leak, no message loss.
		if nerr := s.lockDialect.NackLock(ctx, lr, s.table, penalty); nerr != nil {
			s.logger.Error("msgin/sql: failed to penalize/close the tx on an undecodable lock row",
				"table", s.table, "id", lr.ID, "err", nerr)
		}
		return nil, nil
	}

	// Reconstruct the persisted envelope verbatim, overlaying the live claim's
	// delivery_count (same as the lease path) so the runtime's native attempt
	// count is driven by the durable row.
	msg := msgin.NewMessage[any](lr.Payload, headers).
		WithHeader(msgin.HeaderDeliveryCount, lr.DeliveryCount)

	return []msgin.Delivery{{
		Msg:  msg,
		Ack:  s.ackLockClosure(lr),
		Nack: s.nackLockClosure(lr),
	}}, nil
}

// ackLockClosure returns the lock-strategy Ack for lr: it DELETEs the row and
// commits the carried tx (AckLock). There is no fence/ErrStaleLease — the open
// FOR UPDATE tx exclusively owns the row, so the settle always applies.
func (s *Source) ackLockClosure(lr *LockedRow) func(context.Context) error {
	return func(ctx context.Context) error {
		return s.lockDialect.AckLock(ctx, lr, s.table)
	}
}

// nackLockClosure returns the lock-strategy Nack for lr: it clears the lock,
// pushes visible_after out by delay, and ALWAYS commits (NackLock), persisting
// the delivery_count++. As on the lease path, requeue=false collapses to a
// delayed requeue with delay 0 (an at-least-once source cannot drop).
func (s *Source) nackLockClosure(lr *LockedRow) func(context.Context, bool, time.Duration) error {
	return func(ctx context.Context, requeue bool, delay time.Duration) error {
		d := delay
		if !requeue {
			d = 0 // requeue=false ≡ requeue=true with delay 0 (ADR 0010 D4/D5)
		}
		return s.lockDialect.NackLock(ctx, lr, s.table, d)
	}
}

// ackClosure returns the fenced Ack for the row leased at (id, epoch): it
// deletes the row only if the lease still holds. A fence miss (applied=false)
// means the handler outran the lease TTL and another worker re-claimed the row;
// it returns ErrStaleLease (non-nil) so the runtime suppresses the phantom
// OnAck/eviction for a settle this worker did not actually perform — the row is
// owned by the worker that re-leased it and will be settled there (ADR 0010 D4).
func (s *Source) ackClosure(id, epoch int64) func(context.Context) error {
	return func(ctx context.Context) error {
		applied, err := s.dialect.Ack(ctx, s.db, s.table, id, s.lockedBy, epoch)
		if err != nil {
			return err
		}
		if !applied {
			s.logger.Warn("msgin/sql: Ack found no row; lease was reclaimed by another worker (stale)",
				"table", s.table, "id", id)
			return ErrStaleLease
		}
		return nil
	}
}

// nackClosure returns the fenced Nack for the row leased at (id, epoch). Because
// an at-least-once source cannot drop a message, requeue=false collapses to a
// delayed requeue with delay 0 (immediate re-visibility). A fence miss returns
// ErrStaleLease, symmetric to Ack.
func (s *Source) nackClosure(id, epoch int64) func(context.Context, bool, time.Duration) error {
	return func(ctx context.Context, requeue bool, delay time.Duration) error {
		d := delay
		if !requeue {
			// requeue=false ≡ requeue=true with delay 0 (ADR 0010 D4).
			d = 0
		}
		applied, err := s.dialect.Nack(ctx, s.db, s.table, id, s.lockedBy, epoch, d)
		if err != nil {
			return err
		}
		if !applied {
			s.logger.Warn("msgin/sql: Nack found no row; lease was reclaimed by another worker (stale)",
				"table", s.table, "id", id)
			return ErrStaleLease
		}
		return nil
	}
}

// NativeRedelivery reports true: a nacked or lease-expired row persists and is
// re-leased by a later poll, so the runtime relies on the DB for redelivery
// rather than its in-process attempt tracker.
func (s *Source) NativeRedelivery() bool { return true }

// NativeDeadLetter reports false: the Source has no built-in dead-letter, so the
// runtime's RetryPolicy (MaxAttempts -> DeadLetter sink) governs dead-lettering.
func (s *Source) NativeDeadLetter() bool { return false }

// EnsureSchema and Ready are promoted from adapterBase (shared with Outbound):
// see adapterBase.EnsureSchema and adapterBase.Ready.
