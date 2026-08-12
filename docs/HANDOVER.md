# Session handover — msgin

> **READ FIRST.** Read `CLAUDE.md`, then `docs/plans/027-core-package-layout.md` (its **Progress table** is
> the authority on what is done), then `docs/specs/014-core-package-layout.md`. **Trust those files and
> `git log` over this one**, and over any memory.
>
> ### ✅ TASK 11 IS DONE. THE WHOLE-BRANCH `/code-review` HAS RUN AND ITS FINDINGS ARE FIXED.
> ### THE NEXT STEP IS TASK 12 — the last task in Plan 027.
>
> **`/security-review` HAS NOT RUN.** It is the remaining half of the whole-branch gate and **the user must
> run it** — the assistant cannot launch it.

## 1. Objective & position

`msgin` is a Go 1.25 Enterprise Integration Patterns library. The active effort is the **pre-v1 core
refactor** (Plan 027): flatten-to-packages, channel interface segregation, EIP lexical alignment.

| Task | Size | State |
|---|---|---|
| **11** — package docs + unowned godoc obligations | M | **DONE** (`88e3e38`) — 11a/11b/11c all complete; 16/16 gates GREEN |
| **12** — migration guide, doc sync, whole-branch gate | M | **NOT STARTED — START HERE** |

## 2. Exact state

**VERIFY THESE, NEVER COPY THEM** — they have been wrong in seven consecutive handovers:
`git rev-parse --short main @{u}` · `git rev-list --left-right --count @{u}...HEAD`.
Measured **before** this file's own commit: `main` = `0de54e9`, `@{u}` = `6f44db6`, **31 ahead of `main`**,
**28 unpushed**, 0 behind. Branch `claude/repo-structure-refactor-jt79t1`. Committing this file makes them
32 / 29.

**This file cannot state its own commit's SHA** — committing it invalidates it. Identify `HEAD` by *subject*:
run `git log --oneline -3` and read the top line. `88e3e38` and every SHA below it are ancestors and safe to
cite. `git status --short` was **clean** at the last safepoint.

### What landed this session

| SHA | Task | Substance |
|---|---|---|
| `88e3e38` | **11** | Seven godocs closing Spec 014 §8/§10's obligations (Correlation Identifier, the AMQP disclaimer, two Spring FQNs, the widened HTTP `target`, the single-process guard, the per-instance retry bound) **plus a review round**: a false durability promise corrected, and **all ten Plan-only gates rewritten** after four were proven to pass with their obligation deleted |
| *(top of log)* | 12-prep | The whole-branch `/code-review` findings: `divertTerminal`'s hook now joins the sink error on the discard arm, and `pollErrorBackoff`'s false "six iterations" invariant corrected. Spec §2.1 gains **register row 10** |

## 3. Read in this order

1. `CLAUDE.md` — hard rules. **SDD is the default execution mode; ask before writing any implementation code.**
2. `docs/plans/027-core-package-layout.md` — **Task 12 is at the section `^## Task 12`.** Progress table is
   authoritative. Task 11's execution record carries the gate-design lessons Task 12 must not undo.
3. `docs/specs/014-core-package-layout.md` — §4.1 (the 97 removals in five classes, which `MIGRATION.md`
   must reconcile), §2.1 (the behavior register, **now ten rows**), §8/§8.0a (the godoc obligations).
4. `.superpowers/sdd/027-core-package-layout/progress.md` — this session's ledger, §F19/§F19b/**§F19c**.
   **Git-ignored scratch — read it, don't rely on it surviving.**

## 4. Decisions this session

| | Decision | Outcome |
|---|---|---|
| **Gate anchoring** | *User ruling.* Tighten **all ten** Plan-only gates, including Task 9's `8.4c`–`8.4f` — fix the class, not the instance | every gate now counterexample-proven |
| **Invalid-sink loss** | *User ruling.* Signal it by **joining the sink error**, not by minting a sentinel — a new sentinel would move root's count, which §4.1 and Task 12 both pin | Spec §2.1 row 10; `apidiff` unchanged |
| **Commit shape** | *User ruling.* Amend Task 11 rather than pile on a follow-up, so the godoc and the gates that prove it stay one unit | `88e3e38` |

**No pending approvals** beyond the standing rule that any commit the plan does not spell out needs one.

## 5. Backlog — triaged, NOT fixed

