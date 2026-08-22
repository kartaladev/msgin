# ADR 0034 — A byte cap is bounded by what a `[]byte` can represent, not by what a payload might be

- **Status:** **PROPOSED — revision 1, pre-audit, NOT accepted.** Written before any code, per
  [CLAUDE.md](../../CLAUDE.md)'s design-time gate. The adversarial design audit over the assembled bundle
  ([Spec 018](../specs/018-byte-cap-ceilings.md) + this ADR + [Plan 032](../plans/032-byte-cap-ceilings.md))
  has **not** run.
  - 🔴 **Decisions D-AM through D-AT were taken WITHOUT USER RATIFICATION.** The user was not asked. Every
    decision below is **open to reversal**; [Spec 018 §8](../specs/018-byte-cap-ceilings.md) lists the four that
    most deserve a second look, and each decision here carries a **REVERSIBILITY** line stating what undoing it
    costs.
  - The decision-letter series continues from [ADR 0033](0033-group-member-bounds.md), which ends at **D-AL**.
- **Prompted by:** [Spec 018](../specs/018-byte-cap-ceilings.md); the backlog item in
  [`docs/HANDOVER.md`](../HANDOVER.md) §7 item **6**.
- **Realized by:** [Plan 032](../plans/032-byte-cap-ceilings.md).
- **Finishes** [ADR 0032](0032-sizing-option-bounds.md): **D-AB** classified `msghttp.WithMaxBodyBytes`,
  `WithMaxEventBytes` and `WithMaxResponseBytes` as class members with a **deferred** remedy and explicitly
  refused to certify them safe. This ADR supplies that remedy. It **does not supersede** any part of ADR 0032 —
  **D-W** (a stated ceiling, not a runtime-derived one), **D-X** (reuse the sentinels, mint none, wrap the value),
  **D-Z**'s reasoning style and **D-AB**'s membership criterion are all reused **unchanged**.
- **Related:** [ADR 0031](0031-nil-option-elements.md) **D-R** (per-package unexported helpers, independent copies
  rather than a shared internal package — the precedent for D-AP); [ADR 0029](0029-eip-lexical-alignment.md)
  **D-M** (a constructor return is not `Permanent`-wrapped); [ADR 0033](0033-group-member-bounds.md), the sibling
  increment descending from the same parent.

---

## Context

**Three `msghttp` options survived Plan 029 unbounded, by design.** ADR 0032 **D-AB** established the membership
criterion — *"a knob is a CLASS MEMBER iff `n` is the sole bound on an accumulation"* — and all three satisfy it:
each is the sole bound on a read driven by a **remote peer** whose result is **retained in memory**.

| Knob | Reaches | Retained as |
|---|---|---|
| `WithMaxBodyBytes` | `io.ReadAll(http.MaxBytesReader(nil, body, cfg.maxBody()))` `encode.go:102` | the decoded message payload |
| `WithMaxResponseBytes` | `io.ReadAll(io.LimitReader(resp.Body, max))` `exchange.go:130-131` | the reply payload |
| `WithMaxEventBytes` | `p.dataBuf.WriteString(value)` / `buf = append(buf, b)` `sse.go:387`, `:472` | the in-progress SSE event |

D-AB deferred rather than fixed because [CLAUDE.md](../../CLAUDE.md)'s Sensible-defaults gate names this exact
case: *"If **no** value can be safe for an unknown caller (e.g. **a byte cap that depends on the caller's
legitimate payload size**), make it explicit/opt-in … rather than guessing a default."* That reasoning was
correct **for a ceiling justified by payload size**. It is silent on a ceiling justified by something else.

**Four facts, each re-derived against the code this revision, shape every decision below:**

1. **All three already reject an explicit `n <= 0`** with their own typed sentinel — `options.go:1189-1193`,
   `:1201-1205`, `:1211-1215`. **Leaving the option unset already *is* the documented default state** (1 MiB, all
   three). So the open question is not *"invent an off-state"*; it is *"should an explicit off-state exist, and
   which sentinel value would carry it"* — and `-1` and `0` are already taken by the rejection.
