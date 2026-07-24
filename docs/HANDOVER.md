# Session handover — msgin

> **READ FIRST, before doing anything.** Read `CLAUDE.md` (root), then §3's artifacts. Trust those files over this
> handover and over any memory. **Safepoint: `main` @ `5197382` (merge commit) — Plan 025 (HTTP SSE Phase 3, S-out
> server) is MERGED. `go test ./... -race` green, `adapter/http` + `adapter/http/stdlib` both 100.0% coverage,
> lint/vet/fmt/govulncheck clean. `main` is LOCAL-ONLY (origin/main = `51330e9`, behind — NOT pushed).** Working
> tree carries only the two pre-existing unrelated edits (`.claude/settings.json`, `docs/HANDOVER.md`) — never
> commit those. **Next increment: Plan 026 (SSE client).**

## 1. Objective & roadmap position

**Spec 011 Phase 3 (SSE server) — MERGED to `main` @ `5197382` (branch `feat/http-sse-server` deleted).** Phase 4
(SSE client, Plan 026) is the next increment.

Workflow position: brainstorm ✅ → spec/ADR ✅ → plans 025/026 ✅ → **3 adversarial design audits ✅** (all folded)
→ **SDD execution of Plan 025 ✅** (7 tasks + the C8 placement fix, per-task review each) → **whole-branch delivery
gate ✅** (code-review + security-review + mutation spot-checks) → **MERGED ✅.** Remaining, user-gated: (a) push
`main` to `origin`; (b) start Plan 026.

## 2. Exact state

- Branch **`feat/http-sse-server`** off `main` @ `2b4ee1d`. **9 commits** (`git log --oneline main..HEAD`):
  - `2c71535` docs — plans 025/026 + Plan-025 spec/ADR deltas (Task 0)
  - `a8192d3` feat — SSE encode core (Task 1)
  - `65e5535` feat — WHATWG SSE parser + fuzz (Task 2)
  - `8872f4d` feat — server option surface + sentinels (Task 3)
  - `222621a` docs — **ADR 0023 Addendum C8**: SSEServer lives in `msghttp`, not `stdlib` (mid-impl placement fix)
  - `d0a379d` feat — SSEServer lifecycle: ServeHTTP/heartbeat/Close/write-deadline (Task 4)
  - `988128c` feat — Send fan-out + slow-client policy + replay ring (Task 5)
  - `0d23a0d` docs — examples + deployment docs (Task 6)
  - `ecc1594` **fix** — SSE data-channel CR-injection + godoc/test hygiene (whole-branch review)
- `main` is **LOCAL-ONLY**, not pushed (`origin/main` = `51330e9`, local `main` = `2b4ee1d`). The whole SSE branch
  is local-only.
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

## 5. Next actions (precise)

1. ~~Merge `feat/http-sse-server` → `main`~~ **DONE** (`5197382`, `--no-ff`; branch deleted).
2. Offer to push `main` (all of `2b4ee1d..5197382`) to `origin` — push needs explicit approval; `origin/main` is
   behind at `51330e9`.
3. **Plan 026 (Phase 4, SSE client)** is the next increment: fresh brainstorm/audit already done (plan committed);
   its Task 0 folds the client-specific spec/ADR deltas (C7 placement, terminal `ErrNotEventStream`, no-`Timeout`
   client, `WithReadTimeout`) that Plan 025 Task 0 deliberately deferred. Start from a fresh branch off the merged
   `main`, run its adversarial audit if not already, then SDD.

## 6. Gotchas / environment

- Go 1.25 pin: always `GOTOOLCHAIN=go1.25.12`. `govulncheck` at `$(go env GOPATH)/bin/govulncheck`; `gofumpt` not
  installed (`gofmt` only — `test -z "$(gofmt -l .)"`); `golangci-lint` on PATH (`0 issues`). `gopls`/`LSP` was NOT
  available inside the SDD subagents (they verified symbols by reading source); it IS at `$(go env GOPATH)/bin/gopls`.
- SSE server + core now BOTH in `package msghttp` (`adapter/http/sse.go`, `sse_server.go`); `stdlib` untouched by
  this increment. Server tests are `package msghttp_test`, sharing `encode_test.go`'s `goleak.VerifyTestMain`.
- The SDD ledger + task briefs/reports/diffs live under `.superpowers/sdd/` (git-ignored; `git clean -fdx` destroys
  them — recover from `git log`).
- Leave `.claude/settings.json` alone. Stage explicit pathspecs, never `git add .`.
