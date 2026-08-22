# Spec 017 — A message group's member count is bounded at the store, not at the release decision

- **Status:** **DRAFT — revision 1, pre-audit, NOT approved for implementation.** Written before any code, per
  [CLAUDE.md](../../CLAUDE.md)'s design-time gate. The adversarial design audit (spec + ADR + plan together) has
  **not** run; two rounds are this project's established norm.
  - 🔴 **The decisions recorded here were taken WITHOUT USER RATIFICATION.** The user was away when this bundle was
    drafted. Every decision in [ADR 0033](../adrs/0033-group-member-bounds.md) (**D-AC** … **D-AL**) is **open to
    reversal**, and §8 lists the four that most deserve a second look before any code is written.
  - Every structural claim below was re-derived against the tree at `2b2dec1` with the command or file read shown
    next to it, per the project's standing lesson *"verify structural claims against code."* Two claims inherited
    from the increment's framing were found **imprecise** and are corrected in place (§1.4, §2.1).
- **Realized by:** [Plan 031](../plans/031-group-member-bounds.md).
- **Decided by:** [ADR 0033](../adrs/0033-group-member-bounds.md).
- **Builds on:** [Spec 016](016-sizing-option-bounds.md) — this spec is the **direct successor to that defect
  class**. It reuses §2.1's membership criterion, §3.4's ceiling reasoning and §3.5's sentinel-reuse rule verbatim
  rather than re-arguing them, and it fixes the hole Spec 016 left open **by construction** (its class is expressed
  over `int`-typed *options*; this one is not an option at all). Also
  [ADR 0032](../adrs/0032-sizing-option-bounds.md) **D-Z** (the ceiling values) and **D-AB** (the criterion).
- **Origin:** [`docs/HANDOVER.md`](../HANDOVER.md) §6 backlog item **7**, filed at Plan 029's delivery gate:
  *"`routing.WithReleaseWhen` reaches the same unbounded per-group growth that `WithCompletionSize`'s ceiling was
  added to stop … Being func-typed, it is structurally invisible to the class gate."* That entry names one option;
  §1.1 below finds **three**, and §1.3 finds a **second store** with the same gap.

## 1. The gap

**`routing.WithCompletionSize` is the only bounded release path, and it is one of four.** Plan 029 gave it a
ceiling of 65,536 group members ([Spec 016 §3.4](016-sizing-option-bounds.md), ADR 0032 **D-Z**) because a huge
completion size lets a single correlation key accumulate members without limit. That ceiling is real and it is
enforced — and it guards **one** of the four ways an `Aggregator` decides a group is complete.

### 1.1 Four release paths, one bound

Re-derived from `routing/aggregator.go` at `2b2dec1`:

| # | Release path | Site | Bounded today? | Why |
|---|---|---|---|---|
| 1 | `WithCompletionSize(n)` | `aggregator.go:154` | **YES** | `NewAggregator` runs `checkRange(msgin.ErrInvalidCapacity, "routing.WithCompletionSize", cfg.completionSize, 1, completionSizeCeiling)` at `aggregator.go:353-358`, with `completionSizeCeiling = 1 << 16` at `aggregator.go:33` |
| 2 | `WithReleaseStrategy(fn)` | `aggregator.go:116` | **NO** | `fn` is caller-supplied and opaque; `func(c *aggregatorConfig) { c.release = fn }` is the whole body |
| 3 | `WithReleaseWhen(fn)` | `aggregator.go:128` | **NO** | sugar over path 2; wraps `fn` to return a nil error |
| 4 | `defaultRelease` (no option at all) | `aggregator.go:222` | **NO** | reads `msgin.HeaderSequenceSize` from the **first member's headers** — the threshold is **DATA**, not code |

**The guard is gated on a flag only path 1 sets.** `aggregatorConfig.completionSizeSet` (`aggregator.go:22`) is
written by exactly one function — `WithCompletionSize` (`aggregator.go:156`,
`c.completionSize, c.completionSizeSet = n, true`) — and read by exactly one — `NewAggregator`
(`aggregator.go:353`). Paths 2, 3 and 4 never set it, so the range check never runs for them.

Three reproductions, all through the shipped public API, all accepted at construction:

```go
routing.WithReleaseWhen(func(g msgin.MessageGroup) bool { return len(g.Messages()) >= 1<<62 })
routing.WithReleaseStrategy(func(g msgin.MessageGroup) (bool, error) { return len(g.Messages()) >= 1<<62, nil })
// path 4: no option at all — the producer stamps msgin.HeaderSequenceSize = 1<<62 on the first member
```

**Path 4 is the worst of the three, and it is the default.** `defaultRelease` reads the release threshold out of a
**message header**, so the bound on an aggregation group is set by whatever upstream produced the message. A
splitter, a remote peer, or a corrupted header decides how much memory one correlation key may consume. No amount
of option validation can reach it, because there is no option.

> **This is why no bound expressed at the release decision can ever be complete.** Paths 2 and 3 hand the decision
> to a closure the library cannot inspect; path 4 hands it to data. The only site that observes **every** member
> that joins a group is the **store**.

### 1.2 The accumulation site is unbounded — measured

`memory.GroupStore.Add` (`adapter/memory/groupstore.go:117-136`) has **one** admission check, and it is for **new
keys only**:

```go
g, ok := s.groups[key]
if !ok {
    if len(s.groups) >= s.maxGroups {        // :122-124 — the ONLY admission check
        return nil, msgin.ErrOverflowDropped
    }
    g = &groupState{ids: make(map[string]struct{}), createdAt: s.clock.Now()}
    s.groups[key] = g
}
if id := msg.ID(); id != "" {
    if _, seen := g.ids[id]; seen {
        return snapshot{key: key, msgs: slices.Clone(g.msgs[g.claimedLen:]), createdAt: g.createdAt}, nil  // :130
    }
    g.ids[id] = struct{}{}
}
g.msgs = append(g.msgs, msg)                 // :134 — NO per-group cap
return snapshot{key: key, msgs: slices.Clone(g.msgs[g.claimedLen:]), createdAt: g.createdAt}, nil  // :135
```

**`WithMaxGroups` bounds the NUMBER of groups; nothing bounds the members inside one.** Its own godoc
(`groupstore.go:64-72`) says so — *"bounds the number of concurrently held (incomplete) groups"* — and Spec 016
§1.4 already recorded the consequence: *"`WithMaxGroups` caps the number of groups, not members per group."*

**The per-group cost is quadratic in time, not linear in bytes.** `Add` calls `slices.Clone` on **every** call
(lines 130 and 135), so growing a group to *m* members clones Θ(*m*²) message headers. Spec 016 §1.4's measurement,
one correlation key, realistic `msgin.New` messages, `TotalAlloc` cumulative / `HeapAlloc` live with an explicit
`runtime.GC()` before both reads:

