# Spec 017 — A message group's member count is bounded at the store, not at the release decision

- **Status:** **DRAFT — revision 3, post-audit-round-2, NOT approved for implementation.** Written before any code,
  per [CLAUDE.md](../../CLAUDE.md)'s design-time gate.
  - **Round 1 verdict: NOT SAFE TO IMPLEMENT** — 3 BLOCKERs, 8 MAJORs, 10 MINORs, recorded immutably in
    [`docs/plans/031-audit-round-1.md`](../plans/031-audit-round-1.md). Revision 2 folded every finding back in.
  - **Round 2 verdict: NOT SAFE TO IMPLEMENT** — 1 BLOCKER, 6 MAJORs, 7 MINORs, recorded immutably in
    [`docs/plans/031-audit-round-2.md`](../plans/031-audit-round-2.md), against a fix-verification score of
    **12 clean LANDED, 8 LANDED-BUT-FLAWED, 1 (M-8) landed with a defensible ADR omission, 0 NOT LANDED,
    0 REGRESSED** — *"the revision is genuinely responsive; every flaw is new ground, not a re-run of round 1."*
    *(That summary and the record's own 21-row table differ by one row in the middle bucket; the record reconciles
    it by name and leaves it open, and nothing downstream depends on it.)* This revision folds those 14 back in.
    **The lesson round 2 returned is not a count — it is that revision 2 failed to GENERALIZE its own two
    structural fixes:** B-2's *"a cross-module edit is a red commit"* was applied to the class gate but not to the
    `GroupDialect.AddMember` signature (**N-1**, the BLOCKER), and M-3's *"one mechanism asserted for three
    engines"* was fixed for the transaction wrappers and then recurred for the **reaper** (**N-3**), the shared
    **`SelectMembers`** helper (**N-5**) and the very **shipped SPI godoc M-3 was about** (**N-9**). Read that
    before reading any individual fix below.
  - **A round 3 audit is a judgement call, not an automatic gate.** Two rounds is this project's established norm
    and both have run; revision 3 changes no runtime contract that revision 2 did not already establish — the edits
    are premise corrections, coverage additions and one new declaration-form constraint (**D-AR**) plus one new
    private helper parameter (**D-AS**). Re-audit if the user wants the norm exceeded, as Plan 029 did five times.
  - 🔴 **The decisions recorded here were taken WITHOUT USER RATIFICATION.** The user was away when this bundle was
    drafted and away again when round 1's findings were dispositioned. Every decision in
    [ADR 0033](../adrs/0033-group-member-bounds.md) (**D-AC** … **D-AQ**) is **open to reversal**, and §8 lists the
    ones that most deserve a second look before any code is written.
  - **Every structural claim below is re-derived against `d2c69fe`** — current `main`, **post-Plan-030** — not
    against `2b2dec1` where revision 1 measured them.
    [Plan 030](../plans/030-post-029-maintenance.md) landed mid-audit (`7ab91cd`, `1a1c135`, `d2c69fe`) and
    **shares four files with this increment**, contradicting revision 1's *"they share no file"*: it shifted every
    line in `adapter/memory/groupstore.go` from `:93` down and `adapter/database/sql/groupstore.go` from `:207`
    down by one, and rewrote 135 lines of `sizing_option_class_gate_test.go` (audit **B-3**, **M-1**).
  - Two claims inherited from the increment's framing were found **imprecise** in revision 1 and are corrected in
    place (§1.4, §2.1); round 1 found a further **21** and round 2 a further **14**, and the substantive ones are
    marked 🔴 in situ.
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

Re-derived from `routing/aggregator.go` at `d2c69fe` (unchanged by Plan 030):

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

`memory.GroupStore.Add` (`adapter/memory/groupstore.go:118-137`) has **one** admission check, and it is for **new
keys only**:

```go
g, ok := s.groups[key]
if !ok {
    if len(s.groups) >= s.maxGroups {        // :123-125 — the ONLY admission check
        return nil, msgin.ErrOverflowDropped
    }
    g = &groupState{ids: make(map[string]struct{}), createdAt: s.clock.Now()}
    s.groups[key] = g
}
if id := msg.ID(); id != "" {                // :129
    if _, seen := g.ids[id]; seen {          // :130 — the dedup LOOKUP
        return snapshot{key: key, msgs: slices.Clone(g.msgs[g.claimedLen:]), createdAt: g.createdAt}, nil  // :131
    }
    g.ids[id] = struct{}{}                   // :133 — the dedup INSERT; §3.4a's cap check must sit ABOVE this
}
g.msgs = append(g.msgs, msg)                 // :135 — NO per-group cap
return snapshot{key: key, msgs: slices.Clone(g.msgs[g.claimedLen:]), createdAt: g.createdAt}, nil  // :136
```

**`WithMaxGroups` bounds the NUMBER of groups; nothing bounds the members inside one.** Its own godoc
(`groupstore.go:64-72`) says so — *"bounds the number of concurrently held (incomplete) groups"* — and Spec 016
§1.4 already recorded the consequence: *"`WithMaxGroups` caps the number of groups, not members per group."*

> **The `:130` / `:133` distinction is load-bearing and revision 1 collapsed it.** Revision 1 said the cap check
> goes *"after the dedup branch"*; the dedup branch **ends with the id insert at `:133`**, so that placement
> records a member as *seen* and then rejects it — silent data loss on redelivery. **§3.4a** and
> [ADR 0033 **D-AO**](../adrs/0033-group-member-bounds.md) fix it (audit **M-6**, second defect).

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

**Growth is monotonic for a group that never releases.** `SettleGroup` (`groupstore.go:160-183`) trims `g.msgs` to
the residual and `delete(s.groups, claim.Key())` when the residual is empty (`:173-178`) — but it runs only after a
successful release.

#### 1.2.1 🔴 The reaper, re-derived PER STORE — the two stores behave differently by default

> **Revision 2 wrote *"the reaper is opt-in"* as a fact about both stores. It is a fact about `memory` only, and
> §3.3.1's entire classification argument was resting on it** (audit **N-3**). This is audit **M-3**'s defect —
> *one mechanism asserted where there are two* — recurring in the reaper, and it is corrected here the same way
> §3.6.1 corrects it for the transaction wrappers: **per store, with the mechanism named.**

`Aggregator.reapInterval` (`routing/aggregator.go:558-565`) takes the **minimum positive** of `WithGroupTimeout` and
the store's `RecoverInterval()` — not `cfg.timeout` alone:

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

| Store | `RecoverInterval()` | With **no** `WithGroupTimeout` | What the default sweep can surface |
|---|---|---|---|
| `memory.GroupStore` | **`0`** (`adapter/memory/groupstore.go:220`) | interval is `0` ⇒ `Run` blocks on `ctx.Done()` and **never sweeps** | nothing |
| `sql.GroupStore` | **the lease TTL** (`adapter/database/sql/groupstore.go:348`), default **5m** | interval is **5m** ⇒ `Run` starts a ticker (`aggregator.go:544`) and **sweeps every 5m** | **crashed-lease groups only** — see below |

`Aggregator.Run`'s own godoc says so, and calls `Run` **required** for a durable store: *"A durable store
(RecoverInterval() = its lease TTL) gets crash-recovery sweeps even with no expiry timeout set — so go agg.Run(ctx)
is REQUIRED for multi-process/crash safety whenever the store is durable, not only when WithGroupTimeout is set"*
(`aggregator.go:530-532`).

**Why the conclusion survives anyway, by a mechanism revision 2 never stated.** With `cfg.timeout == 0`, `reap`
passes a **zero cutoff**, and every dialect's `ExpiredGroups` gates the age path on it:

```go
adapter/database/sql/postgres/groupdialect.go:275-282
	beforeSet := !before.IsZero()
	…`WHERE (locked_by IS NOT NULL AND locked_at <= now - $2)
	     OR ($1 AND locked_by IS NULL AND created_at < $3)`…
		beforeSet, leaseTTL.Microseconds(), before.UnixMicro(), limit)
```

`$1` is false, so the second arm never matches: the default `sql` sweep returns **crashed-lease groups only**. **An
UNLEASED group at cap is never surfaced without an expiry cutoff, in either store** — which is the true, per-store
form of the premise §3.3.1 needs, and the form D-AM now states.

**The live counter-example this obliges the design to own** is in §3.3.1: a `sql` group at cap that **also** holds a
**stranded lease** *is* surfaced by the default sweep, *is* claimed, and *is* drained if its predicate fires.

**`AbandonGroup` is the third exit, and §3.3a turns on it.** `AbandonGroup` — godoc `groupstore.go:185-188`, func
`:189-199`, not `:185-198` as revision 2 cited (audit **N-13**) — clears the lease and `claimedLen` **without
shrinking `g.msgs`**, and its godoc states the recovery contract the member cap must not break: *"the claimed
members return to live (along with anything appended during the lease) **so a retry / next member / next reaper tick
re-releases**"* (`:185-187`, and the two assignments at `:196-197`).

### 1.3 The SQL group store has the same gap — and no group-count cap at all

`adapter/database/sql.GroupStore` (`adapter/database/sql/groupstore.go:250-276`) is the durable twin. Re-derived:

1. **It has no member cap.** `Add` delegates straight to `GroupDialect.AddMember` (the call is at
   `groupstore.go:271`) and then tail-calls `s.decodeGroupRows(rows)` (`groupstore.go:275`).
2. **It has no group-count cap either** — `grep -n 'maxGroups\|MaxGroups' adapter/database/sql/*.go` returns
   **nothing**. `sql.NewGroupStore`'s options are `WithGroupLeaseTTL` (`groupstore.go:140`), `WithGroupLockedBy`
   (`:155`) and a logger; there is no `WithMaxGroups` equivalent.
3. **Every `Add` materializes the WHOLE live group in the process heap.** The `GroupDialect.AddMember` contract
   (`groupdialect.go:108-126`) says the dialect *"finally SELECTs the group's current CreatedAt plus its LIVE
   members (claimed_epoch IS NULL), ordered by seq then msg_id"*, and `decodeGroupRows` — **defined at
   `groupstore.go:365`**, not at `:270-280` as revision 1 claimed (audit **m-3**) — then does
   `make([]msgin.Message[any], 0, len(rows.Members))` plus a `DecodeHeaders` per member.
4. **The error path costs an extra round-trip.** `Add` routes every dialect error through `classifyQueryErr`
   (`groupstore.go:273` → `:91-96`), which runs a `SchemaExists` query before returning. The `errors.Is` chain
   survives, so correctness is unaffected; the cost is not, and §3.6 records it as a stated cost of the overflow
   arm (audit **m-6**).
5. **An empty msg id is rejected before any query runs** — `groupstore.go:251-253`,
   `if msgID == "" { return nil, ErrMissingMsgID }`, repeated in every dialect's `AddMember`. **This is why §3.3a's
   deadlock is `memory`-only.**

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
silently — **rewritten by [Plan 030](../plans/030-post-029-maintenance.md) Task 2 (`d2c69fe`, 135 lines changed);
every line citation below is post-030.** Half 1 walks the AST and collects every exported, `Recv == nil` function
with an `int`/`int64` parameter in any position, via `hasIntOrInt64Param` (`:215-225`) → `isIntOrInt64`
(`:231-243`). Verified green on this tree:

```
$ GOTOOLCHAIN=go1.25.13 go test -run TestSizingOptionClass -v .
    sizing_option_class_gate_test.go:317: === EXPORTED FUNCTIONS (Recv==nil) with int/int64 param in ANY position: 17
    sizing_option_class_gate_test.go:318: === EXPORTED METHODS with int/int64 param: 27 (excluded by the Recv==nil boundary, Spec 016 §2.0)
--- PASS: TestSizingOptionClass_Completeness (0.15s)
--- PASS: TestSizingOptionClass_Conformance (0.00s)
```

> 🔴 **Half 1 is EXACT SET EQUALITY, in both directions** — `assert.Equal(t, want, found, …)` at `:321-324`, not a
> subset check. **The moment `memory.WithMaxGroupMembers` exists on disk, the ROOT module's suite goes red**, and it
> stays red until the matching key is added. That is the gate working, and it is why §6 AC-8 and Plan 031 require
> the gate rows to land in the **same commit** as the option they describe rather than in a later task
> (audit **B-2**).

**Neither `WithReleaseStrategy` nor `WithReleaseWhen` is visible to it — but by TWO different mechanisms, and the
backlog entry names only one.** `isIntOrInt64`'s type switch handles `*ast.Ident`, `*ast.Ellipsis`,
`*ast.ArrayType` and `*ast.StarExpr`, and returns `false` for everything else:

| Option | Parameter's AST node | Why it is invisible |
|---|---|---|
| `WithReleaseWhen(fn func(msgin.MessageGroup) bool)` | `*ast.FuncType` | falls through the type switch to `return false` — **this is the mechanism the backlog entry describes** |
| `WithReleaseStrategy(fn ReleaseStrategy)` | `*ast.Ident{Name: "ReleaseStrategy"}` | matched as an `*ast.Ident` whose name is neither `"int"` nor `"int64"` — the **named-type** path, the same one the gate's header already states for `type Bytes int64` |

The distinction matters because the gate's header (`:103-129`) enumerates four accepted limitations — the
root-module import boundary, the `Recv == nil` boundary, **named integer types**, and `time.Duration`. The
named-type bullet *partially* covers `WithReleaseStrategy`; **nothing covers `WithReleaseWhen`'s `*ast.FuncType`,
and nothing at all covers "a knob that is not a parameter"** (path 4, the header-driven `defaultRelease`).

