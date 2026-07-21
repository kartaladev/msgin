# Plan 023 — Producer-side outbound retry (`WithProducerRetry`, `RetryAfter`)

> **For agentic workers:** REQUIRED SUB-SKILL: use `superpowers:subagent-driven-development` (the project default) or
> `superpowers:executing-plans`. Steps use checkbox (`- [ ]`) syntax.
>
> **Go-skills hard rule (CLAUDE.md, restated because `writing-plans` omits it):** every task starts from
> **`cc-skills-golang:golang-how-to`**, which routes to the applicable `golang-*` skills (here: `golang-error-handling`,
> `golang-concurrency`, `golang-context`, `golang-design-patterns`, `golang-safety`, `golang-testing`).
> **`superpowers:test-driven-development`** governs every task (red → green → refactor). **`gopls`** (via the `LSP`
> tool) is mandatory for navigation, symbol lookup and refactoring — not `grep`. The project-local **`table-test`**
> override governs every table's shape (assert-closure form, `t.Context()`). `use-mockgen` and `use-testcontainers` do
> **not** apply: every double here is a hand-written blackbox fake and there is no external resource.

**Goal:** Close the outbound retry gap. `Producer.Send` is a bare passthrough to `OutboundAdapter.Send` with no retry,
no backoff and no dead-letter. This plan adds `WithProducerRetry(RetryPolicy)`, the `RetryAfter(err, d)` marker, and the
safety bounds the design audit proved are required for that loop to be safe on a caller's goroutine.

**Architecture:** Reliability stays runtime-owned (ADR 0002): the adapter only **classifies** (`Permanent` /
`RetryAfter` / plain) and the producer **decides**. No new dependency — the existing `RetryPolicy` /
`BackoffStrategy` / `ExponentialBackoff` machinery is reused verbatim, and waits run on the producer's
already-injected `clockwork.Clock`.

**Tech stack:** Go 1.25, stdlib + `clockwork`. No new dependency; `go.mod`/`go.sum` must be byte-identical at the end.

**Traceability:** Implements [Spec 013](../specs/013-producer-outbound-retry.md). Decided by
[ADR 0025](../adrs/0025-producer-outbound-retry.md), which supersedes the outbound-HTTP clause of
[ADR 0005](../adrs/0005-cenkalti-backoff-dependency.md) and honors [ADR 0002](../adrs/0002-adapter-spi.md)
(runtime-owned reliability) and [ADR 0004](../adrs/0004-clockwork-dependency.md) (injectable time).
Followed by [Plan 024](024-http-outbound.md) (HTTP outbound O1/O2), which is the first *producer* of `RetryAfter`
markers. Independent of, and un-coupled to, [Plan 022](022-header-message-id-rename.md).

> **Why this is its own increment.** Spec 013 §7 records the reversal: the round-1 adversarial audit (three independent
> Opus auditors over spec + ADR + the combined plan) returned **3 critical and ~20 major findings** spanning core, HTTP
> and the SPI at once. The core lands and is reviewed alone, so the HTTP increment's `/security-review` faces a diff
> that is only HTTP.

---

## Global Constraints

- **Go 1.25 only.** `GOTOOLCHAIN=go1.25.12` on every invocation (a bare `go1.25` is rejected as "a language version but
  not a toolchain version").
- **No new dependency.** This increment *removes* a phantom one from the documentation; `go.mod`/`go.sum` end unchanged.
- **Blackbox tests only** — `package msgin_test`, exported API only. A branch that seems to need a whitebox test is
  either reachable through the public surface or should not exist (see the `retryAfterOf` note in Task 1).
- **Assert-closure tables always** (`table-test`): every case carries `assert func(t *testing.T, …)`, never
  `want`/`wantErr` fields. `t.Context()`, never `context.Background()`, outside `Example` functions.
- **Fake clock, no real sleeps.** Retry timing is driven by `clockwork.NewFakeClock()`. The single exception is
  `ExampleWithProducerRetry` (Task 5), which cannot inject a clock through an `// Output:` block.
- **`BlockUntilContext`, never `BlockUntil`.** `BlockUntil` is `// Deprecated:` in clockwork v0.5.0 in favour of
  `BlockUntilContext`, "which offers context cancellation to prevent deadlock". The repo already uses the context form
  in ~10 sites in `aggregator_test.go`. Use the context form everywhere here too: a driver that blocks on the
  deprecated `BlockUntil` and never reaches the expected waiter count hangs with no way out, whereas
  `BlockUntilContext` surfaces it as a test failure bounded by `t.Context()`.
- **Never call `require`/`t.Fatal`/`t.FailNow` from a spawned goroutine.** `t.FailNow` off the test goroutine calls
  `runtime.Goexit`, producing a `goleak` straggler storm that masks the real failure. Buffer the value and assert on the
  test goroutine.
- **Measured waits, two-phase.** An assertion that advances by `wantWait` and then checks elapsed `== wantWait` is true
  by construction and cannot detect *under*-waiting. Every timing assertion in this plan uses the two-phase form:
  `Advance(want - 1ns)` → assert **not yet returned** → `Advance(1ns)` → assert returned. (Plan 021 lesson: a
  concurrency test passed, was `-race` clean and line-covered while hitting its target arm **0 times in 200 runs**.)
- **`goleak`** guards every test that spawns a goroutine.
- **Coverage gate:** every function in `producer.go`, `reliability.go`, `backoff.go` and `codec.go` at **100.0%** at the
  end of Task 5; package total ≥ 85% (it is 99.1% today, so it must not regress).

## Baseline (capture before Task 1; re-verify if the tree has drifted)

```bash
cd /Users/zakyalvan/Documents/RND/msgin
GOTOOLCHAIN=go1.25.12 go test ./... -race >/dev/null && echo GREEN
GOTOOLCHAIN=go1.25.12 go test ./... -cover -count=1 2>&1 | grep coverage \
  | sed -E 's/[[:space:]]+(\(cached\)|[0-9.]+s)[[:space:]]+/ /'
```

| Fact | Value |
|---|---|
| `go test ./... -race` | all 6 packages `ok` |
| Coverage | core **99.1%**, `adapter/http` **100.0%**, `adapter/http/stdlib` **100.0%**, `adapter/database/sql` **93.7%**, `adapter/memory` **71.3%**, `adapter/cron` **50.8%** |
| `grep -c cenkalti go.mod go.sum` | **0** — the dependency ADR 0005 ratified was never added |
| `grep -rn cenkalti --include='*.go' .` | **1** — a stale comment in `reliability.go` |

> **Branch note.** The current branch `feat/producer-retry-http-outbound` was named for the withdrawn combined
> increment. It already carries this plan's governing spec (`df7eacb`) and ADRs (`8459d07`). Keep it for Plan 023 — that
> is now its natural scope — or rename it locally (`git branch -m feat/producer-outbound-retry`); it has never been
> pushed. Do **not** cherry-pick the design commits elsewhere.

---

## Design deltas the audit forced (read before Task 1)

The round-1 audit found the drafted design unsafe in six specific ways. Each is folded into a task below; this section
is the single place they are stated together, so an implementer can see *why* the loop is shaped as it is. **Spec 013
§3 and ADR 0025 §1/§3 are amended to match, in Task 5 — the artifacts must not be left describing the pre-audit
design.**

| # | Audit finding | Resolution | Task |
|---|---|---|---|
| D1 | **Dead-letter runs on the cancelled `ctx`** — a `ctx` cancelled mid-retry (or a `ctx` whose deadline expired, which is *why* the loop is ending) makes the DLQ send fail too, so the message reaches neither target nor DLQ. | Divert on `context.WithoutCancel(ctx)`, the repo's existing precedent at `exchange.go:347`. Add a covering branch. | 4 |
| D2 | **Unbounded retry / hot spin.** `RetryPolicy{}` is a *valid* zero value meaning "retry forever, immediately". On the producer that is a zero-delay infinite loop **on the caller's goroutine**. | Three independent bounds: construction rejects `MaxAttempts == 0 && Backoff == nil` (`ErrUnboundedRetry`); every computed wait is floored to `minRetryDelay` (100ms); a cumulative, **always-on** `WithProducerRetryBudget` (2m default) bounds total wall-clock, marking a budget stop with `ErrRetryBudgetExhausted`. | 1, 4 |
| D3 | **`Retry-After` is a MINIMUM, not an override** (RFC 9110 §10.2.3). The drafted design let a server *shorten* the client's backoff — to zero — which is a remote-triggerable hot spin. | Effective wait = `max(computed, min(serverDelay, cap))`. | 4 |
| D4 | **`ExponentialBackoff` can return a NEGATIVE delay.** `Delay` guards only `IsInf`/`IsNaN`; a large-but-finite `d` (e.g. `Initial=1s, Mult=2, attempt=100` ≈ 1.27e39) exceeds `MaxInt64`, and the out-of-range float→int conversion yields `MinInt64` on amd64. The result is negative, so it also slips past the `out > b.Max` cap. **Pre-existing; affects the Consumer's Nack delay today.** | Fix in `backoff.go` at the correct layer — clamp before conversion — with its own task and test. The producer's floor (D2) is defence in depth, not the fix. | 2 |
| D5 | **A dead-letter divert is invisible.** The caller cannot distinguish "dead-lettered" from "failed outright", and the producer fires no hooks and has no logger — against CLAUDE.md's mandatory observability constraint. | `ErrDeadLettered` sentinel joined onto the cause via `fmt.Errorf("%w: %w", …)`, plus `WithProducerHooks` reusing the existing `Hooks` type (`OnRetry`/`OnDeadLetter` already exist at `retry.go:52`). | 1, 4 |
| D6 | **"`Permanent` beats `RetryAfter`" is documented everywhere and tested nowhere.** The drafted test asserted only `errors.Is(err, cause)`, which is true of either marker independently and therefore proves nothing. | The precedence test asserts the **observable consequence**: attempt count `== 1`, DLQ count `== 0`, and zero clock advance. | 4 |

Two further audit findings are **out of scope here and belong to Plan 024** — the redirect-following SSRF and the
reflected-XSS-from-the-outbound-side. They are recorded in `docs/HANDOVER.md` §5; do not attempt them in this plan.

### D7 — Multi-instance topology (CLAUDE.md mandatory rule)

`WithProducerRetry` is **per-process by construction**: each instance's retry loop is independent state on one
goroutine. In the common N-instances-behind-a-load-balancer topology, N instances retrying a throttling endpoint
deliver **N× the load the server asked to shed**, and `Retry-After` compliance is per-instance, not fleet-wide.

This is stated, not solved: coordinating a fleet-wide retry budget needs shared state the core cannot assume.
**The named seam is [ADR 0006](../adrs/0006-resilience-flow-control.md)'s rate-limit and circuit-breaker interfaces** —
both already clockwork-driven and pluggable, and both the correct home for a distributed limiter backed by Redis or a
DB. The godoc on `WithProducerRetry` must say this. No core change is required to adopt it later.

---

## Task 1: The `RetryAfter` marker, the new sentinels, and retiring the phantom cenkalti dependency

**Files:**
- Modify: `reliability.go` — add the marker + `retryAfterOf`; correct the stale cenkalti paragraph in `Permanent`'s godoc
- Modify: `errors.go` — add six sentinels, so Tasks 2–4 need no further `errors.go` edit
- Modify: `CLAUDE.md` — Dependency policy: four → three accepted exceptions
- Test: `reliability_test.go` (append)

**Interfaces:**
- **Produces** (Task 4 consumes): `func RetryAfter(err error, d time.Duration) error`;
  unexported `func retryAfterOf(err error) (time.Duration, bool)`;
  the **six** sentinels `ErrInvalidRetryAfterCap`, `ErrInvalidRetryBudget`, `ErrUnboundedRetry`,
  `ErrRetryBudgetExhausted`, `ErrInvalidDeadLetterTimeout`, `ErrDeadLettered`.
- **Consumes:** the existing `isPermanent(err) bool` and `permanentError` in the same file.

**Hot-path branches this task introduces:**

| # | Branch | Covering case |
|---|--------|---------------|
| 1 | `RetryAfter(nil, d)` → `nil` | `nil error stays nil` |
| 2 | `RetryAfter(err, d)`, `d > 0` → wraps, `Unwrap`-transparent | `wraps transparently for errors.Is` |
| 3 | `d < 0` normalized to `0` | `negative delay is normalized` |
| 4 | `retryAfterOf(marker)` → `(d,true)` | **Task 4** — not observable here |
| 5 | `retryAfterOf(non-marker)` → `(0,false)` | **Task 4** — not observable here |

> **Branches 4/5 are deliberately NOT in this task's coverage gate.** `retryAfterOf` is unexported and this plan is
> blackbox-only; both arms are exercised through `Producer.Send` in Task 4. Do **not** add a whitebox test to reach
> them, and do **not** fail Task 1 on `reliability.go` being below 100% — it reaches 100% at Task 4's gate. (Round-2
> plan-craft lesson: do not put a branch in Task N's coverage table if it is only observable in Task N+2.)
>
> **There is deliberately no `retryAfterOf(nil)` branch.** The drafted code had an `if err == nil` guard; it is
> redundant (`errors.As(nil, &re)` already returns false) and unreachable through the public API (`nextDelay` only
> calls it with a non-nil `err`), so it is **not written in the first place** — see Task 4 Step 8's
> "delete, don't `nolint`, don't whitebox" note, which this pre-empts.

- [ ] **Step 1: Write the failing test**

Append to `reliability_test.go` (already `package msgin_test`; confirm its existing imports before adding).

```go
func TestRetryAfter(t *testing.T) {
	t.Parallel()

	cause := errors.New("boom")

	tests := []struct {
		name   string
		err    error
		delay  time.Duration
		assert func(t *testing.T, got error)
	}{
		{
			name:  "nil error stays nil",
			err:   nil,
			delay: 5 * time.Second,
			assert: func(t *testing.T, got error) {
				t.Helper()
				require.NoError(t, got)
			},
		},
		{
			name:  "wraps transparently for errors.Is",
			err:   cause,
			delay: 5 * time.Second,
			assert: func(t *testing.T, got error) {
				t.Helper()
				require.Error(t, got)
				assert.ErrorIs(t, got, cause)
				assert.Contains(t, got.Error(), "boom")
			},
		},
		{
			name:  "negative delay is normalized, still wraps",
			err:   cause,
			delay: -1 * time.Second,
			assert: func(t *testing.T, got error) {
				t.Helper()
				require.Error(t, got)
				assert.ErrorIs(t, got, cause)
			},
		},
		{
			name:  "zero delay still wraps",
			err:   cause,
			delay: 0,
			assert: func(t *testing.T, got error) {
				t.Helper()
				require.Error(t, got)
				assert.ErrorIs(t, got, cause)
			},
		},
		{
			name:  "a sentinel cause stays matchable",
			err:   msgin.ErrPayloadTooLarge,
			delay: time.Second,
			assert: func(t *testing.T, got error) {
				t.Helper()
				assert.ErrorIs(t, got, msgin.ErrPayloadTooLarge)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tt.assert(t, msgin.RetryAfter(tt.err, tt.delay))
		})
	}
}
```

> **No `TestRetryAfterPermanentPrecedence` here.** The drafted plan put one in this task asserting only
> `errors.Is(err, cause)` — true of both markers independently, so it proves nothing about precedence (audit finding
> D6). Precedence is observable only through the retry loop and is tested in **Task 4**.

- [ ] **Step 2: Run the test — verify it fails**

```bash
GOTOOLCHAIN=go1.25.12 go test -run 'TestRetryAfter' . 2>&1 | head -20
```

Expected: FAIL — `undefined: msgin.RetryAfter`.

- [ ] **Step 3: Write the minimal implementation**

In `reliability.go`, add `"time"` to the imports and append:

```go
// retryAfterError marks a transient error with a server-instructed minimum
// delay. Wrapping is transparent to errors.Is/As via Unwrap.
type retryAfterError struct {
	err error
	d   time.Duration
}

func (e *retryAfterError) Error() string {
	return "msgin: retry after " + e.d.String() + ": " + e.err.Error()
}

func (e *retryAfterError) Unwrap() error { return e.err }

// RetryAfter marks err as transient with a server-provided MINIMUM delay: a
// producer configured with WithProducerRetry waits at least d before the next
// attempt. It is how an adapter relays an explicit server instruction (an HTTP
// Retry-After header on a 429 or 503) that a BackoffStrategy, being stateless
// and closed-form, cannot express.
//
// d is a FLOOR, not an override (RFC 9110 §10.2.3: Retry-After is the minimum
// time the client should wait). The effective wait is
//
//	max(policyBackoff, min(d, WithProducerRetryAfterCap))
//
// and is always additionally bounded by ctx and by WithProducerRetryBudget. A
// server therefore cannot SHORTEN the client's own backoff — including to zero,
// which would be a remote-triggerable hot spin — it can only lengthen it, up to
// the cap.
//
// It mirrors Permanent: same wrapper shape, same Unwrap transparency, same nil
// handling. RetryAfter(nil, d) returns nil. A negative d is normalized to 0
// (meaning "no server-instructed floor") rather than rejected, so a skewed or
// already-elapsed server deadline degrades to the computed backoff instead of
// an error.
//
// Permanent WINS over RetryAfter when both markers are present, in either
// nesting order: permanent means do not retry, so a delay is meaningless.
//
// A RetryAfter marker on an error returned to a Producer WITHOUT
// WithProducerRetry is inert — there is no retry loop to honour it.
func RetryAfter(err error, d time.Duration) error {
	if err == nil {
		return nil
	}
	if d < 0 {
		d = 0
	}
	return &retryAfterError{err: err, d: d}
}

// retryAfterOf reports the server-instructed minimum delay carried by err, if
// any, matching isPermanent's structure (errors.As over the wrap chain).
//
// Deliberately NO `if err == nil` guard: errors.As(nil, &re) already returns
// false, and the only caller (nextDelay) never passes nil, so the guard would be
// both redundant and blackbox-unreachable.
func retryAfterOf(err error) (time.Duration, bool) {
	var re *retryAfterError
	if errors.As(err, &re) {
		return re.d, true
	}
	return 0, false
}
```

- [ ] **Step 4: Run the test — verify it passes**

```bash
GOTOOLCHAIN=go1.25.12 go test -run 'TestRetryAfter' -race .
```

Expected: `ok github.com/kartaladev/msgin`.

> `retryAfterOf` is unused until Task 4. `go vet` does not flag unused package-level funcs, and this repo's
> `.golangci.yml` sets `linters.default: none` and does **not** enable `unused`, so nothing here will complain —
> **verified**. Either way, do **not** silence anything with a `nolint`. The full `golangci-lint run ./...` gate runs
> once, at **Task 4 Step 9**, not here.

- [ ] **Step 5: Correct the stale cenkalti paragraph in `reliability.go`**

Replace `Permanent`'s closing godoc paragraph (currently `reliability.go:17–19`):

```go
// msgin uses its own marker rather than cenkalti/backoff.Permanent so the core
// runtime stays stdlib + clockwork (see ADR 0007); cenkalti enters only via the
// outbound-HTTP adapter (ADR 0005).
```

with:

```go
// msgin uses its own marker rather than a third-party backoff library's
// Permanent so the core runtime stays stdlib + clockwork (ADR 0007). ADR 0005
// once reserved cenkalti/backoff/v4 for an adapter-side outbound-HTTP retry
// loop; that clause is SUPERSEDED by ADR 0025 — outbound retry is producer-side
// and reuses RetryPolicy (see WithProducerRetry), and cenkalti/backoff is not a
// dependency of this module.
```

- [ ] **Step 6: Add the six sentinels to `errors.go`**

The six are `ErrInvalidRetryAfterCap`, `ErrInvalidRetryBudget`, `ErrUnboundedRetry`, `ErrRetryBudgetExhausted`,
`ErrInvalidDeadLetterTimeout` and `ErrDeadLettered` — all of them, in this one step, so Tasks 2–4 need no further
`errors.go` edit.

Insert immediately after the `ErrInvalidMaxAttempts` declaration (`errors.go:30`), matching the file's existing
one-doc-comment-per-sentinel style:

```go
	// ErrInvalidRetryAfterCap is returned by NewProducer when an explicit
	// WithProducerRetryAfterCap is <= 0. Leaving the option unset takes the
	// documented default instead of reaching this error (the set-flag pattern,
	// as in WithMaxInFlight/WithAttemptTTL): only an explicit non-positive value
	// is rejected, so a caller mistake is a construction error rather than a
	// silently-disabled clamp.
	ErrInvalidRetryAfterCap = errors.New("msgin: retry-after cap must be > 0")

	// ErrInvalidRetryBudget is returned by NewProducer when an explicit
	// WithProducerRetryBudget is <= 0. As with the cap, the unset case takes the
	// documented default; only an explicit non-positive value is an error.
	ErrInvalidRetryBudget = errors.New("msgin: retry budget must be > 0")

	// ErrUnboundedRetry is returned by NewProducer for a producer RetryPolicy
	// that is unbounded in BOTH dimensions: MaxAttempts == 0 (retry forever) with
	// a nil Backoff (no delay). That combination — which is exactly the valid
	// RetryPolicy zero value — is a zero-delay infinite loop on the CALLER'S
	// goroutine, so the producer rejects it at construction. It stays valid on
	// NewConsumer, where "retry forever, immediately" means broker redelivery,
	// not a spin. Set a MaxAttempts (with a DeadLetter) or a Backoff, or both.
	ErrUnboundedRetry = errors.New("msgin: producer retry policy must bound attempts or delay")

	// ErrRetryBudgetExhausted is joined onto the error returned by Producer.Send
	// when the retry loop stopped because the next wait would have overrun
	// WithProducerRetryBudget, rather than because MaxAttempts was spent. The
	// budget always applies — including to a finite MaxAttempts — so this
	// sentinel is what stops it silently truncating an explicit attempt count:
	// errors.Is(err, ErrRetryBudgetExhausted) says "the policy wanted more time
	// than the caller allowed", which is a different operational signal from
	// "we tried N times and gave up".
	ErrRetryBudgetExhausted = errors.New("msgin: retry budget exhausted before attempts were spent")

	// ErrInvalidDeadLetterTimeout is returned by NewProducer when an explicit
	// WithProducerDeadLetterTimeout is <= 0. The unset case takes the documented
	// 30-second default; there is deliberately no "no timeout" value, because the
	// divert runs on a ctx detached from the caller's and an unbounded detached
	// call can block the caller's goroutine forever.
	ErrInvalidDeadLetterTimeout = errors.New("msgin: dead-letter timeout must be > 0")

	// ErrDeadLettered is joined onto the error returned by Producer.Send when the
	// retry loop ended — because attempts were spent, because the retry budget
	// was exhausted, or because ctx was cancelled during a backoff wait — AND the
	// message was successfully diverted to the policy's DeadLetter sink. It lets
	// a caller tell "the message is safely in the DLQ" from "the send failed
	// outright" — errors.Is(err, ErrDeadLettered) — while the causing error stays
	// matchable through the same wrap chain.
	//
	// The guarantee is scoped to errors PRODUCED BY THIS PRODUCER. Being an
	// exported sentinel, nothing stops a third-party OutboundAdapter from wrapping
	// it into an error of its own, so errors.Is(err, ErrDeadLettered) on an error
	// that did not come out of Producer.Send proves nothing. Do not use it as an
	// authorization or audit primitive.
	ErrDeadLettered = errors.New("msgin: message diverted to the dead-letter sink")
```

- [ ] **Step 7: Correct CLAUDE.md's dependency policy (ADR 0025 §4 obligation)**

Three edits in `CLAUDE.md`:

1. **Dependency policy** — `with four accepted, ADR-justified third-party exceptions` → `with three accepted,
   ADR-justified third-party exceptions`.
2. Delete the whole `cenkalti/backoff/v4` bullet from that list.
3. **Architecture blueprint → The six shipped adapters**, HTTP bullet: change the tail
   ``outbound webhook `POST` with cenkalti/backoff retry`` to
   ``outbound webhook `POST`, retried by the producer's `RetryPolicy` (ADR 0025)``.

> Do **not** touch `docs/adrs/0005-cenkalti-backoff-dependency.md` — its supersession annotation is already in history
> from the design commits. Do **not** delete ADR 0005; it is superseded in one clause, not withdrawn.

- [ ] **Step 8: Verify**

```bash
test -z "$(GOTOOLCHAIN=go1.25.12 gofmt -l .)" && \
GOTOOLCHAIN=go1.25.12 go vet ./... && \
GOTOOLCHAIN=go1.25.12 go test ./... -race && \
GOTOOLCHAIN=go1.25.12 go mod tidy && git diff --exit-code go.mod go.sum && \
! grep -rn '"github.com/cenkalti' --include='*.go' . && \
! grep -q cenkalti go.mod go.sum && echo "CENKALTI: not imported, not in the root module"
```

(`gofmt -l` exits **0 while listing files**, so a bare `gofmt -l . && …` silently passes on unformatted code — the
`test -z` wrapper is what makes it a real gate.)

> **This gate was rewritten — the original form was self-contradictory and could never have passed.** It ran
> `! grep -rn 'cenkalti' --include='*.go' --include='go.mod' --include='go.sum' .`, a **bare-substring, whole-tree**
> search, and two things in this repo match it unavoidably:
>
> 1. **Step 5 of this very task writes a godoc containing the word "cenkalti"** — deliberately, to record that the
>    dependency was *not* adopted. The gate would have failed on the text the same task was told to write.
> 2. The **test-only** `adapter/cron/crontest` and `adapter/database/sql/dbtest` modules carry
>    `github.com/cenkalti/backoff/v4 v4.3.0 // indirect` — a **pre-existing** transitive dependency of
>    `testcontainers-go`, in `go.mod`/`go.sum` files this increment neither introduces nor can remove. Six lines,
>    matched before Task 1 started.
>
> What the gate actually needs to assert is the ADR 0025 §4 claim: **no Go file imports it, and it is not in the root
> module's dependency set.** Hence the import-scoped pattern (`"github.com/cenkalti`, with the opening quote, matching
> an import path rather than prose) and the explicit two-file `go.mod`/`go.sum` check. For a human read,
> `grep -c cenkalti go.mod go.sum` prints `go.mod:0` and `go.sum:0`; note it **exits 1** when every count is zero, so
> it cannot be chained with `&&` — use the `! grep -q` form above in the gate.

