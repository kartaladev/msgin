# Session handover — msgin

> **READ FIRST.** Read `CLAUDE.md`, then this file, then the governing spec/plan/ADR named in §3 — and **trust
> those files and `git log` over this document.** Every count below was measured when written; **re-derive before
> relying on one.** The sharpest evidence: a **comment-only** commit (`de38a95`) once invalidated **41**
> `file:line` citations in a bundle that had just passed a dedicated mechanical sweep. In the session that wrote
> this, **every cited coordinate in three consecutive findings was stale**, and **five documented claims were
> refuted outright by running them** (§2).
>
> ### ✅ ROUND 1: all 15 whole-branch findings fixed, all 5 capped items dispositioned, both blockers closed.
> ### ✅ ROUND 2: the user re-ran **`/code-review high`** — **3 findings, no security issues, ALL FIXED** (§2b).
> ### 🔴 NEXT, AND ONLY THE USER CAN DO IT: re-run **`/security-review`** over `main..HEAD` — §5.
> ### It has **not** run since round 1. Then Task 10 Steps 5–10 (§7).
> ### NOTHING MERGED, NOTHING PUSHED, NOTHING TAGGED.
>
> | | State |
> |---|---|
> | Branch | **`chore/backlog-sweep-post-029`**, clean. Count: `git rev-list --count main..HEAD` — do not quote a number, this file's own commit moves it |
> | `main` / `origin/main` | **`2b2dec1`**, untouched — verified with `git ls-remote origin main`, never `git rev-parse origin/…` |
> | Working tree | **clean.** Derive HEAD with `git rev-parse --short HEAD` |
> | Last **code** commit | derive it: `git log -1 --diff-filter=ACMR --format='%h %s' -- '*.go'`. Anything after it is docs-only |
> | 8 CI steps × 8 modules | ✅ **8/8 on all 8**, re-run after the last code commit — §6 |
> | Workspace build · `GOARCH=386` vet · docs links | ✅ all clean; arm 1 at its 2 known false positives, arm 2 zero |
> | Tags | **zero, as always.** Do NOT propose tagging |

## 1. Where things stand

**Plan 031 Task 11 — working the whole-branch review findings — is COMPLETE.** Task 11 did not exist when the
plan was written; Task 10 Steps 1–2 produced it. Tasks 1–9b were delivered in earlier sessions.

| Task 11 step | State | Commit |
|---|---|---|
| 0 — ratify the three design decisions | ✅ D-AU, D-AV, D-AW | `b78ff09` |
| 1 — three-artifact reconciliation | ✅ clean, by label **and** by substance | `b78ff09` |
| 2 — R-7, R-15, R-10 (+ R-3, R-4) | ✅ | `00bc8b2` |
| 3 — R-6, R-9, R-12 | ✅ | `99f2564` |
| 4 — R-1, R-11, R-5, R-8, R-14 | ✅ two agents, disjoint modules | `8b5727b` |
| 4b — R-2, R-13 (+ D-AX) | ✅ | `ac15650` |
| 5 — §6's five capped items | ✅ 3 fixed, 2 triaged with rationale | `ac15650` |
| 6 — re-run the gate: 8 CI steps × 8 modules | ✅ **8/8 on all 8** (§6) | — |
| 6 — re-run `/code-review` (**round 2**) | ✅ user-invoked; 3 findings, all confirmed and fixed | `f7ada02` |
| **6 — re-run `/security-review`** | ⏭️ **NOT RUN since round 1. NEEDS THE USER** (§5) | — |

**[`docs/plans/031-review-findings.md`](plans/031-review-findings.md) is the disposition list. Its LIVE STATUS
header block is the current stratum; every status line below it is annotated history, not a live claim.**

## 2. 🔴 THE DOMINANT LESSON, NOW PROVEN TWICE OVER

The previous handover recorded **six ratified instructions proved defective at execution time**. This session
added **five refuted claims and one recommended fix that would have crashed** — all found by *running*, none by
reading. **Treat every documented claim as a hypothesis and run it.** A finding that no longer reproduces is a
valid, valuable result.

