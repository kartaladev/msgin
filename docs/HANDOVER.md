# Session handover — msgin

> **READ FIRST.** Read `CLAUDE.md`, then this file, then the governing spec/plan/ADR named in §3 — and **trust
> those files and `git log` over this document.** Every count below was measured when written; **re-derive before
> relying on one.** The sharpest evidence for why: a **comment-only** commit (`de38a95`) silently invalidated
> **41** `file:line` citations in an in-flight design bundle that had just passed a dedicated mechanical sweep —
> and the citation *of the finding about stale citations* was itself off by eleven.
>
> ### ✅ BACKLOG SWEEP COMPLETE. ADR 0033 ACCEPTED. Plan 031 is 4 of 10 tasks in. NOTHING MERGED OR PUSHED.
> ### Next: **Plan 031 Task 3** — the AST invariant. Its parse set is complete for the first time.
>
> | | State |
> |---|---|
> | Branch | **`chore/backlog-sweep-post-029`**, clean. Count: `git rev-list --count main..HEAD` — do not quote a number here, this file's own commit moves it |
> | `main` / `origin/main` | **`2b2dec1`**, untouched — verify with `git ls-remote origin main`, never `git rev-parse origin/…` |
> | Working tree | **clean** at `355504e` |
> | Suite | **11/11 root packages green** `-race -shuffle=on`; all 6 touched modules green standalone (`GOWORK=off`) |
> | `GOARCH=386` | **vet clean** (was 24 compile errors at the branch point) |
> | Other gates | `govulncheck` clean · `golangci-lint` 0 issues · `apidiff` 0/0 · docs-link gate at its 2 known false positives |
> | Tags | **zero, as always.** Do NOT propose tagging |

## 1. Where things stand

**The eight-item backlog is settled.** Four delivered, one closed WONTFIX, one adjudicated, one delivered, and the
last — the group-member bound — is **in flight as Plan 031**.

| # | Item | Outcome |
|---|---|---|
| 2 | Dedup the 7 delegator pre-check loops | **CLOSED-WONTFIX** — not a defect (§2) |
| 3 | Guard gate is syntactic | **Adjudicated**; rejection stands, folded into Plan 031 Task 9 |
| 4 | gin plan number + ADR 0024 | **DELIVERED** `7ab91cd` |
| 5 | False "first statement" godoc | **DELIVERED** `1a1c135` + `de38a95` |
| 6 | The byte-ceiling class | **DELIVERED** `f39725d` + `a306241` — 4 audit rounds |
| 7 | Aggregator group growth | **IN PROGRESS** — Plan 031, see §4 |
| 8 | 32-bit test overflow | **DELIVERED** `d2c69fe` — 24 errors → 0 |

## 2. Two results worth reading even if you read nothing else

**Backlog item 2 was never a defect.** Collapsing the seven duplicated delegator pre-check loops turns the
**Spec 015 AC-7 guard gate red at all seven sites** — `hasNilElementGuard` clears a parameter only on an
`*ast.RangeStmt` over it *inside the constructor's own body*, and a helper call is invisible (the helper is
non-variadic, so it is never scanned). The gate ships a committed probe asserting exactly the post-refactor shape
is unguarded. The duplication is the ratified consequence of **ADR 0031 D-R** and that gate acting together.
Repairing it means amending a shipped spec *and* ADR to net ~14 lines. **The gate is working as designed.** Full
chain: Plan 030 decision **D1**. Do not re-propose without first deciding to amend AC-7.

**Selectors must match the property, not the string.** The same defect appeared four ways: `grep "first
statement"` misses ~5 sites because the phrase **wraps across comment lines**; `grep 'must be > 0'` missed six
sites spelling the same contract as `n<=0`; the class-gate site inventory was declared complete and wrong in
**three consecutive audit rounds**; and the fix for that inventory was a *wider token list* that missed three
more. The durable remedy is in Plan 031 Task 9 — **a script whose output IS the table**, verified by an auditor
extracting and re-running it.

## 3. Traceability — read before acting

- `CLAUDE.md` (binding). Its project-status counts are **command-derived** — the command is the authority, never
  the number beside it.
- **Plan 031 (live):** [`docs/specs/017-group-member-bounds.md`](specs/017-group-member-bounds.md) ·
  [`docs/adrs/0033-group-member-bounds.md`](adrs/0033-group-member-bounds.md) **(ACCEPTED)** ·
  [`docs/plans/031-group-member-bounds.md`](plans/031-group-member-bounds.md) rev 5 · audit rounds
  [1](plans/031-audit-round-1.md) [2](plans/031-audit-round-2.md) [3](plans/031-audit-round-3.md)
  [4](plans/031-audit-round-4.md) — **immutable**