Expected: no `gofmt` output; `go vet` silent; all packages `ok`; `go.mod`/`go.sum` unchanged; no Go file imports
cenkalti and the root module does not require it, printing the `CENKALTI:` line. Remaining matches in `docs/`, in
`reliability.go`'s godoc, and in the two test-only modules' `go.mod`/`go.sum` are correct and expected.

- [ ] **Step 9: Commit**

```bash
git add reliability.go reliability_test.go errors.go CLAUDE.md docs/plans/023-producer-outbound-retry.md
git commit -m "$(cat <<'EOF'
feat(core): add the RetryAfter marker and retire the phantom cenkalti dependency

RetryAfter(err, d) mirrors Permanent(err) to carry a server-instructed MINIMUM
delay that a stateless BackoffStrategy cannot express; per RFC 9110 the delay is
a floor, so a server can lengthen the client's backoff but never shorten it.
Adds the six sentinels the producer option validates against, or reports through,
in a later task: ErrInvalidRetryAfterCap, ErrInvalidRetryBudget,
ErrInvalidDeadLetterTimeout, ErrUnboundedRetry (which rejects the RetryPolicy
zero value on the producer path, because "retry forever, immediately" is a spin
on the caller's goroutine there), ErrRetryBudgetExhausted (which distinguishes a
budget stop from genuine attempt exhaustion) and ErrDeadLettered (which makes a
terminal divert visible to the caller instead of silent).

Corrects CLAUDE.md's dependency policy and reliability.go's godoc: cenkalti
/backoff/v4 was ratified by ADR 0005 for an adapter-side retry loop that never
shipped, is absent from go.mod, and is superseded by ADR 0025.

Spec: 013
Plan: 023
ADR: 0025
EOF
)"
```

---

## Task 2: Fix `ExponentialBackoff.Delay`'s negative-duration overflow

**A pre-existing bug, not new work.** `Delay` guards `math.IsInf`/`math.IsNaN` but not the finite-yet-out-of-range case.
`float64(time.Second) * math.Pow(2, 100)` ≈ 1.27e39 is finite and far above `MaxInt64` (9.22e18); Go leaves an
out-of-range float→int conversion implementation-defined, and on amd64 `CVTTSD2SI` yields `MinInt64`. The result is a
**negative** `time.Duration`, which then slips past the `if b.Max > 0 && out > b.Max` cap because a negative value is
not greater than `Max`.

**Consequences today:** the Consumer Nacks with a negative redelivery delay. **Consequence if left unfixed:** Task 4's
producer loop would take a `d <= 0` fast path and spin. Fixing it at the `backoff.go` layer fixes both; the producer's
floor (Task 4) is defence in depth, not the fix.

**Files:**
- Modify: `backoff.go`
- Test: `backoff_test.go` (append to the existing table — **verify its shape with `gopls`/`Read` first**)

**Hot-path branches this task introduces:**

**The existing contract this fix must NOT break.** `backoff_test.go` (verified) already pins the `+Inf` behaviour with
two cases at `attempt: 2000`:

- `"overflow with Max caps at Max"` → `Max`
- `"overflow without Max returns Initial"` → **`Initial`**

So the established semantics for "the computation blew up" are **`Max` if capped, otherwise fall back to `Initial`**.
The fix therefore **widens the guard's trigger** from `IsInf` to "`IsNaN` or `>= MaxInt64`" and leaves the two outcomes
alone. It must **not** introduce a `MaxInt64` saturation return — that would change a reachable, already-tested arm and
break `"overflow without Max returns Initial"`.

**Hot-path branches this task introduces:**

| # | Branch | Covering case |
|---|--------|---------------|
| 1 | finite `d` >= `MaxInt64`, `Max > 0` → returns `Max` | `astronomic-but-finite growth is capped at Max` |
| 2 | finite `d` >= `MaxInt64`, `Max <= 0` → returns `Initial`, never negative | `astronomic-but-finite growth uncapped falls back to Initial` |
| 3 | the existing `+Inf` / `NaN` arms | already covered by the two `attempt: 2000` cases — re-run, do not duplicate |

- [ ] **Step 1: Write the failing test**

`backoff_test.go`'s table is `TestExponentialBackoff_Delay` with fields
`{name string; backoff msgin.ExponentialBackoff; attempt int; assert func(t *testing.T, d time.Duration)}`, written in
positional-literal style (**verified** — `backoff_test.go:14-19`). Extend that **existing** table; do not add a
parallel one. Note the file does **not** currently import `math` — add it only if a case needs it.

The distinguishing input is an attempt index large enough to exceed `MaxInt64` but **small enough to stay finite**.
`attempt: 2000` overflows `float64` to `+Inf` and hits the already-covered arm; `attempt: 100` does not
(`1s × 2^100 ≈ 1.27e39`, finite, and ~1.4e20 times `MaxInt64`).

```go
		{"astronomic-but-finite growth is capped at Max", base, 100, func(t *testing.T, d time.Duration) {
			// 100ms*2^100 ~ 1.27e38ns: finite, far above MaxInt64, so the old
			// IsInf-only guard let it convert to MinInt64 and slip past the cap.
			assert.Equal(t, time.Second, d)
		}},
		{"astronomic-but-finite growth uncapped falls back to Initial",
			msgin.ExponentialBackoff{Initial: time.Second, Max: 0, Mult: 2}, 100,
			func(t *testing.T, d time.Duration) {
				assert.Positive(t, d, "an out-of-range float->int conversion must not yield a negative delay")
				assert.Equal(t, time.Second, d)
			}},
```

`base` is the table's existing shared fixture (`Initial: 100ms, Max: 1s, Mult: 2`) — reuse it, matching the
surrounding cases' positional style.

- [ ] **Step 2: Run — verify it fails**

```bash
GOTOOLCHAIN=go1.25.12 go test -run 'TestExponentialBackoff' . 2>&1 | head -30
```

Expected: FAIL, with the uncapped case reporting a large **negative** duration (on amd64,
`-9223372036854775808ns`). **Record the observed value in the task report** — it is the proof the bug was real.

- [ ] **Step 3: Fix `backoff.go`**

Replace the `IsInf`/`IsNaN` guard and the conversion with a single saturating clamp:

The existing guard is

```go
	if math.IsInf(d, 1) || math.IsNaN(d) {
		if b.Max > 0 {
			return b.Max
		}
		return b.Initial
	}
```

Change **only its condition**, leaving both return arms exactly as they are:

```go
	// Widen the overflow guard from +Inf to "anything time.Duration cannot hold".
	// math.Pow reaches +Inf only past ~1.8e308, but time.Duration overflows at
	// 9.2e18 — every value in between is FINITE, so the old IsInf-only test let
	// it through, and Go leaves an out-of-range float->int conversion
	// implementation-defined (MinInt64 on amd64). The resulting negative duration
	// then slipped past the "out > b.Max" cap below, because a negative value is
	// not greater than Max. Both outcomes below are unchanged: Max when capped,
	// Initial otherwise.
	if math.IsNaN(d) || d >= float64(math.MaxInt64) {
		if b.Max > 0 {
			return b.Max
		}
		return b.Initial
	}
```

`d >= float64(math.MaxInt64)` subsumes `math.IsInf(d, 1)`, so the two existing `attempt: 2000` cases keep passing
through the same arms they always did. `d` cannot be negative here — `b.Initial <= 0` already returned and `mult` is
forced positive — so no lower-bound test is needed. **Do not** change the return values; `"overflow without Max returns
Initial"` is an existing passing test and this is a bug fix, not a semantic change.

**`jitter` has the IDENTICAL unchecked conversion — fix it in the same task or the class is not closed.**
`backoff.go:62-70` computes `j := lo + rand.Float64()*(2*delta)` and returns `time.Duration(j)` with no range guard.
With an uncapped policy (`Max <= 0`), `RandomizationFactor: 0.5` and `attempt: 33`, `j` reaches ~1.29e19 — **measured**
on this tree — above `MaxInt64`. The post-jitter re-clamp is `if b.Max > 0 && out > b.Max`, which cannot catch a
negative and does not run at all when `Max <= 0`. Add the guard inside `jitter`:

```go
	j := lo + rand.Float64()*(2*delta)
	if j < 0 {
		j = 0
	}
	// Same out-of-range conversion hazard as Delay: jitter can push a large
	// uncapped backoff above MaxInt64, and the caller's Max re-clamp cannot
	// catch the resulting negative.
	if j >= float64(math.MaxInt64) {
		return time.Duration(math.MaxInt64)
	}
	return time.Duration(j)
```

Cover it with a case that **repeats the draw**, because a single draw does not reliably reach the new arm:

```go
		{"jitter cannot push an uncapped backoff negative",
			msgin.ExponentialBackoff{Initial: time.Second, Max: 0, Mult: 2, RandomizationFactor: 0.5}, 33,
			func(t *testing.T, _ time.Duration) {
				// jitter samples uniformly in [d/2, 3d/2]; at attempt 33 the
				// centre is ~8.6e18ns, so only the upper ~28% of the range
				// exceeds MaxInt64 and reaches the new guard. ONE draw would hit
				// it ~28% of the time, so coverage on this arm would flap
				// run-to-run. 200 draws make a total miss ~(0.72)^200 ~ 1e-28,
				// and every draw independently pins the property that matters.
				b := msgin.ExponentialBackoff{Initial: time.Second, Max: 0, Mult: 2, RandomizationFactor: 0.5}
				for i := range 200 {
					d := b.Delay(33)
					assert.Positive(t, d,
						"draw %d: jitter must never produce a negative delay", i)
				}
			}},
```

The case's own `d` parameter is ignored — the table calls `Delay` once, and this closure re-draws inside itself,
which is what makes the arm reliably reachable. Keep the positional style of the surrounding cases.

> **PLATFORM DEPENDENCE — this determines whether the TDD red step actually goes red. Measured, not assumed.**
> An out-of-range float→int conversion is implementation-defined in Go. On **amd64** (`CVTTSD2SI`) it yields
> `MinInt64`; on **arm64** (`FCVTZS`) it **saturates** to `MaxInt64`. Verified on this machine (`darwin/arm64`):
> `time.Duration(1e8 * 2^100)` returns `2562047h47m16.854775807s`, i.e. `MaxInt64` — **positive**.
>
> Consequences for Step 2's red run, per case:
> - **`"astronomic-but-finite growth uncapped falls back to Initial"` is RED on both architectures.** Pre-fix it
>   returns `MaxInt64` on arm64 and `MinInt64` on amd64; the case asserts `Initial` (1s), so it fails either way. This
>   is the case that proves the fix.
> - **`"astronomic-but-finite growth is capped at Max"` is RED only on amd64.** On arm64 the saturated positive value
>   is still `> b.Max`, so the existing cap clamps it and the case passes pre-fix.
>
> **On arm64, expect ONE failure, not two.** That is correct, not drift. Record which architecture you ran on and the
> observed pre-fix value in the task report. The underlying negative-duration bug is **amd64-only**, which is to say it
> is a bug on CI and on essentially every Linux server, and invisible on an Apple-silicon dev machine — worth stating
> in the commit message.

- [ ] **Step 4: Run — verify it passes, and that nothing else moved**

```bash
GOTOOLCHAIN=go1.25.12 go test -race . && \
GOTOOLCHAIN=go1.25.12 go test -coverprofile=/tmp/cov23.out . >/dev/null && \
GOTOOLCHAIN=go1.25.12 go tool cover -func=/tmp/cov23.out | grep -E 'backoff.go|total:'
```

Expected: `ok`; every `backoff.go` function at **100.0%**. The consumer's existing backoff tests must still pass
untouched — this is a fix to an unreachable-by-design arm, not a semantic change to any reachable one.

