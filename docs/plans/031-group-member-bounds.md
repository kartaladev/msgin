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

**Revision 2 — post-audit-round-1. NOT approved for implementation.**

**Round 1 verdict: NOT SAFE TO IMPLEMENT** — 3 BLOCKERs, 8 MAJORs, 10 MINORs, recorded immutably in
[`031-audit-round-1.md`](031-audit-round-1.md). This revision folds every finding back in. The three that reshaped
*this document* are:

| Finding | What it did to this plan |
|---|---|
| **B-2** | The class gate is exact set equality, so revision 1's "all gate edits in the final task" would have made **six of nine tasks commit a red suite**. The gate key + conformance row now land **in the same commit as the option**, and the task list is restructured around that. |
| **B-1** / **M-6** | The store's error classification (Spec 017 §3.3.1) and `Handle`'s snapshot branch (§3.3a) are new work that did not exist in revision 1. Task 1 grew; a new Task 3 appeared. |
| **B-3** / **M-1** | Every figure about `sizing_option_class_gate_test.go` is **re-derived at `d2c69fe`**, post-Plan-030. Revision 1's *"they share no file"* was false. |

🔴 **The design this plan executes was decided WITHOUT USER RATIFICATION**, and round 1's dispositions were taken
the same way. Every decision in [ADR 0033](../adrs/0033-group-member-bounds.md) (**D-AC** … **D-AQ**) is open to
reversal, and **three** now change this plan's size materially:

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

**A ROUND 2 AUDIT IS REQUIRED BEFORE IMPLEMENTATION.** [CLAUDE.md](../../CLAUDE.md) makes it a hard gate, and this
revision changed the *runtime contract*, not just prose: a fresh Opus subagent attacks the complete bundle —
[Spec 017](../specs/017-group-member-bounds.md) + [ADR 0033](../adrs/0033-group-member-bounds.md) + this plan —
**together**. Two rounds is this project's established norm; Plan 029 needed **five**.

> **🔴 CONCURRENT-WORK DEPENDENCIES — revision 1's claim here was FALSE (audit B-3).** Revision 1 said Plan 030 and
> Plan 031 *"share no file."* They share **four**, and Plan 030 has since **landed**
> (`7ab91cd`, `1a1c135`, `d2c69fe`):
>
> | File | Plan 030 did | Plan 031 does |
> |---|---|---|
> | `sizing_option_class_gate_test.go` | rewrote **135 lines** (`d2c69fe`) | Tasks 1, 5, 9 |
> | `adapter/memory/groupstore.go` | +1 line at `:93` (`1a1c135`) | Task 1 |
> | `adapter/database/sql/groupstore.go` | +1 line at `:207` (`1a1c135`) | Task 5 |
> | `adapter/memory/queuestore.go` | +1 line (`1a1c135`) | cited by Spec 017 §3.3 |
>
> **Consequence 1 — every line citation in this bundle is re-derived at `d2c69fe`.** The `groupstore.go` shifts are
> `+1` below the insertion point: the group-count arm is `:123-125` (was `:122-124`), the append is `:135`, the
> `checkRange` call is `:105-108`, `sql.GroupStore.Add` is `:250-276`.
>
> **Consequence 2 — the `fixed` arm now uses `1<<30`, not `1<<62`** (audit M-1). See Task 1 Step 5.
>
> **Consequence 3 — Plan 031 and [Plan 032](032-byte-cap-ceilings.md) SERIALIZE on
> `sizing_option_class_gate_test.go`.** Both target the same `sizingConformanceKeys` slice and the same arm table.
> Whichever lands second **re-derives the arm table from the tree**, never from a number written in its own plan.
> Confirm the state of both before branching; if 032 has landed first, rebase and re-derive rather than merge.

**Goal.** Deliver [Spec 017](../specs/017-group-member-bounds.md): a message group cannot grow without a stated
bound **whichever of the four release paths is in force**, because the bound moves from the release decision — where
three of four paths are opaque — to the **store**, which is the only site that can refuse a member *before*
retaining it.

