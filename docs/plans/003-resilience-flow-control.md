# Resilience & Flow Control — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add the spec §7.4 flow-control governors on top of Plan 002's settlement engine for the **streaming-source** path: a mandatory credit gate (`WithMaxInFlight`), an optional rate limiter, per-handler timeout, an ingress+dispatch circuit breaker, an overflow policy, and a bounded (TTL-swept) attempt tracker — so a message flood can never over-pull, leak credit, stall, or wedge the flow.

**Architecture:** A new **ingress goroutine** sits between the source's `Stream` output and the worker pool. Per delivery it applies, in composition order, rate-limit → circuit-breaker admit → **credit acquire** (or overflow) → forward. Credit is a size-`n` counting semaphore (buffered `chan struct{}`); each admitted delivery carries a `sync.OnceFunc` release that its **wrapped** `Ack`/`Nack` fire **release-first** (before the possibly-blocking, possibly-re-injecting settle), so a Nacked message never pins its slot (NF-5) and never deadlocks at `n=1`. The worker also `defer`s the release as a panic-safe net. Handler timeout and the breaker's half-open wakeup are `clockwork`-driven; the breaker's ingress park uses a subscribe-then-recheck channel to be missed-wakeup-free. The attempt tracker gains a `clockwork` TTL sweep that reclaims idle ids without ever evicting one still in redelivery (NF-2).

**Tech Stack:** Go 1.25, `github.com/jonboulle/clockwork` (injectable time — already a core dep), `log/slog` (stdlib), `sync` (`OnceFunc`, semaphore-as-channel), `testing/synctest` (deterministic goroutine-park observation — stable in 1.25), `go.uber.org/goleak` + `github.com/stretchr/testify` (test-only). **No new forced dependency** — the token bucket and circuit breaker are first-party, dependency-free defaults; `golang.org/x/time/rate` and `sony/gobreaker` remain optional plug-ins behind interfaces.

**Traceability:** Implements [spec 001](../specs/001-messaging-core.md) §7.4 (credit/rate/timeout/breaker/overflow) and refines §7's `finish` into the credit-release chokepoint; realizes [ADR 0006](../adrs/0006-resilience-flow-control.md) (resilience decisions) and **fixes the "attemptTracker unbounded growth" limitation in [ADR 0007](../adrs/0007-reliability-settlement-api.md) §D5**. Introduces the **new [ADR 0008](../adrs/0008-resilience-flow-control-api.md)** (flow-control API: credit model, RateLimiter/CircuitBreaker interfaces, overflow semantics, bounded-tracker design — authored/extended within this plan's commits, full outline at the end). Builds on [plan 002](002-reliability.md).

## Global Constraints

- **Go 1.25 exact.** `go.mod` has `go 1.25`; build/test with `GOTOOLCHAIN=go1.25.0`. No features newer than 1.25. (`sync.OnceFunc`, `testing/synctest`, `context.WithoutCancel` are all ≤1.25.)
- **Core = stdlib + `clockwork` only.** No other third-party import in non-test code. The token bucket and breaker are hand-written; `x/time/rate`/`sony/gobreaker` are plug-ins behind interfaces, never imported. Any forced dep needs a NEW ADR (recommend against).
- **Blackbox tests only.** Every `_test.go` is `package msgin_test` (or `<pkg>_test`) and exercises the exported API. Export any sentinel a test must `errors.Is`. Unexported machinery (`creditGate`, `managedDelivery`, the ingress loop, the default breaker/bucket, the swept `attemptTracker`) is tested **through** `Run`/`NewConsumer` and the public options, observing settle recorders, hooks, and settle delays.
- **Table tests use the assert-closure form** (project `table-test` skill): each case carries `assert func(t *testing.T, …)`, never `want`/`wantErr` fields. Use `t.Context()`, not `context.Background()`.
- **Every consumer/`Run` test asserts no goroutine leaks** with `goleak` (`TestMain` already lives in `consumer_test.go`) — including on the breaker-open, overflow-drop, handler-timeout, and TTL-sweep paths.
- **Injectable time via `clockwork.Clock`** used directly; tests use `clockwork.NewFakeClock()` + `clk.BlockUntil(n)` for clock-parks and `synctest.Test`/`synctest.Wait` for clock-less goroutine-parks (credit/breaker ingress). **No real `time.Sleep` gating.**
- **Library, no `os.Exit`/`panic`/`log.Fatal` on caller input; no global logger.** Options validate to typed errors at construction.
- **Coverage gate (mandatory).** Every hot-path/typed-error branch enumerated per task has ≥1 covering case; target ≥85% on `package msgin`. The credit-accounting branches (acquire/tryAcquire/release-once, overflow drop, breaker gate, shutdown-abandon) are the hard-required hot path.
- **Module path:** `github.com/kartaladev/msgin`. Package `msgin` at repo root; memory adapter at `adapter/memory`.
- **Branch:** `feat/resilience` (already checked out off `main`). Per-task commits are pre-authorized once this plan is approved and an SDD/executing-plans mode is chosen; `push`/merge/branch-deletion still need explicit approval.

---

## File Structure

New files (all `package msgin` at repo root):
- `flowcontrol.go` — `RateLimiter` interface, `CircuitBreaker` interface, `OverflowPolicy` enum + `String()`, the flow-control `defaultMaxInFlight`/`defaultAttemptTTL` constants, and the `WithMaxInFlight`/`WithRateLimit`/`WithHandlerTimeout`/`WithCircuitBreaker`/`WithOverflow` options. (The public flow-control surface.)
- `credit.go` — unexported `creditGate` (semaphore-as-channel) + `managedDelivery` + `manage` (release-first wrapping); the ingress loop (`ingest`/`admit`).
- `ratelimit.go` — `NewTokenBucket(rps, burst, opts...)` — the dependency-free `clockwork`-driven default `RateLimiter`.
- `breaker.go` — `NewCircuitBreaker(opts...)` — the dependency-free `clockwork`-driven default `CircuitBreaker` (closed/open/half-open, explicit half-open wakeup).

Modified files:
- `consumer.go` — new `consumerConfig`/`consumer` fields; `NewConsumer` defaults+validation; `Run` rewritten to add the ingress stage while preserving the C1 bounded-shutdown drain; `process` (worker body) with the credit-release net + breaker dispatch-gate; `dispatch` gains an `error` return (Task 5) and the handler-timeout context (Task 4).
- `retry.go` — `attemptTracker` gains a `clockwork` clock, per-entry `lastSeen`, and a TTL sweep (`sweep`/`sweepLoop`); `newAttemptTracker(clock, ttl)`.
- `errors.go` — add `ErrInvalidMaxInFlight`, `ErrInvalidRateLimit`, `ErrOverflowDropped`.
- `adapter/memory/memory.go` — no change (reference source); its at-most-once loss-on-drop is documented in ADR 0008.

New test files (all `package msgin_test`):
- `flowcontrol_test.go`, `ratelimit_test.go`, `breaker_test.go` — unit + through-`Run` tests.
- `settlement_doubles_test.go` — **extended** with `countingSource`, `controllableSource`, and a `blockingSink`/`recordingBreaker` where needed (the Plan 002 doubles stay).
- `consumer_test.go` — appended (credit bound, Nack-releases-slot, handler-timeout, shutdown interplay).

Docs (ride with the code per CLAUDE.md — no standalone ADR commit):
- `docs/adrs/0008-resilience-flow-control-api.md` — created in Task 1 (Status Accepted; D1–D2 full, D3–D9 stubs), extended in the task that realizes each decision.
- `docs/adrs/0007-reliability-settlement-api.md` — the §D5 "Known limitation" block is updated **in Task 7's commit** to point at its resolution (status: Resolved by Plan 003 / ADR 0008 D8).

---

### Task 1: Flow-control public surface + interfaces + errors + ADR 0008 skeleton

**Files:**
- Create: `flowcontrol.go`
- Modify: `errors.go` (append three sentinels)
- Modify: `consumer.go` (`consumerConfig` + `consumer` fields; defaults + validation in `NewConsumer`; behavior otherwise UNCHANGED)
- Create: `docs/adrs/0008-resilience-flow-control-api.md` (Status: Accepted; D1–D2 full, D3–D9 stubs)
- Test: `flowcontrol_test.go`, `consumer_test.go` (append)

**Interfaces:**
- Consumes: `ConsumerOption[T]`, `consumerConfig[T]`, `OutboundAdapter` (Plan 001/002).
- Produces:
  - `type RateLimiter interface { Wait(ctx context.Context) error }`
  - `type CircuitBreaker interface { Allow() bool; Record(success bool); HalfOpen() <-chan struct{} }`
  - `type OverflowPolicy int` with `OverflowBlock` (0, default), `OverflowDropNewest`, `OverflowDropOldest`, `OverflowReject`, and a `String()`.
  - `func WithMaxInFlight[T any](n int) ConsumerOption[T]`
  - `func WithRateLimit[T any](r RateLimiter) ConsumerOption[T]`
  - `func WithHandlerTimeout[T any](d time.Duration) ConsumerOption[T]`
  - `func WithCircuitBreaker[T any](b CircuitBreaker) ConsumerOption[T]`
  - `func WithOverflow[T any](p OverflowPolicy) ConsumerOption[T]`
  - `const defaultMaxInFlight = 1024`; `const defaultAttemptTTL = 5 * time.Minute`
  - sentinels `ErrInvalidMaxInFlight`, `ErrInvalidRateLimit`, `ErrOverflowDropped`.

**Design note (ADR 0008 D1/D2).** The whole surface is added here and **validated** at construction, but `Run`'s behavior is unchanged (options are stored, wired in Tasks 2–6) — mirroring Plan 002's Task 4 "widen then wire" pattern so the suite stays green.

**Distinguish unset from explicit-0 (C2).** `defaultMaxInFlight` (1024) is non-zero, so a bare zero cannot double as the "unset" sentinel — an explicit `WithMaxInFlight(0)` is a *caller error* that must be rejected, not silently defaulted. So `consumerConfig` carries a **`maxInFlightSet bool`** (mirroring the existing `codecSet` pattern): `WithMaxInFlight(n)` sets **both** `maxInFlight = n` and `maxInFlightSet = true`. `NewConsumer` then resolves "**not** set → `defaultMaxInFlight`" and validates "**set** → require `n >= 1`, else `ErrInvalidMaxInFlight`" — so `WithMaxInFlight(0)` reaches the validation gate and returns the typed error (the Step-1 case below actually fails without the flag: `0` would be indistinguishable from unset and get defaulted).

Defaults: `maxInFlight` unset → `defaultMaxInFlight` (the credit gate is **always active**, never unbounded — spec §7.4.1); `rateLimiter = nil` (no limiting); `handlerTimeout = 0` (no timeout); `breaker = nil` (always allow); `overflow = OverflowBlock`. `OverflowPolicy` out of range → treated as `OverflowBlock` (no error; the enum is closed and `String()` renders unknowns as `"OverflowBlock"`). A nil `RateLimiter`/`CircuitBreaker` passed to the option is a no-op (option guards `if r != nil`), so `WithRateLimit(nil)` cannot install a nil that would panic later.

**Set-flag scope (C2).** The set-flag pattern is applied **only** where zero is a genuinely ambiguous, *meaningful* value: that is `WithMaxInFlight` alone among the new options (default 1024 ≠ 0, and 0 is a distinct invalid input). The others have no conflation and need no flag — `WithRateLimit(nil)`/`WithCircuitBreaker(nil)` treat nil as an unambiguous no-op (no error case), `WithHandlerTimeout(0)` and the unset default are both "disabled" (identical behavior), and `WithOverflow(OverflowBlock)` equals the zero-value default. (`WithConcurrency`, Plan 002, already detects explicit 0 via its non-zero `cfg.concurrency = 1` initializer → `< 1` → `ErrInvalidConcurrency`.)

**Hot-path/typed-error branches introduced:** `WithMaxInFlight(0)` **set** → `ErrInvalidMaxInFlight`; `WithMaxInFlight(n<0)` set → `ErrInvalidMaxInFlight`; `n>=1` set → accepted; **unset** (`maxInFlightSet==false`) → `defaultMaxInFlight`; `WithRateLimit(nil)`/`WithCircuitBreaker(nil)` → no-op (field stays nil); `OverflowPolicy.String()` known vs unknown.

- [ ] **Step 1: Write the failing test**

```go
// flowcontrol_test.go
package msgin_test

import (
	"context"
	"testing"
	"time"

	"github.com/kartaladev/msgin"
	"github.com/kartaladev/msgin/adapter/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewConsumer_FlowControlValidation(t *testing.T) {
	h := func(context.Context, msgin.Message[order]) error { return nil }

	tests := []struct {
		name   string
		opts   []msgin.ConsumerOption[order]
		assert func(t *testing.T, err error)
	}{
		{"max-in-flight below 1 is rejected",
			[]msgin.ConsumerOption[order]{msgin.WithMaxInFlight[order](0)},
			func(t *testing.T, err error) { assert.ErrorIs(t, err, msgin.ErrInvalidMaxInFlight) }},
		{"negative max-in-flight is rejected",
			[]msgin.ConsumerOption[order]{msgin.WithMaxInFlight[order](-4)},
			func(t *testing.T, err error) { assert.ErrorIs(t, err, msgin.ErrInvalidMaxInFlight) }},
		{"all flow-control options construct cleanly",
			[]msgin.ConsumerOption[order]{
				msgin.WithMaxInFlight[order](8),
				msgin.WithRateLimit[order](nil),        // nil is a no-op, must not panic later
				msgin.WithCircuitBreaker[order](nil),   // nil is a no-op
				msgin.WithHandlerTimeout[order](time.Second),
				msgin.WithOverflow[order](msgin.OverflowDropNewest),
			},
			func(t *testing.T, err error) { assert.NoError(t, err) }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := msgin.NewConsumer[order](memory.New(), h, tc.opts...)
			tc.assert(t, err)
		})
	}
}

func TestOverflowPolicy_String(t *testing.T) {
	tests := []struct {
		name   string
		policy msgin.OverflowPolicy
		want   string
	}{
		{"block", msgin.OverflowBlock, "OverflowBlock"},
		{"drop newest", msgin.OverflowDropNewest, "OverflowDropNewest"},
		{"drop oldest", msgin.OverflowDropOldest, "OverflowDropOldest"},
		{"reject", msgin.OverflowReject, "OverflowReject"},
		{"unknown renders as block", msgin.OverflowPolicy(99), "OverflowBlock"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, tc.policy.String())
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `GOTOOLCHAIN=go1.25.0 go test ./... -run 'TestNewConsumer_FlowControlValidation|TestOverflowPolicy_String' -v`
Expected: FAIL — `undefined: msgin.WithMaxInFlight` / `msgin.OverflowPolicy`.

- [ ] **Step 3: Write minimal implementation**

```go
// flowcontrol.go
package msgin

import (
	"context"
	"time"
)

// RateLimiter paces ingress. Wait blocks until the caller may proceed or ctx is
// done. The default is a dependency-free clockwork token bucket (NewTokenBucket);
// golang.org/x/time/rate.Limiter satisfies this interface directly.
type RateLimiter interface {
	Wait(ctx context.Context) error
}