| members | elapsed | cumulative alloc | live (GC'd) |
|---|---|---|---|
| `1<<12` | 50 ms | 206.7 MiB | 2.0 MiB |
| `1<<14` | 644 ms | 3,143.5 MiB | 7.8 MiB |
| 60,000 | — | 41,474.0 MiB | 28.7 MiB |
| `1<<16` | **8.6 s** | **49,451.7 MiB (48.3 GiB)** | **31.0 MiB** |

**Do not re-derive these figures for this increment — cite them.** They are Spec 016's, measured under its stated
protocol, and reproducing them costs 8.6 s and 48.3 GiB of churn per run. §6 AC-6 forbids any test in this
increment from growing a group to the ceiling for exactly that reason.

**Growth is monotonic for a group that never releases.** `SettleGroup` (`groupstore.go:159-186`) trims `g.msgs` to
the residual and `delete(s.groups, claim.Key())` when the residual is empty (`:172-175`) — but it runs only after a
successful release. The only other exit is the reaper, and the reaper is **opt-in**: `Aggregator.reapInterval`
returns `cfg.timeout`, `memory.GroupStore.RecoverInterval()` returns `0` (`groupstore.go:219`), so with no
`WithGroupTimeout` the interval is `0` and `Aggregator.Run` blocks on `ctx.Done()` without ever sweeping
(Spec 016 §1.4, re-verified). **A group whose release predicate never fires never settles, never expires, and
never shrinks.**

### 1.3 The SQL group store has the same gap — and no group-count cap at all

`adapter/database/sql.GroupStore` (`adapter/database/sql/groupstore.go:249-280`) is the durable twin. Re-derived:

1. **It has no member cap.** `Add` delegates straight to `GroupDialect.AddMember` and then calls
   `s.decodeGroupRows(rows)` (`groupstore.go:279`).
2. **It has no group-count cap either** — `grep -n 'maxGroups\|MaxGroups' adapter/database/sql/*.go` returns
   **nothing**. `sql.NewGroupStore`'s options are `WithGroupLeaseTTL`, `WithGroupLockedBy` and a logger; there is
   no `WithMaxGroups` equivalent.
3. **Every `Add` materializes the WHOLE live group in the process heap.** The `GroupDialect.AddMember` contract
   (`groupdialect.go:108-126`) says the dialect *"finally SELECTs the group's current CreatedAt plus its LIVE
   members (claimed_epoch IS NULL), ordered by seq then msg_id"*, and `decodeGroupRows`
   (`groupstore.go:270-280`) then does `make([]msgin.Message[any], 0, len(rows.Members))` plus a `DecodeHeaders`
   per member.

**So the SQL store is quadratic too, and worse in kind.** Memory clones message *headers*; SQL round-trips and
decodes **every live member's framed bytes from the database on every single Add**. A 100,000-member group costs
100,000 rows fetched and decoded per arriving member.

**The missing group-count cap is NOT the same defect and is deliberately out of scope** (§3.12). For `memory`, the
group map lives in the process heap, so an unbounded group count is a process-memory lever — which is why
`WithMaxGroups` exists. For `sql`, groups live in the **database**; the process materializes exactly **one** group
per `Add`. An unbounded group count there is a durable-storage sizing concern for the operator, not a process OOM
lever. **Per-group members are different: they are a process-heap lever for `sql` in a way group count is not**,
because the whole live group crosses into the process on every `Add`. That asymmetry is the entire reason this
spec bounds members in both stores while bounding group count in neither beyond what already ships.

### 1.4 The class gate cannot see this class — and the reason is not the one the backlog entry gives

Plan 029 Task 7 shipped `sizing_option_class_gate_test.go`, the gate that stops Spec 016's class from returning
silently. Half 1 walks the AST and collects every exported, `Recv == nil` function with an `int`/`int64` parameter
in any position, via `hasIntOrInt64Param` (`:175`) → `isIntOrInt64` (`:191-202`). Verified green on this tree:

```
$ GOTOOLCHAIN=go1.25.13 go test -run TestSizingOptionClass -v .
    sizing_option_class_gate_test.go:277: === EXPORTED FUNCTIONS (Recv==nil) with int/int64 param in ANY position: 17
    sizing_option_class_gate_test.go:278: === EXPORTED METHODS with int/int64 param: 27 (excluded by the Recv==nil boundary, Spec 016 §2.0)
--- PASS: TestSizingOptionClass_Completeness (0.01s)
--- PASS: TestSizingOptionClass_Conformance (0.00s)
```

**Neither `WithReleaseStrategy` nor `WithReleaseWhen` is visible to it — but by TWO different mechanisms, and the
backlog entry names only one.** `isIntOrInt64`'s type switch handles `*ast.Ident`, `*ast.Ellipsis`,
`*ast.ArrayType` and `*ast.StarExpr`, and returns `false` for everything else:

| Option | Parameter's AST node | Why it is invisible |
|---|---|---|
| `WithReleaseWhen(fn func(msgin.MessageGroup) bool)` | `*ast.FuncType` | falls through the type switch to `return false` — **this is the mechanism the backlog entry describes** |
| `WithReleaseStrategy(fn ReleaseStrategy)` | `*ast.Ident{Name: "ReleaseStrategy"}` | matched as an `*ast.Ident` whose name is neither `"int"` nor `"int64"` — the **named-type** path, the same one the gate's header already states for `type Bytes int64` |

The distinction matters because the gate's header (`:64-91`) enumerates four accepted limitations — the
root-module import boundary, the `Recv == nil` boundary, **named integer types**, and `time.Duration`. The
named-type bullet *partially* covers `WithReleaseStrategy`; **nothing covers `WithReleaseWhen`'s `*ast.FuncType`,
and nothing at all covers "a knob that is not a parameter"** (path 4, the header-driven `defaultRelease`).

**The deeper point, and the reason this spec exists:** Spec 016's class is *"an exported sizing option validating
only its lower bound."* That class is expressed over **options with an integer parameter**. Paths 2, 3 and 4 impose
a size without being one — path 4 is not even a function. **No extension of the AST scan can reach them.** A gate
built on the option surface is structurally incapable of covering a bound that arrives as a closure or as a header
value. That is not a hole in the gate's implementation; it is a boundary of its premise, and §3.1 responds by
moving the enforcement rather than by widening the scan.

## 2. The inventory

### 2.1 The criterion, applied unchanged

ADR 0032 **D-AB** / Spec 016 §2.1 state it, and this spec adopts it verbatim rather than inventing a second rule:

> **A knob is a CLASS MEMBER iff `n` is the sole bound on an accumulation** — that is, memory retained (or
> goroutines spawned) grows with input and nothing but `n` stops it.

Applied to the four release paths, membership is decided by the criterion, not by whether the path has an integer
parameter. **All four size the same accumulation — `groupState.msgs` / the group's live member set.** Path 1 is a
class member that was **fixed** by Plan 029 at the option; paths 2–4 are class members that were **never
reachable** from the option surface.

> **The criterion says nothing about parameter types.** Reading it as *"an `int` option that bounds an
> accumulation"* is the narrowing that let paths 2–4 through Plan 029's five audit rounds. The accumulation is the
> subject of the sentence; `n` is only whatever happens to bound it. **Re-derive membership from the accumulation,
> not from the signature.**

### 2.2 The table

| Path | Kind | Accumulation | Bounded by Plan 029? | Bounded by this spec? |
|---|---|---|---|---|
| `routing.WithCompletionSize` | `int` option | group members | **yes** — `completionSizeCeiling = 1<<16` | unchanged; gains a godoc cross-reference (§3.8) |
| `routing.WithReleaseStrategy` | func-typed option | group members | no — invisible (named type) | **yes, at the store** (§3.1) |
| `routing.WithReleaseWhen` | func-typed option | group members | no — invisible (`*ast.FuncType`) | **yes, at the store** (§3.1) |
| `routing` `defaultRelease` | **no option** — `msgin.HeaderSequenceSize` header | group members | no — not a parameter at all | **yes, at the store** (§3.1) |
| `memory.GroupStore.Add` | the accumulation site | `groupState.msgs` | no — only `WithMaxGroups`, which caps group *count* | **yes** — `memory.WithMaxGroupMembers` (§3.2) |
| `sql.GroupStore.Add` | the accumulation site | the live member set, decoded per `Add` | no — no cap of any kind | **yes** — `sql.WithMaxGroupMembers`, enforced in-transaction (§3.6) |

## 3. The contract

> **No `msgin.MessageGroupStore` may let one correlation group grow without a stated bound, whichever release
> strategy the Aggregator is configured with — including a caller-supplied closure and including no option at
> all.** An `Add` that would take a group past its bound is reported as `msgin.ErrOverflowDropped`, naming the
> site, the group key, the offending count and the limit.

The invariant is stated over **the store**, which observes every append, rather than over **the release decision**,
which is opaque in three of four paths. It is therefore complete by construction and stays complete when a fifth
release path is added.

### 3.1 The bound lives at the ACCUMULATION SITE, not at the release decision

This is the decision the whole increment turns on (ADR 0033 **D-AC**). Three reasons, in order of weight:

1. **Completeness.** §1.1: the release strategy is an opaque caller closure in paths 2 and 3, and a message header
   in path 4. A bound expressed there covers path 1 only — which is exactly the state Plan 029 shipped.
2. **Precedent inside the same file.** `memory.GroupStore`'s *existing* admission check for group **count** already
   lives in `Add` (`groupstore.go:122-124`), not in the aggregator. This spec adds the member check **two lines
   below it**, in the same function, under the same lock, returning the same sentinel. The two arms become
   symmetric rather than one being a special case.
3. **The SPI seam.** `msgin.MessageGroupStore` is a public contract that third parties implement
   (`groupstore.go:37`). A bound stated on the store is a bound every implementation must honour; a bound stated in
   `routing` protects only the stores `routing` happens to ship with (§3.7).

**`routing.WithCompletionSize`'s ceiling is NOT removed.** It stays exactly as Plan 029 delivered it. Two bounds on
the same quantity is not redundancy: the option ceiling is a **construction-time** rejection of a nonsense
configuration (fail at boot, best debuggability), and the store cap is a **runtime** bound on the accumulation
(fail at the boundary, complete coverage). §3.5 works out their interaction exactly.

### 3.2 The two new options

Both follow the established option shape, both reuse `checkRange` + `msgin.ErrInvalidCapacity`, and neither changes
an existing signature.

| Option | Package | Default | Ceiling | Validated in |
|---|---|---|---|---|
| `WithMaxGroupMembers(n int)` | `adapter/memory` | **`1 << 16`** (65,536) | **`1 << 20`** (1,048,576) | `memory.NewGroupStore` |
| `WithMaxGroupMembers(n int)` | `adapter/database/sql` | **`1 << 16`** (65,536) | **`1 << 20`** (1,048,576) | `sql.NewGroupStore` |

**The default of 65,536 REUSES a ratified value; it is not a new judgement.** Spec 016 §3.4 / ADR 0032 **D-Z**
fixed `completionSizeCeiling = 1 << 16` on the stated ground that 65,536 group members is *"far beyond any
plausible aggregation"*, and that row survived attack in **all five** of Plan 029's audit rounds. The unit here is
identical — members of one correlation group — so the reasoning transfers without re-argument. §3.9 stress-tests
whether a *ceiling*'s reference value may legitimately become a *default*.

**The ceiling of `1 << 20` matches `maxGroupsCeiling`** (`adapter/memory/groupstore.go:62`), the sibling bound in
the same struct, so a reader comparing `WithMaxGroups` and `WithMaxGroupMembers` sees one number for "the largest
in-flight aggregation quantity this library will accept."

**Validation is the shipped helper, unchanged:**

```go
if err := checkRange(msgin.ErrInvalidCapacity, "memory.WithMaxGroupMembers",
    cfg.maxGroupMembers, 1, maxGroupMembersCeiling); err != nil {
    return nil, err
}
// memory.NewGroupStore(memory.WithMaxGroupMembers(0))
//   → "msgin: capacity out of range: memory.WithMaxGroupMembers: 0 not in [1, 1048576]"
```

`checkRange` exists in `adapter/memory/helpers.go:54` and follows ADR 0031 **D-R**'s four-independent-copies
precedent; `adapter/database/sql` does **not** have one today (`grep -rn 'func checkRange'` finds four copies, in
`endpoint`, `routing`, `adapter/memory`, `adapter/http`) and gains a fifth, unexported, identical copy.

**This makes `msgin.ErrInvalidCapacity` a SIXTH producer.** Spec 016 §3.5 counted four and warned: *"a fifth
producer should be a conscious decision rather than a default."* This increment adds two at once. The decision is
conscious and recorded as ADR 0033 **D-AD**: the alternative — minting `memory.ErrInvalidGroupMembers` and
`sql.ErrInvalidGroupMembers` — adds two exported sentinels to a pre-v1 surface we are keeping small, and splits a
caller's *"this size is wrong"* branch across four `errors.Is` targets. The generic sentinel text
(*"msgin: capacity out of range"*) plus the site name in the wrap is what makes six producers tolerable, exactly as
it made four tolerable. **A seventh needs its own ADR.**

### 3.3 The overflow report

An `Add` that would exceed the cap returns **`msgin.ErrOverflowDropped`** — the same sentinel as the group-count
arm two lines above it (`groupstore.go:123`), so the two overflow arms are symmetric (ADR 0033 **D-AE**).

**It is wrapped, where the existing arm is bare, and that asymmetry is deliberate.** `groupstore.go:123` reads
`return nil, msgin.ErrOverflowDropped` with no context. The new arm renders:

```go
fmt.Errorf("%w: %s: group %q holds %d members, limit %d",
    msgin.ErrOverflowDropped, "memory.GroupStore.Add", key, len(g.msgs), s.maxGroupMembers)
// → msgin: message dropped by overflow policy: memory.GroupStore.Add: group "order-42" holds 65536 members, limit 65536
```

Two reasons the wrap wins over literal symmetry: debuggability is [CLAUDE.md](../../CLAUDE.md)'s stated **core**
quality criterion (*"prefer typed, wrapping errors that name the offending field/input"*), and — decisively —
`msgin.ErrOverflowDropped` now has **three** producers in `memory` alone (`queuestore.go:170`, `:175`,
`groupstore.go:123`) plus a fourth in `endpoint/consumer.go:576`, so a bare sentinel cannot tell an operator which
cap fired. **The existing bare arm is upgraded to the same shape in the same commit** — fix the class, not the
instance (§4).

**The error is NOT `Permanent`-wrapped, and this is a decision, not an omission.** Verified: `IsPermanent`
(`reliability.go:35-46`) matches `*permanentError`, `ErrPayloadType`, `ErrPayloadDecode` and `ErrPayloadTooLarge` —
**`ErrOverflowDropped` is not among them**, so it classifies as **transient** today, and the new arm keeps that.
`Aggregator.Handle` (`aggregator.go:412-415`) returns `store.Add`'s error unchanged, so the fault travels the
runtime's ordinary `RetryPolicy`: retry with backoff, then dead-letter on exhaustion.

**Transient is the right classification, for a reason that is true of both arms:** an over-cap `Add` **can** succeed
later — when the group releases and `SettleGroup` deletes the claimed prefix, or when the reaper expires it. That is
precisely the "retry might work" property `Permanent` denies. For a group whose release genuinely never fires, the
retry budget exhausts and the message dead-letters with a cause naming the group and the limit — **loud, bounded,
and diagnosable**, which is the outcome §3.11 argues for.

**Rejected: silently dropping, evicting the oldest member, or force-releasing at the cap.** All three are in §5.

### 3.4 What the cap counts

**`memory` counts `len(g.msgs)` — live PLUS claimed** (ADR 0033 **D-AF**), because that slice is what the process
retains. A claimed-but-unsettled member is still in the heap; a cap that ignored it would let a group hold up to
2× the cap across a claim boundary.

**`sql` counts the LIVE members only** (`claimed_epoch IS NULL`), because for `sql` the claimed members are
retained by the **database**, not by the process, and the quantity this spec bounds for `sql` is *"what one `Add`
drags into the process heap"* (§1.3).

> 🔴 **This is an asymmetry between the two stores, and it is the finding in this spec most likely to be reversed
> by the audit.** The uniform alternative — both stores count live members only — makes the SPI contract one
> sentence instead of two, at the price of letting `memory` retain up to 2× the cap transiently. The asymmetry is
> defensible (each store bounds *what it actually retains*), but a reader comparing the two godocs will see two
> different rules for one option name. §3.7 states the SPI contract in a form that admits both, and §8 lists this
> as open.

### 3.5 The boundary interaction with `WithCompletionSize` — the exact arithmetic

**Question:** with the default cap at 65,536 and `WithCompletionSize(1<<16)` also 65,536, does release fire before
the cap rejects?

**Answer: yes, exactly — and the boundary is exactly attainable, with zero margin.**

The check is placed **before the append**, so:

```go
if len(g.msgs) >= s.maxGroupMembers {   // ← BEFORE the append at :134
    return nil, fmt.Errorf(...)
}
g.msgs = append(g.msgs, msg)
```

Let `C` be the cap. The check rejects when the group already holds `C`, so the **maximum attainable**
`len(g.msgs)` is exactly `C` — the `C`-th `Add` sees `len == C-1 < C`, appends, and returns a snapshot of `C`
members. `WithCompletionSize(N)` releases when `len(snapshot.Messages()) >= N`, and for an unclaimed group the
snapshot is `g.msgs[0:]`, i.e. all `C`. So with `N = C = 65,536`:

| `Add` # | `len(g.msgs)` before check | Check | `len` after append | Release predicate `>= 65536` |
|---|---|---|---|---|
| 65,535 | 65,534 | pass | 65,535 | false |
| **65,536** | **65,535** | **pass** | **65,536** | **TRUE — releases** |
| 65,537 | 65,536 | **reject** — but unreachable: the group settled at 65,536 | — | — |

**The invariant this rests on, stated so it can be checked:** the default `maxGroupMembers` must be **≥
`completionSizeCeiling`**, or a caller could configure a legal `WithCompletionSize` that the default cap makes
permanently unreachable — a silent deadlock instead of a bound. Both are `1 << 16` today. They live in **different
packages** (`routing/aggregator.go:33` and `adapter/memory/groupstore.go`) and both are **unexported**, so a
blackbox test cannot compare them directly. §6 AC-3 states how the invariant is defended without a 48.3 GiB test,
and §8 records the residual drift risk honestly rather than claiming it is closed.

**The claim-window interaction, which the naive analysis misses.** `memory` counts live + claimed (§3.4), and
`ClaimGroup` sets `g.claimedLen = len(g.msgs)` without shrinking `g.msgs` — the trim happens in `SettleGroup`
(`groupstore.go:172-175`). So between `ClaimGroup` and `SettleGroup`, a group sitting at exactly `C` **rejects new
arrivals for the same key**, even though its live residual is empty. That window is:

- **Bounded** — `Aggregator.release` settles or abandons the claim on every path, including a panic-safe
  defer-abandon.
- **Recoverable** — the rejection is `ErrOverflowDropped`, which §3.3 classifies **transient**, so the runtime
  retries and the retry succeeds once the settle lands.
- **An accepted consequence, not a defect** — and it is stated on the option's godoc (§4) so it is not
  rediscovered as a bug report.

### 3.6 The SQL enforcement point — in the dialect's transaction, not in the store

**`sql.GroupStore` cannot enforce the bound the way `memory` does, and pretending otherwise would ship a bound that
does not bound.** `Add` (`groupstore.go:249-280`) delegates to `GroupDialect.AddMember`, which — per its own
contract (`groupdialect.go:108-126`) — **commits the member row and then returns the whole live member set** in one
transaction. By the time the store can count anything, the row is durable and its bytes are in the process heap.

Three enforcement points were considered; the plan implements the third (ADR 0033 **D-AG**):

| Option | Bounds the durable table? | Bounds the raw fetch? | Bounds the decode? | SPI change |
|---|---|---|---|---|
| (A) count `len(rows.Members)` in `GroupStore.Add`, after `AddMember` returns | **no** — the row is committed | **no** — `[]MemberRow` is already materialized | yes | none |
| (B) as (A), plus a `LIMIT max+1` on the dialect's member SELECT | no | yes | yes | `AddMember` signature |
| **(C) count INSIDE the dialect's transaction and roll back — CHOSEN** | **yes** | **yes** | **yes** | `AddMember` signature |

**(A) is a bound that halves the peak and fixes nothing.** `MemberRow` carries the full framed payload bytes for
every live member, so the raw fetch is already the dominant cost; skipping the decode saves roughly a copy. A
remedy that leaves the actual lever in place while reading as "bounded" is the false-safety inversion Spec 016 §1.1
and §3.8 both warn about.

**(C) is exact, and the existing dialect code is already shaped for it.** Every dialect's `AddMember` runs one
transaction that **takes the group row lock first** (`postgres/groupdialect.go:105-112`: an
`INSERT … ON CONFLICT DO UPDATE … RETURNING`, whose comment states it *"serializes same-key adds (H1)"*), then
upserts the member, then SELECTs the live members. Adding a member cap is: pass `maxMembers` in, and after the
member upsert, if the live count exceeds it, return the overflow error — the existing `pgRunInTx` wrapper rolls the
transaction back, so **nothing is committed**. The group row lock makes the check atomic **across processes**,
which is what §7 needs.

**The SPI change is breaking, and that is affordable exactly once.**
`AddMember(ctx, q, table, groupKey, msgID string, seq int64, headers, payload []byte)` gains a trailing
`maxMembers int`. `GroupDialect` is documented in its own godoc as *"a pre-1.0 (v0) contract that may still
evolve"* (`groupdialect.go:106`), the project is **unreleased with no tags and no consumers**
([CLAUDE.md](../../CLAUDE.md) project status), and the change reaches five call sites the plan enumerates:
`postgres`, `mysql`, `sqlite`, `harness/groupstore.go:345`, and `groupdialect_fake_test.go:137`.

**The dialects return `msgin.ErrOverflowDropped` directly**, wrapped in §3.3's shape. No new exported sentinel
(ADR 0032 **D-X**, *"mint none"*). The three dialect modules already depend on the `msgin` root module transitively
through `msginsql`, so importing `msgin` adds **zero** net dependency — `postgres/groupdialect.go` imports only
`msginsql` today, and the plan verifies `go mod tidy` leaves each dialect's `go.mod` unchanged.

### 3.7 The SPI states the bound as a contract requirement

`msgin.MessageGroupStore.Add`'s godoc (`groupstore.go:41-52`) gains a stated requirement, so a third-party store —
a future Redis, NATS or pgx group store — implements the bound rather than rediscovering the defect
(ADR 0033 **D-AH**):

> An implementation MUST bound the number of members it retains for a single group, and MUST report an `Add` that
> would exceed that bound as `msgin.ErrOverflowDropped` rather than growing without limit. The Aggregator's release
> strategy cannot supply this bound: three of its four paths are a caller-supplied closure or a message header, so
> the store is the only site that observes every member. The exact set counted is implementation-specific and MUST
> be stated in the implementation's godoc — `adapter/memory` counts retained members (live + claimed);
> `adapter/database/sql` counts live members, because claimed members are retained by the database rather than by
> the process.

**This is a contract addition to a shipped SPI, and it is not enforced by the compiler.** A third-party store that
ignores it still compiles. §6 AC-7 states what the increment does instead: both first-party stores get a
conformance case, and the `sql` conformance kit (`harness`) gains one so every dialect is held to it.

### 3.8 Godoc cross-references — closing the inference gap

Three edits (ADR 0033 **D-AI**), each one or two sentences, no logic:

| Site | Addition |
|---|---|
| `routing.WithCompletionSize` (`aggregator.go:141-153`) | a pointer to the store-level bound: the option ceiling is a construction-time check on *this* option, and the store's `WithMaxGroupMembers` is what bounds the group whatever strategy is in force |
| `routing.WithReleaseStrategy` (`aggregator.go:100-115`) | **it bypasses `completionSizeCeiling` entirely** — a closure that never returns true grows the group until the store's member cap rejects the next `Add` with `ErrOverflowDropped` |
| `routing.WithReleaseWhen` (`aggregator.go:120-127`) | the same note; it is sugar over `WithReleaseStrategy` and inherits the bypass |

**`defaultRelease` needs the same disclosure and has no exported godoc to carry it.** It is unexported
(`aggregator.go:222`), so the note goes on **`msgin.HeaderSequenceSize`**'s declaration in root and on
`routing.NewAggregator`'s godoc, which is where a caller relying on the default release path actually looks.

### 3.9 Is a DEFAULT cap legitimate at all? — argued both ways, then decided

[CLAUDE.md](../../CLAUDE.md)'s Sensible-defaults gate contains an escape clause that this increment must clear:

> *"If **no** value can be safe for an unknown caller (e.g. a byte cap that depends on the caller's legitimate
> payload size), make it **explicit/opt-in** with a clear typed error or documented off state rather than guessing a
> default that lulls the caller into a false guarantee."*

