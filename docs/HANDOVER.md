# HANDOVER — msgin

> **Next session: read this first, then trust the referenced files over any memory.** Read, in order:
> `CLAUDE.md`, then for the active increment: `docs/specs/009-splitter-aggregator-endpoints.md` (§3.4),
> `docs/adrs/0020-splitter-aggregator-group-store.md` (§8), `docs/adrs/0021-sql-group-store.md`, and
> `docs/plans/017-sql-group-store.md`. The SDD ledger `.superpowers/sdd/progress.md` (gitignored, local) holds
> per-task history — trust it + `git log` over memory.

## LATEST: Phase 3 (`sql.GroupStore`, Plan 017) implementation COMPLETE on branch `feat/sql-groupstore` — Tasks 1-4 all done (core SPI reshape + `memory.GroupStore` rework, `sql.GroupStore` facade + `GroupDialect` SPI, 3 per-engine dialects + 4-engine conformance, and this finalize task: runnable Example, review-polish fixes, `go mod tidy` hygiene, docs coherence). Mechanical gate green (see below). **NOT YET MERGED**: the coordinator still owes the whole-branch `/code-review` + `/security-review` over `main..HEAD` (CLAUDE.md §5 final pre-merge gate) before `main` merge + branch delete + user's explicit push/merge approval.

## Where we are (2026-07-21)

Executing **Spec 009** — Splitter + Aggregator endpoints (+ durable group store, + expr sugar) — a **4-phase**
increment. Phases each get their own plan + ADR + 2-round adversarial audit + SDD + whole-branch gate.

- **Phase 1 — Splitter (Plan 015): DONE & MERGED to `main`** (merge `e4b346d`, `--no-ff`). Branch `feat/splitter`
  deleted. **`main` is NOT pushed** — `git push origin main` awaits explicit user approval.
- **Phase 2 — Aggregator (Plan 016): SHIPPED & MERGED to `main`** (`94cda1f`, `--no-ff`) and PUSHED; `feat/aggregator`
  deleted. Shipped: `MessageGroupStore` SPI (Phase-2 shape, since superseded), `memory.GroupStore`,
  `NewAggregator[A,B]`/`Handle`/`Run(ctx)` reaper, sharded per-key lock (also since superseded).
- **Phase 3 — `sql.GroupStore` (Plan 017 / ADR 0021 + the ADR 0020 §8 revision): IMPLEMENTATION COMPLETE, NOT YET
  MERGED.** Branch `feat/sql-groupstore`, commits `c481ece` (core SPI reshape), `3f02f0e` (GroupDialect SPI + sql
  facade + sequence-header int framing), `5b2d680` (per-engine dialects + 4-engine conformance), plus this
  session's finalize work (Example + polish + tidy + docs, not yet committed as of writing this handover — see
  "Exact state"). Reopened the Phase-2 settlement into a store-level lease-claim
  (`ClaimGroup`/`SettleGroup`/`AbandonGroup`, claim tags a member set, fenced by epoch) that serializes both
  within AND across processes; the per-key `[256]sync.Mutex` was removed. See "What shipped in Phase 3" below.
- **Phase 4 (expr sugar, Plan 018 / ADR 0019 addendum): not started.**

## What shipped in Phase 3 (for a fresh session with zero context)

- **Core (`groupstore.go`, `aggregator.go`):** `MessageGroupStore` SPI reshaped — `Remove` replaced by
  `ClaimGroup(ctx, key) (MessageGroupClaim, error)` / `SettleGroup(ctx, claim) error` /
  `AbandonGroup(ctx, claim) error`, plus `Expired`/`RecoverInterval`/`EmitsLiveValue` unchanged in shape.
  `Aggregator.Handle` reworked to claim-before-send; the per-key lock is gone (the store's atomic claim is now the
  sole serializer). `memory.GroupStore` reworked to the same lease shape (unconditional in-process lease, no TTL,
  `RecoverInterval()==0`).
