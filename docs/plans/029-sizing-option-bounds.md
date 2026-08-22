# Plan 029 — Sizing options must not panic or fatally exhaust memory

> **For agentic workers:** REQUIRED SUB-SKILL: `superpowers:subagent-driven-development` (the project default) or
> `superpowers:executing-plans`. Steps use checkbox (`- [ ]`) syntax.
>
> **Go-skills hard rule (CLAUDE.md, restated because `writing-plans` omits it):** every task starts from
> **`cc-skills-golang:golang-how-to`** (here routing to: `golang-safety` — the increment is about panics and
> allocation limits — `golang-error-handling`, `golang-design-patterns` (functional options), `golang-testing`,
> `golang-documentation`). **`superpowers:test-driven-development`** governs every task: red → green → refactor.
> **`gopls`** (via the `LSP` tool) for navigation and refactoring — not `grep` — when reasoning about Go symbols.
> Project-local **`table-test`** override applies to every test (assert-closure form, never `want`/`wantErr`;
> `t.Context()`). **`use-mockgen` / `use-testcontainers` do not apply** — every test here is a constructor call or a
> `Run`/`ServeHTTP` invocation with no external resource.
>
> **This plan is deliberately thin** (Plan 024/026/028 precedent): signatures, positions, branch coverage and commit
> boundaries — no embedded implementations. Write the code TDD-first from the tables below.

**Revision 6.** Round 5 ([`029-audit-round-5.md`](029-audit-round-5.md)): **NOT SAFE TO IMPLEMENT** — 2 BLOCKERs,
5 MAJORs, 8 MINORs. Round-4 fix verification was the **cleanest of five rounds**: **11 LANDED · 5
LANDED-BUT-FLAWED · 0 NOT LANDED · 0 REGRESSED**, and the first round in which the ADR received *every* fold-in.

**BLOCKER-1** — `msghttp.WithMaxResponseBytes` is a class member certified *safe (d)*: `drainBounded` is **five of
six** reads; the sixth (`exchange.go:130-131`) is `io.ReadAll(io.LimitReader(resp.Body, max))`, **retained** as the
reply payload (67,108,864 bytes at `1<<62`, measured). It is a **byte** cap ⇒ the **deferred** arm per §"split by
kind". Census: **9 fixed + 3 deferred + 4 safe**. **Safety cause (d) is now empty**, as (c) was in revision 5.

**BLOCKER-2** — revision 5's own `WithReplayBuffer` reclassification **stopped one file short**: seven twins
survived, including **Task 7, the task that writes the gate**, which still filed it under *accepts, safe*.

> **🔴 TWO LESSONS, both now twice-proven, both more important than any single finding.**
>
> 1. **A row's verdict can be stale without any count being wrong.** Round-4 BLOCKER-1 (`WithReplayBuffer`) and
>    round-5 BLOCKER-1 (`WithMaxResponseBytes`) were both rows whose *"safe"* verdict survived the criterion
>    written to catch them — and **neither changed the 16/17 totals**, so no count-check could find either.
>    **Re-derive §2.1 row by row FROM D-AB. Never read the verdict column.**
> 2. **A guard you write but do not run is worth nothing.** Revision 5 *added* the cross-file grep guard, ran it
>    against a list of retracted phrases (where it caught a real survivor) and **never ran it against the
>    classification it had just changed** — which is precisely BLOCKER-2's seven twins, all findable in one
>    command. **Run every guard against the thing you just edited.**
>
> Earlier failure modes still apply: fix the **class**, not the instance; fold into **all three** files; name the
> fixture **and the measurement protocol**; and an AC whose text cannot become a running test is a blocker —
> rounds 1, 2, 3, 4 **and 5** each found exactly one.

**Verification pass pending — this plan is not approved for implementation.**

**Goal.** Deliver [Spec 016](../specs/016-sizing-option-bounds.md): the **9** exported sizing options that reach an
allocation or bound a growing structure stop panicking, corrupting runtime state, or silently unbounding it; the
**3** byte knobs are recorded as class members with a deferred remedy (never as *safe*); and the **4** that are
genuinely safe stop being *accidentally* safe, each under a stated criterion (ADR **D-AB**).

**Architecture.** [ADR 0032](../adrs/0032-sizing-option-bounds.md) — **D-W** (a stated per-knob ceiling, not the
runtime's limit), **D-X** (reuse the existing sentinels, wrap the value into the error), **D-Y** (the range check
`return`s unconditionally, independent of the latch), **D-Z** (the nine ceiling values), **D-AA** (the class gate
is completeness + conformance, every row executable), **D-AB** (membership is a stated criterion; the byte-ceiling
remedy is deferred).

**Tech stack.** Go 1.25 (`GOTOOLCHAIN=go1.25.13`). Touches **one module** — root (packages `endpoint`, `routing`,
`channel`-adjacent, `adapter/http`, `adapter/memory`) — but the delivery gate is all **eight**.

**Traceability.** Implements Spec 016; decided by ADR 0032. Every commit carries `Spec: 016`, `Plan: 029`,
`ADR: 0032` trailers. Branch: `fix/sizing-option-bounds`, off `main`.

---

## Global constraints

1. **Blackbox tests only** — `package <pkg>_test`, exercising the exported API. No whitebox fallback.
2. **Assert-closure tables** — every case carries `assert func(t *testing.T, …)`.
3. **No signature change, and no new exported symbol.** D-X makes the net exported-surface delta **zero**. A task
   that appears to need one has hit a design fault: **stop and escalate**. Task 8 verifies this with an
   exported-surface AST diff, *not* with `apidiff` (Plan 028 proved `apidiff` is blind outside root).
4. **R1 errors are wrapped with the value but NOT with `Permanent`; the R2 error IS `Permanent`-wrapped.**
   **One shape everywhere** — `fmt.Errorf("%w: %s: %d not in [%d, %d]", sentinel, site, n, lo, hi)`. The sentinel
   stays the `errors.Is` target, the site + range are the debuggability payload (D-X), and `Permanent` stays
   absent on the constructor arm (ADR 0029 D-M). **Never render `"exceeds"`**: it is false on the lower arm
   (round-2 **M2-1**), which ships correct today. AC-2b asserts the render **at both ends**; AC-1 asserts the wrap
   direction. A reflex in either direction fails the suite.
5. **Mutation-prove every assertion, with a mutant that targets THAT assertion.** Deleting an upper-bound arm makes
   the constructor *accept* the value and the test then panics — turning every case in the file red regardless of
   what it asserts — so guard-deletion is permitted for **AC-1 alone**. Use each task's mutant table for the rest.
   Record the killed mutant per case in the task's Evidence block. **A case that survives its own mutant is
   rewritten** — a green run is not evidence.
6. **Per-task commit is pre-authorized** by CLAUDE.md's plan-execution exception once this plan is approved and an
   execution mode is chosen. `git push`, merge and branch deletion still need explicit per-action approval.
7. **Each task is a green unit** — `GOWORK=off go test ./... -race -shuffle=on` passes in every module it touched
   before its commit.
