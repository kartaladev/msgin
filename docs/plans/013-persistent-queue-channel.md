# Persistent Queue Channel Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.
>
> **Go skills are mandatory (CLAUDE.md writing-plans override):** every task starts from **`cc-skills-golang:golang-how-to`** (it routes to the relevant `golang-*` skills), uses **`superpowers:test-driven-development`** (red→green→refactor), navigates/refactors via **`gopls`** (the native `LSP` tool), and obeys the project testing overrides: **`table-test`** (assert-closure tables, `t.Context()`), **`use-mockgen`** (`--typed`, mocks beside the interface), **`use-testcontainers`** (real DBs via `database.RunTestDatabase`, never fakes). Blackbox `_test` packages only.

**Goal:** Add a first-class, pollable **`QueueChannel`** whose buffer is a swappable **`ChannelStore`** — an in-memory store (async, process-scoped) and a SQL store (durable, at-least-once) — so a flow segment can be made durable by swapping the store, with no core runtime change.

**Architecture:** `QueueChannel` (core) implements the *existing* `msgin.OutboundAdapter` (`Send`→`ChannelStore.Enqueue`) + `msgin.PollingSource` (`Poll`→`ChannelStore.Claim`) + `msgin.LiveValueSource` (delegates `EmitsLiveValue`). The existing `Poller`/worker-pool/retry/DLQ/backpressure drain it verbatim. `ChannelStore.Claim` returns `[]msgin.Delivery` (reused, not a new type). Two stores: a new in-memory `memory.QueueStore` (bounded FIFO + `visibleAt` visibility + `clockwork`, no goroutine) and a SQL `sql.QueueStore` facade over the existing `Outbound`+`Source`.

**Tech Stack:** Go 1.25, stdlib + `github.com/jonboulle/clockwork` (core), `database/sql` (driver injected) for the SQL store; `go.uber.org/goleak`, `stretchr/testify`, `testcontainers-go` (tests).

## Global Constraints

- **Go 1.25 only.** `go.mod` carries `go 1.25`; build/test with `GOTOOLCHAIN=go1.25.12`. No language/stdlib features newer than 1.25.
- **Dependency policy.** Core stays stdlib + `clockwork` + `cenkalti/backoff/v4` only. **This increment adds NO new dependency** to any module. The SQL store is `database/sql` (driver injected by the caller).
- **Dependency points inward.** core ← adapters, never the reverse. The `QueueChannel` type + the `ChannelStore` interface live in the core `msgin` package; the concrete stores `memory.QueueStore` (in `adapter/memory`) and `sql.QueueStore` (in `adapter/database/sql`) implement that interface. The core must not import an adapter.
- **No goroutine leaks.** `QueueChannel` starts none; `memory.QueueStore` starts none (visibility is timestamp-based, not timer-based). Prove with `go.uber.org/goleak` (adapter `TestMain` + the end-to-end test). `go test ./... -race` clean.
- **Testing overrides:** blackbox `package <pkg>_test`; **assert-closure tables** (never `want`/`wantErr` fields); `t.Context()` not `context.Background()`; injectable time via `clockwork.NewFakeClock()` (no first-party clock); `Example…` tests double as godoc.
- **Test-coverage gate:** target ≥85% statement coverage on every changed package; **every hot-path/typed-error branch has a covering test** (enumerated per task under "Hot-path branches").
- **Every exported symbol has a godoc comment.** Keep the public surface small; document each default's value + rationale.
- **Traceability:** every commit carries `Spec: 007` / `Plan: 013` / `ADR: 0018` footer trailers.
- **No commit without the green unit:** each task's `go test ./... -race` passes before its (pre-authorized) commit. The whole-branch gate (`/code-review` + `/security-review` over `main..HEAD`, findings resolved, `-race`/`vet`/lint/`govulncheck`/`gorelease` clean) runs before the final increment.

---

## File Structure

- `store.go` (core, package `msgin`) — the `ChannelStore` SPI. New.
- `queuechannel.go` (core, package `msgin`) — `QueueChannel` + `NewQueueChannel`. New.
- `errors.go` (core) — add `ErrNilStore`, `ErrInvalidCapacity`. Modify.
- `queuechannel_test.go`, `store_test.go` (core, package `msgin_test`) — tests. New.
- `adapter/memory/queuestore.go` — `QueueStore` + `NewQueueStore` + `WithCapacity`/`WithOverflow`/`WithClock`. New.
- `adapter/memory/queuestore_test.go` — tests. New. (`adapter/memory` already has a goleak `TestMain`; confirm and reuse.)
- `adapter/database/sql/queuestore.go` — `QueueStore` facade + `NewQueueStore` (+ `Ready`/`EnsureSchema`/`NativeReliability` forwarders). New.
- `adapter/database/sql/queuestore_test.go` — construction/validation unit tests. New.
- `adapter/database/sql/harness/queuestore.go` — `RunQueueStore` conformance runner (harness module). New.
- `adapter/database/sql/dbtest/conformance_{pg,mysql,sqlite}_test.go` — add one `t.Run("QueueStore", …)` line each. Modify.
- `docs/adrs/0018-persistent-queue-channel.md`, `docs/specs/007-persistent-queue-channel.md` — already written; ride in Task-1's commit (couple design with code).

---

### Task 1: Core `ChannelStore` SPI + `QueueChannel`

**Files:**
- Create: `store.go`, `queuechannel.go`
- Modify: `errors.go` (add two sentinels)
- Test: `queuechannel_test.go` (package `msgin_test`)

