# Plan 030 — Post-029 maintenance: false godoc, 32-bit test overflow, stale plan numbers

> 🔴 **ONE STATEMENT SUPERSEDED — 2026-08-22, by [Plan 032](032-byte-cap-ceilings.md).** This plan's Task 2 and
> its backlog table describe the three `msghttp` byte caps as *"the gate's **deferred** arm"*. That was true
> when it ran; Plan 032 ([Spec 018](../specs/018-byte-cap-ceilings.md) / [ADR 0034](../adrs/0034-byte-cap-ceilings.md))
> has since bounded all three and moved their rows into the **`fixed`** arm, leaving `deferred` empty (a
> retained tombstone). **The literal Task 2 chose for them — `1<<62` — is unchanged and still correct**, and
> for the reason this plan gives: they are `func(n int64)`, so the value is in range on every `GOARCH`. It is
> now also load-bearing in a second way — `1<<30` sits BELOW `byteCapCeiling` and would be accepted.

> **For agentic workers:** REQUIRED SUB-SKILL: `superpowers:subagent-driven-development` (the project default).
> Steps use checkbox (`- [ ]`) syntax.
>
> **Go-skills hard rule (CLAUDE.md, restated because `writing-plans` omits it):** every task starts from
> **`cc-skills-golang:golang-how-to`** (here routing to: **`golang-documentation`** for Tasks 1 and 3 — both are
> comment/prose corrections — **`golang-testing`** for Task 2, plus **`golang-code-style`**). **`golang-refactoring`
> is NO LONGER routed** — revision 1's extraction task is dropped, see decision **D1** below, and there is no
> longer any behavior-preserving code restructure in this plan. **`superpowers:test-driven-development`** governs
> every task: red → green → refactor — though note only **Task 2** has executable assertions to turn red, and its
> "red" is the 386 compile failure plus the arm-specific mutants. **`gopls`** (via the `LSP` tool) for navigation
> — not `grep` — when reasoning about Go symbols. Project-local **`table-test`** override applies to every test
> (assert-closure form, never `want`/`wantErr`; `t.Context()`). **`use-mockgen` / `use-testcontainers` do not
> apply** — no task here introduces a test double or an external resource.

> ## ✅ DELIVERED — all three tasks, 2026-08-22
>
> | Task | Commit | Verified by |
> |---|---|---|
> | **Task 1** — false "first statement" godoc, 16 sites | **`1a1c135`** | wrap-tolerant scan → **12 hedged / 9 bare / 21 total**; AST checker reports zero violations |
> | **Task 2** — 32-bit test overflow | **`d2c69fe`** | `GOARCH=386 GOOS=linux go vet ./...` → **clean** (was 24 compile errors) |
> | **Task 3** — stale gin plan number | **`7ab91cd`** | docs-link gate both arms at baseline; no live `gin = Plan 028` assertion remains |
>
> Whole-tree at delivery: `go test ./... -race -shuffle=on` **11/11 root packages green**.
>
> **🔴 THE UNTICKED CHECKBOXES BELOW ARE A BOOKKEEPING ARTEFACT, NOT OUTSTANDING WORK.** They were never ticked
> during execution. Plan 032's audit round 3 read `grep -c '\[x\]' → 0` against `grep -c '\[ \]' → 32` and
> concluded only Task 2 had landed, then wrote a rebase instruction against that false state. **Two of the three
> commits do not carry "030" in their subject line, so `git log --oneline | grep -i 030` finds only `d2c69fe`.**
>
> **Derive delivery from the trailer, never from the subject or the checkboxes:**
> ```
> git log --format='%h %s' --grep='Plan: 030'
> ```
> The five `gin`/`Plan 028` hits that survive a naive grep are this file's own **defect-description table**
> (`:528-531`) plus one historical note in Spec 011 — descriptions of the defect, not live assertions of it.

**Revision 2 — 2026-08-22.** Supersedes revision 1 (same date). Revision 1 was audited and returned **NOT SAFE TO
IMPLEMENT** — 3 BLOCKERs, 4 MAJORs, 5 MINORs. The record is
[`030-audit-round-1.md`](030-audit-round-1.md), which is **immutable**: read it for the evidence, read this file
for what was decided. Every finding is dispositioned here, and the coordinator's decisions **D1–D7** are recorded
in "Decisions taken on the audit" below.

**What changed from revision 1, in one table:**

| Revision 1 | Revision 2 | Driver |
|---|---|---|
| Task 1 — collapse the seven delegator pre-check loops | **DROPPED.** Backlog item 2 is **CLOSED-WONTFIX** | **D1** ← BLOCKER 1 |
| Task 2 — false "first statement" godoc, **11** sites | **Task 1**, **16** sites, wrap-tolerant scan, invariant stated | **D3** ← BLOCKER 3 |
| Task 3 — uniform `1 << 30` over 23 sites | **Task 2**, split by **arm**: 17 → `1 << 30`, 6 → `math.MaxInt` | **D2** ← BLOCKER 2, MINOR 8 |
| Task 4 — six stale citations in three files | **Task 3**, mechanically derived, `docs/rfcs/` swept | **D4** ← MAJOR 4 |
| Global constraint 1 — `apidiff` / decl-**count** | Exported-symbol AST **set diff by name**, probe in `adapter/http` | **D5** ← MAJOR 5 |
| (nothing) | **Carry-forward section** — backlog items 3, 6, 7 | **D6** ← MINOR 12 |
| Task 2 §Sequencing note | **Deleted** — it named five files Task 2 never edits | **D1** ← MAJOR 6 |

**What did NOT change: the arithmetic.** The audit re-derived every count in revision 1 and confirmed it —
24 compile errors under 386 (4 memory + 2 routing + 16 root + 2 endpoint); the 16 gate-prose lines; the 16
int-typed gate code lines; `GOARCH=386 go build ./...` clean; all seven non-root modules 386-vet clean; seven
byte-identical delegator sites; the three `int64` exclusions correct and complete; the eighth-delegator claim
correct. Those figures survive verbatim.

---

## Governing artifacts