// CircuitBreaker gates dispatch on downstream health (spec §7.4.5, NF-10).
//
//   - Allow reports whether work may proceed now (closed or half-open).
//   - Record feeds the outcome of an allowed dispatch back to the breaker.
//   - HalfOpen returns a channel that is closed when the breaker next
//     transitions open→half-open, so a parked ingress goroutine wakes
//     explicitly (no missed-wakeup stuck-open — the caller re-checks Allow
//     AFTER subscribing). A fresh channel is minted for each open cycle.
//
// The default is a dependency-free clockwork breaker (NewCircuitBreaker);
// sony/gobreaker can be adapted via its OnStateChange callback.
type CircuitBreaker interface {
	Allow() bool
	Record(success bool)
	HalfOpen() <-chan struct{}
}

// OverflowPolicy selects what a push source's ingress does when the credit pool
// is exhausted (spec §7.4.6). Block backpressures; the Drop/Reject policies shed.
type OverflowPolicy int

const (
	// OverflowBlock backpressures the stream loop until a credit frees (default).
	OverflowBlock OverflowPolicy = iota
	// OverflowDropNewest drops the just-arrived message (Nack, no requeue).
	OverflowDropNewest
	// OverflowDropOldest is accepted for API stability; on a streaming source
	// with no discrete buffer stage it behaves as OverflowDropNewest until the
	// Poller buffer lands (Plan 004) — see ADR 0008 D7.
	OverflowDropOldest
	// OverflowReject is HTTP-semantics (503/429); on a non-HTTP streaming source
	// it behaves as OverflowDropNewest until adapter/http lands — see ADR 0008 D7.
	OverflowReject
)

// String renders the policy; unknown values render as OverflowBlock (the safe default).
func (p OverflowPolicy) String() string {
	switch p {
	case OverflowDropNewest:
		return "OverflowDropNewest"
	case OverflowDropOldest:
		return "OverflowDropOldest"
	case OverflowReject:
		return "OverflowReject"
	default:
		return "OverflowBlock"
	}
}

// defaultMaxInFlight bounds claimed-but-unsettled messages when WithMaxInFlight
// is unset. It is finite (never unbounded — spec §7.4.1): the credit gate is
// always active.
const defaultMaxInFlight = 1024

// defaultAttemptTTL is how long an idle attempt-tracker entry survives before the
// clockwork sweep reclaims it (ADR 0008 D8). It vastly exceeds any in-process
// redelivery cadence, so an actively-redelivering id (re-observed each attempt)
// is never swept mid-flight (NF-2).
const defaultAttemptTTL = 5 * time.Minute

// WithMaxInFlight bounds claimed-but-unsettled messages to n (n >= 1). This is
// the load-bearing flood defense (spec §7.4.1); default defaultMaxInFlight.
// Setting the flag lets NewConsumer tell an explicit WithMaxInFlight(0) (a
// caller error → ErrInvalidMaxInFlight) apart from "unset" (→ default) (C2).
func WithMaxInFlight[T any](n int) ConsumerOption[T] {
	return func(o *consumerConfig[T]) { o.maxInFlight = n; o.maxInFlightSet = true }
}

// WithRateLimit paces ingress with r (spec §7.4.2). A nil r is a no-op.
func WithRateLimit[T any](r RateLimiter) ConsumerOption[T] {
	return func(o *consumerConfig[T]) {
		if r != nil {
			o.rateLimiter = r
		}
	}
}

// WithHandlerTimeout runs each handler under a clockwork-derived deadline d
// (spec §7.4.4). A stuck cooperative handler is cancelled → transient failure
// (retried), not a pinned worker. d <= 0 disables the timeout.
func WithHandlerTimeout[T any](d time.Duration) ConsumerOption[T] {
	return func(o *consumerConfig[T]) { o.handlerTimeout = d }
}

// WithCircuitBreaker gates ingress AND dispatch on b (spec §7.4.5). A nil b is a no-op.
func WithCircuitBreaker[T any](b CircuitBreaker) ConsumerOption[T] {
	return func(o *consumerConfig[T]) {
		if b != nil {
			o.breaker = b
		}
	}
}

// WithOverflow selects the ingress overflow policy for a push source (spec §7.4.6);
// default OverflowBlock.
func WithOverflow[T any](p OverflowPolicy) ConsumerOption[T] {
	return func(o *consumerConfig[T]) { o.overflow = p }
}
```

```go
// errors.go (append inside the existing var block)
	// ErrInvalidMaxInFlight is returned when WithMaxInFlight is given n < 1.
	ErrInvalidMaxInFlight = errors.New("msgin: max in-flight must be >= 1")
	// ErrInvalidRateLimit is returned when a rate-limit configuration is invalid
	// (non-positive rps or burst on the default token bucket).
	ErrInvalidRateLimit = errors.New("msgin: rate limit must have positive rps and burst")
	// ErrOverflowDropped is the cause carried to OnRetry/logs when an overflow
	// policy sheds a message; it is not returned from any exported function.
	ErrOverflowDropped = errors.New("msgin: message dropped by overflow policy")
```

```go
// consumer.go — extend consumerConfig and consumer with the flow-control fields.
type consumerConfig[T any] struct {
	// ... existing fields ...
	maxInFlight    int
	maxInFlightSet bool // C2: distinguishes explicit WithMaxInFlight(0) from unset
	rateLimiter    RateLimiter
	handlerTimeout time.Duration
	breaker        CircuitBreaker
	overflow       OverflowPolicy
}

type consumer[T any] struct {
	// ... existing fields ...
	maxInFlight    int
	rateLimiter    RateLimiter
	handlerTimeout time.Duration
	breaker        CircuitBreaker
	overflow       OverflowPolicy
}
```

In `NewConsumer`, after the existing `policy.Validate()` gate, resolve + validate the flow-control config and thread it into the returned `consumer`:

```go
	// C2: unset → default; explicitly set → must be >= 1 (so WithMaxInFlight(0)
	// is a rejected caller error, not silently defaulted).
	if !cfg.maxInFlightSet {
		cfg.maxInFlight = defaultMaxInFlight
	} else if cfg.maxInFlight < 1 {
		return nil, ErrInvalidMaxInFlight
	}
	// ... existing resolveCodec + StreamingSource + native wiring ...
	return &consumer[T]{
		// ... existing fields ...
		maxInFlight: cfg.maxInFlight, rateLimiter: cfg.rateLimiter,
		handlerTimeout: cfg.handlerTimeout, breaker: cfg.breaker, overflow: cfg.overflow,
	}, nil
```

Author `docs/adrs/0008-resilience-flow-control-api.md` (Status Accepted) with D1 (surface + always-on credit gate) and D2 (nil limiter/breaker = no-op; overflow enum closed) in full, D3–D9 as one-line stubs (fleshed out in their tasks). Full outline at the end of this plan.

- [ ] **Step 4: Run test to verify it passes**

Run: `GOTOOLCHAIN=go1.25.0 go test ./... -race -v`
Expected: PASS (existing tests still green; new validation + String tests pass), no goroutine leaks.

- [ ] **Step 5: Commit**

```bash
git add flowcontrol.go errors.go consumer.go flowcontrol_test.go docs/adrs/0008-resilience-flow-control-api.md
git commit -m "feat: flow-control option surface, interfaces, and construction validation" \
  -m "Adds WithMaxInFlight/WithRateLimit/WithHandlerTimeout/WithCircuitBreaker/WithOverflow, the RateLimiter/CircuitBreaker/OverflowPolicy types, and always-on credit-gate defaults; runtime behavior wired in later tasks. Authors ADR 0008." \
  -m "Spec: 001" -m "Plan: 003" -m "ADR: 0006, 0008"
```

---

### Task 2: The credit gate — `WithMaxInFlight`, ingress stage, release-first wrapping (the core)

**Files:**
- Create: `credit.go`
- Modify: `consumer.go` (rewrite `Run` to add the ingress stage; add `process`; keep the C1 bounded-shutdown drain intact)
- Modify: `docs/adrs/0008-resilience-flow-control-api.md` (flesh out D3 credit model, D4 release-first)
- Modify: `settlement_doubles_test.go` (add `countingSource`)
- Test: `consumer_test.go` (append)

**Interfaces:**
- Consumes: `Delivery`, `StreamingSource`, `consumer[T]` fields (`maxInFlight`, `overflow`), the existing `dispatch`/`finish` (unchanged).
- Produces (unexported): `type creditGate`, `func newCreditGate(n int) *creditGate`, `(*creditGate).acquire(ctx) error`, `(*creditGate).tryAcquire() bool`, `(*creditGate).release()`; `type managedDelivery struct { Delivery; release func() }`; `func manage(Delivery, release func()) managedDelivery`; `func (c *consumer[T]) ingest(...)`; `func (c *consumer[T]) admit(...) bool`; `func (c *consumer[T]) process(drainCtx, settleCtx context.Context, md managedDelivery)`.

**Design note (spec §7.4.1, ADR 0008 D3/D4).** The bound on claimed-but-unsettled messages is a size-`n` **counting semaphore** implemented as a buffered `chan struct{}` (acquire = send, release = receive; capacity `n` is the whole bound — this *is* the spec's "buffer of size n as the credit pool", realized precisely). The old `deliveries` channel is replaced by a two-stage pipeline: `Stream → rawCh → ingest goroutine → workerCh(unbuffered) → workers`. `ingest` reads a delivery, applies the overflow policy at credit acquisition (Task 2 ships only `OverflowBlock`: `acquire` blocks = backpressure), then wraps and forwards.

**Release-first (the deadlock/stall fix — audit class 3).** Each admitted delivery gets `release := sync.OnceFunc(gate.release)`. `manage` wraps the delivery's `Ack`/`Nack` so they call `release()` **before** the original closure. This matters because the memory adapter's `Nack(requeue=true)` synchronously re-injects via `Send`, which re-enters the pipeline and needs a credit — releasing *before* that re-injection breaks the `n=1` circular wait (the freed slot is taken by the re-injected message; net in-flight never exceeds `n`). The dispatch/divert/settlement switch from Plan 002 is **not touched**: it calls `d.Ack`/`d.Nack` exactly as before; the wrapping makes those release credit. The worker also `defer md.release()` as a panic-safe net (OnceFunc ⇒ idempotent, so the wrapped settle + the defer = exactly one release — audit class 2).

**No over-pull (audit class 1) — precise accounting (M5).** A delivery only reaches a worker *after* acquiring a credit, so **claimed-but-unsettled (credit-holding) is ≤ `n` strictly**, regardless of worker count or burst size — this is the load-bearing bound. Note the weaker "**pulled-from-source-but-not-yet-settled**" figure is *not* `n+1`: it can reach `n+2`, because two messages can sit parked off the credit pool at once — one parked in `ingest` on `acquire` (pulled from `rawCh`, awaiting a credit) **and** one parked in the source's `Stream` trying to send the next delivery onto the unbuffered `rawCh`. Both hold **no credit**, so neither breaks the `≤ n` credit bound; they simply mean the source has been read up to two deliveries ahead of the credit frontier. The source still cannot be drained faster than credits free (the two parked messages are the whole slack).

**Hot-path branches introduced (each a covered case):** `acquire` success vs ctx-done (Nack of the un-acquired delivery, I4 — may block until cancelSettle); `release` runs once (OnceFunc) across every settle path incl. Nack/DLQ/invalid/shutdown-abandon; `admit` forward success vs ctx-done (Nack + release the held delivery); `process` drain short-circuit (`drainCtx.Err()!=nil` → Nack) vs normal dispatch; `ingest` rawCh-closed vs ctx-done exit; `n=1` Nack-redeliver does not deadlock; ingest holding a surplus delivery at shutdown → Run still returns, goleak-clean (I4).

- [ ] **Step 1: Write the failing test**

```go
// settlement_doubles_test.go (append) — a source that reports how many
// deliveries it has emitted but not yet had settled, to catch over-pull.
type countingSource struct {
	broker *memory.Broker
}

func (s *countingSource) EmitsLiveValue() bool { return true }
func (s *countingSource) Stream(ctx context.Context, out chan<- msgin.Delivery) error {
	return s.broker.Stream(ctx, out)
}
```

```go
// consumer_test.go (append) — the credit gate strictly bounds in-flight.
func TestConsumer_MaxInFlight_BoundsClaimedUnsettled(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		const n = 3
		const burst = 20
		b := memory.New(memory.WithBuffer(burst))

		var inFlight, maxSeen, processed atomic.Int64
		release := make(chan struct{})
		h := func(ctx context.Context, _ msgin.Message[order]) error {
			cur := inFlight.Add(1)
			for {
				m := maxSeen.Load()
				if cur <= m || maxSeen.CompareAndSwap(m, cur) {
					break
				}
			}
			<-release
			inFlight.Add(-1)
			processed.Add(1)
			return nil
		}
		c, err := msgin.NewConsumer[order](b, h,
			// I3: inject a fake consumer clock and NEVER advance it. Once Task 7
			// adds the always-on tracker sweep, the sweep goroutine creates a
			// ticker on the CONSUMER clock; with a real clock that would be a real
			// ticker inside the synctest bubble, perturbing synctest.Wait(). A
			// clockwork fake ticker fires ONLY on Advance, so — never advanced — it
			// stays durably blocked and is invisible to synctest.Wait().
			msgin.WithConsumerClock[order](clockwork.NewFakeClock()),
			msgin.WithConcurrency[order](burst),   // many workers…
			msgin.WithMaxInFlight[order](n))       // …but credit caps in-flight at n
		require.NoError(t, err)

		ctx, cancel := context.WithCancel(t.Context())
		runDone := make(chan error, 1)
		go func() { runDone <- c.Run(ctx) }()
		for i := 0; i < burst; i++ {
			require.NoError(t, b.Send(ctx, msgin.New[any](order{ID: "x"})))
		}

		synctest.Wait() // steady state: exactly n handlers entered, the rest gated
		assert.LessOrEqual(t, maxSeen.Load(), int64(n), "never more than n claimed-but-unsettled (no over-pull)")
		assert.Equal(t, int64(n), inFlight.Load(), "exactly n admitted; the surplus waits in the source")

		close(release)  // let everything drain; credits recycle
		synctest.Wait()
		assert.Equal(t, int64(burst), processed.Load(), "all messages eventually processed (no credit leak)")

		cancel()
		assert.ErrorIs(t, <-runDone, context.Canceled)
	})
}

// audit class 3: at n=1 a transient Nack must release its slot BEFORE the memory
// re-enqueue, or the redelivery deadlocks waiting for the credit it still holds.
func TestConsumer_MaxInFlight_NackReleasesSlot_NoDeadlock(t *testing.T) {
	b := memory.New()
	var attempts atomic.Int64
	done := make(chan struct{})
	h := func(context.Context, msgin.Message[order]) error {
		if attempts.Add(1) == 1 {
			return errors.New("transient once") // Nack(requeue) re-enqueues via Send
		}
		select {
		case <-done:
		default:
			close(done)
		}
		return nil
	}
	c, err := msgin.NewConsumer[order](b, h, msgin.WithMaxInFlight[order](1))
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	runDone := make(chan error, 1)
	go func() { runDone <- c.Run(ctx) }()
	require.NoError(t, b.Send(ctx, msgin.New[any](order{ID: "x"})))

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("redelivery deadlocked: the Nacked message pinned its only credit (NF-5)")
	}
	cancel()
	assert.ErrorIs(t, <-runDone, context.Canceled)
	assert.GreaterOrEqual(t, attempts.Load(), int64(2), "message was redelivered after the transient Nack")
}

