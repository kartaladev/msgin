# ADR 0034 — A byte cap is bounded by what a `[]byte` can represent, not by what a payload might be

- **Status:** **PROPOSED — revision 2, post-audit-round-1, NOT accepted.** Written before any code, per
  [CLAUDE.md](../../CLAUDE.md)'s design-time gate. **Round 1 of the adversarial design audit has run** over the
  assembled bundle ([Spec 018](../specs/018-byte-cap-ceilings.md) + this ADR +
  [Plan 032](../plans/032-byte-cap-ceilings.md)) and returned **NOT SAFE TO IMPLEMENT** — 3 BLOCKERs, 7 MAJORs,
  4 MINORs. The record is [`docs/plans/032-audit-round-1.md`](../plans/032-audit-round-1.md) (immutable); this
  revision folds every finding back. **Round 2 has not run.**
  - 🔴 **Decisions D-AM through D-AT were taken WITHOUT USER RATIFICATION.** The user was not asked. Every
    decision below is **open to reversal**; [Spec 018 §8](../specs/018-byte-cap-ceilings.md) lists the four that
    most deserve a second look, and each decision here carries a **REVERSIBILITY** line stating what undoing it
    costs.
  - **What round 1 changed here:** **D-AN(a)** rewritten (M-5 — the justification sentence was false in both
    directions); **D-AN(b)** now addresses `endpoint.WithMaxPayloadBytes` head-on (M-7); **D-AP** rewritten to
    carry the tested `(n int)` rejection (M-6); **D-AQ** gains a covering test for its non-`Permanent` claim
    (M-8); **D-AR(a)** now records that the ceiling **retires** a shipped `MaxInt64` regression probe (B-2);
    **D-AR(b)**'s 386 command corrected (M-4); **D-AS** re-derived mechanically as 12 sites and re-expressed as
    deltas with an explicit Plan 031 ordering (B-3, M-9); **D-AT** now lands in one commit with the code
    (B-1, m-14).
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
   `deferred` arm rather than in Plan 030's 32-bit overflow list. Verified with the **corrected** commands
   (round-1 M-4): `GOARCH=386 GOOS=linux go vet ./...` → exit `0`, and `GOARCH=386 GOOS=linux go build ./...` →
   exit `0`. 🔴 Revision 1 cited `go test -gcflags=all=-e -run=NONE ./...` and called it clean; **that command
   exits `1` on an untouched tree** (`exec format error`, all 11 root packages). See D-AR(b).
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
failure-mode clause to make these three a separate class would either amend the criterion — obliging a re-check of
all 19 gate rows against it, and this census took five audit rounds to stabilise — or maintain two overlapping
criteria, which is the hand-maintained census D-AB exists to kill. A taxonomy change that alters no outcome is not
worth that.

> **Cost corrected (round-1 m-12).** Revision 1 said splitting *"re-opens all 19 gate rows"*. **As an edit count
> that is wrong — it moves 3 rows and adds one `byArm` key.** The cost is the criterion re-check, not the diff.
> The conclusion stands without the inflation.