This plan has **no spec, no RFC and no ADR** — and after **D1** that premise now **holds**, which it did not in
revision 1. Every remaining task is a correction to existing, shipped **prose**: there is no new contract, no
contested "how", and no architectural decision. Exported surface delta is **zero by construction** — Task 1 edits
only comments, Task 2 only `_test.go` files, Task 3 only Markdown. **No task edits a single line of production
Go code.**

Its parent is not a spec but the **backlog recorded in [`../HANDOVER.md`](../HANDOVER.md) §6** at the Plan 029
merge — items **5** (Task 1), **8** (Task 2) and **4** (Task 3). Item **2** is closed by **D1**; items **3**, **6**
and **7** are carried forward untouched (see "Carry-forward"). Where a task corrects an artifact, that artifact is
cited in the task.

Related: [`029-sizing-option-bounds.md`](029-sizing-option-bounds.md),
[`030-audit-round-1.md`](030-audit-round-1.md),
[`../specs/015-nil-option-elements.md`](../specs/015-nil-option-elements.md),
[`../specs/016-sizing-option-bounds.md`](../specs/016-sizing-option-bounds.md),
[`../adrs/0031-nil-option-elements.md`](../adrs/0031-nil-option-elements.md),
[`../adrs/0032-sizing-option-bounds.md`](../adrs/0032-sizing-option-bounds.md).

> **⚠️ The backlog undercounted two of the three surviving items, and revision 1 undercounted one of them
> again.** Every count below was re-derived from the tree and then **independently re-derived by the round-1
> auditor**; the figures each supersedes are named inline so the discrepancy is auditable rather than silent.
> Re-derive again before relying on one.

---

## Decisions taken on the audit

### D1 — Backlog item 2 is **CLOSED-WONTFIX**. Do not re-propose it.

**This is the load-bearing decision in revision 2. It is written at length deliberately, so that a future session
finds the answer here instead of re-deriving the whole chain and re-proposing the same task.**

**The proposal.** Backlog item 2 ([`../HANDOVER.md`](../HANDOVER.md) §6): *"Seven copies of the delegator
pre-check loop in `adapter/http` (×5) and `adapter/http/stdlib` (×2). A package-local helper collapses each to one
line (~35 lines)."* Revision 1 made it Task 1.

**The decision: it will not be done. The duplication is not a defect.** It is the deliberate, ratified consequence
of **two shipped decisions acting together**, and collapsing it fights both.

**Decision one — the duplication is chosen, not accidental.**
[ADR 0031 D-R](../adrs/0031-nil-option-elements.md) / [Spec 014 §3.3](../specs/014-core-package-layout.md) chose
**per-package helper duplication over exporting an internal from root**. That is why `nilOptionAt` exists seven
times and `checkRange` four times rather than once. `adapter/http/helpers.go:27-30` says so in the source:

```
// This mirrors endpoint.nilOptionAt, routing.nilOptionAt, resilience.nilOptionAt,
// memory.nilOptionAt, cron.nilOptionAt and sql.nilOptionAt rather than sharing
// one of them: the body is two lines over exported API, and exporting an
// internal from root to save it is the trade ADR 0031 D-R rejected.
```

A `checkNilOptions` helper does not remove duplication from the repository — it *relocates* it, and adds a second
duplicated helper (one per package) on top of the first.

**Decision two — the loop shape is what the guard gate recognises.**
[Spec 015](../specs/015-nil-option-elements.md) **AC-7**'s guard gate (`option_guard_gate_test.go`) flags every
variadic `...XxxOption` parameter unconditionally and clears the flag **only** on an `*ast.RangeStmt` over that
parameter **inside the constructor's own body** (`option_guard_gate_test.go:160-175`; `hasNilElementGuard` at
`:219-238`). The syntactic strictness is **deliberate** — a syntactic gate cannot be defeated by a helper that
does not actually guard.

**Therefore the refactor turns a green shipped gate RED at all seven sites.** After the collapse,
`NewExchange`'s body contains `if err := checkNilOptions(...)` and no `RangeStmt` over `opts`, so
`guarded == false` and `assert.Emptyf(t, unguarded, ...)` at `:464` fails. `checkNilOptions(ctor string, opts
[]Option)` is not variadic, so it is never scanned — the guard becomes **invisible**, not merely unrecognised.
32 variadic option params are scanned today.

**The gate already ships a committed probe asserting exactly this.** Two recogniser cases pin the boundary:

| Probe case | Relationship to the refactor |
|---|---|
| `TestOptionGuardRecognizer/R1_pre-check_—_standalone_loop,_no_opt(&cfg)_call` | the shape the refactor **deletes** |
| `PROBE_qualified_type_—_msghttp.Option,_unguarded_delegator` | the shape `stdlib.NewInbound` **becomes** |

**The cost of proceeding.** Making the gate accept a helper call means teaching the recogniser that a **helper
call is a dominance proof** — i.e. amending **Spec 015 AC-7** and **ADR 0031 D-R**, re-auditing the gate, and
weakening a class gate whose strictness is its entire value. Backlog item **3** already records that the related
Plan 028 AST gate *"is syntactic, not a dominance proof"* and that promoting it to a `go/analysis` analyzer was
**rejected as out of scope pre-v1**. This refactor would force exactly that promotion, from the other direction.

**The benefit of proceeding: ~14 net lines.** Revision 1's own honest sizing: `7 × 5 = 35` is **gross**; an
idiomatic call site is three lines, so the loops net **~14 lines**, plus ~21 comment lines that *move* onto the
two helpers' godoc rather than disappearing. In a **pre-v1 library with no consumers**.

**The verdict: the gate is working as designed.** A gate that rejects a proposed change is a gate doing its job,
not an obstacle. **Amend a shipped spec and ADR and re-audit a class gate to net 14 lines** is the wrong trade,
and it stays the wrong trade until someone brings a reason other than line count.

**Evidence to preserve, so the eighth site is not re-discovered as a finding.** Revision 1's factual base was
sound (audit MINOR 9 confirms it) and is recorded here rather than lost with the task:

| # | Site (the `for` line) | ctor string |
|---|---|---|
| 1 | `adapter/http/exchange.go:78` | `"msghttp.NewExchange"` |
| 2 | `adapter/http/outbound.go:328` | `"msghttp.NewOutbound"` |
| 3 | `adapter/http/sse_server.go:133` | `"msghttp.NewSSEServer"` |
| 4 | `adapter/http/sse.go:228` | `"msghttp.NewSSEParser"` |
| 5 | `adapter/http/sseclient.go:69` | `"msghttp.NewSSEClient"` |
| 6 | `adapter/http/stdlib/inbound.go:61` | `"stdlib.NewInbound"` |
| 7 | `adapter/http/stdlib/inbound.go:127` | `"stdlib.NewInboundGateway"` |
| **8** | **`adapter/database/sql/queuestore.go:48`** | `"sql.NewQueueStore"` — an **eighth** delegator pre-check exists repo-wide, in a third package |

*(Site 8's line number is corrected from revision 1's `:45`, which was the comment-banner line, not the `for` —
audit **MINOR 10**, decision **D7**. The table's convention is the `for` line throughout.)*

`adapter/http/options.go:1181` (`NewConfig`) is **not** a delegator pre-check — its loop also calls `opt(cfg)`, so
it *applies* rather than pre-checks. Correctly excluded. Of 20 `nilOptionAt` call sites repo-wide, exactly **8**
are standalone pre-checks.

**Consequential deletions.** With Task 1 gone: the Task 1 ↔ Task 2 collision on `adapter/http/helpers.go`
**disappears**, and revision 1's §Sequencing note — which named five files Task 2 never edits and invited the
implementer to "correct" the seven *accurate* godocs (audit **MAJOR 6**) — is **deleted outright**, not rewritten.
No task in revision 2 collides with another; the three tasks touch disjoint file sets.

**Action required.** Mark backlog item 2 **CLOSED-WONTFIX** in [`../HANDOVER.md`](../HANDOVER.md) §6, citing this
section. That edit is part of Task 3's commit.

### D2 — Task 2 splits by arm (BLOCKER 2, MAJOR 7, MINOR 8). See Task 2.
### D3 — Task 1 adopts the wrap-tolerant scan and the corrected 16 sites (BLOCKER 3). See Task 1.
### D4 — Task 3 derives its citation list mechanically (MAJOR 4). See Task 3.
### D5 — Global constraint 1 is replaced (MAJOR 5). See Global constraints.
### D6 — Backlog items 3, 6 and 7 are carried forward (MINOR 12). See Carry-forward.
### D7 — MINOR 10 fixed above (`sql/queuestore.go:48`); MINOR 11 fixed in Task 3's docs-link step.

**On MAJOR 4's traceability sub-finding: no action.** The audit explicitly cleared *"unnumbered until written"*
against CLAUDE.md's traceability rule — the rule binds **artifacts**, and an unwritten roadmap entry is not one.
Task 3 keeps that wording, and keeps the decision **not** to write ADR 0024.

---

## Global constraints

1. **Zero exported-surface delta, verified by an AST set diff BY NAME — not by `apidiff`, not by counts.**
   Collect every exported declaration in **all packages of all 8 modules** at `main` and at `HEAD`, and diff the
   two **sets of names**. A count passes a rename; this project's own stored lesson is *"reconcile by name, never
   by count"* (CLAUDE.md, on the 43-vs-43 sentinel reconciliation), and revision 1's offered decl-**count** diff
   violated it.
   - **`apidiff` is dropped as the primary gate.** [`029-sizing-option-bounds.md:72`](029-sizing-option-bounds.md)
     and `:291` both record that Plan 028 proved it *"captures only the root package"*. It may be retained as a
     **root-only secondary** check, and if so it must run against
     [`028-root-api-baseline.txt`](028-root-api-baseline.txt) — the **newer** baseline. Revision 1 cited the
     superseded `027-root-api-baseline.txt`.
   - **Vacuity probe planted in `adapter/http`, NOT in root** — per
     [`029-sizing-option-bounds.md:615`](029-sizing-option-bounds.md): *"Plan 028's `apidiff` blindness survived
     Task 0 because its probe was planted in root — proving the gate *fires* is not proving it *covers*."* Add an
     exported symbol to `adapter/http`, watch the diff report it, revert, watch it disappear.
   - **Honest note:** after **D1** no task edits production Go at all, so this constraint is *trivially* satisfied.
     The probe is therefore the **only** thing distinguishing "the check ran and found nothing" from "the check did
     not run". Do not skip it because the answer is obvious.
2. **Behavior-preserving.** No task may change a single runtime outcome. Tasks 1 and 3 are prose; Task 2 changes
   only the *magnitude* of a test input, never which branch it exercises — with the arm-specific mutants of Task 2
   as the proof, since the blanket mutation gate of revision 1 was inapplicable to six of the sites.
3. **Green per task.** `GOTOOLCHAIN=go1.25.13 go test ./... -race` passes before each task's commit.
4. **Per-task commits are pre-authorized** by CLAUDE.md's plan-execution exception once this plan is approved and a
   task-by-task mode is chosen. `git push`, merges, tags and branch deletion are **not** — they stay with the user.
5. **Trailers.** Every commit carries `Plan: 030`. No `Spec:`/`ADR:` trailer — see "Governing artifacts".

---

## Task 1 — Correct the false "first statement" comment class

**Backlog item 5.** HANDOVER §6 says *"four godoc sites"*. Revision 1 said **eleven** (5 production + 6 test).
**Both are wrong. The real count is SIXTEEN — 8 false production sites and 8 false test sites** (audit
**BLOCKER 3**, decision **D3**).

### The invariant (assert this; do NOT work from the enumeration)

> **No comment may assert that a statement is a function's first statement when the function's
> `fd.Body.List[0]` is a different statement.**

Revision 1's own step said *"assert the invariant, not the enumeration"* — and then prescribed a command that
could not see a third of its own class. The enumeration below is a **starting point measured on 2026-08-22**, not
the definition of done. The definition of done is the invariant, verified by the scan below.

### The scan — wrap-tolerant, and it is a STEP, not a footnote

The phrase **wraps across comment lines**, so a line-oriented `grep` misses it. This is the whole reason revision 1
undercounted:

```bash
# WRONG — revision 1's command. Line-oriented; blind to wrapped occurrences.
grep -rn "first statement" --include="*.go" .          # → 19 hits

# RIGHT — wrap-tolerant, whole-file slurp, tolerating a newline + comment marker
# between the two words. Run this; work from its output.
for f in $(git ls-files '*.go'); do
  perl -0777 -ne 'while (/first\s*(?:\n\s*(?:\/\/|\*)\s*)?statement/g) {
      $p = substr($_,0,pos($_)); $n = ($p =~ tr/\n//)+1; print "$ARGV:$n\n"; }' "$f"
done                                                    # → 24 hits
```

**24 = 16 false + 8 accurate.** Every one of the 24 must be re-classified against the actual constructor body
(`fd.Body.List[0]`), not against this table.

### 1a — False PRODUCTION sites: 8 (revision 1 said 5; HANDOVER said 4)

Each asserts the apply loop is the constructor's first statement when a `cfg := …` initializer precedes it —
except `helpers.go`, which is inverted the other way.

| Site | Function | Actual first statement | New in rev 2? |
|---|---|---|---|
| `adapter/database/sql/groupstore.go:206-209` | `NewGroupStore` | `cfg := groupStoreConfig{logger: discardLogger()}` | |
| `adapter/memory/queuestore.go:99-102` | `NewQueueStore` | `cfg := config{clock: clockwork.NewRealClock()}` | |
| `adapter/http/options.go:1168-1172` | `NewConfig` | `cfg := &Config{}` | |
| `adapter/memory/groupstore.go:92-95` | `NewGroupStore` | `cfg := groupStoreConfig{clock: …, maxGroups: 1024}` | |
| `adapter/cron/source.go:168-171` | `NewSource` | `cfg := config{clock: …, location: time.UTC, logger: …}` | |
| **`adapter/http/helpers.go:16-17`** | `nilOptionAt`'s godoc, describing the five delegators | **INVERTED** — says the delegators *"each call `NewConfig(opts...)` as their first / statement"*. They call their **own pre-check** first. | **🆕** |
| **`adapter/database/sql/outbound.go:56-57`** | `NewOutboundAdapter` | `outbound.go:61` `cfg := config{logger: discardLogger()}` | **🆕** |
| **`adapter/database/sql/source.go:89-90`** | `NewSource` | `source.go:95` `cfg := config{logger: discardLogger()}` | **🆕** |

> **🔴 `adapter/http/helpers.go:16-17` is the finding that condemns revision 1's method.** It is **nearly verbatim
> the same sentence** as `nil_option_test.go:22`, which revision 1 called *"the more serious error"* — and revision
> 1 fixed the **test** copy while leaving the **production godoc** copy. Fix production first.

> **🔴 Revision 1 had the evidence and did not use it.** Its own 2b table cited `outbound.go:61` and
> `source.go:95` as *the reason the TEST comments are wrong*, while never checking the production godoc **two
> lines above each constructor**, which says the same false thing. When a table cites a line number as proof, read
> the surrounding function.

### 1b — False TEST-comment sites: 8 (revision 1 said 6)

| Site | Error | New in rev 2? |
|---|---|---|
| `adapter/database/sql/groupstore_unit_test.go:543` | same `cfg :=` error as 1a | |
| `adapter/database/sql/outbound_test.go:135` | same (`cfg := config{logger: discardLogger()}`, `outbound.go:61`) | |
| `adapter/database/sql/source_test.go:138` | same (`cfg := config{logger: discardLogger()}`, `source.go:95`) | |
| `adapter/http/nil_option_test.go:22` | **INVERTED** — says `NewConfig(opts...)` is the delegator's first statement | |
| `adapter/http/nil_option_test.go:101` | **INVERTED** — same | |
| `adapter/http/stdlib/nil_option_test.go:20` | **INVERTED** — same | |
| **`adapter/cron/source_test.go:267-268`** | same `cfg :=` error; actual first stmt `source.go:179` | **🆕** |
| **`adapter/http/nil_option_test.go:47-48`** | **self-contradictory** — *"`cfg := &Config{}` then the apply loop is its first / statement"* names the preceding statement in the same sentence | **🆕** |

### The 8 ACCURATE sites — do not touch

Every *delegator* constructor (standalone pre-check loop, no `cfg` yet) genuinely is first-statement:
`sql/queuestore.go:42`, `http/sse.go:221`, `http/exchange.go:72`, `http/sseclient.go:63`, `http/outbound.go:322`,
`http/stdlib/inbound.go:43` and `:109` — **plus `adapter/http/nil_option_test.go:305`**, which is accurate.

> **🔴 The load-bearing claim is TRUE at every false site — fix the wording WITHOUT weakening it.** In each `cfg :=`
> case the preceding statement is a pure struct-literal initializer that performs no validation and cannot return
> an error, so the apply loop **is** the first statement *that can fail*, and every *"…runs BEFORE X and beats
> it"* clause remains correct. Those clauses are what the tests pin. Replace only the false premise (e.g. *"the
> apply loop is the first statement that can fail, preceded only by the zero-value config initializer"*); **do
> not** delete or soften the ordering guarantee.
>
> For the four **INVERTED** sites the correct wording is the reverse of what they say: *"…runs its own nil
> pre-check first, then forwards to `msghttp.NewConfig`."* Those four state the opposite of the (correct)
> production godoc at `exchange.go:72` / `outbound.go:322` / `sseclient.go:63` / `sse.go:221` /
> `inbound.go:43,109`, and contradict the very reason those tests exist — the standalone pre-check is what buys
> the truthful position.

### Steps

- [ ] Run the **wrap-tolerant scan** above. Record its hit count. If it is not 24, the tree has moved — re-derive
      the whole classification rather than trusting the tables.
- [ ] Fix the **8 false production sites** (1a) first, `adapter/http/helpers.go:16-17` included, preserving every
      ordering clause verbatim.
- [ ] Fix the **8 false test sites** (1b). Leave `adapter/http/nil_option_test.go:305` alone.
- [ ] Re-run the wrap-tolerant scan and **re-classify every remaining hit against `fd.Body.List[0]`**. The
      invariant, not the enumeration, is the gate. Expected residue: the 8 accurate sites and nothing else.
- [ ] `go test ./... -race`; `gofmt -l .` clean. **`adapter/cron` and `adapter/database/sql` are edited too** —
      confirm both packages, and remember `sql`'s leaf modules are separate `go.mod`s (see Verification).

### Hot-path branches introduced

**None** — comments only. No new test case is required; the existing `nil_option_test.go` suites (23 cases across
six msghttp entry points, 8 across two stdlib entry points) already pin every ordering claim the corrected prose
makes.

**Commit:** `docs(core): correct the false "first statement" comment class`

---

## Task 2 — Make the sizing tests compile under GOARCH=386

**Backlog item 8.** HANDOVER §6 says vet *"fails in 4 packages"*. Four *packages* is right; **`go vet` aborts after
the first error per package, so the real figure is 24 compile errors** (4 memory + 2 routing + 16 root + 2
endpoint — re-derived and confirmed by the audit). Get the full list with:

```bash
GOTOOLCHAIN=go1.25.13 GOARCH=386 GOOS=linux go test -gcflags=all=-e -run=NONE ./...
```

`GOARCH=386 go build ./...` is **clean** — no non-test code is affected. Only the **root** module fails; the other
seven modules are already 386-clean.

### The fix: split by ARM, because the two arms need OPPOSITE properties

Revision 1 half-saw this (its hazard 4) and then told the implementer to *"convert the 23 ceiling-class sites to
`1 << 30`"* anyway, naming only three of the six safe-arm sites. Audit **BLOCKER 2**. Decision **D2**: the arm a
site belongs to decides its replacement value, and the site list below is authoritative.

**Reject arm (17 sites) → `1 << 30`.** `1<<30` = **1,073,741,824**, fits in int32 (max 2,147,483,647), and still
exceeds every ceiling in the codebase (the largest is `1<<20` = 1,048,576) — so it selects the identical branch
(out-of-range → typed error) on both architectures, with a **fixed decimal string** in the expected message. That
fixed decimal is precisely what keeps the `EqualError` assertions architecture-*independent*.

**Safe arm (6 sites) → `math.MaxInt`.** These rows assert **`require.NoError`** plus a product-usable check and
**carry no decimal string at all**, so architecture-dependence is harmless — and the value must stay *maximally
absurd*, because that is the row's entire purpose. `sizing_option_class_gate_test.go:544-547` states it:

> *"Each accepts 1<<62 AND its product is proven usable … a comparison-only knob is exercised past the point where
> a buggy comparison (e.g. an int32 truncation) would misbehave."*

**`1<<30` IS an int32 value.** Demoting a safe-arm row to `1<<30` leaves the assertion green while probing
nothing. Worse, if an implementation ever regressed to `make([]T, max)`, `math.MaxInt` fails fast whereas `1<<30`
quietly allocates ~1 GiB, turning a clean assertion into a slow near-OOM.

> **Accepted, recorded limitation.** On `GOARCH=386` no `int` value exceeds int32, so the int32-truncation probe
> those six rows exist to run is **unachievable by magnitude reduction** on 32-bit. `math.MaxInt` keeps the probe
> intact where it is meaningful (64-bit) and degrades to a tautology where it cannot be (32-bit). That is the best
> available outcome; do not "fix" it by picking a smaller number.

Update each row's assertion **message** in the same edit — they currently say the literal text `1<<62`. Add
`"math"` to the gate file's imports.

### Per-site table — site | arm | actual ceiling | replacement

| # | Site | Arm | Actual ceiling / assertion | Replacement |
|---|---|---|---|---|
| 1 | `adapter/memory/sizing_bounds_test.go:320` | reject | `memory.WithBuffer`, `[0, 1<<20]` | `1 << 30` |
| 2 | `adapter/memory/sizing_bounds_test.go:326` | reject | same | `1 << 30` |
| 3 | `adapter/memory/sizing_bounds_test.go:332` | reject | same | `1 << 30` |
| 4 | `adapter/memory/sizing_bounds_test.go:338` | reject | same | `1 << 30` |
| 5 | `routing/completion_size_bounds_test.go:93` (`const n`) | reject | `WithCompletionSize`, `[1, 1<<16]` | `1 << 30` |
| 6 | `sizing_option_class_gate_test.go:375` | reject (`fixed`) | `endpoint.WithMaxInFlight`, `[1, 1048576]` | `1 << 30` |
| 7 | `:387` | reject (`fixed`) | `endpoint.WithConcurrency`, `[1, 65536]` | `1 << 30` |
| 8 | `:398` | reject (`fixed`) | `msghttp.WithConnectionBuffer`, `[1, 65536]` | `1 << 30` |
| 9 | `:411` | reject (`fixed`) | `memory.WithBuffer`, `[0, 1048576]` | `1 << 30` |
| 10 | `:424` | reject (`fixed`) | `memory.WithCapacity`, `[1, 1048576]` | `1 << 30` |
| 11 | `:435` | reject (`fixed`) | `memory.WithMaxGroups`, `[1, 1048576]` | `1 << 30` |
| 12 | `:446` | reject (`fixed`) | `msghttp.WithMaxConnections`, `[1, 65536]` | `1 << 30` |
| 13 | `:462` | reject (`fixed`) | `routing.WithCompletionSize`, `[1, 65536]` | `1 << 30` |
| 14 | `:473` | reject (`fixed`) | `msghttp.WithReplayBuffer`, `[1, 65536]` | `1 << 30` |
| 15 | `:493` | reject (`rejects`) | `msghttp.WithSuccessStatus`, its own `[100,599]` check | `1 << 30` |
| 16 | **`:568`** | **SAFE** | `endpoint.WithPollMaxBatch` — `require.NoError`, no decimal | **`math.MaxInt`** |
| 17 | **`:587`** | **SAFE** | `resilience.WithBreakerThreshold` — `require.NoError`, no decimal | **`math.MaxInt`** |
| 18 | **`:612`** | **SAFE** | `endpoint.WithMaxPayloadBytes` — `require.NoError`, no decimal | **`math.MaxInt`** |
| 19 | **`:634`** | **SAFE** | `resilience.NewTokenBucket` burst (positional, 17th key) | **`math.MaxInt`** |
| 20 | **`:647`** | **SAFE** | `memory.QueueStore.Claim` (method arg, not an option) | **`math.MaxInt`** |
| 21 | **`:666`** | **SAFE** | `channel.QueueChannel.Poll` (method arg, not an option) | **`math.MaxInt`** |
| — | `routing/completion_size_bounds_test.go:97`, `:103` | reject | consume `n`; `:103` already uses `%d` | **no edit** — they follow site 5 |
| — | `endpoint/zero_size_element_test.go:31,32` | **special** | not a ceiling — a Go **runtime** property | see "The special case" |

**Reject-arm count: 15 gate/memory/routing literal sites + site 5's two dependents = the 17 the audit counted.**
Safe arm: 6. Special case: 1 file, 2 lines. `15 + 6 = 21` literal edits in the gate/memory/routing set, `+ 2` in
`endpoint`, `+ 1` companion string (below) — do not treat any single count here as the definition of done; the
386 compile list is.

### The companion edit revision 1 omitted (audit MAJOR 7)

Revision 1's hazard 2 said the ten in-string decimals *"become **false prose**"*. **They do not — they FAIL AT
RUNTIME.** Nine are `assert.EqualError` arguments (`sizing_option_class_gate_test.go:379, 391, 402, 417, 428, 439,
450, 466, 477`) and the tenth is an `assert.Contains`:

```
adapter/memory/sizing_bounds_test.go:378:
  assert.Contains(t, err.Error(), "memory.WithBuffer: 4611686018427387904 not in [0, 1048576]")
```

`:378` sits inside the helper `assertFirstFaultIsSizing` (`:372-379`) and **appears nowhere in revision 1's site
list**. An implementer working that list converts the four literals at `:320,326,332,338`, leaves `:378`, and
**`TestNew_SizingGuardIsIndependentOfTheLatch/AC-3b` goes red**. It is site **22** and it is mandatory.

```bash
# All 11 decimal occurrences — the eleventh is a bare literal, invisible to any `1<<62` grep.
grep -rn "4611686018427387904" --include="*.go" .        # → 11
```

`1<<30` renders as **`1073741824`**. Every reject-arm decimal becomes that; the safe arm carries none.

### Hazards — a blanket `sed` WILL break this

1. **Three sites are `int64` and compile fine on 386. Do not touch them.**
   `sizing_option_class_gate_test.go:520` (`WithMaxBodyBytes`), `:529` (`WithMaxEventBytes`), `:538`
   (`WithMaxResponseBytes`) — all `func(n int64)`. They are the gate's **deferred** arm; changing them changes that
   arm's meaning. *(Verified correct and complete by the audit.)*
2. **The 16 gate-prose lines must move with the code.** `sizing_option_class_gate_test.go:31,35,37,485,488,510,545,
   570,580,588,594,614,624,635,648,667`, plus `adapter/memory/sizing_bounds_test.go:290,294,299,301`,
   `endpoint/zero_size_element_test.go:18,29`, `adapter/http/config_sizing_bounds_test.go:186`. **Note these now
   split two ways**: reject-arm prose says `1<<30`, safe-arm prose (`:545,570,580,588,594,614,624,635,648,667`)
   says `math.MaxInt`. The arm-summary lines `:31,35,37` must describe **both** values.
3. **`endpoint/zero_size_element_test.go:31` is NOT an msgin option.** See "The special case".
4. **The safe arm needs the OPPOSITE treatment to the reject arm.** A blanket rewrite of all 21 literals to one
   value is wrong. This is audit **BLOCKER 2** and it is why the table above exists.
5. **🆕 The `makechan` mutant becomes an OOM, not a panic — an ACCEPTED TRADE, not a blocker** (audit **MINOR 8**).
   `adapter/memory/sizing_bounds_test.go:292-294` documents that the wrong implementation shape *"reaches
   `make(chan msgin.Message[any], 1<<62)` and panics"*, and `:306` records the test as **mutation-proven**.
   `runtime.makechan` raises `"size out of range"` only when `elemsize × cap > maxAlloc` (≈ `1<<48` on 64-bit). At
   `1<<30` with element size ≥ 8 bytes the product is ≥ **8 GiB** — under that threshold — so **re-running that
   mutation attempts a real allocation and will likely OOM-kill the test binary** instead of producing a
   recoverable panic.
   **The shipped test is unaffected**: the ceiling rejects `1<<30` long before `makechan` is reached, so the
   assertion holds exactly as it does today. **Only *reproducing the mutation* becomes expensive.** Record this in
   the file's header comment so the next session does not lose a machine to it, and do **not** re-run that
   particular mutant as part of this task.

### The special case — `endpoint/zero_size_element_test.go`

```go
31:	ch := make(chan struct{}, 1<<62)
32:	assert.Equal(t, 4611686018427387904, cap(ch))
```

This exercises a Go **runtime** property — a zero-size element type never trips `makechan`'s size check at *any*
capacity — not a msgin ceiling. Line 32's decimal is a **bare literal** and a hard 386 compile error; no `1<<62`
grep finds it.

**`1<<30` is wrong here**: it may well *succeed* for reasons that have nothing to do with the zero-size element,
silently destroying what the test demonstrates.

**Recommended first attempt: `math.MaxInt` at both lines** — `make(chan struct{}, math.MaxInt)` still succeeds
precisely *because* the element is zero-size, `assert.Equal(t, math.MaxInt, cap(ch))` stays self-consistent, and
the pair compiles on both architectures. **Prove it**, do not assume: mutate `struct{}` to a non-empty type and
confirm the test fails. If no architecture-independent value preserves the demonstration, constrain the file with
a build tag and say so in its header rather than weakening it. Also update the prose at `:18` and `:29`.

### Steps

- [ ] Capture the full 24-error list with the `-gcflags=all=-e` command above; work from it, not from `go vet`.
- [ ] Convert the **15 reject-arm literal sites** (rows 1–15) to `1 << 30`, updating every paired decimal string
      and reject-arm prose mention in the same edit.
- [ ] Convert the **6 safe-arm sites** (rows 16–21) to `math.MaxInt`; add the `"math"` import; update their
      assertion messages from the literal text `1<<62`.
- [ ] Update `adapter/memory/sizing_bounds_test.go:378` (the `assertFirstFaultIsSizing` helper) to the new decimal
      **`1073741824`**. This is site 22 and it is a red test if missed.
- [ ] Update the arm-summary prose at `sizing_option_class_gate_test.go:31,35,37` to name **both** values and say
      why they differ.
- [ ] Handle `endpoint/zero_size_element_test.go` separately per "The special case"; **prove** the intended
      property still holds (mutate the element type and watch it fail) rather than assuming.
- [ ] Leave the three `int64` sites untouched; assert that in a comment so a future sweep does not "finish the job".
- [ ] Record hazard 5's accepted trade in `adapter/memory/sizing_bounds_test.go`'s header comment.
- [ ] `GOTOOLCHAIN=go1.25.13 GOARCH=386 GOOS=linux go vet ./...` → clean.
- [ ] `GOTOOLCHAIN=go1.25.13 go test ./... -race` → green on amd64. **Both architectures, or the task is not done.**

### Hot-path branches introduced

**No new branches** — every test must select the *same* branch it selects today. The gate is a **mutation check**,
and revision 1's blanket version was **inapplicable to six of the sites** (they have no ceiling check to remove).
Decision **D2** replaces it with an **arm-specific** gate:

| Arm | Mutant | Expected |
|---|---|---|
| Reject (rows 1–15, 22) | Remove the ceiling check from the option under test | The `EqualError` / `Contains` assertion **fails** |
| Safe (rows 16–21) | Wrap the knob's stored value in `int32(n)` at its comparison site | The `require.NoError` / product-usable assertion **fails** on 64-bit |
| Special (`endpoint`) | Change `chan struct{}` to a channel of a non-empty type | The `make` panics / the test **fails** |

A test that passes at the new value **because it stopped testing anything** is the failure mode this task exists
to rule out. Do not run the `makechan` mutant of hazard 5 — see the accepted trade.

**Commit:** `test(core): make the sizing tests compile on 32-bit`

---

## Task 3 — Retire the stale gin plan number and the ADR 0024 forward reference

**Backlog item 4.** HANDOVER §6 says the gin increment *"still needs a plan number, and its ADR is still a forward
reference."* Both are true, but there is a **third, worse defect the backlog does not name: the plan number
currently written down is WRONG.**

`gin` was pencilled in as **Plan 028**. Plan 028 was then consumed by `nil-option-elements`
([`028-nil-option-elements.md`](028-nil-option-elements.md)).
[ADR 0031](../adrs/0031-nil-option-elements.md) `:260` records the move — but **no citation was updated**.

### Derive the list mechanically — do NOT work from the table below alone (D4)

Revision 1 said *"five files"* and listed **three**, and missed at least six further lines, three of them in
`docs/rfcs/` — **a directory it never scanned** (audit **MAJOR 4**). Sweep, do not enumerate:

```bash
# Every tracked Markdown line that ties "gin" to plan number 028.
for f in $(git ls-files '*.md'); do
  grep -nE '\bgin\b' "$f" | grep -E '028' | sed "s|^|$f:|"
done

# And every ADR 0024 forward reference.
grep -rn 'ADR 0024\|ADR-0024' $(git ls-files '*.md')
```

**Derived on 2026-08-22** (supersedes both revision 1's table and the audit's "at least six more"):

| File | Line(s) | Nature |
|---|---|---|
| [`../specs/011-http-adapter.md`](../specs/011-http-adapter.md) | 630, 633, 685, 691, 694 | gin is Plan 028 |
| [`../adrs/0023-http-channel-adapter.md`](../adrs/0023-http-channel-adapter.md) | 32, 36, 203 | gin is Plan 028 |
| [`020-http-adapter-inbound.md`](020-http-adapter-inbound.md) | 19 | gin is Plan 028 |
| **[`../rfcs/README.md`](../rfcs/README.md)** | **116, 122, 126** | gin is Plan 028 — **🆕, directory never swept** |
| **[`027-core-package-layout.md`](027-core-package-layout.md)** | **3523** | *"before Plan 028 (gin)"* — **🆕** |
| **[`../specs/011-http-adapter.md`](../specs/011-http-adapter.md)** | **94, 95** | ADR 0024 + Plan 028 — **🆕** |

**`../specs/011-http-adapter.md:95` is DOUBLY false** and gets its own line in the commit body: it asserts
*"Neither ADR 0024 nor Plan 028 exists yet"* when [`028-nil-option-elements.md`](028-nil-option-elements.md) has
shipped. Fixing only the plan-number half leaves a second falsehood in place.

**ADR 0024 forward references — verified complete and accurate by the audit; preserve the set:**
`../specs/011-http-adapter.md:25,92,95,685` and `../adrs/0023-http-channel-adapter.md:32,33,36,200,203`.

**DO NOT EDIT — immutable execution records** (they correctly record what was true when written):
`027-audit-round-1.md:441`, `027-derivation-findings.md:1412`.
**Already correct, no edit needed:** `../adrs/0031-nil-option-elements.md:260`,
`../specs/015-nil-option-elements.md:508` — both already record the displacement.

**[`../../MESSAGING.md`](../../MESSAGING.md) is CLEAN** — verified by the audit (`grep -n "Plan 028\|ADR 0024"` →
no hits). Do not add a citation to it.

### What this task does — and deliberately does NOT do

**DOES NOT write ADR 0024.** Writing it means *deciding to adopt `github.com/gin-gonic/gin`*, which is an
architectural decision under CLAUDE.md's **Dependency policy** (*"Justify every dependency in an ADR"*; adding a
direct dependency *"is an architectural decision"*). That call belongs to the user, and no session should make it
as a side effect of tidying citations. **Left open; flagged in the handover.**