> **`jitter`'s `j >= MaxInt64` arm is covered PROBABILISTICALLY, by repetition — this is deliberate, and is the one
> place in this increment where a branch is not reached by construction.** `jitter` samples uniformly in
> `[d/2, 3d/2]`; at `attempt: 33` only the top slice of that range clears `MaxInt64`, so a **single** draw reaches the
> guard roughly a quarter of the time. That is why the case loops 200 draws: with a per-draw miss probability of
> ~0.72, a run that never reaches the arm has probability ~`0.72^200` ≈ **1e-28**, i.e. it will not happen. Every draw
> also independently asserts the property that actually matters (`Delay` is never negative), so the loop is not just
> coverage padding.
>
> **If this arm ever shows as uncovered, or a draw ever fails, that is a REAL finding — investigate it.** An
> implementer must **not** "fix" a red gate here by weakening the assertion, dropping the loop, shrinking the
> iteration count, or `nolint`-ing the arm. The only legitimate responses are: fix `jitter`, or — if the sampling
> range genuinely changed — re-derive the attempt index and the iteration count and record the new arithmetic here.

- [ ] **Step 5: Commit**

```bash
git add backoff.go backoff_test.go
git commit -m "$(cat <<'EOF'
fix(core): stop ExponentialBackoff.Delay returning a negative duration

Delay guarded +Inf and NaN but not the finite-yet-out-of-range case: with
Initial=1s and Mult=2, attempt 100 computes ~1.27e39, which is finite, far above
MaxInt64, and converts to MinInt64 on amd64. The negative result then slipped
past the "out > b.Max" cap, because a negative value is not greater than Max, so
the documented min(Max, Initial x Mult^attempt) contract did not hold.

Reachable today through the Consumer's Nack redelivery delay. Clamping before
the conversion returns Max when capped, or falls back to Initial when uncapped,
leaving both existing return arms intact; jitter, which had the identical
unguarded conversion, saturates to MaxInt64.

The sign is platform-dependent: amd64 yields MinInt64, arm64 saturates positive.
So the bug is a NEGATIVE duration on amd64 (CI, and every Linux server) and an
absurdly LARGE one on arm64 dev machines. Both break the documented
min(Max, Initial x Mult^attempt) contract.

Plan: 023
EOF
)"
```

---

## Task 3: `BytesPayloadCodec` — stop the default JSON codec base64-ing `[]byte` payloads

**Verified defect, not a hypothetical.** `resolveCodec` (`producer.go`) defaults **every** wire adapter to
`JSONPayloadCodec`, and `json.Marshal([]byte("payload"))` is `"cGF5bG9hZA=="` — a quoted base64 string.
`JSONPayloadCodec` is the only codec in the repo. So `NewProducer[[]byte](someWireAdapter)` — the composition
Plan 024's webhook adapter makes the flagship example — silently sends base64-in-quotes rather than the caller's bytes.

**Files:**
- Modify: `codec.go` — add `BytesPayloadCodec`
- Test: `codec_test.go` (append)

**Interfaces:**
- **Produces:** `type BytesPayloadCodec struct{}` implementing `PayloadCodec[[]byte]`.
- Plan 024 consumes it: `NewOutbound`/`NewExchange` godoc and **every** example must pair it explicitly.

> **Decision (settled with the user): an explicit codec, NOT a special case in `resolveCodec`.** Making
> `resolveCodec` sniff `T == []byte` and swap the default would change behaviour for existing SQL/Redis/NATS callers
> whose persisted rows are base64 today — a silent wire-format break on adapters this increment does not touch.
> `BytesPayloadCodec` is purely additive; the pairing is the caller's explicit choice.

**Hot-path branches this task introduces:**

| # | Branch | Covering case |
|---|--------|---------------|
| 1 | `Encode` round-trips bytes verbatim (no base64, no quotes) | `encode is identity, not JSON` |
| 2 | `Decode` round-trips bytes verbatim | `decode is identity` |
| 3 | `Encode(nil)` / `Decode(nil)` | `nil payload round-trips` |
| 4 | the returned slice does not alias the input | `encode does not alias the caller's slice` |

- [ ] **Step 1: Write the failing test**

Append to `codec_test.go`, extending its existing table if the shape fits; otherwise add a sibling table.

```go
func TestBytesPayloadCodec(t *testing.T) {
	t.Parallel()

	var codec msgin.BytesPayloadCodec

	tests := []struct {
		name   string
		assert func(t *testing.T)
	}{
		{
			name: "encode is identity, not JSON",
			assert: func(t *testing.T) {
				t.Helper()
				got, err := codec.Encode([]byte("payload"))
				require.NoError(t, err)
				assert.Equal(t, []byte("payload"), got)
				// The bug this codec exists to prevent.
				assert.NotEqual(t, []byte(`"cGF5bG9hZA=="`), got)
			},
		},
		{
			name: "decode is identity",
			assert: func(t *testing.T) {
				t.Helper()
				got, err := codec.Decode([]byte("payload"))
				require.NoError(t, err)
				assert.Equal(t, []byte("payload"), got)
			},
		},
		{
			name: "nil round-trips both ways",
			assert: func(t *testing.T) {
				t.Helper()
				enc, err := codec.Encode(nil)
				require.NoError(t, err)
				assert.Empty(t, enc)
				dec, err := codec.Decode(nil)
				require.NoError(t, err)
				assert.Empty(t, dec)
			},
		},
		{
			name: "encode does not alias the caller's slice",
			assert: func(t *testing.T) {
				t.Helper()
				in := []byte("payload")
				got, err := codec.Encode(in)
				require.NoError(t, err)
				in[0] = 'X'
				assert.Equal(t, []byte("payload"), got,
					"mutating the caller's slice after Encode must not change the encoded bytes")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tt.assert(t)
		})
	}
}
```

- [ ] **Step 2: Run — verify it fails** (`undefined: msgin.BytesPayloadCodec`).

- [ ] **Step 3: Implement**

In `codec.go`:

```go
// BytesPayloadCodec is the identity PayloadCodec for []byte payloads: it passes
// the bytes through unchanged in both directions.
//
// Pair it EXPLICITLY whenever T is []byte and the adapter is a wire adapter:
//
//	msgin.NewProducer[[]byte](out, msgin.WithProducerCodec[[]byte](msgin.BytesPayloadCodec{}))
//
// Without it, resolveCodec defaults to JSONPayloadCodec, and json.Marshal of a
// []byte is a quoted base64 STRING — so a raw body would go on the wire as
// "cGF5bG9hZA==" rather than payload. That is almost never what a caller sending
// raw bytes intends.
//
// It is deliberately NOT the automatic default for T == []byte: adapters that
// already persist base64-encoded envelopes (database/sql, redis, nats) would
// silently change their on-wire format. The pairing is the caller's explicit
// choice.
//
// Both methods COPY, so neither the caller's slice nor the returned one can be
// mutated through the other. Messages are immutable by contract, and a message
// may be shared across a pub-sub channel; aliasing would break that.
//
// TWO RESIDUALS, both consequences of the pass-through being exact:
//
//   - It removes an ACCIDENTAL escaping layer. JSONPayloadCodec's quoting and
//     escaping neutralised some hostile bytes as a side effect — never as a
//     security control, but in practice. This codec emits the caller's bytes
//     verbatim, so sanitising or validating the payload for whatever the sink
//     interprets it as (SQL, a shell, HTML, a log) is wholly the caller's and the
//     adapter's responsibility.
//   - Encode(nil) returns nil, where JSONPayloadCodec returned the four bytes
//     null. A store with a NOT NULL payload column that accepted a nil payload
//     before will now reject it. Encode an explicit empty or sentinel payload if
//     the sink cannot take a NULL.
type BytesPayloadCodec struct{}

// Encode returns a copy of v.
func (BytesPayloadCodec) Encode(v []byte) ([]byte, error) {
	return bytes.Clone(v), nil
}

// Decode returns a copy of b.
func (BytesPayloadCodec) Decode(b []byte) ([]byte, error) {
	return bytes.Clone(b), nil
}
```

Add `"bytes"` to `codec.go`'s imports. `bytes.Clone(nil)` returns `nil`, which is why the nil case asserts `Empty`
rather than `Equal([]byte{})`.

- [ ] **Step 4: Run — verify it passes, and coverage**

```bash
GOTOOLCHAIN=go1.25.12 go test -race . && \
GOTOOLCHAIN=go1.25.12 go test -coverprofile=/tmp/cov23.out . >/dev/null && \
GOTOOLCHAIN=go1.25.12 go tool cover -func=/tmp/cov23.out | grep -E 'codec.go'
```

Expected: every `codec.go` function at **100.0%**.

- [ ] **Step 5: Commit**

```bash
git add codec.go codec_test.go
git commit -m "$(cat <<'EOF'
feat(core): add BytesPayloadCodec, the identity codec for []byte payloads

resolveCodec defaults every wire adapter to JSONPayloadCodec, and json.Marshal
of a []byte is a quoted base64 string, so NewProducer[[]byte] over a wire
adapter silently sent "cGF5bG9hZA==" instead of the caller's bytes. The design
audit flagged this against the composition the HTTP webhook adapter will make
its flagship example.

Additive and explicitly paired, deliberately not an automatic default for
T == []byte: the sql/redis/nats adapters persist base64 envelopes today and must
not change format. Both methods copy, so the codec cannot alias a caller slice
into an immutable message.

Plan: 023
EOF
)"
```

---

## Task 4: `WithProducerRetry` — the retry loop, its bounds, and its observability

The substantive task. **Read the "Design deltas" section above before starting** — D1–D6 all land here.

**Files:**
- Modify: `producer.go` (config fields, **six** options, `NewProducer` validation, the `Send` retry path + helpers)
- Test: `producer_retry_test.go` (create)

**Interfaces:**
- **Consumes:** Task 1's `RetryAfter`/`retryAfterOf`/the six sentinels; the pre-existing `isPermanent`, `RetryPolicy`
  (`MaxAttempts`, `Backoff`, `DeadLetter`, `Validate()`, `delayFor(attempt int)`), `Hooks`, and
  `producerConfig[T].clock`.
- **Produces:** `WithProducerRetry[T]`, `WithProducerRetryAfterCap[T]`, `WithProducerRetryBudget[T]`,
  `WithProducerDeadLetterTimeout[T]`, `WithProducerHooks[T]`, `WithProducerLogger[T]`, and the classification
  contract Plan 024's adapter must satisfy.

**Hot-path branches this task introduces (every one needs a case):**