// I4: shutdown must still return (bounded, goleak-clean) even when the ingress
// goroutine is holding a surplus delivery whose ctx-done Nack(requeue=true)
// blocks on the memory adapter's UNBUFFERED Send (the stopped Stream no longer
// reads). With n=1 and a handler that holds the single credit, a second message
// is pulled by ingest and parks on acquire; at cancel, admit's acquire returns
// ctx.Err and Nacks that held delivery via requeue → the memory Send blocks
// until cancelSettle fires at the shutdown deadline. Run must not hang past the
// (finite) deadline, and no goroutine may leak.
func TestConsumer_MaxInFlight_ShutdownWithIngestHeldSurplus_NoLeak(t *testing.T) {
	b := memory.New() // UNBUFFERED: Nack(requeue) Send blocks once Stream stops
	clk := clockwork.NewFakeClock()
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	h := func(context.Context, msgin.Message[order]) error {
		select {
		case entered <- struct{}{}:
		default:
		}
		<-release // hold the only credit so the 2nd msg parks in ingest on acquire
		return nil
	}
	c, err := msgin.NewConsumer[order](b, h,
		msgin.WithConsumerClock[order](clk),
		msgin.WithMaxInFlight[order](1))
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	runDone := make(chan error, 1)
	go func() { runDone <- c.Run(ctx) }()

	require.NoError(t, b.Send(ctx, msgin.New[any](order{ID: "a"})))
	require.NoError(t, b.Send(ctx, msgin.New[any](order{ID: "b"}))) // parks in ingest on acquire
	<-entered
	cancel() // Stream returns; ingest's acquire returns ctx.Err → Nack(requeue) blocks on Send

	require.NoError(t, clk.BlockUntilContext(t.Context(), 1)) // drain's clock.After registered (bumped to 2 in Task 7: +1 sweep ticker, C1)
	clk.Advance(time.Hour)                                    // fire the deadline → cancelSettle unblocks the stuck Send
	close(release)

	select {
	case err := <-runDone:
		assert.ErrorIs(t, err, context.Canceled)
	case <-time.After(2 * time.Second):
		t.Fatal("Run hung: ingest-held surplus Nack blocked on the unbuffered Send past the deadline (I4)")
	}
}
```

Add imports to `consumer_test.go` if absent: `"sync/atomic"`, `"testing/synctest"`, `"errors"`, `"time"`, `"github.com/jonboulle/clockwork"` (for the I3 fake consumer clock).

- [ ] **Step 2: Run test to verify it fails**

Run: `GOTOOLCHAIN=go1.25.0 go test ./... -run 'TestConsumer_MaxInFlight' -race -v`
Expected: FAIL — the current `Run` has no credit gate (`WithConcurrency(20)` lets 20 handlers run), so `maxSeen` exceeds `n`.

- [ ] **Step 3: Write minimal implementation**

```go
// credit.go
package msgin

import (
	"context"
	"sync"
)

// creditGate bounds claimed-but-unsettled messages: a buffered channel used as a
// counting semaphore of capacity n (acquire = send a token, release = receive
// one). The capacity IS the whole bound (spec §7.4.1) — for a streaming source
// this realizes "the bounded buffer of size n as the credit pool".
type creditGate struct {
	tokens chan struct{}
}

func newCreditGate(n int) *creditGate {
	return &creditGate{tokens: make(chan struct{}, n)}
}

