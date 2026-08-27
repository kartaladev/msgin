# Plan 032 — adversarial design audit, round 3 (2026-08-22)

Independent Opus subagent, handed the **complete revision-3 bundle together** —
[Spec 018](../specs/018-byte-cap-ceilings.md) + [ADR 0034](../adrs/0034-byte-cap-ceilings.md) +
[Plan 032](032-byte-cap-ceilings.md) — **before any implementation code existed**, per
[`CLAUDE.md`](../../CLAUDE.md)'s design-time gate. The plan is part of what was audited, not merely the context
for it. Round 3 additionally **verifies round 2's fixes**: all twelve findings and both residues in
[`032-audit-round-2.md`](032-audit-round-2.md) were re-checked against revision 3 and against the tree.

**Traceability.** Audits: [Spec 018](../specs/018-byte-cap-ceilings.md),
[ADR 0034](../adrs/0034-byte-cap-ceilings.md), [Plan 032](032-byte-cap-ceilings.md). Prior rounds:
[`032-audit-round-1.md`](032-audit-round-1.md), [`032-audit-round-2.md`](032-audit-round-2.md). Parent artifacts
whose contracts are implicated: [Spec 016](../specs/016-sizing-option-bounds.md),
[ADR 0032](../adrs/0032-sizing-option-bounds.md), [Plan 029](029-sizing-option-bounds.md),
[Plan 030](030-post-029-maintenance.md), [Plan 031](031-group-member-bounds.md),
[ADR 0031](../adrs/0031-nil-option-elements.md), [ADR 0029](../adrs/0029-eip-lexical-alignment.md). Parent
backlog: [`docs/HANDOVER.md`](../HANDOVER.md) §7 item 6.

This record is **evidence-primary** — it is the audit artifact, not a user-facing summary. Every structural claim
in the bundle was re-derived on this tree (`main` at `f733667`, clean worktree, `GOTOOLCHAIN=go1.25.13`,
darwin/arm64); the commands and their output are pasted below. No file in the repository was modified.

> **⚠️ IMMUTABLE EXECUTION RECORD.** This file records what round 3 found, at the time it found it. Do not edit it
> to reflect later decisions, later measurements, or the revised bundle. Corrections belong in a later round's
> record or in the artifacts themselves — with the single, clearly-labelled exception of the **coordinator's
> correction** section at the end, which was appended after the round closed and is marked as such. The
> coordinator's dispositions for these findings live in **Spec 018 / ADR 0034 / Plan 032 revision 4**, each of
> which cites this file.

**Verdict: NOT SAFE TO IMPLEMENT.** 0 BLOCKERs, 3 MAJORs, 4 MINORs.

**The fix verification is the strongest of the three rounds, and it is worth saying so before the findings.**
All twelve round-2 findings landed, both round-1 residues are closed, and **nothing regressed** — which is the
first time that sentence has been writable about this bundle. Round 2's BLOCKER (N-1) is genuinely repaired: the
two-dimensional invariant is correct, the `safe` arm's six `math.MaxInt` rows are explicitly protected, and the
`:61-77` "do not demote these rows" warning is ordered carried forward verbatim in all three artifacts.

**What round 3 finds is the third consecutive instance of one defect.** Round 1 failed at derivation against the
tree. Round 2 failed at the derivation's *predicate*. Round 3 finds that the **widened** predicate still
under-selects — by three sites, one of which is inside a **live assertion message** — and that is no longer a
fixable-by-widening problem. **NEW-2 is the finding that matters**, and its remedy is not a better `grep`: the
arm partition is duplicated in roughly ten prose locations with **no mechanical link to `wantArms`**, and every
round has fixed the named instances while the defect returned through new ones. *Fix the class, not the
instance* — and here the class is the duplication itself.

The remaining two MAJORs are of a different kind. **NEW-1** is one more site of the same under-selection, four
lines from text the increment already rewrites. **NEW-3** is a false premise about a sibling plan's delivery
state, which the bundle turned into an operative instruction. Round 3's four MINORs are all test-design: a mutant
that cannot discriminate, a fixture both larger and weaker than the alternative, an acceptance criterion that is
unachievable as worded, and a sub-check whose command is described but never given.

---

## Part 1 — fix verification for all twelve round-2 findings and both round-1 residues

Each re-checked against revision 3 and against the tree. **LANDED** = the required fix is present and correct.
**LANDED-BUT-FLAWED** = the fix is present but introduces or leaves a defect, tracked as a new finding.
**NOT LANDED** = absent from at least one artifact. **REGRESSED** = the fix made the artifact worse.
**CLOSED** = a residue whose instruction is gone from every artifact.

