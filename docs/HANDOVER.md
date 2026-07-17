# HANDOVER — msgin

> **Next session: read this first, then trust the referenced files over any memory.** Read, in order:
> `CLAUDE.md`, then the active design bundle — `docs/specs/002-sql-multi-module-and-sqlite.md` (§4–§8
> authoritative, **both audit rounds folded**), `docs/adrs/0011-sql-engine-dialect-module-split.md`,
> `docs/plans/006-sql-engine-dialect-split.md` (the 6-task Structure-Z plan being executed), and the audit
> records `.superpowers/sdd/plan-006-audit-round-{1,2}.md` (gitignored — may not survive a fresh clone; their
> findings are summarized in the plan's fold-in notes). Also `docs/adrs/0010-poller-sql-adapter.md`.
>
> **This is a DESIGN safepoint** (code tree green at `2974c01`; no refactor code written yet). The design is
> **settled and twice-audited** — implementation of Plan 006 (increment A) proceeds via SDD.

_Updated: reconciled ADR 0011 + Plan 006 to Structure Z; round-2 adversarial audit done (SOUND WITH FIXES,
R2-1…R2-7 folded, no round 3). Design bundle committed. Next: execute Plan 006 Tasks 1→(2∥3)→4→5→6 via SDD._

## 1. Objective & roadmap position

`msgin` (`github.com/kartaladev/msgin`) — Go 1.25 EIP library, minimal deps. Branch **`feat/poller-sql-adapter`**
(Plan 005 Tasks 1–10 landed; Task 11 docs/examples PENDED behind this refactor). Active work: **Plan 006 /
increment A** — refactor `adapter/database/sql` into a driver-free **engine** + **leaf-test dialect modules**
(Structure Z: engine + `harness` + `postgres` + `mysql` + `dbtest`) so the root module AND every dialect
*production* consumer carry zero driver/testcontainers deps (measured: co-located = 102 polluted go.sum lines;
leaf-test = 0). Then increment B (SQLite, ADR 0012 + Plan 007) then Plan 005 Task 11.

## 2. Exact state

- **Code:** clean green safepoint at `2974c01` (`GOTOOLCHAIN=go1.25.0 go build/vet ./...` clean; Docker up).
  **No refactor code exists yet.** `git status --short` should show only the design-bundle docs (+ the user's
  `.claude/settings.json` plugin toggle, left unstaged).
- **Design (settled, twice-audited):** spec 002, ADR 0011, Plan 006 all reconciled to Structure Z; round-2
  findings R2-1…R2-7 folded (see the plan's "Audit fold-in (round 2)" note). No further audit round needed.

## 3. The design in one screen (do NOT re-litigate — user-approved + twice-audited)

**Structure Z — 5 modules (increment A):**
- **engine** (root `adapter/database/sql`, driver-free): SPIs, `Source`/`Outbound`/`InboxDeduper`, framing,
  options, errors; newly EXPORTED `ValidateIdent` + `BeginLockTx` + `SettleLockTx`; fake-dialect + stub-driver
  unit tests only (test-deps testify+goleak). Explicit-dialect constructors; auto-detect DELETED.
- **`harness`** (own go.mod, pkg `harness`, requires engine + testify): `RunSource`/`RunLock`/`RunOutbound`/
  `RunOutbox`/`RunInbox`/`RunDialect(t, TestKit)`; no driver/testcontainers.
- **`postgres`** / **`mysql`** (own go.mod, require the ENGINE ONLY — zero test-deps):
  `postgres.LeaseDialect()`/`DDL`/`InboxDDL`; the single stateless value satisfies all 3 SPIs.
- **`dbtest`** (own go.mod, leaf — nobody imports): drivers + testcontainers + `RunTestDatabase`/`RunTestMySQL`/
  `RunTestMariaDB`; runs the harness per engine against real containers. All heavy test-deps live here.
- **`go.work`** committed (Task 6); CI runs `GOWORK=off`; release = root tagged FIRST then dialect modules swap
  dev `replace`→pinned `require` + module-prefixed tags.

## 4. Next actions (resume here — run CONTINUOUSLY & AUTONOMOUSLY per the session directive)

Execute **Plan 006** task-by-task via `superpowers:subagent-driven-development` (fresh implementer per task,
TDD red→green; coordinator/main session verifies `-race` green + commits; adversarial reviewer per task).
Per-task commits are pre-authorized (approved plan). Sequencing **1 → (2 ∥ 3) → 4 → 5 → 6**:
1. Explicit-dialect engine API + export `ValidateIdent`/`BeginLockTx`/`SettleLockTx` + delete auto-detect
   (+ relocate the `openDB`/`fakeDriver` test helpers — R2-3).
2. Engine fake-dialect unit tests + stub-`database/sql/driver` tests for `BeginLockTx`/`SettleLockTx` (R2-1).
3. `harness` module (TestKit + all `Run*`) — **parallel with Task 2** (both depend only on Task 1).
4. `postgres` + `dbtest` modules; run the harness vs Postgres (RunSource-first checkpoint — R2-5). Root green.
5. `mysql` module + extend `dbtest` (MySQL+MariaDB); root goes lean (driver-free). 
6. Commit `go.work` + `.gitignore` un-ignore + CI/release workflows + `docs/RELEASE.md` + go.sum isolation probe.

Then the **delivery gate**: whole-branch `/code-review` + `/security-review` (`main..HEAD`), resolve/triage,
all 5 modules `-race` green, two-profile coverage (root fakes+stub-driver; `dbtest -coverpkg`), go.sum probe
(102→0). Update this handover + memory. **Push the branch for GitHub MR — do NOT ff-`main`** (`git push` needs
explicit approval).

## 5. Gotchas / environment

- **Go 1.25 pinned:** always `GOTOOLCHAIN=go1.25.0`. **Docker MUST run** for the `dbtest` module (Postgres +
  MySQL + MariaDB via testcontainers).
- **On-branch module resolution:** no published root tag carries the new engine API, so each non-root module's
  go.mod resolves the engine via `go.work` (local) + a dev `replace github.com/kartaladev/msgin => ../../../..`
  for `GOWORK=off`. Swap `replace`→pinned `require` only at release (root tagged first).
- **`go.work` is gitignored until Task 6** un-ignores + commits it; create it locally for dev in Tasks 3–5.
- **Coverage (R2-2):** NOT per-prod-module (they have no tests → ~0%). Measure two profiles — root
  fakes+stub-driver, and `dbtest -coverpkg=…/adapter/database/sql/...` over merged engine+dialect packages.
- **Custom skills (mandatory):** `table-test`, `use-mockgen`, `use-testcontainers`. Start Go work from
  `cc-skills-golang:golang-how-to`; blackbox `_test` packages; assert-closure tables; `t.Context()`.
</content>