- **Plan 032 (delivered):** [`docs/specs/018-byte-cap-ceilings.md`](specs/018-byte-cap-ceilings.md) ·
  [`docs/adrs/0034-byte-cap-ceilings.md`](adrs/0034-byte-cap-ceilings.md) ·
  [`docs/plans/032-byte-cap-ceilings.md`](plans/032-byte-cap-ceilings.md) · audit rounds
  [1](plans/032-audit-round-1.md) [2](plans/032-audit-round-2.md) [3](plans/032-audit-round-3.md)
  [4](plans/032-audit-round-4.md)
- **Plan 030 (delivered):** [`docs/plans/030-post-029-maintenance.md`](plans/030-post-029-maintenance.md) ·
  [`docs/plans/030-audit-round-1.md`](plans/030-audit-round-1.md)

## 4. Plan 031 — 4 of 10 tasks delivered, Task 3 next

**ADR 0033 is ACCEPTED.** The user ratified **D-AC…D-AT** on 2026-08-23 with *"go with best practices"*, having
been shown the four load-bearing choices rather than all eighteen. **Execution order is
`1 → 2 → 4 → 5+6 → 3 → 7 → 8 → 9 → 9b → 10`** — it is not task-number order, and Task 3 sits after 5+6
deliberately (**D-AT**).

| Task | State | Commit |
|---|---|---|
| 1 — the `memory` bound, classification, `Handle`'s branch | ✅ | `7abc9f8` — 21 mutants, 21 killed |
| 2 — the bound holds for **all four** release paths | ✅ | `18e5dc0` — 10 mutants, 10 killed |
| 4 — godoc cross-references (D-AI) | ✅ | `12cec15` — comment-only, AST-proven, 3 probes |
| 5+6 — `sql` + the three dialects + `harness` | ✅ | `355504e` — 6 modules, real Docker run |
| **3 — the `default ≥ completionSizeCeiling` AST invariant (D-AQ)** | **⏭️ NEXT** | — |
| 7, 8, 9, 9b, 10 | pending | — |

**Why Task 3 is next and could not have run earlier:** it parses **three** files by name and asserts
`defaultMaxGroupMembers ≥ completionSizeCeiling`. `sql`'s constant only came into existence at `355504e`, so this
is the first commit at which its parse set is complete. Its non-vacuity guard fires on a missing declaration, so
running it earlier would have failed for the wrong reason.

### 🔴 Task 7 carries two defects found by RUNNING mutants — both are blocking, both are in the task list

Neither was findable by reading; five adversarial rounds approved both.

