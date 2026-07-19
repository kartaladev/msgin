# Cron / recurring source + distributed coordination Implementation Plan

- **Status:** Ready for implementation (2026-07-19) — **both** adversarial audit rounds complete (the project's
  2-round norm), all findings folded: Round-1 (2 BLOCKER + 4 HIGH + 3 MEDIUM + 6 LOW) and Round-2 (1 HIGH
  regression in the H-2 demoted-error test provocation, + NEW-LOW-1 gap-lock godoc, + NEW-NIT-1 single
  `sql.Register`). Round-2 verified both BLOCKERs genuinely closed and cleared the design for implementation with
  no round-3 needed. Records: `.superpowers/sdd/plan-011-audit-round-{1,2}.md`. **Still gated on an explicit user
  go-ahead + execution-mode choice before any implementation code (CLAUDE.md design-time gate).**

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.
>
> **Mandatory Go skills (CLAUDE.md hard rule):** every task starts from **`cc-skills-golang:golang-how-to`** (the always-on orchestrator — it loads the relevant `golang-*` skills: error handling, naming, concurrency, structs/interfaces, testing, lint, security). Follow **TDD** via **`superpowers:test-driven-development`** (red → green → refactor). Use **`gopls`** (the native `LSP` tool, or the `gopls` CLI) for all Go navigation, diagnostics, and refactoring — prefer it over text search when reasoning about symbols. Obey the project-local testing skills, which **override** samber's testing guidance where they conflict: **`table-test`** (assert-closure tables, `ctx`/`t.Context()`), **`use-mockgen`** (uber-go/mock), **`use-testcontainers`** (real external resources via a `crontest` leaf module, never fakes for the real-DB conformance). These are not optional.

**Goal:** Ship an `adapter/cron` package (root module) with a recurring/cron **`Source[T]`** (a `StreamingSource` that emits a caller-defined message on each schedule fire) plus msgin-native **distributed single-fire coordination** — an `Elector` (leader) and `Locker` (per-fire) seam with **dependency-free SQL-backed implementations of each** (PG/MySQL/SQLite).

**Architecture:** `Source[T]` parses its schedule once with `robfig/cron/v3` (the accepted third core dependency, ADR 0016) and drives a single **goleak-clean loop** over the injected `clockwork.Clock` (no background goroutine): it **grid-tracks** a single `next` pointer (seeded once, advanced by `schedule.Next(next)` per fire — NOT recomputed from `clock.Now()`, which would shift a non-grid `@every` phase after an overrun; skip-missed is preserved by advancing past elapsed instants without emitting), waits on `clock.After`, then (optionally) consults a coordination **gate** and emits `msgin.New[any](factory(fire))` as an at-most-once `Delivery` (no-op Ack/Nack). Coordination is checked **synchronously per fire** (no heartbeat goroutine): `WithElector`/`WithLocker` inject the gate; a coordinator error **skips the fire fail-safe**. Two SQL coordinators reuse proven `adapter/database/sql` patterns — the `Locker` is the `InboxDeduper` dedup-INSERT keyed on `(scope, fire_ts)`; the `Elector` is a leader-lease atomic acquire-or-renew — both DB-server-clock-based via a per-engine dialect seam kept **inside `adapter/cron`**.

**Tech Stack:** Go 1.25, stdlib + the three blessed core deps: `github.com/robfig/cron/v3` (**NEW**, ADR 0016 — schedule parse + `Next` only), `github.com/jonboulle/clockwork` (firing clock), and `github.com/cenkalti/backoff/v4` (unused here). SQL coordinators are `database/sql` only (driver injected). Tests: blackbox `_test` packages, `stretchr/testify`, `goleak`, and a new `adapter/cron/crontest` leaf module (testcontainers: real PG/MySQL/MariaDB/SQLite).

**Traceability:** Implements [Spec 006](../specs/006-cron-source.md); realizes [ADR 0016](../adrs/0016-robfig-cron-dependency.md) (the dependency) and [ADR 0017](../adrs/0017-cron-source.md) (the design). Builds on [ADR 0002](../adrs/0002-adapter-spi.md) (`StreamingSource`/`LiveValueSource`/`Delivery`), [ADR 0004](../adrs/0004-clockwork-dependency.md) (clock precedent), [ADR 0010](../adrs/0010-poller-sql-adapter.md) (the `InboxDeduper` dedup + dialect/`ValidateIdent` patterns reused). Commits carry `Spec: 006` / `Plan: 011` / `ADR: 0016,0017` trailers. **Task 1's commit couples Plan 011 + ADR 0016 + ADR 0017 with the first code** (the spec is already committed at `8cab221`).

## Global Constraints

