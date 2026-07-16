# Reliability — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace Plan 001's minimal `dispatch` (Ack-on-success / blind Nack-requeue-on-error) with the spec §7 reliability engine for the **streaming-source** path: a guarded settlement switch (permanent → invalid sink, exhausted-finite → dead-letter, else → Nack with backoff delay), attempt counting, a closed-form backoff, observability (`*slog.Logger` + `Hooks`), and bounded graceful shutdown.

**Architecture:** The runtime owns reliability (ADR 0002). A recovered handler panic and a decode failure become classified errors; a per-message classifier (`isPermanent`) and a per-delivery attempt count (native `msgin.delivery-count` header, else a runtime tracker) drive the exact settlement branch order from spec §7. Backoff is a stateless, attempt-indexed closed form (spec §8) — **not** `cenkalti/backoff`, which by ADR 0005 enters the tree only via `adapter/http`, so the core runtime stays `stdlib + clockwork`. Shutdown is `clockwork`-driven; settles run under a detached context so they complete while the flow drains.

**Tech Stack:** Go 1.25, `github.com/jonboulle/clockwork` (injectable time — already a core dep), `log/slog` (stdlib, observability), `go.uber.org/goleak` (test-only), `github.com/stretchr/testify` (test-only). **No new forced dependency** — `cenkalti/backoff/v4` is deliberately *not* imported here (see ADR 0007 / spec §8).

