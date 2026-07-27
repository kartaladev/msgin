# Session handover — msgin

> **READ FIRST, before doing anything.** Read `CLAUDE.md` (root), then the traceability pointers in §3. Trust
> those files over this handover and over any memory. **Safepoint: on branch `claude/repo-structure-refactor-jt79t1`
> @ `2463a4f`, NOT pushed.** No Go code changed this session — the diff is documentation only, `go build ./...`
> is green, and the test suite is untouched from `main`'s last green state. **The next gate is the mandatory
> adversarial design audit on the Plan 027 bundle; no implementation code may be written before it passes.**

## 1. Objective & roadmap position

`msgin` is a Go 1.25 Enterprise Integration Patterns library (`github.com/kartaladev/msgin`). The HTTP/SSE work
is complete (Plans 025/026 merged). **The active effort is the pre-v1 refactor program** — five RFCs covering a
core package restructure, EIP lexical alignment, endpoint behavior types, trigger-driven scheduling, and five
missing EIP components.

**All five RFCs are now Accepted** (22 open questions settled 2026-07-27), and **the first slice is promoted**
to Spec 014 + ADRs 0027–0029 + Plan 027. Nothing is implemented.

**Sequencing decision: the breaking window runs FIRST, ahead of the feature roadmap.** The gin binding
(previously slated as "Plan 027", now a later plan number) and any `pgx`/`redis`/`nats` adapter wait — every
adapter landed before the window enlarges its blast radius, and the C-full decision removed the "land the
non-breaking slices early" mitigation the RFC index had relied on.

## 2. Exact state

- Branch `claude/repo-structure-refactor-jt79t1`, two commits ahead of `main` (`6f44db6`), **not pushed**:
  - `a54f023` — `docs(rfcs): settle all open questions; accept the refactor program`
  - `2463a4f` — `docs: promote the first refactor slice to spec 014, ADRs 0027-0029, plan 027`
- `git status --short`: ` M CLAUDE.md` only.
- **`CLAUDE.md` is modified, uncommitted, and is NOT this session's change** — it was already dirty at session
  start (a project-status refresh: greenfield → pre-v1, corrected adapter list, `go 1.25.0` wording). It was
  deliberately kept out of both commits. Decide whether to commit it; it is unrelated to the RFC work.
- No code changed. `GOTOOLCHAIN=go1.25.12 go build ./...` green.

## 3. Traceability pointers (read first, in this order)

1. `CLAUDE.md` (root) — workflow, gates, conventions that OVERRIDE defaults.
2. `docs/rfcs/README.md` — program index, **the decided package layout**, promotion status, sequencing.
3. `docs/rfcs/0001`–`0005` — each RFC's **§7 Decisions** records what was settled and why. Read §7 *before* §3:
   several §3 passages predate the decisions and carry dated resolution notes rather than being rewritten.
4. `docs/specs/014-core-package-layout.md` — the contract for the first slice.
5. `docs/adrs/0027` (layout, C-full, clean break), `0028` (channel segregation), `0029` (renames, behavior
   types, expr module).
6. `docs/plans/027-core-package-layout.md` — 13 tasks, green per increment.

## 4. Decisions & deviations this session

All 22 RFC open questions were settled with the user, one decision at a time. The load-bearing ones:

- **C-full** — the engine leaves root in this window; C-full is *chosen*, not deferred, to avoid buying a second
  breaking window for a change already known to be wanted.
- **EIP-chapter packages** — `endpoint`/`routing`/`transform` (+ `channel`, `resilience`). This corrected a
  fidelity bug in RFC-0001's draft, which filed five ch.7/ch.8 patterns under a package named `endpoint`.
- **Channel segregation** — `MessageChannel` (send-only) + `SubscribableChannel`; `PollableChannel` deliberately
  omitted (it would duplicate `PollingSource` with no caller); `DirectChannel.Subscribe` gains a `Subscription`
  return so both subscribable channels satisfy one contract.
