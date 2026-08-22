# Plan 031 — adversarial design audit, round 4 (2026-08-22)

Independent Opus subagent, handed the **complete Plan 031 revision-4 bundle together** — [Spec
017](../specs/017-group-member-bounds.md) + [ADR 0033](../adrs/0033-group-member-bounds.md) +
[Plan 031](031-group-member-bounds.md) — **before any implementation code existed**, per
[`CLAUDE.md`](../../CLAUDE.md)'s design-time gate. All three artifacts declare themselves **revision 4,
post-audit-round-3**, and carry the 🔴 *"decided WITHOUT USER RATIFICATION"* banner; that banner is not itself a
finding, but every decision it covers (**D-AC** … **D-AT**) was treated as open.

Round 4 has the same **two jobs** rounds 2 and 3 had, and they remain separate: (1) **verify the 10 round-3
findings actually landed** in the revision — not that the revision *mentions* them — and (2) **attack the revised
bundle afresh**, including the reversed **D-AF** and the new **D-AT**. Both are recorded below; the
fix-verification table comes first because it is what distinguishes a revision from a rewrite.

**Round 4 was warranted, and all three artifacts said so themselves.** Revision 4 is the first revision since
round 1 to change a **decision** rather than a premise: D-AF was reversed, which puts a new `COUNT(*)`, a new
`locked_by` read and a **new transient classification arm** into three dialect modules. That is not the bundle
round 3 audited.

**Traceability.** Audits: [Spec 017](../specs/017-group-member-bounds.md),
[ADR 0033](../adrs/0033-group-member-bounds.md), [Plan 031](031-group-member-bounds.md). Predecessor rounds:
[`031-audit-round-1.md`](031-audit-round-1.md), [`031-audit-round-2.md`](031-audit-round-2.md) and
[`031-audit-round-3.md`](031-audit-round-3.md) — **all three immutable**; nothing below edits any of them.
Origin: [`docs/HANDOVER.md`](../HANDOVER.md) §6 backlog item **7**. Predecessors whose ratified decisions the
bundle reuses: [Spec 016](../specs/016-sizing-option-bounds.md), [ADR 0032](../adrs/0032-sizing-option-bounds.md),
[Plan 029](029-sizing-option-bounds.md), [ADR 0031](../adrs/0031-nil-option-elements.md) **D-R**. Colliding
concurrent work: [Plan 030](030-post-029-maintenance.md) (landed) and — **new since round 3** —
[Spec 018](../specs/018-byte-cap-ceilings.md) / [ADR 0034](../adrs/0034-byte-cap-ceilings.md) /
[Plan 032](032-byte-cap-ceilings.md), **which has now LANDED** (`f39725d`) and **rewrote the file this increment
serializes on**. That landing is the mechanism behind three of this round's eight findings.

This record is **evidence-primary** — it is the audit artifact, not a user-facing summary. Every structural claim
in the revised bundle was re-derived on this tree with `GOTOOLCHAIN=go1.25.13`, darwin/arm64, at **`f39725d`** —
the last commit to touch Go code. (`a306241`, current `main`, is docs-only: `git diff --stat f39725d..a306241`
lists four `.md` files and no `.go` file, so the code tree the bundle's instructions run against is identical at
both.) Commands and their output are pasted below. No file in the repository was modified while auditing.

> **⚠️ IMMUTABLE EXECUTION RECORD.** This file records what round 4 found, at the time it found it. Do not edit it
> to reflect later decisions, later measurements, or the revised bundle. Corrections belong in a later round's
> record or in the artifacts themselves. The coordinator's dispositions for these findings live in **Spec 017 /
> ADR 0033 / Plan 031 revision 5**, each of which cites this file.

> **📋 PROVENANCE — read this before treating any sentence below as the auditor's own words.**
>
> **Verbatim, from the round-4 report:** the **verdict framing sentence**, the **score line**, and **every verdict
> cell of the 10-row fix-verification table** in Part 1. The verdict column is the auditor's, row by row, and is
> not a reconstruction — the round-3 record's lesson (*an earlier draft reconstructed the column from the totals
> and got three rows wrong*) is honoured here by copying the cells rather than deriving them.
>
> **Relayed and then re-derived:** every **finding** below — its claim-under-attack, its coordinates and its
> required fix — and the **evidence cells** of Part 1's table. **Every citation in this file was independently
> re-run against the tree at `f39725d` by the recorder before it was written down**, and the commands are pasted
> inline.
>
> ⚠️ **R4-7 WAS RELAYED UNLABELLED, AND IS RECONSTRUCTED FROM THE COORDINATOR'S BRIEF RATHER THAN QUOTED FROM THE
> AUDITOR.** The auditor's finding index runs **R4-1 … R4-8** and its verdict counts **1 BLOCKER, 3 MAJORs, 4
> MINORs = 8**. The coordinator's hand-off carried **R4-7's remedy folded into R4-2's third bullet, without an
> R4-7 label**, because the two findings take the *same edit*. The recorder's first draft therefore listed R4-7 as
> **NOT RELAYED** and left it open; **the coordinator has since confirmed the labelling error and supplied the
> finding's own wording, and it is written up below as LANDED via that same edit.** The correction is noted rather
> than silently folded in, because this record's provenance is its whole value: **R4-7's claim-under-attack and
> required fix below are the COORDINATOR's restatement, not the auditor's verbatim text**, unlike every other
> finding in Part 2. Its **evidence** was re-derived first-hand like all the rest.
>
> **All eight findings are therefore accounted for, and revision 5 closes all eight.** No hole remains.

---

## Verdict

**NOT SAFE TO IMPLEMENT.** 1 BLOCKER, 3 MAJORs, 4 MINORs. The auditor's score line, verbatim:

> **Score: 7 clean LANDED, 3 LANDED-BUT-FLAWED, 0 NOT LANDED, 0 REGRESSED — counted off the table below, row by
> row, not tallied alongside it.**

> ✅ **THE ROUND-3 PROCEDURAL FIX WORKED, AND THAT IS ITSELF A RESULT WORTH RECORDING.** Rounds 2 and 3 each
> produced a summary tally that disagreed with the auditor's own evidence table by exactly one row in the middle
> bucket, and [`031-audit-round-3.md`](031-audit-round-3.md) recorded that as *"a method defect in the audit
> apparatus, not in the artifacts"*, with the remedy stated on the audit brief: **a round-4 report should generate
> its score line from its own table rather than state it separately.** Round 4 did exactly that — the auditor
> counted its own ten rows to produce `7 / 3 / 0 / 0` — and **the score line and the table agree for the first
> time on this increment.** The recorder re-tallied the column by name and reproduces `7 / 3`. **The `43 ≠ 43`
> rule, applied to the audit apparatus itself, closed a two-round drift.**
>
> | Round | Summary line | Table tally | Gap |
> |---|---|---|---|
> | 2 ([`031-audit-round-2.md`](031-audit-round-2.md)) | 12 / 8 / 1 (M-8) | 13 / 7 / 1 | one row, middle bucket |
> | 3 ([`031-audit-round-3.md`](031-audit-round-3.md)) | 8 / 6 | 9 / 5 | one row, middle bucket |
> | **4 (this record)** | **7 / 3** | **7 / 3** | **none — the line was GENERATED from the table** |

**The framing matters more than the count, and it is the lesson of this round.** Verbatim:

> **The design is sound; the execution instructions are not synchronised with the tree they run against.**

That sentence partitions the round exactly. **No decision in ADR 0033 was falsified.** D-AF's reversal survives
attack and its supporting code claims are all true (see *"Newly verified CLEAN"*, which verifies **every code
claim the reversal rests on** rather than spot-checking it). D-AT is right. D-AS, D-AR, D-AQ, D-AE's two new
sub-decisions, D-AH's restated clause and D-AN's deleted claim all land clean.

**What is broken is the layer between the design and the keyboard.** [Plan 032](032-byte-cap-ceilings.md) landed
at `f39725d` and **rewrote `sizing_option_class_gate_test.go` — 237 lines, 869 lines total, every coordinate past
`:19` moved.** Revision 4 was written against `6865886`, before that landing. The consequences:

- **Every `sizing_option_class_gate_test.go` coordinate in all three artifacts is stale** (**R4-2**), including
  **two of round 3's own three NEW-9 fixes** — `hasIntOrInt64Param` and `isIntOrInt64`, corrected in revision 4 to
  `:215`/`:231` and now at `:243`/`:259`. *The off-by-one class returned through the fix for the off-by-one class,
  for the fourth consecutive round.*
- **Two of those figures are wrong in COMPOSITION, not merely in coordinate** — the arm arithmetic and the
  *"`fixed` ⇒ `1<<30`"* rule (**R4-2**).
- **Two exact-map `require.Equal`s that no artifact in this bundle names** now stand between Tasks 1 and 5 and
  green (**R4-1**, the BLOCKER). They **predate Plan 030** and have been missing from the edit list through all
  four rounds.

**The BLOCKER also carries a design question the bundle has no authority to answer**, and it is the sharpest thing
in this round: the assertion Tasks 1 and 5 must edit **tells its own implementer to go and amend Spec 016 §2.1 and
§6 AC-5** — a different, **delivered** spec — and nothing in Plan 031 authorises that edit. See R4-1.

---

## Part 1 — fix verification: did round 3's 10 findings land?

**The verdict column below is the auditor's, verbatim, row by row.** The evidence cells were relayed and then
re-run against the tree at `f39725d` by the recorder. **The column tallies 7 LANDED / 3 LANDED-BUT-FLAWED, which
is what the score line says** — the first round on this increment where those agree.