Spec 016 §3.8 invoked exactly this clause to **defer** three byte knobs. So the question is real: is a member cap
more like a byte cap (defer, opt-in) or like `WithMaxGroups` (default, ship it)?

**The case AGAINST a default — stated at its strongest, not as a strawman.** Spec 016 §3.4's own words are
*"None of these is reachable by a correct program. They are the boundary between a workload and a typo."* That
sentence justifies 65,536 as a **ceiling** — a typo boundary. This spec proposes to reuse the same number as a
**runtime default** — a policy that a correct program is expected to live under. Those are different jobs, and
importing a ratified number from one to the other imports a justification that was never argued for the second job.
A caller aggregating a 200,000-row export into one group is doing something perfectly sensible that this default
breaks at member 65,537.

**The case FOR a default — three disanalogies with the byte case, and a direct answer to the objection.**

1. **The unit is caller-owned and countable, not remote-driven.** A byte cap is unsafe to default because the
   quantity belongs to an unknown peer's payload. A member count belongs to the **caller's own aggregation
   design** — their splitter's `HeaderSequenceSize`, their completion size. The library can reason about it, and
   already has: ADR 0032 **D-Z**, five audit rounds.
2. **"Off" is not a safe state here.** For the byte knobs, leaving the option unset already **is** the documented,
   safe default (Spec 016 §3.8, measured). For per-group members, the current unset state is **unbounded** — it is
   the defect. An opt-in-only bound protects only callers who already know about the problem, which is the
   fix-the-instance pattern this project's stored lessons name repeatedly.