| # | Branch | Covering case |
|---|--------|---------------|
| 1 | no `WithProducerRetry` → exactly one passthrough attempt | `unset policy sends exactly once` |
| 2 | success on the first attempt | `first attempt succeeds` |
| 3 | transient → wait → success | `transient then success` |
| 4 | `isPermanent` short-circuit (explicit `Permanent` marker) | `permanent marker is not retried` |
| 5 | `isPermanent` via `ErrPayloadTooLarge` | `sentinel-permanent is not retried` |
| 6 | **`Permanent` beats `RetryAfter`** — both nesting orders | `TestProducerPermanentBeatsRetryAfter` (D6) |
| 7 | exhaustion → dead-letter, cause + `ErrDeadLettered` returned | `exhaustion dead-letters` |
| 8 | dead-letter `Send` returns an error → joined | `dead-letter error is joined` |
| 9 | dead-letter `Send` panics → recovered, joined | `dead-letter panic is joined` |
| 10 | **dead-letter runs on a detached ctx** (D1) | `dead-letter succeeds after ctx cancel` |
| 11 | `ctx` cancelled while parked on the backoff timer | `cancel during backoff` |
| 12 | `ctx` already cancelled → no further attempt | `pre-cancelled ctx stops after one attempt` |
| 13 | `RetryAfter` **lengthens** the computed backoff | `retry-after floors the wait` |
| 14 | `RetryAfter` clamped by an explicit cap | `retry-after is clamped by an explicit cap` |
| 15 | `RetryAfter` clamped by the default cap | `retry-after is clamped by the default cap` |
| 16 | **`RetryAfter` shorter than the computed backoff does NOT shorten it** (D3) | `retry-after cannot shorten the backoff` |
| 17 | no marker → `policy.delayFor(attempt)` (exercises `retryAfterOf`'s false arm) | `no marker takes the computed backoff` |
| 18 | computed delay `<= 0` floored to `minRetryDelay` (D2) | `a zero computed backoff is floored` |
| 19 | **budget exhausted → stop retrying** (D2) | `the retry budget bounds an infinite policy` |
| 20 | budget exhausted **with** a DeadLetter → dead-letters | `budget exhaustion dead-letters` |
| 21 | `OnRetry` hook fires per retry (D5) | `hooks observe retries and the divert` |
| 22 | `OnDeadLetter` hook fires once on divert (D5) | same |
| 23 | a **panicking hook** does not break the loop | `a panicking hook is contained` |
| 24 | `box` error returned before any attempt | `encode failure never reaches the adapter` |
| 25 | `NewProducer`: `MaxAttempts > 0` without `DeadLetter` → `ErrNoDeadLetter` | construction table |
| 26 | `NewProducer`: `MaxAttempts < 0` → `ErrInvalidMaxAttempts` | construction table |
| 27 | `NewProducer`: `RetryPolicy{}` (unbounded both ways) → `ErrUnboundedRetry` (D2) | construction table |
| 28 | `NewProducer`: explicit cap `<= 0` → `ErrInvalidRetryAfterCap` | construction table |
| 29 | `NewProducer`: explicit budget `<= 0` → `ErrInvalidRetryBudget` | construction table |
| 30 | unset cap/budget take their defaults and construct | construction table |
| 31 | `SendAfter`/`SendAt` bypass retry entirely | `scheduled send is not retried` |
| 32 | `NewProducer`: explicit dead-letter timeout `== 0` → `ErrInvalidDeadLetterTimeout` | construction table |
| 33 | `NewProducer`: explicit dead-letter timeout `< 0` → `ErrInvalidDeadLetterTimeout` | construction table |
| 34 | `NewProducer`: explicit positive dead-letter timeout constructs | construction table |
| 35 | `WithProducerLogger(nil)` keeps the default logger and constructs | construction table |
| 36 | `WithProducerLogger(l)` with a non-nil `l` installs it and constructs | construction table |
| 37 | the dead-letter timeout actually bounds the divert — an expired `dlCtx` surfaces as a joined error | `dead-letter timeout expiry is joined` |

> **Branches 32–36 exist because the round-2 audit found `WithProducerDeadLetterTimeout` and `WithProducerLogger` — the
> two options the round-1 fixes ADDED — exercised by nothing at all.** Every other option had a validation case; these
> two were introduced in prose and never reached a table. Both their reject arms, both their default/accept arms, and
> the non-nil logger arm are therefore explicit rows, not assumed.

- [ ] **Step 1: Write the failing test — harness + branches 1–3**

Create `producer_retry_test.go` (`package msgin_test`).

**Before writing:** run `grep -n 'type recordingSink' settlement_doubles_test.go` — a `recordingSink` **already exists**
in the `msgin_test` package (`settlement_doubles_test.go:33`) and must **not** be redeclared. There is **no** existing
func-adapter for `OutboundAdapter` in `msgin_test` (verified), so `outboundFunc` below is new and safe to declare.

```go
package msgin_test

import (
	"bytes" // the panicking-hook test asserts on the logger's output
	"context"
	"errors"
	"io"       // the WithProducerLogger construction cases
	"log/slog" // ditto, and the hook-panic logger in TestProducerHooks
	"sync"
	"testing"
	"time"

	"github.com/jonboulle/clockwork"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"

	"github.com/kartaladev/msgin"
)

// outboundFunc adapts a func to msgin.OutboundAdapter.
type outboundFunc func(context.Context, msgin.Message[any]) error

func (f outboundFunc) Send(ctx context.Context, msg msgin.Message[any]) error { return f(ctx, msg) }

// scriptedOutbound returns a scripted error per attempt and records every
// message it was handed. Safe for concurrent use: Send runs on the goroutine
// under test while a driver advances the clock from another.
type scriptedOutbound struct {
	mu      sync.Mutex
	script  []error // script[i] is attempt i+1's result; past the end the last entry repeats
	calls   int
	ctxs    []error // ctxs[i] is ctx.Err() as seen by attempt i+1
	gotMsgs []msgin.Message[any]
}

func newScriptedOutbound(script ...error) *scriptedOutbound {
	return &scriptedOutbound{script: script}
}

func (o *scriptedOutbound) Send(ctx context.Context, msg msgin.Message[any]) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.calls++
	o.ctxs = append(o.ctxs, ctx.Err())
	o.gotMsgs = append(o.gotMsgs, msg)
	switch {
	case len(o.script) == 0:
		return nil
	case o.calls <= len(o.script):
		return o.script[o.calls-1]
	default:
		return o.script[len(o.script)-1]
	}
}

func (o *scriptedOutbound) attempts() int {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.calls
}

func (o *scriptedOutbound) messages() []msgin.Message[any] {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]msgin.Message[any](nil), o.gotMsgs...)
}

// lastCtxErr reports ctx.Err() as observed by the most recent Send — how the
// detached-dead-letter branch (D1) is proven.
func (o *scriptedOutbound) lastCtxErr() error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if len(o.ctxs) == 0 {
		return nil
	}
	return o.ctxs[len(o.ctxs)-1]
}

// retryHarness runs p.Send on its own goroutine and lets the test step the fake
// clock deterministically. sendErr is read only after done is closed, so there
// is no race and no require/t.Fatal off the test goroutine.
type retryHarness struct {
	clock *clockwork.FakeClock
	done  chan struct{}
	err   error
}

func runSend(t *testing.T, p msgin.Producer[[]byte], clock *clockwork.FakeClock, ctx context.Context) *retryHarness {
	t.Helper()
	h := &retryHarness{clock: clock, done: make(chan struct{})}
	go func() {
		defer close(h.done)
		h.err = p.Send(ctx, msgin.New[[]byte]([]byte("payload")))
	}()
	return h
}

// stepTo advances the clock past a single expected wait in TWO phases, so the
// assertion can detect UNDER-waiting as well as over-waiting: after the first
// advance the producer must still be parked. (A one-shot Advance(want) followed
// by "did it return?" is true by construction — Plan 021 lesson.)
func (h *retryHarness) stepTo(t *testing.T, want time.Duration) {
	t.Helper()
	require.NoError(t, h.clock.BlockUntilContext(t.Context(), 1), "producer never parked on a timer")
	if want > 0 {
		h.clock.Advance(want - time.Nanosecond)
		select {
		case <-h.done:
			t.Fatalf("Send returned after %v, but the expected wait was %v — it under-waited", want-time.Nanosecond, want)
		case <-time.After(20 * time.Millisecond):
		}
		h.clock.Advance(time.Nanosecond)
	}
}

func (h *retryHarness) wait(t *testing.T) error {
	t.Helper()
	select {
	case <-h.done:
		return h.err
	case <-time.After(5 * time.Second):
		t.Fatal("Send did not return")
		return nil
	}
}
```

> **On the 20 ms negative wait in `stepTo`:** it is a *bounded* real sleep proving a negative (the producer has **not**
> returned), which a fake clock cannot express. It is the only real sleep in the test file and it never gates a
> positive assertion, so it cannot make the suite flaky in the failing direction — a slow machine makes it *more*
> reliable, not less.

Then the first table:

```go
func TestProducerRetry(t *testing.T) {
	defer goleak.VerifyNone(t)

	transient := errors.New("transient")

	tests := []struct {
		name   string
		script []error
		// waits are the delays the producer is EXPECTED to park for, in order.
		waits  []time.Duration
		policy func(dlq msgin.OutboundAdapter) msgin.RetryPolicy
		assert func(t *testing.T, out, dlq *scriptedOutbound, err error)
	}{
		{
			name:   "unset policy sends exactly once",
			script: []error{transient},
			policy: nil, // no WithProducerRetry at all
			assert: func(t *testing.T, out, dlq *scriptedOutbound, err error) {
				t.Helper()
				assert.ErrorIs(t, err, transient)
				assert.NotErrorIs(t, err, msgin.ErrDeadLettered)
				assert.Equal(t, 1, out.attempts(), "no policy must mean no retry")
				assert.Equal(t, 0, dlq.attempts())
			},
		},
		{
			name:   "first attempt succeeds",
			script: []error{nil},
			policy: func(dlq msgin.OutboundAdapter) msgin.RetryPolicy {
				return msgin.RetryPolicy{MaxAttempts: 3, DeadLetter: dlq}
			},
			assert: func(t *testing.T, out, dlq *scriptedOutbound, err error) {
				t.Helper()
				require.NoError(t, err)
				assert.Equal(t, 1, out.attempts())
				assert.Equal(t, 0, dlq.attempts())
			},
		},
		{
			name:   "transient then success",
			script: []error{transient, nil},
			waits:  []time.Duration{time.Second},
			policy: func(dlq msgin.OutboundAdapter) msgin.RetryPolicy {
				return msgin.RetryPolicy{
					MaxAttempts: 3,
					Backoff:     msgin.ExponentialBackoff{Initial: time.Second, Mult: 2},
					DeadLetter:  dlq,
				}
			},
			assert: func(t *testing.T, out, dlq *scriptedOutbound, err error) {
				t.Helper()
				require.NoError(t, err)
				assert.Equal(t, 2, out.attempts())
				assert.Equal(t, 0, dlq.attempts())
			},
		},
		{
			name:   "exhaustion dead-letters and returns the cause",
			script: []error{transient},
			waits:  []time.Duration{time.Second, 2 * time.Second},
			policy: func(dlq msgin.OutboundAdapter) msgin.RetryPolicy {
				return msgin.RetryPolicy{
					MaxAttempts: 3,
					Backoff:     msgin.ExponentialBackoff{Initial: time.Second, Mult: 2},
					DeadLetter:  dlq,
				}
			},
			assert: func(t *testing.T, out, dlq *scriptedOutbound, err error) {
				t.Helper()
				assert.ErrorIs(t, err, transient, "a successful divert must still surface the cause")
				assert.ErrorIs(t, err, msgin.ErrDeadLettered, "the caller must be able to tell DLQ from outright failure")
				assert.Equal(t, 3, out.attempts())
				require.Equal(t, 1, dlq.attempts())
				got := dlq.messages()
				require.Len(t, got, 1)
				assert.Equal(t, []byte("payload"), got[0].Payload())
			},
		},
		{
			name:   "permanent marker is not retried",
			script: []error{msgin.Permanent(transient)},
			policy: func(dlq msgin.OutboundAdapter) msgin.RetryPolicy {
				return msgin.RetryPolicy{MaxAttempts: 5, DeadLetter: dlq}
			},
			assert: func(t *testing.T, out, dlq *scriptedOutbound, err error) {
				t.Helper()
				assert.ErrorIs(t, err, transient)
				assert.NotErrorIs(t, err, msgin.ErrDeadLettered)
				assert.Equal(t, 1, out.attempts(), "permanent must consume no attempt budget")
				assert.Equal(t, 0, dlq.attempts(), "permanent must never dead-letter")
			},
		},
		{
			name:   "sentinel-permanent is not retried",
			script: []error{msgin.ErrPayloadTooLarge},
			policy: func(dlq msgin.OutboundAdapter) msgin.RetryPolicy {
				return msgin.RetryPolicy{MaxAttempts: 5, DeadLetter: dlq}
			},
			assert: func(t *testing.T, out, dlq *scriptedOutbound, err error) {
				t.Helper()
				assert.ErrorIs(t, err, msgin.ErrPayloadTooLarge)
				assert.Equal(t, 1, out.attempts())
				assert.Equal(t, 0, dlq.attempts())
			},
		},
		{
			name:   "a zero computed backoff is floored, not spun",
			script: []error{transient, nil},
			waits:  []time.Duration{100 * time.Millisecond}, // minRetryDelay
			policy: func(dlq msgin.OutboundAdapter) msgin.RetryPolicy {
				// Backoff present but always zero: Initial <= 0 makes Delay return 0.
				return msgin.RetryPolicy{
					MaxAttempts: 3,
					Backoff:     msgin.ExponentialBackoff{},
					DeadLetter:  dlq,
				}
			},
			assert: func(t *testing.T, out, dlq *scriptedOutbound, err error) {
				t.Helper()
				require.NoError(t, err)
				assert.Equal(t, 2, out.attempts())
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clock := clockwork.NewFakeClock()
			out := newScriptedOutbound(tt.script...)
			dlq := newScriptedOutbound(nil)

			opts := []msgin.ProducerOption[[]byte]{
				msgin.WithProducerClock[[]byte](clock),
				// REQUIRED, not decorative. scriptedOutbound is not a
				// LiveValueSource, so resolveCodec would install
				// JSONPayloadCodec[[]byte] and json.Marshal([]byte("payload"))
				// is []byte(`"cGF5bG9hZA=="`) — the exact Task 3 bug. Without
				// this pairing the DLQ payload assertion below cannot pass.
				msgin.WithProducerCodec[[]byte](msgin.BytesPayloadCodec{}),
			}
			if tt.policy != nil {
				opts = append(opts, msgin.WithProducerRetry[[]byte](tt.policy(dlq)))
			}

			p, err := msgin.NewProducer[[]byte](out, opts...)
			require.NoError(t, err)

			h := runSend(t, p, clock, t.Context())
			for _, w := range tt.waits {
				h.stepTo(t, w)
			}
			tt.assert(t, out, dlq, h.wait(t))
		})
	}
}
```

- [ ] **Step 2: Run — verify it fails** (`undefined: msgin.WithProducerRetry`).

- [ ] **Step 3: Implement `producer.go`**

Add `"errors"` to the imports, then:

**(a)** Constants, after `producerConfig`:

```go
const (
	// defaultRetryAfterCap bounds the server-instructed delay a producer honours
	// from a RetryAfter marker when WithProducerRetryAfterCap is unset. 60s is
	// at the top of the plausible legitimate range — HTTP rate-limit windows are
	// typically <= 60s — and a hostile or misconfigured "Retry-After: 86400" is
	// clamped to it. Without a clamp, a deadline-less ctx (context.Background()
	// is common on a Send) would let a remote endpoint park the caller's
	// goroutine for as long as it likes.
	//
	// The default deliberately does NOT cover the longest legitimate case (a
	// maintenance 503 asking for 120s): a caller who needs that raises the cap
	// explicitly. Defaulting ABOVE the worst legitimate value would optimise for
	// obeying a remote-controlled instruction over bounding the caller, which is
	// the wrong side of CLAUDE.md's safe-defaults gate.
	defaultRetryAfterCap = 60 * time.Second

	// defaultRetryBudget bounds the CUMULATIVE wall-clock a Send may spend
	// retrying when WithProducerRetryBudget is unset. Two minutes sits above
	// defaultRetryAfterCap — a budget below the cap would silently defeat the
	// Retry-After compliance the cap exists to allow — while keeping "retry
	// forever" finite by default.
	//
	// It is ALSO chosen to sit well below the shortest plausible upstream lease:
	// adapter/database/sql defaults leaseTTL to 5 minutes (options.go:17). A Send
	// blocking longer than the lease that covers the message being handled lets
	// the source reclaim and redeliver it while the first attempt is still
	// running, turning one logical message into duplicate outbound calls that fan
	// out across instances.
	//
	// That budget < lease relation holds for the DEFAULTS as shipped; it is not a
	// guarantee. The budget bounds retry waits only, so a single blocking adapter
	// call can push a Send past any lease regardless (see
	// WithProducerRetryBudget), and a caller may raise the budget or shorten the
	// lease. Where the relation matters, bound the adapter's I/O and size the two
	// against each other explicitly.
	defaultRetryBudget = 2 * time.Minute

	// defaultDeadLetterTimeout bounds the divert. The DeadLetter send runs on a
	// ctx detached from the caller's, so nothing else would stop a hung sink
	// (blackholed TCP, wedged DB) from blocking the caller's goroutine FOREVER —
	// immune to their own cancel or deadline, which would be a strict regression
	// against the un-retried passthrough and a violation of CLAUDE.md's
	// everything-cancellable and graceful-shutdown gates.
	defaultDeadLetterTimeout = 30 * time.Second

	// minRetryDelay floors every computed retry wait. A policy whose Backoff
	// yields 0 (an ExponentialBackoff with a non-positive Initial, say) would
	// otherwise re-attempt with no delay at all, hammering the target from the
	// caller's goroutine for as long as the budget allows. 100ms is below any
	// meaningful backoff yet high enough that a degenerate policy makes hundreds,
	// not hundreds of thousands, of attempts before the budget ends it.
	//
	// This floor is deliberately NOT configurable. CLAUDE.md's "never hard-code a
	// policy the caller cannot change" governs behaviour a caller might
	// legitimately need to tune; a caller wanting a LONGER wait sets Backoff, and
	// there is no legitimate reason to want a SHORTER one than this on a loop
	// that holds the caller's own goroutine.
	minRetryDelay = 100 * time.Millisecond
)
```

**(b)** Extend `producerConfig[T]`:

```go
	retry                RetryPolicy
	retrySet             bool
	retryAfterCap        time.Duration
	retryAfterCapSet     bool
	retryBudget          time.Duration
	retryBudgetSet       bool
	deadLetterTimeout    time.Duration
	deadLetterTimeoutSet bool
	hooks                Hooks
	logger               *slog.Logger
```

and default `logger` in `NewProducer`'s config literal exactly as `NewConsumer` does (`consumer.go:147`):

```go
	cfg := producerConfig[T]{
		clock:  clockwork.NewRealClock(),
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
```

`producer[T]` gains `retry`, `retrySet`, `retryAfterCap`, `retryBudget`, `deadLetterTimeout`, `hooks`, `logger`.
Add `"errors"`, `"io"` and `"log/slog"` to `producer.go`'s imports.

**(c)** The options, after `WithProducerClock` (five here; `WithProducerLogger` is the sixth, in the blockquote
following **(e)**):

```go
// WithProducerRetry applies p to Producer.Send: a failing OutboundAdapter.Send is
// retried on the producer's injected clock until it succeeds, the policy's
// MaxAttempts are spent, the retry budget is exhausted, or ctx is cancelled.
// Default: unset — Send makes exactly ONE attempt and returns the adapter's
// error unchanged, so this option is purely additive.
//
// Classification is the adapter's job and the runtime's decision (ADR 0002):
//
//   - Permanent(err), ErrPayloadType, ErrPayloadDecode, ErrPayloadTooLarge —
//     returned immediately, consuming NO attempt budget and NEVER dead-lettered.
//     A permanent outbound failure is the caller's to see, not something to bury
//     in a DLQ after N pointless attempts. Permanent WINS over RetryAfter.
//   - RetryAfter(err, d) — waits at least d, clamped by
//     WithProducerRetryAfterCap. d is a FLOOR on the computed backoff, never a
//     replacement for it, so a server cannot shorten the client's own backoff.
//   - anything else — waits p.Backoff.Delay(attempt-1), floored to 100ms.
//
// On attempt exhaustion, budget exhaustion, OR cancellation during a backoff
// wait, the message is routed to p.DeadLetter and the returned error carries
// BOTH the causing error and ErrDeadLettered, so
// errors.Is(err, ErrDeadLettered) distinguishes "safely in the DLQ" from "failed
// outright". The divert runs on a ctx detached from the caller's
// (context.WithoutCancel), because the usual reason the loop is ending is that
// ctx was cancelled — diverting on it would mean the message reached neither its
// target nor the DLQ.
//
// TRADE — diverting on cancellation is not free. Because a cancel during backoff
// is terminal for the message, cancelling a Send does NOT return promptly: it
// returns only once the detached divert finishes, so a caller waits up to
// WithProducerDeadLetterTimeout (30s default) per in-flight Send, and a mass
// shutdown dumps EVERY in-flight message into the DLQ rather than dropping it.
// That is the deliberate choice — a message in the DLQ is recoverable, a lost one
// is not — but size WithProducerDeadLetterTimeout against your shutdown deadline,
// and expect DLQ volume proportional to in-flight sends at shutdown.
//
// DELIVERY GUARANTEE — retrying CHANGES it. A retry after a timeout or a
// connection reset re-sends a request the peer may already have received and
// committed; the producer cannot tell "never arrived" from "arrived, reply lost".
// So WithProducerRetry converts Producer.Send from the underlying adapter's
// guarantee to AT-LEAST-ONCE, with duplicates visible to the receiver. The
// receiver must be idempotent: carry a stable idempotency key in a header (the
// message ID is one) and deduplicate on it, or make the operation naturally
// idempotent. Without WithProducerRetry, Send makes exactly one attempt and the
// adapter's own guarantee is unchanged.
//
// p is validated here by RetryPolicy.Validate, so a finite MaxAttempts without a
// DeadLetter fails at construction with ErrNoDeadLetter and a negative
// MaxAttempts with ErrInvalidMaxAttempts — never at send time. Additionally, a
// policy unbounded in BOTH dimensions (MaxAttempts == 0 with a nil Backoff — the
// RetryPolicy zero value) is rejected with ErrUnboundedRetry: on the producer
// that is a zero-delay infinite loop on the CALLER'S goroutine, unlike the
// Consumer, where the same policy means broker redelivery.
//
// SCOPE — this governs Producer.Send ONLY. It does NOT apply to:
//
//   - SendAfter/SendAt, whose delivery is the adapter's durable store rather
//     than a live call (v1 decision, Spec 013 §3.1);
//   - an outbound reached as a To(sink) step inside a flow. There the Consumer's
//     own RetryPolicy already retries by full message redelivery, so a second
//     loop here would MULTIPLY attempts (inner x outer) and re-run every prior
//     step's side effects. A flow with no inbound Consumer gets no retry at all;
//     use a Producer if you need one (ADR 0025 §2).
//
// The loop holds the CALLER'S goroutine for the whole retry sequence — inherent
// to a synchronous Send. A caller wanting fire-and-forget composes this with
// their own concurrency.
//
// TOPOLOGY — this retry is PER-PROCESS by construction. Across N horizontally
// scaled instances, each retries independently, so a throttling endpoint receives
// N times the load it asked to shed and Retry-After compliance is per-instance,
// not fleet-wide. Coordinating a fleet-wide budget needs shared state the core
// cannot assume; the seam for it is ADR 0006's rate-limit and circuit-breaker
// interfaces, which a distributed (Redis- or DB-backed) limiter plugs into
// without a core change.
func WithProducerRetry[T any](p RetryPolicy) ProducerOption[T] {
	return func(o *producerConfig[T]) { o.retry = p; o.retrySet = true }
}

// WithProducerRetryAfterCap clamps the delay honoured from a RetryAfter marker:
// the effective wait is max(computedBackoff, min(serverDelay, d)), always
// additionally bounded by ctx and by WithProducerRetryBudget. Default: 60 seconds
// — see defaultRetryAfterCap for why an unclamped server-supplied delay is a
// denial-of-service lever against the caller's own goroutine.
//
// A custom value is safe as long as it stays below what the caller is willing to
// block a Send for, and at or below the retry budget (a cap above the budget is
// legal but the budget wins). It has no effect without WithProducerRetry, and
// none on errors carrying no RetryAfter marker.
//
// d MUST be > 0: NewProducer returns ErrInvalidRetryAfterCap for an explicit
// d <= 0. Leaving the option unset is how a caller asks for the default.
func WithProducerRetryAfterCap[T any](d time.Duration) ProducerOption[T] {
	return func(o *producerConfig[T]) { o.retryAfterCap = d; o.retryAfterCapSet = true }
}

// WithProducerRetryBudget bounds the CUMULATIVE wall-clock, measured on the
// producer's injected clock, that one Send may spend retrying. Once the budget
// is spent the loop stops as if attempts were exhausted: the message is routed
// to the policy's DeadLetter (if any) and the causing error is returned with
// ErrDeadLettered joined.
//
// It exists because MaxAttempts == 0 means "retry forever", bounded otherwise
// only by ctx — and Producer.Send is routinely called with context.Background().
// The budget makes the safety property ADR 0025 claims actually true. Default:
// 2 minutes. A wait that would overrun the remaining budget ends the loop
// rather than being truncated, so the producer never makes an attempt it has no
// budget to back. The budget ALWAYS applies, including to a finite MaxAttempts;
// a stop caused by the budget rather than by spent attempts is marked with
// ErrRetryBudgetExhausted so the two are distinguishable.
//
// WHAT IT DOES NOT BOUND — the budget bounds the cumulative RETRY WAITS and the
// elapsed time BETWEEN attempts. It does NOT bound the duration of any single
// OutboundAdapter.Send: the loop checks the budget before parking, never during
// an adapter call, so one adapter call that blocks forever blocks Send forever.
// Worst-case wall clock for a Send is therefore
//
//	retryBudget + one unbounded adapter call + deadLetterTimeout
//
// Bound the adapter's own I/O yourself — pass a ctx with a deadline, or configure
// the client's timeouts — if you need a hard upper bound on Send.
//
// d MUST be > 0: NewProducer returns ErrInvalidRetryBudget for an explicit
// d <= 0. There is deliberately no "unlimited" value — an unbounded retry on a
// caller's goroutine is not a configuration this library offers.
func WithProducerRetryBudget[T any](d time.Duration) ProducerOption[T] {
	return func(o *producerConfig[T]) { o.retryBudget = d; o.retryBudgetSet = true }
}

// WithProducerDeadLetterTimeout bounds the dead-letter divert. The divert runs
// on a ctx DETACHED from the caller's — otherwise a cancelled caller could not
// be dead-lettered at all, which is the loss this design exists to prevent — so
// this timeout is the ONLY thing standing between a hung DeadLetter sink
// (blackholed TCP, wedged DB) and a caller goroutine blocked forever, immune to
// its own cancel. Default: 30 seconds. d MUST be > 0
// (ErrInvalidDeadLetterTimeout); there is deliberately no "unlimited" value.
//
// It is also what a CANCELLING caller waits for. A cancel during backoff diverts
// (see WithProducerRetry), and the divert is detached, so Send returns only after
// the sink answers or this timeout expires: a cancelled Send can take up to d to
// return, and a mass shutdown pays that per in-flight Send while dumping each
// in-flight message into the DLQ. Size d against your shutdown deadline, not just
// against the sink's latency.
func WithProducerDeadLetterTimeout[T any](d time.Duration) ProducerOption[T] {
	return func(o *producerConfig[T]) { o.deadLetterTimeout = d; o.deadLetterTimeoutSet = true }
}

// WithProducerHooks installs optional, nil-safe observability callbacks on the
// retry loop. Only OnRetry (fired before each wait, with the causing error) and
// OnDeadLetter (fired once, after a divert) are used by a Producer; the other
// Hooks fields belong to the Consumer's settlement path and are ignored here.
//
// Hooks run synchronously on the caller's goroutine, so a slow hook slows the
// send. A panicking hook is recovered and ignored — an observability callback
// must not be able to take down the flow (the fault-isolation constraint) — so
// do not use a hook for control flow.
func WithProducerHooks[T any](h Hooks) ProducerOption[T] {
	return func(o *producerConfig[T]) { o.hooks = h }
}
```

**(d)** Extend `producer[T]` with **all seven** new fields — `retry RetryPolicy`, `retrySet bool`,
`retryAfterCap time.Duration`, `retryBudget time.Duration`, **`deadLetterTimeout time.Duration`**, `hooks Hooks`,
**`logger *slog.Logger`** — and validate in `NewProducer` **after** the `resolveCodec` block, **before** the return:

> **This list was short by two, and that is exactly the round-2 critical repeating itself.** An earlier revision of
> this paragraph named only five fields, omitting `deadLetterTimeout` and `logger` — the same two the round-2 audit
> caught missing from the construction literal further down. Both are load-bearing, and both fail **silently**:
> with `deadLetterTimeout` unset, `context.WithTimeout(detached, 0)` is **expired on creation**, so **every divert
> fails instantly**, converting the message-loss fix into total message loss; with `logger` nil, `fireHook`'s recover
> arm nil-derefs, so a panicking hook crashes the caller's goroutine — precisely what `fireHook` exists to prevent.
> **Cross-check this list, the `producerConfig` listing, and the construction literal against each other** before
> moving on; a field is a fact repeated at three sites.

```go
	if cfg.retrySet {
		if err := cfg.retry.Validate(); err != nil {
			return nil, err
		}
		// Reject the structurally unbounded policy: MaxAttempts == 0 (forever)
		// with a nil Backoff (no delay) — the RetryPolicy zero value.
		//
		// This is deliberately a STRUCTURAL check and NOT a probe of the
		// strategy's actual output. Evaluating cfg.retry.delayFor(1) here would
		// invoke CALLER CODE inside a constructor: BackoffStrategy is an exported
		// public interface, so an implementation may panic (CLAUDE.md: never
		// panic on caller input) or block; it is NON-DETERMINISTIC whenever
		// RandomizationFactor > 0, since jitter can truncate the sampled delay to
		// 0 and intermittently reject a valid config; and sampling one index
		// false-rejects the legitimate "first retry immediate, then back off"
		// shape (Delay(0) == 0, Delay(n>0) == 5s).
		//
		// The output-based hazard it was reaching for — ExponentialBackoff{},
		// whose Delay is always 0 — is fully handled at runtime instead, by the
		// minRetryDelay floor and the always-on budget below: 100ms x 2m caps it
		// at ~1200 attempts, not the ~900,000 the unfloored, unbudgeted loop
		// allowed.
		if cfg.retry.MaxAttempts == 0 && cfg.retry.Backoff == nil {
			return nil, ErrUnboundedRetry
		}
	}
	if !cfg.retryAfterCapSet {
		cfg.retryAfterCap = defaultRetryAfterCap
	} else if cfg.retryAfterCap <= 0 {
		return nil, ErrInvalidRetryAfterCap
	}
	// The budget ALWAYS applies. An earlier revision defaulted it only when
	// MaxAttempts == 0, to stop it silently truncating an explicit attempt count
	// — but that left MaxAttempts > 0 completely unbudgeted, so
	// RetryPolicy{MaxAttempts: 1_000_000, Backoff: ExponentialBackoff{}} runs
	// 10^6 x the 100ms floor = ~27.7 HOURS on the caller's goroutine. That is the
	// same flood the attempt-unbounded case was rejected for, respelled with a
	// large MaxAttempts instead of 0.
	//
	// The truncation objection is answered by making the two stops
	// DISTINGUISHABLE (ErrRetryBudgetExhausted) rather than by removing the
	// bound.
	if !cfg.retryBudgetSet {
		cfg.retryBudget = defaultRetryBudget
	} else if cfg.retryBudget <= 0 {
		return nil, ErrInvalidRetryBudget
	}
	if !cfg.deadLetterTimeoutSet {
		cfg.deadLetterTimeout = defaultDeadLetterTimeout
	} else if cfg.deadLetterTimeout <= 0 {
		return nil, ErrInvalidDeadLetterTimeout
	}
	return &producer[T]{
		out:               out,
		codec:             codec,
		liveValue:         live,
		clock:             cfg.clock,
		retry:             cfg.retry,
		retrySet:          cfg.retrySet,
		retryAfterCap:     cfg.retryAfterCap,
		retryBudget:       cfg.retryBudget,
		deadLetterTimeout: cfg.deadLetterTimeout,
		hooks:             cfg.hooks,
		logger:            cfg.logger,
	}, nil
```

> **`deadLetterTimeout` and `logger` are load-bearing in this literal, and the round-1 revision omitted both.**
> Round 2 caught it: with `deadLetterTimeout` unset, `context.WithTimeout(detached, 0)` is **expired on creation**, so
> **every divert fails instantly** — silently converting the message-loss fix into total message loss. With `logger`
> nil, `fireHook`'s recover arm nil-derefs, so a panicking hook crashes the caller's goroutine: exactly what
> `fireHook` exists to prevent. Both fields were added by round-1 fixes and mentioned only in surrounding prose, never
> in the authoritative code block.

**(e)** Replace `Send` and add the helpers:

```go
// Send writes msg to the outbound adapter for immediate delivery, applying the
// WithProducerRetry policy when one is configured.
func (p *producer[T]) Send(ctx context.Context, msg Message[T]) error {
	boxed, err := p.box(msg)
	if err != nil {
		return err
	}
	if !p.retrySet {
		return p.out.Send(ctx, boxed)
	}
	return p.sendRetrying(ctx, boxed)
}

// sendRetrying runs the configured RetryPolicy over a single outbound send.
// attempt is 1-based, matching RetryPolicy.delayFor's contract. deadline bounds
// the cumulative wall-clock spent retrying (WithProducerRetryBudget).
func (p *producer[T]) sendRetrying(ctx context.Context, boxed Message[any]) error {
	// The budget always applies (NewProducer defaults it and rejects a
	// non-positive explicit value), so there is no unbudgeted mode and no flag.
	deadline := p.clock.Now().Add(p.retryBudget)
	for attempt := 1; ; attempt++ {
		err := p.out.Send(ctx, boxed)
		if err == nil {
			return nil
		}
		// Permanent wins over every other classification, including RetryAfter:
		// a delay is meaningless when the answer is "do not retry".
		if isPermanent(err) {
			return err
		}
		if p.retry.MaxAttempts > 0 && attempt >= p.retry.MaxAttempts {
			return p.deadLetter(ctx, boxed, err)
		}
		wait := p.nextDelay(attempt, err)
		if p.clock.Now().Add(wait).After(deadline) {
			// The next wait would overrun the budget. Stop now rather than make
			// an attempt the budget cannot back. ErrRetryBudgetExhausted marks
			// this stop so the caller can tell it from genuine attempt
			// exhaustion — the budget is a safety bound, and hitting it usually
			// means the policy wanted more time than the caller allowed, which
			// is a different operational signal from "we tried N times".
			return p.deadLetter(ctx, boxed, fmt.Errorf("%w: %w", ErrRetryBudgetExhausted, err))
		}
		p.fireHook(p.hooks.OnRetry, ctx, boxed, err)
		if waitErr := p.wait(ctx, wait); waitErr != nil {
			// The caller went away mid-backoff. This is the COMMON cancellation
			// path, and it is terminal for this message: diverting here is what
			// stops it being lost. The divert is detached and timed, so it still
			// completes after the caller's ctx is dead.
			return errors.Join(p.deadLetter(ctx, boxed, err), waitErr)
		}
	}
}

// nextDelay picks the wait before the next attempt. A RetryAfter marker supplies
// a server-instructed MINIMUM (RFC 9110 §10.2.3), clamped to the configured cap,
// so a server may only lengthen the computed backoff — never shorten it, which
// would hand a remote endpoint a hot-spin lever. Every result is floored to
// minRetryDelay so a zero-or-negative computed backoff cannot spin.
func (p *producer[T]) nextDelay(attempt int, err error) time.Duration {
	d := p.retry.delayFor(attempt)
	if server, ok := retryAfterOf(err); ok {
		d = max(d, min(server, p.retryAfterCap))
	}
	return max(d, minRetryDelay)
}

// wait blocks for d on the injected clock, aborting on ctx cancellation so a
// cancelled caller is never parked (CLAUDE.md: everything cancellable).
func (p *producer[T]) wait(ctx context.Context, d time.Duration) error {
	timer := p.clock.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.Chan():
		return nil
	}
}

// deadLetter routes a terminal message — attempt-exhausted, budget-exhausted, or
// cancelled during a backoff wait — to the policy's
// DeadLetter sink and returns cause joined with ErrDeadLettered — the caller
// must learn the send never reached its target, so a successful divert does NOT
// become a nil error, and must be able to tell it from an outright failure.
//
// The divert runs on a ctx DETACHED from the caller's (context.WithoutCancel,
// the precedent at exchange.go:347): the usual reason the loop is ending is that
// ctx was cancelled or its deadline passed, and diverting on that same ctx would
// fail too, leaving the message in neither the target nor the DLQ.
//
// With no DeadLetter configured (MaxAttempts == 0, budget-exhausted), there is
// nowhere to divert: the cause is returned WITHOUT ErrDeadLettered, because
// nothing was dead-lettered.
func (p *producer[T]) deadLetter(ctx context.Context, boxed Message[any], cause error) error {
	if p.retry.DeadLetter == nil {
		return cause
	}
	// Detached so the caller's cancellation cannot defeat the divert, but TIMED
	// so a hung sink cannot block the caller's goroutine forever. Detaching
	// without bounding would be strictly worse than the un-retried passthrough.
	dlCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), p.deadLetterTimeout)
	defer cancel()

	// Fire on BOTH arms — "the DLQ is down and the message is lost" is the single
	// most operationally important event this loop can produce — and hand the
	// hook EXACTLY the error Send is about to return, so the hook can tell the
	// two apart the same way the caller does.
	//
	// Do NOT pass errors.Join(cause, dlErr): with dlErr == nil that is a non-nil
	// *joinError wrapping only cause, carrying no ErrDeadLettered, so the hook
	// would receive the same shape on success and on failure — unable to
	// distinguish "safely in the DLQ" from "lost", which is the whole reason
	// this hook fires.
	//
	// The hook receives dlCtx, which MAY ALREADY BE EXPIRED: the sink can consume
	// the whole deadLetterTimeout before returning. It is passed for
	// trace/value propagation, not as a work budget — a hook that performs I/O
	// must not rely on it and should derive its own bounded ctx.
	var out error
	if dlErr := p.safeDeadLetter(dlCtx, boxed); dlErr != nil {
		out = errors.Join(cause, dlErr)
	} else {
		out = fmt.Errorf("%w: %w", ErrDeadLettered, cause)
	}
	p.fireHook(p.hooks.OnDeadLetter, dlCtx, boxed, out)
	return out
}

// safeDeadLetter invokes the DeadLetter sink, recovering a panic so a faulty
// sink cannot crash the caller's goroutine — the same fault-isolation boundary
// the consumer applies to its divert sinks. The producer's own out.Send is
// deliberately NOT wrapped: it runs on the caller's goroutine and its panic
// belongs to the caller, propagating with its original value and stack.
func (p *producer[T]) safeDeadLetter(ctx context.Context, msg Message[any]) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("msgin: producer DeadLetter.Send panicked: %v", r)
		}
	}()
	return p.retry.DeadLetter.Send(ctx, msg)
}

// fireHook invokes an optional observability callback, tolerating both a nil
// hook and a panicking one: telemetry must never break the send. It mirrors
// consumer.safeFire (consumer.go:807) exactly, including logging the recovered
// panic with the message ID only — NEVER the payload.
func (p *producer[T]) fireHook(h func(context.Context, Message[any], error), ctx context.Context, msg Message[any], err error) {
	if h == nil {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			p.logger.Warn("msgin: hook panicked", "id", msg.ID(), "panic", r)
		}
	}()
	h(ctx, msg, err)
}
```

> **The dead-letter timeout is NOT clockwork-driven — a deliberate, documented deviation.** Every other wait in this
> increment runs on the injected `clockwork.Clock`, per CLAUDE.md's injectable-time rule. `deadLetter`'s bound cannot:
> it must be a **`context` deadline**, because that is the only form the `OutboundAdapter.Send` contract can carry into
> a third-party sink, and `context.WithTimeout` reads the real monotonic clock. There is no fake-clock seam for it, and
> inventing one (a `clockwork`-driven watchdog goroutine that cancels a `context.WithCancel`) would add a goroutine per
> divert to the caller's hot path for a test-only benefit. **Consequence for the tests:** the expiry arm cannot be
> reached by advancing the fake clock — Task 4 Step 5 reaches it deterministically with a
> `WithProducerDeadLetterTimeout(time.Nanosecond)` instead. Record the deviation in Spec 013 §4 at Task 5 rather than
> leaving it implied.

> **The producer needs a logger, and did not have one.** `consumer.safeFire` (**verified**, `consumer.go:807-817`)
> recovers and **logs** via `c.logger.Warn("msgin: hook panicked", "id", …, "panic", r)`; the drafted `fireHook`
> silently discarded the panic with a bare `_ = recover()`. Swallowing an observability failure *silently* is exactly
> what CLAUDE.md's "observability hooks, not global state" constraint forbids, and it invents a second convention for
> the same problem. Add the field and the option, matching the consumer's defaults:
>
> ```go
> // WithProducerLogger injects the structured logger the producer uses to report
> // a panicking hook. Default: a discard logger, so a library consumer who wants
> // no output gets none. Named distinctly from the Consumer's WithLogger to avoid
> // collision on the shared option vocabulary (cf. WithProducerClock, ADR 0007 D10).
> func WithProducerLogger[T any](l *slog.Logger) ProducerOption[T] {
> 	return func(o *producerConfig[T]) {
> 		if l != nil {
> 			o.logger = l
> 		}
> 	}
> }
> ```
>
> Default it in `NewProducer`'s config literal exactly as `NewConsumer` does (**verified**, `consumer.go:147`):
> `logger: slog.New(slog.NewTextHandler(io.Discard, nil))`. Add `"io"` and `"log/slog"` to `producer.go`'s imports,
> carry `logger` onto the `producer[T]` struct, and add the logger cases to the construction table (branches 35/36:
> `WithProducerLogger(nil)` keeps the default and constructs; a non-nil logger is installed and constructs). Cover the
> panic-logging branch with the existing
> `"a panicking hook is contained"` test by injecting a logger writing to a `bytes.Buffer` and asserting the warning
> was emitted.

> **On `deadLetter`'s no-sink arm:** it is reachable only through budget exhaustion with `MaxAttempts == 0`, because
> `Validate` already rejects a finite `MaxAttempts` without a `DeadLetter`. Branch 19's test is what covers it — do not
> assume the `MaxAttempts > 0` path reaches it.

- [ ] **Step 4: Run — verify Step 1's table passes**

```bash
GOTOOLCHAIN=go1.25.12 go test -run 'TestProducerRetry$' -race .
```

- [ ] **Step 5: Branches 6, 8–12, 21–23 and 37 — the standalone tests**

Each needs a shape the main table cannot express. Append to `producer_retry_test.go`:

```go
// TestProducerPermanentBeatsRetryAfter proves the precedence rule by its
// OBSERVABLE consequence, not by errors.Is: a cause is matchable through either
// marker independently, so an errors.Is assertion proves nothing about which one
// won. What only Permanent produces is: exactly one attempt, no dead-letter, and
// no clock advance at all.
func TestProducerPermanentBeatsRetryAfter(t *testing.T) {
	defer goleak.VerifyNone(t)

	cause := errors.New("boom")

	// Both nesting orders must produce the identical observable outcome, so the
	// assert closure body is the same text twice — deliberately, per the
	// table-test rule: the expectation belongs in the case, not in the loop, so a
	// future case that legitimately differs cannot silently inherit these
	// assertions.
	assertPermanentWon := func(t *testing.T, out, dlq *scriptedOutbound, elapsed time.Duration, err error) {
		t.Helper()
		assert.ErrorIs(t, err, cause)
		assert.Equal(t, 1, out.attempts(), "Permanent must stop after one attempt despite the RetryAfter marker")
		assert.Equal(t, 0, dlq.attempts(), "Permanent must never dead-letter")
		assert.Equal(t, time.Duration(0), elapsed, "Permanent must not park for the RetryAfter delay")
	}

	tests := []struct {
		name   string
		err    error
		assert func(t *testing.T, out, dlq *scriptedOutbound, elapsed time.Duration, err error)
	}{
		{
			name:   "permanent outside retry-after",
			err:    msgin.Permanent(msgin.RetryAfter(cause, time.Minute)),
			assert: assertPermanentWon,
		},
		{
			name:   "retry-after outside permanent",
			err:    msgin.RetryAfter(msgin.Permanent(cause), time.Minute),
			assert: assertPermanentWon,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clock := clockwork.NewFakeClock()
			start := clock.Now()
			out := newScriptedOutbound(tt.err)
			dlq := newScriptedOutbound(nil)

			p, err := msgin.NewProducer[[]byte](out,
				msgin.WithProducerClock[[]byte](clock),
				msgin.WithProducerRetry[[]byte](msgin.RetryPolicy{MaxAttempts: 5, DeadLetter: dlq}),
			)
			require.NoError(t, err)

			sendErr := p.Send(t.Context(), msgin.New[[]byte]([]byte("payload")))

			tt.assert(t, out, dlq, clock.Now().Sub(start), sendErr)
		})
	}
}

// TestProducerDeadLetterFailure covers both failure arms of the divert: a sink
// returning an error and a sink panicking. Both join onto the cause, never
// swallow it, and the panic must not escape to the caller. Neither arm carries
// ErrDeadLettered — nothing was successfully dead-lettered.
func TestProducerDeadLetterFailure(t *testing.T) {
	defer goleak.VerifyNone(t)

	transient := errors.New("transient")
	dlqErr := errors.New("dlq down")

	tests := []struct {
		name   string
		dlq    msgin.OutboundAdapter
		assert func(t *testing.T, err error)
	}{
		{
			name: "dead-letter error is joined",
			dlq:  outboundFunc(func(context.Context, msgin.Message[any]) error { return dlqErr }),
			assert: func(t *testing.T, err error) {
				t.Helper()
				assert.ErrorIs(t, err, transient)
				assert.ErrorIs(t, err, dlqErr)
				assert.NotErrorIs(t, err, msgin.ErrDeadLettered)
			},
		},
		{
			name: "dead-letter panic is recovered and joined",
			dlq:  outboundFunc(func(context.Context, msgin.Message[any]) error { panic("dlq exploded") }),
			assert: func(t *testing.T, err error) {
				t.Helper()
				assert.ErrorIs(t, err, transient)
				assert.Contains(t, err.Error(), "dlq exploded")
				assert.Contains(t, err.Error(), "panicked")
				assert.NotErrorIs(t, err, msgin.ErrDeadLettered)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clock := clockwork.NewFakeClock()
			out := newScriptedOutbound(transient)
			p, err := msgin.NewProducer[[]byte](out,
				msgin.WithProducerClock[[]byte](clock),
				msgin.WithProducerRetry[[]byte](msgin.RetryPolicy{MaxAttempts: 1, DeadLetter: tt.dlq}),
			)
			require.NoError(t, err)

			tt.assert(t, p.Send(t.Context(), msgin.New[[]byte]([]byte("payload"))))
		})
	}
}

// TestProducerDeadLetterDetachedContext is branch 10 (audit D1): the divert must
// run on a ctx detached from the caller's, because the usual reason the loop is
// ending is that ctx was cancelled. If the DLQ send saw the cancelled ctx, a
// real sink would fail and the message would reach neither target nor DLQ.
func TestProducerDeadLetterDetachedContext(t *testing.T) {
	defer goleak.VerifyNone(t)

	transient := errors.New("transient")
	clock := clockwork.NewFakeClock()
	out := newScriptedOutbound(transient)
	dlq := newScriptedOutbound(nil)

	p, err := msgin.NewProducer[[]byte](out,
		msgin.WithProducerClock[[]byte](clock),
		msgin.WithProducerRetry[[]byte](msgin.RetryPolicy{MaxAttempts: 1, DeadLetter: dlq}),
	)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	sendErr := p.Send(ctx, msgin.New[[]byte]([]byte("payload")))

	assert.ErrorIs(t, sendErr, msgin.ErrDeadLettered)
	require.Equal(t, 1, dlq.attempts(), "the divert must still happen on a cancelled caller ctx")
	assert.NoError(t, dlq.lastCtxErr(), "the DeadLetter sink must NOT observe the caller's cancellation")
}

// TestProducerDeadLetterTimeout is branch 37: the divert's ctx must actually
// CARRY the configured timeout, so a hung sink cannot hold the caller's
// goroutine forever. It is the only bound on a detached ctx.
//
// The timeout is a context deadline, NOT a clockwork wait (see the note on
// deadLetter), so the fake clock cannot drive it. A 1ns timeout reaches the
// expiry arm deterministically instead — the deadline has passed before the sink
// is ever entered — with no real sleep and no flakiness on a fast machine.
func TestProducerDeadLetterTimeout(t *testing.T) {
	defer goleak.VerifyNone(t)

	transient := errors.New("transient")
	clock := clockwork.NewFakeClock()
	out := newScriptedOutbound(transient)

	// A realistic sink: it does not ignore its ctx, it reports it.
	dlq := outboundFunc(func(ctx context.Context, _ msgin.Message[any]) error {
		return ctx.Err()
	})

	p, err := msgin.NewProducer[[]byte](out,
		msgin.WithProducerClock[[]byte](clock),
		msgin.WithProducerDeadLetterTimeout[[]byte](time.Nanosecond),
		msgin.WithProducerRetry[[]byte](msgin.RetryPolicy{MaxAttempts: 1, DeadLetter: dlq}),
	)
	require.NoError(t, err)

	sendErr := p.Send(t.Context(), msgin.New[[]byte]([]byte("payload")))

	assert.ErrorIs(t, sendErr, transient, "the cause must survive a failed divert")
	assert.ErrorIs(t, sendErr, context.DeadlineExceeded,
		"the divert must run under the configured deadLetterTimeout, and its expiry must surface")
	assert.NotErrorIs(t, sendErr, msgin.ErrDeadLettered, "nothing was successfully dead-lettered")
}

// TestProducerRetryContextCancel covers the two cancellation arms. Both drive
// the SAME call (Producer.Send under a cancellable ctx) with different timing, so
// the mandatory table-test rule applies: drive-inputs are fields, every outcome
// assertion lives in a per-case assert closure.
func TestProducerRetryContextCancel(t *testing.T) {
	defer goleak.VerifyNone(t)

	transient := errors.New("transient")

	tests := []struct {
		name string
		// preCancel cancels before Send is ever entered; otherwise the driver
		// waits for the producer to park on the backoff timer and cancels there.
		preCancel bool
		assert    func(t *testing.T, out *scriptedOutbound, err error)
	}{
		{
			name:      "cancel during backoff",
			preCancel: false,
			assert: func(t *testing.T, out *scriptedOutbound, err error) {
				t.Helper()
				assert.ErrorIs(t, err, context.Canceled)
				assert.ErrorIs(t, err, transient, "the last attempt's error must stay visible")
				assert.Equal(t, 1, out.attempts())
			},
		},
		{
			name:      "a pre-cancelled ctx stops after one attempt",
			preCancel: true,
			assert: func(t *testing.T, out *scriptedOutbound, err error) {
				t.Helper()
				assert.ErrorIs(t, err, context.Canceled)
				assert.Equal(t, 1, out.attempts(), "an already-cancelled ctx must stop after one attempt")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clock := clockwork.NewFakeClock()
			out := newScriptedOutbound(transient)
			p, err := msgin.NewProducer[[]byte](out,
				msgin.WithProducerClock[[]byte](clock),
				msgin.WithProducerRetry[[]byte](msgin.RetryPolicy{
					Backoff: msgin.ExponentialBackoff{Initial: time.Minute},
				}), // MaxAttempts 0 = bounded only by the budget and ctx
			)
			require.NoError(t, err)

			ctx, cancel := context.WithCancel(t.Context())
			if tt.preCancel {
				cancel()
				tt.assert(t, out, p.Send(ctx, msgin.New[[]byte]([]byte("payload"))))
				return
			}

			h := runSend(t, p, clock, ctx)
			require.NoError(t, clock.BlockUntilContext(t.Context(), 1),
				"producer never parked on the backoff timer")
			cancel()
			tt.assert(t, out, h.wait(t))
		})
	}
}

// TestProducerHooks is branches 21–23: the loop's observability surface must
// fire for every retry and once for the divert, and a panicking hook must not
// break the send. Both cases drive the same call, so they are one table with
// per-case assert closures (table-test rule).
func TestProducerHooks(t *testing.T) {
	defer goleak.VerifyNone(t)

	transient := errors.New("transient")

	// counters is the only shared mutable state; each case gets its own.
	type counters struct {
		mu            sync.Mutex
		retries, dlqs int
	}

	tests := []struct {
		name   string
		script []error
		policy func(dlq msgin.OutboundAdapter) msgin.RetryPolicy
		hooks  func(c *counters) msgin.Hooks
		// waits are the delays the producer is expected to park for, in order.
		waits  []time.Duration
		assert func(t *testing.T, c *counters, out *scriptedOutbound, logs *bytes.Buffer, err error)
	}{
		{
			name:   "hooks observe retries and the divert",
			script: []error{transient},
			waits:  []time.Duration{time.Second, 2 * time.Second},
			policy: func(dlq msgin.OutboundAdapter) msgin.RetryPolicy {
				return msgin.RetryPolicy{
					MaxAttempts: 3,
					Backoff:     msgin.ExponentialBackoff{Initial: time.Second, Mult: 2},
					DeadLetter:  dlq,
				}
			},
			hooks: func(c *counters) msgin.Hooks {
				return msgin.Hooks{
					OnRetry: func(context.Context, msgin.Message[any], error) {
						c.mu.Lock()
						c.retries++
						c.mu.Unlock()
					},
					OnDeadLetter: func(context.Context, msgin.Message[any], error) {
						c.mu.Lock()
						c.dlqs++
						c.mu.Unlock()
					},
				}
			},
			assert: func(t *testing.T, c *counters, out *scriptedOutbound, logs *bytes.Buffer, err error) {
				t.Helper()
				require.ErrorIs(t, err, msgin.ErrDeadLettered)
				c.mu.Lock()
				defer c.mu.Unlock()
				assert.Equal(t, 2, c.retries, "one OnRetry per wait, not per attempt")
				assert.Equal(t, 1, c.dlqs)
			},
		},
		{
			name:   "a panicking hook is contained and logged",
			script: []error{transient, nil},
			waits:  []time.Duration{time.Second},
			policy: func(msgin.OutboundAdapter) msgin.RetryPolicy {
				return msgin.RetryPolicy{Backoff: msgin.ExponentialBackoff{Initial: time.Second}}
			},
			hooks: func(*counters) msgin.Hooks {
				return msgin.Hooks{
					OnRetry: func(context.Context, msgin.Message[any], error) { panic("hook exploded") },
				}
			},
			assert: func(t *testing.T, _ *counters, out *scriptedOutbound, logs *bytes.Buffer, err error) {
				t.Helper()
				assert.NoError(t, err, "a panicking observability hook must not break the send")
				assert.Equal(t, 2, out.attempts())
				assert.Contains(t, logs.String(), "hook panicked",
					"the recovered panic must be logged, not silently discarded")
				assert.Contains(t, logs.String(), "hook exploded")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var c counters
			var logs bytes.Buffer

			clock := clockwork.NewFakeClock()
			out := newScriptedOutbound(tt.script...)
			dlq := newScriptedOutbound(nil)

			p, err := msgin.NewProducer[[]byte](out,
				msgin.WithProducerClock[[]byte](clock),
				msgin.WithProducerLogger[[]byte](slog.New(slog.NewTextHandler(&logs, nil))),
				msgin.WithProducerRetry[[]byte](tt.policy(dlq)),
				msgin.WithProducerHooks[[]byte](tt.hooks(&c)),
			)
			require.NoError(t, err)

			h := runSend(t, p, clock, t.Context())
			for _, w := range tt.waits {
				h.stepTo(t, w)
			}
			tt.assert(t, &c, out, &logs, h.wait(t))
		})
	}
}
```

> **`logs` is read on the test goroutine only after `h.wait` returns**, which happens-after the producer goroutine's
> last write, so the `bytes.Buffer` needs no mutex and stays `-race` clean. Do **not** assert on it mid-flight.

- [ ] **Step 6: Branches 13–17, 19, 20 — the timing and budget tests**

```go
// TestProducerRetryAfter measures the ACTUAL wait, two-phase, so an
// UNDER-wait fails as loudly as an over-wait.
func TestProducerRetryAfter(t *testing.T) {
	defer goleak.VerifyNone(t)

	transient := errors.New("transient")

	// wantWait is a DRIVE-INPUT only — it tells stepTo how far to advance the
	// fake clock. Every outcome assertion lives in the case's assert closure
	// (table-test rule); nothing is asserted in the loop body.
	assertParked := func(want time.Duration) func(*testing.T, *scriptedOutbound, time.Duration, error) {
		return func(t *testing.T, out *scriptedOutbound, elapsed time.Duration, err error) {
			t.Helper()
			require.NoError(t, err)
			assert.Equal(t, 2, out.attempts())
			assert.Equal(t, want, elapsed, "the producer must park for exactly the expected delay")
		}
	}

	tests := []struct {
		name     string
		first    error
		extra    []msgin.ProducerOption[[]byte]
		wantWait time.Duration // drive-input for stepTo, not an expectation field
		assert   func(t *testing.T, out *scriptedOutbound, elapsed time.Duration, err error)
	}{
		{
			name:     "no marker takes the computed backoff",
			first:    transient,
			wantWait: time.Second,
			assert:   assertParked(time.Second),
		},
		{
			name:     "retry-after floors the wait above the computed backoff",
			first:    msgin.RetryAfter(transient, 30*time.Second),
			wantWait: 30 * time.Second,
			assert:   assertParked(30 * time.Second),
		},
		{
			name:     "retry-after cannot shorten the computed backoff",
			first:    msgin.RetryAfter(transient, time.Millisecond),
			wantWait: time.Second, // the computed backoff wins; the server may only lengthen
			assert:   assertParked(time.Second),
		},
		{
			name:     "a zero retry-after cannot shorten it either",
			first:    msgin.RetryAfter(transient, 0),
			wantWait: time.Second,
			assert:   assertParked(time.Second),
		},
		{
			// The 2m wait is exactly the 2m default budget, and survives only
			// because the budget test is a strict .After. Pin it by DESIGN rather
			// than by luck: raise the budget for this case so the wait is
			// comfortably inside it and the case tests the cap, not the budget.
			name:  "retry-after is clamped by an explicit cap",
			first: msgin.RetryAfter(transient, 10*time.Minute),
			extra: []msgin.ProducerOption[[]byte]{
				msgin.WithProducerRetryAfterCap[[]byte](2 * time.Minute),
				msgin.WithProducerRetryBudget[[]byte](10 * time.Minute),
			},
			wantWait: 2 * time.Minute,
			assert:   assertParked(2 * time.Minute),
		},
		{
			name:     "retry-after is clamped by the 60s default cap",
			first:    msgin.RetryAfter(transient, 10*time.Minute),
			wantWait: 60 * time.Second,
			assert:   assertParked(60 * time.Second),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clock := clockwork.NewFakeClock()
			start := clock.Now()
			out := newScriptedOutbound(tt.first, nil)

			opts := append([]msgin.ProducerOption[[]byte]{
				msgin.WithProducerClock[[]byte](clock),
				msgin.WithProducerRetry[[]byte](msgin.RetryPolicy{
					Backoff: msgin.ExponentialBackoff{Initial: time.Second, Mult: 2},
				}),
			}, tt.extra...)

			p, err := msgin.NewProducer[[]byte](out, opts...)
			require.NoError(t, err)

			h := runSend(t, p, clock, t.Context())
			h.stepTo(t, tt.wantWait)

			tt.assert(t, out, clock.Now().Sub(start), h.wait(t))
		})
	}
}

// TestProducerRetryBudget is branches 19/20 (audit D2): a MaxAttempts == 0
// policy is bounded by the cumulative budget, and the loop stops BEFORE a wait
// that would overrun it rather than truncating one.
func TestProducerRetryBudget(t *testing.T) {
	defer goleak.VerifyNone(t)

	transient := errors.New("transient")

	// waits is a DRIVE-INPUT (how far stepTo advances the fake clock); withDLQ is
	// a drive-input too. Everything else — attempt count, DLQ count, error shape
	// — is asserted inside the case's single assert closure, per the table-test
	// rule. No wantSends/wantWaits expectation fields.
	tests := []struct {
		name    string
		withDLQ bool
		waits   []time.Duration
		assert  func(t *testing.T, out, dlq *scriptedOutbound, err error)
	}{
		{
			name:    "budget exhaustion with no dead-letter returns the cause alone",
			withDLQ: false,
			waits:   []time.Duration{time.Second, 2 * time.Second},
			assert: func(t *testing.T, out, dlq *scriptedOutbound, err error) {
				t.Helper()
				assert.ErrorIs(t, err, transient)
				assert.ErrorIs(t, err, msgin.ErrRetryBudgetExhausted,
					"a budget stop must be distinguishable from spent attempts")
				assert.NotErrorIs(t, err, msgin.ErrDeadLettered, "nothing was dead-lettered — there is no sink")
				assert.Equal(t, 3, out.attempts())
				assert.Equal(t, 0, dlq.attempts())
			},
		},
		{
			name:    "budget exhaustion with a dead-letter diverts",
			withDLQ: true,
			waits:   []time.Duration{time.Second, 2 * time.Second},
			assert: func(t *testing.T, out, dlq *scriptedOutbound, err error) {
				t.Helper()
				assert.ErrorIs(t, err, transient)
				assert.ErrorIs(t, err, msgin.ErrRetryBudgetExhausted)
				assert.ErrorIs(t, err, msgin.ErrDeadLettered)
				assert.Equal(t, 3, out.attempts())
				assert.Equal(t, 1, dlq.attempts())
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clock := clockwork.NewFakeClock()
			out := newScriptedOutbound(transient)
			dlq := newScriptedOutbound(nil)

			// Budget 6s, and the arithmetic that picks it. ExponentialBackoff
			// {Initial: 1s, Mult: 2} yields 1s, 2s, 4s. The loop stops before a
			// wait whose END would pass the deadline (strictly After), so:
			//
			//   attempt 1 fails -> wait 1s  -> t0+1s  <= t0+6s : proceeds
			//   attempt 2 fails -> wait 2s  -> t0+3s  <= t0+6s : proceeds
			//   attempt 3 fails -> wait 4s  -> t0+7s  >  t0+6s : STOPS
			//
			// which is why the case drives exactly two waits and asserts three
			// attempts. 7s would NOT work: the third wait lands exactly ON the
			// deadline, and t0+7s.After(t0+7s) is false, so the loop would run a
			// fourth attempt and the case would hang waiting for a third step.
			policy := msgin.RetryPolicy{Backoff: msgin.ExponentialBackoff{Initial: time.Second, Mult: 2}}
			if tt.withDLQ {
				policy.DeadLetter = dlq
			}

			p, err := msgin.NewProducer[[]byte](out,
				msgin.WithProducerClock[[]byte](clock),
				msgin.WithProducerRetry[[]byte](policy),
				msgin.WithProducerRetryBudget[[]byte](6*time.Second),
			)
			require.NoError(t, err)

			h := runSend(t, p, clock, t.Context())
			for _, w := range tt.waits {
				h.stepTo(t, w)
			}

			tt.assert(t, out, dlq, h.wait(t))
		})
	}
}
```

> **Verify the budget arithmetic against the implementation before running.** With `Initial: 1s, Mult: 2` the waits are
> 1s, 2s, 4s. Starting at `t0` with a 6s budget the deadline is `t0+6s`; after two waits the clock is at `t0+3s` and the
> third wait would land at `t0+7s`, which `.After(deadline)` reports true → the loop stops after **3** sends and **2**
> waits. If the implementation's comparison is `>=` rather than `After`, or the deadline is computed differently, this
> arithmetic changes — **derive the `waits` drive-input and the asserted attempt counts from the code you actually
> wrote**, and state the derivation in the task report. Do not tune the numbers until the test passes without
> understanding why.

- [ ] **Step 7: Branches 24–36 — construction validation and scope**

```go
func TestNewProducerRetryValidation(t *testing.T) {
	t.Parallel()

	sink := outboundFunc(func(context.Context, msgin.Message[any]) error { return nil })

	tests := []struct {
		name   string
		opts   []msgin.ProducerOption[[]byte]
		assert func(t *testing.T, p msgin.Producer[[]byte], err error)
	}{
		{
			name: "finite MaxAttempts without a DeadLetter is rejected",
			opts: []msgin.ProducerOption[[]byte]{
				msgin.WithProducerRetry[[]byte](msgin.RetryPolicy{MaxAttempts: 3}),
			},
			assert: func(t *testing.T, p msgin.Producer[[]byte], err error) {
				t.Helper()
				assert.Nil(t, p)
				assert.ErrorIs(t, err, msgin.ErrNoDeadLetter)
			},
		},
		{
			name: "negative MaxAttempts is rejected",
			opts: []msgin.ProducerOption[[]byte]{
				msgin.WithProducerRetry[[]byte](msgin.RetryPolicy{MaxAttempts: -1, DeadLetter: sink}),
			},
			assert: func(t *testing.T, p msgin.Producer[[]byte], err error) {
				t.Helper()
				assert.Nil(t, p)
				assert.ErrorIs(t, err, msgin.ErrInvalidMaxAttempts)
			},
		},
		{
			name: "the RetryPolicy zero value is rejected on a producer",
			opts: []msgin.ProducerOption[[]byte]{
				msgin.WithProducerRetry[[]byte](msgin.RetryPolicy{}),
			},
			assert: func(t *testing.T, p msgin.Producer[[]byte], err error) {
				t.Helper()
				assert.Nil(t, p)
				assert.ErrorIs(t, err, msgin.ErrUnboundedRetry)
			},
		},
		{
			name: "MaxAttempts 0 with a Backoff is accepted",
			opts: []msgin.ProducerOption[[]byte]{
				msgin.WithProducerRetry[[]byte](msgin.RetryPolicy{
					Backoff: msgin.ExponentialBackoff{Initial: time.Second},
				}),
			},
			assert: func(t *testing.T, p msgin.Producer[[]byte], err error) {
				t.Helper()
				require.NoError(t, err)
				assert.NotNil(t, p)
			},
		},
		{
			name: "explicit zero retry-after cap is rejected",
			opts: []msgin.ProducerOption[[]byte]{msgin.WithProducerRetryAfterCap[[]byte](0)},
			assert: func(t *testing.T, p msgin.Producer[[]byte], err error) {
				t.Helper()
				assert.Nil(t, p)
				assert.ErrorIs(t, err, msgin.ErrInvalidRetryAfterCap)
			},
		},
		{
			name: "explicit negative retry-after cap is rejected",
			opts: []msgin.ProducerOption[[]byte]{msgin.WithProducerRetryAfterCap[[]byte](-time.Second)},
			assert: func(t *testing.T, p msgin.Producer[[]byte], err error) {
				t.Helper()
				assert.Nil(t, p)
				assert.ErrorIs(t, err, msgin.ErrInvalidRetryAfterCap)
			},
		},
		{
			name: "explicit zero retry budget is rejected",
			opts: []msgin.ProducerOption[[]byte]{msgin.WithProducerRetryBudget[[]byte](0)},
			assert: func(t *testing.T, p msgin.Producer[[]byte], err error) {
				t.Helper()
				assert.Nil(t, p)
				assert.ErrorIs(t, err, msgin.ErrInvalidRetryBudget)
			},
		},
		{
			name: "explicit negative retry budget is rejected",
			opts: []msgin.ProducerOption[[]byte]{msgin.WithProducerRetryBudget[[]byte](-time.Second)},
			assert: func(t *testing.T, p msgin.Producer[[]byte], err error) {
				t.Helper()
				assert.Nil(t, p)
				assert.ErrorIs(t, err, msgin.ErrInvalidRetryBudget)
			},
		},
		{
			name: "explicit zero dead-letter timeout is rejected",
			opts: []msgin.ProducerOption[[]byte]{msgin.WithProducerDeadLetterTimeout[[]byte](0)},
			assert: func(t *testing.T, p msgin.Producer[[]byte], err error) {
				t.Helper()
				assert.Nil(t, p)
				assert.ErrorIs(t, err, msgin.ErrInvalidDeadLetterTimeout)
			},
		},
		{
			name: "explicit negative dead-letter timeout is rejected",
			opts: []msgin.ProducerOption[[]byte]{msgin.WithProducerDeadLetterTimeout[[]byte](-time.Second)},
			assert: func(t *testing.T, p msgin.Producer[[]byte], err error) {
				t.Helper()
				assert.Nil(t, p)
				assert.ErrorIs(t, err, msgin.ErrInvalidDeadLetterTimeout)
			},
		},
		{
			name: "an explicit positive dead-letter timeout is accepted",
			opts: []msgin.ProducerOption[[]byte]{msgin.WithProducerDeadLetterTimeout[[]byte](5 * time.Second)},
			assert: func(t *testing.T, p msgin.Producer[[]byte], err error) {
				t.Helper()
				require.NoError(t, err)
				assert.NotNil(t, p)
			},
		},
		{
			// A nil logger must NOT install nil and must NOT be an error: the
			// option keeps the discard default, so fireHook's recover arm always
			// has a logger to write to.
			name: "a nil logger keeps the default and constructs",
			opts: []msgin.ProducerOption[[]byte]{msgin.WithProducerLogger[[]byte](nil)},
			assert: func(t *testing.T, p msgin.Producer[[]byte], err error) {
				t.Helper()
				require.NoError(t, err)
				assert.NotNil(t, p)
			},
		},
		{
			name: "a non-nil logger is accepted",
			opts: []msgin.ProducerOption[[]byte]{
				msgin.WithProducerLogger[[]byte](slog.New(slog.NewTextHandler(io.Discard, nil))),
			},
			assert: func(t *testing.T, p msgin.Producer[[]byte], err error) {
				t.Helper()
				require.NoError(t, err)
				assert.NotNil(t, p)
			},
		},
		{
			name: "unset cap and budget take their defaults and construct",
			opts: nil,
			assert: func(t *testing.T, p msgin.Producer[[]byte], err error) {
				t.Helper()
				require.NoError(t, err)
				assert.NotNil(t, p)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p, err := msgin.NewProducer[[]byte](sink, tt.opts...)
			tt.assert(t, p, err)
		})
	}
}

// TestProducerScheduledSendIsNotRetried pins the documented v1 scope: retry
// governs Send only, never SendAfter/SendAt.
func TestProducerScheduledSendIsNotRetried(t *testing.T) {
	defer goleak.VerifyNone(t)

	transient := errors.New("transient")
	sched := &scriptedScheduled{scriptedOutbound: newScriptedOutbound(transient)}

	clock := clockwork.NewFakeClock()
	p, err := msgin.NewProducer[[]byte](sched,
		msgin.WithProducerClock[[]byte](clock),
		msgin.WithProducerRetry[[]byte](msgin.RetryPolicy{
			Backoff: msgin.ExponentialBackoff{Initial: time.Minute},
		}),
	)
	require.NoError(t, err)

	assert.ErrorIs(t, p.SendAfter(t.Context(), msgin.New[[]byte]([]byte("x")), time.Minute), transient)
	assert.Equal(t, 1, sched.scheduledCalls())
}

// scriptedScheduled adds ScheduledSender to scriptedOutbound.
type scriptedScheduled struct {
	*scriptedOutbound
	mu   sync.Mutex
	sent int
}

func (s *scriptedScheduled) SendAfter(ctx context.Context, msg msgin.Message[any], _ time.Duration) error {
	s.mu.Lock()
	s.sent++
	s.mu.Unlock()
	return s.scriptedOutbound.Send(ctx, msg)
}

func (s *scriptedScheduled) scheduledCalls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sent
}
```

**Branch 24 (`box` error before any attempt)** is already covered by `producer_test.go`'s
`"wire adapter encode failure propagates wrapped error"` case (verified: `producer_test.go:96`). Confirm it still
passes and do **not** duplicate it; if it has moved, add an equivalent case here asserting the adapter recorded **0**
attempts.

- [ ] **Step 8: Coverage gate**

```bash
GOTOOLCHAIN=go1.25.12 go test ./... -race && \
GOTOOLCHAIN=go1.25.12 go test -coverprofile=/tmp/cov23.out -count=1 . >/dev/null && \
GOTOOLCHAIN=go1.25.12 go tool cover -func=/tmp/cov23.out | grep -E 'producer\.go|reliability\.go|backoff\.go|codec\.go|total:'
```

Expected: all packages `ok`; **every function in `producer.go`, `backoff.go` and `codec.go` at 100.0%**;
`reliability.go` at 100% **for every function this increment adds or touches** (`RetryAfter`, `retryAfterOf`,
`retryAfterError.Error`, `retryAfterError.Unwrap`); `total:` ≥ **99.1%** (the measured pre-increment core figure — this
increment must not lower it). A function below 100% means a branch from the table above is uncovered: add the case, do
not proceed and do not lower the bar.

> **`reliability.go` is deliberately NOT held to "every function at 100%", because it cannot be — verified by running
> coverage on the current tree:**
>
> ```
> reliability.go:32  isPermanent        83.3%
> reliability.go:51  NativeRedelivery    0.0%
> ```
>
> Both are **pre-existing** and unreachable blackbox. `isPermanent`'s `err == nil` arm is never hit (every caller
> passes a non-nil error), and `noNativeReliability.NativeRedelivery` is called by nothing — `consumer.go:724` uses
> only `NativeDeadLetter()` — yet it cannot be deleted, because it exists to satisfy the `NativeReliability`
> interface. So the repo's usual "delete the unreachable guard" escape hatch does **not** apply to it. Demanding 100%
> on this file would have made the gate unsatisfiable and invited an implementer to add a whitebox test, which the
> project forbids.
>
> **The same reasoning is already applied to the new code, at Task 1.** A `retryAfterOf` nil guard would be likewise
> unreachable (`nextDelay` only calls it with a non-nil `err`) **and** redundant — `errors.As(nil, &re)` already
> returns false — so Task 1 Step 3 does **not write it**, and Task 1's branch table carries no such branch. This is
> the "delete, don't `nolint`, don't whitebox" precedent that `retry.go`'s `sweepLoop` records for its `ttl<=0` case,
> applied one step earlier: never write the uncoverable guard rather than write-then-delete it.

