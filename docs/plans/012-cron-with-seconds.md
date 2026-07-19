# Cron `WithSeconds` (6-field schedule) Implementation Plan

- **Status:** Ready for implementation (2026-07-19) — **single-round** adversarial design audit complete (user
  approved a lighter check than the 2-round norm for this small, additive increment): verdict **SOUND WITH FIXES**,
  no BLOCKER/HIGH/MEDIUM. All six ground-truth checks confirmed against the robfig source + current code (API +
  required-`Second` flag; 6-field cron → `*SpecSchedule` so Locker accepted while `@every` → `ConstantDelaySchedule`
  refused; behavior-preserving reorder; µs-precision `fire_ts` so no coordinator/`crontest` change). The two NITs
  (test-compile hygiene) are folded (reuse the existing `&fakeLocker{}` double). **Gated only on an explicit user
  go-ahead + execution-mode choice before any implementation code (CLAUDE.md design-time gate).**

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.
>
> **Mandatory Go skills (CLAUDE.md hard rule):** the task starts from **`cc-skills-golang:golang-how-to`** (the always-on orchestrator — it loads the relevant `golang-*` skills: error handling, naming, testing, lint). Follow **TDD** via **`superpowers:test-driven-development`** (red → green → refactor). Use **`gopls`** (the native `LSP` tool, or the `gopls` CLI) for all Go navigation, diagnostics, and refactoring — prefer it over text search when reasoning about symbols. Obey the project-local testing skills that **override** samber's testing guidance: **`table-test`** (assert-closure tables, `t.Context()`), blackbox `_test` packages. `use-mockgen`/`use-testcontainers` are not triggered by this increment (no new interface to mock, no external resource).

**Goal:** Add an opt-in `cron.WithSeconds()` option that switches `NewSource` to a required 6-field (seconds) cron parser, closing Spec 006 O6-2 / N4 — the default (5-field + `@every` + descriptors) is unchanged.

**Architecture:** `robfig/cron/v3` (already the 3rd core dependency, ADR 0016) exposes a `Second` parse flag. Add a package-level `secondsParser = robfig.NewParser(Second|Minute|Hour|Dom|Month|Dow|Descriptor)` (the exact option set robfig's own `cron.WithSeconds()` uses — required seconds). A new no-arg `WithSeconds() Option` sets an unexported `config.seconds` flag; `NewSource` applies options **before** parsing so it can pick `secondsParser` when the flag is set, otherwise the current `robfig.ParseStandard`. Every other mechanism (grid-tracking loop, satisfiability guard, timezone handling, `@every`+Locker refusal, SQL coordinators) is untouched — a 6-field cron is grid-aligned, so a `Locker` is still accepted; the SQL coordinators are schedule-granularity-agnostic (no `dialect.go`/`crontest` change). This is purely `source.go` + tests + `doc.go`.

**Tech Stack:** Go 1.25, stdlib + the three blessed core deps (`robfig/cron/v3`, `jonboulle/clockwork`, `cenkalti/backoff/v4` — the last unused here). Tests: blackbox `package cron_test`, `stretchr/testify`, `go.uber.org/goleak` (via the existing `TestMain`), `clockwork.NewFakeClock`.

**Traceability:** Implements [Spec 006](../specs/006-cron-source.md) **D13** (closes N4/O6-2); records the decision in [ADR 0017](../adrs/0017-cron-source.md) (the **`WithSeconds` addendum**). Builds on ADR 0016 (the robfig parser). Commits carry `Spec: 006` / `Plan: 012` / `ADR: 0017` trailers. **Task 1's commit couples this plan + the Spec 006 D13 amendment + the ADR 0017 addendum with the code** (the amendments already exist in the working tree from the design step — they land in Task 1's commit).

## Global Constraints