**Interfaces:**
- Consumes: `msgin.Message[any]`, `msgin.Delivery`, `msgin.OutboundAdapter`, `msgin.PollingSource`, `msgin.LiveValueSource` (all existing, `spi.go`).
- Produces:
  - `type ChannelStore interface { Enqueue(ctx context.Context, msg Message[any]) error; Claim(ctx context.Context, max int) ([]Delivery, error); EmitsLiveValue() bool }`
  - `func NewQueueChannel(store ChannelStore) (*QueueChannel, error)` — returns `ErrNilStore` if store is nil.
  - `*QueueChannel` implements `Send(ctx, Message[any]) error`, `Poll(ctx, int) ([]Delivery, error)`, `EmitsLiveValue() bool`, and `NativeReliability` (`NativeRedelivery`/`NativeDeadLetter`) by forwarding to the store when the store implements `msgin.NativeReliability`, else `false`/`false` (audit M-2: a durable SQL-backed channel must not silently drop the store's native-reliability capability).
  - Sentinels: `ErrNilStore`, `ErrInvalidCapacity`.

- [ ] **Step 1: Write the failing test** — `queuechannel_test.go`. A tiny in-test fake `ChannelStore` (a mutex + FIFO slice + inflight map) drives the channel through its public surface. Assert-closure table.

```go
package msgin_test

import (
	"context"
	"sync"
	"testing"

	"github.com/kartaladev/msgin"
	"github.com/stretchr/testify/require"
)

// fakeStore is a minimal in-test ChannelStore: FIFO ready slice + inflight map.
type fakeStore struct {
	mu       sync.Mutex
	ready    []msgin.Message[any]
	inflight map[string]msgin.Message[any]
	live     bool
	claimErr error
}

func newFakeStore(live bool) *fakeStore {
	return &fakeStore{inflight: map[string]msgin.Message[any]{}, live: live}
}

func (f *fakeStore) Enqueue(_ context.Context, m msgin.Message[any]) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ready = append(f.ready, m)
	return nil
}

func (f *fakeStore) Claim(_ context.Context, max int) ([]msgin.Delivery, error) {
	if f.claimErr != nil {
		return nil, f.claimErr
	}
	if max <= 0 {
		return nil, nil
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	n := min(max, len(f.ready))
	out := make([]msgin.Delivery, 0, n)
	for _, m := range f.ready[:n] {
		id := m.ID()
		f.inflight[id] = m
		out = append(out, msgin.Delivery{
			Msg:  m,
			Ack:  func(context.Context) error { f.mu.Lock(); delete(f.inflight, id); f.mu.Unlock(); return nil },
			Nack: func(context.Context, bool, time.Duration) error { return nil },
		})
	}
	f.ready = f.ready[n:]
	return out, nil
}

func (f *fakeStore) EmitsLiveValue() bool { return f.live }

// nativeFake is a fakeStore that ALSO advertises native reliability, so the
// channel's forwarding (audit M-2) can be exercised in both directions.
type nativeFake struct{ *fakeStore }

func (nativeFake) NativeRedelivery() bool { return true }
func (nativeFake) NativeDeadLetter() bool { return false }

func TestQueueChannel(t *testing.T) {
	tests := []struct {
		name   string
		assert func(t *testing.T)
	}{
		{
			name: "nil store is ErrNilStore",
			assert: func(t *testing.T) {
				_, err := msgin.NewQueueChannel(nil)
				require.ErrorIs(t, err, msgin.ErrNilStore)
			},
		},
		{
			name: "Send enqueues; Poll returns it as a Delivery, capped at max",
			assert: func(t *testing.T) {
				qc, err := msgin.NewQueueChannel(newFakeStore(true))
				require.NoError(t, err)
				require.NoError(t, qc.Send(t.Context(), msgin.New[any]("a")))
				require.NoError(t, qc.Send(t.Context(), msgin.New[any]("b")))
				got, err := qc.Poll(t.Context(), 1)
				require.NoError(t, err)
				require.Len(t, got, 1)
				require.Equal(t, "a", got[0].Msg.Payload())
			},
		},
		{
			name: "Poll surfaces the store's error with no deliveries (invariant 2)",
			assert: func(t *testing.T) {
				fs := newFakeStore(true)
				fs.claimErr = errors.New("boom")
				qc, err := msgin.NewQueueChannel(fs)
				require.NoError(t, err)
				got, err := qc.Poll(t.Context(), 10)
				require.Error(t, err)
				require.Empty(t, got)
			},
		},
		{
			name: "EmitsLiveValue delegates to the store",
			assert: func(t *testing.T) {
				live, _ := msgin.NewQueueChannel(newFakeStore(true))
				wire, _ := msgin.NewQueueChannel(newFakeStore(false))
				require.True(t, live.EmitsLiveValue())
				require.False(t, wire.EmitsLiveValue())
			},
		},
		{
			name: "NativeReliability forwards when the store implements it, else false",
			assert: func(t *testing.T) {
				plain, _ := msgin.NewQueueChannel(newFakeStore(true)) // fakeStore is NOT NativeReliability
				require.False(t, plain.NativeRedelivery())
				require.False(t, plain.NativeDeadLetter())
				native, _ := msgin.NewQueueChannel(nativeFake{newFakeStore(false)})
				require.True(t, native.NativeRedelivery())
				require.False(t, native.NativeDeadLetter())
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) { tt.assert(t) })
	}
}
```

(Add the `errors`, `time` imports. `min` is a Go 1.21+ builtin.)

- [ ] **Step 2: Run the test to verify it fails**

Run: `GOTOOLCHAIN=go1.25.12 go test . -run TestQueueChannel`
Expected: FAIL — `undefined: msgin.NewQueueChannel`, `undefined: msgin.ChannelStore`, `undefined: msgin.ErrNilStore`.

- [ ] **Step 3: Add the two sentinels** to `errors.go` (inside the existing `var (…)` block, each with a godoc comment), **and correct the stale `ErrOverflowDropped` godoc** (audit H-1): it currently says "it is not returned from any exported function," which Task 2's `memory.QueueStore.Enqueue` breaks by returning it under `OverflowReject`/`OverflowDropNewest`-full.

```go
	// ErrNilStore is returned by NewQueueChannel when the ChannelStore is nil.
	ErrNilStore = errors.New("msgin: channel store is nil")

	// ErrInvalidCapacity is returned by a bounded store constructor (e.g.
	// memory.NewQueueStore) when an explicit capacity is <= 0.
	ErrInvalidCapacity = errors.New("msgin: capacity must be > 0")
```

Change the `ErrOverflowDropped` godoc to:

```go
	// ErrOverflowDropped is the cause carried to OnRetry/logs when a streaming
	// source's overflow policy sheds a message, AND is returned by a bounded
	// ChannelStore.Enqueue (e.g. memory.QueueStore) when OverflowReject rejects a
	// full-buffer Send. It is NOT returned for the silent Drop* policies.
	ErrOverflowDropped = errors.New("msgin: message dropped by overflow policy")
```

- [ ] **Step 4: Write `store.go`** — the SPI (interface + godoc only, no logic).

```go
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
```

- [ ] **Step 5: Write `queuechannel.go`** — the thin, store-agnostic channel.

```go
package msgin

import "context"

// QueueChannel is a pollable, point-to-point queue channel (EIP Point-to-Point
// Channel; Spring Integration QueueChannel). It is backed by a swappable
// ChannelStore: in-memory for async decoupling, or a persistent store for
// Guaranteed Delivery across restarts.
//
// It is NOT a Subscribe-based MessageChannel (that is DirectChannel's synchronous
// push model). A QueueChannel is pollable: it implements OutboundAdapter (Send
// enqueues) and PollingSource (Poll claims), so you produce into it with
// NewProducer and drain it with NewConsumer — the existing Poller, worker pool,
// retry, dead-letter, and backpressure carry it unchanged. The channel starts no
// goroutine of its own.
type QueueChannel struct {
	store ChannelStore
}

var (
	_ OutboundAdapter   = (*QueueChannel)(nil)
	_ PollingSource     = (*QueueChannel)(nil)
	_ LiveValueSource   = (*QueueChannel)(nil)
	_ NativeReliability = (*QueueChannel)(nil)
)

// NewQueueChannel wraps store as a pollable queue channel. A nil store is
// ErrNilStore. Capacity and overflow policy are properties of the store (only a
// bounded in-memory store has them); configure them on the store's constructor.
func NewQueueChannel(store ChannelStore) (*QueueChannel, error) {
	if store == nil {
		return nil, ErrNilStore
	}
	return &QueueChannel{store: store}, nil
}

// Send enqueues msg (OutboundAdapter). Backpressure/overflow is the store's
// concern: a bounded store's Enqueue blocks, drops, or rejects per its policy.
func (q *QueueChannel) Send(ctx context.Context, msg Message[any]) error {
	return q.store.Enqueue(ctx, msg)
}

// Poll claims up to max ready deliveries (PollingSource). The store owns the
// three Poll invariants (<=max; no deliveries with a non-nil error; own cleanup
// on the error/cancel path); QueueChannel forwards verbatim.
func (q *QueueChannel) Poll(ctx context.Context, max int) ([]Delivery, error) {
	return q.store.Claim(ctx, max)
}

// EmitsLiveValue delegates to the store (LiveValueSource), so NewConsumer pairs a
// codec iff the store carries []byte.
func (q *QueueChannel) EmitsLiveValue() bool { return q.store.EmitsLiveValue() }

// NativeRedelivery / NativeDeadLetter forward the store's NativeReliability when
// it implements it (e.g. the SQL store, whose backing Source redelivers via the
// DB row), else report false. Forwarding keeps a durable-store-backed
// QueueChannel behaviorally identical to its underlying Source and prevents a
// future store whose NativeDeadLetter is true from being silently double
// dead-lettered by the runtime (audit M-2).
func (q *QueueChannel) NativeRedelivery() bool {
	if nr, ok := q.store.(NativeReliability); ok {
		return nr.NativeRedelivery()
	}
	return false
}

func (q *QueueChannel) NativeDeadLetter() bool {
	if nr, ok := q.store.(NativeReliability); ok {
		return nr.NativeDeadLetter()
	}
	return false
}
```

- [ ] **Step 6: Run the test to verify it passes**

Run: `GOTOOLCHAIN=go1.25.12 go test . -run TestQueueChannel -race`
Expected: PASS (all subtests).

- [ ] **Step 7: Coverage check.** No `Example` here — the runnable `ExampleQueueChannel` lands in Task 3 (it needs the real `memory.QueueStore` + `Producer`/`Consumer`, none of which exist yet). Task 1 only verifies branch coverage.

Run: `GOTOOLCHAIN=go1.25.12 go test . -run TestQueueChannel -cover`
Expected: the new lines covered; nil-store and happy-path both exercised.

**Hot-path branches (must each have a covering test):** `NewQueueChannel` nil→`ErrNilStore`, non-nil→ok; `Poll` error passthrough (invariant 2); `EmitsLiveValue` true/false delegation; `NativeRedelivery`/`NativeDeadLetter` store-implements vs not (both branches). (All present in Step 1.)

- [ ] **Step 8: Commit** (couples the design docs with the code that realizes them).

```bash
git add store.go queuechannel.go errors.go queuechannel_test.go \
        docs/specs/007-persistent-queue-channel.md docs/adrs/0018-persistent-queue-channel.md \
        docs/plans/013-persistent-queue-channel.md
git commit -m "$(cat <<'EOF'
feat(core): pollable QueueChannel + ChannelStore SPI

Add a store-agnostic QueueChannel (OutboundAdapter + PollingSource +
LiveValueSource) over a narrow ChannelStore seam. No core runtime change.

Spec: 007
Plan: 013
ADR: 0018
EOF
)"
```

---

### Task 2: In-memory `ChannelStore` (`memory.QueueStore`)

**Files:**
- Create: `adapter/memory/queuestore.go`
- Test: `adapter/memory/queuestore_test.go` (package `memory_test`)

**Interfaces:**
- Consumes: `msgin.ChannelStore`, `msgin.Message[any]`, `msgin.Delivery`, `msgin.OverflowPolicy` + `OverflowBlock`/`OverflowDropNewest`/`OverflowDropOldest`/`OverflowReject` (existing, `flowcontrol.go`), `msgin.ErrOverflowDropped`, `msgin.ErrInvalidCapacity`, `clockwork.Clock`.
- Produces:
  - `func NewQueueStore(opts ...Option) (*QueueStore, error)`
  - `func WithCapacity(n int) Option` — bounds ready+inflight; **default 1024**; explicit `<=0` → `msgin.ErrInvalidCapacity`.
  - `func WithOverflow(p msgin.OverflowPolicy) Option` — full-buffer behavior at `Enqueue`; **default `OverflowBlock`**; unknown value → `OverflowBlock` (matches the codebase convention, `flowcontrol.go`).
  - `func WithClock(c clockwork.Clock) Option` — nil → real clock.
  - `*QueueStore` implements `msgin.ChannelStore` (`Enqueue`/`Claim`/`EmitsLiveValue`→true).

**Design (goleak-clean, timer-free):** a `capacity`-buffered `chan struct{}` semaphore accounts occupancy (ready+inflight) and gives **`ctx`-aware `Block`** backpressure; a mutex guards a FIFO slice of `entry{msg, visibleAt}` + an `inflight` map keyed by delivery id + epoch. `Nack(requeue,delay)` re-appends with `visibleAt = clock.Now().Add(delay)`; `Claim` skips entries with `visibleAt > clock.Now()` (visibility is timestamp-based — **no timer, no goroutine**). A sem slot is released on `Ack` and on `Nack(requeue=false)` (a genuine drop) and on the shed overflow policies.

- [ ] **Step 1: Write the failing test** — cover enqueue/claim FIFO, ack removal, nack-requeue-with-delay (fake clock), each overflow policy, capacity validation, and `EmitsLiveValue`. Assert-closure table, `clockwork.NewFakeClock()`.

```go
package memory_test

import (
	"testing"
	"time"

	"github.com/jonboulle/clockwork"
	"github.com/kartaladev/msgin"
	"github.com/kartaladev/msgin/adapter/memory"
	"github.com/stretchr/testify/require"
)

func TestQueueStore(t *testing.T) {
	tests := []struct {
		name   string
		assert func(t *testing.T)
	}{
		{
			name: "explicit non-positive capacity is ErrInvalidCapacity",
			assert: func(t *testing.T) {
				_, err := memory.NewQueueStore(memory.WithCapacity(0))
				require.ErrorIs(t, err, msgin.ErrInvalidCapacity)
			},
		},
		{
			name: "enqueue then claim returns FIFO, capped at max",
			assert: func(t *testing.T) {
				s, err := memory.NewQueueStore()
				require.NoError(t, err)
				require.NoError(t, s.Enqueue(t.Context(), msgin.New[any]("a")))
				require.NoError(t, s.Enqueue(t.Context(), msgin.New[any]("b")))
				got, err := s.Claim(t.Context(), 5)
				require.NoError(t, err)
				require.Len(t, got, 2)
				require.Equal(t, "a", got[0].Msg.Payload())
				require.Equal(t, "b", got[1].Msg.Payload())
			},
		},
		{
			name: "ack removes; a claimed-then-acked message is not re-claimed",
			assert: func(t *testing.T) {
				s, _ := memory.NewQueueStore()
				_ = s.Enqueue(t.Context(), msgin.New[any]("a"))
				got, _ := s.Claim(t.Context(), 1)
				require.NoError(t, got[0].Ack(t.Context()))
				again, _ := s.Claim(t.Context(), 1)
				require.Empty(t, again)
			},
		},
		{
			name: "nack requeue with delay: invisible until the clock advances",
			assert: func(t *testing.T) {
				clk := clockwork.NewFakeClock()
				s, _ := memory.NewQueueStore(memory.WithClock(clk))
				_ = s.Enqueue(t.Context(), msgin.New[any]("a"))
				got, _ := s.Claim(t.Context(), 1)
				require.NoError(t, got[0].Nack(t.Context(), true, time.Minute))
				// still within the delay window → not claimable
				pending, _ := s.Claim(t.Context(), 1)
				require.Empty(t, pending)
				clk.Advance(time.Minute)
				ready, _ := s.Claim(t.Context(), 1)
				require.Len(t, ready, 1)
			},
		},
		{
			name: "overflow Reject: Enqueue past capacity returns ErrOverflowDropped",
			assert: func(t *testing.T) {
				s, _ := memory.NewQueueStore(memory.WithCapacity(1), memory.WithOverflow(msgin.OverflowReject))
				require.NoError(t, s.Enqueue(t.Context(), msgin.New[any]("a")))
				require.ErrorIs(t, s.Enqueue(t.Context(), msgin.New[any]("b")), msgin.ErrOverflowDropped)
			},
		},
		{
			name: "overflow DropOldest: newest survives, oldest evicted",
			assert: func(t *testing.T) {
				s, _ := memory.NewQueueStore(memory.WithCapacity(1), memory.WithOverflow(msgin.OverflowDropOldest))
				_ = s.Enqueue(t.Context(), msgin.New[any]("old"))
				require.NoError(t, s.Enqueue(t.Context(), msgin.New[any]("new")))
				got, _ := s.Claim(t.Context(), 5)
				require.Len(t, got, 1)
				require.Equal(t, "new", got[0].Msg.Payload())
			},
		},
		{
			name: "overflow DropNewest: oldest survives, newest dropped (no error)",
			assert: func(t *testing.T) {
				s, _ := memory.NewQueueStore(memory.WithCapacity(1), memory.WithOverflow(msgin.OverflowDropNewest))
				_ = s.Enqueue(t.Context(), msgin.New[any]("old"))
				require.NoError(t, s.Enqueue(t.Context(), msgin.New[any]("new")))
				got, _ := s.Claim(t.Context(), 5)
				require.Len(t, got, 1)
				require.Equal(t, "old", got[0].Msg.Payload())
			},
		},
		{
			name: "overflow DropOldest with nothing evictable (all in-flight) drops the newcomer",
			assert: func(t *testing.T) {
				s, _ := memory.NewQueueStore(memory.WithCapacity(1), memory.WithOverflow(msgin.OverflowDropOldest))
				_ = s.Enqueue(t.Context(), msgin.New[any]("a"))
				got, _ := s.Claim(t.Context(), 1) // "a" now in-flight; ready is empty
				require.Len(t, got, 1)
				// buffer full (the in-flight "a" holds the only slot), ready empty →
				// nothing to evict → the newcomer is dropped
				require.ErrorIs(t, s.Enqueue(t.Context(), msgin.New[any]("b")), msgin.ErrOverflowDropped)
			},
		},
		{
			name: "overflow Block: Enqueue unblocks when a slot frees via Ack; honors ctx cancel",
			assert: func(t *testing.T) {
				s, _ := memory.NewQueueStore(memory.WithCapacity(1)) // default Block
				_ = s.Enqueue(t.Context(), msgin.New[any]("a"))
				// full: a cancelled ctx must make Enqueue return ctx.Err(), not hang
				ctx, cancel := context.WithCancel(t.Context())
				cancel()
				require.ErrorIs(t, s.Enqueue(ctx, msgin.New[any]("b")), context.Canceled)
				// free the slot, then Enqueue succeeds
				got, _ := s.Claim(t.Context(), 1)
				require.NoError(t, got[0].Ack(t.Context()))
				require.NoError(t, s.Enqueue(t.Context(), msgin.New[any]("b")))
			},
		},
		{
			name: "nack requeue=false drops the message and frees a slot",
			assert: func(t *testing.T) {
				s, _ := memory.NewQueueStore(memory.WithCapacity(1))
				_ = s.Enqueue(t.Context(), msgin.New[any]("a"))
				got, _ := s.Claim(t.Context(), 1)
				require.NoError(t, got[0].Nack(t.Context(), false, 0)) // genuine drop
				// dropped → not re-claimable, and the freed slot admits a new Enqueue
				empty, _ := s.Claim(t.Context(), 1)
				require.Empty(t, empty)
				require.NoError(t, s.Enqueue(t.Context(), msgin.New[any]("b")))
			},
		},
		{
			name: "fence: a second settle after the first is a no-op (no panic, no double-free)",
			assert: func(t *testing.T) {
				s, _ := memory.NewQueueStore(memory.WithCapacity(1))
				_ = s.Enqueue(t.Context(), msgin.New[any]("a"))
				got, _ := s.Claim(t.Context(), 1)
				require.NoError(t, got[0].Ack(t.Context()))
				// stale settles on the same delivery must be inert (epoch fence)
				require.NoError(t, got[0].Ack(t.Context()))
				require.NoError(t, got[0].Nack(t.Context(), true, time.Minute))
				// the slot was freed exactly once: a fresh Enqueue still succeeds
				require.NoError(t, s.Enqueue(t.Context(), msgin.New[any]("b")))
			},
		},
		{
			name: "claim caps at max and leaves the remainder claimable",
			assert: func(t *testing.T) {
				s, _ := memory.NewQueueStore()
				for _, p := range []string{"a", "b", "c"} {
					_ = s.Enqueue(t.Context(), msgin.New[any](p))
				}
				first, _ := s.Claim(t.Context(), 2)
				require.Len(t, first, 2)
				require.Equal(t, "a", first[0].Msg.Payload())
				require.Equal(t, "b", first[1].Msg.Payload())
				rest, _ := s.Claim(t.Context(), 2)
				require.Len(t, rest, 1)
				require.Equal(t, "c", rest[0].Msg.Payload())
			},
		},
		{
			name: "WithClock(nil) falls back to a real clock (no panic)",
			assert: func(t *testing.T) {
				s, err := memory.NewQueueStore(memory.WithClock(nil))
				require.NoError(t, err)
				require.NoError(t, s.Enqueue(t.Context(), msgin.New[any]("a")))
				got, _ := s.Claim(t.Context(), 1)
				require.Len(t, got, 1)
			},
		},
		{
			name: "EmitsLiveValue is true",
			assert: func(t *testing.T) {
				s, _ := memory.NewQueueStore()
				require.True(t, s.EmitsLiveValue())
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) { tt.assert(t) })
	}
}
```

(Add `context` import.)

- [ ] **Step 2: Run the test to verify it fails**

Run: `GOTOOLCHAIN=go1.25.12 go test ./adapter/memory/ -run TestQueueStore`
Expected: FAIL — `undefined: memory.NewQueueStore`, etc.

- [ ] **Step 3: Implement `queuestore.go`.** Semaphore for capacity/backpressure; mutex+slice for order+visibility; epoch-fenced settlement so a Nack’d-then-reclaimed entry can’t be double-settled.

```go
package memory

import (
	"context"
	"sync"
	"time"

	"github.com/jonboulle/clockwork"
	"github.com/kartaladev/msgin"
)

// QueueStore is an in-memory msgin.ChannelStore: a bounded FIFO with lease/claim
// settlement, carrying live Go values (no codec). It is the fast, DB-free default
// buffer behind a msgin.QueueChannel.
//
// Delivery guarantee: at-least-once within the process lifetime — a Nacked or
// never-settled claim is re-claimable. Buffered and in-flight messages are LOST
// on process exit (at-most-once across a restart). For durability across a
// restart, back the QueueChannel with the SQL ChannelStore.
//
// It starts no goroutine: delayed redelivery is timestamp-based (visibleAt), and
// Block backpressure uses a ctx-aware semaphore.
type QueueStore struct {
	sem      chan struct{} // buffered to capacity; one slot per occupied (ready or inflight) message
	overflow msgin.OverflowPolicy
	clock    clockwork.Clock

	mu       sync.Mutex
	ready    []entry          // FIFO; may include not-yet-visible entries
	inflight map[int64]entry  // claimed, keyed by epoch (fence token)
	epoch    int64
}

type entry struct {
	msg       msgin.Message[any]
	visibleAt time.Time
	epoch     int64
}

const defaultCapacity = 1024

var _ msgin.ChannelStore = (*QueueStore)(nil)

// Option configures a QueueStore.
type Option func(*config)

type config struct {
	capacity    int
	capacitySet bool
	overflow    msgin.OverflowPolicy
	clock       clockwork.Clock
}

// WithCapacity bounds the number of occupied messages (ready + in-flight);
// default 1024. A bounded buffer is the safe default — an unbounded in-memory
// queue is an OOM lever (CLAUDE.md fail-safe defaults). An explicit n <= 0 is
// msgin.ErrInvalidCapacity.
func WithCapacity(n int) Option { return func(c *config) { c.capacity = n; c.capacitySet = true } }

// WithOverflow sets the behavior when Enqueue meets a full buffer: OverflowBlock
// (default — backpressure until a slot frees or ctx cancels), OverflowReject
// (returns msgin.ErrOverflowDropped), OverflowDropNewest (silently drops the
// arriving message, returns nil), or OverflowDropOldest (evicts the head to admit
// the newcomer). An unknown value falls back to OverflowBlock (the safe default),
// matching the runtime's convention.
//
// Overflow is evaluated non-atomically against concurrent settlement: under a
// shed policy, an Enqueue racing a concurrent Ack may shed even though that Ack
// frees a slot microseconds later (audit M-3). This is inherent to lock-free
// capacity checks and is acceptable for a shedding policy; use OverflowBlock when
// no message may be dropped.
func WithOverflow(p msgin.OverflowPolicy) Option { return func(c *config) { c.overflow = p } }

// WithClock injects the clock used for delayed-requeue visibility; nil selects a
// real clock. Tests pass clockwork.NewFakeClock().
func WithClock(c clockwork.Clock) Option { return func(cfg *config) { cfg.clock = c } }

// NewQueueStore builds an in-memory ChannelStore with the given options.
func NewQueueStore(opts ...Option) (*QueueStore, error) {
	cfg := config{clock: clockwork.NewRealClock()}
	for _, o := range opts {
		o(&cfg)
	}
	capacity := defaultCapacity
	if cfg.capacitySet {
		if cfg.capacity <= 0 {
			return nil, msgin.ErrInvalidCapacity
		}
		capacity = cfg.capacity
	}
	if cfg.clock == nil {
		cfg.clock = clockwork.NewRealClock()
	}
	return &QueueStore{
		sem:      make(chan struct{}, capacity),
		overflow: cfg.overflow,
		clock:    cfg.clock,
		inflight: map[int64]entry{},
	}, nil
}

// Enqueue appends msg, applying the overflow policy when the buffer is full.
func (s *QueueStore) Enqueue(ctx context.Context, msg msgin.Message[any]) error {
	switch s.overflow {
	case msgin.OverflowDropNewest, msgin.OverflowDropOldest, msgin.OverflowReject:
		select {
		case s.sem <- struct{}{}: // acquired a slot
		default:
			return s.shed(msg) // buffer full — apply the shed policy
		}
	default: // OverflowBlock and any unknown value → backpressure
		select {
		case s.sem <- struct{}{}:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	s.mu.Lock()
	s.ready = append(s.ready, entry{msg: msg, visibleAt: s.clock.Now()})
	s.mu.Unlock()
	return nil
}

// shed handles a full-buffer Enqueue for the non-Block policies (the caller
// reached the `default` select arm — no slot was acquired). DropNewest silently
// drops the newcomer (returns nil); Reject reports ErrOverflowDropped; DropOldest
// evicts the FIFO head (its slot transfers to the newcomer, so occupancy is
// unchanged and the semaphore is untouched) and falls back to a drop when nothing
// is evictable (every entry in-flight). Note: DropOldest evicts s.ready[0], which
// after requeues may be a not-yet-visible (mid-retry) entry rather than the oldest
// visible one — benign, it still frees capacity (audit L-2).
func (s *QueueStore) shed(msg msgin.Message[any]) error {
	if s.overflow == msgin.OverflowDropOldest {
		s.mu.Lock()
		if len(s.ready) > 0 {
			s.ready = s.ready[1:] // evict head; its slot is reused by the newcomer
			s.ready = append(s.ready, entry{msg: msg, visibleAt: s.clock.Now()})
			s.mu.Unlock()
			return nil
		}
		s.mu.Unlock()
		return msgin.ErrOverflowDropped // nothing evictable (all in-flight) → drop
	}
	if s.overflow == msgin.OverflowDropNewest {
		return nil // silently drop the arriving message (distinguishes it from Reject)
	}
	return msgin.ErrOverflowDropped // OverflowReject
}

// Claim leases up to max visible ready entries in FIFO order. A non-positive max
// yields no deliveries (guards make(...) against a negative cap panic — Claim is
// exported and directly callable; audit M-1).
func (s *QueueStore) Claim(_ context.Context, max int) ([]msgin.Delivery, error) {
	if max <= 0 {
		return nil, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.clock.Now()
	out := make([]msgin.Delivery, 0, max)
	kept := s.ready[:0]
	for _, e := range s.ready {
		if len(out) < max && !e.visibleAt.After(now) {
			s.epoch++
			e.epoch = s.epoch
			s.inflight[e.epoch] = e
			out = append(out, msgin.Delivery{
				Msg:  e.msg,
				Ack:  s.ackClosure(e.epoch),
				Nack: s.nackClosure(e.epoch),
			})
			continue
		}
		kept = append(kept, e)
	}
	s.ready = kept
	return out, nil
}

func (s *QueueStore) ackClosure(epoch int64) func(context.Context) error {
	return func(context.Context) error {
		s.mu.Lock()
		_, ok := s.inflight[epoch]
		delete(s.inflight, epoch)
		s.mu.Unlock()
		if ok {
			<-s.sem // release the slot
		}
		return nil
	}
}

func (s *QueueStore) nackClosure(epoch int64) func(context.Context, bool, time.Duration) error {
	return func(_ context.Context, requeue bool, delay time.Duration) error {
		s.mu.Lock()
		e, ok := s.inflight[epoch]
		delete(s.inflight, epoch)
		if !ok {
			s.mu.Unlock()
			return nil // already settled (fence)
		}
		if !requeue {
			s.mu.Unlock()
			<-s.sem // genuine drop → free the slot
			return nil
		}
		e.visibleAt = s.clock.Now().Add(delay)
		e.epoch = 0
		s.ready = append(s.ready, e) // keeps its slot
		s.mu.Unlock()
		return nil
	}
}

// EmitsLiveValue reports true: the store carries live Go values (no codec).
func (s *QueueStore) EmitsLiveValue() bool { return true }
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `GOTOOLCHAIN=go1.25.12 go test ./adapter/memory/ -run TestQueueStore -race`
Expected: PASS (all subtests).

- [ ] **Step 5: goleak.** Confirm `adapter/memory` has a `TestMain` calling `goleak.VerifyTestMain(m)`; if not, add it. Re-run the package.

Run: `GOTOOLCHAIN=go1.25.12 go test ./adapter/memory/ -race`
Expected: PASS, no leaked goroutines.

- [ ] **Step 6: Coverage.**

Run: `GOTOOLCHAIN=go1.25.12 go test ./adapter/memory/ -run TestQueueStore -cover`
Expected: ≥85%; every overflow branch, the capacity-validation branch, ack/nack (requeue true/false, fence miss), and the visibility-skip branch covered.

**Hot-path branches:** capacity `<=0`→error / default 1024; each overflow policy (Block acquire, Block ctx-cancel, DropNewest, DropOldest with/without an evictable head, Reject); Claim visibility skip + max cap; Ack present/absent (fence); Nack requeue-true (with delay) / requeue-false / fence-miss.

- [ ] **Step 7: Commit**

```bash
git add adapter/memory/queuestore.go adapter/memory/queuestore_test.go
git commit -m "$(cat <<'EOF'
feat(memory): in-memory ChannelStore (bounded FIFO, claim/ack, overflow)

Timer-free, goleak-clean QueueStore: visibleAt-based delayed requeue and a
ctx-aware semaphore for Block backpressure. At-least-once within the process.

Spec: 007
Plan: 013
ADR: 0018
EOF
)"
```

---

### Task 3: End-to-end — produce → QueueChannel(memory) → consume

**Files:**
- Test: `queuechannel_e2e_test.go` (package `msgin_test`) — or extend `queuechannel_test.go` with a runnable `Example`.

**Interfaces:**
- Consumes: `msgin.NewProducer[T]`, `msgin.NewConsumer[T]`, `Consumer.Run`, `memory.NewQueueStore`, `msgin.NewQueueChannel`. (All from Tasks 1–2 + existing runtime.)

- [ ] **Step 1: Write the failing test** — a producer sends N typed messages into a memory-backed `QueueChannel`; a consumer drains them with a handler recording payloads; assert all arrive; goleak-clean shutdown via `ctx` cancel. The store uses its default real clock (the messages are immediately visible — no delayed requeue here, so no fake clock is needed); the poller is paced with a short `WithPollInterval`.

Signatures are exact (verified): `Consumer[T].Run(ctx context.Context) error` (consumer.go:19), `Producer[T].Send(ctx, Message[T]) error` (producer.go:13), `Handler[T any] = func(ctx, Message[T]) error` (consumer.go:15) — a bare func literal converts to it; `WithPollInterval[T](d)` (flowcontrol.go:217). **Do NOT use `HandlerFunc` — it is non-generic (`func(ctx, Message[any]) error`, handler.go:16) and is the wrong type.**

```go
func TestQueueChannel_EndToEnd(t *testing.T) {
	store, err := memory.NewQueueStore()
	require.NoError(t, err)
	qc, err := msgin.NewQueueChannel(store)
	require.NoError(t, err)

	prod, err := msgin.NewProducer[string](qc)
	require.NoError(t, err)
	for _, s := range []string{"x", "y", "z"} {
		require.NoError(t, prod.Send(t.Context(), msgin.New(s))) // Send takes Message[string]
	}

	var mu sync.Mutex
	var got []string
	h := func(_ context.Context, m msgin.Message[string]) error { // converts to Handler[string]
		mu.Lock()
		got = append(got, m.Payload())
		mu.Unlock()
		return nil
	}
	cons, err := msgin.NewConsumer[string](qc, h, msgin.WithPollInterval[string](2*time.Millisecond))
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- cons.Run(ctx) }()
	require.Eventually(t, func() bool { mu.Lock(); defer mu.Unlock(); return len(got) == 3 }, time.Second, 5*time.Millisecond)
	cancel()
	require.ErrorIs(t, <-done, context.Canceled)
	require.ElementsMatch(t, []string{"x", "y", "z"}, got)
}
```

(Imports: `context`, `sync`, `time`, plus `msgin`, `adapter/memory`, `testify/require`.)

- [ ] **Step 2: Run to verify it fails / then passes** (no new production code expected — this is an integration proof; if it fails it reveals a wiring gap in Tasks 1–2 to fix).

Run: `GOTOOLCHAIN=go1.25.12 go test . -run TestQueueChannel_EndToEnd -race`
Expected: PASS.

- [ ] **Step 3: Add a runnable `Example`** — `ExampleQueueChannel` (package `msgin_test`) driving `Send` then `Poll`+`Ack` directly for deterministic `// Output:` (a full `Consumer.Run` example is racy). Doubles as godoc for the pattern.