> If a defensive guard turns out to be genuinely unreachable through the public API, do **not** add a whitebox test and
> do **not** `nolint` it — **delete the guard**, exactly as `retry.go`'s `sweepLoop` records for its `ttl<=0` case.
> An unreachable guard is dead code that the coverage gate correctly refuses to bless.

- [ ] **Step 9: Full lint, vet, format, module hygiene**

```bash
test -z "$(GOTOOLCHAIN=go1.25.12 gofmt -l .)" && \
GOTOOLCHAIN=go1.25.12 go vet ./... && \
GOTOOLCHAIN=go1.25.12 golangci-lint run ./... && \
CGO_ENABLED=0 GOTOOLCHAIN=go1.25.12 go build ./... && \
GOTOOLCHAIN=go1.25.12 go mod tidy && git diff --exit-code go.mod go.sum && echo "HYGIENE CLEAN"
```

(As in Task 1 Step 8: `gofmt -l` exits 0 while listing, so the `test -z` wrapper — not a bare `gofmt -l . && …` —
is what makes the format check actually fail the chain.)

Expected: no `gofmt` output; vet and lint clean; the cgo-free build succeeds; `go.mod`/`go.sum` unchanged.

- [ ] **Step 10: Commit**

```bash
git add producer.go producer_retry_test.go
git commit -m "$(cat <<'EOF'
feat(core): retry Producer.Send under a RetryPolicy

WithProducerRetry applies the existing RetryPolicy/BackoffStrategy machinery to
producer.Send, waiting on the producer's injected clockwork.Clock so tests need
no real sleeps. Permanent short-circuits before consuming attempt budget and is
never dead-lettered, and wins over RetryAfter in either nesting order.

The design audit's bounds are what make the loop safe on a caller's goroutine:
a RetryAfter delay is a MINIMUM per RFC 9110, so a server can lengthen the
computed backoff (up to WithProducerRetryAfterCap, 60s default) but never shorten
it; every wait is floored so a zero-yielding Backoff cannot spin; the
RetryPolicy zero value is rejected outright with ErrUnboundedRetry; and
WithProducerRetryBudget (2m default) bounds cumulative wall-clock so
MaxAttempts == 0 is finite even under context.Background().

Exhaustion diverts to the policy's DeadLetter on a ctx detached with
context.WithoutCancel — the usual reason the loop is ending is that ctx was
cancelled, so diverting on it would lose the message entirely — and returns the
cause joined with ErrDeadLettered so a caller can tell "safely in the DLQ" from
"failed outright". A failing or panicking DeadLetter sink is joined onto the
cause, never swallowed. WithProducerHooks wires the existing OnRetry/OnDeadLetter
callbacks so a terminal divert is visible in telemetry.

Scope is Send only, deliberately: inside a flow the Consumer already owns retry
by redelivery, so a second loop would multiply attempts (ADR 0025 section 2).
Retry is per-process; the seam for a fleet-wide budget is ADR 0006's rate-limit
and breaker interfaces, documented on the option.

Spec: 013
Plan: 023
ADR: 0025
EOF
)"
```

