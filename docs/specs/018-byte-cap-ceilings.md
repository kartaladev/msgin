# Spec 018 — A byte cap carries a representability ceiling, and no off-state

- **Status:** **PROPOSED — revision 4, post-audit-round-3, NOT accepted.** Written before any code, per
  [CLAUDE.md](../../CLAUDE.md)'s design-time gate. **Three rounds of the adversarial design audit have run** over
  the assembled bundle (this spec + [ADR 0034](../adrs/0034-byte-cap-ceilings.md) +
  [Plan 032](../plans/032-byte-cap-ceilings.md)). Round 1 returned **NOT SAFE TO IMPLEMENT** — 3 BLOCKERs,
  7 MAJORs, 4 MINORs ([`docs/plans/032-audit-round-1.md`](../plans/032-audit-round-1.md), immutable). Round 2
  returned **NOT SAFE TO IMPLEMENT** — 1 BLOCKER, 5 MAJORs, 6 MINORs
  ([`docs/plans/032-audit-round-2.md`](../plans/032-audit-round-2.md), immutable). Round 3 returned **NOT SAFE TO
  IMPLEMENT** — 0 BLOCKERs, 3 MAJORs, 4 MINORs
  ([`docs/plans/032-audit-round-3.md`](../plans/032-audit-round-3.md), immutable), with **all twelve round-2
  findings LANDED and nothing regressed**. This revision folds every round-3 finding back. **Round 4 has not
  run.** Two rounds is this project's norm; Plan 029 needed five.
  - 🔴 **Every decision here was taken by the coordinator WITHOUT USER RATIFICATION.** The user was not asked.
    Each is **explicitly reversible**; §8 lists the four that most deserve a second look, and each ADR 0034
    decision carries a **REVERSIBILITY** line stating what undoing it costs.
  - **Round-1 findings and where each is discharged:** B-1/m-14 → §6 AC-4 + Plan Task 1 (one commit);
    B-2 → §1.3 item 2, §3.1, §6 AC-2c; B-3 → §6 AC-4 (mechanical inventory);
    M-4 → §2, §6 AC-3; M-5 → §3.2; M-6 → §3.5, §5; M-7 → §3.4; M-8 → §6 AC-2b/§6 AC-4;
    M-9 → §6 AC-4 (deltas + ordering); M-10 → §6 AC-1; m-11 → §2.1; m-12 → §1.1; m-13 → Plan constraint 6.
  - **Round-2 findings and where each is discharged:** **N-1** (BLOCKER — the restated gate invariant is false
    for the six `safe`-arm rows) → §6 AC-4.1 site 7, rewritten **two-dimensionally**; **N-2** (the derivation
    grep's predicate misses two sites) → §6 AC-4.1's widened command, pasted output, 14 sites, 18 lines;
    **N-3** (Global constraint 6 regressed) → Plan constraint 6, restored to bound the **fixture**;
    **N-4** (the "whichever lands second" protocol is unilateral) → §6 AC-4.2 / AC-5, **Plan 032 owns Spec 016
    §2.1 unconditionally and re-derives it**; **N-5** (call-site totals 48/34/18 → 49/40/24) → §3.1a;
    **N-6** (the parents were scheduled one commit late) → §6 AC-5, folded into **Task 1**; **N-7**
    (`math.MaxInt32 - 1` dissolves the `(n int)` rejection) → §3.5, §5, §8 item 2; **N-8** (the falsification
    sweep's grep case) → Plan Step 11; **N-9** (`checkRange`'s godoc counts) → Plan Step 5; **N-10** (the
    orphaned `math` import) → §6 AC-2c; **N-11** (`WithMaxEventBytes` is parse-side only) → §1.3 item 3, §4
    item 3; **N-12** (AC-3 never vacuity-probed) → §6 AC-3, §6 AC-7. The two round-1 residues round 2 re-raised —
    **m-11 not landed in the ADR**, and **M-8's wrong string still in §6 AC-4.1 site 2** — are fixed in ADR 0034
    Context and in §6 AC-4.1 respectively.
  - **Round-3 findings and where each is discharged:** **NEW-1** (MAJOR — `:48-49` is a third instance of the
    arm→literal claim and goes false in **both** clauses) → §6 AC-4.1 site 14, extended to **`:47-49`** with the
    `int`-typed narrowing spelled out; **NEW-2** (MAJOR — the widened selector still under-selects: `:409`,
    `:601`, `:799-800`) → §6 AC-4.1, **16 sites**, site 12 extended to the whole `require.Equal` message, the
    selector replaced by a **deliberately noisy** one, **and** §7 *Out* / §8 item 5 recording that the durable
    fix is to derive the counts from `wantArms` at test time; **NEW-3** (MAJOR — the bundle asserted three
    inconsistent Plan 030 states) → §1's re-derive note, corrected to the delivered state; **NEW-4** (MINOR —
    mutant M3-6 cannot discriminate) → Plan mutant table; **NEW-5** (MINOR — B1-4's 2 MiB fixture is larger and
    weaker than the boundary pair) → §6 AC-1's small-`n` paragraph and Plan constraint 6 / B1-4;
    **NEW-6** (MINOR — AC-1's *"observable"* is unachievable for a ceiling-valued cap) → §6 AC-1, reworded to
    *"its product is usable"* with the unobservability stated; **NEW-7** (MINOR — the narrow sub-check has no
    command) → §6 AC-4.1, sub-check **dropped**. The two smaller notes — D-3's non-wrap-safe count and the
    unnamed over-inclusion lines `:33`/`:521` — land in Plan Step 11 and §6 AC-4.1 respectively.
  - 🔴 **One defect the coordinator found while folding NEW-5 back, not raised by any round.** Revision 3's §6
    AC-1 claimed the small-`n` proof *"already exists and is re-asserted, not invented: `WithMaxBodyBytes(1<<20)`
    + a 64 MiB body → `http: request body too large`."* **It does not exist** —
    `grep -rn 'body too large' --include='*.go' .` returns **zero hits** workspace-wide. The quoted line is a
    **Plan 029 measurement** from §1's benchmark transcript, not a shipped assertion, and a 64 MiB fixture would
    breach Plan Global constraint 6's ≤ 2 MiB bound by 32×. Corrected in §6 AC-1.
  - **Line/offset convention, stated once (m-11).** Every `file.go:NNN` citation in this bundle names the
    **`func` (or `const`/`var`) declaration line**, never a line inside its godoc — except where a specific
    godoc *sentence* is the subject, in which case the sentence's own line is cited and the text is quoted.
  - **This spec closes a class that [Spec 016](016-sizing-option-bounds.md) opened and deliberately left open.**
    It adds no new concept: it reuses ADR 0032's **D-AB** membership criterion, **D-W**'s stated-ceiling
    mechanism, **D-X**'s sentinel-reuse-plus-wrap shape, and the shipped `checkRange` render — all unchanged.
- **Realized by:** [Plan 032](../plans/032-byte-cap-ceilings.md).
- **Decided by:** [ADR 0034](../adrs/0034-byte-cap-ceilings.md).
- **Finishes:** [Spec 016 §3.8](016-sizing-option-bounds.md) / [ADR 0032 D-AB](../adrs/0032-sizing-option-bounds.md),
  which classified these three knobs as **class members with a DEFERRED remedy** and explicitly refused to certify
  them safe. [Plan 029](../plans/029-sizing-option-bounds.md) bounded the other nine.
- **Sibling, not predecessor:** [Spec 017](017-group-member-bounds.md) / [ADR 0033](../adrs/0033-group-member-bounds.md)
  / [Plan 031](../plans/031-group-member-bounds.md) — the *other* increment descending from Spec 016. The two are
  **independent**: 018 touches `adapter/http` and the root class gate; 017 touches `routing`, `adapter/memory` and
  `adapter/database/sql`. They **share exactly one file**, `sizing_option_class_gate_test.go` (§6 AC-4). Whichever
  lands second rebases.
- **Origin:** the triaged backlog item in [`docs/HANDOVER.md`](../HANDOVER.md) §7 item **6**.

---

## 1. The gap

**Three `msghttp` options are the sole bound on a remote-peer-driven read that is retained in memory, and each
accepts a value that is effectively infinite.** They are the last members of Spec 016's class still unbounded.

| Knob | Signature | Default | Accumulates in | Site |
|---|---|---|---|---|
| `msghttp.WithMaxBodyBytes` | `(n int64)` `options.go:463` | `1 << 20` (1 MiB) `options.go:23` | the `[]byte` returned by `io.ReadAll` | `encode.go:102` |
| `msghttp.WithMaxResponseBytes` | `(n int64)` `options.go:767` | `1 << 20` (1 MiB) `options.go:30` | the `[]byte` **retained as the reply payload** | `exchange.go:130-131` |
| `msghttp.WithMaxEventBytes` | `(n int64)` `options.go:856` | `1 << 20` (1 MiB) `options.go:44` | a `bytes.Buffer` and a line `[]byte` | `sse.go:387`, `sse.go:472` |

> **Re-derive every line number before citing it.** The *anchors* below (function names, sentinel names,
> predicate shapes) are what this spec is written over, not the offsets.
>
> 🔴 **Round-3 NEW-3, corrected: `adapter/http/options.go` and `adapter/http/helpers.go` are NOT contested.**
> Revision 3 said they were *"being edited concurrently by Plan 030"*. **[Plan 030](../plans/030-post-029-maintenance.md)
> is fully delivered** — Task 1 at `1a1c135`, Task 2 at `d2c69fe`, Task 3 at `7ab91cd`. Its checkboxes were never
> ticked and two of its three commit subjects omit "030", so both naive signals read as undelivered; **the
> reliable signal is the trailer**, `git log --format='%h %s' --grep='Plan: 030'`. Offsets in this spec are
> post-030 and stable; re-derive them because plans drift, not because a sibling is mid-edit.

Measured by Plan 029 (Go 1.25.13, darwin/arm64), and **not re-run this revision** — cited as a prior measurement,
not as a fresh one:

```
WithMaxBodyBytes(1048576)                 body=64 MiB -> err=msghttp: decode request failed: http: request body too large
WithMaxBodyBytes(4611686018427387904)     body=64 MiB -> err=<nil>                       TotalAlloc delta=375.2 MiB
WithMaxResponseBytes(4611686018427387904) reply=64 MiB -> err=<nil>, 67,108,864 bytes RETAINED   TotalAlloc delta=376.4 MiB
```

### 1.1 What is different about these three — and why it is a REMEDY difference, not a CLASS difference

**This is the crux, and getting it wrong re-opens the census churn that cost Spec 016 five audit rounds.**

The nine knobs Plan 029 bounded fail because **the knob stops meaning what it says**:

| Knob | At `1<<62` the knob… |
|---|---|
| `memory.WithBuffer` | …*panics* — `makechan: size out of range`. The buffer is never created. |
| `memory.WithMaxGroups` | …*stops capping* — `len(s.groups) >= n` is never true, so the cap admits everything. |
| `endpoint.WithConcurrency` | …*corrupts a `WaitGroup`* — `Wait()` returns while workers still run. |

These three fail differently: **the knob does exactly what it says.** `WithMaxBodyBytes(1<<62)` permits bodies up
to `1<<62` bytes, faithfully, and `http.MaxBytesReader` enforces that limit correctly. Nothing lies. The hazard is
that *"effectively infinite" is a reachable configuration*.

**Decision: same class, different remedy.** ADR 0032 **D-AB** defines class membership by a stated criterion —
*"a knob is a CLASS MEMBER iff `n` is the sole bound on an accumulation"* — and that criterion says nothing about
panics, corruption, or whether the knob is honest. All three satisfy it. Splitting them into a second class would
mean either (a) amending D-AB's criterion to add a failure-mode clause, or (b) maintaining two criteria that
partition the same knobs — the hand-maintained-census pattern D-AB exists to kill. Neither is worth buying a
taxonomy that changes no outcome.

> **The cost of (a), stated accurately (round-1 m-12).** Revision 1 said splitting *"re-opens all 19 gate rows"*.
> **As an edit count that is wrong: it moves 3 rows and adds one `byArm` key.** The real cost is not editing —
> it is that amending the membership criterion obliges a **re-check of all 19 rows against the new criterion**,
> and this census took five audit rounds to stabilise. The conclusion stands on that, and does not need the
> inflated figure.

**What genuinely differs is the ceiling's UNIT, and that is where the remedy diverges** (§3.2): D-Z's nine
ceilings are each expressed in the knob's own domain unit (messages, connections, events, goroutines, group
members) and justified by *what the knob means*. A ceiling in **bytes** cannot be justified that way, because the
library does not know the caller's payload. This spec's ceiling is therefore justified in a **different currency
entirely** — representability — and §3.2 is the argument that this is not a rebranded guess.