3. **A larger value is available and safe.** The byte case has no larger safe value to escape to — that is why it
   was deferred. Here the escape hatch is real: `WithMaxGroupMembers` accepts up to `1 << 20`, sixteen times the
   default, and the ceiling itself is a stated contract with a typed error.

**Answering the objection directly, because it is the strongest one.** The ceiling-to-default transfer is valid
*a fortiori*: if no correct program reaches 65,536 members **via `WithCompletionSize`** — the ratified claim — then
no correct program reaches 65,536 members **at all**, because the quantity and the unit are identical and only the
enforcement site differs. A group does not become a different object because its release threshold arrived as a
closure. What changes is the **failure mode** at the boundary, and this spec makes that failure loud, typed,
retryable and named (§3.3) rather than a silent guess.

**DECIDED: ship the default (ADR 0033 D-AJ).** The 200,000-member caller sets `WithMaxGroupMembers(1<<18)` and
reads a godoc sentence explaining the trade-off. The alternative leaves every caller who has not read this spec
running the unbounded version.

### 3.10 The behavioral break, stated

**A caller aggregating more than 65,536 members into one correlation group changes behavior**: today the group
grows (quadratically, §1.2); after this increment member 65,537 returns `ErrOverflowDropped` and travels the retry
→ dead-letter path.

