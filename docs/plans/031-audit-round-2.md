# Plan 031 — adversarial design audit, round 2 (2026-08-22)

Independent Opus subagent, handed the **complete Plan 031 revision-2 bundle together** — [Spec
017](../specs/017-group-member-bounds.md) + [ADR 0033](../adrs/0033-group-member-bounds.md) +
[Plan 031](031-group-member-bounds.md) — **before any implementation code existed**, per
[`CLAUDE.md`](../../CLAUDE.md)'s design-time gate. All three artifacts declare themselves **revision 2,
post-audit-round-1**, and carry the 🔴 *"decided WITHOUT USER RATIFICATION"* banner; that banner is not itself a
finding, but every decision it covers (**D-AC** … **D-AQ**) was treated as open.

Round 2 has **two jobs**, and they are separate: (1) **verify the 21 round-1 findings actually landed** in the
revision — not that the revision *mentions* them — and (2) **attack the revised bundle afresh**, including the new
material the revision added. Both are recorded below; the fix-verification table comes first because it is what
distinguishes a revision from a rewrite.

**Traceability.** Audits: [Spec 017](../specs/017-group-member-bounds.md),
[ADR 0033](../adrs/0033-group-member-bounds.md), [Plan 031](031-group-member-bounds.md). Predecessor round:
[`031-audit-round-1.md`](031-audit-round-1.md) — **immutable**; nothing below edits it. Origin:
[`docs/HANDOVER.md`](../HANDOVER.md) §6 backlog item **7**. Predecessors whose ratified decisions the bundle reuses:
[Spec 016](../specs/016-sizing-option-bounds.md), [ADR 0032](../adrs/0032-sizing-option-bounds.md),
[Plan 029](029-sizing-option-bounds.md), [ADR 0031](../adrs/0031-nil-option-elements.md) **D-R**. Colliding
concurrent work: [Plan 030](030-post-029-maintenance.md) (landed), [Spec 018](../specs/018-byte-cap-ceilings.md) /
[Plan 032](032-byte-cap-ceilings.md) (drafted, under concurrent revision — **explicitly out of scope for this
round**).

This record is **evidence-primary** — it is the audit artifact, not a user-facing summary. Every structural claim in
the revised bundle was re-derived on this tree with `GOTOOLCHAIN=go1.25.13`, darwin/arm64, at **`46803c6`** (current
`main`, the commit that carries the revision-2 artifacts). Commands and their output are pasted below. No file in
the repository was modified while auditing.

> **⚠️ IMMUTABLE EXECUTION RECORD.** This file records what round 2 found, at the time it found it. Do not edit it
> to reflect later decisions, later measurements, or the revised bundle. Corrections belong in a later round's
> record or in the artifacts themselves. The coordinator's dispositions for these findings live in **Spec 017 /
> ADR 0033 / Plan 031 revision 3**, each of which cites this file.

> **📋 PROVENANCE — what in this file is the auditor's, and what was added later.**
>
> **The auditor's, verbatim or transcribed from the round-2 report:** the **verdict and score line**, the
> **21-row fix-verification table** (Part 1), **every N-finding** in Part 2 including its cited coordinates, and
> the **eight point-by-point answers** to the audit brief (Part 3).
>
> **Added later, and labelled as such:** the closing section
> **"Round-3 corrections to this record"**, which lists two coordinates the auditor cited slightly off. Those
> corrections are **not** applied inside the auditor's own findings above them — the findings stand as the auditor
> wrote them, and the corrections are recorded separately so the record remains the auditor's rather than an
> edited composite. **Spec 017 / ADR 0033 / Plan 031 revision 3 use the corrected coordinates.**

---

## Verdict

**NOT SAFE TO IMPLEMENT.** 1 BLOCKER, 6 MAJORs, 7 MINORs. The auditor's score line, verbatim:

> **Score: 12 clean LANDED, 8 LANDED-BUT-FLAWED, 1 (M-8) landed with a defensible ADR omission. 0 NOT LANDED,
> 0 REGRESSED. The revision is genuinely responsive; every flaw below is new ground, not a re-run of round 1.**

**The framing matters more than the count, and it is the lesson of this round.**