**Architecture.** [ADR 0033](../adrs/0033-group-member-bounds.md) — **D-AC** (the bound lives at the accumulation
site), **D-AD** (two `WithMaxGroupMembers` options, one name in both packages; default `1<<16`, ceiling `1<<20`;
`checkRange` + `msgin.ErrInvalidCapacity`; mint no sentinel), **D-AE** (`msgin.ErrOverflowDropped`, wrapped),
**D-AM** (**classified by cause** — not-leased ⇒ `Permanent`, leased ⇒ transient), **D-AN** (**the live snapshot
rides out with the error; `Handle` re-evaluates the release**), **D-AO** (the cap check sits between the dedup
lookup and the dedup insert), **D-AF** (`memory` counts live+claimed, `sql` counts live), **D-AG** (SQL enforcement
in-transaction, `AddMember` takes `maxMembers`), **D-AP** (**per-dialect placement; the `*sql.Tx` caller-owned
precondition**), **D-AH** (the SPI states the bound), **D-AI** (godoc cross-references on the three unbounded
release paths), **D-AJ** (a default is legitimate here), **D-AK** (bounded-but-stuck is accepted), **D-AL** (the
class gate is extended by hand; its blind spot is stated, not widened), **D-AQ** (**the
`default ≥ completionSizeCeiling` invariant IS mechanically enforceable, by AST**).

**Predecessors this builds on, not re-argues.** [Spec 016](../specs/016-sizing-option-bounds.md) /
[ADR 0032](../adrs/0032-sizing-option-bounds.md) / [Plan 029](029-sizing-option-bounds.md): **D-X** (sentinel
reuse + wrap shape), **D-Z** (why 65,536), **D-AB** (the membership criterion), and the shipped `checkRange`
helper and class gate — **as rewritten by [Plan 030](030-post-029-maintenance.md) Task 2**.

**Tech stack.** Go 1.25 (`GOTOOLCHAIN=go1.25.13`). Touches **six** of the eight modules — root (packages `msgin`,
`routing`, `adapter/memory`, `adapter/database/sql`) plus `adapter/database/sql/{postgres,mysql,sqlite,harness}`
and `adapter/database/sql/dbtest` — and the delivery gate is all **eight**. *(Revision 1 said "five" in one place
and implied six in another — audit m-8. It is **six**.)* Tasks 6–7 need a **running Docker daemon**.

**Traceability.** Implements Spec 017; decided by ADR 0033; audited in
[`031-audit-round-1.md`](031-audit-round-1.md). Every commit carries `Spec: 017`, `Plan: 031`, `ADR: 0033`
trailers. Branch: `feat/group-member-bounds`, off `main`.

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
4. **The error shape is fixed and identical in both stores and all three dialects:**
   `fmt.Errorf("%w: %s: group %q holds %d members, limit %d", msgin.ErrOverflowDropped, site, key, n, max)`.
   **Wrapped in `msgin.Permanent` when the group is NOT leased, bare when it is** (D-AM — this REPLACES revision
   1's blanket "no `Permanent` wrap"). The construction-time shape is the shipped `checkRange` render, unchanged.
5. **No new exported sentinel, in any module** (D-AD / ADR 0032 D-X). A task that appears to need one has hit a
   design fault: **stop and escalate.**
6. **No test grows a group past 16 members** (Spec 017 AC-6). Ceiling values are exercised by **constructors
   only**. Growing a group to `1<<16` costs 8.6 s and 48.3 GiB of churn (Spec 016 §1.4) and the shipped
   `completionSizeCeiling` godoc already forbids it.
7. **Mutation-prove every new assertion** with a mutant that targets **that** assertion (the project's standing
   rule: *a killed mutant is the evidence, not a green run*). Each task carries a mutant table; record the killed
   mutant per case in the task's Evidence block. **A case that survives its own mutant is rewritten.**
8. **Each task is a green unit** — `GOWORK=off go test ./... -race -shuffle=on` passes in **every module it
   touched** before its commit. No WIP or broken-build commits.

   > 🔴 **This is why the class-gate edits are distributed rather than deferred (audit B-2).** Half 1 of
   > `sizing_option_class_gate_test.go` is **exact set equality in both directions**
   > (`assert.Equal(t, want, found, …)` at `:321-324`, not a subset check), and it is a **root-module** test that
   > walks the filesystem — so no import boundary shields it. **The moment `memory.WithMaxGroupMembers` exists on
   > disk, root's suite is red.** Revision 1 deferred every gate edit to a final task, which would have left six
   > of nine tasks committing a red suite. In this revision the gate key, the conformance row and the executable
   > counts land **inside the commit that adds the option** (Tasks 1 and 5), and "observe the RED first" is a
   > **within-task TDD step** — never a cross-task condition.

9. **Per-task commit is pre-authorized** by CLAUDE.md's plan-execution exception, once this plan is approved **and**
   an execution mode is chosen. `git push`, merges, tags and branch deletion still need explicit per-action
   approval.
10. **Never `git commit --amend` while the controller may be committing** (Plan 028 scar). Run `git log -1`
    immediately before any amend.
11. **Docs links are relative to the CITING file's directory.** A bare `[0033](0033-group-member-bounds.md)` from
    inside `docs/plans/` silently 404s. The pre-merge link gate (CLAUDE.md, both arms) is a Task 10 blocker.
12. **Re-derive, never transcribe, every figure about `sizing_option_class_gate_test.go`.** Plan 030 rewrote it and
    Plan 032 will rewrite it again. Run the gate and read its output; do not trust a number in this plan.

---

## The counted set — read D-AF and D-AO before writing either check

> **The two stores count different sets, deliberately, and pattern-matching one onto the other is the mistake this
> box exists to prevent.**

| Store | Counts | Site | Why |
|---|---|---|---|
| `memory.GroupStore` | `len(g.msgs)` — **live + claimed** | `Add`, between the dedup lookup (`groupstore.go:130`) and the dedup insert (`:133`) | that slice is what the **process** retains |
| `sql.GroupStore` | **live only** (`claimed_epoch IS NULL`) | inside the dialect's transaction, after that engine's serializing statement (Spec 017 §3.6.1) | claimed members are retained by the **database**, not the process |

**🔴 The `memory` check does NOT go "after the dedup branch" — revision 1's instruction lost messages** (audit M-6,
second defect; D-AO). The dedup branch **ends** with `g.ids[id] = struct{}{}` at `groupstore.go:133`; a check
placed after it records the member as *seen* and then rejects it, so the redelivery returns the dedup no-op with a
**nil** error and the source Acks a message that was never appended. The check goes **after the `seen` lookup and
before any mutation**, with the id hoisted so it also runs on the id-less path. **Spec 017 §3.4a carries the exact
shape — read it, do not reconstruct it.**

