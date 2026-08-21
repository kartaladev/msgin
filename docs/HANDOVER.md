# Session handover — msgin

> **READ FIRST.** Read `CLAUDE.md`, then this file, then the artifacts in §3. **Trust `git log` and the tree over
> this document.** Every count below was measured when written; **re-derive before relying on one** — that has
> failed in fourteen consecutive handovers.
>
> ### ✅ PLAN 029 IS IMPLEMENTED AND GATE-CLEARED on branch `fix/sizing-option-bounds` (8 implementation tasks, **9 commits** — 1 design + 8 task commits; Task 0 was evidence-only and carried none).
> ### 🔴 IT IS **NOT MERGED AND NOT PUSHED**. `origin/main` is still `48bbe83`. Merging, pushing and branch deletion need EXPLICIT user approval — they were deliberately not taken.
>
> | | State |
> |---|---|
> | Branch | **`fix/sizing-option-bounds`**, **9 commits** ahead of `main` (`git log --oneline main..HEAD | wc -l` → 9), working tree clean |
> | `main` / `origin/main` | **`48bbe83`** — unchanged, untouched |
> | Suite | **green**: 8 modules × 8 CI steps run standalone (`GOWORK=off`); see §5 |
> | Exported surface | **delta ZERO**, verified by AST diff over all 540 exported decls in all 8 modules |
> | Tags | **zero, as always.** Do NOT propose tagging |
>
> **SAME-MACHINE handover.** `.superpowers/sdd/029-sizing-option-bounds/` holds the task briefs, per-task reports
> and the SDD ledger. It is **gitignored** (this branch added the rule), so it does NOT survive a fresh clone —
> the git history is the durable record.

## 1. What this session did

Executed **Plan 029 Tasks 1–8** via subagent-driven development: a fresh implementer subagent per task, the
coordinator verifying green and committing, an adversarial reviewer subagent per task before moving on.

| Task | Commit | What landed | Review |
|---|---|---|---|
| — | `c951189` | Spec 016 / ADR 0032 / Plan 029 + 5 audit rounds (design, committed ahead of code) | — |
| 1 | `5278797` | `endpoint`: `WithMaxInFlight` (1<<20), `WithConcurrency` (1<<16) | clean, 0 findings |
| 2 | `982ae38` | `msghttp`: `WithConnectionBuffer`, `WithMaxConnections`, `WithReplayBuffer` (all 1<<16) | clean |
| 3 | `543d977` | `memory`: `WithCapacity`, `WithMaxGroups` (both 1<<20) | clean |
| 4 | `55beb1c` | `routing`: `WithCompletionSize` (1<<16) | clean, ZERO findings |
| 5 | `be42d87` | `memory.WithBuffer` — the R2 latch family, ADR 0032 **D-Y** | clean after 1 fix round |
| 6 | `6c3f931` | AC-4: the zero-size-element property `credit.go:21` and `sem` rest on | clean, ZERO findings |
| 7 | `3569d16` | The root AST class gate (`sizing_option_class_gate_test.go`) | clean |
| 8 | **this commit** | Whole-branch delivery gate: `/simplify`, both reviews, 8×8 matrix, docs | — |

**Nine sizing knobs are now bounded**; three byte knobs are recorded as class members with a **deferred** remedy
(§8 item 6) rather than certified safe.

## 2. Exact state

Working tree at the moment this file was written (before the Task 8 commit was made):

```
 M CLAUDE.md
 M adapter/http/helpers.go        adapter/http/options.go
 M adapter/memory/groupstore.go   adapter/memory/helpers.go
 M adapter/memory/memory.go       adapter/memory/queuestore.go
 M docs/HANDOVER.md               docs/specs/016-sizing-option-bounds.md
 M endpoint/consumer.go           endpoint/helpers.go
 M routing/aggregator.go          routing/helpers.go
 M sizing_option_class_gate_test.go
```

Last commit before this one: `3569d16 test(core): gate the sizing-option class`.

## 3. Read these before acting

