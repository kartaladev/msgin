package msgin

import "context"

// ChannelStore is the swappable buffer behind a QueueChannel: a durable FIFO
// with lease/claim settlement. It is the extension seam that makes a queue
// channel's buffer in-memory or persistent. Implementations live in adapter
// packages (adapter/memory, adapter/database/sql); the core never imports them.
//
// A future id-addressable MessageStore or group-keyed MessageGroupStore can be
// added later as a separate interface this one embeds — a non-breaking change
// for existing implementers (ADR 0018, Spec 007 D3).
type ChannelStore interface {
	// Enqueue durably appends msg to the tail of the queue.
	Enqueue(ctx context.Context, msg Message[any]) error
	// Claim leases up to max ready messages in FIFO order and returns them as
	// settleable Deliveries (Ack removes, Nack returns/redelivers). It upholds
	// the msgin.PollingSource.Poll contract: at most max deliveries, and never a
	// non-empty result alongside a non-nil error.
	Claim(ctx context.Context, max int) ([]Delivery, error)
	// EmitsLiveValue reports whether this store carries live Go values (no codec,
	// in-memory) or []byte (wire). It drives NewConsumer/NewProducer codec pairing
	// via the channel's LiveValueSource.
	EmitsLiveValue() bool
}