// acquire blocks until a credit is free or ctx is done.
func (g *creditGate) acquire(ctx context.Context) error {
	select {
	case g.tokens <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// tryAcquire takes a credit without blocking; false if none is free.
func (g *creditGate) tryAcquire() bool {
	select {
	case g.tokens <- struct{}{}:
		return true
	default:
		return false
	}
}

// release returns exactly one credit. Callers wrap this in sync.OnceFunc per
// delivery so it fires exactly once across all settle paths (NF-5).
func (g *creditGate) release() { <-g.tokens }

// managedDelivery is a Delivery whose Ack/Nack release the delivery's credit
// (release-first, via manage). release is the same sync.OnceFunc, exposed so the
// worker can defer it as a panic-safe net.
type managedDelivery struct {
	Delivery
	release func()
}

// manage wraps d's settle closures so each releases the credit BEFORE invoking
// the original closure — essential so a Nack(requeue) that synchronously
// re-injects the message (memory) does not deadlock waiting for the credit it
// still holds (ADR 0008 D4). release is idempotent (sync.OnceFunc).
func manage(d Delivery, release func()) managedDelivery {
	origAck, origNack := d.Ack, d.Nack
	d.Ack = func(ctx context.Context) error {
		release()
		return origAck(ctx)
	}
	d.Nack = func(ctx context.Context, requeue bool, delay time.Duration) error {
		release()
		return origNack(ctx, requeue, delay)
	}
	return managedDelivery{Delivery: d, release: release}
}
```

Add `import "time"` to `credit.go`.

```go
// consumer.go — the ingress stage. ingest reads deliveries, applies the credit
// gate (Task 2: OverflowBlock only), wraps, and forwards to the workers. It stops
// on rawCh close (source stream ended) or ctx cancel (shutdown: stop admitting).
func (c *consumer[T]) ingest(ctx, settleCtx context.Context, gate *creditGate, in <-chan Delivery, out chan<- managedDelivery) {
	for {
		select {
		case <-ctx.Done():
			return
		case d, ok := <-in:
			if !ok {
				return
			}
			if !c.admit(ctx, settleCtx, gate, d, out) {
				return
			}
		}
	}
}

// admit acquires a credit for d (Block policy: backpressure until one frees),
// wraps it release-first, and forwards to a worker. Returns false when ctx is
// cancelled (shutdown) — the held delivery is Nacked (releasing any credit) so it
// is not lost. Rate-limit / breaker-admit / non-Block overflow are inserted here
// in Tasks 3/5/6.
//
// I4 — bounded, not leak: the ctx-done Nack(requeue=true) on the settleCtx can
// BLOCK until cancelSettle. For the memory adapter a requeue is a Send back onto
// the broker channel; once Run's Stream has stopped reading, that Send blocks
// until the shutdown deadline cancels settleCtx (Run's C1 bound), then returns.
// It never leaks — Run always reaches the deadline and cancels — but it can wait
// up to the full shutdownTimeout, which is the documented shutdown contract.
func (c *consumer[T]) admit(ctx, settleCtx context.Context, gate *creditGate, d Delivery, out chan<- managedDelivery) bool {
	if err := gate.acquire(ctx); err != nil {
		c.finish(d.Nack(settleCtx, true, 0)) // ctx done before a credit was free; no credit held; the Nack may block until cancelSettle (I4)
		return false
	}
	md := manage(d, sync.OnceFunc(gate.release))
	select {
	case out <- md:
		return true
	case <-ctx.Done():
		c.finish(md.Nack(settleCtx, true, 0)) // wrapped: releases the credit + re-enqueues
		return false
	}
}

// process runs one delivery on a worker: the drain short-circuit (Nack without
// dispatch once the shutdown deadline expired) or the full settlement switch. The
// deferred release is a panic-safe net; the wrapped Ack/Nack already release
// exactly once (OnceFunc), so this is idempotent.
func (c *consumer[T]) process(drainCtx, settleCtx context.Context, md managedDelivery) {
	defer md.release()
	if drainCtx.Err() != nil {
		c.finish(md.Nack(settleCtx, true, 0))
		return
	}
	c.dispatch(drainCtx, settleCtx, md.Delivery)
}
```

Add `import "sync"` to `consumer.go` (already imports it). Rewrite `Run` to insert the ingress stage, preserving the detached, cancellable settle/drain contexts and the always-finite deadline:

```go
func (c *consumer[T]) Run(ctx context.Context) error {
	rawCh := make(chan Delivery)          // Stream writes here
	workerCh := make(chan managedDelivery) // ingress -> workers (unbuffered handoff)
	gate := newCreditGate(c.maxInFlight)

	settleCtx, cancelSettle := context.WithCancel(context.WithoutCancel(ctx))
	defer cancelSettle()
	drainCtx, cancelDrain := context.WithCancel(context.WithoutCancel(ctx))
	defer cancelDrain()

	var ingressWG sync.WaitGroup
	ingressWG.Add(1)
	go func() {
		defer ingressWG.Done()
		defer close(workerCh) // closing workerCh lets the workers finish their range
		c.ingest(ctx, settleCtx, gate, rawCh, workerCh)
	}()

	var wg sync.WaitGroup
	wg.Add(c.workers)
	for i := 0; i < c.workers; i++ {
		go func() {
			defer wg.Done()
			for md := range workerCh {
				c.process(drainCtx, settleCtx, md)
			}
		}()
	}

	streamErr := c.src.Stream(ctx, rawCh) // blocks until ctx is cancelled
	close(rawCh)                          // ingress drains remaining reads, then closes workerCh

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()

	timeout := c.shutdownTimeout
	if timeout <= 0 {
		timeout = defaultShutdownTimeout
	}
	select {
	case <-done:
	case <-c.clock.After(timeout):
		cancelDrain()
		cancelSettle()
		<-done
	}
	ingressWG.Wait() // ingress exits on rawCh close / ctx done; join for goleak
	return streamErr
}
```

Flesh out ADR 0008 **D3** (credit = size-n semaphore, always active; the ingress two-stage pipeline; no over-pull because a delivery reaches a worker only after acquiring) and **D4** (release-first wrapping; idempotent OnceFunc; the `n=1` deadlock it prevents).

- [ ] **Step 4: Run test to verify it passes**

Run: `GOTOOLCHAIN=go1.25.0 go test ./... -race -v`
Expected: PASS — all Plan 002 settlement/shutdown tests remain green (the wrapping is transparent to the `settle` recorders; the C1 transient-Nack-during-drain test still returns, now with the credit released before the blocking Send), plus the two new credit tests. No goroutine leaks.

- [ ] **Step 5: Commit**

```bash
git add credit.go consumer.go settlement_doubles_test.go consumer_test.go docs/adrs/0008-resilience-flow-control-api.md
git commit -m "feat: credit-based flow control (WithMaxInFlight) with release-first settlement" \
  -m "Adds the ingress credit gate (size-n semaphore) and release-first Ack/Nack wrapping so claimed-but-unsettled is strictly bounded, a Nack never pins its slot, and n=1 redelivery cannot deadlock (spec §7.4.1, NF-4/NF-5)." \
  -m "Spec: 001" -m "Plan: 003" -m "ADR: 0006, 0008"
```

---

### Task 3: Rate limiter — `RateLimiter` default (clockwork token bucket) + ingress wiring

**Files:**
- Create: `ratelimit.go`
- Modify: `consumer.go` (`admit`: rate-limit BEFORE credit acquire — composition order)
- Modify: `docs/adrs/0008-resilience-flow-control-api.md` (flesh out D5 rate-limit)
- Test: `ratelimit_test.go`, `consumer_test.go` (append)

**Interfaces:**
- Consumes: `RateLimiter`, `clockwork.Clock`, `ErrInvalidRateLimit`.
- Produces: `type tokenBucket` (unexported); `func NewTokenBucket(rps float64, burst int, opts ...TokenBucketOption) (RateLimiter, error)`; `type TokenBucketOption`; `func WithTokenBucketClock(clockwork.Clock) TokenBucketOption`.

**Design note (spec §7.4.2, ADR 0008 D5).** `NewTokenBucket` returns the dependency-free default: `rps` tokens/sec, `burst` capacity, `clockwork`-driven so `Wait` is deterministic under a fake clock. `Wait(ctx)` computes the time until the next token, `<-clock.After(delay)` (cancellable via ctx), and consumes it. Validation: `rps <= 0` or `burst < 1` → `ErrInvalidRateLimit`. In `admit`, the limiter runs **before** the credit acquire (composition order rate-limit → credit gate → buffer); a limiter error (ctx done) Nacks the un-acquired delivery. The user may instead pass `golang.org/x/time/rate` (its `*Limiter` already satisfies `RateLimiter`), which uses the real clock — documented as the plug-in path.

**Hot-path/typed-error branches introduced:** `NewTokenBucket` `rps<=0` → err; `burst<1` → err; valid → limiter. `Wait` token available now (no wait) vs must wait then consume vs ctx-done→err vs loop re-check (M1: after the wait, tokens still `< 1` → wait again, never a blind over-admit). `admit` limiter-error → Nack + return false vs success → proceed to credit.

- [ ] **Step 1: Write the failing test**

```go
// ratelimit_test.go
package msgin_test

import (
	"testing"
	"time"

	"github.com/jonboulle/clockwork"
	"github.com/kartaladev/msgin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewTokenBucket_Validation(t *testing.T) {
	tests := []struct {
		name   string
		rps    float64
		burst  int
		assert func(t *testing.T, err error)
	}{
		{"non-positive rps rejected", 0, 1, func(t *testing.T, err error) { assert.ErrorIs(t, err, msgin.ErrInvalidRateLimit) }},
		{"burst below 1 rejected", 10, 0, func(t *testing.T, err error) { assert.ErrorIs(t, err, msgin.ErrInvalidRateLimit) }},
		{"valid config", 10, 5, func(t *testing.T, err error) { assert.NoError(t, err) }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := msgin.NewTokenBucket(tc.rps, tc.burst)
			tc.assert(t, err)
		})
	}
}

// Deterministic pacing: burst 1, 10 rps => second Wait must block ~100ms on the
// fake clock. First Wait returns immediately (initial token); the second parks
// until the clock advances one refill interval.
func TestTokenBucket_Wait_PacesOnFakeClock(t *testing.T) {
	clk := clockwork.NewFakeClock()
	rl, err := msgin.NewTokenBucket(10, 1, msgin.WithTokenBucketClock(clk))
	require.NoError(t, err)

	require.NoError(t, rl.Wait(t.Context())) // initial token, no wait

	waited := make(chan error, 1)
	go func() { waited <- rl.Wait(t.Context()) }()

	clk.BlockUntil(1)                 // the second Wait is parked on clock.After
	clk.Advance(100 * time.Millisecond) // one refill interval
	select {
	case err := <-waited:
		assert.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("second Wait did not unblock after a refill interval")
	}
}

func TestTokenBucket_Wait_CancelledContext(t *testing.T) {
	clk := clockwork.NewFakeClock()
	rl, err := msgin.NewTokenBucket(1, 1, msgin.WithTokenBucketClock(clk))
	require.NoError(t, err)
	require.NoError(t, rl.Wait(t.Context())) // consume the initial token

	ctx, cancel := context.WithCancel(t.Context())
	waited := make(chan error, 1)
	go func() { waited <- rl.Wait(ctx) }()
	clk.BlockUntil(1)
	cancel()
	assert.ErrorIs(t, <-waited, context.Canceled)
}
```

Add imports `"context"` to `ratelimit_test.go`.

- [ ] **Step 2: Run test to verify it fails**

Run: `GOTOOLCHAIN=go1.25.0 go test ./... -run 'TokenBucket' -v`
Expected: FAIL — `undefined: msgin.NewTokenBucket`.

- [ ] **Step 3: Write minimal implementation**

```go
// ratelimit.go
package msgin

import (
	"context"
	"sync"
	"time"

	"github.com/jonboulle/clockwork"
)

// tokenBucket is the dependency-free default RateLimiter: a clockwork-driven
// bucket of `burst` tokens refilling at `rps` per second.
type tokenBucket struct {
	clock    clockwork.Clock
	interval time.Duration // time to accrue one token
	burst    float64
	mu       sync.Mutex
	tokens   float64
	last     time.Time
}

// TokenBucketOption configures NewTokenBucket.
type TokenBucketOption func(*tokenBucket)

// WithTokenBucketClock injects the clock (default: real). Tests pass a fake clock.
func WithTokenBucketClock(c clockwork.Clock) TokenBucketOption {
	return func(b *tokenBucket) {
		if c != nil {
			b.clock = c
		}
	}
}

// NewTokenBucket builds the default RateLimiter. rps must be > 0 and burst >= 1,
// else ErrInvalidRateLimit. It starts full (burst tokens).
func NewTokenBucket(rps float64, burst int, opts ...TokenBucketOption) (RateLimiter, error) {
	if rps <= 0 || burst < 1 {
		return nil, ErrInvalidRateLimit
	}
	b := &tokenBucket{
		clock:    clockwork.NewRealClock(),
		interval: time.Duration(float64(time.Second) / rps),
		burst:    float64(burst),
		tokens:   float64(burst),
	}
	for _, opt := range opts {
		opt(b)
	}
	b.last = b.clock.Now()
	return b, nil
}

// Wait blocks until a token is available or ctx is done, then consumes exactly
// one. It LOOPS re-checking after each wait rather than admitting unconditionally
// once the timer fires (M1): if a concurrent caller consumed the freed token
// while we slept, tokens can be < 1 again, and a blind post-wait admit would
// over-admit (return without decrementing, i.e. hand out a token that isn't
// there). Looping consumes a token only when one is genuinely present, so the
// rps bound holds even if the limiter is shared by several callers. (In msgin
// the limiter is driven only by the single serial ingest goroutine, so the loop
// runs at most once in practice — but correctness must not rely on that.)
func (b *tokenBucket) Wait(ctx context.Context) error {
	for {
		b.mu.Lock()
		b.refill()
		if b.tokens >= 1 {
			b.tokens--
			b.mu.Unlock()
			return nil
		}
		deficit := 1 - b.tokens
		wait := time.Duration(deficit * float64(b.interval))
		b.mu.Unlock()

		select {
		case <-b.clock.After(wait):
			// loop: re-check; the token may have been taken by another caller.
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// refill accrues tokens for the elapsed time, capped at burst. Caller holds mu.
func (b *tokenBucket) refill() {
	now := b.clock.Now()
	elapsed := now.Sub(b.last)
	if elapsed <= 0 {
		return
	}
	b.last = now
	b.tokens += float64(elapsed) / float64(b.interval)
	if b.tokens > b.burst {
		b.tokens = b.burst
	}
}
```

Wire the limiter into `admit`, **before** the credit acquire (composition order):

```go
func (c *consumer[T]) admit(ctx, settleCtx context.Context, gate *creditGate, d Delivery, out chan<- managedDelivery) bool {
	if c.rateLimiter != nil {
		if err := c.rateLimiter.Wait(ctx); err != nil {
			c.finish(d.Nack(settleCtx, true, 0)) // ctx done; no credit held
			return false
		}
	}
	if err := gate.acquire(ctx); err != nil {
		c.finish(d.Nack(settleCtx, true, 0))
		return false
	}
	// ... unchanged: manage + forward select ...
}
```

Add a through-`Run` pacing test asserting the limiter gates ingress (fake clock; two messages, only one processed until the clock advances one interval):

```go
// consumer_test.go (append)
func TestConsumer_RateLimit_GatesIngress(t *testing.T) {
	clk := clockwork.NewFakeClock()
	rl, err := msgin.NewTokenBucket(10, 1, msgin.WithTokenBucketClock(clk))
	require.NoError(t, err)

	b := memory.New(memory.WithBuffer(4))
	var processed atomic.Int64
	h := func(context.Context, msgin.Message[order]) error { processed.Add(1); return nil }
	c, err := msgin.NewConsumer[order](b, h, msgin.WithRateLimit[order](rl))
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	runDone := make(chan error, 1)
	go func() { runDone <- c.Run(ctx) }()
	require.NoError(t, b.Send(ctx, msgin.New[any](order{ID: "a"})))
	require.NoError(t, b.Send(ctx, msgin.New[any](order{ID: "b"})))

	require.Eventually(t, func() bool { return processed.Load() == 1 }, time.Second, 5*time.Millisecond)
	// The second is gated on the limiter (parked on clock.After) — advancing a
	// refill interval lets it through.
	clk.BlockUntil(1)
	clk.Advance(100 * time.Millisecond)
	require.Eventually(t, func() bool { return processed.Load() == 2 }, time.Second, 5*time.Millisecond)

	cancel()
	assert.ErrorIs(t, <-runDone, context.Canceled)
}
```

Flesh out ADR 0008 **D5** (RateLimiter interface + token-bucket default; composition-order rate-limit→credit; x/time/rate as the real-clock plug-in).

- [ ] **Step 4: Run test to verify it passes**

Run: `GOTOOLCHAIN=go1.25.0 go test ./... -race -v`
Expected: PASS, no goroutine leaks.

- [ ] **Step 5: Commit**

```bash
git add ratelimit.go consumer.go ratelimit_test.go consumer_test.go docs/adrs/0008-resilience-flow-control-api.md
git commit -m "feat: WithRateLimit and a clockwork token-bucket default RateLimiter" \
  -m "Ingress paces before the credit gate (composition order); default bucket is dependency-free and deterministic under a fake clock; x/time/rate plugs in directly (spec §7.4.2)." \
  -m "Spec: 001" -m "Plan: 003" -m "ADR: 0006, 0008"
```

---

### Task 4: Handler timeout — `WithHandlerTimeout` (clockwork-derived handler context)

**Files:**
- Modify: `consumer.go` (`dispatch`: derive a timeout context for `safeHandle`; add `handlerContext`)
- Modify: `docs/adrs/0008-resilience-flow-control-api.md` (flesh out D6 handler-timeout)
- Test: `consumer_test.go` (append)

**Interfaces:**
- Consumes: `consumer[T].handlerTimeout`, `consumer[T].clock`.
- Produces (unexported): `func (c *consumer[T]) handlerContext(parent context.Context) (context.Context, context.CancelFunc)`.

**Design note (spec §7.4.4, ADR 0008 D6).** Each handler runs under a context that cancels after `handlerTimeout`, derived from the (drain) handler context via `clock.AfterFunc` so it is **clockwork-driven and joins cleanly**: on normal completion the returned cancel `Stop()`s the timer and cancels; on timeout the `AfterFunc` cancels the context. A cooperative handler observing `ctx.Done()` returns `context.Canceled`/`DeadlineExceeded`, which `isPermanent` classifies **transient** → the message is Nacked (retried), not diverted, and the worker is freed instead of pinned. `handlerTimeout <= 0` returns the parent context and a no-op cancel (zero overhead). This also bounds Plan 002's documented residual (a cooperative handler that ignores its context is now cut off; a handler that never returns at all is still only bounded by shutdown).

**Hot-path branches introduced:** `handlerTimeout<=0` → parent ctx, no-op cancel (no timer goroutine); `>0` → timed ctx; timeout fires → handler ctx cancelled → transient Nack; handler completes first → timer stopped (no leak); the timed-out `context.Canceled` is classified transient (not permanent).

- [ ] **Step 1: Write the failing test**

```go
// consumer_test.go (append)
func TestConsumer_HandlerTimeout_CancelsStuckHandlerAndRetries(t *testing.T) {
	st := &settle{}
	src := &reemittingSource{st: st, id: "m1", payload: order{ID: "o"}}
	clk := clockwork.NewFakeClock()

	var calls atomic.Int64
	proceeded := make(chan struct{}, 1)
	h := func(ctx context.Context, _ msgin.Message[order]) error {
		n := calls.Add(1)
		if n == 1 {
			<-ctx.Done() // first attempt: stuck until the timeout cancels us
			return ctx.Err()
		}
		select {
		case proceeded <- struct{}{}:
		default:
		}
		return nil
	}
	c, err := msgin.NewConsumer[order](src, h,
		msgin.WithConsumerClock[order](clk),
		msgin.WithHandlerTimeout[order](2*time.Second))
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	runDone := make(chan error, 1)
	go func() { runDone <- c.Run(ctx) }()

	clk.BlockUntil(1)             // the first handler's timeout is registered (bumped to 2 in Task 7: +1 sweep ticker, C1)
	clk.Advance(2 * time.Second) // fire it → ctx.Done → transient Nack → redelivery
	select {
	case <-proceeded:            // the redelivery ran and succeeded
	case <-time.After(2 * time.Second):
		t.Fatal("stuck handler was not cancelled by the timeout")
	}
	_, nacks, _ := st.snapshot()
	assert.GreaterOrEqual(t, nacks, 1, "timed-out handler is a transient failure (Nacked, retried)")

	cancel()
	assert.ErrorIs(t, <-runDone, context.Canceled)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `GOTOOLCHAIN=go1.25.0 go test ./... -run 'TestConsumer_HandlerTimeout' -race -v`
Expected: FAIL — no timeout wiring; the first handler blocks on `ctx.Done()` forever (the parent ctx never fires until cancel), so `proceeded` never receives.

- [ ] **Step 3: Write minimal implementation**

```go
// consumer.go — clockwork-derived handler context.
func (c *consumer[T]) handlerContext(parent context.Context) (context.Context, context.CancelFunc) {
	if c.handlerTimeout <= 0 {
		return parent, func() {}
	}
	hctx, cancel := context.WithCancel(parent)
	timer := c.clock.AfterFunc(c.handlerTimeout, cancel)
	return hctx, func() {
		timer.Stop()
		cancel()
	}
}
```

In `dispatch`, wrap the handler invocation (replace `err := c.safeHandle(ctx, msg)`):

```go
	hctx, cancelHandler := c.handlerContext(ctx)
	err := c.safeHandle(hctx, msg)
	cancelHandler()
```

Flesh out ADR 0008 **D6** (clockwork `AfterFunc`-derived handler ctx; timed-out = transient; the Plan 002 residual it bounds).

- [ ] **Step 4: Run test to verify it passes**

Run: `GOTOOLCHAIN=go1.25.0 go test ./... -race -v`
Expected: PASS, no goroutine leaks (the timer is stopped on completion; the fired timer only calls `cancel`).

- [ ] **Step 5: Commit**

```bash
git add consumer.go consumer_test.go docs/adrs/0008-resilience-flow-control-api.md
git commit -m "feat: WithHandlerTimeout via a clockwork-derived handler context" \
  -m "A stuck cooperative handler is cancelled → transient Nack (retried), not a pinned worker; deterministic under a fake clock; bounds the Plan 002 cooperative-handler residual (spec §7.4.4)." \
  -m "Spec: 001" -m "Plan: 003" -m "ADR: 0006, 0008"
```

---

### Task 5: Circuit breaker — `CircuitBreaker` default + ingress AND dispatch gating (NF-10)

**Files:**
- Create: `breaker.go`
- Modify: `consumer.go` (`dispatch` returns `error`; `process` records the outcome + gates dispatch; `admit` parks on an open breaker with a missed-wakeup-free re-check)
- Modify: `docs/adrs/0008-resilience-flow-control-api.md` (flesh out D7 breaker)
- Modify: `settlement_doubles_test.go` (add `scriptedBreaker` if a deterministic breaker is needed)
- Test: `breaker_test.go`, `consumer_test.go` (append)

**Interfaces:**
- Consumes: `CircuitBreaker`, `clockwork.Clock`, `isPermanent`.
- Produces: `type breaker` (unexported); `func NewCircuitBreaker(opts ...CircuitBreakerOption) CircuitBreaker`; `type CircuitBreakerOption`; `WithBreakerClock`, `WithBreakerThreshold(int)`, `WithBreakerCooldown(time.Duration)`. Also changes `dispatch` to `func (c *consumer[T]) dispatch(...) error`; adds `func (c *consumer[T]) admitBreaker(ctx context.Context) bool`.

**Design note (spec §7.4.5, ADR 0008 D7).** The default breaker is a `clockwork`-driven state machine: **closed** → `threshold` consecutive failures → **open** (schedules half-open via `clock.AfterFunc(cooldown, …)`) → **half-open** (allows probes) → **closed** (probe success) or **open** (probe failure). `HalfOpen()` returns a channel **closed** on the open→half-open transition; a fresh channel is minted each open cycle. An open breaker gates **both** ingress and dispatch (NF-10): `admit` calls `admitBreaker`, which parks the ingress goroutine — but with a **subscribe-then-recheck** (`wake := b.HalfOpen(); if b.Allow() { proceed }; select { <-wake; <-ctx.Done() }`) so a transition between the first `Allow()` and the park cannot be missed; and `process` checks `breaker.Allow()` before dispatch, Nacking (credit-releasing) any buffered/in-flight message instead of driving it into a failing downstream. `dispatch` now returns the **classification error** so `process` derives health from the transient-failure signal only: `Record(err == nil || isPermanent(err))`. A poison/bad *message* (a decode failure or a permanent handler error) must **not** count as a downstream failure — else a burst of poison messages would trip the breaker and stall healthy traffic (M4).

**`dispatch` return at each path (M4) — this is the breaker's health signal, so it is exact:**

| path | `dispatch` returns | `isPermanent`? | `Record` health |
|---|---|---|---|
| decode failed (→ invalid sink) | the decode error (`ErrPayloadDecode`/`ErrPayloadType`) | yes (permanent) | healthy (not a failure) |
| `err == nil` (Ack) | `nil` | — | healthy |
| permanent handler error (→ invalid sink) | that error | yes | healthy (not a failure) |
| transient exhausted (→ dead-letter) | the transient error | no | **unhealthy** (counts) |
| transient (→ Nack redeliver) | the transient error | no | **unhealthy** (counts) |

So only the two genuinely transient (downstream/handler) paths feed `Record(false)`; decode/permanent poison is `Record(true)`. `process` uses the returned error solely for `Record`; the settlement itself already happened inside `dispatch` exactly as in Plan 002 (the return is additive, no behavior change to settling).

**Hot-path/typed-error branches introduced:** breaker Record success (reset fails; half-open→closed) vs failure (closed increments; ≥threshold→open; half-open→reopen); `HalfOpen` closes-and-mints; `Allow` open→false vs closed/half-open→true; `admitBreaker` nil-breaker→true, Allow-immediately→true, park→wake vs ctx-done→false, re-check-after-subscribe catches the race; `process` breaker-open→Nack-without-dispatch vs allowed→dispatch+Record; `dispatch` return classified healthy (nil/permanent) vs unhealthy (transient).

- [ ] **Step 1: Write the failing test**

```go
// breaker_test.go
package msgin_test

import (
	"testing"
	"time"

	"github.com/jonboulle/clockwork"
	"github.com/kartaladev/msgin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCircuitBreaker_OpensClosesAndHalfOpenWakeup(t *testing.T) {
	clk := clockwork.NewFakeClock()
	b := msgin.NewCircuitBreaker(
		msgin.WithBreakerClock(clk),
		msgin.WithBreakerThreshold(2),
		msgin.WithBreakerCooldown(3*time.Second),
	)

	assert.True(t, b.Allow(), "starts closed")
	b.Record(false)
	assert.True(t, b.Allow(), "one failure below threshold: still closed")
	b.Record(false)
	assert.False(t, b.Allow(), "threshold reached: open")

	// A parked waiter must be woken EXPLICITLY when the cooldown elapses.
	wake := b.HalfOpen()
	woken := make(chan struct{})
	go func() {
		<-wake
		close(woken)
	}()
	clk.BlockUntil(1)            // the AfterFunc cooldown timer is registered
	clk.Advance(3 * time.Second) // fire cooldown → open→half-open → close(wake)
	select {
	case <-woken:
	case <-time.After(time.Second):
		t.Fatal("half-open transition did not wake the parked waiter (missed wakeup)")
	}
	assert.True(t, b.Allow(), "half-open allows a probe")

	b.Record(true)
	assert.True(t, b.Allow(), "probe success closes the breaker")
}

func TestCircuitBreaker_HalfOpenProbeFailureReopens(t *testing.T) {
	clk := clockwork.NewFakeClock()
	b := msgin.NewCircuitBreaker(msgin.WithBreakerClock(clk), msgin.WithBreakerThreshold(1), msgin.WithBreakerCooldown(time.Second))
	b.Record(false)
	require.False(t, b.Allow())
	clk.BlockUntil(1)
	clk.Advance(time.Second) // half-open
	require.True(t, b.Allow())
	b.Record(false)          // probe fails
	assert.False(t, b.Allow(), "half-open probe failure reopens")
}
```

```go
// consumer_test.go (append) — an open breaker pauses ingress AND Nacks buffered
// work (NF-10), and recovers on half-open.
func TestConsumer_CircuitBreaker_GatesDispatchAndRecovers(t *testing.T) {
	clk := clockwork.NewFakeClock()
	b := msgin.NewCircuitBreaker(msgin.WithBreakerClock(clk), msgin.WithBreakerThreshold(1), msgin.WithBreakerCooldown(5*time.Second))

	broker := memory.New(memory.WithBuffer(8))
	var ok atomic.Int64
	fail := true
	var mu sync.Mutex
	h := func(context.Context, msgin.Message[order]) error {
		mu.Lock()
		f := fail
		mu.Unlock()
		if f {
			return errors.New("downstream down") // trips the breaker (threshold 1)
		}
		ok.Add(1)
		return nil
	}
	c, err := msgin.NewConsumer[order](broker, h,
		msgin.WithConsumerClock[order](clk),
		msgin.WithCircuitBreaker[order](b))
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	runDone := make(chan error, 1)
	go func() { runDone <- c.Run(ctx) }()

	require.NoError(t, broker.Send(ctx, msgin.New[any](order{ID: "a"})))
	require.Eventually(t, func() bool { return !b.Allow() }, time.Second, 5*time.Millisecond) // opened

	// While open, more messages must NOT be dispatched into the failing handler.
	require.NoError(t, broker.Send(ctx, msgin.New[any](order{ID: "b"})))
	// Fix the downstream, then let the cooldown half-open and drain.
	mu.Lock()
	fail = false
	mu.Unlock()
	clk.BlockUntil(1)            // (bumped to 2 in Task 7: +1 sweep ticker, C1)
	clk.Advance(5 * time.Second) // half-open wakes the parked ingress
	require.Eventually(t, func() bool { return ok.Load() >= 1 }, 2*time.Second, 5*time.Millisecond)

	cancel()
	assert.ErrorIs(t, <-runDone, context.Canceled)
}

// M4: a stream of PERMANENT-error (poison) messages must NOT open the breaker —
// a bad message is not a downstream failure. With threshold 1, even one transient
// failure would open it; here every message is permanent, so the breaker stays
// closed and all messages keep flowing to the invalid sink.
func TestConsumer_CircuitBreaker_PermanentErrorsDoNotTrip(t *testing.T) {
	clk := clockwork.NewFakeClock()
	b := msgin.NewCircuitBreaker(msgin.WithBreakerClock(clk), msgin.WithBreakerThreshold(1), msgin.WithBreakerCooldown(5*time.Second))

	broker := memory.New(memory.WithBuffer(8))
	sink := &recordingSink{}
	h := func(context.Context, msgin.Message[order]) error {
		return msgin.Permanent(errors.New("poison")) // permanent → invalid sink, NOT a breaker failure
	}
	c, err := msgin.NewConsumer[order](broker, h,
		msgin.WithConsumerClock[order](clk),
		msgin.WithCircuitBreaker[order](b),
		msgin.WithInvalidMessageSink[order](sink))
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	runDone := make(chan error, 1)
	go func() { runDone <- c.Run(ctx) }()

	const total = 5
	for i := 0; i < total; i++ {
		require.NoError(t, broker.Send(ctx, msgin.New[any](order{ID: "p"})))
	}
	// All must reach the invalid sink; the breaker must never open on poison.
	require.Eventually(t, func() bool { return sink.count() == total }, 2*time.Second, 5*time.Millisecond)
	assert.True(t, b.Allow(), "permanent-error messages must not trip the breaker (M4)")

	cancel()
	assert.ErrorIs(t, <-runDone, context.Canceled)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `GOTOOLCHAIN=go1.25.0 go test ./... -run 'CircuitBreaker' -race -v`
Expected: FAIL — `undefined: msgin.NewCircuitBreaker`.

- [ ] **Step 3: Write minimal implementation**

```go
// breaker.go
package msgin

import (
	"sync"
	"time"

	"github.com/jonboulle/clockwork"
)

type breakerState int

const (
	breakerClosed breakerState = iota
	breakerOpen
	breakerHalfOpen
)

// breaker is the dependency-free default CircuitBreaker. It is clockwork-driven
// so the cooldown is deterministic in tests, and it signals the open→half-open
// transition by closing the HalfOpen channel (explicit wakeup, no missed-wakeup).
type breaker struct {
	clock     clockwork.Clock
	threshold int
	cooldown  time.Duration

	mu    sync.Mutex
	state breakerState
	fails int
	wake  chan struct{} // closed on open→half-open; re-minted each open cycle
	timer clockwork.Timer
}

// CircuitBreakerOption configures NewCircuitBreaker.
type CircuitBreakerOption func(*breaker)

// WithBreakerClock injects the clock (default: real).
func WithBreakerClock(c clockwork.Clock) CircuitBreakerOption {
	return func(b *breaker) {
		if c != nil {
			b.clock = c
		}
	}
}

// WithBreakerThreshold sets consecutive failures before opening (default 5, min 1).
func WithBreakerThreshold(n int) CircuitBreakerOption {
	return func(b *breaker) {
		if n >= 1 {
			b.threshold = n
		}
	}
}

// WithBreakerCooldown sets the open→half-open delay (default 30s, min > 0).
func WithBreakerCooldown(d time.Duration) CircuitBreakerOption {
	return func(b *breaker) {
		if d > 0 {
			b.cooldown = d
		}
	}
}

// NewCircuitBreaker builds the default breaker.
func NewCircuitBreaker(opts ...CircuitBreakerOption) CircuitBreaker {
	b := &breaker{
		clock:     clockwork.NewRealClock(),
		threshold: 5,
		cooldown:  30 * time.Second,
		state:     breakerClosed,
		wake:      make(chan struct{}),
	}
	for _, opt := range opts {
		opt(b)
	}
	return b
}

func (b *breaker) Allow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.state != breakerOpen
}

func (b *breaker) Record(success bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if success {
		b.fails = 0
		if b.state == breakerHalfOpen {
			b.state = breakerClosed
		}
		return
	}
	b.fails++
	switch b.state {
	case breakerClosed:
		if b.fails >= b.threshold {
			b.openLocked()
		}
	case breakerHalfOpen:
		b.openLocked() // probe failed → reopen (restarts the cooldown)
	}
}

func (b *breaker) HalfOpen() <-chan struct{} {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.wake
}

// openLocked transitions to open and schedules the half-open. Caller holds mu.
func (b *breaker) openLocked() {
	b.state = breakerOpen
	if b.timer != nil {
		b.timer.Stop()
	}
	b.timer = b.clock.AfterFunc(b.cooldown, b.toHalfOpen)
}

func (b *breaker) toHalfOpen() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.state != breakerOpen {
		return
	}
	b.state = breakerHalfOpen
	close(b.wake)                // explicit wakeup of parked waiters
	b.wake = make(chan struct{}) // fresh channel for the next open cycle
}
```

Change `dispatch` to `func (c *consumer[T]) dispatch(...) error`, returning the classification error at each path's tail per the M4 table above (the switch already holds the values): the decode-failed path `return derr`; the success path `return nil`; the permanent path `return err`; the dead-letter and Nack-redeliver paths `return err`. Because the decode/permanent errors are `isPermanent`-true, `process`'s `Record(err == nil || isPermanent(err))` counts only the two transient tails as failures. Wire the breaker in `admit` (ingress park) and `process` (dispatch gate + Record):

```go
// consumer.go — ingress park on an open breaker (missed-wakeup-free).
func (c *consumer[T]) admitBreaker(ctx context.Context) bool {
	if c.breaker == nil {
		return true
	}
	for {
		if c.breaker.Allow() {
			return true
		}
		wake := c.breaker.HalfOpen() // subscribe BEFORE re-checking
		if c.breaker.Allow() {       // re-check closes the missed-wakeup window
			return true
		}
		select {
		case <-wake:
		case <-ctx.Done():
			return false
		}
	}
}
```

In `admit`, insert `admitBreaker` after the rate-limit and before the credit acquire:

```go
	if !c.admitBreaker(ctx) {
		c.finish(d.Nack(settleCtx, true, 0))
		return false
	}
```

In `process`, gate dispatch and record the outcome:

```go
func (c *consumer[T]) process(drainCtx, settleCtx context.Context, md managedDelivery) {
	defer md.release()
	if drainCtx.Err() != nil {
		c.finish(md.Nack(settleCtx, true, 0))
		return
	}
	if c.breaker != nil && !c.breaker.Allow() {
		// Open: do not drive a buffered message into the failing downstream — Nack
		// it (releasing its credit) for later redelivery (NF-10).
		c.finish(md.Nack(settleCtx, true, 0))
		return
	}
	err := c.dispatch(drainCtx, settleCtx, md.Delivery)
	if c.breaker != nil {
		c.breaker.Record(err == nil || isPermanent(err)) // a bad message is not a downstream failure
	}
}
```

Flesh out ADR 0008 **D7** (breaker interface with the `HalfOpen` explicit-wakeup channel; ingress park subscribe-then-recheck; dispatch gate + `Record(healthy)` classification; gobreaker plug-in via `OnStateChange`; half-open admits probes, first outcome decides).

- [ ] **Step 4: Run test to verify it passes**

Run: `GOTOOLCHAIN=go1.25.0 go test ./... -race -v`
Expected: PASS, no goroutine leaks.

- [ ] **Step 5: Commit**

```bash
git add breaker.go consumer.go breaker_test.go consumer_test.go settlement_doubles_test.go docs/adrs/0008-resilience-flow-control-api.md
git commit -m "feat: WithCircuitBreaker gating ingress AND dispatch with explicit half-open wakeup" \
  -m "Default clockwork breaker; open pauses ingress (missed-wakeup-free park) and Nacks buffered work releasing credit (NF-10); dispatch returns its classification so only transient failures trip the breaker (spec §7.4.5)." \
  -m "Spec: 001" -m "Plan: 003" -m "ADR: 0006, 0008"
```

---

### Task 6: Overflow policy — `WithOverflow` (Block / DropNewest; DropOldest & Reject mapped)

**Files:**
- Modify: `consumer.go` (`admit`: apply the overflow policy at credit contention)
- Modify: `docs/adrs/0008-resilience-flow-control-api.md` (flesh out D7-overflow / D9 the mapping)
- Test: `consumer_test.go` (append)

**Interfaces:**
- Consumes: `consumer[T].overflow`, `creditGate.tryAcquire`, `ErrOverflowDropped`, `c.hooks.OnRetry`.
- Produces (unexported): the non-Block branch of `admit` (drop path).

**Design note (spec §7.4.6, ADR 0008 D9).** For `OverflowBlock` (default) `admit` blocks on `acquire` (backpressure — Task 2). For the shedding policies, `admit` uses `tryAcquire`: on success it proceeds; on contention it **sheds** — `Nack(requeue=false)` the just-arrived message, fires `OnRetry` with `ErrOverflowDropped` for observability, and continues. The dropped message never acquired a credit, so no credit leaks (audit class 2 on the drop path). `OverflowDropNewest` is implemented literally. `OverflowDropOldest` and `OverflowReject` are **accepted for API stability but behave as DropNewest** on a streaming source: DropOldest needs a discrete buffer stage to pick a victim (arrives with the Poller, Plan 004) and Reject needs HTTP request/response semantics (arrives with `adapter/http`). This mapping is a flagged ADR-0008 decision, not silent.

**Drop uses `requeue=false` — a genuine drop, not a redeliver (I2).** For Plan 003's only source (memory, at-most-once) shedding MUST be `Nack(requeue=false)`: a `requeue=true` would re-enqueue the shed message via `Send`, which re-enters the pipeline and defeats the whole point of load-shedding (re-enqueue-and-respin, not drop). So the message is genuinely dropped and, for memory, **lost** (at-most-once — documented per adapter). This **diverges from spec NF-14's "Nacked so an at-least-once source can redeliver"**: do **not** claim an at-least-once wire source will redeliver on this drop — `requeue=false` explicitly tells the source *not* to. ADR 0008 D9 **supersedes** the NF-14 wording accordingly (see D9). The redeliver-on-drop semantics for at-least-once sources (redis PEL / JetStream Nak/Term) are **deferred to the adapter plans (Plan 006)** — flagged in "Notes for Plan 006" below.

**Unknown policy → Block, not drop (I1).** The overflow `switch` **enumerates the shed policies explicitly** (`case OverflowDropNewest, OverflowDropOldest, OverflowReject:` → shed) and the `default:` **BLOCKS** (backpressure). This matches the documented `String()`/D2 contract that an out-of-range `OverflowPolicy` behaves as `OverflowBlock` (the safe default). A `default:`-sheds switch would silently *drop* under an unknown value — the opposite of the contract — so the arms are inverted from the naive version.

**Hot-path branches introduced:** shed policy (`DropNewest`/`DropOldest`/`Reject`) `tryAcquire` success → proceed vs fail → drop (Nack requeue=false + OnRetry(ErrOverflowDropped)) → continue ingest; `default` (`OverflowBlock` **and any out-of-range value**) → acquire-block (backpressure), covered by an out-of-range test asserting it blocks, never drops (I1).

- [ ] **Step 1: Write the failing test**

```go
// consumer_test.go (append) — with n=1 and DropNewest, a burst behind a blocked
// handler sheds the surplus (Nack, no requeue) without leaking credit; once the
// handler releases, later sends still flow (credit recycled, not leaked).
func TestConsumer_Overflow_DropNewest_ShedsWithoutLeak(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		b := memory.New(memory.WithBuffer(16))
		var processed atomic.Int64
		var dropped atomic.Int64
		release := make(chan struct{})
		h := func(context.Context, msgin.Message[order]) error {
			<-release // hold the single credit so the burst overflows
			processed.Add(1)
			return nil
		}
		hooks := msgin.Hooks{
			OnRetry: func(_ context.Context, _ msgin.Message[any], err error) {
				if errors.Is(err, msgin.ErrOverflowDropped) {
					dropped.Add(1)
				}
			},
		}
		c, err := msgin.NewConsumer[order](b, h,
			// I3: fake consumer clock, NEVER advanced — the Task-7 sweep ticker is
			// then a clockwork fake ticker that never fires, so it cannot perturb
			// synctest.Wait() (a real ticker inside the bubble would).
			msgin.WithConsumerClock[order](clockwork.NewFakeClock()),
			msgin.WithMaxInFlight[order](1),
			msgin.WithOverflow[order](msgin.OverflowDropNewest),
			msgin.WithHooks[order](hooks))
		require.NoError(t, err)

		ctx, cancel := context.WithCancel(t.Context())
		runDone := make(chan error, 1)
		go func() { runDone <- c.Run(ctx) }()
		for i := 0; i < 6; i++ {
			require.NoError(t, b.Send(ctx, msgin.New[any](order{ID: "x"})))
		}

		synctest.Wait() // one handler holds the credit; the rest are shed
		assert.GreaterOrEqual(t, dropped.Load(), int64(1), "surplus dropped by DropNewest")

		close(release) // free the credit
		synctest.Wait()
		// The one that held the credit completed; the credit was not leaked.
		assert.GreaterOrEqual(t, processed.Load(), int64(1))

		// A fresh send after recovery still flows → credit recycled, not leaked.
		require.NoError(t, b.Send(ctx, msgin.New[any](order{ID: "y"})))
		synctest.Wait()
		assert.GreaterOrEqual(t, processed.Load(), int64(2))

		cancel()
		assert.ErrorIs(t, <-runDone, context.Canceled)
	})
}

// I1: an out-of-range OverflowPolicy must BLOCK (backpressure), never drop —
// the documented unknown→OverflowBlock contract. With n=1 and a held handler, a
// burst behind an unknown policy backpressures (nothing shed); once released,
// every message is processed and OnRetry(ErrOverflowDropped) never fires.
func TestConsumer_Overflow_UnknownPolicy_BlocksNeverDrops(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		b := memory.New(memory.WithBuffer(16))
		var processed atomic.Int64
		var dropped atomic.Int64
		release := make(chan struct{})
		h := func(context.Context, msgin.Message[order]) error {
			<-release
			processed.Add(1)
			return nil
		}
		hooks := msgin.Hooks{
			OnRetry: func(_ context.Context, _ msgin.Message[any], err error) {
				if errors.Is(err, msgin.ErrOverflowDropped) {
					dropped.Add(1)
				}
			},
		}
		c, err := msgin.NewConsumer[order](b, h,
			msgin.WithConsumerClock[order](clockwork.NewFakeClock()), // I3
			msgin.WithMaxInFlight[order](1),
			msgin.WithOverflow[order](msgin.OverflowPolicy(99)), // out of range → must behave as Block
			msgin.WithHooks[order](hooks))
		require.NoError(t, err)

		ctx, cancel := context.WithCancel(t.Context())
		runDone := make(chan error, 1)
		go func() { runDone <- c.Run(ctx) }()
		const total = 5
		for i := 0; i < total; i++ {
			require.NoError(t, b.Send(ctx, msgin.New[any](order{ID: "x"})))
		}

		synctest.Wait() // one holds the credit; the rest backpressure (buffered), NONE shed
		assert.Equal(t, int64(0), dropped.Load(), "unknown policy must block, never drop (I1)")

		close(release)
		synctest.Wait()
		assert.Equal(t, int64(total), processed.Load(), "all processed under backpressure — nothing was shed")

		cancel()
		assert.ErrorIs(t, <-runDone, context.Canceled)
	})
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `GOTOOLCHAIN=go1.25.0 go test ./... -run 'TestConsumer_Overflow' -race -v`
Expected: `TestConsumer_Overflow_DropNewest_ShedsWithoutLeak` FAILs — Task 2's `admit` always uses the blocking `acquire`, so nothing is dropped (`dropped == 0`) and the burst backpressures instead. (`TestConsumer_Overflow_UnknownPolicy_BlocksNeverDrops` may pass under Task 2's always-block `admit`; after Task 6 it stays green precisely because `default` blocks — guarding the I1 inversion.)

