package sql

import (
	"context"
	stdsql "database/sql"
	"fmt"

	msgin "github.com/kartaladev/msgin"
)

// Compile-time assertion that Outbound satisfies msgin.OutboundAdapter.
//
// Outbound is a WIRE adapter: it deliberately does NOT implement
// msgin.LiveValueSource, so msgin.NewProducer always JSON-encodes the payload
// to []byte before calling Send (ADR 0010 D8). Go has no negative-interface
// compile check, so that property cannot be asserted here — it is enforced by
// the runtime test TestOutbound_NotLiveValueSource.
var _ msgin.OutboundAdapter = (*Outbound)(nil)

// Outbound is the sql INSERT outbound channel adapter: it writes a message as
// a new, immediately-visible row (visible_after = now(), delay 0). Paired with
// a Task-5 Source polling the SAME table, it forms a durable, at-least-once
// produce/consume queue; Outbound is also usable directly as a
// msgin.RetryPolicy.DeadLetter sink or a msgin.WithInvalidMessageSink target,
// since both are just an OutboundAdapter (but see WithSharedTransaction's
// DLQ-sink caveat before using a strict shared-tx Outbound that way).
type Outbound struct {
	adapterBase

	txResolver TransactionResolver // nil ⇒ no shared-tx option was given; always insert on the pool db
	txStrict   bool                // meaningful only when txResolver != nil (ADR 0010 D8)
}

// NewOutboundAdapter builds an Outbound over table on db, using dialect to
// generate the exact SQL (ADR 0011 — the dialect is a required, explicit
// constructor argument; there is no driver auto-detect), the same explicit
// argument NewPollingSource takes. It validates the table identifier
// (ErrInvalidTableName). WithLeaseTTL/WithLockedBy are lease-Source-specific
// and are simply inert here. A nil db is msgin.ErrNilAdapter; a nil dialect is
// ErrNilDialect. A nil TransactionResolver passed to
// WithSharedTransaction/WithOpportunisticSharedTransaction is a construction
// error (ErrNilResolver), never a deferred nil-func panic on the first Send.
//
// The returned Outbound implements msgin.OutboundAdapter; pass it to
// msgin.NewProducer, msgin.RetryPolicy.DeadLetter, or
// msgin.WithInvalidMessageSink. Call Ready once at boot to fail fast on an
// un-provisioned schema (ADR 0010 D2).
func NewOutboundAdapter(db *stdsql.DB, table string, dialect LeaseDialect, opts ...Option) (*Outbound, error) {
	cfg := config{logger: discardLogger()}
	for _, o := range opts {
		o(&cfg)
	}

	if cfg.txResolverSet && cfg.txResolver == nil {
		return nil, ErrNilResolver
	}

	base, err := newAdapterBase(db, table, dialect, cfg)
	if err != nil {
		return nil, err
	}

	return &Outbound{adapterBase: base, txResolver: cfg.txResolver, txStrict: cfg.txStrict}, nil
}

// Send frames msg's headers (EncodeHeaders) and INSERTs a new,
// immediately-visible row (delay 0) via the resolved LeaseDialect. msg's payload
// MUST be []byte — Outbound is a wire adapter (not a LiveValueSource), so
// msgin.NewProducer always JSON-encodes T to []byte before calling Send; a
// non-[]byte payload here is a defensive case (ErrInvalidPayload) that trusted
// producers do not hit.
//
// With no shared-transaction option configured, Send inserts on the pool db,
// unchanged from the plain wire-adapter behavior. With WithSharedTransaction
// or WithOpportunisticSharedTransaction configured, Send resolves the
// caller's transaction from ctx and applies the ADR 0010 D8 policy — see
// resolveQuerier.
//
// An INSERT failure is wrapped as ErrSchemaNotReady iff a follow-up portable
// probe finds the table missing, otherwise the raw error propagates.
func (o *Outbound) Send(ctx context.Context, msg msgin.Message[any]) error {
	msgID := msg.ID()

	headers, err := EncodeHeaders(msg.Headers())
	if err != nil {
		return err
	}

	payload, ok := msg.Payload().([]byte)
	if !ok {
		return fmt.Errorf("%w: got %T", ErrInvalidPayload, msg.Payload())
	}

	q, err := o.resolveQuerier(ctx, msgID)
	if err != nil {
		return err
	}

	if err := o.dialect.Insert(ctx, q, o.table, msgID, headers, payload, 0); err != nil {
		return o.classifyQueryErr(ctx, err)
	}
	return nil
}

// resolveQuerier picks the Querier Send inserts on (ADR 0010 D8):
//
//   - No shared-tx option configured (txResolver == nil): the pool db,
//     unchanged from the Task-6 plain-Outbound behavior.
//   - A resolver is configured: q, err := txResolver(ctx).
//   - err != nil: wrapped and returned; Send attempts no insert.
//   - q != nil: the caller's transaction — BORROWED. Send only ever issues
//     the INSERT on it; it never calls Commit or Rollback (the caller
//     owns the transaction's lifecycle).
//   - q == nil (no shared transaction present in ctx): strict mode
//     (WithSharedTransaction) refuses the dual-write and returns
//     ErrNoSharedTransaction; opportunistic mode
//     (WithOpportunisticSharedTransaction) logs the fallback at WARN (message
//     id only, never the payload) and falls back to the pool db.
func (o *Outbound) resolveQuerier(ctx context.Context, msgID string) (Querier, error) {
	if o.txResolver == nil {
		return o.db, nil
	}

	q, err := o.txResolver(ctx)
	if err != nil {
		return nil, fmt.Errorf("msgin/sql: resolving shared transaction: %w", err)
	}
	if q != nil {
		return q, nil
	}

	if o.txStrict {
		return nil, ErrNoSharedTransaction
	}

	o.logger.Warn("msgin/sql: no shared transaction in context; standalone insert — atomicity NOT achieved",
		"id", msgID)
	return o.db, nil
}
