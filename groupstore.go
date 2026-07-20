package msgin

import (
	"context"
	"time"
)

// MessageGroup is a snapshot of one correlation group held by a MessageGroupStore:
// its key, members in arrival order, and the arrival time of the first member
// (used for expiry). The Messages slice is a copy — mutating it does not affect
// the store.
type MessageGroup interface {
	Key() string
	Messages() []Message[any]
	CreatedAt() time.Time
}

// MessageGroupStore is the swappable state behind an Aggregator: correlation-keyed
// groups of held messages. Implementations live in adapter packages (adapter/memory,
// adapter/database/sql); the core never imports them. It mirrors ChannelStore
// (ADR 0018) one level up, for the Aggregator/Resequencer group-fan-in pattern.
type MessageGroupStore interface {
	// Add durably appends msg to group key and returns the resulting group
	// snapshot. It is idempotent by msg id: re-adding an already-stored member
	// is a no-op returning the unchanged group — so a redelivered member does
	// not double-count toward release (at-least-once).
	Add(ctx context.Context, key string, msg Message[any]) (MessageGroup, error)
	// Remove deletes an entire group (after a successful release) and returns a
	// snapshot of the group it removed, or (nil, nil) if key was absent. The
	// returned snapshot lets a caller (e.g. the Aggregator's expiry reaper)
	// atomically claim-and-inspect what it removed without a separate keyed
	// read, which this SPI does not otherwise offer.
	Remove(ctx context.Context, key string) (MessageGroup, error)
	// Expired returns snapshots of groups whose CreatedAt is strictly before the
	// cutoff (for the Aggregator's expiry reaper).
	Expired(ctx context.Context, before time.Time) ([]MessageGroup, error)
	// EmitsLiveValue reports live Go values (memory) vs []byte (wire, sql), for
	// codec pairing — the same mechanism ChannelStore uses.
	EmitsLiveValue() bool
}