**What the difference DOES buy** is the remedy's shape (**D-AN**): a ceiling expressed in the knob's **domain
unit** and justified by *what the knob means* (D-Z's nine) is unavailable here, because the domain unit is
**bytes** and the library does not know the caller's payload. So the ceiling must be justified in a different
currency, or not exist.

**REVERSIBILITY:** free as prose, expensive in consequence. Splitting the class means amending ADR 0032 D-AB and
re-deriving all 19 class-gate rows against a second criterion. Nothing below depends on the split; everything
below depends on the *remedy*.

### D-AN — the remedy is a **representability** ceiling, and **no off-state**

Two decisions that stand or fall together, because each is the reason the other is acceptable.

**(a) A ceiling justified by WIDTH SAFETY — by what every target platform can represent, not by what a payload
might be.**

> 🔴 **REWRITTEN in revision 2 (round-1 M-5).** Revision 1's sentence — *"A cap above that cannot be honoured
> anywhere the library builds"* — is **false in both directions**, and the gate-clearance argument rested on it.
> It is false on 64-bit, where `int` is 64-bit and `WithMaxBodyBytes(3<<30)` is honoured **exactly** today on a
> machine with the memory; and false on 32-bit, where ~3 GiB of address space and `io.ReadAll`'s ~2× doubling
> peak mean a cap *at* `math.MaxInt32` is not obeyable either. Revision 1 then demoted the one unconditionally
> true statement — width safety — into a footnote headed *"none is the primary argument"*. **It is the
> argument.** The *value* does not change.

All three reads terminate in a single contiguous in-memory `[]byte`. A slice's `len`/`cap` are `int`, whose width
is **`GOARCH`-dependent**: 64-bit on `amd64`/`arm64`, 32-bit on `386`/`arm`/`mips`/`mipsle`.

> **`math.MaxInt32` is the largest value for which the cap is exactly representable as an `int` on EVERY `GOARCH`
> this module builds for — so one configuration means the same thing on all of them.**

Equivalently: `n <= math.MaxInt32` guarantees `int(n)` is lossless everywhere. Above it, the knob's meaning
becomes word-size-dependent — legal and honourable on 64-bit, inexpressible as a slice length on 32-bit — and a
configuration whose semantics depend on the deployment's word size is [Spec 016 §1.1](../specs/016-sizing-option-bounds.md)'s
inversion in a new costume.

**Stated plainly so no reader is later surprised:** on 64-bit a higher cap **is** honourable. This ceiling is a
**deliberate portability choice**, not a claim that larger reads are impossible.

**This is what survives CLAUDE.md's gate where a policy number does not.** **State the objection at full strength
before answering it:** *"a ceiling is still a number the library picked; calling it a ceiling rather than a
default is a relabelling."* Correct — for ceilings in general. The test is whether the ceiling's justification
mentions the caller:

| Candidate | Justification | Verdict |
|---|---|---|
| `1 << 30` (1 GiB) | *"no sane API sends more"* | a payload guess in a ceiling's clothes — **fails the gate** |
| `1 << 32` (4 GiB) | *"a round number above anything real"* | the same guess, **and** its meaning differs by word size — fails harder |
| **`math.MaxInt32`** | *"the largest cap exactly representable as an `int` on every `GOARCH` in scope"* | a property of the language on the platforms in scope — **passes** |

**The argument licenses exactly one value, not a family.** `math.MaxInt32` is not "a large round number" — it is
the *only* value at which the same-everywhere property holds. One lower forfeits it for nothing; one higher breaks
it. That is why D-AO's *value* is load-bearing rather than a tuning detail.

**(b) No off-state.** `msghttp` gains no way to say "unbounded". A named exported constant
(`msghttp.Unbounded`) is new exported surface on a pre-v1 API being kept deliberately small, whose **only**
purpose is to re-enable the hazard the class exists to close — which is [Spec 016 §5](../specs/016-sizing-option-bounds.md)'s
rejection of `WithUnsafeUnboundedSizing`, verbatim. A magic value is undiscoverable, and `-1`/`0` are taken.

> 🔴 **The counter-example this ADR must answer, and revision 1 did not mention (round-1 M-7).**
> **`endpoint.WithMaxPayloadBytes` (`endpoint/flowcontrol.go:144`) is a shipped byte cap in this repository that
> HAS an off-state** — `n <= 0` disables it — needing **zero** new exported surface, and its godoc justifies that
> with *precisely* the CLAUDE.md sentence this bundle cites in its own support: *"n <= 0 disables the cap (the
> default): **a library cannot guess a caller's legitimate maximum, so the cap is opt-in.**"* The class gate files
> it in the `safe` arm at `math.MaxInt` (`sizing_option_class_gate_test.go:669`). Revision 1 argued "no off-state"
> from a uniform project stance; `grep -c 'WithMaxPayloadBytes'` across all three bundle files returned `0/0/0`.
> **There is no such stance, and this ADR no longer claims one.**

**Why `msghttp` differs — a fact about which sentinel values are already spoken for, not a doctrine:**

