package sql_test

// fakedialect_test.go provides an in-memory LeaseDialect + LockDialect +
// InboxDialect (Plan 006 Task 2 — root coverage safety net): it reproduces the
// lease/claim, lock/FOR-UPDATE, and dedup-inbox contracts deterministically in
// a plain Go map, so the engine's Go-logic hot-path branches (Source.Poll
// batching/framing, Ack/Nack fencing, InboxDeduper dedup) are covered without
// a real database — the coverage Tasks 4-5 need once the real-DB suites move
// out of root into leaf-test dialect modules (audit F6/R2-1).
//
// Every method ignores the Querier argument it receives: a real dialect runs
// SQL through it, but this fake keeps its own state, so the engine's
// constructors still require a non-nil *sql.DB (openDB(t, fakeDriverName)
// satisfies that without ever dialing).

import (
	"context"
	stdsql "database/sql"
	"sort"
	"sync"
	"testing"
	"time"

	msgin "github.com/kartaladev/msgin"
	msginsql "github.com/kartaladev/msgin/adapter/database/sql"
	"github.com/stretchr/testify/require"
)

// fakeRow is one in-memory row backing fakeDialect.
type fakeRow struct {
	id            int64
	msgID         string
	headers       []byte
	payload       []byte
	deliveryCount int
	leaseEpoch    int64
	lockedBy      string    // lease-strategy fence owner; "" = unlocked
	leaseUntil    time.Time // zero = no active lease
	visibleAfter  time.Time // zero = immediately visible
	locked        bool      // lock-strategy (FOR UPDATE) claim marker
}

// fakeDialect is the in-memory LeaseDialect + LockDialect + InboxDialect used
// across source_unit_test.go, outbound_unit_test.go, and inbox_unit_test.go.
// The zero value is not usable; build one with newFakeDialect.
type fakeDialect struct {
	mu     sync.Mutex
	rows   map[int64]*fakeRow
	nextID int64

	// tables tracks which table names have had EnsureSchema/EnsureInboxSchema
	// called (or been explicitly marked ready via markReady) — SchemaExists
	// reports false for any table not in this set, mirroring a fresh,
	// unprovisioned real dialect.
	tables map[string]bool
	// uniqueIndex tracks which inbox tables report a msg_id unique
	// constraint (InboxDeduper.Ready's second probe).
	uniqueIndex map[string]bool

	// processed is the dedup-inbox state: msgID -> processed_at.
	processed map[string]time.Time

	// now is the injectable clock (defaults to time.Now); tests fix it for
	// deterministic visibleAfter/leaseUntil assertions.
	now func() time.Time

	// Error injection, so Source.Poll/Outbound.Send's classifyQueryErr
	// wrapping (ErrSchemaNotReady vs raw passthrough) is directly testable
	// without a real DB.
	claimErr     error
	claimLockErr error
	insertErr    error
	nackErr      error // forces the corrupt-row penalty Nack to fail (pollLease)
	nackLockErr  error // forces the corrupt-row penalty NackLock to fail (pollLock)

	// uniqueIndexErr forces MsgIDUniqueIndexExists to error, covering
	// InboxDeduper.Ready's second-probe error-passthrough branch.
	uniqueIndexErr error

	// schemaExistsErr forces SchemaExists to error, covering
	// InboxDeduper.Ready's FIRST-probe error-passthrough branch (Ready must
	// return a genuine infrastructure error unchanged, never masking it as
	// ErrSchemaNotReady — moved here from the root mysql-backed
	// TestInboxDeduper_ReadyPassesThroughProbeError, Plan 006 Task 5, since no
	// built-in dialect remains in root to reproduce it against a real closed
	// pool).
	schemaExistsErr error

	// lastInsertQuerier records the Querier the most recent Insert call
	// received, so Outbound's resolveQuerier branches (pool vs resolved tx)
	// are directly assertable.
	lastInsertQuerier msginsql.Querier
}

func newFakeDialect() *fakeDialect {
	return &fakeDialect{
		rows:        make(map[int64]*fakeRow),
		tables:      make(map[string]bool),
		uniqueIndex: make(map[string]bool),
		processed:   make(map[string]time.Time),
		now:         time.Now,
	}
}

