# Plan 032 — adversarial design audit, round 1 (2026-08-22)

Independent Opus subagent, handed the **complete revision-1 bundle together** —
[Spec 018](../specs/018-byte-cap-ceilings.md) + [ADR 0034](../adrs/0034-byte-cap-ceilings.md) +
[Plan 032](032-byte-cap-ceilings.md) — **before any implementation code existed**, per
[`CLAUDE.md`](../../CLAUDE.md)'s design-time gate. The plan is part of what was audited, not merely the context
for it.

**Traceability.** Audits: [Spec 018](../specs/018-byte-cap-ceilings.md),
[ADR 0034](../adrs/0034-byte-cap-ceilings.md), [Plan 032](032-byte-cap-ceilings.md). Parent artifacts whose
contracts are implicated: [Spec 016](../specs/016-sizing-option-bounds.md),
[ADR 0032](../adrs/0032-sizing-option-bounds.md), [Plan 029](029-sizing-option-bounds.md),
[Plan 030](030-post-029-maintenance.md), [Plan 031](031-group-member-bounds.md),
[ADR 0031](../adrs/0031-nil-option-elements.md), [ADR 0029](../adrs/0029-eip-lexical-alignment.md). Parent
backlog: [`docs/HANDOVER.md`](../HANDOVER.md) §7 item 6.

This record is **evidence-primary** — it is the audit artifact, not a user-facing summary. Every structural claim
in the bundle was re-derived on this tree (`main` at `1212c63`, `GOTOOLCHAIN=go1.25.13`, darwin/arm64); the
commands and their output are pasted below.

> **⚠️ IMMUTABLE EXECUTION RECORD.** This file records what round 1 found, at the time it found it. Do not edit it
> to reflect later decisions, later measurements, or the revised bundle. Corrections belong in a later round's
> record or in the artifacts themselves. The coordinator's dispositions for these findings live in **Spec 018 /
> ADR 0034 / Plan 032 revision 2**, each of which cites this file.

**Verdict: NOT SAFE TO IMPLEMENT.** 3 BLOCKERs, 7 MAJORs, 4 MINORs.

The bundle's *reasoning* is strong: the representability argument is genuinely different from a payload guess, the
"same class, different remedy" call is right, and the `deferred`-arm-emptying traps (a counting map has no zero
key; the 🔴 block is instructions that must become a record) are both real and correctly identified. What fails is
**derivation against the tree**. Every line offset in the gate's edit inventory predates Plan 030's already-landed
conversion; the inventory is 7 sites of at least 12; a certified-clean acceptance command is red on this tree
right now; the load-bearing justification sentence is false in both directions on 64-bit and on 32-bit; and the
single strongest counter-example to the "no off-state" decision — a shipped byte cap in this repo that has one —
appears in none of the three files.

---

## Finding index

| # | Rank | One line |
|---|---|---|
| **B-1** | BLOCKER | Task 1 cannot be a green commit — Task 3's own RED precondition contradicts Global constraint 8 |
| **B-2** | BLOCKER | `adapter/http/exchange_test.go` branch 20 breaks under Task 1 and is in no task's Files list |
| **B-3** | BLOCKER | The class-gate edit inventory is 7 sites of ≥12, and every offset but one predates Plan 030 |
| **M-4** | MAJOR | Spec §6 AC-3's "verified clean" acceptance command is RED on this tree |
| **M-5** | MAJOR | The ceiling's primary justification sentence is false on 64-bit **and** on 32-bit |
| **M-6** | MAJOR | `(n int)` was never considered — asserted as a premise, absent from both rejected-alternatives tables |
| **M-7** | MAJOR | `endpoint.WithMaxPayloadBytes` is a shipped byte cap **with** an off-state; neither artifact mentions it |
| **M-8** | MAJOR | Task 3 edit 2 prescribes the wrong string, and drops the `IsPermanent` assertion the arm carries |
| **M-9** | MAJOR | The Plan 031 collision is not a mechanical rebase — three normative claims go false if 031 lands first |
| **M-10** | MAJOR | Spec §6 AC-1 requires an accessor assertion that Global constraint 2 forbids |
| **m-11** | MINOR | `WithReplayBuffer` cites Spec 016 **§1.5**, not §1.3; and both sibling cites use godoc lines |
| **m-12** | MINOR | "Splitting re-opens all 19 gate rows" is inflated; the conclusion is right on other grounds |
| **m-13** | MINOR | Global constraint 6 ("no test reads more than 1 MiB") vs branch B1-4's 2 MiB fixture |
| **m-14** | MINOR | Six godoc sentences go false between Task 1's commit and Task 2's |

---

## BLOCKER B-1 — Task 1 cannot be a green commit; Task 3's RED precondition contradicts Global constraint 8

**The claim under attack.** Two statements in the same plan:

- Global constraint 8: *"**Each task is a green unit** — `GOWORK=off go test ./... -race -shuffle=on` passes in
  the root module before its commit. No WIP or broken-build commits."*
- Task 3 Step 2 (RED): *"Run `GOWORK=off go test -run TestSizingOptionClass ./... -race`. **It must already be
  red** after Task 1 … **Confirm exactly three failures and no others.** If it is green, Task 1 did not land."*

**The evidence.** `sizing_option_class_gate_test.go` is `package msgin_test` in the **root module** — the same
module Task 1 edits — and its three `deferred` rows call `require.NoError` on exactly what Task 1 makes an error:

```
$ grep -n 'arm: "deferred"' -A 4 sizing_option_class_gate_test.go
570:			arm: "deferred", // class member, remedy deferred — Spec 016 §3.8
571:			assert: func(t *testing.T) {
572:				cfg, err := msghttp.NewConfig(msghttp.WithMaxBodyBytes(1 << 62))
573:				require.NoError(t, err)
574:				require.NotNil(t, cfg)
579:			arm: "deferred", // class member, remedy deferred — Spec 016 §3.8
581:				cfg, err := msghttp.NewConfig(msghttp.WithMaxEventBytes(1 << 62))
588:			arm: "deferred", // class member, remedy deferred — Spec 016 §3.8
590:				cfg, err := msghttp.NewConfig(msghttp.WithMaxResponseBytes(1 << 62))
```

