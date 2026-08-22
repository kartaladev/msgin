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

**Revision 3 — post-audit-round-2. NOT approved for implementation.**

**Two rounds of the adversarial design audit have run** over the assembled bundle
([Spec 018](../specs/018-byte-cap-ceilings.md) + [ADR 0034](../adrs/0034-byte-cap-ceilings.md) + this plan).
Round 1 returned **NOT SAFE TO IMPLEMENT** — 3 BLOCKERs, 7 MAJORs, 4 MINORs
([`032-audit-round-1.md`](032-audit-round-1.md), **immutable**). Round 2 returned **NOT SAFE TO IMPLEMENT** —
1 BLOCKER, 5 MAJORs, 6 MINORs ([`032-audit-round-2.md`](032-audit-round-2.md), **immutable**). This revision folds
every round-2 finding back. **Round 3 has not run.**

**What round 2 changed in THIS plan:**

| Finding | Change here |
|---|---|
| **N-1** BLOCKER (the restated gate invariant is false for the six `safe`-arm rows) | Step 6 Trap 3 rewritten **two-dimensionally** — the **arm** fixes the property, and only *within the reject arms* does the parameter type choose the literal. Revision 2's form would have demoted six `math.MaxInt` rows to `1<<30` and silently disabled the int32-truncation probe. |
| **N-2** (the derivation grep's predicate misses two sites) | Step 6's command **widened to the property**; the inventory is **14 sites**, adding `:26` and `:47`; the narrow form's output is **18 lines**, not 17. |
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

**The adversarial design audit has run TWICE and has not yet cleared this bundle.**
[CLAUDE.md](../../CLAUDE.md) makes it a hard gate: a fresh Opus subagent attacks the complete bundle —
[Spec 018](../specs/018-byte-cap-ceilings.md) + [ADR 0034](../adrs/0034-byte-cap-ceilings.md) + this plan —
**together**, before any implementation code. Both rounds returned **NOT SAFE TO IMPLEMENT**
([round 1](032-audit-round-1.md), [round 2](032-audit-round-2.md)). Two rounds is this project's established
norm and **this bundle has exhausted it without clearing**; Plan 029 needed **five**. **Round 3 must run on
revision 3 before implementation begins.**

> **Plan number — re-derived, not assumed.** `ls docs/plans/[0-9]*.md | sed -E 's|.*/([0-9]{3}).*|\1|' | sort -u | tail -3`
> → `029 030 031`. **030 and 031 are both TAKEN** by concurrent, undelivered work
> ([`030-post-029-maintenance.md`](030-post-029-maintenance.md), [`031-group-member-bounds.md`](031-group-member-bounds.md)).
> This plan is **032**.
>
> 🔴 **File overlap with the two live plans — check before branching.**
>
> | Plan | Touches | Overlap with 032 |
> |---|---|---|
> | **030** (backlog sweep) | `adapter/http` godoc, test-file constants, 386 arm | **`adapter/http/options.go` and `adapter/http/helpers.go` are being edited RIGHT NOW.** Line offsets in this plan will drift. Rebase on 030, do not merge. |
> | **031** (group members) | `routing`, `adapter/memory`, `adapter/database/sql`, **`sizing_option_class_gate_test.go`** | **The class gate is shared** (ADR 0033 **D-AL** extends it by hand; ADR 0034 **D-AS** empties its `deferred` arm). Whichever lands second rebases. |
>
> Re-derive with `git log --oneline main..` on each branch before Task 1.

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
   larger than a small multiple of the 1 MiB default — concretely **≤ 2 MiB**, and **never** a fixture sized to
   `byteCapCeiling`. The fixture is the hazard: a 2 GiB body peaks near **~4 GiB** through `io.ReadAll`'s
   doubling, in a package whose sibling runs `goleak.VerifyTestMain`, so it cannot be written.
   **A cap may be CONFIGURED at any legal value, including `byteCapCeiling` itself and `byteCapCeiling + 1`** —
   an `int64` field costs eight bytes to set and allocates nothing. The property *"the cap caps"* is a fact about
   the comparison, not about the ceiling value, so it is proven at small `n` with a small fixture; the ceiling
   itself is exercised by **`NewConfig` only** (Spec 018 §6 AC-1). Branch B1-4's **2 MiB** body against the 1 MiB
   default is the largest fixture in this increment and is the bound's intended ceiling, not an exception to it.

   > 🔴 **Revision 2 swapped the subject and made this worse, not better (round-2 N-3).** Round-1 **m-13**
   > correctly flagged that revision 1's *"no test **reads** more than 1 MiB"* collided with **one** branch;
   > revision 2 "fixed" it to *"no test may **configure** a cap above ~1 MiB"*, which bounds the harmless thing
   > (an integer) and collides with **five** branches — **B1-1** (`2147483647`), **B1-3** (`2147483648`),
   > **B1-5** (`2147483647`), **B1-9** (`1<<62`) — plus **both AC-2 upper-arm `EqualError` assertions** and
   > **AC-1's own first bullet**, which round-1 M-10 rewrote this same revision to require
   > `NewConfig(WithX(byteCapCeiling))` → accepted with an observable effect. An implementer applying revision 2
   > literally deletes the increment's entire upper arm. **Do not restore the revision-2 wording on the strength
   > of the m-13 citation.**
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
    the pasted line count was wrong. **The inventory is 14 sites; the widened command in Task 1 Step 6 is the
    authority.** This project's stored lesson is *derive move-lists mechanically* — **and derive them against the
    PROPERTY, not against a token list drawn from the sites you already know about** (*fix the class, not the
    instance*). **Re-derive every `adapter/http` offset with `gopls`** (match on the function name, the sentinel
    name and the predicate shape), and **generate** the gate's site list with the `grep` in Task 1 Step 6.
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

> 🔴 **Rebase check before starting.** [Plan 030](030-post-029-maintenance.md) has **landed** — the gate's
> `fixed`/`rejects` arms are already at `1<<30` and its `safe` arm at `math.MaxInt`. [Plan 031](031-group-member-bounds.md)
> (ADR 0033 **D-AL**) extends the same file by hand and may or may not have landed. Run
> `git log --oneline main..` and Step 6's `grep` before editing anything.

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
      (9 rows → 12, one literal → two). Run **the widened form** against **current `HEAD`** and paste the output
      into Evidence:

      ```bash
      grep -nE 'deferred|DEFERRED|"fixed"|9/1/3/6|9 \+ 1 \+ 3 \+ 6|\(9\)|\(3\)' sizing_option_class_gate_test.go
      ```

      Over-inclusion is safe — the `arm: "fixed"` rows (`:412`-`:511`) and the `wantArms` entries (`:772`-`:780`)
      classify as "no change" in one pass. **Under-inclusion ships.** The narrow revision-2 form returns **18**
      lines, not the 17 it pasted; run it too if you want the smaller list, but classify against the wide one.

      Then edit **every** hit that changes, per [Spec 018 §6 AC-4.1](../specs/018-byte-cap-ceilings.md)'s
      **14-site** table. The offsets below are **from `1212c63`/`46803c6` and will drift** — the grep is the
      authority:

      | # | Line(s) | Change |
      |---|---|---|
      | 1 | `:570`, `:579`, `:588` | `arm` field `"deferred"` → `"fixed"`; drop the trailing `// class member, remedy deferred` comment |
      | 2 | `:571-575`, `:580-584`, `:589-593` | `require.NoError` → `require.ErrorIs` on the knob's sentinel **+** `assert.EqualError` on the render **the row itself produces** — each row passes `1 << 62`, so the string is `msghttp: max body bytes out of range: msghttp.WithMaxBodyBytes: 4611686018427387904 not in [1, 2147483647]` and its twins — **+** `assert.False(t, msgin.IsPermanent(err), "R1 constructor error stays bare (ADR 0029 D-M)")` |
      | 3 | `:782-784` | `wantArms` entries → `"fixed"` |
      | 4 | `:803` | `byArm` — **remove** the `deferred` key |
      | 5 | `:35` | the header's arm-list bullet |
      | 6 | `:38` | `9 + 1 + 3 + 6 = 19` → `12 + 1 + 6 = 19` |
      | 7 | `:55-59` | **Plan 030's per-arm literal block** — goes false twice; see below |
      | 8 | `:401` | the `arm` field's vocabulary doc — **keep `"deferred"` with a tombstone**: *(no members as of Plan 032 — see Spec 018)* |
      | 9 | `:539-546` | the `---- arm: deferred ----` section banner |
      | 10 | `:547-556` | 🔴 block **one** — *"WHEN §3.8's CEILING LANDS…"* → a record |
      | 11 | `:557-568` | 🔴 block **two** — *"THESE THREE ROWS KEEP THE 1<<62 LITERAL"* → a record |
      | 12 | `:758`, `:761`, `:801`, `:805` | four prose strings **inside live `require.Equal` messages** naming the `9/1/3/6` split |
      | **13** | **`:26`** | 🔴 **round-2 N-2** — the header's `fixed` bullet, `- "fixed"    (9) — …`. Invisible to the narrow grep. `9` → `12` |
      | **14** | **`:47`** | 🔴 **round-2 N-2** — Plan 030's `- "fixed" (9) and "rejects" (1) → 1<<30 …` bullet. Goes false **twice**: the count (9 → 12) **and** the literal, since 3 of the 12 sit at `1<<62`. This is site 7's defect one bullet higher |

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
      | D-3 | *"the disclosure is actually present"* | `grep -c 'not a safety guarantee' adapter/http/options.go` → **3**, **vacuity-probed**: delete one occurrence, confirm the count reads 2, restore |

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
      - **`docs/HANDOVER.md`** §7 item 6 → **CLOSED**, citing this bundle.
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

      🔴 **Do NOT edit** `docs/plans/029-audit-round-*.md`, `docs/plans/030-audit-round-1.md`,
      [`032-audit-round-1.md`](032-audit-round-1.md), [`032-audit-round-2.md`](032-audit-round-2.md), or any
      `*-derivation-findings.md` — they are **immutable execution records** and correctly record what was true
      when written (`docs/HANDOVER.md` §8). Plan 029 itself is a *delivered* plan and **may** be corrected in
      place if it states the deferral as a standing fact; prefer a dated one-line note over a rewrite.
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
| B1-4 | body gate, `!set` → default | `NewConfig_default_body_cap_is_1MiB` (unset ⇒ a **2 MiB** body is rejected by `DecodeRequest`) — the largest fixture in the increment, at Global constraint 6's stated ≤ 2 MiB bound | delete the default assignment ⇒ the cap reads `0`, the case fails |
| B1-5 | body gate, set + in range | `NewConfig_accepts_the_ceiling/body` **and** `NewConfig_accepts_one` | see B1-1 |
| B1-6 | body gate, set + out of range | `…/rejects_ceiling_plus_one/body`, `…/rejects_zero/body`, `…/rejects_1<<62/body` | delete the `else if` arm ⇒ all three fail |
| B1-7 | response gate, all three arms | the `…/response` twins of B1-4/5/6 | as above, per arm |
| B1-8 | event gate, all three arms | the `…/event` twins of B1-4/5/6 | as above, per arm |
| B1-9 | **the `1<<62` case specifically** | `NewConfig_rejects_the_gate_value` (all three) — the exact literal the class gate uses | keep the `<= 0` check and drop the upper arm ⇒ fails. **This is the case Step 6's gate rows depend on** |
| B1-10 | **the classification** (round-1 M-8) | `assert.False(t, msgin.IsPermanent(err), …)` on every rejecting case, and on the three moved gate rows | wrap the return in `msgin.Permanent(...)` ⇒ every rejecting case fails. **Without this, D-AQ's non-`Permanent` claim has no covering test at all** |

> **B1-4/B1-7/B1-8's default arms are pre-existing branches**, not new ones. They are listed because the
> `else if` rewrite sits inside the same `if/else if` and a botched edit can swallow the `!set` arm — a class of
> failure the existing suite would catch only for whichever knob happens to be covered. Cover all three.

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
`NewConfig` returns a non-nil `*Config` **and a nil error**, and that the knob's effect is observable — for
`WithMaxBodyBytes` via `DecodeRequest` on a small body, for `WithMaxResponseBytes` via an `httptest` round-trip,
for `WithMaxEventBytes` via `NewSSEParser` + `Next` on a small event. 🔴 **Do not assert an accessor** — `maxBody()`
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
| M3-6 | drop `assert.False(t, msgin.IsPermanent(err), …)` from one moved row and wrap that sentinel in `msgin.Permanent` | nothing — **which is the point**: run it to prove the assertion is load-bearing before relying on it |

**Evidence block to record:** Step 2's two greps with every hit classified **and the re-derived totals**; the RED
failures (count and names); Step 6's widened `grep` output verbatim plus which of the **14** sites each hit maps
to, and the disposition of `:766`; Step 7's landing-order determination; the killed mutants (B1-1 … B1-10,
M3-1 … M3-6); M1-7's stated non-kill; Step 9's two exit codes **and Step 9b's four probe outputs**; Step 11's
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
- [ ] **Step 2.** 🔴 **Do NOT edit** `docs/plans/029-audit-round-*.md`, `docs/plans/030-audit-round-1.md`,
      [`032-audit-round-1.md`](032-audit-round-1.md), [`032-audit-round-2.md`](032-audit-round-2.md), or any
      `*-derivation-findings.md` — they are
      **immutable execution records** and correctly record what was true when written (`docs/HANDOVER.md` §8).
      Plan 029 itself is a *delivered* plan and **may** be corrected in place if it states the deferral as a
      standing fact; prefer a dated one-line note over a rewrite.