2. **All three take `int64`**, which is why they never break `GOARCH=386` and why they sit in the class gate's
   `deferred` arm rather than in Plan 030's 32-bit overflow list. Verified:
   `GOARCH=386 GOOS=linux go test -gcflags=all=-e -run=NONE ./...` compiles every root package on this tree.
3. **`drainBounded` is five of `cfg.maxResponseBytes`'s six I/O-consuming reads** — verified, not transcribed
   (`sseclient.go:335,338,341`, `outbound.go:370`, `exchange.go:126`; the sixth is `exchange.go:130-131`). **The
   sixth is not an omission.** `drainBounded` discards; the sixth read produces the value the caller asked for.
   You cannot discard the body you are contracted to return.
4. **The shipped `checkRange` (`adapter/http/helpers.go:64`) takes `int`.** It exists in four independent copies
   (`endpoint`, `routing`, `adapter/memory`, `adapter/http`) under ADR 0031 D-R, each with godoc asserting it
   mirrors the other three.

**And one delivered claim is FALSE.** [Spec 016 §3.8](../specs/016-sizing-option-bounds.md) item 2 promised that
*"their godoc gains the hazard disclosure."* It does not — read this revision, none of the three godocs carries
it, and Plan 029 has no task for it. The bounded siblings did get theirs (`WithMaxConnections` `options.go:901`,
`WithReplayBuffer` `:976`). **D-AT** delivers it, in the superseding post-ceiling form.

---

## Decision

### D-AM — same class as the nine, different remedy; the class is NOT split

The three knobs stay **class members under ADR 0032 D-AB's criterion, unamended**. What differs from the nine
Plan 029 bounded is the **failure signature**, and that difference does not touch the criterion.

The nine fail because the knob **stops meaning what it says**: `memory.WithBuffer(1<<62)` panics in `makechan`;
`memory.WithMaxGroups(1<<62)` makes `len(s.groups) >= n` permanently false, so the cap admits everything;
`endpoint.WithConcurrency(1<<62)` corrupts a `WaitGroup` so `Wait()` returns while workers run. These three fail
because the knob **does exactly what it says** — `WithMaxBodyBytes(1<<62)` faithfully permits bodies up to `1<<62`
bytes, and `http.MaxBytesReader` enforces it correctly. The hazard is that *"effectively infinite" is a reachable
configuration*, not that the configuration is dishonest.

**Why not a second class.** D-AB's criterion says nothing about panics, corruption, or honesty. Adding a
failure-mode clause to make these three a separate class would either amend the criterion — re-opening all 19 gate
rows and the census that took five audit rounds to stabilise — or maintain two overlapping criteria, which is the
hand-maintained census D-AB exists to kill. A taxonomy change that alters no outcome is not worth that.