- **Go skills (mandatory, CLAUDE.md)** — start from `cc-skills-golang:golang-how-to`; TDD via `superpowers:test-driven-development`; navigate/refactor with `gopls`; obey `table-test` (assert-closure tables, `t.Context()`); blackbox `_test` packages only. See the header note.
- **Go 1.25** — `GOTOOLCHAIN=go1.25.12`; no language/stdlib features newer than 1.25. CI pins 1.25.
- **No new dependency.** `robfig/cron/v3` is already a direct core dep; this increment adds NO new module or `require`. `go mod tidy` must leave `go.mod`/`go.sum` unchanged.
- **Reuse existing symbols — do NOT redeclare:** `robfig` is imported aliased as `robfig "github.com/robfig/cron/v3"` in `source.go`; `config`, `Option`, `ErrInvalidSchedule`, `ErrNilFactory`, `Source[T]`, `startStream` (test helper in `source_test.go`), `TestMain` goleak. Only ADD `config.seconds`, `WithSeconds`, and the `secondsParser` package var.
- **Required 6-field, not optional (Spec D13 / ADR 0017 addendum).** `WithSeconds()` MUST use robfig's `Second` flag (required), NOT `SecondOptional`. Under `WithSeconds`, a 5-field spec returns `ErrInvalidSchedule`; without it, a 6-field spec returns `ErrInvalidSchedule` (the default 5-field parser is unchanged).
- **Behavior-preserving default.** The no-`WithSeconds` path must remain exactly `robfig.ParseStandard(spec)`; existing tests must pass untouched. The options-before-parse reorder must preserve error precedence (invalid-spec reported before nil-factory).
- **Blackbox tests only** — `package cron_test`; assert-closure tables (`assert func(t, …)`, never `want`/`wantErr`); `t.Context()`; ≥2 same-call cases ⇒ a table. Reuse the `startStream` helper and the fake-clock `BlockUntilContext`→`Advance` pattern from `source_test.go`.
- **Every exported symbol has a godoc comment.** `WithSeconds` documents: required-6-field, the default-is-5-field, `@every`/descriptor still work, and the sub-minute footguns (one DB round-trip per fire with a SQL coordinator; sub-30s schedules hold Elector leadership across several fires at the default TTL).
- **Gate before the increment commit (CLAUDE.md §5)** — `go test ./adapter/cron/ -race` green + goleak-clean; `go vet ./adapter/cron/`, `gofmt -l adapter/cron/`, `golangci-lint run ./adapter/cron/...` clean; `CGO_ENABLED=0 go build ./...` succeeds; `go mod tidy` leaves `go.mod`/`go.sum` unchanged. Coverage: the new `WithSeconds` branch + the seconds/`ErrInvalidSchedule` typed-error branches each have a covering test case; `adapter/cron` stays ≥85%.

## File structure

All under `adapter/cron/` (root module, `package cron`).

- `source.go` — MODIFY: add `config.seconds bool`; add `WithSeconds() Option`; add the package-level `secondsParser` var; restructure `NewSource` to apply options before selecting the parser.
- `doc.go` — MODIFY: note the optional 6-field via `WithSeconds` in the trigger-kinds sentence + add the sub-minute footgun note.
- `seconds_test.go` — CREATE: blackbox `package cron_test` unit tests (firing at seconds granularity + validation matrix + grid-aligned Locker acceptance).

---

### Task 1: `WithSeconds()` option + required 6-field parser + docs

**Files:**
- Modify: `adapter/cron/source.go` (config field, `WithSeconds`, `secondsParser`, `NewSource` reorder)
- Modify: `adapter/cron/doc.go` (trigger-kinds sentence + sub-minute footgun note)
- Create: `adapter/cron/seconds_test.go` (blackbox `package cron_test`)
- Docs: the Spec 006 D13 amendment + ADR 0017 `WithSeconds` addendum (already in the working tree) ride in this commit.

**Interfaces:**
- Consumes: `robfig.NewParser`, `robfig.Second`/`Minute`/`Hour`/`Dom`/`Month`/`Dow`/`Descriptor`, `robfig.ParseStandard`, `robfig.Schedule` (aliased `robfig`); `config`, `Option`, `Source[T]`, `NewSource`, `ErrInvalidSchedule`, `ErrNilFactory`; the `startStream` test helper + `TestMain` goleak (in `source_test.go`).
- Produces:
  - `func WithSeconds() Option` — the new exported option.
  - unexported: `config.seconds bool`; `var secondsParser robfig.Parser`.

- [ ] **Step 1: Write the failing test** — `adapter/cron/seconds_test.go`

