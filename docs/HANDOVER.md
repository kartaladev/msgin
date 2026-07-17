# HANDOVER — msgin

> **Next session: read this first, then trust the referenced files over any memory.** Read, in order:
> `CLAUDE.md`, then `docs/specs/001-messaging-core.md` (§9 sql adapter) and `docs/specs/002-sql-multi-module-and-sqlite.md`,
> `docs/adrs/0010-poller-sql-adapter.md` + `0011-sql-engine-dialect-module-split.md` + `0012-sqlite-dialect.md`,
> and `docs/plans/005-poller-sql-adapter.md` (Task 11 is the outstanding one). Audit records (gitignored):
> `.superpowers/sdd/plan-007-audit-round-{1,2}.md`. SDD ledger: `.superpowers/sdd/progress.md`.

_Updated: **Plan 007 (SQLite dialect, increment B) is COMPLETE and MERGED to `main`, and pushed** to
`origin/main`. Whole-branch-gated before merge (all green). The `feat/sqlite-dialect` branch has been deleted.
Next: **Plan 005 Task 11** (docs/examples across the multi-module layout + the SQLite story) — the only
outstanding Plan-005 work — folding the small follow-ups listed in §4._

## 1. Objective & roadmap position

`msgin` (`github.com/kartaladev/msgin`) — Go 1.25 EIP library, minimal deps, multi-module monorepo (Structure Z).
`main` now carries: **Plans 001–004** + **Plan 005 (Poller + sql adapter, Tasks 1–10)** + **Plan 006 (sql
engine/dialect split, increment A)** + **Plan 007 (SQLite dialect, increment B)**. The roadmap's remaining
item is **Plan 005 Task 11** (docs/examples), pended since Plan 006.

## 2. What Plan 007 delivered (increment B — the SQLite dialect)

A sixth shipped adapter surface: the **`adapter/database/sql/sqlite`** module — a driver-free (engine-only)
SQLite `LeaseDialect` + `InboxDialect`, certified against the reusable `harness` conformance suite via an
embedded, **Docker-free** run in `dbtest` (pure-Go `modernc.org/sqlite`, confined to `dbtest`).
- **Lease-only** (ADR 0012 D1): no `LockDialect`; `WithStrategy(StrategyLockForUpdate)` returns the existing
  `ErrLockStrategyUnsupported`. Claim is a **one-shot atomic** `UPDATE … WHERE id IN (SELECT … LIMIT ?)
  RETURNING …` (SQLite serializes writers — no `SKIP LOCKED`/two-step needed; empirically proven double-claim-safe).
- **INTEGER unix-µs timestamps** via `CAST(unixepoch('now','subsec')*1000000 AS INTEGER)` — exact parity with
  the PG/MySQL `.Microseconds()` interval math; DB-clock invariant preserved.
- **Exact inbox** (`ON CONFLICT(msg_id) DO NOTHING RETURNING`, like Postgres) → `MySQLFamily=false`.
  `sqlite_master` / `pragma_index_list(?)` probes.
