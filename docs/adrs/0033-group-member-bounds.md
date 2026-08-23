# ADR 0033 — A message group's member bound lives at the store, not at the release decision

- **Status:** **ACCEPTED — 2026-08-23. Decisions D-AC…D-AT ratified by the user.** Revision 5, after five
  adversarial audit rounds (round 5: **SAFE TO IMPLEMENT**, 0 findings). Written before any code, per
  [CLAUDE.md](../../CLAUDE.md)'s design-time gate.
  - **How it was ratified.** The decisions were taken while the user was away, which is why each carries a
    `REVERSIBILITY:` line. Presented back as four load-bearing choices rather than eighteen — **D-AC** (the bound
    lives at the store), **D-AG** (the expensive one: in-transaction SQL enforcement, six modules), **D-AF**
    (count live + claimed, which forces the per-add `COUNT(*)`) and **D-AM** (classify by cause) — the user
    ratified all eighteen with *"go with best practices"*, stating no strong preference.
  - **🔴 One standing constraint was added at ratification and governs every knob from here:** *"this library
    must be flexible with sensible default but with opt-in available."* It sharpens
    [CLAUDE.md](../../CLAUDE.md)'s "Sensible defaults (opinionated, but overridable)" section. **This ADR
    complies:** `WithMaxGroupMembers` ships a sensible default (`1<<16`), is overridable up to `1<<20`, and its
    godoc states the default and its rationale. It has **no off-state**, which is deliberate and defensible here
    — unlike a byte cap, the safe value is *not* unknowable to the library: the cost curve is quadratic and
    measured (65,536 members ≈ 48 GiB of allocation churn), so a caller above 1,048,576 is already pathological
    and the bound protects them. Should a real caller ever need one, adding it is **purely additive** — one
    constant and one branch, no breaking change. Compare `endpoint.WithMaxPayloadBytes`, which *is* opt-in
    (`n <= 0 disables`) precisely because a caller's legitimate payload size **is** unknowable.
  - **Round 1 verdict: NOT SAFE TO IMPLEMENT** — 3 BLOCKERs, 8 MAJORs, 10 MINORs, recorded immutably in
    [`docs/plans/031-audit-round-1.md`](../plans/031-audit-round-1.md). Revision 2 folded every finding back in.
    The two that reshaped the design are **B-1** (the overflow error hot-spins forever on the shipped zero-value
    `RetryPolicy`) and **M-6** (the cap check destroys a release opportunity the current code preserves) — they
    produced the new decisions **D-AM**, **D-AN** and **D-AO**, and forced rewrites of **D-AE**, **D-AJ** and
    **D-AK**.
  - **Round 2 verdict: NOT SAFE TO IMPLEMENT** — 1 BLOCKER, 6 MAJORs, 7 MINORs, recorded immutably in
    [`docs/plans/031-audit-round-2.md`](../plans/031-audit-round-2.md), against a fix-verification score of
    **12 clean LANDED, 8 LANDED-BUT-FLAWED, 1 (M-8) landed with a defensible ADR omission, 0 NOT LANDED,
    0 REGRESSED** *(the record reconciles that summary against its own 21-row table by name and leaves a one-row
    gap open; nothing downstream depends on it)*. **The lesson is not the count: revision
    2 failed to GENERALIZE its own two structural fixes.** B-2's *"a cross-module edit is a red commit"* reached
    the class gate and not the `AddMember` signature (**N-1**); M-3's *"one mechanism asserted for three engines"*
    was fixed for the transaction wrappers (D-AP) and then recurred for the **reaper** (**N-3**, D-AM's premise),
    the shared **`SelectMembers`** helper (**N-5**, D-AP's `LIMIT`) and the **shipped SPI godoc M-3 was about**
    (**N-9**). Revision 3 adds **D-AR** and **D-AS** and repairs the premises of D-AM, D-AP, D-AQ and D-AL.
  - **Round 3 verdict: NOT SAFE TO IMPLEMENT** — 1 BLOCKER, 6 MAJORs, 3 MINORs, recorded immutably in
    [`docs/plans/031-audit-round-3.md`](../plans/031-audit-round-3.md), against a fix-verification score of
    **9 clean LANDED, 5 LANDED-BUT-FLAWED, 0 NOT LANDED, 0 REGRESSED** — *the auditor's summary line reads 8/6;
    both total 14, the record leaves the one-row gap **unreconciled**, and **the table governs** because each row
    carries its own evidence while the score line is a derived tally. **Second consecutive round with that gap**,
    recorded there as a method defect in the audit apparatus rather than in this bundle.* **Its lesson, verbatim: *revision 3
    generalized round 2's two structural fixes correctly, but stopped the fix at the boundary of each finding's
    own wording.*** Both generalizations — Plan 031's *compiles-against* rule and **D-AS** — survive.
    **Revision 4 is the first revision since round 1 to change a DECISION rather than a premise:**
    - **D-AF is REVERSED** — both stores now count **live + claimed** (audit **NEW-7**, the round's only genuine
      design flaw). With `sql` counting live only, `ClaimGroup` zeroed the live count and the **durable member
      table was unbounded**. This is the decision Spec 017 §8 item 1 listed as *"most likely to be reversed"*, and
      it is reversed **toward** the alternative that item named.
    - **D-AT is NEW** — Plan 031's Task 3 is **reordered after Tasks 5+6, never split** (audit **NEW-1**, the
      BLOCKER), and the reasoning is recorded here so the split is not re-proposed.
    - **D-AH's downgrade clause is RESTATED** — the revision-3 form is false for two of §3.3a.1's own six exits
      (audit **NEW-6**).
    - **D-AE gains the count-divergence decision** it never received in revision 3 (audit **NEW-8**) and the
      **site-string** decision for the three dialects (audit **NEW-5**).
    - **D-AN loses its *"a bug in the drain path costs a dead-letter, not a production-down spin"* claim**, which
      is untrue of exits 3 and 5 (audit **NEW-6**).
    - **D-AR gains the declaration's FILE** (audit **NEW-4**), **D-AQ its parse-set consequence**, and **D-AM its
      `locked_by` discriminator** for `sql` (a consequence of D-AF's reversal).
  - **Round 4 verdict: NOT SAFE TO IMPLEMENT** — 1 BLOCKER, 3 MAJORs, 4 MINORs, recorded immutably in
    [`docs/plans/031-audit-round-4.md`](../plans/031-audit-round-4.md), against a fix-verification score of
    **7 clean LANDED, 3 LANDED-BUT-FLAWED, 0 NOT LANDED, 0 REGRESSED** — *and for the first time on this
    increment the score line and the auditor's own table **AGREE**, because round 3's procedural remedy was
    applied: the line was **generated by counting the table**, not written alongside it.* **Its lesson, verbatim:
    *the design is sound; the execution instructions are not synchronised with the tree they run against.***

    🔴 **ROUND 4 FALSIFIED NO DECISION IN THIS ADR — and that is the load-bearing result, not the count.**
    **D-AF's reversal survives attack**, and round 4 verified **every code claim it rests on**, one by one rather
    than by sampling: the three `*CountMembers` helpers exist with an identical signature; **none carries a
    `claimed_epoch` predicate**, so all three already count live + claimed and the reversal needs **zero new
    SQL**; each has **exactly one** existing caller, that dialect's own `SettleGroup`, so an `AddMember` caller
    disturbs nothing; the **`locked_by` read is genuinely zero-round-trip in all three dialects** (postgres
    already `RETURNING`s from the group row, mysql and sqlite already `SELECT` it); the **nine `*SelectMembers`
    sites** reproduce, still take a **string** predicate, and `AddMember`'s site passes `"claimed_epoch IS NULL"`
    — so *"the count cannot be derived from that `SELECT`"* is verified at the source. **D-AT, D-AH's restated
    clause, D-AE's two new sub-decisions, D-AR's file mandate and D-AS all stand.**

    **What round 4 broke is the execution layer.** [Plan 032](../plans/032-byte-cap-ceilings.md) **landed** at
    `f39725d` and rewrote `sizing_option_class_gate_test.go`. Revision 5's edits here:
    - **D-AL** gains the two exact-map assertions (`wantArms`, `byArm`) it never named (**R4-1**, the BLOCKER),
      the corrected arm **composition** and the corrected literal rule (**R4-2**), and it stops enumerating count
      sites by line number.
    - **D-AT `:1346`** names the assertion its Step 0 gate must make, because the plan implemented a **count**
      where this ADR already said *"greps for all three declarations"* (**R4-4**).
    - **D-AF consequence 2** and the **Consequences** carry the crashed-lease window at its true width — **up to
      `2 × leaseTTL`**, not one (**R4-3**) — and the per-engine `COUNT(*)` cost (**R4-8**).
    - The **`classifyQueryErr` Consequences bullet** is scoped to the not-leased arm, which D-AF's own reversal
      falsified in revision 4 (**R4-6**).
  - **R4-7** — *all three artifact headers assert re-derivation against `d2c69fe`, a commit that is neither the
    measured tree nor `main`* (Spec `:44`, Plan `:38`, **ADR `:55`**) — **closes with R4-2's header work**, since
    the two take the same edit. **Revision 5 closes all eight of round 4's findings.** *(R4-7 reached
    [`031-audit-round-4.md`](../plans/031-audit-round-4.md) **unlabelled**, folded into R4-2's bullet; its
    write-up there is the coordinator's restatement rather than the auditor's verbatim text, and the provenance is
    marked in place. Note also that the relayed ADR coordinate was `:44`; the re-derivation sentence is at `:55` —
    **the citation of the stale-citation finding was itself off by eleven**, which is the class R4-2 exists to
    close.)*
  - 🔴 **REVISION 5 CHANGES NO DECISION, so a round 5 is not automatically warranted.** Revision 4 warranted one
    because it reversed D-AF and so was not the bundle round 3 had audited; **nothing here reverses, weakens or
    re-scopes a decision, and no round-4 finding is left open.**
  - 🔴 **Decisions D-AC through D-AT were taken WITHOUT USER RATIFICATION.** The user was away when this bundle was
    drafted, away again when round 1's findings were dispositioned, and away again for rounds 2, 3 and 4. Every
    decision below is **open to reversal**; [Spec 017 §8](../specs/017-group-member-bounds.md) lists the ones that
    most deserve a second look, and each such decision carries a **REVERSIBILITY** line stating what undoing it
    costs.
  - **All structural claims re-derived at `de38a95`** — **the tree at the time revision 5 was written**,
    post-Plan-032 — not at `d2c69fe` where revisions 1-4 measured them, and not at `2b2dec1` where revision 1 did.
    *(Round 4 audited `f39725d`. Revisions 1-4 called `d2c69fe` *"current `main`"*; that was true when written and
    is now false on both halves — audit **R4-2**.)*
    [Plan 030](../plans/030-post-029-maintenance.md) landed mid-audit and shifted every line number in
    `adapter/memory/groupstore.go` (from `:93` down) and `adapter/database/sql/groupstore.go` (from `:207` down)
    by one, and rewrote 135 lines of `sizing_option_class_gate_test.go` (audit **B-3**, **M-1**);
    [Plan 032](../plans/032-byte-cap-ceilings.md) then rewrote **237 more** and moved `adapter/http/helpers.go`'s
    `checkRange` from `:64` to `:73`.

    > 🔴 **AND A GODOC-ONLY COMMIT (`de38a95`) LANDED WHILE REVISION 5 WAS BEING WRITTEN, SHIFTING 41 CITATIONS.**
    > It changed **comments only** — and still moved **every `adapter/database/sql/groupstore.go` coordinate at or
    > below `:207` by +1** (`:211`→`:212`, `:271`→`:272`, `:273`→`:274`, `:284-297`→`:285-298`, `:348`→`:349`,
    > `:365`→`:366`). All 41 were corrected **mechanically, not by eye**. `adapter/memory/groupstore.go` is net
    > **0**; `sizing_option_class_gate_test.go`, `routing/` and both `helpers.go` files were untouched.
    > **This is audit R4-2's thesis demonstrated live: a line number is falsifiable by a comment edit.** It is why
    > the load-bearing class-gate coordinates in this ADR are now written as the **grep that locates them**, and
    > why Plan 031 Task 9's site table is **generated by a script** rather than enumerated.
  - The decision-letter series continues from [ADR 0032](0032-sizing-option-bounds.md), which ends at **D-AB**.
- **Prompted by:** [Spec 017](../specs/017-group-member-bounds.md); the backlog item in
  [`docs/HANDOVER.md`](../HANDOVER.md) §6 item **7**, filed at Plan 029's delivery gate and widened here from one
  option to **three release paths plus two stores**.
- **Realized by:** [Plan 031](../plans/031-group-member-bounds.md).
- **Extends** [ADR 0032](0032-sizing-option-bounds.md): this ADR is the direct successor to that defect class. It
  reuses **D-X** (reuse the existing sentinels; mint none; wrap the value into the error), **D-Z**'s ceiling
  reasoning for the value 65,536, and **D-AB**'s membership criterion, all unchanged. It **does not supersede** any
  part of ADR 0032 — `routing.WithCompletionSize`'s ceiling stays exactly as delivered.
- **Related:** [ADR 0031](0031-nil-option-elements.md) **D-R** (per-package unexported helpers, four independent
  copies rather than a shared internal package — the precedent for the fifth `checkRange`); ADR 0021 (the SQL group
  store and its `GroupDialect` SPI).

## Context

**Plan 029 bounded one of the four ways an Aggregator decides a group is complete.** `routing.WithCompletionSize`
got a ceiling of 65,536 members (`routing/aggregator.go:33`), enforced in `NewAggregator`
(`aggregator.go:353-358`) and gated on `cfg.completionSizeSet` — a field **only** `WithCompletionSize` writes
(`aggregator.go:156`). The other three paths never set it, so the check never runs for them:

| Path | Site | Why no bound can be expressed there |
|---|---|---|
| `WithReleaseStrategy(fn)` | `aggregator.go:116` | `fn` is a caller-supplied closure; the option body is `c.release = fn` |
| `WithReleaseWhen(fn)` | `aggregator.go:128` | sugar over the above |
| `defaultRelease` — **no option at all** | `aggregator.go:222` | reads the threshold from the **first member's `msgin.HeaderSequenceSize` header**: the bound is **DATA** |

**The accumulation itself is unbounded in both first-party stores.** `memory.GroupStore.Add`
(`adapter/memory/groupstore.go:118-137`) admits new **keys** against `WithMaxGroups` (`:123-125`) and then appends
at `:135` with **no per-group cap**; it also `slices.Clone`s the group on every call (`:131`, `:136`), so per-group
cost is **quadratic in time**. Spec 016 §1.4 measured reaching 65,536 members at **48.3 GiB of allocation churn and
8.6 s**. `SettleGroup` (`:160-183`) shrinks the group only after a successful release, and the reaper is opt-in —
with no `WithGroupTimeout`, `memory.GroupStore.RecoverInterval()` returns `0` (`:220`) and `Aggregator.Run` blocks
on `ctx.Done()` without sweeping. **A group whose release never fires grows monotonically forever.**

`adapter/database/sql.GroupStore` is worse in kind and had not been examined: it has **no member cap and no
group-count cap of any kind**, and its `Add` (`groupstore.go:251-277`) re-fetches and re-decodes **every live
member** on every arrival, per `GroupDialect.AddMember`'s own contract (`groupdialect.go:108-126`). The decode runs
in `decodeGroupRows` (`groupstore.go:366`), reached from `Add`'s tail call at `:275`.

**The class gate cannot reach any of this, and not for a fixable reason.** `sizing_option_class_gate_test.go`'s
`isIntOrInt64` (reached from `hasIntOrInt64Param` — locate both with
`grep -n 'func hasIntOrInt64Param\|func isIntOrInt64' sizing_option_class_gate_test.go`) matches
`*ast.Ident{"int"|"int64"}` through `...`, slice and pointer wrappers.

> 🔴 **NAMED, NOT NUMBERED — and that is a method change, not a tidy-up** (audit **R4-2**). Revisions 1-3 cited
> these at `:191-202`; round 3's **NEW-9** corrected them to `:215`/`:231`; **`f39725d` moved them to
> `:243`/`:259` one commit later, falsifying the correction itself.** The off-by-one citation class has now
> returned in **four consecutive rounds**, and round 4 is the first in which it returned **through** the fix
> rather than beside it. **A hand-typed coordinate against a file another increment is rewriting cannot be kept
> true by being retyped more carefully** — so load-bearing class-gate coordinates in this ADR are now written as
> the **grep that locates them**, and Plan 031 Task 9's site table is **generated by a script**, not enumerated.

`WithReleaseWhen`'s parameter is an `*ast.FuncType` (falls through to `false`); `WithReleaseStrategy`'s is an
`*ast.Ident{"ReleaseStrategy"}` (a named type — a limitation the gate's header already states for
`type Bytes int64`); and `defaultRelease` has no parameter at all. **Spec 016's class is defined over options with
an integer parameter. A bound that arrives as a closure or as a header value is outside its premise, not outside
its implementation.**

## Decision

### D-AC — the bound lives at the ACCUMULATION SITE (the store), not at the release decision

**Decision.** The per-group member bound is enforced in `MessageGroupStore.Add`, in both first-party stores, and
stated as a requirement on the SPI. It is **not** expressed at, derived from, or validated against the release
strategy.

**Rationale, in order of weight — REWRITTEN in revision 2 (audit M-4).** Revision 1 gave three reasons; two of them
were false, and the audit demonstrated it. They are replaced rather than patched, because the conclusion is right
and deserves reasons that survive attack.

1. **Only the store can refuse a member BEFORE retaining it.** This is the load-bearing reason and revision 1 did
   not state it. `store.Add` has exactly **one** caller in the workspace — `Aggregator.Handle`
   (`routing/aggregator.go:412`) — so *"the store observes every member"* does **not** discriminate: `Handle`
   observes exactly the same population. What discriminates is *when*. By the time `Handle` sees `Add`'s returned
   snapshot, **the member has already been appended and is already retained**; a check there would bound the
   *reported* size while the heap grew anyway. That is the same false-safety inversion this ADR rejects for SQL
   enforcement (A) below, applied to the memory store.
2. **Completeness across release paths.** Three of the four release paths are opaque to the library: two are
   caller-supplied closures, one is a message header. **No bound expressed at the release decision can ever cover
   more than path 1** — which is exactly the state Plan 029 shipped. (Note this argues against *the release
   decision*, not against `Handle`; reason 1 is what argues against `Handle`.)
3. **Precedent inside the same function.** `memory.GroupStore`'s existing admission check for group **count**
   already lives in `Add` (`groupstore.go:123-125`). The member check goes a few lines below it, under the same
   mutex, returning the same sentinel. The two overflow arms become symmetric rather than one being a special case
   living in a different package.
4. **A store used directly, without an Aggregator, is otherwise unbounded.** `msgin.MessageGroupStore`
   (`groupstore.go:37`) is public; nothing obliges a consumer to drive it through `routing.Aggregator`. A bound in
   `routing` would leave the direct user unprotected.

> **DELETED in revision 2, as false:** revision 1's reason 3 — *"a bound stated in `routing` protects only the
> stores `routing` ships with."* `routing` ships **no** stores; both first-party stores live in `adapter/memory`
> and `adapter/database/sql`, and a check in `Aggregator` would in fact cover *every* store an Aggregator is
> pointed at, including third-party ones. The sentence asserted the opposite of the fact. Reason 4 above is the
> true, narrower form of the benefit it was reaching for.

**Consequence for `WithCompletionSize`:** its ceiling is **not** removed. Two bounds on the same quantity is not
redundancy — the option ceiling is a *construction-time* rejection of a nonsense configuration (best
debuggability), the store cap is a *runtime* bound on the accumulation (complete coverage).

**REVERSIBILITY:** this is the load-bearing decision; reversing it discards the increment. Everything below is
downstream of it.

### D-AD — two new options, reusing `checkRange` and `msgin.ErrInvalidCapacity`; mint no sentinel

**Decision.** `memory.WithMaxGroupMembers(n int)` and `sql.WithMaxGroupMembers(n int)`, both **default `1 << 16`**
(65,536), both with **ceiling `1 << 20`** (1,048,576), both validated in their `NewGroupStore` with the shipped
per-package unexported `checkRange` helper against `msgin.ErrInvalidCapacity` — exactly as `WithMaxGroups` does
today (`adapter/memory/groupstore.go:105-108`).

**ONE NAME IN BOTH PACKAGES — settled in revision 2 (audit m-9).** Round 1 objected that `adapter/database/sql`'s
`GroupStore` options are `WithGroup…`-prefixed (`WithGroupLeaseTTL`, `WithGroupLockedBy`) and that
`sql.WithMaxGroupMembers` breaks that convention. Re-deriving the convention rather than accepting its statement
shows it is **not** a blanket prefix rule in either package — it is a **collision rule**, and both packages already
follow it:

| Package | Prefixed | Unprefixed sibling it disambiguates from | Discriminating? |
|---|---|---|---|
| `adapter/database/sql` | `WithGroupLeaseTTL` | `WithLeaseTTL` (`options.go:115`) | no — consistent with **both** hypotheses |
| `adapter/database/sql` | `WithGroupLockedBy` | `WithLockedBy` (`options.go:129`) | no — same |
| `adapter/memory` | `WithGroupClock` | `WithClock` (`queuestore.go:94`) | no — same |
| **`adapter/memory`** | ***(none)* — `WithMaxGroups` takes no prefix** | **nothing collides** | **YES — the only row that distinguishes a collision rule from a blanket prefix** |

> 🔴 **REVISION 3 DELETES A ROW AND DOWNGRADES THE CLAIM** (audit **N-10**). Revision 2's table carried a fifth
> row — `adapter/database/sql` | `WithInboxTable` | *"the `Option` family's table handling"*. **There is no
> `WithTable`:** `grep -rn "^func WithInboxTable\|^func WithTable" adapter/database/sql/*.go` finds only
> `inbox_dedup.go:35`. `WithInboxTable` collides with nothing, so it is a prefix taken where no ambiguity exists —
> **evidence against the collision rule, presented as evidence for it.**
>
> **And `sql` cannot discriminate the rule at all.** Its `GroupStore` option surface is exactly two names
> (`WithGroupLeaseTTL`, `WithGroupLockedBy`), both of which happen to collide; two data points consistent with both
> hypotheses select neither. **The rule is PROVEN in `adapter/memory`** — `WithMaxGroups` unprefixed and
> uncolliding beside `WithGroupClock` prefixed and colliding — **and merely CONSISTENT WITH `sql`.** That is the
> honest strength of the argument, and it is still sufficient: `MaxGroupMembers` collides with nothing in either
> package, so no rule under consideration forbids the name.

`MaxGroupMembers` already contains "Group", collides with nothing in either package, and reads correctly under both
packages' actual rule. **So one name is not a convention break.** Two names for one SPI concept would also force
`MessageGroupStore.Add`'s contract paragraph (D-AH) to name both, which is precisely the readability cost the
prefix rule exists to avoid.

**REVERSIBILITY of the name:** one identifier per package plus its godoc and its class-gate key. Cheap, but it is a
public identifier, so decide it here rather than at the keyboard.

```
memory.NewGroupStore(memory.WithMaxGroupMembers(0))
  → "msgin: capacity out of range: memory.WithMaxGroupMembers: 0 not in [1, 1048576]"
```

**Why 65,536 is not a new judgement.** ADR 0032 **D-Z** fixed `completionSizeCeiling = 1 << 16` on the ground that
65,536 group members is *"far beyond any plausible aggregation"*, sized by **time, not bytes**, and that row
survived attack in **all five** of Plan 029's audit rounds. The unit here — members of one correlation group — is
**identical**. Reusing a ratified reference value for the identical quantity is the opposite of guessing.

**Why `1 << 20` for the ceiling.** It matches `maxGroupsCeiling` (`adapter/memory/groupstore.go:62`), the sibling
bound in the same struct, so `WithMaxGroups` and `WithMaxGroupMembers` read as one number for "the largest
in-flight aggregation quantity this library accepts." It is 16× the default, giving a real escape hatch to a caller
with a genuinely enormous aggregation.

**Why no new sentinel.** ADR 0032 **D-X**'s reuse rule, applied unchanged: one `errors.Is` target for *"this size
is wrong,"* with the site name in the wrap doing the disambiguation. **This makes `msgin.ErrInvalidCapacity` a
SIXTH producer**, where Spec 016 §3.5 counted four and warned that a fifth *"should be a conscious decision rather
than a default."* It is conscious: the alternative adds two exported sentinels to a pre-v1 surface we are keeping
small and splits a caller's size-validation branch four ways. **A seventh producer needs its own ADR.**

`adapter/database/sql` has no `checkRange` today and gains a **fifth, unexported, identical `int` copy** — ADR 0031
**D-R** / Spec 014 §3.3's four-independent-copies precedent, extended by one rather than converted into a shared
internal package. The four shipped `int` copies are in `endpoint`, `routing`, `adapter/memory` and `adapter/http`:

```
$ grep -rn 'func checkRange(' --include='*.go' . | wc -l          # → 4, the int copies
endpoint/helpers.go:97   routing/helpers.go:88   adapter/memory/helpers.go:54   adapter/http/helpers.go:73
```

> 🔴 **QUALIFY THE GREP, OR THE COMMAND AND THE NUMBER DISAGREE — audit R4-5.** The **unqualified**
> `grep -rn 'func checkRange'` that revisions 1-4 used now returns **five**, because
> [Plan 032](../plans/032-byte-cap-ceilings.md) added an `int64` twin — **`checkRangeInt64` at
> `adapter/http/helpers.go:115`** — which matches on a prefix, and moved `adapter/http`'s `checkRange` from `:64`
> to **`:73`**. Both the number and the coordinate were stale together, in Spec 017 §3.2 and Plan 031 Task 5
> Step 4. **`checkRangeInt64` is a different helper and is not this increment's business:** the fifth copy here is
> an **`int`** copy modelled on `adapter/memory/helpers.go:54`, which is unchanged. *(Recorded in this ADR, not
> only in the spec and the plan, because the four-copies-not-a-shared-package shape is D-AD's decision and the
> inventory is its evidence — the project's "fold into all three artifacts" rule.)*

**REVERSIBILITY:** the default value is one constant per store (Spec 017 §8 item 3). The sentinel choice is one
`checkRange` argument per call site.

### D-AE — exceeding the cap returns `msgin.ErrOverflowDropped`, wrapped, and classified BY CAUSE (revision 2)

**Decision.** An `Add` that would take a group past its cap returns an error wrapping `msgin.ErrOverflowDropped` —
the same sentinel as the group-count arm a few lines above it — in the shape:

```go
fmt.Errorf("%w: %s: group %q holds %d members, limit %d",
    msgin.ErrOverflowDropped, "memory.GroupStore.Add", key, len(g.msgs), s.maxGroupMembers)
```

**Fail loud — never silently drop, never evict, never force-release.** Whether that error is **transient** or
**permanent** is decided by **D-AM**, which is the substance of what changed in revision 2.

**Why the same sentinel.** Symmetry with the group-count arm is worth more than a new `errors.Is` target: a caller
handling *"the store rejected this message because a cap was hit"* handles both with one branch, and both arms are
the same phenomenon (an admission check in `Add`) at two granularities.

**Why WRAPPED, where the existing arm is bare.** `groupstore.go:124` reads `return nil, msgin.ErrOverflowDropped`
with no context. Debuggability is [CLAUDE.md](../../CLAUDE.md)'s stated **core** quality criterion, and
decisively — `msgin.ErrOverflowDropped` already has **four** producer sites (`queuestore.go:171`, `:176`,
`groupstore.go:124` — all returns — plus `endpoint/consumer.go:576`, an `OnRetry` hook argument), so a bare
sentinel cannot tell an operator which cap fired. **The existing bare arm is upgraded to the same shape in the same
commit** — fix the class, not the instance. This is a message-text change only; `errors.Is` is unaffected and no
test asserts the string (the plan verifies this before the edit).

#### D-AE's `%s` — the SITE is four different strings, and one of them is not a store (NEW in revision 4)

**The format is fixed; the substitution was not, and revision 3 specified `sql`'s wrong** (audit **NEW-5**).

| Minted in | `site` |
|---|---|
| `adapter/memory/groupstore.go`, both overflow arms | `memory.GroupStore.Add` |
| `postgres/groupdialect.go` | **`msgin/sql/postgres: AddMember`** |
| `mysql/groupdialect.go` | **`msgin/sql/mysql: AddMember`** |
| `sqlite/groupdialect.go` | **`msgin/sql/sqlite: AddMember`** |

**Decision, and three reasons.** Spec 017 §6 AC-2c pinned the `sql` render as `sql.GroupStore.Add`. **(1)** The
error is minted **inside the dialect** (D-AG), which cannot know it was reached through the store — and `AddMember`
has a second caller the bundle deliberately exercises: **AC-4b drives `kit.Group.AddMember(ctx, tx, …)` directly**
(D-AP, audit N-2), where that render names a store never involved. **(2)** It defeats D-AE's own argument: three
dialects rendering one identical site cannot tell an operator **which engine** rejected, which is precisely the
*"a bare sentinel cannot tell an operator which cap fired"* problem the wrap exists to solve. **(3)** It
contradicts the shipped convention — every error these three files mint is prefixed `msgin/sql/<engine>:`
(`postgres/groupdialect.go:67`, `mysql:63`, `sqlite:55`).

**The render assertion is EXECUTED in Spec 017 §6 AC-4 item 6, against a real engine.** Revision 3 pinned the
string in an AC that **no task ran**: outside the `harness`, the only `sql` overflow cases drive the
`fakeGroupDialect`, where a render assertion is vacuous because the fake mints whatever the test hands it.

#### D-AE's `%d` — the two stores render DIFFERENT COUNTS, deliberately (NEW in revision 4)

> 🔴 **This disposition was taken in revision 3 and recorded in the SPEC and the PLAN and NOT HERE** (audit
> **NEW-8**): `grep -n "65537\|holds 5 members\|members retained at the moment" docs/adrs/0033-*.md` returned
> nothing, against 5 hits in the spec and 2 in the plan. **Round 2's N-8 asked for a decision — *"decide it here,
> not at the keyboard"* — and decisions live in the ADR.** Two-of-three, the project's named failure mode, on the
> round's own finding list.

**Decision.** `%d` is **"members retained at the moment of the check"**, not a single arithmetic value. The two
checks sit on **opposite sides of the write**, so at a cap of 4 `memory` renders `holds 4 members, limit 4` and the
dialects render `holds 5 members, limit 4`:

| Store | Check position | Why it must be there |
|---|---|---|
| `memory` | **before** the append | D-AO — a rejected member must leave no trace in `g.ids` |
| `sql` dialects | **after** the member upsert | D-AP — so an idempotent re-add of an existing id at exactly the cap stays a **no-op** rather than becoming an overflow |

**Rejected: normalising `sql` to `len(members)-1`** so a plan sentence reading *"identical"* would be true. It
would render a count **no statement in that transaction ever observed**, which is the opposite of the
debuggability this wrap exists for. Spec 017 §6 **AC-2c pins both renders** so the divergence is asserted rather
than discovered. *(Since D-AF's revision-4 reversal the two counts are over the **same set** — live + claimed — so
the side of the write is the only remaining difference.)*

> 🔴 **SUPERSEDED WITHIN THIS ADR — revision 1's "NOT `Permanent`" clause is WRONG and is replaced by D-AM.**
> Revision 1 argued: *"an over-cap `Add` **can** succeed later, once the group releases … or once the reaper expires
> it. `Permanent` would deny a retry that works. For a group whose release genuinely never fires, the retry budget
> exhausts and the message dead-letters."* Audit **B-1** falsified the second sentence outright and showed the first
> to be conditional on configuration this ADR elsewhere insists is opt-in. The reasoning is preserved here, struck,
> so a later reader can see what was believed and why it failed — see D-AM.

**REVERSIBILITY:** the wrap is one `fmt.Errorf` per site; the classification is D-AM's.

### D-AM — the overflow error is classified by WHY the group is full: not-leased ⇒ PERMANENT, leased ⇒ transient

**NEW in revision 2. This is the disposition of audit BLOCKER B-1 and it is the most consequential change in this
revision.**

**The defect being fixed.** `msgin.RetryPolicy`'s zero value — the shipped default — is
`{MaxAttempts: 0, Backoff: nil, DeadLetter: nil}`. Trace a **transient** error through the consumer's settlement
switch:

```go
endpoint/consumer.go:860-869   (revision 2 cited :861-869; the block starts at :860 — audit N-13)
	n := c.attempts(d)
	switch {
	case c.policy.MaxAttempts > 0 && n >= c.policy.MaxAttempts && !c.native.NativeDeadLetter():
		if c.divert(settleCtx, c.policy.DeadLetter, d, c.hooks.OnDeadLetter, err, n) { … }
	default:
		c.safeFire(c.hooks.OnRetry, settleCtx, d.Msg, err)
		c.finish(c.safeNack(settleCtx, d, true, retryDelay(c.policy, n)))
	}
```

`MaxAttempts == 0` makes the dead-letter guard false, so **every** attempt takes `default`. `OnRetry` is nil, so
`safeFire` is a no-op. `retryDelay` returns **0** when `Backoff` is nil (`endpoint/consumer.go:1323-1328`). The
result is an **infinite, zero-delay hot spin with no log line and no dead-letter** — the runtime documents this exact
shape for a different arm at `endpoint/consumer.go:96`. For `sql` each iteration is a full rolled-back `AddMember`
transaction **plus** a `SchemaExists` probe (D-AP), forever: **strictly worse than the unbounded status quo.**

**The permanent arm, by contrast, is correct on the zero value** — and that is what makes this fixable:

```go
endpoint/consumer.go:843-857
	if msgin.IsPermanent(err) {
		// … Settled TERMINALLY: one attempt at the sink, never a Nack (D-P).
		// Note (M8): the attempt tracker is deliberately NOT consulted here.
		sink, fellBack := c.invalidTarget(err)
		if fellBack { c.warnInvalidFallback(id) }
		if c.divertTerminal(settleCtx, sink, d, c.hooks.OnInvalidMessage, err) { c.tracker.evict(id) }
		return err
	}
```

It **never consults `MaxAttempts`** and is terminal by construction, so it cannot spin.

> 🔴 **WHICH WARN FIRES DEPENDS ON THE SINKS, and revision 2 named the wrong one for the case it argues about**
> (audit **N-11**). `invalidTarget` returns `fellBack = (c.policy.DeadLetter != nil)`
> (`endpoint/consumer.go:942`), so on the **zero-value `RetryPolicy`** `fellBack` is **false** and
> `warnInvalidFallback` **never fires**. The real signals:
>
> | Configuration | Signal | Settlement | Site |
> |---|---|---|---|
> | invalid-message sink set | sent there | sink's | `:914`+ |
> | no invalid sink, `DeadLetter` set | fallback WARN — **once per CONSUMER**, `sync.Once`-deduped, **not per message** | dead-letter divert | `:942`, `:968-973` |
> | **neither sink (the shipped default)** | WARN naming **both** missing options | **`safeAck`** — the source **drops** the message | `:1049`, `:1073` |

**The ceiling on this claim, stated rather than oversold.** With **neither** sink configured the outcome is a
**logged, terminal, one-WARN-per-message discard that the source ACKS** — not a durable capture, and not a
redelivery: an at-least-once source will not hand the message back. That is still strictly better than an unlogged
infinite spin, and it is the honest limit of what a classification can buy — **a sink is what turns the loss into a
capture, and the library cannot supply one.** Both options' godoc says so, **including the Ack**.

**Decision.** The store distinguishes the two rejection causes — it can, because it holds the lease flag
(`adapter/memory/groupstore.go:43`, `leased bool`):

| Cause | Store's classification | Why |
|---|---|---|
| group at cap and **NOT leased** | **`msgin.Permanent(…ErrOverflowDropped…)`** | nothing will drain it on its own; the release will not re-fire without an admission this cap forbids. Terminal, diverted to a sink, and **works on the shipped zero-value `RetryPolicy`.** **In that shipped configuration the loud signal is `divertTerminal`'s nil-sink WARN (`consumer.go:1049`) followed by `safeAck` (`:1073`) — a WARN and an ACK, so the source drops the message; `warnInvalidFallback` fires only when a dead-letter sink IS configured, and then ONCE PER CONSUMER (`:968-973`).** *This is what kills the retry storm.* |
| group at cap and **LEASED** | plain transient `ErrOverflowDropped` | a claim is in flight; `SettleGroup`/`AbandonGroup` runs on **every** release path including a panic-safe defer, so the window is bounded and a retry genuinely succeeds after it. |

> 🔴 **THE FIRST ROW'S SIGNAL WAS STILL WRONG IN REVISION 3 — twenty lines below the box that corrects it** (audit
> **NEW-10**). N-11's fix landed as the correction box above and left this cell, D-AK's *"strict improvement"*
> paragraph, and two cells in Spec 017 (§3.3.1's contract table and §3.9's D-AJ box) all reading *"WARN on the
> dead-letter fallback"* — the WARN N-11 **proved never fires** on `RetryPolicy{}`. **A correction box that does
> not reach the table it corrects has fixed the prose and left the contract.** All four are edited in revision 4.

**The leased/not-leased split applies to BOTH stores, and since revision 4 it is literally one predicate.**
`memory` reads `g.leased` (`adapter/memory/groupstore.go:43`). **`sql` reads the group row's `locked_by`** —
NULL ⇒ not leased ⇒ permanent, non-NULL ⇒ leased ⇒ transient — obtained at **zero extra round-trips** by extending
the statement each dialect already runs to read `created_at` (D-AG). `locked_by IS NULL` is *exactly* the premise
restated below: **nothing drains an UNLEASED group without an expiry cutoff.** One rule, two stores.

> 🔴 **REVISION 3 SAID THE SPLIT *COLLAPSED* FOR `sql`, AND D-AF's REVERSAL MAKES THAT FALSE** (audit **NEW-7**).
> It read: *"For `sql` the cap counts **live** members only (D-AF), and a live member set is by definition
> unclaimed, so … **every** `sql` over-cap rejection is the not-leased case and is classified permanent."* **D-AF
> now counts live + claimed**, so a `sql` group can be over cap *because* a claim is in flight — the **leased**
> arm. Keeping the unconditional-permanent rule would **dead-letter healthy traffic in a routine claim window**,
> which is the exact outcome the leased arm exists to prevent in `memory`. The simplification was bought by the
> defect NEW-7 found; it does not survive the fix.

### D-AM's premise, RESTATED in revision 3 — the revision-2 form was `memory`-only

> 🔴 **This is audit N-3, and it is M-3's defect recurring in the reaper.** Revision 2 justified the classification
> from *"in the default configuration neither escape exists"*, resting on *"with no `WithGroupTimeout` the reaper
> never sweeps."* **That is a fact about `memory` only:**
>
> ```
> adapter/memory/groupstore.go:220        func (s *GroupStore) RecoverInterval() time.Duration { return 0 }
> adapter/database/sql/groupstore.go:349  func (s *GroupStore) RecoverInterval() time.Duration { return s.leaseTTL }
> ```
>
> `reapInterval` takes the **min positive** of `cfg.timeout` and the store's interval
> (`routing/aggregator.go:558-565`), so a `sql` store with **no `WithGroupTimeout` at all** gives a **5m** ticker,
> and `Aggregator.Run`'s godoc calls `Run` **REQUIRED** for a durable store (`aggregator.go:530-532`).
>
> **The conclusion survives, by a mechanism revision 2 never wrote down:** with `cfg.timeout == 0` the cutoff is
> zero, and `ExpiredGroups` gates its age arm on `beforeSet := !before.IsZero()`
> (`postgres/groupdialect.go:275-282`), so the default sweep returns **crashed-lease groups only**.
>
> **THE PREMISE, in the form that is true of both stores:**
>
> > **Nothing drains an UNLEASED group without an expiry cutoff.**
>
> That is what the classification needs, and it is what D-AM now asserts.

**THE COUNTER-EXAMPLE THIS OBLIGES D-AM TO OWN — the trade is wider than revision 2 stated.** A `sql` group that is
at cap **and** holds a **stranded lease** (a releaser crashed mid-release) matches `ExpiredGroups`' **first**
`WHERE` arm — `locked_by IS NOT NULL AND locked_at <= now - leaseTTL` — **regardless of cutoff**. So the default 5m
sweep surfaces it, `reapGroup` claims it, and the recovery path drains it if its predicate fires. Meanwhile every
member arriving for that key hits the live residual at cap and is classified **permanent** and terminated.
**D-AM therefore dead-letters members that the DEFAULT `sql` configuration would have admitted one tick later**, not
only members a `WithGroupTimeout` caller would have. The recommendation does not change — a 5m wait is not a reason
to prefer an unlogged infinite spin — but Spec 017 §8 item 5 now states the true width of the trade.

`msgin.Permanent` wraps transparently — `permanentError.Unwrap` (`reliability.go:14`) — so
`errors.Is(err, msgin.ErrOverflowDropped)` still holds and the caller's existing branch is undisturbed. The rendered
string gains the shipped `"msgin: permanent: "` prefix; that is stated in the acceptance criteria so the render
assertion is written against the real text.

**THE TRADE-OFF, STATED HONESTLY — this is a judgement call, not an obvious one.** With `WithGroupTimeout`
configured, the reaper *would* eventually expire a stuck group, so a permanent classification **dead-letters a
message that might have succeeded later**. That is a real cost and it is paid deliberately:

- **A hot spin is a production-down event; a dead-lettered message is recoverable.** One burns a CPU core and a
  database connection indefinitely with no log; the other lands in the operator's sink with a typed cause naming the
  group and the limit.
- **[CLAUDE.md](../../CLAUDE.md)'s Sensible-defaults gate says to default to the safe, conservative value** — and to
  pick the value that fails safe when a wrong default could cause unbounded growth or a DoS lever. An unlogged
  infinite spin against a database is exactly that lever.
- **The classification is only the default, and D-AN overturns it on evidence.** When the group *is* drainable, the
  Aggregator downgrades it back to transient (see D-AN), so the permanent verdict is reserved for groups that have
  been shown not to drain.
- **The counter-argument, kept rather than dismissed:** a caller who *has* set `WithGroupTimeout` gets a strictly
  worse outcome than transient would have given them — their message dead-letters where waiting one reaper tick
  would have worked. The remedy available to them is `WithMaxGroupMembers` sized above their aggregation, or an
  invalid-message sink they drain back. **If the round-2 audit or the user prefers the other side of this trade,
  the change is one branch in each store.**

**REVERSIBILITY:** one `if !leased` branch per store, plus the AC that asserts `IsPermanent`. Cheap to reverse,
expensive to get wrong — reverse it only with the hot-spin analysis above answered, not merely disagreed with.

### D-AN — an over-cap `Add` returns the LIVE SNAPSHOT alongside the error; the Aggregator re-evaluates the release

**NEW in revision 2. This is the disposition of audit MAJOR M-6, which found that the cap check as specified in
revision 1 introduces a permanent deadlock the current code does not have.**

**The defect being fixed.** With an **id-less** message (`msg.ID() == ""` — a supported shape, explicitly branched
on at `adapter/memory/groupstore.go:129`) against a group at `cap-1`:

| Step | What happens | Site |
|---|---|---|
| 1 | cap check passes, append → `len(g.msgs) == cap` | `groupstore.go:135` |
| 2 | `Handle`: the release predicate **fires** on `cap` members | `routing/aggregator.go:426` |
| 3 | `ClaimGroup` → claim; `releaseOnce` **fails** (agg error, or the output `Send` fails) | `aggregator.go:451-467` |
| 4 | the deferred abandon runs: `AbandonGroup` clears the lease and `claimedLen` — **and does NOT shrink `g.msgs`** | `groupstore.go:196-197` |
| 5 | Nack → redelivery. `Add` again: id is `""`, so **the dedup branch is skipped entirely** | `groupstore.go:129` |
| 6 | cap check: `len(g.msgs) == cap >= cap` → **REJECT, before the release decision is ever reached** | the new arm |

The group now holds a **complete, releasable set that nothing will ever re-trigger.** `AbandonGroup`'s own godoc
states the invariant the new arm breaks — *"the claimed members return to live … **so a retry / next member / next
reaper tick re-releases**"* (godoc `groupstore.go:185-188`, func `:189-199`, assignments `:196-197`; revision 2
cited `:185-197` — audit **N-13**) — and the cap check removes the "retry / next member" half
while the reaper half is off by default. **Before this change, step 6 appended and re-fired the release, which
succeeded.** So revision 1's D-AK claim that liveness is *"unchanged"* was false: this is a **regression**.

