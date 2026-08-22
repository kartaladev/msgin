# Plan 032 — Bound the three `msghttp` byte caps, and empty the class gate's `deferred` arm

> **For agentic workers:** REQUIRED SUB-SKILL: `superpowers:subagent-driven-development` (the project default) or
> `superpowers:executing-plans`. Steps use checkbox (`- [ ]`) syntax.
>
> **Go-skills hard rule ([CLAUDE.md](../../CLAUDE.md), restated here because `superpowers:writing-plans` omits
> it):** every task starts from **`cc-skills-golang:golang-how-to`**, the always-on orchestrator, which routes
> this increment to `golang-safety` (numeric conversion overflow, unbounded reads, defensive bounds),
> `golang-security` (remote-peer-driven input at the HTTP boundary), `golang-error-handling` (sentinel reuse,
> `%w` wrapping, `errors.Is`), `golang-design-patterns` (functional options, the set-flag pattern),
> `golang-testing` and `golang-documentation`. Load the primary skill **plus all applicable secondary skills
> together, up front** — do not work from memory.
> **`superpowers:test-driven-development`** governs every task: red → green → refactor, failing test first, never
> implementation ahead of a failing test. **`gopls`** (via the `LSP` tool) for all navigation, diagnostics and
> refactoring — go-to-definition, find-references, rename, post-edit diagnostics — **not `grep`** when reasoning
> about Go symbols. The project-local overrides apply and beat samber's guidance where they conflict:
> **`table-test`** (assert-closure form, never `want`/`wantErr` fields; `ctx` modifier; `t.Context()`),
> **`use-mockgen`** (uber-go/mock, `--typed`, alongside the interface — **not exercised** by this increment; no
> new interface or test double is introduced), and **`use-testcontainers`** (**not exercised** — this increment
> touches no external resource; `httptest` is stdlib and stays).
>
> **This plan is deliberately thin** (Plan 024/026/028/029/031 precedent): signatures, positions, branch coverage
> and commit boundaries — **no embedded implementations**. Write the code TDD-first from the tables below.

**Revision 1 — pre-audit. NOT approved for implementation.**

🔴 **The design this plan executes was decided WITHOUT USER RATIFICATION.** Every decision in
[ADR 0034](../adrs/0034-byte-cap-ceilings.md) (**D-AM** … **D-AT**) is open to reversal.
[Spec 018 §8](../specs/018-byte-cap-ceilings.md) lists the four worth a second look. **One of them changes this
plan's shape:**

- **D-AN(b)** — *no off-state*. If the user wants an explicit "unbounded" sentinel, that is a **new exported
  const plus a branch in Task 1**, and it must land before Task 1 starts, not after Task 3.
- **D-AO** — *the ceiling is `math.MaxInt32`*. Any other value changes one constant and every rendered-message
  assertion in Tasks 1 and 3 (`2147483647` appears in ~8 string literals). Changing it after Task 3 is a
  cross-file sweep, not a one-line edit.

**The adversarial design audit has not run.** [CLAUDE.md](../../CLAUDE.md) makes it a hard gate: a fresh Opus
subagent attacks the complete bundle — [Spec 018](../specs/018-byte-cap-ceilings.md) +
[ADR 0034](../adrs/0034-byte-cap-ceilings.md) + this plan — **together**, before any implementation code. Two
rounds is this project's established norm; Plan 029 needed **five**.

> **Plan number — re-derived, not assumed.** `ls docs/plans/[0-9]*.md | sed -E 's|.*/([0-9]{3}).*|\1|' | sort -u | tail -3`
> → `029 030 031`. **030 and 031 are both TAKEN** by concurrent, undelivered work
> ([`030-post-029-maintenance.md`](030-post-029-maintenance.md), [`031-group-member-bounds.md`](031-group-member-bounds.md)).
> This plan is **032**.
>
> 🔴 **File overlap with the two live plans — check before branching.**
>
> | Plan | Touches | Overlap with 032 |
> |---|---|---|
> | **030** (backlog sweep) | `adapter/http` godoc, test-file constants, 386 arm | **`adapter/http/options.go` and `adapter/http/helpers.go` are being edited RIGHT NOW.** Line offsets in this plan will drift. Rebase on 030, do not merge. |
> | **031** (group members) | `routing`, `adapter/memory`, `adapter/database/sql`, **`sizing_option_class_gate_test.go`** | **The class gate is shared** (ADR 0033 **D-AL** extends it by hand; ADR 0034 **D-AS** empties its `deferred` arm). Whichever lands second rebases. |
>
> Re-derive with `git log --oneline main..` on each branch before Task 1.