8. **Never `git commit --amend` while the controller may be committing** (Plan 028 scar: a subagent's amend landed
   on the controller's commit). Run `git log -1` immediately before any amend.

## The SIX code shapes — read each site, do not pattern-match

> **Revision 2 named three shapes and assigned two of the six R1 knobs to the wrong one** (round-2 **M2-2**) — the
> `m-9` defect returning through the next door. `m-9` said *"the `maxInFlightSet` shape doesn't transfer to
> `WithConcurrency`"*; the fix added R1-b **for `WithConcurrency` only** and left `WithMaxGroups` (also
> `set`-flagless) filed under R1-a, while `WithCapacity` has a **nested** shape no revision had named. The lesson
> is not "there are three shapes" but **"each site has its own shape — read it."** Revision 4 added a **fifth**
> (`WithCompletionSize` has **no config field at all**); revision 5 adds a **sixth** (`WithReplayBuffer`'s
> *flattened gate*). **Every one of the nine sites is pasted below verbatim so no one has to infer** — the count
> of shapes has risen in three consecutive revisions, so assume a seventh exists until you have read the site.

| Shape | Knobs | Sites |
|---|---|---|
| **R1-a** — two-arm, `set` flag, `else if` | `WithMaxInFlight`, `WithMaxConnections`, `WithConnectionBuffer` | `consumer.go:272`, `options.go:1158`, `options.go:1164` |
| **R1-b** — single-arm, **no** `set` flag, default in the config literal | `WithConcurrency`, **`WithMaxGroups`** | `consumer.go:262` (literal `:251`), `groupstore.go:91` (literal **`:84`**) |
| **R1-c** — **nested**, `capacitySet`-gated | `WithCapacity` | `queuestore.go:97-103` (check at `:99`) |
| **R1-d** — **no config field**; the option sets only a closure | **`WithCompletionSize`** | `aggregator.go:132-135`; validated in `NewAggregator` `aggregator.go:318-330` |
| **R1-e** — **flattened gate**, `set &&` in one condition, **no default assignment** | **`WithReplayBuffer`** | `options.go:1174` |
| **R2** — latch + unconditional `return` | `WithBuffer` | `memory.go:38` |

```go
// ── R1-a ── two-arm with a `set` flag. consumer.go:270-274 today:
if !cfg.maxInFlightSet {
	cfg.maxInFlight = defaultMaxInFlight
} else if cfg.maxInFlight < 1 {                      // ← extend THIS condition
	return nil, msgin.ErrInvalidMaxInFlight
}
// becomes:
} else if cfg.maxInFlight < 1 || cfg.maxInFlight > maxInFlightCeiling {
	return nil, fmt.Errorf("%w: %s: %d not in [%d, %d]",
		msgin.ErrInvalidMaxInFlight, "endpoint.WithMaxInFlight", cfg.maxInFlight, 1, maxInFlightCeiling)
}
// options.go:1156-1166 has the same shape TWICE (maxConnections, connectionBuffer), both `<= 0`.

// ── R1-b ── SINGLE arm, no `set` flag, default in the literal. Extend the condition;
// do NOT add an else-if — there is no `if !set` arm to attach it to.
//   consumer.go:251   cfg := consumerConfig[T]{concurrency: 1, …}
//   consumer.go:262   if cfg.concurrency < 1 { return nil, msgin.ErrInvalidConcurrency }
//   groupstore.go:84  cfg := groupStoreConfig{clock: …, maxGroups: 1024}
//   groupstore.go:91  if cfg.maxGroups <= 0 { return nil, msgin.ErrInvalidCapacity }
if cfg.concurrency < 1 || cfg.concurrency > concurrencyCeiling {
	return nil, fmt.Errorf("%w: %s: %d not in [%d, %d]",
		msgin.ErrInvalidConcurrency, "endpoint.WithConcurrency", cfg.concurrency, 1, concurrencyCeiling)
}

// ── R1-c ── NESTED, not an else-if. queuestore.go:97-103 today:
capacity := defaultCapacity
if cfg.capacitySet {
	if cfg.capacity <= 0 {                           // ← extend THIS inner condition
		return nil, msgin.ErrInvalidCapacity
	}
	capacity = cfg.capacity
}

// ── R1-d ── WithCompletionSize. THE OPTION SETS ONLY A CLOSURE — aggregatorConfig
// (aggregator.go:14-21) has NO completionSize field, so there is nothing for
// NewAggregator to inspect. The option must ALSO record n.  Today:
//   func WithCompletionSize(n int) AggregatorOption {
//       return func(c *aggregatorConfig) {
//           c.release = func(g msgin.MessageGroup) (bool, error) { return len(g.Messages()) >= n, nil }
//       }
//   }
// becomes (new UNEXPORTED field + set flag; exported surface unchanged):
func WithCompletionSize(n int) AggregatorOption {
	return func(c *aggregatorConfig) {
		c.completionSize, c.completionSizeSet = n, true
		c.release = func(g msgin.MessageGroup) (bool, error) { return len(g.Messages()) >= n, nil }
	}
}
// ...and NewAggregator gains an arm beside its existing correlate/release/output
// checks (aggregator.go:318-330):
if cfg.completionSizeSet && (cfg.completionSize < 1 || cfg.completionSize > completionSizeCeiling) {
	return nil, fmt.Errorf("%w: %s: %d not in [%d, %d]",
		msgin.ErrInvalidCapacity, "routing.WithCompletionSize", cfg.completionSize, 1, completionSizeCeiling)
}

// ── R1-e ── WithReplayBuffer. A FLATTENED GATE: `set &&` folded into ONE condition,
// and NO default assignment (unset means OFF -- replaySize()==0 disables the ring
// entirely). This is neither R1-a (`if !set {default} else if`) nor R1-c (nested,
// with a default). options.go:1174 today:
if cfg.replayBufferSet && cfg.replayBuffer <= 0 {
	return nil, ErrInvalidReplayBuffer
}
// becomes -- extend the SAME condition; do NOT add an else-if, there is no
// `if !set` arm to attach one to:
if cfg.replayBufferSet && (cfg.replayBuffer < 1 || cfg.replayBuffer > replayBufferCeiling) {
	return nil, fmt.Errorf("%w: %s: %d not in [%d, %d]",
		ErrInvalidReplayBuffer, "msghttp.WithReplayBuffer", cfg.replayBuffer, 1, replayBufferCeiling)
}

// ── R2 ── memory.WithBuffer. The `return` is the guard; the latch only picks the message (D-Y).
// The negative end is folded in (Spec §3.6), so ONE condition covers the whole range.
func WithBuffer(n int) Option {
	return func(b *Broker) {
		if n < 0 || n > maxBufferCeiling {
			if b.err == nil {           // first-fault-wins, same latch as Plan 028
				b.err = fmt.Errorf("%w: %s: %d not in [%d, %d]",
					msgin.Permanent(msgin.ErrInvalidCapacity), "memory.WithBuffer", n, 0, maxBufferCeiling)
			}
			return                      // ← LOAD-BEARING (D-Y). NOT inside the `if b.err == nil`.
		}
		b.ch = make(chan msgin.Message[any], n)
	}
}
```