**Decision.** On rejecting an over-cap member, `Add` returns **the current live group snapshot together with the
error**, and `Aggregator.Handle` re-evaluates the release predicate against it. **Reject the member, not the
release.**

```go
// routing/aggregator.go — Handle, replacing the bare `if err != nil { return err }` at :412-415
group, err := a.store.Add(ctx, key, msg)
if err != nil {
    if group == nil {
        return err // ordinary Add failure; unchanged behavior for every store that returns (nil, err)
    }
    // D-AN: an over-cap rejection that still carries the live snapshot. The MEMBER
    // is rejected; the RELEASE opportunity is not. Give the group a chance to drain,
    // then report the rejection so the source never Acks an unstored member.
    ok, rerr := a.cfg.release(group)
    if rerr != nil || !ok {
        return err // nothing to drain — the store's classification (D-AM) stands
    }
    claim, cerr := a.store.ClaimGroup(ctx, key)
    if cerr != nil {
        return cerr
    }
    if claim == nil {
        return a.overflowRetryable(key, err) // another holder is releasing it → transient
    }
    if relErr := a.release(ctx, claim); relErr != nil {
        return relErr
    }
    return a.overflowRetryable(key, err) // drained → the retry WILL be admitted → transient
}
```

`overflowRetryable` mints a fresh **transient** error rather than unwrapping D-AM's permanent marker:

