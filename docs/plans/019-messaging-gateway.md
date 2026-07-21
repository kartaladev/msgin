# Messaging Gateway (in-process request-reply) — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax.
>
> **Go skills are mandatory (CLAUDE.md writing-plans override):** every task starts from **`cc-skills-golang:golang-how-to`**, uses **`superpowers:test-driven-development`** (red→green→refactor), navigates/refactors via **`gopls`**, and obeys the project testing overrides: **`table-test`** (assert-closure tables, `t.Context()`), **`use-mockgen`**, **`use-testcontainers`**. Blackbox `_test` packages only.

**Goal:** Add the EIP **Messaging Gateway** to the core `msgin` package — an inbound typed `Gateway[Req,Rep]` and an in-flow `OutboundGateway` `Step`, both delegating to a new `RequestReplyExchange` SPI with one in-process `ChannelExchange` implementation over a shared, zero-goroutine reply correlator.

**Architecture:** One primitive (`replyCorrelator`: a mutex-guarded `map[correlationID]→one-shot slot`, plus a `MessageHandler` subscribed to the reply channel that demuxes replies to blocked waiters), one SPI (`RequestReplyExchange.Exchange`), one in-process impl (`ChannelExchange`), and two thin façades. Inbound `Request` boxes `Req`→`Message[any]` and unboxes the reply→`Rep`; `OutboundGateway` runs the exchange mid-flow and forwards the reply to `next`, saving/restoring the incoming `HeaderCorrelationID` so an upstream splitter/aggregator id survives the round-trip. No background goroutines → leak-free by construction.

**Tech Stack:** Go 1.25, stdlib (`context`, `sync`, `log/slog`, `io`), `github.com/jonboulle/clockwork` (existing core dep). **No new dependency.**

**Spec:** [Spec 010 — Messaging Gateway](../specs/010-messaging-gateway.md). **ADR:** [ADR 0022 — Messaging Gateway & the `RequestReplyExchange` model](../adrs/0022-messaging-gateway.md). Builds on [ADR 0013 — Composition endpoints](../adrs/0013-composition-endpoints.md), [ADR 0001 — Payload typing](../adrs/0001-message-payload-typing.md), [ADR 0004 — clockwork](../adrs/0004-clockwork-dependency.md).

## Global Constraints