> **The revision engaged all 21 findings and regressed none. What it failed to do is GENERALIZE its own two
> structural fixes.**
>
> - **B-2's insight was *"a cross-module edit is a red commit."*** It was applied to the class gate — correctly,
>   thoroughly, and restructured into the task list. It was **not** applied to the `GroupDialect.AddMember`
>   signature, which is the other cross-module edit in the same increment. **N-1**, the BLOCKER: Tasks 5+6 land one
>   commit that leaves `harness` and `dbtest` non-compiling, and Task 6 Step 5's gate cannot see the break.
> - **M-3's insight was *"one mechanism asserted for three engines."*** It was applied to the transaction wrappers —
>   §3.6.1's per-dialect table is exactly right. The same defect then **recurs three more times** in the same
>   revision: for the **reaper** (**N-3** — D-AM's premise is `memory`-only), for the shared **`SelectMembers`**
>   helper (**N-5** — one `LIMIT` specified for a helper with three callers), and for the very **SPI godoc M-3 was
>   about** (**N-9** — the shipped *"takes the GROUP ROW LOCK"* sentence is untouched, and Task 5 Step 5 only adds
>   to it).
>
> A revision that fixes the named instance while the same defect class returns through a different file is the
> project's stored lesson *"fix the class, not the instance"* failing on the very increment whose Consequences
> section claims that lesson as its own. **That is the finding behind the finding, and it is why this round returns
> a BLOCKER rather than a clean bill with polish items.**

Two secondary patterns, both stored lessons of this project, also recur and are worth naming once here rather than
repeating per finding:

1. ***"Fold into all three artifacts."*** **N-14**: B-2's fix — the single most restructuring finding of round 1 —
   landed in the spec and the plan and **never reached the ADR**. `grep -n "same commit\|red suite\|exact set
   equality" docs/adrs/0033-*.md` returns nothing.
2. ***"Docs can contradict the code they describe."*** **N-13**: four of the revision's *new* citations — added by
   the fixes for B-1, M-3 and M-6 — are off by one to eight lines. The fixes for m-1/m-2/m-3 landed clean; the
   class returned through the new prose.

---

## Part 1 — fix verification: did round 1's 21 findings land?

**The auditor's fix-verification table, verbatim.** For each round-1 finding: the disposition, and the evidence it
was read against on the tree.

| # | Sev | Verdict | Evidence |
|---|---|---|---|
| B-1 | BLOCKER | LANDED-BUT-FLAWED | D-AM in all three. `consumer.go:843` returns before `:860`, so MaxAttempts is genuinely never consulted. `divertTerminal` (`:1033`) nil sink WARNs (`:1049`) then `safeAck`s (`:1074`) — discard **plus Ack**. Flaw: the "reaper never sweeps" premise is memory-only (N-3), D-AJ names the wrong WARN (N-11). |
| B-2 | BLOCKER | LANDED-BUT-FLAWED | Gate key+row ship inside Task 1 / Task 5 commits; root stays green. But the same cross-module red-commit shape reappears at the AddMember signature change (N-1, BLOCKER), and the ordering clause is absent from D-AL (N-14). |
| B-3 | BLOCKER | LANDED | Independence claim deleted; four shared files tabulated in all three. Spot-checked citations reproduce. |
| M-1 | MAJOR | LANDED | `1<<30` / decimal `1073741824` specified 3-way with the three-way arm split. Task 10 Step 3b adds `GOARCH=386 go vet` — better than the required fix. |
| M-2 | MAJOR | LANDED-BUT-FLAWED | D-AP + §3.6.2 + AC-4b + Task 7 Step 3 exist, but the reachability claim is false (N-2). |
| M-3 | MAJOR | LANDED-BUT-FLAWED | Per-dialect table 3-way; `sqlite/groupdialect.go:52` `withImmediateConn`, `:62` BEGIN IMMEDIATE, `:114` DO NOTHING verified. Flaw: the shipped AddMember godoc still asserts the falsified GROUP ROW LOCK (N-9). |
| M-4 | MAJOR | LANDED | Reason 3 struck as false in §3.1 and D-AC; post-hoc reason is now reason 1 in both. `store.Add` has one caller, `aggregator.go:412`. |
| M-5 | MAJOR | LANDED-BUT-FLAWED | D-AQ ships, §8 item 4 closed, Consequences carries the REMOVED note, Task 3 exists. But the test has no constant to parse (N-4). |
| M-6 | MAJOR | LANDED-BUT-FLAWED | D-AN + D-AO 3-way; defect re-confirmed at `groupstore.go:130`/`:133`/`:135`. AC-1b is the right case. Flaw: its id-less fixture is unpinned (N-6); `Handle`'s new exits under-covered (N-7). |
| M-7 | MAJOR | LANDED | Ten sites in AC-8.5, D-AL, Task 9 Step 3, `:335`/`:753` flagged executable, `:107-108` singled out as falsified. Verified at HEAD. |
| M-8 | MAJOR | **LANDED (with a defensible ADR omission)** | Two probes (postgres leaf module + adapter/database/sql same-module package) in AC-10 and Task 9 Step 7. 8 go.mod files, none in adapter/database/sql. **ADR is silent, correctly — it has no verification section.** |
| m-1 | MINOR | LANDED | §3.3.1 cites `reliability.go:86-97`; the stale `:35-46` is gone from the ADR. Task 10 Step 8 fixes CLAUDE.md's copy. |
| m-2 | MINOR | LANDED | `groupstore.go:38-45` 3-way; `Add(...)` declared at `:45`. |
| m-3 | MINOR | LANDED | `:250-276`, call `:271`, `decodeGroupRows` definition `:365` — all verified. |
| m-4 | MINOR | LANDED | §3.12.3 now reads "the same ceiling (`1<<20`, deliberately)". |
| m-5 | MINOR | LANDED | Seven sites, kind-labelled, in §3.6.3 / D-AG / Task 5 Step 1; the grep returns exactly those seven. |
| m-6 | MINOR | **LANDED** | `classifyQueryErr` named in §1.3.4 + §3.6.3 + AC-4c, D-AP, B5-7. Verified `groupstore.go:273` → `:91-96`. |
| m-7 | MINOR | LANDED | Task 4 names `routing/example_aggregator_test.go`; `HeaderSequenceSize` at `message.go:24`. |
| m-8 | MINOR | LANDED | "Six" stated once in Tech stack, Sizing footer and D-AG reversibility. |
| m-9 | MINOR | LANDED-BUT-FLAWED | Decided explicitly 3-way with a rationale; the rationale over-claims (N-10). |
| m-10 | MINOR | LANDED | "methodCount stays 27, do not bump" in AC-8.6, D-AL, Task 1 Step 2 / Task 5 Step 2 / Task 9 Step 4. |

**The 21 rows close.** Tallied **by name** off the table above: **13 LANDED + 7 LANDED-BUT-FLAWED + 1 M-8 = 21.**

> ⚠️ **The score line and this table disagree by one row in the middle bucket, and that is recorded rather than
> reconciled.** The verdict states *"12 clean LANDED, 8 LANDED-BUT-FLAWED"*; the table yields **13 / 7**. Both total
> 21, and no row is missing or duplicated — one finding is counted LANDED-BUT-FLAWED by the score line and LANDED by
> the table. **Reconcile by name, never by count** (this project's standing `43 ≠ 43` rule): the by-name tally is in
> the closing **"Round-3 corrections to this record"** section, which also names the most probable candidate.
> Neither number has been altered here.

**Note the shape of M-8's row, because it is the one disposition that is not a simple pass or fail.** The fix
landed in the spec and the plan, and the ADR is **silent — correctly**: ADR 0033 has no verification section, so a
probe-siting correction has no home there. **That is a defensible ADR omission, not an instance of N-14.** N-14 is
about **D-AL**, a decision section that *does* own the class gate and *should* have carried B-2's ordering rule.
The distinction is the difference between "the ADR does not discuss testing" and "the ADR discusses the gate and
omits how it constrains commits."

**Nothing regressed and nothing was ignored.** The revision is genuinely responsive; every flaw in Part 2 is **new
ground, not a re-run of round 1**. It is nevertheless still not implementable.

---

## Part 2 — findings

## Finding index

| # | Rank | One line |
|---|---|---|
| **N-1** | BLOCKER | The Tasks 5+6 commit ships **two non-compiling modules**. `harness` holds a `GroupDialect` and calls the 8-arg `AddMember`; Task 7 is the first task that touches it. Task 6 Step 5's gate lists four modules and cannot see the break |
| **N-2** | MAJOR | `*sql.Tx` is **unreachable** from `sql.GroupStore` — `NewGroupStore` takes a concrete `*stdsql.DB` and `WithSharedTransaction` returns an `Option`, not a `GroupStoreOption`. D-AP's *"reachable through `WithSharedTransaction`"* is false, and AC-4b names the wrong entry point |
| **N-3** | MAJOR | D-AM's premise *"the reaper never sweeps without `WithGroupTimeout`"* is **`memory`-only**. `sql.GroupStore.RecoverInterval()` returns the lease TTL, so the default sweep runs — and it surfaces exactly the group D-AM permanently dead-letters |
| **N-4** | MAJOR | The AST invariant has **no constant to parse**. `adapter/memory` has no named default; the shipped precedent is a bare `maxGroups: 1024` literal, and Task 1 Step 4 says only *"(default `1 << 16`)"*. D-AQ's evidence block quotes the wrong constant, and the test covers one of the two stores that carry the risk |
| **N-5** | MAJOR | `LIMIT maxMembers+1` is specified on `*SelectMembers`, which has **three** callers per dialect — only one of which has a `maxMembers`. As written it is unimplementable; read literally it silently truncates `ClaimGroup` and `ExpiredGroups` |
| **N-6** | MAJOR | AC-1b is billed as the case that would have caught M-6, and its whole content is *"no id"* — but `msgin.New` **always** stamps one, including under `WithID("")`. The obvious implementation is an id-ful test that passes while proving nothing |
| **N-7** | MAJOR | `Handle`'s new branch has ~6 exits; the plan's table and AC-9 name 4. The uncovered ones include a deliberate, untested divergence from the normal path's `claim == nil` behavior |
| **N-8** | MINOR | The two stores render **different counts at the same boundary** (65536 vs 65537) while Global constraint 4 calls the shape *"identical"* |
| **N-9** | MINOR | The shipped `GroupDialect.AddMember` godoc still says *"takes the GROUP ROW LOCK"* — the exact claim M-3 falsified — and Task 5 Step 5 edits that godoc only to **add** things |
| **N-10** | MINOR | D-AD's collision-rule table has a row with **no collision** (`WithInboxTable`), and `sql`'s two `GroupStoreOption`s cannot discriminate the rule from a blanket prefix |
| **N-11** | MINOR | D-AJ cites `warnInvalidFallback` as the loud signal; in the fully-default config it **never fires**. The real bare outcome is a WARN **+ Ack** — the source drops the message — which the bundle never says |
| **N-12** | MINOR | AC-7 is not executable: *"asserting §3.7's requirement"*, where §3.7 is a four-clause MUST/SHOULD/MAY paragraph |
| **N-13** | MINOR | Four residual off-by-one citations, all introduced by revision 2's own new prose |
| **N-14** | MINOR | **B-2's fix landed in the spec and plan but NOT the ADR.** The project's named failure mode, on the finding that restructured the task list |

---

## BLOCKER N-1 — the Tasks 5+6 commit ships two non-compiling modules, and the task's own gate cannot see it

**The claim under attack.** Plan 031 Task 5's header box: *"**This task leaves the three dialect modules RED**;
Task 6 makes them green. **Land Tasks 5 and 6 as ONE commit** so no commit is a broken build (Global constraint
8)."* Task 6 Step 5: *"`GOWORK=off go test ./... -race -shuffle=on` green in **root + all three dialect
modules**."*

**Three dialect modules is not the blast radius of an interface signature change. `harness` holds the interface
value, and `dbtest` requires `harness`.**

```
$ grep -n "GroupDialect" adapter/database/sql/harness/*.go
adapter/database/sql/harness/testkit.go:87:	Group msginsql.GroupDialect

$ grep -n "AddMember" adapter/database/sql/harness/*.go
adapter/database/sql/harness/groupstore.go:345:			_, err = kit.Group.AddMember(ctx, db, table, key, id, seq, headers, []byte(`"p"`))
```

`harness` is its own module (`adapter/database/sql/harness/go.mod`). It **stores** a `msginsql.GroupDialect` in
`TestKit.Group` and **calls** it with the current **8-argument** signature. The moment Task 5 Step 5 adds
`maxMembers int` to the interface, that call site fails to compile — `not enough arguments in call to
kit.Group.AddMember`. And `dbtest` requires `harness` with a local `replace`:

```
$ grep -n "harness" adapter/database/sql/dbtest/go.mod
	github.com/kartaladev/msgin/adapter/database/sql/harness v0.0.0
	github.com/kartaladev/msgin/adapter/database/sql/harness => ../harness
```

so `dbtest` goes red with it. **Two modules, not zero.**

**Task 7 is the first task in the plan that touches `harness`.** Task 5's Files list is
`adapter/database/sql/{groupstore.go,groupdialect.go,helpers.go,groupdialect_fake_test.go,groupstore_unit_test.go}`
plus `sizing_option_class_gate_test.go`; Task 6's is the three dialects' `groupdialect.go`. Task 7's Files is
`adapter/database/sql/harness/groupstore.go`. So the Tasks 5+6 commit — the one the plan explicitly constructs *"so
no commit is a broken build"* — **is** a broken build, in exactly the two modules the plan corrected its own module
count to include (round 1's **m-8**).

**The gate cannot catch it, twice over.** Task 6 Step 5 enumerates *"root + all three dialect modules"* — `harness`
and `dbtest` are not in the loop. And even if `harness` were added, `go test` there is a **false pass**:

```
$ ls adapter/database/sql/harness/
dialect.go  go.mod  go.sum  groupstore.go  harness.go  inbox.go  lock.go  outbound.go  outbox.go  queuestore.go  source.go  testkit.go
```

**No `_test.go` files.** CLAUDE.md says so explicitly (*"`harness` has no test files, so `go test` there reports a
false pass — check it with `go vet ./...` instead"*), and Plan 031 **already knows this** — Task 7 Step 5 says it
verbatim. The knowledge is in the plan; it is just in the wrong task.

**Why this is the BLOCKER and not a MINOR sequencing nit.** Global constraint 8 is what CLAUDE.md's per-task-commit
pre-authorization is conditioned on (*"Each task must be a **green unit** … no WIP/broken-build commits"*). A plan
that instructs the implementer to make a commit which violates it — while asserting in the same box that it does
not — will be followed. This is **B-2's own finding, one file over**: B-2 established that *a cross-module edit is a
red commit and must land with its consumers*. The revision applied that to the class gate with real rigor and did
not apply it to the other cross-module edit in the same increment.

**Required fix.**

1. **Fold Task 7 Step 1's harness call-site update into the Tasks 5+6 commit.** The harness signature change is
   mechanical (one call site, one extra argument threaded from the existing `TestKit`); Task 7 keeps its
   **behavioral** conformance cases, which is what it is actually for.
2. **Restate Global constraint 8** so it cannot be read as an edit-list check: *"green in every module that
   **compiles against** a changed signature, not merely every module in a Files list."* An interface method
   signature is a whole-workspace edit whatever the Files list says.
3. **Add `harness` and `dbtest` to Task 6 Step 5's gate**, with **`go vet`** for `harness` (no test files) and the
   Docker-backed run for `dbtest`.

---

## MAJOR N-2 — `*sql.Tx` is unreachable from `sql.GroupStore`; D-AP's reachability claim is false

**The claim under attack.** Spec 017 §3.6.2: *"Under a caller-supplied `*sql.Tx` — an explicitly supported Querier,
**reachable through `WithSharedTransaction`** — the over-cap member row is already inserted into the caller's open
transaction."* ADR 0033 **D-AP** repeats it (*"reachable via `WithSharedTransaction`"*), and §6 **AC-4b** operationalizes
it: *"`BeginTx` on the caller's side; pass the `*sql.Tx` as the Querier; **`Add`** the `cap+1`-th member."*

**The evidence — `sql.GroupStore` can never hand a `*sql.Tx` to a dialect.**

```
$ grep -n "func NewGroupStore" adapter/database/sql/groupstore.go
211:func NewGroupStore(db *stdsql.DB, table string, dialect GroupDialect, opts ...GroupStoreOption) (*GroupStore, error)

$ sed -n '40,44p' adapter/database/sql/groupstore.go
type groupBase struct {
	db      *stdsql.DB
	table   string
	dialect GroupDialect

$ sed -n '271p' adapter/database/sql/groupstore.go
	rows, err := s.dialect.AddMember(ctx, s.db, s.table, key, msgID, seq, headers, payload)
```

The constructor takes a **concrete `*stdsql.DB`**, `groupBase.db` is a **concrete `*stdsql.DB`**, and every dialect
call passes `s.db`. There is no seam. And `WithSharedTransaction` is not a `GroupStore` option at all:

```
$ grep -n "^func WithSharedTransaction" adapter/database/sql/options.go
201:func WithSharedTransaction(r TransactionResolver) Option

$ grep -n "GroupStoreOption" adapter/database/sql/groupstore.go | grep "^1[45]"
140:func WithGroupLeaseTTL(d time.Duration) GroupStoreOption {
155:func WithGroupLockedBy(id string) GroupStoreOption {
```

`WithSharedTransaction` returns an **`Option`** — the `NewPollingSource`/`Outbound` config family — not a
`GroupStoreOption`. `NewGroupStore`'s entire option surface is `WithGroupLeaseTTL` and `WithGroupLockedBy` (plus the
logger). **The two types are not interchangeable and the compiler says so.**

**What this breaks, and what survives.** The *hazard* is real — `pgRunInTx`'s `return fn(tx)` branch exists and does
not roll back — but it is a **`GroupDialect`-level** contract, not a `GroupStore`-level one. It is reachable only by
a **direct dialect caller**, which is precisely what `harness` is
(`harness/groupstore.go:345`, `kit.Group.AddMember(ctx, db, …)` — a `TestKit`-supplied Querier). So:

- D-AP's decision **stands**; its stated reachability does not.
- The normative home of the precondition is wrong. D-AP puts it on **`sql.WithMaxGroupMembers`'s godoc** (place #2),
  where it is unreachable: a caller of that option *always* gets a store that owns its transaction. A reader is
  handed a caveat that cannot apply to them, which is worse than no caveat.
- **AC-4b as written is not executable** — *"pass the `*sql.Tx` as the Querier; `Add` …"* reads as
  `GroupStore.Add`, which cannot take a Querier.

**Required fix.**

1. **Delete *"reachable through `WithSharedTransaction`"*** from Spec §3.6.2 and ADR D-AP. State the real
   reachability: the `*sql.Tx` branch is a **`GroupDialect`-level** contract, exercised only by a direct dialect
   caller.
2. **Move D-AP's normative home #2 off `sql.WithMaxGroupMembers`'s godoc onto `GroupDialect.AddMember`'s** — the
   interface a direct caller actually reads. Add **one sentence** to `sql.WithMaxGroupMembers` saying the bound
   **is** unconditionally durable for this store, because it always owns its transaction. That is the useful,
   true statement for that reader.
3. **Reword AC-4b** to name the dialect entry point (`kit.Group.AddMember(ctx, tx, …)`), not `GroupStore.Add`.
4. **Soften ADR Consequences' exposure claim.** *"Durability is conditional on the dialect owning the transaction"*
   overstates it for the shipped store, whose durability is not conditional at all.

---

## MAJOR N-3 — D-AM's premise is `memory`-only; the `sql` reaper sweeps by default, and surfaces the very group D-AM dead-letters

**The claim under attack.** Spec 017 §3.3.1, the load-bearing step of the permanent classification: *"Both escapes
are conditional on configuration this spec **elsewhere insists is opt-in**: §3.11 states the remedy 'remains
**opt-in**', and §1.2 re-verifies that with no `WithGroupTimeout` the reaper never sweeps. In the default
configuration neither escape exists."* ADR **D-AM** rests on the same sentence, and §3.11 states it as fact.

**The evidence — that is true for `memory` and false for `sql`.**

```
$ grep -n "func (s \*GroupStore) RecoverInterval" -A 1 adapter/memory/groupstore.go
220:func (s *GroupStore) RecoverInterval() time.Duration { return 0 }

$ grep -n "func (s \*GroupStore) RecoverInterval" adapter/database/sql/groupstore.go
348:func (s *GroupStore) RecoverInterval() time.Duration { return s.leaseTTL }
```

`sql.GroupStore.RecoverInterval()` returns the **lease TTL** — default **5m** (`groupstore.go:118-122`). And
`reapInterval` takes the minimum *positive* of the timeout and the store's interval:

```go
routing/aggregator.go:558-565
func (a *Aggregator) reapInterval() time.Duration {
	interval := a.cfg.timeout
	if storeInterval := a.store.RecoverInterval(); storeInterval > 0 && (interval <= 0 || storeInterval < interval) {
		interval = storeInterval
	}
	return interval
}
```

With **no `WithGroupTimeout` at all**, `a.cfg.timeout` is 0, `storeInterval` is 5m, so `interval` is **5m** and
`Run` starts a ticker (`aggregator.go:544`) instead of blocking on `ctx.Done()`. The Aggregator's own godoc says so
in as many words, and calls `Run` **required**:

```
routing/aggregator.go:530-532
// A durable store (RecoverInterval() = its lease TTL) gets
// crash-recovery sweeps even with no expiry timeout set — so go agg.Run(ctx)
// is REQUIRED for multi-process/crash safety whenever the store is durable,
```

**The conclusion survives — but by a mechanism the bundle never states.** With `a.cfg.timeout == 0`, `reap` passes a
**zero cutoff** (`aggregator.go:568-571`), and the dialect's `ExpiredGroups` gates the age path on it:

```go
adapter/database/sql/postgres/groupdialect.go:277-281
	beforeSet := !before.IsZero()
	rows, err := q.QueryContext(ctx,
		fmt.Sprintf(`SELECT group_key, created_at FROM %s
WHERE (locked_by IS NOT NULL AND locked_at <= %s - $2)
   OR ($1 AND locked_by IS NULL AND created_at < $3)
…`, gt, pgNowMicros),
		beforeSet, leaseTTL.Microseconds(), before.UnixMicro(), limit)
```

So the default sweep surfaces **crashed-lease groups only** (`locked_by IS NOT NULL AND locked_at <= now - TTL`);
an *unleased* group at cap is never returned, because `$1` is false. **The un-drainable case D-AM classifies
permanent is genuinely un-drainable for `sql` too — but for a completely different reason than the one written
down.**

**And the un-stated mechanism has a live counter-example the bundle needs to own.** Consider a `sql` group that is
**at cap** *and* holds a **stranded lease** (a crashed releaser). That group **is** returned by the default sweep
(first `WHERE` arm, no cutoff needed), **is** claimed by `reapGroup`, and **is** drained if its predicate fires —
today, with zero configuration. Under D-AM, every member that arrived for that key in the interim was classified
**permanent** and terminated at a sink. So D-AM does not merely *dead-letter a message a `WithGroupTimeout` caller
would have admitted* (the trade the ADR states honestly); it dead-letters messages the **default** `sql`
configuration would have admitted one reaper tick later.

**Why this is a MAJOR and not a MINOR.** D-AM is the disposition of round 1's BLOCKER, it is Spec §8's *"most
consequential"* open item, and its argument is one sentence: *"in the default configuration neither escape
exists."* That sentence is `memory`-only, and it is stated as being about both stores. This is **M-3's defect
recurring in the reaper**: one mechanism asserted where there are two.

**Required fix.**

1. **Re-derive §1.2's reaper paragraph PER STORE**, exactly as §3.6.1 now does for the transaction wrappers —
   `memory`: `RecoverInterval() == 0`, no ticker, genuinely no sweep. `sql`: `RecoverInterval() == leaseTTL`
   (default 5m), ticker runs, sweep runs, `ExpiredGroups`' `beforeSet` guard limits it to crashed leases.
2. **Restate D-AM's premise** as *"nothing drains an **UNLEASED** group without an expiry cutoff"* — which is true
   for both stores and is what the classification actually needs.
3. **Add the live counter-example** to D-AM's table or Spec §8: a group with a stranded lease **and** a live
   residual at cap is surfaced by the default sweep, claimed by `reapGroup`, and drained if its predicate fires —
   so D-AM permanently dead-letters members rejected in the interim.
4. **Fix §3.11's *"that remains opt-in"***, which is false for a durable store.

---

## MAJOR N-4 — the AST invariant has no constant to parse, and covers one of the two stores at risk

**The claim under attack.** Spec 017 §6 AC-3.3: the test *"locates `const completionSizeCeiling` and the
`maxGroupMembers` default **by name** on the `*ast.GenDecl` tree, **failing loudly if either declaration is not
found**."* ADR **D-AQ** and Plan Task 3 Step 3 say the same.

**The evidence — `adapter/memory` has no named default constant, and the shipped precedent is a bare literal.**

```
$ grep -n "^const" adapter/memory/groupstore.go
62:const maxGroupsCeiling = 1 << 20

$ sed -n '98p' adapter/memory/groupstore.go
	cfg := groupStoreConfig{clock: clockwork.NewRealClock(), maxGroups: 1024}
```

The **ceiling** is a named `const`. The **default** is a bare `1024` inside a composite literal in
`NewGroupStore`'s first statement. And Plan Task 1 Step 4 specifies the new field the same way: *"the
`maxGroupMembers` config field (default `1 << 16`)"* — no constant named, no declaration form given.

**So an implementer following local precedent writes `maxGroupMembers: 1 << 16` inline, and Task 3's not-found guard
fires.** That is Task 3's *designed* failure (Step 4 makes non-vacuity mandatory), which means the increment
green-lights Task 1, then blocks at Task 3 on a defect Task 1 was never told to avoid. The dependency runs
backwards: **AC-3.3 constrains Task 1's declaration form and Task 1 does not know it.**

**D-AQ's evidence block quotes the wrong constant.** Its proof that both values are parseable reads:

```
routing/aggregator.go:33          const completionSizeCeiling = 1 << 16
adapter/memory/groupstore.go:62   const maxGroupsCeiling      = 1 << 20
```

`maxGroupsCeiling` is the group-**COUNT** ceiling. It is **not** the quantity in the invariant
(`defaultMaxGroupMembers >= completionSizeCeiling`) and it is not the constant the test parses. The evidence block
demonstrates the parseability of a constant the test never reads, for an invariant it is not part of.

**And the test covers one of the two stores that carry the risk.** `sql.NewGroupStore` gets the **same default**
(`1 << 16`, Spec §3.2), under the **same Aggregator**, with the **same `WithCompletionSize`** — so a `sql` caller
configuring `WithCompletionSize(1<<16)` against a smaller `sql` default hits the identical silent deadlock the
invariant exists to prevent. AC-3.3 parses `routing/aggregator.go` and `adapter/memory/groupstore.go` only.
**Covering one store while the other carries the identical risk is this increment's own "fix the class, not the
instance" lesson violated inside the fix for M-5.**

**A third inconsistency, in the same feature.** Three artifacts name **three different homes** for the
cross-reference comment: Spec §4 item 2 puts it on **`maxGroupMembersCeiling`** (*"a constant godoc … and a
cross-reference to the other constant"*), Plan Task 1 Step 7 puts it on **the ceiling constant**, and Plan Task 3
Step 5 says *"add the cross-reference comment to **each** constant"*. The invariant is about the **default**, not
the ceiling.

**Required fix.**

1. **Mandate named constants — `defaultMaxGroupMembers = 1 << 16` — in BOTH packages** (Plan Task 1 Step 4, Task 5
   Step 4), and **say in the ADR that this deliberately deviates from the `maxGroups: 1024` precedent because
   AC-3.3 depends on it.** A deviation from a shipped precedent that is not recorded is a future audit finding.
2. **Correct D-AQ's evidence block** to quote `defaultMaxGroupMembers`, not `maxGroupsCeiling`.
3. **Extend the AST test to `adapter/database/sql/groupstore.go`** — three files, two assertions, same guard.
4. **Put the cross-reference comment on the DEFAULT**, and reconcile Spec §4 item 2, Task 1 Step 7 and Task 3
   Step 5 to say the same thing.

---

## MAJOR N-5 — `LIMIT maxMembers+1` is specified on a helper with three callers, only one of which has a `maxMembers`

**The claim under attack.** Spec 017 §3.6.3: *"Each dialect's live-member `SELECT` (`pgSelectMembers` /
`mysqlSelectMembers` / `sqliteSelectMembers`, `claimed_epoch IS NULL`) gains a **`LIMIT maxMembers+1`**."* ADR
**D-AP** and Plan Task 6 Step 3 repeat it (*"Add `LIMIT maxMembers+1` to each dialect's live-member `SELECT`"*).

**The evidence — each of those three helpers has THREE call sites, and only one is `AddMember`.**

```
$ grep -rn "SelectMembers(ctx" adapter/database/sql/{postgres,mysql,sqlite}/groupdialect.go
postgres/groupdialect.go:121:  pgSelectMembers(ctx, tx, mt, groupKey, "claimed_epoch IS NULL")          ← AddMember
postgres/groupdialect.go:163:  pgSelectMembers(ctx, tx, mt, groupKey, fmt.Sprintf("claimed_epoch = %d", newEpoch))  ← ClaimGroup
postgres/groupdialect.go:307:  pgSelectMembers(ctx, q,  mt, c.key,    "claimed_epoch IS NULL")          ← ExpiredGroups
mysql/groupdialect.go:113 / :161 / :298      — identical three-site shape
sqlite/groupdialect.go:131 / :177 / :314     — identical three-site shape
```

`AddMember` is the **only** one of the three that will have a `maxMembers` in scope. So the instruction as written
is **unimplementable** at two of every three call sites.

**And the failure mode of the obvious workaround is silent.** An implementer who resolves the ambiguity by putting
the `LIMIT` **inside the helper** — the reading the text most supports, since the text says *"the SELECT gains a
LIMIT"* rather than *"the AddMember call site gains one"* — has to invent a value for the other two callers, and
whatever they invent:

- **`ClaimGroup`** would return a **truncated claimed set**. A group that legitimately holds `cap` claimed members
  releases with fewer, and the aggregate is silently incomplete — the *"force-release an incomplete aggregate"*
  outcome Spec §5 rejects as **silent data corruption**, arrived at by a different road.
- **`ExpiredGroups`** would return a **truncated recovery set**, so the reaper's expiry route drops members.

**Neither loss is visible to any acceptance criterion in the bundle.** AC-4 asserts the *overflow* path's row count;
AC-5 asserts overflow behavior per dialect; nothing asserts that a **legitimate at-cap** claim or expiry returns
its full set.

**A second, smaller error in the same paragraph.** §3.6.3 says *"On overflow the just-upserted member is filtered
out of the materialized `[]MemberRow` in Go, and the remaining rows are **exactly the post-rollback live set**."*
That holds only when the live count was **≤ cap before the add**. AC-4b deliberately commits `cap+1` under a
caller-owned `*sql.Tx`; and with a `LIMIT cap+1` the materialized set is at most `cap+1` rows, so after filtering
one the remainder is at most `cap` — which is the post-rollback set **only** under the normal path's precondition.
State the precondition rather than the absolute.

**Required fix.**

1. **Specify a private `limit int` parameter (0 = unlimited)** on each of `pgSelectMembers` / `mysqlSelectMembers` /
   `sqliteSelectMembers`, with **`AddMember` the only caller passing non-zero**. That makes the instruction
   implementable and makes the other two call sites' behavior explicit rather than inherited.
2. **Add a mutant proving the other two pass 0**: pass `maxMembers+1` from `ClaimGroup` ⇒ an over-cap claimed group
   is truncated ⇒ a `harness` case fails. Without that mutant, the parameter is a convention rather than a
   constraint.
3. **Correct the *"exactly the post-rollback live set"* claim** to state its precondition.

---

## MAJOR N-6 — AC-1b's whole content is "no id", and no artifact names a constructor that produces one

**The claim under attack.** Spec 017 §6 **AC-1b** — *"NEW in revision 2; **this is the case that would have caught
audit M-6, and no other case does**"* — step 1: *"Add four **id-less** messages (`msgin.Message` with an empty id)
via `Handle`."* Plan Task 2 Step 3 and branch **B1-3c** rest on the same fixture.

**The evidence — the obvious constructor cannot produce an id-less message.**

```go
message.go:167  func New[T any](payload T, opts ...MessageOption) Message[T] {
message.go:178-180
	if cfg.id == "" {
		cfg.id = NewID()
	}
	m[HeaderMessageID] = cfg.id
```

`msgin.New` **always** stamps an id — and the guard is on the *empty string*, so `msgin.New(p, msgin.WithID(""))`
stamps a fresh generated id too. The **only** id-less route in the public API is:

```go
message.go:198  func NewMessage[T any](payload T, headers Headers) Message[T]
```

— *"WITHOUT stamping msgin.message-id/msgin.timestamp"* — called with headers that carry no `HeaderMessageID`.

**Consequence, and it is exactly the M-6 shape again.** An implementer reaching for the obvious constructor writes
`msgin.New(payload)`, gets an **id-ful** message, and the test **passes** — through the dedup branch
(`groupstore.go:130-131`), which returns the snapshot with a **nil** error and lets `Handle` reach the predicate
anyway. The deadlock AC-1b exists to prove absent is **never entered**. Worse, **B1-3c's killing mutant survives**:
folding the cap check back inside `if id != ""` changes nothing for an id-ful fixture, so the case reports a green
run against the mutant it was written to kill. That is the project's stored lesson *"mutation-test every new
assertion"* failing at the fixture, not at the assertion.

**Why the fixture cannot be left to the implementer.** AC-1b is the only case in the bundle whose *entire*
discriminating power is a property of its input. Every other case discriminates on behavior.

**Required fix.**

1. **Pin the fixture by name** — `msgin.NewMessage(payload, headers)` with no `HeaderMessageID` — in **AC-1b**,
   in **B1-3c** and in **Plan Task 2 Step 3**. Name it in all three; this is precisely a *"fold into all three
   artifacts"* case.
2. **Make `require.Empty(t, m.ID())` the case's FIRST assertion**, so the fixture is **asserted** rather than
   assumed. A fixture that silently stops being id-less is how this case dies quietly two increments from now.

---

## MAJOR N-7 — `Handle`'s new branch has six exits; the plan's table and AC-9 cover four

**The claim under attack.** Plan Task 1's hot-path table rows **B1-11** … **B1-14**, and Spec AC-9 rows 12-13, are
the complete stated coverage of §3.3a's new branch. CLAUDE.md's test-coverage gate: *"every `if`/`else`, `switch`
case, early-return, condition-gate, and error-return on that path must be exercised by at least one test. A
hot-path branch with no test is a delivery blocker."*

**The evidence.** Spec §3.3a's own pseudocode has **six** exits:

```go
group, err := a.store.Add(ctx, key, msg)
if err != nil {
    if group == nil            { return err }                       // 1 ← B1-14 ✔
    ok, rerr := a.cfg.release(group)
    if rerr != nil || !ok      { return err }                       // 2 ← B1-13 covers !ok only
    claim, cerr := a.store.ClaimGroup(ctx, key)
    if cerr != nil             { return cerr }                      // 3 ← UNCOVERED
    if claim == nil            { return a.overflowRetryable(…) }    // 4 ← UNCOVERED
    if relErr := a.release(ctx, claim); relErr != nil {
                                 return relErr }                    // 5 ← UNCOVERED
    return a.overflowRetryable(…)                                   // 6 ← B1-11/B1-12 ✔
}
```

Four rows for six exits, and the two half-covered ones are not the innocuous kind:

| Exit | Why it needs its own case | Killing mutant |
|---|---|---|
| `rerr != nil` | B1-13 names *"release does NOT fire"*, which is `!ok`. A **strategy that errored** is a different fact: dropping this half of the `||` **claim-and-releases a group the strategy rejected** | make the condition `!ok` only ⇒ a strategy returning `(true, err)` releases |
| `cerr != nil` | `ClaimGroup` failed. Returning `cerr` **discards the overflow classification entirely** — the caller loses `ErrOverflowDropped` and gets a store error instead | return `err` instead of `cerr` ⇒ the case's `errors.Is(…, ClaimGroup's error)` assertion fails |
| `claim == nil` | **A deliberate divergence from the normal path, and nothing tests it.** The success path returns **`nil`** for the identical condition (`aggregator.go:438-439`, *"another Handle/process is releasing this group; held"*); the new branch returns a **retryable error**. Both are defensible; the divergence is untested and undocumented | return `nil` ⇒ the member is silently lost, and no current case notices |
| `relErr != nil` | The release failed **after** an overflow. `Handle` returns the **release** error, so the Nack names the **output channel** rather than the cap — an operator debugging a full group is pointed at the wrong subsystem | return the overflow error instead ⇒ the case's message assertion fails |

**A second defect at the same site: the covering cases have nowhere to live.** Task 1's **Files** are
`adapter/memory/groupstore.go`, `routing/aggregator.go`, `adapter/memory/groupstore_test.go`,
`sizing_option_class_gate_test.go`. **B1-11 … B1-14 are `Handle` cases** — `package routing_test`, per Global
constraint 2 — and `routing/aggregator_test.go` first appears in **Task 2's** Files. Yet Task 1 Step 8 already
demands *"coverage on `adapter/memory` **and `routing`** ≥ 85% and every branch below covered."* The task is asked
to raise `routing` coverage without being given a `routing` test file.

**Third, a contract gap this exposes.** D-AN argues the direction-safety of the classification in prose — *"the
store's default is the conservative one … only positive evidence of drainability downgrades it"* — but **D-AH's
MAY clause**, the paragraph a third-party store author actually reads, does not say it. A store author reading only
the SPI cannot know that the Aggregator may only ever **DOWNGRADE** the store's classification, never upgrade it.

**Required fix.**

1. **Add rows to Task 1's table and AC-9** for `rerr != nil`, `cerr != nil`, `claim == nil` and `relErr != nil`,
   each with a named case and the killing mutant above. Call out `claim == nil` as a **deliberate divergence** from
   `aggregator.go:438-439` and say why in the godoc.
2. **Add `routing/aggregator_test.go` to Task 1's Files.**
3. **Put the downgrade-only asymmetry into D-AH's MAY clause**: the Aggregator may only ever downgrade the store's
   classification, never upgrade it.

---

## MINOR N-8 — the two stores render different counts at the same boundary, under a constraint that calls them identical

Plan 031 **Global constraint 4**: *"The error shape is fixed and **identical** in both stores and all three
dialects: `fmt.Errorf("%w: %s: group %q holds %d members, limit %d", …)`."*

**The shape is identical; the value is not.** `memory` checks **before** the append (Spec §3.4a), so `len(g.msgs)`
is the **pre-add** count:

```
msgin: permanent: msgin: message dropped by overflow policy: memory.GroupStore.Add: group "k" holds 65536 members, limit 65536
```

The dialects check **after** the member upsert (Spec §3.6.1, Plan Task 6 Step 2 — *"after the member upsert in all
three, so an idempotent re-add of an existing id at exactly the cap is a no-op"*), so the live count includes the
offending member:

```
… sql.GroupStore.Add: group "k" holds 65537 members, limit 65536
```

Both are defensible; neither is documented; and **AC-2c pins only `memory`'s render** (`holds 4 members, limit 4`),
so the `sql` twin can render anything and stay green.

**Required fix:** either say the count is *"members retained at the moment of the check"* and **pin `sql`'s render
in AC-2c** at `cap+1`, or **normalise `sql` to `len(members)-1`** so the constraint's word *"identical"* is true.
Decide it here, not at the keyboard.

---

## MINOR N-9 — the shipped `AddMember` godoc still carries the exact claim M-3 falsified

```go
adapter/database/sql/groupdialect.go:109-113
	// AddMember durably, idempotently appends one member to the group table,
	// in ONE transaction: it upserts the group row (created_at set once, via
	// the DB server clock — never a caller-supplied now), takes the GROUP
	// ROW LOCK (SELECT ... FOR UPDATE or equivalent) BEFORE reading or
	// writing any member row …
```

*"Takes the GROUP ROW LOCK (SELECT ... FOR UPDATE or equivalent)"* is **the sentence M-3 falsified for sqlite**,
which takes no row lock at all — `BEGIN IMMEDIATE`'s database-wide write lock plus `ON CONFLICT DO NOTHING` and a
separate `SELECT`. The revision corrected the *bundle's* prose (§3.6.1, D-AP) and left the **shipped godoc**
standing, and **Plan Task 5 Step 5 edits that godoc only to ADD things**: *"update its interface godoc to state the
in-transaction enforcement contract, the `msgin.ErrOverflowDropped` rollback, **and** D-AP's caller-owned-transaction
precondition."* Nothing tells the implementer to fix what is already there.

This is M-3 recurring in the one place a third-party dialect author reads.

**Required fix:** Task 5 Step 5 also **corrects** the existing sentence — *"serializes concurrent same-key adds — by
a group-row lock on postgres/mysql, by `BEGIN IMMEDIATE`'s database-wide write lock on sqlite (D-AP)"* — and Spec §4
item 6 says so.

---

## MINOR N-10 — D-AD's collision-rule table contains a row with no collision, and `sql` cannot discriminate the rule

ADR **D-AD**'s five-row table is the whole evidence for *"the `WithGroup…` prefix is a **collision rule**, not a
blanket prefix rule."* Row 3 reads: `adapter/database/sql` | `WithInboxTable` | *"the `Option` family's table
handling."*

```
$ grep -rn "^func WithInboxTable\|^func WithTable" adapter/database/sql/*.go
adapter/database/sql/inbox_dedup.go:35:func WithInboxTable(table string) InboxOption
```

**There is no `WithTable`.** `WithInboxTable` collides with nothing, so it is evidence *against* the collision rule,
not for it — it is a prefix taken where no ambiguity exists. Presented as a supporting row, it inverts.

And the rule is **under-determined in `sql` regardless**: the package has exactly **two** `GroupStoreOption`s
(`WithGroupLeaseTTL`, `WithGroupLockedBy`), both of which happen to collide. Two data points that satisfy both
hypotheses cannot distinguish them. The discriminating evidence is `adapter/memory`'s **`WithMaxGroups`** — a
group-store option with **no** prefix and **no** collision — which is a genuinely different package.

The conclusion (one name, `WithMaxGroupMembers`) is fine and the reversibility line is right. The evidence is
overstated.

**Required fix:** delete the `WithInboxTable` row, and state honestly that the rule is **proven in
`adapter/memory`** (`WithMaxGroups` unprefixed, `WithGroupClock` prefixed) and merely **consistent with** `sql`,
whose two group options cannot discriminate it.

---

## MINOR N-11 — D-AJ's "loud" boundary cites a signal the fully-default config never emits, and omits the Ack

**The claim under attack.** ADR **D-AJ**'s revised box: *"the boundary is **loud** (a WARN on the dead-letter
fallback), typed, terminal rather than retryable …"* Spec §3.3.1 and §3.11 carry the same *"plus a WARN when the
dead-letter fallback fires."* Both trace to D-AM's quote of the permanent arm: `if fellBack { c.warnInvalidFallback(id) }`.

**The evidence — in the fully-default configuration `fellBack` is false.**

```go
endpoint/consumer.go:942
	return c.policy.DeadLetter, c.policy.DeadLetter != nil
```

`invalidTarget` returns `fellBack = (DeadLetter != nil)`. On `RetryPolicy{}` — the config the whole B-1 argument is
about — `DeadLetter` is nil, so `fellBack` is **false** and `warnInvalidFallback` **never fires**.

The loudness in that configuration comes from somewhere else entirely — `divertTerminal`'s nil-sink arm:

```go
endpoint/consumer.go:1049
		c.logger.Warn("msgin: discarding message; neither an invalid-message sink (WithInvalidMessageSink) nor a dead-letter sink (RetryPolicy.DeadLetter) is configured",
			"id", d.Msg.ID(), "cause", causeForLog(cause))
```

**And that path finishes with an Ack, which the bundle never states:**

```go
endpoint/consumer.go:1074
	ackErr := c.safeAck(ctx, d)
```

So the bare outcome is **WARN + Ack** — the **source drops the message**. The spec's §3.3.1 box comes close
(*"a logged, terminal, one-line-per-message discard"*) but never says *the source Acks*, which is the operationally
load-bearing half: an at-least-once source will not redeliver it, and any upstream tracking sees success.

**A second inaccuracy in the same citation.** `warnInvalidFallback` is **`sync.Once`-deduped** —
*"It fires ONCE per consumer"* (`consumer.go:968-973`) — while Spec §3.11's table reads **per-message** (*"plus a
WARN when the dead-letter fallback fires"*, alongside *"one typed, named `ErrOverflowDropped` per rejected
member"*). One WARN for the lifetime of the consumer is a materially weaker signal than one per rejection.

**Required fix:** name `divertTerminal`'s nil-sink WARN (`consumer.go:1049`) as the loud signal in the bare config,
state that it **Acks** (`:1074`) so the source drops the message, and mark `warnInvalidFallback` as **once per
consumer**, not per message, wherever §3.11's table implies otherwise.

---

## MINOR N-12 — AC-7 is not executable

Spec 017 §6 **AC-7**: *"`memory.GroupStore` and `sql.GroupStore` each get a case asserting **§3.7's requirement**
through the `msgin.MessageGroupStore` interface — i.e. driven through the interface type, not the concrete type."*

**§3.7 is a four-clause paragraph** — a MUST (bound), a MUST (report `ErrOverflowDropped`), a SHOULD (mark
`Permanent`) and a MAY (return the live snapshot). *"Asserting §3.7's requirement"* names none of them, and the
four are already covered elsewhere: the MUSTs by AC-1/AC-4, the SHOULD by AC-1/AC-4.5, the MAY by AC-1b/AC-4.4.
What AC-7 uniquely adds is the **interface-typed drive**, which is a *style* requirement, not a new assertion.

This is the standing bar the spec sets for itself in §6's own opening — *"Every acceptance criterion below is
executable as written — the standing bar after Plan 029's audit found an unexecutable AC in **five consecutive
rounds**."* Six.

**Required fix:** either **name the clause and the assertion** (e.g. *"MUST-report: `errors.Is(err,
msgin.ErrOverflowDropped)` where the store is held in a `msgin.MessageGroupStore` variable"*), or **delete AC-7**
and make the interface-typed drive a stated style requirement on AC-1 and AC-4.

---

## MINOR N-13 — four residual off-by-one citations, all introduced by revision 2's own new prose

The revision fixed round 1's m-1, m-2 and m-3 cleanly. The class returned through the text those fixes were written
in. All four verified at `46803c6`:

| Cited as | Actually | Cited in |
|---|---|---|
| `endpoint/consumer.go:861-869` | the quoted block **starts at `:860`** (`n := c.attempts(d)`); `switch {` is `:861` | Spec §3.3.1, ADR D-AM |
| `sqlite/groupdialect.go:63` | `BEGIN IMMEDIATE` is **`:62`** | Spec §7.1 part 2 table |
| `mysql/groupdialect.go:93-96` | the **comment** is `:85-92`; `:93-96` is the `INSERT` statement | Spec §3.6.1 + §7.1, ADR D-AP |
| `groupstore.go:185-198` | godoc **`:185-188`**, func **`:189-199`** | Spec §1.2, ADR D-AN |

```
$ sed -n '860,861p' endpoint/consumer.go
	n := c.attempts(d)
	switch {
$ sed -n '62p' adapter/database/sql/sqlite/groupdialect.go
	if _, err := conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
$ sed -n '85p;93p' adapter/database/sql/mysql/groupdialect.go
		// Upsert-and-X-LOCK the group row FIRST (H1): INSERT ... ON DUPLICATE KEY
		if _, err := tx.ExecContext(ctx,
$ sed -n '185p;189p' adapter/memory/groupstore.go
// AbandonGroup releases the lease WITHOUT deleting: the claimed members return
func (s *GroupStore) AbandonGroup(_ context.Context, claim msgin.MessageGroupClaim) error {
```

Each claim's **content** is correct; only the coordinates are wrong. Fix the four.

---

## MINOR N-14 — B-2's fix landed in the spec and the plan and NOT in the ADR

```
$ grep -n "same commit\|red suite\|exact set equality" docs/adrs/0033-group-member-bounds.md
$   (no output)
```

**B-2 was round 1's most structurally consequential finding** — it restructured the plan's task list. The spec
carries it twice (§1.4's 🔴 box, AC-8's ORDERING box) and the plan carries it three times (Global constraint 8's
box, Task 1 Step 2, Task 9's preamble). **D-AL — the decision that owns the class gate — says nothing about it.**
D-AL discusses the arm literals, the count sites, `methodCount`, the stated limitation and Plan 032 serialization,
and never records that **half 1 is exact set equality in both directions**, or the consequence that **the key must
ship in the option's own commit**.

That matters because D-AL is where a future increment adding a sizing option will look. A reader of D-AL alone
learns the gate is *"extended by hand"* and not that extending it late makes root red.

**This is the project's named failure mode — the stored lesson *"fold into all three artifacts"*: 4 of 6 unclean
findings in an earlier audit were the ADR missing an edit the spec and plan got. It recurred here, on the highest-
impact finding of the round.**

**Required fix:** D-AL records both halves — half 1 is **exact set equality in both directions**, so the key and its
conformance row **ship in the option's own commit**, and *"observe the RED first"* is a within-task TDD step. And
before revision 3 is closed, **diff all three artifacts against each other for EVERY finding**, not only this one.

---

## Part 3 — point-by-point answers to the audit brief's eight questions

The coordinator's brief put eight questions to this round. Answered in order.

**1. D-AM — does the settlement path behave as the revision claims?** **Yes, as claimed.**
`endpoint/consumer.go:843` returns before `:860`, so on the permanent arm `MaxAttempts` is genuinely never
consulted. With **neither** sink configured the outcome is `divertTerminal` (`:1033`) → nil-sink WARN (`:1049`) →
**`safeAck`** (`:1074`): a WARN, a discard, **and an Ack**. **The logged terminal discard is acceptable as the
honest ceiling on what a classification can buy, and the revision states it as such.** Two defects around it,
though: D-AJ names the **wrong WARN** (**N-11**), and the drainability premise the whole classification rests on is
**memory-only**, with a live `sql` counter-case (**N-3**).

**2. D-AN — is the empty-leased-snapshot hazard real?** **Real, but correctly avoided — this is NOT a defect.** The
store classifies on **`g.leased`** (`adapter/memory/groupstore.go:43`), not on whether the release fires, so an
empty live snapshot cannot be mistaken for a drainable group. `defaultRelease` guards `len(msgs) == 0`
(`routing/aggregator.go:223`), and an empty snapshot **already reaches caller closures today** through the leased
dedup path — so nothing new is exposed to a caller's strategy. Two real findings came out of tracing it anyway:
the **downgrade-only asymmetry** is argued in D-AN's prose and absent from **D-AH's MAY clause**, where a
third-party store author would read it, and the full ordering trace surfaced **four uncovered exits** — both folded
into **N-7**.

**3. D-AO — was the ordering defect real, and is the fix complete?** **Real, and correctly specified.** Confirmed at
`groupstore.go:130` (the `seen` lookup), `:133` (the `g.ids` insert) and `:135` (the append): a check placed after
`:133` makes the redelivery return `(snapshot, nil)`, `Handle` return `nil`, and the source **Ack a message that was
never appended**. **No other ordering hazard exists at that site.** The group-count arm cannot leak an empty group —
`checkRange`'s `lo = 1` makes `0 >= max` impossible — and the rejection path never touches `g.ids`.

**4. The five soft spots the brief flagged.**

| # | Soft spot | Verdict |
|---|---|---|
| 1 | the empty snapshot | **Not a defect** — see answer 2 |
| 2 | the sink ceiling | **Honestly stated in D-AM, over-claimed in D-AJ** → **N-11** |
| 3 | m-9's naming rationale | **Decision sound, evidence over-derived** → **N-10** |
| 4 | the Plan 032 collision | **Handled correctly by both plans** — no finding |
| 5 | the AST invariant covering only `memory` | **CONFIRMED a real gap** → **N-4** |

**5. Task 1 — is it coherent as a green unit?** **Yes.** The coupling argument (the cap check introduces M-6's
deadlock; `Handle`'s branch removes it, so they cannot be split) is right, and the stated fallback seam —
*"store + `Handle` + gate as one commit, the wrapped group-count class fix as a follow-up"* — is safe. Two gaps:
the **branch table is short by four rows** and the **Files list by one file** → **N-7**.

**6. Re-derivation — do the bundle's structural claims reproduce?** **Spot-checked and correct** across
`adapter/memory/groupstore.go`, `adapter/database/sql/groupstore.go`, `routing/aggregator.go`, `reliability.go`,
`message.go`, `sizing_option_class_gate_test.go` and the three dialects. **Four residual off-by-ones** → **N-13**.

**7. Plan 032 convergence — do the two increments agree in both landing orders?** **Yes, in both orders.** Current
arms are `9 + 1 + 3 + 6 = 19`.

| Order | Result |
|---|---|
| **031 first** | 031 ⇒ `11 + 1 + 3 + 6 = 21`; 032 then moves 3 `deferred` → `fixed` ⇒ `14 + 1 + 0 + 6 = 21` |
| **032 first** | 032 ⇒ `12 + 1 + 0 + 6 = 19`; 031 then ⇒ `14 + 1 + 0 + 6 = 21` |

Both converge on **21**. Plan 032 explicitly records that 031 puts its rows in **`fixed`**, never `deferred` — which
is what makes the two orders agree rather than merely happen to.

**8. What is new ground versus a re-run of round 1?** **New ground: N-1 (BLOCKER), N-2, N-3, N-5, N-12.** The
remainder attach to a round-1 finding whose fix landed but is flawed.

---

## What I checked and found CLEAN

Recorded so a round 3 does not re-derive it. Everything below was attacked in round 2 and survives.

**Round 1's clean list still holds.** Nothing in revision 2 disturbed §1.1's four-release-path table, §1.4's
two-mechanism analysis, the quadratic claim, the `checkRange`/`ErrInvalidCapacity`/`ErrOverflowDropped` producer
counts, or `GroupDialect`'s *"pre-1.0 (v0) contract"* reservation. Those are round 1's findings and are not
re-listed.

**New material in revision 2 that survives attack.**

- **§3.4a's cap-check placement is exactly right**, and its three killing mutants (above the `seen` lookup / below
  the `g.ids` insert / folded inside `if id != ""`) are the correct three. The pseudocode compiles mentally against
  the shipped `Add` with no gaps. **The only defect near it is N-6's fixture**, which is a test-input problem, not a
  placement problem.
- **§3.5's boundary arithmetic table is correct** — the `C`-th `Add` sees `len == C-1`, appends, returns `C`, and
  `WithCompletionSize(C)` fires. The *"maximum attainable is exactly `C`, with zero margin"* claim reproduces.
- **D-AM's permanent-arm trace is accurate** — `IsPermanent` → `invalidTarget` → `divertTerminal`, never consulting
  `MaxAttempts`, terminal by construction. **N-11 attacks only the *loudness* citation**, not the classification.
- **§3.3a's `group == nil` compatibility arm is right**, and the existing `(nil, nil)` guard at
  `aggregator.go:416-424` really is untouched by it.
- **D-AF's claim-window analysis survives**, including the honest *"zero-delay busy-wait under `RetryPolicy{}`"*
  residual-hazard note — which is the kind of naming this project's gates exist to produce.
- **AC-9 branch 2 remains the sharpest single row in the bundle.** A cap check written `> cap` after the append
  admits `cap+1` while every overflow test passes.
- **AC-6's "no test grows a group to a ceiling" constraint** is correctly restated as an AC and correctly reasoned
  from Spec 016's 8.6 s / 48.3 GiB measurement, which the bundle correctly **cites rather than re-derives**.
- **Task 10 Step 3b (`GOARCH=386 go vet`) is a genuine improvement** over round 1's required fix — it converts M-1
  from a prose warning into an executable gate.
- **AC-10's two-probe design is right**, and the table stating *what each probe proves* is the correct response to
  M-8. Probe A (a real leaf module) answers the coverage question; probe B answers the ordinary one.
- **The plan's "counted set" box** is well-placed and correctly warns against pattern-matching one store onto the
  other.
- **Global constraints 1-3, 5-7 and 9-12** are correct as written. **Constraint 4 is defective (N-8)** and
  **constraint 8 is defective (N-1)**.

**Gates verified non-vacuous or verified clean.**

- **The docs-link gate baseline is unchanged** at this branch point: exactly the two known arm-1 false positives
  (`docs/plans/016-aggregator.md -> docs/plans/m` and
  `docs/specs/006-cron-source.md -> docs/specs/factory(fireTime`, both Go identifiers leaking from line-wrapped
  inline code) and **zero** arm-2 hits.
- **All three bundle files' internal links resolve.**
- **`harness` still contributes no half-1 key** — `grep -rnE "^func [A-Z][A-Za-z]*\(.*\b(int|int64)\b"
  adapter/database/sql/harness/*.go` returns nothing — so Spec AC-5's box and Task 7 Step 5's box remain correct
  and remain necessary.

---

## Auditor's method note

Every command in this record was run on the tree at **`46803c6`** (current `main`, carrying the revision-2
artifacts) with `GOTOOLCHAIN=go1.25.13` on darwin/arm64. The `harness`/`dbtest` module inventory, the
`RecoverInterval` pair, the `reapInterval` read, the `WithSharedTransaction` return type, the three-call-site
`SelectMembers` enumeration, the `msgin.New` id stamp, the `invalidTarget`/`divertTerminal` trace, the four
off-by-one coordinates and the ADR grep are all first-hand output, not transcription. **No file in the repository
was modified.**

Round 2 deliberately did **not** re-audit [Spec 018](../specs/018-byte-cap-ceilings.md) /
[ADR 0034](../adrs/0034-byte-cap-ceilings.md) / [Plan 032](032-byte-cap-ceilings.md): they are a different
increment under concurrent revision, and the only interaction with this bundle — serialization on
`sizing_option_class_gate_test.go` — is already correctly stated in all three artifacts (round 1's **B-3**, clean).

---

## Round-3 corrections to this record — LATER ADDITION, not the auditor's

> **Everything above this heading is the auditor's.** This section was added afterwards, while folding round 2's
> findings into revision 3 of the artifacts, and is kept **separate** rather than edited into the findings above —
> an immutable record stays the auditor's, not a composite. **Two cited coordinates are off by one to six lines.
> Neither changes any finding's content, verdict or required fix.**

| Cited in | The auditor wrote | Re-derived at `46803c6` |
|---|---|---|
| **B-1**'s table row and **N-11** | `divertTerminal` … `safeAck`s (**`consumer.go:1074`**) | **`:1073`** — `sed -n '1073p' endpoint/consumer.go` → `\tackErr := c.safeAck(ctx, d)` |
| **N-3**'s `ExpiredGroups` code block | `postgres/groupdialect.go:**277-281**` | **`:275-282`** — `beforeSet := !before.IsZero()` is `:275`; the bound parameters are `:282` |

**Revision 3 of Spec 017 / ADR 0033 / Plan 031 uses the corrected coordinates**, so the artifacts and this record
disagree by design on exactly these two numbers. Everything else the auditor cited reproduces as written — including
`consumer.go:843`/`:860`, `:942`, `:968-973`, `:1033`, `:1049`; `groupstore.go:130`/`:133`/`:135`/`:220`/`:348`;
`aggregator.go:223`/`:412`/`:438-439`/`:530-532`/`:558-565`; `sqlite/groupdialect.go:52`/`:62`/`:114`;
`harness/testkit.go:87`; `harness/groupstore.go:345`; and `message.go:178-180`/`:198`.

### The score line versus the table — a one-row gap, left open

**The verdict's score line says `12 clean LANDED, 8 LANDED-BUT-FLAWED, 1 (M-8)`. The 21-row table says
`13 / 7 / 1`.** Both total 21; no row is missing, duplicated or unclassified. Tallied by name off the table:

| Disposition | Findings |
|---|---|
| **LANDED-BUT-FLAWED (7)** | B-1, B-2, M-2, M-3, M-5, M-6, m-9 |
| **LANDED (13)** | B-3, M-1, M-4, M-7, m-1, m-2, m-3, m-4, m-5, m-6, m-7, m-8, m-10 |
| **LANDED, defensible ADR omission (1)** | M-8 |

**Neither figure was altered.** One finding is scored LANDED-BUT-FLAWED by the summary and LANDED by the table, and
which one cannot be settled from this record alone.

**The most probable candidate is `m-8`, and it is named as a candidate only — not as a correction.** Its table row
is evidenced entirely on the *module count* (*"Six stated once in Tech stack, Sizing footer and D-AG
reversibility"*), which is true; but the same increment's **Task 6 Step 5 gate still enumerated four modules**
(*"root + all three dialect modules"*), so the two modules m-8's fix added to the count are exactly the two the
per-task gate could not see. That is a flaw in m-8's fix, and it is the second half of **N-1**. If the summary
counted m-8 flawed on that basis while the table row addressed only the count, the two are reconciled at 12/8/1.

**This is the project's `43 ≠ 43` lesson in miniature: two counts of the same set agreed on the total and differed
on the partition. Reconcile by name, never by count** — and note that **nothing downstream depends on the
resolution**: all 21 dispositions, and every N-finding they attach to, are unchanged either way, and revision 3
folds in all 14 findings regardless.

**Narrowed by the coordinator, who holds the auditor's report verbatim (LATER ADDITION).** The 21-row table above
is a faithful transcription — checked row by row against the source, including `m-8`'s verdict cell, which reads
`LANDED` in the original. **So the gap is not a transcription error introduced by this record; it is internal to
the auditor's own report, between its table and its summary line.** That rules out the one explanation that would
have made this record untrustworthy, and leaves the discrepancy where it actually is.

**Which of the two figures governs: the TABLE.** Every table row carries its own evidence and is independently
checkable; the score line is a tally *derived* from those rows and carries none. A derived count cannot overrule
the evidenced partition it was derived from. The by-name tally — **13 LANDED / 7 LANDED-BUT-FLAWED / 1 M-8** — is
therefore the operative one, and the score line is recorded as an off-by-one in the auditor's summary.

The `m-8` candidacy above remains **a candidate and nothing more**. It is a plausible reconstruction of what the
summary may have intended, and reconstructing an auditor's intent is precisely what this record was corrected to
stop doing — see the provenance note at the top. Do not promote it to a finding.

---

**VERDICT: NOT SAFE TO IMPLEMENT.**

*The revision engaged all 21 findings and regressed none — but it did not generalize its own two structural fixes.
B-2's "a cross-module edit is a red commit" was applied to the class gate and not to the `AddMember` signature, so
the Tasks 5+6 commit ships two non-compiling modules behind a gate that cannot see them (**N-1**). M-3's "one
mechanism asserted for three engines" was fixed for the transaction wrappers and then recurred three times in the
same revision — for the reaper (**N-3**), for the shared `SelectMembers` helper (**N-5**), and for the very SPI
godoc M-3 was about (**N-9**). A fix that stops at the instance it was reported against is not a fix of the class.*