```go
fmt.Errorf("%w: routing.Aggregator.Handle: group %q drained by this release; retry to admit the rejected member",
    msgin.ErrOverflowDropped, key)
```

**Why an error is still returned when the drain succeeds.** The member was never stored. Returning `nil` would make
the source **Ack a message that was never aggregated** — the delivery-guarantee violation Spec 017 §5 rejects under
*"Drop the over-cap member silently."* Transient is correct here and is not a re-litigation of D-AM: the group
provably just shrank, so the retry provably succeeds.

**Why the classification direction is safe — scoped, in revision 4, to the exits it is actually true of.** For
exits **4** and **6** the store's default is the **conservative** one (permanent, no spin) and only **positive
evidence of drainability** downgrades it, so a bug in the *drain-detection* logic costs a dead-letter rather than a
production-down spin.

> 🔴 **REVISION 3'S UNQUALIFIED SENTENCE IS DELETED** (audit **NEW-6**). It read: *"A bug in the drain path costs
> a dead-letter, not a production-down spin."* **Exits 3 and 5 are a drain path that failed, and they return an
> unmarked — transient — fault**, which under `RetryPolicy{}` is a zero-delay Nack loop. The sentence asserted the
> exact property those two exits do not have, and revision 3 promoted it into the SPI contract (D-AH) where a
> third-party store author would rely on it.

**The rule, in the form that survives:** the Aggregator **never upgrades** a transient rejection to permanent, on
any exit; it **downgrades on positive evidence of drainage** (exits 4 and 6) **or replaces the overflow error with
a distinct fault carrying that fault's own classification** (exits 3 and 5). **A persistently failing
claim/release path therefore RETRIES rather than terminating** — accepted, because marking a store fault or a
channel fault `Permanent` merely for having been reached through an overflow would dead-letter on the wrong cause
and misattribute it in the operator's sink. Recorded as a residual hazard in Spec 017 §3.3a.1 and §8 item 6.
D-AH's MAY clause carries this wording, which is where a third-party store author reads it.

**THE BRANCH HAS SIX EXITS, NOT FOUR** (audit **N-7**). Revision 2's coverage tables (Plan 031 B1-11…B1-14, Spec §6
AC-9 rows 12-13) named four; the pseudocode above has six early returns, and CLAUDE.md's test-coverage gate makes
each one a delivery blocker. The two that matter most:

- **`claim == nil`** returns a **retryable** error here, where the success path returns **`nil`** for the identical
  condition (`routing/aggregator.go:438-439`, *"another Handle/process is releasing this group; held"*). The
  divergence is **deliberate** — the member was never stored, so `nil` would Ack an unstored message — and revision
  2 neither tested nor documented it. It is now in `Handle`'s godoc (Spec §4 item 7) and in AC-9 row 12c.
- **`rerr != nil`** must be kept distinct from `!ok`: a mutant dropping that half of the `||` **claim-and-releases a
  group the strategy rejected**. A strategy's *error* is not a "no".

Spec 017 §3.3a.1 carries the full six-row table and AC-9 rows 12a-12d the killing mutants.

**Scope, measured.** The deadlock is `memory`-only and id-less-only — with a non-empty id the dedup branch returns
the snapshot with a **nil** error and `Handle` reaches the release predicate anyway, and `sql.GroupStore.Add`
rejects an empty msg id before any query runs (`adapter/database/sql/groupstore.go:252-254`). **The snapshot return
is nonetheless implemented in `sql` too**, because without it a `sql` caller gets a false-permanent dead-letter in a
case a `memory` caller does not, and an asymmetry with no principle behind it is how the next audit round starts.
The `sql` cost is bounded: the dialect's live-member `SELECT` already runs and emits a `LIMIT maxMembers+1` **via
D-AS's private `limit` parameter — not by editing the shared helper's SQL, which has three callers** — and the
rejected member is filtered out of the in-memory `[]MemberRow` before the rows are returned with the error — **no
extra query, and the fetch stays bounded** (D-AP, D-AS).

