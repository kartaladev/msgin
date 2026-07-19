# Scheduled / delayed send Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.
>
> **Mandatory Go skills (CLAUDE.md hard rule):** every task starts from **`cc-skills-golang:golang-how-to`** (the always-on orchestrator — it loads the relevant `golang-*` skills: error handling, naming, concurrency, structs/interfaces, testing, lint, security). Follow **TDD** via **`superpowers:test-driven-development`** (red → green → refactor). Use **`gopls`** (the native `LSP` tool, or the `gopls` CLI) for all Go navigation, diagnostics, and refactoring — prefer it over text search when reasoning about symbols. Obey the project-local testing skills, which **override** samber's testing guidance where they conflict: **`table-test`** (assert-closure tables, `ctx`/`t.Context()`), **`use-mockgen`** (uber-go/mock), **`use-testcontainers`** (real external resources via the existing `harness`/`dbtest`, never fakes for the real-DB conformance). These are not optional.

**Goal:** Add a public, durable, typed **scheduled/delayed send** — deliver a message no earlier than `now + delay` (or at a wall-clock time `t`) — over the sql adapter's existing `visible_after` mechanism, with no DDL or dialect-SQL change.

**Architecture:** A new optional core capability `ScheduledSender { SendAfter(ctx, msg, delay) error }` (discovered by type-assertion, mirroring `NativeReliability`/`LockDialect`). `sql.Outbound` implements it by threading `delay` into the existing `dialect.Insert`; `Outbound.Send` becomes `SendAfter(...,0)`. The typed `Producer[T]` interface widens with `SendAfter(delay)` (skew-free primitive) and `SendAt(t)` (core-side sugar over an injected `clockwork` clock); an unsupported sink returns `ErrScheduledSendUnsupported`. Purely additive; memory fails loud (sql-only this increment).

**Tech Stack:** Go 1.25, stdlib + the already-approved `github.com/jonboulle/clockwork` (no new dependency). Tests: blackbox `_test` packages, `stretchr/testify`, `goleak` (root `TestMain`), and the existing sql `harness`/`dbtest` real-DB conformance.

**Traceability:** Implements [Spec 005](../specs/005-scheduled-send.md); realizes [ADR 0015](../adrs/0015-scheduled-send.md). Builds on [ADR 0010](../adrs/0010-poller-sql-adapter.md) (`visible_after`/`LeaseDialect.Insert(delay)`) and [ADR 0002](../adrs/0002-adapter-spi.md) (optional-capability SPI). Commits carry `Spec: 005` / `Plan: 010` / `ADR: 0015` trailers. **Task 1's commit couples Plan 010 + Spec 005 + ADR 0015 with the first code.**

## Global Constraints

- **Go skills (mandatory, CLAUDE.md)** — start every task from `cc-skills-golang:golang-how-to`; TDD via `superpowers:test-driven-development`; navigate/refactor with `gopls`; obey `table-test` / `use-mockgen` / `use-testcontainers`. See the header note.
- **Go 1.25** — `GOTOOLCHAIN=go1.25.12`; no language/stdlib features newer than 1.25.
- **No new dependency** — only stdlib + the already-approved `clockwork`; `go mod tidy` must leave every module's `go.mod`/`go.sum` unchanged. **No DDL, dialect-SQL, or framing change.**
- **Root `package msgin`** for the core capability + producer; the sql impl is in `package sql` (`adapter/database/sql`), the real-DB conformance in `package harness`.
- **Reuse existing symbols** — do NOT redeclare: `OutboundAdapter`, `Message[any]`, `Producer[T]`, `NewProducer`, `producerConfig`/`producer`, `resolveCodec`, `ErrNilAdapter` (core); `Outbound`, `NewOutboundAdapter`, `LeaseDialect`, `EncodeHeaders`, `ErrInvalidPayload`, `resolveQuerier`, `classifyQueryErr` (sql); `fakeDialect`/`newFakeDialect`/`onlyRow`/`openDB`/`fakeDriverName` (sql tests); `RunOutbound`/`TestKit`/`rowCount`/`freshTable` (harness).
- **Blackbox tests only** — every `_test.go` is `package <pkg>_test`. Assert-closure tables (`assert func(t, …)`, never `want`/`wantErr`); `t.Context()`; ≥2 same-call cases ⇒ a table.
- **No panic on caller input** — an unsupported sink returns `ErrScheduledSendUnsupported`; a negative/past delay clamps to 0 (deliver now), never a panic or error.
- **No goroutine leaks** — this feature starts **no goroutine** (the delay lives in the DB row); `goleak` (root `TestMain`) stays clean.
- **Backward compatible** — `Send`/`NewProducer` behavior unchanged; absent any scheduling call every path behaves as today. The `Send`→`SendAfter(...,0)` refactor is behavior-preserving.
- **Every exported symbol has a godoc comment**; the skew caveat (D8) and clamp (D7) documented on `SendAt`/`SendAfter`.
- **Gate before the final increment commit** — `go test ./... -race` green (all modules via `go.work`), `go vet`/`gofmt`/`golangci-lint`/`govulncheck` clean, `CGO_ENABLED=0 go build ./...` succeeds, coverage ≥85% on every changed package with every hot-path/typed-error branch covered.

