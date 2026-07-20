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

// MessageGroupClaim is an exclusive lease over the members of a group that were
// present at claim time. An Aggregator claims a complete group, aggregates and
// forwards it, then SettleGroups the claim (fenced by Epoch). Members that arrive
// during the lease are NOT part of the claim and survive settlement as a fresh
// group (loss-free — ADR 0020 §8).
type MessageGroupClaim interface {
	MessageGroup
	// Epoch is the fence token: SettleGroup/AbandonGroup take effect only while
	// the store's lease for the key still carries this epoch. A lease that
	// expired and was re-claimed (epoch bumped) makes a stale holder's settle a
	// no-op — no phantom delete.
	Epoch() int64
}

// MessageGroupStore is the swappable state behind an Aggregator: correlation-keyed
// groups of held messages, with a store-level atomic lease-claim that makes
// release exactly-once within AND across processes. Implementations live in
// adapter packages (adapter/memory, adapter/database/sql); the core never imports
// them. It mirrors ChannelStore (ADR 0018) one level up.
type MessageGroupStore interface {
	// Add durably appends msg to group key and returns the resulting group
	// snapshot of the LIVE (unclaimed) members — the residual when a claim is in
	// flight, else all members (audit R2 M-C: memory and sql must agree, so the
	// release check sees the same set; a member arriving during a lease is a
	// fresh-residual member, not part of the in-flight claim). Idempotent by msg
	// id: re-adding an already-stored member is a no-op (a redelivered member
	// does not double-count toward release — at-least-once).
	Add(ctx context.Context, key string, msg Message[any]) (MessageGroup, error)
	// ClaimGroup atomically leases the members present now for key and returns
	// them plus a fence epoch. It returns (nil, nil) when key is absent or is
	// already leased by another holder whose lease has not expired — the caller
	// then treats the group as held (someone else is releasing it). The lease
	// TTL is store-owned (each implementation exposes its own configuration,
	// e.g. a WithGroupLeaseTTL-style option — this core interface makes no
	// assumption about it); a crash before SettleGroup lets the lease age out
	// so another holder re-claims (duplicate, never loss).
	ClaimGroup(ctx context.Context, key string) (MessageGroupClaim, error)
	// SettleGroup deletes exactly the claimed member set (fenced on claim.Epoch)
	// after a successful release. Members added during the lease survive as a
	// fresh live group. A fence miss (the lease was stolen) is a no-op, not an
	// error.
	SettleGroup(ctx context.Context, claim MessageGroupClaim) error
	// AbandonGroup releases the lease WITHOUT deleting (the release Send failed,
	// or the reaper found a not-actually-expired group): the claimed members
	// return to live so a retry / next member / next reaper tick re-releases.
	// Fenced on claim.Epoch; a fence miss is a no-op.
	AbandonGroup(ctx context.Context, claim MessageGroupClaim) error
	// Expired returns groups the reaper's settlement sweep must re-examine: any
	// group whose LEASE has expired (a crashed holder — sql) regardless of age,
	// PLUS (when before is non-zero) unleased groups whose CreatedAt is before the
	// cutoff. Excludes groups under a live lease. (audit R2 H-A: the crashed-lease
	// case is how a durable store's stuck complete group is found and re-released.)
	Expired(ctx context.Context, before time.Time) ([]MessageGroup, error)
	// RecoverInterval is the cadence at which the reaper sweeps for crashed leases,
	// independent of WithGroupTimeout (audit R2 H-A). memory returns 0 (unconditional
	// lease — no crash-recovery sweep needed); sql returns its lease TTL, so a
	// crashed holder's group is recovered within ~one TTL even with no expiry timeout.
	RecoverInterval() time.Duration
	// EmitsLiveValue reports live Go values (memory) vs []byte (wire, sql).
	EmitsLiveValue() bool
}