| # | Round-2 finding | Status | Evidence |
|---|---|---|---|
| **N-1** | BLOCKER — the restated gate invariant is false for the six `safe`-arm rows | **LANDED-BUT-FLAWED** | The invariant is now **two-dimensional** in all three artifacts — Spec §6 AC-4.1's site-7 block (`:875-884`), ADR D-AS trap 4 (`:551-557`), Plan Task 1 Step 6 Trap 3b (`:403-415`). Each states arm-fixes-property first, parameter-type-within-reject-arms second, and each orders `:61-77`'s warning **carried forward verbatim**. The six `int` signatures are re-derived and pasted in the spec. Correct. **But the file carries a THIRD instance of the arm→literal claim, at `:48-49`, which no artifact schedules (→ NEW-1).** |
| **N-2** | MAJOR — the derivation grep's predicate misses two sites | **LANDED-BUT-FLAWED** | The command is widened, the inventory is **14 sites** with `:26` and `:47` added as sites 13/14, `:766` is called out for classification, and *"17 lines"* is corrected to **18**. All three artifacts carry it. **But the widened selector is still a TOKEN ENUMERATION and still under-selects — `:409`, `:601` and `:799-800` are missed, and `:799-800` is inside a live `require.Equal` failure message (→ NEW-2).** |
| **N-3** | MAJOR — Global constraint 6 regressed | **LANDED** | Plan constraint 6 (`:149-167`) restores the **FIXTURE** as the subject, states the ≤ 2 MiB bound, states explicitly that *"a cap may be CONFIGURED at any legal value, including `byteCapCeiling` itself and `byteCapCeiling + 1`"*, and carries a 🔴 block warning the next reader not to restore the revision-2 wording on the strength of the m-13 citation. All five colliding branches and both acceptance criteria are named. Exactly the required fix. |
| **N-4** | MAJOR — the "whichever lands second" protocol is unilateral | **LANDED** | Spec §6 **AC-4.2b** (new, `:931-954`), ADR D-AS (`:584-597`) and Plan Step 11b (`:517-530`) all state unconditional ownership and re-derivation from the tree. Both deleted instructions are named as deleted. The two greps proving Plan 031 has no such task are pasted in all three. |
| **N-5** | MAJOR — call-site totals 48/34/18 vs the tree's 49/40/24 | **LANDED** | Spec §3.1a (`:284-305`) carries **49 / 40 / 24**, enumerates the nine non-call hits, expands the ten `sse_test.go` sites, and adds the arithmetic check `3 + 1 + 12 + 24 = 40`. Plan Step 2 (`:282-291`) now says **re-derive, do not compare**, and distinguishes a total mismatch (record it) from an unscheduled out-of-range hit (halt). Re-derived this round: 49 hits ✅. |
| **N-6** | MAJOR — the parents were scheduled one commit late | **LANDED** | Task 1's title is now *"…**and the parent fold-back** — **ONE COMMIT**"*; Spec 016 / ADR 0032 / HANDOVER are in Task 1's Files (`:249-252`); Step 11b is inside Task 1; Task 2's preamble records that it is *"gates and the status flip only"*. Spec §6 AC-5 and ADR D-AT(b) both name **Task 1** as the carrier. |
| **N-7** | MINOR — `math.MaxInt32 - 1` dissolves the `(n int)` rejection | **LANDED** | Spec §5 gains a dedicated row (`:668`), Spec §3.5 and ADR D-AP(a) (`:352-361`) price it with the three costs, and the plan's "three knobs" box (`:215-221`) drops *"the argument licenses exactly one ceiling"* in favour of the trade. D-AO's reversibility records that lowering the ceiling re-opens the signature question. |
| **N-8** | MINOR — the falsification sweep greps a case the godoc does not use | **LANDED** | Plan Step 11 D-1 (`:478`) uses `grep -rin`, states the expected **13 → 10**, and orders a vacuity probe (plant a lower-case occurrence, confirm `-i` finds it and the case-sensitive form does not, revert). The explanatory 🔴 block (`:483-495`) records why the case-sensitive form got *cleaner* as the task proceeded. Re-derived: `grep -rin 'must be > 0' adapter/http/` → **13** ✅. |
| **N-9** | MINOR — `checkRange`'s godoc enumerates "this package's three sites" | **LANDED in the spec and the plan; ABSENT from the ADR** | Spec §4 item 4 (`:646-654`) and Plan Step 5's fourth godoc edit (`:328-338`) both carry it, correctly scoped (*"the three `int`-typed sites this helper serves"*) and with the both-ends cross-reference. **ADR 0034 does not mention it** — neither in the *"What round 2 changed here"* list (`:22-32`) nor in D-AP(b), which is where the sibling helper's cost is priced. `grep -rn 'N-9'` returns hits in the spec and the plan only. **This is the project's named two-of-three failure, for the second consecutive round** (round 2 caught m-11 in exactly this shape). Recorded here rather than as a numbered finding because the *substance* landed; see the smaller note below. |
| **N-10** | MINOR — Step 8's rewrite orphans the `math` import | **LANDED** | Plan Step 8 (`:441-447`) and Spec §6 AC-2c's 🔴 block (`:736-741`) both say **spell it `math.MaxInt32`**, both cite the single-use grep, and both name it as a stated exception to Global constraint 2. Re-derived: `grep -c 'math\.' adapter/http/exchange_test.go` → **1** ✅. |
| **N-11** | MINOR — `WithMaxEventBytes` is parse-side only; a dropped godoc clause | **LANDED** | Spec §1.3 item 3 carries the parse-side scope; Spec §4 item 3's 🔴 block (`:639-644`) and Plan Step 5 row 6 (`:309`) both **preserve `errors.go:132`'s `(and so by NewSSEParser)`** clause verbatim, with the reason. Re-derived: the clause is at `errors.go:132-133` ✅. |
| **N-12** | MINOR — AC-3's 386 gate is never vacuity-probed | **LANDED** | New Spec §6 **AC-3b** (`:761-776`) and new Plan **Step 9b** (`:458-471`), both four-step: plant a 32-bit-only overflow, exactly one 386 vet failure naming the file, an **amd64-clean** run, revert. Spec §6 AC-7 (`:993-996`) now claims every gate in the bundle carries a probe. |
| **M-8 residue** | Spec §6 AC-4.1 site 2 still prescribed `assert.EqualError` on "the §3.1 render" | **CLOSED** | `grep -n '§3.1 render' docs/specs/018-byte-cap-ceilings.md docs/plans/032-byte-cap-ceilings.md docs/adrs/0034-byte-cap-ceilings.md` → **no hits**. Site 2 (`:824`) now reads *"on **the render THAT ROW produces**"* and names `4611686018427387904` explicitly, in all three twins. |
| **m-11 residue** | ADR 0034 still cited `WithReplayBuffer` at `:976` under a shared-cite sentence | **CLOSED** | The ADR's *"What round 2 changed here"* list records the correction (`:30-32`); the `:976` citation is gone; the bundle-wide **Line/offset convention** is stated once in Spec `:31-33` and the ADR now conforms. |

**Score: 10 LANDED, 2 LANDED-BUT-FLAWED, 2 residues CLOSED. 0 NOT LANDED, 0 REGRESSED.**

**Nothing regressed.** Round 2's N-3 was a fix that made an artifact worse; round 3 found no instance of that
shape. Revision 3 is a monotone improvement over revision 2, which is the first time that has been true of this
bundle.

---

## Part 2 — new findings

