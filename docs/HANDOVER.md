# Session handover — msgin

> **READ FIRST.** Read `CLAUDE.md`, then this file, then the artifacts in §3. **Trust `git log` and the tree over
> this document.** Every count below was measured when written; **re-derive before relying on one** — that has
> failed in thirteen consecutive handovers, including the last one (see §7, "my own corrections were wrong again").
>
> ### ✅ PLAN 028 IS MERGED AND PUSHED. `origin/main` = `48bbe83`. Nothing is in flight.
> ### 🔴 PLAN 029 IS A DESIGN BUNDLE ONLY — **FIVE** AUDIT ROUNDS DONE, ALL "NOT SAFE TO IMPLEMENT". **NO CODE EXISTS.**
> ### ✅ REVISION 6 IS WRITTEN and its targeted verification pass is GREEN (§5). Next action: **ask the user for the implementation go-ahead, proposing SDD** — or run one confirming audit if they prefer.
>
> | | State |
> |---|---|
> | Branch | **`main`**, clean of code changes. No feature branch has been created yet |
> | `main` | **`48bbe83`** — merged + pushed |
> | Working tree | **10 uncommitted doc files** — see §2. No `.go` file is modified anywhere |
> | Suite | untouched; round 3 re-verified 11/11 `ok` at `48bbe83` |
> | Tags | **zero, as always.** Do NOT propose tagging |
>
> **SAME-MACHINE handover.** Nothing git-ignored matters. No SDD ledger exists — implementation has not started.

## 1. What this session did

1. **Folded audit round 2 into revision 3**, then **round 3 into revision 4**, of all three bundle artifacts.
2. **Ran adversarial audit rounds 3 and 4** (fresh Opus subagents, whole bundle each time).
3. **Took round 3's BLOCKER-1 design fork to the user and got a decision** — "split by kind", recorded in §6.
4. Verified each round's BLOCKER and headline MAJORs **independently against source** before accepting them —
   which caught real auditor errors in both directions, **and caught three errors of my own** (§7).

| Round | Verdict | Findings | Fix-verification of the PRIOR round |
|---|---|---|---|
| 1 | NOT SAFE | 2 B / 7 MAJ / 10 MIN | — |
| 2 | NOT SAFE | 3 B / 6 MAJ / 8 MIN | 10 of 19 unclean, **2 REGRESSED** |
| 3 | NOT SAFE | 1 B / 4 MAJ / 7 MIN | 5 of 17 unclean, 0 regressed |
| 4 | NOT SAFE | 1 B / 5 MAJ / 10 MIN | 6 of 12 unclean, 0 regressed |
| **5** | **NOT SAFE** | **2 B / 5 MAJ / 8 MIN** | **5 of 16 unclean, 0 NOT LANDED, 0 REGRESSED — cleanest yet** |

**Regressions are at zero and holding, and all four of round 3's substantive fold-ins (M3-1…M3-4) landed
cleanly.** But the unclean *fraction* rose, and round 4 diagnosed one shape behind almost all of it:
**a finding was folded into two of the three files, and the ADR absorbed four of the six misses.** The ADR is the
normative artifact — fold into all three, every time, and diff the three against each other before declaring done.

**No implementation code was written, and none is authorized.**

## 2. Exact state — the uncommitted tree

```
 M docs/HANDOVER.md                        ← this file
 M docs/adrs/0031-nil-option-elements.md   ← one added block: D-U's forward pointer to ADR 0032 D-Y
?? docs/adrs/0032-sizing-option-bounds.md  ← revision 6
?? docs/plans/029-audit-round-1.md         ← audit record (IMMUTABLE)
?? docs/plans/029-audit-round-2.md         ← audit record (IMMUTABLE)
?? docs/plans/029-audit-round-3.md         ← audit record (IMMUTABLE)
?? docs/plans/029-audit-round-4.md         ← audit record (IMMUTABLE)
?? docs/plans/029-audit-round-5.md         ← audit record (IMMUTABLE) — NEWEST
?? docs/plans/029-sizing-option-bounds.md  ← revision 6
?? docs/specs/016-sizing-option-bounds.md  ← revision 6
```

**Nothing is committed.** Revision 6 folds every round-5 finding and its verification pass is green, but **no audit has yet returned SAFE**, so it does not qualify for
CLAUDE.md's standalone-`docs:` handoff exception. **Ask before committing.**

## 3. Read these before acting

1. **`CLAUDE.md`** — hard rules: ask before writing implementation code, SDD is the default execution mode, never
   commit/push without approval, the adversarial-audit gate, the 8-module command loops.
