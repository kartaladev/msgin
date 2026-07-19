# HANDOVER — msgin

> **Next session: read this first, then trust the referenced files over any memory.** Read, in order:
> `CLAUDE.md`, then the design bundle for the active increment — `docs/specs/005-scheduled-send.md`,
> `docs/adrs/0015-scheduled-send.md`, `docs/plans/010-scheduled-send.md`. Audit records (gitignored):
> `.superpowers/sdd/plan-010-audit-round-{1,2}.md`.

_Updated 2026-07-19: **Plan 010 (scheduled / delayed send) is CODE-COMPLETE and whole-branch-gate-CLEAN on branch
`feat/scheduled-send`, but NOT yet merged — awaiting explicit user approval to merge to `main` + push.** All 3
tasks implemented via SDD, each adversarially reviewed (Approved); `/code-review` + `/security-review` over
`main..HEAD` both returned no findings. The design was twice-adversarially-audited before code (gate CLEAR)._

## 1. Objective & roadmap position

`msgin` (`github.com/kartaladev/msgin`) — Go 1.25 EIP library, minimal deps, multi-module monorepo. `main`
carries Plans 001–009 (core + reliability + resilience + `sql`/`memory`/dialects + composition Phase 1 + Phase 3
Publish-Subscribe), all merged.

**Active increment = Spec 005 / Plan 010 — scheduled / delayed send.** A durable producer-side delayed send over
the sql adapter's EXISTING `visible_after` mechanism (no DDL/dialect-SQL change): a new optional core capability
`ScheduledSender{ SendAfter(ctx, msg, delay) error }` (type-asserted, like `NativeReliability`/`LockDialect`);
`sql.Outbound` implements it; `Producer[T]` widens with `SendAfter(delay)` (skew-free primitive) + `SendAt(t)`
(absolute sugar over an injected `clockwork` clock, `WithProducerClock`); unsupported sink →
`ErrScheduledSendUnsupported`; negative/past delay clamps to 0. sql-only this increment (memory fails loud).

**Roadmap:** after this merges, **NEXT = an optional gocron-backed recurring / cron-triggered *source* adapter**
(Spec 005 O5-5 — its own isolated module per ADR 0003; gocron researched 2026-07-19: MIT, 3 pure-Go runtime deps,
non-durable-but-OK for ephemeral recurring triggers). Also deferred (Spec 005): the EIP-named `Delayer`
composition step (O5-1) and memory-adapter delayed send (O5-2). Still pended: pgx/redis/nats/http adapters, Plan
005 Task 11 examples, the Phase-4 fluent DSL (gated).

## 2. Exact state (safepoint — code complete, gate clean, UNMERGED)

- **Branch:** `feat/scheduled-send` (off `main` @ `e0f4004`). **Not merged, not pushed.**
- **`git status --short`:** only `.claude/settings.json` (the user's own file — leave untouched). All feature
  work is committed on the branch.
- **Branch commits (`main..HEAD`):**
  - `b09160c` feat(core): add ScheduledSender capability + producer SendAfter/SendAt — Task 1 (+ Spec 005/ADR 0015/Plan 010).
  - `df35aea` feat(sql): Outbound implements ScheduledSender (delayed send) — Task 2.
  - `99de417` test(sql): real-DB delayed-visibility conformance + example + package doc — Task 3.
- **Gate — CLEAR:** `go test ./... -race` green (all modules via go.work); root-pkg coverage **99.1%**; `go vet`,
  `gofmt`, `golangci-lint` (0 issues, root + harness), `govulncheck` (no called vulns), `CGO_ENABLED=0` build,
  `go mod tidy` (root + harness, no new dep) all clean. **Real-DB conformance PASSED on PostgreSQL, MySQL,
  MariaDB, and SQLite** (dbtest via testcontainers, Docker). `/code-review` (high, `main..HEAD`) → no findings;
  `/security-review` → no findings.

## 3. Traceability pointers

`CLAUDE.md` → `docs/specs/005-scheduled-send.md` (D1–D8, open items O5-1..O5-5) →
`docs/adrs/0015-scheduled-send.md` (Nygard) → `docs/plans/010-scheduled-send.md` (3-task plan, both audit rounds
folded). Audit records: `.superpowers/sdd/plan-010-audit-round-{1,2}.md`. SDD ledger:
`.superpowers/sdd/progress.md`.

## 4. Decisions & deviations (this session)

- **Mechanism = capability SPI, NOT a header** (Spec 005 D1). Chosen after researching existing-code precedent
  (capability type-assertion is a 4-site load-bearing pattern; a caller-set behavior header has no precedent and
  would leak onto the delivered message via framing).
- **gocron evaluated and scoped out of delayed send** (Spec 005 N7/O5-5): non-durable + no cross-instance
  exactly-once without external infra → wrong tool for durable one-shot delay; reserved for a future optional
  recurring-source module.
- **`Producer[T]` interface widened** (ADR 0015 D3): breaking only for external *implementers* (none in-repo);
  acceptable pre-1.0, minor bump. SemVer-noted.
- **Two adversarial audit rounds** (R1 SOUND WITH FIXES → F1 flaky-test/F2 -run/F3 Eventually/F4 all folded;
  R2 SOUND + one MINOR folded).

## 5. Next actions

1. **Get explicit user approval to merge `feat/scheduled-send` → `main` and push** (the whole-branch gate is
   clean; this is the only remaining step for this increment). On approval: merge (`--no-ff`, matching the Plan
   008/009 merge-commit style), push `main`, then delete `feat/scheduled-send` (local; it was never pushed).
   **Commit this HANDOVER** with/ahead of the merge (docs artifact) — subject to the never-commit-without-approval
   rule.
2. **After merge:** brainstorm → spec → ADR → plan → 2-round audit → ask-before-code for the **gocron recurring /
   cron source adapter** (Spec 005 O5-5), as an isolated module.

## 6. Gotchas / environment

- **Go 1.25 pinned:** always `GOTOOLCHAIN=go1.25.12`. Stdlib + `clockwork` only — **no new dependency**
  (`clockwork` is already a direct root dep; `go mod tidy` stays clean).
- **Docker IS available** here — the `dbtest` real-DB conformance (PG/MySQL/MariaDB/SQLite via testcontainers)
  runs; the new case is `/Outbound/ScheduledSendDelaysVisibility` (select with `-run '/Outbound/ScheduledSendDelaysVisibility'`,
  NOT `TestConformance.*`).
- **Tooling:** `golangci-lint` on PATH; `gopls`/`govulncheck` under `$(go env GOPATH)/bin` (`govulncheck` not on
  bare PATH — call by full path).
- **Custom skills (mandatory):** start Go work from `cc-skills-golang:golang-how-to`; TDD; `gopls`; `table-test`
  / `use-mockgen` / `use-testcontainers`; blackbox `_test` packages.
- **`.claude/settings.json`** shows modified — user's own file; do not stage/commit it.
