# HANDOVER — msgin

> **Next session: read this first, then trust the referenced files over any memory.** Read, in order:
> `CLAUDE.md`, then — since Spec 009 is now fully shipped — the governing artifacts for whatever the next
> increment is (see "Next actions"). For the just-shipped Phase 4: `docs/specs/009-splitter-aggregator-endpoints.md`
> (§3.5 D12–D14), `docs/adrs/0019-runtime-expression-evaluation.md` (the "Addendum (2026-07-21)"), and
> `docs/plans/018-expr-sugar.md`. The SDD ledger `.superpowers/sdd/progress.md` (gitignored, local) holds
> per-task history — trust it + `git log` over memory.

## LATEST: Phase 4 (expr sugar, Plan 018) SHIPPED, MERGED to `main` (`e7ef491`, `--no-ff`), and PUSHED to origin; `feat/expr-sugar` deleted. **Spec 009 (Splitter + Aggregator + durable group store + expr sugar) is now COMPLETE — all four phases shipped.** Whole-branch gate PASSED — whole-branch Opus review (code 8-angles + security): **0 Critical / 0 Important**; the audit-critical release-`(bool,error)` refactor verified correct + behavior-preserving; security clean (pure in-process expr-over-Go-values, no new unsafe surface, expr posture = ADR 0019 §6 accepted). Two non-blocking Minors triaged to backlog (see below). Design was **2-round Opus-audited (SOUND-WITH-NITS, all folded)** + an isolated expr v1.17.8 compile+run spike before any code.

## Where we are (2026-07-21)

**Spec 009 fully delivered.** All four phases merged to `main` and pushed:
- **Phase 1 — Splitter (Plan 015):** merged `e4b346d`.
- **Phase 2 — Aggregator + `MessageGroupStore` SPI + `memory.GroupStore` (Plan 016):** merged `94cda1f`.
- **Phase 3 — `sql.GroupStore` durable multi-process (Plan 017 / ADR 0021 + ADR 0020 §8):** merged `c7eb673`.
- **Phase 4 — expr sugar (Plan 018 / ADR 0019 addendum):** merged `e7ef491` (this session), pushed.

`origin/main == main == <the push HEAD>` (see "Exact state").

## What shipped in Phase 4 (for a fresh session with zero context)

Four additive expr surfaces in the core `msgin` package, reusing the in-core `expr-lang/expr` engine — **no DB, no new
dependency** (`go mod tidy` leaves `go.mod`/`go.sum` unchanged). All in `expr.go` (+ a small `aggregator.go` seam):

- **`TransformExpr[A, B any](expression string) (Step, error)`** — projection expr over `{payload, header(k)}`,
  result asserted to B (non-B → `ErrExprResultType`), wrapped via `WithPayload`; delegates to the existing
  `Transform`. Adds an `exprAny` compile output kind (no `AsBool`/`AsKind`).
- **`SplitExpr[A, B any](expression string) (Step, error)`** — expr → slice, each element asserted to B, forwarded
  through the shared `forwardSplit` helper (extracted from `Split`, so sequence headers / `parentID#seq` id /
  correlation are stamped identically). Non-slice/non-B → `ErrExprResultType`.
- **`WithCorrelationExpr[A any](expression string) AggregatorOption`** — correlation key by expr (`exprString`
  compile); empty key → `Permanent(ErrNoCorrelation)` (symmetry with `defaultCorrelate`).
- **`WithReleaseExpr[A any](expression string) AggregatorOption`** — bool release over a NEW group-scoped env
  `groupExprEnv[A]{Messages []exprMember[A]{payload, header(k)}, Size int}` (a per-member VIEW, not raw
  `Message[A]`). Canonical form is **size-gated**: `size > 0 && len(messages) >= size`.
- **New sentinel `ErrExprResultType`** (eval-time result-type mismatch). Expr options report compile errors through
  `NewAggregator` via `aggregatorConfig.optErr` (first-error-wins) → `ErrInvalidExpression`, surfaced before
  `ErrNilOutput`/`ErrExpiryChannelRequired`.
- **Internal release strategy evolved to `func(MessageGroup) (bool, error)`** (public `WithReleaseStrategy(func(
  MessageGroup) bool)` UNCHANGED, wrapped). This landed three audit-mandated correctness fixes in the shipped
  release/reaper paths: **H-1** (`reapGroup` release-error falls through to age-expiry, not early-return), **H-2/H-3**
  (the drain loop is best-effort after the main settle — ALL post-settle exits return nil; a non-nil return would
  Nack an already-settled member → idempotent re-Add → a second aggregate / double-count). Only the top-level
  `releaseOnce` failure still propagates. A shipped Phase-3 test that had enshrined the H-3 bug
  (`TestAggregator_ReleaseDrainLoopReleaseError`, asserted `ErrorIs`) was corrected to assert the swallow (`NoError`).

**Aggregate-by-expr is DEFERRED** (Spec 009 §2 / §3.5 D14): expr cannot build an arbitrary struct and the aggregate
fn is a required constructor arg, so it would need a separate `NewAggregatorExpr` for scalar/slice/map results only —
non-breaking to add later.

