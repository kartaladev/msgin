# HANDOVER — msgin

> **Next session: read this first, then trust the referenced files over any memory.** Read, in order:
> `CLAUDE.md`, then the **audited, implementation-ready** design bundle for the active increment —
> `docs/specs/006-cron-source.md`, `docs/adrs/0016-robfig-cron-dependency.md`, `docs/adrs/0017-cron-source.md`,
> and the plan `docs/plans/011-cron-source.md`. **The design-time gate is DONE** (both adversarial audit rounds
> complete). The active work is **executing Plan 011 via SDD, starting at Task 1** — §5 below.

_Updated 2026-07-19: **Plan 011 (cron source + distributed coordination) is WRITTEN and TWO-ROUND ADVERSARIALLY
AUDITED; all findings folded; the whole design bundle is committed.** User approved **SDD execution** and a
standalone **docs: commit** of the bundle. Next: execute Task 1 via `superpowers:subagent-driven-development`
(fresh implementer → coordinator verify+commit → adversarial reviewer). This is a clean design safepoint — docs
committed, no `.go` changes yet, tree builds._

## 1. Objective & roadmap position

`msgin` (`github.com/kartaladev/msgin`) — Go 1.25 EIP library, minimal deps, multi-module monorepo. `main` @
`410a45e` carries Plans 001–010 (core + reliability + resilience + `sql`/`memory`/dialects + composition Phase 1 +
Phase 3 Publish-Subscribe + scheduled/delayed send), all merged.

**Active increment = Spec 006 — cron / recurring message source + distributed coordination.** A recurring/cron
`Source[T]` (a `StreamingSource`) that emits a caller-defined message on each schedule fire, driven by the existing
runtime; PLUS msgin-native multi-instance single-fire via an `Elector` (leader) + `Locker` (per-fire) seam with
**dependency-free SQL-backed implementations of both** (PG/MySQL/SQLite). Un-defers Spec 005 O5-5.

**Roadmap after this:** (Spec 006 open items) Redis/etcd-backed coordinators (O6-1, optional modules), seconds-field
cron `WithSeconds` (O6-2); plus still-deferred: EIP `Delayer` composition step (Spec 005 O5-1), memory delayed-send
(O5-2), pgx/redis/nats/http adapters, Plan 005 T11 examples, Phase-4 fluent DSL (gated).

## 2. Exact state (design safepoint — bundle audited + committed, no code yet)

- **Branch:** `feat/cron-source` (off `main` @ `410a45e`).
- **Design bundle COMMITTED** as a standalone `docs:` commit (fresh-session-handoff exception, user-approved):
  `docs/specs/006-cron-source.md` (audit-fold edits), `docs/adrs/0016` + `0017` (→ **Accepted**),
  `docs/plans/011-cron-source.md` (new, 2432 lines, 6 tasks), and this HANDOVER.
