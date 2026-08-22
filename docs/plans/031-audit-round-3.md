# Plan 031 — adversarial design audit, round 3 (2026-08-22)

Independent Opus subagent, handed the **complete Plan 031 revision-3 bundle together** — [Spec
017](../specs/017-group-member-bounds.md) + [ADR 0033](../adrs/0033-group-member-bounds.md) +
[Plan 031](031-group-member-bounds.md) — **before any implementation code existed**, per
[`CLAUDE.md`](../../CLAUDE.md)'s design-time gate. All three artifacts declare themselves **revision 3,
post-audit-round-2**, and carry the 🔴 *"decided WITHOUT USER RATIFICATION"* banner; that banner is not itself a
finding, but every decision it covers (**D-AC** … **D-AS**) was treated as open.

Round 3 has the same **two jobs** round 2 had, and they remain separate: (1) **verify the 14 round-2 findings
actually landed** in the revision — not that the revision *mentions* them — and (2) **attack the revised bundle
afresh**, including the two new decisions (**D-AR**, **D-AS**) the revision added. Both are recorded below; the
fix-verification table comes first because it is what distinguishes a revision from a rewrite.

**Traceability.** Audits: [Spec 017](../specs/017-group-member-bounds.md),
[ADR 0033](../adrs/0033-group-member-bounds.md), [Plan 031](031-group-member-bounds.md). Predecessor rounds:
[`031-audit-round-1.md`](031-audit-round-1.md) and [`031-audit-round-2.md`](031-audit-round-2.md) — **both
immutable**; nothing below edits either. Origin: [`docs/HANDOVER.md`](../HANDOVER.md) §6 backlog item **7**.
Predecessors whose ratified decisions the bundle reuses: [Spec 016](../specs/016-sizing-option-bounds.md),
[ADR 0032](../adrs/0032-sizing-option-bounds.md), [Plan 029](029-sizing-option-bounds.md),
[ADR 0031](../adrs/0031-nil-option-elements.md) **D-R**. Colliding concurrent work:
[Plan 030](030-post-029-maintenance.md) (landed), [Spec 018](../specs/018-byte-cap-ceilings.md) /
[ADR 0034](../adrs/0034-byte-cap-ceilings.md) / [Plan 032](032-byte-cap-ceilings.md) (a **sibling increment,
CLEARED and landing first — explicitly out of scope for this round**).

This record is **evidence-primary** — it is the audit artifact, not a user-facing summary. Every structural claim in
the revised bundle was re-derived on this tree with `GOTOOLCHAIN=go1.25.13`, darwin/arm64, at **`6865886`** (current
`main`). Commands and their output are pasted below. No file in the repository was modified while auditing.

> **⚠️ IMMUTABLE EXECUTION RECORD.** This file records what round 3 found, at the time it found it. Do not edit it
> to reflect later decisions, later measurements, or the revised bundle. Corrections belong in a later round's
> record or in the artifacts themselves. The coordinator's dispositions for these findings live in **Spec 017 /
> ADR 0033 / Plan 031 revision 4**, each of which cites this file.

> **📋 PROVENANCE — read this before treating any sentence below as the auditor's own words.**
>
> **Verbatim, from the round-3 report:** the **verdict framing sentence**, the **score line**, and **every verdict
> cell of the 14-row fix-verification table** in Part 1. The verdict column is the auditor's, row by row, and is
> not a reconstruction.
>
> **Relayed and then re-derived:** every **NEW-finding** below — its claim-under-attack, its coordinates and its
> required fix — and the **evidence cells** of Part 1's table. **Every citation in this file was independently
> re-run against the tree at `6865886` by the recorder before it was written down**, and the commands are pasted
> inline. Where the re-derivation *strengthened* or *corrected* the relayed form, that is marked **`RE-DERIVED`**
> in situ rather than silently folded in — the two such marks are on **NEW-7** (the count source is forced, not a
> choice) and **NEW-10** (the four sites do not contain the string `warnInvalidFallback`; they name the signal in
> prose, so an identifier grep cannot see them).
>
> **An earlier draft of this record reconstructed the verdict column by name and got THREE ROWS WRONG** — N-5 and
> N-7 were scored flawed and are **LANDED**; N-13 was scored clean and is **LANDED-BUT-FLAWED**. The verdict cells
> below are now the auditor's. **That correction is noted, not hidden, because it is the same failure mode this
> project's `43 ≠ 43` rule exists to catch: a partition inferred from a total rather than read off the evidence.**
>
> **Not reconciled, deliberately:** the auditor's **summary line says 8/6** and the auditor's **table tallies
> 9/5**. Both total 14. See *"The score line versus the table"* at the end of Part 1. **Reconcile by name, never
> by count.**

---

## Verdict

**NOT SAFE TO IMPLEMENT.** 1 BLOCKER, 6 MAJORs, 3 MINORs. The auditor's score line, verbatim:

> **Score: 8 clean LANDED, 6 LANDED-BUT-FLAWED, 0 NOT LANDED, 0 REGRESSED. Every flaw is new ground; the revision
> engaged all fourteen.**

> ⚠️ **That line disagrees with the auditor's own table, which tallies 9/5. Both total 14, and neither figure is
> altered here** — see *"The score line versus the table"* at the end of Part 1. **This is the second consecutive
> round on this increment where the auditor's table and summary differ by exactly one row in the middle bucket.**

**The framing matters more than the count, and it is the lesson of this round.** Verbatim:

> **Revision 3 generalized round 2's two structural fixes correctly, but stopped the fix at the boundary of each
> finding's own wording.**

Round 2 returned a BLOCKER because revision 2 fixed the **named instance** and left the **class** open. Revision 3
did not repeat that: N-1's *"a cross-module edit is a red commit"* was promoted into Global constraint 8 as a
**compiles-against** rule, and N-5's `LIMIT` defect was promoted into **D-AS**, a general declaration-form decision
about a shared helper with divergent callers. **Both generalizations are correct and both are the right shape.**

What revision 3 did instead is narrower and more mechanical: **each fix was applied to exactly the words the
finding quoted, and to nothing adjacent.** That is the whole content of the five LANDED-BUT-FLAWED rows:

- **N-1's fix added a six-module gate.** The `dbtest` row of that gate specifies `go build ./...` — and **every Go
  file in `dbtest` is a `_test.go` file**, so `go build` compiles nothing and exits 0 whatever breaks (**NEW-2**).
  That is N-1's own defect — *the task's own gate cannot see the break* — reproduced **inside N-1's fix**, by a
  mechanism the plan reasons about correctly **one row above** (`harness` has no test files, so `go test` there is
  a false pass). The same fix also threads a cap from a `TestKit` field that does not exist (**NEW-3**).
- **N-4's fix mandated the declaration *form*** (a named `const`). It did not mandate the declaration *file*, and
  AC-3.3's parse set is an **asserted** three-file list, so an implementer who puts the constant one file over
  fires the not-found guard (**NEW-4**). One attribute across from the finding's own wording.
- **N-8's fix pinned the `sql` render in AC-2c.** The render it pins names `sql.GroupStore.Add` — a site the error
  is not minted at and cannot know about (**NEW-5**) — and the finding's disposition never reached the ADR at all
  (**NEW-8**).
- **N-11's fix added a correction box naming the right WARN.** Four normative statements elsewhere — **including
  D-AM's own decision table twenty lines below its own correction box** — still name the WARN N-11 proved never
  fires (**NEW-10**).
- **N-13's fix corrected its four named coordinates** and revision 3's own prose then introduced **three more**
  (**NEW-9**). Third consecutive round for that class.

**The BLOCKER is not any of those.** It is **NEW-1**: revision 3 read N-1's rule — *a gate reading a declaration
must ship in that declaration's commit* — and applied it to Task 3, which does not need it, prescribing a **split**
that is strictly worse than the reorder in four independent ways. **N-1's rule is about a PRE-EXISTING gate; Task
3's gate does not exist yet, so the dependency runs the other way.** Pattern-matching a rule's *shape* instead of
its *mechanism* is how a correct generalization produces an incorrect instance.

**Two findings are NEW GROUND rather than a fix that stopped short**, and the auditor's summary says so — *"every
flaw is new ground"*. They attach to findings that landed **clean**, and conflating them with the flawed rows is
the error an earlier draft of this record made:

- **NEW-6** is a *separate* finding, not a flaw in **N-7**'s landing. N-7 asked for the downgrade-only rule to be
  promoted into the SPI, and revision 3 promoted it — completely, in all three artifacts, alongside all six exits,
  their cases and their mutants. **The rule it promoted is over-broad**, which is a defect in the *rule*, not in
  the *promotion*. N-7 scores **LANDED**.
- **NEW-7** is a design flaw produced by **composing** D-AF with D-AS. **N-5's fix is right**: `limit = 0` on
  `ClaimGroup`/`ExpiredGroups` is the correct value, because truncating a claimed set releases an incomplete
  aggregate. It is D-AF's `sql`-counts-live-only rule — which N-5 had no reason to look at — that leaves the
  durable table unbounded. N-5 scores **LANDED**.

---

## Part 1 — fix verification: did round 2's 14 findings land?