| | `endpoint.WithMaxPayloadBytes` | the three `msghttp` byte caps |
|---|---|---|
| Default | **off** | **on** — 1 MiB, protective |
| Posture | **opt-in hardening** — the caller adds protection | **protective by default** — the caller can only relax it |
| What `n <= 0` means | **"disabled"**; the value was idle | **already a shipped typed rejection** (`options.go:1189-1193`, `:1201-1205`, `:1211-1215`) |
| Cost of an off-state | **zero** | **a new exported constant** — `-1` and `0` are taken |

Where a knob is off by default, `n <= 0` is free and can carry "off" for nothing. Where a knob is protective by
default and already rejects `n <= 0` with its own sentinel, an off-state costs new exported surface whose only
purpose is to re-enable the hazard.

**Disposition: the divergence is ACCEPTED and recorded, not harmonised.** Two byte caps in this workspace have
different off-state semantics, for the stated reason. Whether `endpoint.WithMaxPayloadBytes` should gain a
representability ceiling on its *positive* range is a **recorded follow-up**, out of scope here — different
package, different gate arm ([Spec 018 §3.4a](../specs/018-byte-cap-ceilings.md), §8 open item 2).

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

**Width safety is D-AN(a)'s argument, not a fourth benefit** (round-1 M-5 — revision 1 listed it here as an
afterthought). `n <= math.MaxInt32` guarantees `int(n)` is lossless on **every** `GOARCH`, so a future refactor
doing `make([]byte, n)` or `int(n)` cannot silently truncate. The current `int64`-with-no-upper-bound shape carries
that hazard latently; this closes it. The table row *"Largest `len([]byte)` on 32-bit `GOARCH` — exactly
2,147,483,647"* is the same fact stated as a measurement.

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

### D-AP — keep `(n int64)`; add a sibling `checkRangeInt64` — not a generic, and never an `int(n)` conversion

**(a) The signature stays `(n int64)`, and that is now a DECISION rather than a premise (round-1 M-6).**
Revision 1 asserted *"No signature change"* as a stated fact and put `(n int)` in **neither** rejected-alternatives
table, while four decisions here rested on it. Round 1 called that out. It has now been tested.

**The case FOR `(n int)`, at full strength.** Narrowing the parameter would delete the need for
`checkRangeInt64` (the shipped `checkRange` would be used verbatim); **dissolve D-AR(b)'s accepted mutation gap**,
because with no `int64`→`int` conversion anywhere there is no truncation mutant to survive; turn
`WithMaxBodyBytes(1<<62)` into a **compile** error on 32-bit rather than a runtime one; and match
`endpoint.WithMaxPayloadBytes(n int)`, this repo's other byte cap. It is free in the compatibility sense — pre-v1,
no tags, no consumers.

**It was tried on a probe module and it fails, on the one property this increment cannot give up:**

> **With `(n int)` and `byteCapCeiling = math.MaxInt32`, the ceiling on a 32-bit build IS `math.MaxInt` — so no
> `int` literal can exceed it, and the upper arm becomes INEXPRESSIBLE rather than merely untested.**

```
$ GOARCH=386 GOOS=linux go build ./...          # func WithMaxBodyBytes(n int)
./p.go:12:23: cannot use 2147483648 (untyped int constant) as int value in argument to WithMaxBodyBytes (overflows)
./p.go:13:23: cannot use 1 << 62 (untyped int constant 4611686018427387904) as int value in argument to WithMaxBodyBytes (overflows)

$ GOARCH=386 GOOS=linux go build ./...          # constant-conversion workaround
./p.go:12:14: constant 2147483648 overflows int  # int64(math.MaxInt32)+1 is STILL a constant expression
```

Consequences, each verified: [Spec 018 §6 AC-2](../specs/018-byte-cap-ceilings.md)'s `2147483648` literal would
not compile on 386, so **AC-3's replacement gate (`GOARCH=386 go vet ./...`, which type-checks test files) would
go red** — undoing what Plan 030 just delivered; **no `fixed`-arm literal exists** that is both compilable on 386
and above the ceiling, so the class gate's three moved rows would become architecture-conditional, splitting a
single-file normative gate; and the only escape is deliberate signed **runtime overflow**
(`n := math.MaxInt32; n++` → `-2147483648` on 386), which exercises the **lower** arm under a different render.