```go
func ExampleQueueChannel() {
	store, _ := memory.NewQueueStore()
	qc, _ := msgin.NewQueueChannel(store)

	_ = qc.Send(context.Background(), msgin.New[any]("hello"))
	_ = qc.Send(context.Background(), msgin.New[any]("world"))

	deliveries, _ := qc.Poll(context.Background(), 10)
	for _, d := range deliveries {
		fmt.Println(d.Msg.Payload())
		_ = d.Ack(context.Background())
	}
	// Output:
	// hello
	// world
}
```

- [ ] **Step 4: goleak + full core suite.**

Run: `GOTOOLCHAIN=go1.25.12 go test . ./adapter/memory/ -race`
Expected: PASS, leak-free.

- [ ] **Step 5: Commit**

```bash
git add queuechannel_e2e_test.go queuechannel_test.go
git commit -m "$(cat <<'EOF'
test(core): end-to-end producer→QueueChannel(memory)→consumer + Example

Spec: 007
Plan: 013
ADR: 0018
EOF
)"
```

---

### Task 4: SQL `QueueStore` (facade over `Outbound`+`Source`) + conformance

> Named `sql.QueueStore` (not `sql.ChannelStore`) to mirror `memory.QueueStore` and avoid shadowing the interface name `msgin.ChannelStore` (audit H-2).