- **24 nil-option-element sites** (carried, unchanged). Every `for _, opt := range opts { opt(&cfg) }` calls
  the element unguarded, so `NewConsumer(src, h, nil)` panics. A **uniform class needing one uniform answer**.
  **Re-derive the list; do not trust any written copy.**
- **`admit`'s ctx-done arm has no test forcing it** — coverage there is incidental.
- **The no-sink discard arm is still not machine-detectable** (Spec §2.1 row 10 states why this is deliberate:
  it is a static misconfiguration, not a transient loss). Revisit only if a caller asks.
- **Task 12 also owns** the `CLAUDE.md` sync (module counts, the Dependency-policy "Still outstanding"
  sentence, sentinel/exported counts) and two `NOT YET IMPLEMENTED` blocks in ADR 0028 belonging to Task 9.6.

## 6. Next actions

1. **START HERE: Task 12.** `MIGRATION.md`, the doc sync, and the whole-branch gate.
2. **Ask before writing implementation code, and default to SDD.** Plan approval does **not** authorize the
   execution mode; approval is per-task, never standing.
3. **Run `/security-review` over `main..HEAD`** — the missing half of the gate. **The user must run it.**
4. Re-run `/code-review` after Task 12's changes; its two findings this round were both in code from *earlier*
   tasks, surfaced only by the branch-wide sweep.

## 7. What this session's gates caught — read before trusting a green suite

| Defect | Found by |
|---|---|
| A public godoc promising **durability the type does not deliver** (`QueueChannel` "survives the instance") — shipped behind 16/16 GREEN and a comments-only proof | adversarial review |
| **Four gates GREEN with their obligation deleted outright** — the class round 6 believed it had killed, surviving in the ten gates nothing ever diffed | adversarial review, by counterexample |
| **Three files' work silently dropped from a commit**, with every gate still GREEN | reading `git status --short` **after** the amend |
| A `grep -v '^---'` arm that **errored and "passed"** without running (BSD `grep` reads `---` as flags) | probing the check with a planted deletion |
| Messages lost to a down invalid-message sink with **no signal a caller could count** | whole-branch `/code-review` |

**The lesson that outranks the others: THE GATES MEASURE THE WORKING TREE, THE ARTIFACT IS THE COMMIT.**
`git checkout <sha> -- <file>` — used by per-file mutation loops — writes to the worktree **and the index**;
restoring with `cp` does not unstage. Before citing any gate result as evidence about a commit, prove
`git status --short` **and** `git diff HEAD --stat` are both empty, or run the gate on a pristine checkout.

## 8. Gotchas & environment

- **`export GOTOOLCHAIN=go1.25.12`** always; **`export PATH="$(go env GOPATH)/bin:$PATH"`** — `apidiff`,
  `gopls`, `govulncheck`, `gofumpt` live there and none are on `PATH`.
- **`./...` is not the repo — there are EIGHT modules.** CI now covers all eight.
- **The §11 gate block was REWRITTEN this session.** Use the block in the plan; **any gate command quoted in
  older prose is stale**. Two traps it encodes: `--include='*.go'` matches `_test.go`, and `go doc` prints the
  **signature**, so a conjunct naming a parameter type self-satisfies.
- **`docs/plans/027-tools/symmap.tsv` is a DERIVED gate input** and goes stale on every symbol addition. It is
  **96** entries at `88e3e38`. Regenerate before running the staleness sweep.
- **`43 ≠ 43`.** Root's sentinel count reads 43 across several revisions but the **sets differ**. Reconcile by
  name, never by count.
- `go tool cover -func` resolves line numbers against the **current** source — running it on a profile from
  another revision silently mis-attributes every block.
- `.golangci.yml` sets `linters.default: none` — **`ST1000` and `unused` are both off**, so missing package
  docs and dead code after a move are reported by nothing.
- `gofumpt -l .` reports **43 files repo-wide** and always has — pre-existing, zero delta. **CI gates
  `gofmt`** (`ci.yml:71`), which is clean.
- The docs-link gate's arm 1 emits exactly **two** known false positives (`docs/plans/m`,
  `docs/specs/factory(fireTime`) — both Go identifiers, not links. Anything else is real.
- `gopls` has **no Move refactoring**. Repo has **zero git tags** — do NOT propose tagging.
- Never commit `.claude/settings.json`; stage explicit pathspecs.
- **Per-task commits are pre-authorized** by the approved plan; `git push`, merges, tags and branch deletion
  are **not**. A commit the plan does not spell out needs its own approval.
