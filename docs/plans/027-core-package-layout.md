# Plan 027 — Core package layout, channel segregation, and behavior types

> ## Status: **REGENERATED 2026-07-28 from a green tree — ROUND-3 AUDIT: `NEEDS-REVISION` 3/3, findings folded in**
>
> **Read [Global Constraint 0](#global-constraints) first.** Round 3 found the *generated* tables perfect and
> **every surviving defect in hand-written prose or in a command that was pasted but never run** — each one a
> number pinned to an intermediate state (one commit, the derivation tree, the root module) and presented as a
> property of the finished branch. Global Constraint 0 states the rule that closes the class; the round-3 code
> fixes are F12 and the document fixes are F13.
>
> The round-2 `⛔ DO NOT EXECUTE` banner is **cleared**. Every blocker it listed is resolved, and each
> resolution is evidenced in [`027-derivation-findings.md`](027-derivation-findings.md) rather than argued:
>
> | Round-2 blocker | Status | Evidence |
> |---|---|---|
> | `endpoint` reads `Message`'s unexported fields (6 errors) | **FIXED** (D-H) | F7 — six sites rewrote over `NewMessage[T](m.Payload(), m.Headers())`; `endpoint` compiles |
> | §3.2's split tables omit declarations | **FIXED** | F11.1 — six splits, **80** declarations, generated, zero unlocated |
> | Task 1 leaves the root test binary red (`collector`, `order`) | **FIXED** | F2, F8.4 — both resolved; `go vet ./...` clean workspace-wide |
> | `poller.go:131` forces `endpoint → resilience` | **FIXED** (D-A) | F11.4 — local `pollErrorBackoff`; zero sibling edges |
> | No task migrates the adapter tree | **FIXED** | Task 7 below; F9 — the requalification pass is 115 code + 39 godoc across 28 files; the **whole window** is `git diff --stat c83dde9~1..HEAD -- adapter/` → 31 files, ±239/−191 (F13). All seven modules green |
> | `apidiff`/`gorelease` not installed | **FIXED** | F11.9 — both in `$(go env GOPATH)/bin`; §0 exports the path |
> | `expr` cannot build under `GOWORK=off` | **FIXED in the plan** | Task 10 now specifies the `require` + `replace` pair |
> | "the ledger" load-bearing 8× and never defined | **FIXED** | §Ledger below defines it: file path, contents, lifecycle |
> | Task 8 is ~9,890 lines | **MOOT** (D-G) | the extraction is done and committed as `c83dde9`; sizing is historical |
>
> **Tasks 0–8 are DONE and GREEN.** Tasks 9–12 remain. §Progress states exactly what is committed, what is
> in the working tree, and what has not been started.
>
> *(History: rounds 1 and 2 each returned `NEEDS-REVISION` from all three auditors, both times on hand-typed
> tables. [Round 1](027-audit-round-1.md) §K dispositions its findings and its six §H decisions stand;
> [round 2](027-audit-round-2.md) §F called for exactly this regeneration and its eight §G.1 decisions
> D-A…D-H stand.)*

> **For agentic workers:** REQUIRED SUB-SKILL: `superpowers:subagent-driven-development` (the project default) or
> `superpowers:executing-plans`. Steps use checkbox (`- [ ]`) syntax.
>
> **Go-skills hard rule (CLAUDE.md, restated because `writing-plans` omits it — see also Global Constraint 8):**
> every task starts from **`cc-skills-golang:golang-how-to`** (here routing to: `golang-refactoring` — this is
> an at-scale behavior-preserving restructure — plus `golang-project-layout`, `golang-gopls`, `golang-naming`,
> `golang-structs-interfaces`, `golang-documentation`, `golang-testing`).
> **`superpowers:test-driven-development`** governs every task (red → green → refactor).
> **`gopls`** is the required tool for Go navigation/diagnostics/rename. Project-local overrides that beat
> samber's testing guidance: **`table-test`** (assert-closure form, `ctx` modifier, `t.Context()`);
> **`use-mockgen`** and **`use-testcontainers`** **do not apply** to any remaining task in this plan (no
> interface needs a generated double, and the Docker-backed runners already exist).
>
> ## Environment — required before any step
>
> ```bash
> export GOTOOLCHAIN=go1.25.12                  # bare `go1.25` is rejected: "a language version but not a toolchain version"
> export PATH="$(go env GOPATH)/bin:$PATH"      # gofumpt, goimports, apidiff, gorelease, gopls, govulncheck ALL live here
> ```
>
> **All six tools are installed and NONE is on the bare `PATH`** (verified 2026-07-28, F11.9). Round-2 §C1's
> *"`apidiff`/`gorelease` are not installed"* is no longer true, and the earlier plan's *"`gofumpt` is not
> installed"* was always false — it was inherited from `docs/HANDOVER.md` and carried forward unverified. The
> durable instruction is the `PATH` export above, not a per-tool availability claim. If a tool is genuinely
> missing: `go install golang.org/x/exp/cmd/apidiff@latest golang.org/x/exp/cmd/gorelease@latest`.
>
> ## How the moves are actually performed
>
> **`gopls` has NO Move refactoring.** `gopls api-json` (v0.23.0) exposes rename-related options only. The real
> mechanics, in order:
>
> 1. `git mv <file> <pkg>/<file>` — preserves history.
> 2. Edit the package clause; add the `msgin` import; qualify every root symbol the file now references.
>    Do this **at the AST level**, not with a regex — a regex version corrupted EIP pattern names inside
>    godoc during the derivation run. An AST rewrite provably cannot touch comments or string literals.
> 3. `"$(go env GOPATH)/bin/goimports" -w <pkg>/` to settle imports. **Never invoke `goimports` bare.**
> 4. **`go vet ./...` is the authoritative reference-finder — not `go build ./...`.** `go build` does not
>    compile test binaries and cannot see the six satellite modules; that is exactly how round-2 §B2 survived.
> 5. `gopls` **rename** is still used where a symbol is renamed in place — it is only *Move* that does not exist.
>
> **Enumerate change sets with `grep`, then confirm with the compiler — never the reverse.** Go caps at 10
> errors per package, so a single build's stderr is a *sample*, not an inventory. During the derivation run
> the compiler named 4 failing adapter packages when the real set was 28 files, and hid `producer.go:423`
> entirely behind the cap (F7, F8.7).
>
> **This plan is deliberately thin:** signatures, names, invariants, branch coverage, commit boundaries — no
> embedded implementations. Write the code TDD-first from the tables in Spec 014.

**Goal.** Ship [Spec 014](../specs/014-core-package-layout.md): restructure the flat core into EIP-chapter
packages with vocabulary and SPI in root (C-full), segregate `MessageChannel`, land the EIP renames and named
behavior types, and move expression support to its own module.

**Architecture.** [ADR 0027](../adrs/0027-core-package-restructure.md) (layout, C-full, clean break,
shared-helper resolution, D-A, D-B), [ADR 0028](../adrs/0028-channel-interface-segregation.md) (channel
interfaces, Pipe vs Channel Adapter, exchange exclusivity, D-F),
[ADR 0029](../adrs/0029-eip-lexical-alignment.md) (renames, behavior types, expr module, D-D, D-E).

**Traceability.** Implements [Spec 014](../specs/014-core-package-layout.md); promoted from
[RFC-0001](../rfcs/0001-core-package-restructure.md) / [RFC-0002](../rfcs/0002-eip-alignment.md) /
[RFC-0003](../rfcs/0003-endpoint-behavior-types.md) (all accepted 2026-07-27); governed by ADRs
[0027](../adrs/0027-core-package-restructure.md), [0028](../adrs/0028-channel-interface-segregation.md),
[0029](../adrs/0029-eip-lexical-alignment.md); amends [ADR 0019](../adrs/0019-runtime-expression-evaluation.md)
and annotates [ADR 0013](../adrs/0013-composition-endpoints.md). Derivation evidence:
[`027-derivation-brief.md`](027-derivation-brief.md) and
[`027-derivation-findings.md`](027-derivation-findings.md) (F0–F13). Tools and the `apidiff` baseline are
**committed in-repo** at [`027-tools/`](027-tools/) and
[`027-root-api-baseline.txt`](027-root-api-baseline.txt) — **no gate may depend on `/tmp`**. Branch:
`claude/repo-structure-refactor-jt79t1`.

**This increment is behavior-preserving by construction, with exactly four decided exceptions**
(Spec 014 §2.1): the channel segregation, `ChannelExchange.Close` cancelling its reply subscription,
`channel.WithSingleSubscriber()` (off by default), and `WithReleaseStrategy`'s retyping. Everywhere else, a
task that finds itself rewriting an assertion has either found a real defect (stop, report it) or is doing
more than the plan says (stop, re-read the task).

**Tech stack.** Go 1.25 (`GOTOOLCHAIN=go1.25.12`), the root module plus a new `expr` module, and the six
existing satellite modules — **eight modules** at the end.

---

## The ledger — what it is, where it lives, who writes it

Under one-fresh-subagent-per-task, a fact discovered in Task N is unavailable to Task N+2 unless it is
written down. "The ledger" is that written handoff. Round-2 §C3 found the word load-bearing in eight places
and defined nowhere; it is defined here.

- **Path:** [`docs/plans/027-derivation-findings.md`](027-derivation-findings.md) — the same file the
  derivation run wrote. **Append only**; never rewrite an earlier finding, correct it in place with a marked
  `CORRECTION` block (F8.3 is the worked example).
- **Section per entry:** `### F<n> — <one-line claim>`, followed by **the command and its pasted output**.
  A number with no command is not a ledger entry.
- **Who writes:** the task that discovers the fact, in the same commit as its code.
- **Who reads:** every later task that depends on it, by section number. This plan cites sections explicitly
  (`F7`, `F9.3`, …) so a fresh subagent can find them without reading the whole file.
- **What is in it today:** the apidiff baseline diff (F11.2), per-package coverage both ways (F10.8, F11.11,
  F12.8, F13), the six declaration-level split tables (F11.1), the test-placement census (F8.2), the
  cross-file test-identifier inventory (F8.4), the adapter change inventory (F9.3, F9.9, F13), the RED
  compiler transcript for the capability test (F10.1), and the round-3 code + doc fixes (F12, F13).
- **What is NOT in it, despite an earlier claim that it was:** the **deleted `*Expr` test cases**. The ledger
  holds two table rows (M-1, M-6) and nothing else; none of the twelve deleted test functions is recorded
  anywhere under `docs/`. Task 10's parity source is **`git show ab233d9:expr_test.go`** and
  **`git show ab233d9:expr.go`**, named there explicitly. *(Round-3 correction: "all present today" was
  false for this item.)*

---

## Global constraints

0. **THE GOVERNING RULE — every pasted command carries its explicit commit range and module scope.**
   Round 3 (3/3 `NEEDS-REVISION`) found that the *generated* tables verified perfectly — all 80 §3.2
   declaration rows, the apidiff partition, the file counts — while **every surviving defect was a number or a
   command pinned to an intermediate state and then presented as a property of the finished branch**: one
   task's commit (`c83dde9`) quoted as the whole window; the derivation working tree quoted as the repository;
   the **root module** quoted as the workspace. Concretely, `git diff --stat -- adapter/` (implicitly
   `HEAD`-relative at the moment it ran) and `go mod tidy` (implicitly root-only) each produced a true number
   about a *different* thing than the sentence around it claimed.

   Therefore:
   - A `git diff`/`git log`/`git show` in a document **must name its range in the command itself** —
     `git diff --stat c83dde9~1..HEAD -- adapter/`, never a bare `git diff --stat -- adapter/`.
   - A per-module fact (`go mod tidy`, `go test`, coverage, `go vet`) **must be shown for every module it
     claims to cover**, in the loop form, never measured on root and generalised.
   - A coverage figure **must name the tree it was taken from** (`ab233d9` = pre-refactor, `c83dde9` =
     post-extraction, `b6ce7bb`, HEAD) *and* the profile mode (`-coverpkg=./...` vs default). "Baseline" is
     not a tree.
   - A "verified"/"clean"/"dropped cleanly" claim with no pasted output is **not a claim**, it is a wish.

   This rule is the reason round 3 failed and the reason rounds 1 and 2 failed before it. Re-derive under it;
   do not re-type a number from a previous round's document.

1. **`go vet ./...` after every single move**, not `go build`, and not only at task end. `go vet` compiles
   test binaries; `go build` does not, and cannot see the satellite modules. An import cycle surfaces
   instantly; found late, it is expensive to unpick.
2. **Blackbox tests only** — every `_test.go` stays `package <pkg>_test` and drives the exported API. Moving a
   test must not tempt anyone into whitebox access to reach a now-unexported helper; if that happens, the
   symbol's placement is wrong, not the test.
3. **Exactly one package-doc file per package — asserted by COUNT, because no tool catches a duplicate.**
   `go vet`, `go build`, `gofmt` and `golangci-lint` were all run against a deliberately planted duplicate
   `// Package` comment and **every one of them passed** (round 3, proven by execution — Spec 014 §3.5).
   `ST1000` is off (`.golangci.yml` sets `linters.default: none`) and checks for a *missing* comment anyway.
   The only assertion that fails is this one:
   ```bash
   for p in . endpoint routing transform channel resilience; do
     n=$(grep -l '^// Package ' $p/*.go 2>/dev/null | wc -l | tr -d ' ')
     [ "$n" = 1 ] || { echo "FAIL $p has $n"; exit 1; }
   done
   ```
   Silent at HEAD. *(The earlier wording — "duplicate `// Package` comments after a merge are a `go vet`
   failure" — was false and appeared in three places.)*
4. **Coverage is measured with `-coverpkg=./...` on BOTH sides.** This is not a preference; a
   default-vs-default comparison across a package split is not like-for-like and **fails CLAUDE.md's 85%
   gate falsely on every extraction task**. Default per-package puts root at 81.8% (was 99.3%) purely because
   blackbox tests moved to sibling packages and coverage is credited where the *test binary* lives.
   `-coverpkg=./...` puts the workspace at **93.3%** against a **93.23%** `-coverpkg` baseline (Spec 014
   §3.4e, F10.8, F11.11).
   > The earlier wording — *"a pure move that loses coverage means tests were dropped"* — actively
   > misdiagnosed this and sent the worker hunting a bug that does not exist (round-2 §A8).
5. **Eight modules, not one tree.** `./...` at root covers 11 packages only. The per-module `GOWORK=off` loop
   is the gate, plus the new `expr` module from Task 10 onward:
   ```bash
   for d in . adapter/database/sql/{harness,postgres,mysql,sqlite,dbtest} adapter/cron/crontest expr; do
     (cd "$d" && GOWORK=off go build ./... >/dev/null 2>&1 && GOWORK=off go vet ./... >/dev/null 2>&1 \
       && echo "GREEN: $d") || echo "RED: $d"
   done
   ```
   **`go test` on `harness` proves nothing** — it has zero test files of its own and reports
   `[no test files]`. Only `go vet` (which compiles its 69 non-test selectors) and `dbtest`'s Docker run
   exercise it. A gate that reads "no test files" as a pass is not a gate (F9.7).
6. **No core package imports another core package.** This is the acyclicity invariant and the C-full check;
   assert it mechanically, not by eye:
   ```bash
   go list -deps . | grep -E 'kartaladev/msgin/(endpoint|routing|transform|channel|resilience)'         # EMPTY

   # The second grep is REQUIRED. `go list -deps` includes its ARGUMENT packages, so the earlier
   # published form printed five lines on a CORRECT tree and six on a broken one — it could neither
   # pass nor distinguish the two. Round-3 defect; corrected in all four places it appeared.
   go list -deps ./endpoint ./routing ./transform ./channel ./resilience \
     | grep 'kartaladev/msgin/' \
     | grep -vE '^github.com/kartaladev/msgin/(endpoint|routing|transform|channel|resilience)$'        # EMPTY
   ```
   **Adapters importing `msgin/resilience` violate nothing** — they are consumers, not peers. The earlier
   blanket instruction *"any proposed edge from root into a subpackage, or between two subpackages, is a
   design error"* was right; the accompanying claim that *nothing* imports `resilience` was not (round-2 §A1,
   decision D-A).
7. **Every task's commit carries traceability trailers** (CLAUDE.md):
   ```
   Spec: 014
   Plan: 027
   ADR: 0027            # plus 0028 and/or 0029 where the task realizes them
   RFC: 0001            # plus 0002/0003 where applicable
   ```
8. **Every task starts from `cc-skills-golang:golang-how-to`**, uses TDD via
   `superpowers:test-driven-development`, uses **`gopls`** for navigation/diagnostics/rename, and obeys the
   project-local **`table-test`** override. `use-mockgen` / `use-testcontainers` do not apply here. *(Restated
   in Global Constraints as well as the header because `superpowers:writing-plans` omits this hard rule and
   relaying it only through an SDD dispatch prompt has failed before.)*
9. **Enumerate with `grep`, confirm with the compiler.** Never derive a change set from compiler stderr — Go
   caps at 10 errors per package (F7).

---

## Progress — what is done, what is in the tree, what remains

| Task | Content | State |
|---|---|---|
| 0 | Baseline, move-list, apidiff baseline | **DONE** — committed at `docs/plans/027-root-api-baseline.txt`, F0 |
| 1 | Delete the `*Expr` constructors, drop `expr-lang`, root `doc.go`, **D-D**, **D-E pulled forward** | **DONE** — F2–F6 |
| 2 | Segregate `MessageChannel`; `SubscribableChannel`; `DirectChannel.Subscription`; **D-F** | **DONE** — commit `b6ce7bb`, F10 |
| 3 | `StreamingSource` → `EventDrivenSource` | **DONE** — commit `b6ce7bb`, F10.4 |
| 3.5 | Export `IsPermanent`/`RetryAfterOf`/`NewID`; delete `RetryPolicy.delayFor` | **DONE** — commit `c83dde9` |
| 4–8 | Extract `routing`, `transform`, `channel`, `resilience`, `endpoint`; place the 44 test files | **DONE** — commit `c83dde9`, F8 |
| 7a | Requalify `adapter/` + the six satellite modules | **DONE** — commit `c83dde9`, F9 |
| — | Round-3 code fixes (7-module `go mod tidy`, ST1008, reliability tests, dead-helper deletion, 5 × `doc.go`, article agreement) | **DONE, UNCOMMITTED** — F12 |
| **9** | Named behavior types + combinators | **PARTIAL** — `CorrelationStrategy`/`ReleaseStrategy` shipped; 4 types + combinators remain |
| **9.5** | Residual cleanups the migration left behind | **PARTIAL** — the dead-helper deletion and the article sweep landed in the round-3 pass; the sentinel **decision**, the two-arm sweep, and the capability-test widening remain |
| **10** | The `expr` provider module | **NOT STARTED** |
| **11** | Package docs + Spec 014 §8/§10 godoc obligations | **PARTIAL** — 11a (`doc.go` × 5) done; 11b/11c not started |
| **12** | `MIGRATION.md`, doc sync, whole-branch gate | **NOT STARTED** |

```
$ git log --oneline -3
0e2dcf0 docs(027): regenerate the bundle from the verified tree; clear all round-2 banners
b6ce7bb refactor(core)!: segregate MessageChannel; add WithSingleSubscriber; rename StreamingSource
c83dde9 refactor(core)!: extract the flat core into endpoint/routing/transform/channel/resilience
```

> **CORRECTED (round 3).** This section said Tasks 2 and 3 were *"DONE, UNCOMMITTED"* and instructed the next
> session that *"committing it is the first action of the resumed plan"*. **They landed in `b6ce7bb`.** The
> status was pinned to the tree as it stood when it was written and never requoted against `git log` — Global
> Constraint 0's shape again, and ADR 0027's status line carried the identical defect. What **is** uncommitted
> is the round-3 fix pass (F12) plus this documentation correction pass (F13); that is what the next session
> commits first, subject to the usual approval.

---

## Tasks 0–8 — COMPLETED, recorded for traceability

Do not re-execute. Recorded here because a plan whose completed tasks are missing cannot be read against a
diff, and because four of them carry findings later tasks depend on.

### Task 0 — baseline · **DONE**

`apidiff -w docs/plans/027-root-api-baseline.txt .` before any change (the file is **committed**, so a `/tmp`
reap or a fresh clone cannot break Task 12's `apidiff` gate); per-module coverage baselines; all seven modules
green at baseline (F0). Root: 32 source + 45 test files; 42 error sentinels (F1); 245 exported / 138
unexported top-level declarations.

### Task 1 — remove the `*Expr` constructors; drop `expr-lang` · **DONE**

Deleted `FilterExpr`, `RouterExpr`, `TransformExpr`, `SplitExpr`, `WithCorrelationExpr`, `WithReleaseExpr`,
`expr.go`, `expr_test.go`, `doc_composition.go`; created root `doc.go` in the same change. Root direct deps
3 → 2 (F6).

> **CORRECTED (round 3).** This task's earlier text said *"ran `go mod tidy` in all seven modules"* and
> *"`expr-lang` dropped cleanly"*. **It only ran in root.** F6 was a root-only measurement stated
> workspace-wide, and the other six `go.mod`s **and** `go.sum`s carried
> `github.com/expr-lang/expr v1.17.8 // indirect` for the rest of the window. CI runs `go mod tidy` +
> `git diff --exit-code` **per module**, so 5 of 6 matrix jobs were red on a purely mechanical omission.
> **The satellites were re-tidied in the round-3 fix pass** and all seven are now `go mod tidy -diff`-clean
> with `expr-lang` gone from every `go.mod` and every `go.sum` — pasted output in F12.1 and Spec 014 §7.
> This is Global Constraint 0's exact failure shape: a true root-module number presented as a workspace fact.

**Four findings this task produced that later tasks depend on:**

- **F2 — a test helper crossed a package boundary and only `go vet` saw it.** `collector`, declared in
  `expr_test.go`, was used 9× by `gateway_test.go`, which goes to `endpoint`. Deleting `expr_test.go` left
  the root test binary RED with `undefined: collector` — invisible to `go build`. Resolution: re-declare it
  in `gateway_test.go`. **Spec 014 §3.4c now lists test-only *identifiers*, not only test *files*.**
- **F3 — D-E is a Task 1 prerequisite, not Task 9 work.** Deleting the `*Expr` constructors removes the only
  driver for **three core aggregator hot-path branches** (H-1 reaper fall-through, H-2/H-3 drain-loop
  release errors), because `WithReleaseExpr` was the sole fallible release strategy. D-E
  (`WithReleaseStrategy(ReleaseStrategy)`) was pulled forward into this task and the three branches rewritten
  over a Go-func `requireQtyRelease(min) msgin.ReleaseStrategy` helper. Coverage preserved.
- **F4 — two fixtures became dead code and no tool reported it.** `mixedTypeAddStore`/`mixedTypeGroup`
  existed only to drive the deleted M-6 case; `unused` is off (`linters.default: none`). Removed by hand.
  `emptyGroupAddStore` **is** still live — the two are not symmetric despite looking it.
- **F5 — D-D confirmed by the compiler.** `expr.go` held **every** writer of `cfg.optErr`; only the two read
  sites remained. The field and its `NewAggregator` guard were deleted: those branches were unreachable, not
  merely untested.

### Task 2 — segregate `MessageChannel` · **DONE (uncommitted)**

ADR 0028. **RED was evidenced correctly**, and the technique matters: all root tests are one `package
msgin_test` binary, so a capability test that fails to *compile* takes the whole binary down and produces no
`FAIL` line. The RED artifact is the compiler transcript (F10.1), and it must come from
`go test -c -o /dev/null .` — **`go vet .` shows only the first of the three failures**, because it stops
after one type-error batch.

Delivered: `MessageChannel` narrowed to `Send`; `SubscribableChannel` added; `Subscription` folded into root
`channel.go` (**D-C**); `DirectChannel.Subscribe` → `(Subscription, error)` with the six-row semantics table
(ADR 0028 §7); call sites narrowed; `NewChannelExchange` stores and `Close`s the reply `Subscription`;
`channel.WithSingleSubscriber()` (**D-F**), off by default.

**Two findings that contradict what this task's earlier draft said:**

- **F10.2 — the call-site census is NINE, not four-of-five and not six-of-seven.** Eight send-only, one
  subscribing, and **two of the eight are in the adapter tree** (`adapter/http/inbound.go:116`,
  `adapter/http/stdlib/inbound.go:33`). Both audits searched only the pattern core. Spec 014 §5.0 now states
  the **scope rule** and the command, with the enumeration as illustration.
- **F10.6 — this task is NOT root-module-only.** It edits two satellite modules:
  `adapter/database/sql/harness/groupstore.go:402,408` (a **non-test** file in a module with zero test files
  of its own) and `adapter/database/sql/postgres/example_sql_groupstore_test.go:54`. ADR 0028 and this task's
  earlier draft both described it as a root-module change.

Five **no-op `Subscribe` stubs** were deleted — `fakeAggChannel`, `failNthChannel`, `idsAggChannel`,
`collector`, `scriptedChannel` each carried a `Subscribe(msgin.MessageHandler) error { return nil }` that
existed only to satisfy the old bundled interface. **The five fakes themselves all survive**, migrated with
their tests into `routing` and `endpoint`.

> *Corrected (round 3).* This line previously said the five **fakes** were "deleted, not migrated". False —
> `git grep 'func (.*) Subscribe(.*msgin.MessageHandler) error' ab233d9 -- '*_test.go'` lists the five stubs,
> the same grep at HEAD returns nothing, and all five `type` declarations are still present
> (`routing/aggregator_test.go:22,157`, `routing/aggregator_settlement_test.go:24`,
> `endpoint/gateway_test.go:19`, `endpoint/exchange_test.go:811`). See F13.

### Task 3 — `StreamingSource` → `EventDrivenSource` · **DONE (uncommitted)**

Renamed 30 occurrences across 12 `.go` files — **ADR 0029 §1's sizing is exactly right** — plus five more in
`CLAUDE.md` (2) and `MESSAGING.md` (3) that the bundle named nowhere. Total **35 / 14 files** (F10.4). The
compiler-invisible site was renamed with them: `errors.go:22`'s `ErrUnsupportedSource` message string.

**The verification step is scoped, and the earlier one was unsatisfiable** (round-2 §C4). *"`grep -rn
'StreamingSource' .` returns nothing outside `MIGRATION.md`"* is false against **129 hits in 29 `.md` files**
under `docs/`, including shipped ADRs 0002/0006/0008/0009/0010/0017/0018/0023 that CLAUDE.md forbids
rewriting. The correct gate:

```bash
grep -rn 'StreamingSource' . --exclude-dir=.git --exclude-dir=docs        # must be empty (exit 1)
```

### Task 3.5 — shared-helper resolution · **DONE** (`c83dde9`)

Exported `IsPermanent`, `RetryAfterOf`, `NewID`. Deleted `RetryPolicy.delayFor` and replaced it with a
package-local `retryDelay` in `endpoint/consumer.go:948`.

> Round-2 §D11 stands corrected in Spec 014 §3.3a: `delayFor` had **three** call sites, not two. And
> *"apidiff shows exactly three additions and zero removals"* could never hold against the Task 0 baseline,
> because Task 1 had already removed six exported `*Expr` constructors. The apidiff expectation is stated
> once, for the whole window, in Spec 014 §4.1 — **95 removals, 5 additions**.

### Tasks 4–8 — the package extractions · **DONE** (`c83dde9`)

`routing` → `transform` → `channel` → `resilience` → `endpoint`, `endpoint` last. Sizing decision **D-G**
(splitting Task 8 into 8a/8b) is moot: the extraction is committed.

**D-A shipped as decided:** `BackoffStrategy` stays in root, `ExponentialBackoff` + `jitter` move to
`resilience`, and the `endpoint → resilience` edge round-2 §A1 found is **removed**, not accepted —
`endpoint` declares its own bounded `pollErrorBackoff` over integer doubling, behavior-identical to
`ExponentialBackoff{Initial: pollInterval, Max: 30s, Mult: 2}.Delay(n-1)` on every arm.

**D-B shipped as decided:** `pubsub_registry.go` is the **sixth** split — `TopicPublisher`/`TopicSubscriber`
to root `spi.go`, the registry to `channel`.

**D-H was forced by the type system and cost nothing structurally** (F7): six field-access sites in
`endpoint` rewrote over `msgin.NewMessage[T](payload, m.Headers())` and `m.Payload()`. **Never `msgin.New[T]`**
— it re-stamps `msgin.message-id`/`msgin.timestamp` and no assertion would catch the regression. Verified
absent: `grep -rn 'msgin\.New\[' endpoint/` → exit 1.

The 44 test files were placed by SUT, **zero splits** (F8.2). Behavior identity was **proved, not asserted**:
211 `Test*`/`Example*` functions before and after with identical name sets, and a normalised per-file diff
showing exactly one intentional difference (the `order` duplication).

### Task 7a — requalify `adapter/` and the six satellite modules · **DONE** (`c83dde9`)

**28 files, 154 occurrences: 115 CODE + 39 COMMENT + 0 STRING**, classified against the AST so the two
rewrite passes are provably exhaustive and non-overlapping (F9.2). No `go.mod` needed an edit — every
satellite already `require`s and `replace`s the root module (F9.6). All seven modules green, `dbtest` and
`crontest` run for real against Docker (F9.7).

> Round-2 §A2's *"the known non-test adapter code changes are exactly two sites"* understated this by two
> orders of magnitude, and **no task existed to do the work**. Spec 014 §3.6 now carries the measured
> inventory.

---

## Task 9 — Named behavior types and combinators · **M** · PARTIAL

**Shipped already** (pulled forward with D-E, Task 1): `routing.CorrelationStrategy`
(`routing/aggregator.go:25`), `routing.ReleaseStrategy` (`:35`), `WithReleaseStrategy(ReleaseStrategy)`
(`:82`), `WithReleaseWhen(func(MessageGroup) bool)` (`:89`).

- [ ] Declare the remaining four types and type their base constructors:
      ```go
      // package routing
      type Predicate[A any]    func(ctx context.Context, m msgin.Message[A]) (bool, error)
      type RouteFunc           func(ctx context.Context, m msgin.Message[any]) (msgin.MessageChannel, error)
      type SplitFunc[A, B any] func(ctx context.Context, m msgin.Message[A]) ([]msgin.Message[B], error)
      // package transform
      type Transformer[A, B any] func(ctx context.Context, m msgin.Message[A]) (msgin.Message[B], error)
      ```
- [ ] Add `Predicate.And` / `Or` / `Not` — **with nil semantics specified, because the naive version panics.**

      > **`p.And(nil)` would panic on caller input**, which CLAUDE.md forbids outright (*"library code … must
      > not `panic` on caller input — return errors instead"*) and which contradicts this package's own
      > settled convention: `routing/filter.go:29` returns `nilFuncStep()` for a nil `pred`, and
      > `NewRouter`'s godoc says *"A nil pick is tolerated at construction and surfaces as `ErrNilFunc` at
      > Handle time (no panic on input)."* A nil receiver is equally reachable — `var p routing.Predicate[T]`
      > is nil, and `p.Or(q)` is legal Go on a nil func value.

      **Decided semantics — degrade to the existing sentinel, never panic:**

      | Expression | Result |
      |---|---|
      | `p.And(nil)` / `nil.And(q)` | a `Predicate[A]` returning `(false, msgin.ErrNilFunc)` |
      | `p.Or(nil)` / `nil.Or(q)` | a `Predicate[A]` returning `(false, msgin.ErrNilFunc)` |
      | `nil.Not()` | a `Predicate[A]` returning `(false, msgin.ErrNilFunc)` |

      **The error is returned at evaluation, not at construction** — combinators are pure and return a
      `Predicate`, not `(Predicate, error)`, so there is nowhere for a construction-time error to go. This is
      exactly `nilFuncStep`'s shape, and it reuses the **existing** `ErrNilFunc` rather than minting a
      sentinel, so a caller's `errors.Is` handling already covers it. **No short-circuit may skip the nil
      check**: `p.Or(nil)` must not silently return `(true, nil)` when `p` is true — a nil operand is a
      programming error and must surface even when the short-circuit would hide it. State this on each
      combinator's godoc.
- [ ] Every type's godoc **names its Spring equivalent** — this is the mitigation that justifies dropping the
      Spring names (ADR 0029 §4), so verify it **per type**, not sampled.
- [ ] Record the expected `apidiff` output as **reviewed, source-compatible** parameter-type changes. Do not
      claim zero output.

**Hot-path branches needing a case each:** `And` short-circuit on false; `And` error propagation from the
left and from the right predicate; `Or` short-circuit on true; `Or` error propagation from each side; `Not`
inverting a true and a false; `Not` **propagating** an error rather than inverting it *(the case a naive
`Not` gets wrong)*; **`And`/`Or` with a nil argument → `ErrNilFunc`**; **`And`/`Or`/`Not` on a nil receiver →
`ErrNilFunc`**; **`Or` with a nil argument when the left side is `true`** — the short-circuit must not hide
the nil (the case a naive short-circuit gets wrong, and the reason the nil check precedes it).

> **Do NOT re-verify "aggregator coverage returns to 100% on `NewAggregator` and `Handle`."** That criterion
> is void: **D-D deleted** the `NewAggregator` guard rather than rescuing it (F5, round-2 §B4), and the three
> `Handle`-side branches were already restored in Task 1 via D-E (F3). `routing` measures **100%** today.

**Verify:** existing tests compile unchanged with bare closures — that is the source-compatibility claim, so
demonstrate it rather than assume it (round-2 §E confirms bare closures still infer against named generic
func types on Go 1.25). `-coverpkg=./...` on both sides.

**Commit:** `feat(routing,transform): name the endpoint behavior types and add combinators`

---

## Task 9.5 — Residual cleanups the migration left behind · **S** · PARTIAL

Each item is invisible to `go build`, `go vet`, `go test`, and `gofmt`. None is cosmetic; each is a delivery
blocker under CLAUDE.md's godoc and dead-code expectations.

### 9.5.0 — DECIDE THIS FIRST: the two orphaned expr sentinels

**This is the task's first action, not its third**, because Task 12's assertion block and Spec 014 §4.1's
apidiff expectation are all **contingent on it**, and Task 10 *consumes* whatever is decided.

- [ ] **Decide:** do `ErrInvalidExpression` (`errors.go:161`) and `ErrExprResultType` (`errors.go:183`) stay in
      root's closed contract, or leave it? Both have **zero users** today and their godoc names constructors
      that no longer exist (F11.7). Leaving two unreferenced sentinels inside a contract this window declares
      *closed* is not a neutral default.

      | Option | Root exported | Root sentinels | `apidiff` removals |
      |---|--:|--:|--:|
      | **A — keep in root** (the `expr` module imports them; one error contract, consistent with ADR 0029 §5's provider-side wrapping) | 101 | 42 | 95 |
      | **B — remove from root** (the `expr` module mints its own, consistent with §3.2's rule that a directly-imported package owns its sentinels) | **99** | **40** | **97** |

      **Recommended default: A.** ADR 0029 §5 keeps the *fail-at-construction* error contract in one place, and
      §3.2's rule cuts the other way only for packages a consumer imports *instead of* root — an `expr` caller
      imports `routing` and `msgin` anyway. But B is defensible and this plan does not pre-empt the user.
- [ ] **Propagate the decision** the same commit: the ledger; Spec 014 §3.2's orphan note and §4.1's
      contingency block; Task 12's assertion numbers; and Task 10's `RouteFunc`, whose **two construction
      validations wrap `ErrInvalidExpression`** — so option B means Task 10 declares the replacement sentinel
      before it can compile.

### 9.5.1 — the rest

- [x] **Delete root's dead `boxMessage` and `nilFuncStep`.** **DONE** in the round-3 pass (F12.4); zero users
      in root and zero in root's tests after every package inlined its own copy, and `.golangci.yml`'s
      `linters.default: none` means `unused` is off, so nothing reported it (F11.6). Confirmed
      surface-neutral: `apidiff` still reports 95/5, because both were unexported.
- [x] **Fix the article-agreement class**, not its instances. **DONE** in the round-3 pass (F12.6), and the
      sweep was wrong in *both* directions before it: `routing`/`transform` are consonant-initial so
      "a routing.X" was always correct, while the real defects were three "an msgin.X" sites. The two-way
      check that is now empty:
      ```bash
      grep -rnE '\b[Aa] endpoint\.|\b[Aa]n (msgin|routing|transform|channel|resilience)\.' --include='*.go' .   # empty
      grep -rn --include='*.go' -E '\[(endpoint|routing|channel|transform|resilience)\.[A-Z]' adapter/          # empty
      ```
- [ ] **Run the two-arm staleness sweep to empty** (Spec 014 §8.1). Arm 1 (moved symbols) currently has **2**
      survivors; arm 2 (deleted symbols — a class arm 1 structurally cannot see) has **7**:
      ```
      ARM 1: codec.go:33, routing/aggregator_test.go:21
      ARM 2: errors.go:156,175,176,177 · routing/splitter.go:52 · routing/aggregator.go:316
             routing/aggregator_test.go:1276
      ```
      Regenerate `docs/plans/027-tools/symmap.tsv` before running arm 1 — it is derived and it went stale by
      one entry (`channel.WithSingleSubscriber`) between `c83dde9` and `b6ce7bb`.
- [ ] **Extend the capability test to ALL EIGHT send-only positions.** `capability_test.go`'s
      `TestSendOnlyCallSitesAcceptEveryChannel` covers **3 targets × 3 sites = 9 subtests** today (filter
      discard, router default, exchange request — `capability_test.go:152,163,174`). Spec 014 §9.4 requires all
      eight, so **five** are missing, not four:

      | # | Missing site | Why it matters |
      |---|---|---|
      | 3 | **`routing.NewRouter`'s `pick` return** (`routing/router.go:29,37`) | **The omission the earlier draft made.** This is the position where a *user-supplied* `pick` returns a durable `QueueChannel` — precisely the widening ADR 0028 exists for, and the only one of the eight where the channel is chosen at **message time** rather than at construction. |
      | 4 | `routing.WithOutputChannel` | a durable `QueueChannel` as the Aggregator **output** — round-2 §A6's point |
      | 5 | `routing.WithExpiredGroupChannel` | same, for the **expired-group** sink |
      | 7 | `msghttp.ServeAsync`'s `target` | an HTTP request parked in a durable queue channel |
      | 8 | `stdlib.NewInbound`'s `target` | same, via the stdlib binding |

      > The earlier draft's own Verify line said *"5 core + 2 HTTP"* two lines after asserting all eight were
      > required — 7 of 8, and the one it dropped was #3. Six of the eight positions are core, two are HTTP.

      The two HTTP sites live in `adapter/http` and `adapter/http/stdlib`, so their cases belong in **those
      packages' tests**, not in root's `capability_test.go`.

**Verify:** the sentinel decision recorded in the ledger with its three downstream numbers propagated; both
sweep arms empty; the capability test covers **3 targets × 6 core sites** in `capability_test.go` **plus 3 × 2
HTTP sites** in the two adapter packages — 24 subtests total, not 9; the eight-module `GOWORK=off` loop green;
`-coverpkg=./...` measured against a **named** tree (Global Constraint 0).

**Commit:** `refactor(core): delete dead root helpers, clear the staleness sweep, widen the capability test`

---

## Task 10 — The `expr` provider module · **M** · NOT STARTED

- [ ] New module `expr` with its own `go.mod` (Go 1.25). **It needs BOTH:**
      ```
      require github.com/kartaladev/msgin v0.0.0
      replace github.com/kartaladev/msgin => ..
      ```
      `git tag | wc -l` → **0**, so without the `replace` the module cannot resolve the root module under
      `GOWORK=off` — which is exactly how CI's `module` job runs it. A `use` line in `go.work` is necessary
      but **not sufficient** (round-2 §C2).
- [ ] **CI: three edits, not one.** `.github/workflows/ci.yml`'s `module` matrix lists six directories and
      the `workspace` job hard-codes a six-directory loop:
      1. add `expr` to the `module` matrix;
      2. add `expr` to the `workspace` job's loop;
      3. **fix the pre-existing gap** — `adapter/cron/crontest` is missing from **both** and has been since
         it was created.
- [ ] Providers returning the Task 9 types. **The shape is NOT uniform, and it is NOT non-generic** — the
      earlier plan got both wrong (round-2 §D2 for the first; round 3, compile-proven, for the second):
      ```go
      func Predicate[A any](s string)      (routing.Predicate[A], error)
      func SplitFunc[A, B any](s string)   (routing.SplitFunc[A, B], error)
      func Transformer[A, B any](s string) (transform.Transformer[A, B], error)
      func Correlation[A any](s string)    (routing.CorrelationStrategy, error)   // ← A, not non-generic
      func Release[A any](s string)        (routing.ReleaseStrategy, error)       // ← A, not non-generic
      func RouteFunc[A any](s string, routes map[string]msgin.MessageChannel) (routing.RouteFunc, error)
                                                                                 // ← A AND an extra param
      ```
      **Why `A` is mandatory on all three, and why dropping it does not compile.** The deleted originals were
      all `[A any]`, and the parameter is load-bearing twice:

      ```
      $ git show ab233d9:expr.go | grep -nE '^func '
       89:func FilterExpr[A any](expression string, opts ...FilterOption) (Step, error)
      115:func RouterExpr[A any](keyExpr string, routes map[string]MessageChannel, opts ...RouterOption) (*Router, error)
      167:func TransformExpr[A, B any](expression string) (Step, error)
      217:func SplitExpr[A, B any](expression string) (Step, error)
      321:func WithCorrelationExpr[A any](expression string) AggregatorOption
      390:func WithReleaseExpr[A any](expression string) AggregatorOption
      ```

      1. **`compile[A]` type-checks `payload.Field` against `A`** (`expr.go:35`). Without `A` the compiler has
         no type to check the expression's `payload` root against, and ADR 0019's *fail-at-construction*
         contract collapses to a runtime error.
      2. **`PayloadOf[A]` IS the M-6 `ErrPayloadType` branch this task mandates** — `expr.go:129,224,284,331`.
         A non-generic provider has nothing to assert the member payload to, so the branch cannot exist, and
         the acceptance bar below ("M-6 non-`A` member → `ErrPayloadType`") is unreachable.

      **`A` is not inferable from a `string` argument**, so callers must instantiate explicitly:
      `expr.Release[Order]("…")`, `expr.Correlation[Order]("…")`, `expr.RouteFunc[Order]("…", routes)`. Say so
      in each provider's godoc; a caller who writes `expr.Release("…")` gets an unhelpful inference error.

      `RouteFunc` additionally carries the **two construction validations** `RouterExpr` had. Do not force it
      into a `(string) → (T, error)` mould it cannot fit.
- [ ] `Release` returns `routing.ReleaseStrategy`, whose `(bool, error)` shape is what lets an evaluation
      failure propagate instead of being swallowed into a permanent `false`. `WithReleaseStrategy(expr.Release(…))`
      now compiles, which is the point of D-E.
- [ ] **Reinstate the deleted `*Expr` test cases against the providers. The parity source of truth is git,
      NOT the ledger.**

      > **CORRECTED (round 3).** This step previously said *"reinstate the Task 1 test cases from the ledger …
      > (all present today)"*, and §Ledger listed *"the deleted `*Expr` test cases Task 10 must reinstate"*
      > among the things *"all present today"*. **They are not there.** The ledger holds exactly **two table
      > rows** (M-1 and M-6, in F3 / ADR 0029 §3b) and **none of the twelve deleted test functions** is
      > recorded anywhere under `docs/`. A worker following the instruction as written would have found
      > nothing and either invented a parity bar or skipped it.

      The twelve functions to reach parity with (`git show ab233d9:expr_test.go | grep -nE '^func (Test|Example)'`):

      ```
       45:TestFilterExpr             141:TestFilterExpr_Concurrent   207:ExampleFilterExpr
      233:ExampleRouterExpr          251:TestTransformExpr           326:ExampleTransformExpr
      348:TestSplitExpr              459:ExampleSplitExpr            483:TestRouterExpr
      589:TestWithCorrelationExpr    676:TestWithReleaseExpr         843:ExampleWithReleaseExpr
      ```

      **Read the sources before writing anything:**
      - `git show ab233d9:expr_test.go` — the 12 functions and every case they carry. This is the bar.
      - `git show ab233d9:expr.go` — the implementations, including `compile`, `compileGroup`, `toGroupEnv`,
        `exprSliceToChildren`, and the two `RouterExpr` construction validations.

      *(`docs/plans/014-expr-endpoints.md` and `docs/plans/018-expr-sugar.md` carry partial skeletons of some
      of these tests. They are a useful cross-check on intent but are **not** the parity bar — they predate
      several revisions of the cases.)*

      **Two cases are `toGroupEnv` guard cases that genuinely belong here** — M-1 (empty group snapshot) and
      M-6 (non-`A` member → `ErrPayloadType`) — while H-1/H-2/H-3 already returned in Task 1 and must **not**
      be re-added (F3).
- [ ] Runtime failures wrap the **source expression text** — the debuggability mitigation ADR 0029 §3 traded
      the interface shape for, so it is a requirement, not a nicety.

**Hot-path branches needing a case each:** invalid expression → typed error at construction; valid
expression, wrong result type; runtime evaluation error carrying the expression text; nil/empty expression
string; `Release`'s runtime error surfacing through `Handle` rather than returning `false`; **`RouteFunc`'s
two construction validations**; `toGroupEnv`'s empty-group and non-`A`-member guards.

**Verify:** ADR 0019's fail-at-construction contract holds — an invalid expression errors at the provider
call, never at first message. All **eight** modules green standalone under `GOWORK=off`.

**Commit:** `feat(expr): expression providers as a separate module`

---

## Task 11 — Package docs AND the unowned godoc obligations · **M** · PARTIAL

> **This task grew in round 3.** Spec 014 **§8's nine godoc bullets** and **§10's four multi-instance godoc
> obligations** were written in the indicative, as though they described the tree, and **had no owning task
> at all** — so nothing in this plan was ever going to produce them. Audited against HEAD, **five of the nine
> and two of the four were unmet.** They are Task 11 checkboxes now, each grep-verifiable.

### 11a — the five subpackage `doc.go` files · **DONE** (uncommitted, F12.5)

All five landed in the round-3 fix pass, closing F11.8. `ST1000` is off (`linters.default: none`), so nothing
would ever have flagged their absence.

- [x] `endpoint/doc.go` — EIP ch.10 *Messaging Endpoints*; Spring `org.springframework.integration.endpoint`.
- [x] `routing/doc.go` — EIP ch.7 *Message Routing*; Spring `…integration.router`.
- [x] `transform/doc.go` — EIP ch.8 *Message Transformation*; Spring `…integration.transformer`.
- [x] `channel/doc.go` — EIP ch.3 (Pipe) and ch.4 (Publish-Subscribe Channel); Spring `…integration.channel`.
- [x] `resilience/doc.go` — **states explicitly that it has no EIP chapter and no Spring counterpart** and
      cites [ADR 0006](../adrs/0006-resilience-flow-control.md) instead (round-2 §D15). Inventing a chapter
      would be exactly the lexical drift this program exists to prevent.
- [x] Root's `doc.go` states the **Pipes-and-Filters** model in Spec 014 §3.5's terms: a `MessageChannel` is
      the **Pipe**, a `Step` is the **filter**, `Chain` assembles the pipeline (`doc.go:5-14`).

### 11b — Spec 014 §8's unmet godoc bullets

Each line pairs the edit with the grep that proves it. **Run each grep before ticking the box.**

- [ ] **§8.1 — name Correlation Identifier.** "Return Address" is present; the *in-process* pattern is never
      named. Add it to `endpoint`'s `ChannelExchange`/`doc.go` prose.
      → `grep -rn -i 'correlation identifier' --include='*.go' .` must be **non-empty**.
- [ ] **§8.3 — the AMQP disclaimer on `RequestReplyExchange`.** Currently **absent workspace-wide**, despite
      Spec 014 §6 and ADR 0029 §2 both asserting in the present tense that it exists.
      → `grep -rn -i 'amqp' --include='*.go' .` must be **non-empty** and must hit `spi.go`.
- [ ] **§8.4 — every named behavior type names its Spring equivalent, per type.** Currently only the *package*
      docs name Spring; `routing.CorrelationStrategy` and `routing.ReleaseStrategy` do not. This is the
      mitigation that justifies dropping the Spring names (ADR 0029 §4), so it is **not** discharged by a
      package-level mention.
      → for each of `CorrelationStrategy`, `ReleaseStrategy`, and Task 9's `Predicate`, `RouteFunc`,
      `SplitFunc`, `Transformer`: `grep -B10 'type <T>' <file> | grep -i spring` must hit.
- [ ] **§8.7 — `msghttp.ServeAsync` and `stdlib.NewInbound` state the widened `target` contract.** Neither
      godoc mentions that any `MessageChannel` — a durable `QueueChannel`, a `PublishSubscribeChannel`, any
      `OutboundAdapter` — now qualifies, which is the whole user-visible payoff of §5.0 rows 7–8.
      → `grep -n -i 'QueueChannel' adapter/http/inbound.go adapter/http/stdlib/inbound.go` must hit both.

### 11c — Spec 014 §10's unmet multi-instance obligations (CLAUDE.md mandatory)

- [ ] **`channel.WithSingleSubscriber` states it is a SINGLE-PROCESS guard.** ADR 0028 §6.2 requires it in so
      many words — *"must not be documented as a distributed exclusivity guarantee"* — and the godoc
      (`channel/pubsub.go:69-83`) never mentions the process boundary. Two instances each holding their own
      `PublishSubscribeChannel` still each accept a subscriber.
      → `grep -n -A22 'func WithSingleSubscriber' channel/pubsub.go | grep -i 'single-process\|per-process\|instance'` must hit.
- [ ] **`RetryPolicy.MaxAttempts` states the per-instance bound.** `retry.go:37-41` says nothing about
      topology, so a caller sizing a poison-message threshold behind a load balancer gets `N ×` what they
      asked for. Say: attempts are tracked per instance (`endpoint/attempts.go:26`), so across N nodes the
      effective global bound is `N × MaxAttempts`; this applies **only** to sources without a native
      delivery-count header (a `NativeReliability` source is unaffected); the distributed answer is the
      broker's own redelivery count or a shared idempotency/dedup store.
      → `grep -n -B12 'MaxAttempts int' retry.go | grep -i 'instance'` must hit.

**Verify:**
```bash
for p in . endpoint routing transform channel resilience; do
  n=$(grep -l '^// Package ' $p/*.go 2>/dev/null | wc -l | tr -d ' ')
  [ "$n" = 1 ] || { echo "FAIL $p has $n"; exit 1; }
done                                   # silent — this is the check, NOT `go vet` (Global Constraint 3)
```
Plus every grep in 11b/11c above, run and pasted into the ledger. `go vet ./...` clean.

**Commit:** `docs(core): package docs and the godoc obligations Spec 014 §8/§10 require`

---

## Task 12 — Migration guide, doc sync, and the whole-branch gate · **M** · NOT STARTED

- [ ] **`MIGRATION.md`**, covering at minimum:
      - every moved symbol old→new — **95 removals reconciled against Spec 014 §4.1's four classes**;
      - the `DirectChannel.Subscribe` signature change;
      - **`WithReleaseStrategy` is both retyped and relocated** (D-E) — say both; `WithRelease` never existed
        and `WithReleaseWhen` is the new sugar;
      - the five new exports (`IsPermanent`, `RetryAfterOf`, `NewID`, `SubscribableChannel`,
        `EventDrivenSource`) plus `channel.WithSingleSubscriber`, which `apidiff` on the root package does
        **not** show because it lives in `channel`;
      - the `*Expr` → `expr` module move;
      - the `EventDrivenSource` rename **including `ErrUnsupportedSource`'s user-visible message string**;
      - **the HTTP inbound widening** — `msghttp.ServeAsync` and `stdlib.NewInbound` now accept any
        `MessageChannel`, so an HTTP request can be parked in a durable `QueueChannel` (Spec 014 §5.0);
      - **`ChannelExchange.Close`'s post-`Close` reply behavior change** (Spec 014 §5.2a);
      - the two-import shape for a retry policy:
        `msgin.RetryPolicy{Backoff: resilience.ExponentialBackoff{…}}`.
- [ ] **CLAUDE.md, same commit** (traceability, non-optional): dependency policy drops `expr-lang` and
      **keeps `robfig`**; the `FilterExpr`/`RouterExpr` mention at `CLAUDE.md:235` goes with it; the
      architecture blueprint's `StreamingSource` becomes `EventDrivenSource`; the package layout and the
      `./...`-is-not-the-repo command block reflect the new tree and the **eighth** module.
- [ ] **`MESSAGING.md`** reconciled against the new package names (it carries 3 `EventDrivenSource` mentions
      and is named nowhere in the bundle — F10.4).
- [ ] **Assert every invariant mechanically**, into the ledger:
      ```bash
      go list -deps . | grep -E 'kartaladev/msgin/(endpoint|routing|transform|channel|resilience)'        # EMPTY
      go list -deps ./endpoint ./routing ./transform ./channel ./resilience \
        | grep 'kartaladev/msgin/' \
        | grep -vE '^github.com/kartaladev/msgin/(endpoint|routing|transform|channel|resilience)$'       # EMPTY
      ls *.go | grep -v _test.go | wc -l                                                                 # 14
      go run docs/plans/027-tools/decls.go . | grep -v _test.go \
        | awk -F'\t' '$5=="exported" && $3!="method"{print $4}' | sort -u | wc -l                        # 101 (see contingency below)
      go run docs/plans/027-tools/decls.go . | grep -v _test.go \
        | awk -F'\t' '$3=="var" && $4 ~ /^Err/{print $1}' | sort | uniq -c                               # 42 errors.go (contingent)
      ```
      > **These two numbers are CONTINGENT on Task 9.5's first decision** (the fate of `ErrInvalidExpression`
      > and `ErrExprResultType`). They are **101 / 42 / apidiff 95** only if both sentinels **stay** in root.
      > If they are removed, the assertions become **99 / 40 / apidiff 97**. Read the decision recorded in the
      > ledger by Task 9.5 before asserting either pair; do not treat the numbers above as fixed.
      Then diff root's exported surface against **Spec 014 §4's closed list** — every symbol accounted for,
      nothing extra.
- [ ] **Re-run the `MessageChannel` scope-rule census** (Spec 014 §5.0) rather than citing a number. Three
      documents have now quoted three different wrong counts; the check is the contract.
- [ ] Run the full per-module `GOWORK=off` loop across **eight** modules, `go vet`, `golangci-lint`,
      `test -z "$(gofmt -l .)"`, `govulncheck`, `go mod tidy` (no-op in every module), and
      `CGO_ENABLED=0 go build ./...`.
- [ ] `apidiff`/`gorelease` against the Task 0 baseline; reconcile **every** entry against Spec 014 §4.1's
      95-removal / 5-addition decomposition. An unexplained entry blocks the merge.
- [ ] Coverage with **`-coverpkg=./...` on both sides**. The two known non-regressions
      (`resilience/breaker.go:176 toHalfOpen` at 87.5%, and `endpoint/consumer.go:467`'s `ctx.Done()` race arm
      covered in 1 of 3 identical runs) are pre-recorded as expected; do not report either as a drop.
- [ ] Whole-branch `/code-review` and `/security-review` over `main..HEAD`; resolve or triage every finding.

**Commit:** `docs: migration guide and doc sync for the core restructure`

---

## Risks

| Risk | Mitigation |
|---|---|
| A count in a document rots and nobody notices | Every normative number in Spec 014 §3–§4 is generated with its command recorded in the ledger; Task 12 re-derives them rather than reading them. The `MessageChannel` census is stated as a **scope rule + command**, because three rounds of correcting the *number* produced three wrong numbers |
| A change is invisible to the compiler | `go vet ./...` (not `go build`) after every move; the two-arm staleness sweep in Task 9.5; `unused` is off, so dead code needs an explicit check |
| Coverage looks like it regressed when it did not | Global Constraint 4: `-coverpkg=./...` on both sides. The two known pre-existing gaps are named in Spec 014 §9.7 so a worker does not chase them |
| A satellite module is left red | The eight-module `GOWORK=off` loop; **`harness` is checked with `go vet`, never `go test`** — it has no test files and `go test` reports a false pass |
| A "pure move" quietly changes behavior | No assertion may change outside Spec 014 §2.1's four exceptions; identity is proved by identical `Test*` name sets plus a normalised per-file diff |
| `apidiff` noise hides a real break | Read against Spec 014 §4.1's decomposition of the 95 removals into four named classes |
| RED cannot be evidenced for a compile-time failure | The transcript comes from `go test -c -o /dev/null .`, not `go vet` (which stops after one type-error batch) |
| `expr` cannot build standalone | Task 10 ships `require` + `replace` together, and CI gets all three edits |
| `gopls` unavailable in a subagent | No task depends on it; `go vet ./...` is the authoritative reference-finder and `grep` the fallback |
| Root loses its package doc | Done in Task 1's change; the five subpackage docs landed in the round-3 pass (F12.5). Task 11's verify **counts** them — `go vet` does NOT catch a duplicate (Global Constraint 3) |
| A godoc obligation has no owning task | Spec 014 §8's nine bullets and §10's four obligations were unowned and six were unmet; **Task 11b/11c owns all thirteen**, each with a grep that must be run and pasted |
| A number is pinned to an intermediate state | **Global Constraint 0** — every pasted command carries its commit range and module scope. This is the signature behind every round-3 defect |
| Expression support absent mid-branch | Bounded to Tasks 1→10 within one branch; Task 1 preserved the test cases in the ledger that Task 10 must satisfy |
| Branch conflicts with in-flight feature work | Run this window **before** Plan 028 (gin) or any new adapter; blast radius grows with every adapter landed first |