**The verdict column below is the auditor's, verbatim, row by row.** The evidence cells were relayed and then
re-run against the tree at `6865886` by the recorder. **The column tallies 9 LANDED / 5 LANDED-BUT-FLAWED, which
is not what the summary line says** — that discrepancy is recorded, unreconciled, after the table.

| # | Sev | Verdict | Evidence |
|---|---|---|---|
| **N-1** | BLOCKER | **LANDED-BUT-FLAWED** | Task 7 Step 1's harness update folded into the Tasks 5+6 commit (Task 5 **Step 6b**); Global constraint 8 restated as a **compiles-against** rule with the `harness`/`dbtest` grep pasted; Task 6 Step 5's gate widened from four modules to six. **The generalization is correct.** Three flaws inside it: the `dbtest` arm's command cannot see a break (**NEW-2**), Step 6b's cap-threading mechanism has no referent (**NEW-3**), and the rule was over-applied to Task 3 (**NEW-1**, BLOCKER) |
| **N-2** | MAJOR | LANDED | *"reachable through `WithSharedTransaction`"* deleted from Spec §3.6.2 and D-AP; both now carry the four-line compiler proof (`groupstore.go:211`, `:40-42`, `:271`, `options.go:201`). AC-4b drives `kit.Group.AddMember(ctx, tx, …)`; the option godoc states the **contrapositive**; §7.1 2b and ADR Consequences both qualified. Spot-checked: all four coordinates reproduce |
| **N-3** | MAJOR | LANDED | New Spec **§1.2.1**, per store, with the `RecoverInterval()` pair (`adapter/memory/groupstore.go:220` → `0`; `adapter/database/sql/groupstore.go:348` → `s.leaseTTL`), `reapInterval` quoted, and the `beforeSet` guard at `postgres/groupdialect.go:275-282`. D-AM's premise restated as *"nothing drains an UNLEASED group without an expiry cutoff"*. §3.11's *"that remains opt-in"* corrected. The stranded-lease counter-example is carried in **three** places (§3.3.1, D-AM, §8 item 5) |
| **N-4** | MAJOR | **LANDED-BUT-FLAWED** | **D-AR** minted; §3.2's box, Task 1 Step 4's box and Task 5 Step 4 all require named constants; AC-3.3 parses **three** files; the cross-reference is reconciled onto the **default** in all three artifacts; D-AQ's evidence block now quotes `defaultMaxGroupMembers`. Flaw: the declaration's **file** is unspecified while the parse set is asserted (**NEW-4**) |
| **N-5** | MAJOR | **LANDED** | **D-AS** minted and carried three-way (Spec §3.6.3, ADR D-AS, Plan Task 6 Step 3): a private `limit int`, `0` = unlimited, **`AddMember` the only non-zero caller**; **B6-7** and Task 7 **Step 5b** supply the mutant that makes it a constraint rather than a comment. All **nine** call sites reproduce — `postgres:121`/`163`/`307`, `mysql:113`/`161`/`298`, `sqlite:131`/`177`/`314` — over helpers at `pgSelectMembers:383`, `mysqlSelectMembers:368`, `sqliteSelectMembers:385`, each taking a **string** `claimedWhere`, so a separate `limit int` is the right shape. **And the post-rollback-live-set precondition is correctly stated**: Spec `:963-966` and ADR `:699-701` both read *"whenever the live count was ≤ cap before the add"* and both exclude the AC-4b path. *(**NEW-7** attacks D-AF composed with this decision, not this decision — see the verdict.)* |
| **N-6** | MAJOR | LANDED | AC-1b pins **`msgin.NewMessage(payload, headers)`** by name with the `message.go:178-180` / `:198` proof, and makes `require.Empty(t, m.ID())` the case's **first** assertion. Folded into all three sites the finding named (AC-1b, AC-9 row 3c, Plan Task 2 Step 3) |
| **N-7** | MAJOR | **LANDED** | Spec **§3.3a.1** tabulates **all six exits** — seven rows, because exit 2 is split by condition (`!ok` vs `rerr != nil`); Plan Task 1 gains **B1-13b … B1-13e**; Spec AC-9 gains rows **12a-12d**; **each has a named case AND a killing mutant**; `routing/aggregator_test.go` is in Task 1's Files, closing the second half of the finding; the `claim == nil` divergence from `aggregator.go:438-439` is named in the godoc contract (Spec §4 item 7). *(**NEW-6** attacks the over-broad rule the fix promoted — a separate new finding, not a flaw in this landing.)* |
| **N-8** | MINOR | **LANDED-BUT-FLAWED** | Spec §3.4a's *"members retained at the moment of the check"* box; AC-2c pins both renders; Plan Global constraint 4's box forbids normalising `sql`. Two flaws: the pinned `sql` render names a site the dialect cannot know (**NEW-5**), and the whole disposition is **absent from the ADR** (**NEW-8**) |
| **N-9** | MINOR | LANDED | Task 5 Step 5 restructured as *"one CORRECTION, two additions"* with the replacement sentence given verbatim; Spec §4 item 6's first bullet is the correction; D-AP carries the box. The shipped text at `groupdialect.go:109-113` reproduces as quoted |
| **N-10** | MINOR | LANDED | The `WithInboxTable` row is **deleted** from D-AD's table; the claim is downgraded to *"PROVEN in `adapter/memory`, merely CONSISTENT WITH `sql`"* in both D-AD and Plan Task 5's name box. `grep -rn "^func WithInboxTable\|^func WithTable" adapter/database/sql/*.go` → one hit, `inbox_dedup.go:35` |
| **N-11** | MINOR | **LANDED-BUT-FLAWED** | Correction boxes added in D-AM (`:266-276`), Spec §3.3.1 (`:519-529`) and §3.11's table row: `invalidTarget` returns `fellBack = (DeadLetter != nil)` (`consumer.go:942`), the bare-config signal is `divertTerminal`'s nil-sink WARN (`:1049`) **plus `safeAck` (`:1073`)**, and `warnInvalidFallback` is marked once-per-consumer (`:968-973`). Flaw: **four** normative statements still name the old signal (**NEW-10**) |
| **N-12** | MINOR | LANDED | AC-7 rewritten to one **named clause** (MUST-report) with an executable four-line snippet and a killing mutant; the *"what this uniquely buys"* paragraph and the delete-or-fold escape are both stated. Plan Task 8 Step 3 carries the same code |
| **N-13** | MINOR | **LANDED-BUT-FLAWED** | All four **named** coordinates corrected, each with the old value struck in place: `consumer.go:860-869` (Spec §3.3.1, D-AM), `sqlite/groupdialect.go:62` (§7.1), `mysql/groupdialect.go:85-92` vs `:93-96` (§3.6.1, D-AP, §7.1), `groupstore.go:185-188`/`:189-199` (§1.2, D-AN). Re-derived: all four reproduce. Flaw: **revision 3's own prose introduced three MORE** (**NEW-9**) — `isIntOrInt64` at `:191-202`, `ErrExpiryChannelRequired` at `:360-362`, `groupstore_test.go:30-39`. **Third consecutive round for this class**, and the third round whose fix corrected the named instances only |
| **N-14** | MINOR | LANDED | **D-AL** now carries B-2's ordering rule in three numbered points, plus the **generalization** (*a cross-module edit is a red commit*) tied forward to D-AG. Plan Task 10 **Step 0** adds a whole-revision three-artifact reconciliation. `grep -n "same commit\|red suite\|exact set equality" docs/adrs/0033-*.md` now returns hits where round 2 got none |

**The 14 rows close.** Tallied **by name** off the auditor's verdict column above:

| Disposition | Findings | Count |
|---|---|---|
| **LANDED (clean)** | N-2, N-3, N-5, N-6, N-7, N-9, N-10, N-12, N-14 | **9** |
| **LANDED-BUT-FLAWED** | N-1, N-4, N-8, N-11, N-13 | **5** |
| **NOT LANDED** | — | **0** |
| **REGRESSED** | — | **0** |

**9 + 5 = 14; no row is missing, duplicated or unclassified.**

**Where the ten new findings attach.** Eight attach to a flawed row; **two are new ground attaching to rows that
landed clean**, which is what the auditor's summary means by *"every flaw is new ground"*:

