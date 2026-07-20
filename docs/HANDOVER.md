# HANDOVER — msgin

> **Next session: read this first, then trust the referenced files over any memory.** Read, in order:
> `CLAUDE.md`, then for the active increment: `docs/specs/009-splitter-aggregator-endpoints.md`,
> `docs/adrs/0020-splitter-aggregator-group-store.md`, and `docs/plans/016-aggregator.md`. The SDD ledger
> `.superpowers/sdd/progress.md` (gitignored, local) holds per-task history — trust it + `git log` over memory.

## Where we are (2026-07-20)

Executing **Spec 009** — Splitter + Aggregator endpoints (+ durable group store, + expr sugar) — a **4-phase**
increment. Phases each get their own plan + ADR + 2-round adversarial audit + SDD + whole-branch gate.

- **Phase 1 — Splitter (Plan 015): DONE & MERGED to `main`** (merge `e4b346d`, `--no-ff`). Whole-branch gate
  passed (code-review 0 bugs, security-review clean, `-race`/lint/gofmt/CGO/tidy/govulncheck all clean). Branch
  `feat/splitter` deleted. **`main` is NOT pushed** — `git push origin main` awaits explicit user approval
  (main is ahead of origin/main by the Phase-1 commits).
- **Phase 2 — Aggregator (Plan 016): DESIGN COMPLETE, 2-ROUND AUDITED (SOUND-WITH-NITS, all folded), IMPLEMENTATION-READY. NO CODE YET.**
  On branch **`feat/aggregator`** (off `main` @ `e4b346d`; design bundle committed). Round-1 (NEEDS-REVISION: H-1
  concurrency + M-1/M-2/M-3/L) and round-2 (SOUND-WITH-NITS: F1 reaper re-check, F2 cyclic-Send deadlock doc, F3
  `==n` caveat scope, F4 de-stale snippets, F5/F6) BOTH folded into ADR 0020 (§2/§3), Spec 009 (D16), and Plan 016
  (the "⚠️ Audit fold" + "Round-2 re-audit folds" sections — they OVERRIDE the inline task code, esp. the 2-return
  `Remove` signature). **NEXT = SDD execution of Plan 016** (no round-3 needed). See §Phase-2 below.
- **Phases 3 (sql.GroupStore, Plan 017/ADR 0021) and 4 (expr sugar, Plan 018/ADR 0019 addendum): not started.**

## Exact state

- **Branch:** `feat/aggregator` (current). `git status --short`: `M .claude/settings.json` (pre-existing, unrelated,
  NEVER stage), `M docs/adrs/0020-splitter-aggregator-group-store.md` (uncommitted Phase-2 API edits — KEEP),
  `?? docs/plans/016-aggregator.md` (uncommitted — KEEP). **No code files touched on this branch** → tree builds
  and `go test ./... -race` is green (Phase-1 code only). This is a clean safepoint.
- **`main` @ `e4b346d`** (Phase-1 merged). Unpushed.
- The Phase-2 design bundle (ADR 0020 edits + Plan 016) is **uncommitted**; it will ride with Task 1's first code
  commit once revised (couple plan/ADR with code) — or commit standalone if you prefer a cross-session safety net
  (needs user approval either way).

## Traceability pointers (read first)

- `CLAUDE.md` — workflow/gates (SDD, 2-round audit, table-test/goleak/testcontainers, Go 1.25 `GOTOOLCHAIN=go1.25.12`).
- `docs/specs/009-splitter-aggregator-endpoints.md` — §3.2/§3.3 Aggregator + MessageGroupStore; §8 open items (O9-6 nested correlation).
- `docs/adrs/0020-splitter-aggregator-group-store.md` — §1 Splitter (shipped), §2 SPI + memory store, §3 Aggregator, §4 settlement, §5 expiry, §6.
- `docs/plans/016-aggregator.md` — the Phase-2 plan (4 tasks). **Currently pre-revision — see below.**

## Phase 2 — REQUIRED REVISIONS before implementation (round-1 audit, Opus, NEEDS-REVISION)

