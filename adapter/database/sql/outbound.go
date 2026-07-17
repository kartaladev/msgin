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
// since both are just an OutboundAdapter.
type Outbound struct {
	adapterBase
}

// NewOutboundAdapter builds an Outbound over table on db. It resolves the
// Dialect (WithDialect, else driver auto-detect, else ErrDialectUndetected —
// ADR 0010 D3) and validates the table identifier (ErrInvalidTableName), the
// same resolution NewPollingSource applies. WithLeaseTTL/WithLockedBy are
// lease-Source-specific and are simply inert here. A nil db is
// msgin.ErrNilAdapter.
//
// The returned Outbound implements msgin.OutboundAdapter; pass it to
// msgin.NewProducer, msgin.RetryPolicy.DeadLetter, or
// msgin.WithInvalidMessageSink. Call Ready once at boot to fail fast on an
// un-provisioned schema (ADR 0010 D2).
func NewOutboundAdapter(db *stdsql.DB, table string, opts ...Option) (*Outbound, error) {
	cfg := config{logger: discardLogger()}
	for _, o := range opts {
		o(&cfg)
	}

	base, err := newAdapterBase(db, table, cfg)
	if err != nil {
		return nil, err
	}

	return &Outbound{adapterBase: base}, nil
}

// Send frames msg's headers (EncodeHeaders) and INSERTs a new,
// immediately-visible row (delay 0) via the resolved Dialect. msg's payload
// MUST be []byte — Outbound is a wire adapter (not a LiveValueSource), so
// msgin.NewProducer always JSON-encodes T to []byte before calling Send; a
// non-[]byte payload here is a defensive case (ErrInvalidPayload) that trusted
// producers do not hit. An INSERT failure is wrapped as ErrSchemaNotReady iff
// a follow-up portable probe finds the table missing, otherwise the raw error
// propagates.
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

	if err := o.dialect.Insert(ctx, o.db, o.table, msgID, headers, payload, 0); err != nil {
		return o.classifyQueryErr(ctx, err)
	}
	return nil
}