**Files:**
- Create: `adapter/database/sql/queuestore.go`
- Create: `adapter/database/sql/harness/queuestore.go` (conformance runner, in the `harness` module)
- Test: `adapter/database/sql/queuestore_test.go` (unit: construction/validation) + wire the conformance runner into `adapter/database/sql/dbtest/conformance_{pg,mysql,sqlite}_test.go` (testcontainers, PG/MySQL/SQLite).

**Interfaces:**
- Consumes: `sql.NewOutboundAdapter`, `sql.NewPollingSource`, `sql.Outbound`, `sql.Source`, `sql.LeaseDialect`, `sql.Option`; `msgin.ChannelStore`, `msgin.Delivery`, `msgin.NativeReliability`. Test helpers `openDB(t, fakeDriverName)` + `newFakeDialect()` (existing, `testhelpers_test.go`/`fakedialect_test.go`); conformance `harness.TestKit` + the `harness.RunSource`/`RunOutbound` pattern (harness/source.go:33, invoked from `dbtest/conformance_*_test.go`).
- Produces:
  - `func NewQueueStore(db *stdsql.DB, table string, dialect LeaseDialect, opts ...Option) (*QueueStore, error)`
  - `*QueueStore` implements `msgin.ChannelStore` (`Enqueue`→`Outbound.Send`; `Claim`→`Source.Poll`; `EmitsLiveValue`→`false`) and `msgin.NativeReliability` (forwards the embedded `Source`), plus `Ready(ctx) error` / `EnsureSchema(ctx) error` forwarders.
  - `func RunQueueStore(t *testing.T, kit harness.TestKit, db *sql.DB)` (in the `harness` module) — the multi-dialect conformance runner.

