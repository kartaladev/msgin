# Session handover — msgin

> **READ FIRST.** Read `CLAUDE.md`, then `docs/plans/027-core-package-layout.md` (its **Progress table** is
> the authority on what is done), then `docs/specs/014-core-package-layout.md`. **Trust those files and
> `git log` over this one**, and over any memory.
>
> ### ✅ TASKS 9.6 AND 10 ARE DONE. THE NEXT STEP IS TASK 11.
>
> Two commits of real code landed this session, each verified independently by the coordinator rather than
> accepted on a subagent's report. The tree is a **clean safepoint**: `git status --short` empty, 11/11 root
> packages green under `-race -shuffle=on`, **all eight** modules green standalone.
>
> **Nothing has been pushed. 26 commits are unpushed** — pushing needs explicit approval.

## 1. Objective & position

`msgin` is a Go 1.25 Enterprise Integration Patterns library. The active effort is the **pre-v1 core
refactor** (Plan 027): flatten-to-packages, channel interface segregation, EIP lexical alignment.

**Remaining: Task 11 → 12.** Task 11 was already PARTIAL before this session and is unchanged by it.

| Task | Size | State |
|---|---|---|
| **11** — package docs + unowned godoc obligations | M | **PARTIAL — START HERE.** 11a done (`1d7fc80`); **11b and 11c remain** |
| 12 — migration guide, doc sync, whole-branch gate | M | NOT STARTED |

**Gate `11c1` is RED and that is correct** — it belongs to Task 11c (`channel.WithSingleSubscriber`'s
single-process clause). Task 9.6 was explicitly forbidden to "fix" it. Do not read its RED as a regression.

## 2. Exact state

```
$ git log --oneline -4
5e7829c feat(expr,core,routing): expression providers as a separate module; widen ErrPayloadType's contract
f460610 feat(core,channel,endpoint)!: probe reply-channel exclusivity at construction
fde23f3 docs(handover): record the five implemented tasks; pin the task headers to their commits
511cefa fix(core,endpoint): guard the nil constructor arguments the With* sweep missed

$ git status --short
(clean, apart from this file's own edit)
```

**VERIFY THESE, NEVER COPY THEM** — they have been wrong in six consecutive handovers:
`git rev-parse --short main @{u}` · `git rev-list --left-right --count @{u}...HEAD`.
Measured at `5e7829c`: `main` = `0de54e9`, `@{u}` = `6f44db6`, **29 ahead of `main`**, **26 unpushed**,
0 behind. Branch `claude/repo-structure-refactor-jt79t1`. Committing this file makes them 30 / 27.

**This file cannot state its own commit's SHA or counts** — committing it invalidates them. `HEAD` is
identified by *subject*: run `git log --oneline -4` and read the top line. Every SHA above is an ancestor
and safe to cite.

### What the two commits delivered

| SHA | Task | Substance |
|---|---|---|
| `f460610` | **9.6** | `ExclusiveSubscribable` + `ErrSharedReplyChannel` (root), `SingleSubscriber` on both `channel` types, `endpoint.WithSharedReplyChannel`, the `safeSingleSubscriber` recover helper, a four-outcome `NewChannelExchange` godoc, and a **seven**-row truth table |
| `5e7829c` | **10** | The `expr` provider module (8th module) with six generic providers, twelve reinstated `*Expr` tests, D-K's consumer+RetryPolicy+DLQ acceptance fixture, root's widened `ErrPayloadType` contract, `ErrNilMessageGroup` + the nil-group choke-point guard, and CI extended to all eight modules |

## 3. Read in this order

1. `CLAUDE.md` — hard rules. **SDD is the default execution mode; ask before writing any implementation code.**
2. `docs/plans/027-core-package-layout.md` — **Task 11 is at line ~2644.** Its Progress table is authoritative.
3. `docs/specs/014-core-package-layout.md` §8 (the godoc obligations 11b owes) and §10 (the multi-instance
   obligations 11c owes), plus §8.0b's **canonical gate block**.
4. `docs/adrs/0030-*.md` (Task 9.6's design), `docs/adrs/0029-*.md` §5.0a–**§5.0d** (Task 10's; §5.0d is new).
5. `.superpowers/sdd/027-core-package-layout/progress.md` — this session's SDD ledger: every dispatch,
   finding, ruling and verification, with the commands. **Git-ignored scratch — read it, don't rely on it
   surviving.**

## 4. Decisions this session

| | Decision | Outcome |
|---|---|---|
| **Nil-group class** | *User ruling.* A `MessageGroupStore` whose `Add` returns a nil group **and** a nil error is an SPI contract violation → **typed error at the single choke point**, not a panic and not a hold. A hold would `Ack` a message the store just said it cannot read back — durable nowhere | `routing/aggregator.go`, `msgin.ErrNilMessageGroup`, ADR 0029 §5.0d (**D-Q**), Spec §2.1 row 9 |
| **Nil-guard scope** | *User ruling.* Fix the **class**, not the instance — the first guard closed 1 of 4 release paths; `WithCompletionSize`, `WithReleaseWhen` and a caller's `WithReleaseStrategy` all still panicked (measured) | one guard, all four paths |
| **Table form** | *User ruling.* Comply literally with CLAUDE.md's assert-closure rule even where a shared-assertion table reads more honestly | `expr/expr_test.go` |
| **`Permanent` by wrap** | Wrap at the producing site rather than widening `IsPermanent`'s enumeration — that set is documented **closed** in Spec §4.1, D-K's reasoning depends on its stability, and enumeration would make the test's `IsPermanent` assertion structurally unkillable | `routing/aggregator.go` |
| **Task 10 split** | *User ruling.* Executed as two subagent dispatches but **one commit**, because the plan spells out one commit for Task 10 and the per-task pre-authorization covers only commits the plan spells out | — |