```go
package cron_test

import (
	"testing"
	"time"

	"github.com/jonboulle/clockwork"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kartaladev/msgin/adapter/cron"
)

// TestSource_WithSeconds proves a 6-field (seconds) schedule fires at the
// sub-minute instant when WithSeconds is set. Reuses startStream + the
// fake-clock BlockUntilContext->Advance pattern from source_test.go.
func TestSource_WithSeconds(t *testing.T) {
	epoch := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	type testCase struct {
		name   string
		spec   string
		step   time.Duration
		assert func(t *testing.T, fire time.Time)
	}
	cases := []testCase{
		{
			name: "every 5 seconds fires at +5s",
			spec: "*/5 * * * * *", step: 5 * time.Second,
			assert: func(t *testing.T, fire time.Time) {
				assert.Equal(t, epoch.Add(5*time.Second), fire.UTC())
			},
		},
		{
			name: "second-0 of each minute fires at the next minute boundary",
			spec: "0 * * * * *", step: time.Minute,
			assert: func(t *testing.T, fire time.Time) {
				assert.Equal(t, epoch.Add(time.Minute), fire.UTC())
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			clk := clockwork.NewFakeClockAt(epoch)
			src, err := cron.NewSource(tc.spec, func(fire time.Time) time.Time { return fire },
				cron.WithClock(clk), cron.WithSeconds())
			require.NoError(t, err)

			out, ctx, stop := startStream(t, src)
			defer stop()

			require.NoError(t, clk.BlockUntilContext(ctx, 1))
			clk.Advance(tc.step)

			select {
			case d := <-out:
				tc.assert(t, d.Msg.Payload().(time.Time))
			case <-time.After(2 * time.Second):
				t.Fatal("expected a fire, got none")
			}
		})
	}
}

// TestSource_WithSecondsValidation locks the required-6-field contract and the
// unchanged default: with WithSeconds a 5-field spec is ErrInvalidSchedule;
// without it a 6-field spec is ErrInvalidSchedule; @every/descriptors still work.
func TestSource_WithSecondsValidation(t *testing.T) {
	type testCase struct {
		name   string
		build  func() (any, error)
		assert func(t *testing.T, err error)
	}
	cases := []testCase{
		{
			name:  "WithSeconds + valid 6-field constructs",
			build: func() (any, error) { return cron.NewSource("*/5 * * * * *", func(time.Time) int { return 0 }, cron.WithSeconds()) },
			assert: func(t *testing.T, err error) { assert.NoError(t, err) },
		},
		{
			name:  "WithSeconds + 5-field spec is ErrInvalidSchedule",
			build: func() (any, error) { return cron.NewSource("* * * * *", func(time.Time) int { return 0 }, cron.WithSeconds()) },
			assert: func(t *testing.T, err error) { assert.ErrorIs(t, err, cron.ErrInvalidSchedule) },
		},
		{
			name:  "default (no WithSeconds) + 6-field spec is ErrInvalidSchedule",
			build: func() (any, error) { return cron.NewSource("*/5 * * * * *", func(time.Time) int { return 0 }) },
			assert: func(t *testing.T, err error) { assert.ErrorIs(t, err, cron.ErrInvalidSchedule) },
		},
		{
			name:  "WithSeconds + @hourly descriptor constructs",
			build: func() (any, error) { return cron.NewSource("@hourly", func(time.Time) int { return 0 }, cron.WithSeconds()) },
			assert: func(t *testing.T, err error) { assert.NoError(t, err) },
		},
		{
			name:  "WithSeconds + @every constructs",
			build: func() (any, error) { return cron.NewSource("@every 30s", func(time.Time) int { return 0 }, cron.WithSeconds()) },
			assert: func(t *testing.T, err error) { assert.NoError(t, err) },
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tc.build()
			tc.assert(t, err)
		})
	}
}

// TestSource_WithSecondsLockerGridAlignment proves a 6-field cron is still
// grid-aligned, so a Locker is accepted (unlike @every, which stays refused).
func TestSource_WithSecondsLockerGridAlignment(t *testing.T) {
	type testCase struct {
		name   string
		spec   string
		assert func(t *testing.T, err error)
	}
	cases := []testCase{
		{
			name:   "6-field cron + Locker constructs (grid-aligned)",
			spec:   "*/5 * * * * *",
			assert: func(t *testing.T, err error) { assert.NoError(t, err) },
		},
		{
			name:   "@every + Locker still refused under WithSeconds",
			spec:   "@every 30s",
			assert: func(t *testing.T, err error) { assert.ErrorIs(t, err, cron.ErrLockerRequiresGridSchedule) },
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Reuse the existing fakeLocker double from coordinator_test.go (same
			// package cron_test) — it has a POINTER receiver, so pass &fakeLocker{}.
			_, err := cron.NewSource(tc.spec, func(time.Time) int { return 0 },
				cron.WithSeconds(), cron.WithLocker(&fakeLocker{won: true}))
			tc.assert(t, err)
		})
	}
}
```

