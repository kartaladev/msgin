# Plan 029 — Adversarial design audit, round 4

**What was audited.** The complete **revision-4** design bundle, together, as CLAUDE.md's design-time gate
requires (spec + ADR + plan in one pass, before any implementation code):

- [`docs/specs/016-sizing-option-bounds.md`](../specs/016-sizing-option-bounds.md) (revision 4)
- [`docs/adrs/0032-sizing-option-bounds.md`](../adrs/0032-sizing-option-bounds.md) (revision 4)
- [`docs/plans/029-sizing-option-bounds.md`](029-sizing-option-bounds.md) (revision 4)

Prior records read and treated as **immutable history, not authority**:
[round 1](029-audit-round-1.md), [round 2](029-audit-round-2.md), [round 3](029-audit-round-3.md).

**Tree state.** `48bbe83` (`Merge branch 'feat/nil-option-elements'`), darwin/arm64, `GOTOOLCHAIN=go1.25.13`.
No implementation code exists for this increment.

**Method.** Every figure below was produced first-hand by running a command, from a throwaway module **outside**
the repository (`/private/tmp/…/scratchpad/p2`, `replace github.com/kartaladev/msgin => …/msgin`) plus one
`go/ast` scanner in a second throwaway module. **Every figure names its fixture**, and where a figure is
fixture- or protocol-sensitive both variants were measured. No repository file was modified except this report.
See the method note at the foot for what was re-derived versus spot-checked.

---

**VERDICT: NOT SAFE TO IMPLEMENT** — **1 BLOCKER, 5 MAJORs, 10 MINORs.**
Round-3 fix verification: **6 LANDED, 5 LANDED-BUT-FLAWED, 1 NOT LANDED, 0 REGRESSED** (of 12).

The single BLOCKER is the project's named failure mode for the **fourth** consecutive round: a partition
asserted "with no residual" that has one more member, reachable through the next door. This time the door is
`msghttp.WithReplayBuffer`, which round 3 explicitly **carried forward as suspected-not-proven** and revision 4
neither adjudicated nor moved. It is now proven, by measurement, using **revision 4's own criterion D-AB**.

---

## What revision 4 got right

This is a materially better bundle than revision 3, and two of its headline claims are exactly correct.

1. **The retraction is right, and it reproduces to the tenth of a MiB.** Revision 3 asserted in three document
   headers that round 2's `WithMaxGroups` growth figure "did not survive re-derivation". Revision 4 retracts
   that. I re-ran both fixtures under `-race` and **every one of the eight numbers in Plan Task 3's fixture
   table reproduces exactly**:

   ```
   === -race build: true ===
   WithMaxGroups(1<<20) realistic msgin.New cumulative=  1042.7 MiB  LIVE=   853.4 MiB  elapsed=2.876s
   WithMaxGroups(1<<20) zero-value Message  cumulative=   362.7 MiB  LIVE=   221.3 MiB  elapsed=1.287s
   WithCapacity(1<<20)  realistic msgin.New cumulative=   770.7 MiB  LIVE=   484.1 MiB  elapsed=1.8s
   WithCapacity(1<<20)  zero-value Message  cumulative=   298.7 MiB  LIVE=    60.1 MiB  elapsed=473ms
   ```

   The "wrong fixture" thesis is not a rationalisation — it is a measured fact, and the fixture-sensitivity is
   a factor of **2.9× (cumulative) / 3.9× (live)**. Naming the fixture beside every figure is the right
   structural response.

2. **D-AB is the right shape of fix.** Replacing verdict strings with a stated criterion is exactly what the
   three prior rounds' evidence demanded, and the criterion *works*: applied to the 27 exported methods it
   yields a reproducible **two** (verified below), retiring a count that had read 3, then 4, then 4-with-two-
   uncovered. The observation that "the *same* verdict string was true for one row and false for its sibling"
   is a genuinely good root-cause diagnosis. My BLOCKER-1 below is not an argument against D-AB — **it is
   D-AB applied to a row revision 4 did not re-run it against.**

3. **The `WithCompletionSize` census addition and its R1-d shape are correct and well-evidenced.** The site
   citations (`aggregator.go:14`, `:132`, `:134`, `:320-330`, `:505`, `:526`), the "no config field to
   validate" diagnosis, the `msgin: channel store is nil` correction, the quadratic-churn rationale, and the
   `1 << 16` ceiling all verify. So does the decision to keep the byte knobs out of the *safe* arm.

4. **Citation hygiene is now genuinely good.** I resolved **37** `file:line` citations across the three
   documents; **35 resolve exactly**, and the two that do not are both round-3 findings landed in only one of
   the three files (m3-1 residue, MINORs 2 and 3 below).

---

## BLOCKER-1 — `msghttp.WithReplayBuffer` is a class member under D-AB's own criterion, and is certified *safe (c)*; AC-5's safe arm would ship a test asserting an unbounded retention lever is conformant

### The claim

Spec §2.1 files `WithReplayBuffer` as **safe, cause (c)** — *"the structure **evicts** at the cap, for any `n`
… retention is bounded by the ring, not by trusting `n`"* — and adds a note: *"A caller who sets it to `1<<40`
has asked for that much retention and will get it, one entry at a time. That is a resource decision under an
explicit bound, not an absent bound."*

D-AB's criterion is: **a knob is a class member iff `n` is the sole bound on an accumulation.** For
`WithReplayBuffer`, `n` **is** the sole bound on `SSEServer.ring`, and cause (c) is self-refuting as written:
*"retention is bounded by the ring"* — the ring's size **is** `n`. That is precisely *trusting `n`*.

### Evidence

The ring is appended on **every** `SSEServer.Send` with a non-empty event id, **independently of whether any
connection is registered** (`sse_server.go:429-431`), and `appendRing` evicts only once `len(s.ring) >= max`
(`sse_server.go:470-476`). `NewConfig` accepts `1<<62` — only `<= 0` is rejected (`options.go:1174`):

```
NewConfig(WithReplayBuffer(1<<62))          -> cfg=true  err=<nil>
```

Measured retention. **Fixture: `msgin.New[any](strings.Repeat("x",1024))`, auto id header, no registered
connections, `HeapAlloc` delta after `runtime.GC()`, `runtime.KeepAlive(s)`:**

