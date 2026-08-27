package memory

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/jonboulle/clockwork"
	"github.com/kartaladev/msgin"
)

// GroupStore is an in-memory msgin.MessageGroupStore: correlation-keyed groups
// of held messages, for an Aggregator. Delivery guarantee: at-least-once within
// the process lifetime; partial groups are LOST on process exit (use the sql
// store to survive a restart). Add is idempotent by msg id (a redelivered member
// does not double-count); id-less messages are appended without dedup (and lack
// idempotent-by-id redelivery dedup — rare, since source messages carry ids).
// Carries live values. Starts no goroutine — the Aggregator owns the expiry
// reaper.
//
// The lease a ClaimGroup grants is UNCONDITIONAL while held (no wall-clock
// TTL): it is released only by a matching SettleGroup or AbandonGroup, both
// synchronous in the claiming goroutine. RecoverInterval reports 0 — there is
// no crash-recovery sweep to run, since a lease cannot outlive the goroutine
// that holds it within one process.
type GroupStore struct {
	mu              sync.Mutex
	groups          map[string]*groupState
	clock           clockwork.Clock
	maxGroups       int
	maxGroupMembers int
}

// groupState is one correlation group's mutable state. msgs grows only by
// append; the active claim (if leased) is the PREFIX msgs[:claimedLen] — a
// member Added during a lease lands beyond claimedLen and survives settlement
// as a fresh residual group.
type groupState struct {
	msgs       []msgin.Message[any]
	ids        map[string]struct{}
	createdAt  time.Time
	epoch      int64 // bumped on each ClaimGroup, fences Settle/Abandon
	leased     bool  // true between ClaimGroup and Settle/Abandon (UNCONDITIONAL — no wall-clock TTL)
	claimedLen int   // # members frozen into the active claim; Add only appends, so claimed = msgs[:claimedLen]
}

type groupStoreConfig struct {
	clock           clockwork.Clock
	maxGroups       int
	maxGroupMembers int
}

// GroupStoreOption configures a GroupStore.
type GroupStoreOption func(*groupStoreConfig)

// maxGroupsCeiling is the upper bound WithMaxGroups accepts (Spec 016 §3.4).
// maxGroups is the ONLY thing evicting: Add's admission check
// (groupstore.go's len(s.groups) >= s.maxGroups) is what caps s.groups, so
// with no ceiling a huge maxGroups admits an unbounded number of new
// correlation keys and s.groups grows without bound (Spec 016 §1.3). The
// ceiling is in-flight aggregation groups, matched to the queue depth
// (maxCapacityCeiling) that can feed them, and 1024x the default (ADR 0032).
const maxGroupsCeiling = 1 << 20

// WithMaxGroups bounds the number of concurrently held (incomplete) groups to
// n, which must be in [1, maxGroupsCeiling] (1,048,576); default 1024. A new
// correlation key beyond the cap makes Add return msgin.ErrOverflowDropped
// rather than evicting a partial group (which would be silent data loss). n
// outside [1, maxGroupsCeiling] is a construction-time error
// (msgin.ErrInvalidCapacity), not a silent clamp — see maxGroupsCeiling's
// godoc for why the upper bound matters even though the check itself is a
// scalar comparison.
func WithMaxGroups(n int) GroupStoreOption {
	return func(c *groupStoreConfig) { c.maxGroups = n }
}

// defaultMaxGroupMembers is the number of members WithMaxGroupMembers admits
// into ONE correlation group when the option is not set: 65,536 (Spec 017
// §3.2). The value is not a fresh judgement — it REUSES routing's
// completionSizeCeiling (Spec 016 §3.4, ADR 0032 D-Z), which fixed 65,536 as
// "far beyond any plausible aggregation" over the identical unit, members of
// one correlation group.
//
// The cost basis is TIME, not bytes: Add clones the live member slice on
// every call, so growing one group to m members clones Θ(m²) message headers
// — 8.6 s and 48.3 GiB of allocation churn at 65,536, against only ~31 MiB
// live (Spec 016 §1.4). That is why no test grows a group to this value.
//
// INVARIANT: this default must stay >= routing's completionSizeCeiling. A
// caller may legally configure routing.WithCompletionSize up to that ceiling,
// and a smaller default cap here would make such a group reject its own
// completing member before the release predicate could ever fire — a silent
// deadlock in place of a bound (Spec 017 §3.5). The same invariant binds this
// default's twin, sql.defaultMaxGroupMembers, and the root blackbox test
// group_member_bound_invariant_test.go enforces it for both stores by reading
// all three constants out of the AST — this comment explains the relation,
// that test is what defends it.
const defaultMaxGroupMembers = 1 << 16

