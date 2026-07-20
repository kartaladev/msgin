package sql_test

// groupdialect_fake_test.go provides an in-memory GroupDialect (Plan 017 Task
// 2 — mirrors fakedialect_test.go's fakeDialect pattern): it reproduces the
// group-aggregation lease-claim contract deterministically in a plain Go map,
// so sql.GroupStore's framing/decoding/delegation Go-logic hot-path branches
// are covered without a real database — the real per-engine SQL (group-row
// locking, claimed-epoch re-absorption, etc.) is proven against real engines
// in Task 3's testcontainers conformance suite.
//
// Every method ignores the Querier argument it receives, exactly like
// fakeDialect.

import (
	"context"
	"sync"
	"time"

	msginsql "github.com/kartaladev/msgin/adapter/database/sql"
)

// fakeGroupRow is one in-memory member row backing fakeGroupDialect.
type fakeGroupRow struct {
	msgID        string
	seq          int64
	headers      []byte
	payload      []byte
	claimedEpoch int64 // 0 = live (unclaimed); matches the group's current epoch when claimed
}

// fakeGroupState is one in-memory group backing fakeGroupDialect.
type fakeGroupState struct {
	createdAt time.Time
	epoch     int64
	lockedBy  string // "" = unleased
	members   []*fakeGroupRow
}

// fakeGroupDialect is the in-memory msginsql.GroupDialect used by
// groupstore_unit_test.go. The zero value is not usable; build one with
// newFakeGroupDialect. It deliberately implements only the SUBSET of the real
// per-engine fencing semantics that sql.GroupStore's own Go logic depends on
// (framing/decoding/delegation/error-propagation) — it is NOT a conformance
// double for the group-row-lock/re-absorption SQL, which Task 3 proves against
// real engines.
type fakeGroupDialect struct {
	mu     sync.Mutex
	groups map[string]*fakeGroupState
	tables map[string]bool

	// now is the injectable clock (defaults to time.Now); tests fix it for
	// deterministic CreatedAt assertions.
	now func() time.Time

	// Error injection, so GroupStore's classifyQueryErr wrap-vs-passthrough
	// branch is directly testable without a real DB.
	addMemberErr    error
	claimGroupErr   error
	settleGroupErr  error
	abandonGroupErr error
	expiredErr      error

	// Recorded call arguments, for assertions that the store passes the
	// configured leaseTTL/lockedBy through.
	lastClaimLockedBy   string
	lastClaimLeaseTTL   time.Duration
	lastSettleLockedBy  string
	lastAbandonLockedBy string
	lastExpiredLeaseTTL time.Duration

	// settleApplied / abandonApplied let a test force the dialect's
	// applied=false (fence-miss, not-an-error) branch.
	settleApplied  bool
	abandonApplied bool

	// schemaExistsErr forces SchemaExists to error, covering
	// GroupStore.Ready's first-probe error-passthrough branch.
	schemaExistsErr error
}

func newFakeGroupDialect() *fakeGroupDialect {
	return &fakeGroupDialect{
		groups:         make(map[string]*fakeGroupState),
		tables:         make(map[string]bool),
		now:            time.Now,
		settleApplied:  true,
		abandonApplied: true,
	}
}

// markGroupReady marks table as schema-initialized (SchemaExists -> true).
func (f *fakeGroupDialect) markGroupReady(table string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tables[table] = true
}

// seedClaimed directly seeds a group with a live claim in place (test setup
// helper for SettleGroup/AbandonGroup fence tests), bypassing
// AddMember/ClaimGroup.
func (f *fakeGroupDialect) seedClaimed(key, lockedBy string, epoch int64, members ...*fakeGroupRow) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, m := range members {
		m.claimedEpoch = epoch
	}
	f.groups[key] = &fakeGroupState{
		createdAt: f.now(),
		epoch:     epoch,
		lockedBy:  lockedBy,
		members:   members,
	}
}

// groupExists reports whether key is still present (test assertion helper).
func (f *fakeGroupDialect) groupExists(key string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, ok := f.groups[key]
	return ok
}

// liveMembersLocked returns the group's live (claimed_epoch == 0) members as
// msginsql.MemberRow. Callers must hold f.mu.
func liveMembersLocked(g *fakeGroupState) []msginsql.MemberRow {
	var out []msginsql.MemberRow
	for _, m := range g.members {
		if m.claimedEpoch == 0 {
			out = append(out, msginsql.MemberRow{MsgID: m.msgID, Seq: m.seq, Headers: m.headers, Payload: m.payload})
		}
	}
	return out
}

// ---- GroupDialect -----------------------------------------------------