1. **`CLAUDE.md`** — hard rules: ask before writing implementation code, SDD is the default execution mode, never
   commit/push without approval, the adversarial-audit gate, the 8-module command loops.
2. **`docs/specs/016-sizing-option-bounds.md`** — §2.1 the classification, §3.1 the one error shape, §3.2/§3.3 the
   R2 latch and D-Y, §3.4 the ceilings, §6 the ACs.
3. **`docs/adrs/0032-sizing-option-bounds.md`** — decisions **D-W**…**D-AB**.
4. **`docs/plans/029-sizing-option-bounds.md`** — Tasks 0–8.
5. `docs/plans/029-audit-round-{1..5}.md` — immutable audit records. Do not edit them to match a later revision.

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

## 5. The Task 8 delivery gate — what was actually run

Full evidence lives in `.superpowers/sdd/029-sizing-option-bounds/task-8-report.md` (gitignored). Summary:

1. **`/simplify`** — four parallel angle reviewers (reuse, simplification, efficiency, altitude). Three of four
   converged on the same production finding, which was **applied**: the nine inline `fmt.Errorf` range checks
   became a per-package unexported `checkRange(sentinel, site, n, lo, hi)` helper in the four existing
   `helpers.go` files (the `nilOptionAt` precedent, ADR 0031 D-R). The point is not DRY — it is that the
   **enforced** range and the **printed** range are now the same two values; inline, each site spelled each bound
   twice and the spellings had already drifted (`<= 0` guards printing a lower bound of `1`). Three mutants
   (drop the upper conjunct, drop the lower conjunct, print `hi+1`) were planted in the helper and each was
   killed; all reverted clean.
2. **`/code-review`** over `main..HEAD` plus the uncommitted refactor — **no Critical**, 3 Important, 8 Minor.
   The reviewer re-derived the old predicate vs `checkRange` at all nine sites, mutation-proved **D-Y** by
   planting the nested-`return` shape (a real `makechan` panic followed), and ran boundary mutants both
   directions. **Seven findings were folded into this commit**, three of them substantive:
   - **the class gate matched only a bare `*ast.Ident`**, so `...int` / `[]int` / `*int` were invisible while
     three documents promised "ANY position, EITHER direction". Now unwrapped; **key set unchanged at 17**, so
     it closed a latent hole without reclassifying anything. Vacuity-proved with variadic and slice probes.
   - **the spec and ADR still showed the inline `fmt.Errorf`** the refactor replaced — the project's own
     "docs contradict the code" class. As-delivered notes added to Spec §3.1/§3.3 and ADR 0032 D-X.
   - **the AC-5 arm partition was decorative** — `arm` was read once, to build a subtest name, so the normative
     9/1/3/6 split was asserted nowhere and a row could be moved between arms silently. Now asserted, and
     vacuity-proved by moving one row.

   The four other fixes corrected **false claims in godoc I had just written** (helper comments describing
   drift that never happened in those packages, and an R2 caller that exists only in `memory`), a duplicated
   clause in Spec §4, and the stale artifact statuses (**M-5**, below). Two were triaged, not fixed, with
   rationale in the task report: `adapter/memory`'s pre-existing coverage, and the reviewer's inability to
   re-run `govulncheck`/Docker in its own environment (both **were** run here — see below).
3. **`/security-review`** over the same range — **no HIGH or MEDIUM findings**, with an auditable null result:
   all nine accept-sets verified strictly narrower or identical, SSE replay/Last-Event-ID untouched, the new
   error strings carrying only an `int` and a compile-time site name, and no new error reaching an HTTP
   response body.
4. **8 modules × 8 CI steps**, each standalone with `GOWORK=off` — build, vet, `gofmt -l`, `CGO_ENABLED=0` build,
   `go mod tidy` + no diff, `govulncheck`, `golangci-lint`, `go test -race -shuffle=on`.
