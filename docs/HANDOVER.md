# HANDOVER — msgin

> **Next session: read this first.** Before acting, read `CLAUDE.md`, then the governing artifacts for
> the active increment: `docs/specs/001-messaging-core.md` (§7.4.1, §7, §8, §9), **`docs/adrs/0010-poller-sql-adapter.md`**,
> and **`docs/plans/005-poller-sql-adapter.md`**. **Trust those files over any memory.** Written at a
> clean safepoint: **design for Plan 005 is COMPLETE, two-round-audited, and reconciled — but NOT yet
> implemented, and the design docs are UNCOMMITTED** (see §2). `main` builds and is race-green (no code
> changed this session — only docs).

_Updated: Plan 005 design finalized (Poller + `sql` adapter + outbox + dedup-inbox), both adversarial
audit rounds folded in, D9 deferred. Ready to start SDD at Task 1._

## 1. Objective & roadmap position

`msgin` (`github.com/kartaladev/msgin`) — a Go 1.25 Enterprise Integration Patterns library (minimal
deps). Plans 001–004 landed on `main`. **Active increment: Plan 005 — Poller + `database/sql` adapter.**

- **Design phase: DONE.** ADR 0010 (D1–D8, D10; **D9 deferred**) + Plan 005 (**11 tasks**) drafted,
  hardened by **two rounds** of independent adversarial Opus audits, and fully reconciled.
- **Implementation phase: NOT STARTED.** No source code written; no branch created. **Resume at Plan 005
  Task 1** via SDD (`superpowers:subagent-driven-development`).

**Scope (Plan 005):** the shared **Poller** with credit-at-fetch (spec §7.4.1) + the **`sql` adapter**
(lease/claim + lock strategies × PostgreSQL + MySQL, at-least-once) + transactional **outbox** (D8) +
different-DB **idempotent dedup-inbox** (D10). **Deferred to a follow-up plan:** transactional consume
(D9) + the `Delivery.BindContext` core hook (the audit proved the single-carried-tx model needs a
redesign — see ADR 0010 D9 "Why deferred" / "Intended Plan-006 redesign").

## 2. Exact state — DESIGN COMMITTED on branch `feat/poller-sql-adapter` (for cross-machine handoff)

The design phase's output is committed **standalone** (per CLAUDE.md's new cross-machine-handoff exception
to couple-with-code) on branch **`feat/poller-sql-adapter`** (off `main` @ `86ffa11`), and pushed to
`origin` — so it survives a fresh clone on another machine. The commit is a `docs:` commit containing:

```
CLAUDE.md                              # "Sensible defaults" principle + the cross-machine commit exception
docs/HANDOVER.md                       # this file
docs/specs/001-messaging-core.md       # §9 status-column reconciled → ADR 0010 D4
docs/adrs/0010-poller-sql-adapter.md   # the design of record (D1–D8, D10; D9 deferred)
docs/plans/005-poller-sql-adapter.md   # the 11-task implementation plan
```

**No source code is committed** — implementation has not started. On the other machine: `git fetch &&
git checkout feat/poller-sql-adapter`, then resume at Task 1 (§5). Each task's **code** lands in its own
`feat`/`fix` commit on this branch, carrying `Plan: 005` / `ADR: 0010` trailers back to this design commit.

## 3. Traceability pointers (read first, in order)

1. `CLAUDE.md` — workflow, gates, conventions, the new **sensible-defaults** principle.
2. `docs/specs/001-messaging-core.md` — §7.4.1 (credit-at-fetch), §7 (settlement), §8 (framing), §9 (`sql`).
3. `docs/adrs/0010-poller-sql-adapter.md` — decisions D1–D10 (D9 **DEFERRED**); read Consequences for the
   full audit-fix list and the retraction.
4. `docs/plans/005-poller-sql-adapter.md` — 11 tasks with interfaces, hot-path branches, test targets;
   read "Self-review notes (post-audit, 2 rounds)" at the end.

## 4. Decisions this session, deviations, pending approvals

**Key decisions (user-ratified):** generic `sql` constructors with driver auto-detect + `WithDialect`
opt-in + **exported `Dialect` SPI** (correctness-for-flexibility trade-off, ADR 0010 D3); keep package
name `sql` + documented alias convention; explicit clear names (`NewPollingSource`/`NewOutboundAdapter`);
lease default TTL **5m**; **outbox strict-by-default** (`WithSharedTransaction` strict /
`WithOpportunisticSharedTransaction` + WARN); durable publishing = **outbox only** (cross-DB needs 2PC →
rejected; CDC adapter = backlog); durable consume = two opt-in strategies, **D10 dedup ships / D9
transactional-consume deferred**; **full lock+lease matrix kept** with all audit fixes.

**Deviations / retractions:** the round-1 "lock-strategy `delivery_count` is restart-durable" claim was
**retracted** (round-2 CRITICAL 2); fixed by **Nack-always-commits** + **detached claim-tx** (ADR 0010
D5, Plan Task 8).

**Pending approvals (blockers for the next session):**
1. **SDD execution mode** — subagent-driven (recommended) vs inline (`executing-plans`).
2. **Commit strategy for the uncommitted design** — leave it to ride with Task 1's first `feat` (default,
   couple-with-code), OR commit the design docs now for durability (deliberate deviation — needs explicit
   OK). CLAUDE.md's never-commit-without-approval rule applies.
3. Branch creation (`feat/poller-sql-adapter` off `main`) — the per-task commits enumerated in Plan 005
   become pre-authorized only after the plan is approved AND an SDD mode is chosen.

## 5. Next actions (resume here)

1. Read the artifacts in §3. Confirm `main` green: `GOTOOLCHAIN=go1.25.0 go test ./... -race`.
2. Resolve §4 pending approvals with the user (SDD mode; commit-now-or-with-Task-1).
3. Create branch `feat/poller-sql-adapter` off `main`.
4. Drive **Task 1** (poll-pacing options + sentinels + config plumbing — small, pure config/errors, no
   DB), red→green TDD, then its per-task commit. Proceed through Tasks 2–11.
5. Whole-branch gate (CLAUDE.md §5) before landing; then fast-forward `main` + push + delete branch.

## 6. Gotchas / environment

- **Go 1.25 pinned:** always `GOTOOLCHAIN=go1.25.0` (local default is newer). No `toolchain` directive.
- **Docker IS running** — Plan 005 Tasks 4–11 use `use-testcontainers` (real Postgres + MySQL). Add
  test-only deps then (`testcontainers-go`, a Postgres driver, `go-sql-driver/mysql`) — **never a driver
  in non-test code** (dependency policy; ADR 0010 D2 uses a portable `information_schema` probe, not
  driver-error sniffing, precisely to avoid this).
- **No core SPI change in Plan 005** — the `Delivery.BindContext` hook is deferred with D9. `spi.go`'s
  `Delivery` is unchanged this increment.
- **Task 1 is DB-free** (options/errors) — a safe, fast first unit to validate the SDD loop.
- **Two audit rounds already done** on the design; don't re-audit the design pre-implementation. The
  per-task SDD reviews + the whole-branch `/code-review`+`/security-review` gate still apply to the code.
- **`gopls@latest` can't run under 1.25** — subagents use `go build`/`vet`/`gofmt` + tests as diagnostics.
- Custom test skills (mandatory): `table-test` (assert-closure), `use-mockgen`, `use-testcontainers`.
  Blackbox `_test` packages only; one `goleak` `TestMain` per goroutine-starting package.