```
WithReplayBuffer(8)          n=8                      sends=20000  live heap delta=17384 B (0.0 MiB)
WithReplayBuffer(1<<62)      n=4611686018427387904    sends=20000  live heap delta=24615928 B (23.5 MiB)
WithReplayBuffer(1<<62)      n=4611686018427387904    sends=80000  live heap delta=98408360 B (93.8 MiB)
```

**4× the sends ⇒ 4.0× the retention.** Linear and unbounded, while the bounded configuration retains 17 KiB
regardless of traffic. This is the §1.3 shape *exactly*: `len(s.ring) >= max` is structurally identical to
`len(s.conns) >= s.cfg.maxConns()` (`sse_server.go:182`, certified **DEFECTIVE**) and to
`len(s.groups) >= s.maxGroups` (`groupstore.go:108`, certified **DEFECTIVE**).

**The escape argument is not a discriminator** — the same failure D-AB was written to eliminate. *"A caller who
sets it to `1<<40` has asked for that much retention"* is true verbatim of `WithCapacity(1<<40)`,
`WithMaxGroups(1<<40)` and `WithMaxConnections(1<<40)`, all three of which are class members. And *"evicts at
the cap"* is true of `WithMaxGroups` too — `groupstore.go:108` sheds at the cap (`msgin: message dropped by
overflow policy`, measured below); evict-versus-reject is a *policy* difference that has no bearing on
retention.

### Why it matters

Three separate consequences, in ascending order of cost:

1. **The census is wrong again.** §2.1's summary line — *"8 fixed + 2 deferred + 6 safe = 16 options"* — and
   every count derived from it are off by one. This is the **fourth** round in which the partition asserted
   "with no residual" has a residual (round-1 BLOCKER-2, round-3 BLOCKER-1, now this).
2. **AC-5 half 2 would ship an inverted gate.** §6 AC-5's *safe* arm asserts a key *"**accepts** `1<<62` and its
   product is usable"*, and §6/Task 7 define "usable" for `WithReplayBuffer` as *"one event round-trips
   through a live `SSEServer`"* — the **single most expensive fixture in the plan**, built specifically to
   certify this row. That test would assert an unbounded, traffic-driven retention lever is conformant **and
   would fail if anyone later bounded it**. Spec §3.8 names this exact inversion as the reason the byte knobs
   must not be called safe; the same argument applies here and was not applied.
3. **Round 3 flagged it and revision 4 did not adjudicate it.** [`029-audit-round-3.md`](029-audit-round-3.md)
   §"Carried into round 4" says: *"Whether `WithReplayBuffer` belongs in BLOCKER-1's residual class … §2.1's
   'a resource decision under an explicit bound' may or may not survive that framing; not settled here."*
   Revision 4 re-stated the unsettled framing verbatim as a settled verdict, without re-running D-AB against
   it. A carried-forward suspicion that is restated rather than tested is how this bundle has lost three
   rounds.

### Recommended fix

**Move `WithReplayBuffer` into the *fixed here* arm, as a ninth defective knob** — it is the cheapest of the
nine, not the most expensive:

- It is **R1-a shaped with an existing sentinel and an existing lower arm**:
  `options.go:1174 if cfg.replayBufferSet && cfg.replayBuffer <= 0 { return nil, ErrInvalidReplayBuffer }`.
  The fix is one extended condition plus one constant — the same edit as `WithMaxConnections`, in the same
  function, in the same task (Task 2).
- Its unit is **events retained**, which is exactly the kind of unit D-AB says the library *can* reason about
  (*"a ceiling in messages, connections or goroutines is a statement about what the knob means"*). It is not
  a byte cap, so §3.8's deferral rationale does not reach it.
- Suggested ceiling `1 << 16` events (4096× a plausible reconnect window; at the measured ~1.2 KiB/entry for a
  1 KiB payload that is ~80 MiB, in the same band as the other ceilings).
- `ErrInvalidReplayBuffer`'s message (`msghttp: replay buffer must be > 0`) joins §3.5's genericisation table,
  taking it from 5 rows to 6 and finally making the ADR's *"all six sentinel messages"* true (MINOR-4).

If instead it is **deferred**, it must be recorded like the byte knobs — *class member, remedy deferred*, with
its AC-5 row annotated so it never reads as a safety certificate. **What it must not remain is *safe*.**

Either way, §2.1's cause **(c)** needs rewriting or deleting: as written it is not a safety property, because
"the structure evicts at the cap" is a statement about `n`, not a bound independent of `n`. If
`WithReplayBuffer` is the only row it ever covered, deleting (c) leaves a three-cause list that is
demonstrably discriminating.

---

## MAJOR findings

| # | Finding | Evidence | Recommended fix |
|---|---|---|---|
| **M4-1** | **ADR 0032 did not receive three of revision 4's own fold-ins, and now contradicts the spec on a number the spec calls out as load-bearing.** The fold-in was applied to the spec and the plan and skipped in the ADR — the named failure mode, one file short. Four instances: (a) **D-AA half 2 still says a defective knob *"asserts it **rejects** `1<<62` with a typed error"*** — the exact sentence round-3 **m3-5** ruled unwritable for `memory.WithBuffer`, and which Spec §6 and Plan Task 7 both correctly restate over the *surface*. (b) **D-AA's third limitation bullet still claims `time.Duration` is *"outside the gate **and currently safe, checked**"*** — the claim round-3 **m3-2** falsified and which Spec §3.7.4 explicitly narrows and Plan Task 7 explicitly forbids (*"Do not claim `time.Duration` is 'checked'"*). The ADR is the **normative** artifact; an implementer reading D-AA gets the retracted claim. (c) **D-X says `ErrInvalidCapacity` has *"THREE producers after this increment, not two"*** while Spec §3.5 says **FOUR** and names them, and Spec §3.9 and Plan Task 4 both say "fourth". Spec §2.1 warns *"Re-derive this line before citing it — it has been wrong in every prior revision"*; the ADR cites the stale one. (d) **D-Y cites `memory.go:58`** for `b := &Broker{ch: …}` — round-3 **m3-1(a)**; the spec fixed it to `:59`, the ADR did not. | `docs/adrs/0032-sizing-option-bounds.md:206-207` (rejects), `:249-252` (checked), `:140-141` (THREE), `:168` (`:58`). Ground truth: `sed -n '59p' adapter/memory/memory.go` → `b := &Broker{ch: make(chan msgin.Message[any])}`; `sed -n '58p'` → `func New(opts ...Option) *Broker`. Spec counterparts: `016:796-800`, `016:559-569`, `016:481-484`, `016:449`. | Apply the four edits to the ADR. Then add a mechanical check to the revision procedure: **after folding a finding, grep the finding's key phrase across all three files** (`grep -n 'rejects .1<<62' docs/{specs,adrs,plans}/…`) and require zero hits outside a quoted-history block. Five of the last six partial fold-ins would have been caught by that grep. |
| **M4-2** | **The "complication recorded now so it is not rediscovered" is false: `WithMaxBodyBytes(-1)` does *not* mean "use 1 MiB" — `NewConfig` rejects it with a typed error.** Spec §3.8 and ADR D-AB both assert *"`maxBody()` (`options.go:235`) currently **back-fills the default for `n <= 0`**, so `WithMaxBodyBytes(-1)` today means *'use 1 MiB'*, not *'unbounded'*. **There is no off-state to document — adding one is a behavioral change**"*. `NewConfig` validates the explicit value at `options.go:1128-1131`; `maxBody()`'s back-fill fires **only** for a hand-built `*Config` that skipped `NewConfig`, and the code's own comment says so. The record handed to the follow-up increment therefore mis-states both the current behaviour and the shape of the remaining work. Same class as the stored lesson *"verify structural claims against code"* — and, like it, the false claim is adjacent to a comment that contradicts it. | Measured: `NewConfig(WithMaxBodyBytes(-1)) -> cfg=false err=msghttp: max body bytes must be > 0`; `(0)` → same; `(1<<62)` → `err=<nil>`. `WithMaxEventBytes(-1)` → `msghttp: max event bytes must be > 0`. Source: `options.go:1128-1131` `if !cfg.maxBodyBytesSet { … } else if cfg.maxBodyBytes <= 0 { return nil, ErrInvalidMaxBodyBytes }`; `options.go:232-234` *"NewConfig rejects an explicit non-positive WithMaxBodyBytes outright, so this fallback only ever fires for a Config that skipped NewConfig."* | Replace the paragraph in **both** Spec §3.8 and ADR D-AB with what is true: *both* byte knobs already reject an explicit `n <= 0` with a typed error (`ErrInvalidMaxBodyBytes` / `ErrInvalidMaxEventBytes`), so the follow-up increment's decision is **not** "invent an off-state where a back-fill lives" but "choose whether an explicit off-state should exist at all, and if so which sentinel value carries it" — a strictly smaller and different problem. Also drop *"There is no off-state to document"*: leaving the option unset already **is** the documented default state. |
| **M4-3** | **Task 4's mandatory small-`n` case and AC-5's `WithCompletionSize` row cannot be turned into running tests as written — the fixture is under-specified in three ways, and revision 4 mislabels round 2's error string as "wrong" when it is the very next gate.** This is the **fourth** instance of the class that produced round-1 BLOCKER-1, round-2 BLOCKER-3 and round-3 M3-1. Task 4 says: *"RED (cap-still-caps, at small `n`): `WithCompletionSize(2)` over 6 messages → **`released=3`**"*, and Spec §6/Task 7 size the AC-5 fixture as *"`NewAggregator(store, fn, opts…)` — a `msgin.MessageGroupStore` **and an aggregation `fn`**, both positional"*. Measured, that fixture does not construct: `NewAggregator` also requires **`WithOutputChannel`**, and `Handle` additionally requires either a **correlation strategy** or a correlation-key header on every message, plus a **subscribed handler** on the output channel for `released` to be observable at all. Revision 4's correction block states *"the bare call returns `msgin: channel store is nil`, not round 2's `msgin: aggregator output channel is nil` — do not paste that string into an assertion"*; both strings are real, they are simply **consecutive gates** — round 2's is what you get once `store` and `fn` are supplied. Calling it "wrong" hides the missing fixture rather than exposing it. | ```NewAggregator(store, fn)                    err=msgin: aggregator output channel is nil```<br>```NewAggregator(store, fn, WithOutputChannel) err=<nil>```<br>```+ WithCompletionSize(1<<62)                 err=<nil>```<br>```default correlate, Handle(no corr header)   err=msgin: permanent: msgin: message has no correlation key```<br>With the full fixture (store + fn + `WithOutputChannel` + `WithCorrelationStrategy` + a subscribed handler): ```WithCompletionSize(2) over 6 msgs: released=3``` — the asserted figure is right; only the fixture is missing. | State the fixture completely in Task 4 and in Spec §6/Task 7: `NewAggregator(store, fn, WithOutputChannel(ch), WithCorrelationStrategy(fixedKey), WithCompletionSize(n))` **plus** `ch.Subscribe(counter)`. Rewrite the correction block: round 2's string is not wrong, it is the **second** gate — record both, in order, so an implementer recognises each. Apply the same "state the whole fixture" rule Task 2 already applies to `serveInBackground` and Task 3 now applies to `WithOverflow(OverflowReject)`. |
| **M4-4** | **§1.4's *live* column does not reproduce under its stated fixture, and contradicts §1.4's own prose figure by 2.4×.** §1.4 reports, for one correlation key and *"realistic `msgin.New` messages"*, `live` of **3.2 / 13.8 / 67.8 MiB** at `1<<12` / `1<<14` / `1<<16` members — and, four lines above, *"after 60000 Handle() into ONE group … live heap delta=30078064 B (**28.7 MiB**)"*. 60,000 → 28.7 MiB and 65,536 → 67.8 MiB cannot both hold: 1.09× the members for 2.4× the retention. Re-measured, the **prose figure is right and the table's live column is inflated** — it was evidently read without a `runtime.GC()` before `ReadMemStats`, so it counts uncollected garbage, which under a quadratic-churn workload grows with `n`. This matters because revision 4's headline promise is *"every figure now names its fixture"*: the **fixture** is named, the **protocol** is not, and this is a revision-4-*new* figure, quoted in D-Z's public rationale. (The load-bearing columns — churn and elapsed — reproduce almost exactly; see Verified-negative.) | Fixture: `msgin.New[any](i)`, one key, `WithCompletionSize(1<<62)`, `runtime.GC()` before both reads, `KeepAlive(agg, store)`.<br>```1<<12  cumulative= 206.7 MiB  LIVE=  2.0 MiB``` (spec: 207.0 / **3.2**)<br>```1<<14  cumulative=3143.5 MiB  LIVE=  7.8 MiB``` (spec: 3145.3 / **13.8**)<br>```60000  cumulative=41474.0 MiB LIVE= 28.7 MiB``` (spec prose: **28.7** ✓)<br>```1<<16  cumulative=49451.7 MiB LIVE= 31.0 MiB``` (spec: 49459.6 / **67.8**) | Re-measure the live column with an explicit `runtime.GC()` and `runtime.KeepAlive`, and **state the protocol beside the fixture** — "GC'd, `HeapAlloc` delta, `KeepAlive`d" — in §1.4, §3.4 and D-Z. Use **31.0 MiB** at the ceiling. Extend the revision-4 rule from *"name the fixture"* to *"name the fixture **and** the measurement protocol"*: AC-4 already encodes exactly these three conditions for the tests, and the design's own probes are not held to them. |
| **M4-5** | **Six counts across the three files still describe the seven-knob census, one of them in the goal statement of the plan's very first task.** Revision 4 moved the census 7 → 8 and updated the tables, but not the prose that keys on the old totals. This is the same class as round-2 **BLOCKER-1** (16 vs 17 printed under a 17-row table), which that round rated a BLOCKER because it makes the first task's evidence gate fail on arrival. Rated MAJOR here only because every instance is prose rather than a gate threshold — the AST key count (17) is correct everywhere. | `docs/plans/029-…:246` *"Reproduce all **7** defects"* (8 — and the sub-bullets never mention `WithCompletionSize`, so Task 0's baseline would omit the knob Task 4 depends on). `:261` *"Confirm the **9 safe** rows"* (6 safe + `burst` = 7; 9 was the pre-`WithCompletionSize`, pre-deferral count). `:94` *"All **seven** sites are pasted below verbatim"* (eight knobs; five shapes; two of the eight are described, not pasted). Spec `:677` *"For each of the **7** defective knobs"*, `:679` *"the **six** R1"* (7 R1), `:718` *"For each of the 7"*, `:847` *"The **6** R1 upper arms"* (7), `:854` *"**In:** the 7 defective knobs"*, `:404` *"R2 — latch, report at first use (**1 of 7**)"* (1 of 8 — while `:357` correctly reads "7 of 8"). ADR `:109` *"The **six** R1 knobs return…"*. | Fix all ten, then add the count-consistency probe to the revision checklist: `grep -nE '\b(6\|six\|7\|seven\|9\|nine)\b' docs/{specs/016,adrs/0032,plans/029}-*.md` and hand-adjudicate every hit against §2.1's table. **Do not increment the printed numbers — re-derive them from the table**, exactly as Plan Task 8 already requires for CLAUDE.md's own counts. |

---

## MINOR findings

| # | Finding | Evidence | Recommended fix |
|---|---|---|---|
| **m4-1** | **Stale task cross-reference: Spec §3.6's justification for folding in `WithBuffer(-1)` says it *"costs three lines in a closure **Task 3** already rewrites"*.** After revision 4's renumbering, `WithBuffer` is **Task 5**; Task 3 is `WithCapacity`/`WithMaxGroups`. | `docs/specs/016-…:521`; plan Task 5 header `:407`. | `Task 3` → `Task 5`. |
| **m4-2** | **Round-3 m3-1(b) landed in the spec only.** The `clamped to 0` godoc is at `memory.go:35-37`; Spec §4 says `:35-37` (fixed), **Plan Task 5 still says `memory.go:34-37`**. Line 34 is the godoc's first line, not the sentence. | `sed -n '34,37p' adapter/memory/memory.go` → `34: // WithBuffer sets the channel buffer size (default 0 …` / `35-36:` the clamp sentence. `docs/plans/029-…:415`. | `:34-37` → `:35-37` in Task 5. |
| **m4-3** | **Round-3 m3-1(a) landed in the spec only** — ADR D-Y still cites `memory.go:58`. (Rolled into M4-1(d); listed separately so the m3-1 fix-verification row is auditable.) | `docs/adrs/0032-…:168`. | `:58` → `:59`. |
| **m4-4** | **"All six sentinel messages are genericised" contradicts the five-row table in the same paragraph.** Spec §3.5 says *"All **six** sentinel messages…"* then tables five and notes *"(Five rows, not seven: `WithCapacity`, `WithMaxGroups`, `WithBuffer` and `WithCompletionSize` all share `ErrInvalidCapacity`.)"*. ADR D-X repeats "six". The eight knobs use **five** distinct sentinels; "six" has been wrong since revision 2 and survived rounds 2 and 3. *(It becomes six if BLOCKER-1 is fixed by including `ErrInvalidReplayBuffer`.)* | `docs/specs/016-…:492` + table `:497-504`; `docs/adrs/0032-…:124`. | "six" → "five" in both, or → "six" **with** `ErrInvalidReplayBuffer` added as the sixth row if BLOCKER-1 is fixed. |
| **m4-5** | **A dangling *"see note"*.** §2.1's `WithPollMaxBatch` row reads *"safe **(a)** … *derived from `maxInFlightCeiling`*, see note"*, but no note after the table discusses it. (The `WithReplayBuffer` row's *"see note"* does resolve.) The derivation is real and worth stating: `held` is bounded by `min(pollMaxBatch, free credits)` (`poller.go:34-36`), and free credits are `maxInFlight` (`consumer.go:385`) — so **this row's safety is created by Task 1**, not pre-existing. | `docs/specs/016-…:316`; `endpoint/poller.go:34-36`, `endpoint/credit.go:21`, `endpoint/consumer.go:385`. | Add the note, and say explicitly that `WithPollMaxBatch` is *derivatively* safe — a reader who later relaxes `maxInFlightCeiling` must revisit it. |
| **m4-6** | **Plan Task 3's producer arithmetic is wrong at the point in the sequence where it is stated.** Task 3 says `ErrInvalidCapacity` *"now has **three** producers … Widen its godoc to name **all three**"*. At the end of Task 3 there are **two** (`NewQueueStore`, `NewGroupStore`); the third arrives in Task 5 (`WithBuffer`) and the fourth in Task 4 (`NewAggregator`), which runs **before** Task 5. Following the plan literally ships a godoc that is wrong twice in a row. | Plan `:361-362` vs `:398-399` (Task 4, "FOURTH"), `:407` (Task 5). | Move the genericisation + godoc widening to **Task 5** (the last producer to land), or state in Task 3 that the godoc is written for the end-state four and cross-reference Tasks 4 and 5. |
| **m4-7** | **`docs/HANDOVER.md` §8 now has a **sixth** backlog item — created by this bundle — and both places the plan enumerates the backlog say "items 2–5".** An implementer following Task 8 literally could strike item 1 and drop item 6, which is the *only* record of the deferred byte-ceiling work. | `docs/HANDOVER.md` §8 item 6 (*"NEW — the byte-ceiling class, deferred out of Plan 029…"*), cited by Spec §3.8 and ADR D-AB. Plan `:557` (*"strike backlog item 1 while leaving items 2–5"*) and `:574` (*"items 2–5"*). | "2–5" → "2–6" in both. |
| **m4-8** | **Section order: §3.9 is printed before §3.8.** §3.8 (the deferred byte class) is the newer section and was appended after §3.9; the spec's own cross-references (§2.1 → §3.8, §2.1 → §3.9) both jump backwards or forwards unpredictably. | `docs/specs/016-…:571` `### 3.9 …` then `:596` `### 3.8 …`. | Swap them, or renumber §3.9 → §3.10. |
| **m4-9** | **R1-d over-rejects a configuration in which `n` is inert, and no document records the decision.** `WithReleaseStrategy` / `WithReleaseWhen` **overwrite** `c.release`, so `WithCompletionSize(1<<62)` followed by either leaves the huge `n` with no effect at all — yet the planned `completionSizeSet` arm still rejects. That is arguably the *right* choice (fail loud on a nonsense value), but it is a new construction-time rejection of a currently-legal, currently-harmless config, and it is undocumented. | Measured today: ```CompletionSize(1<<62)+ReleaseStrategy  err=<nil>``` (accepted; `n` inert — `aggregator.go:105`, `:122`, `:134` all assign `c.release`). After Task 4 the same call returns `ErrInvalidCapacity`. | One sentence in Spec §3.9 and on `WithCompletionSize`'s godoc: the bound is validated on the **value passed**, whether or not a later option replaces the release strategy. Add a table case so the behaviour is pinned rather than incidental. |
| **m4-10** | **`msgin.ErrInvalidCapacity` will span four different units.** After the increment its generic message *"capacity out of range"* fronts queue depth (`WithCapacity`), group **count** (`WithMaxGroups`), channel **buffer** (`WithBuffer`) and group **members** (`WithCompletionSize`) — e.g. `msgin: capacity out of range: routing.WithCompletionSize: 65537 not in [1, 65536]`. D-X's reuse argument (one `errors.Is` target for "this size is wrong") still holds and the site name disambiguates, but §3.5's *"One sentinel cannot carry four different stated ranges"* understates it: it is four different **units**, not just four ranges. | Rendered first-hand from the §3.1 shape against the real sentinels; `errors.Is` true and `IsPermanent` false in all four. | One clause in §3.5 / D-X acknowledging the unit spread, so a future fifth producer is a conscious decision rather than a default. |

