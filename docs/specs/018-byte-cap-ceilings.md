# Spec 018 — A byte cap carries a representability ceiling, and no off-state

- **Status:** **PROPOSED — revision 1, pre-audit, NOT accepted.** Written before any code, per
  [CLAUDE.md](../../CLAUDE.md)'s design-time gate. The adversarial design audit over the assembled bundle
  (this spec + [ADR 0034](../adrs/0034-byte-cap-ceilings.md) + [Plan 032](../plans/032-byte-cap-ceilings.md))
  has **not** run. Two rounds is this project's norm; Plan 029 needed five.
  - 🔴 **Every decision here was taken by the coordinator WITHOUT USER RATIFICATION.** The user was not asked.
    Each is **explicitly reversible**; §8 lists the four that most deserve a second look, and each ADR 0034
    decision carries a **REVERSIBILITY** line stating what undoing it costs.
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
mean either (a) amending D-AB's criterion to add a failure-mode clause, which re-opens all 19 gate rows, or
(b) maintaining two criteria that partition the same knobs — the hand-maintained-census pattern D-AB exists to
kill. Neither is worth buying a taxonomy that changes no outcome.

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
2. **`exchange.go:130-131`** — `io.ReadAll(io.LimitReader(resp.Body, max))`, retained as the reply payload. Note
   `exchange.go:133`'s `int64(len(body)) == max` INV-6 probe: it compares an `int`-derived length against the
   `int64` cap and is unaffected by this change, since a bounded `max` is still `int64`.
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
| They do **not** break `GOARCH=386` | **TRUE** | `GOARCH=386 GOOS=linux go test -gcflags=all=-e -run=NONE ./...` compiles every package; the gate's `1 << 62` fits `int64` |

So the question is exactly: **should an explicit off-state exist at all, and which sentinel value would carry
it?** `-1` and `0` are already taken by the rejection, so an off-state needs a **new** sentinel value — a named
exported constant, or a magic number. **§3.4 answers: no off-state.**

### 2.1 One claim in the delivered bundle is FALSE

**Spec 016 §3.8 item 2 promised a godoc change that Plan 029 never delivered.** It states, under *"What this
increment DOES do for them"*:

> *"2. Their **godoc gains the hazard disclosure**: the knob is the only bound on a remote-driven read, the default
> is safe, and a large explicit value removes the protection."*

**No such disclosure exists in any of the three godocs.** Read this revision: `WithMaxBodyBytes` (`options.go:446-462`),
`WithMaxResponseBytes` (`:749-766`), `WithMaxEventBytes` (`:834-855`) each carry a `CAVEAT` about *headers* and a
paragraph about `n <= 0` — neither of which is the promised disclosure. Plan 029 has **no task** for it
(`grep -n 'hazard disclosure' docs/plans/029-sizing-option-bounds.md` → no hits), so it was promised in the spec
and never scheduled. By contrast the *bounded* siblings did get theirs: `WithMaxConnections` (`options.go:901`) and
`WithReplayBuffer` (`:976`) both cite Spec 016 §1.3 in their godoc.

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

`errors.Is` is unaffected — the sentinel is the same object, carried through `%w`. **Verified test-safe this
revision:** `grep -rn 'max body bytes must be\|max response bytes must be\|max event bytes must be' --include='*.go' .`
returns **only the three declarations in `errors.go`**; no test asserts any of the strings. Re-verify at
implementation time, because Plan 030 is editing test files concurrently.

**Not `Permanent`-wrapped**, matching ADR 0029 **D-M** and Spec 016 §3.1: it is a constructor return and never
travels through a `MessageHandler`.

### 3.2 The ceiling — `math.MaxInt32`, and why it is a fact rather than a guess

**Value: `byteCapCeiling int64 = math.MaxInt32` = `2,147,483,647` bytes** (one byte under 2 GiB).

**The justification never mentions the caller's payload.** All three reads terminate in a single contiguous
in-memory buffer — an `io.ReadAll` `[]byte`, a `bytes.Buffer`'s `[]byte`, an `append`-grown `[]byte`. A Go slice's
`len` and `cap` are of type `int`. On the narrowest `GOARCH` this module builds for — `386`, `arm`, `mips`,
`mipsle`, all 32-bit `int` — the largest expressible slice length is exactly `math.MaxInt32`. **A cap above that
value cannot be honoured anywhere the library builds: no `[]byte` of that length can exist.**