### 1.2 `drainBounded` covers five of six reads — verified, and the sixth is not a bug

Spec 016 §3.8 asserts that `drainBounded` is *"five of six"* reads of `cfg.maxResponseBytes`. **Re-derived this
revision and TRUE**, on the natural reading of "read" — a site that consumes the cap as a bound on I/O (the option
setter and `NewConfig`'s validator are excluded; they *write* it and *validate* it):

| # | Site | Shape | Retained? |
|---|---|---|---|
| 1 | `sseclient.go:335` | `drainBounded` | no — `io.CopyN(io.Discard, …)` |
| 2 | `sseclient.go:338` | `drainBounded` | no |
| 3 | `sseclient.go:341` | `drainBounded` | no |
| 4 | `outbound.go:370` | `drainBounded` | no |
| 5 | `exchange.go:126` | `drainBounded` (the non-2xx arm) | no |
| **6** | **`exchange.go:130-131`** | **`io.ReadAll(io.LimitReader(resp.Body, max))`** | **YES — it is the reply payload** |

**The sixth read is CORRECT and this spec does not restructure it.** It is not a missing `drainBounded` call: you
cannot discard the body you are contracted to return. `drainBounded` exists to bound a body being *thrown away*;
read 6 bounds a body being *kept*. The "5 of 6" figure is evidence that the safety verdict rested on the wrong
five sites — it is not a to-do list.

**Therefore: this increment BOUNDS the sixth read; it does not FIX it.** After this spec, `max` at
`exchange.go:130` is bounded by the ceiling instead of by `1<<62`, and every one of the six reads inherits the
same bound from the same field. Nothing moves. State this plainly in the ADR and the plan, because *"5 of 6"*
reads like an omission and the next reader will try to close it.

### 1.3 The three sites in detail

1. **`encode.go:102`** — `io.ReadAll(http.MaxBytesReader(nil, body, cfg.maxBody()))`. `MaxBytesReader` returns
   `http.MaxBytesError` past the limit, so the cap is enforced; the result is one contiguous `[]byte`. On the
   **async** path (`ServeAsync`) that slice is retained for as long as the queued message lives — already
   disclosed in the option's `CAVEAT` paragraph.
2. **`exchange.go:130-131`** — `io.ReadAll(io.LimitReader(resp.Body, max))`, retained as the reply payload.

   > 🔴 **The INV-6 probe: the PRODUCTION code is unaffected; its TEST is RETIRED (round-1 B-2).** Revision 1
   > said only the first half, and the omission was a BLOCKER. `exchange.go:133`'s `int64(len(body)) == max`
   > compares an `int`-derived length against the `int64` cap; a bounded `max` is still `int64`, so **the
   > arithmetic does not change**. But the case that exercised that arithmetic at the overflow boundary —
   > `adapter/http/exchange_test.go:613` *"branch 20: `WithMaxResponseBytes(MaxInt64)` returns a non-empty body
   > intact, **the overflow regression**"* — passes `math.MaxInt64` through a `newExchange` helper that
   > `require.NoError`s on construction (`:590-596`). After this increment `math.MaxInt64` is a construction
   > error, so **the ceiling makes the probe's input unreachable through the public API.** The ceiling does not
   > break branch 20; it **retires** it.
   >
   > **What still covers INV-6's arithmetic afterwards:** branch 18 (a body exactly at cap succeeds intact,
   > `:598-604`) and branch 19 (cap+1 → `ErrReplyTooLarge`, `:606-611`) exercise the `==` comparison at its
   > boundary from both sides at a small cap, which is where the comparison's correctness actually lives. What is
   > lost is the *magnitude* probe — the assurance that no arithmetic overflows at `MaxInt64`. That assurance is
   > now supplied structurally instead: no `max` above `byteCapCeiling` can exist, so the overflow band is
   > unreachable rather than merely untested. Branch 20 is rewritten to the ceiling value and its header comment
   > updated to say so (§6 AC-2c).
3. **`sse.go:387` / `sse.go:472`** — the data buffer (`p.dataBuf.WriteString(value)` then
   `if int64(p.dataBuf.Len()) > p.maxEventBytes`) and the line buffer (`buf = append(buf, b)` then
   `if int64(len(buf)) > p.maxEventBytes`). **Both check AFTER the append**, so peak retention is the cap plus one
   line / one byte — immaterial at any ceiling, but stated so nobody "fixes" it into an off-by-one.

   > 🔴 **`WithMaxEventBytes` is PARSE-SIDE ONLY, and no artifact said so before revision 3 (round-2 N-11).**
   > The cap is consulted by `NewSSEParser` (`sse.go:239`) and by the SSE **client** (`sseclient.go:401`, which
   > hands `cfg.maxEventBytes` to `newSSEParserWithCap`). **The SSE *server* never reads it.**
   > `adapter/http/sse_server.go` reads `maxConns()`, `connBuffer()`, `replaySize()`, `heartbeatInterval()`,
   > `perWriteTimeout()` and `slowPolicy()` off the same `*Config` and frames outbound events through
   > `EncodeSSEEvent` into a `bytes.Buffer` with **no size check** — verified:
   > `grep -rn 'maxEventBytes' adapter/http/*.go | grep -v _test` returns no `sse_server.go` hit. The option's
   > own godoc is already correctly scoped (*"caps the number of bytes **NewSSEParser's SSEParser** buffers"*,
   > `options.go:834-836`); what was missing is the design artifacts saying it, so a reader of this spec does not
   > conclude the ceiling bounds "the SSE byte cap" in both directions.
   >
   > **Why the server side is outside ADR 0032 D-AB's class, and therefore outside this increment.** D-AB's
   > criterion is *"a knob is a CLASS MEMBER iff `n` is the sole bound on an accumulation."* On the server path
   > there is **no `n`**: the outbound event's size is whatever the caller passed to `Send`, bounded by the
   > caller's own message, not by an msgin knob. A knob that does not exist cannot be the sole bound, so there is
   > no class member to bound — this is a *scope* statement, not a silent omission. Whether the SSE server
   > *should* grow an outbound-frame cap is a different question, in a different arm; it is **not** opened here.

   > The line buffer grows **one byte per iteration** (`append(buf, b)` inside the read loop). At a 2 GiB ceiling
   > the practical bound on that path is **time**, not bytes — the same "sized by time, not bytes" argument
   > Spec 016 §3.4 makes for `routing.WithCompletionSize`. This is an argument *for* a generous ceiling being
   > harmless, not against it.

---

## 2. The framing — what is actually open

**The open question is NOT "invent an off-state."** [Spec 016 §3.8](016-sizing-option-bounds.md) states it
precisely, and this spec re-verified every clause against the code:

| Claim | Verified | Evidence |
|---|---|---|
| `NewConfig` **rejects** an explicit `n <= 0` | **TRUE** | `options.go:1189-1193`, `:1201-1205`, `:1211-1215` — `if !set { default } else if n <= 0 { return nil, Err… }` |
| Each has its own typed sentinel | **TRUE** | `ErrInvalidMaxBodyBytes` `errors.go:19`, `ErrInvalidMaxResponseBytes` `:77`, `ErrInvalidMaxEventBytes` `:138` |
| Leaving the option unset **is** the documented default | **TRUE** | the `…Set` flag pattern; all three godocs say *"Leaving this option unset … is how a caller asks for the default"* |
| All three defaults are `1 << 20` (1 MiB) | **TRUE** | `options.go:23`, `:30`, `:44` |
| All three take `int64` | **TRUE** | `options.go:463`, `:767`, `:856` |
| They do **not** break `GOARCH=386` | **TRUE**, by a **corrected** command (round-1 M-4) | `GOARCH=386 GOOS=linux go vet ./...` → exit `0`; `GOARCH=386 GOOS=linux go build ./...` → exit `0`. The gate's `1 << 62` fits `int64`. 🔴 **Revision 1 certified `go test -gcflags=all=-e -run=NONE ./...` "verified clean"; that command exits `1` on an untouched tree** (`exec format error`, all 11 packages) — see §6 AC-3 |

So the question is exactly: **should an explicit off-state exist at all, and which sentinel value would carry
it?** `-1` and `0` are already taken by the rejection, so an off-state needs a **new** sentinel value — a named
exported constant, or a magic number. **§3.4b answers: no off-state** — and §3.4a first addresses the shipped
counter-example that revision 1 never mentioned.

### 2.1 One claim in the delivered bundle is FALSE

**Spec 016 §3.8 item 2 promised a godoc change that Plan 029 never delivered.** It states, under *"What this
increment DOES do for them"*:

> *"2. Their **godoc gains the hazard disclosure**: the knob is the only bound on a remote-driven read, the default
> is safe, and a large explicit value removes the protection."*

**No such disclosure exists in any of the three godocs.** Read this revision: `WithMaxBodyBytes` (`options.go:446-462`),
`WithMaxResponseBytes` (`:749-766`), `WithMaxEventBytes` (`:834-855`) each carry a `CAVEAT` about *headers* and a
paragraph about `n <= 0` — neither of which is the promised disclosure. Plan 029 has **no task** for it
(`grep -n 'hazard disclosure' docs/plans/029-sizing-option-bounds.md` → no hits), so it was promised in the spec
and never scheduled. By contrast the *bounded* siblings did get theirs, **each citing a different Spec 016
section** — corrected this revision (round-1 m-11; revision 1 said both cited §1.3, at `:976`):

| Sibling | Declared | Cites | At |
|---|---|---|---|
| `WithMaxConnections` | `options.go:908` | Spec 016 **§1.3** | godoc line `:901` |
| `WithReplayBuffer` | `options.go:986` | Spec 016 **§1.5** | godoc line `:977` |

Both are the shape Plan 032's godoc task copies.

This spec's §4 delivers it — in the superseding form, since after this increment the honest disclosure is
*"bounded at the ceiling, and the ceiling is not a safety guarantee"* rather than *"unbounded"*.

---

## 3. The contract

> **No `msghttp` byte cap may be configured above the largest length a Go `[]byte` can represent on any platform
> this library builds for.** A value outside `[1, byteCapCeiling]` is rejected by `NewConfig` with the knob's own
> typed sentinel, wrapped in Spec 016 §3.1's single rendered shape. There is **no off-state**: the option cannot
> be set to "unbounded", and leaving it unset remains the way to ask for the safe default.

This is Spec 016 §3's contract with the word *"bounded structure"* read literally. No new mechanism, no new
concept, no new exported symbol.

### 3.1 R1 — reject at construction (3 of 3)

All three are validated by `NewConfig`, which returns an error. All three are therefore **R1** in Spec 016's
taxonomy; **none** is R2, so this increment touches no latch and has no ADR 0031 D-U interaction.

| Knob | Checked in | Existing arm | `lo` | `hi` (added arm) |
|---|---|---|---|---|
| `WithMaxBodyBytes` | `NewConfig` `options.go:1189-1193` | `<= 0` → `ErrInvalidMaxBodyBytes` | `1` | `byteCapCeiling` |
| `WithMaxResponseBytes` | `NewConfig` `options.go:1201-1205` | `<= 0` → `ErrInvalidMaxResponseBytes` | `1` | `byteCapCeiling` |
| `WithMaxEventBytes` | `NewConfig` `options.go:1211-1215` | `<= 0` → `ErrInvalidMaxEventBytes` | `1` | `byteCapCeiling` |

Each gate today is Spec 016's **R1-a** shape — `if !set { default } else if <bad> { return nil, sentinel }` — and
each becomes `if !set { default } else if err := checkRangeInt64(sentinel, site, n, 1, byteCapCeiling); err != nil { return nil, err }`.

**The rendered message changes at BOTH arms**, and this is a deliberate, stated behavioral change:

```
before:  msghttp: max body bytes must be > 0
after:   msghttp: max body bytes out of range: msghttp.WithMaxBodyBytes: 0 not in [1, 2147483647]
after:   msghttp: max body bytes out of range: msghttp.WithMaxBodyBytes: 2147483648 not in [1, 2147483647]
```

