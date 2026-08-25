# Session handover — msgin

> **READ FIRST.** Read `CLAUDE.md`, then this file, then the governing spec/plan/ADR named in §3 — and **trust
> those files and `git log` over this document.** Every count below was measured when written; **re-derive before
> relying on one.** The sharpest evidence for why: a **comment-only** commit (`de38a95`) silently invalidated
> **41** `file:line` citations in an in-flight design bundle that had just passed a dedicated mechanical sweep —
> and the citation *of the finding about stale citations* was itself off by eleven.
>
> ### ✅ Plan 031 is 9 of 10 tasks in. BOTH TASK 10 REVIEWS HAVE NOW RUN. NOTHING MERGED OR PUSHED.
> ### 🔴 Next: **work the 15 open findings in [`docs/plans/031-review-findings.md`](plans/031-review-findings.md).**
> ### `/security-review`: **0 findings.** `/code-review max`: **15**, two of them CLAUDE.md delivery blockers.
> ### **DO NOT MERGE while any finding is un-dispositioned.** Three need design decisions first — see that file §7.
>
> | | State |
> |---|---|
> | Branch | **`chore/backlog-sweep-post-029`**, clean. Count: `git rev-list --count main..HEAD` — do not quote a number here, this file's own commit moves it |
> | `main` / `origin/main` | **`2b2dec1`**, untouched — verify with `git ls-remote origin main`, never `git rev-parse origin/…` |
> | Working tree | **clean.** Derive HEAD with `git rev-parse --short HEAD` — this file's own commits move it |
> | Last **code** commit | **`a2cc568`** (Task 9b). Everything after it is `docs:` only — verify with `git log --format='%h %s' a2cc568..HEAD` |
> | Task 10 Step 3 (8 CI steps × 8 modules) | ✅ **ALL GREEN**, run at `a2cc568`; **still valid**, since every commit since is docs-only — see §6 |
> | Task 10 Step 3b (`GOARCH=386`) | ✅ **vet clean**, root + all 5 touched sql modules |
> | Task 10 Step 4 (docs links) | ✅ arm 1 at its **2 known false positives**; arm 2 **zero**, vacuity-probed |
> | Tags | **zero, as always.** Do NOT propose tagging |

## 1. Where things stand

**Plan 031 — the group-member bound — is 9 of 10 tasks delivered.** Execution order was
`1 → 2 → 4 → 5+6 → 3 → 7 → 8 → 9 → 9b → 10`, not task-number order.

| Task | State | Commit |
|---|---|---|
| 1, 2, 4, 5+6 | ✅ (earlier sessions) | `7abc9f8` · `18e5dc0` · `12cec15` · `355504e` |
| 3 — the `default ≥ completionSizeCeiling` AST invariant | ✅ | `7324e85` |
| 7 — the shared dialect conformance case | ✅ | `920da96` |
| 8 — the SPI contract + interface-level conformance | ✅ | `02c7804` |
| 9 — the class gate's blind spot + count sweep | ✅ | `acdeea5` |
| 9b — fold the two arm rows into Spec 016 | ✅ | `a2cc568` |
| **10 — whole-branch delivery gate** | **⏭️ Steps 3/3b/4 DONE; Steps 0/1/2/5+ REMAIN** | — |

## 2. 🔴 READ THIS FIRST: SIX RATIFIED INSTRUCTIONS PROVED DEFECTIVE AT EXECUTION TIME

**Every single one was found by RUNNING something. None was findable by reading. All had cleared multi-round
adversarial design audits.** This is the dominant lesson of the increment and it should change how the next
session reads the remaining plan text: **treat every ratified claim as a hypothesis, and run it.**