The project is **pre-v1, unreleased, with no tags and no consumers**, so breaking changes are free
([CLAUDE.md](../../CLAUDE.md) project status; the stored lesson *"no tag until released"*). **Free is not the same
as unstated** — it is recorded here, on both options' godoc (§4), and in the plan's final task, so nobody discovers
it from a dead-letter queue.

**The `GroupDialect.AddMember` signature change (§3.6) is the second break**, and it is a **compile-time** break
for any out-of-tree dialect. There are none: `grep -rn 'msginsql.GroupDialect'` finds only the three first-party
dialects, the harness and a test fake.

### 3.11 Liveness versus boundedness — stated, not implied

**With the cap in place, a group whose release never fires is memory-BOUNDED but permanently STUCK.** It holds
exactly `maxGroupMembers` members forever; every subsequent member for that key is rejected, retried, and
dead-lettered. It is never released, never settled, never garbage.

**This is accepted (ADR 0033 D-AK), and it is a strict improvement on both axes that matter:**

| | Before | After |
|---|---|---|
| Process memory | unbounded — grows until OOM or 48.3 GiB of churn | bounded at `maxGroupMembers` members |
| Observability | silent, until the process dies | one typed, named `ErrOverflowDropped` per rejected member, into the operator's dead-letter store |
| Liveness of the stuck group | never releases | **still never releases** |