---

### Task 1: Core `ScheduledSender` capability + producer `SendAfter`/`SendAt` + `ErrScheduledSendUnsupported`

**Files:**
- Modify: `spi.go` (add `ScheduledSender` interface)
- Modify: `errors.go` (add `ErrScheduledSendUnsupported`)
- Modify: `producer.go` (widen `Producer[T]`; add clock to config/struct; `WithProducerClock`; `SendAfter`/`SendAt`; extract a `box` helper; default clock in `NewProducer`)
- Test: `producer_scheduled_test.go` (blackbox `package msgin_test`)

**Interfaces:**
- Consumes: `OutboundAdapter` (`spi.go`), `Message[T]`/`Message[any]` + `New` (message), `PayloadCodec`, `resolveCodec`, `ErrNilAdapter`, `clockwork` (already a dep).
- Produces:
  - `type ScheduledSender interface { SendAfter(ctx context.Context, msg Message[any], delay time.Duration) error }`
  - `var ErrScheduledSendUnsupported error`
  - `func WithProducerClock[T any](c clockwork.Clock) ProducerOption[T]`
  - `Producer[T]` gains `SendAfter(ctx, msg Message[T], delay time.Duration) error` and `SendAt(ctx, msg Message[T], t time.Time) error`.
  - unexported: `(*producer[T]).box`, `producer.clock`, `producerConfig.clock`.

- [ ] **Step 1: Write the failing test** — `producer_scheduled_test.go`

```go
package msgin_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jonboulle/clockwork"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kartaladev/msgin"
)

// schedSink is a fake OutboundAdapter that ALSO implements ScheduledSender,
// recording the last delay and payload it received.
type schedSink struct {
	lastDelay   time.Duration
	lastPayload any
	sendErr     error
	afterErr    error
}

func (s *schedSink) Send(_ context.Context, m msgin.Message[any]) error {
	s.lastPayload = m.Payload()
	return s.sendErr
}

func (s *schedSink) SendAfter(_ context.Context, m msgin.Message[any], d time.Duration) error {
	s.lastDelay = d
	s.lastPayload = m.Payload()
	return s.afterErr
}

// plainSink implements only OutboundAdapter (no ScheduledSender).
type plainSink struct{}

func (plainSink) Send(context.Context, msgin.Message[any]) error { return nil }

func TestProducer_SendAfter(t *testing.T) {
	epoch := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name   string
		run    func(t *testing.T, p msgin.Producer[string], sink *schedSink) error
		assert func(t *testing.T, err error, sink *schedSink)
	}{
		{
			name: "forwards the exact delay and encodes the payload",
			run: func(t *testing.T, p msgin.Producer[string], _ *schedSink) error {
				return p.SendAfter(t.Context(), msgin.New("hi"), 30*time.Minute)
			},
			assert: func(t *testing.T, err error, sink *schedSink) {
				require.NoError(t, err)
				assert.Equal(t, 30*time.Minute, sink.lastDelay)
				assert.Equal(t, []byte(`"hi"`), sink.lastPayload) // JSON-encoded
			},
		},
		{
			name: "negative delay clamps to 0 (deliver now)",
			run: func(t *testing.T, p msgin.Producer[string], _ *schedSink) error {
				return p.SendAfter(t.Context(), msgin.New("hi"), -5*time.Second)
			},
			assert: func(t *testing.T, err error, sink *schedSink) {
				require.NoError(t, err)
				assert.Equal(t, time.Duration(0), sink.lastDelay)
			},
		},
		{
			name: "SendAt computes delay = t - now via the injected clock",
			run: func(t *testing.T, p msgin.Producer[string], _ *schedSink) error {
				return p.SendAt(t.Context(), msgin.New("hi"), epoch.Add(2*time.Hour))
			},
			assert: func(t *testing.T, err error, sink *schedSink) {
				require.NoError(t, err)
				assert.Equal(t, 2*time.Hour, sink.lastDelay)
			},
		},
		{
			name: "SendAt with a past time clamps to 0",
			run: func(t *testing.T, p msgin.Producer[string], _ *schedSink) error {
				return p.SendAt(t.Context(), msgin.New("hi"), epoch.Add(-time.Hour))
			},
			assert: func(t *testing.T, err error, sink *schedSink) {
				require.NoError(t, err)
				assert.Equal(t, time.Duration(0), sink.lastDelay)
			},
		},
		{
			name: "the sink's SendAfter error is propagated",
			run: func(t *testing.T, p msgin.Producer[string], sink *schedSink) error {
				sink.afterErr = errors.New("insert boom")
				return p.SendAfter(t.Context(), msgin.New("hi"), time.Minute)
			},
			assert: func(t *testing.T, err error, _ *schedSink) {
				assert.ErrorContains(t, err, "insert boom")
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sink := &schedSink{}
			p, err := msgin.NewProducer[string](sink, msgin.WithProducerClock[string](clockwork.NewFakeClockAt(epoch)))
			require.NoError(t, err)
			tc.assert(t, tc.run(t, p, sink), sink)
		})
	}
}

func TestProducer_ScheduledSendUnsupported(t *testing.T) {
	p, err := msgin.NewProducer[string](plainSink{})
	require.NoError(t, err)
	t.Run("SendAfter on a non-ScheduledSender sink", func(t *testing.T) {
		assert.ErrorIs(t, p.SendAfter(t.Context(), msgin.New("x"), time.Minute), msgin.ErrScheduledSendUnsupported)
	})
	t.Run("SendAt on a non-ScheduledSender sink", func(t *testing.T) {
		assert.ErrorIs(t, p.SendAt(t.Context(), msgin.New("x"), time.Now().Add(time.Hour)), msgin.ErrScheduledSendUnsupported)
	})
}

// TestProducer_SendAfter_EncodeErrorPropagates covers SendAfter's OWN
// encode-error forwarding branch (`boxed, err := p.box(msg); if err != nil`),
// distinct from Send's — audit round-2 MINOR. Reuses the existing failingCodec /
// errEncode declared in producer_test.go (same msgin_test package). The sink is
// a *schedSink (implements ScheduledSender) so the type-assert passes and control
// reaches box(); the failing codec then makes box return a wrapped encode error.
func TestProducer_SendAfter_EncodeErrorPropagates(t *testing.T) {
	p, err := msgin.NewProducer[string](&schedSink{}, msgin.WithProducerCodec[string](failingCodec{}))
	require.NoError(t, err)
	assert.ErrorIs(t, p.SendAfter(t.Context(), msgin.New("x"), time.Minute), errEncode)
}
```

