# Plan 032 — adversarial design audit, round 4 (2026-08-22)

Independent Opus subagent, handed the **complete revision-4 bundle together** —
[Spec 018](../specs/018-byte-cap-ceilings.md) + [ADR 0034](../adrs/0034-byte-cap-ceilings.md) +
[Plan 032](032-byte-cap-ceilings.md) — **before any implementation code existed**, per
[`CLAUDE.md`](../../CLAUDE.md)'s design-time gate. The plan is part of what was audited, not merely the context
for it. Round 4 additionally **verifies round 3's fixes**: all seven findings, the N-9 fix-verification residue
and both smaller notes in [`032-audit-round-3.md`](032-audit-round-3.md) were re-checked against revision 4 and
against the tree.

**Traceability.** Audits: [Spec 018](../specs/018-byte-cap-ceilings.md),
[ADR 0034](../adrs/0034-byte-cap-ceilings.md), [Plan 032](032-byte-cap-ceilings.md). Prior rounds:
[`032-audit-round-1.md`](032-audit-round-1.md), [`032-audit-round-2.md`](032-audit-round-2.md),
[`032-audit-round-3.md`](032-audit-round-3.md). Parent artifacts whose contracts are implicated:
[Spec 016](../specs/016-sizing-option-bounds.md), [ADR 0032](../adrs/0032-sizing-option-bounds.md),
[Plan 029](029-sizing-option-bounds.md), [Plan 030](030-post-029-maintenance.md),
[Plan 031](031-group-member-bounds.md), [ADR 0031](../adrs/0031-nil-option-elements.md),
[ADR 0029](../adrs/0029-eip-lexical-alignment.md). Parent backlog: [`docs/HANDOVER.md`](../HANDOVER.md) §7 item 6.

This record is **evidence-primary** — it is the audit artifact, not a user-facing summary. Every structural claim
in the bundle was re-derived on this tree (`main` at `6865886`, clean worktree, `GOTOOLCHAIN=go1.25.13`,
darwin/arm64); the commands and their output are pasted below. No file in the repository was modified.

> **⚠️ IMMUTABLE EXECUTION RECORD.** This file records what round 4 found, at the time it found it. Do not edit it
> to reflect later decisions, later measurements, or the revised bundle. Corrections belong in a later round's
> record or in the artifacts themselves. The coordinator's dispositions for these findings live in
> **Spec 018 / ADR 0034 / Plan 032 revision 5**, each of which cites this file.

**Verdict: SAFE TO IMPLEMENT.** 0 BLOCKERs, 1 MAJOR, 9 MINORs.

**Fold R4-1 through R4-10 in without a fifth round; they are corrections, not re-designs.**

**Why this round clears the bundle when three did not, said plainly before the findings.** Rounds 1, 2 and 3 each
found a defect in the *design* — a false justification sentence, a false gate invariant, an unschedulable
acceptance criterion, a protocol with one signatory. **Round 4 found none.** Every finding below is a sentence
that disagrees with another sentence, a line offset that is off by two, or a mutant row that should exist and
does not. Not one of them changes a decision, a value, a signature, a task boundary or a commit boundary.
D-AM through D-AT stand exactly as revision 4 states them; the sixteen-site inventory stands; the two-dimensional
invariant stands; the one-commit coupling stands. That is what "safe to implement" means here — **the design is
settled, and what is left is copy-editing an unusually large amount of prose against itself.**

**The one MAJOR is a self-contradiction, not a design hole.** R4-1: revision 4 correctly reworded AC-1's third
clause from *"the knob's effect is observable"* to *"its product is usable"* — and then left the **heading of the
block that explains the change** saying *"**"Observable effect"** is the definition in force"*, eight lines
below. The spec now asserts both definitions, in the same section, one of them in the very block that exists to
retire it. It is ranked MAJOR because AC-1 is an **acceptance criterion** an implementer reads to know when a
case is done, and the criterion currently reads two ways.

---

## Part 1 — fix verification for round 3's seven findings, the N-9 residue and both smaller notes

Each re-checked against revision 4 and against the tree. **LANDED** = the required fix is present and correct.
**LANDED-BUT-FLAWED** = the fix is present but introduces or leaves a defect, tracked as a new finding.
**NOT LANDED** = absent from at least one artifact. **REGRESSED** = the fix made the artifact worse.
**CLOSED** = a residue whose instruction is gone from every artifact.