**The cap does not, and is not intended to, provide liveness.** The remedy for a stuck group is the one that
already ships: `routing.WithGroupTimeout` + `routing.WithExpiredGroupChannel`, which expires the group and routes
its partial members to a channel the caller owns. That remains **opt-in**.

**Rejected: making `WithGroupTimeout` mandatory.** It would be a second, larger behavioral break; it requires a
paired `WithExpiredGroupChannel` (`NewAggregator` returns `ErrExpiryChannelRequired` otherwise,
`aggregator.go:360-362`), so making it mandatory forces every caller to provision an expiry channel; and choosing a
default timeout for an unknown aggregation workload is the *"no value can be safe for an unknown caller"* case for
real. **Recorded as a follow-up** (§8, backlog): a future increment should consider whether the *combination* of a
member cap and no timeout deserves a construction-time warning or a documented recommendation.

### 3.12 What this spec does not change

1. **No release path is removed or restricted.** `WithReleaseStrategy`, `WithReleaseWhen` and the header-driven
   default keep working exactly as they do; they simply stop being able to grow a group without limit.
2. **`completionSizeCeiling` is unchanged** at `1 << 16`, and `WithCompletionSize`'s validation is unchanged.
3. **`memory.WithMaxGroups` is unchanged** — group **count** and group **members** are different quantities with
   different ceilings (`1<<20` and `1<<20`) and different defaults (1024 and 65,536).
4. **`sql.GroupStore` does NOT gain a group-count cap** (§1.3): for a durable store the group count is a
   database-sizing concern, not a process-heap lever. Recorded as a named non-goal, not an oversight.
5. **`memory.GroupStore.Add`'s quadratic `slices.Clone` is not optimized.** The cap bounds it; it does not make it
   cheap. Even at the cap, growing a group to 65,536 members costs 8.6 s and 48.3 GiB of churn (§1.2). That is a
   performance defect of its own, out of scope here, and named in §8.
6. **The class gate's AST scan is not widened to func-typed parameters.** §1.4 shows why it cannot be made
   complete; §6 AC-8 states the limitation instead of pretending to close it.

## 4. Documentation contract

Every knob and site this spec touches states, on its own godoc: **the range, the default, why that default, and the
typed error a violation produces** — [CLAUDE.md](../../CLAUDE.md)'s Sensible-defaults gate.

Specifically required, each cross-referenced from the plan task that writes it so no edit is orphaned:

1. **`memory.WithMaxGroupMembers` / `sql.WithMaxGroupMembers`** — the range `[1, 1<<20]`, the default 65,536 and
   the Spec 016 §3.4 reasoning it inherits, the `ErrOverflowDropped` behavior at the boundary, **what the cap
   counts** (§3.4 — the two stores differ, and each godoc says which), and — for `memory` — the claim-window
   rejection of §3.5.
2. **`maxGroupMembersCeiling`** (both packages) — a constant godoc in the shape of `maxGroupsCeiling`
   (`groupstore.go:55-62`) and `completionSizeCeiling` (`aggregator.go:25-33`): what the ceiling means, why this
   number, and the cost basis (time, not bytes — §1.2).
3. **`msgin.MessageGroupStore.Add`** — §3.7's contract paragraph.
4. **The three release-path godocs** — §3.8.
5. **`memory.GroupStore.Add`'s existing bare `ErrOverflowDropped`** (`groupstore.go:123`) — upgraded to §3.3's
   wrapped shape **in the same commit**, and its godoc (`groupstore.go:112-116`) updated to name both overflow
   arms rather than only the group-count one.

> **The recurring failure mode this list exists to prevent** — the project's stored lesson *"docs can contradict
> the code they describe"*: all three fix rounds in Plan 028 were godoc, not logic. Read each sentence against the
> constructor, not for plausibility. In particular, `groupstore.go:112-116` currently says *"A new key beyond
> WithMaxGroups returns msgin.ErrOverflowDropped"* — after this increment that sentence is **incomplete**, not
> wrong, and incompleteness in an overflow contract is how the next audit round starts.

## 5. Alternatives rejected

| Alternative | Why rejected |
|---|---|
| **Bound at the release decision** — validate the closure, or cap what a release strategy may return | §1.1: opaque in paths 2 and 3, and path 4 has no code to validate. Incompletable by construction. |
| **Widen the class gate's AST scan to `*ast.FuncType` parameters** | §1.4: catches `WithReleaseWhen`, misses `WithReleaseStrategy` (named type) and cannot express path 4 at all. It would also have to decide *which* func-typed options are sizing knobs — a judgement `go/ast` cannot make. A gate that covers 1 of 3 while reading as complete is worse than a stated limitation. |
| **Force-release the group when it hits the cap** | Emits an incomplete aggregate as if it were complete — silent data corruption dressed as liveness, and it overrides the caller's release contract. Same family as Spec 016 §5's rejected *"clamp silently to the ceiling."* |
| **Evict the oldest member at the cap** | Silent data loss. `WithMaxGroups`'s own godoc already rejects this reasoning for groups: *"rather than evicting a partial group (which would be silent data loss)."* |
| **Drop the over-cap member silently (return nil error)** | The caller's source `Ack`s a message that was never aggregated. Fails the delivery-guarantee invariant. |
| **`Permanent`-wrap `ErrOverflowDropped`** | §3.3: an over-cap `Add` genuinely can succeed after a settle or an expiry, so `Permanent` would deny a retry that works. It would also change the classification of the **existing** group-count arm, a behavioral change to shipped code with no defect behind it. |
| **Mint `memory.ErrInvalidGroupMembers` / `sql.ErrInvalidGroupMembers`** | Two new exported sentinels on a pre-v1 surface we are keeping small; splits *"this size is wrong"* across four `errors.Is` targets. ADR 0032 **D-X** reuse rule, §3.2. |
| **SQL enforcement (A): count after `AddMember` returns** | §3.6: the row is already committed and the raw `[]MemberRow` already materialized. Halves the peak, bounds nothing durable, and reads as a bound. |
| **Opt-in only, no default** | §3.9: leaves every caller who has not read this spec running the unbounded version. The escape clause it invokes applies to quantities the library cannot reason about; this one it can. |
| **Make `WithGroupTimeout` mandatory** | §3.11: a second, larger break; forces an expiry channel on every caller; and no default timeout is safe for an unknown aggregation workload. |
| **Export the ceilings** (`const MaxGroupMembersCeiling`) | Same answer as Spec 016 §5's row: new exported surface, and the wrapped error already names the bound. Revisit if a consumer asks. |

## 6. Verification

Every acceptance criterion below is **executable as written** — the standing bar after Plan 029's audit found an
unexecutable AC in **five consecutive rounds**. Where an AC cannot be executed, it says so and says what is done
instead.

**AC-1 — the bound holds for ALL FOUR release paths.** One table, four cases, each configuring a different release
path over a `memory.GroupStore` built with a **small** cap (`WithMaxGroupMembers(4)`), each asserting that
`Handle`'s `cap+1`-th message returns an error satisfying `errors.Is(err, msgin.ErrOverflowDropped)`:

| Case | Aggregator configuration |
|---|---|
| `WithCompletionSize` far above the cap | `WithCompletionSize(1000)` |
| `WithReleaseStrategy` that never releases | `WithReleaseStrategy(func(msgin.MessageGroup) (bool, error) { return false, nil })` |
| `WithReleaseWhen` that never releases | `WithReleaseWhen(func(msgin.MessageGroup) bool { return false })` |
| the default path, header-driven | no release option; first message carries `msgin.HeaderSequenceSize = 1000` |