> Note: `failingCodec` (whose `Encode` returns the sentinel `errEncode`) already exists in `producer_test.go` in the same `package msgin_test`; reuse it — do NOT redeclare it. Verify the exact names with `gopls`/grep before running (they are the doubles the existing "wire adapter encode failure" case uses).

- [ ] **Step 2: Run test to verify it fails** — `GOTOOLCHAIN=go1.25.12 go test -run 'TestProducer_SendAfter|TestProducer_ScheduledSendUnsupported' .` → FAIL (undefined `WithProducerClock`, `SendAfter`, `ErrScheduledSendUnsupported`).

- [ ] **Step 3a: Implement — `spi.go`** (add near the other optional capabilities; `context` and `time` are already imported there):

```go
// ScheduledSender is an optional capability of an OutboundAdapter: it delivers a
// message so that it becomes consumable only after a delay elapses (durable
// delayed send). An adapter that can defer delivery — e.g. the database/sql
// adapter via its visible_after column — implements it; the producer discovers
// it by type-assertion and returns ErrScheduledSendUnsupported when the sink
// does not.
//
// The capability carries the RELATIVE primitive only: the delivery time is
// computed by the adapter's own store as now+delay, so it is free of app-vs-store
// clock skew. A delay <= 0 delivers immediately (equivalent to Send). Absolute-time
// scheduling is a producer-side concern (Producer.SendAt), never pushed into the
// adapter.
type ScheduledSender interface {
	SendAfter(ctx context.Context, msg Message[any], delay time.Duration) error
}
```

- [ ] **Step 3b: Implement — `errors.go`** (add with the other core sentinels; `errors` already imported):

```go
// ErrScheduledSendUnsupported is returned by Producer.SendAfter/SendAt when the
// underlying OutboundAdapter does not implement ScheduledSender (it cannot defer
// delivery). It is never a silent immediate send — the caller is told the sink
// cannot schedule. It is errors.Is-able.
var ErrScheduledSendUnsupported = errors.New("msgin: outbound adapter does not support scheduled send")
```

- [ ] **Step 3c: Implement — `producer.go`** (imports become `context`, `fmt`, `time`, `github.com/jonboulle/clockwork`):

Widen the interface:

```go
// Producer sends typed messages into a flow.
type Producer[T any] interface {
	Send(ctx context.Context, msg Message[T]) error
	// SendAfter delivers msg so it becomes consumable only after delay elapses,
	// when the underlying adapter supports scheduled delivery (ScheduledSender);
	// otherwise it returns ErrScheduledSendUnsupported. A delay <= 0 delivers
	// immediately (identical to Send) — a negative delay is normalized to 0. The
	// delay is relative and skew-free: the adapter's store computes visibility as
	// now+delay.
	SendAfter(ctx context.Context, msg Message[T], delay time.Duration) error
	// SendAt delivers msg so it becomes consumable no earlier than the wall-clock
	// time t, when the adapter supports scheduled delivery; otherwise it returns
	// ErrScheduledSendUnsupported. It is sugar over SendAfter, computing
	// delay = t - now with the producer's clock; a t already in the past delivers
	// immediately. Because the delay is realized on the adapter's (e.g. DB) clock,
	// the absolute target inherits any app-vs-store clock skew — use SendAfter when
	// the delay must be exact.
	SendAt(ctx context.Context, msg Message[T], t time.Time) error
}
```

Add the clock to config + option + struct:

```go
type producerConfig[T any] struct {
	codec    PayloadCodec[T]
	codecSet bool
	clock    clockwork.Clock
}

// WithProducerClock injects the clock Producer.SendAt uses to convert an absolute
// delivery time into a relative delay. Defaults to a real clock; a nil clock is
// ignored (keeps the default). Named distinctly from the message-level WithClock
// to avoid collision (cf. WithConsumerClock, ADR 0007 D10).
func WithProducerClock[T any](c clockwork.Clock) ProducerOption[T] {
	return func(o *producerConfig[T]) {
		if c != nil {
			o.clock = c
		}
	}
}

type producer[T any] struct {
	out       OutboundAdapter
	codec     PayloadCodec[T]
	liveValue bool
	clock     clockwork.Clock
}
```

Default the clock in the constructor:

```go
// NewProducer builds a Producer, validating codec pairing at construction.
func NewProducer[T any](out OutboundAdapter, opts ...ProducerOption[T]) (Producer[T], error) {
	if out == nil {
		return nil, ErrNilAdapter
	}
	cfg := producerConfig[T]{clock: clockwork.NewRealClock()}
	for _, opt := range opts {
		opt(&cfg)
	}
	codec, live, err := resolveCodec[T](out, cfg.codec, cfg.codecSet)
	if err != nil {
		return nil, err
	}
	return &producer[T]{out: out, codec: codec, liveValue: live, clock: cfg.clock}, nil
}
```

Extract the boxing shared by `Send`/`SendAfter`, then implement all three methods:

```go
// box lifts msg from Message[T] to Message[any]: live-value adapters keep the
// payload unencoded, wire adapters get it encoded to []byte via the codec.
func (p *producer[T]) box(msg Message[T]) (Message[any], error) {
	if p.liveValue {
		return Message[any]{payload: msg.payload, headers: msg.headers}, nil
	}
	b, err := p.codec.Encode(msg.payload)
	if err != nil {
		return Message[any]{}, fmt.Errorf("msgin: producer encode failed: %w", err)
	}
	return Message[any]{payload: any(b), headers: msg.headers}, nil
}

// Send writes msg to the outbound adapter for immediate delivery.
func (p *producer[T]) Send(ctx context.Context, msg Message[T]) error {
	boxed, err := p.box(msg)
	if err != nil {
		return err
	}
	return p.out.Send(ctx, boxed)
}

// SendAfter writes msg for delivery after delay, if the sink supports scheduling.
func (p *producer[T]) SendAfter(ctx context.Context, msg Message[T], delay time.Duration) error {
	sched, ok := p.out.(ScheduledSender)
	if !ok {
		return ErrScheduledSendUnsupported
	}
	if delay < 0 {
		delay = 0
	}
	boxed, err := p.box(msg)
	if err != nil {
		return err
	}
	return sched.SendAfter(ctx, boxed, delay)
}

// SendAt writes msg for delivery no earlier than t (sugar over SendAfter).
func (p *producer[T]) SendAt(ctx context.Context, msg Message[T], t time.Time) error {
	return p.SendAfter(ctx, msg, t.Sub(p.clock.Now()))
}
```

- [ ] **Step 4: Run tests** — `GOTOOLCHAIN=go1.25.12 go test -race -run 'TestProducer_SendAfter|TestProducer_ScheduledSendUnsupported' .` → PASS. Then the whole root package: `GOTOOLCHAIN=go1.25.12 go test -race .` (the `Send`-path refactor must not regress existing producer/consumer tests). Run `gofmt -l producer.go spi.go errors.go` (silent) and `gopls check producer.go` (no diagnostics).

- [ ] **Step 5: Commit** (couples Plan 010 + Spec 005 + ADR 0015 with the first code)