- **Driver-free `sqlite.DSN(path, opts…)`** builder (WAL + busy_timeout defaults; `WithJournalMode`/
  `WithBusyTimeout`/`WithSharedMemory`) — opinionated, overridable connection guidance (the module never opens
  the caller's `*sql.DB`). `doc.go` explains why WAL + busy_timeout are required for concurrent consumers.
- **Two gated, behavior-preserving `harness` changes** (ADR 0012 D8): the `RunDialect` `ClaimLock`
  invalid-ident case is conditional on `LockDialect`; a new `TestKit.SingleWriter` skips only
  `RunOutbox/CommitGatesVisibility` (which needs MVCC non-blocking reads SQLite lacks — pool claim during an
  open write tx blocks → `SQLITE_BUSY`, confirmed by spike). Both default off → PG/MySQL fully unaffected.
- **CI fix (the "CI problem"):** pinned the two non-reproducible `@latest` tools — `govulncheck` →
  `golang.org/x/vuln@v1.6.0`, `golangci-lint` → `v2.12.2`. Plus sqlite in the CI matrix + aggregate job,
  release.yml sqlite prefixed-tag, and RELEASE.md tag-order.

## 3. Exact state (commits — all on `main` now)

Branch `feat/sqlite-dialect` (off `main` @ `0e20df3`), 4 task commits, fast-forward merged to `main` + pushed:
`5119117` T1 scaffold + DSN builder (+ spec/ADR/plan coupled) → `6f5c9a2` T2 harness gates →
`a88b590` T3 dialect + dbtest conformance → `65f53cc` T4 CI matrix/pins + release + RELEASE.md
(+ a final `docs:` HANDOVER commit). Each task: SDD implementer → coordinator verify+commit → adversarial
reviewer (T3 on Opus). Working tree clean except the user's unstaged `.claude/settings.json` (left alone).

**Design-time gate:** two adversarial Opus audits (round 1 NEEDS REVISION → F1 blocker + F2–F7; a no-Docker
spike against real SQLite (modernc 3.53.3) confirmed F1 and proved claim-safety/inbox/pragma; round 2 SOUND
WITH FIXES; no round 3). **Whole-branch gate — CLEAN:** all 6 modules build/vet/gofmt/`CGO_ENABLED=0`/
`tidy`-stable (GOWORK=off, go1.25.12); `-race` green incl. dbtest 4-engine conformance (goleak-clean); merged
sqlite coverage 92.1%; `govulncheck@v1.6.0` clean; `golangci-lint v2.12.2` 0 issues; isolation probe 0/0 (a
sqlite consumer inherits no driver/testcontainers); `/code-review` no correctness findings; `/security-review`
no vulnerabilities.

## 4. Follow-ups (Minor, non-blocking — for Plan 005 Task 11 or a later increment)

1. **New (Plan 007 code-review Minors):** (a) surface the SQLite ≥3.42 version floor on the exported
   `sqlite.DDL`/`InboxDDL` godoc (currently only on `doc.go` + the internal `nowMicros` const); (b) `sqlite.DSN`
   — a filesystem `path` containing a literal `?` flips the pragma separator to `&` (malformed URI); doc already
   directs exotic cases to a hand-built DSN — consider a doc note or `?`-guard.
2. **Carried from Plan 006 §4:** wider golangci linters (`errcheck`/`gosec`/`revive`) still deferred (config is
   the minimal govet/staticcheck/ineffassign/misspell set); `NewOutboundAdapter` `ErrNilResolver`-before-
   `ErrNilAdapter` precedence (cosmetic). (The `@latest` CI-pin follow-up is now DONE.)
3. **Plan-005 backlog:** `TransactionResolver` typed-nil guard + godoc (Plan 005 Task 9 review) — address in
   Plan 005 Task 11.

## 5. Next actions

1. **Start Plan 005 Task 11** (docs/examples) from a fresh branch off `main` — runnable `Example…` tests across
   the multi-module layout (memory/sql engine + postgres/mysql/**sqlite** dialects), the transactional-outbox
   and dedup-inbox stories, and the SQLite embedded/no-Docker story. Fold the §4 follow-ups where they touch
   the same files.
2. Follow the full workflow: brainstorm → (Task 11 is already specced in Plan 005; confirm scope) → SDD
   implementer + reviewer → whole-branch gate → merge.

## 6. Gotchas / environment

- **Go 1.25 pinned, patch = go1.25.12:** always `GOTOOLCHAIN=go1.25.12` (the `go` directive stays `1.25.0`).
  CI sets it and now pins `govulncheck@v1.6.0` + `golangci-lint v2.12.2` (no more `@latest`).
- **Multi-module (6 members):** `go.work` for local dev; **CI runs `GOWORK=off`** per module; each non-root
  module has a dev-time `replace github.com/kartaladev/msgin => ../../../..` swapped for a pinned `require`
  only at release (root tagged FIRST — `docs/RELEASE.md`, which now lists `sqlite`).
- **Docker** is needed only for the `dbtest` module (PG+MySQL+MariaDB via testcontainers). **SQLite conformance
  needs NO Docker** (embedded `modernc.org/sqlite`, in `dbtest` only). Root/harness/postgres/mysql/sqlite need
  no Docker.
- **modernc.org/sqlite** bundles SQLite ≥3.45 (satisfies the ≥3.42 floor for `unixepoch('now','subsec')`).
- **Custom skills (mandatory):** `table-test`, `use-mockgen`, `use-testcontainers`. Start Go work from
  `cc-skills-golang:golang-how-to`; blackbox `_test` packages; assert-closure tables; `t.Context()`.
