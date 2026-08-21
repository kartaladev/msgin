# Spec 016 — Sizing options must not panic or fatally exhaust memory

- **Status:** **DELIVERED (2026-08-22)** — revision 6 was written before any code, per CLAUDE.md's design-time
  gate; realized by [`plans/029-sizing-option-bounds.md`](../plans/029-sizing-option-bounds.md) Tasks 1–8 on
  `fix/sizing-option-bounds`, whole-branch gate green. Two corrections landed at the delivery gate: §2.1's
  `WithSuccessStatus` row (classification arms ≠ AC-5 behavioral arms — see §2.1) and §3.1/§3.3's as-delivered
  `checkRange` notes. Revision-6 text below is otherwise unchanged.
  - **Revision 6** folds round-5 findings ([`029-audit-round-5.md`](../plans/029-audit-round-5.md)): 2 BLOCKERs,
    5 MAJORs, 8 MINORs. Round 5's verdict on the *previous* fold-in was the cleanest of five —
    **11 LANDED · 5 LANDED-BUT-FLAWED · 0 NOT LANDED · 0 REGRESSED**, and the first round in which the ADR
    received **every** fold-in aimed at it.
  - **BLOCKER-1: `msghttp.WithMaxResponseBytes` is a class member and was certified *safe (d)*.** `drainBounded`
    is **five of six** reads of that field; the sixth (`exchange.go:130-131`) is
    `io.ReadAll(io.LimitReader(resp.Body, max))`, whose body is **retained** as the reply payload — 67,108,864
    bytes retained at `1<<62`, measured. It is a **byte** cap, so the user's standing **"split by kind"** rule
    sends it to the **deferred** arm (§3.8), not to a ceiling. Census: **9 fixed + 3 deferred + 4 safe**.
    **Safety cause (d) is now empty**, as cause (c) was in revision 5.
  - **BLOCKER-2: revision 5's own `WithReplayBuffer` reclassification stopped ONE FILE SHORT** — seven twins
    survived, including **Plan Task 7, the task that writes the gate**, which still filed the knob in the
    *accepts, safe* arm. **The cross-file grep guard revision 5 introduced finds all seven in one command; it was
    written and not run.** Revision 6 runs it and pastes the output (§ below).
  - **🔴 TWO SAFETY CAUSES HAVE NOW BEEN EMPTIED IN CONSECUTIVE REVISIONS**, each by the very criterion they were
    written alongside, and **neither emptying changed the 16/17 totals** — so no count-check could have caught
    either. **§2.1's standing rule is therefore load-bearing: re-derive every row FROM the criterion; never read
    the verdict column.**
  - Also folded: the withdrawn un-GC'd figures that survived in three further places (M5-1); *"AC-4's protocol"*
    cited for `HeapAlloc` when AC-4 mandates `TotalAlloc` — a **3.3×** error headed for a godoc (M5-2);
    `WithReplayBuffer(8)` → *"ring holds 8"* is **unobservable blackbox** and needs a `Last-Event-ID` request
    (M5-3, the *fifth* consecutive unexecutable AC); Task 8's coverage gate red on arrival (M5-5); the section
    order; and the eight MINORs.
  - **Revisions 2–5** folded rounds 1–4: the census moved 4 → 7 → 8 → 9 (+3 deferred), AC-4 was made executable,
    `WithConcurrency`'s verdict was corrected twice, the key set was corrected 16 → 17 with a stated `Recv == nil`
    boundary, the error shape was unified, the ceiling row was split into families, and **D-AB replaced the
    hand-maintained census with a criterion**. §1's reproductions and §1.1's two-regime table have been
    re-attacked in **all five rounds** and **held verbatim**. **Verification pass pending.**
- **Realized by:** [Plan 029](../plans/029-sizing-option-bounds.md).
- **Decided by:** [ADR 0032](../adrs/0032-sizing-option-bounds.md).
- **Builds on:** [Spec 015](015-nil-option-elements.md) — this spec reuses its **R1/R2 reporting families** and its
  **latch** verbatim rather than inventing a second mechanism; and
  [ADR 0031 D-U](../adrs/0031-nil-option-elements.md) (the R2 loop `continue`s), whose interaction with a sizing
  fault is §3.3 and was the reason the backlog item was left open.
- **Origin:** the triaged backlog item in [`docs/HANDOVER.md`](../HANDOVER.md) §8.1, **widened twice** — that entry
  names one site; revision 1 found four; the round-1 audit found seven.

## 1. The gap

**A sizing option validates only its lower bound.** Every `WithX(n int)` in the workspace that reaches an
allocation or an unbounded structure rejects `n < 1` (or `n <= 0`) with a typed error and accepts everything above
it, up to and including values that kill the process.

Reproduced on this tree (Go 1.25.13, darwin/arm64), and **independently re-reproduced by the round-1 audit**:

| Knob | Constructor validates | Fails at | Observed |
|---|---|---|---|
| `endpoint.WithMaxInFlight(n)` | `< 1` only | **`Consumer.Run`** | `Run PANICKED: makechan: size out of range` |
| `msghttp.WithConnectionBuffer(n)` | `<= 0` only | **`SSEServer.ServeHTTP`**, per connection | `ServeHTTP PANICKED: makechan: size out of range` |
| `memory.WithBuffer(n)` | **nothing** (clamps `n < 0` → `0`) | `memory.New` | `PANICKED: makechan: size out of range` |
| `endpoint.WithConcurrency(n)` | `< 1` only | **`Consumer.Run`** | `Run PANICKED: sync: negative WaitGroup counter` (`n = 1<<31`) — **and** silent corruption on the other half of the band; see §1.2 |
| `memory.WithCapacity(n)` | `<= 0` only | `QueueStore.Enqueue` | **unbounded growth** — see §1.3 |
| `memory.WithMaxGroups(n)` | `<= 0` only | `GroupStore.Add` | **unbounded growth** — see §1.3 |
| `msghttp.WithMaxConnections(n)` | `<= 0` only | `SSEServer.ServeHTTP` | **unbounded growth** — see §1.3 |

Reproduce the first with `n = 1 << 62`:

```go
c, err := endpoint.NewConsumer[any](src, h, endpoint.WithMaxInFlight[any](1<<62))
// err == nil                       ← the constructor ACCEPTS it
_ = c.Run(ctx)                      // panic: makechan: size out of range
```

**There are FOUR panics, and three of them are not at construction** (round-2 **BLOCKER-2** — revision 2 said
three panics and filed `WithConcurrency` as corruption-only). `WithMaxInFlight` passes `NewConsumer` and detonates
inside `Consumer.Run` — the library's primary execution path. `WithConcurrency` passes `NewConsumer` and detonates
inside `Consumer.Run` too, on `wg.Add` (§1.2). `WithConnectionBuffer` passes `NewConfig` **and** `NewSSEServer` and
detonates inside `ServeHTTP`, once per connection, in a live HTTP handler. Only `memory.WithBuffer` panics at
construction. CLAUDE.md forbids this without qualification: *"Library code must not … `panic` on caller input
(return errors instead)."*

The value arrives by the ordinary route — a config field, an env var, a units mistake (`WithConnectionBuffer(bytes)`
where frames were meant), or an untrusted operator input.

### 1.1 The two regimes — why the runtime's own check cannot be the guard

This is the finding that decides the mechanism, and it is counter-intuitive enough to be quoted rather than
summarized. `runtime.makechan` panics only when `elemSize × n` exceeds `maxAlloc`. **Below that threshold it
attempts the allocation for real**, and a large-but-representable size dies with an *unrecoverable* fatal error.

Measured with a 64-byte element type on darwin/arm64 (`maxAlloc = 1<<48`):

| `n` | `elemSize × n` | Result |
|---|---|---|
| `2^43` | `2^49` | `panic: makechan: size out of range` — **recoverable** |
| `2^42` | `2^48` | `panic: makechan: size out of range` — **recoverable** |
| `2^41` | `2^47` | `runtime: out of memory: cannot allocate 140737492549632-byte block` → **`fatal error: out of memory`** |

The third row is `runtime.throw`, not `panic`. **No deferred `recover` can intercept it.** The process dies.

**`GOMEMLIMIT` does not help, measured rather than assumed** (round-1 **m-6**): `GOMEMLIMIT=67108864` in front of
the `2^41` case still produces `fatal error: out of memory`. It is a GC-pacing soft limit; a single oversized
allocation throws regardless.

Three consequences, each load-bearing for §3:

1. **A guard that mirrors `makechan`'s own check is worse than no guard.** It rejects `2^43` and admits `2^41` —
   certifying as safe precisely the value with the *more* severe failure mode.
2. **Recovering the fault after the fact is impossible.** The mechanism must prevent the allocation, not convert it.
3. **`maxAlloc` is platform-specific** (it derives from `heapAddrBits`) and is a runtime internal with no exported
   accessor. A guard built on it would be unportable *and* a copy of an unstable constant.

Therefore the bound this spec imposes is a **stated part of each option's contract**, not a number derived from the
runtime. CLAUDE.md anticipates exactly this case: *"If no value can be safe for an unknown caller … make it
explicit/opt-in with a clear typed error … rather than guessing a default that lulls the caller into a false
guarantee — and say so in the godoc."*

### 1.2 `WithConcurrency` has TWO failure modes, and the predicate is `int32(n) < 0`

> **This section has now been wrong twice and is corrected here** (round-1 **M-3**, round-2 **BLOCKER-2**).
> Revision 1 claimed *"no panic — spawns `n` goroutines"*. Revision 2 replaced that with *"above `2^31` the defect
> is a silently corrupted `WaitGroup` … plus a **latent** panic on the first `Done()`"* — also wrong on both
> counts: the panic is in `Add` **itself**, not on a later `Done()`, and the boundary is not `2^31`.

`consumer.go:457-459` is `wg.Add(c.workers)` followed by the spawn loop (`workers: cfg.concurrency`,
`consumer.go:330`), and `drainWorkers` joins on that `wg`. `wg.Add` is reached **before a single worker is
spawned**, on the primary execution path.

`sync.WaitGroup` keeps its counter in the **high 32 bits** of the state word, so `Add` panics for every `n` whose
low 32 bits land in `[2^31, 2^32)`. Re-measured this revision, with the `int32(n)` column that makes the predicate
visible rather than inferred:

| `n` | `int32(n)` | `wg.Add(n)` |
|---|---|---|
| `2^30` | `1073741824` | ok |
| `2^31 - 1` | `2147483647` | ok |
| **`2^31`** | **`-2147483648`** | **PANICS: `sync: negative WaitGroup counter`** |
| `2^31 + 1` | `-2147483647` | PANICS |
| `2^32 - 1` | `-1` | PANICS |
| **`2^32`** | **`0`** | **ok** ← above `2^31`, does *not* panic — kills the "above `2^31`" formulation |
| `2^32 + 5` | `5` | ok |
| `3 × 2^30` | `-1073741824` | PANICS |
| `2^40` | `0` | ok |
| `2^62` | `0` | ok |