`errors.Is` is unaffected — the sentinel is the same object, carried through `%w`.

#### 3.1a The test-safety check — the RIGHT one (round-1 B-2)

**Revision 1's check was a grep for MESSAGE STRINGS, and it was structurally blind.** It asked *"does any test
assert the wording?"* (answer: no — the grep returns only the three `errors.go` declarations, and that remains
true). It could not ask the question that matters: **"does any test depend on a value this increment stops
accepting?"** One did, and it was in no task's file list.

**The replacement check enumerates CALL SITES and classifies every one.** Run it, classify each hit, and treat any
unclassified hit as a stop-and-reassess:

```bash
grep -rn 'WithMaxBodyBytes(\|WithMaxResponseBytes(\|WithMaxEventBytes(' --include='*_test.go' .
```

**Classification, RE-DERIVED at revision 3 on the tree at `46803c6` — 49 hits, of which 40 are calls.**

> 🔴 **Revision 2 pasted `48 hits, of which 34 are calls`, and `18` in the fourth class. All three were wrong
> (round-2 N-5); the tree says 49 / 40 / 24.** The load-bearing half was right — there are exactly **four**
> out-of-range call sites and exactly one of them (`exchange_test.go:615`) was unscheduled — but the arithmetic
> around it was not, and **Plan Task 1 Step 2 treats a mismatch as a stop-and-reassess**, so a wrong total
> manufactures a false alarm on a step whose designed response to an alarm is to halt. The fourth class's
> under-count came from collapsing ten `sse_test.go` sites into a `:730`…`:883` ellipsis; they are enumerated
> below.

The **nine non-call hits** — the options' names inside `name:` strings, a `t.Run` name and comments, which no
ceiling affects — are `nil_option_test.go:87`, `outbound_test.go:29`, `:58`, `:66`, `:74`, `encode_test.go:50`,
`:58`, `exchange_test.go:577`, `exchange_test.go:613`. `49 − 9 = 40` calls, which partition as:

| Class | Value(s) | Count | Sites | Effect |
|---|---|---|---|---|
| **OUT OF RANGE — breaks** | `1 << 62` | 3 | `sizing_option_class_gate_test.go:572`, `:581`, `:590` | rejected after this increment. **Intended** — these are the class-gate rows being moved (§6 AC-4) |
| **OUT OF RANGE — breaks** | `math.MaxInt64` | 1 | `adapter/http/exchange_test.go:615` | 🔴 **the BLOCKER.** Rejected after this increment, through a helper that `require.NoError`s. Rewritten to the ceiling value (§6 AC-2c) |
| **IN RANGE — rejected today, still rejected** | `0`, `-1` | 12 | `encode_test.go:51`,`:59`; `outbound_test.go:59`,`:67`,`:420`; `sse_test.go:1031`,`:1039`; `sseclient_test.go:214`; `exchange_test.go:141`; `nil_option_test.go:92`; `stdlib/inbound_test.go:99`,`:159` | unaffected — **every one asserts `ErrorIs`, none asserts rendered text** (verified; the message change is invisible to them) |
| **IN RANGE — accepted, stays accepted** | `1`, `4`, `10`, `16`, `1024`, `2048` | **24** | `encode_test.go:84`,`:151`; `outbound_test.go:75`,`:657`; `sseclient_test.go:1048`; `inbound_test.go:155`,`:395`; `exchange_test.go:600`,`:608`,`:624`,`:637`,`:658`; `sse_test.go:730`,`:743`,`:756`,`:771`,`:786`,`:807`,`:825`,`:845`,`:865`,`:883`,`:1047`,`:1096` | unaffected — all far below the ceiling |

`3 + 1 + 12 + 24 = 40` ✅ — the classes sum to the call count, which is the check revision 2's figures failed.

**Only two of the four classes are affected, and both are now scheduled work.** **Re-derive at implementation
time and paste the output; do NOT compare against the totals above.** Plan 030 has landed and Plan 031 may land
between now and then, so the *counts* are a snapshot and only the *classification rule* is normative: any
out-of-range hit that is not in Plan 032 Task 1's Files list is a stop-and-reassess.

**Not `Permanent`-wrapped**, matching ADR 0029 **D-M** and Spec 016 §3.1: it is a constructor return and never
travels through a `MessageHandler`.

### 3.2 The ceiling — `math.MaxInt32`, justified by WIDTH SAFETY, not by what a payload might be

**Value: `byteCapCeiling int64 = math.MaxInt32` = `2,147,483,647` bytes** (one byte under 2 GiB).

> 🔴 **This section was REWRITTEN in revision 2 (round-1 M-5).** Revision 1's load-bearing sentence — *"A cap
> above that value cannot be honoured anywhere the library builds"* — is **false in both directions**, and the
> whole gate-clearance argument rested on it:
>
> - **False on 64-bit.** `int` is 64-bit on `amd64`/`arm64`. A 3 GiB `[]byte` is entirely representable there,
>   and `WithMaxBodyBytes(3<<30)` is honoured **exactly** today on a machine with the memory for it.
> - **False on 32-bit, oppositely.** On `linux/386` a process has ~3 GiB of user address space and `io.ReadAll`'s
>   doubling peaks near 2× the final size, so a cap **at** `math.MaxInt32` is not obeyable either — the
>   allocation fails long before the length limit binds.
>
> Revision 1 also demoted the one unconditionally true statement (width safety) into a footnote headed *"none is
> the primary argument"*. **It IS the argument.** The corrected version follows; nothing about the *value*
> changes.

**The justification, corrected — and it still never mentions the caller's payload.** All three reads terminate in
a single contiguous in-memory buffer: an `io.ReadAll` `[]byte`, a `bytes.Buffer`'s `[]byte`, an `append`-grown
`[]byte`. A Go slice's `len` and `cap` are of type `int`, whose width is **`GOARCH`-dependent** — 64-bit on
`amd64`/`arm64`, 32-bit on `386`/`arm`/`mips`/`mipsle`.

> **`math.MaxInt32` is the largest value for which the cap is exactly representable as an `int` on EVERY `GOARCH`
> this module builds for — so one configuration is valid on all of them.**

Equivalently: `n <= math.MaxInt32` guarantees `int(n)` is **lossless on every platform**, so a cap that is legal
is also a cap that every build can express as a slice length. Above that value the knob's meaning becomes
architecture-dependent — legal and honourable on 64-bit, inexpressible as a `[]byte` length on 32-bit — and a
configuration whose semantics depend on the deployment's word size is precisely
[Spec 016 §1.1](016-sizing-option-bounds.md)'s inversion in a new costume.

**Stated plainly so no reader is later surprised:** on a 64-bit build, a cap **above** `math.MaxInt32` *can* be
honoured. The ceiling is a **deliberate portability choice** — one number that means the same thing everywhere —
not a claim that larger reads are impossible. A caller who genuinely needs a >2 GiB in-memory payload on 64-bit is
not served by this API shape anyway (§3.4).

**Why this survives CLAUDE.md's Sensible-defaults gate — the objection, stated at full strength first.**

> *The gate says a byte cap cannot be guessed because it "depends on the caller's legitimate payload size."
> Spec 016 deferred these three for exactly that reason. A ceiling is still a number the library picked. Calling it
> a "ceiling" rather than a "default" is a relabelling, not an argument.*

**The objection is correct about ceilings in general and wrong about this one, and the distinction is the VALUE,
not the mechanism.** Test it against a policy ceiling:

| Candidate | Its justification | Verdict |
|---|---|---|
| `1 << 30` (1 GiB) | *"no sane HTTP API sends more than a gigabyte"* | **A guess about the caller's payload wearing a ceiling's clothes.** Fails the gate. |
| `1 << 32` (4 GiB) | *"a round number safely above anything real"* | Same guess, **plus** the cap's meaning would differ by word size — the portability defect above. Fails harder. |
| **`math.MaxInt32`** | *"the largest cap that is exactly representable as an `int` on every `GOARCH` this builds for"* | **A property of the language's `int` on the platforms in scope.** Nothing about the caller. Passes. |

**The argument licenses exactly one MAXIMUM, and that is the honest reading — narrower than revision 2's
(round-2 N-7).** `math.MaxInt32` is not "a large round number": it is the **largest** value at which the
"same everywhere" property holds. Any *higher* number breaks the property outright. Any *lower* number still has
it — the property is *"representable on every `GOARCH`"*, which every value at or below `math.MaxInt32` satisfies
— so what a lower number forfeits is not the property but the **justification**: it re-opens "why this number and
not that one," which is exactly the payload-guess question the deferral was about. Revision 2 wrote *"the only
value at which the property holds"*, which is false as stated and is the sentence §3.5 leans on to reject
`(n int)`; the two decisions must not prop each other up.

**Two further properties, each an independent benefit** (neither is the argument):

1. **Margin.** 2048× the 1 MiB default; ~32× the 64 MiB body Plan 029 exercised.
2. **Distance from the fatal band.** [Spec 016 §1.1](016-sizing-option-bounds.md) measured `runtime.throw`
   (unrecoverable) starting around `2^47` bytes of *requested allocation* on darwin/arm64. `2^31` is **65,536×**
   below it, and `io.ReadAll`'s doubling growth peaks near 2× the final size — ~4 GiB — still inside the
   recoverable regime.

**One shared constant, not three.** D-Z gave each of the nine knobs its own ceiling because each is expressed in
its own unit and justified by what that knob means. These three share a unit (**bytes**) *and* share the entire
justification (**`[]byte` width**), so three constants would encode an independence that does not exist and would
invite three divergent edits. One constant, `byteCapCeiling`, in `adapter/http/options.go` beside the three
`defaultMax*Bytes`.

> **Attacked, and answered:** *should `WithMaxEventBytes` get a lower ceiling? A 2 GiB single SSE event is
> absurd where a 2 GiB file upload is not.* **No.** Any number lower than the representability limit is a guess
> about the caller's event size — precisely the guess the gate forbids and precisely why the item was deferred.
> "Absurd" is a claim about payloads, and payloads are what the library does not know. The **default** (1 MiB) is
> where per-knob opinion belongs, and all three already carry it.

### 3.3 What the ceiling does NOT claim

**It does not make every permitted value safe, and the spec must not be read as saying so.** A 2 GiB read can
still exhaust a container. What the ceiling delivers is narrower and sufficient:

1. **The accumulation is bounded.** After this increment `n` is finite for every configuration, so ADR 0032
   D-AB's criterion — *"`n` is the sole bound on an accumulation"* — is satisfied by a bound that exists. That,
   and only that, is what moves the rows out of the `deferred` arm.
2. **The absurd configuration becomes a construction-time typed error.** A config typo, a units mistake
   (`WithMaxBodyBytes(gigabytes)`), an env var read as `1<<62` — each now fails at `NewConfig` with a message
   naming the value, the site and the range, instead of at an OOM. That is CLAUDE.md's debuggability criterion.
3. **The default keeps doing the real work.** 1 MiB is what protects the caller who configures nothing, and it is
   unchanged.

### 3.4 No off-state — and why the question is closed rather than deferred again

#### 3.4a The strongest counter-example, addressed head-on: `endpoint.WithMaxPayloadBytes` (round-1 M-7)

**This repository already ships a byte cap WITH an off-state, and it is justified by precisely the CLAUDE.md
sentence this spec cites in its own support.** Revision 1 argued "no off-state" from a project-wide stance and
mentioned it nowhere (`grep -c 'WithMaxPayloadBytes'` across the three bundle files → `0`, `0`, `0`). That was the
audit's MAJOR finding, and it is fixed by naming the thing.

```
$ sed -n '138,144p' endpoint/flowcontrol.go
// n <= 0 disables the cap (the default): a library cannot guess a caller's
// legitimate maximum, so the cap is opt-in. Wire adapters consuming UNTRUSTED
// sources SHOULD set it to bound decode-time memory. The live-value (memory) path
// never carries []byte and is unaffected. Payload structural complexity (deep
// nesting) is bounded by the codec, not here — encoding/json returns an error on
// pathologically nested input rather than overflowing the stack.
func WithMaxPayloadBytes[T any](n int) ConsumerOption[T] {
```

It needs **zero new exported surface** to say "unbounded" — `n <= 0` carries it — it is off by default, and the
class gate itself files it in the `safe` arm (`sizing_option_class_gate_test.go:669`, at `math.MaxInt`). So there
is no uniform project stance to argue from, and this spec does not claim one.