Task 3 does not merely *observe* that the gate is red after Task 1 — it **requires** it as the task's entry
condition. So Task 1's commit is, by the plan's own design, a commit whose root-module suite fails. The two
statements cannot both hold.

This is not a naming quibble. CLAUDE.md's per-task-commit pre-authorization is explicitly conditioned on *"Each
task must be a **green unit** — its `go test ./... -race` passes — before its commit; no WIP/broken-build
commits."* Task 1 as written is not pre-authorized to commit at all.

**Required fix.** Merge the tasks so that the production change and the gate move land in one commit, or move the
gate edit into Task 1. Whichever is chosen, Global constraint 8 and Task 3 Step 2 must stop contradicting each
other; deleting one of the two sentences is not a fix, because the underlying coupling is real.

---

## BLOCKER B-2 — `adapter/http/exchange_test.go` branch 20 breaks under Task 1, and is in no task's Files list

**The claim under attack.** Spec §3.1 and ADR D-AQ: *"**Verified test-safe this revision:**
`grep -rn 'max body bytes must be\|max response bytes must be\|max event bytes must be' --include='*.go' .`
returns **only the three declarations in `errors.go`**; no test asserts any of the strings."* Plan Task 1 Step 1
re-runs the same grep as its stop-and-reassess check.

**The evidence.** The check is a grep for **message strings**. It is structurally blind to a test that depends on
the current *acceptance* of a value rather than on the current *wording* of an error. Exactly one such test
exists:

```
$ grep -rn 'WithMaxResponseBytes(math.MaxInt64)' --include='*_test.go' .
adapter/http/exchange_test.go:615:		x := newExchange(t, http.StatusOK, io.NopCloser(strings.NewReader("hello")), msghttp.WithMaxResponseBytes(math.MaxInt64))
```

`newExchange` `require.NoError`s on construction:

```
$ sed -n '590,596p' adapter/http/exchange_test.go
	newExchange := func(t *testing.T, code int, body io.ReadCloser, opts ...msghttp.Option) *msghttp.Exchange {
		t.Helper()
		all := append([]msghttp.Option{msghttp.WithHTTPClient(clientReturning(code, body))}, opts...)
		x, err := msghttp.NewExchange("https://example.test/rpc", all...)
		require.NoError(t, err)
		return x
	}
```

After Task 1, `math.MaxInt64 > byteCapCeiling` and `NewExchange` returns an error, so `require.NoError` fails and
`TestExchange_bodyBounds/branch_20` goes red. **`adapter/http/exchange_test.go` appears in no task's Files list**
(Task 1: `options.go`, `helpers.go`, `errors.go`, `options_test.go`; Task 2: `options.go`, `helpers.go`,
`errors.go`; Task 3: `sizing_option_class_gate_test.go`; Task 4: docs only).

**Second-order, and the more important half.** Branch 20 is not incidental — it is a **regression probe for INV-6's
arithmetic at `MaxInt64`**, declared as such in the test's own header:

```
$ sed -n '575,581p' adapter/http/exchange_test.go
// TestExchange_bodyBounds covers branches 18-23, 20b (INV-6 and INV-7 body
// lifecycle): a body exactly at cap succeeds intact (18); cap+1 -> ErrReplyTooLarge
// (19); WithMaxResponseBytes(MaxInt64) returns a non-empty body intact, the
// overflow regression (20); an over-cap body whose boundary read returns (0, nil)
```

The ceiling makes `MaxInt64` **unreachable through the public API**, so the ceiling does not "break" branch 20 —
it **retires** it. Spec §1.3 item 2 says of `exchange.go:133`'s `int64(len(body)) == max` probe that it *"is
unaffected by this change, since a bounded `max` is still `int64`"*. That is **true of the production code and
false of its test**: the arithmetic is unchanged, but the only case that exercised it at the overflow boundary
becomes inexpressible. A bundle that retires a shipped regression probe must say so and say what replaces it.

**Required fix.** Three parts. (1) Add `adapter/http/exchange_test.go` to the merged task's Files, rewrite branch
20 to the ceiling value, and update its header comment. (2) **Replace the test-safety check** with one that
covers the real property — enumerate every test call site of the three options and classify each as in-range or
out-of-range — and paste the classification into the spec. (3) Record in the ADR that the ceiling retires the
`MaxInt64` overflow probe, and state what still covers INV-6's arithmetic.

---

## BLOCKER B-3 — the class-gate edit inventory is 7 sites of ≥12, and every offset but one predates Plan 030

**The claim under attack.** Spec §6 AC-4's seven-row table and Plan Task 3's identical seven-row table, each
introduced as the complete set: *"every one of the following must move in the same commit or the gate goes red."*

**The evidence.** The mechanical derivation:

```
$ grep -n 'deferred\|DEFERRED\|9/1/3/6\|9 + 1 + 3 + 6' sizing_option_class_gate_test.go
35://       - "deferred" (3) — accepts 1<<62, annotated so it never reads as a
38://     9 + 1 + 3 + 6 = 19 rows = 17 AST keys + 2 manual rows.
55://   - "deferred" (3) → still 1<<62. These three take an int64, NOT an int, so
58://     — "this knob accepts an absurd value and the remedy is DEFERRED" — mean
401:	arm    string // "fixed" | "rejects" | "deferred" | "safe" — Spec 016 §6 AC-5's four BEHAVIORAL arms, …
539:		// ---- arm: deferred — 3 class members whose ceiling is DEFERRED (Spec 016 §3.8) ----
565:		// DEFERRED, Spec 016 §3.8"), and on 386 math.MaxInt would shrink it to
570:			arm: "deferred", // class member, remedy deferred — Spec 016 §3.8
579:			arm: "deferred", // class member, remedy deferred — Spec 016 §3.8
588:			arm: "deferred", // class member, remedy deferred — Spec 016 §3.8
758:	// split at 9/1/3/6; without this, a contributor could move a row between
761:	// (WithMaxResponseBytes, safe -> deferred) each were — and the suite would
782:		"msghttp.WithMaxBodyBytes":           "deferred",
783:		"msghttp.WithMaxEventBytes":          "deferred",
784:		"msghttp.WithMaxResponseBytes":       "deferred",
801:			"3 with a deferred ceiling (§3.8), 6 safe (4 AST + 2 manual). Moving a row between arms is a "+
803:	require.Equal(t, map[string]int{"fixed": 9, "rejects": 1, "deferred": 3, "safe": 6}, byArm,
805:			"from Spec 016 §2.1's 9/1/3/6 split")
```

