# Plan 031 — A message group's member count is bounded at the store

> **For agentic workers:** REQUIRED SUB-SKILL: `superpowers:subagent-driven-development` (the project default) or
> `superpowers:executing-plans`. Steps use checkbox (`- [ ]`) syntax.
>
> **Go-skills hard rule ([CLAUDE.md](../../CLAUDE.md), restated here because `superpowers:writing-plans` omits
> it):** every task starts from **`cc-skills-golang:golang-how-to`**, the always-on orchestrator, which routes this
> increment to `golang-safety` (unbounded growth and admission checks), `golang-error-handling` (sentinel reuse,
> wrapping, `errors.Is`, permanent-vs-transient classification), `golang-design-patterns` (functional options),
> `golang-database` (the in-transaction dialect enforcement of Tasks 5–6), `golang-testing` and
> `golang-documentation`. Load the primary skill **plus all applicable secondary skills together, up front** — do
> not work from memory.
> **`superpowers:test-driven-development`** governs every task: red → green → refactor, failing test first, never
> implementation ahead of a failing test. **`gopls`** (via the `LSP` tool) for all navigation, diagnostics and
> refactoring — go-to-definition, find-references, rename, post-edit diagnostics — **not `grep`** when reasoning
> about Go symbols. The project-local overrides apply and beat samber's guidance where they conflict:
> **`table-test`** (assert-closure form, never `want`/`wantErr` fields; `ctx` modifier; `t.Context()`),
> **`use-mockgen`** (uber-go/mock, `--typed`, alongside the interface — applies to Task 5's dialect double only if
> a generated mock replaces the existing hand-written `fakeGroupDialect`), and **`use-testcontainers`**
> (Tasks 6–7 use the shipped Docker-backed `dbtest`/`harness` runners — **never** a mock, in-memory fake or shared
> dev database for the dialect conformance work).
>
> **This plan is deliberately thin** (Plan 024/026/028/029 precedent): signatures, positions, branch coverage and
> commit boundaries — **no embedded implementations**. Write the code TDD-first from the tables below. The two
> exceptions are the `memory` cap-check ordering (Spec 017 §3.4a) and `Handle`'s snapshot branch (§3.3a), which
> carry pseudocode in the spec because *both* are places where a plausible implementation is a defect.

**Revision 5 — post-audit-round-4. NOT approved for implementation.**

**Round 1 verdict: NOT SAFE TO IMPLEMENT** — 3 BLOCKERs, 8 MAJORs, 10 MINORs, recorded immutably in
[`031-audit-round-1.md`](031-audit-round-1.md). Revision 2 folded every finding back in. The three that reshaped
*this document* were:

| Finding | What it did to this plan |
|---|---|
| **B-2** | The class gate is exact set equality, so revision 1's "all gate edits in the final task" would have made **six of nine tasks commit a red suite**. The gate key + conformance row now land **in the same commit as the option**, and the task list is restructured around that. |
| **B-1** / **M-6** | The store's error classification (Spec 017 §3.3.1) and `Handle`'s snapshot branch (§3.3a) are new work that did not exist in revision 1. Task 1 grew; a new Task 3 appeared. |
| **B-3** / **M-1** | Every figure about `sizing_option_class_gate_test.go` is **re-derived at `d2c69fe`**, post-Plan-030. Revision 1's *"they share no file"* was false. |

**Round 2 verdict: NOT SAFE TO IMPLEMENT** — 1 BLOCKER, 6 MAJORs, 7 MINORs, recorded immutably in
[`031-audit-round-2.md`](031-audit-round-2.md), against a fix-verification score of **12 clean LANDED, 8
LANDED-BUT-FLAWED, 1 (M-8) landed with a defensible ADR omission, 0 NOT LANDED, 0 REGRESSED** *(that summary and
the record's own 21-row table differ by one row in the middle bucket — reconciled by name in the record, left open,
nothing downstream depends on it)*. **Its lesson is not the count — revision 2 failed to GENERALIZE
its own two structural fixes**, and both failures land in *this* document:

| Finding | What it did to this plan |
|---|---|
| **N-1** (BLOCKER) | **B-2's insight — *a cross-module edit is a red commit* — was applied to the class gate and NOT to the `AddMember` signature.** `harness` holds a `GroupDialect` and calls the 8-arg `AddMember`; `dbtest` requires `harness`. **The Tasks 5+6 commit shipped two non-compiling modules**, behind a Task 6 Step 5 gate that lists four. Task 7 Step 1's harness update is now **folded into that commit**, Global constraint 8 is restated as a *compiles-against* rule, and the gate gains `harness` (`go vet`) and `dbtest`. |
| **N-4** / **N-5** | Two **declaration-form** constraints an implementer copying local precedent would violate: named `defaultMaxGroupMembers` constants (**D-AR**, Tasks 1/3/5) and a private `limit int` on the three `*SelectMembers` helpers (**D-AS**, Task 6). Revision 2's instructions were, respectively, un-parseable and unimplementable. |
| **N-6** / **N-7** | Test fixtures and branch coverage: AC-1b's id-less message is pinned to `msgin.NewMessage` and asserted (`msgin.New` **always** stamps an id), and `Handle`'s branch gains four covering cases for its six exits. `routing/aggregator_test.go` joins Task 1's Files. |
| **N-9** / **N-14** | Two *"fix the class"* misses: Task 5 Step 5 now **corrects** the shipped `AddMember` godoc rather than only adding to it, and B-2's ordering rule — which never reached ADR 0033 — is recorded in **D-AL**. |

**Round 3 verdict: NOT SAFE TO IMPLEMENT** — 1 BLOCKER, 6 MAJORs, 3 MINORs, recorded immutably in
[`031-audit-round-3.md`](031-audit-round-3.md), against a fix-verification score whose **table tallies 9 LANDED /
5 LANDED-BUT-FLAWED, 0 NOT LANDED, 0 REGRESSED** *(the auditor's summary line reads 8/6; both total 14, the record
leaves the one-row gap unreconciled, and **the table governs** because each row carries its own evidence while the
score line is a derived tally. This is the **second consecutive round** with that gap — a pattern the record names
as a method defect in the audit apparatus, not in the artifacts)*. **Its lesson, verbatim: *revision 3 generalized round 2's two
structural fixes correctly, but stopped the fix at the boundary of each finding's own wording.*** Both
generalizations — Global constraint 8's *compiles-against* rule and **D-AS** — are right and survive. What did not
survive is everything one step outside the words each finding quoted:

| Finding | What it did to this plan |
|---|---|
| **NEW-1** (BLOCKER) | **Task 3's *"or split it"* alternative is DELETED and the task is REORDERED to run after Tasks 5+6.** The split was justified from N-1's rule, and that reads N-1's *shape* rather than its *mechanism*: N-1 is about a **pre-existing** gate (`sizing_option_class_gate_test.go` ships on `main` and asserts exact set equality, so the option's existence alone makes root red). Task 3's AST test **does not exist yet**, so the dependency runs the other way — the test ships no earlier than the last declaration. See **D-AT**. |
| **NEW-2** | The six-module gate's **`dbtest` arm is now `go vet ./...`, not `go build ./...`**: every Go file in `dbtest` is a `_test.go` file, so `go build` compiles nothing and exits 0 whatever breaks — N-1's own defect reproduced inside N-1's fix, one row below the `harness` row that reasons about the mirror case correctly. |
| **NEW-3** | Task 5 Step 6b's *"threading the cap from the existing `TestKit`"* had **no referent** — `TestKit` has no cap field and `testkit.go` is in no Files list. Replaced with a **`harness`-package unexported constant** serving both the call site and Task 7's case. |
| **NEW-4** | AC-3.3's parse set is **asserted**, so the constants' **file** is as load-bearing as their form. Tasks 1 and 5 now name `<store>/groupstore.go` explicitly. |
| **NEW-5** | The `sql` overflow render's site is decided as **`msgin/sql/<engine>: AddMember`** — the error is minted *inside the dialect*, which cannot know it was reached through `GroupStore.Add`. The render assertion moves to **Task 7**, where a real engine mints it. |
| **NEW-6** | The downgrade-only rule is **restated truthfully**: exits 3 and 5 replace the overflow error with a *different fault carrying its own classification*, so a persistently failing claim/release path **retries** rather than terminating. |
| **NEW-7** (design flaw) | **`sql` now counts LIVE + CLAIMED, like `memory`** (**D-AF**, reversed). With live-only counting plus D-AS's `limit = 0` on `ClaimGroup`, the durable member table was **unbounded**. Tasks 6 and 7 grow: a `COUNT(*)` via the shipped `*CountMembers` helper, a `locked_by` read for D-AM's leased arm, and their coverage. |
| **NEW-9** / **NEW-10** | Three off-by-one citations and four normative cells still naming the WARN N-11 proved never fires. |

**Round 4 verdict: NOT SAFE TO IMPLEMENT** — 1 BLOCKER, 3 MAJORs, 4 MINORs, recorded immutably in
[`031-audit-round-4.md`](031-audit-round-4.md), against a fix-verification score of **7 clean LANDED, 3
LANDED-BUT-FLAWED, 0 NOT LANDED, 0 REGRESSED** — *and for the first time on this increment the auditor's score
line and its own table AGREE, because round 3's procedural remedy was applied: the line was **generated by
counting the table**, not written alongside it.* **Its lesson, verbatim: *the design is sound; the execution
instructions are not synchronised with the tree they run against.*** **No decision in ADR 0033 was falsified** —
D-AF's reversal survives attack, and round 4 verified **every code claim it rests on**, one by one. What broke is
this document's layer: [Plan 032](032-byte-cap-ceilings.md) **landed** at `f39725d` and rewrote
`sizing_option_class_gate_test.go`.

| Finding | What it did to this plan |
|---|---|
| **R4-1** (BLOCKER) | **`sizing_option_class_gate_test.go` half 2 holds TWO exact-map `require.Equal`s this plan never named** — `wantArms` (19 entries) and the `byArm` literal `{"fixed": 12, "rejects": 1, "safe": 6}`. Both are `require`, so adding two conformance rows **aborts** the test and **Tasks 1 and 5 cannot reach green**. They predate Plan 030 (`git log -S 'wantArms'` → `e473deb`) and have been missing through **all four rounds** — this plan used `wantArms` only as Task 9's *probe target*, never as an *edit target*. Both are now enumerated in Tasks 1 and 5 Step 2, in Task 9 Step 3's derived table, in Spec AC-8 and in **D-AL**. **And the assertion's own failure message directs its implementer to amend Spec 016 §2.1 + §6 AC-5 — a delivered spec no task here opened — so this plan gains [Task 9b](#task-9b--fold-the-two-new-arm-rows-back-into-spec-016-r4-1) to own that fold-back.** |
| **R4-2** | **Every** class-gate figure was stale, **including two of round 3's own three NEW-9 fixes** (`hasIntOrInt64Param`, `isIntOrInt64`), and **two were wrong in COMPOSITION**: the arm arithmetic (`11+1+3+6` — the `deferred` arm is now **empty and tombstoned**) and the *"`fixed` ⇒ `1<<30`"* rule (`fixed` now holds three `int64` rows that keep `1<<62`). 🔴 **The fix is a METHOD change, adopted verbatim from the auditor: *"Re-deriving citations is not the remedy; deriving the edit list from the file is."*** Task 9 Step 3's table is now **generated by a script that ships in the task**. |
| **R4-3** | The crashed-lease window was understated **~2×** — the reaper ticks on a cadence unrelated to the crash, so discovery is up to **2 × `leaseTTL` ≈ 10 minutes** — and the wrong figure was scheduled into `sql.WithMaxGroupMembers`'s **public godoc** (Step 7 below). The per-iteration cost is restated as what it is: a full write transaction taking the group-row lock, plus a `SchemaExists` probe. **The transient disposition is unchanged and correct.** |
| **R4-4** | **Task 3 Step 0's order gate asserted a COUNT and stopped on a CORRECT tree.** Tasks 1 and 5 each put ≥2 occurrences of `defaultMaxGroupMembers` in one file (the `const`, the initialiser, and — per Step 7's mandated godoc shape — the doc comment). Now **three per-declaration conditions, no count anywhere** — which is what **D-AT `:1346`** already said the gate did. |
| **R4-5** | The `checkRange` inventory was stale in **both** the number and the coordinate: the pasted grep now returns **five**, and `adapter/http`'s copy moved `:64 → :73` (Plan 032 added `checkRangeInt64` at `:115`). |
| **R4-6** / **R4-8** | Two disclosure defects created by D-AF's reversal: the `classifyQueryErr` Consequences bullet is false for the **leased** arm, and the `COUNT(*)` cost is priced engine-neutrally when sqlite's sits inside a **database-wide** write lock. |
| **R4-7** | **All three artifact headers assert re-derivation against `d2c69fe`, a commit that is neither the measured tree nor `main`** (Spec `:44`, this plan `:38`, ADR `:55`). **Closes with R4-2** — same edit, which is why it was relayed folded into R4-2's bullet; see [`031-audit-round-4.md`](031-audit-round-4.md)'s provenance box for that labelling correction. **Revision 5 closes all eight of round 4's findings.** |

🔴 **The design this plan executes was decided WITHOUT USER RATIFICATION**, and rounds 1, 2, 3 and 4's
dispositions were taken the same way. Every decision in [ADR 0033](../adrs/0033-group-member-bounds.md) (**D-AC**
… **D-AT**) is open to reversal, and **four** now change this plan's size materially:

- **D-AG** (SQL enforcement inside the dialect's transaction) is what makes Tasks 5–7 exist at all. The cheap
  alternative — count in `sql.GroupStore.Add` after `AddMember` returns — collapses them into a single task but
  bounds nothing durable. **If that reversal is coming, it must land before Task 5 starts, not at Task 7.**
- **D-AJ** (ship 65,536 as a *default*, not opt-in) is the behavioral break. Reversing it changes one constant per
  store and one paragraph of godoc — **but D-AJ now depends on D-AM**; reversing D-AM without also reverting to
  opt-in re-opens the hot spin.
- **D-AM** (permanent classification for a not-leased over-cap rejection) is the disposition of BLOCKER B-1. It is
  a genuine trade-off, it is recommended rather than obvious, and [Spec 017 §8 item 5](../specs/017-group-member-bounds.md)
  lists three alternatives if the user rejects it. Reversal is one branch per store — **but reversing it without
  a replacement restores an unlogged infinite hot spin under the shipped defaults.**
- **D-AF, REVERSED in revision 4** (both stores count **live + claimed**) is what makes Tasks 6–7 grow again.
  Revision 3 had `sql` count live only; audit **NEW-7** showed that composed with **D-AS**'s `limit = 0` on
  `ClaimGroup`, the durable member table is **unbounded**. The reversal costs a `COUNT(*)` per `AddMember` and
  gives `sql` D-AM's leased arm, which it did not previously have. **Reversal cost: one predicate per dialect —
  but reversing it back re-opens the unbounded table.**

**FOUR AUDIT ROUNDS HAVE RUN** ([round 1](031-audit-round-1.md), [round 2](031-audit-round-2.md),
[round 3](031-audit-round-3.md), [round 4](031-audit-round-4.md)) — two more than this project's established norm,
and both extras were earned: round 3 returned the increment's first genuine design flaw since round 1 (**NEW-7**),
and round 4 returned a BLOCKER that had survived every previous round.

> 🔴 **A ROUND 5 IS NOT AUTOMATICALLY WARRANTED, AND THIS PLAN SAYS WHY RATHER THAN LEAVING IT TO JUDGEMENT.**
> Round 4 called for a fourth round because **revision 4 changed a DECISION** (D-AF's reversal) and so was not the
> bundle round 3 had audited. **Revision 5 changes no decision.** Round 4 falsified none: *"the design is sound;
> the execution instructions are not synchronised with the tree they run against."* Every fix below is a
> coordinate, a magnitude, a count, an edit-list omission, or the new **Task 9b** — nothing reverses, weakens or
> re-scopes an ADR 0033 decision.
>
> **No round-4 finding is left open.** All eight are folded in, including **R4-7**, which reached the record
> **unlabelled** — the coordinator's brief folded it into R4-2's bullet because the two take the same edit — and
> is written up in [`031-audit-round-4.md`](031-audit-round-4.md) with that provenance marked.
> **Implementation remains blocked on user ratification** of the unratified decisions (**D-AC** … **D-AT**), which
> is the only thing still gating it.
>
> **A note on the shape of the remaining risk, so a fifth round is not spent re-checking what round 4 already
> proved.** Round 4 verified **every code claim D-AF's reversal rests on**, individually — the three
> `*CountMembers` helpers, their absent `claimed_epoch` predicate, their single `SettleGroup` caller each, the
> zero-round-trip `locked_by` read per dialect, and the nine `*SelectMembers` sites — plus a 261-citation
> mechanical sweep whose only failures were confined to the two files `git diff d2c69fe..HEAD -- '*.go'` reports
> as changed. **The design layer is well audited. The layer that keeps breaking is this plan's synchronisation
> with a moving tree**, and Global constraint 12 is now a *generate-it* rule rather than a *re-derive-it* rule for
> exactly that reason.

> **🔴 CONCURRENT-WORK DEPENDENCIES — revision 1's claim here was FALSE (audit B-3), AND BOTH COLLIDING
> INCREMENTS HAVE NOW LANDED.** Revision 1 said Plan 030 and Plan 031 *"share no file."* They share **four**;
> Plan 030 landed at `7ab91cd`/`1a1c135`/`d2c69fe`, and **[Plan 032](032-byte-cap-ceilings.md) has since landed
> too, at `f39725d` — it lands SECOND, so Plan 031 is the increment that must re-derive** (audit **R4-2**).
>
> | File | Plan 030 did | Plan 032 did | Plan 031 does |
> |---|---|---|---|
> | `sizing_option_class_gate_test.go` | rewrote **135 lines** (`d2c69fe`) | **rewrote 237 more** (`f39725d`); file is now **869** lines | Tasks 1, 5, 9, **9b** |
> | `adapter/memory/groupstore.go` | +1 line at `:93` (`1a1c135`) | — | Task 1 |
> | `adapter/database/sql/groupstore.go` | +1 line at `:207` (`1a1c135`) | — | Task 5 |
> | `adapter/memory/queuestore.go` | +1 line (`1a1c135`) | — | cited by Spec 017 §3.3 |
> | `adapter/http/helpers.go` | — | `checkRange` `:64 → :73`; added `checkRangeInt64` at `:115` | cited by Task 5 Step 4 |
>
> **Consequence 1 — the measurement tree is `de38a95`, NOT `d2c69fe`.** Revisions 1-4 asserted re-derivation
> *"against `d2c69fe` — current `main`"*; that is now false on both halves (audit **R4-2**). Round 4 audited
> `f39725d`; `a306241` was docs-only; **`de38a95` then landed a GODOC-ONLY change that STILL shifted 41 citations
> in this bundle.** Run it and read the answer — do not trust this paragraph either:
>
> ```
> $ git diff --stat d2c69fe..HEAD -- '*.go'
>  adapter/http/{config_sizing_bounds_test.go,errors.go,exchange_test.go,helpers.go,options.go}
>  sizing_option_class_gate_test.go | 237 ++++++++++++++---------
>  … plus de38a95's comment-only edits: net +1 in adapter/database/sql/groupstore.go, net 0 in adapter/memory/
> ```
>
> 🔴 **A GODOC COMMIT IS ENOUGH TO FALSIFY A LINE NUMBER, AND THIS ONE DID — 41 TIMES, WHILE REVISION 5 WAS BEING
> WRITTEN.** `de38a95` reworded two doc comments and moved **every `adapter/database/sql/groupstore.go` coordinate
> at or below `:207` by +1**: `:211`→`:212`, `:250-276`→**`:251-277`**, `:271`→`:272`, `:273`→`:274`,
> `:284-297`→`:285-298`, `:348`→`:349`, `:365`→`:366`. All 41 were corrected **mechanically, not by eye**.
> **`adapter/memory/groupstore.go` is net 0 and did NOT move** — its group-count arm is still `:123-125`, the
> append `:135`, the `checkRange` call `:105-108` — and `sizing_option_class_gate_test.go`, `routing/` and both
> `helpers.go` files were untouched by `de38a95`. **This is Global constraint 12's thesis demonstrated live, on
> this very revision.**
>
> **Consequence 2 — the two new rows assert `1<<30`, and the REASON the bundle gave for it is FALSIFIED** (audit
> **R4-2**). *"`fixed` ⇒ `1<<30`"* is no longer true: since Plan 032 the `fixed` arm holds **three `int64` rows
> that keep `1<<62`**, for which `1<<30` would be **ACCEPTED** (`byteCapCeiling = math.MaxInt32 = 2,147,483,647`,
> above `1<<30`). The file now states the rule in **two dimensions**, and the second one is the operative one:
>
> 1. **The ARM fixes the required PROPERTY.** `safe` ⇒ must be *accepted* and maximally absurd ⇒ `math.MaxInt`.
>    `fixed`/`rejects` ⇒ must be *out of range* and must render an **architecture-independent decimal**.
> 2. 🔴 **Within a reject arm, the PARAMETER TYPE chooses the literal.** `int`-typed ⇒ **`1<<30`** (decimal
>    `1073741824`). `int64`-typed ⇒ **`1<<62`**.
>
> **Both new options are `func(n int)`, so both rows assert `1<<30` — the instruction is unchanged. Do not carry
> the old reason forward**: an increment adding an `int64` knob to `fixed` and following *"`fixed` ⇒ `1<<30`"*
> ships a row that is accepted and whose every `require.ErrorIs` fails.
>
> **Consequence 3 — the arm ARITHMETIC changed composition, and the total survived by coincidence** (audit
> **R4-2**). Revisions 1-4 said *"11 fixed + 1 rejects + 3 deferred + 6 safe = 21."* Plan 032 moved the three
> `msghttp` byte caps out of `deferred` into `fixed` and **tombstoned the `deferred` arm empty**, so the file now
> reads `12 + 1 + 6 = 19` and the true post-increment partition is:
>
> **14 fixed + 1 rejects + 0 deferred + 6 safe = 21 rows = 19 AST keys + 2 manual rows.**
>
> `11+1+3+6` and `14+1+0+6` both total 21. **That is the project's `43 ≠ 43` lesson landing on this bundle:
> reconcile by NAME, never by count.** 🔴 **And `byArm` is built by COUNTING, so the empty arm has NO KEY there —
> adding `"deferred": 0` FAILS the assertion.** The file's own comment says so; Tasks 1 and 5 must not add it.

**Goal.** Deliver [Spec 017](../specs/017-group-member-bounds.md): a message group cannot grow without a stated
bound **whichever of the four release paths is in force**, because the bound moves from the release decision — where
three of four paths are opaque — to the **store**, which is the only site that can refuse a member *before*
retaining it.

**Architecture.** [ADR 0033](../adrs/0033-group-member-bounds.md) — **D-AC** (the bound lives at the accumulation
site), **D-AD** (two `WithMaxGroupMembers` options, one name in both packages; default `1<<16`, ceiling `1<<20`;
`checkRange` + `msgin.ErrInvalidCapacity`; mint no sentinel), **D-AE** (`msgin.ErrOverflowDropped`, wrapped),
**D-AM** (**classified by cause** — not-leased ⇒ `Permanent`, leased ⇒ transient), **D-AN** (**the live snapshot
rides out with the error; `Handle` re-evaluates the release**), **D-AO** (the cap check sits between the dedup
lookup and the dedup insert), **D-AF** (**REVERSED in revision 4 — BOTH stores count live + claimed**; audit
**NEW-7**), **D-AG** (SQL enforcement
in-transaction, `AddMember` takes `maxMembers`), **D-AP** (**per-dialect placement; the `*sql.Tx` caller-owned
precondition**), **D-AH** (the SPI states the bound), **D-AI** (godoc cross-references on the three unbounded
release paths), **D-AJ** (a default is legitimate here), **D-AK** (bounded-but-stuck is accepted), **D-AL** (the
class gate is extended by hand; its blind spot is stated, not widened — **and half 1's exact set equality means a
gate key ships in its option's own commit**), **D-AQ** (the `default ≥ completionSizeCeiling` invariant IS
mechanically enforceable, by AST), **D-AR** (**both new sizing values are NAMED constants in both packages**),
**D-AS** (**a private `limit int` on the three `*SelectMembers` helpers; only `AddMember` passes non-zero**),
**D-AT** (**Task 3 is REORDERED after Tasks 5+6, never split** — audit **NEW-1**).

**Predecessors this builds on, not re-argues.** [Spec 016](../specs/016-sizing-option-bounds.md) /
[ADR 0032](../adrs/0032-sizing-option-bounds.md) / [Plan 029](029-sizing-option-bounds.md): **D-X** (sentinel
reuse + wrap shape), **D-Z** (why 65,536), **D-AB** (the membership criterion), and the shipped `checkRange`
helper and class gate — **as rewritten by [Plan 030](030-post-029-maintenance.md) Task 2**.

**Tech stack.** Go 1.25 (`GOTOOLCHAIN=go1.25.13`). Touches **six** of the eight modules — root (packages `msgin`,
`routing`, `adapter/memory`, `adapter/database/sql`) plus `adapter/database/sql/{postgres,mysql,sqlite,harness}`
and `adapter/database/sql/dbtest` — and the delivery gate is all **eight**. *(Revision 1 said "five" in one place
and implied six in another — audit m-8. It is **six**.)* Tasks 6–7 need a **running Docker daemon**.

**Traceability.** Implements Spec 017; decided by ADR 0033; audited in
[`031-audit-round-1.md`](031-audit-round-1.md), [`031-audit-round-2.md`](031-audit-round-2.md),
[`031-audit-round-3.md`](031-audit-round-3.md) and [`031-audit-round-4.md`](031-audit-round-4.md) (all four
**immutable**). **Task 9b additionally amends [Spec 016](../specs/016-sizing-option-bounds.md) §2.1 / §6 AC-5** —
a delivered spec — and carries a `Spec: 016` trailer alongside the rest. Every commit carries `Spec: 017`,
`Plan: 031`, `ADR: 0033` trailers. Branch: `feat/group-member-bounds`, off `main`.

---

## Global constraints

1. **Start every task from `cc-skills-golang:golang-how-to`**, plus the secondary `golang-*` skills it routes to
   (header note). **TDD via `superpowers:test-driven-development`** — failing test first, always. **`gopls` for
   navigation and refactoring**, not text search. **`table-test` / `use-mockgen` / `use-testcontainers` override**
   samber's testing guidance wherever they conflict. This is restated per-task in each task's first step; it is
   **not** delegated to an SDD dispatch prompt.
2. **Blackbox tests only** — `package <pkg>_test`, exercising the exported API. No whitebox fallback. A test that
   seems to need an unexported helper is rewritten through the public surface. *(The AST tests of Task 3 and
   Task 9 are the canonical example: they reach unexported constants without whitebox access, by parsing.)*
3. **Assert-closure tables** — every case carries `assert func(t *testing.T, …)`; never `want`/`wantErr` fields.
   `t.Context()`, never `context.Background()`.
4. **The error SHAPE is fixed and identical in both stores and all three dialects:**
   `fmt.Errorf("%w: %s: group %q holds %d members, limit %d", msgin.ErrOverflowDropped, site, key, n, max)`.
   **Wrapped in `msgin.Permanent` when the group is NOT leased, bare when it is** (D-AM — this REPLACES revision
   1's blanket "no `Permanent` wrap"). The construction-time shape is the shipped `checkRange` render, unchanged.

   > 🔴 **`site` IS FOUR DIFFERENT STRINGS, AND REVISION 3 SPECIFIED ONE OF THEM WRONG** (audit **NEW-5**). The
   > shape fixes the format; it never fixed the substitution, and AC-2c pinned `sql`'s as `sql.GroupStore.Add` —
   > **a site the error is not minted at.** Task 6 Step 4 mints it **inside the dialect**, which cannot know it was
   > reached through `GroupStore.Add`; and AC-4b deliberately drives `kit.Group.AddMember` **directly**, where that
   > render names a store never involved. The decided values:
   >
   > | Minted in | `site` |
   > |---|---|
   > | `adapter/memory/groupstore.go` (both arms) | `memory.GroupStore.Add` |
   > | `postgres/groupdialect.go` | **`msgin/sql/postgres: AddMember`** |
   > | `mysql/groupdialect.go` | **`msgin/sql/mysql: AddMember`** |
   > | `sqlite/groupdialect.go` | **`msgin/sql/sqlite: AddMember`** |
   >
   > This is the **shipped convention** — every error a dialect mints today is prefixed `msgin/sql/<engine>:`
   > (`postgres/groupdialect.go:67`, `mysql/groupdialect.go:63`, `sqlite/groupdialect.go:55`) — and it is the only
   > form that **discriminates the engine**, which is D-AE's own debuggability argument (*"a bare sentinel cannot
   > tell an operator which cap fired"*) applied to three dialects rendering one identical site.

   > 🔴 **The shape is identical; the COUNT is not, and must not be forced to be** (audit **N-8**). `n` is
   > **"members retained at the moment of the check"**, and the two checks sit on opposite sides of the write:
   > `memory` checks **before** the append, so at a cap of 4 it renders `holds 4 members, limit 4`; the dialects
   > check **after** the member upsert (Task 6 Step 2 — required, so an idempotent re-add at cap stays a no-op), so
   > they render `holds 5 members, limit 4`. **Spec 017 §6 AC-2c pins BOTH renders**, and **Task 7 executes the
   > `sql` half against a real engine** (audit **NEW-5**: a render assertion through Task 5's fake dialect would be
   > vacuous, since the fake mints whatever the test hands it). Do not "normalise" `sql` to `len(members)-1` — that
   > renders a count no statement in the transaction ever observed.
   >
   > **Both counts are now over the SAME SET — live + claimed** (D-AF, reversed in revision 4; audit **NEW-7**).
   > Revision 3 had `sql` count live only, so the two `%d`s were over different sets *and* on different sides of
   > the write. Only the side-of-the-write difference survives.
5. **No new exported sentinel, in any module** (D-AD / ADR 0032 D-X). A task that appears to need one has hit a
   design fault: **stop and escalate.**
6. **No test grows a group past 16 members** (Spec 017 AC-6). Ceiling values are exercised by **constructors
   only**. Growing a group to `1<<16` costs 8.6 s and 48.3 GiB of churn (Spec 016 §1.4) and the shipped
   `completionSizeCeiling` godoc already forbids it.
7. **Mutation-prove every new assertion** with a mutant that targets **that** assertion (the project's standing
   rule: *a killed mutant is the evidence, not a green run*). Each task carries a mutant table; record the killed
   mutant per case in the task's Evidence block. **A case that survives its own mutant is rewritten.**
8. **Each task is a green unit** — `GOWORK=off go test ./... -race -shuffle=on` (and `go vet ./...` where a module
   has no test files) passes in **every module that COMPILES AGAINST anything the task changed**, before its
   commit. No WIP or broken-build commits.

   > 🔴 **"COMPILES AGAINST", NOT "APPEARS IN THE FILES LIST" — this is the BLOCKER of audit round 2 (N-1).**
   > Revision 2 read this constraint as *"every module it touched"* and let each task's **Files** list define the
   > blast radius. **An interface method signature is a whole-workspace edit whatever the Files list says.**
   > `GroupDialect.AddMember` gains a parameter in Task 5, and:
   >
   > ```
   > $ grep -n "GroupDialect" adapter/database/sql/harness/testkit.go
   > 87:	Group msginsql.GroupDialect
   > $ grep -n "AddMember" adapter/database/sql/harness/groupstore.go
   > 345:			_, err = kit.Group.AddMember(ctx, db, table, key, id, seq, headers, []byte(`"p"`))
   > $ grep -n "harness" adapter/database/sql/dbtest/go.mod
   > 	github.com/kartaladev/msgin/adapter/database/sql/harness v0.0.0
   > 	github.com/kartaladev/msgin/adapter/database/sql/harness => ../harness
   > ```
   >
   > **`harness` and `dbtest` go red on the signature alone.** Revision 2's Tasks 5+6 commit — constructed
   > explicitly *"so no commit is a broken build"* — **was** one, in the two modules audit m-8 had just corrected
   > the plan's module count to include. Worse, **`harness` has no `_test.go` files**, so `go test` there is a
   > false pass; only `go vet`/`go build` catches it (CLAUDE.md; and this plan already says so, in Task 7 Step 5 —
   > the knowledge was in the wrong task). Task 7 Step 1's harness call-site update is therefore **folded into the
   > Tasks 5+6 commit**, and Task 6 Step 5's gate covers **six** modules.
   >
   > **The generalization, stated once so it is not lost a third time:** *a cross-module edit is a red commit and
   > must land with the code that makes it green.* It is true of the class gate (below) **and** of the SPI
   > signature. Before adding any task, ask which modules **compile against** what it changes — not which files it
   > opens.

   > 🔴 **This is why the class-gate edits are distributed rather than deferred (audit B-2).** Half 1 of
   > `sizing_option_class_gate_test.go` is **exact set equality in both directions** — locate it with
   > `grep -n 'assert.Equal(t, want, found' sizing_option_class_gate_test.go`, not a subset check — and it is a
   > **root-module** test that walks the filesystem, so no import boundary shields it. **The moment
   > `memory.WithMaxGroupMembers` exists on disk, root's suite is red.** Revision 1 deferred every gate edit to a
   > final task, which would have left six of nine tasks committing a red suite. In this revision the gate key,
   > the conformance row, **the two exact-map assertions (audit R4-1)** and the executable counts land **inside
   > the commit that adds the option** (Tasks 1 and 5), and "observe the RED first" is a **within-task TDD step** —
   > never a cross-task condition.
   >
   > 🔴 **THERE ARE FOUR EXECUTABLE ASSERTIONS IN THAT FILE, NOT TWO — this is the BLOCKER of audit round 4
   > (R4-1), and it survived all four rounds.** Revisions 1-4 enumerated `require.Len(t, tests, …)` and
   > `require.Equal(t, …, methodCount, …)` and stopped there. Half 2 also holds **two exact-map `require.Equal`s**
   > that this plan named only as a *probe target* (Task 9 Step 7 probe C, branch B9-3) and never as an *edit
   > target*:
   >
   > ```
   > $ grep -nE 'require\.(Len|Equal)\(t, (tests, [0-9]+|[0-9]+, methodCount|wantArms, gotArms|map\[string\]int\{)' \
   >     sizing_option_class_gate_test.go
   > …	require.Equal(t, 27, methodCount, …           ← does NOT move
   > …	require.Len(t, tests, 19,                     ← 19 → 21
   > …	require.Equal(t, wantArms, gotArms,           ← 🔴 wantArms is a 19-ENTRY LITERAL; add both keys
   > …	require.Equal(t, map[string]int{"fixed": 12, "rejects": 1, "safe": 6}, byArm,   ← 🔴 "fixed" 12 → 14
   > ```
   >
   > **Both are `require`, so an unedited map ABORTS the test rather than merely failing an assertion**, and
   > `gotArms` would be a 21-entry map compared against a 19-entry `wantArms`. **Tasks 1 and 5 cannot reach green
   > without editing them**, and Global constraint 8 makes each a green unit before its commit. They **predate
   > Plan 030** (`git log -S 'wantArms' -- sizing_option_class_gate_test.go` → `e473deb`, a Plan 029-era commit),
   > so this is not fallout from a moving tree — **the edit list was never complete.**

9. **Per-task commit is pre-authorized** by CLAUDE.md's plan-execution exception, once this plan is approved **and**
   an execution mode is chosen. `git push`, merges, tags and branch deletion still need explicit per-action
   approval.
10. **Never `git commit --amend` while the controller may be committing** (Plan 028 scar). Run `git log -1`
    immediately before any amend.
11. **Docs links are relative to the CITING file's directory.** A bare `[0033](0033-group-member-bounds.md)` from
    inside `docs/plans/` silently 404s. The pre-merge link gate (CLAUDE.md, both arms) is a Task 10 blocker.
12. 🔴 **GENERATE — do not re-derive by hand — every figure and every coordinate about
    `sizing_option_class_gate_test.go`.** Plan 030 rewrote it (`d2c69fe`) and **Plan 032 has now rewritten it
    again** (`f39725d`, 237 lines). Run the gate and read its output; do not trust a number in this plan.

    > 🔴 **THE RULE CHANGED IN REVISION 5, AND THE REASON IS THAT THE OLD RULE FAILED — audit R4-2, adopted
    > verbatim: *"Re-deriving citations is not the remedy; deriving the edit list from the file is."*** Revision 4
    > did exactly what round 3's NEW-9 asked for — a mechanical sweep of every `file:line` in the bundle — and
    > **the sweep went stale in one commit, falsifying two of its own three results**: `hasIntOrInt64Param` and
    > `isIntOrInt64`, corrected in revision 4 to `:215`/`:231`, are now `:243`/`:259`. **Four consecutive rounds
    > have returned this class, and round 4 is the first in which it returned THROUGH the fix rather than beside
    > it.** A hand-typed list of coordinates against a file another increment is rewriting cannot be kept true by
    > being retyped more carefully. Therefore:
    >
    > 1. **Task 9 Step 3's site table is GENERATED by a script that ships in the task** — the table is *derived
    >    evidence with a timestamp*, not an asserted list. The next increment **reruns it**.
    > 2. **Elsewhere in this bundle, a load-bearing class-gate coordinate is written as the GREP THAT LOCATES IT**
    >    (`grep -n 'require.Len(t, tests' sizing_option_class_gate_test.go`), which cannot go stale, rather than as
    >    a bare `:NNN`.
    > 3. **Where a NUMBER is genuinely needed** (a count, a partition), the tree it was measured on is named **in
    >    the same sentence**.
    >
    > This is the same shape as CLAUDE.md's docs-link gate: *a command whose output is the finding*.

---

## The counted set — read D-AF and D-AO before writing either check

> 🔴 **REVERSED IN REVISION 4 — the two stores now count the SAME SET** (audit **NEW-7**; D-AF). Revision 3 had
> `sql` count **live only**, justified as *"claimed members are retained by the database, not the process."* Both
> halves of that were wrong: `sql.GroupStore.ClaimGroup` decodes the **entire** claimed set into the process heap
> at `limit = 0` (`groupstore.go:285-298`), and — decisively — `ClaimGroup` stamps **every** live member, so live
> drops to 0 and up to `cap` more rows are admitted; a failed release `AbandonGroup`s them all back to live, and
> the cycle repeats. **`SettleGroup` is the only statement that ever deletes a member row, and it runs on success
> only. The durable table was unbounded.** `memory` never had that hole *because* it counts live + claimed.

| Store | Counts | Site | How the count is obtained |
|---|---|---|---|
| `memory.GroupStore` | `len(g.msgs)` — **live + claimed** | `Add`, between the dedup lookup (`groupstore.go:130`) and the dedup insert (`:133`) | the slice length — the process retains exactly that slice |
| `sql.GroupStore` | **live + claimed** — every member row for the key | inside the dialect's transaction, after that engine's serializing statement **and** after the member upsert (Spec 017 §3.6.1) | **the shipped `*CountMembers` helper** — `SELECT count(*) … WHERE group_key = ?`, **no `claimed_epoch` predicate** (`postgres/groupdialect.go:373`, `mysql:358`, `sqlite:375`) |

> 🔴 **THE COUNT SOURCE IS FORCED, NOT A CHOICE — do not compute it from the member `SELECT`.** `len()` of
> `*SelectMembers(…, "claimed_epoch IS NULL", limit)` cannot see claimed members **at all**, so it cannot produce a
> live+claimed count under any `LIMIT`. The three dialects already ship an identical `*CountMembers(ctx, q, mt,
> groupKey) (int64, error)` — called today from `SettleGroup` (`postgres:196`, `mysql:192`, `sqlite:208`) — which
> counts **every** row for the key. **Zero new SQL; one extra `COUNT(*)` per `AddMember`,** inside the transaction
> the dialect already opens. That cost is real, is paid on every add rather than only on overflow, and is stated
> in D-AG rather than discovered.

> 🔴 **AND `sql` THEREFORE GAINS D-AM's LEASED ARM, WHICH IT DID NOT HAVE.** Revision 3 could write *"a `sql` live
> set is by definition unclaimed, so every `sql` over-cap rejection is the not-leased case."* **That sentence is
> false once claimed members count**: a `sql` group can be over cap *because* a claim is in flight, which is
> exactly D-AM's **transient** arm. Classifying it permanent would dead-letter healthy traffic in a routine claim
> window. The discriminator is the group row's **`locked_by`**, read at **zero extra round-trips** because all
> three dialects already read that row inside `AddMember` (Task 6 Step 2). `locked_by IS NULL` is also *exactly*
> §3.3.1's restated premise — *nothing drains an UNLEASED group without an expiry cutoff* — so one predicate
> serves both stores and D-AM loses its `sql` special case.

**🔴 The `memory` check does NOT go "after the dedup branch" — revision 1's instruction lost messages** (audit M-6,
second defect; D-AO). The dedup branch **ends** with `g.ids[id] = struct{}{}` at `groupstore.go:133`; a check
placed after it records the member as *seen* and then rejects it, so the redelivery returns the dedup no-op with a
**nil** error and the source Acks a message that was never appended. The check goes **after the `seen` lookup and
before any mutation**, with the id hoisted so it also runs on the id-less path. **Spec 017 §3.4a carries the exact
shape — read it, do not reconstruct it.**

---

## Task 1 — the `memory` bound, its classification, and `Handle`'s release re-evaluation

**Files:** `adapter/memory/groupstore.go`, `routing/aggregator.go`, `adapter/memory/groupstore_test.go`,
**`routing/aggregator_test.go`**, `sizing_option_class_gate_test.go`.
**Module:** root.

> 🔴 **`routing/aggregator_test.go` is in THIS task's Files (audit N-7).** B1-11 … B1-14 are `Handle` cases, so by
> Global constraint 2 they are `package routing_test` — and revision 2 listed that file only under **Task 2**,
> while Step 8 here already demands *"coverage on `adapter/memory` **and `routing`** ≥ 85%."* The task was asked to
> raise `routing` coverage with no `routing` test file.

> **Why the store and `Handle` land in ONE commit.** Task 1's cap check is what introduces audit **M-6**'s deadlock;
> `Handle`'s snapshot branch is what removes it. Splitting them ships a commit containing a known permanent
> deadlock. Global constraint 8 makes a task a *green* unit; this makes it a *correct* one.
>
> **Why the class-gate pair is in this commit too:** Global constraint 8's box — half 1 goes red the instant the
> option exists.

- [ ] **Step 1.** Load `cc-skills-golang:golang-how-to` (→ `golang-safety`, `golang-error-handling`,
      `golang-design-patterns`, `golang-documentation`) and the `table-test` override. Read `groupstore.go:55-137`
      and `routing/aggregator.go:404-445` with `gopls`, not `grep`, and confirm the anchors this task edits still
      read as Spec 017 §1.2 records them **at `d2c69fe`**: `maxGroupsCeiling` (`:62`), the group-count admission
      arm (`:123-125`), the dedup lookup (`:130`), the dedup insert (`:133`), the append (`:135`), the `leased`
      field (`:43`), and `Handle`'s `store.Add` error return (`aggregator.go:412-415`). **If any has moved,
      re-derive before editing** — Plan 030 already moved them once.
- [ ] **Step 2 (RED — the gate first). FIVE edits, not two** (audit **R4-1**). Locate each by grep, never by line
      number (Global constraint 12):

      | # | Edit | Locate with |
      |---|---|---|
      | 1 | Add `"memory.WithMaxGroupMembers"` to `sizingConformanceKeys` | `grep -n 'sizingConformanceKeys = \[\]string' …` |
      | 2 | Add its conformance row to the **`fixed`** arm | `grep -n 'arm: "fixed"' …` |
      | 3 | `require.Len(t, tests, 19 → 20)` | `grep -n 'require.Len(t, tests' …` |
      | 4 | 🔴 Add `"memory.WithMaxGroupMembers": "fixed"` to the **`wantArms`** literal (19 → 20 entries) | `grep -n 'wantArms := map\[string\]string' …` |
      | 5 | 🔴 Bump the **`byArm`** literal's `"fixed"` entry (12 → 13) | `grep -n 'require.Equal(t, map\[string\]int' …` |

      Run `GOTOOLCHAIN=go1.25.13 go test -run TestSizingOptionClass -v .` and **observe both halves fail** (half 1:
      a key with no function; half 2: a row calling a constructor that does not exist — it will not compile, which
      is the same signal). **`require.Equal(t, 27, methodCount, …)` does NOT move** (audit m-10).

      > 🔴 **EDITS 4 AND 5 ARE NEW IN REVISION 5, AND WITHOUT THEM THIS TASK CANNOT REACH GREEN — the BLOCKER of
      > audit round 4 (R4-1).** Both are **`require`**, so they **abort** rather than merely fail: `gotArms` would
      > be a 20-entry map compared against a 19-entry `wantArms`, and `byArm` `{"fixed": 13, …}` against an
      > asserted `12`. Revisions 1-4 named `wantArms` **only** as Task 9 Step 7's probe target and B9-3's mutation
      > subject, never as something this increment edits — and Task 9 runs **eighth**, seven tasks after this
      > commit first breaks it.
      >
      > 🔴 **DO NOT ADD A `"deferred": 0` KEY to `byArm`.** It is built by **counting**, so an empty arm has no key
      > there; the file's own comment warns that adding one **fails** the assertion.
      >
      > 🔴 **AND THE FAILURE MESSAGE ON EDIT 4 TELLS YOU TO AMEND SPEC 016. That is real, it is authorised, and it
      > is [Task 9b](#task-9b--fold-the-two-new-arm-rows-back-into-spec-016-r4-1) — not something to do here.** The
      > assertion reads *"Moving a row between arms is a SPEC change — update §2.1 and §6 AC-5, do not just edit
      > this map"*, and [Spec 016](../specs/016-sizing-option-bounds.md) is **DELIVERED**. Make the map edit here,
      > because this task must be green; **Task 9b owns the Spec 016 fold-back**, once both new rows exist.

      > 🔴 **The row asserts `1<<30` — but NOT for the reason revisions 1-4 gave** (audit **M-1**, corrected by
      > audit **R4-2**). The old rule *"`fixed` ⇒ `1<<30`"* is **false** since Plan 032: the `fixed` arm now holds
      > **three `int64` rows that keep `1<<62`**, and `1<<30` is **below** their `byteCapCeiling`
      > (`math.MaxInt32 = 2,147,483,647`) so it would be **accepted**. The operative rule is two-dimensional — the
      > **arm** fixes the required property (rejected, with an architecture-independent decimal), and **within a
      > reject arm the PARAMETER TYPE chooses the literal**: `int` ⇒ `1<<30` (decimal **1073741824**), `int64` ⇒
      > `1<<62`. `memory.WithMaxGroupMembers` is `func(n int)`, **so `1<<30` — and `1<<62` would not fit an `int`
      > on `GOARCH=386` and would break compilation of the whole test binary there.** Nothing in this plan's
      > per-task gate builds for 386 (only Task 10 Step 3b does), so get it right here. **Carry the two-dimensional
      > rule, not the one-line one**, into anything you write about the arm.

- [ ] **Step 3 (RED — the behavior).** Write the failing cases of the branch table below in
      `adapter/memory/groupstore_test.go`. All must fail before any production edit.
- [ ] **Step 4 (GREEN — the store).** Add **two NAMED package constants, DECLARED IN
      `adapter/memory/groupstore.go` — the file AC-3.3 parses** (audit **NEW-4**; D-AR) —
      `const defaultMaxGroupMembers = 1 << 16` and `const maxGroupMembersCeiling = 1 << 20` — the `maxGroupMembers`
      config field initialised **from the named default** (`cfg := groupStoreConfig{…, maxGroupMembers:
      defaultMaxGroupMembers}`), `WithMaxGroupMembers(n int)`, the `checkRange` call in `NewGroupStore` (mirroring
      `:105-108`), and the cap check **at Spec 017 §3.4a's exact position** — after the `seen` lookup, before any
      mutation, id hoisted. The check returns **`(liveSnapshot, err)`**, with `err` wrapped in `msgin.Permanent`
      **iff `!g.leased`** (D-AM/D-AN).

      > 🔴 **THE DEFAULT MUST BE A NAMED CONSTANT — do NOT copy the sibling arm's form** (audit **N-4**; D-AR).
      > The shipped precedent one line up is a **bare literal in a composite literal**:
      > `cfg := groupStoreConfig{clock: clockwork.NewRealClock(), maxGroups: 1024}` (`groupstore.go:98`), and
      > revision 2's Step 4 said only *"(default `1 << 16`)"*. **Task 3's AST invariant locates this default BY
      > NAME and fires its not-found guard on a bare literal** — so following local precedent here green-lights
      > Task 1 and blocks Task 3 on a defect Task 1 was never told to avoid. **`maxGroups: 1024` is NOT changed**
      > (nothing reads it mechanically; D-AR scopes the deviation deliberately).
      >
      > 🔴 **AND THE FILE IS AS LOAD-BEARING AS THE FORM** (audit **NEW-4**). AC-3.3's parse set is **asserted**
      > (B3-3 forbids shrinking it), so a constant declared anywhere other than `adapter/memory/groupstore.go`
      > fires the same not-found guard a bare literal would. Revision 3 fixed the *form* and left the *location*
      > unstated — N-4 one attribute over.
- [ ] **Step 5 (GREEN — `Handle`).** Replace `routing/aggregator.go:412-415`'s bare `return err` with Spec 017
      §3.3a's branch: on a non-nil error **with** a non-nil group, re-evaluate `a.cfg.release`, claim and release
      if it fires, and return a **fresh transient** `ErrOverflowDropped` (`overflowRetryable`) when the group
      drained or another holder is releasing it. **A `(nil, err)` return keeps the old path exactly**, so every
      third-party store is unaffected.
- [ ] **Step 6 (GREEN — the class arm).** Upgrade the **existing bare** `return nil, msgin.ErrOverflowDropped`
      (`:124`) to the same wrapped shape (D-AE, fix the class). **It stays TRANSIENT** — the group map drains when
      any group settles, so its retry genuinely can succeed (Spec 017 §5). **First verify no test asserts the bare
      string** — `grep -rn 'message dropped by overflow policy' --include='*_test.go' .` and confirm every hit is
      an `errors.Is` / `require.ErrorIs`, not an `EqualError`.
- [ ] **Step 7 (DOCS).** Godoc per Spec 017 §4 items 1, 2, 5 and 7:
      - **the option** — range, default + its Spec 016 §3.4 provenance, **the permanent/transient classification
        and why** (including that with **neither** sink configured the message is **WARNed and ACKed**, so the
        source drops it — `consumer.go:1049`, `:1073`, audit **N-11**), what it counts, and the claim-window
        rejection named as a busy-wait under `RetryPolicy{}`. 🔴 **For `sql`'s twin (Task 5), the crashed-lease
        bound in that godoc is `2 × leaseTTL`, NOT `leaseTTL` — see the box below** (audit **R4-3**);
      - **both constants** — shaped like `maxGroupsCeiling`'s at `:55-62`. 🔴 **The cross-reference naming the
        `default ≥ completionSizeCeiling` invariant goes on `defaultMaxGroupMembers`, NOT on the ceiling** (audit
        **N-4** — revision 2's Spec §4, this step and Task 3 Step 5 named three different homes). The ceiling is
        not a term of the invariant;
      - **`Add`'s own godoc** at `:112-117`, which today names only the group-count arm;
      - **`Handle`'s snapshot branch** — what a non-nil group beside a non-nil error means, the **downgrade-only**
        direction rule (Spec §3.3a.1), and **why `claim == nil` returns a retryable error here where the success
        path returns `nil` at `aggregator.go:438-439`**. Without that last sentence the divergence reads as a bug
        and gets "fixed."

      > 🔴 **THE CRASHED-LEASE FIGURE IN PUBLIC GODOC WAS UNDERSTATED ~2×, AND A GODOC NUMBER IS AN API CONTRACT**
      > (audit **R4-3**). Revision 4 scheduled *"the claim window's tail is a crashed releaser's lease TTL (default
      > 5m)"* into `sql.WithMaxGroupMembers`'s own godoc. **Two independent terms compose, and revision 4 counted
      > one:**
      >
      > 1. **Eligibility.** A lease stamped at `t₀` becomes reap-eligible at `t₀ + leaseTTL` — `ExpiredGroups`'
      >    first `WHERE` arm is `locked_by IS NOT NULL AND locked_at <= now - leaseTTL`.
      > 2. **Discovery.** The reaper does **not** tick at `t₀ + leaseTTL`. `Aggregator.Run` builds
      >    `clock.NewTicker(reapInterval())` (`routing/aggregator.go:544`) at **`Run`'s own start time**, and with
      >    no `WithGroupTimeout` `reapInterval() == store.RecoverInterval() == leaseTTL`
      >    (`aggregator.go:559-565`; `adapter/database/sql/groupstore.go:349`, `:22` → **5m**). **Discovery is the
      >    first tick at or after `t₀ + leaseTTL`.** Best case `t₀ + leaseTTL`; worst case
      >    **`t₀ + 2·leaseTTL − ε`** — eligibility landing just *after* a tick waits a full further interval.
      >
      > **State the bound as `up to 2 × leaseTTL` — ≈ 10 minutes at the shipped defaults — with that derivation**,
      > and note that it presumes `go agg.Run(ctx)` is running at all (Spec §1.2.1: a durable store makes `Run`
      > **required**; without it the window has no upper bound).
      >
      > 🔴 **And state the real per-iteration cost, not *"a zero-delay busy-wait against the database."*** D-AM's
      > own rejection argument already spells it out (*"each iteration is a full rolled-back `AddMember`
      > transaction plus a `SchemaExists` probe, forever"*). One iteration is: `BEGIN`; the group upsert **taking
      > the group-row X-lock** (on sqlite, `BEGIN IMMEDIATE`'s **database-wide** lock); the member upsert; **the
      > new `COUNT(*)`** (Task 6 Step 2a); the live `SELECT`; `ROLLBACK`; **plus `classifyQueryErr`'s
      > `SchemaExists` probe** (`adapter/database/sql/groupstore.go:91-95`, from `:274`) — **× `WithConcurrency(N)`
      > goroutines, all contending on the very group row the recovery path must lock to drain it.**
      >
      > **The transient disposition is UNCHANGED and correct** (Spec §8 item 9): the retry genuinely succeeds once
      > the reaper drains the group, and classifying an expired lease permanent would dead-letter messages the
      > default configuration is about to admit. **Only the magnitude and the cost were wrong.**
- [ ] **Step 8.** Mutation-prove every case (table below). `GOWORK=off go test ./... -race -shuffle=on` green.
      Coverage on `adapter/memory` and `routing` ≥ 85% and every branch below covered.
- [ ] **Step 9.** Commit: `feat(memory): bound a message group's member count in GroupStore.Add`.

**Hot-path branches introduced, and the case that covers each** (CLAUDE.md's test-coverage gate — a branch with no
test is a delivery blocker):

| # | Branch | Covering case | Killing mutant |
|---|---|---|---|
| B1-1 | `len(g.msgs) >= s.maxGroupMembers` → reject | `Add_rejects_the_cap_plus_one_member` | delete the arm ⇒ case fails |
| B1-2 | the same condition **false** → append proceeds | `Add_admits_members_up_to_the_cap` (cap = 4, four `Add`s all succeed, 4th snapshot has 4 members) | invert to `>` ⇒ the group admits `cap+1`; **this is the one no other case catches** |
| B1-3 | dedup lookup wins at exactly the cap | `Add_readding_an_existing_id_at_the_cap_is_a_noop` | move the cap check **above** the `seen` lookup ⇒ case fails |
| **B1-3b** | **a rejected member leaves NO trace in `g.ids`** | `Add_rejected_member_is_admitted_after_the_group_drains` (Spec 017 AC-1c) | move the cap check **below** the `g.ids` insert ⇒ the member is silently swallowed as a duplicate |
| **B1-3c** | **the cap check runs on the ID-LESS path** | `Add_bounds_an_idless_member` | fold the check back inside `if id != ""` ⇒ unbounded append returns |
| B1-4 | `checkRange` upper arm in `NewGroupStore` | `NewGroupStore_rejects_ceiling_plus_one` | delete the arm ⇒ case fails |
| B1-5 | `checkRange` lower arm | `NewGroupStore_rejects_zero` | change `lo` to `0` ⇒ case fails |
| B1-6 | `checkRange` in-range → default/explicit accepted | `NewGroupStore_accepts_the_ceiling` **and** `NewGroupStore_default_is_usable` | make `checkRange` always error ⇒ both fail |
| B1-7 | the **existing** group-count arm, now wrapped | `Add_rejects_a_new_key_beyond_MaxGroups` (extend the shipped case at `groupstore_test.go:28-41`, whose `name:` is **`:29`** — not `:30-39` as revision 3 cited, audit **NEW-9** — to assert the render) | drop the wrap ⇒ the render assertion fails |
| **B1-8** | **`!g.leased` ⇒ `msgin.Permanent`** | `Add_over_cap_unleased_is_permanent` (asserts `msgin.IsPermanent(err)` **and** the `msgin: permanent: ` render — Spec 017 AC-2c) | drop the wrap ⇒ fails |
| **B1-9** | **`g.leased` ⇒ transient** | `Add_over_cap_while_leased_is_transient` (claim the group first, then `Add`) | wrap unconditionally ⇒ fails |
| **B1-10** | **`Add` returns the live snapshot WITH the error** | `Add_over_cap_returns_the_live_snapshot` | return `nil` ⇒ fails |
| **B1-11** | **`Handle`: non-nil group + non-nil error ⇒ re-evaluate the release** | `Handle_idless_redelivery_re_fires_the_release` (Spec 017 AC-1b) | delete the branch ⇒ M-6's deadlock returns and the case hangs at step 3 |
| **B1-12** | **`Handle`: release fires + drain succeeds ⇒ fresh TRANSIENT error** | same, step 3 asserts `!msgin.IsPermanent(err)`; step 4 asserts the retry is admitted | return the store's permanent error ⇒ step 4 never runs; return `nil` ⇒ the silent-loss assertion fails |
| **B1-13** | **`Handle`: release does NOT fire (`!ok`) ⇒ the store's classification stands** | `Handle_over_cap_unreleasable_stays_permanent` | downgrade unconditionally ⇒ fails |
| **B1-13b** | **`Handle`: the release STRATEGY ERRORED (`rerr != nil`) ⇒ the store's classification stands** | `Handle_over_cap_release_strategy_error_stays_permanent` (a strategy returning `(true, err)`) | drop `rerr != nil` from the `||` ⇒ the group is **claimed and released** despite the strategy's error |
| **B1-13c** | **`Handle`: `ClaimGroup` failed (`cerr != nil`) ⇒ return `cerr`** | `Handle_over_cap_claim_error_is_returned` (a stub store whose `ClaimGroup` errors) | return `err` instead ⇒ the overflow classification masks a store fault and the case's `ErrorIs` on the claim error fails |
| **B1-13d** | **`Handle`: `claim == nil` (another holder) ⇒ fresh TRANSIENT error — a DELIBERATE divergence from `aggregator.go:438-439`, which returns `nil`** | `Handle_over_cap_claim_taken_by_another_holder_is_transient` | return `nil` ⇒ the member is silently lost and no other case notices |
| **B1-13e** | **`Handle`: `relErr != nil` ⇒ return the RELEASE error, not the overflow error** | `Handle_over_cap_release_failure_returns_the_release_error` | return the overflow error ⇒ the message assertion fails (an operator would be pointed at the cap, not the output channel) |
| **B1-14** | **`Handle`: `group == nil` ⇒ unchanged path** | `Handle_store_error_without_snapshot_is_returned_verbatim` (a stub store returning `(nil, err)`) | drop the nil guard ⇒ nil-deref panic |

> 🔴 **B1-13b … B1-13e are NEW in revision 3 (audit N-7).** Spec 017 §3.3a's branch has **six** early returns;
> revision 2's table named four. [CLAUDE.md](../../CLAUDE.md)'s test-coverage gate makes **every** early-return on
> the hot path a delivery blocker, and two of these are not bookkeeping: **B1-13b** guards against
> claim-and-releasing a group the strategy *rejected*, and **B1-13d** is a **deliberate divergence** from the
> success path that revision 2 neither tested nor documented. All six exits are tabulated in Spec 017 §3.3a.1.

**Also asserted (AC-2b, both ends):** the full construction-time render, not merely `errors.Is` —
`"msgin: capacity out of range: memory.WithMaxGroupMembers: 1048577 not in [1, 1048576]"` and the `0` twin.

**Evidence block to record:** the RED gate output (both halves), the RED behavioral failures, **one killed mutant
per row of the branch table** (count the rows in the table above — do not transcribe a number from here), and
`go test -cover` for `adapter/memory` and `routing` before and after.

**Sizing note:** this is the **largest** task in the plan — it carries the store, the aggregator branch, the
classification and the gate pair. If it needs splitting, the only safe seam is *"store + `Handle` + gate"* as one
commit and *"the wrapped group-count class fix"* (Step 6) as a follow-up; **do not** split store from `Handle`.

---

## Task 2 — the bound holds for ALL FOUR release paths (the increment's reason to exist)

**Files:** `routing/aggregator_test.go` (new cases; blackbox `package routing_test`).
**Module:** root.

> **This is Spec 017 AC-1, and it is the task that distinguishes this increment from Plan 029.** A test that
> exercises only `WithMaxGroupMembers` in isolation passes against an implementation that bounds nothing new —
> path 1 was already bounded.

- [ ] **Step 1.** Load the skills (Global constraint 1) + `table-test`. Read `routing/aggregator.go:100-160` and
      `:404-445` with `gopls`.
- [ ] **Step 2 (RED).** One table, four cases, over a `memory.GroupStore` built with `WithMaxGroupMembers(4)`,
      each configuring a different release path and each asserting that `Handle`'s 5th message returns an error
      satisfying `errors.Is(err, msgin.ErrOverflowDropped)` **and** `msgin.IsPermanent(err)` (the group is not
      leased in any of them — Spec 017 §3.3.1):

  | Case | Aggregator configuration |
  |---|---|
  | `completion_size_above_the_cap` | `WithCompletionSize(1000)` |
  | `release_strategy_never_releases` | `WithReleaseStrategy(func(msgin.MessageGroup) (bool, error) { return false, nil })` |
  | `release_when_never_releases` | `WithReleaseWhen(func(msgin.MessageGroup) bool { return false })` |
  | `default_path_header_driven` | **no** release option; the first message carries `msgin.HeaderSequenceSize = 1000` |

      **Killing mutant for the `IsPermanent` half:** drop the `msgin.Permanent` wrap ⇒ all four fail. Without it
      these four cases pass against revision 1's hot-spinning implementation, which is exactly how B-1 survived.
- [ ] **Step 3 (AC-1b — the id-less deadlock case).** Spec 017 AC-1b, in full: four **id-less** members, a release
      that fails once then succeeds, then re-`Handle` the same message and assert the release **re-fires**, the
      error is transient and names `routing.Aggregator.Handle`, and a further retry is **admitted**. **This is the
      case that would have caught audit M-6 and no other case does.**

      > 🔴 **BUILD THE FIXTURE WITH `msgin.NewMessage`, AND ASSERT IT FIRST** (audit **N-6**). `msgin.New`
      > **always** stamps an id — `if cfg.id == "" { cfg.id = NewID() }` (`message.go:178-180`) — so even
      > `msgin.New(p, msgin.WithID(""))` yields an id-**ful** message. The only id-less route is
      > **`msgin.NewMessage(payload, headers)`** (`message.go:198`) with headers carrying **no
      > `HeaderMessageID`**. **`require.Empty(t, m.ID())` is the case's FIRST assertion**, before any `Handle`
      > call.
      >
      > **Why this is not pedantry:** with an id-ful fixture the dedup branch
      > (`adapter/memory/groupstore.go:130-131`) returns the snapshot with a **nil** error, `Handle` reaches the
      > predicate anyway, **M-6's deadlock is never entered — and the case passes**. Worse, **Task 1's B1-3c mutant
      > survives**: folding the cap check back inside `if id != ""` changes nothing for an id-ful message, so the
      > case reports a green run against the mutant it exists to kill. *A killed mutant is the evidence — and a
      > mutant can only be killed by the right fixture.*
- [ ] **Step 4 (AC-1c — the dedup-set case).** `Add` a fifth id-ful member at the cap ⇒ rejected; drain the group;
      re-`Add` the same id ⇒ **admitted**, not swallowed as a duplicate.
- [ ] **Step 5 (AC-3.1 — boundary arithmetic, both directions).** `WithMaxGroupMembers(4)` +
      `WithCompletionSize(4)` over 5 messages ⇒ the **4th** `Handle` releases (observed via the output channel's
      subscriber) and the 5th starts a fresh group; `WithMaxGroupMembers(4)` + `WithCompletionSize(5)` ⇒ the
      **5th** returns `ErrOverflowDropped` and **nothing** is released.
- [ ] **Step 6 (AC-3.2 — ceiling-level, constructor-only).**
      `routing.NewAggregator(..., WithCompletionSize(1<<16))`, `memory.NewGroupStore()` and
      `memory.NewGroupStore(WithMaxGroupMembers(1<<16))` all construct without error. **No members are added.**
- [ ] **Step 7.** Mutation-prove; green; commit:
      `test(routing): prove the store bound holds for all four release paths`.

> 🔴 **Fixture, measured — five parts, not two** (Spec 016 §6 AC-5's finding, re-verified against
> `aggregator.go:340-370`). `NewAggregator` needs `store`, `fn`, **`WithOutputChannel(ch)`** *and*
> **`WithCorrelationStrategy(fixedKey)`**, plus **`ch.Subscribe(counter)`** for release to be observable at all.
> A bare `NewAggregator(store, fn)` returns `msgin: aggregator output channel is nil`, and the default correlator
> returns `Permanent(msgin.ErrNoCorrelation)` for a message with no correlation header. **Write the fixture once as
> a helper in the test file; every case above reuses it.** AC-1b additionally needs an output channel whose `Send`
> fails exactly once — extend the helper, do not fork it.

| # | Branch | Covering case | Killing mutant |
|---|---|---|---|
| B2-1 | `Handle` propagates `store.Add`'s error when there is no snapshot | Step 2's four cases + `Handle_store_error_without_snapshot` | swallow the error (`return nil`) ⇒ all fail |
| B2-2 | release fires at exactly the cap | Step 5's `completion_size_equals_cap` case | move the memory cap check **after** the append ⇒ the 5th admits and "nothing released" flips |

---

## Task 3 — the `default ≥ completionSizeCeiling` invariant, mechanically enforced (D-AQ)

> 🔴 **EXECUTION ORDER: THIS TASK RUNS AFTER TASKS 5+6, NOT THIRD.** It keeps the number 3 (renumbering would
> churn every cross-reference in three artifacts for no gain); it does **not** keep the position. The plan's
> execution order is **1 → 2 → 4 → 5+6 → 3 → 7 → 8 → 9 → 9b → 10** — see the Sizing table, which is ordered that
> way.
> **Step 0 below is a hard gate that fails loudly if you arrive here early.**

**Files:** a new root blackbox test (`group_member_bound_invariant_test.go`), plus the cross-reference comments on
`routing/aggregator.go:33` and **each store's new `defaultMaxGroupMembers` constant** (`adapter/memory/groupstore.go`
**and `adapter/database/sql/groupstore.go`**).
**Module:** root.

> 🔴 **THE TASK IS REORDERED, NOT SPLIT — this is the BLOCKER of audit round 3 (NEW-1), and it REVERSES revision
> 3's instruction** (**D-AT**). Revision 3 offered *"or split it: the `memory` + `routing` assertions with Task 1
> and the `sql` assertion with Task 5"*, justified from N-1's rule — *a gate reading a declaration must ship in
> that declaration's commit*. **That reads N-1's shape and not its mechanism.**
>
> - **N-1/B-2's rule governs a PRE-EXISTING gate.** `sizing_option_class_gate_test.go` ships on `main` and asserts
>   **exact set equality in both directions** (`grep -n 'assert.Equal(t, want, found' …`), so **the option's
>   existence alone makes root red** before one line of new test code exists. The gate is the fixed point; the key
>   must ride in the option's commit because there is no order that avoids a red commit otherwise.
> - **Task 3's AST test DOES NOT EXIST YET.** Adding `const defaultMaxGroupMembers` makes **nothing** red. The
>   *test* depends on the *declarations*, so it must ship **no earlier than the last declaration**. That is a
>   **reorder** constraint, not a co-commit constraint.
>
> **And the split is strictly worse in four independent ways**, which is why the alternative is deleted rather
> than deprecated:
>
> | # | What the split breaks |
> |---|---|
> | 1 | **B3-3's mutant is unprovable in the first half.** `sql`'s constant legitimately does not exist yet, so the asserted file list must be written as **two** files and then rewritten to **three** — a file list edited to match what happens to exist, which is exactly what B3-3 forbids |
> | 2 | **Step 4's mandatory non-vacuity probe runs twice**, giving the second half a standing excuse to skip it |
> | 3 | **Step 2's RED becomes impossible.** Pre-Task-1 there is no constant, so what fires is the **not-found guard** — a different RED, proving the guard rather than the value read. The two probes collapse and the weaker one wins |
> | 4 | **Three references dangle** — Task 10 Step 5, the Sizing table row, and Step 6's single commit message |
>
> **Do not** weaken the parse set to whatever exists — mutant (c) below exists to forbid exactly that.

> **NEW in revision 2 (audit M-5).** Revision 1 recorded this invariant as *"not mechanically enforced — both
> constants are unexported and in different packages, so no blackbox test can compare them"* and defended it with
> two prose comments and a one-time `grep`. **That claim is false**: `sizing_option_class_gate_test.go` is a root
> blackbox test that already parses every non-test `.go` file in all eight modules with `go/parser`
> (`grep -n 'parser.ParseFile' sizing_option_class_gate_test.go`), and **unexportedness and package boundaries are
> irrelevant to a parser.**

- [ ] **Step 0 (THE ORDER GATE — audit NEW-1, D-AT; the ASSERTION corrected per audit R4-4).** Confirm all
      **three** declarations are on the branch before writing a line. **Assert the CONDITION, per declaration —
      never a count:**

      ```bash
      grep -q "^const defaultMaxGroupMembers" adapter/memory/groupstore.go       || { echo "STOP: Task 1 has not landed";    exit 1; }
      grep -q "^const defaultMaxGroupMembers" adapter/database/sql/groupstore.go || { echo "STOP: Tasks 5+6 have not landed"; exit 1; }
      grep -q "completionSizeCeiling"         routing/aggregator.go              || { echo "STOP: shipped constant missing";  exit 1; }
      ```

      Do not shorten the parse set to match what exists; that is the defect B3-3 exists to catch.

      > 🔴 **REVISION 4'S GATE COUNTED, AND IT STOPPED ON A CORRECT TREE** (audit **R4-4**). It read: *"`grep -n
      > "defaultMaxGroupMembers" <two files>` must return **two** hits … If any is missing, Tasks 1 and/or 5+6 have
      > not landed — STOP."* **On a correct tree it returns at least four, and most likely six.** Step 4 of both
      > Task 1 and Task 5 puts **two** occurrences in the same file — the `const` declaration **and** the
      > initialiser reference `maxGroupMembers: defaultMaxGroupMembers` — and Step 7's mandated godoc shape
      > (*"shaped like `maxGroupsCeiling`'s"*) adds a **third**, because a Go doc comment **begins with the
      > identifier it documents** (`// maxGroupsCeiling is the upper bound …`, `adapter/memory/groupstore.go:55`).
      > Task 3 Step 5's cross-reference comment may add more.
      >
      > **So the gate installed to enforce D-AT halted on the tree D-AT describes, and sent the implementer back to
      > re-run tasks that had already landed correctly. A gate whose only failure mode is a false positive is worse
      > than no gate** — the first person to hit it deletes it. The defect is the assertion's **shape**: a count is
      > a proxy for the condition, and the condition is *"each of these three declarations exists."* **This is the
      > project's stored lesson *"assert the partition, not just the rows"*, and *"fix the class, not the
      > instance."***
      >
      > **ADR D-AT already described the correct gate** — *"greps for all three declarations"* — and this plan did
      > not implement it. Two-of-three, inverted: the ADR had the rule and the plan had the defective instance.
- [ ] **Step 1.** Skills + `table-test`. Read `scanSizingParamRepo` in `sizing_option_class_gate_test.go`
      (`grep -n 'func scanSizingParamRepo' …`) with `gopls` — it is the model; **do not** import from it, write a
      focused parse.
- [ ] **Step 2 (RED — a VALUE failure, not a not-found failure).** With all three constants present (Step 0),
      write the test with a **deliberately wrong expectation** — assert `defaultMaxGroupMembers < completionSizeCeiling`
      — and confirm the failure message **names all three constants, their files and their real values**. That is
      what proves it reads the AST rather than passing vacuously. Then invert the assertion to the real one.

      > 🔴 **Revision 3 said *"write the test against the PRE-TASK-1 tree"*, which is now impossible and was
      > always the wrong probe** (audit **NEW-1**, consequence 3). Pre-Task-1 there is no constant at all, so what
      > fires is Step 4's **not-found guard** — a different failure that proves the guard, not the value read.
      > **Step 2 and Step 4 are two distinct probes and both are mandatory.**
- [ ] **Step 3 (GREEN).** The test parses **three** files — `routing/aggregator.go`,
      `adapter/memory/groupstore.go` **and `adapter/database/sql/groupstore.go`** — locates
      `const completionSizeCeiling` and **`const defaultMaxGroupMembers` in EACH store package** by name, evaluates
      the `1 << N` `*ast.BinaryExpr` values, and asserts `defaultMaxGroupMembers >= completionSizeCeiling` **for
      both stores**. The failure message names the constants, their files and their values.

      > 🔴 **BOTH STORES, NOT JUST `memory`** (audit **N-4**). `sql` takes the **same** default under the **same**
      > Aggregator with the **same** `WithCompletionSize`, so it carries the **identical** silent-deadlock risk.
      > Covering one store while the other carries the same risk is this increment's own *"fix the class, not the
      > instance"* lesson violated inside the fix for M-5.

- [ ] **Step 4 (NON-VACUITY — mandatory).** The test **must fail loudly if ANY of the three declarations is not
      found**, never pass on a zero value. Prove it: rename one constant locally, confirm the not-found guard fires
      (not a `0 >= 0` pass), and revert. *(The project's stored lesson: "measurement is only as good as its
      fixture".)* **The file list is asserted, not discovered** — a missing file is a failure, not a shorter run.
- [ ] **Step 5.** Add the cross-reference comment to **each `defaultMaxGroupMembers`** (naming
      `routing.completionSizeCeiling`, the other store's twin, and this test) and to `completionSizeCeiling`
      (naming both defaults and this test). 🔴 **Not on the CEILING constants** — `maxGroupMembersCeiling` is not a
      term of the invariant, and revision 2's three artifacts named three different homes (audit **N-4**). They are
      now human-facing explanation, **not** the defence.
- [ ] **Step 6.** Mutation-prove; green; commit **after the Tasks 5+6 commit** (Step 0's order gate; **D-AT**) —
      **one** commit, not two: `test(core): enforce the group-member default against the completion-size ceiling`.

| # | Branch | Covering case | Killing mutant |
|---|---|---|---|
| B3-1 | the relation holds, per store | the two assertions | change any of the three literals ⇒ fails |
| B3-2 | a constant is missing/renamed | the not-found guard | delete the guard ⇒ a renamed constant yields a vacuous `0 >= 0` pass |
| **B3-3** | **the parse set is complete** | the file-list assertion | drop `adapter/database/sql/groupstore.go` from the set ⇒ **must fail**, not shrink to one assertion |

---

## Task 4 — godoc cross-references on the three unbounded release paths (D-AI)

**Files:** `routing/aggregator.go` (godoc only), `message.go` (where `msgin.HeaderSequenceSize` is declared, `:24`),
`routing/example_aggregator_test.go` (one runnable example).
**Module:** root.

> **The project's stored lesson applies directly here:** *"docs can contradict the code they describe — all three
> fix rounds in Plan 028 were godoc, not logic."* **Read each sentence against the constructor, not for
> plausibility.** Round 1 found the same pattern again in this bundle's own prose.

- [ ] **Step 1.** Skills + `golang-documentation`. Confirm with `gopls` that `msgin.HeaderSequenceSize` is still at
      `message.go:24` and `defaultRelease` still reads it at `aggregator.go:227`. *(Revision 1 hedged with
      "`headers.go` or wherever" — audit m-7. It is `message.go:24`.)*
- [ ] **Step 2.** Edit five godocs per Spec 017 §3.8 / §4 item 4:
      `WithCompletionSize` (`:154` — pointer to the store bound), `WithReleaseStrategy` (`:116` — **it bypasses
      `completionSizeCeiling`; the store bound is what stops the group**), `WithReleaseWhen` (`:128` — same,
      inherited), and — since `defaultRelease` (`:222`) is unexported — `msgin.HeaderSequenceSize`
      (`message.go:24`) **and** `routing.NewAggregator` (`aggregator.go:327`).
- [ ] **Step 3 (RUNNABLE COMPANION).** Add one `Example` showing a `WithReleaseWhen` strategy that never releases,
      over a small `WithMaxGroupMembers`, producing the `ErrOverflowDropped` outcome — so the prose has a
      compilable proof next to it and the doc edit is not orphaned from a test.

      > **File name, corrected (audit m-7):** `routing/example_test.go` **does not exist**. The package convention
      > is `example_<subject>_test.go` (`example_aggregator_test.go`, `example_predicate_test.go`,
      > `example_splitter_test.go`). Extend `example_aggregator_test.go`.
- [ ] **Step 4.** `go test -run '^Example' ./...` green; `go vet ./...`; `gofmt -l .` empty.
- [ ] **Step 5.** Commit: `docs(routing): cross-reference the store-level group member bound`.

**No new logic branch.** The example's own path is covered by Task 1's B1-11/B1-12 and Task 2's B2-1.

---

## Task 5 — `sql.WithMaxGroupMembers`, `checkRange`, and the `AddMember` signature

**Files:** `adapter/database/sql/{groupstore.go,groupdialect.go,helpers.go,groupdialect_fake_test.go,
groupstore_unit_test.go}`, `sizing_option_class_gate_test.go`, **`adapter/database/sql/harness/groupstore.go`**
(the call site only — see the box).
**Modules:** root, **`harness`** (and `dbtest` recompiles).

> **This task changes an exported SPI method signature.** `GroupDialect.AddMember` gains a trailing
> `maxMembers int`. Global constraint 5 forbids new exported **sentinels**, not this — the change is explicitly
> ratified by **D-AG**, and `GroupDialect`'s own godoc (`groupdialect.go:106`) reserves the right
> (*"a pre-1.0 (v0) contract that may still evolve"*). **This task leaves the three dialect modules RED**; Task 6
> makes them green. **Land Tasks 5 and 6 as ONE commit** so no commit is a broken build (Global constraint 8) —
> Task 5's steps are separated here for review clarity, not for commit boundaries.
>
> 🔴 **THE HARNESS CALL SITE LANDS IN THAT SAME COMMIT — this is the BLOCKER of audit round 2 (N-1).** Revision 2
> scheduled it in **Task 7**, so the Tasks 5+6 commit shipped **`harness` and `dbtest` non-compiling**:
> `harness/testkit.go:87` stores a `msginsql.GroupDialect` and `harness/groupstore.go:345` calls the **8-argument**
> `AddMember`; `dbtest/go.mod` requires `harness` with a local `replace`. **The mechanical signature update is
> Step 6b of this task.** Task 7 keeps its **behavioral** conformance cases, which is what it is for.
>
> **The name is `sql.WithMaxGroupMembers`, matching `memory`** (D-AD, audit m-9) — see the box after Step 1.

> **The name is `sql.WithMaxGroupMembers`, matching `memory`** (D-AD, audit m-9). `adapter/database/sql`'s
> `WithGroup…` prefix is a **collision rule**, not a blanket convention; `MaxGroupMembers` collides with nothing.
> The rule is **proven in `adapter/memory`** (`WithMaxGroups` unprefixed and uncolliding beside `WithGroupClock`
> prefixed and colliding) and merely **consistent with `sql`**, whose two `GroupStoreOption`s both happen to
> collide and so cannot discriminate it (audit **N-10**). Either way nothing forbids the name. **Do not rename it.**

- [ ] **Step 1.** Skills + `golang-database`. With `gopls`' find-references, enumerate **every** `AddMember` site
      rather than trusting this plan's list. Expected **seven** (audit m-5 — revision 1 said five and omitted the
      two that matter most):

      | # | Site | Kind |
      |---|---|---|
      | 1 | `adapter/database/sql/groupdialect.go:126` | **interface declaration** |
      | 2 | `adapter/database/sql/groupstore.go:272` | **production call** — threads `s.maxGroupMembers` |
      | 3–5 | `postgres/groupdialect.go:80`, `mysql/groupdialect.go:75`, `sqlite/groupdialect.go:102` | implementations (Task 6, **same commit**) |
      | 6 | `harness/groupstore.go:345` | test-kit call — **Step 6b of THIS task, same commit** (audit **N-1**; revision 2 deferred it to Task 7 and shipped a red `harness`) |
      | 7 | `groupdialect_fake_test.go:137` | test fake |

      **Sites 3-6 are in other modules.** Global constraint 8's *compiles-against* rule governs: all of them, plus
      `dbtest`, must be green before the Tasks 5+6 commit.

- [ ] **Step 2 (RED — the gate first). THE SAME FIVE EDITS Task 1 Step 2 makes** (audit **R4-1**) — locate each by
      grep, never by line number (Global constraint 12):

      | # | Edit |
      |---|---|
      | 1 | Add `"sql.WithMaxGroupMembers"` to `sizingConformanceKeys` |
      | 2 | Add its conformance row to the **`fixed`** arm, asserting **`1<<30`** — it is `func(n int)`; see Task 1 Step 2's two-dimensional box |
      | 3 | `require.Len(t, tests, 20 → 21)` |
      | 4 | 🔴 Add `"sql.WithMaxGroupMembers": "fixed"` to the **`wantArms`** literal (20 → 21 entries) |
      | 5 | 🔴 Bump the **`byArm`** literal's `"fixed"` entry (13 → **14**) |

      Observe both halves fail. **`methodCount` still does not move** — the dialect `AddMember` methods already
      match on `seq int64` (audit m-10). **After this task the partition is `14 fixed + 1 rejects + 0 deferred +
      6 safe = 21` — and `byArm` still carries NO `"deferred"` key** (concurrent-work box, consequence 3).
      🔴 **Edits 4 and 5 are `require`; without them this task cannot reach green** (audit **R4-1**).
- [ ] **Step 3 (RED — the behavior).** Failing cases for `sql.NewGroupStore(WithMaxGroupMembers(...))` at `0`,
      `1<<20` and `1<<20+1`, asserting the full `checkRange` render (AC-2b).
- [ ] **Step 4 (GREEN — the option).** Add `checkRange` to `adapter/database/sql/helpers.go` — a **fifth,
      independent, unexported `int` copy**, identical to `adapter/memory/helpers.go:54`, carrying the same
      ADR 0031 **D-R** / Spec 014 §3.3 provenance comment the other four carry (`endpoint/helpers.go:97`,
      `routing/helpers.go:88`, `adapter/memory/helpers.go:54`, **`adapter/http/helpers.go:73`**). Add

      > 🔴 **THE INVENTORY WAS STALE IN BOTH THE NUMBER AND THE COORDINATE** (audit **R4-5**). Revisions 1-4 said
      > *"four copies"* with `adapter/http`'s at `:64`. [Plan 032](032-byte-cap-ceilings.md) moved it to **`:73`**
      > and added an `int64` twin, so the loose grep now returns **five**:
      >
      > ```
      > $ grep -rn 'func checkRange' --include='*.go' . | wc -l     # → 5 (one is checkRangeInt64)
      > $ grep -rn 'func checkRange(' --include='*.go' . | wc -l    # → 4 — the int copies this step models on
      > adapter/memory/helpers.go:54  adapter/http/helpers.go:73  routing/helpers.go:88  endpoint/helpers.go:97
      > ```
      >
      > **`adapter/http/helpers.go:115` is `checkRangeInt64`, a DIFFERENT helper** — the `int64` twin Plan 032's
      > byte caps needed. **This step adds an `int` copy**, modelled on `adapter/memory/helpers.go:54`, which is
      > unchanged. Use the **qualified** grep so the stated command and the stated number agree; the loose one made
      > revision 4 assert *"four"* against a command that printed five.

      **two NAMED package constants** — `const defaultMaxGroupMembers = 1 << 16` and
      `const maxGroupMembersCeiling = 1 << 20` (**D-AR**, audit **N-4**; Task 3's AST invariant parses **this
      package too** and locates the default **by name** — see Task 1 Step 4's box) — the config field initialised
      from the named default, and `WithMaxGroupMembers(n int)` with the `checkRange` call in `NewGroupStore`.

      > 🔴 **THE CONSTANTS GO IN `adapter/database/sql/groupstore.go`, NOT IN `helpers.go`** (audit **NEW-4**).
      > This step's own subject is `helpers.go`, and `checkRange`'s range arms are the constants' natural
      > neighbours — so the obvious reading puts them there and **fires AC-3.3's not-found guard**, because the
      > parse set is **asserted** (B3-3) at `routing/aggregator.go` + `adapter/memory/groupstore.go` +
      > **`adapter/database/sql/groupstore.go`**. That file already declares this package's other two defaults as
      > named constants — `defaultGroupLeaseTTL` (`:22`) and `defaultExpiredGroupsLimit` (`:30`) — so it is also
      > the local-precedent home. `checkRange` still goes in `helpers.go`; only the constants move.

      🔴 **`sql.WithMaxGroupMembers`'s OWN GODOC carries the `sql`-specific clauses of Spec §4 item 1** — the
      unconditional-durability contrapositive (§3.6.2), the instances-must-agree operator requirement (§7.1
      item 3), and **the crashed-lease window stated as `up to 2 × leaseTTL` ≈ 10 minutes at the defaults, with
      Task 1 Step 7's two-term derivation** (audit **R4-3**). **Do not write *"up to a lease TTL"*** — that is the
      figure round 4 found understated ~2×, and it was scheduled into public godoc.
- [ ] **Step 5 (GREEN — the SPI godoc: one CORRECTION, two additions).** Add `maxMembers int` to
      `GroupDialect.AddMember`, then edit its interface godoc.

      > 🔴 **FIRST, CORRECT WHAT IS ALREADY THERE — this is not an append-only edit** (audit **N-9**).
      > `adapter/database/sql/groupdialect.go:109-113` still reads *"takes the **GROUP ROW LOCK** (SELECT ... FOR
      > UPDATE or equivalent) BEFORE reading or writing any member row"* — **the exact claim audit M-3 falsified
      > for sqlite**, which takes no row lock at all. Revision 2 corrected the bundle's prose (Spec §3.6.1, D-AP)
      > and left the shipped godoc — the one a third-party dialect author reads — asserting the falsified
      > mechanism, because this step said only to *add*. Replace it with: *"serializes concurrent same-key adds —
      > by a group-row lock on postgres/mysql, by `BEGIN IMMEDIATE`'s database-wide write lock on sqlite (D-AP)."*

      Then **add**: (a) the in-transaction enforcement contract and the `msgin.ErrOverflowDropped` rollback;
      (b) **D-AP's caller-owned-transaction precondition** — enforced by rollback only when the dialect owns the
      transaction; a **direct dialect caller** supplying a `*sql.Tx` owns the rollback.

      > 🔴 **This godoc is that precondition's ONLY home** (audit **N-2**). Revision 2 also put it on
      > `sql.WithMaxGroupMembers`, where it can never apply: `NewGroupStore` takes a concrete `*stdsql.DB`
      > (`groupstore.go:212`), `groupBase.db` is a concrete `*stdsql.DB` (`:40-42`), `:272` always passes `s.db`,
      > and `WithSharedTransaction` returns an **`Option`**, not a `GroupStoreOption` (`options.go:201`). **The
      > option's godoc states the opposite, which is the true statement for its reader:** *"For a store built by
      > `NewGroupStore` this bound is unconditionally durable — the store always owns the transaction the dialect
      > runs in."*

      Finally, thread `s.maxGroupMembers` through `sql.GroupStore.Add` (`:272`), and **propagate the dialect's
      snapshot past `classifyQueryErr`** rather than discarding it with the current `return nil, …` (`:274`).
- [ ] **Step 6.** Extend `fakeGroupDialect` to record the `maxMembers` it received, to return the overflow error on
      demand, **and to return rows alongside that error**, so the store's pass-through of both is provable without
      Docker.
- [ ] **Step 6b (THE HARNESS CALL SITE — audit N-1, the BLOCKER of round 2).** Update `harness/groupstore.go:345`
      to the new signature, passing a **new unexported package constant declared in
      `adapter/database/sql/harness/groupstore.go`**:

      ```go
      // groupMemberCap is the member cap every harness group case runs under: small
      // enough for Spec 017 AC-6 (no test grows a group past 16), large enough for
      // the largest member count any harness case adds. It is UNEXPORTED on purpose
      // — see the box in Task 7 Step 5.
      const groupMemberCap = 4
      ```

      **Mechanical only — the behavioral conformance cases stay in Task 7**, and Task 7's new cases use the same
      constant.

      > 🔴 **REVISION 3 SAID *"threading the cap from the existing `TestKit`"*, WHICH HAS NO REFERENT** (audit
      > **NEW-3**). `harness.TestKit` has **no cap, limit or integer field of any kind** —
      > `grep -nE "^\s+[A-Z][A-Za-z]*\s+(int|int64)\b" adapter/database/sql/harness/testkit.go` returns nothing —
      > and **`testkit.go` is in no task's Files list**, so no task is authorized to add one. The implementer was
      > left with three options, one of which (an exported `int` parameter) the very next box **forbids**.
      >
      > 🔴 **Do NOT add an exported function with an `int`/`int64` parameter to `harness`**, and do **not** add a
      > `TestKit` field either (Task 7 Step 5's box): an exported key from a leaf module is an unsatisfiable class-
      > gate failure by design, and an exported struct field is a public-surface change to a leaf module — an
      > architectural decision, not an implementation detail. An **unexported package constant is neither.**
      > *(If a `TestKit` field is nevertheless preferred, `testkit.go` must join this task's Files list and ADR
      > 0033 must record that `harness` gains an exported field.)*

      Verify **`GOWORK=off go vet ./...` in `harness`** — it has **no `_test.go` files**, so `go test` there is a
      false pass — and **`GOWORK=off go vet ./...` in `dbtest`** — where every Go file **is** a `_test.go` file, so
      `go build` is the false pass (audit **NEW-2**; Task 6 Step 5's table).
- [ ] **Step 7.** Mutation-prove; commit **with Task 6** (below).

| # | Branch | Covering case | Killing mutant |
|---|---|---|---|
| B5-1 | `checkRange` upper arm in `sql.NewGroupStore` | `NewGroupStore_rejects_ceiling_plus_one` | delete ⇒ fails |
| B5-2 | `checkRange` lower arm | `NewGroupStore_rejects_zero` | `lo` → `0` ⇒ fails |
| B5-3 | in-range → accepted | `NewGroupStore_accepts_the_ceiling` | always-error ⇒ fails |
| B5-4 | `Add` passes the configured cap to the dialect | `Add_passes_maxMembers_to_the_dialect` (fake records it) | pass a literal `0`/`math.MaxInt` ⇒ fails |
| B5-5 | `Add` propagates the dialect's overflow error unchanged | `Add_propagates_dialect_overflow` (fake returns it) | strip the `Permanent` marker ⇒ the `IsPermanent` assertion fails |
| **B5-6** | **`Add` propagates the dialect's SNAPSHOT alongside the error** | `Add_propagates_dialect_overflow_snapshot` (fake returns rows + error) | keep `return nil, …` ⇒ fails, and Task 1's `Handle` branch is unreachable for `sql` |
| **B5-7** | **the marker and sentinel survive `classifyQueryErr`** | `Add_overflow_survives_schema_classification` (Spec 017 AC-4c, fake dialect) | have `classifyQueryErr` return `schemaNotReady()` unconditionally ⇒ fails |

---

## Task 6 — in-transaction enforcement in postgres, mysql and sqlite (D-AG, D-AP)

**Files:** `adapter/database/sql/{postgres,mysql,sqlite}/groupdialect.go`.
**Modules:** `postgres`, `mysql`, `sqlite`.

> 🔴 **THE PLACEMENT IS PER-DIALECT. Revision 1's single instruction — "after the statement that takes the group
> row lock … the existing `RunInTx` wrapper rolls back" — has NO REFERENT in sqlite** (audit M-3). Read Spec 017
> §3.6.1 before touching any file.

- [ ] **Step 1.** Skills + `golang-database` + **`use-testcontainers`** (no mocks, no in-memory fakes, no shared
      dev DB for this work). Read each dialect's `AddMember` with `gopls`.
- [ ] **Step 2 (GREEN, per dialect).** Inside the existing transaction, **after that engine's serializing statement
      and after the member upsert**, count **every member row for the key** — live **and** claimed — and, if the
      count exceeds `maxMembers`, return the D-AE/D-AM error so the transaction rolls back:

      | Dialect | Wrapper | Serializing statement | Where the check goes |
      |---|---|---|---|
      | **postgres** | `pgRunInTx` (`groupdialect.go:52`) | `INSERT … ON CONFLICT (group_key) DO UPDATE … RETURNING created_at` (`:107-110`) — locks the conflicting row | after that upsert **and** after the member upsert |
      | **mysql** | `mysqlRunInTx` (`groupdialect.go:48`) | `INSERT … ON DUPLICATE KEY UPDATE group_key = group_key` (`:93-96`) — X-locks the group row | after that upsert **and** after the `INSERT IGNORE` member upsert |
      | **sqlite** | **`withImmediateConn`** (`groupdialect.go:52-77`) — dedicated `*sql.Conn`, raw `BEGIN IMMEDIATE`/`COMMIT`/`ROLLBACK`. **There is no `sqliteRunInTx`.** | `BEGIN IMMEDIATE` itself — a **database-wide** write lock. The group upsert is `DO NOTHING` + a separate `SELECT`; there is **no row lock and no `RETURNING`** | anywhere inside `withImmediateConn` after the member upsert |

      **After the member upsert in all three**, so an idempotent re-add of an existing id at exactly the cap is a
      no-op rather than an overflow.

      🔴 **Two changes here are NEW in revision 4 and both come from audit NEW-7 (D-AF, reversed).**

      **(a) The count is `*CountMembers`, not a live-member count.** Use the **shipped** helper —
      `pgCountMembers` (`postgres/groupdialect.go:373`), `mysqlCountMembers` (`mysql:358`), `sqliteCountMembers`
      (`sqlite:375`), all `SELECT count(*) … WHERE group_key = ?` with **no `claimed_epoch` predicate** — passing
      the same `tx`/`conn` the rest of `AddMember` uses. Each is already called from that dialect's `SettleGroup`,
      so this is **zero new SQL and no new helper**. **Do NOT derive the count from Step 3's member `SELECT`:**
      that `SELECT` is live-only, so `len()` cannot see claimed members under any `LIMIT`. Cost, stated: **one
      extra `COUNT(*)` per `AddMember`**, on every add rather than only on overflow (D-AG).

      🔴 **THE COST IS NOT THE SAME COST ON ALL THREE ENGINES — state it per engine** (audit **R4-8**). The scan
      is `O(members)` in the group (up to `maxGroupMembers`, default **65,536**), and what it lengthens differs:

      | Engine | The added `COUNT(*)` lengthens | Scope |
      |---|---|---|
      | postgres | the group-row lock held by `ON CONFLICT … DO UPDATE` (`:107-110`) | **per correlation key** — other keys unaffected |
      | mysql | the group-row X lock taken by `ON DUPLICATE KEY UPDATE` (`:93-96`) | **per correlation key** |
      | **sqlite** | 🔴 **`BEGIN IMMEDIATE`'s DATABASE-WIDE write lock** (`sqlite/groupdialect.go:62`) | **GLOBAL** — every add to **any** group, and every other writer, waits |

      **No design change follows.** (C) is still correct for sqlite and `BEGIN IMMEDIATE` is still the right
      serializer — Spec §7.1 records that same whole-database lock as a correctness **advantage**, and it is. **It
      is the same property seen on the throughput axis**, and the bundle priced only the benefit.

      **(b) Read the group row's `locked_by` in the statement that already reads `created_at`**, so the dialect can
      classify (Step 4). Zero extra round-trips:

      | Dialect | Today | Becomes |
      |---|---|---|
      | postgres | `… RETURNING created_at` | `… RETURNING created_at, locked_by` |
      | mysql | `SELECT created_at FROM <group> WHERE group_key = ?` | `SELECT created_at, locked_by …` |
      | sqlite | `SELECT created_at FROM <group> WHERE group_key = ?` | `SELECT created_at, locked_by …` |

      `locked_by` is nullable — scan into a `*string` / `sql.NullString` and treat NULL as *not leased*. It is
      **not** added to `msginsql.GroupRows`; it is local to `AddMember`'s classification.
- [ ] **Step 3 (GREEN — the bounded fetch and the snapshot).** Give each of `pgSelectMembers` /
      `mysqlSelectMembers` / `sqliteSelectMembers` a **private `limit int` parameter, where `0` means unlimited**
      (emit no `LIMIT` clause). **`AddMember` is the ONLY caller that passes non-zero** — `maxMembers+1`;
      `ClaimGroup` and `ExpiredGroups` pass **`0`** and keep their current behavior byte-for-byte. On overflow,
      **filter the just-upserted `msgID` out of the materialized `[]MemberRow` and return the remaining rows WITH
      the error** (D-AN) — the post-rollback live set, at **no extra query**. *(Step 2(a)'s `COUNT(*)` is the only
      new round-trip `AddMember` gains; this snapshot adds none.)*

      > 🔴 **DO NOT PUT THE `LIMIT` IN THE HELPER'S SQL — it has THREE callers and only one has a cap** (audit
      > **N-5**; **D-AS**). Revision 2 said *"each dialect's live-member `SELECT` gains a `LIMIT maxMembers+1`"*,
      > which is **unimplementable** at two sites in three:
      >
      > ```
      > $ grep -rn "SelectMembers(ctx" adapter/database/sql/{postgres,mysql,sqlite}/groupdialect.go
      > postgres/groupdialect.go:121   ← AddMember      (has maxMembers)
      > postgres/groupdialect.go:163   ← ClaimGroup     (does NOT)
      > postgres/groupdialect.go:307   ← ExpiredGroups  (does NOT)
      > mysql/groupdialect.go:113 / :161 / :298      — identical three-site shape
      > sqlite/groupdialect.go:131 / :177 / :314     — identical three-site shape
      > ```
      >
      > A `LIMIT` baked into the helper **silently truncates `ClaimGroup`'s claimed set** — a legitimately at-cap
      > group then releases an **incomplete aggregate**, the silent data corruption Spec 017 §5 rejects — **and
      > `ExpiredGroups`' recovery set**, so the reaper drops members. Neither loss is visible to any AC without
      > Task 7's mutant below. The parameter is **unexported**, so it adds no class-gate key.
      >
      > 🔴 **`limit = 0` on `ClaimGroup` IS CORRECT AND IS ALSO HALF OF NEW-7.** Truncating the claimed set is
      > worse than fetching it — but with revision 3's live-only counting it left the durable table unbounded.
      > **Step 2(a)'s live+claimed count is what closes that**, not a `LIMIT` on `ClaimGroup`. Do not "fix" NEW-7
      > by bounding `ClaimGroup`; that re-introduces exactly the truncation D-AS forbids.
- [ ] **Step 4 (GREEN — the error, its SITE STRING and its CLASSIFICATION).** Return the D-AE shape wrapped per
      D-AM. Import `msgin` in each dialect (zero net dependency — already required transitively via `msginsql`).
      Verify with `GOWORK=off go mod tidy && git diff --exit-code -- go.mod go.sum` in each of the three modules.

      🔴 **THE SITE STRING IS `msgin/sql/<engine>: AddMember`, NOT `sql.GroupStore.Add`** (audit **NEW-5**; Global
      constraint 4's box). The error is minted **here**, inside the dialect, which cannot know whether it was
      reached through `sql.GroupStore.Add` or — as **AC-4b deliberately does** — through
      `kit.Group.AddMember(ctx, tx, …)` directly. It matches the shipped convention (`postgres/groupdialect.go:67`
      and siblings all prefix `msgin/sql/<engine>:`) and it is the only form that tells an operator **which
      engine** rejected, which is D-AE's own argument.

      🔴 **THE CLASSIFICATION IS `locked_by`-DISCRIMINATED, NOT UNCONDITIONALLY PERMANENT** (audit **NEW-7**;
      D-AF reversed, D-AM). Revision 3 said *"a `sql` live set is by definition unclaimed, so every `sql` over-cap
      rejection is D-AM's not-leased case."* **That is false once claimed members count** — a `sql` group can be
      over cap precisely *because* a claim is in flight, which is D-AM's **transient** arm; classifying it
      permanent dead-letters healthy traffic in a routine claim window. Using Step 2(b)'s `locked_by`:

      | Group row | Classification | Rendered |
      |---|---|---|
      | `locked_by IS NULL` — **not leased** | `msgin.Permanent(fmt.Errorf(…))` | `msgin: permanent: msgin: message dropped by overflow policy: msgin/sql/postgres: AddMember: group "k" holds 5 members, limit 4` |
      | `locked_by IS NOT NULL` — **leased** | the bare `fmt.Errorf(…)`, transient | the same string **without** the `msgin: permanent: ` prefix |

      **One rule, two stores:** `locked_by IS NULL` is exactly `memory`'s `!g.leased`, and exactly §3.3.1's
      restated premise *"nothing drains an UNLEASED group without an expiry cutoff."*
- [ ] **Step 5 (THE GATE — SIX modules, not four; audit N-1, arm corrected per audit NEW-2).** Global constraint
      8's *compiles-against* rule:

      | Module | Command | Why |
      |---|---|---|
      | root | `GOWORK=off go test ./... -race -shuffle=on` | the option, the store, the SPI declaration |
      | `postgres`, `mysql`, `sqlite` | `GOWORK=off go test ./... -race -shuffle=on` | the three implementations |
      | **`harness`** | **`GOWORK=off go vet ./...`** | holds a `msginsql.GroupDialect` (`testkit.go:87`) and calls `AddMember` (`groupstore.go:345`). 🔴 **It has NO `_test.go` files, so `go test` is a FALSE PASS** (CLAUDE.md) |
      | **`dbtest`** | **`GOWORK=off go vet ./...`** (full Docker run in Task 7) | requires `harness` with a local `replace` (`dbtest/go.mod`). 🔴 **Every Go file here IS a `_test.go` file, so `go build` compiles NOTHING and exits 0 whatever breaks** — `go list -f '{{.GoFiles}}'` → `[]`, all four files are `XTestGoFiles`. `go vet` type-checks them; `go build` cannot |

      **Revision 2's gate listed only "root + all three dialect modules"**, so it could not see the two modules
      Step 6b exists to keep green.

      > 🔴 **BOTH FALSE-PASS DIRECTIONS, RECORDED ONCE** (audit **NEW-2**). Revision 3 fixed one and introduced
      > the other, one row apart, in this same table:
      >
      > | Module shape | `go test` | `go build` | `go vet` |
      > |---|---|---|---|
      > | non-test files, **no** `_test.go` (`harness`) | **FALSE PASS** | sees the break | sees the break |
      > | **only** `_test.go` files (`dbtest`) | sees the break (needs Docker) | **FALSE PASS** | sees the break |
      >
      > `go vet` is the only command correct for **both**. Before adding any module to a gate, ask which of the
      > two shapes it has.
- [ ] **Step 6.** Commit **Tasks 5 + 6 together** (including Task 5 Step 6b's harness call site):
      `feat(sql): bound a group's member count inside the dialect transaction`.

      > **NEXT UP IS TASK 3, not Task 7** (Sizing table's order column; **D-AT**). `sql`'s
      > `defaultMaxGroupMembers` now exists, so Task 3's AST invariant has its full three-file parse set for the
      > first time. Task 7 follows it.

| # | Branch | Covering case (lands in Task 7's harness) | Killing mutant |
|---|---|---|---|
| B6-1 | live count > `maxMembers` → return + rollback | `member_cap_rejects_and_rolls_back` | return the error **after** the commit ⇒ the row-count assertion fails |
| B6-2 | live count ≤ `maxMembers` → commit normally | `member_cap_admits_up_to_the_cap` | off-by-one to `>=` ⇒ fails |
| B6-3 | re-add of an existing id at exactly the cap → no-op | `member_cap_readd_at_cap_is_a_noop` | move the check **before** the upsert ⇒ fails |
| **B6-4** | **the live snapshot is returned with the error, rejected member filtered** | `member_cap_returns_the_live_snapshot` | return empty `GroupRows` ⇒ fails |
| **B6-5** | **an UNLEASED rejection is `Permanent`** | `member_cap_rejection_is_permanent` | drop the wrap ⇒ fails |
| **B6-8** | **the count includes CLAIMED members** (D-AF, reversed — audit **NEW-7**) | `member_cap_counts_claimed_members` — fill to exactly `cap`, `ClaimGroup` (which stamps every live member ⇒ live = 0), then `AddMember` once more ⇒ **rejected** | count `claimed_epoch IS NULL` instead of `count(*)` ⇒ the member is **admitted** and the case fails. **This is the mutant that proves the durable table is bounded**; without it, `cap` more rows are admitted per claim cycle, forever |
| **B6-9** | **a LEASED rejection is TRANSIENT** (`locked_by IS NOT NULL`) | same fixture as B6-8, asserting `!msgin.IsPermanent(err)` | wrap unconditionally ⇒ fails, and a routine claim window dead-letters healthy traffic |
| **B6-10** | **the rendered site names the ENGINE** (audit **NEW-5**) | `member_cap_render_names_the_dialect` — the full AC-2c string, per engine, through **both** entry points (`GroupStore.Add` and `kit.Group.AddMember`) | render `sql.GroupStore.Add` ⇒ both halves fail, and the second one names a store never involved |
| **B6-6** | **`*sql.Tx` Querier: no rollback, caller owns it** | `member_cap_under_caller_owned_tx` (Spec 017 AC-4b) — driven at **`kit.Group.AddMember(ctx, tx, …)`**, the dialect, **never** `GroupStore.Add`, which cannot take a Querier (audit **N-2**) | assume `pgRunInTx` rolled back ⇒ the in-transaction row-count assertion fails |
| **B6-7** | **`ClaimGroup` / `ExpiredGroups` pass `limit = 0`** (D-AS) | `member_cap_claim_at_cap_returns_every_member` — fill a group to exactly `cap`, `ClaimGroup`, assert **`cap`** members claimed | pass `maxMembers+1` from `ClaimGroup` ⇒ the claimed set is **truncated** and the case fails. **Without this mutant, "0 means unlimited" is a comment** (audit **N-5**) |

> **Task 6's branches are proven by Task 7's harness cases**, because a dialect's behavior is only observable
> against a real engine. Tasks 6 and 7 therefore land in **two commits but one green unit** — Task 6's commit must
> already pass Task 7's harness locally before it is made. If Docker is unavailable, **stop and escalate**; do not
> substitute a fake (`use-testcontainers`).

---

## Task 7 — the shared dialect conformance case (AC-4, AC-4b, AC-4c, AC-5)

**Files:** `adapter/database/sql/harness/groupstore.go`; run via `adapter/database/sql/dbtest`.
**Modules:** `harness`, `dbtest`. **Requires a running Docker daemon.**

> 🔴 **THE SIGNATURE UPDATE ALREADY LANDED, IN THE TASKS 5+6 COMMIT** (Task 5 Step 6b; audit **N-1**). This task
> adds **behavior only**. If `harness` does not compile when you start, Tasks 5+6 were committed red — stop and fix
> that commit rather than repairing it here.

- [ ] **Step 1.** Skills + **`use-testcontainers`**. Read the group conformance kit around
      `harness/groupstore.go:345` (the `AddMember` call site, already threaded with `groupMemberCap`) with `gopls`.
      **Every case below uses that same unexported constant** (Task 5 Step 6b) — not a `TestKit` field, not an
      exported parameter (audit **NEW-3**; the box at Step 5).
- [ ] **Step 2 (AC-4).** Add one conformance case, run by **all three** dialects, asserting **seven** things:
      1. the `cap+1`-th live member ⇒ `errors.Is(err, msgin.ErrOverflowDropped)`;
      2. **the rollback, asserted not assumed** — a subsequent `ClaimGroup` returns exactly `cap` members **and** a
         direct member-row count over the table equals `cap`. *Without this half, D-AG's enforcement (C) is
         indistinguishable from the rejected (A);*
      3. re-adding an **existing** member id while the group sits at exactly `cap` is a **no-op returning the
         unchanged snapshot**, not an overflow;
      4. **the returned `GroupRows` is non-empty and holds exactly `cap` members** — the post-rollback live set,
         rejected member filtered (D-AN);
      5. **`msgin.IsPermanent(err)`** for this (unleased) case (D-AM);
      6. 🔴 **NEW in revision 4 (audit NEW-5) — THE FULL RENDER, per engine.** Assert Spec 017 AC-2c's `sql`
         string exactly, including the `msgin: permanent: ` prefix and the **engine-naming site**
         (`msgin/sql/postgres: AddMember` / `…mysql…` / `…sqlite…`). **This assertion lives here and nowhere
         else:** Task 5's fake-dialect cases assert only `IsPermanent` and `errors.Is`, and a render assertion
         through the fake would be **vacuous** — the fake mints whatever the test hands it. Revision 3 pinned the
         string in an AC that **no task executed**;
      7. 🔴 **NEW in revision 4 (audit NEW-7) — CLAIMED MEMBERS COUNT.** Fill the group to exactly `cap`,
         `ClaimGroup` it (which stamps **every** live member, so the live count is **0**), then `AddMember` once
         more and assert it is **rejected** with `!msgin.IsPermanent(err)` — over cap because of the claimed set,
         and **transient** because a claim is in flight (`locked_by IS NOT NULL`). **This is the case that proves
         the durable table is bounded** (B6-8 / B6-9). **Killing mutants:** count `claimed_epoch IS NULL` ⇒ the
         member is admitted and `cap` more rows land per claim cycle, forever; wrap unconditionally ⇒ a routine
         claim window dead-letters healthy traffic.
- [ ] **Step 3 (AC-4b — the caller-owned transaction). NEW in revision 2 (audit M-2); entry point corrected in
      revision 3 (audit N-2).** `BeginTx` on the caller's side, then call **`kit.Group.AddMember(ctx, tx, …)` — the
      DIALECT, directly** — for the `cap+1`-th member. Assert the rejection still fires; assert the member row
      **IS** present inside the still-open transaction (a `SELECT` on that same `*sql.Tx` sees `cap+1`); assert it
      is gone after the caller's own `Rollback`. **This documents D-AP's precondition as tested behavior rather
      than as a hope.** *(sqlite's `withImmediateConn` requires a `*sql.DB` Querier and errors on anything else —
      assert that error instead, and record the asymmetry.)*

      > 🔴 **NOT through `sql.GroupStore.Add`.** `NewGroupStore` takes a concrete `*stdsql.DB` and `:272` always
      > passes `s.db`, so the store can never reach this branch — revision 2's *"pass the `*sql.Tx` as the Querier;
      > `Add` …"* has no executable reading (audit **N-2**). **Add the pair:** drive the same overflow through a
      > real `sql.GroupStore` and assert exactly `cap` rows remain — i.e. the bound **is** unconditionally durable
      > there. That is the contrapositive the option's godoc now promises.
- [ ] **Step 4 (AC-4c).** Assert the sentinel **and** the `Permanent` marker survive `classifyQueryErr`'s
      `SchemaExists` pass-through, with the table present.
- [ ] **Step 5.** `harness` has **no test files**, so `go test` there is a false pass — check it with
      `GOWORK=off go vet ./...` (CLAUDE.md). Run the real conformance through `dbtest`.

      > 🔴 **Do not add an exported function with an `int`/`int64` parameter to `harness`.** Half 1 of the class
      > gate walks the filesystem into leaf modules, and half 2 (a root test) **cannot import a leaf module** — so
      > such a key is an unsatisfiable gate failure by design. Verified clean today:
      > `grep -rnE "^func [A-Z][A-Za-z]*\(.*\b(int|int64)\b" adapter/database/sql/harness/*.go` returns nothing.
      > **The cap value is the unexported package constant `groupMemberCap`** (Task 5 Step 6b) — **not** a
      > `TestKit` field and **not** a new exported parameter. Revision 3 said *"the cap travels in the existing
      > `TestKit`"*, which has no referent: `TestKit` has no integer field, and `testkit.go` is in no Files list
      > (audit **NEW-3**). **Re-run that grep before committing.**
- [ ] **Step 5b (D-AS's guard — audit N-5).** Add the `limit = 0` conformance case: fill a group to exactly `cap`,
      `ClaimGroup`, and assert **all `cap`** members come back. Mutation-prove it by passing `maxMembers+1` from
      `ClaimGroup` ⇒ truncation ⇒ the case fails. Without this, nothing in the increment notices a `LIMIT` leaking
      into `ClaimGroup` or `ExpiredGroups`. *(Step 2 item 7 shares this fixture — fill to cap, then claim — and
      asserts the complementary half: the claimed members still count against the bound.)*
- [ ] **Step 6.** Mutation-prove **B6-1 … B6-10** from Task 6 against this harness (that is what makes them
      real) — **ten rows, counted off Task 6's table; do not transcribe this number**.
- [ ] **Step 7.** Commit: `test(sql): add the group member-cap dialect conformance case`.

---

## Task 8 — the SPI contract, and interface-level conformance on both stores (D-AH)

**Files:** `groupstore.go` (root, godoc only), `adapter/memory/groupstore_test.go`,
`adapter/database/sql/groupstore_unit_test.go`.
**Module:** root.

- [ ] **Step 1.** Skills + `golang-documentation` + `golang-structs-interfaces`.
- [ ] **Step 2.** Add Spec 017 §3.7's paragraph to `msgin.MessageGroupStore.Add`'s godoc — **`groupstore.go:38-45`**
      (doc comment `:38-44`, declaration `:45`; revision 1 cited `:41-52`, audit m-2). It carries four clauses:
      the **MUST**-bound requirement, the **MUST**-report-`ErrOverflowDropped` requirement, the **SHOULD**-mark-
      `Permanent`-when-the-group-cannot-drain requirement (naming the default-`RetryPolicy` hot spin as the reason),
      and the **MAY**-return-the-live-snapshot clause with its D-AN consequence. Plus the D-AF note that the counted
      set is implementation-specific and must be stated.

      > 🔴 **THE DOWNGRADE-ONLY CLAUSE IS RESTATED IN REVISION 4 — the revision-3 form is FALSE for two of the six
      > exits** (audit **NEW-6**). Revision 3 promoted N-7's rule into this paragraph as *"may only ever DOWNGRADE
      > … **on positive evidence that the group drained** … a bug in the drain path costs a retry, **never a
      > message the implementation marked recoverable**."* But **exits 3 and 5** of Spec §3.3a.1's own table —
      > `cerr != nil` ⇒ `return cerr` and `relErr != nil` ⇒ `return relErr` — **discard the store's
      > `Permanent`-marked error and return an unmarked, hence transient, one**, and they do it because the drain
      > **FAILED**: evidence of the opposite of drainage. Under `RetryPolicy{}` that is B-1's unlogged zero-delay
      > spin, for the sub-case *"the release fires but claim/release keeps failing"*. **Write the true rule:**
      > the Aggregator either **downgrades on positive evidence of drainage** (exits 4 and 6) **or replaces the
      > overflow error entirely with a distinct fault carrying that fault's own classification** (exits 3 and 5);
      > it **never upgrades** a transient rejection to permanent; and a **persistently failing claim/release path
      > therefore RETRIES rather than terminating.** Spec §3.7 carries the wording — copy it, do not paraphrase.
- [ ] **Step 3 (AC-7 — ONE named clause, not "§3.7's requirement").** Add one case per first-party store for
      §3.7's **MUST-report** clause, with the store **held in a `msgin.MessageGroupStore` variable** rather than
      its concrete type:

      ```go
      var store msgin.MessageGroupStore = /* memory.NewGroupStore(...) | sql.NewGroupStore(...) */
      _, err := store.Add(t.Context(), "k", overCapMessage)
      require.ErrorIs(t, err, msgin.ErrOverflowDropped)
      ```

      **Killing mutant:** return a bare, non-wrapping error from either store ⇒ the `ErrorIs` fails.

      > 🔴 **Revision 2 said *"a case asserting §3.7's requirement"* — §3.7 has FOUR clauses and three are already
      > covered** (audit **N-12**): the MUSTs by AC-1/AC-4, the SHOULD by AC-1/AC-4.5, the MAY by AC-1b/AC-4.4.
      > Naming a section instead of a clause is the unexecutable-AC defect Plan 029's audit found in **five
      > consecutive rounds**. What this case uniquely buys is the **interface-typed drive** — copyable verbatim by
      > a third-party implementer.

      The `sql` half uses the `fakeGroupDialect` (no Docker) to assert the propagation contract; the real-engine
      proof is Task 7's.
- [ ] **Step 4.** Green; commit: `docs(core): state the per-group member bound on the MessageGroupStore SPI`.

**No new logic branch** — this task adds prose and re-drives covered branches through the interface.

---

## Task 9 — the class gate's stated blind spot and its count sweep (D-AL)

**Files:** `sizing_option_class_gate_test.go` (root).
**Module:** root.

> **The keys, the rows and the executable counts already landed in Tasks 1 and 5** (Global constraint 8's box —
> audit B-2). What remains here is prose that is *false* rather than merely stale, plus the vacuity probes.

- [ ] **Step 1.** Skills + `table-test`. Run the gate and record the **current** figures rather than trusting this
      plan (Global constraint 12): `GOTOOLCHAIN=go1.25.13 go test -count=1 -run TestSizingOptionClass -v .` →
      expect **19** functions, **27** methods, both halves PASS. *(Baseline at `f39725d`, before this increment:
      **17 / 27 / PASS** — re-derived, not transcribed; `go test -count=1` so a cached result cannot stand in.)*
- [ ] **Step 2 (the FALSIFIED claim, not a stale number).** The ROOT-MODULE IMPORT BOUNDARY limitation — locate it
      with `grep -n 'ROOT-MODULE IMPORT BOUNDARY' sizing_option_class_gate_test.go` — reads *"All 17 keys live in
      root-module packages today (endpoint, adapter/http, adapter/memory, channel, resilience, routing)."*
      `sql.WithMaxGroupMembers` lives in **`adapter/database/sql`**, which is not on that list. Add it, and update
      the count. **This is a corrected claim about the gate's own coverage** (audit M-7).
- [ ] **Step 3 (the count sites — GENERATED, NEVER TRANSCRIBED).** 🔴 **Run this script and paste its output as
      the task's site table** (audit **R4-2**; Global constraint 12). Do **not** edit a list of line numbers:

      ```sh
      #!/bin/sh
      # Plan 031 Task 9 — DERIVE the class-gate site table FROM THE FILE, at HEAD.
      # Run from the repo root, on the tree this task will actually edit.
      F=sizing_option_class_gate_test.go
      N='12|17|19|21|27|44|46'
      NOUN='key|row|arm|method|manual|ast|funcdecl|fixed|rejects|safe|deferred|excluded|module|package|conformance|positional|burst'

      echo "### A. EXECUTABLE — these FAIL THE SUITE (edit, or the task cannot reach green)"
      grep -nE 'require\.(Len|Equal)\(t, (tests, [0-9]+|[0-9]+, methodCount|wantArms, gotArms|map\[string\]int\{)' "$F"
      echo; echo "### B. EXACT-MAP LITERALS the asserts in A compare against — edit these too"
      grep -nE '^[[:space:]]*(wantArms := map\[string\]string\{|require\.Equal\(t, map\[string\]int\{)' "$F"
      printf '  wantArms entries: %s\n' "$(awk '/wantArms := map\[string\]string\{/,/^\t\}/' "$F" | grep -c '":')"
      echo; echo "### C. ASSERTION MESSAGES — compile, never fail; still normative prose"
      awk -v n="$N" '/^[[:space:]]+"/ && $0 ~ ("(^|[^0-9,.])(" n ")([^0-9,.]|$)") { printf "%d:%s\n", NR, $0 }' "$F"
      echo; echo "### D. COMMENT/PROSE sites stating a count"
      awk -v n="$N" -v noun="$NOUN" 'substr($0,1,2)=="//" && $0 ~ ("(^|[^0-9,.])(" n ")([^0-9,.]|$)") \
        && tolower($0) ~ noun { printf "%d:%s\n", NR, $0 }' "$F"
      echo; echo "### D2. PER-ARM COUNT sites — the \"<arm>\" (n) form"
      grep -nE '"(fixed|rejects|deferred|safe)"[[:space:]]*\([0-9]+\)' "$F"
      echo; echo "### E. ORDINAL sites — valid only while the key ORDER holds"
      grep -nE '[0-9]+(st|nd|rd|th) key' "$F"
      echo; echo "### F. GROUND TRUTH — trust this, not any number above"
      GOTOOLCHAIN=go1.25.13 go test -count=1 -run TestSizingOptionClass -v . 2>&1 \
        | grep -E '=== EXPORTED|^(ok|FAIL)|^--- ' | sed 's/^[[:space:]]*//'
      ```

      **Its output at `f39725d`, pasted as the shape to expect — not as the answer:** **27 unique sites**, of which
      **A yields FOUR EXECUTABLE assertions** (`require.Equal(…, methodCount)` — *does not move*;
      `require.Len(t, tests, 19)`; `require.Equal(t, wantArms, gotArms)`; `require.Equal(t, map[string]int{"fixed":
      12, …}, byArm)`), **B yields the two exact-map literals** (`wantArms`, **19 entries**), **C yields four
      assertion-message sites**, **D ∪ D2 yield 17 comment/prose sites**, and **E yields one ordinal** —
      *"burst is the 17th key, positional"*, valid only if the two new keys were appended **after**
      `resilience.NewTokenBucket`.

      > 🔴 **REVISION 4 SAID *"ten in all / two executable"*. Both halves were wrong** (audit **R4-2**, **R4-1**).
      > It is **27 sites, four executable — and THREE of the four move.** The two that revisions 1-4 never named
      > are `wantArms` and the `byArm` literal, and they are `require`, so **Tasks 1 and 5 abort without them**
      > (Global constraint 8's box). They are edited in Tasks 1 and 5; **this task verifies they were.**
      >
      > 🔴 **The arm ARITHMETIC also changed composition, not just coordinate.** Revision 4's row 2 read
      > *"9 + 1 + 3 + 6"* and Spec AC-8's twin read *"11 + 1 + 3 + 6 = 21"*. The `deferred` arm is **empty and
      > tombstoned** since Plan 032; the file reads `12 + 1 + 6 = 19` and the true post-increment partition is
      > **14 + 1 + 0 + 6 = 21**. `11+1+3+6` also totals 21 — *the total survived by coincidence.* **Reconcile by
      > name, never by count.**
      >
      > 🔴 **And the *"`fixed` ⇒ `1<<30`"* rule is FALSE as a rule** — see Task 1 Step 2's two-dimensional box.
      > When editing the header's literal split, carry *the arm fixes the property; within a reject arm the
      > parameter type chooses the literal*, not the one-liner.
- [ ] **Step 4.** State, in the header, that **`methodCount` stays 27**: `GroupDialect.AddMember` gains an `int`
      parameter but all three dialect implementations already carry `seq int64` and are already counted — the
      header itself names `postgresGroupDialect.AddMember` (`grep -n 'postgresGroupDialect.AddMember'
      sizing_option_class_gate_test.go`; audit m-10). **Do not bump `require.Equal(t, 27, methodCount, …)`.**
- [ ] **Step 5.** Add the **fifth accepted limitation** to the file header, verbatim from ADR 0033 **D-AL**: a
      bound that does not arrive as an integer parameter is invisible — `*ast.FuncType`, a named func type, or a
      **message header** — naming `WithReleaseWhen`, `WithReleaseStrategy` and `defaultRelease`, and pointing at
      Spec 017 §1.4 and the store as their enforcement site.
- [ ] **Step 6.** State, in the same header, that `GroupDialect.AddMember`'s new `int` parameter is a **method**
      (excluded by the ratified `Recv == nil` boundary) and is **not** a class member under D-AB — `maxMembers`
      *is* the bound, not a bounded quantity — so no manual row is required.
- [ ] **Step 7 (AC-10, vacuity — TWO probes, both recorded).**

      > 🔴 **Revision 1's single probe site rested on a false premise (audit M-8).** It said to plant in
      > *"`adapter/database/sql`, **the module** this increment newly touches, not in root."*
      > **`adapter/database/sql` is not a module** — `find . -name go.mod` lists eight and it is not among them; it
      > is a package *in* the root module. A probe there **is** a probe in root, and it answers a question that was
      > never in doubt.

      | Probe | Where | Expected |
      |---|---|---|
      | A | `adapter/database/sql/postgres` — a **real leaf module**, one this increment genuinely newly touches | half 1 reports exactly one extra key, and it **cannot** be satisfied by half 2 (a root test cannot import a leaf module) — the deliberate gate failure the header promises |
      | B | `adapter/database/sql` — a **same-module package** | half 1 reports exactly one extra key, adoptable by half 2 |
      | C | flip one conformance row's `arm` | half 2 reports the **pairwise** mismatch, not merely a count mismatch |

      **Record all three outcomes in the evidence block. Revert every probe and re-run.**
- [ ] **Step 8.** Commit: `test(core): state the sizing class gate's func-typed blind spot`.

| # | Branch | Covering case | Killing mutant |
|---|---|---|---|
| B9-1 | half 1 set-difference in the **added** direction | probes A and B | remove the diff check ⇒ the probes pass silently |
| B9-2 | half 1 set-difference in the **removed** direction | delete one key from `sizingConformanceKeys` | as above |
| B9-3 | half 2's pairwise `arm` mapping | probe C | replace the map with a count map ⇒ a pairwise swap passes (the stored lesson *"assert the partition, not just the rows"*) |

---

## Task 9b — fold the two new arm rows back into Spec 016 (R4-1)

**Files:** [`docs/specs/016-sizing-option-bounds.md`](../specs/016-sizing-option-bounds.md) (§2.1's arm table, §6
AC-5's tabulation, and the plan list).
**Module:** none — documentation only.

> 🔴 **THIS TASK EXISTS BECAUSE THE GATE ITSELF DEMANDS IT, AND REVISIONS 1-4 HAD NO ANSWER** (audit **R4-1**).
> `wantArms`'s failure message is not neutral — it is an **instruction**, and it points **outside this increment**:
>
> ```
> $ grep -n 'SPEC change' sizing_option_class_gate_test.go
> …	"Spec 016 §2.1's arm table and §6 AC-5 fix EVERY key's arm … Moving a row between arms is a
>  	 SPEC change — update §2.1 and §6 AC-5, do not just edit this map"
> ```
>
> **Plan 031 adds two rows to the `fixed` arm**, which is exactly *"moving a row between arms"* at the granularity
> §2.1 tabulates — and **[Spec 016](../specs/016-sizing-option-bounds.md) is DELIVERED** (Plan 029, merged). Before
> revision 5, **no task in this plan opened it**, so an implementer meeting the red at Task 1 Step 2 had three
> options and the plan authorised none: edit the map and ignore the instruction (silent drift — exactly what the
> map exists to catch); amend a delivered spec ad hoc at the keyboard (no ADR, no trailer, forbidden by
> [CLAUDE.md](../../CLAUDE.md)'s traceability rule); or escalate, stalling the increment's **first** task.
>
> **DECISION: Plan 031 takes unconditional ownership of the fold-back**, following the precedent
> [Plan 032](032-byte-cap-ceilings.md) set when its own audit hit this same question — it took ownership rather
> than deferring, **and it re-derived the arm table from the tree at fold-back time rather than transcribing a
> pre-computed count.** This task does the same. **A fold-back task that carries a number written seven tasks
> earlier is audit R4-2 again, one artifact over.**

- [ ] **Step 1.** Skills + `golang-documentation`. Read Spec 016 **§2.1** (the classification arms and their
      table) and **§6 AC-5** (the behavioral-arm tabulation) end to end. 🔴 **They are two different partitions and
      Spec 016 says so** — §2.1's *classification verdicts* are **not** a relabelling of AC-5's *behavioral arms*,
      and the gate file's own header restates that. **Do not collapse them.**
- [ ] **Step 2 (RE-DERIVE, do not transcribe).** Regenerate the arm table **from the tree**, at this commit, with
      Task 9 Step 3's script (arms **A**, **B**, **D2** and **F**). The two new rows and their arms are read off
      `wantArms` and `byArm` as Tasks 1 and 5 left them — **not** off any number written in this plan, in Spec 017,
      in ADR 0033 or in Spec 016.

      > 🔴 **This is the whole point of the task and the reason it runs LAST among the gate tasks.** Both of Spec
      > 016's own count claims have been falsified once already by an increment landing underneath it: Plan 032
      > moved three rows out of `deferred` into `fixed` and tombstoned the arm, and this plan carried
      > *"11 + 1 + 3 + 6"* through four revisions afterwards. **Generate; do not increment.**
- [ ] **Step 3 (§2.1).** Fold `memory.WithMaxGroupMembers` and `sql.WithMaxGroupMembers` into §2.1's table as
      **class members** under §2.1's own criterion (*"n is the sole bound on an accumulation"* — ADR 0032 **D-AB**;
      they are, which is this whole increment's premise), and update any count §2.1 states, **by name**.
- [ ] **Step 4 (§6 AC-5).** Update AC-5's behavioral-arm tabulation to the re-derived partition — at the time of
      writing that is **14 fixed / 1 rejects / 0 deferred / 6 safe = 21 rows = 19 AST keys + 2 manual rows**, but
      **Step 2's output governs, not this sentence.** 🔴 **Carry the `deferred`-is-tombstoned note and the
      *"`byArm` has NO key for an empty arm"* warning**, so the next increment does not reintroduce `"deferred": 0`.
- [ ] **Step 5 (TRACEABILITY, both directions).** Add **Plan 031** to Spec 016's *"realized by / amended by"* list
      with a one-line reason, and make the §2.1 and AC-5 edits cite **Spec 017 / ADR 0033 D-AL / Plan 031** so the
      chain reads in both directions. **A delivered spec amended by a later increment with no back-link is exactly
      the untraceable artifact CLAUDE.md forbids merging.**
- [ ] **Step 6 (VERIFY, then the gate).** Re-run `GOTOOLCHAIN=go1.25.13 go test -count=1 -run TestSizingOptionClass
      -v .` — green — and **re-run both arms of the docs-link gate** (CLAUDE.md), because this task edits a file
      other artifacts link into. Any output is a blocker.
- [ ] **Step 7.** Commit, standalone (a `spec` commit stands alone — CLAUDE.md's commit discipline):
      `spec(016): fold Plan 031's two new sizing rows into §2.1 and §6 AC-5`, with `Spec: 016`, `Spec: 017`,
      `Plan: 031`, `ADR: 0033` trailers.

**No new logic branch** — this task edits a spec. Its correctness gate is the class gate itself, which is already
green from Tasks 1, 5 and 9, plus the docs-link gate.

> **If the user rejects this ownership decision**, the alternative is to file the Spec 016 divergence as a backlog
> item in `docs/HANDOVER.md` §6 and **delete this task** — but then Tasks 1 and 5 ship a `wantArms` edit that the
> assertion's own message says is insufficient, and the gate's Spec-016 cross-reference is knowingly stale from
> the moment this increment lands. **Recommendation: keep the task.** It is documentation-only, it is small, and
> deferring it converts a mechanical edit into a silent drift the gate was built to catch.

---

## Task 10 — whole-branch delivery gate

**Files:** `docs/HANDOVER.md`, `docs/specs/017-*`, `docs/adrs/0033-*`, this plan (status lines).

- [ ] **Step 0 (THREE-ARTIFACT RECONCILIATION — audit N-14).** Before any review: for **every** finding this bundle
      dispositions, confirm the fix is present in **all three** artifacts that carry the claim — Spec 017,
      ADR 0033 and this plan. **This is the project's named failure mode** (*"fold into all three artifacts"*): in
      an earlier audit 4 of 6 unclean findings were the ADR missing an edit the spec and plan got, and it recurred
      in revision 2 on B-2, the highest-impact finding of round 1 —
      `grep -n "same commit\|red suite\|exact set equality" docs/adrs/0033-*.md` returned **nothing**. Diff the
      three against each other; do not spot-check.

      > 🔴 **AND IT RECURRED AGAIN IN REVISION 3, ON THE ROUND'S OWN FINDING LIST** (audit **NEW-8**). N-8's
      > disposition — the two stores' divergent counts — reached the spec (5 hits) and the plan (2 hits) and
      > **the ADR not at all**: `grep -n "65537\|holds 5 members\|members retained at the moment"
      > docs/adrs/0033-*.md` returned nothing. **This step being scheduled at the DELIVERY gate is why it did not
      > catch a DESIGN divergence.** Revision 4 therefore runs this reconciliation **as part of closing the
      > revision**, not only here — and this step remains, unchanged, as the last-chance re-run.
      >
      > 🔴 **AND IN REVISION 4 IT WAS NOT ACTUALLY RUN — WHICH IS HOW R4-6 SURVIVED.** Round 4 found a
      > **Consequences** bullet (*"Every overflow rejection on the `sql` path … **Bounded now that D-AM makes the
      > rejection terminal**"*) that D-AF's own reversal had falsified in the same revision — and its **spec twin**
      > in §8's backlog carried the identical sentence. Two-of-three in the other direction from NEW-8: **both**
      > artifacts stale, for **one** reason. **Revision 5 ran this step for real, finding-by-finding, across all
      > three artifacts** — and that is what caught R4-6's twin. **Do not close a future revision without it.**
- [ ] **Step 1.** `/code-review` over the **whole-branch** diff `main..HEAD` — not the last commit. Fix or
      explicitly triage every finding with a written rationale.
- [ ] **Step 2.** `/security-review` over the same range. Same rule.
- [ ] **Step 3.** **Library quality gates, per touched module** — the eight CI steps, not the two the local loop
      runs: `go build ./...`, `go vet ./...`, `gofmt -l .`, `CGO_ENABLED=0 go build ./...`,
      `go mod tidy` + `git diff --exit-code -- go.mod go.sum`, `govulncheck ./...`, `golangci-lint run ./...`,
      `go test ./... -race -shuffle=on`. Run the **8-module** loops from CLAUDE.md's Commands section, `GOWORK=off`
      for the per-module pass and `GOWORK` unset for the workspace pass. **`harness` has no test files — check it
      with `go vet`.**
- [ ] **Step 3b (32-BIT). NEW in revision 2 (audit M-1).** `GOARCH=386 GOOS=linux go vet ./...` in root and every
      touched module. Nothing else in this plan builds for 386, and the class gate's `fixed` arm is exactly where a
      `1<<62` slip would land. **Treat any output as a blocker.**
- [ ] **Step 4.** **Docs link gate, both arms**, over every tracked Markdown file (CLAUDE.md). Treat any output as
      a blocker. Baseline at this branch point is **exactly two arm-1 false positives** — `docs/plans/016-aggregator.md
      -> docs/plans/m` and `docs/specs/006-cron-source.md -> docs/specs/factory(fireTime`, both Go identifiers
      leaking from line-wrapped inline code — and **zero** arm-2 hits. *A hit naming a plausible `.md` path is
      real; a hit naming a Go identifier or containing a space is the parser limitation.* **Verify arm 2 is not
      vacuous** by planting a bad anchor and re-running. **Stage this plan (`git add -N`) before measuring**, or
      `git ls-files` will not scan the artifact the gate governs (Plan 030's MINOR 11).
- [ ] **Step 5.** ~~The un-mechanizable invariant.~~ **Superseded by Task 3** (audit M-5, D-AQ): the
      `default ≥ completionSizeCeiling` invariant is now enforced by an AST test, not by a hand `grep`. Confirm
      **Task 3's single test** — one task, one commit, executed **after Tasks 5+6** (**D-AT**; audit **NEW-1**
      deleted the split that would have made this two half-tests) — is present, non-vacuous (**both** probes: Task
      3 Step 2's value RED and Step 4's not-found RED) and green. **That** is the evidence, and it needs no
      transcribed `grep` output.
- [ ] **Step 6.** Re-derive, do not transcribe, the figures the artifacts cite: the class-gate key count (expect
      **19**), method count (expect **27**) and **arm partition** (expect **14 fixed / 1 rejects / 0 deferred /
      6 safe = 21**, and **no `"deferred"` key in `byArm`**) — all four from **Task 9 Step 3's script**, not from
      this plan; **confirm Task 9b left Spec 016 §2.1 and §6 AC-5 consistent with that same output**; the
      `ErrInvalidCapacity` producer count — expect **six**, and
      reconcile **by option name**: `memory.WithBuffer` (`adapter/memory/memory.go:82`), `memory.WithMaxGroups`
      (`groupstore.go:105`), `memory.WithCapacity` (`queuestore.go:114`), `routing.WithCompletionSize`
      (`aggregator.go:354`), plus the two new `WithMaxGroupMembers` sites — and the `ErrOverflowDropped` producer
      count. **Reconcile by name, never by count** (the project's standing `43 ≠ 43` lesson).
- [ ] **Step 7.** Update `docs/HANDOVER.md`: close §6 backlog item **7**; add the follow-ups Spec 017 §8 records
      (`memory`'s quadratic clone, `sql`'s per-`Add` full-group re-fetch, *cap-without-timeout* diagnostics,
      **`classifyQueryErr`'s extra round-trip — recording that it is bounded ONLY for a NOT-LEASED rejection**,
      because D-AM makes only that arm terminal and a **leased** rejection is transient and pays the probe on
      every retry for up to §8 item 9's window (audit **R4-6**; Spec §8's bullet and ADR 0033's Consequences twin
      both carry the scoped form) — and — **the root cause behind audit B-1** — that the default
      `msgin.RetryPolicy` neither logs nor bounds a transient fault); and refresh the artifact counts in
      `docs/HANDOVER.md` **and** in CLAUDE.md's Project status paragraph. **Re-derive every count with the commands
      that paragraph names — do not increment a number in this plan**, because Plan 030 **and Plan 032** have both
      landed. Count **distinct plan numbers, not files** — and note that this increment adds
      [`031-audit-round-1.md`](031-audit-round-1.md), [`031-audit-round-2.md`](031-audit-round-2.md),
      [`031-audit-round-3.md`](031-audit-round-3.md) and [`031-audit-round-4.md`](031-audit-round-4.md) as **four
      satellites** of plan number 031, not new plan numbers. 🔴 **Also add the follow-up
      [`031-audit-round-4.md`](031-audit-round-4.md)'s LATER-ADDITION subsection recommends**: give
      `adapter/database/sql/groupstore.go` and `routing/aggregator.go` the same *cite-the-grep-not-the-line*
      treatment Global constraint 12 gave the class-gate file — they are this bundle's two most-cited files and
      revision 5 corrected 41 of the former's coordinates **by hand** after a comment-only commit shifted them.
- [ ] **Step 8.** Also fix CLAUDE.md's stale `reliability.go:46` citation for `IsPermanent`, which is
      `reliability.go:86-97` (audit m-1 — the same stale citation this bundle inherited).
- [ ] **Step 9.** Flip the status lines: Spec 017 → DELIVERED, ADR 0033 → ACCEPTED, this plan → DELIVERED — and
      **remove the "without user ratification" banners only if the user has by then ratified them.** If not, leave
      them and say so.
- [ ] **Step 10.** Stage, show the diff, and **wait for explicit approval** before the final commit. `git push`, the
      merge and the branch deletion each need their own approval (Global constraint 9).

---

## Sizing

🔴 **THIS TABLE IS ORDERED BY EXECUTION, NOT BY TASK NUMBER** (audit **NEW-1**; **D-AT**). **Task 3 runs after
Tasks 5+6** — it parses a constant Task 5 Step 4 writes, and it is **never split** (its header box gives the four
reasons). **Task 9b runs after Task 9**, because it folds back a partition Task 9 has just re-derived. Task
numbers are stable so cross-references in Spec 017 and ADR 0033 stay valid; the **order** column is what an
executor follows.

| Order | Task | Modules that must be GREEN (constraint 8: *compiles against*) | Docker | Rough size |
|---|---|---|---|---|
| 1 | 1 | root | no | **large** — store + `Handle` (six exits) + classification + the gate quintet (**five** edits, not two — audit **R4-1**); the correctness core |
| 2 | 2 | root | no | medium — 8 cases over a 5-part fixture, incl. the M-6 regression case |
| 3 | 4 | root | no | small — godoc + one example |
| 4 | **5+6** | **root, postgres, mysql, sqlite, `harness`, `dbtest`** | Task 6 verification | **large, and LARGER since revision 4** — a breaking SPI change across 7 sites, 3 enforcement points, the harness call site (audit N-1), **plus the `*CountMembers` live+claimed count, the `locked_by` read and the leased/not-leased classification in all three dialects** (audit **NEW-7**) |
| 5 | **3** | root | no | small — one AST test over **three** files + three comments. **Runs HERE, after 5+6** (Step 0 is a hard order gate, now asserting **conditions, not a count** — audit **R4-4**) |
| 6 | 7 | harness, dbtest | **yes** | **medium→large since revision 4** — 7 conformance assertions (incl. the per-engine render, audit **NEW-5**, and the claimed-counting case, audit **NEW-7**) + the `*sql.Tx` dialect case + D-AS's `limit=0` guard |
| 7 | 8 | root | no | small |
| 8 | 9 | root | no | medium — **27 count sites in one file, GENERATED by Step 3's script** (audit **R4-2**), three vacuity probes |
| 9 | **9b** | none (docs) | no | **small — NEW in revision 5** (audit **R4-1**): fold the two new rows into Spec 016 §2.1 + §6 AC-5, **re-derived from the tree**, with two-way traceability |
| 10 | 10 | all 8 | yes | medium |

**Six modules touched** (root, postgres, mysql, sqlite, harness, dbtest); the delivery gate is all **eight**.

**If D-AF's revision-4 reversal is itself reversed** (Spec 017 §8 item 1 — back to `sql` counting live only), Task
6 loses the `COUNT(*)`, the `locked_by` read and the leased arm, and Task 7 loses two assertions — **and the
durable member table becomes unbounded again** (audit **NEW-7**). That is not a sizing trade; it is the increment's
purpose. **Do not reverse it for size.**

**If D-AG is reversed to enforcement (A)** (Spec 017 §8 item 2), Tasks 5+6+7 collapse into one small root-only
task and the increment drops from six touched modules to one. **That decision belongs before Task 5.**

**If D-AM is reversed** (Spec 017 §8 item 5), Task 1's B1-8/B1-9 and Task 2's `IsPermanent` assertions change, and
**D-AJ must be revisited in the same breath** — a transient classification plus a default cap is the hot spin
BLOCKER B-1 identified. **That decision belongs before Task 1.**