- [ ] **Step 3: Write minimal implementation**

Replace the credit-acquire block in `admit` with the policy-aware version:

```go
	// Credit acquisition, per the overflow policy (composition order: after
	// rate-limit + breaker-admit). The shed policies are enumerated EXPLICITLY so
	// that default (OverflowBlock AND any out-of-range value) backpressures rather
	// than silently drops — matching the documented unknown→Block contract (I1).
	switch c.overflow {
	case OverflowDropNewest, OverflowDropOldest, OverflowReject:
		// Shed on contention (all three map to DropNewest on a streaming source —
		// ADR 0008 D9). requeue=false: a genuine drop, never a re-enqueue (I2).
		if !gate.tryAcquire() {
			c.safeFire(c.hooks.OnRetry, settleCtx, d.Msg, ErrOverflowDropped)
			c.finish(d.Nack(settleCtx, false, 0)) // genuine drop; no credit held to release
			return true                           // keep ingesting
		}
	default: // OverflowBlock and any out-of-range OverflowPolicy → backpressure
		if err := gate.acquire(ctx); err != nil {
			c.finish(d.Nack(settleCtx, true, 0))
			return false
		}
	}
```

Flesh out ADR 0008 **D9** (overflow: Block backpressures; DropNewest sheds via Nack(requeue=false); DropOldest/Reject mapped to DropNewest on streaming until Plan 004 / adapter/http; NF-14 credit note — the dropped newest holds no credit, so nothing leaks).

