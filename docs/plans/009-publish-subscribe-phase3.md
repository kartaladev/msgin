# Publish-Subscribe (composition Phase 3) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.
>
> **Mandatory Go skills (CLAUDE.md hard rule):** every task starts from **`cc-skills-golang:golang-how-to`** (the always-on orchestrator — it loads the relevant `golang-*` skills: error handling, naming, concurrency, structs/interfaces, testing, lint, security, …). Follow **TDD** via **`superpowers:test-driven-development`** (red → green → refactor). Use **`gopls`** (the native `LSP` tool) for all Go navigation, diagnostics, and refactoring — prefer it over text search when reasoning about symbols. Obey the project-local testing skills, which **override** samber's testing guidance where they conflict: **`table-test`** (assert-closure tables, `ctx`/`t.Context()`), **`use-mockgen`** (uber-go/mock, `--typed`, mock beside the interface), **`use-testcontainers`** (real external resources, never fakes). These are not optional.

**Goal:** Ship an in-process **Publish-Subscribe Channel** with dynamic subscribe/unsubscribe, a topic pub/sub SPI, and an EIP-native topic registry (`PubSub`), so a produced message can fan out to every subscriber of a topic.

**Architecture:** A single-topic `PublishSubscribeChannel` (the building block) fans out synchronously to all subscribers on the caller's goroutine (no goroutine started); `Subscribe` returns a `Subscription` handle whose `Cancel()` unsubscribes. A `PubSub` registry maps topic name → channel (lazily created, dropped when empty) and satisfies the `TopicPublisher`/`TopicSubscriber` SPI that future native-topic broker adapters will implement. Default fan-out settlement is all-subscribers-succeed (joined error); `WithFanOut(FanOutBestEffort)` logs-and-continues. **Purely additive** — no changes to shipped Phase-1 code.

**Tech Stack:** Go 1.25, stdlib only (no new dependency). Tests: blackbox `_test` packages, `stretchr/testify`, `goleak` (already wired via the root `TestMain`).

**Traceability:** Implements [Spec 004](../specs/004-publish-subscribe.md); realizes [ADR 0014](../adrs/0014-publish-subscribe.md); details [Spec 003](../specs/003-composition-endpoints.md) §3 D7 Phase 3. Commits carry `Spec: 004` / `Plan: 009` / `ADR: 0014` trailers. **Task 1's commit couples Plan 009 + Spec 004 + ADR 0014 with the first code.**

## Global Constraints

