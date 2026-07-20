# Aggregator + MessageGroupStore (Phase 2) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax.
>
> **Go skills are mandatory (CLAUDE.md writing-plans override):** every task starts from **`cc-skills-golang:golang-how-to`**, uses **`superpowers:test-driven-development`** (red→green→refactor), navigates/refactors via **`gopls`**, and obeys the project test overrides — **`table-test`** (assert-closure tables, `t.Context()`), **`use-mockgen`**, **`use-testcontainers`**. Phase 2 is stdlib + `clockwork` + `goleak` (test); no containers (sql is Phase 3).

**Goal:** Ship the EIP **Aggregator** (`NewAggregator[A,B]`) — an N→1 endpoint that correlates messages into groups, holds them in a swappable **`MessageGroupStore`**, releases the aggregate when a group completes, and reaps expired partial groups — plus the core SPI and its in-memory reference store (`memory.GroupStore`).

**Architecture:** A core `MessageGroupStore` SPI (mirrors `ChannelStore`), a `Run(ctx)`-owning `*Aggregator` struct implementing `MessageHandler`, and a goleak-clean `memory.GroupStore`. Hold-until-release settlement: `store.Add` persists before the source Acks; release forwards to the output channel before `store.Remove` → documented at-least-once (within-process on memory). No core runtime change; the Aggregator drives off the existing `Consumer`.

**Tech Stack:** Go 1.25; core stdlib + `clockwork`; `adapter/memory` (clockwork); tests blackbox `_test`, testify, `goleak`.

## Global Constraints

- **Go 1.25 pinned:** build/test with `GOTOOLCHAIN=go1.25.12`.
- **Module path** `github.com/kartaladev/msgin`; core in root `package msgin`; the store impl in `package memory` (`adapter/memory`). **Dependency points inward** — core must NOT import `adapter/memory`.
- **No new dependency.** `clockwork`/`goleak` already present; `go mod tidy` unchanged.
- **Blackbox tests only** (`package msgin_test`, `package memory_test`); assert-closure tables; `t.Context()`; **`goleak`** on anything starting a goroutine (the Aggregator reaper). Inject time via `clockwork` (`WithClock`), drive with `clockwork.NewFakeClock()`.
- **No panics on caller input:** every construction-time validation returns a typed error (`ErrNilStore`, `ErrNilFunc`, `ErrNilOutput`, `ErrExpiryChannelRequired`); runtime type/correlation failures are permanent typed errors routed to the invalid-message channel (`ErrPayloadType`, `ErrNoCorrelation`).
- **Hot-path branch coverage mandatory:** every construction-validation branch, the correlate/add/release/hold branches, the number-tolerant size read, the reaper tick + expiry routing, and idempotent re-Add each need a covering test.
- **Full pre-commit gate** on the final task: `go build ./...`, `go test ./... -race`, `go vet`, `golangci-lint run ./...`, `gofmt -l .` (empty), `CGO_ENABLED=0 go build ./...`, `go mod tidy` no-diff, `govulncheck ./...` — all clean.
- **Commit trailers:** `Spec: 009`, `Plan: 016`, `ADR: 0020`.
- **Traceability:** [Spec 009](../specs/009-splitter-aggregator-endpoints.md) §3.2/§3.3; [ADR 0020](../adrs/0020-splitter-aggregator-group-store.md) §2/§3/§4/§5; builds on [ADR 0018](../adrs/0018-persistent-queue-channel.md) (the `ChannelStore` memory/sql precedent this mirrors) and [ADR 0013](../adrs/0013-composition-endpoints.md).

## Resolved design decisions (fold from ADR 0020 + this plan)

- **Constructor:** `NewAggregator[A,B](store MessageGroupStore, fn func(ctx, []Message[A]) (Message[B], error), opts ...AggregatorOption) (*Aggregator, error)` — store positional (mirrors `NewQueueChannel`); output channel via **required** `WithOutputChannel`.
- **Correlation default:** the `HeaderCorrelationID` header (string); missing/empty → `ErrNoCorrelation`. Override `WithCorrelationStrategy`.
- **Release default:** `len(group.Messages()) >= HeaderSequenceSize` read **number-tolerantly** (`int`/`int64`/`float64`); no sequence-size header present → never completes via size (relies on `WithCompletionSize(n)` / `WithReleaseStrategy` / expiry). Overrides: `WithCompletionSize(n)`, `WithReleaseStrategy`.
- **Expiry:** no default timeout (off). `WithGroupTimeout(d)` **requires** `WithExpiredGroupChannel(ch)` (else `ErrExpiryChannelRequired`). Reaper interval defaults to `d` (a group is reaped within `[d, 2d)`); `WithClock` injects time.
- **`memory.GroupStore`:** `NewGroupStore(opts ...GroupStoreOption)`; bounds the number of **groups** — `WithMaxGroups(n)` default **1024**; a new key beyond the cap → `ErrOverflowDropped` (reject; never evict a partial group — that would be silent loss). Idempotent `Add` by `msg.ID()`; id-less messages are appended without dedup (documented — not redelivery-idempotent). `Messages()`/`Expired()` return **snapshots** (cloned slices) to preserve immutability. `EmitsLiveValue()→true`. **No goroutine.**