**One of those four limitations is FALSIFIED by this increment and must be edited, not merely extended.** The
root-module import boundary bullet reads *"All 17 keys live in root-module packages today (endpoint, adapter/http,
adapter/memory, channel, resilience, routing)"* (`:107-108`). `sql.WithMaxGroupMembers` lives in
`adapter/database/sql` — a **package in the root module**, but **not on that list**. Leaving it turns a stated
limitation into a false claim about the gate's own coverage (audit **M-7**).

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

This is the decision the whole increment turns on (ADR 0033 **D-AC**). **The rationale was REWRITTEN in revision 2**
— audit **M-4** showed two of revision 1's three reasons were false, though the conclusion survives. Four reasons,
in order of weight:

1. **Only the store can refuse a member BEFORE retaining it.** `store.Add` has exactly **one** caller in the
   workspace — `Aggregator.Handle` at `routing/aggregator.go:412` — so *"the store observes every member"* does not
   discriminate between the store and `Handle`; both see the same population. What discriminates is **when**: by the
   time `Handle` holds `Add`'s snapshot, the member is **already appended and already retained**. A check there
   would bound the *reported* size while the heap grew anyway — the same false-safety inversion §3.6 rejects for
   SQL enforcement (A).
2. **Completeness across release paths.** §1.1: the release strategy is an opaque caller closure in paths 2 and 3,
   and a message header in path 4. A bound expressed **there** covers path 1 only — exactly the state Plan 029
   shipped. (This argues against the *release decision*; reason 1 is what argues against `Handle`.)
3. **Precedent inside the same file.** `memory.GroupStore`'s *existing* admission check for group **count** already
   lives in `Add` (`groupstore.go:123-125`), not in the aggregator. This spec adds the member check a few lines
   below it, in the same function, under the same lock, returning the same sentinel. The two arms become symmetric
   rather than one being a special case.
4. **A store used directly, without an Aggregator, is otherwise unbounded.** `msgin.MessageGroupStore`
   (`groupstore.go:37`) is public; nothing obliges a consumer to drive it through `routing.Aggregator`.

> 🔴 **DELETED in revision 2, as false:** revision 1's reason 3 — *"a bound stated in `routing` protects only the
> stores `routing` happens to ship with."* **`routing` ships no stores at all**; both first-party stores live in
> `adapter/memory` and `adapter/database/sql`, so a check in `Aggregator` would cover *every* store an Aggregator
> is pointed at, third-party ones included. The sentence asserted the opposite of the fact. Reason 4 is the true,
> narrower form of the benefit it was reaching for.

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

> 🔴 **BOTH VALUES ARE NAMED CONSTANTS IN BOTH PACKAGES — a deliberate deviation from the shipped precedent**
> (audit **N-4**; ADR 0033 **D-AR**). `adapter/memory` today declares its ceiling as a `const`
> (`maxGroupsCeiling`, `groupstore.go:62`) but its **default as a bare literal inside a composite literal** —
> `cfg := groupStoreConfig{clock: …, maxGroups: 1024}` (`groupstore.go:98`). An implementer following that local
> precedent writes `maxGroupMembers: 1 << 16` inline, and **§6 AC-3.3's not-found guard fires**, because the AST
> invariant has no declaration to locate. So this spec requires, in **both** packages:
>
> ```go
> const defaultMaxGroupMembers = 1 << 16 // 65,536 — §3.2; AC-3.3 parses this declaration BY NAME
> const maxGroupMembersCeiling = 1 << 20 // 1,048,576 — §3.2
> ```
>
> **The deviation is deliberate and is recorded as a decision** (D-AR) rather than left as a style choice, because
> a shipped precedent departed from silently is a future audit finding. It is not a change to `maxGroups: 1024`,
> which no invariant test reads.

**Validation is the shipped helper, unchanged:**

```go
cfg := groupStoreConfig{…, maxGroupMembers: defaultMaxGroupMembers}
…
if err := checkRange(msgin.ErrInvalidCapacity, "memory.WithMaxGroupMembers",
    cfg.maxGroupMembers, 1, maxGroupMembersCeiling); err != nil {
    return nil, err
}
// memory.NewGroupStore(memory.WithMaxGroupMembers(0))
//   → "msgin: capacity out of range: memory.WithMaxGroupMembers: 0 not in [1, 1048576]"
```

`checkRange` exists in `adapter/memory/helpers.go:54` and follows ADR 0031 **D-R**'s four-independent-copies
precedent; `adapter/database/sql` does **not** have one today — `grep -rn 'func checkRange'` finds exactly four
copies, at `endpoint/helpers.go:97`, `routing/helpers.go:88`, `adapter/memory/helpers.go:54` and
`adapter/http/helpers.go:64` — and gains a fifth, unexported, identical copy.

**ONE NAME IN BOTH PACKAGES, and it is the convention rather than a break from it** (ADR 0033 **D-AD**; audit
**m-9**). Round 1 objected that `adapter/database/sql`'s `GroupStore` options are `WithGroup…`-prefixed. Re-deriving
the convention shows it is a **collision rule**, not a blanket prefix rule, and both packages already follow it:
`WithGroupLeaseTTL` disambiguates from `WithLeaseTTL` (`options.go:115`), `WithGroupLockedBy` from `WithLockedBy`
(`:129`), and in `adapter/memory` `WithGroupClock` from `WithClock` (`queuestore.go:94`) — while `WithMaxGroups`
takes no prefix, because nothing collides with it. `MaxGroupMembers` already contains "Group" and collides with
nothing in either package. Two names for one SPI concept would also force §3.7's contract paragraph to name both.

**This makes `msgin.ErrInvalidCapacity` a SIXTH producer.** Spec 016 §3.5 counted four and warned: *"a fifth
producer should be a conscious decision rather than a default."* This increment adds two at once. The decision is
conscious and recorded as ADR 0033 **D-AD**: the alternative — minting `memory.ErrInvalidGroupMembers` and
`sql.ErrInvalidGroupMembers` — adds two exported sentinels to a pre-v1 surface we are keeping small, and splits a
caller's *"this size is wrong"* branch across four `errors.Is` targets. The generic sentinel text
(*"msgin: capacity out of range"*) plus the site name in the wrap is what makes six producers tolerable, exactly as
it made four tolerable. **A seventh needs its own ADR.**

### 3.3 The overflow report — and its classification, which is the thing round 1 broke

An `Add` that would exceed the cap returns **`msgin.ErrOverflowDropped`** — the same sentinel as the group-count
arm a few lines above it (`groupstore.go:124`), so the two overflow arms are symmetric (ADR 0033 **D-AE**).

**It is wrapped, where the existing arm is bare, and that asymmetry is deliberate.** `groupstore.go:124` reads
`return nil, msgin.ErrOverflowDropped` with no context. The new arm renders:

```go
fmt.Errorf("%w: %s: group %q holds %d members, limit %d",
    msgin.ErrOverflowDropped, "memory.GroupStore.Add", key, len(g.msgs), s.maxGroupMembers)
// → msgin: message dropped by overflow policy: memory.GroupStore.Add: group "order-42" holds 65536 members, limit 65536
```

Two reasons the wrap wins over literal symmetry: debuggability is [CLAUDE.md](../../CLAUDE.md)'s stated **core**
quality criterion (*"prefer typed, wrapping errors that name the offending field/input"*), and — decisively —
`msgin.ErrOverflowDropped` has **four producer sites**: three returns (`queuestore.go:171`, `:176`,
`groupstore.go:124`) plus an `OnRetry` hook argument at `endpoint/consumer.go:576`. A bare sentinel cannot tell an
operator which cap fired. **The existing bare arm is upgraded to the same shape in the same commit** — fix the
class, not the instance (§4).

#### 3.3.1 🔴 The classification: PERMANENT when the group cannot drain itself

> **Revision 1 said this error is transient, always. That was BLOCKER B-1, and it is the most serious finding round
> 1 returned.** The reasoning below replaces it (ADR 0033 **D-AM**).

**What revision 1 assumed.** *"`Aggregator.Handle` returns `store.Add`'s error unchanged, so the fault travels the
runtime's ordinary `RetryPolicy`: retry with backoff, then dead-letter on exhaustion."* That describes a
`RetryPolicy` with a `MaxAttempts` and a `Backoff`. **The shipped zero value has neither.**

**What actually happens on `RetryPolicy{}`.** Re-derived at `d2c69fe`:

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