| # | Task | The ratified instruction | What was actually true |
|---|---|---|---|
| 1 | 3 | B3-1: *"change any of the three literals ⇒ fails"* | **Half the directions must PASS.** All three constants are `1<<16`, so the invariant holds as **equality**; raising a default or lowering the ceiling *strengthens* it. 3 of 6 killed, 3 correctly survived |
| 2 | 3 | B3-2: a renamed constant yields a vacuous **`0 >= 0`** | **Wrong constant and wrong arithmetic.** Renaming a *store default* still fails (`0 >= 65536`). Only renaming the **ceiling** exposes the guard: **`65536 >= 0`** |
| 3 | 7 | D-AS: *"pass `maxMembers+1` from `ClaimGroup`"* | **Unimplementable AND incapable.** `ClaimGroup` has no `maxMembers` parameter — D-AS's own point — and `LIMIT 5` cannot truncate 4 rows. **Run: SURVIVED.** Use a limit that bites (`0`→`3`) |
| 4 | 7 | `ExpiredGroups` was covered | **It had NO mutant at all.** `LIMIT 1` on the reaper — dropping every member of an expired group past the first — **passed all 14 subtests** |
| 5 | 8 | Step 2: the counted set is *"implementation-specific"* | **Pre-reversal D-AF.** Revision 4 reversed it; §3.7 requires live **AND** claimed |
| 6 | 9 | Step 3's derivation script **is the remedy** | **The remedy was built out of the defect.** Its selector is a hand-typed number list `N='12\|17\|19\|21\|27\|44\|46'`, blind to 13/18/20/45, and `substr($0,1,2)=="//"` misses indented comments. It **missed 9 stale sites**, including the file's canonical partition line |

**Defect 6 is the one to internalise.** The script existed *because* four rounds of hand-patched counts kept
being overtaken (7 → 12 → 14 → 16 → 17 sites). It was then written from the digits that happened to be wrong
last time — so it could only find last time's defect. **The fix that shipped is deleting the digits, not a better
selector:** seven per-arm restatements collapsed to one, two assertion messages now formatted from `len(...)`,
the hand-maintained ordinal removed. Script arms C, D2 and E now return nothing, permanently.

## 3. Traceability — read before acting

- `CLAUDE.md` (binding). Its counts are **command-derived** — the command is the authority, never the number.
- **Plan 031 (live):** [`docs/specs/017-group-member-bounds.md`](specs/017-group-member-bounds.md) ·
  [`docs/adrs/0033-group-member-bounds.md`](adrs/0033-group-member-bounds.md) **(ACCEPTED)** ·
  [`docs/plans/031-group-member-bounds.md`](plans/031-group-member-bounds.md) · audit rounds
  [1](plans/031-audit-round-1.md) [2](plans/031-audit-round-2.md) [3](plans/031-audit-round-3.md)
  [4](plans/031-audit-round-4.md) — **immutable**
- **Amended by this increment:** [`docs/specs/016-sizing-option-bounds.md`](specs/016-sizing-option-bounds.md)
  (delivered by Plan 029; Task 9b folded in the two new rows and now carries an *"Amended by"* list).

## 3b. 🔴 THE WHOLE-BRANCH GATE HAS RUN — 15 OPEN FINDINGS

[`docs/plans/031-review-findings.md`](plans/031-review-findings.md) is the disposition list. **Read it before
any other work.** Highlights:

- **`/security-review`: 0 findings.** 5 candidates raised, 4 refuted at confidence 9, 1 at confidence 2. **No
  security blockers on this branch.** It examined the same `selectLimit` overflow and `Handle` overflow branch
  the code review flags and correctly found them not *exploitable* — they remain **correctness** defects. The
  two gates corroborate; they do not conflict.
- **Two CLAUDE.md delivery blockers**, both re-verified by the coordinator independently: an uncovered typed-error
  branch (`sql/groupstore.go:424`, hit count `0`) and `selectLimit(math.MaxInt)` wrapping to `math.MinInt`, which
  silently disables **both** the cap check and the fetch bound for a caller who opted into the largest bound.
