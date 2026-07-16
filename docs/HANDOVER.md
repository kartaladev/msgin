# HANDOVER — msgin

> **Next session: read this first.** Before acting, read `CLAUDE.md`, then the governing artifacts:
> `docs/specs/001-messaging-core.md` (§7.4 for the next increment), ADRs `docs/adrs/0001`–`0007`, and the
> completed plans `docs/plans/001`,`002`. **Trust those files over any memory.** Written at a clean
> safepoint: Plans 001 + 002 complete, race-green, and landed on `main`.

_Updated: Plan 002 (reliability) complete and merged to `main` (`26f30a4`)._

## 1. Objective & roadmap position

`msgin` — a Go 1.25 Enterprise Integration Patterns library (minimal deps). Design ratified in spec 001.

- **DONE & on `main`:** Plan 001 (core + in-memory transport) and Plan 002 (reliability: guarded
  settlement switch, RetryPolicy + closed-form backoff, invalid-message + dead-letter routing,
  ErrHandlerPanic + msgin-native `Permanent`, panic-safe observability hooks, bounded graceful shutdown).
- **NEXT: Plan 003 — Resilience & flow control (spec §7.4, ADR 0006).** Credit-based `WithMaxInFlight`
  (the flood defense — the load-bearing feature), plus optional rate limit, handler timeout
  (`WithHandlerTimeout`), circuit breaker, overflow policy. All clockwork-driven, dependency-free
  defaults (x/time/rate, sony/gobreaker are optional plug-ins, not forced deps).
  **Plan 003 MUST also resolve the triaged `attemptTracker` unbounded-growth limitation** (ADR 0007
  "Known limitation") with a bounded/TTL tracker that respects NF-2 and pairs with credit/delay-park.
- Then Plan 004 (Poller + sql, testcontainers — Docker is available here), 005 (http), 006–008
  (pgx/redis/nats — separate modules, go.work).

## 2. Exact state

- **`main` = `origin/main` = `26f30a4`**, in sync. History (newest first): `feat(runtime)` 26f30a4,
  `feat(reliability)` 1980443, then the Plan-001 commits. Feature branches deleted after landing.
- **Whole module green:** `GOTOOLCHAIN=go1.25.0 go test ./... -race` passes; vet, gofmt, `CGO_ENABLED=0`
  build, `go mod tidy` (no-op), `go mod verify`, `govulncheck`, `golangci-lint` all clean. Coverage ~99%
  core, 100% memory.
- **Deps:** core non-test = stdlib + `clockwork` only. `cenkalti/backoff/v4` (ADR 0005) still NOT a dep —
  it enters with `adapter/http` (Plan 005), not the core. Test-only: `testify`, `goleak`.

## 3. Method (established, working well)

Per increment: draft the plan (opus) from the spec → **adversarial Opus audit of the plan** (this caught
a shutdown-deadlock C1 in Plan 002 before any code) → reconcile/ADR → **SDD** execute task-by-task
(fresh implementer + reviewer per task; opus for the concurrency-critical tasks) → whole-branch
`/code-review` + `/security-review` (opus) → fold gate fixes into the branch → **consolidate into
logical-feature commits** (no micro-commits on `main`; proper `Spec:/Plan:/ADR:` + Co-Authored-By +
Claude-Session trailers; each commit an independently-green unit) → fast-forward `main` + push + delete
branch. Live recovery map: `.superpowers/sdd/progress.md` (git-ignored).

## 4. Open questions for the user (non-blocking)

- **`ErrNoPayloadCodec` is an unused exported sentinel** (wire+no-codec defaults to JSON, not error).
  Kept pre-1.0. Decide keep/error/drop when convenient — likely used when a wire `PollingSource` lands
  in Plan 004.
- **Impose payload size/complexity limits on untrusted wire decoding** when the first wire adapter lands
  (Plan 004+). Not reachable in Plans 001–002 (memory carries live values, no decode).

## 5. Next actions (resume here)

1. Confirm `main` green: `GOTOOLCHAIN=go1.25.0 go test ./... -race`.
2. Draft `docs/plans/003-*.md` from spec §7.4 (opus) → Opus audit → SDD → gate → consolidate → land.
   Remember the tracker-DoS resolution is in Plan 003's scope.

## 6. Gotchas / environment

- **Go 1.25 pinned**; always `GOTOOLCHAIN=go1.25.0` (local default is newer). No `toolchain` directive.
- **gopls@latest can't run under 1.25**; subagents use `go build`/`vet`/`gofmt` + tests as diagnostics.
- **Custom test skills (mandatory):** `table-test`, `use-mockgen`, `use-testcontainers` (the latter two
  from Plan 004). Blackbox tests only; one goleak `TestMain` per goroutine-starting test package.
- **Docker is RUNNING** — Plan 004+ testcontainers are feasible.
- Landing to `main` is user-authorized for each completed plan in this run (fast-forward + push).