| # | Rank | One line |
|---|---|---|
| **NEW-1** | MAJOR | `:48-49`'s *"exceeds every ceiling … the largest is `1<<20`"* goes false in **both** clauses, four lines from site 14, and no artifact schedules it |
| **NEW-2** | MAJOR | The widened selector is still a token enumeration: `:409`, `:601` and `:799-800` are missed — and `:799-800` is inside a **live assertion message**. Three rounds have each fixed the named sites while the defect returned through new ones |
| **NEW-3** | MAJOR | The bundle asserts Plan 030 is undelivered and mid-edit, and turns that into an operative rebase instruction. *(See the coordinator's correction: the DEFECT is real; the CONCLUSION is wrong.)* |
| **NEW-4** | MINOR | Mutant M3-6 changes the assertion **and** the behaviour it checks, so it can only ever pass — it discriminates nothing, and Global constraint 7 forbids exactly that |
| **NEW-5** | MINOR | Branch B1-4's 2 MiB fixture is both larger and weaker than the `1<<20` / `1<<20 + 1` pair, and does not pin the value in the case's own name |
| **NEW-6** | MINOR | AC-1's *"and the knob's effect is observable"* is unachievable for a ceiling-valued cap under constraint 6 — every prescribed observation succeeds identically with the option dropped |
| **NEW-7** | MINOR | The narrow sub-check is described but its command is never given, so a reader cannot run the thing the spec says to run |

---

## MAJOR NEW-1 — `:48-49` is a THIRD instance of the arm→literal claim, it goes false in both clauses, and no artifact schedules it

**The claim under attack.** Spec §6 AC-4.1's 14-site table, site 14:

> | **14** | **`:47`** | Plan 030's `fixed`/`rejects` literal bullet — `- "fixed" (9) and "rejects" (1) → 1<<30 = 1,073,741,824. These rows assert an EqualError against a rendered decimal.` | 🔴 **round-2 N-2, and it goes false TWICE** — the count (9 → 12) **and** the literal… |

Mirrored in ADR D-AS's N-2 block (`:515-517`) and Plan Task 1 Step 6 site 14 (`:372`). The site is cited as
`:47`, and the quoted text stops at *"…against a rendered decimal."*

**The evidence — the bullet does not stop there.** It continues for two more lines, and those two lines carry an
independent claim that this increment also falsifies:

```
$ sed -n '47,52p' sizing_option_class_gate_test.go
//   - "fixed" (9) and "rejects" (1) → 1<<30 = 1,073,741,824. These rows assert
//     an EqualError against a rendered decimal. 1<<30 fits an int32 yet still
//     exceeds every ceiling in the codebase (the largest is 1<<20 = 1,048,576),
//     so it selects the identical out-of-range branch on both architectures
//     while keeping the expected decimal — 1073741824 — architecture-INDEPENDENT.
//     That fixed decimal is the whole point; math.MaxInt here would render
```

**Both clauses of `:48-49` go false**, and for the same reason — this increment introduces a ceiling of
`2,147,483,647`:

| Clause | Text | Goes false because |
|---|---|---|
| (a) | *"`1<<30` … still **exceeds every ceiling in the codebase**"* | `1<<30` = 1,073,741,824 **< `byteCapCeiling`** = 2,147,483,647. After this increment there exists a ceiling `1<<30` does **not** exceed — which is precisely why the bundle's own corollary says `1 << 30` cannot serve the three moved rows |
| (b) | *"(the largest is `1<<20` = 1,048,576)"* | `1<<20` stops being the largest ceiling in the codebase the moment `byteCapCeiling` is declared |

Arithmetic, first-hand: `1<<30 = 1073741824`, `byteCapCeiling = math.MaxInt32 = 2147483647`,
`1073741824 < 2147483647`. ✅

**Why this is a MAJOR and not a wording MINOR.** Three reasons, compounding.

1. **The bundle already knows the fact and states it three times** — Spec §6 AC-4.1's *"🔴 `1 << 30` CANNOT be
   used for the three moved rows. `1,073,741,824 < byteCapCeiling = 2,147,483,647`, so it would be
   **accepted**"* (`:886-888`), ADR D-AP's corollary 1 (`:363-364`), and the plan's "three knobs" corollary
   (`:223-225`). It draws the *operational* conclusion (do not use `1<<30` for the moved rows) and never draws
   the *documentary* one (the file's justification for `1<<30` in the other nine rows now has a false premise).
2. **It is four lines from text the increment rewrites.** An implementer editing site 14 at `:47` has their
   cursor on the previous line. Leaving `:48-49` untouched produces a bullet whose first sentence is corrected
   and whose next two sentences justify the corrected value with a claim the same commit falsifies.
3. **The bundle's own dimension-2 wording is ALREADY correctly narrowed, so the file will contradict itself.**
   Spec §6 AC-4.1's replacement invariant reads *"`int` → `1<<30` (fits int32, **exceeds every `int`-typed
   ceiling**, renders `1073741824` on every architecture)"* — note **`int`-typed**. ADR D-AS trap 4 and Plan
   Trap 3b carry the same narrowing. So after the increment the header block would contain the narrowed,
   true statement at `:55-59`'s replacement **and** the unnarrowed, false statement at `:48-49`, seven lines
   apart, describing the same literal. That is the exact shape of CLAUDE.md's stored lesson *docs can contradict
   the code they describe* — with the added indignity that the correct wording is already in the same file.

**Required fix.** Extend site 14 from `:47` to **`:47-49`**, and specify the replacement text, not merely the
count:

- **The count:** `9` → `12` (already scheduled).
- **The literal:** the bullet must acknowledge that `fixed` now carries two literals (this is site 7's
  correction, and `:47` is its other half — already scheduled).
- **NEW: narrow clause (a)** to *"exceeds every **`int`-typed** ceiling in the codebase"*, matching the
  dimension-2 wording the bundle already uses.
- **NEW: state the reason inline**, so the next reader does not re-broaden it: *"`byteCapCeiling` is an
  `int64` ceiling above `1<<30`; that is why the three `int64`-typed rows keep `1<<62`."*
- **NEW: replace clause (b)'s parenthetical.** `1<<20` is no longer the largest ceiling. Either name
  `byteCapCeiling` as the largest and `1<<20` as the largest `int`-typed one, or delete the parenthetical —
  it is an enumeration, and the project's stored lesson is *assert the invariant, not the enumeration*.

Mirror into ADR D-AS's N-2 block and Plan Task 1 Step 6 site 14. The delivery checklist's *"All **14** derived
gate sites edited or explicitly classified, including `:26` and `:47`"* should read `:47-49`.

---

## MAJOR NEW-2 — the widened selector is STILL a token enumeration; three more partition-recording sites are missed, one of them inside a live assertion message

**This is the third consecutive round in which the gate site inventory has been claimed complete and been
wrong.** Round 1 (B-3): seven sites of at least twelve, offsets stale. Round 2 (N-2): twelve sites, predicate
selects on `deferred` while the property is the `fixed` partition, two sites missed. Round 3: fourteen sites,
predicate widened, **three more missed**. Each round's remedy was a better `grep`; each round's remedy was
defeated by a site the new `grep` did not happen to cover.

**The claim under attack.** Spec §6 AC-4.1 (`:808-812`), ADR D-AS (`:521-523`), Plan Task 1 Step 6 (`:345-347`),
all identical:

```bash
grep -nE 'deferred|DEFERRED|"fixed"|9/1/3/6|9 \+ 1 \+ 3 \+ 6|\(9\)|\(3\)' sizing_option_class_gate_test.go
```

followed by *"edit **every** hit that changes"* and a 14-site table, and by the assurance that
*"over-inclusion is safe … under-inclusion ships."*

**The evidence — three partition-recording sites contain none of the seven alternatives.** Re-derived on this
tree (the widened form returns 42 lines):

```
$ grep -n 'the 9 class members this increment bounds' sizing_option_class_gate_test.go
409:		// ---- arm: fixed — the 9 class members this increment bounds ----

$ grep -n 'math.MaxInt, NOT the 1<<30' sizing_option_class_gate_test.go
601:		// math.MaxInt, NOT the 1<<30 the reject arms use (Plan 030 Task 2):

$ sed -n '798,802p' sizing_option_class_gate_test.go
	require.Equal(t, wantArms, gotArms,
		"Spec 016 §2.1's arm table and §6 AC-5 fix EVERY key's arm, not just the per-arm counts: 9 class "+
			"members fixed here, 1 that rejects without being a class member (msghttp.WithSuccessStatus), "+
			"3 with a deferred ceiling (§3.8), 6 safe (4 AST + 2 manual). Moving a row between arms is a "+
			"SPEC change — update §2.1 and §6 AC-5, do not just edit this map")
```

| Site | Text | Goes false because | Why the widened grep misses it |
|---|---|---|---|
| **`:409`** | `// ---- arm: fixed — the 9 class members this increment bounds ----` | the arm becomes **12** | `fixed` appears **unquoted**; the alternative is `"fixed"` with literal quotes |
| **`:601`** | `// math.MaxInt, NOT the 1<<30 the reject arms use (Plan 030 Task 2):` | after the move the reject arms use **two** literals, `1<<30` **and** `1<<62` | contains none of the seven tokens |
| **`:799-800`** | `"…not just the per-arm counts: 9 class "+ "members fixed here, 1 that rejects…"` | `9` becomes **12**; and this is a **live `require.Equal` failure message**, not a comment | `:799` has no token; `:800` has `fixed` unquoted |

Verified mechanically — the widened selector's hit set does not contain `:409`, `:601`, `:799` or `:800`:

```
$ grep -nE 'deferred|DEFERRED|"fixed"|9/1/3/6|9 \+ 1 \+ 3 \+ 6|\(9\)|\(3\)' sizing_option_class_gate_test.go \
    | cut -d: -f1 | grep -cx -e 409 -e 601 -e 799 -e 800
0
```

**`:799-800` is the one that condemns the method.** Site 12 of the 14-site table reads
*"| 12 | `:758`, `:761`, `:801`, `:805` | four prose strings **inside live assertion messages** naming the
`9/1/3/6` split | they are arguments to `require.Equal`, not comments |"*. `:799` and `:800` are **the same
`require.Equal` call**, two lines above `:801`. The inventory reached into the middle of a five-line string
concatenation, took lines 3 and 5, and left lines 1 and 2 — because lines 3 and 5 happen to contain the token
`deferred` and `9/1/3/6` respectively, and lines 1 and 2 do not.

**The consequence is concrete and shippable.** An implementer who satisfies the delivery checklist verbatim —
*"All **14** derived gate sites edited or explicitly classified"* — ships a green gate whose own failure message,
when a future contributor does move a row, reads:

> `Spec 016 §2.1's arm table and §6 AC-5 fix EVERY key's arm, not just the per-arm counts: 9 class members fixed
> here, 1 that rejects without being a class member (msghttp.WithSuccessStatus), 3 with a deferred ceiling
> (§3.8), 6 safe (4 AST + 2 manual).`

— over a `wantArms` map of **twelve** `fixed` entries and **no** `deferred` entry. The gate's diagnostic
contradicts the gate's data. That is worse than a stale comment, because the message is what a failing
contributor reads to learn what they violated.

**Required fix — do BOTH halves. The second is the one that matters.**

**(i) Add the three sites.** `:409` and `:601` as sites 15 and 16; **fold `:799-800` into site 12**, whose line
list becomes `:758`, `:761`, **`:799`, `:800`**, `:801`, `:805` — six lines, of which four are one contiguous
string literal. Say in the table that site 12 is a *single* `require.Equal` message spanning `:799-801` plus a
second at `:805`, so the next reader edits the message, not the lines.

**(ii) STOP TREATING A BETTER GREP AS THE REMEDY.** Widening the token list is what round 2 did, and it is what
produced this finding. The durable defect is structural: **the arm partition is duplicated in roughly ten prose
locations — the header's arm list, the header's arithmetic identity, Plan 030's per-arm literal block, the `arm`
field's doc comment, the section banners, the `wantArms` comment's illustrative map, and two live assertion
messages — with NO MECHANICAL LINK to `wantArms`.** Nothing fails when one of them drifts. Every count in that
list is a hand-maintained copy of a value the test already computes.

Two things are required:

1. **Select genuinely broadly, and accept the noise.** Something of the shape

   ```bash
   grep -nE 'deferred|DEFERRED|fixed|rejects|safe|1<<30|1<<20|1<<62|9/1/3/6|9 \+ 1 \+ 3 \+ 6|[0-9]+ (class|rows|AST)' \
     sizing_option_class_gate_test.go
   ```

   returns **104** of the file's 812 lines on this tree, and its hit set is a strict superset of the widened
   form's 42 — verified: every one of the 29 sites this bundle and this round have named is present, and the
   four the narrow-only tokens contribute (`:58`, `:565`, `:758`, `:805` — uppercase `DEFERRED` and the
   `9/1/3/6` string) are retained. 104 lines is a ten-minute classification pass and it is the correct trade
   against a defect that has now shipped past three inventories.

2. **Record as a follow-up backlog item that the REAL fix is to derive the header's counts from the table at
   test time.** The gate already computes `byArm` from `tests`. The two live assertion messages at `:799-801`
   and `:803-805` should be **formatted from `byArm`/`wantArms`** rather than spelling the partition as a
   literal string; the header block's counts should carry a one-line pointer to the assertion rather than
   restating the numbers. Then a partition change breaks exactly one place, and no `grep` is needed at all.
   That is out of scope for this increment — it is a refactor of a delivered gate — but it must be **written
   down**, because the alternative is a fourth round finding a fifteenth site.

**Say all of this plainly in the plan.** Not *"the inventory is now 17 sites"* — that is the sentence three
revisions have written. Say: **three consecutive audit rounds have each fixed the named sites while the same
defect returned through new ones, the inventory is a symptom, and the cause is that ten locations restate a
partition the test already computes.**

---

## MAJOR NEW-3 — the bundle asserts Plan 030 is undelivered and mid-edit, and makes that an operative instruction

> **🔴 READ THE COORDINATOR'S CORRECTION at the end of this record before acting on this finding.** The
> **defect** identified here is real and the required fix stands. The **conclusion** — that only Plan 030 Task 2
> landed — is **WRONG**, and the coordinator has corrected the cause. The finding is preserved verbatim because
> this record is immutable; the correction is appended, clearly labelled, as a later addition.

**The claim under attack.** Three statements in Plan 032, presented as current fact:

- **`:87-88`:** *"**030 and 031 are both TAKEN** by concurrent, **undelivered** work."*
- **`:95`**, the file-overlap table's 030 row: *"**`adapter/http/options.go` and `adapter/http/helpers.go` are
  being edited RIGHT NOW.** Line offsets in this plan will drift. **Rebase on 030, do not merge.**"*
- **Spec `:62-64`:** *"`adapter/http/options.go` and `adapter/http/helpers.go` are **being edited concurrently
  by Plan 030**; the *anchors* below … are what this spec is written over, not the offsets."*

These are contradicted **inside the same plan**, twice: `:185` says *"Plan 030's conversion had already landed
(`d2c69fe`)"*, and `:255-256` says *"[Plan 030] has **landed** — the gate's `fixed`/`rejects` arms are already at
`1<<30` and its `safe` arm at `math.MaxInt`."* Spec `:308` says *"Plan 030 has landed"* while Spec `:62` says it
is editing files right now.

**The evidence as this round derived it.**

```
$ grep -c '\[x\]' docs/plans/030-post-029-maintenance.md
0
$ grep -c '\[ \]' docs/plans/030-post-029-maintenance.md
32
$ git log --oneline | grep -i 030
d2c69fe test(core): make the sizing tests compile on 32-bit
```

Thirty-two unticked checkboxes, none ticked, and one commit mentioning 030 — which is Task 2, the 386 arm, and
is exactly the task the plan's `:185` and `:255` cite as landed. **This round concluded that Task 2 landed and
Tasks 1 and 3 did not**, and that the plan's `:95` instruction is therefore live while its `:255` claim is
overstated.

**Why it is a MAJOR under either reading.** The bundle asserts a sibling's delivery state in three places with
three different answers — *undelivered*, *has landed*, *being edited right now* — and one of them
(`:95`) is an **operative instruction** telling the implementer to rebase before touching `options.go` and
`helpers.go`, which are two of Task 1's four production files. An implementer cannot act on a state the bundle
describes three ways. Whichever value is true, the other two are false, and the plan has no single place a
reader can go to find out.

**It also propagates a narrow claim that is TRUE and must be kept.** `:255-256`'s substance —
*"the gate's `fixed`/`rejects` arms are already at `1<<30` and its `safe` arm at `math.MaxInt`"* — is verified
first-hand this round: `:47` reads `"fixed" (9) and "rejects" (1) → 1<<30`, `:61` reads `"safe" (6) →
math.MaxInt`, and the rows at `:412`-`:511` carry `1<<30`. Every offset the 14-site table cites is a
post-conversion offset. **Whatever is decided about Plan 030's task-level state, that claim must survive**,
because the entire inventory depends on it.

**Required fix.** Establish the state **once**, from a reliable signal, and make every other mention point at
it rather than restate it. Delete `:87-88`'s *"undelivered"* and `:95`'s *"being edited RIGHT NOW"* / Spec
`:62-64`'s *"being edited concurrently"* if they are false; keep `:95` as an operative instruction only if they
are true. Name the command that settles it, and note that **checkbox state is not that command** — a plan whose
boxes were never ticked reads identical to a plan that was never executed.

---

## MINOR NEW-4 — mutant M3-6 changes the assertion AND the behaviour it checks, so it can only ever pass

**The claim under attack.** Plan Task 1's gate-mutant table (`:593`):

> | M3-6 | drop `assert.False(t, msgin.IsPermanent(err), …)` from one moved row **and wrap that sentinel in
> `msgin.Permanent`** | nothing — **which is the point**: run it to prove the assertion is load-bearing before
> relying on it |

**The evidence — the mutant is not a discriminating experiment.** It applies two edits at once:

1. deletes the assertion, and
2. changes the production behaviour the assertion checks.

With the assertion gone, no case observes `IsPermanent`, so the suite is green **whatever** edit (2) does. Edit
(2) is therefore inert, and the mutant reduces to *"delete an assertion, observe that its subject is no longer
asserted"* — which is a tautology, not evidence. It **cannot fail**, and a mutant that cannot fail measures
nothing about the assertion's power.

**It also collides with the plan's own Global constraint 7** (`:168-170`):

> *"**Mutation-prove every new assertion** with a mutant that targets **that** assertion … **A case that survives
> its own mutant is rewritten.**"*

M3-6 is *specified* to survive. Under constraint 7 read literally, the case it targets must therefore be
rewritten — which is not what the plan intends and not what should happen.

**The experiment the plan actually wants already exists, one table up.** B1-10 (`:555`):

> | B1-10 | **the classification** (round-1 M-8) | `assert.False(t, msgin.IsPermanent(err), …)` on every rejecting
> case, and on the three moved gate rows | **wrap the return in `msgin.Permanent(...)`** ⇒ every rejecting case
> fails |

That is the correct shape: **keep** the assertion, apply only edit (2), and the row goes red. A killed mutant.
It already covers the three moved gate rows explicitly (*"and on the three moved gate rows"*), which is exactly
M3-6's stated scope.

**Required fix — one of two, both acceptable.**

- **(a) Restate M3-6 as an explicit two-arm pair**, so the discrimination is visible: **arm 1** — keep the
  assertion, wrap the sentinel in `msgin.Permanent` ⇒ the row **fails** (this is the kill); **arm 2** — drop the
  assertion, keep the wrap ⇒ the row **passes** (this is what proves arm 1's failure came from the assertion and
  not from something else). Two arms, opposite outcomes, is what "load-bearing" means. Note that arm 1 is
  B1-10 restricted to the moved rows.
- **(b) Delete M3-6 as redundant**, and add one clause to B1-10 recording that it covers the moved rows' copy of
  the assertion.

**(b) is the recommendation** — the bundle already has the experiment, and a mutant table whose rows are all
kills is easier to audit than one carrying a documented non-kill next to M1-7's genuine, architecture-forced
non-kill. If (a) is preferred, the two-arm form must be spelled out; *"run it to prove the assertion is
load-bearing"* does not describe an experiment a reader can perform.

---

## MINOR NEW-5 — B1-4's 2 MiB fixture is both larger and weaker than the pair that would pin the default

**The claim under attack.** Plan Task 1's branch table (`:549`):

> | B1-4 | body gate, `!set` → default | `NewConfig_default_body_cap_is_1MiB` (unset ⇒ a **2 MiB** body is
> rejected by `DecodeRequest`) — the largest fixture in the increment, at Global constraint 6's stated ≤ 2 MiB
> bound | delete the default assignment ⇒ the cap reads `0`, the case fails |

and Global constraint 6 (`:156-157`), which is written **around** this fixture: *"Branch B1-4's **2 MiB** body
against the 1 MiB default is the largest fixture in this increment and is the bound's intended ceiling, not an
exception to it."*

**The evidence — the fixture is larger than it needs to be and proves less than its name claims.**

1. **Its listed mutant is killed by a one-byte body.** The mutant is *"delete the default assignment ⇒ the cap
   reads `0`"*. A cap of `0` rejects **everything**, including a 1-byte body. The 2 MiB fixture is roughly two
   million times larger than the mutant requires.
2. **A 2 MiB rejection proves only that the default lies in `(0, 2 MiB)`.** It is consistent with a default of
   1 MiB, 512 KiB, 1 byte, or `1<<20 - 1`. The case is named `NewConfig_default_body_cap_is_1MiB` and does not
   establish `1 MiB`.
3. **The pair that does pin it is strictly stronger and smaller at the peak.** `1<<20` bytes **accepted** and
   `1<<20 + 1` bytes **rejected** brackets the default at exactly `1048576` — the only value satisfying both
   arms. Peak fixture drops from 2 MiB to `1 MiB + 1`, halving the largest allocation in the increment, and the
   pair additionally lands on `MaxBytesReader`'s exact boundary, which is where an off-by-one in the comparison
   would live. It also kills the listed mutant (cap `0` rejects the `1<<20` accept-arm) and kills two mutants
   the 2 MiB fixture does not: `default = 1<<20 + 1` (accept-arm still passes, reject-arm goes green ⇒ caught)
   and `default = 1<<20 - 1` (accept-arm goes red ⇒ caught).

**Why it is a MINOR rather than a MAJOR.** The branch itself — `!set` → default — *is* covered by the 2 MiB
fixture, so the hot-path requirement is met and nothing ships uncovered. What is wrong is that the increment's
single largest allocation buys the weakest available assertion, and Global constraint 6 has been written to
accommodate it. The constraint is now shaped by a fixture that need not exist at that size.

**Required fix — one of two.**

- **(a) Re-specify B1-4 as the pair** — `1<<20` accepted / `1<<20 + 1` rejected, a two-case `table-test` per
  `table-test`'s assert-closure form — and **tighten Global constraint 6's bound accordingly**: the largest
  fixture in the increment becomes `1 MiB + 1`, not 2 MiB. Update the constraint's closing sentence, which
  currently names B1-4's 2 MiB body as *"the bound's intended ceiling"*. **Recommended.**
- **(b) Keep 2 MiB and correct the justification** — rename the case to what it proves
  (`NewConfig_default_body_cap_binds`, not `…_is_1MiB`), and record in the branch table that the fixture is
  sized for readability rather than for discrimination, with the mutant it actually kills.

Either is defensible; what is not is the current state, where the case's name asserts a value the case cannot
distinguish and the plan's fixture bound is derived from it.

---

## MINOR NEW-6 — AC-1's "and the knob's effect is observable" is unachievable for a ceiling-valued cap under constraint 6

**The claim under attack.** Spec §6 AC-1 (`:679-683`):

> For each of the three knobs … `NewConfig(WithX(byteCapCeiling))` → nil error, non-nil `*Config`, **and the
> knob's effect is observable**;

with the definition supplied two paragraphs later (`:691-693`) and repeated in the plan (`:577-580`):

> *"**"Observable effect" is the definition in force**, per Spec 016 §6's *"its product is usable"* …
> `WithMaxBodyBytes` via `DecodeRequest` on a small body; `WithMaxResponseBytes` via an `httptest` round-trip;
> `WithMaxEventBytes` via `NewSSEParser` + `Next` on a small event."*

**The evidence — an effect that is identical with and without the option is not an effect of the option.**
Global constraint 6 forbids a fixture larger than ~2 MiB. `byteCapCeiling` is 2,147,483,647. So every prescribed
observation runs a **small** fixture against a **ceiling-sized** cap, and:

| Prescribed observation | Under `WithX(byteCapCeiling)` | Under the 1 MiB default | With the option dropped entirely |
|---|---|---|---|
| `DecodeRequest` on a small body | succeeds | succeeds | succeeds |
| `httptest` round-trip, small response | succeeds | succeeds | succeeds |
| `NewSSEParser` + `Next` on a small event | succeeds | succeeds | succeeds |

Three identical columns. The assertion cannot distinguish the configured ceiling from the default, and cannot
distinguish either from no option at all — which is the definition of an unobservable knob setting. **This is
not a defect in the tests; it is a property of a ceiling.** Observing a 2 GiB cap's effect requires a fixture
above 1 MiB *and* below 2 GiB to behave differently under the two settings — which the small-fixture rule
forbids at the top end and the 1 MiB default forbids at the bottom. There is no legal fixture that
discriminates.

**The bundle already contains the honest formulation, one clause away.** Spec 016 §6's phrase is *"its product
is **usable**"* — not *"its effect is observable"*. "Usable" is exactly what the three prescribed checks
establish: the `*Config` the ceiling produced is not a broken object; `DecodeRequest`, the round-trip and the
parser all work on it. That is a real and worthwhile assertion, and it is achievable. AC-1 restates it as
"observable", which is a stronger claim it cannot support. The plan's own heading for this paragraph is
`**"Its product is usable"**` (`:577`) — the plan has the right word and the spec has the wrong one, in the
paragraph the plan is quoting.

**Required fix.** Word AC-1's third clause as **"and its product is usable"**, matching Spec 016 §6 and the
plan's own heading, and add a sentence stating plainly what is and is not proven:

> **The ceiling's effect is unobservable by construction, and that is accepted, not a gap.** No legal fixture
> distinguishes a 2 GiB cap from the 1 MiB default: constraint 6 forbids a fixture above ~2 MiB, and any fixture
> below 1 MiB passes under both. The ceiling is therefore proven **at the constructor only** — accepted at
> `byteCapCeiling`, rejected at `byteCapCeiling + 1` (AC-2's upper arm) — and the separate property *"the cap
> caps"* is proven at **small `n`** with a small fixture, where it is a fact about the comparison rather than
> about the ceiling value. The two together are the whole contract; neither alone is.

Mirror the clause into Plan `:577-580` (which needs only the sentence, since it already uses the right heading)
and into ADR 0034 wherever AC-1 is characterised.

---

## MINOR NEW-7 — the narrow sub-check is described but its command is never given

**The claim under attack.** Spec §6 AC-4.1 (`:814-817`):

> *"The narrow revision-2 form is retained **only** as a sub-check, and its own output is **18 lines** (`:35 :38
> :55 :58 :401 :539 :565 :570 :579 :588 :758 :761 :782 :783 :784 :801 :803 :805`), not 17."*

and Plan Task 1 Step 6 (`:351`): *"The narrow revision-2 form returns **18** lines, not the 17 it pasted; **run
it too** if you want the smaller list, but classify against the wide one."*

**The evidence.** Neither artifact contains the narrow command. `grep -n "grep -n 'deferred"` across the three
files returns nothing; only the widened `grep -nE …` form appears. A reader told to *"run it too"* has the
expected output (18 lines, with their offsets) and no command to produce it. The ADR does not carry it either.

This is small, but it is the same class as N-8 and NEW-2: a check specified by its **result** rather than by its
**procedure** cannot be re-derived, and this project's standing rule is *derive move-lists mechanically*. A
pasted 18-line expectation with no command is a transcription, which is precisely what the surrounding
paragraphs forbid.

**Required fix — one of two.** Either **paste the command**:

```bash
grep -nE 'deferred|DEFERRED|9/1/3/6|9 \+ 1 \+ 3 \+ 6' sizing_option_class_gate_test.go
```

(verified this round: **18** lines, at exactly the offsets the spec lists ✅), or **drop the sub-check
entirely** — it is subsumed by the broad form NEW-2 requires, and keeping a second, weaker selector alongside it
invites a future reader to use the wrong one. **Dropping is the recommendation**, given NEW-2.

---

### Smaller note 1 — Step 11 D-3's count assertion is not wrap-safe, and the phrase it counts is six words long

Plan Task 1 Step 11 (`:480`):

> | D-3 | *"the disclosure is actually present"* | `grep -c 'not a safety guarantee' adapter/http/options.go` →
> **3**, **vacuity-probed**: delete one occurrence, confirm the count reads 2, restore |

Round 2's N-8 caught the *case* defect in D-1's grep and this revision fixed it. **The same class of defect is
live in D-3, in two ways**, and this is the round-2 lesson applied one row down:

1. **`grep -c` counts matching LINES, not occurrences.** Two occurrences on one line count as 1.
2. **The phrase is six words and the godoc wraps at ~80 columns**, so an occurrence split across a `// `
   continuation is invisible. Plan 030 Task 1 hit exactly this and adopted a `perl -0777` whole-file slurp for
   it — the established house form.

Demonstrated first-hand on a two-occurrence probe, one wrapped and one not:

```
$ printf '// alpha not a\n// safety guarantee beta\n// gamma not a safety guarantee\n' > probe.go
$ grep -c 'not a safety guarantee' probe.go
1
$ perl -0777 -ne 's{\n\s*//\s*}{ }g; my $c = () = /not a safety guarantee/g; print "$c\n"' probe.go
2
```

The naive form reports 1 where the truth is 2. On the real file today the count is legitimately **0** (the
disclosure does not exist yet — it is what the increment adds), so the assertion is `→ 3` after Step 5.

**Required fix.** Replace D-3's command with the wrap-tolerant form above (Plan 030 Task 1's precedent),
state the expectation as **3 occurrences**, and keep the vacuity probe — which under the naive form would
have been satisfiable by a wrapped occurrence that was never counted in the first place.

### Smaller note 2 — the over-inclusion account names two ranges and should name the two individual lines too

Spec §6 AC-4.1's assurance (`:804-806`) reads *"accept over-inclusion — the rows at `:412`-`:511` and
`:772`-`:780` classify as 'no change' in one pass."* ADR D-AS says the same in one clause (`:525`).

Two **individual** over-inclusion hits fall outside both ranges and are unaccounted for:

```
$ sed -n '33p;521p' sizing_option_class_gate_test.go
//                          in neither "fixed" (not a class member) nor "safe"
		// M2 (Task 7 review): this row previously sat in the arm labelled "fixed",
```

- **`:33`** — part of the `rejects` bullet's explanation of why `WithSuccessStatus` belongs in neither `fixed`
  nor `safe`. Still true after the move; **no change**.
- **`:521`** — the historical M2 note recording that this row *previously* sat in `fixed`. Past tense, still
  true; **no change**.

Both are matched by the widened grep (both contain `"fixed"`), so an implementer classifying its 42 hits will
meet them and find no guidance. Naming them costs one clause and removes two of the 42 from the "why is this
here?" pile — which matters because the classification pass is exactly where an over-broad selector's cost is
paid, and NEW-2 makes that pass larger, not smaller.

**Required fix.** Extend the over-inclusion sentence: *"…the rows at `:412`-`:511`, the `wantArms` entries at
`:772`-`:780`, and the two individual lines `:33` (the `rejects` bullet's neither-nor clause) and `:521` (the
historical M2 note, past tense) all classify as 'no change' in one pass."* Mirror the clause into ADR D-AS.

---

## Checked and found CLEAN — 16 rows, newly verified this round

Round 4, if there is one, should not re-derive these. Each was checked first-hand against the tree at `f733667`,
and each is a claim round 2 did **not** verify — round 2's own 16 clean rows are not repeated here.

| # | Claim | Where | Verification |
|---|---|---|---|
| 1 | The M-8 residue is gone from all three artifacts | Spec §6 AC-4.1 site 2 | `grep -n '§3.1 render' docs/specs/018-*.md docs/plans/032-byte-cap-ceilings.md docs/adrs/0034-*.md` → **no hits**; site 2 now reads *"the render THAT ROW produces"* and names `4611686018427387904` ✅ |
| 2 | The narrow revision-2 form returns exactly 18 lines, at the offsets the spec lists | Spec §6 AC-4.1 | `grep -n 'deferred\|DEFERRED\|9/1/3/6\|9 + 1 + 3 + 6' … \| wc -l` → **18**; offsets match `:35 :38 :55 :58 :401 :539 :565 :570 :579 :588 :758 :761 :782 :783 :784 :801 :803 :805` exactly ✅ |
| 3 | The widened form returns 42 lines and is a strict superset of the narrow 18 | Spec §6 AC-4.1, ADR D-AS, Plan Step 6 | `grep -nE '…' \| wc -l` → **42**; all 18 narrow offsets present ✅ |
| 4 | `:26` and `:47` are real, contain none of the narrow tokens, and go false as sites 13/14 describe | Spec sites 13-14 | `:26` = `- "fixed"    (9) — the fault is reported…`; `:47` = `- "fixed" (9) and "rejects" (1) → 1<<30 …` ✅ (their *extent* is the NEW-1 defect, not their existence) |
| 5 | `:766` carries `map[string]int{"fixed": 9, ...}` inside the `wantArms` rationale comment | Spec §6 AC-4.1's "also classify" | `:766` = `// a count map (map[string]int{"fixed": 9, ...}) is blind to a PAIRWISE` — illustrative, correctly flagged as decide-don't-skip ✅ |
| 6 | Site 12's four cited offsets are real and are `require.Equal` arguments, not comments | Spec site 12 | `:758`, `:761` are inside the `wantArms` rationale comment; `:801`, `:805` are inside the two `require.Equal` message strings ✅ (the message at `:799-801` extends two lines further up — NEW-2) |
| 7 | The `safe` arm's six parameters are all `int` and all assert acceptance | Spec §6 AC-4.1's N-1 block | `resilience/breaker.go:51`, `endpoint/flowcontrol.go:144`, `:166`, `resilience/ratelimit.go:42`, `adapter/memory/queuestore.go:182`, `channel/queuechannel.go:50` — six `int` signatures ✅; round 2's finding correctly folded |
| 8 | `1<<30 < byteCapCeiling`, so `1<<30` cannot serve the three moved rows | Spec §3.5, §6 AC-4.1; ADR D-AP corollary 1; Plan "three knobs" | `1073741824 < 2147483647` ✅ — and this is what makes NEW-1's `:49` clause false |
| 9 | The 386 vet and build gates both exit 0 on this tree | Spec §6 AC-3; Plan Step 9 | `GOTOOLCHAIN=go1.25.13 GOARCH=386 GOOS=linux go vet ./...` → **exit 0**, zero output ✅ |
| 10 | `math` is used exactly once in `exchange_test.go`, so N-10's fix is necessary | Spec §6 AC-2c; Plan Step 8 | `grep -c 'math\.' adapter/http/exchange_test.go` → **1** ✅ |
| 11 | The case-insensitive `must be > 0` sweep returns 13 hits, as Step 11 D-1 now states | Plan Step 11 D-1 | `grep -rin 'must be > 0' adapter/http/ \| wc -l` → **13** ✅ (7 godoc sentences + 6 sentinel messages) |
| 12 | `checkRange` has exactly three call sites and the two godoc sentences N-9 names are at `helpers.go:51` and `:57` | Spec §4 item 4; Plan Step 5 | `grep -c 'checkRange(' adapter/http/options.go` → **3**, `…/helpers.go` → **1** (the declaration); `:51` = *"each of this package's three sites"*, `:57` = *"All three sites are R1"* ✅ |
| 13 | `errors.go:132`'s `(and so by NewSSEParser)` clause exists and is what N-11 asks to preserve | Spec §4 item 3; Plan Step 5 row 6 | `errors.go:132-133` = *"ErrInvalidMaxEventBytes is returned by NewConfig (and so by / NewSSEParser) when an explicit WithMaxEventBytes is <= 0"* ✅ |
| 14 | The call-site grep returns 49 hits, as §3.1a's re-derived total states | Spec §3.1a; Plan Step 2 | `grep -rn 'WithMaxBodyBytes(\|WithMaxResponseBytes(\|WithMaxEventBytes(' --include='*_test.go' . \| wc -l` → **49** ✅ — N-5 correctly folded |
| 15 | Plan 031 has ten tasks and none is a Spec 016 fold-back, so N-4's unilateral-protocol argument holds | Spec §6 AC-4.2b; ADR D-AS; Plan Step 11b | `grep -c '^## Task' docs/plans/031-group-member-bounds.md` → **10**; `grep -n 'Spec 016 §2.1' …031…` → no hits ✅ |
| 16 | The hazard disclosure does not yet exist, so D-3's assertion is a genuine post-condition | Plan Step 11 D-3 | `grep -c 'not a safety guarantee' adapter/http/options.go` → **0** today ✅ (the wrap-safety of the *command* is smaller note 1) |

**Also clean, and worth preserving as reasoning rather than derivation.** Revision 3's repair of N-1 is the best
work in this bundle's history: it identifies that the arm and the parameter type **compose** rather than
compete, orders the `:61-77` warning carried forward verbatim rather than replaced, and pastes the six `int`
signatures as evidence in the spec rather than asserting them. Global constraint 6's restoration (N-3) is the
second-best: it re-identifies the hazard as the *fixture*, enumerates the five branches revision 2 would have
deleted, and — unusually — warns the next reader not to re-apply the round-1 citation that produced the
regression. The unconditional-ownership rule (N-4) removes a two-party protocol in favour of a
re-derive-from-the-tree instruction that is correct under both landing orders by construction. Those three
should survive round 4 unchanged.

---

## Auditor's method note

Every command in this record was run on the tree at `f733667` (clean worktree) with `GOTOOLCHAIN=go1.25.13` on
darwin/arm64. The narrow and widened grep line counts and their hit sets, the `:409` / `:601` / `:799-800`
non-membership check, the `:47-49` bullet's full extent, the six `safe`-arm signatures, the 49-hit call-site
total, the 13-hit case-insensitive `must be > 0` sweep, the `checkRange` call-site count, the single `math` use
in `exchange_test.go`, the `errors.go:132` clause, the 386 vet exit code, the Plan 031 task count, and the
`grep -c` versus wrap-tolerant probe are all first-hand output, not transcription. No file in the repository was
modified; the wrap-safety probe file lived outside it and was discarded.

**What round 3 deliberately did NOT re-derive:** round 2's 16 clean rows, which stand. The 16 rows above are all
claims round 2 did not check.

---

**VERDICT: NOT SAFE TO IMPLEMENT.**

*Round 1 failed at derivation against the tree. Round 2 failed at the derivation's predicate. Round 3 finds the
widened predicate still under-selecting — by three sites, one of them inside a live assertion message — and that
is the finding that matters, because it is no longer fixable by widening. The arm partition is restated in
roughly ten prose locations with no mechanical link to the table that computes it; three rounds have each
repaired the named instances and each been overtaken by new ones. The remedy is to stop enumerating and start
deriving: select broadly enough to be noisy, and schedule the refactor that makes the header's counts a function
of `wantArms` rather than a copy of it.*

---

## 🔵 COORDINATOR'S CORRECTION — a LATER ADDITION, not part of the auditor's record (2026-08-22)

> **This section was written by the coordinator AFTER round 3 closed.** It is appended here, clearly labelled,
> because leaving a proven-false conclusion unmarked inside an immutable record would mislead every later reader.
> **Nothing above this line was altered.** The correction concerns **NEW-3 only**; NEW-1, NEW-2 and NEW-4 through
> NEW-7 are unaffected and stand as written.

**NEW-3's DEFECT is real. NEW-3's CONCLUSION is wrong, and the cause was mine.**

The auditor concluded that only Plan 030 Task 2 had landed. It had not; **all three Plan 030 tasks are
delivered.** The auditor's two signals were both misleading, and both because of a bookkeeping failure in this
repository rather than an error of method:

| Signal the auditor used | What it returned | Why it misled |
|---|---|---|
| `grep -c '[x]' docs/plans/030-post-029-maintenance.md` → **0** | zero ticked boxes | **The checkboxes were never ticked during execution.** A plan whose boxes were never ticked reads identically to a plan that was never executed. This was my failure, not the auditor's. |
| `git log --oneline \| grep -i 030` → **one commit** | `d2c69fe` only | **Two of the three task commits do not carry "030" in their subject line.** Subject-line grep is not a delivery signal in this repository; the `Plan:` trailer is. |

**The delivered state, verified independently of both signals:**

| Plan 030 task | Commit | Independent verification |
|---|---|---|
| **Task 1** — the false "first statement" godoc class, 16 sites | **`1a1c135`** | wrap-tolerant `perl -0777` scan over `git ls-files '*.go'` → **12 hedged / 9 bare / 21 total**; the AST checker reports zero violations |
| **Task 2** — the 32-bit test overflow | **`d2c69fe`** | `GOTOOLCHAIN=go1.25.13 GOARCH=386 GOOS=linux go vet ./...` → **exit 0**, zero output (was 24 compile errors) — the same command round 3 ran as clean row 9 |
| **Task 3** — the stale `gin` plan number and the ADR 0024 forward reference | **`7ab91cd`** | the five surviving `gin`/`028` hits are Plan 030's own **defect-description table** plus one historical note in Spec 011 — descriptions of the defect, not live assertions of it |

**The reliable signal, and the one every future round should use:**

```bash
git log --format='%h %s' --grep='Plan: 030'
```

I have committed **`7d671b4`** — *"docs: record Plan 030's delivery on the plan itself"* — adding a delivery
banner to [`030-post-029-maintenance.md`](030-post-029-maintenance.md) that names the three commits, records
that the unticked boxes are an artefact, and states the trailer command. That closes the cause.

**What this changes in the required fix, and what it does not:**

- **DISCHARGED — the rebase instruction.** Plan 032 `:95`'s *"`adapter/http/options.go` and
  `adapter/http/helpers.go` are being edited **RIGHT NOW** … Rebase on 030, do not merge"* is **not operative**.
  Those two files are no longer contested. Plan `:87-88`'s *"030 and 031 are both TAKEN by concurrent,
  **undelivered** work"* is false of 030 (it remains true of 031). Spec `:62-64`'s *"being edited concurrently
  by Plan 030"* is false. All three must be replaced with the true state, not merely reconciled with each other.
- **UNCHANGED — the narrow claim.** Plan `:255-256`'s *"the gate's `fixed`/`rejects` arms are already at
  `1<<30` and its `safe` arm at `math.MaxInt`"* was true when written and remains true. Round 3 verified it
  first-hand (clean row 8 and the NEW-1 evidence block both depend on it). Every offset in the 14-site inventory
  is a post-conversion offset. **Do not "correct" it.**
- **UNCHANGED — the finding's core.** The bundle really did assert three mutually inconsistent states for the
  same sibling plan, in three places, one of them operative. That defect stands regardless of which state is
  true, and NEW-3's required fix — establish the state **once**, from a named reliable command, and have every
  other mention point at it — is exactly right. Only the *value* it resolves to changes.

**One disposition NEW-3 did not supply, and revision 4 must:** Plan 032 Task 1 **Step 11b** lists the artifacts
it may and may not edit, and does not mention `030-post-029-maintenance.md` at all. State it explicitly: that
file is a **DELIVERED PLAN**, not an immutable audit record. It is **editable in place** — the Plan 020
precedent, and the same latitude Step 11b already grants Plan 029 (*"a delivered plan and may be corrected in
place … prefer a dated one-line note over a rewrite"*). Only `030-audit-round-*.md` is immutable. The banner at
`7d671b4` is itself an exercise of that latitude.