| # | Sev | Verdict | Evidence |
|---|---|---|---|
| **NEW-1** | BLOCKER | **LANDED-BUT-FLAWED** | The *"or split it"* alternative is **deleted, not deprecated** (Plan Task 3 header box); **D-AT** minted with the four-row shape-vs-mechanism table and the four ways the split is worse; the Sizing table gains an explicit **Order** column (`1 → 2 → 4 → 5+6 → 3 → 7 → 8 → 9 → 10`); all three dangling references fixed — Task 10 Step 5, the Sizing row and Task 3 Step 6's single commit message; Task 3 Step 2's *"pre-Task-1 tree"* probe corrected to run where all three constants exist. **The reversal is correct and completely landed.** One flaw, inside the mechanism that enforces it: **Step 0's order gate asserts a COUNT and stops on a correct tree** (**R4-4**) |
| **NEW-2** | MAJOR | LANDED | Plan Task 6 Step 5's `dbtest` row is now **`GOWORK=off go vet ./...`**, and the row carries the reason on it (*"every Go file here IS a `_test.go` file, so `go build` compiles NOTHING and exits 0"*). The **both-directions table** was added below it — `harness` (no `_test.go` ⇒ `go test` false-passes) against `dbtest` (only `_test.go` ⇒ `go build` false-passes) — with *"`go vet` is the only command correct for both."* Re-derived: `cd adapter/database/sql/dbtest && GOWORK=off go list -f '{{.GoFiles}}'` → `[]`, all four files `XTestGoFiles`. Task 5 Step 6b carries the same pair |
| **NEW-3** | MAJOR | LANDED | *"threading the cap from the existing `TestKit`"* is **gone**; Task 5 Step 6b now declares a **`harness`-package unexported constant**, given verbatim as a code block with its godoc (`const groupMemberCap = 4`), in `adapter/database/sql/harness/groupstore.go` — a file already in that task's Files list. Task 7 Step 1 and Step 5 both bind to *"that same unexported constant"*. The escape hatch is stated and priced (*"if a `TestKit` field is nevertheless preferred, `testkit.go` must join this task's Files list and ADR 0033 must record that `harness` gains an exported field"*). Re-derived: `grep -nE "^\s+[A-Z][A-Za-z]*\s+(int\|int64)\b" adapter/database/sql/harness/testkit.go` still returns nothing, so the finding's premise still holds and the fix is still necessary |
| **NEW-4** | MAJOR | LANDED | The declaration's **file** is now named everywhere the form was: Plan Task 1 Step 4 (*"DECLARED IN `adapter/memory/groupstore.go` — the file AC-3.3 parses"*), Plan Task 5 Step 4's box (*"THE CONSTANTS GO IN `adapter/database/sql/groupstore.go`, NOT IN `helpers.go`"*, with the reason: the step's own subject is `helpers.go` and `checkRange`'s range arms are the constants' natural neighbours), Spec §4 item 2, and **ADR D-AR's new box**. Four-way, and the local-precedent argument (`defaultGroupLeaseTTL` `:22`, `defaultExpiredGroupsLimit` `:30`) is carried in three of the four. Both coordinates reproduce |
| **NEW-5** | MAJOR | LANDED | The site string is decided as **`msgin/sql/<engine>: AddMember`** and carried four-way: Plan **Global constraint 4**'s new four-row table (`memory.GroupStore.Add` \| `msgin/sql/postgres: AddMember` \| `…mysql…` \| `…sqlite…`), Plan Task 6 Step 4, Plan Task 7 Step 2 **item 6**, and **ADR's new `#### D-AE's %s` subsection**. The render assertion **moved to Task 7**, against a real engine, with the reason stated (*"a render assertion through Task 5's fake dialect would be vacuous — the fake mints whatever the test hands it"*), and **B6-10** was added to Task 6's branch table. The shipped-convention evidence reproduces exactly: `postgres/groupdialect.go:67`, `mysql:63`, `sqlite:55` |
| **NEW-6** | MAJOR | LANDED | The two-clause rule is written out in full in **all three** places the false form appeared: Spec §3.3a.1's *"direction rule that governs all six — RESTATED in revision 4"* (`:695`), Spec **§3.7**, and **ADR D-AH's MAY clause**. The consequence is stated, not glossed — *"a persistently failing claim/release path RETRIES rather than terminating"* — and appears again as a new **Consequences** bullet. **D-AN's *"a bug in the drain path costs a dead-letter, not a production-down spin"* is DELETED**, with the deletion recorded in place (ADR `:523`, Spec `:735`). Plan Task 8 Step 2 carries the wording and says *"copy it, do not paraphrase"* |
| **NEW-7** | MAJOR | **LANDED-BUT-FLAWED** | **The reversal is thorough and correct.** D-AF is rewritten head-to-toe (both stores count live + claimed; the three-part refutation of revision 3's justification; the rejected *"bound `ClaimGroup` instead"* alternative; the REVERSIBILITY line); Spec §3.4, §3.5, §3.6, §7.1 item 1, §8 item 1 (**closed**), AC-9 rows **16/17**, Plan's *"counted set"* section, Task 6 Step 2(a)+(b), **B6-8/B6-9**, Task 7 Step 2 item 7 and the Sizing table all move together. **Every code claim it rests on was independently verified and every one is true** — see *"Newly verified CLEAN"*. Two flaws, both in the **magnitude** of the hazard it introduces rather than in the decision: the crashed-lease window is understated by a factor of ~2 and the wrong figure is scheduled into **public godoc** (**R4-3**), and the `sqlite` engine's `COUNT(*)` cost is stated engine-neutrally when sqlite's is qualitatively different (**R4-8**). A third consequence, the `classifyQueryErr` Consequences bullet, is now false for the arm the reversal created (**R4-6**) |
| **NEW-8** | MINOR | LANDED | The count divergence reached the ADR as its own subsection — **`#### D-AE's %d — the two stores render DIFFERENT COUNTS, deliberately (NEW in revision 4)`** (`:269`) — carrying the shape, the *"members retained at the moment of the check"* contract, the opposite-sides-of-the-write mechanism, and the rejection of normalising `sql` to `len(members)-1`. The two-of-three is closed: `grep -c "65537\|holds 5 members\|members retained at the moment"` now returns hits in **all three** artifacts, where round 3 measured 5 / 2 / **0** |
| **NEW-9** | MINOR | **LANDED-BUT-FLAWED** | All three named coordinates corrected, each with the old value struck in place: `isIntOrInt64` (ADR `:99` — *"`:231`, reached from `hasIntOrInt64Param` at `:215` — not `:191-202` as revisions 1-3 cited"*), `ErrExpiryChannelRequired`'s guard (`aggregator.go:362`, ADR `:1088` / Spec `:1336`), and `groupstore_test.go`'s case header (Plan B1-7 — *"the shipped case at `:28-41`, whose `name:` is `:29` — not `:30-39` as revision 3 cited"*). **Two of the three fixes are stale again at `f39725d`** (**R4-2**): `hasIntOrInt64Param` is `:243` and `isIntOrInt64` is `:259`. The third (`aggregator.go:362`) and the fourth (`groupstore_test.go:29`) still reproduce. **FOURTH consecutive round for this class, and the first in which the fix itself was falsified rather than merely joined by new instances** |
| **NEW-10** | MINOR | LANDED | **The cells themselves were edited, not boxed.** All four sites now name `divertTerminal`'s nil-sink WARN (`consumer.go:1049`) **plus `safeAck` (`:1073`)**, and mark `warnInvalidFallback` as **once per consumer** (`:968-973`): ADR D-AM's decision table `NOT leased` row (`:366`), ADR D-AK's *"strict improvement"* paragraph (`:1077-1080`, which additionally records the old wording it replaced), Spec §3.3.1's **THE CONTRACT** table `NOT leased` row (`:561`, which names NEW-10 in the cell), and Spec §3.9's D-AJ box (`:1267-1268`). `grep -n "WARN on the dead-letter fallback\|WARN on fallback\|WARN when the fallback fires"` over both files returns **nothing** |

**The 10 rows close.** Tallied **by name** off the auditor's verdict column above:

| Disposition | Findings | Count |
|---|---|---|
| **LANDED (clean)** | NEW-2, NEW-3, NEW-4, NEW-5, NEW-6, NEW-8, NEW-10 | **7** |
| **LANDED-BUT-FLAWED** | NEW-1, NEW-7, NEW-9 | **3** |
| **NOT LANDED** | — | **0** |
| **REGRESSED** | — | **0** |

**7 + 3 = 10; no row is missing, duplicated or unclassified, and the score line agrees.**

**Where the findings attach.** Seven of the eight attach to a flawed row; **one is new ground**, and it is the
BLOCKER:

| Row | Verdict | Findings |
|---|---|---|
| **NEW-1** | FLAWED | **R4-4** (Step 0's order gate counts instead of asserting, and stops on a correct tree) |
| **NEW-7** | FLAWED | **R4-3** (the crashed-lease window understated ~2×, in **public godoc**), **R4-6** (the `classifyQueryErr` bullet is false for the new leased arm), **R4-8** (sqlite's `COUNT(*)` cost is not the same cost as postgres/mysql's) |
| **NEW-9** | FLAWED | **R4-2** (every gate coordinate stale, **including two of NEW-9's own three fixes**; two figures wrong in composition), **R4-5** (the `checkRange` inventory), **R4-7** (all three artifact headers name a commit that is no longer `main`) |
| — | **new ground** | **R4-1** (BLOCKER — two exact-map `require.Equal`s no artifact names) |

**Nothing regressed and nothing was ignored.** The revision is responsive, its one decision reversal is correct
and thoroughly propagated, and it is nevertheless still not implementable — **because the tree moved under it.**

### Why "the design is sound" is a stronger statement this round than it looks

Rounds 1–3 each falsified a **premise** or a **decision**: B-1 (the hot spin), M-6 (the deadlock), M-3 (one
mechanism for three engines), N-1 (a cross-module edit is a red commit), NEW-7 (the durable table is unbounded).
**Round 4 falsified none.** Every finding below is a **coordinate, a magnitude, a count, or an edit-list omission**
— defects in the *instructions*, not in the *design*.

That distinction changes what revision 5 must do. **A prose sweep is not the remedy**, and R4-2 says so
explicitly: the class returned through its own fix precisely because the fix was another hand-typed list. The
remedy is a **method change** — generate the coordinate list from the file, at execution time.

---

## Part 2 — findings

## Finding index

| # | Rank | One line |
|---|---|---|
| **R4-1** | BLOCKER | `sizing_option_class_gate_test.go` half 2 holds **two exact-map `require.Equal`s no artifact in this bundle names** — `wantArms` (19 entries) and the `byArm` literal `{"fixed": 12, "rejects": 1, "safe": 6}`. Adding two conformance rows makes both mismatch, both are `require`, and **Tasks 1 and 5 cannot reach green.** They predate Plan 030 and have been missing through all four rounds. **And the assertion's own failure message directs the implementer to amend Spec 016 — a different, DELIVERED spec — which nothing in this bundle authorises** |
| **R4-2** | MAJOR | **Every** `sizing_option_class_gate_test.go` figure in the bundle is stale — Plan 032 rewrote the file at `f39725d` — and **two are wrong in COMPOSITION**: the arm arithmetic (*"11 + 1 + 3 + 6"*) and the *"`fixed` ⇒ `1<<30`"* rule. The three headers also name the wrong tree. **Re-deriving the citations is not the remedy; deriving the edit list from the file is** |
| **R4-3** | MAJOR | The crashed-lease hazard the user is asked to ratify is understated by **~2×** — the reaper's tick cadence is independent of the crash, so discovery is up to **2 × leaseTTL ≈ 10 minutes** — and the wrong figure is scheduled into `sql.WithMaxGroupMembers`'s **public godoc**. The per-iteration cost is also mis-stated as *"a zero-delay busy-wait against the database"* |
| **R4-4** | MAJOR | Task 3 **Step 0**'s order gate asserts `grep` returns **two** hits and says STOP otherwise. On a **correct** tree it returns four or more. **The gate that enforces round 3's BLOCKER fix stops on a correct tree** |
| **R4-5** | MINOR | The `checkRange` inventory is stale: the pasted command returns **five** hits, not four, and `adapter/http`'s copy moved `:64 → :73` |
| **R4-6** | MINOR | ADR Consequences: *"**Every** overflow rejection on the `sql` path costs an extra `SchemaExists` round-trip … **Bounded now that D-AM makes the rejection terminal**."* Revision 4 gave `sql` a **transient** leased arm, so a leased rejection is not terminal and pays the probe on every retry, for R4-3's window |
| **R4-7** | MINOR | **All three artifact headers assert re-derivation against a commit that is NO LONGER `main`** — *"re-derived against `d2c69fe` — current `main`"* (Spec `:44`, Plan `:38`, ADR `:55`). Both halves are false at `f39725d`. **Same remedy as R4-2**, which is why the coordinator's brief folded it in unlabelled |
| **R4-8** | MINOR | The `COUNT(*)` cost is stated engine-neutrally, but on sqlite the enclosing transaction holds `BEGIN IMMEDIATE`'s **database-wide** write lock, so the added `O(members)` scan lengthens a **global** write-serialisation window on every add to **any** group — not a per-key one as on postgres/mysql |

---

## BLOCKER R4-1 — two exact-map `require.Equal`s stand between Tasks 1 and 5 and green, and no artifact names them

**The claim under attack.** Plan 031 Task 1 **Step 2** and Task 5 **Step 2**, which enumerate the class-gate edits
each task must make:

> **Step 2 (RED — the gate first).** Add `"memory.WithMaxGroupMembers"` to `sizingConformanceKeys` and its
> conformance row to the **`fixed`** arm, and bump the executable counts this moves:
> `require.Len(t, tests, 19 → 20)` at `:753`.

and Spec 017 §6 **AC-8** item 5, which enumerates the same set:

> **Ten sites state a count, and two of them are executable.** … **`:335` — `require.Equal(t, 27, methodCount, …)`**
> and **`:753-754` — `require.Len(t, tests, 19, …)`**. The last two fail the suite, not a reader.

**There are FOUR executable assertions in that file, not two, and TWO of them are exact-map comparisons the bundle
never mentions.**

```
$ grep -nE 'require\.(Len|Equal)\(t, (tests, [0-9]+|[0-9]+, methodCount|wantArms, gotArms|map\[string\]int\{)' \
    sizing_option_class_gate_test.go
363:	require.Equal(t, 27, methodCount, "the excluded-method count moved — re-derive Spec 016 §2.0's "+
807:	require.Len(t, tests, 19,
853:	require.Equal(t, wantArms, gotArms,
859:	require.Equal(t, map[string]int{"fixed": 12, "rejects": 1, "safe": 6}, byArm,
```

**`:853` compares against a 19-entry literal:**

```
$ sed -n '826,846p' sizing_option_class_gate_test.go
	wantArms := map[string]string{
		"endpoint.WithMaxInFlight":           "fixed",
		"endpoint.WithConcurrency":           "fixed",
		…
		"(manual) channel.QueueChannel.Poll": "safe",
	}
$ awk '/wantArms := map\[string\]string\{/,/^\t\}/' sizing_option_class_gate_test.go | grep -c '":'
19
```

**Both are `require`, so both ABORT the test.** Adding `memory.WithMaxGroupMembers` and `sql.WithMaxGroupMembers`
as conformance rows makes `gotArms` a **21**-entry map compared against a **19**-entry `wantArms` (`:853` fails and
returns), and `byArm` `{"fixed": 14, "rejects": 1, "safe": 6}` compared against the asserted
`{"fixed": 12, …}` (`:859`). **Task 1 cannot reach green. Task 5 cannot reach green.** Global constraint 8 makes
each task a green unit before its commit, so the plan is unexecutable as written at its very first task.

**The bundle uses `wantArms` as a PROBE TARGET and never as an EDIT TARGET.** This is the sharp part: the map is
not invisible to the bundle — it is *named*, in exactly one role, in exactly the place that makes the omission
easy to miss.

```
$ grep -rn "wantArms\|byArm\|pairwise" docs/plans/031-group-member-bounds.md docs/specs/017-*.md docs/adrs/0033-*.md
docs/plans/031-group-member-bounds.md:1112:      | C | flip one conformance row's `arm` | half 2 reports the **pairwise** mismatch, not merely a count mismatch |
docs/plans/031-group-member-bounds.md:1121:| B9-3 | half 2's pairwise `arm` mapping | probe C | replace the map with a count map ⇒ a pairwise swap passes …
docs/specs/017-group-member-bounds.md:1826:… flip one conformance row's `arm` (half 2 must report the pairwise mismatch). …
```

Task 9 Step 7 probe C flips a row's `arm` **to prove the map fires**, and B9-3 mutation-proves that the map is a
mapping rather than a count map. **Both treat the map as a fixed piece of apparatus to be probed. Neither says the
increment must edit it** — and Task 9 runs **eighth**, six tasks after the commit that first breaks it.

**It predates Plan 030, so this is not fallout from a moving tree.**

```
$ git log --oneline -S 'wantArms' -- sizing_option_class_gate_test.go
e473deb  fix(http): label the replay-buffer cost figures by metric, and tighten the class gate
```

`e473deb` is a **Plan 029-era** commit, an ancestor of `2b2dec1` where revision 1 measured. **`wantArms` and
`byArm` were on `main` when round 1 ran, and every round since has re-derived the gate's edit list without them.**
That is what makes this a BLOCKER rather than another stale-coordinate MINOR: R4-2's class is *"the tree moved";
this one is *"the list was never complete"*.

### The design question inside the BLOCKER — the assertion tells its implementer to amend a DELIVERED spec

**`:853`'s failure message is not neutral. It is an instruction, and it points outside this increment.**

```
$ sed -n '853,862p' sizing_option_class_gate_test.go
	require.Equal(t, wantArms, gotArms,
		"Spec 016 §2.1's arm table and §6 AC-5 fix EVERY key's arm, not just the per-arm counts: 12 class "+
			…
			"Spec 018), 6 safe (4 AST + 2 manual). Moving a row between arms is a "+
			"SPEC change — update §2.1 and §6 AC-5, do not just edit this map")
	require.Equal(t, map[string]int{"fixed": 12, "rejects": 1, "safe": 6}, byArm,
		"the per-arm counts follow from wantArms above; a mismatch here means wantArms itself drifted "+
			"from Spec 016 §2.1's split, now 12/1/0/6. …")
```

**"Moving a row between arms is a SPEC change — update §2.1 and §6 AC-5, do not just edit this map."**

[Spec 016](../specs/016-sizing-option-bounds.md) is **DELIVERED** (Plan 029, merged). Plan 031 **adds two rows to
the `fixed` arm**, which is a change to §2.1's arm table and §6 AC-5's tabulation — and **no task in Plan 031 opens
Spec 016.** `grep -n "016-sizing-option-bounds" docs/plans/031-group-member-bounds.md` returns only *"builds on"*
citations, never a Files entry.

So an implementer meeting the red at Task 1 Step 2 has three options, and the plan authorises none of them:

| Option | Why it is wrong as things stand |
|---|---|
| Edit `wantArms`/`byArm` and stop | The assertion message explicitly forbids it (*"do not just edit this map"*), and the divergence between the gate and Spec 016 §2.1 becomes silent — the exact drift the map exists to catch |
| Amend Spec 016 §2.1 / §6 AC-5 ad hoc | An unbudgeted edit to a **delivered** spec, made at the keyboard, with no ADR and no traceability trailer. CLAUDE.md forbids committing work whose governing artifact link is missing |
| Escalate | Correct, but it stalls the increment's **first** task on a question the design phase should have answered |

**This is a decision the bundle must take, not an instruction it can sharpen.** Three of this project's standing
rules bear on it: *fix the class, not the instance*; *fold into all three artifacts*; and *a plan must reference
the spec it implements*.

**Required fix.**

1. **Add `wantArms` and the `byArm` literal to Plan Task 1 Step 2 and Task 5 Step 2 as explicit enumerated
   edits** — by *what they are*, not by coordinate (see R4-2). State that both are `require`, so an unedited map
   aborts the test rather than merely failing an assertion.
2. **Add both to Task 9 Step 3's site table**, and correct that table's *"ten in all / two executable"* header to
   the re-derived figures.
3. **Add both to Spec §6 AC-8** — item 5's *"two of them are executable"* is the sentence that is wrong.
4. **Add both to ADR D-AL**, whose subject is *how the gate is extended by hand*. D-AL currently reasons about the
   key set and the arm counts and is silent on the two assertions that enforce them.
5. **Decide the Spec 016 question, and give it a task.** The recommended disposition is the one
   [Plan 032](032-byte-cap-ceilings.md) already set on this exact question when its own audit hit it: **Plan 031
   takes unconditional ownership of a Spec 016 §2.1 + §6 AC-5 fold-back task**, and that task **re-derives the arm
   table from the tree at fold-back time** rather than transcribing a pre-computed count. A fold-back task that
   carries a number written six tasks earlier is R4-2 again, one artifact over.

---

## MAJOR R4-2 — every gate figure is stale, two are wrong in composition, and the remedy is a METHOD change

**The claim under attack.** All three artifacts assert their gate figures are re-derived:

> **Every structural claim below is re-derived against `d2c69fe`** — current `main`, **post-Plan-030**.
> *(Spec 017 status block; ADR 0033's twin reads "All structural claims re-derived at `d2c69fe` (current `main`,
> post-Plan-030)"; Plan 031's Global constraint 12 says "Re-derive, never transcribe, every figure about
> `sizing_option_class_gate_test.go`.")*

**`d2c69fe` is no longer the tree, and it is not `main`.**

```
$ git log --oneline -3
a306241  docs: accept the byte-cap ceiling design and record the delivery gates
f39725d  fix(http): bound the three byte caps at a representability ceiling
f106d82  docs: revise the group-member bound design after audit round 3

$ git show --stat --oneline f39725d | grep sizing
 sizing_option_class_gate_test.go | 237 +++++++++++++++----------

$ git diff --stat d2c69fe..HEAD -- '*.go'
 adapter/http/config_sizing_bounds_test.go | 286 ++++++++++++++++++++++++++++++
 adapter/http/errors.go                    |  38 ++--
 adapter/http/exchange_test.go             |  19 +-
 adapter/http/helpers.go                   |  63 ++++++-
 adapter/http/options.go                   | 121 +++++++++++--
 sizing_option_class_gate_test.go          | 237 +++++++++++++++----------
```

Plan 032 landed at `f39725d`. **Exactly two areas moved: `adapter/http` and the class gate.** Every other
coordinate in the bundle — `routing`, `adapter/memory`, `adapter/database/sql`, `endpoint`, `message.go` — is
untouched and still correct, which is why this is a MAJOR and not a rewrite.

### The coordinate drift, measured

```
$ grep -n 'func hasIntOrInt64Param\|func isIntOrInt64\|func scanSizingParamRepo\|parser.ParseFile\|assert.Equal(t, want, found\|require.Equal(t, 27\|require.Len(t, tests\|th key, positional' sizing_option_class_gate_test.go
243:func hasIntOrInt64Param(ft *ast.FuncType) bool {
259:func isIntOrInt64(expr ast.Expr) bool {
282:func scanSizingParamRepo(t *testing.T, root string) (funcs []string, methodCount int) {
308:		f, perr := parser.ParseFile(fset, rel, src, parser.SkipObjectResolution)
349:	assert.Equal(t, want, found, "the AST-discovered set of exported sizing-shaped functions must match "+
363:	require.Equal(t, 27, methodCount, "the excluded-method count moved — re-derive Spec 016 §2.0's "+
745:				rl, err := resilience.NewTokenBucket(1, math.MaxInt) // burst is the 17th key, positional
807:	require.Len(t, tests, 19,

$ git show 6865886:sizing_option_class_gate_test.go | grep -n 'func hasIntOrInt64Param\|func isIntOrInt64\|func scanSizingParamRepo\|parser.ParseFile\|assert.Equal(t, want, found\|require.Equal(t, 27\|require.Len(t, tests\|th key, positional'
215:func hasIntOrInt64Param(ft *ast.FuncType) bool {
231:func isIntOrInt64(expr ast.Expr) bool {
254:func scanSizingParamRepo(t *testing.T, root string) (funcs []string, methodCount int) {
280:		f, perr := parser.ParseFile(fset, rel, src, parser.SkipObjectResolution)
321:	assert.Equal(t, want, found, "the AST-discovered set …
335:	require.Equal(t, 27, methodCount, …
691:				rl, err := resilience.NewTokenBucket(1, math.MaxInt) // burst is the 17th key, positional
753:	require.Len(t, tests, 19,
```

Every citation in the right-hand column appears somewhere in the bundle. The full inventory of stale coordinates:

| Cited as | Cited in | Truth at `f39725d` |
|---|---|---|
| `assert.Equal(t, want, found, …)` at `:321-324` | Plan `:257`, `:539`; Spec `:263`, `:1722`; ADR `:1107`, `:1338` | `:349-352` |
| `require.Equal(t, 27, methodCount, …)` at `:335` | Plan `:346`, `:1085`, `:1092`; Spec `:1746`; ADR `:1143` | `:363` |
| `require.Len(t, tests, …)` at `:753` / `:753-754` | Plan `:344`, `:689`, `:1086`; Spec `:1746-1747`; ADR `:1143` | `:807-808` |
| `go/parser` / `parser.ParseFile` at `:280` | Plan `:561`; Spec `:913`, `:1576`; ADR `:1194` | `:308` |
| `scanSizingParamRepo` at `:254-300` | Plan `:570` | `:282-328` |
| `hasIntOrInt64Param` at `:215` / `:215-225` | Spec `:252`; ADR `:99` | `:243-253` |
| `isIntOrInt64` at `:231` / `:231-243` | Spec `:253`; ADR `:99` | `:259-271` |
| the ROOT-MODULE IMPORT BOUNDARY package list at `:107-108` | Plan `:1068`, `:1082`; Spec `:285`, `:1741`, `:1745`; ADR `:1143-1144` | the limitation is `:133-140`; the package list is `:135-136` |
| *"Recv == nil yields 17; ANY FuncDecl yields 44"* at `:83-85` | Plan `:1080`; Spec `:1745` | `:111-113` |
| `postgresGroupDialect.AddMember` in the header at `:84-86` | Plan `:1092`; Spec `:1752`; ADR `:1151` | `:113` |
| *"the 27 excluded methods"* at `:92` | Plan `:1081`; Spec `:1745` | `:120` |
| *"cross-check the full 17"* / *"unchanged at 17"* at `:176` / `:210` | Plan `:1083`; Spec `:1745` | `:204` / `:238` |
| the `assert.Equal` **message** at `:322` | Plan `:1084`; Spec `:1745` | `:350` |
| the arm arithmetic at `:38` | Plan `:1078`; Spec `:1744` | `:45` |
| the per-arm counts at `:47` / `:55` / `:61` | Plan `:1079`; Spec `:1744-1745` | `:26`, `:31`, `:37`, `:44` — and `:57`, `:61`, `:89` |
| *"burst is the 17th key, positional"* at `:691` | Plan `:1088`; Spec `:1747` | `:745` |

**Only `:19` — *"Every one of the 17 AST-discovered keys"* — survives its own coordinate, by coincidence.**

🔴 **TWO OF THESE ARE ROUND 3'S OWN NEW-9 FIXES.** `hasIntOrInt64Param` and `isIntOrInt64` were *the* corrected
citations of NEW-9 — ADR `:99` reads *"`:231`, reached from `hasIntOrInt64Param` at `:215` — **not `:191-202` as
revisions 1-3 cited**, audit NEW-9"*. **That correction is now itself wrong.** The class has now returned in four
consecutive rounds, and for the first time it returned **through the fix**, not merely beside it.

### The two figures that are wrong in COMPOSITION, not merely in coordinate

**These do not survive a coordinate refresh, and that is what makes R4-2 a MAJOR.**

**(a) The arm arithmetic.** Spec AC-8 item 2, ADR D-AL `:1125` and Plan Task 9 Step 3 row 2 all read some form of:

> Arm totals become **11 fixed + 1 rejects + 3 deferred + 6 safe = 21 rows = 19 AST keys + 2 manual rows.**

**Post-032 the `deferred` arm is EMPTY and TOMBSTONED**, and its three `int64` byte caps moved into `fixed`:

```
$ sed -n '26,45p' sizing_option_class_gate_test.go
//       - "fixed"   (12) — … 9 were bounded by Spec 016 / Plan 029; the 3 msghttp byte caps
//                          joined them from "deferred" at Spec 018 / Plan 032.
//       - "rejects"  (1) — msghttp.WithSuccessStatus. …
//       - "deferred" (0) — no members as of Plan 032; see Spec 018. The arm is
//                          retained so a future knob with a genuinely deferred
//                          remedy has it … NOTE: byArm below is built by COUNTING,
//                          so an empty arm has NO KEY there — it is absent, not zero.
//       - "safe"     (6) — accepts math.MaxInt AND its product is usable.
//     12 + 1 + 6 = 19 rows = 17 AST keys + 2 manual rows.
```

**The file now reads `12 + 1 + 6 = 19`. The true post-increment partition is `14 fixed + 1 rejects + 0 deferred +
6 safe = 21`.** The bundle's `= 21` total survived **by coincidence** — `11 + 1 + 3 + 6` and `14 + 1 + 0 + 6` both
total 21 — which is precisely why a total is not a partition. *This is the project's `43 ≠ 43` lesson landing on
this bundle for the second time: **reconcile by name, never by count.***

**And there is an executable consequence:** the `byArm` literal at `:859` is `{"fixed": 12, "rejects": 1,
"safe": 6}` and must become `{"fixed": 14, …}` — with **no `"deferred": 0` key**, because `byArm` is built by
counting and the file's own comment warns that adding a zero entry **fails** the assertion. An implementer
following the bundle's *"3 deferred"* would add one.

**(b) The *"`fixed` ⇒ `1<<30`"* rule is FALSE as stated.** ADR D-AL's literal table and Spec AC-8's box both say:

| Arm | Literal |
|---|---|
| `fixed` (9→11), `rejects` (1) | `1<<30` |
| `deferred` (3) | `1<<62` |
| `safe` (6) | `math.MaxInt` |

**`fixed` now contains three `int64` rows that keep `1<<62`**, and for which `1<<30` would be **accepted**:

```
$ sed -n '560,575p' sizing_option_class_gate_test.go
// 🔴 THEY KEEP THE 1<<62 LITERAL — see the file header's dimension 2.
// 1<<30 is BELOW byteCapCeiling and would be ACCEPTED, turning every
// require.ErrorIs below into a failure; on 386, math.MaxInt lands
// exactly ON the ceiling and would be accepted too. The literal is
// chosen by the PARAMETER TYPE within a reject arm, not by the arm.
	{
		key: "msghttp.WithMaxBodyBytes",
		arm: "fixed",
		…
			"msghttp: max body bytes out of range: msghttp.WithMaxBodyBytes: 4611686018427387904 not in [1, 2147483647]")
```

`byteCapCeiling = math.MaxInt32 = 2,147,483,647`, which is **above** `1<<30 = 1,073,741,824`. The file now states
the rule two-dimensionally: *the arm fixes the required property (rejected vs accepted); **within a reject arm the
PARAMETER TYPE chooses the literal**.*

**The instruction the bundle gives is still right — the two new `int`-typed options must assert `1<<30` — but its
stated REASON is falsified.** That matters because the reason is what a future increment reuses: an increment
adding an `int64` knob to `fixed` and following *"`fixed` ⇒ `1<<30`"* ships a row that is **accepted** and whose
every `require.ErrorIs` fails.

### 🔴 The required fix is a METHOD change — re-deriving the citations is NOT the remedy

**This is the finding's whole point, and it is stated as an instruction rather than a preference.**

Round 3's NEW-9 already said *"round 4's fix should be a mechanical sweep of every `file:line` in the bundle, not
another list of corrected instances."* Revision 4 did the sweep — and **the sweep went stale in one commit**,
falsifying two of its own three results. **A hand-typed list of coordinates against a file another increment is
rewriting cannot be kept true by being retyped more carefully.** The fourth consecutive round of this class is the
evidence that the instance-level fix does not work.

1. **Generate Task 9's site table by running a script over `sizing_option_class_gate_test.go` at HEAD, and paste
   its output** — the table becomes *derived evidence with a timestamp*, not an asserted list.
2. **Put the script IN the task**, so the next increment **reruns it** rather than editing a list. This is the
   same shape as the docs-link gate in CLAUDE.md: a command whose output is the finding.
3. **Do the same for every class-gate coordinate in all three artifacts.** Where a coordinate is load-bearing,
   replace the bare `:NNN` with the **grep that locates it**, which cannot go stale. Where a *number* is genuinely
   needed (a count, a partition), state the tree it was measured on **in the same sentence**.
4. **Correct the two composition errors** — the arm arithmetic to `14 + 1 + 0 + 6 = 21` (with the *"do not add
   `deferred: 0`"* warning carried across) and the literal rule to *"the arm fixes the property; the parameter
   type chooses the literal within a reject arm"*.
5. **Fix the three artifact headers** to name the tree actually measured — `f39725d` — and to stop asserting it is
   *"current `main`"*, which it is not.

---

## MAJOR R4-3 — the crashed-lease window is understated ~2×, and the wrong figure is scheduled into PUBLIC GODOC

**The claim under attack**, in the four places revision 4 put it. Spec §3.5's box:

> **but if the releasing process CRASHES mid-release the lease is held until it expires**: the default
> `WithGroupLeaseTTL` is **5 minutes** (`adapter/database/sql/groupstore.go:22`). For that window, arrivals for
> that key are rejected **transiently**, which under `RetryPolicy{}` is a zero-delay busy-wait against the
> database for **up to a lease TTL**.

Spec §8 item 9, ADR D-AF consequence 2 and the ADR Consequences bullet all carry the same *"up to `leaseTTL`,
default 5 minutes"* bound. **And Plan Task 1 Step 7 / Spec §4 item 1 schedule it into `sql.WithMaxGroupMembers`'s
own godoc:**

> **For `sql` additionally:** that the claim window's tail is a **crashed releaser's lease TTL (default 5m)**,
> drained by the default reaper sweep.

**That is a public API contract sentence, and the number in it is wrong.**

**Two independent terms compose, and the bundle counts only one.**

**Term 1 — eligibility.** A lease stamped at `t₀` becomes reap-eligible at `t₀ + leaseTTL`. Verified — the
dialect's `ExpiredGroups` first `WHERE` arm:

```
$ sed -n '275,281p' adapter/database/sql/postgres/groupdialect.go
	rows, err := q.QueryContext(ctx,
		fmt.Sprintf(`SELECT group_key, created_at FROM %s
WHERE (locked_by IS NOT NULL AND locked_at <= %s - $2)
   OR ($1 AND locked_by IS NULL AND created_at < $3)
```

**Term 2 — discovery. The reaper does not tick at `t₀ + leaseTTL`; it ticks on a FIXED CADENCE that has nothing
to do with `t₀`.**

```
$ sed -n '538,548p' routing/aggregator.go
	interval := a.reapInterval()
	if interval <= 0 {
		<-ctx.Done()
		return ctx.Err()
	}

	ticker := a.cfg.clock.NewTicker(interval)
	defer ticker.Stop()

$ sed -n '556,565p' routing/aggregator.go
// reapInterval is the reaper's tick cadence: the min positive of
// WithGroupTimeout and the store's RecoverInterval(), or 0 (no sweep) if
// neither is set.
func (a *Aggregator) reapInterval() time.Duration {
	interval := a.cfg.timeout
	if storeInterval := a.store.RecoverInterval(); storeInterval > 0 && (interval <= 0 || storeInterval < interval) {
		interval = storeInterval
	}
	return interval
}

$ grep -n 'func (s \*GroupStore) RecoverInterval' adapter/database/sql/groupstore.go
348:func (s *GroupStore) RecoverInterval() time.Duration { return s.leaseTTL }

$ grep -n 'defaultGroupLeaseTTL =' adapter/database/sql/groupstore.go
22:const defaultGroupLeaseTTL = 5 * time.Minute
```

**In the default configuration** — no `WithGroupTimeout` — `reapInterval() == RecoverInterval() == leaseTTL`, and
the ticker is created when `Run` starts, at a phase unrelated to any lease. **Discovery is therefore the first
tick at or after `t₀ + leaseTTL`.**

| | |
|---|---|
| Ticks land at | `T₀ + k·leaseTTL`, for the reaper's own start time `T₀` |
| The group becomes eligible at | `t₀ + leaseTTL` |
| It is discovered at | the first tick `≥ t₀ + leaseTTL` |
| **Best case** | `t₀ + leaseTTL` — eligibility lands exactly on a tick |
| **Worst case** | `t₀ + 2·leaseTTL − ε` — eligibility lands just *after* a tick, so it waits a full further interval |

**At the shipped defaults that is up to ≈ 10 minutes, not 5.** The busy-wait runs for the whole of it.

*(And it presumes `go agg.Run(ctx)` is running at all. Spec §1.2.1 already states that a durable store makes `Run`
**required**; if it is not started the window has no upper bound whatsoever. The godoc sentence should not imply a
bound that a caller who skipped `Run` does not have.)*

### The per-iteration cost is also mis-stated — and D-AF's own sibling decision already spells out the true one

The bundle calls the window *"a **zero-delay busy-wait against the database**"*. **D-AM's own rejection argument,
twenty decisions earlier in the same ADR, states the real per-iteration cost:**

```
$ grep -n 'each iteration is a full rolled-back' docs/adrs/0033-group-member-bounds.md
326:… For `sql` each iteration is a full rolled-back `AddMember` transaction **plus** a `SchemaExists` probe
   (D-AP), forever: **strictly worse than the unbounded status quo.**
```

Enumerated against the shipped code, one iteration of the leased-arm retry is:

| # | Statement | Evidence |
|---|---|---|
| 1 | `BEGIN` | `pgRunInTx` / `mysqlRunInTx` / `withImmediateConn` |
| 2 | the group upsert — **which TAKES THE GROUP-ROW X-LOCK** | `postgres/groupdialect.go:107-110`, `mysql:93-96`; sqlite takes the **database-wide** `BEGIN IMMEDIATE` lock (`sqlite/groupdialect.go:62`) |
| 3 | the member upsert | `postgres/groupdialect.go:115-119` and siblings |
| 4 | **the NEW `COUNT(*)`** — D-AF's own added cost | `pgCountMembers` (`:373`), `mysql:358`, `sqlite:375` |
| 5 | the live member `SELECT` | `pgSelectMembers` (`:383`) and siblings |
| 6 | `ROLLBACK` | the overflow return unwinds the tx |
| 7 | **`classifyQueryErr`'s `SchemaExists` probe** — a *further* round-trip on the error path | `adapter/database/sql/groupstore.go:91-95`, reached from `:273` |

`× WithConcurrency(N)` goroutines, **all contending on the very group row the recovery path must lock to drain
it.** That is not a busy-wait against the database; it is a **full write transaction per iteration, taking the
lock the remedy needs**, for up to ten minutes.

**The disposition STAYS TRANSIENT — that is correct.** Spec §8 item 9's reasoning is sound and this finding does
not touch it: the retry genuinely succeeds once the reaper drains the group, and the alternative (classify an
expired lease permanent) dead-letters messages the default configuration is about to admit. **Only the magnitude
is wrong** — and it is wrong in the direction that flatters the decision the user is being asked to ratify.

**Required fix.** Restate the bound **with the derivation** — both terms, named — in **Spec §3.5's box**, **Spec §8
item 9**, **ADR D-AF consequence 2**, the **ADR Consequences bullet**, **and the mandated godoc text** (Plan
Task 1 Step 7 / Spec §4 item 1). Replace *"a zero-delay busy-wait against the database"* with the per-iteration
cost above, at `WithConcurrency(N)`, contending on the group row. **Keep the transient disposition and keep §8 item
9's reasoning; change only the number and the cost.**

---

## MAJOR R4-4 — the gate enforcing round 3's BLOCKER fix stops on a CORRECT tree

**The claim under attack.** Plan 031 Task 3 **Step 0**, the hard order gate **D-AT** exists to install:

> - [ ] **Step 0 (THE ORDER GATE — audit NEW-1, D-AT).** Confirm all **three** declarations are already on the
>   branch before writing a line:
>   `grep -n "defaultMaxGroupMembers" adapter/memory/groupstore.go adapter/database/sql/groupstore.go` must
>   return **two** hits and `grep -n "completionSizeCeiling" routing/aggregator.go` at least one. **If any is
>   missing, Tasks 1 and/or 5+6 have not landed — STOP and run them first.**

**On a correct tree that grep returns at least four, and most likely six.** Each of Tasks 1 and 5 Step 4 puts
**two** occurrences of the identifier into the **same file**:

> **Step 4 (GREEN — the store).** Add **two NAMED package constants, DECLARED IN `adapter/memory/groupstore.go`**
> … `const defaultMaxGroupMembers = 1 << 16` … the `maxGroupMembers` config field initialised **from the named
> default** (`cfg := groupStoreConfig{…, maxGroupMembers: defaultMaxGroupMembers}`) …

— the `const` declaration **and** the initialiser reference. That is two hits per file, **four** in total.

**And Step 7 adds a third per file.** Task 1 Step 7 requires *"**both constants** — shaped like `maxGroupsCeiling`'s
at `:55-62`"*, and a Go doc comment **begins with the identifier it documents**:

```
$ sed -n '55,62p' adapter/memory/groupstore.go
// maxGroupsCeiling is the upper bound WithMaxGroups accepts (Spec 016 §3.4).
…
const maxGroupsCeiling = 1 << 20
```

So the shape the plan mandates produces `// defaultMaxGroupMembers is …` as well — **three hits per file, six in
total.** Task 3 Step 5 then adds the cross-reference comment, which may add more.

**So the gate installed to enforce D-AT halts on the tree D-AT describes**, and its failure text —
*"Tasks 1 and/or 5+6 have not landed — STOP and run them first"* — sends the implementer to re-run tasks that
already landed correctly. **A gate whose only failure mode is a false positive is worse than no gate**, because
the first person to hit it will delete it.

**The defect is the assertion's SHAPE, and this project has a named lesson for it.** From
[`MEMORY.md`](../../CLAUDE.md): *"Assert the partition, not just the rows"*, and *"Fix the class, not the
instance"*. **A count is a proxy for the condition; the condition is *"each of these three declarations exists"*.**
Counting occurrences of an identifier that legitimately appears N times per file is a proxy that does not hold.

**And the ADR already describes the correct gate.** D-AT `:1346`:

> Task 3 opens with a hard **Step 0 order gate** that **greps for all three declarations** and stops if any is
> missing.

**"greps for all three declarations" is right. The plan does not implement it** — it greps for one identifier
across two files and counts. Two-of-three, inverted: the ADR has the correct rule and the plan has the defective
instance.

**Required fix.** **Assert the condition, not a count**, in **Plan Task 3 Step 0** *and* in **ADR D-AT `:1346`**
(which must name the assertion it is describing, so the plan cannot drift from it again):

```bash
grep -q "^const defaultMaxGroupMembers" adapter/memory/groupstore.go        || { echo "Task 1 has not landed"; exit 1; }
grep -q "^const defaultMaxGroupMembers" adapter/database/sql/groupstore.go  || { echo "Tasks 5+6 have not landed"; exit 1; }
grep -q "completionSizeCeiling" routing/aggregator.go                       || { echo "shipped constant missing"; exit 1; }
```

Three independent, per-declaration conditions; **no count anywhere**; and each failure message names the task that
is actually missing rather than a disjunction.

---

## MINOR R4-5 — the `checkRange` inventory is stale in both the number and the coordinate

**The claim under attack.** Spec §3.2 (`:422-425`) and Plan Task 5 Step 4 (`:694-696`) both enumerate the shipped
`checkRange` copies as **four**, at:

> `endpoint/helpers.go:97`, `routing/helpers.go:88`, `adapter/memory/helpers.go:54` and
> **`adapter/http/helpers.go:64`** — and gains a fifth, unexported, identical copy.

**The stated command now returns five, and the `adapter/http` coordinate has moved.**

```
$ grep -rn 'func checkRange' --include='*.go' .
adapter/memory/helpers.go:54:func checkRange(sentinel error, site string, n, lo, hi int) error {
adapter/http/helpers.go:73:func checkRange(sentinel error, site string, n, lo, hi int) error {
adapter/http/helpers.go:115:func checkRangeInt64(sentinel error, site string, n, lo, hi int64) error {
routing/helpers.go:88:func checkRange(sentinel error, site string, n, lo, hi int) error {
endpoint/helpers.go:97:func checkRange(sentinel error, site string, n, lo, hi int) error {
$ grep -rn 'func checkRange' --include='*.go' . | wc -l
       5
```

[Plan 032](032-byte-cap-ceilings.md) added **`checkRangeInt64`** at `adapter/http/helpers.go:115` — the `int64`
twin the three byte caps needed — and pushed `checkRange` from `:64` to `:73`.

**Both statements in the bundle are now false together**, which is the tell: *"the command returns four"* and
*"the fourth is at `:64`"*. The count is only wrong because the grep is under-qualified — `checkRangeInt64` matches
`func checkRange` on a prefix.

**Neither error is load-bearing for the design** — the fifth copy Plan 031 adds is an `int` copy modelled on
`adapter/memory/helpers.go:54`, which is unchanged. It is a MINOR. **It is recorded because it is the same class as
R4-2 reached through a different file**, and because a plan that pastes a command and a number that disagree
teaches an implementer to trust neither.

**Required fix.** Correct the coordinate to `adapter/http/helpers.go:73`, and **qualify the grep so the stated
command and the stated number agree** — `grep -rn 'func checkRange(' --include='*.go' .` → **four**, or keep the
loose grep and say **five, one of which is `checkRangeInt64`**. Either is fine; **stating a command and a number
that contradict each other is not.**

---

## MINOR R4-6 — "bounded now that the rejection is terminal" is false for the arm revision 4 created

**The claim under attack.** ADR 0033 Consequences, `:1459-1461`:

> - **Every overflow rejection on the `sql` path costs an extra `SchemaExists` round-trip** through
>   `classifyQueryErr` (D-AP / audit m-6). **Bounded now that D-AM makes the rejection terminal**; it was unbounded
>   under revision 1.

and its spec twin, §8's backlog list, `:2034-2036`:

> - **`classifyQueryErr` runs a `SchemaExists` probe on every dialect error** (§1.3 item 4, §3.6.3). **Bounded now
>   that the rejection is terminal**; still an avoidable round-trip on a hot error path across the whole `sql`
>   adapter, not just the group store.

**Revision 4 gave `sql` a transient arm, so the premise no longer holds for every rejection.** The bundle's own
Task 6 Step 4 table:

| Group row | Classification |
|---|---|
| `locked_by IS NULL` — **not leased** | `msgin.Permanent(…)` — **terminal** ✔ the bullet is true here |
| `locked_by IS NOT NULL` — **leased** | the bare `fmt.Errorf(…)`, **transient** ✘ **not terminal** |

A **leased** rejection is Nacked and retried. Every retry re-enters `sql.GroupStore.Add`, fails, and pays the probe
again:

```
$ sed -n '271,275p' adapter/database/sql/groupstore.go
	rows, err := s.dialect.AddMember(ctx, s.db, s.table, key, msgID, seq, headers, payload)
	if err != nil {
		return nil, s.classifyQueryErr(ctx, err)
	}
$ sed -n '91,95p' adapter/database/sql/groupstore.go
func (b groupBase) classifyQueryErr(ctx context.Context, err error) error {
	if exists, probeErr := b.dialect.SchemaExists(ctx, b.db, b.table); probeErr == nil && !exists {
		return b.schemaNotReady()
	}
	return err
}
```

**So the probe is paid on every iteration of R4-3's window** — item 7 of R4-3's per-iteration cost table is this
same probe. The word **"Every"** in the bullet is what makes it self-contradictory: it correctly says *every*
rejection pays the probe, and then bounds it with a property only *some* rejections have.

**This is two-of-three in the other direction from NEW-8** — the sentence appears in the ADR **and** the spec, and
both are stale for the same reason. It is a MINOR because the exposure is already recorded under R4-3; it is
recorded separately because a **Consequences** bullet is what a future reader trusts for *"what did this cost us"*.

**Required fix.** **Scope the bullet to the not-leased arm** in both places, and **cross-reference §8 item 9** for
the leased arm's exposure:

> Bounded for a **not-leased** rejection, which D-AM makes terminal. **A LEASED rejection is transient**, so it
> pays the probe on every retry for the duration of the claim window — up to the crashed-lease bound of §8 item 9.

---

## MINOR R4-7 — all three artifact headers assert re-derivation against a commit that is no longer `main`

> ⚠️ **RESTATED, NOT QUOTED — read the provenance box first.** The coordinator's hand-off folded R4-7's remedy
> into R4-2's third bullet **without an R4-7 label**, because the two findings take the *same edit*. The
> recorder's first draft consequently listed R4-7 as *"NOT RELAYED"* and left it open; **the coordinator has since
> confirmed the labelling error and supplied the finding's wording.** The claim and required fix below are the
> **coordinator's restatement of the auditor**, unlike every other finding in Part 2, which is verbatim or
> relayed-and-re-derived. **The evidence below was re-derived first-hand**, like all the rest.

**The claim under attack**, as relayed:

> *"Three artifact headers assert re-derivation against a commit that is no longer `main`"* — citing **Spec `:44`,
> ADR `:44` and Plan `:38`**.

**Re-derived against the artifacts as committed at revision 4.** Two of the three coordinates reproduce exactly:

```
$ git show HEAD:docs/specs/017-group-member-bounds.md | sed -n '44p'
  - **Every structural claim below is re-derived against `d2c69fe`** — current `main`, **post-Plan-030** — not

$ git show HEAD:docs/plans/031-group-member-bounds.md | sed -n '38p'
| **B-3** / **M-1** | Every figure about `sizing_option_class_gate_test.go` is **re-derived at `d2c69fe`**, post-Plan-030. …

$ git show HEAD:docs/adrs/0033-group-member-bounds.md | sed -n '55p'
  - **All structural claims re-derived at `d2c69fe`** (current `main`, post-Plan-030), not at `2b2dec1` where
```

> 🔴 **RE-DERIVED — the ADR's coordinate is `:55`, not `:44`.** ADR `:44` is the *"A ROUND 4 IS WARRANTED"*
> bullet; the re-derivation sentence is **eleven lines below it**. *The citation of the finding whose subject is a
> stale citation is itself off by eleven* — a further instance of the very class **R4-2** and **NEW-9** are about,
> and the fourth-round evidence that **no amount of care makes a hand-typed `file:line` stable.** Recorded, not
> smoothed over.

**Both halves of the sentence are false at `f39725d`.**

| Half | Why it is false |
|---|---|
| *"re-derived against `d2c69fe`"* | [Plan 032](032-byte-cap-ceilings.md) landed at `f39725d` and **rewrote 237 lines of `sizing_option_class_gate_test.go`**. Every class-gate figure and coordinate in the bundle was measured before that landing — which is exactly **R4-2** |
| *"current `main`"* | `main` is `a306241`. `d2c69fe` is **three commits behind** it |

**Required fix**, as relayed:

> *"Re-derive at `f39725d` and update the sentence in all three headers, **or** explicitly mark the class-gate
> figures as measured pre-`f733667` and pending re-derivation."*

**The first branch is the right one**, and it is the only one compatible with R4-2: marking the figures *"pending
re-derivation"* leaves an artifact that states a known-stale number and asks its reader to discount it, which is
how *"11 fixed + 1 rejects + 3 deferred"* survived four rounds in the first place. **A design bundle that carries
figures it says are wrong is not a bundle an implementer can execute.**

**Why this is a MINOR and not a MAJOR, despite touching all three artifacts.** The sentence is a *provenance
claim*, not a design claim: it says when the measurements were taken, and nothing downstream reads it. Its damage
is indirect — it is what let a reader trust the class-gate figures R4-2 falsifies. **R4-2 is the finding; R4-7 is
the label on the jar.** They share one edit, which is why the coordinator's brief merged them, and why closing
R4-2's header work closes this too.

---

## MINOR R4-8 — the `COUNT(*)` cost is stated engine-neutrally, and sqlite's is not the same cost

**The claim under attack.** D-AF consequence 1, Spec §3.6, the ADR Consequences bullet and Spec §8's backlog all
price the new count identically for all three engines:

> **`sql`'s `AddMember` costs ONE EXTRA `COUNT(*)` PER ADD** — on every add, not only on overflow, inside a
> transaction that already issues three statements.

**True everywhere; but on sqlite the transaction it sits inside holds a DATABASE-WIDE write lock.**

```
$ sed -n '52,66p' adapter/database/sql/sqlite/groupdialect.go
func withImmediateConn(ctx context.Context, q msginsql.Querier, fn func(conn msginsql.Querier) error) error {
	…
	if _, err := conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return err
	}

$ sed -n '374,380p' adapter/database/sql/sqlite/groupdialect.go
func sqliteCountMembers(ctx context.Context, q msginsql.Querier, mt, groupKey string) (int64, error) {
	var n int64
	err := q.QueryRowContext(ctx,
		fmt.Sprintf("SELECT count(*) FROM %s WHERE group_key = ?", mt), groupKey).Scan(&n)
	return n, err
}
```

| Engine | What the added `COUNT(*)` lengthens | Scope |
|---|---|---|
| postgres | the group-row lock held by `ON CONFLICT … DO UPDATE` (`:107-110`) | **per correlation key** — concurrent adds to *other* keys are unaffected |
| mysql | the group-row X lock taken by `ON DUPLICATE KEY UPDATE` (`:93-96`) | **per correlation key** |
| **sqlite** | **`BEGIN IMMEDIATE`'s whole-database write lock** (`:62`) | 🔴 **GLOBAL** — every add to **any** group, and every other writer in the process, waits |

**The scan is `O(members)` in the group** — it counts every row for the key, live and claimed, which by
construction is up to `maxGroupMembers`, **default 65,536**. On sqlite that scan is inside a global
write-serialisation window, on **every** add, not only on overflow.

**Spec §7.1 currently records sqlite's whole-database lock only as an ADVANTAGE:**

```
$ sed -n '1897p' docs/specs/017-group-member-bounds.md
| sqlite | **`BEGIN IMMEDIATE`**'s database-wide **write lock** … | whole-database — *stronger* |
```

It **is** stronger for correctness — that cell is right, and round 3 verified it. **The same property is a cost on
the throughput axis, and the bundle records only the benefit.** A reader pricing D-AF's reversal from §3.6 gets one
number for three engines whose worst case differs by the number of concurrently-active correlation keys.

**No design change is implied.** Enforcement (C) is still correct for sqlite, `BEGIN IMMEDIATE` is still the right
serializer, and the count is still forced to be a `COUNT(*)`. This is a **disclosure** finding: the price is
recorded once, engine-neutrally, for a cost that is not engine-neutral.

**Required fix.** **One sentence per engine** in **Spec §3.6** (where the cost is priced) and in the **ADR
Consequences bullet** (where a future reader looks up what it cost), naming sqlite's window as global and the scan
as `O(members)`. Optionally cross-reference §7.1 so the correctness advantage and the throughput cost are read as
two faces of the same property rather than as two claims.

---

## What I checked and found CLEAN — newly verified this round

Recorded so a round 5 does not re-derive it. Everything below was attacked in round 4 and survives.

**Rounds 1, 2 and 3's clean lists still hold.** Nothing in revision 4 disturbed §1.1's four-release-path table,
§1.4's two-mechanism analysis, the quadratic claim, §3.4a's cap-check placement and its three mutants, §3.5's
boundary arithmetic, D-AM's permanent-arm trace, §3.3a's `group == nil` compatibility arm, AC-6's
no-test-grows-a-group constraint, AC-10's two-probe design, Task 10 Step 3b's `GOARCH=386` gate, D-AS's shape, or
D-AR's scope note. Those are earlier rounds' findings and are not re-listed.

### 🔴 EVERY CODE CLAIM D-AF's REVERSAL RESTS ON — verified, one by one

**This is the check that matters most this round, and it is deliberately exhaustive rather than sampled.** D-AF's
reversal is the only *decision* revision 4 changed; it is the reason a fourth round was called; and it rests on a
chain of claims about code in three leaf modules that **no artifact in the bundle can execute**. A single false
link would make the reversal wrong in the direction that *looks* safe. **Every link was re-run. All hold.**

**1. The three `*CountMembers` helpers exist, with an identical signature, in all three dialects.**

```
$ grep -rn "func .*CountMembers" adapter/database/sql/{postgres,mysql,sqlite}/groupdialect.go
postgres/groupdialect.go:373:func pgCountMembers(ctx context.Context, q msginsql.Querier, mt, groupKey string) (int64, error) {
mysql/groupdialect.go:358:func mysqlCountMembers(ctx context.Context, q msginsql.Querier, mt, groupKey string) (int64, error) {
sqlite/groupdialect.go:375:func sqliteCountMembers(ctx context.Context, q msginsql.Querier, mt, groupKey string) (int64, error) {
```

**Identical in signature and in shape**, and each takes a `msginsql.Querier` — so it composes with the `tx`/`conn`
the dialect already holds, exactly as D-AF claims. The coordinates the bundle cites (`:373`, `:358`, `:375`)
**reproduce exactly**.

**2. NONE of the three carries a `claimed_epoch` predicate — the load-bearing claim, verified by reading all three
bodies rather than one.**

```
$ sed -n '373,379p' adapter/database/sql/postgres/groupdialect.go
func pgCountMembers(ctx context.Context, q msginsql.Querier, mt, groupKey string) (int64, error) {
	var n int64
	err := q.QueryRowContext(ctx,
		fmt.Sprintf(`SELECT count(*) FROM %s WHERE group_key = $1`, mt), groupKey).Scan(&n)
	return n, err
}
$ sed -n '358,364p' adapter/database/sql/mysql/groupdialect.go
		fmt.Sprintf("SELECT count(*) FROM %s WHERE group_key = ?", mt), groupKey).Scan(&n)
$ sed -n '375,381p' adapter/database/sql/sqlite/groupdialect.go
		fmt.Sprintf("SELECT count(*) FROM %s WHERE group_key = ?", mt), groupKey).Scan(&n)

$ grep -c 'claimed_epoch' <(sed -n '373,379p' adapter/database/sql/postgres/groupdialect.go) \
    <(sed -n '357,363p' adapter/database/sql/mysql/groupdialect.go) \
    <(sed -n '374,380p' adapter/database/sql/sqlite/groupdialect.go)
0
0
0
```

**`WHERE group_key = ?` and nothing else.** All three count **live + claimed**, which is precisely what the
reversal needs, and **zero new SQL is required** — D-AF's strongest practical claim is true.

**3. Each has exactly ONE existing caller, and it is that dialect's own `SettleGroup` — so adding an `AddMember`
caller does not disturb existing behavior.**

```
$ grep -rn "CountMembers(ctx" adapter/database/sql/{postgres,mysql,sqlite}/groupdialect.go
postgres/groupdialect.go:196:		remaining, err := pgCountMembers(ctx, tx, mt, groupKey)
mysql/groupdialect.go:192:		remaining, err := mysqlCountMembers(ctx, tx, mt, groupKey)
sqlite/groupdialect.go:208:		remaining, err := sqliteCountMembers(ctx, conn, mt, groupKey)

$ sed -n '179,200p' adapter/database/sql/postgres/groupdialect.go   # the enclosing func
func (postgresGroupDialect) SettleGroup(ctx context.Context, q msginsql.Querier, table, groupKey, lockedBy string, epoch int64) (bool, error) {
	…
		remaining, err := pgCountMembers(ctx, tx, mt, groupKey)
```

`mysql:192` sits inside `mysqlGroupDialect.SettleGroup` (`:177`) and `sqlite:208` inside
`sqliteGroupDialect.SettleGroup` (`:193`). **One caller each, all three inside a group transaction already** —
so the helper is already proven to work in the transactional context `AddMember` will call it from. The
coordinates the bundle cites (`postgres:196`, `mysql:192`, `sqlite:208`) **reproduce exactly**.

**4. The `locked_by` read really is available at ZERO extra round-trips — verified per dialect, because the
statement differs per dialect and the bundle's table asserts three different edits.**

```
$ sed -n '105,112p' adapter/database/sql/postgres/groupdialect.go
		if err := tx.QueryRowContext(ctx,
			fmt.Sprintf(`INSERT INTO %s (group_key, created_at, epoch) VALUES ($1, %s, 0)
ON CONFLICT (group_key) DO UPDATE SET group_key = EXCLUDED.group_key
RETURNING created_at`, gt, pgNowMicros),
			groupKey).Scan(&createdMicros); err != nil {
```

**postgres already runs a `RETURNING` on the group row inside `AddMember`'s transaction**, so `RETURNING
created_at, locked_by` adds a column, not a statement — exactly as Plan Task 6 Step 2(b) says. mysql and sqlite
already run a `SELECT created_at FROM <group> WHERE group_key = ?` in the same transaction, so both take a second
column for free. **All three rows of the bundle's "Today → Becomes" table are implementable as stated.**

`locked_by` is nullable, and the plan's *"scan into a `*string` / `sql.NullString` and treat NULL as not leased"*
matches the column's use in `ExpiredGroups` (`WHERE locked_by IS NOT NULL AND …`) and in `SettleGroup`'s
`SET locked_by = NULL`. **The discriminator is the same one the reaper already uses**, which is the convergence
D-AF's box claims — and the claim is true.

**5. The nine `*SelectMembers` call sites reproduce, and the three-callers-per-dialect shape D-AS depends on is
unchanged by Plan 032.**

```
$ grep -rn "SelectMembers(ctx" adapter/database/sql/{postgres,mysql,sqlite}/groupdialect.go
postgres/groupdialect.go:121:		members, err := pgSelectMembers(ctx, tx, mt, groupKey, "claimed_epoch IS NULL")       ← AddMember
postgres/groupdialect.go:163:		members, err := pgSelectMembers(ctx, tx, mt, groupKey, fmt.Sprintf("claimed_epoch = %d", newEpoch))  ← ClaimGroup
postgres/groupdialect.go:307:		members, err := pgSelectMembers(ctx, q, mt, c.key, "claimed_epoch IS NULL")           ← ExpiredGroups
mysql/groupdialect.go:113:  /  :161  /  :298      — identical three-site shape
sqlite/groupdialect.go:131:  /  :177  /  :314     — identical three-site shape
```

**Nine sites, three per dialect, at exactly the coordinates D-AS and Plan Task 6 Step 3 list.** The helper
signature is still `(ctx, q, mt, groupKey, claimedWhere string)` — a **string** predicate — so D-AS's separate
`limit int` is still the right shape rather than an overload. **And D-AF's *"the count cannot be derived from this
`SELECT`"* is verified true at the source:** `AddMember`'s site passes `"claimed_epoch IS NULL"`, so `len()` of its
result **cannot see a claimed member under any `LIMIT`**. The count source is **forced**, exactly as the bundle
says.

**6. The cycle that made the durable table unbounded reproduces in all three dialects.** `ClaimGroup` stamps every
live member; `AbandonGroup` returns them all to live; `SettleGroup` is the **only** statement that deletes a member
row, and only on success:

```
$ grep -rn "DELETE FROM %s WHERE group_key" adapter/database/sql/{postgres,mysql,sqlite}/groupdialect.go | grep -i member
postgres/groupdialect.go:191:  DELETE FROM <members> WHERE group_key = $1 AND claimed_epoch = $2   ← SettleGroup only
```

with `mysql` and `sqlite` identical in shape. **NEW-7's premise holds and the reversal is the right remedy.**

**7. `sql.GroupStore.ClaimGroup` really does decode the whole claimed set in-process**, which is what falsifies
revision 3's *"retained by the database, not the process"*:

```
$ grep -n 'decodeGroupRows' adapter/database/sql/groupstore.go
365:func (b groupBase) decodeGroupRows(rows msginsql.GroupRows) (msgin.MessageGroup, error) {
```

reached from `ClaimGroup`'s tail (`:284-297`) with no `LIMIT` by D-AS's own rule. **The third leg of D-AF's
three-part refutation is true.**

**Conclusion on the reversal: D-AF's reversal is CORRECT, and every code claim it rests on is verified true.**
R4-3, R4-6 and R4-8 attack the *magnitudes and disclosures* the reversal introduces. **None of them attacks the
reversal**, and none of them should be read as a reason to revisit it. Spec §8 item 1 should stay **closed**.

### The mechanical citation sweep — 261 citations, and the failures are confined to two files

Round 3's NEW-9 asked for *"a mechanical sweep of every `file:line` in the bundle."* It was run.

```
$ grep -ohE '`[a-zA-Z0-9_/.-]+\.(go|md)`?:[0-9]+(-[0-9]+)?' \
    docs/specs/017-*.md docs/adrs/0033-*.md docs/plans/031-group-member-bounds.md | wc -l
     261
```

**Result: 261 file:line citations; every cited FILE exists; and every stale coordinate is in one of exactly two
files** — `sizing_option_class_gate_test.go` (R4-2) and `adapter/http/helpers.go` (R4-5). **Both are the two areas
`git diff --stat d2c69fe..HEAD -- '*.go'` reports as changed.** Nothing else drifted, because nothing else moved.

**That is a genuinely good result and it should be read as one.** Revision 4's sweep was real and it was correct
**for the tree it was run against**. The class did not return because the sweep was careless; it returned because
**a sweep is a snapshot and the artifacts outlive the snapshot.** This is the whole argument for R4-2's method
change, and it is why R4-2's required fix is a script rather than a list.

Spot-verified as still correct at `f39725d`, across every non-drifted area:

| Citation | Verified |
|---|---|
| `routing/aggregator.go:33` — `completionSizeCeiling = 1 << 16` | exact |
| `routing/aggregator.go:362` — the `ErrExpiryChannelRequired` guard; `:363` the return | exact (NEW-9's fix holds) |
| `routing/aggregator.go:438-439` — *"another Handle/process is releasing this group; held"* | exact |
| `routing/aggregator.go:530-532` — *"go agg.Run(ctx) is REQUIRED"* | exact |
| `adapter/memory/groupstore.go:220` → `0`; `adapter/database/sql/groupstore.go:348` → `s.leaseTTL` | exact — §1.2.1's table is right in every cell |
| `adapter/database/sql/groupstore.go:22` / `:30` — the two named-`const` precedents D-AR leans on | exact |
| `adapter/database/sql/groupstore.go:211`, `:40-42`, `:271` — N-2's four-line compiler proof | exact |
| `adapter/memory/groupstore.go:55-62` — `maxGroupsCeiling`'s godoc shape | exact |
| `adapter/memory/groupstore_test.go:29` — the case `name:` | exact (NEW-9's fix holds) |
| `postgres/groupdialect.go:67`, `mysql:63`, `sqlite:55` — the `msgin/sql/<engine>:` convention NEW-5's fix adopts | exact |
| `sqlite/groupdialect.go:52-77` / `:62` — `withImmediateConn` / `BEGIN IMMEDIATE` | exact |
| `endpoint/helpers.go:97`, `routing/helpers.go:88`, `adapter/memory/helpers.go:54` | exact — **only the `adapter/http` copy moved** (R4-5) |

### Decisions attacked and left standing

- **D-AT is correct**, and its shape-vs-mechanism table is the clearest thing in the ADR. *"A gate that already
  runs constrains the commit; a gate you are about to write constrains only the order"* is the right
  generalization, and it is the right **scope** for it — it says where N-1's rule does **not** apply without
  weakening N-1. **R4-4 attacks the plan's implementation of D-AT's Step 0, not D-AT.**
- **D-AH's restated two-clause rule is true**, and it was checked exit by exit against §3.3a.1's own table: exits
  4 and 6 downgrade on evidence of drainage; exits 3 and 5 replace the error with a distinct fault; nothing
  upgrades. **The accepted consequence — a persistently failing claim/release path retries — is stated in all
  three artifacts and in the Consequences.** NEW-6 is fully discharged.
- **D-AE's two new sub-decisions are both right.** The `%s` site string matches the shipped convention and
  discriminates the engine; the `%d` divergence is correctly framed as *"members retained at the moment of the
  check"* with the two checks on opposite sides of the write, and normalising `sql` to `len(members)-1` is
  correctly rejected as rendering a count no statement observed.
- **D-AS is untouched by the reversal and still correct.** `limit = 0` on `ClaimGroup`/`ExpiredGroups` remains the
  right value; the bundle says so in three places and explicitly forbids *"fixing" an unbounded table by bounding
  `ClaimGroup`*. Verified: the nine call sites and the string-predicate signature are unchanged.
- **D-AR's file mandate is right and its scope note still is.** `maxGroups: 1024` staying unchanged is correct —
  no invariant test reads it.
- **Task 7's seven conformance assertions are the right seven**, and item 7 (fill to cap → `ClaimGroup` → add once
  more ⇒ rejected, transient) is the case that makes B6-8/B6-9 real. Its fixture is correctly shared with Step 5b.
- **The Sizing table's execution-order column is correct and complete**, and `1 → 2 → 4 → 5+6 → 3 → 7 → 8 → 9 →
  10` is consistent with every task's own header. No task's dependencies are violated by it.

### Gates verified non-vacuous or verified clean

- **The class gate is GREEN on this tree**, and its own output is the authoritative source R4-2's fix must use:

  ```
  $ GOTOOLCHAIN=go1.25.13 go test -count=1 -run TestSizingOptionClass -v .
  === EXPORTED FUNCTIONS (Recv==nil) with int/int64 param in ANY position: 17
  === EXPORTED METHODS with int/int64 param: 27 (excluded by the Recv==nil boundary, Spec 016 §2.0)
  --- PASS: TestSizingOptionClass_Completeness (0.01s)
  --- PASS: TestSizingOptionClass_Conformance (0.00s)
  ok  	github.com/kartaladev/msgin	0.230s
  ```

  **17 functions / 27 methods**, so the bundle's *"17 → 19 keys"* and *"`methodCount` stays 27"* are **both still
  correct** — Plan 032 added no `Recv == nil` key and no method. **Only the arm partition and the coordinates
  moved.**
- **The docs-link gate baseline is unchanged** at this tree: exactly the two known arm-1 false positives
  (`docs/plans/016-aggregator.md -> docs/plans/m` and
  `docs/specs/006-cron-source.md -> docs/specs/factory(fireTime`, both Go identifiers leaking from line-wrapped
  inline code) and **zero** arm-2 hits.
- **All three bundle files' internal links resolve**, including revision 4's new cross-references between §3.4,
  §3.5's box, §7.1 item 1, §8 items 1 and 9, D-AF and the Consequences.
- **`dbtest`'s `go vet` is still the only non-vacuous command there** — `GoFiles=[]`, four `XTestGoFiles` — so
  NEW-2's fix remains necessary as well as correct.

---

## ⚠️ LATER ADDITION — NOT THE AUDITOR'S: the generalised lesson, now with two instances one commit apart

> **This subsection was added by the COORDINATOR after the audit closed, and after revision 5 was written.** It is
> **not** part of what round 4 found, and no finding above depends on it. It is placed in this record rather than
> in the artifacts because it is a lesson about *how artifacts cite code*, which outlives this increment. The
> second instance below **was not available to the auditor** — it happened while revision 5 was being written.

**Two independent instances, one commit apart, of the same failure:**

| # | The commit | What it changed | What it invalidated |
|---|---|---|---|
| 1 | **`f39725d`** (Plan 032) | 237 lines of `sizing_option_class_gate_test.go` — real code and real test rows | **Every class-gate figure and coordinate in this bundle**, including **two of round 3's own three NEW-9 citation fixes** (`hasIntOrInt64Param` `:215`→`:243`, `isIntOrInt64` `:231`→`:259`) — finding **R4-2** |
| 2 | **`de38a95`** | 🔴 **COMMENTS ONLY** — two reworded doc comments, net `+1` line | **41 `adapter/database/sql/groupstore.go` citations across the three artifacts**, every coordinate at or below `:207`: `:211`→`:212`, `:250-276`→`:251-277`, `:271`→`:272`, `:273`→`:274`, `:284-297`→`:285-298`, `:348`→`:349`, `:365`→`:366` |

**Instance 2 is the sharper one, and it is why this subsection exists.** Instance 1 can be dismissed as bad luck —
a sibling increment rewrote a shared file, and the two plans knew they serialized on it (round 1's **B-3** said so).
**Instance 2 cannot.** It was a *documentation* commit. It changed no behavior, no signature, no test. It was the
kind of commit a reviewer waves through. **And it silently falsified 41 citations in a design bundle that had, one
revision earlier, been through a dedicated mechanical citation sweep.**

**The invariant, stated so it is not re-derived a fifth time:**

> 🔴 **On a repository where concurrent increments land, ANY artifact that cites `file:line` is stale from the
> moment it is written.** Not "may go stale" — *is* stale, with a probability that rises with every commit,
> including commits that change nothing an artifact cares about.

**The remedy is not a better re-derivation cadence.** Rounds 2, 3 and 4 each tried that, at increasing rigour, and
each was falsified: round 3 asked for *"a mechanical sweep of every `file:line` in the bundle"*, revision 4
delivered exactly that, and **the sweep was wrong one commit later.** Adding a fifth, more careful sweep is doing
the same thing harder.

**The remedy is to ELIMINATE THE CITATION.** Cite the **symbol** and the **grep that locates it**:

| Instead of | Write |
|---|---|
| ``the assertion at `:321-324` `` | `` `grep -n 'assert.Equal(t, want, found' sizing_option_class_gate_test.go` `` |
| ``ten count sites, at `:19`, `:38`, `:47` … `` | a **script** whose output *is* the table (Plan 031 Task 9 Step 3) |
| ``a number measured at `d2c69fe` `` | the number **plus the tree it was measured on, in the same sentence** |

A grep cannot go stale; it can only stop matching, and a grep that stops matching is a **loud** failure at the
moment someone runs it, which is exactly what a citation is supposed to be and never is.

**What revision 5 did, and what it deliberately did NOT do — stated so the gap is a known one, not a discovered
one.** R4-2's required fix was **scoped to the class-gate file**, and revision 5 implemented it there in full: the
site table is generated by a script that ships in Plan 031 Task 9 Step 3, and every load-bearing class-gate
coordinate in the three artifacts is now the grep that locates it. **The `adapter/database/sql/groupstore.go`
coordinates were corrected BY HAND — 41 of them, mechanically applied and each verified against the tree, but by
hand nonetheless — because the method change was not scoped to them.**

> **RECOMMENDATION for a future revision: give `adapter/database/sql/groupstore.go` — and `routing/aggregator.go`,
> which is the bundle's other citation-dense file — the same treatment.** They are the two files most cited by
> this bundle and both are live targets for exactly the kind of godoc commit that produced instance 2. Until then,
> **those coordinates carry the risk this subsection describes, and the next reader should re-derive them before
> relying on one.**

*(A note on scope discipline: revision 5 did **not** widen R4-2's fix beyond the class gate on its own initiative,
because an audit disposition that quietly grows past the finding is how a revision stops being reviewable against
the round that produced it. The wider treatment is recommended here and left for a revision that can state it as
its own decision.)*

---

## Auditor's method note

Every command in this record was run on the tree at **`f39725d`** with `GOTOOLCHAIN=go1.25.13` on darwin/arm64.
(`a306241` is current `main` and is docs-only; the Go tree is identical.) The class-gate run, the
`git show 6865886:` coordinate diff, the `git log -S 'wantArms'` provenance, the three `*CountMembers` bodies and
their callers, the nine `*SelectMembers` sites, the three `AddMember` group-row reads, `ExpiredGroups`' `WHERE`
arms, `reapInterval`/`RecoverInterval`/`defaultGroupLeaseTTL`, `classifyQueryErr`'s body, the `checkRange`
inventory, the 261-citation sweep and the docs-link gate are all first-hand output, not transcription. **No file in
the repository was modified.**

Round 4 deliberately did **not** re-audit [Spec 018](../specs/018-byte-cap-ceilings.md) /
[ADR 0034](../adrs/0034-byte-cap-ceilings.md) / [Plan 032](032-byte-cap-ceilings.md) as designs — they are a
**sibling increment, cleared and now LANDED**. They enter this record only as the **cause** of R4-1, R4-2 and R4-5:
the increment that landed second was supposed to *"re-derive the arm table from the tree"*, and Plan 031 is the one
that has to.

---

**VERDICT: NOT SAFE TO IMPLEMENT.**

***The design is sound; the execution instructions are not synchronised with the tree they run against.*** *No
decision in ADR 0033 was falsified this round. **D-AF's reversal is correct and every code claim it rests on was
verified true, one by one** — the three `*CountMembers` helpers, their missing `claimed_epoch` predicate, their
single existing `SettleGroup` caller each, the zero-round-trip `locked_by` read in all three dialects, and the
nine `*SelectMembers` sites. D-AT, D-AH's restated clause, D-AE's two new sub-decisions, D-AR's file mandate and
D-AS all stand.*

***What blocks implementation is the layer between the design and the keyboard.*** *[Plan 032](032-byte-cap-ceilings.md)
landed at `f39725d` and rewrote the file this increment serializes on, and **two exact-map `require.Equal`s that
no artifact in this bundle names — `wantArms` and `byArm` — now stop Tasks 1 and 5 from ever reaching green**
(**R4-1**). They predate Plan 030 and have been missing from the edit list through all four rounds; worse, the
assertion's own failure message sends its implementer to amend **Spec 016**, a delivered spec no task here opens.
Every other gate coordinate is stale, **including two of round 3's own three NEW-9 fixes**, and two figures are
wrong in composition rather than in coordinate (**R4-2**) — which is why the required fix is a **method** change,
not another list. The crashed-lease window the user is asked to ratify is understated by ~2× **in public godoc**
(**R4-3**), and the gate installed to enforce round 3's BLOCKER fix **stops on a correct tree** (**R4-4**).*

***All eight findings are accounted for.*** *R4-7 — all three artifact headers naming a commit that is no longer
`main` — reached this record **unlabelled**, folded into R4-2's third bullet because the two take the same edit;
it is written up in Part 2 as the coordinator's restatement, with its provenance marked, and it closes with
R4-2's header work.*