**No pending approvals.** Nothing is blocked.

## 5. Backlog — triaged, NOT fixed

- **24 nil-option-element sites** (carried from the previous session, unchanged). Every
  `for _, opt := range opts { opt(&cfg) }` calls the element unguarded, so `NewConsumer(src, h, nil)` panics.
  A **uniform class needing one uniform answer**; the previous ledger's §F18 has the site list, the
  reproduction and both candidate fixes (recoverable via `git show 1c4f73e:docs/HANDOVER.md`).
  **Re-derive; do not trust the list.**
- **`admit`'s ctx-done arm has no test forcing it** — coverage today is incidental.
- **M38 discriminates the default release case from the *group* of the other three**, which all fail
  identically. "Proves no case is redundant" is accurate but slightly stronger than that one mutant shows.
- **Task 12 also owns** the `CLAUDE.md` sync (module counts, the Dependency-policy "Still outstanding"
  sentence, the sentinel/exported counts) and two `NOT YET IMPLEMENTED` blocks in ADR 0028 that belong to
  Task 9.6. Its checkbox was extended this session to enumerate all of them — an earlier review caught a
  deferral whose named owner did not actually cover the work.

## 6. Next actions

1. **START HERE: Task 11** (plan line ~2644). 11a is done; **11b** owes Spec §8's unmet godoc bullets and
   **11c** owes Spec §10's multi-instance obligations, including **gate 11c1**.
2. **Ask before writing implementation code, and default to SDD.** Plan approval does **not** authorize the
   execution mode; approval is per-task, never standing.
3. **Run the §11 gate baseline for your task and confirm RED first.** Gates are pinned per task.
4. **Before merge:** `/code-review` and `/security-review` over `main..HEAD`. **The assistant cannot launch
   `/code-review` — the user must run it.**

## 7. What this session's gates actually caught — read before trusting a green suite

Tasks 9.6 and 10 were both green at 100% coverage while carrying real defects. What found them:

| Defect | Found by |
|---|---|
| D-M2's probe-ordering guarantee pinned by **no test** — the pre-D-M2 mutant left all six rows *and* gate 8.13 green | adversarial task review |
| Two assertions on error text that **`expr-lang` itself produces** — deleting msgin's own wrap left the whole suite green | reviewer's independent mutant |
| The expression-text wrap reaching **1 of 3** `PayloadOf` sites | adversarial task review |
| Guarding `defaultRelease` closed **1 of 4** nil-group paths | the implementer, who escalated instead of widening silently |
| Round 2 changed facts; round 1's prose was never re-swept — 8 findings, incl. a **merge blocker** (AC-1 and AC-2 contradicting each other) | scoped re-review |

**Four lessons, all earned this session:**
- **A gate that has never failed proves nothing — same as a test.** One sweep reported CLEAN on every arm and
  was meaningless: zsh does not word-split unquoted parameters, so `grep` got one bogus filename and an `||`
  fallback printed a pass. **Vacuity-probe every gate before believing it** (plant the thing it should catch).
- **The SHA-orphan sweep was itself vacuous in the only case it exists for.** `git cat-file -e … && {…}`
  short-circuits on an unresolvable SHA — i.e. the **fresh-clone** case — so a dropped commit produced a
  *clean* result. It now reports `UNRESOLVABLE` separately from `ORPHAN`.
- **Never export a lying gate across a session boundary.** Two Minor doc findings were fixed rather than
  deferred for exactly this reason: handing a fresh session a command with known false positives is how the
  `ci.yml` `grep crontest` trap was born (three comment lines that made a bare grep read as "already done").
- **Amending destroys the SHA your prose cited.** Task 10 was amended four times; three citations to
  `618dd73` became unresolvable. **Cite by task, not by SHA**, and let Task 12 backfill the final one.

## 8. Gotchas & environment

- **`export GOTOOLCHAIN=go1.25.12`** always; **`export PATH="$(go env GOPATH)/bin:$PATH"`** — `apidiff`,
  `gopls`, `govulncheck`, `gofumpt` live there and none are on `PATH`.
- **`./...` is not the repo — there are now EIGHT modules**, not seven. `expr` is the new one. **CI now covers
  all eight**, including `adapter/cron/crontest`, whose absence was a pre-existing gap Task 10 closed.
- **`docs/plans/027-tools/symmap.tsv` is a DERIVED gate input and goes stale on every symbol addition.** It
  silently under-checks staleness-sweep arm 1 when stale. Regenerate before running the sweep; it is **96**
  entries at `5e7829c`.
- **`43 ≠ 43`.** Root's sentinel count reads 43 both at `dadc775` and today, but they are **different sets**
  — two left, two entered. **Reconcile by name, never by count.**
- **The staleness sweep's declared side is now TWELVE directories** (the eleven plus `expr`). Without `expr`
  it reports `ErrInvalidExpression` as a false survivor on a correct tree — verified by control run.
- `go tool cover -func` resolves line numbers against the **current** source — running it on a profile from
  another revision silently mis-attributes every block.
- `.golangci.yml` sets `linters.default: none` — **`ST1000` and `unused` are both off**, so missing package
  docs and dead code after a move are reported by nothing.
- `gopls` has **no Move refactoring**.
- The docs-link gate's arm 1 emits exactly **two** known false positives (`docs/plans/m`,
  `docs/specs/factory(fireTime`) — both Go identifiers, not links. Anything else is real.
- Repo has **zero git tags** — do NOT propose tagging.
- Never commit `.claude/settings.json`; stage explicit pathspecs.
- **Per-task commits are pre-authorized** by the approved plan; `git push`, merges, tags and branch deletion
  are **not**. A commit the plan does not spell out needs its own approval.