---

## Task 5: Documentation, the example, artifact reconciliation, and the delivery gate

**Files:**
- Modify: `example_reliability_test.go` — add `ExampleWithProducerRetry`
- Modify: `docs/specs/013-producer-outbound-retry.md` — §3 amended to the audited design
- Modify: `docs/adrs/0025-producer-outbound-retry.md` — §1/§3 amended; Status → Accepted
- Modify: `MESSAGING.md` — if it documents the reliability surface (check first)
- Modify: `docs/HANDOVER.md`

- [ ] **Step 1: The runnable example**

`example_reliability_test.go` currently declares only `ExampleConsumer_deadLetter` (verified — it has **no**
`outboundFn` or local sink helper). `recordingSink` **does** exist in the same test package at
`settlement_doubles_test.go:33` with a `count()` method; reuse it and do **not** redeclare it. Confirm with `gopls`
before writing.

**Imports.** The example needs `context`, `errors`, `fmt` and `time` (plus `msgin` itself). `example_reliability_test.go`
already imports `context`, `fmt`, `time` and `msgin` for `ExampleConsumer_deadLetter`; **`errors` is the one to add**
(the flaky sink returns `errors.New("connection reset")`). Confirm the existing import block with `Read`/`gopls` before
editing rather than trusting this list — and note the example takes no clock, so nothing from `clockwork` is needed.