| # | Round-3 finding | Status | Evidence |
|---|---|---|---|
| **NEW-1** | MAJOR — `:48-49` is a third instance of the arm→literal claim, false in **both** clauses | **LANDED** | Site 14 is extended from `:47` to **`:47-49`** in all three artifacts — Spec §6 AC-4.1's site table (`:941`) plus the dedicated four-clause block (`:949-978`), Plan Step 6 site 14 (`:441`) plus **Trap 3c** (`:461-487`), ADR D-AS trap 3 **(c)** and **(d)** (`:630-642`). Each carries the required *replacement wording*, not merely the count: narrow to *"every **`int`-typed** ceiling"*, state inline that `byteCapCeiling` is an `int64` ceiling above `1<<30`, and fix-or-delete the *"largest is `1<<20`"* parenthetical. Each cites the seven-lines-apart self-contradiction as the reason. The delivery checklist gains a standalone item at `:846-848`. Arithmetic re-derived: `1073741824 < 2147483647` ✅ |
| **NEW-2** | MAJOR — the widened selector is still a token enumeration; `:409`, `:601`, `:799-800` missed | **LANDED-BUT-FLAWED** | Both halves landed. **(i)** `:409` and `:601` are sites 15/16 and `:799-800` is folded into site 12 (*"edit the MESSAGE, not the lines"*) in all three. **(ii)** The selector is replaced by the deliberately-broad form, and all three say in terms that *three rounds have each fixed the named sites while the defect returned through new ones*; the durable fix is recorded at Spec §8 item 5 and §7 *Out*. Re-derived: the broad form returns **104 of 812 lines** and its hit set contains `:26 :33 :35 :38 :47 :409 :521 :601 :799 :800` — all ten ✅. **But a seventeenth partition-recording site exists at `:22` and the union selector cannot reach it (→ R4-4), and site 5's disposition is stated as "the header's arm-list bullet" without saying delete-or-tombstone (→ R4-4).** |
| **NEW-3** | MAJOR — three inconsistent Plan 030 states, one operative | **LANDED** | The plan-number box (`:109-130`) is now the single place the state lives: 030 is **✅ FULLY DELIVERED**, the rebase instruction is **DISCHARGED**, and both naive signals are tabulated as liars with `git log --format='%h %s' --grep='Plan: 030'` named as the reliable one. Spec `:87-92` and Plan Task 1's rebase check (`:296-301`) both point at it rather than restating. Step 11b gains the `030-post-029-maintenance.md` disposition NEW-3 left open (`:652`). Re-derived: the trailer grep returns **six** commits including `1a1c135`, `d2c69fe`, `7ab91cd` ✅ |
| **NEW-4** | MINOR — mutant M3-6 changes the assertion **and** the behaviour, so it can only ever pass | **LANDED-BUT-FLAWED** | Recommendation (b) taken: M3-6 is deleted from the plan's gate-mutant table, with a block (`:738-750`) recording why, and B1-10 gains the scope clause. ADR D-AQ carries the same at `:484-492`. **But M3-6 is still cited as a live probe in Spec `:1135` and ADR `:551` (→ R4-2), and B1-10's *"the ONLY mutant proving that assertion load-bearing"* claim is not attributable — the wrap reds the `EqualError` assertions too (→ R4-3).** |
| **NEW-5** | MINOR — B1-4's 2 MiB fixture is larger and weaker than the boundary pair | **LANDED** | Recommendation (a) taken in full. Plan B1-4 (`:666`) is the pair — `1<<20` accepted / `1<<20 + 1` rejected — with **three** kills listed, not one; Global constraint 6's bound is tightened to **`1 MiB + 1`** (`:181-190`) with a 🔴 block recording the swap; Spec §6 AC-1 carries the six-row comparison table (`:762-769`). The shipped model is named in both: `adapter/http/exchange_test.go:309-334`, re-derived — `:309` is `const defaultCap = 1 << 20` ✅. The coordinator's own catch (the cited small-`n` proof does not exist) is recorded in all three; re-derived: `grep -rn 'body too large' --include='*.go' .` → **0 hits** ✅ |
| **NEW-6** | MINOR — AC-1's *"observable"* is unachievable for a ceiling-valued cap | **LANDED-BUT-FLAWED** | AC-1's third clause now reads *"**and its product is usable**"* (`:709`), with the three-identical-columns table and the *"unobservable by construction, and that is ACCEPTED"* paragraph (`:713-733`); Plan `:707-722` carries the twin. **But the block eight lines below AC-1's fix still opens *"**"Observable effect"** is the definition in force"* (`:741`), and `:733`'s *"revision 3 was the only artifact of the three saying "observable""* is falsified by `:741` in its own file (→ R4-1, the MAJOR). Plan `:206` also still says *"accepted with an observable effect"*.** |
| **NEW-7** | MINOR — the narrow sub-check is described but its command is never given | **LANDED** | Dropped, not retained, in all three — Spec `:918-922`, Plan `:411-413`, ADR `:603-605`, each stating the reason (*a check specified by its result rather than its procedure cannot be re-derived*) and that the broad form subsumes it. Re-derived: `grep -rn '18 lines' docs/specs/018-*.md docs/adrs/0034-*.md docs/plans/032-byte-cap-ceilings.md` → no surviving prescription ✅ |
| **N-9 residue** | Round 3 recorded, as a fix-verification note rather than a finding, that N-9 had landed in the spec and the plan and **not** in the ADR — the project's named two-of-three failure, for the second consecutive round | **CLOSED** | ADR **D-AP(b)** now prices it at `:425-438`, where the sibling helper's cost paragraph belongs, and generalises it: *"whenever D-R's duplication precedent is invoked to add a copy, the existing copies' godoc is part of the change."* The ADR's revision-4 changelog names the pattern explicitly at `:36-41` and orders *"diff the three artifacts against each other before closing any revision."* `grep -rn 'N-9'` now returns hits in **all three** files ✅ |
| **Note 1** | Step 11 D-3's `grep -c` is not wrap-safe, and the phrase is six words long | **LANDED** | Plan Step 11 D-3 (`:579`, `:596-609`) carries the `perl -0777` whole-file slurp — Plan 030 Task 1's house form — asserting **3 occurrences**, with the two-occurrence demonstration and the vacuity probe retained. The delivery checklist gains a standalone item (`:854`). Re-derived: `grep -c 'not a safety guarantee' adapter/http/options.go` → **0** today, so the post-condition is genuine ✅ |
| **Note 2** | The over-inclusion account names two ranges and should name `:33` and `:521` too | **LANDED** | Spec §6 AC-4.1 carries them as a two-row table with classifications (`:913-916`), Plan Step 6 as prose (`:417-420`), ADR D-AS in one clause (`:601-603`). Both re-derived first-hand: `:33` is the `rejects` bullet's neither-nor clause, `:521` the past-tense M2 note — both still true after the move ✅ |

**Score: 6 LANDED, 3 LANDED-BUT-FLAWED, 1 residue CLOSED. 0 NOT LANDED, 0 REGRESSED.**

**Nothing regressed, for the second consecutive round.** Revision 4 is a monotone improvement over revision 3.
More importantly: **the two-of-three failure did not recur.** Round 3 closed with the N-9 residue open in the
ADR and with an explicit instruction to diff the three artifacts against each other; revision 4 did that, and
every one of the seven round-3 findings is present in every artifact that should carry it. Of R4-1 through
R4-10 below, only **two** (R4-2, R4-5) are two-of-three misses, and both are stale *citations* rather than
missing substance. That is a different failure from the one this project has been living with.

---

## Part 2 — the stop-gap's judgement, and ten new findings

### The judgement round 4 was convened to make: is the sixteen-site stop-gap safe to implement?

**This is the round's central question, and the answer is yes.** Round 3's verdict named the class-gate inventory
as the thing a fourth round should attack first, on the grounds that three rounds had each patched it at the
instance level and each been overtaken. Round 4 attacked it, found a seventeenth site (R4-4), and **still clears
the bundle** — because revision 4 changed the *kind* of artifact the inventory is, and the change survives the
seventeenth site. Three reasons, in order of weight.

**1. The residual risk is honestly bounded, and the bundle says so in its own voice.** Revision 4 does not claim
the inventory is complete. All three artifacts call it a **stop-gap** in terms — Plan Step 6 orders the
implementer to *"SAY THIS OUT LOUD BEFORE YOU RUN ANYTHING: three consecutive audit rounds have each fixed the
sites they were shown and been overtaken by new ones"* (`:389-398`) and to describe the inventory as a stop-gap
**in the Evidence block**; Spec §6 AC-4.1 carries the four-row round-by-round defeat table (`:879-891`) and the
sentence *"the follow-up in §7 Out / §8 item 5 is the real fix; this inventory is the stop-gap"*; ADR D-AS says
*"THE DURABLE DEFECT IS STRUCTURAL, NOT LEXICAL"* in a heading. **A stop-gap that is labelled a stop-gap, whose
selector is deliberately over-broad, and whose classification pass is scheduled as a ten-minute task, is a
different object from three inventories that each claimed completeness.** The failure mode that produced rounds
1-3 was *believing the list*; revision 4 instructs the implementer not to.

**2. The durable follow-up is specified well enough to execute, not merely gestured at.** Spec §8 item 5 does not
say "improve this later." It names the mechanism (**format the two `require.Equal` messages from
`byArm`/`wantArms` instead of spelling the partition as a string literal**), names the second half (**reduce the
header block's counts to a one-line pointer at the assertion**), states the resulting property (**a partition
change breaks exactly one place and no derivation is needed at all**), states why it is out of scope here (**a
behavior-preserving refactor of a delivered gate, and this increment must not also restructure the file it is
moving rows inside**), and is cross-referenced from §7 *Out*, from ADR D-AS's *"(2) Record the real fix as a
follow-up"*, and from Plan Step 6. That is an executable brief. **Round 4's R4-10 is that this brief lives only
in Spec 018 §8 and is therefore undiscoverable from the project's actual backlog** — a bookkeeping fix, not a
specification gap.

**3. Decisively: a green delivery checklist CANNOT ship the failure mode.** This is the argument that converts
"probably fine" into "safe to implement", and it turns on a structural property of the checklist rather than on
the inventory's completeness. Round 3's concrete harm was that *"an implementer who satisfies the delivery
checklist verbatim ships a green gate whose own failure message reads '9 class members fixed here' over a
`wantArms` map of twelve."* **Revision 4 makes that unreachable**, because the checklist now carries the message
as its own item:

```
$ sed -n '843,850p' docs/plans/032-byte-cap-ceilings.md
- [ ] All **16** derived gate sites edited or explicitly classified — including `:26` and `:47-49` (round-2 N-2,
      round-3 NEW-1) and `:409`, `:601`, `:799-800` (round-3 NEW-2) — plus the over-inclusion account for `:33`
      and `:521`, and a decision recorded for `:766`.
- [ ] `:48-49`'s *"exceeds every ceiling in the codebase (the largest is `1<<20`)"* is **narrowed to
      `int`-typed**, with `byteCapCeiling`'s `int64` width stated inline (round-3 NEW-1) — the file agrees with
      its own dimension-2 wording.
- [ ] The gate's own `require.Equal` failure message at `:799-801` reports the **post-move** partition, not
      *"9 class members fixed here"* over a table of twelve (round-3 NEW-2).
```