---

## Task 1 — the `memory` bound, its classification, and `Handle`'s release re-evaluation

**Files:** `adapter/memory/groupstore.go`, `routing/aggregator.go`, `adapter/memory/groupstore_test.go`,
`sizing_option_class_gate_test.go`.
**Module:** root.

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
- [ ] **Step 2 (RED — the gate first).** Add `"memory.WithMaxGroupMembers"` to `sizingConformanceKeys` and its
      conformance row to the **`fixed`** arm, and bump the executable counts this moves:
      `require.Len(t, tests, 19 → 20)` at `:753`. Run `go test -run TestSizingOptionClass -v .` and **observe both
      halves fail** (half 1: a key with no function; half 2: a row calling a constructor that does not exist —
      it will not compile, which is the same signal). **`require.Equal(t, 27, methodCount, …)` at `:335` does NOT
      move** (audit m-10).

      > 🔴 **The row asserts `1<<30`, not `1<<62`** (audit M-1). Plan 030 Task 2 split the literal by arm:
      > `fixed`/`rejects` → `1<<30`, rendering the architecture-independent decimal **1073741824**; `deferred` →
      > `1<<62` (int64, untouched); `safe` → `math.MaxInt`. `1<<62` does not fit an `int` on `GOARCH=386` and
      > **breaks compilation of the whole test binary there**. Nothing in this plan's per-task gate builds for 386,
      > so this will not be caught downstream — get it right here.

- [ ] **Step 3 (RED — the behavior).** Write the failing cases of the branch table below in
      `adapter/memory/groupstore_test.go`. All must fail before any production edit.
