# Session handover — msgin

> **READ FIRST, before doing anything.** Read `CLAUDE.md` (root) and then the governing artifacts named in
> §3 below — **`docs/specs/011-http-adapter.md`**, **`docs/adrs/0023-http-channel-adapter.md`**, and
> **`docs/plans/020-http-adapter-inbound.md`** — and trust those files over anything in this handover or in memory.
> This document orients you; the spec/ADR/plan are the source of truth. The design is at a clean safepoint: **design
> docs are authored, adversarially audited, and committed — there is NO implementation code yet.**

## 1. Objective & roadmap position

**Building:** the **HTTP channel adapter** for `msgin` (Spec 011) — the sixth adapter family, and the first external
transport for the `RequestReplyExchange` SPI shipped in Plan 019.

**Structure:** one spec, **five phased plans**, one merged increment per phase (the Spec 009 → Plans 015-018 pattern):

| Phase | Plan | Content | Status |
|-------|------|---------|--------|
| **1** | **020** | `adapter/http` core (`msghttp`) + `adapter/http/stdlib` inbound: **I1** async + **I2** sync gateway → `http.Handler` | **design done + audited; NO CODE** |
| 2 | 021 | `adapter/http/stdlib` outbound: **O1** webhook + **O2** request-reply (`RequestReplyExchange`, Return Address) | not started |
| 3 | 022 | SSE **server** (S-out) | not started |
| 4 | 023 | SSE **client** (S-in, `StreamingSource`) | not started |
| 5 | 024 | `adapter/http/gin` module + **ADR 0024** (gin dependency) | not started |

**Active position: Plan 020, pre-implementation.** The immediate next step is to get the **user's explicit go-ahead**
and then implement Plan 020 via **SDD** (CLAUDE.md hard rule: never start implementation code without asking).

## 2. Exact state

- **Branch:** `feat/http-adapter-inbound` (off `main` @ `57b3ffd`), **3 commits ahead**, not pushed, not merged.
- **`git status --short`:** only ` M .claude/settings.json` — a **pre-existing intentional** uncommitted local
  settings change (present since before this increment; leave it alone, keep it out of commits).