**This is the acceptance criterion the whole increment exists for.** A test that covers only `WithMaxGroupMembers`
in isolation would pass against an implementation that bounds nothing new, because path 1 was already bounded.
**Fixture note, measured (Spec 016 §6 AC-5's five-part finding, re-verified):** `NewAggregator` needs
`store`, `fn`, `WithOutputChannel(ch)` **and** `WithCorrelationStrategy(fixedKey)`, plus `ch.Subscribe(...)` for
release to be observable — the default correlator returns `Permanent(ErrNoCorrelation)` for a message with no
correlation header, and a bare `NewAggregator(store, fn)` returns `msgin: aggregator output channel is nil`.

**AC-2 — the ceiling is the boundary, asserted on both sides, for both stores.** Per store, a table with
`WithMaxGroupMembers(1<<20)` (accepted — **constructor only**, no members added) and `WithMaxGroupMembers(1<<20+1)`
(rejected), plus the lower arm `WithMaxGroupMembers(0)`.

**AC-2b — the rendered message is true at both ends.** Per store, two cases asserting the **full** render, not
merely `errors.Is`: the site name, the offending value and the `[1, 1048576]` range — one at `ceiling+1`, one at
`0`. This is Spec 016 AC-2b's rule, and it is the case that would have caught revision 2's `"0 exceeds 1048576"`.

**AC-3 — the `WithCompletionSize(1<<16)` boundary still releases, proven at small `n` and defended at the
ceiling.** §3.5's arithmetic, made executable:

1. **Small-`n` proof of the arithmetic** (the load-bearing case): `WithMaxGroupMembers(4)` +
   `WithCompletionSize(4)` over 5 messages ⇒ the 4th `Handle` **releases** (observed via the output channel's
   subscriber) and the 5th starts a fresh group rather than overflowing. Then `WithMaxGroupMembers(4)` +
   `WithCompletionSize(5)` ⇒ the 5th `Handle` returns `ErrOverflowDropped` and **nothing is released**. The two
   cases together pin the off-by-one in both directions.
2. **Ceiling-level defence, constructor-only:** `routing.NewAggregator(..., WithCompletionSize(1<<16))` and
   `memory.NewGroupStore()` (default cap) both construct without error, and
   `memory.NewGroupStore(WithMaxGroupMembers(1<<16))` does too.

> 🔴 **What AC-3 does NOT do, stated rather than left as a gap.** It does **not** execute a 65,536-member group.
> Spec 016 §1.4 measured that at **8.6 s and 48.3 GiB of allocation churn**, and `completionSizeCeiling`'s shipped
> godoc (`aggregator.go:25-32`) states *"why no test grows a group to it."* **Consequence:** the invariant
> *"default `maxGroupMembers` ≥ `completionSizeCeiling`"* (§3.5) is **not** mechanically enforced — both constants
> are unexported and in different packages, so no blackbox test can compare them. The increment defends it with a
> cross-reference comment on **each** constant naming the other, and the plan's final task greps that both still
> read `1 << 16`. **This is a stated limitation, not a closed one** (§8).

**AC-4 — the SQL bound is enforced IN the transaction and nothing is committed.** Against a real database via the
existing `dbtest`/`harness` Docker-backed conformance runner (`use-testcontainers`; never a mock):

1. `Add` the `cap+1`-th live member ⇒ `errors.Is(err, msgin.ErrOverflowDropped)`.
2. **The rollback is asserted, not assumed:** a subsequent `ClaimGroup` returns exactly `cap` members, and a direct
   member-row count over the table equals `cap`. Without this second half, enforcement (C) is indistinguishable
   from enforcement (A) — §3.6's whole argument.
3. **Idempotency is preserved at the boundary:** re-`Add`ing an **existing** member id while the group sits at
   exactly `cap` is a **no-op returning the unchanged snapshot**, not an overflow. This case fails against the
   natural implementation that counts before checking `ON CONFLICT DO NOTHING`.

**AC-5 — one conformance case per dialect, in `harness`.** The shared dialect test kit
(`adapter/database/sql/harness/groupstore.go`) gains a member-cap case so **postgres, mysql and sqlite** are each
held to the contract, rather than testing whichever dialect was convenient.

**AC-6 — no test grows a group to any ceiling.** A standing constraint, restated as an AC because it is the one a
future contributor will violate: every group-growth test uses a cap of ≤ 16 members. The ceiling values are
exercised by **constructors only**.

**AC-7 — the SPI contract has a conformance case on both first-party stores.** `memory.GroupStore` and
`sql.GroupStore` each get a case asserting §3.7's requirement through the `msgin.MessageGroupStore` interface —
i.e. driven through the interface type, not the concrete type, so the case is copyable by a third-party
implementer.

**AC-8 — the class gate is updated, and its blind spot is STATED, not silently widened.**

1. **Half 1 gains two keys.** `memory.WithMaxGroupMembers` and `sql.WithMaxGroupMembers` are exported,
   `Recv == nil`, `int`-parameter functions in **root-module** packages, so the AST scan finds them: **17 → 19**.
   `sizingConformanceKeys` must gain both or the gate fails — which is the gate working.
2. **Half 2 gains two rows in the `fixed` arm**, since both constructors reject `1<<62`. Arm totals become
   **11 fixed + 1 rejects + 3 deferred + 6 safe = 21 rows = 19 AST keys + 2 manual rows.** Re-derive from the arm
   table; do not increment.
3. **A NEW accepted limitation is added to the gate's header**, alongside the four it already states: *"a bound
   that does not arrive as an integer parameter is invisible — a func-typed option (`*ast.FuncType`), a named
   func type (`*ast.Ident`), or a threshold read from a message header (no parameter at all). Spec 017 §1.4
   enumerates the three that exist today and moves their enforcement to the store instead."*
4. **The `GroupDialect.AddMember` signature change adds an `int` parameter to a METHOD**, which the ratified
   `Recv == nil` boundary excludes (Spec 016 §2.0 / ADR 0032 **D-AA**). Under D-AB's criterion it is **not** a class
   member — `maxMembers` is the bound itself, not a quantity bounded by something else — so no manual row is
   required. **Stated explicitly** so the next audit does not have to re-derive it.

**AC-9 — every new branch is covered and mutation-proven.** Under [CLAUDE.md](../../CLAUDE.md)'s test-coverage
gate, each of these is a hot-path or typed-error branch needing a named covering case, and — per the stored lesson
*"mutation-test every new assertion"* — a mutant that the case kills:

| # | Branch | Site | Killing mutant |
|---|---|---|---|
| 1 | member-cap reject | `memory.GroupStore.Add`, before the append | delete the arm ⇒ AC-1's four cases fail |
| 2 | member-cap **pass** (the group is under cap) | same | invert to `>` ⇒ AC-3's release case fails |
| 3 | idempotent re-add at exactly cap | `memory.GroupStore.Add`, the `g.ids` seen branch | move the cap check **above** the dedup check ⇒ AC-4.3's twin fails |
| 4 | `checkRange` upper arm | `memory.NewGroupStore` | delete ⇒ AC-2 fails |
| 5 | `checkRange` lower arm | `memory.NewGroupStore` | change `1` to `0` ⇒ AC-2's `0` case fails |
| 6 | `checkRange` upper/lower arms | `sql.NewGroupStore` | as 4/5 ⇒ AC-2 `sql` half fails |
| 7 | dialect in-tx overflow ⇒ rollback | each dialect's `AddMember` | return the error **after** the tx commits ⇒ AC-4.2's row count fails |
| 8 | dialect under-cap pass-through | each dialect's `AddMember` | off-by-one to `>=` ⇒ AC-3's `sql` twin fails |

> **Branch 2 is the one a plausible implementation gets wrong and no other case catches.** A cap check written as
> `if len(g.msgs) > s.maxGroupMembers` (after the append) admits `cap+1` members — the group is bounded, every
> overflow test still passes, and the boundary arithmetic of §3.5 is silently off by one.

**AC-10 — vacuity probes, per the project's standing rule** (*"a gate that has never failed proves nothing"*).
Each gate half is proven to fire: plant a third `WithMaxGroupMembers`-shaped option (half 1 must report it) and
flip one conformance row's arm (half 2 must report it). **Prove it COVERS, not just that it FIRES** — plant the
half-1 probe in `adapter/database/sql`, the module this increment newly touches, not in root.

## 7. Scope

**In:** the per-group member bound at both first-party stores; the two new options with their ceilings, defaults,
godoc and typed errors; the `GroupDialect.AddMember` signature change and its three dialect implementations; the
`harness` conformance case; the `MessageGroupStore` SPI contract addition; the three release-path godoc
cross-references; the existing bare `ErrOverflowDropped` upgraded to the wrapped shape; the class-gate update and
its new stated limitation.

**Out:** any change to `completionSizeCeiling` or `WithCompletionSize`'s validation (§3.12.2); a group-**count** cap
for `sql` (§3.12.4); optimizing `memory.GroupStore.Add`'s quadratic clone (§3.12.5); making `WithGroupTimeout`
mandatory (§3.11); widening the class gate's AST scan (§3.12.6); the remaining `docs/HANDOVER.md` §6 backlog
items 2–6 and 8.

### 7.1 Multi-instance / deployment topology

[CLAUDE.md](../../CLAUDE.md) makes this mandatory for any component whose correctness depends on state spanning a
message's lifetime. A message group **is** exactly such state, so this section is load-bearing rather than a
formality.

**`memory.GroupStore` — in-process only, and its cap is per-process.** Each instance holds its own `s.groups` map;
two instances aggregating the same correlation key do not see each other's members at all. That is a pre-existing
property of the store, stated in its own godoc and in ADR 0020/0021, and **the member cap does not change it**: the
cap bounds the members *this process* retains, which is exactly the quantity that can OOM *this process*. **The EIP
pattern a distributed deployment requires here is the durable store itself** — `sql.GroupStore`, whose atomic
`ClaimGroup` is the cross-process serializer. Nothing new is needed.

**`sql.GroupStore` — shared and durable, and the cap is evaluated on a GLOBAL count.** This is the case that needs
real reasoning, and it has three parts:

1. **The counted quantity is global, not per-instance.** Each instance's check runs on the live member set of the
   **shared** group — every instance sees the same rows. So N instances do not get N× the cap; they enforce **one**
   cap on **one** group. This is the correct semantic and it falls out of the design rather than being engineered.
2. **Enforcement is atomic across instances, because of where it sits.** §3.6 places the check inside the
   dialect's transaction, **after** the statement that takes the group row lock (`postgres/groupdialect.go:105-112`,
   whose comment records that this *"serializes same-key adds (H1)"*). Two instances adding concurrently to a group
   at `cap-1` therefore serialize: one commits, the other counts `cap`, exceeds, and rolls back. **Enforcement (A)
   would NOT have this property** — its check runs after the transaction, so N instances could each commit past
   the cap. This is a second, independent reason for choosing (C).