**Offset-by-offset, the inventory versus the tree:**

| Inventory site | Cited | Actual | Status |
|---|---|---|---|
| the three rows' `arm` field | `:519`, `:528`, `:537` | `:570`, `:579`, `:588` | **stale** |
| `wantArms` entries | `:726-728` | `:782-784` | **stale** |
| `byArm` count assertion | `:747` | `:803` | **stale** |
| the `arm` field's doc comment | `:362` | `:401` | **stale** |
| the 🔴 block above the rows | `:500-518`, one block | **two** blocks, `:547-556` and `:557-568` | **stale and miscounted** |
| the file header comment | `:35` | `:35` | correct — the only one |

Every stale offset is **pre-Plan-030**. Plan 030's literal conversion has already landed on `main`
(`d2c69fe test(core): make the sizing tests compile on 32-bit`), which is what moved them. The bundle's Global
constraint 10 anticipates drift in `adapter/http/options.go` and `helpers.go` — it does **not** anticipate that
the class gate has already moved, and Task 3 Step 2's "confirm exactly three failures" gives an implementer no
signal that the offsets are wrong, because the row content still matches.

**Missing sites — at least five, all load-bearing:**

| Site | What breaks |
|---|---|
| `:38` | `9 + 1 + 3 + 6 = 19` — the arithmetic identity becomes `12 + 1 + 6 = 19` |
| `:55-59` | Plan 030's per-arm literal block (see below) |
| `:557-568` | the second 🔴 block — *"THESE THREE ROWS KEEP THE 1<<62 LITERAL — DO NOT CONVERT THEM"* |
| `:758`, `:801`, `:805` | three prose strings inside live assertion messages naming the `9/1/3/6` split |

**The worst of them is `:40-59` — Plan 030's header block, which goes false in two independent ways.**

```
$ sed -n '46,59p' sizing_option_class_gate_test.go
// # THE OVERSIZED LITERAL IS NOT ONE VALUE — IT IS THREE, BY ARM (Plan 030 Task 2)
…
//   - "fixed" (9) and "rejects" (1) → 1<<30 = 1,073,741,824. These rows assert
//     an EqualError against a rendered decimal. …
//   - "deferred" (3) → still 1<<62. These three take an int64, NOT an int, so
//     they compile fine on 386 and were never part of the defect. 🔴 DO NOT
//     "finish the job" by converting them: …
```

1. **The arm→literal mapping it declares normative breaks.** After the move, `fixed` holds 12 rows of which 3
   carry `1<<62` and 9 carry `1<<30`. The block's central claim — that the literal is determined by the arm — is
   no longer true of any arm.
2. **Its "deferred (3) → still 1<<62" bullet ceases to have a referent**, because there is no `deferred` arm.

Neither the spec's AC-4 nor the plan's Task 3 mentions `:40-59` at all.

**Aggravating.** `1<<30` cannot rescue the mapping either. `1<<30 = 1,073,741,824` is **below**
`byteCapCeiling = 2,147,483,647`, so the three moved rows at `1<<30` would be **accepted**, not rejected, and
their `require.ErrorIs` would fail on every architecture. The `fixed` arm genuinely holds two literals after this
increment; the header must say so.

**Required fix.** Regenerate the inventory **mechanically** against current `HEAD` with the grep above, paste the
output into the AC, and edit every hit — `:40-59` included. Drop hand-typed offsets in favour of the command.
State the surviving invariant honestly: the literal is chosen by the option's **parameter type** (`int` → `1<<30`,
`int64` → `1<<62`), not by its arm. This project's stored lesson is *derive move-lists mechanically*.

---

## MAJOR M-4 — Spec §6 AC-3's "verified clean" acceptance command is RED on this tree

**The claim under attack.** Spec §6 AC-3: *"**Verified clean on this tree before any edit** — every root package
compiles"*, of

```bash
GOTOOLCHAIN=go1.25.13 GOARCH=386 GOOS=linux go test -gcflags=all=-e -run=NONE ./...
```

restated in ADR Context fact 2 (*"compiles every root package on this tree"*) and in Plan Task 1 Step 4
(*"compiles clean. **Verified clean on this tree before any edit** — it must stay clean"*).

**The evidence.** The command exits **1** and prints `FAIL` for all eleven root packages:

```
$ GOTOOLCHAIN=go1.25.13 GOARCH=386 GOOS=linux go test -gcflags=all=-e -run=NONE ./... 2>&1 | tail -6
fork/exec /var/folders/.../resilience.test: exec format error
FAIL	github.com/kartaladev/msgin/resilience	0.002s
fork/exec /var/folders/.../routing.test: exec format error
FAIL	github.com/kartaladev/msgin/routing	0.001s
FAIL
$ echo $?
1
```

The bundle **already knows this** and says so, one paragraph later, without reconciling it with the
"verified clean" claim:

> ADR D-AR(b): *"`go test -run=NONE` **compiles** the 386 binaries but cannot **execute** them on darwin/arm64
> (`exec format error`)."*

So the bundle simultaneously certifies the command clean and states that it cannot run. A gate whose acceptance
command exits 1 on an untouched tree cannot detect a regression: an implementer running it after Task 1 sees the
same `FAIL` and has no way to distinguish "unchanged" from "broken".

**The usable gate exists and is already available.** `go vet` type-checks test files and exits 0:

```
$ GOARCH=386 GOOS=linux go vet ./... ; echo $?
0
$ GOARCH=386 GOOS=linux go build ./... ; echo $?
0
```

