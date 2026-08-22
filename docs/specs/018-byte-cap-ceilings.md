# Spec 018 — A byte cap carries a representability ceiling, and no off-state

- **Status:** **PROPOSED — revision 2, post-audit-round-1, NOT accepted.** Written before any code, per
  [CLAUDE.md](../../CLAUDE.md)'s design-time gate. **Round 1 of the adversarial design audit has run** over the
  assembled bundle (this spec + [ADR 0034](../adrs/0034-byte-cap-ceilings.md) +
  [Plan 032](../plans/032-byte-cap-ceilings.md)) and returned **NOT SAFE TO IMPLEMENT** — 3 BLOCKERs, 7 MAJORs,
  4 MINORs. The record is [`docs/plans/032-audit-round-1.md`](../plans/032-audit-round-1.md) (immutable); this
  revision folds every finding back. **Round 2 has not run.** Two rounds is this project's norm; Plan 029
  needed five.
  - 🔴 **Every decision here was taken by the coordinator WITHOUT USER RATIFICATION.** The user was not asked.
    Each is **explicitly reversible**; §8 lists the four that most deserve a second look, and each ADR 0034
    decision carries a **REVERSIBILITY** line stating what undoing it costs.
  - **Round-1 findings and where each is discharged:** B-1/m-14 → §6 AC-4 + Plan Task 1 (one commit);
    B-2 → §1.3 item 2, §3.1, §6 AC-2c; B-3 → §6 AC-4 (mechanical inventory);
    M-4 → §2, §6 AC-3; M-5 → §3.2; M-6 → §3.5, §5; M-7 → §3.4; M-8 → §6 AC-2b/§6 AC-4;
    M-9 → §6 AC-4 (deltas + ordering); M-10 → §6 AC-1; m-11 → §2.1; m-12 → §1.1; m-13 → Plan constraint 6.
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

> **Re-derive every line number before citing it.** `adapter/http/options.go` and `adapter/http/helpers.go` are
> being edited concurrently by Plan 030; the *anchors* below (function names, sentinel names, predicate shapes)
> are what this spec is written over, not the offsets.

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

**Classification against the tree at `1212c63` — 48 hits, of which 34 are calls** (the remainder are the options'
names inside `name:` strings and comments, which no ceiling affects):

| Class | Value(s) | Count | Sites | Effect |
|---|---|---|---|---|
| **OUT OF RANGE — breaks** | `1 << 62` | 3 | `sizing_option_class_gate_test.go:572`, `:581`, `:590` | rejected after this increment. **Intended** — these are the class-gate rows being moved (§6 AC-4) |
| **OUT OF RANGE — breaks** | `math.MaxInt64` | 1 | `adapter/http/exchange_test.go:615` | 🔴 **the BLOCKER.** Rejected after this increment, through a helper that `require.NoError`s. Rewritten to the ceiling value (§6 AC-2c) |
| **IN RANGE — rejected today, still rejected** | `0`, `-1` | 12 | `encode_test.go:51`,`:59`; `outbound_test.go:59`,`:67`,`:420`; `sse_test.go:1031`,`:1039`; `sseclient_test.go:214`; `exchange_test.go:141`; `nil_option_test.go:92`; `stdlib/inbound_test.go:99`,`:159` | unaffected — **every one asserts `ErrorIs`, none asserts rendered text** (verified; the message change is invisible to them) |
| **IN RANGE — accepted, stays accepted** | `1`, `4`, `10`, `16`, `1024`, `2048` | 18 | `encode_test.go:84`,`:151`; `outbound_test.go:75`,`:657`; `sse_test.go:730`…`:883`,`:1047`,`:1096`; `sseclient_test.go:1048`; `inbound_test.go:155`,`:395`; `exchange_test.go:600`,`:608`,`:624`,`:637`,`:658` | unaffected — all far below the ceiling |

**Only two of the four classes are affected, and both are now scheduled work.** Re-run the classification at
implementation time and paste the output: Plan 030 has landed and Plan 031 may land between now and then.

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