- **Go skills (mandatory, CLAUDE.md)** — start every task from `cc-skills-golang:golang-how-to`; TDD via `superpowers:test-driven-development`; navigate/refactor with `gopls`; obey `table-test` / `use-mockgen` / `use-testcontainers`. See the header note.
- **Go 1.25** — `GOTOOLCHAIN=go1.25.12`; no language/stdlib features newer than 1.25. CI pins 1.25.
- **Exactly one new root dependency: `github.com/robfig/cron/v3`.** ADR 0016's acceptance test is a hard gate: after adding it, `go mod graph` must show it introduces **no transitive dependency** (its `go.mod` has no `require`), `go mod verify` passes, and `go mod tidy` leaves `go.mod`/`go.sum` reproducible. Add `robfig/cron/v3` attribution to `NOTICE` (MIT © 2012 Rob Figueiredo). **No driver, no testcontainers dep in the root or leaf-dialect modules** — those live only in the new `crontest` leaf module.
- **New code lives in `adapter/cron`** (root module, `package cron`). robfig's package is also named `cron`, so **import it aliased**: `robfig "github.com/robfig/cron/v3"`. The real-DB conformance lives in `adapter/cron/crontest` (`package crontest_test`, own `go.mod`).
- **Reuse existing core symbols** — do NOT redeclare: `msgin.StreamingSource`, `msgin.LiveValueSource`, `msgin.Delivery`, `msgin.Message[any]`, `msgin.New`, `msgin.WithClock`, `msgin.ErrNilAdapter` (root). The coordinator dialects reuse the **conventions** of `adapter/database/sql` (`ValidateIdent`, quote helpers, DDL builders, DB-clock SQL) but `adapter/cron` defines its **own** minimal **exported** `Querier`, `validateIdent`, `ErrInvalidTableName`, and quote helpers so it stays decoupled from the queue adapter (see **Key design decisions → KD-1**). `Querier` is exported everywhere it appears — it is legitimate dialect-author SPI, mirroring `adapter/database/sql`'s exported `Querier`.
- **Blackbox tests only** — every `_test.go` is `package cron_test` / `package crontest_test`. Assert-closure tables (`assert func(t, …)`, never `want`/`wantErr`); `t.Context()`; ≥2 same-call cases ⇒ a table. Example tests are `_test`-package too.
- **Injectable time via `clockwork` directly** — `WithClock(clockwork.Clock)`, nil = real (no-op); drive tests with `clockwork.NewFakeClock()` and `clk.BlockUntilContext(ctx, n)` → `clk.Advance(d)` (the `poller_test.go` pattern). No first-party clock abstraction. **The Source starts no goroutine; the SQL coordinators start no goroutine** (synchronous queries) — `goleak` stays clean.
- **No panic on caller input** — every construction-time misuse returns a typed error (`ErrInvalidSchedule` — including a syntactically valid but unsatisfiable spec, `ErrNilFactory`, `ErrConflictingCoordinator`, `ErrLockerRequiresGridSchedule`, `ErrNilAdapter`, `ErrNilDialect`, `ErrInvalidTableName`, `ErrInvalidLeaseTTL`, `ErrInvalidRetention`); a nil clock/logger/option is ignored (default kept), never a deferred nil-panic. The `Stream` loop never hot-spins: a zero `schedule.Next(...)` result (an unsatisfiable schedule surfacing only after construction) parks on `ctx.Done()` instead of firing.
- **Safe, overridable defaults (CLAUDE.md)** — `WithLocation` default **UTC** (overridden by a spec-embedded `CRON_TZ=`/`TZ=` prefix; `@every` ignores location); coordinator tables default (`msgin_cron_fired` / `msgin_cron_leader`); instance id default per-process crypto-random; Elector `WithLeaseTTL` default **30s** (documented invariant). Every default is overridable via a `WithX` option and documented on its godoc.
- **Every exported symbol has a godoc comment.** Delivery guarantee (at-most-once, skip-missed on overrun, fail-safe skip on coordinator error, Elector failover ≤ TTL, multi-instance-without-coordinator footgun, Locker's grid-alignment requirement) documented on the types.
- **Gate before the final increment commit (CLAUDE.md §5)** — `go test ./... -race` green across all modules (`go.work`), `go vet`/`gofmt`/`golangci-lint`/`govulncheck` clean, `CGO_ENABLED=0 go build ./...` succeeds, coverage ≥85% on every changed package with **every hot-path/typed-error branch covered** (the win/lose/error gate paths, the acquire-or-renew truthiness per dialect, the dedup verdict per dialect, the skip-missed branch, the unsatisfiable-schedule guard, the `@every`+Locker refusal, all sentinels). The MySQL/MariaDB demoted-error branches and all per-engine SQL bodies in `dialect.go` are covered by the **real-DB `crontest` run**, not the driver-free unit tests — CI MUST run `crontest`; a Docker-less local run does not satisfy this gate for `dialect.go`.

## Key design decisions (recommended defaults — flagged for the adversarial audit and the user go-ahead)

- **KD-1 — coordinator dialects live IN `adapter/cron`, and `adapter/cron` defines its own SQL micro-primitives (Spec O6-5).** The `LockerDialect`/`ElectorDialect` interfaces and their PG/MySQL/SQLite implementations live directly in the `adapter/cron` package (exported constructors `cron.PostgresLocker()`/`cron.MySQLLocker()`/`cron.SQLiteLocker()` and the `…Elector()` peers), **not** as separate per-dialect modules — verified safe because those dialects import no driver (pure `database/sql` strings), exactly like the sql adapter's dialect packages. To avoid coupling `adapter/cron` to the entire `database/sql` queue adapter for two tiny helpers, `adapter/cron` **re-declares** its own **exported** `Querier` interface + `validateIdent`/`identPattern` + per-engine quote helpers + exported `ErrInvalidTableName`, mirroring `adapter/database/sql` (~20 lines). *Alternative (rejected default):* import `msginsql "…/adapter/database/sql"` and reuse `msginsql.Querier`/`ValidateIdent` — DRYer, but drags the queue adapter into every `adapter/cron` importer. **Decided: own primitives (decoupled), `Querier` exported** — it is legitimate dialect-author SPI (a custom `LockerDialect`/`ElectorDialect` needs to name it), exactly as `adapter/database/sql` exports `Querier`; this is settled, not an open question for the audit.
- **KD-2 — the Source owns a `scope` for the Locker AND the Elector.** `Locker.Claim(ctx, scope, fire)` and `Elector.IsLeader(ctx, scope)` both take a per-call scope (Round-1 audit M-1 — the two coordinators are now symmetric) so one `SQLLocker`/`SQLElector` can gate many distinct schedules; the Source supplies it via `WithScope(string)`, **default = the raw cron `spec` string**. Documented footgun, sharpened by the Locker's grid-alignment requirement (audit B-1/L-1): all instances of the *same* job must share a scope (they do, by running the same spec) and two *distinct* jobs that happen to share a spec MUST set distinct `WithScope` or they will steal each other's fires — a real risk precisely for the grid-aligned (cron/descriptor) schedules the Locker supports, since colliding specs are common for e.g. `@hourly`.
- **KD-3 — REMOVED (superseded by KD-2 / audit M-1).** The original design baked the Elector's scope at construction (`WithElectorScope`, one leadership domain per `SQLElector` instance, leader-owns-all-schedules-it-gates). Round-1 audit M-1 flagged this as an asymmetric-interface trap: a caller sharing one `SQLElector` across `Source`s by analogy with sharing a `Locker` would silently get "one instance wins leadership and fires ALL those schedules." Fixed by giving `Elector.IsLeader` a per-call `scope` parameter (KD-2) — `WithElectorScope` no longer exists; the Elector's leadership domain is now the `scope` argument of each call, exactly like the Locker's `Claim`.
- **KD-4 — Elector `WithLeaseTTL` default 30s.** Because renewal is **on-demand per fire**, single-fire correctness holds at any TTL (the atomic acquire-or-renew serializes racers at the fire instant), but the TTL bounds the crash-failover gap for schedules whose interval is shorter than the TTL. 30s comfortably covers cross-instance clock skew at a single fire while keeping failover fast. **Coordinator choice is schedule-shaped, not "always prefer one" (audit B-1):** the `Locker` (no failover gap) is recommended for grid-aligned (cron/descriptor) schedules; the `Elector` is required for `@every` (the Locker refuses that combination, `ErrLockerRequiresGridSchedule`) and remains an option for grid-aligned schedules when the TTL failover gap is acceptable. Non-positive TTL → `ErrInvalidLeaseTTL`.
- **KD-5 — `NewSource` refuses `WithLocker` + `@every` (audit B-1).** At construction, if a `Locker` is configured and the parsed schedule type-asserts to robfig's `ConstantDelaySchedule` (the `@every <duration>` implementation), `NewSource` returns `ErrLockerRequiresGridSchedule` rather than silently shipping a coordinator that dedups nothing. This is checked in `wireGate` (Task 2) alongside the conflicting-coordinator check.

## File structure

All under `adapter/cron/` (root module, `package cron`) unless noted.

- `doc.go` — package doc (Task 6): what it is, the delivery guarantee, the multi-instance footgun, the two coordinators (grid-aligned schedules → Locker, `@every` → Elector), the `CRON_TZ=`/`WithLocation` interaction, import-alias note.
- `errors.go` — sentinels: `ErrInvalidSchedule`, `ErrNilFactory`, `ErrConflictingCoordinator`, `ErrLockerRequiresGridSchedule`, `ErrInvalidTableName`, `ErrNilDialect`, `ErrInvalidLeaseTTL`, `ErrInvalidRetention`, `ErrLockerClaimFailed`, `ErrElectorAcquireFailed` (Tasks 1–4, grown per task).
- `source.go` — `Source[T]`, `NewSource`, `Stream`, `EmitsLiveValue`, the firing loop (incl. the zero-`Next` guard), the `gate` type + `win`, the no-op settle closures, `Option` + `WithClock`/`WithLocation`/`WithScope`/`WithCronLogger` (Task 1); `WithElector`/`WithLocker` + the real `wireGate` body (incl. the `@every`+Locker refusal) (Task 2).
- `coordinator.go` — `Elector`, `Locker` interfaces (Task 1 — needed by `source.go`'s `config` fields, so declared alongside the `Source` type; the *options* and gating logic that consume them are added in Task 2).
- `sqlutil.go` — the exported `Querier` interface, `identPattern`/`validateIdent`, `ErrInvalidTableName`, per-engine quote helpers (Task 3).
- `sqllock.go` — `SQLLocker`, `NewSQLLocker`, `Claim`, `Purge`, `EnsureSchema`; `LockerOption` (`WithLockerTable`) (Task 3; no claimant identity — removed as YAGNI, see Task 3's note).
- `sqlelector.go` — `SQLElector`, `NewSQLElector`, `IsLeader(ctx, scope)`, `EnsureSchema`; `ElectorOption` (`WithElectorTable`/`WithElectorInstanceID`/`WithLeaseTTL`) (Task 4).
- `dialect.go` — `LockerDialect`/`ElectorDialect` interfaces (both taking the exported `Querier`) + the three concrete constructors each + the per-dialect DDL string builders (`PostgresLockerDDL(table)` etc.) (Tasks 3–4).
- `*_test.go` — blackbox unit tests (Tasks 1–4).
- `crontest/` — NEW leaf module (`go.mod`, `go.work` entry): real-DB conformance for both coordinators, incl. the MySQL/MariaDB demoted-error branches on a malformed table (Task 5).
- root `go.mod`/`go.sum`, `NOTICE`, `go.work` — modified (Tasks 1, 5).

---

### Task 1: `robfig/cron/v3` core dep + `adapter/cron` scaffold + `Source[T]` (schedule loop, factory, clock/location, sentinels)

**Files:**
- Modify: root `go.mod` (add `require github.com/robfig/cron/v3 vX.Y.Z`), `go.sum`, `NOTICE` (MIT attribution)
- Create: `adapter/cron/source.go`, `adapter/cron/errors.go`, `adapter/cron/coordinator.go`
- Test: `adapter/cron/source_test.go` (blackbox `package cron_test`, with `TestMain` goleak), `adapter/cron/consumer_integration_test.go` (drives the Source through `msgin.NewConsumer[T]`)
- Docs: this plan + `docs/adrs/0016-robfig-cron-dependency.md` (Status → Accepted) + `docs/adrs/0017-cron-source.md` (Status → Accepted) ride in the commit.

**Interfaces:**
- Consumes: `msgin.StreamingSource`, `msgin.LiveValueSource`, `msgin.Delivery`, `msgin.New`, `msgin.WithClock`, `msgin.NewConsumer` (root); `clockwork.Clock`; `robfig.Schedule`/`robfig.ParseStandard`.
- Produces (relied on by later tasks):
  - `type Source[T any] struct{…}`; `func NewSource[T any](spec string, factory func(fire time.Time) T, opts ...Option) (*Source[T], error)`
  - `func (*Source[T]) Stream(ctx context.Context, out chan<- msgin.Delivery) error`; `func (*Source[T]) EmitsLiveValue() bool`
  - `type Option func(*config)`; `func WithClock(clockwork.Clock) Option`; `func WithLocation(*time.Location) Option`; `func WithScope(string) Option`; `func WithCronLogger(*slog.Logger) Option`
  - `var ErrInvalidSchedule, ErrNilFactory error`
  - **`type Elector interface { IsLeader(ctx context.Context, scope string) (bool, error) }`; `type Locker interface { Claim(ctx context.Context, scope string, fire time.Time) (won bool, err error) }`** — declared here (`coordinator.go`), NOT in Task 2, because Task 1's `config` struct references both types (see H-1 in the Round-1 audit: Task 1 must compile standalone). Task 2 adds only the `WithElector`/`WithLocker` *options* and the real `wireGate` body that consumes them.
  - unexported (used by Task 2): `config` fields `clock`/`location`/`scope`/`logger`/`elector Elector`/`locker Locker`; `type gate func(ctx context.Context, fire time.Time) (won bool, err error)`; `(*Source[T]).win`; `(*Source[T]).gate`; `(*Source[T]).scope`; the Task-1 `wireGate` stub (returns nil; Task 2 replaces the body)

- [ ] **Step 1: Add the dependency + attribution.** From the repo root:

```bash
export GOTOOLCHAIN=go1.25.12
go get github.com/robfig/cron/v3@latest   # pins the latest released tag into go.mod/go.sum
```
Then append to `NOTICE` (create it if absent — it is referenced by CLAUDE.md License & release):
```
This product includes software from the robfig/cron project
(https://github.com/robfig/cron), licensed under the MIT License,
Copyright (c) 2012 Rob Figueiredo.
```
Verify the ADR 0016 acceptance test immediately:
```bash
go mod graph | grep robfig            # exactly one line: <root> github.com/robfig/cron/v3@vX.Y.Z ; NO transitive edges out of robfig
go mod verify                          # all modules verified
```
Expected: `robfig/cron/v3` appears with **no** outbound edges (it is dependency-free). If `go mod graph` shows any transitive dep rooted at robfig, STOP — the ADR 0016 premise is violated; do not proceed.

- [ ] **Step 2: Write the failing test** — `adapter/cron/source_test.go`

```go
package cron_test

import (
	"context"
	"testing"
	"time"

	"github.com/jonboulle/clockwork"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"

	"github.com/kartaladev/msgin"
	"github.com/kartaladev/msgin/adapter/cron"
)

func TestMain(m *testing.M) { goleak.VerifyTestMain(m) }

// startStream runs src.Stream on a background goroutine and returns the delivery
// channel plus a stop func that cancels and joins the goroutine (goleak safety).
func startStream(t *testing.T, src interface {
	Stream(context.Context, chan<- msgin.Delivery) error
}) (<-chan msgin.Delivery, context.Context, func()) {
	t.Helper()
	out := make(chan msgin.Delivery)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- src.Stream(ctx, out) }()
	return out, ctx, func() {
		cancel()
		<-done // join before the test returns
	}
}

func TestSource_FiresOnSchedule(t *testing.T) {
	epoch := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	type testCase struct {
		name   string
		spec   string
		step   time.Duration // the delay to Advance to reach the first fire
		assert func(t *testing.T, fire time.Time)
	}
	cases := []testCase{
		{
			name: "@every interval fires after the interval",
			spec: "@every 1h", step: time.Hour,
			assert: func(t *testing.T, fire time.Time) {
				assert.Equal(t, epoch.Add(time.Hour), fire.UTC())
			},
		},
		{
			name: "5-field cron fires at the next minute boundary",
			spec: "* * * * *", step: time.Minute,
			assert: func(t *testing.T, fire time.Time) {
				assert.Equal(t, epoch.Add(time.Minute), fire.UTC())
			},
		},
		{
			name: "@hourly descriptor fires at the next hour",
			spec: "@hourly", step: time.Hour,
			assert: func(t *testing.T, fire time.Time) {
				assert.Equal(t, epoch.Add(time.Hour), fire.UTC())
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			clk := clockwork.NewFakeClockAt(epoch)
			// factory captures the fire time into the payload so we can assert it.
			src, err := cron.NewSource(tc.spec, func(fire time.Time) time.Time { return fire },
				cron.WithClock(clk))
			require.NoError(t, err)

			out, ctx, stop := startStream(t, src)
			defer stop()

			require.NoError(t, clk.BlockUntilContext(ctx, 1)) // loop parked on clock.After
			clk.Advance(tc.step)

			select {
			case d := <-out:
				tc.assert(t, d.Msg.Payload().(time.Time))
				// stamped id + timestamp present (New path), Ack/Nack are safe no-ops.
				assert.NotEmpty(t, d.Msg.ID())
				require.NoError(t, d.Ack(t.Context()))
				require.NoError(t, d.Nack(t.Context(), false, 0))
			case <-time.After(2 * time.Second):
				t.Fatal("expected a fire, got none")
			}
		})
	}
}

// TestSource_SkipsMissedFireOnOverrun proves the skip-missed semantic (Spec D5):
// while the loop is blocked handing off one fire, advancing past a SECOND fire
// does not queue it — after the first is consumed, the next delivery is the
// THIRD scheduled instant, not the skipped second.
func TestSource_SkipsMissedFireOnOverrun(t *testing.T) {
	epoch := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	clk := clockwork.NewFakeClockAt(epoch)
	src, err := cron.NewSource("@every 1h", func(fire time.Time) time.Time { return fire },
		cron.WithClock(clk))
	require.NoError(t, err)

	out, ctx, stop := startStream(t, src)
	defer stop()

	require.NoError(t, clk.BlockUntilContext(ctx, 1))
	clk.Advance(time.Hour) // reach fire #1 (01:00); loop now blocks on `out <-`
	// Advance past fire #2 (02:00) WITHOUT reading — the loop is stuck on the send,
	// so it never registers a timer for #2; #2 is missed.
	clk.Advance(90 * time.Minute) // clock now at 02:30

	d1 := <-out // fire #1 delivered
	require.Equal(t, epoch.Add(time.Hour), d1.Msg.Payload().(time.Time).UTC())

	// The loop grid-tracks: from next=02:00 it skips (02:00 <= 02:30) to 03:00 and
	// blocks — #2 (02:00) was skipped, not queued.
	require.NoError(t, clk.BlockUntilContext(ctx, 1))
	clk.Advance(30 * time.Minute) // reach 03:00
	d2 := <-out
	require.Equal(t, epoch.Add(3*time.Hour), d2.Msg.Payload().(time.Time).UTC(),
		"the missed 02:00 fire must be skipped, not queued")
}

func TestSource_CtxCancelReturnsPromptly(t *testing.T) {
	clk := clockwork.NewFakeClockAt(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	src, err := cron.NewSource("@every 1h", func(time.Time) int { return 1 }, cron.WithClock(clk))
	require.NoError(t, err)

	out := make(chan msgin.Delivery)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- src.Stream(ctx, out) }()
	require.NoError(t, clk.BlockUntilContext(ctx, 1))

	cancel()
	select {
	case err := <-done:
		assert.ErrorIs(t, err, context.Canceled)
	case <-time.After(2 * time.Second):
		t.Fatal("Stream did not return on ctx cancel")
	}
}

func TestSource_ConstructionValidation(t *testing.T) {
	type testCase struct {
		name   string
		build  func() (any, error)
		assert func(t *testing.T, err error)
	}
	cases := []testCase{
		{
			name:  "invalid spec is ErrInvalidSchedule",
			build: func() (any, error) { return cron.NewSource("not a cron", func(time.Time) int { return 0 }) },
			assert: func(t *testing.T, err error) { assert.ErrorIs(t, err, cron.ErrInvalidSchedule) },
		},
		{
			// "0 0 30 2 *" (Feb 30) is syntactically valid but has no future
			// occurrence — robfig.Schedule.Next returns the zero time after a
			// 5-year search. Construction MUST catch this (not just the parse),
			// or the firing loop hot-spins on a zero Next (Round-1 audit B-2).
			name: "unsatisfiable spec (Feb 30) is ErrInvalidSchedule",
			build: func() (any, error) {
				return cron.NewSource("0 0 30 2 *", func(time.Time) int { return 0 })
			},
			assert: func(t *testing.T, err error) { assert.ErrorIs(t, err, cron.ErrInvalidSchedule) },
		},
		{
			name:  "nil factory is ErrNilFactory",
			build: func() (any, error) { return cron.NewSource[int]("@every 1h", nil) },
			assert: func(t *testing.T, err error) { assert.ErrorIs(t, err, cron.ErrNilFactory) },
		},
		{
			name:  "valid spec + factory constructs",
			build: func() (any, error) { return cron.NewSource("@every 1h", func(time.Time) int { return 0 }) },
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

func TestSource_EmitsLiveValue(t *testing.T) {
	src, err := cron.NewSource("@every 1h", func(time.Time) int { return 0 })
	require.NoError(t, err)
	var _ msgin.LiveValueSource = src
	assert.True(t, src.EmitsLiveValue())
}

// TestSource_WithLocation shifts the fire instant by the configured timezone.
func TestSource_WithLocation(t *testing.T) {
	// 00:30 UTC; a daily 09:00 job in a UTC+9 zone fires at 00:00 UTC (09:00 local).
	loc, err := time.LoadLocation("Asia/Tokyo") // UTC+9
	require.NoError(t, err)
	base := time.Date(2026, 1, 1, 0, 30, 0, 0, time.UTC)
	clk := clockwork.NewFakeClockAt(base)
	src, err := cron.NewSource("0 9 * * *", func(fire time.Time) time.Time { return fire },
		cron.WithClock(clk), cron.WithLocation(loc))
	require.NoError(t, err)

	out, ctx, stop := startStream(t, src)
	defer stop()
	require.NoError(t, clk.BlockUntilContext(ctx, 1))
	clk.Advance(24 * time.Hour) // comfortably past the next 09:00 Tokyo

	d := <-out
	got := d.Msg.Payload().(time.Time)
	// Next 09:00 Tokyo after 09:30 Tokyo (=00:30 UTC) is the following day 09:00 Tokyo = 00:00 UTC.
	assert.Equal(t, time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC), got.UTC())
}
```

- [ ] **Step 3: Run the test to verify it fails** — `GOTOOLCHAIN=go1.25.12 go test ./adapter/cron/` → FAIL (`package .../adapter/cron` does not exist / undefined `cron.NewSource`).

- [ ] **Step 4: Implement — `adapter/cron/errors.go`**

```go
package cron

import "errors"

var (
	// ErrInvalidSchedule is returned by NewSource when spec cannot be parsed as a
	// 5-field cron expression, an "@every <duration>" interval, or a descriptor
	// (@daily/@hourly/@weekly/@monthly/@yearly/@midnight), OR when spec parses but
	// has NO future occurrence (e.g. "0 0 30 2 *" — Feb 30 never happens; robfig's
	// Schedule.Next returns the zero time after a 5-year search). Construction
	// probes satisfiability, not just parseability, so an unsatisfiable schedule
	// never reaches the firing loop (Round-1 audit B-2 — an unguarded zero Next
	// would hot-spin). It wraps the parser's error (or names the unsatisfiable
	// spec), naming the offending spec — the construction-time debuggability
	// surface — rather than deferring the failure to Stream. errors.Is-able.
	ErrInvalidSchedule = errors.New("msgin/cron: invalid schedule spec")

	// ErrNilFactory is returned by NewSource when the message factory is nil. The
	// factory is the required source of every emitted payload, so a nil one is
	// refused up front rather than dereferenced into a panic on the first fire.
	ErrNilFactory = errors.New("msgin/cron: nil message factory")
)
```

- [ ] **Step 4b: Implement — `adapter/cron/coordinator.go`.** Declared in Task 1 (not Task 2) because Task 1's `config` struct types its `elector`/`locker` fields on these interfaces — deferring them to Task 2 would leave Task 1 non-compiling (Round-1 audit H-1). Task 2 adds only the `WithElector`/`WithLocker` options and the real `wireGate` gating logic; the interfaces themselves are stable from Task 1 onward.

```go
package cron

import (
	"context"
	"time"
)

// Elector gates fires under a given scope: only the elected leader for that
// scope emits. IsLeader runs an atomic acquire-or-renew each call (checked
// synchronously per fire — no heartbeat goroutine); leadership holds while a
// call's lease is still valid and re-elects once it has expired — NOT
// unconditionally "sticky": with on-demand renewal and WithLeaseTTL shorter
// than the fire interval, every fire is a fresh election. A non-nil error
// causes the fire to be SKIPPED fail-safe (never N-fold firing). Configure it
// with WithElector; at most one of WithElector/WithLocker may be set. The
// scope passed on each call is the Source's own scope (WithScope), so one
// Elector instance naturally gates many independent schedules — symmetric with
// Locker.Claim. The SQL-backed implementation is SQLElector; it is the
// coordinator to use for "@every" schedules (the Locker is restricted to
// grid-aligned schedules — see Locker).
type Elector interface {
	IsLeader(ctx context.Context, scope string) (bool, error)
}

// Locker gates ONE fire of a Source: the instance that claims (scope, fire)
// emits; the rest skip. Claim is a deterministic per-fire dedup — every
// instance computes the same (scope, fire) key and exactly one wins. A
// non-nil error causes the fire to be SKIPPED fail-safe. Configure it with
// WithLocker; the Source supplies scope from WithScope (default: the spec
// string). The SQL-backed implementation is SQLLocker; it is the recommended
// primitive for GRID-ALIGNED schedules (standard 5-field cron and
// @daily/@hourly/... descriptors) — no failover gap. It is UNSUPPORTED for
// "@every" schedules: NewSource refuses a Locker paired with an "@every"
// schedule at construction (ErrLockerRequiresGridSchedule), because
// "@every"'s next-fire computation is relative to each instance's own
// last-fire/start time, so independent instances never converge on the same
// dedup key — the Locker would silently dedup nothing. Use the Elector for
// "@every".
type Locker interface {
	Claim(ctx context.Context, scope string, fire time.Time) (won bool, err error)
}
```

- [ ] **Step 5: Implement — `adapter/cron/source.go`**

```go
// Package cron is the msgin channel adapter that ORIGINATES messages on a
// recurring / cron schedule. See doc.go for the full package overview.
package cron

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/jonboulle/clockwork"
	robfig "github.com/robfig/cron/v3"

	"github.com/kartaladev/msgin"
)

// Source is a msgin.StreamingSource that emits a caller-defined message on each
// fire of a cron / recurring schedule. It carries LIVE Go values (no codec),
// like the memory adapter, so NewConsumer[T] pairs it with no codec.
//
// Delivery guarantee: AT-MOST-ONCE. A fire is an ephemeral trigger, not a
// durable row — Ack/Nack are no-ops. A transient handler failure is still
// retried in-process by the runtime's RetryPolicy (same delivery); a permanent
// failure routes to the invalid/DLQ sink. On overrun (a slow handler blocks the
// hand-off past a scheduled instant) the missed fire is SKIPPED, not queued.
//
// Multi-instance: with NO coordinator (WithElector/WithLocker), N replicas each
// fire on every tick (N-fold). Configure a coordinator for single-fire.
type Source[T any] struct {
	schedule robfig.Schedule
	factory  func(fire time.Time) T
	clock    clockwork.Clock
	location *time.Location
	scope    string
	gate     gate // nil unless a coordinator is configured (Task 2)
	logger   *slog.Logger
}

var (
	_ msgin.StreamingSource = (*Source[any])(nil)
	_ msgin.LiveValueSource = (*Source[any])(nil)
)

// gate reports whether THIS instance should emit the fire at fireTime — the
// coordination hook consulted once per fire (Task 2). nil means "always emit".
type gate func(ctx context.Context, fire time.Time) (won bool, err error)

// config accumulates Option settings before NewSource builds a Source.
type config struct {
	clock    clockwork.Clock
	location *time.Location
	scope    string
	scopeSet bool
	elector  Elector // Task 2
	locker   Locker  // Task 2
	logger   *slog.Logger
}

// Option configures a Source built by NewSource.
type Option func(*config)

// WithClock injects the clock the firing loop waits on. Default is the real wall
// clock; tests inject a clockwork.FakeClock. A nil clock is ignored (the default
// stays in place) rather than a deferred nil-panic.
func WithClock(c clockwork.Clock) Option {
	return func(o *config) {
		if c != nil {
			o.clock = c
		}
	}
}

// WithLocation sets the timezone the schedule is evaluated in. Cron specs are
// timezone-sensitive ("0 9 * * *" means 09:00 in WHICH zone); the default is
// UTC — the safe, explicit choice. A nil location is ignored (UTC kept). A
// spec-embedded "CRON_TZ=..."/"TZ=..." prefix (robfig-supported) OVERRIDES
// this option for that spec — robfig's parser bakes the prefix's zone into the
// parsed Schedule, which then ignores the input time's location. "@every"
// intervals ignore location entirely (they are relative durations, not
// wall-clock instants).
func WithLocation(loc *time.Location) Option {
	return func(o *config) {
		if loc != nil {
			o.location = loc
		}
	}
}

// WithScope sets the coordination scope passed to a Locker's Claim or an
// Elector's IsLeader (it has no effect without WithLocker/WithElector).
// Default is the raw spec string. All instances of the SAME scheduled job must
// share a scope (running the same spec, they do); two DISTINCT jobs that
// happen to share a spec MUST set distinct scopes, or one job's per-fire claim
// (Locker) or leadership (Elector) will suppress the other's fire. This
// collision risk is real precisely for the GRID-ALIGNED (cron/descriptor)
// specs the Locker supports — e.g. two unrelated jobs both scheduled
// "@hourly" collide by default — since the Locker's dedup key is only
// instance-invariant for that class of schedule (see Locker). An empty string
// is treated as unset (the spec-string default is used).
func WithScope(scope string) Option {
	return func(o *config) {
		o.scope = scope
		o.scopeSet = true
	}
}

// WithCronLogger injects the structured logger the Source uses to report a
// coordinator error before it skips a fire (fail-safe). Default is a discard
// logger — the Source never logs to a package global. A nil logger is ignored.
func WithCronLogger(l *slog.Logger) Option {
	return func(o *config) {
		if l != nil {
			o.logger = l
		}
	}
}

// NewSource parses spec once (cron 5-field + "@every" + descriptors) and builds
// a Source emitting factory(fireTime) on each fire. An unparseable spec, OR a
// syntactically valid spec with no future occurrence (e.g. "0 0 30 2 *"), is
// ErrInvalidSchedule; a nil factory is ErrNilFactory; both a WithElector and a
// WithLocker set is ErrConflictingCoordinator (Task 2); a WithLocker paired
// with an "@every" schedule is ErrLockerRequiresGridSchedule (Task 2) — all at
// construction, so misuse fails loudly up front rather than on the first fire
// (or, for the unsatisfiable-schedule case, never — see the Stream guard).
func NewSource[T any](spec string, factory func(fire time.Time) T, opts ...Option) (*Source[T], error) {
	schedule, err := robfig.ParseStandard(spec)
	if err != nil {
		return nil, fmt.Errorf("%w: %q: %v", ErrInvalidSchedule, spec, err)
	}
	if factory == nil {
		return nil, ErrNilFactory
	}

	cfg := config{
		clock:    clockwork.NewRealClock(),
		location: time.UTC,
		logger:   discardLogger(),
	}
	for _, opt := range opts {
		opt(&cfg)
	}

	// Probe satisfiability (Round-1 audit B-2): ParseStandard accepts specs with
	// no future occurrence (e.g. Feb 30). An unguarded Stream loop would compute
	// a zero Next, a hugely negative Sub, and hot-spin on an immediately-firing
	// real clock.After. Catch it here rather than at runtime.
	if schedule.Next(cfg.clock.Now().In(cfg.location)).IsZero() {
		return nil, fmt.Errorf("%w: %q has no future occurrence", ErrInvalidSchedule, spec)
	}

	scope := spec
	if cfg.scopeSet && cfg.scope != "" {
		scope = cfg.scope
	}

	s := &Source[T]{
		schedule: schedule,
		factory:  factory,
		clock:    cfg.clock,
		location: cfg.location,
		scope:    scope,
		logger:   cfg.logger,
	}
	if err := s.wireGate(&cfg); err != nil { // Task 2 — no-op until WithElector/WithLocker exist
		return nil, err
	}
	return s, nil
}

// Stream drives the firing loop until ctx is cancelled (msgin.StreamingSource).
// It starts NO goroutine — the loop runs on the caller's (runtime Run) goroutine
// — so it is goleak-clean.
//
// GRID-TRACKING (refined in Task 1 — see the plan's Task-1 loop note): the loop
// holds a single `next` pointer, seeded ONCE from the clock's current time, and
// advances it by exactly one schedule step (schedule.Next(next)) each time an
// instant is reached — NEVER recomputing from clock.Now() every iteration.
// Recomputing from "now" is wrong for a non-grid "@every <duration>" schedule
// (robfig ConstantDelaySchedule.Next(t) = t+duration, not grid-aligned):
// reseeding from an arbitrary post-overrun "now" would silently shift the
// interval's phase. Before waiting, the loop advances `next` past every
// already-elapsed instant WITHOUT emitting (a slow hand-off let them pass) —
// the "skip missed, not queued" guarantee (Spec D5).
func (s *Source[T]) Stream(ctx context.Context, out chan<- msgin.Delivery) error {
	next := s.schedule.Next(s.clock.Now().In(s.location))
	for {
		if next.IsZero() {
			// Belt-and-suspenders for a schedule that becomes unsatisfiable only
			// after construction validated it (NewSource already refuses an
			// unsatisfiable spec up front, Round-1 audit B-2; the far-future
			// zero-Next case is Round-2 audit NEW-NIT-2) — never fire, exit
			// cleanly on cancel rather than hot-spin on a hugely negative Sub.
			<-ctx.Done()
			return ctx.Err()
		}
		for !next.After(s.clock.Now().In(s.location)) {
			// An overrun on the previous fire's hand-off let this instant elapse:
			// skip it (never deliver, never queue) and grid-track to the next step.
			next = s.schedule.Next(next)
			if next.IsZero() {
				<-ctx.Done()
				return ctx.Err()
			}
		}
		select {
		case <-s.clock.After(next.Sub(s.clock.Now())):
			fire := next
			next = s.schedule.Next(fire) // advance the grid pointer for the next iteration
			if !s.win(ctx, fire) {
				continue // lost the fire, or a coordinator error → skip fail-safe
			}
			msg := msgin.New[any](s.factory(fire), msgin.WithClock(s.clock))
			select {
			case out <- msgin.Delivery{Msg: msg, Ack: noopAck, Nack: noopNack}:
			case <-ctx.Done():
				return ctx.Err()
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// win reports whether this instance should emit the fire at fireTime. With no
// gate it always wins. A coordinator ERROR is logged and treated as a LOSS
// (fail-safe skip) — a coordination outage must degrade to NO fire, never to
// N-fold firing.
func (s *Source[T]) win(ctx context.Context, fireTime time.Time) bool {
	if s.gate == nil {
		return true
	}
	won, err := s.gate(ctx, fireTime)
	if err != nil {
		s.logger.Error("msgin/cron: coordinator error; skipping fire (fail-safe)",
			"err", err, "fire", fireTime)
		return false
	}
	return won
}

// EmitsLiveValue reports that this source carries live Go values (no codec).
func (s *Source[T]) EmitsLiveValue() bool { return true }

// noopAck / noopNack settle an at-most-once fire: there is no durable row to
// delete or requeue, so both are no-ops.
func noopAck(context.Context) error                    { return nil }
func noopNack(context.Context, bool, time.Duration) error { return nil }

// discardLogger is the default logger: it drops every record.
func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
```

> Note on `wireGate`: Task 1 introduces the method as a stub that only checks the conflicting-coordinator case is impossible yet (no elector/locker fields are settable until Task 2). To keep Task 1 green and self-contained, define it minimally in `source.go`:
> ```go
> // wireGate resolves the configured coordinator (if any) into s.gate. Extended in
> // Task 2 with WithElector/WithLocker; until then no coordinator is settable.
> func (s *Source[T]) wireGate(cfg *config) error { return nil }
> ```
> Task 2 replaces this body. (Declaring it now keeps `NewSource` stable across tasks — no signature churn.)

- [ ] **Step 6: Run the tests** — `GOTOOLCHAIN=go1.25.12 go test -race ./adapter/cron/` → PASS (all Task-1 subtests, goleak-clean). Then `gofmt -l adapter/cron/` (silent) and `gopls check ./adapter/cron/source.go` (no diagnostics). Confirm the whole workspace still builds: `GOTOOLCHAIN=go1.25.12 go build ./...`.

- [ ] **Step 6b: Prove the cron `Source` works THROUGH `msgin.NewConsumer[T]`, not only via direct `Stream` draining (Round-1 audit H-4).** Every other Task-1 test drives `Source.Stream` by hand; nothing proves the runtime actually accepts a cron `Source` as a `LiveValueSource`, pairs it with no codec, and round-trips the payload back to the handler's `T`. Add `adapter/cron/consumer_integration_test.go` (blackbox `package cron_test`, mirroring `consumer_test.go`'s `runConsumer`/handler-recorder pattern):

```go
package cron_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/jonboulle/clockwork"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kartaladev/msgin"
	"github.com/kartaladev/msgin/adapter/cron"
)

// handlerRecorder records every fire the consumer's handler observes, so the
// test can assert the payload arrived as the handler's own T (proving the
// factory(fire time.Time) T -> msgin.New[any] -> runtime -> Handler[T] round
// trip, not just that Source.Stream emits something).
type handlerRecorder struct {
	mu      sync.Mutex
	payload []string
}

func (r *handlerRecorder) record(p string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.payload = append(r.payload, p)
}

func (r *handlerRecorder) snapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.payload...)
}

// TestSource_ThroughNewConsumer drives a fake-clock cron Source through
// msgin.NewConsumer[T] + Run — the shape every real caller uses — instead of
// draining Source.Stream by hand. Proves: NewConsumer resolves the Source via
// LiveValueSource (no codec needed), the handler receives the fired payload
// typed as T, and shutdown is goleak-clean.
func TestSource_ThroughNewConsumer(t *testing.T) {
	epoch := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	clk := clockwork.NewFakeClockAt(epoch)

	src, err := cron.NewSource("@every 1h",
		func(fire time.Time) string { return "tick@" + fire.UTC().Format("15:04") },
		cron.WithClock(clk))
	require.NoError(t, err)

	rec := &handlerRecorder{}
	h := func(_ context.Context, m msgin.Message[string]) error {
		rec.record(m.Payload())
		return nil
	}

	c, err := msgin.NewConsumer[string](src, h) // cron Source ⇒ LiveValueSource ⇒ no codec
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- c.Run(ctx) }()

	require.NoError(t, clk.BlockUntilContext(ctx, 1))
	clk.Advance(time.Hour)

	require.Eventually(t, func() bool { return len(rec.snapshot()) == 1 }, time.Second, 5*time.Millisecond)
	assert.Equal(t, []string{"tick@01:00"}, rec.snapshot())

	cancel()
	assert.ErrorIs(t, <-done, context.Canceled) // goleak: Run's goroutine joined before TestMain checks
}
```
Run: `GOTOOLCHAIN=go1.25.12 go test -race -run TestSource_ThroughNewConsumer ./adapter/cron/` → PASS. No implementation change is required — `Source[T]` already satisfies `StreamingSource`/`LiveValueSource` from Step 5 — this step exists purely to close the untested integration gap the audit flagged.

- [ ] **Step 7: Commit** (couples Plan 011 + ADR 0016 + ADR 0017 with the first code)

```bash
git add go.mod go.sum NOTICE adapter/cron/source.go adapter/cron/errors.go \
        adapter/cron/coordinator.go adapter/cron/source_test.go \
        adapter/cron/consumer_integration_test.go docs/plans/011-cron-source.md \
        docs/adrs/0016-robfig-cron-dependency.md docs/adrs/0017-cron-source.md
git commit -m "feat(cron): recurring Source[T] over robfig/cron + clockwork loop

adapter/cron.Source[T] is a StreamingSource/LiveValueSource emitting
factory(fireTime) on each cron/@every/descriptor fire, driven by a single
goleak-clean clockwork loop (no background goroutine, skip-missed on
overrun, at-most-once no-op Ack/Nack). robfig/cron/v3 accepted as the third
core dependency (ADR 0016; go mod graph confirms zero transitive deps).
Construction validates ErrInvalidSchedule (incl. unsatisfiable specs) and
ErrNilFactory; the Stream loop guards a zero Next against hot-spinning.
Elector/Locker interfaces declared (coordinator.go) so config compiles
standalone; wiring lands in the next commit. A NewConsumer[T] integration
test proves the runtime round-trips a fire to the handler's T.

Spec: 006
Plan: 011
ADR: 0016,0017"
```

---

### Task 2: `WithElector`/`WithLocker` gating + fail-safe skip + `ErrConflictingCoordinator` + `ErrLockerRequiresGridSchedule`

The `Elector`/`Locker` **interfaces** were declared in Task 1 (`coordinator.go`) so `source.go`'s `config` compiles standalone (Round-1 audit H-1). This task adds the *options* that let a caller supply an implementation, the real gate-wiring logic (replacing the Task-1 `wireGate` stub), and the construction-time refusal of a `Locker` paired with an `@every` schedule (Round-1 audit B-1 — the Locker's dedup key is not instance-invariant for `@every`, so the combination is refused rather than silently shipping a coordinator that dedups nothing).

**Files:**
- Modify: `adapter/cron/source.go` (add `WithElector`/`WithLocker` options + real `wireGate` body, incl. the `@every`+Locker refusal)
- Modify: `adapter/cron/errors.go` (add `ErrConflictingCoordinator`, `ErrLockerRequiresGridSchedule`)
- Test: `adapter/cron/coordinator_test.go` (blackbox `package cron_test`)

**Interfaces:**
- Consumes: `Elector`/`Locker` (Task 1, `coordinator.go`); `config.elector`/`config.locker`/`gate`/`(*Source[T]).scope`/`(*Source[T]).win` (Task 1); `robfig.ConstantDelaySchedule` (the `@every` schedule type, type-asserted to detect the refused combination).
- Produces:
  - `func WithElector(Elector) Option`; `func WithLocker(Locker) Option`
  - `var ErrConflictingCoordinator, ErrLockerRequiresGridSchedule error`
  - the real `(*Source[T]).wireGate` (replaces the Task-1 stub)

- [ ] **Step 1: Write the failing test** — `adapter/cron/coordinator_test.go`

```go
package cron_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jonboulle/clockwork"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kartaladev/msgin"
	"github.com/kartaladev/msgin/adapter/cron"
)

// fakeElector / fakeLocker are hand-written doubles (no external dep). They record
// the call and return a scripted verdict/error.
type fakeElector struct {
	leader    bool
	err       error
	calls     int
	lastScope string
}

func (f *fakeElector) IsLeader(_ context.Context, scope string) (bool, error) {
	f.calls++
	f.lastScope = scope
	return f.leader, f.err
}

type fakeLocker struct {
	won       bool
	err       error
	lastScope string
	lastFire  time.Time
	calls     int
}

func (f *fakeLocker) Claim(_ context.Context, scope string, fire time.Time) (bool, error) {
	f.calls++
	f.lastScope, f.lastFire = scope, fire
	return f.won, f.err
}

// drainOne advances the fake clock to the first fire and reports whether a
// delivery arrived within a short window (true) or the fire was gated out (false).
func drainOne(t *testing.T, clk *clockwork.FakeClock, out <-chan msgin.Delivery, ctx context.Context) (msgin.Delivery, bool) {
	t.Helper()
	require.NoError(t, clk.BlockUntilContext(ctx, 1))
	clk.Advance(time.Hour)
	select {
	case d := <-out:
		return d, true
	case <-time.After(300 * time.Millisecond):
		return msgin.Delivery{}, false
	}
}

func TestSource_Gating(t *testing.T) {
	type testCase struct {
		name   string
		opts   func(fe *fakeElector, fl *fakeLocker) []cron.Option
		fe     fakeElector
		fl     fakeLocker
		assert func(t *testing.T, emitted bool, fe *fakeElector, fl *fakeLocker)
	}
	cases := []testCase{
		{
			name: "elector leader emits, receiving the Source's scope",
			fe:   fakeElector{leader: true},
			opts: func(fe *fakeElector, _ *fakeLocker) []cron.Option {
				return []cron.Option{cron.WithElector(fe), cron.WithScope("job-x")}
			},
			assert: func(t *testing.T, emitted bool, fe *fakeElector, _ *fakeLocker) {
				assert.True(t, emitted)
				assert.Equal(t, 1, fe.calls)
				assert.Equal(t, "job-x", fe.lastScope, "IsLeader must receive the Source's scope (M-1 symmetry with Locker.Claim)")
			},
		},
		{
			name: "elector non-leader skips",
			fe:   fakeElector{leader: false},
			opts: func(fe *fakeElector, _ *fakeLocker) []cron.Option { return []cron.Option{cron.WithElector(fe)} },
			assert: func(t *testing.T, emitted bool, _ *fakeElector, _ *fakeLocker) { assert.False(t, emitted) },
		},
		{
			name: "elector error skips fail-safe",
			fe:   fakeElector{err: errors.New("db down")},
			opts: func(fe *fakeElector, _ *fakeLocker) []cron.Option { return []cron.Option{cron.WithElector(fe)} },
			assert: func(t *testing.T, emitted bool, _ *fakeElector, _ *fakeLocker) { assert.False(t, emitted) },
		},
		{
			name: "locker winner emits and receives scope+fire",
			fl:   fakeLocker{won: true},
			opts: func(_ *fakeElector, fl *fakeLocker) []cron.Option {
				return []cron.Option{cron.WithLocker(fl), cron.WithScope("job-x")}
			},
			assert: func(t *testing.T, emitted bool, _ *fakeElector, fl *fakeLocker) {
				assert.True(t, emitted)
				assert.Equal(t, "job-x", fl.lastScope)
				assert.False(t, fl.lastFire.IsZero())
			},
		},
		{
			name: "locker loser skips",
			fl:   fakeLocker{won: false},
			opts: func(_ *fakeElector, fl *fakeLocker) []cron.Option { return []cron.Option{cron.WithLocker(fl)} },
			assert: func(t *testing.T, emitted bool, _ *fakeElector, _ *fakeLocker) { assert.False(t, emitted) },
		},
		{
			name: "locker error skips fail-safe",
			fl:   fakeLocker{err: errors.New("db down")},
			opts: func(_ *fakeElector, fl *fakeLocker) []cron.Option { return []cron.Option{cron.WithLocker(fl)} },
			assert: func(t *testing.T, emitted bool, _ *fakeElector, _ *fakeLocker) { assert.False(t, emitted) },
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fe, fl := tc.fe, tc.fl
			clk := clockwork.NewFakeClockAt(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
			opts := append(tc.opts(&fe, &fl), cron.WithClock(clk))
			// "@hourly" (grid-aligned) rather than "@every 1h": the Locker cases in
			// this table require a grid-aligned schedule (ErrLockerRequiresGridSchedule
			// refuses @every, tested separately below); the Elector cases work
			// identically with either. At this epoch (an exact hour boundary) both
			// specs fire at the same instant, so drainOne's Advance(time.Hour) behaves
			// identically.
			src, err := cron.NewSource("@hourly", func(time.Time) int { return 1 }, opts...)
			require.NoError(t, err)

			out := make(chan msgin.Delivery)
			ctx, cancel := context.WithCancel(t.Context())
			done := make(chan error, 1)
			go func() { done <- src.Stream(ctx, out) }()

			_, emitted := drainOne(t, clk, out, ctx)
			tc.assert(t, emitted, &fe, &fl)

			cancel()
			<-done
		})
	}
}

func TestSource_ConflictingCoordinator(t *testing.T) {
	_, err := cron.NewSource("@every 1h", func(time.Time) int { return 1 },
		cron.WithElector(&fakeElector{}), cron.WithLocker(&fakeLocker{}))
	assert.ErrorIs(t, err, cron.ErrConflictingCoordinator)
}

// TestSource_DefaultScopeIsSpec proves the WithScope default: with no WithScope,
// the Locker receives the raw spec string as the scope. Uses a grid-aligned
// spec ("@hourly") — the Locker refuses @every (see
// TestSource_LockerRequiresGridSchedule below).
func TestSource_DefaultScopeIsSpec(t *testing.T) {
	fl := fakeLocker{won: true}
	clk := clockwork.NewFakeClockAt(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	src, err := cron.NewSource("@hourly", func(time.Time) int { return 1 },
		cron.WithLocker(&fl), cron.WithClock(clk))
	require.NoError(t, err)

	out := make(chan msgin.Delivery)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- src.Stream(ctx, out) }()
	_, _ = drainOne(t, clk, out, ctx)
	cancel()
	<-done

	assert.Equal(t, "@hourly", fl.lastScope)
}

// TestSource_LockerRequiresGridSchedule proves the Round-1 audit B-1 fix: a
// Locker paired with an "@every" schedule is refused at construction, because
// robfig's ConstantDelaySchedule.Next(t) = t+Delay is relative to each
// instance's own last-fire/start time — independent instances never converge
// on the same dedup key, so the Locker would silently dedup nothing. Cron and
// descriptor specs (grid-aligned) are unaffected — construct fine with a Locker.
func TestSource_LockerRequiresGridSchedule(t *testing.T) {
	type testCase struct {
		name   string
		spec   string
		assert func(t *testing.T, err error)
	}
	cases := []testCase{
		{
			name: "@every + Locker is refused",
			spec: "@every 1h",
			assert: func(t *testing.T, err error) {
				assert.ErrorIs(t, err, cron.ErrLockerRequiresGridSchedule)
			},
		},
		{
			name:   "5-field cron + Locker constructs fine",
			spec:   "0 * * * *",
			assert: func(t *testing.T, err error) { assert.NoError(t, err) },
		},
		{
			name:   "@hourly descriptor + Locker constructs fine",
			spec:   "@hourly",
			assert: func(t *testing.T, err error) { assert.NoError(t, err) },
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := cron.NewSource(tc.spec, func(time.Time) int { return 1 }, cron.WithLocker(&fakeLocker{}))
			tc.assert(t, err)
		})
	}
}
```

- [ ] **Step 2: Run the test to verify it fails** — `GOTOOLCHAIN=go1.25.12 go test -run 'TestSource_Gating|TestSource_ConflictingCoordinator|TestSource_DefaultScopeIsSpec|TestSource_LockerRequiresGridSchedule' ./adapter/cron/` → FAIL (undefined `cron.WithElector`/`WithLocker`/`ErrConflictingCoordinator`/`ErrLockerRequiresGridSchedule`).

- [ ] **Step 3a: Implement — `adapter/cron/errors.go`** (add the two sentinels)

```go
	// ErrConflictingCoordinator is returned by NewSource when BOTH WithElector and
	// WithLocker are configured. The two coordination strategies are mutually
	// exclusive — a fire is gated by leadership OR by per-fire claim, never both —
	// so configuring both is a caller mistake refused at construction.
	ErrConflictingCoordinator = errors.New("msgin/cron: at most one of WithElector/WithLocker may be set")

	// ErrLockerRequiresGridSchedule is returned by NewSource when a Locker is
	// configured against an "@every <duration>" schedule. The Locker's dedup key
	// (scope, fire_ts) is instance-invariant only for grid-aligned schedules
	// (5-field cron / @daily.../ descriptors) — robfig's ConstantDelaySchedule
	// (the "@every" implementation) computes the next fire relative to EACH
	// instance's own last-fire/start time, so independent instances never
	// converge on the same key and the Locker would silently dedup nothing
	// (Round-1 audit B-1). Use an Elector for "@every" schedules instead.
	// errors.Is-able.
	ErrLockerRequiresGridSchedule = errors.New("msgin/cron: a Locker requires a grid-aligned schedule (cron or descriptor); @every is unsupported — use an Elector instead")
```

- [ ] **Step 3b: Implement — `adapter/cron/source.go`** (add the two options; replace the Task-1 `wireGate` stub)

```go
// WithElector gates every fire behind leader election (see Elector): only the
// leader instance emits. Mutually exclusive with WithLocker (ErrConflictingCoordinator).
func WithElector(e Elector) Option {
	return func(o *config) { o.elector = e }
}

// WithLocker gates each fire behind a per-fire claim (see Locker): exactly one
// instance wins each (scope, fire) and emits. Mutually exclusive with WithElector
// (ErrConflictingCoordinator). Requires a GRID-ALIGNED schedule (5-field cron or
// a @daily/@hourly/... descriptor) — an "@every" schedule is refused with
// ErrLockerRequiresGridSchedule (Round-1 audit B-1; use WithElector for
// "@every"). Pair with WithScope when distinct jobs share a spec.
func WithLocker(l Locker) Option {
	return func(o *config) { o.locker = l }
}

// wireGate resolves the configured coordinator into s.gate: an Elector wraps
// IsLeader(ctx, scope) with the Source's own scope; a Locker wraps
// Claim(scope, fire); neither leaves s.gate nil (always emit). Both set is
// ErrConflictingCoordinator. A Locker paired with a ConstantDelaySchedule
// ("@every") is ErrLockerRequiresGridSchedule (checked BEFORE the delegation
// switch, alongside the conflicting-coordinator check, so both construction-time
// refusals live in one place).
func (s *Source[T]) wireGate(cfg *config) error {
	switch {
	case cfg.elector != nil && cfg.locker != nil:
		return ErrConflictingCoordinator
	case cfg.locker != nil:
		if _, isEvery := s.schedule.(robfig.ConstantDelaySchedule); isEvery {
			return ErrLockerRequiresGridSchedule
		}
	}
	switch {
	case cfg.elector != nil:
		e, scope := cfg.elector, s.scope
		s.gate = func(ctx context.Context, _ time.Time) (bool, error) { return e.IsLeader(ctx, scope) }
	case cfg.locker != nil:
		l, scope := cfg.locker, s.scope
		s.gate = func(ctx context.Context, fire time.Time) (bool, error) { return l.Claim(ctx, scope, fire) }
	}
	return nil
}
```

- [ ] **Step 4: Run the tests** — `GOTOOLCHAIN=go1.25.12 go test -race ./adapter/cron/` → PASS (Task 1 + Task 2 subtests, goleak-clean). `gofmt -l adapter/cron/` silent; `gopls check ./adapter/cron/` clean.

- [ ] **Step 5: Commit**

```bash
git add adapter/cron/source.go adapter/cron/errors.go adapter/cron/coordinator_test.go
git commit -m "feat(cron): WithElector/WithLocker gating + fail-safe skip

Wires the Task-1 Elector/Locker interfaces into the Source: WithElector/
WithLocker inject a coordinator, wireGate resolves it into the per-fire gate
(Elector.IsLeader(ctx, scope) / Locker.Claim(ctx, scope, fire), both scoped by
the Source's own WithScope), and a coordinator error is logged and the fire
skipped (never N-fold firing). Both set is ErrConflictingCoordinator. A
Locker paired with an @every schedule is refused at construction
(ErrLockerRequiresGridSchedule, Round-1 audit B-1) — the Locker's dedup key
is not instance-invariant for @every; use an Elector instead.

Spec: 006
Plan: 011
ADR: 0017"
```

---

### Task 3: SQL `Locker` — dedup-INSERT, `LockerDialect` (PG/MySQL/SQLite), DDL, `Purge`

Mirrors the proven `InboxDeduper` mechanism (ADR 0010 D10) keyed on the deterministic `(scope, fire_ts)` instead of `msg_id`. Every instance computes the same fire time from the schedule, so exactly one INSERT wins. This task also introduces the shared SQL micro-primitives (`sqlutil.go`, per **KD-1**) and the `LockerDialect` seam + DDL, used again by Task 4.

**Files:**
- Create: `adapter/cron/sqlutil.go` (the exported `Querier`, `identPattern`/`validateIdent`, `ErrInvalidTableName`, quote helpers)
- Create: `adapter/cron/sqllock.go` (`SQLLocker`, `NewSQLLocker`, `Claim`, `Purge`, `EnsureSchema`, `LockerOption`)
- Create/Modify: `adapter/cron/dialect.go` (`LockerDialect` interface + `PostgresLocker()`/`MySQLLocker()`/`SQLiteLocker()` + `PostgresLockerDDL`/`MySQLLockerDDL`/`SQLiteLockerDDL`)
- Modify: `adapter/cron/errors.go` (add `ErrNilDialect`, `ErrInvalidRetention`, `ErrLockerClaimFailed`; `ErrInvalidTableName` is declared in `sqlutil.go`)
- Test: `adapter/cron/sqllock_test.go` (blackbox `package cron_test`, driver-free Go-logic tests — DDL builders, validation, retention guard, and the dedup verdict via a hand-written fake `LockerDialect`)

**Interfaces:**
- Consumes: `msgin.ErrNilAdapter`; `Querier`/`validateIdent`/quote helpers (this task).
- Produces:
  - `type Querier interface { ExecContext(...); QueryContext(...); QueryRowContext(...) }` (exported — mirrors `msginsql.Querier`; it is legitimate dialect-author SPI, decided per KD-1, not an open question)
  - `type LockerDialect interface { ClaimFire(ctx, q Querier, table, scope string, fire time.Time) (won bool, err error); PurgeFired(ctx, q Querier, table string, olderThan time.Duration) (int64, error); EnsureFiredSchema(ctx, q Querier, table string) error }` *(corrected post-implementation: Task 3 review — EnsureSchema multi-statement split for pgx)*
  - `func PostgresLocker() LockerDialect`; `func MySQLLocker() LockerDialect`; `func SQLiteLocker() LockerDialect`
  - `func PostgresLockerDDL(table string) (string, error)` (+ MySQL/SQLite peers)
  - `type SQLLocker struct{…}`; `func NewSQLLocker(db *sql.DB, dialect LockerDialect, opts ...LockerOption) (*SQLLocker, error)`
  - `func (*SQLLocker) Claim(ctx, scope string, fire time.Time) (bool, error)` (implements `Locker`)
  - `func (*SQLLocker) Purge(ctx, olderThan time.Duration) (int64, error)`; `func (*SQLLocker) EnsureSchema(ctx) error`
  - `type LockerOption func(*lockerConfig)`; `func WithLockerTable(string) LockerOption` *(corrected post-implementation: Task 3 review pass 2 — `WithInstanceID`/claimed_by/holder/randomID removed from the Locker as YAGNI; the winner is decided solely by whose INSERT succeeds, nothing reads a per-fire claimant, and the Elector's `WithElectorInstanceID` (Task 4) already covers the one identity that is correctness-bearing)*
  - `var ErrNilDialect, ErrInvalidRetention, ErrLockerClaimFailed error`

- [ ] **Step 1: Write the failing test** — `adapter/cron/sqllock_test.go` (driver-free; a real-DB run is Task 5)

```go
package cron_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kartaladev/msgin/adapter/cron"
)

// fakeLockerDialect records ClaimFire/Purge calls and returns a scripted verdict,
// so the SQLLocker's non-SQL logic (validation, delegation, retention guard) is
// tested with no database.
type fakeLockerDialect struct {
	won        bool
	claimErr   error
	claimCalls int
	lastScope  string
	lastFire   time.Time
	purged     int64
	purgeErr   error
	lastOlder  time.Duration
}

func (f *fakeLockerDialect) ClaimFire(_ context.Context, _ cron.Querier, _ string, scope string, fire time.Time) (bool, error) {
	f.claimCalls++
	f.lastScope, f.lastFire = scope, fire
	return f.won, f.claimErr
}
func (f *fakeLockerDialect) PurgeFired(_ context.Context, _ cron.Querier, _ string, older time.Duration) (int64, error) {
	f.lastOlder = older
	return f.purged, f.purgeErr
}
func (f *fakeLockerDialect) EnsureFiredSchema(context.Context, cron.Querier, string) error { return nil }
```

> **`Querier` is exported (KD-1, decided).** `sqlutil.go` declares `Querier` as an exported interface (the subset of `*sql.DB`/`*sql.Tx` the dialects use), not an unexported `querier` — a blackbox `cron_test` package must be able to name the type to implement `LockerDialect`/`ElectorDialect` in a fake, exactly as `adapter/database/sql` exports its own `Querier` for the same reason. This is settled, not an open choice for the audit.

```go
func TestSQLLocker_Construction(t *testing.T) {
	type testCase struct {
		name   string
		build  func() (*cron.SQLLocker, error)
		assert func(t *testing.T, err error)
	}
	cases := []testCase{
		{
			name:   "nil db is ErrNilAdapter",
			build:  func() (*cron.SQLLocker, error) { return cron.NewSQLLocker(nil, cron.PostgresLocker()) },
			assert: func(t *testing.T, err error) { assert.ErrorIs(t, err, msginErrNilAdapter()) },
		},
		{
			name:   "nil dialect is ErrNilDialect",
			build:  func() (*cron.SQLLocker, error) { return cron.NewSQLLocker(stubDB(t), nil) },
			assert: func(t *testing.T, err error) { assert.ErrorIs(t, err, cron.ErrNilDialect) },
		},
		{
			name: "invalid table is ErrInvalidTableName",
			build: func() (*cron.SQLLocker, error) {
				return cron.NewSQLLocker(stubDB(t), cron.PostgresLocker(), cron.WithLockerTable("bad; drop"))
			},
			assert: func(t *testing.T, err error) { assert.ErrorIs(t, err, cron.ErrInvalidTableName) },
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tc.build()
			tc.assert(t, err)
		})
	}
}

func TestSQLLocker_ClaimDelegates(t *testing.T) {
	fd := &fakeLockerDialect{won: true}
	l, err := cron.NewSQLLocker(stubDB(t), fd)
	require.NoError(t, err)

	fire := time.Date(2026, 1, 1, 1, 0, 0, 0, time.UTC)
	won, err := l.Claim(t.Context(), "job-x", fire)
	require.NoError(t, err)
	assert.True(t, won)
	assert.Equal(t, "job-x", fd.lastScope)
	assert.Equal(t, fire, fd.lastFire)
	assert.Equal(t, 1, fd.claimCalls)
}

func TestSQLLocker_ClaimErrorPropagates(t *testing.T) {
	fd := &fakeLockerDialect{claimErr: cron.ErrLockerClaimFailed}
	l, err := cron.NewSQLLocker(stubDB(t), fd)
	require.NoError(t, err)
	_, err = l.Claim(t.Context(), "s", time.Now())
	assert.ErrorIs(t, err, cron.ErrLockerClaimFailed)
}

func TestSQLLocker_PurgeRejectsNonPositive(t *testing.T) {
	fd := &fakeLockerDialect{}
	l, err := cron.NewSQLLocker(stubDB(t), fd)
	require.NoError(t, err)
	for _, d := range []time.Duration{0, -time.Second} {
		_, err := l.Purge(t.Context(), d)
		assert.ErrorIs(t, err, cron.ErrInvalidRetention)
	}
	assert.Zero(t, fd.lastOlder, "Purge must reject before any dialect call")
}

func TestLockerDDL(t *testing.T) {
	type testCase struct {
		name   string
		build  func(table string) (string, error)
		expect []string // substrings that must appear
	}
	cases := []testCase{
		{
			name: "postgres composite PK + fired table", build: cron.PostgresLockerDDL,
			expect: []string{"CREATE TABLE IF NOT EXISTS", "PRIMARY KEY (scope, fire_ts)", "claimed_at"},
		},
		{
			name: "mysql inline index", build: cron.MySQLLockerDDL,
			expect: []string{"CREATE TABLE IF NOT EXISTS", "PRIMARY KEY (scope, fire_ts)"},
		},
		{
			name: "sqlite composite PK", build: cron.SQLiteLockerDDL,
			expect: []string{"CREATE TABLE IF NOT EXISTS", "PRIMARY KEY (scope, fire_ts)"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ddl, err := tc.build("msgin_cron_fired")
			require.NoError(t, err)
			for _, want := range tc.expect {
				assert.Contains(t, ddl, want)
			}
			// invalid identifier is rejected before any SQL is built.
			_, err = tc.build("bad; drop")
			assert.ErrorIs(t, err, cron.ErrInvalidTableName)
		})
	}
}
```

> The test references two tiny helpers — `stubDB(t) *sql.DB` (opens a non-connecting `*sql.DB`, e.g. `sql.OpenDB` over a stub connector, or mirror `adapter/database/sql`'s `openDB(t, fakeDriverName)` by registering a no-op driver in the test file) and `msginErrNilAdapter()` (returns `msgin.ErrNilAdapter`). Declare both once in `sqllock_test.go`. Because construction never queries, `stubDB` need not connect. **Verify the exact `openDB`/`fakeDriverName` pattern with `gopls` against `adapter/database/sql/*_test.go` and copy it** — it registers a stub `driver.Driver` so `sql.Open` succeeds without a real database. **Register the stub driver EXACTLY ONCE (Round-2 audit NEW-NIT-1):** `database/sql.Register(name, driver)` panics on a duplicate name, so put the `sql.Register(fakeDriverName, …)` in a single `init()` in one test file of `package cron_test` (as the sql adapter does) — never register it per-test or in multiple files, or the whole `cron_test` binary panics at package init.

- [ ] **Step 2: Run the test to verify it fails** — `GOTOOLCHAIN=go1.25.12 go test -run 'TestSQLLocker|TestLockerDDL' ./adapter/cron/` → FAIL (undefined symbols).

- [ ] **Step 3a: Implement — `adapter/cron/sqlutil.go`** (own primitives, KD-1)

```go
package cron

import (
	"context"
	stdsql "database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// Querier is the subset of *sql.DB and *sql.Tx the coordinator dialects use.
// Both *sql.DB and *sql.Tx satisfy it. It is the dialect-author SPI surface — a
// custom LockerDialect/ElectorDialect names it — mirroring adapter/database/sql's
// Querier, but declared here so adapter/cron carries no dependency on the queue
// adapter (Plan 011 KD-1).
type Querier interface {
	ExecContext(ctx context.Context, query string, args ...any) (stdsql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*stdsql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *stdsql.Row
}

// ErrInvalidTableName is returned when a table identifier fails validation
// against ^[A-Za-z_][A-Za-z0-9_]*$. The name cannot be a bound parameter, so it
// is validated and dialect-quoted before interpolation; an invalid identifier is
// refused up front (the sole injection guard). errors.Is-able.
var ErrInvalidTableName = errors.New("msgin/cron: invalid table name")

var identPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// validateIdent returns ErrInvalidTableName unless name matches identPattern. A
// dialect method or DDL builder MUST call it before quoting/interpolating a table
// name.
func validateIdent(name string) error {
	if !identPattern.MatchString(name) {
		return fmt.Errorf("%w: %q", ErrInvalidTableName, name)
	}
	return nil
}

// pgQuote / mysqlQuote / sqliteQuote double the engine's identifier quote char.
func pgQuote(n string) string     { return `"` + strings.ReplaceAll(n, `"`, `""`) + `"` }
func mysqlQuote(n string) string  { return "`" + strings.ReplaceAll(n, "`", "``") + "`" }
func sqliteQuote(n string) string { return `"` + strings.ReplaceAll(n, `"`, `""`) + `"` }

// quoteTable validates then quotes via q; used at the entry of every dialect
// method and DDL builder.
func quoteTable(q func(string) string, table string) (string, error) {
	if err := validateIdent(table); err != nil {
		return "", err
	}
	return q(table), nil
}

// nowMicrosSQLite is the SQLite DB-clock expression in epoch microseconds
// (mirrors adapter/database/sql/sqlite). It is constant within a single SQL
// statement/step, so two uses in one statement compare equal.
const nowMicrosSQLite = `CAST(unixepoch('now','subsec')*1000000 AS INTEGER)`
```

- [ ] **Step 3b: Implement — `adapter/cron/dialect.go`** (`LockerDialect` + the three impls + DDL). The dedup verdict per engine mirrors `InsertInboxIfAbsent` (report as verified against the reference files):

```go
package cron

import (
	"context"
	stdsql "database/sql"
	"errors"
	"fmt"
	"time"
)

// LockerDialect owns the per-engine SQL for the per-fire dedup Locker. It is the
// exported SPI a caller supplies to NewSQLLocker (built-ins PostgresLocker() /
// MySQLLocker() / SQLiteLocker()). Every method validates+quotes its table
// first, uses the DB server clock for claimed_at, and passes scope/fire as bound
// parameters. fire is the schedule's fire time — the dedup key alongside scope,
// and it is instance-invariant ONLY for grid-aligned schedules (standard cron /
// descriptors) under bounded clock skew (skew ≪ smallest inter-fire gap); it is
// NOT invariant for "@every" (NewSource refuses that combination outright,
// ErrLockerRequiresGridSchedule — see the Locker interface godoc).
//
// PRECONDITION: q must be an autocommitting handle (*sql.DB), never a *sql.Tx —
// each method's atomicity reasoning relies on a fresh per-statement snapshot.
type LockerDialect interface {
	// ClaimFire idempotently inserts (scope, fire_ts) and reports whether THIS
	// call inserted the row (won). A conflict (row already present) is
	// won=false. There is no recorded claimant identity — the winner is
	// decided solely by whose INSERT succeeds.
	ClaimFire(ctx context.Context, q Querier, table, scope string, fire time.Time) (won bool, err error)
	// PurgeFired deletes fired-keys rows whose claimed_at (DB clock) is older than
	// olderThan and returns the count removed.
	PurgeFired(ctx context.Context, q Querier, table string, olderThan time.Duration) (int64, error)
	// EnsureFiredSchema idempotently creates the fired-keys table (CREATE ... IF
	// NOT EXISTS). Opt-in; production provisions via the *LockerDDL builder.
	EnsureFiredSchema(ctx context.Context, q Querier, table string) error
}
```
*(corrected post-implementation: Task 3 review pass 2 — Locker claimant surface removed as YAGNI; `claimed_by`/`WithInstanceID` dropped.)*

```go
// --- PostgreSQL --------------------------------------------------------------

type postgresLocker struct{}

// PostgresLocker returns the PostgreSQL LockerDialect (also serves wire-compatible
// derivatives). Stateless; safe to share.
func PostgresLocker() LockerDialect { return postgresLocker{} }

func (postgresLocker) ClaimFire(ctx context.Context, q Querier, table, scope string, fire time.Time) (bool, error) {
	qt, err := quoteTable(pgQuote, table)
	if err != nil {
		return false, err
	}
	var returned string
	err = q.QueryRowContext(ctx,
		fmt.Sprintf(`INSERT INTO %s (scope, fire_ts, claimed_at) VALUES ($1, $2, now())
ON CONFLICT (scope, fire_ts) DO NOTHING
RETURNING scope`, qt),
		scope, fire.UTC(),
	).Scan(&returned)
	if errors.Is(err, stdsql.ErrNoRows) {
		return false, nil // conflict: another instance already claimed this fire
	}
	if err != nil {
		return false, err
	}
	return true, nil // inserted: this instance won the fire
}

func (postgresLocker) PurgeFired(ctx context.Context, q Querier, table string, olderThan time.Duration) (int64, error) {
	qt, err := quoteTable(pgQuote, table)
	if err != nil {
		return 0, err
	}
	res, err := q.ExecContext(ctx,
		fmt.Sprintf(`DELETE FROM %s WHERE claimed_at < now() - ($1 * interval '1 microsecond')`, qt),
		olderThan.Microseconds())
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// EnsureFiredSchema issues the CREATE TABLE then the CREATE INDEX as two
// separate ExecContext calls — never a single combined multi-statement Exec:
// pgx's extended protocol rejects multi-statement Exec (mirrors
// adapter/database/sql/postgres's EnsureSchema two-Exec split).
func (postgresLocker) EnsureFiredSchema(ctx context.Context, q Querier, table string) error {
	qt, err := quoteTable(pgQuote, table)
	if err != nil {
		return err
	}
	if _, err := q.ExecContext(ctx, postgresCreateFiredTable(qt)); err != nil {
		return err
	}
	qidx, _ := quoteTable(pgQuote, table+"_claimed_idx")
	_, err = q.ExecContext(ctx, postgresCreateFiredIndex(qt, qidx))
	return err
}

// postgresCreateFiredTable / postgresCreateFiredIndex build the two DDL
// statements separately (qt/qidx already quoted) so EnsureFiredSchema can
// issue them as two Execs; PostgresLockerDDL joins them for migration tooling.
func postgresCreateFiredTable(qt string) string {
	return fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
  scope      VARCHAR(255) NOT NULL,
  fire_ts    TIMESTAMPTZ  NOT NULL,
  claimed_at TIMESTAMPTZ  NOT NULL DEFAULT now(),
  PRIMARY KEY (scope, fire_ts)
)`, qt)
}

func postgresCreateFiredIndex(qt, qidx string) string {
	return fmt.Sprintf(`CREATE INDEX IF NOT EXISTS %s ON %s (claimed_at)`, qidx, qt)
}

// PostgresLockerDDL returns the reference CREATE TABLE (+ retention index) for
// the PG fired-keys table, as a single combined statement, for a migration
// tool. It validates table first.
func PostgresLockerDDL(table string) (string, error) {
	qt, err := quoteTable(pgQuote, table)
	if err != nil {
		return "", err
	}
	qidx, _ := quoteTable(pgQuote, table+"_claimed_idx")
	return postgresCreateFiredTable(qt) + ";\n" + postgresCreateFiredIndex(qt, qidx) + ";", nil
}
```
*(corrected post-implementation: Task 3 review — `EnsureFiredSchema` split into two `ExecContext` calls via `postgresCreateFiredTable`/`postgresCreateFiredIndex`, mirroring `adapter/database/sql/postgres`'s `EnsureSchema`; the combined-string `PostgresLockerDDL` is unchanged in output, now composed from the same two helpers.)*

> The **MySQL** `postgresLocker` peer (`mysqlLocker`) mirrors `mysql.InsertInboxIfAbsent` exactly (report §2): `INSERT IGNORE INTO %s (scope, fire_ts, claimed_at) VALUES (?, ?, UTC_TIMESTAMP(6))` (args `scope, fire.UTC()`) → `RowsAffected()==1` ⇒ won; `==0` ⇒ verify with `SELECT scope FROM %s WHERE scope=? AND fire_ts=? LOCK IN SHARE MODE` (NOT `FOR SHARE`, MariaDB-compat) → a row ⇒ lost (`false,nil`); `sql.ErrNoRows` ⇒ a demoted data error ⇒ `return false, fmt.Errorf("%w: scope %q fire %s …", ErrLockerClaimFailed, scope, fire)`. `PurgeFired`: `DELETE FROM %s WHERE claimed_at < DATE_SUB(UTC_TIMESTAMP(6), INTERVAL ? MICROSECOND)`. DDL (`MySQLLockerDDL`): inline index, `PRIMARY KEY (scope, fire_ts)`, `INDEX %[2]s (claimed_at)`, `DATETIME(6)` columns with `DEFAULT (UTC_TIMESTAMP(6))`, genuinely single statement (no trailing `;` join, verified) — `EnsureFiredSchema` issues it as ONE Exec, unlike PG/SQLite.
>
> The **SQLite** peer (`sqliteLocker`) mirrors `sqlite.InsertInboxIfAbsent`: `INSERT INTO %s (scope, fire_ts, claimed_at) VALUES (?, ?, <nowMicrosSQLite>) ON CONFLICT (scope, fire_ts) DO NOTHING RETURNING scope` (args `scope, fire.UTC().UnixMicro()`) → `sql.ErrNoRows` ⇒ lost; row ⇒ won. `fire_ts` is stored as INTEGER epoch micros. `PurgeFired`: `DELETE FROM %s WHERE claimed_at < <nowMicrosSQLite> - ?` with `olderThan.Microseconds()`. DDL (`SQLiteLockerDDL`): `scope TEXT NOT NULL, fire_ts INTEGER NOT NULL, claimed_at INTEGER NOT NULL DEFAULT (<nowMicrosSQLite>), PRIMARY KEY (scope, fire_ts)` + a separate `CREATE INDEX IF NOT EXISTS … (claimed_at)`, joined with `";\n"` for the combined `SQLiteLockerDDL`; `EnsureFiredSchema` issues the two as separate `ExecContext` calls via `sqliteCreateFiredTable`/`sqliteCreateFiredIndex` helpers, mirroring the Postgres split. *(corrected post-implementation: Task 3 review pass 2 — `claimed_by` column + `holder` bind param removed from both MySQL/SQLite as YAGNI, mirroring the Postgres and interface changes above; `EnsureFiredSchema` two-Exec split for driver parity with Postgres is unchanged.)*
>
> **fire_ts type/precision:** PG/MySQL store `fire.UTC()` (a `time.Time`, requires the driver's `parseTime=true` for MySQL — the crontest helper sets it); SQLite stores `fire.UTC().UnixMicro()`. Cron fires are second-aligned in v1, and — because `NewSource` refuses a Locker for `@every` (`ErrLockerRequiresGridSchedule`) — every `SQLLocker` caller's schedule is grid-aligned, so every instance computes the identical schedule grid instant under bounded clock skew and the composite key matches exactly across instances. (Do NOT round to the DB clock — the fire time is an app-computed deterministic key, unlike claimed_at.)

- [ ] **Step 3c: Implement — `adapter/cron/sqllock.go`**

```go
package cron

import (
	"context"
	stdsql "database/sql"
	"time"

	"github.com/kartaladev/msgin"
)

// defaultFiredTable is the per-fire dedup table used when WithLockerTable is
// unset — a sensible default (CLAUDE.md), so the common case needs no config.
const defaultFiredTable = "msgin_cron_fired"

type lockerConfig struct {
	table string
}

// LockerOption configures an SQLLocker.
type LockerOption func(*lockerConfig)

// WithLockerTable sets the fired-keys table name (default "msgin_cron_fired").
// An empty/invalid identifier is rejected by NewSQLLocker with ErrInvalidTableName.
func WithLockerTable(table string) LockerOption {
	return func(c *lockerConfig) { c.table = table }
}

// SQLLocker is the dependency-free, SQL-backed Locker (ADR 0017): each Claim
// idempotently inserts (scope, fire_ts); the inserter wins — there is no
// recorded claimant identity (removed as YAGNI: nothing reads it, and the
// winner is decided solely by whose INSERT succeeds). It reuses the proven
// InboxDeduper dedup mechanism keyed on the deterministic fire time. Recommended
// coordination primitive for GRID-ALIGNED schedules (standard cron / descriptors)
// — no failover gap. NewSource enforces this: a Locker paired with an "@every"
// schedule is refused at construction (ErrLockerRequiresGridSchedule) because
// the dedup key is not instance-invariant there — use an Elector instead.
// Starts no goroutine.
type SQLLocker struct {
	db      *stdsql.DB
	table   string
	dialect LockerDialect
}

var _ Locker = (*SQLLocker)(nil)

// NewSQLLocker builds an SQLLocker over db using dialect for the exact SQL (pass
// PostgresLocker()/MySQLLocker()/SQLiteLocker() or your own). Checked in order:
// a nil db is msgin.ErrNilAdapter; a nil dialect is ErrNilDialect; an invalid
// table is ErrInvalidTableName — all at construction.
// (corrected post-implementation: Task 3 review — priority reordered to
// db → dialect → table, matching the brief; was previously db → table → dialect.)
func NewSQLLocker(db *stdsql.DB, dialect LockerDialect, opts ...LockerOption) (*SQLLocker, error) {
	if db == nil {
		return nil, msgin.ErrNilAdapter
	}
	if dialect == nil {
		return nil, ErrNilDialect
	}
	cfg := lockerConfig{table: defaultFiredTable}
	for _, o := range opts {
		o(&cfg)
	}
	if err := validateIdent(cfg.table); err != nil {
		return nil, err
	}
	return &SQLLocker{db: db, table: cfg.table, dialect: dialect}, nil
}

// Claim implements Locker: it inserts (scope, fire) and reports whether this
// instance won the fire. Runs on the pool (no tx) — the insert is autonomous.
func (l *SQLLocker) Claim(ctx context.Context, scope string, fire time.Time) (bool, error) {
	return l.dialect.ClaimFire(ctx, l.db, l.table, scope, fire)
}

// Purge deletes fired-keys rows whose claimed_at (DB clock) is older than
// olderThan and returns the count. It is manual (no background goroutine) — the
// caller schedules it. A non-positive olderThan is ErrInvalidRetention (a
// zero/negative cutoff would delete rows for fires still being claimed by lagging
// instances). Size olderThan comfortably above your longest fire interval plus
// cross-instance clock skew.
func (l *SQLLocker) Purge(ctx context.Context, olderThan time.Duration) (int64, error) {
	if olderThan <= 0 {
		return 0, ErrInvalidRetention
	}
	return l.dialect.PurgeFired(ctx, l.db, l.table, olderThan)
}

// EnsureSchema idempotently creates the fired-keys table (opt-in; production uses
// the *LockerDDL reference builder instead — msgin never runs DDL implicitly).
func (l *SQLLocker) EnsureSchema(ctx context.Context) error {
	return l.dialect.EnsureFiredSchema(ctx, l.db, l.table)
}
```
*(corrected post-implementation: Task 3 review pass 2 — `WithInstanceID`, `lockerConfig.instanceID`, `SQLLocker.instanceID`, and the now-unused `randomID` helper all removed as YAGNI; see the "Produces" note above.)*

- [ ] **Step 3d: Implement — `adapter/cron/errors.go`** (add the three sentinels)

```go
	// ErrNilDialect is a construction error from NewSQLLocker/NewSQLElector when
	// the required dialect argument is nil (there is no driver auto-detect).
	ErrNilDialect = errors.New("msgin/cron: nil dialect")

	// ErrInvalidRetention is returned by SQLLocker.Purge when olderThan is
	// non-positive — a cutoff of now()-or-future would delete fired-keys rows for
	// fires still being claimed by lagging instances, re-opening the fire to a
	// double claim. Refused before any DB call. errors.Is-able.
	ErrInvalidRetention = errors.New("msgin/cron: retention (olderThan) must be > 0")

	// ErrLockerClaimFailed is returned by the MySQL LockerDialect when an
	// INSERT IGNORE affected no row AND a verifying SELECT finds none — INSERT
	// IGNORE demoted a genuine (non-duplicate) data error to a warning. The fire
	// is NOT treated as claimed-by-another (which would silently skip it on every
	// instance); the error surfaces so the Source skips this fire fail-safe and
	// logs it. Postgres/SQLite have no equivalent path (ON CONFLICT … RETURNING is
	// exact). errors.Is-able.
	ErrLockerClaimFailed = errors.New("msgin/cron: locker claim did not take effect and is not a conflict")
```

- [ ] **Step 4: Run the tests** — `GOTOOLCHAIN=go1.25.12 go test -race ./adapter/cron/` → PASS (Tasks 1–3, goleak-clean). `gofmt -l adapter/cron/` silent; `gopls check ./adapter/cron/` clean; `GOTOOLCHAIN=go1.25.12 go build ./...` OK; `GOTOOLCHAIN=go1.25.12 go mod tidy && git diff --exit-code go.mod go.sum` (no new dep beyond robfig). **Note (Round-1 audit H-2/H-3):** these driver-free tests exercise `SQLLocker`'s delegation/validation logic only — they do NOT execute `dialect.go`'s per-engine SQL bodies (`postgresLocker`/`mysqlLocker`/`sqliteLocker`'s `ClaimFire`/`PurgeFired`), including the MySQL demoted-error path that constructs `ErrLockerClaimFailed`. That coverage comes ONLY from Task 5's real-DB `crontest` conformance; do not report or treat `dialect.go` as covered from this task alone.

- [ ] **Step 5: Commit**

```bash
git add adapter/cron/sqlutil.go adapter/cron/dialect.go adapter/cron/sqllock.go \
        adapter/cron/errors.go adapter/cron/sqllock_test.go
git commit -m "feat(cron): SQL-backed Locker (per-fire dedup-INSERT)

NewSQLLocker + LockerDialect (Postgres/MySQL/SQLite) implement single-fire
across N replicas by idempotently inserting (scope, fire_ts): the inserter
wins, mirroring the InboxDeduper mechanism (PG/SQLite ON CONFLICT DO NOTHING
RETURNING; MySQL INSERT IGNORE + verifying SELECT → ErrLockerClaimFailed on a
demoted error). DB-clock claimed_at, opt-in EnsureSchema/*LockerDDL, Purge
with an ErrInvalidRetention guard. adapter/cron owns its Querier/validateIdent
primitives (KD-1). Driver-free unit tests; real-DB conformance in Task 5.

Spec: 006
Plan: 011
ADR: 0017"
```

---

### Task 4: SQL `Elector` — leader-lease atomic acquire-or-renew, `ElectorDialect` (PG/MySQL/SQLite), TTL

**The hardest correctness surface in the library (Spec R2, ADR 0017).** The audit scrutinizes this task. `IsLeader(ctx, scope)` runs an **atomic acquire-or-renew** against a single lease row `(scope PK, holder, expires_at)`: set `holder := instanceID, expires_at := db_now + leaseTTL` **iff** the row is absent OR held by self OR expired. It returns true iff self now holds it. On-demand — the per-fire call IS the renewal; no heartbeat goroutine. DB-server-clock throughout (skew-free). Failover latency ≤ leaseTTL (documented). **`scope` is now a per-call argument (Round-1 audit M-1), symmetric with `Locker.Claim`** — the original design baked a single scope at `NewSQLElector` construction (`WithElectorScope`); that option is REMOVED, replaced by threading the `Source`'s own `WithScope` value through on each call, exactly like the Locker. One `SQLElector` can therefore gate many independent schedules.

**Files:**
- Create: `adapter/cron/sqlelector.go` (`SQLElector`, `NewSQLElector`, `IsLeader(ctx, scope)`, `EnsureSchema`, `ElectorOption`)
- Modify: `adapter/cron/dialect.go` (add `ElectorDialect` + `PostgresElector()`/`MySQLElector()`/`SQLiteElector()` + `*ElectorDDL`)
- Modify: `adapter/cron/errors.go` (add `ErrInvalidLeaseTTL`, `ErrElectorAcquireFailed`)
- Test: `adapter/cron/sqlelector_test.go` (driver-free Go-logic: construction, TTL guard, delegation, verdict via a fake `ElectorDialect`)

**Interfaces:**
- Produces:
  - `type ElectorDialect interface { AcquireOrRenew(ctx, q Querier, table, scope, holder string, leaseTTL time.Duration) (isLeader bool, err error); EnsureLeaseSchema(ctx, q Querier, table string) error }` — unchanged; `AcquireOrRenew` already took `scope` per call, so no dialect-level change is needed for M-1, only the `SQLElector` wrapper (below).
  - `func PostgresElector() ElectorDialect` (+ MySQL/SQLite); `func PostgresElectorDDL(table string) (string, error)` (+ peers)
  - `type SQLElector struct{…}`; `func NewSQLElector(db *sql.DB, dialect ElectorDialect, opts ...ElectorOption) (*SQLElector, error)`
  - `func (*SQLElector) IsLeader(ctx context.Context, scope string) (bool, error)` (implements `Elector`, scope-parameterized per M-1); `func (*SQLElector) EnsureSchema(ctx) error`
  - `type ElectorOption func(*electorConfig)`; `WithElectorTable(string)`; `WithElectorInstanceID(string)` (the Locker has no peer option — its `WithInstanceID` was removed as YAGNI, Task 3 review pass 2 — `WithElectorInstanceID` is Elector-only, and correctness-bearing there since it decides lease `holder`); `WithLeaseTTL(time.Duration)`. **`WithElectorScope` does NOT exist** — scope moved to the `IsLeader` call argument (M-1); there is nothing to bake at construction.
  - `var ErrInvalidLeaseTTL, ErrElectorAcquireFailed error`

- [ ] **Step 1: Write the failing test** — `adapter/cron/sqlelector_test.go`

```go
package cron_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kartaladev/msgin/adapter/cron"
)

type fakeElectorDialect struct {
	leader      bool
	err         error
	calls       int
	lastScope   string
	lastHolder  string
	lastTTL     time.Duration
}

func (f *fakeElectorDialect) AcquireOrRenew(_ context.Context, _ cron.Querier, _, scope, holder string, ttl time.Duration) (bool, error) {
	f.calls++
	f.lastScope, f.lastHolder, f.lastTTL = scope, holder, ttl
	return f.leader, f.err
}
func (f *fakeElectorDialect) EnsureLeaseSchema(context.Context, cron.Querier, string) error { return nil }

func TestSQLElector_Construction(t *testing.T) {
	type testCase struct {
		name   string
		build  func() (*cron.SQLElector, error)
		assert func(t *testing.T, err error)
	}
	cases := []testCase{
		{
			name:   "nil db is ErrNilAdapter",
			build:  func() (*cron.SQLElector, error) { return cron.NewSQLElector(nil, cron.PostgresElector()) },
			assert: func(t *testing.T, err error) { assert.ErrorIs(t, err, msginErrNilAdapter()) },
		},
		{
			name:   "nil dialect is ErrNilDialect",
			build:  func() (*cron.SQLElector, error) { return cron.NewSQLElector(stubDB(t), nil) },
			assert: func(t *testing.T, err error) { assert.ErrorIs(t, err, cron.ErrNilDialect) },
		},
		{
			name: "invalid table is ErrInvalidTableName",
			build: func() (*cron.SQLElector, error) {
				return cron.NewSQLElector(stubDB(t), cron.PostgresElector(), cron.WithElectorTable("bad;"))
			},
			assert: func(t *testing.T, err error) { assert.ErrorIs(t, err, cron.ErrInvalidTableName) },
		},
		{
			name: "non-positive lease TTL is ErrInvalidLeaseTTL",
			build: func() (*cron.SQLElector, error) {
				return cron.NewSQLElector(stubDB(t), cron.PostgresElector(), cron.WithLeaseTTL(0))
			},
			assert: func(t *testing.T, err error) { assert.ErrorIs(t, err, cron.ErrInvalidLeaseTTL) },
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tc.build()
			tc.assert(t, err)
		})
	}
}

func TestSQLElector_IsLeaderDelegates(t *testing.T) {
	fd := &fakeElectorDialect{leader: true}
	e, err := cron.NewSQLElector(stubDB(t), fd,
		cron.WithElectorInstanceID("inst-1"), cron.WithLeaseTTL(45*time.Second))
	require.NoError(t, err)

	// scope is a per-call argument (Round-1 audit M-1), not baked at
	// construction — pass it explicitly, symmetric with SQLLocker.Claim.
	leader, err := e.IsLeader(t.Context(), "job-x")
	require.NoError(t, err)
	assert.True(t, leader)
	assert.Equal(t, "job-x", fd.lastScope)
	assert.Equal(t, "inst-1", fd.lastHolder)
	assert.Equal(t, 45*time.Second, fd.lastTTL)
}

func TestSQLElector_IsLeaderErrorPropagates(t *testing.T) {
	fd := &fakeElectorDialect{err: errors.New("db down")}
	e, err := cron.NewSQLElector(stubDB(t), fd)
	require.NoError(t, err)
	_, err = e.IsLeader(t.Context(), "s")
	assert.ErrorContains(t, err, "db down")
}

// TestSQLElector_DefaultTTL proves the 30s default reaches the dialect when
// WithLeaseTTL is unset.
func TestSQLElector_DefaultTTL(t *testing.T) {
	fd := &fakeElectorDialect{leader: true}
	e, err := cron.NewSQLElector(stubDB(t), fd)
	require.NoError(t, err)
	_, err = e.IsLeader(t.Context(), "s")
	require.NoError(t, err)
	assert.Equal(t, 30*time.Second, fd.lastTTL)
}

func TestElectorDDL(t *testing.T) {
	for _, build := range []func(string) (string, error){
		cron.PostgresElectorDDL, cron.MySQLElectorDDL, cron.SQLiteElectorDDL,
	} {
		ddl, err := build("msgin_cron_leader")
		require.NoError(t, err)
		assert.Contains(t, ddl, "CREATE TABLE IF NOT EXISTS")
		assert.Contains(t, ddl, "holder")
		assert.Contains(t, ddl, "expires_at")
		_, err = build("bad; drop")
		assert.ErrorIs(t, err, cron.ErrInvalidTableName)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails** — `GOTOOLCHAIN=go1.25.12 go test -run 'TestSQLElector|TestElectorDDL' ./adapter/cron/` → FAIL (undefined symbols).

- [ ] **Step 3a: Implement — `adapter/cron/dialect.go`** (add `ElectorDialect` + the three impls). **PostgreSQL / SQLite** use `INSERT … ON CONFLICT DO UPDATE … WHERE … RETURNING`; **MySQL** uses a conditional `UPDATE` → verifying `SELECT` → `INSERT IGNORE` sequence.

```go
// ElectorDialect owns the per-engine atomic acquire-or-renew for the leader-lease
// Elector. Built-ins: PostgresElector()/MySQLElector()/SQLiteElector(). All time
// math uses the DB server clock (skew-free).
//
// PRECONDITION: q must be an autocommitting handle (*sql.DB), never a *sql.Tx —
// the MySQL implementation's three-step sequence (Round-1 audit M-2) relies on
// each statement getting a fresh, independently-committed snapshot; under a
// single REPEATABLE-READ transaction the verifying read would see a stale
// snapshot and could misreport leadership.
type ElectorDialect interface {
	// AcquireOrRenew atomically sets holder=holder, expires_at=db_now+leaseTTL on
	// the lease row for scope IFF the row is absent, already held by holder, or
	// expired; it returns true iff holder now holds a valid lease.
	AcquireOrRenew(ctx context.Context, q Querier, table, scope, holder string, leaseTTL time.Duration) (isLeader bool, err error)
	// EnsureLeaseSchema idempotently creates the lease table (opt-in).
	EnsureLeaseSchema(ctx context.Context, q Querier, table string) error
}

// --- PostgreSQL --------------------------------------------------------------

type postgresElector struct{}

func PostgresElector() ElectorDialect { return postgresElector{} }

func (postgresElector) AcquireOrRenew(ctx context.Context, q Querier, table, scope, holder string, ttl time.Duration) (bool, error) {
	qt, err := quoteTable(pgQuote, table)
	if err != nil {
		return false, err
	}
	// now() is evaluated once per statement in Postgres, so EXCLUDED.expires_at
	// (from VALUES) and the WHERE's now() are consistent. The DO UPDATE fires only
	// when the row is held by self or expired; otherwise no row is returned.
	var gotHolder string
	err = q.QueryRowContext(ctx, fmt.Sprintf(`INSERT INTO %[1]s (scope, holder, expires_at)
VALUES ($1, $2, now() + ($3 * interval '1 microsecond'))
ON CONFLICT (scope) DO UPDATE SET holder = EXCLUDED.holder, expires_at = EXCLUDED.expires_at
WHERE %[1]s.holder = $2 OR %[1]s.expires_at < now()
RETURNING holder`, qt),
		scope, holder, ttl.Microseconds(),
	).Scan(&gotHolder)
	if errors.Is(err, stdsql.ErrNoRows) {
		return false, nil // held by another, not expired → not leader
	}
	if err != nil {
		return false, err
	}
	return gotHolder == holder, nil // RETURNING guarantees this, but assert defensively
}
```

> **SQLite peer (`sqliteElector`)** — identical shape, SQLite quoting and `<nowMicrosSQLite>` for the clock; `'now'` is constant within a step so the VALUES `expires_at` and the WHERE clock agree:
> ```sql
> INSERT INTO %[1]s (scope, holder, expires_at)
> VALUES (?, ?, <nowMicrosSQLite> + ?)
> ON CONFLICT (scope) DO UPDATE SET holder = excluded.holder, expires_at = excluded.expires_at
> WHERE %[1]s.holder = ? OR %[1]s.expires_at < <nowMicrosSQLite>
> RETURNING holder
> ```
> params: `scope, holder, ttl.Microseconds(), holder`; `sql.ErrNoRows` ⇒ not leader; else compare `gotHolder == holder`. (SQLite ≥ 3.35 supports `RETURNING`; modernc bundles a newer version.)

- [ ] **Step 3b: Implement the MySQL Elector (`mysqlElector`) — the intricate path.** MySQL lacks `ON CONFLICT … WHERE`, so acquire-or-renew is a three-step atomic-enough sequence. Each step is a single statement; the row's PK serializes concurrent writers.

```go
type mysqlElector struct{}

func MySQLElector() ElectorDialect { return mysqlElector{} }

func (mysqlElector) AcquireOrRenew(ctx context.Context, q Querier, table, scope, holder string, ttl time.Duration) (bool, error) {
	qt, err := quoteTable(mysqlQuote, table)
	if err != nil {
		return false, err
	}
	micros := ttl.Microseconds()

	// Step 1: conditional renew/takeover of an existing row.
	res, err := q.ExecContext(ctx, fmt.Sprintf(
		`UPDATE %s SET holder = ?, expires_at = DATE_ADD(UTC_TIMESTAMP(6), INTERVAL ? MICROSECOND)
WHERE scope = ? AND (holder = ? OR expires_at < UTC_TIMESTAMP(6))`, qt),
		holder, micros, scope, holder)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	if n >= 1 {
		return true, nil // renewed or took over an expired/self row → leader
	}

	// Step 2: n==0 — either the row is held by another (not expired), or absent,
	// or it is ours but the UPDATE was a no-op (identical values, same microsecond).
	// Verify the current committed state with a LOCKING read (LOCK IN SHARE MODE,
	// not FOR UPDATE — MariaDB-compatible, mirrors mysqlLocker.ClaimFire, Task 3)
	// so the verdict is robust even if q is ever handed a transaction, not only
	// the autocommit *sql.DB the ElectorDialect precondition requires (Round-1
	// audit M-2 — belt-and-suspenders on top of the documented precondition).
	var curHolder string
	var expired bool
	err = q.QueryRowContext(ctx, fmt.Sprintf(
		`SELECT holder, (expires_at < UTC_TIMESTAMP(6)) FROM %s WHERE scope = ? LOCK IN SHARE MODE`, qt), scope).
		Scan(&curHolder, &expired)
	switch {
	case errors.Is(err, stdsql.ErrNoRows):
		// Step 3: absent → try to acquire. INSERT IGNORE avoids driver-specific
		// dup-key detection: 1 row ⇒ we acquired; 0 rows ⇒ a concurrent instance
		// inserted first (or a demoted data error — distinguished below).
		res, err := q.ExecContext(ctx, fmt.Sprintf(
			`INSERT IGNORE INTO %s (scope, holder, expires_at)
VALUES (?, ?, DATE_ADD(UTC_TIMESTAMP(6), INTERVAL ? MICROSECOND))`, qt),
			scope, holder, micros)
		if err != nil {
			return false, err
		}
		ins, err := res.RowsAffected()
		if err != nil {
			return false, err
		}
		if ins == 1 {
			return true, nil // we won the insert → leader
		}
		// ins==0: a concurrent insert won, OR INSERT IGNORE demoted a real error.
		// Re-check (locking read, same rationale as Step 2's verify): a row now
		// present with another holder ⇒ raced-loss (fail-safe, not leader); still
		// absent ⇒ a demoted data error ⇒ surface it.
		err = q.QueryRowContext(ctx, fmt.Sprintf(`SELECT holder FROM %s WHERE scope = ? LOCK IN SHARE MODE`, qt), scope).
			Scan(&curHolder)
		if errors.Is(err, stdsql.ErrNoRows) {
			return false, fmt.Errorf("%w: scope %q (INSERT IGNORE affected no row and none exists)",
				ErrElectorAcquireFailed, scope)
		}
		if err != nil {
			return false, err
		}
		return curHolder == holder, nil // extremely unlikely holder==self here; false otherwise
	case err != nil:
		return false, err
	default:
		// Row present. It is ours and valid ⇒ leader (the UPDATE was a no-op);
		// otherwise held by another and not expired ⇒ not leader.
		return curHolder == holder && !expired, nil
	}
}
```

> **Why this is correct under concurrency — PRECONDITION: autocommit (Round-1 audit M-2).** This reasoning holds **only when `q` is an autocommitting handle (`*sql.DB`)**, never a single `*sql.Tx`: each step must observe the latest COMMITTED state, not a transaction's fixed snapshot. `SQLElector.IsLeader` always passes `e.db` (the pool), so the precondition holds in-tree; it is documented on `ElectorDialect.AcquireOrRenew` because `Querier` is exported dialect-author SPI and a custom dialect/caller could otherwise pass a `*sql.Tx` and silently break the argument below. **Two hazards, not one (Round-2 audit NEW-LOW-1):** violating the precondition breaks (a) snapshot freshness — a single REPEATABLE-READ tx pins the Step-1 snapshot so Step-2/3's verify reads stale, misfiring the verdict — AND (b) it courts an InnoDB **gap-lock deadlock**: on an absent row the `SELECT … LOCK IN SHARE MODE` takes a shared gap lock, and two such transactions then both `INSERT`, whose insert-intention locks conflict with the other's gap lock → error 1213. Autocommit dissolves both (each statement's locks release at statement end). State BOTH reasons on the `AcquireOrRenew` godoc so a custom-dialect author sees the full cost of passing a `*sql.Tx`. Given autocommit: the lease row's `scope` PK serializes the UPDATE and the INSERT across instances. Step 1 wins for the incumbent leader (holder=self) and for any instance when the lease is expired — and because the `WHERE … expires_at < UTC_TIMESTAMP(6)` predicate is evaluated under the row lock the UPDATE takes, **at most one** expired-takeover UPDATE succeeds (a second concurrent UPDATE re-reads the now-renewed row, sees holder≠self and not expired, matches nothing, `n==0`). Step 3's `INSERT IGNORE` resolves the absent-row race to exactly one winner. Both verifying SELECTs now use `LOCK IN SHARE MODE` (matching `mysqlLocker.ClaimFire`, Task 3) so the verdict is robust even under an accidental transaction, on top of the documented autocommit precondition. The verifying reads distinguish a raced-loss (fail-safe → not leader) from a demoted data error (`ErrElectorAcquireFailed` → the Source skips the fire fail-safe and logs). **This is the audit's focus — the conformance test in Task 5 exercises every branch on a real MySQL AND MariaDB.**

- [ ] **Step 3c: DDL builders** — `PostgresElectorDDL`/`MySQLElectorDDL`/`SQLiteElectorDDL`, validate-first. Lease table `(scope PK, holder NOT NULL, expires_at NOT NULL)`:
  - **PG:** `scope VARCHAR(255) PRIMARY KEY, holder VARCHAR(255) NOT NULL, expires_at TIMESTAMPTZ NOT NULL` (single CREATE TABLE IF NOT EXISTS; no secondary index needed — all access is by PK).
  - **MySQL:** same with `DATETIME(6)`, single statement.
  - **SQLite:** `scope TEXT PRIMARY KEY, holder TEXT NOT NULL, expires_at INTEGER NOT NULL`.
  `EnsureLeaseSchema` executes the corresponding `*ElectorDDL` (like `EnsureFiredSchema`).

- [ ] **Step 3d: Implement — `adapter/cron/sqlelector.go`**

```go
package cron

import (
	"context"
	stdsql "database/sql"
	"time"

	"github.com/kartaladev/msgin"
)

const (
	defaultLeaseTable = "msgin_cron_leader"
	defaultLeaseTTL   = 30 * time.Second
)

type electorConfig struct {
	table      string
	instanceID string
	leaseTTL   time.Duration
	ttlSet     bool
}

// ElectorOption configures an SQLElector.
type ElectorOption func(*electorConfig)

// WithElectorTable sets the lease table (default "msgin_cron_leader").
func WithElectorTable(table string) ElectorOption { return func(c *electorConfig) { c.table = table } }

// WithElectorInstanceID sets this instance's holder identity (default a
// per-process crypto-random id). Two instances MUST NOT share an id. This is
// Elector-only — the Locker carries no claimant identity (its WithInstanceID
// was removed as YAGNI, Task 3 review pass 2); the Elector's holder is
// correctness-bearing (it decides lease ownership), unlike the Locker's
// removed observability-only claimant.
func WithElectorInstanceID(id string) ElectorOption {
	return func(c *electorConfig) {
		if id != "" {
			c.instanceID = id
		}
	}
}

// WithLeaseTTL sets how long an acquired lease is valid before another instance
// may take over. Default 30s. Because renewal is on-demand (each IsLeader call is
// the renewal), single-fire holds at any TTL, but the TTL bounds the crash
// failover gap: after a leader crash, fires within [crash, lease-expiry] are
// missed by everyone. Smaller = faster failover, more re-election churn. This
// does NOT make leadership unconditionally "sticky": with an on-demand renewal
// and a TTL shorter than the fire interval, the lease has always expired by the
// next fire, so every call is a fresh election (single-fire correctness still
// holds — see the Elector interface godoc). Prefer the SQLLocker for
// grid-aligned schedules where the failover gap matters; the Elector is the
// coordinator for "@every" schedules (the Locker refuses that combination). A
// non-positive d is ErrInvalidLeaseTTL (a caller mistake, not a request for the
// default).
func WithLeaseTTL(d time.Duration) ElectorOption {
	return func(c *electorConfig) {
		c.leaseTTL = d
		c.ttlSet = true
	}
}

// SQLElector is the dependency-free, SQL-backed Elector (ADR 0017): IsLeader
// runs an atomic acquire-or-renew of a single lease row, scoped per call (Round-1
// audit M-1 — symmetric with SQLLocker.Claim; there is no scope baked at
// construction, so one SQLElector can gate many independent schedules). On-demand
// (no heartbeat goroutine). DB-server-clock throughout. Failover latency ≤
// WithLeaseTTL.
type SQLElector struct {
	db         *stdsql.DB
	table      string
	instanceID string
	leaseTTL   time.Duration
	dialect    ElectorDialect
}

var _ Elector = (*SQLElector)(nil)

// NewSQLElector builds an SQLElector over db using dialect (PostgresElector()/
// MySQLElector()/SQLiteElector() or your own). A nil db is msgin.ErrNilAdapter; an
// invalid table is ErrInvalidTableName; a non-positive WithLeaseTTL is
// ErrInvalidLeaseTTL; a nil dialect is ErrNilDialect — checked in that order.
func NewSQLElector(db *stdsql.DB, dialect ElectorDialect, opts ...ElectorOption) (*SQLElector, error) {
	if db == nil {
		return nil, msgin.ErrNilAdapter
	}
	cfg := electorConfig{
		table:      defaultLeaseTable,
		instanceID: randomID(),
		leaseTTL:   defaultLeaseTTL,
	}
	for _, o := range opts {
		o(&cfg)
	}
	if err := validateIdent(cfg.table); err != nil {
		return nil, err
	}
	if cfg.ttlSet && cfg.leaseTTL <= 0 {
		return nil, ErrInvalidLeaseTTL
	}
	if dialect == nil {
		return nil, ErrNilDialect
	}
	return &SQLElector{
		db: db, table: cfg.table,
		instanceID: cfg.instanceID, leaseTTL: cfg.leaseTTL, dialect: dialect,
	}, nil
}

// IsLeader implements Elector: it runs an atomic acquire-or-renew for scope and
// reports whether this instance now holds a valid lease for it. scope is the
// leadership domain (per call, not baked at construction — Round-1 audit M-1);
// the Source passes its own WithScope value.
func (e *SQLElector) IsLeader(ctx context.Context, scope string) (bool, error) {
	return e.dialect.AcquireOrRenew(ctx, e.db, e.table, scope, e.instanceID, e.leaseTTL)
}

// EnsureSchema idempotently creates the lease table (opt-in).
func (e *SQLElector) EnsureSchema(ctx context.Context) error {
	return e.dialect.EnsureLeaseSchema(ctx, e.db, e.table)
}
```

- [ ] **Step 3e: Implement — `adapter/cron/errors.go`** (add two sentinels)

```go
	// ErrInvalidLeaseTTL is a construction error from NewSQLElector when
	// WithLeaseTTL is given a non-positive duration. Unset leaves the 30s default;
	// an explicit non-positive value is a caller mistake, not a request for the
	// default. errors.Is-able.
	ErrInvalidLeaseTTL = errors.New("msgin/cron: lease TTL must be > 0")

	// ErrElectorAcquireFailed is returned by the MySQL ElectorDialect when an
	// INSERT IGNORE of an absent lease row affected no row AND a verifying SELECT
	// finds none — INSERT IGNORE demoted a genuine data error. Leadership is NOT
	// silently granted or denied on a corrupt write; the error surfaces so the
	// Source skips the fire fail-safe. errors.Is-able.
	ErrElectorAcquireFailed = errors.New("msgin/cron: elector acquire did not take effect")
```

- [ ] **Step 4: Run the tests** — `GOTOOLCHAIN=go1.25.12 go test -race ./adapter/cron/` → PASS (Tasks 1–4, goleak-clean). `gofmt -l adapter/cron/` silent; `gopls check ./adapter/cron/` clean; coverage check: `GOTOOLCHAIN=go1.25.12 go test -coverprofile=/tmp/cover11.out ./adapter/cron/ && GOTOOLCHAIN=go1.25.12 go tool cover -func=/tmp/cover11.out | tail -1`. **Note (Round-1 audit H-2/H-3):** the dialect *SQL* branches — including `mysqlElector.AcquireOrRenew`'s three-step sequence and the `ErrElectorAcquireFailed` demoted-error path — are executed ONLY by Task 5's real-DB `crontest` conformance; the driver-free tests here exercise `SQLElector`'s delegation/validation logic only and never call the real `dialect.go` bodies. Do NOT report or treat `dialect.go` as covered from this task's driver-free run — a Docker-less local run is not gate-satisfying for it (see the Global Constraints coverage bullet). Task 5 is the only real coverage for these branches.

- [ ] **Step 5: Commit**

```bash
git add adapter/cron/sqlelector.go adapter/cron/dialect.go adapter/cron/errors.go \
        adapter/cron/sqlelector_test.go
git commit -m "feat(cron): SQL-backed Elector (leader-lease acquire-or-renew)

NewSQLElector + ElectorDialect (Postgres/MySQL/SQLite): IsLeader(ctx, scope)
runs an atomic acquire-or-renew of a single (scope, holder, expires_at) lease
row on-demand (no heartbeat goroutine), DB-clock throughout, failover ≤ lease
TTL (default 30s, WithLeaseTTL). scope is a per-call argument, symmetric with
SQLLocker.Claim (Round-1 audit M-1) -- WithElectorScope does not exist. PG/
SQLite use ON CONFLICT DO UPDATE ... WHERE ... RETURNING; MySQL uses
conditional UPDATE -> locking verifying SELECT -> INSERT IGNORE (->
ErrElectorAcquireFailed on a demoted error); both dialects require an
autocommitting Querier (audit M-2). ErrInvalidLeaseTTL guard. Driver-free
unit tests; real-DB single-leader conformance in Task 5.

Spec: 006
Plan: 011
ADR: 0017"
```

---

### Task 5: `adapter/cron/crontest` leaf module — real PG/MySQL/MariaDB/SQLite conformance for both coordinators

Keeps the root and leaf-dialect modules driver/testcontainer-free (Plan 006 acceptance gate) by putting all real-DB tests in a dedicated leaf module, mirroring `adapter/database/sql/dbtest`. This is where the SQL verdict/acquire-or-renew branches get their real coverage, including the **concurrency** properties (exactly-one-winner, single-leader) that a mock cannot prove.

**Files:**
- Create: `adapter/cron/crontest/go.mod`, `adapter/cron/crontest/go.sum`
- Create: `adapter/cron/crontest/containers_test.go` (container helpers — copy/adapt `dbtest`'s)
- Create: `adapter/cron/crontest/locker_conformance_test.go`, `adapter/cron/crontest/elector_conformance_test.go`
- Modify: `go.work` (add `./adapter/cron/crontest`)

**Interfaces:**
- Consumes: `cron.NewSQLLocker`/`NewSQLElector` + the six dialect constructors + `EnsureSchema` + `Claim`/`IsLeader`/`Purge` (Tasks 3–4); real drivers + testcontainers.

- [ ] **Step 1: Create the module** — `adapter/cron/crontest/go.mod` (mirror `dbtest/go.mod`; `crontest`→root is **three** levels up):

```
module github.com/kartaladev/msgin/adapter/cron/crontest

go 1.25.0

require (
	github.com/go-sql-driver/mysql v1.10.0
	github.com/jackc/pgx/v5 v5.10.0
	github.com/kartaladev/msgin v0.0.0
	github.com/stretchr/testify v1.11.1
	github.com/testcontainers/testcontainers-go v0.43.0
	github.com/testcontainers/testcontainers-go/modules/mariadb v0.43.0
	github.com/testcontainers/testcontainers-go/modules/mysql v0.43.0
	github.com/testcontainers/testcontainers-go/modules/postgres v0.43.0
	go.uber.org/goleak v1.3.0
	modernc.org/sqlite v1.54.0
)

replace github.com/kartaladev/msgin => ../../..
```
(Run `GOTOOLCHAIN=go1.25.12 go mod tidy` inside `adapter/cron/crontest` to populate `go.sum` + the indirect block. It does NOT need the leaf sql-dialect modules — the cron coordinator dialects live in the root module.)

- [ ] **Step 2: Add the module to `go.work`** — append `./adapter/cron/crontest` to the `use (…)` list.

- [ ] **Step 3: Container helpers** — `adapter/cron/crontest/containers_test.go` (`package crontest_test`). Copy the four helpers from `adapter/database/sql/dbtest/testutils_test.go` verbatim (verify with `gopls`/`Read`), keeping their exact images/drivers/waits:
  - `RunTestDatabase(t, opts...) *sql.DB` — PostgreSQL, `postgres:16.10-alpine`, driver `pgx` (blank-import `_ "github.com/jackc/pgx/v5/stdlib"`), `ConnectionString(ctx, "sslmode=disable")`.
  - `RunTestMySQL(t, opts...) *sql.DB` — `mysql:8.0.40`, driver `mysql`, `ConnectionString(ctx, "parseTime=true")` (**parseTime is required** — the Locker stores `fire_ts` as a `time.Time`).
  - `RunTestMariaDB(t, opts...) *sql.DB` — `mariadb:11.4`, driver `mysql`, same DSN.
  - `RunTestSQLite(t, ...) *sql.DB` — no container; `sql.Open("sqlite", <tmp DSN>)`. **The DSN MUST set `_pragma=busy_timeout(5000)` plus WAL** (Round-1 audit M-3): without it, modernc's SQLite serializes writers and N concurrent write connections (the Locker's exactly-one-winner and the Elector's single-leader conformance both use N=8 concurrent writers) return `SQLITE_BUSY` instead of blocking, flaking the concurrency assertions. `crontest` has no dependency on the `adapter/database/sql/sqlite` leaf module (Global Constraints: no leaf-dialect deps in `crontest` beyond the root), so **replicate the DSN construction inline** rather than importing `sqlite.DSN` — e.g. `fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)", path)` (verify the exact pragma string against `adapter/database/sql/sqlite/dsn.go` with `gopls`/`Read` and mirror it). Blank-import `_ "modernc.org/sqlite"`.
  - `TestMain(m)` = `goleak.VerifyTestMain(m, <the four dbtest ignore options>)` (Ryuk reaper + net/http persistConn read/write loops + `internal/poll.runtime_pollWait`).

  > Add a small `dialectFor(driver)`-style table in the test so each engine pairs its `*sql.DB` with the matching `cron.PostgresLocker()/MySQLLocker()/SQLiteLocker()` and `…Elector()`. MariaDB pairs with the MySQL dialect (wire-compatible), exactly as `dbtest` does.

- [ ] **Step 4: Locker conformance** — `adapter/cron/crontest/locker_conformance_test.go`. For each engine, a table-driven suite asserting:

```go
// Pseudocode shape — implement per engine via a shared runLockerConformance(t, db, dialect).
// 1. EnsureSchema succeeds; a second EnsureSchema is idempotent (no error).
// 2. Exactly-one-winner: N=8 goroutines Claim(ctx, "job", sameFire) concurrently;
//    exactly ONE returns won=true, the rest won=false, no error. (errgroup + a
//    counter; the fire time is a fixed time.Date value all goroutines share.)
// 3. Different fire ⇒ both win: Claim("job", fireA) and Claim("job", fireB) each won=true.
// 4. Different scope, same fire ⇒ both win: Claim("jobA", fire) and Claim("jobB", fire) each won.
// 5. Purge: after a Claim, Purge(ctx, tiny) with a claimed_at older-than that has
//    elapsed removes the row (assert count via a follow-up Claim(sameFire) winning
//    again); Purge(ctx, 0) is ErrInvalidRetention.
```
Requirements: use `t.Context()`; drive concurrency with `golang.org/x/sync/errgroup` or a `sync.WaitGroup` + atomic counter; the shared fire is a fixed `time.Date(2026, 1, 2, 3, 0, 0, 0, time.UTC)` so all goroutines compute the identical key. **Every dialect runs the same `runLockerConformance` body** (PG, MySQL, MariaDB, SQLite), so the won/lost verdict branches are exercised on real SQL.

**Step 4b (Round-1 audit H-2): deterministically drive the MySQL/MariaDB demoted-error branch on REAL MySQL and MariaDB — do NOT claim the driver-free fake covers `ErrLockerClaimFailed`; it cannot, because the fake replaces `mysqlLocker.ClaimFire` outright.** Add `TestMySQLLocker_ClaimDemotedError` (and the MariaDB peer, same body against `RunTestMariaDB`) in `locker_conformance_test.go`:
```go
// Point a SQLLocker at a table carrying a CHECK constraint the Locker's INSERT
// necessarily violates: a `guard INT NOT NULL DEFAULT 0 CHECK (guard = 1)`
// column the INSERT never sets, so the row defaults guard=0 and fails CHECK.
// Under INSERT IGNORE (strict mode) a CHECK violation is demoted to a warning
// and NO row is inserted (RowsAffected()==0) — exactly the condition
// ClaimFire's demoted-error branch exists to catch. Because no row was
// inserted, the verifying SELECT ... LOCK IN SHARE MODE finds nothing, so
// ClaimFire falls through to fmt.Errorf(ErrLockerClaimFailed, ...). Assert
// errors.Is(err, cron.ErrLockerClaimFailed).
//
// CREATE TABLE msgin_cron_fired_malformed (
//   scope VARCHAR(255) NOT NULL, fire_ts DATETIME(6) NOT NULL,
//   claimed_at DATETIME(6) NOT NULL DEFAULT (UTC_TIMESTAMP(6)),
//   guard INT NOT NULL DEFAULT 0 CHECK (guard = 1),  -- INSERT never sets guard -> 0 -> CHECK fails
//   PRIMARY KEY (scope, fire_ts)
// );
```
> **Round-2 audit (NEW-HIGH-1) — the provocation MUST be a CHECK violation, not an omitted NOT-NULL column.** An extra `NOT NULL` column with no default is NOT discarded by `INSERT IGNORE`: in strict mode it is inserted with the column's *implicit* default (empty string for `VARCHAR`), affecting **1** row — empirically verified on MySQL 8.0.40 and MariaDB 11.4 (`ROW_COUNT()=1`, row present) — which takes the `won` path and never reaches the demoted-error branch (the test would go RED). A row-level `CHECK (guard = 1)` that the default `guard=0` violates was verified on both engines to yield `ROW_COUNT()=0` with **no** persisted row, deterministically reaching the branch. **Version floor:** `CHECK` is enforced on **MySQL ≥ 8.0.16** and **MariaDB ≥ 10.2.1**; both pinned images (`mysql:8.0.40`, `mariadb:11.4`) qualify.

This is deterministic (no timing/race dependency) and exercises the REAL `mysqlLocker.ClaimFire` demoted-error path end to end, closing the coverage gap the driver-free unit test cannot reach. This test — and its Elector peer (Step 5b) — improve on `adapter/database/sql`, whose analogous `ErrInboxInsertFailed` demoted-error branch is untested.

- [ ] **Step 5: Elector conformance** — `adapter/cron/crontest/elector_conformance_test.go`. For each engine, a shared `runElectorConformance(t, db, dialect)`. All `IsLeader` calls pass the SAME scope string (scope is a per-call argument, Round-1 audit M-1 — there is no `WithElectorScope` to set at construction anymore):

```go
// 1. EnsureSchema idempotent.
// 2. Single-leader: N=8 instances (distinct WithElectorInstanceID) call
//    IsLeader(ctx, "job-x") concurrently on the SAME scope with a long TTL;
//    exactly ONE is leader.
// 3. Renewal within TTL: the winner calls IsLeader(ctx, "job-x") again (before
//    the lease expires) → still leader; a different instance calls
//    IsLeader(ctx, "job-x") → not leader. (NOT unconditionally "sticky" — this
//    only holds because the call happens within the TTL window; see the
//    WithLeaseTTL godoc, Round-1 audit L-3.)
// 4. Failover after expiry: build the Elector(s) with a SHORT TTL (e.g. 200ms);
//    the leader stops calling; require.Eventually a DIFFERENT instance's
//    IsLeader(ctx, "job-x") returns true once the lease has expired (real
//    wall-clock wait — the lease uses the DB clock, so a real sleep past the
//    TTL is needed; this is the one place a real delay is legitimate, kept
//    ≤ ~1s).
// 5. Distinct scopes are independent: IsLeader(ctx, "job-a") and
//    IsLeader(ctx, "job-b") from the SAME SQLElector instance are gated
//    independently (both can report leader=true for the same instance,
//    proving one SQLElector now gates many schedules — Round-1 audit M-1).
// 6. DB-clock proof: no app clock is injected anywhere in the Elector path
//    (assert by construction — SQLElector takes no clock).
```
This is the **highest-value test in the plan** — it is the only place the MySQL three-step acquire-or-renew and the PG/SQLite `ON CONFLICT … WHERE … RETURNING` are proven correct under real concurrency on MySQL **and** MariaDB (the two engines the intricate MySQL path must both satisfy). Keep TTLs short but comfortably above container round-trip jitter for the failover case; use `require.Eventually` (return `false` on mismatch inside its closure — no `require.*` in the spawned goroutine, per the F3 caveat noted in Plan 010).

- [ ] **Step 5b (Round-1 audit H-2): deterministically drive `ErrElectorAcquireFailed` on REAL MySQL and MariaDB.** Same rationale as Step 4b: the driver-free unit test's fake replaces `mysqlElector.AcquireOrRenew` outright and cannot exercise the demoted-error path. Add `TestMySQLElector_AcquireDemotedError` (+ MariaDB peer) in `elector_conformance_test.go`:
```go
// Point a SQLElector at a lease table carrying a CHECK the Elector's INSERT
// necessarily violates: `guard INT NOT NULL DEFAULT 0 CHECK (guard = 1)`, never
// set by the INSERT. With no existing row for the scope, Step 1's UPDATE affects
// 0 rows, Step 2's locking SELECT finds none (ErrNoRows), and Step 3's INSERT
// IGNORE demotes the CHECK violation to a warning inserting NO row (0 rows
// affected). The re-check SELECT still finds nothing, so AcquireOrRenew falls
// through to fmt.Errorf(ErrElectorAcquireFailed, ...). Assert
// errors.Is(err, cron.ErrElectorAcquireFailed).
//
// CREATE TABLE msgin_cron_leader_malformed (
//   scope VARCHAR(255) PRIMARY KEY, holder VARCHAR(255) NOT NULL,
//   expires_at DATETIME(6) NOT NULL,
//   guard INT NOT NULL DEFAULT 0 CHECK (guard = 1)  -- INSERT never sets guard -> 0 -> CHECK fails
// );
```
> **Round-2 audit (NEW-HIGH-1):** use the CHECK-constraint provocation, NOT an omitted NOT-NULL column — the latter is inserted with an implicit default (`ROW_COUNT()=1`) on MySQL 8/MariaDB 11 and never reaches the branch. The `CHECK (guard = 1)` violated by `DEFAULT 0` was verified to give `ROW_COUNT()=0` + no row on both engines. Version floor: MySQL ≥ 8.0.16 / MariaDB ≥ 10.2.1 (both pinned images qualify).

Deterministic (no timing dependency); closes the coverage gap the driver-free unit test cannot reach for `ErrElectorAcquireFailed`, mirroring Step 4b for the Locker.

- [ ] **Step 6: Run the conformance suites** (Docker required, `use-testcontainers`):

```bash
cd adapter/cron/crontest
GOTOOLCHAIN=go1.25.12 go test -race ./...
```
Expected: PASS on PostgreSQL, MySQL, MariaDB, and SQLite for both coordinators. If Docker is unavailable in the run environment, note it and rely on the driver-free unit coverage plus a later CI run; do NOT weaken the concurrency assertions. Then `GOTOOLCHAIN=go1.25.12 go mod tidy && git diff --exit-code go.mod go.sum` inside `crontest`.

- [ ] **Step 7: Commit**

```bash
git add adapter/cron/crontest/ go.work
git commit -m "test(cron): real-DB conformance for SQL Locker + Elector (crontest leaf)

New adapter/cron/crontest leaf module (own go.mod, testcontainers, added to
go.work) proving both coordinators on real PostgreSQL, MySQL, MariaDB, and
SQLite: Locker exactly-one-winner per (scope, fire) under concurrency + Purge;
Elector single-leader under concurrency + renewal-within-TTL + TTL failover +
independent multi-scope leadership (M-1). Deterministic malformed-table tests
drive the MySQL/MariaDB demoted-error branches (ErrLockerClaimFailed,
ErrElectorAcquireFailed) on real SQL -- the driver-free fakes cannot reach
them (Round-1 audit H-2). SQLite DSN sets busy_timeout+WAL so concurrent
writers serialize instead of SQLITE_BUSY (M-3). Keeps the root/leaf-dialect
modules driver- and testcontainer-free (Plan 006 gate).

Spec: 006
Plan: 011
ADR: 0017"
```

---

### Task 6: End-to-end example + package doc + whole-branch gate

Proves the headline shape — `NewConsumer[T]` over a cron `Source` fires a handler on schedule — as a runnable `Example` (doubling as godoc), documents the package, and runs the final pre-merge gate over the whole branch.

**Files:**
- Create: `adapter/cron/example_test.go` (root cron module, `package cron_test`, runnable `Example`)
- Create: `adapter/cron/doc.go` (package doc)
- Modify: `docs/HANDOVER.md` (Plan 011 complete)

**Interfaces:**
- Consumes: `cron.NewSource`, `msgin.NewConsumer`, the runtime `Run`/handler wiring, `clockwork` (for a deterministic example if needed).

- [ ] **Step 1: Write the package doc** — `adapter/cron/doc.go`

```go
// Package cron is the msgin channel adapter that ORIGINATES messages on a
// recurring / cron schedule — the Enterprise Integration Scheduled Producer /
// Polling Consumer shape. A Source[T] fires on a wall-clock schedule (standard
// 5-field cron, "@every <duration>" intervals, or @daily/@hourly/... descriptors,
// parsed by robfig/cron/v3) and emits a caller-defined message into a flow that
// the existing runtime (retry/DLQ/flow-control/graceful shutdown) then carries.
//
// # Import alias
//
// The package is named cron and collides with github.com/robfig/cron/v3, which
// this package uses internally. Callers importing both should alias one; most
// callers need only this package:
//
//	import "github.com/kartaladev/msgin/adapter/cron"
//
// # Timezone
//
// WithLocation sets the zone the schedule is evaluated in (default UTC). A
// spec-embedded "CRON_TZ=..."/"TZ=..." prefix OVERRIDES WithLocation for that
// spec; "@every" intervals ignore location entirely (they are relative
// durations, not wall-clock instants).
//
// # Delivery guarantee
//
// At-most-once. A fire is an ephemeral trigger, not a durable row: Ack/Nack are
// no-ops, and a fire missed because a slow handler stalled the loop past its
// instant is SKIPPED, not queued (standard cron overrun behavior — no stampede
// after a pause). A transient handler failure is still retried in-process by the
// runtime's RetryPolicy; a permanent failure routes to the invalid/DLQ sink. A
// syntactically valid schedule with no future occurrence (e.g. "0 0 30 2 *") is
// refused at construction (ErrInvalidSchedule), never left to hot-loop.
//
// # Multi-instance single-fire
//
// With NO coordinator, N replicas each fire on every tick (N-fold — a documented
// footgun). For single-fire across replicas, configure exactly one of:
//
//   - WithLocker: a per-fire claim; exactly one instance wins each (scope, fire).
//     The SQL-backed SQLLocker reuses the InboxDeduper dedup-INSERT and has NO
//     failover gap. RECOMMENDED for GRID-ALIGNED schedules — standard 5-field
//     cron and @daily/@hourly/... descriptors. It is UNSUPPORTED for "@every"
//     schedules (ErrLockerRequiresGridSchedule at construction): "@every"'s next
//     fire is computed relative to each instance's own last-fire/start time, so
//     independent instances never converge on the same dedup key.
//   - WithElector: leader election; only the leader fires. The SQL-backed
//     SQLElector is an on-demand leader-lease, scoped per call so one instance
//     can gate many schedules; its failover latency is bounded by WithLeaseTTL.
//     REQUIRED for "@every" schedules; also usable for grid-aligned schedules
//     when the TTL failover gap is acceptable.
//
// Both SQL coordinators are dependency-free (database/sql, driver injected;
// PostgreSQL/MySQL/SQLite dialects) and DB-server-clock based (skew-free) and
// require an autocommitting Querier (*sql.DB, not *sql.Tx). A coordinator error
// skips the fire FAIL-SAFE — a coordination outage degrades to no fire, never to
// N-fold firing.
package cron
```

- [ ] **Step 2: Write the runnable example** — `adapter/cron/example_test.go`. Use a fake clock so the `// Output:` is deterministic; run the source directly (a full `NewConsumer.Run` example is harder to make output-stable, so demonstrate the Source + a manual drain, which is what a consumer does):

```go
package cron_test

import (
	"context"
	"fmt"
	"time"

	"github.com/jonboulle/clockwork"

	"github.com/kartaladev/msgin"
	"github.com/kartaladev/msgin/adapter/cron"
)

// ExampleSource shows a recurring source: an "@every 1h" schedule emits a
// message carrying the fire time. A fake clock makes the output deterministic;
// in production you would pass the source to msgin.NewConsumer and Run it.
func ExampleSource() {
	epoch := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	clk := clockwork.NewFakeClockAt(epoch)

	src, err := cron.NewSource("@every 1h",
		func(fire time.Time) string { return "tick at " + fire.UTC().Format("15:04") },
		cron.WithClock(clk))
	if err != nil {
		panic(err)
	}

	out := make(chan msgin.Delivery)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = src.Stream(ctx, out) }()

	_ = clk.BlockUntilContext(ctx, 1)
	clk.Advance(time.Hour)
	d := <-out
	fmt.Println(d.Msg.Payload())
	// Output:
	// tick at 01:00
}
```
> If the blackbox `Example` cannot cleanly join the goroutine for goleak within the example (examples don't take a `*testing.T`), keep the `cancel()` deferred and the `Stream` goroutine exits on it; the package `TestMain` goleak check tolerates a just-cancelled goroutine only if it has exited — add a tiny drain or rely on the `<-out` + deferred cancel. Verify the example is goleak-clean by running the package suite (Step 4), not just the example alone.

- [ ] **Step 3: Verify the example** — `GOTOOLCHAIN=go1.25.12 go test -run '^ExampleSource$' ./adapter/cron/` → PASS (the `// Output:` matches).

- [ ] **Step 4: Whole-package suite** — `GOTOOLCHAIN=go1.25.12 go test -race ./adapter/cron/` → PASS, goleak-clean.

- [ ] **Step 5: Update `docs/HANDOVER.md`** — record Plan 011 complete (all six tasks merged), the `robfig/cron/v3` dependency added, the `crontest` leaf module, and the next roadmap items (Spec 006 O6-1 Redis/etcd coordinators, O6-2 `WithSeconds`; deferred Spec 005 items).

- [ ] **Step 6: Commit**

```bash
git add adapter/cron/doc.go adapter/cron/example_test.go docs/HANDOVER.md
git commit -m "docs(cron): package doc + runnable Source example + handover

Package doc (delivery guarantee, multi-instance single-fire, coordinator
choice) and ExampleSource (fake-clock deterministic recurring fire). Handover
updated: Plan 011 complete.

Spec: 006
Plan: 011
ADR: 0017"
```

---

## Whole-branch delivery gate (before merge to main)

Run over `main..HEAD`, resolve/triage every finding, confirm green — per CLAUDE.md §5:

- [ ] `/code-review` on `main..HEAD`; fix or triage every finding.
- [ ] `/security-review` on the pending changes. Focus surface: the dialect SQL — confirm **every** table identifier flows through `validateIdent`+quote (never interpolated raw), and `scope`/`fire`/`holder`/`instanceID` are always **bound parameters**, never interpolated. Confirm the MySQL `LOCK IN SHARE MODE` / `INSERT IGNORE` paths cannot silently drop a real error (the `ErrLockerClaimFailed`/`ErrElectorAcquireFailed` guards).
- [ ] `GOTOOLCHAIN=go1.25.12 go test ./... -race` green across ALL modules (root + every leaf via `go.work`, including `crontest` with Docker).
- [ ] `GOTOOLCHAIN=go1.25.12 go vet ./...`, `golangci-lint run ./...`, `govulncheck ./...` clean; `gofmt -l .` silent.
- [ ] `CGO_ENABLED=0 GOTOOLCHAIN=go1.25.12 go build ./...` succeeds (pure Go — modernc.org/sqlite is cgo-free and lives only in `crontest`).
- [ ] **ADR 0016 acceptance:** `go mod graph | grep robfig` shows no transitive edge; `go mod verify` passes; root `go mod tidy` leaves `go.mod`/`go.sum` unchanged; `NOTICE` carries the MIT attribution.
- [ ] Coverage: `adapter/cron` ≥85% and every typed-error/hot-path branch covered — the gate win/lose/error (Task 2), the `@every`+Locker refusal (`ErrLockerRequiresGridSchedule`, Task 2), the Locker verdict won/lost + `ErrLockerClaimFailed` (Tasks 3/5), the Elector acquire/renew/takeover/not-leader + `ErrElectorAcquireFailed` (Tasks 4/5), skip-missed (Task 1), the unsatisfiable-schedule guard at both construction and in the `Stream` loop (Task 1), all sentinels, `ErrInvalidRetention`/`ErrInvalidLeaseTTL`/`ErrConflictingCoordinator`/`ErrInvalidSchedule`/`ErrNilFactory`. **`ErrLockerClaimFailed`/`ErrElectorAcquireFailed` and every other `dialect.go` SQL branch are covered ONLY by the `crontest` real-DB run (Tasks 5 Step 4b/5b's deterministic malformed-table tests) — the driver-free unit-test fakes do NOT and CANNOT reach them (Round-1 audit H-2/H-3); do not report or accept a coverage number that omits `crontest`.**
- [ ] Set ADR 0016 + ADR 0017 Status → **Accepted** (they ship Accepted in Task 1's commit; confirm the final state).
- [ ] Confirm with the user before merge/push (never merge/push without explicit approval); then delete `feat/cron-source` after merge (`git branch -d`, and remote if pushed).

## Self-review notes (author)

**Adversarial audit — BOTH rounds complete (2026-07-19), all findings folded.** Round-2
(`.superpowers/sdd/plan-011-audit-round-2.md`) verified both BLOCKERs genuinely closed and every other Round-1 fix
correct, and caught **one HIGH regression**: the H-2 demoted-error test provocation (an omitted NOT-NULL column)
was empirically shown on live MySQL 8.0.40 / MariaDB 11.4 to insert a row (`ROW_COUNT()=1`) rather than demote —
corrected here to a `CHECK (guard = 1)` violation (verified `ROW_COUNT()=0` + no row on both engines; Steps 4b/5b),
plus NEW-LOW-1 (gap-lock godoc clause on `AcquireOrRenew`) and NEW-NIT-1 (single `sql.Register` guard). Round-2's
verdict: implementable, no round-3 needed. Every Round-1 finding from `.superpowers/sdd/plan-011-audit-round-1.md`
was resolved in the spec/ADR/plan (not merely noted):

- **B-1 (BLOCKER — Locker dedup key not instance-invariant for `@every`):** `NewSource` now refuses `WithLocker` +
  an `@every` schedule at construction (`ErrLockerRequiresGridSchedule`, KD-5, Task 2's `wireGate`); the Locker is
  reframed everywhere as the grid-aligned (cron/descriptor) primitive, the Elector as the `@every` primitive
  (spec D10/D11, ADR 0017 part 3, `doc.go`, `Source`/`WithLocker` godoc). Tested: `TestSource_LockerRequiresGridSchedule`
  (Task 2).
- **B-2 (BLOCKER — unsatisfiable schedule hot-loops):** `NewSource` probes `schedule.Next(...).IsZero()` after
  parsing (ErrInvalidSchedule); `Stream` additionally guards a zero `Next` by parking on `ctx.Done()`
  (belt-and-suspenders). Tested: a Feb-30 spec case added to `TestSource_ConstructionValidation` (Task 1).
- **H-1 (Task 1 doesn't compile standalone):** `Elector`/`Locker` interfaces moved to Task 1 (`coordinator.go`);
  Task 2 now adds only the options, `wireGate` body, and the two sentinels.
- **H-2/H-3 (false/absent coverage claims for the MySQL demoted-error branches and dialect SQL generally):** the
  plan no longer claims the driver-free fakes cover `ErrLockerClaimFailed`/`ErrElectorAcquireFailed`; Task 5 adds
  deterministic malformed-table tests (Steps 4b/5b) that drive the real dialect code on MySQL and MariaDB, and the
  Global Constraints + whole-branch gate now say explicitly that `dialect.go` coverage requires the `crontest`
  Docker run.
- **H-4 (no `NewConsumer` integration test):** Task 1 Step 6b adds `TestSource_ThroughNewConsumer`, driving the
  Source through `msgin.NewConsumer[T]` + `Run`.
- **M-1 (Elector/Locker scope asymmetry):** `Elector.IsLeader` now takes `scope` per call, symmetric with
  `Locker.Claim` (KD-2/KD-3 rewritten); `WithElectorScope` removed.
- **M-2 (MySQL atomicity requires autocommit; non-locking verify read):** `ElectorDialect`/`LockerDialect` godoc
  now states the autocommitting-`Querier` precondition explicitly; the Elector's MySQL verifying SELECTs use
  `LOCK IN SHARE MODE` (Task 4 Step 3b).
- **M-3 (SQLite busy_timeout):** Task 5 Step 3 specifies `_pragma=busy_timeout(5000)` + WAL in the `crontest`
  SQLite DSN.
- **L-1 through L-6:** L-1 (WithScope collision risk) — tightened in the `WithScope` godoc (Task 1). L-2
  (`querier`/`Querier` inconsistency + broken fake signature) — `Querier` exported everywhere, fake signatures
  fixed (Task 3). L-3 ("sticky" wording) — `WithLeaseTTL`/`Elector` godoc clarified (Tasks 1/4). L-4 (`CRON_TZ=`
  override) — documented on `WithLocation`/`doc.go` (Tasks 1/6). L-5 (`randomID` error ignored) — commented,
  consistent with the core's own `randomID` (Task 3, since superseded — see below). L-6 (`WithInstanceID` naming
  drift) — spec/plan wording aligned on `WithInstanceID` (Locker) / `WithElectorInstanceID` (Elector), *since
  superseded*: the Locker's `WithInstanceID`/`claimed_by`/`randomID` were removed as YAGNI in a Task 3 review
  pass 2 (post-implementation, before this plan's Task 4) — see Task 3's "Produces" note and Spec 006 D12 /
  ADR 0017. `WithElectorInstanceID` (Elector) is unaffected and remains the sole holder-identity option.

- **Spec coverage:** G1 (recurring Source[T]/StreamingSource) → Task 1; G2 (cron/@every/descriptors via robfig) → Task 1 + ADR 0016 (Task 1 dep step); G3 (goleak-clean, no goroutine, fake-clock) → Task 1 (loop, `TestMain`, `BlockUntilContext`); G4 (Elector+Locker + dependency-free SQL impls PG/MySQL/SQLite) → Tasks 1/2/3/4/5; G5 (safe defaults: at-most-once, skip-missed, UTC, injectable clock, all `WithX`-overridable) → Tasks 1/3/4 + doc (Task 6). D1–D12 all realized: D1 robfig root dep (Task 1), D2/D4 StreamingSource+LiveValueSource (Task 1), D3 factory (Task 1), D5 skip-missed loop (Task 1 test), D6 no-op Ack/Nack (Task 1), D7 clock+location incl. CRON_TZ override (Task 1), D8 construction validation incl. satisfiability (Tasks 1/3/4), D9 on-demand scoped gate + fail-safe skip + ErrConflictingCoordinator + ErrLockerRequiresGridSchedule (Tasks 1/2), D10 SQL Locker dedup-INSERT, grid-aligned only (Tasks 3/5), D11 SQL Elector leader-lease, scope-parameterized (Tasks 4/5), D12 holder identity is Elector-only via `WithElectorInstanceID` (Task 4) — the Locker's `WithInstanceID` was removed as YAGNI post-implementation (Task 3 review pass 2). N1 (no core runtime change) held — only `adapter/cron` + one root dep. N3/N4 (no Redis/etcd, no seconds field) deferred, documented.
- **Open items resolved:** O6-3 (coordination interfaces in `adapter/cron`) — yes, `coordinator.go`, now declared in Task 1. O6-4 (one plan, phased) — yes, six tasks. O6-5 (dialect seam location) — **KD-1**: in `adapter/cron`, own SQL micro-primitives, `Querier` exported (decided). O6-1/O6-2 deferred.
- **Risks addressed:** R1 (universal dep) → ADR 0016 acceptance gate (`go mod graph` no-transitive check, Task 1 Step 1 + whole-branch gate). R2 (leader-election correctness) → Task 4's per-dialect atomic acquire-or-renew with the MySQL three-step (locking verify reads, autocommit precondition documented) + Task 5's real MySQL **and** MariaDB concurrency conformance + deterministic demoted-error tests + the dedicated audit focus. R3 (overrun) → skip-missed test (Task 1). R4 (no-coordinator footgun) → documented in `doc.go`/`Source` godoc (Tasks 1/6). R5 (size) → six green-unit tasks. R6 (Locker grid-alignment, new) → `ErrLockerRequiresGridSchedule` refusal + test (Task 2). R7 (unsatisfiable schedule, new) → construction probe + loop guard (Task 1).
- **Decisions flagged for the audit/user (Key design decisions):** KD-1 own-primitives, `Querier` exported (decided, not open); KD-2 Source-owned scope for BOTH Locker and Elector (default spec string); KD-3 REMOVED — superseded by KD-2 (Elector scope is now per-call, not baked at construction); KD-4 30s lease-TTL default, reframed around schedule-shaped coordinator choice; KD-5 (new) the `@every`+Locker construction-time refusal.
- **Type consistency:** `Source[T]`/`NewSource`/`Stream`/`EmitsLiveValue`; `Elector.IsLeader(ctx, scope)`/`Locker.Claim(ctx, scope, fire)` (scope-symmetric, M-1); `WithClock`/`WithLocation`/`WithScope`/`WithCronLogger`/`WithElector`/`WithLocker`; `Querier` (exported everywhere); `LockerDialect.ClaimFire`/`PurgeFired`/`EnsureFiredSchema`; `ElectorDialect.AcquireOrRenew`/`EnsureLeaseSchema` (both require an autocommitting `Querier`); `SQLLocker.Claim`/`Purge`/`EnsureSchema`; `SQLElector.IsLeader(ctx, scope)`/`EnsureSchema` (no `WithElectorScope`); the six dialect constructors + six `*DDL` builders; `WithLockerTable` (Locker; no claimant option — removed as YAGNI, Task 3 review pass 2) vs `WithElectorTable`/`WithElectorInstanceID`/`WithLeaseTTL` (Elector; holder identity is Elector-only); sentinels `ErrInvalidSchedule` (incl. unsatisfiable specs)/`ErrNilFactory`/`ErrConflictingCoordinator`/`ErrLockerRequiresGridSchedule`/`ErrNilDialect`/`ErrInvalidTableName`/`ErrInvalidRetention`/`ErrLockerClaimFailed`/`ErrInvalidLeaseTTL`/`ErrElectorAcquireFailed` — verified: no lingering `WithElectorScope`, no unscoped `IsLeader(ctx)`, no claim that a fake covers a demoted-error branch, `Querier` exported everywhere, `ErrLockerRequiresGridSchedule` present end to end.
- **Deferred (documented):** Redis/etcd coordinators (O6-1), seconds-field cron (O6-2), a `Ready`/`SchemaExists` fail-fast for the coordinators (the first Claim/IsLeader errors clearly if the table is missing — a lighter posture than the sql adapter's `Ready`; note as a possible follow-up), and prod-migration DDL is served by the `*DDL` string builders + opt-in `EnsureSchema`.
- **SemVer:** all-additive new package + one new root dependency (a minor bump; the dep is an architectural decision recorded in ADR 0016). No existing exported symbol changes.