**Item `:849-850` is standalone and independent of the site count.** It does not say "site 12 is edited"; it
states a **property of the shipped artifact** — the failure message reports the post-move partition — which is
checkable by reading the message, and which stays checkable if the inventory is 16, 17 or 20. Its truth is not
conditioned on the item three lines above it. So the round-3 harm cannot ship past a green checklist even under
the assumption that the inventory is still incomplete, which R4-4 proves it is. **The checklist has been
converted from an enumeration into an invariant** — which is precisely the project's stored lesson *assert the
invariant, not the enumeration*, applied to the checklist itself rather than to the file it governs.

**What that leaves as residual risk, stated exactly.** A partition-recording *comment* that the union selector
misses and no checklist item names — `:22` is today's instance (R4-4) — ships stale. That is a stale comment in a
test file: it misleads a future reader, it fails no gate, it corrupts no assertion, and the follow-up in §8 item
5 removes the whole class. **It is not a delivery blocker, and treating it as one would mean a fifth audit round
to fix a comment.** Fold R4-4 in and implement.

### The ten findings

| # | Rank | One line |
|---|---|---|
| **R4-1** | MAJOR | AC-1's fix block still headlines *"**"Observable effect"** is the definition in force"* eight lines under the AC-1 bullet that retires it, and `:733` claims revision 3 was the only artifact saying *"observable"* — falsified by `:741` in the same file |
| **R4-2** | MINOR | M3-6 was deleted from the plan but is still cited as a live probe in Spec `:1135` — **inside AC-7, whose claim is that every gate is probe-covered** — and in ADR `:551` |
| **R4-3** | MINOR | B1-10 is called *"the ONLY mutant proving that assertion load-bearing"*, but the `msgin.Permanent` wrap also reds every `EqualError` in AC-1/AC-2 and the three moved rows — the mutant is killed, but **not attributably** |
| **R4-4** | MINOR | A seventeenth partition-recording site exists at `:22` (*"in one of FOUR arms"*) and the union selector cannot reach it; and site 5's disposition is under-specified — delete-or-tombstone is not stated |
| **R4-5** | MINOR | `int64(len(body)) == max` is at `exchange.go:135`, not `:133`; `:133` is `return zero, err`. Cited wrongly in Spec `:172` and ADR `:507` |
| **R4-6** | MINOR | Constraint 6 was tightened to `1 MiB + 1`, but the spec's supporting prose still says ~2 MiB in four places and the `32×` arithmetic is now **64×** |
| **R4-7** | MINOR | Plan `:95` says the audit *"has run TWICE"*; `:98` says *"All three rounds"*. It is now four |
| **R4-8** | MINOR | Task 2 Step 4's `git add -N` prerequisite is a no-op — all six artifacts are tracked — and the count becomes **seven** once this record lands, which Step 5 does not anticipate |
| **R4-9** | MINOR | Step 4 item 4 renames three sentinel messages and six new `EqualError` assertions carry the new text, but **no mutant reverts a sentinel's text or the `site` argument** |
| **R4-10** | MINOR | Step 11b closes `docs/HANDOVER.md` §7 item 6 without entering the deferred follow-up in the same register — §7 is the project's discoverable backlog; Spec 018 §8 is not |

---

## MAJOR R4-1 — the spec states BOTH definitions of AC-1's third clause, one of them in the block that exists to retire the other

**The claim under attack.** Spec §6 AC-1's fix block, at `:741`:

```
$ sed -n '735,743p' docs/specs/018-byte-cap-ceilings.md
> 🔴 **The accessor clause was DELETED in revision 2 (round-1 M-10).** Revision 1 required *"and the accessor
> returns the value set"*, which is unsatisfiable: `maxBody()` (`options.go:272`) is **unexported**, and
> `maxResponseBytes`/`maxEventBytes` **have no accessor at all** — they are read as struct fields
> (`exchange.go:130`, `sseclient.go:401`). Plan Global constraint 2 forbids the whitebox escape, and the plan
> already contradicted the spec by saying *"Do not assert an accessor."*
>
> **"Observable effect" is the definition in force**, per Spec 016 §6's *"its product is usable"* for a
> `NewConfig`-only key: `WithMaxBodyBytes` via `DecodeRequest` on a small body; `WithMaxResponseBytes` via an
> `httptest` round-trip; `WithMaxEventBytes` via `NewSSEParser` + `Next` on a small event.
```

**The evidence — AC-1's own bullet, eight lines above, says the opposite.** `:709` reads *"`NewConfig(WithX(byteCapCeiling))`
→ nil error, non-nil `*Config`, **and its product is usable**"*, and the block at `:713-733` exists precisely to
explain why *"observable"* was wrong and was replaced. **The replacement's own explanatory block then reasserts
the retired term as the term in force.** Both sentences are in §6 AC-1; they are 32 lines apart; and the second
one carries the words *"is the definition in force"*, which is exactly the phrase an implementer would grep for
to settle the question.

**Why MAJOR and not a wording MINOR.** Three reasons, compounding.

1. **AC-1 is an acceptance criterion, not narrative.** It is the sentence an implementer reads to know whether a
   case is done. Revision 4's own NEW-6 fold-back argues at length that the two terms are **not** synonyms — that
   *"observable"* is a strictly stronger claim which **no legal fixture can satisfy** for a ceiling-valued cap.
   The criterion therefore reads two ways, and one of the two is provably unachievable under Plan Global
   constraint 6. An implementer who takes `:741` at its word will look for a discriminating fixture, fail to find
   one, and conclude the criterion is broken.
2. **`:733` asserts a fact its own file falsifies.** The paragraph closes: *"…it is the heading Plan 032 already
   uses for this paragraph — revision 3 was the only artifact of the three saying "observable"."* Re-derived:

   ```
   $ grep -n 'observable' docs/specs/018-byte-cap-ceilings.md | head
   714:> 🔴 **"Usable", not "observable" — and the difference is not pedantry (round-3 NEW-6).** Revision 3's third
   725:> Three identical columns is the definition of an unobservable setting. **The ceiling's effect is unobservable by
   733:> revision 3 was the only artifact of the three saying *"observable"*.
   741:> **"Observable effect" is the definition in force**, per Spec 016 §6's *"its product is usable"* for a
   ```

   `:741` is in the same file as `:733`, and `:741` says *"observable"*. The claim is false at the moment it is
   written. This is the shape of CLAUDE.md's stored lesson *verify structural claims against code* — applied
   here to a claim about the document's own contents, which is the cheapest kind to check and was not checked.
3. **The sentence body at `:741` is already correct, so the fix is one clause.** The block goes on to name the
   right three checks — `DecodeRequest` on a small body, an `httptest` round-trip, `NewSSEParser` + `Next` — and
   to cite Spec 016 §6's *"its product is usable"* **in the same sentence** that mislabels it *"Observable
   effect"*. The paragraph is arguing for "usable" while naming itself "observable".

**Required fix — three sentence edits, no design change.**

- **Spec `:741`:** reword the heading to **"**"Its product is usable"** is the definition in force"**. Leave the
  sentence body: it is already correct and already cites the right source.
- **Spec `:733`:** delete the clause *"revision 3 was the only artifact of the three saying "observable""*, or
  restate it as *"revision 3's AC-1 bullet"* — which is true and is what the paragraph means.