**DOES** remove the false statements, and fixes the *class* rather than the instance: replacing "Plan 028" with a
new concrete number would simply re-arm the same staleness the moment that number is consumed too — which is
exactly how this defect arose. So the gin increment is stated as **unnumbered until written**, and ADR 0024 as
**reserved but unwritten**, neither of which can go stale.

> **"Unnumbered until written" was attacked and cleared.** The audit examined it against CLAUDE.md's hard
> traceability requirement and returned **"No finding"**: the rule binds **artifacts**, and an unwritten roadmap
> entry is not one. Keep the wording.

### Steps

- [ ] Run **both sweep commands** above. If the derived line set differs from the table, trust the sweep and say so
      in the commit body.
- [ ] Rewrite every stale "Plan 028 = gin" citation to *"a plan number assigned when the increment is written"*.
- [ ] Fix `../specs/011-http-adapter.md:95`'s **second** falsehood (Plan 028 *does* exist) in the same edit.
- [ ] Leave the ADR 0024 references, but state uniformly that the number is **reserved and the artifact unwritten**,
      and that admitting `gin` is an **open dependency decision**, not a scheduled one.
- [ ] Update [`../../CLAUDE.md`](../../CLAUDE.md)'s project-status sentence, which currently calls ADR 0024 *"a
      forward reference to an unwritten artifact"* — still true, but it should also say the plan number is
      unassigned.