**Goal.** Deliver [Spec 018](../specs/018-byte-cap-ceilings.md): the three `msghttp` byte caps — `WithMaxBodyBytes`,
`WithMaxResponseBytes`, `WithMaxEventBytes` — gain a finite, stated ceiling, so no configuration leaves a
remote-peer-driven read into retained memory unbounded. This **closes the last deferred remedy** in the sizing-option
class that [Spec 016](../specs/016-sizing-option-bounds.md) opened.

**Architecture.** [ADR 0034](../adrs/0034-byte-cap-ceilings.md) — **D-AM** (same class as the nine, different
remedy; the class is not split), **D-AN** (a representability ceiling; **no off-state**), **D-AO**
(`byteCapCeiling = math.MaxInt32`, one shared constant), **D-AP** (`checkRangeInt64` sibling — not a generic,
never `int(n)`), **D-AQ** (reuse the three sentinels, genericise their messages, `[lo, hi]` render at both arms),
**D-AR** (the sixth read is bounded not restructured; the 32-bit guard is a compile arm, an accepted gap),
**D-AS** (empty the gate's `deferred` arm; keep the name as a tombstone), **D-AT** (ship Spec 016 §3.8 item 2's
undelivered godoc; amend every artifact recording the old arm).

**Predecessors this builds on, not re-argues.** [Spec 016](../specs/016-sizing-option-bounds.md) /
[ADR 0032](../adrs/0032-sizing-option-bounds.md) / [Plan 029](029-sizing-option-bounds.md): **D-W** (a stated
ceiling), **D-X** (sentinel reuse + the wrap shape), **D-AB** (the membership criterion), and the shipped
`checkRange` helper and class gate. [ADR 0031](../adrs/0031-nil-option-elements.md) **D-R** (per-package helper
duplication) is D-AP's precedent. [ADR 0029](../adrs/0029-eip-lexical-alignment.md) **D-M** (a constructor return
is not `Permanent`-wrapped).

**Tech stack.** Go 1.25 (`GOTOOLCHAIN=go1.25.13`). Touches **one** module — root, packages `msghttp`
(`adapter/http`) and `msgin` (the root class gate). The delivery gate is still all **eight**. **No Docker
needed.** No new dependency, in any module.

**Traceability.** Implements Spec 018; decided by ADR 0034. Every commit carries `Spec: 018`, `Plan: 032`,
`ADR: 0034` trailers. Branch: `fix/byte-cap-ceilings`, off `main`.

---

## Global constraints

1. **Start every task from `cc-skills-golang:golang-how-to`**, plus the secondary `golang-*` skills it routes to
   (header note). **TDD via `superpowers:test-driven-development`** — failing test first, always. **`gopls` for
   navigation and refactoring**, not text search. **`table-test` / `use-mockgen` / `use-testcontainers` override**
   samber's testing guidance wherever they conflict. This is restated per-task in each task's first step; it is
   **not** delegated to an SDD dispatch prompt.
2. **Blackbox tests only** — `package msghttp_test`, `package msgin_test`, exercising the exported API. No
   whitebox fallback. A test that seems to need an unexported helper is rewritten through the public surface.
   `byteCapCeiling` is unexported, so tests spell the literal `2147483647` (or `math.MaxInt32`) — **not** the
   constant.
3. **Assert-closure tables** — every case carries `assert func(t *testing.T, …)`; never `want`/`wantErr` fields.
   `t.Context()`, never `context.Background()`.
4. **The error shape is the shipped `checkRange` render, unchanged:**
   `fmt.Errorf("%w: %s: %d not in [%d, %d]", sentinel, site, n, lo, hi)`. **No `msgin.Permanent` wrap** — these
   are constructor returns (ADR 0029 D-M / ADR 0032 D-X).
5. **No new exported symbol, of any kind** (D-AN(b), D-AO, D-AP). `apidiff` must report **0 removals / 0
   additions**, and the AST exported-symbol set diff (Plan 030 Global constraint 1's shape, probed in
   `adapter/http` **not** root) must be empty. A task that appears to need an exported symbol has hit a design
   fault: **stop and escalate.**