**Why `msghttp` differs, and it is not a preference — it is a fact about what the sentinel values already mean:**

| | `endpoint.WithMaxPayloadBytes` | the three `msghttp` byte caps |
|---|---|---|
| Default | **off** — no cap unless the caller sets one | **on** — 1 MiB, protective, unset *is* the safe default |
| Posture | **opt-in hardening**: the caller adds protection | **protective by default**: the caller can only relax it |
| What `n <= 0` means | **"disabled"** — free to carry that meaning, because nothing else claims it | **already taken**: an explicit `n <= 0` is a *typed rejection* today (`options.go:1189-1193`, `:1201-1205`, `:1211-1215`), shipped, tested and godoc'd |
| Cost of an off-state | zero — the sentinel value was unused | a **new exported constant**, since `-1` and `0` are spoken for |

The distinction is therefore about **which sentinel values are free**, not about a doctrine. Where a knob is off
by default, `n <= 0` is idle and can mean "off" for nothing; where a knob is protective by default and already
rejects `n <= 0`, an off-state costs new exported surface whose only purpose is to re-enable the hazard the class
exists to close.

**Disposition: the divergence is ACCEPTED and recorded, not harmonised.** Two byte caps in this workspace have
different off-state semantics, for the stated reason. **Recorded as a follow-up, explicitly out of scope here:**
whether `endpoint.WithMaxPayloadBytes` should itself gain a representability ceiling on its *positive* range
(today it accepts `math.MaxInt` and the gate certifies that as `safe` because the option never validates `n` at
all). That is a different package, a different spec, and a different arm of the class gate — see §7 *Out*.

#### 3.4b The decision

**Decision: `msghttp` gains no way to express "unbounded".** The alternatives, and why each loses:

| Option | Cost |
|---|---|
| `WithMaxBodyBytes(msghttp.Unbounded)`, new exported const | New exported surface on a pre-v1 API being kept deliberately small, whose **only** purpose is to re-enable the hazard the class exists to close. This is [Spec 016 §5](016-sizing-option-bounds.md)'s rejection of `WithUnsafeUnboundedSizing`, verbatim. |
| A magic value (e.g. `-2`, `math.MaxInt64`) | Undiscoverable, undocumentable, and `math.MaxInt64` is precisely what the ceiling rejects. |
| A separate `WithUnboundedBody()` option | Same new surface, plus a second way to configure one field. |
| **No off-state (CHOSEN)** | A caller who genuinely needs >2 GiB in one in-memory payload has no escape. |

**The residual cost is real and is stated, not minimised:** today `WithMaxBodyBytes(1<<62)` is a legal, working
configuration; after this increment it is a construction error. A caller wanting a 3 GiB in-memory body loses that
capability. **That caller is not served by this API shape anyway** — `io.ReadAll` into a message payload is the
wrong tool for multi-gigabyte transfers, and msgin offers no streaming payload type. The right answer for them is
a streaming payload, which is a different increment.

**Free at pre-v1** — no tags, no consumers ([CLAUDE.md](../../CLAUDE.md) "Project status"). And **additive to
reverse**: adding an off-state later is a new const plus a new branch, never a breaking change. Not adding it now
costs nothing that cannot be recovered.

### 3.5 The signature — `(n int64)` is a DECISION, and `(n int)` was tested (round-1 M-6)

**Revision 1 asserted *"No signature change. All three keep `(n int64)`"* as a premise** (§3.6 item 1) and put it
in neither rejected-alternatives table — while four downstream decisions rested on it. Round 1 called that out.
The question is now answered with evidence rather than by omission.

**The case FOR `(n int)` is genuinely strong, and it is stated at full strength first.** Narrowing the parameter
would: delete the need for `checkRangeInt64` entirely (the shipped `checkRange` is used verbatim); **dissolve**
ADR 0034 D-AR(b)'s accepted mutation gap, because with no `int64`→`int` conversion anywhere there is no
truncation mutant to survive; make `WithMaxBodyBytes(1<<62)` a **compile** error on 32-bit rather than a runtime
one; and match `endpoint.WithMaxPayloadBytes(n int)`, this repo's other byte cap. It is free in the
compatibility sense — pre-v1, no tags, no consumers ([CLAUDE.md](../../CLAUDE.md) "Project status").

**It was tried, and it fails on a property this increment cannot give up.**

> **With an `int` parameter and `byteCapCeiling = math.MaxInt32`, the ceiling on a 32-bit build IS `math.MaxInt`
> — so NO `int` literal can exceed it, and the upper arm becomes INEXPRESSIBLE rather than merely untested.**

Measured on a probe module (`GOTOOLCHAIN=go1.25.13`), `func WithMaxBodyBytes(n int)`:

```
$ GOARCH=386 GOOS=linux go build ./...
./p.go:12:23: cannot use 2147483648 (untyped int constant) as int value in argument to WithMaxBodyBytes (overflows)
./p.go:13:23: cannot use 1 << 62 (untyped int constant 4611686018427387904) as int value in argument to WithMaxBodyBytes (overflows)

$ GOARCH=386 GOOS=linux go build ./...     # with the constant-conversion workaround
./p.go:12:14: constant 2147483648 overflows int      # int64(math.MaxInt32)+1 is still a CONSTANT expression
```

**The three consequences, each verified:**

1. **§6 AC-2's `2147483648` literal would not compile on 386**, so §6 AC-3's replacement gate
   (`GOARCH=386 GOOS=linux go vet ./...`, which type-checks test files) would go **red** — undoing exactly what
   Plan 030 just delivered.
2. **No `fixed`-arm literal exists** that is simultaneously compilable on 386 and above the ceiling. The class
   gate's three moved rows would have to become architecture-conditional, splitting a single-file normative gate
   whose "read the whole file" property is the point.
3. Escaping via a genuinely non-constant value requires deliberate signed **runtime overflow**
   (`n := math.MaxInt32; n++` → `-2147483648` on 386), which exercises the **lower** arm under a different
   render. That is a worse test, not a workaround.

**The escape the rejection does NOT survive, named honestly (round-2 N-7): `byteCapCeiling = math.MaxInt32 - 1`.**
Consequence 2 above — *"no `fixed`-arm literal exists that is simultaneously compilable on 386 and above the
ceiling"* — is true **because the ceiling is exactly `math.MaxInt32`**, which on 32-bit is exactly `math.MaxInt`.
Lower it by one and the set stops being empty. Measured on the same probe module:

```
$ cat p.go
const byteCapCeiling int = math.MaxInt32 - 1
func WithMaxBodyBytes(n int) int { return n }
var _ = WithMaxBodyBytes(2147483647)          // the upper arm, as a plain literal

$ GOARCH=386 GOOS=linux go build ./... ; GOARCH=386 GOOS=linux go vet ./...
exit=0                                         exit=0
$ GOARCH=amd64 GOOS=linux go build ./...
exit=0
```

`2147483647` fits an `int` on 386; `2147483648` does not. So at `math.MaxInt32 - 1` **all three of the
consequences above fall**, `(n int)` becomes fully expressible on every `GOARCH`, and `2,147,483,646` is still
exactly representable everywhere — §3.2's width-safety property is untouched. **Neither audit round considered
this, and it means D-AO's *value* and D-AP's *signature* are mutually load-bearing:** the value is used to justify
the signature, while the signature is no argument for the value.

**Decision: keep `(n int64)` — and rest it on the TRADE, not on "the argument licenses exactly one ceiling".**
That phrase is true of the width-safety property's **maximum**, not of the property itself: every value at or
below `math.MaxInt32` is representable everywhere, so a family of admissible ceilings does exist and the
`MaxInt32 - 1` member of it is a real alternative. It loses on three stated costs, and those are the reason:

1. **It forfeits the "largest representable value" story**, which is §3.2's whole rhetorical strength. *"One below
   the largest value representable everywhere"* re-opens the question §3.2 exists to close — why one below and not
   two? The ceiling would stop being a property of the language and become a property of the test suite, which is
   a policy number wearing a new costume.
2. **It shrinks the moved gate rows' magnitude** from `1<<62` (`4611686018427387904`, ~2⁶²) to `2147483647`
   (~2³¹). The rows would still select the out-of-range branch, but the "maximally absurd value" property the
   gate's `safe`-arm block defends would be weakened for the `fixed` arm too.
3. Against those, the `int` shape's gains are real but bounded: it deletes `checkRangeInt64` (~7 lines), dissolves
   D-AR(b)'s **hypothetical** mutant — one that exists only in a mis-implementation nobody has written — and
   matches `endpoint.WithMaxPayloadBytes(n int)`. D-AR(b)'s gap is accepted and *recorded*, which is a cost the
   project already tolerates elsewhere.

The trade is: pay ~7 duplicated lines and one recorded mutation gap, and keep a ceiling whose justification is a
fact about Go's `int` rather than a fact about this test suite. **That is the reason, and it is reversible** — see
[ADR 0034 D-AO](../adrs/0034-byte-cap-ceilings.md)'s REVERSIBILITY line and §8 item 2.

**Two corollaries worth stating, because a future reader will re-open this:**

- **`1 << 30` cannot serve the three moved gate rows under EITHER signature.** `1,073,741,824 <
  2,147,483,647`, so it is **accepted**, not rejected. The `fixed` arm genuinely carries two literals after this
  increment (§6 AC-4).
- **A future retype to `int` would not hide the knobs from the class gate.** Half 1's AST walk accepts either
  width — `sizing_option_class_gate_test.go:234`, `return t.Name == "int" || t.Name == "int64"` — so
  completeness survives the change. That is a fact about the gate, not a licence to make the change.

### 3.5a The helper — `checkRangeInt64`, a sibling; not a generic, and never a conversion

The shipped `checkRange` (`adapter/http/helpers.go:64`) takes `int`:

```go
func checkRange(sentinel error, site string, n, lo, hi int) error
```

**Passing `int(n)` is the bug this spec exists to prevent.** On `GOARCH=386`, `int(int64(1<<62))` truncates
silently — so `checkRange(sentinel, site, int(n), 1, int(byteCapCeiling))` would **accept `1<<62`** on 32-bit
while rejecting it on 64-bit. A guard that admits the value it exists to reject, on exactly the platform where the
value is least serviceable, is the §1.1 inversion for the third time.

**Chosen: a sibling `checkRangeInt64` in the same file**, rendering the identical shape:

```go
func checkRangeInt64(sentinel error, site string, n, lo, hi int64) error
```

| Alternative | Why not |
|---|---|
| Generic `checkRange[T ~int \| ~int64]` | Changes the shape of a helper that exists in **four** independent copies (`endpoint`, `routing`, `adapter/memory`, `adapter/http`), each carrying godoc asserting it mirrors the other three. Either all four change for a need only `msghttp` has, or the four diverge. ADR 0031 **D-R**'s precedent is duplication over abstraction. |
| Widen `checkRange` itself to `int64` | Forces `int64` conversions at the nine shipped `int` call sites — churn on delivered, gate-cleared code, for no benefit. |
| Convert at the call site | The truncation bug above. |

**Cost: ~7 duplicated lines in one file**, and one more helper for a future reader to notice. The render is
byte-identical (`"%w: %s: %d not in [%d, %d]"` — `%d` handles both widths), so every AC-2b-style assertion in the
project carries over unchanged.

### 3.6 What this spec does not change

1. **No signature change — a DECISION, argued in §3.5, not a premise** (round-1 M-6). All three keep
   `(n int64)`. `apidiff`'s exported-surface delta is **0 removals / 0 additions** — `byteCapCeiling` and
   `checkRangeInt64` are unexported.
2. **No default changes.** All three stay at 1 MiB.
3. **No new exported sentinel** (ADR 0032 **D-X**). The three existing ones are reused at both arms.
4. **`exchange.go:130-131` is not restructured** (§1.2). It is bounded by the same field, now finite.
5. **`drainBounded` is untouched.** Its `int64` `max` parameter already accepts the bounded value.
6. **No `time.Duration` knob enters scope** (Spec 016 §3.7.4 — outside the gate by construction).

---

## 4. Documentation contract

Each of the three godocs states, on the option itself: **the range, the ceiling's value, why that ceiling, the
typed error a violation produces, and the hazard the knob bounds** — the last being
[Spec 016 §3.8](016-sizing-option-bounds.md) item 2's undelivered promise (§2.1), rewritten for the post-ceiling
world. Shape, on `WithMaxBodyBytes`:

> `n` must be in `[1, 2147483647]`. `NewConfig` returns `ErrInvalidMaxBodyBytes` outside it, naming the value and
> the range. The upper bound is `math.MaxInt32`: the body is read into a single `[]byte`, whose length is an
> `int`, so no larger cap can be honoured on a 32-bit build. **The ceiling is not a safety guarantee** — this
> option is the only bound on a read driven by a remote peer, and raising it above the 1 MiB default trades
> flood protection for payload size. Leaving the option unset is how a caller asks for the safe default.

Additionally:

1. **`byteCapCeiling`'s own godoc** carries the §3.2 argument (representability, one constant for three knobs,
   why it is not a payload guess), shaped like `maxConnectionsCeiling`'s.
2. **`checkRangeInt64`'s godoc** states why it is a sibling rather than a generic or a conversion (§3.5a), naming
   the 32-bit truncation it exists to prevent **and** why the option is not simply narrowed to `int` (§3.5), and
   mirrors the existing `checkRange` godoc's cross-reference block.
3. **No existing godoc sentence becomes false — and not for one commit either** (round-1 m-14). Six sentences
   describe the pre-ceiling constructor: the three options' `n MUST be > 0` paragraphs (`options.go:458`, `:764`,
   `:851`) and the three sentinel godocs' *"when an explicit … is <= 0"* (`errors.go:14`, `:72`, `:132`). Each is
   *narrowed*, not contradicted, and each is rewritten **in the same COMMIT as the code**, not merely in the same
   edit — revision 1 split them across two commits and left all six false in between. §6 AC-4 makes the single
   commit a requirement. The `CAVEAT` paragraphs (headers, the O1 drain) are unaffected. This is the check
   CLAUDE.md's stored lesson — *"all three fix rounds in Plan 028 were godoc, not logic"* — demands.

   > 🔴 **Preserve `errors.go:132`'s `(and so by NewSSEParser)` clause (round-2 N-11).** The sentence reads
   > *"ErrInvalidMaxEventBytes is returned by NewConfig **(and so by NewSSEParser)** when an explicit
   > WithMaxEventBytes is <= 0."* That parenthesis is the only place the sentinel's godoc tells a caller which
   > constructor they will actually see it from — `NewSSEParser` builds a `Config` internally (`sse.go:239`) — and
   > Plan revision 2's rewrite table had no home for it. The replacement keeps it: *"returned by `NewConfig` (and
   > so by `NewSSEParser`) when an explicit `WithMaxEventBytes` is outside `[1, 2147483647]`."*

4. **`checkRange`'s OWN godoc is amended too, not just the new sibling's** (round-2 N-9). Neither of its
   enumerations becomes literally false — `checkRange` keeps exactly three callers (`options.go:1219`, `:1226`,
   `:1238`), all R1 — but *"each of this package's **three sites**"* (`helpers.go:51`) and *"**All three sites**
   are R1"* (`:57`) both read as **package-wide** statements, and after this increment the package has **six**
   range-checked sizing options across two helpers sitting seven lines apart. Its last paragraph enumerates the
   peer copies (`endpoint`, `routing`, `memory`) and a hypothetical fifth in `adapter/http/stdlib` while saying
   nothing about the sibling immediately below it. Scope the counts (*"the three `int`-typed sites this helper
   serves"*) and cross-reference `checkRangeInt64` — why it exists (the 32-bit truncation) and when to reach for
   it. `checkRangeInt64`'s godoc points back, so the pair reads as a pair from either end.

---

## 5. Alternatives rejected

| Alternative | Why rejected |
|---|---|
| **Keep deferring** | The class gate's `deferred` arm is a standing assertion that an unbounded remote-driven read is conformant. Every month it stays, it reads more like a verdict and less like a placeholder. |
| **A policy ceiling (1 GiB / 4 GiB)** | §3.2: a guess about the caller's payload, which is exactly what CLAUDE.md's Sensible-defaults gate forbids and why Spec 016 deferred. `1<<32` additionally is unrepresentable on 32-bit. |
| **Per-knob ceilings** | §3.2: three knobs, one unit, one justification. Three constants encode a false independence. |
| **A ceiling plus an off-state** | §3.4b: new exported surface whose only purpose is to re-enable the hazard — Spec 016 §5's `WithUnsafeUnboundedSizing` rejection, verbatim. |
| **Lower the defaults instead** | The defaults are already safe (1 MiB) and are not the problem. Lowering them breaks working callers and leaves `1<<62` reachable. |
| **Narrow the options to `(n int)`** (round-1 M-6) | §3.5, **tested**: with `int` **at this ceiling**, the ceiling equals `math.MaxInt` on 32-bit, so no literal can exceed it — §6 AC-2's `2147483648` stops compiling on 386, AC-3's vet gate goes red, and the class gate's three moved rows become architecture-conditional. Strong on every other criterion; loses on the one that matters. |
| **`(n int)` PLUS `byteCapCeiling = math.MaxInt32 - 1`** (round-2 N-7) | §3.5, **tested and it WORKS** — `2147483647` compiles and vets clean on 386 and amd64, so the upper arm is expressible and D-AR(b)'s mutation gap dissolves. Rejected on the **trade**, not on impossibility: it forfeits the "largest value representable everywhere" justification (re-opening "why this number?"), and drops the moved gate rows' magnitude from ~2⁶² to ~2³¹. Named because revision 2's *"the argument licenses exactly one ceiling"* concealed it. |
| **Make `checkRange` generic** | §3.5a: changes four mirrored copies for a need one package has. |
| **`int(n)` into the existing `checkRange`** | §3.5a: silently truncates on `GOARCH=386` and **accepts** the value it exists to reject, on the narrowest platform. |
| **An off-state, on the `endpoint.WithMaxPayloadBytes` model** (round-1 M-7) | §3.4a: there `n <= 0` is idle and free to mean "off"; here it is already a shipped typed rejection, so an off-state costs new exported surface. The divergence is accepted and recorded, not harmonised. |
| **Restructure `exchange.go:130-131` to stream** | §1.2: the sixth read is correct — it produces the reply payload. Streaming payloads are a different increment with a different SPI. |
| **Export the ceiling** (`msghttp.MaxByteCap`) | New exported surface; the wrapped error already names the bound. Spec 016 §5 deferred the same request for the nine. Revisit if a consumer asks. |

---

## 6. Verification

**AC-1 — the ceiling is the boundary, asserted on both sides.** For each of the three knobs, a `table-test` in
`adapter/http` (blackbox `package msghttp_test`) with:
- `NewConfig(WithX(byteCapCeiling))` → nil error, non-nil `*Config`, **and its product is usable**;
- `NewConfig(WithX(byteCapCeiling + 1))` → `errors.Is` the knob's sentinel;
- `NewConfig(WithX(1 << 62))` → the same sentinel.

> 🔴 **"Usable", not "observable" — and the difference is not pedantry (round-3 NEW-6).** Revision 3's third
> clause read *"**and the knob's effect is observable**"*, which **cannot be satisfied for a ceiling-valued cap
> under Plan Global constraint 6**. The ceiling is `2,147,483,647`; the constraint caps every fixture at ~2 MiB.
> So each prescribed observation runs a *small* fixture against a *ceiling-sized* cap, and succeeds **identically**
> under the 1 MiB default and with the option dropped entirely:
>
> | Prescribed observation | Under `WithX(byteCapCeiling)` | Under the 1 MiB default | Option dropped |
> |---|---|---|---|
> | `DecodeRequest` on a small body | succeeds | succeeds | succeeds |
> | `httptest` round-trip, small response | succeeds | succeeds | succeeds |
> | `NewSSEParser` + `Next` on a small event | succeeds | succeeds | succeeds |
>
> Three identical columns is the definition of an unobservable setting. **The ceiling's effect is unobservable by
> construction, and that is ACCEPTED, not a gap:** no legal fixture distinguishes a 2 GiB cap from the 1 MiB
> default — anything above ~2 MiB is forbidden, anything below 1 MiB passes under both. The ceiling is therefore
> proven **at the constructor only** (accepted at `byteCapCeiling`, rejected at `byteCapCeiling + 1` — AC-2's
> upper arm), and the separate property *"the cap caps"* is proven at **small `n`** with a small fixture, where it
> is a fact about the comparison rather than about the ceiling value. **The two together are the whole contract;
> neither alone is.** *"Its product is usable"* is Spec 016 §6's own phrase, it is exactly what the three checks
> establish, and it is the heading [Plan 032](../plans/032-byte-cap-ceilings.md) already uses for this paragraph —
> revision 3 was the only artifact of the three saying *"observable"*.

> 🔴 **The accessor clause was DELETED in revision 2 (round-1 M-10).** Revision 1 required *"and the accessor
> returns the value set"*, which is unsatisfiable: `maxBody()` (`options.go:272`) is **unexported**, and
> `maxResponseBytes`/`maxEventBytes` **have no accessor at all** — they are read as struct fields
> (`exchange.go:130`, `sseclient.go:401`). Plan Global constraint 2 forbids the whitebox escape, and the plan
> already contradicted the spec by saying *"Do not assert an accessor."*
>
> **"Observable effect" is the definition in force**, per Spec 016 §6's *"its product is usable"* for a
> `NewConfig`-only key: `WithMaxBodyBytes` via `DecodeRequest` on a small body; `WithMaxResponseBytes` via an
> `httptest` round-trip; `WithMaxEventBytes` via `NewSSEParser` + `Next` on a small event.

**No test reads 2 GiB.** These three join Spec 016 §6 AC-1's **growth** family: the property *"the cap caps"* is a
fact about the comparison, not about the ceiling value, so it is proven at small `n` and the ceiling is exercised
by the **constructor only**.

**The small-`n` proof is the BOUNDARY PAIR, at the default** — `1<<20` bytes **accepted**, `1<<20 + 1` bytes
**rejected**:

> 🔴 **Revision 3 claimed this proof "already exists and is re-asserted, not invented", citing
> `WithMaxBodyBytes(1<<20)` + a 64 MiB body → `http: request body too large`. IT DOES NOT EXIST.**
> `grep -rn 'body too large' --include='*.go' .` returns **zero hits** across the whole workspace. The quoted
> line is a **Plan 029 benchmark measurement** (transcribed in §1 above), not a shipped assertion — and a 64 MiB
> fixture would breach Plan Global constraint 6's ≤ 2 MiB bound by **32×**, so it could not be written even if
> someone wanted to. Found by the coordinator while folding round-3 **NEW-5** back; not raised by any round.

**Why the pair, and why it is small (round-3 NEW-5).** Revision 3's Plan branch B1-4 proved the default arm with
a **2 MiB** body rejected under the unset cap. That is both larger and weaker than the pair:

| | 2 MiB body, rejected | `1<<20` accepted / `1<<20 + 1` rejected |
|---|---|---|
| What it proves about the default | it lies somewhere in `(0, 2 MiB)` | it is **exactly** `1048576` — the only value satisfying both arms |
| Kills *"delete the default assignment"* (cap reads `0`) | yes — but a **one-byte** body kills it too | yes, via the accept arm |
| Kills `default = 1<<20 - 1` | no | **yes** — the accept arm goes red |
| Kills `default = 1<<20 + 1` | no | **yes** — the reject arm goes green |
| Exercises `MaxBytesReader`'s exact boundary (`encode.go:102`) | no | **yes** |
| Largest allocation | 2 MiB | **1 MiB + 1** |

The pair is strictly stronger, strictly smaller at the peak, and **the same shape already ships in this package**
for the response cap — `adapter/http/exchange_test.go:309-334` runs `defaultCap` accepted / `defaultCap+1` →
`ErrReplyTooLarge`, with `const defaultCap = 1 << 20` at `:309`. Mirror it, do not invent a new form. This also
sets Plan Global constraint 6's real bound: the largest fixture in the increment is **`1 MiB + 1`**, not 2 MiB.

> **This split is mandatory, not a convenience.** Running `WithMaxBodyBytes(byteCapCeiling)` against a real 2 GiB
> body would allocate ~4 GiB at `io.ReadAll`'s doubling peak, in a package whose sibling runs
> `goleak.VerifyTestMain`. It cannot be written.

**AC-2 — the rendered message is true at BOTH ends** (Spec 016 §6 AC-2b). Per knob, **two** `EqualError`
assertions — one at `0` (lower arm) and one at `byteCapCeiling + 1` (upper arm) — each asserting the site name and
the full `[lo, hi]` render, not merely that `errors.Is` matches:

```
msghttp: max body bytes out of range: msghttp.WithMaxBodyBytes: 0 not in [1, 2147483647]
msghttp: max body bytes out of range: msghttp.WithMaxBodyBytes: 2147483648 not in [1, 2147483647]
```

The lower-end case is what would catch a `lo` regression and what makes §3.1's message change enforced rather
than asserted in prose.

**AC-2b — the classification is asserted, not merely claimed** (round-1 M-8). §3.1 and ADR 0034 D-AQ both state
these errors are **not `Permanent`-wrapped** (ADR 0029 D-M) — and revision 1 had **no acceptance criterion
testing it**, which is exactly the untested typed-error branch CLAUDE.md's hot-path rule forbids. Every rejecting
case in AC-1/AC-2 additionally asserts:

```go
assert.False(t, msgin.IsPermanent(err), "R1 constructor error stays bare (ADR 0029 D-M)")
```

This is the shape the class gate's `fixed` arm already carries on eight of its nine rows
(`sizing_option_class_gate_test.go:417`, `:429`, `:440`, `:466`, `:477`, `:488`, `:504`, `:515`; the ninth,
`:455`, asserts `True` because it is the single R2 latch). The three moved rows adopt it too (AC-4 site 2).

**AC-2c — the retired `MaxInt64` overflow probe is rewritten, not deleted** (round-1 B-2). `adapter/http/exchange_test.go`
branch 20 (`:613-620`) passes `math.MaxInt64` through a helper that `require.NoError`s on construction
(`:590-596`). It is rewritten to the ceiling value, and its header comment (`:577-578`) — which today reads
*"`WithMaxResponseBytes(MaxInt64)` returns a non-empty body intact, the overflow regression"* — is updated to name
the ceiling and to record that the `MaxInt64` input is no longer reachable through the public API. §1.3 item 2
states what still covers INV-6's arithmetic afterwards.

> 🔴 **Spell the replacement `math.MaxInt32`, NOT the decimal `2147483647` (round-2 N-10).** `math` is used
> **exactly once** in that file — `grep -n 'math\.' adapter/http/exchange_test.go` → only `:615` — so rewriting
> the call to a bare decimal orphans the import and the package stops compiling
> (`"math" imported and not used`). This is a deliberate, stated exception to Plan Global constraint 2's
> *"tests spell the literal `2147483647` (or `math.MaxInt32`)"* latitude: here only the second spelling is
> viable, and it also reads as the same constant `byteCapCeiling` is defined from.

**AC-3 — the width guard is checked on a 32-bit target.** `checkRangeInt64`'s reason to exist is invisible on
`darwin/arm64`, where `int(1<<62)` does not truncate. The increment therefore builds **and vets** the workspace
for a 32-bit `GOARCH`:

```bash
GOTOOLCHAIN=go1.25.13 GOARCH=386 GOOS=linux go vet ./...     # exit 0 — TYPE-CHECKS TEST FILES
GOTOOLCHAIN=go1.25.13 GOARCH=386 GOOS=linux go build ./...   # exit 0 — non-test code
```

**Both are verified exit-0 on this tree before any edit, and must stay exit-0 after.**

> 🔴 **Revision 1's command was `go test -gcflags=all=-e -run=NONE ./...`, certified "verified clean". It is RED
> on an untouched tree** (round-1 M-4): exit **1**, `FAIL` for all 11 root packages, `exec format error` — the
> 386 binaries compile but cannot execute on darwin/arm64. The bundle said so itself, one paragraph later, without
> reconciling the two. A gate that exits 1 before any edit cannot detect a regression after one: the implementer
> sees the same `FAIL` either way. `go vet` is the usable form because it type-checks `_test.go` files — which is
> the whole point, since the 32-bit exposure lives in test literals — and exits 0.

**AC-3b — the 386 gate is VACUITY-PROBED, because a gate that has only ever passed proves nothing**
(round-2 N-12). Both commands exit 0 on an untouched tree, which is what makes AC-3 a *usable* gate and also what
makes it unproven: the claim is not *"vet exits 0"* but *"vet **type-checks `_test.go` files**, so a 32-bit-only
overflow in a test literal goes red."* Nothing in revisions 1 or 2 demonstrated that. This increment therefore
**plants the probe where the coverage is doubtful**, per the project's stored lesson (*proving a gate FIRES is not
proving it COVERS*):