- [ ] Update [`../HANDOVER.md`](../HANDOVER.md) §6: mark item **2 CLOSED-WONTFIX** citing decision **D1**, and
      leave items **3**, **6**, **7** open (see Carry-forward).
- [ ] **Re-baseline the docs-link gate with this plan STAGED** (audit **MINOR 11**, decision **D7**). Both arms
      iterate `git ls-files '*.md'`, so an untracked plan is **invisible to its own gate**. `git add -N
      docs/plans/030-post-029-maintenance.md docs/plans/030-audit-round-1.md` first, then run both arms.
      Known-good baseline is **exactly two arm-1 false positives** (`docs/plans/m`, `docs/specs/factory(fireTime` —
      Go identifiers in wrapped code spans, not links) and **zero** arm-2 hits. Anything else is a blocker.
- [ ] Prove arm 2 is not vacuous by planting a bad anchor, confirming exactly one hit, and reverting.

### Hot-path branches introduced

**None** — Markdown only, no Go code.

**Commit:** `docs: retire the stale gin plan number and reserve ADR 0024`

---

## Carry-forward — what this plan deliberately leaves open (D6)

The [`../HANDOVER.md`](../HANDOVER.md) §6 backlog carries eight items. This plan consumes **4, 5 and 8**; item
**1** was closed by Plan 029; item **2** is closed by **D1**. **Items 3, 6 and 7 remain OPEN and are NOT in this
plan's scope.** They are named here so the next handover does not have to re-derive the residue.