---

## ⚠️ Audit fold (round 1 → NEEDS-REVISION → folded here; round-2 re-audit PENDING before SDD)

The round-1 Opus audit found a real concurrency design bug and correctness gaps. **These directives OVERRIDE the
task code below where they conflict** — apply them; the round-2 re-audit verifies them.

- **H-1 (concurrency) — the SPI + Aggregator change.**
  1. **`MessageGroupStore.Remove` returns the removed group:** signature is `Remove(ctx context.Context, key string)
     (MessageGroup, error)` (nil group if the key was absent). `memory.GroupStore.Remove` deletes the key and
     returns a snapshot of what it removed (or `nil`). Update Task 1's interface, the memory impl, and the
     conformance test accordingly.
  2. **Aggregator holds a sharded per-correlation-key lock.** Add `locks [256]sync.Mutex` (or similar) to the
     `Aggregator`; a `func (a *Aggregator) keyLock(key string) *sync.Mutex` hashing `key` (e.g. `fnv32a`). In
     `Handle`, `mu := a.keyLock(key); mu.Lock(); defer mu.Unlock()` **wrapping the whole** correlate-result→Add→
     release-check→agg→Send→Remove sequence (acquire AFTER computing the key, since the key is the lock selector).
     Different keys → different (mostly) shards → concurrent. The reaper (Task 3) acquires the **same** `keyLock(g.Key())`
     around its re-check+Remove+route for each expired group.
  3. **Concurrency proof:** Task 4 MUST add an N>1 same-key stress test (many goroutines feeding correlated members
     through one `Aggregator.Handle` under a real `memory.GroupStore`) asserting **exactly one** aggregate is emitted
     and **no member is lost** — `-race` cannot catch this class, so this test is the gate.
  4. Document (godoc + ADR): correct under `WithConcurrency>1` within one process; multi-process durable = Phase 3
     SQL transactional release. Exact-count (`==n`) release is racy under concurrency → steer to `>=`/worker=1.
- **M-1 — `ErrNoCorrelation` must be permanent.** `defaultCorrelate` returns `Permanent(ErrNoCorrelation)` (verify
  `Permanent` exists in reliability.go; `isPermanent` does NOT match a bare `ErrNoCorrelation`, so without the wrap
  the runtime retries it to the DLQ instead of the invalid-message channel). Test the **routing** (permanent), not
  just that `Handle` returns the error.
- **M-2 — ingress fail-fast.** In `Handle`, assert `if _, err := PayloadOf[A](msg); err != nil { return err }`
  **before** `store.Add` (a stored `assert func(Message[any]) error` closure set in `NewAggregator[A,B]`, since the
  struct is non-generic). Test that a mistyped message leaves the store group **empty** (never Added).
- **M-3 — error-branch tests.** Add a fake `MessageGroupStore` (Add→sentinel) and fake `MessageChannel`
  (Send→sentinel) to cover `store.Add` error and `output.Send` error; assert the group is **not** removed on Send
  error (retry re-releases).
- **L-1** godoc: a persistently-failing `agg` orphans the group until expiry (recommend `WithGroupTimeout`).
  **L-2** note id-less duplicates can cause a spurious `>=` release (edge; source messages carry ids).
  **L-3** the reaper test MUST `clock.BlockUntil(1)` BEFORE `clock.Advance(...)` (else the tick is lost → hang/flake).
  **L-4** `Run` with no timeout = **block until ctx cancel, return `ctx.Err()`**; drop the unused `strings` import.

### Round-2 re-audit folds (verdict SOUND-WITH-NITS — fold these, no round-3 needed)

- **F1 (reaper re-check — the one real gap).** The reaper must route only the group it *atomically removed*, and only if
  it is *still expired* — the SPI has no keyed read, so "re-check before Remove" is impossible; re-check the
  `Remove`-returned group instead. **Exact reaper inner loop per expired key `g` from `Expired(cutoff)`:**
  ```go
  mu := a.keyLock(g.Key()); mu.Lock()
  removed, err := a.store.Remove(ctx, g.Key())
  if err != nil || removed == nil {         // released concurrently, or gone
      mu.Unlock(); continue
  }
  if !removed.CreatedAt().Before(cutoff) {   // a FRESH group re-formed at this key since Expired()
      for _, m := range removed.Messages() { _, _ = a.store.Add(ctx, g.Key(), m) } // restore (idempotent)
      mu.Unlock(); continue
  }
  for _, m := range removed.Messages() { _ = a.cfg.expired.Send(ctx, m) }
  mu.Unlock()
  ```
  Without the `CreatedAt().Before(cutoff)` re-check, a group removed+refilled by a concurrent `Handle` between
  `Expired()` and the lock would be prematurely diverted to the expired channel (a live, still-fillable group). Test
  it: two goroutines, one reaping, one refilling the same key across the tick — assert the refilled group is NOT
  expired-routed.