- **Go 1.25 only** (`go.mod` `go 1.25`; build/test `GOTOOLCHAIN=go1.25.12`). No features newer than 1.25.
- **No new dependency.** Core stays stdlib + `clockwork`. The whole-branch gate re-asserts `go mod tidy`/`go.sum` show **no** new module and `go mod verify` passes.
- **Purely additive public API** → **minor SemVer**. New exported symbols only: `RequestReplyExchange`, `ChannelExchange`, `NewChannelExchange`, `ExchangeOption`, `WithReplyTimeout`, `WithUnmatchedReplySink`, `WithExchangeClock`, `WithExchangeLogger`, `Gateway[Req,Rep]`, `NewGateway`, `GatewayOption`, `OutboundGateway`, `Message.WithoutHeader`, and the sentinels `ErrGatewayClosed`/`ErrReplyTimeout`/`ErrNilExchange`/`ErrNilChannel`/`ErrInvalidReplyTimeout`. No shipped signature changes. Verify additions-only (`apidiff`/manual).
- **No logging to a package global by default.** Default logger is `slog.New(slog.NewTextHandler(io.Discard, nil))`, exactly as `consumer.go:147`. Never `slog.Default()`.
- **Never `os.Exit`/`log.Fatal`/`panic` on caller input.** Construction faults return typed errors; the set-flag option pattern (per `WithMaxInFlight`) distinguishes "unset → default" from "explicit invalid → typed error".
- **Blackbox tests** (`package msgin_test`), **assert-closure tables**, `t.Context()`. `Example…` tests double as godoc. **`goleak`** in `TestMain` (or per-test) proves no goroutine leak.
- **Coverage** ≥ 85% on the core package for new code; **every hot-path/typed-error branch has a covering test** (enumerated per task).
- **`go test ./... -race`** green; `go vet`/`gofmt`/`golangci-lint`/`govulncheck`/`CGO_ENABLED=0 go build ./...` clean; `go mod tidy`/`go mod verify` stable.
- Every exported symbol has a godoc comment (value + rationale for defaults, per the CLAUDE.md defaults gate — the 30s reply-timeout godoc states the value, why it's safe, and that `min(ctx, timeout)` applies).
- **Traceability:** every commit carries `Spec: 010` / `Plan: 019` / `ADR: 0022` trailers.

---

## File Structure

- `exchange.go` (core `msgin`, **create**) — the `RequestReplyExchange` interface; the unexported `replyCorrelator` (`register`/`deliver`/`closeAll`); `ChannelExchange` + `NewChannelExchange` + `Exchange` + `Close`; `exchangeConfig` + the four `ExchangeOption`s; `defaultReplyTimeout`.
- `gateway.go` (core `msgin`, **create**) — `Gateway[Req,Rep]` + `NewGateway` + `Request`; `GatewayOption[Req,Rep]`; `OutboundGateway` `Step`.
- `errors.go` (**modify**) — add the five sentinels to the existing `var (…)` block.
- `message.go` (**modify**) — add `Message.WithoutHeader(key)` + `Headers.without(key)` (copy-on-write delete).
- `exchange_test.go` (`package msgin_test`, **create**) — `ChannelExchange`/correlator tests + goleak.
- `gateway_test.go` (`package msgin_test`, **create**) — inbound + outbound gateway tests + `Example`s.
- `message_test.go` (**modify** — it exists, `package msgin_test`) — `WithoutHeader` table test.
- `doc_composition.go` (**modify**) — one short paragraph naming the gateway endpoints.

---

### Task 1: `RequestReplyExchange` SPI + `replyCorrelator` + `ChannelExchange` + sentinels

**Files:** Create `exchange.go`, `exchange_test.go`; modify `errors.go`.

**Interfaces:**
- Consumes (existing): `MessageChannel` (`Send`/`Subscribe`), `MessageHandler`/`HandlerFunc`, `Message[any]`, `Headers.String`, `OutboundAdapter` (`Send`), `clockwork.Clock` (`NewTimer(d).Chan()/.Stop()`), `ErrChannelSubscribed`.
- Produces: `type RequestReplyExchange interface { Exchange(ctx, Message[any]) (Message[any], error) }`; `func NewChannelExchange(request, reply MessageChannel, opts ...ExchangeOption) (*ChannelExchange, error)`; `func (*ChannelExchange) Exchange(ctx, Message[any]) (Message[any], error)`; `func (*ChannelExchange) Close() error`; options `WithReplyTimeout(time.Duration)`, `WithUnmatchedReplySink(OutboundAdapter)`, `WithExchangeClock(clockwork.Clock)`, `WithExchangeLogger(*slog.Logger)`; sentinels `ErrGatewayClosed`, `ErrReplyTimeout`, `ErrNilChannel`, `ErrInvalidReplyTimeout` (and `ErrNilExchange`, consumed in Task 2).

- [ ] **Step 1: Add the sentinels** to `errors.go` (inside the existing `var (…)` block):

```go
	// ErrGatewayClosed is returned by ChannelExchange.Exchange (and any Gateway
	// built on it) once Close has been called: new exchanges are rejected and
	// any waiter pending at Close time fails with it. Part of graceful shutdown.
	ErrGatewayClosed = errors.New("msgin: request-reply exchange is closed")

	// ErrReplyTimeout is returned by Exchange when no correlated reply arrives
	// before the effective deadline (min of the caller ctx deadline and the
	// configured reply timeout, default 30s). Distinct from ctx cancellation,
	// which returns the ctx error.
	ErrReplyTimeout = errors.New("msgin: timed out awaiting reply")

	// ErrNilExchange is returned by NewGateway when the RequestReplyExchange is nil.
	ErrNilExchange = errors.New("msgin: request-reply exchange is nil")

	// ErrNilChannel is returned by NewChannelExchange when the request or reply
	// MessageChannel is nil.
	ErrNilChannel = errors.New("msgin: request or reply channel is nil")

	// ErrInvalidReplyTimeout is returned by NewChannelExchange when an explicit
	// WithReplyTimeout is set to a non-positive duration. An unset timeout takes
	// the 30s default and never yields this error.
	ErrInvalidReplyTimeout = errors.New("msgin: reply timeout must be > 0")

	// ErrDuplicateCorrelation is returned by Exchange when the request's
	// HeaderCorrelationID already has an in-flight request registered on the same
	// exchange (audit G1). Correlation ids must be unique per concurrent
	// in-flight request; the gateway façades mint a fresh id so they never trigger
	// this — it guards direct ChannelExchange users who set the header by hand.
	ErrDuplicateCorrelation = errors.New("msgin: correlation id already has an in-flight request")
```

> **Reuse `ErrNoCorrelation`** (already in `errors.go`, `"msgin: message has no correlation key"`): `Exchange` returns it when the request carries an **empty** `HeaderCorrelationID` — mirroring the Aggregator's `Permanent(ErrNoCorrelation)` invariant (`aggregator.go:138`) so the gateway does not silently accept an un-correlatable request (audit G1). Do **not** redefine it.

- [ ] **Step 2: Write the failing test** (`exchange_test.go`, `package msgin_test`). Use `goleak` in `TestMain`. Cover the hot-path branches with an assert-closure table plus a few focused tests. Helpers: a `newLoopExchange(t, opts...)` that builds `request := NewDirectChannel()`, `reply := NewDirectChannel()`, an `ex, err := NewChannelExchange(request, reply, opts...)`, and subscribes a **flow** onto `request` that echoes the request straight to `reply` via `To(reply)` (a synchronous round-trip) — so `Exchange` returns the echoed message. Assert-closure table cases:

```go
func TestMain(m *testing.M) { goleak.VerifyTestMain(m) }

func TestNewChannelExchange_validation(t *testing.T) {
	direct := msgin.NewDirectChannel()
	tests := []struct {
		name    string
		request msgin.MessageChannel
		reply   msgin.MessageChannel
		opts    []msgin.ExchangeOption
		assert  func(t *testing.T, ex *msgin.ChannelExchange, err error)
	}{
		{
			name: "nil request", request: nil, reply: direct,
			assert: func(t *testing.T, ex *msgin.ChannelExchange, err error) {
				if !errors.Is(err, msgin.ErrNilChannel) {
					t.Fatalf("want ErrNilChannel, got %v", err)
				}
			},
		},
		{
			name: "nil reply", request: direct, reply: nil,
			assert: func(t *testing.T, ex *msgin.ChannelExchange, err error) {
				if !errors.Is(err, msgin.ErrNilChannel) {
					t.Fatalf("want ErrNilChannel, got %v", err)
				}
			},
		},
		{
			name: "explicit non-positive timeout", request: msgin.NewDirectChannel(), reply: msgin.NewDirectChannel(),
			opts: []msgin.ExchangeOption{msgin.WithReplyTimeout(0)},
			assert: func(t *testing.T, ex *msgin.ChannelExchange, err error) {
				if !errors.Is(err, msgin.ErrInvalidReplyTimeout) {
					t.Fatalf("want ErrInvalidReplyTimeout, got %v", err)
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ex, err := msgin.NewChannelExchange(tt.request, tt.reply, tt.opts...)
			tt.assert(t, ex, err)
		})
	}
}
```

Add separate tests (each drives `Exchange` through the exported API). **Synchronization note (avoid flakiness):** for the tests that fire a timeout or `Close` while a waiter is blocked, the non-replying `request` flow uses a `Consume` sink that **signals a channel** when invoked (`sinkHit := make(chan struct{}, 1)`; the sink does `sinkHit <- struct{}{}`). Because a synchronous `DirectChannel` runs the flow inside `request.Send`, receiving on `sinkHit` proves the waiter is registered and `Exchange` has reached (or is about to reach) its `select` before the test advances the clock or calls `Close`. For the timeout test also add `fakeClock.BlockUntil(1)` after `sinkHit` to guarantee the timer is registered before `Advance`.
- `TestChannelExchange_roundTrip` — the echo flow returns the reply with the same `HeaderCorrelationID`; assert `Payload()`/id.
- `TestChannelExchange_replyTimeout` — non-replying `Consume` sink (signals `sinkHit`); `clockwork.NewFakeClock()` via `WithExchangeClock`; run `Exchange` in a goroutine; `<-sinkHit`; `fakeClock.BlockUntil(1)`; `fakeClock.Advance(30 * time.Second)`; assert `errors.Is(err, ErrReplyTimeout)`.
- `TestChannelExchange_ctxCancel` — non-replying flow (signals `sinkHit`); `<-sinkHit`; `cancel()`; assert `errors.Is(err, context.Canceled)`.
- `TestChannelExchange_sendError` — `request` is an unsubscribed `NewDirectChannel()`; `Exchange` returns the channel's `ErrNoSubscriber` and does not leak a waiter (a subsequent reply with that id is unmatched).
- `TestChannelExchange_closed` — after `Close()`, a fresh `Exchange` returns `ErrGatewayClosed`; and a waiter blocked at `Close` time (non-replying flow signals `sinkHit`; `Exchange` in a goroutine; `<-sinkHit`; then `Close()`) unblocks with `ErrGatewayClosed`.
- `TestChannelExchange_unmatchedReply_drop` — send a reply (via the `reply` channel directly) whose correlation id has no waiter; assert the reply-channel `Send` returns `nil` (dropped) — capture logs optionally via a `slog` test handler.
- `TestChannelExchange_unmatchedReply_sink` — with `WithUnmatchedReplySink(sink)`, the unmatched reply lands in `sink` and `Send` returns `nil`.
- `TestChannelExchange_emptyCorrelation` (audit G1) — `Exchange` with a request carrying no/empty `HeaderCorrelationID` returns `errors.Is(err, ErrNoCorrelation)` without sending.
- `TestChannelExchange_duplicateCorrelation` (audit G1) — two concurrent `Exchange` calls sharing one `HeaderCorrelationID` (first parked on a non-replying flow via `sinkHit`): the second returns `errors.Is(err, ErrDuplicateCorrelation)`; the first still completes/​times-out normally, proving no cross-delete.

**Async + concurrent hot path (audit G2 — the reason the primitive exists; currently the ONLY cross-goroutine coverage).** Add an async harness whose request flow hands the echo to a **separate goroutine** that calls `reply.Send`, with the worker joined so `goleak` stays clean:

```go
// asyncEcho wires request → a worker goroutine that echoes each request to reply.
// stop() drains and joins the worker (goleak-clean). Because reply.Send runs on
// the worker goroutine, the waiter's select genuinely races deliver.
func asyncEcho(t *testing.T, request, reply msgin.MessageChannel) (stop func()) {
	work := make(chan msgin.Message[any], 64)
	done := make(chan struct{})
	if err := request.Subscribe(msgin.HandlerFunc(func(_ context.Context, m msgin.Message[any]) error {
		work <- m
		return nil
	})); err != nil {
		t.Fatal(err)
	}
	go func() {
		defer close(done)
		for m := range work {
			_ = reply.Send(context.Background(), m) // m already carries HeaderCorrelationID
		}
	}()
	return func() { close(work); <-done }
}
```

- `TestChannelExchange_asyncRoundTrip` — one `Exchange` over `asyncEcho`; reply is delivered from the worker goroutine; assert the correlated reply returns. `goleak` clean after `stop()`.
- `TestChannelExchange_concurrentRequests_race` — over `asyncEcho`, launch N=50 goroutines each doing `Exchange` with a **distinct** `HeaderCorrelationID` (e.g. `strconv.Itoa(i)`); assert every call returns its own reply (match a payload/id echoed per request). Run under `-race`; this is the primary proof of the concurrency claim.
- `TestChannelExchange_timeoutRacesDelivery` (audit G4) — fake clock; a request whose reply is delivered from the worker at ~the same time the timeout fires; loop/repeat to exercise both outcomes; assert the call returns **either** the reply **or** `ErrReplyTimeout` (never a panic, never a hang, `-race` clean), and that when it returns `ErrReplyTimeout` a `WithUnmatchedReplySink` receives the raced-in reply (the `giveUp` drain) rather than it vanishing.
- `TestChannelExchange_closeRacesGiveUp` (audit N2 — covers `giveUp`'s `ok==false` drain arm, where `closeAll` closed the slot before the waiter reconciled) — non-replying flow (signals `sinkHit`); fake clock; run `Exchange` in a goroutine; `<-sinkHit`; then race `Close()` against `fakeClock.Advance(30s)`; assert the call returns one of `ErrGatewayClosed`/`ErrReplyTimeout` with **no panic and no goroutine leak** (goleak), and no reply is routed to the sink (the slot was closed, not delivered). This is the defensive-arm coverage the hot-path gate requires.

Note the **send-error** case (`TestChannelExchange_sendError`) now routes through `giveUp`; assert no waiter leak (a later reply with that id is unmatched) — `deregister` removes it before any deliver, so `giveUp` returns immediately.

- [ ] **Step 3: Run to verify it fails.** `GOTOOLCHAIN=go1.25.12 go test . -run 'ChannelExchange|NewChannelExchange'` → `undefined: msgin.NewChannelExchange` etc.

- [ ] **Step 4: Implement `exchange.go`** — the SPI, correlator, and `ChannelExchange`:

```go
package msgin

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"time"

	"github.com/jonboulle/clockwork"
)

// defaultReplyTimeout bounds how long Exchange waits for a correlated reply when
// the caller ctx carries no deadline. 30s is comfortably above any plausible
// in-process round-trip while still failing safe (a deadline-less ctx cannot
// hang a waiter — and its registry slot — forever). Override with WithReplyTimeout;
// the effective deadline is always min(ctx deadline, this).
const defaultReplyTimeout = 30 * time.Second

// RequestReplyExchange is the narrow SPI a gateway delegates to: it sends a
// request and returns the correlated reply (or an error). ChannelExchange is the
// in-process implementation; a future HTTP/NATS adapter implements Exchange for
// a real external round-trip, so both gateway façades work over it unchanged.
type RequestReplyExchange interface {
	Exchange(ctx context.Context, req Message[any]) (Message[any], error)
}

// replyCorrelator maps a request's correlation id to a one-shot reply slot and
// demuxes incoming replies back to the blocked waiter. It owns no goroutine: the
// reply receiver runs on the reply channel's driving goroutine; each waiter is
// the caller's own goroutine.
type replyCorrelator struct {
	mu      sync.Mutex
	waiters map[string]chan Message[any]
	closed  bool
}

func newReplyCorrelator() *replyCorrelator {
	return &replyCorrelator{waiters: make(map[string]chan Message[any])}
}

// register inserts a fresh cap-1 slot for id and returns it with a deregister
// func. err is ErrGatewayClosed if the correlator is closed, or
// ErrDuplicateCorrelation if id already has an in-flight waiter (audit G1 — the
// uniqueness the whole design leans on, enforced at the primitive).
//
// Uniqueness is required across the exchange LIFETIME, not just concurrently
// (audit N1): the guard blocks a concurrent duplicate, but a caller that REUSES
// an id sequentially after a prior request gave up (timeout/cancel) can have the
// prior request's genuinely-late reply delivered to the new waiter. The façades
// mint fresh 128-bit ids so they never hit this; direct ChannelExchange callers
// must use unique ids.
//
// deregister is the give-up reconciler (called on ctx/timeout). It returns true
// if it removed the slot (the waiter still owned it → no delivery is in flight),
// or false if the slot was ALREADY gone — claimed by a concurrent deliver
// (a reply is committed to the slot) or closeAll (the slot is closed). On false
// the caller must drain the slot so a delivered-but-abandoned reply is not lost
// (audit G4).
func (c *replyCorrelator) register(id string) (slot chan Message[any], deregister func() bool, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil, nil, ErrGatewayClosed
	}
	if _, exists := c.waiters[id]; exists {
		return nil, nil, ErrDuplicateCorrelation
	}
	slot = make(chan Message[any], 1)
	c.waiters[id] = slot
	deregister = func() bool {
		c.mu.Lock()
		defer c.mu.Unlock()
		if _, ok := c.waiters[id]; ok {
			delete(c.waiters, id)
			return true
		}
		return false
	}
	return slot, deregister, nil
}

// deliver routes reply to the waiter for id, returning true if one matched. The
// slot is removed under the lock before the (non-blocking, cap-1) send, so it can
// never race closeAll onto a closed channel.
func (c *replyCorrelator) deliver(id string, reply Message[any]) bool {
	c.mu.Lock()
	slot, ok := c.waiters[id]
	if ok {
		delete(c.waiters, id)
	}
	c.mu.Unlock()
	if !ok {
		return false
	}
	slot <- reply
	return true
}

// closeAll marks the correlator closed and fails every pending waiter by closing
// its slot (a waiter reading a closed slot observes ErrGatewayClosed). Idempotent.
func (c *replyCorrelator) closeAll() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return
	}
	c.closed = true
	for id, slot := range c.waiters {
		close(slot)
		delete(c.waiters, id)
	}
}

type exchangeConfig struct {
	timeout    time.Duration
	timeoutSet bool
	clock      clockwork.Clock
	logger     *slog.Logger
	unmatched  OutboundAdapter
}

// ExchangeOption configures a ChannelExchange built by NewChannelExchange.
type ExchangeOption func(*exchangeConfig)

// WithReplyTimeout overrides the default 30s reply timeout. The effective
// deadline is min(ctx deadline, this). A non-positive value is ErrInvalidReplyTimeout.
func WithReplyTimeout(d time.Duration) ExchangeOption {
	return func(c *exchangeConfig) { c.timeout, c.timeoutSet = d, true }
}

// WithUnmatchedReplySink routes replies with no pending waiter (already
// timed-out/cancelled, or an unknown correlation id) to out instead of logging
// and dropping them. A sink error is logged, never propagated to the reply sender.
func WithUnmatchedReplySink(out OutboundAdapter) ExchangeOption {
	return func(c *exchangeConfig) { c.unmatched = out }
}

// WithExchangeClock injects the clock used for the reply timeout (tests use a
// clockwork.FakeClock). A nil clock leaves the real-clock default.
func WithExchangeClock(clock clockwork.Clock) ExchangeOption {
	return func(c *exchangeConfig) {
		if clock != nil {
			c.clock = clock
		}
	}
}

// WithExchangeLogger injects the structured logger (default: a discard logger).
func WithExchangeLogger(l *slog.Logger) ExchangeOption {
	return func(c *exchangeConfig) {
		if l != nil {
			c.logger = l
		}
	}
}

// ChannelExchange is the in-process RequestReplyExchange: it sends requests to a
// request channel and correlates replies received on a reply channel (which it
// owns as the sole subscriber). Construct it with NewChannelExchange.
type ChannelExchange struct {
	request   MessageChannel
	corr      *replyCorrelator
	timeout   time.Duration
	clock     clockwork.Clock
	logger    *slog.Logger
	unmatched OutboundAdapter
}

var _ RequestReplyExchange = (*ChannelExchange)(nil)

// NewChannelExchange builds a ChannelExchange over request/reply channels. It
// subscribes its reply receiver onto reply, so reply must be dedicated to this
// exchange (a second subscriber is ErrChannelSubscribed). A nil channel is
// ErrNilChannel; an explicit non-positive WithReplyTimeout is ErrInvalidReplyTimeout.
func NewChannelExchange(request, reply MessageChannel, opts ...ExchangeOption) (*ChannelExchange, error) {
	if request == nil || reply == nil {
		return nil, ErrNilChannel
	}
	cfg := exchangeConfig{
		timeout: defaultReplyTimeout,
		clock:   clockwork.NewRealClock(),
		logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	if cfg.timeoutSet && cfg.timeout <= 0 {
		return nil, ErrInvalidReplyTimeout
	}
	e := &ChannelExchange{
		request:   request,
		corr:      newReplyCorrelator(),
		timeout:   cfg.timeout,
		clock:     cfg.clock,
		logger:    cfg.logger,
		unmatched: cfg.unmatched,
	}
	if err := reply.Subscribe(e.receiver()); err != nil {
		return nil, err
	}
	return e, nil
}

// receiver is the MessageHandler subscribed to the reply channel. It demuxes each
// reply to its waiter by HeaderCorrelationID, or handles an unmatched reply. It
// always returns nil so a slow/absent waiter never fails the reply producer.
func (e *ChannelExchange) receiver() MessageHandler {
	return HandlerFunc(func(ctx context.Context, reply Message[any]) error {
		id, _ := reply.Headers().String(HeaderCorrelationID)
		if e.corr.deliver(id, reply) {
			return nil
		}
		e.routeUnmatched(ctx, reply)
		return nil
	})
}

// routeUnmatched sends an unmatched reply to the configured sink, or warn-logs
// and drops it. A sink error is logged, never propagated (audit G4/§3.5).
func (e *ChannelExchange) routeUnmatched(ctx context.Context, reply Message[any]) {
	if e.unmatched != nil {
		if err := e.unmatched.Send(ctx, reply); err != nil {
			e.logger.Warn("msgin: unmatched-reply sink failed", "id", reply.ID(), "err", err)
		}
		return
	}
	id, _ := reply.Headers().String(HeaderCorrelationID)
	e.logger.Warn("msgin: dropping unmatched gateway reply", "id", reply.ID(), "correlation-id", id)
}

// Exchange sends req to the request channel and blocks for the reply correlated
// by req's HeaderCorrelationID, returning it or ctx.Err()/ErrReplyTimeout/
// ErrGatewayClosed. An empty correlation id is ErrNoCorrelation and a duplicate
// in-flight id is ErrDuplicateCorrelation (audit G1). Both are direct-caller
// guards: the Gateway/OutboundGateway façades always set a fresh non-empty id,
// so they never surface them (audit N3). A request-channel send error propagates
// (waiter deregistered).
func (e *ChannelExchange) Exchange(ctx context.Context, req Message[any]) (Message[any], error) {
	id, _ := req.Headers().String(HeaderCorrelationID)
	if id == "" {
		return Message[any]{}, ErrNoCorrelation
	}
	slot, deregister, err := e.corr.register(id)
	if err != nil {
		return Message[any]{}, err // ErrGatewayClosed | ErrDuplicateCorrelation
	}
	if err := e.request.Send(ctx, req); err != nil {
		e.giveUp(ctx, slot, deregister)
		return Message[any]{}, err
	}
	timer := e.clock.NewTimer(e.timeout)
	defer timer.Stop()
	select {
	case reply, open := <-slot:
		if !open {
			return Message[any]{}, ErrGatewayClosed // closeAll closed our slot
		}
		return reply, nil
	case <-ctx.Done():
		e.giveUp(ctx, slot, deregister)
		return Message[any]{}, ctx.Err()
	case <-timer.Chan():
		e.giveUp(ctx, slot, deregister)
		return Message[any]{}, ErrReplyTimeout
	}
}

// giveUp reconciles a waiter that is abandoning its slot (send error, ctx,
// timeout) with a possibly-concurrent deliver. If deregister removed the slot,
// no reply was in flight and we are done. Otherwise a deliver already claimed
// the slot and is committed to sending (or closeAll closed it): we block on the
// slot and route any delivered reply to the unmatched path rather than dropping
// it silently (audit G4). context.WithoutCancel is used so the sink send is not
// itself cancelled by the ctx that just fired.
func (e *ChannelExchange) giveUp(ctx context.Context, slot chan Message[any], deregister func() bool) {
	if deregister() {
		return
	}
	if reply, ok := <-slot; ok {
		e.routeUnmatched(context.WithoutCancel(ctx), reply)
	}
}

// Close stops the exchange: subsequent Exchange calls return ErrGatewayClosed and
// every waiter pending at Close time is failed with it. Idempotent. The reply
// receiver remains subscribed (channels have no unsubscribe); it simply finds no
// waiters after Close. Close returns nil today; the signature allows a future
// adapter-backed exchange to report a teardown error.
func (e *ChannelExchange) Close() error {
	e.corr.closeAll()
	return nil
}
```

- [ ] **Step 5: Run to verify it passes.** `GOTOOLCHAIN=go1.25.12 go test . -run 'ChannelExchange|NewChannelExchange' -race` → PASS. Then `GOTOOLCHAIN=go1.25.12 go test . -run 'ChannelExchange|NewChannelExchange' -cover` and `go vet ./...`.

- [ ] **Step 6: Commit** (design docs — spec 010 / ADR 0022 / this plan — ride in this first task, per CLAUDE.md couple-with-code):

```bash
git add exchange.go exchange_test.go errors.go docs/specs/010-messaging-gateway.md docs/adrs/0022-messaging-gateway.md docs/plans/019-messaging-gateway.md
git commit -m "$(cat <<'EOF'
feat(core): RequestReplyExchange SPI + ChannelExchange + reply correlator

The in-process request-reply mechanism the Messaging Gateway builds on: a
zero-goroutine reply correlator, the RequestReplyExchange SPI, and the
ChannelExchange implementation with 30s-default reply timeout, graceful
Close, and warn-log/opt-in-sink unmatched-reply handling.

Spec: 010
Plan: 019
ADR: 0022
Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01Ret7sFbL49k5Wjav9M4hZC
EOF
)"
```

**Hot-path branches covered:** nil request; nil reply; explicit invalid timeout; round-trip reply (sync); **async round-trip (cross-goroutine deliver)**; **N concurrent Requests under `-race`**; reply timeout (fake clock); **timeout-races-delivery + giveUp drain to sink** (`giveUp` `ok==true` arm); **close-races-giveUp** (`giveUp` `ok==false` arm, audit N2); ctx cancel; request send error + no waiter leak; closed (new + pending); unmatched drop; unmatched sink; **empty correlation id → `ErrNoCorrelation`**; **duplicate in-flight id → `ErrDuplicateCorrelation`**; `WithExchangeClock(nil)`/`WithExchangeLogger(nil)` no-op guards.

---

### Task 2: Inbound `Gateway[Req,Rep]` (`NewGateway` + `Request`)

**Files:** Create `gateway.go`, `gateway_test.go`.

**Interfaces:**
- Consumes: `RequestReplyExchange` (Task 1), `New[Req]`, `WithID`, `HeaderCorrelationID`, `boxMessage`, `PayloadOf[Rep]`/`ErrPayloadType`, `randomID` (via a fresh `New` id — see note), `ErrNilExchange`.
- Produces: `type Gateway[Req, Rep any] struct{…}`; `func NewGateway[Req, Rep any](x RequestReplyExchange, opts ...GatewayOption[Req,Rep]) (*Gateway[Req,Rep], error)`; `func (*Gateway[Req,Rep]) Request(ctx, Req) (Rep, error)`; `type GatewayOption[Req, Rep any] func(*gatewayConfig)`.

- [ ] **Step 1: Write the failing test** (`gateway_test.go`). Use a lightweight fake exchange (idiomatic for a one-method SPI; no mockgen needed):

```go
type fakeExchange struct {
	fn func(ctx context.Context, req msgin.Message[any]) (msgin.Message[any], error)
}

func (f fakeExchange) Exchange(ctx context.Context, req msgin.Message[any]) (msgin.Message[any], error) {
	return f.fn(ctx, req)
}
```

Cases (assert-closure table + focused tests):
- `nil exchange` → `NewGateway` returns `ErrNilExchange`.
- `happy path` — a fake that asserts the request payload is the `Req` value and that the request carries a **non-empty** `HeaderCorrelationID`, then returns a `Rep`-payload reply built in the blackbox test as `msgin.New[any](repValue)` (a `Message[any]` whose payload is the `Rep` — `boxMessage` is unexported, so `New[any]` is the blackbox way to build one); assert `Request` returns that `Rep`.
- `each request gets a fresh correlation id` — call `Request` twice; the fake records the two ids; assert they differ and are non-empty.
- `wrong reply type` — fake returns a reply whose payload is not `Rep`; assert `errors.Is(err, msgin.ErrPayloadType)`.
- `exchange error propagates` — fake returns `context.DeadlineExceeded`; assert `Request` returns it (wrapped or `Is`).
- Also an **integration** test wiring `NewGateway` over a real `ChannelExchange` + echo flow (from Task 1's helper), asserting a real round-trip.

- [ ] **Step 2: Run to verify it fails.** `GOTOOLCHAIN=go1.25.12 go test . -run 'TestGateway|TestNewGateway'` → `undefined: msgin.NewGateway`.

- [ ] **Step 3: Implement `gateway.go`** (the inbound façade; `OutboundGateway` lands in Task 3):

```go
package msgin

import "context"

// Gateway is the inbound EIP Messaging Gateway: a typed, application-facing
// request-reply bridge into a message flow. Request turns a Req into a Message,
// drives it through a RequestReplyExchange, and returns the correlated Rep reply
// (or an error/timeout) — hiding the messaging from the caller. Build it with
// NewGateway.
type Gateway[Req, Rep any] struct {
	exchange RequestReplyExchange
}

type gatewayConfig struct{}

// GatewayOption configures a Gateway built by NewGateway. Reserved for future
// options (e.g. request-header seeding); none are defined yet.
type GatewayOption[Req, Rep any] func(*gatewayConfig)

// NewGateway builds an inbound Gateway over x. A nil exchange is ErrNilExchange.
func NewGateway[Req, Rep any](x RequestReplyExchange, opts ...GatewayOption[Req, Rep]) (*Gateway[Req, Rep], error) {
	if x == nil {
		return nil, ErrNilExchange
	}
	var cfg gatewayConfig
	for _, opt := range opts {
		opt(&cfg)
	}
	return &Gateway[Req, Rep]{exchange: x}, nil
}

// Request sends req into the flow and blocks for the correlated reply, returning
// it as a Rep. It always mints a fresh correlation id (the caller passes a raw
// Req with no headers), so concurrent requests never collide. A reply whose
// payload is not a Rep yields ErrPayloadType; ctx cancellation, ErrReplyTimeout,
// and ErrGatewayClosed propagate from the exchange.
func (g *Gateway[Req, Rep]) Request(ctx context.Context, req Req) (Rep, error) {
	var zero Rep
	msg := New(req).WithHeader(HeaderCorrelationID, randomID())
	reply, err := g.exchange.Exchange(ctx, boxMessage(msg))
	if err != nil {
		return zero, err
	}
	out, err := PayloadOf[Rep](reply)
	if err != nil {
		return zero, err
	}
	return out.Payload(), nil
}
```

Note: `New(req)` stamps a fresh `HeaderMessageID`; `WithHeader(HeaderCorrelationID, randomID())` adds a distinct correlation id (kept separate from the message id so a caller-visible id and the reply-correlation key are not conflated). `randomID()` is the existing unexported generator in `message.go`.

- [ ] **Step 4: Run to verify it passes.** `GOTOOLCHAIN=go1.25.12 go test . -run 'TestGateway|TestNewGateway' -race` → PASS. Then `-cover` + `go vet ./...`.

- [ ] **Step 5: Commit.**

```bash
git add gateway.go gateway_test.go
git commit -m "$(cat <<'EOF'
feat(core): inbound Gateway[Req,Rep] request-reply façade

NewGateway/Request: the typed application-facing Messaging Gateway over a
RequestReplyExchange — box Req, mint a fresh correlation id, unbox the reply
to Rep (ErrPayloadType on mismatch).

Spec: 010
Plan: 019
ADR: 0022
Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01Ret7sFbL49k5Wjav9M4hZC
EOF
)"
```

**Hot-path branches covered:** nil exchange; happy round-trip; fresh-id-per-request; wrong reply type → `ErrPayloadType`; exchange error propagation.

---

### Task 3: `Message.WithoutHeader` + outbound `OutboundGateway` Step

**Files:** Modify `message.go`, `gateway.go`, `doc_composition.go`; create/extend `message_test.go`; extend `gateway_test.go`.

**Interfaces:**
- Consumes: `Headers` internals (`maps.Clone`), `RequestReplyExchange`, `Step`/`MessageHandler`/`HandlerFunc`, `HeaderCorrelationID`, `randomID`.
- Produces: `func (Message[T]) WithoutHeader(key string) Message[T]`; `func (Headers) without(key string) Headers` (unexported); `func OutboundGateway(x RequestReplyExchange) Step`.

- [ ] **Step 1: Write the failing test for `WithoutHeader`** (`message_test.go`, assert-closure table). Cases: removing a present key yields a message without it (`_, ok := m2.Header(k); ok == false`) while the receiver is unchanged (copy-on-write — original still has it); removing an absent key is a no-op equal-value copy; removing from an empty header set is safe.

```go
func TestMessageWithoutHeader(t *testing.T) {
	tests := []struct {
		name   string
		build  func() msgin.Message[int]
		key    string
		assert func(t *testing.T, orig, got msgin.Message[int])
	}{
		{
			name:  "removes present key, leaves original",
			build: func() msgin.Message[int] { return msgin.New(1, msgin.WithHeaders(map[string]any{"k": "v"})) },
			key:   "k",
			assert: func(t *testing.T, orig, got msgin.Message[int]) {
				if _, ok := got.Header("k"); ok {
					t.Fatal("want k removed from result")
				}
				if _, ok := orig.Header("k"); !ok {
					t.Fatal("want k retained on original (copy-on-write)")
				}
			},
		},
		// ... absent-key no-op; empty-headers safe
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			orig := tt.build()
			tt.assert(t, orig, orig.WithoutHeader(tt.key))
		})
	}
}
```

- [ ] **Step 2: Run to verify it fails.** `GOTOOLCHAIN=go1.25.12 go test . -run TestMessageWithoutHeader` → `undefined: (msgin.Message[int]).WithoutHeader`.

- [ ] **Step 3: Implement `WithoutHeader` + `without`** (`message.go`, next to `WithHeader`/`with`):

```go
// without returns a copy with key removed (copy-on-write; used by
// Message.WithoutHeader). Removing an absent key returns an equivalent copy.
func (h Headers) without(key string) Headers {
	if _, ok := h.m[key]; !ok {
		return Headers{m: maps.Clone(h.m)}
	}
	nm := maps.Clone(h.m)
	delete(nm, key)
	return Headers{m: nm}
}

// WithoutHeader returns a copy of the message with key removed from its headers,
// leaving the receiver unchanged (copy-on-write). Removing an absent key is a
// no-op copy. Used by OutboundGateway to strip a transient correlation id when
// the inbound message carried none.
func (m Message[T]) WithoutHeader(key string) Message[T] {
	return Message[T]{payload: m.payload, headers: m.headers.without(key)}
}
```

- [ ] **Step 4: Run to verify it passes.** `GOTOOLCHAIN=go1.25.12 go test . -run TestMessageWithoutHeader -race` → PASS.

- [ ] **Step 5: Write the failing test for `OutboundGateway`** (`gateway_test.go`). Drive the `Step` through a tiny chain: `Chain(OutboundGateway(fake), To(collector))` where `collector` captures the forwarded reply. Cases:
- `forwards reply downstream` — fake returns a reply payload; assert `collector` received it.
- `restores a pre-existing correlation id` — incoming message has `HeaderCorrelationID = "G"`; the fake asserts the request it saw carried a **fresh** id `!= "G"`; assert the forwarded reply carries `"G"` again.
- `strips correlation id when the incoming had none` — incoming has no correlation id; the fake asserts the request carried a non-empty fresh id; assert the forwarded reply has **no** `HeaderCorrelationID`.
- `restores a present non-string correlation id` (audit G5) — incoming carries `HeaderCorrelationID = 42` (an `int`); assert the fake saw a **string** fresh id, and the forwarded reply carries the original `42` back (proves raw-presence via `Header`, not the `String`-conflating check that would have wrongly stripped it).
- `unique keys for split-children sharing G` — two messages both with `HeaderCorrelationID = "G"` produce two **distinct** request ids at the fake (fold into the table by recording ids).
- `exchange error propagates` — fake returns an error; assert `Handle` returns it and `collector` got nothing.

- [ ] **Step 6: Run to verify it fails.** `GOTOOLCHAIN=go1.25.12 go test . -run TestOutboundGateway` → `undefined: msgin.OutboundGateway`.

- [ ] **Step 7: Implement `OutboundGateway`** (`gateway.go`):

```go
// OutboundGateway is the in-flow EIP outbound gateway: a Step that performs a
// request-reply exchange on the in-flight message and forwards the reply to next.
// It reuses HeaderCorrelationID as the reply key, so it mints a FRESH id for its
// own exchange and RESTORES the incoming correlation state on the reply — if the
// message arrived carrying an id it is put back (so an upstream splitter/aggregator
// group key survives the round-trip); if it arrived with none, the transient id is
// stripped. The fresh id guarantees a unique registry key even when the entering
// messages share a correlation id (e.g. all children of one split). An Exchange
// error propagates to the driving Consumer (retry/dead-letter owns it).
func OutboundGateway(x RequestReplyExchange) Step {
	return func(next MessageHandler) MessageHandler {
		return HandlerFunc(func(ctx context.Context, msg Message[any]) error {
			savedVal, had := msg.Header(HeaderCorrelationID) // raw presence — NOT Headers().String (audit G5)
			reply, err := x.Exchange(ctx, msg.WithHeader(HeaderCorrelationID, randomID()))
			if err != nil {
				return err
			}
			if had {
				reply = reply.WithHeader(HeaderCorrelationID, savedVal)
			} else {
				reply = reply.WithoutHeader(HeaderCorrelationID)
			}
			return next.Handle(ctx, reply)
		})
	}
}
```

- [ ] **Step 8: Run to verify it passes.** `GOTOOLCHAIN=go1.25.12 go test . -run 'TestOutboundGateway|TestMessageWithoutHeader' -race` → PASS. Then `-cover` + `go vet ./...`.

- [ ] **Step 9: Add one paragraph to `doc_composition.go`** naming the gateway endpoints (inbound `Gateway`/`Request`, `OutboundGateway`) as the request-reply members of the composition set, mentioning the shared `RequestReplyExchange`.

- [ ] **Step 10: Commit.**

```bash
git add message.go message_test.go gateway.go gateway_test.go doc_composition.go
git commit -m "$(cat <<'EOF'
feat(core): OutboundGateway Step + Message.WithoutHeader

The in-flow outbound gateway: run a RequestReplyExchange mid-flow and forward
the reply, saving/restoring the incoming HeaderCorrelationID (stripping it when
absent, via the new additive Message.WithoutHeader) so an upstream split/
aggregate group key survives the round-trip and concurrent exchanges get
unique registry keys.

Spec: 010
Plan: 019
ADR: 0022
Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01Ret7sFbL49k5Wjav9M4hZC
EOF
)"
```

**Hot-path branches covered:** `WithoutHeader` present/absent/empty; outbound forward; restore-present-id; strip-absent-id; unique-keys-for-shared-G; exchange error propagation.

---

### Task 4: `Example` tests, package doc, and the whole-branch delivery gate

**Files:** Extend `gateway_test.go` (Examples); confirm `doc.go`/package doc; no production code.

- [ ] **Step 1: Write `Example` tests** (they double as godoc). `ExampleGateway_Request` — wire `request := NewDirectChannel()`, a flow `Chain(Activate(func(ctx, m Message[int]) (Message[int], error) { return WithPayload(m, m.Payload()*2), nil }), To(reply))`, `request.Subscribe(flow)`, `ex, _ := NewChannelExchange(request, reply)`, `gw, _ := NewGateway[int,int](ex)`, print `gw.Request(ctx, 21)` → `// Output: 42`. **The activator MUST use `WithPayload` (not `New`) so the reply preserves `HeaderCorrelationID`** — otherwise the reply is unmatched and `Request` blocks on the real 30s timeout and the example fails (audit G6). `ExampleOutboundGateway` — a flow containing `OutboundGateway` whose exchange is a small in-process echo, showing the reply forwarded downstream. Keep outputs deterministic (no timestamps/ids printed). For the non-Example unit tests that risk a mis-wire hang, inject `WithExchangeClock(clockwork.NewFakeClock())` so a dropped-correlation-id bug fails fast under `Advance` rather than blocking 30s wall-clock.

- [ ] **Step 2: Run examples.** `GOTOOLCHAIN=go1.25.12 go test . -run '^Example' -v` → PASS (output matches).

- [ ] **Step 3: Full package suite, race + leak.** `GOTOOLCHAIN=go1.25.12 go test ./... -race` → PASS (goleak clean).

- [ ] **Step 4: Coverage.** `GOTOOLCHAIN=go1.25.12 go test . -coverprofile=/tmp/gw.cov && go tool cover -func=/tmp/gw.cov | tail -1` → confirm ≥ 85% on the core package; inspect `exchange.go`/`gateway.go` lines for any uncovered branch and add a case if so.

- [ ] **Step 5: Lint / fmt / vet / vuln / cgo / module hygiene.**

```bash
GOTOOLCHAIN=go1.25.12 go vet ./...
gofmt -l . ; gofumpt -l .            # expect no output
golangci-lint run ./...
govulncheck ./...
CGO_ENABLED=0 GOTOOLCHAIN=go1.25.12 go build ./...
GOTOOLCHAIN=go1.25.12 go mod tidy && git diff --exit-code go.mod go.sum   # no new dependency
GOTOOLCHAIN=go1.25.12 go mod verify
```

- [ ] **Step 6: Whole-branch review gate** (CLAUDE.md §5, over `main..HEAD`). Run `/code-review` and `/security-review` on the branch diff; resolve or triage every finding (re-run the affected review + `-race` after fixes). Confirm the API is additions-only (`apidiff`/manual) → minor SemVer.

- [ ] **Step 7: Commit** any gate fixes / examples.

```bash
git add gateway_test.go
git commit -m "$(cat <<'EOF'
test(core): gateway Example tests + whole-branch gate

Runnable Example tests for Gateway.Request and OutboundGateway (godoc), and
the whole-branch quality gate (race/leak/lint/vet/govulncheck/CGO0/tidy).

Spec: 010
Plan: 019
ADR: 0022
Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01Ret7sFbL49k5Wjav9M4hZC
EOF
)"
```

---

## Self-review notes (author)

- **Spec coverage:** Task 1 → Spec §3.1/3.2/3.3/3.5/3.6 + §6.1/6.2; Task 2 → §3.4 (inbound) + §4.2; Task 3 → §3.4 (outbound save/restore) + §4.3 + §8 (`WithoutHeader`); Task 4 → §5 (examples/gate). Non-goals (one-way, async, external adapter, `RequestMessage`, `NewChannelGateway`) intentionally absent.
- **Deviation from Spec §6:** the spec listed the correlator as a separate phase; the plan merges it into Task 1 because it is unexported and can only be exercised through the exported `ChannelExchange` (blackbox-only rule). Same coverage, one fewer commit.
- **Open items** (Spec §8) resolved here: no `NewChannelGateway` sugar (kept explicit two-step); Step name is `OutboundGateway`; no `Gateway.Close` (the exchange owns lifecycle); `WithoutHeader` shape confirmed (`Message.WithoutHeader` / `Headers.without`).
- **Adversarial audit round 1 folded** (Opus, NEEDS-REVISION → fixes applied): **G1** correlator guards duplicate in-flight ids (`ErrDuplicateCorrelation`) + rejects empty id (`ErrNoCorrelation`), with covering tests (Task 1); **G2** added async cross-goroutine + N-concurrent `-race` + timeout-races-delivery tests (Task 1) — the previously-untested hot path the primitive exists for; **G3** spec/ADR async-wiring language corrected (reply channel is `DirectChannel`-only in core; async = who calls `reply.Send`) + a real async Example; **G4** `giveUp` drains a delivered-but-abandoned reply to the unmatched path instead of dropping it silently (also resolves **G7** — a sync flow that replies then errors now routes the reply to unmatched, not the void); **G5** `OutboundGateway` uses raw `Header` presence (not `String`) so a present non-string correlation id is preserved, with a covering test; **G6** the inbound Example's activator must use `WithPayload` + tests inject a fake clock to fail fast; **G8** nits (nil-option guard tests, `message_test.go` precision, Task 2 reply built via `New[any]`). Re-audit of the changed correlator/`giveUp` contract requested before implementation.
- **Adversarial audit round 2 (re-audit): SOUND-WITH-NITS, no must-fix** — all five G-fixes CONFIRMED-FIXED; the `giveUp` blocking-drain proven leak-free across every deliver/closeAll/timeout interleaving. Folded: **N1** (correlation-id uniqueness is a lifetime, not just concurrent, contract — `register`/`Exchange` godoc); **N2** (added `TestChannelExchange_closeRacesGiveUp` covering the `giveUp` `ok==false` defensive arm — Task 1); **N3** (godoc: the empty/duplicate guards are direct-caller guards the façades never trip). Design gate cleared.