**One error shape across all six** (D-X, round-2 **M2-1**): `"%w: %s: %d not in [%d, %d]"` — sentinel, site,
value, range. Revision 2's R1 `"%d exceeds %d"` renders **`"…must be >= 1: 0 exceeds 1048576"`** on the lower arm,
which is false, on a branch that ships correct today.

**The `return`'s placement is the whole increment's one subtle line.** Nesting it inside the latch's
`if b.err == nil` compiles, reads naturally, passes every test except AC-3, and panics in production on
`memory.New(nil, WithBuffer(1<<62))`.

## The ceilings (D-Z) — declared per package, unexported

| Constant | Package | Value | Guards |
|---|---|---|---|
| `maxInFlightCeiling` | `endpoint` | `1 << 20` | `WithMaxInFlight` |
| `concurrencyCeiling` | `endpoint` | `1 << 16` | `WithConcurrency` |
| `maxConnBufferCeiling` | `msghttp` | `1 << 16` | `WithConnectionBuffer` |
| `maxConnectionsCeiling` | `msghttp` | `1 << 16` | `WithMaxConnections` |
| `maxBufferCeiling` | `memory` | `1 << 20` | `WithBuffer` |
| `maxCapacityCeiling` | `memory` | `1 << 20` | `WithCapacity` |
| `maxGroupsCeiling` | `memory` | `1 << 20` | `WithMaxGroups` |
| **`completionSizeCeiling`** | `routing` | `1 << 16` | `WithCompletionSize` |
| **`replayBufferCeiling`** | `msghttp` | `1 << 16` | `WithReplayBuffer` |

Each sits beside its package's existing `defaultX` constant, with the godoc stating **the value, the unit, why that
number, and the typed error** (Spec §4). Use Spec §3.4's rationales verbatim — they were corrected in revision 2
and two of them were arithmetically wrong before.

## Branch coverage — the hot-path enumeration (CLAUDE.md test-coverage gate)

| Arm / property | Covering case | Applies to |
|---|---|---|
| `n > ceiling` | `ceiling + 1` → typed error (R1) / latched-and-reported (R2) | all **9** |
| `n <= ceiling` — **allocating knobs** | `ceiling` exactly → accepted **and the hazardous path runs**; the allocation *is* the hazard | `WithMaxInFlight` (48.0 MiB), `WithConnectionBuffer` (1.5 MiB), `WithBuffer` (24.0 MiB) |
| `n <= ceiling` — **`WithConcurrency`** | `ceiling` exactly → accepted **and `Run` spawns**; state the cost: 65,536 goroutines ≈ **128 MiB stack, ~257 MiB under `-race`** | `WithConcurrency` |
| `n <= ceiling` — **growth knobs** | `ceiling` → **construction + acceptance only**; the cap-still-caps property is proven **separately at small `n`** | `WithCapacity`, `WithMaxGroups`, `WithMaxConnections`, **`WithCompletionSize`**, **`WithReplayBuffer`** |
| the cap still caps | small-`n`: `WithMaxGroups(1)` → 2nd `Add` = `ErrOverflowDropped`; **`WithCapacity(1)` + `WithOverflow(OverflowReject)`** → 2nd `Enqueue` = `ErrOverflowDropped` (**HANGS without that fixture** — M3-1); `WithMaxConnections(1)` → 2nd conn = 503; `WithCompletionSize(2)` over 6 msgs → `released=3` (**needs the 5-part fixture**, Task 4); `WithReplayBuffer(8)` → ring holds 8 after 20 `Send`s | the 5 growth knobs |
| lower arm still fires | the pre-existing `< 1` / `<= 0` case | 8 of 9 (already covered — do not delete). **`WithCompletionSize` has NO existing arm** — it gains both bounds at once, so its lower case is NEW |
| the message is true at **both** ends (AC-2b) | assert the site + `[lo, hi]` render at `ceiling+1` **and** at the lower bound. `lo = 1` for all eight R1 knobs; `lo = 0` for `WithBuffer` (Spec §3.1) | 8 R1 (**two** cases each) |
| the wrap is right | `IsPermanent == false` (R1) / `== true` (R2) | 8 R1 / 1 R2 |
| the failure surface is the right one | `WithMaxInFlight`/`WithConcurrency`→`NewConsumer`; `WithConnectionBuffer`/`WithMaxConnections`→`NewConfig`; `WithCapacity`→`NewQueueStore`; `WithMaxGroups`→`NewGroupStore`; **`WithCompletionSize`→`NewAggregator`**; **`WithReplayBuffer`→`NewConfig`**; `WithBuffer`→`Send` **and** `Stream` | all 9 |
| **D-Y's unconditional `return`** | `New(nil, WithBuffer(1<<62))` → no panic, reports `ErrNilFunc` index 0 (AC-3) | `memory` |
| first-**fault**-wins, and the negative end | `New(WithBuffer(1<<62), nil)` → `ErrInvalidCapacity`; `New(WithBuffer(-1))` → same (AC-3b) | `memory` |
| zero-size-element safety | AC-4's two halves, **asserted per site** — see Task 6 | `memory`, `endpoint` |

> **Why the `n <= ceiling` row is split** (round-2 **BLOCKER-3**). Revision 2 demanded *"the hazardous path runs at
> the ceiling"* for **all 7** (now **9**). For `WithMaxConnections` that hazard is the admission check
> `sse_server.go:182 len(s.conns) >= s.cfg.maxConns()`, so running it at `1<<16` needs **65,536 live SSE
> connections** — each a `ServeHTTP` goroutine that blocks until its request context is done, under a package
> whose root sibling runs `goleak.VerifyTestMain`. It cannot be written, and Task 2's own bullets never asked for
> it, so the table and the task text contradicted each other. For a growth knob the hazard is a property of the
> **comparison**, not of the ceiling value — proving it at `n = 1` proves it at `n = 1<<20`, and
> `groupstore_test.go:30-39` already proves it that way today.
>
> **Measured, so the choice is on the record rather than assumed** (`-race`, this tree, **realistic `msgin.New`
> messages**): filling past a `1<<20` cap costs `WithMaxGroups` **853.4 MiB live / 1,042.7 MiB cumulative, 2.7 s**
> and `WithCapacity` **484.1 MiB live / 770.7 MiB cumulative, 1.7 s**. *(Revision 3 printed 283 MiB / 108 MiB
> here and claimed round 2's ~1 GB "did not survive re-derivation" — **that was wrong**; those figures came from a
> **zero-value `Message[any]{}`** fixture. Round 2's number was right. Round-3 **M3-2**; see the header
> retraction.)* The decision is unchanged either way — `WithMaxConnections` is what makes the row unexecutable —
> but at ~1 GB the growth-knob split is *more* clearly right, not less.

**Coverage caution (project scar):** a package split re-attributes blackbox coverage to the package the test lives
in. These tests sit beside their constructors, so no re-attribution is expected — verify with `-coverpkg=./...`.

---

## Task 0 — Establish the baseline, and prove it is red for the right reasons

**Why this task exists:** Plan 028's round-3 audit produced a BLOCKER because a later task gated on a baseline that
was already red on arrival.

