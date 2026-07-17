package sql

import (
	"context"
	stdsql "database/sql"
	"fmt"
	"log/slog"
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
	db       *stdsql.DB
	table    string
	dialect  Dialect
	leaseTTL time.Duration
	lockedBy string
	logger   *slog.Logger
}

// NewPollingSource builds a lease/claim Source over table on db. It resolves
// the Dialect (WithDialect, else driver auto-detect, else ErrDialectUndetected
// — ADR 0010 D3), validates the table identifier (ErrInvalidTableName), and
// applies the sensible defaults documented on each option (5m lease TTL, a
// random lease owner, a discard logger). A nil db is msgin.ErrNilAdapter.
//
// The returned Source implements msgin.PollingSource and
// msgin.NativeReliability; pass it to msgin.NewConsumer. Call Ready once at boot
// to fail fast on an un-provisioned schema (ADR 0010 D2), and prefer a non-zero
// RetryPolicy.Backoff so a repeatedly-failing row idles the poll loop instead of
// hot-looping the DB (ADR 0010 D1 poison-recycle caveat).
func NewPollingSource(db *stdsql.DB, table string, opts ...Option) (*Source, error) {
	if db == nil {
		return nil, msgin.ErrNilAdapter
	}
	if err := validateIdent(table); err != nil {
		return nil, err
	}

	cfg := config{logger: discardLogger()}
	for _, o := range opts {
		o(&cfg)
	}

	leaseTTL := defaultLeaseTTL
	if cfg.leaseTTLSet {
		if cfg.leaseTTL <= 0 {
			return nil, fmt.Errorf("%w: %v", ErrInvalidLeaseTTL, cfg.leaseTTL)
		}
		leaseTTL = cfg.leaseTTL
	}

	dialect := cfg.dialect
	if dialect == nil {
		d, err := resolveDialect(db)
		if err != nil {
			return nil, err
		}
		dialect = d
	}

	lockedBy := cfg.lockedBy
	if lockedBy == "" {
		lockedBy = randomLockedBy()
	}

	return &Source{
		db:       db,
		table:    table,
		dialect:  dialect,
		leaseTTL: leaseTTL,
		lockedBy: lockedBy,
		logger:   cfg.logger,
	}, nil
}

// Poll leases up to max claimable rows and returns them as settleable
// deliveries (msgin.PollingSource). It upholds the Poll contract: at most max
// deliveries, and none alongside a non-nil error. A claim query error is
// surfaced as ErrSchemaNotReady iff a follow-up portable probe finds the table
// missing (a table dropped mid-run is diagnosed clearly, no driver import),
// otherwise the raw error propagates to the poll-loop backoff.
//
// A row whose framed headers cannot be decoded (a corrupt or foreign row — a
// defensive case trusted producers do not hit) is logged, has its lease
// released so at-least-once does not lose it, and is skipped, rather than
// failing the whole batch.
func (s *Source) Poll(ctx context.Context, max int) ([]msgin.Delivery, error) {
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

// EnsureSchema idempotently creates the table and its claim index. It is
// optional and opt-in (dev/test/opt-in callers); production callers provision
// the schema via the reference DDL (see PostgresDDL) instead — msgin never runs
// DDL implicitly (ADR 0010 D2).
func (s *Source) EnsureSchema(ctx context.Context) error {
	return s.dialect.EnsureSchema(ctx, s.db, s.table)
}

// Ready is the fail-fast boot check (ADR 0010 D2): it returns ErrSchemaNotReady
// (naming the table) when the table is not initialized, so a forgotten
// migration fails the deploy immediately instead of the consumer sitting in a
// silent poll-error loop. Call it once at startup before NewConsumer.Run.
func (s *Source) Ready(ctx context.Context) error {
	exists, err := s.dialect.SchemaExists(ctx, s.db, s.table)
	if err != nil {
		return err
	}
	if !exists {
		return s.schemaNotReady()
	}
	return nil
}

// classifyQueryErr wraps a Claim/query failure as ErrSchemaNotReady iff a
// follow-up portable probe reports the table missing (diagnosing a table
// dropped mid-run without a driver import); otherwise the raw error propagates.
func (s *Source) classifyQueryErr(ctx context.Context, err error) error {
	if exists, probeErr := s.dialect.SchemaExists(ctx, s.db, s.table); probeErr == nil && !exists {
		return s.schemaNotReady()
	}
	return err
}

// schemaNotReady builds the ErrSchemaNotReady error naming the table.
func (s *Source) schemaNotReady() error {
	return fmt.Errorf("%w: table %q not initialized; run EnsureSchema or apply the DDL",
		ErrSchemaNotReady, s.table)
}