| Item | Status after Plan 030 | Note |
|---|---|---|
| **3** — the Plan 028 AST gate is syntactic, not a dominance proof | **OPEN.** Promoting it to a `go/analysis` analyzer was rejected as out of scope pre-v1 | **🔴 Now directly load-bearing.** Decision **D1** closes backlog item 2 *because* the gate is syntactic. Anyone who reopens the delegator-collapse question must resolve item 3 first — they are the same question seen from two sides |
| **6** — the byte-ceiling class (`msghttp.WithMaxBodyBytes`, `WithMaxEventBytes`, `WithMaxResponseBytes`) | **OPEN.** Needs its own spec/ADR | The open question is *"should an explicit off-state exist at all, and which sentinel value carries it"* — **not** "invent an off-state". A negative `n` is already taken by the rejection. These are the gate's `deferred` arm and Task 2 must not touch them |
| **7** — `routing.WithReleaseWhen` reaches the same unbounded per-group growth | **OPEN.** Needs its own spec/ADR | Func-typed, so **structurally invisible to the class gate**. Verified outside Spec 016's stated class, so the shipped prose does not over-claim. A one-sentence cross-reference on `WithCompletionSize`'s godoc would close the inference gap meanwhile |

Additionally opened by this plan:

- **Backlog item 2 is CLOSED-WONTFIX**, not deferred. See **D1**. If a future session proposes it again, that
  section is the answer.