```bash
git add spi.go errors.go producer.go producer_scheduled_test.go \
        docs/plans/010-scheduled-send.md docs/specs/005-scheduled-send.md docs/adrs/0015-scheduled-send.md
git commit -m "feat(core): add ScheduledSender capability + producer SendAfter/SendAt

Optional OutboundAdapter capability ScheduledSender{SendAfter(delay)} for
durable delayed send; Producer[T] gains SendAfter (skew-free relative
primitive) and SendAt (absolute sugar over an injected clockwork clock,
WithProducerClock). An unsupported sink returns ErrScheduledSendUnsupported;
a negative/past delay clamps to 0. Purely additive; stdlib+clockwork only.

Spec: 005
Plan: 010
ADR: 0015"
```

---

### Task 2: `sql.Outbound` implements `ScheduledSender`

**Files:**
- Modify: `adapter/database/sql/outbound.go` (add `SendAfter`; refactor `Send` to delegate; add the compile-time capability assertion)
- Test: `adapter/database/sql/outbound_unit_test.go` (add a delay-threading table against `fakeDialect`)

**Interfaces:**
- Consumes: `msgin.ScheduledSender` (Task 1), `msgin.Message[any]`, `LeaseDialect.Insert(..., delay)`, `EncodeHeaders`, `ErrInvalidPayload`, `resolveQuerier`, `classifyQueryErr`, `fakeDialect` (records `visibleAfter = now.Add(delay)`; injectable `now`; `onlyRow`), `openDB`, `fakeDriverName`.
- Produces: `(*Outbound).SendAfter(ctx, msg, delay) error`; `Send` delegates to it; `var _ msgin.ScheduledSender = (*Outbound)(nil)`.

- [ ] **Step 1: Write the failing test** — add to `adapter/database/sql/outbound_unit_test.go`

```go
// TestOutbound_SendAfter_ThreadsDelay covers the ScheduledSender capability:
// SendAfter sets visible_after = db-now + delay (recorded by fakeDialect), and
// Send is exactly SendAfter with delay 0. now() is fixed for a deterministic
// visible_after assertion.
func TestOutbound_SendAfter_ThreadsDelay(t *testing.T) {
	t.Parallel()
	epoch := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	type testCase struct {
		name   string
		send   func(ctx context.Context, out *msginsql.Outbound, msg msgin.Message[any]) error
		assert func(t *testing.T, visibleAfter time.Time)
	}
	cases := []testCase{
		{
			name: "Send inserts an immediately visible row (delay 0)",
			send: func(ctx context.Context, out *msginsql.Outbound, msg msgin.Message[any]) error {
				return out.Send(ctx, msg)
			},
			assert: func(t *testing.T, va time.Time) { assert.Equal(t, epoch, va) },
		},
		{
			name: "SendAfter sets visible_after = now + delay",
			send: func(ctx context.Context, out *msginsql.Outbound, msg msgin.Message[any]) error {
				return out.SendAfter(ctx, msg, 30*time.Minute)
			},
			assert: func(t *testing.T, va time.Time) { assert.Equal(t, epoch.Add(30*time.Minute), va) },
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fd := newFakeDialect()
			fd.now = func() time.Time { return epoch }
			out, err := msginsql.NewOutboundAdapter(openDB(t, fakeDriverName), "msgs", fd)
			require.NoError(t, err)

			msg := msgin.New[any]([]byte(`{"k":"v"}`), msgin.WithID("s-1"))
			require.NoError(t, tc.send(t.Context(), out, msg))
			tc.assert(t, fd.onlyRow(t).visibleAfter)
		})
	}
}

// TestOutbound_IsScheduledSender documents the capability at the type level.
func TestOutbound_IsScheduledSender(t *testing.T) {
	t.Parallel()
	var _ msgin.ScheduledSender = (*msginsql.Outbound)(nil)
}
```

(Ensure `time` and `context` are imported in `outbound_unit_test.go`; add them if the file does not already import them.)

- [ ] **Step 2: Run test to verify it fails** — `GOTOOLCHAIN=go1.25.12 go test -run 'TestOutbound_SendAfter_ThreadsDelay|TestOutbound_IsScheduledSender' ./adapter/database/sql/` → FAIL (`out.SendAfter` undefined; `*Outbound` does not implement `msgin.ScheduledSender`).

- [ ] **Step 3: Implement** — `adapter/database/sql/outbound.go`

Widen the compile-time assertion block (near the top, where `var _ msgin.OutboundAdapter = (*Outbound)(nil)` is):

```go
var (
	_ msgin.OutboundAdapter = (*Outbound)(nil)
	_ msgin.ScheduledSender = (*Outbound)(nil)
)
```

