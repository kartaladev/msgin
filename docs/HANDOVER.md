# HANDOVER — msgin

> **Next session: read this first.** Before acting, read `CLAUDE.md`, then the governing artifacts:
> `docs/specs/001-messaging-core.md`, the ADRs `docs/adrs/0001`–`0006`, and — for the next increment —
> author `docs/plans/002-*.md`. **Trust those files over any memory or recollection.** This handover
> was written at a clean safepoint: Plan 001 is complete, race-green, and landed on `main`.

_Updated: Plan 001 (core foundation & in-memory transport) complete and merged to `main`._

## 1. Objective & roadmap position

Building **`msgin`** — a Go 1.25 Enterprise Integration Patterns library (Spring-Integration model,
minimal deps). Design complete; **Plan 001 is DONE and on `main`**.

- **Completed:** `docs/plans/001-core-foundation.md` — the typed `Message[T]` envelope, error contract,
  `PayloadCodec[T]`, the adapter SPI, the in-memory adapter, and the minimal `Producer[T]`/`Consumer[T]`
  runtime (streaming ingest + worker pool + Ack/Nack). Executed via SDD (9 tasks), each with a task
  review + fix loop, then a whole-branch `/code-review` + `/security-review`, gate fixes folded in, and
  consolidated into 3 logical-feature commits.
- **Next:** **Plan 002 — reliability.** Replace `dispatch`'s minimal settle with the guarded settlement
  switch: `RetryPolicy` + closed-form backoff, attempt counting (`msgin.delivery-count`), the
  invalid-message channel (decode failures + permanent errors), dead-letter, a public panic-error
  sentinel + observability hooks (`*slog.Logger` + `Hooks`), and `WithShutdownTimeout`. Adds
  `WithRetryPolicy`, `WithInvalidMessageSink`, `WithLogger`, `WithHooks`. (See Plan 001's "Notes for
  Plan 002" and spec §7.) Then Plan 003 (resilience/credit flow-control), 004 (poller + sql), 005 (http),
  006–008 (pgx/redis/nats).

## 2. Exact state

- **`main`** now contains Plan 001 (fast-forwarded from `feat/messaging-core`, which was deleted after
  landing). Three logical-feature commits on top of the design/scaffold:
  `feat(core)` → `feat(spi,memory)` → `feat(runtime)`, plus this handover doc commit.
- **Whole module is green:** `GOTOOLCHAIN=go1.25.0 go test ./... -race` passes; `go vet`, `gofmt -l .`,
  `CGO_ENABLED=0 go build ./...`, `go mod tidy` (no-op), `go mod verify`, and `govulncheck ./...` all
  clean. Statement coverage is 100% on the root `msgin` package and `adapter/memory`.
- **Deps:** core non-test = stdlib + `clockwork` only. `cenkalti/backoff/v4` (ADR 0005) is NOT yet a dep
  — it arrives with Plan 002's retry backoff. Test-only: `testify`, `goleak`.

## 3. Traceability pointers (read in this order)

1. `CLAUDE.md` — project rules (Go 1.25 exact, gopls, custom test skills, never commit/push without
   approval, minimal deps, workflow gates, commit discipline).
2. `docs/specs/001-messaging-core.md` — the normative design.
3. `docs/adrs/0001`–`0006` — ratified decisions.
4. `docs/plans/001-core-foundation.md` — the just-completed plan (and its "Notes for Plan 002").

## 4. Decisions & deviations this session

- **Test deps added incrementally** (as each task hit the import) rather than in a final Task 9; Task 9's
  `go mod tidy` was consequently a no-op. No separate deps commit.
- **Panic handling (Task 8):** `safeHandle` returns a local `fmt.Errorf("msgin: handler panicked: %v", r)`
  rather than misusing the public `ErrPayloadType` sentinel. The **public** panic-error sentinel + hook
  remain deferred to Plan 002 as designed.
- **Determinism:** the memory adapter's ctx-cancel-mid-delivery test uses Go 1.25 `testing/synctest`
  (not a wall-clock sleep); the consumer's live-value-mismatch test uses a single-delivery fake, not a
  hot requeue loop.
- **Gate fixes folded in** (not separate commits): wrap the consumer decode error (`%w ErrPayloadDecode`),
  clamp `memory.WithBuffer(n<0)` to 0 (no panic on caller input), extract the shared `resolveCodec`
  helper, drop a redundant conversion.
- **Commit granularity (user directive):** logical-feature commits, no micro-commits; dev-branch fixes
  amended rather than piled on; branch fast-forwarded to `main` and pushed.

## 5. OPEN QUESTION for the user (non-blocking; decide before/with Plan 002)

- **`ErrNoPayloadCodec` is currently an unused exported sentinel.** Plan 001's producer/consumer default
  wire+no-codec to JSON (matching the documented "default JSON codec" ergonomics) instead of erroring.
  The brief's prose had said wire+no-codec should error via `ErrNoPayloadCodec`; the code (kept)
  defaults to JSON. Decide: (a) keep default-JSON + keep the sentinel reserved for a later wire source,
  (b) error on wire+no-codec, or (c) drop the sentinel until a task uses it. Also: **impose payload
  size/complexity limits on untrusted wire decoding** when the first wire adapter lands (Plan 004+) —
  not reachable in Plan 001 (memory carries live values, no decode).

## 6. Next actions (resume here)

1. Confirm `main` is green: `GOTOOLCHAIN=go1.25.0 go test ./... -race`.
2. Decide the §5 open question.
3. Brainstorm + spec-refine Plan 002 (reliability), record any new ADRs, Opus-audit, then write
   `docs/plans/002-*.md` and execute via SDD off a fresh `feat/*` branch from `main`.

## 7. Gotchas / environment

- **Go toolchain:** local default is newer (go1.26); the project pins **Go 1.25**. Always build/test with
  `GOTOOLCHAIN=go1.25.0`. `go.mod` has `go 1.25`, no `toolchain` directive.
- **gopls@latest cannot run under the 1.25 toolchain** (needs Go ≥1.26); subagents substituted
  `go build`/`vet`/`gofmt` + tests as diagnostics. Fine for this work; revisit if a task needs semantic nav.
- **Custom test skills (mandatory):** `table-test`, `use-mockgen`, `use-testcontainers` (the latter two
  become relevant from Plan 004). Blackbox tests only; one goleak `TestMain` per test package that starts
  goroutines.
- **SDD scratch** lives in `.superpowers/sdd/` (git-ignored): the progress ledger + per-task reports.