- **Plan `:206`:** the constraint-6 history block reads *"…which round-1 M-10 rewrote this same revision to
  require `NewConfig(WithX(byteCapCeiling))` → accepted **with an observable effect**."* Reword the trailing
  clause to *"accepted, with its product usable"*. The surrounding argument — that revision 2's constraint would
  have deleted AC-1's first bullet — is unaffected.

The ADR does not characterise AC-1's third clause and needs no edit; verified —
`grep -n 'observable' docs/adrs/0034-byte-cap-ceilings.md` returns nothing.

---

## MINOR R4-2 — M3-6 is deleted from the plan and still cited as a live probe in the spec and the ADR, inside the criterion that claims total probe coverage

**The claim under attack.** Spec §6 **AC-7**, `:1132-1135`:

> **Three further gates carry a probe, and every gate in the bundle is now covered by one** (round-2 N-12): the
> **386 vet gate** (§6 AC-3b …), the **`apidiff` / exported-symbol diff** (probed in `adapter/http`, not root —
> Plan Task 2), and the **class gate's own assertions** (Plan mutants **M3-3 and M3-6**).

and ADR `:551`, the same list in the D-AR(b) probe block: *"the class gate via **M3-3/M3-6**"*.

**The evidence.** M3-6 no longer exists. Plan `:738-750` deletes it, Plan `:755` names it as deleted in the
Evidence block, Plan `:853` makes *"**M3-6 is absent**"* a delivery-checklist item, and ADR D-AQ `:484-492`
records why. Re-derived:

```
$ grep -rn 'M3-6' docs/specs/018-*.md docs/adrs/0034-*.md docs/plans/032-byte-cap-ceilings.md | cut -d: -f1-2
docs/specs/018-byte-cap-ceilings.md:41      ← changelog, correctly describes the deletion
docs/specs/018-byte-cap-ceilings.md:1135    ← AC-7, cites it as LIVE
docs/plans/032-byte-cap-ceilings.md:43      ← changelog
docs/plans/032-byte-cap-ceilings.md:672     ← B1-10, "revision 3's M3-6 could not, and is deleted"
docs/plans/032-byte-cap-ceilings.md:738     ← the deletion block
docs/plans/032-byte-cap-ceilings.md:755     ← Evidence: "M3-6 is deleted"
docs/plans/032-byte-cap-ceilings.md:853     ← checklist: "M3-6 is absent"
docs/adrs/0034-byte-cap-ceilings.md:51      ← changelog
docs/adrs/0034-byte-cap-ceilings.md:484     ← D-AQ, "NOT by M3-6"
docs/adrs/0034-byte-cap-ceilings.md:551     ← D-AR(b), cites it as LIVE
```

Eight of the ten hits describe the deletion. Two assert the thing still exists.

**Why this is worse than an ordinary stale citation, and why it is nevertheless a MINOR.** The two survivors are
inside the bundle's **coverage claim**. AC-7's assertion is *"every gate in the bundle is now covered by a
probe"*, and it supports that assertion by enumerating the probes — one of which was deleted for being incapable
of probing anything. **Satisfying Plan `:853` (*"M3-6 is absent"*) therefore makes Spec AC-7 false**, which is a
plan and a spec that cannot both be discharged. That is a genuine contradiction between two artifacts of the same
bundle, which is why it is a finding rather than a typo.

