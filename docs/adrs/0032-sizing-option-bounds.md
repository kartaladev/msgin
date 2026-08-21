# ADR 0032 — A sizing option carries a stated ceiling, enforced before the allocation

- **Status:** **PROPOSED (2026-08-21), revision 6** — written before any code, per CLAUDE.md's design-time gate.
  - Decisions **D-W** through **D-AB** were settled with the user. **D-AB has now been amended twice**: safety
    cause **(c)** was deleted in revision 5 and cause **(d)** is emptied in revision 6 — both as circular or
    under-derived, both by the criterion they were written alongside.
  - **Revision 6** folds round-5 findings ([`029-audit-round-5.md`](../plans/029-audit-round-5.md)): 2 BLOCKERs,
    5 MAJORs, 8 MINORs. **BLOCKER-1: `msghttp.WithMaxResponseBytes` is a class member certified *safe (d)*** —
    `drainBounded` is five of six reads; the sixth retains the body as the reply payload. It is a **byte** cap, so
    the user's standing **"split by kind"** rule sends it to the **deferred** arm. Census: **9 fixed + 3 deferred
    + 4 safe**. **BLOCKER-2** was a fold-in that stopped one file short, in the plan.
  - **🔴 THE OPERATIVE LESSON, now twice-proven: a row's verdict can be stale without any count being wrong.**
    Both of the last two BLOCKERs were rows whose *"safe"* verdict survived the criterion written to catch them,
    and **neither changed the 16/17 totals**. D-AB is only as good as the discipline of **re-deriving every row
    from it** — including rows nobody is changing.
  - **Revisions 2–5** folded rounds 1–4: the census moved 4 → 7 → 8 → 9, D-X was corrected twice and now wraps one
    truthful shape, D-Z's rationales were corrected three times, D-AA's key count moved 16 → 17 with a stated
    `Recv == nil` boundary, and **D-AB replaced the hand-maintained census with a criterion**.
    **Verification pass pending.**
- **Prompted by:** [Spec 016](../specs/016-sizing-option-bounds.md); the backlog item in
  [`docs/HANDOVER.md`](../HANDOVER.md) §8.1, widened from one site to nine (+3 deferred).
- **Realized by:** [Plan 029](../plans/029-sizing-option-bounds.md).
- **Extends** [ADR 0031](0031-nil-option-elements.md): this ADR reuses that ADR's **R1/R2 reporting families** and
  its **latch** unchanged, and **D-Y below resolves an interaction with its D-U** that D-U itself created and
  Plan 028 deferred.

## Context

Every `WithX(n int)` in the workspace that reaches an allocation *or bounds a growing structure* validates only its
lower bound. **NINE are defective and fixed here; two more are class members whose remedy is deferred (D-AB).**
Of the nine, **four panic**, and five leave a bounded structure unbounded. All were
reproduced on this tree (Go 1.25.13, darwin/arm64) and independently re-reproduced across three audit rounds — see
[Spec 016 §1](../specs/016-sizing-option-bounds.md). **Three of the four panics fire *after* construction:**
`WithMaxInFlight` and `WithConcurrency` inside `Consumer.Run`, `WithConnectionBuffer` inside
`SSEServer.ServeHTTP`. Only `memory.WithBuffer` panics at construction.

> **Revisions 1 and 2 both got `WithConcurrency` wrong** (round-1 **M-3**, round-2 **BLOCKER-2**). Revision 1 said
> it does not panic; revision 2 said it *"corrupts a `WaitGroup`"* with a **latent** panic on a later `Done()`,
> and put "Three panic, one corrupts a `WaitGroup`" in this paragraph. Measured end-to-end, `Consumer.Run`
> **panics in `wg.Add` itself**, and it does **both** things depending on the value — so it is counted with the
> panics here and the disjunction is dropped.

The decisive measurement is that an oversized allocation has **two** regimes, and the runtime's own check catches
only the milder one. With a 64-byte element on darwin/arm64 (`maxAlloc = 1<<48`):