**The SPI change is additive and backward-compatible.** `MessageGroupStore.Add`'s contract gains a **MAY**: an
implementation *may* return the live snapshot alongside a non-nil error, and the Aggregator will use it. A store
that returns `(nil, err)` — every store written against revision 1's contract, and every third-party store — keeps
working through `Handle`'s `group == nil` arm. The existing `(nil, nil)` guard at `aggregator.go:416-424` is
untouched.

**REVERSIBILITY:** the store side is one return value; the `Handle` side is one branch. Reversing it restores the
deadlock, so reverse it only by fixing M-6 another way.

### D-AO — the memory cap check sits BETWEEN the dedup lookup and the id insert, not "after the dedup branch"

**NEW in revision 2. Found while dispositioning M-6; a silent-data-loss defect in revision 1's placement
instruction.**

**The defect.** Revision 1 (Spec 017 §3.5, Plan 031's "counted set" box) said the check must sit *"BEFORE the append
and AFTER the dedup branch."* The dedup branch **ends with the id insert**:

```go
adapter/memory/groupstore.go:129-135  (as shipped)
	if id := msg.ID(); id != "" {
		if _, seen := g.ids[id]; seen {
			return snapshot{…}, nil          // :131 — idempotent no-op
		}
		g.ids[id] = struct{}{}               // :133 — the member is now RECORDED
	}
	g.msgs = append(g.msgs, msg)             // :135
```

A check placed after `:133` records the member's id as *seen* and then rejects it. On redelivery the dedup branch
returns the snapshot with a **nil** error, `Handle` returns nil, and **the source Acks a message that was never
appended.** Silent data loss — exactly what Spec 017 §5 rejects.

**Decision.** The check sits between the `seen` lookup and the `g.ids` insert, with the id hoisted so the check also
runs on the id-less path:

```go
id := msg.ID()
if id != "" {
	if _, seen := g.ids[id]; seen {
		return snapshot{…}, nil                     // unchanged: idempotent no-op, never an overflow
	}
}
if len(g.msgs) >= s.maxGroupMembers {               // ← the cap check: after the lookup, before ANY mutation
	live := snapshot{…}                             // D-AN: the live snapshot travels with the error
	err := fmt.Errorf("%w: memory.GroupStore.Add: group %q holds %d members, limit %d",
		msgin.ErrOverflowDropped, key, len(g.msgs), s.maxGroupMembers)
	if !g.leased {
		return live, msgin.Permanent(err)           // D-AM: structurally stuck
	}
	return live, err                                 // D-AM: the claim window will drain it
}
if id != "" {
	g.ids[id] = struct{}{}
}
g.msgs = append(g.msgs, msg)
```

**Three invariants this ordering buys, each with its own killing mutant** (Spec 017 §6 AC-9):

1. **An idempotent re-add at exactly the cap is a no-op, never an overflow.** Mutant: move the cap check above the
   `seen` lookup.
2. **A rejected member leaves NO trace in `g.ids`.** Mutant: move the cap check below the `g.ids` insert ⇒ the
   redelivery is silently swallowed and the AC's "the member is still admitted after the group drains" case fails.
3. **The check runs on the id-less path too.** Mutant: fold the check back inside `if id != ""` ⇒ M-6's deadlock
   case regains an unbounded append.

**REVERSIBILITY:** none worth having — the alternative placements are the two defects above.

### D-AF — BOTH stores count live + claimed (🔴 REVERSED in revision 4; audit NEW-7)

**Decision.** Both first-party stores bound **every member they still retain for one key — live plus claimed**.
`memory.GroupStore` bounds `len(g.msgs)`; `sql.GroupStore` bounds `SELECT count(*) … WHERE group_key = ?` with
**no `claimed_epoch` predicate**.

**The stated principle, unchanged and now applied consistently:** *a store bounds what it retains.* Revision 3
applied it to two different readings of "retains" and got two rules; revision 4 applies it to the one reading that
makes the bound a bound.

> 🔴 **WHAT REVISION 3 SAID, AND WHY IT FAILED — this is the only genuine DESIGN flaw audit round 3 found.**
> Revision 3: *"`sql.GroupStore` bounds the **live** member set (`claimed_epoch IS NULL`), because for `sql` the
> claimed members are retained by the **database**, not by the process."*
>
> **1. The bound did not bound the durable table.** `ClaimGroup` stamps the new epoch on **every** live member —
> `UPDATE <members> SET claimed_epoch = $2 WHERE group_key = $1 AND (claimed_epoch IS NULL OR claimed_epoch < $2)`
> — so the live count drops to **0** and up to `cap` more members are admitted. A failed release runs
> `AbandonGroup`, which returns them all to live (`SET claimed_epoch = NULL`), and the next claim stamps the larger
> set. **`SettleGroup` is the only statement in any dialect that ever DELETES a member row, and it runs on success
> only.** Each failed-release cycle therefore adds up to `cap` durable rows, **without limit** — while every
> acceptance criterion in the bundle still passed, because the `cap+1`-th *live* member really was rejected.
> That is the false-safety inversion D-AG rejects enforcement (A) for, reached through the counted set instead of
> the enforcement point.
>
> **2. `memory` never had the hole, precisely BECAUSE it counts live + claimed.** `ClaimGroup` sets
> `g.claimedLen = len(g.msgs)` (`groupstore.go:151`) **without shrinking `g.msgs`**, so claimed members keep
> counting against the cap. The "Cost" paragraph below describes that as `memory`'s *price*; it is in fact the
> property that makes its bound real, and it is now both stores'.
>
> **3. The justification was false on its own terms.** `sql.GroupStore.ClaimGroup` decodes the **entire** claimed
> set into the process heap, at `limit = 0` by D-AS's own rule (`adapter/database/sql/groupstore.go:285-298`,
> `s.decodeGroupRows(cg.GroupRows)`). The `sql` process **does** retain the claimed set.
>
> **Rejected alternative — keep live-only and bound the claimed set instead** (e.g. `ClaimGroup` at `LIMIT cap`).
> That re-introduces exactly the truncation **D-AS** exists to forbid: a legitimately at-cap group releases an
> **incomplete aggregate**, the silent data corruption Spec 017 §5 rejects. **Do not fix an unbounded table by
> bounding `ClaimGroup`.**

**Two consequences, both load-bearing and both stated rather than absorbed.**

1. **The count source is FORCED to be a `COUNT(*)`, and it costs one round-trip per `AddMember`.** `len()` of the
   live-member `SELECT` — even at D-AS's `LIMIT maxMembers+1` — cannot see claimed members at all. The three
   dialects already ship an identical `*CountMembers(ctx, q, mt, groupKey) (int64, error)`
   (`postgres/groupdialect.go:373`, `mysql:358`, `sqlite:375`), already called from their own `SettleGroup`, so
   this is **zero new SQL and no new helper** — but it is **one extra statement on every add**, not only on
   overflow, and the scan is `O(members)` (up to `maxGroupMembers`, default 65,536). Recorded in the Consequences
   and in Spec 017 §8's backlog. 🔴 **The cost is PER-ENGINE, not neutral** (audit **R4-8**): postgres and mysql
   pay it inside a **per-key** group-row lock, while **sqlite pays it inside `BEGIN IMMEDIATE`'s DATABASE-WIDE
   write lock** (`sqlite/groupdialect.go:62`), so on sqlite it lengthens a **global** write-serialisation window
   on every add to *any* group. Spec 017 §3.6 states it per engine. *(No design change: (C) is still right for
   sqlite, and §7.1 records that same lock as a correctness **advantage** — this is the identical property seen
   on the throughput axis, and revisions 1-4 recorded only the benefit.)*