- **ADR 0024 / the gin increment** remain an **open user decision** (Dependency policy). Task 3 makes the citations
  honest; it does not schedule the work.

---

## Verification (whole plan)

- [ ] `GOTOOLCHAIN=go1.25.13 go test ./... -race -shuffle=on` — 11/11 root packages green.
- [ ] `GOTOOLCHAIN=go1.25.13 GOARCH=386 GOOS=linux go vet ./...` — clean (this is new; it fails today).
- [ ] Per-module CI parity over all **8** module directories: `go build`, `go vet`, `gofmt -l .`,
      `CGO_ENABLED=0 go build`, `go mod tidy` + `git diff --exit-code -- go.mod go.sum`, `govulncheck`,
      `golangci-lint run ./...`, `go test ./... -race -shuffle=on`. (`harness` has no test files — `go test`
      there is a false pass; use `go vet`. `govulncheck` lives in `$(go env GOPATH)/bin`, not on `PATH`.)
      **Task 1 edits `adapter/database/sql` and `adapter/cron` comments**, so the leaf modules must be re-run.
- [ ] **Exported-surface AST set diff BY NAME, `main..HEAD`, all packages, all modules** — identical. Vacuity
      probe planted in **`adapter/http`**, not root: add an exported symbol, watch the diff report it, revert,
      watch it disappear. (Global constraint 1 / **D5**.)
- [ ] Docs-link gate, both arms, **with this plan and the audit record staged** (`git add -N`) — at the two known
      arm-1 false positives and nothing more, zero arm-2 hits. Arm-2 vacuity probe planted and reverted.
- [ ] `/simplify` over the branch diff before the reviews (CLAUDE.md Development workflow §4).
- [ ] `/code-review` and `/security-review` over `main..HEAD`; every finding fixed or triaged with a written
      rationale.
- [ ] [`../HANDOVER.md`](../HANDOVER.md) §6 updated: item 2 CLOSED-WONTFIX, items 3/6/7 still open and named.