3. **🔴 Instances MUST agree on the cap.** A per-instance configuration over a shared store means instance A with
   `WithMaxGroupMembers(1000)` and instance B with the 65,536 default will disagree: B grows the group past 1000,
   and A then dead-letters **every** subsequent member for that key while B continues normally — a partition of the
   traffic by which instance happened to receive it. This is the **same configuration-coherence requirement the
   store already carries for `WithGroupLeaseTTL`**, whose godoc explains why a tighter value is unsafe. It is
   documented as an operator requirement on `sql.WithMaxGroupMembers`'s godoc (§4) and it is **not** enforceable by
   the library — a store cannot know what its peers are configured with. Naming it is the deliverable.

**The SPI seam is shaped to accept a stronger future pattern without a core change.** §3.7 states the bound as a
requirement on the *store*, not as a number in the core, so a future distributed store may enforce it with a
database CHECK constraint, a partition-level quota, or a broker-side limit, and a future `pgx` or Redis group store
inherits the requirement without `routing` or the core being touched.

## 8. Open items — flagged for the user and for the audit

**None of the decisions in [ADR 0033](../adrs/0033-group-member-bounds.md) has been ratified by the user.** These
four are the ones where a reasonable person could land elsewhere, and where reversal is cheapest **before** Plan
031 starts:

1. **The `memory` / `sql` counting asymmetry** (§3.4) — `memory` counts live + claimed, `sql` counts live. The
   uniform alternative (both count live) simplifies the SPI contract to one sentence at the cost of letting
   `memory` transiently retain 2× the cap. *Recommendation: keep the asymmetry; it is honest about what each store
   retains. Reversible in one line per store.*
2. **The scope of the SQL fix** (§3.6) — enforcement (C) is the only option that bounds anything durable, but it
   costs a breaking `GroupDialect.AddMember` signature change across **five** call sites in **four** modules,
   roughly doubling the increment. Enforcement (A) is one line and no SPI change, and bounds roughly half the peak.
   *Recommendation: (C). But if the increment must be small, (A) plus a named follow-up is defensible — say so
   before the plan starts, not at task 6.*
3. **The default cap's value, 65,536** (§3.9) — the argument that a *ceiling*'s reference value may become a
   *default* is the load-bearing step, and it is an argument rather than a measurement. A more conservative default
   (say `1<<14`) would protect more callers and break more of them. *Recommendation: 65,536, because it is the
   number Plan 029 already ratified for this exact unit.*
4. **The unenforceable `default ≥ completionSizeCeiling` invariant** (§3.5, AC-3) — two unexported constants in two
   packages, defended by cross-reference comments and a grep. *Recommendation: accept, and record the drift risk.
   Exporting either constant to make it checkable is new public surface for a test's benefit.*

**Also recorded, out of scope, for `docs/HANDOVER.md` §6:**

- **`memory.GroupStore.Add`'s per-call `slices.Clone` is quadratic** (§1.2). The cap bounds the damage; it does not
  remove it. 8.6 s and 48.3 GiB to reach a legal cap value is a performance defect with its own remedy (return a
  view, or clone lazily) and its own spec.
- **`sql.GroupStore` re-fetches and re-decodes the entire live group on every `Add`** (§1.3). Same shape, durable
  side, and arguably worse: the cap bounds it, a `LIMIT`-and-count-only dialect contract would remove it.
- **A member cap without `WithGroupTimeout` yields a bounded-but-stuck group** (§3.11). Consider whether that
  combination deserves a construction-time diagnostic.