**What the difference DOES buy** is the remedy's shape (**D-AN**): a ceiling expressed in the knob's **domain
unit** and justified by *what the knob means* (D-Z's nine) is unavailable here, because the domain unit is
**bytes** and the library does not know the caller's payload. So the ceiling must be justified in a different
currency, or not exist.

**REVERSIBILITY:** free as prose, expensive in consequence. Splitting the class means amending ADR 0032 D-AB and
re-deriving all 19 class-gate rows against a second criterion. Nothing below depends on the split; everything
below depends on the *remedy*.

### D-AN — the remedy is a **representability** ceiling, and **no off-state**

Two decisions that stand or fall together, because each is the reason the other is acceptable.

**(a) A ceiling, justified by what the receiving data structure can hold — not by what a payload might be.**
All three reads terminate in a single contiguous in-memory `[]byte`. A slice's `len`/`cap` are `int`. On the
narrowest `GOARCH` this module builds for (32-bit `int`: `386`, `arm`, `mips`, `mipsle`) the largest expressible
length is `math.MaxInt32`. **A cap above that cannot be honoured anywhere the library builds.**

This is what survives CLAUDE.md's gate where a policy number does not. **State the objection at full strength
before answering it:** *"a ceiling is still a number the library picked; calling it a ceiling rather than a
default is a relabelling."* Correct — for ceilings in general. The test is whether the ceiling's justification
mentions the caller:

| Candidate | Justification | Verdict |
|---|---|---|
| `1 << 30` (1 GiB) | *"no sane API sends more"* | a payload guess in a ceiling's clothes — **fails the gate** |
| `1 << 32` (4 GiB) | *"a round number above anything real"* | the same guess, **and** unrepresentable on 32-bit — fails harder |
| **`math.MaxInt32`** | *"a `[]byte` cannot be longer"* | a property of the data structure — **passes** |

**The argument licenses exactly one value, not a family.** Had `[]byte` no width limit, there would be no
defensible ceiling and the item would stay deferred. That is the honest reading, and it is why D-AO's *value* is
load-bearing rather than a tuning detail.

**(b) No off-state.** `msghttp` gains no way to say "unbounded". A named exported constant
(`msghttp.Unbounded`) is new exported surface on a pre-v1 API being kept deliberately small, whose **only**
purpose is to re-enable the hazard the class exists to close — which is [Spec 016 §5](../specs/016-sizing-option-bounds.md)'s
rejection of `WithUnsafeUnboundedSizing`, verbatim. A magic value is undiscoverable, and `-1`/`0` are taken.

**The residual cost, stated rather than minimised:** `WithMaxBodyBytes(1<<62)` is legal today and becomes a
construction error. A caller wanting a 3 GiB in-memory payload loses that capability outright. **That caller is
not served by this API shape anyway** — `io.ReadAll` into a message payload is the wrong tool for multi-gigabyte
transfers, and msgin has no streaming payload type. Free at pre-v1 (no tags, no consumers).

**What the ceiling explicitly does NOT claim:** that every permitted value is safe. A 2 GiB read can still
exhaust a container. The ceiling delivers three narrower things — the accumulation becomes *bounded* (satisfying
D-AB's criterion with a bound that exists), the absurd configuration becomes a *construction-time typed error*
(CLAUDE.md's debuggability criterion), and the **default** keeps doing the real protective work at 1 MiB.

**REVERSIBILITY:** (a) is the load-bearing decision; reversing it discards the increment and returns the three
rows to the `deferred` arm. **(b) is additive to reverse** — one exported const plus one branch, at any later
date, with no break to callers. This is the decision most likely to be contested, and deliberately the cheapest
to change later.

### D-AO — the value is `math.MaxInt32`, in **one** shared constant

```go
// adapter/http/options.go, beside the three defaultMax*Bytes
const byteCapCeiling int64 = math.MaxInt32 // 2,147,483,647 B — one byte under 2 GiB
```

**In bytes, and why:**

| Property | Figure |
|---|---|
| Ceiling | **2,147,483,647 B** |
| Ratio to the 1 MiB default | **2048×** |
| Ratio to the largest body Plan 029 exercised (64 MiB) | **~32×** |
| Distance below the measured unrecoverable band (`~2^47` B requested, Spec 016 §1.1) | **~65,536×** |
| `io.ReadAll` doubling peak at the ceiling | ~4 GiB — still inside the recoverable regime |
| Largest `len([]byte)` on 32-bit `GOARCH` | **exactly `2,147,483,647`** |

**A fourth, independent benefit: width safety.** `n <= math.MaxInt32` guarantees `int(n)` is lossless on **every**
`GOARCH`, so a future refactor doing `make([]byte, n)` or `int(n)` cannot silently truncate. The current
`int64`-with-no-upper-bound shape carries that hazard latently; this closes it.

**One constant, not three.** D-Z gave each of the nine its own ceiling because each is expressed in its own unit
(messages, connections, events, goroutines, group members) and justified by what *that knob* means. These three
share a unit (**bytes**) *and* the entire justification (**`[]byte` width**). Three constants would encode an
independence that does not exist and would invite three divergent edits.

> **Attacked:** *should `WithMaxEventBytes` be lower? A 2 GiB SSE event is absurd where a 2 GiB upload is not.*
> **No.** "Absurd" is a claim about payloads, and payloads are exactly what the library does not know — any
> number below the representability limit re-imports the guess that caused the deferral. Per-knob opinion belongs
> in the **default**, and all three already carry 1 MiB. Note also that `sse.go:472` grows its line buffer **one
> byte per iteration**, so that path's practical bound is *time*, not bytes — the same reasoning Spec 016 §3.4
> applies to `routing.WithCompletionSize`.

**REVERSIBILITY:** the constant is one line. But the *value* is the whole argument: any lower number forfeits the
justification and re-opens the deferral; any higher number is unrepresentable on 32-bit. Splitting into three
per-knob constants is three lines and forfeits the single justification. See Spec 018 §8 item 2.

### D-AP — a sibling `checkRangeInt64`; not a generic, and never an `int(n)` conversion

```go
func checkRangeInt64(sentinel error, site string, n, lo, hi int64) error
```

placed in `adapter/http/helpers.go` beside the shipped `checkRange`, rendering the **identical** shape
`"%w: %s: %d not in [%d, %d]"` (`%d` handles both widths), so every AC-2b-style assertion in the project carries
over unchanged.

**`int(n)` is the bug this ADR exists to prevent.** On `GOARCH=386`,
`checkRange(sentinel, site, int(n), 1, int(byteCapCeiling))` truncates silently and would **accept `1<<62`** on
32-bit while rejecting it on 64-bit — a guard that admits the value it exists to reject, on precisely the platform
where that value is least serviceable. That is [Spec 016 §1.1](../specs/016-sizing-option-bounds.md)'s inversion
for the third time in this class's history.

| Alternative | Why not |
|---|---|
| Generic `checkRange[T ~int \| ~int64]` | Changes the shape of a helper existing in **four** independent copies, each with godoc asserting it mirrors the other three. Either all four change for a need only `msghttp` has, or they diverge. ADR 0031 **D-R**'s precedent is duplication over abstraction — and ADR 0033 already adds a **fifth** copy on the same precedent. |
| Widen `checkRange` to `int64` | Forces conversions at the nine shipped `int` call sites — churn on delivered, gate-cleared code for no benefit. |
| Convert at the call site | The truncation above. |

**Cost: ~7 duplicated lines in one file**, plus one more helper for a future reader to notice — mitigated by
godoc that names the 32-bit truncation as its reason to exist.

**REVERSIBILITY:** free — it is one unexported function in one file. Promoting it to a generic later is a
mechanical change if a second package ever needs an `int64` range check.

### D-AQ — reuse the three sentinels; genericise their messages; adopt the `[lo, hi]` render at both arms

**No new exported sentinel** (ADR 0032 **D-X**, unchanged). `ErrInvalidMaxBodyBytes`,
`ErrInvalidMaxResponseBytes` and `ErrInvalidMaxEventBytes` are returned from the upper arm as well as the lower,
so an existing caller's `errors.Is` branch handles both ends with no change.

Each sentinel's own message states a **lower bound only** and becomes false under a two-ended range — the same
defect ADR 0032 §3.5 fixed for six sentinels. Fix the class, not the instance:

| Sentinel | Declared | Today | Becomes |
|---|---|---|---|
| `msghttp.ErrInvalidMaxBodyBytes` | `errors.go:19` | `msghttp: max body bytes must be > 0` | `msghttp: max body bytes out of range` |
| `msghttp.ErrInvalidMaxResponseBytes` | `errors.go:77` | `msghttp: max response bytes must be > 0` | `msghttp: max response bytes out of range` |
| `msghttp.ErrInvalidMaxEventBytes` | `errors.go:138` | `msghttp: max event bytes must be > 0` | `msghttp: max event bytes out of range` |

**Verified test-safe this revision:** grepping the three strings across `--include='*.go'` returns **only the three
declarations**; no test asserts them. Re-verify at implementation time — Plan 030 is editing test files
concurrently.

**Not `Permanent`-wrapped** (ADR 0029 **D-M**): these are constructor returns and never travel through a
`MessageHandler`, so a retry classification on them is meaningless.

**REVERSIBILITY:** the wrap is three `checkRangeInt64` call sites; the message text is three string literals.

### D-AR — the sixth read is **bounded, not restructured**; and the 32-bit guard's proof is a compile arm

Two acknowledgements that belong in the record rather than in a future rediscovery.

**(a) `exchange.go:130-131` stays as it is.** *"`drainBounded` is 5 of 6 reads"* is evidence that the earlier
*safe* verdict rested on the wrong five sites — **it is not a to-do list**. The sixth read produces the reply
payload; discarding it is not an option. After this ADR, `max` at that site is bounded by `byteCapCeiling`
instead of by `1<<62`, and all six reads inherit the same finite bound from the same field. Nothing moves. Say so
explicitly, because *"5 of 6"* reads like an omission and the next reader will try to close it.

**(b) The width guard cannot be behaviorally mutation-killed on this project's reference platform.** D-AP's reason
to exist is invisible on `darwin/arm64`, where `int(1<<62)` does not truncate. The shipped guard is therefore a
**compile-and-vet arm** —

```bash
GOTOOLCHAIN=go1.25.13 GOARCH=386 GOOS=linux go test -gcflags=all=-e -run=NONE ./...
```

— which **compiles** the 386 binaries but cannot **execute** them (`exec format error` on darwin/arm64). So the
`checkRange(…, int(n), …)` mutant is caught by review and by godoc, not by a red test. **This is an accepted gap,
stated rather than papered over.** Closing it needs a 386 runner in CI or `qemu`, which is a CI decision outside
this increment's scope. Recording it here means the next author knows the guard's strength precisely instead of
assuming a mutant was killed.

**REVERSIBILITY:** (a) free — reversing means designing a streaming payload type, a different increment. (b) free
— adding a 386 CI runner later strengthens the guard without changing any code here.

### D-AS — the class gate's `deferred` arm is **emptied**, and its name is kept as a tombstone

`sizing_option_class_gate_test.go` files all three knobs in `deferred`. Moving them is **seven** coordinated
edits, enumerated in [Spec 018 §6 AC-4](../specs/018-byte-cap-ceilings.md) and in Plan 032 Task 3. Two are traps:

1. **`byArm` is built by counting, so an emptied arm has no key.**
   `{"fixed": 12, "rejects": 1, "deferred": 0, "safe": 6}` **fails** — Go's counting map never produces a zero
   entry. The `deferred` key must be **removed**, not zeroed.
2. **The 🔴 comment block above the deferred rows is instructions for a future contributor** — *"WHEN §3.8's
   CEILING LANDS, THIS GATE WILL GO RED, AND THAT IS CORRECT … the repair is to MOVE the row into the `fixed`
   arm."* **This increment is that event.** The block is rewritten into a record of what happened, so the next
   reader is not told to wait for something already done. A mechanical row-move misses this.

**Keep `"deferred"` in the `arm` field's documented vocabulary with a tombstone** — *(no members as of Plan 032 —
see Spec 018)* — mirroring how ADR 0032 D-AB retained safety causes **(c)** and **(d)** as tombstones so the
surviving identifiers kept their letters. Delete it from the counts map (it must be absent) and from the header's
arm list. **Do not delete the concept:** a future knob whose remedy is genuinely deferred needs the arm back, and
the tombstone tells the next author it existed and why.

`require.Len(t, tests, 19)` and `sizingConformanceKeys` are **unchanged** — 17 AST rows + 2 manual rows, no key
added or removed, three changing arm. Half 1 (the AST completeness walk) is unchanged: the three functions still
exist with the same `int64` parameters.

**REVERSIBILITY:** free — the arm partition is test data. But note it is **normative**: Spec 016 §2.1 and §6 AC-5
fix every key's arm, so moving a row is a **spec change**, which is why D-AT amends the parents in the same
commit.

### D-AT — deliver Spec 016 §3.8 item 2's undelivered godoc, and amend every artifact that records the old arm

**(a) The hazard disclosure ships here, in its superseding form.** Spec 016 promised it and Plan 029 never
scheduled it (Context, fact 4). After a ceiling exists, the honest disclosure is not *"this knob is unbounded"* but:

> **The ceiling is not a safety guarantee** — this option is the only bound on a read driven by a remote peer, and
> raising it above the 1 MiB default trades flood protection for payload size.

paired with the range, the ceiling's value, its representability rationale, and the typed error.

**(b) Every artifact recording the `deferred` arm of 3 is amended in the same commit.** Spec 016 §2.1 (census
table + arm table), §3.8, §6 AC-5's arm table; ADR 0032 **D-AB**; `docs/HANDOVER.md` §7 item 6.
**A bundle that changes a knob's class without moving every row that records it is exactly the "stopped ONE FILE
SHORT" failure Spec 016 revision 6 opened with** — seven twins survived a reclassification because the cross-file
grep guard *"was written and not run."* Plan 032 Task 4 carries that guard and **runs it**, pasting the output.

**REVERSIBILITY:** free — both halves are prose. Omitting (a) would leave a two-increment-old promise
outstanding; omitting (b) would ship a bundle that contradicts itself.

---

## Consequences

**Positive**

- **The class is closed.** After this increment ADR 0032's census is **12 fixed + 1 rejects + 6 safe**, with **no
  deferred remedy** anywhere in the sizing-option class. The gate stops asserting that an unbounded remote-driven
  read is conformant.
- **Every `msghttp` byte cap is finite**, so `n` is no longer the sole *unbounded* bound on any accumulation the
  criterion covers.
- **A latent 32-bit truncation hazard is closed** (D-AO's width safety), on three options whose `int64` type is
  the reason they escaped Plan 030's 386 sweep.
- **Debuggability improves at both arms**: `msghttp: max body bytes out of range: msghttp.WithMaxBodyBytes: 0 not in [1, 2147483647]`
  replaces a bare sentinel that named neither the value nor the site.
- **Zero exported-surface change.** `apidiff`: 0 removals / 0 additions. `byteCapCeiling` and `checkRangeInt64`
  are unexported.
- **A two-increment-old broken promise is discharged** (D-AT(a)).

**Negative / accepted**

- **A behavioral break at pre-v1:** `WithMaxBodyBytes(1<<62)` goes from legal to a construction error, and a
  caller wanting >2 GiB in one in-memory payload has **no escape** (D-AN(b)). Free today — no tags, no consumers
  — and additive to reverse.
- **Three rendered messages change**, including on the already-shipped lower arm (D-AQ). `errors.Is` is
  unaffected; no test asserts the strings today.
- **~7 duplicated lines** for `checkRangeInt64` (D-AP), and a fifth range-checking helper in the workspace once
  ADR 0033 lands its own.
- **The width guard is not behaviorally mutation-killed** on the reference platform (D-AR(b)) — a compile arm and
  godoc, not a red test.
- **The ceiling does not make 2 GiB safe.** A caller who raises a cap to 100 MiB still gets no warning; the
  *default* is what protects them. Spec 018 §8 flags default-observability as a later question.
- **One more file shared with the sibling increment.** `sizing_option_class_gate_test.go` is touched by both
  Plan 031 (ADR 0033 D-AL) and Plan 032 (D-AS). Whichever lands second rebases.
