# Session handover — msgin

> **READ FIRST.** Read `CLAUDE.md`, then this file, then whatever spec/plan/ADR the next increment names — and
> **trust those files and `git log` over this document.** Every figure here was measured when written;
> **re-derive before relying on one.**
>
> ### ✅ THE POST-029 SWEEP IS MERGED AND PUSHED. There is no work in flight.
> ### `origin/main` moved **`2b2dec1` → `b2649e3`** on 2026-08-28. Plans 030, 031, 032 are all on `main`.
> ### 🔴 Nothing is tagged, and **nothing should be** — msgin is unreleased with zero consumers.
> ### **The next increment starts from a FRESH BRANCH off `main`.**
>
> | | State |
> |---|---|
> | Branch | **`main`**, clean, in sync with `origin/main`. Verify with `git status -sb` |
> | `origin/main` | **`b2649e3`** — verify with `git ls-remote origin main`, **never** `git rev-parse origin/…` on this machine |
> | Merged branch | **`chore/backlog-sweep-post-029` still exists LOCALLY, merged but not deleted** — see §7 |
> | Working tree | **clean** |
> | Gates at merge | ✅ **8 CI steps × 8 modules, 8/8 on all 8**; `govulncheck` clean; `GOARCH=386` vet clean; all four Docker runners incl. MariaDB; both docs-link arms |
> | Reviews | ✅ `/code-review` **twice** (15 findings, then 3) — all 18 fixed. `/security-review` **twice** — **0 findings both times** |
> | Tags | **zero, as always.** Do NOT propose tagging |

## 1. What landed

`chore/backlog-sweep-post-029` merged with **`--no-ff`**, matching the repo's convention (23 prior merge commits,
zero fast-forwards). The merged tree was verified **byte-identical** to the gated branch tip before the push.

| Plan | What it delivered |
|---|---|
| **030** | The post-029 maintenance pass that scoped the other two |
| **031** | [Spec 017](specs/017-group-member-bounds.md) / [ADR 0033](adrs/0033-group-member-bounds.md) **D-AC…D-AX** — a message group's member count is bounded at the **store**, not at the release decision |
| **032** | [Spec 018](specs/018-byte-cap-ceilings.md) / [ADR 0034](adrs/0034-byte-cap-ceilings.md) — closed Spec 016's deferred byte-ceiling class |

**Plan 031 in one paragraph.** Both first-party stores now cap members per correlation group. `sql` enforces the
bound **inside the dialect's transaction**, counting live **and** claimed rows, so the durable member table is
bounded across a claim boundary. An over-cap rejection is classified by whether anything will actually drain the
group — on `sql` by lease **expiry**, not merely by `locked_by` being stamped — and the store returns the live
snapshot alongside the error so a group that is full but complete still releases instead of deadlocking.

## 2. 🔴 THE LESSON THIS BRANCH EXISTS TO TEACH — carry it into the next increment

**Eleven ratified or documented instructions were defeated at execution time**, plus one recommended fix that
would have panicked. **Every one was found by RUNNING something. None was findable by reading. All had cleared
multi-round adversarial design audits.** Treat every documented claim — a plan step, an ADR decision, a review
finding — as a **hypothesis to run**, and treat a non-reproducing finding as a **valid, valuable result**.

Concrete instances from the final session, all in `docs/plans/031-review-findings.md`:

| Claim | What was true |
|---|---|
| *"`adapter/memory` is a twin of the three dialects"* | Not a twin — `memory` has no lease TTL **by construction** |
| *"the operator gets a raw driver error"* | `classifyQueryErr` **replaces** rather than wraps; they always got the typed one |
| *"the snapshot is always empty"* | True only at the instant `ClaimGroup` returns |
| *"Fix: `reflect.Indirect(...)`"* | **Would panic** on the exact shape the SPI godoc prescribes |
| A `file:line` for a phrase | `git log -S` proved it had **never lived in that file** |

**And two ratified decisions were superseded by findings that arrived after them** — D-AV part 3 by R-1, D-AS's
mechanism by CR2-1. Both supersessions are recorded **with the rule that survived**, because concluding
otherwise would have reopened a delivery blocker. If you touch `ValidateMaxMembers`, read D-AV's supersession box
first: only *one* of R-15's two fail-open routes became structurally impossible.

