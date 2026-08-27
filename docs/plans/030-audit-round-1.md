# Plan 030 — adversarial design audit, round 1 (2026-08-22)

Independent Opus subagent, handed **Plan 030 revision 1**
([`030-post-029-maintenance.md`](030-post-029-maintenance.md)) **before any implementation code existed**, per
[`CLAUDE.md`](../../CLAUDE.md)'s design-time gate. The bundle is a plan alone: revision 1 declares "**no spec, no
RFC and no ADR**, deliberately", and that declaration is itself one of the claims under attack.

**Traceability.** Audits: [Plan 030](030-post-029-maintenance.md). Parent backlog:
[`docs/HANDOVER.md`](../HANDOVER.md) §6. Artifacts whose contracts are implicated:
[Spec 015](../specs/015-nil-option-elements.md) AC-7, [ADR 0031](../adrs/0031-nil-option-elements.md) D-R,
[Spec 016](../specs/016-sizing-option-bounds.md), [ADR 0032](../adrs/0032-sizing-option-bounds.md),
[Plan 029](029-sizing-option-bounds.md).

This record is **evidence-primary** — it is the audit artifact, not a user-facing summary. Every structural claim
in the plan was re-derived on this tree (`main` at `2b2dec1`, `GOTOOLCHAIN=go1.25.13`, darwin/arm64); the commands
and their output are pasted below.

> **⚠️ IMMUTABLE EXECUTION RECORD.** This file records what round 1 found, at the time it found it. Do not edit it
> to reflect later decisions, later measurements, or the revised plan. Corrections belong in a later round's
> record or in the plan itself. The coordinator's dispositions for these findings live in **Plan 030 revision 2**,
> which cites this file.

**Verdict: NOT SAFE TO IMPLEMENT.** 3 BLOCKERs, 4 MAJORs, 5 MINORs.

The plan's *arithmetic* is unusually good — the 24 compile errors, the 16 gate-prose lines, the 16 int-typed gate
code lines, the seven byte-identical delegator sites, the three `int64` exclusions and the eighth-delegator claim
all reproduce exactly. What fails is not measurement but **scope**: two of the four tasks are aimed at a target
that a shipped gate, a shipped spec and a shipped ADR are actively holding in place, and the third task's own
`grep` cannot see a third of its own class.

---

## Finding index

| # | Rank | One line |
|---|---|---|
| **1** | BLOCKER | Task 1 turns the Spec 015 AC-7 guard gate RED at all seven sites; the "no spec, no ADR" premise fails |
| **2** | BLOCKER | Task 3's uniform `1<<30` destroys the class gate's SIX safe-arm rows |
| **3** | BLOCKER | Task 2 fixes the instance, not the class; its own grep is blind to line-wrapped occurrences |
| **4** | MAJOR | Task 4's citation sweep is incomplete and internally inconsistent |
| **5** | MAJOR | The exported-surface gate is vacuous for the only Go-changing task, and cites a superseded baseline |
| **6** | MAJOR | Task 2's sequencing rationale names five files Task 2 does not edit |
| **7** | MAJOR | Task 3 omits a required companion edit; the decimals are live assertions, not prose |
| **8** | MINOR | Unnamed fourth hazard in Task 3 — the `makechan` mutant becomes an OOM, not a panic |
| **9** | MINOR | Task 1's first step is unorderable |
| **10** | MINOR | Line-number convention inconsistency |
| **11** | MINOR | The docs-link baseline excluded the plan itself |
| **12** | MINOR | Backlog items 3, 6, 7 dropped without a written carry-forward |

---

## BLOCKER 1 — Task 1 turns the Spec 015 AC-7 guard gate RED at all seven sites; the "no spec, no ADR" premise fails

**The claim under attack.** Plan 030 §"Governing artifacts": *"This plan has **no spec, no RFC and no ADR**,
deliberately … there is **no new contract** … **no contested "how"** … and **no architectural decision**."* Task 1
then collapses the seven delegator nil-option pre-check loops into a per-package `checkNilOptions` helper.

