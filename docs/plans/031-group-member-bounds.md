# Plan 031 — A message group's member count is bounded at the store

> **For agentic workers:** REQUIRED SUB-SKILL: `superpowers:subagent-driven-development` (the project default) or
> `superpowers:executing-plans`. Steps use checkbox (`- [ ]`) syntax.
>
> **Go-skills hard rule ([CLAUDE.md](../../CLAUDE.md), restated here because `superpowers:writing-plans` omits
> it):** every task starts from **`cc-skills-golang:golang-how-to`**, the always-on orchestrator, which routes this
> increment to `golang-safety` (unbounded growth and admission checks), `golang-error-handling` (sentinel reuse,
> wrapping, `errors.Is`), `golang-design-patterns` (functional options), `golang-database` (the in-transaction
> dialect enforcement of Tasks 5–6), `golang-testing` and `golang-documentation`. Load the primary skill **plus all
> applicable secondary skills together, up front** — do not work from memory.
> **`superpowers:test-driven-development`** governs every task: red → green → refactor, failing test first, never
> implementation ahead of a failing test. **`gopls`** (via the `LSP` tool) for all navigation, diagnostics and
> refactoring — go-to-definition, find-references, rename, post-edit diagnostics — **not `grep`** when reasoning
> about Go symbols. The project-local overrides apply and beat samber's guidance where they conflict:
> **`table-test`** (assert-closure form, never `want`/`wantErr` fields; `ctx` modifier; `t.Context()`),
> **`use-mockgen`** (uber-go/mock, `--typed`, alongside the interface — applies to Task 4's dialect double only if
> a generated mock replaces the existing hand-written `fakeGroupDialect`), and **`use-testcontainers`**
> (Tasks 5–6 use the shipped Docker-backed `dbtest`/`harness` runners — **never** a mock, in-memory fake or shared
> dev database for the dialect conformance work).
>
> **This plan is deliberately thin** (Plan 024/026/028/029 precedent): signatures, positions, branch coverage and
> commit boundaries — **no embedded implementations**. Write the code TDD-first from the tables below.

**Revision 1 — pre-audit. NOT approved for implementation.**

🔴 **The design this plan executes was decided WITHOUT USER RATIFICATION.** The user was away when the bundle was
drafted. Every decision in [ADR 0033](../adrs/0033-group-member-bounds.md) (**D-AC** … **D-AL**) is open to
reversal, and **two of them change this plan's size materially**:

- **D-AG** (SQL enforcement inside the dialect's transaction) is what makes Tasks 4–6 exist at all. The cheap
  alternative — count in `sql.GroupStore.Add` after `AddMember` returns — collapses Tasks 4–6 into a single task
  but bounds nothing durable. **If that reversal is coming, it must land before Task 4 starts, not at Task 6.**
- **D-AJ** (ship 65,536 as a *default*, not opt-in) is the behavioral break. Reversing it changes one constant per
  store and one paragraph of godoc.

**The adversarial design audit has not run.** [CLAUDE.md](../../CLAUDE.md) makes it a hard gate: a fresh Opus
subagent attacks the complete bundle — [Spec 017](../specs/017-group-member-bounds.md) +
[ADR 0033](../adrs/0033-group-member-bounds.md) + this plan — **together**, before any implementation code.
Two rounds is this project's established norm; Plan 029 needed **five**.

> **Plan number — re-derived, not assumed.** 029 is the last *delivered* plan, but **030 is TAKEN**:
> [`030-post-029-maintenance.md`](030-post-029-maintenance.md) was drafted concurrently with this bundle and
> covers `docs/HANDOVER.md` §6 backlog items 2, 5 and 8. This plan is therefore **031**, and the two are
> **independent** — 030 touches `adapter/http`, godoc wording and test-file constants; 031 touches
> `routing`, `adapter/memory` and `adapter/database/sql`. **They share no file.** Confirm that is still true
> before branching (`git log --oneline main..` on both branches), and if 030 has landed first, rebase rather
> than merge.

**Goal.** Deliver [Spec 017](../specs/017-group-member-bounds.md): a message group cannot grow without a stated
bound **whichever of the four release paths is in force**, because the bound moves from the release decision — where
three of four paths are opaque — to the **store**, which observes every member.

**Architecture.** [ADR 0033](../adrs/0033-group-member-bounds.md) — **D-AC** (the bound lives at the accumulation
site), **D-AD** (two `WithMaxGroupMembers` options; default `1<<16`, ceiling `1<<20`; `checkRange` +
`msgin.ErrInvalidCapacity`; mint no sentinel), **D-AE** (`msgin.ErrOverflowDropped`, wrapped, not `Permanent`),
**D-AF** (`memory` counts live+claimed, `sql` counts live), **D-AG** (SQL enforcement in-transaction, `AddMember`
takes `maxMembers`), **D-AH** (the SPI states the bound), **D-AI** (godoc cross-references on the three unbounded
release paths), **D-AJ** (a default is legitimate here), **D-AK** (bounded-but-stuck is accepted), **D-AL** (the
class gate is extended by hand; its blind spot is stated, not widened).

**Predecessors this builds on, not re-argues.** [Spec 016](../specs/016-sizing-option-bounds.md) /
[ADR 0032](../adrs/0032-sizing-option-bounds.md) / [Plan 029](029-sizing-option-bounds.md): **D-X** (sentinel
reuse + wrap shape), **D-Z** (why 65,536), **D-AB** (the membership criterion), and the shipped `checkRange`
helper and class gate.

**Tech stack.** Go 1.25 (`GOTOOLCHAIN=go1.25.13`). Touches **five** of the eight modules — root (packages `msgin`,
`routing`, `adapter/memory`, `adapter/database/sql`) plus `adapter/database/sql/{postgres,mysql,sqlite,harness}` —
and the delivery gate is all **eight**. Tasks 5–6 need a **running Docker daemon**.

**Traceability.** Implements Spec 017; decided by ADR 0033. Every commit carries `Spec: 017`, `Plan: 031`,
`ADR: 0033` trailers. Branch: `feat/group-member-bounds`, off `main`.

---

## Global constraints

1. **Start every task from `cc-skills-golang:golang-how-to`**, plus the secondary `golang-*` skills it routes to
   (header note). **TDD via `superpowers:test-driven-development`** — failing test first, always. **`gopls` for
   navigation and refactoring**, not text search. **`table-test` / `use-mockgen` / `use-testcontainers` override**
   samber's testing guidance wherever they conflict. This is restated per-task in each task's first step; it is
   **not** delegated to an SDD dispatch prompt.
2. **Blackbox tests only** — `package <pkg>_test`, exercising the exported API. No whitebox fallback. A test that
   seems to need an unexported helper is rewritten through the public surface.
3. **Assert-closure tables** — every case carries `assert func(t *testing.T, …)`; never `want`/`wantErr` fields.
   `t.Context()`, never `context.Background()`.
4. **The error shape is fixed and identical in both stores and all three dialects:**
   `fmt.Errorf("%w: %s: group %q holds %d members, limit %d", msgin.ErrOverflowDropped, site, key, n, max)`.
   **No `msgin.Permanent` wrap** (D-AE — it is transient by design). The construction-time shape is the shipped
   `checkRange` render, unchanged.
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
9. **Per-task commit is pre-authorized** by CLAUDE.md's plan-execution exception, once this plan is approved **and**
   an execution mode is chosen. `git push`, merges, tags and branch deletion still need explicit per-action
   approval.
10. **Never `git commit --amend` while the controller may be committing** (Plan 028 scar). Run `git log -1`
    immediately before any amend.
11. **Docs links are relative to the CITING file's directory.** A bare `[0033](0033-group-member-bounds.md)` from
    inside `docs/plans/` silently 404s. The pre-merge link gate (CLAUDE.md, both arms) is a Task 9 blocker.

## The counted set — read D-AF before writing either check

> **The two stores count different sets, deliberately, and pattern-matching one onto the other is the mistake this
> box exists to prevent.**

| Store | Counts | Site | Why |
|---|---|---|---|
| `memory.GroupStore` | `len(g.msgs)` — **live + claimed** | `Add`, **before** the append at `groupstore.go:134` | that slice is what the **process** retains |
| `sql.GroupStore` | **live only** (`claimed_epoch IS NULL`) | inside the dialect's transaction, **after** the group-row lock | claimed members are retained by the **database**, not the process |

**The `memory` check must sit BEFORE the append and AFTER the dedup branch.** Before the append, so the maximum
attainable `len(g.msgs)` is exactly the cap (Spec 017 §3.5's arithmetic). After the dedup branch
(`groupstore.go:127-133`), so an idempotent re-add of an existing member id at exactly the cap is still a **no-op
returning the unchanged snapshot**, not an overflow — Task 1's mutant M1-3.

---

## Task 1 — `memory.WithMaxGroupMembers`, and the cap in `Add`

**Files:** `adapter/memory/groupstore.go`, `adapter/memory/groupstore_test.go` (new cases).
**Module:** root.

- [ ] **Step 1.** Load `cc-skills-golang:golang-how-to` (→ `golang-safety`, `golang-error-handling`,
      `golang-design-patterns`, `golang-documentation`) and the `table-test` override. Read `groupstore.go:55-136`
      with `gopls`, not `grep`, and confirm the three anchors this task edits still read as Spec 017 §1.2 records
      them: the `maxGroupsCeiling` constant (`:62`), the group-count admission arm (`:122-124`), the append
      (`:134`).
- [ ] **Step 2 (RED).** Write the failing cases of the table below. All four must fail before any production edit.
- [ ] **Step 3 (GREEN).** Add `maxGroupMembersCeiling = 1 << 20`, the `maxGroupMembers` config field (default
      `1 << 16`), `WithMaxGroupMembers(n int)`, the `checkRange` call in `NewGroupStore` (mirroring `:104-107`),
      and the cap check in `Add`, **before the append and after the dedup branch**.
- [ ] **Step 4 (GREEN).** Upgrade the **existing bare** `return nil, msgin.ErrOverflowDropped` (`:123`) to the same
      wrapped shape (D-AE, fix the class). **First verify no test asserts the bare string** —
      `grep -rn 'message dropped by overflow policy' --include='*_test.go' .` and confirm every hit is an
      `errors.Is` / `require.ErrorIs`, not an `EqualError`.
- [ ] **Step 5 (DOCS).** Godoc per Spec 017 §4 items 1, 2 and 5: the option (range, default + its Spec 016 §3.4
      provenance, `ErrOverflowDropped` at the boundary, **what it counts**, and the claim-window rejection), the
      ceiling constant (shaped like `maxGroupsCeiling`'s at `:55-62`), **and** `Add`'s own godoc at `:112-116`,
      which today names only the group-count arm and becomes **incomplete** the moment this task lands.
- [ ] **Step 6.** Mutation-prove every case (table below). `GOWORK=off go test ./... -race -shuffle=on` green.
      Coverage on `adapter/memory` ≥ 85% and every branch below covered.
- [ ] **Step 7.** Commit: `feat(memory): bound a message group's member count in GroupStore.Add`.

**Hot-path branches introduced, and the case that covers each** (CLAUDE.md's test-coverage gate — a branch with no
test is a delivery blocker):

| # | Branch | Covering case | Killing mutant |
|---|---|---|---|
| B1-1 | `len(g.msgs) >= s.maxGroupMembers` → reject | `Add_rejects_the_cap_plus_one_member` | delete the arm ⇒ case fails |
| B1-2 | the same condition **false** → append proceeds | `Add_admits_members_up_to_the_cap` (cap = 4, four `Add`s all succeed, 4th snapshot has 4 members) | invert to `>` ⇒ the group admits `cap+1`; **this is the one no other case catches** |
| B1-3 | dedup branch wins at exactly the cap | `Add_readding_an_existing_id_at_the_cap_is_a_noop` | move the cap check **above** the dedup branch ⇒ case fails |
| B1-4 | `checkRange` upper arm in `NewGroupStore` | `NewGroupStore_rejects_ceiling_plus_one` | delete the arm ⇒ case fails |
| B1-5 | `checkRange` lower arm | `NewGroupStore_rejects_zero` | change `lo` to `0` ⇒ case fails |
| B1-6 | `checkRange` in-range → default/explicit accepted | `NewGroupStore_accepts_the_ceiling` **and** `NewGroupStore_default_is_usable` | make `checkRange` always error ⇒ both fail |
| B1-7 | the **existing** group-count arm, now wrapped | `Add_rejects_a_new_key_beyond_MaxGroups` (extend the shipped case at `groupstore_test.go:30-39` to assert the render) | drop the wrap ⇒ the render assertion fails |

**Also asserted (AC-2b, both ends):** the full render, not merely `errors.Is` —
`"msgin: capacity out of range: memory.WithMaxGroupMembers: 1048577 not in [1, 1048576]"` and the `0` twin.

**Evidence block to record:** the four RED failures, the seven killed mutants, `go test -cover` for
`adapter/memory` before and after.

---

## Task 2 — the bound holds for ALL FOUR release paths (the increment's reason to exist)

**Files:** `routing/aggregator_test.go` (new cases; blackbox `package routing_test`).
**Module:** root.

> **This is Spec 017 AC-1, and it is the task that distinguishes this increment from Plan 029.** A test that
> exercises only `WithMaxGroupMembers` in isolation passes against an implementation that bounds nothing new —
> path 1 was already bounded.

- [ ] **Step 1.** Load the skills (Global constraint 1) + `table-test`. Read `routing/aggregator.go:100-160` and
      `:404-440` with `gopls`.
- [ ] **Step 2 (RED).** One table, four cases, over a `memory.GroupStore` built with `WithMaxGroupMembers(4)`,
      each configuring a different release path and each asserting that `Handle`'s 5th message returns an error
      satisfying `errors.Is(err, msgin.ErrOverflowDropped)`:

  | Case | Aggregator configuration |
  |---|---|
  | `completion_size_above_the_cap` | `WithCompletionSize(1000)` |
  | `release_strategy_never_releases` | `WithReleaseStrategy(func(msgin.MessageGroup) (bool, error) { return false, nil })` |
  | `release_when_never_releases` | `WithReleaseWhen(func(msgin.MessageGroup) bool { return false })` |
  | `default_path_header_driven` | **no** release option; the first message carries `msgin.HeaderSequenceSize = 1000` |

- [ ] **Step 3.** Add the boundary-arithmetic cases (AC-3.1), which pin Spec 017 §3.5 in **both** directions:
      `WithMaxGroupMembers(4)` + `WithCompletionSize(4)` over 5 messages ⇒ the **4th** `Handle` releases (observed
      via the output channel's subscriber) and the 5th starts a fresh group; `WithMaxGroupMembers(4)` +
      `WithCompletionSize(5)` ⇒ the **5th** returns `ErrOverflowDropped` and **nothing** is released.
- [ ] **Step 4.** Add the ceiling-level, **constructor-only** defence (AC-3.2):
      `routing.NewAggregator(..., WithCompletionSize(1<<16))`, `memory.NewGroupStore()` and
      `memory.NewGroupStore(WithMaxGroupMembers(1<<16))` all construct without error. **No members are added.**
- [ ] **Step 5.** Mutation-prove; green; commit:
      `test(routing): prove the store bound holds for all four release paths`.

> 🔴 **Fixture, measured — five parts, not two** (Spec 016 §6 AC-5's finding, re-verified against
> `aggregator.go:340-370`). `NewAggregator` needs `store`, `fn`, **`WithOutputChannel(ch)`** *and*
> **`WithCorrelationStrategy(fixedKey)`**, plus **`ch.Subscribe(counter)`** for release to be observable at all.
> A bare `NewAggregator(store, fn)` returns `msgin: aggregator output channel is nil`, and the default correlator
> returns `Permanent(msgin.ErrNoCorrelation)` for a message with no correlation header. **Write the fixture once as
> a helper in the test file; every case below reuses it.**

| # | Branch | Covering case | Killing mutant |
|---|---|---|---|
| B2-1 | `Handle` propagates `store.Add`'s error unchanged (`aggregator.go:412-415`) | all four AC-1 cases | swallow the error (`return nil`) ⇒ all four fail |
| B2-2 | release fires at exactly the cap | `completion_size_equals_cap_releases_at_the_cap` | move the memory cap check **after** the append ⇒ the 5th admits and the assertion on "nothing released" flips |

---

## Task 3 — godoc cross-references on the three unbounded release paths (D-AI)

**Files:** `routing/aggregator.go` (godoc only), `headers.go` or wherever `msgin.HeaderSequenceSize` is declared,
`routing/example_test.go` (one runnable example).
**Module:** root.

> **The project's stored lesson applies directly here:** *"docs can contradict the code they describe — all three
> fix rounds in Plan 028 were godoc, not logic."* **Read each sentence against the constructor, not for
> plausibility.**

- [ ] **Step 1.** Skills + `golang-documentation`. Locate `msgin.HeaderSequenceSize`'s declaration with `gopls`
      (`defaultRelease` reads it at `aggregator.go:222`), rather than assuming the file.
- [ ] **Step 2.** Edit four godocs per Spec 017 §3.8 / §4 item 4:
      `WithCompletionSize` (pointer to the store bound), `WithReleaseStrategy` (**it bypasses
      `completionSizeCeiling`; the store bound is what stops the group**), `WithReleaseWhen` (same, inherited),
      and — since `defaultRelease` is unexported — `msgin.HeaderSequenceSize` **and** `routing.NewAggregator`.
- [ ] **Step 3 (RUNNABLE COMPANION).** Add one `Example` test showing a `WithReleaseWhen` strategy that never
      releases, over a small `WithMaxGroupMembers`, producing the `ErrOverflowDropped` outcome — so the prose has a
      compilable proof next to it and the doc edit is not orphaned from a test.
- [ ] **Step 4.** `go test -run '^Example' ./...` green; `go vet ./...`; `gofmt -l .` empty.
- [ ] **Step 5.** Commit: `docs(routing): cross-reference the store-level group member bound`.

**No new logic branch.** The example's own path is covered by Task 2's B2-1.

---

## Task 4 — `sql.WithMaxGroupMembers`, `checkRange`, and the `AddMember` signature

**Files:** `adapter/database/sql/{groupstore.go,groupdialect.go,helpers.go,groupdialect_fake_test.go,
groupstore_unit_test.go}`.
**Module:** root.

> **This task changes an exported SPI method signature.** `GroupDialect.AddMember` gains a trailing
> `maxMembers int`. Global constraint 5 forbids new exported **sentinels**, not this — the change is explicitly
> ratified by **D-AG**, and `GroupDialect`'s own godoc (`groupdialect.go:106`) reserves the right
> (*"a pre-1.0 (v0) contract that may still evolve"*). **This task leaves the three dialect modules RED**; Task 5
> makes them green. **Land Tasks 4 and 5 as ONE commit** so no commit is a broken build (Global constraint 8) —
> Task 4's steps are separated here for review clarity, not for commit boundaries.

- [ ] **Step 1.** Skills + `golang-database`. With `gopls`' find-references, enumerate **every** `AddMember` call
      site rather than trusting this plan's list. Expected five: `postgres`, `mysql`, `sqlite`,
      `harness/groupstore.go:345`, `groupdialect_fake_test.go:137`.
- [ ] **Step 2 (RED).** Failing cases for `sql.NewGroupStore(WithMaxGroupMembers(...))` at `0`, `1<<20` and
      `1<<20+1`, asserting the full `checkRange` render (AC-2b).
- [ ] **Step 3 (GREEN).** Add `checkRange` to `adapter/database/sql/helpers.go` — a **fifth, independent,
      unexported copy**, identical to `adapter/memory/helpers.go:54`, carrying the same ADR 0031 **D-R** /
      Spec 014 §3.3 provenance comment the other four carry. Add `maxGroupMembersCeiling = 1 << 20`, the config
      field (default `1 << 16`), `WithMaxGroupMembers(n int)`, and the `checkRange` call in `NewGroupStore`.
- [ ] **Step 4 (GREEN).** Add `maxMembers int` to `GroupDialect.AddMember`, update its interface godoc to state the
      in-transaction enforcement contract and the `msgin.ErrOverflowDropped` rollback, thread `s.maxGroupMembers`
      through `sql.GroupStore.Add`, and update `fakeGroupDialect`.
- [ ] **Step 5.** Extend `fakeGroupDialect` to record the `maxMembers` it received and to return the overflow error
      on demand, so the store's **pass-through** is provable without Docker.
- [ ] **Step 6.** Mutation-prove; commit **with Task 5** (below).

| # | Branch | Covering case | Killing mutant |
|---|---|---|---|
| B4-1 | `checkRange` upper arm in `sql.NewGroupStore` | `NewGroupStore_rejects_ceiling_plus_one` | delete ⇒ fails |
| B4-2 | `checkRange` lower arm | `NewGroupStore_rejects_zero` | `lo` → `0` ⇒ fails |
| B4-3 | in-range → accepted | `NewGroupStore_accepts_the_ceiling` | always-error ⇒ fails |
| B4-4 | `Add` passes the configured cap to the dialect | `Add_passes_maxMembers_to_the_dialect` (fake records it) | pass a literal `0`/`math.MaxInt` ⇒ fails |
| B4-5 | `Add` propagates the dialect's overflow error unchanged | `Add_propagates_dialect_overflow` (fake returns it) | wrap it in `Permanent` ⇒ the `IsPermanent` assertion fails |

---

## Task 5 — in-transaction enforcement in postgres, mysql and sqlite (D-AG)

**Files:** `adapter/database/sql/{postgres,mysql,sqlite}/groupdialect.go`.
**Modules:** `postgres`, `mysql`, `sqlite`.

- [ ] **Step 1.** Skills + `golang-database` + **`use-testcontainers`** (no mocks, no in-memory fakes, no shared
      dev DB for this work). Read each dialect's `AddMember` with `gopls`; `postgres/groupdialect.go:80` is the
      reference shape.
- [ ] **Step 2 (GREEN, per dialect).** Inside the existing transaction, **after** the statement that takes the
      group row lock and **after** the member upsert, count the live members and — if the count exceeds
      `maxMembers` — return the D-AE error so the existing `RunInTx` wrapper **rolls back**.
      **The ordering is load-bearing:** after the lock, so the check is atomic across processes (Spec 017 §7.1);
      after the upsert, so an idempotent re-add of an existing id at exactly the cap is a no-op rather than an
      overflow.
- [ ] **Step 3.** Import `msgin` in each dialect (zero net dependency — the module is already required
      transitively via `msginsql`). Verify with `GOWORK=off go mod tidy && git diff --exit-code -- go.mod go.sum`
      in each of the three modules.
- [ ] **Step 4.** `GOWORK=off go test ./... -race -shuffle=on` green in **root + all three dialect modules**.
- [ ] **Step 5.** Commit **Tasks 4 + 5 together**:
      `feat(sql): bound a group's member count inside the dialect transaction`.

| # | Branch | Covering case (lands in Task 6's harness) | Killing mutant |
|---|---|---|---|
| B5-1 | live count > `maxMembers` → return + rollback | `member_cap_rejects_and_rolls_back` | return the error **after** the commit ⇒ the row-count assertion fails |
| B5-2 | live count ≤ `maxMembers` → commit normally | `member_cap_admits_up_to_the_cap` | off-by-one to `>=` ⇒ fails |
| B5-3 | re-add of an existing id at exactly the cap → no-op | `member_cap_readd_at_cap_is_a_noop` | move the check **before** the upsert ⇒ fails |

> **Task 5's branches are proven by Task 6's harness cases**, because a dialect's behavior is only observable
> against a real engine. Tasks 5 and 6 therefore land in **two commits but one green unit** — Task 5's commit must
> already pass Task 6's harness locally before it is made. If Docker is unavailable, **stop and escalate**; do not
> substitute a fake (`use-testcontainers`).

---

## Task 6 — the shared dialect conformance case (AC-4, AC-5)

**Files:** `adapter/database/sql/harness/groupstore.go`; run via `adapter/database/sql/dbtest`.
**Modules:** `harness`, `dbtest`. **Requires a running Docker daemon.**

- [ ] **Step 1.** Skills + **`use-testcontainers`**. Read the existing group conformance kit around
      `harness/groupstore.go:345` (the current `AddMember` call site) with `gopls`.
- [ ] **Step 2.** Add one conformance case, run by **all three** dialects, asserting the three halves of AC-4:
      1. the `cap+1`-th live member ⇒ `errors.Is(err, msgin.ErrOverflowDropped)`;
      2. **the rollback, asserted not assumed** — a subsequent `ClaimGroup` returns exactly `cap` members **and** a
         direct member-row count over the table equals `cap`. *Without this half, D-AG's enforcement (C) is
         indistinguishable from the rejected (A);*
      3. re-adding an **existing** member id while the group sits at exactly `cap` is a **no-op returning the
         unchanged snapshot**, not an overflow.
- [ ] **Step 3.** `harness` has **no test files**, so `go test` there is a false pass — check it with
      `GOWORK=off go vet ./...` (CLAUDE.md). Run the real conformance through `dbtest`.
- [ ] **Step 4.** Mutation-prove B5-1/2/3 from Task 5 against this harness (that is what makes them real).
- [ ] **Step 5.** Commit: `test(sql): add the group member-cap dialect conformance case`.

---

## Task 7 — the SPI contract, and interface-level conformance on both stores (D-AH)

**Files:** `groupstore.go` (root, godoc only), `adapter/memory/groupstore_test.go`,
`adapter/database/sql/groupstore_unit_test.go`.
**Module:** root.

- [ ] **Step 1.** Skills + `golang-documentation` + `golang-structs-interfaces`.
- [ ] **Step 2.** Add Spec 017 §3.7's paragraph to `msgin.MessageGroupStore.Add`'s godoc (`groupstore.go:41-52`):
      the MUST-bound requirement, the MUST-report-`ErrOverflowDropped` requirement, the reason the release strategy
      cannot supply it, and the D-AF note that the counted set is implementation-specific and must be stated.
- [ ] **Step 3.** Add one conformance case per first-party store, **driven through the `msgin.MessageGroupStore`
      interface type rather than the concrete type**, so the case is copyable by a third-party implementer. The
      `sql` half uses the `fakeGroupDialect` (no Docker) to assert the propagation contract; the real-engine proof
      is Task 6's.
- [ ] **Step 4.** Green; commit: `docs(core): state the per-group member bound on the MessageGroupStore SPI`.

**No new logic branch** — this task adds prose and re-drives covered branches through the interface.

---

## Task 8 — extend the class gate, and STATE its blind spot (D-AL)

**Files:** `sizing_option_class_gate_test.go` (root).
**Module:** root.

- [ ] **Step 1.** Skills + `table-test`. Run the gate first and record the **current** figures rather than
      trusting this plan: `GOTOOLCHAIN=go1.25.13 go test -run TestSizingOptionClass -v .` → **17** functions,
      **27** methods, both halves PASS.
- [ ] **Step 2 (RED).** Run it again after Tasks 1 and 4 have landed: half 1 must now **FAIL**, reporting
      `memory.WithMaxGroupMembers` and `sql.WithMaxGroupMembers` as unlisted. **That failure is the gate working
      and must be observed before it is fixed** — if it does not fail, the gate is not covering the new options and
      that is a blocker, not a convenience.
- [ ] **Step 3 (GREEN).** Add both keys to `sizingConformanceKeys` (17 → **19**) and one conformance row each in
      the **`fixed`** arm (both constructors reject `1<<62`). Update every count the file states, in **all** of
      the header comment, the arm table and the pairwise `arm` map — arms become
      **11 fixed + 1 rejects + 3 deferred + 6 safe = 21 rows = 19 AST keys + 2 manual rows.**
- [ ] **Step 4.** Add the **fifth accepted limitation** to the file header, verbatim from ADR 0033 **D-AL**: a
      bound that does not arrive as an integer parameter is invisible — `*ast.FuncType`, a named func type, or a
      **message header** — naming `WithReleaseWhen`, `WithReleaseStrategy` and `defaultRelease`, and pointing at
      Spec 017 §1.4 and the store as their enforcement site.
- [ ] **Step 5.** State, in the same header, that `GroupDialect.AddMember`'s new `int` parameter is a **method**
      (excluded by the ratified `Recv == nil` boundary) and is **not** a class member under D-AB — `maxMembers`
      *is* the bound, not a bounded quantity — so no manual row is required.
- [ ] **Step 6 (AC-10, vacuity).** Prove **both** halves fire, and prove the probe **covers**, not merely that it
      fires: plant a third `WithMaxGroupMembers`-shaped option in **`adapter/database/sql`** (the module this
      increment newly touches — *not* root; Plan 028's `apidiff` blindness came from probing only root) and confirm
      half 1 reports exactly one extra key; flip one conformance row's `arm` and confirm half 2 reports the
      pairwise mismatch. **Revert both probes and re-run.**
- [ ] **Step 7.** Commit: `test(core): extend the sizing class gate and state its func-typed blind spot`.

| # | Branch | Covering case | Killing mutant |
|---|---|---|---|
| B8-1 | half 1 set-difference in the **added** direction | the Step 6 planted option | remove the diff check ⇒ the probe passes silently |
| B8-2 | half 1 set-difference in the **removed** direction | delete one key from `sizingConformanceKeys` | as above |
| B8-3 | half 2's pairwise `arm` mapping | the Step 6 flipped arm | replace the map with a count map ⇒ a pairwise swap passes (the stored lesson *"assert the partition, not just the rows"*) |

---

## Task 9 — whole-branch delivery gate

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
- [ ] **Step 4.** **Docs link gate, both arms**, over every tracked Markdown file (CLAUDE.md). Treat any output as
      a blocker. Remember the two known false positives (wrapped Go generics, shape `-> docs/plans/m`) — *a hit
      naming a plausible `.md` path is real; a hit naming a Go identifier or containing a space is the parser
      limitation.* **Verify arm 2 is not vacuous** by planting a bad anchor and re-running.
- [ ] **Step 5.** **The un-mechanizable invariant** (Spec 017 §8 item 4, AC-3): confirm by hand that
      `routing/aggregator.go`'s `completionSizeCeiling` and `adapter/memory/groupstore.go`'s default
      `maxGroupMembers` both still read `1 << 16`, and that each carries a cross-reference comment naming the
      other. Record the two `grep` outputs in the evidence block. **This is the one invariant no test enforces.**
- [ ] **Step 6.** Re-derive, do not transcribe, the figures the artifacts cite: the class-gate key count, the
      `ErrInvalidCapacity` producer count (expect **six** — `memory.NewQueueStore`, `memory.NewGroupStore`,
      `memory.WithBuffer`, `routing.NewAggregator`, and the two new `WithMaxGroupMembers` sites), and the
      `ErrOverflowDropped` producer count. **Reconcile by name, never by count** (the project's standing
      `43 ≠ 43` lesson).
- [ ] **Step 7.** Update `docs/HANDOVER.md`: close §6 backlog item **7**; add the three follow-ups Spec 017 §8
      records (`memory`'s quadratic clone, `sql`'s per-`Add` full-group re-fetch, and *cap-without-timeout*
      diagnostics); and refresh the artifact counts in `docs/HANDOVER.md` **and** in CLAUDE.md's Project status
      paragraph. **Re-derive every count with the commands that paragraph names — do not increment a number in
      this plan**, because [Plan 030](030-post-029-maintenance.md) is landing in parallel and moves the same
      totals. Count **distinct plan numbers, not files**.
- [ ] **Step 8.** Flip the status lines: Spec 017 → DELIVERED, ADR 0033 → ACCEPTED, this plan → DELIVERED — and
      **remove the "without user ratification" banners only if the user has by then ratified them.** If not, leave
      them and say so.
- [ ] **Step 9.** Stage, show the diff, and **wait for explicit approval** before the final commit. `git push`, the
      merge and the branch deletion each need their own approval (Global constraint 9).

---

## Sizing

| Task | Modules touched | Docker | Rough size |
|---|---|---|---|
| 1 | root | no | medium — the core change |
| 2 | root | no | medium — 6 cases over a 5-part fixture |
| 3 | root | no | small — godoc + one example |
| 4+5 | root, postgres, mysql, sqlite | Task 5 verification | **large** — a breaking SPI change across 5 call sites |
| 6 | harness, dbtest | **yes** | medium |
| 7 | root | no | small |
| 8 | root | no | medium — counts appear in 3 places in one file |
| 9 | all 8 | yes | medium |

**If D-AG is reversed to enforcement (A)** (Spec 017 §8 item 2), Tasks 4+5+6 collapse into one small root-only
task and the increment drops from five touched modules to two. **That decision belongs before Task 4.**