- **F2 (cyclic self-deadlock — document).** `Handle`/reaper hold `keyLock(key)` across `output.Send`/`expired.Send`,
  and `DirectChannel.Send` runs the subscriber synchronously. Godoc on `NewAggregator`/`WithOutputChannel`/
  `WithExpiredGroupChannel`: **never wire an aggregator's output or expired channel back into its own input under the
  same correlation key** (non-reentrant mutex → hard deadlock). Non-cyclic wiring is fine.
- **F3 (scope the `==n` caveat).** WITH the per-key lock, exact-count (`==n`) release is **safe within a single
  process** (members serialize per key; the group is removed the instant it hits `n`; idempotent `Add` blocks
  redelivery double-count). The caveat is genuinely true only for **multi-process durable** (Phase 3 sql, no
  cross-process lock) or **id-less** over-delivery. Correct the ADR §3 + Spec D16 caveat wording accordingly (still
  fine to *recommend* `>=`, but the single-process `==n` race justification is wrong post-lock).
- **F4 (de-stale the inline snippets — they won't compile against the 2-return `Remove`).** The task code below is
  SUPERSEDED where it conflicts; specifically, an implementer MUST use:
  - Task 1 `MessageGroupStore.Remove` and `memory.GroupStore.Remove`: `Remove(ctx, key string) (MessageGroup, error)`
    returning a **cloned** (`slices.Clone`) snapshot of the removed group, or `(nil, nil)` if the key was absent (F6).
  - Task 1 conformance test line `require.NoError(t, s.Remove(t.Context(), "k"))` → `_, err := s.Remove(t.Context(), "k"); require.NoError(t, err)`.
  - Task 2 `Handle`: wrap in the per-key lock, add the ingress `PayloadOf[A]` assert before `store.Add`, and
    `_, err := a.store.Remove(ctx, key); return err` (ignore the returned group on the release path).
  - Task 3 reaper: use the F1 loop above (keyLock + `Remove`-returns-group + re-check), not the stale
    `_ = a.store.Remove(...)` single-assign.
- **F5 (expired-Send error).** Decide + document: on `expired.Send` error the reaper currently removes-then-drops.
  Prefer routing before Remove is already done (F1 removes first); at minimum **document** that a failed expired-Send
  drops the group (or re-Add on Send error for at-least-once to the expired sink — implementer's call, documented).
- **F6 (clone `Remove`'s snapshot)** — see F4; `Remove` returns `slices.Clone`d members like `Add`/`Expired`.

**Confirmed correct by BOTH audit rounds (build on as-is):** the per-key lock design (closes H-1, no ABBA deadlock —
consistent `keyLock→storeMutex` order, stable `fnv%256` keyed on the correlation key), `Permanent(ErrNoCorrelation)`
+ ingress `ErrPayloadType` both route to the invalid-message channel (verified vs `consumer.go` `dispatch`/`isPermanent`),
generic boxing, clockwork v0.5.0 ticker API (`NewTicker(d).Chan()/.Stop()`, `FakeClock.Advance/BlockUntil`), snapshot
immutability, `asInt` tolerance, dependency-inward, the 4-task split.

---

## File Structure

- **Create `groupstore.go`** (core) — `MessageGroup` + `MessageGroupStore` interfaces.
- **Create `aggregator.go`** (core) — `NewAggregator`, `Aggregator`, `AggregatorOption` + all `WithX`, `Handle`, `Run`, the reaper, correlation/release defaults, the number-tolerant `asInt` helper.
- **Modify `errors.go`** — add `ErrNoCorrelation`, `ErrNilOutput`, `ErrExpiryChannelRequired`.
- **Create `adapter/memory/groupstore.go`** — `memory.GroupStore` + `GroupStoreOption`/`WithMaxGroups`/`WithClock`.
- **Tests:** `groupstore_conformance_test.go` (a shared store contract table, run against `memory.GroupStore`), `aggregator_test.go`, `adapter/memory/groupstore_test.go`, `example_aggregator_test.go`.

Existing symbols reused: `Message`, `Headers`, `PayloadOf`, `boxMessage`, `MessageHandler`, `MessageChannel`, `HeaderCorrelationID`, `HeaderSequenceSize`, `OverflowPolicy`/`ErrOverflowDropped`, `ErrNilStore`, `ErrNilFunc`, `ErrPayloadType`, `clockwork.Clock`.

---

## Task 1: `MessageGroupStore` SPI (core) + `memory.GroupStore` + conformance table

Deliver the core interfaces and the in-memory reference store, verified by a store-contract conformance table (goleak-clean).

**Files:** Create `groupstore.go`; create `adapter/memory/groupstore.go`; test `groupstore_conformance_test.go` (drives `memory.GroupStore` through the core interface), `adapter/memory/groupstore_test.go` (memory-specifics: capacity/overflow).

**Interfaces:**
- Produces (core `groupstore.go`):
```go
type MessageGroup interface {
	Key() string
	Messages() []Message[any]
	CreatedAt() time.Time
}
type MessageGroupStore interface {
	Add(ctx context.Context, key string, msg Message[any]) (MessageGroup, error)
	Remove(ctx context.Context, key string) error
	Expired(ctx context.Context, before time.Time) ([]MessageGroup, error)
	EmitsLiveValue() bool
}
```
- Produces (`adapter/memory`): `func NewGroupStore(opts ...GroupStoreOption) (*GroupStore, error)`; `type GroupStoreOption func(*groupStoreConfig)`; `func WithMaxGroups(n int) GroupStoreOption`; `func WithClock(c clockwork.Clock) GroupStoreOption`.

- [ ] **Step 1: Write the core interfaces** (`groupstore.go`)

```go
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
	// Remove deletes an entire group (after a successful release).
	Remove(ctx context.Context, key string) error
	// Expired returns snapshots of groups whose CreatedAt is strictly before the
	// cutoff (for the Aggregator's expiry reaper).
	Expired(ctx context.Context, before time.Time) ([]MessageGroup, error)
	// EmitsLiveValue reports live Go values (memory) vs []byte (wire, sql), for
	// codec pairing — the same mechanism ChannelStore uses.
	EmitsLiveValue() bool
}
```

- [ ] **Step 2: Write the failing conformance test** (`groupstore_conformance_test.go`)

A table that any `MessageGroupStore` must pass; run against `memory.GroupStore`. Cases (each a closure over a fresh store from a `newStore func() msgin.MessageGroupStore` factory): Add returns a growing group in arrival order; **idempotent Add by id** (same id twice → group size 1); two keys are independent; Remove drops a group (subsequent Add starts fresh); Expired returns groups older than the cutoff and excludes newer ones (drive `CreatedAt` via the store's fake clock); `EmitsLiveValue()==true`. Use `goleak.VerifyNone(t)` at the end of each (the store starts no goroutine).

```go
package msgin_test

import (
	"testing"
	"time"

	"github.com/jonboulle/clockwork"
	"github.com/kartaladev/msgin"
	"github.com/kartaladev/msgin/adapter/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
)

func TestGroupStore_Conformance(t *testing.T) {
	clock := clockwork.NewFakeClock()
	newStore := func() msgin.MessageGroupStore {
		s, err := memory.NewGroupStore(memory.WithClock(clock))
		require.NoError(t, err)
		return s
	}
	msg := func(id string) msgin.Message[any] { return msgin.New[any](id, msgin.WithID(id)) }

	tests := []struct {
		name   string
		assert func(t *testing.T, s msgin.MessageGroupStore)
	}{
		{
			name: "Add grows the group in arrival order",
			assert: func(t *testing.T, s msgin.MessageGroupStore) {
				_, _ = s.Add(t.Context(), "k", msg("a"))
				g, err := s.Add(t.Context(), "k", msg("b"))
				require.NoError(t, err)
				assert.Equal(t, "k", g.Key())
				assert.Len(t, g.Messages(), 2)
				assert.Equal(t, "a", g.Messages()[0].ID())
			},
		},
		{
			name: "Add is idempotent by id",
			assert: func(t *testing.T, s msgin.MessageGroupStore) {
				_, _ = s.Add(t.Context(), "k", msg("a"))
				g, err := s.Add(t.Context(), "k", msg("a")) // same id
				require.NoError(t, err)
				assert.Len(t, g.Messages(), 1)
			},
		},
		{
			name: "keys are independent",
			assert: func(t *testing.T, s msgin.MessageGroupStore) {
				_, _ = s.Add(t.Context(), "k1", msg("a"))
				g, _ := s.Add(t.Context(), "k2", msg("b"))
				assert.Len(t, g.Messages(), 1)
			},
		},
		{
			name: "Remove drops the group",
			assert: func(t *testing.T, s msgin.MessageGroupStore) {
				_, _ = s.Add(t.Context(), "k", msg("a"))
				require.NoError(t, s.Remove(t.Context(), "k"))
				g, _ := s.Add(t.Context(), "k", msg("b"))
				assert.Len(t, g.Messages(), 1) // fresh group
			},
		},
		{
			name: "Expired returns only groups older than the cutoff",
			assert: func(t *testing.T, s msgin.MessageGroupStore) {
				_, _ = s.Add(t.Context(), "old", msg("a"))
				clock.Advance(10 * time.Second)
				cutoff := clock.Now()
				_, _ = s.Add(t.Context(), "new", msg("b"))
				exp, err := s.Expired(t.Context(), cutoff)
				require.NoError(t, err)
				require.Len(t, exp, 1)
				assert.Equal(t, "old", exp[0].Key())
			},
		},
		{
			name: "EmitsLiveValue is true",
			assert: func(t *testing.T, s msgin.MessageGroupStore) {
				assert.True(t, s.EmitsLiveValue())
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			defer goleak.VerifyNone(t)
			tc.assert(t, newStore())
		})
	}
}
```

- [ ] **Step 3: Run — verify it fails** (`undefined: memory.NewGroupStore`). `GOTOOLCHAIN=go1.25.12 go test ./... -run 'TestGroupStore_Conformance'` → FAIL (compile).

- [ ] **Step 4: Implement `memory.GroupStore`** (`adapter/memory/groupstore.go`)

```go
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

// WithClock injects the clock used to stamp group CreatedAt (default real clock;
// tests pass clockwork.NewFakeClock()).
func WithClock(c clockwork.Clock) GroupStoreOption {
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

func (s *GroupStore) Remove(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.groups, key)
	return nil
}

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

// snapshot is an immutable MessageGroup view returned by Add/Expired.
type snapshot struct {
	key       string
	msgs      []msgin.Message[any]
	createdAt time.Time
}

func (s snapshot) Key() string                     { return s.key }
func (s snapshot) Messages() []msgin.Message[any]   { return s.msgs }
func (s snapshot) CreatedAt() time.Time             { return s.createdAt }
```

- [ ] **Step 5: Run conformance — verify it passes.** `GOTOOLCHAIN=go1.25.12 go test ./... -run 'TestGroupStore_Conformance' -race -v` → PASS.

- [ ] **Step 6: memory-specifics test** (`adapter/memory/groupstore_test.go`, `package memory_test`): `NewGroupStore(WithMaxGroups(0))` → `msgin.ErrInvalidCapacity`; at `WithMaxGroups(1)`, adding a 2nd distinct key → `msgin.ErrOverflowDropped` (a 2nd message to the SAME key still succeeds). `goleak.VerifyNone` in each. Then run `GOTOOLCHAIN=go1.25.12 go test ./adapter/memory/... -race`.

- [ ] **Step 7: Commit** — stage `groupstore.go adapter/memory/groupstore.go groupstore_conformance_test.go adapter/memory/groupstore_test.go docs/plans/016-aggregator.md docs/adrs/0020-splitter-aggregator-group-store.md` (design docs ride with this first Phase-2 code commit). Message `feat(core): MessageGroupStore SPI + memory.GroupStore` with trailers `Spec: 009 / Plan: 016 / ADR: 0020`. Do NOT stage `.claude/settings.json`.

---

## Task 2: Aggregator core — correlate, release, aggregate (no reaper yet)

Deliver `NewAggregator` + `Handle` for the synchronous path: validate construction, correlate, `store.Add`, release-check (default size / `WithCompletionSize` / `WithReleaseStrategy`), aggregate, forward to the output channel, `store.Remove`. Expiry/`Run` land in Task 3.

**Files:** Create `aggregator.go`; modify `errors.go` (add `ErrNoCorrelation`, `ErrNilOutput`, `ErrExpiryChannelRequired`); test `aggregator_test.go`.

**Interfaces:**
- Produces: `func NewAggregator[A, B any](store MessageGroupStore, fn func(ctx context.Context, group []Message[A]) (Message[B], error), opts ...AggregatorOption) (*Aggregator, error)`; `type Aggregator struct{…}` with `Handle(ctx, Message[any]) error`; `type AggregatorOption func(*aggregatorConfig)`; `WithOutputChannel(MessageChannel)`, `WithCorrelationStrategy(func(Message[any]) (string, error))`, `WithReleaseStrategy(func(MessageGroup) bool)`, `WithCompletionSize(int)`. (`WithGroupTimeout`/`WithExpiredGroupChannel`/`WithClock`/`Run` added in Task 3.)

- [ ] **Step 1: Add error sentinels** (`errors.go`)

```go
	// ErrNoCorrelation is returned when an Aggregator's correlation strategy
	// yields no key for a message (permanent → invalid-message channel).
	ErrNoCorrelation = errors.New("msgin: message has no correlation key")
	// ErrNilOutput is returned by NewAggregator when no output channel is set.
	ErrNilOutput = errors.New("msgin: aggregator output channel is nil")
	// ErrExpiryChannelRequired is returned by NewAggregator when WithGroupTimeout
	// is set without WithExpiredGroupChannel (expiry must have a visible sink).
	ErrExpiryChannelRequired = errors.New("msgin: group timeout requires an expired-group channel")
```

- [ ] **Step 2: Write failing `aggregator_test.go`** (`package msgin_test`). Table over `Handle`, driving a real `memory.GroupStore` and a capturing output `MessageChannel` (a `*msgin.DirectChannel` with a recording subscriber, or a small fake `MessageChannel`). Cases:
  - construction: nil store → `ErrNilStore`; nil fn → `ErrNilFunc`; no `WithOutputChannel` → `ErrNilOutput`.
  - correlation default: two messages sharing `HeaderCorrelationID="g"` with `HeaderSequenceSize=2` → on the 2nd `Handle`, `fn` runs over both and the aggregate is sent to output; group removed (a 3rd message to "g" starts a new group).
  - missing correlation → `ErrNoCorrelation`.
  - hold: 1st of 2 → output NOT called, `Handle` returns nil.
  - `WithCompletionSize(2)` releases at 2 regardless of sequence headers.
  - `WithReleaseStrategy` custom predicate.
  - number-tolerant size: `HeaderSequenceSize` as `float64(2)` (simulating an sql round-trip) still releases at 2.
  - wrong payload type → `ErrPayloadType`.
  - `fn` error propagates and the group is NOT removed (so a retry can re-release).

- [ ] **Step 3: Run — verify fail** (`undefined: msgin.NewAggregator`).

- [ ] **Step 4: Implement `aggregator.go`**

```go
package msgin

import (
	"context"
	"strings"
)

// Aggregator is an EIP Aggregator endpoint: it correlates incoming messages into
// groups held in a MessageGroupStore, and when a group's release strategy is
// satisfied it aggregates the group's payloads (fn) into one Message[B] sent to
// the output channel. It implements MessageHandler; place it as a chain's next
// (Subscribe) or a flow head (NewConsumer[any](src, agg.Handle)). Optionally it
// reaps expired partial groups via Run(ctx) — see WithGroupTimeout (Task 3).
//
// Settlement: store.Add persists a member before Handle returns (before the
// source Acks), and a release forwards to the output channel before store.Remove
// — documented at-least-once (within-process on memory.GroupStore; across
// restart on sql.GroupStore). A member redelivered after its group already
// completed forms a fresh group (idempotent downstream expected).
type Aggregator struct {
	store  MessageGroupStore
	fn     func(ctx context.Context, group []Message[any]) (Message[B_], error) // see NOTE
	cfg    aggregatorConfig
}
```

> **NOTE for the implementer — generic boxing.** `Aggregator` is a non-generic struct (like `Router`), but `fn` is generic `[]Message[A]→Message[B]`. Box `fn` at construction into an `any`-typed closure the struct stores:
> ```go
> type Aggregator struct {
> 	store MessageGroupStore
> 	agg   func(ctx context.Context, group []Message[any]) (Message[any], error) // boxed
> 	cfg   aggregatorConfig
> }
> ```
> In `NewAggregator[A,B]`, wrap the typed `fn`:
> ```go
> agg := func(ctx context.Context, group []Message[any]) (Message[any], error) {
> 	typed := make([]Message[A], len(group))
> 	for i, m := range group {
> 		tm, err := PayloadOf[A](m) // re-assert; members passed ingress
> 		if err != nil { return Message[any]{}, err }
> 		typed[i] = tm
> 	}
> 	out, err := fn(ctx, typed)
> 	if err != nil { return Message[any]{}, err }
> 	return boxMessage(out), nil
> }
> ```
> This mirrors how `Transform`/`Router` keep a non-generic core with a typed constructor (ADR 0001/0013). The ingress `Handle` still asserts each message to `A` for fail-fast (`PayloadOf[A]`), discarding the typed result (it only needs the error) — the group re-assert above produces the slice `fn` consumes.

```go
type aggregatorConfig struct {
	output      MessageChannel
	correlate   func(Message[any]) (string, error)
	release     func(MessageGroup) bool
	// timeout/expired/clock added in Task 3
}

// AggregatorOption configures an Aggregator.
type AggregatorOption func(*aggregatorConfig)

// WithOutputChannel sets the (required) channel a released aggregate is sent to.
func WithOutputChannel(ch MessageChannel) AggregatorOption {
	return func(c *aggregatorConfig) { c.output = ch }
}

// WithCorrelationStrategy overrides how a message's group key is derived. The
// default reads HeaderCorrelationID; an empty key is ErrNoCorrelation.
func WithCorrelationStrategy(fn func(Message[any]) (string, error)) AggregatorOption {
	return func(c *aggregatorConfig) { c.correlate = fn }
}

// WithReleaseStrategy overrides when a group is complete. The default is
// len(group) >= HeaderSequenceSize (number-tolerant).
func WithReleaseStrategy(fn func(MessageGroup) bool) AggregatorOption {
	return func(c *aggregatorConfig) { c.release = fn }
}

// WithCompletionSize releases a group once it holds n messages — sugar for a
// fixed-size release when there is no HeaderSequenceSize.
func WithCompletionSize(n int) AggregatorOption {
	return func(c *aggregatorConfig) {
		c.release = func(g MessageGroup) bool { return len(g.Messages()) >= n }
	}
}

func defaultCorrelate(m Message[any]) (string, error) {
	if s, ok := m.Headers().String(HeaderCorrelationID); ok && s != "" {
		return s, nil
	}
	return "", ErrNoCorrelation
}

func defaultRelease(g MessageGroup) bool {
	if len(g.Messages()) == 0 {
		return false
	}
	size, ok := asInt(firstHeader(g.Messages(), HeaderSequenceSize))
	return ok && len(g.Messages()) >= size
}

func firstHeader(msgs []Message[any], key string) any {
	v, _ := msgs[0].Header(key)
	return v
}

// asInt reads a header number tolerant of the sql JSON framing (int/int64/float64).
func asInt(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case float64:
		return int(n), true
	default:
		return 0, false
	}
}

// NewAggregator builds an Aggregator. store and an output channel (WithOutputChannel)
// are required; a nil store/fn/output — or WithGroupTimeout without
// WithExpiredGroupChannel — is a construction error.
func NewAggregator[A, B any](
	store MessageGroupStore,
	fn func(ctx context.Context, group []Message[A]) (Message[B], error),
	opts ...AggregatorOption,
) (*Aggregator, error) {
	if store == nil {
		return nil, ErrNilStore
	}
	if fn == nil {
		return nil, ErrNilFunc
	}
	cfg := aggregatorConfig{correlate: defaultCorrelate, release: defaultRelease}
	for _, opt := range opts {
		opt(&cfg)
	}
	if cfg.output == nil {
		return nil, ErrNilOutput
	}
	// (Task 3 adds: if timeout set && expired channel nil → ErrExpiryChannelRequired.)
	agg := boxAggFn(fn)
	return &Aggregator{store: store, agg: agg, cfg: cfg}, nil
}

// Handle correlates msg, adds it to its group, and releases the aggregate when
// the group is complete.
func (a *Aggregator) Handle(ctx context.Context, msg Message[any]) error {
	key, err := a.cfg.correlate(msg)
	if err != nil {
		return err
	}
	group, err := a.store.Add(ctx, key, msg)
	if err != nil {
		return err
	}
	if !a.cfg.release(group) {
		return nil // held; source Acks — durability now on the store
	}
	out, err := a.agg(ctx, group.Messages())
	if err != nil {
		return err // group NOT removed → a retry can re-release
	}
	if err := a.cfg.output.Send(ctx, out); err != nil {
		return err // NOT removed → retry
	}
	return a.store.Remove(ctx, key)
}
```
(with `boxAggFn` = the boxing helper from the NOTE, and the ingress `PayloadOf[A]` assert — the implementer wires the assert into `Handle` via a stored `assert func(Message[any]) error` set in `NewAggregator`, OR folds the ingress assert into the correlate step; keep the fail-fast ErrPayloadType. `strings` import is for any trimming in defaultCorrelate if needed — drop if unused.)

- [ ] **Step 5: Run — verify pass** (`go test . -run 'TestAggregator' -race -v`). **Step 6: Commit** `feat(core): Aggregator core — correlate, release, aggregate` (stage `aggregator.go errors.go aggregator_test.go`; trailers).

---

## Task 3: Expiry reaper — `Run(ctx)`, `WithGroupTimeout`, `WithExpiredGroupChannel`

Add the optional reaper goroutine: `Run(ctx)` ticks on the group timeout, routes expired partial groups to the expired channel, and removes them. goleak proves clean start/stop/join and the no-timeout no-goroutine path.

**Files:** modify `aggregator.go` (config fields, options, `Run`, `WithClock`); test `aggregator_test.go` (reaper cases).

**Interfaces:** Produces `WithGroupTimeout(time.Duration)`, `WithExpiredGroupChannel(MessageChannel)`, `WithClock(clockwork.Clock)`, `func (a *Aggregator) Run(ctx context.Context) error`.

- [ ] **Step 1: Failing tests** — with a `clockwork.NewFakeClock`: set `WithGroupTimeout(30s)` + `WithExpiredGroupChannel(exp)`; Add one message to a group, `go agg.Run(ctx)`, advance the fake clock past the timeout, assert the partial group is sent to `exp` and removed (poll the recorder / use `clock.BlockUntil`). Construction: `WithGroupTimeout` without `WithExpiredGroupChannel` → `ErrExpiryChannelRequired`. goleak: `Run(ctx)` with timeout → cancel ctx → joins cleanly; NO `WithGroupTimeout` → `Run` starts no goroutine and returns promptly (or `Handle` works without `Run`).

- [ ] **Step 2: Implement** — add `timeout time.Duration`, `expired MessageChannel`, `clock clockwork.Clock` to `aggregatorConfig`; the options; `NewAggregator` validates `timeout>0 && expired==nil → ErrExpiryChannelRequired` and defaults `clock` to real. `Run`:
```go
func (a *Aggregator) Run(ctx context.Context) error {
	if a.cfg.timeout <= 0 {
		<-ctx.Done() // nothing to reap; block until cancelled (or return nil — pick per test)
		return ctx.Err()
	}
	ticker := a.cfg.clock.NewTicker(a.cfg.timeout)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.Chan():
			cutoff := a.cfg.clock.Now().Add(-a.cfg.timeout)
			groups, err := a.store.Expired(ctx, cutoff)
			if err != nil {
				continue // transient; try next tick
			}
			for _, g := range groups {
				// route each held member to the expired channel, then remove the group.
				for _, m := range g.Messages() {
					_ = a.cfg.expired.Send(ctx, m)
				}
				_ = a.store.Remove(ctx, g.Key())
			}
		}
	}
}
```
(Confirm the exact `clockwork.Clock` ticker API — `NewTicker(d).Chan()` — against the version in `go.mod`; adjust if the method names differ. Decide via the test whether the no-timeout `Run` blocks-until-cancel or returns nil immediately; document the chosen contract on `Run`'s godoc.)

- [ ] **Step 3: Run/verify + Commit** `feat(core): Aggregator expiry reaper (Run + WithGroupTimeout)`.

---

## Task 4: Settlement integration + Example + full gate

Prove the hold-until-release + idempotent-redelivery contract end-to-end, add the `Example`, and run the whole pre-commit gate.

**Files:** test `aggregator_test.go` (or a new `aggregator_settlement_test.go`); create `example_aggregator_test.go`.

- [ ] **Step 1** — Idempotent-redelivery test: Add the same 2 members twice (simulating a source redelivery via duplicate ids) → the group still releases exactly once at size 2 (idempotent Add prevents double-count). A splitter→aggregator round-trip test: `Split` a batch into N children (they carry `HeaderCorrelationID`=parent id + `HeaderSequenceSize`=N + deterministic ids), feed each child through `agg.Handle`, assert one aggregate emitted with all N. (Reuses Phase-1 `Split`.)
- [ ] **Step 2** — `ExampleAggregator` (`example_aggregator_test.go`): correlate 3 line-items of an order into one total; print the released aggregate. Deterministic `// Output:`.
- [ ] **Step 3** — coverage: `aggregator.go`/`groupstore.go` hot-path branches at/near 100%.
- [ ] **Step 4** — full pre-commit gate (all 8 commands from Global Constraints) clean.
- [ ] **Step 5** — Commit `test(core): Aggregator settlement + ExampleAggregator + gate`.

---

## Self-Review

**Spec/ADR coverage:** MessageGroupStore SPI (ADR §2) → Task 1; memory.GroupStore + capacity (ADR §2/§6) → Task 1; Aggregator construct/correlate/release/aggregate/settlement (ADR §3/§4) → Task 2; number-tolerant size (ADR §2 M-1) → Task 2; expiry opt-in + required channel + reaper (ADR §5) → Task 3; at-least-once + idempotent redelivery + Splitter round-trip (ADR §4) → Task 4. ✅

**Open items to confirm during implementation (flag to coordinator, don't guess):** (a) exact `clockwork` ticker API method names for the installed version; (b) the no-timeout `Run` contract (block-until-cancel vs return nil) — pick one, test it, document it; (c) whether the ingress `PayloadOf[A]` assert is best folded into `Handle` before correlate or kept as a stored `assert` closure — either is fine, keep fail-fast `ErrPayloadType`; (d) O9-6 nested-correlation note — add a godoc sentence that nested split/aggregate needs `WithCorrelationStrategy`.

**Placeholder scan:** the `B_` in the first Aggregator struct sketch is illustrative only — the real struct is the boxed non-generic form in the NOTE. The implementer builds the NOTE's version. No other placeholders.

**Type consistency:** `NewAggregator`, `Handle`, `Run`, `WithOutputChannel`, `WithCorrelationStrategy`, `WithReleaseStrategy`, `WithCompletionSize`, `WithGroupTimeout`, `WithExpiredGroupChannel`, `WithClock`, `MessageGroup`, `MessageGroupStore`, `memory.NewGroupStore`, `WithMaxGroups`, `asInt`, `ErrNoCorrelation`, `ErrNilOutput`, `ErrExpiryChannelRequired` used consistently across tasks.