endpoint/consumer.go:1323-1328
func retryDelay(p msgin.RetryPolicy, attempt int) time.Duration {
	if p.Backoff == nil { return 0 }
	return p.Backoff.Delay(attempt - 1)
}
```

`MaxAttempts == 0` ⇒ the dead-letter guard is false ⇒ **every** attempt takes `default`. `OnRetry` is nil ⇒
`safeFire` is a no-op. `Backoff` is nil ⇒ delay **0** ⇒ immediate redelivery ⇒ reject ⇒ `default` ⇒ **an infinite,
zero-delay hot spin with no log line and no dead-letter.** The runtime documents this exact shape for a different
arm at `endpoint/consumer.go:96`: *"hot-spins when Backoff is nil (the default)."*

**For `sql` it is worse than the status quo.** Each iteration is a full `AddMember` transaction — `BEGIN`, group-row
upsert-and-lock, member upsert, live-member `SELECT`, `ROLLBACK` — **plus** the `SchemaExists` probe of §1.3 item 4,
forever. Today the same message simply appends and succeeds. A remedy that converts an unbounded group into an
unlogged infinite loop against the database is not a remedy.

**Why the "a retry can work" argument does not rescue it.** Revision 1 justified transient from *"an over-cap `Add`
can succeed later — when the group releases … or when the reaper expires it."* The release escape requires the
predicate to fire, which for the class of groups at issue it does not. The reaper escape requires an **expiry
cutoff**:

> 🔴 **THE PREMISE, RESTATED IN REVISION 3 — the revision-2 form was `memory`-only** (audit **N-3**). Revision 2
> wrote *"with no `WithGroupTimeout` the reaper never sweeps."* That is true for `memory` (`RecoverInterval() == 0`)
> and **false for `sql`**, whose `RecoverInterval()` returns the lease TTL, so the reaper ticks every 5m by default
> (§1.2.1). The premise this classification actually needs, and the one that is true for **both** stores, is:
>
> **Nothing drains an UNLEASED group without an expiry cutoff.**
>
> `memory` never sweeps at all; `sql` sweeps, but with `cfg.timeout == 0` the cutoff is zero and `ExpiredGroups`'
> `beforeSet` guard restricts the result to **crashed-lease** groups (§1.2.1). Either way, a group that is at cap
> and **unleased** is not surfaced, not claimed and not drained. "Can succeed later" is false for that group in
> the default configuration of either store — **and that group is exactly the one D-AM classifies permanent.**

So the classification is not derived from a configuration the decision does not require — but **only under the
restated premise**. Under revision 2's premise it was, for `sql`.

**The permanent arm behaves correctly on the zero value**, which is what makes this fixable:

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

> 🔴 **WHICH SIGNAL IS ACTUALLY LOUD DEPENDS ON THE SINKS, and revision 2 named the wrong one for the case its
> whole argument is about** (audit **N-11**). Revision 2 cited `warnInvalidFallback` as the WARN. But
> `invalidTarget` returns `fellBack = (c.policy.DeadLetter != nil)` (`endpoint/consumer.go:942`), so on the
> **zero-value `RetryPolicy`** — the configuration B-1 is about — `fellBack` is **false** and
> `warnInvalidFallback` **never fires**. Two corrections follow:

| Configuration | What actually happens | Site |
|---|---|---|
| `WithInvalidMessageSink` set | the message is sent there; no fallback WARN | `consumer.go:914`+ |
| no invalid sink, **`RetryPolicy.DeadLetter` set** | falls back to the dead-letter sink, and `warnInvalidFallback` fires — **once per consumer**, `sync.Once`-deduped (`consumer.go:968-973`), **not once per message** | `:942`, `:968` |
| **neither sink** (the shipped default) | `divertTerminal`'s nil-sink arm WARNs naming *both* missing options (`:1049`) and then **`safeAck`s** (`:1073`) — so the outcome is **WARN + Ack: the source drops the message** | `:1049`, `:1073` |

> **The bare-configuration outcome, stated so the claim is not oversold.** It is a **logged, terminal, one-WARN-
> per-message discard that the source Acks** — not a durable capture, and not a redelivery. An at-least-once source
> will not hand the message back. That is still strictly better than revision 1's unlogged infinite spin, and it is
> the honest ceiling on what this classification can promise: **a sink is what turns the loss into a capture, and
> the library cannot supply one.** Both options' godoc says so, including the Ack (§4 item 1).

**THE CONTRACT.** The store classifies by **why** the group is full:

| Cause | Classification | Why |
|---|---|---|
| group at cap and **NOT leased** | **`msgin.Permanent(…)`** wrapping the §3.3 error | nothing will drain it on its own. Terminal, sink-diverted, WARN on fallback, **works on `RetryPolicy{}`** |
| group at cap and **LEASED** | plain transient, exactly as revision 1 | a claim is in flight; `SettleGroup`/`AbandonGroup` runs on every release path including a panic-safe defer, so the retry genuinely succeeds after it |

`memory` reads `g.leased` (`adapter/memory/groupstore.go:43`) directly. For `sql` the cap counts **live** members
(§3.4), and a live set is by definition unclaimed, so **every `sql` over-cap rejection is the not-leased case** and
is classified permanent. One rule, two stores.

> 🔴 **THE COUNTER-EXAMPLE THIS TRADE MUST OWN — wider than revision 2 stated** (audit **N-3**). Revision 2's
> honest trade was *"a caller who has set `WithGroupTimeout` sees messages dead-lettered that their reaper would
> eventually have admitted."* For `sql` the exposure reaches the **default** configuration too. Consider a `sql`
> group that is **at cap** *and* holds a **stranded lease** (a releaser crashed mid-release):
>
> 1. `ExpiredGroups`' **first** `WHERE` arm — `locked_by IS NOT NULL AND locked_at <= now - leaseTTL` — matches it
>    **regardless of cutoff**, so the 5m default sweep **surfaces** it (§1.2.1).
> 2. `reapGroup` **claims** it, and the recovery path **drains** it if its predicate fires.
> 3. But the cap counts **live** members, and a stranded lease leaves a live residual — so an arriving member for
>    that key meanwhile hits the cap and, under this contract, is classified **permanent** and terminated.
>
> **So D-AM permanently dead-letters members that the default `sql` sweep would have admitted one tick later.** The
> trade still stands — a 5m wait for an unlogged spin is not a trade anyone should take, and the alternative is
> B-1 — but it is **not** limited to callers who configured a timeout, and Spec §8 item 5 records that.

`msgin.Permanent` wraps transparently (`permanentError.Unwrap`, `reliability.go:14`), so
`errors.Is(err, msgin.ErrOverflowDropped)` still holds. **`IsPermanent` is `reliability.go:86-97`** — not `:35-46`
as revision 1 cited (audit **m-1**) — and matches `*permanentError` via `errors.As`, so the marker is what
classifies, not the sentinel. The rendered string gains the shipped `"msgin: permanent: "` prefix; §6 AC-2b asserts
the real text, prefix included.

**The trade-off, stated as a decision rather than an obvious call.** With `WithGroupTimeout` set, the reaper would
eventually expire the group, so a permanent classification **dead-letters a message that might have succeeded
later**. That cost is paid deliberately: a hot spin is a production-down event, a dead-lettered message is
recoverable, and [CLAUDE.md](../../CLAUDE.md)'s Sensible-defaults gate says to pick the value that fails safe when
a wrong default is an unbounded-growth or DoS lever. **The counter-argument is kept, not dismissed:** a caller who
*has* configured a timeout is strictly worse off than transient would have left them. ADR 0033 **D-AM** records
both sides and the reversal cost (one branch per store).

**Rejected: silently dropping, evicting the oldest member, or force-releasing at the cap.** All three are in §5.

### 3.3a The rejection must not destroy the RELEASE — the store returns the live snapshot with the error

> **This section exists because revision 1's cap check introduced a permanent deadlock the current code does not
> have** (audit **M-6**; ADR 0033 **D-AN**). It is a regression, not an inherited limitation, and §3.11's
> "liveness unchanged" claim was false without it.

**The deadlock, step by step.** An **id-less** message (`msg.ID() == ""` — a supported shape, branched on at
`adapter/memory/groupstore.go:129`) against a group at `cap-1`:

| Step | What happens | Site |
|---|---|---|
| 1 | cap check passes; append ⇒ `len(g.msgs) == cap` | `groupstore.go:135` |
| 2 | `Handle`: the release predicate **fires** on `cap` members | `routing/aggregator.go:426` |
| 3 | `ClaimGroup` → claim; `releaseOnce` **fails** (agg error, or the output `Send` fails) | `aggregator.go:451-467` |
| 4 | the deferred abandon runs: lease and `claimedLen` cleared, **`g.msgs` NOT shrunk** | `groupstore.go:196-197` |
| 5 | Nack ⇒ redelivery. `Add` again: id is `""`, so **the dedup branch is skipped entirely** | `groupstore.go:129` |
| 6 | cap check: `len(g.msgs) == cap >= cap` ⇒ **REJECT, before the release decision is ever reached** | the new arm |

**The group now holds a complete, releasable set that nothing will ever re-trigger.** `AbandonGroup`'s godoc
promises *"a retry / next member / next reaper tick re-releases"* (§1.2); the cap check removes the first two and
the third is off by default. **Before this change, step 6 appended and re-fired the release, which succeeded.**

**THE CONTRACT.** On rejecting an over-cap member, `Add` returns **the current live group snapshot together with the
error**, and `Aggregator.Handle` re-evaluates the release predicate against it. **Reject the member, not the
release.**

```go
// routing/aggregator.go — Handle, replacing the bare error return at :412-415
group, err := a.store.Add(ctx, key, msg)
if err != nil {
    if group == nil {
        return err // unchanged for every store that returns (nil, err)
    }
    ok, rerr := a.cfg.release(group)
    if rerr != nil || !ok {
        return err // nothing to drain — §3.3.1's classification stands
    }
    claim, cerr := a.store.ClaimGroup(ctx, key)
    if cerr != nil {
        return cerr
    }
    if claim == nil {
        return a.overflowRetryable(key, err) // another holder is releasing it
    }
    if relErr := a.release(ctx, claim); relErr != nil {
        return relErr
    }
    return a.overflowRetryable(key, err) // drained — the retry WILL be admitted
}
```

`overflowRetryable` mints a fresh **transient** error rather than unwrapping the permanent marker:

```go
fmt.Errorf("%w: routing.Aggregator.Handle: group %q drained by this release; retry to admit the rejected member",
    msgin.ErrOverflowDropped, key)