| # | The claim | What was actually true |
|---|---|---|
| 1 | R-7: `adapter/memory` is a twin of the three dialects | **Not a twin.** `memory` has no lease TTL *by construction*; `!g.leased` is sound there and unsound in `sql`. D-AU is `sql`-only and `memory` was left unchanged |
| 2 | R-6: *"the operator gets a raw driver error instead of the typed one"* | **False.** `classifyQueryErr` **replaces** rather than wraps, and both arms already returned `classified`. The *contract violation* it also alleged was real and was fixed |
| 3 | R-12: *"Fix: `reflect.Indirect(...)`"* | **Would PANIC** on `(*yourDialect)(nil)` — the shape the SPI godoc prescribes. The RED run also **segfaulted** on `reflect.TypeOf(nil).PkgPath()` |
| 4 | R-5: the leased snapshot is *"always empty"* | **False.** `claimedLen == len(msgs)` holds only at the instant `ClaimGroup` returns; `Add` appends beyond it for the width of the lease |
| 5 | R-2's citation of `groupstore.go:78-86` | `git log -S` shows that phrase **only ever lived in `routing/aggregator.go`**. The substantive complaint was still right |

**And one ratified decision was superseded by its own task.** **D-AV** part 3 was ratified as *"`selectLimit`
becomes total"*. R-1 then showed the `LIMIT` **was** the defect, not the thing to make safe — and that the
truncation reached the **success path** too, so any overflow-path-only fix would have been insufficient.
`selectLimit` is **deleted**. Neither five design-audit rounds nor the delivery-gate reviewer saw this.

> **D-AV is NOT thereby redundant, and both artifacts say so.** R-15 had two independent fail-open routes at
> `math.MaxInt`; only the LIMIT half became structurally impossible. The **cap comparison** `n > int64(maxMembers)`
> still cannot fire there, and **nothing but the validator prevents it.** A reader concluding *"R-1 removed the
> overflow, so drop the validation"* would reopen a delivery blocker.

## 2b. Review round 2 — 3 findings, ALL DOCUMENTATION DEFECTS

The user re-ran **`/code-review high`** over `main...HEAD` after round 1 closed. Everything else was green (all 11
root packages `-race`, `golangci-lint` 0 issues, `routing` 100%). **All three were confirmed independently by the
coordinator before any edit, and all three are fixed in `f7ada02`.**

| # | Defect | Fix |
|---|---|---|
| **CR2-1** | The `limit` parameter on the three `*SelectMembers` helpers was **DEAD** — all nine call sites pass `0` — while its godoc still read *"Only AddMember passes a non-zero value (`maxMembers+1`)"*, **precisely the truncation R-1 removed** | **Deleted** the parameter rather than correcting the comment. A dead branch plus a comment instructing its restoration is a live trap |
| **CR2-2** | `sql`'s `WithMaxGroupMembers` godoc documented the **pre-D-AU** discriminator (`locked_by` alone), so it promised transient where the code now returns **Permanent** for a stranded lease | Prose rewritten to match the code, including a two-phase timeline |
| **CR2-3** | `WithCompletionSize`'s *"can always reach its own release"* holds only at the **default** cap | Claim qualified; both `WithMaxGroupMembers` godocs warn. **No validation code** — see below |

**Every one was godoc, not logic.** The only non-comment change in that commit is the dead-parameter deletion.
This is the project's *"docs can contradict the code they describe"* lesson, and CR2-2 is the sharpest form of it:
**D-AU corrected the classification code in this same session and left the option's godoc, two hundred lines
away, describing the old behaviour.**

> 🔴 **CR2-1 SUPERSEDES THE MECHANISM OF D-AS, NOT ITS RULE.** D-AS introduced the parameter because *"one shared
> helper serves three callers with different bounds"*. After R-1 all three want unlimited, so the premise is
> spent — while D-AS's **rule** (*a member set the caller acts on is never truncated*) is now enforced by there
> being **no way to express truncation at all**. Recorded in D-AS, not silently dropped.
> **`ExpiredGroups`' `limit` is a DIFFERENT, live parameter** bounding the *groups* table
> (`defaultExpiredGroupsLimit = 100`) — verified untouched.