**The predicate is `int32(n) < 0`** — half of all `n > 2^31`. So the knob has two distinct failure modes, and the
bundle previously described only the second:

1. **`int32(n) < 0` → `Consumer.Run` panics** on `wg.Add`, end-to-end through the shipped public API:
   `WithConcurrency(2147483648): NewConsumer err=<nil>` then `Run PANICKED: sync: negative WaitGroup counter`.
   This is the **fourth** panic in §1's table, and it is in `Run`.
2. **`int32(n) >= 0` (e.g. `2^40`) → `Add` returns cleanly, the counter is wrong, and the spawn loop
   `for i := 0; i < c.workers; i++` runs `2^40` times.** `Wait()` then returns immediately while workers still
   run — shutdown believes it drained when it did not. Whether this OOMs, thrashes, or eventually returns is
   **not known**: the round-2 probe was killed at a 2-minute timeout while the loop was still running. It is
   recorded as *timed out*, not as *observed*.

The ceiling (`1 << 16`, §3.4) forecloses both modes, so the *decision* was never in doubt — what was wrong is the
evidence headed for Task 0 and the public godoc.

### 1.3 Three knobs bound nothing — the contract's second clause

**Revision 1 stated a two-clause contract — no panic *"or fatally exhaust memory"* — and then established only the
first clause** (round-1 **BLOCKER-2**). Three knobs cannot panic and are still OOM levers, because each is the
*bound* on a structure that grows by insertion:

| Knob | Structure | Why a huge `n` is unbounded growth |
|---|---|---|
| `memory.WithCapacity` | `queuestore.go:132` `s.ready = append(s.ready, …)` | The append is gated only by a semaphore of size `capacity`. At `1<<62` the semaphore never fills, so `ready` grows until OOM — **reinstating the exact overflow lever `WithCapacity`'s own godoc says it prevents.** |
| `memory.WithMaxGroups` | `groupstore.go:108` `len(s.groups) >= s.maxGroups` | The cap is the only thing evicting; at `1<<62` the map grows without bound. |
| `msghttp.WithMaxConnections` | `sse_server.go:182` `len(s.conns) >= s.cfg.maxConns()` | Same shape: the cap is the admission control. At `1<<62` there is none. |

**`WithCapacity` is not a zero-size-element case.** Revision 1 filed it under that heading because its *semaphore*
is `chan struct{}`. The semaphore is indeed panic-proof at any `n`; the `ready` slice it gates is not. The
zero-size-element property is real but protects the wrong object.

### 1.4 `WithCompletionSize` bounds group MEMBERS — and nothing else does

**Certified *"safe — comparison only"* through revision 3; it is the §1.3 shape exactly** (round-3 **BLOCKER-1**).
`aggregator.go:132-135` sets the release predicate to `len(g.Messages()) >= n`, and `Handle` adds every message to
the group **first**, releasing only when that predicate holds. At `n = 1<<62` it never holds, so one correlation
key accumulates without bound:

```
NewAggregator(WithCompletionSize(1<<62)) err=<nil>          ← ACCEPTED
after 60000 Handle() into ONE group: released=0 groups=1 live heap delta=30078064 B (28.7 MiB)
WithCompletionSize(2) over 6 msgs: released=3      # needs the FULL 5-part fixture — see AC-5 / Task 4
```

**Nothing else bounds it.** `WithMaxGroups` caps the *number of groups*, not members per group. The reaper does not
help either: `reapInterval()` (`aggregator.go:526-532`) returns `cfg.timeout`, and `memory.GroupStore.RecoverInterval()`
returns `0` (`groupstore.go:205`), so with no `WithGroupTimeout` the interval is `0` and `Run` blocks on `ctx.Done()`
without ever sweeping (`aggregator.go:505-509`).

**The cost is quadratic in TIME, not linear in bytes** — `memory.GroupStore.Add` clones the group snapshot per
call. Measured this revision, one key, realistic `msgin.New` messages:

| members | elapsed | cumulative alloc | live (GC'd) |
|---|---|---|---|
| `1<<12` | 50 ms | 206.7 MiB | 2.0 MiB |
| `1<<14` | 644 ms | 3,143.5 MiB | 7.8 MiB |
| 60,000 | — | 41,474.0 MiB | **28.7 MiB** |
| `1<<16` | **8.6 s** | **49,451.7 MiB (48.3 GiB)** | **31.0 MiB** |

**Fixture AND protocol** (round-4 **M4-4**): one correlation key, realistic `msgin.New` messages,
`WithCompletionSize(1<<62)`; `runtime.GC()` **before both reads**, `HeapAlloc` delta for *live*, `TotalAlloc` for
*cumulative*, `runtime.KeepAlive` on the aggregator and store.

> **"AC-4's protocol" is `TotalAlloc`, NOT `HeapAlloc` — do not conflate the two** (round-5 **M5-2**). AC-4
> mandates `TotalAlloc` precisely because it is cumulative and GC-independent; the *live* column here needs
> `HeapAlloc` **plus** an explicit GC, which is a *different* protocol for a different question. Revision 5 cited
> "AC-4's protocol" while specifying `HeapAlloc`. Measured on the same fixture the two differ by **3.3×**
> (78.4 vs 23.5 MiB) — an error that was headed for a public godoc. **Say which metric you mean, every time.** **Revision 4's live column was read WITHOUT a GC**
and printed 3.2 / 13.8 / 67.8 MiB — inflated by uncollected garbage, which under a quadratic-churn workload grows
with `n`, and self-contradicting: it claimed 67.8 MiB at 65,536 members four lines below its own prose figure of
28.7 MiB at 60,000. **Revision 4's rule — "name the fixture" — is hereby extended to "name the fixture AND the
measurement protocol."** AC-4 already imposes exactly these three conditions on the *tests*; the design's own
probes are now held to them too.

That quadratic churn is *itself* the argument for a ceiling far above any plausible workload — and the reason
§6 AC-1 never runs this knob's hazardous path at the ceiling (it is a growth knob; the property is proven at
small `n`).

### 1.5 `WithReplayBuffer` bounds retained events — and the ring's bound IS `n`

**Certified *"safe"* in every revision through 4, most recently as safety cause (c)** — *"the structure evicts at
the cap, for any `n`"*. That reasoning is **circular** (round-4 **BLOCKER-1**): `appendRing` does evict, but only
at `max := s.cfg.replaySize()`, and `replaySize()` returns **`n`**:

```go
// sse_server.go:466-476
func (s *SSEServer) appendRing(e ringEntry) {
	if max := s.cfg.replaySize(); len(s.ring) >= max {   // ← max IS n
		n := copy(s.ring, s.ring[len(s.ring)-max+1:])
		s.ring = s.ring[:n]
	}
	s.ring = append(s.ring, e)
}
```