- **Named func types with combinator methods**, not interfaces; names drop the qualifier the package already
  carries (`routing.Predicate`, not `FilterPredicate` or Spring's `MessageSelector`).
- **`expr-lang` leaves to its own module; `robfig/cron` stays in root.** Reconciled by a stated rule: *a
  zero-transitive dependency is pushed to its own module when its weight is material to consumers who don't use
  it* — 7.1 MB vs 144 KB.
- **`DedupStore` two-phase Claim/Settle with a lease** (the RFC-0005 OQ6 blocker), complementing the existing
  tx-coupled `InboxDeduper` path rather than replacing it; **`MessageGroupStore.SettleMembers`** pulled into
  this window.

**Deviations from my recommendations, at the user's direction** — all recorded with their costs in the RFCs:
`Poller` is exported publicly; `Once` ships in the v1 trigger set; Content Enricher is a full endpoint (S→M,
re-opening a gate RFC-0005's risk table had set); `robfig` stays in-module, which **deleted** RFC-0004's own
success metric rather than reworded it.

**Pending approvals / open questions:**

- **Nothing is pushed.** Pushing needs explicit approval.
- **The adversarial design audit has NOT run.** Hard CLAUDE.md gate before any implementation code.
- **Two items are deliberately left for the audit to settle, not resolved:**
  1. ADR 0029 §2 asserts *from recall* that Spring Integration names its equivalent interface
     `RequestReplyExchanger`. This gates the "keep `Exchange`" decision and **must be verified**; if it is
     false, that decision reverts to a rename and Plan 027 Task 3 changes shape.
  2. `SettleMembers` ships with **no in-tree caller**, justified solely by breaking-window timing. Plan 027
     Task 11 explicitly invites the audit to reject it.

## 5. Next actions

1. **Run the adversarial design audit** on the complete bundle — Spec 014 + ADR 0027 + ADR 0028 + ADR 0029 +
   Plan 027 handed to a fresh Opus subagent **together** (CLAUDE.md: auditing spec+ADR without the plan misses
   plan-level flaws — task decomposition, coverage gaps, sequencing, test strategy, sizing). Fold every material
   finding back; re-audit if the fixes destabilise the design (two rounds is this project's norm).
2. Get the user's approval on the audited plan and confirm the execution mode (SDD is the default; direct
   main-session implementation requires explicit per-task approval).
3. Branch `refactor/core-package-layout` from a quiet `main` and start Plan 027 Task 0 (baseline + move-list).

## 6. Gotchas & environment

- **`export GOTOOLCHAIN=go1.25.12`** always (bare `go1.25` is rejected — a language, not a toolchain, version).
- **`./...` is not the repo.** Seven modules today, eight once `expr` lands; the gate is the per-module
  `GOWORK=off` loop in CLAUDE.md's Commands section, exactly as CI runs it.
- Tooling paths: `govulncheck` at `$(go env GOPATH)/bin/govulncheck`; **`gofumpt` is NOT installed** — use
  `test -z "$(gofmt -l .)"`; `golangci-lint` on PATH; `gopls`/`LSP` at `$(go env GOPATH)/bin/gopls`. **`gopls`
  was NOT available inside SDD subagents in past sessions** — they read source directly. Plan 027 leans hard on
  `gopls` for the moves, so **verify subagent `gopls` access before dispatching Tasks 4–8**, or run those moves
  from the main session with per-task approval.
- `dbtest` and `crontest` need a running Docker daemon.
- **Plan 027 Task 1 leaves expression support absent from the branch** until Task 10 restores it via the `expr`
  module. That is deliberate (Spec 014 §7) — do not "fix" it by reinstating the `*Expr` constructors.
- `.claude/settings.json` has been permanently dirty in past sessions — never commit it. Stage explicit
  pathspecs, never `git add .`. (It happened to be clean this session; only `CLAUDE.md` was dirty.)
- Repo has **zero git tags** — do NOT propose tagging (unreleased, no consumers). This is what makes every break
  in Spec 014 affordable.