> 🔴 **THE REPLACEMENT MUTANT'S OBVIOUS FORM IS A FALSE NEGATIVE, AND IT WAS THE COORDINATOR'S ASSERTION THAT
> FAILED.** The claim *"the harness cases still kill a re-introduced LIMIT"* was measured rather than assumed:
> **`LIMIT 3` kills six cases but SURVIVES `ExpiredReturnsEveryLiveMember`**, whose group holds exactly three
> members — the mutant is arithmetically incapable. **`LIMIT 1` kills both.** This bundle's own recorded trap,
> catching the person who wrote the warning.

**Deliberately NOT built:** CR2-3's runtime check would need a new `MaxMembers() int` method on the
`msgin.MessageGroupStore` SPI so `NewAggregator` could compare against it — public API growth to catch a
misconfiguration. The seam is described in **Spec 017 §3.5**; nothing was built. **This is an open design
question, not an oversight.**

## 3. Traceability — read before acting

- `CLAUDE.md` (binding). Its counts are **command-derived** — the command is the authority, never the number.
  🔴 **Two known-stale figures in it, both logged in Plan 031 Task 10 Step 8:** the `reliability.go:46` citation
  for `IsPermanent`, and the *"53 anchor links"* census — the tree has **55**, and it is not this increment's doing.
- **Plan 031 (live):** [`docs/specs/017-group-member-bounds.md`](specs/017-group-member-bounds.md) ·
  [`docs/adrs/0033-group-member-bounds.md`](adrs/0033-group-member-bounds.md) **(ACCEPTED)** ·
  [`docs/plans/031-group-member-bounds.md`](plans/031-group-member-bounds.md) ·
  [`docs/plans/031-review-findings.md`](plans/031-review-findings.md) · audit rounds
  [1](plans/031-audit-round-1.md) [2](plans/031-audit-round-2.md) [3](plans/031-audit-round-3.md)
  [4](plans/031-audit-round-4.md) — **immutable**
- **Amended by this increment:** [`docs/specs/016-sizing-option-bounds.md`](specs/016-sizing-option-bounds.md) —
  **twice**. Task 9b folded in two `fixed` rows; Task 11 folded in a `rejects` row for `sql.ValidateMaxMembers`.
  **The class gate's arm rows have a FOURTH artifact — that file — and it is not in the obvious bundle.** It was
  missed again at Task 11 and caught by the coordinator at commit time.

## 4. The four decisions ratified this session

All sit in ADR 0033 immediately before `## Consequences`, each with a spec twin.

| ADR | Finding | Substance | Spec |
|---|---|---|---|
| **D-AU** | R-7 | `AddMember` gains `leaseTTL`; classify on lease **expiry**, not on `locked_by` being non-NULL. **`sql` only** | §3.6a.1 |
| **D-AV** | R-15 | `UnboundedGroupMembers = -1`; reject anything outside `{-1} ∪ [1, 1<<20]`, in one shared validator | §3.6a.2 |
| **D-AW** | R-10 | Release **strategy** failure ⇒ `errors.Join`; decline ⇒ bare `err`. The `IsPermanent` **escalation is intended and asserted** | §3.3b |
| **D-AX** | R-2 | Release **execution** failure ⇒ a fresh **transient** `ErrOverflowDropped` interpolating the cause with `%v` | §3.3a.1 |

> 🔴 **D-AW AND D-AX SIT TEN LINES APART IN THE CODE AND LOOK CONTRADICTORY. THEY ARE NOT.** The discriminator,
> now stated in both: a failing release **strategy** is a predicate about *this* group and fails identically on
> every redelivery, so escalating to terminal is correct. A failing **aggregate/`Send`** is about the *other,
> claimed* members' payloads — the refused member is unrelated collateral and must stay redeliverable.
> **Do not "fix" D-AX's `%v` back to `%w`**; severing the chain is the decision, and a mutant guards it.