**Decision: `(n int64)`.** The `int` shape trades a hypothetical mutant — one that exists only in a
mis-implementation nobody has written — for the loss of a real, uniform, cross-architecture assertion. D-AR(b)'s
gap is *recorded*; an inexpressible upper arm could not be.

**Two corollaries, because a future reader will re-open this.** (1) `1 << 30` cannot serve the three moved gate
rows under **either** signature: `1,073,741,824 < 2,147,483,647`, so it would be **accepted**. (2) A future retype
to `int` would not hide the knobs from the class gate — half 1's AST walk accepts either width
(`sizing_option_class_gate_test.go:234`, `return t.Name == "int" || t.Name == "int64"`). That is a fact about the
gate, not a licence.

**(b) The helper.**

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

**REVERSIBILITY:** (b) is free — one unexported function in one file; promoting it to a generic later is
mechanical if a second package ever needs an `int64` range check. **(a) is not free**: reversing to `(n int)`
means re-doing Spec 018 §6 AC-2, AC-3 and AC-4 for an architecture-conditional gate, and is an **exported-surface
change** (three signatures) that `apidiff` would report — see Consequences.

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

**Test-safety — the check was WRONG, and is replaced (round-1 B-2).** Grepping the three *message strings*
across `--include='*.go'` does return only the three declarations, and no test asserts the wording. But that
question is not the risk. The risk is a test depending on the current **acceptance** of a value, which a
message-string grep cannot see — and one existed. **The check is now a call-site enumeration**, classified in
full in [Spec 018 §3.1a](../specs/018-byte-cap-ceilings.md):

```bash
grep -rn 'WithMaxBodyBytes(\|WithMaxResponseBytes(\|WithMaxEventBytes(' --include='*_test.go' .
```

**Not `Permanent`-wrapped** (ADR 0029 **D-M**): these are constructor returns and never travel through a
`MessageHandler`, so a retry classification on them is meaningless. 🔴 **Revision 1 stated this and no acceptance
criterion tested it** (round-1 M-8) — an untested typed-error claim, which CLAUDE.md's hot-path rule forbids.
[Spec 018 §6 AC-2b](../specs/018-byte-cap-ceilings.md) now requires
`assert.False(t, msgin.IsPermanent(err), …)` on every rejecting case, matching the shape the class gate's `fixed`
arm already carries on eight of its nine rows.

**REVERSIBILITY:** the wrap is three `checkRangeInt64` call sites; the message text is three string literals.

### D-AR — the sixth read is **bounded, not restructured**; and the 32-bit guard's proof is a compile arm

Two acknowledgements that belong in the record rather than in a future rediscovery.

**(a) `exchange.go:130-131` stays as it is — but its MaxInt64 regression probe is RETIRED.**
*"`drainBounded` is 5 of 6 reads"* is evidence that the earlier *safe* verdict rested on the wrong five sites —
**it is not a to-do list**. The sixth read produces the reply payload; discarding it is not an option. After this
ADR, `max` at that site is bounded by `byteCapCeiling` instead of by `1<<62`, and all six reads inherit the same
finite bound from the same field. Nothing moves in the production code. Say so explicitly, because *"5 of 6"*
reads like an omission and the next reader will try to close it.

> 🔴 **The half revision 1 omitted, and it was a BLOCKER (round-1 B-2).** `exchange.go:133`'s
> `int64(len(body)) == max` INV-6 check is unaffected — but the test that exercised it at the overflow boundary is
> not. `adapter/http/exchange_test.go:613` — *"branch 20: `WithMaxResponseBytes(MaxInt64)` returns a non-empty
> body intact, **the overflow regression**"* — passes `math.MaxInt64` through a helper that `require.NoError`s on
> construction (`:590-596`). **After this ADR that input is unreachable through the public API, so the ceiling
> RETIRES the probe.** Revision 1's §1.3 claim that INV-6 is *"unaffected"* was true of the production code and
> false of its test, and `exchange_test.go` was in no task's file list.
>
> **What still covers INV-6 afterwards:** branches 18 and 19 (`:598-611`) exercise the `==` comparison at its
> boundary from both sides at a small cap, which is where the comparison's correctness lives. What is lost is the
> *magnitude* assurance — and that is now supplied **structurally**: no `max` above `byteCapCeiling` can exist, so
> the overflow band is unreachable rather than untested. Branch 20 is rewritten to the ceiling value and its
> header comment (`:577-578`) updated to record why (Spec 018 §6 AC-2c).