**The evidence.** `option_guard_gate_test.go` flags every variadic `...XxxOption` parameter unconditionally; the
ONLY thing clearing the flag is an `*ast.RangeStmt` over the parameter itself inside the constructor's own body
(`option_guard_gate_test.go:160-175`, `hasNilElementGuard` at `:219-238`). After Task 1, `NewExchange`'s body has
`if err := checkNilOptions(...)` and no RangeStmt over `opts` → `guarded == false` → `assert.Emptyf(t, unguarded,
...)` at `:464` fails for all seven.

`checkNilOptions(ctor string, opts []Option)` is **not variadic**, so it is never scanned — the guard becomes
invisible to the gate rather than merely unrecognised.

Total variadic option params scanned by the gate today: **32**.

The gate is green today **and ships a committed probe asserting that exactly the post-Task-1 shape is unguarded**:

- `TestOptionGuardRecognizer/R1_pre-check_—_standalone_loop,_no_opt(&cfg)_call` is the shape Task 1 **deletes**.
- `PROBE_qualified_type_—_msghttp.Option,_unguarded_delegator` is the shape `stdlib.NewInbound` **becomes**.

**Why it matters.** Fixing this means teaching the recognizer that a helper call counts as a **dominance proof** —
which is amending **Spec 015 AC-7** and **ADR 0031 D-R**. That is a new contract and an architectural decision, so
the plan's "no spec, no ADR" premise is false as written. Task 1 cannot be executed as a pure refactor.

**Required fix.** Either (a) drop Task 1, or (b) promote it into a spec+ADR increment that amends AC-7's
recogniser and re-audits the gate. It is not a docs-and-refactor task.

---

## BLOCKER 2 — Task 3's uniform `1<<30` destroys the class gate's SIX safe-arm rows

**The claim under attack.** Plan 030 Task 3: *"Convert the 23 ceiling-class sites to `1 << 30`."* The plan's own
hazard 4 half-sees the problem ("the `safe` arm needs the OPPOSITE treatment") but names only three safe-arm sites
(`:634`, `:647`, `:666`) and the Sites list still enumerates all 16 int-typed gate lines under one instruction.

**The evidence.** Of the 16 int-typed gate lines, **9 are `fixed`**, **1 is `rejects`**, and **6 are the `safe`
arm** — `:568, :587, :612, :634, :647, :666` — whose asserted branch is the OPPOSITE (accept).

```
$ grep -n "1 << 62\|1<<62" sizing_option_class_gate_test.go
375:  endpoint.WithMaxInFlight[any](1<<62)          fixed
387:  endpoint.WithConcurrency[any](1<<62)          fixed
398:  msghttp.WithConnectionBuffer(1 << 62)         fixed
411:  memory.New(memory.WithBuffer(1 << 62))        fixed
424:  memory.WithCapacity(1 << 62)                  fixed
435:  memory.WithMaxGroups(1 << 62)                 fixed
446:  msghttp.WithMaxConnections(1 << 62)           fixed
462:  routing.WithCompletionSize(1<<62)             fixed
473:  msghttp.WithReplayBuffer(1 << 62)             fixed
493:  msghttp.WithSuccessStatus(1 << 62)            rejects
568:  endpoint.WithPollMaxBatch[any](1<<62)         SAFE
587:  resilience.WithBreakerThreshold(1 << 62)      SAFE
612:  endpoint.WithMaxPayloadBytes[[]byte](1<<62)   SAFE
634:  resilience.NewTokenBucket(1, 1<<62)           SAFE
647:  store.Claim(t.Context(), 1<<62)               SAFE
666:  qc.Poll(t.Context(), 1<<62)                   SAFE
```

`sizing_option_class_gate_test.go:544-547` states the safe arm's purpose verbatim: *"Each accepts 1<<62 AND its
product is proven usable … a comparison-only knob is exercised past the point where a buggy comparison (e.g. an
int32 truncation) would misbehave."*

**`1<<30` IS an int32 value.** Converting a safe-arm row to `1<<30` leaves the assertion passing while probing
nothing.

Representative row `:586-594` — `resilience.WithBreakerThreshold`:

```
587:  b, err := resilience.NewCircuitBreaker(resilience.WithBreakerThreshold(1 << 62))
588:  require.NoError(t, err, "accepts 1<<62 — the option silently ignores n < 1 and never rejects "...
594:      "product usable: 1,000 consecutive failures must not trip a breaker whose threshold is 1<<62")
```