func (f *fakeGroupDialect) AddMember(_ context.Context, _ msginsql.Querier, _, groupKey, msgID string, seq int64, headers, payload []byte) (msginsql.GroupRows, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.addMemberErr != nil {
		return msginsql.GroupRows{}, f.addMemberErr
	}
	if msgID == "" {
		return msginsql.GroupRows{}, msginsql.ErrMissingMsgID
	}

	g, ok := f.groups[groupKey]
	if !ok {
		g = &fakeGroupState{createdAt: f.now()}
		f.groups[groupKey] = g
	}
	found := false
	for _, m := range g.members {
		if m.msgID == msgID {
			found = true
			break
		}
	}
	if !found {
		g.members = append(g.members, &fakeGroupRow{msgID: msgID, seq: seq, headers: headers, payload: payload})
	}

	return msginsql.GroupRows{
		GroupKey:  groupKey,
		CreatedAt: g.createdAt,
		Members:   liveMembersLocked(g),
	}, nil
}

func (f *fakeGroupDialect) ClaimGroup(_ context.Context, _ msginsql.Querier, _, groupKey, lockedBy string, leaseTTL time.Duration) (*msginsql.ClaimedGroup, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.lastClaimLockedBy = lockedBy
	f.lastClaimLeaseTTL = leaseTTL

	if f.claimGroupErr != nil {
		return nil, f.claimGroupErr
	}

	g, ok := f.groups[groupKey]
	if !ok || g.lockedBy != "" {
		return nil, nil // absent, or actively leased by another holder
	}

	g.epoch++
	g.lockedBy = lockedBy
	var claimed []msginsql.MemberRow
	for _, m := range g.members {
		if m.claimedEpoch == 0 {
			m.claimedEpoch = g.epoch
		}
		if m.claimedEpoch == g.epoch {
			claimed = append(claimed, msginsql.MemberRow{MsgID: m.msgID, Seq: m.seq, Headers: m.headers, Payload: m.payload})
		}
	}

	return &msginsql.ClaimedGroup{
		GroupRows: msginsql.GroupRows{
			GroupKey:  groupKey,
			CreatedAt: g.createdAt,
			Members:   claimed,
		},
		Epoch: g.epoch,
	}, nil
}

func (f *fakeGroupDialect) SettleGroup(_ context.Context, _ msginsql.Querier, _, groupKey, lockedBy string, epoch int64) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.lastSettleLockedBy = lockedBy

	if f.settleGroupErr != nil {
		return false, f.settleGroupErr
	}
	g, ok := f.groups[groupKey]
	if !ok || g.lockedBy != lockedBy || g.epoch != epoch {
		return false, nil
	}
	if !f.settleApplied {
		return false, nil
	}

	remaining := g.members[:0]
	for _, m := range g.members {
		if m.claimedEpoch != epoch {
			remaining = append(remaining, m)
		}
	}
	g.members = remaining
	if len(g.members) == 0 {
		delete(f.groups, groupKey)
	} else {
		g.lockedBy = ""
		g.createdAt = f.now()
	}
	return true, nil
}

func (f *fakeGroupDialect) AbandonGroup(_ context.Context, _ msginsql.Querier, _, groupKey, lockedBy string, epoch int64) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.lastAbandonLockedBy = lockedBy

	if f.abandonGroupErr != nil {
		return false, f.abandonGroupErr
	}
	g, ok := f.groups[groupKey]
	if !ok || g.lockedBy != lockedBy || g.epoch != epoch {
		return false, nil
	}
	if !f.abandonApplied {
		return false, nil
	}

	for _, m := range g.members {
		if m.claimedEpoch == epoch {
			m.claimedEpoch = 0
		}
	}
	g.lockedBy = ""
	return true, nil
}

func (f *fakeGroupDialect) ExpiredGroups(_ context.Context, _ msginsql.Querier, _ string, before time.Time, leaseTTL time.Duration, limit int) ([]msginsql.GroupRows, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.lastExpiredLeaseTTL = leaseTTL

	if f.expiredErr != nil {
		return nil, f.expiredErr
	}

	var out []msginsql.GroupRows
	for key, g := range f.groups {
		if g.lockedBy != "" {
			continue // actively leased; excluded
		}
		if before.IsZero() || g.createdAt.Before(before) {
			out = append(out, msginsql.GroupRows{
				GroupKey:  key,
				CreatedAt: g.createdAt,
				Members:   liveMembersLocked(g),
			})
		}
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, nil
}

func (f *fakeGroupDialect) EnsureGroupSchema(_ context.Context, _ msginsql.Querier, table string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tables[table] = true
	return nil
}

func (f *fakeGroupDialect) SchemaExists(_ context.Context, _ msginsql.Querier, table string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.schemaExistsErr != nil {
		return false, f.schemaExistsErr
	}
	return f.tables[table], nil
}

// Compile-time assertion that fakeGroupDialect satisfies GroupDialect.
var _ msginsql.GroupDialect = (*fakeGroupDialect)(nil)