**(b) The width guard cannot be behaviorally mutation-killed on this project's reference platform.** D-AP's reason
to exist is invisible on `darwin/arm64`, where `int(1<<62)` does not truncate. The shipped guard is therefore a
**build-and-vet arm** —

```bash
GOTOOLCHAIN=go1.25.13 GOARCH=386 GOOS=linux go vet ./...      # exit 0 — TYPE-CHECKS TEST FILES
GOTOOLCHAIN=go1.25.13 GOARCH=386 GOOS=linux go build ./...    # exit 0 — non-test code
```

> 🔴 **Command corrected (round-1 M-4).** Revision 1 prescribed `go test -gcflags=all=-e -run=NONE ./...` and
> Spec 018 called it *"verified clean"*. **It exits `1` on an untouched tree** — `FAIL` for all 11 root packages,
> `exec format error`. A gate that is red before any edit cannot detect a regression after one. `go vet` is the
> usable form precisely because it type-checks `_test.go` files, which is where the 32-bit exposure lives, and it
> exits `0`.

Neither command **executes** anything on 386, so the `checkRange(…, int(n), …)` mutant is caught by review and by
godoc, not by a red test — it still compiles and still passes on 64-bit. **This is an accepted gap, stated rather
than papered over.** Closing it needs a 386 runner in CI or `qemu`, which is a CI decision outside this
increment's scope. Recording it here means the next author knows the guard's strength precisely instead of
assuming a mutant was killed.

**REVERSIBILITY:** (a) free — reversing means designing a streaming payload type, a different increment. (b) free
— adding a 386 CI runner later strengthens the guard without changing any code here.

### D-AS — the class gate's `deferred` arm is **emptied**, and its name is kept as a tombstone

`sizing_option_class_gate_test.go` files all three knobs in `deferred`. Moving them is **at least twelve**
coordinated edits, **derived mechanically** and enumerated in
[Spec 018 §6 AC-4.1](../specs/018-byte-cap-ceilings.md).

> 🔴 **Revision 1 said SEVEN, and every offset but one was stale (round-1 B-3).** The cited offsets predated Plan
> 030's already-landed conversion (`d2c69fe`), which moved the rows from `:519/:528/:537` to `:570/:579/:588`,
> `wantArms` from `:726-728` to `:782-784`, `byArm` from `:747` to `:803` and the `arm` doc from `:362` to `:401`;
> the "🔴 block" is **two** blocks, not one. **Do not transcribe a site list — derive it**
> (`grep -n 'deferred\|DEFERRED\|9/1/3/6\|9 + 1 + 3 + 6' sizing_option_class_gate_test.go`), which is this
> project's stored lesson. Three traps, not two:

1. **`byArm` is built by counting, so an emptied arm has no key.**
   `{"fixed": 12, "rejects": 1, "deferred": 0, "safe": 6}` **fails** — Go's counting map never produces a zero
   entry. The `deferred` key must be **removed**, not zeroed.
2. **The 🔴 comment blocks above the deferred rows are instructions for a future contributor** — *"WHEN §3.8's
   CEILING LANDS, THIS GATE WILL GO RED, AND THAT IS CORRECT … the repair is to MOVE the row into the `fixed`
   arm."* **This increment is that event.** Both blocks (`:547-556` and `:557-568`) are rewritten into a record of
   what happened, so the next reader is not told to wait for something already done. A mechanical row-move misses
   this, and the warning about weakening the production check is **generalised, not deleted**.
3. 🔴 **Plan 030's header block (`:40-59`) goes false in TWO independent ways, and revision 1 missed it
   entirely.** It declares the oversized literal to be a function of the **arm**. **(a)** After the move, `fixed`
   holds 12 rows carrying **two** literals — 9 at `1<<30`, 3 at `1<<62` — so the arm→literal mapping is no longer
   true of any arm. **(b)** Its *"deferred (3) → still 1<<62"* bullet loses its referent. **Restate the surviving
   invariant, which is the true one: the literal is chosen by the option's PARAMETER TYPE** — `int` → `1<<30`,
   `int64` → `1<<62`. And note that **`1 << 30` cannot be used for the three moved rows**:
   `1,073,741,824 < byteCapCeiling`, so it would be **accepted**.