**The argument licenses exactly one ceiling, not a family of them.** That is the honest reading: `math.MaxInt32`
is not "a large round number" — it is the *only* value at which the "same everywhere" property holds. One lower
forfeits it for nothing; one higher breaks it.

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

**Decision: keep `(n int64)`.** The `int` shape trades a hypothetical mutant — one that exists only in a
mis-implementation nobody has written — for the loss of a real, uniform, cross-architecture assertion. D-AR(b)'s
gap is accepted and *recorded*; an inexpressible upper arm could not even be recorded, because there would be
nothing to write down.

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

---

## 5. Alternatives rejected

| Alternative | Why rejected |
|---|---|
| **Keep deferring** | The class gate's `deferred` arm is a standing assertion that an unbounded remote-driven read is conformant. Every month it stays, it reads more like a verdict and less like a placeholder. |
| **A policy ceiling (1 GiB / 4 GiB)** | §3.2: a guess about the caller's payload, which is exactly what CLAUDE.md's Sensible-defaults gate forbids and why Spec 016 deferred. `1<<32` additionally is unrepresentable on 32-bit. |
| **Per-knob ceilings** | §3.2: three knobs, one unit, one justification. Three constants encode a false independence. |
| **A ceiling plus an off-state** | §3.4b: new exported surface whose only purpose is to re-enable the hazard — Spec 016 §5's `WithUnsafeUnboundedSizing` rejection, verbatim. |
| **Lower the defaults instead** | The defaults are already safe (1 MiB) and are not the problem. Lowering them breaks working callers and leaves `1<<62` reachable. |
| **Narrow the options to `(n int)`** (round-1 M-6) | §3.5, **tested**: with `int`, the ceiling equals `math.MaxInt` on 32-bit, so no literal can exceed it — §6 AC-2's `2147483648` stops compiling on 386, AC-3's vet gate goes red, and the class gate's three moved rows become architecture-conditional. Strong on every other criterion; loses on the one that matters. |
| **Make `checkRange` generic** | §3.5a: changes four mirrored copies for a need one package has. |
| **`int(n)` into the existing `checkRange`** | §3.5a: silently truncates on `GOARCH=386` and **accepts** the value it exists to reject, on the narrowest platform. |
| **An off-state, on the `endpoint.WithMaxPayloadBytes` model** (round-1 M-7) | §3.4a: there `n <= 0` is idle and free to mean "off"; here it is already a shipped typed rejection, so an off-state costs new exported surface. The divergence is accepted and recorded, not harmonised. |
| **Restructure `exchange.go:130-131` to stream** | §1.2: the sixth read is correct — it produces the reply payload. Streaming payloads are a different increment with a different SPI. |
| **Export the ceiling** (`msghttp.MaxByteCap`) | New exported surface; the wrapped error already names the bound. Spec 016 §5 deferred the same request for the nine. Revisit if a consumer asks. |

---

## 6. Verification

**AC-1 — the ceiling is the boundary, asserted on both sides.** For each of the three knobs, a `table-test` in
`adapter/http` (blackbox `package msghttp_test`) with:
- `NewConfig(WithX(byteCapCeiling))` → nil error, non-nil `*Config`, **and the knob's effect is observable**;
- `NewConfig(WithX(byteCapCeiling + 1))` → `errors.Is` the knob's sentinel;
- `NewConfig(WithX(1 << 62))` → the same sentinel.

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
by the **constructor only**. The small-`n` proof already exists and is re-asserted, not invented:
`WithMaxBodyBytes(1<<20)` + a 64 MiB body → `http: request body too large`.

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

**AC-4.1 — derive the site list, do not transcribe it** (round-1 B-3). Revision 1 listed **7** sites; there are
**at least 12**, and **every offset but one was stale** — they predated Plan 030's already-landed conversion
(`d2c69fe`), which moved the rows from `:519/:528/:537` to `:570/:579/:588`. This project's stored lesson is
*derive move-lists mechanically*. Run this against **current `HEAD`**, paste the output into the task's Evidence
block, and edit **every** hit:

```bash
grep -n 'deferred\|DEFERRED\|9/1/3/6\|9 + 1 + 3 + 6' sizing_option_class_gate_test.go
```

Output at `1212c63` — **17 lines, 12 distinct edit sites**:

| # | Line(s) | Site | Change |
|---|---|---|---|
| 1 | `:570`, `:579`, `:588` | the three rows' `arm` field | `"deferred"` → `"fixed"`; drop the trailing `// class member, remedy deferred — Spec 016 §3.8` comment |
| 2 | `:571-575`, `:580-584`, `:589-593` | the three rows' `assert` closures | `require.NoError` → `require.ErrorIs` on the knob's sentinel **+** `assert.EqualError` on the §3.1 render **+** `assert.False(t, msgin.IsPermanent(err), …)` (AC-2b) |
| 3 | `:782-784` | `wantArms` entries | `"deferred"` → `"fixed"` |
| 4 | `:803` | the `byArm` count assertion | remove the `deferred` key — see the trap below |
| 5 | `:35` | the header's arm list | the `- "deferred" (3) — accepts 1<<62 …` bullet |
| 6 | `:38` | the header's arithmetic identity | `9 + 1 + 3 + 6 = 19` → `12 + 1 + 6 = 19` |
| 7 | `:55-59` | **Plan 030's per-arm literal block** | 🔴 goes false **twice** — see below |
| 8 | `:401` | the `arm` field's doc comment | the four-arm vocabulary string — **keep `"deferred"` with a tombstone** |
| 9 | `:539-546` | the `---- arm: deferred ----` section banner | the arm no longer exists; the rows join `fixed` |
| 10 | `:547-556` | 🔴 block **one** — *"WHEN §3.8's CEILING LANDS…"* | instructions → record |
| 11 | `:557-568` | 🔴 block **two** — *"THESE THREE ROWS KEEP THE 1<<62 LITERAL"* | revision 1 counted these two blocks as one |
| 12 | `:758`, `:761`, `:801`, `:805` | four prose strings **inside live assertion messages** naming the `9/1/3/6` split | they are arguments to `require.Equal`, not comments |

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
> **Restate the surviving invariant, which is the true one and is mechanically checkable: the literal is chosen
> by the option's PARAMETER TYPE, not by its arm** — an `int` parameter takes `1<<30` (fits int32, exceeds every
> `int`-typed ceiling, renders an architecture-independent decimal); an `int64` parameter takes `1<<62` (in range
> on every architecture, renders `4611686018427387904`). Both then select the out-of-range branch everywhere.
>
> 🔴 **`1 << 30` CANNOT be used for the three moved rows.** `1,073,741,824 < byteCapCeiling = 2,147,483,647`, so
> it would be **accepted** and every `require.ErrorIs` would fail. The three rows keep `1 << 62`. §3.5 records
> that this holds under an `int` signature too — indeed more sharply, since on 386 no `int` literal exceeds the
> ceiling at all.
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

**Spec 016 §2.1's arm table is updated by WHICHEVER LANDS SECOND**, in one edit reflecting both increments. The
first lander updates only its own rows and says in its fold-back task that the other is pending. Recording this
here is what stops both increments claiming the table or both skipping it.

**Retire the arm name, or keep it as a tombstone?** Keep `"deferred"` in the `arm` field's documented vocabulary
with an explicit *(no members as of Plan 032 — see Spec 018)* tombstone, mirroring how ADR 0032 D-AB retained
safety causes **(c)** and **(d)** as tombstones so the surviving letters keep their identifiers. Delete it from
the counts map (it must be absent) and from the header's arm list. **Do not delete the concept** — a future knob
whose remedy is genuinely deferred needs the arm back, and the tombstone tells the next author it existed.