## 3. Where the artifacts are

- `CLAUDE.md` (binding). Its counts are **command-derived** — the command is the authority, never the number.
  Its stale `IsPermanent` coordinate and anchor-link census were fixed by **deleting the digit**, not refreshing
  it; keep doing that.
- **Plan 031:** [Spec 017](specs/017-group-member-bounds.md) · [ADR 0033](adrs/0033-group-member-bounds.md)
  (ACCEPTED, D-AC…D-AX) · [Plan 031](plans/031-group-member-bounds.md) (DELIVERED) ·
  [`031-review-findings.md`](plans/031-review-findings.md) — the disposition list, **all 18 closed**. Its LIVE
  STATUS block is the current stratum; everything below it is annotated history.
- **Amended twice by this work:** [Spec 016](specs/016-sizing-option-bounds.md). **The class gate's arm rows have
  a FOURTH artifact — that file — and it is not in the obvious bundle.** It was missed twice.
- `*-audit-round-*.md` and `*-derivation-findings.md` are **IMMUTABLE**. A stale number there is *correct*.

## 4. Design decisions that will confuse a fresh reader

| ADR | Substance |
|---|---|
| **D-AU** | `AddMember` takes `leaseTTL`; classify on lease **expiry**. **`sql` only — `memory` is correct as shipped** |
| **D-AV** | `UnboundedGroupMembers = -1`; reject anything outside `{-1} ∪ [1, 1<<20]`. **Part 3 superseded by R-1** |
| **D-AW** | Release **strategy** failure ⇒ `errors.Join`; the `IsPermanent` escalation **is intended and asserted** |
| **D-AX** | Release **execution** failure ⇒ a fresh **transient** `ErrOverflowDropped`, interpolating the cause with `%v` |

> 🔴 **D-AW AND D-AX SIT TEN LINES APART AND LOOK CONTRADICTORY. THEY ARE NOT.** A failing release **strategy**
> is a predicate about *this* group and fails identically on every redelivery, so escalating to terminal is
> right. A failing **aggregate/`Send`** is about the *other, claimed* members' payloads — the refused member is
> unrelated collateral and must stay redeliverable. **Do not "fix" D-AX's `%v` back to `%w`**; severing the chain
> is the decision, and a mutant guards it.

## 5. Both review commands are USER-INVOCABLE ONLY

The `Skill` tool refuses `/code-review` and `/security-review` with `disable-model-invocation`. **The model
cannot run them and must never claim the gate passed when it did not.** Adversarial reviewer **subagents** are
SDD's prescribed substitute and earned their keep repeatedly — but they are **not** equivalent for the
whole-branch gate. Ask the user.