// maxGroupMembersCeiling is the upper bound WithMaxGroupMembers accepts
// (Spec 017 §3.2). It matches maxGroupsCeiling above, the sibling bound in the
// same struct, so one number reads as "the largest in-flight aggregation
// quantity this library will accept" for both group COUNT and members per
// group. The ceiling matters even though the check itself is a scalar
// comparison: it is what stops a caller from configuring a cap so large that
// the bound is nominal, and — per defaultMaxGroupMembers' cost note — the
// quadratic clone cost makes even the ceiling unreachable in practice.
const maxGroupMembersCeiling = 1 << 20

// WithMaxGroupMembers bounds the number of members ONE correlation group may
// hold to n, which must be in [1, maxGroupMembersCeiling] (1,048,576);
// default 65,536 (see defaultMaxGroupMembers for why that number). n outside
// the range is a construction-time error (msgin.ErrInvalidCapacity), not a
// silent clamp.
//
// WHAT IT COUNTS: every member the store retains for the key — LIVE plus
// CLAIMED. ClaimGroup freezes a prefix without shrinking the group, so
// claimed members keep counting until SettleGroup deletes them; a group at
// exactly n therefore rejects new arrivals for the duration of a claim, even
// though its live residual may be empty. That is what makes the bound a
// bound. The count rendered in the error is "members retained at the moment
// of the check", and this store checks BEFORE the append, so at n = 4 it
// renders "holds 4 members, limit 4".
//
// AT THE BOUNDARY: an Add that would take the group past n returns
// msgin.ErrOverflowDropped, wrapped with the site, the group key, the count
// and the limit. The member is NOT stored, and it leaves no trace in the
// dedup set — a redelivery after the group drains is admitted normally. The
// live snapshot is returned ALONGSIDE the error so routing.Aggregator.Handle
// can still re-evaluate the release: the member is rejected, the release is
// not.
//
// CLASSIFICATION — the rejection is classified by CAUSE (Spec 017 §3.3.1):
//
//   - The group is NOT leased: nothing will drain it on its own (nothing
//     drains an unleased group without an expiry cutoff, and this store's
//     RecoverInterval is 0, so an Aggregator with no WithGroupTimeout never
//     sweeps). The error is msgin.Permanent-wrapped, which the runtime
//     settles TERMINALLY — one attempt at the invalid-message sink, or the
//     dead-letter sink as a fallback, never a Nack. This is deliberate: a
//     plain transient rejection on the SHIPPED zero-value msgin.RetryPolicy
//     (no MaxAttempts, no Backoff) is an unlogged, zero-delay redelivery
//     loop. With NEITHER sink configured the runtime WARNs, naming both
//     missing options, and then ACKs — so the source DROPS the message.
//     Configure endpoint.WithInvalidMessageSink (or RetryPolicy.DeadLetter) to
//     turn that loss into a capture; the library cannot supply one.
//   - The group IS leased: a claim is in flight and Settle/Abandon runs on
//     every release path, so the retry genuinely succeeds afterwards. The
//     error stays transient (unwrapped). Under the zero-value RetryPolicy
//     that retry is a zero-delay busy-wait for the width of the claim window;
//     set RetryPolicy.Backoff if that matters.
//
// A group that is full and unreleasable stays full: this option bounds
// growth, it does not provide liveness. Set routing.WithGroupTimeout to have
// the reaper expire such a group.
//
// 🔴 LOWERING n BELOW A CONFIGURED routing.WithCompletionSize DEADLOCKS THE
// GROUP, and nothing rejects the pair. The two options live in different
// packages, so neither constructor can see the other: n = 10 with
// WithCompletionSize(20) constructs cleanly, then refuses every arrival past
// the 10th while the release predicate declines at 10 < 20 — each refused
// member dead-lettered, or DROPPED outright with neither
// endpoint.WithInvalidMessageSink nor RetryPolicy.DeadLetter configured, and
// the group never releasing. The default cap is >= routing's
// completionSizeCeiling precisely so this cannot happen unless you lower it
// (see defaultMaxGroupMembers' INVARIANT). Keep n at or above the completion
// size; routing.WithGroupTimeout is the liveness escape if you cannot, since
// this store's RecoverInterval is 0 and without a timeout no reaper sweeps at
// all.
func WithMaxGroupMembers(n int) GroupStoreOption {
	return func(c *groupStoreConfig) { c.maxGroupMembers = n }
}

// WithGroupClock injects the clock used to stamp group CreatedAt (default real
// clock; tests pass clockwork.NewFakeClock()). Named distinctly from
// queuestore.go's WithClock(QueueStoreOption) to avoid a same-package function
// redeclaration — both configure a clockwork.Clock but for different option
// types.
func WithGroupClock(c clockwork.Clock) GroupStoreOption {
	return func(cfg *groupStoreConfig) {
		if c != nil {
			cfg.clock = c
		}
	}
}