> Note (audit NIT, resolved): `TestSource_WithSecondsLockerGridAlignment` reuses the existing `fakeLocker` from `coordinator_test.go` (confirmed present, `package cron_test`, **pointer receiver** — wire as `&fakeLocker{...}`). So `seconds_test.go` needs NO `context` import and defines no local Locker double. Verify with `gopls` that `fakeLocker` is in scope before running; if it were ever removed, add a local value-receiver stub + the `context` import instead.

- [ ] **Step 2: Run the test to verify it fails** — `GOTOOLCHAIN=go1.25.12 go test -run 'TestSource_WithSeconds' ./adapter/cron/` → FAIL (`undefined: cron.WithSeconds`).

- [ ] **Step 3: Implement — `adapter/cron/source.go`.**

  (a) Add the parser singleton near the top-level `var (...)` block (after the imports / interface-assertion vars):

```go
// secondsParser parses a REQUIRED 6-field schedule (leading seconds field),
// selected by WithSeconds. It uses the same option set as robfig's own
// cron.WithSeconds(): Second is required, so a 5-field spec fails to parse.
// @every and descriptors still work (the Descriptor flag is set). The default
// (no WithSeconds) path keeps using robfig.ParseStandard (5-field).
var secondsParser = robfig.NewParser(
	robfig.Second | robfig.Minute | robfig.Hour | robfig.Dom | robfig.Month | robfig.Dow | robfig.Descriptor,
)
```

  (b) Add the `seconds` field to `config`:

```go
type config struct {
	clock    clockwork.Clock
	location *time.Location
	scope    string
	scopeSet bool
	elector  Elector
	locker   Locker
	logger   *slog.Logger
	seconds  bool // WithSeconds — require a 6-field (seconds) schedule
}
```

  (c) Add the option (place it alongside the other `WithX` options, e.g. after `WithLocation`):

```go
// WithSeconds opts the schedule into a REQUIRED leading seconds field — a
// 6-field cron expression (e.g. "*/5 * * * * *" fires every 5 seconds). The
// default is 5-field cron; with WithSeconds a 5-field spec is refused at
// construction (ErrInvalidSchedule), and without it a 6-field spec is likewise
// refused — one spec, one meaning. "@every" intervals and descriptors
// (@hourly/@daily/...) work with or without this option.
//
// Footguns for sub-minute schedules (both non-fatal, but size accordingly):
// paired with a SQL coordinator, EVERY fire does one DB round-trip (e.g. a
// 5-second schedule = a query every 5s per instance); and with the Elector's
// default 30s WithLeaseTTL, a sub-30s schedule holds leadership across several
// fires before re-election (still single-fire-correct — lower WithLeaseTTL for
// faster failover). Prefer the Locker for grid-aligned sub-minute schedules.
func WithSeconds() Option {
	return func(o *config) { o.seconds = true }
}
```

  (d) Restructure `NewSource` to apply options before parsing and pick the parser. Replace the current top of `NewSource` (the `robfig.ParseStandard` call + `factory == nil` check + `cfg := config{...}` + option loop) with:

```go
func NewSource[T any](spec string, factory func(fire time.Time) T, opts ...Option) (*Source[T], error) {
	cfg := config{
		clock:    clockwork.NewRealClock(),
		location: time.UTC,
		logger:   discardLogger(),
	}
	for _, opt := range opts {
		opt(&cfg)
	}

	// Pick the parser BEFORE parsing (WithSeconds ⇒ required 6-field). The
	// default path stays exactly robfig.ParseStandard, so existing callers are
	// unaffected. Parse before the nil-factory check to preserve the prior
	// error precedence (an invalid spec is reported before a nil factory).
	var schedule robfig.Schedule
	var err error
	if cfg.seconds {
		schedule, err = secondsParser.Parse(spec)
	} else {
		schedule, err = robfig.ParseStandard(spec)
	}
	if err != nil {
		return nil, fmt.Errorf("%w: %q: %v", ErrInvalidSchedule, spec, err)
	}
	if factory == nil {
		return nil, ErrNilFactory
	}

	// (satisfiability probe, scope resolution, Source construction, wireGate —
	// all UNCHANGED from here down)
```

  Leave everything from the satisfiability probe onward exactly as-is. Verify with `gopls` that `robfig.Schedule` and `robfig.Parser` (the type `secondsParser` holds) resolve and no import changes are needed.

- [ ] **Step 4: Run the tests** — `GOTOOLCHAIN=go1.25.12 go test -race ./adapter/cron/` → PASS (the new `seconds_test.go` cases + every pre-existing test, goleak-clean). Then `gofmt -l adapter/cron/` (silent) and `gopls check ./adapter/cron/source.go` (no diagnostics).

