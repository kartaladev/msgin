# HANDOVER — msgin

> **Next session: read this first.** Before acting, read `CLAUDE.md`, then the governing artifacts:
> `docs/specs/001-messaging-core.md` (§9 for the next increment), ADRs `docs/adrs/0001`–`0009`, and the
> completed plans `docs/plans/001`–`004`. **Trust those files over any memory.** Written at a clean
> safepoint: Plans 001–004 complete, race-green, and landed on `main`.

_Updated: Plan 004 (resilience hardening — cleared the ADR 0008 D10 backlog) complete and merged to
`main`._

## 1. Objective & roadmap position

`msgin` (`github.com/kartaladev/msgin`) — a Go 1.25 Enterprise Integration Patterns library (minimal
deps). Design ratified in spec 001 + ADRs 0001–0009.

- **DONE & on `main`:**
  - **Plan 001** — core (`Message[T]`, `Headers`, `PayloadCodec`, adapter SPI) + in-memory adapter +
    minimal `Producer[T]`/`Consumer[T]`.
  - **Plan 002** — reliability: guarded settlement switch, `RetryPolicy` + backoff, invalid-message +
    dead-letter routing, `Permanent`/`ErrHandlerPanic`, panic-safe hooks, bounded graceful shutdown.
  - **Plan 003** — resilience/flow-control: `WithMaxInFlight` credit gate, `WithRateLimit`,
    `WithHandlerTimeout`, `WithCircuitBreaker`, `WithOverflow`, TTL-swept attempt tracker.
  - **Plan 004** — resilience hardening (ADR 0009), clearing 5 of 6 tracked follow-ups (see §4):
    - **D1** SPI-plug-in panic isolation — `safe{LimiterWait,Allow,TryProbe,Record,HalfOpen}` fail open
      on a panicking plug-in; ERROR-logged once per method (`governorPanic` `sync.Map` dedup).
    - **D2** breaker probe-storm — optional `ProbeGate{TryProbe() bool}` capability; the default breaker
      admits a single half-open probe under `WithConcurrency(N>1)`; one-time `Run` WARN when an N>1
      breaker lacks `ProbeGate`; `toHalfOpen` unconditionally resets `probeInFlight` (wedge-safe).
    - **D3** `WithAttemptTTL` + `ErrInvalidAttemptTTL`; TTL invariant reframed (round-trip incl. handler
      time, not just `Backoff.Max`).
    - **D4** dropped the unused `ErrNoPayloadCodec`; ratified wire+no-codec→JSON default.
    - **D5** `WithMaxPayloadBytes` caps untrusted `[]byte` before `codec.Decode` (`ErrPayloadTooLarge`,
      permanent → invalid sink).
- **NEXT: Plan 005 — Poller + SQL adapter (spec §9).** The shared **Poller** (interval/fixed-delay,
  max-per-poll, backoff, clockwork, cancellable) driving `PollingSource` **with credit-at-FETCH** (spec
  §7.4.1 — the credit gate is currently streaming-source only; Plan 005 wires the pull side: fetch
  `k = min(maxBatch, n − inFlight)`, acquire k credits at claim). Plus the `adapter/database/sql` adapter
  (generic `database/sql`, `SELECT … FOR UPDATE SKIP LOCKED` inbound + `INSERT` outbound, at-least-once,
  v1 PostgreSQL + MySQL dialects via a `Dialect` seam; driver injected by the caller). **Use
  `use-testcontainers`** (real Postgres/MySQL — Docker IS available) via a `database.RunTestDatabase(t,
  opts...)` helper. First **wire** adapter → first place `PayloadCodec.Decode` runs on external bytes,
  where **the D5 byte-cap should get a sensible adapter default** and follow-up #5-complexity is revisited.
- Then Plan 006 (http, `cenkalti/backoff/v4` enters via `adapter/http`), 007–009 (pgx/redis/nats —
  **separate Go modules**, `go.work`; ADR 0003).

## 2. Exact state

- **`main` fast-forwarded to include Plan 004; pushed to `origin/main`.** Feature branch
  `feat/resilience-hardening` deleted after landing. Commit history is logical-feature commits: per plan,
  1 `feat(...)` commit + a `docs: handover` commit, each an independently-green unit with
  `Spec:/Plan:/ADR:` + Co-Authored-By + Claude-Session trailers.
- **Whole module green:** `GOTOOLCHAIN=go1.25.0 go test ./... -race` passes (stable `-count=3`+); vet,
  gofmt, `CGO_ENABLED=0` build, `go mod tidy` (no-op), `go mod verify`, `govulncheck`, `golangci-lint`
  all clean. Coverage ~99% core, 100% memory.
- **Deps:** core non-test = stdlib + `clockwork` ONLY. `cenkalti/backoff/v4` (ADR 0005) still NOT a dep —
  it enters with `adapter/http` (Plan 006). Test-only: `testify`, `goleak`.

## 3. Method (established, working extremely well)