- [ ] Reproduce all **9** defects on a clean tree and record verbatim output: the three
      `makechan: size out of range` panics (`Consumer.Run`, `SSEServer.ServeHTTP`, `memory.New`), the three
      growth levers (Spec §1.3 — show the structure exceeding its nominal cap), `WithConcurrency`, and the two
      knobs later revisions added — **`routing.WithCompletionSize`** (Spec §1.4: one group accumulating without
      bound; needs the 5-part fixture of Task 4) and **`msghttp.WithReplayBuffer`** (Spec §1.5: the ring growing
      linearly at `1<<62`, **with no client connected**). Revision 4's version of this bullet said "7" and never
      named either, so Task 0's baseline would have omitted the knobs Tasks 2 and 4 depend on (round-4 **M4-5**).
- [ ] For `WithConcurrency`, assert **both** failure modes (round-2 **BLOCKER-2** — this bullet has now been wrong
      in two revisions, so assert it, do not describe it):
      - **the `Run` panic:** `NewConsumer(WithConcurrency(1<<31))` returns a **nil** error, then `Run` **panics**
        with `sync: negative WaitGroup counter`. Cheap, deterministic, one line.
      - **the `int32` band:** `wg.Add(n)` panics iff `int32(n) < 0`. Include `1<<32`, `1<<40` and `1<<62` as the
        **non**-panicking counter-examples that kill the "above `2^31`" formulation, and print the `int32(n)`
        column so the predicate is visible rather than inferred.
      - **Do NOT assert "it is not a panic"** (revision 1 instructed that) and **do NOT assert the panic is
        latent, on a later `Done()`** (revision 2 instructed that). Both are false; the panic is in `Add` itself.
      - Record the second mode's spawn loop as **timed out, not observed**: `WithConcurrency(1<<40)` + `Run` was
        killed at a 2-minute timeout with the loop still running. Whether it OOMs or eventually returns is unknown
        and does not need to be known — the ceiling forecloses it.
- [ ] Confirm the **4 safe** rows of Spec §2.1 (+ `burst`) by execution — **not by reading this plan**. Record
      `make(chan struct{}, 1<<62)` succeeding, and that `WithSuccessStatus(1<<62)` **rejects** (it belongs in the
      "rejects" arm of the gate, not the allocation-free arm). Note `WithCompletionSize` **accepts** — a bare
      `NewAggregator` call fails on a missing *fixture* (`msgin: channel store is nil`), not on the option.
- [ ] Capture the **exported-surface AST baseline** for all packages (the check Task 7 diffs against). Do **not**
      use `apidiff` as the primary gate — Plan 028 proved it captures only the root package.
- [ ] Confirm 8/8 modules green before any edit.

**Commit:** none (evidence only, recorded in the task report).

## Task 1 — `endpoint`: ceilings on `WithMaxInFlight` and `WithConcurrency`

Both live in `NewConsumer` and the same package, so they are one green unit. **They take different code shapes** —
R1-a and R1-b above; `WithConcurrency` has no `set` flag (round-1 **m-9**).

- [ ] RED: table cases for each ceiling and `+1`, asserting the existing sentinels, `IsPermanent == false`, and
      **AC-2b at BOTH ends** — a `ceiling+1` case *and* a lower-bound case (`0`), each asserting the site name and
      the full `[lo, hi]` render. The lower-end case is what catches the `"0 exceeds 1048576"` regression
      (round-2 **M2-1**). The accepting cases must reach `Run` so the allocation actually happens — a
      constructor-only assertion does not prove the panic is gone.
- [ ] GREEN: the two constants + the two conditions. **They are different shapes** — `WithMaxInFlight` is **R1-a**
      (`else if`, `consumer.go:272`), `WithConcurrency` is **R1-b** (single arm, no `set` flag, `consumer.go:262`).
- [ ] Widen the godoc on both options and genericise `msgin.ErrInvalidMaxInFlight` / `msgin.ErrInvalidConcurrency`
      to `"… out of range"` (D-X, round-2 **m2-4**). Use Spec §3.4's corrected rationales — **not** revision 1's
      "≥ 8 KiB / 512 MiB" and **not** revision 2's "~134–257 MiB", which mixes MB and MiB in one range. The
      measured figure is **~128 MiB of stack, ~257 MiB under `-race`** (2,052 B/goroutine; 4,114 B under `-race`).
- [ ] **Expect a memory spike in CI, and say so in the report** (round-1 **m-8**): at the ceiling under `-race`,
      `WithMaxInFlight(1<<20)` allocates the **48.0 MiB** channel and `WithConcurrency(1<<16)` reaches ~257 MiB of
      goroutine stack. Both pass; this is expected, not a leak.
- [ ] **Mutants:** (a) delete each upper arm → the `+1` case fails; (b) set a ceiling to the default → the
      accepting case fails; (c) swap the two sentinels → the assertions fail; (d) drop the `[lo, hi]` wrap →
      **both** AC-2b cases fail; (e) render `"exceeds"` instead of the range → the **lower-end** AC-2b case fails.

**Commit:** `fix(endpoint): bound WithMaxInFlight and WithConcurrency against allocation overflow`

## Task 2 — `msghttp`: ceilings on `WithConnectionBuffer`, `WithMaxConnections` and `WithReplayBuffer`

All three are validated in `NewConfig`, so one green unit. `WithMaxConnections` is the second factor of
`WithConnectionBuffer`'s rationale and must land with it (round-1 **M-7**). **`WithReplayBuffer` is new in
revision 5** (round-4 **BLOCKER-1**) — it was certified *safe* in four consecutive revisions while being the sole
bound on the replay ring (`appendRing` evicts at `max := s.cfg.replaySize()`, and that **is `n`** — Spec §1.5).

**🔴 `WithReplayBuffer` is shape R1-e, NOT R1-a.** Its site is a *flattened gate* with **no default assignment**
(`options.go:1174`). Extend the same condition; do not add an `else if`. Pattern-matching it onto its two
siblings is round-2 **M2-2** verbatim.

- [ ] RED: each ceiling and `+1` against `NewConfig`, plus AC-2b at **both** ends for each. Then, per the split
      branch-coverage table:
      - **`WithConnectionBuffer` (allocating)** — a **`ServeHTTP` case at the accepted ceiling**, proving the
        per-connection `make` still runs. Cost: 1.5 MiB for the one connection.
      - **`WithMaxConnections` (growth)** — at the ceiling assert **construction + acceptance only**. Do **not**
        attempt to run the admission check at `1<<16`: that needs 65,536 live SSE connections (round-2
        **BLOCKER-3**). Prove the cap still caps with **`WithMaxConnections(1)`** — the second connection gets
        `503 SSE connection limit reached` (`sse_server.go:182-184`).
      - **`WithReplayBuffer` (growth)** — at the ceiling assert **construction + acceptance only**. Prove the cap
        still caps with **`WithReplayBuffer(8)`** — but **NOT by asserting "the ring holds 8": that is
        unobservable blackbox** (round-5 **M5-3**, the *fifth* consecutive unexecutable AC). `*SSEServer` exports
        only `ServeHTTP`, `Close` and `Send`; there is no ring accessor. **Assert through a `Last-Event-ID`
        replay request instead** — measured: 20 `Send`s at `WithReplayBuffer(8)`, resume from entry 13 → **7**
        frames; resume from the **evicted** entry 1 → **0** frames.
        🔴 **This needs a connection carrying that header, and `serveInBackground`
        (`adapter/http/sse_server_test.go:180`) HARD-CODES its request** — Task 2 must write its own helper.
        Revision 5's claim that this case *"needs no connection at all"* is **false**. *(The ring does grow with
        no client connected — `sse_server.go:429-431` precedes the fan-out loop — but growing is not the thing
        under test; **eviction** is, and only a replay request observes it.)*
      - **Re-measure the per-event cost under AC-4's stated protocol — `TotalAlloc`, NOT `HeapAlloc`** (round-5
        **M5-2**: the two differ by **3.3×**, 78.4 vs 23.5 MiB, on this fixture; AC-4 mandates `TotalAlloc`)
        and put the figure in `WithReplayBuffer`'s godoc. Round 4 measured ~1.2 KiB/event **at its own frame
        size**; the real cost is `n × the caller's frame`, so do **not** copy that ratio (round-4 **M4-4**).