**The plan's own mutation gate is inapplicable there.** Task 3's "Hot-path branches introduced" section makes the
task a mutation check: *"for each converted site, confirm the assertion still fails when the ceiling check is
removed."* The safe-arm sites **have no ceiling check to remove**. The gate silently covers 17 of 23 sites.

Worse: on `GOARCH=386` no `int` value exceeds int32 at all, so the truncation probe those six rows exist to run is
**unachievable by magnitude reduction**. There is no 32-bit-legal `int` literal that reproduces it.

**Required fix.** Split the conversion by **arm**, not by file. The reject arm needs a fixed decimal that clears
every ceiling; the safe arm needs a value that stays maximally absurd and must not carry a decimal string at all.
The mutation gate must likewise be arm-specific.

---

## BLOCKER 3 — Task 2 fixes the instance, not the class; its own grep is blind to line-wrapped occurrences

**The claim under attack.** Task 2 §Steps: *"`grep -rn "first statement" --include="*.go" .` and re-classify
**every** remaining hit … **Assert the invariant, not the enumeration** — the recurring failure on this project is
fixing the named instance while the class returns through a site nobody listed."* The plan then enumerates 11
sites (5 production + 6 test).

**The evidence.** The command the plan prescribes cannot see its own class. The phrase **wraps across comment
lines**, and a line-oriented `grep` misses every wrapped occurrence:

```
$ grep -rn "first statement" --include="*.go" . | wc -l
      19

$ for f in $(git ls-files '*.go'); do perl -0777 -ne 'while (/first\s*(?:\n\s*(?:\/\/|\*)\s*)?statement/g)
    { $p = substr($_,0,pos($_)); $n = ($p =~ tr/\n//)+1; print "$ARGV:$n\n"; }' "$f"; done | wc -l
      24
```

Five extra false sites, **all unlisted by the plan**:

| Site | Nature |
|---|---|
| `adapter/http/helpers.go:16-17` | *"five delegators … that each call `NewConfig(opts...)` as their first / statement"* — **INVERTED, and it is PRODUCTION godoc** |
| `adapter/database/sql/outbound.go:56-57` | *"The apply loop is this constructor's first / statement"*; actual first stmt is `outbound.go:61` `cfg := config{logger: discardLogger()}` |
| `adapter/database/sql/source.go:89-90` | same; actual first stmt `source.go:95` |
| `adapter/cron/source_test.go:267-268` | same; actual first stmt `source.go:179` |
| `adapter/http/nil_option_test.go:47-48` | *"`cfg := &Config{}` then the apply loop is its first / statement"* — **self-contradictory** |

`adapter/http/helpers.go:16-17` is nearly verbatim the same sentence as `nil_option_test.go:22`, which the plan
itself calls *"the more serious error"*. **The plan fixes the test copy and leaves the production copy.**

**Aggravating.** The plan's own 2b table cites `outbound.go:61` and `source.go:95` as the reason the TEST comments
are wrong — while never noticing that the **production godoc two lines above each constructor says the same false
thing**. The evidence needed to find the missed sites was already in the plan's own table.

**Corrected totals: 8 false production sites (not 5) and 8 false test sites (not 6) — 16, not 11.**

The **8 accurate sites** are correctly identified by the plan and must not be touched: the 7 delegators
(`sql/queuestore.go:42`, `http/sse.go:221`, `http/exchange.go:72`, `http/sseclient.go:63`,
`http/outbound.go:322`, `http/stdlib/inbound.go:43`, `:109`) plus `adapter/http/nil_option_test.go:305`.

**Required fix.** Make the wrap-tolerant scan the task's *command*, not a follow-up check, and state the invariant
the scan enforces rather than enumerating sites. Add the five missed sites.

---

## MAJOR 4 — Task 4's citation sweep is incomplete and internally inconsistent

**The claim under attack.** Task 4: *"**five files** still assert a number that means something else"*, followed
by a table.

**The evidence.** The plan says "five files" but **the table lists three** (`docs/specs/011-http-adapter.md`,
`docs/adrs/0023-http-channel-adapter.md`, `docs/plans/020-http-adapter-inbound.md`). The six listed lines are
correctly quoted and exist.

Missed, **at least six more**, three of them in `docs/rfcs/README.md` — a directory the plan never scans:

```
docs/rfcs/README.md:116        "Plan 028 (the HTTP SSE gin binding — renumbered from 027 …)"
docs/rfcs/README.md:122        "Then the feature roadmap: Plan 028 (gin), then RFC-0005's five components …"
docs/rfcs/README.md:126        "Plan 028 — next, and"
docs/specs/011-http-adapter.md:94
docs/specs/011-http-adapter.md:95
docs/plans/027-core-package-layout.md:3523
```

`docs/specs/011-http-adapter.md:95` is now **doubly false** — it asserts Plan 028 *"does not exist yet"* when
`docs/plans/028-nil-option-elements.md` has shipped.

**Verified clean / verified complete (preserve these):**
- **`MESSAGING.md` is clean** — `grep -n "Plan 028\|ADR 0024" MESSAGING.md` → no hits.
- **The ADR-0024 reference list is verified complete and accurate** (`docs/specs/011-http-adapter.md:25,92,95,685`
  and `docs/adrs/0023-http-channel-adapter.md:32,33,36,200,203`).

**On traceability — explicitly NOT a finding.** *"Unnumbered until written"* IS consistent with CLAUDE.md. The
traceability rule binds **artifacts**, and an unwritten roadmap entry is not one. The plan's decision to state the
gin increment as unnumbered rather than re-assigning a fresh number is sound and survives attack.

**Required fix.** Derive the citation list mechanically, sweep `docs/rfcs/` and `docs/plans/027-core-package-layout.md`,
add the six missed lines, single out `docs/specs/011-http-adapter.md:95` for its second falsehood, and reconcile
the "five files" claim with the table.

---

## MAJOR 5 — the exported-surface gate is vacuous for the only Go-changing task, and cites a superseded baseline

**The claim under attack.** Global constraint 1: *"Verify with `apidiff` against `027-root-api-baseline.txt` or an
AST decl-count diff against the branch point — **non-vacuously**."*

**The evidence.** Plan 029 records **twice** that `apidiff` is blind outside root:

```
029-sizing-option-bounds.md:72   "…an exported-surface AST diff, *not* with `apidiff`
                                  (Plan 028 proved `apidiff` is blind outside root)."
029-sizing-option-bounds.md:291  "Do **not** use `apidiff` as the primary gate —
                                  Plan 028 proved it captures only the root package."
```

Task 1 — the only task that changes Go non-test code — touches `adapter/http` and `adapter/http/stdlib`.
**`apidiff` against a root baseline cannot see either.** The gate is vacuous for exactly the task it exists to
constrain.

Two baselines exist and **the plan cites the older**:

```
$ ls docs/plans/*api-baseline*
docs/plans/027-root-api-baseline.txt
docs/plans/028-root-api-baseline.txt
```

The offered fallback is an AST decl-**COUNT** diff. A count **passes a rename** — against this project's own
stored lesson, *"reconcile by name, never by count"* (CLAUDE.md Dependency policy, on the 43-vs-43 sentinel
reconciliation).

