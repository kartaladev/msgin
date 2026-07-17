# HANDOVER — msgin

> **Next session: read this first.** Before acting, read `CLAUDE.md`, then the governing artifacts:
> `docs/specs/001-messaging-core.md` (§9 for the next increment), ADRs `docs/adrs/0001`–`0008`, and the
> completed plans `docs/plans/001`,`002`,`003`. **Trust those files over any memory.** Written at a clean
> safepoint: Plans 001 + 002 + 003 complete, race-green, and landed on `main`.

_Updated: Plan 003 (resilience & flow control) complete and merged to `main` (`2e7d2d1`)._

## 1. Objective & roadmap position

`msgin` (`github.com/kartaladev/msgin`) — a Go 1.25 Enterprise Integration Patterns library (minimal
deps). Design ratified in spec 001 + ADRs 0001–0008.

- **DONE & on `main`:**
  - **Plan 001** — core (`Message[T]`, `Headers`, `PayloadCodec`, the adapter SPI) + in-memory adapter +
    minimal `Producer[T]`/`Consumer[T]`.
  - **Plan 002** — reliability: guarded settlement switch, `RetryPolicy` + closed-form backoff,
    invalid-message + dead-letter routing, `Permanent`/`ErrHandlerPanic`, panic-safe hooks, bounded
    graceful shutdown.
  - **Plan 003** — resilience/flow-control: `WithMaxInFlight` credit gate (semaphore + release-first),
    `WithRateLimit`, `WithHandlerTimeout`, `WithCircuitBreaker`, `WithOverflow`, TTL-swept bounded
    attempt tracker. Fixed a real default-config deadlock (workerCh buffered to credit capacity).
- **NEXT: Plan 004 — Poller + SQL adapter (spec §9).** The shared **Poller** (interval/fixed-delay,
  max-per-poll, backoff, clockwork, cancellable) driving `PollingSource` **with credit-at-FETCH** (spec
  §7.4.1 — the Plan-003 credit gate is currently streaming-source only; Plan 004 wires the pull side:
  fetch `k = min(maxBatch, n − inFlight)`, acquire k credits at claim). Plus the `adapter/database/sql`
  adapter (generic `database/sql`, `SELECT … FOR UPDATE SKIP LOCKED` inbound + `INSERT` outbound,
  at-least-once, v1 PostgreSQL + MySQL dialects via a `Dialect` seam; driver injected by the caller).
  **Use `use-testcontainers`** (real Postgres/MySQL — Docker IS available here) via a
  `database.RunTestDatabase(t, opts...)` helper. This is the first **wire** adapter → the
  first place `PayloadCodec` decode runs on external bytes.
- Then Plan 005 (http, `cenkalti/backoff/v4` enters here via `adapter/http`), 006–008 (pgx/redis/nats —
  **separate Go modules**, `go.work`; ADR 0003).

## 2. Exact state

- **`main` = `origin/main` = `2e7d2d1`**, in sync. Feature branches deleted after landing. Commit history
  is logical-feature commits (no micro-commits): per plan, 1–3 `feat(...)` commits + a `docs: handover`
  commit, each an independently-green unit with `Spec:/Plan:/ADR:` + Co-Authored-By + Claude-Session
  trailers.
- **Whole module green:** `GOTOOLCHAIN=go1.25.0 go test ./... -race` passes (stable `-count=5`+); vet,
  gofmt, `CGO_ENABLED=0` build, `go mod tidy` (no-op), `go mod verify`, `govulncheck`, `golangci-lint`
  all clean. Coverage ~99% core, 100% memory.
- **Deps:** core non-test = stdlib + `clockwork` ONLY. `cenkalti/backoff/v4` (ADR 0005) still NOT a dep —
  it enters with `adapter/http` (Plan 005). Test-only: `testify`, `goleak`.

## 3. Method (established, working extremely well)

Per increment: draft the plan (opus) from the spec → **adversarial Opus audit of the plan** (caught a
shutdown-deadlock in 002 and a fake-clock hang + zero-sentinel in 003 BEFORE any code) → reconcile/ADR →
**SDD** task-by-task (fresh implementer + reviewer per task; opus for concurrency-critical tasks) →
whole-branch `/code-review` + `/security-review` (opus — caught the 003 default-config deadlock,
hook-panic, etc.) → fold gate fixes into the branch → **consolidate into logical-feature commits** →
fast-forward `main` + push + delete branch. Live recovery map: `.superpowers/sdd/progress.md`
(git-ignored). Landing to `main` was user-authorized per completed plan.