- [ ] **Step 4 (GREEN — the store).** Add `maxGroupMembersCeiling = 1 << 20`, the `maxGroupMembers` config field
      (default `1 << 16`), `WithMaxGroupMembers(n int)`, the `checkRange` call in `NewGroupStore` (mirroring
      `:105-108`), and the cap check **at Spec 017 §3.4a's exact position** — after the `seen` lookup, before any
      mutation, id hoisted. The check returns **`(liveSnapshot, err)`**, with `err` wrapped in `msgin.Permanent`
      **iff `!g.leased`** (D-AM/D-AN).
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
- [ ] **Step 7 (DOCS).** Godoc per Spec 017 §4 items 1, 2, 5 and 7: the option (range, default + its Spec 016 §3.4
      provenance, **the permanent/transient classification and why**, what it counts, and the claim-window
      rejection named as a zero-delay busy-wait under `RetryPolicy{}`), the ceiling constant (shaped like
      `maxGroupsCeiling`'s at `:55-62`, **carrying the cross-reference to `completionSizeCeiling`**), `Add`'s own
      godoc at `:112-117` (which today names only the group-count arm), and `Handle`'s snapshot branch.
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
| B1-7 | the **existing** group-count arm, now wrapped | `Add_rejects_a_new_key_beyond_MaxGroups` (extend the shipped case at `groupstore_test.go:30-39` to assert the render) | drop the wrap ⇒ the render assertion fails |
| **B1-8** | **`!g.leased` ⇒ `msgin.Permanent`** | `Add_over_cap_unleased_is_permanent` (asserts `msgin.IsPermanent(err)` **and** the `msgin: permanent: ` render — Spec 017 AC-2c) | drop the wrap ⇒ fails |
| **B1-9** | **`g.leased` ⇒ transient** | `Add_over_cap_while_leased_is_transient` (claim the group first, then `Add`) | wrap unconditionally ⇒ fails |
| **B1-10** | **`Add` returns the live snapshot WITH the error** | `Add_over_cap_returns_the_live_snapshot` | return `nil` ⇒ fails |
| **B1-11** | **`Handle`: non-nil group + non-nil error ⇒ re-evaluate the release** | `Handle_idless_redelivery_re_fires_the_release` (Spec 017 AC-1b) | delete the branch ⇒ M-6's deadlock returns and the case hangs at step 3 |
| **B1-12** | **`Handle`: release fires + drain succeeds ⇒ fresh TRANSIENT error** | same, step 3 asserts `!msgin.IsPermanent(err)`; step 4 asserts the retry is admitted | return the store's permanent error ⇒ step 4 never runs; return `nil` ⇒ the silent-loss assertion fails |
| **B1-13** | **`Handle`: release does NOT fire ⇒ the store's classification stands** | `Handle_over_cap_unreleasable_stays_permanent` | downgrade unconditionally ⇒ fails |
| **B1-14** | **`Handle`: `group == nil` ⇒ unchanged path** | `Handle_store_error_without_snapshot_is_returned_verbatim` (a stub store returning `(nil, err)`) | drop the nil guard ⇒ nil-deref panic |

**Also asserted (AC-2b, both ends):** the full construction-time render, not merely `errors.Is` —
`"msgin: capacity out of range: memory.WithMaxGroupMembers: 1048577 not in [1, 1048576]"` and the `0` twin.

**Evidence block to record:** the RED gate output (both halves), the RED behavioral failures, the fourteen killed
mutants, and `go test -cover` for `adapter/memory` and `routing` before and after.

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

**Files:** a new root blackbox test (`group_member_bound_invariant_test.go`), plus the cross-reference comments on
`routing/aggregator.go:33` and `adapter/memory/groupstore.go`'s new default constant.
**Module:** root.

> **NEW in revision 2 (audit M-5).** Revision 1 recorded this invariant as *"not mechanically enforced — both
> constants are unexported and in different packages, so no blackbox test can compare them"* and defended it with
> two prose comments and a one-time `grep`. **That claim is false**: `sizing_option_class_gate_test.go` is a root
> blackbox test that already parses every non-test `.go` file in all eight modules with `go/parser` (`:280`), and
> **unexportedness and package boundaries are irrelevant to a parser.**

- [ ] **Step 1.** Skills + `table-test`. Read `sizing_option_class_gate_test.go:254-300` (`scanSizingParamRepo`)
      with `gopls` — it is the model; **do not** import from it, write a focused parse.
- [ ] **Step 2 (RED).** Write the test against the *pre-Task-1* tree with a deliberately wrong expectation, to
      prove it reads real values off the AST rather than passing vacuously.
- [ ] **Step 3 (GREEN).** The test parses `routing/aggregator.go` and `adapter/memory/groupstore.go`, locates
      `const completionSizeCeiling` and the `maxGroupMembers` default **by name**, evaluates both `1 << N`
      `*ast.BinaryExpr` values, and asserts `defaultMaxGroupMembers >= completionSizeCeiling`. The failure message
      names both constants, both files and both values.
- [ ] **Step 4 (NON-VACUITY — mandatory).** The test **must fail loudly if either declaration is not found**, never
      pass on a zero value. Prove it: rename one constant locally, confirm the not-found guard fires (not a
      `0 >= 0` pass), and revert. *(The project's stored lesson: "measurement is only as good as its fixture".)*
- [ ] **Step 5.** Add the cross-reference comment to **each** constant naming the other and this test. They are now
      human-facing explanation, **not** the defence.
- [ ] **Step 6.** Mutation-prove; green; commit:
      `test(core): enforce the group-member default against the completion-size ceiling`.

| # | Branch | Covering case | Killing mutant |
|---|---|---|---|
| B3-1 | the relation holds | the assertion itself | change either literal ⇒ fails |
| B3-2 | a constant is missing/renamed | the not-found guard | delete the guard ⇒ a renamed constant yields a vacuous `0 >= 0` pass |

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
groupstore_unit_test.go}`, `sizing_option_class_gate_test.go`.
**Module:** root.

> **This task changes an exported SPI method signature.** `GroupDialect.AddMember` gains a trailing
> `maxMembers int`. Global constraint 5 forbids new exported **sentinels**, not this — the change is explicitly
> ratified by **D-AG**, and `GroupDialect`'s own godoc (`groupdialect.go:106`) reserves the right
> (*"a pre-1.0 (v0) contract that may still evolve"*). **This task leaves the three dialect modules RED**; Task 6
> makes them green. **Land Tasks 5 and 6 as ONE commit** so no commit is a broken build (Global constraint 8) —
> Task 5's steps are separated here for review clarity, not for commit boundaries.
>
> **The name is `sql.WithMaxGroupMembers`, matching `memory`** (D-AD, audit m-9). `adapter/database/sql`'s
> `WithGroup…` prefix is a **collision rule** (`WithGroupLeaseTTL` vs `WithLeaseTTL`, `WithGroupLockedBy` vs
> `WithLockedBy`), not a blanket convention; `MaxGroupMembers` collides with nothing. Do not rename it.

- [ ] **Step 1.** Skills + `golang-database`. With `gopls`' find-references, enumerate **every** `AddMember` site
      rather than trusting this plan's list. Expected **seven** (audit m-5 — revision 1 said five and omitted the
      two that matter most):

      | # | Site | Kind |
      |---|---|---|
      | 1 | `adapter/database/sql/groupdialect.go:126` | **interface declaration** |
      | 2 | `adapter/database/sql/groupstore.go:271` | **production call** — threads `s.maxGroupMembers` |
      | 3–5 | `postgres/groupdialect.go:80`, `mysql/groupdialect.go:75`, `sqlite/groupdialect.go:102` | implementations (Task 6) |
      | 6 | `harness/groupstore.go:345` | test-kit call (Task 7) |
      | 7 | `groupdialect_fake_test.go:137` | test fake |

- [ ] **Step 2 (RED — the gate first).** Add `"sql.WithMaxGroupMembers"` to `sizingConformanceKeys` and its
      conformance row to the **`fixed`** arm (asserting **`1<<30`** — see Task 1 Step 2's box), and bump
      `require.Len(t, tests, 20 → 21)` at `:753`. Observe both halves fail. **`methodCount` still does not move** —
      the dialect `AddMember` methods already match on `seq int64` (audit m-10).
- [ ] **Step 3 (RED — the behavior).** Failing cases for `sql.NewGroupStore(WithMaxGroupMembers(...))` at `0`,
      `1<<20` and `1<<20+1`, asserting the full `checkRange` render (AC-2b).
- [ ] **Step 4 (GREEN — the option).** Add `checkRange` to `adapter/database/sql/helpers.go` — a **fifth,
      independent, unexported copy**, identical to `adapter/memory/helpers.go:54`, carrying the same
      ADR 0031 **D-R** / Spec 014 §3.3 provenance comment the other four carry (`endpoint/helpers.go:97`,
      `routing/helpers.go:88`, `adapter/memory/helpers.go:54`, `adapter/http/helpers.go:64`). Add
      `maxGroupMembersCeiling = 1 << 20`, the config field (default `1 << 16`), and `WithMaxGroupMembers(n int)`
      with the `checkRange` call in `NewGroupStore`.
- [ ] **Step 5 (GREEN — the SPI).** Add `maxMembers int` to `GroupDialect.AddMember`; update its interface godoc
      to state the in-transaction enforcement contract, the `msgin.ErrOverflowDropped` rollback, **and D-AP's
      caller-owned-transaction precondition** (the bound is enforced by rollback only when the dialect owns the
      transaction; under a caller-supplied `*sql.Tx` the caller owns rollback). Thread `s.maxGroupMembers` through
      `sql.GroupStore.Add` (`:271`), and **propagate the dialect's snapshot past `classifyQueryErr`** rather than
      discarding it with the current `return nil, …` (`:273`).
- [ ] **Step 6.** Extend `fakeGroupDialect` to record the `maxMembers` it received, to return the overflow error on
      demand, **and to return rows alongside that error**, so the store's pass-through of both is provable without
      Docker.
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
      and after the member upsert**, count the live members and — if the count exceeds `maxMembers` — return the
      D-AE/D-AM error so the transaction rolls back:

      | Dialect | Wrapper | Serializing statement | Where the check goes |
      |---|---|---|---|
      | **postgres** | `pgRunInTx` (`groupdialect.go:52`) | `INSERT … ON CONFLICT (group_key) DO UPDATE … RETURNING created_at` (`:107-110`) — locks the conflicting row | after that upsert **and** after the member upsert |
      | **mysql** | `mysqlRunInTx` (`groupdialect.go:48`) | `INSERT … ON DUPLICATE KEY UPDATE group_key = group_key` (`:93-96`) — X-locks the group row | after that upsert **and** after the `INSERT IGNORE` member upsert |
      | **sqlite** | **`withImmediateConn`** (`groupdialect.go:52-77`) — dedicated `*sql.Conn`, raw `BEGIN IMMEDIATE`/`COMMIT`/`ROLLBACK`. **There is no `sqliteRunInTx`.** | `BEGIN IMMEDIATE` itself — a **database-wide** write lock. The group upsert is `DO NOTHING` + a separate `SELECT`; there is **no row lock and no `RETURNING`** | anywhere inside `withImmediateConn` after the member upsert |

      **After the member upsert in all three**, so an idempotent re-add of an existing id at exactly the cap is a
      no-op rather than an overflow.
- [ ] **Step 3 (GREEN — the bounded fetch and the snapshot).** Add **`LIMIT maxMembers+1`** to each dialect's
      live-member `SELECT` (`pgSelectMembers` / `mysqlSelectMembers` / `sqliteSelectMembers`,
      `claimed_epoch IS NULL`). On overflow, **filter the just-upserted `msgID` out of the materialized
      `[]MemberRow` and return the remaining rows WITH the error** (D-AN) — that is exactly the post-rollback live
      set, at **no extra query**.
- [ ] **Step 4 (GREEN — the error).** Return `msgin.Permanent(fmt.Errorf(…ErrOverflowDropped…))` — a `sql` live set
      is by definition unclaimed, so **every `sql` over-cap rejection is D-AM's not-leased case**. Import `msgin`
      in each dialect (zero net dependency — already required transitively via `msginsql`). Verify with
      `GOWORK=off go mod tidy && git diff --exit-code -- go.mod go.sum` in each of the three modules.
- [ ] **Step 5.** `GOWORK=off go test ./... -race -shuffle=on` green in **root + all three dialect modules**.
- [ ] **Step 6.** Commit **Tasks 5 + 6 together**:
      `feat(sql): bound a group's member count inside the dialect transaction`.

| # | Branch | Covering case (lands in Task 7's harness) | Killing mutant |
|---|---|---|---|
| B6-1 | live count > `maxMembers` → return + rollback | `member_cap_rejects_and_rolls_back` | return the error **after** the commit ⇒ the row-count assertion fails |
| B6-2 | live count ≤ `maxMembers` → commit normally | `member_cap_admits_up_to_the_cap` | off-by-one to `>=` ⇒ fails |
| B6-3 | re-add of an existing id at exactly the cap → no-op | `member_cap_readd_at_cap_is_a_noop` | move the check **before** the upsert ⇒ fails |
| **B6-4** | **the live snapshot is returned with the error, rejected member filtered** | `member_cap_returns_the_live_snapshot` | return empty `GroupRows` ⇒ fails |
| **B6-5** | **the rejection is `Permanent`** | `member_cap_rejection_is_permanent` | drop the wrap ⇒ fails |
| **B6-6** | **`*sql.Tx` Querier: no rollback, caller owns it** | `member_cap_under_caller_owned_tx` (Spec 017 AC-4b) | assume `pgRunInTx` rolled back ⇒ the in-transaction row-count assertion fails |

> **Task 6's branches are proven by Task 7's harness cases**, because a dialect's behavior is only observable
> against a real engine. Tasks 6 and 7 therefore land in **two commits but one green unit** — Task 6's commit must
> already pass Task 7's harness locally before it is made. If Docker is unavailable, **stop and escalate**; do not
> substitute a fake (`use-testcontainers`).

---

## Task 7 — the shared dialect conformance case (AC-4, AC-4b, AC-4c, AC-5)

**Files:** `adapter/database/sql/harness/groupstore.go`; run via `adapter/database/sql/dbtest`.
**Modules:** `harness`, `dbtest`. **Requires a running Docker daemon.**

- [ ] **Step 1.** Skills + **`use-testcontainers`**. Read the existing group conformance kit around
      `harness/groupstore.go:345` (the current `AddMember` call site) with `gopls`.
- [ ] **Step 2 (AC-4).** Add one conformance case, run by **all three** dialects, asserting five things:
      1. the `cap+1`-th live member ⇒ `errors.Is(err, msgin.ErrOverflowDropped)`;
      2. **the rollback, asserted not assumed** — a subsequent `ClaimGroup` returns exactly `cap` members **and** a
         direct member-row count over the table equals `cap`. *Without this half, D-AG's enforcement (C) is
         indistinguishable from the rejected (A);*
      3. re-adding an **existing** member id while the group sits at exactly `cap` is a **no-op returning the
         unchanged snapshot**, not an overflow;
      4. **the returned `GroupRows` is non-empty and holds exactly `cap` members** — the post-rollback live set,
         rejected member filtered (D-AN);
      5. **`msgin.IsPermanent(err)`** (D-AM).
- [ ] **Step 3 (AC-4b — the caller-owned transaction). NEW in revision 2 (audit M-2).** `BeginTx` on the caller's
      side, pass the `*sql.Tx` as the Querier, `Add` the `cap+1`-th member. Assert the rejection still fires;
      assert the member row **IS** present inside the still-open transaction (a `SELECT` on that same `*sql.Tx`
      sees `cap+1`); assert it is gone after the caller's own `Rollback`. **This documents D-AP's precondition as
      tested behavior rather than as a hope.** *(sqlite's `withImmediateConn` requires a `*sql.DB` Querier and
      errors on anything else — assert that error instead, and record the asymmetry.)*
- [ ] **Step 4 (AC-4c).** Assert the sentinel **and** the `Permanent` marker survive `classifyQueryErr`'s
      `SchemaExists` pass-through, with the table present.
- [ ] **Step 5.** `harness` has **no test files**, so `go test` there is a false pass — check it with
      `GOWORK=off go vet ./...` (CLAUDE.md). Run the real conformance through `dbtest`.

      > 🔴 **Do not add an exported function with an `int`/`int64` parameter to `harness`.** Half 1 of the class
      > gate walks the filesystem into leaf modules, and half 2 (a root test) **cannot import a leaf module** — so
      > such a key is an unsatisfiable gate failure by design. Verified clean today:
      > `grep -rnE "^func [A-Z][A-Za-z]*\(.*\b(int|int64)\b" adapter/database/sql/harness/*.go` returns nothing.
      > The cap value travels in the existing `TestKit`, not as a new exported parameter. **Re-run that grep before
      > committing.**
- [ ] **Step 6.** Mutation-prove B6-1…B6-6 from Task 6 against this harness (that is what makes them real).
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
- [ ] **Step 3.** Add one conformance case per first-party store, **driven through the `msgin.MessageGroupStore`
      interface type rather than the concrete type**, so the case is copyable by a third-party implementer. The
      `sql` half uses the `fakeGroupDialect` (no Docker) to assert the propagation contract; the real-engine proof
      is Task 7's.
- [ ] **Step 4.** Green; commit: `docs(core): state the per-group member bound on the MessageGroupStore SPI`.

**No new logic branch** — this task adds prose and re-drives covered branches through the interface.

---

## Task 9 — the class gate's stated blind spot and its count sweep (D-AL)

**Files:** `sizing_option_class_gate_test.go` (root).
**Module:** root.

> **The keys, the rows and the executable counts already landed in Tasks 1 and 5** (Global constraint 8's box —
> audit B-2). What remains here is prose that is *false* rather than merely stale, plus the vacuity probes.

- [ ] **Step 1.** Skills + `table-test`. Run the gate and record the **current** figures rather than trusting this
      plan (Global constraint 12): `GOTOOLCHAIN=go1.25.13 go test -run TestSizingOptionClass -v .` → expect **19**
      functions, **27** methods, both halves PASS. *(Baseline at `d2c69fe`, before this increment: 17 / 27 / PASS.)*
- [ ] **Step 2 (the FALSIFIED claim, not a stale number).** The ROOT-MODULE IMPORT BOUNDARY limitation at
      `:107-108` reads *"All 17 keys live in root-module packages today (endpoint, adapter/http, adapter/memory,
      channel, resilience, routing)."* `sql.WithMaxGroupMembers` lives in **`adapter/database/sql`**, which is not
      on that list. Add it, and update the count. **This is a corrected claim about the gate's own coverage**
      (audit M-7).