At `n = 1<<62` the eviction branch never runs and the ring grows for the life of the server. Measured by round 4,
first-hand: at `WithReplayBuffer(1<<62)`, **20,000 `Send`s retain 23.5 MiB and 80,000 retain 93.8 MiB** — exactly
linear — while `WithReplayBuffer(8)` retains **17 KiB regardless**. *(Per-event cost scales with the caller's
frame size, so that ratio is fixture-specific; Task 2 re-measures it under AC-4's stated protocol — **`TotalAlloc`**, GC'd, `KeepAlive`'d — for the godoc.)*

**Worse than the §1.3 three: it accumulates with NO client connected.** `Send` appends to the ring *before* the
fan-out loop, so a server with zero subscribers still grows:

```go
// sse_server.go:429-431, ahead of `for conn := range s.conns`
if s.cfg.replaySize() > 0 && ev.ID != "" {
	s.appendRing(ringEntry{id: ev.ID, frame: frame})
}
```

**Why this survived four rounds, and the lesson that generalises.** Round 3 carried it forward as *suspected, not
proven*. Revision 4 then introduced §2.1's criterion **specifically to stop the census moving** — and kept this
row's pre-existing *"safe"* verdict, **inventing a safety cause to justify it** rather than testing the row
against the new rule. **A criterion only helps if every row is re-derived from it, including the rows you are not
changing.** Cause (c) had exactly one member; with that member reclassified, **cause (c) is deleted** (§2.1).

## 2. The inventory (measured, not transcribed)

**16 exported options take a numeric size; the conformance key set is 17.** The two numbers are different things
and revision 2 conflated them, printing 16 in four places for a set that is 17 (round-2 **BLOCKER-1**). Keep them
apart:

- **16** — exported `With…` options with an `int`/`int64` parameter. This is what the `grep` census below counts.
- **17** — the **conformance key set** of §6 AC-5: those 16 **+ `NewTokenBucket`'s positional `burst`**.

> **Regenerate before citing.** The `^func With` anchor is load-bearing. Both int widths must be matched —
> `WithMaxBodyBytes` and two siblings take `int64`.

```bash
git ls-files '*.go' | grep -v _test | xargs grep -hnE \
  '^func With[A-Za-z]+(\[[^]]*\])?\([a-z]+ (int|int64)\)' | wc -l          # → 16 options
```

**This command has a known blind spot, and the class gate closes it, not this command** (round-1 **M-5**): an
exported **positional** sizing parameter is not a `With…` option and is invisible here. One exists —
`resilience.NewTokenBucket(rps float64, burst int, …)` (`ratelimit.go:42`). `burst` is stored as `float64` and
reaches no allocation *today*, which is precisely the fact a class gate must stop depending on. §6 AC-5 half 1
therefore scans **any position**, and `burst` carries the 17th conformance row.

### 2.0 Functions or methods? — the gate's boundary, stated

AC-5 half 1 walks the AST. In `go/ast` a method is an `*ast.FuncDecl` with a **non-nil `Recv`**, so "every exported
function with an `int`/`int64` parameter in any position" reads two ways, and revision 2 said neither. Measured on
this tree, the two readings differ by 27 keys:

| Reading | Keys | Consequence |
|---|---|---|
| **`Recv == nil` only — CHOSEN** | **17** | All 17 constructible from a root blackbox test. Maintainable. |
| any `FuncDecl` | 44 | 27 method rows, **22 of them on unexported receivers** (`mysqlDialect.Claim`, `postgresGroupDialect.AddMember`, … — 21 in leaf modules, plus `responseTracker.WriteHeader` in root) that a root-module blackbox test **cannot construct** ⇒ half 2 unsatisfiable ⇒ a verbatim repeat of round-1 BLOCKER-1. |

**The boundary is `Recv == nil`, and it is a stated limitation, not an oversight** — see ADR 0032 D-AA. The
exclusion is not free: **four** of the 27 methods are genuine members of the class §3 names (*"reachability of an
allocation"*), and one of them **already had this exact bug and was already fixed by a prior audit**:

```
$ sed -n '163,166p' adapter/memory/queuestore.go
// Claim leases up to max visible ready entries in FIFO order. A non-positive max
// yields no deliveries (guards make(...) against a negative cap panic — Claim is
// exported and directly callable; audit M-1).
func (s *QueueStore) Claim(_ context.Context, max int) ([]msgin.Delivery, error) {
```

**The membership criterion for a METHOD is the same one §2.1 states, applied strictly: *the parameter itself
sizes a `make`.*** Revision 3 hand-derived a list of "four" without stating a criterion, and round-3 **M3-4**
showed the list was not reproducible in either direction — under the loose reading that admitted
`sql.Source.Poll` (*"`max` becomes the query `LIMIT`"*), **three more methods qualify identically and were
omitted**: `{postgres,mysql,sqlite}GroupDialect.ExpiredGroups`, which pass `limit` as the SQL `LIMIT` and then
`make([]msginsql.GroupRows, 0, len(cands))` (`postgres/groupdialect.go:266`+`:305` and its two siblings).

**Under the strict criterion the set is TWO**, and both are already covered:

| Class-member method | Site | Why `n` sizes the `make` | Coverable from root? |
|---|---|---|---|
| `memory.QueueStore.Claim` | `adapter/memory/queuestore.go:166` | `make([]msgin.Delivery, 0, min(max, len(s.ready)))` — `max` is **an argument to the capacity**; carries the `audit M-1` comment above | **yes** — `memory.NewQueueStore()` takes no required args |
| `channel.QueueChannel.Poll` | `channel/queuechannel.go:50` | forwards verbatim to `store.Claim` | **yes** — `channel.NewQueueChannel(store)` over a `memory.QueueStore` |

**`sql.Source.Poll`, `sql.QueueStore.Claim` and the three `ExpiredGroups` do NOT qualify**: their `make` capacity
is `len(rows)` / `len(cands)` — sized by **what the database returned**, not by the parameter. `max` reaches only
the SQL `LIMIT`, so the row count is bounded by the table, and a huge `max` degenerates to "no `LIMIT`" — a query
concern, not an allocation the parameter sizes.

**Both members get manual conformance rows** in half 2 (§6 AC-5); because `QueueChannel.Poll` delegates into
`QueueStore.Claim`, one exercised chain covers both. **So the `Recv == nil` boundary now costs nothing at all** —
there is no "named but uncovered" residue, and D-AA's limitation gets *simpler*, not more complex.

> **Revision-3 correction retired.** Revision 3 recorded a four-member list with two members "named but
> uncovered" because root declares no SQL driver. That justification was **true** (verified: no
> `mattn`/`modernc`/`lib/pq`/`go-sql-driver`/`sqlmock` in root's `go.mod`) but is now **moot** — under a stated
> criterion those rows are not class members at all. Round 2 had said "three"; revision 3 said "four"; the
> criterion says **two**. *A count nobody can reproduce from a rule is a count that will keep moving.*

### 2.1 Classification — **by a stated criterion**, not by a verdict string

> **Revisions 1–3 all asserted a partition "with no residual" that was not complete** (round-1 **BLOCKER-2**,
> round-3 **BLOCKER-1**). The root cause is that the verdict strings were **not discriminators**: the *same*
> string was true for one row and false for its sibling. `WithCompletionSize`'s `len(g.Messages()) >= n` was
> certified *"comparison only"* while `WithMaxGroups`'s `len(s.groups) >= n` — identical in shape — was certified
> **DEFECTIVE** for that very reason. `WithMaxBodyBytes` was certified *"limit, never allocated"* while being the
> sole bound on an `io.ReadAll`. Revision 4 therefore states a **criterion first** and derives the table from it.

#### The criterion

**A knob is a CLASS MEMBER iff `n` is the sole bound on an accumulation** — that is, memory retained (or
goroutines spawned) grows with input and nothing but `n` stops it.

A knob is **SAFE** iff one of these holds, and the table records **which**:

| # | Safety cause | Why it is not a class member |
|---|---|---|
| **(a)** | pure comparison over a **scalar counter** | nothing accumulates — the counter is one `int` |
| **(b)** | the check is applied to an **already-materialised** value | the memory was allocated by someone else's bound; this knob only decides accept/reject |
| **(c)** | *(deleted in revision 5 — see below)* | — |
| **(d)** | *(no members as of revision 6 — see below)* | — |

> **Cause (c) is DELETED, not merely re-worded.** It read *"the structure evicts at the cap, for any `n`"* /
> *"retention is bounded by the ring, not by trusting `n`"* — **circular**, because the ring's cap *is* `n`
> (§1.5). It had exactly one member, `WithReplayBuffer`, which revision 5 reclassifies as a class member. The
> letter is retained as a tombstone so that (a), (b) and (d) keep their identifiers across revisions.
>
> **Cause (d) is now EMPTY too** (round-5 **BLOCKER-1**). It read *"it bounds CPU/streaming, not retention"* and
> had one member, `WithMaxResponseBytes` — certified safe on the strength of `drainBounded`
> (`io.CopyN(io.Discard, …)`). But that is **five of six** reads of the field; the sixth,
> `exchange.go:130-131`, is `io.ReadAll(io.LimitReader(resp.Body, max))`, whose body is **retained** as the reply
> payload (§3.8). **Two of the four safety causes have now been emptied in consecutive revisions**, each by the
> criterion they were written alongside. That is the strongest available argument for the standing rule:
> **re-derive every row FROM the criterion; never read the verdict column.** Neither emptying changed the 16/17
> totals, so no count-check could have caught either.

A knob that is safe *by accident* is one element-type change away from being defective, so the cause is recorded
per row and AC-4 pins the two that rest on a structural property.

#### The table

| Knob | Package | Reaches | Verdict |
|---|---|---|---|
| `WithMaxInFlight` | `endpoint` | `make(chan managedDelivery, n)` `consumer.go:384` | **DEFECTIVE — fixed here** · panic, struct element |
| `WithConcurrency` | `endpoint` | `wg.Add(n)` + spawn loop `consumer.go:457-459` | **DEFECTIVE — fixed here** · panic + `WaitGroup` corruption (§1.2) |
| `WithConnectionBuffer` | `msghttp` | `make(chan []byte, n)` `sse_server.go:201` | **DEFECTIVE — fixed here** · panic, slice-header element |
| `WithBuffer` | `memory` | `make(chan msgin.Message[any], n)` `memory.go:43` | **DEFECTIVE — fixed here** · panic, struct element |
| `WithCapacity` | `memory` | `append` gated by the semaphore `queuestore.go:132` | **DEFECTIVE — fixed here** · sole bound on `s.ready` (§1.3) |
| `WithMaxGroups` | `memory` | `len(s.groups) >= n` `groupstore.go:108` | **DEFECTIVE — fixed here** · sole bound on `s.groups` (§1.3) |
| `WithMaxConnections` | `msghttp` | `len(s.conns) >= n` `sse_server.go:182` | **DEFECTIVE — fixed here** · sole bound on `s.conns` (§1.3) |
| **`WithCompletionSize`** | `routing` | `len(g.Messages()) >= n` `aggregator.go:134` | **DEFECTIVE — fixed here** · **sole bound on group MEMBERS** (§1.4) — *was certified "safe — comparison only" through revision 3* |
| **`WithMaxBodyBytes`** | `msghttp` | `io.ReadAll(http.MaxBytesReader(…, cfg.maxBody()))` `encode.go:102` | **DEFECTIVE — ceiling DEFERRED** (§3.8) · sole bound on a **remote-driven** full read |
| **`WithMaxEventBytes`** | `msghttp` | `p.dataBuf.WriteString(value)` then `> p.maxEventBytes` `sse.go:384-389` | **DEFECTIVE — ceiling DEFERRED** (§3.8) · sole bound on a **remote-driven** `bytes.Buffer` |
| **`WithReplayBuffer`** | `msghttp` | `appendRing` `sse_server.go:466-476`, evicting **at `n`** | **DEFECTIVE — fixed here** · **sole bound on retained events** (§1.5) — *was certified safe in revisions 1–4, most recently as cause (c)* |
| `WithPollMaxBatch` | `endpoint` | `held < c.pollMaxBatch` `poller.go:36` | safe **(a)** — loop bound ≤ free credits; **derivatively** safe, see note |
| `WithBreakerThreshold` | `resilience` | `b.fails >= b.threshold` `breaker.go:164` | safe **(a)** — scalar counter, accumulates nothing |
| **`WithMaxResponseBytes`** | `msghttp` | **`io.ReadAll(io.LimitReader(resp.Body, max))`** `exchange.go:130-131` | **DEFECTIVE — ceiling DEFERRED** (§3.8) · sole bound on a **remote-driven** read whose body is **RETAINED** as the reply payload — *`drainBounded` is only 5 of its 6 reads* |
| `WithMaxPayloadBytes` | `endpoint` | `len(b) > c.maxPayloadBytes` `consumer.go:1199` | safe **(b)** — `b` is already materialised |
| `WithSuccessStatus` | `msghttp` | HTTP status code | safe **(a)** — **already range-checked**, `[100,599]`. **Its AC-5 row is the `rejects` arm, not the `safe` arm** — it is the one safe knob that does *not* accept `1<<62`; see the note below |
| **+ `NewTokenBucket`'s `burst`** | `resilience` | `float64(burst)` `ratelimit.go:48-49` | safe **(a)** — **positional, not an option**; carries a conformance row (§2 note) |

**9 fixed here + 3 deferred + 4 safe = 16 options; + 1 positional (`burst`) = 17 conformance keys.** The table has
17 rows for that reason. §6 AC-5 keys on **17**. **Re-derive this line before citing it** — it has been wrong in
**every** prior revision (7/9 → 8/2/6 → 9/2/5 → **9/3/4**), which is precisely why §2.1 now leads with a criterion instead of
a list.

> **CLASSIFICATION arms ≠ AC-5 BEHAVIORAL arms — they are two different partitions of the same rows, and
> conflating them was a live self-contradiction in revisions 1–6.** The census above partitions by *why* a knob
> is or is not a class member. §6 AC-5 partitions by *what a row asserts at `1<<62`*. The two agree on 16 of the
> 17 keys and disagree on exactly one: **`WithSuccessStatus` is `safe (a)` by the criterion** — a pure comparison
> over a scalar, nothing accumulates — **but it does not `accept 1<<62`**, which is what AC-5's `safe` arm
> asserts. Its behavioral row therefore sits in a `rejects` arm of its own. **Derive AC-5's arms from this
> line, not from the verdict column:**
>
> | AC-5 arm | Rows | Which keys |
> |---|---|---|
> | `fixed` | **9** | the 9 class members this increment bounds |
> | `rejects` | **1** | `WithSuccessStatus` — safe by the criterion, but pre-existing `[100,599]` makes it reject |
> | `deferred` | **3** | `WithMaxBodyBytes`, `WithMaxEventBytes`, `WithMaxResponseBytes` |
> | `safe` | **6** | `WithPollMaxBatch`, `WithBreakerThreshold`, `WithMaxPayloadBytes`, `burst`, + the **2 manual** rows |
>
> **9 + 1 + 3 + 6 = 19 rows = 17 AST keys + 2 manual rows.** Both totals are re-derivable from the census line
> above; neither is incremented from a prior revision.

**Note how narrowly the safe rows escape.** `WithBreakerThreshold` shares row-shape with `WithCompletionSize` and
is safe only because `b.fails` is a scalar; `WithMaxPayloadBytes` shares a verdict string with `WithMaxBodyBytes`
and is safe only because its `len(b)` test runs on an already-materialised slice. **In each pair one row is safe
and the other is a class member** — which is exactly why the criterion, not the string, is normative.

**`WithSuccessStatus` already rejects `1<<62`** with `msghttp: status code must be in [100,599]` — it belongs in
the "rejects" arm of AC-5, not the "allocation-free" arm.

**The credit gate (`credit.go:21`, `make(chan struct{}, n)`) is an allocation site, not an option**, and is
deliberately **not** a row here — AC-5 half 1 fails in either direction against this table's keys, so a non-option
key would guarantee a failure. Its zero-size-element property is real, load-bearing and covered by AC-4.

**`WithPollMaxBatch` is DERIVATIVELY safe — its safety is created by this increment, not pre-existing**
(round-4 **m4-5**; revision 4 promised a note here and never wrote it). `held` is bounded by
`min(pollMaxBatch, free credits)` (`poller.go:34-36`), and free credits are `maxInFlight` (`consumer.go:385`) —
so the row is safe **because `maxInFlightCeiling` exists**. A future change relaxing that ceiling, or decoupling
the credit gate from `maxInFlight`, **must revisit this row**. Say so rather than leaving it to be rediscovered.

## 3. The contract

> **No exported msgin sizing option can panic, corrupt runtime state, or leave a bounded structure unbounded —
> at construction or at any later use.** A size outside the option's documented range is reported as a typed error:
> through the constructor's own return if it has one, otherwise through the first use of the object it produced.

The invariant is stated over **what the size bounds**, not over a file list, so a sizing option added later is
governed without this spec being edited (§6 AC-5).

The second sentence is deliberately identical in shape to Spec 015 §3's, and the two reporting families below are
**the same R1 and R2 that Spec 015 defines**. This increment adds no third mechanism and no new concept — it
extends an existing, shipped one to a second fault.

### 3.1 R1 — reject at construction (8 of 9)

Eight knobs have an error-returning constructor between the option and the hazard. Each existing lower-bound check
gains an upper arm returning **the same sentinel**. **`lo` and `hi` are both stated here**, because AC-2b requires
asserting the full `[lo, hi]` render and revision 3 left `lo` undefined for every `<= 0` knob (round-3 **m3-7**):

| Knob | Checked in | Existing arm | `lo` | `hi` (added arm) |
|---|---|---|---|---|
| `WithMaxInFlight` | `NewConsumer` `consumer.go:272` | `< 1` → `msgin.ErrInvalidMaxInFlight` | `1` | `maxInFlightCeiling` |
| `WithConcurrency` | `NewConsumer` `consumer.go:262` | `< 1` → `msgin.ErrInvalidConcurrency` | `1` | `concurrencyCeiling` |
| `WithConnectionBuffer` | `NewConfig` `options.go:1164` | `<= 0` → `msghttp.ErrInvalidConnectionBuffer` | `1` | `maxConnBufferCeiling` |
| `WithCapacity` | `NewQueueStore` `queuestore.go:99` | `<= 0` → `msgin.ErrInvalidCapacity` | `1` | `maxCapacityCeiling` |
| `WithMaxGroups` | `NewGroupStore` `groupstore.go:91` | `<= 0` → `msgin.ErrInvalidCapacity` | `1` | `maxGroupsCeiling` |
| `WithMaxConnections` | `NewConfig` `options.go:1158` | `<= 0` → `msghttp.ErrInvalidMaxConnections` | `1` | `maxConnectionsCeiling` |
| **`WithCompletionSize`** | `NewAggregator` (**new arm**, §3.9) | *none today* | `1` | `completionSizeCeiling` |
| **`WithReplayBuffer`** | `NewConfig` `options.go:1174` | `<= 0` → `msghttp.ErrInvalidReplayBuffer` | `1` | `replayBufferCeiling` |

A `<= 0` check and a `< 1` check are the same predicate over `int`, so **`lo = 1` for all eight**;
`memory.WithBuffer` is the only knob with `lo = 0` (§3.2, unbuffered is legal). `WithCompletionSize` is the one row
with **no existing arm** — it gains both bounds at once. **`WithReplayBuffer`'s existing arm is a *flattened
gate*** — `if cfg.replayBufferSet && cfg.replayBuffer <= 0` — with **no default assignment**, which is neither the
`if !set {default} else if` of R1-a nor the nested form of R1-c. Plan 029 calls it **R1-e**; do not pattern-match
it onto a sibling (that mis-assignment is round-2 **M2-2** verbatim).

**The error is not `Permanent`-wrapped**, matching Spec 015 §3.1 and
[ADR 0029 §5.0b](../adrs/0029-eip-lexical-alignment.md)'s decision **D-M** (the constructor arm): it is handed back
at construction and never travels through a `MessageHandler`, so a retry classification on it is meaningless.

**It IS wrapped with the offending value, the site and the whole range** (round-1 **M-6**, reshaped by round-2
**M2-1**). Revision 1 returned the sentinel bare while ADR 0032 D-X justified sentinel reuse on the message
carrying the value; one of the two had to give, and debuggability is CLAUDE.md's stated core criterion.

**One shape, used by R1 and R2 alike:**

```go
fmt.Errorf("%w: %s: %d not in [%d, %d]", sentinel, site, n, lo, hi)
// endpoint.WithMaxInFlight(0)        → "msgin: max in-flight out of range: endpoint.WithMaxInFlight: 0 not in [1, 1048576]"
// endpoint.WithMaxInFlight(1<<20+1)  → "msgin: max in-flight out of range: endpoint.WithMaxInFlight: 1048577 not in [1, 1048576]"
```

> **AS DELIVERED, this shape is rendered by a helper, not by nine inline `fmt.Errorf` calls.** The Plan 029
> Task 8 `/simplify` pass replaced them with a per-package unexported
> `checkRange(sentinel error, site string, n, lo, hi int) error` — one copy each in `endpoint/helpers.go`,
> `routing/helpers.go`, `adapter/memory/helpers.go`, `adapter/http/helpers.go`, alongside the `nilOptionAt`
> those files already carry under ADR 0031 **D-R**. It returns `nil` when `n` is in `[lo, hi]` and this exact
> `fmt.Errorf` otherwise, so a call site reads
> `if err := checkRange(sentinel, site, n, lo, hi); err != nil { return nil, err }`.
>
> **The reason is not DRY.** Inline, each site spelled each bound **twice** — once in the `if`, once in the
> format arguments — and the two spellings had already diverged (`<= 0` guards printing a lower bound of `1` in
> `memory` and `msghttp`, against `< 1` elsewhere). Nothing rendered wrongly, but only a hand-written
> `EqualError` string stood between the next edit and a message describing a range the code does not enforce.
> With the helper the **enforced** range and the **printed** range are the same two values, by construction.
> The **sentinel stays a parameter** so each knob keeps its own `errors.Is` target (D-X) and so R2's
> `msgin.Permanent`-wrapped sentinel stays visible at its call site rather than becoming a flag inside the
> helper. The snippets in this section and in §3.3 show the *rendering*, which is unchanged and is what AC-2b
> asserts verbatim; they are no longer literal transcriptions of the call sites.

**Why not revision 2's `"%d exceeds %d"`.** Folding both bounds into one condition and rendering *"exceeds"* makes
the message **lie on the lower arm**: `WithMaxInFlight(0)` would read `"…must be >= 1: 0 exceeds 1048576"`, which
is false — 0 exceeds nothing. That arm ships correct today, so revision 2's fix would have *introduced* a
debuggability defect on a working branch while claiming to add one. The `[lo, hi]` form is true at both ends, and
naming the **site** is what §3.5's three-producer resolution needs anyway. Revision 2 also had R1 say
`"%d exceeds %d"` and R2 say `"%d not in [0, %d]"` — two shapes for one idea in one increment; this is now one.

**The lower arm's rendered message changes**, which §3.1's *"each existing lower-bound check gains an upper arm"*
did not previously acknowledge. It is a message-text change only: `errors.Is` is unaffected, `Permanent` is still
absent, and no test asserts these strings (verified — every assertion goes through `errors.Is`/`require.ErrorIs`).

### 3.2 R2 — latch, report at first use (1 of 9)

`memory.New` returns `*Broker` with **no error**. `memory.WithBuffer` therefore reuses the latch Plan 028 shipped
in that exact constructor: the fault is stored on the `Broker` and reported by **`Send` and `Stream`**, wrapped in
`msgin.Permanent`, before either method's own logic (ADR 0031 D-V).

`memory.WithBuffer` is the only R2 member. It is R2 because `memory.New`'s signature makes it so, not by
preference — the same structural test Spec 015 §2.1 applies.

### 3.3 The guard is the `return`, not the latch — the D-U interaction

**This is the subtlety the backlog entry flagged, and it is where a plausible implementation is wrong.**

ADR 0031 D-U makes `memory.New`'s apply loop `continue` past a nil option so every non-nil option still applies.
That means a **later** `WithBuffer(1<<62)` still runs even when the latch is already taken by an earlier nil:

```go
memory.New(nil, memory.WithBuffer(1<<62))
//         ^ latches ErrNilFunc          ^ still applied — D-U — and reaches the make
```

The latch is first-fault-wins, so the sizing fault will **not** be recorded here. If the guard is written as
*"latch, and the latch's `if b.err == nil` decides whether we skip the allocation"*, the allocation runs and the
process panics — with the latch already holding a *different*, unrelated error.

**Required shape.** The range check returns unconditionally, independently of the latch:

```go
func WithBuffer(n int) Option {
	return func(b *Broker) {
		if n < 0 || n > maxBufferCeiling {
			if b.err == nil { // first-fault-wins — the INLINE form Plan 028 shipped (memory.go:60-64)
				b.err = fmt.Errorf("%w: %s: %d not in [%d, %d]",
					msgin.Permanent(msgin.ErrInvalidCapacity), "memory.WithBuffer", n, 0, maxBufferCeiling)
			}
			return // ← LOAD-BEARING: prevents the make whether or not the latch took
		}
		b.ch = make(chan msgin.Message[any], n)
	}
}
```

The `return` is what satisfies the contract; the latch only decides *which* error the caller reads. §6 AC-3 tests
exactly this combination, because every other test passes against the wrong shape.

> **As delivered, the condition and the `fmt.Errorf` are one `checkRange` call** (§3.1's note), so the shipped
> body reads `if err := checkRange(msgin.Permanent(msgin.ErrInvalidCapacity), "memory.WithBuffer", n, 0,
> maxBufferCeiling); err != nil { if b.err == nil { b.err = err }; return }`. **The structure this section is
> about is unchanged and is the whole point: the `return` still sits OUTSIDE the `if b.err == nil` block.**
> Note that the sentinel is `Permanent`-wrapped *before* it reaches `checkRange`, so the wrap lands inside the
> `%w` and the rendered string keeps its `msgin: permanent: ` prefix — wrapping the *returned* error instead
> would print identically while building a different error tree.

**Verified safe** (round-1): `New` initialises `b := &Broker{ch: make(chan msgin.Message[any])}` (`memory.go:59`)
*before* the apply loop, so an early `return` leaves `b.ch` non-nil — no nil-channel deadlock — and D-V reports the
latch before `Send`/`Stream` touch it anyway.

### 3.4 The ceilings

Each ceiling is a statement about **what the knob means**, sized far above its default and far below the
unrecoverable band of §1.1. It is part of the API contract and is documented on the option's godoc with its
rationale, per CLAUDE.md's "Sensible defaults" gate.

| Knob | Default | Ceiling | Why this number |
|---|---|---|---|
| `endpoint.WithMaxInFlight` | 1024 | **`1 << 20`** | 1024× the default. Measured cost at the ceiling: `1<<20 × sizeof(managedDelivery)=48` = **50,331,648 B (48 MiB)** — four orders of magnitude below the fatal band, so the ceiling demonstrably prevents the crash. |
| `endpoint.WithConcurrency` | 1 | **`1 << 16`** | Go's minimum goroutine stack is **~2 KiB** (measured 2,052 B/goroutine; **~4 KiB** under `-race`, 4,114 B), so 65,536 workers cost **~128 MiB of stack — ~257 MiB under `-race`** — before any handler state. |
| `msghttp.WithConnectionBuffer` | 16 | **`1 << 16`** | 4096× the default, **per connection**: at the ceiling each connection allocates `1<<16 × 24` = 1,572,864 B (1.5 MiB). |
| `msghttp.WithMaxConnections` | 1024 | **`1 << 16`** | 64× the default, and at the practical file-descriptor ceiling of a single process. **This is the second factor of the row above** — both are now bounded, so the product is bounded (round-1 **M-7**). |
| `memory.WithBuffer` | 0 (unbuffered) | **`1 << 20`** | Same unit as `WithMaxInFlight` — queued messages. Measured cost at the ceiling: **25,166,072 B (24.0 MiB)**, `sizeof(msgin.Message[any]) = 24`. |
| `memory.WithCapacity` | 1024 | **`1 << 20`** | Same unit again (round-1 **m-10**: three knobs share "queued messages" and must agree). |
| `memory.WithMaxGroups` | 1024 | **`1 << 20`** | In-flight aggregation groups; matched to the queue depth that can feed them. |
| **`msghttp.WithReplayBuffer`** | *unset = off* | **`1 << 16`** | Retained **events**. Matches its two `msghttp` siblings (`WithConnectionBuffer`, `WithMaxConnections`) so all three of this package's ceilings read as one number. Cost is `n × the caller's frame size`, so it is **not** fixed by the library: round 4 measured ~1.2 KiB/event at its own frame size (23.5 MiB at 20,000 events; 93.8 MiB at 80,000 — linear). **Task 2 re-measures under AC-4's protocol** for the godoc; do not copy that ratio, it is fixture-specific. Note the default is **off** (unset ⇒ `replaySize() == 0` ⇒ no ring at all), so this ceiling only ever binds an explicit opt-in. |
| **`routing.WithCompletionSize`** | *none* | **`1 << 16`** | Group **members** per group — 65,536, far beyond any plausible aggregation. Sized by **time, not bytes**: `memory.GroupStore.Add` clones the group snapshot per call, so cost is quadratic — reaching this ceiling costs **48.3 GiB of allocation churn and 8.6 s** (measured, §1.4) against only **31.0 MiB** live (GC'd). That churn is the argument for the ceiling, and the reason AC-1 never runs this knob at it. |

**None of these is reachable by a correct program.** They are the boundary between a workload and a typo. The
library bounds each factor; it does **not** attempt to police the product of two ceilings — a caller who sets both
`WithMaxConnections` and `WithConnectionBuffer` to their maxima has explicitly asked for something no process can
serve, and gets two documented ceilings rather than a third derived one.

### 3.5 Sentinel reuse — no new exported sentinel

Every one of the eight R1 knobs already has a typed sentinel for its lower bound, and callers already `errors.Is`
against it. **The upper arm returns the same sentinel.** The exported surface gains nothing, existing `errors.Is`
targets keep working, and a caller's "this size is wrong" branch handles both ends without change.

**`msgin.ErrInvalidCapacity` has FOUR producers after this increment** (round-1 **M-1**; the fourth added by
revision 4's `WithCompletionSize`, §3.9): `memory.NewQueueStore` (`queuestore.go:100`), `memory.NewGroupStore`
(`groupstore.go:92`), `memory.WithBuffer`, and `routing.NewAggregator`. One sentinel cannot carry four different
stated ranges, so:

- the **sentinel's own message stays generic** — `"msgin: capacity out of range"`;
- the **per-site range lives in the wrapping message** (§3.1's `[lo, hi]` shape) and in each option's godoc.

**It spans four different UNITS, not merely four ranges** (round-4 **m4-10**): queue depth (`WithCapacity`),
group **count** (`WithMaxGroups`), channel **buffer** (`WithBuffer`) and group **members**
(`WithCompletionSize`) — e.g. `msgin: capacity out of range: routing.WithCompletionSize: 65537 not in [1, 65536]`.
D-X's reuse argument still holds (one `errors.Is` target for *"this size is wrong"*, and the site name
disambiguates), but a **fifth** producer should be a conscious decision rather than a default.

This is why §3.1 wraps rather than returning bare: with four producers, the sentinel text cannot state the bound
and the wrap must.

**All six sentinel messages are genericised in the same increment, not just `ErrInvalidCapacity`** (round-2
**m2-4**; the count is **six** only because revision 5's BLOCKER-1 fix adds `ErrInvalidReplayBuffer` — through
revision 4 it was five, printed as six, which round-4 **m4-4** caught). Every one of them states a *lower bound only*, under a range that is now two-ended; leaving five as-is
while fixing one is the fix-the-instance pattern this project keeps being burned by. Measured current text:

| Sentinel | Declared | Today | Becomes |
|---|---|---|---|
| `msgin.ErrInvalidConcurrency` | `errors.go:51` | `msgin: concurrency must be >= 1` | `msgin: concurrency out of range` |
| `msgin.ErrInvalidMaxInFlight` | `errors.go:144` | `msgin: max in-flight must be >= 1` | `msgin: max in-flight out of range` |
| `msgin.ErrInvalidCapacity` | `errors.go:258` | `msgin: capacity must be > 0` | `msgin: capacity out of range` |
| `msghttp.ErrInvalidMaxConnections` | `adapter/http/errors.go:145` | `msghttp: max connections must be > 0` | `msghttp: max connections out of range` |
| `msghttp.ErrInvalidConnectionBuffer` | `adapter/http/errors.go:150` | `msghttp: connection buffer must be > 0` | `msghttp: connection buffer out of range` |
| **`msghttp.ErrInvalidReplayBuffer`** | `adapter/http/errors.go:155` | `msghttp: replay buffer must be > 0` | `msghttp: replay buffer out of range` |

(**Six** rows for **nine** knobs: `WithCapacity`, `WithMaxGroups`, `WithBuffer` and `WithCompletionSize` all share `ErrInvalidCapacity`.) **Verified test-safe** — no
test asserts any of these strings; every assertion goes through `errors.Is`/`require.ErrorIs`, and the three
grep hits outside that form are two subtest *names* and a slice literal that is then `ErrorIs`-checked.

### 3.6 `WithBuffer(-1)`'s silent clamp is FOLDED IN

Revision 1 left this out of scope and flagged it to the audit. **The audit ruled: fold it in**, and revision 2
does. `WithBuffer(-1)` now returns `ErrInvalidCapacity` through the R2 latch like any other out-of-range value —
the range is `[0, maxBufferCeiling]`, checked in one condition (§3.3's shape).

Four reasons, all from the round-1 record:

1. **§5's own rejection of silent clamping condemns it verbatim.** Rejecting "clamp to the ceiling" because *"the
   caller believes they configured a buffer they did not get"* while preserving a clamp at the other end of the
   same function is a contradiction a reader finds in one pass.
2. **It is the only sizing knob left asymmetric** — `WithCapacity(-1)`, `WithMaxInFlight(-1)` and
   `WithConnectionBuffer(-1)` all return typed errors today.
3. **It costs three lines in a closure Task 5 already rewrites**, versus a second increment reopening the same ten
   lines and re-widening the same godoc.
4. **It resolves §3.5's M-1 problem** rather than compounding it: `ErrInvalidCapacity` comes to mean "out of range"
   uniformly at **all four** producers (§3.5) instead of "≤ 0" at some and "> ceiling" at others.

This is a behavioral change to shipped, non-crashing code — free at pre-v1 with no consumers and no tags.

### 3.7 What this spec does not change

1. *(removed in revision 6)* — `WithReplayBuffer`'s retention **is** in scope: it became the 9th defective knob (§1.5). This entry survived one revision after that change, as one of seven twins (round-5 **BLOCKER-2**).
2. **No option's default changes**, and no option gains or loses a parameter. There is **no signature change** in
   this increment — unlike Plan 028, which took one (ADR 0031 D-T).
3. **The zero-size-element sites gain no runtime check** — they cannot panic. They gain a *test* (AC-4) and a
   comment naming the property their safety rests on.
4. **`time.Duration` knobs are deliberately out of scope** (round-1, reshaped by round-2 **M2-4**). The AST sees
   `time.Duration`, not `int64`, so they are outside both the census and the gate *by construction*.

   > **Revision 2 asserted a FALSE fact here and it is corrected, not merely softened.** It stated that
   > `NewTokenBucket`'s `time.Duration(float64(time.Second) / rps)` *"overflows negative for a sub-normal `rps`"*.
   > Round 1 filed that as **suspected**; revision 2 promoted it to an asserted fact in two documents **without
   > running it**. Measured on this project's reference platform (darwin/arm64, `GOARCH=arm64`):
   >
   > ```
   > rps=1e-300   float=+Inf   Duration=2562047h47m16.854775807s (int64=9223372036854775807)   NewTokenBucket err=<nil>
   > rps=1e-310   float=+Inf   Duration=2562047h47m16.854775807s (int64=9223372036854775807)   NewTokenBucket err=<nil>
   > rps=5e-324   float=+Inf   Duration=2562047h47m16.854775807s (int64=9223372036854775807)   NewTokenBucket err=<nil>
   > ```
   >
   > It saturates to **+MaxInt64 (~292 years)**, not negative. An out-of-range float→int conversion is
   > implementation-defined in Go: arm64 saturates, amd64 would yield `-MaxInt64-1` — so the original claim was
   > *platform-specific* **and** wrong on the platform every other figure in this bundle was measured on.

   The accurate statement, which is what belongs in the record: *a sub-normal `rps` saturates `interval` to
   `MaxInt64` on darwin/arm64 (`-MaxInt64-1` on amd64), so `Wait` blocks for ~292 years rather than rate-limiting.*
   Note `ratelimit.go:43` already rejects `rps <= 0` with `ErrInvalidRateLimit` — a sub-normal *positive* `rps`
   passes that check — and `b.interval` never reaches a ticker (`:81`, `:101` use it arithmetically), so the
   failure mode is a hang, not an overflow. It belongs to its own increment.

   **The exclusion is stated as "outside the gate BY CONSTRUCTION" — not as "checked"** (round-3 **m3-2**).
   Revision 3 claimed the stronger *"and currently safe, checked"* on the strength of enumerating the five
   `NewTicker`/`NewTimer` sites. Those five are correct and each is guarded — `sse_server.go:262`
   (← `heartbeat > 0`), `attempts.go:77` (← `attemptTTL > 0`), `exchange.go:449` (← the `timeoutSet && <= 0`
   check at `exchange.go:317`), `producer.go:505`, `aggregator.go:511` (← an explicit `interval <= 0` guard) —
   **but the enumeration is not the whole class.** Non-test `clock.After(d)` with a variable duration also
   consumes a `time.Duration`, at five further sites: `resilience/ratelimit.go:85`, `endpoint/poller.go:172`,
   `endpoint/consumer.go:485`, `adapter/http/sseclient.go:442`, `adapter/cron/source.go:280` — and the **first of
   those is fed by the very saturated `b.interval` this paragraph is about**. Auditing all ten is this increment's
   job only if `time.Duration` is in scope, and it is not. So the claim is narrowed to the one that holds:
   `time.Duration` is **outside the gate by construction**. ADR 0032 D-AA states the boundary is deliberate.

### 3.8 The byte-ceiling class is DEFERRED, with a stated reason — not called "safe"

`msghttp.WithMaxBodyBytes`, `msghttp.WithMaxEventBytes` and — added in revision 6 — **`msghttp.WithMaxResponseBytes`**
**are class members** by §2.1's criterion: each is the sole bound on an accumulation, and — worse than any knob in
§1.3 — the accumulation is driven by a **remote peer**, not by the caller. Measured:

```
WithMaxBodyBytes(1048576)             body=64 MiB -> err=msghttp: decode request failed: http: request body too large   TotalAlloc delta=5.0 MiB
WithMaxBodyBytes(4611686018427387904) body=64 MiB -> err=<nil>                                                          TotalAlloc delta=375.2 MiB
WithMaxResponseBytes(4611686018427387904) reply=64 MiB -> err=<nil>, 67,108,864 bytes RETAINED                          TotalAlloc delta=376.4 MiB
```

**`WithMaxResponseBytes` was certified *safe (d) — discarded, never retained* through revision 5** (round-5
**BLOCKER-1**), on the strength of `drainBounded` (`io.CopyN(io.Discard, body, max)`, `exchange.go:126`). That is
**five of the six** reads of `cfg.maxResponseBytes`. The sixth is the one that matters:

```go
// exchange.go:130-131 — the SUCCESS path
max := x.cfg.maxResponseBytes
body, err := io.ReadAll(io.LimitReader(resp.Body, max))   // ← RETAINED as the reply payload
```

The option's own godoc states the accumulation in its first line. Its churn (376.4 MiB) is within 0.3% of its
already-acknowledged twin `WithMaxBodyBytes` (375.2 MiB) — the two are the same defect at opposite ends of the
same exchange.

**None of the three is given a ceiling in this increment, and none is certified safe.** The distinction matters: a
false *"safe"* verdict would make AC-5 half 2 ship a gate asserting `WithMaxBodyBytes(1<<62)` *"accepts and its
product is usable"* — a test that actively certifies an unbounded remote-driven read as conformant and fails if
anyone later bounds it. That is the inversion §1.1 warns about, embedded in the class gate.

**Why deferred rather than fixed here** — CLAUDE.md's Sensible-defaults gate names this exact case:

> *"If **no** value can be safe for an unknown caller (e.g. **a byte cap that depends on the caller's legitimate
> payload size**), make it **explicit/opt-in** with a clear typed error or documented off state rather than
> guessing a default that lulls the caller into a false guarantee."*

A ceiling in *messages*, *connections*, *events* or *goroutines* is a statement about what the knob means. A ceiling in
**bytes** is a statement about the caller's payload, which the library cannot know — a 1 GiB ceiling is absurd for
a JSON API and too small for a file upload. The remedy is a different shape (an explicit opt-in unbounded state
with a documented hazard), and choosing it needs its own brainstorm.

**What the follow-up increment actually inherits — revision 4 got this WRONG and it is corrected here**
(round-4 **M4-2**). Revision 4 asserted that `maxBody()` (`options.go:236`) back-fills the default for `n <= 0`,
so *"`WithMaxBodyBytes(-1)` today means use 1 MiB"* and *"there is no off-state to document"*. **Measured, both
halves are false:**

```
NewConfig(WithMaxBodyBytes(-1))  -> err=msghttp: max body bytes must be > 0
NewConfig(WithMaxBodyBytes(0))   -> err=msghttp: max body bytes must be > 0
NewConfig(WithMaxBodyBytes(1<<62)) -> err=<nil>
NewConfig(WithMaxEventBytes(-1)) -> err=msghttp: max event bytes must be > 0
```

`NewConfig` **rejects** an explicit non-positive value at `options.go:1128-1131`; the `maxBody()` back-fill fires
**only** for a hand-built `*Config` that skipped `NewConfig`, and the code's own comment two lines above says so.
*(I read the accessor and not the validator — the same class as the project's stored lesson "verify structural
claims against code", and, like it, the false claim sat next to a comment that contradicted it.)*

**So the follow-up's problem is strictly smaller and differently shaped than revision 4 recorded.** Both byte
knobs already reject an explicit `n <= 0` with a typed error, and **leaving the option unset already IS the
documented default state**. The open question is therefore not *"invent an off-state where a back-fill lives"* but
**"should an explicit off-state exist at all, and if so which sentinel value carries it"** — noting that a
negative `n` is already taken by the rejection, so it would need a new sentinel value, not a reinterpretation.

**What this increment DOES do for them**, at zero risk and zero exported-surface change:
1. §2.1 records them as **class members with a deferred remedy**, with the true reason replacing the false
   *"limit, never allocated"*.
2. Their **godoc gains the hazard disclosure**: the knob is the only bound on a remote-driven read, the default is
   safe, and a large explicit value removes the protection.
3. AC-5 half 2 puts them in an arm that asserts **acceptance without claiming safety** (§6 AC-5).

Tracked as `docs/HANDOVER.md` §8 backlog item 6.

### 3.9 `WithCompletionSize` needs a config field before it can be validated

**This is the one knob whose fix is not a one-line condition**, and the reason is structural. `WithCompletionSize`
sets **only a closure** — there is no value for `NewAggregator` to inspect:

```go
// aggregator.go:132-135, today
func WithCompletionSize(n int) AggregatorOption {
	return func(c *aggregatorConfig) {
		c.release = func(g msgin.MessageGroup) (bool, error) { return len(g.Messages()) >= n, nil }
	}
}
```

`aggregatorConfig` (`aggregator.go:14-21`) has fields for `output`, `correlate`, `release`, `timeout`, `expired`
and `clock` — **no `completionSize`**. So the option must **also record `n`** in a new **unexported** field that
`NewAggregator` validates alongside its existing `correlate`/`release`/`output` checks (`aggregator.go:318-330`).

**Exported surface is unchanged** — a new unexported struct field is not exported surface, so Global constraint 3
holds and `apidiff` still reports 0/0. The signature of `WithCompletionSize` does not change.

**The bound is validated on the VALUE PASSED, whether or not a later option replaces the release strategy**
(round-4 **m4-9**). `WithReleaseStrategy` / `WithReleaseWhen` overwrite `c.release`, so
`WithCompletionSize(1<<62)` followed by either leaves the huge `n` **inert** — accepted today, rejected after this
increment. That is a **new construction-time rejection of a currently-legal, currently-harmless config**, and it
is deliberate: a nonsense size should fail loud rather than depend on whether a later option happens to mask it.
Stated here and on the godoc, with a table case pinning it, so the behaviour is decided rather than incidental.

**The sentinel** is `msgin.ErrInvalidCapacity`, reused per D-X — making it a **fourth** producer, not a third.
§3.5's three-producer arithmetic is updated accordingly; the generic message (`"msgin: capacity out of range"`) is
what makes four producers tolerable.

## 4. Documentation contract

Every knob this spec touches states, on the option's own godoc: **the range, the ceiling's value, why that ceiling,
and the typed error a violation produces**. For `WithBuffer`, the godoc additionally states that the fault surfaces
at `Send`/`Stream` rather than at `New`, and why (no error return) — the same disclosure Spec 015 §4 requires of
every R2 member.

The safe rows of §2.1 whose safety rests on a structural property — the two zero-size-element sites,
`endpoint/credit.go:21`'s credit gate and `adapter/memory/queuestore.go`'s `sem`, both `make(chan struct{}, n)` —
get a one-line comment at the allocation site naming that property, so a future edit breaking it is visible in
review rather than only in AC-4.

**One existing godoc sentence becomes FALSE and must be retired in the same commit** (round-2 **m2-7**).
`adapter/memory/memory.go:35-37` currently reads:

> *"A negative `n` is **clamped to 0** rather than panicking, honoring the library's no-panic-on-caller-input
> contract."*

§3.6 folds the negative end into `ErrInvalidCapacity`, so that sentence is wrong the moment the change lands. It
is deleted and replaced with the stated range plus the R2 disclosure. This is precisely the class CLAUDE.md's
stored lesson names — *"all three fix rounds in Plan 028 were godoc, not logic"* — so the plan carries an explicit
bullet for it rather than trusting it to be noticed, and AC-3b's `WithBuffer(-1)` case is cross-referenced from it
so the godoc edit is not orphaned from its test.

## 5. Alternatives rejected

| Alternative | Why rejected |
|---|---|
| **Guard at `makechan`'s own ceiling** (`elemSize × n > maxAlloc`) | §1.1: admits the *unrecoverable* band while rejecting the recoverable one — it certifies the worse value as safe. Also unportable and a copy of an unexported runtime constant. |
| **`recover()` around each allocation** | §1.1 row 3 is `runtime.throw`, not `panic`. `recover` cannot see it. It also normalizes panic-as-control-flow in a library whose stated criterion is debuggability. |
| **Bound via `GOMEMLIMIT`** | Measured: `GOMEMLIMIT=67108864` still yields `fatal error: out of memory` on the `2^41` case. It is a GC-pacing soft limit, not an allocation gate. |
| **One shared byte budget** | Ignores what each knob means, so the error message cannot explain itself to the caller. |
| **Ceiling plus an explicit opt-out** (`WithUnsafeUnboundedSizing`) | New exported surface on a pre-v1 API we are keeping small, whose only purpose is to re-enable the crash. |
| **Clamp silently to the ceiling** | The caller believes they configured a size they did not get — the "silently-substituted default" CLAUDE.md's Sensible-defaults gate forbids. §3.6 applies the same reasoning to the negative end. |
| **Mint `memory.ErrInvalidBuffer`** | Rejected in favor of reusing `msgin.ErrInvalidCapacity` so one `errors.Is` target covers all three of `memory`'s bounded constructors (§3.5); the wrapping message separates the sites. ADR 0032 D-X. |
| **Decouple `workerCh`'s buffer from `maxInFlight`** so the knob never sizes an allocation (round-1 **m-3**) | The real bound is the credit gate (`chan struct{}`, safe at any `n`); `workerCh` is sized to match only for the deadlock fix documented at `consumer.go:375-384`. Rejected: it perturbs a delicate, repro-proven deadlock fix, and helps exactly one of the seven knobs. |
| **Export the ceilings** (`const MaxInFlightCeiling = 1 << 20`) so callers can pre-validate (round-1 **m-7**) | The natural request from the first caller who hits one. Rejected for now: new exported surface against Global constraint 3, and the wrapped error message already names the bound. Revisit if a consumer asks. |
| **Fix only `memory.WithBuffer`** (the backlog entry as written) | Leaves the two *worse* panics — `Consumer.Run` and `ServeHTTP` — plus three OOM levers open, repeating the fix-the-instance pattern that cost Plan 028 three audit rounds. |

## 6. Verification

**AC-1 — no panic, no corruption, at either surface.** For each of the **9** defective knobs, a case passing
`1 << 62`:
- the eight R1: the constructor returns a non-nil error, **and** a second case constructs with a *valid* size and
  confirms the hazardous path still runs. Asserting only the error passes against a constructor that rejects
  everything.
- `memory.WithBuffer`: `New` does not panic, **and** both `Send` and `Stream` return the fault.

**"The hazardous path still runs" means different things for the two families, and demanding one of them for all
seven is not executable** (round-2 **BLOCKER-3**). Revision 2 required *"the hazardous path runs at the ceiling"*
for all 7 (now **9**, with `WithCompletionSize` and `WithReplayBuffer`). For `msghttp.WithMaxConnections` the hazardous path is the admission check
`sse_server.go:182 len(s.conns) >= s.cfg.maxConns()`; running it at `maxConnectionsCeiling = 1<<16` needs **65,536
concurrently registered SSE connections**, each holding a live `ServeHTTP` goroutine (~257 MiB of stack alone
under `-race`, measured), a `make(chan []byte, connBuffer)`, and a request/response pair — under a package whose
root sibling runs `goleak.VerifyTestMain`. It cannot be written. So the criterion splits:

| Family | Knobs | What the accepting case must do |
|---|---|---|
| **Allocating** — the allocation *is* the hazard | `WithMaxInFlight`, `WithConnectionBuffer`, `WithBuffer` | **Run the hazardous path at the ceiling.** Measured and affordable: 48.0 MiB / 1.5 MiB / 24.0 MiB. |
| **Growth** — the hazard is *"the cap stops capping"* | `WithCapacity`, `WithMaxGroups`, `WithMaxConnections`, **`WithCompletionSize`**, **`WithReplayBuffer`** | The property is a fact about the **comparison, not the ceiling value**. **Prove it at small `n`**, then at the ceiling assert **construction + acceptance only**. For `WithCompletionSize` this is not a convenience: its ceiling costs 48.3 GiB of churn and 8.6 s (§1.4). |
| **`WithConcurrency`** — spawns goroutines | `WithConcurrency` | Run at the ceiling, and **state the cost in the task**: 65,536 goroutines ≈ **128 MiB of stack, ~257 MiB under `-race`** (measured). |

The small-`n` proof is not a concession — **the repo already proves it that way** for the `GroupStore`:

```
MaxGroups(1):                      add1=<nil> add2=msgin: message dropped by overflow policy   # cf. groupstore_test.go:30-39
Capacity(1) [OverflowReject]:      enq1=<nil> enq2=msgin: message dropped by overflow policy
Capacity(1) [DEFAULT policy]:      enq1=<nil> (2nd Enqueue BLOCKS — see below)
WithCompletionSize(2) over 6 msgs: released=3                        # needs the 5-part fixture (AC-5)
WithReplayBuffer(8) + Last-Event-ID: 7 frames replayed after entry 13; 0 after the EVICTED entry 1      # needs the FULL fixture — see AC-5 / Task 4
WithReplayBuffer(8):               ring holds 8 after 20 Sends, 17 KiB regardless of how many follow
```

> **🔴 The `WithCapacity` row REQUIRES `memory.WithOverflow(msgin.OverflowReject)` and HANGS without it**
> (round-3 **M3-1**). `QueueStore`'s default policy is `OverflowBlock`, so the second `Enqueue` blocks on
> `s.sem <- struct{}{}` (`queuestore.go:124-129`) until the test binary's 10-minute panic. Revision 3 named that
> fixture **only in the adjacent bullet**, scoped to the optional ceiling-sized exercise that this very split
> removed — so the mandatory case was left un-runnable. **The `groupstore_test.go:30-39` precedent does NOT
> transfer: `GroupStore.Add` has no overflow policy at all.** This is the same class as `m-2`'s
> `serveInBackground` and `M2-5`'s blocking arm, third instance.

> **🔴 `WithReplayBuffer`'s small-`n` proof is NOT "the ring holds 8" — that is UNOBSERVABLE blackbox**
> (round-5 **M5-3**, the *fifth* consecutive unexecutable AC). `*SSEServer` exports only `ServeHTTP`, `Close` and
> `Send`; there is no accessor for the ring. **The only observation is a `Last-Event-ID` replay request**, and the
> repo's `serveInBackground` (`adapter/http/sse_server_test.go:180`) **hard-codes its request**, so it cannot
> issue one — Task 2 needs its own helper. Measured: with `WithReplayBuffer(8)` and 20 `Send`s, resuming from
> entry 13 replays **7** frames, and resuming from the **evicted** entry 1 replays **0**. Revision 5's claim that
> this case *"needs no connection at all"* is **false** — it needs exactly one, carrying the header.

State this split explicitly, so a reader does not see "all 9" and go looking for 65,536 connections — or for
a 65,536-member aggregation group, which costs 48.3 GiB of churn (§1.4).

**AC-2 — the ceiling is the boundary, asserted on both sides.** For each of the 9, a table with `ceiling`
(accepted, per AC-1's split) and `ceiling + 1` (rejected). A test that only asserts `1 << 62` fails passes against
an off-by-many implementation and against one that rejects every value above the default.

**AC-2b — the wrapped message is true at BOTH ends** (§3.1). Per R1 knob, **two** cases assert the rendered
message — one at `ceiling + 1` and **one at the lower bound** (`0` or `-1`) — each asserting the site name and the
full `[lo, hi]` range render, not merely that `errors.Is` matches. The lower-end case is mandatory: it is the one
that would have caught revision 2's `"0 exceeds 1048576"`, and without it §3.5's three-producer resolution is
unenforced.

**AC-3 — §3.3's `return` is proven, not assumed.** `memory.New(nil, memory.WithBuffer(1<<62))` must not panic, and
must report **`ErrNilFunc`** (the first fault, index 0), *not* `ErrInvalidCapacity`. This case fails against an
implementation that gates the allocation on the latch, which is the natural way to write it. It must be
**mutation-tested**: moving the `return` inside the latch's `if` must make it fail.

**AC-3b — the reverse ordering, and the negative end.** `memory.New(memory.WithBuffer(1<<62), nil)` reports
`ErrInvalidCapacity` (index 0 wins). `memory.New(memory.WithBuffer(-1))` reports `ErrInvalidCapacity` (§3.6).

**AC-4 — the zero-size-element property is pinned, WITHOUT depending on this increment's own ceilings.**

> **Revision 1's AC-4 was unexecutable against its own increment** (round-1 **BLOCKER-1**). It proposed building a
> consumer whose credit gate is `1<<62`; the gate's size **is** `maxInFlight` (`consumer.go:385`), which Task 1
> caps at `1<<20`, so no such consumer can be constructed — and blackbox-only forbids reaching `newCreditGate`
> directly. `credit.go:21` would have shipped with **zero** coverage: the exact silent regression AC-4 exists to
> catch.

The property is therefore pinned in two executable parts:
- **Direct:** assert `make(chan struct{}, 1<<62)` succeeds (`cap == 4611686018427387904`), stating the invariant
  `credit.go:21` and `queuestore.go:108` rest on.
- **In situ, at the ceiling:** build a consumer at `maxInFlightCeiling` and a `QueueStore` at
  `maxCapacityCeiling`, and **assert the allocation delta**, not merely the absence of a panic.

**The in-situ assertion is stated PER SITE, because one bound does not fit both** (round-2 **M2-3**). Revision 2
justified the delta with *"`1<<20` of a zero-byte element is nothing, of a 48-byte element is 48 MiB, so the delta
is what distinguishes them."* That is true for the `QueueStore` and **false for the consumer**, whose baseline at
`maxInFlightCeiling` is *already* 48 MiB from `workerCh` (`consumer.go:384`) — so mutating `credit.go:21` moves the
delta 48 → 96 MiB, not 0 → 48. An upper-bound-only assertion is also vacuous: a measurement taken before `Run`
allocates passes trivially. Measured this revision, 3 runs each, `TotalAlloc`, noise floor exactly `0` B:

| Site | Baseline (measured) | Mutant | Assertion |
|---|---|---|---|
| `NewQueueStore(WithCapacity(1<<20))` | **288 B** (3/3) | `chan struct{}` → **`chan msgin.Message[any]`** at `queuestore.go:108` ⇒ `1<<20 × 24` = **25,165,824 B** | **`delta < 1 MiB`** — 3,600× above the noise floor, **24×** below the mutant |
| `NewConsumer(WithMaxInFlight(1<<20))` + `Run` | **48.0 MiB** (50,383,592 / 50,336,152 / 50,334,552 B) | `credit.go:21` `chan struct{}` → `chan managedDelivery` ⇒ ~96 MiB | **`40 MiB < delta < 64 MiB`** — the **lower** bound is mandatory |
| `NewGroupStore(WithMaxGroups(1<<20))` | **120 B** (3/3) | — | map, no preallocation; recorded for completeness |

**`credit.go:21` gets its own mutant, and this is not optional.** Revision 2's Task 5 named only
`queuestore.go:108` — leaving unmutated, for a second revision running, the exact site round-1 BLOCKER-1 was
entirely about. Mutating `credit.go:21` must break the consumer arm.

**Three measurement conditions, or the assertion silently rots** (round-2 **m2-8**): the test must **not** call
`t.Parallel()`; it must `runtime.KeepAlive` the product (else the ceiling-sized channel is collectible before
`ReadMemStats`); and it must read **`TotalAlloc`**, not `HeapAlloc` — cumulative and GC-independent. Written this
way the measurement is **not** flaky: it was attacked specifically and reproduced identically across repeats,
plain and under `-race`, with a no-op noise floor of exactly `0` bytes.

**AC-5 — the class gate.** A root blackbox test in the shape of the shipped `option_guard_gate_test.go`:
1. **Completeness (AST).** Walk every non-test `.go` file from `os.Getwd()` — for a root-package test that is the
   repo root, so it reaches all 8 modules without `go.work` — and collect every exported function
   (**`Recv == nil`; §2.0 states and justifies that boundary**) with an `int`/`int64` parameter **in any position**
   (§2's note: `NewTokenBucket`'s `burst` is why "any position", not "first"). Fail if that set differs from the
   conformance table's key set in **either** direction. **The set is 17** (§2.1) — measured, not assumed:

   ```
   $ go run ./scan <repo-root>          # go/ast, all 8 modules, non-test files
   === EXPORTED FUNCTIONS (Recv==nil) with int/int64 param in ANY position: 17
   === EXPORTED METHODS with int/int64 param: 27          ← excluded by the Recv==nil boundary
   ```

2. **Conformance (behavioral) — every key is executable, none is a declaration.** **17 AST-discovered rows + 2
   manual rows** for the class members the `Recv == nil` boundary excludes but that a root test *can* construct
   (`memory.QueueStore.Claim`, `channel.QueueChannel.Poll` — §2.0). **FOUR arms — behavioral, and NOT a
   relabelling of §2.1's three verdicts** (see §2.1's "classification arms ≠ AC-5 behavioral arms" note, which is
   normative for this table):

   | Arm | Rows | Assertion |
   |---|---|---|
   | **class member, fixed here** (9) | the 7 + `WithCompletionSize` + `WithReplayBuffer` | **reports the fault through the surface §3 names for it** — the constructor's return, **or the first use of the object it produced** |
   | **rejects, but not a class member** (1) | `WithSuccessStatus` | **rejects** `1<<62` through its **pre-existing** `[100,599]` check. It is `safe (a)` by §2.1's criterion and **nothing here fixes it** — the arm exists because it cannot honestly sit in either the `fixed` arm (it is not a class member) or the `safe` arm (which asserts *accepts*) |
   | **class member, ceiling deferred** (3) | `WithMaxBodyBytes`, `WithMaxEventBytes`, `WithMaxResponseBytes` | **accepts** `1<<62`, and the row is annotated *"class member, remedy deferred — §3.8"* so it never reads as a safety certificate. **When §3.8's ceiling lands, MOVE the row to the `fixed` arm — do not weaken the production check to keep the gate green** |
   | **safe** (3 + `burst` + the 2 manual rows = 6) | §2.1's safe rows **minus `WithSuccessStatus`** | **accepts** `1<<62` and its product is usable |

   **9 + 1 + 3 + 6 = 19 rows.** Re-derive from §2.1's arm table; do not increment.

   > **The defective arm is phrased over the SURFACE, not over "rejects"** (round-3 **m3-5**). Revision 3 said
   > *"a defective knob asserts it **rejects** `1<<62` with a typed error"* — which **cannot be written for
   > `memory.WithBuffer`**: after this increment `memory.New(WithBuffer(1<<62))` returns a `*Broker` with **no
   > error**, and the fault surfaces at `Send`/`Stream` (§3.2, R2). One of the 17 rows was unwritable to the
   > stated shape. The wording above matches §3's contract verbatim and covers both families.

> **Revision 1's half 2 asserted behavior for 4 keys of 16 and an inert reason-string for the other 12**
> (round-1 **M-4**), which contradicts ADR 0032 D-AA's claim that it *"cannot be satisfied by a guard that does not
> guard"*. Every key **is** constructible at `1<<62` today — probed behaviorally, not read: 15 of the 17 return a
> nil error, `WithSuccessStatus` rejects (`msghttp: status code must be in [100,599]`), and `WithCompletionSize` is
> **accepted** — the error a bare call returns is a *fixture* gap, not a rejection, so its row belongs in the
> **accepts** arm, not alongside `WithSuccessStatus`. That turns half 2 into a real element-type tripwire for
> `queuestore.go:108` and `groupstore.go:94`, and gives AC-6 probe (b) 19 plantable rows.

**Two of the 17 rows are not one-liners, and the plan must size them** (round-2 **M2-6**):

1. **There are SEVEN `msghttp` keys, not six — and this increment dissolves most of the fixture need**
   (round-3 **M3-3**; revision 3 copied round 2's "six" forward without re-deriving it against its own changes).
   The seven, from the AST scan: `WithConnectionBuffer:883`, `WithMaxBodyBytes:426`, `WithMaxConnections:865`,
   `WithMaxEventBytes:819`, `WithMaxResponseBytes:730`, `WithReplayBuffer:926`, `WithSuccessStatus:566`.
   Partitioned **by arm after this increment**:

   | Arm | Keys | Fixture needed |
   |---|---|---|
   | rejects `1<<62` | `WithConnectionBuffer`, `WithMaxConnections`, **`WithReplayBuffer`** (all newly), `WithSuccessStatus` (already) | `NewConfig` only — one line each |
   | accepts, deferred (§3.8) | `WithMaxBodyBytes`, `WithMaxEventBytes`, **`WithMaxResponseBytes`** | `NewConfig` only |
   | accepts, safe | *(none — `WithMaxResponseBytes` moved to the deferred arm in revision 6)* | — |

   **After revision 5's BLOCKER-1 fix, NO row needs a live `SSEServer` at all.** `WithReplayBuffer` — the one
   candidate revision 4 identified — moves into the **rejects** arm, where `NewConfig` alone suffices. So the
   root-local equivalent of the unexported `serveInBackground` (`adapter/http/sse_server_test.go:180`) is **not
   required for AC-5**; it remains needed only inside `adapter/http`'s own package tests (Plan Task 2). Revision 3
   sized the plan's largest fixture against a set the increment has now dissolved entirely.

   **"Its product is usable" must be DEFINED** — round 2 flagged it as unsized and revision 3 left it undefined.
   For a `NewConfig`-only key it means *"`NewConfig` returns a non-nil `*Config` and a nil error, and the
   accessor for that knob returns the value set"*. For `WithReplayBuffer` it additionally means *"one event
   round-trips through a live `SSEServer`"*.
2. **`WithCompletionSize` needs a FIVE-part fixture, not two** (round-4 **M4-3** — the fourth instance of "an AC
   whose text cannot become a running test"). Measured, the gates fire in this order:

   ```
   NewAggregator(store, fn)                     err=msgin: aggregator output channel is nil
   NewAggregator(store, fn, WithOutputChannel)  err=<nil>
     + WithCompletionSize(1<<62)                err=<nil>
   default correlate, Handle(no corr header)    err=msgin: permanent: msgin: message has no correlation key
   ```

   So the complete fixture is
   **`NewAggregator(store, fn, WithOutputChannel(ch), WithCorrelationStrategy(fixedKey), WithCompletionSize(n))`
   plus `ch.Subscribe(counter)`** — the subscriber is required for `released` to be *observable* at all. With it,
   `WithCompletionSize(2)` over 6 messages yields `released=3`, as AC-1 asserts.

   > **Two prior corrections are themselves retracted.** Round 2 called the second positional parameter a
   > *"release func"*; it is the aggregation **`fn`** (`release` is an *option*) — that correction stands.
   > But revision 3 then called round 2's `msgin: aggregator output channel is nil` **wrong**, and revision 4
   > repeated it. **It is not wrong — it is the NEXT gate**, the one you reach once `store` and `fn` are supplied.
   > Both strings are real and consecutive; calling one "wrong" hid the missing fixture instead of exposing it.
   > Record both, in order.

**AC-6 — vacuity probes, per the project's standing rule.** Each gate half is proven to *fire*: plant a sizing
parameter missing from the table (half 1 must report it) and a table row whose behavioral claim is false (half 2
must report it). **Prove it COVERS, not just that it FIRES** — plant the probe in `adapter/http`, not in root,
since `apidiff`'s Plan 028 blindness came from probing only root.

**AC-7 — every new branch is covered.** The 8 R1 upper arms, the R2 range check and its `return` are hot-path
typed-error branches under CLAUDE.md's test-coverage gate: each needs a covering case, and each must be
**mutation-tested** (delete the arm → a named test fails), per the project's standing rule that a green run is not
evidence.

## 7. Scope

**In:** the 9 defective knobs of §2.1; their ceilings, godoc and typed errors; `WithBuffer`'s negative end (§3.6);
the AC-4 tripwire for the zero-size-element property; the class gate.

**Out:** any signature change (§3.7.2); `time.Duration` knobs (§3.7.4); the
remaining `docs/HANDOVER.md` §8 backlog items 2–6, which are unrelated to this class (item 6 is the byte-ceiling
class this spec itself defers — §3.8).

**Multi-instance / deployment topology** (CLAUDE.md's mandatory statement, even where N/A — round-1 **m-4**): every
ceiling in this spec is a **per-process constant** guarding a **per-process allocation**. No component here holds
state that spans a message's lifetime, crosses an instance boundary, or requires coordination — there is no
correlation, no lease, no consumer group, no dedup store. A horizontally-scaled deployment of N instances multiplies
the per-process cost by N, which is the operator's sizing concern and not something the library can or should
police. No SPI seam is affected, and no future distributed pattern (Return Address, durable lock, consumer groups)
is constrained by these decisions.