---

## Verified-negative (attacked, found sound)

Everything below was attacked with a command and **held**. This list is as important as the findings.

| Attacked | Result |
|---|---|
| **The revision-4 retraction (the headline claim).** | **Sound, exactly.** All eight numbers in Plan Task 3's fixture table reproduce to 0.1 MiB under `-race` (output quoted above). The "wrong fixture" diagnosis is measured, not rationalised. |
| **The AST census — 17 functions / 27 methods / 22 unexported receivers.** | **Sound.** My own `go/ast` walk over the repo root: `EXPORTED FUNCTIONS (Recv==nil) with int/int64 param in ANY position: 17`; `EXPORTED METHODS: 27`. The 17 names match §2.1's 17 rows **exactly**, including `NewTokenBucket`. Unexported receivers: 3+4+3+4+3+4 in the leaf dialect modules + `responseTracker.WriteHeader` in root = **22**, matching D-AA verbatim. The `grep` census still returns **16**. |
| **D-AB's method criterion ⇒ exactly two members.** | **Sound and reproducible.** Applying *"the parameter itself sizes a `make`"* to the 27: `memory.QueueStore.Claim` (`min(max, len(s.ready))`, `queuestore.go:166`) and `channel.QueueChannel.Poll` (`queuechannel.go:50`, delegates). The three `…GroupDialect.ExpiredGroups` and `sql.Source.Poll`/`sql.QueueStore.Claim` size on `len(rows)`/`len(cands)` and correctly fall out. `ExponentialBackoff.Delay` also correctly falls out. Both members are root-constructible. **The M3-4 fix is the best fold-in in this revision.** |
| **The seven `msghttp` keys and their post-increment arms.** | **Sound.** AST: `WithConnectionBuffer:883`, `WithMaxBodyBytes:426`, `WithMaxConnections:865`, `WithMaxEventBytes:819`, `WithMaxResponseBytes:730`, `WithReplayBuffer:926`, `WithSuccessStatus:566` — 7, at the exact lines quoted. *(The arm assignment is sound **given** §2.1; BLOCKER-1 moves `WithReplayBuffer` between arms, which — note — **removes** the one fixture that needed a live `SSEServer`.)* |
| **§1.2's `int32(n) < 0` band.** *(Rev-3 verified; spot-check re-run because the whole `WithConcurrency` case rests on it.)* | **Sound, all nine rows verbatim**: `2^30` ok, `2^31-1` ok, `2^31`/`2^31+1`/`2^32-1`/`3×2^30` PANIC `sync: negative WaitGroup counter`, `2^32`/`2^40`/`2^62` **ok**. The `int32(n)` column renders exactly as tabled. |
| **The single error shape at both ends, `errors.Is`, `IsPermanent`.** | **Sound.** R1: `errors.Is` **true**, `IsPermanent` **false**, and the render is truthful at both ends — `…: endpoint.WithMaxInFlight: 0 not in [1, 1048576]` and `… 1048577 not in [1, 1048576]`. R2 (`Permanent`-wrapped `%w`): `errors.Is(err, ErrInvalidCapacity)` **true**, `IsPermanent` **true**. The rejection of `"%d exceeds %d"` is correct — that render *is* false at `n = 0`. |
| **Four `ErrInvalidCapacity` producers do not collide confusingly.** | **Sound.** The site name disambiguates all four in one line each (see m4-10 for the one caveat, which is cosmetic). |
| **The small-`n` "cap still caps" proofs, and M3-1's mandatory fixture.** | **Sound, and M3-1's fix is correct and necessary.** `MaxGroups(1): add1=<nil> add2=msgin: message dropped by overflow policy`; `Capacity(1)+OverflowReject: enq1=<nil> enq2=msgin: message dropped by overflow policy`; `Capacity(1) DEFAULT policy: enq2 **BLOCKED for 3s**`. The hang is real and the fixture is genuinely mandatory. |
| **`WithCompletionSize(2)` over 6 messages ⇒ `released=3`.** | **Sound** — once the full fixture is supplied (see M4-3, which is about the *fixture statement*, not the figure). |
| **§1.4's quadratic-churn table (the columns D-Z's rationale rests on).** | **Sound.** cumulative **206.7 / 3143.5 / 49451.7 MiB** vs the spec's 207.0 / 3145.3 / 49459.6 — within 0.02%. Elapsed 44 ms / 577 ms / 7.6 s vs 50 ms / 644 ms / 8.6 s — run-to-run noise. *(Only the `live` column fails; M4-4.)* |
| **`1 << 16` for `WithCompletionSize`.** | **Defensible.** Reaching it costs 7.6 s and ~48 GiB of allocation churn for ~31 MiB live, so the binding constraint at the ceiling is CPU — which §3.4 says. 65,536 members is ≥4 orders of magnitude above any plausible aggregation, and the "raising is safe, lowering is breaking" asymmetry favours the low value. |
| **R1-d's shape compiles and changes no exported surface.** | **Sound, and stronger than claimed.** `aggregatorConfig` is **itself unexported** (`aggregator.go:14`), so *any* new field — exported or not — is invisible to `apidiff`. `NewAggregator`'s existing checks are at `:320-330`, exactly where the new arm is placed. No shipped call site passes `n < 1`: the four in-tree uses are `capability_test.go:213` (1), `expr/expr_test.go:902` (2), `harness/groupstore.go:431` (2), `postgres/example_sql_groupstore_test.go:39` (3) — so the new arm breaks nothing, though it does mean **three non-root modules** exercise it, which Global constraint 7 and Task 8's 8-module gate already cover. |
| **AC-5's *deferred* arm for the two byte knobs.** | **Sound as designed.** *"Accepts `1<<62`, annotated 'class member, remedy deferred'"* does not certify safety, and the reasoning in §3.8 about the inversion is correct. **It is the reasoning BLOCKER-1 says was not applied to `WithReplayBuffer`** — the mechanism is right, its coverage is one row short. |
| **Safe causes (a), (b), (d) on the rows that claim them.** | **Sound.** (a) `breaker.go:164 b.fails >= b.threshold` — a scalar `int`, nothing accumulates. (b) `consumer.go:1199 len(b) > c.maxPayloadBytes` — `b` is the already-materialised `[]byte` payload. (d) `WithMaxResponseBytes` → `io.CopyN(io.Discard, …)` — nothing retained. `burst` → `float64(burst)`, and `NewTokenBucket` already rejects `burst < 1` (`ratelimit.go:43`). `WithSuccessStatus(1<<62)` rejects with `msghttp: status code must be in [100,599]` — the spec's string, verbatim. **Only cause (c) fails.** |
| **`make(chan struct{}, 1<<62)`.** | **Sound.** `cap=4611686018427387904`. AC-4's direct half is executable exactly as written. |
| **File:line citations.** | **Sound.** I resolved **37** distinct citations with `sed -n 'Np'` — `consumer.go` 251/262/272/330/384/385/457/458/459/1199, `credit.go:21`, `poller.go:36`, `memory.go` 38/43/59, `queuestore.go` 99/108/132/166, `groupstore.go` 84/91/94/108/205, `options.go` 235/1158/1164, `sse_server.go` 182/201, `queuechannel.go:50`, `errors.go` 51/144/258, `http/errors.go` 145/150, `aggregator.go` 505/526 — plus `aggregator.go` 14/132/134/320-330, `encode.go:102`, `sse.go:384-386`, `ratelimit.go:43`, `sse_server.go:470-476`. **Every one resolves to the quoted construct.** The only two bad citations left in the bundle are the round-3 residue in m4-2 and m4-3, each of which is correct in the *other* two files. |
| **CLAUDE.md compliance on the testable axes.** | **Sound.** Blackbox-only and assert-closure tables are stated in Global constraints 1–2 and re-stated per task. No new exported symbol and no signature change — verified structurally (constants unexported per the ceilings table; the one new field sits in an unexported struct). The multi-instance statement is present, correct and non-vacuous (Spec §7). Hot-path branch enumeration is present as a table and covers every new arm. Task 8's gate is 8 modules × 8 CI steps with the `harness`/`go vet` and Docker caveats. The Go-skills header and the `table-test`/`use-mockgen`/`use-testcontainers` overrides are all restated in the plan, as CLAUDE.md's writing-plans override requires. |
| **Mutation coverage, including Task 4's four.** | **Sound.** Every assertion class has a targeted mutant, and Task 4's (a) delete-the-arm, (b) drop-the-`set`-flag, (c) record-`n`-but-never-read, (d) swap-`lo`/`hi` each target a *different* assertion — (b) in particular targets the one failure mode R1-d newly creates. Global constraint 5's carve-out (guard-deletion permitted for AC-1 only, because it turns the whole file red) is correctly reasoned. AC-4's two mutants and the insistence that `credit.go:21` gets its own are correct and the arithmetic now matches between spec and plan (m3-3 landed). |
| **Task renumbering integrity.** | **Sound.** Tasks 0–8, nine tasks; every intra-plan reference resolves (Task 6 → "after Tasks 1–5"; Task 8's four-audit-record count is already right). The only stale task reference is in the **spec** (m4-1). |
| **The "283 MiB" / "~1 GB corrected" retraction is complete.** | **Sound.** `grep -n '283' docs/{specs/016,adrs/0032,plans/029}-*.md` returns exactly three hits — `plans:30` and `specs:20` (the 🔴 RETRACTION headers, which quote the figure in order to withdraw it) and `plans:230` (*"Revision 3 printed 283 MiB / 108 MiB here and claimed round 2's ~1 GB 'did not survive re-derivation' — **that was wrong**"*). **Zero hits assert the figure**, and the ADR carries none at all. The retraction is complete in all three files — the one fold-in this revision applied everywhere. |

---

## Round-3 fix verification

One row per round-3 finding. **LANDED** = fixed everywhere it appeared. **LANDED-BUT-FLAWED** = fixed, but
incompletely or with a new defect. **NOT LANDED** = unchanged. **REGRESSED** = made worse.

| # | Round-3 finding | Verdict | Why |
|---|---|---|---|
| **BLOCKER-1** | §2.1's partition still incomplete — three knobs certified safe are class members | **LANDED-BUT-FLAWED** | The *mechanism* is right and is the best thing in this revision: D-AB states a criterion, `WithCompletionSize` becomes an 8th fixed knob with a correct ceiling and a correct new code shape, the byte knobs are correctly recorded as class members with a deferred remedy rather than as safe. **But the criterion was not re-run against every row it now governs**: `WithReplayBuffer` — which round 3 *explicitly carried forward as unsettled* — is a class member under D-AB and is still certified safe **(c)**, and cause (c)'s own text is self-refuting. Fourth round, fourth residual. See BLOCKER-1. |
| **M3-1** | The mandatory small-`n` `WithCapacity` proof HANGS without `WithOverflow(OverflowReject)` | **LANDED** | The fixture is now in the **mandatory** bullet in Task 3, in Spec §6's pasted block, and flagged 🔴 in both, with the correct note that the `groupstore_test.go` precedent does not transfer. Re-measured: the hang is real (`BLOCKED for 3s`) and the fixture removes it. |
| **M3-2** | Revision 3's "corrected" growth figures are fixture artifacts; its retraction claim is false | **LANDED** | Retracted in all three headers, both fixtures tabled in Plan Task 3, and **all eight numbers reproduce exactly** under `-race`. The strongest fold-in in the revision. *(The general lesson is only partly internalised — see M4-4, where a rev-4-new figure names its fixture but not its protocol.)* |
| **M3-3** | "Six `msghttp` keys" wrong in count and content | **LANDED** | Corrected to **seven**, named at the exact AST lines, and partitioned by post-increment arm in both Spec §6 and Task 7 — with the correct observation that the increment dissolves most of the fixture need. *"Its product is usable"* is now defined for both key classes, closing round 2's unsized item. |
| **M3-4** | §2.0's "four class members among the 27 methods" is hand-derived and irreproducible | **LANDED** | Now derived from D-AB's strict criterion; I reproduced **two** from the rule independently, and both are root-constructible, so the "named but uncovered" residue genuinely disappears and D-AA's limitation gets simpler. Exemplary application of the stored *"derive move-lists mechanically"* lesson. |
| **m3-1(a)** | `memory.go:58` → `:59` (the `b.ch` init line) | **LANDED-BUT-FLAWED** | Fixed in Spec §3.3; **ADR D-Y still says `:58`**. |
| **m3-1(b)** | `memory.go:34-37` → `:35-37` (the *clamped to 0* godoc) | **LANDED-BUT-FLAWED** | Fixed in Spec §4; **Plan Task 5 still says `:34-37`**. |
| **m3-1(c)** | `ratelimit.go:45` → `:43` (the `rps <= 0` rejection) | **LANDED** | Spec §3.7.4 now cites `:43`; verified `43: if rps <= 0 \|\| burst < 1 {`. |
| **m3-2** | `time.Duration` exclusion claimed as *"checked"* while the `clock.After` class was never audited | **LANDED-BUT-FLAWED** | Spec §3.7.4 narrows the claim correctly and enumerates the five further `clock.After` sites; Plan Task 7 explicitly forbids the word *"checked"*. **ADR D-AA still asserts *"outside the gate and currently safe, checked"*** — and the ADR is normative. |
| **m3-3** | Spec and plan disagree on AC-4's `queuestore.go:108` mutant arithmetic | **LANDED** | Both now state `chan struct{}` → `chan msgin.Message[any]`, 24 B, **25,165,824 B**, **24×** the `delta < 1 MiB` bound. Identical in both files. |
| **m3-4** | Two different code shapes shown for `WithBuffer` (`latchSizing` vs inline) | **LANDED** | Spec §3.3 now uses the inline `if b.err == nil { b.err = fmt.Errorf(…) }` form, matching the plan's R2 block and Plan 028's shipped latch. `latchSizing` appears nowhere. |
| **m3-5** | AC-5 half 2's *"rejects `1<<62`"* cannot be written for `WithBuffer` | **NOT LANDED** *(in the normative artifact)* | Spec §6 and Plan Task 7 both restate the arm over the **surface**, correctly and with the reason. **ADR D-AA half 2 still reads *"A defective knob asserts it *rejects* `1<<62` with a typed error"*** — verbatim the sentence the finding rejected. Since the ADR is the decision of record, the finding did not land where it counts. |
| **m3-6** | §2.1's summary line renders mangled (nested backticks) | **LANDED** | The line is now plain bold; renders correctly. |
| **m3-7** | `lo` never stated for the `<= 0` knobs, yet AC-2b asserts the full `[lo, hi]` render | **LANDED** | §3.1's table gains a `lo` column (`1` for all seven R1 knobs, `0` for `WithBuffer`), with the reasoning that `<= 0` and `< 1` are the same predicate over `int`. Verified by rendering both ends. |

**Totals (12 findings, 14 rows — m3-1 split into its three instances and then re-collapsed):**
**6 LANDED · 5 LANDED-BUT-FLAWED · 1 NOT LANDED · 0 REGRESSED.**
Counting m3-1 as a single finding: **6 LANDED / 5 LANDED-BUT-FLAWED / 1 NOT LANDED** of 12.

**Trend across rounds.** Rounds 1→2: 10 of 19 unclean, 2 regressions. Rounds 2→3: 5 of 17 unclean, 0
regressions. Rounds 3→4: **6 of 12 unclean, 0 regressions.** The regression rate stays at zero and the
*substantive* fold-ins (M3-1 through M3-4) all landed cleanly — but the unclean fraction rose (29% → 50%),
and **every one of the six unclean rows has the same shape: the finding was folded into two files of three.**
The ADR, being the file an implementer is least likely to re-read, absorbed four of them.

---

## Carried into round 5 — suspected, not proven

- **Whether cause (c) survives BLOCKER-1's resolution at all.** If `WithReplayBuffer` is its only member, (c)
  should probably be deleted rather than reworded — a three-cause list that is demonstrably discriminating is
  worth more than a four-cause list with one dead arm. Not settled here; it is a design call.
- **Peak RSS of the whole `go test ./...` run.** Still unmeasurable end-to-end — the code does not exist.
  Individually re-measured or carried: Task 1 ≈ 48 MiB + ~257 MiB of goroutine stack under `-race`, Task 2
  ≈ 1.5 MiB, Task 4 ≈ 0 under the growth split, Task 6 ≈ 48 MiB, Task 3 ≈ 0 under the growth split (or
  484–853 MiB live if a ceiling-sized growth case is written with realistic messages). Go runs packages in
  parallel, so the peak is a **sum**. Task 8 must measure and record it; a ninth knob does not change this.
- **Whether the `93.9%` coverage floor in Task 8 is still the right baseline** after `routing` gains an arm and
  a config field. Not re-derived this round (it needs a full `-coverpkg` run, and the number's provenance is a
  prior increment). If it is stale, Task 8's gate is either vacuous or spuriously red.
- **`WithMaxEventBytes` overshoots its own cap by one line.** `sse.go:384-386` writes the line into `dataBuf`
  and *then* tests `Len() > maxEventBytes`, so the buffer always exceeds the cap by the size of the line that
  tripped it. Bounded and harmless at the default; noted because the deferred increment will have to decide
  whether the cap is a pre- or post-condition. Not a finding against this bundle.
- **Whether the AC-5 gate should key on `int`/`int64` *type* at all**, given that the whole `time.Duration`
  class is excluded "by construction" precisely because `go/ast` sees a different type name. The exclusion is
  correctly stated as deliberate; whether it is *right* is a question for the `time.Duration` increment.

---

## Auditor's method note

**Re-derived first-hand this round** (every figure quoted above is my own command output, none transcribed
from the bundle or from rounds 1–3):

- The `go/ast` census — 17 exported functions with an `int`/`int64` parameter in any position, their names and
  lines; 27 exported methods; the 22-unexported-receiver split. Written from scratch in a throwaway module.
- The `grep` census (16 `With…` options).
- **`WithReplayBuffer`'s unbounded retention** — the BLOCKER — at three configurations, with the fixture and
  the GC/KeepAlive protocol stated.
- The full `WithMaxGroups` / `WithCapacity` growth matrix under `-race`, in **both** fixtures (four rows), to
  test revision 4's retraction.
- The `WithCompletionSize` quadratic table in four sizes, twice — once without and once with `runtime.GC()` +
  `KeepAlive` — which is how M4-4 was found.
- The full `NewAggregator` fixture chain (bare → +`fn` → +`WithOutputChannel` → +correlation), the
  `released=3` proof, and the option-order/inertness probe behind m4-9.
- `NewConfig` behaviour for `WithMaxBodyBytes(-1/0/1<<62)`, `WithMaxEventBytes(-1/1<<62)`,
  `WithReplayBuffer(1<<62)`, `WithSuccessStatus(1<<62)` — which is how M4-2 was found.
- The full §3.1 error-shape matrix at both ends: render, `errors.Is`, `IsPermanent`, for R1 and R2, plus the
  post-genericisation render for all four `ErrInvalidCapacity` producers.
- All 37 `file:line` citations, resolved by `sed -n 'Np'` against the named file.
- The small-`n` cap proofs and the `OverflowBlock` hang (3 s observed).
- `make(chan struct{}, 1<<62)`.
- The four in-tree `WithCompletionSize` call sites and their argument values.

**Spot-checked rather than fully re-run** (verified by prior rounds first-hand, untouched by revision 4, and
cheap to re-confirm partially):

- **§1.1's two-regime `makechan` table** (`2^43`/`2^42` recoverable panic vs `2^41` unrecoverable
  `fatal error: out of memory`, and the `GOMEMLIMIT` result). Not re-run — the `2^41` row kills the process by
  design and rounds 1–3 each reproduced it. I re-confirmed only the zero-size-element corollary
  (`make(chan struct{}, 1<<62)`), which is the row this increment's AC-4 depends on.
