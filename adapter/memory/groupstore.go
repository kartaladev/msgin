package memory

import (
	"context"
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
	mu        sync.Mutex
	groups    map[string]*groupState
	clock     clockwork.Clock
	maxGroups int
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
	clock     clockwork.Clock
	maxGroups int
}

// GroupStoreOption configures a GroupStore.
type GroupStoreOption func(*groupStoreConfig)

// WithMaxGroups bounds the number of concurrently held (incomplete) groups;
// default 1024. A new correlation key beyond the cap makes Add return
// msgin.ErrOverflowDropped rather than evicting a partial group (which would be
// silent data loss). n<=0 is invalid → msgin.ErrInvalidCapacity.
func WithMaxGroups(n int) GroupStoreOption {
	return func(c *groupStoreConfig) { c.maxGroups = n }
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
func NewGroupStore(opts ...GroupStoreOption) (*GroupStore, error) {
	cfg := groupStoreConfig{clock: clockwork.NewRealClock(), maxGroups: 1024}
	for _, opt := range opts {
		opt(&cfg)
	}
	if cfg.maxGroups <= 0 {
		return nil, msgin.ErrInvalidCapacity
	}
	return &GroupStore{groups: make(map[string]*groupState), clock: cfg.clock, maxGroups: cfg.maxGroups}, nil
}

// Add durably appends msg to group key and returns the resulting group
// snapshot of the LIVE (unclaimed) members, allocating a new group (stamped
// with the current clock time) on first arrival for key. It is idempotent by
// msg.ID(): re-adding an already-stored member id is a no-op returning the
// unchanged live-members snapshot. A new key beyond WithMaxGroups returns
// msgin.ErrOverflowDropped.
func (s *GroupStore) Add(_ context.Context, key string, msg msgin.Message[any]) (msgin.MessageGroup, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	g, ok := s.groups[key]
	if !ok {
		if len(s.groups) >= s.maxGroups {
			return nil, msgin.ErrOverflowDropped
		}
		g = &groupState{ids: make(map[string]struct{}), createdAt: s.clock.Now()}
		s.groups[key] = g
	}
	if id := msg.ID(); id != "" {
		if _, seen := g.ids[id]; seen {
			return snapshot{key: key, msgs: slices.Clone(g.msgs[g.claimedLen:]), createdAt: g.createdAt}, nil
		}
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
