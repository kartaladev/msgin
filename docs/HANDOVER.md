# Session handover — msgin

> **READ FIRST.** Read `CLAUDE.md`, then `docs/plans/027-core-package-layout.md` (its **Progress table** is
> the authority on what is done), then `docs/specs/014-core-package-layout.md`. **Trust those files and
> `git log` over this one**, and over any memory.
>
> ### ✅ TASK 12'S WORK IS WRITTEN AND VERIFIED, **UNCOMMITTED**. ONE GATE REMAINS.
> ### Plan 027 has no task after this one.
>
> **Nothing has been committed this session.** **Twelve** files sit in the working tree (§2) — eleven
> modified, one untracked. Before they land:
>
> | Blocker | State |
> |---|---|
> | `/code-review` over `main..HEAD` | ✅ **RUN** — 5 findings, **all verified and all fixed** (§5a) |
> | `/security-review` over `main..HEAD` | ✅ **RUN — ZERO findings** (second consecutive clean round) |
> | The commit itself | ✅ **GATE SATISFIED.** Task 12's commit is spelled out in the plan, so it is **pre-authorized** under CLAUDE.md's per-task-commit exception. 12 files staged, `index == worktree` proven |
>
> **Every other gate is green on the final tree**, re-run *after* the review fixes: 8/8 modules `-race`,
> 16/16 godoc gates, `apidiff` unchanged at 97/9, both staleness arms empty, docs-link clean.
>
> `git push`, the merge, and branch deletion each need **separate** explicit approval. None has been given.

## 1. Objective & position

`msgin` is a Go 1.25 Enterprise Integration Patterns library. The active effort is the **pre-v1 core
refactor** (Plan 027): flatten-to-packages, channel interface segregation, EIP lexical alignment.

| Task | Size | State |
|---|---|---|
| **11** — package docs + unowned godoc obligations | M | **DONE** (`88e3e38`) |
| **12** — migration guide, doc sync, whole-branch gate | M | **WRITTEN & VERIFIED, UNCOMMITTED — finish here** |

## 2. Exact state

**VERIFY THESE, NEVER COPY THEM** — they have been wrong in eight consecutive handovers:
`git rev-parse --short main '@{u}'` · `git rev-list --count main..HEAD`.
Measured **before** this file's own commit: `main` = `0de54e9`, `@{u}` = `6f44db6`, **32 ahead of `main`**,
**29 unpushed**, 0 behind. Branch `claude/repo-structure-refactor-jt79t1`.

**This file cannot state its own commit's SHA.** Identify `HEAD` by *subject*: `git log --oneline -3`.
`3c27224` and every SHA below it are ancestors and safe to cite.

### ⚠️ THE TREE IS NOT CLEAN — this is a deliberate safepoint, not a broken state

```
 M CLAUDE.md
 M MESSAGING.md
 M docs/HANDOVER.md
 M docs/RELEASE.md
 M docs/adrs/0028-channel-interface-segregation.md
 M docs/plans/027-core-package-layout.md
 M docs/specs/014-core-package-layout.md
 M endpoint/exchange.go          <- COMMENT ONLY
 M expr/compile.go               <- code: new unexported stringResult
 M expr/provider.go              <- code: both call sites
 M expr/expr_test.go             <- +7 test cases
?? MIGRATION.md                  <- MUST be `git add`ed; 10+ tracked files link to it
```

**Eight are documentation; three are the `/code-review` fixes.** `docs/plans/027-tools/symmap.tsv` is
deliberately absent: it was regenerated and came back **byte-identical**, which is itself the evidence that
Task 11 was comments-only — and it stays identical after the `expr` fix, because that fix adds no exported
symbol.

### What Task 12 delivered

| File | Change |
|---|---|
| **`MIGRATION.md`** (new) | Every moved symbol old→new; the 97 removals in five classes; the `*Expr` → `expr` move; the six signature/behavior changes |
| `CLAUDE.md` | All **five** enumerated sync sites (module counts 7→8, artifact counts, the Dependency-policy *"Still outstanding"* sentence, the CI-gap paragraph, the sentinel/exported counts) |
| `docs/specs/014-…` | §4.1 gains the **fifth removal class**; §5.0's census corrected **16 → 17** with a ninth public position; AC-7's two drifted `poller.go` line numbers |
| `docs/adrs/0028-…` | The two stale `NOT YET IMPLEMENTED` blocks (`:364`, `:401`) — both shipped in Task 9.6 |
| `docs/plans/027-…` | The orphaned pre-amend SHA → `e120d10`; the `/code-review` finding table |
| `MESSAGING.md` | Layer-vs-package note; the governor options marked `endpoint.*`; **`expr-lang` removed from the "core dependencies" list**, where it was flatly wrong |
| `docs/RELEASE.md` | **Was stale and named `expr` zero times.** Now 8 modules, an `expr` tag row, the closed `crontest` CI gap — plus a new warning that **`release.yml` has NO trigger pattern and NO title case for `expr/v*`** |
| `expr/*` + `endpoint/exchange.go` | The `/code-review` fixes — see §5a |