- **Every per-task adversarial review on this branch came back clean.** These surfaced only at branch level —
  the *"whole-branch review catches what per-task misses"* lesson holding for the second time.
- **Two findings are in code delivered THIS session**: `dialectEngine` (Task 7) returns `""` for a pointer-typed
  dialect — the form the SPI godoc recommends — and Task 8's godoc claim *"unmarked, hence transient"* is false
  when the aggregate error is itself `Permanent`.

**Three design decisions must be made and recorded in ADR 0033 BEFORE any code is written** (findings file §7):
does `AddMember` gain `leaseTTL`; does the SPI reject `maxMembers <= 0` with a typed error plus an explicit
unbounded sentinel; is the *declined*-vs-*failed* merge upheld or reversed (a shipped test asserts the current
behaviour, so reversing it is a deliberate change, not a fix).

## 4. What Task 10 still needs

**Step 0 — THREE-ARTIFACT RECONCILIATION, before any review.** For every finding this bundle dispositions,
confirm the fix is in **all three** of Spec 017, ADR 0033 and Plan 031. **Diff them; do not spot-check.** This is
the project's named failure mode and it has recurred in *three separate revisions* — including revision 4, where
the step was skipped and R4-6 survived because of it. Note that this session amended all three together each
time a defect from §2 was found, so the delta should be small — **but verify, don't assume.**

**Steps 1 and 2 — ✅ BOTH HAVE RUN** (see §3b). What remains is **working the findings**: fix or explicitly
triage each with a written rationale, then **re-run both reviews and the `-race` suite**. The reviews are
user-invocable only (§5), so the fix session must ask the user to re-run them.

**Steps 5+ — status lines.** `docs/specs/017-*` still reads **"DRAFT — revision 5 … NOT approved for
implementation"** while its code has shipped. Plan 031's own status lines need the same pass.

## 5. 🔴 `/code-review` AND `/security-review` ARE USER-INVOCABLE ONLY

The `Skill` tool refuses both with `disable-model-invocation`. **The model cannot run them, and must not claim
the gate passed when it did not run.** In this session an **adversarial reviewer subagent** was substituted —
SDD's own prescribed gate — and it earned its keep: it found the Task 3 wiring defect (§7 item 1) that both the
implementer and the coordinator had signed off on. That substitution is *not* equivalent to the slash commands
for the **whole-branch** gate. **The user must invoke them.**

## 6. Gates already run at `a2cc568` — re-run before merging, but these were green

**Why `a2cc568` and not HEAD:** it is the last commit touching a `.go` file. Every commit after it is `docs:`
only (`git log --format='%h %s' a2cc568..HEAD` to confirm), so these results still hold at HEAD. **Re-derive that
claim rather than trusting this sentence** — the moment a code commit lands, it stops being true.

| Gate | Result |
|---|---|
| 8 CI steps × 8 modules, `GOWORK=off` | **8/8 OK on all 8** (build, vet, gofmt, `CGO_ENABLED=0`, `tidy`+diff, `govulncheck`, `golangci-lint`, `test -race -shuffle=on`) |
| Workspace pass (`GOWORK` unset) | 8/8 build OK |
| `dbtest` | **ok 112s** — postgres, mysql, **mariadb**, sqlite |
| `crontest` | ok 46s |
| `GOARCH=386 GOOS=linux go vet` | clean, root + 5 sql modules |
| Docs links | arm 1 = 2 known FPs; arm 2 = zero, vacuity-probed |

**`harness` reports `[no test files]` — that is a FALSE PASS, not a green.** Check it with `go vet`.

## 7. Carry-forward — open, unscheduled