- **Go skills (mandatory, CLAUDE.md)** — start every task from `cc-skills-golang:golang-how-to`; TDD via `superpowers:test-driven-development`; navigate/refactor with `gopls` (native `LSP` tool); obey the project-local `table-test` / `use-mockgen` / `use-testcontainers` skills (they override samber's testing guidance on conflict). See the header note.
- **Go 1.25** — `GOTOOLCHAIN=go1.25.12`; no language/stdlib features newer than 1.25 (note: `slog.DiscardHandler` is Go 1.24+, so it is allowed).
- **Stdlib-only core** — no new direct dependency; `go mod tidy` must leave `go.mod`/`go.sum` unchanged.
- **Root `package msgin`** — all new production symbols exported from the root package.
- **Reuse existing sentinels** — `ErrNilHandler` **already exists** in `errors.go` (added in Plan 008). Do **not** redeclare it; a nil handler on `Subscribe` returns it.
- **Blackbox tests only** — every `_test.go` is `package msgin_test`. Example tests too.
- **Assert-closure tables** — every table case carries an `assert func(t *testing.T, …)` closure (never `want`/`wantErr` fields); `t.Context()` over `context.Background()`; ≥2 cases of the same call ⇒ a table (`table-test` skill).
- **No panic on caller input** — a nil handler returns `ErrNilHandler`; `Cancel()` is idempotent (double-cancel is a safe no-op); publishing to a topic with no subscribers is a no-op (never a panic).
- **No goroutine leaks** — dispatch is synchronous (no goroutine started); `goleak` (root `TestMain`) must stay clean on every test.
- **Concurrency-safe** — the subscriber set is mutex-guarded; `Send` snapshots under `RLock` and dispatches outside the lock; `go test ./... -race` clean under concurrent subscribe/cancel during a publish.
- **Every exported symbol has a godoc comment**; defaults documented with rationale on the option godoc.
- **Deferred (documented, not built this plan):** `Close()` (O4-1 — YAGNI for a goroutine-free synchronous channel; a broker adapter implements `io.Closer` when it has resources) and the `Router`/`Filter` → `OutboundAdapter` widening (O4-2 — `To(pubsubChannel)` already works; the `Router.pick` return-type widening is a breaking change needing its own ADR).
- **Gate before the final increment commit** — `go test ./... -race` green, `go vet`/`gofmt`/`golangci-lint`/`govulncheck` clean, `CGO_ENABLED=0 go build ./...` succeeds, coverage ≥85% on the root package with every hot-path/typed-error branch covered.

---

### Task 1: `PublishSubscribeChannel` — single-topic fan-out + `Subscription` + settlement policy

**Files:**
- Create: `pubsub.go` (`PublishSubscribeChannel`, `Subscription`, `FanOutPolicy`, `PubSubOption`, `WithFanOut`, `WithPubSubLogger`)
- Test: `pubsub_test.go`

**Interfaces:**
- Consumes: `MessageHandler` (`handler.go`), `Message[any]`, `OutboundAdapter` (`spi.go`), existing `ErrNilHandler` (`errors.go`).
- Produces:
  - `type FanOutPolicy int` with `FanOutAllSucceed FanOutPolicy = iota` (default) and `FanOutBestEffort`.
  - `type Subscription interface { Cancel() }`.
  - `type PubSubOption func(*pubSubConfig)`; `func WithFanOut(p FanOutPolicy) PubSubOption`; `func WithPubSubLogger(l *slog.Logger) PubSubOption`.
  - `type PublishSubscribeChannel struct{…}`; `func NewPublishSubscribeChannel(opts ...PubSubOption) *PublishSubscribeChannel`; methods `Send(ctx, Message[any]) error`, `Subscribe(h MessageHandler) (Subscription, error)`.
  - unexported: `pubSubConfig`, `defaultPubSubConfig`, `subscription`, `(*PublishSubscribeChannel).remove`, `(*PublishSubscribeChannel).isEmpty` (used by Task 2).

- [ ] **Step 1: Write the failing test** — `pubsub_test.go`

```go
package msgin_test

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kartaladev/msgin"
)

func TestPublishSubscribeChannel_FanOut(t *testing.T) {
	tests := []struct {
		name    string
		policy  msgin.FanOutPolicy
		errs    []error // one per subscriber, nil = success
		assert  func(t *testing.T, sendErr error, got []string)
	}{
		{
			name: "fans out to all subscribers in registration order",
			errs: []error{nil, nil, nil},
			assert: func(t *testing.T, sendErr error, got []string) {
				require.NoError(t, sendErr)
				assert.Equal(t, []string{"a", "b", "c"}, got)
			},
		},
		{
			name: "all-succeed: a subscriber error is joined, others still invoked",
			errs: []error{nil, errors.New("boom"), nil},
			assert: func(t *testing.T, sendErr error, got []string) {
				assert.ErrorContains(t, sendErr, "boom")
				assert.Equal(t, []string{"a", "b", "c"}, got) // every subscriber still ran
			},
		},
		{
			name:   "best-effort: a subscriber error is swallowed, Send returns nil",
			policy: msgin.FanOutBestEffort,
			errs:   []error{nil, errors.New("boom"), nil},
			assert: func(t *testing.T, sendErr error, got []string) {
				require.NoError(t, sendErr)
				assert.Equal(t, []string{"a", "b", "c"}, got)
			},
		},
		{
			name: "no subscribers is a no-op",
			errs: nil,
			assert: func(t *testing.T, sendErr error, got []string) {
				require.NoError(t, sendErr)
				assert.Empty(t, got)
			},
		},
	}
	tags := []string{"a", "b", "c"}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ps := msgin.NewPublishSubscribeChannel(msgin.WithFanOut(tc.policy))
			var got []string
			for i, e := range tc.errs {
				tag, e := tags[i], e
				_, err := ps.Subscribe(msgin.HandlerFunc(func(context.Context, msgin.Message[any]) error {
					got = append(got, tag)
					return e
				}))
				require.NoError(t, err)
			}
			tc.assert(t, ps.Send(t.Context(), msgin.New[any]("x")), got)
		})
	}
}

func TestPublishSubscribeChannel_SubscribeAndCancel(t *testing.T) {
	t.Run("nil handler is ErrNilHandler", func(t *testing.T) {
		_, err := msgin.NewPublishSubscribeChannel().Subscribe(nil)
		assert.ErrorIs(t, err, msgin.ErrNilHandler)
	})
	t.Run("cancel removes the subscriber and is idempotent", func(t *testing.T) {
		ps := msgin.NewPublishSubscribeChannel()
		var count int
		sub, err := ps.Subscribe(msgin.HandlerFunc(func(context.Context, msgin.Message[any]) error { count++; return nil }))
		require.NoError(t, err)
		require.NoError(t, ps.Send(t.Context(), msgin.New[any](1)))
		assert.Equal(t, 1, count)
		sub.Cancel()
		sub.Cancel() // idempotent: no panic, no double-remove
		require.NoError(t, ps.Send(t.Context(), msgin.New[any](2)))
		assert.Equal(t, 1, count) // cancelled: not invoked again
	})
}

func TestPublishSubscribeChannel_SubscriberPanicIsIsolated(t *testing.T) {
	tests := []struct {
		name   string
		policy msgin.FanOutPolicy
		assert func(t *testing.T, sendErr error, laterRan bool)
	}{
		{
			name: "all-succeed: panic is a transient error, later subscribers still run",
			assert: func(t *testing.T, sendErr error, laterRan bool) {
				assert.ErrorIs(t, sendErr, msgin.ErrHandlerPanic)
				assert.True(t, laterRan)
			},
		},
		{
			name:   "best-effort: panic is logged, Send returns nil, later subscribers still run",
			policy: msgin.FanOutBestEffort,
			assert: func(t *testing.T, sendErr error, laterRan bool) {
				require.NoError(t, sendErr)
				assert.True(t, laterRan)
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ps := msgin.NewPublishSubscribeChannel(msgin.WithFanOut(tc.policy))
			_, err := ps.Subscribe(msgin.HandlerFunc(func(context.Context, msgin.Message[any]) error { panic("boom") }))
			require.NoError(t, err)
			var laterRan bool
			_, err = ps.Subscribe(msgin.HandlerFunc(func(context.Context, msgin.Message[any]) error { laterRan = true; return nil }))
			require.NoError(t, err)
			tc.assert(t, ps.Send(t.Context(), msgin.New[any](1)), laterRan)
		})
	}
}

func TestPublishSubscribeChannel_PermanentErrorPropagates(t *testing.T) {
	// Unit-settlement (F2): a subscriber's permanent error makes the joined fan-out
	// permanent (errors.Join propagates it), even mixed with a transient failure —
	// so a Consumer-driven publish diverts the whole message to the invalid sink.
	ps := msgin.NewPublishSubscribeChannel()
	_, err := ps.Subscribe(msgin.HandlerFunc(func(context.Context, msgin.Message[any]) error { return msgin.ErrPayloadType }))
	require.NoError(t, err)
	_, err = ps.Subscribe(msgin.HandlerFunc(func(context.Context, msgin.Message[any]) error { return errors.New("transient") }))
	require.NoError(t, err)
	assert.ErrorIs(t, ps.Send(t.Context(), msgin.New[any](1)), msgin.ErrPayloadType)
}

func TestPublishSubscribeChannel_BestEffortLogsToInjectedLogger(t *testing.T) {
	var buf strings.Builder
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))
	ps := msgin.NewPublishSubscribeChannel(msgin.WithFanOut(msgin.FanOutBestEffort), msgin.WithPubSubLogger(logger))
	_, err := ps.Subscribe(msgin.HandlerFunc(func(context.Context, msgin.Message[any]) error { return errors.New("sub-fail") }))
	require.NoError(t, err)
	require.NoError(t, ps.Send(t.Context(), msgin.New[any](1)))
	assert.Contains(t, buf.String(), "sub-fail")
	_ = msgin.NewPublishSubscribeChannel(msgin.WithPubSubLogger(nil)) // nil is a no-op (keeps default discard logger)
}

func TestPublishSubscribeChannel_IsOutboundAdapter(t *testing.T) {
	var _ msgin.OutboundAdapter = msgin.NewPublishSubscribeChannel() // compiles => Send satisfies the SPI
}
```

- [ ] **Step 2: Run test to verify it fails** — `GOTOOLCHAIN=go1.25.12 go test -run 'TestPublishSubscribeChannel' .` → FAIL (undefined symbols).

- [ ] **Step 3: Implement** — `pubsub.go`

```go
package msgin

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
)

// FanOutPolicy selects how a PublishSubscribeChannel settles a fan-out when a
// subscriber's handler returns an error.
type FanOutPolicy int

const (
	// FanOutAllSucceed (the default) invokes every subscriber and, if any returns
	// an error, Send returns a joined error — so a Consumer-driven publish
	// Nacks/retries (at-least-once for the whole fan-out). Because a retry
	// re-invokes ALL subscribers, subscribers should be idempotent.
	//
	// The fan-out settles as ONE unit: the joined error's classification follows
	// the runtime's rules, so if any subscriber returns a permanent error (e.g.
	// ErrPayloadType — errors.Join propagates it), a Consumer-driven publish routes
	// the WHOLE message to the invalid-message sink (observable, not retried);
	// otherwise it is transient and the whole fan-out retries. A subscriber whose
	// permanent failure must NOT affect the others' redelivery needs per-subscriber
	// independent settlement — a durable adapter concern, out of scope here.
	FanOutAllSucceed FanOutPolicy = iota
	// FanOutBestEffort invokes every subscriber, logs each error, and Send always
	// returns nil (Ack). A failed delivery is NOT retried — use only when a missed
	// subscriber is acceptable.
	FanOutBestEffort
)

// Subscription is a handle to an active subscription. Cancel removes the
// subscriber; it is idempotent (a second Cancel is a safe no-op).
type Subscription interface{ Cancel() }

type pubSubConfig struct {
	policy FanOutPolicy
	logger *slog.Logger
}

func defaultPubSubConfig() pubSubConfig {
	return pubSubConfig{policy: FanOutAllSucceed, logger: slog.New(slog.DiscardHandler)}
}

// PubSubOption configures a PublishSubscribeChannel or a PubSub registry.
type PubSubOption func(*pubSubConfig)

// WithFanOut sets the fan-out settlement policy. The default, FanOutAllSucceed,
// is the safe choice: a subscriber error surfaces (joined) so the message is
// retried rather than silently missed. Choose FanOutBestEffort only when a
// dropped delivery to one subscriber is acceptable.
func WithFanOut(p FanOutPolicy) PubSubOption { return func(c *pubSubConfig) { c.policy = p } }

// WithPubSubLogger injects the logger used to report subscriber errors under
// FanOutBestEffort. Defaults to a discarding logger (no output).
func WithPubSubLogger(l *slog.Logger) PubSubOption {
	return func(c *pubSubConfig) {
		if l != nil {
			c.logger = l
		}
	}
}

// subscription is one registered handler on a PublishSubscribeChannel.
type subscription struct {
	ch      *PublishSubscribeChannel
	handler MessageHandler
	once    sync.Once
}

// Cancel removes the subscriber from its channel (idempotent).
func (s *subscription) Cancel() { s.once.Do(func() { s.ch.remove(s) }) }

// PublishSubscribeChannel is an in-process EIP Publish-Subscribe Channel: Send
// fans a message out to EVERY subscriber synchronously, on the caller's
// goroutine, in registration order (no goroutine is started). Subscribe returns
// a Subscription whose Cancel unsubscribes. It is an OutboundAdapter (Send), so
// a flow can terminate in To(psChannel) to broadcast.
type PublishSubscribeChannel struct {
	mu   sync.RWMutex
	subs []*subscription
	cfg  pubSubConfig
}

var _ OutboundAdapter = (*PublishSubscribeChannel)(nil)

// NewPublishSubscribeChannel returns an empty channel; Subscribe handlers, then Send.
func NewPublishSubscribeChannel(opts ...PubSubOption) *PublishSubscribeChannel {
	c := &PublishSubscribeChannel{cfg: defaultPubSubConfig()}
	for _, opt := range opts {
		opt(&c.cfg)
	}
	return c
}

// Subscribe registers h and returns a Subscription. A nil handler is ErrNilHandler.
func (c *PublishSubscribeChannel) Subscribe(h MessageHandler) (Subscription, error) {
	if h == nil {
		return nil, ErrNilHandler
	}
	s := &subscription{ch: c, handler: h}
	c.mu.Lock()
	c.subs = append(c.subs, s)
	c.mu.Unlock()
	return s, nil
}

// remove deletes s from the subscriber slice (called by subscription.Cancel).
func (c *PublishSubscribeChannel) remove(s *subscription) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for i, x := range c.subs {
		if x == s {
			c.subs = append(c.subs[:i], c.subs[i+1:]...)
			return
		}
	}
}

// isEmpty reports whether the channel has no subscribers (used by PubSub for
// drop-on-empty topic hygiene).
func (c *PublishSubscribeChannel) isEmpty() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.subs) == 0
}

// Send fans msg out to every current subscriber. It snapshots the subscriber set
// under a read lock and dispatches OUTSIDE the lock (so a handler may
// Subscribe/Cancel without deadlock, and concurrent Sends do not serialize on
// handler execution). Under FanOutAllSucceed a subscriber error is collected and
// the joined error returned after every subscriber has run; under
// FanOutBestEffort errors are logged and Send returns nil.
//
// Concurrency semantics: a subscriber cancelled AFTER Send snapshots still
// receives this in-flight message (same as DirectChannel). A Send that races the
// last Cancel may fan out to zero subscribers and return nil (delivered-to-none).
// A panicking subscriber is recovered per-subscriber (ErrHandlerPanic, transient)
// so it never aborts the fan-out — the loop always reaches every subscriber.
func (c *PublishSubscribeChannel) Send(ctx context.Context, msg Message[any]) error {
	c.mu.RLock()
	snapshot := make([]*subscription, len(c.subs))
	copy(snapshot, c.subs)
	c.mu.RUnlock()

	var errs []error
	for _, s := range snapshot {
		if err := safeFanOut(ctx, s.handler, msg); err != nil {
			if c.cfg.policy == FanOutBestEffort {
				c.cfg.logger.WarnContext(ctx, "msgin: pub-sub subscriber failed (best-effort)", "err", err)
				continue
			}
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...) // nil when errs is empty
}

// safeFanOut invokes one subscriber, recovering a panic into a transient
// ErrHandlerPanic so a panicking subscriber cannot abort the fan-out (fault
// isolation, CLAUDE.md) — the caller's loop continues to the remaining
// subscribers. ErrHandlerPanic is classified transient (reliability.go), so under
// FanOutAllSucceed a panicked subscriber makes the fan-out retry rather than divert.
func safeFanOut(ctx context.Context, h MessageHandler, msg Message[any]) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("%w: %v", ErrHandlerPanic, r)
		}
	}()
	return h.Handle(ctx, msg)
}
```

- [ ] **Step 4: Run tests** — `GOTOOLCHAIN=go1.25.12 go test -race -run 'TestPublishSubscribeChannel' .` → PASS.

- [ ] **Step 5: Commit** (couples Plan 009 + Spec 004 + ADR 0014 with the first code)

```bash
git add pubsub.go pubsub_test.go docs/plans/009-publish-subscribe-phase3.md docs/specs/004-publish-subscribe.md docs/adrs/0014-publish-subscribe.md
git commit -m "feat(core): add PublishSubscribeChannel (single-topic fan-out)

Synchronous, in-process EIP Publish-Subscribe Channel: Send fans out to
every subscriber on the caller's goroutine (no goroutine started);
Subscribe returns a Subscription handle whose Cancel unsubscribes.
Fan-out settlement is all-succeed (joined error) by default, best-effort
opt-in. Purely additive; stdlib-only.

Spec: 004
Plan: 009
ADR: 0014"
```

---

### Task 2: topic pub/sub SPI (`TopicPublisher`/`TopicSubscriber`) + `PubSub` registry

**Files:**
- Create: `pubsub_registry.go` (`TopicPublisher`, `TopicSubscriber`, `PubSub`, `NewPubSub`, `TopicCount`)
- Test: `pubsub_registry_test.go`

**Interfaces:**
- Consumes: `PublishSubscribeChannel`, `NewPublishSubscribeChannel`, `Subscription`, `MessageHandler`, `PubSubOption`, `pubSubConfig`, `ErrNilHandler`, `(*PublishSubscribeChannel).isEmpty` (Task 1).
- Produces:
  - `type TopicPublisher interface { Publish(ctx context.Context, topic string, msg Message[any]) error }`
  - `type TopicSubscriber interface { Subscribe(topic string, h MessageHandler) (Subscription, error) }`
  - `type PubSub struct{…}`; `func NewPubSub(opts ...PubSubOption) *PubSub`; methods `Publish`, `Subscribe`, `TopicCount() int`.

- [ ] **Step 1: Write the failing test** — `pubsub_registry_test.go`

```go
package msgin_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kartaladev/msgin"
)

func TestPubSub_TopicScopedDelivery(t *testing.T) {
	ps := msgin.NewPubSub()
	var a, b int
	_, err := ps.Subscribe("topic-a", msgin.HandlerFunc(func(context.Context, msgin.Message[any]) error { a++; return nil }))
	require.NoError(t, err)
	_, err = ps.Subscribe("topic-b", msgin.HandlerFunc(func(context.Context, msgin.Message[any]) error { b++; return nil }))
	require.NoError(t, err)

	require.NoError(t, ps.Publish(t.Context(), "topic-a", msgin.New[any]("x")))
	assert.Equal(t, 1, a)
	assert.Equal(t, 0, b) // topic-scoped: topic-b did not receive topic-a's message
}

func TestPubSub_CancelOneOfSeveralKeepsTopic(t *testing.T) {
	// F4: cancelling one of several subscribers keeps the topic alive (the
	// drop-on-empty KEEP branch) and the survivor still receives publishes.
	ps := msgin.NewPubSub()
	var s1, s2 int
	sub1, err := ps.Subscribe("t", msgin.HandlerFunc(func(context.Context, msgin.Message[any]) error { s1++; return nil }))
	require.NoError(t, err)
	_, err = ps.Subscribe("t", msgin.HandlerFunc(func(context.Context, msgin.Message[any]) error { s2++; return nil }))
	require.NoError(t, err)

	sub1.Cancel()
	assert.Equal(t, 1, ps.TopicCount()) // topic survives: s2 is still subscribed

	require.NoError(t, ps.Publish(t.Context(), "t", msgin.New[any]("x")))
	assert.Equal(t, 0, s1) // cancelled: not invoked
	assert.Equal(t, 1, s2) // survivor received
}

func TestPubSub_Behaviors(t *testing.T) {
	tests := []struct {
		name   string
		run    func(t *testing.T, ps *msgin.PubSub) error
		assert func(t *testing.T, err error, ps *msgin.PubSub)
	}{
		{
			name: "publish to a topic with no subscribers is a no-op",
			run:  func(t *testing.T, ps *msgin.PubSub) error { return ps.Publish(t.Context(), "nobody", msgin.New[any](1)) },
			assert: func(t *testing.T, err error, ps *msgin.PubSub) {
				require.NoError(t, err)
				assert.Equal(t, 0, ps.TopicCount())
			},
		},
		{
			name: "nil handler is ErrNilHandler",
			run:  func(t *testing.T, ps *msgin.PubSub) error { _, err := ps.Subscribe("t", nil); return err },
			assert: func(t *testing.T, err error, ps *msgin.PubSub) {
				assert.ErrorIs(t, err, msgin.ErrNilHandler)
				assert.Equal(t, 0, ps.TopicCount()) // no topic created for a rejected subscribe
			},
		},
		{
			name: "subscribe lazily creates the topic; cancel drops it when empty",
			run: func(t *testing.T, ps *msgin.PubSub) error {
				sub, err := ps.Subscribe("t", msgin.HandlerFunc(func(context.Context, msgin.Message[any]) error { return nil }))
				require.NoError(t, err)
				require.Equal(t, 1, ps.TopicCount()) // lazily created
				sub.Cancel()
				return nil
			},
			assert: func(t *testing.T, err error, ps *msgin.PubSub) {
				require.NoError(t, err)
				assert.Equal(t, 0, ps.TopicCount()) // dropped on empty
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ps := msgin.NewPubSub()
			tc.assert(t, tc.run(t, ps), ps)
		})
	}
}

func TestPubSub_SatisfiesSPI(t *testing.T) {
	var _ msgin.TopicPublisher = msgin.NewPubSub()
	var _ msgin.TopicSubscriber = msgin.NewPubSub()
}
```

- [ ] **Step 2: Run test to verify it fails** — `GOTOOLCHAIN=go1.25.12 go test -run 'TestPubSub' .` → FAIL (undefined symbols).

- [ ] **Step 3: Implement** — `pubsub_registry.go`

```go
package msgin

import (
	"context"
	"sync"
)

// TopicPublisher publishes a message to a named topic. Native-topic broker
// adapters (Kafka, NATS, Redis) implement this using their own topics, so topic
// support is handled generically through one SPI.
type TopicPublisher interface {
	Publish(ctx context.Context, topic string, msg Message[any]) error
}

// TopicSubscriber subscribes a handler to a named topic, returning a Subscription
// whose Cancel unsubscribes. The counterpart SPI to TopicPublisher (split per the
// interface-segregation principle: a publish-only or subscribe-only adapter is
// legitimate).
type TopicSubscriber interface {
	Subscribe(topic string, h MessageHandler) (Subscription, error)
}

// PubSub is the in-process topic registry: it maps a topic name to a
// PublishSubscribeChannel, created on first Subscribe and dropped when its last
// subscriber cancels. Publish fans out to that topic's subscribers only.
type PubSub struct {
	mu     sync.Mutex
	topics map[string]*PublishSubscribeChannel
	cfg    pubSubConfig
}

var (
	_ TopicPublisher  = (*PubSub)(nil)
	_ TopicSubscriber = (*PubSub)(nil)
)

// NewPubSub returns an empty registry. Options apply to every topic channel it creates.
func NewPubSub(opts ...PubSubOption) *PubSub {
	p := &PubSub{topics: make(map[string]*PublishSubscribeChannel), cfg: defaultPubSubConfig()}
	for _, opt := range opts {
		opt(&p.cfg)
	}
	return p
}

// Publish fans msg out to the topic's subscribers. A topic with no subscribers is
// a no-op (never an error): publishing before anyone subscribes is normal for
// broadcast. It returns the topic channel's joined fan-out error (see FanOutPolicy).
func (p *PubSub) Publish(ctx context.Context, topic string, msg Message[any]) error {
	p.mu.Lock()
	ch := p.topics[topic]
	p.mu.Unlock()
	if ch == nil {
		return nil
	}
	return ch.Send(ctx, msg)
}

// Subscribe registers h on topic, lazily creating the topic channel. The returned
// Subscription's Cancel unsubscribes AND drops the topic if it becomes empty. A
// nil handler is ErrNilHandler (no topic is created).
func (p *PubSub) Subscribe(topic string, h MessageHandler) (Subscription, error) {
	if h == nil {
		return nil, ErrNilHandler
	}
	p.mu.Lock()
	ch := p.topics[topic]
	if ch == nil {
		ch = NewPublishSubscribeChannel(withConfig(p.cfg))
		p.topics[topic] = ch
	}
	// F1: subscribe UNDER p.mu, so a concurrent last-subscriber Cancel cannot drop
	// the topic in the window between the map insert and the subscribe (a TOCTOU
	// that would orphan this subscriber on a channel no longer in the registry).
	// Lock order stays p.mu -> ch.mu — the SAME nesting topicSubscription.Cancel
	// uses when it calls isEmpty() under p.mu — so no deadlock; and ch.Subscribe
	// runs no handler code, so holding p.mu across it cannot re-enter the registry.
	inner, err := ch.Subscribe(h)
	p.mu.Unlock()
	if err != nil { // defensive: ch.Subscribe only errors on a nil handler, already guarded above
		return nil, err
	}
	return &topicSubscription{ps: p, topic: topic, ch: ch, inner: inner}, nil
}

// TopicCount reports the number of live topics (topics with ≥1 subscriber). Zero
// after every subscriber of every topic has cancelled — proves drop-on-empty.
func (p *PubSub) TopicCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.topics)
}

// topicSubscription wraps a channel Subscription so Cancel also GCs an empty topic.
type topicSubscription struct {
	ps    *PubSub
	topic string
	ch    *PublishSubscribeChannel
	inner Subscription
}

// Cancel unsubscribes, then drops the topic if it has no remaining subscribers.
func (s *topicSubscription) Cancel() {
	s.inner.Cancel()
	s.ps.mu.Lock()
	defer s.ps.mu.Unlock()
	// Only drop the exact channel we hold, and only if still empty — a concurrent
	// Subscribe to the same topic may have re-populated or replaced it.
	if cur, ok := s.ps.topics[s.topic]; ok && cur == s.ch && cur.isEmpty() {
		delete(s.ps.topics, s.topic)
	}
}
```

Also add the small internal option helper to `pubsub.go` (lets `PubSub` seed a channel with its own config):

```go
// withConfig seeds a channel with an already-built config (used by PubSub so all
// topic channels inherit the registry's fan-out policy and logger).
func withConfig(cfg pubSubConfig) PubSubOption { return func(c *pubSubConfig) { *c = cfg } }
```

- [ ] **Step 4: Run tests** — `GOTOOLCHAIN=go1.25.12 go test -race -run 'TestPubSub' .` → PASS.

- [ ] **Step 5: Commit**

```bash
git add pubsub.go pubsub_registry.go pubsub_registry_test.go
git commit -m "feat(core): add topic pub/sub SPI + PubSub registry

TopicPublisher/TopicSubscriber SPI (the seam native-topic adapters
implement) + an in-process PubSub registry: topic name -> a lazily
created PublishSubscribeChannel, dropped when its last subscriber
cancels. Publish is topic-scoped; empty-topic publish is a no-op.

Spec: 004
Plan: 009
ADR: 0014"
```

---

### Task 3: end-to-end integration (through `NewConsumer`), example, package doc, whole-branch gate

Proves the headline claim — a fan-out driven by the existing `Consumer` runtime — and that dynamic subscribe/cancel is race-safe.

**Files:**
- Create: `example_pubsub_test.go` (runnable `Example` — doubles as godoc)
- Create: `pubsub_integration_test.go` (fan-out driven through `NewConsumer[any]` over `memory.New()`; concurrent subscribe/cancel during publish)
- Modify: `doc_composition.go` (append a pub-sub paragraph to the existing package doc — do NOT create a second package-doc file; `doc_composition.go` already holds the `package msgin` doc from Plan 008)
- Test: the example + the integration test are their own tests.

**Interfaces:**
- Consumes: all Task 1–2 symbols + existing `NewConsumer`, `WithShutdownTimeout`, `adapter/memory`.

- [ ] **Step 1: Write the failing integration test** — `pubsub_integration_test.go`

```go
package msgin_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kartaladev/msgin"
	"github.com/kartaladev/msgin/adapter/memory"
)

// A consumer whose handler publishes each message to a topic fans it out to all
// subscribers, inheriting the runtime (retry/DLQ/flow-control/shutdown).
func TestPubSub_DrivesOffConsumer(t *testing.T) {
	src := memory.New(memory.WithBuffer(4))
	ps := msgin.NewPubSub()

	var seenA, seenB atomic.Int64
	_, err := ps.Subscribe("events", msgin.HandlerFunc(func(context.Context, msgin.Message[any]) error { seenA.Add(1); return nil }))
	require.NoError(t, err)
	_, err = ps.Subscribe("events", msgin.HandlerFunc(func(context.Context, msgin.Message[any]) error { seenB.Add(1); return nil }))
	require.NoError(t, err)

	consumer, err := msgin.NewConsumer[any](src,
		func(ctx context.Context, m msgin.Message[any]) error { return ps.Publish(ctx, "events", m) },
		msgin.WithShutdownTimeout[any](2*time.Second),
	)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- consumer.Run(ctx) }()

	require.NoError(t, src.Send(ctx, msgin.New[any]("e-1")))

	require.Eventually(t, func() bool { return seenA.Load() == 1 && seenB.Load() == 1 }, 2*time.Second, 5*time.Millisecond)

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("consumer did not shut down")
	}
	// goleak (root TestMain) verifies no goroutine leak.
}

// Subscribe/Cancel racing a Publish must be race-clean and never panic.
func TestPubSub_ConcurrentSubscribeCancelDuringPublish(t *testing.T) {
	ps := msgin.NewPublishSubscribeChannel()
	var wg sync.WaitGroup
	stop := make(chan struct{})

	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				_ = ps.Send(t.Context(), msgin.New[any](1))
			}
		}
	}()
	for i := 0; i < 50; i++ {
		sub, err := ps.Subscribe(msgin.HandlerFunc(func(context.Context, msgin.Message[any]) error { return nil }))
		require.NoError(t, err)
		sub.Cancel()
	}
	close(stop)
	wg.Wait()
}

// F1 regression: a Subscribe to a topic racing the last-subscriber Cancel of the
// SAME topic must not orphan the new subscriber (the registry-level TOCTOU the
// bare-channel test above cannot reach). Deterministic-pass under the fix
// (Subscribe holds p.mu across ch.Subscribe); flaky-fails without it.
func TestPubSub_SubscribeRacesLastCancel(t *testing.T) {
	for i := 0; i < 200; i++ {
		ps := msgin.NewPubSub()
		sub1, err := ps.Subscribe("t", msgin.HandlerFunc(func(context.Context, msgin.Message[any]) error { return nil }))
		require.NoError(t, err)

		var got atomic.Int64
		var wg sync.WaitGroup
		wg.Add(2)
		go func() { defer wg.Done(); sub1.Cancel() }()
		go func() {
			defer wg.Done()
			_, err := ps.Subscribe("t", msgin.HandlerFunc(func(context.Context, msgin.Message[any]) error { got.Add(1); return nil }))
			assert.NoError(t, err)
		}()
		wg.Wait()

		require.NoError(t, ps.Publish(t.Context(), "t", msgin.New[any](1)))
		assert.Equal(t, int64(1), got.Load(), "iteration %d: surviving subscriber missed the publish (F1 TOCTOU)", i)
		assert.Equal(t, 1, ps.TopicCount())
	}
}
```

- [ ] **Step 2: Run it, verify it passes after Tasks 1–2**

Run: `GOTOOLCHAIN=go1.25.12 go test -race -run 'TestPubSub_DrivesOffConsumer|TestPubSub_ConcurrentSubscribeCancelDuringPublish' .`
Expected: PASS (both subscribers see the message; clean shutdown; race-clean; goleak-clean).

- [ ] **Step 3: Write the runnable example** — `example_pubsub_test.go`

```go
package msgin_test

import (
	"context"
	"fmt"

	"github.com/kartaladev/msgin"
)

// ExamplePubSub shows topic-scoped fan-out: two subscribers on one topic both
// receive a published message; a subscriber on another topic does not.
func ExamplePubSub() {
	ps := msgin.NewPubSub()

	_, _ = ps.Subscribe("orders", msgin.HandlerFunc(func(_ context.Context, m msgin.Message[any]) error {
		fmt.Println("audit:", m.Payload())
		return nil
	}))
	_, _ = ps.Subscribe("orders", msgin.HandlerFunc(func(_ context.Context, m msgin.Message[any]) error {
		fmt.Println("ship:", m.Payload())
		return nil
	}))
	_, _ = ps.Subscribe("invoices", msgin.HandlerFunc(func(_ context.Context, m msgin.Message[any]) error {
		fmt.Println("invoice:", m.Payload())
		return nil
	}))

	_ = ps.Publish(context.Background(), "orders", msgin.New[any]("o-1"))
	// Output:
	// audit: o-1
	// ship: o-1
}
```

- [ ] **Step 4: Append the package-doc paragraph** — add to the existing `doc_composition.go` doc comment (above `package msgin`), after the composition paragraph:

```go
// Publish-Subscribe (Spec 004 / ADR 0014). Beyond point-to-point channels, a
// PublishSubscribeChannel fans a message out to every subscriber; Subscribe
// returns a Subscription whose Cancel unsubscribes. A PubSub registry maps a
// topic name to such a channel (created on first Subscribe, dropped when empty)
// and satisfies the TopicPublisher/TopicSubscriber SPI that native-topic broker
// adapters implement. Dispatch is synchronous (no goroutine); the default
// settlement is all-subscribers-succeed (a subscriber error is joined and the
// message retried), with WithFanOut(FanOutBestEffort) to log-and-continue.
```

- [ ] **Step 5: Run the whole-package suite + gate checks**

```bash
GOTOOLCHAIN=go1.25.12 go test ./... -race
GOTOOLCHAIN=go1.25.12 go vet ./...
gofmt -l .
GOTOOLCHAIN=go1.25.12 go test -coverprofile=/tmp/cover9.out . && GOTOOLCHAIN=go1.25.12 go tool cover -func=/tmp/cover9.out | tail -1
CGO_ENABLED=0 GOTOOLCHAIN=go1.25.12 go build ./...
GOTOOLCHAIN=go1.25.12 go mod tidy && git diff --exit-code go.mod go.sum
```
Expected: `-race` PASS (goleak-clean); `vet`/`gofmt` silent; root coverage ≥85%; `CGO_ENABLED=0` build OK; `go mod tidy` leaves go.mod/go.sum unchanged (no new dep).

- [ ] **Step 6: Commit**

```bash
git add example_pubsub_test.go pubsub_integration_test.go doc_composition.go
git commit -m "test(core): pub-sub end-to-end via NewConsumer + example + package doc

Spec: 004
Plan: 009
ADR: 0014"
```

---

## Whole-branch delivery gate (before merge to main)

Run over `main..HEAD`, resolve/triage every finding, confirm green — per CLAUDE.md §5:

- [ ] `/code-review` on `main..HEAD`; fix or triage findings.
- [ ] `/security-review` on the pending changes (low external surface — no I/O, reflection, or unsafe; the risk is the concurrent subscriber-set mutation, already `-race`-tested).
- [ ] `go test ./... -race` green; `golangci-lint run ./...` clean; `govulncheck ./...` clean; `gofmt`/`gofumpt` silent.
- [ ] Coverage: root package ≥85%; every typed-error/hot-path branch covered — `ErrNilHandler` (channel + registry), all-succeed joined error, best-effort log-and-continue, **permanent-error propagation (F2)**, **per-subscriber panic isolation via `ErrHandlerPanic` (F3)**, no-subscriber no-op, cancel idempotent, drop-on-empty **DROP and KEEP branches (F4)**, **injected-logger set + nil no-op (F8)**, snapshot dispatch + **registry Subscribe-races-last-Cancel TOCTOU (F1)** under concurrent mutation.
- [ ] **F6 (dead-branch note):** do NOT chase coverage on two provably-unreachable defensive branches — `(*PublishSubscribeChannel).remove`'s loop fall-through (the `sync.Once` in `subscription.Cancel` guarantees `remove` runs once while `s` is present) and `PubSub.Subscribe`'s `err != nil` return from `ch.Subscribe` (guarded by the prior nil-`h` check). Leave a one-line comment on each; they are correct-by-construction, not test gaps. `isEmpty` is created in Task 1 but first used in Task 2 — the Task-1 intermediate commit may trip `unused` if linted in isolation (final tree is clean); acceptable.
- [ ] Update `docs/HANDOVER.md`: Phase 3 complete.
- [ ] Confirm with the user before merge/push (never merge/push without explicit approval).

## Adversarial audit round 1 — folded (`.superpowers/sdd/plan-009-audit-round-1.md`)

- **F1 (BLOCKER, fixed):** `PubSub.Subscribe` TOCTOU — now subscribes UNDER `p.mu` (atomic lazy-create+subscribe; lock order `p.mu→ch.mu` preserved, deadlock-free) + a registry-level `TestPubSub_SubscribeRacesLastCancel` regression.
- **F2 (MATERIAL, fixed):** settlement semantics defined — the fan-out settles as ONE unit; a subscriber's permanent error propagates (`errors.Join`) → whole message to the observable invalid sink; per-subscriber independent settlement is out of scope (D7). Documented on the `FanOutAllSucceed` godoc + `TestPublishSubscribeChannel_PermanentErrorPropagates`.
- **F3 (MATERIAL, fixed):** per-subscriber panic recovery — `safeFanOut` wraps each subscriber in a recover → transient `ErrHandlerPanic`, so a panic never aborts the fan-out (best-effort continues; all-succeed retries) + `TestPublishSubscribeChannel_SubscriberPanicIsIsolated`.
- **F4 (MATERIAL, fixed):** `TestPubSub_CancelOneOfSeveralKeepsTopic` covers the drop-on-empty KEEP branch.
- **F5 (MINOR, fixed in spec/ADR):** immutability wording softened to match `Headers`' shallow-copy caveat.
- **F6 (MINOR):** dead defensive branches + `isEmpty` intermediate-lint noted in the gate above.
- **F7 (MINOR, fixed):** `Send` godoc now states cancelled-after-snapshot still receives + Publish-races-last-Cancel → zero-fan-out nil.
- **F8 (MINOR, fixed):** `TestPublishSubscribeChannel_BestEffortLogsToInjectedLogger`.
- **F9 (MINOR, fixed in spec):** O4-2 wording `MessageChannel` declares (not embeds) `Send`; deferrals reconfirmed sound.

## Self-review notes (author)

- **Spec coverage:** D1 (EIP-native topics)→Task 2 registry; D2 (3 layers: SPI+channel+registry)→Tasks 1–2; D3 (Subscription handle)→Task 1; D4 (synchronous, registration order, no goroutine)→Task 1 `Send` + goleak; D5 (all-succeed default / best-effort opt-in / joined error / idempotency)→Task 1 fan-out table; D6 (RWMutex snapshot dispatch, Cancel idempotent, drop-on-empty, immutable share)→Task 1 `Send`/`remove`, Task 2 `topicSubscription.Cancel`, Task 3 concurrency test; D7 (out-of-scope: consumer groups / durable per-sub retry / broker adapters)→documented, not built. Un-defers ADR 0002 §4→delivered.
- **Deferred, documented:** O4-1 `Close()` (YAGNI, goroutine-free); O4-2 Router/Filter widening (`To(psChannel)` already works via `Send`; `pick`-return widening is breaking → own ADR); O4-3 registration order (documented in `Send` godoc); O4-4 split SPI (done — `TopicPublisher`/`TopicSubscriber`).
- **Type consistency:** `FanOutPolicy`/`FanOutAllSucceed`/`FanOutBestEffort`, `Subscription.Cancel`, `PubSubOption`/`WithFanOut`/`WithPubSubLogger`, `PublishSubscribeChannel`/`NewPublishSubscribeChannel`/`Send`/`Subscribe`, `TopicPublisher`/`TopicSubscriber`, `PubSub`/`NewPubSub`/`Publish`/`Subscribe`/`TopicCount`, internal `pubSubConfig`/`withConfig`/`subscription`/`topicSubscription`/`isEmpty`/`remove` — used consistently across tasks. Reuses existing `ErrNilHandler` (no redeclare).