// NewGroupStore builds an in-memory MessageGroupStore.
//
// A nil ELEMENT of opts is a bare [msgin.ErrNilFunc] naming the element's
// 0-based index ("memory.NewGroupStore: nil option at index 1"), not a panic
// — checked as opts is applied (the loop is the first statement that can fail,
// preceded only by the config-defaults initializer, which cannot fail), so it
// runs BEFORE the WithMaxGroups validation below, which runs after the loop and so
// loses to it.
func NewGroupStore(opts ...GroupStoreOption) (*GroupStore, error) {
	cfg := groupStoreConfig{clock: clockwork.NewRealClock(), maxGroups: 1024, maxGroupMembers: defaultMaxGroupMembers}
	for i, opt := range opts {
		if opt == nil {
			return nil, nilOptionAt("memory.NewGroupStore", i)
		}
		opt(&cfg)
	}
	if err := checkRange(msgin.ErrInvalidCapacity, "memory.WithMaxGroups",
		cfg.maxGroups, 1, maxGroupsCeiling); err != nil {
		return nil, err
	}
	if err := checkRange(msgin.ErrInvalidCapacity, "memory.WithMaxGroupMembers",
		cfg.maxGroupMembers, 1, maxGroupMembersCeiling); err != nil {
		return nil, err
	}
	return &GroupStore{
		groups:          make(map[string]*groupState),
		clock:           cfg.clock,
		maxGroups:       cfg.maxGroups,
		maxGroupMembers: cfg.maxGroupMembers,
	}, nil
}

// Add durably appends msg to group key and returns the resulting group
// snapshot of the LIVE (unclaimed) members, allocating a new group (stamped
// with the current clock time) on first arrival for key. It is idempotent by
// msg.ID(): re-adding an already-stored member id is a no-op returning the
// unchanged live-members snapshot.
//
// It has TWO overflow arms, both reporting msgin.ErrOverflowDropped wrapped
// with the site, the offending key, the count and the limit:
//
//   - a NEW key beyond WithMaxGroups — the store already holds its maximum
//     number of concurrently held groups. TRANSIENT: the group map drains
//     whenever any group settles, so the retry genuinely can succeed.
//   - a member beyond WithMaxGroupMembers — this one correlation group is
//     full. Classified by cause: msgin.Permanent when the group is not
//     leased (nothing will drain it), transient while a claim is in flight.
//     See WithMaxGroupMembers for the full contract.
//
// The member-cap arm returns the LIVE SNAPSHOT ALONGSIDE the error so
// routing.Aggregator.Handle can re-evaluate the release predicate against a
// group that is complete but was never re-triggered. The group-count arm has
// no group to report and returns (nil, err).
//
// THAT SNAPSHOT IS NOT DEAD WEIGHT WHILE THE GROUP IS LEASED, though it reads
// that way: claimedLen == len(msgs) holds only at the instant ClaimGroup
// returns. Add appends beyond claimedLen for the width of the lease, so a
// group claimed BEFORE it filled reports a real, non-empty live residual here
// — the members that arrived during the claim — and that is exactly the group
// Handle re-evaluates. Only the wholly-claimed shape (the claim froze every
// member) yields an empty residual, and Handle gates on that explicitly: an
// empty live snapshot means the claim holder is already draining the group, so
// Handle returns this arm's transient error unchanged instead of running a
// release strategy against zero members.
//
// The member check sits BETWEEN the dedup lookup and the dedup insert, and
// the id is hoisted out of the dedup branch so the check also runs for
// id-less messages. Both positions are load-bearing: above the lookup, an
// idempotent re-add at exactly the cap would become an overflow; below the
// insert, a rejected member would be recorded as seen and its redelivery
// would return the dedup no-op with a NIL error — the source would Ack a
// message that was never appended.
func (s *GroupStore) Add(_ context.Context, key string, msg msgin.Message[any]) (msgin.MessageGroup, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	g, ok := s.groups[key]
	if !ok {
		if len(s.groups) >= s.maxGroups {
			return nil, fmt.Errorf("%w: memory.GroupStore.Add: new group %q rejected: store holds %d groups, limit %d",
				msgin.ErrOverflowDropped, key, len(s.groups), s.maxGroups)
		}
		g = &groupState{ids: make(map[string]struct{}), createdAt: s.clock.Now()}
		s.groups[key] = g
	}
	id := msg.ID()
	if id != "" {
		if _, seen := g.ids[id]; seen {
			return snapshot{key: key, msgs: slices.Clone(g.msgs[g.claimedLen:]), createdAt: g.createdAt}, nil
		}
	}
	if len(g.msgs) >= s.maxGroupMembers {
		live := snapshot{key: key, msgs: slices.Clone(g.msgs[g.claimedLen:]), createdAt: g.createdAt}
		err := fmt.Errorf("%w: memory.GroupStore.Add: group %q holds %d members, limit %d",
			msgin.ErrOverflowDropped, key, len(g.msgs), s.maxGroupMembers)
		if !g.leased {
			return live, msgin.Permanent(err) // structurally stuck: nothing drains an unleased group
		}
		return live, err // a claim is in flight; Settle/Abandon will drain it
	}
	if id != "" {
		g.ids[id] = struct{}{}
	}
	g.msgs = append(g.msgs, msg)
	return snapshot{key: key, msgs: slices.Clone(g.msgs[g.claimedLen:]), createdAt: g.createdAt}, nil
}