| Item | State |
|---|---|
| **8 stale `file:line` citations in Spec 016 §2.1's table** | Measured at Task 9b, **not fixed** (out of scope). Expressions are all still correct; only coordinates rotted. **One was broken by Plan 031's own commits** — the `maxGroups` guard cited at `adapter/memory/groupstore.go:108` now sits at `:241` and reads `len(s.groups) >= s.maxGroups`; line 108 is a comment. Re-derive with `grep -n 'len(s.groups) >=' adapter/memory/groupstore.go`, never the coordinate. The two rows Task 9b added deliberately cite *file + expression with no line number* |
| **B6 mutants ran per-implementation, not 10×3** | Task 7 ran 8 on sqlite, B6-6 on postgres+mysql, B6-10 on sqlite+postgres; the coordinator spot-checked **B6-8 on all three**. A full 10×3 sweep is ~16 more Docker runs. Judged low-yield (the assertions are shared and execute against all four runners) but **not** done |
| **`AST checker → permanent gate`** | Plan 030 Task 1's throwaway checker. Task 3 shipped a *second* root AST gate (`group_member_bound_invariant_test.go`); a general one is still unwritten |
| **`WithPollMaxBatch`'s safe-arm row is magnitude-insensitive** | Pre-existing, verified not a regression. Fix: assert on batch **size**, not eventual arrival |
| **`resilience.NewTokenBucket` ordinal** | **CLOSED** at Task 9 — removed rather than corrected. A hand-maintained index is the defect class, not an instance of it |

## 8. Gotchas — these will bite

- **A mutant that does not COMPILE reports as a KILL; one that fails to APPLY reports as a SURVIVAL.** Both
  happened repeatedly this session — three false kills in one Task 3 run (renaming only a `const` breaks the
  build, so the test never runs), and false survivals in Tasks 3, 8 and 9. **Require the mutation to APPLY
  (assert the needle was present and the file changed) AND `go build ./...` to pass, BEFORE recording either
  outcome.** Rename identifiers **file-globally**. Log a value the test computes so a vacuous run is visible.
- **Check a mutant's ARITHMETIC against the fixture's real magnitudes.** Defects 1, 2 and 3 in §2 are all this.
- **`git checkout -- <file>` is NOT a revert for an UNTRACKED file** — exits 0, does nothing. It bit an agent in
  Task 3. Revert from a copied pristine backup, proved with `diff -q` / `git hash-object`.
- **There is a FOURTH dbtest runner.** `TestMariaDBConformance` sets `kit.Name = "mariadb"` while running the
  **mysql** dialect. Every anchor list and the plan omitted it; an engine-render assertion keyed on `kit.Name`
  fails there. Derive the engine from the dialect's package path.
- **`adapter/database/sql` is NOT a module.** `find . -name go.mod` lists eight and it is not among them.
- **A leaf-module import resolves under `go.work` but NOT under `GOWORK=off`.** Task 9's probe A depends on this:
  a workspace-only probe would wrongly conclude a leaf key is adoptable by a root test.
- **`byArm` has NO key for an empty arm.** Writing `"deferred": 0` FAILS the assertion.
- **`apidiff` exits 0 even when it reports changes** — the OUTPUT is the signal. It is blind outside root.
- **`go vet` aborts after the first error per package.** For the full 386 list use
  `GOARCH=386 GOOS=linux go test -gcflags=all=-e -run=NONE ./...`.
- **`govulncheck` lives in `$(go env GOPATH)/bin`**, not on `PATH`.
- **`*-audit-round-*.md` and `*-derivation-findings.md` are IMMUTABLE.** Delivered plans are not.
- **`gopls` / the `LSP` tool is NOT available here.** Plans mandate it; substitute `go doc` + greps and say so.
- `GOTOOLCHAIN=go1.25.13`. `.superpowers/` is git-ignored. Never commit `.claude/settings.json`.

## 9. Pending approvals — nothing here was decided for you

1. **Adopting `github.com/gin-gonic/gin`.** Untouched. ADR 0024 remains reserved-but-unwritten, deliberately —
   writing it decides the dependency by side effect.
2. **Merge, push, tag, branch deletion.** None taken. **Nothing has left this machine.**