1. Plant a 32-bit-only overflow in **one root `_test.go`** — e.g. `const _ int = 1 << 40`, or a call passing
   `1 << 62` to an `int` parameter.
2. `GOARCH=386 GOOS=linux go vet ./...` reports **exactly one** failure, naming that file.
3. `GOARCH=amd64 GOOS=linux go vet ./...` stays **clean** — this is what makes it a *32-bit* probe rather than a
   syntax probe.
4. Revert; both return to exit 0.

Record the probe's output in the Evidence block. This is the same discipline §6 AC-7 applies to the docs-link gate
and Plan Task 2 Step 8 applies to the `apidiff` surface diff; AC-3 was the one gate in the bundle without it.

> **The remaining limitation, recorded rather than papered over:** neither command **executes** anything on 386,
> so the truncation mutant — replacing `checkRangeInt64(…, n, …)` with `checkRange(…, int(n), …)` — is caught by
> review and by godoc, **not** by a red test. It still compiles and still passes on 64-bit. **ADR 0034 D-AR(b)
> records this as an accepted gap.** Closing it needs a 386 runner in CI or `qemu`, which is a CI decision outside
> this increment.

**AC-4 — the class gate moves with the class, in ONE commit, over a MECHANICALLY DERIVED site list.**
`sizing_option_class_gate_test.go` files all three in its **`deferred`** arm today; this increment empties it.

**AC-4.0 — one commit, not three** (round-1 B-1, m-14). The production change, its godoc, and the gate move land
**together**. Revision 1 split them across Tasks 1/2/3 and then required, in Task 3 Step 2, that the root suite be
*"already red after Task 1"* — contradicting the plan's own Global constraint 8 (*"each task is a green unit …
no WIP or broken-build commits"*) and CLAUDE.md's per-task-commit pre-authorization, which is conditioned on
green. The gate lives in the **same module** the production change edits; the coupling is real and cannot be
scheduled away. It also closes m-14's six-false-godoc-sentences window.

**AC-4.1 — derive the site list, do not transcribe it** (round-1 B-3, round-2 N-2). Revision 1 listed **7**
sites and **every offset but one was stale** — they predated Plan 030's already-landed conversion
(`d2c69fe`), which moved the rows from `:519/:528/:537` to `:570/:579/:588`. Revision 2 listed **12**. Revision 3
listed **14**. **The count at revision 4 is 16.** This project's stored lesson is *derive move-lists
mechanically*.

> 🔴 **THREE CONSECUTIVE ROUNDS HAVE EACH FIXED THE NAMED SITES WHILE THE SAME DEFECT RETURNED THROUGH NEW ONES
> (round-3 NEW-2). A better `grep` is NOT the remedy — say so plainly rather than raising the count again.**
>
> | Round | Inventory | What it fixed | How it was defeated |
> |---|---|---|---|
> | 1 (B-3) | 7 sites | offsets were stale — *derive, don't transcribe* | the derivation's **predicate** selected on `deferred` while the property is the `fixed` partition |
> | 2 (N-2) | 12 sites | widened the predicate to `"fixed"`, `(9)`, `(3)` … | still a **token enumeration**: `:409`, `:601` and `:799-800` contain none of the seven alternatives |
> | 3 (NEW-1, NEW-2) | 14 sites | added `:26` and `:47` | `:409`, `:601` and `:799-800` **still** missed; and `:47`'s bullet was quoted only to line 1 of 3, leaving `:48-49` unscheduled (NEW-1) |
>
> **The durable defect is structural, not lexical.** The arm partition is restated in roughly **ten** prose
> locations — the header's arm list, the header's arithmetic identity, Plan 030's per-arm literal block, the
> `arm` field's doc comment, two section banners, the `wantArms` rationale comment's illustrative map, and **two
> live `require.Equal` failure messages** — with **NO MECHANICAL LINK to `wantArms`**, which is the map the test
> actually computes from. Nothing fails when one of them drifts; every count in that list is a hand-maintained
> copy of a value already in the file. Widening the token list is what produced the last two findings. The
> follow-up in §7 *Out* / §8 item 5 is the real fix; this inventory is the stop-gap.

**Select DELIBERATELY BROADLY and accept the noise.** Run this against **current `HEAD`**, paste the output into
the task's Evidence block, and classify **every** hit:

```bash
grep -nE 'deferred|DEFERRED|fixed|rejects|safe|1<<30|1<<20|1<<62|9/1/3/6|9 \+ 1 \+ 3 \+ 6|[0-9]+ (class|rows|AST)' \
  sizing_option_class_gate_test.go
```

**104 of the file's 812 lines** on the tree at `a1247d1` — a ten-minute classification pass, and the correct
trade against a defect that has now shipped past three inventories. Its hit set is a **strict superset** of
revision 3's 42-line form: verified, every one of the 29 sites this bundle and round 3 have named is present,
including the four that only the `DEFERRED` (upper-case) and `9/1/3/6` alternatives reach — `:58`, `:565`,
`:758`, `:805`. **Both of those alternatives are retained above for exactly that reason; do not prune them.**

**The over-inclusion account, so the classification pass has guidance rather than a shrug.** These classify as
**no change** in one pass and are the bulk of the noise: the `arm:` field rows at `:412`-`:511` (the nine `fixed`),
`:531` (`rejects`) and `:607`-`:713` (the six `safe`), the `wantArms` entries at `:772`-`:780`, the two
`require.Len` / `astKeys` assertions at `:746`-`:756`, and — 🔴 **round-3, smaller note 2** — the two *individual* lines that
fall outside every range revision 3 named, and which an implementer will otherwise meet with no guidance:

| Line | Text | Classification |
|---|---|---|
| **`:33`** | `//                          in neither "fixed" (not a class member) nor "safe"` | the `rejects` bullet's neither-nor clause for `WithSuccessStatus`. **Still true after the move — no change** |
| **`:521`** | `// M2 (Task 7 review): this row previously sat in the arm labelled "fixed",` | a historical note, **past tense**, recording a round-4 reclassification. **Still true — no change** |

> 🔴 **The narrow revision-2 form is DROPPED, not retained (round-3 NEW-7).** Revision 3 kept it *"only as a
> sub-check"*, quoted its expected 18-line output, and **never gave the command**, so a reader told to *"run it
> too"* could not. A check specified by its **result** rather than its **procedure** cannot be re-derived, which
> is what the surrounding paragraphs forbid. It is also strictly subsumed by the broad form above, and keeping a
> second weaker selector alongside it invites the wrong one to be used. Gone.

**16 distinct edit sites:**