// ClaimGroup atomically leases the members currently present for key and
// returns them plus a fence epoch. It returns (nil, nil) when key is absent or
// already leased (a live in-process holder — no wall-clock steal).
func (s *GroupStore) ClaimGroup(_ context.Context, key string) (msgin.MessageGroupClaim, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	g, ok := s.groups[key]
	if !ok || g.leased {
		return nil, nil // absent, or held by a live holder (no wall-clock steal in-process)
	}
	g.epoch++
	g.leased = true
	g.claimedLen = len(g.msgs)
	claimed := slices.Clone(g.msgs[:g.claimedLen])
	return claimGroup{snapshot{key: key, msgs: claimed, createdAt: g.createdAt}, g.epoch}, nil
}

// SettleGroup deletes exactly the claimed member set (fenced on claim.Epoch)
// after a successful release. Members added during the lease survive as a
// fresh live group. A fence miss (already settled/abandoned/stolen) is a
// no-op.
func (s *GroupStore) SettleGroup(_ context.Context, claim msgin.MessageGroupClaim) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	g, ok := s.groups[claim.Key()]
	if !ok || !g.leased || g.epoch != claim.Epoch() { // fence miss / stolen / already settled
		return nil
	}
	// delete exactly the claimed PREFIX; anything appended during the lease survives.
	for _, m := range g.msgs[:g.claimedLen] {
		if id := m.ID(); id != "" {
			delete(g.ids, id) // so a post-completion redelivery forms a fresh group, not a dedup no-op
		}
	}
	residual := slices.Clone(g.msgs[g.claimedLen:])
	if len(residual) == 0 {
		delete(s.groups, claim.Key())
		return nil
	}
	g.msgs = residual
	g.leased = false
	g.claimedLen = 0
	g.createdAt = s.clock.Now() // residual is a fresh group for expiry (matches sql — audit R1 M2)
	return nil
}

// AbandonGroup releases the lease WITHOUT deleting: the claimed members return
// to live (along with anything appended during the lease) so a retry / next
// member / next reaper tick re-releases. Fenced on claim.Epoch; a fence miss
// is a no-op.
func (s *GroupStore) AbandonGroup(_ context.Context, claim msgin.MessageGroupClaim) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	g, ok := s.groups[claim.Key()]
	if !ok || !g.leased || g.epoch != claim.Epoch() {
		return nil
	}
	g.leased = false // members return to live (all of msgs, incl. any appended during the lease)
	g.claimedLen = 0 // epoch stays bumped so the abandoned holder's later settle is a no-op
	return nil
}

// Expired returns snapshots of unleased groups whose CreatedAt is strictly
// before before. A crashed lease never occurs in-process (the lease is
// released synchronously by the claiming goroutine, or by the Aggregator's
// panic-safe defer-abandon), so memory's Expired surfaces only age-old
// unleased groups.
func (s *GroupStore) Expired(_ context.Context, before time.Time) ([]msgin.MessageGroup, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []msgin.MessageGroup
	for key, g := range s.groups {
		if g.createdAt.Before(before) && !g.leased {
			out = append(out, snapshot{key: key, msgs: slices.Clone(g.msgs), createdAt: g.createdAt})
		}
	}
	return out, nil
}

// RecoverInterval reports 0: memory needs no crash-recovery sweep — a lease
// cannot outlive the goroutine that holds it within one process.
func (s *GroupStore) RecoverInterval() time.Duration { return 0 }

// EmitsLiveValue reports that this store carries live Go values (no codec).
func (s *GroupStore) EmitsLiveValue() bool { return true }

// snapshot is an immutable msgin.MessageGroup view returned by Add/ClaimGroup/Expired.
type snapshot struct {
	key       string
	msgs      []msgin.Message[any]
	createdAt time.Time
}

func (s snapshot) Key() string                    { return s.key }
func (s snapshot) Messages() []msgin.Message[any] { return s.msgs }
func (s snapshot) CreatedAt() time.Time           { return s.createdAt }

// claimGroup is a snapshot + fence epoch implementing msgin.MessageGroupClaim.
type claimGroup struct {
	snapshot
	epoch int64
}

func (c claimGroup) Epoch() int64 { return c.epoch }