Fold these into ADR 0020 + Spec 009 D16 + Plan 016, then **run a round-2 re-audit** (H-1 reshapes the SPI, so
re-audit is mandatory per the project's two-round norm). Full audit reasoning is in the round-1 transcript; summary:

- **H-1 (High, design/SPI-level) — concurrent double-release / data-loss.** `Handle` does
  `Add → release-check → agg → output.Send → Remove` as separate un-serialized store calls; the coarse
  `Remove`-by-key is not atomic with the release decision. Under `WithConcurrency>1`, two `Handle`s for the same
  key can both see a complete group → **double-emit** (`>=` release), or a member arriving during the window is
  **silently lost** (exact-count release). The reaper (`Expired → Send → Remove`) races the same way vs `Handle`
  (a group both released to output AND routed to the expired channel). **Invisible to `-race`** (logical, not a
  data race) — so "`-race` clean" is false assurance. The 3-method SPI has no atomic-release seam.
  - **RECOMMENDED FIX (single-process/memory, Phase 2):** give the `Aggregator` a **sharded per-correlation-key
    lock** (e.g. `[256]sync.Mutex`, `key→fnv%256`) held across the WHOLE `Handle` sequence (correlate→Add→
    release→agg→Send→Remove) for a key; different keys stay concurrent (competing-consumers preserved across
    groups). The **reaper acquires the same per-key lock** around its re-check+route+Remove for each expired key.
  - **SPI change:** make `Remove(ctx, key) (MessageGroup, error)` **return the removed group** (nil if absent), so
    the reaper routes exactly what it atomically removed (and `Handle` ignores the return). This avoids a stale
    `Expired` snapshot being routed after `Handle` already released the group.
  - **Document the guarantee honestly:** safe under N>1 **within one process** (memory store). **Multi-process
    durable (Phase 3 sql) needs transactional atomic release** (`DELETE … RETURNING` in one tx) — record as a
    Phase-3/ADR-0021 requirement and note that a per-process lock does NOT cover multi-process. Update ADR §3/§4/§6
    + Spec D16 to state the ACTUAL guarantee (the current "serialized by Add" claim is wrong).
  - **Caveat to document:** exact-count release strategies (`== n`) are inherently racy under concurrency even with
    the key lock if members can exceed n; steer callers to `>=`/`WithCompletionSize` (which the key lock makes
    correct) or worker=1 for exact-count.
- **M-1 — `ErrNoCorrelation` not permanent.** `isPermanent` (reliability.go) matches only `ErrPayloadType`/
  `ErrPayloadDecode`/`ErrPayloadTooLarge` + explicit `Permanent()`. A bare `ErrNoCorrelation` is treated
  **transient → retried to DLQ**, contradicting "permanent → invalid channel". **Fix:** `defaultCorrelate` returns
  `Permanent(ErrNoCorrelation)` (or add it to `isPermanent`), and test the ROUTING (diverted as permanent), not
  just that `Handle` returns the error.
- **M-2 — missing ingress `PayloadOf[A]` fail-fast.** Plan's `Handle` correlates on headers and calls `store.Add`
  WITHOUT asserting the payload → a mistyped message enters the group and only fails at release-time re-assert,
  which (group not removed on error) **permanently stuck-locks the whole group** (every retry re-fails). **Fix:**
  `if _, err := PayloadOf[A](msg); err != nil { return err }` BEFORE `store.Add`; test that a mistyped message
  never reaches the store (group stays empty).
- **M-3 — untested error branches.** `store.Add` error and `output.Send` error branches have no covering test
  (the plan drives a real store/DirectChannel that don't error on demand). **Fix:** add a fake `MessageGroupStore`
  (Add→sentinel) and fake `MessageChannel` (Send→sentinel); assert group NOT removed on Send error.
- **L-1** orphaned group on persistently-failing `agg` (godoc note; expiry mitigates). **L-2** id-less duplicate
  double-counts toward `>=` release (already documented; note spurious-release risk). **L-3** reaper test MUST
  `clock.BlockUntil(1)` before `clock.Advance` (else lost tick → flake/hang). **L-4** pick no-timeout `Run` =
  block-until-cancel returning `ctx.Err()`; drop the unused `strings` import.

**Confirmed CORRECT by the audit (no change needed):** generic boxing (`boxMessage`/`PayloadOf[A]`), **clockwork
v0.5.0 ticker API is exactly as the plan uses** (`NewTicker(d).Chan()/.Stop()`, `FakeClock.Advance/BlockUntil`;
`NewFakeClock().NewTicker` panics on d<=0 but reaper only builds one when timeout>0), snapshot immutability
(`slices.Clone`), `asInt` int/int64/float64 tolerance, `defaultRelease` `msgs[0]` guarded by len==0 early-return,
dependency-inward layering, Splitter→Aggregator `int`-size round-trip, and the 4-task decomposition.

## Next actions (resume here)

1. ✅ DONE: both audit rounds folded (ADR 0020 §2/§3, Spec D16, Plan 016 "⚠️ Audit fold" + "Round-2 re-audit folds").
   ✅ DONE: `git push origin main` (Phase 1). ✅ DONE: design bundle committed on `feat/aggregator` (a `docs:` commit).
2. **SDD execution of Plan 016** — `superpowers:subagent-driven-development`, fresh implementer + reviewer per task
   (4 tasks). **Follow the fold sections in Plan 016 over the inline task code where they conflict** — critically the
   2-return `Remove(ctx,key) (MessageGroup,error)`, the per-key lock in `Handle`+reaper (F1 loop), the ingress
   `PayloadOf[A]` assert, `Permanent(ErrNoCorrelation)`, and the N>1 same-key concurrency stress test (the H-1 proof
   `-race` can't provide). The inline snippets are otherwise-correct starting points but were written pre-fold.
3. Whole-branch gate (`/code-review` + `/security-review` over `main..HEAD`, `-race`/lint/gofmt/CGO/tidy/govulncheck),
   then propose merge (needs explicit approval).
4. Then Phase 3 (sql.GroupStore, Plan 017/ADR 0021 — the multi-process transactional atomic release H-1 needs) and
   Phase 4 (expr sugar, Plan 018).

## Gotchas / environment

- Go 1.25: prefix every go cmd with `GOTOOLCHAIN=go1.25.12`. `golangci-lint`/`govulncheck` needed for the gate
  (govulncheck may be at `$(go env GOPATH)/bin/govulncheck`).
- Blackbox `_test` packages only; assert-closure tables; `t.Context()`; `goleak` on the reaper; `clockwork` fake
  clock for time. Core must NOT import `adapter/memory` (dependency-inward).
- Reserved headers from Phase 1: `HeaderSequenceNumber`/`HeaderSequenceSize` (int), and `Split` stamps a
  deterministic child id `parentID#seq` + `HeaderCorrelationID` = parent id. The Aggregator's default correlation
  reads `HeaderCorrelationID`; default release reads `HeaderSequenceSize` (number-tolerant).
- SDD helper scripts: `~/.claude/plugins/cache/claude-plugins-official/superpowers/6.1.1/skills/subagent-driven-development/scripts/{task-brief,review-package}`.

_Updated 2026-07-20: Phase 1 (Splitter) merged to main (e4b346d, unpushed); Phase 2 (Aggregator) design drafted +
round-1 audited = NEEDS-REVISION (H-1 concurrency), awaiting fold + round-2 re-audit + SDD._