2. **`docs/plans/029-audit-round-5.md`** — the work-list revision 6 folded. **All 15 findings are folded; see §5.**
3. **`docs/specs/016-sizing-option-bounds.md`** (rev 6) — §1 the gap, §2/§2.0/§2.1 the inventory, §3 the contract,
   §6 the ACs.
4. **`docs/adrs/0032-sizing-option-bounds.md`** (rev 6) — decisions **D-W**…**D-AB**.
5. **`docs/plans/029-sizing-option-bounds.md`** (rev 6) — Tasks 0–8 (renumbered in rev 4; Task 4 is new).
6. `029-audit-round-{1,2,3,4}.md` — for context on what earlier revisions were trying to do.

## 4. The design, in one paragraph

Exported sizing options either panic, corrupt runtime state, or silently remove the bound they exist to enforce,
when given a huge `n`. Each gains a **stated per-knob ceiling** (D-W) enforced *before* the hazard, reported
through the **existing** typed sentinel (D-X — zero net exported surface) in **one** message shape
`"%w: %s: %d not in [%d, %d]"`. Most reject at construction (R1); `memory.WithBuffer` has no error return so it
**latches** and reports at `Send`/`Stream` (R2), with its range check returning **unconditionally, independent of
the latch** (D-Y — the subtle line the whole increment turns on). A two-half class gate (D-AA) stops the class
returning.

**Why a runtime-derived bound was rejected, since it is counter-intuitive and will be re-proposed otherwise:**
`makechan` panics only above `maxAlloc`; *below* that it attempts the allocation and dies with an
**unrecoverable** `fatal error: out of memory` no `recover` can intercept. A guard matching the runtime's own
check therefore admits the *worse* value. `GOMEMLIMIT` does not help (measured).

## 5. State — revision 6 is written; its verification pass is GREEN

Revision 6 folded **all** of round 5's 2 BLOCKERs, 5 MAJORs and 8 MINORs.

**BLOCKER-1** — `msghttp.WithMaxResponseBytes` moved to the **deferred** arm (it is a **byte** cap, per the §6
"split by kind" rule). `drainBounded` is five of six reads of the field; the sixth, `exchange.go:130-131`, is
`io.ReadAll(io.LimitReader(resp.Body, max))` whose body is **retained**. Census: **9 fixed + 3 deferred + 4 safe
= 16 options + `burst` = 17 keys.** **Safety cause (d) is now empty**, as (c) was in revision 5.

**BLOCKER-2** — all seven `WithReplayBuffer` twins swept, including Plan Task 7.

### 5.1 The verification pass — run, with evidence

1. **§2.1 re-derived ROW BY ROW from D-AB**, not read off the verdict column. Census confirms 9/3/4 + `burst`.
2. **🔴 The check that would have caught BOTH BLOCKERs, run explicitly:** for every remaining *safe* row, grep
   **every** read of the underlying field — because both BLOCKERs were *"one site says safe, another site
   accumulates"*. Result: `pollMaxBatch` (3 reads), `threshold` (2), `maxPayloadBytes` (2), `successStatus` (4),
   `burst` (4) — **no `make`, no `ReadAll`, no `append`, no buffer among any of them.** Clean.
3. **The cross-file grep guard, run against the classification revision 6 just changed** (not just against
   retracted phrases — that omission WAS round-5 BLOCKER-2). **It caught two real survivors**: ADR `:21`
   *"+2 deferred"* and Plan `:286` *"Confirm the 5 safe rows"*. Both fixed; re-run clean.
4. **Docs-link gate**, both arms, all tracked `.md`: only the two documented parser false-positives
   (`docs/plans/m`, `docs/specs/factory(fireTime` — Go identifiers, not links).
5. **§-reference sanity:** §3.1–§3.9 all exist and all citations resolve (one dangling `§3.10` found and fixed).
6. **Census agreement** verified across all three files.

### 5.2 Next actions

1. **Ask the user for the implementation go-ahead, proposing SDD** (a fresh implementer subagent per task,
   coordinator verifies green and commits, adversarial reviewer before delivery).
2. **Optionally** run one confirming audit round first. The round-5 auditor argued **against** a sixth full round
   — every finding it raised was mechanical — and revision 6 additionally ran the verification pass it prescribed.
   The finding trend is **19 → 17 → 12 → 16 → 15**, with **regressions at zero for three straight rounds**.
3. If the user approves, **also ask whether to commit the design standalone as `docs:`** before code (CLAUDE.md's
   cross-machine/fresh-session exception) — it is now gate-cleared enough to qualify.