- [ ] **Step 3 (the remaining count sites — ten in all).** Sweep every site the increment moves, re-deriving each
      from the run in Step 1:

      | # | Site | States | Executable? |
      |---|---|---|---|
      | 1 | `:19` | "Every one of the **17** AST-discovered keys" | no |
      | 2 | `:38` | "9 + 1 + 3 + 6 = **19** rows = **17** AST keys + 2 manual" | no |
      | 3 | `:47`, `:55`, `:61` | per-arm counts "(9)", "(3)", "(6)" in the by-arm literal split | no |
      | 4 | `:83-85` | "Recv == nil yields **17**; ANY FuncDecl yields **44**" (**44 = 17+27 → 46**) | no |
      | 5 | `:92` | "the **27** excluded methods" | no |
      | 6 | `:107-108` | the package list — **Step 2** | no |
      | 7 | `:176`, `:210` | "cross-check the full **17**" / "unchanged at **17**" | no |
      | 8 | `:322` | the `assert.Equal` **message** ("**17-key** conformance set") | message only |
      | 9 | `:335` | `require.Equal(t, 27, methodCount, …)` — **DOES NOT MOVE** | **YES** |
      | 10 | `:753-754` | `require.Len(t, tests, …)` — already bumped to **21** by Tasks 1 and 5 | **YES** |

      Also check `:691`'s *"burst is the 17th key, positional"* — an **ordinal** into `sizingConformanceKeys`,
      valid only if the two new keys were appended **after** `resilience.NewTokenBucket`.