5. **Exported-surface AST diff, `main` vs `HEAD`, all 8 modules: IDENTICAL** — 540 exported declarations, matched
   with full signatures (not just names). **Vacuity-proved twice**: an added exported func in `adapter/cron/crontest`
   and a `WithMaxInFlight(n int) → (n int64)` signature change were each reported, each reverted clean. `apidiff`
   on root is a supplementary 0/0 and is NOT the gate (Plan 028 proved it is blind outside root).
6. **Coverage — no regression.** Per-package plain coverage, `main` → `HEAD`: `endpoint` 99.2 → 99.2,
   `routing` 100.0 → 100.0, `adapter/http` 100.0 → 100.0, `adapter/memory` **73.3 → 74.0**.
7. **Docs-link gate**, both arms, all tracked `.md`: only the two documented parser false positives. Arm 2
   vacuity-proved (planted a bad anchor, it fired, reverted clean).

## 6. Decisions taken with the user — DO NOT RELITIGATE

| Decision | Choice |
|---|---|
| Scope | The **whole sizing-knob class**, not just the named instance |
| Semantics | **Typed error at construction** where a constructor can report; latch otherwise |
| Ceiling mechanism | **Stated per-knob ceiling** (D-W), after the runtime-derived bound was disproved by measurement |
| `WithConcurrency` | **In scope**, same treatment |
| `WithBuffer(-1)` | **Fold in** — the audit ruled fold-in |
| **BLOCKER-1 (round 4)** | **`msghttp.WithReplayBuffer` gets a ceiling here** (9th defective knob) — a countable unit gets a ceiling, a byte cap is deferred |
| **BLOCKER-1 (round 3) — 2026-08-21** | **"Split by kind."** `WithCompletionSize` joins the census with a ceiling; the **byte** knobs get corrected verdicts + a documented hazard, ceiling **deferred**, because CLAUDE.md's Sensible-defaults gate says a byte cap depending on the caller's payload size must not be guessed |

### Deviations recorded this session

- **Task 0** ran the `WithConcurrency` spawn-loop probe at `n=1<<24` under an RSS watchdog instead of the brief's
  `1<<40`: a companion probe hit ~1.19 GB RSS in ~150 ms, so the literal reproduction risked exhausting host
  memory. Same qualitative "timed out, not observed" fact, at a safe scale. Adjudicated sound.
- **Task 2** reused the existing `serveWithHeader` helper instead of writing a new Last-Event-ID fixture.
- **Task 7** found and Task 8 fixed a **Spec 016 self-contradiction**: §2.1's verdict column called
  `msghttp.WithSuccessStatus` `safe (a)` while §2.1's own closing prose and AC-5's `msghttp` fixture table both
  put it in the **rejects** arm. Resolution: they were describing **two different partitions** — §2.1 classifies
  by *why* a knob is/is not a class member, AC-5 partitions by *what a row asserts at `1<<62`*. The spec now says
  so explicitly and AC-5 has **four** behavioral arms (9 fixed + 1 rejects + 3 deferred + 6 safe = 19 rows), with
  `WithSuccessStatus` in a `rejects` arm of its own. Counts were re-derived, not incremented.

## 7. Gotchas — these will bite the next session

- **🔴 `go test ./... -race` now peaks at ~2.27 GB RSS, and ~2.06 GB of it is ONE test.**
  `endpoint`'s `TestNewConsumer_ConcurrencyCeilingAccepts` spawns 65,536 workers at the ceiling; under `-race`
  each goroutine carries ~30 KB of race context, so the cost is **~32× the plain build**. Measured on this
  branch: `endpoint` alone `-race` = 2,297 MB, and 232 MB with that one test skipped; whole-root
  `go test ./... -race -shuffle=on` = **2,272,870,400 B / 6.6 s**. The plan predicted ~257 MB — it was **8× low**.
  This fits `ubuntu-latest` comfortably and was **deliberately not "fixed"**: the only fixes available are to
  lower `n` under `-race` (CI runs *only* the `-race` pass, so the ceiling would then never be exercised in CI)
  or to export the ceiling for tests (forbidden — blackbox-only + zero-exported-surface). **Diagnose a future CI
  OOM here first.**