**Both ran clean before this merge.** `/security-review`'s second pass specifically traced the SQL identifier
interpolation (bound parameters throughout; `ValidateIdent`'s allow-list untouched), the attacker-influenceable
correlation key now in five new error strings (**all `%q`, not `%s`**), and whether D-AX's `%v` could leak a
redacted class (it cannot — `ErrPayloadDecode` is minted on ingress, before `Handle`).

## 6. Open, unscheduled — inherited by whoever picks this up

| Item | State |
|---|---|
| **`decodeErr`'s content is discarded** | A corrupt stored header never reaches the operator. Suppressing its *effect* on control flow is deliberate; discarding its *content* is a debuggability gap. **Un-triaged** (findings §5b) |
| **`ExampleWithReleaseWhen`'s dead channel wiring** | Triaged, not fixed. Should not survive the next `routing` increment |
| **~120 lines of triplicated dialect logic** | Triaged. Revisit triggers recorded: a fourth dialect, a third bug needing three fixes, or further shared *semantics* |
| **A `MaxMembers()` SPI method** | Would let `NewAggregator` reject a store-cap/`WithCompletionSize` pair that silently deadlocks. Seam described in Spec 017 §3.5; **deliberately not built** — real API growth for a misconfiguration |
| **`ErrMissingMsgID` bare vs `ValidateMaxMembers` `Permanent`** | Same validate-before-I/O site, opposite classification. Flagged, unresolved |
| **8 stale `file:line` citations in Spec 016 §2.1** | Expressions correct; coordinates rotted |
| **A general `AST checker → permanent gate`** | Still unwritten. There are now **three** root AST gate halves |
| **`github.com/gin-gonic/gin`** | ADR 0024 reserved but deliberately unwritten — writing it decides the dependency by side effect. **Needs the user** |

## 7. The one loose end

**`chore/backlog-sweep-post-029` still exists locally**, merged but not deleted. CLAUDE.md says to remove a
branch once merged, but **branch deletion needs its own explicit approval** and it was not given. It was **never
pushed**, so there is no remote branch to clean up:

```bash
git branch -d chore/backlog-sweep-post-029   # ask first
```

## 8. Gotchas — these will bite

- **A mutant that does not COMPILE reports as a KILL; one that fails to APPLY reports as a SURVIVAL.** Require
  the mutation to **apply** (needle present, file hash changed) **and** `go build ./...` to pass **before**
  recording either outcome. **Check the mutant's arithmetic against the fixture's real magnitudes** — a `LIMIT 5`
  cannot truncate 4 rows, and a `LIMIT 3` cannot truncate a 3-member group.
  🔴 **A third mechanism:** a `%v`→`%w` mutant "passes" as a kill only because `fmt.Sprintf` rejects `%w` at
  **vet** time. The mutant that matters is the one a reader "fixing it back" would actually write.
- **Run any gate LAST, after the final edit.** A clean run before the last edit is truthful about the tree it saw
  and worthless about the tree that ships — that mistake shipped a broken link in the final session.
- **Verify the COMMIT, not the worktree** — `git diff HEAD --stat` must be empty. **Never `git add -A` while a
  subagent is running**: its half-finished edits are already in the worktree.
- **Parallelize subagents only across a MODULE seam.** Every agent runs `go test ./...`, which compiles the whole
  module, so two agents in one module make each debug the other's file.
- **`git checkout -- <file>` is NOT a revert for an UNTRACKED file** — exits 0, does nothing.
- **`git merge -F -` does not read stdin** (`could not read file '-'`). Write the message to a file. A failed
  merge after a successful `git checkout main` looks alarmingly like a half-applied merge; it is not — check
  `git log --merges` and the SHA.
- **There is a FOURTH dbtest runner.** `TestMariaDBConformance` sets `kit.Name = "mariadb"` while running the
  **mysql** dialect. **Derive the engine from the dialect's package path — `harness.DialectEngine`** — never from
  `kit.Name`.
- 🔴 **`GOARCH=386 GOOS=linux go test -run=NONE ./...` reports `exec format error` on darwin/arm64.** That is the
  binary failing to **run**, not a vet finding. Use `GOARCH=386 GOOS=linux go vet ./...`, plus
  `go build -gcflags=all=-e ./...` for the full per-package list.
- **`adapter/database/sql` is NOT a module.** `find . -name go.mod` lists eight and it is not among them.
- **`harness` HAS had a real test file since Plan 031** — it is no longer the documented false pass. Still
  `go vet` it.
- **`byArm` has NO key for an empty arm.** Writing `"deferred": 0` FAILS the assertion.
- **`apidiff` exits 0 even when it reports changes** — the OUTPUT is the signal. It is blind outside root.
- **`gopls` / the `LSP` tool is NOT available here.** Plans mandate it; substitute `go doc` + greps and say so.
- **`govulncheck` lives in `$(go env GOPATH)/bin`**, not on `PATH`.
- `GOTOOLCHAIN=go1.25.13`. `.superpowers/` is git-ignored. Never commit `.claude/settings.json`.

## 9. Pending approvals — nothing here was decided for you

1. **Deleting the merged local branch** (§7).
2. **Adopting `github.com/gin-gonic/gin`** — ADR 0024 reserved but deliberately unwritten.
3. **Tagging.** Not proposed and should not be: msgin is unreleased with no consumers, so breaking API changes
   are still free. See `docs/RELEASE.md` before any tag — this is a multi-module monorepo with module-path-
   prefixed tags.