| # | Line(s) | Site | Change |
|---|---|---|---|
| 1 | `:570`, `:579`, `:588` | the three rows' `arm` field | `"deferred"` → `"fixed"`; drop the trailing `// class member, remedy deferred — Spec 016 §3.8` comment |
| 2 | `:571-575`, `:580-584`, `:589-593` | the three rows' `assert` closures | `require.NoError` → `require.ErrorIs` on the knob's sentinel **+** `assert.EqualError` on **the render THAT ROW produces** — each row passes `1 << 62`, so the expected string is `msghttp: max body bytes out of range: msghttp.WithMaxBodyBytes: 4611686018427387904 not in [1, 2147483647]` and its `WithMaxResponseBytes` / `WithMaxEventBytes` twins — **+** `assert.False(t, msgin.IsPermanent(err), …)` (AC-2b) |
| 3 | `:782-784` | `wantArms` entries | `"deferred"` → `"fixed"` |
| 4 | `:803` | the `byArm` count assertion | remove the `deferred` key — see the trap below |
| 5 | `:35` | the header's arm list | the `- "deferred" (3) — accepts 1<<62 …` bullet |
| 6 | `:38` | the header's arithmetic identity | `9 + 1 + 3 + 6 = 19` → `12 + 1 + 6 = 19` |
| 7 | `:55-59` | **Plan 030's per-arm literal block** | 🔴 goes false **twice** — see below |
| 8 | `:401` | the `arm` field's doc comment | the four-arm vocabulary string — **keep `"deferred"` with a tombstone** |
| 9 | `:539-546` | the `---- arm: deferred ----` section banner | the arm no longer exists; the rows join `fixed` |
| 10 | `:547-556` | 🔴 block **one** — *"WHEN §3.8's CEILING LANDS…"* | instructions → record |
| 11 | `:557-568` | 🔴 block **two** — *"THESE THREE ROWS KEEP THE 1<<62 LITERAL"* | revision 1 counted these two blocks as one |
| 12 | `:758`, `:761`, **`:799`, `:800`**, `:801`, `:805` | prose **inside live assertion messages** naming the `9/1/3/6` split — they are arguments to `require.Equal`, not comments | 🔴 **round-3 NEW-2 extends this site.** `:799-801` is **ONE** message, a three-line string concatenation; revision 3 listed lines 3 and 5 of it (`:801`, `:805`) and omitted lines 1 and 2 (`:799`, `:800`) purely because those two happen to contain no selector token. **Edit the MESSAGE, not the lines.** `:799-800` reads *"…not just the per-arm counts: 9 class members fixed here, 1 that rejects…"* — `9` → `12` |
| **13** | **`:26`** | the header's `fixed` bullet — `- "fixed"    (9) — the fault is reported through…` | 🔴 **round-2 N-2, invisible to the narrow grep.** `9` → `12` |
| **14** | **`:47-49`** | Plan 030's `fixed`/`rejects` literal bullet, **in full** — `- "fixed" (9) and "rejects" (1) → 1<<30 = 1,073,741,824. These rows assert / an EqualError against a rendered decimal. 1<<30 fits an int32 yet still / exceeds every ceiling in the codebase (the largest is 1<<20 = 1,048,576),` | 🔴 **round-2 N-2 + round-3 NEW-1 — it goes false in FOUR ways, not two.** See the site-14 block below |
| **15** | **`:409`** | the `fixed` arm's section banner — `// ---- arm: fixed — the 9 class members this increment bounds ----` | 🔴 **round-3 NEW-2.** `9` → `12`. Invisible even to revision 3's widened grep: `fixed` here is **unquoted**, and the selector required `"fixed"` |
| **16** | **`:601`** | the `safe` arm's literal rationale — `// math.MaxInt, NOT the 1<<30 the reject arms use (Plan 030 Task 2):` | 🔴 **round-3 NEW-2.** After the move the reject arms use **TWO** literals — `1<<30` (the 9 `int`-typed rows) and `1<<62` (the 3 `int64`-typed ones). Narrow the sentence to *"NOT the `1<<30`/`1<<62` the reject arms use"*, or restate it as *"NOT any reject-arm literal"*. The paragraph's **substance** — why `safe` may not be demoted to an int32 value — is correct and must survive verbatim |

**Also classify, do not skip:** `:766` carries `map[string]int{"fixed": 9, ...}` inside the comment explaining why
`wantArms` is a mapping rather than a count. It is illustrative, not normative, so it may stay — but it must be
*decided*, not missed.

> 🔴 **Site 14 goes false in FOUR independent ways, and revision 3 scheduled only two of them (round-3 NEW-1).**
> The bullet does not end at *"…against a rendered decimal"* where revision 3's quotation stopped; it runs two
> lines further:
>
> ```
> 47: //   - "fixed" (9) and "rejects" (1) → 1<<30 = 1,073,741,824. These rows assert
> 48: //     an EqualError against a rendered decimal. 1<<30 fits an int32 yet still
> 49: //     exceeds every ceiling in the codebase (the largest is 1<<20 = 1,048,576),
> ```
>
> | # | Clause | Goes false because |
> |---|---|---|
> | 1 | the count `(9)` | the arm becomes **12** *(scheduled since revision 3)* |
> | 2 | `→ 1<<30` as the arm's single literal | 3 of the 12 sit at `1<<62` *(scheduled since revision 3 — this is site 7's defect one bullet higher)* |
> | 3 | **`1<<30` … "exceeds every ceiling in the codebase"** | 🔴 **NEW** — `1,073,741,824 < byteCapCeiling = 2,147,483,647`. After this increment there is a ceiling `1<<30` does **not** exceed, which is exactly why the corollary below says `1<<30` cannot serve the three moved rows |
> | 4 | **"(the largest is `1<<20` = 1,048,576)"** | 🔴 **NEW** — `1<<20` stops being the largest ceiling the moment `byteCapCeiling` is declared |
>
> **Required replacement wording, not merely "edit the count":**
>
> - Narrow clause 3 to *"exceeds every **`int`-typed** ceiling in the codebase (the largest is `1<<20` =
>   1,048,576)"* — which is **exactly the narrowing this spec already uses one screen below**, in the
>   two-dimensional invariant's *"`int` → `1<<30` (fits int32, exceeds every **`int`-typed** ceiling…)"*.
>   Leaving `:48-49` unnarrowed would put the true, narrowed statement and the false, broad one **seven lines
>   apart in the same header block, describing the same literal** — CLAUDE.md's stored lesson *docs can
>   contradict the code they describe*, with the correct wording already present in the file.
> - **State the reason inline** so the next reader does not re-broaden it: *"`byteCapCeiling` is an `int64`
>   ceiling above `1<<30`; that is why the three `int64`-typed rows keep `1<<62`."*
> - Clause 4's parenthetical is an **enumeration**, and this project's stored lesson is *assert the invariant, not
>   the enumeration.* Either name `byteCapCeiling` as the largest ceiling and `1<<20` as the largest `int`-typed
>   one, or delete the parenthetical.

> 🔴 **Site 4 is the trap.** `byArm` is built by counting (`:793` `byArm := map[string]int{}`, `:797`
> `byArm[tc.arm]++`), so an emptied arm has **no key at all** — writing `{"fixed": …, "deferred": 0, …}` fails,
> because Go's counting map never produces a zero entry. The `deferred` key must be **removed**, not zeroed.
>
> 🔴 **Site 7 is the one revision 1 missed entirely, and it goes false in TWO independent ways.** Plan 030's
> header block declares the literal to be a function of the **arm**: *"THE OVERSIZED LITERAL IS NOT ONE VALUE — IT
> IS THREE, **BY ARM**"* — `fixed`/`rejects` → `1<<30`, `deferred` → `1<<62`, `safe` → `math.MaxInt`.
> **(a)** After the move, `fixed` holds 12 rows carrying **two** literals (9 at `1<<30`, 3 at `1<<62`), so the
> arm→literal mapping is no longer true of any arm. **(b)** Its *"deferred (3) → still 1<<62"* bullet loses its
> referent.
>
> 🔴 **The replacement invariant is TWO-DIMENSIONAL. Write it that way — revision 2's one-dimensional form was
> FALSE and actively dangerous (round-2 N-1, the BLOCKER).** Revision 2 wrote *"the literal is chosen by the
> option's PARAMETER TYPE, not by its arm — an `int` parameter takes `1<<30`, an `int64` parameter takes `1<<62`,
> both then select the out-of-range branch."* **That is false of six of the file's nineteen rows.** The entire
> `safe` arm is `int`-typed —
>
> ```
> resilience/breaker.go:51       func WithBreakerThreshold(n int) CircuitBreakerOption
> endpoint/flowcontrol.go:144    func WithMaxPayloadBytes[T any](n int) ConsumerOption[T]
> endpoint/flowcontrol.go:166    func WithPollMaxBatch[T any](n int) ConsumerOption[T]
> resilience/ratelimit.go:42     func NewTokenBucket(rps float64, burst int, …)
> adapter/memory/queuestore.go:182  func (s *QueueStore) Claim(_ context.Context, max int) …
> channel/queuechannel.go:50     func (q *QueueChannel) Poll(ctx context.Context, max int) …
> ```
>
> — and **every one passes `math.MaxInt` and asserts the value is ACCEPTED** (`:644`, `:669`, `:691`, `:704`,
> `:723`, and `endpoint.WithPollMaxBatch`'s row). There is no out-of-range branch for them to select. Applying
> the one-dimensional rule literally demotes all six to `1<<30` — **an int32 value** — which is precisely what
> the block at `:61-77` forbids: *"1<<30 IS an int32 value, so demoting these rows to it would leave every
> assertion green while the int32-truncation probe silently stopped running."* A silently-green, silently
> non-probing gate is worse than the stale offsets B-3 found, because nothing fails.
>
> **The correct statement, and the one to write into the header block:**
>
> 1. **The ARM fixes the required PROPERTY first.** `safe` → the value must be **ACCEPTED** and must stay
>    maximally absurd, so it is `math.MaxInt` and nothing else. `fixed` and `rejects` → the value must be
>    **OUT OF RANGE** and must render an **architecture-independent decimal**.
> 2. **Only WITHIN the reject arms does the PARAMETER TYPE choose the literal.** `int` → `1<<30` (fits int32,
>    exceeds every `int`-typed ceiling, renders `1073741824` on every architecture); `int64` → `1<<62` (in range
>    on every architecture, renders `4611686018427387904`).
>
> **Carry `:61-77`'s "do not demote these rows" warning forward verbatim** — generalise it, do not replace it.
>
> 🔴 **`1 << 30` CANNOT be used for the three moved rows.** `1,073,741,824 < byteCapCeiling = 2,147,483,647`, so
> it would be **accepted** and every `require.ErrorIs` would fail. The three rows keep `1 << 62` — which is
> dimension 2 doing its work, since these three are the `int64`-typed members of a reject arm. §3.5 records that
> this holds under an `int` signature too, at this ceiling — indeed more sharply, since on 386 no `int` literal
> exceeds `math.MaxInt32` at all.
>
> 🔴 **Sites 10 and 11 are what a mechanical row-move misses.** Block one reads *"WHEN §3.8's CEILING LANDS, THIS
> GATE WILL GO RED, AND THAT IS CORRECT … the repair is to MOVE the row into the `fixed` arm. Do NOT weaken the
> production check."* **This increment IS that event.** Both blocks are rewritten from *instructions* into a
> *record*; the warning about weakening the check is **generalised, not deleted**.

**AC-4.2 — express the counts as DELTAS, because Plan 031 changes the absolutes** (round-1 M-9).
[Plan 031](../plans/031-group-member-bounds.md) / [ADR 0033](../adrs/0033-group-member-bounds.md) **D-AL** edits
the same file by hand, taking `sizingConformanceKeys` from 17 to 19 keys and the partition to
`11 fixed + 1 rejects + 3 deferred + 6 safe = 21 rows`. Revision 1 stated `require.Len(t, tests, 19)`,
`sizingConformanceKeys` and `{"fixed": 12, "rejects": 1, "safe": 6}` as **normative literals** — all three of
which Plan 031 falsifies. "Whichever lands second rebases" is adequate for a textual conflict and inadequate when
the second lander's acceptance criteria *are* the literals.

**This increment's effect on the gate, stated as deltas — true under either landing order:**

| Quantity | Delta |
|---|---|
| `require.Len(t, tests, N)` | **+0** — no row added or removed; three change arm |
| `sizingConformanceKeys` | **unchanged by 032** — this increment adds and removes no key |
| `byArm["deferred"]` | **the key is REMOVED** (3 → absent) |
| `byArm["fixed"]` | **+3** from whatever it then is |
| `byArm["rejects"]`, `byArm["safe"]` | **+0** |
| half 1 (the AST completeness walk) | **unchanged** — the three functions still exist, still `int64` (`:234` accepts `int` or `int64` either way) |

**Ordering, stated explicitly. Verified against Plan 031 revision 2: it adds BOTH its new rows to the `fixed`
arm** (Plan 031 Task 1 Step 2, *"conformance row to the **`fixed`** arm"*) **and touches the `deferred` arm not at
all** — so the `deferred` key is removed under **either** order, and the two increments converge on the same end
state:

| Order | 032 writes | End state |
|---|---|---|
| **032 first, then 031** | `{"fixed": 12, "rejects": 1, "safe": 6}`, `require.Len(…, 19)` | 031 then rebases: `fixed` 12 → 14, `Len` 19 → 21, `sizingConformanceKeys` 17 → 19 |
| **031 first, then 032** | `{"fixed": 14, "rejects": 1, "safe": 6}`, `require.Len(…, 21)` | same — `sizingConformanceKeys` is already 19 |

> **Re-derive before relying on this.** Plan 031 is undelivered and under concurrent revision; confirm its arm
> assignment with `grep -n 'arm' docs/plans/031-group-member-bounds.md` before Task 1 Step 7. If a future
> increment *does* place members in `deferred`, the key survives and only 032's three leave — which is exactly why
> D-AS keeps the arm name as a **tombstone** rather than deleting the concept.

**AC-4.2b — PLAN 032 OWNS SPEC 016 §2.1 UNCONDITIONALLY, and RE-DERIVES it from the tree** (round-2 N-4).

> 🔴 **Revision 2's "whichever lands second writes it" protocol is UNILATERAL and cannot work.** It binds a
> sibling increment that never agreed to it, and does not exist in that sibling's plan:
> `grep -n 'Spec 016 §2.1' docs/plans/031-group-member-bounds.md` → **no hits**;
> `grep -c '016-sizing' docs/adrs/0033-group-member-bounds.md` → **0**. Plan 031 has **ten** tasks and **none** is
> a Spec 016 fold-back. Both orders therefore fail: if 032 lands first it writes only its own rows and 031 lands
> with no task to finish the table, leaving §2.1 permanently recording a partition that matches neither end
> state; if 031 lands first it writes nothing, and 032 — believing itself "the second lander" — must write 031's
> rows by **transcribing numbers out of a plan**, which is exactly the hand-typed-total failure two audit rounds
> have already caught on this file.

**The rule, replacing it:** Plan 032's fold-back task updates [Spec 016](016-sizing-option-bounds.md) §2.1's
census line and arm table **itself, in every landing order**, and it does so by **re-deriving the partition from
the tree at fold-back time** — reading the gate file's `wantArms` mapping and `byArm` assertion **as they then
stand**, after this increment's own edits — rather than from any pre-computed count in this spec or in Plan 031.
That is correct under either order by construction, needs no knowledge of the sibling's numbers, and is the only
formulation that does not re-import the transcription hazard.

The delta table above stays, and is what makes the re-derivation safe: it says what *this* increment changes,
without asserting what the absolutes are.

**Deleted with this rule:** any instruction to "amend only the rows this increment owns", and any instruction to
record that the other increment's rows are pending. Both contradict unconditional ownership.

**Retire the arm name, or keep it as a tombstone?** Keep `"deferred"` in the `arm` field's documented vocabulary
with an explicit *(no members as of Plan 032 — see Spec 018)* tombstone, mirroring how ADR 0032 D-AB retained
safety causes **(c)** and **(d)** as tombstones so the surviving letters keep their identifiers. Delete it from
the counts map (it must be absent) and from the header's arm list. **Do not delete the concept** — a future knob
whose remedy is genuinely deferred needs the arm back, and the tombstone tells the next author it existed.

**AC-5 — the parent artifacts are amended in TASK 1's COMMIT, with the code and the gate move.**
[Spec 016](016-sizing-option-bounds.md) §2.1's census line and arm table, §3.8, §6 AC-5's arm table, and
[ADR 0032](../adrs/0032-sizing-option-bounds.md) **D-AB** all record a `deferred` arm of 3. Each is amended to
point here. `docs/HANDOVER.md` §7 item 6 is closed. **A bundle that changes a knob's class without moving every
row that records it is the "stopped ONE FILE SHORT" failure Spec 016 revision 6 opened with** — the cross-file
grep guard that finds them all in one command rides in the same task, and is **run**, with its output pasted.

> 🔴 **Revision 2 said "the same commit" and scheduled them one commit LATER — m-14's defect relocated
> (round-2 N-6).** Revision 1 left six *godoc* sentences false between two commits; revision 2 closed that by
> merging Tasks 1-3, then put Spec 016 / ADR 0032 / HANDOVER in Task 2, leaving **six artifact statements** false
> across the same gap: §2.1's census line (`9 fixed + 3 deferred + 4 safe`), §2.1's three byte-cap rows, §3.8's
> deferral, §6 AC-5's arm table, D-AB's *"refuses to certify them safe"*, and HANDOVER §7 item 6's open state.
> These are **normative** — ADR 0034 D-AS's own REVERSIBILITY line says *"Spec 016 §2.1 and §6 AC-5 fix every
> key's arm, so moving a row is a **spec change**"* — so the window is worse than the godoc one, not better. It
> also violates CLAUDE.md's *"couple plans and ADRs with the code that realizes them — one coherent commit"*
> directly.
>
> **Task 1 carries them. The second task is gates and the status flip only.** This composes with AC-4.2b: Task 1
> is exactly where the gate reaches its final state, so it is also the only place a fold-back can re-derive §2.1's
> arm table from the tree rather than from a prediction.

**AC-6 — every new branch is covered and mutation-proven.** Per CLAUDE.md's test-coverage gate and the project's
standing rule that *a killed mutant is the evidence, not a green run*. Enumerated per task in
[Plan 032](../plans/032-byte-cap-ceilings.md).

**AC-7 — vacuity probes, on every gate this increment relies on.** Both arms of the docs-link gate are run over
the **six** new files (this spec, ADR 0034, Plan 032, [`docs/plans/032-audit-round-1.md`](../plans/032-audit-round-1.md),
[`docs/plans/032-audit-round-2.md`](../plans/032-audit-round-2.md) and
[`docs/plans/032-audit-round-3.md`](../plans/032-audit-round-3.md)) **and** proven non-vacuous by planting a
bad link and a bad anchor in one of them (not in root — the Plan 028 blindness came from probing only root) and
confirming exactly one hit each, which disappears on revert.

**Three further gates carry a probe, and every gate in the bundle is now covered by one** (round-2 N-12): the
**386 vet gate** (§6 AC-3b — plant a 32-bit-only overflow in a root `_test.go`), the **`apidiff` / exported-symbol
diff** (probed in `adapter/http`, not root — Plan Task 2), and the **class gate's own assertions** (Plan mutants
M3-3 and M3-6). A gate that has only ever been observed passing is recorded as unproven, not as clean.