**Traceability:** Implements [spec 001](../specs/001-messaging-core.md) §7 (runtime settlement switch, shutdown, observability) and §8 (backoff); see ADR [0002](../adrs/0002-adapter-spi.md) (settlement/reliability), [0005](../adrs/0005-cenkalti-backoff-dependency.md) (backoff placement), and the **new ADR 0007** (reliability & settlement API — authored/extended within this plan's commits, §"ADR 0007 outline" below). Builds on [plan 001](001-core-foundation.md).

## Global Constraints

- **Go 1.25 exact.** `go.mod` has `go 1.25`; build/test with `GOTOOLCHAIN=go1.25.0`. No features newer than 1.25. (`context.WithoutCancel`, `math/rand/v2`, `log/slog` are all ≤1.25.)
- **Core = stdlib + `clockwork` only.** No other third-party import in non-test code. In particular **do not** import `cenkalti/backoff` in the runtime — the redelivery path is closed-form (ADR 0005/0007). Test-only deps (`testify`, `goleak`) are fine.
- **Blackbox tests only.** Every `_test.go` is `package msgin_test` (or `<pkg>_test`) and exercises the exported API. Export any sentinel a test must `errors.Is`. Unexported helpers (`isPermanent`, `attemptTracker`, the settlement switch) are tested **through** the public surface (`Run`, `NewConsumer`, recording doubles).
- **Table tests use the assert-closure form** (project `table-test` skill): each case carries `assert func(t *testing.T, …)`, never `want`/`wantErr` fields. Use `t.Context()`, not `context.Background()`.
- **Every consumer/`Run` test asserts no goroutine leaks** with `goleak` (`TestMain` already lives in `consumer_test.go`).
- **Injectable time via `clockwork.Clock`** used directly; tests use `clockwork.NewFakeClock()`. The consumer clock option is `WithConsumerClock` (the bare `WithClock` name is already taken by the `MessageOption` in `message.go` — same package, so the names must differ).
- **Library, no `os.Exit`/`panic`/`log.Fatal` on caller input; no global logger.** Accept an injected `*slog.Logger` (default: a discard logger, never `slog.Default()`).
- **Coverage gate (mandatory).** Every settlement branch, every `RetryPolicy.Validate` branch, the panic path, the decode-failure→invalid path (I2) and the handler-returned-`ErrPayloadDecode`→invalid path (I7), the divert-send-failure path **and its non-zero backoff delay** (I6), the DLQ path, the native-`delivery-count` path, each backoff edge case **including both overflow sub-arms** (I4), the transient-Nack-during-drain deadlock regression (C1), the shutdown deadline-expiry path for **both in-flight and buffered** deliveries (I5), the `defaultShutdownTimeout` (unset) path, and each observability hook actually firing (I3) each has ≥1 covering test case. Target ≥85% on `package msgin`.
- **Module path:** `github.com/kartaladev/msgin`. Package `msgin` at repo root; memory adapter at `adapter/memory`.
- **Branch:** `feat/reliability` (already checked out off `main`). Per-task commits are pre-authorized once this plan is approved and an SDD/executing-plans mode is chosen; `push`/merge/branch-deletion still need explicit approval.

---

## File Structure

New files (all `package msgin` at repo root):
- `reliability.go` — `Permanent(err)`, unexported `permanentError`, `isPermanent`, `noNativeReliability`. (The permanent-error classifier + native-capability default.)
- `backoff.go` — `BackoffStrategy` interface, `ExponentialBackoff` default (closed-form + optional jitter).
- `retry.go` — `RetryPolicy` struct + `Validate()`, `Hooks` struct, the unexported `attemptTracker`.

Modified files:
- `errors.go` — add `ErrHandlerPanic`, `ErrNoDeadLetter`, `ErrInvalidMaxAttempts`.
- `consumer.go` — new options (`WithRetryPolicy`, `WithInvalidMessageSink`, `WithLogger`, `WithHooks`, `WithShutdownTimeout`, `WithConsumerClock`); new `consumer` fields; `NewConsumer` validation wiring; the guarded settlement switch replacing `dispatch`; `safeHandle` returns `ErrHandlerPanic`; `Run` gains bounded shutdown.

New test files (all `package msgin_test`):
- `reliability_test.go`, `backoff_test.go`, `retry_test.go` — unit tests.
- `settlement_doubles_test.go` — shared recording test doubles (`recordingSink`, `scriptedSource`, `nativeScriptedSource`) used by the settlement + shutdown tests. Created in Task 5, reused in Tasks 6–7.
- `errors_test.go`, `consumer_test.go` — appended.

Docs (ride with the code per CLAUDE.md — no standalone ADR commit):
- `docs/adrs/0007-reliability-settlement-api.md` — created in Task 1, extended in Tasks 3, 4, 7 (each extension in that task's commit). Full outline at the end of this plan.
- `docs/specs/001-messaging-core.md` — one cross-ref line added in Task 1 noting ADR 0007 refines §5's `backoff.Permanent` mention.

---

### Task 1: Permanent-error marker, `ErrHandlerPanic`, validation sentinels, ADR 0007

**Files:**
- Create: `reliability.go`
- Modify: `errors.go` (append three sentinels)
- Create: `docs/adrs/0007-reliability-settlement-api.md` (Status: Accepted; decisions D1–D2, stubs for D3–D10)
- Modify: `docs/specs/001-messaging-core.md` (add the ADR 0007 cross-ref note in §5)
- Test: `reliability_test.go`, `errors_test.go` (append)

**Interfaces:**
- Consumes: `ErrPayloadType`, `ErrPayloadDecode` (Plan 001, `errors.go`).
- Produces: `func Permanent(err error) error`; sentinels `ErrHandlerPanic`, `ErrNoDeadLetter`, `ErrInvalidMaxAttempts` (all `error`); unexported `type permanentError`, `func isPermanent(error) bool`, `type noNativeReliability struct{}`.

**Design note (ADR 0007 D1).** msgin marks permanent errors with its **own** `Permanent(err)` wrapper (an `Unwrap`-able `*permanentError`), detected via `errors.As`. We deliberately do **not** import `cenkalti/backoff` to reuse `backoff.Permanent`: ADR 0005/spec §8 place `cenkalti` on the tight outbound-HTTP loop (`adapter/http`), not the closed-form redelivery path, so importing it into the core runtime just for a marker would pull the dep in prematurely and against the stdlib+clockwork constraint. This *refines* spec §5's `backoff.Permanent(err)` phrasing → `msgin.Permanent(err)` (recorded in ADR 0007, cross-linked from the spec). `ErrHandlerPanic` is classified **transient** (it is *not* wrapped in `Permanent`), so a panicking handler is retried, not diverted.

**Hot-path/typed-error branches introduced:** `Permanent(nil)`→nil vs `Permanent(err)`→wrapper; `isPermanent`: nil→false, `*permanentError`→true, `ErrPayloadType`/`ErrPayloadDecode`→true, plain error / `ErrHandlerPanic`→false. (`isPermanent` is unexported; its branches are covered here via the exported `Permanent` round-trip + the sentinel wrapping asserted in `errors_test.go`, and again end-to-end through `Run` in Task 5. This task asserts the `Permanent`/`Unwrap` surface directly.)

- [ ] **Step 1: Write the failing test**

```go
// reliability_test.go
package msgin_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/kartaladev/msgin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPermanent_WrapAndUnwrap(t *testing.T) {
	base := errors.New("boom")

	tests := []struct {
		name   string
		in     error
		assert func(t *testing.T, got error)
	}{
		{"nil stays nil", nil, func(t *testing.T, got error) {
			assert.NoError(t, got)
		}},
		{"wraps and unwraps to base", base, func(t *testing.T, got error) {
			require.Error(t, got)
			assert.ErrorIs(t, got, base, "Permanent must remain Is-comparable to its cause")
		}},
		{"survives an outer wrap", fmt.Errorf("ctx: %w", base), func(t *testing.T, got error) {
			assert.ErrorIs(t, got, base)
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tc.assert(t, msgin.Permanent(tc.in))
		})
	}
}
```

```go
// errors_test.go (append) — extend the existing sentinel table with the new ones.
func TestSentinels_Reliability_WrapAndCompare(t *testing.T) {
	sentinels := []error{
		msgin.ErrHandlerPanic, msgin.ErrNoDeadLetter, msgin.ErrInvalidMaxAttempts,
	}
	for _, s := range sentinels {
		t.Run(s.Error(), func(t *testing.T) {
			assert.True(t, errors.Is(fmt.Errorf("context: %w", s), s))
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `GOTOOLCHAIN=go1.25.0 go test ./... -run 'TestPermanent_|TestSentinels_Reliability' -v`
Expected: FAIL — `undefined: msgin.Permanent` / `msgin.ErrHandlerPanic`.

- [ ] **Step 3: Write minimal implementation**

```go
// reliability.go
package msgin

import "errors"

// permanentError marks a handler error as non-retryable: the runtime routes it
// straight to the invalid-message sink instead of retrying. Wrapping is
// transparent to errors.Is/As via Unwrap.
type permanentError struct{ err error }

func (e *permanentError) Error() string { return "msgin: permanent: " + e.err.Error() }
func (e *permanentError) Unwrap() error { return e.err }

// Permanent marks err as permanent (non-retryable). A handler that returns
// Permanent(err) sends the message to the invalid-message sink without
// consuming retry attempts. Permanent(nil) returns nil.
//
// msgin uses its own marker rather than cenkalti/backoff.Permanent so the core
// runtime stays stdlib + clockwork (see ADR 0007); cenkalti enters only via the
// outbound-HTTP adapter (ADR 0005).
func Permanent(err error) error {
	if err == nil {
		return nil
	}
	return &permanentError{err: err}
}

// isPermanent reports whether err must skip retry: an explicit Permanent marker,
// or a decode/type mismatch (ErrPayloadDecode / ErrPayloadType). A recovered
// handler panic (ErrHandlerPanic) is NOT permanent — it is retried.
func isPermanent(err error) bool {
	if err == nil {
		return false
	}
	var pe *permanentError
	if errors.As(err, &pe) {
		return true
	}
	return errors.Is(err, ErrPayloadType) || errors.Is(err, ErrPayloadDecode)
}

// noNativeReliability is the NativeReliability default for sources that do not
// implement the optional capability (e.g. memory): neither native redelivery
// nor native dead-letter. Using a value (never nil) upholds NF-11 — the runtime
// never nil-calls the capability.
type noNativeReliability struct{}

func (noNativeReliability) NativeRedelivery() bool { return false }
func (noNativeReliability) NativeDeadLetter() bool { return false }
```

```go
// errors.go (append inside the existing var block)
	// ErrHandlerPanic wraps a value recovered from a panicking handler. It is a
	// TRANSIENT failure (retried per the RetryPolicy), not permanent.
	ErrHandlerPanic = errors.New("msgin: handler panicked")
	// ErrNoDeadLetter is returned when a RetryPolicy has a finite MaxAttempts
	// (> 0) but no DeadLetter sink to receive exhausted messages.
	ErrNoDeadLetter = errors.New("msgin: finite MaxAttempts requires a DeadLetter sink")
	// ErrInvalidMaxAttempts is returned when a RetryPolicy's MaxAttempts is negative.
	ErrInvalidMaxAttempts = errors.New("msgin: MaxAttempts must be >= 0")
```

Author `docs/adrs/0007-reliability-settlement-api.md` (Status Accepted) with the full Context/Decision/Consequences per the outline at the end of this plan — write decisions **D1–D2** in full and D3–D10 as one-line stubs (each fleshed out in the task that realizes it). Add to spec §5, after the "Permanent vs transient" paragraph: `> Refined by ADR 0007: msgin uses msgin.Permanent(err) (not backoff.Permanent) as the permanent marker; the runtime redelivery path stays closed-form and cenkalti-free.`

- [ ] **Step 4: Run test to verify it passes**

Run: `GOTOOLCHAIN=go1.25.0 go test ./... -run 'TestPermanent_|TestSentinels' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add reliability.go errors.go reliability_test.go errors_test.go docs/adrs/0007-reliability-settlement-api.md docs/specs/001-messaging-core.md
git commit -m "feat: permanent-error marker, handler-panic + validation sentinels" \
  -m "Introduces msgin.Permanent (core stays cenkalti-free) and ErrHandlerPanic/ErrNoDeadLetter/ErrInvalidMaxAttempts; authors ADR 0007." \
  -m "Spec: 001" -m "Plan: 002" -m "ADR: 0002, 0007"
```

---

### Task 2: `BackoffStrategy` + `ExponentialBackoff` (closed-form, edge-guarded)

**Files:**
- Create: `backoff.go`
- Test: `backoff_test.go`

**Interfaces:**
- Produces: `type BackoffStrategy interface { Delay(attempt int) time.Duration }`; `type ExponentialBackoff struct { Initial, Max time.Duration; Mult float64; RandomizationFactor float64 }` implementing it.

**Design note (spec §8, ADR 0007 D4).** Stateless, O(1), `clockwork`-agnostic (pure computation — no clock). `Delay(attempt)` takes a **0-based retry index** (0 = first retry ⇒ `Initial`). Closed form: `min(Max, Initial × Mult^attempt)`. Guards (each a covered branch): `attempt < 0` clamps to 0; `Initial <= 0` ⇒ `0`; `Mult <= 0` ⇒ treated as `1` (no growth); `Max <= 0` ⇒ no cap; overflow / `Inf`/`NaN` ⇒ `Max` if capped else `Initial`. Optional jitter: `RandomizationFactor > 0` spreads the result over `[d·(1-f), d·(1+f)]` using `math/rand/v2` (non-deterministic — matches ADR 0005 L1: exact-delay tests set `RandomizationFactor = 0`; the one jitter test asserts bounds, not an exact value). Per-message-id-seeded jitter is **not** implemented — `Delay(attempt)` has no id parameter (see "Design decisions" / ambiguity note).

**Hot-path branches introduced:** `attempt<0`, `Initial<=0`, `Mult<=0`, normal growth, `Max` cap hit, overflow→cap (`Inf`/`NaN` guard, `Max>0`), overflow→`Initial` (`Inf`/`NaN` guard, `Max<=0`), `RandomizationFactor==0` (deterministic), `RandomizationFactor>0` (bounded jitter). The two `attempt=2000` cases exercise the `math.IsInf/IsNaN` guard and both of its sub-arms.

- [ ] **Step 1: Write the failing test**

```go
// backoff_test.go
package msgin_test

import (
	"testing"
	"time"

	"github.com/kartaladev/msgin"
	"github.com/stretchr/testify/assert"
)

func TestExponentialBackoff_Delay(t *testing.T) {
	base := msgin.ExponentialBackoff{Initial: 100 * time.Millisecond, Max: time.Second, Mult: 2}

	tests := []struct {
		name    string
		backoff msgin.ExponentialBackoff
		attempt int
		assert  func(t *testing.T, d time.Duration)
	}{
		{"first retry is Initial", base, 0, func(t *testing.T, d time.Duration) {
			assert.Equal(t, 100*time.Millisecond, d)
		}},
		{"grows by Mult", base, 1, func(t *testing.T, d time.Duration) {
			assert.Equal(t, 200*time.Millisecond, d)
		}},
		{"caps at Max", base, 10, func(t *testing.T, d time.Duration) {
			assert.Equal(t, time.Second, d) // 100ms*2^10 = 102.4s -> capped
		}},
		{"negative attempt clamps to first", base, -5, func(t *testing.T, d time.Duration) {
			assert.Equal(t, 100*time.Millisecond, d)
		}},
		{"zero Initial yields zero", msgin.ExponentialBackoff{Initial: 0, Max: time.Second, Mult: 2}, 3,
			func(t *testing.T, d time.Duration) {
				assert.Equal(t, time.Duration(0), d)
			}},
		{"non-growing Mult stays at Initial", msgin.ExponentialBackoff{Initial: 50 * time.Millisecond, Max: time.Second, Mult: 0}, 4,
			func(t *testing.T, d time.Duration) {
				assert.Equal(t, 50*time.Millisecond, d)
			}},
		{"no Max means no cap", msgin.ExponentialBackoff{Initial: time.Second, Max: 0, Mult: 2}, 2,
			func(t *testing.T, d time.Duration) {
				assert.Equal(t, 4*time.Second, d)
			}},
		{"jitter stays within bounds", msgin.ExponentialBackoff{Initial: 100 * time.Millisecond, Max: time.Second, Mult: 2, RandomizationFactor: 0.5}, 1,
			func(t *testing.T, d time.Duration) {
				// base for attempt 1 = 200ms; jitter in [100ms, 300ms]
				assert.GreaterOrEqual(t, d, 100*time.Millisecond)
				assert.LessOrEqual(t, d, 300*time.Millisecond)
			}},
		{"overflow with Max caps at Max", base, 2000, func(t *testing.T, d time.Duration) {
			// 100ms*2^2000 overflows float64 to +Inf -> IsInf guard, Max>0 -> Max.
			assert.Equal(t, time.Second, d)
		}},
		{"overflow without Max returns Initial", msgin.ExponentialBackoff{Initial: time.Second, Max: 0, Mult: 2}, 2000,
			func(t *testing.T, d time.Duration) {
				// +Inf with no cap -> IsInf guard, Max<=0 -> Initial.
				assert.Equal(t, time.Second, d)
			}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tc.assert(t, tc.backoff.Delay(tc.attempt))
		})
	}
}

func TestExponentialBackoff_SatisfiesInterface(t *testing.T) {
	var _ msgin.BackoffStrategy = msgin.ExponentialBackoff{}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `GOTOOLCHAIN=go1.25.0 go test ./... -run TestExponentialBackoff -v`
Expected: FAIL — `undefined: msgin.ExponentialBackoff`.

- [ ] **Step 3: Write minimal implementation**

```go
// backoff.go
package msgin

import (
	"math"
	"math/rand/v2"
	"time"
)

// BackoffStrategy computes the redelivery delay for a given 0-based retry index
// (0 = first retry). It is stateless and clock-agnostic: pure arithmetic, safe
// to share across goroutines. This is RetryPolicy.Backoff (spec §8).
type BackoffStrategy interface {
	Delay(attempt int) time.Duration
}

// ExponentialBackoff is the default BackoffStrategy: min(Max, Initial × Mult^attempt),
// with optional jitter. All fields are guarded so no combination panics or
// returns a negative duration (a nil/zero-value ExponentialBackoff yields 0).
type ExponentialBackoff struct {
	Initial             time.Duration // delay for the first retry (attempt 0)
	Max                 time.Duration // cap; <= 0 means uncapped
	Mult                float64       // growth factor per attempt; <= 0 treated as 1 (no growth)
	RandomizationFactor float64       // 0 = deterministic; f>0 spreads over [d·(1-f), d·(1+f)]
}

// Delay returns the backoff for the given 0-based retry index.
func (b ExponentialBackoff) Delay(attempt int) time.Duration {
	if attempt < 0 {
		attempt = 0
	}
	if b.Initial <= 0 {
		return 0
	}
	mult := b.Mult
	if mult <= 0 {
		mult = 1
	}
	d := float64(b.Initial) * math.Pow(mult, float64(attempt))
	if math.IsInf(d, 1) || math.IsNaN(d) {
		if b.Max > 0 {
			return b.Max
		}
		return b.Initial
	}
	out := time.Duration(d)
	if b.Max > 0 && out > b.Max {
		out = b.Max
	}
	if b.RandomizationFactor > 0 {
		out = jitter(out, b.RandomizationFactor)
	}
	return out
}

// jitter spreads d uniformly over [d·(1-f), d·(1+f)], clamped to >= 0.
func jitter(d time.Duration, f float64) time.Duration {
	delta := float64(d) * f
	lo := float64(d) - delta
	j := lo + rand.Float64()*(2*delta)
	if j < 0 {
		j = 0
	}
	return time.Duration(j)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `GOTOOLCHAIN=go1.25.0 go test ./... -run TestExponentialBackoff -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backoff.go backoff_test.go
git commit -m "feat: closed-form ExponentialBackoff strategy" \
  -m "Stateless, attempt-indexed min(Max, Initial*Mult^attempt) with guards and optional jitter (spec §8); no cenkalti dependency." \
  -m "Spec: 001" -m "Plan: 002" -m "ADR: 0005, 0007"
```

---

### Task 3: `RetryPolicy` + `Validate()`

**Files:**
- Create: `retry.go` (`RetryPolicy` + `Validate`; the `Hooks` struct and `attemptTracker` are added in later tasks — keep this file focused on the policy for now)
- Modify: `docs/adrs/0007-reliability-settlement-api.md` (flesh out D3)
- Test: `retry_test.go`

**Interfaces:**
- Consumes: `BackoffStrategy` (Task 2), `OutboundAdapter` (Plan 001), `ErrNoDeadLetter`/`ErrInvalidMaxAttempts` (Task 1).
- Produces: `type RetryPolicy struct { MaxAttempts int; Backoff BackoffStrategy; DeadLetter OutboundAdapter }`; `func (RetryPolicy) Validate() error`.

**Design note (ADR 0007 D3).** `MaxAttempts == 0` ⇒ **retry forever** (no dead-letter); `> 0` ⇒ finite, and then `DeadLetter` is **required**; `< 0` is invalid. `Backoff == nil` is allowed and means **immediate** redelivery (zero delay) — validation does not reject it. The **zero value** `RetryPolicy{}` therefore validates OK and means "retry forever, immediately, no DLQ", which is exactly the consumer's default when no `WithRetryPolicy` is given (preserving Plan 001's requeue behavior). `Validate()` is exported so callers can pre-check a policy and so `NewConsumer` can surface a typed construction error (spec §5). Branch order in `Validate`: negative first, then finite-without-DLQ.

**Hot-path/typed-error branches introduced:** `MaxAttempts < 0` → `ErrInvalidMaxAttempts`; `MaxAttempts > 0 && DeadLetter == nil` → `ErrNoDeadLetter`; `MaxAttempts > 0 && DeadLetter != nil` → nil; `MaxAttempts == 0` (any DeadLetter) → nil.

- [ ] **Step 1: Write the failing test**

```go
// retry_test.go
package msgin_test

import (
	"context"
	"testing"
	"time"

	"github.com/kartaladev/msgin"
	"github.com/stretchr/testify/assert"
)

// nopSink is a minimal OutboundAdapter for policy tests (no recording needed).
type nopSink struct{}

func (nopSink) Send(context.Context, msgin.Message[any]) error { return nil }

func TestRetryPolicy_Validate(t *testing.T) {
	tests := []struct {
		name   string
		policy msgin.RetryPolicy
		assert func(t *testing.T, err error)
	}{
		{"zero value = retry forever, valid", msgin.RetryPolicy{},
			func(t *testing.T, err error) { assert.NoError(t, err) }},
		{"finite with dead-letter is valid",
			msgin.RetryPolicy{MaxAttempts: 3, DeadLetter: nopSink{}, Backoff: msgin.ExponentialBackoff{Initial: time.Millisecond}},
			func(t *testing.T, err error) { assert.NoError(t, err) }},
		{"finite without dead-letter is rejected",
			msgin.RetryPolicy{MaxAttempts: 3},
			func(t *testing.T, err error) { assert.ErrorIs(t, err, msgin.ErrNoDeadLetter) }},
		{"negative MaxAttempts is rejected",
			msgin.RetryPolicy{MaxAttempts: -1, DeadLetter: nopSink{}},
			func(t *testing.T, err error) { assert.ErrorIs(t, err, msgin.ErrInvalidMaxAttempts) }},
		{"retry-forever ignores absent dead-letter",
			msgin.RetryPolicy{MaxAttempts: 0},
			func(t *testing.T, err error) { assert.NoError(t, err) }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tc.assert(t, tc.policy.Validate())
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `GOTOOLCHAIN=go1.25.0 go test ./... -run TestRetryPolicy_Validate -v`
Expected: FAIL — `undefined: msgin.RetryPolicy`.

- [ ] **Step 3: Write minimal implementation**

```go
// retry.go
package msgin

// RetryPolicy governs how the runtime settles a failed delivery (spec §7).
//
//   - MaxAttempts == 0 : retry forever (no dead-letter).
//   - MaxAttempts  > 0 : after that many delivery attempts a still-failing
//     message is diverted to DeadLetter (required); DeadLetter may be any
//     OutboundAdapter, including another msgin adapter.
//   - Backoff nil       : immediate redelivery (zero delay).
//
// The zero value is valid and means "retry forever, immediately, no DLQ".
type RetryPolicy struct {
	MaxAttempts int
	Backoff     BackoffStrategy
	DeadLetter  OutboundAdapter
}

// Validate reports whether the policy is internally consistent. A finite
// MaxAttempts requires a DeadLetter; a negative MaxAttempts is invalid. Called
// by NewConsumer so a bad policy fails at construction (spec §5).
func (p RetryPolicy) Validate() error {
	if p.MaxAttempts < 0 {
		return ErrInvalidMaxAttempts
	}
	if p.MaxAttempts > 0 && p.DeadLetter == nil {
		return ErrNoDeadLetter
	}
	return nil
}

// delayFor returns the redelivery delay for the given 1-based attempt count,
// converting to the 0-based retry index the BackoffStrategy expects. A nil
// Backoff means immediate redelivery.
func (p RetryPolicy) delayFor(attempt int) time.Duration {
	if p.Backoff == nil {
		return 0
	}
	return p.Backoff.Delay(attempt - 1)
}
```

Add `import "time"` to `retry.go`. Flesh out ADR 0007 **D3** (zero-value semantics, nil-Backoff = immediate, `Validate` as the construction gate).

- [ ] **Step 4: Run test to verify it passes**

Run: `GOTOOLCHAIN=go1.25.0 go test ./... -run TestRetryPolicy_Validate -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add retry.go retry_test.go docs/adrs/0007-reliability-settlement-api.md
git commit -m "feat: RetryPolicy with construction-time validation" \
  -m "Zero value = retry forever; finite MaxAttempts requires DeadLetter; nil Backoff = immediate (spec §7)." \
  -m "Spec: 001" -m "Plan: 002" -m "ADR: 0002, 0007"
```

---

### Task 4: Consumer reliability config surface (options, Hooks, tracker, construction validation)

**Files:**
- Modify: `consumer.go` (add options, `consumer` fields, `NewConsumer` wiring; `dispatch`/`Run` behavior UNCHANGED this task)
- Modify: `retry.go` (add `Hooks` struct + `attemptTracker`)
- Modify: `docs/adrs/0007-reliability-settlement-api.md` (flesh out D6 Hooks shape, D7 no-sink policy, D10 clock-option naming)
- Test: `consumer_test.go` (append)

**Interfaces:**
- Consumes: `RetryPolicy` (Task 3), `OutboundAdapter`, `StreamingSource`, `NativeReliability`, `noNativeReliability` (Task 1), `clockwork.Clock`.
- Produces:
  - `type Hooks struct { OnRetry, OnDeadLetter, OnInvalidMessage, OnAck func(context.Context, Message[any], error) }`
  - `func WithRetryPolicy[T any](p RetryPolicy) ConsumerOption[T]`
  - `func WithInvalidMessageSink[T any](out OutboundAdapter) ConsumerOption[T]`
  - `func WithLogger[T any](l *slog.Logger) ConsumerOption[T]`
  - `func WithHooks[T any](h Hooks) ConsumerOption[T]`
  - `func WithShutdownTimeout[T any](d time.Duration) ConsumerOption[T]`
  - `func WithConsumerClock[T any](c clockwork.Clock) ConsumerOption[T]`
  - unexported `type attemptTracker` (`newAttemptTracker`, `observe(id) int`, `evict(id)`).

**Design note.** This task only widens the construction surface and validates it; the runtime still uses Plan 001's minimal `dispatch` (rewritten in Task 5), so the suite stays green. Defaults: `clock = clockwork.NewRealClock()`, `logger = slog.New(slog.NewTextHandler(io.Discard, nil))` (never `slog.Default()` — CLAUDE.md), `policy = RetryPolicy{}` (retry forever), `shutdownTimeout = 0` (⇒ the finite `defaultShutdownTimeout`, resolved in `Run` — Task 7/C1, **not** an unbounded wait), `hooks = Hooks{}` (all nil, nil-safe). `NewConsumer` calls `cfg.policy.Validate()` and returns its error. The native capability is resolved once (`any(stream).(NativeReliability)` with the `noNativeReliability{}` fallback) and stored. `WithConsumerClock` is named to avoid colliding with `message.go`'s `WithClock` (ADR 0007 D10).

**Hot-path/typed-error branches introduced:** `NewConsumer` now also returns `ErrNoDeadLetter`/`ErrInvalidMaxAttempts` (from `policy.Validate()`); native-capability present vs absent (both-false fallback).

- [ ] **Step 1: Write the failing test**

```go
// consumer_test.go (append)
func TestNewConsumer_RetryPolicyValidation(t *testing.T) {
	h := func(context.Context, msgin.Message[order]) error { return nil }

	tests := []struct {
		name   string
		opts   []msgin.ConsumerOption[order]
		assert func(t *testing.T, err error)
	}{
		{"finite policy without dead-letter is rejected at construction",
			[]msgin.ConsumerOption[order]{msgin.WithRetryPolicy[order](msgin.RetryPolicy{MaxAttempts: 3})},
			func(t *testing.T, err error) { assert.ErrorIs(t, err, msgin.ErrNoDeadLetter) }},
		{"negative MaxAttempts is rejected",
			[]msgin.ConsumerOption[order]{msgin.WithRetryPolicy[order](msgin.RetryPolicy{MaxAttempts: -1})},
			func(t *testing.T, err error) { assert.ErrorIs(t, err, msgin.ErrInvalidMaxAttempts) }},
		{"valid reliability options construct cleanly",
			[]msgin.ConsumerOption[order]{
				msgin.WithRetryPolicy[order](msgin.RetryPolicy{}),
				msgin.WithLogger[order](slog.New(slog.NewTextHandler(io.Discard, nil))),
				msgin.WithHooks[order](msgin.Hooks{}),
				msgin.WithShutdownTimeout[order](time.Second),
				msgin.WithConsumerClock[order](clockwork.NewFakeClock()),
				msgin.WithInvalidMessageSink[order](memory.New()),
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
```

Add imports `"io"`, `"log/slog"`, `"time"`, `"github.com/jonboulle/clockwork"` to `consumer_test.go` if not present.

- [ ] **Step 2: Run test to verify it fails**

Run: `GOTOOLCHAIN=go1.25.0 go test ./... -run TestNewConsumer_RetryPolicyValidation -v`
Expected: FAIL — `undefined: msgin.WithRetryPolicy`.

- [ ] **Step 3: Write minimal implementation**

```go
// retry.go (append)
import "sync" // add to the retry.go import block

// Hooks are optional, nil-safe callbacks fired on the operationally important
// settlement events (spec §7 observability). The error argument carries the
// triggering error (nil on a successful Ack).
type Hooks struct {
	OnRetry          func(ctx context.Context, msg Message[any], err error)
	OnDeadLetter     func(ctx context.Context, msg Message[any], err error)
	OnInvalidMessage func(ctx context.Context, msg Message[any], err error)
	OnAck            func(ctx context.Context, msg Message[any], err error)
}

// attemptTracker counts delivery attempts per message id for sources without a
// native msgin.delivery-count header. Entries are evicted only on terminal
// settle (Ack/DLQ/invalid), never while a message is still being redelivered
// (NF-2), so a poison count cannot reset mid-flight.
type attemptTracker struct {
	mu sync.Mutex
	m  map[string]int
}

func newAttemptTracker() *attemptTracker { return &attemptTracker{m: make(map[string]int)} }

// observe records one more attempt for id and returns the new count (1-based).
func (t *attemptTracker) observe(id string) int {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.m[id]++
	return t.m[id]
}

// evict forgets id (call only on terminal settle).
func (t *attemptTracker) evict(id string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.m, id)
}
```

Add `import "context"` to `retry.go` (it already imports `time`).

```go
// consumer.go — extend consumerConfig and add the options.
// (add imports: "io", "log/slog", "time", "github.com/jonboulle/clockwork")

type consumerConfig[T any] struct {
	concurrency     int
	codec           PayloadCodec[T]
	codecSet        bool
	policy          RetryPolicy
	invalidSink     OutboundAdapter
	logger          *slog.Logger
	hooks           Hooks
	shutdownTimeout time.Duration
	clock           clockwork.Clock
}

// WithRetryPolicy sets the settlement policy (default: retry forever, immediate).
func WithRetryPolicy[T any](p RetryPolicy) ConsumerOption[T] {
	return func(o *consumerConfig[T]) { o.policy = p }
}

// WithInvalidMessageSink sets where permanent/undecodable messages are diverted.
// If unset, such messages are logged and discarded (ADR 0007).
func WithInvalidMessageSink[T any](out OutboundAdapter) ConsumerOption[T] {
	return func(o *consumerConfig[T]) { o.invalidSink = out }
}

// WithLogger injects the structured logger (default: a discard logger).
func WithLogger[T any](l *slog.Logger) ConsumerOption[T] {
	return func(o *consumerConfig[T]) { o.logger = l }
}

// WithHooks sets the observability callbacks (all nil-safe).
func WithHooks[T any](h Hooks) ConsumerOption[T] {
	return func(o *consumerConfig[T]) { o.hooks = h }
}

// WithShutdownTimeout bounds the drain on cancel (default 0 = wait fully).
func WithShutdownTimeout[T any](d time.Duration) ConsumerOption[T] {
	return func(o *consumerConfig[T]) { o.shutdownTimeout = d }
}

// WithConsumerClock injects the clock used for shutdown timing (default: real).
// Named to avoid colliding with the MessageOption WithClock in this package.
func WithConsumerClock[T any](c clockwork.Clock) ConsumerOption[T] {
	return func(o *consumerConfig[T]) { o.clock = c }
}
```

Extend the `consumer[T]` struct with the resolved fields and wire `NewConsumer`:

```go
type consumer[T any] struct {
	src             StreamingSource
	handler         Handler[T]
	codec           PayloadCodec[T]
	liveValue       bool
	workers         int
	policy          RetryPolicy
	invalidSink     OutboundAdapter
	logger          *slog.Logger
	hooks           Hooks
	shutdownTimeout time.Duration
	clock           clockwork.Clock
	native          NativeReliability
	tracker         *attemptTracker
}
```

In `NewConsumer`, after `cfg := consumerConfig[T]{concurrency: 1}` set defaults and validate:

```go
	cfg := consumerConfig[T]{
		concurrency: 1,
		clock:       clockwork.NewRealClock(),
		logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	if cfg.concurrency < 1 {
		return nil, ErrInvalidConcurrency
	}
	if err := cfg.policy.Validate(); err != nil {
		return nil, err
	}
	// ... existing resolveCodec + StreamingSource type-switch ...
	native, ok := any(stream).(NativeReliability)
	if !ok {
		native = noNativeReliability{}
	}
	return &consumer[T]{
		src: stream, handler: h, codec: codec, liveValue: live, workers: cfg.concurrency,
		policy: cfg.policy, invalidSink: cfg.invalidSink, logger: cfg.logger,
		hooks: cfg.hooks, shutdownTimeout: cfg.shutdownTimeout, clock: cfg.clock,
		native: native, tracker: newAttemptTracker(),
	}, nil
```

Flesh out ADR 0007 **D6** (Hooks shape), **D7** (no-sink discard policy), **D10** (clock-option naming).

- [ ] **Step 4: Run test to verify it passes**

Run: `GOTOOLCHAIN=go1.25.0 go test ./... -race -v`
Expected: PASS (existing tests still green; new validation test passes), no goroutine leaks.

- [ ] **Step 5: Commit**

```bash
git add consumer.go retry.go consumer_test.go docs/adrs/0007-reliability-settlement-api.md
git commit -m "feat: consumer reliability options, Hooks, attempt tracker + construction validation" \
  -m "Adds WithRetryPolicy/WithInvalidMessageSink/WithLogger/WithHooks/WithShutdownTimeout/WithConsumerClock; validates the policy at construction. Runtime behavior unchanged (Task 5)." \
  -m "Spec: 001" -m "Plan: 002" -m "ADR: 0002, 0007"
```

---

### Task 5: The guarded settlement switch (permanent → invalid, transient → Nack, panic transient, hooks, divert NF-3)

**Files:**
- Modify: `consumer.go` (rewrite `dispatch`; add `divert`, `fire`, `finish`, `safeHandle` change)
- Create: `settlement_doubles_test.go` (shared recording doubles)
- Test: `consumer_test.go` (append)

**Interfaces:**
- Consumes: `isPermanent`, `Permanent`, `ErrHandlerPanic`, `Hooks`, `RetryPolicy`, `noNativeReliability` (Tasks 1/3/4), memory adapter.
- Produces (unexported): `func (c *consumer[T]) dispatch(ctx, settleCtx context.Context, d Delivery)`; `func (c *consumer[T]) divert(ctx context.Context, sink OutboundAdapter, d Delivery, hook func(...), cause error, attempt int) bool`; `func (c *consumer[T]) finish(err error)`; `func fire(hook func(...), ...)`; `safeHandle` returns `ErrHandlerPanic`-wrapped.

**Design note (spec §7 switch — this task implements every branch EXCEPT the finite-DLQ branch + transient backoff delay, which are Task 6).** The rewritten `dispatch` takes two contexts: `ctx` (handler; may be cancelled at shutdown) and `settleCtx` (settle closures; kept alive during drain — Task 7 supplies a detached, cancellable context; for now both callers pass the same `ctx`). The branch order (spec §7) is expressed with **early returns** (not a single `switch`), so that Task 6 can insert its `n := c.attempts(d)` **lazily on the transient path only** (M8 — the tracker is never touched on the success/decode/permanent paths):
1. decode failed (before the handler) → **invalid** via `divert` (`OnInvalidMessage`).
2. `err == nil` → `Ack` (+ `OnAck` + tracker evict) → return.
3. `isPermanent(err)` → **invalid** via `divert` → return. (Reachable both from `Permanent(err)` and from a handler that *returns* `ErrPayloadDecode`/`ErrPayloadType` — I7.)
4. *(Task 6: finite-exhausted → DeadLetter, computed after a lazy `n := c.attempts(d)`)*.
5. default (transient) → `Nack(requeue=true, 0)` (+ `OnRetry`). *(Task 6 replaces the `0` with `c.policy.delayFor(n)`.)*

`safeHandle` now wraps the recovered panic in `ErrHandlerPanic` (transient → falls through to the default Nack branch, i.e. retried). `divert` upholds **NF-3**: on `sink.Send` success it `Ack`s (returns `true`); on failure it fires the hook and `Nack`s the original (returns `false`) — never Ack-and-lose. **I6 — the send-failure `Nack` carries a non-zero backoff delay** `c.policy.delayFor(attempt)` (spec §7's `backoff(sinkAttempts)`), **not** `0`, so a down sink is not hammered; `divert` takes an `attempt int` for this — the invalid/decode callers pass `1` (first sink attempt ⇒ `Backoff.Delay(0)` = `Initial`), and Task 6's DLQ caller passes the delivery's climbing attempt count `n`. A **nil** invalid sink means "log + `OnInvalidMessage` + `Ack` (discard)" (ADR 0007 D7). `finish(err)` logs a non-nil settle error (Plan 003 adds credit release here).

**Hot-path branches introduced (each a covered test case):** success→Ack; decode-fail→invalid (I2: a live-value payload whose dynamic type isn't `T`, not retried); `Permanent(err)`→invalid; handler-returns-`ErrPayloadDecode`→invalid (I7 — makes `isPermanent`'s sentinel arm reachable); panic→transient Nack; plain error→transient Nack; `divert` sink-send success→Ack+evict; `divert` sink-send failure→Nack with **non-zero backoff delay** (I6, no evict); nil invalid sink→discard(Ack); `fire` nil-hook vs set-hook (I3: hooks configured to record, asserting `OnAck`/`OnRetry`/`OnInvalidMessage` fire — `OnDeadLetter` firing is asserted in Task 6).

- [ ] **Step 1: Write the failing test**

```go
// settlement_doubles_test.go
package msgin_test

import (
	"context"
	"sync"
	"time"

	"github.com/kartaladev/msgin"
)

// recordingSink is an OutboundAdapter that records sends and can be forced to fail.
type recordingSink struct {
	mu     sync.Mutex
	sent   []msgin.Message[any]
	failWith error
}

func (s *recordingSink) Send(_ context.Context, m msgin.Message[any]) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failWith != nil {
		return s.failWith
	}
	s.sent = append(s.sent, m)
	return nil
}

func (s *recordingSink) count() int { s.mu.Lock(); defer s.mu.Unlock(); return len(s.sent) }

// settle records how a Delivery was settled.
type settle struct {
	mu       sync.Mutex
	acks     int
	nacks    int
	requeue  []bool
	delays   []time.Duration
}

func (s *settle) ack(context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.acks++
	return nil
}

func (s *settle) nack(_ context.Context, requeue bool, d time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nacks++
	s.requeue = append(s.requeue, requeue)
	s.delays = append(s.delays, d)
	return nil
}

func (s *settle) snapshot() (acks, nacks int, delays []time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.acks, s.nacks, append([]time.Duration(nil), s.delays...)
}

// scriptedSource is a StreamingSource that emits preset deliveries (with the
// given settle recorders) once, re-emitting each on Nack-requeue up to a cap so
// retry loops terminate, then blocks until ctx is done.
type scriptedSource struct {
	deliveries []msgin.Delivery
}

func (s *scriptedSource) EmitsLiveValue() bool { return true }

func (s *scriptedSource) Stream(ctx context.Context, out chan<- msgin.Delivery) error {
	for _, d := range s.deliveries {
		select {
		case out <- d:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	<-ctx.Done()
	return ctx.Err()
}

// newDelivery builds a live-value Delivery whose Ack/Nack are recorded by st.
func newDelivery(payload any, id string, st *settle) msgin.Delivery {
	m := msgin.New[any](payload, msgin.WithID(id))
	return msgin.Delivery{Msg: m, Ack: st.ack, Nack: st.nack}
}

// hookRec records which observability hooks fired, keyed by event name.
type hookRec struct {
	mu sync.Mutex
	n  map[string]int
}

func newHookRec() *hookRec { return &hookRec{n: map[string]int{}} }

func (r *hookRec) inc(event string) { r.mu.Lock(); defer r.mu.Unlock(); r.n[event]++ }
func (r *hookRec) count(event string) int { r.mu.Lock(); defer r.mu.Unlock(); return r.n[event] }

// hooks returns a msgin.Hooks whose callbacks record into r.
func (r *hookRec) hooks() msgin.Hooks {
	return msgin.Hooks{
		OnAck:            func(context.Context, msgin.Message[any], error) { r.inc("ack") },
		OnRetry:          func(context.Context, msgin.Message[any], error) { r.inc("retry") },
		OnInvalidMessage: func(context.Context, msgin.Message[any], error) { r.inc("invalid") },
		OnDeadLetter:     func(context.Context, msgin.Message[any], error) { r.inc("deadletter") },
	}
}
```

```go
// consumer_test.go (append) — drive dispatch through Run with a scriptedSource.
func TestConsumer_Settlement_PermanentAndTransient(t *testing.T) {
	type orderPayload = order

	tests := []struct {
		name    string
		handler msgin.Handler[orderPayload]
		sink    *recordingSink        // invalid-message sink; nil => discard
		assert  func(t *testing.T, st *settle, sink *recordingSink)
	}{
		{"success acks",
			func(context.Context, msgin.Message[orderPayload]) error { return nil },
			&recordingSink{},
			func(t *testing.T, st *settle, sink *recordingSink) {
				acks, nacks, _ := st.snapshot()
				assert.Equal(t, 1, acks)
				assert.Equal(t, 0, nacks)
				assert.Equal(t, 0, sink.count())
			}},
		{"permanent error goes to invalid sink and acks source",
			func(context.Context, msgin.Message[orderPayload]) error { return msgin.Permanent(errors.New("bad")) },
			&recordingSink{},
			func(t *testing.T, st *settle, sink *recordingSink) {
				acks, nacks, _ := st.snapshot()
				assert.Equal(t, 1, sink.count(), "diverted to invalid sink")
				assert.Equal(t, 1, acks, "source acked after sink accepted")
				assert.Equal(t, 0, nacks)
			}},
		{"transient error nacks for redelivery",
			func(context.Context, msgin.Message[orderPayload]) error { return errors.New("temporary") },
			&recordingSink{},
			func(t *testing.T, st *settle, sink *recordingSink) {
				_, nacks, _ := st.snapshot()
				assert.GreaterOrEqual(t, nacks, 1)
				assert.Equal(t, 0, sink.count())
			}},
		{"panic is transient (nacked, not diverted)",
			func(context.Context, msgin.Message[orderPayload]) error { panic("kaboom") },
			&recordingSink{},
			func(t *testing.T, st *settle, sink *recordingSink) {
				_, nacks, _ := st.snapshot()
				assert.GreaterOrEqual(t, nacks, 1)
				assert.Equal(t, 0, sink.count(), "panic must not go to invalid sink")
			}},
		{"invalid with nil sink discards (acks) rather than looping",
			func(context.Context, msgin.Message[orderPayload]) error { return msgin.Permanent(errors.New("bad")) },
			nil,
			func(t *testing.T, st *settle, sink *recordingSink) {
				acks, nacks, _ := st.snapshot()
				assert.Equal(t, 1, acks)
				assert.Equal(t, 0, nacks)
			}},
		{"handler returning ErrPayloadDecode is permanent (invalid sink) — I7",
			func(context.Context, msgin.Message[orderPayload]) error {
				return fmt.Errorf("decode in handler: %w", msgin.ErrPayloadDecode)
			},
			&recordingSink{},
			func(t *testing.T, st *settle, sink *recordingSink) {
				acks, nacks, _ := st.snapshot()
				assert.Equal(t, 1, sink.count(), "ErrPayloadDecode from the handler diverts to invalid")
				assert.Equal(t, 1, acks)
				assert.Equal(t, 0, nacks, "permanent decode error must not be retried")
			}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			st := &settle{}
			src := &scriptedSource{deliveries: []msgin.Delivery{newDelivery(order{ID: "o"}, "m1", st)}}
			opts := []msgin.ConsumerOption[orderPayload]{}
			if tc.sink != nil {
				opts = append(opts, msgin.WithInvalidMessageSink[orderPayload](tc.sink))
			}
			c, err := msgin.NewConsumer[orderPayload](src, tc.handler, opts...)
			require.NoError(t, err)

			ctx, cancel := context.WithCancel(t.Context())
			done := make(chan error, 1)
			go func() { done <- c.Run(ctx) }()

			// Let the single delivery settle, then stop.
			require.Eventually(t, func() bool {
				acks, nacks, _ := st.snapshot()
				return acks+nacks >= 1
			}, time.Second, 5*time.Millisecond)
			cancel()
			<-done

			sink := tc.sink
			if sink == nil {
				sink = &recordingSink{}
			}
			tc.assert(t, st, sink)
		})
	}
}

func TestConsumer_DivertSendFailure_NacksNotAcks(t *testing.T) {
	st := &settle{}
	src := &scriptedSource{deliveries: []msgin.Delivery{newDelivery(order{ID: "o"}, "m1", st)}}
	sink := &recordingSink{failWith: errors.New("sink down")}
	h := func(context.Context, msgin.Message[order]) error { return msgin.Permanent(errors.New("bad")) }

	// I6: a non-nil Backoff so the divert-send-failure Nack carries a non-zero
	// delay (delayFor(1) = Backoff.Delay(0) = Initial). RandomizationFactor 0 =>
	// exact. MaxAttempts 0 (retry forever) keeps the policy valid without a DLQ.
	const initial = 250 * time.Millisecond
	c, err := msgin.NewConsumer[order](src, h,
		msgin.WithInvalidMessageSink[order](sink),
		msgin.WithRetryPolicy[order](msgin.RetryPolicy{Backoff: msgin.ExponentialBackoff{Initial: initial, Mult: 2}}))
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- c.Run(ctx) }()
	require.Eventually(t, func() bool { _, nacks, _ := st.snapshot(); return nacks >= 1 }, time.Second, 5*time.Millisecond)
	cancel()
	<-done

	acks, nacks, delays := st.snapshot()
	assert.Equal(t, 0, acks, "must not Ack a message the sink rejected")
	assert.GreaterOrEqual(t, nacks, 1, "must Nack so the message is not lost")
	require.NotEmpty(t, delays)
	assert.Equal(t, initial, delays[0], "divert-send-failure Nack must carry a non-zero backoff delay (I6)")
}

// I2: a live-value payload whose dynamic type is not T fails the type assert in
// decode -> permanent invalid, diverted to the sink and NOT retried.
func TestConsumer_DecodeFailure_DivertsToInvalidNotRetried(t *testing.T) {
	st := &settle{}
	bad := msgin.Delivery{Msg: msgin.New[any]("not-an-order", msgin.WithID("m1")), Ack: st.ack, Nack: st.nack}
	src := &scriptedSource{deliveries: []msgin.Delivery{bad}}
	sink := &recordingSink{}
	h := func(context.Context, msgin.Message[order]) error { return nil } // never reached

	c, err := msgin.NewConsumer[order](src, h, msgin.WithInvalidMessageSink[order](sink))
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- c.Run(ctx) }()
	require.Eventually(t, func() bool { acks, nacks, _ := st.snapshot(); return acks+nacks >= 1 }, time.Second, 5*time.Millisecond)
	cancel()
	<-done

	acks, nacks, _ := st.snapshot()
	assert.Equal(t, 1, sink.count(), "undecodable payload diverted to invalid sink")
	assert.Equal(t, 1, acks)
	assert.Equal(t, 0, nacks, "decode failure must not be retried")
}

// I3: hooks configured as recording closures must fire on their events. OnAck,
// OnRetry, OnInvalidMessage are covered here; OnDeadLetter is covered in Task 6.
func TestConsumer_Hooks_Fire(t *testing.T) {
	tests := []struct {
		name    string
		handler msgin.Handler[order]
		assert  func(t *testing.T, r *hookRec)
	}{
		{"OnAck fires on success",
			func(context.Context, msgin.Message[order]) error { return nil },
			func(t *testing.T, r *hookRec) { assert.GreaterOrEqual(t, r.count("ack"), 1); assert.Equal(t, 0, r.count("invalid")) }},
		{"OnRetry fires on transient error",
			func(context.Context, msgin.Message[order]) error { return errors.New("temporary") },
			func(t *testing.T, r *hookRec) { assert.GreaterOrEqual(t, r.count("retry"), 1) }},
		{"OnInvalidMessage fires on permanent error",
			func(context.Context, msgin.Message[order]) error { return msgin.Permanent(errors.New("bad")) },
			func(t *testing.T, r *hookRec) { assert.Equal(t, 1, r.count("invalid")); assert.Equal(t, 0, r.count("ack")) }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			st := &settle{}
			r := newHookRec()
			src := &scriptedSource{deliveries: []msgin.Delivery{newDelivery(order{ID: "o"}, "m1", st)}}
			c, err := msgin.NewConsumer[order](src, tc.handler,
				msgin.WithInvalidMessageSink[order](&recordingSink{}),
				msgin.WithHooks[order](r.hooks()))
			require.NoError(t, err)

			ctx, cancel := context.WithCancel(t.Context())
			done := make(chan error, 1)
			go func() { done <- c.Run(ctx) }()
			require.Eventually(t, func() bool { acks, nacks, _ := st.snapshot(); return acks+nacks >= 1 }, time.Second, 5*time.Millisecond)
			cancel()
			<-done
			tc.assert(t, r)
		})
	}
}
```

Ensure `consumer_test.go` imports `"errors"` and `"fmt"`. (`scriptedSource` declares `EmitsLiveValue`, so the live-value path is used and no codec is required.)

- [ ] **Step 2: Run test to verify it fails**

Run: `GOTOOLCHAIN=go1.25.0 go test ./... -run 'TestConsumer_Settlement|TestConsumer_DivertSendFailure' -v`
Expected: FAIL — permanent errors are currently Nacked (Plan 001), so the invalid-sink assertions fail.

- [ ] **Step 3: Write minimal implementation**

```go
// consumer.go — replace dispatch/safeHandle; add divert, fire, finish.
// (dispatch keeps two contexts; Task 7 makes them differ. For now Run passes
//  the same ctx as both arguments — see the interim Run edit below.)

func (c *consumer[T]) dispatch(ctx, settleCtx context.Context, d Delivery) {
	id := d.Msg.ID()

	payload, derr := c.decode(d.Msg)
	if derr != nil {
		// Decode failure = permanent invalid message. Sink-attempt 1.
		if c.divert(settleCtx, c.invalidSink, d, c.hooks.OnInvalidMessage, derr, 1) {
			c.tracker.evict(id)
		}
		return
	}

	msg := Message[T]{payload: payload, headers: d.Msg.headers}
	err := c.safeHandle(ctx, msg)

	if err == nil {
		c.finish(d.Ack(settleCtx))
		fire(c.hooks.OnAck, settleCtx, d.Msg, nil)
		c.tracker.evict(id)
		return
	}
	if isPermanent(err) {
		// Permanent (Permanent(err), or a handler that returned
		// ErrPayloadDecode/ErrPayloadType) → invalid sink. Sink-attempt 1.
		// Note (M8): the attempt tracker is deliberately NOT consulted here.
		if c.divert(settleCtx, c.invalidSink, d, c.hooks.OnInvalidMessage, err, 1) {
			c.tracker.evict(id)
		}
		return
	}
	// Transient: redeliver. Task 6 inserts the lazy `n := c.attempts(d)`, the
	// finite-DLQ branch, and replaces the 0 below with c.policy.delayFor(n).
	fire(c.hooks.OnRetry, settleCtx, d.Msg, err)
	c.finish(d.Nack(settleCtx, true, 0))
}

// divert sends d.Msg to sink and settles safely (NF-3): Ack only if the sink
// accepted it (returns true); on send failure fire the hook and Nack the
// original so it is not lost (returns false). The failure Nack carries a
// non-zero backoff delay c.policy.delayFor(attempt) (spec §7 backoff(sinkAttempts),
// I6) so a down sink is not hammered. A nil sink logs and discards (Ack), so a
// permanent message never loops forever (ADR 0007 D7).
func (c *consumer[T]) divert(ctx context.Context, sink OutboundAdapter, d Delivery, hook func(context.Context, Message[any], error), cause error, attempt int) bool {
	if sink == nil {
		c.logger.Warn("msgin: discarding message; no invalid-message sink configured", "id", d.Msg.ID())
		fire(hook, ctx, d.Msg, cause)
		c.finish(d.Ack(ctx))
		return true
	}
	if err := sink.Send(ctx, d.Msg); err != nil {
		fire(hook, ctx, d.Msg, err)
		c.finish(d.Nack(ctx, true, c.policy.delayFor(attempt))) // non-zero backoff (I6)
		return false
	}
	fire(hook, ctx, d.Msg, cause)
	c.finish(d.Ack(ctx))
	return true
}

// finish records a settle error. Plan 003 releases a flow-control credit here.
func (c *consumer[T]) finish(err error) {
	if err != nil {
		c.logger.Error("msgin: settle failed", "err", err)
	}
}

// fire invokes a nil-safe observability hook.
func fire(hook func(context.Context, Message[any], error), ctx context.Context, msg Message[any], err error) {
	if hook != nil {
		hook(ctx, msg, err)
	}
}

// safeHandle recovers a panicking handler and reports it as ErrHandlerPanic (a
// TRANSIENT failure — retried, not diverted). Fault isolation (CLAUDE.md).
func (c *consumer[T]) safeHandle(ctx context.Context, msg Message[T]) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("%w: %v", ErrHandlerPanic, r)
		}
	}()
	return c.handler(ctx, msg)
}
```

Interim `Run` edit (keep Plan 001's structure; pass `ctx` as both dispatch contexts — Task 7 splits them):

```go
	for i := 0; i < c.workers; i++ {
		go func() {
			defer wg.Done()
			for d := range deliveries {
				c.dispatch(ctx, ctx, d)
			}
		}()
	}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `GOTOOLCHAIN=go1.25.0 go test ./... -race -v`
Expected: PASS, no goroutine leaks.

- [ ] **Step 5: Commit**

```bash
git add consumer.go settlement_doubles_test.go consumer_test.go
git commit -m "feat: guarded settlement switch (permanent->invalid, transient->nack, panic transient)" \
  -m "Implements the spec §7 branch order minus the finite-DLQ branch (Task 6); divert upholds NF-3; ErrHandlerPanic is transient; hooks fired nil-safely." \
  -m "Spec: 001" -m "Plan: 002" -m "ADR: 0002, 0007"
```

---

### Task 6: Attempt-limited dead-letter + backoff delay + native `delivery-count`

**Files:**
- Modify: `consumer.go` (add the finite-DLQ branch + attempt counting + backoff delay to `dispatch`; add `attempts`)
- Test: `consumer_test.go` (append)

**Interfaces:**
- Consumes: `attemptTracker` (Task 4), `RetryPolicy.delayFor` (Task 3), `NativeReliability`, `HeaderDeliveryCount`.
- Produces (unexported): `func (c *consumer[T]) attempts(d Delivery) int`.

**Design note (spec §7 switch, NF-1/NF-2, M8).** Insert the DLQ branch **before** the default (native-redelivery-or-requeue) branch, guarded exactly as spec §7:
```go
case c.policy.MaxAttempts > 0 && n >= c.policy.MaxAttempts && !c.native.NativeDeadLetter():
    if c.divert(settleCtx, c.policy.DeadLetter, d, c.hooks.OnDeadLetter, err, n) { c.tracker.evict(id) }
```
**M8 — `n := c.attempts(d)` is computed *lazily*, only after the success and permanent early-returns**, i.e. exclusively on the transient path that actually needs it. This keeps the tracker untouched on the success/decode/permanent paths (a permanent message must not consume a delivery-count). `n` then drives both the DLQ guard and the redelivery delay. `attempts` prefers the native `msgin.delivery-count` header (int); otherwise it increments the runtime tracker (1-based). The `MaxAttempts > 0` guard preserves retry-forever (`MaxAttempts == 0`) and prevents the nil-`DeadLetter` deref (NF-1). The default branch's `Nack` now carries `c.policy.delayFor(n)` (M10 — a non-zero backoff delay actually reaches the settle closure). The DLQ `divert` passes `n` as its sink-attempt count (I6). Tracker eviction stays terminal-only (NF-2): success/invalid/DLQ evict; transient Nack keeps the entry so the next redelivery increments.

**Hot-path branches introduced:** finite `MaxAttempts` reached + no native DLQ → DeadLetter divert (+ `OnDeadLetter` fires — I3); finite reached + native DLQ true → falls through to Nack (native redelivery); `MaxAttempts == 0` → never DLQ (retry forever, no nil deref); native `delivery-count` header present → header used (tracker untouched); header absent → tracker increments; `delayFor(n)` applied on the transient Nack (M10: `Initial × Mult^(n-1)`); attempts computed lazily (not on success/permanent — M8).

- [ ] **Step 1: Write the failing test**

```go
// consumer_test.go (append)
func TestConsumer_DeadLetterAfterMaxAttempts(t *testing.T) {
	st := &settle{}
	// Re-emit the same failing delivery each time it is Nacked, so attempts climb.
	src := &reemittingSource{st: st, id: "m1", payload: order{ID: "o"}}
	dlq := &recordingSink{}
	r := newHookRec() // I3: assert OnDeadLetter fires
	h := func(context.Context, msgin.Message[order]) error { return errors.New("always fails") }

	c, err := msgin.NewConsumer[order](src, h,
		msgin.WithHooks[order](r.hooks()),
		msgin.WithRetryPolicy[order](msgin.RetryPolicy{
			MaxAttempts: 3,
			DeadLetter:  dlq,
			Backoff:     msgin.ExponentialBackoff{}, // zero => immediate; keeps the test fast
		}))
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- c.Run(ctx) }()

	require.Eventually(t, func() bool { return dlq.count() == 1 }, time.Second, 5*time.Millisecond)
	cancel()
	<-done

	acks, nacks, _ := st.snapshot()
	assert.Equal(t, 1, dlq.count(), "diverted to DLQ once exhausted")
	assert.Equal(t, 2, nacks, "attempts 1 and 2 nacked; attempt 3 dead-lettered")
	assert.Equal(t, 1, acks, "DLQ send acked the source")
	assert.Equal(t, 1, r.count("deadletter"), "OnDeadLetter fired once on divert to DLQ (I3)")
}

// M10: assert the computed backoff delay actually reaches Nack. Deterministic
// (RandomizationFactor=0, MaxAttempts=0 retry-forever, non-nil Backoff): the
// n-th transient Nack must carry Initial × Mult^(n-1).
func TestConsumer_RetryBackoffDelay_ReachesNack(t *testing.T) {
	st := &settle{}
	src := &reemittingSource{st: st, id: "m1", payload: order{ID: "o"}}
	const initial = 10 * time.Millisecond
	const mult = 2.0
	h := func(context.Context, msgin.Message[order]) error { return errors.New("always fails") }

	c, err := msgin.NewConsumer[order](src, h, msgin.WithRetryPolicy[order](msgin.RetryPolicy{
		MaxAttempts: 0, // retry forever
		Backoff:     msgin.ExponentialBackoff{Initial: initial, Mult: mult}, // RandomizationFactor 0 => exact
	}))
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- c.Run(ctx) }()
	require.Eventually(t, func() bool { _, nacks, _ := st.snapshot(); return nacks >= 3 }, 2*time.Second, 5*time.Millisecond)
	cancel()
	<-done

	_, _, delays := st.snapshot()
	require.GreaterOrEqual(t, len(delays), 3)
	assert.Equal(t, initial, delays[0], "attempt 1 -> Delay(0) = Initial")
	assert.Equal(t, time.Duration(float64(initial)*mult), delays[1], "attempt 2 -> Initial*Mult")
	assert.Equal(t, time.Duration(float64(initial)*mult*mult), delays[2], "attempt 3 -> Initial*Mult^2")
}

func TestConsumer_NativeDeliveryCount_UsesHeaderNotTracker(t *testing.T) {
	st := &settle{}
	// Header says this is already attempt 3; with MaxAttempts=3 it must DLQ on
	// the FIRST delivery (no reliance on the runtime tracker).
	m := msgin.New[any](order{ID: "o"}, msgin.WithID("m1"),
		msgin.WithHeaders(map[string]any{msgin.HeaderDeliveryCount: 3}))
	src := &nativeScriptedSource{d: msgin.Delivery{Msg: m, Ack: st.ack, Nack: st.nack}}
	dlq := &recordingSink{}
	h := func(context.Context, msgin.Message[order]) error { return errors.New("fail") }

	c, err := msgin.NewConsumer[order](src, h,
		msgin.WithRetryPolicy[order](msgin.RetryPolicy{MaxAttempts: 3, DeadLetter: dlq}))
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- c.Run(ctx) }()
	require.Eventually(t, func() bool { return dlq.count() == 1 }, time.Second, 5*time.Millisecond)
	cancel()
	<-done

	_, nacks, _ := st.snapshot()
	assert.Equal(t, 0, nacks, "header count drove straight to DLQ, no requeue")
}

func TestConsumer_NativeDeadLetter_FallsThroughToRedelivery(t *testing.T) {
	st := &settle{}
	src := &nativeDLQSource{st: st, id: "m1", payload: order{ID: "o"}} // NativeDeadLetter()=true
	dlq := &recordingSink{}
	h := func(context.Context, msgin.Message[order]) error { return errors.New("fail") }

	c, err := msgin.NewConsumer[order](src, h,
		msgin.WithRetryPolicy[order](msgin.RetryPolicy{MaxAttempts: 1, DeadLetter: dlq}))
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- c.Run(ctx) }()
	require.Eventually(t, func() bool { _, n, _ := st.snapshot(); return n >= 1 }, time.Second, 5*time.Millisecond)
	cancel()
	<-done

	assert.Equal(t, 0, dlq.count(), "native DLQ backend must not use the runtime DLQ")
}
```

Add these doubles to `settlement_doubles_test.go`:

```go
// reemittingSource re-emits its single delivery whenever it is Nacked, capped so
// the test terminates once the message is dead-lettered.
type reemittingSource struct {
	st      *settle
	id      string
	payload any
}

func (s *reemittingSource) EmitsLiveValue() bool { return true }

func (s *reemittingSource) Stream(ctx context.Context, out chan<- msgin.Delivery) error {
	for {
		before := func() int { _, n, _ := s.st.snapshot(); return n }()
		d := msgin.Delivery{
			Msg:  msgin.New[any](s.payload, msgin.WithID(s.id)),
			Ack:  s.st.ack,
			Nack: s.st.nack,
		}
		select {
		case out <- d:
		case <-ctx.Done():
			return ctx.Err()
		}
		// Wait until this delivery is settled (ack or nack) before re-emitting.
		for {
			acks, nacks, _ := s.st.snapshot()
			if acks > 0 || nacks > before {
				break
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Millisecond):
			}
		}
		if acks, _, _ := s.st.snapshot(); acks > 0 {
			<-ctx.Done() // terminal (dead-lettered/acked): stop re-emitting
			return ctx.Err()
		}
	}
}

// nativeScriptedSource emits one delivery then blocks; declares no native reliability.
type nativeScriptedSource struct{ d msgin.Delivery }

func (s *nativeScriptedSource) EmitsLiveValue() bool { return true }
func (s *nativeScriptedSource) Stream(ctx context.Context, out chan<- msgin.Delivery) error {
	select {
	case out <- s.d:
	case <-ctx.Done():
		return ctx.Err()
	}
	<-ctx.Done()
	return ctx.Err()
}

// nativeDLQSource re-emits like reemittingSource but reports a native dead-letter
// pipeline, so the runtime DLQ branch must be skipped.
type nativeDLQSource struct {
	st      *settle
	id      string
	payload any
}

func (s *nativeDLQSource) EmitsLiveValue() bool    { return true }
func (s *nativeDLQSource) NativeRedelivery() bool  { return true }
func (s *nativeDLQSource) NativeDeadLetter() bool  { return true }
func (s *nativeDLQSource) Stream(ctx context.Context, out chan<- msgin.Delivery) error {
	select {
	case out <- msgin.Delivery{Msg: msgin.New[any](s.payload, msgin.WithID(s.id)), Ack: s.st.ack, Nack: s.st.nack}:
	case <-ctx.Done():
		return ctx.Err()
	}
	<-ctx.Done()
	return ctx.Err()
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `GOTOOLCHAIN=go1.25.0 go test ./... -run 'TestConsumer_DeadLetter|TestConsumer_Native' -v`
Expected: FAIL — no DLQ branch yet, so `dlq.count()` stays 0.

- [ ] **Step 3: Write minimal implementation**

```go
// consumer.go — add attempts and the DLQ branch to the switch.

// attempts returns the 1-based delivery-attempt count for d: the native
// msgin.delivery-count header if present, else the runtime tracker (incremented
// once per delivery). Called once per failed dispatch (NF-2).
func (c *consumer[T]) attempts(d Delivery) int {
	if dc, ok := d.Msg.Headers().Int(HeaderDeliveryCount); ok {
		return dc
	}
	return c.tracker.observe(d.Msg.ID())
}
```

Replace the transient tail of `dispatch` (the Task 5 `fire(OnRetry)`/`Nack(…, 0)` lines) with a lazy attempt count and the DLQ branch. The success and permanent early-returns from Task 5 are unchanged and stay **above** this, so `attempts` is reached only on the transient path (M8):

```go
	// ... unchanged from Task 5: decode-fail divert; err == nil -> Ack;
	//     isPermanent(err) -> divert(invalid, attempt 1). Both early-return.

	// Transient path only: NOW compute the attempt count (M8 — lazy; the
	// tracker is never touched on the success/decode/permanent paths).
	n := c.attempts(d)
	switch {
	case c.policy.MaxAttempts > 0 && n >= c.policy.MaxAttempts && !c.native.NativeDeadLetter():
		if c.divert(settleCtx, c.policy.DeadLetter, d, c.hooks.OnDeadLetter, err, n) {
			c.tracker.evict(id)
		}
	default:
		fire(c.hooks.OnRetry, settleCtx, d.Msg, err)
		c.finish(d.Nack(settleCtx, true, c.policy.delayFor(n)))
	}
```

(`n`/`attempts` is computed only after the success and permanent early-returns — success and permanent messages must not touch the tracker beyond eviction. `isPermanent` stays an early return in Task 5's layout, not a `switch` case, so the tracker is untouched on that path.)

- [ ] **Step 4: Run test to verify it passes**

Run: `GOTOOLCHAIN=go1.25.0 go test ./... -race -v`
Expected: PASS, no goroutine leaks.

- [ ] **Step 5: Commit**

```bash
git add consumer.go settlement_doubles_test.go consumer_test.go
git commit -m "feat: attempt-limited dead-letter with backoff delay and native delivery-count" \
  -m "Adds the finite-DLQ switch branch (NF-1 guard), attempt counting preferring msgin.delivery-count (NF-2), and backoff-delayed Nack (spec §7/§8)." \
  -m "Spec: 001" -m "Plan: 002" -m "ADR: 0002, 0005, 0007"
```

---

### Task 7: Bounded graceful shutdown (`WithShutdownTimeout` + clockwork)

**Files:**
- Modify: `consumer.go` (`Run`: cancellable detached settle context + drain context + always-finite `clock.After` deadline; add `const defaultShutdownTimeout`)
- Modify: `docs/adrs/0007-reliability-settlement-api.md` (flesh out D8/D9 shutdown — the C1 always-finite/cancellable-settle revision)
- Test: `consumer_test.go` (append)

**Interfaces:**
- Consumes: `clockwork.Clock` (consumer field, Task 4), `context.WithCancel`/`context.WithoutCancel`.
- Produces: revised `func (c *consumer[T]) Run(ctx context.Context) error`; unexported `const defaultShutdownTimeout = 30 * time.Second`.

**Design note (spec §7 step 3, ADR 0007 D8/D9 — CRITICAL C1: the drain is ALWAYS finite).** Two derived contexts, **both detached from the parent but cancellable**:
- `settleCtx, cancelSettle = context.WithCancel(context.WithoutCancel(ctx))` — Ack/Nack/divert-send run under this so they survive the parent cancel and can reach a durable backend while the flow drains, **yet can be released** on deadline expiry. `defer cancelSettle()`.
- `drainCtx, cancelDrain = context.WithCancel(context.WithoutCancel(ctx))` — the **handler** context. Not cancelled by the parent (so in-flight handlers finish gracefully on shutdown), only by `cancelDrain` on deadline expiry. `defer cancelDrain()`.

**Why `settleCtx` must be cancellable (the C1 deadlock).** The earlier design used a bare, uncancellable `settleCtx = context.WithoutCancel(ctx)`. But the **default** `memory` adapter's `Nack(requeue=true)` calls `Send(settleCtx, m)`, which blocks on a channel the now-stopped `Stream` no longer reads. Because `settleCtx` could never be cancelled, a single transient failure during drain blocked a worker **forever** → `wg.Wait()` never returned → `Run` never returned → goroutine leak. This deadlocked even the `shutdownTimeout <= 0` "wait fully" path. The fix makes the drain unconditionally bounded and lets the deadline release the stuck settle.

**The drain is ALWAYS finite (revises D9/D3).** An unbounded drain violates the "graceful shutdown *within a deadline*" robustness constraint (spec §7.4 / CLAUDE.md), and is what enabled the deadlock. So there is **no infinite-wait path**:
- `WithShutdownTimeout(d)` with `d > 0` bounds the drain to `d`.
- `d <= 0` (the default, and Plan 001's old "wait fully") now uses a **finite default**, `const defaultShutdownTimeout = 30 * time.Second` — NOT infinite.

On parent-cancel, `Stream` returns and `deliveries` is closed; workers drain the buffer. A watcher goroutine closes `done` when the pool joins. `Run` always `select`s `done` vs `c.clock.After(timeout)` (clockwork), where `timeout` is `shutdownTimeout` if positive else `defaultShutdownTimeout`. On expiry it calls **both** `cancelDrain()` (cooperative handlers abort → `ctx.Err()` → transient → Nacked) **and `cancelSettle()`** (so any settle stuck on a non-accepting backend — e.g. memory `Send` — returns `ctx.Err()`, `finish` proceeds, and the worker exits), then `<-done`. This guarantees `Run` **always** returns and no goroutine leaks, for **every** adapter. A handler that *ignores* its context is still bounded — the deadline+`cancelSettle` releases its subsequent settle regardless — but a handler that never returns at all is bounded only by `WithHandlerTimeout` (Plan 003), documented here.

**Hot-path branches introduced:** graceful drain (`done` before deadline); deadline expiry (`clock.After` fires → `cancelDrain`+`cancelSettle` → remaining Nacked, stuck settle released); `shutdownTimeout <= 0` → `defaultShutdownTimeout` finite path (no infinite wait); buffered-delivery-after-cancelDrain short-circuit Nack.

- [ ] **Step 1: Write the failing test**

```go
// consumer_test.go (append)

// C1: the CRITICAL regression test. A transient failure during drain makes the
// REAL memory adapter's Nack(requeue=true) block on Send to a channel the
// stopped Stream no longer reads. With the old uncancellable settleCtx this
// deadlocks (wg.Wait never returns → Run never returns → goroutine leak). The
// fixed, always-finite drain must let Run return and stay goleak-clean, for both
// the default-timeout path and an explicit small WithShutdownTimeout. A fake
// clock keeps it fast and deterministic; the real memory adapter (not a stub
// Nack) is essential — a stub would hide the blocking re-enqueue.
func TestConsumer_Shutdown_TransientNackDuringDrain(t *testing.T) {
	tests := []struct {
		name string
		opts func(clk *clockwork.FakeClock) []msgin.ConsumerOption[order]
	}{
		{"default shutdown timeout (finite, not infinite)", func(clk *clockwork.FakeClock) []msgin.ConsumerOption[order] {
			return []msgin.ConsumerOption[order]{msgin.WithConsumerClock[order](clk)}
		}},
		{"explicit small shutdown timeout", func(clk *clockwork.FakeClock) []msgin.ConsumerOption[order] {
			return []msgin.ConsumerOption[order]{
				msgin.WithConsumerClock[order](clk),
				msgin.WithShutdownTimeout[order](50 * time.Millisecond),
			}
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			b := memory.New() // default: unbuffered, synchronous handoff
			entered := make(chan struct{}, 1)
			h := func(context.Context, msgin.Message[order]) error {
				select {
				case entered <- struct{}{}:
				default:
				}
				return errors.New("transient") // memory Nack(requeue) re-enqueues via Send
			}
			clk := clockwork.NewFakeClock()
			c, err := msgin.NewConsumer[order](b, h, tc.opts(clk)...)
			require.NoError(t, err)

			ctx, cancel := context.WithCancel(t.Context())
			done := make(chan error, 1)
			go func() { done <- c.Run(ctx) }()

			require.NoError(t, b.Send(t.Context(), msgin.New[any](order{ID: "o"})))
			<-entered // a message is in flight
			cancel()  // parent cancel: Stream returns; the transient Nack will block on Send

			clk.BlockUntil(1)      // the drain's clock.After is registered
			clk.Advance(time.Hour) // fire it (>= any configured timeout): cancelSettle releases the stuck Send

			select {
			case err := <-done:
				assert.ErrorIs(t, err, context.Canceled)
			case <-time.After(2 * time.Second):
				t.Fatal("Run did not return: shutdown deadlocked on a transient Nack during drain (C1)")
			}
		})
	}
}

// M9 + I5: deterministic (handlerEntered channel, no time.Sleep) deadline-expiry
// test with TWO deliveries — one in the handler (in-flight), one buffered. On
// expiry the in-flight one is Nacked (cooperative abort) and the buffered one is
// short-circuit Nacked WITHOUT ever entering the handler.
func TestConsumer_ShutdownDeadlineExpiry_NacksInflightAndBuffered(t *testing.T) {
	st1, st2 := &settle{}, &settle{}
	entered := make(chan struct{})
	var mu sync.Mutex
	handled := map[string]bool{}
	h := func(ctx context.Context, m msgin.Message[order]) error {
		id := m.Payload().ID
		mu.Lock()
		handled[id] = true
		mu.Unlock()
		if id == "first" {
			close(entered) // in-flight signal
			<-ctx.Done()   // block until cancelDrain (deadline expiry) releases it
			return ctx.Err()
		}
		return nil
	}
	// scriptedSource emits both; with workers=1 and defaultDeliveryBuffer=1 the
	// second sits buffered while the first blocks in the handler.
	src := &scriptedSource{deliveries: []msgin.Delivery{
		newDelivery(order{ID: "first"}, "m1", st1),
		newDelivery(order{ID: "second"}, "m2", st2),
	}}
	clk := clockwork.NewFakeClock()

	c, err := msgin.NewConsumer[order](src, h,
		msgin.WithConsumerClock[order](clk),
		msgin.WithShutdownTimeout[order](5*time.Second))
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- c.Run(ctx) }()

	<-entered // deterministic: the first delivery is in the handler
	cancel()

	clk.BlockUntil(1)            // drain reached clock.After(5s)
	clk.Advance(5 * time.Second) // fire the deadline

	assert.ErrorIs(t, <-done, context.Canceled) // Run returns (always finite)

	_, n1, _ := st1.snapshot()
	_, n2, _ := st2.snapshot()
	assert.GreaterOrEqual(t, n1, 1, "in-flight message Nacked on deadline expiry")
	assert.GreaterOrEqual(t, n2, 1, "buffered message Nacked after expiry (I5)")
	mu.Lock()
	defer mu.Unlock()
	assert.False(t, handled["second"], "buffered message must be short-circuit Nacked, never handled (I5)")
}

// M12: leave work in flight at cancel() time (do NOT wait for all handled first),
// so this exercises POST-cancel draining rather than pre-cancel completion.
func TestConsumer_GracefulDrain_ProcessesBuffered(t *testing.T) {
	b := memory.New(memory.WithBuffer(8))
	p, err := msgin.NewProducer[order](b)
	require.NoError(t, err)
	for i := 0; i < 5; i++ {
		require.NoError(t, p.Send(t.Context(), msgin.New(order{ID: "o"})))
	}

	var seen atomic.Int64
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	h := func(context.Context, msgin.Message[order]) error {
		select {
		case entered <- struct{}{}:
		default:
		}
		<-release // hold the first handler so we can cancel with work still queued
		seen.Add(1)
		return nil
	}
	c, err := msgin.NewConsumer[order](b, h) // shutdownTimeout 0 => finite default drain
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- c.Run(ctx) }()

	<-entered      // a handler is in flight and more messages are still queued
	cancel()       // cancel BEFORE the in-flight handler completes (post-cancel drain)
	close(release) // let the drain finish the in-flight work

	assert.ErrorIs(t, <-done, context.Canceled)
	assert.GreaterOrEqual(t, seen.Load(), int64(1), "in-flight work drained after cancel, not dropped")
}
```

> Imports: `TestConsumer_GracefulDrain_ProcessesBuffered` uses `sync/atomic` (`atomic.Int64`) — add `"sync/atomic"`; the expiry test uses `"sync"`. Both shutdown tests are deterministic (handler-entry channel + fake clock), with no `time.Sleep` gating.

- [ ] **Step 2: Run test to verify it fails**

Run: `GOTOOLCHAIN=go1.25.0 go test ./... -run 'TestConsumer_Shutdown|TestConsumer_GracefulDrain' -race -v`
Expected: FAIL — against the pre-C1 `Run` (bare `context.WithoutCancel` settle context, unbounded `<-done` on `shutdownTimeout<=0`), `TestConsumer_Shutdown_TransientNackDuringDrain` deadlocks: the memory `Nack(requeue)` blocks on `Send`, `wg.Wait()` never returns, and the 2-second guard fires (`t.Fatal`) plus a `goleak` violation. The expiry test also fails for want of a deadline. This is the point of C1 — the test must not pass until the always-finite, cancellable-settle drain is in place.

- [ ] **Step 3: Write minimal implementation**

```go
// consumer.go — add near defaultDeliveryBuffer.

// defaultShutdownTimeout bounds the drain when WithShutdownTimeout is unset or
// non-positive. The drain is ALWAYS finite (C1): an unbounded drain violates the
// "graceful shutdown within a deadline" constraint and can deadlock on an
// adapter whose Nack blocks on a channel the stopped Stream no longer reads.
const defaultShutdownTimeout = 30 * time.Second
```

```go
// consumer.go — revised Run.
func (c *consumer[T]) Run(ctx context.Context) error {
	deliveries := make(chan Delivery, defaultDeliveryBuffer)

	// Both contexts are detached from the parent (so settles/handlers survive
	// parent cancel during drain) but cancellable, so the deadline can release
	// a settle stuck on a non-accepting backend (C1).
	settleCtx, cancelSettle := context.WithCancel(context.WithoutCancel(ctx))
	defer cancelSettle()
	drainCtx, cancelDrain := context.WithCancel(context.WithoutCancel(ctx))
	defer cancelDrain()

	var wg sync.WaitGroup
	wg.Add(c.workers)
	for i := 0; i < c.workers; i++ {
		go func() {
			defer wg.Done()
			for d := range deliveries {
				if drainCtx.Err() != nil {
					// Deadline already expired: don't start new work, Nack it.
					// settleCtx is cancelled too, so this returns promptly.
					c.finish(d.Nack(settleCtx, true, 0))
					continue
				}
				c.dispatch(drainCtx, settleCtx, d)
			}
		}()
	}

	streamErr := c.src.Stream(ctx, deliveries) // blocks until ctx is cancelled
	close(deliveries)

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()

	// The drain is ALWAYS finite (C1): non-positive timeout uses the default.
	timeout := c.shutdownTimeout
	if timeout <= 0 {
		timeout = defaultShutdownTimeout
	}
	select {
	case <-done:
	case <-c.clock.After(timeout):
		cancelDrain()  // cooperative handlers abort (ctx.Err() -> transient -> Nack)
		cancelSettle() // release any settle stuck on a non-accepting backend (e.g. memory Send)
		<-done
	}
	return streamErr
}
```

Flesh out ADR 0007 **D8/D9** (divert non-zero backoff on send failure; the C1 always-finite, cancellable-settle-context shutdown and cooperative-handler limitation).

- [ ] **Step 4: Run test to verify it passes**

Run: `GOTOOLCHAIN=go1.25.0 go test ./... -race -v`
Expected: PASS, no goroutine leaks.

- [ ] **Step 5: Commit**

```bash
git add consumer.go consumer_test.go docs/adrs/0007-reliability-settlement-api.md
git commit -m "feat: bounded graceful shutdown via WithShutdownTimeout and clockwork" \
  -m "Detached settle context lets in-flight deliveries settle during drain; a clock.After deadline Nacks remaining in-flight and joins goleak-clean (spec §7 step 3)." \
  -m "Spec: 001" -m "Plan: 002" -m "ADR: 0002, 0004, 0007"
```

---

### Task 8: End-to-end example, module hygiene, whole-branch delivery gate

**Files:**
- Create: `example_reliability_test.go` (runnable `Example` — doubles as godoc)
- Verify: `go.mod`/`go.sum` unchanged except any test-only additions; ADR 0007 complete
- Test: whole-suite race + vet + fmt

**Design note.** No new dependency should have entered the core (`slog` is stdlib; `cenkalti` was deliberately avoided). This task adds a documentation-grade example and runs the whole-branch gate (CLAUDE.md §5): `/code-review` + `/security-review` over `main..HEAD`, resolve/triage findings, confirm coverage, then the full `-race` suite.

- [ ] **Step 1: Write the failing test (runnable example)**

```go
// example_reliability_test.go
package msgin_test

import (
	"context"
	"errors"
	"fmt"

	"github.com/kartaladev/msgin"
	"github.com/kartaladev/msgin/adapter/memory"
)

// ExampleConsumer_deadLetter shows a finite RetryPolicy routing a permanently
// failing message to a dead-letter sink.
func ExampleConsumer_deadLetter() {
	dlq := memory.New(memory.WithBuffer(1))

	handler := func(context.Context, msgin.Message[string]) error {
		return msgin.Permanent(errors.New("cannot process"))
	}
	src := memory.New(memory.WithBuffer(1))
	c, err := msgin.NewConsumer[string](src, handler,
		msgin.WithInvalidMessageSink[string](dlq),
		msgin.WithRetryPolicy[string](msgin.RetryPolicy{}),
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

> The example is intentionally construction-only (it must terminate without a live loop for `go test` Output matching). If a fuller runnable flow is desired, gate it on a `context.WithCancel` and drive one message, printing a deterministic line — but keep `// Output:` stable.

- [ ] **Step 2: Run test to verify it fails**

Run: `GOTOOLCHAIN=go1.25.0 go test ./... -run 'Example' -v`
Expected: FAIL first if any symbol is missing; otherwise it compiles and the Output matches once implementation from Tasks 1–7 is in place. (If it already passes, that is acceptable — the example is a doc/coverage artifact, not a red-first requirement.)

- [ ] **Step 3: Module hygiene + coverage check**

Run:
```bash
GOTOOLCHAIN=go1.25.0 go mod tidy
GOTOOLCHAIN=go1.25.0 go test ./... -race
GOTOOLCHAIN=go1.25.0 go test ./... -coverprofile=/tmp/cov.out && GOTOOLCHAIN=go1.25.0 go tool cover -func=/tmp/cov.out | tail -1
GOTOOLCHAIN=go1.25.0 go vet ./... && gofmt -l .
GOTOOLCHAIN=go1.25.0 go mod why github.com/jonboulle/clockwork
```
Expected: suite green race-clean; `package msgin` coverage ≥ 85%; `vet`/`gofmt` silent; `go.mod` shows **no** `cenkalti/backoff` require; only `clockwork` reachable from non-test core.

- [ ] **Step 4: Whole-branch review gate**

Run `/code-review` and `/security-review` over `main..HEAD`; resolve or triage every finding; re-run the affected review and `go test ./... -race`. Confirm the coverage gate (every settlement branch, every `Validate` branch, panic, decode→invalid (I2), handler-returned-`ErrPayloadDecode`→invalid (I7), divert-send-failure + its backoff delay (I6), DLQ, native `delivery-count`, backoff edges incl. both overflow sub-arms (I4), backoff-delay-reaches-Nack (M10), hooks firing (I3), the C1 transient-Nack-during-drain regression, and shutdown-expiry for in-flight + buffered (I5)) has a covering case, and that `go test ./... -race -count=3` is stable (no shutdown-test flakiness).

- [ ] **Step 5: Commit**

```bash
git add example_reliability_test.go go.mod go.sum docs/adrs/0007-reliability-settlement-api.md
git commit -m "test: runnable reliability example; finalize ADR 0007 and module hygiene" \
  -m "Whole-branch gate green (code-review + security-review over main..HEAD, -race clean, coverage >= 85%). Core remains stdlib + clockwork." \
  -m "Spec: 001" -m "Plan: 002" -m "ADR: 0002, 0005, 0007"
```

---

## ADR 0007 outline (`docs/adrs/0007-reliability-settlement-api.md`)

Author with Nygard sections (Title, Status, Context, Decision, Consequences). **Decisions:**

- **D1 — Permanent marker (Task 1).** msgin-native `Permanent(err)` + `*permanentError` (Unwrap), detected via `errors.As`; core stays cenkalti-free (cenkalti enters via `adapter/http` only, ADR 0005). Refines spec §5's `backoff.Permanent`. `ErrPayloadType`/`ErrPayloadDecode` also classify permanent.
- **D2 — `ErrHandlerPanic` (Task 1).** Public sentinel; recovered panics wrap it; classified **transient** (retried).
- **D3 — `RetryPolicy` zero-value + default semantics (Task 3; revised for C1).** `{0, nil, nil}` = retry forever, immediate, no DLQ (the consumer default, preserving Plan 001). `Validate()`: `MaxAttempts<0`→`ErrInvalidMaxAttempts`; finite w/o DeadLetter→`ErrNoDeadLetter`; nil Backoff allowed (= immediate). **Note (C1): the retry-forever *redelivery* default does NOT extend to shutdown** — the consumer's *drain* default (`WithShutdownTimeout` unset) is the **finite** `defaultShutdownTimeout` (D9), not an unbounded wait. "Retry forever" governs redelivery of a live flow; a shutdown must still complete within a deadline, otherwise a transient Nack during drain can block `Run` indefinitely.
- **D4 — Backoff indexing (Task 2).** `BackoffStrategy.Delay(attempt)` is 0-based (0 = first retry ⇒ Initial); default `ExponentialBackoff` = `min(Max, Initial×Mult^attempt)` with guards + optional (non-deterministic) jitter, `RandomizationFactor=0` for exact tests. `attempts(d)` is 1-based; the runtime passes `attempt-1`.
- **D5 — Attempt counting / NF-2 (Tasks 4/6).** Prefer native `msgin.delivery-count`; else runtime `attemptTracker` keyed by id, evicted **only** on terminal settle. **M11 — a `NativeRedelivery()` adapter MUST store `msgin.delivery-count` as a Go `int`**, not `int64`: `Headers.Int(key)` type-asserts `int` only, so an `int64` value would be missed and the runtime would silently fall back to its ephemeral tracker (wrong count for a message that leaves the process). Document this on the header constant / adapter SPI.
- **D6 — Hooks shape (Task 4).** `Hooks{OnRetry, OnDeadLetter, OnInvalidMessage, OnAck func(context.Context, Message[any], error)}`; nil-safe; err arg carries the trigger (nil on Ack).
- **D7 — No-invalid-sink policy (Tasks 4/5).** Absent sink ⇒ log(WARN) + `OnInvalidMessage` + Ack(discard), not an infinite retry. *(Flagged decision — see below.)*
- **D8 — `divert` NF-3 + backoff (Task 5).** Ack only if sink accepted; else fire hook + Nack the original; returns `settled` gating tracker eviction. **I6 — the send-failure Nack carries a non-zero backoff delay** `policy.delayFor(attempt)` (spec §7 `backoff(sinkAttempts)`), not `0`, so a down sink is not hammered; `divert` takes an `attempt int` (invalid/decode callers pass `1`; the DLQ caller passes the climbing `n`).
- **D9 — Shutdown, always finite (Task 7) — revises the earlier "wait fully" default (C1).** `WithShutdownTimeout(d)` bounds the drain via `clockwork.After`; **`d<=0` uses a finite `defaultShutdownTimeout = 30s`, NOT an infinite wait** — an unbounded drain violates spec §7.4 / CLAUDE.md's "graceful shutdown *within a deadline*" and previously **deadlocked** the default memory adapter (its `Nack(requeue)`→`Send` blocks on a channel the stopped `Stream` no longer reads). Settles run under a **cancellable** `settleCtx, cancelSettle := context.WithCancel(context.WithoutCancel(ctx))`; on deadline the runtime calls **both** `cancelDrain()` (cooperative handlers abort → Nack remaining) **and `cancelSettle()`** (releases any settle stuck on a non-accepting backend), then joins — so `Run` **always** returns goleak-clean for every adapter. Handlers that never return at all are still only bounded later by `WithHandlerTimeout` (Plan 003).
- **D10 — `WithConsumerClock` naming (Task 4).** Avoids collision with the `MessageOption` `WithClock` in the same package.

## Design decisions to ratify (ADR 0007) — recommended defaults

1. **Permanent marker: msgin.Permanent vs backoff.Permanent** → **Recommend msgin.Permanent** (keeps core cenkalti-free; spec §8/ADR 0005 keep cenkalti on the HTTP outbound loop). *Genuine deviation from spec §5's wording — ratified in ADR 0007, cross-referenced in the spec.*
2. **No-invalid-sink behavior** → **Recommend discard-with-loud-log + hook (Ack)**. Spec §7 assumes a sink exists but does not specify the unset case. Alternatives: `Nack(requeue=false)` (drops for at-least-once, no-op for memory) or block. Discard-with-log avoids the poison-retry loop while remaining observable. **This is the one spec-unresolved point worth an explicit ratification.**
3. **RetryPolicy zero-value = retry forever** → **Recommend yes** (matches spec `MaxAttempts==0` and preserves Plan 001's default requeue). Production users should set a finite policy; documented on `RetryPolicy`.
4. **Backoff `Delay` indexing** → **Recommend 0-based (first retry = Initial)**, `attempts(d)` 1-based, runtime passes `attempt-1`. Minor; recorded so the two never drift.
5. **Per-message-id-seeded jitter** → **Recommend NOT implementing in Plan 002.** `BackoffStrategy.Delay(attempt)` has no id parameter (spec §8 signature), so id-seeded jitter would require widening the interface. Plan 002 ships attempt-indexed closed-form + optional factor jitter; per-id-reproducible jitter is deferred (would be a new `BackoffStrategy` variant, non-breaking).
6. **Shutdown drain is always finite (C1)** → **Recommend yes — no infinite-wait path.** `WithShutdownTimeout` unset (`<=0`) resolves to `defaultShutdownTimeout = 30s`, and settles run under a cancellable detached context that the deadline releases. This *revises* the earlier "wait fully = block on `<-done`" default, which deadlocked the default memory adapter when a transient failure during drain re-enqueued via a blocking `Send`. Ratified in ADR 0007 D9 (and cross-noted in D3). Alternative considered — a separate "wait forever" opt-in — is rejected as it re-introduces the exact leak the robustness constraint forbids; a caller wanting a long drain sets a large explicit timeout.

## Self-Review

**Spec coverage (§7 runtime settlement, §8 backoff):**
- §7 settlement switch branch order (nil→Ack; permanent→invalid; finite-exhausted→DLQ; default→Nack+delay) → Tasks 5 + 6. ✓
- §7 `divert` NF-3 (Ack only if sink accepted, else Nack **with a non-zero backoff delay** — I6) → Task 5. ✓
- §7 decode-failure → invalid, not retried (I2) + handler-returned `ErrPayloadDecode` → invalid (I7, makes `isPermanent`'s sentinel arm reachable) → Task 5. ✓
- §7 observability hooks actually fire — `OnAck`/`OnRetry`/`OnInvalidMessage` (Task 5) + `OnDeadLetter` (Task 6) each asserted (I3). ✓
- §7 `attempts(d)` native-`delivery-count`-else-tracker, evict-on-terminal (NF-2) → Task 6. ✓
- §7 `noNativeReliability` two-value assert (NF-11) → Tasks 1 + 4. ✓
- §7 MaxAttempts==0 retry-forever guard (NF-1, the earlier nil-panic) → Task 6 (guard) + Task 3 (validation) + explicit test. ✓
- §7 observability (`*slog.Logger`, `Hooks`) → Task 4 (surface) + Tasks 5/6/7 (firing). ✓
- §7 graceful shutdown — **always finite** (C1), deadline expiry Nacks in-flight AND buffered (I5), cancellable settle context releases a stuck memory `Nack`, `defaultShutdownTimeout=30s` when unset, deterministic tests (handler-entry channel + fake clock, M9), and the post-cancel drain leaves work in flight (M12) → Task 7. ✓
- §8 `BackoffStrategy.Delay` closed-form + edge guards incl. overflow→cap and overflow→Initial (I4); the transient Nack delay reaching the settle closure = `Initial × Mult^(n-1)` (M10) → Tasks 2 + 6. ✓
- §5 construction-time typed errors (finite w/o DeadLetter) → Task 3 + Task 4. ✓
- `ErrHandlerPanic` public + transient → Tasks 1 + 5. ✓
- **Deferred (documented):** credit/`WithMaxInFlight`/rate-limit/handler-timeout/circuit-breaker/overflow → Plan 003 (`finish` is settle-only here, no credit accounting; in-process delay-park not built — memory ignores Nack delay). Poller/`PollingSource` → Plan 004.

**Placeholder scan:** the `Nack(..., 0)` in Task 5's default branch is an intentional interim (immediate) replaced by `delayFor(n)` in Task 6; the two-context `dispatch(ctx, ctx, d)` in Task 5 is an intentional interim split properly in Task 7. Both are called out at their step. No `TODO`/"handle edge cases"/unshown code remain.

**Type consistency:** `dispatch(ctx, settleCtx, d)` signature is consistent Tasks 5→7; `divert(...) bool` returns `settled` used identically in Tasks 5/6; `attempts(d) int` (1-based) feeds `policy.delayFor(n)` (converts to 0-based) — indices reconciled in D4. `WithConsumerClock` (not `WithClock`) used throughout. `Hooks` field set (`OnRetry/OnDeadLetter/OnInvalidMessage/OnAck`) identical in Tasks 4/5/6. `recordingSink`/`scriptedSource`/`settle` defined once in `settlement_doubles_test.go` (Task 5), extended in Task 6.

## Notes for Plan 003 (Resilience & flow control — spec §7.4)

Plan 003 turns `finish` into a credit-release point and adds the flood defense: `WithMaxInFlight(n)` (credit acquired at fetch, released at every terminal settle — every `finish`/`divert`/Nack path), the separately-bounded in-process delay-park that finally **honors** the backoff delay for adapters (like memory) with no server-side delay, `WithRateLimit`, `WithHandlerTimeout` (bounds the non-cooperative-handler leak Task 7 documented), `WithCircuitBreaker`, and `WithOverflow`. The internal `defaultDeliveryBuffer` becomes the `n`-sized credit pool for streaming sources. Plan 004 then drives `PollingSource` through the shared Poller (the credit gate governs its fetch batch size).