## 3. Read in this order

1. `CLAUDE.md` — hard rules. **SDD is the default execution mode; ask before writing any implementation code.**
2. `docs/plans/027-core-package-layout.md` — **Task 12 is at `^## Task 12`.** Progress table is authoritative.
3. `docs/specs/014-core-package-layout.md` — §4.1 (five removal classes), §5.0 (the census), §9 AC-7.
4. `.superpowers/sdd/027-core-package-layout/progress.md` — the SDD ledger. **Git-ignored, so it is NOT in a
   fresh clone — but it IS on disk on this machine.**

## 4. Verification run this session — all green, all re-derived

Every number below was **measured, not transcribed**. Commands are in Plan 027 Task 12.

| Gate | Result |
|---|---|
| 16-gate godoc block | **16/16 GREEN**, regenerated from the plan; **vacuity-probed** (planting a defect in `channel/pubsub.go` drove `11c1` RED, restoring returned GREEN) |
| `apidiff` vs Task 0 baseline | **97 removals / 9 additions** — exactly the projection |
| Removal partition | 87 relocated + 6 `*Expr` + 1 renamed + 1 segregation + 2 D-I sentinels = **97**, **empty residual** |
| Root surface | **103** exported · **43** sentinels · **14** root `.go` files |
| Dependency direction | root→subpackages **EMPTY**; subpackage→subpackage/root **EMPTY** |
| Staleness sweep (Spec §8.1) | both arms **EMPTY**; **both vacuity-probed** (a planted `msgin.Consumer` and a planted `WithTotallyBogusOption` were each reported) |
| `MessageChannel` census | **17** — the spec's 16 was stale (see §5) |
| 8-module `GOWORK=off` test loop | **8/8 green**, `-race -shuffle=on`; `harness` by `go vet` (it has no tests) |
| vet · gofmt · `CGO_ENABLED=0` · `go mod tidy` | clean in **all 8**; tidy a genuine no-op (no `go.mod`/`go.sum` churn) |
| `govulncheck` · `golangci-lint` | **0 vulnerabilities, 0 issues** in all 8 |
| Coverage `-coverpkg=./...` | **93.7%**; **5** uncovered blocks, all in AC-7's accepted six (the sixth is the flaky `consumer.go:467` arm, covered this run). **Zero unexplained.** |
| SHA reachability sweep | one `ORPHAN` **found and fixed** (§5); now only the known gist-id `UNRESOLVABLE` |
| Docs-link gate | arm 1 = the 2 known Go-identifier false positives; arm 2 clean over **52** anchor links |

## 5. Findings this session — things that were WRONG in the bundle

Task 12 is a measurement task, and measuring found four defects no previous gate caught:

1. **Spec §5.0's `MessageChannel` census was stale — a FOURTH recurrence of the class that section opens by
   warning about.** Published **16** (measured at `c83dde9`); delivered is **17**. Task 9 took it to 15 (the
   plan's Progress row recorded that; the spec was never updated) and Task 10's `expr` module took it to 17.
   A **ninth public send-only position** — `expr.RouteFunc`'s `routes` map — was listed nowhere. Every line
   number in that table had also drifted.
2. **An orphaned SHA in the plan.** It named the parent of Task 11's **pre-amend** commit. The amend
   orphaned it: present in this machine's reflog, **absent from a fresh clone**. Replaced with `e120d10`
   (same tree — amending preserves the parent).
3. **`MESSAGING.md` listed `expr-lang/expr` as a core dependency.** It has not been one since Task 1.
4. **Two `poller.go` block ids in AC-7 had drifted +4 lines** from Task 11's comment-only edits. The blocks
   are byte-identical; a shifted id must not be read as a new gap. The table now says so.

**None of those four required a code change.** All were documentation asserting something the tree contradicts.

## 5a. `/code-review` over `main..HEAD` — RUN, 5 findings, ALL FIXED

**Every finding was verified independently before action; two by writing a throwaway probe test.**

| # | Finding | Fix |
|---|---|---|
| 1 | **`expr.RouteFunc` SILENTLY MISROUTES a named string type.** `AsKind(reflect.String)` constrains the KIND, so `type Region string` compiles — but `key, _ := out.(string)` is an EXACT-type assertion, which fails, yielding `""`; `routes[""]` is nil, which `NewRouter` reads as "no destination". Probe returned **`(nil, nil)`** — no channel, no error, no hook | New unexported `stringResult` extracts **by kind** (`reflect.ValueOf(out).String()`). User ruling: accept named string types, because `AsKind` was deliberately chosen over an exact-type check |
| 2 | **`expr.Correlation` reports the WRONG cause**, same root | same helper; a non-string *kind* is now `ErrPayloadType`, not `ErrNoCorrelation` |
| 3 | `docs/RELEASE.md` stale, `expr` named zero times | fixed; `release.yml` gaps documented, not silently patched |
| 4 | `MIGRATION.md` untracked while 10+ tracked files link to it | **`git add MIGRATION.md` in the delivery commit — do not forget** |
| 5 | `ErrNilSubscription` guard leaks the subscription (LOW) | comment-only; control flow untouched |

**No exported symbol moved** — `stringResult` is unexported and `resultTypeError` already reuses root's
`ErrPayloadType` (D-K), so `apidiff` is still **97 / 9** and `expr` still exports exactly **7** symbols.
`expr` coverage stayed **100.0%**.

**The lesson: findings 1 and 2 sat behind a 100%-coverage module, 16/16 green godoc gates and a clean
8-module `-race` suite.** Coverage proves lines RAN, not that they ran with the input that breaks them.

## 6. Next actions

1. **Ask the user to run `/code-review` and `/security-review` over `main..HEAD`.** Both are required; the
   assistant cannot launch either. Task 11's round proved the branch-wide sweep finds what per-task review
   passed — its two findings were in code from *earlier* tasks.
2. **Resolve or triage every finding**, then re-run the affected review and the `-race` suite.
3. **Commit** the seven files as one unit — the plan spells this commit out, so it is pre-authorized once the
   gate passes:
   ```
   docs: migration guide and doc sync for the core restructure

   Spec: 014
   Plan: 027
   ADR: 0007
   ADR: 0027
   ADR: 0028
   ADR: 0029
   ADR: 0030
   RFC: 0001
   RFC: 0002
   RFC: 0003
   ```
4. **Then Plan 027 is complete.** Push, merge and branch deletion each need **separate** approval.

## 7. Backlog — triaged, NOT fixed

- **24 nil-option-element sites** (carried, unchanged). Every `for _, opt := range opts { opt(&cfg) }` calls
  the element unguarded, so `NewConsumer(src, h, nil)` panics. A **uniform class needing one uniform answer**.
  **Re-derive the list; do not trust any written copy.**
- **`admit`'s ctx-done arm has no test forcing it** — coverage there is incidental.
- **The no-sink discard arm is still not machine-detectable** (Spec §2.1 row 10 states why this is deliberate).
- **`endpoint/consumer.go:467`'s ctx-done arm is genuinely flaky** — ~1 run in 3. Do not gate on it.

## 8. Gotchas & environment

- **`export GOTOOLCHAIN=go1.25.12`** always; **`export PATH="$(go env GOPATH)/bin:$PATH"`** — `apidiff`,
  `gopls`, `govulncheck`, `gofumpt` live there and none are on `PATH`.
- **`./...` is not the repo — there are EIGHT modules.** CI now covers all eight (`grep -c 'dir:' ci.yml` → 8).
- **Regenerate the 16-gate script; it does not survive the session:**
  ```bash
  awk '/^# ==== CANONICAL GATE BLOCK/{p=1} p{print} /^g 11c2 /{exit}' \
    docs/plans/027-core-package-layout.md > /tmp/gate11.sh   # 44 lines, 16 `g ` ids
  bash /tmp/gate11.sh                                        # expect 16 GREEN, 0 RED
  ```
- **A coverage block id rots when ANY line above it moves**, including a pure comment edit. Diff the source at
  the two revisions before calling a shifted id a new gap.
- **`43 ≠ 43`.** Root's sentinel count reads 43 across several revisions but the **sets differ**. Reconcile by
  name, never by count.
- `.golangci.yml` sets `linters.default: none` — **`ST1000` and `unused` are both off**, so missing package
  docs and dead code after a move are reported by nothing.
- `gofumpt -l .` reports **43 files repo-wide** and always has — pre-existing, zero delta. **CI gates
  `gofmt`**, which is clean.
- `gopls` has **no Move refactoring**. Repo has **zero git tags** — do NOT propose tagging.
- Never commit `.claude/settings.json`; stage explicit pathspecs.
- **Per-task commits are pre-authorized** by the approved plan; `git push`, merges, tags and branch deletion
  are **not**.