6. **No test reads more than 1 MiB.** Ceiling values are exercised by **`NewConfig` only** (Spec 018 §6 AC-1).
   A 2 GiB read peaks at ~4 GiB through `io.ReadAll`'s doubling, in a package whose sibling runs
   `goleak.VerifyTestMain`.
7. **Mutation-prove every new assertion** with a mutant that targets **that** assertion (the project's standing
   rule: *a killed mutant is the evidence, not a green run*). Each task carries a mutant table; record the killed
   mutant per case in the task's Evidence block. **A case that survives its own mutant is rewritten.**
   **Exception, stated: the width mutant of Task 1 (M1-7) is NOT behaviorally killable on darwin/arm64** — it is
   caught by the 386 compile arm and by review. ADR 0034 **D-AR(b)** accepts this gap; do not fake a kill.
8. **Each task is a green unit** — `GOWORK=off go test ./... -race -shuffle=on` passes in the root module before
   its commit. No WIP or broken-build commits.
9. **Per-task commit is pre-authorized** by CLAUDE.md's plan-execution exception, once this plan is approved
   **and** an execution mode is chosen. `git push`, merges, tags and branch deletion still need explicit
   per-action approval.
10. **Line numbers in this plan are anchors, not addresses.** Plan 030 is editing `adapter/http/options.go` and
    `adapter/http/helpers.go` concurrently. **Re-derive every offset with `gopls` before editing**; match on the
    function name, the sentinel name and the predicate shape.
11. **Docs links are relative to the CITING file's directory.** A bare `[0034](0034-byte-cap-ceilings.md)` from
    inside `docs/plans/` silently 404s. The pre-merge link gate (CLAUDE.md, **both arms**) is a Task 4 blocker.

---

## The three knobs — one table, read it before Task 1

| Knob | Option | `NewConfig` gate | Sentinel | Default | Accumulates in |
|---|---|---|---|---|---|
| body | `WithMaxBodyBytes(n int64)` `options.go:463` | `options.go:1189-1193` | `ErrInvalidMaxBodyBytes` `errors.go:19` | `1<<20` `options.go:23` | `io.ReadAll` `[]byte` `encode.go:102` |
| response | `WithMaxResponseBytes(n int64)` `options.go:767` | `options.go:1201-1205` | `ErrInvalidMaxResponseBytes` `errors.go:77` | `1<<20` `options.go:30` | the **retained reply payload** `exchange.go:130-131` |
| event | `WithMaxEventBytes(n int64)` `options.go:856` | `options.go:1211-1215` | `ErrInvalidMaxEventBytes` `errors.go:138` | `1<<20` `options.go:44` | `dataBuf` `sse.go:387` / line `buf` `sse.go:472` |

**All three gates are Spec 016's R1-a shape** — `if !set { default } else if <bad> { return nil, sentinel }`.
**None is R2**, so this increment touches no latch and has no ADR 0031 D-U interaction.

---

## Task 1 — `byteCapCeiling`, `checkRangeInt64`, and the three upper arms

**Files:** `adapter/http/options.go`, `adapter/http/helpers.go`, `adapter/http/errors.go`,
`adapter/http/options_test.go` (new cases; blackbox `package msghttp_test`).
**Module:** root.

- [ ] **Step 1.** Load `cc-skills-golang:golang-how-to` (→ `golang-safety`, `golang-security`,
      `golang-error-handling`, `golang-design-patterns`, `golang-documentation`) and the `table-test` override.
      With **`gopls`, not `grep`**, read `adapter/http/helpers.go`'s `checkRange`, the three `NewConfig` gates,
      and the three sentinel declarations, and confirm they still read as "The three knobs" table records them.
      **Re-run the test-safety check before touching the sentinel strings:**
      `grep -rn 'max body bytes must be\|max response bytes must be\|max event bytes must be' --include='*.go' .`
      — every hit must be a declaration in `errors.go`. **Any hit in a `_test.go` is a stop-and-reassess.**