// sortedIDsLocked returns claimed row ids in ascending order (deterministic
// claim ordering, mirroring the real dialects' ORDER BY visible_after, id).
// Callers must hold f.mu.
func (f *fakeDialect) sortedIDsLocked() []int64 {
	ids := make([]int64, 0, len(f.rows))
	for id := range f.rows {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}

// insertRow seeds a claimable row directly (test setup helper), independent
// of Insert's delay handling.
func (f *fakeDialect) insertRow(msgID string, headers, payload []byte) int64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextID++
	id := f.nextID
	f.rows[id] = &fakeRow{id: id, msgID: msgID, headers: headers, payload: payload}
	return id
}

// row returns a snapshot copy of the row (test assertion helper).
func (f *fakeDialect) row(id int64) fakeRow {
	f.mu.Lock()
	defer f.mu.Unlock()
	r := f.rows[id]
	if r == nil {
		return fakeRow{}
	}
	return *r
}

// exists reports whether id is still present (not Ack-deleted).
func (f *fakeDialect) exists(id int64) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, ok := f.rows[id]
	return ok
}

// rowCount reports how many rows remain.
func (f *fakeDialect) rowCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.rows)
}

// onlyRow requires exactly one row to exist and returns a snapshot of it —
// an Outbound.Send assertion helper.
func (f *fakeDialect) onlyRow(t *testing.T) fakeRow {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	require.Len(t, f.rows, 1)
	for _, r := range f.rows {
		return *r
	}
	return fakeRow{}
}

// bumpEpoch simulates another worker having re-claimed the row (a fence
// mismatch), so the ORIGINAL claim's Ack/Nack closure becomes stale.
func (f *fakeDialect) bumpEpoch(id int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r := f.rows[id]; r != nil {
		r.leaseEpoch++
		r.lockedBy += "-reclaimed"
	}
}

// markReady marks table as schema-initialized (SchemaExists -> true).
func (f *fakeDialect) markReady(table string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tables[table] = true
}

// setUniqueIndex configures whether table's msg_id column reports a unique
// constraint (InboxDeduper.Ready's second probe).
func (f *fakeDialect) setUniqueIndex(table string, v bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.uniqueIndex[table] = v
}

// seedProcessed directly seeds an inbox dedup entry (test setup helper for
// Purge), bypassing InsertInboxIfAbsent.
func (f *fakeDialect) seedProcessed(msgID string, at time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.processed[msgID] = at
}

// processedCount reports how many dedup entries are recorded.
func (f *fakeDialect) processedCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.processed)
}

// ---- LeaseDialect ---------------------------------------------------------

func (f *fakeDialect) Claim(_ context.Context, _ msginsql.Querier, _ string, limit int, lockedBy string, leaseTTL time.Duration) ([]msginsql.ClaimedRow, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.claimErr != nil {
		return nil, f.claimErr
	}

	now := f.now()
	var claimed []msginsql.ClaimedRow
	for _, id := range f.sortedIDsLocked() {
		if len(claimed) >= limit {
			break
		}
		row := f.rows[id]
		if !row.visibleAfter.IsZero() && row.visibleAfter.After(now) {
			continue // not yet visible
		}
		if !row.leaseUntil.IsZero() && row.leaseUntil.After(now) {
			continue // still leased by someone else
		}
		row.deliveryCount++
		row.leaseEpoch++
		row.lockedBy = lockedBy
		row.leaseUntil = now.Add(leaseTTL)
		claimed = append(claimed, msginsql.ClaimedRow{
			ID:            row.id,
			MsgID:         row.msgID,
			Headers:       row.headers,
			Payload:       row.payload,
			DeliveryCount: row.deliveryCount,
			LeaseEpoch:    row.leaseEpoch,
		})
	}
	return claimed, nil
}

func (f *fakeDialect) Ack(_ context.Context, _ msginsql.Querier, _ string, id int64, lockedBy string, epoch int64) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	row, ok := f.rows[id]
	if !ok || row.lockedBy != lockedBy || row.leaseEpoch != epoch {
		return false, nil
	}
	delete(f.rows, id)
	return true, nil
}

func (f *fakeDialect) Nack(_ context.Context, _ msginsql.Querier, _ string, id int64, lockedBy string, epoch int64, delay time.Duration) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.nackErr != nil {
		return false, f.nackErr
	}
	row, ok := f.rows[id]
	if !ok || row.lockedBy != lockedBy || row.leaseEpoch != epoch {
		return false, nil
	}
	row.lockedBy = ""
	row.leaseUntil = time.Time{}
	row.visibleAfter = f.now().Add(delay)
	return true, nil
}

