# Session handover — msgin

> **READ FIRST, before doing anything.** Read `CLAUDE.md` (root), then §3's artifacts. Trust those files over this
> handover and over any memory. **Safepoint: on branch `feat/http-sse-client` @ `1ea29bc` — Plan 025 (SSE server)
> is MERGED to `main`/`origin` @ `0f1fe00`; Plan 026 (SSE client) is UNDERWAY: Task 0 (spec/ADR client deltas)
> committed, Tasks 1–4 (code) NOT started.** `go test ./... -race` green (no SSE-client code yet); working tree
> carries only the two pre-existing edits (`.claude/settings.json`, `docs/HANDOVER.md`) — never commit those.
> **RESUME AT: Plan 026 Task 1 (client option surface + constructor) via SDD.**

## 1. Objective & roadmap position

**Spec 011 Phase 3 (SSE server) — DONE & MERGED** (`main`/`origin` @ `0f1fe00`; the SSE-server work is
`2b4ee1d..5197382`). **Phase 4 (SSE client, Plan 026) — IN PROGRESS on branch `feat/http-sse-client`**: Task 0
committed (`1ea29bc`, client spec/ADR deltas); Tasks 1–4 remain.

Plan 026 design is ALREADY AUDITED — the 3-round combined 025+026 audit folded every client finding (MAJOR-1
no-`Timeout` client, MINOR-2 404-asymmetry, MINOR-4, and the F1 read-idle watchdog / INV-C7). **No fresh audit
needed — go straight to SDD for Tasks 1–4.**

Workflow position: design ✅ (3 audits) → Plan 025 SDD + gate + MERGE ✅ → **Plan 026 SDD: Task 0 ✅, Tasks 1–4 ⏸.**

## 2. Exact state

- **Plan 025 SHIPPED on `main` (pushed to `origin` @ `0f1fe00`).** The SSE-server increment is `2b4ee1d..5197382`
  (merge commit `5197382`; 9 commits: Task 0 `2c71535`, encode `a8192d3`, parser `65e5535`, options `8872f4d`,
  **C8** `222621a`, lifecycle `d0a379d`, Send `988128c`, docs `0d23a0d`, CR-injection fix `ecc1594`), then handover
  `0f1fe00`. `adapter/http` + `stdlib` both 100.0% coverage.
- **Now on branch `feat/http-sse-client`** (off `main` @ `0f1fe00`). One commit: `1ea29bc` docs — Plan 026 Task 0
  (client spec/ADR deltas: C7 placement, terminal `ErrNotEventStream`, no-`Timeout` client, `WithReadTimeout`).
  **No SSE-client CODE yet** (`adapter/http/sseclient.go` not created).
- The SDD ledger `.superpowers/sdd/progress.md` was reset for Plan 026 (Task 0 done; Tasks 1–4 pending).
- `git status --short`: only ` M .claude/settings.json` and ` M docs/HANDOVER.md` (both pre-existing, NEVER commit).

## 3. Traceability pointers (read in this order)

1. `CLAUDE.md`.
2. `docs/specs/011-http-adapter.md` §3.5/§3.6/§4/§7 (SSE).
3. `docs/adrs/0023-http-channel-adapter.md` Addendum C (C1–C6 + amendments) + **C8** (server placement).
4. `docs/plans/025-http-sse-server.md` (delivered) and `docs/plans/026-http-sse-client.md` (next increment).
5. `.superpowers/sdd/progress.md` — the SDD ledger (git-ignored scratch): per-task commits + review outcomes +
   the whole-branch gate record.

## 4. Decisions this session & how the gate performed

- **C8 (new, user-approved 2026-07-24):** the stateful `SSEServer` lives in `msghttp`, not `stdlib` — Task 4's
  implementer escalated that `stdlib` cannot read `msghttp`'s deliberately-unexported `Config`. Resolved by
  unexported same-package accessors, symmetric with the S-in client and `Outbound`.
- **Whole-branch security + code review BOTH independently caught one Critical/High bug** the 3 design audits, 6
  per-task reviews, and the mutation gate all missed: **bare-CR payload injection into the SSE `data:` channel**
  (INV-S1 was scoped to id/event only, never the data path). Fixed in `ecc1594` (normalize CR/LF/CRLF in data
  framing; INV-S1 widened to "message-derived bytes, header OR payload"). C1-mutation RED-confirmed.