- [ ] **Step 1: Write the construction/validation unit test** (no real DB — `sql.Open` is lazy and never dials at construction). Use the existing helpers `openDB(t, fakeDriverName)` (opens an `*sql.DB` on the in-process fake driver; `testhelpers_test.go:56`) and `newFakeDialect()` (a valid non-built-in `LeaseDialect`; `fakedialect_test.go`). Mirror `TestNewInboxDeduper_Construction` (`inbox_dedup_test.go:47`). Assert-closure table.

```go
func TestNewQueueStore_Construction(t *testing.T) {
	tests := []struct {
		name   string
		assert func(t *testing.T)
	}{
		{
			name: "nil db is ErrNilAdapter",
			assert: func(t *testing.T) {
				_, err := msginsql.NewQueueStore(nil, "q", newFakeDialect())
				require.ErrorIs(t, err, msgin.ErrNilAdapter)
			},
		},
		{
			name: "nil dialect is ErrNilDialect",
			assert: func(t *testing.T) {
				_, err := msginsql.NewQueueStore(openDB(t, fakeDriverName), "q", nil)
				require.ErrorIs(t, err, msginsql.ErrNilDialect)
			},
		},
		{
			name: "empty table is ErrInvalidTableName",
			assert: func(t *testing.T) {
				_, err := msginsql.NewQueueStore(openDB(t, fakeDriverName), "", newFakeDialect())
				require.ErrorIs(t, err, msginsql.ErrInvalidTableName)
			},
		},
		{
			name: "valid args construct; EmitsLiveValue is false (wire store)",
			assert: func(t *testing.T) {
				s, err := msginsql.NewQueueStore(openDB(t, fakeDriverName), "q", newFakeDialect())
				require.NoError(t, err)
				require.False(t, s.EmitsLiveValue())
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) { tt.assert(t) })
	}
}
```