- [ ] **Step 4: Run test to verify it passes**

Run: `GOTOOLCHAIN=go1.25.0 go test ./... -race -v`
Expected: PASS, no goroutine leaks.

- [ ] **Step 5: Commit**

```bash
git add consumer.go consumer_test.go docs/adrs/0008-resilience-flow-control-api.md
git commit -m "feat: WithOverflow (Block/DropNewest) shedding for push sources" \
  -m "DropNewest sheds via Nack(requeue=false) without leaking credit; Block backpressures; DropOldest/Reject accepted and mapped to DropNewest on streaming until a buffer stage / adapter/http lands (spec §7.4.6, NF-14)." \
  -m "Spec: 001" -m "Plan: 003" -m "ADR: 0006, 0008"
```

---

### Task 7: Bounded attempt tracker — clockwork TTL sweep (fixes ADR 0007 §D5)

**Files:**
- Modify: `retry.go` (`attemptTracker`: add clock + `lastSeen` + `sweep`/`sweepLoop`; `newAttemptTracker(clock, ttl)`)
- Modify: `consumer.go` (`NewConsumer` passes the clock + `defaultAttemptTTL`; `Run` starts/joins the sweep goroutine)
- Modify: `docs/adrs/0008-resilience-flow-control-api.md` (flesh out D8 bounded tracker)
- Modify: `docs/adrs/0007-reliability-settlement-api.md` (§D5 "Known limitation" → Resolved by Plan 003 / ADR 0008 D8)
- Modify: `settlement_doubles_test.go` (add `controllableSource`)
- Test: `consumer_test.go` (append)

**Interfaces:**
- Consumes: `clockwork.Clock`, `defaultAttemptTTL`.
- Produces (unexported): `newAttemptTracker(clock clockwork.Clock, ttl time.Duration) *attemptTracker`; `(*attemptTracker).sweep()`; `(*attemptTracker).sweepLoop(ctx context.Context)`; entry type `{ count int; lastSeen time.Time }`.

**Design note (ADR 0007 §D5, ADR 0008 D8).** The tracker keeps evicting on terminal settle (unchanged), and additionally records `lastSeen = clock.Now()` on each `observe`. A `clockwork` ticker (`sweepLoop`, started by `Run`, joined on return) sweeps entries whose age `≥ ttl` (`defaultAttemptTTL = 5m`). This respects **NF-2**: an actively-redelivering id is re-observed each attempt — for the only Plan-003 source (memory) redelivery is immediate, and even for a future delay-parked source the gap is `≤ Backoff.Max ≪ ttl` — so a still-in-redelivery message is never swept; only ids that stopped being delivered (the distinct-id-per-delivery garbage of ADR 0007 §D5) age out. The publicly observable effect: an id that reappears **after** a `ttl` gap of silence is treated as attempt 1 again (fresh backoff), whereas one redelivered continuously keeps climbing to `MaxAttempts` — this is exactly what the two tests assert.

**Hot-path branches introduced:** `sweep` age `≥ ttl` → evict (covered by the idle-sweep test) vs `< ttl` → keep (covered by the refreshed-id keep test, M3); `sweepLoop` ticker tick → sweep vs ctx-done → return (join) — no `ttl<=0` branch (M2: removed as uncoverable dead code); `observe` records `lastSeen`; terminal evict still fires (a redelivering id keeps climbing — NF-2 preserved).

- [ ] **Step 1: Write the failing test**

```go
// settlement_doubles_test.go (append) — a source the test drives one delivery at
// a time, so it can withhold an id across a TTL sweep then re-deliver it.
type controllableSource struct {
	emit chan msgin.Delivery
}

func newControllableSource() *controllableSource {
	return &controllableSource{emit: make(chan msgin.Delivery)}
}
func (s *controllableSource) EmitsLiveValue() bool { return true }
func (s *controllableSource) Stream(ctx context.Context, out chan<- msgin.Delivery) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case d := <-s.emit:
			select {
			case out <- d:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
}
func (s *controllableSource) deliver(ctx context.Context, d msgin.Delivery) {
	select {
	case s.emit <- d:
	case <-ctx.Done():
	}
}
```

```go
// consumer_test.go (append) — the idle-entry sweep resets a re-appearing id's
// attempt count (observable via the backoff delay carried on the Nack).
//
// Runs under synctest for DETERMINISM: the sweep runs on a goroutine, so after
// clk.Advance fires the sweep ticker we must be sure sweep() has completed (and
// re-blocked) BEFORE re-delivering — otherwise a sweep-vs-deliver race would
// make it flaky under -count. synctest.Wait() gives exactly that barrier. The
// clockwork fake clock is advanced INSIDE the bubble but is independent of
// synctest's own time; the sweep ticker fires ONLY on these explicit Advances,
// and clockwork fake tickers use non-blocking sends with no background
// goroutine, so they compose cleanly with synctest.
func TestConsumer_AttemptTracker_TTLSweepResetsIdleIds(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		clk := clockwork.NewFakeClock()
		src := newControllableSource()
		const initial = 10 * time.Millisecond
		h := func(context.Context, msgin.Message[order]) error { return errors.New("always transient") }

		c, err := msgin.NewConsumer[order](src, h,
			msgin.WithConsumerClock[order](clk),
			msgin.WithRetryPolicy[order](msgin.RetryPolicy{
				Backoff: msgin.ExponentialBackoff{Initial: initial, Mult: 2.0}, // RandomizationFactor 0 => exact
			}))
		require.NoError(t, err)

		ctx, cancel := context.WithCancel(t.Context())
		runDone := make(chan error, 1)
		go func() { runDone <- c.Run(ctx) }()

		st := &settle{}
		deliverX := func() {
			src.deliver(ctx, msgin.Delivery{Msg: msgin.New[any](order{ID: "o"}, msgin.WithID("x")), Ack: st.ack, Nack: st.nack})
		}

		// First delivery of x → attempt 1 → Nack delay = Initial.
		deliverX()
		synctest.Wait()
		_, n, delays := st.snapshot()
		require.Equal(t, 1, n)
		assert.Equal(t, initial, delays[0], "first ever attempt for x → Delay(0) = Initial")

		// Advance past the TTL; synctest.Wait() makes the sweep deterministic
		// (it runs and re-blocks before we re-deliver — no sweep-vs-deliver race).
		clk.Advance(6 * time.Minute) // > defaultAttemptTTL (5m) → sweep evicts idle x
		synctest.Wait()

		// Re-deliver x: with the entry swept, it must be attempt 1 again (fresh delay).
		deliverX()
		synctest.Wait()
		_, n, delays = st.snapshot()
		require.Equal(t, 2, n)
		assert.Equal(t, initial, delays[1], "after a TTL gap x is treated as attempt 1 again (idle entry swept)")

		cancel()
		assert.ErrorIs(t, <-runDone, context.Canceled)
	})
}

// M3: the sweep's KEEP branch — an id refreshed within the TTL survives a sweep
// that evicts an idle sibling. Two ids a and b start at attempt 1; b is
// re-delivered (refreshed) inside the TTL window; when the 5m sweep fires, a
// (idle since t0) is evicted but b (age 2m) is kept — so a restarts at attempt 1
// while b keeps climbing to attempt 3. Covers sweep()'s age>=ttl (a) AND age<ttl
// (b) branches in one test.
func TestConsumer_AttemptTracker_TTLSweepKeepsRefreshedId(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		clk := clockwork.NewFakeClock()
		src := newControllableSource()
		const initial = 10 * time.Millisecond
		const mult = 2.0
		h := func(context.Context, msgin.Message[order]) error { return errors.New("always transient") }

		c, err := msgin.NewConsumer[order](src, h,
			msgin.WithConsumerClock[order](clk),
			msgin.WithRetryPolicy[order](msgin.RetryPolicy{
				Backoff: msgin.ExponentialBackoff{Initial: initial, Mult: mult},
			}))
		require.NoError(t, err)

		ctx, cancel := context.WithCancel(t.Context())
		runDone := make(chan error, 1)
		go func() { runDone <- c.Run(ctx) }()

		stA, stB := &settle{}, &settle{}
		deliver := func(st *settle, id string) {
			src.deliver(ctx, msgin.Delivery{
				Msg: msgin.New[any](order{ID: "o"}, msgin.WithID(id)),
				Ack: st.ack, Nack: st.nack,
			})
		}

		deliver(stA, "a") // a attempt 1, lastSeen = t0
		deliver(stB, "b") // b attempt 1, lastSeen = t0
		synctest.Wait()

		clk.Advance(3 * time.Minute) // < TTL: sweep ticker (5m) does NOT fire
		synctest.Wait()
		deliver(stB, "b") // b attempt 2, lastSeen(b) = t0+3m (refreshed within TTL)
		synctest.Wait()

		clk.Advance(2 * time.Minute) // total 5m → sweep fires: a age 5m evicted, b age 2m kept
		synctest.Wait()

		deliver(stA, "a") // a was swept → attempt 1 again → delay Initial
		synctest.Wait()
		_, nA, delaysA := stA.snapshot()
		require.Equal(t, 2, nA)
		assert.Equal(t, initial, delaysA[1], "a idled past TTL → swept → attempt 1 again")

		deliver(stB, "b") // b was kept → attempt 3 → delay Initial*Mult^2 (never reset)
		synctest.Wait()
		_, nB, delaysB := stB.snapshot()
		require.Equal(t, 3, nB)
		assert.Equal(t, time.Duration(float64(initial)*mult*mult), delaysB[2], "b refreshed within TTL → kept → attempt 3 keeps climbing")

		cancel()
		assert.ErrorIs(t, <-runDone, context.Canceled)
	})
}

// NF-2: a continuously-redelivering id is NEVER reset by the sweep — it climbs to
// the dead-letter after MaxAttempts even though sweeps fire during redelivery.
func TestConsumer_AttemptTracker_NF2_ActiveIdNotSwept(t *testing.T) {
	st := &settle{}
	src := &reemittingSource{st: st, id: "poison", payload: order{ID: "o"}}
	dlq := &recordingSink{}
	h := func(context.Context, msgin.Message[order]) error { return errors.New("always transient") }

	c, err := msgin.NewConsumer[order](src, h,
		msgin.WithRetryPolicy[order](msgin.RetryPolicy{MaxAttempts: 3, DeadLetter: dlq}))
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	runDone := make(chan error, 1)
	go func() { runDone <- c.Run(ctx) }()

	// memory-style immediate redelivery keeps the id fresh; it must dead-letter at
	// attempt 3, proving the sweep never reset a still-in-redelivery id.
	require.Eventually(t, func() bool { return dlq.count() == 1 }, 2*time.Second, 5*time.Millisecond)
	_, nacks, _ := st.snapshot()
	assert.Equal(t, 2, nacks, "attempts 1 and 2 Nacked; 3 dead-lettered — count never reset (NF-2)")

	cancel()
	assert.ErrorIs(t, <-runDone, context.Canceled)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `GOTOOLCHAIN=go1.25.0 go test ./... -run 'TestConsumer_AttemptTracker' -race -v`
Expected: FAIL — with no sweep yet, `clk.Advance` fires nothing, so after the gap `x` (and `a`) are attempt 2 (`delays[1] == Initial*Mult`), not attempt 1 — both `TTLSweepResetsIdleIds` and `TTLSweepKeepsRefreshedId` fail their attempt-1 assertion. (The NF-2 test already passes; it guards against a regression from the sweep.)

- [ ] **Step 3: Write minimal implementation**

```go
// retry.go — swept attempt tracker.
type attemptEntry struct {
	count    int
	lastSeen time.Time
}