**Keep `"deferred"` in the `arm` field's documented vocabulary with a tombstone** — *(no members as of Plan 032 —
see Spec 018)* — mirroring how ADR 0032 D-AB retained safety causes **(c)** and **(d)** as tombstones so the
surviving identifiers kept their letters. Delete it from the counts map (it must be absent) and from the header's
arm list. **Do not delete the concept:** a future knob whose remedy is genuinely deferred needs the arm back, and
the tombstone tells the next author it existed and why.

**The counts are stated as DELTAS, not literals (round-1 M-9).** [Plan 031](../plans/031-group-member-bounds.md) /
[ADR 0033](0033-group-member-bounds.md) **D-AL** edits this same file by hand, taking `sizingConformanceKeys` from
17 to 19 keys and the partition to `11 fixed + 1 rejects + 3 deferred + 6 safe = 21 rows` — **falsifying all three
absolutes revision 1 declared normative** (`require.Len(t, tests, 19)`, `sizingConformanceKeys` unchanged,
`{"fixed": 12, "rejects": 1, "safe": 6}`). *"Whichever lands second rebases"* is adequate for a textual conflict
and inadequate when the second lander's acceptance criteria **are** the literals.

| Quantity | This increment's delta |
|---|---|
| `require.Len(t, tests, N)` | **+0** — three rows change arm; none is added or removed |
| `sizingConformanceKeys` | **unchanged by 032** |
| `byArm["deferred"]` | **key REMOVED** (3 → absent). Verified: Plan 031 revision 2 adds both its rows to **`fixed`** and touches `deferred` not at all, so this holds under either order |
| `byArm["fixed"]` | **+3** from whatever it then is |
| `byArm["rejects"]`, `byArm["safe"]` | **+0** |
| half 1 (the AST completeness walk) | **unchanged** — the three functions still exist, still `int64`; `:234` accepts either width regardless |

Both landing orders and their consequences are tabulated in
[Spec 018 §6 AC-4.2](../specs/018-byte-cap-ceilings.md). **Spec 016 §2.1's arm table is written by whichever
increment lands SECOND**, in one edit covering both; the first lander updates only its own rows and records that
the other is pending. Recording this is what stops both increments claiming the table or both skipping it.

**REVERSIBILITY:** free — the arm partition is test data. But note it is **normative**: Spec 016 §2.1 and §6 AC-5
fix every key's arm, so moving a row is a **spec change**, which is why D-AT amends the parents in the same
commit.

### D-AT — deliver Spec 016 §3.8 item 2's undelivered godoc, and amend every artifact that records the old arm

**(a) The hazard disclosure ships here, in its superseding form.** Spec 016 promised it and Plan 029 never
scheduled it (Context, fact 4). After a ceiling exists, the honest disclosure is not *"this knob is unbounded"* but:

> **The ceiling is not a safety guarantee** — this option is the only bound on a read driven by a remote peer, and
> raising it above the 1 MiB default trades flood protection for payload size.

paired with the range, the ceiling's value, its representability rationale, and the typed error.

**(a-bis) The production change, its godoc and the gate move land in ONE commit (round-1 B-1, m-14).**
Revision 1 split them across Plan 032 Tasks 1/2/3 and then required, in Task 3 Step 2, that the root suite be
*"already red after Task 1"* — which contradicts the plan's own Global constraint 8 (*"each task is a green
unit"*) and CLAUDE.md's per-task-commit pre-authorization, conditioned on green. **The class gate is in the same
module the production change edits; the coupling is real and cannot be scheduled away.** Merging also closes the
window in which six godoc sentences (`options.go:458`, `:764`, `:851`; `errors.go:14`, `:72`, `:132`) describe a
constructor that no longer behaves that way.