So the ceiling is not a policy number picked from a plausible range. It is the answer to *"what is the largest
value this knob could ever be obeyed at?"* — and admitting anything larger is the same inversion
[Spec 016 §1.1](016-sizing-option-bounds.md) identified: **certifying as configurable a value the machine cannot
serve.**

**Why this survives CLAUDE.md's Sensible-defaults gate — the objection, stated at full strength first.**

> *The gate says a byte cap cannot be guessed because it "depends on the caller's legitimate payload size."
> Spec 016 deferred these three for exactly that reason. A ceiling is still a number the library picked. Calling it
> a "ceiling" rather than a "default" is a relabelling, not an argument.*

**The objection is correct about ceilings in general and wrong about this one, and the distinction is the VALUE,
not the mechanism.** Test it against a policy ceiling:

| Candidate | Its justification | Verdict |
|---|---|---|
| `1 << 30` (1 GiB) | *"no sane HTTP API sends more than a gigabyte"* | **A guess about the caller's payload wearing a ceiling's clothes.** Fails the gate. |
| `1 << 32` (4 GiB) | *"a round number safely above anything real"* | Same guess, **plus** it is **not representable on 32-bit** — so it re-commits the §1.1 inversion. Fails harder. |
| **`math.MaxInt32`** | *"a `[]byte` cannot be longer, on any platform this builds for"* | **A property of the receiving data structure.** Nothing about the caller. Passes. |

**The argument licenses exactly one ceiling, not a family of them.** That is the honest reading: had `[]byte` no
width limit, this spec would have no ceiling to propose and the item would stay deferred.

**Three further properties, each an independent benefit** (none is the primary argument):

1. **Width safety.** `n <= math.MaxInt32` guarantees `int(n)` is lossless on **every** `GOARCH`. A future
   refactor that does `make([]byte, n)` or `int(n)` cannot silently truncate. This closes a latent 32-bit hazard
   the current `int64`-with-no-upper-bound shape carries.
2. **Margin.** 2048× the 1 MiB default; ~32× the 64 MiB body Plan 029 exercised.
3. **Distance from the fatal band.** [Spec 016 §1.1](016-sizing-option-bounds.md) measured `runtime.throw`
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

### 3.5 The helper — `checkRangeInt64`, a sibling; not a generic, and never a conversion

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

1. **No signature change.** All three keep `(n int64)`. `apidiff`'s exported-surface delta is **0 removals / 0
   additions** — `byteCapCeiling` and `checkRangeInt64` are unexported.
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
2. **`checkRangeInt64`'s godoc** states why it is a sibling rather than a generic or a conversion (§3.5), naming
   the 32-bit truncation it exists to prevent, and mirrors the existing `checkRange` godoc's cross-reference block.
3. **No existing godoc sentence becomes false.** Checked this revision: the three options' `n MUST be > 0`
   paragraphs are *narrowed*, not contradicted, and are rewritten in the same edit. The `CAVEAT` paragraphs (about
   headers and about the O1 drain) are unaffected. This is the check CLAUDE.md's stored lesson — *"all three fix
   rounds in Plan 028 were godoc, not logic"* — demands; Plan 032 Task 2 carries it as a step.

---

## 5. Alternatives rejected

