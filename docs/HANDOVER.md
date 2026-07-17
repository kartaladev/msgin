# HANDOVER — msgin

> **Next session: read this first, then trust the referenced files over any memory.** Read, in order:
> `CLAUDE.md` (note the TWO new hard rules this session — see §4), then the active design bundle
> `docs/specs/002-sql-multi-module-and-sqlite.md` (§4–§8 are **authoritative, post-audit**),
> `docs/adrs/0011-sql-engine-dialect-module-split.md` (+ its reconciliation banner),
> `docs/plans/006-sql-engine-dialect-split.md` (+ its POST-AUDIT REVISION banner), and the audit record
> `.superpowers/sdd/plan-006-audit-round-1.md`. Also `docs/adrs/0010-poller-sql-adapter.md` (the shipped sql
> adapter this refactors) and the memory index.
>
> **This is a DESIGN safepoint** (the code tree is green at `e5abe40`; Plan 005 Task 10 is done + committed).
> The active work is a **design in progress** — the sql multi-module refactor — with audit round 1 folded and
> round 2 pending. No code has been written for it yet.

_Updated: Plan 005 Task 10 committed (through `e5abe40`); PIVOTED to the sql multi-module refactor (spec 002 /
ADR 0011 / Plan 006), audit round 1 folded into spec 002; resume by finishing the design → round-2 re-audit →
approval → implement._

## 1. Objective & roadmap position

`msgin` (`github.com/kartaladev/msgin`) — a Go 1.25 EIP library (minimal deps). Plans 001–004 on `main`
(@ `86ffa11`). Branch **`feat/poller-sql-adapter`**. **Plan 005 (Poller + `sql` adapter) Tasks 1–10 are
committed** (HEAD `e5abe40`).

**The user PIVOTED (this session):** Plan 005 **Task 11 (docs/examples) is PENDED**. New goal, in order:
1. **Increment A — refactor `adapter/database/sql` into a driver-free engine + per-dialect modules**
   (Plan 006 / ADR 0011 / spec 002). *Why:* a consumer's `go.sum` currently inherits pgx + mysql +
   testcontainers + docker/moby(24) + otel(10) even though the binary is driver-free (measured; Go 1.17
   pruning shields the binary, not `go.sum`).
2. **Increment B — add a SQLite dialect** (Plan 007 + ADR 0012, to be written).
3. **Resume Plan 005 Task 11** (docs/examples), now across the multi-module layout.

## 2. Exact state

- **Code:** clean green safepoint at `e5abe40` (`git log --oneline`): `e5abe40 docs: SDD+ask rule`,
  `e401cd4 feat(sql): InboxDeduper`, `0a305ee refactor(sql): rename Dialect→LeaseDialect`, then Plan-005
  Tasks 1–9. `GOTOOLCHAIN=go1.25.0 go test ./... -race` was green (root 98.9%, sql 91.8% incl. real MariaDB,
  memory 100%). **No refactor code exists yet.**
- **Design docs (working tree, likely UNCOMMITTED — commit them first thing, see §5):**
  `docs/specs/002-sql-multi-module-and-sqlite.md` (§4–§8 **authoritative, Structure Z, audit-folded**),
  `docs/adrs/0011-sql-engine-dialect-module-split.md` (banner: reconcile Decisions 2/4/5 to Structure Z),
  `docs/plans/006-sql-engine-dialect-split.md` (banner: revise tasks to Structure Z before running),
  `.superpowers/sdd/plan-006-audit-round-1.md` (the audit findings) + `.superpowers/sdd/task-10-decisions.md`
  (both gitignored — do not survive a fresh clone).
- **Also uncommitted:** `CLAUDE.md` (two new rules — §4), `docs/HANDOVER.md` (this file). Run
  `git status --short` to see the full set.

## 3. The design — SETTLED decisions (do NOT re-litigate; user-approved via AskUserQuestion)

**Increment A — engine/dialect split (spec 002 §4 = the layout, Structure Z):**
- **5 modules:** root `github.com/kartaladev/msgin` (the ENGINE — SPIs `LeaseDialect`/`LockDialect`/
  `InboxDialect`, `Source`/`Outbound`/`InboxDeduper`, framing, options, errors; driver-free) +
  `adapter/database/sql/harness` (pkg `harness`, requires engine + testify, **reusable conformance suite**
  `RunSource`/`RunLock`/`RunOutbound`/`RunOutbox`/`RunInbox`/`RunDialect` taking a per-dialect **`TestKit`**
  `{Lease; Quote func(string)string; Placeholder func(int)string; NowExpr func()string; …}`) +
  `adapter/database/sql/postgres` + `…/mysql` (prod dialects, **require the ENGINE ONLY** → clean consumer
  go.sum; `postgres.LeaseDialect()`/`DDL`/`InboxDDL`) + `adapter/database/sql/dbtest` (the **runner** — holds
  ALL drivers + testcontainers, runs the harness per engine against real containers; **nobody imports it**).
