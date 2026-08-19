package sql

import (
	"context"
	stdsql "database/sql"

	msgin "github.com/kartaladev/msgin"
)

// QueueStore is a durable, at-least-once msgin.ChannelStore backed by a single
// table. It is a thin facade pairing an Outbound (Enqueue = INSERT) with a Source
// (Claim = lease/claim, fenced Ack/Nack) over the SAME table — reusing the proven
// sql adapter machinery (ADR 0010/0011). Carries []byte (a wire store): the
// paired Producer JSON-encodes T, exactly as sql.Outbound. Named to mirror
// memory.QueueStore.
//
// Delivery guarantee: at-least-once across restarts/crashes (Guaranteed
// Delivery). Back a channel.QueueChannel with this store when the flow segment must
// survive a restart.
type QueueStore struct {
	out *Outbound
	src *Source
}

var (
	_ msgin.ChannelStore      = (*QueueStore)(nil)
	_ msgin.NativeReliability = (*QueueStore)(nil)
)

// NewQueueStore builds a durable QueueStore over table on db with dialect. Options
// apply to both halves; lease-only options (WithLeaseTTL/WithStrategy/WithLockedBy)
// are inert on the Outbound and shared-tx options are inert on the Source, so there
// is no cross-misconfiguration (verified: options.go). A nil db is
// msgin.ErrNilAdapter; a nil dialect is ErrNilDialect; a bad table is
// ErrInvalidTableName. Call Ready once at boot (ADR 0010 D2).
//
// A nil ELEMENT of opts is a bare [msgin.ErrNilFunc] naming the element's
// 0-based index ("sql.NewQueueStore: nil option at index 1"), not a panic. It is
// checked HERE, by a standalone pre-check above the delegation, so the position
// names sql.NewQueueStore rather than one of the two constructors it forwards to
// — a function the caller never invoked (Spec 015 §3.4). The pre-check is the
// function's first statement, so the nil-option check runs BEFORE the nil-db,
// nil-dialect and table checks the delegates perform, and wins over all of them.
func NewQueueStore(db *stdsql.DB, table string, dialect LeaseDialect, opts ...Option) (*QueueStore, error) {
	// Delegator pre-check (Spec 015 §3.4, ADR 0031 D-R): both delegates
	// re-scan opts and find nothing. The duplicated pass is deliberate — it
	// buys a truthful position at this entry point.
	for i, opt := range opts {
		if opt == nil {
			return nil, nilOptionAt("sql.NewQueueStore", i)
		}
	}

	out, err := NewOutboundAdapter(db, table, dialect, opts...)
	if err != nil {
		return nil, err
	}
	src, err := NewPollingSource(db, table, dialect, opts...)
	if err != nil {
		return nil, err
	}
	return &QueueStore{out: out, src: src}, nil
}

// Enqueue INSERTs msg as an immediately-visible row.
func (q *QueueStore) Enqueue(ctx context.Context, msg msgin.Message[any]) error {
	return q.out.Send(ctx, msg)
}

// Claim leases up to max claimable rows as settleable Deliveries.
func (q *QueueStore) Claim(ctx context.Context, max int) ([]msgin.Delivery, error) {
	return q.src.Poll(ctx, max)
}

// EmitsLiveValue reports false: this is a wire store ([]byte payloads).
func (q *QueueStore) EmitsLiveValue() bool { return false }

// NativeRedelivery / NativeDeadLetter forward the Source's capability (true /
// false): the DB row redelivers on nack/lease-expiry, and dead-lettering is the
// runtime's RetryPolicy. Forwarded so a QueueChannel over this store behaves
// identically to the raw Source.
func (q *QueueStore) NativeRedelivery() bool { return q.src.NativeRedelivery() }
func (q *QueueStore) NativeDeadLetter() bool { return q.src.NativeDeadLetter() }

// Ready / EnsureSchema forward to the Source (both halves share one table, so
// either delegate suffices). Explicit forwarders — named fields do not promote.
func (q *QueueStore) Ready(ctx context.Context) error        { return q.src.Ready(ctx) }
func (q *QueueStore) EnsureSchema(ctx context.Context) error { return q.src.EnsureSchema(ctx) }