type attemptTracker struct {
	clock clockwork.Clock
	ttl   time.Duration
	mu    sync.Mutex
	m     map[string]attemptEntry
}

func newAttemptTracker(clock clockwork.Clock, ttl time.Duration) *attemptTracker {
	return &attemptTracker{clock: clock, ttl: ttl, m: make(map[string]attemptEntry)}
}

// observe records one more attempt for id, refreshes its lastSeen, and returns
// the new count (1-based).
func (t *attemptTracker) observe(id string) int {
	t.mu.Lock()
	defer t.mu.Unlock()
	e := t.m[id]
	e.count++
	e.lastSeen = t.clock.Now()
	t.m[id] = e
	return e.count
}

// evict forgets id (call only on terminal settle).
func (t *attemptTracker) evict(id string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.m, id)
}

// sweep reclaims entries idle for >= ttl. An actively-redelivering id is
// re-observed each attempt (gap <= Backoff.Max << ttl), so it is never swept
// mid-flight (NF-2); only ids that stopped arriving age out.
func (t *attemptTracker) sweep() {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := t.clock.Now()
	for id, e := range t.m {
		if now.Sub(e.lastSeen) >= t.ttl {
			delete(t.m, id)
		}
	}
}

// sweepLoop runs the periodic sweep until ctx is done. ttl is always the
// positive defaultAttemptTTL (NewConsumer's only caller passes it; there is no
// public opt-out knob — ADR 0008 D8/decision 7), so there is deliberately NO
// ttl<=0 guard: it would be uncoverable dead code under the blackbox coverage
// gate (M2 — YAGNI; a knob can be added non-breakingly later if a need appears).
func (t *attemptTracker) sweepLoop(ctx context.Context) {
	ticker := t.clock.NewTicker(t.ttl)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.Chan():
			t.sweep()
		}
	}
}
```

Add `import "github.com/jonboulle/clockwork"` to `retry.go`. In `NewConsumer`, build the tracker with the resolved clock + TTL: `tracker: newAttemptTracker(cfg.clock, defaultAttemptTTL)`.

**C1 — the sweep must live for the WHOLE `Run` (start → return), including the drain.** The sweep goroutine is given a **Run-scoped context detached from the parent** (`context.WithoutCancel(ctx)`) and cancelled **explicitly at the very end of `Run`**, then joined — *not* keyed off the parent `ctx`. This is load-bearing for test determinism (see the sweep-ticker ordering invariant below): if the sweep exited on parent-cancel, its ticker would deregister from the fake clock *concurrently* with the drain registering its `clock.After`, so the blocker count during the drain would race between 1 and 2 and no fixed `BlockUntil(N)` would be safe (it could return early **or** hang). Keeping the sweep alive until after the drain makes the sweep ticker a **stable, always-present waiter** for `Run`'s entire lifetime, so it is a constant `+1` the tests account for deterministically.

In `Run`, add the sweep under the existing `ingressWG` but with the detached, Run-scoped context, and cancel it right before `ingressWG.Wait()`:

```go
	// Run-lifetime attempt-tracker sweep (C1). Detached from the parent so a
	// parent cancel does NOT deregister its clock ticker mid-drain; cancelled
	// explicitly at Run's end. The ingest goroutine (also on ingressWG) has
	// already exited by then (rawCh closed), so ingressWG.Wait() joins both.
	sweepCtx, cancelSweep := context.WithCancel(context.WithoutCancel(ctx))
	ingressWG.Add(1)
	go func() {
		defer ingressWG.Done()
		c.tracker.sweepLoop(sweepCtx) // registers the sweep ticker on c.clock at Run start (waiter #0)
	}()

	// ... existing Run body (ingest goroutine, workers, Stream, drain select) ...

	// After the drain has completed (<-done, whether normally or via the deadline
	// path), stop the Run-lifetime sweep and join everything.
	cancelSweep()
	ingressWG.Wait() // joins ingest (exited on rawCh close) + sweep (exits on sweepCtx) — goleak-clean
	return streamErr
```

**Sweep-ticker ordering invariant (state in ADR 0008 D8, C1(c)).** The tracker sweep ticker is created at `Run` start and is the **first (`#0`) waiter** registered on the consumer clock, and it stays registered until `Run` returns. Therefore any fake-clock test that `BlockUntil(N)`s on the consumer clock must count it: a test waiting for its *own* timer to register waits for `N = 1 (sweep ticker) + (number of other consumer-clock timers it expects)`. This is why the C1 bump step below adds `+1` to every pre-existing consumer-fake-clock `BlockUntil`.

Update `docs/adrs/0007-reliability-settlement-api.md` §D5: change the "Known limitation — attemptTracker unbounded growth (triaged; deferred to Plan 003)" heading/body to note it is **Resolved by Plan 003** via the clockwork TTL sweep (ADR 0008 D8), NF-2 preserved. Flesh out ADR 0008 **D8** (TTL sweep design; the NF-2 argument; TTL-≫-redelivery-cadence invariant; default 5m; observable reset-after-idle behavior; **and the sweep-ticker ordering invariant** from C1(c): the sweep ticker is the always-present waiter `#0` on the consumer clock for `Run`'s whole lifetime).

**Step 3 (cont.) — C1: bump every consumer-fake-clock `BlockUntil` count by +1 (mandatory).**

Adding the always-on, Run-lifetime sweep registers the sweep ticker as a permanent extra waiter (`#0`) on the **consumer** clock from `Run` start until `Run` returns. Every pre-existing test that `BlockUntil`s on the consumer clock therefore counts one more waiter than before. Left unbumped, `BlockUntilContext(ctx, 1)` is satisfied by the sweep ticker alone and returns **before** the target timer (drain/handler/breaker) registers, so `Advance` races that registration and the test intermittently hangs (this is the C1 regression). Revisit and bump each by +1, adding a comment documenting the `+1`:

| test | file | before | after |
|---|---|---|---|
| `TestConsumer_Shutdown_TransientNackDuringDrain` | consumer_test.go (Plan 002) | `BlockUntilContext(ctx, 1)` | `BlockUntilContext(ctx, 2)` |
| `TestConsumer_ShutdownDeadlineExpiry_NacksInflightAndBuffered` | consumer_test.go (Plan 002) | `BlockUntilContext(ctx, 1)` | `BlockUntilContext(ctx, 2)` |
| `TestConsumer_MaxInFlight_ShutdownWithIngestHeldSurplus_NoLeak` | consumer_test.go (Task 2, I4) | `BlockUntilContext(ctx, 1)` | `BlockUntilContext(ctx, 2)` |
| `TestConsumer_HandlerTimeout_CancelsStuckHandlerAndRetries` | consumer_test.go (Task 4) | `BlockUntil(1)` | `BlockUntil(2)` |
| `TestConsumer_CircuitBreaker_GatesDispatchAndRecovers` | consumer_test.go (Task 5) | `BlockUntil(1)` | `BlockUntil(2)` |

At each bumped site add: `// +1 waiter is the always-on tracker sweep ticker (registered at Run start, C1)`.

**Not bumped (verify):** Task 7's own two sweep tests use `synctest.Wait()` (no consumer-clock `BlockUntil`). `TestConsumer_RateLimit_GatesIngress` and `TestTokenBucket_*`/`TestCircuitBreaker_*` `BlockUntil` on a **separate** fake clock (the limiter/breaker's own), NOT the consumer clock (they do not inject `WithConsumerClock`), so the sweep ticker is not on that clock — leave them at their current counts. `TestConsumer_CircuitBreaker_PermanentErrorsDoNotTrip` (Task 5, M4) injects the consumer clock but never `BlockUntil`s it — no change. This bump is thus the **complete** set of affected sites.

- [ ] **Step 4: Run test to verify it passes**

Run: `GOTOOLCHAIN=go1.25.0 go test ./... -race -v`
Expected: PASS, no goroutine leaks (the sweep goroutine exits on ctx cancel and is joined).

- [ ] **Step 5: Commit**

```bash
git add retry.go consumer.go settlement_doubles_test.go consumer_test.go docs/adrs/0007-reliability-settlement-api.md docs/adrs/0008-resilience-flow-control-api.md
git commit -m "feat: bounded attempt tracker via clockwork TTL sweep (fixes ADR 0007 D5)" \
  -m "Idle id entries age out after defaultAttemptTTL without ever evicting an id still in redelivery (NF-2); resolves the unbounded-growth limitation. Deterministic under a fake clock." \
  -m "Spec: 001" -m "Plan: 003" -m "ADR: 0006, 0007, 0008"
```

---

### Task 8: End-to-end composition, module hygiene, whole-branch delivery gate

**Files:**
- Create: `example_flowcontrol_test.go` (runnable `Example` — doubles as godoc)
- Verify: `go.mod`/`go.sum` unchanged except test-only additions; ADR 0008 complete
- Test: whole-suite race + vet + fmt + coverage

**Design note.** No new forced dependency should have entered the core (`sync`, `testing/synctest`, `clockwork` only). This task adds a documentation-grade example composing several governors and runs the whole-branch gate (CLAUDE.md §5): `/code-review` + `/security-review` over `main..HEAD`, resolve/triage findings, confirm coverage, then the full `-race` suite (with `-count` for the concurrency-heavy flow-control tests).

- [ ] **Step 1: Write the failing test (runnable example)**

```go
// example_flowcontrol_test.go
package msgin_test

import (
	"context"
	"fmt"

	"github.com/kartaladev/msgin"
	"github.com/kartaladev/msgin/adapter/memory"
)

// ExampleConsumer_flowControl composes the flood defenses: bounded in-flight, a
// rate limit, a handler timeout, a circuit breaker, and an overflow policy.
func ExampleConsumer_flowControl() {
	rl, _ := msgin.NewTokenBucket(100, 10)
	src := memory.New(memory.WithBuffer(1))
	handler := func(context.Context, msgin.Message[string]) error { return nil }

	c, err := msgin.NewConsumer[string](src, handler,
		msgin.WithMaxInFlight[string](64),
		msgin.WithRateLimit[string](rl),
		msgin.WithHandlerTimeout[string](5_000_000_000), // 5s
		msgin.WithCircuitBreaker[string](msgin.NewCircuitBreaker()),
		msgin.WithOverflow[string](msgin.OverflowDropNewest),
	)
	if err != nil {
		fmt.Println("construct:", err)
		return
	}
	_ = c
	fmt.Println("configured")
	// Output: configured
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `GOTOOLCHAIN=go1.25.0 go test ./... -run 'Example' -v`
Expected: compiles and matches once Tasks 1–7 are in place (if it already passes, that is acceptable — the example is a doc/coverage artifact, not a red-first requirement).

- [ ] **Step 3: Module hygiene + coverage check**

Run:
```bash
GOTOOLCHAIN=go1.25.0 go mod tidy
GOTOOLCHAIN=go1.25.0 go test ./... -race
GOTOOLCHAIN=go1.25.0 go test ./... -coverprofile=/tmp/cov.out && GOTOOLCHAIN=go1.25.0 go tool cover -func=/tmp/cov.out | tail -1
GOTOOLCHAIN=go1.25.0 go vet ./... && gofmt -l .
GOTOOLCHAIN=go1.25.0 go mod why github.com/jonboulle/clockwork
```
Expected: suite green race-clean; `package msgin` coverage ≥ 85%; `vet`/`gofmt` silent; `go.mod` shows **no** new require (no `x/time/rate`, no `sony/gobreaker`); only `clockwork` reachable from non-test core.

- [ ] **Step 4: Whole-branch review gate**

Run `/code-review` and `/security-review` over `main..HEAD`; resolve or triage every finding; re-run the affected review and `go test ./... -race`. Confirm the coverage gate — every branch enumerated per task, and in particular the credit-accounting hot path (acquire/tryAcquire/release-once, overflow drop, breaker ingress+dispatch gate, shutdown-abandon), the handler-timeout cancel, the breaker half-open explicit wakeup, the breaker poison-does-not-trip case (M4), the overflow unknown-policy-blocks case (I1), the sweep keep-vs-evict pair (M3), the ingest-held-surplus shutdown (I4), and the TTL-sweep reset-vs-NF-2 pair — has a covering case.

**`-count` stability (I3, mandatory).** Run `GOTOOLCHAIN=go1.25.0 go test . -race -count=20` and confirm **zero** flakes. The `synctest`-driven tests are the ones at risk and MUST be stable: `TestConsumer_MaxInFlight_BoundsClaimedUnsettled`, `TestConsumer_Overflow_DropNewest_ShedsWithoutLeak`, `TestConsumer_Overflow_UnknownPolicy_BlocksNeverDrops` (all three inject a **never-advanced** fake consumer clock so the Task-7 sweep ticker is a fake ticker that never fires and cannot perturb `synctest.Wait()` — I3), and `TestConsumer_AttemptTracker_TTLSweepResetsIdleIds` / `TestConsumer_AttemptTracker_TTLSweepKeepsRefreshedId` (which advance the fake consumer clock inside the bubble and gate the sweep with `synctest.Wait()` — deterministic, no sweep-vs-deliver race). Also confirm the C1-bumped fake-clock `BlockUntil` tests are stable at `-count=20`.

- [ ] **Step 5: Commit**

```bash
git add example_flowcontrol_test.go go.mod go.sum docs/adrs/0008-resilience-flow-control-api.md
git commit -m "test: runnable flow-control example; finalize ADR 0008 and module hygiene" \
  -m "Whole-branch gate green (code-review + security-review over main..HEAD, -race clean, coverage >= 85%). Core remains stdlib + clockwork; token bucket and breaker are first-party." \
  -m "Spec: 001" -m "Plan: 003" -m "ADR: 0006, 0008"