(`msginsql` is the existing test alias for the `sql` package; match the alias the neighboring `_test.go` files use.)

- [ ] **Step 2: Implement `queuestore.go`** — the thin facade. Fields are **named** (`out`/`src`), NOT embedded: embedding both `*Outbound` and `*Source` would ambiguously promote `adapterBase`'s `Ready`/`EnsureSchema`/`classifyQueryErr`/etc. (they both embed `adapterBase`), so `Ready`/`EnsureSchema`/`NativeReliability` are **explicit forwarders** (audit B-3/M-2).

```go
package sql

import (
	"context"
	stdsql "database/sql"

	msgin "github.com/kartaladev/msgin"
)

// QueueStore is a durable, at-least-once msgin.ChannelStore backed by a single
// table. It is a thin facade pairing an Outbound (Enqueue = INSERT) with a Source
// (Claim = lease/claim, fenced Ack/Nack) over the SAME table — reusing the proven
// sql adapter machinery (ADR 0010/0011). Carries []byte (a wire store): the
// paired Producer JSON-encodes T, exactly as sql.Outbound. Named to mirror
// memory.QueueStore.
//
// Delivery guarantee: at-least-once across restarts/crashes (Guaranteed
// Delivery). Back a msgin.QueueChannel with this store when the flow segment must
// survive a restart.
type QueueStore struct {
	out *Outbound
	src *Source
}

var (
	_ msgin.ChannelStore     = (*QueueStore)(nil)
	_ msgin.NativeReliability = (*QueueStore)(nil)
)

// NewQueueStore builds a durable QueueStore over table on db with dialect. Options
// apply to both halves; lease-only options (WithLeaseTTL/WithStrategy/WithLockedBy)
// are inert on the Outbound and shared-tx options are inert on the Source, so there
// is no cross-misconfiguration (verified: options.go). A nil db is
// msgin.ErrNilAdapter; a nil dialect is ErrNilDialect; a bad table is
// ErrInvalidTableName. Call Ready once at boot (ADR 0010 D2).
func NewQueueStore(db *stdsql.DB, table string, dialect LeaseDialect, opts ...Option) (*QueueStore, error) {
	out, err := NewOutboundAdapter(db, table, dialect, opts...)
	if err != nil {
		return nil, err
	}
	src, err := NewPollingSource(db, table, dialect, opts...)
	if err != nil {
		return nil, err
	}
	return &QueueStore{out: out, src: src}, nil
}

// Enqueue INSERTs msg as an immediately-visible row.
func (q *QueueStore) Enqueue(ctx context.Context, msg msgin.Message[any]) error {
	return q.out.Send(ctx, msg)
}

// Claim leases up to max claimable rows as settleable Deliveries.
func (q *QueueStore) Claim(ctx context.Context, max int) ([]msgin.Delivery, error) {
	return q.src.Poll(ctx, max)
}

// EmitsLiveValue reports false: this is a wire store ([]byte payloads).
func (q *QueueStore) EmitsLiveValue() bool { return false }

// NativeRedelivery / NativeDeadLetter forward the Source's capability (true /
// false): the DB row redelivers on nack/lease-expiry, and dead-lettering is the
// runtime's RetryPolicy. Forwarded so a QueueChannel over this store behaves
// identically to the raw Source.
func (q *QueueStore) NativeRedelivery() bool { return q.src.NativeRedelivery() }
func (q *QueueStore) NativeDeadLetter() bool { return q.src.NativeDeadLetter() }

// Ready / EnsureSchema forward to the Source (both halves share one table, so
// either delegate suffices). Explicit forwarders — named fields do not promote.
func (q *QueueStore) Ready(ctx context.Context) error       { return q.src.Ready(ctx) }
func (q *QueueStore) EnsureSchema(ctx context.Context) error { return q.src.EnsureSchema(ctx) }
```