Replace the existing `Send` with a delegation, and move its body into `SendAfter` (threading `delay` instead of the literal `0`). Ensure `time` is imported.

```go
// Send writes msg for immediate delivery. It is exactly SendAfter(ctx, msg, 0).
func (o *Outbound) Send(ctx context.Context, msg msgin.Message[any]) error {
	return o.SendAfter(ctx, msg, 0)
}

// SendAfter writes msg so it becomes claimable only after delay elapses:
// visible_after = <db-now> + delay, computed on the DB server clock (skew-free).
// A delay <= 0 makes the row immediately visible. Implements msgin.ScheduledSender.
func (o *Outbound) SendAfter(ctx context.Context, msg msgin.Message[any], delay time.Duration) error {
	msgID := msg.ID()
	headers, err := EncodeHeaders(msg.Headers())
	if err != nil {
		return err
	}
	payload, ok := msg.Payload().([]byte)
	if !ok {
		return fmt.Errorf("%w: got %T", ErrInvalidPayload, msg.Payload())
	}
	q, err := o.resolveQuerier(ctx, msgID)
	if err != nil {
		return err
	}
	if err := o.dialect.Insert(ctx, q, o.table, msgID, headers, payload, delay); err != nil {
		return o.classifyQueryErr(ctx, err)
	}
	return nil
}
```

- [ ] **Step 4: Run tests** — `GOTOOLCHAIN=go1.25.12 go test -race ./adapter/database/sql/` → PASS (the new tests + the existing `TestOutbound_SendFramesPayload`/`TestOutbound_ResolveQuerier` still green, proving the `Send`→`SendAfter(...,0)` refactor is behavior-preserving). `gofmt -l adapter/database/sql/outbound.go` silent; `gopls check adapter/database/sql/outbound.go` clean.

- [ ] **Step 5: Commit**

```bash
git add adapter/database/sql/outbound.go adapter/database/sql/outbound_unit_test.go
git commit -m "feat(sql): Outbound implements ScheduledSender (delayed send)

Outbound.SendAfter threads a caller delay into the existing
dialect.Insert (visible_after = db-now + delay); Send now delegates to
SendAfter(...,0) (behavior-preserving). No DDL, dialect-SQL, or framing
change — the visible_after machinery already existed.

Spec: 005
Plan: 010
ADR: 0015"
```

---

### Task 3: Real-DB delayed-visibility conformance + example + package doc + whole-branch gate

Proves the headline claim on real engines — a `SendAfter`-inserted row is invisible before its `visible_after` and claimable after — across PostgreSQL, MySQL, and SQLite, by adding one subtest to the shared `harness.RunOutbound` (already invoked by all three `dbtest` conformance suites, so it rides along with no dbtest change).

**Files:**
- Modify: `adapter/database/sql/harness/outbound.go` (add a `ScheduledSendDelaysVisibility` subtest inside `RunOutbound`)
- Create: `example_scheduled_test.go` (root, runnable `Example` — doubles as godoc)
- Modify: `doc_composition.go` (append a scheduled-send paragraph to the existing `package msgin` doc)

**Interfaces:**
- Consumes: all Task 1–2 symbols + `harness.TestKit`/`rowCount`/`freshTable` (via the existing `fresh` closure in `RunOutbound`), `msginsql.NewOutboundAdapter`/`NewPollingSource`, `msgin.NewProducer`, `PollingSource.Poll(ctx, max)`, `Delivery.Ack`, `adapter/memory`.

- [ ] **Step 1: Add the real-DB conformance subtest** — inside `RunOutbound` in `adapter/database/sql/harness/outbound.go`, after the existing `t.Run("RoundTripProduceThenConsume", …)` block (reuse its `fresh`/`rowCount` helpers):

```go
	t.Run("ScheduledSendDelaysVisibility", func(t *testing.T) {
		ctx := t.Context()
		table := fresh(ctx)

		out, err := msginsql.NewOutboundAdapter(db, table, kit.Lease)
		require.NoError(t, err)
		p, err := msgin.NewProducer[string](out)
		require.NoError(t, err)
		src, err := msginsql.NewPollingSource(db, table, kit.Lease)
		require.NoError(t, err)

		// (a) A far-future delayed row is persisted but NOT claimable. Using a 1h
		// delay makes the invisibility assertion race-free (no plausible test/CI
		// stall approaches 1h between INSERT and the first Claim — F1), unlike a
		// short delay whose window a slow container could exceed.
		require.NoError(t, p.SendAfter(ctx, msgin.New("far"), 1*time.Hour))
		require.Equal(t, 1, rowCount(t, ctx, kit, db, table), "the row is persisted immediately")
		got, err := src.Poll(ctx, 10)
		require.NoError(t, err)
		require.Empty(t, got, "a far-future row must not be claimable before its visible_after")

		// (b) A short-delay row becomes claimable after its delay elapses; the
		// far-future row stays invisible. Eventually waits real time for (b) only.
		require.NoError(t, p.SendAfter(ctx, msgin.New("soon", msgin.WithID("soon")), 500*time.Millisecond))
		require.Eventually(t, func() bool {
			// No require.* here: testify runs this in a spawned goroutine, where a
			// FailNow/Goexit is unsupported (F3). Return false on any mismatch.
			d, err := src.Poll(ctx, 10)
			if err != nil || len(d) != 1 || d[0].Msg.ID() != "soon" {
				return false
			}
			return d[0].Ack(ctx) == nil
		}, 5*time.Second, 50*time.Millisecond, "the short-delay row becomes claimable after its delay")

		// The far-future row is still present and unclaimed (soon was Acked/deleted).
		require.Equal(t, 1, rowCount(t, ctx, kit, db, table), "the far-future row remains invisible")
	})
```

