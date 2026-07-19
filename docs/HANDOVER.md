# HANDOVER — msgin

> **Next session: read this first, then trust the referenced files over any memory.** Read, in order:
> `CLAUDE.md`, then the **audited, implementation-ready** design bundle for the completed increment —
> `docs/specs/006-cron-source.md`, `docs/adrs/0016-robfig-cron-dependency.md`, `docs/adrs/0017-cron-source.md`,
> and the plan `docs/plans/011-cron-source.md`. **Plan 011 execution is DONE — all six tasks complete and
> committed.** What remains is the **whole-branch delivery gate + merge to `main`**, which is the coordinator's
> job in the next session (NOT done yet — do not assume `feat/cron-source` is merged). Read the SDD progress
> ledger `.superpowers/sdd/progress.md` (gitignored, local) for the full per-task history; trust it + `git log`
> over memory.

_Updated 2026-07-19: **Plan 011 execution COMPLETE via `superpowers:subagent-driven-development`.** All six tasks
done and committed on `feat/cron-source`. `adapter/cron` ships Source[T] (recurring/cron StreamingSource) +
Elector/Locker coordination + dependency-free SQL-backed Locker/Elector (PG/MySQL/SQLite) + the `crontest` leaf
module (real-DB conformance) + package doc + a goleak-clean runnable example. Clean safepoint (tree builds, full
package suite green, race-clean, goleak-clean). Handing over for the **whole-branch delivery gate** (CLAUDE.md
§5: `/code-review` + `/security-review` over `main..HEAD`, full-module `-race` suite incl. `crontest` Docker,
lint/vuln/build gates, coverage confirmation) and, after explicit user approval, merge to `main` + branch
cleanup. Push/merge/branch-delete still need explicit user approval — none of that has happened yet._

## 1. Objective & roadmap position

`msgin` (`github.com/kartaladev/msgin`) — Go 1.25 EIP library, minimal deps, multi-module monorepo. `main` @
`410a45e` carries Plans 001–010 (core + reliability + resilience + `sql`/`memory`/dialects + composition Phase 1 +
Phase 3 Publish-Subscribe + scheduled/delayed send), all merged.

**Completed increment (not yet merged) = Spec 006 — cron / recurring message source + distributed
coordination.** A recurring/cron `Source[T]` (a `StreamingSource`) that emits a caller-defined message on each
schedule fire, driven by the existing runtime; PLUS msgin-native multi-instance single-fire via an `Elector`
(leader) + `Locker` (per-fire) seam with **dependency-free SQL-backed implementations of both** (PG/MySQL/SQLite).
Un-defers Spec 005 O5-5.

**Roadmap after this merges:** (Spec 006 open items) Redis/etcd-backed coordinators (O6-1, optional modules),
seconds-field cron `WithSeconds` (O6-2); plus still-deferred: EIP `Delayer` composition step (Spec 005 O5-1),
memory delayed-send (O5-2), pgx/redis/nats/http adapters, Plan 005 T11 examples, Phase-4 fluent DSL (gated).

## 2. Exact state (Plan 011 complete — awaiting whole-branch gate + merge)