| Alternative | Why rejected |
|---|---|
| **Keep deferring** | The class gate's `deferred` arm is a standing assertion that an unbounded remote-driven read is conformant. Every month it stays, it reads more like a verdict and less like a placeholder. |
| **A policy ceiling (1 GiB / 4 GiB)** | §3.2: a guess about the caller's payload, which is exactly what CLAUDE.md's Sensible-defaults gate forbids and why Spec 016 deferred. `1<<32` additionally is unrepresentable on 32-bit. |
| **Per-knob ceilings** | §3.2: three knobs, one unit, one justification. Three constants encode a false independence. |
| **A ceiling plus an off-state** | §3.4: new exported surface whose only purpose is to re-enable the hazard — Spec 016 §5's `WithUnsafeUnboundedSizing` rejection, verbatim. |
| **Lower the defaults instead** | The defaults are already safe (1 MiB) and are not the problem. Lowering them breaks working callers and leaves `1<<62` reachable. |
| **Make `checkRange` generic** | §3.5: changes four mirrored copies for a need one package has. |
| **`int(n)` into the existing `checkRange`** | §3.5: silently truncates on `GOARCH=386` and **accepts** the value it exists to reject, on the narrowest platform. |
| **Restructure `exchange.go:130-131` to stream** | §1.2: the sixth read is correct — it produces the reply payload. Streaming payloads are a different increment with a different SPI. |
| **Export the ceiling** (`msghttp.MaxByteCap`) | New exported surface; the wrapped error already names the bound. Spec 016 §5 deferred the same request for the nine. Revisit if a consumer asks. |

---

## 6. Verification