Per increment: draft the plan (opus) from the spec → **two independent adversarial Opus audits of the
design** (Plan 004: a concurrency-correctness pass caught the `toHalfOpen` wedge landmine + the TTL
round-trip-vs-Backoff.Max reframe; an API/policy pass caught the silent gobreaker-cliff and pulled
finding #5's byte-cap back in-scope) → reconcile into the ADR → **SDD** (fresh implementer + reviewers
per unit; subagents write code, the coordinator session stays clean and does only the consolidated
commit) → whole-branch `/code-review` + `/security-review` + an independent Opus whole-branch review →
fold gate fixes → consolidate to logical-feature commits → fast-forward `main` + push + delete branch.
Live recovery map: `.superpowers/sdd/progress.md` (git-ignored). Landing to `main` was user-authorized.

## 4. TRACKED FOLLOW-UPS (backlog)

**CLEARED in Plan 004 (ADR 0009):** #1 SPI-panic isolation → D1; #2 breaker half-open probe storm → D2;
#3 `Backoff.Max`/TTL → D3 (`WithAttemptTTL` escape hatch + reframed invariant doc; best-effort concrete
enforcement still deferred, below); #4 `ErrNoPayloadCodec` decision → D4; #5 untrusted wire-decode
**byte cap** → D5 (`WithMaxPayloadBytes`).

**STILL OPEN:**
1. **(#6) Overflow-drop at-least-once redeliver semantics — Plan 007+.** `DropNewest` uses
   `requeue=false` (correct for at-most-once memory). Revisit redeliver-on-drop when redis/nats land
   (ADR 0008 D9, ADR 0009 D6) — needs a real at-least-once source to model/test.
2. **(#3-residual) Best-effort `ExponentialBackoff`-vs-attempt-TTL enforcement — Plan 007+.** D3 shipped
   the `WithAttemptTTL` escape hatch + a prominent godoc invariant, but does NOT statically enforce that
   a caller's `Backoff.Max`/handler round-trip stays below the TTL. Revisit a concrete-type guard when
   the first **delay-honoring** source lands (memory ignores the Nack delay, so it is not reachable via
   memory beyond the documented tiny-TTL footgun).
3. **(#5-complexity) Payload structural-complexity limits — Plan 005+.** D5 caps payload *bytes*; deep
   nesting is currently bounded only by the codec (`encoding/json` errors on pathological nesting). If a
   custom/binary codec adapter lands, consider a codec-level complexity knob.
4. **(NEW — from the Plan 004 whole-branch review, out-of-scope observation) Dispatch-boundary panic
   recovery.** `dispatch` does NOT `recover` a panic from `codec.Decode`, `sink.Send`, or an adapter
   `Ack`/`Nack` — such a panic crashes the process (the worker loop has no recover; only `safeHandle`
   and the new `safe*` governor wrappers recover). Pre-existing (not introduced by Plan 004). Consider a
   `safeHandle`-style guard at the decode/settle boundary when the first wire adapter (Plan 005) makes an
   adapter `Ack`/`Nack`/`Decode` panic realistic. Track alongside Plan 005.

## 5. Next actions (resume here)

1. Confirm `main` green: `GOTOOLCHAIN=go1.25.0 go test ./... -race`.
2. Draft `docs/plans/005-*.md` from spec §9 (sql) + §7.4.1 (credit-at-fetch) + the Poller design (opus)
   → two adversarial Opus audits (credit-at-fetch over-pull/lease-expiry is the highest-risk piece) →
   SDD with `use-testcontainers` (Postgres/MySQL) → whole-branch gate → consolidate → land. Fold in the
   D5 adapter-default byte-cap and revisit follow-ups #3-complexity and #4 (dispatch-panic recovery)
   there.

## 6. Gotchas / environment

- **Go 1.25 pinned**; always `GOTOOLCHAIN=go1.25.0` (local default is newer). No `toolchain` directive.
- **gopls@latest can't run under 1.25**; subagents use `go build`/`vet`/`gofmt` + tests as diagnostics.
- **Fake-clock (`clockwork`) `BlockUntilContext(ctx, N)` counts ALL registered waiters** — the always-on
  attempt-tracker sweep ticker is a permanent consumer-clock waiter, so consumer-fake-clock tests that
  wait for a drain/timeout/breaker timer use `N = (real timers) + 1`. `WithAttemptTTL` changes the sweep
  ticker's *period*, NOT the waiter *count* — the `+1` invariant is unchanged (ADR 0008 D8 C1 / ADR 0009).
- **Custom test skills (mandatory):** `table-test` (assert-closure), `use-mockgen`, `use-testcontainers`
  (Plan 005+). Blackbox tests only; one goleak `TestMain` per goroutine-starting test package.
- **Docker is RUNNING** — Plan 005+ testcontainers are feasible.
- **Multi-module (ADR 0003):** pgx/redis/nats (Plans 007–009) are SEPARATE Go modules with a `go.work`;
  release tags are module-path-prefixed. Core + memory + sql + http stay in the root module.
- **Breaker single-probe residuals (ADR 0009 D2, documented, accepted):** single-probe is best-effort
  under `N>1` cross-cycle stragglers; half-open under `N>1` hot-spins on immediate-redelivery sources
  (memory ignores the Nack delay) — not present on delay-honoring wire adapters.