(`Ready`/`EnsureSchema` are value-receiver methods on `adapterBase`, base.go:50/59, reached via `q.src`.)

- [ ] **Step 3: Run the unit test**

Run: `GOTOOLCHAIN=go1.25.12 go test ./adapter/database/sql/ -run TestNewQueueStore_Construction`
Expected: PASS.

- [ ] **Step 4: Write the conformance runner** `harness.RunQueueStore(t, kit, db)` in `adapter/database/sql/harness/queuestore.go`, mirroring `RunSource` (harness/source.go:33) — it constructs a `sql.NewQueueStore` over a `kit`-provisioned table and drives it. Cases (assert-closure table): (a) `Enqueue`→`Claim`→`Ack` is durable — a second `Claim` after `Ack` is empty; (b) a `Nack(requeue=true, 0)`'d delivery is redelivered on the next `Claim`; (c) two `QueueStore`s over the SAME table get **disjoint** claims (competing consumers — no row claimed by both). Then **wire it into each dialect** by adding one line to `dbtest/conformance_{pg,mysql,sqlite}_test.go` alongside the existing `t.Run("Source", …)` block:

```go
t.Run("QueueStore", func(t *testing.T) { harness.RunQueueStore(t, kit, db) })
```

(The `TestKit` already carries the dialect + table-provisioning the other runners use — reuse it; no new testcontainers setup.)