| Row | Verdict | New findings |
|---|---|---|
| **N-1** | FLAWED | **NEW-1** (BLOCKER — the rule over-applied to Task 3), **NEW-2** (the `dbtest` gate arm), **NEW-3** (Step 6b's cap mechanism) |
| **N-4** | FLAWED | **NEW-4** (the declaration's file) |
| **N-8** | FLAWED | **NEW-5** (AC-2c's `sql` site string), **NEW-8** (absent from the ADR) |
| **N-11** | FLAWED | **NEW-10** (four statements still name the wrong signal) |
| **N-13** | FLAWED | **NEW-9** (three new off-by-ones in revision 3's own prose) |
| **N-7** | **LANDED** | **NEW-6** — the promoted rule is over-broad. **A defect in the RULE, not in the promotion**; the promotion is complete and three-way |
| **N-5** | **LANDED** | **NEW-7** — D-AF **composed with** D-AS. **D-AS's `limit = 0` is the correct value**; it is D-AF's live-only counted set, which N-5 had no reason to examine, that leaves the table unbounded |

**Nothing regressed and nothing was ignored.** The revision is responsive, its two generalizations are correct, and
it is nevertheless still not implementable.

### The score line versus the table — a one-row gap, left open (SECOND CONSECUTIVE ROUND)

**The verdict's score line says `8 clean LANDED, 6 LANDED-BUT-FLAWED`. The 14-row table above tallies `9 / 5`.**
Both total 14; no row is missing, duplicated or unclassified. One finding is counted LANDED-BUT-FLAWED by the
summary and LANDED by the table.

**Which figure governs: the TABLE.** Every table row carries its own evidence and is independently checkable; the
score line is a tally **derived** from those rows and carries none. **A derived count cannot overrule the evidenced
partition it was derived from.** The by-name tally — **9 LANDED / 5 LANDED-BUT-FLAWED** — is therefore the
operative one, and the score line is recorded as an **off-by-one in the auditor's summary**. Neither figure has
been altered here.

**Nothing downstream depends on the resolution.** All 14 dispositions and every NEW-finding attached to them are
unchanged either way, and revision 4 folds in all ten findings regardless.

> 🔴 **THIS IS THE SECOND CONSECUTIVE ROUND ON THIS INCREMENT WHERE THE AUDITOR'S TABLE AND SUMMARY DISAGREED BY
> EXACTLY ONE ROW IN THE MIDDLE BUCKET — record it as a PATTERN, not an incident.**
>
> | Round | Summary line | Table tally | Gap |
> |---|---|---|---|
> | 2 ([`031-audit-round-2.md`](031-audit-round-2.md)) | 12 / 8 / 1 (M-8) | 13 / 7 / 1 | one row, middle bucket |
> | **3 (this record)** | **8 / 6** | **9 / 5** | **one row, middle bucket** |
>
> Twice is a method defect, not a slip: a summary tally written *alongside* an evidence table rather than
> *generated from* it will drift, and both rounds drifted in the same direction (the summary over-counting the
> flawed bucket by one). **The remedy is on the audit brief, not on the artifacts: a round-4 report should
> generate its score line from its own table rather than state it separately.** This is the project's standing
> `43 ≠ 43` lesson — *reconcile by name, never by count* — reappearing in the audit apparatus itself.
>
> **And this record's own first draft made the sharper version of the same mistake:** it *reconstructed* the
> verdict column from the summary's totals and misplaced **three** rows (N-5, N-7 clean → wrongly flawed; N-13
> flawed → wrongly clean). **Inferring a partition from a total is exactly what the rule forbids**, and it is why
> the verdict column here is now the auditor's, cell by cell.

---

## Part 2 — findings

## Finding index

| # | Rank | One line |
|---|---|---|
| **NEW-1** | BLOCKER | Task 3's *"or split it"* alternative is **strictly worse** than the reorder in four ways, and the reasoning behind it misreads N-1: N-1's rule governs a **pre-existing** gate; Task 3's AST test **does not exist yet**, so the dependency runs the other way |
| **NEW-2** | MAJOR | The six-module gate's **`dbtest` arm is `go build ./...`** — and `dbtest` has **zero non-test Go files**, so it compiles nothing and exits 0 whatever breaks. N-1's own defect, inside N-1's fix |
| **NEW-3** | MAJOR | *"Threading the cap from the existing `TestKit`"* has **no referent**: `harness.TestKit` has no cap field, and `testkit.go` is in no task's Files list |
| **NEW-4** | MAJOR | AC-3.3's parse set is an **asserted** three-file list, but no step says **which file** `sql`'s constants are declared in — and the only file Task 5 Step 4 names is `helpers.go`, where the not-found guard fires |
| **NEW-5** | MAJOR | AC-2c pins an executable `sql` render naming **`sql.GroupStore.Add`**, but the error is minted **inside the dialect**, which cannot know it was reached through the store — and AC-4b deliberately drives the dialect directly, where that render names a store never involved |
| **NEW-6** | MAJOR | The downgrade-only rule was promoted into the SPI in its strong form — *"only … on positive evidence that the group drained"* — and **exits 3 and 5 of §3.3a.1's own table violate it**, replacing a permanent classification with an unmarked (hence transient) one **because the drain failed** |
| **NEW-7** | MAJOR | **A design flaw, not a fix that stopped short.** D-AF (`sql` counts live only) composed with D-AS (`ClaimGroup` at `limit = 0`) leaves the **durable member table unbounded**: each failed-release cycle admits up to `cap` more rows, forever. `memory` does not have this hole *because* D-AF has it count live + claimed |
| **NEW-8** | MINOR | N-8's disposition — the two stores' divergent counts — is **absent from ADR 0033 entirely**. Two-of-three again, on the round's own finding list |
| **NEW-9** | MINOR | Three **new** off-by-one citations, introduced or inherited by revision 3's own prose |
| **NEW-10** | MINOR | Four normative statements still name the WARN **N-11 proved never fires** — including D-AM's own decision table, twenty lines below its own correction box |

---

## BLOCKER NEW-1 — Task 3's "or split it" misreads N-1, and the split is strictly worse than the reorder in four ways

**The claim under attack.** Plan 031 Task 3's header box:

> 🔴 **THIS TASK NOW SITS AFTER TASK 5, OR PARSES TWO OF THREE FILES AND FAILS.** `sql`'s `defaultMaxGroupMembers`
> is written in Task 5 Step 4. Run Task 3 **after** Tasks 5+6 land, **or split it: the `memory` + `routing`
> assertions with Task 1 and the `sql` assertion with Task 5.**

The stated ground for the split is N-1's lesson, as the revision restated it in Global constraint 8: *a gate
reading a declaration must ship in that declaration's commit.*

**The reasoning is wrong, and the mechanism is what shows it.**

**N-1/B-2's rule is about a PRE-EXISTING gate.** `sizing_option_class_gate_test.go` **ships on `main`** and asserts
**exact set equality in both directions**:

```
$ sed -n '321,324p' sizing_option_class_gate_test.go     # assert.Equal(t, want, found, …) — set equality
$ git log --oneline -1 -- sizing_option_class_gate_test.go
d2c69fe  (Plan 030 Task 2 — the gate is on main, rewritten, and green)
```

Because that assertion already runs, **the option's mere existence on disk makes root red before one line of new
test code is written.** The gate is the fixed point; the declaration moves toward it. Hence the key must ride in
the option's own commit — there is no ordering that avoids a red commit otherwise.

**Task 3's AST invariant test DOES NOT EXIST YET.** Adding `const defaultMaxGroupMembers` to
`adapter/memory/groupstore.go` makes **nothing** red:

```
$ ls group_member_bound_invariant_test.go
ls: group_member_bound_invariant_test.go: No such file or directory
$ grep -rn "defaultMaxGroupMembers" --include='*.go' .
$   (no output — the constant does not exist either)
```

**So the dependency runs the other way.** The *test* depends on the *declarations*; the declarations depend on
nothing. That is a **reorder constraint** — "the test ships no earlier than the last declaration" — and not a
co-commit constraint. The two rules have the same *shape* (a test and a declaration must be sequenced) and
opposite *mechanisms* (which one is already red).

**The split is also strictly worse than the reorder, in four independent ways.**

| # | What the split breaks | Why |
|---|---|---|
| 1 | **B3-3's mutant becomes unprovable in the first half** | B3-3 is *"the parse set is complete — drop `adapter/database/sql/groupstore.go` from the set ⇒ **must fail**, not shrink."* In the Task-1 half the `sql` constant **legitimately does not exist**, so the asserted file list must be written as **two** files and then rewritten to **three** in the Task-5 half. A file list that is edited to match what happens to exist is exactly the discovery-not-assertion defect B3-3 forbids |
| 2 | **Step 4's mandatory non-vacuity probe runs twice** | Step 4 is *"rename one constant locally, confirm the not-found guard fires, revert"* — **mandatory**, not optional. Two halves means two probes, two evidence blocks, and two chances for the second one to be skipped as "already done" |
| 3 | **Step 2's RED becomes impossible** | Step 2 writes the test *"against the **pre-Task-1** tree with a deliberately wrong expectation, to prove it reads real values off the AST rather than passing vacuously."* In the Task-1 half there is **no constant**, so what fires is the **not-found guard** — a different RED, proving the guard rather than the value read. The two probes collapse into one and the weaker one wins |

> **A separate defect surfaced while checking consequence 3, and it is NOT a consequence of the split.** Step 2's
> *"against the **pre-Task-1** tree"* is already wrong in revision 3 **as written**, under either permitted order:
> pre-Task-1 there is no `defaultMaxGroupMembers` in `adapter/memory` either, so the not-found guard is what fires
> whichever way the task is scheduled. The phrase is a leftover from when Task 3 sat at position 3 and parsed two
> files. **Step 2 and Step 4 are meant to be two distinct probes — a value RED and a guard RED — and revision 3
> collapses them.** Fixing the order does not fix this; Step 2 must be rewritten to run against the tree where all
> three constants exist.
| 4 | **Three references dangle** | Task 10 Step 5 (*"Confirm **Task 3's** test is present, non-vacuous and green"*), the Sizing table's single **Task 3** row, and Task 3 Step 6's single commit message. A task that is two half-tasks has none of those |

**Why this is the BLOCKER and not a MINOR sequencing nit.** The plan currently states **two** permitted orders and
recommends neither. An implementer executing it will pick one, and the one that is spelled out in more detail —
with an explicit per-half assignment — is the one that is wrong. A plan that offers a defective option as a
co-equal alternative will have that option taken.

**Required fix.**

1. **Move Task 3 to run after Tasks 5+6** — either renumber, or **state the execution order explicitly** in the
   Sizing table and in Task 3's own header, so a top-to-bottom executor cannot miss it.
2. **Delete the *"or split it"* alternative entirely.** The plan must state **one** order.
3. **Fix the three dangling references** — Task 10 Step 5, the Sizing table row, and the commit message — so each
   names Task 3's real execution position.
4. **Record the reversal and its reasoning in ADR 0033**, so the split is not re-proposed by a future reader who
   sees N-1's rule and pattern-matches on its shape.

---

## MAJOR NEW-2 — the six-module gate's `dbtest` arm is a command that cannot fail

**The claim under attack.** Plan 031 Task 6 **Step 5**, the gate N-1's fix widened from four modules to six:

| Module | Command | Why |
|---|---|---|
| **`harness`** | **`GOWORK=off go vet ./...`** | 🔴 **It has NO `_test.go` files, so `go test` is a FALSE PASS** (CLAUDE.md) |
| **`dbtest`** | **`GOWORK=off go build ./...`** (full run in Task 7) | requires `harness` with a local `replace` |

**The row above it is right. The row itself is the mirror-image false pass.**

```
$ cd adapter/database/sql/dbtest && GOWORK=off go list -f '{{.GoFiles}} TEST={{.TestGoFiles}} XTEST={{.XTestGoFiles}}' ./...
[] TEST=[] XTEST=[conformance_mysql_test.go conformance_pg_test.go conformance_sqlite_test.go testutils_test.go]

$ ls adapter/database/sql/dbtest/
conformance_mysql_test.go  conformance_pg_test.go  conformance_sqlite_test.go  go.mod  go.sum  testutils_test.go

$ cd adapter/database/sql/dbtest && GOWORK=off go build ./...
$ echo $?
0
```

**`GoFiles` is empty.** Every Go file in `dbtest` is an **external test file**. `go build ./...` therefore compiles
**zero** Go files, produces no output, and exits **0** — whether or not `harness` still compiles, whether or not
`kit.Group.AddMember`'s signature matches, whether or not the module is coherent at all.

`go vet` does read them:

```
$ cd adapter/database/sql/dbtest && GOWORK=off go vet ./...
$ echo $?
0
```

**Why this matters more than a wrong command.** `dbtest` is one of the exact **two** modules N-1 existed to protect
— the modules that go red on the `AddMember` signature alone and that revision 2's four-module gate could not see.
Revision 3 put them both in the gate and then gave one of them a command that **cannot see them**. The plan reasons
about this failure mode correctly **one row above**, for `harness`, in the same table.

**A false pass in one direction was fixed and the same false pass in the other direction was introduced.** Both
directions belong on the record, because a future module is going to be one or the other:

| Module shape | `go test` | `go build` | `go vet` |
|---|---|---|---|
| non-test files, **no** `_test.go` (`harness`) | **FALSE PASS** — no tests to run | sees the break | sees the break |
| **only** `_test.go` files (`dbtest`) | sees the break (needs Docker) | **FALSE PASS** — nothing to compile | sees the break |

**Required fix.** Change the `dbtest` row's command to **`GOWORK=off go vet ./...`**, and **state the reason on the
row** — *"every Go file here is a `_test.go` file, so `go build` compiles nothing and exits 0"* — so the record
carries both false-pass directions rather than only the one CLAUDE.md already names.

---

## MAJOR NEW-3 — "threading the cap from the existing `TestKit`" has no referent

**The claim under attack.** Plan 031 Task 5 **Step 6b**, the step N-1's fix created:

> **Step 6b (THE HARNESS CALL SITE — audit N-1, the BLOCKER).** Update `harness/groupstore.go:345` to the new
> signature, **threading the cap from the existing `TestKit`**. **Mechanical only** … 🔴 **Do NOT add an exported
> function with an `int`/`int64` parameter to `harness`**.

**`TestKit` has no cap.**

```
$ grep -nE "^\s+[A-Z][A-Za-z]*\s+(int|int64)\b" adapter/database/sql/harness/testkit.go
$   (no output — TestKit has no int-typed field at all)

$ grep -niE "max|limit|\bcap\b" adapter/database/sql/harness/testkit.go
144:	// (an independent *sql.DB's connection limit and connection set), not
```

The one hit is a prose comment about connection pools. `TestKit`'s fields are `Name`, `SingleWriter`, `Lease`,
`Inbox`, `Group`, `GroupDDL`, `Quote`, `Placeholder`, `NowExpr`, `HeadersTextExpr` and their siblings — **no
`MaxGroupMembers`, no cap, no limit, no integer field of any kind.**

**And `testkit.go` is in no task's Files list**, so no task is authorized to add one:

```
$ grep -rn "testkit.go" docs/plans/031-group-member-bounds.md
175:   > $ grep -n "GroupDialect" adapter/database/sql/harness/testkit.go
681:   ... holds a `msginsql.GroupDialect` (`testkit.go:87`) and calls `AddMember` (`groupstore.go:345`)
```

Both hits are **evidence citations**, not Files entries. Task 5's Files list names
`adapter/database/sql/harness/groupstore.go` and nothing else in `harness`.

**So Step 6b describes an edit to a field that does not exist, in a file no task may open.** The implementer's
options are (a) add a `TestKit` field — an **exported struct field** addition to a leaf module's public surface,
unbudgeted and un-ADR'd; (b) add an exported parameter — **explicitly forbidden** by the box in the same step and
by AC-5's box; or (c) invent an unexported constant, which is correct but which no artifact authorizes.

**The forbidden option is correctly forbidden, and that is what makes the gap load-bearing.** AC-5's box:

```
$ grep -rnE "^func [A-Z][A-Za-z]*\(.*\b(int|int64)\b" adapter/database/sql/harness/*.go
$   (no output — still clean today)
```

Half 1 of the class gate walks the filesystem into leaf modules; half 2 (a root test) cannot import one. An
exported `int` parameter in `harness` is therefore an **unsatisfiable gate failure by design**. The box is right;
the step contradicts it by naming a mechanism that does not exist and leaving the implementer to improvise.

**Required fix.** Replace *"threading the cap from the existing `TestKit`"* with the actual mechanism: a
**`harness`-package unexported constant** serving **both** the `AddMember` call site at `groupstore.go:345` **and**
Task 7's conformance case. It touches neither `TestKit` nor an exported `int` parameter, so it satisfies AC-5's box
by construction and needs no new file in any Files list. Fix it in **Plan Task 5 Step 6b**, **Plan Task 7 Step 5**
and **Spec AC-5**.

*(If a `TestKit` field is chosen instead: `testkit.go` must join Task 5's Files list, and the ADR must record that
`harness` gains an exported field — a public-surface change to a leaf module, which is an architectural decision,
not an implementation detail.)*

---

## MAJOR NEW-4 — AC-3.3 asserts a three-file parse set, and no step says which file `sql`'s constants live in

**The claim under attack.** Spec 017 §6 **AC-3.3**, as repaired for N-4, parses **three** files and **asserts** the
list:

> - parses **three** files with `go/parser` — `routing/aggregator.go`, `adapter/memory/groupstore.go` **and
>   `adapter/database/sql/groupstore.go`** …
> - locates `const completionSizeCeiling` and **`const defaultMaxGroupMembers` in EACH store package**, by name,
>   **failing loudly if any of the three declarations is not found**

and **B3-3** forbids shrinking the set: *"drop `adapter/database/sql/groupstore.go` from the set ⇒ **must fail**,
not shrink to one assertion."*

**The step that writes those constants names no file — and the only file it names is the wrong one.** Plan Task 5
**Step 4**:

> **Step 4 (GREEN — the option).** Add `checkRange` to **`adapter/database/sql/helpers.go`** — a fifth,
> independent, unexported copy … Add **two NAMED package constants** — `const defaultMaxGroupMembers = 1 << 16`
> and `const maxGroupMembersCeiling = 1 << 20` … the config field initialised from the named default, and
> `WithMaxGroupMembers(n int)` with the `checkRange` call in `NewGroupStore`.

The step's own subject is `helpers.go`. `checkRange` goes there, and `checkRange`'s range arms are the immediate
neighbours of the two constants. **An implementer who declares the constants beside the `checkRange` copy — the
most natural reading of the step as written — puts them in `adapter/database/sql/helpers.go`, and AC-3.3's
not-found guard fires.**

```
$ ls adapter/database/sql/helpers.go adapter/database/sql/groupstore.go
adapter/database/sql/groupstore.go   adapter/database/sql/helpers.go        # both real files
$ grep -n "^const" adapter/database/sql/groupstore.go
22:const defaultGroupLeaseTTL = 5 * time.Minute
30:const defaultExpiredGroupsLimit = 100
```

`groupstore.go` already carries the package's other two defaults at `:22` and `:30`, so it is the correct home —
but that is an inference, and the AC is an assertion.

**This is N-4 one attribute over, and the artifacts say so themselves.** N-4's fix (**D-AR**) mandated the
declaration **form** — a named `const` rather than a bare literal — because *"the declaration form is load-bearing
for a mechanical gate."* The declaration's **location** is load-bearing for the same gate, by the same mechanism,
in the same test. D-AR reasons about form and is silent on file.

**The same gap exists on the `memory` side**, less dangerously because Task 1's Files list contains only one
plausible home: Task 1 Step 4's box says *"THE DEFAULT MUST BE A NAMED CONSTANT"* and never says
`adapter/memory/groupstore.go`.

**Required fix.** Name the file, and fold in the reason exactly as D-AR already does for the form — *"declared in
`<store>/groupstore.go`, the file AC-3.3 parses"* — in **Plan Task 1 Step 4**, **Plan Task 5 Step 4**, **Spec §4
item 2** and **ADR D-AR**.

---

## MAJOR NEW-5 — AC-2c pins a `sql` render naming a site the error is not minted at

**The claim under attack.** Spec 017 §6 **AC-2c**, made executable for N-8:

```
memory: msgin: permanent: msgin: message dropped by overflow policy: memory.GroupStore.Add: group "k" holds 4 members, limit 4
sql:    msgin: permanent: msgin: message dropped by overflow policy: sql.GroupStore.Add: group "k" holds 5 members, limit 4
```

**The `sql` error is minted inside the dialect, which cannot know how it was reached.** Plan Task 6 Step 4:

> **Step 4 (GREEN — the error).** Return `msgin.Permanent(fmt.Errorf(…ErrOverflowDropped…))` … Import `msgin` in
> **each dialect**.

The site string is a compile-time constant in `postgres/groupdialect.go`, `mysql/groupdialect.go` and
`sqlite/groupdialect.go`. `sql.GroupStore.Add` is one of **two** call paths into `AddMember`, and the dialect sees
neither.

**The second path is one this bundle deliberately exercises.** AC-4b, corrected for N-2, drives
`kit.Group.AddMember(ctx, tx, …)` — the dialect, **directly**, with no `GroupStore` in the picture at all. A render
naming `sql.GroupStore.Add` on that path names **a store that was never involved**, which is precisely the
false-attribution debuggability defect D-AE's wrap exists to prevent.

**It also defeats D-AE's own argument.** D-AE justifies the wrap because *"`msgin.ErrOverflowDropped` already has
four producer sites … a bare sentinel cannot tell an operator which cap fired."* One identical site string across
three dialect modules reintroduces exactly that: **an operator cannot tell which engine rejected.**

**And it contradicts the shipped convention, which the three dialects already follow.**

```
$ grep -rn '"msgin/sql/' adapter/database/sql/{postgres,mysql,sqlite}/groupdialect.go
postgres/groupdialect.go:67:  "msgin/sql/postgres: group ops require a *sql.DB or *sql.Tx Querier, got %T"
mysql/groupdialect.go:63:     "msgin/sql/mysql: group ops require a *sql.DB or *sql.Tx Querier, got %T"
sqlite/groupdialect.go:55:    "msgin/sql/sqlite: group ops require a *sql.DB Querier (dedicated BEGIN IMMEDIATE conn), got %T"
```

Every error a dialect mints today is prefixed `msgin/sql/<engine>:`. The proposed render is the only one that would
not be.

**Nothing in the bundle executes the render.** Task 5's fake-dialect cases (B5-5, B5-7) assert `IsPermanent` and
`errors.Is`, never the string — and a render assertion through the **fake** would be vacuous, since the fake mints
whatever the test hands it. **AC-2c's `sql` half currently has no owning task.**

**Required fix.**

1. **Decide the site string as `msgin/sql/postgres: AddMember`** and its `mysql` / `sqlite` siblings — matching the
   shipped convention and discriminating the engine.
2. **Correct AC-2c's `sql` render** to the decided string.
3. **Add the `%s` value to Plan Global constraint 4's shape spec**, which today fixes the format and leaves the
   substitution unstated.
4. **Assign the render assertion to Task 7's harness case**, against a real engine, where the string the dialect
   actually mints is observable.

---

## MAJOR NEW-6 — the downgrade-only rule was promoted into the SPI in a form two of its own six exits violate

> **N-7 LANDED CLEAN — this is a separate finding.** N-7 asked for the downgrade-only rule to be promoted into
> the SPI contract, and revision 3 promoted it: three-way, alongside all six exits, their named cases and their
> killing mutants. **The defect is in the RULE, not in the promotion.** Recording it against N-7's landing would
> credit revision 3 with a failure it did not commit — and would hide the actual lesson, which is that
> *promoting a rule into a contract does not make the rule true*.

**The claim under attack.** N-7's fix promoted the direction rule out of D-AN's prose and into
`MessageGroupStore.Add`'s **SPI contract**, where a third-party store author reads it (Spec §3.7, ADR **D-AH**'s MAY
clause):

> When the Aggregator acts on that snapshot it may only ever **DOWNGRADE** the implementation's classification —
> permanent to transient, **on positive evidence that the group drained**. It never upgrades a transient rejection
> to permanent. An implementation may therefore treat its own classification as the **conservative floor**: **a bug
> in the Aggregator's drain path costs a retry, never a message the implementation marked recoverable.**

**Two of the six exits §3.3a.1 itself tabulates violate it.** From the spec's own pseudocode:

```go
    claim, cerr := a.store.ClaimGroup(ctx, key)
    if cerr != nil {
        return cerr                                  // exit 3 — the STORE's error, unmarked
    }
    …
    if relErr := a.release(ctx, claim); relErr != nil {
        return relErr                                // exit 5 — the RELEASE's error, unmarked
    }
```

Both exits **discard the store's `Permanent`-marked overflow error** and return a different error that carries **no
marker** — and an unmarked error is **transient** by construction (`IsPermanent` matches `*permanentError` via
`errors.As`, `reliability.go:86-97`). So both replace a *permanent* classification with a *transient* one.

**And they do it on evidence of the opposite of drainage.** §3.3a.1's own commentary says so for exit 3 —
*"returning `cerr` **discards the overflow classification**"* — without following the sentence to its consequence.
The drain **failed**; the group is, if anything, *less* likely to drain than when the store classified it.

**The consequence is B-1, in a narrower case.** Under the zero-value `RetryPolicy` — the configuration the whole
D-AM argument is about — a transient error is an **unlogged, zero-delay Nack loop** (`consumer.go:860-869`,
`retryDelay` → 0). For the sub-case *"the release predicate fires but `ClaimGroup` (or the release) keeps
failing"*, every retry runs: `Add` → over cap → permanent + snapshot → release fires → claim/release fails →
**transient** → immediate redelivery. **That is the unlogged hot spin B-1 was about, reached through the very
branch §3.3a exists to serve.**

**Why the strong form cannot simply be weakened to "never upgrade".** The *"never upgrade"* half is true and is the
half a store author most needs. The false half is *"only … on positive evidence that the group drained"* plus its
corollary *"a bug in the drain path costs a retry, never a message the implementation marked recoverable"* — which
tells a store author their `Permanent` marker survives to the consumer on **every** path. It does not survive on
exits 3 and 5.

**Required fix.** Restate the rule truthfully in all three places it now appears — **Spec §3.7**, **ADR D-AH's MAY
clause**, and **Spec §3.3a.1's "direction rule that governs all six"**:

> The Aggregator either **downgrades on positive evidence of drainage** (exits 4 and 6) **or replaces the overflow
> error entirely with a distinct fault — a `ClaimGroup` error or a release error — which carries that fault's own
> classification** (exits 3 and 5). It never upgrades a transient rejection to permanent.

and say **explicitly** that a persistently failing claim/release path therefore **retries rather than terminating**.

**Also: delete D-AN's *"a bug in the drain path costs a dead-letter, not a production-down spin"***, or scope it to
exits 4 and 6. As written it asserts the exact property exits 3 and 5 do not have.

---

## MAJOR NEW-7 — `sql` counts live only and `ClaimGroup` claims everything, so the durable table is unbounded

**This is the one finding of this round that is a design flaw rather than a fix that stopped short.** It is not
visible in either component alone; it is produced by **composing D-AF with D-AS**.

> **N-5 LANDED CLEAN — D-AS is not the defect.** `limit = 0` on `ClaimGroup`/`ExpiredGroups` is the **correct**
> value: any other value truncates a claimed set, and a legitimately at-cap group would then release an
> **incomplete aggregate** — the silent data corruption Spec §5 rejects. **D-AF's live-only counted set is the
> defect**, and D-AS had no reason to examine it. Scoring N-5 flawed for this would push a future revision toward
> "fix it by bounding `ClaimGroup`", which is the one repair that must not be made.

**The two claims under attack.**

**D-AF:** *"`sql.GroupStore` bounds the **live** member set (`claimed_epoch IS NULL`), because for `sql` the claimed
members are retained by the **database**, not by the process."*

**D-AS:** *"`AddMember` is the only caller that passes a non-zero value (`maxMembers+1`); **`ClaimGroup` and
`ExpiredGroups` pass `0`** and keep their current behavior byte-for-byte."*

**The composition. `ClaimGroup` stamps EVERY live member, so after a claim the live count is zero.**

```go
adapter/database/sql/postgres/groupdialect.go — ClaimGroup, inside the transaction
	// Re-absorb a dead claim's members (H2): stamp the new epoch on every
	// member the lease fence proves is no longer actively claimed.
	tx.ExecContext(ctx, `UPDATE <members> SET claimed_epoch = $2
WHERE group_key = $1 AND (claimed_epoch IS NULL OR claimed_epoch < $2)`, groupKey, newEpoch)
```

**`AbandonGroup` puts them all back:**

```go
adapter/database/sql/postgres/groupdialect.go — AbandonGroup
	// Return the claimed members to live; the epoch stays bumped …
	tx.ExecContext(ctx, `UPDATE <members> SET claimed_epoch = NULL
WHERE group_key = $1 AND claimed_epoch = $2`, groupKey, epoch)
```

**And `SettleGroup` is the only statement in the dialect that ever DELETES a member row — on success only:**

```go
adapter/database/sql/postgres/groupdialect.go — SettleGroup
	// Delete only the claimed set; a member added during the lease
	// (claimed_epoch NULL) survives.
	tx.ExecContext(ctx, `DELETE FROM <members> WHERE group_key = $1 AND claimed_epoch = $2`, groupKey, epoch)
```

`mysql` and `sqlite` are identical in shape.

**The unbounded cycle, step by step.**

| Step | Live | Claimed | Rows in the table |
|---|---|---|---|
| group fills to the cap | `cap` | 0 | `cap` |
| `ClaimGroup` — stamps every live member | **0** | `cap` | `cap` |
| new members arrive; the cap counts **live**, which is 0 | `cap` | `cap` | **2 × cap** |
| the release **fails**; the deferred `AbandonGroup` runs — claimed return to live | `2 × cap` | 0 | 2 × cap |
| the next claim stamps all `2 × cap` | 0 | `2 × cap` | 2 × cap |
| new members arrive again | `cap` | `2 × cap` | **3 × cap** |
| … | | | **unbounded** |

**Each failed-release cycle admits up to `cap` more durable rows, without limit**, and the only deleter runs on
success. The bound this increment exists to install does not bound the table it was installed in.

**`memory` does not have this hole, and the reason is exactly D-AF's other half.** `memory` counts `len(g.msgs)` —
**live + claimed** — and `ClaimGroup` sets `g.claimedLen` **without shrinking `g.msgs`** (`groupstore.go:151`), so
the claimed members keep counting against the cap. D-AF's own residual-hazard box calls this out as a *cost* (*"a
group at exactly the cap rejects new arrivals even though its live residual is empty"*). **That cost is the
property that makes `memory`'s bound a bound.** `sql` traded it away and got unboundedness in exchange.

**D-AF's justification is also false on its own terms.** *"Claimed members are retained by the database, not by the
process"* — but `sql.GroupStore.ClaimGroup` decodes the **entire claimed set into the process heap**, at
`limit = 0`:

```go
adapter/database/sql/groupstore.go:284-297
func (s *GroupStore) ClaimGroup(ctx context.Context, key string) (msgin.MessageGroupClaim, error) {
	cg, err := s.dialect.ClaimGroup(ctx, s.db, s.table, key, s.lockedBy, s.leaseTTL)
	…
	snap, err := s.decodeGroupRows(cg.GroupRows)      // ← every claimed member, decoded, in-process
	…
}
```

So the `sql` process **does** retain them — one `decodeGroupRows` over the whole claimed set, with **no `LIMIT` at
all** by D-AS's own rule. The quantity D-AF says it is bounding (*"what one `Add` drags into the process heap"*) is
dragged in by `ClaimGroup` instead, unbounded.

**Two options, and the audit recommends (a).**

**(a) Count LIVE + CLAIMED for `sql` too — adopt symmetry.** It collapses D-AF's two-sentence SPI contract to one
(*"a store bounds every member it retains"*), removes the asymmetry Spec §8 item 1 lists as most-likely-reversed,
and bounds the durable table. Cost: one predicate per dialect, plus the consequences below.

**(b) Keep live-only and bound the claimed set separately** — e.g. `ClaimGroup` at `LIMIT cap`. This re-introduces
exactly the truncation D-AS exists to forbid (a legitimately at-cap group releases an incomplete aggregate — the
silent data corruption Spec §5 rejects). **Rejected.**

**RE-DERIVED — the count source is FORCED, not a choice.** The relayed finding asks the bundle to *"pin how the
dialect obtains the live count (a `COUNT(*)` vs `len()` of the `LIMIT maxMembers+1` rows)"*. Under option (a) the
question resolves itself: `len()` of a **live-only** `SELECT` cannot see claimed members at all, so it cannot
produce a live+claimed count under any `LIMIT`. A `COUNT(*)` is the only mechanism — **and all three dialects
already ship one, with an identical signature, already called inside a group transaction**:

```
$ grep -rn "CountMembers" adapter/database/sql/{postgres,mysql,sqlite}/groupdialect.go
postgres/groupdialect.go:196:  remaining, err := pgCountMembers(ctx, tx, mt, groupKey)        ← SettleGroup
postgres/groupdialect.go:373:  func pgCountMembers(ctx, q, mt, groupKey string) (int64, error)
mysql/groupdialect.go:192 / :358      — identical
sqlite/groupdialect.go:208 / :375     — identical

$ sed -n '373,378p' adapter/database/sql/postgres/groupdialect.go
func pgCountMembers(ctx context.Context, q msginsql.Querier, mt, groupKey string) (int64, error) {
	var n int64
	err := q.QueryRowContext(ctx,
		fmt.Sprintf(`SELECT count(*) FROM %s WHERE group_key = $1`, mt), groupKey).Scan(&n)
	return n, err
}
```

**No `claimed_epoch` predicate — it already counts live + claimed.** Option (a) therefore needs **zero new SQL**:
one call to the shipped helper, inside the transaction the dialect already opens, after the member upsert. Its cost
is **one extra `COUNT(*)` per `AddMember`** — on every add, not only the overflow path — and that cost must be
stated rather than discovered.

**RE-DERIVED — a second consequence the finding did not name, and it is not optional.** Under live-only counting,
D-AM's `sql` half reads: *"a `sql` live set is by definition unclaimed, so **every** `sql` over-cap rejection is the
not-leased case and is classified permanent."* **Under option (a) that sentence is false**: a `sql` group can be
over cap **because of its claimed members**, i.e. while a claim is in flight — precisely D-AM's **leased** arm,
which must stay **transient** or a routine claim window permanently dead-letters healthy traffic. So `sql` needs the
leased/not-leased discriminator `memory` gets from `g.leased`.

It is available at **zero extra round-trips**, because all three dialects already read the group row inside
`AddMember`:

| Dialect | Today | Becomes |
|---|---|---|
| postgres | `INSERT … ON CONFLICT … RETURNING created_at` | `RETURNING created_at, locked_by` |
| mysql | `SELECT created_at FROM <group> WHERE group_key = ?` | `SELECT created_at, locked_by …` |
| sqlite | `SELECT created_at FROM <group> WHERE group_key = ?` | `SELECT created_at, locked_by …` |

And `locked_by IS NULL` is **exactly** the premise §3.3.1 was restated to for N-3 — *"nothing drains an UNLEASED
group without an expiry cutoff"* — so the discriminator and the classification premise become the same predicate,
in both stores. D-AM stops needing a `sql` special case.

**Required fix.**

1. **Adopt option (a).** Update **Spec §3.6's decision table**, **D-AF**, **D-AG**, **Spec §7.1** and the **ADR
   Consequences** (which today claim the bound is *"exact, durable"* — true only under (a)).
2. **Pin the count source** as the shipped `*CountMembers` `COUNT(*)` helper, with the reason (`len()` of a
   live-only `SELECT` cannot see claimed members) and the cost (one extra statement per `AddMember`).
3. **Fold in the classification consequence:** `sql` gains D-AM's leased arm, discriminated on `locked_by`, read at
   zero extra round-trips. State the new residual hazard honestly — a **crashed** releaser holds the lease for up to
   `leaseTTL` (default **5m**), and the leased arm is a zero-delay busy-wait under `RetryPolicy{}` for that
   duration — and record it as an open item for the user.
4. **Give it coverage and a mutant:** a `harness` case that fills a group to the cap, claims it, and asserts the
   `cap+1`-th add is **rejected** (not admitted) — mutant: count `claimed_epoch IS NULL` ⇒ the member is admitted
   and the case fails.

---

## MINOR NEW-8 — N-8's disposition is absent from the ADR entirely

```
$ grep -n "65537\|holds 5 members\|members retained at the moment" docs/adrs/0033-group-member-bounds.md
$   (no output)

$ grep -c "65537\|holds 5 members\|members retained at the moment" docs/specs/017-group-member-bounds.md
5
$ grep -c "65537\|holds 5 members\|members retained at the moment" docs/plans/031-group-member-bounds.md
2
```

**Five hits in the spec, two in the plan, zero in the ADR.** N-8 was *"the two stores render different counts at the
same boundary, under a constraint that calls them identical."* Its required fix was a **decision** — *"Decide it
here, not at the keyboard"* — and the decision (keep the divergence; the contract is *"members retained at the
moment of the check"*; pin both renders) was taken and recorded in the spec's §3.4a box and AC-2c, and in the plan's
Global constraint 4. **The ADR, which is where decisions live, records none of it.**

This is two-of-three again — the project's named failure mode *"fold into all three artifacts"* — on the very round
whose Task 10 **Step 0** was added to prevent it. Step 0 is scheduled at the **delivery gate**, which is after
implementation; the divergence it would have caught is a **design** decision.

**Required fix.**

1. **Add the count divergence and its rationale to D-AE**, or as a short new decision: the shape is fixed, the
   number is *"members retained at the moment of the check"*, the two checks sit on opposite sides of the write,
   and normalising `sql` to `len(members)-1` was rejected because it renders a count no statement observed.
2. **Actually run Task 10 Step 0 over the whole revision before closing it** — diff all three artifacts against
   each other for **every** finding, not only this one. **NEW-8 and NEW-10 are both two-of-three today**, which is
   what a spot-check misses and a diff does not.

---

## MINOR NEW-9 — three new off-by-one citations, none of them among N-13's four

N-13's four coordinates were fixed cleanly and each carries its own correction in place. The **class** returned
through different text — two of these three predate revision 3 and were missed by round 2:

| Cited in | The artifact says | Re-derived at `6865886` |
|---|---|---|
| **ADR 0033 Context, `:69`** | `isIntOrInt64` (**`:191-202`**) | **`hasIntOrInt64Param` is `:215`, `isIntOrInt64` is `:231`.** Neither is at `:191-202` |
| **Spec §3.11 and ADR D-AK** | `ErrExpiryChannelRequired` at `aggregator.go:**360-362**` | **the guard is `:362`, the return `:363`** |
| **Plan Task 1, row B1-7** | *"extend the shipped case at `groupstore_test.go:**30-39**`"* | **the case's `name:` is `:29`** (the case runs `:28-41`) |

```
$ grep -n "func hasIntOrInt64Param\|func isIntOrInt64" sizing_option_class_gate_test.go
215:func hasIntOrInt64Param(ft *ast.FuncType) bool {
231:func isIntOrInt64(expr ast.Expr) bool {

$ grep -n "ErrExpiryChannelRequired" routing/aggregator.go | tail -1
363:		return nil, msgin.ErrExpiryChannelRequired
$ sed -n '362p' routing/aggregator.go
	if cfg.timeout > 0 && cfg.expired == nil {

$ sed -n '29p' adapter/memory/groupstore_test.go
			name: "a new key beyond WithMaxGroups is ErrOverflowDropped",
```

Each claim's **content** is correct; only the coordinates are wrong. Fix the three.

**This is why N-13 scores LANDED-BUT-FLAWED rather than clean.** Its four *named* coordinates were fixed
correctly and each carries its correction in place — but a finding whose subject is *"citations written by hand and
not re-run"* is not discharged by fixing the four instances it happened to list, when the revision that fixes them
adds three more. **Fixing the enumerated instances of a class-shaped finding is the definition of landing it
flawed.**

**Note the pattern, since it is now three rounds old:** every round of this audit has returned an off-by-one
citation finding, and every round's fix has corrected the named instances only. The class is *"a line number
written by hand and not re-run before the artifact was closed."* **Round 4's fix should be a mechanical sweep of
every `file:line` in the bundle, not another list of corrected instances.**

---

## MINOR NEW-10 — four normative statements still name the WARN N-11 proved never fires

N-11's fix landed as **correction boxes** — one in D-AM (`:266-276`), one in Spec §3.3.1 (`:519-529`), and the
`:968-973` once-per-consumer note in §3.11's table. All three are correct. **Four normative statements elsewhere
were not touched**, and one of them is twenty lines below its own correction box.

**RE-DERIVED — none of the four contains the string `warnInvalidFallback`.** They name the signal in prose (*"a
WARN on the dead-letter fallback"*, *"WARN on fallback"*, *"a WARN when the fallback fires"*), which is why a grep
for the identifier finds only the correction boxes and misses these:

```
$ grep -n "warnInvalidFallback" docs/adrs/0033-*.md docs/specs/017-*.md
docs/adrs/0033-group-member-bounds.md:258     (the quoted consumer.go source)
docs/adrs/0033-group-member-bounds.md:269     (the correction box)
docs/adrs/0033-group-member-bounds.md:836     (the D-AJ correction box)
docs/specs/017-group-member-bounds.md:511     (the quoted consumer.go source)
docs/specs/017-group-member-bounds.md:520     (the correction box)
docs/specs/017-group-member-bounds.md:523     (the correction box)
docs/specs/017-group-member-bounds.md:528     (§3.11's table — correctly marked once-per-consumer)
```

**The four sites, quoted:**

| # | Site | What it says |
|---|---|---|
| 1 | **ADR `:288`** — **D-AM's own decision table**, the `NOT leased` row | *"Terminal, diverted to a sink, **WARN on the dead-letter fallback**, and works on the shipped zero-value `RetryPolicy`."* **Twenty lines below the correction box at `:266-276` that proves that WARN never fires on the zero value** |
| 2 | **ADR `:869`** — D-AK's *"strict improvement"* paragraph | *"observability goes from silent … to one typed, named error per rejected member at the operator's sink, **plus a WARN when the fallback fires**."* |
| 3 | **Spec `:541`** — §3.3.1's **THE CONTRACT** table, the `NOT leased` row | *"Terminal, sink-diverted, **WARN on fallback**, works on `RetryPolicy{}`"* |
| 4 | **Spec `:1072`** — §3.9's revised D-AJ box | *"the boundary is loud (**a WARN on the dead-letter fallback**), typed, terminal rather than retryable"* |

Sites 1 and 3 are the **normative classification tables** of the decision this increment turns on. Site 4 is the
load-bearing sentence of D-AJ's *"is a default legitimate"* argument — the argument B-1 already forced a rewrite of
once. **A reader who reads only the tables gets revision 2's falsified claim.**

**Required fix.** Edit **the cells themselves** — not another box beside them — to name `divertTerminal`'s nil-sink
WARN (`consumer.go:1049`) plus the `safeAck` (`:1073`), and mark `warnInvalidFallback` as **once per consumer**
wherever it is the signal being described. A correction box that does not reach the table it corrects has fixed the
prose and left the contract.

---

## What I checked and found CLEAN — newly verified this round

Recorded so a round 4 does not re-derive it. Everything below was attacked in round 3 and survives.

**Rounds 1 and 2's clean lists still hold.** Nothing in revision 3 disturbed §1.1's four-release-path table, §1.4's
two-mechanism analysis, the quadratic claim, §3.4a's cap-check placement and its three mutants, §3.5's boundary
arithmetic, D-AM's permanent-arm trace, §3.3a's `group == nil` compatibility arm, AC-6's no-test-grows-a-group
constraint, AC-9 branch 2, AC-10's two-probe design, or Task 10 Step 3b's `GOARCH=386` gate. Those are earlier
rounds' findings and are not re-listed.

### Every error string and every file the bundle quotes was verified to EXIST

**This is the check that caught a phantom assertion on the sibling increment**, and it was run over this bundle for
the same reason: a quoted string that no code produces is invisible to every other kind of review, because it reads
as evidence. *(The sibling finding itself belongs to [Plan 032](032-byte-cap-ceilings.md)'s record — out of scope
here, and not restated.)*

**Result for this bundle: every quoted error string and every cited file exists, with one exception, and the
exception is NEW-5's — the `sql` overflow render, which is a string the bundle PROPOSES rather than quotes, and
which no shipped site would produce.** Verified:

| Quoted by the bundle | Verified |
|---|---|
| `"msgin: capacity out of range: memory.WithMaxGroupMembers: 0 not in [1, 1048576]"` | the `checkRange` render shape reproduces from `adapter/memory/helpers.go:54`; the four shipped copies are at `endpoint/helpers.go:97`, `routing/helpers.go:88`, `adapter/memory/helpers.go:54`, `adapter/http/helpers.go:64`, and `adapter/database/sql` has none — as §3.2 states |
| `"msgin: message dropped by overflow policy"` | `msgin.ErrOverflowDropped`'s text; four producer sites reproduce (`queuestore.go:171`, `:176`, `adapter/memory/groupstore.go:124`, `endpoint/consumer.go:576`) |
| `"msgin: permanent: "` | `permanentError.Error()`'s prefix, `reliability.go:13` |
| `"msgin/sql/postgres: group ops require a *sql.DB or *sql.Tx Querier, got %T"` | `postgres/groupdialect.go:67` — **exact** |
| `"msgin/sql/mysql: …"` / `"msgin/sql/sqlite: … (dedicated BEGIN IMMEDIATE conn)"` | `mysql/groupdialect.go:63`, `sqlite/groupdialect.go:55` — **exact** |
| `"takes the GROUP ROW LOCK (SELECT ... FOR UPDATE or equivalent)"` | `adapter/database/sql/groupdialect.go:109-113` — reproduces verbatim, still uncorrected on `main`, which is what makes N-9's fix necessary |
| `"a retry / next member / next reaper tick re-releases"` | `adapter/memory/groupstore.go:185-187` |
| `"serializes same-key adds (H1)"` | `postgres/groupdialect.go`'s `AddMember` comment |
| `"another Handle/process is releasing this group; held"` | `routing/aggregator.go:438-439` |
| `"A durable store (RecoverInterval() = its lease TTL) … go agg.Run(ctx) is REQUIRED"` | `routing/aggregator.go:530-532` |
| `"hot-spins when Backoff is nil (the default)"` | `endpoint/consumer.go:96` |
| `"WITHOUT stamping msgin.message-id/msgin.timestamp"` | `message.go:198`'s godoc; the `if cfg.id == "" { cfg.id = NewID() }` guard is `message.go:178-180` |
| `"a pre-1.0 (v0) contract that may still evolve"` | `adapter/database/sql/groupdialect.go:106` |
| `"rather than evicting a partial group (which would be silent data loss)"` | `memory.WithMaxGroups`'s godoc |

**Every test file and code file the bundle names exists**, including the ones a task is told to open:
`routing/aggregator_test.go`, `adapter/memory/groupstore_test.go`,
`adapter/database/sql/{groupstore.go,groupdialect.go,helpers.go,groupdialect_fake_test.go,groupstore_unit_test.go}`,
`adapter/database/sql/harness/{groupstore.go,testkit.go}`, `sizing_option_class_gate_test.go`, `message.go`,
`routing/example_aggregator_test.go`. **Two names are correctly recorded as NOT existing** and the bundle says so:
`routing/example_test.go` (m-7 — the convention is `example_<subject>_test.go`) and
`group_member_bound_invariant_test.go` (Task 3 creates it).

### Structural claims re-derived and correct

- **The `harness`/`dbtest` blast radius is real and exactly two modules.** `harness/testkit.go:87` holds
  `Group msginsql.GroupDialect`; `harness/groupstore.go:345` calls the **8-argument** `AddMember`;
  `dbtest/go.mod` requires `harness` with a local `replace`. Global constraint 8's box reproduces its own greps.
- **The seven `AddMember` sites reproduce**, at the coordinates Task 5 Step 1 lists.
- **The three `*SelectMembers` helpers have exactly three callers each**, at the coordinates D-AS lists. The
  helper signature is `(ctx, q, mt, groupKey, claimedWhere string)` — a **string predicate**, so D-AS's separate
  `limit int` parameter is the right shape rather than an overload of the existing one.
- **`sqlite` really has no `sqliteRunInTx`**; `withImmediateConn` is `sqlite/groupdialect.go:52-77` and
  `BEGIN IMMEDIATE` is `:62`. M-3's per-dialect table reproduces in all three rows.
- **The `pgRunInTx` three-branch shape reproduces**, `*sql.Tx` → `return fn(tx)` with no rollback, and
  `WithSharedTransaction` really does return an `Option` (`options.go:201`), not a `GroupStoreOption`.
- **`NewGroupStore`'s option surface is exactly `WithGroupLeaseTTL` (`:140`), `WithGroupLockedBy` (`:155`) and a
  logger**, over a concrete `*stdsql.DB` (`:211`, `:40-42`), and `:271` always passes `s.db`. N-2's fix is exact.
- **`RecoverInterval()` returns `0` for `memory` (`:220`) and `s.leaseTTL` for `sql` (`:348`)**, and
  `defaultGroupLeaseTTL = 5 * time.Minute` (`groupstore.go:22`). §1.2.1's table is right in every cell.
- **`adapter/database/sql/groupstore.go` already declares its other two defaults as named `const`s** — `:22` and
  `:30` — so D-AR's named-constant requirement is *consistent* with that file's local precedent even though it
  deviates from `adapter/memory`'s `maxGroups: 1024`. **D-AR's reasoning is stronger than D-AR states.**
- **`WithInboxTable` has no `WithTable` twin** (`inbox_dedup.go:35` only), so N-10's deletion is correct.
- **`harness` still contributes no half-1 key** —
  `grep -rnE "^func [A-Z][A-Za-z]*\(.*\b(int|int64)\b" adapter/database/sql/harness/*.go` returns nothing — so
  AC-5's box and Task 7 Step 5's box remain correct and remain necessary.

### Decisions attacked and left standing

- **D-AS is correct and is the right generalization of N-5** — which is why **N-5 scores LANDED**. A private
  parameter, `0` = unlimited, one non-zero caller, mutation-proven by B6-7 / Task 7 Step 5b, over helpers whose
  existing parameter is a **string** predicate (so a separate `limit int` is the right shape, not an overload).
  **NEW-7 attacks D-AF's counted set, which D-AS is composed with — not D-AS itself.** `limit = 0` on
  `ClaimGroup` is the correct value and must stay: any other truncates a claimed set.
- **D-AR is correct**, and its scope note (*"`maxGroups: 1024` is NOT changed"*) is the right call: no invariant
  test reads it, and an unforced edit to shipped code for symmetry alone is what this project's review gate
  rejects.
- **Global constraint 8's *compiles-against* restatement is the correct generalization of N-1**, and the box
  states it as a class rather than an instance. **NEW-2 attacks one command inside the gate it created, not the
  rule.**
- **Task 3's three mutants (B3-1/B3-2/B3-3) are the right three**, and B3-3 — *"the file list is asserted, not
  discovered"* — is the sharpest row in the task. It is also what NEW-1 shows the split cannot honour.
- **AC-4b's dialect-level entry point is right**, and its added contrapositive pair (drive the same overflow
  through a real `sql.GroupStore` and assert exactly `cap` rows remain) is a genuine improvement on N-2's required
  fix rather than a minimal compliance.
- **The two new Spec §8 open items (7, D-AR; 8, D-AS) are correctly framed as user decisions** with reversal costs
  stated, and neither is an audit finding.

### Gates verified non-vacuous or verified clean

- **The docs-link gate baseline is unchanged** at this tree: exactly the two known arm-1 false positives
  (`docs/plans/016-aggregator.md -> docs/plans/m` and
  `docs/specs/006-cron-source.md -> docs/specs/factory(fireTime`, both Go identifiers leaking from line-wrapped
  inline code) and **zero** arm-2 hits.
- **All three bundle files' internal links resolve**, including the new cross-references revision 3 added between
  §1.2.1, §3.3.1, §3.11 and §8 item 5.
- **`go vet` in `dbtest` is not vacuous** — it type-checks all four external test files
  (`go list` shows `XTEST=[…]`, `GoFiles=[]`), which is exactly what `go build` there does not. This is the
  evidence behind NEW-2's required fix.

---

## Auditor's method note

Every command in this record was run on the tree at **`6865886`** (current `main`) with `GOTOOLCHAIN=go1.25.13` on
darwin/arm64. The `dbtest` `go list`/`go build`/`go vet` triple, the `TestKit` field scan, the
`ClaimGroup`/`AbandonGroup`/`SettleGroup` SQL, the `*CountMembers` inventory, the three dialect `AddMember` bodies,
the `hasIntOrInt64Param`/`isIntOrInt64` coordinates, the `ErrExpiryChannelRequired` guard, the
`groupstore_test.go` case header, the four NEW-10 sites and the ADR/spec/plan greps are all first-hand output, not
transcription. **No file in the repository was modified.**

Round 3 deliberately did **not** re-audit [Spec 018](../specs/018-byte-cap-ceilings.md) /
[ADR 0034](../adrs/0034-byte-cap-ceilings.md) / [Plan 032](032-byte-cap-ceilings.md): they are a **sibling
increment, already CLEARED and landing first**, and the only interaction with this bundle — serialization on
`sizing_option_class_gate_test.go` — is already correctly stated in all three artifacts (round 1's **B-3**, clean).

---

**VERDICT: NOT SAFE TO IMPLEMENT.**

*Revision 3 generalized round 2's two structural fixes correctly, but stopped the fix at the boundary of each
finding's own wording. **Five findings landed flawed for exactly that reason:** N-1's compiles-against rule was
promoted to a Global constraint and then given a `dbtest` gate arm that compiles nothing (**NEW-2**), a Step 6b
that threads a cap from a field that does not exist (**NEW-3**), and a Task 3 split that misreads the rule's
mechanism for its shape (**NEW-1**, the BLOCKER); N-4 fixed the declaration's form and not its file (**NEW-4**);
N-8's fix pinned a render naming a site the dialect cannot know (**NEW-5**) and never reached the ADR
(**NEW-8**); N-11's correction box never reached the four tables it corrects (**NEW-10**); and N-13 corrected its
four named citations while revision 3's own prose added three more (**NEW-9**).*

***The remaining two findings are new ground, against fixes that landed CLEAN.*** *N-7's promotion of the
downgrade rule was complete — the **rule** it promoted is over-broad (**NEW-6**). And D-AS's `limit = 0` is the
right value — it is **D-AF**, composed with it, that leaves the durable table this increment exists to bound
growing without limit (**NEW-7**), the only design flaw of the round.*