```

---

## ADR 0008 outline (`docs/adrs/0008-resilience-flow-control-api.md`)

Author with Nygard sections (Title, Status, Context, Decision, Consequences), Related: ADR 0002, 0004, 0006, 0007. **Decisions:**

- **D1 — Always-on, finite credit gate (Task 1).** `WithMaxInFlight(n)` (default `defaultMaxInFlight = 1024`, never unbounded); `n<1` → `ErrInvalidMaxInFlight`. The credit gate is the load-bearing flood defense and is always active (spec §7.4.1). **Unset-vs-explicit-0 (C2):** because the default (1024) is non-zero, a `maxInFlightSet bool` flag (mirroring `codecSet`) distinguishes "unset → default" from an explicit `WithMaxInFlight(0)`, which is a rejected caller error — a bare zero cannot double as the unset sentinel. The flag pattern is applied only to `WithMaxInFlight` (the sole new option with a meaningful/ambiguous zero); nil limiter/breaker, `handlerTimeout<=0`, and `OverflowBlock=0` are unambiguous and need none.
- **D2 — Option ergonomics (Task 1).** Nil `RateLimiter`/`CircuitBreaker` are no-ops (the option guards nil, so no nil-call at runtime); the `OverflowPolicy` enum is closed, unknown values render **and behave** as `OverflowBlock` — the Task-6 overflow `switch` enumerates the shed policies explicitly and its `default` blocks, so an out-of-range value backpressures rather than silently drops (I1).
- **D3 — Credit model = size-n semaphore, acquired at ingress (Task 2).** A buffered `chan struct{}` of capacity `n` is the whole bound (realizing the spec's "buffer of size n as the credit pool" precisely). A two-stage pipeline `Stream→rawCh(unbuffered)→ingest→workerCh(unbuffered)→workers`; a delivery reaches a worker only after acquiring a credit, so **claimed-but-unsettled (credit-holding) ≤ n strictly** (no over-pull, NF-4). **Precise slack (M5):** the weaker "pulled-from-source-but-not-yet-settled" count is **not** `n+1` — it can reach `n+2`, because two deliveries can sit *off* the credit pool at once: one parked in `ingest` on `acquire`, and one parked in the source's `Stream` trying to send onto the unbuffered `rawCh`. Both hold no credit, so the `≤ n` credit bound is intact; they are just the (bounded, ≤ 2) read-ahead slack of the pipeline.
- **D4 — Release-first settlement wrapping (Task 2).** Each admitted delivery carries `sync.OnceFunc(gate.release)`; `manage` wraps `Ack`/`Nack` to release **before** the original closure, so a `Nack(requeue)` that synchronously re-injects (memory) cannot deadlock at `n=1`, and a Nacked message never pins its slot (NF-5). Idempotent release (OnceFunc + a worker `defer` net) makes double-release impossible. **The Plan 002 settlement switch is untouched.**
- **D5 — RateLimiter interface + token-bucket default (Task 3).** `RateLimiter{ Wait(ctx) error }`; dependency-free clockwork token bucket (`NewTokenBucket(rps, burst)`, `ErrInvalidRateLimit`); composition order rate-limit → credit gate; `x/time/rate` is the real-clock plug-in.
- **D6 — Handler timeout via clockwork context (Task 4).** `WithHandlerTimeout(d)` derives a `clock.AfterFunc`-cancelled handler context; timed-out = `context.Canceled`/`DeadlineExceeded` → classified transient → Nack; bounds the Plan 002 cooperative-handler residual.
- **D7 — CircuitBreaker gates ingress AND dispatch (Task 5).** `CircuitBreaker{ Allow; Record(success); HalfOpen() <-chan struct{} }`; default clockwork breaker; open pauses ingress via a subscribe-then-recheck park (missed-wakeup-free) AND makes workers Nack buffered work (releasing credit) instead of dispatching (NF-10); `HalfOpen` closes-and-remints on the open→half-open transition for explicit wakeup; `dispatch` returns its classification so only transient failures `Record(false)` (a bad message does not trip the breaker); gobreaker plugs in via `OnStateChange`.
- **D8 — Bounded attempt tracker via TTL sweep (Task 7; resolves ADR 0007 §D5).** `observe` stamps `lastSeen`; a clockwork ticker sweeps entries idle `≥ defaultAttemptTTL` (5m). NF-2 preserved because a still-in-redelivery id is re-observed within a gap `≤ Backoff.Max ≪ ttl`; only distinct-id garbage ages out. Observable: an id re-appearing after a `ttl` silence restarts at attempt 1. The sweep is **always on** (no public opt-out knob → no `ttl<=0` disable branch — M2). **Sweep-ticker ordering invariant (C1):** the sweep runs under a Run-scoped context detached from the parent and cancelled only at `Run`'s end, so its ticker is the **first (`#0`), always-present waiter on the consumer clock for `Run`'s entire lifetime (including the drain)**. This makes the extra waiter a deterministic constant `+1` that every consumer-fake-clock `BlockUntil(N)` test must account for (bump N by 1); without the detached lifetime the ticker would deregister on parent-cancel and race the drain's timer registration, making no fixed `N` safe.
- **D9 — Overflow semantics + streaming mapping (Task 6).** `Block` backpressures (default; and any out-of-range `OverflowPolicy` → `Block`, per the I1 switch, matching D2/`String()`); `DropNewest` sheds via `Nack(requeue=false)` + `OnRetry(ErrOverflowDropped)`, no credit leaked (the newest holds no credit). **Supersedes spec NF-14's "Nacked so an at-least-once source can redeliver" (I2):** in Plan 003 the only source is memory (at-most-once), and shedding uses `requeue=false` — a **genuine drop**, not a redeliver (a `requeue=true` would re-enqueue-and-respin, defeating load-shedding). So a shed message is genuinely lost on memory (at-most-once, documented per adapter); the redeliver-on-drop semantics for at-least-once sources (redis PEL, JetStream `Nak`/`Term`) are **deferred to the adapter plans (Plan 006)** and flagged in "Notes for Plan 006". `DropOldest`/`Reject` are accepted for API stability but behave as `DropNewest` on a streaming source until a discrete buffer stage (Poller, Plan 004) / HTTP semantics (`adapter/http`) exist.

## Design decisions to ratify (ADR 0008) — recommended defaults

1. **`defaultMaxInFlight` value** → **Recommend 1024** (finite, ample headroom so existing single-message/small-burst flows are unaffected; production users set an explicit `WithMaxInFlight`). *Spec §7.4 requires "finite, never unbounded" but does not fix the number — this is the one genuinely unspecified value.*
2. **Credit realized as a semaphore, not a literal size-n Go channel buffer** → **Recommend the semaphore.** A Go channel frees its slot when a worker *pulls*, before settle, so it cannot express "occupies a slot until terminal settle"; a size-n semaphore released at settle does, with the same bound and no double-count. Faithful clarification of the spec's "buffer = credit pool" phrasing.
3. **Release-first vs release-after-settle** → **Recommend release-first** (wrap Ack/Nack). Release-after-dispatch-return deadlocks at `n=1` for any adapter whose `Nack(requeue)` synchronously re-injects (memory). Non-negotiable for correctness.
4. **`CircuitBreaker` interface shape (with `HalfOpen()` channel)** → **Recommend the three-method interface.** The `HalfOpen` channel is what makes the explicit, missed-wakeup-free ingress park possible; it is satisfiable by a gobreaker adapter via `OnStateChange`. Alternative (poll `Allow()` on a ticker) is simpler but not "explicit" and burns a goroutine spinning — rejected.
5. **`DropOldest`/`Reject` mapped to `DropNewest` on streaming in Plan 003** → **Recommend the mapping, flagged.** True `DropOldest` needs a discrete buffer to pick a victim (Poller, Plan 004); `Reject` needs HTTP request/response (`adapter/http`). Shipping the enum now keeps the public API stable; the mapping is documented, not silent. Alternative — omit the two enum values until implementable — churns the public API later. 
6. **`defaultAttemptTTL` value (5m) and the TTL-≫-redelivery-cadence invariant** → **Recommend 5m.** It vastly exceeds any in-process redelivery gap (memory: immediate; future delay-park: `≤ Backoff.Max`), so NF-2 holds. If a future adapter's redelivery cadence could approach the TTL, that adapter must use the native `delivery-count` header (which bypasses the tracker) — documented on the header constant.
7. **Sweep as an always-on internal (no public `WithAttemptTTL`)** → **Recommend internal-only (YAGNI).** The default is safe and tests drive it via the injected consumer clock; a public knob can be added non-breakingly if a real need appears.

## Self-Review

**Spec coverage (§7.4):**
- §7.4.1 credit gate — acquire at ingress, release at every terminal settle incl. Nack (NF-4/NF-5), finite default, idempotent release → Tasks 1 + 2. ✓
- §7.4.2 rate limit — `RateLimiter` + clockwork token-bucket default, composition order → Task 3. ✓
- §7.4.4 handler timeout — clockwork-derived ctx, timed-out = transient → Task 4. ✓
- §7.4.5 circuit breaker — gates ingress AND dispatch (NF-10), explicit half-open wakeup, buffered Nack releases credit → Task 5. ✓
- §7.4.6 overflow — Block/DropNewest with dropped-Nack + credit integrity; unknown→Block (I1); `requeue=false` genuine drop superseding NF-14 for memory (I2); DropOldest/Reject mapped → Task 6. ✓
- ADR 0007 §D5 attemptTracker unbounded growth — bounded via clockwork TTL sweep respecting NF-2, always-on (no dead ttl<=0 branch, M2), Run-lifetime sweep with the C1 ordering invariant → Task 7. ✓
- Audit failure classes each an explicit test: (1) no over-pull past n → `TestConsumer_MaxInFlight_BoundsClaimedUnsettled`; (2) no leak / no double-release → same test's recycle assertion + OnceFunc + `TestConsumer_Overflow_DropNewest_ShedsWithoutLeak`; (3) Nack releases slot, no stall/deadlock → `TestConsumer_MaxInFlight_NackReleasesSlot_NoDeadlock`; (4) breaker half-open explicit wakeup → `TestCircuitBreaker_OpensClosesAndHalfOpenWakeup`; (5) handler-timeout cancels a stuck handler, no leak → `TestConsumer_HandlerTimeout_CancelsStuckHandlerAndRetries`; (6) all clockwork/synctest-driven (no real sleeps); (7) goleak on every path (TestMain). ✓
- Audit-revision fixes each with a covering test: C2 explicit `WithMaxInFlight(0)`→`ErrInvalidMaxInFlight` → `TestNewConsumer_FlowControlValidation`; I1 unknown-policy blocks → `TestConsumer_Overflow_UnknownPolicy_BlocksNeverDrops`; I4 ingest-held-surplus shutdown → `TestConsumer_MaxInFlight_ShutdownWithIngestHeldSurplus_NoLeak`; M3 sweep keep-branch → `TestConsumer_AttemptTracker_TTLSweepKeepsRefreshedId`; M4 poison-does-not-trip-breaker → `TestConsumer_CircuitBreaker_PermanentErrorsDoNotTrip`. C1 (sweep-ticker `BlockUntil` +1) is verified by the whole fake-clock suite staying green at `-count=20`. ✓
- **Deferred (documented):** Poller / `PollingSource` credit-at-FETCH → Plan 004 (Plan 003 is StreamingSource/memory only; credit pool = the ingress semaphore); true `DropOldest`/`Reject` → Plan 004 / `adapter/http`; redeliver-on-overflow-drop for at-least-once sources → Plan 006 (I2); any concrete external adapter → later plans.

**Placeholder scan:** the Task 5 `dispatch` returns are enumerated per path in the M4 table (the switch already holds the values); the Task 6 overflow `switch` explicitly enumerates the shed cases with `default` blocking (I1 — not a catch-all shed). No `TODO`/"handle edge cases"/unshown code remain.

**Type consistency:** `creditGate`/`managedDelivery`/`manage`/`ingest`/`admit`/`process` names are used identically across Tasks 2–6; `admit`'s body grows monotonically (credit → +rate-limit → +breaker → +overflow) with each insertion point stated; the Task 6 overflow arms are enumerated-shed vs `default`-block (I1); `dispatch` gains its `error` return in Task 5 and `process` consumes it as the breaker health signal derived from the transient-failure signal only (M4); `newAttemptTracker(clock, ttl)` signature (Task 7) matches its `NewConsumer` call site; `consumerConfig.maxInFlightSet` (Task 1, C2) gates the unset-vs-explicit-0 resolution; `HalfOpen`/`Allow`/`Record` match the `CircuitBreaker` interface (Task 1) and the default `breaker` (Task 5). `WithConsumerClock` (Plan 002) supplies the clock the handler-timeout, breaker default, and tracker sweep all share in tests — and its sweep ticker is the C1 `+1` waiter every consumer-fake-clock `BlockUntil` accounts for.

## Notes for Plan 004 (Poller / PollingSource — spec §7.1/§7.4.1)

Plan 004 adds the shared **Poller** driving `PollingSource`, where **credit is acquired at FETCH** (`fetch min(maxBatch, n − inFlight)`, `inFlight += k`), not at ingress — the poller skips polling at zero credit and lets the surplus wait durably in the source (the true pull-model flood defense). The credit gate from this plan becomes shared: for streaming, `acquire` at ingress (this plan); for polling, the poller consults the same gate's free count before fetching. That is also where a discrete buffer stage exists, so **true `OverflowDropOldest`** (evict the oldest buffered row) and the in-process **delay-park** (finally honoring the backoff delay for adapters like memory that ignore it) can land. `adapter/http` (a later plan) realizes **`OverflowReject`** (503/429) and the outbound cenkalti retry loop (ADR 0005).

## Notes for Plan 006 (at-least-once wire adapters — redis / nats)

- **Redeliver-on-overflow-drop (I2, deferred).** Plan 003's overflow shedding uses `Nack(requeue=false)` — a genuine drop, correct for the at-most-once memory source but a **loss** by definition. For at-least-once wire sources (redis Streams PEL, NATS JetStream) a shed message should instead be **left un-acked / `Nak`/`Term`ed so the broker redelivers it later** (or routes it per the broker's max-deliver/DLQ policy), rather than dropped. This is the semantics spec NF-14 originally described; ADR 0008 D9 supersedes NF-14 for Plan 003's memory-only scope and defers the at-least-once redeliver-on-drop behavior to here. When these adapters land, revisit `admit`'s shed path so the drop maps to the adapter's native redelivery contract (likely via the `NativeReliability` seam), and document the per-adapter overflow guarantee.
- **Distinct-id-per-delivery + attempt tracker.** These sources may mint a fresh id per delivery; pair them with the native `msgin.delivery-count` header (which bypasses the runtime tracker) so the TTL sweep never has to reason about their cadence — see ADR 0008 D8 / decision 6.