| `n` | `elemSize × n` | Result |
|---|---|---|
| `2^43` | `2^49` | `panic: makechan: size out of range` — **recoverable** |
| `2^41` | `2^47` | `runtime: out of memory: cannot allocate 140737492549632-byte block` → **`fatal error: out of memory`** |

The second is `runtime.throw`. **No `recover` can intercept it; the process dies.** A guard that mirrors
`makechan`'s check therefore rejects `2^43` while certifying `2^41` as acceptable — it validates precisely the
value with the worse outcome. `maxAlloc` is additionally platform-derived (from `heapAddrBits`) and unexported.

**`GOMEMLIMIT` does not close the gap, measured rather than assumed:** `GOMEMLIMIT=67108864` in front of the
`2^41` case still produces `fatal error: out of memory`. It paces the GC; it does not gate a single oversized
allocation.

Two further facts shape the decisions:

- **`sync.WaitGroup.Add` panics when `int32(n) < 0`** — the counter lives in the high 32 bits of the state word,
  so `Add` panics for every `n` whose low 32 bits land in `[2^31, 2^32)`. That is **half of all `n > 2^31`**:
  `2^31`, `2^31+1` and `2^32-1` panic; **`2^32`, `2^40` and `2^62` do not** (the last three kill the "above `2^31`"
  formulation revision 2 used). `wg.Add(c.workers)` (`consumer.go:458`) sits on the primary execution path, before
  a single worker is spawned. On the non-panicking half the counter is silently wrong: `Wait()` returns while
  workers still run, and the spawn loop iterates `n` times. So `endpoint.WithConcurrency` **panics on one half of
  the band and corrupts shutdown on the other** — see [Spec 016 §1.2](../specs/016-sizing-option-bounds.md) for the
  measured band table.
- **Five knobs bound nothing.** `memory.WithCapacity`, `memory.WithMaxGroups`, `msghttp.WithMaxConnections`,
  `routing.WithCompletionSize` (revision 4) and `msghttp.WithReplayBuffer` (revision 5) cannot panic; each *is* the cap on a structure that grows
  by insertion, so an enormous value removes the cap. `WithCapacity(1<<62)` reinstates the exact overflow lever
  its own godoc says it prevents; `WithCompletionSize(1<<62)` lets a single correlation key accumulate members
  without bound (28.7 MiB at 60,000 members, measured, and quadratic in time).
- **THREE further class members get a DEFERRED remedy, not a ceiling** (D-AB): `msghttp.WithMaxBodyBytes`,
  `msghttp.WithMaxEventBytes` and `msghttp.WithMaxResponseBytes` are each the sole bound on a
  **remote-peer-driven** read into memory that is **retained**.

Seven of the nine knobs already carry a typed sentinel for their lower bound. `memory.WithBuffer` has no validation
whatsoever and sits in `memory.New`, which returns `*Broker` with no error; `routing.WithCompletionSize` has no
validation **and no config field to validate** — it sets only a closure (Spec 016 §3.9).

## Decision

### D-W — the bound is a stated per-knob ceiling, not a runtime-derived limit

Each defective knob gets a **ceiling that is part of its documented contract**, expressed in the knob's own unit
and justified by what the knob means — not a number computed from `maxAlloc`, available memory, or `GOMEMLIMIT`
(all three rejected on measured evidence in §Context).

**Why not the runtime's limit:** it admits the unrecoverable band. This is not a refinement of the runtime bound,
it is a rejection of it: any guard keyed to what the allocator *can* attempt is guaranteed to admit sizes the
allocator will die attempting. And it says nothing at all about the five growth levers, which never call `make`.

**Why not "no ceiling, document the risk":** the nine sites are reached from caller input by the ordinary route (a
config field, an env var, a units mistake). CLAUDE.md forbids a library panic on caller input without
qualification, and three of the four panics land in a request handler or in `Run`.