## 4. TRACKED FOLLOW-UPS (Important backlog — address in/with the noted plan)

1. **SPI-panic isolation (Important) — ADR 0008 D10.** `RateLimiter.Wait` / `CircuitBreaker.Allow/
   Record/HalfOpen` are invoked directly (not `safeFire`-wrapped), so a panicking CUSTOM plug-in crashes
   the process (shipped defaults never panic). Deferred because recovering control-flow calls needs a
   deliberate fail-open/fail-closed policy (what does a panicking `Allow()` resolve to?). **Fix in a
   dedicated cycle before the first non-default SPI plug-in ships.** The interface godoc now documents a
   hard no-panic contract as the interim mitigation.
2. **Circuit-breaker half-open probe storm (Important) — ADR 0008 D10.** Under `WithConcurrency(N>1)` +
   `WithCircuitBreaker`, half-open admits UNLIMITED concurrent probes (default N=1 is single-probe by
   construction). A proper single-in-flight-probe state-machine change deserves its own TDD cycle.
3. **`Backoff.Max < defaultAttemptTTL` (5m) invariant — ADR 0008 D10 (triage b).** The TTL sweep's NF-2
   safety assumes redelivery cadence ≪ 5m. Not reachable today (memory redelivers immediately). Enforce
   (or add a `WithAttemptTTL` escape hatch) **when the first delay-parking / at-least-once wire source
   lands (Plan 006+).**
4. **`ErrNoPayloadCodec` unused sentinel (open question).** Wire+no-codec defaults to JSON, not error.
   Kept pre-1.0. Decide keep/error/drop — likely resolved when the wire `PollingSource` lands in Plan 004.
5. **Untrusted wire-decode limits (security, Plan 004+).** Impose payload size/complexity limits on
   decoding external bytes when the first wire adapter (sql) lands — not reachable in Plans 001–003
   (memory carries live values, no decode).
6. **Overflow-drop `requeue=false` (ADR 0008 D9).** DropNewest uses `requeue=false` (correct for
   at-most-once memory). Revisit the at-least-once redeliver-on-drop semantics when redis/nats land
   (Plan 006+).

## 5. Next actions (resume here)

1. Confirm `main` green: `GOTOOLCHAIN=go1.25.0 go test ./... -race`.
2. Draft `docs/plans/004-*.md` from spec §9 (sql) + §7.4.1 (credit-at-fetch) + the Poller design (opus)
   → adversarial Opus audit (credit-at-fetch is the highest-risk new piece — over-pull/lease-expiry
   classes) → SDD (with `use-testcontainers` for Postgres/MySQL) → whole-branch gate → consolidate → land.
   The `ErrNoPayloadCodec` decision (§4.4) belongs in this plan.

## 6. Gotchas / environment

- **Go 1.25 pinned**; always `GOTOOLCHAIN=go1.25.0` (local default is newer). No `toolchain` directive.
- **gopls@latest can't run under 1.25**; subagents use `go build`/`vet`/`gofmt` + tests as diagnostics.
- **Fake-clock (`clockwork`) `BlockUntilContext(ctx, N)` counts ALL registered waiters** — the always-on
  attempt-tracker sweep ticker is a permanent consumer-clock waiter, so consumer-fake-clock tests that
  wait for a drain/timeout/breaker timer use `N = (real timers) + 1`. A wrong count HANGS the test — run
  `-count=N` to shake out miscounts. (This bit Plan 003 twice; documented in ADR 0008.)
- **Custom test skills (mandatory):** `table-test` (assert-closure), `use-mockgen`, `use-testcontainers`
  (Plan 004+). Blackbox tests only; one goleak `TestMain` per goroutine-starting test package.
- **Docker is RUNNING** — Plan 004+ testcontainers are feasible.
- **Multi-module (ADR 0003):** pgx/redis/nats (Plans 006–008) are SEPARATE Go modules with a `go.work`;
  release tags are module-path-prefixed. Core + memory + sql + http stay in the root module.