**Required fix.** Replace AC-3's command with `GOARCH=386 GOOS=linux go vet ./...` plus
`GOARCH=386 GOOS=linux go build ./...`, and delete the "verified clean" claim for the `go test -run=NONE` form
wherever it appears (Spec §2, Spec §6 AC-3, ADR Context fact 2, ADR D-AR(b), Plan Task 1 Step 4, Plan Task 3
Step 4).

---

## MAJOR M-5 — the ceiling's primary justification sentence is false on 64-bit **and** on 32-bit

**The claim under attack.** Spec §3.2, the load-bearing sentence: *"**A cap above that value cannot be honoured
anywhere the library builds: no `[]byte` of that length can exist.**"* Repeated as ADR D-AN(a): *"**A cap above
that cannot be honoured anywhere the library builds.**"* And the framing that follows: the ceiling *"is the answer
to **'what is the largest value this knob could ever be obeyed at?'**"*

**The evidence.** The sentence is false in both directions.

**False on 64-bit.** `int` is 64-bit on `amd64`, `arm64` and every other 64-bit `GOARCH` this module builds for. A
3 GiB `[]byte` is entirely representable there, and `WithMaxBodyBytes(3<<30)` is honoured **exactly** today —
`io.ReadAll` will return a 3 GiB slice on a machine with the memory for it. "Cannot be honoured anywhere the
library builds" is contradicted by the platform the project develops and tests on.

**False on 32-bit, in the other direction.** On `linux/386` a process has ~3 GiB of user address space and
`io.ReadAll`'s doubling growth peaks near 2× the final size. A cap **at** `math.MaxInt32` (~2 GiB) is therefore
not obeyable either — the allocation fails long before the length limit binds. So `math.MaxInt32` is not "the
largest value this knob could ever be obeyed at" on the narrowest platform any more than it is on the widest.

**The one unconditionally true statement is the one the bundle demotes.** Spec §3.2 lists it fourth, under
*"Three further properties, each an independent benefit (**none is the primary argument**)"*:

> *"1. **Width safety.** `n <= math.MaxInt32` guarantees `int(n)` is lossless on **every** `GOARCH`."*

That is exact, platform-independent, and mentions nothing about the caller's payload — which is the property that
clears CLAUDE.md's Sensible-defaults gate. It is the argument, not a footnote to it.

**Why it matters beyond wording.** The bundle stakes its gate-clearance on this sentence and says so:
*"The argument licenses exactly one ceiling, not a family of them."* If the licensing sentence is false, the
gate-clearance is unproven, and the next reader who checks it on `amd64` will conclude the ceiling is a policy
number after all — reopening exactly the deferral the increment exists to close.

**Required fix.** Delete *"cannot be honoured anywhere the library builds"* and *"the largest value this knob
could ever be obeyed at"*. Restate the justification as **width-safety / portability**: `math.MaxInt32` is the
largest value for which the cap is **exactly representable as an `int` on every `GOARCH` this module builds for**,
so one configuration is valid on all of them. Add an explicit sentence that on 64-bit a higher cap **is**
honourable and that the ceiling is a deliberate portability choice, so no reader is later surprised.

---

## MAJOR M-6 — `(n int)` was never considered; it is asserted as a premise and absent from both rejected-alternatives tables

**The claim under attack.** Spec §3.6 item 1, stated as a fact about the increment rather than as a decision:
*"**No signature change.** All three keep `(n int64)`."* Everything downstream rests on it — D-AP's
`checkRangeInt64`, D-AR(b)'s unkillable mutant, the `apidiff` 0/0 consequence, and Global constraint 7's
exception.

**The evidence.** Neither rejected-alternatives table contains it.

- Spec §5's nine rows: keep deferring; a policy ceiling; per-knob ceilings; ceiling plus off-state; lower the
  defaults; make `checkRange` generic; `int(n)` into `checkRange`; restructure `exchange.go:130-131`; export the
  ceiling. **No row for `(n int)`.**
- ADR D-AP's three rows: generic `checkRange[T]`; widen `checkRange` to `int64`; convert at the call site. **No
  row for narrowing the option instead.**

The omission is conspicuous because the bundle repeatedly argues *around* it. D-AP exists only because the option
is `int64` and the helper is `int`; D-AR(b)'s accepted mutation gap exists only because an `int64`→`int`
conversion is expressible; and D-AO's fourth benefit is that `int(n)` would be lossless — which is an argument
that the value should have been an `int` all along. The alternative that dissolves all three is never named.

The repo also contains a **precedent in the same class**: `endpoint.WithMaxPayloadBytes(n int)`
(`endpoint/flowcontrol.go:144`) is a byte cap that takes `int`.

Pre-v1 with no tags and no consumers ([CLAUDE.md](../../CLAUDE.md) "Project status"), a signature change is free
in the compatibility sense — so "no signature change" is a **choice**, and an unargued one.

**This finding does not assert that `(n int)` is correct** — it asserts that a premise carrying four downstream
decisions was never tested. See the coordinator note below this record for what the tested answer turned out to
be.

**Required fix.** Add `(n int)` to both rejected-alternatives tables with a derived verdict, and demote Spec §3.6
item 1 from a stated fact to a decision with a reason. If it is rejected, the reason must be evidence, not the
absence of the question.

---

## MAJOR M-7 — `endpoint.WithMaxPayloadBytes` is a shipped byte cap **with** an off-state; neither artifact mentions it

**The claim under attack.** ADR D-AN(b) and Spec §3.4, which argue "no off-state" from a project-wide stance:
*"A named exported constant … is new exported surface on a pre-v1 API being kept deliberately small, whose
**only** purpose is to re-enable the hazard the class exists to close."* Spec §3.4's table presents four options
and concludes the question is *"closed rather than deferred again."*

**The evidence.** The strongest counter-example is a shipped, exported byte cap in this repository that **has** an
off-state, and it is justified by **precisely the CLAUDE.md sentence the bundle cites in its own support**:

```
$ sed -n '138,145p' endpoint/flowcontrol.go
// n <= 0 disables the cap (the default): a library cannot guess a caller's
// legitimate maximum, so the cap is opt-in. Wire adapters consuming UNTRUSTED
// sources SHOULD set it to bound decode-time memory. The live-value (memory) path
// never carries []byte and is unaffected. Payload structural complexity (deep
// nesting) is bounded by the codec, not here — encoding/json returns an error on
// pathologically nested input rather than overflowing the stack.
func WithMaxPayloadBytes[T any](n int) ConsumerOption[T] {
	return func(o *consumerConfig[T]) { o.maxPayloadBytes = n }
```

It needs **zero new exported surface** to express "unbounded" — `n <= 0` carries it — and it is off by default.

```
$ grep -c 'WithMaxPayloadBytes' docs/specs/018-byte-cap-ceilings.md docs/adrs/0034-byte-cap-ceilings.md docs/plans/032-byte-cap-ceilings.md
docs/specs/018-byte-cap-ceilings.md:0
docs/adrs/0034-byte-cap-ceilings.md:0
docs/plans/032-byte-cap-ceilings.md:0
```

**Zero mentions across the bundle.** D-AN(b) therefore argues from a uniform project stance on byte-cap
off-states **that does not exist**. The class gate itself files `endpoint.WithMaxPayloadBytes` in the `safe` arm
(`sizing_option_class_gate_test.go:669`) with `math.MaxInt`, so the bundle's own governing gate knows about it.

**There is a real distinction available** — msghttp's caps are protective *by default* (1 MiB, on), while
`WithMaxPayloadBytes` is opt-in and off, so `n <= 0` is free there and already taken in msghttp by the existing
rejection. That distinction is exactly what the ADR should have argued and did not.

**Required fix.** Name `endpoint.WithMaxPayloadBytes` head-on, quote its godoc, and explain why msghttp differs.
Then either accept the divergence explicitly or open it as a recorded follow-up. Do not leave the strongest
counter-example to a decision the spec itself flags as *"the decision most likely to be contested"* unaddressed.

---

## MAJOR M-8 — Task 3 edit 2 prescribes the wrong string, and drops the `IsPermanent` assertion the arm carries