2. **`sql` gains D-AM's leased arm**, discriminated on the group row's `locked_by` at zero extra round-trips
   (D-AM, D-AG) — verified per dialect in audit round 4: postgres already `RETURNING`s from the group row inside
   `AddMember`, mysql and sqlite already `SELECT` it, so all three take a second column rather than a statement.
   Its residual hazard is a **crashed** releaser holding the lease, during which the rejection is transient. It is
   bounded and self-healing (the default sweep drains a crashed-lease group, D-AM's premise box) and is recorded
   as **Spec 017 §8 item 9** for the user.

   > 🔴 **REVISION 4 PUT THE WIDTH OF THAT HAZARD AT `leaseTTL`. IT IS UP TO `2 × leaseTTL`** (audit **R4-3**).
   > **Two independent terms compose, and revision 4 counted one:**
   >
   > 1. **Eligibility — `t₀ + leaseTTL`.** `ExpiredGroups`' first `WHERE` arm is
   >    `locked_by IS NOT NULL AND locked_at <= now - leaseTTL`; `defaultGroupLeaseTTL = 5 * time.Minute`
   >    (`adapter/database/sql/groupstore.go:22`).
   > 2. 🔴 **Discovery — the FIRST REAPER TICK AT OR AFTER eligibility, on a cadence whose phase is unrelated to
   >    `t₀`.** `Aggregator.Run` builds `clock.NewTicker(reapInterval())` (`routing/aggregator.go:544`) at
   >    **`Run`'s own start**, and with no `WithGroupTimeout` `reapInterval() = store.RecoverInterval() =
   >    leaseTTL` (`aggregator.go:559-565`; `groupstore.go:349`). Ticks land at `T₀ + k·leaseTTL`.
   >
   > **Worst case `t₀ + 2·leaseTTL − ε` ≈ 10 minutes at the shipped defaults** (and only while `go agg.Run(ctx)`
   > is running at all — Spec 017 §1.2.1 makes `Run` **required** for a durable store).
   >
   > 🔴 **And the per-iteration cost is NOT "a zero-delay busy-wait against the database" — D-AM's own rejection
   > argument above already states the real one.** Each retry is a full rolled-back `AddMember` transaction —
   > `BEGIN`; the group upsert **taking the group-row X-lock** (database-wide on sqlite); the member upsert;
   > **consequence 1's new `COUNT(*)`**; the live `SELECT`; `ROLLBACK` — **plus `classifyQueryErr`'s
   > `SchemaExists` probe** (`adapter/database/sql/groupstore.go:91-95`, from `:274`), **× `WithConcurrency(N)`
   > goroutines, all contending on the very group row the recovery path must lock to drain it.**
   >
   > **The disposition is UNCHANGED — transient is correct**, for the reason Spec 017 §8 item 9 gives: the retry
   > genuinely succeeds once the reaper drains the group, and classifying an *expired* lease permanent would
   > dead-letter messages the default configuration is about to admit. **Only the magnitude and the cost were
   > wrong — and the wrong figure had been scheduled into `sql.WithMaxGroupMembers`'s PUBLIC GODOC** (Spec 017 §4
   > item 1). Under CLAUDE.md's Sensible-defaults gate a default's rationale is part of the API contract, so a
   > 2×-optimistic number there is a defect in the contract, not in the prose. **The user is being asked to
   > ratify this trade; a trade presented at half its cost has not been presented.**

> 🔴 **THE CONVERGENCE — this is the strongest argument that adopting symmetry was right, and it is deliberate,
> not incidental.** Revision 3 sold its `sql`-live-only rule as a *simplification*: *"the leased/not-leased split
> collapses; one rule, two stores, no asymmetry to remember."* **That simplification was bought by the defect
> NEW-7 found** — it collapsed only because the counted set could never include a claimed member.
>
> Reversing D-AF removes the collapse and produces a **better** unification in its place. Round 2's **N-3** had
> already forced D-AM's premise to be restated, per store, as:
>
> > **Nothing drains an UNLEASED group without an expiry cutoff.**
>
> That restatement was made for an entirely different reason — the `sql` reaper sweeps by default while `memory`'s
> does not — and it lands on the **same predicate** the counted-set reversal now requires:
> **`locked_by IS NULL` for `sql`, `!g.leased` for `memory`.** So the two stores share **one classification rule
> expressed as one predicate**, and that predicate is *identical to the premise the rule rests on*. Two
> independent findings, two rounds apart, converge on the same discriminator.
>
> **A simplification that survives an unrelated finding is a real one; a simplification that exists because a set
> was under-counted is an artifact of the defect.** D-AF's reversal trades the second for the first.

**The cost, stated rather than discovered later — and since revision 4 it is BOTH stores'.** In `memory`,
`ClaimGroup` sets `g.claimedLen = len(g.msgs)` (`groupstore.go:151`) without shrinking `g.msgs`; the trim happens
in `SettleGroup` (`groupstore.go:173-178`). In `sql`, `ClaimGroup` stamps the epoch and only `SettleGroup` deletes.
So between claim and settle, a group sitting at exactly the cap **rejects new arrivals for that key**, even though
its live residual is empty. That window is bounded (release settles or abandons on every path, including a
panic-safe defer-abandon) and recoverable — it is precisely **D-AM's leased arm**, which stays **transient** so the
retry succeeds after the settle. It is documented on the option's godoc. **This "cost" is what bounds the durable
table; see the box above.**

> **Residual hazard, named in revision 2:** the leased arm's retry is a Nack with `retryDelay == 0` under the
> shipped zero-value `RetryPolicy`, so it is a **busy-wait for the duration of the claim window**, not a sleep. It
> is bounded (the release settles or abandons unconditionally) where B-1's spin was not, and it is the same
> exposure the **existing** `WithMaxGroups` overflow arm already carries. Accepted; named so it is not rediscovered
> as a new defect. A caller who cares configures `RetryPolicy.Backoff`.

> 🔴 **Revision 3 called this asymmetry *"the finding most likely to be reversed by the audit"*, and round 3
> reversed it — toward symmetry, not toward the uniform-live-only alternative it named.** Uniform live-only would
> have made the SPI contract one sentence at the price of letting **neither** store bound anything durable; uniform
> live-**plus**-claimed makes it one sentence and bounds both. Spec 017 §8 item 1 is now **CLOSED**.
>
> **REVERSIBILITY: one predicate per dialect (`count(*)` → `count(*) … WHERE claimed_epoch IS NULL`) plus the
> `locked_by` read and one sentence in the SPI godoc — and reversing it re-opens an unbounded durable member
> table.** That is not a cost trade; it is the increment's purpose. **Do not reverse this to save the `COUNT(*)`.**

### D-AG — the SQL bound is enforced INSIDE the dialect's transaction, and `AddMember` takes `maxMembers`

**Decision.** `GroupDialect.AddMember` gains a trailing `maxMembers int` parameter. Each dialect, **inside the
transaction it already opens and after the statement that serializes same-key adds**, counts **every member row for
the key — live and claimed** (D-AF, reversed in revision 4) and —
if the cap is exceeded — returns the D-AE/D-AM error, letting the dialect's own transaction wrapper roll back.
**Nothing is committed — SUBJECT TO the precondition in D-AP, which revision 1 asserted falsely.** The per-dialect
placement, the three different serialization mechanisms, and the caller-owned-transaction hole are all in **D-AP**;
this decision records only the *choice* of enforcement point.

**Why not the one-line alternative.** Three enforcement points exist:

| | Bounds the durable table? | Bounds the raw fetch? | Bounds the decode? | SPI change | Atomic across instances? |
|---|---|---|---|---|---|
| (A) count in `GroupStore.Add` after `AddMember` returns | **no** | **no** | yes | none | **no** |
| (B) (A) + `LIMIT max+1` on the dialect's SELECT | no | yes | yes | signature | no |
| **(C) count in-transaction, roll back — CHOSEN** | **yes — but only over LIVE + CLAIMED** (D-AF; audit **NEW-7**) | **yes** | **yes** | signature | **yes** |

> 🔴 **(C)'s first column was FALSE in revision 3, and the fix is in the PREDICATE, not the placement** (audit
> **NEW-7**). Counting live members only, `ClaimGroup` zeroes the count and the table grows by up to `cap` per
> failed-release cycle without limit (D-AF's box). The enforcement point was always right.

**How the count is obtained, and what it costs.** Each dialect calls its own shipped `*CountMembers` helper —
`pgCountMembers` (`postgres/groupdialect.go:373`), `mysqlCountMembers` (`mysql:358`), `sqliteCountMembers`
(`sqlite:375`) — `SELECT count(*) … WHERE group_key = ?`, **no `claimed_epoch` predicate**, already called from
each dialect's own `SettleGroup`. **Zero new SQL; one extra statement per `AddMember`**, on every add rather than
only on overflow. It cannot be derived from D-AS's member `SELECT`, which is live-only.

**And one more read rides along at zero cost:** the group row's **`locked_by`**, for D-AM's leased/not-leased
classification. Each dialect already runs a statement that reads `created_at` from that row — postgres'
`… RETURNING created_at`, mysql's and sqlite's `SELECT created_at FROM <group> WHERE group_key = ?` — so
`locked_by` is added to the existing projection. It is **not** added to `msginsql.GroupRows`; it is local to
`AddMember`'s classification, so the SPI's return shape is unchanged.

(A) is a bound that halves the peak and fixes nothing: the member row is already committed and `[]MemberRow`
already carries every live member's framed payload bytes. **A remedy that leaves the actual lever in place while
reading as "bounded" is the false-safety inversion Spec 016 §1.1 and §3.8 both warn about** — and it is the same
mistake as ADR 0032's twice-emptied safety causes (c) and (d), where a verdict outran its evidence.

(C) is additionally the only option that is **atomic across processes**: the check sits after the statement that
serializes same-key adds (per engine — D-AP), so two instances adding concurrently to a group at `cap-1` serialize —
one commits, the other counts `cap`, exceeds, and rolls back. Under (A) each could commit past the cap.

**Why the breaking SPI change is affordable.** `GroupDialect`'s godoc states *"This is a pre-1.0 (v0) contract that
may still evolve"* (`groupdialect.go:106`); the project is **unreleased, untagged, with no consumers**
([CLAUDE.md](../../CLAUDE.md) project status); and `grep -rn 'msginsql.GroupDialect'` finds only first-party
implementers.

**SEVEN sites change, not five (audit m-5).** Revision 1 listed five and omitted the two that matter most — the
interface declaration itself and the production call site that must thread the configured cap:

| # | Site | Kind | What changes |
|---|---|---|---|
| 1 | `adapter/database/sql/groupdialect.go:126` | **interface declaration** | the signature, plus the in-transaction-enforcement contract in its godoc (D-AP) |
| 2 | `adapter/database/sql/groupstore.go:272` | **production call** | threads `s.maxGroupMembers` — the only site that changes *behavior*, not just shape |
| 3 | `adapter/database/sql/postgres/groupdialect.go:80` | implementation | signature + the check |
| 4 | `adapter/database/sql/mysql/groupdialect.go:75` | implementation | signature + the check |
| 5 | `adapter/database/sql/sqlite/groupdialect.go:102` | implementation | signature + the check |
| 6 | `adapter/database/sql/harness/groupstore.go:345` | test-kit call | signature |
| 7 | `adapter/database/sql/groupdialect_fake_test.go:137` | test fake | signature + records `maxMembers` |

**Why the dialects return `msgin.ErrOverflowDropped` directly** rather than a new `msginsql` sentinel or an
exported helper: D-X's *mint none* rule, and an exported `msginsql` helper taking an `int` would itself become a
class-gate key. The three dialect modules already depend on the `msgin` root module transitively through
`msginsql`, so the import is **zero net dependency** — the plan verifies `go mod tidy` leaves each dialect's
`go.mod` unchanged.

**REVERSIBILITY: this is the expensive decision.** It roughly doubles the increment (five extra modules:
`postgres`, `mysql`, `sqlite`, `harness`, plus `dbtest` running them — **six** modules touched in total, counting
root). Falling back to (A) plus a named follow-up is defensible if the increment must be small — **but that choice
must be made before Plan 031 starts, not at task 6.** See Spec 017 §8 item 2.

### D-AP — per-dialect placement, and the caller-owned-transaction precondition

**NEW in revision 2. This is the disposition of audit MAJOR M-2 and MAJOR M-3, plus MINOR m-6.**

**M-2 — "nothing is committed" is FALSE on one of three branches.** `pgRunInTx` and `mysqlRunInTx` are
byte-identical in shape and have **three** branches, only one of which rolls back:

```go
adapter/database/sql/postgres/groupdialect.go:52-68   (mysql/groupdialect.go:48-64 is the same)
func pgRunInTx(ctx context.Context, q msginsql.Querier, fn func(tx msginsql.Querier) error) error {
	if b, ok := q.(txBeginner); ok {          // *sql.DB — the dialect owns the tx
		tx, err := b.BeginTx(ctx, nil)
		…
		if err := fn(tx); err != nil {
			_ = tx.Rollback()                 // ← the ONLY branch revision 1 described
			return err
		}
		return tx.Commit()
	}
	if tx, ok := q.(*stdsql.Tx); ok {         // *sql.Tx — the CALLER owns the tx
		return fn(tx)                         // ← NO rollback. NO commit. "Caller owns commit."
	}
	return fmt.Errorf("msgin/sql/postgres: group ops require a *sql.DB or *sql.Tx Querier, got %T", q)
}
```

**Decision — state the precondition rather than pretend it away.** The in-transaction bound is enforced *by
rollback* **only when the dialect owns the transaction** (a `*sql.DB` Querier). Under a `*sql.Tx` Querier the
over-cap member row is already inserted into the caller's open transaction when the error is returned, and **the
caller owns the rollback**.

> 🔴 **WHO CAN REACH THAT BRANCH — revision 2 named a route the compiler forbids** (audit **N-2**). Revision 2
> called the `*sql.Tx` Querier *"reachable via `WithSharedTransaction`"*. **It is unreachable from
> `sql.GroupStore` entirely:**
>
> ```
> adapter/database/sql/groupstore.go:212   NewGroupStore(db *stdsql.DB, table string, dialect GroupDialect, opts ...GroupStoreOption)
> adapter/database/sql/groupstore.go:40-42 type groupBase struct { db *stdsql.DB; … }
> adapter/database/sql/groupstore.go:272   s.dialect.AddMember(ctx, s.db, s.table, …)
> adapter/database/sql/options.go:201      func WithSharedTransaction(r TransactionResolver) Option   ← Option, NOT GroupStoreOption
> ```
>
> `NewGroupStore` takes a **concrete `*stdsql.DB`**, stores a **concrete `*stdsql.DB`**, always passes `s.db`, and
> its entire option surface is `WithGroupLeaseTTL` (`:140`) and `WithGroupLockedBy` (`:155`) plus a logger.
> `WithSharedTransaction` belongs to the `NewPollingSource`/`Outbound` `Option` family and cannot be passed to it.
>
> **The real reachability:** the `*sql.Tx` branch is a **`GroupDialect`-level** contract, exercised only by a
> **direct dialect caller** — which is exactly what the `harness` test kit is (`harness/groupstore.go:345`,
> `kit.Group.AddMember(ctx, db, …)`), and what any future SPI caller would be.

**The precondition is recorded in two normative places, CORRECTED in revision 3:**

1. `GroupDialect.AddMember`'s **interface godoc** — the SPI a direct dialect caller actually reads. This is the
   precondition's only godoc home.
2. An **acceptance criterion that drives the `*sql.Tx` branch AT THE DIALECT** (Spec 017 §6 AC-4b), which revision 1
   had no coverage for at all.

**`sql.WithMaxGroupMembers`'s godoc says the OPPOSITE, and that is the true statement for its reader.** Revision 2
put a copy of the caveat there, where it can never apply: a caller of that option always gets a store that owns its
transaction. A caveat that cannot apply to the reader is worse than no caveat. The option's godoc now states:
*"For a store built by `NewGroupStore`, this bound is unconditionally durable — the store always owns the
transaction the dialect runs in."*

**Rejected: making the dialect roll back a transaction it does not own.** A library that issues `ROLLBACK` on a
caller's transaction destroys work it cannot see. Naming the requirement is the correct remedy; it is the same
shape as §7.1's configuration-coherence requirement, which the library also cannot enforce.

**M-3 — there is no single "group row lock"; there are three mechanisms.** Revision 1 instructed the implementer to
place the check *"after the statement that takes the group row lock"* and to rely on *"the existing `RunInTx`
wrapper"*. Neither has a referent in sqlite, which has no `sqliteRunInTx` at all:

| Dialect | Transaction wrapper | What serializes same-key adds | Where the check goes |
|---|---|---|---|
| **postgres** | `pgRunInTx` (`groupdialect.go:52`) | `INSERT … ON CONFLICT (group_key) DO UPDATE SET group_key = EXCLUDED.group_key RETURNING created_at` — the `DO UPDATE` **locks the conflicting row**; the comment records it *"serializes same-key adds (H1)"* (`:107-110`) | after that upsert **and** after the member upsert |
| **mysql** | `mysqlRunInTx` (`groupdialect.go:48`) | `INSERT … ON DUPLICATE KEY UPDATE group_key = group_key` (the **statement**, `:93-96`) — takes an **X lock** on the group row directly; the **comment** recording why `INSERT IGNORE` + `SELECT … FOR UPDATE` self-deadlocks is `:85-92`, not `:93-96` as revision 2 cited (audit **N-13**) | after that upsert **and** after `INSERT IGNORE` on the member table |
| **sqlite** | `withImmediateConn` (`groupdialect.go:52-77`) — a **dedicated `*sql.Conn`** with raw `BEGIN IMMEDIATE` (`:62`) / `COMMIT` / `ROLLBACK` | `BEGIN IMMEDIATE` itself: a **database-wide write lock**. sqlite's group upsert is `ON CONFLICT (group_key) DO NOTHING` + a **separate** `SELECT created_at` (`:112-124`) — there is **no row lock and no `RETURNING`** | anywhere inside `withImmediateConn` after the member upsert; the whole-database lock makes placement relative to the group upsert irrelevant |

**Consequence for §7.1.** The cross-instance atomicity argument is **true for all three engines but for three
different reasons**, and must be stated per engine. sqlite's `BEGIN IMMEDIATE` is in fact *stronger* than a row
lock; the argument does not weaken, it just cannot be stated once.

> 🔴 **AND THE SHIPPED SPI GODOC STILL ASSERTS THE FALSIFIED MECHANISM** (audit **N-9**). Revision 2 corrected this
> ADR and Spec §3.6.1 and left `adapter/database/sql/groupdialect.go:109-113` reading *"takes the **GROUP ROW
> LOCK** (SELECT ... FOR UPDATE or equivalent) BEFORE reading or writing any member row"* — **the exact sentence
> M-3 falsified for sqlite** — and Plan 031 Task 5 Step 5 edits that godoc **only to ADD things**. The one place a
> third-party dialect author reads was left carrying the defect the finding was about. **Revision 3 makes it a
> CORRECTION, not an addition:** *"serializes concurrent same-key adds — by a group-row lock on postgres/mysql, by
> `BEGIN IMMEDIATE`'s database-wide write lock on sqlite (D-AP)."* See Spec 017 §4 item 6.

**Consequence for D-AN's snapshot return — and it cannot be spelled `LIMIT maxMembers+1` on the shared helper.**
Each dialect's live-member `SELECT` already runs at the end of `AddMember` (`pgSelectMembers` /
`mysqlSelectMembers` / `sqliteSelectMembers` with `claimed_epoch IS NULL`), and it must emit a
**`LIMIT maxMembers+1`** — which is what makes enforcement (C)'s "bounds the raw fetch" claim exact rather than
approximate. **How it emits that limit is D-AS**, because each helper has **three** callers and only one has a cap.
On the overflow path the just-upserted member is filtered out of the materialized `[]MemberRow` in Go, and the
remaining rows are returned **with** the error — so D-AN's live snapshot costs **no extra query** (D-AF's
`COUNT(*)` is the only new round-trip `AddMember` gains), and it equals the
post-rollback set whenever the live count was ≤ cap before the add (the normal path's precondition; not the AC-4b
caller-owned-transaction path, where `cap+1` rows stay committed in the caller's transaction).

**m-6 — `classifyQueryErr` is not a pass-through.** `sql.GroupStore.Add` routes **every** dialect error through it:

```go
adapter/database/sql/groupstore.go:272-275        adapter/database/sql/groupstore.go:91-96
	rows, err := s.dialect.AddMember(…)           func (b groupBase) classifyQueryErr(ctx context.Context, err error) error {
	if err != nil {                                   if exists, probeErr := b.dialect.SchemaExists(ctx, b.db, b.table); probeErr == nil && !exists {
		return nil, s.classifyQueryErr(ctx, err)          return b.schemaNotReady()
	}                                                 }
	                                                  return err
	                                              }
```

So a routine overflow rejection costs **an additional `SchemaExists` round-trip** before the error reaches the
caller. Correctness is unaffected (the error is returned unchanged when the table exists, so the `errors.Is` target
and D-AM's `Permanent` marker both survive) — but the cost is real, and under revision 1's transient classification
it was paid **on every iteration of an infinite spin**. It is named here as a stated cost of the overflow arm, and
an acceptance criterion asserts the sentinel and the permanence marker survive the pass-through. `Add` must also
propagate D-AN's snapshot past this call site rather than discarding it with the current `return nil, …`.

**REVERSIBILITY:** the precondition and the placement table are prose plus one `LIMIT` clause per dialect; the
`*sql.Tx` AC is one test.

### D-AH — the `msgin.MessageGroupStore` SPI states the per-group bound as a contract requirement

**Decision.** `MessageGroupStore.Add`'s godoc (`groupstore.go:38-45` — the doc comment runs `:38-44`, the method
declaration is `:45`) gains:

> An implementation MUST bound the number of members it retains for a single group, and MUST report an `Add` that
> would exceed that bound as `msgin.ErrOverflowDropped` rather than growing without limit. The Aggregator's release
> strategy cannot supply this bound: three of its four paths are a caller-supplied closure or a message header, so
> the store is the only site that observes every member — and the store is the only site that can refuse a member
> *before* retaining it (D-AC).
>
> The counted set MUST include **every member the implementation still retains for that group — live and
> claimed** — and MUST be stated in the implementation's godoc (D-AF). A bound that ignores the claimed set does
> not bound: a claim moves members out of the live set without removing them.
>
> An implementation SHOULD mark the rejection `msgin.Permanent` when the group cannot drain itself, and leave it
> transient when a claim is in flight that will drain it (D-AM). A bare transient rejection of a group that will
> never drain hot-spins under the default `msgin.RetryPolicy`, which has neither a `MaxAttempts` nor a `Backoff`.
>
> An implementation MAY return the group's current LIVE snapshot **alongside** the overflow error. When it does,
> the Aggregator re-evaluates the release strategy against that snapshot and releases the group if it is ready, so
> a full-but-releasable group is not deadlocked by its own bound (D-AN). Returning `(nil, err)` remains valid and
> is what every pre-existing implementation does.
>
> When the Aggregator acts on that snapshot it **NEVER UPGRADES** the implementation's classification: a transient
> rejection is never turned permanent. It either **downgrades** the rejection to a fresh transient overflow error
> **on positive evidence that the group drained** (or that another holder is draining it), **or it replaces the
> overflow error entirely with a distinct fault — a claim failure or a release failure — which carries that
> fault's own classification, not the implementation's.** An implementation MUST NOT assume its `msgin.Permanent`
> marker survives to the consumer on every path: when the Aggregator's claim or release fails, an unmarked (hence
> transient) fault is reported instead, so a **persistently failing claim/release path retries rather than
> terminating** (D-AN; Spec 017 §3.3a.1).

> 🔴 **The downgrade clause was NEW in revision 3 (audit N-7) and its STRONG FORM WAS FALSE — corrected in
> revision 4** (audit **NEW-6**). Revision 3 promoted D-AN's prose into this clause as *"may only ever DOWNGRADE …
> **on positive evidence that the group drained** … a bug in the Aggregator's drain path costs a retry, **never a
> message the implementation marked recoverable**."* **Two of the six exits D-AN itself tabulates violate that:**
> `cerr != nil ⇒ return cerr` (exit 3) and `relErr != nil ⇒ return relErr` (exit 5) discard the store's
> `Permanent`-marked error and return an **unmarked, hence transient** one — and they do it **because the drain
> FAILED**, evidence of the opposite of drainage. Under the zero-value `RetryPolicy` that is B-1's unlogged
> zero-delay Nack loop, for the sub-case *"the release predicate fires but claim/release keeps failing"*.
>
> **The half that IS unconditional — *never upgrade* — is the half a store author most needs, and it is stated
> first.** The rest is stated truthfully rather than reassuringly, because a store author designing around a
> guarantee the Aggregator does not give is worse off than one who knows the marker can be replaced. **Promoting a
> rule into a contract does not make the rule true; N-7 asked for the promotion and revision 3 promoted the wrong
> form of it.**

**Why the MAY rather than a MUST.** A store that cannot cheaply produce the live set on the rejection path (a
broker-backed store, say) must not be forced to. The Aggregator's `group == nil` arm keeps such a store working
exactly as before; it simply forgoes the self-healing D-AN buys.

**Why it belongs on the SPI and not only on the two stores.** `MessageGroupStore` is public and
third-party-implementable — a future `pgx`, Redis or NATS group store inherits the requirement without `routing`
or the core being edited. Stating it here is what makes D-AC's "the store is the right place" true of *every*
store rather than of the two we happen to ship.

**Honest limitation.** A contract addition is **not compiler-enforced**: a third-party store that ignores it still
compiles and still satisfies the interface. The increment's answer is conformance coverage, not enforcement — both
first-party stores get a case driven through the **interface type** so it is copyable by an implementer, and the
`harness` kit gains one so all three dialects are held to it.

**REVERSIBILITY:** free — it is prose. Removing it would only lose the guidance.

### D-AI — godoc cross-references close the inference gap on the three unbounded release paths

**Decision.** Four godoc edits, no logic:

1. `routing.WithCompletionSize` gains a pointer to the store-level bound.
2. `routing.WithReleaseStrategy` gains a note that it **bypasses `completionSizeCeiling` entirely** and relies on
   the store's member cap.
3. `routing.WithReleaseWhen` gains the same note (it is sugar over the above and inherits the bypass).
4. `defaultRelease` (`routing/aggregator.go:222`) is **unexported**, so its disclosure goes on
   `msgin.HeaderSequenceSize`'s declaration — **`message.go:24`**, resolved in revision 2 rather than left as
   *"headers.go or wherever"* — and on `routing.NewAggregator`'s godoc (`aggregator.go:327`), where a caller
   relying on the default release path actually looks.

**Why this is a decision and not a chore.** The backlog entry that prompted this increment
([`docs/HANDOVER.md`](../HANDOVER.md) §6 item 7) proposed *"a one-sentence cross-reference on `WithCompletionSize`'s
godoc"* as the **interim** measure. This ADR ships the real bound **and** the cross-references, because a reader who
sees a ceiling on one option and none on its two siblings will infer the siblings are safe. That inference is what
made this defect survive Plan 029's five audit rounds.

**REVERSIBILITY:** free.

### D-AJ — a DEFAULT cap is legitimate here, where a default BYTE cap was not

**Decision.** Ship `1 << 16` as a **default**, not as an opt-in.

**The gate this must clear.** [CLAUDE.md](../../CLAUDE.md)'s Sensible-defaults escape clause — *"If no value can be
safe for an unknown caller (e.g. a byte cap that depends on the caller's legitimate payload size), make it
explicit/opt-in … rather than guessing a default that lulls the caller into a false guarantee"* — is exactly what
ADR 0032 **D-AB** used to **defer** three byte knobs. So the question is live.

**The strongest objection, stated before it is answered.** Spec 016 §3.4's own words about the ceilings are *"None
of these is reachable by a correct program. They are the boundary between a workload and a typo."* That justifies
65,536 as a **typo boundary**. This decision reuses the number as a **runtime default** — a policy correct programs
live under. Those are different jobs, and a caller aggregating a 200,000-row export into one group is doing
something sensible that this default breaks.

**Three disanalogies with the byte case, and a direct answer.**

1. **The unit is caller-owned and countable, not remote-driven.** A byte cap is unsafe to default because the
   quantity belongs to an unknown peer's payload. A member count belongs to the caller's own aggregation design.
2. **"Off" is not a safe state here.** For the byte knobs, leaving the option unset already **is** the documented,
   safe default (Spec 016 §3.8, measured). For per-group members the unset state is **unbounded** — it is the
   defect. Opt-in-only protects only callers who already know about the problem.
3. **A larger safe value exists.** The byte case had none — that is why it was deferred. Here the escape hatch is
   `WithMaxGroupMembers` up to `1 << 20`, 16× the default, with a typed error at its own boundary.

**The answer to the objection.** The ceiling→default transfer holds *a fortiori*: if no correct program reaches
65,536 members **via `WithCompletionSize`** (the ratified claim), then no correct program reaches 65,536 members
**at all** — the quantity and the unit are identical, and only the enforcement site differs. A group does not
become a different object because its release threshold arrived as a closure. What changes at the boundary is the
**failure mode**.

> 🔴 **REVISED in revision 2 (audit B-1).** Revision 1 finished that sentence *"…and D-AE makes it loud, typed,
> **retryable** and named rather than a silent guess."* That was the load-bearing claim, and it was **false under
> the shipped defaults**: `RetryPolicy{}` has no `MaxAttempts` and no `Backoff`, so the transient arm neither
> dead-lettered nor logged — it spun. **A default cap whose boundary behavior is an unlogged infinite spin is not
> a safe default; it is a worse defect than the one it replaces.** D-AJ therefore now depends on **D-AM**, not on
> D-AE alone: the boundary is loud, typed (`ErrOverflowDropped` through the `Permanent` marker), **terminal rather
> than retryable** when the group cannot drain, and **self-healing** rather than terminal when it can (D-AN).
> **If D-AM is reversed, D-AJ falls with it** and the honest fallback is opt-in (no default), because an opt-in
> bound at least does not convert an unbounded group into a spinning one.
>
> 🔴 **"LOUD" — corrected in revision 3** (audit **N-11**). Revision 2 glossed it as *"a WARN on the dead-letter
> fallback"*, citing `warnInvalidFallback`. In the **fully-default** configuration this decision is about,
> `invalidTarget` returns `fellBack = (DeadLetter != nil)` = **false** (`endpoint/consumer.go:942`), so that WARN
> **never fires**. The loud signal there is `divertTerminal`'s nil-sink WARN (`:1049`), which names both missing
> options — and it is followed by **`safeAck`** (`:1073`), so **the source drops the message**. "Loud" is still
> true and is still enough to carry D-AJ; it is a **WARN + Ack**, not a WARN + capture, and D-AM's table now says
> so.

**The behavioral break is accepted and stated.** A caller aggregating >65,536 members per group changes behavior.
The project is pre-v1, unreleased, untagged, with no consumers — breaking changes are free. **Free is not
unstated:** it is recorded in Spec 017 §3.10, on both options' godoc, and in Plan 031's final task.

**REVERSIBILITY:** one constant per store. See Spec 017 §8 item 3.

### D-AK — bounded-but-stuck is accepted; liveness stays opt-in via `WithGroupTimeout`

**Decision.** With the cap in place, a group whose release predicate **can never be satisfied** is
**memory-bounded but permanently stuck**: it holds exactly `maxGroupMembers` members forever, and every subsequent
member for that key is rejected and **terminated at the invalid-message sink, or the dead-letter sink when none is
configured** (D-AM). **This is accepted.**

> 🔴 **REVISED in revision 2 (audit B-1 and M-6).** Revision 1 said the rejected member is *"rejected, retried and
> dead-lettered"* and that liveness is **"unchanged"**. Both were wrong:
>
> - *"retried and dead-lettered"* assumed a `RetryPolicy` with a `MaxAttempts` and a `DeadLetter`. **The shipped
>   zero value has neither**, so the real behavior was an unlogged infinite spin (B-1). D-AM replaces it with a
>   terminal settlement that works on the zero value.
> - *"liveness unchanged"* was false for a group that **is** releasable but whose release failed once: revision 1's
>   cap check removed the "retry / next member re-releases" recovery that `AbandonGroup`'s own godoc promises,
>   creating a **new** permanent deadlock (M-6). **D-AN restores that recovery**, so the class of stuck groups is
>   now strictly *"the release predicate is unsatisfiable"* — which genuinely never released before either.

**Why it is a strict improvement.** On the two axes that matter it strictly dominates the status quo — memory goes
from unbounded to bounded, and observability goes from *silent until the process dies* to *one typed, named error
per rejected member at the operator's sink — and, with **neither** sink configured, `divertTerminal`'s nil-sink
WARN naming both missing options (`consumer.go:1049`) followed by `safeAck` (`:1073`), so the source drops the
message. A `DeadLetter`-only caller additionally sees `warnInvalidFallback` — **once per consumer**
(`:968-973`), not per message.* *(Revision 3 read *"plus a WARN when the fallback fires"* here, naming the WARN
N-11 proved never fires on `RetryPolicy{}` — audit **NEW-10**.)* The third axis, liveness of a
group whose predicate is unsatisfiable, is **unchanged**: it never released before and it does not release now.
**The cap does not, and is not intended to, provide liveness — but after D-AN it no longer REMOVES any.**

**The remedy already ships and stays opt-in:** `routing.WithGroupTimeout` + `routing.WithExpiredGroupChannel`.

**Rejected: making `WithGroupTimeout` mandatory.** It is a second, larger behavioral break; it requires a paired
expiry channel (`NewAggregator` returns `ErrExpiryChannelRequired` otherwise — the guard is `aggregator.go:362`,
the return `:363`, **not `:360-362`** as revisions 2-3 cited, audit **NEW-9**), so
mandating it forces every caller to provision one; and choosing a default timeout for an unknown aggregation
workload is the *"no value can be safe for an unknown caller"* case **for real** — the very clause D-AJ argues does
not apply to the member count. Recorded as a follow-up: whether *cap without timeout* deserves a construction-time
diagnostic.

**REVERSIBILITY:** free — it is a stated acceptance, not code.

### D-AL — the class gate is extended by hand, and its blind spot is STATED rather than widened

**Decision.** The gate is updated mechanically for the two new options, and a **fifth accepted limitation** is added
to its header. The AST scan is **not** widened to func-typed parameters.

> 🔴 **HALF 1 IS EXACT SET EQUALITY IN BOTH DIRECTIONS, AND THE KEY SHIPS IN THE OPTION'S OWN COMMIT — recorded
> here in revision 3** (audit **B-2**, whose fix reached the spec and the plan and **never reached this ADR**;
> audit **N-14**). This is the decision that owns the class gate, so it is where a future increment adding a sizing
> option will look, and it was silent on the one property that dictates *when* the gate may be edited:
>
> 1. **Half 1's assertion is `assert.Equal(t, want, found, …)`** (`grep -n 'assert.Equal(t, want, found'
>    sizing_option_class_gate_test.go`) — **exact set equality in both directions**, not a subset check. Adding the
>    option without the key fails it; adding the key without the option fails it the other way.
> 2. **It is a ROOT-module test that walks the filesystem**, so no import boundary shields it. **The moment
>    `memory.WithMaxGroupMembers` exists on disk, root's suite is RED.**
> 3. **Therefore the conformance key and its row land in the SAME COMMIT as the option they describe** — the
>    `memory` pair with the `memory` option, the `sql` pair with the `sql` option — and *"observe the RED first"* is
>    a **within-task TDD step** (write the row, watch it fail, write the option), never a cross-task condition.
>    Deferring all gate edits to a final task would leave **six of nine tasks committing a red suite**, violating
>    Plan 031's Global constraint 8 and CLAUDE.md's per-task-commit pre-authorization.
>
> **The generalization, stated so it is not lost again** (audit **N-1**): *a cross-module edit is a red commit, and
> must land with the code that makes it green.* That is true of this gate **and of the `GroupDialect.AddMember`
> signature** (D-AG), whose consumers include `harness` and `dbtest` — see Plan 031 Global constraint 8.

**The mechanical part.** `memory.WithMaxGroupMembers` and `sql.WithMaxGroupMembers` are exported, `Recv == nil`,
`int`-parameter functions in **root-module** packages, so half 1 finds them: **17 → 19 keys** (re-derived at
`f39725d`: the gate reports 17 functions / 27 methods, so Plan 032 moved neither figure). Half 2 gains two rows in
the `fixed` arm, making the arms

**14 fixed + 1 rejects + 0 deferred + 6 safe = 21 rows = 19 AST keys + 2 manual rows.**

> 🔴 **THE COMPOSITION CHANGED AND THE TOTAL DID NOT — revisions 1-4 read *"11 fixed + 1 rejects + 3 deferred +
> 6 safe = 21"*** (audit **R4-2**). [Plan 032](../plans/032-byte-cap-ceilings.md) moved the three `msghttp` byte
> caps out of `deferred` into `fixed` and **tombstoned the `deferred` arm empty**, so the file now reads
> `12 + 1 + 6 = 19`. **`11+1+3+6` and `14+1+0+6` both total 21** — *the total survived by coincidence, which is
> exactly why a total is not a partition.* **This project's `43 ≠ 43` rule: reconcile by NAME, never by count.**
>
> 🔴 **AND `byArm` IS BUILT BY COUNTING, so the empty arm has NO KEY there. Adding `"deferred": 0` FAILS the
> assertion** — the gate file's own comment says so, and an implementer following *"3 deferred"* would add one.

> 🔴 **THE NEW ROWS USE `1<<30` — BUT THE ARM-KEYED RULE THAT SAID SO IS NOW FALSE** (audit **M-1**, corrected by
> audit **R4-2**). Revision 1 said *"both constructors reject `1<<62`"*; revisions 2-4 replaced that with
> *"`fixed`/`rejects` → `1<<30`; `deferred` → `1<<62`"*. **Since Plan 032 the `fixed` arm holds three `int64` rows
> that keep `1<<62`**, and `1<<30` is **below** their `byteCapCeiling` (`math.MaxInt32 = 2,147,483,647`), so it
> would be **ACCEPTED** — every `require.ErrorIs` on those rows would fail. The file now states the rule in **two
> dimensions**, and the second is the operative one:
>
> | Dimension | Rule |
> |---|---|
> | 1 — **the ARM fixes the required PROPERTY** | `safe` ⇒ must be **accepted** and *maximally* absurd ⇒ **`math.MaxInt`**; `1<<30` IS an int32 value, so demoting a `safe` row leaves every assertion green while the int32-truncation probe silently stops running. `fixed`/`rejects` ⇒ must be **out of range** and must render an **architecture-independent decimal**, because those rows assert `EqualError` against a rendered string |
> | 2 — 🔴 **within a REJECT ARM, the PARAMETER TYPE chooses the literal** | `int`-typed ⇒ **`1<<30`** = 1,073,741,824 (fits an `int32`, so the file compiles on `GOARCH=386`; exceeds every **int-typed** ceiling in the codebase, the largest being `1<<20`). `int64`-typed ⇒ **`1<<62`** — these compile fine on 386 and were never part of the defect. 🔴 **Do not "finish the job" by converting them** |
>
> **Both new options are `func(n int)`, so both rows assert the decimal `1073741824`** — the *instruction* is
> unchanged; only its stated reason was falsified. Following revision 1 verbatim would have re-broken 32-bit
> compilation, and nothing in Plan 031's per-task gate builds for 386 (only its Task 10 Step 3b does). **Carry the
> two-dimensional rule forward, not the one-liner:** a future increment adding an `int64` knob to `fixed` under
> the arm-keyed rule ships a row that is accepted and asserts nothing.

**FOUR executable assertions, not two — and two of them were never named until revision 5** (audit **R4-1**, the
BLOCKER of round 4). This is the decision that owns the class gate, so this is where a future increment adding a
sizing option looks, and it was silent on **half** of what such an increment must edit:

```
$ grep -nE 'require\.(Len|Equal)\(t, (tests, [0-9]+|[0-9]+, methodCount|wantArms, gotArms|map\[string\]int\{)' \
    sizing_option_class_gate_test.go
…	require.Equal(t, 27, methodCount, …                                              ← does NOT move (below)
…	require.Len(t, tests, 19,                                                        ← 19 → 21
…	require.Equal(t, wantArms, gotArms,                                              ← 🔴 wantArms: a 19-ENTRY key→arm literal
…	require.Equal(t, map[string]int{"fixed": 12, "rejects": 1, "safe": 6}, byArm,    ← 🔴 "fixed" 12 → 14
```

**Each new key needs a `"<pkg>.WithMaxGroupMembers": "fixed"` entry in `wantArms` AND a bump to `byArm`'s
`"fixed"`.** Both are **`require`**, so a stale map **aborts** the test — Plan 031's Tasks 1 and 5 **cannot reach
green** without them, and each is required to be a green unit before its commit. **They predate Plan 030**
(`git log -S 'wantArms' -- sizing_option_class_gate_test.go` → `e473deb`, a Plan 029-era commit), so this was never
tree drift: **the edit list was never complete, through all four audit rounds.** Revisions 1-4 named `wantArms`
only as Spec 017 AC-10's *probe target* and Plan 031 B9-3's *mutation subject* — never as an **edit target**.

> 🔴 **AND `wantArms`'s FAILURE MESSAGE SENDS ITS IMPLEMENTER TO A DELIVERED SPEC. THAT IS A DECISION, AND IT IS
> TAKEN HERE.** The message ends *"Moving a row between arms is a **SPEC change** — update §2.1 and §6 AC-5, do
> not just edit this map"*, and [Spec 016](../specs/016-sizing-option-bounds.md) is **DELIVERED** (Plan 029,
> merged). Adding two rows to the `fixed` arm **is** that change. Left unowned, it gave the implementer three
> unauthorised options at the increment's **first** task: edit the map and ignore the instruction (silent drift —
> exactly what the map exists to catch), amend a delivered spec ad hoc (no ADR, no trailer — forbidden by
> [CLAUDE.md](../../CLAUDE.md)'s traceability rule), or escalate.
>
> **DECISION: Plan 031 takes unconditional ownership of the Spec 016 §2.1 + §6 AC-5 fold-back, as a dedicated
> task (its Task 9b), and that task RE-DERIVES the arm table from the tree at fold-back time rather than
> transcribing a count written earlier.** This follows the precedent [Plan 032](../plans/032-byte-cap-ceilings.md)
> set when its own audit reached the same question — it took ownership rather than deferring, and it re-derived.
> **Rationale:** deferring converts a mechanical edit into a silent divergence between the gate and the spec it
> cites, in the one file whose whole purpose is to make such divergence loud; and **a fold-back task carrying a
> pre-computed number is audit R4-2 repeated one artifact over.**
>
> **REVERSIBILITY:** the alternative is a `docs/HANDOVER.md` §6 backlog item and no task — **but then this
> increment knowingly ships a `wantArms` edit the assertion's own message calls insufficient.** Not recommended.

**The count sites are GENERATED, NOT ENUMERATED — revised in revision 5** (audit **R4-2**). Revisions 2-4 said
*"ten count sites, two executable"* and listed them by line number; **both halves were wrong, and the list went
stale in one commit.** At `f39725d` the file states a count in **27** places, four of them executable. **Plan 031
Task 9 Step 3 carries the script that derives that list from the file, and the next increment reruns it.** The
site that matters most is still the **ROOT-MODULE IMPORT BOUNDARY** limitation (`grep -n 'ROOT-MODULE IMPORT
BOUNDARY' sizing_option_class_gate_test.go`): *"All 17 keys live in root-module packages today (endpoint,
adapter/http, adapter/memory, channel, resilience, routing)."* **`sql.WithMaxGroupMembers` lives in
`adapter/database/sql`, which is not on that list** — leaving it unedited turns a stated limitation into a false
claim about the gate's own coverage (audit **M-7**). *(Revision 4 pointed at "Plan 031 Task 8" for the
enumeration; the gate task is **Task 9**.)*

**`methodCount` stays 27 (audit m-10), and the gate hard-asserts it.** `GroupDialect.AddMember` gains an `int`
parameter, but all three dialect implementations **already** have `seq int64` and are therefore already counted —
the gate's own header names one of them (`grep -n 'postgresGroupDialect.AddMember'
sizing_option_class_gate_test.go`). Adding a parameter to an already-matching method changes nothing. **Stated so
the next reader neither re-derives it nor bumps it to make the gate pass.** Re-derived at `f39725d`: **17
functions / 27 methods**, unchanged by Plan 032.

**The stated limitation, verbatim for the header:**

> A bound that does not arrive as an **integer parameter** is invisible to half 1: a func-typed option
> (`*ast.FuncType`), a named func type (`*ast.Ident` — the same path as `type Bytes int64`), or a threshold read
> from a **message header** (no parameter at all). Spec 017 §1.4 enumerates the three that exist today —
> `routing.WithReleaseWhen`, `routing.WithReleaseStrategy` and `routing`'s header-driven `defaultRelease` — and
> moves their enforcement to the **store**, which observes every member regardless of how the threshold arrived.

**Why not widen the scan.** Catching `*ast.FuncType` would find `WithReleaseWhen` and miss `WithReleaseStrategy`
(a named type), and could never express `defaultRelease` (no parameter). It would also have to decide *which*
func-typed options are sizing knobs — a judgement `go/ast` cannot make without `go/types` and a loadable build of
all eight modules, the exact coupling the filesystem walk exists to avoid (ADR 0032 **D-AA**). **A gate that covers
one of three while reading as complete is worse than a stated limitation** — that is the same inversion D-AB was
written to stop.

**One more thing stated so the next audit need not re-derive it.** `GroupDialect.AddMember` gains an `int`
parameter, but it is a **method**, excluded by the ratified `Recv == nil` boundary (ADR 0032 **D-AA**), and under
**D-AB**'s criterion it is **not** a class member: `maxMembers` **is** the bound, not a quantity bounded by
something else. No manual conformance row is required.

**REVERSIBILITY:** the key set and arm table are data in one test file.

**Serialization with Plan 032 (audit B-3) — RESOLVED: 032 LANDED FIRST, so the obligation is Plan 031's.**
[Spec 018](../specs/018-byte-cap-ceilings.md) / [Plan 032](../plans/032-byte-cap-ceilings.md) targeted **the same
`sizingConformanceKeys` slice and the same arm table**, and **landed at `f39725d`**. B-3's rule — *whichever lands
second re-derives from the tree, never from a number written in its own plan* — now binds **Plan 031,
unconditionally**. 🔴 **It was not honoured in revision 4**: this ADR still carried `11 + 1 + 3 + 6`, a partition
Plan 032 had already invalidated, and the error went unnoticed **because the stale total happened to equal the
correct one** (audit **R4-2**). **Re-derive with Plan 031 Task 9 Step 3's script, at the commit the task runs on —
and reconcile by name, not by total.**

### D-AQ — the `default maxGroupMembers ≥ completionSizeCeiling` invariant IS mechanically enforceable, by AST

**NEW in revision 2. This is the disposition of audit MAJOR M-5, and it CLOSES what revision 1 recorded as an
accepted open item.**

**The claim that was wrong.** Revision 1's Consequences said: *"**The invariant … is NOT mechanically enforced.**
Both constants are unexported and live in different packages, so no blackbox test can compare them … The defence is
a cross-reference comment on each constant plus a grep in the final task."* Spec 017 §3.5, §6 AC-3 and §8 item 4
all repeated it.

**It is false, and the technique that refutes it is already shipped in the very file this increment edits.**
`sizing_option_class_gate_test.go` is a **root blackbox test** (`package msgin_test`) that parses every non-test
`.go` file in all eight modules with `go/parser` (`grep -n 'parser.ParseFile' sizing_option_class_gate_test.go`).
**Unexportedness and package
boundaries are irrelevant to a parser.**

**Decision.** Ship an **AST invariant test** — a root blackbox test that parses **three** files, reads the
`*ast.BasicLit`/`*ast.BinaryExpr` constant values off the tree, and asserts
`defaultMaxGroupMembers >= completionSizeCeiling` **for each store**:

```
routing/aggregator.go                const completionSizeCeiling  = 1 << 16    (shipped, :33)
adapter/memory/groupstore.go         const defaultMaxGroupMembers = 1 << 16    (NEW — D-AR)
adapter/database/sql/groupstore.go   const defaultMaxGroupMembers = 1 << 16    (NEW — D-AR)
```

It is **less work than the cross-reference comments it replaces**, and unlike them it fails when someone edits one
number.

> 🔴 **TWO REPAIRS IN REVISION 3, both audit N-4.**
>
> 1. **There has to be a constant to parse.** Revision 2's evidence block quoted
>    `adapter/memory/groupstore.go:62 const maxGroupsCeiling = 1 << 20` — **the group-COUNT ceiling**, which is
>    neither term of the invariant and is not a constant this test reads. It demonstrated the parseability of the
>    wrong declaration. The **default** is not a `const` at all today: it is a bare literal in a composite literal,
>    `cfg := groupStoreConfig{clock: …, maxGroups: 1024}` (`:98`), and Plan 031 Task 1 Step 4 specified the new
>    field the same way — *"(default `1 << 16`)"*. A faithful implementation therefore gives this test **nothing to
>    find**, and its own not-found guard fires. **D-AR** fixes that by requiring named constants.
> 2. **`sql` carries the identical risk and was not covered.** Same default, same Aggregator, same
>    `WithCompletionSize` — a `sql` caller hits the same silent deadlock. Covering one store is this increment's own
>    *"fix the class, not the instance"* lesson violated inside the fix for M-5.

The cross-reference comments stay — they explain *why* to a human — but they are no longer the defence, **and they
go on the DEFAULT constant, not the ceiling**: the ceiling is not a term of the invariant. **Delete every "not
mechanically enforced" / "no blackbox test can compare them" claim** from Spec 017 §3.5, §6 AC-3 and §8 item 4, and
from this ADR's Consequences.

**Killing mutants** (the project's standing rule — a killed mutant is the evidence, not a green run): (a) change any
of the three literals so a relation is violated ⇒ the test fails naming the constants, their files and their values;
(b) rename a constant without updating the test ⇒ the **not-found guard** fires rather than a vacuous `0 >= 0` pass;
(c) drop the `sql` file from the parse set ⇒ the test **fails**, because the file list is asserted rather than
iterated over whatever happens to exist.

**REVERSIBILITY:** free; it is one test — but reversing **D-AR** with it costs the enforcement.

### D-AR — both new sizing values are NAMED CONSTANTS in both packages, deviating from a shipped precedent

**NEW in revision 3. This is the disposition of the declaration-form half of audit MAJOR N-4.**

**The precedent being deviated from.** `adapter/memory` declares its group-count **ceiling** as a package constant
(`const maxGroupsCeiling = 1 << 20`, `groupstore.go:62`) and its group-count **default** as a bare literal inside a
composite literal (`cfg := groupStoreConfig{clock: clockwork.NewRealClock(), maxGroups: 1024}`, `:98`). An
implementer copying the sibling arm — which is exactly what D-AC reason 3 tells them to do — writes
`maxGroupMembers: 1 << 16` inline.

**Decision.** Both store packages declare **both** values as named package constants, **in a NAMED FILE**:

```go
// adapter/memory/groupstore.go        AND        adapter/database/sql/groupstore.go
const defaultMaxGroupMembers = 1 << 16 // 65,536 — D-AD; D-AQ's AST test locates this declaration BY NAME, IN THIS FILE
const maxGroupMembersCeiling = 1 << 20 // 1,048,576 — D-AD
```

**Why the deviation, and why it is a decision rather than a style choice.** D-AQ's invariant test locates the
default **by name on the AST**. A bare literal in a composite literal has no name to locate, so the test's own
non-vacuity guard fires and Task 3 blocks on a defect Task 1 was never told to avoid — the dependency runs
backwards. **A shipped precedent departed from silently is a future audit finding**, so it is recorded here with
its reason: *the declaration form is load-bearing for a mechanical gate.*

> 🔴 **AND SO IS THE DECLARATION'S FILE — NEW in revision 4** (audit **NEW-4**). D-AQ's parse set is **asserted**,
> not discovered (its mutant (c)), so a constant declared **anywhere else in the same package** fires the same
> not-found guard a bare literal would. Revision 3 mandated the *form* and left the *location* unstated — **N-4
> one attribute over** — and the natural wrong home is real: Plan 031 Task 5 Step 4's own subject is
> `adapter/database/sql/helpers.go` (where the fifth `checkRange` copy lands), and `checkRange`'s range arms are
> the constants' obvious neighbours. **`groupstore.go` is also the local-precedent home** in that package, which
> already declares `defaultGroupLeaseTTL` (`:22`) and `defaultExpiredGroupsLimit` (`:30`) as named constants — so
> for `adapter/database/sql` this decision is *consistent* with local precedent, and only `adapter/memory`'s
> `maxGroups: 1024` is actually deviated from.

**Scope.** `maxGroups: 1024` is **not** changed. No invariant test reads it, and changing shipped code for symmetry
alone is the kind of unforced edit this project's review gate rejects.

**REVERSIBILITY:** two declarations. Reversing it **removes D-AQ's enforcement**, returning the invariant to the
accepted-drift state round 1's M-5 closed. Do not reverse one without the other.

### D-AS — the three `*SelectMembers` helpers take a private `limit int` (0 = unlimited); only `AddMember` passes non-zero

**NEW in revision 3. This is the disposition of audit MAJOR N-5.**

**The defect.** D-AP (revision 2) said each dialect's live-member `SELECT` *"gains a `LIMIT maxMembers+1`"*. Those
`SELECT`s live in **one shared helper per dialect**, and each helper has **three** callers:

```
$ grep -rn "SelectMembers(ctx" adapter/database/sql/{postgres,mysql,sqlite}/groupdialect.go
postgres/groupdialect.go:121   pgSelectMembers(…, "claimed_epoch IS NULL")        ← AddMember      (has maxMembers)
postgres/groupdialect.go:163   pgSelectMembers(…, "claimed_epoch = <newEpoch>")   ← ClaimGroup     (does NOT)
postgres/groupdialect.go:307   pgSelectMembers(…, "claimed_epoch IS NULL")        ← ExpiredGroups  (does NOT)
mysql/groupdialect.go:113 / :161 / :298      — identical three-site shape
sqlite/groupdialect.go:131 / :177 / :314     — identical three-site shape
```

As written the instruction is **unimplementable** at two sites in three. Read literally — the `LIMIT` baked into the
helper's SQL — it **silently truncates `ClaimGroup`'s claimed set** (a legitimately at-cap group releases an
**incomplete aggregate** — the silent data corruption Spec 017 §5 rejects under *"force-release the group"*, reached
by a different road) **and `ExpiredGroups`' recovery set** (the reaper drops members). **Neither loss is visible to
any acceptance criterion in the bundle.**

**Decision.** Each of `pgSelectMembers` / `mysqlSelectMembers` / `sqliteSelectMembers` gains a **private
`limit int` parameter**, where **`0` means unlimited** and emits no `LIMIT` clause. **`AddMember` is the only caller
that passes a non-zero value** (`maxMembers+1`); `ClaimGroup` and `ExpiredGroups` pass **`0`** and keep their
current behavior byte-for-byte.

**Why a parameter rather than a second helper.** Two near-identical query builders per dialect is three duplicated
pairs to keep in sync across three engines — the shape ADR 0031 **D-R**'s four-independent-copies rule tolerates
*across packages* and not *within one function's callers*. A parameter makes each call site state its own bound,
which is what the finding is actually about: the limit belongs to the **caller's contract**, not to the query.

**Why private.** An unexported helper parameter adds no class-gate key (the gate's `Recv == nil` **exported**
boundary, ADR 0032 **D-AA**), so this costs nothing at the gate.

**This is a constraint, not a convention, and it is mutation-proven.** Spec 017 §6 AC-9 row 15: pass `maxMembers+1`
from `ClaimGroup` ⇒ an over-cap claimed group is truncated ⇒ a `harness` conformance case fails. Without that
mutant the "0 means unlimited" rule is a comment.

**REVERSIBILITY:** one parameter per dialect. Reversing it degrades enforcement (C)'s *"bounds the raw fetch"* claim
from exact to approximate — and reversing it by baking the `LIMIT` into the helper re-introduces the truncation.

> 🔴 **`limit = 0` on `ClaimGroup` IS CORRECT, AND IT IS ALSO HALF OF NEW-7.** Composed with revision 3's
> live-only counted set, passing `0` there left the durable table unbounded (D-AF's box). **The remedy is D-AF's
> reversal, not a `LIMIT` on `ClaimGroup`** — truncating a claimed set releases an incomplete aggregate, which is
> the silent data corruption this decision exists to prevent. Recorded here because the two decisions are only
> safe *together*, and a future reader looking at either alone will not see it.

### D-AT — Plan 031's Task 3 is REORDERED after Tasks 5+6, never SPLIT

**NEW in revision 4. This is the disposition of audit round 3's BLOCKER, NEW-1, and it REVERSES an instruction
revision 3 gave.**

**What revision 3 said.** Plan 031 Task 3's header offered two permitted orders: *"Run Task 3 **after** Tasks 5+6
land, **or split it**: the `memory` + `routing` assertions with Task 1 and the `sql` assertion with Task 5."* The
split was justified from B-2/N-1's rule — *a gate reading a declaration must ship in that declaration's commit*.

**Why that reads the rule's SHAPE and not its MECHANISM.**

| | B-2 / N-1's gate | Task 3's gate |
|---|---|---|
| Does it exist yet? | **Yes** — `sizing_option_class_gate_test.go` ships on `main` | **No** — `group_member_bound_invariant_test.go` is created by Task 3 |
| What is already red? | **Root's suite**, the moment `memory.WithMaxGroupMembers` exists on disk, because half 1 asserts **exact set equality in both directions** (`grep -n 'assert.Equal(t, want, found' …`) | **Nothing.** Adding `const defaultMaxGroupMembers` makes no existing test fail |
| Which way does the dependency run? | The **declaration** depends on the **gate** ⇒ they must ship **together** | The **test** depends on the **declarations** ⇒ the test ships **no earlier** than the last one |
| Therefore | a **co-commit** constraint | a **reorder** constraint |

**Decision.** Task 3 runs **after Tasks 5+6**, as a single task producing a single commit. **The split
alternative is deleted from the plan** — not deprecated, deleted — because a plan that offers a defective option
as a co-equal alternative will have that option taken. The task keeps its number (renumbering would churn every
cross-reference in three artifacts for no gain); Plan 031's Sizing table carries an explicit **execution-order**
column, and Task 3 opens with a hard **Step 0 order gate** that greps for all three declarations and stops if any
is missing.

> 🔴 **"GREPS FOR ALL THREE DECLARATIONS" IS NORMATIVE, AND THE PLAN DID NOT IMPLEMENT IT — corrected in revision
> 5** (audit **R4-4**). Revision 4's Step 0 asserted a **count**: *"`grep -n "defaultMaxGroupMembers" <two files>`
> must return **two** hits … If any is missing, Tasks 1 and/or 5+6 have not landed — STOP."* **On a correct tree
> it returns at least four, and most likely six** — Step 4 of both Task 1 and Task 5 puts the `const` declaration
> **and** an initialiser reference (`maxGroupMembers: defaultMaxGroupMembers`) in the same file, and Step 7's
> mandated godoc shape adds a **third**, because a Go doc comment **begins with the identifier it documents**
> (`// maxGroupsCeiling is the upper bound …`, `adapter/memory/groupstore.go:55`).
>
> **So the gate installed to enforce THIS decision halted on the tree this decision describes, and sent the
> implementer back to re-run tasks that had already landed correctly. A gate whose only failure mode is a false
> positive is worse than no gate**, because the first person to hit it deletes it. Two-of-three, inverted: **this
> ADR had the correct rule and the plan had the defective instance.** The assertion is therefore pinned **here**,
> so the plan cannot drift from it again:
>
> ```bash
> grep -q "^const defaultMaxGroupMembers" adapter/memory/groupstore.go       || { echo "STOP: Task 1 has not landed";    exit 1; }
> grep -q "^const defaultMaxGroupMembers" adapter/database/sql/groupstore.go || { echo "STOP: Tasks 5+6 have not landed"; exit 1; }
> grep -q "completionSizeCeiling"         routing/aggregator.go              || { echo "STOP: shipped constant missing";  exit 1; }
> ```
>
> **Three independent per-declaration conditions; no count anywhere; each failure names the task actually
> missing.** This is the project's standing *"assert the partition, not just the rows"* — a count is a **proxy**
> for the condition, and the condition is *"each of these three declarations exists."*

**And the split is strictly worse in four independent ways**, which is why this is a decision rather than a
preference:

1. **B3-3's mutant is unprovable in the first half.** `sql`'s constant legitimately does not exist yet, so the
   *asserted* file list would have to be written as two files and then rewritten to three — a file list edited to
   match what happens to exist, which is precisely what B3-3 forbids.
2. **The mandatory non-vacuity probe runs twice**, giving the second half a standing excuse to skip it.
3. **The RED probe becomes impossible.** In the Task-1 half there is no constant, so what fires is the
   **not-found guard** — a different failure, proving the guard rather than the value read. The two distinct
   probes collapse and the weaker one wins.
4. **Three references dangle** — Plan 031 Task 10 Step 5, the Sizing table's single Task 3 row, and Step 6's
   single commit message.

**REVERSIBILITY:** free — it is an ordering, not code. **But do not re-derive it from N-1's rule**: that is the
mistake this decision exists to record. *A gate that already runs constrains the commit; a gate you are about to
write constrains only the order.*

## Consequences

**Positive.**

- **The defect class is closed by construction, not by enumeration.** The bound sits at the one site every member
  passes through, so a **fifth** release path — or a caller's own exotic strategy — is governed without any
  document being edited. This is the project's stored lesson *"fix the class, not the instance"* applied to a class
  whose members cannot all be listed.
- **The SQL store is bounded for the first time**, and D-AG's in-transaction placement makes that bound exact,
  durable and **atomic across horizontally-scaled instances** — properties the cheap alternative does not have.
  🔴 **"Durable" became true only in revision 4** (D-AF, audit **NEW-7**): with `sql` counting live members only,
  `ClaimGroup` zeroed the count and the member table grew by up to `cap` rows per failed-release cycle, without
  limit, while every acceptance criterion passed. **Counting live + claimed is what makes this sentence true.**
  **For a store built by `NewGroupStore` the bound is unconditionally durable**: the store holds a concrete
  `*stdsql.DB` and always owns the transaction the dialect runs in (D-AP, audit **N-2**). The
  caller-owned-`*sql.Tx` exception is a **`GroupDialect`-level** contract, reachable only by a direct dialect
  caller; it is stated on the SPI godoc and covered by AC-4b. *(Revision 2's Consequences said "durability is
  conditional on the dialect owning the transaction" without that qualification, which overstates the exposure for
  every consumer of the shipped store — i.e. all of them.)*
- **The two overflow arms in `memory.GroupStore.Add` become symmetric**, and both gain a message naming which cap
  fired. Four producer sites of `ErrOverflowDropped` stop being indistinguishable in a log.
- **The SPI carries the requirement forward** to `pgx`, Redis and NATS group stores that do not exist yet.
- **The class gate's blind spot becomes documented rather than latent.** A future contributor adding a func-typed
  sizing option reads the limitation instead of trusting a green run.
- **The overflow boundary is terminal-and-logged rather than an unlogged infinite spin** (D-AM). The remedy no
  longer degrades the failure mode it is fixing, and it behaves correctly on the shipped zero-value `RetryPolicy`
  rather than only on a fully configured one.
- **A full-but-releasable group heals itself** (D-AN): the Aggregator re-fires the release on the snapshot the
  store returns with the rejection, so the cap never destroys a release opportunity. The `AbandonGroup` recovery
  contract — *"a retry / next member / next reaper tick re-releases"* — survives the change.
- **The `default ≥ completionSizeCeiling` invariant is CLOSED, not accepted** (D-AQ), by an AST test cheaper than
  the comments it replaces — **for BOTH stores**, over **named** constants the test can actually locate (D-AR).
- **The class gate's ordering rule is now recorded in the decision that owns the gate** (D-AL), not only in the
  spec and the plan — and it is stated as the **class** (*a cross-module edit is a red commit*) rather than as the
  one instance it was found on. **D-AT records where that class does NOT apply** (a gate that does not exist yet
  constrains only the order), so the generalization does not over-fire the way it did on Task 3.
- **Both stores now count the same set** (D-AF), so `MessageGroupStore.Add`'s contract paragraph carries **one**
  sentence about the counted set instead of two, and `memory`'s claim-window rejection stops reading as an
  idiosyncrasy: it is the mechanism that bounds the group across a claim boundary in **both** stores.

**Negative / accepted costs.**

- **A behavioral break** for any caller aggregating >65,536 members per group (D-AJ). Free at pre-v1; stated.
- **A breaking `GroupDialect.AddMember` signature change** across **seven** sites in **six** modules (D-AG),
  roughly doubling the increment. Free at pre-v1; the SPI's own godoc reserves the right.
- **`msgin.ErrInvalidCapacity` reaches SIX producers** across four units — queue depth, group count, channel
  buffer, group members (D-AD). D-X's argument still holds, but the margin is thinner and a seventh needs an ADR.
- **A group whose release predicate is unsatisfiable stays stuck** (D-AK), and its members now **terminate at the
  invalid-message or dead-letter sink** rather than retrying forever (D-AM). The cap converts an OOM into a
  bounded, logged loss stream; it does not release anything.
- **D-AM dead-letters some messages that a reaper would eventually have admitted** — for `memory`, only a
  `WithGroupTimeout` caller's; **for `sql`, also the DEFAULT configuration's**, in the stranded-lease case
  (D-AM's counter-example box, audit **N-3**). This is the deliberate trade in D-AM, taken because a hot spin is a
  production-down event and a dead-lettered message is recoverable. It is the decision in this ADR most likely to
  be argued the other way, and revision 3 states its true width.
- **With NEITHER sink configured, a permanent rejection is WARNed and then ACKED** (`endpoint/consumer.go:1049`,
  `:1073`) — the source **drops** the message rather than redelivering it, and the fallback WARN a dead-letter-only
  caller sees fires **once per consumer**, not per message (`:968-973`). Named in revision 3 (audit **N-11**) so it
  is not rediscovered from a missing message.
- **`Aggregator.Handle`'s error branch has SIX exits**, four of which revision 2's coverage tables did not name
  (audit **N-7**), including a deliberate divergence from the success path's `claim == nil` behavior. All six are
  now covered and mutation-proven (Spec 017 §3.3a.1, §6 AC-9 rows 12a-12d).
- **Two new declaration-level constraints** that a reader copying local precedent would violate: named
  `defaultMaxGroupMembers` constants **in a named file** (**D-AR**, deviating from `maxGroups: 1024`) and the
  private `limit int` parameter on the three `*SelectMembers` helpers (**D-AS**, because one shared helper serves
  three callers with different bounds).
- **🔴 THIS INCREMENT AMENDS A DELIVERED SPEC** (**D-AL**, audit **R4-1**). Adding two rows to the class gate's
  `fixed` arm is a change to [Spec 016](../specs/016-sizing-option-bounds.md) §2.1 and §6 AC-5, and the gate's own
  `wantArms` assertion says so in its failure message. Plan 031 owns that fold-back as its **Task 9b**, with a
  `Spec: 016` trailer and two-way cross-links. **The cost is one extra documentation task and one more artifact in
  the traceability chain; the alternative is a knowing, silent divergence in the one file built to make such
  divergence loud.**
- **🔴 THE CLASS GATE NOW COSTS FOUR ASSERTION EDITS PER NEW SIZING OPTION, NOT TWO** (**D-AL**, audit **R4-1**) —
  the key, the row, `require.Len(t, tests, …)`, `wantArms` and `byArm`, of which the last two are **`require`** and
  abort the test. That is the price of the gate asserting a **partition** rather than a **count** (the project's
  *"assert the partition, not just the rows"* lesson, applied by Plan 029 Task 8 and paid here). **It is the right
  trade** — a count map is blind to a pairwise arm swap — **but it is a real per-increment cost, and leaving it
  unstated cost this increment four audit rounds.**
- **🔴 `sql`'s `AddMember` costs ONE EXTRA `COUNT(*)` PER ADD** (D-AF, D-AG) — on every add, not only on overflow,
  inside a transaction that already issues three statements, and the scan is `O(members)` (up to
  `maxGroupMembers`, default 65,536). It is the price of counting live + claimed, it needs no new SQL (the
  `*CountMembers` helpers ship today), and it cannot be derived from D-AS's live-only member `SELECT`. 🔴 **The
  cost is per-engine** (audit **R4-8**): postgres and mysql pay it inside a **per-key** group-row lock; **sqlite
  pays it inside `BEGIN IMMEDIATE`'s DATABASE-WIDE write lock** (`sqlite/groupdialect.go:62`), so there it
  lengthens a **global** write-serialisation window on every add to *any* group — the same property Spec 017 §7.1
  records as sqlite's correctness **advantage**, seen on the throughput axis. Named as a follow-up in Spec 017 §8.
- **BOTH stores' claim windows transiently reject arrivals** for a group at exactly the cap (D-AF — `sql` joins
  `memory` here in revision 4). Bounded and retryable — but under the zero-value `RetryPolicy` the retry is a
  **busy-wait** for the claim window's duration, the same exposure the existing `WithMaxGroups` arm carries.
  🔴 **For `sql` that window has a much longer tail than revision 4 stated: a CRASHED releaser holds the lease
  until it expires AND until the reaper next ticks — `up to 2 × WithGroupLeaseTTL`, ≈ 10 minutes at the defaults,
  not 5** (audit **R4-3**; the two-term derivation is on D-AF consequence 2). Each retry there is a full
  rolled-back write transaction taking the group-row lock, plus a `SchemaExists` probe, at `WithConcurrency(N)` —
  **not** a bare "zero-delay busy-wait". It is bounded and self-healing (the default sweep drains a crashed-lease
  group at the same interval, D-AM's premise box), transient is therefore the correct classification, and it is
  recorded as **Spec 017 §8 item 9** for the user rather than absorbed.
- **🔴 A persistently failing claim/release path RETRIES rather than terminating** (D-AN exits 3 and 5, D-AH;
  audit **NEW-6**). The overflow error is replaced by an unmarked — hence transient — store or channel fault, so
  under `RetryPolicy{}` that sub-case is B-1's zero-delay Nack loop. Accepted, because marking a store or channel
  fault `Permanent` merely for having been reached through an overflow would dead-letter on the wrong cause. The
  remedy for a caller who cares is `RetryPolicy.Backoff`; the root cause is the zero-value policy itself, which
  Spec 017 §8 already records as surviving this increment.
- **`Aggregator.Handle` gains a second release-firing site** (D-AN). The release path is now reachable from the
  error branch as well as the success branch, which is more surface to keep correct — mitigated by driving both
  through the same `a.release` helper and by AC-1b's id-less case.
- **Neither store's quadratic per-`Add` cost is fixed.** The cap bounds the damage; `memory` still clones the group
  on every call and `sql` still re-fetches and re-decodes every live member on every arrival (now capped by
  `AddMember`'s `LIMIT maxMembers+1` — D-AP via D-AS's `limit` parameter; **`ClaimGroup` and `ExpiredGroups` stay
  unbounded, deliberately**). Both are named follow-ups. **`sql.GroupStore.ClaimGroup` in particular decodes the
  whole claimed set into the process heap at `limit = 0` (`groupstore.go:285-298`)** — which is why D-AF's
  *"claimed members are retained by the database, not the process"* was false, and which the member cap (not a
  `LIMIT`) is what bounds.
- **Every overflow rejection on the `sql` path costs an extra `SchemaExists` round-trip** through
  `classifyQueryErr` (D-AP / audit m-6; `adapter/database/sql/groupstore.go:91-95`, reached from `:274`).
  🔴 **Bounded only for a NOT-LEASED rejection, which D-AM makes terminal. A LEASED rejection is TRANSIENT — the
  arm D-AF's reversal created — so it pays the probe on EVERY RETRY, for up to the crashed-lease window of Spec
  017 §8 item 9** (see the claim-window bullet above). It was unbounded on every path under revision 1.

  > 🔴 **REVISION 4 READ *"Bounded now that D-AM makes the rejection terminal"*, AND D-AF's REVERSAL FALSIFIED
  > THAT IN THE SAME REVISION** (audit **R4-6**). The bullet's first clause is right — **every** rejection pays
  > the probe — and it was then bounded by a property only **some** rejections have, because `sql` had had no
  > transient arm until revision 4 gave it one. **The identical sentence stood in Spec 017 §8's backlog list**;
  > both are corrected in revision 5. **Two-of-three in the opposite direction from NEW-8: both artifacts stale,
  > one reason.** This is what Plan 031 Task 10 **Step 0** exists to catch, and it is why that reconciliation must
  > run as part of **closing a revision**, not only at the delivery gate — revision 4 did not run it, which is how
  > this survived.

> **REMOVED in revision 2:** the entry claiming *"The invariant default `maxGroupMembers` ≥ `completionSizeCeiling`
> is NOT mechanically enforced."* It is enforced, by D-AQ's AST test. Audit **M-5**.

**Neutral / to watch.**

- ~~**The `memory`/`sql` counting asymmetry (D-AF)**~~ — **RESOLVED in revision 4 by adopting symmetry** (audit
  **NEW-7**). It was defensible per-store and it did not bound the durable table. Both stores now count live +
  claimed; the SPI carries one sentence. Spec 017 §8 item 1 is closed.
- **Nothing in the library can enforce that horizontally-scaled instances agree on the cap** (Spec 017 §7.1). It is
  documented as an operator requirement, in the same family as the existing `WithGroupLeaseTTL` coherence
  requirement.
- **🔴 THE COORDINATE-DRIFT CLASS IS NOT CLOSED BY REVISION 5 — IT IS RE-SHAPED, AND ONLY FOR ONE FILE.**
  Four consecutive rounds returned an off-by-one citation finding; revision 4's mechanical sweep was **correct for
  the tree it ran against** and went stale in **one commit** (audit **R4-2**). Revision 5's answer is structural
  **but scoped to the class-gate file**, as R4-2's required fix was: Plan 031 Task 9's site table is **generated
  by a script**, and load-bearing class-gate coordinates are written as the **grep that locates them**.

  **The `adapter/database/sql/groupstore.go` and `routing/aggregator.go` coordinates did NOT get that treatment**
  — revision 5 corrected 41 of the former **by hand** after `de38a95`, a **comment-only** commit, shifted every
  one at or below `:207`. **Those two files are this bundle's most-cited and remain exposed to exactly the failure
  that produced that correction.** Widening the method change to them is **recommended for a future revision** and
  is deliberately not done here, because an audit disposition that grows past its finding stops being reviewable
  against the round that produced it. The generalised lesson and the recommendation are recorded in
  [`031-audit-round-4.md`](../plans/031-audit-round-4.md)'s clearly-marked **LATER ADDITION** subsection.

  **And the script itself is untested — no round has audited it.** If a fifth round runs, this is the mechanism to
  attack: *does its output actually cover every site an implementer must edit?* The project's *"vacuity-probe
  every gate"* lesson applies unchanged — **proving it FIRES is not proving it COVERS.**