**Also settled, without an ADR:** the new `sql.ValidateMaxMembers` rejection is `msgin.Permanent`-wrapped, alone
among the `ErrInvalidCapacity` producers, because it is the only one returned from a **per-message hot path**
rather than a constructor (ADR 0031 **D-V**). The sibling `ErrMissingMsgID` at the same site is bare — a genuine
asymmetry, flagged, not resolved.

## 5. 🔴 `/code-review` AND `/security-review` ARE USER-INVOCABLE ONLY

The `Skill` tool refuses both with `disable-model-invocation`. **The model cannot run them and must never claim
the gate passed when it did not.** Adversarial reviewer **subagents** are SDD's prescribed substitute and earned
their keep repeatedly — but that is **not** equivalent for the **whole-branch** gate. **The user must invoke them
over `main..HEAD`, not the last commit.**

**`/code-review` HAS now run twice** (round 1: 15 findings; round 2: 3, all fixed).
**`/security-review` has run ONCE, before any of Task 11's fixes existed.** It must run again: the branch has
since had an SPI signature broken a *second* time, a `Handle` branch grown from six exits to **nine**, two
functions deleted (`selectLimit`, the `limit` parameter), a new exported `harness.DialectEngine`, and a new AST
gate half. None of round 2's findings was security-shaped, but that is not evidence about the code round 1's
security pass never saw.

**Why re-running catches things:** round 1 produced all 15 findings while **every per-task review came back
clean**, and round 2 then found three more in code that had *just* passed round 1's fixes — including one godoc
that this session's own D-AU fix had left stale two hundred lines from the code it changed.

## 6. Gates re-run after the last code commit — all green

| Gate | Result |
|---|---|
| 8 CI steps × 8 modules, `GOWORK=off` | **8/8 on all 8** (build, vet, gofmt, `CGO_ENABLED=0`, `tidy`+diff, `govulncheck`, `golangci-lint`, `test -race -shuffle=on`) |
| Workspace pass (`GOWORK` unset) | 8/8 build OK |
| `dbtest` | green — postgres, mysql, **mariadb**, sqlite |
| `GOARCH=386 GOOS=linux go vet` | clean, root + 5 sql modules |
| Docs links | arm 1 = 2 known FPs; arm 2 = zero |
| Coverage | `routing` **100%** · `adapter/database/sql` 94.2% · root 95.6% · `adapter/memory` 88.0% |

> **`harness` is NO LONGER a false pass** — Task 11 Step 3 gave it its first real test file. Still `go vet` it too.

## 7. What Task 10 still needs (Steps 5–10)

1. **Steps 5/6 — the derived figures. Re-derive, never transcribe.** The class-gate partition moved again:
   **20 keys / 14 fixed / 2 rejects / 0 deferred / 6 safe / 22 rows** at the time of writing. `ErrInvalidCapacity`
   now has **SEVEN** producers — reconcile **by name**; the plan's *"expect six"* was corrected.
2. **Step 7** — refresh CLAUDE.md's Project status counts and this file's; fold in Spec 017 §8's follow-ups.
3. **Step 8** — CLAUDE.md's two stale figures (§3). **Delete the digit rather than update it** where you can.
4. **Step 9 — status lines.** Spec 017 is corrected. **Plan 031 still needs its own pass.**
5. **Step 10** — stage, show the diff, **wait for explicit approval**. `git push`, the merge and the branch
   deletion each need their own separate approval.

## 8. Carry-forward — open, unscheduled