- [ ] **Step 2: Verify the conformance subtest passes on real engines** — run the `dbtest` suites (Docker required; per `use-testcontainers`, they provision real PG/MySQL and an embedded SQLite):

```
GOTOOLCHAIN=go1.25.12 go test -race -run '/Outbound/ScheduledSendDelaysVisibility' ./adapter/database/sql/dbtest/
```
(The conformance entry points are `TestPostgresConformance`/`TestMySQLConformance`/`TestMariaDBConformance`/`TestSQLiteConformance`; the `/Outbound/ScheduledSendDelaysVisibility` subtest path selects the new case under each — do NOT use a `TestConformance.*` prefix, which matches nothing.)
Expected: PASS on PostgreSQL, MySQL, MariaDB, and SQLite (the subtest is invoked by each engine's `RunOutbound`). If Docker is unavailable in the run environment, note it and rely on Task 2's deterministic `fakeDialect` coverage plus a later CI run; do NOT weaken the assertion.

- [ ] **Step 3: Write the runnable example** — `example_scheduled_test.go` (root, `package msgin_test`)

```go
package msgin_test

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/kartaladev/msgin"
	"github.com/kartaladev/msgin/adapter/memory"
)

// scheduleLogSink is a tiny OutboundAdapter that also implements
// msgin.ScheduledSender, printing the delay it is asked to schedule.
type scheduleLogSink struct{}

func (scheduleLogSink) Send(context.Context, msgin.Message[any]) error {
	fmt.Println("send now")
	return nil
}

func (scheduleLogSink) SendAfter(_ context.Context, _ msgin.Message[any], d time.Duration) error {
	fmt.Printf("send after %s\n", d)
	return nil
}

// ExampleProducer_SendAfter shows a durable delayed send against a
// scheduling-capable adapter, and the fail-loud error when the adapter cannot
// schedule (the in-memory Broker does not implement ScheduledSender).
func ExampleProducer_SendAfter() {
	// A scheduling-capable sink (the real durable one is the database/sql adapter).
	sched, _ := msgin.NewProducer[string](scheduleLogSink{})
	_ = sched.SendAfter(context.Background(), msgin.New("payload"), 2*time.Hour)

	// A non-scheduling sink fails loud rather than delivering immediately.
	mem, _ := msgin.NewProducer[string](memory.New())
	err := mem.SendAfter(context.Background(), msgin.New("payload"), time.Minute)
	fmt.Println(errors.Is(err, msgin.ErrScheduledSendUnsupported))
	// Output:
	// send after 2h0m0s
	// true
}
```

- [ ] **Step 4: Append the package-doc paragraph** — add to the existing `doc_composition.go` doc comment (above `package msgin`), after the Publish-Subscribe paragraph:

```go
// Scheduled / delayed send (Spec 005 / ADR 0015). An OutboundAdapter that can
// defer delivery implements the optional ScheduledSender capability; the sql
// adapter does so via its visible_after column. Producer.SendAfter(delay) is the
// skew-free relative primitive (the store computes now+delay) and SendAt(t) is
// absolute sugar over an injected clock; a sink that cannot schedule returns
// ErrScheduledSendUnsupported (never a silent immediate send). A negative or past
// delay delivers immediately. No goroutine is started — the delay lives in the
// durable row, so a scheduled send survives restarts and fires once across N
// competing consumers.
```

- [ ] **Step 5: Run the whole-package suite + gate checks**

```bash
GOTOOLCHAIN=go1.25.12 go test ./... -race
GOTOOLCHAIN=go1.25.12 go test -run '^ExampleProducer_SendAfter$' .
GOTOOLCHAIN=go1.25.12 go vet ./...
gofmt -l .
GOTOOLCHAIN=go1.25.12 go test -coverprofile=/tmp/cover10.out . && GOTOOLCHAIN=go1.25.12 go tool cover -func=/tmp/cover10.out | tail -1
CGO_ENABLED=0 GOTOOLCHAIN=go1.25.12 go build ./...
GOTOOLCHAIN=go1.25.12 go mod tidy && git diff --exit-code go.mod go.sum
```
Expected: `-race` PASS (goleak-clean); the example's `// Output:` matches; `vet`/`gofmt` silent; root coverage ≥85%; `CGO_ENABLED=0` build OK; `go mod tidy` leaves go.mod/go.sum unchanged (no new dep). Also confirm the harness/dbtest modules tidy-clean if touched (`cd adapter/database/sql/harness && GOTOOLCHAIN=go1.25.12 go mod tidy && git diff --exit-code go.mod go.sum`).

- [ ] **Step 6: Commit**

```bash
git add adapter/database/sql/harness/outbound.go example_scheduled_test.go doc_composition.go
git commit -m "test(sql): real-DB delayed-visibility conformance + example + package doc

Adds a ScheduledSendDelaysVisibility case to harness.RunOutbound (rides
along to PG/MySQL/SQLite via the existing dbtest suites): a SendAfter row
is persisted immediately but not claimable until visible_after, then
claimable exactly once. Adds ExampleProducer_SendAfter and the
scheduled-send package-doc paragraph.

Spec: 005
Plan: 010
ADR: 0015"
```

---

## Whole-branch delivery gate (before merge to main)

Run over `main..HEAD`, resolve/triage every finding, confirm green — per CLAUDE.md §5:

- [ ] `/code-review` on `main..HEAD`; fix or triage findings.
- [ ] `/security-review` on the pending changes (low surface — no new I/O, no untrusted input; the delay is a `time.Duration`, the SQL is unchanged and identifier-validated as before).
- [ ] `go test ./... -race` green (all modules via `go.work`); `golangci-lint run ./...` clean; `govulncheck ./...` clean; `gofmt -l .` silent.
- [ ] Coverage: root ≥85% and every typed-error/hot-path branch covered — `ErrScheduledSendUnsupported` (SendAfter + SendAt), the negative-delay clamp, the SendAt past-clamp, the encode-error propagation, the sink-error propagation, `Send`==`SendAfter(0)` parity, and the real-DB delayed-visibility (before/after) case.
- [ ] Confirm `NewProducer` still returns `Producer[T]` and the widened interface breaks no in-repo caller; regenerate any `mockgen` mock of `Producer[T]` if one exists (`grep -rl "Producer\[" --include=*mock*.go` — none expected).
- [ ] Update `docs/HANDOVER.md`: Plan 010 complete.
- [ ] Confirm with the user before merge/push (never merge/push without explicit approval).

## Self-review notes (author)

- **Spec coverage:** G1 (relative primitive)→Task 1 `SendAfter` + Task 2 sql; G2 (absolute sugar)→Task 1 `SendAt`+`WithProducerClock`; G3 (fail loud)→`ErrScheduledSendUnsupported` (Task 1); G4 (capability pattern)→`ScheduledSender` type-assert (Task 1); G5 (additive/back-compat)→`Send`→`SendAfter(0)` refactor + existing tests (Task 2). D1–D8 all realized; D7 clamp tested (negative + past); D8 skew documented on `SendAt`. N1 (no DDL/SQL) held — only `Insert`'s existing `delay` param is threaded. Real-DB proof (Spec §5)→Task 3 harness case across PG/MySQL/SQLite.
- **Encode-error branches (audit F4 + round-2 MINOR):** `box`'s encode-error path is already exercised by the EXISTING `producer_test.go` `TestProducer_Send` "wire adapter encode failure" case (via `failingCodec`), because `Send` delegates to `box`. Additionally, `SendAfter`'s OWN encode-error *forwarding* branch (`if err != nil { return err }`) is covered by the new `TestProducer_SendAfter_EncodeErrorPropagates` (Task 1 Step 1). Both branches covered; no further test needed.
- **Deferred, documented (Spec O5-1/O5-2/O5-5):** EIP `Delayer` step, memory delayed send, gocron recurring source — none built here.
- **Type consistency:** `ScheduledSender.SendAfter`, `Producer[T].SendAfter`/`SendAt`, `WithProducerClock`, `ErrScheduledSendUnsupported`, `(*Outbound).SendAfter`, `producer.box`/`producer.clock` — used consistently across tasks. Reuses `fakeDialect.now`/`onlyRow`/`visibleAfter`, `RunOutbound`/`rowCount`/`fresh`, `PollingSource.Poll` — all verified against the current tree.
- **SemVer:** widening `Producer[T]` is breaking for external implementers only; acceptable pre-1.0, ADR 0015 Decision 3.