**Required fix.** Replace with an exported-symbol AST **set diff by name**, `main..HEAD`, across **all** packages,
and plant the vacuity probe outside root — Plan 029 already settled where (`029-sizing-option-bounds.md:615`:
*"AC-6 vacuity probes, planted in `adapter/http`, NOT in root. Plan 028's `apidiff` blindness survived Task 0
because its probe was planted in root — proving the gate *fires* is not proving it *covers*."*). Drop `apidiff`
or demote it to a root-only secondary check against the **newer** baseline.

---

## MAJOR 6 — Task 2's sequencing rationale names five files Task 2 does not edit

**The claim under attack.** Task 2 §Sequencing: *"Task 2 edits comments in `adapter/http/exchange.go`,
`outbound.go`, `sseclient.go`, `sse.go` and `stdlib/inbound.go` — **the same files Task 1 rewrites.** Run Task 2
**after** Task 1 to avoid a conflict, and re-read each site rather than applying a pre-computed patch."*

**The evidence.** Task 2's own §2a forbids touching those files, in bold: *"**The seven *accurate* sites must not
be touched**"* — and names `http/sse.go:221`, `http/exchange.go:72`, `http/sseclient.go:63`,
`http/outbound.go:322`, `http/stdlib/inbound.go:43` and `:109` as exactly the untouchable set. **Neither the 2a
nor the 2b table contains a single line from any of those five files.**

**Why it matters.** The note tells the implementer to *"re-read each site"* in files that contain **no Task 2
sites** — an open invitation to "correct" the seven accurate godocs, which is the one outcome §2a exists to
prevent.

**Task 1 and Task 3 do NOT collide** (disjoint file sets: `adapter/http/*.go` + `stdlib/*.go` non-test vs. test
files in root/`endpoint`/`routing`/`adapter/memory`).

**The genuine collision is Task 1 ↔ Task 2 on `adapter/http/helpers.go`** — Task 1 adds `checkNilOptions` to that
file and Task 2 must fix the inverted godoc at `:16-17`. The plan cannot see it, because it missed that site
(BLOCKER 3).

**Required fix.** Delete the sequencing note as written and replace it with the real collision, or remove it
entirely if Task 1 is dropped.

---

## MAJOR 7 — Task 3 omits a required companion edit; the decimals are live assertions, not prose

**The claim under attack.** Task 3 hazard 2: *"**11 occurrences of the decimal `4611686018427387904` must move in
lockstep.** Ten are inside expected-error string literals (they compile either way but **become *false prose*** if
the value changes)."*

**The evidence.** The count is right and the classification is wrong.

```
$ grep -rno "4611686018427387904" --include="*.go" . | wc -l
      11
```

Nine of the ten in-string occurrences are **`assert.EqualError` arguments**
(`sizing_option_class_gate_test.go:379, 391, 402, 417, 428, 439, 450, 466, 477`) and the tenth is an
**`assert.Contains`**. They **FAIL AT RUNTIME**. They do not "become false prose".

The tenth:

```
adapter/memory/sizing_bounds_test.go:378:
  assert.Contains(t, err.Error(), "memory.WithBuffer: 4611686018427387904 not in [0, 1048576]")
```

It sits inside the helper `assertFirstFaultIsSizing` (`:372-379`) and **appears NOWHERE in Task 3's site list** —
which enumerates `adapter/memory/sizing_bounds_test.go:320,326,332,338` and stops. An implementer working that
list converts the four `1 << 62` literals, leaves `:378` at the old decimal, and
**`TestNew_SizingGuardIsIndependentOfTheLatch/AC-3b` goes red.**

**Verified correct and to be preserved** — the plan's remaining arithmetic all reproduces:
- **24 compile errors** under `GOARCH=386 GOOS=linux ... -gcflags=all=-e` (4 memory + 2 routing + 16 root + 2 endpoint).
- The **16 gate-prose lines** `31,35,37,485,488,510,545,570,580,588,594,614,624,635,648,667`.
- The **16 int-typed gate code lines**.
- **`GOARCH=386 go build ./...` clean** — no non-test code affected.
- **All seven non-root modules 386-vet clean.**

**Required fix.** Add `adapter/memory/sizing_bounds_test.go:378` to the site list, and reclassify the ten in-string
occurrences as runtime assertions rather than prose.

---

## MINOR 8 — unnamed fourth hazard in Task 3

**The claim under attack.** Task 3's hazard list is presented as complete for `adapter/memory`.

**The evidence.** `adapter/memory/sizing_bounds_test.go:292-294` documents that the wrong implementation shape
*"therefore reaches `make(chan msgin.Message[any], 1<<62)` and panics"*, and `:306` records the test as
**mutation-proven** (*"the NotPanics is what makes this one fail against it (mutation-proven — see the task
report)"*).

`runtime.makechan` raises `"size out of range"` only when `elemsize × cap > maxAlloc` (≈ `1<<48` on 64-bit). At
`1<<30` with an element size ≥ 8 bytes the product is ≥ **8 GiB** — comfortably *under* that threshold — so
re-running the mutation attempts a **real allocation** and will likely OOM-kill the test binary instead of
producing a recoverable panic.

**Required fix.** Name the hazard. The shipped test is unaffected (the ceiling rejects long before `makechan`);
only *reproducing the mutation* becomes expensive. That distinction must be written down, or the next session
re-runs the mutant and loses a machine to it.

---

## MINOR 9 — Task 1's first step is unorderable

**The claim under attack.** Task 1 §Steps, first bullet: *"…confirm they are green, then **mutate the helper**
(e.g. make it return `nil` unconditionally) and confirm the existing tests **fail** … Revert the mutation."*

**The evidence.** That step precedes the step that **creates** the helper (*"Add `checkNilOptions` to
`adapter/http/helpers.go`"*, bullet 2). There is nothing to mutate at the point the plan says to mutate it.

**Recorded as a PASS — Task 1's claimed safety net is real.** `adapter/http/nil_option_test.go` has **23 cases**
across six msghttp entry points; `stdlib/nil_option_test.go` has **8 cases** across two. Each asserts the full
position string **and** that the message does NOT name `msghttp.NewConfig` (`:24-29`). A helper returning `nil`
unconditionally flips every position string, so the mutation would in fact fire — the step is mis-ordered, not
unfounded.

**Also verified clean (Task 1's factual base is sound):**
- All seven sites are **byte-identical** but for the constructor-name string literal.
- Both proposed helper signatures **compile at all seven** call sites.
- `checkNilOptions` is **unexported**; no exported signature changes.
- The **eighth-delegator claim is correct**: of 20 `nilOptionAt` call sites repo-wide, exactly **8** are standalone
  pre-checks.
- The **three `int64` exclusions are correct and complete**.

**Required fix.** Reorder, if the task survives BLOCKER 1.

---

## MINOR 10 — line-number convention inconsistency

**The claim under attack.** Task 1 §"Not in scope, deliberately": *"`adapter/database/sql/queuestore.go:45` — an
**eighth** delegator pre-check exists repo-wide."*

**The evidence.** The seven-site table uses the **`for` line** for every entry. The excluded eighth is cited at
`:45`, which is the **comment-banner line**; the `for` is at `:48`:

```
$ sed -n '45,48p' adapter/database/sql/queuestore.go
	// Delegator pre-check (Spec 015 §3.4, ADR 0031 D-R): both delegates
	// re-scan opts and find nothing. The duplicated pass is deliberate — it
	// buys a truthful position at this entry point.
	for i, opt := range opts {
```

**Required fix.** Cite `sql/queuestore.go:48`.

---

## MINOR 11 — the docs-link baseline excluded the plan itself

**The claim under attack.** Task 4 §Steps: *"Baseline at this branch point is **exactly two arm-1 false
positives** … and **zero** arm-2 hits."*

**The evidence.** Both arms of the CLAUDE.md gate iterate `git ls-files '*.md'`. `docs/plans/030-post-029-maintenance.md`
is **untracked** at the time the baseline was taken, so **the plan's own links were never scanned**.

Verified by hand by the auditor: **all seven resolve.** The baseline figure itself is confirmed — exactly two
arm-1 false positives, zero arm-2.

**Required fix.** Note that the baseline must be re-measured once the plan is staged (`git add -N`), so the gate
covers the artifact it governs.

---

## MINOR 12 — backlog items 3, 6, 7 dropped without a written carry-forward

**The claim under attack.** Plan 030 §"Governing artifacts": its parent is *"the **backlog** recorded in
`../HANDOVER.md` §6 … items **2** (Task 1), **5** (Task 2), **8** (Task 3) and **4** (Task 4)."*

**The evidence.** HANDOVER §6 carries **eight** items. Item 1 is `~~DONE~~`. The plan consumes 2, 4, 5 and 8. Items
**3** (the Plan 028 AST gate is syntactic, not a dominance proof), **6** (the byte-ceiling class) and **7**
(`routing.WithReleaseWhen` reaches the same unbounded per-group growth) are **not mentioned anywhere in the plan**
— not as scope, not as deferred, not as out of scope.

**Why it matters.** The next handover has to re-derive the residue from the previous handover, which is the exact
drift CLAUDE.md's handover section exists to stop.

**Required fix.** A carry-forward section naming 3, 6 and 7 as open and out of scope.

---

## Auditor's method note

Every command in this record was run by the auditor on the tree at `2b2dec1` with `GOTOOLCHAIN=go1.25.13`. The
24-error 386 list, the wrap-tolerant `first statement` scan, the 11 decimal occurrences, the gate's arm partition,
the two baseline files, the `git ls-files` blindness to the untracked plan and the eighth-delegator count are all
first-hand output, not transcription. No file in the repository was modified.

---

**VERDICT: NOT SAFE TO IMPLEMENT.**

*The plan's arithmetic is unusually good … What fails is not measurement but **scope**.*