- [ ] **Step 3.** Flip Spec 018 / ADR 0034 status headers from **PROPOSED** to **ACCEPTED** with the date, and add
      the *"as delivered"* addenda for anything the implementation taught (the amend-don't-pile-on rule: fold
      these into this task's commit, do not add a follow-up `docs:`).
- [ ] **Step 4 (LINK GATE).** Run **both arms** of CLAUDE.md's docs-link gate over every tracked `.md` — note
      that `git ls-files` is blind to **untracked** files, so `git add -N` the **five** new artifacts first
      (Plan 030 round-1 MINOR 11). Known false positives on this tree are exactly two — `docs/plans/m` and
      `docs/specs/factory(fireTime`, both Go identifiers in wrapped code spans. **Anything else is a blocker.**
- [ ] **Step 5 (VACUITY PROBE).** Prove the gate is not vacuous **on the new files, not on root** (the Plan 028
      `apidiff` blindness came from probing only root): plant one bad relative link and one bad `#anchor` in
      `docs/specs/018-byte-cap-ceilings.md`, re-run both arms, confirm **exactly one new hit each**, revert, and
      confirm both vanish. The **five** files under gate are this plan, Spec 018, ADR 0034,
      [`032-audit-round-1.md`](032-audit-round-1.md) and [`032-audit-round-2.md`](032-audit-round-2.md).
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
- [ ] All **14** derived gate sites edited or explicitly classified, including `:26` and `:47` (round-2 N-2).
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
- [ ] Both link-gate arms clean but for the two known false positives, run over the **five** `git add -N`'d
      artifacts, and **vacuity-probed on the new files**.
- [ ] Eight-module loop green; the six non-test quality-gate steps run per touched module.
- [ ] Every finding from `/code-review` and `/security-review` over `main..HEAD` fixed or triaged in writing.
- [ ] **Then, and only then, ask** before merging or pushing. `git push`, merge, tag and branch deletion each need
      explicit per-action approval. Delete `fix/byte-cap-ceilings` after it merges.