## Exact state

- **`main` @ `e7ef491`** (the Phase-4 `--no-ff` merge) — pushed to origin (see below). Branch `feat/expr-sugar`
  **deleted** (local; never pushed to the remote). Working tree clean except the pre-existing
  `M .claude/settings.json` (unrelated, NEVER stage).
- Phase-4 commits now on `main` via the merge: `7647e1f` (TransformExpr + exprAny + ErrExprResultType + design docs)
  → `50ab31b` (SplitExpr + forwardSplit) → `be4a859` (release `(bool,error)` refactor + H-1/H-2/H-3) → `cffdb2d`
  (WithCorrelationExpr / WithReleaseExpr + group env + optErr) → `5b2a6f8` (examples + package doc).
- **API additive** (5 new exported symbols); `WithReleaseStrategy` signature unchanged → **minor SemVer**; **no new
  dependency**. `msgin` has no release tag yet — the tag itself is the distribution when a release is cut.

## Traceability pointers (read first)

- `CLAUDE.md` — workflow/gates (SDD, 2-round audit, table-test/goleak/testcontainers, Go 1.25 `GOTOOLCHAIN=go1.25.12`).
- `docs/specs/009-splitter-aggregator-endpoints.md` — §3.5 (Phase 4, D12–D14; O9-4 resolved dual-param); §2 (aggregate-by-expr deferred).
- `docs/adrs/0019-runtime-expression-evaluation.md` — the "Addendum (2026-07-21)" (A1–A4): surfaces, the two `compile`
  extensions, the per-member view env, and the A3 "Per-call-site error policy" (H-1/H-2/H-3, poison-member M-5).
- `docs/plans/018-expr-sugar.md` — the Phase-4 plan (5 tasks, all done) + the two audit-fold sections.

## Next actions (resume here)

Spec 009 is DONE. There is no active in-flight increment. Candidate next increments (each starts a FRESH design
cycle: brainstorm → spec → ADR → plan → 2-round adversarial Opus audit → **ask the user before implementation** →
SDD → whole-branch gate → merge):

1. **Messaging Gateway** — the synchronous request-reply bridge; Spec 003 §2 named it as the increment after
   Splitter/Aggregator. Likely the natural next EIP endpoint.
2. **Resequencer** — the order-restoring sibling of the Aggregator (Spec 009 §2 non-goal); the sequence headers
   Phase 1 stamps make it a clean addition.
3. **Other group-store backends** — `redis`/`pgx`/`nats` `MessageGroupStore` (the SPI is the seam; Spec 009 §2).
4. **Aggregate-by-expr** — `NewAggregatorExpr[A,B]` for scalar/slice/map aggregates (deferred, non-breaking).

## Backlog (deferred, not blocking anything)

- **Phase-4 Minor (triaged, backlog):** `compileGroup` duplicates `compile`'s ~8-line trim/`expr.Compile`/wrap
  boilerplate — extract a `compileProgram(expression, opts...)` helper (behavior-identical, self-contained).
- **Phase-4 Minor (accepted, no change):** `WithReleaseExpr` is O(n) per `Add` / O(n²) per group (the documented L-3
  trade-off; recommend the Go-func `WithReleaseStrategy` for large groups).
- **From Phase 3:** `decodeGroupRows` fails the whole call on one undecodable stored member header (poison group, no
  dead-letter-after-N escape) — needs externally-corrupt DB bytes; low priority.
- **From Phase 2:** O9-6 nested-correlation godoc note.

## Gotchas / environment

- Go 1.25: prefix every go cmd with `GOTOOLCHAIN=go1.25.12`. `golangci-lint` (2.12.2) / `govulncheck`
  (`~/go/bin/govulncheck`) needed for the gate. `go.work` pins 1.25.0; the local default toolchain is newer (1.26) —
  do not let a stray `go` invocation silently build on 1.26+.
- Blackbox `_test` packages only; assert-closure tables; `t.Context()`; `goleak` on the reaper; `clockwork` fake
  clock for time. Phase 4 touches ONLY the core module (`./`) — the 4-engine `dbtest` conformance (needs Docker) is
  unaffected but is still part of the full `-race` gate.
- expr surface: `expr-lang/expr` is in-core (ADR 0019). New expr endpoints share the `compile[A]` primitive and the
  `exprEnv[A]` (`{payload, header(k)}`) / `groupExprEnv[A]` (`{messages, size}`) envs. Reserved: expr's known
  no-time-budget / no-ctx-cancellation gap is documented on every new godoc — operator/config-authored expressions
  only, not untrusted end-user input.
- SDD helper scripts: `~/.claude/plugins/cache/claude-plugins-official/superpowers/6.1.1/skills/subagent-driven-development/scripts/{task-brief,review-package}`.

_Updated 2026-07-21: Phase 4 (expr sugar, Plan 018) SHIPPED — merged to `main` (`e7ef491`, `--no-ff`) and pushed to
origin; `feat/expr-sugar` deleted; whole-branch gate passed (0 Critical/Important). Spec 009 fully complete. Next
increment: a fresh design cycle (Messaging Gateway / Resequencer / other group-store backends — user's choice)._
