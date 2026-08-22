# Plan 032 — adversarial design audit, round 2 (2026-08-22)

Independent Opus subagent, handed the **complete revision-2 bundle together** —
[Spec 018](../specs/018-byte-cap-ceilings.md) + [ADR 0034](../adrs/0034-byte-cap-ceilings.md) +
[Plan 032](032-byte-cap-ceilings.md) — **before any implementation code existed**, per
[`CLAUDE.md`](../../CLAUDE.md)'s design-time gate. The plan is part of what was audited, not merely the context
for it. Round 2 additionally **verifies round 1's fixes**: every one of the 14 findings in
[`032-audit-round-1.md`](032-audit-round-1.md) was re-checked against revision 2 and against the tree.

**Traceability.** Audits: [Spec 018](../specs/018-byte-cap-ceilings.md),
[ADR 0034](../adrs/0034-byte-cap-ceilings.md), [Plan 032](032-byte-cap-ceilings.md). Prior round:
[`032-audit-round-1.md`](032-audit-round-1.md). Parent artifacts whose contracts are implicated:
[Spec 016](../specs/016-sizing-option-bounds.md), [ADR 0032](../adrs/0032-sizing-option-bounds.md),
[Plan 029](029-sizing-option-bounds.md), [Plan 030](030-post-029-maintenance.md),
[Plan 031](031-group-member-bounds.md), [ADR 0031](../adrs/0031-nil-option-elements.md),
[ADR 0029](../adrs/0029-eip-lexical-alignment.md). Parent backlog: [`docs/HANDOVER.md`](../HANDOVER.md) §7 item 6.

This record is **evidence-primary** — it is the audit artifact, not a user-facing summary. Every structural claim
in the bundle was re-derived on this tree (`main` at `46803c6`, clean worktree, `GOTOOLCHAIN=go1.25.13`,
darwin/arm64); the commands and their output are pasted below. No file in the repository was modified, except a
throwaway probe module outside the repo.

> **⚠️ IMMUTABLE EXECUTION RECORD.** This file records what round 2 found, at the time it found it. Do not edit it
> to reflect later decisions, later measurements, or the revised bundle. Corrections belong in a later round's
> record or in the artifacts themselves. The coordinator's dispositions for these findings live in **Spec 018 /
> ADR 0034 / Plan 032 revision 3**, each of which cites this file.

**Verdict: NOT SAFE TO IMPLEMENT.** 1 BLOCKER, 5 MAJORs, 6 MINORs.

**The revision is a large improvement and the trend is the right one** — 3 BLOCKERs became 1, and the one that
remains is *new*, not a survivor. Six findings landed cleanly, and two of them (M-5's justification rewrite, M-6's
tested `(n int)` rejection) turned the bundle's weakest argument into its strongest. What round 2 finds is a
narrower and more specific class of defect than round 1's: **round 1 failed at derivation against the tree; round
2 fails at the derivations' PREDICATES.** Three of the twelve findings (N-1, N-2, N-5) are the same shape — a
mechanical procedure was adopted, exactly as round 1 demanded, but its *selector* does not match the *property*
being changed, so the procedure returns a confident wrong answer. That is a harder failure to see than a stale
line number, because the output looks derived.

The remaining pattern is the project's own named one. **m-11 landed in the spec and the plan and not in the
ADR** — *fold into all three artifacts* — and **M-8's fix landed in the plan and not in the spec**. Two of the
five LANDED-BUT-FLAWED rows below are that single failure mode, twice.

---

## Part 1 — fix verification for all 14 round-1 findings

Each round-1 finding re-checked against revision 2 and against the tree. **LANDED** = the required fix is present
and correct. **LANDED-BUT-FLAWED** = the fix is present but introduces or leaves a defect, tracked as a new
finding. **NOT LANDED** = absent from at least one artifact. **REGRESSED** = the fix made the artifact worse than
revision 1.

