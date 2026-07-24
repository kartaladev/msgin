# Session handover — msgin

> **READ FIRST, before doing anything.** Read `CLAUDE.md` (root), then the traceability pointers in §3. Trust those
> files over this handover and over any memory. **Safepoint: on `main` @ `fa57091` (merge commit, PUSHED to `origin`).
> Plan 026 (HTTP SSE Phase 4, S-in SSE client) is DONE & MERGED. Working tree is clean but for the permanently-dirty
> `.claude/settings.json` (never commit it). `go test ./... -race` green, both `adapter/http` packages at 100%
> coverage.** There is NO active branch and NO in-flight task — the next increment (Phase 5 / gin) is not yet
> spec'd or planned.

## 1. Objective & roadmap position

`msgin` is a Go 1.25 Enterprise Integration Patterns library (`github.com/kartaladev/msgin`). The HTTP adapter's SSE
work is being delivered in phases: **Phase 3 (S-out SSE *server*) — MERGED** (Plan 025); **Phase 4 (S-in SSE
*client*) — MERGED** (Plan 026, this session). **The only remaining SSE-adjacent increment is Phase 5: the gin
binding (Plan 027), which does not yet exist** — no spec section, no plan, no ADR beyond the placeholders. Starting it
means brainstorming first (`superpowers:brainstorming`), then a spec/plan, then the mandatory adversarial design audit,
then SDD — the full CLAUDE.md loop.

## 2. Exact state

- **`main` @ `fa57091`** — `Merge branch 'feat/http-sse-client'` (Plan 026), pushed to `origin/main`. The branch
  `feat/http-sse-client` has been **deleted** (local; it was never pushed to origin).
- Plan 026's 6 commits (now on `main` via the merge): `1ea29bc` (Task 0 spec/ADR deltas), `cd5befc` (handover),
  `973cc86` (options+constructor), `496c7c9` (Stream), `953912b` (hardening+watchdog), `ba282bf` (e2e+docs+100%+gate fixes).
- `git status --short`: only ` M .claude/settings.json` (permanently dirty — NEVER commit).
- Delivery gate that cleared the merge: whole-branch `/code-review` (opus) = READY TO MERGE; `/security-review` (opus)
  = no high-confidence vulns; **5/5 mutation spot-checks** confirm the INV-C1/C2/C4/C7 + MAJOR-1 tests are load-bearing;
  100% coverage; `-race` green; golangci-lint 0; govulncheck clean; `go mod tidy` no-op.
- **This handover (`docs/HANDOVER.md`) is currently UNCOMMITTED** on `main` — offer to commit it as a standalone
  `docs:` commit if a fresh clone/machine needs it (approval-gated).

## 3. Traceability pointers (read first, in this order)

1. `CLAUDE.md` (root) — the workflow, gates, and conventions that OVERRIDE defaults.
2. `docs/specs/011-http-adapter.md` — the HTTP adapter spec; §3.5/§3.6/§4/§7 (SSE), §3.0 (layout).
3. `docs/adrs/0023-http-channel-adapter.md` — Addendum C (SSE decisions): C1–C8, C7 (client placement), and the
   dated "Pacing precedence" note (server `retry:` wins over event-reset — user-decided 2026-07-24).
4. `docs/plans/025-http-sse-server.md` and `docs/plans/026-http-sse-client.md` — the two delivered SSE plans.
5. `.superpowers/sdd/progress.md` — Plan 026's SDD ledger (git-ignored scratch): per-task commits, review outcomes,
   the full delivery-gate record (mutation results, the G1 fix). Recover from `git log` if `git clean -fdx` wipes it.

## 4. Decisions & deviations this session

- **Reconnect-backoff precedence (user-decided 2026-07-24):** `hasRetry > gotEvent > doubling` — a server `retry:`
  directive takes precedence over the event-reset-to-min heuristic when a connection both emits an event and carries a
  `retry:`. Corrected the plan text (which said reset-to-min unconditionally) + ADR 0023 Addendum C.
- **Controller scope split (Task 2↔3):** the `WithReadTimeout` idle watchdog code was deferred from Task 2 to Task 3
  so it landed TDD-first alongside its tests; Task 2 built the rest of the `Stream` loop.
- **Gate finding G1 (mutation-surfaced):** the MAJOR-1 "no-Timeout default" test was NOT load-bearing (its ctx window
  was shorter than the reconnect backoff, masking a finite-Timeout abort). Fixed by a short `WithReconnectBackoff` in
  the test; re-verified the mutation now goes RED. Both whole-branch reviews had passed the branch clean — the mutation
  gate is what caught it.
- No pending approvals; no open questions. The merge was user-approved and executed.

## 5. Next actions

**No in-flight work.** To start the next increment (Phase 5 / gin binding, Plan 027):
1. Branch off `main`: `git checkout -b feat/http-sse-gin` (or similar) — do NOT work on `main`.
2. `superpowers:brainstorming` to settle the gin-binding design (it mirrors `adapter/http/stdlib`'s net/http binding;
   note ADR 0023 B4/C7 already argued a gin SSE *client* has no place — Phase 5 is the *server*-side gin binding).
3. Write the spec delta (`docs/specs/011` §Phase 5) + `docs/plans/027-*` + any ADR, then run the **mandatory
   adversarial design audit** (fresh Opus subagent, full spec+ADR+plan bundle) BEFORE any code. ASK before implementing.
4. Execute via SDD; close with the whole-branch `/code-review` + `/security-review` + mutation gate, then merge (approval-gated).

If instead the user wants something else, there is no half-done state to resume — start from their request + the loop above.

## 6. Gotchas / environment

- Go 1.25 pin: always `GOTOOLCHAIN=go1.25.12` (bare `go1.25` rejected). `govulncheck` at `$(go env GOPATH)/bin/govulncheck`;
  `gofumpt` not installed (`gofmt` only — use `test -z "$(gofmt -l .)"`); `golangci-lint` on PATH (`0 issues`);
  `gopls`/`LSP` at `$(go env GOPATH)/bin/gopls` (was NOT available inside SDD subagents — they read source directly).
- All SSE code (core, server, client) lives in `package msghttp` (`adapter/http/sse.go`, `sse_server.go`, `sseclient.go`);
  `adapter/http/stdlib` is the thin net/http binding. Tests are blackbox `package msghttp_test`; `goleak.VerifyTestMain`
  is wired in `encode_test.go` — no per-test `goleak.VerifyNone`.
- `.claude/settings.json` is permanently dirty — never commit it. Stage explicit pathspecs, never `git add .`.
- Repo has zero git tags — do NOT propose tagging (unreleased, no consumers).