**Why a policy number is acceptable here,** when the project normally rejects guessed defaults: CLAUDE.md's
Sensible-defaults gate states the escape explicitly — *"If no value can be safe for an unknown caller … make it
explicit/opt-in with a clear typed error or documented off state rather than guessing a default that lulls the
caller into a false guarantee — and say so in the godoc."* The ceiling is not a hidden default substituted for the
caller's value; it is a published boundary that produces a typed error, named in the godoc with its rationale.

The ceilings are **not tunable and not exported**. An override option would be new exported surface whose only
purpose is to re-enable the crash; exporting the constants for caller-side pre-validation was considered and
deferred (Spec 016 §5) because the error message already names the bound.

### D-X — reuse the existing sentinels; mint none; wrap the value into the R1 error

The eight R1 knobs return **the same sentinel from the upper arm as from the lower**, wrapped with the offending
value, the site, and the whole range — **one shape, used by R1 and R2 alike**:

```go
return nil, fmt.Errorf("%w: %s: %d not in [%d, %d]",
	msgin.ErrInvalidMaxInFlight, "endpoint.WithMaxInFlight", n, 1, maxInFlightCeiling)
```

**Why not revision 2's `"%d exceeds %d"`** (round-2 **M2-1**): merging both bounds into one condition and
rendering *"exceeds"* makes the message **false on the lower arm** — `WithMaxInFlight(0)` would read
`"…must be >= 1: 0 exceeds 1048576"`. That arm ships **correct today**, so revision 2's debuggability fix would
have introduced a debuggability defect on a working branch. `[lo, hi]` is true at both ends. Revision 2 also had
R1 render `"%d exceeds %d"` while R2 rendered `"%d not in [0, %d]"` — two shapes for one idea inside one
increment. Naming the **site** is independently required by the three-producer resolution below.

**All six sentinel messages are genericised together** — the six being `ErrInvalidConcurrency`, `ErrInvalidMaxInFlight`, `ErrInvalidCapacity`, `ErrInvalidMaxConnections`, `ErrInvalidConnectionBuffer` and (new in revision 5) `ErrInvalidReplayBuffer`, covering nine knobs — not just `ErrInvalidCapacity` (round-2 **m2-4**): each
states a lower bound only (`"msgin: concurrency must be >= 1"`, …) under a range that is now two-ended. Fixing one
and leaving five is the fix-the-instance pattern this project is repeatedly burned by. Spec 016 §3.5 tables the
exact before/after text; it is verified test-safe (every assertion goes through `errors.Is`).

**Why reuse:** the fault a caller acts on is "this size is out of range", and it is one branch in their code at
either end. Callers already `errors.Is` against these six; splitting the range across two sentinels would silently
change the meaning of code matching only the old one, and doubles the exported surface for a distinction nobody
handles differently.

