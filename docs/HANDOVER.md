# HANDOVER — msgin

> **Next session: read this first, then trust the referenced files over any memory.** Read, in order:
> `CLAUDE.md`, then `docs/specs/002-sql-multi-module-and-sqlite.md`, `docs/adrs/0011-sql-engine-dialect-module-split.md`,
> `docs/plans/006-sql-engine-dialect-split.md`, and `docs/RELEASE.md`. Audit records:
> `.superpowers/sdd/plan-006-audit-round-{1,2}.md` (gitignored). Per-task SDD ledger: `.superpowers/sdd/progress.md`.

_Updated: **Plan 006 (increment A — sql engine/dialect module split) is COMPLETE, whole-branch-gated, and
pushed to `origin/feat/poller-sql-adapter` for MR review.** NOT merged to `main` (per the session directive).
Next increments: B (SQLite, ADR 0012 + Plan 007) → resume Plan 005 Task 11 (docs/examples)._

## 1. Objective & roadmap position

`msgin` (`github.com/kartaladev/msgin`) — Go 1.25 EIP library, minimal deps. Branch `feat/poller-sql-adapter`
carries **Plan 005 (Poller + sql adapter, Tasks 1–10)** and now **Plan 006 (increment A, complete)**. `main` is
at `86ffa116` (Plans 001–004). The branch is pushed for the user's GitHub MR; do NOT ff-`main` without approval.

## 2. What Plan 006 delivered (Structure Z — 5 modules, driver-free root)

`adapter/database/sql` is split into leaf-test modules so the root module AND every dialect *production*
consumer carry zero driver/testcontainers deps (**empirically 102 → 0 polluted go.sum lines**):
- **engine** (root `adapter/database/sql`, driver-free): SPIs, `Source`/`Outbound`/`InboxDeduper`, framing,
  options, errors; **explicit-dialect constructors** (auto-detect deleted); newly EXPORTED `ValidateIdent` +
  `BeginLockTx` + `SettleLockTx`; new `ErrNilDialect`. Tests: fake dialect + stub-`database/sql/driver` only.
- **`harness`** (own module, pkg `harness`, requires engine + testify): `RunSource`/`RunLock`/`RunOutbound`/
  `RunOutbox`/`RunInbox`/`RunDialect(t, TestKit, db)` — the reusable conformance suite.
- **`postgres`** / **`mysql`** (own modules, require the ENGINE ONLY): `postgres.LeaseDialect()`/`InboxDialect()`/
  `DDL`/`InboxDDL`, same for `mysql.*`. Dialect SQL moved VERBATIM (byte-verified).
- **`dbtest`** (leaf runner, nobody imports): drivers + testcontainers; runs the harness against real Postgres,
  MySQL, and MariaDB.
- **`go.work`** committed; CI (`.github/workflows/ci.yml`) runs `GOWORK=off` per-module matrix + an aggregate
  workspace job; `release.yml` handles root + module-prefixed tags; `docs/RELEASE.md` documents the
  root-tagged-first, replace→pin choreography.

## 3. Exact state (commits on the branch, this increment)

`2974c01`(prev design) → `7d1b16e` docs: reconcile design to Structure Z + round-2 audit → `27b5ab6` T1
explicit-dialect API + exports → `f6ee0f1` T2 engine fake+stub-driver tests → `6d92e84` T3 harness module →
`ba959bb` T4 postgres + dbtest → `626ca85` T5 mysql + root goes lean → `2d7c794` T5-fix per-SPI invalid-ident
coverage → `0d4024d` T6 go.work + CI/release + isolation probe. Each task: SDD implementer → coordinator
verify+commit → adversarial reviewer → fix. Working tree clean except the user's unstaged `.claude/settings.json`
(plugin toggle, deliberately left alone).

**Whole-branch gate — CLEAN:** all 5 modules build/vet/gofmt/`CGO_ENABLED=0`/`go mod tidy`-stable (GOWORK=off,
go1.25.12); root+mysql+dbtest `-race` green (dbtest = real PG+MySQL+MariaDB, goleak-clean); engine coverage
93.8%; govulncheck clean; isolation probe 0/0; `/code-review` no correctness findings; `/security-review` no
vulnerabilities.

## 4. Follow-ups (Minor, non-blocking — for the MR discussion or a later increment)

1. **CI pins `@latest`** (`govulncheck@latest`, `golangci-lint-action version: latest`) → pin explicit versions
   for reproducibility.
2. **Wider golangci linters** (`errcheck`/`gosec`/`revive`) deferred — `.golangci.yml` is intentionally minimal
   (govet/staticcheck/ineffassign/misspell, 0-issue); enabling the rest surfaces pre-existing findings.
3. Pre-existing cosmetic: `NewOutboundAdapter(nil, table, dialect, WithSharedTransaction(nil))` returns
   `ErrNilResolver` before `ErrNilAdapter` (arbitrary precedence, both errors).
4. **Plan-005 backlog (NOT Plan 006):** `TransactionResolver` typed-nil guard + godoc (Plan 005 Task 9 review) —
   address in Plan 005 Task 11.

## 5. Next actions

1. **Await the user's MR review** on GitHub (`origin/feat/poller-sql-adapter`). Address MR feedback on-branch.
2. **Increment B — SQLite** (spec 002 §7): brainstorm → update spec → write ADR 0012 + Plan 007 → adversarial
   audit → SDD. Pure-Go `modernc.org/sqlite` (cgo-free, no Docker); lease-only (`sqlite.LeaseDialect()` does NOT
   implement `LockDialect` → `ErrLockStrategyUnsupported`); inbox via `ON CONFLICT … RETURNING`. Adds a `sqlite`
   prod module + extends `dbtest`; the `harness` already runs correctness-only concurrency assertions (F8) so a
   single-writer engine passes the same contract.
3. **Resume Plan 005 Task 11** (docs/examples across the multi-module layout) + fold the §4 follow-ups.

## 6. Gotchas / environment

- **Go 1.25 pinned, patch = go1.25.12:** always `GOTOOLCHAIN=go1.25.12` (bumped from 1.25.0 after govulncheck
  flagged GO-2025-4007; the `go` directive stays `1.25.0`). CI sets it.
- **Multi-module:** `go.work` for local dev; **CI runs `GOWORK=off`**; each non-root module has a dev-time
  `replace github.com/kartaladev/msgin => ../../../..` swapped for a pinned `require` only at release (root
  tagged FIRST — `docs/RELEASE.md`). The dev `replace` pulls the engine's testify/goleak into the dialect
  modules' go.sum (~11 lines, non-heavy) — resolves at release.
- **Docker MUST run** for the `dbtest` module (PG+MySQL+MariaDB via testcontainers). Root/harness/postgres/mysql
  need no Docker.
- **Custom skills (mandatory):** `table-test`, `use-mockgen`, `use-testcontainers`. Start Go work from
  `cc-skills-golang:golang-how-to`; blackbox `_test` packages; assert-closure tables; `t.Context()`.
</content>