- **Last commits (newest first):**
  - `fb9d48b` docs: ADR 0023 + Plan 020 — HTTP adapter Phase 1 (inbound server), Opus-audited
  - `8df9611` spec: add Spec 011 — HTTP channel adapter (inbound + outbound + SSE, stdlib & gin)
  - `7f9b544` docs: HANDOVER … (the previous increment's handover, on `main`)
- **Safepoint:** yes. No source files were touched — the tree builds and the existing test suite is unaffected (this
  session only added Markdown under `docs/`). No half-done edits.
- **Design docs were committed standalone AHEAD of code** — the CLAUDE.md handoff exception: the audited spec/ADR/plan
  survive a fresh clone / tree wipe. When you implement, each Plan-020 task's code lands in its own `feat(http)` commit
  carrying `Spec: 011` / `Plan: 020` / `ADR: 0023` trailers (the plan spells out the exact commit messages).

## 3. Traceability pointers — read these first (in order)

1. `CLAUDE.md` (root) — the mandatory workflow, dependency policy, multi-instance rule, sensible-defaults + coverage gates.
2. `docs/specs/011-http-adapter.md` — the whole HTTP adapter surface: six modes, five phases, delivery guarantees, security.
3. `docs/adrs/0023-http-channel-adapter.md` — the architecture: framework-agnostic core + stdlib/gin bindings; the
   Return-Address-by-construction reasoning; the outbound classification; the per-mode SPI mapping.
4. `docs/plans/020-http-adapter-inbound.md` — the Phase-1 implementation plan (4 tasks, hot-path branches, test strategy).
5. Context the design reuses: `docs/adrs/0022-messaging-gateway.md` + `docs/specs/010-messaging-gateway.md`
   (the `RequestReplyExchange` SPI), `docs/adrs/0002-adapter-spi.md`, `docs/adrs/0001-message-payload-typing.md`.
   Source seams the plan binds to: `exchange.go` (`RequestReplyExchange`/`ChannelExchange`), `channel.go`
   (`MessageChannel`/`DirectChannel`), `message.go` (`New`/`WithHeaders`/`ID`/header constants), `spi.go`,
   `handler.go` (`To`/`Chain`), `errors.go` (the exported gateway sentinels).

## 4. Decisions, deviations & pending approvals

**Decisions settled with the user this session (in brainstorming):**
- One spec, five **phased** plans; foundational-first order (inbound → outbound → SSE server → SSE client → gin).
- **Both** SSE directions (server *and* client).
- **Framework-agnostic core + separate gin module**, with the stdlib binding placed at **`adapter/http/stdlib`** (the
  user explicitly asked for this path, symmetric with `adapter/http/gin`). The core package is `msghttp` (dir
  `adapter/http`) — named `msghttp`, not `http`, to avoid the `net/http` import clash.

**Adversarial design audit — DONE for Phase 1 (round 1, Opus, fresh context): verdict SOUND-WITH-NITS.** Architecture
verified sound against the actual code (SPI reuse, Return-Address reasoning, every load-bearing API). Four must-fix
items and all nits were **folded** into the spec/ADR/plan already committed:
- **H1** — the request→message path uses **`msgin.New`** (fresh id+timestamp), not `NewMessage`. The spec had said
  `NewMessage`; following it would have left `ID()` empty and — since the default correlation id is the message's own
  `ID()` — made **every I2 request fail `ErrNoCorrelation`→400**. Corrected in Spec §3.2 / ADR §2.
- **M1** — I2 success status is **200** (never 202); `WithSuccessStatus` governs **I1 only**.
- **M2** — the custom `WithErrorStatus` branch is factored into a shared `statusFor` helper and given covering tests.
- **M3** — an `errReader` fixture covers the non-oversize read-error → 400 branch (distinct from oversize → 413).
- Nits **M4** (inbound-payload-is-`[]byte` / no inbound codec seam, documented symmetrically), **L1** (I1 over a
  `DirectChannel` is synchronous + `reqCtx`-coupled; steer at-least-once users to `QueueChannel`), **L2** (honest
  error→status: `ErrNoCorrelation`→500, `ErrDuplicateCorrelation`→409), **L3** (`ErrNilTarget` justified vs
  `msgin.ErrNilChannel`), **L4** (extract reply bytes before writing any header), **L5** (reserved-strip test uses
  `delivery-count`), **L6** (method-not-restricted godoc). **P1** (1 MiB body-cap default) confirmed defensible.
- **No re-audit round warranted** (the status-model change did not ripple into new option surface).

**Pending approval (BLOCKING the next step):** the user has **not yet given the go-ahead to write implementation
code.** Per CLAUDE.md this is a hard gate — you must ask first, and state the intended execution mode (SDD). Do not
start coding Plan 020 without it.

**One open point deliberately deferred to Plan 021 (Phase 2), not blocking Phase 1:** whether the `Producer`/outbound
path already applies a `RetryPolicy` to `OutboundAdapter.Send`. If it does, O1/O2 add no adapter-side backoff
(reliability stays runtime-owned, ADR 0002); if not, Phase 2 adds a thin producer-side retry. Recorded in ADR 0023 §5.

## 5. Next actions

1. **Ask the user for the go-ahead to implement Plan 020, via SDD** (state the execution mode). Wait for explicit
   approval — this is the CLAUDE.md gate.
2. On approval, execute **Plan 020** with `superpowers:subagent-driven-development`: a fresh implementer subagent per
   task (TDD red→green, starting from `cc-skills-golang:golang-how-to`, `gopls`, the `table-test` override), the
   coordinator verifies green + commits each task (per-task commits are pre-authorized once the plan+mode are chosen),
   then an adversarial reviewer per task. The four tasks:
   - **Task 1** — `msghttp` core: `Config`/`Option`/`WithX`, sentinels, `DecodeRequest`/`EncodeResponse`.
   - **Task 2** — I1 async: `ServeAsync` + `stdlib.NewInbound`.
   - **Task 3** — I2 sync gateway: `ServeGateway` + `stdlib.NewInboundGateway` + `Register`.
   - **Task 4** — `Example` tests + whole-branch gate (**`/code-review` + `/security-review`** — inbound is the
     untrusted boundary — + `-race`/lint/vet/govulncheck/CGO0/tidy, coverage ≥85% with every hot-path branch tested).
3. On green + gate-clean, ask the user before merging; then merge `--no-ff` to `main` and delete the branch
   (`git branch -d`). Phases 2-5 each start fresh off `main` with their own brainstorm-delta → plan/ADR →
   **adversarial audit** → ask → SDD.
4. Build/test with `GOTOOLCHAIN=go1.25.12` (the local default is newer; the module pins Go 1.25).

## 6. Gotchas / environment

- **Go 1.25 pin:** always `GOTOOLCHAIN=go1.25.12` for build/test (bare `go1.25` is rejected).
- **`adapter/http` + `adapter/http/stdlib` are ROOT-module packages** (like `adapter/memory`, `adapter/cron`) —
  **no new `go.mod`, no `go.work` change, no new dependency** in Phase 1. The gin dependency + its module land only in
  Phase 5 (ADR 0024).
- **Package name is `msghttp`** (directory `adapter/http`) to avoid shadowing `net/http`; import with that alias.
- **Tests are fully hermetic** — `httptest.Server` / `httptest.ResponseRecorder`, **no testcontainers** (a deliberate
  contrast with the SQL adapter). `goleak` `VerifyTestMain` in both new packages even though Phase-1 handlers start no
  goroutines (SSE, Phase 3, is the first mode that owns goroutines).
- **Blackbox tests only** (`package msghttp_test`, `package stdlib_test`); assert-closure tables; `t.Context()`.
- **Inbound is the untrusted boundary** — the Phase-1 whole-branch gate includes a real `/security-review`, not just
  `/code-review`.
- **Leave `.claude/settings.json` alone** — it is an intentional pre-existing local modification, not part of this work.