**Why wrap — revision 1 had this wrong.** Revision 1 justified reuse on *"the offending value and its bound appear
in the message"* while Plan 029's own code shape returned the sentinel **bare**, with neither (round-1 **M-6**).
One of the two had to give. Wrapping costs one line per arm, keeps every `errors.Is` working, adds no `Permanent`
(so Spec 016 §3.1's bare-of-`Permanent` rule is intact), and delivers the debuggability CLAUDE.md names as the
library's core quality criterion.

**`msgin.ErrInvalidCapacity` has FOUR producers after this increment** (round-1 **M-1**; the fourth added by
`routing.WithCompletionSize`, Spec 016 §3.9): `memory.NewQueueStore`, `memory.NewGroupStore`,
`memory.WithBuffer`, and `routing.NewAggregator`. One sentinel cannot state four different ranges, so **the
sentinel's own message stays generic** (`"msgin: capacity out of range"`) and the per-site range lives in the
wrapping message and the option's godoc. This is the second reason D-X wraps.

**They are four different UNITS, not merely four ranges** (round-4 **m4-10**): queue depth, group **count**,
channel **buffer**, group **members**. The reuse argument holds — one `errors.Is` target for *"this size is
wrong"*, with the site name disambiguating — but a **fifth** producer should be a conscious decision.
*(Revision 4 said "THREE" here while the spec said FOUR and flagged the line as load-bearing; round-4 **M4-1**.)*

**Why not mint `memory.ErrInvalidBuffer`,** which would follow the adapters-mint-their-own-sentinels precedent:
`memory` has three bounded constructors and a caller handling "this store was sized wrong" wants one `errors.Is`
target across them, not three kept in sync. `ErrInvalidCapacity` already has a root-module producer, so the
Plan 027 goal state (*"root has a producer, inside the root module, for every sentinel it declares"*) holds either
way.

**Net exported-surface change from this increment: zero.** No sentinel added, none removed, no signature changed.

### D-Y — the range check returns unconditionally, independent of the latch

**This resolves an interaction ADR 0031 D-U created, and it was re-attacked in round 1 and held.** D-U makes
`memory.New`'s apply loop `continue` past a nil option so every non-nil option still applies. Consequently a
**later** `WithBuffer(1<<62)` is still applied when an earlier nil already took the latch:

```go
memory.New(nil, memory.WithBuffer(1<<62))
//         ^ latches ErrNilFunc          ^ still applied under D-U — and reaches the make
```

Because the latch is first-fault-wins, an implementation that gates the allocation on *"did I just latch?"* skips
the guard and panics. **The `return` that prevents the allocation must not be conditional on the latch taking.**
The latch decides only *which* error the caller reads.

Verified safe in round 1: `New` initialises `b.ch` **before** the apply loop (`memory.go:59`), so an early `return`
leaves no nil channel. The plan's `memory` task must mutation-prove this — moving the `return` inside the latch's
`if` must turn a named test red (Spec 016 AC-3). Every other test in the increment passes against the wrong shape.

### D-Z — the nine ceiling values

| Knob | Default | Ceiling | Rationale |
|---|---|---|---|
| `endpoint.WithMaxInFlight` | 1024 | `1 << 20` | 1024× the default. **Measured** cost at the ceiling: `1<<20 × 48` = 48 MiB — four orders of magnitude below the fatal band, so the ceiling demonstrably prevents the crash. |
| `endpoint.WithConcurrency` | 1 | `1 << 16` | Go's minimum goroutine stack is **~2 KiB** (measured 2,052 B; **~4 KiB** under `-race`, 4,114 B) → **~128 MiB of stack at the ceiling, ~257 MiB under `-race`**. *(Revision 1 said "≥ 8 KiB … 512 MiB" — 4× out. Revision 2 fixed the 4× and left a **unit** error behind it, "~134–257 MiB", which is 134 **MB** and 257 **MiB** in one range — round-2 **m2-2**. Both were headed for the public godoc.)* |
| `msghttp.WithConnectionBuffer` | 16 | `1 << 16` | 4096× the default, **per connection**: `1<<16 × 24` = 1,572,864 B (1.5 MiB) each. |
| `msghttp.WithMaxConnections` | 1024 | `1 << 16` | 64× the default, at a single process's practical file-descriptor ceiling. **Newly bounded** so the row above stops reasoning about an unbounded product (round-1 **M-7**). |
| `memory.WithBuffer` | 0 | `1 << 20` | Queued messages. **Measured** cost at the ceiling: **25,166,072 B (24.0 MiB)**, `sizeof(msgin.Message[any]) = 24`. *(Round-2 **m2-3**: this was the only row with no measured cost, under a §3.4 framing that is explicitly a claim about bytes.)* |
| `memory.WithCapacity` | 1024 | `1 << 20` | Same unit; three knobs share "queued messages" and must agree (round-1 **m-10**). |
| `memory.WithMaxGroups` | 1024 | `1 << 20` | In-flight aggregation groups, matched to the queue depth feeding them. |
| **`msghttp.WithReplayBuffer`** | *unset = off* | `1 << 16` | Retained **events**. Matches its two `msghttp` siblings so all three of this package's ceilings read as one number. Cost is `n × the caller's frame size` — **not** a library constant; round 4 measured ~1.2 KiB/event at its own frame size (23.5 MiB at 20k events, 93.8 MiB at 80k — linear). **New in revision 5** (round-4 **BLOCKER-1**). |
| **`routing.WithCompletionSize`** | *none* | `1 << 16` | Group **members** per group. Sized by **time, not bytes**: `memory.GroupStore.Add` clones the group snapshot per call, so cost is quadratic — the ceiling costs **48.3 GiB of churn and 8.6 s** (measured) against only **31.0 MiB** live (GC'd — round-5 **M5-1**; revision 5 retracted the un-GC'd 67.8 MiB in the spec and left it standing here, where it reaches the public godoc). **New in revision 4** (round-3 **BLOCKER-1**); it is the one knob with **no existing lower-bound arm**, so it gains both bounds at once, and the one needing a **new unexported config field** before it can be validated at all (Spec 016 §3.9). |

Powers of two are chosen so the value is legible in the godoc and the error message, not because the implementation
needs alignment. **These are part of the API contract**: raising one later is permissive and safe; lowering one is a
breaking behavioral change subject to the SemVer gate.

The library bounds **each factor**. It does not police the *product* of two ceilings — a caller setting both
`WithMaxConnections` and `WithConnectionBuffer` to their maxima has explicitly asked for something no process can
serve, and gets two documented ceilings rather than a third derived one.

### D-AA — the class gate is completeness-plus-conformance, every row executable

Plan 028's gate (`option_guard_gate_test.go`) recognizes a *guard shape* in the AST. That does not transfer: an
upper-bound check has no distinctive syntax, and Plan 028's own backlog records that a syntactic recognizer is not
a dominance proof.

Instead, two halves:

1. **Completeness (AST).** Walk every non-test `.go` file from the repo root — reaching all 8 modules without
   `go.work`, exactly as the shipped gate does — and collect every exported **function** (`Recv == nil`, see the
   boundary decision below) with an `int`/`int64` parameter **in any position**. Fail if that set differs from the
   conformance table's keys **in either direction**. **The set is 17**, measured.
2. **Conformance (behavioral).** **Every key is executable** — 17 AST-discovered rows plus **2 manual rows**
   (below), in **three arms matching Spec §2.1's three verdicts**: a **class member fixed here** asserts the fault
   is **reported through the surface Spec §3 names for it — the constructor's return, or the first use of the
   object it produced**; a **class member with a deferred remedy** asserts it *accepts* `1<<62` and carries an
   annotation naming it a class member, so the row never reads as a safety certificate; a **safe** knob asserts it
   *accepts* `1<<62` and its product is usable.

   > **Phrased over the SURFACE, not "rejects `1<<62`"** (round-3 **m3-5**). That older wording **cannot be
   > written for `memory.WithBuffer`**, which returns no error — the fault surfaces at `Send`/`Stream`.
   > *(Revision 4 fixed this in the spec and the plan and left it stale here; round-4 **M4-1**.)*

**The function-vs-method boundary is `Recv == nil`, and that is a DECISION, not an omission** (round-2
**BLOCKER-1**). Revisions 1 and 2 left it unstated, and in `go/ast` *"every exported function"* reads both ways.
Measured, the two readings differ by 27 keys: `Recv == nil` yields **17**, all constructible from a root blackbox
test; *any* `FuncDecl` yields **44**, of which **22 sit on unexported receivers** (21 in leaf modules, plus
`responseTracker.WriteHeader` in root) that a root-module blackbox test cannot construct — making half 2
unsatisfiable and reproducing round-1 BLOCKER-1 verbatim. So: **functions only.**

**The exclusion costs NOTHING, once membership is derived from D-AB's criterion rather than by hand.** Applying
*"the parameter itself sizes a `make`"* to the 27 methods yields exactly **two** members —
`memory.QueueStore.Claim` (`min(max, len(s.ready))`; it carries an `audit M-1` comment proving a prior audit
already found this bug *in a method*) and `channel.QueueChannel.Poll`, which delegates into it. **Both are
constructible from a root blackbox test, so both get manual conformance rows** and one exercised chain covers
both. Spec 016 §2.0 shows the derivation.

> **This number has moved every round because nobody stated a rule.** Round 2 said three; revision 3 said four and
> carried two of them as *"named but uncovered"* (justified by root declaring no SQL driver — **true**, but moot);
> round-3 **M3-4** showed the loose reading admits three more (`{postgres,mysql,sqlite}GroupDialect.ExpiredGroups`)
> that revision 3 omitted. Under D-AB the answer is **two**, reproducible from the rule. The `sql` rows are **not**
> class members: their `make` capacity is `len(rows)`/`len(cands)` — sized by what the database returned, not by
> the parameter, which reaches only the SQL `LIMIT`. *A count nobody can reproduce from a rule is a count that
> will keep moving.*

**Two revision-1 errors are also corrected here.** *(a)* Half 1 keyed on `func With…` with the size as the
**first** parameter, while Spec 016 §3 states the invariant over what the size **bounds** — so an exported
*positional* sizing parameter was invisible to both the census and the gate. One exists today:
`resilience.NewTokenBucket(rps float64, burst int, …)` (round-1 **M-5**). Scanning any position costs one
conformance row — the 17th. *(b)* Half 2 was behavioral for 4 keys of 16 and an inert reason-string for the other
12, which contradicts this decision's own claim that it *"cannot be satisfied by a guard that does not guard"*
(round-1 **M-4**). Every key is constructible, so every row is executable — which also gives the vacuity probe
**19** plantable rows instead of 4, and turns half 2 into a real element-type tripwire.

**Accepted limitations, stated because Plan 028 was burned by an unstated one:**

- The AST half sees all 8 modules; the behavioral half is a root-module test and can only import root-module
  packages. All 17 keys live in root-module packages today, so both halves cover all of them. A sizing option
  added to `expr`, `sqlite` or another leaf module **fails half 1 and cannot be added to half 2** — a deliberate
  gate failure demanding a spec revision, not a silent pass.
- **Exported methods are outside half 1 by the `Recv == nil` decision above.** Under D-AB's criterion that
  excludes exactly **two** class members, **both covered** by manual conformance rows — so the boundary limits
  *discovery*, not *coverage*, and there is no uncovered residue.
- **`time.Duration` parameters are outside the gate BY CONSTRUCTION** — the AST sees `time.Duration`, not
  `int64`. **This boundary is deliberate, not an oversight.** The five non-test `NewTicker`/`NewTimer` sites do
  each guard their duration (Spec 016 §3.7.4 lists them), **but that is not the whole class**: a further five
  non-test `clock.After(d)` sites also consume a `time.Duration`, and one of them is fed by the saturated
  `b.interval` in question. **Do NOT claim `time.Duration` is "currently safe, checked"** — only that it is
  outside the gate by construction. *(Round-3 **m3-2** falsified the stronger claim; revision 4 narrowed it in the
  spec and the plan and left it stale here; round-4 **M4-1**.)*

  > **Revision 2 asserted a false fact here.** It stated that `NewTokenBucket`'s
  > `time.Duration(float64(time.Second) / rps)` *"overflows negative for a sub-normal `rps`"* — promoted from
  > round 1's *suspected* to an asserted fact in two documents **without running it**. Measured on darwin/arm64,
  > the project's reference platform, it saturates to **+MaxInt64 (~292 years)**, not negative; out-of-range
  > float→int conversion is implementation-defined, and amd64 would give `-MaxInt64-1`. The real failure mode is a
  > `Wait` that blocks ~292 years, not an overflow. Corrected in Spec 016 §3.7.4; the adjacent defect still
  > belongs to its own increment.
- Vacuity probes must be planted in `adapter/http`, **not** in root: Plan 028's `apidiff` blindness survived
  Task 0 precisely because its probe was planted in root, proving the gate *fires* without proving it *covers*.

### D-AB — class membership is decided by a stated CRITERION, and the byte-ceiling remedy is deferred

**New in revision 4** (round-3 **BLOCKER-1**), and it exists because the same defect returned three times: rounds
1 and 3 both found a partition asserted "with no residual" that was not complete. The root cause was never the
census effort — it was that §2.1's **verdict strings were not discriminators**. The *same* string was true for one
row and false for its sibling:

| Pair | Same verdict string | Reality |
|---|---|---|
| `WithMaxGroups` vs `WithCompletionSize` | `len(x) >= n` comparison | one certified **DEFECTIVE**, the other *"safe — comparison only"* |
| `WithMaxPayloadBytes` vs `WithMaxBodyBytes` | *"limit, never allocated"* | one tests an **already-materialised** slice; the other is the sole bound on an `io.ReadAll` |
| `WithBreakerThreshold` vs `WithCompletionSize` | *"comparison only"* | one compares a **scalar counter**; the other gates an **accumulating group** |

**The criterion.** A knob is a **class member iff `n` is the sole bound on an accumulation.** It is safe iff (a)
it is a pure comparison over a scalar counter, or (b) the check applies to an **already-materialised** value.

> **Cause (c) — *"the structure evicts at the cap for any `n`"* — is DELETED in revision 5** (round-4
> **BLOCKER-1**). It was **circular**: the ring's cap *is* `n`, so "bounded by the ring, not by trusting `n`"
> asserts the thing in question. It had exactly one member, `msghttp.WithReplayBuffer`, now reclassified as a
> class member with a ceiling. The letter is kept as a tombstone so (a) and (b) keep their identifiers.
>
> **Cause (d) — *"it bounds CPU/streaming, not retention"* — is EMPTIED in revision 6** (round-5 **BLOCKER-1**).
> Its one member, `msghttp.WithMaxResponseBytes`, was certified safe on `drainBounded` — **five of six** reads of
> the field. The sixth, `exchange.go:130-131`, is `io.ReadAll(io.LimitReader(resp.Body, max))`, **retained** as
> the reply payload. It joins the deferred byte class.
>
> **Two of the four safety causes have now been emptied in consecutive revisions**, each by the criterion they
> were written alongside, and **neither emptying moved the 16/17 totals**. The operative rule is therefore
> **re-derive every row FROM the criterion; never read the verdict column** — a count-check cannot substitute.
>
> **The lesson is bigger than the row.** D-AB was introduced *specifically* to stop the census moving — and
> revision 4 then kept `WithReplayBuffer`'s pre-existing "safe" verdict and **invented a cause to justify it**
> rather than testing the row against the new rule. **A criterion only helps if every row is re-derived from it,
> including the rows you are not changing.** §2.1 records which
cause applies per row, and the same criterion decides method membership for D-AA.

**Why a criterion rather than a longer list:** a list is re-derived by hand every revision and has been wrong every
time. A criterion is checkable against a row in one step, and it is what AC-5's gate encodes.

**The remedy splits by KIND — the user's decision, taken 2026-08-21 over two alternatives** (extend uniformly to
all ten; or narrow the contract and defer all three):

- **`routing.WithCompletionSize` gets a ceiling here** (D-Z, `1 << 16` group members). Its unit is *members*,
  which the library can reason about — the same kind of statement as *messages*, *connections* or *goroutines*.
- **`msghttp.WithMaxBodyBytes`, `msghttp.WithMaxEventBytes` and — added in revision 6 —
  `msghttp.WithMaxResponseBytes` get a corrected verdict, a godoc hazard disclosure, and a DEFERRED ceiling**
  (Spec 016 §3.8). None is certified safe — that false verdict is precisely what both BLOCKER-1s were about, and
  it would have made AC-5 ship a gate asserting an unbounded remote-driven read is conformant.

**Why the byte knobs are genuinely different, not just deprioritised.** CLAUDE.md's Sensible-defaults gate names
this case verbatim: *"If **no** value can be safe for an unknown caller (e.g. **a byte cap that depends on the
caller's legitimate payload size**), make it explicit/opt-in with a clear typed error or documented off state
rather than guessing a default."* A ceiling in messages is a statement about what the knob means; a ceiling in
**bytes** is a statement about the caller's payload, which the library cannot know — 1 GiB is absurd for a JSON
API and too small for a file upload. The right remedy is a different shape (an explicit opt-in unbounded state),
and choosing it needs its own brainstorm.

**What the follow-up actually inherits — revision 4 recorded this WRONG** (round-4 **M4-2**). It claimed
`maxBody()` back-fills the default for `n <= 0`, so *"`WithMaxBodyBytes(-1)` today means use 1 MiB"* and *"there
is no off-state to document"*. **Both halves are false, measured:** `NewConfig` **rejects** an explicit
non-positive value (`options.go:1128-1131` → `ErrInvalidMaxBodyBytes`); the back-fill fires only for a hand-built
`*Config` that skipped `NewConfig`, as the code's own comment says. So both byte knobs already reject `n <= 0`
with a typed error, and **leaving the option unset already IS the documented default state**. The follow-up's
question is therefore **"should an explicit off-state exist at all, and if so which sentinel value carries it"** —
noting a negative `n` is already taken by the rejection. Tracked as `docs/HANDOVER.md` §8 backlog item 6.

## Consequences

**Good.**
- The headline contract of Plan 028 — *no constructor panics on caller input* — stops being false for four knobs,
  including the two that panicked in `Run` and in an HTTP handler rather than at construction; and three
  bounded-but-unbounded structures regain their bound.
- Zero net exported-surface change: no new sentinel, no signature change, no new option. `apidiff` on root should
  report 0/0, and the exported-surface AST diff (the check that actually covers non-root packages) identical.
- The safe knobs stop being *accidentally* safe: AC-4 pins the zero-size-element property, and D-AA's executable
  half 2 makes every safe row a tripwire rather than a comment.
- D-Y closes a real hole D-U opened, which the Plan 028 backlog recorded but could not fix in scope.
- Folding in `WithBuffer(-1)` (Spec 016 §3.6) removes the last asymmetric sizing knob and makes
  `ErrInvalidCapacity` mean "out of range" uniformly at all **four** producers.
- **D-AB replaces a hand-maintained list with a checkable rule.** Three of four audit rounds found a census hole;
  the criterion is what stops the fourth from finding a fifth.

**Bad, accepted.**
- **Nine policy numbers now live in the API contract.** Lowering any later is a breaking behavioral change.
  Mitigated by setting each 64×–4096× above its default, so no plausible workload meets one.
- **A caller with a genuinely enormous, correct workload is blocked** with no override (D-W). We judge this
  hypothetical relative to nine reproduced defects; if a real consumer ever hits a ceiling, raising it is a minor,
  non-breaking change.
- **The ceilings cannot be derived, only asserted.** A reader who wants to know *why* `1 << 20` gets a rationale,
  not a proof. That is inherent to D-W and is the price of not using a number that would admit the fatal band.
- **`memory.Option` is exported and takes the live `*Broker`**, so `WithBuffer(1<<62)(b)` is a legal call on an
  already-running `Broker` — which after D-Y also writes `b.err` from an arbitrary goroutine while `Send`/`Stream`
  read it unsynchronised. **Round 2 resolved this carried-forward item: the answer is "widen", and no new race
  class is created** (round-2 **m2-6**). Concretely, at `adapter/memory/memory.go:16-23`:
  - the comment scopes the latch to *"the **first nil ELEMENT of opts**"*, which is now incomplete — after this
    increment the same field also latches a **sizing** fault, so the sentence is **widened to name both sources**;
  - the escape hazard itself is **already covered verbatim** — the comment already says *"a hostile option could
    stash `b` and read `err` concurrently with `New`'s write"* — and `WithBuffer` **already** writes `b.ch` on a
    live `*Broker` today. So this increment escalates the *consequence* (a `Permanent` error from `Send`/`Stream`)
    without creating a new race class, and that sentence needs no change.

  Still not proven reachable from any shipped path. Commit `4ce4d84` scoped the lock-free justification to *"as
  long as no caller-supplied `Option` lets the live `*Broker` escape `New`"*, and that scoping remains accurate.
