# Composition Endpoints — Phase 1 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship the in-process composition backbone (`MessageHandler`/`MessageChannel`, synchronous `DirectChannel`) plus the four linear EIP endpoints (Transformer, Filter, Content-Based Router, Service Activator) with typed sugar, so a consumer can compose an in-process message flow that drives off the existing `Consumer` runtime.

**Architecture:** A monomorphic `Message[any]` composition core (`MessageHandler` forwards by sending onward; `MessageChannel` is the conduit). Endpoints are `Step` decorators (`func(next MessageHandler) MessageHandler`) composed by `Chain`; typed generic free-function constructors box into the `any` core. `DirectChannel` runs synchronously on the caller's goroutine, so a flow driven by `NewConsumer[any](src, flow.Handle)` inherits retry/DLQ/invalid/flow-control/worker-pool and its errors propagate into the existing runtime. Stdlib-only; root `package msgin`.

**Tech Stack:** Go 1.25, stdlib only (no new dependency). Tests: blackbox `_test` packages, `stretchr/testify`, `goleak` (already wired via root `TestMain`), `adapter/memory` (live-value source).

**Traceability:** Implements [Spec 003](../specs/003-composition-endpoints.md) Phase 1 (§6.1); realizes [ADR 0013](../adrs/0013-composition-endpoints.md). Commits carry `Spec: 003` / `Plan: 008` / `ADR: 0013` trailers. **Task 1's commit couples [Plan 008] + [ADR 0013] with the code**. ADR 0014 (Phases 2–3) stays uncommitted until Phase 2. **Revised after adversarial audit round 1** (`.superpowers/sdd/plan-008-audit-round-1.md`): F1 reuse existing `ErrPayloadType`; F2 `To(OutboundAdapter)`; F3 wire-vs-live scoping + integration test; F4 nil-func guards; F5 `WithPayload`; F6 `Chain`-terminal contract.

## Global Constraints