| # | Round-1 finding | Status | Evidence / new finding |
|---|---|---|---|
| **B-1** | Task 1 cannot be a green commit | **LANDED** | Revision 1's Tasks 1+2+3 are one task (Plan §Task 1 header). Global constraint 8 no longer contradicts anything; there is no point at which the root suite is red between commits. |
| **B-2** | `exchange_test.go` branch 20 in no task's Files | **LANDED-BUT-FLAWED** | `adapter/http/exchange_test.go` is in Task 1's Files; branch 20 is Step 8; the message-string check is replaced by a call-site enumeration. **But the pasted classification's totals are wrong (→ N-5), and Step 8's rewrite can orphan the `math` import (→ N-10).** |
| **B-3** | Gate inventory 7 sites of ≥12, offsets stale | **LANDED-BUT-FLAWED** | The inventory is now derived by command and lists 12 sites at correct offsets. **But the grep's predicate misses two sites (→ N-2), the pasted line count is wrong (→ N-2), and the "surviving invariant" the finding asked for is FALSE as written (→ N-1, this round's BLOCKER).** |
| **M-4** | AC-3's "verified clean" command is RED | **LANDED** | `go vet` + `go build` replace `go test -run=NONE` in Spec §2, §6 AC-3, ADR fact 2 and D-AR(b), Plan Step 9 and the Delivery checklist. Re-derived: both exit **0**; the old form still exits **1** with `exec format error`. |
| **M-5** | The justification sentence is false both ways | **LANDED** | Spec §3.2 and ADR D-AN(a) both rewritten to width-safety/portability, both state plainly that a higher cap **is** honourable on 64-bit, and D-AO no longer lists width safety as a fourth benefit. The strongest fix in the revision. |
| **M-6** | `(n int)` never considered | **LANDED** | Added to both rejected-alternatives tables (Spec §5 row 6, ADR D-AP's table context) with probe evidence; Spec §3.6 item 1 is demoted to *"a DECISION, argued in §3.5"*. **Independently reproduced this round — see Part 3.** |
| **M-7** | `endpoint.WithMaxPayloadBytes` unmentioned | **LANDED** | Spec §3.4a and ADR D-AN(b) both name it, quote its godoc, tabulate the difference, and record the divergence as accepted with a follow-up in Spec §7 *Out* / §8 item 2. |
| **M-8** | Task 3 edit 2's wrong string + missing `IsPermanent` | **LANDED-BUT-FLAWED** | Plan Trap 2 states it correctly (*"Assert the render each row actually produces"*), and AC-2b adds the `IsPermanent` assertion with B1-10/M3-6 mutants. **But Spec §6 AC-4.1 site 2 still prescribes `assert.EqualError` on "the §3.1 render"** — the exact wrong instruction M-8 raised. Fold-into-all-three failure; see the M-8 residue below. |
| **M-9** | Plan 031 falsifies three normative literals | **LANDED-BUT-FLAWED** | Counts are expressed as deltas in Spec §6 AC-4.2, ADR D-AS and Plan Step 7, with both landing orders tabulated. **But the Spec 016 §2.1 "whichever lands second writes it" protocol the finding also asked for is unilateral and cannot work (→ N-4).** |
| **M-10** | AC-1 requires a forbidden accessor assertion | **LANDED-BUT-FLAWED** | The accessor clause is deleted from Spec §6 AC-1 and replaced by the observable-effect definition, consistent with the plan. **But m-13's re-wording (below) forbids the very configuration AC-1's replacement requires (→ N-3).** |
| **m-11** | `WithReplayBuffer` cites §1.5 at `:977`, not §1.3 at `:976` | **NOT LANDED** | Corrected in **Spec §2.1's table** and in **Plan Task 1 Step 1**. **ADR 0034 lines 74-75 still read `` `WithReplayBuffer` `:976` ``**, still under the sentence *"The bounded siblings did get theirs"* which implies a shared cite, and still uses godoc lines in violation of the convention Spec §"Line/offset convention" declares binding on this bundle. |
| **m-12** | "re-opens all 19 gate rows" inflated | **LANDED** | Spec §1.1's block quote and ADR D-AM's block quote both state the corrected figure (3 rows + one `byArm` key) and keep the conclusion on the criterion-re-check ground. Spec §8 item 3 restated too. |
| **m-13** | Constraint 6 vs branch B1-4's 2 MiB fixture | **REGRESSED** | Revision 1 bounded the **fixture** (*"no test READS more than 1 MiB"*) and collided with one branch. Revision 2 bounds the **cap** (*"no test may CONFIGURE a cap above ~1 MiB"*) and now collides with **five** branches, both AC-2 upper-arm assertions and AC-1's own requirement — while ceasing to bound the thing that is actually dangerous. See **N-3**. |
| **m-14** | Six godoc sentences false between two commits | **LANDED — defect RELOCATED** | The godoc window is genuinely closed: Plan Task 1 Step 5 rewrites all six in the same commit as Step 4. **But the identical defect now exists one level up** — the governing artifacts (Spec 016, ADR 0032, HANDOVER) assert a `deferred` arm of 3 across the gap between Task 1's commit and Task 2's. See **N-6**. |

**Score: 6 LANDED, 5 LANDED-BUT-FLAWED, 1 NOT LANDED, 1 REGRESSED, 1 RELOCATED.**

### M-8 residue — the spec still carries the instruction M-8 was raised against

```
$ grep -n '§3.1 render' docs/specs/018-byte-cap-ceilings.md docs/plans/032-byte-cap-ceilings.md docs/adrs/0034-byte-cap-ceilings.md
docs/specs/018-byte-cap-ceilings.md:676:| 2 | `:571-575`, … | the three rows' `assert` closures | `require.NoError` → `require.ErrorIs` … **+** `assert.EqualError` on the §3.1 render **+** …
```

One hit, in the spec, and none in the plan or the ADR. §3.1 renders exactly two strings:

```
msghttp: max body bytes out of range: msghttp.WithMaxBodyBytes: 0 not in [1, 2147483647]
msghttp: max body bytes out of range: msghttp.WithMaxBodyBytes: 2147483648 not in [1, 2147483647]
```

The three moved rows pass `1 << 62`, which renders `4611686018427387904` (verified: `python3 -c "print(1<<62)"`
→ `4611686018427387904`). An implementer working from **Spec §6 AC-4.1** — which the plan's Step 6 explicitly
directs them to (*"per Spec 018 §6 AC-4.1's 12-site table"*) — copies a string the row cannot produce. The plan's
Trap 2 is correct and would rescue them; the spec is normative and would not. **Name the actual expected string in
the spec.**

---

## Part 2 — new findings

| # | Rank | One line |
|---|---|---|
| **N-1** | BLOCKER | The restated "surviving invariant" is false for the six `safe`-arm rows and silently disables the int32-truncation probe |
| **N-2** | MAJOR | The mechanically-derived inventory's grep predicate does not match the property being changed — two sites missed, line count wrong |
| **N-3** | MAJOR | Global constraint 6's re-wording is a regression: it bounds the harmless thing and forbids five of the increment's own branches |
| **N-4** | MAJOR | "Spec 016 §2.1 is written by whichever lands second" is unilateral — Plan 031 has no such task, so one order leaves it permanently wrong |
| **N-5** | MAJOR | The pasted call-site classification's totals are 48/34/18; the tree says 49/40/24 — and Task 1 Step 2 stops on a mismatch |
| **N-6** | MAJOR | AC-5 and D-AT(b) require the parent artifacts in the same commit; the plan puts them in the next one — m-14 relocated |
| **N-7** | MINOR | `byteCapCeiling = math.MaxInt32 - 1` dissolves the `(n int)` rejection and neither round considered it |
| **N-8** | MINOR | Step 11's falsification sweep greps a case the godoc does not use, so it can only ever read clean |
| **N-9** | MINOR | `checkRange`'s godoc enumerates "this package's three sites" and will read as a package total once the sibling lands |
| **N-10** | MINOR | Step 8's rewrite orphans `exchange_test.go`'s only `math` import and breaks the build |
| **N-11** | MINOR | `WithMaxEventBytes` is parse-side only; the SSE **server** never consults it, and no artifact says so |
| **N-12** | MINOR | AC-3's 386 gate is the one gate in the bundle that is never vacuity-probed |

---

## BLOCKER N-1 — the restated "surviving invariant" is FALSE for the six `safe`-arm rows, and applying it disables the int32-truncation probe

**The claim under attack.** Round 1's B-3 required the bundle to *"state the surviving invariant honestly: the
literal is chosen by the option's **parameter type** (`int` → `1<<30`, `int64` → `1<<62`), not by its arm."*
Revision 2 adopted it verbatim, in all three artifacts:

- **Spec §6 AC-4.1**, site 7's block: *"the literal is chosen by the option's **PARAMETER TYPE**, not by its
  arm — an `int` parameter takes `1<<30` …; an `int64` parameter takes `1<<62` …. **Both then select the
  out-of-range branch everywhere.**"*
- **ADR D-AS trap 3:** *"Restate the surviving invariant, which is the true one: the literal is chosen by the
  option's PARAMETER TYPE — `int` → `1<<30`, `int64` → `1<<62`."*
- **Plan Task 1 Step 6, Trap 3:** *"Restate the surviving invariant: the literal follows the option's PARAMETER
  TYPE — `int` → `1<<30`, `int64` → `1<<62`."*

The instruction is to write this into `sizing_option_class_gate_test.go`'s header block (`:40-59`) as the
replacement for Plan 030's arm→literal mapping.

**The evidence — the invariant is false for six of the file's nineteen rows, and falsifying it is not academic.**
The `safe` arm's six rows are **all `int`-typed**:

```
$ grep -n 'func WithPollMaxBatch\|func WithBreakerThreshold\|func WithMaxPayloadBytes' endpoint/flowcontrol.go resilience/breaker.go
resilience/breaker.go:51:func WithBreakerThreshold(n int) CircuitBreakerOption {
endpoint/flowcontrol.go:144:func WithMaxPayloadBytes[T any](n int) ConsumerOption[T] {
endpoint/flowcontrol.go:166:func WithPollMaxBatch[T any](n int) ConsumerOption[T] {
$ grep -n 'func NewTokenBucket' resilience/ratelimit.go
42:func NewTokenBucket(rps float64, burst int, opts ...TokenBucketOption) (msgin.RateLimiter, error) {
$ grep -n 'func (s \*QueueStore) Claim' adapter/memory/queuestore.go
182:func (s *QueueStore) Claim(_ context.Context, max int) ([]msgin.Delivery, error) {
$ grep -rn ') Poll(' channel/queuechannel.go
50:func (q *QueueChannel) Poll(ctx context.Context, max int) ([]msgin.Delivery, error) {
```

Six `int` parameters. **Every one of them passes `math.MaxInt` and asserts the value is ACCEPTED**, not rejected —
`endpoint.WithPollMaxBatch`, `resilience.WithBreakerThreshold` (`:644-646`),
`endpoint.WithMaxPayloadBytes` (`:669`), `resilience.NewTokenBucket` (`:691-692`),
`(manual) memory.QueueStore.Claim` (`:704-706`), `(manual) channel.QueueChannel.Poll` (`:723-724`). So:

1. **"An `int` parameter takes `1<<30`" is false of all six.** They take `math.MaxInt`.
2. **"Both then select the out-of-range branch everywhere" is false of all six.** There is no out-of-range branch;
   the rows exist to assert acceptance plus a product-usable check.

**Applying the stated invariant literally is the harm, and the file says so in its own words.** Demoting the six
rows to `1<<30` is exactly what `sizing_option_class_gate_test.go:61-77` forbids, in the block Plan 030 wrote:

```
$ sed -n '61,77p' sizing_option_class_gate_test.go
//   - "safe" (6) → math.MaxInt. These rows assert require.NoError plus a
//     product-usable check and carry NO decimal string, so architecture
//     dependence is harmless — and the value must stay MAXIMALLY absurd,
//     because that is the row's entire purpose (see the arm's comment at the
//     "safe" section: the knob is exercised "past the point where a buggy
//     comparison, e.g. an int32 truncation, would misbehave"). 1<<30 IS an
//     int32 value, so demoting these rows to it would leave every assertion
//     green while the int32-truncation probe silently stopped running. Worse,
//     if an implementation regressed to make([]T, n), math.MaxInt fails fast
//     whereas 1<<30 quietly allocates ~1 GiB.
//
//     ACCEPTED, RECORDED LIMITATION: on GOARCH=386 no int value exceeds int32,
//     so the int32-truncation probe these six rows exist to run is
//     UNACHIEVABLE there by any choice of magnitude. math.MaxInt keeps the
//     probe intact where it is meaningful (64-bit) and degrades to a tautology
//     where it cannot be (32-bit). Do not "fix" that by picking a smaller
//     number — that would disable the probe on BOTH architectures.
```

*"Demoting these rows to it would leave every assertion green while the int32-truncation probe silently stopped
running."* The bundle instructs an implementer to overwrite this block with a rule whose literal application
produces precisely that outcome — a **silently green, silently non-probing** gate. That is worse than a red gate,
and it is worse than the stale-offset defect B-3 raised, because nothing fails.

**Why this is a BLOCKER rather than a wording MINOR.** It is not that the sentence is imprecise; it is that the
sentence is offered in all three artifacts as *"the surviving invariant, which is the true one and is
mechanically checkable"*, as the replacement for a normative block, in an increment whose entire subject is that
same block. An implementer following the plan has no signal that the rule excludes a third of the file. The
bundle's own `safe`-arm reasoning — which round 1 explicitly certified clean (*"the `1<<30`-is-an-int32-value
argument that keeps the `safe` arm at `math.MaxInt`"*) — contradicts it, and no artifact reconciles the two.

**Required fix — state the invariant TWO-DIMENSIONALLY, because it is two-dimensional.** The arm and the parameter
type are not alternatives; they compose, in that order:

1. **The ARM fixes the required PROPERTY.** `safe` → the value must be **accepted** and must stay maximally
   absurd, so it is `math.MaxInt` and no other value. `fixed` and `rejects` → the value must be **out of range**
   and must render an architecture-independent decimal.
2. **Within the reject arms ONLY, the PARAMETER TYPE chooses the literal.** `int` → `1<<30` (fits int32, exceeds
   every `int`-typed ceiling, renders `1073741824` on every architecture); `int64` → `1<<62` (in range on every
   architecture, renders `4611686018427387904`, and is required for the three moved rows because
   `1<<30 < byteCapCeiling`).

Write it that way in Spec §6 AC-4.1's site-7 block, ADR D-AS trap 3, and Plan Task 1 Step 6 Trap 3 — and carry the
`safe`-arm block's "do not demote these" warning forward verbatim rather than replacing it.

---

## MAJOR N-2 — the mechanically-derived inventory inherits its grep's blind spot: the predicate is "mentions `deferred`", the property is "records the `fixed` partition"

**The claim under attack.** Spec §6 AC-4.1: *"**derive the site list, do not transcribe it** … Run this against
**current `HEAD`**, paste the output into the task's Evidence block, and edit **every** hit"*, of

```bash
grep -n 'deferred\|DEFERRED\|9/1/3/6\|9 + 1 + 3 + 6' sizing_option_class_gate_test.go
```

*"Output at `1212c63` — **17 lines, 12 distinct edit sites**."* Restated as ADR D-AS's derivation command and
Plan Task 1 Step 6.

**The evidence, part 1 — the line count is wrong.** Re-run on this tree:

```
$ grep -n 'deferred\|DEFERRED\|9/1/3/6\|9 + 1 + 3 + 6' sizing_option_class_gate_test.go | wc -l
      18
```

**18, not 17** — `:35 :38 :55 :58 :401 :539 :565 :570 :579 :588 :758 :761 :782 :783 :784 :801 :803 :805`. Nothing
in this file changed between `1212c63` and `46803c6` (`git diff 1212c63 HEAD -- sizing_option_class_gate_test.go`
is empty), so this is a miscount at authoring time, in the one figure the AC presents as pasted command output.
The 12-site *grouping* is right; the count above it is not.

**The evidence, part 2 — and this is the substantive half. The predicate does not match the property.** The four
alternatives all select on the word `deferred` or on the string `9/1/3/6`. But what this increment changes is the
**`fixed` partition**: `fixed` goes from 9 to 12, and — per N-1 — from one literal to two. **Two sites record the
`fixed` partition and contain none of the four tokens:**

```
$ grep -nE 'deferred|DEFERRED|"fixed"|9/1/3/6|9 \+ 1 \+ 3 \+ 6|\(9\)|\(3\)' sizing_option_class_gate_test.go
26://       - "fixed"    (9) — the fault is reported through the surface Spec 016
33://                          in neither "fixed" (not a class member) nor "safe"
35://       - "deferred" (3) — accepts 1<<62, annotated so it never reads as a
38://     9 + 1 + 3 + 6 = 19 rows = 17 AST keys + 2 manual rows.
47://   - "fixed" (9) and "rejects" (1) → 1<<30 = 1,073,741,824. These rows assert
55://   - "deferred" (3) → still 1<<62. These three take an int64, NOT an int, so
58://     — "this knob accepts an absurd value and the remedy is DEFERRED" — mean
401:	arm    string // "fixed" | "rejects" | "deferred" | "safe" — Spec 016 §6 AC-5's four BEHAVIORAL arms, not §2.1's three classification verdicts
412:			arm: "fixed",
…
539:		// ---- arm: deferred — 3 class members whose ceiling is DEFERRED (Spec 016 §3.8) ----
551:		// row into the "fixed" arm above and rewrite its assertion to the fixed
565:		// DEFERRED, Spec 016 §3.8"), and on 386 math.MaxInt would shrink it to
570:			arm: "deferred", // class member, remedy deferred — Spec 016 §3.8
579:			arm: "deferred", // class member, remedy deferred — Spec 016 §3.8
588:			arm: "deferred", // class member, remedy deferred — Spec 016 §3.8
758:	// split at 9/1/3/6; without this, a contributor could move a row between
761:	// (WithMaxResponseBytes, safe -> deferred) each were — and the suite would
766:	// a count map (map[string]int{"fixed": 9, ...}) is blind to a PAIRWISE
772:		"endpoint.WithMaxInFlight":           "fixed",
…
801:			"3 with a deferred ceiling (§3.8), 6 safe (4 AST + 2 manual). Moving a row between arms is a "+
803:	require.Equal(t, map[string]int{"fixed": 9, "rejects": 1, "deferred": 3, "safe": 6}, byArm,
805:			"from Spec 016 §2.1's 9/1/3/6 split")
```

**`:26` and `:47` are absent from the 12-site table and from the narrow grep, and both go false:**

| Site | Text | Goes false because |
|---|---|---|
| **`:26`** | `//       - "fixed"    (9) — the fault is reported through the surface…` | `fixed` becomes **12** |
| **`:47`** | `//   - "fixed" (9) and "rejects" (1) → 1<<30 = 1,073,741,824. These rows assert an EqualError against a rendered decimal.` | **TWICE** — the count (9 → 12) **and** the literal, since 3 of the 12 sit at `1<<62`, not `1<<30`, and render a different decimal |

`:47` is the same defect the bundle already caught at `:55` (site 7) — the arm→literal mapping — one bullet
higher, and the narrow grep sees `:55` only because that bullet happens to contain the word `deferred`. **The
bundle found site 7 by luck of vocabulary, not by derivation.** A third site, `:766`, carries
`map[string]int{"fixed": 9, ...}` inside a comment and is likewise invisible to the narrow predicate; it is
illustrative rather than normative, but an implementer should classify it, not miss it.

**Aggravating.** This is the *second* round in which the same file's inventory has been under-derived, and the
first fix was exactly right in method (derive, don't transcribe) and wrong in selector. The project's stored
lesson — *fix the class, not the instance* — applies to the grep itself: assert the invariant (*"every site that
records the arm partition or its literals"*), not an enumeration of tokens that happen to appear in the sites you
already know about.

**Required fix.** Widen the derivation to the **property**, e.g.

```bash
grep -nE 'deferred|DEFERRED|"fixed"|9/1/3/6|9 \+ 1 \+ 3 \+ 6|\(9\)|\(3\)' sizing_option_class_gate_test.go
```

**paste the actual output** into Spec §6 AC-4.1 (over-inclusion is safe — the rows at `:412`-`:511` and
`:772`-`:780` classify as "no change"; under-inclusion is the defect), fold `:26` and `:47` into the site table as
sites 13 and 14, classify `:766`, and correct **"17 lines" → "18 lines"**. Mirror all of it into ADR D-AS and Plan
Step 6.

---

## MAJOR N-3 — Global constraint 6's re-wording is a REGRESSION: it bounds the harmless thing and forbids five of the increment's own branches

**The claim under attack.** Plan Global constraint 6, revision 2:

> *"**No test may CONFIGURE a cap above ~1 MiB.** Ceiling *values* are exercised by **`NewConfig` only**
> (Spec 018 §6 AC-1). … **A fixture MAY exceed a small cap in order to prove the cap binds** … *(Round-1 m-13:
> revision 1 said "no test reads more than 1 MiB", which forbade the only case proving the default arm.)*"*

**The evidence — the subject was swapped, and the wrong one is now bounded.**

Revision 1 said *"no test **READS** more than 1 MiB"*. That bounds the **fixture**, and the fixture is the actual
hazard: the spec's own §6 explains it — *"running `WithMaxBodyBytes(byteCapCeiling)` against a real 2 GiB body
would allocate ~4 GiB at `io.ReadAll`'s doubling peak, in a package whose sibling runs `goleak.VerifyTestMain`.
It cannot be written."* The 4 GiB comes from the **body**, not from the integer in the option.

Revision 2 bounds the **cap** — an `int64` field. Configuring `WithMaxBodyBytes(2147483647)` allocates nothing;
it stores eight bytes. So the new constraint forbids a harmless act and permits the dangerous one (a 2 GiB
fixture under a 2 GiB cap satisfies "the cap is not above ~1 MiB"? no — but a 2 GiB fixture under a *1 KiB* cap
does, and that is the shape that OOMs).

**It forbids five of this increment's own branches, plus both AC-2 assertions, plus AC-1 itself.** From the plan's
own branch table, three sections later:

| Branch | Case | Cap configured |
|---|---|---|
| **B1-1** | `NewConfig_accepts_the_ceiling` (all three) | `2147483647` |
| **B1-3** | `NewConfig_rejects_ceiling_plus_one` (all three) | `2147483648` |
| **B1-5** | `NewConfig_accepts_the_ceiling/body` | `2147483647` |
| **B1-9** | `NewConfig_rejects_the_gate_value` (all three) | `1 << 62` |
| — | Spec §6 AC-2's upper-arm `EqualError`, both renders | `2147483648` |
| — | Spec §6 AC-1's first bullet, `NewConfig(WithX(byteCapCeiling))` → accepted **with an observable effect** | `2147483647` |

Every one of those **configures a cap above ~1 MiB**. An implementer applying the constraint literally deletes the
increment's entire upper arm — the thing it exists to add. That is a strictly worse contradiction than m-13's:
revision 1 collided with **one** branch, revision 2 collides with **five plus two acceptance criteria**.

**It also collides with M-10's fix.** Spec §6 AC-1 was rewritten this revision (round-1 M-10) to require
`NewConfig(WithX(byteCapCeiling))` → nil error **and an observable effect** — `DecodeRequest` on a small body, an
`httptest` round-trip, `NewSSEParser` + `Next` on a small event. All three configure the ceiling. The two fixes
landed in the same revision and contradict each other.

**Required fix — restore the FIXTURE as the subject, and say why.** Something of the shape:

> **No test may present a body / response / event **fixture** larger than ~1 MiB.** A cap may be configured at
> **any legal value, including `byteCapCeiling` itself** — an `int64` field costs nothing to set. What must never
> be written is a *fixture* sized to the ceiling: a 2 GiB body peaks near 4 GiB through `io.ReadAll`'s doubling,
> in a package whose sibling runs `goleak.VerifyTestMain`. The property *"the cap caps"* is a fact about the
> comparison, so it is proven at small `n` with a small fixture (B1-4's 2 MiB body against the 1 MiB default is
> the largest fixture in the increment and is expressly permitted).

The parenthetical crediting m-13 should be rewritten to record that revision 2 over-corrected, so the next reader
does not restore the revision-2 form on the strength of the round-1 citation.

---

## MAJOR N-4 — "Spec 016 §2.1 is written by whichever lands second" is a UNILATERAL protocol; Plan 031 never agreed to it

**The claim under attack.** Three artifacts, in identical words:

- **Spec §6 AC-4.2:** *"**Spec 016 §2.1's arm table is updated by WHICHEVER LANDS SECOND**, in one edit reflecting
  both increments. The first lander updates only its own rows and says in its fold-back task that the other is
  pending. Recording this here is what stops both increments claiming the table or both skipping it."*
- **Spec §6 AC-5** and **ADR D-AS**, same sentence.
- **Plan Task 2 preamble** and **Step 2:** *"**Amend only the rows this increment owns** (the ordering rule
  above)."*

**The evidence — the other party has no such task.**

```
$ grep -n 'Spec 016 §2.1' docs/plans/031-group-member-bounds.md
$ echo $?
1
$ grep -c '016-sizing' docs/adrs/0033-group-member-bounds.md
0
$ grep -n '^## Task' docs/plans/031-group-member-bounds.md
182:## Task 1 — the `memory` bound, its classification, and `Handle`'s release re-evaluation
275:## Task 2 — the bound holds for ALL FOUR release paths (the increment's reason to exist)
331:## Task 3 — the `default ≥ completionSizeCeiling` invariant, mechanically enforced (D-AQ)
366:## Task 4 — godoc cross-references on the three unbounded release paths (D-AI)
398:## Task 5 — `sql.WithMaxGroupMembers`, `checkRange`, and the `AddMember` signature
462:## Task 6 — in-transaction enforcement in postgres, mysql and sqlite (D-AG, D-AP)
514:## Task 7 — the shared dialect conformance case (AC-4, AC-4b, AC-4c, AC-5)
553:## Task 8 — the SPI contract, and interface-level conformance on both stores (D-AH)
576:## Task 9 — the class gate's stated blind spot and its count sweep (D-AL)
645:## Task 10 — whole-branch delivery gate
```

Ten tasks. **None is a Spec 016 §2.1 fold-back**, and ADR 0033 does not reference Spec 016 by filename at all.
Plan 032 is writing a two-party protocol into its own artifacts and binding a sibling that has not been told.

**Both orders fail:**

| Landing order | What happens |
|---|---|
| **032 first, 031 second** | 032 amends only its three byte-cap rows and records that 031's are pending. 031 then lands with **no task to complete the table**, so Spec 016 §2.1 is left permanently recording a partition that matches neither increment's end state. |
| **031 first, 032 second** | 031 lands with no Spec 016 edit at all — its own rows are missing from §2.1. 032 then arrives believing it is "the second lander" and must write **both** sets of rows, which it can only do by transcribing Plan 031's numbers from a plan, not from the tree — exactly the hand-typed-total failure two audit rounds have already caught on this file. |

The protocol also has no way to *detect* which lander it is other than by reading the sibling's plan, which is
the transcription hazard again.

**Required fix — Plan 032 owns Spec 016 §2.1 UNCONDITIONALLY, and re-derives it.** Drop the "whichever lands
second" protocol from Spec §6 AC-4.2, Spec §6 AC-5, ADR D-AS and Plan Task 2, and **delete the "amend only the
rows this increment owns" instruction that contradicts it**. In its place: Plan 032's Task 2 (or Task 1 — see
N-6) updates Spec 016 §2.1's arm table **from the tree**, by reading the gate file's `wantArms`/`byArm` values
**as they then stand**, not from any pre-computed count. That is correct under either landing order by
construction, and it is the only formulation that does not require one increment to know the other's numbers.

Keep the delta table (M-9's fix) — it is right, and it is what makes the re-derivation safe.

---

## MAJOR N-5 — the pasted call-site classification is 48/34/18; the tree says 49/40/24 — and Step 2 stops on a mismatch

**The claim under attack.** Spec §3.1a: *"**Classification against the tree at `1212c63` — 48 hits, of which 34
are calls** (the remainder are the options' names inside `name:` strings and comments, which no ceiling
affects)"*, followed by a four-class table whose fourth class reads **`| IN RANGE — accepted, stays accepted | 1,
4, 10, 16, 1024, 2048 | 18 | … |`**. Plan Task 1 Step 2 tells the implementer to re-run the grep, classify every
hit, and treat *"any out-of-range hit not in this task's Files list"* as a **stop-and-reassess**.

**The evidence.** Re-run on this tree — again, the file set is unchanged since `1212c63`:

```
$ grep -rn 'WithMaxBodyBytes(\|WithMaxResponseBytes(\|WithMaxEventBytes(' --include='*_test.go' . | wc -l
      49
```

**49 hits, not 48.** Of those, **9 are non-calls** — `nil_option_test.go:87` (comment),
`outbound_test.go:29` (comment), `outbound_test.go:58`, `:66`, `:74` (`name:` strings), `encode_test.go:50`,
`:58` (`name:` strings), `exchange_test.go:577` (comment), `exchange_test.go:613` (`t.Run` name string) — leaving
**40 calls, not 34**.

| Class | Value(s) | Spec says | Tree says | Sites |
|---|---|---|---|---|
| OUT OF RANGE — breaks | `1 << 62` | 3 | **3** ✅ | `sizing_option_class_gate_test.go:572`, `:581`, `:590` |
| OUT OF RANGE — breaks | `math.MaxInt64` | 1 | **1** ✅ | `adapter/http/exchange_test.go:615` |
| IN RANGE — rejected today | `0`, `-1` | 12 | **12** ✅ | as listed |
| IN RANGE — accepted | `1`…`2048` | **18** | **24** ❌ | 24 distinct call sites (`sse_test.go:730`, `:743`, `:756`, `:771`, `:786`, `:807`, `:825`, `:845`, `:865`, `:883` are **ten** sites, which the spec's `:730`…`:883` ellipsis collapses) |

`3 + 1 + 12 + 24 = 40` ✅. The spec's own four classes sum to `3 + 1 + 12 + 18 = 34`, which is internally
consistent with its wrong call total and inconsistent with the tree.

**The load-bearing half is CORRECT, and that matters.** There are exactly **four** out-of-range call sites, and
exactly one of them — `exchange_test.go:615` — was unscheduled before this revision. B-2's substantive fix is
sound. What is wrong is the arithmetic around it.

**Why it is a MAJOR and not a MINOR.** Plan Task 1 Step 2 instructs: *"The expected shape is Spec 018 §3.1a's
table: **4 out-of-range** …, the rest unaffected. **Any out-of-range hit not in this task's Files list is a
stop-and-reassess.**"* An implementer who re-runs the grep, gets 49/40, and compares against a pasted 48/34 has
been handed a **manufactured discrepancy** on a step whose designed response to a discrepancy is to halt. The
worst outcome is not the halt — it is the implementer concluding the totals are "close enough" and thereby
learning to distrust the very check that catches a real new call site.

**Required fix.** Re-derive on the tree at implementation time, **paste the real output**, and correct the totals
to **49 hits / 40 calls / 24 in the fourth class**. Expand the `sse_test.go:730`…`:883` ellipsis to the ten
explicit line numbers so the count is checkable by inspection. Add to Plan Step 2 that the totals must be
re-derived, not compared against the spec's figures, since Plan 030 has landed and Plan 031 may land in between —
which the spec already says one sentence later and the plan does not.

---

## MAJOR N-6 — the parent artifacts are required "in the same commit" and scheduled in the next one; m-14's defect relocated

**The claim under attack.** Two normative statements and one schedule that contradicts them:

- **Spec §6 AC-5:** *"**AC-5 — the parent artifacts are updated in the same commit.** Spec 016 §2.1's census table
  and arm table, §3.8, §6 AC-5's arm table, and ADR 0032 **D-AB** all record a `deferred` arm of 3. Each is
  amended to point here. `docs/HANDOVER.md` §7 item 6 is closed."*
- **ADR D-AT(b):** *"**Every artifact recording the `deferred` arm of 3 is amended in the same commit.**"*
- **Plan Task 2:** those exact files — `../specs/016-sizing-option-bounds.md`,
  `../adrs/0032-sizing-option-bounds.md`, `../HANDOVER.md` — are **Task 2's** Files, committed as
  `docs: record the byte-cap ceiling and close the sizing-option class` (Step 10), one commit after Task 1's
  `fix(http): bound the three byte caps…` (Task 1 Step 12).

"The same commit" as *what* is unambiguous in both AC-5 and D-AT(b): D-AT(a-bis) sets the referent one paragraph
earlier — *"The production change, its godoc and the gate move land in ONE commit."* The parents are meant to ride
along. They do not.

**The evidence — and the reason this is exactly m-14 one level up.** Between Task 1's commit and Task 2's:

| Artifact | Asserts | Reality after Task 1 |
|---|---|---|
| Spec 016 §2.1 census line | `9 fixed + 3 deferred + 4 safe` | the gate has **no `deferred` arm** |
| Spec 016 §2.1 arm table, three rows | the three byte caps are `deferred` | they are `fixed` |
| Spec 016 §3.8 | the remedy is deferred | delivered |
| Spec 016 §6 AC-5 arm table | `deferred` = 3 | absent |
| ADR 0032 D-AB | *"class members with a deferred remedy … explicitly refuses to certify them safe"* | certified and bounded |
| `docs/HANDOVER.md` §7 item 6 | open backlog item | closed |

Six artifact statements false for the duration of one commit — the identical shape, and the identical count, as
m-14's six godoc sentences. The revision closed the godoc window by merging three tasks into one and then opened
an equivalent window one level up, in artifacts that are **normative** (ADR D-AS's own REVERSIBILITY line:
*"Spec 016 §2.1 and §6 AC-5 fix every key's arm, so moving a row is a **spec change**"*).

**It also violates CLAUDE.md's coupling rule directly.** *"Couple plans and ADRs with the code that realizes
them — one coherent commit. By default, do **not** make separate plan/ADR commits."* A commit that moves three
rows between normative arms while the spec fixing those arms still says otherwise is precisely the
plan-out-of-sync-with-code split that rule forbids.

**Required fix.** **Fold the parent-artifact edits into Task 1's commit** — Spec 016 (§2.1 census + arm table,
§3.8, §6 AC-5), ADR 0032 D-AB, and `docs/HANDOVER.md` §7 item 6, together with the cross-file grep guard (Task 2
Step 1) that finds them. Task 2 then becomes **gates and status flip only**: the link gate and its vacuity probe,
the whole-branch `/code-review` + `/security-review`, the eight-module loop, the `apidiff` surface probe, the
386 re-run, and the PROPOSED → ACCEPTED flip on Spec 018 / ADR 0034. Update AC-5, D-AT(b) and the Delivery
checklist to name Task 1 as the commit that carries the parents.

Note that this composes with **N-4**: once Plan 032 owns Spec 016 §2.1 unconditionally, the fold-back must
re-derive the arm table from the tree at that moment — which is possible in Task 1, because Task 1 is where the
gate reaches its final state.

---

## MINOR N-7 — `byteCapCeiling = math.MaxInt32 - 1` dissolves the `(n int)` rejection, and neither round considered it

**The claim under attack.** Two decisions that lean on each other:

- **D-AO / Spec §3.2:** *"**The argument licenses exactly one ceiling, not a family of them.** … `math.MaxInt32`
  is not 'a large round number' — it is the *only* value at which the 'same everywhere' property holds. One lower
  forfeits it for nothing; one higher breaks it."*
- **D-AP(a) / Spec §3.5:** `(n int)` is rejected because *"with `(n int)` and `byteCapCeiling = math.MaxInt32`,
  the ceiling on a 32-bit build **IS** `math.MaxInt` — so no `int` literal can exceed it, and the upper arm
  becomes INEXPRESSIBLE."*

**The evidence.** The `(n int)` rejection is conditioned on the ceiling being **exactly** `math.MaxInt32`. Move it
down by one and the rejection evaporates. Measured on a probe module (`GOTOOLCHAIN=go1.25.13`):

```
$ cat p.go
package probe
import "math"
const byteCapCeiling int = math.MaxInt32 - 1
func WithMaxBodyBytes(n int) int { return n }
var _ = WithMaxBodyBytes(2147483647)          // the upper arm, as a plain literal
func Check(n int) bool { return n >= 1 && n <= byteCapCeiling }

$ GOARCH=386 GOOS=linux go build ./... ; echo "exit=$?"
exit=0
$ GOARCH=386 GOOS=linux go vet ./... ; echo "vet exit=$?"
vet exit=0
$ GOARCH=amd64 GOOS=linux go build ./... ; echo "exit=$?"
exit=0
```

`2147483647` fits an `int` on 386; `2147483648` does not. So at `byteCapCeiling = math.MaxInt32 - 1` the upper
arm **is** expressible as a single architecture-independent decimal literal, on every `GOARCH` — and with it,
each of D-AP(a)'s three stated consequences falls:

| D-AP(a) consequence | At `math.MaxInt32` | At `math.MaxInt32 - 1` |
|---|---|---|
| AC-2's upper literal will not compile on 386 | true | **false** — `2147483647` compiles |
| AC-3's `go vet` gate goes red | true | **false** — vet exits 0 |
| No `fixed`-arm literal is both 386-compilable and above the ceiling | true | **false** — `2147483647` is both |

And `math.MaxInt32 - 1 = 2,147,483,646` is still exactly representable as an `int` on every `GOARCH`, so
D-AN(a)'s width-safety property holds unchanged. **Neither round considered this**, and the two decisions are
therefore mutually load-bearing in a way no artifact discloses: D-AO's value is used to justify D-AP's signature,
while D-AP's signature is not itself an argument for D-AO's value.

**This finding does not recommend changing the ceiling.** The `MaxInt32 - 1` shape has real costs, and they
should be the reason it loses:

1. **It forfeits the "largest representable value" story**, which is D-AN(a)'s entire rhetorical strength. *"One
   below the largest value representable everywhere"* invites the question the bundle spent §3.2 closing: why one
   below and not two? The ceiling stops being a property of the language and becomes a property of the test suite.
2. **It shrinks the moved gate rows' magnitude** from `1<<62` (`4611686018427387904`) to `2147483647` — roughly
   `2^31` instead of `2^62`. The rows would still select the out-of-range branch, but the "maximally absurd
   value" property the `safe` arm's comment defends would be weakened for the `fixed` arm too.
3. It would make three `msghttp` byte caps `int` while the codebase's other byte cap
   (`endpoint.WithMaxPayloadBytes`) is also `int` — a *consistency gain*, which is why the trade must be named
   rather than dismissed.

**Required fix.** Name `byteCapCeiling = math.MaxInt32 - 1` in **Spec §3.5** (as the escape D-AP(a)'s rejection
does not survive), in **Spec §5**'s rejected-alternatives table (a row of its own), and in **ADR D-AP(a)**. Price
it honestly with the three costs above, and **keep `(n int64)` on that trade** — not on the currently-stated
grounds that *"the argument licenses exactly one ceiling"*, which is true of the width-safety property's
*maximum* and not of the property itself. D-AO's REVERSIBILITY line should record that lowering the ceiling by one
would re-open the signature question.

---

## MINOR N-8 — Step 11's falsification sweep greps a case the godoc does not use, so it can only read clean

**The claim under attack.** Plan Task 1 Step 11, the godoc falsification sweep:

> | D-1 | *"no other godoc sentence became false"* | `grep -rn 'must be > 0' adapter/http/` — every hit read **against the constructor**, not for plausibility |

**The evidence.** The godoc form is upper-case `MUST`:

```
$ grep -rn 'must be > 0' adapter/http/ | wc -l
       6
$ grep -rin 'must be > 0' adapter/http/
adapter/http/options.go:458:// n MUST be > 0: NewConfig returns ErrInvalidMaxBodyBytes for an explicit
adapter/http/options.go:764:// n MUST be > 0: NewConfig returns ErrInvalidMaxResponseBytes for an explicit
adapter/http/options.go:851:// n MUST be > 0: NewSSEParser (via NewConfig) returns ErrInvalidMaxEventBytes
adapter/http/options.go:1004:// d MUST be > 0 when set explicitly: NewConfig returns ErrInvalidHeartbeat
adapter/http/options.go:1031:// d MUST be > 0: NewConfig returns ErrInvalidWriteTimeout for an explicit
adapter/http/options.go:1112:// min MUST be > 0 and max MUST be >= min: NewConfig returns
adapter/http/options.go:1138:// d MUST be > 0 when set explicitly: NewConfig returns ErrInvalidReadTimeout
adapter/http/errors.go:19:	ErrInvalidMaxBodyBytes = errors.New("msghttp: max body bytes must be > 0")
adapter/http/errors.go:77:	ErrInvalidMaxResponseBytes = errors.New("msghttp: max response bytes must be > 0")
adapter/http/errors.go:138:	ErrInvalidMaxEventBytes = errors.New("msghttp: max event bytes must be > 0")
adapter/http/errors.go:175:	ErrInvalidHeartbeat = errors.New("msghttp: heartbeat interval must be > 0")
adapter/http/errors.go:181:	ErrInvalidWriteTimeout = errors.New("msghttp: write timeout must be > 0")
adapter/http/errors.go:207:	ErrInvalidReadTimeout = errors.New("msghttp: read timeout must be > 0")
```

The case-sensitive grep returns **six hits, and not one of them is a godoc sentence** — all six are `errors.New`
**message strings**. **Zero of the seven `MUST be > 0` godoc sentences are visible to it**, including the three
(`:458`, `:764`, `:851`) that Step 5 rewrites and that D-1 exists to prove were rewritten.

**The failure mode is worse than a miss: the sweep gets *cleaner* as the increment proceeds.** Step 4 item 4
renames the three byte-cap sentinels from `must be > 0` to `out of range`, so after Task 1 the sweep returns
**three** hits — `ErrInvalidHeartbeat`, `ErrInvalidWriteTimeout`, `ErrInvalidReadTimeout`, all untouched and all
still true. An implementer sees the count fall, reads three unrelated survivors, and records D-1 as discharged.
The claim it was written to falsify — *"no other godoc sentence became false"* — is never tested at all.

Four more `MUST be > 0` godoc sentences exist on adjacent options (`:1004`, `:1031`, `:1112`, `:1138`). None is
touched by this increment, but they are exactly what D-1 should be scanning, and they are invisible.

**Required fix.** Use `grep -rin 'must be > 0' adapter/http/` in Plan Step 11's D-1 row, state the expected hit
count before and after (13 → 10: the three sentinel *messages* change, the ten godoc/sentinel sentences remain and
each must be read against the constructor), and **vacuity-probe it** — plant one lower-case `must be > 0` in a
godoc, confirm the `-i` form finds it and the case-sensitive form does not, revert. Same treatment for D-3's
`grep -c 'not a safety guarantee'` → **3**, which is a count assertion with no probe.

---

## MINOR N-9 — `checkRange`'s godoc enumerates "this package's three sites" and will read as a package total once the sibling lands seven lines below

**The claim under attack.** Plan Task 1 Step 4 item 2 places `checkRangeInt64` *"in `adapter/http/helpers.go`,
beside `checkRange`"*, and Step 5 godocs the new helper — but no step touches the **existing** helper's godoc.

**The evidence.**

```
$ sed -n '50,63p' adapter/http/helpers.go
// The helper exists so the ENFORCED range and the PRINTED range are the same
// two values; written inline, each of this package's three sites spelled each
// bound twice.
//
// The sentinel is a PARAMETER because each knob keeps its own errors.Is target
// (ADR 0032 D-X) — here they are msghttp's own ErrInvalidMaxConnections,
// ErrInvalidConnectionBuffer and ErrInvalidReplayBuffer rather than root's.
// All three sites are R1, so all three pass a BARE sentinel; there is no
// msgin.Permanent wrap on a constructor return (ADR 0029 D-M).
//
// This mirrors endpoint.checkRange, routing.checkRange and memory.checkRange
// rather than sharing one of them — the same ADR 0031 D-R / Spec 014 §3.3
// precedent that governs nilOptionAt above. adapter/http/stdlib would get its
// OWN copy for the same reason, if it ever grew a sizing option.
```

**Stated precisely, because the distinction matters:** neither sentence becomes *false*. `checkRange` keeps
exactly three callers —

```
$ grep -rn 'checkRange(' adapter/http/*.go
adapter/http/helpers.go:64:func checkRange(sentinel error, site string, n, lo, hi int) error {
adapter/http/options.go:1219:	} else if err := checkRange(ErrInvalidMaxConnections, "msghttp.WithMaxConnections",
adapter/http/options.go:1226:	} else if err := checkRange(ErrInvalidConnectionBuffer, "msghttp.WithConnectionBuffer",
adapter/http/options.go:1238:		if err := checkRange(ErrInvalidReplayBuffer, "msghttp.WithReplayBuffer",
```

— and all three remain R1. What changes is that *"each of this package's **three sites**"* (`:51`) and *"**All
three sites** are R1"* (`:57`) both read as **package-wide** statements, and after this increment the package has
**six** range-checked sizing options across two helpers. The last paragraph makes it worse: it enumerates the
package's peers (`endpoint`, `routing`, `memory`) and says a *fifth* copy would live in `adapter/http/stdlib` —
with no mention that a sibling now sits seven lines below in the same file.

This is the class CLAUDE.md's stored lesson names — *docs can contradict the code they describe*; all three Plan
028 fix rounds were godoc, not logic — and this increment is adding a second helper to a file whose first helper's
godoc is written as if it were alone.

**Required fix.** Add to Plan Task 1 Step 5 a fourth godoc edit: amend `checkRange`'s godoc to scope its counts
(*"each of the three `int`-typed sites this helper serves"*) and to cross-reference `checkRangeInt64` — why the
sibling exists (the 32-bit truncation, D-AP(b)), and that a caller with an `int64` bound uses it rather than
converting. `checkRangeInt64`'s own godoc, which Step 5 already specifies, should point back the other way, so the
pair reads as a pair from either end.

---

## MINOR N-10 — Step 8's rewrite orphans `exchange_test.go`'s only `math` import

**The claim under attack.** Plan Task 1 Step 8: *"Rewrite `adapter/http/exchange_test.go:613-620` from
`math.MaxInt64` to **the ceiling value**"*, and Spec §6 AC-2c: *"It is rewritten to **the ceiling value**."*
Global constraint 2 adds: *"`byteCapCeiling` is unexported, so tests spell the literal `2147483647` (or
`math.MaxInt32`) — **not** the constant."*

**The evidence.** `math` is used exactly once in the file:

```
$ grep -n 'math\.' adapter/http/exchange_test.go
615:		x := newExchange(t, http.StatusOK, io.NopCloser(strings.NewReader("hello")), msghttp.WithMaxResponseBytes(math.MaxInt64))
```

Constraint 2 offers two spellings and the plan picks neither. If the implementer takes the first — the decimal
`2147483647` — the `math` import becomes unused and **the package fails to compile**:
`"math" imported and not used`. The RED step would then fail for a reason unrelated to the assertion under test,
in a file the plan added only this revision (round-1 B-2) and whose header comment the same step is editing.

Minor in consequence — the compiler catches it immediately — but it is a scheduled, avoidable stop on the one
file the previous round found missing entirely, and it is one word to prevent.

**Required fix.** In Plan Step 8 and Spec §6 AC-2c, say **spell it `math.MaxInt32`** (not the decimal), so the
import stays live and the value reads as the same constant `byteCapCeiling` is defined from. Note the exception
to Global constraint 2's "or the literal" latitude and why.

---

## MINOR N-11 — `WithMaxEventBytes` is PARSE-SIDE ONLY; the SSE server never consults it, and no artifact says so

**The claim under attack.** Every artifact presents the three knobs as one uniform family — Spec §1's table,
Spec §3's contract (*"**No `msghttp` byte cap may be configured above…**"*), ADR Context's table, Plan's
"three knobs" table — and Spec §1.3 item 3 describes `WithMaxEventBytes`'s two accumulation points
(`sse.go:387`, `sse.go:472`) with no scope statement.

**The evidence.** The cap is read only on the **parsing** path:

```
$ grep -rn 'maxEventBytes' adapter/http/*.go | grep -v _test
adapter/http/options.go:204,205,208,209,858,859,1211,1212,1213   # declaration, setter, NewConfig gate
adapter/http/sseclient.go:401:	parser := newSSEParserWithCap(body, c.cfg.maxEventBytes)
adapter/http/sse.go:239:	return newSSEParserWithCap(r, cfg.maxEventBytes), nil
adapter/http/sse.go:253,258:	func newSSEParserWithCap(...)  / &SSEParser{…}
adapter/http/sse.go:387:		if int64(p.dataBuf.Len()) > p.maxEventBytes {
adapter/http/sse.go:472:		if int64(len(buf)) > p.maxEventBytes {
```

**`adapter/http/sse_server.go` does not appear.** The SSE **server** reads `maxConns()`, `connBuffer()`,
`replaySize()`, `heartbeatInterval()`, `perWriteTimeout()` and `slowPolicy()` from the same `*Config`
(`sse_server.go:182, 201, 213, 261, 311, 441`) and **never** `maxEventBytes` — it frames outbound events through
`EncodeSSEEvent` into a `bytes.Buffer` (`:414-420`) with no size check.

**The option's godoc is accurate** and scopes itself correctly — *"caps the number of bytes **NewSSEParser's
SSEParser** buffers for a single Server-Sent Event"* (`options.go:834-836`). The defect is at the design-artifact
level: a reader of Spec 018 reasonably concludes that the ceiling now bounds "the SSE byte cap" in both
directions, when it bounds one. The bundle's own framing invites that reading — the three knobs are
introduced as *"the sole bound on a remote-peer-driven read that is retained in memory"*, and for the SSE server
there is no such bound at all, which is a fact about D-AB's class membership that no artifact states.

This matters for **D-AB's criterion**, not just for prose: an unbounded server-side event size is either outside
the criterion (*"`n` is the sole bound on an accumulation"* — there is no `n`, so no class membership) or a
separate, unrecorded gap. Saying which is the difference between a scoped increment and a silent omission.

**Required fix.** Add a sentence to **Spec §1.3 item 3** stating that `WithMaxEventBytes` is **parse-side only** —
consulted by `NewSSEParser` (`sse.go:239`) and the SSE client (`sseclient.go:401`), never by the SSE server — and
why the server side falls outside D-AB's class (the server's outbound event size is bounded by the caller's own
`Send` input, not by an msgin knob, so there is no `n` to be the sole bound). Mirror one clause into ADR Context's
table row.

**Second, and independent — a clause Step 5's rewrite table drops.** `errors.go:132` reads:

```
$ sed -n '132,133p' adapter/http/errors.go
	// ErrInvalidMaxEventBytes is returned by NewConfig (and so by
	// NewSSEParser) when an explicit WithMaxEventBytes is <= 0. …
```

Plan Step 5's rewrite table row 4-6 replaces all three sentinel godocs with *"…outside [1, 2147483647]"*, and the
`(and so by NewSSEParser)` clause — the only place the sentinel's godoc tells a caller which constructor they will
actually see it from — has no home in the replacement. **Preserve it**: *"returned by `NewConfig` (and so by
`NewSSEParser`) when an explicit `WithMaxEventBytes` is outside `[1, 2147483647]`."*

---

## MINOR N-12 — AC-3's 386 gate is the only gate in the bundle that is never vacuity-probed

**The claim under attack.** Spec §6 AC-3 makes `GOARCH=386 GOOS=linux go vet ./...` the increment's 32-bit guard —
*"`go vet` is the usable form because it type-checks `_test.go` files, which is the whole point, since the 32-bit
exposure lives in test literals"* — and Plan Step 9 and Step 9-again record its exit codes. Spec §6 AC-7 requires
vacuity probes, but scopes them to the **docs-link gate** only.

**The evidence.** Re-derived on this tree:

```
$ GOTOOLCHAIN=go1.25.13 GOARCH=386 GOOS=linux go vet ./...   ; echo "exit=$?"
exit=0
$ GOTOOLCHAIN=go1.25.13 GOARCH=386 GOOS=linux go build ./... ; echo "exit=$?"
exit=0
```

Both exit 0, as the bundle says — and **that is the whole problem**. A gate that has only ever been observed
passing proves nothing about what it would catch, and this one replaced a command that round 1 proved could not
work at all (M-4). The project's stored lesson is explicit: *vacuity-probe every gate* — *a gate that has never
failed proves nothing; and plant the probe where coverage is doubtful — proving it FIRES is not proving it
COVERS.* Coverage is exactly what is doubtful here: the claim is not "vet exits 0" but "vet **type-checks test
files**, so a 32-bit-only overflow in a `_test.go` goes red." Nothing in the bundle demonstrates that.

The rest of the bundle probes conscientiously — Plan Step 6 probes the link gate on the new files rather than on
root (Plan 028's blindness), Step 8 probes the `apidiff` surface diff in `adapter/http` rather than root, and
M3-3/M3-6 probe the gate's own assertions. AC-3 is the omission in an otherwise disciplined set.

**Required fix.** Add a step to Plan Task 1 (adjacent to Step 9): plant a **32-bit-only** overflow in one root
`_test.go` — e.g. `const _ int = 1 << 40` or a call passing `1 << 62` to an `int` parameter — confirm
`GOARCH=386 GOOS=linux go vet ./...` reports **exactly one** failure naming that file, confirm
`GOARCH=amd64 go vet ./...` stays clean (which is what makes it a *32-bit* probe rather than a syntax probe),
revert, and confirm both return to exit 0. Record the probe's output in the Evidence block, and add the probe as
a clause of Spec §6 AC-3 so it is a criterion rather than a step.

---

## Part 3 — independently reproduced: the `(n int)` rejection, and all five of the reviser's corrections

Round 1 closed with a **coordinator's re-derivation note** recording that the coordinator had proposed narrowing
the three options to `(n int)`, and that the reviser refuted it with compiler output rather than accepting it (see
`563ea19`'s commit body: *"I proposed changing the three options from int64 to int … That was wrong, and the
reviser refuted it with compiler output"*). **Round 2 re-ran the experiment from scratch on a fresh probe module,
without reusing the recorded output.** It reproduces exactly.

**The probe.** A throwaway module outside the repository, `go 1.25.0`, `GOTOOLCHAIN=go1.25.13`:

```
$ cat p.go
package probe
import "math"
const byteCapCeiling int = math.MaxInt32
func WithMaxBodyBytes(n int) int { return n }
var _ = WithMaxBodyBytes(2147483648)
var _ = WithMaxBodyBytes(1 << 62)

$ GOARCH=386 GOOS=linux go build ./... ; echo "exit=$?"
# probe
./p.go:9:26: cannot use 2147483648 (untyped int constant) as int value in argument to WithMaxBodyBytes (overflows)
./p.go:10:26: cannot use 1 << 62 (untyped int constant 4611686018427387904) as int value in argument to WithMaxBodyBytes (overflows)
exit=1
```

```
$ cat p.go                                  # the constant-conversion workaround
…
var over = int(int64(math.MaxInt32) + 1)
var _ = WithMaxBodyBytes(over)

$ GOARCH=386 GOOS=linux go build ./... ; echo "exit=$?"
# probe
./p.go:9:16: constant 2147483648 overflows int
exit=1
```

Column offsets differ from round 1's transcript (`:9:26` vs `:12:23`) because the probe file is differently
laid out; the diagnostics are identical.

**All five of the reviser's corrections of the coordinator, verified:**

| # | The reviser's correction | Round-2 verification |
|---|---|---|
| 1 | With `(n int)` and `byteCapCeiling = math.MaxInt32`, the ceiling on 32-bit **is** `math.MaxInt`, so **no `int` literal can exceed it** — the upper arm is *inexpressible*, not merely untested | **CORRECT.** Both candidate literals are rejected at compile time on `GOARCH=386` (output above) |
| 2 | The constant-conversion workaround fails too, because `int64(math.MaxInt32) + 1` is still a **constant expression** | **CORRECT.** `constant 2147483648 overflows int` (output above) |
| 3 | Spec §6 AC-2's `2147483648` literal would not compile on 386, so **AC-3's replacement `go vet` gate would go red** — undoing what Plan 030 just delivered | **CORRECT.** Follows from (1); `go vet` type-checks test files, so a test-file literal that does not compile is a vet failure |
| 4 | **No `fixed`-arm literal exists** that is simultaneously compilable on 386 and above the ceiling, so the three moved rows would become architecture-conditional | **CORRECT at this ceiling.** On 386 the `int` range's maximum *is* the ceiling, so the set of "compilable and above" is empty. **See N-7: this is the one correction that is conditional on the ceiling's exact value** — at `math.MaxInt32 - 1` the set is non-empty (`2147483647`) |
| 5 | `1 << 30` cannot serve the three moved rows under **either** signature, because `1,073,741,824 < 2,147,483,647` — it would be **accepted**, not rejected | **CORRECT.** Arithmetic, and confirmed against the gate: the `fixed` arm's nine existing rows assert `EqualError` on an out-of-range render, which a value below the ceiling cannot produce |

**The reviser was right to overrule the coordinator, and the record should keep that.** The `(n int)` proposal was
attractive on four independent grounds (deletes `checkRangeInt64`, dissolves D-AR(b)'s mutation gap, makes
`1<<62` a compile error on 32-bit, matches `endpoint.WithMaxPayloadBytes`) and was refuted by evidence rather than
by preference. N-7 does not reverse that; it observes that correction 4 — and therefore the rejection as a whole —
holds **given** `byteCapCeiling = math.MaxInt32`, and asks the bundle to say so.

---

## Checked and found CLEAN — 16 rows of verified derivation

Round 3, if there is one, should not re-derive these. Each was checked first-hand against the tree at `46803c6`.

| # | Claim | Where | Verification |
|---|---|---|---|
| 1 | The 386 replacement gate is genuinely clean, and the round-1 form genuinely is not | Spec §2, §6 AC-3; ADR fact 2, D-AR(b); Plan Step 9 | `GOARCH=386 GOOS=linux go vet ./...` → exit **0**; `go build ./...` → exit **0**; `go test -gcflags=all=-e -run=NONE ./...` still exits **1** with `exec format error` ✅ |
| 2 | `1 << 62` renders `4611686018427387904` | Plan Trap 2 | `python3 -c "print(1<<62)"` ✅ (and this is why M-8's residue in the spec is wrong) |
| 3 | `1<<30 = 1,073,741,824 < byteCapCeiling = 2,147,483,647`, so it would be **accepted** | Spec §3.5, §6 AC-4.1; ADR D-AP corollary 1; Plan "three knobs" corollary | arithmetic ✅ — the three moved rows must keep `1<<62` |
| 4 | The `byArm` counting-map trap is real | Spec AC-4.1 site 4; ADR D-AS trap 1; Plan Trap 1 | `:793` `byArm := map[string]int{}`, `:797` `byArm[tc.arm]++`, `:803` `require.Equal(map[string]int{…}, byArm, …)` ✅ |
| 5 | `wantArms` is a key→arm **mapping**, so a pairwise swap is caught | Plan mutant M3-2 | `:766-770` comment records exactly that reasoning; `:772-784` is the map; `:801` is the `require.Equal` message ✅ |
| 6 | The 🔴 blocks are **two**, at the cited offsets, and are instructions this increment fulfils | Spec AC-4.1 sites 10-11; ADR D-AS trap 2 | `:547` *"🔴 WHEN §3.8's CEILING LANDS, THIS GATE WILL GO RED, AND THAT IS CORRECT."*; `:557` *"🔴 THESE THREE ROWS KEEP THE 1<<62 LITERAL — DO NOT CONVERT THEM"* ✅ |
| 7 | `require.Len(t, tests, 19)` at `:753`; `sizingConformanceKeys` at `:179` | Spec AC-4.2; ADR D-AS | ✅ |
| 8 | Plan 031 adds **both** its rows to the **`fixed`** arm and touches `deferred` not at all | Spec AC-4.2 ordering table; ADR D-AS; Plan Step 7 | `docs/plans/031-group-member-bounds.md:203` *"conformance row to the **`fixed`** arm"*, `:428` same ✅ — so the `deferred` key is removed under either order, as claimed |
| 9 | `endpoint.WithMaxPayloadBytes` is `(n int)`, off at `n <= 0`, and the gate files it `safe` at `math.MaxInt` | Spec §3.4a; ADR D-AN(b) | `endpoint/flowcontrol.go:144`; godoc `:138-143` quoted accurately; gate row key `:655`, arm `:656`, `math.MaxInt` `:669` ✅ |
| 10 | The three sentinels' current messages and declaration lines | Spec §2, ADR D-AQ table | `errors.go:19`, `:77`, `:138` — `"msghttp: max … bytes must be > 0"` ✅ |
| 11 | The shipped `checkRange` takes `int`, lives at `helpers.go:64`, and renders `"%w: %s: %d not in [%d, %d]"` | Spec §3.5a; ADR fact 4, D-AP(b) | `helpers.go:64`, `:68` ✅ |
| 12 | `checkRange` has exactly **three** call sites, all R1, all in `NewConfig` | Spec §3.5a; ADR D-AP table row 2 (*"the nine shipped `int` call sites"* refers to the workspace's four copies, not this one) | `options.go:1219`, `:1226`, `:1238` ✅ |
| 13 | `maxResponseBytes` and `maxEventBytes` have no accessor; `maxBody()` is unexported | Spec §6 AC-1's deletion note; Plan's "do not assert an accessor" | `options.go:272` `func (c *Config) maxBody() int64`; no `maxResponse`/`maxEvent` accessor; `sseclient.go:401` reads the field directly ✅ |
| 14 | Both SSE caps check **after** the append | Spec §1.3 item 3 | `sse.go:387` (`WriteString` then `if int64(p.dataBuf.Len()) > …`), `sse.go:472` (`append` then `if int64(len(buf)) > …`) ✅ |
| 15 | The four out-of-range test call sites, and only those four | Spec §3.1a's first two classes; Plan Step 2 | `sizing_option_class_gate_test.go:572`, `:581`, `:590` (`1<<62`); `adapter/http/exchange_test.go:615` (`math.MaxInt64`) ✅ — the *classification* is right even though the *totals* are not (N-5) |
| 16 | Nothing in `sizing_option_class_gate_test.go` changed between `1212c63` and `46803c6` | implicit in every offset the bundle cites | `git diff 1212c63 HEAD -- sizing_option_class_gate_test.go` empty ✅ — so N-2's and N-5's discrepancies are authoring errors, not drift |

**Also clean, and worth preserving as reasoning rather than derivation.** M-5's rewrite is the revision's best
work: the width-safety/portability argument is unconditionally true, states plainly that a 64-bit build *can*
honour a larger cap, and no longer rests on a sentence that a reader on `amd64` could falsify in one line. D-AN(b)'s
`endpoint.WithMaxPayloadBytes` treatment (M-7) does the harder thing — it names the strongest counter-example,
concedes there is no project-wide stance, and grounds the divergence in which sentinel values are already spoken
for rather than in a doctrine. D-AS's tombstone decision, D-AM's "same class, different remedy" call, and the
decision to record D-AR(b)'s mutation gap rather than fake a kill all survive round 2 unchanged.

---

## Auditor's method note

Every command in this record was run on the tree at `46803c6` (clean worktree) with `GOTOOLCHAIN=go1.25.13` on
darwin/arm64. The gate's site derivation and its 18-line count, the six `safe`-arm signatures and their
`math.MaxInt` assertions, the 49/40/24 call-site classification, the 386 exit codes for all three command forms,
the `math` single-use in `exchange_test.go`, the `must be > 0` case split, the `maxEventBytes` reader census
across `sse_server.go`, the `checkRange` call-site count, the Plan 031 task list and arm assignment, and the
`(n int)` / `math.MaxInt32 - 1` probe modules are all first-hand output, not transcription. No file in the
repository was modified; the probe module lived outside it and was discarded.

**What round 2 deliberately did NOT re-derive:** round 1's 20 clean rows, except the handful a new finding
depended on — the `byArm` counting trap, the `wantArms` mapping, the `checkRange` signature and render, and the
`1<<30`-is-below-the-ceiling arithmetic, all of which are re-derived above and all of which still hold.

---

**VERDICT: NOT SAFE TO IMPLEMENT.**

*Round 1 failed at derivation against the tree. Round 2 fails at the derivations' predicates — three of twelve
findings are a mechanical procedure, correctly adopted, whose selector does not match the property it was adopted
to track. The BLOCKER is the sharpest instance: an invariant restated exactly as round 1 demanded, true of
thirteen rows, false of six, and applied literally it would leave the gate green while the probe it protects
stopped running.*