- **`adapter/database/sql` (root module):** `GroupDialect` — a new segregated SPI (like `InboxDialect`) — plus
  `groupstore.go`'s `GroupStore` facade: `NewGroupStore(db, table, dialect, opts ...GroupStoreOption)`. Options:
  `WithGroupLeaseTTL` (default 5m, matches the Source's default — NOT a tighter value, see its godoc for why),
  `WithGroupLockedBy`. `framing.go`'s `EncodeHeaders`/`DecodeHeaders` extended so
  `msgin.sequence-number`/`msgin.sequence-size` round-trip losslessly as `int` (ADR 0021 §2 M-1).
- **Per-engine dialects (`postgres`/`mysql`/`sqlite` submodules):** `GroupDialect()` + `GroupDDL(table)` each,
  implementing the two-table schema (`<table>` group-lease row + `<table>_member` append-only member rows,
  `claimed_epoch NULL` = live). Postgres/MySQL use `SELECT ... FOR UPDATE` / row locking inside a tx; SQLite has no
  `FOR UPDATE`, so its multi-statement ops run on a dedicated `*sql.Conn` via raw `BEGIN IMMEDIATE`/`COMMIT`
  (`sqlite/groupdialect.go`'s `withImmediateConn`).
- **`harness/groupstore.go`'s `RunGroupStore`:** the full contract conformance suite, run against real
  Postgres/MySQL/MariaDB/SQLite containers via `dbtest`. Covers idempotent `Add`, exclusive `ClaimGroup`, fenced
  `Settle`/`Abandon`, late-member survival + `created_at` reset, crashed-lease `Expired`, cross-connection races
  (`ConcurrentFirstAddCompletionDetection_H1`, `TwoConnectionClaimRace`, `StaleEpochCrashRecovery_H2`), the
  crash-mid-release reaper recovery proof (`CrashMidReleaseReEmitsToOutput_HA` — now also asserts the recovered
  group's expired-channel count is 0, not just outCount==1), and a deadlock-freedom stress test
  (`NoDeadlockUnderConcurrentAddSettle_HB`).
- **Example:** `adapter/database/sql/postgres/example_sql_groupstore_test.go` — `Example_memoryGroupStore`
  (Output-checked, runs with no container, shows `go agg.Run(ctx)`) and `Example_sqlGroupStore` (compile-only, no
  `Output:` comment so `go test` never executes it — shows the durable `msginsql.NewGroupStore(db, "msgin_group",
  postgres.GroupDialect())` swap and reiterates why `Run` is required for that store's crash-recovery).

## Exact state

- **Branch:** `feat/sql-groupstore`. `git status --short` at the start of this finalize session:
  `M .claude/settings.json` (pre-existing, unrelated, NEVER stage), plus uncommitted `docs/` edits (spec 009, ADR
  0021, plan 017 — the `NewGroupStore(db, table, dialect, opts)` reconciliation, folded in this session along with
  the Phase-3-status/pseudocode-drift fixes below).
- **This session's finalize work (Task 4), staged for two commits (not yet committed as of writing — commit after
  the mechanical gate is confirmed green; see the task-4 report for the exact gate output):**
  1. `chore(sql): go mod tidy adapter modules (expr indirect via core, Plan-014 residue)` — `go mod tidy` in every
     `go.work` module; adds `github.com/expr-lang/expr v1.17.8 // indirect` to the sql submodules (transitive via
     the core `msgin` import, not a new direct dep).
  2. `test(sql): GroupStore example + review-polish; docs finalize` — the Example above; the
     `sqlite/groupdialect.go` `discardConn` bad-conn-on-failed-ROLLBACK fix (`Conn.Raw` + `driver.ErrBadConn`, the
     documented `database/sql` mechanism to evict a pooled connection); the H-A expired-channel assertion in
     `harness/groupstore.go`; a postgres `groupdialect.go` godoc note on the `ON CONFLICT DO UPDATE` dead-tuple
     cost (correctness-neutral, autovacuum-reclaimed); the core `groupstore.go` `ClaimGroup` godoc de-cross-ref
     (no dangling `WithGroupLeaseTTL` symbol from core); the `framing.go` godoc addition for
     sequence-number/sequence-size round-tripping; the `groupstore.go` `decodeGroupRows` all-or-nothing-decode
     godoc note; plus the spec/ADR doc-coherence fixes (Status line, §4.2 pseudocode signature drift, D16
     supersession pointer) and this HANDOVER rewrite.
- **`main` @ `b509db6`** (Phase-2 handover commit; Phase 1+2 merged, Phase 1 unpushed). Phase 3 has NOT been merged
  to `main` yet.

## Traceability pointers (read first)

- `CLAUDE.md` — workflow/gates (SDD, 2-round audit, table-test/goleak/testcontainers, Go 1.25 `GOTOOLCHAIN=go1.25.12`).
- `docs/specs/009-splitter-aggregator-endpoints.md` — §3.4 (Phase 3, revised); §3.6 D16 (Phase-2 guarantee,
  superseded pointer added this session); §8 open items (O9-6 nested correlation, still open/deferred).
- `docs/adrs/0020-splitter-aggregator-group-store.md` — §8 (Phase-3 revision: SPI reshape, supersedes §2/§3).
- `docs/adrs/0021-sql-group-store.md` — the full `sql.GroupStore` design: schema, `GroupDialect`, safe defaults,
  4-round audit history.
- `docs/plans/017-sql-group-store.md` — the Phase-3 plan (4 tasks, all done). Task 4's brief:
  `.superpowers/sdd/task-4-brief.md`; its report (this session): `.superpowers/sdd/task-4-report.md`.

## Next actions (resume here)

1. **Confirm the mechanical gate is green** (this session's report has the exact command output —
   `.superpowers/sdd/task-4-report.md`): `go build ./...`, `go test ./... -race` (all `go.work` modules, including
   the 4-engine `dbtest` conformance under Docker), `go vet ./...`, `golangci-lint run ./...`, `gofmt -l .`,
   `CGO_ENABLED=0 go build ./...`, `go mod tidy` no-diff, `govulncheck ./...`.
2. **If not already committed:** commit the two commits described in "Exact state" above (chore first, then the
   finalize commit) — the user must approve each `git commit` per CLAUDE.md.
3. **Run the whole-branch pre-merge gate** (CLAUDE.md §5, the coordinator's job, NOT part of Task 4's scope):
   `/code-review` and `/security-review` over `main..HEAD`, resolve/triage every finding, re-confirm `-race` green.
4. **On explicit user approval:** merge `feat/sql-groupstore` to `main` (`--no-ff`), delete the branch (local +
   remote if pushed), and decide on pushing `main` (still pending from Phase 1 — `main` has been unpushed since
   before Phase 2 per prior handovers; confirm current push state before assuming).
5. **Then Phase 4** (expr sugar `TransformExpr`/`SplitExpr`/aggregator exprs, Plan 018 / ADR 0019 addendum —
   lighter, reuses `compile[A]`, no DB). Spec 009 §3.5/D12-D14 has the design; not yet planned/audited.

## Gotchas / environment

- Go 1.25: prefix every go cmd with `GOTOOLCHAIN=go1.25.12`. `golangci-lint`/`govulncheck` needed for the gate
  (govulncheck may be at `$(go env GOPATH)/bin/govulncheck`). The local default toolchain is newer (1.26) —
  `go.work` pins 1.25.0; do not let a stray `go` invocation silently build on 1.26+.
- Blackbox `_test` packages only; assert-closure tables; `t.Context()`; `goleak` on the reaper/dbtest `TestMain`;
  `clockwork` fake clock for time. Core must NOT import `adapter/memory` or any `adapter/database/sql/*`
  (dependency-inward) — the new `example_sql_groupstore_test.go` lives in the `postgres` SUBMODULE precisely to
  respect this (it can import core `msgin` + `adapter/memory` + `adapter/database/sql`, but the CORE module itself
  never imports postgres).
- The 4-engine conformance (`dbtest` module) needs Docker running locally (testcontainers); it's part of the
  `-race` gate.
- Reserved headers: `HeaderSequenceNumber`/`HeaderSequenceSize` (int, and now also proven to round-trip through
  the sql JSON header framing per this session's `framing.go` godoc fix). `Split` stamps a deterministic child id
  `parentID#seq` + `HeaderCorrelationID` = parent id.
- SDD helper scripts: `~/.claude/plugins/cache/claude-plugins-official/superpowers/6.1.1/skills/subagent-driven-development/scripts/{task-brief,review-package}`.

_Updated 2026-07-21: Phase 3 (`sql.GroupStore`, Plan 017) implementation complete on `feat/sql-groupstore`
(Tasks 1-4, including this finalize task) — pending commit confirmation, the coordinator's whole-branch
`/code-review` + `/security-review` gate, and user-approved merge to `main`._