## 6. Decisions taken with the user — DO NOT RELITIGATE

| Decision | Choice |
|---|---|
| Scope | The **whole sizing-knob class**, not just the named instance |
| Semantics | **Typed error at construction** where a constructor can report; latch otherwise |
| Ceiling mechanism | **Stated per-knob ceiling** (D-W), after the runtime-derived bound was disproved by measurement |
| `WithConcurrency` | **In scope**, same treatment |
| B-2 (round 1) | **Widen to all 7 knobs** |
| `WithBuffer(-1)` | **Fold in** — the audit ruled fold-in |
| **BLOCKER-1 (round 4) — RULE APPLICATION, not a new decision** | **`msghttp.WithReplayBuffer` gets a ceiling here** (9th defective knob). Its unit is **events**, and the "split by kind" rule below already says a countable unit gets a ceiling while a **byte** cap is deferred. Flag it to the user; do **not** reopen the fork. |
| **BLOCKER-1 (round 3) — 2026-08-21** | **"Split by kind."** `WithCompletionSize` joins the census as an 8th defective knob **with a ceiling**. The two **byte** knobs (`WithMaxBodyBytes`, `WithMaxEventBytes`) get **corrected verdict strings + a documented hazard**, and their ceiling is **deferred** — because CLAUDE.md's Sensible-defaults gate says a byte cap depending on the caller's payload size must not be guessed. **The user chose this over (a) extending to all three with uniform ceilings and (b) narrowing the contract.** |

## 7. Gotchas — these will bite the next session

- **🔴 MY OWN "CORRECTION" WAS WRONG AGAIN, AND IT SHIPPED INTO THREE DOCUMENT HEADERS.** I measured
  `WithMaxGroups` growth with a **zero-value `msgin.Message[any]{}`** — no payload, no headers, no id — got
  283 MiB, and declared round 2's ~1 GB figure "did not survive re-derivation". **With a realistic `msgin.New`
  message round 2's figure reproduces exactly** (1,042.7 MiB cumulative / 853.4 MiB live under `-race`). The two
  probes measured different things. **A measurement is only as good as its fixture — state the fixture beside
  every figure.** This is the third consecutive session where a "correction" was itself wrong.
  **Round 4 extended this:** §1.4's live column was read **without a GC** and disagreed with its own prose four
  lines above (67.8 vs 28.7 MiB). So the rule is **"name the fixture AND the measurement protocol"** — GC before
  read, `TotalAlloc` vs `HeapAlloc`, `KeepAlive`.
- **I also copied two round-2 numbers forward while holding the correct data.** I wrote *"six `msghttp` keys"*
  when **my own AST scan output on screen listed seven**, and wrote Task 3's small-`n` bullet without the
  `OverflowReject` fixture **that my own probe had used**. Re-deriving a number is not enough — you must re-derive
  it **against the revision you are writing**, and check the prose against the probe that produced it.
- **🔴 A GUARD YOU WRITE BUT DO NOT RUN IS WORTH NOTHING.** Revision 5 *added* the cross-file grep guard round 4
  recommended, ran it against a list of **retracted phrases** — where it worked, catching a real survivor — and
  **never ran it against the classification it had just changed**. Round-5 BLOCKER-2 is seven surviving twins that
  one invocation of that command would have listed. **Run every guard against the thing you just edited, not only
  against the thing the last audit named.**
- **🔴 A ROW'S VERDICT CAN BE STALE WITHOUT ANY COUNT BEING WRONG.** Round-4 BLOCKER-1 (`WithReplayBuffer`) and
  round-5 BLOCKER-1 (`WithMaxResponseBytes`) were both rows whose "safe" verdict survived a criterion written to
  catch them — and **neither changed the 16/17 totals**, so no count-check could find either. **Re-derive §2.1
  row by row FROM the criterion; never read the verdict column.**
- **🔴 EVIDENCE ON SCREEN IS NOT EVIDENCE NOTICED.** `exchange.go:131`'s
  `io.ReadAll(io.LimitReader(resp.Body, max))` appeared in my own grep output in an earlier session and I
  explicitly thought *"that's a fourth candidate — whatever, the point stands"* and moved on. It became
  round-5 BLOCKER-1 two revisions later. **When a probe surfaces something outside the question you asked, file
  it before moving on.**
- **🔴 ROUND 4's DIAGNOSIS: a finding folded into TWO of the three files reads as done and is not.** Four of
  round 4's six unclean rows were **the ADR missing a fold-in the spec and plan received** — and the ADR is the
  **normative** artifact. **Fold into all three, every time, then diff the three against each other** before
  declaring a revision written.
