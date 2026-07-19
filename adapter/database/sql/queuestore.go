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
// Delivery). Back a msgin.QueueChannel with this store when the flow segment must
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
func NewQueueStore(db *stdsql.DB, table string, dialect LeaseDialect, opts ...Option) (*QueueStore, error) {
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
