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
	//
	// # The per-group member bound is part of this contract (Spec 017 §3.7)
	//
	// An implementation MUST bound the number of members it retains for a
	// single group, and MUST report an Add that would exceed that bound as
	// ErrOverflowDropped rather than growing without limit. The Aggregator's
	// release strategy cannot supply this bound: three of its four paths are a
	// caller-supplied closure or a message header — and the store is the only
	// site that can refuse a member BEFORE retaining it.
	//
	// The counted set MUST include every member the implementation still
	// retains for that group — LIVE AND CLAIMED — and MUST be stated in the
	// implementation's godoc. Both first-party stores count live + claimed: a
	// bound that ignores the claimed set does not bound, because a claim moves
	// members out of the live set without removing them (Spec 017 §3.4).
	//
	// An implementation SHOULD mark the rejection Permanent when the group
	// cannot drain itself, and leave it transient when a claim is in flight
	// that will drain it (Spec 017 §3.3.1). A bare transient rejection of a
	// group that will never drain HOT-SPINS under the default RetryPolicy,
	// which has neither a MaxAttempts nor a Backoff.
	//
	// An implementation MAY return the group's current LIVE snapshot ALONGSIDE
	// the overflow error. When it does, the Aggregator re-evaluates the release
	// strategy against that snapshot and releases the group if it is ready, so
	// a full-but-releasable group is not deadlocked by its own bound (Spec 017
	// §3.3a). Returning (nil, err) remains valid and is what every pre-existing
	// implementation does.
	//
	// THE RE-EVALUATION IS CONDITIONAL. Three gates decide whether the
	// Aggregator touches the snapshot at all, and when any of them fails the
	// error is returned unchanged and the snapshot is ignored entirely
	// (Spec 017 §3.3a.1):
	//
	//   - The error must carry ErrOverflowDropped. This clause is the OVERFLOW
	//     contract, so a snapshot handed back beside any OTHER fault is never
	//     acted on: an implementation must not have a group claimed,
	//     aggregated, emitted and settled because an unrelated error path
	//     happened to return its zero-value snapshot.
	//   - The snapshot must be non-nil, TYPED NILS INCLUDED. A
	//     (*yourGroup)(nil) — the value the conformance idiom
	//     "var _ MessageGroup = (*yourGroup)(nil)" produces — is rejected
	//     exactly like an untyped nil, because its methods would panic.
	//   - The snapshot must hold at least one member. An empty live residual
	//     means another holder's claim already covers every member, so there is
	//     nothing left here to release. That is evidence the group is LEASED,
	//     NOT proof it drains — a holder ending in AbandonGroup leaves the group
	//     exactly as full (Plan 031 finding R-13). Nothing turns on the
	//     difference at this gate, which returns the error unchanged either way;
	//     it matters at the downgrade below, which does classify.
	//
	// When the Aggregator does act on that snapshot it NEVER UPGRADES the
	// implementation's classification ON ITS OWN ACCOUNT: no path of its own
	// re-marks a transient rejection permanent. It either DOWNGRADES the
	// rejection to a fresh, TRANSIENT overflow error, or REPLACES the overflow
	// error entirely with the distinct fault it hit, which then carries that
	// fault's own classification rather than the implementation's.
	//
	// THE DOWNGRADE IS NOT ALWAYS BACKED BY PROOF OF DRAINAGE, and an
	// implementation must not design against it as though it were (Plan 031
	// finding R-13). Three exits downgrade, on evidence of three different
	// strengths: the group PROVABLY drained, because the Aggregator claimed it
	// and released it; a claim was REFUSED because another holder's lease is in
	// flight, which is evidence a drain is in progress and not proof one
	// completes, since a holder ending in AbandonGroup leaves the group exactly
	// as full; or the claim SUCCEEDED and the RELEASE THEN FAILED, which is
	// evidence of the opposite of drainage. The last is downgraded deliberately
	// all the same — see the paragraph after next.
	//
	// EXACTLY ONE EXIT REPLACES: a failing ClaimGroup, whose store fault is
	// returned verbatim, because masking a store fault behind ErrOverflowDropped
	// would point an operator at the cap. It is unmarked, hence transient, so a
	// persistently failing claim path RETRIES rather than terminating. That is
	// deliberate: marking a store fault permanent because it was reached through
	// an overflow would dead-letter messages for a cause that has nothing to do
	// with the cap.
	//
	// A FAILING RELEASE DOES NOT REPLACE. The Aggregator re-mints a fresh,
	// TRANSIENT ErrOverflowDropped carrying the release fault's rendered TEXT
	// and not the fault itself, so errors.Is/errors.As cannot reach that cause
	// through the returned error. Severing the chain is the point: propagating
	// the fault verbatim let a Permanent-marked aggregate or output Send
	// TERMINALLY SETTLE a member the implementation never stored, LOSING it, for
	// a fault in the other, already-claimed members' payloads (ADR 0033 D-AX;
	// Plan 031 finding R-2).
	//
	// THE NEVER-UPGRADES PROMISE IS NOT UNCONDITIONAL, and an implementation
	// must not design against it as one. When the CALLER's release strategy
	// FAILS — a strategy that returned an ERROR, which is a different exit from
	// the aggregate/Send failure above — the
	// Aggregator returns errors.Join(overflowErr, strategyErr); IsPermanent
	// uses errors.As, which traverses the join, so a Permanent-marked strategy
	// error makes the reported error permanent even though the implementation
	// classified its own rejection transient. The marker is the caller's, not
	// the Aggregator's — but it reaches the consumer all the same (Spec 017
	// §3.3b; ADR 0033 D-AW).
	//
	// An implementation MUST NOT assume its Permanent marker survives to the
	// consumer on every path either: per the two paragraphs above, a failing
	// claim reports the store's own unmarked fault and a failing release
	// reports a fresh transient overflow error, so BOTH drop the marker and a
	// persistently failing claim/release path RETRIES rather than terminating
	// (Spec 017 §3.3a.1).
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