- [ ] **Step 4.** State, in the header, that **`methodCount` stays 27**: `GroupDialect.AddMember` gains an `int`
      parameter but all three dialect implementations already carry `seq int64` and are already counted — the
      header itself names `postgresGroupDialect.AddMember` at `:84-86` (audit m-10). **Do not bump `:335`.**
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

## Task 10 — whole-branch delivery gate

**Files:** `docs/HANDOVER.md`, `docs/specs/017-*`, `docs/adrs/0033-*`, this plan (status lines).

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
      Task 3's test is present, non-vacuous (Task 3 Step 4) and green — **that** is the evidence, and it needs no
      transcribed `grep` output.
- [ ] **Step 6.** Re-derive, do not transcribe, the figures the artifacts cite: the class-gate key count (expect
      **19**) and method count (expect **27**), the `ErrInvalidCapacity` producer count — expect **six**, and
      reconcile **by option name**: `memory.WithBuffer` (`adapter/memory/memory.go:82`), `memory.WithMaxGroups`
      (`groupstore.go:105`), `memory.WithCapacity` (`queuestore.go:114`), `routing.WithCompletionSize`
      (`aggregator.go:354`), plus the two new `WithMaxGroupMembers` sites — and the `ErrOverflowDropped` producer
      count. **Reconcile by name, never by count** (the project's standing `43 ≠ 43` lesson).
- [ ] **Step 7.** Update `docs/HANDOVER.md`: close §6 backlog item **7**; add the follow-ups Spec 017 §8 records
      (`memory`'s quadratic clone, `sql`'s per-`Add` full-group re-fetch, *cap-without-timeout* diagnostics,
      `classifyQueryErr`'s extra round-trip, and — **the root cause behind audit B-1** — that the default
      `msgin.RetryPolicy` neither logs nor bounds a transient fault); and refresh the artifact counts in
      `docs/HANDOVER.md` **and** in CLAUDE.md's Project status paragraph. **Re-derive every count with the commands
      that paragraph names — do not increment a number in this plan**, because Plan 030 has landed and Plan 032 may
      land in parallel. Count **distinct plan numbers, not files** — and note that this increment adds
      `031-audit-round-1.md` (and any round-2 record) as **satellites** of plan number 031, not new plan numbers.
- [ ] **Step 8.** Also fix CLAUDE.md's stale `reliability.go:46` citation for `IsPermanent`, which is
      `reliability.go:86-97` (audit m-1 — the same stale citation this bundle inherited).
- [ ] **Step 9.** Flip the status lines: Spec 017 → DELIVERED, ADR 0033 → ACCEPTED, this plan → DELIVERED — and
      **remove the "without user ratification" banners only if the user has by then ratified them.** If not, leave
      them and say so.
- [ ] **Step 10.** Stage, show the diff, and **wait for explicit approval** before the final commit. `git push`, the
      merge and the branch deletion each need their own approval (Global constraint 9).

---

## Sizing

| Task | Modules touched | Docker | Rough size |
|---|---|---|---|
| 1 | root | no | **large** — store + `Handle` + classification + gate pair; the correctness core |
| 2 | root | no | medium — 8 cases over a 5-part fixture, incl. the M-6 regression case |
| 3 | root | no | small — one AST test + two comments |
| 4 | root | no | small — godoc + one example |
| 5+6 | root, postgres, mysql, sqlite | Task 6 verification | **large** — a breaking SPI change across 7 sites, 3 different enforcement points |
| 7 | harness, dbtest | **yes** | medium — 3 conformance halves + the `*sql.Tx` case |
| 8 | root | no | small |
| 9 | root | no | medium — ten count sites in one file, three vacuity probes |
| 10 | all 8 | yes | medium |

**Six modules touched** (root, postgres, mysql, sqlite, harness, dbtest); the delivery gate is all **eight**.

**If D-AG is reversed to enforcement (A)** (Spec 017 §8 item 2), Tasks 5+6+7 collapse into one small root-only
task and the increment drops from six touched modules to one. **That decision belongs before Task 5.**

**If D-AM is reversed** (Spec 017 §8 item 5), Task 1's B1-8/B1-9 and Task 2's `IsPermanent` assertions change, and
**D-AJ must be revisited in the same breath** — a transient classification plus a default cap is the hot spin
BLOCKER B-1 identified. **That decision belongs before Task 1.**
