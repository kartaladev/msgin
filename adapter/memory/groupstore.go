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
// does not double-count); id-less messages are appended without dedup. Carries
// live values. Starts no goroutine — the Aggregator owns the expiry reaper.
type GroupStore struct {
	mu        sync.Mutex
	groups    map[string]*groupState
	clock     clockwork.Clock
	maxGroups int
}

type groupState struct {
	msgs      []msgin.Message[any]
	ids       map[string]struct{}
	createdAt time.Time
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
// snapshot, allocating a new group (stamped with the current clock time) on
// first arrival for key. It is idempotent by msg.ID(): re-adding an
// already-stored member id is a no-op returning the unchanged group snapshot.
// A new key beyond WithMaxGroups returns msgin.ErrOverflowDropped.
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
			return snapshot{key: key, msgs: slices.Clone(g.msgs), createdAt: g.createdAt}, nil
		}
		g.ids[id] = struct{}{}
	}
	g.msgs = append(g.msgs, msg)
	return snapshot{key: key, msgs: slices.Clone(g.msgs), createdAt: g.createdAt}, nil
}

// Remove deletes the group at key and returns a cloned snapshot of the group
// it removed, or (nil, nil) if key was absent.
func (s *GroupStore) Remove(_ context.Context, key string) (msgin.MessageGroup, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	g, ok := s.groups[key]
	if !ok {
		return nil, nil
	}
	delete(s.groups, key)
	return snapshot{key: key, msgs: slices.Clone(g.msgs), createdAt: g.createdAt}, nil
}

// Expired returns snapshots of groups whose CreatedAt is strictly before
// before.
func (s *GroupStore) Expired(_ context.Context, before time.Time) ([]msgin.MessageGroup, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []msgin.MessageGroup
	for key, g := range s.groups {
		if g.createdAt.Before(before) {
			out = append(out, snapshot{key: key, msgs: slices.Clone(g.msgs), createdAt: g.createdAt})
		}
	}
	return out, nil
}

// EmitsLiveValue reports that this store carries live Go values (no codec).
func (s *GroupStore) EmitsLiveValue() bool { return true }

// snapshot is an immutable msgin.MessageGroup view returned by Add/Remove/Expired.
type snapshot struct {
	key       string
	msgs      []msgin.Message[any]
	createdAt time.Time
}

func (s snapshot) Key() string                    { return s.key }
func (s snapshot) Messages() []msgin.Message[any] { return s.msgs }
func (s snapshot) CreatedAt() time.Time           { return s.createdAt }