- Accepted (documented, not fixed): parser ~2×-cap transient peak (M1, wording note added); `mu`-per-write only
  under a deadline-unsupported degraded config (M2); heartbeat-warning paraphrase (T3).

## 5. Next actions (precise) — RESUME PLAN 026 HERE

**You are on `feat/http-sse-client`, Task 0 committed. Run Plan 026 Tasks 1–4 via `superpowers:subagent-driven-development`,
exactly as Plan 025 was executed this session.** For EACH task: `scripts/task-brief docs/plans/026-http-sse-client.md N`
→ dispatch a fresh implementer subagent (the brief + the plan's "Final exported surface" block + Global Constraints +
the relevant INV-C1…C7) → on DONE, `scripts/review-package BASE HEAD` → dispatch a task reviewer → fix loop if
Critical/Important → mark the ledger. Then Task 4's delivery gate (whole-branch `/code-review` + `/security-review` +
mutation spot-checks on INV-C1…C7 + coverage/fuzz/`-race`/govulncheck), then `finishing-a-development-branch`
(merge = approval-gated). SDD scripts: `/Users/zakyalvan/.claude/plugins/cache/claude-plugins-official/superpowers/6.1.1/skills/subagent-driven-development/scripts/`.

- **Task 1** — client option surface + constructor: `NewSSEClient`, `WithConnectHeaders`, `WithReconnectBackoff`,
  `WithReadTimeout` + 3 sentinels, `resolveSSEClient` (the **no-`Timeout`** resolution + `httpClientSet` flag set
  ONLY inside `WithHTTPClient`'s `if c != nil` guard — audit F3). Reuse `validateURL`; do NOT reuse `resolveClient`
  (it carries the 30 s default — MAJOR-1). `SSEClient` type + `NativeReliability` methods; `Stream` lands in Task 2.
- **Task 2** — `Stream`: WHATWG triage (INV-C1: 200+event-stream → emit; 204 → nil; 200+wrong-CT → terminal
  `ErrNotEventStream`; non-2xx → reconnect), clamped reconnect (INV-C4), `Last-Event-ID` resume, the F1/INV-C7
  read-idle watchdog (`time.AfterFunc(d, ccancel)`, reset per `Read`, parent-ctx = terminal / `cctx` = reconnect).
- **Task 3** — hardening: cancellation (INV-C5), caps (INV-C3), redaction (INV-C2), no-follow (INV-C6), read-timeout.
- **Task 4** — cross-phase e2e (`SSEServer`↔`SSEClient` resume, both in `msghttp` now), `ExampleNewSSEClient`, docs,
  delivery gate.

Watch for: the F1/F2/F3 subtleties are pinned in the plan (INV-C7 mechanism block) — the watchdog is real-time (not
clockwork), the terminal-vs-reconnect keys off the PARENT ctx, reset-on-`Read` (not per-event). blackbox
`package msghttp_test`; existing `goleak.VerifyTestMain`.

### (background) earlier next-actions, now done
1. ~~Merge `feat/http-sse-server`~~ DONE (`5197382`). 2. ~~Push `main`~~ DONE (`0f1fe00` on `origin`). 3. ~~Plan 026
   Task 0~~ DONE (`1ea29bc`). After Plan 026 merges, delete `feat/http-sse-client`; Phase 5 (gin, Plan 027) is the
   only remaining SSE-adjacent increment.

## 6. Gotchas / environment

- Go 1.25 pin: always `GOTOOLCHAIN=go1.25.12`. `govulncheck` at `$(go env GOPATH)/bin/govulncheck`; `gofumpt` not
  installed (`gofmt` only — `test -z "$(gofmt -l .)"`); `golangci-lint` on PATH (`0 issues`). `gopls`/`LSP` was NOT
  available inside the SDD subagents (they verified symbols by reading source); it IS at `$(go env GOPATH)/bin/gopls`.
- SSE core + server (and the client, to be added) ALL live in `package msghttp` (`adapter/http/sse.go`,
  `sse_server.go`, `sseclient.go`); `stdlib` untouched by Phases 3/4 (ADR 0023 C7/C8). Tests are `package
  msghttp_test`, sharing `encode_test.go`'s `goleak.VerifyTestMain`.
- The SDD ledger + task briefs/reports/diffs live under `.superpowers/sdd/` (git-ignored; `git clean -fdx` destroys
  them — recover from `git log`).
- Leave `.claude/settings.json` alone. Stage explicit pathspecs, never `git add .`.