- **Two project rules jointly force that cost**: blackbox-only testing closes the `export_test.go` seam, and
  zero-new-exported-surface closes lowering the ceiling. Neither is wrong; the interaction is worth knowing.
- **Classification arms ≠ behavioral arms.** Six spec revisions conflated them (see §6). When you read §2.1's
  verdict column, you are reading *why a knob is a class member* — **not** what its gate row asserts.
- **🔴 The class gate WILL go red when §8 item 6's byte ceilings land, and that is correct.** The repair is to
  MOVE those rows from the `deferred` arm to the `fixed` arm and rewrite the assertion — **never** to weaken the
  production check to keep the gate green. The gate file now says this inline; do not remove that comment.
- **🔴 CHECK AN ARTIFACT'S STATUS LINE WHEN ITS INCREMENT LANDS.** ADR 0031 merged with Plan 028 and sat at
  `PROPOSED` for three days; ADR 0032 and Spec 016 were about to do the same. Both were caught by the Task 8
  code review, not by anything mechanical. An ADR whose decisions have shipped is `ACCEPTED` (Nygard, which
  CLAUDE.md mandates). **Two instances in two consecutive increments makes it a class** — there is no gate for
  it, so it is a manual step in the delivery checklist until someone writes one.
- **A verdict nothing asserts is a comment.** The class gate's `arm` field was written 19 times and read once,
  to build a subtest name — so the 9/1/3/6 partition that two spec sections call normative was enforced by
  nobody. It is asserted now. Apply the same suspicion to any other "table column" that looks load-bearing.
- **Re-derive counts, never increment them.** CLAUDE.md's Project-status line was re-derived this session:
  specs 16, ADR *files* 31 (numbers 0001–0032, **no 0024**), plans **29 distinct** / **45 files**. Root's
  exported-symbol count (**103**) and `errors.go` sentinel count (**43**) were verified **by set, not by count** —
  both sets are byte-identical to `main`, which is the only check that means anything here.
- **A measurement is only as good as its fixture.** Prior sessions shipped three wrong "corrections" in a row by
  measuring with a zero-value `msgin.Message[any]{}`, or reading live heap without a GC. Name the fixture AND the
  protocol (GC before read, `TotalAlloc` not `HeapAlloc`, `KeepAlive`) beside every figure.
- **Docs-link gate:** arm 1 reports Go generics inside code spans as false positives — a hit only matters if it
  names a plausible `.md` path. Two such pre-existing hits are expected (`docs/plans/m`,
  `docs/specs/factory(fireTime`) and are code, not links.
- **`GOTOOLCHAIN=go1.25.13`** (local default is 1.26). **`govulncheck` is installed but at `$(go env GOPATH)/bin`,
  which is NOT on `PATH`** — prepend it or the gate silently cannot run. `harness` has no test files — `go test`
  there is a false pass; use `go vet`.
- **Do not write scratch `.go` files into the repo.** Use the scratchpad dir and run `git status --short` after
  any probe.
- Never commit `.claude/settings.json`.

## 8. Backlog

1. ~~**The sizing-option class**~~ → **DONE.** Spec 016 / ADR 0032 / Plan 029, delivered on
   `fix/sizing-option-bounds` (8 commits, gate-cleared, awaiting merge approval).
2. **Seven copies of the delegator pre-check loop** in `adapter/http` (×5) and `adapter/http/stdlib` (×2).
   A package-local helper collapses each to one line (~35 lines).
3. **The Plan 028 AST gate is syntactic, not a dominance proof.** Two contrived shapes defeat it; both named in
   the file header. Promoting it to a `go/analysis` analyzer was rejected as out of scope pre-v1.
4. **The `gin` increment** still needs a plan number, and its ADR is still a forward reference.
5. **Minor godoc wording class** — four sites say the apply loop is "this constructor's first statement" when a
   `cfg := …` initializer precedes it, and `adapter/http/options.go` has a line break leaving a dangling `(`
   before `ErrInvalidMaxBodyBytes`. **Fix the class in one pass.** Deliberately NOT folded into Plan 029.