- **Branch:** `feat/cron-source` (off `main` @ `410a45e`), NOT merged yet.
- **All six tasks done and committed:**
  - **Task 1** (`d0c4204`) — `Source[T]` (grid-tracking clockwork loop, skip-missed) + `coordinator.go`
    (`Elector`/`Locker` interfaces) + `errors.go` + `robfig/cron/v3` dep (zero-transitive gate confirmed).
  - **Task 2** (`3344767`) — `WithElector`/`WithLocker` options + real `wireGate` body +
    `ErrConflictingCoordinator` + `ErrLockerRequiresGridSchedule` (the `@every`+Locker refusal).
  - **Task 3** (`8aff050`) — SQL Locker: `sqlutil.go` (exported `Querier`/`validateIdent`/quote helpers, KD-1),
    `dialect.go` `LockerDialect` (PG/MySQL/SQLite dedup-INSERT + DDL), `sqllock.go` (`SQLLocker`).
  - **Task 4** (`301a5ea`) — SQL Elector: `sqlelector.go` (`SQLElector`, scope-per-call `IsLeader`,
    `WithElectorTable`/`WithElectorInstanceID`/`WithLeaseTTL`) + `dialect.go` `ElectorDialect` (PG/SQLite
    `ON CONFLICT DO UPDATE ... RETURNING`; MySQL three-step acquire-or-renew with `ErrElectorAcquireFailed`
    demoted-error).
  - **Task 5** (`9c82471`) — `adapter/cron/crontest` leaf module (own `go.mod`, testcontainers, `go.work` entry):
    real-DB conformance for both coordinators across PostgreSQL/MySQL/MariaDB/SQLite, incl. the CHECK-constraint
    demoted-error provocations for `ErrLockerClaimFailed`/`ErrElectorAcquireFailed`.
  - **Task 6** (this commit) — `adapter/cron/doc.go` (package doc: import alias, timezone/`CRON_TZ`, delivery
    guarantee, multi-instance single-fire / coordinator choice) + `adapter/cron/example_test.go` (`ExampleSource`,
    fake-clock deterministic, goroutine explicitly joined before return for goleak-safety) + this HANDOVER
    rewrite. `source.go`'s original top-of-file doc comment was demoted (blank line inserted before `package
    cron`) so `doc.go` is the sole package-doc source — verified no duplicate-package-doc `go vet` warning and
    `go doc ./adapter/cron` renders the `doc.go` text.
- **`git status --short`** immediately before this commit: only `.claude/settings.json` modified (the user's
  own file — never staged/committed) plus this session's new/modified files
  (`adapter/cron/doc.go`, `adapter/cron/example_test.go`, `adapter/cron/source.go`, `docs/HANDOVER.md`).
- **Last commit on the branch before this one:** `9c82471` — `test(cron): real-DB conformance for SQL Locker +
  Elector (crontest leaf)`.
- **SDD ledger (local, gitignored):** `.superpowers/sdd/progress.md` — all six tasks marked complete with
  per-task coverage/finding notes. **Audit records:** `.superpowers/sdd/plan-011-audit-round-{1,2}.md`.

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

**Design change folded in during implementation (Task 3, post-implementation review pass 2):** the Locker's
`claimed_by` column / `WithInstanceID` option / `randomID()` claimant surface was **removed as YAGNI**. The
Locker's `fired` table is now just `(scope, fire_ts, claimed_at)` — it proves *someone* claimed a fire, not
*who*. Claimant/holder identity is now an **Elector-only** concept, via `WithElectorInstanceID` (Task 4
re-introduced `randomID()` there, scoped to the Elector). This is captured in Spec 006 D12 and ADR 0017 (both
updated in-branch, coupled with the Task 3/4 code commits per the amend-in-branch convention).

## 4. Audit outcome (both rounds complete — the design-time gate is satisfied; unaffected by execution)

- **Round 1** (`.superpowers/sdd/plan-011-audit-round-1.md`): 2 BLOCKER + 4 HIGH + 3 MED + 6 LOW. Folded — notably
  B-1 (`@every`+Locker refusal), B-2 (unsatisfiable-spec guard), H-1 (Task 1 compiles alone), M-1 (Elector scope
  symmetry), H-2/H-4 (real-DB demoted-branch + `NewConsumer` integration tests).
- **Round 2** (`-round-2.md`): verified both BLOCKERs genuinely closed and every fix correct; caught ONE HIGH
  regression — the H-2 demoted-error test provocation (an omitted NOT-NULL column) inserts a row on live MySQL
  8.0.40 / MariaDB 11.4 rather than demoting — **corrected to a `CHECK (guard = 1)` violation** (empirically
  verified `ROW_COUNT()=0`); plus NEW-LOW-1 (gap-lock godoc) + NEW-NIT-1 (single `sql.Register`). Verdict:
  **implementable, no round-3 needed.**

## 5. Execution outcome (Tasks 1–6, all green + reviewed)

Per-task coverage progression on `adapter/cron` (driver-free unit tests only, per-task — `dialect.go`'s per-engine
SQL bodies are BY DESIGN uncovered until Task 5's real-DB run merges in): Task 1 81.7% → Task 2 94.7% (M1 closed:
`win()` gate-loss/gate-error-skip/loop-`continue` tests added) → Task 3 57.6% (Locker's `dialect.go` bodies still
uncovered by design) → Task 4 50.3% (Elector's `dialect.go` bodies still uncovered by design) → **Task 5: merged
unit + `crontest` real-DB coverage = 87.5%** (≥85% gate met, no 0.0%-covered function; `dialect.go` SQL bodies for
both Locker and Elector now exercised on real PostgreSQL/MySQL/MariaDB/SQLite, including the
`ErrLockerClaimFailed`/`ErrElectorAcquireFailed` demoted-error branches via the CHECK-constraint provocation).
Task 6 adds no new logic (doc + example only); the 87.5% figure stands as the package's coverage going into the
whole-branch gate.

**Carry-forward MINOR findings** (not yet triaged/fixed — fold into the whole-branch fix-wave, CLAUDE.md §5):
- **M2** (Task 1): run `govulncheck ./...` in the whole-branch pre-merge gate (not run locally during task
  execution).
- **M3** (Task 2): `coordinator_test.go` uses hand-written `fakeElector`/`fakeLocker` instead of `mockgen`
  (brief-mandated verbatim; single-method interfaces arguably clearer by hand — low impact, noted as a
  deliberate exception rather than a defect).
- **Task 4 minors:** interface-method godoc could restate the autocommit precondition more explicitly;
  `fakeElectorDialect.calls` is populated but unasserted in one test.
- **Task 5 minor:** `crontest/locker_conformance_test.go` (~L1007, "purge rejects non-positive retention") uses a
  bare `for` loop + inline assert instead of the mandated table-test assert-closure form (`table-test` skill) —
  fix in the final whole-branch fix-wave.

None of these are BLOCKER/HIGH; they are candidates for the `/code-review` pass over `main..HEAD`.

## 6. Next actions — the whole-branch delivery gate, then merge (coordinator, next session)

Plan 011 / Task 6's brief specifies this gate verbatim (`.superpowers/sdd/task-6-brief.md`, "Whole-branch delivery
gate" section) — run it over `main..HEAD` before any merge, per CLAUDE.md §5:

1. **`/code-review` on `main..HEAD`**; fix or triage every finding — including the four carry-forward minors in
   §5 above.
2. **`/security-review` on the pending changes.** Focus surface: the dialect SQL — confirm every table identifier
   flows through `validateIdent`+quote (never interpolated raw), and `scope`/`fire`/`holder`/`instanceID` are
   always bound parameters. Confirm the MySQL `LOCK IN SHARE MODE`/`INSERT IGNORE` paths cannot silently drop a
   real error (the `ErrLockerClaimFailed`/`ErrElectorAcquireFailed` guards).
3. **`GOTOOLCHAIN=go1.25.12 go test ./... -race`** green across ALL modules (root + every leaf via `go.work`,
   including `crontest` — requires Docker).
4. **`GOTOOLCHAIN=go1.25.12 go vet ./...`, `golangci-lint run ./...`, `govulncheck ./...`** clean; `gofmt -l .`
   silent.
5. **`CGO_ENABLED=0 GOTOOLCHAIN=go1.25.12 go build ./...`** succeeds (pure Go — `modernc.org/sqlite` is cgo-free
   and lives only in `crontest`).
6. **ADR 0016 acceptance:** `go mod graph | grep robfig` shows no transitive edge; `go mod verify` passes; root
   `go mod tidy` leaves `go.mod`/`go.sum` unchanged; `NOTICE` carries the MIT attribution.
7. **Coverage:** `adapter/cron` ≥85% (merged unit+`crontest` = 87.5%, confirmed Task 5) and every hot-path/typed-
   error branch covered — see the brief's exhaustive branch list; remember `dialect.go`'s SQL branches are
   covered ONLY by the `crontest` real-DB run, never by the driver-free unit tests alone.
8. **Confirm ADR 0016 + ADR 0017 Status = Accepted** (they shipped Accepted in Task 1's commit — verify the
   final state hasn't regressed).
9. **Confirm with the user before merge/push** (never merge/push without explicit approval); then **delete
   `feat/cron-source`** after merge (`git branch -d`, and remote too if it was pushed).

## 7. Gotchas / environment

- **Go 1.25 pinned:** `GOTOOLCHAIN=go1.25.12`. Only new root dep = `robfig/cron/v3` (verify zero-transitive).
- **Docker IS available** here — `crontest` (and `dbtest`) testcontainers run; `use-testcontainers` mandatory.
- Custom skills (mandatory): start each task from `cc-skills-golang:golang-how-to`; TDD; `gopls`; `table-test` /
  `use-mockgen` / `use-testcontainers`; blackbox `_test` packages.
- **`.claude/settings.json`** shows modified — user's own file; never stage/commit it.
- **Coverage gate for `adapter/cron` REQUIRES the `crontest` (Docker) run** for `dialect.go`; a Docker-less local
  run does NOT satisfy it (a local, Docker-less `adapter/cron` run alone shows ~50% — this is expected, not a
  regression). The MySQL/MariaDB demoted-error branches use a `CHECK (guard = 1)` provocation (MySQL ≥ 8.0.16 /
  MariaDB ≥ 10.2.1 — both pinned images qualify).
- `adapter/cron/example_test.go`'s `ExampleSource` spawns a `Stream` goroutine on a fake clock; it explicitly
  joins that goroutine (`cancel()` then `<-done`) before returning, because the package's `TestMain` in
  `source_test.go` runs `goleak.VerifyTestMain` — a deferred-only `cancel()` would race that check.