```

#### 3.3a.1 🔴 The branch has SIX exits, and each one is a hot-path branch

> **Revision 2's coverage tables named four** — Plan 031's B1-11…B1-14 and §6 AC-9 rows 12-13 (audit **N-7**).
> [CLAUDE.md](../../CLAUDE.md)'s test-coverage gate makes *every* early-return on the hot path a delivery blocker,
> and two of the missing ones are not innocuous.

| # | Exit | Covered by | Why it needs its own case |
|---|---|---|---|
| 1 | `group == nil` ⇒ `return err` | AC-1's four cases via a `(nil, err)` stub store | the compatibility arm every pre-existing store takes |
| 2a | `!ok` (the strategy declined) ⇒ `return err` | AC-1 | the store's D-AM classification stands |
| **2b** | **`rerr != nil` (the strategy ERRORED) ⇒ `return err`** | **NEW** | a mutant dropping this half of the `||` **claim-and-releases a group the strategy rejected** — the strategy's error is not a "no" |
| **3** | **`cerr != nil` (`ClaimGroup` failed) ⇒ `return cerr`** | **NEW** | returning `cerr` **discards the overflow classification**: the caller loses `ErrOverflowDropped` and sees a store error instead. Deliberate; assert it |
| **4** | **`claim == nil` (another holder) ⇒ `return overflowRetryable(…)`** | **NEW** | **a deliberate divergence from the normal path**, which returns **`nil`** for the identical condition (`routing/aggregator.go:438-439`, *"another Handle/process is releasing this group; held"*). Here the member was never stored, so `nil` would Ack an unstored message — hence retryable. **Say so in the godoc**, or the next reader "fixes" it |
| **5** | **`relErr != nil` (the release failed) ⇒ `return relErr`** | **NEW** | the Nack then names the **output channel**, not the cap. An operator debugging a full group is pointed at the wrong subsystem unless the error is asserted |
| 6 | drained ⇒ `return overflowRetryable(…)` | AC-1b steps 3-4 | the self-healing path |

**Killing mutants** are in §6 AC-9 rows 12a-12d. **The direction rule that governs all six**: the Aggregator may
only ever **DOWNGRADE** the store's classification (permanent → transient), never upgrade it. That rule is now in
§3.7's MAY clause, where a third-party store author reads it, rather than only in this section's prose.

**Why an error is still returned when the drain succeeds.** The member was never stored. Returning `nil` would make
the source **Ack a message that was never aggregated** — the delivery-guarantee violation §5 rejects under *"Drop
the over-cap member silently."* Transient is right here and does not re-litigate §3.3.1: the group provably just
shrank, so the retry provably succeeds.

**Why the direction is safe.** The store's default is the conservative one (permanent, no spin); only **positive
evidence of drainability** downgrades it. A bug in the drain path costs a dead-letter, not a production-down spin.

**Scope, measured — and why `sql` implements it anyway.** The deadlock is `memory`-only and id-less-only: with a
non-empty id the dedup branch returns the snapshot with a **nil** error and `Handle` reaches the predicate anyway,
and `sql.GroupStore.Add` rejects an empty msg id before any query runs (§1.3 item 5). `sql` implements the snapshot
return regardless, because without it a `sql` caller gets a false-permanent dead-letter in a case a `memory` caller
does not — an asymmetry with no principle behind it. The `sql` cost is **zero extra queries**: the dialect's
live-member `SELECT` already runs, gains a `LIMIT maxMembers+1`, and the rejected member is filtered out of the
materialized `[]MemberRow` in Go before the rows are returned with the error (§3.6).

**The SPI change is additive.** §3.7's contract gains a **MAY**; a store returning `(nil, err)` keeps working
through `Handle`'s `group == nil` arm, and the existing `(nil, nil)` guard at `aggregator.go:416-424` is untouched.

### 3.4 What the cap counts

**`memory` counts `len(g.msgs)` — live PLUS claimed** (ADR 0033 **D-AF**), because that slice is what the process
retains. A claimed-but-unsettled member is still in the heap; a cap that ignored it would let a group hold up to
2× the cap across a claim boundary.

**`sql` counts the LIVE members only** (`claimed_epoch IS NULL`), because for `sql` the claimed members are
retained by the **database**, not by the process, and the quantity this spec bounds for `sql` is *"what one `Add`
drags into the process heap"* (§1.3).

> 🔴 **This is an asymmetry between the two stores.** Revision 1 flagged it as the finding most likely to be
> reversed; **round 1 attacked it and left it standing** — *"each store bounds what it actually retains, and the SPI
> godoc in §3.7 is written to admit both. **Not** a finding."* The uniform alternative — both stores count live
> members only — makes the SPI contract one sentence instead of two, at the price of letting `memory` retain up to
> 2× the cap transiently. It remains listed in §8 as open **for the user**, not for the audit.

### 3.4a WHERE the memory check goes — between the dedup lookup and the dedup insert

> **Revision 1 said "before the append and after the dedup branch." That placement silently loses messages**
> (audit **M-6**, second defect; ADR 0033 **D-AO**).

The dedup branch **ends with the id insert** at `groupstore.go:133`. A check placed after it records the member's id
as *seen* and then rejects it; on redelivery the dedup branch returns the snapshot with a **nil** error, `Handle`
returns `nil`, and **the source Acks a message that was never appended** — precisely what §5 rejects under *"Drop
the over-cap member silently."*

The check sits **after the `seen` lookup and before any mutation**, with the id hoisted so it also runs on the
id-less path §3.3a turns on:

```go
id := msg.ID()
if id != "" {
	if _, seen := g.ids[id]; seen {
		return snapshot{…}, nil                     // unchanged: idempotent no-op, never an overflow
	}
}
if len(g.msgs) >= s.maxGroupMembers {               // ← after the lookup, before ANY mutation
	live := snapshot{…}                             // §3.3a: the live snapshot travels with the error
	err := fmt.Errorf("%w: memory.GroupStore.Add: group %q holds %d members, limit %d",
		msgin.ErrOverflowDropped, key, len(g.msgs), s.maxGroupMembers)
	if !g.leased {
		return live, msgin.Permanent(err)           // §3.3.1: structurally stuck
	}
	return live, err                                 // §3.3.1: the claim window will drain it
}
if id != "" {
	g.ids[id] = struct{}{}
}
g.msgs = append(g.msgs, msg)
```

**Three invariants, each with a killing mutant in §6 AC-9:** an idempotent re-add at exactly the cap is a no-op
(mutant: move the check above the `seen` lookup); a rejected member leaves **no trace** in `g.ids` (mutant: move it
below the insert); and the check runs on the **id-less** path (mutant: fold it back inside `if id != ""`).

> 🔴 **THE RENDERED COUNT IS "MEMBERS RETAINED AT THE MOMENT OF THE CHECK", AND THE TWO STORES DIFFER BY ONE**
> (audit **N-8**). Plan 031's Global constraint 4 calls the error shape *"identical in both stores and all three
> dialects."* The **shape** is identical; the **number** is not, and it cannot be, because the checks sit on
> opposite sides of the write:
>
> | Store | Check position | `%d` renders | At the default cap |
> |---|---|---|---|
> | `memory` | **before** the append (§3.4a) | the **pre-add** live+claimed count | `holds 65536 members, limit 65536` |
> | `sql` dialects | **after** the member upsert (§3.6.1 — required, so an idempotent re-add at cap stays a no-op) | the **post-upsert** live count, which includes the offending member | `holds 65537 members, limit 65536` |
>
> **Both are correct readings of "members retained at the moment of the check", and that phrase is the contract** —
> not a single arithmetic value. The alternative, normalising `sql` to `len(members)-1`, was rejected: it would
> render a count that no statement in that transaction ever observed, purely to make a sentence in a plan true.
> **§6 AC-2c pins BOTH renders** so the difference is asserted rather than discovered.

### 3.5 The boundary interaction with `WithCompletionSize` — the exact arithmetic

**Question:** with the default cap at 65,536 and `WithCompletionSize(1<<16)` also 65,536, does release fire before
the cap rejects?

**Answer: yes, exactly — and the boundary is exactly attainable, with zero margin.**

The check is placed **before the append** (and after the dedup lookup — §3.4a), so:

```go
if len(g.msgs) >= s.maxGroupMembers {   // ← BEFORE the append at :135
    return live, err                     // §3.3a: snapshot WITH the error
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
permanently unreachable — a silent deadlock instead of a bound. **It binds BOTH stores**, because both take the
same default under the same Aggregator with the same `WithCompletionSize`. Three declarations, all `1 << 16`:

```
routing/aggregator.go:33                  const completionSizeCeiling  = 1 << 16
adapter/memory/groupstore.go              const defaultMaxGroupMembers = 1 << 16   (§3.2, D-AR — a NAMED constant)
adapter/database/sql/groupstore.go        const defaultMaxGroupMembers = 1 << 16   (§3.2, D-AR — a NAMED constant)
```

> 🔴 **REVISION 2 CLOSES THIS. Revision 1's claim that "a blackbox test cannot compare them directly" is FALSE**
> (audit **M-5**; ADR 0033 **D-AQ**). The two constants are unexported and in different packages — **and neither
> fact matters to a parser.** `sizing_option_class_gate_test.go` is already a root blackbox test
> (`package msgin_test`) that parses every non-test `.go` file in all eight modules with `go/parser` (`:280`). An
> **AST invariant test** — read the `const` declarations off the tree, evaluate the shift expressions, assert the
> relation — is **less work than the cross-reference comments revision 1 proposed instead**, and unlike them it
> fails when someone edits one number. §6 AC-3 now specifies it, with a killing mutant. The cross-reference comments
> stay as human-facing explanation; they are no longer the defence, and §8's "unenforceable invariant" item is
> **deleted**.
>
> 🔴 **REVISION 3 GIVES IT SOMETHING TO PARSE, AND BOTH STORES TO PARSE IT IN** (audit **N-4**). Revision 2
> specified a test that locates *"the `maxGroupMembers` default by name"* while specifying the default itself as a
> bare literal in a composite literal (the shipped `maxGroups: 1024` precedent) — so the not-found guard would have
> fired on a faithful implementation of Task 1. **§3.2 / D-AR now require a named `defaultMaxGroupMembers` constant
> in both packages**, and **AC-3.3 parses three files, not two**: `routing/aggregator.go`,
> `adapter/memory/groupstore.go` **and `adapter/database/sql/groupstore.go`**. Covering one store while the other
> carries the identical risk is this increment's own *"fix the class, not the instance"* lesson violated inside the
> fix for M-5.

**The claim-window interaction, which the naive analysis misses.** `memory` counts live + claimed (§3.4), and
`ClaimGroup` sets `g.claimedLen = len(g.msgs)` (`groupstore.go:151`) without shrinking `g.msgs` — the trim happens
in `SettleGroup` (`groupstore.go:173-178`). So between `ClaimGroup` and `SettleGroup`, a group sitting at exactly
`C` **rejects new arrivals for the same key**, even though its live residual is empty. That window is:

- **Bounded** — `Aggregator.release` settles or abandons the claim on every path, including a panic-safe
  defer-abandon.
- **Recoverable** — this is exactly §3.3.1's **leased** arm, which stays **transient**, so the runtime retries and
  the retry succeeds once the settle lands.
- **An accepted consequence, not a defect** — and it is stated on the option's godoc (§4) so it is not
  rediscovered as a bug report.
- **A zero-delay busy-wait under `RetryPolicy{}`**, named rather than glossed: the Nack carries
  `retryDelay == 0`, so the retry spins for the claim window's duration. It is *bounded* where §3.3.1's defect was
  not, and it is the same exposure the **existing** `WithMaxGroups` arm already carries. A caller who cares
  configures `RetryPolicy.Backoff`.

### 3.6 The SQL enforcement point — in the dialect's transaction, not in the store

**`sql.GroupStore` cannot enforce the bound the way `memory` does, and pretending otherwise would ship a bound that
does not bound.** `Add` (`groupstore.go:250-276`) delegates to `GroupDialect.AddMember`, which — per its own
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

**(C) is exact, and the existing dialect code is already shaped for it** — but **not in one shape**. Revision 1 said
*"every dialect's `AddMember` … takes the group row lock first"* and *"the existing `pgRunInTx` wrapper rolls the
transaction back, so nothing is committed."* **Round 1 falsified both** (audit **M-3**, **M-2**). The corrected
picture, re-derived at `d2c69fe` (ADR 0033 **D-AP**):

#### 3.6.1 🔴 Three engines, three serialization mechanisms — the check's placement is per-dialect

| Dialect | Transaction wrapper | What serializes same-key adds | Where the check goes |
|---|---|---|---|
| **postgres** | `pgRunInTx` (`postgres/groupdialect.go:52`) | `INSERT … ON CONFLICT (group_key) DO UPDATE SET group_key = EXCLUDED.group_key RETURNING created_at` — the `DO UPDATE` **locks the conflicting row**; the comment records it *"serializes same-key adds (H1)"* (`:107-110`) | after that upsert **and** after the member upsert |
| **mysql** | `mysqlRunInTx` (`mysql/groupdialect.go:48`) | `INSERT … ON DUPLICATE KEY UPDATE group_key = group_key` (the statement, `:93-96`) — takes an **X lock** on the group row directly; the **comment** recording why `INSERT IGNORE` + `SELECT … FOR UPDATE` self-deadlocks is `:85-92`, not `:93-96` as revision 2 cited (audit **N-13**) | after that upsert **and** after `INSERT IGNORE` on the member table |
| **sqlite** | **`withImmediateConn`** (`sqlite/groupdialect.go:52-77`) — a **dedicated `*sql.Conn`** with raw `BEGIN IMMEDIATE` (`:62`) / `COMMIT` / `ROLLBACK`. **There is no `sqliteRunInTx`.** | `BEGIN IMMEDIATE` itself: a **database-wide write lock**. sqlite's group upsert is `ON CONFLICT (group_key) DO NOTHING` + a **separate** `SELECT created_at` (`:112-124`) — **no row lock, no `RETURNING`** | anywhere inside `withImmediateConn` after the member upsert; the whole-database lock makes placement relative to the group upsert irrelevant |

The atomicity claim of §7.1 is **true for all three, for three different reasons.** sqlite's is in fact the
strongest; it simply cannot be stated once.

#### 3.6.2 🔴 "Nothing is committed" holds only when the DIALECT owns the transaction

`pgRunInTx` and `mysqlRunInTx` have **three** branches, and only the first rolls back:

```go
adapter/database/sql/postgres/groupdialect.go:52-68   (mysql/groupdialect.go:48-64 is identical in shape)
	if b, ok := q.(txBeginner); ok {          // *sql.DB — the dialect owns the tx
		…
		if err := fn(tx); err != nil { _ = tx.Rollback(); return err }   // ← the branch revision 1 described
		return tx.Commit()
	}
	if tx, ok := q.(*stdsql.Tx); ok {         // *sql.Tx — the CALLER owns the tx
		return fn(tx)                         // ← NO rollback. NO commit.
	}
```

**Under a caller-supplied `*sql.Tx`** the over-cap member row is **already inserted into the caller's open
transaction** when the error is returned, and **the caller owns the rollback**. If they commit without matching
`msgin.ErrOverflowDropped`, the cap is exceeded durably.

> 🔴 **WHO CAN REACH THAT BRANCH — revision 2 named a route that does not exist** (audit **N-2**). Revision 2 called
> the `*sql.Tx` Querier *"reachable through `WithSharedTransaction`"*. It is not reachable from `sql.GroupStore`
> **at all**:
>
> ```
> adapter/database/sql/groupstore.go:211   func NewGroupStore(db *stdsql.DB, table string, dialect GroupDialect, opts ...GroupStoreOption)
> adapter/database/sql/groupstore.go:40-42 type groupBase struct { db *stdsql.DB; table string; dialect GroupDialect … }
> adapter/database/sql/groupstore.go:271   rows, err := s.dialect.AddMember(ctx, s.db, s.table, …)
> adapter/database/sql/options.go:201      func WithSharedTransaction(r TransactionResolver) Option      ← Option, NOT GroupStoreOption
> ```
>
> The constructor takes a **concrete `*stdsql.DB`**, the base field is a **concrete `*stdsql.DB`**, every dialect
> call passes `s.db`, and `WithSharedTransaction` belongs to the `NewPollingSource`/`Outbound` **`Option`** family —
> `NewGroupStore`'s entire option surface is `WithGroupLeaseTTL` (`:140`), `WithGroupLockedBy` (`:155`) and a
> logger. **The compiler forbids the route revision 2 described.**
>
> **The real reachability:** the `*sql.Tx` branch is a **`GroupDialect`-level** contract, exercised only by a
> **direct dialect caller** — which is exactly what the `harness` test kit is
> (`harness/groupstore.go:345`, `kit.Group.AddMember(ctx, db, …)` with a `TestKit`-supplied Querier), and what any
> future first- or third-party caller of the SPI would be.

**This is stated, not engineered away.** A library must not `ROLLBACK` a transaction it does not own — that
destroys work it cannot see. The precondition is recorded in two normative places, **corrected in revision 3**:

1. **`GroupDialect.AddMember`'s interface godoc** — the SPI a direct dialect caller actually reads. *(Revision 2
   put the second copy on `sql.WithMaxGroupMembers`'s godoc, where it is unreachable: a caller of that option always
   gets a store that owns its transaction, so the caveat cannot apply to them. That is worse than no caveat.)*
2. **§6 AC-4b**, which drives the branch through the dialect (revision 1 had no coverage for it at all).

**`sql.WithMaxGroupMembers`'s godoc says the opposite, and it is the true statement for that reader:** *"For a
store built by `NewGroupStore`, this bound is unconditionally durable — the store always owns the transaction the
dialect runs in."*

#### 3.6.3 The signature change, and the seven sites it reaches

`AddMember(ctx, q, table, groupKey, msgID string, seq int64, headers, payload []byte)` gains a trailing
`maxMembers int`. `GroupDialect` is documented in its own godoc as *"a pre-1.0 (v0) contract that may still
evolve"* (`groupdialect.go:106`), and the project is **unreleased with no tags and no consumers**
([CLAUDE.md](../../CLAUDE.md) project status).

**Seven sites, not five** (audit **m-5** — revision 1 omitted the interface declaration and the production call):

| # | Site | Kind |
|---|---|---|
| 1 | `adapter/database/sql/groupdialect.go:126` | **interface declaration** — signature + the D-AP enforcement contract in its godoc |
| 2 | `adapter/database/sql/groupstore.go:271` | **production call** — threads `s.maxGroupMembers`; the only site that changes *behavior* |
| 3–5 | `postgres/groupdialect.go:80`, `mysql/groupdialect.go:75`, `sqlite/groupdialect.go:102` | implementations |
| 6 | `harness/groupstore.go:345` | test-kit call |
| 7 | `groupdialect_fake_test.go:137` | test fake — also records `maxMembers` |

**The dialects return `msgin.ErrOverflowDropped` directly**, wrapped in §3.3's shape and marked
`msgin.Permanent` per §3.3.1. No new exported sentinel (ADR 0032 **D-X**, *"mint none"*). The three dialect modules
already depend on the `msgin` root module transitively through `msginsql`, so importing `msgin` adds **zero** net
dependency — `postgres/groupdialect.go` imports only `msginsql` today, and the plan verifies `go mod tidy` leaves
each dialect's `go.mod` unchanged.

**The live snapshot rides out with the error, at no extra query** (§3.3a), and the fetch is bounded — but **not by
editing the shared helper's SQL**:

> 🔴 **`LIMIT maxMembers+1` CANNOT BE PUT ON `*SelectMembers`; THE HELPER HAS THREE CALLERS AND ONLY ONE HAS A CAP**
> (audit **N-5**). Revision 2 said *"each dialect's live-member `SELECT` … gains a `LIMIT maxMembers+1`."*
> Re-derived:
>
> ```
> $ grep -rn "SelectMembers(ctx" adapter/database/sql/{postgres,mysql,sqlite}/groupdialect.go
> postgres/groupdialect.go:121   pgSelectMembers(…, "claimed_epoch IS NULL")            ← AddMember
> postgres/groupdialect.go:163   pgSelectMembers(…, "claimed_epoch = <newEpoch>")       ← ClaimGroup
> postgres/groupdialect.go:307   pgSelectMembers(…, "claimed_epoch IS NULL")            ← ExpiredGroups
> mysql/groupdialect.go:113 / :161 / :298      — identical three-site shape
> sqlite/groupdialect.go:131 / :177 / :314     — identical three-site shape
> ```
>
> **`AddMember` is the only one of the three with a `maxMembers` in scope.** As written the instruction is
> unimplementable at two sites in three; read literally — the `LIMIT` baked into the helper's SQL — it **silently
> truncates `ClaimGroup`'s claimed set** (a legitimately at-cap group releases an **incomplete aggregate**, the
> silent data corruption §5 rejects) **and `ExpiredGroups`' recovery set** (the reaper drops members). Neither loss
> is visible to any acceptance criterion in this spec.