- [ ] **Step 5: Update `adapter/cron/doc.go`.** In the opening paragraph, change the trigger-kinds sentence so 6-field is mentioned, and add a short sub-minute note. Concretely, update the sentence that currently reads "(standard 5-field cron, "@every <duration>" intervals, or @daily/@hourly/... descriptors, parsed by robfig/cron/v3)" to also mention the optional 6-field, e.g. append: " — or a 6-field seconds schedule when WithSeconds is set." Then add one sentence to the delivery-guarantee or a new short "# Sub-minute schedules" note: "A sub-minute schedule (WithSeconds) paired with a SQL coordinator does one DB round-trip per fire, and at the Elector's default 30s lease a sub-30s schedule holds leadership across several fires — size WithLeaseTTL accordingly." Keep it terse; do not restate the WithSeconds godoc.

- [ ] **Step 6: Verify docs + whole package** — `GOTOOLCHAIN=go1.25.12 go doc ./adapter/cron | head` renders (no build error); `GOTOOLCHAIN=go1.25.12 go test -race ./adapter/cron/` still green; `GOTOOLCHAIN=go1.25.12 go vet ./adapter/cron/` clean; `GOTOOLCHAIN=go1.25.12 go build ./...` OK; `GOTOOLCHAIN=go1.25.12 go mod tidy && git diff --exit-code go.mod go.sum` (no change — no new dep); `golangci-lint run ./adapter/cron/...` → 0 issues. Coverage spot-check: `GOTOOLCHAIN=go1.25.12 go test -cover ./adapter/cron/` (≥85%; `WithSeconds`, the seconds-parse path, and both `ErrInvalidSchedule` branches are exercised by `seconds_test.go`).

- [ ] **Step 7: Commit** (couples Plan 012 + the Spec 006 D13 amendment + the ADR 0017 addendum with the code)

```bash
git add adapter/cron/source.go adapter/cron/doc.go adapter/cron/seconds_test.go \
        docs/specs/006-cron-source.md docs/adrs/0017-cron-source.md docs/plans/012-cron-with-seconds.md
git commit -m "feat(cron): WithSeconds() — opt-in 6-field (seconds) schedule

Adds a no-arg WithSeconds() Option that switches NewSource to a required
6-field cron parser (Second|Minute|Hour|Dom|Month|Dow|Descriptor, matching
robfig's own cron.WithSeconds()); the default stays 5-field ParseStandard.
A 5-field spec under WithSeconds — and a 6-field spec without it — is
ErrInvalidSchedule at construction. @every/descriptors work in both modes;
a 6-field cron is grid-aligned so a Locker is accepted, @every still refused.
NewSource applies options before parsing to select the parser (error
precedence preserved). Purely source.go + tests + doc.go — no dialect/crontest
change. Closes Spec 006 O6-2/N4 (D13); ADR 0017 WithSeconds addendum.

Spec: 006
Plan: 012
ADR: 0017"
```

---

## Self-review notes (author)

- **Spec coverage:** Spec 006 **D13** (required-6-field opt-in, default unchanged, invariants preserved, sub-minute footguns) is realized entirely by Task 1 — the option, the required-seconds parser, the reorder, the validation matrix, the grid-aligned-Locker acceptance, and the doc note. N4/O6-2 are marked closed in the spec.
- **No new dependency / no coordinator change:** confirmed — `robfig/cron/v3`'s `Second` flag is used; no `dialect.go`/`crontest`/`go.mod` change. The gate re-checks `go mod tidy` cleanliness.
- **Behavior-preserving default:** the non-`WithSeconds` path is literally `robfig.ParseStandard`; the options-before-parse reorder preserves the invalid-spec-before-nil-factory precedence. Existing tests are untouched and must stay green (Step 4/6).
- **Hot-path/typed-error coverage:** the `cfg.seconds` branch (both arms), the required-6-field rejection of a 5-field spec, and the default rejection of a 6-field spec each have a `seconds_test.go` case.
- **Audit scope (single round, user-approved):** the design is small and additive; the audit should still confirm (a) required-vs-optional is correctly `Second` not `SecondOptional`, (b) the reorder can't change any existing error/behavior, (c) the grid-alignment claim for 6-field cron + Locker holds (robfig maps 6-field cron to a `SpecSchedule`, not `ConstantDelaySchedule`, so it is NOT caught by the `@every` type-assert), and (d) no coordinator/`crontest` change is truly needed.