- [ ] **Step 5: Run conformance (Docker required)**

Run: `GOTOOLCHAIN=go1.25.12 go test ./adapter/database/sql/dbtest/... -race` (with the container env the existing suite uses)
Expected: PASS across PG/MySQL/MariaDB/SQLite — `QueueStore` subtest green for every dialect.

- [ ] **Step 6: Coverage + module hygiene.**

Run: `GOTOOLCHAIN=go1.25.12 go test ./adapter/database/sql/... -cover` then `go mod tidy` (expect no diff), `go vet ./...`.
Expected: ≥85% on the `sql` package for `queuestore.go`; `go.mod`/`go.sum` unchanged (no new dep).

**Hot-path branches:** construction nil-db / nil-dialect / bad-table / valid (all four in Step 1); `Enqueue` insert, `Claim` claim, `Ack` durable-remove, `Nack` redelivery, competing-consumer disjointness (Step 4). The inherited fence-miss/`ErrStaleLease`/corrupt-row-skip live in the reused `Source` and are covered by its existing tests; the facade adds only delegation, exercised here.

- [ ] **Step 7: Commit**

```bash
git add adapter/database/sql/queuestore.go adapter/database/sql/queuestore_test.go \
        adapter/database/sql/harness/queuestore.go adapter/database/sql/dbtest/
git commit -m "$(cat <<'EOF'
feat(sql): durable QueueStore facade over Outbound+Source

At-least-once ChannelStore reusing the lease/claim/framing machinery; Enqueue=
INSERT, Claim=lease/claim, PG/MySQL/SQLite. Forwards NativeReliability/Ready/
EnsureSchema. No new durability code, no new dep.

Spec: 007
Plan: 013
ADR: 0018
EOF
)"
```

---

## Whole-branch delivery gate (after Task 4, before any merge)

- [ ] `GOTOOLCHAIN=go1.25.12 go build ./... && go test ./... -race` green (all modules; use `go.work`).
- [ ] `go vet ./...`, `golangci-lint run ./...`, `gofumpt -l .` clean; `go mod tidy`/`go mod verify` no diff; `CGO_ENABLED=0 go build ./...` ok; `govulncheck ./...` clean.
- [ ] `gorelease` / `apidiff` against the last tag: the new exported surface (`ChannelStore` interface, `QueueChannel` + `NewQueueChannel`, `ErrNilStore`, `ErrInvalidCapacity`, `memory.QueueStore` + `NewQueueStore` + `WithCapacity`/`WithOverflow`/`WithClock`, `sql.QueueStore` + `NewQueueStore`) is **additive** → minor SemVer bump, no break. Confirm the `ErrOverflowDropped` godoc change is documentation-only (no signature change).
- [ ] Run **`/code-review`** and **`/security-review`** over `main..HEAD`; resolve or triage-with-rationale every finding; re-run `-race`.
- [ ] Update `docs/HANDOVER.md`; confirm spec/ADR/plan cross-links and commit trailers.

## Self-review notes (coverage of Spec 007)

- G1 `QueueChannel` (Send/Poll/EmitsLiveValue/NativeReliability) → Task 1. G2 narrow `ChannelStore` SPI → Task 1 (Delivery reused, no `Claimed`; O7-5 resolved). G3 memory store → Task 2, SQL store → Task 4. G4 defaults (cap 1024 / Block / FIFO) → Task 2. G5 goleak/shutdown/typed errors → Tasks 1–3.
- D6 capacity/overflow located on the **store** (only the bounded backend has a buffer) — ADR 0018 §4 + Spec 007 D6 synced. `Claim` reuses `msgin.Delivery` (no `Claimed`; O7-5) — synced across spec/ADR.
- N1 no core runtime change — verified: only new files in core + two sentinels + one godoc fix. N2 not Subscribe-based — `QueueChannel` implements `OutboundAdapter`+`PollingSource`(+`LiveValueSource`/`NativeReliability`), not `MessageChannel`.
- **Adversarial audit round 1 (2026-07-20) — all findings folded:** B-1 (DropNewest→`nil`, not `ErrOverflowDropped`), B-2 (e2e `Handler[string]`/`msgin.New(s)`/`WithPollInterval` — exact signatures verified), B-3 (`Ready`/`EnsureSchema` explicit forwarders; named fields don't promote), B-4 (added Nack-drop / fence-miss / Claim-max-cap / DropOldest-nothing-evictable test cases). H-1 (`ErrOverflowDropped` godoc corrected). H-2 (`sql.ChannelStore`→`sql.QueueStore`, symmetry + no interface-shadow). M-1 (`Claim` negative-max guard). M-2 (`NativeReliability` **forwarded**, not just documented). M-3 (TOCTOU-shed godoc note). L-1/L-2/L-3 folded. **Every placeholder resolved** to a concrete signature/helper (`openDB(t, fakeDriverName)`, `newFakeDialect()`, `harness.RunQueueStore` + `dbtest` wiring).
- Resolved open items: O7-4 — keep the SQL facade (renamed `QueueStore`): it is the *only* `ChannelStore` a durable `QueueChannel` can use (neither `Source` nor `Outbound` is one), so it earns its ~40 lines as the swap adapter. O7-6 (in-memory lease-TTL reclaim) — omitted (YAGNI; within one process a panicking handler is Nacked by the runtime, and process exit loses the buffer anyway).
- **Adversarial audit round 2 (2026-07-20) — bundle SOUND.** Every round-1 fix VERIFIED-FIXED; the rewritten material confirmed correct (shed token-accounting invariant `tokens==len(ready)+len(inflight)`; `NativeReliability` forwarding is indistinguishable from the runtime default at the sole resolution site consumer.go:209, no regression; memory-store settle closures release the mutex before `<-sem`, no lock hazard; e2e is goleak-checked + `Run` joins all goroutines; SQL-facade options are inert across halves, no collision). No code-snippet bugs. Sole residual (two stale `sql.ChannelStore` header refs) fixed. No further re-audit needed.