6. **The byte-ceiling class**, deferred out of Plan 029 by the "split by kind" rule. **THREE members**:
   `msghttp.WithMaxBodyBytes` (`encode.go:102`, `io.ReadAll(http.MaxBytesReader(…))`), `msghttp.WithMaxEventBytes`
   (`sse.go:384-389`, a `bytes.Buffer`), and `msghttp.WithMaxResponseBytes` (`exchange.go:130-131`,
   `io.ReadAll(io.LimitReader(resp.Body, max))` — the body is **retained** as the reply payload; `drainBounded`
   is only five of its six reads). Measured: a 64 MiB body is rejected at the 1 MiB default and **fully read
   (375 MiB TotalAlloc) at `1<<62`**. Needs its own increment deciding between a ceiling and a documented opt-in
   unbounded state. **🔴 An opt-in unbounded state would need a NEW SENTINEL VALUE**, not a reinterpretation of a
   negative `n` — `NewConfig` **rejects** `WithMaxBodyBytes(-1)`; the `maxBody()` back-fill applies only to a
   hand-built `*Config`. **When this lands, move the three gate rows from `deferred` to `fixed` (see §7).**
7. **🔴 NEW — per-group member growth is still unbounded, and `WithCompletionSize`'s ceiling does not cover it.**
   Found by the Task 8 `/simplify` altitude pass and verified against source. `adapter/memory/groupstore.go`
   bounds the **number of groups** (`len(s.groups) >= s.maxGroups`) but there is **no per-group member cap**:
   `g.msgs = append(g.msgs, msg)` grows without limit. `routing.WithCompletionSize` is sugar over a release
   strategy, so the identical growth is reachable through `routing.WithReleaseWhen(func(msgin.MessageGroup) bool
   { return false })` — which takes a **`func`**, and is therefore structurally invisible to the Plan 029 class
   gate (that gate scans for `int`/`int64` parameters). **This is not a Plan 029 regression and Spec 016 does not
   claim to cover it** — the spec's stated class is sizing *options*, and a func-typed option is outside it by
   construction. But the remedy covers one spelling of the hazard while the general mechanism ships unbounded.
   Likely fix: a `maxGroupMembers` admission check beside the existing `maxGroups` check, returning
   `msgin.ErrOverflowDropped`, which bounds it for *every* release strategy. Needs its own spec/ADR — it is a
   behavioral change to `memory.GroupStore`.
8. **NEW — `internal/` was never considered as the home for the shared per-package helpers.** `endpoint`,
   `routing`, `adapter/memory` and `adapter/http` are all in the **root module**, so an unexported
   `internal/sizing` package would be importable by all four and adds **zero** exported surface — and CLAUDE.md's
   own quality gate says *"keep internals in `internal/` so they can't be imported."* ADR 0031 D-R adjudicated
   "export from root vs duplicate per package" for `nilOptionAt` and chose duplication, but it framed the choice
   as a **false dilemma**: `internal/` is a third option nobody costed. Four copies each of `nilOptionAt` and
   `checkRange` (plus `nilFuncAt`, `nilFuncStep`, `boxMessage`) are the standing cost. Worth an ADR that either
   adopts `internal/` or records explicitly why not.
9. **NEW — the two root AST gates duplicate their repo walk verbatim.** `option_guard_gate_test.go`'s `scanRepo`
   and `sizing_option_class_gate_test.go`'s `scanSizingParamRepo` are in the **same** test package and repeat ~35
   identical lines (`WalkDir`, dot-dir/`vendor`/`_test.go` skips, `filepath.Rel`, `parser.ParseFile`). Their
   header comments have **already diverged** on which directories are skipped. Deliberately NOT fixed in Plan 029:
   it edits a shipped Plan 028 file this increment does not otherwise touch, and there is a real counter-argument
   (two independent gates sharing one walker means one traversal bug blinds both). Decide it on its own merits.