- **Go 1.25** — `GOTOOLCHAIN=go1.25.12`; no language/stdlib features newer than 1.25.
- **Stdlib-only core** — no new direct dependency; `go mod tidy` must leave `go.mod`/`go.sum` unchanged.
- **Root `package msgin`** — all new production symbols exported from the root package.
- **Reuse existing sentinels** — `ErrPayloadType` and `ErrPayloadDecode` **already exist** in `errors.go` and are already classified permanent by `isPermanent` (`reliability.go:40`). Do **not** redeclare them.
- **Blackbox tests only** — every `_test.go` is `package msgin_test`, exercising the exported API. Example tests too.
- **Assert-closure tables** — every table case carries an `assert func(t *testing.T, …)` closure (never `want`/`wantErr` fields); `t.Context()` over `context.Background()`; ≥2 cases of the same call ⇒ a table (`table-test` skill).
- **No panic on caller input** — nil channels/handlers/**functions** return typed errors at call time, never panic (F4). Library returns errors, never `os.Exit`/`log.Fatal`/`panic` on caller input.
- **Every exported symbol has a godoc comment**; defaults documented with rationale on the option godoc.
- **Immutability + header propagation** — endpoints return a *new* `Message`; a Transformer/Activator that must preserve correlation uses `WithPayload`/`NewMessage(payload, m.Headers())`, NOT bare `New` (which restamps id and drops headers — F5).
- **Gate before the final increment commit** — `go test ./... -race` green, `go vet`/`gofmt`/`golangci-lint`/`govulncheck` clean, `CGO_ENABLED=0 go build ./...` succeeds, coverage ≥85% on the root package with every hot-path/typed-error branch covered.

---

### Task 1: Composition primitives — `MessageHandler`, `MessageChannel`, `DirectChannel`

**Files:**
- Create: `handler.go` (`MessageHandler`/`HandlerFunc`)
- Create: `channel.go` (`MessageChannel` + `DirectChannel`)
- Modify: `errors.go` (add `ErrChannelSubscribed`, `ErrNoSubscriber`, `ErrNilHandler`)
- Test: `channel_test.go`

**Interfaces:**
- Produces:
  - `type MessageHandler interface { Handle(ctx context.Context, msg Message[any]) error }`
  - `type HandlerFunc func(ctx context.Context, msg Message[any]) error` (method `Handle`)
  - `type MessageChannel interface { Send(ctx context.Context, msg Message[any]) error; Subscribe(h MessageHandler) error }`
  - `type DirectChannel struct{…}`; `func NewDirectChannel() *DirectChannel`; methods `Send`, `Subscribe`
  - sentinels `ErrChannelSubscribed`, `ErrNoSubscriber`, `ErrNilHandler`

- [ ] **Step 1: Write the failing test** — `channel_test.go`

```go
package msgin_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kartaladev/msgin"
)

func TestDirectChannel_SendInvokesSubscriber(t *testing.T) {
	tests := []struct {
		name       string
		handlerErr error
		assert     func(t *testing.T, sendErr error, got []msgin.Message[any])
	}{
		{
			name:       "send invokes the subscribed handler synchronously",
			handlerErr: nil,
			assert: func(t *testing.T, sendErr error, got []msgin.Message[any]) {
				require.NoError(t, sendErr)
				require.Len(t, got, 1)
				assert.Equal(t, "hello", got[0].Payload())
			},
		},
		{
			name:       "send propagates the handler error",
			handlerErr: errors.New("boom"),
			assert: func(t *testing.T, sendErr error, _ []msgin.Message[any]) {
				assert.ErrorContains(t, sendErr, "boom")
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dc := msgin.NewDirectChannel()
			var got []msgin.Message[any]
			require.NoError(t, dc.Subscribe(msgin.HandlerFunc(func(_ context.Context, m msgin.Message[any]) error {
				got = append(got, m)
				return tc.handlerErr
			})))
			tc.assert(t, dc.Send(t.Context(), msgin.New[any]("hello")), got)
		})
	}
}

func TestDirectChannel_Errors(t *testing.T) {
	tests := []struct {
		name   string
		run    func(t *testing.T) error
		assert func(t *testing.T, err error)
	}{
		{
			name: "subscribe twice is ErrChannelSubscribed",
			run: func(t *testing.T) error {
				dc := msgin.NewDirectChannel()
				_ = dc.Subscribe(msgin.HandlerFunc(func(context.Context, msgin.Message[any]) error { return nil }))
				return dc.Subscribe(msgin.HandlerFunc(func(context.Context, msgin.Message[any]) error { return nil }))
			},
			assert: func(t *testing.T, err error) { assert.ErrorIs(t, err, msgin.ErrChannelSubscribed) },
		},
		{
			name:   "subscribe nil handler is ErrNilHandler",
			run:    func(t *testing.T) error { return msgin.NewDirectChannel().Subscribe(nil) },
			assert: func(t *testing.T, err error) { assert.ErrorIs(t, err, msgin.ErrNilHandler) },
		},
		{
			name:   "send with no subscriber is ErrNoSubscriber",
			run:    func(t *testing.T) error { return msgin.NewDirectChannel().Send(t.Context(), msgin.New[any](1)) },
			assert: func(t *testing.T, err error) { assert.ErrorIs(t, err, msgin.ErrNoSubscriber) },
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) { tc.assert(t, tc.run(t)) })
	}
}
```

- [ ] **Step 2: Run test to verify it fails** — `GOTOOLCHAIN=go1.25.12 go test -run 'TestDirectChannel' .` → FAIL (undefined symbols).

- [ ] **Step 3: Implement** — `handler.go`

```go
package msgin

import "context"

// MessageHandler is one processing step in an in-process flow: it consumes a
// message and MAY forward a (possibly transformed) message onward. For a
// DirectChannel it runs synchronously on the caller's goroutine, so an error it
// returns propagates back to the driving Consumer, which owns
// retry/dead-letter/invalid-message. A MessageHandler is structurally a
// Handler[any], so a composed flow drives off NewConsumer[any](src, flow.Handle, …).
type MessageHandler interface {
	Handle(ctx context.Context, msg Message[any]) error
}

// HandlerFunc adapts an ordinary function to a MessageHandler.
type HandlerFunc func(ctx context.Context, msg Message[any]) error

// Handle calls f(ctx, msg).
func (f HandlerFunc) Handle(ctx context.Context, msg Message[any]) error { return f(ctx, msg) }
```

- [ ] **Step 4: Implement** — `channel.go`

```go
package msgin

import (
	"context"
	"sync"
)

// MessageChannel is the conduit endpoints send into and subscribe to. Its Send
// is structurally identical to OutboundAdapter.Send.
type MessageChannel interface {
	Send(ctx context.Context, msg Message[any]) error
	Subscribe(h MessageHandler) error
}

// DirectChannel is a synchronous, point-to-point channel with exactly one
// subscriber: Send invokes the subscribed handler on the caller's goroutine and
// returns its error. It starts no goroutine, and running in the caller's
// settlement scope preserves end-to-end at-least-once when driven by a Consumer.
type DirectChannel struct {
	mu      sync.RWMutex
	handler MessageHandler
}

var _ MessageChannel = (*DirectChannel)(nil)

// NewDirectChannel returns an empty DirectChannel; Subscribe one handler before Send.
func NewDirectChannel() *DirectChannel { return &DirectChannel{} }

// Subscribe registers the single point-to-point handler. A nil handler is
// ErrNilHandler; a second Subscribe is ErrChannelSubscribed.
func (c *DirectChannel) Subscribe(h MessageHandler) error {
	if h == nil {
		return ErrNilHandler
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.handler != nil {
		return ErrChannelSubscribed
	}
	c.handler = h
	return nil
}

// Send invokes the subscribed handler synchronously. With no subscriber it is
// ErrNoSubscriber (never a silent drop).
func (c *DirectChannel) Send(ctx context.Context, msg Message[any]) error {
	c.mu.RLock()
	h := c.handler
	c.mu.RUnlock()
	if h == nil {
		return ErrNoSubscriber
	}
	return h.Handle(ctx, msg)
}
```

- [ ] **Step 5: Add sentinels** — append to the `var (…)` block in `errors.go`

```go
	// ErrChannelSubscribed is returned by a point-to-point channel's Subscribe
	// when a handler is already registered (single-consumer invariant).
	ErrChannelSubscribed = errors.New("msgin: channel already has a subscriber")
	// ErrNoSubscriber is returned by a point-to-point channel's Send when no
	// handler is subscribed — a message is never silently dropped.
	ErrNoSubscriber = errors.New("msgin: channel has no subscriber")
	// ErrNilHandler is returned when a nil MessageHandler is subscribed.
	ErrNilHandler = errors.New("msgin: nil message handler")
```

- [ ] **Step 6: Run tests** — `GOTOOLCHAIN=go1.25.12 go test -race -run 'TestDirectChannel' .` → PASS.

- [ ] **Step 7: Commit** (couples Plan 008 + ADR 0013 with the first code)

```bash
git add handler.go channel.go errors.go channel_test.go docs/plans/008-composition-endpoints-phase1.md docs/adrs/0013-composition-endpoints.md
git commit -m "feat(core): add MessageHandler/MessageChannel + DirectChannel

Composition primitives for in-process EIP flows: MessageHandler (a
processing step, structurally Handler[any]), MessageChannel (conduit,
Send == OutboundAdapter.Send), and the synchronous single-subscriber
DirectChannel. Stdlib-only; no goroutine started.

Spec: 003
Plan: 008
ADR: 0013"
```

---

### Task 2: Typed payload sugar — `PayloadOf[T]`, `WithPayload[A,B]` (reuse existing `ErrPayloadType`)

**Files:**
- Create: `payload.go`
- Test: `payload_test.go`

**Interfaces:**
- Consumes: existing `ErrPayloadType` (`errors.go:7`) — do NOT redeclare (F1).
- Produces:
  - `func PayloadOf[T any](m Message[any]) (Message[T], error)` — mismatch wraps existing `ErrPayloadType`.
  - `func WithPayload[A, B any](m Message[A], payload B) Message[B]` — new payload, SAME headers (preserves id/correlation — F5).
  - internal `func boxMessage[T any](m Message[T]) Message[any]` (unexported), used by later tasks.

- [ ] **Step 1: Write the failing test** — `payload_test.go`

```go
package msgin_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kartaladev/msgin"
)

func TestPayloadOf(t *testing.T) {
	tests := []struct {
		name   string
		run    func(t *testing.T) (any, error)
		assert func(t *testing.T, payload any, err error)
	}{
		{
			name: "matching type unboxes and preserves headers",
			run: func(t *testing.T) (any, error) {
				return msgin.PayloadOf[string](msgin.New[any]("hi", msgin.WithID("id-1")))
			},
			assert: func(t *testing.T, payload any, err error) {
				require.NoError(t, err)
				tm := payload.(msgin.Message[string])
				assert.Equal(t, "hi", tm.Payload())
				assert.Equal(t, "id-1", tm.ID())
			},
		},
		{
			name: "mismatched type wraps the existing ErrPayloadType",
			run: func(t *testing.T) (any, error) {
				return msgin.PayloadOf[int](msgin.New[any]("not-an-int"))
			},
			assert: func(t *testing.T, _ any, err error) {
				assert.ErrorIs(t, err, msgin.ErrPayloadType)
				assert.ErrorContains(t, err, "int")
			},
		},
		{
			name: "any target always succeeds",
			run:  func(t *testing.T) (any, error) { return msgin.PayloadOf[any](msgin.New[any](42)) },
			assert: func(t *testing.T, payload any, err error) {
				require.NoError(t, err)
				assert.Equal(t, 42, payload.(msgin.Message[any]).Payload())
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p, err := tc.run(t)
			tc.assert(t, p, err)
		})
	}
}

func TestWithPayload_PreservesHeaders(t *testing.T) {
	in, _ := msgin.PayloadOf[string](msgin.New[any]("x", msgin.WithID("id-9"), msgin.WithHeaders(map[string]any{"k": "v"})))
	out := msgin.WithPayload(in, 100) // string -> int
	assert.Equal(t, 100, out.Payload())
	assert.Equal(t, "id-9", out.ID()) // id + custom headers survive the transform
	v, ok := out.Headers().String("k")
	require.True(t, ok)
	assert.Equal(t, "v", v)
}
```

- [ ] **Step 2: Run test to verify it fails** — `GOTOOLCHAIN=go1.25.12 go test -run 'TestPayloadOf|TestWithPayload' .` → FAIL.

- [ ] **Step 3: Implement** — `payload.go`

```go
package msgin

import "fmt"

// PayloadOf asserts m's payload to T, returning a typed Message[T] with the same
// headers (no re-stamp). On mismatch it wraps the package sentinel ErrPayloadType
// with the wanted/actual types; because isPermanent classifies ErrPayloadType as
// permanent, the driving Consumer routes it to the invalid-message channel (never
// a panic). PayloadOf[any] always succeeds.
func PayloadOf[T any](m Message[any]) (Message[T], error) {
	v, ok := m.Payload().(T)
	if !ok {
		return Message[T]{}, fmt.Errorf("%w: want %T, got %T", ErrPayloadType, *new(T), m.Payload())
	}
	return NewMessage[T](v, m.Headers()), nil
}

// WithPayload returns a new Message carrying payload but the SAME headers as m
// (id, timestamp, correlation-id, custom keys preserved). It is the
// header-propagating way to write a Transformer/Activator body — prefer it over
// New, which stamps a fresh id and drops the incoming headers.
func WithPayload[A, B any](m Message[A], payload B) Message[B] {
	return NewMessage[B](payload, m.Headers())
}

// boxMessage lifts a typed Message[T] into Message[any], preserving headers
// verbatim. Inverse of PayloadOf; backs the typed endpoint constructors.
func boxMessage[T any](m Message[T]) Message[any] {
	return NewMessage[any](m.Payload(), m.Headers())
}
```

- [ ] **Step 4: Run tests** — `GOTOOLCHAIN=go1.25.12 go test -race -run 'TestPayloadOf|TestWithPayload' .` → PASS.

- [ ] **Step 5: Commit**

```bash
git add payload.go payload_test.go
git commit -m "feat(core): add PayloadOf[T] + WithPayload[A,B] (reuse existing ErrPayloadType)

Spec: 003
Plan: 008
ADR: 0013"
```

---

### Task 3: Linear composition — `Step`, `Chain`, `To`

**Files:**
- Modify: `handler.go` (add `Step`, `Chain`, `To`, internal `discardHandler`)
- Modify: `errors.go` (add `ErrNilSink`)
- Test: `handler_test.go`

**Interfaces:**
- Consumes: `MessageHandler`, `HandlerFunc`, `OutboundAdapter` (existing, `spi.go`).
- Produces:
  - `type Step = func(next MessageHandler) MessageHandler`
  - `func Chain(steps ...Step) MessageHandler` — folds right-to-left; **a flow that reaches the end without a `To`/`Consume` terminal DISCARDS its final message (documented contract, F6).**
  - `func To(sink OutboundAdapter) Step` — terminal; sends to sink (a `*DirectChannel`, `*memory.Broker`, or any `OutboundAdapter`), ignores next; nil sink → `ErrNilSink` (F2/F4).
  - sentinel `ErrNilSink`.

- [ ] **Step 1: Write the failing test** — `handler_test.go`

```go
package msgin_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kartaladev/msgin"
	"github.com/kartaladev/msgin/adapter/memory"
)

func appendStep(log *[]string, tag string) msgin.Step {
	return func(next msgin.MessageHandler) msgin.MessageHandler {
		return msgin.HandlerFunc(func(ctx context.Context, m msgin.Message[any]) error {
			*log = append(*log, tag)
			return next.Handle(ctx, m)
		})
	}
}

func TestChain(t *testing.T) {
	tests := []struct {
		name   string
		steps  func(log *[]string) []msgin.Step
		assert func(t *testing.T, err error, log []string)
	}{
		{
			name:  "steps run in declaration order then terminal consumes",
			steps: func(log *[]string) []msgin.Step { return []msgin.Step{appendStep(log, "a"), appendStep(log, "b")} },
			assert: func(t *testing.T, err error, log []string) {
				require.NoError(t, err)
				assert.Equal(t, []string{"a", "b"}, log)
			},
		},
		{
			name:   "single step",
			steps:  func(log *[]string) []msgin.Step { return []msgin.Step{appendStep(log, "only")} },
			assert: func(t *testing.T, err error, log []string) { require.NoError(t, err); assert.Equal(t, []string{"only"}, log) },
		},
		{
			name:   "empty chain is a no-op consume",
			steps:  func(*[]string) []msgin.Step { return nil },
			assert: func(t *testing.T, err error, log []string) { require.NoError(t, err); assert.Empty(t, log) },
		},
		{
			name: "a mid-chain error stops the chain and propagates",
			steps: func(log *[]string) []msgin.Step {
				boom := func(next msgin.MessageHandler) msgin.MessageHandler {
					return msgin.HandlerFunc(func(context.Context, msgin.Message[any]) error { return errors.New("boom") })
				}
				return []msgin.Step{appendStep(log, "a"), boom, appendStep(log, "c")}
			},
			assert: func(t *testing.T, err error, log []string) {
				assert.ErrorContains(t, err, "boom")
				assert.Equal(t, []string{"a"}, log)
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var log []string
			tc.assert(t, msgin.Chain(tc.steps(&log)...).Handle(t.Context(), msgin.New[any](1)), log)
		})
	}
}

func TestTo(t *testing.T) {
	tests := []struct {
		name   string
		run    func(t *testing.T) error
		assert func(t *testing.T, err error)
	}{
		{
			name: "To sends the message to an OutboundAdapter sink",
			run: func(t *testing.T) error {
				sink := memory.New(memory.WithBuffer(1)) // *memory.Broker is an OutboundAdapter
				return msgin.Chain(msgin.To(sink)).Handle(t.Context(), msgin.New[any]("x"))
			},
			assert: func(t *testing.T, err error) { require.NoError(t, err) },
		},
		{
			name: "To(nil) is ErrNilSink",
			run:  func(t *testing.T) error { return msgin.Chain(msgin.To(nil)).Handle(t.Context(), msgin.New[any](1)) },
			assert: func(t *testing.T, err error) { assert.ErrorIs(t, err, msgin.ErrNilSink) },
		},
		{
			name: "To propagates the sink send error",
			run: func(t *testing.T) error {
				return msgin.Chain(msgin.To(errSink{})).Handle(t.Context(), msgin.New[any](1))
			},
			assert: func(t *testing.T, err error) { assert.ErrorContains(t, err, "sink-fail") },
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) { tc.assert(t, tc.run(t)) })
	}
}

// errSink is an OutboundAdapter whose Send always fails (covers the To send-error branch).
type errSink struct{}

func (errSink) Send(context.Context, msgin.Message[any]) error { return errors.New("sink-fail") }
```

- [ ] **Step 2: Run test to verify it fails** — `GOTOOLCHAIN=go1.25.12 go test -run 'TestChain|TestTo' .` → FAIL.

- [ ] **Step 3: Implement** — append to `handler.go`

```go
// Step is a composable pipeline stage: it wraps next and returns a handler that
// does its work then (usually) forwards to next. The Go middleware idiom; the
// typed endpoint constructors (Transform/Filter/Activate) return a Step.
type Step = func(next MessageHandler) MessageHandler

// discardHandler is the terminal no-op reached at the end of a Chain that has no
// To/Consume terminal: it CONSUMES the message and returns nil. See Chain's doc —
// a producing flow MUST end in To or Consume, or its final message is discarded.
type discardHandler struct{}

func (discardHandler) Handle(context.Context, Message[any]) error { return nil }

// Chain composes steps into one MessageHandler, running them in order (steps[0]
// first). The innermost next is a no-op consume, so:
//
// CONTRACT: a flow whose last producing step (Transform/Filter-pass/Activate) has
// no downstream terminal will DISCARD its final message silently. Always end a
// producing flow with a terminal — To(sink) to deliver outward, or Consume for a
// side-effect sink. Chain() with no steps is a no-op consume.
func Chain(steps ...Step) MessageHandler {
	next := MessageHandler(discardHandler{})
	for i := len(steps) - 1; i >= 0; i-- {
		next = steps[i](next)
	}
	return next
}

// To is a terminal Step that sends the message to sink (any OutboundAdapter — a
// *DirectChannel, a *memory.Broker, or a real outbound adapter) and ignores next.
// A nil sink yields ErrNilSink at send time (no panic on caller input).
func To(sink OutboundAdapter) Step {
	return func(MessageHandler) MessageHandler {
		return HandlerFunc(func(ctx context.Context, m Message[any]) error {
			if sink == nil {
				return ErrNilSink
			}
			return sink.Send(ctx, m)
		})
	}
}
```

- [ ] **Step 4: Add sentinel** — append to `errors.go`

```go
	// ErrNilSink is returned by To when its OutboundAdapter sink is nil.
	ErrNilSink = errors.New("msgin: nil outbound sink")
```

- [ ] **Step 5: Run tests** — `GOTOOLCHAIN=go1.25.12 go test -race -run 'TestChain|TestTo' .` → PASS.

- [ ] **Step 6: Commit**

```bash
git add handler.go errors.go handler_test.go
git commit -m "feat(core): add Step/Chain/To linear composition (To over OutboundAdapter)

Spec: 003
Plan: 008
ADR: 0013"
```

---

### Task 4: Transformer / Message Translator — `Transform[A,B]`

**Files:**
- Create: `transformer.go`
- Modify: `errors.go` (add `ErrNilFunc`)
- Test: `transformer_test.go`

**Interfaces:**
- Consumes: `Step`, `PayloadOf`, `WithPayload`, `boxMessage`, `ErrPayloadType`.
- Produces: `func Transform[A, B any](fn func(ctx context.Context, m Message[A]) (Message[B], error)) Step`; sentinel `ErrNilFunc`.

- [ ] **Step 1: Write the failing test** — `transformer_test.go`

```go
package msgin_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kartaladev/msgin"
)

func TestTransform(t *testing.T) {
	capture := func(got *msgin.Message[any]) msgin.MessageHandler {
		return msgin.HandlerFunc(func(_ context.Context, m msgin.Message[any]) error { *got = m; return nil })
	}
	tests := []struct {
		name   string
		fn     func(context.Context, msgin.Message[int]) (msgin.Message[string], error)
		input  any
		next   func(got *msgin.Message[any]) msgin.MessageHandler
		assert func(t *testing.T, err error, forwarded msgin.Message[any])
	}{
		{
			name:  "maps A to B, forwards, preserves headers via WithPayload",
			fn:    func(_ context.Context, m msgin.Message[int]) (msgin.Message[string], error) { return msgin.WithPayload(m, "n"), nil },
			input: 5,
			next:  capture,
			assert: func(t *testing.T, err error, forwarded msgin.Message[any]) {
				require.NoError(t, err)
				assert.Equal(t, "n", forwarded.Payload())
			},
		},
		{
			name:   "wrong input payload type is ErrPayloadType",
			fn:     func(_ context.Context, m msgin.Message[int]) (msgin.Message[string], error) { return msgin.WithPayload(m, ""), nil },
			input:  "not-int",
			next:   capture,
			assert: func(t *testing.T, err error, _ msgin.Message[any]) { assert.ErrorIs(t, err, msgin.ErrPayloadType) },
		},
		{
			name:  "fn error propagates and nothing is forwarded",
			fn:    func(context.Context, msgin.Message[int]) (msgin.Message[string], error) { return msgin.Message[string]{}, errors.New("boom") },
			input: 1,
			next:  capture,
			assert: func(t *testing.T, err error, forwarded msgin.Message[any]) {
				assert.ErrorContains(t, err, "boom")
				assert.Nil(t, forwarded.Payload())
			},
		},
		{
			name:  "downstream (next) error propagates",
			fn:    func(_ context.Context, m msgin.Message[int]) (msgin.Message[string], error) { return msgin.WithPayload(m, "ok"), nil },
			input: 1,
			next: func(*msgin.Message[any]) msgin.MessageHandler {
				return msgin.HandlerFunc(func(context.Context, msgin.Message[any]) error { return errors.New("downstream") })
			},
			assert: func(t *testing.T, err error, _ msgin.Message[any]) { assert.ErrorContains(t, err, "downstream") },
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var forwarded msgin.Message[any]
			step := msgin.Transform(tc.fn)
			tc.assert(t, step(tc.next(&forwarded)).Handle(t.Context(), msgin.New[any](tc.input)), forwarded)
		})
	}
}

func TestTransform_NilFn(t *testing.T) {
	step := msgin.Transform[int, int](nil)
	next := msgin.HandlerFunc(func(context.Context, msgin.Message[any]) error { return nil })
	assert.ErrorIs(t, step(next).Handle(t.Context(), msgin.New[any](1)), msgin.ErrNilFunc)
}
```

- [ ] **Step 2: Run test to verify it fails** — `GOTOOLCHAIN=go1.25.12 go test -run 'TestTransform' .` → FAIL.

- [ ] **Step 3: Implement** — `transformer.go`

```go
package msgin

import "context"

// Transform is a Message Translator endpoint: it asserts the input payload to A,
// applies fn to produce a Message[B], and forwards it downstream. fn MUST return
// a new message and is responsible for header propagation — use WithPayload
// (keeps id/correlation) rather than bare New. A non-A payload yields
// ErrPayloadType (routed to the invalid-message channel); an fn error propagates
// without forwarding. A nil fn yields ErrNilFunc (no panic on caller input).
func Transform[A, B any](fn func(ctx context.Context, m Message[A]) (Message[B], error)) Step {
	if fn == nil {
		return nilFuncStep()
	}
	return func(next MessageHandler) MessageHandler {
		return HandlerFunc(func(ctx context.Context, msg Message[any]) error {
			in, err := PayloadOf[A](msg)
			if err != nil {
				return err
			}
			out, err := fn(ctx, in)
			if err != nil {
				return err
			}
			return next.Handle(ctx, boxMessage(out))
		})
	}
}

// nilFuncStep is the Step returned by an endpoint constructor given a nil
// function: its handler returns ErrNilFunc instead of panicking on a nil call.
func nilFuncStep() Step {
	return func(MessageHandler) MessageHandler {
		return HandlerFunc(func(context.Context, Message[any]) error { return ErrNilFunc })
	}
}
```

- [ ] **Step 4: Add sentinel** — append to `errors.go`

```go
	// ErrNilFunc is returned by an endpoint (Transform/Filter/Activate/Consume/
	// Router) constructed with a nil function, instead of panicking at dispatch.
	ErrNilFunc = errors.New("msgin: nil endpoint function")
```

- [ ] **Step 5: Run tests** — `GOTOOLCHAIN=go1.25.12 go test -race -run 'TestTransform' .` → PASS.

- [ ] **Step 6: Commit**

```bash
git add transformer.go errors.go transformer_test.go
git commit -m "feat(core): add Transform[A,B] transformer endpoint (+ ErrNilFunc guard)

Spec: 003
Plan: 008
ADR: 0013"
```

---

### Task 5: Filter — `Filter[A]` + `WithDiscardChannel`

**Files:**
- Create: `filter.go`
- Test: `filter_test.go`

**Interfaces:**
- Consumes: `Step`, `MessageChannel`, `PayloadOf`, `ErrPayloadType`, `ErrNilFunc`, `nilFuncStep`.
- Produces: `func Filter[A any](pred func(ctx context.Context, m Message[A]) (bool, error), opts ...FilterOption) Step`; `type FilterOption func(*filterConfig)`; `func WithDiscardChannel(ch MessageChannel) FilterOption`.

- [ ] **Step 1: Write the failing test** — `filter_test.go`

```go
package msgin_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kartaladev/msgin"
)

func TestFilter(t *testing.T) {
	tests := []struct {
		name       string
		pred       func(context.Context, msgin.Message[int]) (bool, error)
		withDiscard bool
		discardErr error
		assert     func(t *testing.T, err error, forwarded, discarded bool)
	}{
		{
			name:   "true forwards downstream",
			pred:   func(_ context.Context, m msgin.Message[int]) (bool, error) { return m.Payload() > 0, nil },
			assert: func(t *testing.T, err error, forwarded, discarded bool) { require.NoError(t, err); assert.True(t, forwarded); assert.False(t, discarded) },
		},
		{
			name:   "false with no discard channel is silently dropped",
			pred:   func(context.Context, msgin.Message[int]) (bool, error) { return false, nil },
			assert: func(t *testing.T, err error, forwarded, discarded bool) { require.NoError(t, err); assert.False(t, forwarded); assert.False(t, discarded) },
		},
		{
			name:        "false with discard channel routes the drop",
			pred:        func(context.Context, msgin.Message[int]) (bool, error) { return false, nil },
			withDiscard: true,
			assert:      func(t *testing.T, err error, forwarded, discarded bool) { require.NoError(t, err); assert.False(t, forwarded); assert.True(t, discarded) },
		},
		{
			name:        "discard channel send error propagates",
			pred:        func(context.Context, msgin.Message[int]) (bool, error) { return false, nil },
			withDiscard: true,
			discardErr:  errors.New("discard-fail"),
			assert:      func(t *testing.T, err error, _, _ bool) { assert.ErrorContains(t, err, "discard-fail") },
		},
		{
			name:   "predicate error propagates",
			pred:   func(context.Context, msgin.Message[int]) (bool, error) { return false, errors.New("boom") },
			assert: func(t *testing.T, err error, _, _ bool) { assert.ErrorContains(t, err, "boom") },
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var forwarded, discarded bool
			next := msgin.HandlerFunc(func(context.Context, msgin.Message[any]) error { forwarded = true; return nil })
			var opts []msgin.FilterOption
			if tc.withDiscard {
				discard := msgin.NewDirectChannel()
				_ = discard.Subscribe(msgin.HandlerFunc(func(context.Context, msgin.Message[any]) error { discarded = true; return tc.discardErr }))
				opts = append(opts, msgin.WithDiscardChannel(discard))
			}
			step := msgin.Filter(tc.pred, opts...)
			tc.assert(t, step(next).Handle(t.Context(), msgin.New[any](1)), forwarded, discarded)
		})
	}
}

func TestFilter_Guards(t *testing.T) {
	next := msgin.HandlerFunc(func(context.Context, msgin.Message[any]) error { return nil })
	t.Run("type mismatch is ErrPayloadType", func(t *testing.T) {
		step := msgin.Filter(func(context.Context, msgin.Message[int]) (bool, error) { return true, nil })
		assert.ErrorIs(t, step(next).Handle(t.Context(), msgin.New[any]("nope")), msgin.ErrPayloadType)
	})
	t.Run("nil predicate is ErrNilFunc", func(t *testing.T) {
		step := msgin.Filter[int](nil)
		assert.ErrorIs(t, step(next).Handle(t.Context(), msgin.New[any](1)), msgin.ErrNilFunc)
	})
}
```

- [ ] **Step 2: Run test to verify it fails** — `GOTOOLCHAIN=go1.25.12 go test -run 'TestFilter' .` → FAIL.

- [ ] **Step 3: Implement** — `filter.go`

```go
package msgin

import "context"

type filterConfig struct{ discard MessageChannel }

// FilterOption configures a Filter endpoint.
type FilterOption func(*filterConfig)

// WithDiscardChannel routes messages a Filter rejects (predicate false) to ch
// instead of silently dropping them (the default). The default — silent drop —
// matches the pattern's intent (a filter's job is to drop); set this when you
// need to audit or dead-letter filtered-out messages.
func WithDiscardChannel(ch MessageChannel) FilterOption {
	return func(c *filterConfig) { c.discard = ch }
}

// Filter is a Message Filter endpoint: it asserts the payload to A, evaluates
// pred, and forwards downstream when true. When false the message is dropped —
// silently by default, or sent to WithDiscardChannel if set. A predicate error
// (or a discard-channel send error) propagates; a non-A payload yields
// ErrPayloadType; a nil pred yields ErrNilFunc.
func Filter[A any](pred func(ctx context.Context, m Message[A]) (bool, error), opts ...FilterOption) Step {
	if pred == nil {
		return nilFuncStep()
	}
	var cfg filterConfig
	for _, opt := range opts {
		opt(&cfg)
	}
	return func(next MessageHandler) MessageHandler {
		return HandlerFunc(func(ctx context.Context, msg Message[any]) error {
			in, err := PayloadOf[A](msg)
			if err != nil {
				return err
			}
			pass, err := pred(ctx, in)
			if err != nil {
				return err
			}
			if pass {
				return next.Handle(ctx, msg)
			}
			if cfg.discard != nil {
				return cfg.discard.Send(ctx, msg)
			}
			return nil
		})
	}
}
```

- [ ] **Step 4: Run tests** — `GOTOOLCHAIN=go1.25.12 go test -race -run 'TestFilter' .` → PASS.

- [ ] **Step 5: Commit**

```bash
git add filter.go filter_test.go
git commit -m "feat(core): add Filter[A] endpoint + WithDiscardChannel

Spec: 003
Plan: 008
ADR: 0013"
```

---

### Task 6: Content-Based Router — `NewRouter` + `WithDefaultChannel` + `ErrNoRoute`

**Files:**
- Create: `router.go`
- Modify: `errors.go` (add `ErrNoRoute`)
- Test: `router_test.go`

**Interfaces:**
- Consumes: `MessageHandler`, `MessageChannel`, `ErrNilFunc`.
- Produces: `type Router struct{…}` (implements `MessageHandler`); `func NewRouter(pick func(ctx context.Context, m Message[any]) (MessageChannel, error), opts ...RouterOption) *Router`; `type RouterOption func(*routerConfig)`; `func WithDefaultChannel(ch MessageChannel) RouterOption`; sentinel `ErrNoRoute`.

- [ ] **Step 1: Write the failing test** — `router_test.go`

```go
package msgin_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kartaladev/msgin"
)

func TestRouter(t *testing.T) {
	tests := []struct {
		name   string
		pick   func(target, def msgin.MessageChannel) func(context.Context, msgin.Message[any]) (msgin.MessageChannel, error)
		useDef bool
		assert func(t *testing.T, err error, routed, def bool)
	}{
		{
			name: "resolved channel receives the message",
			pick: func(target, _ msgin.MessageChannel) func(context.Context, msgin.Message[any]) (msgin.MessageChannel, error) {
				return func(context.Context, msgin.Message[any]) (msgin.MessageChannel, error) { return target, nil }
			},
			assert: func(t *testing.T, err error, routed, def bool) { require.NoError(t, err); assert.True(t, routed); assert.False(t, def) },
		},
		{
			name: "nil channel with no default is ErrNoRoute",
			pick: func(msgin.MessageChannel, msgin.MessageChannel) func(context.Context, msgin.Message[any]) (msgin.MessageChannel, error) {
				return func(context.Context, msgin.Message[any]) (msgin.MessageChannel, error) { return nil, nil }
			},
			assert: func(t *testing.T, err error, _, _ bool) { assert.ErrorIs(t, err, msgin.ErrNoRoute) },
		},
		{
			name: "nil channel with default routes to default",
			pick: func(msgin.MessageChannel, msgin.MessageChannel) func(context.Context, msgin.Message[any]) (msgin.MessageChannel, error) {
				return func(context.Context, msgin.Message[any]) (msgin.MessageChannel, error) { return nil, nil }
			},
			useDef: true,
			assert: func(t *testing.T, err error, _, def bool) { require.NoError(t, err); assert.True(t, def) },
		},
		{
			name: "pick returning (chan, err) propagates err and ignores chan",
			pick: func(target, _ msgin.MessageChannel) func(context.Context, msgin.Message[any]) (msgin.MessageChannel, error) {
				return func(context.Context, msgin.Message[any]) (msgin.MessageChannel, error) { return target, errors.New("boom") }
			},
			assert: func(t *testing.T, err error, routed, _ bool) { assert.ErrorContains(t, err, "boom"); assert.False(t, routed) },
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var routed, def bool
			target := msgin.NewDirectChannel()
			_ = target.Subscribe(msgin.HandlerFunc(func(context.Context, msgin.Message[any]) error { routed = true; return nil }))
			defCh := msgin.NewDirectChannel()
			_ = defCh.Subscribe(msgin.HandlerFunc(func(context.Context, msgin.Message[any]) error { def = true; return nil }))
			var opts []msgin.RouterOption
			if tc.useDef {
				opts = append(opts, msgin.WithDefaultChannel(defCh))
			}
			r := msgin.NewRouter(tc.pick(target, defCh), opts...)
			tc.assert(t, r.Handle(t.Context(), msgin.New[any](1)), routed, def)
		})
	}
}

func TestRouter_NilPick(t *testing.T) {
	r := msgin.NewRouter(nil)
	assert.ErrorIs(t, r.Handle(t.Context(), msgin.New[any](1)), msgin.ErrNilFunc)
}
```

- [ ] **Step 2: Run test to verify it fails** — `GOTOOLCHAIN=go1.25.12 go test -run 'TestRouter' .` → FAIL.

- [ ] **Step 3: Implement** — `router.go`

```go
package msgin

import "context"

type routerConfig struct{ defaultCh MessageChannel }

// RouterOption configures a Router endpoint.
type RouterOption func(*routerConfig)

// WithDefaultChannel sets the channel a Router uses when pick resolves no
// destination (returns a nil channel). Without it, an unresolved message is
// ErrNoRoute — the safe, visible default (an unroutable message is usually a
// misconfiguration you want surfaced, not silently dropped).
func WithDefaultChannel(ch MessageChannel) RouterOption {
	return func(c *routerConfig) { c.defaultCh = ch }
}

// Router is a Content-Based Router endpoint: pick selects the destination for
// each message. A resolved channel receives it; a nil channel routes to
// WithDefaultChannel if set, else ErrNoRoute; a pick error propagates (the
// returned channel is ignored). A nil pick yields ErrNilFunc. Router implements
// MessageHandler — subscribe it to a channel to place it after a Chain, or use
// it as a flow head via NewConsumer[any](src, router.Handle).
type Router struct {
	pick func(ctx context.Context, m Message[any]) (MessageChannel, error)
	cfg  routerConfig
}

var _ MessageHandler = (*Router)(nil)

// NewRouter builds a Router from pick and options. A nil pick is tolerated at
// construction and surfaces as ErrNilFunc at Handle time (no panic on input).
func NewRouter(pick func(ctx context.Context, m Message[any]) (MessageChannel, error), opts ...RouterOption) *Router {
	r := &Router{pick: pick}
	for _, opt := range opts {
		opt(&r.cfg)
	}
	return r
}

// Handle routes msg to the channel pick selects.
func (r *Router) Handle(ctx context.Context, msg Message[any]) error {
	if r.pick == nil {
		return ErrNilFunc
	}
	ch, err := r.pick(ctx, msg)
	if err != nil {
		return err
	}
	if ch == nil {
		if r.cfg.defaultCh == nil {
			return ErrNoRoute
		}
		ch = r.cfg.defaultCh
	}
	return ch.Send(ctx, msg)
}
```

- [ ] **Step 4: Add sentinel** — append to `errors.go`

```go
	// ErrNoRoute is returned by a Router when pick resolves no destination and no
	// WithDefaultChannel is configured (Spring resolutionRequired=true).
	ErrNoRoute = errors.New("msgin: no route for message")
```

- [ ] **Step 5: Run tests** — `GOTOOLCHAIN=go1.25.12 go test -race -run 'TestRouter' .` → PASS.

- [ ] **Step 6: Commit**

```bash
git add router.go errors.go router_test.go
git commit -m "feat(core): add content-based Router + WithDefaultChannel + ErrNoRoute

Spec: 003
Plan: 008
ADR: 0013"
```

---

### Task 7: Service Activator — `Activate[A,B]` + `Consume[A]`

**Files:**
- Create: `activator.go`
- Test: `activator_test.go`

**Interfaces:**
- Consumes: `Step`, `PayloadOf`, `boxMessage`, `WithPayload`, `ErrPayloadType`, `ErrNilFunc`, `nilFuncStep`.
- Produces:
  - `func Activate[A, B any](svc func(ctx context.Context, m Message[A]) (Message[B], error)) Step` — request-reply; forwards reply.
  - `func Consume[A any](svc func(ctx context.Context, m Message[A]) error) Step` — one-way (side effect, no reply); terminal.

- [ ] **Step 1: Write the failing test** — `activator_test.go`

```go
package msgin_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kartaladev/msgin"
)

func TestActivate(t *testing.T) {
	tests := []struct {
		name   string
		svc    func(context.Context, msgin.Message[int]) (msgin.Message[string], error)
		input  any
		next   func(reply *msgin.Message[any]) msgin.MessageHandler
		assert func(t *testing.T, err error, reply msgin.Message[any])
	}{
		{
			name:  "invokes the service and forwards the reply",
			svc:   func(_ context.Context, m msgin.Message[int]) (msgin.Message[string], error) { return msgin.WithPayload(m, "ok"), nil },
			input: 1,
			next:  func(r *msgin.Message[any]) msgin.MessageHandler { return msgin.HandlerFunc(func(_ context.Context, m msgin.Message[any]) error { *r = m; return nil }) },
			assert: func(t *testing.T, err error, reply msgin.Message[any]) { require.NoError(t, err); assert.Equal(t, "ok", reply.Payload()) },
		},
		{
			name:   "service error propagates without forwarding",
			svc:    func(context.Context, msgin.Message[int]) (msgin.Message[string], error) { return msgin.Message[string]{}, errors.New("boom") },
			input:  1,
			next:   func(r *msgin.Message[any]) msgin.MessageHandler { return msgin.HandlerFunc(func(_ context.Context, m msgin.Message[any]) error { *r = m; return nil }) },
			assert: func(t *testing.T, err error, reply msgin.Message[any]) { assert.ErrorContains(t, err, "boom"); assert.Nil(t, reply.Payload()) },
		},
		{
			name:   "wrong payload type is ErrPayloadType",
			svc:    func(_ context.Context, m msgin.Message[int]) (msgin.Message[string], error) { return msgin.WithPayload(m, ""), nil },
			input:  "nope",
			next:   func(*msgin.Message[any]) msgin.MessageHandler { return msgin.HandlerFunc(func(context.Context, msgin.Message[any]) error { return nil }) },
			assert: func(t *testing.T, err error, _ msgin.Message[any]) { assert.ErrorIs(t, err, msgin.ErrPayloadType) },
		},
		{
			name:   "downstream error propagates",
			svc:    func(_ context.Context, m msgin.Message[int]) (msgin.Message[string], error) { return msgin.WithPayload(m, "ok"), nil },
			input:  1,
			next:   func(*msgin.Message[any]) msgin.MessageHandler { return msgin.HandlerFunc(func(context.Context, msgin.Message[any]) error { return errors.New("downstream") }) },
			assert: func(t *testing.T, err error, _ msgin.Message[any]) { assert.ErrorContains(t, err, "downstream") },
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var reply msgin.Message[any]
			step := msgin.Activate(tc.svc)
			tc.assert(t, step(tc.next(&reply)).Handle(t.Context(), msgin.New[any](tc.input)), reply)
		})
	}
}

func TestConsume(t *testing.T) {
	tests := []struct {
		name   string
		svc    func(seen *int) func(context.Context, msgin.Message[int]) error
		assert func(t *testing.T, err error, seen int, forwarded bool)
	}{
		{
			name:   "runs the side effect and does not forward",
			svc:    func(seen *int) func(context.Context, msgin.Message[int]) error { return func(_ context.Context, m msgin.Message[int]) error { *seen = m.Payload(); return nil } },
			assert: func(t *testing.T, err error, seen int, forwarded bool) { require.NoError(t, err); assert.Equal(t, 7, seen); assert.False(t, forwarded) },
		},
		{
			name:   "service error propagates",
			svc:    func(*int) func(context.Context, msgin.Message[int]) error { return func(context.Context, msgin.Message[int]) error { return errors.New("boom") } },
			assert: func(t *testing.T, err error, _ int, _ bool) { assert.ErrorContains(t, err, "boom") },
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var seen int
			var forwarded bool
			next := msgin.HandlerFunc(func(context.Context, msgin.Message[any]) error { forwarded = true; return nil })
			tc.assert(t, msgin.Consume(tc.svc(&seen))(next).Handle(t.Context(), msgin.New[any](7)), seen, forwarded)
		})
	}
}

func TestActivator_NilFn(t *testing.T) {
	next := msgin.HandlerFunc(func(context.Context, msgin.Message[any]) error { return nil })
	assert.ErrorIs(t, msgin.Activate[int, int](nil)(next).Handle(t.Context(), msgin.New[any](1)), msgin.ErrNilFunc)
	assert.ErrorIs(t, msgin.Consume[int](nil)(next).Handle(t.Context(), msgin.New[any](1)), msgin.ErrNilFunc)
}
```

- [ ] **Step 2: Run test to verify it fails** — `GOTOOLCHAIN=go1.25.12 go test -run 'TestActivate|TestConsume|TestActivator' .` → FAIL.

- [ ] **Step 3: Implement** — `activator.go`

```go
package msgin

import "context"

// Activate is a request-reply Service Activator: the boundary where a flow
// invokes your domain service. It asserts the payload to A, calls svc, and
// forwards svc's reply (Message[B]) downstream. Use WithPayload in svc to keep
// the id/correlation headers. A non-A payload yields ErrPayloadType; an svc
// error propagates without forwarding; a nil svc yields ErrNilFunc. For a
// one-way service with no reply, use Consume.
func Activate[A, B any](svc func(ctx context.Context, m Message[A]) (Message[B], error)) Step {
	if svc == nil {
		return nilFuncStep()
	}
	return func(next MessageHandler) MessageHandler {
		return HandlerFunc(func(ctx context.Context, msg Message[any]) error {
			in, err := PayloadOf[A](msg)
			if err != nil {
				return err
			}
			reply, err := svc(ctx, in)
			if err != nil {
				return err
			}
			return next.Handle(ctx, boxMessage(reply))
		})
	}
}

// Consume is a one-way Service Activator: it asserts the payload to A and calls
// svc for its side effect, forwarding nothing (a terminal step — next never
// runs). A non-A payload yields ErrPayloadType; an svc error propagates; a nil
// svc yields ErrNilFunc.
func Consume[A any](svc func(ctx context.Context, m Message[A]) error) Step {
	if svc == nil {
		return nilFuncStep()
	}
	return func(MessageHandler) MessageHandler {
		return HandlerFunc(func(ctx context.Context, msg Message[any]) error {
			in, err := PayloadOf[A](msg)
			if err != nil {
				return err
			}
			return svc(ctx, in)
		})
	}
}
```

- [ ] **Step 4: Run tests** — `GOTOOLCHAIN=go1.25.12 go test -race -run 'TestActivate|TestConsume|TestActivator' .` → PASS.

- [ ] **Step 5: Commit**

```bash
git add activator.go activator_test.go
git commit -m "feat(core): add Service Activator (Activate[A,B] + Consume[A])

Spec: 003
Plan: 008
ADR: 0013"
```

---

### Task 8: End-to-end integration (through `NewConsumer`), example, package doc, whole-branch gate

Proves the headline claim — a composed flow drives off the existing `Consumer` runtime — with a **live-value** source (F3), and documents the wire-source constraint.

**Files:**
- Create: `example_composition_test.go` (runnable `Example` — doubles as godoc)
- Create: `composition_integration_test.go` (drives a flow through `NewConsumer[any]` over `memory.New()`)
- Create: `doc_composition.go` (package-level doc paragraph) — verify first with `ls doc*.go`; the root currently has **no** `doc.go`, so a new package-doc file is safe (if that changes, MERGE the paragraph instead).
- Test: the example + the integration test are their own tests.

**Interfaces:**
- Consumes: all Task 1–7 symbols + existing `NewConsumer`, `WithShutdownTimeout`, `adapter/memory`.

- [ ] **Step 1: Write the failing integration test** — `composition_integration_test.go`

```go
package msgin_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kartaladev/msgin"
	"github.com/kartaladev/msgin/adapter/memory"
)

// A composed flow driven by NewConsumer over a LIVE-VALUE source (memory)
// inherits the runtime and processes typed payloads end-to-end (Spec 003 D8 / F3).
func TestComposition_DrivesOffConsumer(t *testing.T) {
	type Order struct {
		ID   string
		Paid bool
	}
	type Invoice struct{ OrderID string }

	src := memory.New(memory.WithBuffer(4))
	got := make(chan Invoice, 4)

	flow := msgin.Chain(
		msgin.Filter(func(_ context.Context, m msgin.Message[Order]) (bool, error) { return m.Payload().Paid, nil }),
		msgin.Transform(func(_ context.Context, m msgin.Message[Order]) (msgin.Message[Invoice], error) {
			return msgin.WithPayload(m, Invoice{OrderID: m.Payload().ID}), nil
		}),
		msgin.Consume(func(_ context.Context, m msgin.Message[Invoice]) error { got <- m.Payload(); return nil }),
	)

	consumer, err := msgin.NewConsumer[any](src, flow.Handle, msgin.WithShutdownTimeout[any](2*time.Second))
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- consumer.Run(ctx) }()

	require.NoError(t, src.Send(ctx, msgin.New[any](Order{ID: "o-1", Paid: true})))
	require.NoError(t, src.Send(ctx, msgin.New[any](Order{ID: "o-2", Paid: false}))) // filtered out

	select {
	case inv := <-got:
		assert.Equal(t, "o-1", inv.OrderID)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for the flow to process")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("consumer did not shut down")
	}
	// goleak (root TestMain) verifies no goroutine leak.
}
```

- [ ] **Step 2: Run it, verify it passes after Tasks 1–7**

Run: `GOTOOLCHAIN=go1.25.12 go test -race -run 'TestComposition_DrivesOffConsumer' .`
Expected: PASS (o-1 delivered, o-2 filtered, clean shutdown, goleak-clean). If `NewConsumer[any]` inference complains, the explicit `[any]` is already given — confirm the `src` is recognized as a live-value StreamingSource.

- [ ] **Step 3: Write the runnable example** — `example_composition_test.go`

```go
package msgin_test

import (
	"context"
	"fmt"

	"github.com/kartaladev/msgin"
)

// ExampleChain shows a linear in-process flow: filter unpaid orders, translate
// Order->Invoice (preserving headers via WithPayload), charge, and deliver.
func ExampleChain() {
	type Order struct {
		ID   string
		Paid bool
	}
	type Invoice struct{ OrderID string }
	type Receipt struct{ OrderID string }

	receipts := make(chan Receipt, 1)

	flow := msgin.Chain(
		msgin.Filter(func(_ context.Context, m msgin.Message[Order]) (bool, error) { return m.Payload().Paid, nil }),
		msgin.Transform(func(_ context.Context, m msgin.Message[Order]) (msgin.Message[Invoice], error) {
			return msgin.WithPayload(m, Invoice{OrderID: m.Payload().ID}), nil
		}),
		msgin.Activate(func(_ context.Context, m msgin.Message[Invoice]) (msgin.Message[Receipt], error) {
			return msgin.WithPayload(m, Receipt{OrderID: m.Payload().OrderID}), nil
		}),
		msgin.Consume(func(_ context.Context, m msgin.Message[Receipt]) error { receipts <- m.Payload(); return nil }),
	)

	_ = flow.Handle(context.Background(), msgin.New[any](Order{ID: "o-1", Paid: true}))
	_ = flow.Handle(context.Background(), msgin.New[any](Order{ID: "o-2", Paid: false})) // filtered

	fmt.Println((<-receipts).OrderID)
	// Output: o-1
}
```

- [ ] **Step 4: Write the package doc** — `doc_composition.go`

```go
// Package msgin — in-process composition (Spec 003 / ADR 0013).
//
// Beyond adapters, msgin composes an in-process message flow from small
// endpoints wired as pipes and filters. A MessageHandler is one step; a
// MessageChannel is the conduit. The linear endpoints — Transform (Message
// Translator), Filter, and Activate/Consume (Service Activator) — are Steps
// composed by Chain; a content-based Router branches to a MessageChannel. End a
// producing Chain with To(sink) or Consume, or its final message is discarded.
//
// A composed flow is a Handler[any], so NewConsumer[any](src, flow.Handle, …)
// drives it and it inherits retry, dead-letter, invalid-message, flow-control,
// and the worker pool. Typed endpoints assume the payload is the live Go value:
// this holds for live-value sources (memory); a WIRE source at T=any decodes to
// map[string]any, so decode to the concrete type in the first endpoint (a
// bytes-passthrough WithConsumerCodec[any] + Transform[[]byte, T]). Endpoint
// errors propagate into the runtime; a payload-type mismatch is ErrPayloadType,
// routed to the invalid-message channel.
package msgin
```

- [ ] **Step 5: Run the whole-package suite + gate checks**

```bash
GOTOOLCHAIN=go1.25.12 go test ./... -race
GOTOOLCHAIN=go1.25.12 go vet ./...
gofmt -l .
GOTOOLCHAIN=go1.25.12 go test -coverprofile=/tmp/cover.out . && GOTOOLCHAIN=go1.25.12 go tool cover -func=/tmp/cover.out | tail -1
CGO_ENABLED=0 GOTOOLCHAIN=go1.25.12 go build ./...
GOTOOLCHAIN=go1.25.12 go mod tidy && git diff --exit-code go.mod go.sum
```
Expected: `-race` PASS (goleak-clean); `vet`/`gofmt` silent; root coverage ≥85%; `CGO_ENABLED=0` build OK; `go mod tidy` leaves go.mod/go.sum unchanged (no new dep).

- [ ] **Step 6: Commit**

```bash
git add example_composition_test.go composition_integration_test.go doc_composition.go
git commit -m "test(core): composition end-to-end via NewConsumer + example + package doc

Spec: 003
Plan: 008
ADR: 0013"
```

---

## Whole-branch delivery gate (before merge to main)

Run over `main..HEAD`, resolve/triage every finding, confirm green — per CLAUDE.md §5:

- [ ] `/code-review` on `main..HEAD`; fix or triage findings.
- [ ] `/security-review` on the pending changes; confirm the type-assertion + nil-guard paths (low external surface — no I/O, reflection, or unsafe).
- [ ] `go test ./... -race` green; `golangci-lint run ./...` clean; `govulncheck ./...` clean; `gofmt`/`gofumpt` silent.
- [ ] Coverage: root package ≥85%; every typed-error branch covered (`ErrPayloadType`, `ErrNoRoute`, `ErrChannelSubscribed`, `ErrNoSubscriber`, `ErrNilHandler`, `ErrNilSink`, `ErrNilFunc`) and each endpoint's forward/drop/error/downstream branch.
- [ ] Update `docs/HANDOVER.md`: Phase 1 complete; Phase 2 (`QueueChannel`, ADR 0014) next.
- [ ] Confirm with the user before merge/push (never merge/push without explicit approval).

## Self-review notes (author, post-audit-round-1)

- **Spec coverage:** D1→all tasks root pkg; D2→Tasks 2,4,5,7 (typed constructors + PayloadOf + WithPayload); D3→Task 1; D4→Task 3 (Step/Chain/To); D5→error-propagation cases in Tasks 3–7 + ErrPayloadType routing (validated via `reliability.go:40`); D6→Tasks 4–7 (Filter silent-drop, Router ErrNoRoute, Activate/Consume split); D8→Task 8 integration test. **Deferred (correct):** D7 QueueChannel/PubSub = Phases 2–3 (ADR 0014); D9 DSL = Phase 4 gated.
- **Audit fixes folded:** F1 (reuse `ErrPayloadType`), F2 (`To(OutboundAdapter)` + `ErrNilSink`), F3 (wire/live scoping + `TestComposition_DrivesOffConsumer` integration + doc), F4 (nil-func guards + `ErrNilFunc` in Tasks 4/5/6/7 via `nilFuncStep`/`Router.Handle`), F5 (`WithPayload`), F6 (`Chain`-terminal contract doc), F7 (send-error/downstream/single-step/pick-(ch,err)/nil-func cases added; integration test), F8 (D6 Activate/Consume split — see ADR edit), F9 (explicit `NewConsumer[any]` in examples).
- **Type consistency:** `MessageHandler.Handle`, `HandlerFunc`, `MessageChannel.{Send,Subscribe}`, `DirectChannel`, `Step`, `Chain`, `To(OutboundAdapter)`, `PayloadOf[T]`, `WithPayload[A,B]`, `boxMessage`, `nilFuncStep`, `Transform[A,B]`, `Filter[A]`+`WithDiscardChannel`+`FilterOption`, `NewRouter`/`Router`/`WithDefaultChannel`/`RouterOption`, `Activate[A,B]`, `Consume[A]`, and sentinels `ErrChannelSubscribed/ErrNoSubscriber/ErrNilHandler/ErrNilSink/ErrNilFunc/ErrNoRoute` (+ reused `ErrPayloadType`) used consistently across tasks.
- **Round-2 re-audit: DONE — SOUND WITH FIXES.** All nine round-1 fixes verified against real source; two doc-only residuals folded (spec §4 `To` signature; dead `sendErrChan` comment). Design-time gate CLEAR; ready for implementation pending explicit user go-ahead + SDD-vs-direct choice.
