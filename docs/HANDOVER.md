# HANDOVER — msgin

> **Next session: read this first, then trust the referenced files over any memory.** Read, in order:
> `CLAUDE.md`, then for the active increment: `docs/specs/009-splitter-aggregator-endpoints.md` (§3.4),
> `docs/adrs/0020-splitter-aggregator-group-store.md` (§8), `docs/adrs/0021-sql-group-store.md`, and
> `docs/plans/017-sql-group-store.md`. The SDD ledger `.superpowers/sdd/progress.md` (gitignored, local) holds
> per-task history — trust it + `git log` over memory.

## LATEST: Phase 3 (`sql.GroupStore`, Plan 017) SHIPPED, MERGED to `main` (`c7eb673`, `--no-ff`) and PUSHED to origin; `feat/sql-groupstore` deleted. Whole-branch gate PASSED — `/code-review` high (0 Critical/Important; 2 low triaged: decodeGroupRows poison-group has no dead-letter escape [needs externally-corrupt DB bytes; conservative all-or-nothing decode documented], compile-only `Example_sqlGroupStore` nil-db panic is unreachable since Go never runs a no-Output example); `/security-review` clean (SQL injection-guarded via `ValidateIdent`+bound params, `DecodeHeaders` stock `encoding/json`, no secrets/crypto/auth surface); full `-race`/vet/lint(0)/gofmt/CGO0/tidy/govulncheck green incl the 4-engine testcontainers conformance. Design was **4-rounds adversarially audited (SOUND-WITH-NITS)** before any code. **NEXT = Phase 4 (expr sugar, Plan 018 / ADR 0019 addendum) — not started.** Deferred backlog: decodeGroupRows dead-letter-after-N escape; the two Task-review polish Minors already folded.

## Where we are (2026-07-21)

Executing **Spec 009** — Splitter + Aggregator endpoints (+ durable group store, + expr sugar) — a **4-phase**
increment. Phases each get their own plan + ADR + 2-round adversarial audit + SDD + whole-branch gate.

- **Phase 1 — Splitter (Plan 015): DONE & MERGED to `main`** (merge `e4b346d`, `--no-ff`). Branch `feat/splitter`
  deleted. **`main` is NOT pushed** — `git push origin main` awaits explicit user approval.
- **Phase 2 — Aggregator (Plan 016): SHIPPED & MERGED to `main`** (`94cda1f`, `--no-ff`) and PUSHED; `feat/aggregator`
  deleted. Shipped: `MessageGroupStore` SPI (Phase-2 shape, since superseded), `memory.GroupStore`,
  `NewAggregator[A,B]`/`Handle`/`Run(ctx)` reaper, sharded per-key lock (also since superseded).
- **Phase 3 — `sql.GroupStore` (Plan 017 / ADR 0021 + the ADR 0020 §8 revision): SHIPPED & MERGED to `main`
  (`c7eb673`, `--no-ff`) and PUSHED; `feat/sql-groupstore` deleted.** Branch commits (now on main via the merge):
  `c481ece` (core SPI reshape + Aggregator claim rework + memory rework), `3f02f0e` (GroupDialect SPI + sql facade +
  sequence-header int framing), `5b2d680` (per-engine dialects + 4-engine conformance), `c27990e` (chore: go mod
  tidy — expr indirect), `668672d` (Example + review-polish + docs finalize). Reopened the Phase-2 settlement into a
  store-level lease-claim (`ClaimGroup`/`SettleGroup`/`AbandonGroup`, claim tags a member set, fenced by epoch) that
  serializes both within AND across processes; the per-key `[256]sync.Mutex` was removed. See "What shipped in
  Phase 3" below.
- **Phase 4 (expr sugar, Plan 018 / ADR 0019 addendum): NOT STARTED — this is the next increment.** Full expr sugar
  `TransformExpr[A,B]` / `SplitExpr[A]` / aggregator `WithCorrelationExpr`/`WithReleaseExpr`/aggregate-by-expr,
  reusing the `compile[A]` primitive (Spec 008 / ADR 0019); no DB, no new dependency (expr already in-core). Start a
  fresh design cycle: spec-delta (Spec 009 §3.5 D12–D14 already sketches it) → ADR 0019 addendum → Plan 018 →
  2-round adversarial audit → SDD → whole-branch gate → merge.

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

- **`main` @ `c7eb673`** (the Phase-3 `--no-ff` merge) and **PUSHED to origin** (`b509db6..c7eb673`). Branch
  `feat/sql-groupstore` **deleted** (was never on the remote). Working tree clean except the pre-existing
  `M .claude/settings.json` (unrelated, NEVER stage).
- Phase-3 commits now on `main` via the merge: `c481ece` (core SPI reshape + Aggregator + memory) → `3f02f0e`
  (GroupDialect SPI + sql facade + framing) → `5b2d680` (per-engine dialects + 4-engine conformance) → `c27990e`
  (chore: go mod tidy — expr indirect) → `668672d` (Example + review-polish + docs finalize).
- **Phase 1's `main` is now pushed too** (this push carried the whole history forward), so the long-standing
  "Phase 1 unpushed" caveat from prior handovers is resolved — `origin/main == main == c7eb673`.

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

Phase 3 is DONE, merged, and pushed — nothing outstanding on it. The next increment is **Phase 4 (expr sugar)**:

1. **Fresh design cycle for Phase 4** (Plan 018 / ADR 0019 addendum). Scope: `TransformExpr[A,B]` (payload
   projection → asserted to B), `SplitExpr[A]` (expr → slice → Phase-1 Split machinery), and the aggregator exprs
   `WithCorrelationExpr[A]` / `WithReleaseExpr` / aggregate-by-expr over a fixed group-scoped env
   `{messages []A, size int}`. Reuses the `compile[A]` primitive (Spec 008 / ADR 0019); a bad expr is
   `ErrInvalidExpression` at construction. **No DB, no new dependency** (`expr-lang/expr` already in-core).
2. Follow the mandatory workflow: brainstorm → spec-delta (Spec 009 §3.5 D12–D14 already sketches it) → ADR 0019
   addendum → Plan 018 → **2-round (min) adversarial Opus audit of spec+ADR+plan** → **ask the user before any
   implementation** → SDD (fresh implementer per task, coordinator verifies+commits, adversarial reviewer) →
   whole-branch `/code-review`+`/security-review` gate → merge.
3. Backlog (deferred, not blocking Phase 4): `decodeGroupRows` dead-letter-after-N-reclaims escape for a
   poison/corrupt-header group (`/code-review` low finding — needs externally-corrupt DB bytes; low priority);
   O9-6 nested-correlation godoc note; Resequencer/other group-store backends (redis/pgx/nats).

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

_Updated 2026-07-21: Phase 3 (`sql.GroupStore`, Plan 017) SHIPPED — merged to `main` (`c7eb673`, `--no-ff`) and
pushed to origin; `feat/sql-groupstore` deleted; whole-branch `/code-review`+`/security-review` gate passed. Next
increment: Phase 4 (expr sugar, Plan 018 / ADR 0019 addendum) — not started._