- **The `int32(n) < 0` band** — re-run in full anyway (all nine rows), because `WithConcurrency`'s entire case
  rests on it and it has been mis-stated twice.
- **The four original panic reproductions end-to-end through `Consumer.Run` / `ServeHTTP` / `memory.New`.** Not
  re-run; verified identically in rounds 1, 2 and 3 and unchanged by revision 4. The `wg.Add` predicate — the
  one of the four that moved between revisions — *was* re-run.
- **The 48 MiB / 1.5 MiB / 24.0 MiB / ~257 MiB ceiling costs and the AC-4 allocation-delta series.** Not
  re-run; round 3 measured all of them first-hand, revision 4 did not change them, and m3-3's arithmetic
  correction verifies by inspection (`sizeof(msgin.Message[any]) = 24`, `1<<20 × 24 = 25,165,824`).
- **The docs-link gate.** Not re-run repo-wide; the bundle's own relative links were resolved individually as
  part of the citation pass.

**Hygiene.** A leftover `…/scratchpad/r4/{probe,scan}` from the session that died before writing anything was
found in the shared scratchpad; it was **not** read or reused, and every measurement above was taken in
**freshly created** modules (`…/scratchpad/probe` for the AST scanner, `…/scratchpad/p2` for every behavioural
and allocation probe) so no earlier session's output could be mistaken for mine. Both mine and the stale `r4`
were deleted after the run; the scratchpad is now empty. All probe files lived outside the repository, in
throwaway modules with `replace github.com/kartaladev/msgin => /Users/zakyalvan/Documents/RND/msgin`. No
scratch `.go` file was ever written into the repo. `git status --short` was re-checked after the probes and
shows exactly the expected 8 documentation files (2 modified — `docs/HANDOVER.md`,
`docs/adrs/0031-nil-option-elements.md`; 6 untracked — the 3 bundle files and the 3 prior audit records) plus
this report. **No repository file was modified except this one.**

---

**VERDICT: NOT SAFE TO IMPLEMENT.** 1 BLOCKER, 5 MAJORs, 10 MINORs.

The BLOCKER is one row and one paragraph of work; the MAJORs are four document edits and one fixture
statement. **This bundle is close.** The single change that would most improve the odds of round 5 being the
last is procedural rather than technical: **fold every finding into all three files and grep to prove it**.
Four of this round's six unclean rows, and the whole of M4-1, are the same defect — a fix applied to the spec
and the plan and not to the ADR.