- [ ] **Step 2 (RED).** Write the failing cases of the two tables below. All must fail before any production edit.
- [ ] **Step 3 (GREEN).** Add, in this order:
      1. `const byteCapCeiling int64 = math.MaxInt32` in `adapter/http/options.go`, beside the three
         `defaultMax*Bytes` (import `math`).
      2. `func checkRangeInt64(sentinel error, site string, n, lo, hi int64) error` in
         `adapter/http/helpers.go`, beside `checkRange`, rendering the **identical** shape.
      3. The three `NewConfig` gates rewritten to
         `else if err := checkRangeInt64(<sentinel>, "msghttp.With<X>", cfg.<field>, 1, byteCapCeiling); err != nil { return nil, err }`.
      4. The three sentinel messages: `must be > 0` → `out of range` (D-AQ).
- [ ] **Step 4 (386).** `GOTOOLCHAIN=go1.25.13 GOARCH=386 GOOS=linux go test -gcflags=all=-e -run=NONE ./...`
      compiles clean. **Verified clean on this tree before any edit** — it must stay clean. Record the output.
- [ ] **Step 5.** Mutation-prove every case (table below). `GOWORK=off go test ./... -race -shuffle=on` green.
      Coverage on `adapter/http` ≥ 85% and every branch below covered.
- [ ] **Step 6.** Commit: `fix(http): bound the three byte caps at the representability ceiling`.

**Hot-path branches introduced, and the case that covers each** (CLAUDE.md's test-coverage gate — a branch with
no test is a delivery blocker). All nine `NewConfig` branches are typed-error or default-assignment branches on
the construction hot path:

| # | Branch | Covering case | Killing mutant |
|---|---|---|---|
| B1-1 | `checkRangeInt64`: `n >= lo && n <= hi` → `nil` | `NewConfig_accepts_the_ceiling` (all three knobs, `2147483647`) | make it always return an error ⇒ all three fail |
| B1-2 | `checkRangeInt64`: `n < lo` → error | `NewConfig_rejects_zero` (all three) | change `lo` to `0` ⇒ all three fail |
| B1-3 | `checkRangeInt64`: `n > hi` → error | `NewConfig_rejects_ceiling_plus_one` (all three, `2147483648`) | change `hi` to `math.MaxInt64` ⇒ all three fail |
| B1-4 | body gate, `!set` → default | `NewConfig_default_body_cap_is_1MiB` (unset ⇒ a 2 MiB body is rejected by `DecodeRequest`) | delete the default assignment ⇒ the cap reads `0`, the case fails |
| B1-5 | body gate, set + in range | `NewConfig_accepts_the_ceiling/body` **and** `NewConfig_accepts_one` | see B1-1 |
| B1-6 | body gate, set + out of range | `NewConfig_rejects_ceiling_plus_one/body`, `…/rejects_zero/body`, `…/rejects_1<<62/body` | delete the `else if` arm ⇒ all three fail |
| B1-7 | response gate, all three arms | the `…/response` twins of B1-4/5/6 | as above, per arm |
| B1-8 | event gate, all three arms | the `…/event` twins of B1-4/5/6 | as above, per arm |
| B1-9 | **the `1<<62` case specifically** | `NewConfig_rejects_the_gate_value` (all three) — the exact literal the class gate uses | keep the `<= 0` check and drop the upper arm ⇒ fails. **This is the case Task 3's gate row depends on** |

> **B1-4/B1-7/B1-8's default arms are pre-existing branches**, not new ones. They are listed because the
> `else if` rewrite sits inside the same `if/else if` and a botched edit can swallow the `!set` arm — a class of
> failure the existing suite would catch only for whichever knob happens to be covered. Cover all three.

**Mutant that CANNOT be behaviorally killed — record, do not fake** (Global constraint 7, ADR 0034 D-AR(b)):

| # | Mutant | Why it survives | What catches it |
|---|---|---|---|
| M1-7 | replace `checkRangeInt64(s, site, n, 1, byteCapCeiling)` with `checkRange(s, site, int(n), 1, int(byteCapCeiling))` | On `darwin/arm64` `int` is 64-bit, so `int(n)` is lossless and **every case still passes**. On `GOARCH=386` it truncates and **accepts `1<<62`** — but the 386 binaries compile and cannot execute on this host (`exec format error`). | Step 4's compile arm (the mutant still compiles, so this is **not** a kill), `checkRangeInt64`'s godoc naming the truncation, and code review. **Accepted gap.** |

**Also asserted (Spec 018 §6 AC-2 — the render is true at BOTH ends).** Per knob, two `EqualError` assertions:

```
msghttp: max body bytes out of range: msghttp.WithMaxBodyBytes: 0 not in [1, 2147483647]
msghttp: max body bytes out of range: msghttp.WithMaxBodyBytes: 2147483648 not in [1, 2147483647]
```

and the `WithMaxResponseBytes` / `WithMaxEventBytes` twins. **The lower-end assertion is mandatory** — it is what
would catch a `lo` regression and what makes D-AQ's message change enforced rather than promised.

**"Its product is usable"** (Spec 016 §6's definition for a `NewConfig`-only key): each accepting case asserts
`NewConfig` returns a non-nil `*Config` **and a nil error**, and that the knob's effect is observable — for
`WithMaxBodyBytes` via `DecodeRequest` on a small body, for `WithMaxResponseBytes` via an `httptest` round-trip,
for `WithMaxEventBytes` via `NewSSEParser` + `Next` on a small event. **Do not assert an accessor** — the
accessors are unexported.

**Evidence block to record:** the RED failures (count and names), the killed mutants (B1-1 … B1-9), M1-7's
stated non-kill with the 386 compile output, `go test -cover` for `adapter/http` before and after.

---

## Task 2 — the godoc, including Spec 016's undelivered promise

**Files:** `adapter/http/options.go` (three option godocs + the new constant's), `adapter/http/helpers.go`
(`checkRangeInt64`'s godoc), `adapter/http/errors.go` (three sentinel godocs).
**Module:** root.

> **This task exists because [Spec 016 §3.8](../specs/016-sizing-option-bounds.md) item 2 promised a hazard
> disclosure that Plan 029 never scheduled and never shipped** (Spec 018 §2.1). Verify that before writing:
> `grep -n 'hazard disclosure' docs/plans/029-sizing-option-bounds.md` → no hits, and none of the three godocs
> carries it. Their bounded siblings do (`options.go:901` `WithMaxConnections`, `:976` `WithReplayBuffer`) —
> **copy that shape.**

- [ ] **Step 1.** Load the skills (Global constraint 1) + `golang-documentation`. Read the three option godocs
      and the two sibling godocs above with `gopls`.
- [ ] **Step 2.** Rewrite each of the three option godocs to state, per Spec 018 §4: **the range**
      (`[1, 2147483647]`), **the ceiling's value and why** (`math.MaxInt32` — the read lands in a single `[]byte`
      whose length is an `int`, so no larger cap can be honoured on a 32-bit build), **the typed error**, and
      **the hazard disclosure** — *"the ceiling is not a safety guarantee; this option is the only bound on a
      read driven by a remote peer, and raising it above the 1 MiB default trades flood protection for payload
      size."* Keep the existing `CAVEAT` paragraphs (headers / the O1 drain) — they are unaffected and still true.
- [ ] **Step 3.** Rewrite the `n MUST be > 0` paragraph in each — it is **narrowed, not contradicted**, and must
      not survive verbatim. Keep the *"leaving this option unset is how a caller asks for the default"* sentence:
      it is still true and is the closest thing to an off-state the API has (**D-AN(b)** — there is no other).
- [ ] **Step 4.** Godoc `byteCapCeiling` (shaped like `maxConnectionsCeiling`'s) carrying Spec 018 §3.2's
      argument: representability, **one constant for three knobs and why**, and why it is not a payload guess.
      Godoc `checkRangeInt64` (shaped like `checkRange`'s) naming the **32-bit truncation it exists to prevent**
      and stating it is a sibling rather than a generic (**D-AP**), with the ADR 0031 D-R cross-reference the
      neighbouring helpers already carry.
- [ ] **Step 5.** Update the three sentinel godocs in `errors.go` — each says *"returned by `NewConfig` when an
      explicit … `n <= 0`"* and becomes false the moment Task 1 lands. **This is the CLAUDE.md stored-lesson
      check** (*"all three fix rounds in Plan 028 were godoc, not logic"*): read every prose sentence **against
      the constructor**, not for plausibility.
- [ ] **Step 6.** Sweep for any other sentence made false: `grep -rn 'must be > 0' adapter/http/` and read each
      hit against Task 1's code. `gofmt -l .` clean, `go vet ./...` clean, `golangci-lint run ./...` clean.
- [ ] **Step 7.** Commit: `docs(http): state the byte-cap ceilings and the remote-read hazard on the godoc`.

**Hot-path branches introduced:** **none** — this task changes comments only. Its verification is therefore
**not** a mutant table but a **falsification sweep**:

| # | Claim to falsify | How |
|---|---|---|
| D2-1 | *"no other godoc sentence became false"* | `grep -rn 'must be > 0' adapter/http/` — every hit read against Task 1's code, each either updated or justified in the Evidence block |
| D2-2 | *"the runnable examples still compile and their `// Output:` still holds"* | `go test -run '^Example' ./adapter/http/...` |
| D2-3 | *"the disclosure is actually present"* | `grep -c 'not a safety guarantee' adapter/http/options.go` → **3** |

**Evidence block to record:** the `grep` outputs for D2-1 and D2-3, the Example run, and the before/after text of
one option godoc in full (so a reviewer can judge the shape without diffing three).

---

## Task 3 — move the class gate's three rows out of `deferred`, and empty the arm

**Files:** `sizing_option_class_gate_test.go` (repo root; blackbox `package msgin_test`).
**Module:** root.

> 🔴 **This is the task a bundle fails.** [Spec 016](../specs/016-sizing-option-bounds.md) revision 6 opened with
> **BLOCKER-2: a reclassification that "stopped ONE FILE SHORT"** — seven twins survived, including *the plan task
> that writes the gate*. **A knob whose class changes without its gate row moving is a delivery blocker.**
>
> 🔴 **Rebase check before starting.** [Plan 031](031-group-member-bounds.md) (ADR 0033 **D-AL**) extends this
> same file by hand. If 031 has landed, rebase onto it and re-derive every offset below.

- [ ] **Step 1.** Load the skills (Global constraint 1) + `table-test`. Read `sizing_option_class_gate_test.go`
      end to end — **all of it**, not the three rows. The seven edit sites below are spread across the file.
- [ ] **Step 2 (RED).** Run `GOWORK=off go test -run TestSizingOptionClass ./... -race`. **It must already be red**
      after Task 1 — the three `deferred` rows assert `require.NoError` on `NewConfig(WithX(1<<62))`, which Task 1
      makes an error. **Confirm exactly three failures and no others.** If it is green, Task 1 did not land.
- [ ] **Step 3 (GREEN).** Apply all seven edits of the table below. **The gate must be green after, with no row
      deleted and no assertion weakened.**
- [ ] **Step 4.** `GOWORK=off go test ./... -race -shuffle=on` green in root. Re-run the 386 compile arm — the
      rewritten rows still spell `1 << 62`, which fits `int64`; confirm no new overflow.
- [ ] **Step 5.** Mutation-prove (table below).
- [ ] **Step 6.** Commit: `test(core): move the three byte caps from the gate's deferred arm to fixed`.

**The seven edits** (Spec 018 §6 AC-4; ADR 0034 **D-AS**). **Re-derive every offset — Plan 030/031 may have moved
them:**

| # | Site (approx.) | Change |
|---|---|---|
| 1 | the three rows' `arm` field, `:519`, `:528`, `:537` | `"deferred"` → `"fixed"`; drop the trailing `// class member, remedy deferred — Spec 016 §3.8` comment |
| 2 | the three rows' `assert` closures | `require.NoError` → `require.ErrorIs(t, err, msghttp.ErrInvalidMax*Bytes)` **plus** `assert.EqualError` on the §3.1 render (the same string Task 1's AC-2 case asserts) |
| 3 | `wantArms`, `:726-728` | the three entries `"deferred"` → `"fixed"` |
| 4 | the `byArm` count assertion, `:747` | `{"fixed": 9, "rejects": 1, "deferred": 3, "safe": 6}` → **`{"fixed": 12, "rejects": 1, "safe": 6}`** |
| 5 | the file header comment, `:35` | the `- "deferred" (3) — accepts 1<<62, annotated so it never reads as a…` bullet |
| 6 | the `arm` field's doc comment, `:362` | the four-arm vocabulary string — **keep `"deferred"` with a tombstone**: *(no members as of Plan 032 — see Spec 018)* |
| 7 | the 🔴 block above the deferred rows, `:500-518` | rewrite from *instructions* into a *record*: the ceiling landed in Plan 032, the rows moved, this is what that looked like |

> 🔴 **Edit 4 is the trap.** `byArm` is built by **counting**, so an emptied arm has **no key at all**.
> `{"fixed": 12, "rejects": 1, "deferred": 0, "safe": 6}` **fails** — a counting map never produces a zero entry.
> The `deferred` key must be **removed**, not zeroed.
>
> 🔴 **Edit 7 is the one a mechanical row-move misses.** That block currently tells a future contributor
> *"WHEN §3.8's CEILING LANDS, THIS GATE WILL GO RED, AND THAT IS CORRECT … the repair is to MOVE the row into
> the `fixed` arm. Do NOT weaken the production check."* **This increment IS that event.** Leaving it tells the
> next reader to wait for something already done. Rewrite it; do not delete the warning about weakening the
> check — generalise it.

**Unchanged, and assert that they are:** `require.Len(t, tests, 19)` (17 AST rows + 2 manual), the
`sizingConformanceKeys` slice, and half 1 (the AST completeness walk — the three functions still exist with the
same `int64` parameters).

**Hot-path branches introduced:** none — this is test data. **The mutants target the gate's own assertions:**

| # | Mutant | Must fail |
|---|---|---|
| M3-1 | relabel one moved row back to `"deferred"` | `wantArms` diff **and** `byArm` (two independent failures — that redundancy is the point) |
| M3-2 | **pairwise swap**: relabel `msghttp.WithMaxBodyBytes` → `"safe"` and `endpoint.WithPollMaxBatch` → `"fixed"` | `wantArms` diff **only** — `byArm` stays green. **This is exactly why the key→arm map exists** (the gate's own Task 8 review finding M-1); prove it still holds after the edit |
| M3-3 | leave edit 4 as `"deferred": 0` | the `byArm` assertion — proves the trap is real, not folklore |
| M3-4 | revert Task 1's upper arm for `WithMaxBodyBytes` only | that row's `require.ErrorIs` — proves edit 2's assertion is not vacuous |
| M3-5 | delete one moved row entirely | `require.Len(t, tests, 19)` **and** the `astKeys` completeness diff |

**Evidence block to record:** Step 2's three-and-only-three RED failures by name; the five killed mutants; the
post-edit `byArm` and `wantArms` values pasted verbatim.

---

## Task 4 — fold back into the parents, close the backlog item, run the gates

**Files:** [`../specs/016-sizing-option-bounds.md`](../specs/016-sizing-option-bounds.md) — note the `../specs/`
prefix; a bare `016-…md` from inside `docs/plans/` is the 404 Global constraint 11 is about;
[`../adrs/0032-sizing-option-bounds.md`](../adrs/0032-sizing-option-bounds.md);
[`../HANDOVER.md`](../HANDOVER.md); this plan; [Spec 018](../specs/018-byte-cap-ceilings.md);
[ADR 0034](../adrs/0034-byte-cap-ceilings.md).
**Module:** none (docs).

> **ADR 0034 D-AT(b).** Spec 016 revision 6's BLOCKER-2 was a reclassification that reached one file and missed
> seven. **The cross-file grep guard finds them in one command — Spec 016's own words are that it "was written
> and not run."** Run it, and paste the output into the Evidence block.

- [ ] **Step 1.** Find every site recording the old arm:

      ```bash
      grep -rn 'deferred' docs/specs/016-sizing-option-bounds.md docs/adrs/0032-sizing-option-bounds.md \
        docs/plans/029-sizing-option-bounds.md docs/HANDOVER.md
      grep -rn 'WithMaxBodyBytes\|WithMaxEventBytes\|WithMaxResponseBytes' docs/ CLAUDE.md
      ```

      **Enumerate every hit before editing any of them**, and classify each as *(a) must change*,
      *(b) immutable audit record — leave*, or *(c) historical prose, still true*.
- [ ] **Step 2.** Amend the **(a)** sites:
      - **Spec 016** §2.1 census line (`9 fixed + 3 deferred + 4 safe`), §2.1's arm table, the three verdict rows,
        §3.8 (now *"deferred → delivered by Spec 018"*), §6 AC-5's arm table and its "accepts, deferred" row.
      - **ADR 0032** **D-AB**'s deferral paragraph and the status header's census line.
      - **`docs/HANDOVER.md`** §7 item 6 → **CLOSED**, citing this bundle.
      - Add a **Superseded/finished-by** pointer from Spec 016 §3.8 and ADR 0032 D-AB to Spec 018 / ADR 0034.
- [ ] **Step 3.** 🔴 **Do NOT edit** `docs/plans/029-audit-round-*.md` or any `*-derivation-findings.md` — they are
      **immutable execution records** and correctly record what was true when written (`docs/HANDOVER.md` §8).
      Plan 029 itself is a *delivered* plan and **may** be corrected in place if it states the deferral as a
      standing fact; prefer a dated one-line note over a rewrite.
- [ ] **Step 4.** Flip Spec 018 / ADR 0034 status headers from **PROPOSED** to **ACCEPTED** with the date, and add
      the *"as delivered"* addenda for anything the implementation taught (the amend-don't-pile-on rule: fold
      these into the Task-4 commit, do not add a follow-up `docs:`).
- [ ] **Step 5 (LINK GATE).** Run **both arms** of CLAUDE.md's docs-link gate over every tracked `.md`. Known
      false positives on this tree are exactly two — `docs/plans/m` and `docs/specs/factory(fireTime`, both Go
      identifiers in wrapped code spans. **Anything else is a blocker.**
- [ ] **Step 6 (VACUITY PROBE).** Prove the gate is not vacuous **on the new files, not on root** (the Plan 028
      `apidiff` blindness came from probing only root): plant one bad relative link and one bad `#anchor` in
      `docs/specs/018-byte-cap-ceilings.md`, re-run both arms, confirm **exactly one new hit each**, revert, and
      confirm both vanish.
- [ ] **Step 7 (WHOLE-BRANCH GATE).** `/code-review` and `/security-review` over `main..HEAD` — **not** the last
      commit. Resolve or explicitly triage every finding with a written rationale. Then the full **Library quality
      gates**, all eight steps, over all eight modules:

      ```bash
      for d in . expr adapter/database/sql/{harness,postgres,mysql,sqlite,dbtest} adapter/cron/crontest; do
        (cd "$d" && GOWORK=off go test ./... -race -shuffle=on) || echo "FAILED: $d"
      done
      ```

      plus, per touched module (root): `go vet`, `gofmt -l .`, `CGO_ENABLED=0 go build ./...`,
      `go mod tidy` + `git diff --exit-code -- go.mod go.sum`, `govulncheck ./...`, `golangci-lint run ./...`.
      `harness` has no test files — check it with `go vet` instead. `govulncheck` lives in
      `$(go env GOPATH)/bin`, not on `PATH`.
- [ ] **Step 8 (SURFACE).** Prove Global constraint 5: `apidiff` **0/0**, and the AST exported-symbol set diff
      empty, **probed in `adapter/http`** — plant an exported symbol there, confirm the diff reports it, revert.
- [ ] **Step 9.** Commit: `docs: record the byte-cap ceiling and close the sizing-option class`.

**Hot-path branches introduced:** none. **Verification is the gate output**, pasted verbatim.

**Evidence block to record:** Step 1's full grep output with each hit classified (a)/(b)/(c); both link-gate arms
before and after the probe; the eight-module loop; `apidiff` 0/0 and the AST set diff with its probe; the
`/code-review` and `/security-review` findings with their resolutions.

---

## Delivery checklist

- [ ] All four tasks committed, each a green unit, each carrying `Spec: 018` / `Plan: 032` / `ADR: 0034` trailers.
- [ ] `sizing_option_class_gate_test.go` has **no `deferred` row**, `byArm` has **no `deferred` key**, and
      `wantArms` maps all three byte caps to `"fixed"`.
- [ ] Spec 016, ADR 0032 and `docs/HANDOVER.md` §7 item 6 all record the class as **closed**, with no site left
      asserting a deferral (Step 1's grep, re-run, is empty of (a)-class hits).
- [ ] `apidiff` **0 removals / 0 additions**; no new exported symbol in any module.
- [ ] Both link-gate arms clean but for the two known false positives, and **vacuity-probed on the new files**.
- [ ] Eight-module loop green; the six non-test quality-gate steps run per touched module.
- [ ] Every finding from `/code-review` and `/security-review` over `main..HEAD` fixed or triaged in writing.
- [ ] **Then, and only then, ask** before merging or pushing. `git push`, merge, tag and branch deletion each need
      explicit per-action approval. Delete `fix/byte-cap-ceilings` after it merges.