It is a MINOR because the *substance* is intact: the class gate really does carry probes — M3-3 (*"leave site 4
as `"deferred": 0`"* ⇒ the `byArm` assertion fails) is a genuine kill, and B1-10 covers the `IsPermanent`
assertion on the moved rows. The enumeration is stale, not the coverage.

**Required fix.**

- **Spec `:1135`:** *"(Plan mutants **M3-1…M3-5 and B1-10**)"*.
- **ADR `:551`:** *"the class gate via **M3-3 and B1-10**"*.

Neither needs a re-argument; both are naming the probes that exist.

---

## MINOR R4-3 — B1-10 kills its mutant, but not ATTRIBUTABLY, and the plan claims otherwise

**This is the finding an implementer would actually act on**, and it is the only one below R4-1 that changes what
gets written rather than what a sentence says.

**The claim under attack.** Plan `:672`, B1-10's row:

> | B1-10 | **the classification** (round-1 M-8) | `assert.False(t, msgin.IsPermanent(err), …)` on every rejecting
> case, and on the three moved gate rows | wrap the return in `msgin.Permanent(...)` — **keeping the assertion**
> — ⇒ every rejecting case fails, **including the three moved gate rows**. … 🔴 **This is the ONLY mutant proving
> that assertion load-bearing (round-3 NEW-4); revision 3's M3-6 could not, and is deleted** |

**The evidence — the wrap is not a targeted mutation.** `msgin.Permanent(err)` returns a `*permanentError` whose
`Error()` **prefixes the message**:

```
$ sed -n '13p' reliability.go
func (e *permanentError) Error() string { return "msgin: permanent: " + e.err.Error() }
```

So wrapping the `checkRangeInt64` return changes the rendered string from

```
msghttp: max body bytes out of range: msghttp.WithMaxBodyBytes: 0 not in [1, 2147483647]
```

to

```
msgin: permanent: msghttp: max body bytes out of range: msghttp.WithMaxBodyBytes: 0 not in [1, 2147483647]
```

**Every `assert.EqualError` in the increment therefore reds under the same mutant** — the six AC-2 assertions
(two arms × three knobs, Plan `:697-705`) and the three moved gate rows' `assert.EqualError` on the `1<<62`
render (Plan Step 6 site 2, `:429`). The mutant *is* killed. But it is killed **whether or not the
`assert.False(t, msgin.IsPermanent(err), …)` line exists at all** — delete that line from every case, apply the
wrap, and the same cases still go red, via `EqualError`.

**That is exactly what Global constraint 7 forbids.** Plan `:209-211`: *"**Mutation-prove every new assertion**
with a mutant that targets **that** assertion."* A mutant that reds the case through a *different* assertion does
not prove the assertion under test is load-bearing — it proves the *case* is sensitive to the mutation, which
was never in doubt. The distinction matters here because `assert.False(IsPermanent)` is the **only** covering
test for ADR 0034 D-AQ's non-`Permanent` claim (Plan `:672` says so itself: *"Without this, D-AQ's
non-`Permanent` claim has no covering test at all"*), and round-1 M-8 raised it precisely because an untested
typed-error claim violates CLAUDE.md's hot-path rule. **The bundle deleted M3-6 for being non-discriminating and
replaced it with a mutant that is non-attributable.** That is an improvement — a killed mutant beats a mutant
specified to survive — but it does not discharge constraint 7.

**Round 3 did not catch this** because it was arguing the narrower point (M3-6 cannot fail) and B1-10 was the
obvious contrast case. The contrast is right; the "ONLY mutant proving" claim is what overreaches.

**Required fix — re-specify B1-10's mutant so exactly one assertion fails.** Two edits to the row:

1. **Make the mutant two-part:** *wrap the return in `msgin.Permanent(...)` **and** update that case's
   `assert.EqualError` expectation to the `"msgin: permanent: …"` render.* With the expected string corrected,
   `EqualError` passes and **only `assert.False(t, msgin.IsPermanent(err), …)` fails** — which is a mutant that
   targets that assertion, and is what constraint 7 asks for. Apply it to **one** rejecting case; the row's
   existing statement that the unmodified wrap reds *every* rejecting case is true and worth keeping as the
   coarse arm.
2. **Drop or qualify *"the ONLY mutant proving that assertion load-bearing"*.** Either delete the clause, or
   qualify it to *"the only mutant in this bundle that targets that assertion — and only in the two-part form
   above; the bare wrap also reds the `EqualError` assertions, so it is not attributable on its own."*

Mirror the qualification into ADR D-AQ `:484-492`, whose closing sentence — *"A killed mutant is the evidence"* —
is the sentence this finding narrows. Spec §6 AC-2b needs no change: it specifies the **assertion**, which is
correct; only the mutant that proves it needs re-specifying.

---

## MINOR R4-4 — a seventeenth partition-recording site the union selector cannot reach, and site 5's disposition is under-specified

Two defects in the same inventory, and the second is the one that ships.

### (a) Site 17 — `:22`, *"in one of FOUR arms"*

**The evidence.**

```
$ sed -n '19,24p' sizing_option_class_gate_test.go
//  2. CONFORMANCE (behavioral). Every one of the 17 AST-discovered keys, plus
//     2 MANUAL rows for the class members the Recv == nil boundary excludes
//     but a root test can still construct, gets an executable row — never a
//     declaration string — in one of FOUR arms. The arms are BEHAVIORAL and are
//     NOT a relabelling of Spec 016 §2.1's three classification verdicts; §2.1's
//     "classification arms ≠ AC-5 behavioral arms" note is normative for the
```

**`:22` records the arm partition's cardinality.** After this increment the `deferred` arm is emptied and — if
site 5's bullet is deleted rather than tombstoned — only **three** arms are populated, so *"FOUR arms"* joins the
list of header statements this increment falsifies.

**And it is unreachable by the union selector — first-hand:**

```
$ grep -nE 'deferred|DEFERRED|fixed|rejects|safe|1<<30|1<<20|1<<62|9/1/3/6|9 \+ 1 \+ 3 \+ 6|[0-9]+ (class|rows|AST)' \
    sizing_option_class_gate_test.go | cut -d: -f1 | grep -cx 22
0
```

The line contains no arm name, no literal, and no `9/1/3/6`; its only number-adjacent token is the **word**
`FOUR`, and the `[0-9]+ (class|rows|AST)` alternative requires a digit. **This is further evidence for the
stop-gap judgement above, not against it:** round 3 predicted that a fourth round would find a seventeenth site,
and here it is — reached by reading the file rather than by any selector, which is precisely why Spec §8 item 5's
follow-up is the real fix and this inventory is a stop-gap. Say that in the artifacts when folding it in.

### (b) Site 5's disposition — the actual defect

**The claim under attack.** Site 5 is stated in two words. Spec `:932`:

> | 5 | `:35` | the header's arm list | the `- "deferred" (3) — accepts 1<<62 …` bullet |

Plan `:432`:

> | 5 | `:35` | the header's arm-list bullet |

**Neither says whether the bullet is DELETED or TOMBSTONED**, and the two produce different files. Site 8
(`:401`, the `arm` field's vocabulary doc) is explicit — *"**keep `"deferred"` with a tombstone**: *(no members as
of Plan 032 — see Spec 018)*"* — and AC-4.2's *"Retire the arm name, or keep it as a tombstone?"* paragraph
resolves the **concept** in favour of a tombstone while saying, of the header, only *"Delete it from the counts
map (it must be absent) and from the header's arm list."* So the bundle currently reads: tombstone the
vocabulary, delete the header bullet. **That combination is what makes `:22` go false.**

**Choose the tombstone**, for three reasons:

1. **`:22`'s *"FOUR arms"* stays true.** A tombstoned bullet — `- "deferred" (0) — no members as of Plan 032; see
   Spec 018. The arm is retained so a future knob with a genuinely deferred remedy has it.` — keeps the header's
   list at four entries, and the arm-count claim at `:22` needs no edit at all.
2. **It is consistent with site 8.** Tombstoning the `arm` field's vocabulary while deleting the header's
   description of that same vocabulary leaves the file explaining a value in one place and denying it in another
   — the *docs can contradict the code they describe* class, one more time.
3. **Deleting the bullet actively creates a contradiction.** With `:35` gone, the header's arm list carries
   **three** items (`fixed`, `rejects`, `safe`) four lines below a sentence asserting **four** arms. An
   implementer who deletes the bullet and does not also edit `:22` ships that; an implementer who deletes the
   bullet and *does* edit `:22` has to re-argue the tombstone decision AC-4.2 already took.

Note that the **counts map** is unaffected either way: `byArm` is built by counting and cannot carry a zero
entry, so the `deferred` key is removed regardless (site 4's trap). The tombstone is a *documentation* decision,
not a data one.

**Required fix.**

- **Add site 17** — `:22`, *"…declaration string — in one of FOUR arms"* — to Spec §6 AC-4.1's table, Plan Step
  6's table and ADR D-AS's site count, with the note that the union selector does not reach it and that under the
  tombstone disposition its edit is **"no change"**. Raise the inventory to **17** in all three, and in the
  delivery checklist.
- **Make site 5 explicit in both artifacts:** *"**keep the bullet as a tombstone** — `(0)`, `no members as of
  Plan 032 — see Spec 018` — matching site 8's tombstone for the `arm` field vocabulary, so `:22`'s *FOUR arms*
  stays true. **Deleting the bullet instead would leave `:22` contradicting the three-item list four lines below
  it.**"*
- Reconcile AC-4.2's *"and from the header's arm list"* clause with the choice.

---

## MINOR R4-5 — the INV-6 comparison is at `exchange.go:135`, and `:133` is a different statement

**The claim under attack.** Spec `:172`:

> `exchange.go:133`'s `int64(len(body)) == max` compares an `int`-derived length against the `int64` cap …

and ADR `:507`, identically: *"`exchange.go:133`'s `int64(len(body)) == max` INV-6 check is unaffected."*

**The evidence.**

```
$ sed -n '130,136p' adapter/http/exchange.go
	max := x.cfg.maxResponseBytes
	body, err := io.ReadAll(io.LimitReader(resp.Body, max))
	if err != nil {
		return zero, err
	}
	if int64(len(body)) == max {
```

`:133` is `return zero, err` — the `io.ReadAll` error arm. The INV-6 comparison is at **`:135`**.

**Why it matters enough to record.** The bundle's own **Line/offset convention** (Spec `:53-55`) says a
`file.go:NNN` cite names the declaration, *"except where a specific godoc **sentence** is the subject, in which
case the sentence's own line is cited **and the text is quoted**."* Both citations quote the text — so the offset
is load-bearing under the bundle's own rule, and both are wrong by two. The surrounding argument is unaffected:
the comparison genuinely is `int64`-vs-`int64`, the arithmetic genuinely does not change, and branch 20 genuinely
is retired. Only the address is wrong.

**Required fix.** `exchange.go:133` → **`exchange.go:135`** in Spec `:172` and ADR `:507`. The plan does not cite
this offset; verified — `grep -n 'exchange.go:133' docs/plans/032-byte-cap-ceilings.md` returns nothing.

---

## MINOR R4-6 — constraint 6 was tightened to `1 MiB + 1` and the spec's supporting prose still argues from 2 MiB

**The claim under attack.** Four spec statements, each written against the superseded bound.

| Spec line | Text | Why it is now wrong |
|---|---|---|
| `:52` | *"a 64 MiB fixture would breach Plan Global constraint 6's **≤ 2 MiB** bound by **32×**"* | the bound is `1 MiB + 1`; the ratio is 64× |
| `:715` | *"the constraint caps every fixture at **~2 MiB**"* | it caps at `1,048,577` bytes |
| `:727` | *"anything above **~2 MiB** is forbidden, anything below 1 MiB passes under both"* | the forbidden band starts at `1 MiB + 2` |
| `:756` | *"a 64 MiB fixture would breach Plan Global constraint 6's **≤ 2 MiB** bound by **32×**"* | as `:52` |

**The plan is correct and is the authority.** Plan `:181-190` states the bound as **`1 MiB + 1` bytes
(`1<<20 + 1` = 1,048,577)** and — this is the part that makes the spec's prose stale rather than merely
imprecise — states that **B1-4's reject arm IS the bound**: *"the bound is derived from the boundary pair, not
the other way round."* The 🔴 block at `:192-198` records the tightening explicitly. So the spec is not
disagreeing about a rounding; it is arguing from a bound the plan deliberately retired one revision ago, in the
same fold.

**The arithmetic, re-derived:**

```
$ python3 -c "print(67108864/1048577)"
63.99993896490196
```

64 MiB ÷ (1 MiB + 1) = **63.99993…**, i.e. **64×**, not 32×. (32× was correct against the 2 MiB bound:
64 ÷ 2 = 32.)

**Why it is a MINOR.** No test is specified from these sentences — B1-4 is specified in the plan, correctly, and
Spec §6 AC-1's own comparison table (`:762-769`) already names *"Largest allocation … **1 MiB + 1**"* as the
pair's advantage. The stale prose is in the *justification* paragraphs, not in the criterion. But it is the
justification an implementer reads when deciding whether a fixture is legal, and `:715`/`:727` are inside the
NEW-6 block that AC-1 points at.

**Required fix.** In the spec only: `~2 MiB` → `1 MiB + 1` at `:715` and `:727`, and `≤ 2 MiB … by 32×` →
`≤ 1 MiB + 1 … by 64×` at `:52` and `:756`. The ADR carries no fixture bound; verified —
`grep -n '2 MiB' docs/adrs/0034-byte-cap-ceilings.md` returns nothing.

---

## MINOR R4-7 — the plan says the audit has run twice, three times and (implicitly) not four, in four lines

**The claim under attack.** Plan `:95-103`:

```
$ sed -n '95,101p' docs/plans/032-byte-cap-ceilings.md
**The adversarial design audit has run TWICE and has not yet cleared this bundle.**
[CLAUDE.md](../../CLAUDE.md) makes it a hard gate: a fresh Opus subagent attacks the complete bundle —
[Spec 018](../specs/018-byte-cap-ceilings.md) + [ADR 0034](../adrs/0034-byte-cap-ceilings.md) + this plan —
**together**, before any implementation code. **All three rounds returned NOT SAFE TO IMPLEMENT**
([round 1](032-audit-round-1.md), [round 2](032-audit-round-2.md), [round 3](032-audit-round-3.md)). Two rounds
is this project's established norm and **this bundle has now exhausted it by 50% without clearing**; Plan 029
needed **five**.
```

**Three inconsistencies in seven lines.** *"has run TWICE"* (`:95`) contradicts *"All three rounds"* (`:98`) two
lines later — the heading was not updated when revision 4 folded round 3 in. *"exhausted it by 50%"* is the
arithmetic of three rounds against a norm of two, so `:100` agrees with `:98` and not with `:95`. And after this
record lands, all three figures are wrong: it is **four** rounds, and the norm is exhausted by 100%.

**Why it matters.** The plan's revision header is where a fresh session establishes how much scrutiny the design
has had before deciding whether to run another round. A header that says "twice" invites a fifth round that this
record explicitly says not to run. It is the same class as R4-2 and R4-8 — a count that was true when written and
was not re-derived when the thing it counts changed — which is this project's most-repeated defect.

**Required fix.** Make `:95` and `:98` agree, at **four**: *"The adversarial design audit has run **FOUR TIMES**,
and round 4 **cleared** this bundle"*; *"Rounds 1-3 returned NOT SAFE TO IMPLEMENT; **round 4 returned SAFE TO
IMPLEMENT**"*; and restate `:100`'s norm sentence without the percentage, which will go stale again. The
*"Round 4 must run on revision 4 before implementation begins"* instruction at `:101-103` is **discharged by this
record** and must be replaced with the clearance — not merely renumbered — since leaving it standing would tell
the next session to convene the fifth round this record says not to convene.

---

## MINOR R4-8 — Task 2's `git add -N` prerequisite is a no-op, and the file count goes stale the moment this record lands

**The claim under attack.** Plan Task 2 Step 4 (`:793-796`) and Step 5 (`:797-802`):

> **Step 4 (LINK GATE).** Run **both arms** … over every tracked `.md` — note that `git ls-files` is blind to
> **untracked** files, so `git add -N` the **six** new artifacts first (Plan 030 round-1 MINOR 11). …
>
> **Step 5 (VACUITY PROBE).** … The **six** files under gate are this plan, Spec 018, ADR 0034,
> `032-audit-round-1.md`, `032-audit-round-2.md` and `032-audit-round-3.md`.

**The evidence — all six are already tracked at `6865886`:**

```
$ git ls-files docs/specs/018-* docs/adrs/0034-* docs/plans/032-*
docs/adrs/0034-byte-cap-ceilings.md
docs/plans/032-audit-round-1.md
docs/plans/032-audit-round-2.md
docs/plans/032-audit-round-3.md
docs/plans/032-byte-cap-ceilings.md
docs/specs/018-byte-cap-ceilings.md
$ git status --short
(no output)
```

So `git add -N` is a no-op on this tree, and the step's stated *reason* — *"`git ls-files` is blind to untracked
files"* — no longer applies. **The step is not harmful**, but a prerequisite that cannot fire trains the reader to
skip it, and it will matter again the moment a seventh artifact exists and is not yet committed.

**And a seventh artifact is about to exist.** This record — `docs/plans/032-audit-round-4.md` — lands with
revision 5, taking the count to **seven**. Step 5's enumeration names six files by name and does not anticipate
it, and Step 4 says "six" in the same breath as `git add -N`.

**Required fix — and word it so it does not go stale a fifth time.**

- **Step 4:** keep `git add -N` but restate its purpose as a **guard**, not a fix: *"`git add -N` any bundle
  artifact not yet tracked — on a clean tree this is a no-op, and that is the expected state; it exists so an
  uncommitted new artifact cannot slip past `git ls-files`."*
- **Both steps:** replace the literal **six** with a derived description — *"every `docs/specs/018-*`,
  `docs/adrs/0034-*`, `docs/plans/032-*` file, whatever their number"* — and, where a count is genuinely wanted,
  name the command that produces it (`ls docs/specs/018-* docs/adrs/0034-* docs/plans/032-* | wc -l`) rather
  than the figure.
- **Spec §6 AC-7** (`:1125-1130`) carries the same *"the **six** new files"* enumeration and needs the same
  treatment.

---

## MINOR R4-9 — three sentinel messages are renamed and six new assertions carry the new text, and no mutant reverts either the text or the `site` argument

**The claim under attack.** Plan Step 4 item 4 (`:344`) — *"The three sentinel messages: `must be > 0` →
`out of range` (D-AQ)"* — together with the *"Also asserted"* block at `:697-705`, which adds **six** new
`EqualError` assertions (two arms × three knobs) whose expected strings embed both the renamed sentinel text and
the `site` argument:

```
msghttp: max body bytes out of range: msghttp.WithMaxBodyBytes: 0 not in [1, 2147483647]
msghttp: max body bytes out of range: msghttp.WithMaxBodyBytes: 2147483648 not in [1, 2147483647]
```

**The evidence — nothing guards the wording, before or after.** Spec §3.1a states the pre-condition:
*"it asked 'does any test assert the wording?' (answer: no — the grep returns only the three `errors.go`
declarations, and that remains true)."* Re-derived:

```
$ grep -rn 'max body bytes must be\|max response bytes must be\|max event bytes must be' --include='*.go' . | wc -l
3
$ sed -n '19p' adapter/http/errors.go
	ErrInvalidMaxBodyBytes = errors.New("msghttp: max body bytes must be > 0")
```

Three hits, all declarations. So **today** no test asserts the message text; **after** the increment six do —
and the mutant tables in Plan `:661-672` and `:730-736` contain no row that perturbs either the sentinel string
or the `site` literal. B1-1 through B1-3 mutate the *bounds* (`lo`, `hi`, the always-error case); B1-6 deletes an
arm; M3-1 through M3-5 target the gate's arms and lengths. Every one of them would be killed by a bare
`require.ErrorIs`. **The six `EqualError` assertions are therefore unproven under Global constraint 7's
*"mutation-prove every new assertion with a mutant that targets that assertion"*** — they are the *only* thing
standing between D-AQ's rename and a silent revert, and nothing demonstrates they would notice one.

This is the same shape as R4-3: a new assertion whose covering mutant is killed by a different assertion. It is
listed separately because the remedy is an added row rather than a re-specified one, and because the assertion
here is the increment's **stated behavioral change** — Spec §3.1 calls the message change *"a deliberate, stated
behavioral change"*, and ADR D-AQ's whole table is the rename.

**Required fix — add one mutant row to Plan Task 1's table:**

> | B1-11 | **the renamed sentinel text** (D-AQ) | revert `errors.go:19` to `"msghttp: max body bytes must be > 0"` ⇒ **both** `WithMaxBodyBytes` `EqualError` assertions (lower and upper arm) fail, while every `ErrorIs` assertion in the increment stays green — which is what proves the `EqualError` pair is carrying the rename |

**Optionally, and cheaply, a second:** mutate the `site` argument at one call site
(`"msghttp.WithMaxBodyBytes"` → `"msghttp.WithMaxResponseBytes"`) ⇒ that knob's two `EqualError` assertions fail
and nothing else does — proving the `site` string is asserted rather than merely rendered. Both belong in the
Evidence block's killed-mutant list, and both are near-free: they are one-token edits to a string literal.

Mirror a one-clause note into ADR D-AQ, whose *"Test-safety"* paragraph currently establishes only that **no
existing** test asserts the wording — which is the pre-condition, not the post-condition.

---

## MINOR R4-10 — the deferred follow-up is recorded where the next session will not look

**The claim under attack.** Plan Step 11b (`:611-653`) closes `docs/HANDOVER.md` §7 item 6 — *"**`docs/HANDOVER.md`**
§7 item 6 → **CLOSED**, citing this bundle"* — and says nothing about entering the deferred class-gate refactor
anywhere in §7.

**The evidence — §7 is the project's discoverable backlog, and it is where item 6 lived.**

```
$ awk '/^## 7/,/^## 8/' docs/HANDOVER.md | grep -c '^| [0-9]'
7
$ awk '/^## 7/,/^## 8/' docs/HANDOVER.md | grep '^| 6'
| 6 | Byte-ceiling class | **Untouched.** Needs its own spec/ADR/plan. …
```

Spec 018 exists **because** §7 item 6 existed — the spec's own **Origin** line says so (`:69`). The follow-up
this increment defers is of exactly the same kind: a known, specified, unscheduled piece of work in the same
file, produced by the same increment. Recording it only at **Spec 018 §8 item 5** and **§7 *Out*** puts it inside
a 1,200-line design document for an increment that is about to be marked delivered — a place a fresh session
reads when it is working on byte caps, and nowhere else.

**Why this is not bookkeeping pedantry.** Round 3's verdict, and Spec §8 item 5's closing sentence, both give the
same reason for recording it: *"so a fourth round does not find a seventeenth site."* **R4-4 found the
seventeenth site.** The prediction was correct, the recording did not prevent it, and the next thing that happens
to this file — Plan 031's hand-edit — will meet the same duplication with no backlog entry pointing at the fix.
The item is load-bearing for the *next* increment, which is the definition of what §7 is for.

**Required fix.** Extend Step 11b's `docs/HANDOVER.md` bullet so the same edit that closes item 6 **opens a new
§7 row** — the file is being edited in that commit either way, so the cost is one line:

> - **`docs/HANDOVER.md`** §7 item 6 → **CLOSED**, citing this bundle — **and add a new §7 row: *"Derive the
>   class gate's prose counts from `wantArms` at test time"* — `sizing_option_class_gate_test.go` restates the
>   arm partition in ~10 prose locations with no mechanical link to the map the test computes; four audit rounds
>   have each patched the instances (7 → 12 → 14 → 16 → 17 sites). Designed at Spec 018 §8 item 5; unscheduled.**

Mirror the requirement into Spec §6 AC-5 (which enumerates what the fold-back amends) and into ADR D-AT(b)
(which lists the artifacts recording the old arm), so all three carry it — this is the artifact class the N-9
residue was about.

---

## Checked and found CLEAN — 24 rows, newly verified this round

Round 5, if a later increment ever convenes one, should not re-derive these. Each was checked first-hand against
the tree at `6865886`. Rounds 2 and 3 each contributed 16 clean rows of their own; those stand and are not
repeated here.

| # | Claim | Where | Verification |
|---|---|---|---|
| 1 | The broad selector returns 104 of the file's 812 lines, as all three artifacts state | Spec §6 AC-4.1, ADR D-AS, Plan Step 6 | `grep -nE '…' sizing_option_class_gate_test.go \| wc -l` → **104**; `wc -l` → **812** ✅ |
| 2 | The broad selector reaches every site the bundle names, including round 3's three additions | Spec §6 AC-4.1 sites 12-16 | its hit set contains `:26 :33 :35 :38 :47 :409 :521 :601 :799 :800` — **10 of 10** ✅ (`:22` is the exception, → R4-4) |
| 3 | `:409` really is the `fixed` banner and really does say 9 | Site 15 | `409:\t\t// ---- arm: fixed — the 9 class members this increment bounds ----` ✅ |
| 4 | `:22`'s *"FOUR arms"* is real text and is a partition claim | R4-4 | `22://     declaration string — in one of FOUR arms. The arms are BEHAVIORAL and are` ✅ |
| 5 | `:35` is the `deferred` bullet and `:38` the arithmetic identity, so sites 5 and 6 are correctly addressed | Sites 5-6 | `:35` = `- "deferred" (3) — accepts 1<<62, annotated so it never reads as a`; `:38` = `9 + 1 + 3 + 6 = 19 rows = 17 AST keys + 2 manual rows.` ✅ |
| 6 | `1<<30 < byteCapCeiling`, so site 14's clauses 3 and 4 genuinely go false | Spec site-14 block, ADR trap 3(c)/(d) | `1073741824 < 2147483647` ✅ |
| 7 | `math` is used exactly once in `exchange_test.go`, so N-10's `math.MaxInt32` instruction is still necessary | Spec §6 AC-2c, Plan Step 8 | `grep -c 'math\.' adapter/http/exchange_test.go` → **1** ✅ |
| 8 | The shipped boundary-pair model B1-4 copies exists at the cited offset | Plan B1-4, Spec §6 AC-1 | `sed -n '309p' adapter/http/exchange_test.go` → `const defaultCap = 1 << 20 // the documented 1 MiB default` ✅ |
| 9 | The small-`n` proof revision 3 cited genuinely does not exist, so the coordinator's correction is right | Spec `:752-757`, Plan `:684-689` | `grep -rn 'body too large' --include='*.go' . \| wc -l` → **0** ✅ |
| 10 | Both 386 gates exit 0 on this tree, so AC-3 remains a usable gate | Spec §6 AC-3, Plan Step 9 | `GOTOOLCHAIN=go1.25.13 GOARCH=386 GOOS=linux go vet ./...` → **exit 0**, zero output ✅ |
| 11 | The call-site grep still returns 49, as §3.1a states | Spec §3.1a, Plan Step 2 | `grep -rn 'WithMaxBodyBytes(\|WithMaxResponseBytes(\|WithMaxEventBytes(' --include='*_test.go' . \| wc -l` → **49** ✅ |
| 12 | The case-insensitive `must be > 0` sweep still returns 13, as Step 11 D-1 states | Plan Step 11 D-1 | `grep -rin 'must be > 0' adapter/http/ \| wc -l` → **13** ✅ |
| 13 | The hazard disclosure still does not exist, so D-3's `→ 3` is a genuine post-condition | Plan Step 11 D-3 | `grep -c 'not a safety guarantee' adapter/http/options.go` → **0** ✅ |
| 14 | No test asserts the three sentinel messages today, so the rename is safe on its pre-condition | ADR D-AQ, Spec §3.1a | the three-message grep returns **3** hits, all `errors.go` declarations ✅ (the *post*-condition is R4-9) |
| 15 | `checkRange` has exactly three callers and the two godoc sentences N-9 names are still at `helpers.go:51`/`:57` | Spec §4 item 4, ADR D-AP(b), Plan Step 5 | `grep -c 'checkRange(' adapter/http/options.go` → **3**; `:51` = *"each of this package's three sites"*, `:57` = *"All three sites are R1"*; `:64` = `func checkRange(sentinel error, site string, n, lo, hi int) error` ✅ |
| 16 | `errors.go`'s `(and so by NewSSEParser)` clause survives and is what Step 5 row 6 preserves | Plan Step 5 row 6 | `sed -n '131,134p' adapter/http/errors.go` → *"ErrInvalidMaxEventBytes is returned by NewConfig (and so by / NewSSEParser) when an explicit WithMaxEventBytes is <= 0"* ✅ |
| 17 | Plan 030 is fully delivered and the trailer is the signal that shows it | Plan's plan-number box, Spec `:87-92` | `git log --format='%h %s' --grep='Plan: 030'` → six commits, including `1a1c135`, `d2c69fe`, `7ab91cd` ✅ — the coordinator's round-3 correction is confirmed independently |
| 18 | Plan 031 still has ten tasks and none is a Spec 016 fold-back, so AC-4.2b's unconditional-ownership rule still holds | Spec §6 AC-4.2b, ADR D-AS, Plan Step 11b | `grep -c '^## Task' docs/plans/031-group-member-bounds.md` → **10**; `grep -n 'Spec 016 §2.1' …031….md` → **0 hits** ✅ |
| 19 | N-9 is now present in all three artifacts — the two-of-three residue is genuinely closed | ADR D-AP(b) | `grep -rn 'N-9'` returns hits in the spec, the plan **and** the ADR ✅ |
| 20 | The bundle's six artifacts are all tracked and the worktree is clean, so this round's derivations are against committed state | method | `git ls-files` → six paths; `git status --short` → empty ✅ (the *consequence* for Step 4 is R4-8) |
| 21 | `permanentError` prefixes rather than replaces, which is what makes R4-3 true and B1-10's coarse arm real | Plan B1-10 | `reliability.go:13` → `return "msgin: permanent: " + e.err.Error()` ✅ |
| 22 | The one-commit coupling is intact end to end — Task 1 carries production, godoc, gate and parents | Spec §6 AC-4.0/AC-5, ADR D-AT(a-bis)/(b), Plan Task 1 | Task 1's Files list carries `016-…`, `0032-…` and `../HANDOVER.md` with correct `../specs/`/`../adrs/` prefixes; Step 11b is inside Task 1; Task 2's preamble says *"verification and the PROPOSED → ACCEPTED flip"* ✅ |
| 23 | The two-dimensional invariant survived the revision-4 fold unchanged in all three artifacts | Spec §6 AC-4.1, ADR D-AS trap 4, Plan Trap 3b | all three state arm-fixes-property first, parameter-type-within-reject-arms second, and all three order `:61-77`'s warning **carried forward verbatim** ✅ — round 3's *"should survive round 4 unchanged"* is satisfied |
| 24 | Global constraint 6's restoration and AC-4.2b's unconditional-ownership rule likewise survived unchanged | Plan constraint 6, Spec §6 AC-4.2b | constraint 6 still bounds the **fixture**, still carries the 🔴 do-not-restore-revision-2 warning, and still says a cap may be configured at any legal value; AC-4.2b still deletes both prior instructions ✅ (the bound's *value* moved with NEW-5 — R4-6 is the prose that did not follow) |

**Also clean, and worth preserving as reasoning rather than derivation.** Three things in revision 4 are better
than what they replaced and should not be touched when folding R4-1…R4-10 in. **(1) The plan-number box**
(`:109-130`) is now a single-source-of-truth table with a *"how to derive a plan's delivery state — and how NOT
to"* sub-table naming both liars and the trailer. That shape should be copied by every future plan that has to
reason about a sibling's state. **(2) Global constraint 6's derivation is now inverted in the right direction** —
*"the bound is derived from the boundary pair, not the other way round"* — which is what stops the next revision
re-inflating it to accommodate a fixture. **(3) The stop-gap framing** discussed at the head of Part 2: labelling
an inventory as provisional, over-selecting deliberately, and moving the load-bearing guarantee into a standalone
checklist invariant is the correct response to a defect three rounds could not fix by widening. It is what makes
this bundle implementable.

---

## Auditor's method note

Every command in this record was run on the tree at `6865886` (clean worktree) with `GOTOOLCHAIN=go1.25.13` on
darwin/arm64. The 104/812 selector counts and their hit sets, the `:22` non-membership check, the `:19`-`:24`
and `:130`-`:136` extracts, the `permanentError.Error()` body, the `1073741824 < 2147483647` arithmetic, the
`64 MiB ÷ (1 MiB + 1)` division, the 49-hit call-site total, the 13-hit case-insensitive sweep, the three-hit
sentinel-message grep, the `checkRange` call-site count, the single `math` use in `exchange_test.go`, the
`errors.go` `NewSSEParser` clause, the 386 vet exit code, the `Plan: 030` trailer log, the Plan 031 task count,
the `git ls-files` / `git status` pair, and the ten-way M3-6 citation sweep are all first-hand output, not
transcription. No file in the repository was modified.

**What round 4 deliberately did NOT re-derive:** rounds 2 and 3's clean rows, which stand. The 24 rows above are
claims those rounds did not check, or claims whose *value* could have moved in the revision-4 fold.

---

**VERDICT: SAFE TO IMPLEMENT.**

*Rounds 1, 2 and 3 each found a defect in the design; round 4 found none. Every finding above is a sentence
disagreeing with another sentence, an offset off by two, or a mutant row that should exist. Not one changes a
decision, a value, a signature, a task boundary or a commit boundary. The sixteen-site inventory is still
incomplete — `:22` is the seventeenth site, and the union selector cannot reach it — but revision 4 stopped
depending on the inventory's completeness: the harm round 3 identified is now blocked by a standalone checklist
invariant that holds at any site count, and the durable fix is specified well enough to execute. That is the
difference between an unfinished list and an unsafe design. **Fold R4-1 through R4-10 in without a fifth round;
they are corrections, not re-designs.***

*Of the ten, three most repay the edit. **R4-3** is the only one an implementer would otherwise get wrong: B1-10
kills its mutant through the wrong assertion, and Global constraint 7 asks for a mutant that targets the
`assert.False(IsPermanent)` line specifically — re-specify it as the wrap **plus** the corrected `EqualError`
expectation, so exactly one assertion fails. **R4-4** is the one that keeps the class alive if skipped: choose
the tombstone for site 5 explicitly, add `:22` as site 17, and say in the artifacts that a selector could not
have found it — that sentence is the evidence for §8 item 5's follow-up, and it is worth more than the site
itself. **R4-9** closes the last unproven new assertion in the increment: the increment's stated behavioral
change is the rename, six assertions carry it, and one one-token mutant proves them. The remaining seven are
copy-edits — make them, and start Task 1.***