```go
// ExampleWithProducerRetry shows a Producer retrying a transient outbound
// failure with exponential backoff.
func ExampleWithProducerRetry() {
	dlq := &recordingSink{}
	attempts := 0
	flaky := outboundFunc(func(context.Context, msgin.Message[any]) error {
		attempts++
		if attempts < 2 {
			return errors.New("connection reset")
		}
		return nil
	})

	p, err := msgin.NewProducer[[]byte](flaky,
		msgin.WithProducerCodec[[]byte](msgin.BytesPayloadCodec{}),
		msgin.WithProducerRetry[[]byte](msgin.RetryPolicy{
			MaxAttempts: 3,
			Backoff:     msgin.ExponentialBackoff{Initial: time.Millisecond, Mult: 2},
			DeadLetter:  dlq,
		}),
	)
	if err != nil {
		panic(err)
	}

	if err := p.Send(context.Background(), msgin.New[[]byte]([]byte("hello"))); err != nil {
		panic(err)
	}

	fmt.Println("attempts:", attempts)
	fmt.Println("dead-lettered:", dlq.count())
	// Output:
	// attempts: 2
	// dead-lettered: 0
}
```

> This example deliberately uses a **real** clock with a 1 ms `Initial` — an `Example` cannot inject a fake clock
> through an `// Output:` block. It is the only real backoff sleep in the increment; the outcome is deterministic
> regardless of machine speed because nothing asserts elapsed time. It also demonstrates the `BytesPayloadCodec`
> pairing Task 3 added, which is the point of that codec existing.
>
> `outboundFunc` is declared in `producer_retry_test.go`, same package — reuse it, do not redeclare.

- [ ] **Step 2: Reconcile Spec 013 and ADR 0025 with what was actually built**

The artifacts describe the **pre-audit** design and must not be left doing so (CLAUDE.md: plan/ADR ride with the code
that realizes them).

**`docs/specs/013-producer-outbound-retry.md`:**
- §3.1 — replace the loop description with the implemented one: `Permanent` short-circuit, the budget check, the
  detached dead-letter ctx, `ErrDeadLettered`, hooks, the `minRetryDelay` floor.
- §3.2 — `RetryAfter` is a **minimum**, effective wait `max(computed, min(server, cap))`. Add
  `WithProducerRetryBudget` and `WithProducerHooks` to the surface.
- §4 — add the multi-instance paragraph (D7) and the `ErrUnboundedRetry` rationale.
- §5 — replace the test-obligation list with the 37 branches of Task 4's table.
- §8 — add `WithProducerRetryBudget`, `WithProducerDeadLetterTimeout`, `WithProducerHooks`, `WithProducerLogger`,
  `BytesPayloadCodec`, and the six sentinels to the new
  exported surface. Still additive → **minor** bump.

**`docs/adrs/0025-producer-outbound-retry.md`:**
- Status → `Accepted (<date>)`.
- §1 — record the three bounds (`ErrUnboundedRetry`, floor, budget) and the detached-ctx divert as part of the decision,
  citing the audit.
- §3 — correct "overriding the computed backoff" to the RFC 9110 minimum semantics.
- Add §6 recording `BytesPayloadCodec` and why it is explicit rather than an automatic default.
- Consequences → add the observability gain (`ErrDeadLettered` + hooks); the accepted negative that the always-on
  2-minute default budget makes **every** producer policy finite — including an explicit `MaxAttempts` — a deliberate
  divergence from the Consumer's reading of the same field, made non-silent by `ErrRetryBudgetExhausted`; the
  at-least-once delivery contract retry creates; the cancel-path divert trade (a cancelled `Send` waits up to
  `deadLetterTimeout`, and a mass shutdown fills the DLQ); and the documented deviation that the dead-letter timeout is
  a **`context` deadline, not clockwork-driven**.

- [ ] **Step 2b: Commit the example and the artifact reconciliation**

Every task ends in a green commit — the plan-execution pre-authorization covers the commits a plan enumerates, and a
task that ends without one leaves the tree unlanded at the gate.

```bash
GOTOOLCHAIN=go1.25.12 go test ./... -race && \
git add example_reliability_test.go retry.go docs/specs/013-producer-outbound-retry.md \
        docs/adrs/0025-producer-outbound-retry.md docs/plans/023-producer-outbound-retry.md MESSAGING.md
git commit -m "$(cat <<'EOF'
docs(core): reconcile spec 013 and ADR 0025 with the audited retry design

The artifacts described the pre-audit design: a Retry-After that OVERRODE the
computed backoff, no retry budget, no dead-letter timeout, no observability on
the divert, and no statement of the per-process topology or the at-least-once
delivery contract the retry loop creates. Rewrites spec 013 sections 3-5 and 8
and ADR 0025 sections 1, 3 and 6 to match what shipped, and amends RetryPolicy's
own godoc, whose "MaxAttempts == 0 means retry forever" now reads differently on
the producer path than on the consumer path.

Adds ExampleWithProducerRetry, which doubles as the BytesPayloadCodec pairing
example.

Spec: 013
Plan: 023
ADR: 0025
EOF
)"
```

- [ ] **Step 3: Whole-branch delivery gate**

```bash
git log --oneline main..HEAD
GOTOOLCHAIN=go1.25.12 go test ./... -race
GOTOOLCHAIN=go1.25.12 go run golang.org/x/vuln/cmd/govulncheck@latest ./...
```

Then, per CLAUDE.md, over the **whole-branch diff** (`main..HEAD`, not the last commit):

1. **`/code-review`** — resolve or explicitly triage every finding, with a written rationale for anything triaged.
2. **`/security-review`** — this increment adds a loop that a remote party can influence through `Retry-After` and
   through error classification. Pay specific attention to: the cap/floor/budget interaction, whether any arm can be
   driven to a zero-delay loop, and whether `ErrDeadLettered` can mask a genuine failure.
3. Re-run `go test ./... -race` after any fix.