**The claim under attack.** Plan Task 3, edit 2: *"`require.NoError` → `require.ErrorIs(t, err,
msghttp.ErrInvalidMax*Bytes)` **plus** `assert.EqualError` on the §3.1 render (**the same string Task 1's AC-2
case asserts**)."*

**The evidence, part 1 — the strings differ.** Task 1's AC-2 case renders the **ceiling+1** value:

```
msghttp: max body bytes out of range: msghttp.WithMaxBodyBytes: 2147483648 not in [1, 2147483647]
```

The gate rows pass `1 << 62`, which renders:

```
msghttp: max body bytes out of range: msghttp.WithMaxBodyBytes: 4611686018427387904 not in [1, 2147483647]
```

An implementer following the instruction literally copies the AC-2 string into a row that produces a different
one, and the row fails. The two are the same *shape*, not the same string.

**The evidence, part 2 — a required assertion is silently dropped.** Every existing row in the `fixed` arm asserts
the `Permanent` classification:

```
$ grep -n 'IsPermanent' sizing_option_class_gate_test.go
417:				assert.False(t, msgin.IsPermanent(err), "R1 constructor error stays bare (ADR 0029 D-M)")
429:				assert.False(t, msgin.IsPermanent(err), "R1 constructor error stays bare (ADR 0029 D-M)")
440:				assert.False(t, msgin.IsPermanent(err), "R1 constructor error stays bare (ADR 0029 D-M)")
455:				assert.True(t, msgin.IsPermanent(err), "R2: latched and reported wrapped in Permanent (ADR 0031 D-V)")
466:				assert.False(t, msgin.IsPermanent(err), "R1 constructor error stays bare (ADR 0029 D-M)")
477:				assert.False(t, msgin.IsPermanent(err), "R1 constructor error stays bare (ADR 0029 D-M)")
488:				assert.False(t, msgin.IsPermanent(err), "R1 constructor error stays bare (ADR 0029 D-M)")
504:				assert.False(t, msgin.IsPermanent(err), "R1 constructor error stays bare (ADR 0029 D-M)")
515:				assert.False(t, msgin.IsPermanent(err), "R1 constructor error stays bare (ADR 0029 D-M)")
```

Nine rows, nine assertions — eight `False` (R1 constructor returns) and one `True` (the single R2 latch). Edit 2
prescribes `ErrorIs` + `EqualError` and stops. A moved row would be the only `fixed`-arm member not asserting its
classification.

**Aggravating.** ADR D-AQ states explicitly *"**Not `Permanent`-wrapped** (ADR 0029 **D-M**)"*, and Spec §3.1
repeats it — and **no acceptance criterion anywhere in the bundle tests it.** Spec §6 has AC-1 (boundary), AC-2
(render), AC-3 (32-bit), AC-4 (gate arms), AC-5 (parents), AC-6 (mutants), AC-7 (links). None asserts
`IsPermanent`. A stated behavioral claim with no covering test is exactly what CLAUDE.md's hot-path branch rule
forbids: *"every typed-error branch … must be exercised by at least one test."*

**Required fix.** Replace "the same string" with the value each row actually passes, add
`assert.False(t, msgin.IsPermanent(err), …)` to the three moved rows to match the arm's shape, and add a matching
acceptance criterion so D-AQ's non-`Permanent` claim is tested rather than asserted.

---

## MAJOR M-9 — the Plan 031 collision is not a mechanical rebase; three normative claims go false if 031 lands first

**The claim under attack.** Spec §"Sibling, not predecessor": *"They **share exactly one file**,
`sizing_option_class_gate_test.go` (§6 AC-4). **Whichever lands second rebases.**"* ADR Consequences: *"One more
file shared with the sibling increment … Whichever lands second rebases."* Plan header: *"The class gate is
shared … Whichever lands second rebases."*

**The evidence.** Plan 031 does not merely edit the same file — it changes the same three quantities Plan 032
states as **normative and unchanged**.

```
$ grep -n 'sizingConformanceKeys\|require.Len(t, tests\|byArm\|19\b' docs/plans/031-group-member-bounds.md | head
```

Plan 031 (ADR 0033 **D-AL**) takes `sizingConformanceKeys` from **17 to 19** and the arm partition to
**11 fixed + 1 rejects + 3 deferred + 6 safe = 21 rows**.

Plan 032 states, in three places, as things to *assert are unchanged*:

| Plan 032 claim | Where | After Plan 031 |
|---|---|---|
| *"`require.Len(t, tests, 19)` is unchanged — 17 AST rows + 2 manual rows"* | Spec §6 AC-4, ADR D-AS, Plan Task 3 | **false** — 21 rows, 19 AST keys |
| *"**`sizingConformanceKeys` is unchanged**"* | Spec §6 AC-4, ADR D-AS, Plan Task 3 | **false** — 19 keys |
| `{"fixed": 12, "rejects": 1, "safe": 6}` | Spec §6 AC-4 edit 4, ADR D-AS, Plan Task 3 edit 4 | **false** — `{"fixed": 14, "rejects": 1, "safe": 6}` |

"Whichever lands second rebases" is adequate for a textual conflict. It is not adequate when the second-lander's
**acceptance criteria** are literal counts that the first lander invalidates: an implementer rebasing Plan 032
onto Plan 031 reads AC-4, finds `require.Len(t, tests, 19)` in a file that says 21, and has no written guidance on
whether that is the rebase or a regression. Plan 032 Task 3 Step 4's *"confirm exactly three failures and no
others"* has the same problem.

**Required fix.** Express the gate counts as **deltas**, not literals: *"remove the `deferred` key; `fixed`
increases by 3 from whatever it then is; `require.Len` increases by 0."* Add an explicit ordering statement naming
both landing orders and their consequences, and say which increment updates Spec 016 §2.1's arm table so the two
do not both claim it or both skip it.

---

## MAJOR M-10 — Spec §6 AC-1 requires an accessor assertion that Global constraint 2 forbids

**The claim under attack.** Spec §6 AC-1: *"`NewConfig(WithX(byteCapCeiling))` → nil error, non-nil `*Config`,
**and the accessor returns the value set** (Spec 016 §6's *"its product is usable"* definition for a
`NewConfig`-only key)."*

**The evidence.** The accessors are unexported, and for two of the three knobs there is **no accessor at all**:

```
$ grep -n 'func (c \*Config) maxBody\|func (c \*Config) replaySize' adapter/http/options.go
272:func (c *Config) maxBody() int64 {
441:func (c *Config) replaySize() int { return c.replayBuffer }
$ grep -c 'func (c \*Config) maxResponse\|func (c \*Config) maxEvent' adapter/http/options.go
0
```

`maxResponseBytes` and `maxEventBytes` are read as struct fields directly (`exchange.go:130`,
`sseclient.go:401`), so there is nothing an accessor assertion could call even in a whitebox test.

Plan Global constraint 2 forbids the whitebox escape: *"**Blackbox tests only** — `package msghttp_test` …
exercising the exported API. No whitebox fallback."* And the plan **correctly contradicts the spec** in Task 1:
*"**Do not assert an accessor** — the accessors are unexported."*

So AC-1 as written is unsatisfiable, and the plan already knows it. The two artifacts disagree about a hard
requirement, which is a bundle that fails its own consistency check before an implementer opens an editor.

**Required fix.** Delete AC-1's accessor clause and adopt the plan's observable-effect definition in its place —
`DecodeRequest` on a small body, an `httptest` round-trip, `NewSSEParser` + `Next` on a small event.

---

## MINOR m-11 — `WithReplayBuffer` cites Spec 016 §1.5, not §1.3; and both sibling cites use godoc lines

**The claim under attack.** Spec §2.1: *"By contrast the *bounded* siblings did get theirs:
`WithMaxConnections` (`options.go:901`) and `WithReplayBuffer` (`:976`) **both cite Spec 016 §1.3** in their
godoc."* Repeated in ADR Context (*"`WithMaxConnections` `options.go:901`, `WithReplayBuffer` `:976`"*) and Plan
Task 2's preamble.

**The evidence.**

```
$ grep -n 'Spec 016 §1.3\|Spec 016 §1.5' adapter/http/options.go
901:// an unbounded admission gate (Spec 016 §1.3 — below the ceiling this was
977:// of the server, even with no client connected (Spec 016 §1.5). The cost is
$ grep -n 'func WithMaxConnections\|func WithReplayBuffer' adapter/http/options.go
908:func WithMaxConnections(n int) Option {
986:func WithReplayBuffer(n int) Option {
```

`WithReplayBuffer` cites **§1.5**, at `:977` not `:976`. Two errors in one clause.

**Secondary, and worth a convention.** Both cites point at **godoc lines** (`:901`, `:977`), while every other
citation in the bundle points at the **`func` line** (`options.go:463`, `:767`, `:856`; `helpers.go:64`). A reader
checking `:976` finds a godoc line one off from the citation and a `func` ten lines below it. The bundle should
state its convention once rather than mixing two.

**Required fix.** Correct §1.5 and `:977`, and state the func-line-versus-godoc-line convention once, in the spec.

---

## MINOR m-12 — "splitting re-opens all 19 gate rows" is inflated

**The claim under attack.** Spec §1.1 and ADR D-AM, used to close the class-split question: splitting *"would
mean either (a) amending D-AB's criterion to add a failure-mode clause, **which re-opens all 19 gate rows**, or
(b) maintaining two criteria."* Repeated as *"re-deriving all 19 class-gate rows"* in D-AM's REVERSIBILITY line
and Spec §8 item 3.

**The evidence.** Mechanically, a second class would move **3** rows into a new arm and add **one** key to the
`byArm` map. The other 16 rows' `key`, `arm` and `assert` fields are untouched: they are already classified
against a criterion the split does not amend for them.

```
$ grep -c 'key:' sizing_option_class_gate_test.go
19
```

The *conceptual* cost is real — a criterion amendment means re-checking each row against the new criterion, and
the census took five audit rounds to stabilise. But "re-opens all 19 rows" reads as an edit count, and as an edit
count it is 3.

**The conclusion is right on the merits** and does not depend on the inflation: two overlapping criteria
partitioning the same knobs is the hand-maintained census D-AB exists to kill, and a taxonomy change that alters
no outcome is not worth it. Overstating a cost weakens an argument that stands without it.

**Required fix.** State the cost accurately — 3 row moves plus a criterion amendment requiring all 19 rows to be
re-checked — and keep the conclusion.

---

## MINOR m-13 — Global constraint 6 versus branch B1-4's fixture

**The claim under attack.** Plan Global constraint 6: *"**No test reads more than 1 MiB.** Ceiling values are
exercised by **`NewConfig` only** (Spec 018 §6 AC-1)."*

**The evidence.** Task 1's own branch table, three sections later:

> | B1-4 | body gate, `!set` → default | `NewConfig_default_body_cap_is_1MiB` (**unset ⇒ a 2 MiB body is rejected by `DecodeRequest`**) |

A 2 MiB body is a 2 MiB read. The constraint and the required case contradict each other verbatim.

The intent is clear and correct — the constraint is about not allocating at the **ceiling** — but as worded it
forbids the only case that proves the default arm, and an implementer following it literally deletes B1-4.

**Required fix.** Reword constraint 6 to bound the **cap under test**, not the fixture: no test may configure a
cap above ~1 MiB; a fixture may exceed a small cap in order to prove the cap binds.

---

## MINOR m-14 — six godoc sentences go false between Task 1's commit and Task 2's

**The claim under attack.** Spec §4 item 3: *"**No existing godoc sentence becomes false.** Checked this revision:
the three options' `n MUST be > 0` paragraphs are *narrowed*, not contradicted, **and are rewritten in the same
edit**."* But Task 1 and Task 2 are separate commits, and Task 1 changes the behavior while Task 2 changes the
prose.

**The evidence.** After Task 1 commits and before Task 2 commits, six sentences describe a constructor that no
longer behaves that way:

```
$ grep -n 'n MUST be > 0' adapter/http/options.go
458:// n MUST be > 0: NewConfig returns ErrInvalidMaxBodyBytes for an explicit
764:// n MUST be > 0: NewConfig returns ErrInvalidMaxResponseBytes for an explicit
851:// n MUST be > 0: NewSSEParser (via NewConfig) returns ErrInvalidMaxEventBytes
$ sed -n '14,19p;72,77p;132,138p' adapter/http/errors.go
	// ErrInvalidMaxBodyBytes is returned by NewConfig when an explicit
	// WithMaxBodyBytes is <= 0. …
	// ErrInvalidMaxResponseBytes is returned by NewConfig when an explicit
	// WithMaxResponseBytes is <= 0. …
	// ErrInvalidMaxEventBytes is returned by NewConfig (and so by
	// NewSSEParser) when an explicit WithMaxEventBytes is <= 0. …
```

Three option godocs plus three sentinel godocs, each false for the duration of one commit. Plan Task 2 Step 5
names the sentinel godocs as its own work and says *"becomes false the moment Task 1 lands"* — acknowledging the
window rather than closing it. Spec §4 item 3's "in the same edit" is true of Task 2's edit and false of the
commit boundary.

This is the same class the project's stored lesson names — *docs can contradict the code they describe*, and all
three Plan 028 fix rounds were godoc rather than logic. A deliberately-created window is a poor place to start.

**Required fix.** Land the production change and its godoc in one commit (which B-1 requires for a different
reason), or state the window as accepted with a rationale.

---

## Checked and found CLEAN — 20 rows of verified derivation

Round 2 should not re-derive these. Each was checked first-hand against the tree at `1212c63` and reproduces
exactly as the bundle states it.

| # | Claim | Where | Verification |
|---|---|---|---|
| 1 | All three options take `int64` | Spec §2 table, §1 table | `options.go:463`, `:767`, `:856` — `func WithMaxBodyBytes(n int64) Option` and twins ✅ |
| 2 | All three defaults are `1 << 20` (1 MiB) | Spec §2 table | `const defaultMaxBodyBytes int64 = 1 << 20` `options.go:23`; `defaultMaxResponseBytes` `:30`; `defaultMaxEventBytes` ✅ |
| 3 | All three `NewConfig` gates reject an explicit `n <= 0` with their own sentinel, in R1-a shape | Spec §2, §3.1; ADR fact 1 | `options.go:1189-1193`, `:1201-1205`, `:1211-1215` — `if !set { default } else if n <= 0 { return nil, Err… }` ✅ |
| 4 | The three sentinels exist where cited | Spec §2, ADR D-AQ | `errors.go:19`, `:77`, `:138` ✅ |
| 5 | No test asserts any of the three sentinel **message strings** | Spec §3.1, ADR D-AQ | `grep -rn 'max body bytes must be\|…' --include='*.go' .` → exactly the three declarations in `errors.go` ✅ (this is true; B-2 is about a *different* dependency the grep cannot see) |
| 6 | Every test that rejects one of the three uses `ErrorIs`, never a rendered-text assertion | implied by §3.1 | `encode_test.go:50-64`, `outbound_test.go:57-76`, `sse_test.go:1031-1047`, `exchange_test.go:141`, `sseclient_test.go:214`, `stdlib/inbound_test.go:99,159` — all `assert.ErrorIs` ✅ |
| 7 | The shipped `checkRange` takes `int` and lives where cited | Spec §3.5, ADR fact 4 | `helpers.go:64` — `func checkRange(sentinel error, site string, n, lo, hi int) error` ✅ |
| 8 | Its render is `"%w: %s: %d not in [%d, %d]"`, so `%d` carries both widths | Spec §3.5, Plan constraint 4 | `helpers.go:68` ✅ |
| 9 | `checkRange` is one of four independent per-package copies under ADR 0031 D-R | Spec §3.5, ADR D-AP | `helpers.go:55-63` godoc names `endpoint.checkRange`, `routing.checkRange`, `memory.checkRange` ✅ |
| 10 | `drainBounded` is 5 of `maxResponseBytes`' 6 I/O-consuming reads, and the 6th is not an omission | Spec §1.2, ADR fact 3, D-AR(a) | `sseclient.go:335,338,341`, `outbound.go:370`, `exchange.go:126`; sixth at `exchange.go:130-131`. `func drainBounded(body io.Reader, max int64)` `outbound.go:410` discards via `io.CopyN(io.Discard, …)` ✅ |
| 11 | Both SSE caps check **after** the append | Spec §1.3 item 3 | `sse.go:385-387` (`WriteString`/`WriteByte` then `if int64(p.dataBuf.Len()) > …`); `sse.go:471-472` (`buf = append(buf, b)` then `if int64(len(buf)) > …`) ✅ |
| 12 | Spec 016 §3.8 item 2's hazard disclosure was promised and never delivered | Spec §2.1, ADR Context | `grep -c 'hazard disclosure' docs/plans/029-sizing-option-bounds.md` → `0`; none of `options.go:446-462`, `:749-766`, `:834-855` carries it ✅ |
| 13 | The gate's half-1 AST walk accepts `int` **or** `int64`, so all three keys stay discovered under any retype | Spec §6 AC-4, and relevant to M-6 | `sizing_option_class_gate_test.go:234` — `return t.Name == "int" \|\| t.Name == "int64"` ✅ |
| 14 | `require.Len(t, tests, 19)` = 17 AST keys + 2 manual rows | Spec §6 AC-4, ADR D-AS | `:753-754`; `grep -c 'key:'` → `19` ✅ (but see M-9 — Plan 031 changes it) |
| 15 | `byArm` is built by **counting**, so an emptied arm has no key and `"deferred": 0` fails — the trap is real | Spec §6 AC-4 item 4, ADR D-AS trap 1, Plan Task 3 edit 4 | `:793` `byArm := map[string]int{}`, `:797` `byArm[tc.arm]++`; `:803` `require.Equal(map[string]int{…}, byArm, …)` ✅ |
| 16 | `wantArms` is a key→arm **mapping**, so a pairwise swap is caught | Plan Task 3 mutant M3-2 | `:770-784` plus the `:765-769` comment recording exactly that reasoning ✅ |
| 17 | The 🔴 block above the rows really is *instructions for a future contributor*, and this increment is that event | Spec §6 AC-4 item 7, ADR D-AS trap 2 | `:547-556` — *"WHEN §3.8's CEILING LANDS, THIS GATE WILL GO RED, AND THAT IS CORRECT … the repair is to MOVE the row into the `fixed` arm"* ✅ |
| 18 | `math.MaxInt32` is exactly the largest `len([]byte)` on a 32-bit `GOARCH` | Spec §3.2, ADR D-AO | Slice `len`/`cap` are `int`; `int` is 32-bit on `386`/`arm`/`mips`/`mipsle` ✅ |
| 19 | `GOARCH=386 GOOS=linux go build ./...` is clean — no non-test code is 32-bit-affected | Spec §2 table row 6 | exit `0` ✅ (the *test* form is not — see M-4) |
| 20 | The multi-instance / topology statement is correct | Spec §7 | `byteCapCeiling` is a compile-time constant guarding a per-process, per-request allocation; no cross-instance state, no SPI seam affected ✅ |

**Also clean, and worth preserving as reasoning rather than derivation:** D-AM's "same class, different remedy"
call; D-AN(a)'s framing of the objection at full strength before answering it; the `1<<30`-is-an-int32-value
argument that keeps the `safe` arm at `math.MaxInt`; and D-AS's decision to keep `"deferred"` as a tombstone in
the `arm` vocabulary rather than deleting the concept.

---

## Auditor's method note

Every command in this record was run on the tree at `1212c63` with `GOTOOLCHAIN=go1.25.13` on darwin/arm64. The
gate's `deferred` inventory, the offset drift against Plan 030's landed conversion, the 386 exit codes for all
three command forms, the `exchange_test.go` `MaxInt64` dependency, the `IsPermanent` row census, the
`WithMaxPayloadBytes` zero-hit grep and the §1.5 citation are all first-hand output, not transcription. No file in
the repository was modified.

---

## Coordinator's re-derivation note (written with this record, not an edit to it)

Recorded here because it is evidence round 2 must not have to rediscover, and because it bears directly on M-6.

**`(n int)` was tested empirically and does not work, for a reason the audit could not have reached from the
bundle alone.** With `byteCapCeiling = math.MaxInt32` and an `int` parameter, on `GOARCH=386` the ceiling **is**
`math.MaxInt`, so **no `int` literal can exceed it** — the upper arm becomes inexpressible rather than merely
untested:

```
$ GOARCH=386 GOOS=linux go build ./...      # probe module, func WithMaxBodyBytes(n int)
./p.go:12:23: cannot use 2147483648 (untyped int constant) as int value in argument to WithMaxBodyBytes (overflows)
./p.go:13:23: cannot use 1 << 62 (untyped int constant 4611686018427387904) as int value in argument to WithMaxBodyBytes (overflows)
$ GOARCH=386 GOOS=linux go build ./...      # with `over := int(int64(math.MaxInt32) + 1)`
./p.go:12:14: constant 2147483648 overflows int
```

The constant-conversion workaround fails too, because `int64(math.MaxInt32) + 1` is still a constant expression.
Consequences, all verified: Spec §6 AC-2's `2147483648` literal would not compile on 386; the M-4 replacement gate
(`GOARCH=386 go vet ./...`) would go red; and no `fixed`-arm literal exists that is both compilable on 386 and
above the ceiling. Separately, `1 << 30` cannot serve the three moved rows under **either** signature, because
`1,073,741,824 < 2,147,483,647` — it would be **accepted**.

`(n int64)` is therefore retained, and M-6 is discharged by *argument* rather than by adoption: the alternative is
now named, tested, and rejected with this evidence in Spec §5 and ADR D-AP.

---

**VERDICT: NOT SAFE TO IMPLEMENT.**

*The bundle's reasoning is strong. What fails is derivation against the tree — and the one place the reasoning
does fail (M-5) is the sentence the gate-clearance rests on.*