| Item | State |
|---|---|
| **`decodeErr`'s content is discarded** | NEW (findings §5b item 1). A corrupt stored header never reaches the operator; suppressing its *effect* on control flow is deliberate, discarding its *content* is a debuggability gap. **Un-triaged** |
| **`ExampleWithReleaseWhen`'s dead channel wiring** | Triaged, not fixed (findings §6 item 4). Not a merge blocker; should not survive the next `routing` increment |
| **~120 lines of triplicated dialect logic** | Triaged (findings §6 item 1). Revisit triggers recorded: a fourth dialect, a third bug needing three fixes, or further shared *semantics* in `AddMember` |
| **`ErrMissingMsgID` is bare where `ValidateMaxMembers` is `Permanent`** | Same validate-before-I/O site, opposite classification. Flagged in §4, not resolved |
| **8 stale `file:line` citations in Spec 016 §2.1** | Measured at Task 9b, still not fixed. Expressions correct; coordinates rotted |
| **B6 mutants ran per-implementation, not 10×3** | Unchanged. ~16 more Docker runs; judged low-yield but **not** done |
| **A general `AST checker → permanent gate`** | Still unwritten. Task 11 added a **third** root AST gate half |
| **`WithPollMaxBatch`'s safe-arm row is magnitude-insensitive** | Pre-existing, verified not a regression |

## 9. Gotchas — these will bite

- **A mutant that does not COMPILE reports as a KILL; one that fails to APPLY reports as a SURVIVAL.** Require
  the mutation to **apply** (needle present, file hash changed) **and** `go build ./...` to pass **before**
  recording either outcome. Rename identifiers **file-globally**. **Check the mutant's arithmetic against the
  fixture's real magnitudes** — a `LIMIT 5` cannot truncate 4 rows.
  🔴 **NEW:** a `%v`→`%w` mutant "passes" as a kill only because `fmt.Sprintf` rejects `%w` at **vet** time. The
  mutant that matters is the one that *genuinely re-wraps* — the shape a reader "fixing it back" would write.
- **Run the docs-link gate LAST, after the final edit.** A pass taken before the last edit proves nothing — that
  mistake shipped a broken link in this very session, in the coordinator's own edit.
- **Verify the COMMIT, not the worktree** — `git diff HEAD --stat` must be empty. **When agents are running,
  never `git add -A`**: it will sweep in another agent's half-finished edits.
- **Parallel agents are safe only across a real module seam.** Two agents in the same Go module both run
  `go test ./...`, and a mid-edit tree makes one debug the other's file. Leaf-vs-root worked because the dialects
  import neither `adapter/memory` nor `routing`.
- **`git checkout -- <file>` is NOT a revert for an UNTRACKED file** — exits 0, does nothing.
- **There is a FOURTH dbtest runner.** `TestMariaDBConformance` sets `kit.Name = "mariadb"` while running the
  **mysql** dialect. **Derive the engine from the dialect's package path — `harness.DialectEngine`, exported at
  Task 11 Step 3 — never from `kit.Name`.**
- 🔴 **`GOARCH=386 GOOS=linux go test -run=NONE ./...`** — the form this file used to recommend — reports
  `exec format error` on darwin/arm64. **That is the binary failing to RUN, not a vet finding.** Use
  `GOARCH=386 GOOS=linux go vet ./...`, plus `go build -gcflags=all=-e ./...` for the full per-package list.
- **`adapter/database/sql` is NOT a module.** `find . -name go.mod` lists eight and it is not among them.
- **A leaf-module import resolves under `go.work` but NOT under `GOWORK=off`.**
- **`byArm` has NO key for an empty arm.** Writing `"deferred": 0` FAILS the assertion.
- **`apidiff` exits 0 even when it reports changes** — the OUTPUT is the signal. It is blind outside root.
- **`*-audit-round-*.md` and `*-derivation-findings.md` are IMMUTABLE.** A stale number there is *correct* — it
  records a past state. Delivered plans are not immutable.
- **`gopls` / the `LSP` tool is NOT available here.** Plans mandate it; substitute `go doc` + greps and say so.
- **`govulncheck` lives in `$(go env GOPATH)/bin`**, not on `PATH`.
- `GOTOOLCHAIN=go1.25.13`. `.superpowers/` is git-ignored. Never commit `.claude/settings.json`.

## 10. Pending approvals — nothing here was decided for you

1. **Re-running the two review commands** (§5) — required before merge, and only the user can do it.
2. **Adopting `github.com/gin-gonic/gin`.** Untouched. ADR 0024 remains reserved-but-unwritten, deliberately —
   writing it decides the dependency by side effect.
3. **Merge, push, tag, branch deletion.** None taken. **Nothing has left this machine.**