- [ ] **Use the repo's existing `serveInBackground` helper** (`adapter/http/sse_server_test.go:180` — note: **:180**,
      not `:176` as round 2 cited). A naive `s.ServeHTTP(rec, httptest.NewRequest(...))` **hangs** — measured,
      `BLOCKED for 3s` — because the SSE handler streams until the request context is done, and it also leaks a
      goroutine under `goleak` (round-1 **m-2**).
- [ ] GREEN: **three** constants + three upper arms — `options.go:1158` and `:1164` (**R1-a**), `:1174` (**R1-e**).
- [ ] Widen the godoc on all three options and all three sentinels (`ErrInvalidReplayBuffer` becomes the sixth
      genericised message — Spec §3.5). `WithConnectionBuffer`'s must state the cost is **per
      connection**; the process total is that × `WithMaxConnections`, and **both factors are now bounded**.
- [ ] **Mutants:** delete each arm → the `+1` case fails; move the connBuffer check after `NewSSEServer` returns →
      the `ServeHTTP` case still panics; **drop the `cfg.replayBufferSet &&` conjunct from R1-e → the
      UNSET case (replay OFF, `replayBuffer == 0`) is now rejected by the new `< 1` arm, so a config that sets no
      replay buffer fails to construct** (round-5 **m5-4**: revision 5's *"convert to an `else if`"* is not a
      mechanically-defined mutation — R1-e has no preceding `if` to attach an `else` to and no default assignment —
      and it did not target R1-e's actual risk, which is exactly this `set`-guard).

**Commit:** `fix(http): bound WithConnectionBuffer, WithMaxConnections and WithReplayBuffer`

## Task 3 — `memory`: the two growth levers · touches **root + `adapter/memory`**

`WithCapacity` and `WithMaxGroups` both return `msgin.ErrInvalidCapacity`, but they are **NOT the same shape**
(round-2 **M2-2**): `WithCapacity` is **R1-c** (nested, `capacitySet`-gated, `queuestore.go:97-103`) and
`WithMaxGroups` is **R1-b** (single arm, no `set` flag, `groupstore.go:91`). Doing them **before** Task 5 keeps the
delicate `WithBuffer` work isolated.

**Module scope** (round-2 **m2-5**): `msgin.ErrInvalidCapacity`'s message lives in **root**'s `errors.go:258`, so
this task edits two modules. Global constraint 7 requires **both** green before the commit, despite the
`fix(memory)` subject.

- [ ] RED: each ceiling and `+1`; **AC-2b at both ends**; `IsPermanent == false`.
- [ ] RED (the cap-still-caps property, **at small `n`** — round-2 **BLOCKER-3**). This is the whole defect — the
      cap stopping capping — and it is a property of the comparison, not of the ceiling value. At the **ceiling**,
      assert construction + acceptance only.
      - `memory.NewGroupStore(memory.WithMaxGroups(1))` → the **2nd** `Add` returns `msgin.ErrOverflowDropped`.
        `groupstore_test.go:30-39` already proves it in this shape.
      - **`memory.NewQueueStore(memory.WithCapacity(1), memory.WithOverflow(msgin.OverflowReject))`** → the
        **2nd** `Enqueue` returns `msgin.ErrOverflowDropped`.
      - > **🔴 The `WithOverflow(OverflowReject)` fixture is MANDATORY here, not optional — WITHOUT IT THIS CASE
        > HANGS** (round-3 **M3-1**). `QueueStore`'s default policy is `OverflowBlock`, so the 2nd `Enqueue`
        > blocks on `s.sem <- struct{}{}` (`queuestore.go:124-129`) until the test binary's 10-minute panic.
        > Revision 3 named the fixture only in the *next* bullet, scoped to a ceiling-sized exercise that
        > BLOCKER-3's split had already removed. **The `groupstore_test.go` precedent does NOT transfer:
        > `GroupStore.Add` has no overflow policy at all.** Same class as `m-2`/`M2-5`, third instance.
- [ ] **A ceiling-sized growth exercise is NOT required (BLOCKER-3's split). If you write one anyway it HANGS
      unless you pass `memory.WithOverflow(msgin.OverflowReject)`** — `QueueStore` defaults to `OverflowBlock`, so
      enqueue #1,048,577 blocks on `s.sem <- struct{}{}` (`queuestore.go:124-129`) until the binary's 10-minute
      panic (round-2 **M2-5**). **And know its real cost — the figure depends entirely on the FIXTURE** (round-3
      **M3-2**). Measured under `-race`, filling past a `1<<20` cap:

      | Fixture | `WithMaxGroups(1<<20)` | `WithCapacity(1<<20)` |
      |---|---|---|
      | **realistic `msgin.New` message** | **1,042.7 MiB cumulative / 853.4 MiB live**, 2.7 s | 770.7 MiB cumulative / 484.1 MiB live, 1.7 s |
      | zero-value `msgin.Message[any]{}` | 362.7 MiB cumulative / 221.3 MiB live, 2.3 s | 298.7 MiB cumulative / 60.1 MiB live, 0.45 s |

      **Revision 3 quoted the zero-value row and declared round 2's realistic figure wrong. It was not.** Use the
      realistic row — it is what the exercise would actually enqueue. `go test ./...` runs these packages
      **concurrently** with Task 1's 48 MiB and Task 6's 48 MiB.
- [ ] GREEN: the two constants + the two conditions, **each in its own shape**.
- [ ] `msgin.ErrInvalidCapacity`'s own message becomes **generic** (`"msgin: capacity out of range"`) — it cannot
      state one range (D-X, round-1 **M-1**). **Write the godoc for the END STATE of four producers**
      (`NewQueueStore`, `NewGroupStore`, `NewAggregator` in Task 4, `WithBuffer` in Task 5) and cross-reference
      those tasks — do **not** write "three" here. At the end of *this* task there are only **two**; following
      revision 4's text literally shipped a godoc that was wrong twice in a row (round-4 **m4-6**).
- [ ] **Mutants:** delete each arm → the `+1` case fails; convert R1-c's nested `if` to an `else if` → the
      `capacitySet == false` default path breaks; delete the generic-message change → nothing should fail, which
      is the signal that the message change needs its own assertion (add one).

**Commit:** `fix(memory): bound WithCapacity and WithMaxGroups so the cap keeps capping`

## Task 4 — `routing`: `WithCompletionSize` — the knob with no field to validate

**New in revision 4** (round-3 **BLOCKER-1**). Certified *"safe — comparison only"* through revision 3; it is the
§1.3 shape exactly, and nothing else bounds it (`WithMaxGroups` caps *groups*, not members; the reaper never
sweeps without `WithGroupTimeout` — Spec §1.4).

**This is the only task that changes a struct**, and the only knob with **no existing lower-bound arm** — it gains
both bounds at once. Shape **R1-d**: the option sets only a closure, so it must also record `n` in a **new
unexported** `aggregatorConfig` field (+ a `set` flag) that `NewAggregator` validates. **Exported surface is
unchanged** — Global constraint 3 holds and `apidiff` still reports 0/0.

- [ ] RED: `completionSizeCeiling` (`1 << 16`) and `+1`; **the NEW lower case** (`0`); **AC-2b at both ends**
      asserting the site `routing.WithCompletionSize` and the `[1, 65536]` render; `IsPermanent == false`.
- [ ] RED (cap-still-caps, **at small `n`**): `WithCompletionSize(2)` over 6 messages → **`released=3`**. It is a
      **growth** knob, so at the ceiling assert **construction + acceptance only** — see the cost bullet.
- [ ] **🔴 THE FIXTURE HAS FIVE PARTS, NOT TWO — as written in revision 4 this case does not construct**
      (round-4 **M4-3**, the *fourth* instance of "an AC whose text cannot become a running test"). Measured, the
      gates fire in this order:

      ```
      NewAggregator(store, fn)                     err=msgin: aggregator output channel is nil
      NewAggregator(store, fn, WithOutputChannel)  err=<nil>
        + WithCompletionSize(1<<62)                err=<nil>
      default correlate, Handle(no corr header)    err=msgin: permanent: msgin: message has no correlation key
      ```

      Use **`NewAggregator(store, fn, WithOutputChannel(ch), WithCorrelationStrategy(fixedKey),
      WithCompletionSize(n))` plus `ch.Subscribe(counter)`** — the subscriber is required for `released` to be
      *observable* at all.
      > **A prior "correction" is itself retracted.** Revision 3 called round 2's
      > `msgin: aggregator output channel is nil` **wrong**, and revision 4 repeated it. **It is not wrong — it is
      > the NEXT gate**, reached once `store` and `fn` are supplied. Both strings are real and consecutive.
      > Calling one "wrong" hid the missing fixture instead of exposing it.
- [ ] GREEN: the constant, the two config fields, the option write, and the `NewAggregator` arm beside the
      existing `correlate`/`release`/`output` checks (`aggregator.go:318-330`).
- [ ] **DO NOT run the hazardous path at the ceiling.** Measured this revision (one key, realistic `msgin.New`
      messages — **the fixture matters**, see the header retraction): `memory.GroupStore.Add` clones the group
      snapshot per call, so cost is **quadratic in time**, not linear in bytes.

      | members | elapsed | cumulative alloc | live |
      |---|---|---|---|
      | members | elapsed | cumulative alloc | live (GC'd) |
      |---|---|---|---|
      | `1<<12` | 50 ms | 206.7 MiB | 2.0 MiB |
      | `1<<14` | 644 ms | 3,143.5 MiB | 7.8 MiB |
      | 60,000 | — | 41,474.0 MiB | 28.7 MiB |
      | `1<<16` | **8.6 s** | **48.3 GiB** | **31.0 MiB** |

      **Protocol, not just fixture** (round-5 **M5-1/M5-2**): `runtime.GC()` before both reads, `HeapAlloc` delta
      for *live*, `TotalAlloc` for *cumulative*, `runtime.KeepAlive`. Revision 5's live column (3.2 / 13.8 / 67.8)
      was read **without a GC**; it was retracted in Spec §1.4 and left standing here and in D-Z.

      A 400,000-member probe **did not finish inside a 4m20s timeout**. That churn is *why* the ceiling is where it
      is — and why the growth-knob split (BLOCKER-3) is load-bearing rather than a convenience.
- [ ] `msgin.ErrInvalidCapacity` now has a **FOURTH** producer (Spec §3.5). Its message is already generic from
      Task 3; widen its godoc to name all four.
- [ ] Godoc on `WithCompletionSize`: the range, the ceiling, why (the quadratic clone), and the typed error.
- [ ] **Pin the inert-`n` case** (round-4 **m4-9**): `WithReleaseStrategy`/`WithReleaseWhen` overwrite `c.release`,
      so `WithCompletionSize(1<<62)` followed by either leaves the huge `n` with **no effect** — accepted today,
      **rejected after this task**. That is deliberate (fail loud on a nonsense value), but it is a **new
      construction-time rejection of a currently-legal config**, so add a table case and one godoc sentence: the
      bound is validated on the **value passed**, whether or not a later option replaces the release strategy.
- [ ] **Mutants:** (a) delete the arm → the `+1` case fails; (b) drop the `set` flag so an unset aggregator is
      validated → the **no-option** construction case fails; (c) record `n` but never read it → the `+1` case
      fails; (d) swap `1` and `completionSizeCeiling` in the render → **both** AC-2b cases fail.

**Commit:** `fix(routing): bound WithCompletionSize so a group cannot grow without limit`

## Task 5 — `memory`: `WithBuffer`, D-Y's unconditional `return`, and the negative end

**The highest-risk task in this plan.** Read ADR 0032 **D-Y** before writing anything.

- [ ] RED: `maxBufferCeiling`/`+1`; `WithBuffer(-1)` (Spec §3.6, folded in); the fault reported by **both** `Send`
      and `Stream`; `IsPermanent == true`; **AC-3** (`New(nil, WithBuffer(1<<62))` → no panic, reports
      `ErrNilFunc` index 0) and **AC-3b** (`New(WithBuffer(1<<62), nil)` → `ErrInvalidCapacity`).
- [ ] GREEN: the constant, the one-condition range check, and the `return` **outside** the latch's `if`.
- [ ] **Retire the godoc sentence that this task makes FALSE** (round-2 **m2-7**). `memory.go:35-37` currently
      says *"A negative `n` is **clamped to 0** rather than panicking, honoring the library's
      no-panic-on-caller-input contract."* §3.6 folds the negative end into `ErrInvalidCapacity`, so **delete that
      sentence** and replace it with the stated range `[0, maxBufferCeiling]` + the R2 disclosure (the fault
      surfaces at `Send`/`Stream`, not at `New`, and why). AC-3b's `WithBuffer(-1)` case is the test that keeps
      this godoc edit honest — cross-reference it so the two do not drift apart. *(CLAUDE.md's stored lesson:
      all three fix rounds in Plan 028 were godoc, not logic.)*
- [ ] **Widen `memory.go:16-23`** — note **:16**, not `:17`; the comment starts one line earlier than revision 2
      cited. The answer here is **determinate, so just do it** (round-2 **m2-6**): the comment scopes the latch to
      *"the **first nil ELEMENT of opts**"*, and after this task the same field also latches a **sizing** fault —
      so widen it to name **both** latch sources. The `Option`-escape sentence needs **no** change: it already
      says *"a hostile option could stash `b` and read `err` concurrently with `New`'s write"*, and `WithBuffer`
      already writes `b.ch` on a live `*Broker` today — so this task escalates the *consequence* without creating
      a new race class. Round 1's carried-forward item is **answered, not open**.
- [ ] **Mutants, and AC-3's is mandatory:** (a) move the `return` inside `if b.err == nil` → AC-3 must fail;
      (b) drop the `if b.err == nil` → AC-3 reports the wrong sentinel; (c) delete the range check → AC-2 panics;
      (d) drop `n < 0` → the negative case fails.
      **If mutant (a) does not turn AC-3 red, AC-3 is wrong — rewrite it before proceeding.**

**Commit:** `fix(memory): bound WithBuffer, guarding the allocation independently of the latch`

## Task 6 — Pin the zero-size-element property (AC-4)

> **Revision 1's AC-4 was unexecutable after Task 1** (round-1 **BLOCKER-1**): it proposed a consumer whose credit
> gate is `1<<62`, but the gate's size **is** `maxInFlight` (`consumer.go:385`), which Task 1 now caps at `1<<20`.
> `credit.go:21` would have shipped with zero coverage. Both halves below are executable **after** Tasks 1–5.

- [ ] **Direct half:** assert `make(chan struct{}, 1<<62)` succeeds (`cap == 4611686018427387904`), naming the
      invariant `credit.go:21` and `queuestore.go:108` rest on.
- [ ] **In-situ half — assert the allocation delta PER SITE; one bound does not fit both** (round-2 **M2-3**).
      Revision 2 justified this with *"`1<<20` of a zero-byte element is nothing, of a 48-byte element is 48 MiB"*
      — true for the `QueueStore`, **false for the consumer**, whose baseline at the ceiling is *already* 48 MiB
      from `workerCh` (`consumer.go:384`). Measured, 3 runs each, `TotalAlloc`, noise floor exactly `0` B:

      | Site | Baseline | Assertion |
      |---|---|---|
      | `NewQueueStore(WithCapacity(1<<20))` | **288 B** | **`delta < 1 MiB`** (3,600× above noise, 48× below the mutant) |
      | `NewConsumer(WithMaxInFlight(1<<20))` + `Run` | **48.0 MiB** | **`40 MiB < delta < 64 MiB`** — the **lower** bound is mandatory, or a measurement taken before `Run` allocates passes vacuously |

- [ ] **Three measurement conditions, or it silently rots** (round-2 **m2-8**): **no `t.Parallel()`**;
      `runtime.KeepAlive` the product (else the ceiling channel is collectible before `ReadMemStats`); read
      **`TotalAlloc`**, not `HeapAlloc` (cumulative, GC-independent). Written this way it is **not** flaky —
      reproduced identically across repeats, plain and under `-race`.
- [ ] Add the one-line comment at each structural-safety site (Spec §4).
- [ ] **Two mutants — and `credit.go:21` is the one that has been missed twice:**
      (a) `queuestore.go:108` `chan struct{}` → **`chan msgin.Message[any]`** (24 B ⇒ `1<<20 × 24` =
      **25,165,824 B**, i.e. **24×** the `delta < 1 MiB` bound — spec and plan now state the same mutant and the
      same arithmetic; round-3 **m3-3**) → the `QueueStore` arm fails on the delta;
      (b) **`credit.go:21` `chan struct{}` → `chan managedDelivery` → the consumer arm must fail.** Revision 2's
      Task 5 named only (a), leaving unmutated — for a second revision running — **the exact site round-1
      BLOCKER-1 was entirely about**. **This is the entire point of the task; if either does not fire, that half
      of the test is decorative.**
- [ ] Task cost, stated (round-2 **M2-5**): the in-situ half allocates ~48 MiB, concurrent with Tasks 1 and 3.

**Commit:** `test(core): pin the zero-size-element property the safe sizing knobs rest on`

## Task 7 — The class gate (AC-5, AC-6)

Modeled on the shipped `option_guard_gate_test.go` — same root-blackbox placement, same `os.Getwd()` walk reaching
all 8 modules without `go.work`, stdlib + testify only.

- [ ] **Half 1 (completeness, AST):** collect every exported **function** (`Recv == nil`) with an `int`/`int64`
      parameter **in any position** across all 8 modules (**not** just `func With…` with it first — round-1
      **M-5**); fail if the set differs from the conformance table's keys **in either direction**.
      **The set is 17, not 16** (round-2 **BLOCKER-1**): the 16 `With…` options **+ `NewTokenBucket`'s `burst`**.
      Revision 2 printed 16 in four places under a 17-row table, so half 1 would have failed on its first run.
      **Re-derive the number before trusting it** — the `go/ast` scan is the source of truth, not this plan.
- [ ] **State the `Recv == nil` boundary in the file header** (Spec §2.0). It is a decision, not an omission:
      *any* `FuncDecl` yields 44 keys, 22 on unexported receivers a root blackbox test cannot construct.
      **Derive the excluded class members from D-AB's criterion — do NOT copy a count.** *"The parameter itself
      sizes a `make`"* yields exactly **two** (`memory.QueueStore.Claim`, `channel.QueueChannel.Poll`), **both
      constructible from root**, so there is no uncovered residue (round-3 **M3-4**). The `sql` rows are **not**
      members: their `make` capacity is `len(rows)`/`len(cands)`, sized by what the DB returned. *(Round 2 said
      three; revision 3 said four and carried two as "named but uncovered". The rule says two.)*
- [ ] **Half 2 (conformance, behavioral) — 17 AST rows + 2 manual rows, none a declaration string** (round-1
      **M-4**), in **three arms matching Spec §2.1's three verdicts**:
      - **class member, fixed here (9)** — asserts the fault is **reported through the surface §3 names for it**:
        the constructor's return, **or the first use of the object it produced**. *(Phrased over the SURFACE, not
        "rejects `1<<62`" — that shape **cannot be written for `WithBuffer`**, which returns no error; round-3
        **m3-5**.)*
      - **class member, ceiling deferred (2)** — `WithMaxBodyBytes`, `WithMaxEventBytes` **accept** `1<<62`, and
        each row is annotated *"class member, remedy deferred — Spec §3.8"* so it never reads as a safety
        certificate. **This annotation is the whole point of BLOCKER-1** — without it the gate certifies an
        unbounded remote-driven read as conformant.
      - **safe (4 + `burst`)** — accepts `1<<62` and its product is usable.

      `WithSuccessStatus` is in the **rejects** arm; **`WithCompletionSize` is now a class member (Task 4)**, so
      after this increment it **rejects** too. The 2 manual rows are `memory.QueueStore.Claim` and
      `channel.QueueChannel.Poll`; because `Poll` delegates into `Claim`, one exercised chain covers both.
- [ ] **Size the fixtures against THIS increment's arms — there are SEVEN `msghttp` keys, not six, and the
      increment dissolves most of the need** (round-3 **M3-3**; revision 3 copied round 2's "six" forward without
      re-deriving it). The seven: `WithConnectionBuffer:883`, `WithMaxBodyBytes:426`, `WithMaxConnections:865`,
      `WithMaxEventBytes:819`, `WithMaxResponseBytes:730`, `WithReplayBuffer:926`, `WithSuccessStatus:566`.

      | Arm after this increment | Keys | Fixture |
      |---|---|---|
      | rejects | `WithConnectionBuffer`, `WithMaxConnections`, **`WithReplayBuffer`** (all newly), `WithSuccessStatus` | `NewConfig` only |
      | accepts, deferred | `WithMaxBodyBytes`, `WithMaxEventBytes`, **`WithMaxResponseBytes`** | `NewConfig` only |
      | accepts, safe | *(none — both moved: `WithReplayBuffer` → rejects (rev 5), `WithMaxResponseBytes` → deferred (rev 6))* | — |

      So **at most ONE** row needs a **root-local** equivalent of `serveInBackground`
      (`adapter/http/sse_server_test.go:180`, unexported and not importable) with explicit teardown under root's
      `goleak.VerifyTestMain` (`main_test.go:15`).
      **Define *"its product is usable"***, which round 2 flagged as unsized and revision 3 left undefined: for a
      `NewConfig`-only key it is *"`NewConfig` returns a non-nil `*Config` and a nil error, and the accessor for
      that knob returns the value set"* — which, after revisions 5–6, is what **all seven** `msghttp` rows need.
      **No AC-5 row requires a live `SSEServer`.**
      - **`WithCompletionSize`** needs `NewAggregator(store, fn, opts…)` — a `msgin.MessageGroupStore` **and an
        aggregation `fn`**, both positional. *(Round 2 called the second one a "release func"; `release` is an
        **option**, defaulted in the config literal. And the bare call's error is `msgin: channel store is nil`,
        not round 2's `msgin: aggregator output channel is nil` — do not paste that string into an assertion.)*
- [ ] Document the accepted limitations in the file header (ADR 0032 D-AA): the root-module import boundary, the
      `Recv == nil` boundary (which under D-AB excludes **two** members, **both covered** — no residue), and that
      `time.Duration` knobs are **outside the gate by construction**. **Do not claim `time.Duration` is
      "checked"** — the five `NewTicker`/`NewTimer` sites are guarded, but the `clock.After(d)` class is a further
      five sites and was never audited (round-3 **m3-2**).
- [ ] **AC-6 vacuity probes, planted in `adapter/http`, NOT in root.** Plan 028's `apidiff` blindness survived
      Task 0 because its probe was planted in root — proving the gate *fires* is not proving it *covers*. Plant
      (a) a sizing parameter missing from the table → half 1 reports it; (b) a table row whose behavioral claim is
      false → half 2 reports it. Revert both; show the hits appearing and disappearing.

**Commit:** `test(core): gate the sizing-option class`

## Task 8 — Whole-branch delivery gate

- [ ] **`/simplify`** over the branch diff (CLAUDE.md Development workflow §4 — round-1 **m-5**), before the
      reviews.
- [ ] `/code-review` over `main..HEAD` — resolve or triage with written rationale.
- [ ] `/security-review` over `main..HEAD` — same.
- [ ] **8 modules × 8 CI steps** (build, vet, gofmt, `CGO_ENABLED=0`, `go mod tidy` + no diff, `govulncheck`,
      `golangci-lint`, `go test ./... -race -shuffle=on`). `harness` has no test files — check it with `go vet`.
      `dbtest`/`crontest` need Docker actually running.
- [ ] Workspace coherence: 8/8 build with `GOWORK` unset.
- [ ] **Coverage — re-derive the baseline at Task 0; the "93.9%" figure carried through revision 5 is WRONG**
      (round-5 **M5-5**). It is `adapter/database/sql`'s **plain** figure, for a package this increment does not
      touch, and `-coverpkg=./...` yields **1.4–42.4%** per package because it counts every package's statements
      against each test binary. Measured **plain per-package** baselines for the four packages this increment
      changes:

      | package | baseline | note |
      |---|---|---|
      | `endpoint` | 99.5% | Tasks 1, 6 |
      | `routing` | 100.0% | Task 4 |
      | `adapter/http` | 100.0% | Task 2 |
      | **`adapter/memory`** | **73.3%** | Tasks 3, 5 — **already below CLAUDE.md's 85% target before this increment** |

      Gate on **per-package plain coverage not regressing**, and treat `adapter/memory`'s 73.3% as a pre-existing
      condition to be **stated**, not silently inherited or silently "fixed". Every new branch still needs a
      covering case (the branch table above).
- [ ] **Measure and record peak RSS for the whole `go test ./...` run** (round 2, *carried into round 3*). Go runs
      packages in parallel, so the increment's heavy cases **sum** rather than max: Task 1 ≈ 48 MiB + ~257 MiB of
      goroutine stack, Task 3 ≈ 108 MiB if a ceiling-sized growth case is written, Task 6 ≈ 48 MiB. Record the
      number here rather than discovering it in CI. *(Adopting BLOCKER-3's split keeps Task 3's ceiling case out of
      the sum entirely.)*
- [ ] **Exported-surface AST diff, `main` vs `HEAD`, ALL packages** — must be identical (constraint 3). `apidiff`
      on root is a supplementary 0/0, not the gate.
- [ ] Docs-link gate, both arms, over every tracked `.md`; arm 2 vacuity-proved by planting a bad anchor.
- [ ] Update `docs/HANDOVER.md`: record the delivery, and **strike backlog item 1** while leaving items 2–6.
- [ ] Update **CLAUDE.md**'s "Project status" counts — **re-derive them, do not increment the printed numbers**:
      `ls docs/specs/[0-9]*.md | wc -l`, `ls docs/adrs/[0-9]*.md | wc -l`, and for plans the **distinct-number**
      command already written there. This bundle moves specs 15→16 and ADR *files* 30→31 (numbers 0001–0032 with
      **no 0024**), and plans 28→29 distinct — while the plan *file* count rises by more, because the audit-round
      records are satellites — **re-derive the count, do not copy this parenthesis**, which has already been stale once (round-5 **m5-5**).

**Commit:** the final increment commit, carrying the plan/ADR/spec updates (CLAUDE.md's couple-with-code rule).

---

## Out of scope — do not fold in without a spec revision

1. *(removed in revision 6)* — `WithReplayBuffer`'s retention **is in scope**; it became the 9th defective knob
   in revision 5 (Spec §1.5, Task 2). This entry was one of seven twins that survived that change (round-5
   **BLOCKER-2**).
2. **`time.Duration` knobs** (Spec §3.7.4) — outside the gate by construction, boundary stated deliberately in
   D-AA. The suspected `NewTokenBucket` sub-normal-`rps` duration overflow belongs to its own increment.
3. **`docs/HANDOVER.md` §8 items 2–6** — the seven duplicated pre-check loops, the Plan 028 AST gate's dominance
   limitation, the `gin` increment, and the godoc wording class. Item 5 is tempting to fold in because this
   increment edits godoc anyway; **don't** — it would mix an unrelated doc sweep into a bugfix diff and its review.