func (f *fakeDialect) Insert(_ context.Context, q msginsql.Querier, _ string, msgID string, headers, payload []byte, delay time.Duration) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lastInsertQuerier = q
	if f.insertErr != nil {
		return f.insertErr
	}
	f.nextID++
	f.rows[f.nextID] = &fakeRow{
		id: f.nextID, msgID: msgID, headers: headers, payload: payload,
		visibleAfter: f.now().Add(delay),
	}
	return nil
}

func (f *fakeDialect) EnsureSchema(_ context.Context, _ msginsql.Querier, table string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tables[table] = true
	return nil
}

func (f *fakeDialect) SchemaExists(_ context.Context, _ msginsql.Querier, table string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.schemaExistsErr != nil {
		return false, f.schemaExistsErr
	}
	return f.tables[table], nil
}

// ---- LockDialect ------------------------------------------------------

func (f *fakeDialect) ClaimLock(_ context.Context, _ msginsql.Querier, _ string, lockedBy string) (*msginsql.LockedRow, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.claimLockErr != nil {
		return nil, f.claimLockErr
	}

	now := f.now()
	for _, id := range f.sortedIDsLocked() {
		row := f.rows[id]
		if row.locked {
			continue
		}
		if !row.visibleAfter.IsZero() && row.visibleAfter.After(now) {
			continue
		}
		row.locked = true
		row.deliveryCount++
		row.lockedBy = lockedBy
		return &msginsql.LockedRow{
			ID:            row.id,
			MsgID:         row.msgID,
			Headers:       row.headers,
			Payload:       row.payload,
			DeliveryCount: row.deliveryCount,
			Tx:            nil, // the engine wiring never dereferences this (audit F6)
		}, nil
	}
	return nil, nil
}

func (f *fakeDialect) AckLock(_ context.Context, lr *msginsql.LockedRow, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.rows, lr.ID)
	return nil
}

func (f *fakeDialect) NackLock(_ context.Context, lr *msginsql.LockedRow, _ string, delay time.Duration) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.nackLockErr != nil {
		return f.nackLockErr
	}
	row, ok := f.rows[lr.ID]
	if !ok {
		return nil
	}
	row.locked = false
	row.lockedBy = ""
	row.visibleAfter = f.now().Add(delay)
	return nil
}

// ---- InboxDialect -----------------------------------------------------

func (f *fakeDialect) InsertInboxIfAbsent(_ context.Context, _ msginsql.Querier, _ string, msgID string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.processed[msgID]; ok {
		return true, nil
	}
	f.processed[msgID] = f.now()
	return false, nil
}

func (f *fakeDialect) PurgeInbox(_ context.Context, _ msginsql.Querier, _ string, olderThan time.Duration) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	cutoff := f.now().Add(-olderThan)
	var n int64
	for id, at := range f.processed {
		if at.Before(cutoff) {
			delete(f.processed, id)
			n++
		}
	}
	return n, nil
}

func (f *fakeDialect) EnsureInboxSchema(_ context.Context, _ msginsql.Querier, table string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tables[table] = true
	return nil
}

func (f *fakeDialect) MsgIDUniqueIndexExists(_ context.Context, _ msginsql.Querier, table string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.uniqueIndexErr != nil {
		return false, f.uniqueIndexErr
	}
	return f.uniqueIndex[table], nil
}

// Compile-time assertions that fakeDialect satisfies all three SPIs.
var (
	_ msginsql.LeaseDialect = (*fakeDialect)(nil)
	_ msginsql.LockDialect  = (*fakeDialect)(nil)
	_ msginsql.InboxDialect = (*fakeDialect)(nil)
)

// encodeHeadersT frames a minimal Headers set carrying only msgin.message-id, for
// tests that need a decodable headers blob (DecodeHeaders' happy path).
func encodeHeadersT(t *testing.T, id string) []byte {
	t.Helper()
	b, err := msginsql.EncodeHeaders(msgin.NewHeaders(map[string]any{msgin.HeaderMessageID: id}))
	require.NoError(t, err)
	return b
}

// beginNoOpTx opens a *sql.Tx on the shared stub driver (locktx_unit_test.go)
// purely to satisfy InboxDeduper.MarkProcessed's concrete *sql.Tx parameter;
// the fake InboxDialect never issues a statement on it, so its settlement
// (commit/rollback) is irrelevant here — only its non-nilness matters.
func beginNoOpTx(t *testing.T) *stdsql.Tx {
	t.Helper()
	db, _ := newStubDB(t, dsnOK)
	tx, err := db.BeginTx(t.Context(), nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback() })
	return tx
}