**AC-1 — the ceiling is the boundary, asserted on both sides.** For each of the three knobs, a `table-test` in
`adapter/http` (blackbox `package msghttp_test`) with:
- `NewConfig(WithX(byteCapCeiling))` → nil error, non-nil `*Config`, **and the accessor returns the value set**
  (Spec 016 §6's *"its product is usable"* definition for a `NewConfig`-only key);
- `NewConfig(WithX(byteCapCeiling + 1))` → `errors.Is` the knob's sentinel;
- `NewConfig(WithX(1 << 62))` → the same sentinel.

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

**AC-3 — the width guard is proven on a 32-bit target.** `checkRangeInt64`'s reason to exist is invisible on
`darwin/arm64`, where `int(1<<62)` does not truncate. The task therefore compiles and vets the workspace for a
32-bit `GOARCH`:

```bash
GOTOOLCHAIN=go1.25.13 GOARCH=386 GOOS=linux go test -gcflags=all=-e -run=NONE ./...
```

**Verified clean on this tree before any edit** — every root package compiles; the gate's `1 << 62` literals fit
`int64`. It must stay clean after. **The mutant that proves the assertion is a 32-bit mutant** (§AC-6): replacing
`checkRangeInt64(…, n, …)` with `checkRange(…, int(n), …)` must be caught, and it is caught **only** on a 32-bit
target — so the 386 arm is a required evidence step, not a nice-to-have.

> **This is a genuine limitation, recorded rather than papered over:** `go test -run=NONE` **compiles** the 386
> binaries but cannot **execute** them on darwin/arm64 (`exec format error`). The 386 arm therefore proves the
> code *builds and vets*, not that the truncation mutant fails at runtime. Killing that mutant behaviorally needs
> either a 386 runner in CI or `qemu`. **ADR 0034 D-AR records this as an accepted gap**, with the compile-arm as
> the shipped guard and the godoc as the second.

**AC-4 — the class gate moves with the class.** `sizing_option_class_gate_test.go` files all three in its
**`deferred`** arm today. This increment **empties that arm**, and every one of the following must move in the
same commit or the gate goes red:

| # | Site | Change |
|---|---|---|
| 1 | the three rows' `arm` field (`:519`, `:528`, `:537`) | `"deferred"` → `"fixed"` |
| 2 | the three rows' `assert` closures | `require.NoError` → `require.ErrorIs` on the knob's sentinel **+ the §3.1 render** |
| 3 | `wantArms` (`:726-728`) | three entries `"deferred"` → `"fixed"` |
| 4 | the `byArm` count assertion (`:747`) | `{"fixed": 9, "rejects": 1, "deferred": 3, "safe": 6}` → **`{"fixed": 12, "rejects": 1, "safe": 6}`** |
| 5 | the file header comment (`:35`) | `- "deferred" (3) — accepts 1<<62, annotated so it never reads as a…` |
| 6 | the `arm` field's doc comment (`:362`) | the four-arm vocabulary string |
| 7 | the 🔴 block above the deferred rows (`:500-518`) | it instructs a future contributor on what to do *when the ceiling lands*. That is now history. |

> 🔴 **Item 4 is the trap.** `byArm` is built by counting, so an emptied arm has **no key at all** — writing
> `{"fixed": 12, "rejects": 1, "deferred": 0, "safe": 6}` fails, because Go's counting map never produces a zero
> entry. The `deferred` key must be **removed**, not zeroed.
>
> 🔴 **Item 7 is the one a mechanical edit misses.** The block reads *"WHEN §3.8's CEILING LANDS, THIS GATE WILL
> GO RED, AND THAT IS CORRECT … the repair is to MOVE the row into the `fixed` arm."* **This increment is that
> event.** The block is rewritten into a record of what happened, so the next reader is not told to wait for
> something already done.

**`require.Len(t, tests, 19)` is unchanged** — 17 AST rows + 2 manual rows. No key is added or removed; three
change arm. **`sizingConformanceKeys` is unchanged.** Half 1 (the AST completeness walk) is unchanged: the three
functions still exist with the same `int64` parameters.

**Retire the arm name, or keep it as a tombstone?** Keep `"deferred"` in the `arm` field's documented vocabulary
with an explicit *(no members as of Plan 032 — see Spec 018)* tombstone, mirroring how ADR 0032 D-AB retained
safety causes **(c)** and **(d)** as tombstones so the surviving letters keep their identifiers. Delete it from
the counts map (it must be absent) and from the header's arm list. **Do not delete the concept** — a future knob
whose remedy is genuinely deferred needs the arm back, and the tombstone tells the next author it existed.

**AC-5 — the parent artifacts are updated in the same commit.** [Spec 016](016-sizing-option-bounds.md) §2.1's
census table and arm table, §3.8, §6 AC-5's arm table, and [ADR 0032](../adrs/0032-sizing-option-bounds.md)
**D-AB** all record a `deferred` arm of 3. Each is amended to point here. `docs/HANDOVER.md` §7 item 6 is closed.
**A bundle that changes a knob's class without moving every row that records it is the "stopped ONE FILE SHORT"
failure Spec 016 revision 6 opened with** — Plan 032 Task 4 carries the cross-file grep guard that finds them all
in one command, and **runs it**.

**AC-6 — every new branch is covered and mutation-proven.** Per CLAUDE.md's test-coverage gate and the project's
standing rule that *a killed mutant is the evidence, not a green run*. Enumerated per task in
[Plan 032](../plans/032-byte-cap-ceilings.md).

**AC-7 — vacuity probes.** Both arms of the docs-link gate are run over the three new files **and** proven
non-vacuous by planting a bad link and a bad anchor in one of them (not in root — the Plan 028 blindness came from
probing only root) and confirming exactly one hit each, which disappears on revert.

---

## 7. Scope

**In:** `byteCapCeiling`; `checkRangeInt64`; the three upper arms in `NewConfig`; the three sentinel messages
(`must be > 0` → `out of range`); the three options' godoc including Spec 016 §3.8 item 2's undelivered
disclosure; the class-gate arm move; the fold-back into Spec 016 / ADR 0032 / HANDOVER.

**Out:** any signature change; any default change; any off-state (§3.4 — decided, not deferred); restructuring
`exchange.go:130-131` (§1.2); streaming payload types; `time.Duration` knobs; `docs/HANDOVER.md` §7 items 3, 4, 5,
7, 8.

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
| 1 | **D-AN — no off-state** (§3.4) | A caller loses the ability to configure >2 GiB. Reversing means one exported const + one branch, **additive**, and can happen after this lands. This is the decision most likely to be contested. |
| 2 | **D-AO — `math.MaxInt32`, one shared constant** (§3.2) | The value is the whole argument. Any *lower* number is a payload guess and re-opens the deferral; any *higher* number is unrepresentable on 32-bit. Reversing to per-knob constants is three lines but forfeits the single justification. |
| 3 | **D-AM — same class, different remedy** (§1.1) | Splitting the class means amending ADR 0032 D-AB's criterion and re-deriving all 19 gate rows. |
| 4 | **D-AP — `checkRangeInt64` sibling, not a generic** (§3.5) | A generic touches four packages' mirrored helpers; the sibling touches one file. ~7 duplicated lines is the price. |

**Also open, and NOT decided here:** whether the *defaults* (1 MiB × 3) deserve revisiting. They are safe and out
of scope, but a caller who raises one to 100 MiB gets no warning — the ceiling does not help them. That is a
godoc-and-observability question for a later increment.