- **🔴 STATING A CRITERION DOES NOT HELP UNLESS YOU RE-DERIVE EVERY ROW FROM IT.** Revision 4 introduced D-AB
  precisely to stop the census moving — then kept `WithReplayBuffer`'s pre-existing *"safe"* verdict and invented
  a **safety cause to justify it** (cause (c), whose text is self-refuting). The criterion caught it one round
  later. **When you add a rule, re-run it against every row, including the ones you are not changing.**
- **Reading the ACCESSOR is not reading the VALIDATOR.** I asserted `WithMaxBodyBytes(-1)` means "use 1 MiB"
  because `maxBody()` back-fills `<= 0`. `NewConfig` **rejects** it 900 lines earlier. Check the constructor path,
  not just the getter.
- **Fold-ins are where this design keeps failing, not the design itself.** Every round: a fix repairs the instance
  the finding named and leaves the class reachable through another door. **Fix the class; re-derive every number
  you touch.**
- **`docs/plans/029-audit-round-{1,2,3}.md` are immutable execution records.** Do not edit them to match a later
  revision — that destroys the audit trail.
- **A subagent can die from a session limit *after* writing its output.** Check for the output file before
  retrying — a needless retry costs a full audit's tokens.
- **Do not write scratch `.go` files into the repo.** Use the scratchpad dir and run `git status --short` after
  any probe. Build probes as a throwaway module with
  `replace github.com/kartaladev/msgin => /Users/zakyalvan/Documents/RND/msgin`.
- **Docs-link gate:** arm 1 reports Go generics inside code fences as false positives — a hit only matters if it
  names a plausible `.md` path. Two such pre-existing hits are expected (`docs/plans/m`,
  `docs/specs/factory(fireTime`) and are code, not links. Both arms were run this session and **both were
  vacuity-proved against the new spec file** (planted a bad link and a bad anchor; each fired; reverted clean).
- **`GOTOOLCHAIN=go1.25.13`.** `harness` has no test files — `go test` there is a false pass; use `go vet`.
- Never commit `.claude/settings.json`.

## 8. Backlog — unchanged except item 1

1. **The sizing-option class** → **this bundle** (Spec 016 / ADR 0032 / Plan 029). In design, not implemented.
   Revision 3 written; round 3 audited; **revision 4 pending**.
2. **Seven copies of the delegator pre-check loop** in `adapter/http` (×5) and `adapter/http/stdlib` (×2).
   A package-local helper collapses each to one line (~35 lines).
3. **The Plan 028 AST gate is syntactic, not a dominance proof.** Two contrived shapes defeat it; both named in
   the file header. Promoting it to a `go/analysis` analyzer was rejected as out of scope pre-v1.
4. **The `gin` increment** still needs a plan number, and its ADR is still a forward reference.
5. **Minor godoc wording class** — four sites say the apply loop is "this constructor's first statement" when a
   `cfg := …` initializer precedes it, and `adapter/http/options.go:1110` has a line break leaving a dangling `(`
   before `ErrInvalidMaxBodyBytes`. **Fix the class in one pass.** Deliberately NOT folded into Plan 029.
6. **NEW — the byte-ceiling class**, deferred out of Plan 029 by the §6 "split by kind" rule. **THREE members,
   not two** (round-5 BLOCKER-1 added the third): `msghttp.WithMaxBodyBytes` (`encode.go:102`
   `io.ReadAll(http.MaxBytesReader(…))`), `msghttp.WithMaxEventBytes` (`sse.go:384-389`, a `bytes.Buffer`), and
   **`msghttp.WithMaxResponseBytes`** (`exchange.go:130-131` `io.ReadAll(io.LimitReader(resp.Body, max))` — the
   body is **retained** as the reply payload; `drainBounded` is only five of its six reads).
   Measured: a 64 MiB body is rejected at the 1 MiB default and **fully read (375 MiB TotalAlloc) at `1<<62`**.
   Needs its own increment deciding between a ceiling and a documented opt-in unbounded state.
   **🔴 CORRECTION (round-4 M4-2):** an earlier revision claimed `WithMaxBodyBytes(-1)` "means use 1 MiB today".
   **False** — `NewConfig` **rejects** it (`options.go:1128-1130` → `ErrInvalidMaxBodyBytes`). The `maxBody()`
   back-fill (`:236`) applies only to a hand-built `*Config`. So an opt-in unbounded state would need a **new
   sentinel value**, not merely a reinterpretation of a negative `n`.