**(b) Every artifact recording the `deferred` arm of 3 is amended in the same commit.** Spec 016 §2.1 (census
table + arm table), §3.8, §6 AC-5's arm table; ADR 0032 **D-AB**; `docs/HANDOVER.md` §7 item 6.
**A bundle that changes a knob's class without moving every row that records it is exactly the "stopped ONE FILE
SHORT" failure Spec 016 revision 6 opened with** — seven twins survived a reclassification because the cross-file
grep guard *"was written and not run."* Plan 032 **Task 2** carries that guard and **runs it**, pasting the output.

**REVERSIBILITY:** free — both halves are prose. Omitting (a) would leave a two-increment-old promise
outstanding; omitting (b) would ship a bundle that contradicts itself.

---

## Consequences

**Positive**

- **The class is closed** for the three knobs Spec 016 §3.8 deferred. Taken alone, ADR 0032's census becomes
  **12 fixed + 1 rejects + 6 safe** with **no deferred remedy**; taken together with Plan 031 the absolutes differ
  (D-AS's delta table). Either way, **no `msghttp` byte cap has a deferred remedy**, and the gate stops asserting
  that an unbounded remote-driven read is conformant.
- **Every `msghttp` byte cap is finite**, so `n` is no longer the sole *unbounded* bound on any accumulation the
  criterion covers.
- **A latent 32-bit truncation hazard is closed** (D-AO's width safety), on three options whose `int64` type is
  the reason they escaped Plan 030's 386 sweep.
- **Debuggability improves at both arms**: `msghttp: max body bytes out of range: msghttp.WithMaxBodyBytes: 0 not in [1, 2147483647]`
  replaces a bare sentinel that named neither the value nor the site.
- **Zero exported-surface change.** `apidiff`: 0 removals / 0 additions. `byteCapCeiling` and `checkRangeInt64`
  are unexported. This is a **consequence of D-AP(a)**, not a constraint that produced it — narrowing to
  `(n int)` would have been a three-signature exported-surface change, free at pre-v1 and reportable by
  `apidiff`; it was rejected on 32-bit testability, not on compatibility.
- **A two-increment-old broken promise is discharged** (D-AT(a)).

**Negative / accepted**

- **A behavioral break at pre-v1:** `WithMaxBodyBytes(1<<62)` goes from legal to a construction error, and a
  caller wanting >2 GiB in one in-memory payload has **no escape** (D-AN(b)). Free today — no tags, no consumers
  — and additive to reverse.
- **Three rendered messages change**, including on the already-shipped lower arm (D-AQ). `errors.Is` is
  unaffected; no test asserts the strings today.
- **~7 duplicated lines** for `checkRangeInt64` (D-AP), and a fifth range-checking helper in the workspace once
  ADR 0033 lands its own.
- **The width guard is not behaviorally mutation-killed** on the reference platform (D-AR(b)) — a build-and-vet
  arm and godoc, not a red test. **`(n int)` would have removed this gap entirely** and was rejected for a
  different reason (D-AP(a)); the trade is stated so nobody re-discovers it as an oversight.
- **A shipped regression probe is retired.** `adapter/http/exchange_test.go` branch 20's `MaxInt64` input becomes
  unreachable through the public API (D-AR(a)). The probe is rewritten to the ceiling, and the magnitude
  assurance it carried is replaced by a structural one.
- **Two byte caps in this workspace now have different off-state semantics** — `endpoint.WithMaxPayloadBytes`
  disables at `n <= 0`, the three `msghttp` caps reject it (D-AN(b)). Accepted and recorded, with the reason;
  a ceiling for `WithMaxPayloadBytes` is an open follow-up.
- **The ceiling does not make 2 GiB safe.** A caller who raises a cap to 100 MiB still gets no warning; the
  *default* is what protects them. Spec 018 §8 flags default-observability as a later question.
- **One more file shared with the sibling increment.** `sizing_option_class_gate_test.go` is touched by both
  Plan 031 (ADR 0033 D-AL) and Plan 032 (D-AS). Whichever lands second rebases — **and because Plan 031 changes
  the very counts Plan 032 asserts, this increment's gate effect is specified as DELTAS with both landing orders
  written out** (D-AS), not as literals.