> **Plan 021's lesson, which is why this step is not a formality:** its per-task reviews were all clean, and the
> whole-branch gate still found **two proven vulnerabilities** — one of them worse than the bug the increment was
> written to fix. Re-examine settled adjudications at this gate; do not treat a per-task "clean" as binding.

- [ ] **Step 4: Update `docs/HANDOVER.md` and present for merge**

Rewrite `docs/HANDOVER.md` for the next session: Plan 023 delivered, Plan 024 next, the Plan 024 source material
pointer, and the round-1 HTTP security findings carried forward verbatim.

`git push`, the merge, and branch deletion each require **explicit per-action user approval**. Present the diffstat,
the gate output and the coverage table, then wait.

---

## Self-review

**Spec 013 coverage.** §3.1 the loop → Task 4. §3.2 `RetryAfter` + cap → Tasks 1 and 4. §3.3 the Spec 011 delta →
**Plan 024**, deliberately not here. §4 robustness → Task 4 (cancellation, budget, fault isolation) + Task 5 (docs).
§5 test obligations → Task 4's 37-branch table, which supersedes the spec's shorter list. §6.1's CLAUDE.md correction →
Task 1 Step 7.

**ADR 0025 coverage.** §1 producer-side, core → Task 4. §2 scope → documented on the option, tested by branch 31.
§3 the marker → Tasks 1 and 4. §4 cenkalti not adopted → Task 1 Steps 5/7, asserted by Step 8's grep. §5 the O2/SPI
binding → **Plan 024**.

**Round-1 audit findings folded in.** D1–D7 in the Design-deltas table above, each mapped to a task and a numbered
branch. Two findings are explicitly deferred to Plan 024 (redirect SSRF, outbound XSS) and one to it as well
(`ErrUnsupportedPayload` must be `Permanent`-wrapped by the adapter — that is adapter code).

**Round-2 plan-craft lessons applied.**
- *Fix the class, not the instance.* The audit reported "the dead-letter runs on a cancelled ctx"; the fix is the
  detached ctx **plus** a test that reads `ctx.Err()` **as the sink observed it** (`lastCtxErr`), so the class
  "something in the terminal path observes the caller's cancellation" is checked, not just the one call site.
- *Measured, two-phase waits.* `stepTo` advances to `want-1ns`, asserts **not returned**, then advances the last
  nanosecond. A one-shot `Advance(want)` cannot detect under-waiting.
- *Verify "reuse the existing helper" claims while writing the plan, not at implementation time.* Done with `grep`
  and confirmed inline: `recordingSink` **exists** (`settlement_doubles_test.go:33`, has `count()`); `outboundFunc`
  does **not** exist anywhere in `msgin_test`; `example_reliability_test.go` has **no** local sink helper; the
  encode-failure case **does** exist at `producer_test.go:96`. Each is stated at the point of use with its file:line.
- *Do not put a branch in Task N's table if it is only observable in Task N+2.* `retryAfterOf`'s two arms are
  explicitly excluded from Task 1's gate and listed in Task 4's.
- *Commit discipline.* Spec 013 and ADR 0025 are already in history (`df7eacb`, `8459d07`) under the documented
  docs-ahead-of-code exception, so the `Spec:`/`ADR:` trailers point at artifacts that exist. **This plan file is
  already committed too** (`0c7b56c`, same exception); its post-audit **amendments** ride with Task 1's commit, and the
  spec/ADR amendments ride with Task 5's.
- *`BlockUntilContext`, never `BlockUntil`.* Enforced in Global Constraints and used in every driver here.

## Round-1 audit of THIS plan (two independent Opus auditors — correctness + security). Verdict: NOT READY ×2.

All findings below are folded in above. Both auditors independently confirmed two defects the author had already
found and fixed while writing (the `backoff.go` fix breaking `"overflow without Max returns Initial"`, and the missing
producer logger), which is corroboration rather than duplication.

**Folded in — behaviour-changing:**

| Finding | Was | Now |
|---|---|---|
| **Message LOST on cancel-during-backoff** (sec #5) — the *common* cancellation path returned without diverting, which is exactly the loss D1 claims to prevent. D1's fix only covered the narrow "already cancelled at exhaustion" case the test constructs. | `return errors.Join(err, waitErr)` | `return errors.Join(p.deadLetter(ctx, boxed, err), waitErr)` |
| **Divert was uncancellable AND untimed** (sec #4) — a hung DLQ sink blocked the caller forever, immune to their cancel: strictly worse than the un-retried passthrough. | `context.WithoutCancel(ctx)` | `context.WithTimeout(context.WithoutCancel(ctx), p.deadLetterTimeout)`, + `WithProducerDeadLetterTimeout`, + `ErrInvalidDeadLetterTimeout`, 30s default |
| **`ErrUnboundedRetry` let a ~900k-attempt flood through** (sec #1, corr #8) — it tested `Backoff == nil`, missing `ExponentialBackoff{}`, a non-nil interface whose `Delay` is always 0. | `MaxAttempts == 0 && Backoff == nil` | ~~`MaxAttempts == 0 && delayFor(1) <= 0`~~ — **REVERTED by round 2** (see below): `BackoffStrategy` is an exported interface, so `delayFor` in a constructor invokes caller code, and it is non-deterministic under jitter. Back to the structural `MaxAttempts == 0 && Backoff == nil`; the `ExponentialBackoff{}` flood is handled at runtime by the floor + the always-on budget instead |
| **Default budget silently truncated an explicit `MaxAttempts`** (corr #4) and dead-lettered indistinguishably from genuine exhaustion. | budget always applied | ~~default budget applies only when `MaxAttempts == 0`; `retryBudget == 0` means unbudgeted~~ — **REVERTED by round 2** (see below): that left `MaxAttempts > 0` completely unbudgeted, reopening the same flood class. The budget **always** applies, with no flag and no unbudgeted mode; the truncation objection is answered by making the stop distinguishable with `ErrRetryBudgetExhausted` |
| **Defaults optimised for obeying a remote instruction over bounding the caller** (sec #2, corr JC1) — the 5m cap was 2.5× the worst legitimate value its own godoc cited, and the 15m budget outlived `adapter/database/sql`'s 5m default lease, so the source would reclaim and redeliver mid-send (sec #3). | cap 5m, budget 15m, floor 1ms | **cap 60s, budget 2m, floor 100ms**, with the `budget < lease` invariant documented |
| **`OnDeadLetter` never fired when the divert FAILED** (corr #7) — no telemetry for the most operationally important event this loop produces. | fired only on success | fires on both arms, carrying the joined error |
| **`jitter` had the identical overflow** (sec #7) — measured at 1.29e19 for an uncapped policy with `RandomizationFactor: 0.5` at attempt 33, so Task 2 would have claimed to close a class it left half-open. | unguarded | clamped inside `jitter`, with a covering case |
| **Task 4's own table could not pass** (corr #1) — `scriptedOutbound` is not a `LiveValueSource`, so the DLQ payload assertion compared `[]byte("payload")` against base64. | no codec paired | `WithProducerCodec[[]byte](BytesPayloadCodec{})` in the runner; **Task 3 is now a hard prerequisite of Task 4** |
| **Coverage gate unachievable** (corr #3) — `reliability.go` has two *pre-existing* blackbox-unreachable arms (`isPermanent` nil 83.3%, `NativeRedelivery` 0.0%), verified by running coverage. | "every function in reliability.go at 100%" | scoped to the functions this increment adds; `retryAfterOf`'s redundant nil guard deleted rather than carried |
| **Task 5 had no commit step** (corr #5) | — | Step 2b |

**Folded in — documentation/contract:**
`Retry-After`-as-minimum was already right, but the **at-least-once-with-duplicates** contract the retry loop creates
was undocumented (corr #9) — a retried `Send` after a timeout duplicates a send the peer may have committed; CLAUDE.md
forbids leaving a delivery guarantee implied, so `WithProducerRetry`'s godoc and Spec 013 §4 must name the receiver-side
idempotency-key requirement. `ErrDeadLettered`'s godoc softened to "produced by this producer" (sec #11 — an exported
sentinel is not forgery-proof against a hostile adapter). `BytesPayloadCodec` godoc gains two residuals (sec #15):
it removes JSON's accidental escaping layer, and `Encode(nil)` yields `nil` where `JSONPayloadCodec` yielded `null`,
which matters for a `NOT NULL` payload column. Baseline corrected to the **measured 99.1%** (corr #11 claimed 99.3%;
re-measured — the plan was right). The `unused`-linter note deleted (corr #12 — `.golangci.yml` sets
`default: none` and does not enable `unused`). `gofmt -l . && …` replaced with `test -z "$(gofmt -l .)"`, which
actually fails (corr #20). ~~`exchange.go:349`, not `:347` (corr #18)~~ — **corr #18 was WRONG and is rejected**;
round 2 re-checked the file and `:347` is the correct `context.WithoutCancel` precedent. The plan says **347**
everywhere; do not "correct" it. `retry.go` added to Task 5's files (corr #19).
The claim "`clockwork.Advance` never appends waiters" was **wrong** and is removed — `Advance` re-appends tickers via
`setExpirer` (corr #17); immaterial here since only timers are used, but it was stated as verified fact.

**Carried to Plan 024 as binding invariants** (stated here so that plan inherits them):
- Outbound classification must **never** derive `Permanent` from a remote-controlled status alone (sec #10): because
  `isPermanent` short-circuits with no dead-letter, a `413 → ErrPayloadTooLarge` mapping would hand a hostile endpoint
  a one-response "make the producer give up and record nothing" switch.
- Any remote body/status text embedded in a classification error must be **length-capped and CR/LF-stripped**
  (sec #14): this increment is what makes remote-influenced error text reach caller logs.

**Still open, for the round-2 audit:**
- Task 2's red step is **architecture-dependent** (sec #8) — measured: arm64 saturates to `MaxInt64`, amd64 yields
  `MinInt64`, so on arm64 only *one* of the two new cases goes red. Documented in Task 2; confirm CI is amd64 so the
  regression has teeth.
- The reachability of the `backoff.go` bug is **understated** (sec #9): `poller.go:132`'s `pollErrorBackoff` reaches
  attempt ≥35 after ~16 minutes of continuous poll failure, after which the poll loop busy-spins at full CPU against a
  recovering database. Task 2 should be re-framed as an availability fix with regression coverage on the *reachable*
  consumers (`pollErrorBackoff`, `delayFor` at n=40), not just on `Delay` in isolation.
- The plan's own embedded tests violate the mandatory **assert-closure** rule in three places (corr #15):
  `TestProducerRetryAfter` uses a `wantWait` field with assertions in the loop body, `TestProducerPermanentBeatsRetryAfter`'s
  table has no `assert` closure at all, and `TestProducerRetryBudget` mixes expectation fields with a partial
  `assertErr`. **Fix while implementing**: keep only drive-inputs as fields and move every outcome assertion into a
  per-case `assert` closure.

**Known judgement calls, for the round-2 audit of this plan to attack:**

1. **The defaults are now constrained from both sides, not chosen freely.** `cap (60s) < budget (2m) < the shortest
   plausible upstream lease (adapter/database/sql's 5m default)`. The lower bound stops the budget defeating
   `Retry-After` compliance; the upper bound stops a `Send` outliving the lease covering the message being handled,
   which would let the source reclaim and redeliver it mid-send. Both auditors independently argued the original
   5m/15m pair was unsafe. **A reviewer may still argue the numbers; the two inequalities are what must not change.**
2. **`ErrUnboundedRetry` makes `RetryPolicy{}` valid for a Consumer but invalid for a Producer.** That asymmetry is
   deliberate and documented on both the sentinel and the option, but it is a genuine wart: the same struct now means
   different things at two call sites. The alternative — relying on the floor + budget alone — was rejected because it
   turns an obvious caller mistake into a silent 15-minute stall. **Surface this to the user.**
3. **The default budget changes what `MaxAttempts == 0` means on a Producer** from "forever" to "until the budget".
   This is additive (nobody can have relied on it — the option is new) but it does diverge from `RetryPolicy`'s own
   godoc, which Task 5 must therefore amend to say so per-path.
4. ~~**`fireHook`'s bare `defer func() { _ = recover() }()`**~~ **RESOLVED while writing this plan.** Checked, as the
   judgement call required: `consumer.safeFire` (`consumer.go:807-817`) recovers **and logs**
   `Warn("msgin: hook panicked", "id", …, "panic", r)`. The bare form was inventing a second convention *and*
   silently discarding an observability failure. `fireHook` now matches `safeFire`, which forced a
   `WithProducerLogger` option and a `logger` field the producer previously lacked (default: a discard logger, as
   `NewConsumer` does at `consumer.go:147`). **This is the pattern to keep applying** — the plan's own stated
   uncertainty was correct, and checking cost one grep.
5. **The 20 ms real sleep in `stepTo`** is the only way to assert a negative ("has not returned yet") against a fake
   clock. It cannot cause a false failure, only a slower suite. If the reviewer finds a fake-clock-only formulation,
   prefer it.

## Round-2 audit of THIS plan (fresh Opus auditors over spec + ADR + the round-1-fixed plan). Verdict: NOT READY.

**2 critical + 9 major.** All are folded in above; this section records *what* they were, because the shape of the
round-2 findings is more instructive than any individual fix.

**The flagship finding: the round-1 defaults fix was applied to the CONSTANTS BLOCK ONLY.** Round 1 changed the
defaults from `cap 5m / budget 15m / floor 1ms` to `cap 60s / budget 2m / floor 100ms`. That change landed in the
`const` block and **nowhere else** — the plan still carried **six stale sites** derived from the old numbers, including
**two embedded tests that would HANG**: a table case still asserted a `1ms` floor while the implementation floors at
`100ms`, so the two-phase `stepTo` would advance to `1ms-1ns`, the producer would still be parked at `100ms`, and the
driver would block. A test that hangs is worse than one that fails: it burns the whole task's wall clock and reports
nothing. **Lesson: a constant is a fact repeated across a document; changing it is a document-wide edit, not a
one-line edit.** Every remaining default reference was re-derived from the constants block, not from memory.

**The budget rework reopened the class it closed.** Round 1 answered "the default budget silently truncates an
explicit `MaxAttempts`" by scoping the default budget to `MaxAttempts == 0` and introducing a `retryBudget == 0`
unbudgeted mode. Round 2 showed that leaves `MaxAttempts > 0` **completely unbudgeted**:
`RetryPolicy{MaxAttempts: 1_000_000, Backoff: ExponentialBackoff{}}` is `10^6 × 100ms` ≈ **27.7 hours** on the
caller's goroutine — the *identical* flood `ErrUnboundedRetry` rejects, respelled with a large `MaxAttempts` instead
of `0`. **Fix: the budget ALWAYS applies**, with no flag and no unbudgeted mode, and the original truncation objection
is answered by making the two stops *distinguishable* — a new `ErrRetryBudgetExhausted` sentinel joined via
`fmt.Errorf("%w: %w", …)` — rather than by removing the bound. Answer an "it hides information" objection with
information, not by deleting the safety property.

**The `delayFor(1)` probe was reverted.** Round 1 widened `ErrUnboundedRetry`'s predicate from the structural
`MaxAttempts == 0 && Backoff == nil` to `MaxAttempts == 0 && delayFor(1) <= 0`, to catch `ExponentialBackoff{}`.
Round 2 rejected it: `BackoffStrategy` is an **exported public interface**, so evaluating it inside `NewProducer`
invokes **caller code in a constructor** — which may panic (CLAUDE.md forbids panicking on caller input) or block; it
is **non-deterministic** whenever `RandomizationFactor > 0`, since jitter can truncate the sampled delay to 0 and
intermittently reject a valid config; and sampling one index false-rejects the legitimate "first retry immediate, then
back off" shape. The predicate is back to the structural form, and the `ExponentialBackoff{}` hazard is handled where
it belongs — at runtime, by the `minRetryDelay` floor plus the now-always-on budget.

**The `producer[T]` struct literal dropped two fields.** Round 1 added `deadLetterTimeout` and `logger` in prose and
in the `producerConfig` listing, but never in the authoritative construction literal. With `deadLetterTimeout` unset,
`context.WithTimeout(detached, 0)` is **expired on creation**, so **every divert fails instantly** — silently
converting the message-loss fix into total message loss. With `logger` nil, `fireHook`'s recover arm nil-derefs, so a
panicking hook crashes the caller's goroutine: precisely what `fireHook` exists to prevent. Both are now in the
literal, with a blockquote saying why they are load-bearing.

**The remaining majors, in brief:**

| Finding | Fix |
|---|---|
| Sentinel count said "four" in three places while six were declared | heading, Interfaces list and commit message all say **six** and name them |
| `retryAfterOf`'s nil guard was written in Task 1 and ordered deleted in Task 4 | never written at all; Task 1's branch table carries no such branch |
| `WithProducerDeadLetterTimeout` and `WithProducerLogger` — the two options round 1 ADDED — were exercised by **nothing** | branches 32–36, five new construction-table cases |
| The dead-letter timeout is a `context` deadline, so the fake clock cannot drive its expiry arm | documented as a deliberate deviation from the injectable-time rule; branch 37 reaches it with a 1 ns timeout |
| `jitter`'s new overflow arm is hit only ~28% per draw, so coverage would flap run-to-run | the case loops 200 draws; the miss probability and the "do not weaken the assertion" rule are stated at Task 2 Step 4 |
| Six round-1 findings recorded as "folded in" were **never actually applied** to the document | applied: the false `unused`-linter note deleted, `gofmt -l` wrapped in `test -z`, the false `clockwork.Advance` claim removed, the at-least-once contract added to `WithProducerRetry`, `ErrDeadLettered` scoped to "produced by this producer", `BytesPayloadCodec`'s two residuals added |
| The assert-closure violations were **five**, not the three round 1 admitted | `TestProducerRetryAfter`, `TestProducerPermanentBeatsRetryAfter`, `TestProducerRetryBudget`, `TestProducerRetryContextCancel` and `TestProducerHooks` all rewritten to drive-inputs + per-case `assert` closures |
| The cancel-path divert trade was undocumented | stated on `WithProducerRetry` and `WithProducerDeadLetterTimeout`, and pinned by branch 37's test |
| The budget does not bound a single adapter call | `WithProducerRetryBudget` godoc states the real worst case (`budget + one unbounded adapter call + deadLetterTimeout`); the `defaultRetryBudget` comment's unconditional `budget < lease` claim softened to "holds for the defaults as shipped" |
| `exchange.go:349` (round-1 corr #18) was itself wrong | re-verified: **`:347`** is correct; corr #18 rejected and annotated so nobody "fixes" it back |

**Meta-lesson — this is the same failure mode twice.** Every one of the round-2 criticals is *fix-the-instance,
not-the-class*: the defaults were fixed in one block and not the six sites that depend on them; the unbounded-retry
predicate was fixed for `MaxAttempts == 0` and not for the equivalent `MaxAttempts = 10^6`; the divert was given a
timeout field that was never wired. **Plan 022 took three audit rounds for exactly this reason.** When a round-N fix
lands, the round-N+1 question is not "is the fix correct?" but "**where else does this fact appear, and what else is
in this class?**" Ask it *before* declaring a finding folded in — and verify the fold-in landed in the document, since
six of round 1's did not.