**AC-5 — the parent artifacts are updated in the same commit.** [Spec 016](016-sizing-option-bounds.md) §2.1's
census table and arm table, §3.8, §6 AC-5's arm table, and [ADR 0032](../adrs/0032-sizing-option-bounds.md)
**D-AB** all record a `deferred` arm of 3. Each is amended to point here. `docs/HANDOVER.md` §7 item 6 is closed.
**A bundle that changes a knob's class without moving every row that records it is the "stopped ONE FILE SHORT"
failure Spec 016 revision 6 opened with** — Plan 032 Task 2 carries the cross-file grep guard that finds them all
in one command, and **runs it**. **The Spec 016 §2.1 arm table is written by whichever of Plan 031 / Plan 032
lands SECOND** (§6 AC-4.2); the first lander updates only its own rows and records that the other is pending.

**AC-6 — every new branch is covered and mutation-proven.** Per CLAUDE.md's test-coverage gate and the project's
standing rule that *a killed mutant is the evidence, not a green run*. Enumerated per task in
[Plan 032](../plans/032-byte-cap-ceilings.md).

**AC-7 — vacuity probes.** Both arms of the docs-link gate are run over the **four** new files (this spec,
ADR 0034, Plan 032 and [`docs/plans/032-audit-round-1.md`](../plans/032-audit-round-1.md)) **and** proven
non-vacuous by planting a bad link and a bad anchor in one of them (not in root — the Plan 028 blindness came from
probing only root) and confirming exactly one hit each, which disappears on revert.

---

## 7. Scope

**In:** `byteCapCeiling`; `checkRangeInt64`; the three upper arms in `NewConfig`; the three sentinel messages
(`must be > 0` → `out of range`); the three options' godoc **and** the three sentinel godocs, including Spec 016
§3.8 item 2's undelivered disclosure; the rewrite of `adapter/http/exchange_test.go` branch 20 (§6 AC-2c); the
class-gate arm move over the 12-site inventory (§6 AC-4); the fold-back into Spec 016 / ADR 0032 / HANDOVER.

**Out:** any signature change (`(n int)` considered and rejected — §3.5); any default change; any off-state (§3.4b
— decided, not deferred); a representability ceiling for `endpoint.WithMaxPayloadBytes` (§3.4a — recorded as a
follow-up, different package and different gate arm); restructuring `exchange.go:130-131` (§1.2); streaming
payload types; `time.Duration` knobs; a 386 CI runner or `qemu` to close D-AR(b)'s gap (§6 AC-3);
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
| 2 | **D-AO — `math.MaxInt32`, one shared constant** (§3.2) | The value is the whole argument. Any *lower* number is a payload guess and re-opens the deferral; any *higher* number makes the cap's meaning word-size-dependent. Reversing to per-knob constants is three lines but forfeits the single justification. |
| 3 | **D-AM — same class, different remedy** (§1.1) | Splitting the class moves 3 rows and adds one `byArm` key, but obliges a re-check of all 19 rows against an amended criterion (m-12 corrected the inflated figure). |
| 4 | **D-AP — `(n int64)` kept; `checkRangeInt64` a sibling, not a generic** (§3.5, §3.5a) | **Now carries the `(n int)` analysis round 1 demanded.** Narrowing to `int` deletes the helper and the D-AR(b) gap but makes the ceiling equal `math.MaxInt` on 32-bit, so the upper arm becomes inexpressible. Reversing means re-doing §6 AC-2/AC-3/AC-4 for an architecture-conditional gate. |

**Also open, and NOT decided here:**

1. **The defaults (1 MiB × 3).** Safe and out of scope, but a caller who raises one to 100 MiB gets no warning —
   the ceiling does not help them. A godoc-and-observability question for a later increment.
2. **A representability ceiling for `endpoint.WithMaxPayloadBytes`** (§3.4a). It accepts `math.MaxInt` today and
   the class gate certifies that `safe` because the option never validates `n` at all. Different package,
   different arm; recorded so it is not rediscovered.
3. **Closing D-AR(b)'s mutation gap** with a 386 CI runner or `qemu` (§6 AC-3). A CI decision, not a code one.
