# Plan 032 — Bound the three `msghttp` byte caps, and empty the class gate's `deferred` arm

> **For agentic workers:** REQUIRED SUB-SKILL: `superpowers:subagent-driven-development` (the project default) or
> `superpowers:executing-plans`. Steps use checkbox (`- [ ]`) syntax.
>
> **Go-skills hard rule ([CLAUDE.md](../../CLAUDE.md), restated here because `superpowers:writing-plans` omits
> it):** every task starts from **`cc-skills-golang:golang-how-to`**, the always-on orchestrator, which routes
> this increment to `golang-safety` (numeric conversion overflow, unbounded reads, defensive bounds),
> `golang-security` (remote-peer-driven input at the HTTP boundary), `golang-error-handling` (sentinel reuse,
> `%w` wrapping, `errors.Is`), `golang-design-patterns` (functional options, the set-flag pattern),
> `golang-testing` and `golang-documentation`. Load the primary skill **plus all applicable secondary skills
> together, up front** — do not work from memory.
> **`superpowers:test-driven-development`** governs every task: red → green → refactor, failing test first, never
> implementation ahead of a failing test. **`gopls`** (via the `LSP` tool) for all navigation, diagnostics and
> refactoring — go-to-definition, find-references, rename, post-edit diagnostics — **not `grep`** when reasoning
> about Go symbols. The project-local overrides apply and beat samber's guidance where they conflict:
> **`table-test`** (assert-closure form, never `want`/`wantErr` fields; `ctx` modifier; `t.Context()`),
> **`use-mockgen`** (uber-go/mock, `--typed`, alongside the interface — **not exercised** by this increment; no
> new interface or test double is introduced), and **`use-testcontainers`** (**not exercised** — this increment
> touches no external resource; `httptest` is stdlib and stays).
>
> **This plan is deliberately thin** (Plan 024/026/028/029/031 precedent): signatures, positions, branch coverage
> and commit boundaries — **no embedded implementations**. Write the code TDD-first from the tables below.

**Revision 5 — post-audit-round-4. ✅ CLEARED FOR IMPLEMENTATION.**

**Four rounds of the adversarial design audit have run** over the assembled bundle
([Spec 018](../specs/018-byte-cap-ceilings.md) + [ADR 0034](../adrs/0034-byte-cap-ceilings.md) + this plan).
Round 1 returned **NOT SAFE TO IMPLEMENT** — 3 BLOCKERs, 7 MAJORs, 4 MINORs
([`032-audit-round-1.md`](032-audit-round-1.md), **immutable**). Round 2 returned **NOT SAFE TO IMPLEMENT** —
1 BLOCKER, 5 MAJORs, 6 MINORs ([`032-audit-round-2.md`](032-audit-round-2.md), **immutable**). Round 3 returned
**NOT SAFE TO IMPLEMENT** — 0 BLOCKERs, 3 MAJORs, 4 MINORs ([`032-audit-round-3.md`](032-audit-round-3.md),
**immutable**), and verified that **all twelve round-2 findings LANDED, both round-1 residues closed, and nothing
regressed.** **Round 4 returned SAFE TO IMPLEMENT** — 0 BLOCKERs, 1 MAJOR, 9 MINORs
([`032-audit-round-4.md`](032-audit-round-4.md), **immutable**), verifying **6 LANDED, 3 LANDED-BUT-FLAWED,
0 NOT LANDED, 0 REGRESSED** and the N-9 residue **CLOSED**.

✅ **This revision folds R4-1…R4-10, and NO FIFTH ROUND IS REQUIRED — the auditor said so explicitly:**
*"Fold R4-1 through R4-10 in without a fifth round; they are corrections, not re-designs."* Not one of the ten
changes a decision, a value, a signature, a task boundary or a commit boundary. **Implementation may begin at
Task 1**, subject to CLAUDE.md's separate *ask-before-implementing* gate and its **SDD default** (a fresh
implementer subagent per task; the coordinator verifies green and commits; an adversarial reviewer before
delivery). The per-task commits below are pre-authorized **once an execution mode is chosen** (Global
constraint 9).

🔴 **Serialization — this changed at revision 5, and it changes Step 7.**
[Plan 031](031-group-member-bounds.md) is **still in audit and undelivered**; this bundle is cleared, so
**PLAN 032 NOW LANDS FIRST.** Both increments edit `sizing_option_class_gate_test.go`. Under this order Step 7's
*"If Plan 031 has not landed"* branch is the expected one — `{"fixed": 12, "rejects": 1, "safe": 6}` and
`require.Len(t, tests, 19)` — and **Plan 031 rebases, re-deriving its own gate figures against the post-032
file** rather than against the pre-032 counts its current revision assumes. **Still run Step 7's determination:**
the order is *expected*, not *guaranteed*, and the check costs one `git log`.

**What round 4 changed in THIS plan** — ten corrections, no re-design:

| Finding | Change here |
|---|---|
| **R4-1** MAJOR (Spec §6 AC-1 stated **both** definitions of its third clause) | Spec-level; the plan's contribution is `:206`'s trailing *"accepted with an observable effect"* → **"accepted, with its product usable"**, so the constraint-6 history block stops re-introducing the retired term. |
| **R4-2** (M3-6 deleted here, still cited as a live probe in the spec and the ADR) | Discharged there — Spec §6 AC-7 and ADR D-AR(b) now read **M3-1…M3-5 and B1-10**. **Nothing in this plan changes**; the deletion, the Evidence note and the checklist item were already correct, and *satisfying them* is what made the other two false. |
| **R4-3** (B1-10's mutant is killed but **NOT attributably**) | 🔴 **The one an implementer would otherwise get wrong. B1-10 re-specified.** `permanentError.Error()` prefixes `"msgin: permanent: "` (`reliability.go:13`), so the bare wrap also reds every `EqualError` in AC-1/AC-2 and the three moved rows — deleting the `assert.False` line entirely reds the same cases. The mutant now has a **two-part targeted arm**: wrap **and** update that case's `EqualError` expectation, so only `assert.False` fails. The *"ONLY mutant proving"* claim is qualified. |
| **R4-4** (a seventeenth site at `:22`; site 5's disposition under-specified) | Step 6 gains **site 17 (`:22`)** — unreachable by the union selector — and **site 5 is made explicit: TOMBSTONE the header's arm-list bullet**, matching site 8, because deleting it falsifies `:22`. Inventory **16 → 17** here, in Global constraint 10 and in the delivery checklist. |
| **R4-5** (the INV-6 comparison is at `exchange.go:135`, not `:133`) | Spec/ADR-level; this plan never cited the offset. No change. |
| **R4-6** (constraint 6 tightened to `1 MiB + 1`, the spec's prose still argued from 2 MiB / 32×) | **This plan's constraint 6 was already correct** and is the authority; the stale prose was in Spec §6 AC-1 and is fixed there, at `1 MiB + 1` and **64×**. No change here. |
| **R4-7** (`:95` said *"has run TWICE"*, `:98` said *"All three rounds"*) | The header block above is rewritten: **four rounds**, round 4 **cleared** the bundle, and the *"Round 4 must run before implementation begins"* instruction is **discharged and replaced** — leaving it standing would order the fifth round the auditor said not to convene. |
| **R4-8** (`git add -N` is a no-op; the file count goes stale) | Task 2 Steps 4 and 5 restate `git add -N` as a **guard whose no-op result is the expected state**, and replace the literal *"six"* with the derived set — *every `docs/specs/018-*`, `docs/adrs/0034-*`, `docs/plans/032-*` file, whatever their number* (it is **seven** now that `032-audit-round-4.md` exists). |
| **R4-9** (no mutant reverts a sentinel's text or the `site` argument) | New mutant **B1-11**, plus an optional `site`-string arm. Step 4 item 4's rename is the increment's stated behavioral change and six new `EqualError` assertions carry it; every previously-listed mutant is killed by a bare `require.ErrorIs`, so none targeted the text. |
| **R4-10** (the deferred follow-up is recorded where the next session will not look) | Step 11b's `docs/HANDOVER.md` bullet now **opens a new §7 row** for *"derive the class gate's prose counts from `wantArms` at test time"* in the same edit that closes item 6. §7 is the discoverable backlog; Spec 018 §8 is not. |

**What round 3 changed in THIS plan:**

| Finding | Change here |
|---|---|
| **NEW-1** MAJOR (`:48-49` is a third instance of the arm→literal claim) | Step 6 site 14 extended from `:47` to **`:47-49`**, with the required replacement wording: narrow *"exceeds every ceiling"* to *"every **`int`-typed** ceiling"*, state inline that `byteCapCeiling` is an `int64` ceiling above `1<<30`, and fix or delete the *"largest is `1<<20`"* parenthetical. |
| **NEW-2** MAJOR (the widened selector is still a token enumeration) | Step 6 gains sites **15 (`:409`)** and **16 (`:601`)**; **site 12 absorbs `:799-800`** (one `require.Equal` message, not four lines); the grep is replaced by a **deliberately noisy** selector; and the step now says in terms that **three rounds have each fixed the named sites while the defect returned through new ones**, with the structural follow-up recorded at [Spec 018 §8 item 5](../specs/018-byte-cap-ceilings.md). |
| **NEW-3** MAJOR (three inconsistent Plan 030 states, one of them operative) | The plan-number box and the Task 1 rebase note **replaced with the delivered state** — Plan 030 is fully delivered; `options.go`/`helpers.go` are **not contested**; the line-95 rebase instruction is **discharged**. Step 11b gains an explicit disposition for `030-post-029-maintenance.md`. |
| **NEW-4** MINOR (mutant M3-6 cannot discriminate) | **M3-6 deleted**; B1-10 gains the clause recording that it covers the moved rows' copy of the assertion. |
| **NEW-5** MINOR (B1-4's 2 MiB fixture is larger and weaker) | **B1-4 re-specified as the boundary pair** — `1<<20` accepted / `1<<20 + 1` rejected — mirroring the shipped `exchange_test.go:309-334`; Global constraint 6's bound tightened to **`1 MiB + 1`**. |
| **NEW-6** MINOR (AC-1's *"observable"* is unachievable) | The *"its product is usable"* paragraph states plainly that the **ceiling's effect is unobservable by construction**; Spec §6 AC-1 reworded to match this plan's own heading. |
| **NEW-7** MINOR (the narrow sub-check has no command) | The narrow form is **dropped**, not retained. |
| **Note 1** (D-3's count is not wrap-safe) | Step 11 D-3 replaced with the `perl -0777` wrap-tolerant form (Plan 030 Task 1's precedent), asserting **3 occurrences**, still vacuity-probed. |
| **Note 2** (`:33` and `:521` unaccounted for) | Step 6 gains an explicit over-inclusion account naming both. |
| 🔴 **Coordinator, not the audit** | Spec §6 AC-1's *"the small-`n` proof already exists"* named a test that **does not exist** (`grep -rn 'body too large' --include='*.go' .` → 0 hits) and whose 64 MiB fixture would breach constraint 6 by 32×. Corrected in Spec §6 AC-1 and in B1-4 below. |

**What round 2 changed in THIS plan:**

| Finding | Change here |
|---|---|
| **N-1** BLOCKER (the restated gate invariant is false for the six `safe`-arm rows) | Step 6 Trap 3 rewritten **two-dimensionally** — the **arm** fixes the property, and only *within the reject arms* does the parameter type choose the literal. Revision 2's form would have demoted six `math.MaxInt` rows to `1<<30` and silently disabled the int32-truncation probe. |
| **N-2** (the derivation grep's predicate misses two sites) | Step 6's command **widened to the property**; the inventory went to **14 sites**, adding `:26` and `:47`. 🔴 **Round 3 found the widened form still under-selecting — see NEW-2 above; the inventory is now 16 and the selector is deliberately broad.** |
| **N-3** (Global constraint 6 regressed) | Constraint 6 **restored to bound the FIXTURE**. Revision 2's *"no test may CONFIGURE a cap above ~1 MiB"* forbade B1-1, B1-3, B1-5, B1-9, both AC-2 upper-arm assertions and AC-1 itself. |
| **N-4** (the "whichever lands second" protocol is unilateral) | **Plan 032 owns Spec 016 §2.1 unconditionally** and re-derives it from the tree; the *"amend only the rows this increment owns"* instruction is deleted. |
| **N-5** (call-site totals wrong) | Step 2's expected shape corrected to **49 hits / 40 calls / 24 accepted**, and the step now says **re-derive, do not compare**. |
| **N-6** (the parents were scheduled one commit late) | **Spec 016 / ADR 0032 / HANDOVER fold back in TASK 1's commit.** Task 2 is gates + status flip only. |
| **N-7** (`math.MaxInt32 - 1` dissolves the `(n int)` rejection) | The "three knobs" signature box no longer rests on *"the argument licenses exactly one ceiling"*; it names the alternative and the trade. Design-level; argued in Spec §3.5 / ADR D-AP(a). |
| **N-8** (the falsification sweep's grep case) | Step 11 D-1 uses `grep -rin` and is **vacuity-probed**; D-3's count assertion likewise. |
| **N-9** (`checkRange`'s godoc counts) | Step 5 gains a fourth godoc edit: amend the **existing** `checkRange` godoc and cross-reference the sibling. |
| **N-10** (the orphaned `math` import) | Step 8 says **spell it `math.MaxInt32`**, not the decimal. |
| **N-11** (`WithMaxEventBytes` is parse-side only; a dropped godoc clause) | Step 5's rewrite table **preserves `errors.go:132`'s `(and so by NewSSEParser)`**; the parse-side scope is stated in Spec §1.3 item 3. |
| **N-12** (AC-3 never vacuity-probed) | New **Step 9b**: plant a 32-bit-only overflow in a root `_test.go`, confirm exactly one 386 vet failure and an amd64-clean run, revert. |
| **M-8 residue / m-11 residue** | The wrong-string instruction lived in Spec §6 AC-4.1 (fixed there); the `WithReplayBuffer` citation was fixed in this plan at revision 2 and **not** in the ADR (fixed there now). |

**What round 1 changed in THIS plan:**

| Finding | Change here |
|---|---|
| **B-1** (Task 1 could not be a green commit) + **m-14** (six godoc sentences false for one commit) | **Tasks 1, 2 and 3 are MERGED into one task.** Global constraint 8's contradiction with the old Task 3 Step 2 is gone. |
| **B-2** (`exchange_test.go` branch 20 breaks, in no task's Files) | `adapter/http/exchange_test.go` added to Task 1's Files; branch 20 rewritten; the message-string test-safety check **replaced** by a call-site classification. |
| **B-3** (7 sites of ≥12, offsets pre-Plan-030) | The gate inventory is **derived by command**, not transcribed. |
| **M-4** (the 386 command is red on an untouched tree) | `go vet` + `go build`, not `go test -run=NONE`. |
| **M-8** (wrong string; missing `IsPermanent`) | Each row asserts the value **it** passes, plus `assert.False(t, msgin.IsPermanent(err), …)`. |
| **M-9** (Plan 031 falsifies three normative literals) | Gate counts expressed as **deltas**, with both landing orders. |
| **M-10** (AC-1 requires a forbidden accessor assertion) | Spec AC-1 fixed; this plan's "do not assert an accessor" is now consistent with it. |
| **m-13** (constraint 6 vs the 2 MiB fixture) | Constraint 6 reworded to bound the **cap under test**, not the fixture. 🔴 **Round 2 found this a REGRESSION (N-3) and revision 3 reverses it — the FIXTURE is what is bounded.** |
| **M-5 / M-6 / M-7 / m-11 / m-12** | Design-level; discharged in Spec 018 / ADR 0034. **M-6's outcome matters here: `(n int)` was tested and rejected**, so the `int64` signature and `checkRangeInt64` stand. |

🔴 **The design this plan executes was decided WITHOUT USER RATIFICATION.** Every decision in
[ADR 0034](../adrs/0034-byte-cap-ceilings.md) (**D-AM** … **D-AT**) is open to reversal.
[Spec 018 §8](../specs/018-byte-cap-ceilings.md) lists the four worth a second look. **One of them changes this
plan's shape:**

- **D-AN(b)** — *no off-state*. If the user wants an explicit "unbounded" sentinel, that is a **new exported
  const plus a branch in Task 1**, and it must land before Task 1 starts. [Spec 018 §3.4a](../specs/018-byte-cap-ceilings.md)
  now argues this against the shipped counter-example (`endpoint.WithMaxPayloadBytes`) round 1 surfaced.
- **D-AO** — *the ceiling is `math.MaxInt32`*. Any other value changes one constant and every rendered-message
  assertion in Task 1 (`2147483647` appears in ~8 string literals, across three files). Changing it after Task 1
  is a cross-file sweep, not a one-line edit.

**The adversarial design audit has run FOUR TIMES, and round 4 CLEARED this bundle.**
[CLAUDE.md](../../CLAUDE.md) makes it a hard gate: a fresh Opus subagent attacks the complete bundle —
[Spec 018](../specs/018-byte-cap-ceilings.md) + [ADR 0034](../adrs/0034-byte-cap-ceilings.md) + this plan —
**together**, before any implementation code. **Rounds 1-3 returned NOT SAFE TO IMPLEMENT**
([round 1](032-audit-round-1.md), [round 2](032-audit-round-2.md), [round 3](032-audit-round-3.md)); **round 4
returned SAFE TO IMPLEMENT** ([round 4](032-audit-round-4.md)). Two rounds is this project's established norm
and this bundle needed four; Plan 029 needed five. **No fifth round is required** — round 4's instruction is
verbatim *"fold R4-1 through R4-10 in without a fifth round; they are corrections, not re-designs."*

> 🔴 **Round-4 R4-7: revision 4 said *"has run TWICE"* here and *"All three rounds"* three lines later, and
> `:100`'s *"exhausted it by 50%"* agreed with neither.** A header that under-counts the scrutiny a design has
> had is how a fifth round gets convened. **Do not restore a percentage or a fixed count** — state the round
> number and the verdict.

Round 3's verdict named the class-gate inventory as what a fourth round should attack first, and **round 4 did,
finding a seventeenth site (`:22`, R4-4) that no selector can reach — and cleared the bundle anyway**, because
the harm no longer depends on the inventory being complete: it is blocked by a standalone delivery-checklist
invariant (the gate's own `require.Equal` message reports the post-move partition). The durable fix stays a
backlog item (Step 11b; [Spec 018 §8 item 5](../specs/018-byte-cap-ceilings.md)).

> **Plan number — re-derived, not assumed.** `ls docs/plans/[0-9]*.md | sed -E 's|.*/([0-9]{3}).*|\1|' | sort -u | tail -3`
> → `029 030 031`. Both numbers are **TAKEN** ([`030-post-029-maintenance.md`](030-post-029-maintenance.md),
> [`031-group-member-bounds.md`](031-group-member-bounds.md)). This plan is **032**.
>
> 🔴 **File overlap — corrected at revision 4 (round-3 NEW-3). Revisions 1-3 asserted THREE different states for
> Plan 030 in three places — *undelivered*, *has landed*, and *being edited RIGHT NOW* — one of which was an
> operative instruction. This box is now the single place that state lives; everywhere else points here.**
>
> | Plan | State | Touches | Overlap with 032 |
> |---|---|---|---|
> | **030** (backlog sweep) | ✅ **FULLY DELIVERED** — Task 1 `1a1c135`, Task 2 `d2c69fe`, Task 3 `7ab91cd` | `adapter/http` godoc, test-file constants, 386 arm | **NONE — `adapter/http/options.go` and `adapter/http/helpers.go` are NOT contested, and the rebase instruction earlier revisions carried is DISCHARGED, not operative.** What 030 *did* leave behind matters and is kept: the gate's `fixed`/`rejects` arms are already at `1<<30`, its `safe` arm at `math.MaxInt`, and **every offset Step 6 cites is post-030**. |
> | **031** (group members) | ⏳ **undelivered, under concurrent revision** | `routing`, `adapter/memory`, `adapter/database/sql`, **`sizing_option_class_gate_test.go`** | **The class gate is shared** (ADR 0033 **D-AL** extends it by hand; ADR 0034 **D-AS** empties its `deferred` arm). Whichever lands second rebases. Step 7 handles both orders. |
>
> 🔴 **How to derive a plan's delivery state — and how NOT to.** Both naive signals lied about Plan 030 and
> misled round 3:
>
> | Signal | Returned | Why it lies |
> |---|---|---|
> | `grep -c '[x]' docs/plans/030-post-029-maintenance.md` | **0** | its boxes were **never ticked during execution** — an executed plan reads identically to an untouched one |
> | `git log --oneline \| grep -i 030` | **1 commit** (`d2c69fe`) | **two of the three task commits omit "030" from the subject line** |
>
> ```bash
> git log --format='%h %s' --grep='Plan: 030'      # ← the reliable signal: the trailer
> ```
>
> Re-derive with that form, plus `git log --oneline main..` on each live branch, before Task 1.

**Goal.** Deliver [Spec 018](../specs/018-byte-cap-ceilings.md): the three `msghttp` byte caps — `WithMaxBodyBytes`,
`WithMaxResponseBytes`, `WithMaxEventBytes` — gain a finite, stated ceiling, so no configuration leaves a
remote-peer-driven read into retained memory unbounded. This **closes the last deferred remedy** in the sizing-option
class that [Spec 016](../specs/016-sizing-option-bounds.md) opened.

**Architecture.** [ADR 0034](../adrs/0034-byte-cap-ceilings.md) — **D-AM** (same class as the nine, different
remedy; the class is not split), **D-AN** (a representability ceiling; **no off-state**), **D-AO**
(`byteCapCeiling = math.MaxInt32`, one shared constant), **D-AP** (`checkRangeInt64` sibling — not a generic,
never `int(n)`), **D-AQ** (reuse the three sentinels, genericise their messages, `[lo, hi]` render at both arms),
**D-AR** (the sixth read is bounded not restructured; the 32-bit guard is a compile arm, an accepted gap),
**D-AS** (empty the gate's `deferred` arm; keep the name as a tombstone), **D-AT** (ship Spec 016 §3.8 item 2's
undelivered godoc; amend every artifact recording the old arm).

**Predecessors this builds on, not re-argues.** [Spec 016](../specs/016-sizing-option-bounds.md) /
[ADR 0032](../adrs/0032-sizing-option-bounds.md) / [Plan 029](029-sizing-option-bounds.md): **D-W** (a stated
ceiling), **D-X** (sentinel reuse + the wrap shape), **D-AB** (the membership criterion), and the shipped
`checkRange` helper and class gate. [ADR 0031](../adrs/0031-nil-option-elements.md) **D-R** (per-package helper
duplication) is D-AP's precedent. [ADR 0029](../adrs/0029-eip-lexical-alignment.md) **D-M** (a constructor return
is not `Permanent`-wrapped).

**Tech stack.** Go 1.25 (`GOTOOLCHAIN=go1.25.13`). Touches **one** module — root, packages `msghttp`
(`adapter/http`) and `msgin` (the root class gate). The delivery gate is still all **eight**. **No Docker
needed.** No new dependency, in any module.

**Traceability.** Implements Spec 018; decided by ADR 0034. Every commit carries `Spec: 018`, `Plan: 032`,
`ADR: 0034` trailers. Branch: `fix/byte-cap-ceilings`, off `main`.

---

## Global constraints

1. **Start every task from `cc-skills-golang:golang-how-to`**, plus the secondary `golang-*` skills it routes to
   (header note). **TDD via `superpowers:test-driven-development`** — failing test first, always. **`gopls` for
   navigation and refactoring**, not text search. **`table-test` / `use-mockgen` / `use-testcontainers` override**
   samber's testing guidance wherever they conflict. This is restated per-task in each task's first step; it is
   **not** delegated to an SDD dispatch prompt.
2. **Blackbox tests only** — `package msghttp_test`, `package msgin_test`, exercising the exported API. No
   whitebox fallback. A test that seems to need an unexported helper is rewritten through the public surface.
   `byteCapCeiling` is unexported, so tests spell the literal `2147483647` (or `math.MaxInt32`) — **not** the
   constant.
3. **Assert-closure tables** — every case carries `assert func(t *testing.T, …)`; never `want`/`wantErr` fields.
   `t.Context()`, never `context.Background()`.
4. **The error shape is the shipped `checkRange` render, unchanged:**
   `fmt.Errorf("%w: %s: %d not in [%d, %d]", sentinel, site, n, lo, hi)`. **No `msgin.Permanent` wrap** — these
   are constructor returns (ADR 0029 D-M / ADR 0032 D-X).
5. **No new exported symbol, of any kind** (D-AN(b), D-AO, D-AP). `apidiff` must report **0 removals / 0
   additions**, and the AST exported-symbol set diff (Plan 030 Global constraint 1's shape, probed in
   `adapter/http` **not** root) must be empty. A task that appears to need an exported symbol has hit a design
   fault: **stop and escalate.**
6. **The FIXTURE is what is bounded, not the cap.** No test may present a body / response / event **fixture**
   larger than **`1 MiB + 1` bytes** (`1<<20 + 1` = 1,048,577), and **never** one sized to `byteCapCeiling`. The
   fixture is the hazard: a 2 GiB body peaks near **~4 GiB** through `io.ReadAll`'s doubling, in a package whose
   sibling runs `goleak.VerifyTestMain`, so it cannot be written.
   **A cap may be CONFIGURED at any legal value, including `byteCapCeiling` itself and `byteCapCeiling + 1`** —
   an `int64` field costs eight bytes to set and allocates nothing. The property *"the cap caps"* is a fact about
   the comparison, not about the ceiling value, so it is proven at small `n` with a small fixture; the ceiling
   itself is exercised by **`NewConfig` only** (Spec 018 §6 AC-1). **Branch B1-4's `1<<20 + 1` reject arm is the
   largest fixture in this increment, and it IS the bound** — the bound is derived from the boundary pair, not
   the other way round.

   > 🔴 **Revision 3 set this bound at 2 MiB to accommodate B1-4's fixture; round-3 NEW-5 found that fixture both
   > larger and weaker than the alternative, so the bound tightens with it.** A 2 MiB body rejected under the
   > unset cap proves only that the default lies somewhere in `(0, 2 MiB)` — not that it is 1 MiB, which is the
   > case's own name — and its listed mutant (*delete the default assignment ⇒ the cap reads `0`*) is killed by a
   > **one-byte** body. The boundary pair `1<<20` accepted / `1<<20 + 1` rejected pins the default at exactly
   > `1048576`, kills two mutants the 2 MiB fixture does not (`default = 1<<20 ± 1`), lands on
   > `MaxBytesReader`'s exact boundary (`encode.go:102`), and **halves the peak allocation**. See B1-4.

   > 🔴 **Revision 2 swapped the subject and made this worse, not better (round-2 N-3).** Round-1 **m-13**
   > correctly flagged that revision 1's *"no test **reads** more than 1 MiB"* collided with **one** branch;
   > revision 2 "fixed" it to *"no test may **configure** a cap above ~1 MiB"*, which bounds the harmless thing
   > (an integer) and collides with **five** branches — **B1-1** (`2147483647`), **B1-3** (`2147483648`),
   > **B1-5** (`2147483647`), **B1-9** (`1<<62`) — plus **both AC-2 upper-arm `EqualError` assertions** and
   > **AC-1's own first bullet**, which round-1 M-10 rewrote this same revision to require
   > `NewConfig(WithX(byteCapCeiling))` → **accepted, with its product usable**. An implementer applying revision 2
   > literally deletes the increment's entire upper arm. **Do not restore the revision-2 wording on the strength
   > of the m-13 citation.** *(🔴 **Round-4 R4-1:** this clause read *"accepted with an observable effect"* until
   > revision 5 — the term round-3 NEW-6 retired, re-introduced in a paragraph that has nothing to do with the
   > distinction. See the *"Its product is usable"* block below Task 1's branch table.)*
7. **Mutation-prove every new assertion** with a mutant that targets **that** assertion (the project's standing
   rule: *a killed mutant is the evidence, not a green run*). Each task carries a mutant table; record the killed
   mutant per case in the task's Evidence block. **A case that survives its own mutant is rewritten.**
   **Exception, stated: the width mutant of Task 1 (M1-7) is NOT behaviorally killable on darwin/arm64** — it is
   caught by the 386 compile arm and by review. ADR 0034 **D-AR(b)** accepts this gap; do not fake a kill.
8. **Each task is a green unit** — `GOWORK=off go test ./... -race -shuffle=on` passes in the root module before
   its commit. No WIP or broken-build commits. 🔴 **This is why Tasks 1-3 of revision 1 are now ONE task**
   (round-1 B-1): the class gate lives in the **same module** the production change edits, so a task that changes
   `NewConfig` and leaves the gate asserting the old behavior cannot be green. Revision 1 required the gate to be
   *"already red after Task 1"*, which contradicted this constraint and CLAUDE.md's per-task-commit
   pre-authorization. **There is no longer any point in this plan at which the root suite is red between
   commits.**
9. **Per-task commit is pre-authorized** by CLAUDE.md's plan-execution exception, once this plan is approved
   **and** an execution mode is chosen. `git push`, merges, tags and branch deletion still need explicit
   per-action approval.
10. **Line numbers in this plan are anchors, not addresses — and for the class gate, DERIVE the site list rather
    than reading one.** 🔴 Round-1 **B-3** found revision 1's gate inventory was **7 sites of at least 12**, with
    **every offset but one stale** because Plan 030's conversion had already landed (`d2c69fe`). 🔴 Round-2
    **N-2** found revision 2's *derivation* under-selected: its grep's predicate was "mentions `deferred`" while
    the property being changed is "records the `fixed` partition", so two sites (`:26`, `:47`) were invisible and
    the pasted line count was wrong. 🔴 Round-3 **NEW-2** found the *widened* form still under-selecting by
    three; 🔴 round-4 **R4-4** found a **seventeenth** site (`:22`) that the deliberately-noisy form cannot reach
    either, because it carries no arm name, no literal and no digit. **The inventory is 17 sites; the broad
    command in Task 1 Step 6 is the authority for the 16 a selector can find, and `:22` is listed because one
    cannot.** This project's stored lesson is *derive move-lists mechanically* — **and derive them against the
    PROPERTY, not against a token list drawn from the sites you already know about** (*fix the class, not the
    instance*). **Re-derive every `adapter/http` offset with `gopls`** (match on the function name, the sentinel
    name and the predicate shape), and **generate** the gate's site list with the `grep` in Task 1 Step 6.
    **The inventory is a STOP-GAP and Step 6 says so; the durable fix is the Spec 018 §8 item 5 refactor.**
11. **Docs links are relative to the CITING file's directory.** A bare `[0034](0034-byte-cap-ceilings.md)` from
    inside `docs/plans/` silently 404s. The pre-merge link gate (CLAUDE.md, **both arms**) is a Task 2 blocker.

---

## The three knobs — one table, read it before Task 1

| Knob | Option | `NewConfig` gate | Sentinel | Default | Accumulates in |
|---|---|---|---|---|---|
| body | `WithMaxBodyBytes(n int64)` `options.go:463` | `options.go:1189-1193` | `ErrInvalidMaxBodyBytes` `errors.go:19` | `1<<20` `options.go:23` | `io.ReadAll` `[]byte` `encode.go:102` |
| response | `WithMaxResponseBytes(n int64)` `options.go:767` | `options.go:1201-1205` | `ErrInvalidMaxResponseBytes` `errors.go:77` | `1<<20` `options.go:30` | the **retained reply payload** `exchange.go:130-131` |
| event | `WithMaxEventBytes(n int64)` `options.go:856` | `options.go:1211-1215` | `ErrInvalidMaxEventBytes` `errors.go:138` | `1<<20` `options.go:44` | `dataBuf` `sse.go:387` / line `buf` `sse.go:472` |

**All three gates are Spec 016's R1-a shape** — `if !set { default } else if <bad> { return nil, sentinel }`.
**None is R2**, so this increment touches no latch and has no ADR 0031 D-U interaction.

> **The `int64` signature is a tested DECISION, not an accident** (round-1 M-6; [Spec 018 §3.5](../specs/018-byte-cap-ceilings.md),
> ADR 0034 **D-AP(a)**). Narrowing to `(n int)` would delete `checkRangeInt64` and dissolve D-AR(b)'s mutation
> gap — but **at `byteCapCeiling = math.MaxInt32`** the ceiling on 32-bit equals `math.MaxInt`, so **no `int`
> literal could exceed it**: AC-2's `2147483648` would not compile on 386, AC-3's vet gate would go red, and the
> gate's three moved rows would become architecture-conditional.
>
> 🔴 **And that rejection is conditional on the ceiling's VALUE, which revision 2 concealed behind *"the argument
> licenses exactly one ceiling"* (round-2 N-7).** At `byteCapCeiling = math.MaxInt32 - 1` the upper arm **is**
> expressible on every `GOARCH` (`2147483647` compiles and vets clean on 386 and amd64 — tested), and `(n int)`
> becomes viable. It is rejected on the **trade** — it forfeits the "largest value representable everywhere"
> justification and shrinks the moved rows' magnitude from ~2⁶² to ~2³¹ — not on impossibility. **The practical
> instruction is unchanged: do not "simplify" the signature during implementation, and do not change the ceiling
> either.** The value and the signature are one decision, not two.
>
> **Corollary that bites this plan directly: `1 << 30` CANNOT be used for the three moved gate rows.**
> `1,073,741,824 < byteCapCeiling = 2,147,483,647`, so it would be **accepted** and every `require.ErrorIs` would
> fail. They keep `1 << 62`.

---

## Task 1 — the ceiling, its godoc, the class-gate arm move, **and the parent fold-back** — **ONE COMMIT**

> 🔴 **This task is revision 1's Tasks 1 + 2 + 3, merged (round-1 B-1 and m-14), PLUS revision 2's Task 2 Steps
> 1-4 (round-2 N-6).** They cannot be separate commits. The class gate is `package msgin_test` in the **same
> module** as the production change and asserts `require.NoError` on exactly what this task makes an error, so a
> commit that changes `NewConfig` without moving the gate is a **red root suite** — forbidden by Global
> constraint 8 and outside CLAUDE.md's per-task-commit pre-authorization.
>
> 🔴 **Round-2 N-6: the parent artifacts join too.** Revision 2 closed the six-false-*godoc*-sentences window and
> then opened an equivalent one by scheduling Spec 016 / ADR 0032 / HANDOVER in Task 2 — leaving **six artifact
> statements** false across the same gap (§2.1's census line, §2.1's three byte-cap rows, §3.8's deferral,
> §6 AC-5's arm table, D-AB's *"refuses to certify them safe"*, HANDOVER §7 item 6's open state). Those are
> **normative**: ADR 0034 D-AS's REVERSIBILITY line says moving a row is a **spec change**. It also breaches
> CLAUDE.md's *"couple plans and ADRs with the code that realizes them — one coherent commit."*
> **Production change, its godoc, its gate move, and the parents that record the arm — together, or not at all.**

**Files:** `adapter/http/options.go`, `adapter/http/helpers.go`, `adapter/http/errors.go`,
`adapter/http/options_test.go` (new cases; blackbox `package msghttp_test`),
**`adapter/http/exchange_test.go`** (round-1 B-2 — branch 20; revision 1 omitted this file entirely),
`sizing_option_class_gate_test.go` (repo root; blackbox `package msgin_test`),
**[`../specs/016-sizing-option-bounds.md`](../specs/016-sizing-option-bounds.md)**,
**[`../adrs/0032-sizing-option-bounds.md`](../adrs/0032-sizing-option-bounds.md)**,
**[`../HANDOVER.md`](../HANDOVER.md)** (round-2 N-6 — note the `../specs/` and `../adrs/` prefixes; a bare
`016-…md` from inside `docs/plans/` is the 404 Global constraint 11 is about).
**Module:** root.

> 🔴 **Rebase check before starting.** [Plan 030](030-post-029-maintenance.md) is **fully delivered** —
> `1a1c135` / `d2c69fe` / `7ab91cd`, per the plan-number box above — so the gate's `fixed`/`rejects` arms are
> already at `1<<30` and its `safe` arm at `math.MaxInt`, and **nothing in this task is blocked on it.**
> [Plan 031](031-group-member-bounds.md) (ADR 0033 **D-AL**) extends the same file by hand and **may or may not
> have landed**; that is the only live rebase question. Run `git log --format='%h %s' --grep='Plan: 031'`,
> `git log --oneline main..`, and Step 6's `grep` before editing anything.

- [ ] **Step 1 (SKILLS + READ).** Load `cc-skills-golang:golang-how-to` (→ `golang-safety`, `golang-security`,
      `golang-error-handling`, `golang-design-patterns`, `golang-documentation`) and the `table-test` override.
      With **`gopls`, not `grep`**, read `adapter/http/helpers.go`'s `checkRange`, the three `NewConfig` gates,
      the three sentinel declarations, and the two sibling godocs to copy the shape from —
      `WithMaxConnections` (`options.go:908`, citing Spec 016 **§1.3** at `:901`) and `WithReplayBuffer`
      (`:986`, citing Spec 016 **§1.5** at `:977`). *(Round-1 m-11: revision 1 said both cited §1.3, at `:976`.)*
      Confirm all of it still reads as "The three knobs" table records it.
- [ ] **Step 2 (TEST-SAFETY — the RIGHT check).** 🔴 **Do NOT use revision 1's message-string grep.** It asks
      *"does a test assert the wording?"* and is structurally blind to *"does a test depend on a value we stop
      accepting?"* — which is how `exchange_test.go:615` was missed (round-1 B-2). Run **both**:

      ```bash
      # (i) wording — expected: only the three declarations in errors.go
      grep -rn 'max body bytes must be\|max response bytes must be\|max event bytes must be' --include='*.go' .

      # (ii) ACCEPTANCE — the check that matters. Classify EVERY hit.
      grep -rn 'WithMaxBodyBytes(\|WithMaxResponseBytes(\|WithMaxEventBytes(' --include='*_test.go' .
      ```

      Classify each (ii) hit as **out-of-range (breaks)** / **in-range, rejected today** / **in-range,
      accepted** and paste the classification into the Evidence block.

      🔴 **RE-DERIVE the totals; do NOT compare against the pasted figures (round-2 N-5).** Revision 2 pasted
      *48 hits / 34 calls / 18 accepted*; the tree at `46803c6` says **49 / 40 / 24**, and
      [Spec 018 §3.1a](../specs/018-byte-cap-ceilings.md) now carries the corrected table with the ten
      `sse_test.go` sites enumerated instead of elided. Plan 030 has landed and Plan 031 may land before this
      task runs, so the counts are a snapshot in any case.

      **Only the RULE is normative:** the classification must come out **4 out-of-range** (the gate's three
      `1<<62` rows + `exchange_test.go:615`'s `math.MaxInt64`), with the rest unaffected, and the four classes
      must sum to the call count. **Any out-of-range hit not in this task's Files list is a stop-and-reassess.**
      A total that merely disagrees with this plan is **not** a stop-and-reassess — record the new figure.
- [ ] **Step 3 (RED — production).** Write the failing cases of the tables below. All must fail before any
      production edit.
- [ ] **Step 4 (GREEN — production).** Add, in this order:
      1. `const byteCapCeiling int64 = math.MaxInt32` in `adapter/http/options.go`, beside the three
         `defaultMax*Bytes` (import `math`).
      2. `func checkRangeInt64(sentinel error, site string, n, lo, hi int64) error` in
         `adapter/http/helpers.go`, beside `checkRange`, rendering the **identical** shape.
      3. The three `NewConfig` gates rewritten to
         `else if err := checkRangeInt64(<sentinel>, "msghttp.With<X>", cfg.<field>, 1, byteCapCeiling); err != nil { return nil, err }`.
      4. The three sentinel messages: `must be > 0` → `out of range` (D-AQ).
- [ ] **Step 5 (GODOC — same commit, not a follow-up).** Six sentences become false the instant Step 4 lands
      (round-1 m-14). Rewrite all six **now**:

      | # | Site | Today | Becomes |
      |---|---|---|---|
      | 1-3 | `options.go:458`, `:764`, `:851` | `n MUST be > 0: NewConfig returns ErrInvalidMax…` | `n MUST be in [1, 2147483647]` + the ceiling's value + **why** + the hazard disclosure |
      | 4-5 | `errors.go:14`, `:72` | *"returned by NewConfig when an explicit … is <= 0"* | *"…outside [1, 2147483647]"* |
      | 6 | `errors.go:132` | *"returned by NewConfig **(and so by NewSSEParser)** when an explicit WithMaxEventBytes is <= 0"* | *"returned by NewConfig **(and so by NewSSEParser)** when an explicit WithMaxEventBytes is outside [1, 2147483647]"* — 🔴 **keep the parenthesis (round-2 N-11).** It is the only place the sentinel's godoc names the constructor a caller actually sees it from (`NewSSEParser` builds a `Config` internally, `sse.go:239`), and revision 2's collapsed row had no home for it |

      Per [Spec 018 §4](../specs/018-byte-cap-ceilings.md), each option godoc states **the range**, **the
      ceiling's value and why** (`math.MaxInt32` — the read lands in a single `[]byte` whose length is an `int`,
      whose width is `GOARCH`-dependent, so this is the largest cap exactly representable **everywhere**), **the
      typed error**, and **the hazard disclosure** — *"the ceiling is not a safety guarantee; this option is the
      only bound on a read driven by a remote peer, and raising it above the 1 MiB default trades flood
      protection for payload size."* That last is [Spec 016 §3.8](../specs/016-sizing-option-bounds.md) item 2's
      **undelivered promise** (verify: `grep -c 'hazard disclosure' docs/plans/029-sizing-option-bounds.md` →
      `0`). Keep the existing `CAVEAT` paragraphs (headers / the O1 drain) and the *"leaving this option unset is
      how a caller asks for the default"* sentence — both still true, and the latter is the closest thing to an
      off-state the API has (**D-AN(b)**).

      Also godoc **`byteCapCeiling`** (shaped like `maxConnectionsCeiling`'s) carrying Spec 018 §3.2's corrected
      argument — **width safety / portability**, one constant for three knobs, and why it is not a payload guess;
      and **`checkRangeInt64`** (shaped like `checkRange`'s) naming the 32-bit truncation it prevents, stating it
      is a sibling not a generic (**D-AP(b)**), **and** why the option was not simply narrowed to `int`
      (**D-AP(a)**), with the ADR 0031 D-R cross-reference the neighbouring helpers carry.

      🔴 **A FOURTH godoc edit: amend the EXISTING `checkRange`'s godoc too (round-2 N-9).** Neither of its
      enumerations becomes literally false — `checkRange` keeps exactly three callers (`options.go:1219`,
      `:1226`, `:1238`), all R1 — but *"each of this package's **three sites**"* (`helpers.go:51`) and *"**All
      three sites** are R1"* (`:57`) both read as **package-wide** statements, and after this task the package
      has **six** range-checked sizing options across two helpers seven lines apart. Its closing paragraph
      enumerates the peer copies (`endpoint`, `routing`, `memory`) and a hypothetical fifth in
      `adapter/http/stdlib` while saying nothing about the sibling immediately below it. Scope the counts
      (*"the three `int`-typed sites this helper serves"*) and cross-reference `checkRangeInt64` — why it exists
      and when to reach for it — so the pair reads as a pair **from either end**. This is
      [Spec 018 §4](../specs/018-byte-cap-ceilings.md) item 4, and it is the class CLAUDE.md's stored lesson
      names: *docs can contradict the code they describe.*
- [ ] **Step 6 (GATE — DERIVE the site list, do not read one).** 🔴 Round-1 **B-3**: revision 1 listed 7 sites and
      every offset but one was stale. 🔴 Round-2 **N-2**: revision 2 fixed the *method* and got the *predicate*
      wrong — it selected on the word `deferred`, while the property being changed is the **`fixed` partition**
      (9 rows → 12, one literal → two). 🔴 Round-3 **NEW-2**: revision 3's *widened* form was **still a token
      enumeration** and missed three more — `:409` (`fixed` unquoted), `:601`, and `:799-800`, **which is inside a
      live `require.Equal` failure message.**

      🔴 **SAY THIS OUT LOUD BEFORE YOU RUN ANYTHING: FOUR consecutive audit rounds have each fixed the sites
      they were shown and been overtaken by new ones.** 7 sites → 12 → 14 → 16 → **17**. Round 4's addition
      (`:22`, R4-4) was **not** found by a selector and **could not have been** — it carries no arm name, no
      literal, no `9/1/3/6` and no digit, so even the deliberately-broad form below misses it. The remedy is
      **not** a better `grep`, and raising the count a sixth time will not work either. **The durable defect is structural:** the
      arm partition is restated in roughly **ten** prose locations — the header's arm list, its arithmetic
      identity, Plan 030's per-arm literal block, the `arm` field's doc comment, two section banners, the
      `wantArms` rationale comment's illustrative map, and **two live assertion messages** — with **no mechanical
      link to `wantArms`**, the map the test already computes from. Nothing fails when one drifts. The real fix is
      recorded as a backlog item at [Spec 018 §8 item 5](../specs/018-byte-cap-ceilings.md) (*derive the counts
      from the table at test time*); **this inventory is a stop-gap, and it should be described as one in the
      Evidence block.**

      **Select DELIBERATELY BROADLY and accept the noise.** Run against **current `HEAD`**, paste the output into
      Evidence:

      ```bash
      grep -nE 'deferred|DEFERRED|fixed|rejects|safe|1<<30|1<<20|1<<62|9/1/3/6|9 \+ 1 \+ 3 \+ 6|[0-9]+ (class|rows|AST)' \
        sizing_option_class_gate_test.go
      ```

      **104 of the file's 812 lines** at `a1247d1` — a ten-minute classification pass, and a strict superset of
      revision 3's 42-line form. **Keep the upper-case `DEFERRED` and the `9/1/3/6` alternatives:** they are the
      only ones reaching `:58`, `:565`, `:758` and `:805`, which a lower-case word list silently drops.
      **Under-inclusion ships.** 🔴 The narrow revision-2 form is **DROPPED, not retained as a sub-check**
      (round-3 **NEW-7**) — revision 3 quoted its expected 18-line output and never gave its command, and a check
      specified by its *result* rather than its *procedure* cannot be re-derived.

      **The over-inclusion account — classify these as "no change" and move on:** the `arm:` rows at `:412`-`:511`
      (nine `fixed`), `:531` (`rejects`) and `:607`-`:713` (six `safe`); the `wantArms` entries at `:772`-`:780`;
      the `astKeys` / `require.Len` assertions at `:746`-`:756`; and — 🔴 **round-3, smaller note 2** — two
      *individual* lines that fall outside every range earlier revisions named: **`:33`** (the `rejects` bullet's
      *"in neither `"fixed"` … nor `"safe"`"* clause — still true) and **`:521`** (the historical M2 note, *"this
      row **previously** sat in the arm labelled `"fixed"`"* — past tense, still true).

      Then edit **every** hit that changes, per [Spec 018 §6 AC-4.1](../specs/018-byte-cap-ceilings.md)'s
      **17-site** table. The offsets below are **from `1212c63`/`46803c6`/`a1247d1`/`6865886` and will drift** —
      the grep is the authority for sites 1-16, and **site 17 is listed because no grep produces it**:

      | # | Line(s) | Change |
      |---|---|---|
      | 1 | `:570`, `:579`, `:588` | `arm` field `"deferred"` → `"fixed"`; drop the trailing `// class member, remedy deferred` comment |
      | 2 | `:571-575`, `:580-584`, `:589-593` | `require.NoError` → `require.ErrorIs` on the knob's sentinel **+** `assert.EqualError` on the render **the row itself produces** — each row passes `1 << 62`, so the string is `msghttp: max body bytes out of range: msghttp.WithMaxBodyBytes: 4611686018427387904 not in [1, 2147483647]` and its twins — **+** `assert.False(t, msgin.IsPermanent(err), "R1 constructor error stays bare (ADR 0029 D-M)")` |
      | 3 | `:782-784` | `wantArms` entries → `"fixed"` |
      | 4 | `:803` | `byArm` — **remove** the `deferred` key |
      | 5 | `:35` | the header's arm-list bullet — 🔴 **round-4 R4-4: TOMBSTONE it, do not delete it.** Revisions 1-4 said only *"the header's arm-list bullet"*, which does not choose. Write `- "deferred" (0) — no members as of Plan 032; see Spec 018. The arm is retained so a future knob with a genuinely deferred remedy has it.` — consistent with **site 8**'s tombstone for the same vocabulary, and it is what keeps **site 17**'s *"FOUR arms"* true. **Deleting the bullet instead leaves `:22` asserting four arms four lines above a three-item list.** The `byArm` **counts map** loses the key either way (Trap 1) |
      | 6 | `:38` | `9 + 1 + 3 + 6 = 19` → `12 + 1 + 6 = 19` |
      | 7 | `:55-59` | **Plan 030's per-arm literal block** — goes false twice; see below |
      | 8 | `:401` | the `arm` field's vocabulary doc — **keep `"deferred"` with a tombstone**: *(no members as of Plan 032 — see Spec 018)* |
      | 9 | `:539-546` | the `---- arm: deferred ----` section banner |
      | 10 | `:547-556` | 🔴 block **one** — *"WHEN §3.8's CEILING LANDS…"* → a record |
      | 11 | `:557-568` | 🔴 block **two** — *"THESE THREE ROWS KEEP THE 1<<62 LITERAL"* → a record |
      | 12 | `:758`, `:761`, **`:799`, `:800`**, `:801`, `:805` | prose **inside live `require.Equal` messages** naming the `9/1/3/6` split. 🔴 **round-3 NEW-2 extends this site: `:799-801` is ONE message** — a three-line string concatenation — and revision 3 listed lines 3 and 5 while omitting lines 1 and 2, purely because those two carry no selector token. `:799-800` reads *"…not just the per-arm counts: **9 class** / **members fixed here**, 1 that rejects…"*: `9` → `12`. **Edit the MESSAGE, not the lines** |
      | **13** | **`:26`** | 🔴 **round-2 N-2** — the header's `fixed` bullet, `- "fixed"    (9) — …`. Invisible to the narrow grep. `9` → `12` |
      | **14** | **`:47-49`** | 🔴 **round-2 N-2 + round-3 NEW-1 — FOUR falsehoods, not two.** See Trap 3c below. Revision 3 quoted this bullet only to line 1 of 3 and scheduled only the count and the literal |
      | **15** | **`:409`** | 🔴 **round-3 NEW-2** — the `fixed` arm's section banner, `// ---- arm: fixed — the 9 class members this increment bounds ----`. `9` → `12`. Invisible even to revision 3's widened grep, because `fixed` here is **unquoted** and that selector required `"fixed"` |
      | **16** | **`:601`** | 🔴 **round-3 NEW-2** — the `safe` arm's literal rationale, `// math.MaxInt, NOT the 1<<30 the reject arms use (Plan 030 Task 2):`. After the move the reject arms use **TWO** literals (`1<<30` for the 9 `int`-typed rows, `1<<62` for the 3 `int64`-typed ones). Narrow to *"NOT the `1<<30`/`1<<62` the reject arms use"*, or *"NOT any reject-arm literal"*. 🔴 **Its substance — why `safe` may not be demoted to an int32 value — is CORRECT and must survive verbatim** |
      | **17** | **`:22`** | 🔴 **round-4 R4-4** — the CONFORMANCE preamble's arm **cardinality**: `//     declaration string — in one of FOUR arms. The arms are BEHAVIORAL and are`. Under site 5's **tombstone** this is **NO CHANGE** — four arms stay documented, one with zero members — and that is why the tombstone is chosen. **Listed because no selector can find it:** no arm name, no literal, no `9/1/3/6`, no digit. Verified — `grep -nE '<the broad form>' … \| cut -d: -f1 \| grep -cx 22` → **0**. **If you deviate from the tombstone, this line goes false and must be edited too.** It is the standing evidence for [Spec 018 §8 item 5](../specs/018-byte-cap-ceilings.md): a site four rounds of better selectors could not have produced |

      **Also classify, do not skip:** `:766` carries `map[string]int{"fixed": 9, ...}` inside the comment
      explaining why `wantArms` is a mapping rather than a count. Illustrative, not normative — it may stay, but
      it must be **decided**, not missed.

      > 🔴 **Trap 1 — site 4.** `byArm` is built by counting (`:793`, `:797`), so an emptied arm has **no key**.
      > `{"fixed": …, "deferred": 0, …}` **fails**. Remove the key; do not zero it.
      >
      > 🔴 **Trap 2 — site 2's string.** Revision 1 said *"the same string Task 1's AC-2 case asserts"*
      > (round-1 M-8). **It is not the same string.** AC-2 renders `2147483648`; a gate row passing `1 << 62`
      > renders `4611686018427387904`. Assert the render **each row actually produces**.
      >
      > 🔴 **Trap 3 — site 7, which revision 1 missed entirely.** Plan 030's header block declares the oversized
      > literal to be a function of the **arm**. After this move, `fixed` holds 12 rows carrying **two** literals
      > (9 at `1<<30`, 3 at `1<<62`), so the mapping is true of no arm; and its *"deferred (3) → still 1<<62"*
      > bullet loses its referent.
      >
      > 🔴 **Trap 3c — THE SAME BLOCK GOES FALSE TWO MORE TIMES, AT `:48-49`, FOUR LINES FROM SITE 14
      > (round-3 NEW-1).** The `fixed`/`rejects` bullet does **not** end where revision 3's quotation stopped:
      >
      > ```
      > 47: //   - "fixed" (9) and "rejects" (1) → 1<<30 = 1,073,741,824. These rows assert
      > 48: //     an EqualError against a rendered decimal. 1<<30 fits an int32 yet still
      > 49: //     exceeds every ceiling in the codebase (the largest is 1<<20 = 1,048,576),
      > ```
      >
      > | # | Clause | Goes false because | Scheduled before rev 4? |
      > |---|---|---|---|
      > | 1 | the count `(9)` | the arm becomes **12** | ✅ |
      > | 2 | `→ 1<<30` as the arm's single literal | 3 of the 12 sit at `1<<62` | ✅ |
      > | 3 | *"`1<<30` … exceeds **every ceiling in the codebase**"* | `1,073,741,824 < byteCapCeiling = 2,147,483,647` — after Step 4 there **is** a ceiling `1<<30` does not exceed | ❌ **NO** |
      > | 4 | *"(the largest is `1<<20` = 1,048,576)"* | `1<<20` stops being the largest ceiling the moment `byteCapCeiling` is declared | ❌ **NO** |
      >
      > **Write, don't just count:**
      >
      > - Narrow clause 3 to *"exceeds every **`int`-typed** ceiling in the codebase"*. **This is exactly the
      >   narrowing Trap 3b already uses** for dimension 2 — so leaving `:48-49` broad would put the true,
      >   narrowed sentence and the false, broad one **seven lines apart in the same header block, about the same
      >   literal.** *Docs can contradict the code they describe*, with the right wording already in the file.
      > - **State the reason inline:** *"`byteCapCeiling` is an `int64` ceiling above `1<<30`; that is why the
      >   three `int64`-typed rows keep `1<<62`."*
      > - Clause 4's parenthetical is an **enumeration**; the stored lesson is *assert the invariant, not the
      >   enumeration.* Name `byteCapCeiling` as the largest and `1<<20` as the largest `int`-typed one, or delete
      >   the parenthetical.
      >
      > 🔴 **Trap 3b — THE REPLACEMENT INVARIANT IS TWO-DIMENSIONAL, and revision 2's one-dimensional form was
      > FALSE (round-2 N-1, the BLOCKER).** Revision 2 said *"the literal follows the option's PARAMETER TYPE —
      > `int` → `1<<30`, `int64` → `1<<62`; both then select the out-of-range branch."* **False of six of the
      > file's nineteen rows.** The whole `safe` arm is `int`-typed — `resilience/breaker.go:51`,
      > `endpoint/flowcontrol.go:144`, `:166`, `resilience/ratelimit.go:42`,
      > `adapter/memory/queuestore.go:182`, `channel/queuechannel.go:50` — and **every one passes `math.MaxInt`
      > and asserts the value is ACCEPTED** (`:644`, `:669`, `:691`, `:704`, `:723`, and `WithPollMaxBatch`'s
      > row). There is no out-of-range branch for them. **Applying the one-dimensional rule literally demotes all
      > six to `1<<30` — an int32 value — which the block at `:61-77` forbids in terms:** *"1<<30 IS an int32
      > value, so demoting these rows to it would leave every assertion green while the int32-truncation probe
      > silently stopped running."* That is a **silently green, silently non-probing gate** — worse than a red
      > one, because nothing fails.
      >
      > **Write it in two dimensions, in this order:**
      >
      > 1. **The ARM fixes the required PROPERTY.** `safe` → the value must be **ACCEPTED** and stay maximally
      >    absurd ⇒ `math.MaxInt`, and nothing else. `fixed` / `rejects` → the value must be **OUT OF RANGE** and
      >    render an **architecture-independent decimal**.
      > 2. **Only WITHIN the reject arms does the PARAMETER TYPE choose the literal.** `int` → `1<<30` (fits
      >    int32, exceeds every `int`-typed ceiling, renders `1073741824` everywhere); `int64` → `1<<62`
      >    (in range on every architecture, renders `4611686018427387904`).
      >
      > **Carry `:61-77`'s "do not demote these rows" warning forward VERBATIM** — generalise it, never replace
      > it. **And `1 << 30` cannot be used for the three moved rows** — `1,073,741,824 < 2,147,483,647`, so it
      > would be **accepted**; that is dimension 2 doing its work, since those three are the `int64`-typed
      > members of a reject arm.
      >
      > 🔴 **Trap 4 — sites 10 and 11 are what a mechanical row-move misses.** Block one currently tells a future
      > contributor *"WHEN §3.8's CEILING LANDS, THIS GATE WILL GO RED, AND THAT IS CORRECT … the repair is to
      > MOVE the row into the `fixed` arm. Do NOT weaken the production check."* **This increment IS that event.**
      > Rewrite both into a record of what happened; **generalise** the warning about weakening the check, do not
      > delete it.

- [ ] **Step 7 (DELTAS, not literals).** 🔴 Round-1 **M-9**: [Plan 031](031-group-member-bounds.md) takes
      `sizingConformanceKeys` 17 → 19 and the partition to `11 fixed + 1 rejects + 3 deferred + 6 safe = 21`.
      Revision 1 declared `require.Len(t, tests, 19)`, `sizingConformanceKeys` unchanged, and
      `{"fixed": 12, "rejects": 1, "safe": 6}` as **normative literals** — all three of which Plan 031 falsifies.
      **Assert the deltas** ([Spec 018 §6 AC-4.2](../specs/018-byte-cap-ceilings.md)):

      | Quantity | Delta |
      |---|---|
      | `require.Len(t, tests, N)` | **+0** |
      | `sizingConformanceKeys` | **unchanged by 032** |
      | `byArm["deferred"]` | **key removed**. Verified against Plan 031 revision 2: it adds both its rows to **`fixed`**, never to `deferred`, so this holds under either order — but **re-derive** (`grep -n 'arm' docs/plans/031-group-member-bounds.md`), since 031 is undelivered and under revision |
      | `byArm["fixed"]` | **+3** from whatever it then is |
      | `byArm["rejects"]`, `byArm["safe"]` | **+0** |
      | half 1 (AST completeness walk) | **unchanged** — the three functions still exist, still `int64`; `:234` accepts `int` **or** `int64` regardless |

      **If Plan 031 has not landed**, the concrete values are `{"fixed": 12, "rejects": 1, "safe": 6}` and
      `require.Len(t, tests, 19)`. **If it has**, they are `{"fixed": 14, "rejects": 1, "safe": 6}` and
      `require.Len(t, tests, 21)`. Both orders converge on the same end state. Record which case applied.
- [ ] **Step 8 (BRANCH 20).** Rewrite `adapter/http/exchange_test.go:613-620` from `math.MaxInt64` to the ceiling
      value — 🔴 **spell it `math.MaxInt32`, NOT the decimal `2147483647` (round-2 N-10).** `math` is used
      **exactly once** in that file (`grep -n 'math\.' adapter/http/exchange_test.go` → only `:615`), so a bare
      decimal orphans the import and the package stops compiling: `"math" imported and not used`. The RED step
      would then fail for a reason unrelated to the assertion under test. This is a stated exception to Global
      constraint 2's *"the literal `2147483647` (or `math.MaxInt32`)"* latitude — here only the second spelling
      is viable, and it also reads as the same constant `byteCapCeiling` is defined from.

      Update the header comment at `:577-578` — today *"`WithMaxResponseBytes(MaxInt64)` returns a
      non-empty body intact, the overflow regression"* — to name the ceiling and record that `MaxInt64` is no
      longer reachable through the public API. **Do not delete the case.** Spec 018 §1.3 item 2 states what still
      covers INV-6's arithmetic (branches 18 and 19).
- [ ] **Step 9 (386).** `GOTOOLCHAIN=go1.25.13 GOARCH=386 GOOS=linux go vet ./...` → **exit 0**, and
      `… go build ./...` → **exit 0**. 🔴 **Do NOT use `go test -gcflags=all=-e -run=NONE ./...`** — it exits
      **1** on an untouched tree (`exec format error`, all 11 root packages), so it cannot detect a regression
      (round-1 M-4). `go vet` is the usable form because it **type-checks `_test.go` files**, which is where the
      32-bit exposure lives. Record both exit codes.
- [ ] **Step 9b (386 VACUITY PROBE — the gate has only ever passed).** 🔴 Round-2 **N-12**: AC-3 was the one gate
      in this bundle without a probe, and it *replaced* a command round 1 proved could not work. The claim under
      test is not *"vet exits 0"* but *"vet type-checks `_test.go` files, so a 32-bit-only overflow in a test
      literal goes red"* — **prove it, per [Spec 018 §6 AC-3b](../specs/018-byte-cap-ceilings.md)**:

      1. Plant a 32-bit-only overflow in **one root `_test.go`** — e.g. `const _ int = 1 << 40`, or a call
         passing `1 << 62` to an `int` parameter.
      2. `GOARCH=386 GOOS=linux go vet ./...` → **exactly one** failure, naming that file.
      3. `GOARCH=amd64 GOOS=linux go vet ./...` → **clean**. This is what makes it a *32-bit* probe and not a
         syntax probe; without step 3 the probe proves nothing about architecture.
      4. Revert. Both return to **exit 0**.

      Paste all four outputs into Evidence. *Proving a gate FIRES is not proving it COVERS — plant the probe
      where the coverage is doubtful.*
- [ ] **Step 10.** Mutation-prove every case (tables below). `GOWORK=off go test ./... -race -shuffle=on` green
      in root. Coverage on `adapter/http` ≥ 85% and every branch below covered.
- [ ] **Step 11 (FALSIFICATION SWEEP — the godoc half).** Not a mutant table; a sweep:

      | # | Claim to falsify | How |
      |---|---|---|
      | D-1 | *"no other godoc sentence became false"* | 🔴 **`grep -rin 'must be > 0' adapter/http/` — CASE-INSENSITIVE (round-2 N-8).** Every hit read **against the constructor**, not for plausibility; each updated or justified in Evidence |
      | D-2 | *"the runnable examples still compile and their `// Output:` still holds"* | `go test -run '^Example' ./adapter/http/...` |
      | D-3 | *"the disclosure is actually present"* | 🔴 **WRAP-TOLERANT count (round-3, smaller note 1)** — see below → **3 occurrences**, **vacuity-probed**: delete one occurrence, confirm the count reads 2, restore |

      > 🔴 **D-1's case, and why it was a defect (round-2 N-8).** The godoc form is **upper-case** `MUST`, so
      > revision 2's case-sensitive grep returned **six** hits — `errors.go:19`, `:77`, `:138`, `:175`, `:181`,
      > `:207` — and **not one of them was a godoc sentence**; all six are `errors.New` **message strings**. All
      > **seven** `MUST be > 0` godoc sentences (`options.go:458`, `:764`, `:851`, `:1004`, `:1031`, `:1112`,
      > `:1138`) were invisible to it, including the three this task rewrites and that D-1 exists to prove were
      > rewritten. Worse, the sweep gets **cleaner** as the task proceeds: Step 4 item 4 renames the three
      > byte-cap sentinels to `out of range`, so afterwards the case-sensitive form returns three unrelated
      > survivors (`ErrInvalidHeartbeat`, `ErrInvalidWriteTimeout`, `ErrInvalidReadTimeout`) and reads as
      > discharged while testing nothing.
      >
      > **Expected with `-i`: 13 hits before, 10 after** (the three sentinel *messages* change; the seven godoc
      > sentences and three unrelated sentinels remain, each to be read against the constructor).
      > **Vacuity-probe it:** plant one *lower-case* `must be > 0` in a godoc, confirm the `-i` form finds it and
      > the case-sensitive form does not, revert.
      >
      > 🔴 **D-3's command has the SAME class of defect one row down, and revision 3 did not fix it (round-3,
      > smaller note 1).** `grep -c 'not a safety guarantee'` fails **twice**: (i) `grep -c` counts matching
      > **LINES, not occurrences**, so two hits on one line count as 1; (ii) the phrase is **six words** and the
      > godoc wraps at ~80 columns, so an occurrence split across a `// ` continuation is invisible. Plan 030
      > Task 1 hit exactly this and adopted a whole-file `perl -0777` slurp — the established house form. Use it:
      >
      > ```bash
      > perl -0777 -ne 's{\n\s*//\s*}{ }g; my $c = () = /not a safety guarantee/g; print "$c\n"' \
      >   adapter/http/options.go            # → 3 after Step 5; → 0 before it
      > ```
      >
      > **Demonstrated, on a two-occurrence probe with one wrapped and one not:** the naive form reports **1**,
      > the wrap-tolerant form reports **2**. Under the naive form the vacuity probe would itself have been
      > satisfiable by an occurrence that was never counted in the first place.

- [ ] **Step 11b (PARENT FOLD-BACK — in THIS commit, not the next one).** 🔴 Round-2 **N-6**; ADR 0034
      **D-AT(b)**; [Spec 018 §6 AC-5](../specs/018-byte-cap-ceilings.md). Spec 016 revision 6's BLOCKER-2 was a
      reclassification that reached one file and missed seven, because the cross-file grep guard *"was written
      and not run."* **Run it, and paste the output:**

      ```bash
      grep -rn 'deferred' docs/specs/016-sizing-option-bounds.md docs/adrs/0032-sizing-option-bounds.md \
        docs/plans/029-sizing-option-bounds.md docs/HANDOVER.md
      grep -rn 'WithMaxBodyBytes\|WithMaxEventBytes\|WithMaxResponseBytes' docs/ CLAUDE.md
      ```

      **Enumerate every hit before editing any of them**, classifying each as *(a) must change*, *(b) immutable
      audit record — leave*, or *(c) historical prose, still true*. Then amend the **(a)** sites:

      - **Spec 016** §2.1's census line (`9 fixed + 3 deferred + 4 safe`) and arm table, the three verdict rows,
        §3.8 (now *"deferred → delivered by Spec 018"*), §6 AC-5's arm table and its "accepts, deferred" row.
      - **ADR 0032 D-AB**'s deferral paragraph and the status header's census line.
      - **`docs/HANDOVER.md`** §7 item 6 → **CLOSED**, citing this bundle — **and, in the SAME edit, ADD a new
        §7 row** (🔴 round-4 **R4-10**):

        > *"Derive the class gate's prose counts from `wantArms` at test time"* —
        > `sizing_option_class_gate_test.go` restates the arm partition in ~10 prose locations with no
        > mechanical link to the map the test computes; four audit rounds have each patched the instances
        > (7 → 12 → 14 → 16 → 17 sites). Designed at
        > [Spec 018 §8 item 5](../specs/018-byte-cap-ceilings.md); **unscheduled**.

        **Why this is not optional.** §7 is the project's **discoverable backlog** — it is where Spec 018's own
        origin (item 6) lived. Spec 018 §8 is a section of a 1,200-line design document for an increment about
        to be marked delivered. Revision 4 closed §8 item 5 with *"so a fourth round does not find a seventeenth
        site"*; **round 4 found it** (`:22`, R4-4), and the next thing to touch this file is Plan 031's
        hand-edit, which would meet the same duplication with no backlog entry pointing at the fix. The file is
        being edited in this commit either way. **Closing item 6 without opening this row is an incomplete
        fold-back.**
      - A **Superseded/finished-by** pointer from Spec 016 §3.8 and ADR 0032 D-AB to Spec 018 / ADR 0034.

      > 🔴 **PLAN 032 OWNS SPEC 016 §2.1 UNCONDITIONALLY, AND RE-DERIVES IT FROM THE TREE (round-2 N-4).**
      > Revision 2 said the table is *"written by whichever of Plan 031 / Plan 032 lands SECOND"* and told this
      > task to *"amend only the rows this increment owns"*. **That protocol has one signatory:**
      > `grep -n 'Spec 016 §2.1' docs/plans/031-group-member-bounds.md` → **no hits**;
      > `grep -c '016-sizing' docs/adrs/0033-group-member-bounds.md` → **0**. Plan 031 has ten tasks and none is a
      > Spec 016 fold-back, so under one order §2.1 is left permanently wrong and under the other Plan 032 must
      > transcribe Plan 031's numbers out of a plan — the hand-typed-total failure two rounds have already caught
      > on this file.
      >
      > **Write the whole table, in every landing order, by READING THE TREE** — take the partition from
      > `sizing_option_class_gate_test.go`'s `wantArms` mapping and `byArm` assertion **as they stand after Step
      > 6/7**, never from Spec 018's or Plan 031's figures. Step 6 has already brought the gate to its final
      > state, which is why this fold-back belongs in **this** task and not a later one. **Both prior
      > instructions — "whichever lands second" and "amend only your own rows" — are DELETED.**

      🔴 **Immutable vs. editable — decide per file, and the rule is the FILE KIND, not the number.**

      | File | Kind | May this task edit it? |
      |---|---|---|
      | `docs/plans/029-audit-round-*.md`, `docs/plans/030-audit-round-1.md`, [`032-audit-round-1.md`](032-audit-round-1.md), [`032-audit-round-2.md`](032-audit-round-2.md), [`032-audit-round-3.md`](032-audit-round-3.md), [`032-audit-round-4.md`](032-audit-round-4.md), any `*-derivation-findings.md` | **immutable execution record** — correctly records what was true when written (`docs/HANDOVER.md` §8) | ❌ **NO** |
      | [`029-sizing-option-bounds.md`](029-sizing-option-bounds.md) | **delivered plan** | ✅ yes, in place, **if** it states the deferral as a standing fact. Prefer a dated one-line note over a rewrite |
      | 🔴 **[`030-post-029-maintenance.md`](030-post-029-maintenance.md)** | **delivered plan** — round 3's NEW-3 left this undecided, and revision 3 did not mention the file at all | ✅ **YES, editable in place** — the **Plan 020 precedent**, and the same latitude Plan 029 gets one row up. **Only `030-audit-round-*.md` is immutable; the plan itself is not.** Its delivery banner (added at `7d671b4`) is an exercise of exactly this latitude. Prefer a dated one-line note over a rewrite |
      | [`031-group-member-bounds.md`](031-group-member-bounds.md), [`../adrs/0033-group-member-bounds.md`](../adrs/0033-group-member-bounds.md) | **undelivered sibling, under concurrent revision** | ❌ **NO** — do not edit another increment's live design; N-4's whole point is that this task must not depend on, or write into, Plan 031 |
- [ ] **Step 12.** Commit: `fix(http): bound the three byte caps at the representability ceiling`. The commit
      carries the production change, its godoc, the class-gate arm move **and** the Spec 016 / ADR 0032 /
      HANDOVER fold-back, per CLAUDE.md's couple-plans-with-code rule.

**Hot-path branches introduced, and the case that covers each** (CLAUDE.md's test-coverage gate — a branch with
no test is a delivery blocker):

| # | Branch | Covering case | Killing mutant |
|---|---|---|---|
| B1-1 | `checkRangeInt64`: `n >= lo && n <= hi` → `nil` | `NewConfig_accepts_the_ceiling` (all three, `2147483647`) | make it always return an error ⇒ all three fail |
| B1-2 | `checkRangeInt64`: `n < lo` → error | `NewConfig_rejects_zero` (all three) | change `lo` to `0` ⇒ all three fail |
| B1-3 | `checkRangeInt64`: `n > hi` → error | `NewConfig_rejects_ceiling_plus_one` (all three, `2147483648`) | change `hi` to `math.MaxInt64` ⇒ all three fail |
| B1-4 | body gate, `!set` → default | 🔴 **round-3 NEW-5 — the BOUNDARY PAIR, not a 2 MiB body.** `NewConfig_default_body_cap_is_1MiB`, two cases in one `table-test`: unset ⇒ a **`1<<20`-byte** body is **accepted** by `DecodeRequest`, and a **`1<<20 + 1`-byte** body is **rejected**. Largest fixture in the increment (`1 MiB + 1`), which is what sets Global constraint 6's bound | **three** kills, not one: delete the default assignment (cap reads `0`) ⇒ the accept arm fails; `default = 1<<20 - 1` ⇒ the accept arm fails; `default = 1<<20 + 1` ⇒ the reject arm fails |
| B1-5 | body gate, set + in range | `NewConfig_accepts_the_ceiling/body` **and** `NewConfig_accepts_one` | see B1-1 |
| B1-6 | body gate, set + out of range | `…/rejects_ceiling_plus_one/body`, `…/rejects_zero/body`, `…/rejects_1<<62/body` | delete the `else if` arm ⇒ all three fail |
| B1-7 | response gate, all three arms | the `…/response` twins of B1-4/5/6 | as above, per arm |
| B1-8 | event gate, all three arms | the `…/event` twins of B1-4/5/6 | as above, per arm |
| B1-9 | **the `1<<62` case specifically** | `NewConfig_rejects_the_gate_value` (all three) — the exact literal the class gate uses | keep the `<= 0` check and drop the upper arm ⇒ fails. **This is the case Step 6's gate rows depend on** |
| B1-10 | **the classification** (round-1 M-8) | `assert.False(t, msgin.IsPermanent(err), …)` on every rejecting case, and on the three moved gate rows | **TWO ARMS — 🔴 round-4 R4-3.** **(coarse)** wrap the return in `msgin.Permanent(...)`, keeping every assertion ⇒ every rejecting case fails, including the three moved gate rows. **(targeted — this is the one constraint 7 requires)** on **one** rejecting case, wrap the return **AND** update that case's `assert.EqualError` expectation to the `"msgin: permanent: …"` render ⇒ **only `assert.False(t, msgin.IsPermanent(err), …)` fails.** **Without this, D-AQ's non-`Permanent` claim has no covering test at all** |
| **B1-11** | **the RENAMED sentinel text** (D-AQ; 🔴 round-4 R4-9) | the six `assert.EqualError` assertions of Spec 018 §6 AC-2 — two arms × three knobs — which are the only thing asserting Step 4 item 4's rename | revert `errors.go:19` to `errors.New("msghttp: max body bytes must be > 0")` ⇒ **both** `WithMaxBodyBytes` `EqualError` assertions fail while **every `ErrorIs` assertion stays green** — which is what proves the pair is carrying the rename. **Optional second arm:** change one call site's `site` literal (`"msghttp.WithMaxBodyBytes"` → `"msghttp.WithMaxResponseBytes"`) ⇒ that knob's two `EqualError` assertions fail and nothing else does. Both are one-token edits to a string literal |

> **B1-4/B1-7/B1-8's default arms are pre-existing branches**, not new ones. They are listed because the
> `else if` rewrite sits inside the same `if/else if` and a botched edit can swallow the `!set` arm — a class of
> failure the existing suite would catch only for whichever knob happens to be covered. Cover all three.
>
> 🔴 **B1-4's pair has a shipped model in this very package — copy it, do not invent one.**
> `adapter/http/exchange_test.go:309-334` already runs exactly this shape for the **response** cap:
> `const defaultCap = 1 << 20` at `:309`, then *"a reply of exactly the 1 MiB default succeeds intact"*
> (`strings.Repeat("A", defaultCap)`, `assert.Len(t, payload, defaultCap)`) and *"one byte over the 1 MiB default
> -> ErrReplyTooLarge"* (`strings.Repeat("A", defaultCap+1)`). Mirror its structure and its naming.
>
> 🔴 **Spec 018 revision 3 claimed the small-`n` proof "already exists and is re-asserted, not invented", naming
> `WithMaxBodyBytes(1<<20)` + a 64 MiB body → `http: request body too large`. IT DOES NOT EXIST** —
> `grep -rn 'body too large' --include='*.go' .` returns **zero hits** workspace-wide. That line is a Plan 029
> **benchmark measurement** transcribed into Spec 018 §1, not a shipped assertion; and a 64 MiB fixture would
> breach Global constraint 6 by **32×**. Corrected in Spec §6 AC-1. **Write the pair; do not go looking for an
> existing body-cap test to re-assert.**

**Mutant that CANNOT be behaviorally killed — record, do not fake** (Global constraint 7, ADR 0034 D-AR(b)):

| # | Mutant | Why it survives | What catches it |
|---|---|---|---|
| M1-7 | replace `checkRangeInt64(s, site, n, 1, byteCapCeiling)` with `checkRange(s, site, int(n), 1, int(byteCapCeiling))` | On `darwin/arm64` `int` is 64-bit, so `int(n)` is lossless and **every case still passes**. On `GOARCH=386` it truncates and **accepts `1<<62`** — but nothing executes on 386 here. | Step 9's vet/build arm (the mutant still vets, so this is **not** a kill), `checkRangeInt64`'s godoc naming the truncation, and code review. **Accepted gap.** Narrowing to `(n int)` would dissolve this mutant entirely and was rejected for a different reason — ADR 0034 **D-AP(a)**; do not "fix" it that way. |

**Also asserted (Spec 018 §6 AC-2 — the render is true at BOTH ends).** Per knob, two `EqualError` assertions:

```
msghttp: max body bytes out of range: msghttp.WithMaxBodyBytes: 0 not in [1, 2147483647]
msghttp: max body bytes out of range: msghttp.WithMaxBodyBytes: 2147483648 not in [1, 2147483647]
```

and the `WithMaxResponseBytes` / `WithMaxEventBytes` twins. **The lower-end assertion is mandatory** — it is what
would catch a `lo` regression and what makes D-AQ's message change enforced rather than promised.

**"Its product is usable"** (Spec 016 §6's definition for a `NewConfig`-only key): each accepting case asserts
`NewConfig` returns a non-nil `*Config` **and a nil error**, and that **the product it returns works** — for
`WithMaxBodyBytes` via `DecodeRequest` on a small body, for `WithMaxResponseBytes` via an `httptest` round-trip,
for `WithMaxEventBytes` via `NewSSEParser` + `Next` on a small event.

> 🔴 **"Usable" — NOT "the knob's effect is observable" (round-3 NEW-6).** Spec 018 revision 3's AC-1 said
> *"observable"*, and **that is unachievable for a ceiling-valued cap under Global constraint 6.** The ceiling is
> `2,147,483,647`; the constraint caps every fixture at `1 MiB + 1`. So all three checks above run a *small*
> fixture against a *ceiling-sized* cap and succeed **identically** under `WithX(byteCapCeiling)`, under the
> 1 MiB default, and **with the option dropped entirely** — three identical outcomes, which is the definition of
> an unobservable setting. **The ceiling's effect is unobservable BY CONSTRUCTION, and that is accepted, not a
> gap:** no legal fixture distinguishes a 2 GiB cap from the 1 MiB default, because anything above ~1 MiB is
> forbidden and anything below it passes under both. **The ceiling is proven at the CONSTRUCTOR only** —
> accepted at `byteCapCeiling`, rejected at `byteCapCeiling + 1` — and *"the cap caps"* is proven **separately,
> at small `n`** (B1-4's boundary pair), where it is a fact about the comparison. Neither half alone is the
> contract; together they are. Spec §6 AC-1 now uses this plan's wording, not the other way round.

🔴 **Do not assert an accessor** — `maxBody()`
is unexported (`options.go:272`) and `maxResponseBytes`/`maxEventBytes` have **no accessor at all**. Spec 018 §6
AC-1's accessor clause was deleted in revision 2 for exactly this reason (round-1 M-10).

**The gate's own mutants** (the moved rows are test data; these target the gate's assertions):

| # | Mutant | Must fail |
|---|---|---|
| M3-1 | relabel one moved row back to `"deferred"` | `wantArms` diff **and** `byArm` (two independent failures — that redundancy is the point) |
| M3-2 | **pairwise swap**: relabel `msghttp.WithMaxBodyBytes` → `"safe"` and `endpoint.WithPollMaxBatch` → `"fixed"` | `wantArms` diff **only** — `byArm` stays green. **This is exactly why the key→arm map exists** (the gate's own Task 8 review finding M-1); prove it still holds after the edit |
| M3-3 | leave site 4 as `"deferred": 0` | the `byArm` assertion — proves the trap is real, not folklore |
| M3-4 | revert Step 4's upper arm for `WithMaxBodyBytes` only | that row's `require.ErrorIs` — proves the moved assertion is not vacuous |
| M3-5 | delete one moved row entirely | `require.Len(t, tests, N)` **and** the `astKeys` completeness diff |

> 🔴 **M3-6 is DELETED (round-3 NEW-4).** Revision 3 carried it as *"drop `assert.False(t,
> msgin.IsPermanent(err), …)` from one moved row **and** wrap that sentinel in `msgin.Permanent` ⇒ must fail:
> nothing — which is the point."* **That mutant applies two edits at once — it removes the assertion AND changes
> the behaviour the assertion checks — so it can only ever pass, and discriminates nothing.** With the assertion
> deleted, no case observes `IsPermanent`, so the second edit is inert and the "experiment" reduces to *delete an
> assertion, observe that its subject is no longer asserted*. It also collides with **Global constraint 7**
> (*"a case that survives its own mutant is rewritten"*), which read literally would condemn the row it was meant
> to defend.
>
> **The discriminating experiment already exists, one table up: B1-10** — **keep** the assertion, wrap the return
> in `msgin.Permanent(...)`, and every rejecting case **including the three moved gate rows** goes red. That is a
> killed mutant, and it is the evidence. B1-10's scope clause now says so explicitly. *A mutant specified to
> survive is not proof of anything.*
>
> 🔴 **But the BARE wrap is not ATTRIBUTABLE, and revision 4 claimed it was the ONLY mutant proving that
> assertion load-bearing (round-4 R4-3).** `permanentError.Error()` **prefixes** the message —
> `reliability.go:13`, `return "msgin: permanent: " + e.err.Error()` — so wrapping the `checkRangeInt64` return
> also reds **every `assert.EqualError`** in the increment: the six AC-2 render assertions and the three moved
> rows' `EqualError` on the `1<<62` render. **Delete the `assert.False(t, msgin.IsPermanent(err), …)` line from
> every case and the wrap still reds them** — via `EqualError`. Global constraint 7 asks for a mutant targeting
> **that** assertion, so the bare wrap does not discharge it on its own. **B1-10 therefore has two arms**, and
> the *targeted* one is the evidence: wrap **and** correct that case's `EqualError` expectation to the
> `"msgin: permanent: …"` render, leaving `assert.False` as the only failure. Record **both** arms' outcomes in
> the Evidence block. *This narrows B1-10; it does not reinstate M3-6, which could not fail at all.*

**Evidence block to record:** Step 2's two greps with every hit classified **and the re-derived totals**; the RED
failures (count and names); Step 6's **broad** `grep` output verbatim plus which of the **17** sites each hit
maps to, the over-inclusion classifications, and the disposition of `:766`; Step 7's landing-order determination;
the killed mutants (B1-1 … B1-11 — **B1-10 in BOTH arms, the coarse and the targeted (R4-3)** — and M3-1 … M3-5; **M3-6 is deleted**); M1-7's stated non-kill; Step 9's two exit codes **and Step 9b's four probe outputs**; Step 11's
three sweep outputs **and D-1's / D-3's vacuity probes**; Step 11b's two cross-file greps with every hit
classified (a)/(b)/(c), **plus the `wantArms`/`byArm` values the Spec 016 §2.1 table was re-derived from**;
`go test -cover` for `adapter/http` before and after; the post-edit `byArm` and `wantArms` values pasted verbatim.

---

## Task 2 — the gates, and the status flip

> 🔴 **Revision 2's Task 2 also carried the parent fold-back; round-2 N-6 moved that into Task 1.** What is left
> here is **verification and the PROPOSED → ACCEPTED flip** — nothing that could make a normative artifact
> disagree with the tree between commits.

**Files:** this plan; [Spec 018](../specs/018-byte-cap-ceilings.md); [ADR 0034](../adrs/0034-byte-cap-ceilings.md)
(status headers and any *"as delivered"* addenda only).
**Module:** none (docs) — but every gate below runs against the **code** Task 1 landed.

- [ ] **Step 1 (RE-RUN THE FOLD-BACK GUARD).** Task 1 Step 11b ran the cross-file grep and amended the **(a)**
      sites. **Run it again here and confirm the (a) class is now EMPTY** — a guard that is only ever run before
      the edit proves the edit was *needed*, not that it was *complete*:

      ```bash
      grep -rn 'deferred' docs/specs/016-sizing-option-bounds.md docs/adrs/0032-sizing-option-bounds.md \
        docs/plans/029-sizing-option-bounds.md docs/HANDOVER.md
      grep -rn 'WithMaxBodyBytes\|WithMaxEventBytes\|WithMaxResponseBytes' docs/ CLAUDE.md
      ```

      Every surviving hit must classify as *(b) immutable audit record* or *(c) historical prose, still true*.
      **Any (a)-class survivor is a blocker** — it is the "stopped ONE FILE SHORT" failure, caught one commit
      late rather than not at all.
- [ ] **Step 2.** Re-confirm the immutable/editable split — **Task 1 Step 11b's table is the authority**, and it
      now covers [`032-audit-round-3.md`](032-audit-round-3.md) and [`032-audit-round-4.md`](032-audit-round-4.md) (both immutable) and
      [`030-post-029-maintenance.md`](030-post-029-maintenance.md) (a **delivered plan**, editable in place —
      round-3 NEW-3's open disposition). Confirm no `*-audit-round-*.md` or `*-derivation-findings.md` appears in
      `git diff --name-only main..HEAD`.
- [ ] **Step 3.** Flip Spec 018 / ADR 0034 status headers from **PROPOSED** to **ACCEPTED** with the date, and add
      the *"as delivered"* addenda for anything the implementation taught (the amend-don't-pile-on rule: fold
      these into this task's commit, do not add a follow-up `docs:`).
- [ ] **Step 4 (LINK GATE).** Run **both arms** of CLAUDE.md's docs-link gate over every tracked `.md`.
      🔴 **Round-4 R4-8 — `git add -N` is a GUARD, and its no-op result is the EXPECTED state.** `git ls-files`
      is blind to **untracked** files (Plan 030 round-1 MINOR 11), so `git add -N` **any bundle artifact not yet
      tracked** before running the gate; on a clean tree this does nothing, and that is what it should do. It
      exists so an uncommitted new artifact cannot slip past `git ls-files`, which is exactly what would happen
      the first time a round-N record is written and not yet committed. **Do not skip it because it looks
      inert.** Known false positives on this tree are exactly two — `docs/plans/m` and
      `docs/specs/factory(fireTime`, both Go identifiers in wrapped code spans. **Anything else is a blocker.**
- [ ] **Step 5 (VACUITY PROBE).** Prove the gate is not vacuous **on the new files, not on root** (the Plan 028
      `apidiff` blindness came from probing only root): plant one bad relative link and one bad `#anchor` in
      `docs/specs/018-byte-cap-ceilings.md`, re-run both arms, confirm **exactly one new hit each**, revert, and
      confirm both vanish.

      🔴 **The files under gate are DESCRIBED, not counted (round-4 R4-8):** **every `docs/specs/018-*`,
      `docs/adrs/0034-*` and `docs/plans/032-*` file, whatever their number.** Revisions 1-4 said *"the **six**
      new files"* and named them; the count had already gone stale once (round 3's record made it six) and went
      stale again the moment [`032-audit-round-4.md`](032-audit-round-4.md) landed, making it **seven**. Where a
      figure is genuinely wanted, derive it and paste the output:

      ```bash
      ls docs/specs/018-* docs/adrs/0034-* docs/plans/032-*        # the set under gate
      ls docs/specs/018-* docs/adrs/0034-* docs/plans/032-* | wc -l
      ```

      This is the same *assert the invariant, not the enumeration* rule Step 6 applies to the gate file, applied
      to the bundle's own file list. Spec 018 §6 AC-7 carries the same wording.
- [ ] **Step 6 (WHOLE-BRANCH GATE).** `/code-review` and `/security-review` over `main..HEAD` — **not** the last
      commit. Resolve or explicitly triage every finding with a written rationale. Then the full **Library quality
      gates**, all eight steps, over all eight modules:

      ```bash
      for d in . expr adapter/database/sql/{harness,postgres,mysql,sqlite,dbtest} adapter/cron/crontest; do
        (cd "$d" && GOWORK=off go test ./... -race -shuffle=on) || echo "FAILED: $d"
      done
      ```

      plus, per touched module (root): `go vet`, `gofmt -l .`, `CGO_ENABLED=0 go build ./...`,
      `go mod tidy` + `git diff --exit-code -- go.mod go.sum`, `govulncheck ./...`, `golangci-lint run ./...`.
      `harness` has no test files — check it with `go vet` instead. `govulncheck` lives in
      `$(go env GOPATH)/bin`, not on `PATH`.
- [ ] **Step 7 (SURFACE).** Prove Global constraint 5: `apidiff` **0/0**, and the AST exported-symbol set diff
      empty, **probed in `adapter/http`** — plant an exported symbol there, confirm the diff reports it, revert.
- [ ] **Step 8 (386, again).** Re-run `GOARCH=386 GOOS=linux go vet ./...` and `… go build ./...` — both exit 0.
      This task cannot break them, which is exactly why running it here proves the gate survived Task 1.
- [ ] **Step 9.** Commit: `docs: accept the byte-cap ceiling design and record the delivery gates`.

**Hot-path branches introduced:** none. **Verification is the gate output**, pasted verbatim.

**Evidence block to record:** Step 1's re-run grep output showing the **(a) class is empty**; both link-gate arms
before and after the probe; the eight-module loop; `apidiff` 0/0 and the AST set diff with its probe; the 386
re-run's two exit codes; the `/code-review` and `/security-review` findings with their resolutions.

---

## Delivery checklist

- [ ] **Both** tasks committed, each a green unit, each carrying `Spec: 018` / `Plan: 032` / `ADR: 0034` trailers.
      🔴 **Two, not four** — revision 1's Tasks 1-3 are one commit (round-1 B-1), **and the parent artifacts ride
      in that same commit** (round-2 N-6). **At no point between commits is the root suite red, and at no point
      does a normative artifact assert an arm the tree does not have.**
- [ ] `sizing_option_class_gate_test.go` has **no `deferred` row**, `wantArms` maps all three byte caps to
      `"fixed"`, and `byArm` has **no `deferred` key** (Plan 031 adds only to `fixed`, so this holds under either
      landing order — Task 1 Step 7).
- [ ] The gate's header block states the arm→literal rule **two-dimensionally** — arm fixes the property, then
      parameter type chooses the literal **within the reject arms only** — and `:61-77`'s "do not demote the
      `safe` rows to `1<<30`" warning survives verbatim (round-2 N-1).
- [ ] All **17** derived gate sites edited or explicitly classified — including `:26` and `:47-49` (round-2 N-2,
      round-3 NEW-1) and `:409`, `:601`, `:799-800` (round-3 NEW-2) — plus the over-inclusion account for `:33`
      and `:521`, and a decision recorded for `:766`.
- [ ] `:48-49`'s *"exceeds every ceiling in the codebase (the largest is `1<<20`)"* is **narrowed to
      `int`-typed**, with `byteCapCeiling`'s `int64` width stated inline (round-3 NEW-1) — the file agrees with
      its own dimension-2 wording.
- [ ] The gate's own `require.Equal` failure message at `:799-801` reports the **post-move** partition, not
      *"9 class members fixed here"* over a table of twelve (round-3 NEW-2).
- [ ] **B1-4 is the boundary pair** — `1<<20` accepted, `1<<20 + 1` rejected — mirroring
      `adapter/http/exchange_test.go:309-334`, and it is the largest fixture in the increment (round-3 NEW-5).
- [ ] **M3-6 is absent**; B1-10 is the mutant proving the `IsPermanent` assertion load-bearing (round-3 NEW-4),
      **and its TARGETED arm was run** — wrap plus the corrected `EqualError` expectation, so `assert.False` is
      the only failure (round-4 R4-3). The bare wrap alone is a kill, but not an attributable one.
- [ ] **B1-11 was run** — reverting `errors.go:19`'s message reds both `WithMaxBodyBytes` `EqualError`
      assertions while every `ErrorIs` assertion stays green (round-4 R4-9). D-AQ's rename is the increment's
      stated behavioral change and this is the only mutant that targets it.
- [ ] **Site 5 is a TOMBSTONE, not a deletion**, and site 17 (`:22`, *"in one of FOUR arms"*) is therefore
      **no change** — the file documents four arms, one empty, and agrees with itself (round-4 R4-4).
- [ ] `docs/HANDOVER.md` §7 carries a **NEW row** for *"derive the class gate's prose counts from `wantArms` at
      test time"*, opened in the same edit that closed item 6 (round-4 R4-10).
- [ ] Step 11 D-3 uses the **wrap-tolerant** `perl -0777` count, not `grep -c` (round-3, smaller note 1).
- [ ] The three moved rows each carry `require.ErrorIs` + `assert.EqualError` **on the render they themselves
      produce** (`…: 4611686018427387904 not in [1, 2147483647]`) + `assert.False(t, msgin.IsPermanent(err), …)`.
- [ ] `adapter/http/exchange_test.go` branch 20 and its header comment reflect the ceiling, spelled
      **`math.MaxInt32`** so the `math` import stays live (round-2 N-10), not `MaxInt64`.
- [ ] `GOARCH=386 GOOS=linux go vet ./...` and `go build ./...` both exit **0** (not `go test -run=NONE`),
      **and the gate is vacuity-probed** — one planted 32-bit-only overflow, exactly one 386 vet failure, an
      amd64-clean run, reverted (round-2 N-12).
- [ ] Spec 016 §2.1's arm table was **written in full by this increment, re-derived from the tree**, under
      whichever landing order applied (round-2 N-4); Spec 016, ADR 0032 and `docs/HANDOVER.md` §7 item 6 all
      record the class as **closed**, with no site left asserting a deferral (Task 2 Step 1's re-run grep is
      empty of (a)-class hits).
- [ ] `checkRange`'s **existing** godoc is scoped and cross-references `checkRangeInt64` (round-2 N-9), and
      `errors.go:132` keeps its `(and so by NewSSEParser)` clause (round-2 N-11).
- [ ] `apidiff` **0 removals / 0 additions**; no new exported symbol in any module.
- [ ] Both link-gate arms clean but for the two known false positives, run over **every `docs/specs/018-*`,
      `docs/adrs/0034-*` and `docs/plans/032-*` file, whatever their number** (`git add -N` any not yet tracked —
      a no-op on a clean tree, and that is the expected result), and **vacuity-probed on the new files**
      (round-4 R4-8).
- [ ] Eight-module loop green; the six non-test quality-gate steps run per touched module.
- [ ] Every finding from `/code-review` and `/security-review` over `main..HEAD` fixed or triaged in writing.
- [ ] **Then, and only then, ask** before merging or pushing. `git push`, merge, tag and branch deletion each need
      explicit per-action approval. Delete `fix/byte-cap-ceilings` after it merges.