---

## 7. Scope

**In:** `byteCapCeiling`; `checkRangeInt64`; the three upper arms in `NewConfig`; the three sentinel messages
(`must be > 0` → `out of range`); the three options' godoc, the three sentinel godocs (including Spec 016 §3.8
item 2's undelivered disclosure) **and** `checkRange`'s own godoc (§4 item 4); the rewrite of
`adapter/http/exchange_test.go` branch 20 (§6 AC-2c); the class-gate arm move over the **16**-site inventory
(§6 AC-4.1); the 386 gate's vacuity probe (§6 AC-3b); the fold-back into Spec 016 / ADR 0032 / HANDOVER, **owned
unconditionally and re-derived from the tree** (§6 AC-4.2b).

**Out:** any signature change (`(n int)` considered and rejected — §3.5); **lowering the ceiling to
`math.MaxInt32 - 1` to make `(n int)` viable** (§3.5, §5 — tested, works, rejected on the trade); any default
change; any off-state (§3.4b — decided, not deferred); a representability ceiling for
`endpoint.WithMaxPayloadBytes` (§3.4a — recorded as a follow-up, different package and different gate arm);
**an outbound-frame cap for the SSE *server*** (§1.3 item 3 — `WithMaxEventBytes` is parse-side only and the
server has no `n`, so there is no D-AB class member there to bound); restructuring `exchange.go:130-131` (§1.2);
streaming payload types; `time.Duration` knobs; a 386 CI runner or `qemu` to close D-AR(b)'s gap (§6 AC-3);
**deriving the class gate's prose counts from `wantArms` at test time** (§8 item 5, round-3 NEW-2 — a refactor of
a delivered gate, and the actual fix for a defect three audit rounds have each patched at the instance level);
`docs/HANDOVER.md` §7 items 3, 4, 5, 7, 8.

**Multi-instance / deployment topology** (CLAUDE.md's mandatory statement, even where N/A): `byteCapCeiling` is a
**per-process compile-time constant** guarding a **per-process, per-request allocation**. Nothing here holds state
spanning a message's lifetime, crosses an instance boundary, or needs coordination — no correlation, no lease, no
consumer group, no dedup store. Under N horizontally-scaled instances the worst case is N concurrent bounded
reads, which is the operator's capacity concern and not something the library can police. No SPI seam is affected;
no future distributed pattern (Return Address, durable lock, consumer groups) is constrained.

---

## 8. Decisions taken without ratification — the four worth a second look

Every decision in [ADR 0034](../adrs/0034-byte-cap-ceilings.md) (**D-AM** … **D-AT**) was taken by the coordinator
with the user absent. These four change the shape or the cost of the increment:

| # | Decision | If reversed |
|---|---|---|
| 1 | **D-AN — no off-state** (§3.4b) | A caller loses the ability to configure >2 GiB. Reversing means one exported const + one branch, **additive**, and can happen after this lands. This is the decision most likely to be contested — and §3.4a now records the shipped counter-example (`endpoint.WithMaxPayloadBytes`) it must be argued against. |
| 2 | **D-AO — `math.MaxInt32`, one shared constant** (§3.2) | The value is the whole argument. Any *higher* number makes the cap's meaning word-size-dependent; any *lower* number keeps the width-safety property but forfeits the justification, re-opening "why this number?". 🔴 **Round-2 N-7: lowering it by ONE — `math.MaxInt32 - 1` — would also re-open D-AP, because `(n int)` becomes fully expressible on 386** (tested; `2147483647` compiles and vets clean everywhere). The value and the signature are mutually load-bearing; do not change one without re-deciding the other. Reversing to per-knob constants is three lines but forfeits the single justification. |
| 3 | **D-AM — same class, different remedy** (§1.1) | Splitting the class moves 3 rows and adds one `byArm` key, but obliges a re-check of all 19 rows against an amended criterion (m-12 corrected the inflated figure). |
| 4 | **D-AP — `(n int64)` kept; `checkRangeInt64` a sibling, not a generic** (§3.5, §3.5a) | **Now carries the `(n int)` analysis round 1 demanded, and round 2's qualification of it.** At `byteCapCeiling = math.MaxInt32`, narrowing to `int` makes the upper arm inexpressible on 32-bit — tested. **At `math.MaxInt32 - 1` it does not** (round-2 N-7), so the rejection rests on the *trade* (§3.5's three costs), not on impossibility. Reversing means re-doing §6 AC-2/AC-3/AC-4 **and** re-deciding D-AO's value. |

**Also open, and NOT decided here:**

1. **The defaults (1 MiB × 3).** Safe and out of scope, but a caller who raises one to 100 MiB gets no warning —
   the ceiling does not help them. A godoc-and-observability question for a later increment.
2. **A representability ceiling for `endpoint.WithMaxPayloadBytes`** (§3.4a). It accepts `math.MaxInt` today and
   the class gate certifies that `safe` because the option never validates `n` at all. Different package,
   different arm; recorded so it is not rediscovered.
3. **Closing D-AR(b)'s mutation gap** with a 386 CI runner or `qemu` (§6 AC-3). A CI decision, not a code one.
4. **An outbound-frame cap for the SSE server** (§1.3 item 3, round-2 N-11). `WithMaxEventBytes` is parse-side
   only; `sse_server.go` frames outbound events through `EncodeSSEEvent` into a `bytes.Buffer` with no size
   check. There is no D-AB class member there — no `n` exists to be the sole bound — so this is a *new knob*
   question, not a ceiling question. Recorded so it is not rediscovered as an omission in this increment.
5. 🔴 **FOLLOW-UP, and the one that matters most for the next increment: derive the class gate's prose counts
   from `wantArms` at test time** (round-3 **NEW-2**). `sizing_option_class_gate_test.go` restates the arm
   partition in roughly **ten** prose locations — the header's arm list and arithmetic identity, Plan 030's
   per-arm literal block, the `arm` field's doc comment, two section banners, the `wantArms` rationale comment's
   illustrative map, and **two live `require.Equal` failure messages** — none of them mechanically linked to the
   `wantArms` map the test already computes from. Nothing fails when one drifts, which is why **three
   consecutive audit rounds each fixed the named sites and were each overtaken by new ones** (B-3 → 7 sites,
   N-2 → 12, NEW-1/NEW-2 → 16). The fix is not a wider `grep`: **format the two assertion messages from
   `byArm`/`wantArms`** instead of spelling the partition as a string literal, and reduce the header block's
   counts to a one-line pointer at the assertion. Then a partition change breaks exactly one place and no
   derivation is needed at all. **Out of scope here** — it is a behavior-preserving refactor of a delivered,
   gate-cleared test, and this increment must not also restructure the file it is moving rows inside — but it is
   recorded as a backlog item so a fourth round does not find a seventeenth site.