- **`git status --short`:** only `.claude/settings.json` (the user's own — DO NOT stage/commit) should remain.
- **Audit records (local, gitignored):** `.superpowers/sdd/plan-011-audit-round-1.md` and `-round-2.md`.
- **No `.go` changes yet.** `robfig/cron/v3` is NOT yet in any `go.mod` (Task 1 Step 1 adds it).

## 3. Design summary (authoritative detail is in Spec 006 / ADR 0017 / Plan 011 — do not restate, read them)

`adapter/cron` (ROOT module, `package cron`; import robfig aliased). `robfig/cron/v3` accepted as the **3rd core
dependency** (ADR 0016 — MIT, verified zero-transitive-dep). `Source[T]` = `StreamingSource` + `LiveValueSource`,
a goleak-clean `clockwork` firing loop (skip-missed on overrun, at-most-once no-op Ack/Nack). Coordination checked
**synchronously per fire** (no heartbeat goroutine): `Elector.IsLeader(ctx, scope)` / `Locker.Claim(ctx, scope,
fire)` (scope-symmetric); a coordinator error **skips the fire fail-safe**. SQL `Locker` = `InboxDeduper`-style
dedup-INSERT on `(scope, fire_ts)`; SQL `Elector` = leader-lease atomic acquire-or-renew; both DB-clock, per-engine
dialect seam kept **in-package**. Key decisions: KD-1 (in-package dialects + cron-local SQL primitives), KD-2
(Source-owned `scope`, default = spec string), KD-4 (30s lease TTL), KD-5 (`@every` + Locker refused →
`ErrLockerRequiresGridSchedule`, because `@every`'s relative grid can't dedup — use the Elector for intervals).

## 4. Audit outcome (both rounds complete — the design-time gate is satisfied)

- **Round 1** (`.superpowers/sdd/plan-011-audit-round-1.md`): 2 BLOCKER + 4 HIGH + 3 MED + 6 LOW. Folded — notably
  B-1 (`@every`+Locker refusal), B-2 (unsatisfiable-spec guard), H-1 (Task 1 compiles alone), M-1 (Elector scope
  symmetry), H-2/H-4 (real-DB demoted-branch + `NewConsumer` integration tests).
- **Round 2** (`-round-2.md`): verified both BLOCKERs genuinely closed and every fix correct; caught ONE HIGH
  regression — the H-2 demoted-error test provocation (an omitted NOT-NULL column) inserts a row on live MySQL
  8.0.40 / MariaDB 11.4 rather than demoting — **corrected to a `CHECK (guard = 1)` violation** (empirically
  verified `ROW_COUNT()=0`); plus NEW-LOW-1 (gap-lock godoc) + NEW-NIT-1 (single `sql.Register`). Verdict:
  **implementable, no round-3 needed.**

## 5. Next actions (execute Plan 011 via SDD)

1. **Invoke `superpowers:subagent-driven-development`** and execute Plan 011 task-by-task:
   - **Task 1:** `robfig/cron/v3` root dep + `adapter/cron` scaffold + `Source[T]` (+ `coordinator.go` interfaces,
     satisfiability guard, `NewConsumer` integration test). **Task 1 Step 1** verifies the ADR 0016 acceptance
     gate: `go mod graph | grep robfig` shows NO transitive edge.
   - **Task 2:** coordination gating + `@every`+Locker refusal. **Task 3:** SQL Locker. **Task 4:** SQL Elector
     (hardest surface — MySQL three-step). **Task 5:** `crontest` leaf module (real-DB conformance, incl. the
     CHECK-constraint demoted-branch tests). **Task 6:** example + package doc.
2. **Per-task commits are PRE-AUTHORIZED** (plan approved + SDD chosen): commit each green task without pausing.
   `git push`/merge/branch-delete still need **explicit** approval.
3. **Whole-branch delivery gate** before the final increment (CLAUDE.md §5): `/code-review` + `/security-review`
   over `main..HEAD`, `go test ./... -race` green across all modules (Docker for `crontest`), coverage ≥85% +
   every hot-path/typed-error branch (dialect SQL + demoted branches covered by `crontest` — CI MUST run it).
4. After merge (with approval): delete `feat/cron-source`; update this HANDOVER; the next increment starts from a
   fresh branch off `main`.

## 6. Gotchas / environment

- **Go 1.25 pinned:** `GOTOOLCHAIN=go1.25.12`. Only new root dep = `robfig/cron/v3` (verify zero-transitive).
- **Docker IS available** here — `crontest` (and `dbtest`) testcontainers run; `use-testcontainers` mandatory.
- Custom skills (mandatory): start each task from `cc-skills-golang:golang-how-to`; TDD; `gopls`; `table-test` /
  `use-mockgen` / `use-testcontainers`; blackbox `_test` packages.
- **`.claude/settings.json`** shows modified — user's own file; never stage/commit it.
- **Coverage gate for `adapter/cron` REQUIRES the `crontest` (Docker) run** for `dialect.go`; a Docker-less local
  run does NOT satisfy it. The MySQL/MariaDB demoted-error branches use a `CHECK (guard = 1)` provocation
  (MySQL ≥ 8.0.16 / MariaDB ≥ 10.2.1 — both pinned images qualify).