- **Explicit dialect, drop auto-detect:** constructors take the dialect as a required arg
  (`NewPollingSource(db, table, postgres.LeaseDialect(), opts…)`); DELETE `detect.go`/`resolveDialect`/
  `WithDialect`/`WithInboxDialect`/`ErrDialectUndetected`; ADD `ErrNilDialect`; EXPORT `ValidateIdent` +
  `BeginLockTx`/`SettleLockTx` (dialect impls need them — audit F2). `ddl.go` leaves root entirely incl.
  DELETING the `InboxDDL(d,table)` type-switch (audit F3); the `var _ …Dialect = …` compile-time assertions
  move into each dialect module.
- **`go.work` committed;** CI runs `GOWORK=off`; release = tag root FIRST, then dialect modules swap
  `replace`→pinned `require` + module-prefixed tags; **CI/release created from scratch** (none exist — audit
  F5). Author `RunSource` (lease) with correctness-only concurrency assertions so SQLite passes later (F8).

**Increment B — SQLite (spec 002 §7; ADR 0012 + Plan 007 to be written):** pure-Go `modernc.org/sqlite`
(cgo-free, no Docker); no `SKIP LOCKED`/`FOR UPDATE` → lease-only (`sqlite.LeaseDialect()` does NOT implement
`LockDialect` → `ErrLockStrategyUnsupported`); inbox dedup via `ON CONFLICT … RETURNING`.

## 4. Process rules ratified THIS session (now hard rules in CLAUDE.md — obey them)

- **"Prefer SDD, and ALWAYS ASK before implementation"** — never write implementation code without an explicit
  user go-ahead stating the execution mode; default to a fresh SDD implementer subagent per task; coordinator
  (main session) verifies green + commits; adversarial reviewer per task. (The coordinator self-implementing
  Task 10 without asking is what triggered this rule.)
- **"Adversarial design audit — AFTER the plan is written, over the whole spec+ADR+plan bundle, BEFORE any
  code"** — the plan is part of what gets audited (auditing spec+ADR alone misses plan-level flaws). Fold
  findings; re-audit if destabilized (2 rounds is the norm). **This is why round-2 is pending below.**

## 5. Next actions (resume here — the user wants this run CONTINUOUSLY & AUTONOMOUSLY)

1. **Commit the design safepoint first** (ask the user — never commit without approval): the design docs +
   `CLAUDE.md` rules + this handover, as a standalone `docs:`/`spec:` commit (specs precede code), so a fresh
   clone survives. Then confirm the code tree is still green: `GOTOOLCHAIN=go1.25.0 go test ./... -race`.
2. **Finish reconciling ADR 0011 + Plan 006 to Structure Z** — spec 002 §4–§8 is authoritative; the audit
   record lists every fix. Rewrite ADR Decisions 2/4/5 + Consequences, and REWRITE Plan 006's tasks (≈8 tasks:
   engine explicit-dialect API; the `harness` module; engine fake-dialect unit tests; `postgres` prod module +
   its `dbtest` tests; `mysql` prod module + `dbtest` tests; root-goes-lean; CI/release created; go.work).
3. **Round-2 adversarial Opus audit** over the reconciled spec+ADR+plan (per the CLAUDE.md rule). Fold; a
   3rd round only if destabilized.
4. **Get the user's approval of the audited Plan 006 + explicit authorization to execute autonomously** (per
   the ask-before-implementation rule). Once approved + SDD chosen, per-task commits are pre-authorized
   (CLAUDE.md exception) — so the session can then run Plan 006 → Plan 007 (write ADR 0012 + Plan 007 for
   SQLite; audit; implement) → resume Task 11, task-by-task via SDD, without pausing for each commit.
5. **Delivery:** the user reviews on GitHub — PUSH the branch (per-action approval needed for `git push`), do
   NOT ff-`main`. Whole-branch `/code-review` + `/security-review` gate before delivery. Update this handover
   + memory at each safepoint.

## 6. Gotchas / environment

- **Go 1.25 pinned:** always `GOTOOLCHAIN=go1.25.0`. `go.mod` directive is `go 1.25.0`; no `toolchain` line.
- **Docker MUST run** for the sql tests (Postgres + MySQL + MariaDB via testcontainers; the inbox suite runs
  all three). SQLite (increment B) needs NO Docker (embedded, `modernc.org/sqlite`).
- **Multi-module reality (increment A):** `go.work` for local dev; CI is `GOWORK=off`; dialect modules pin the
  root version (release: root-tagged-first). `go.sum` isolation is the acceptance gate — re-run the
  throwaway-consumer probe (spec 002 §1 method) and require a dialect-consumer `go.sum` free of
  testcontainers/docker/otel.
- **`.superpowers/sdd/` is gitignored** — the audit record + task decisions live there and do NOT survive a
  fresh clone. If lost, the audit findings are summarized in spec 002's status + this handover; re-run the
  audit if needed.
- **Custom skills (mandatory):** `table-test`, `use-mockgen`, `use-testcontainers`. Start Go work from
  `cc-skills-golang:golang-how-to`; use `gopls`/`LSP`. Blackbox `_test` packages.