**THE CONTRACT.** Each of `pgSelectMembers` / `mysqlSelectMembers` / `sqliteSelectMembers` gains a **private
`limit int` parameter, where `0` means unlimited** and is what the helper emits no `LIMIT` clause for.
**`AddMember` is the only caller that passes a non-zero value** (`maxMembers+1`); `ClaimGroup` and `ExpiredGroups`
pass **`0`** and keep their current, unbounded behavior exactly. The parameter is unexported, so it adds no
class-gate key (§6 AC-8.7's reasoning, applied to a helper rather than a method).

**This is a constraint, not a convention, and §6 AC-9 row 15 mutation-proves it:** pass `maxMembers+1` from
`ClaimGroup` ⇒ an over-cap claimed group is truncated ⇒ a `harness` case fails.

On overflow the just-upserted member is filtered out of the materialized `[]MemberRow` in Go, and the remaining rows
are returned **with** the error. **That equals the post-rollback live set whenever the live count was ≤ cap before
the add** — the normal path's precondition. It does **not** hold on the AC-4b path, where a caller-owned `*sql.Tx`
deliberately keeps `cap+1` rows committed in the caller's transaction; there the returned rows are the *would-be*
post-rollback set, and the caller owns what is actually durable (§3.6.2).

**The error path costs one extra round-trip, and it is a stated cost** (audit **m-6**). `sql.GroupStore.Add` routes
every dialect error through `classifyQueryErr` (`groupstore.go:273` → `:91-96`), which runs a `SchemaExists` query
before returning. The sentinel and the `Permanent` marker both survive (the error is returned unchanged when the
table exists — §6 AC-4c asserts it), but the query is real. Under revision 1's transient classification it was paid
**on every iteration of an infinite spin**; under §3.3.1 it is paid once. `Add` must also **propagate §3.3a's
snapshot past this call site** rather than discarding it with the current `return nil, …`.

### 3.7 The SPI states the bound as a contract requirement

`msgin.MessageGroupStore.Add`'s godoc — **`groupstore.go:38-45`**, the doc comment at `:38-44` and the method
declaration at `:45`, not `:41-52` as revision 1 cited (audit **m-2**) — gains a stated requirement, so a
third-party store (a future Redis, NATS or pgx group store) implements the bound rather than rediscovering the
defect (ADR 0033 **D-AH**):

> An implementation MUST bound the number of members it retains for a single group, and MUST report an `Add` that
> would exceed that bound as `msgin.ErrOverflowDropped` rather than growing without limit. The Aggregator's release
> strategy cannot supply this bound: three of its four paths are a caller-supplied closure or a message header —
> and the store is the only site that can refuse a member *before* retaining it.
>
> The exact set counted is implementation-specific and MUST be stated in the implementation's godoc —
> `adapter/memory` counts retained members (live + claimed); `adapter/database/sql` counts live members, because
> claimed members are retained by the database rather than by the process.
>
> An implementation SHOULD mark the rejection `msgin.Permanent` when the group cannot drain itself, and leave it
> transient when a claim is in flight that will drain it (§3.3.1). A bare transient rejection of a group that will
> never drain **hot-spins** under the default `msgin.RetryPolicy`, which has neither a `MaxAttempts` nor a
> `Backoff`.
>
> An implementation MAY return the group's current LIVE snapshot **alongside** the overflow error. When it does, the
> Aggregator re-evaluates the release strategy against that snapshot and releases the group if it is ready, so a
> full-but-releasable group is not deadlocked by its own bound (§3.3a). Returning `(nil, err)` remains valid and is
> what every pre-existing implementation does.
>
> When the Aggregator acts on that snapshot it may only ever **DOWNGRADE** the implementation's classification —
> permanent to transient, on positive evidence that the group drained. It never upgrades a transient rejection to
> permanent. An implementation may therefore treat its own classification as the **conservative floor**: a bug in
> the Aggregator's drain path costs a retry, never a message the implementation marked recoverable.

**The MAY is deliberate.** A store that cannot cheaply produce the live set on the rejection path must not be
forced to; `Handle`'s `group == nil` arm keeps it working, it simply forgoes the self-healing.

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
(`aggregator.go:222`), so the note goes on **`msgin.HeaderSequenceSize`**'s declaration — **`message.go:24`**,
resolved rather than left as *"headers.go or wherever"* — and on `routing.NewAggregator`'s godoc
(`aggregator.go:327`), which is where a caller relying on the default release path actually looks.

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
closure. What changes is the **failure mode** at the boundary.

> 🔴 **REVISED in revision 2 (audit B-1).** Revision 1 finished that sentence *"…and this spec makes that failure
> loud, typed, **retryable** and named rather than a silent guess."* That was the load-bearing claim and it was
> **false under the shipped defaults**: the transient arm neither logged nor dead-lettered — it spun (§3.3.1).
> **A default cap whose boundary behavior is an unlogged infinite spin is not a safe default; it is a worse defect
> than the one it replaces.** This argument now depends on **§3.3.1**, not on §3.3 alone: the boundary is loud (a
> WARN on the dead-letter fallback), typed, **terminal rather than retryable** when the group cannot drain, and
> **self-healing** rather than terminal when it can (§3.3a). **If §3.3.1 is reversed, this decision falls with it**
> and the honest fallback is opt-in — an opt-in bound at least does not convert an unbounded group into a spinning
> one.

**DECIDED: ship the default (ADR 0033 D-AJ, as amended by D-AM).** The 200,000-member caller sets
`WithMaxGroupMembers(1<<18)` and reads a godoc sentence explaining the trade-off. The alternative leaves every
caller who has not read this spec running the unbounded version.

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

**With the cap in place, a group whose release predicate can NEVER be satisfied is memory-BOUNDED but permanently
STUCK.** It holds exactly `maxGroupMembers` members forever; every subsequent member for that key is rejected and
**terminated at the invalid-message sink, or the dead-letter sink when none is configured** (§3.3.1). It is never
released, never settled, never garbage.

**This is accepted (ADR 0033 D-AK), and it is a strict improvement on both axes that matter:**

| | Before | After (revision 2) |
|---|---|---|
| Process memory | unbounded — grows until OOM or 48.3 GiB of churn | bounded at `maxGroupMembers` members |
| Observability | silent, until the process dies | one typed, named `ErrOverflowDropped` per rejected member at the operator's sink — and it works on the shipped zero-value `RetryPolicy`. **With no sink at all: one WARN per message, then an Ack** (`consumer.go:1049`, `:1073`). **With a dead-letter sink but no invalid sink: the fallback WARN fires once per CONSUMER, not per message** (`consumer.go:968-973`) — §3.3.1's table (audit **N-11**) |
| Liveness of a group whose predicate is unsatisfiable | never releases | **still never releases** |
| Liveness of a group that IS releasable but whose release failed | re-fires on the next member or retry | **re-fires, restored by §3.3a** |

> 🔴 **REVISED in revision 2.** Revision 1 wrote *"rejected, retried and dead-lettered"* and claimed liveness was
> **unchanged**. Both were wrong. *"Retried and dead-lettered"* assumed a `RetryPolicy` that has a `MaxAttempts`
> and a `DeadLetter`; **the shipped zero value has neither**, so the real behavior was an unlogged infinite spin
> (audit **B-1**, §3.3.1). And *"liveness unchanged"* was false for a group that **is** releasable but whose
> release failed once: revision 1's cap check removed the *"a retry / next member re-releases"* recovery
> `AbandonGroup`'s own godoc promises, creating a **new** permanent deadlock (audit **M-6**). §3.3a restores it —
> which is why the table above now has **four** rows rather than three.

**The cap does not, and is not intended to, provide liveness — but after §3.3a it no longer REMOVES any.** The
remedy for a genuinely stuck group is the one that already ships: `routing.WithGroupTimeout` +
`routing.WithExpiredGroupChannel`, which expires the group and routes its partial members to a channel the caller
owns.

> 🔴 **"That remains opt-in" was false for a durable store, and is corrected here** (audit **N-3**). The **expiry**
> route is opt-in in both stores — it needs a non-zero cutoff, which only `WithGroupTimeout` supplies. The
> **reaper itself** is not: `sql.GroupStore.RecoverInterval()` returns the lease TTL, so `Aggregator.Run` sweeps
> every 5m by default and the Aggregator's godoc calls `Run` **required** for a durable store (§1.2.1). What that
> default sweep can drain is **crashed-lease groups only** — which is why the classification's premise is *"nothing
> drains an **unleased** group without an expiry cutoff"* (§3.3.1) and not *"the reaper never sweeps."*

§3.3.1 records the cost of the opt-in expiry route: a caller who *has* set a timeout sees messages dead-lettered
that their reaper would eventually have admitted — **and, for `sql`, so does a caller who has set nothing**, in the
stranded-lease case §3.3.1's counter-example box states.

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
3. **`memory.WithMaxGroups` is unchanged** — group **count** and group **members** are different quantities that
   share **the same ceiling** (`1<<20`, deliberately — §3.2 picks it *because* it matches `maxGroupsCeiling`) and
   have **different defaults** (1024 and 65,536). *(Revision 1 said "different ceilings (`1<<20` and `1<<20`)" —
   the same number twice, asserting the opposite of the decision it was describing. Audit **m-4**.)*
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
   counts** (§3.4 — the two stores differ, and each godoc says which, **including that the rendered count is
   "members retained at the moment of the check" and therefore differs by one between the stores** — §3.4a's box),
   **the permanent-vs-transient classification and why** (§3.3.1 — including that a permanent rejection terminates
   at the invalid-message or dead-letter sink rather than retrying, **and that with neither sink configured the
   message is WARNed and ACKed, so the source drops it**), and — for `memory` — the claim-window rejection of §3.5,
   named as a zero-delay busy-wait under `RetryPolicy{}`. **For `sql` only:** the bound is **unconditionally
   durable** for a store built by `NewGroupStore`, which always owns its transaction (§3.6.2) — the caller-owned-
   transaction caveat belongs on the SPI (item 6), not here.
2. **`defaultMaxGroupMembers` and `maxGroupMembersCeiling`** (both packages, **both named constants — D-AR**) — a
   constant godoc in the shape of `maxGroupsCeiling` (`groupstore.go:55-62`) and `completionSizeCeiling`
   (`aggregator.go:25-33`): what each value means, why this number, the cost basis (time, not bytes — §1.2).
   **The cross-reference naming the `default ≥ completionSizeCeiling` invariant goes on the DEFAULT constant, not
   on the ceiling** — the ceiling is not part of the invariant. It names `routing.completionSizeCeiling`, the other
   store's twin, and §6 AC-3.3's test. *(Revision 2's §4, Plan Task 1 Step 7 and Plan Task 3 Step 5 named three
   different homes — audit **N-4**.)*
3. **`msgin.MessageGroupStore.Add`** (`groupstore.go:38-45`) — §3.7's contract paragraph, MUST/SHOULD/MAY intact.
4. **The three release-path godocs** — §3.8, plus `msgin.HeaderSequenceSize` at `message.go:24` and
   `routing.NewAggregator` at `aggregator.go:327`.
5. **`memory.GroupStore.Add`'s existing bare `ErrOverflowDropped`** (`groupstore.go:124`) — upgraded to §3.3's
   wrapped shape **in the same commit**, and its godoc (`groupstore.go:112-117`) updated to name both overflow
   arms rather than only the group-count one.
6. **`GroupDialect.AddMember`** (`groupdialect.go:126`) — three edits, and **the first is a CORRECTION, not an
   addition**:
   - 🔴 **Correct the shipped sentence at `groupdialect.go:109-113`**, which still reads *"takes the **GROUP ROW
     LOCK** (SELECT ... FOR UPDATE or equivalent) BEFORE reading or writing any member row"* — **the exact claim
     §3.6.1 falsified for sqlite** (audit **N-9**). Replace it with: *"serializes concurrent same-key adds — by a
     group-row lock on postgres/mysql, by `BEGIN IMMEDIATE`'s database-wide write lock on sqlite (D-AP)."*
     Revision 2 corrected this bundle's prose and left the shipped godoc — the one a third-party dialect author
     reads — asserting the falsified mechanism.
   - The **in-transaction enforcement contract** (the count, the cap, the rollback).
   - **§3.6.2's precondition**, whose normative home this is (§3.6.2, audit **N-2**): the bound is enforced by
     rollback only when the dialect owns the transaction; a **direct dialect caller** supplying a `*sql.Tx` owns the
     rollback and MUST treat `msgin.ErrOverflowDropped` as a rollback trigger.
7. **`routing.Aggregator.Handle`** — §3.3a's snapshot-with-error branch: what a non-nil group beside a non-nil
   error means, why the release is re-evaluated rather than the member re-admitted, the **downgrade-only** direction
   rule (§3.3a.1), and — explicitly — **why `claim == nil` returns a retryable error here where the success path
   returns `nil` at `aggregator.go:438-439`** (the member was never stored, so `nil` would Ack an unstored message).
   Without that sentence the divergence reads as a bug and gets "fixed."

> **The recurring failure mode this list exists to prevent** — the project's stored lesson *"docs can contradict
> the code they describe"*: all three fix rounds in Plan 028 were godoc, not logic, and **round 1 of this audit
> found the same pattern again** (three of its four MINOR line-citation findings, plus §3.12.3's self-contradiction).
> Read each sentence against the constructor, not for plausibility. In particular, `groupstore.go:112-117`
> currently says *"A new key beyond WithMaxGroups returns msgin.ErrOverflowDropped"* — after this increment that
> sentence is **incomplete**, not wrong, and incompleteness in an overflow contract is how the next audit round
> starts.

## 5. Alternatives rejected

| Alternative | Why rejected |
|---|---|
| **Bound at the release decision** — validate the closure, or cap what a release strategy may return | §1.1: opaque in paths 2 and 3, and path 4 has no code to validate. Incompletable by construction. |
| **Widen the class gate's AST scan to `*ast.FuncType` parameters** | §1.4: catches `WithReleaseWhen`, misses `WithReleaseStrategy` (named type) and cannot express path 4 at all. It would also have to decide *which* func-typed options are sizing knobs — a judgement `go/ast` cannot make. A gate that covers 1 of 3 while reading as complete is worse than a stated limitation. |
| **Force-release the group when it hits the cap** | Emits an incomplete aggregate as if it were complete — silent data corruption dressed as liveness, and it overrides the caller's release contract. Same family as Spec 016 §5's rejected *"clamp silently to the ceiling."* |
| **Evict the oldest member at the cap** | Silent data loss. `WithMaxGroups`'s own godoc already rejects this reasoning for groups: *"rather than evicting a partial group (which would be silent data loss)."* |
| **Drop the over-cap member silently (return nil error)** | The caller's source `Ack`s a message that was never aggregated. Fails the delivery-guarantee invariant. |
| ~~**`Permanent`-wrap `ErrOverflowDropped`**~~ — **ADOPTED in revision 2, conditionally** | Revision 1 rejected this on the ground that *"an over-cap `Add` genuinely can succeed after a settle or an expiry."* Audit **B-1** showed the alternative is an unlogged infinite hot spin on the shipped `RetryPolicy{}`, and that "can succeed later" is false in the default configuration. §3.3.1 adopts the `Permanent` wrap **for the not-leased case only**; the leased case stays transient, which preserves the true form of the original objection. The **existing group-count arm keeps its transient classification** — the original objection's second half stands, and no shipped behavior changes there. |
| **`Permanent`-wrap the group-count arm too** | Symmetry is not worth a behavioral change to shipped code with no defect behind it. A group map at `WithMaxGroups` drains when *any* group settles, so its retry genuinely can succeed — the property §3.3.1 requires for a transient classification, and one the group-count arm actually has. |
| **Keep the rejection transient and require `RetryPolicy.Backoff`** | Pushes a mandatory configuration onto every caller to make a default safe — the inverse of [CLAUDE.md](../../CLAUDE.md)'s Sensible-defaults gate. Backoff also only slows the spin; it never terminates it, and it still never logs. |
| **Return `nil` with the live snapshot on overflow, letting `Handle` release and swallow the rejection** | The member was never stored, so the source Acks a message that was never aggregated — the same delivery-guarantee violation as *"drop silently"* below. §3.3a returns the snapshot **with** an error for exactly this reason. |
| **Let `Handle` count the members and enforce the cap** | §3.1 reason 1: by the time `Handle` holds `Add`'s snapshot the member is already appended and already retained. It would bound the reported size, not the heap — the false-safety inversion §3.6 rejects for SQL enforcement (A). |
| **Have the dialect `ROLLBACK` a caller-supplied `*sql.Tx`** | §3.6.2: a library must not roll back a transaction it does not own; it would destroy work it cannot see. Naming the precondition is the correct remedy, in the same family as §7.1's configuration-coherence requirement. |
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

**Each of the four cases additionally asserts `msgin.IsPermanent(err) == true`** — the group is not leased in any
of them, so §3.3.1's permanent arm is the one under test. **Killing mutant:** drop the `msgin.Permanent` wrap ⇒ all
four fail. Without this assertion the four cases pass against revision 1's hot-spinning implementation.

**AC-1b — the id-less redelivery does NOT deadlock the group (§3.3a). NEW in revision 2; this is the case that
would have caught audit M-6, and no other case does.** Over a `memory.GroupStore` with `WithMaxGroupMembers(4)` and
a release strategy that fires at 4:

> 🔴 **THE FIXTURE IS PINNED BY NAME, AND ASSERTED, BECAUSE THE OBVIOUS CONSTRUCTOR CANNOT PRODUCE IT** (audit
> **N-6**). `msgin.New` **always** stamps an id — `if cfg.id == "" { cfg.id = NewID() }` (`message.go:178-180`) —
> so even `msgin.New(p, msgin.WithID(""))` yields an id-**ful** message. The **only** id-less route in the public
> API is **`msgin.NewMessage(payload, headers)`** (`message.go:198`), documented *"WITHOUT stamping
> msgin.message-id/msgin.timestamp"*, called with headers carrying **no `HeaderMessageID`**.
>
> An implementer reaching for `msgin.New` writes an id-ful test that **passes while proving nothing** — the dedup
> branch (`adapter/memory/groupstore.go:130-131`) returns the snapshot with a nil error and `Handle` reaches the
> predicate anyway, so M-6's deadlock is never entered — and **AC-9 row 3c's mutant survives**, because folding the
> cap check back inside `if id != ""` changes nothing for an id-ful message. This is the one case in the spec whose
> entire discriminating power is a property of its **input**.

1. Add four **id-less** messages via `Handle`, built with **`msgin.NewMessage(payload, headers)`** and headers
   carrying no `HeaderMessageID`. **`require.Empty(t, m.ID())` is the case's FIRST assertion**, before any
   `Handle` call — the fixture is asserted, never assumed. Arrange the release to **fail** on its first attempt (an
   output channel whose `Send` returns an error once, then succeeds).
2. The 4th `Handle` returns the release error; the group holds 4 members, unleased, complete.
3. **Re-`Handle` the same id-less message.** Assert: the release **fires again and succeeds** (observed on the
   subscriber), and the returned error is `msgin.ErrOverflowDropped` with `msgin.IsPermanent(err) == false` —
   §3.3a's `overflowRetryable` shape, naming `routing.Aggregator.Handle`.
4. **Re-`Handle` once more.** Assert it now **succeeds** (nil error): the group drained, so the member is admitted.

**Killing mutants:** (a) have `Add` return `(nil, err)` instead of `(live, err)` ⇒ step 3's release never fires;
(b) delete `Handle`'s snapshot branch ⇒ same; (c) return `nil` from step 3 instead of the transient error ⇒ step 4
never runs and the member is silently lost.

**AC-1c — a rejected member leaves no trace in the dedup set (§3.4a).** With `WithMaxGroupMembers(4)` and four
id-**ful** members present: `Add` a fifth with id `"e"` ⇒ `ErrOverflowDropped`. Then drain the group, then `Add`
`"e"` again ⇒ it is **admitted**, not swallowed as a duplicate. **Killing mutant:** place the cap check below
`g.ids[id] = struct{}{}` ⇒ the second `Add` returns the dedup no-op and `"e"` is lost forever.

**AC-2 — the ceiling is the boundary, asserted on both sides, for both stores.** Per store, a table with
`WithMaxGroupMembers(1<<20)` (accepted — **constructor only**, no members added) and `WithMaxGroupMembers(1<<20+1)`
(rejected), plus the lower arm `WithMaxGroupMembers(0)`.

**AC-2b — the rendered message is true at both ends.** Per store, two cases asserting the **full** render, not
merely `errors.Is`: the site name, the offending value and the `[1, 1048576]` range — one at `ceiling+1`, one at
`0`. This is Spec 016 AC-2b's rule, and it is the case that would have caught revision 2's `"0 exceeds 1048576"`.

**AC-2c — the OVERFLOW render is true, prefix included, AND the two stores' counts are pinned separately.**
Separate from AC-2b, which covers construction-time `checkRange`. Per store, assert the **full** runtime render of a
permanent over-cap rejection, with `WithMaxGroupMembers(4)`:

```
memory: msgin: permanent: msgin: message dropped by overflow policy: memory.GroupStore.Add: group "k" holds 4 members, limit 4
sql:    msgin: permanent: msgin: message dropped by overflow policy: sql.GroupStore.Add: group "k" holds 5 members, limit 4
```

> 🔴 **THE `sql` COUNT IS `cap+1`, DELIBERATELY, AND MUST BE PINNED** (audit **N-8**). `memory` checks **before**
> the append; the dialects check **after** the member upsert (§3.6.1 — required, so an idempotent re-add at cap
> stays a no-op). Both render *"members retained at the moment of the check"* (§3.4a's box). Revision 2 pinned only
> `memory`'s render, so `sql` could render anything and stay green. **Killing mutant for this half:** normalise
> `sql` to `len(members)-1` ⇒ the `sql` case fails.

The doubled `msgin:` is the shipped `permanentError.Error()` prefix (`reliability.go:13`) over the sentinel's own
text; it is asserted as written rather than assumed away. The **leased** twin (`memory` only — every `sql` over-cap
rejection is the not-leased case, §3.3.1) asserts the same string *without* the `msgin: permanent: ` prefix.
**Killing mutants:** drop the wrap ⇒ the permanent case fails; add the wrap to the leased arm ⇒ the leased case
fails.

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
3. **🔴 THE INVARIANT ITSELF, MECHANICALLY ENFORCED — an AST test. NEW in revision 2 (audit M-5; ADR 0033 D-AQ);
   REPAIRED in revision 3 (audit N-4).** A root blackbox test (`package msgin_test`) that:
   - parses **three** files with `go/parser` — `routing/aggregator.go`, `adapter/memory/groupstore.go` **and
     `adapter/database/sql/groupstore.go`** — the identical technique `sizing_option_class_gate_test.go:280`
     already uses, and one to which **unexportedness and package boundaries are irrelevant**;
   - locates `const completionSizeCeiling` and **`const defaultMaxGroupMembers` in EACH store package**, by name,
     on the `*ast.GenDecl` tree, **failing loudly if any of the three declarations is not found** (so it cannot
     pass vacuously on a zero value);
   - evaluates the `1 << N` `*ast.BinaryExpr` values;
   - asserts `defaultMaxGroupMembers >= completionSizeCeiling` **for both stores**, with a failure message naming
     the constants, their files and their values.

   > 🔴 **Two repairs, both from audit N-4.** (a) **There must be a constant to parse.** `adapter/memory`'s shipped
   > precedent declares its *ceiling* as a `const` (`:62`) and its *default* as a bare literal
   > (`maxGroups: 1024`, `:98`); a faithful implementation of that precedent gives this test nothing to find and
   > **fires the not-found guard**. §3.2 / **D-AR** therefore require a named `defaultMaxGroupMembers` in both
   > packages. (b) **`sql` carries the identical risk** — same default, same Aggregator, same `WithCompletionSize`
   > — so covering only `memory` is the *"fix the class, not the instance"* lesson violated inside the fix for M-5.

   **Killing mutants:** (a) change any of the three literals so a relation is violated ⇒ fails; (b) rename one
   constant without updating the test ⇒ the not-found guard fires rather than the test silently passing;
   (c) **delete the `sql` file from the parse set ⇒ the test must fail, not shrink to two assertions** (the
   file list is asserted, not iterated over whatever happens to exist).

> **What AC-3 does NOT do, stated rather than left as a gap.** It does **not** execute a 65,536-member group.
> Spec 016 §1.4 measured that at **8.6 s and 48.3 GiB of allocation churn**, and `completionSizeCeiling`'s shipped
> godoc (`aggregator.go:25-32`) states *"why no test grows a group to it."* That limitation is real and stays.
>
> 🔴 **What revision 1 said here was WRONG and is deleted:** *"the invariant … is **not** mechanically enforced —
> both constants are unexported and in different packages, so no blackbox test can compare them."* Item 3 above
> enforces it, and the cross-reference comments are now human-facing explanation rather than the defence. §8's
> corresponding open item is **closed**.

**AC-4 — the SQL bound is enforced IN the transaction and nothing is committed.** Against a real database via the
existing `dbtest`/`harness` Docker-backed conformance runner (`use-testcontainers`; never a mock):

1. `Add` the `cap+1`-th live member ⇒ `errors.Is(err, msgin.ErrOverflowDropped)`.
2. **The rollback is asserted, not assumed:** a subsequent `ClaimGroup` returns exactly `cap` members, and a direct
   member-row count over the table equals `cap`. Without this second half, enforcement (C) is indistinguishable
   from enforcement (A) — §3.6's whole argument.
3. **Idempotency is preserved at the boundary:** re-`Add`ing an **existing** member id while the group sits at
   exactly `cap` is a **no-op returning the unchanged snapshot**, not an overflow. This case fails against the
   natural implementation that counts before checking `ON CONFLICT DO NOTHING`.
4. **The live snapshot rides out with the error (§3.3a):** the returned `MessageGroup` is **non-nil** and holds
   exactly `cap` members — the post-rollback live set, with the rejected member filtered out. **Killing mutant:**
   return `msginsql.GroupRows{}` with the error ⇒ fails.
5. **The rejection is `Permanent`:** `msgin.IsPermanent(err) == true` (§3.3.1 — a `sql` live set is by definition
   unclaimed, so every `sql` over-cap rejection is the not-leased case). **Killing mutant:** drop the wrap ⇒ fails.

**AC-4b — the caller-owned-transaction precondition is exercised, AT THE DIALECT. NEW in revision 2 (audit M-2);
entry point corrected in revision 3 (audit N-2).** Revision 1 had **no** coverage for the `*sql.Tx` branch of
`pgRunInTx`/`mysqlRunInTx` (`return fn(tx)` — no rollback). Against a real database:

1. `BeginTx` on the caller's side, then call **`kit.Group.AddMember(ctx, tx, …)` — the DIALECT, directly**, for the
   `cap+1`-th member.

   > 🔴 **Not `GroupStore.Add`.** `sql.GroupStore` cannot reach this branch: `NewGroupStore` takes a concrete
   > `*stdsql.DB`, `groupBase.db` is a concrete `*stdsql.DB`, and `:271` always passes `s.db` (§3.6.2). Revision 2's
   > *"pass the `*sql.Tx` as the Querier; `Add` …"* has no executable reading. The `*sql.Tx` branch is a
   > **`GroupDialect`-level** contract and the harness is exactly the direct dialect caller that exercises it.

2. Assert `errors.Is(err, msgin.ErrOverflowDropped)` — the rejection still fires.
3. Assert that **the member row IS present inside the open transaction** (a `SELECT` on that same `*sql.Tx` sees
   `cap+1`), and that after the caller's own `Rollback` it is gone. **This documents the precondition as tested
   behavior rather than as a hope**, and is the executable form of §3.6.2.
4. Assert the **contrapositive for the shipped store**: a `sql.GroupStore` built by `NewGroupStore` and driven to
   the same overflow leaves exactly `cap` rows committed — i.e. the bound **is** unconditionally durable there
   (§3.6.2's godoc sentence, made executable). This is AC-4.2's row count, re-used as the pair to this case.

**AC-4c — the overflow error survives `classifyQueryErr` intact (audit m-6).** Against a real database with the
table present: assert `errors.Is(err, msgin.ErrOverflowDropped)` **and** `msgin.IsPermanent(err)` after the error
has passed through `sql.GroupStore.Add`'s `classifyQueryErr` call (`groupstore.go:273`), and that the returned
group snapshot is not discarded by that call site. **Killing mutant:** have `classifyQueryErr` return
`b.schemaNotReady()` unconditionally ⇒ both assertions fail.

**AC-5 — one conformance case per dialect, in `harness`.** The shared dialect test kit
(`adapter/database/sql/harness/groupstore.go`) gains a member-cap case so **postgres, mysql and sqlite** are each
held to the contract, rather than testing whichever dialect was convenient. **The three dialects enforce at three
different points (§3.6.1)**, so the harness case asserts *behavior*, never statement order.

> **The harness must not acquire an exported function with an `int`/`int64` parameter**, or half 1 of the class
> gate gains an unlisted key from a leaf module — which the gate's own ROOT-MODULE IMPORT BOUNDARY limitation says
> is an unfixable failure (half 2 cannot import a leaf module). Verified today:
> `grep -rnE "^func [A-Z][A-Za-z]*\(.*\b(int|int64)\b" adapter/database/sql/harness/*.go` returns nothing. Keep it
> that way: the cap value travels in the existing `TestKit`, not as a new exported parameter.

**AC-6 — no test grows a group to any ceiling.** A standing constraint, restated as an AC because it is the one a
future contributor will violate: every group-growth test uses a cap of ≤ 16 members. The ceiling values are
exercised by **constructors only**.

**AC-7 — the SPI's MUST-report clause, asserted through the interface type.**

> 🔴 **REVISION 2'S AC-7 WAS NOT EXECUTABLE** (audit **N-12**). It said *"a case asserting **§3.7's requirement**"*
> — but §3.7 is a **four-clause** paragraph (MUST-bound, MUST-report, SHOULD-`Permanent`, MAY-snapshot), and three
> of the four are already covered: the MUSTs by AC-1/AC-4, the SHOULD by AC-1/AC-4.5, the MAY by AC-1b/AC-4.4.
> Naming a section instead of a clause is the unexecutable-AC defect Plan 029's audit found in **five consecutive
> rounds**, and §6's own opening sets executability as the standing bar. Named, it is:

`memory.GroupStore` and `sql.GroupStore` each get **one** case for §3.7's **MUST-report** clause, with the store
**held in a `msgin.MessageGroupStore` variable** rather than its concrete type:

```go
var store msgin.MessageGroupStore = /* memory.NewGroupStore(...) | sql.NewGroupStore(...) */
_, err := store.Add(t.Context(), "k", overCapMessage)
require.ErrorIs(t, err, msgin.ErrOverflowDropped)
```

**What this uniquely buys** is the interface-typed drive: the case is copyable verbatim by a third-party
implementer, and it proves the contract is satisfiable through the SPI rather than only through a concrete type.
**Killing mutant:** change either store to return a bare, non-wrapping error ⇒ the `ErrorIs` fails. *(If a future
reviewer judges the interface-typed drive to be a style requirement rather than an assertion, the correct move is
to delete AC-7 and fold the requirement into AC-1 and AC-4 — not to restore a section-level AC.)*

**AC-8 — the class gate is updated, and its blind spot is STATED, not silently widened.**

1. **Half 1 gains two keys.** `memory.WithMaxGroupMembers` and `sql.WithMaxGroupMembers` are exported,
   `Recv == nil`, `int`-parameter functions in **root-module** packages, so the AST scan finds them: **17 → 19**.
   Half 1 is **exact set equality in both directions** (`assert.Equal(t, want, found, …)` at `:321-324`), so
   `sizingConformanceKeys` must gain both **in the same commit as the option itself** — see the ordering box below.
2. **Half 2 gains two rows in the `fixed` arm.** Arm totals become
   **11 fixed + 1 rejects + 3 deferred + 6 safe = 21 rows = 19 AST keys + 2 manual rows.** Re-derive from the arm
   table; do not increment.

   > 🔴 **THE NEW ROWS USE `1<<30`, NOT `1<<62`** (audit **M-1**). Revision 1 said *"both constructors reject
   > `1<<62`"*. [Plan 030](../plans/030-post-029-maintenance.md) Task 2 has since split the literal **by arm**:
   > `fixed`/`rejects` → **`1<<30`** (fits an `int32`, so the file compiles on `GOARCH=386`, and renders the
   > architecture-independent decimal **1073741824**); `deferred` → **`1<<62`**, unchanged (those take `int64` and
   > were never part of the defect — **do not convert them**); `safe` → **`math.MaxInt`** (must stay maximally
   > absurd, or the int32-truncation probe silently stops running). Following revision 1 verbatim re-breaks 32-bit
   > compilation, and nothing in the per-task gate builds for 386.

3. **A NEW accepted limitation is added to the gate's header**, alongside the four it already states: *"a bound
   that does not arrive as an integer parameter is invisible — a func-typed option (`*ast.FuncType`), a named
   func type (`*ast.Ident`), or a threshold read from a message header (no parameter at all). Spec 017 §1.4
   enumerates the three that exist today and moves their enforcement to the store instead."*
4. **The existing ROOT-MODULE IMPORT BOUNDARY limitation is EDITED, not merely extended** (audit **M-7**). Its
   package list at `:107-108` — *"endpoint, adapter/http, adapter/memory, channel, resilience, routing"* — does
   **not** include `adapter/database/sql`, so `sql.WithMaxGroupMembers` falsifies it. This is a corrected claim,
   not a stale number.
5. **Ten sites state a count, and two of them are executable.** `:19`, `:38`, the per-arm counts at `:47`/`:55`/
   `:61`, `:83-85` (including the derived `44` → **46**), `:92`, `:107-108`, `:176`/`:210`, `:322` (assertion
   message), **`:335` — `require.Equal(t, 27, methodCount, …)`** and **`:753-754` —
   `require.Len(t, tests, 19, …)`**. The last two fail the suite, not a reader. `:691`'s *"burst is the 17th key,
   positional"* is an **ordinal** into `sizingConformanceKeys` and survives only if the two new keys are appended
   **after** `resilience.NewTokenBucket`.
6. **`methodCount` stays 27** (audit **m-10**). `GroupDialect.AddMember` gains an `int` parameter, but all three
   dialect implementations **already** carry `seq int64` and are therefore already counted — the gate's own header
   names `postgresGroupDialect.AddMember` among them (`:84-86`). Adding a parameter to an already-matching method
   changes nothing. **Do not bump the `require.Equal(t, 27, …)`.**
7. **The `GroupDialect.AddMember` signature change adds an `int` parameter to a METHOD**, which the ratified
   `Recv == nil` boundary excludes (Spec 016 §2.0 / ADR 0032 **D-AA**). Under D-AB's criterion it is **not** a class
   member — `maxMembers` is the bound itself, not a quantity bounded by something else — so no manual row is
   required. **Stated explicitly** so the next audit does not have to re-derive it.

> 🔴 **ORDERING IS PART OF THE CRITERION (audit B-2).** Because half 1 is exact set equality, **the moment
> `memory.WithMaxGroupMembers` exists on disk the ROOT module's suite is red.** The conformance key and row
> therefore land in the **same commit** as the option they describe — the `memory` pair with the `memory` option,
> the `sql` pair with the `sql` option — and "observe the RED first" is a **within-task TDD step** (write the row,
> watch it fail, write the option), never a cross-task condition. Revision 1 deferred all gate edits to a final
> task, which would have left **six of nine tasks committing a red suite** in violation of the plan's own
> green-unit constraint and of CLAUDE.md's per-task-commit pre-authorization.
>
> **Plan 032 serializes on this file** ([Spec 018](018-byte-cap-ceilings.md)), which targets the same
> `sizingConformanceKeys` slice and the same arm table. Whichever increment lands second re-derives the arm table
> **from the tree**, never from a number written in its own plan (audit **B-3**).

**AC-9 — every new branch is covered and mutation-proven.** Under [CLAUDE.md](../../CLAUDE.md)'s test-coverage
gate, each of these is a hot-path or typed-error branch needing a named covering case, and — per the stored lesson
*"mutation-test every new assertion"* — a mutant that the case kills:

| # | Branch | Site | Killing mutant |
|---|---|---|---|
| 1 | member-cap reject | `memory.GroupStore.Add`, §3.4a's position | delete the arm ⇒ AC-1's four cases fail |
| 2 | member-cap **pass** (the group is under cap) | same | invert to `>` ⇒ AC-3.1's release case fails |
| 3 | idempotent re-add at exactly cap | `memory.GroupStore.Add`, the `g.ids` seen branch | move the cap check **above** the `seen` lookup ⇒ AC-4.3's `memory` twin fails |
| 3b | **rejected member leaves no trace in `g.ids`** | `memory.GroupStore.Add`, §3.4a | move the cap check **below** the `g.ids` insert ⇒ **AC-1c** fails (silent loss) |
| 3c | **the cap check runs on the id-less path** | same | fold the check back inside `if id != ""` ⇒ **AC-1b** regains an unbounded append |
| 4 | `checkRange` upper arm | `memory.NewGroupStore` | delete ⇒ AC-2 fails |
| 5 | `checkRange` lower arm | `memory.NewGroupStore` | change `1` to `0` ⇒ AC-2's `0` case fails |
| 6 | `checkRange` upper/lower arms | `sql.NewGroupStore` | as 4/5 ⇒ AC-2 `sql` half fails |
| 7 | dialect in-tx overflow ⇒ rollback | each dialect's `AddMember` | return the error **after** the tx commits ⇒ AC-4.2's row count fails |
| 8 | dialect under-cap pass-through | each dialect's `AddMember` | off-by-one to `>=` ⇒ AC-3.1's `sql` twin fails |
| **9** | **`!g.leased` ⇒ `Permanent`** | `memory.GroupStore.Add`, §3.3.1 | drop the wrap ⇒ AC-1's four `IsPermanent` assertions and AC-2c fail |
| **10** | **`g.leased` ⇒ transient** | same | wrap unconditionally ⇒ AC-2c's leased twin fails |
| **11** | **`Add` returns the live snapshot with the error** | `memory.GroupStore.Add` / each dialect | return `nil`/empty rows ⇒ **AC-1b** step 3 and **AC-4.4** fail |
| **12** | **`Handle`'s snapshot-with-error branch re-fires the release** | `routing/aggregator.go`, §3.3a | delete the branch (`return err` unconditionally) ⇒ **AC-1b** fails |
| **12a** | **`rerr != nil` — the release STRATEGY errored ⇒ the store's classification stands** | same, §3.3a.1 exit 2b | drop `rerr != nil` from the `||` ⇒ a strategy returning `(true, err)` gets its group **claimed and released** |
| **12b** | **`cerr != nil` — `ClaimGroup` failed ⇒ return `cerr`** | same, exit 3 | return `err` instead ⇒ the case's assertion on the `ClaimGroup` error fails (the overflow classification would mask a store fault) |
| **12c** | **`claim == nil` — another holder ⇒ TRANSIENT, diverging from the success path's `nil`** | same, exit 4 (cf. `aggregator.go:438-439`) | return `nil` ⇒ the member is silently lost and no other case notices |
| **12d** | **`relErr != nil` — the release failed ⇒ return the RELEASE error** | same, exit 5 | return the overflow error ⇒ the case's message assertion fails (an operator would be pointed at the cap, not the output channel) |
| **13** | **`Handle` returns a TRANSIENT error after a successful drain** | same | return the store's permanent error ⇒ AC-1b step 4 never runs; return `nil` ⇒ AC-1b's silent-loss assertion fails |
| **14** | **the `default ≥ completionSizeCeiling` AST invariant, BOTH stores** | root blackbox test, AC-3.3 | change any of the three literals ⇒ fails; rename a constant ⇒ the not-found guard fires; drop the `sql` file from the parse set ⇒ fails |
| **15** | **`ClaimGroup` / `ExpiredGroups` pass `limit = 0` to `*SelectMembers`** | `harness` conformance (AC-5), on all three dialects | pass `maxMembers+1` from `ClaimGroup` ⇒ an over-cap claimed group is **truncated** and the harness case fails (§3.6.3, audit **N-5**) |

> **Branch 2 is the one a plausible implementation gets wrong and no other case catches.** A cap check written as
> `if len(g.msgs) > s.maxGroupMembers` (after the append) admits `cap+1` members — the group is bounded, every
> overflow test still passes, and the boundary arithmetic of §3.5 is silently off by one.
>
> **Branches 9 and 12 are the two that round 1 had to find by reading the runtime**, not by reading this spec.
> Neither is visible to any acceptance criterion revision 1 wrote: branch 9's absence is a green suite with an
> infinite production hot spin, and branch 12's absence is a green suite with a permanently deadlocked group.
>
> **Branches 12a-12d and 15 are round 2's equivalents** (audit **N-7**, **N-5**). 12a-12d are the four exits of
> §3.3a's branch that revision 2's tables did not name — CLAUDE.md's coverage gate makes every one of them a
> delivery blocker, and 12c is a **deliberate divergence** from the success path that nothing tested. **15 is the
> sharpest of the five**: without it, a `LIMIT` that leaks into `ClaimGroup` silently releases incomplete
> aggregates — the data corruption §5 rejects, arrived at through a shared helper rather than a design choice.

**AC-10 — vacuity probes, per the project's standing rule** (*"a gate that has never failed proves nothing"*).
Each gate half is proven to fire: plant a `WithMaxGroupMembers`-shaped option (half 1 must report exactly one extra
key) and flip one conformance row's `arm` (half 2 must report the pairwise mismatch). **Revert both probes and
re-run.**

> 🔴 **THE PROBE IS PLANTED TWICE, and revision 1's single site rested on a false premise** (audit **M-8**).
> Revision 1 said to plant it in *"`adapter/database/sql`, **the module** this increment newly touches, not in
> root."* **`adapter/database/sql` is not a module** — it has no `go.mod` and is a package *in* the root module
> (`find . -name go.mod` lists eight, and it is not among them). A probe there **is** a probe in root, dressed as a
> probe outside it, and it answers a question that was never in doubt. The coverage question for half 1 is whether
> the filesystem walk **crosses a `go.mod`** — the boundary the gate's own header calls out. So:
>
> | Probe site | Kind | What it proves |
> |---|---|---|
> | `adapter/database/sql/postgres` | a **real leaf module** — one this increment genuinely newly touches | the walk reaches code outside the root module's `go.list`; half 1 reports the key and **cannot** be satisfied by half 2 (a root test cannot import a leaf module), which is the deliberate gate failure the header promises |
> | `adapter/database/sql` | a **same-module package** | the ordinary path: half 1 reports it and half 2 can adopt it |
>
> **Record both outcomes.** They are different results, and the first is the one Plan 029's lesson
> (*"proving the gate FIRES is not proving it COVERS"*) was written about.

## 7. Scope

**In:** the per-group member bound at both first-party stores; the two new options with their ceilings, defaults,
godoc and typed errors; **the permanent/transient classification of the overflow error (§3.3.1)**; **the
snapshot-with-error contract and `Aggregator.Handle`'s release re-evaluation (§3.3a)**; the `GroupDialect.AddMember`
signature change and its three dialect implementations, each at its own enforcement point (§3.6.1); the private
`limit int` parameter on the three `*SelectMembers` helpers, with `AddMember` the only non-zero caller (§3.6.3);
the named `defaultMaxGroupMembers` / `maxGroupMembersCeiling` constants in both store packages (§3.2); the
`harness` conformance case; the `MessageGroupStore` SPI
contract addition; the three release-path godoc cross-references; the existing bare `ErrOverflowDropped` upgraded
to the wrapped shape; the class-gate update and its new stated limitation; **the `default ≥ completionSizeCeiling`
AST invariant test (§6 AC-3.3)**.

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
2. **Enforcement is atomic across instances, because of where it sits — but the MECHANISM differs per engine, and
   revision 1 asserted one mechanism for all three** (audit **M-3**). §3.6.1 places the check inside the dialect's
   transaction, **after the statement that serializes same-key adds**:

   | Engine | Serializer | Strength |
   |---|---|---|
   | postgres | the `ON CONFLICT … DO UPDATE … RETURNING` group-row **row lock** (`postgres/groupdialect.go:107-110`, comment: *"serializes same-key adds (H1)"*) | per-key |
   | mysql | the `ON DUPLICATE KEY UPDATE` group-row **X lock** (statement `mysql/groupdialect.go:93-96`; the explanatory comment is `:85-92`) | per-key |
   | sqlite | **`BEGIN IMMEDIATE`**'s database-wide **write lock** (`sqlite/groupdialect.go:62`, not `:63` — audit **N-13**). There is no group row lock and no `RETURNING` — sqlite uses `DO NOTHING` + a separate `SELECT` | whole-database — *stronger* |

   Two instances adding concurrently to a group at `cap-1` therefore serialize on all three: one commits, the other
   counts `cap`, exceeds, and rolls back. **Enforcement (A) would NOT have this property** — its check runs after
   the transaction, so N instances could each commit past the cap. This is a second, independent reason for
   choosing (C).

2b. **🔴 The rollback is the dialect's only when the dialect owns the transaction** (§3.6.2, audit **M-2**, entry
   point corrected per audit **N-2**). Under a `*sql.Tx` Querier — reachable **only by a direct dialect caller**,
   never through `sql.GroupStore`, whose `db` field is a concrete `*stdsql.DB` — `pgRunInTx`/`mysqlRunInTx` take
   the `return fn(tx)` branch, so the over-cap row sits in the **caller's** open transaction and the caller owns
   the outcome. Cross-instance atomicity is unaffected (the lock is still held for the caller's transaction's
   duration, which is *longer*), but *durability of the bound* is the caller's responsibility on that path.
   **For the shipped `sql.GroupStore` the bound is unconditionally durable.** Stated on `GroupDialect.AddMember`'s
   godoc and covered by AC-4b.
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
are the ones where a reasonable person could land elsewhere, and where reversal is cheapest **before** Plan 031
starts. **Item 5 is new in revision 2 and is now the most consequential of them.**

1. **The `memory` / `sql` counting asymmetry** (§3.4) — `memory` counts live + claimed, `sql` counts live. The
   uniform alternative (both count live) simplifies the SPI contract to one sentence at the cost of letting
   `memory` transiently retain 2× the cap. **Round 1 attacked this and left it standing** (*"not a finding"*), so
   it is open for the **user**, not for the audit. *Recommendation: keep the asymmetry; it is honest about what
   each store retains. Reversible in one line per store.*
2. **The scope of the SQL fix** (§3.6) — enforcement (C) is the only option that bounds anything durable, but it
   costs a breaking `GroupDialect.AddMember` signature change across **seven** sites in **six** modules, roughly
   doubling the increment. Enforcement (A) is one line and no SPI change, and bounds roughly half the peak.
   *Recommendation: (C). But if the increment must be small, (A) plus a named follow-up is defensible — say so
   before the plan starts, not at task 6.*
3. **The default cap's value, 65,536** (§3.9) — the argument that a *ceiling*'s reference value may become a
   *default* is the load-bearing step, and it is an argument rather than a measurement. A more conservative default
   (say `1<<14`) would protect more callers and break more of them. *Recommendation: 65,536, because it is the
   number Plan 029 already ratified for this exact unit.*
4. ~~**The unenforceable `default ≥ completionSizeCeiling` invariant**~~ — **CLOSED in revision 2** (audit **M-5**;
   ADR 0033 **D-AQ**). It *is* enforceable, by the `go/parser` technique the class gate already ships, and §6 AC-3.3
   now specifies the test. No public surface is exported; nothing is accepted as drift. **Do not re-open this as an
   accepted limitation.**
5. **🔴 NEW in revision 2, WIDENED in revision 3 — the permanent classification of a not-leased over-cap
   rejection** (§3.3.1, ADR 0033 **D-AM**). A `Permanent` rejection **terminates** the message at the
   invalid-message or dead-letter sink — **and with neither sink configured it is WARNed and ACKed, so the source
   drops it** (§3.3.1, audit **N-11**). A caller who has configured `WithGroupTimeout` would, under the old
   transient classification, have had that message admitted once the reaper expired the group. So this trades
   *"a message that might have succeeded later"* for *"no unlogged infinite hot spin under the shipped defaults."*

   > 🔴 **The exposure is wider than revision 2 stated** (audit **N-3**). For `sql` it reaches the **default**
   > configuration: a group at cap that also holds a **stranded lease** is surfaced by the 5m default sweep
   > (`RecoverInterval() == leaseTTL`), claimed, and drained — so D-AM dead-letters members that the *default*
   > `sql` configuration would have admitted one tick later, not only members a `WithGroupTimeout` caller would
   > have. §1.2.1 and §3.3.1's counter-example box carry the derivation. **This does not change the
   > recommendation** — a 5m wait is not a reason to prefer an unlogged infinite spin — but the user is entitled to
   > see the true width of the trade before ratifying it.
   *Recommendation: permanent. A hot spin is production-down and silent; a dead-letter is recoverable and named,
   and [CLAUDE.md](../../CLAUDE.md)'s Sensible-defaults gate says to fail safe. But this is a genuine trade and the
   user should see it.* **Alternatives, if the recommendation is rejected:** (a) keep transient but make the
   default `RetryPolicy` carry a `MaxAttempts`/`Backoff` — a much larger, cross-cutting change to shipped
   behavior; (b) classify permanent only when `WithGroupTimeout` is **unset** — precise, but the store does not
   know the Aggregator's configuration, so it needs a new SPI signal; (c) drop the default cap entirely and ship
   the option opt-in (§3.9's rejected alternative), which at least does not convert an unbounded group into a
   spinning one. *Reversal cost: one branch per store.*
6. **🔴 NEW in revision 2 — `Aggregator.Handle` gains a second release-firing site** (§3.3a, ADR 0033 **D-AN**).
   Releasing from the error branch is more surface to keep correct than releasing only from the success branch. It
   is mitigated by routing both through the same `a.release` helper and by AC-1b. *Recommendation: accept — the
   alternative is the M-6 deadlock, and no cheaper fix was found.* **Round 2 did not disagree and supplied no
   cheaper fix — but it measured the surface at SIX exits where the artifacts covered four** (§3.3a.1, audit
   **N-7**), one of which (`claim == nil`) is a deliberate, previously undocumented divergence from
   `aggregator.go:438-439`. Revision 3 covers all six.
7. **🔴 NEW in revision 3 — named `defaultMaxGroupMembers` constants deviate from a shipped precedent**
   (§3.2, ADR 0033 **D-AR**). `adapter/memory` declares its default as a bare `maxGroups: 1024` literal; this
   increment declares both new defaults as `const`s so §6 AC-3.3 has something to parse. *Recommendation: accept —
   a mechanically-enforced invariant is worth one constant per package, and the alternative is an AST test that
   fires its own not-found guard on a faithful implementation. Reversal cost: two declarations, and AC-3.3 is lost.*
8. **🔴 NEW in revision 3 — a private `limit int` parameter on the three `*SelectMembers` helpers**
   (§3.6.3, ADR 0033 **D-AS**). *Recommendation: accept — the alternative readings of revision 2's instruction are
   "unimplementable" or "silently truncate `ClaimGroup` and `ExpiredGroups`". Reversal cost: one parameter per
   dialect; enforcement (C)'s "bounds the raw fetch" claim degrades to approximate.*

**Also recorded, out of scope, for `docs/HANDOVER.md` §6:**

- **`memory.GroupStore.Add`'s per-call `slices.Clone` is quadratic** (§1.2). The cap bounds the damage; it does not
  remove it. 8.6 s and 48.3 GiB to reach a legal cap value is a performance defect with its own remedy (return a
  view, or clone lazily) and its own spec.
- **`sql.GroupStore` re-fetches and re-decodes the entire live group on every `Add`** (§1.3). Same shape, durable
  side, and arguably worse: the cap plus §3.6.3's `LIMIT maxMembers+1` bounds it, but a count-only dialect contract
  would remove it.
- **A member cap without `WithGroupTimeout` yields a bounded-but-stuck group** (§3.11). Consider whether that
  combination deserves a construction-time diagnostic — and note that §3.3.1's permanent classification makes the
  *cost* of a stuck group visible (a dead-letter stream) where revision 1's transient classification made it
  invisible (a spin), so the diagnostic is less urgent than it was.
- **`classifyQueryErr` runs a `SchemaExists` probe on every dialect error** (§1.3 item 4, §3.6.3). Bounded now that
  the rejection is terminal; still an avoidable round-trip on a hot error path across the whole `sql` adapter, not
  just the group store.
- **The default `msgin.RetryPolicy` neither logs nor bounds a transient fault** (§3.3.1). This increment routes
  *around* it; it does not fix it. Whether the zero-value policy should carry a default `MaxAttempts` — or at least
  a WARN on repeated Nacks — is a core-runtime question with a much larger blast radius, and it deserves its own
  spec. **This is the root cause behind audit B-1 and it survives this increment.**