1. **The plan's own B6-7 mutant is arithmetically incapable of killing.** It pairs *"fill a group to exactly
   `cap`, then `ClaimGroup`"* with *"pass `maxMembers+1`"* — but **`LIMIT cap+1` cannot truncate `cap` rows**. It
   was run and **SURVIVED**. A limit that actually bites (`3` at cap `4`) and baking the `LIMIT` into the
   helper's SQL both **killed** it. Task 7 must use one of those, or D-AS's `ClaimGroup` arm ships an assertion
   that proves nothing. (The literal instruction is also unimplementable — `ClaimGroup` has no `maxMembers` in
   scope, which is D-AS's own point.)
2. **`ExpiredGroups`' half of D-AS is uncovered by the SHIPPED conformance suite.** Mutating its call site from
   `0` to `1` — the reaper silently dropping every member of an expired group past the first — **passes all 14
   subtests on sqlite**. No case asserts an expired group returns more than one member. Task 7 needs the
   `ExpiredGroups` twin of B6-7.

Also stale, owned by **Task 9 Step 3 arm E**: the class gate's ordinal comment *"burst is the 17th key"* is false
(19 keys; `resilience.NewTokenBucket` is 19th). It went stale at Task 1.

And for **Task 10 Step 6**: `errors.go`'s `ErrInvalidCapacity` godoc makes two claims Task 1 falsified — that the
sentinel covers options that are *"the sole bound"* on a growing structure (`WithCompletionSize` no longer is),
and that *"all FOUR producers have landed"* (it is six now). Step 6 already requires reconciling the producers
**by name**; that sentence is the prose which must move with them.

## 5. The standing constraint added at ratification

> *"this library must be flexible with sensible default but with opt-in available."*

It sharpens CLAUDE.md's "Sensible defaults (opinionated, but overridable)" and **governs every knob from here**.
Cite that section rather than duplicating it.

Both group-member options comply: a sensible default (`1<<16`), overridable to `1<<20`, both documented. **No
off-state**, deliberately — unlike a byte cap, the safe value here is *not* unknowable to the library (the cost
curve is quadratic and measured), so a caller above the ceiling is already pathological and the bound protects
them. Adding one later is **purely additive**. At the **SPI** level `maxMembers <= 0` *does* mean unbounded, for a
direct dialect caller who never opted in — unreachable from `sql.GroupStore`, where validation guarantees ≥ 1.
Compare `endpoint.WithMaxPayloadBytes`, which *is* opt-in (`n <= 0 disables`) precisely because a caller's
legitimate payload size **is** unknowable.

## 6. Pending approvals — nothing here was decided for you

1. **Adopting `github.com/gin-gonic/gin`.** Untouched. Plan 030 Task 3 fixed the *false citations* around ADR
   0024 and deliberately did **not** write the ADR, because writing it decides the dependency by side effect.
2. **Merge, push, tag, branch deletion.** None taken. Nothing has left this machine.

## 7. Carry-forward — open, unscheduled

| Item | State |
|---|---|
| **AST checker → permanent gate** | Plan 030 Task 1's throwaway checker pairs each "first statement" comment against its function's `fd.Body.List[0]`. The class grew **4 → 11 → 16** sites across three counts, each time because the detection method was weaker than the defect. An AST invariant ends it; another grep will not. |
| **`WithPollMaxBatch`'s safe-arm gate row is magnitude-insensitive** | **Pre-existing, verified not a regression** — survives an `int32(n)` truncation mutant at *both* `1<<62` and `math.MaxInt`, because polling one message at a time still delivers both. Fix: assert on batch **size**, not eventual arrival. |
| **Derive the class gate's prose counts from `wantArms` at test time** | The partition is restated in ~10 prose locations — including two *live assertion messages* — with no mechanical link to the map the test already computes. Four rounds patched instances and were overtaken (7 → 12 → 14 → 16 → **17**); the 17th carries no arm name, literal or digit, so **no selector can find it**. Designed at [Spec 018 §8 item 5](specs/018-byte-cap-ceilings.md). |

## 8. Gotchas — these will bite

- **Any artifact citing `file:line` is stale from the moment it is written.** Cite the **symbol and the grep that
  locates it**, not a coordinate.
- **A mutant can be wrong.** Check its arithmetic against the fixture's magnitudes before trusting a kill *or* a
  survival, and **run it against the whole suite**, not just the case you wrote. §4 has two instances.
- **A "never fires" negative row needs a paired positive control** — it cannot otherwise distinguish *the guard
  worked* from *the path was never installed*.
- **`gopls` / the `LSP` tool is NOT available in this environment.** Plans mandate it; substitute `go doc` plus
  targeted greps and say so.
- **`git log --oneline | grep -i <plan>` misses commits** whose subject lacks the number. Use the trailer:
  `git log --format='%h %s' --grep='Plan: 031'`.
- **`apidiff` exits 0 even when it reports changes** — the OUTPUT is the signal, never `$?`. It is also blind
  outside the root package.
- **`go vet` aborts after the first error per package.** For the full 386 list use
  `GOARCH=386 GOOS=linux go test -gcflags=all=-e -run=NONE ./...`.
- **Two modules give false passes in OPPOSITE directions:** `harness` has no test files (`go test` passes
  vacuously — use `go vet`); `dbtest` has **only** `_test.go` files (`go build` compiles nothing — use `go vet`).
  `dbtest` and `crontest` are Docker-backed; the real run takes ~2 minutes.
- **`govulncheck` lives in `$(go env GOPATH)/bin`**, not on `PATH`.
- **The docs-link gate has exactly two known false positives** (`docs/plans/m`,
  `docs/specs/factory(fireTime` — Go identifiers in wrapped code spans).
- **`*-audit-round-*.md` and `*-derivation-findings.md` are IMMUTABLE.** Delivered plans are **not** — Plans 020,
  029, 030 and 032 all carry in-place supersession notes.
- **An auditor's summary can disagree with its own table.** It happened twice, both by one row in the same
  direction. Instruct the auditor to **generate the score by counting the table** and to say that it did.
- `GOTOOLCHAIN=go1.25.13`. `.superpowers/` is git-ignored. Never commit `.claude/settings.json`.
