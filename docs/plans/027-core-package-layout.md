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
> | No task migrates the adapter tree | **FIXED** | Task 7 below; F9 — the requalification pass is 115 code + 39 godoc across 28 files; the **whole window** is `git diff --stat c83dde9~1..dadc775 -- adapter/` → 43 files, +244/−220 (F13; re-pinned round 5). All seven modules green |
> | `apidiff`/`gorelease` not installed | **FIXED** | F11.9 — both in `$(go env GOPATH)/bin`; §0 exports the path |
> | `expr` cannot build under `GOWORK=off` | **FIXED in the plan** | Task 10 now specifies the `require` + `replace` pair |
> | "the ledger" load-bearing 8× and never defined | **FIXED** | §Ledger below defines it: file path, contents, lifecycle |
> | Task 8 is ~9,890 lines | **MOOT** (D-G) | the extraction is done and committed as `c83dde9`; sizing is historical |
>
> **Tasks 0–8 are DONE and GREEN.** Tasks 9–12 remain. §Progress states exactly what is committed, what is
> in the working tree, and what has not been started.
>
> ### Both open decisions are CLOSED (2026-07-28) — this plan changed in three places
>
> The round-3 cycle left exactly two questions for the user. Both are now decided, and **neither is
> implemented** — no code has changed since `3d0b87a`:
>
> | | Decision | Where it lands |
> |---|---|---|
> | **D-I** | The two orphaned expr sentinels **leave root**; the `expr` module mints its own | §9.5.0 (decided), Task 10 (`expr/errors.go`), Task 12 (counts) |
> | **D-J** | Reply-channel exclusivity is **probed and rejected by default**, opt-out via `endpoint.WithSharedReplyChannel()` | **new Task 9.6**, [ADR 0030](../adrs/0030-reply-channel-exclusivity-probe.md) |
>
> **This plan's own recommendation on D-I was A (keep in root), and the user chose B.** The recommendation
> rested on a premise that was never measured — §9.5.0 records why it was wrong. That is a fourth instance of
> the round-3 signature (an assertion about the tree, stated without running the command), this time inside a
> *recommendation* rather than a table, which is a place none of the three audit rounds looked.
>
> **A round-4 audit has NOT run against these changes.** Rounds 1–3 each found defects introduced by the
> previous round's fixes; this pass edits the same documents again and adds a new ADR and a new task.
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
[ADR 0029](../adrs/0029-eip-lexical-alignment.md) (renames, behavior types, expr module, D-D, D-E, **D-I**),
[ADR 0030](../adrs/0030-reply-channel-exclusivity-probe.md) (**D-J** — the reply-channel exclusivity probe;
amends ADR 0028 §6.2's default posture, realized by Task 9.6).

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

**This increment is behavior-preserving by construction, with exactly six decided exceptions**
(Spec 014 §2.1): the channel segregation, `ChannelExchange.Close` cancelling its reply subscription,
`channel.WithSingleSubscriber()` (off by default), `WithReleaseStrategy`'s retyping, **the reply-channel
exclusivity probe (D-J, Task 9.6)**, and **deterministic endpoint faults becoming `Permanent` (D-M,
Tasks 9 and 9.7)**. Everywhere else, a task that finds itself rewriting an assertion has either found a real
defect (stop, report it) or is doing more than the plan says (stop, re-read the task).

> **ROUND-6 CORRECTION (E-B6).** This read *"exactly four decided exceptions"* while D-J (Task 9.6) already
> required changing an assertion and D-M now requires changing four shipped producers. An implementer
> executing Task 9.6 literally was instructed by the sentence above to **stop and report it**. The cardinality
> word is swept here and at Task 9.6's round-4 correction and in the Risks table; Spec §2.1 gains rows 5 and 6.

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
   - A `git diff`/`git log`/`git show` in a document **must name its range in the command itself, with BOTH
     ENDS FIXED TO A SHA** — `git diff --stat c83dde9~1..dadc775 -- adapter/`; never a bare
     `git diff --stat -- adapter/`, and **never a range ending in `HEAD`**.

     > **ROUND-5 CORRECTION (MINOR 4) — this constraint used to publish `c83dde9~1..HEAD` as its own
     > example, i.e. the rule mandated the anti-pattern that Spec §3.6/B6 blocks.** A range ending in `HEAD`
     > looks pinned and is not: it silently re-evaluates on every commit, so the figure beside it rots the
     > moment anything lands. That is precisely how the adapter blast radius went stale **three times**
     > (round-2 §A2, round-3 §3.6, round-4 B6) and how it survived the round-4 sweep in two more places
     > (round-5 BLOCKER 3). `HEAD` is a cursor, not a name.
   - A per-module fact (`go mod tidy`, `go test`, coverage, `go vet`) **must be shown for every module it
     claims to cover**, in the loop form, never measured on root and generalised.
   - A coverage figure **must name the tree it was taken from by SHA** (`ab233d9` = pre-refactor, `c83dde9` =
     post-extraction, `b6ce7bb`, `dadc775`) *and* the profile mode (`-coverpkg=./...` vs default). "Baseline"
     is not a tree, and neither is "HEAD".
   - **A relabel is not a re-measurement.** Replacing "today" or "HEAD" with a SHA converts an unfalsifiable
     claim into a falsifiable one — which is only progress if you then *run it at that SHA*. Round-5
     BLOCKER 1 is this exact failure: a test-file count was re-pinned to `c83dde9` without re-running, where
     the true value is 44, not the 45 that had been true of a later tree.
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
   Silent at `dadc775`. *(The earlier wording — "duplicate `// Package` comments after a merge are a `go vet`
   failure" — was false and appeared in three places.)*
4. **Coverage is measured with `-coverpkg=./...` on BOTH sides.** This is not a preference: a
   default-vs-default comparison across a package split **is not like-for-like**, because credit follows the
   *test binary* and blackbox tests moved to sibling packages, so the two sides describe different things.
   `-coverpkg=./...` puts the workspace at **93.4%** at `dadc775` (Spec 014 §3.4e, which carries the
   per-tree table and the correct `ab233d9` = 93.5% baseline).
   > **ROUND-4 CORRECTION (B2).** This constraint previously justified itself with *"fails CLAUDE.md's 85%
   > gate falsely on every extraction task … puts root at 81.8%"*. **Root reads 95.3% at `dadc775`** —
   > `1d7fc80` deleted the dead root helpers — so the 85%-gate justification is **no longer true** and must
   > not be restated. The rule stands on the not-like-for-like ground alone. The 81.8% figure belongs to
   > `b6ce7bb` and is historical.
   >
   > **Task 9.6 is the exception that proves the rule needs BOTH arms:** its two new `channel` methods are
   > exercised only from `endpoint`, so `-coverpkg` reports them at 100% while the package-local profile
   > shows 0% and `channel` falls to 98.3%. Where a task adds exported symbols to a package whose tests live
   > elsewhere, the **per-package** arm is the only one that can see the gap.
   > The earlier wording — *"a pure move that loses coverage means tests were dropped"* — actively
   > misdiagnosed this and sent the worker hunting a bug that does not exist (round-2 §A8).
5. **Eight modules at the end, SEVEN until Task 10 creates `expr`.** `./...` at root covers 11 packages only.
   The per-module `GOWORK=off` loop is the gate. **Copy this block verbatim — the `expr` entry is commented
   out on purpose** and is uncommented by Task 10, the task that creates the directory:
   ```bash
   for d in . adapter/database/sql/{harness,postgres,mysql,sqlite,dbtest} adapter/cron/crontest \
            ; do                       # ← Task 10 onward, append: expr
     (cd "$d" && GOWORK=off go build ./... >/dev/null 2>&1 && GOWORK=off go vet ./... >/dev/null 2>&1 \
       && echo "GREEN: $d") || echo "RED: $d"
   done
   ```

   > **ROUND-6 CORRECTION (E-M6/E-M3).** This block used to hard-code `expr` in the loop, so a worker on
   > Task 9, 9.5, 9.6 or 9.7 who copied the canonical constraint got a spurious RED for a module that does not
   > exist yet — while those same tasks' own Verify sections say *"the **seven**-module loop, not eight"*.
   > Measured on the untouched tree at `aae6160` with the old block:
   >
   > ```
   > GREEN: .
   > GREEN: adapter/database/sql/harness
   > GREEN: adapter/database/sql/postgres
   > GREEN: adapter/database/sql/mysql
   > GREEN: adapter/database/sql/sqlite
   > GREEN: adapter/database/sql/dbtest
   > GREEN: adapter/cron/crontest
   > (eval):cd:3: no such file or directory: expr
   > RED: expr
   > ```
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
| — | Round-3 code fixes (7-module `go mod tidy`, ST1008, reliability tests, dead-helper deletion, 5 × `doc.go`, article agreement) | **DONE** — committed `1d7fc80` (code) + `3d0b87a` (docs), F12/F13 |
| **9** | Named behavior types + combinators | **PARTIAL** — `CorrelationStrategy`/`ReleaseStrategy` shipped; 4 types + combinators remain |
| **9.5** | Residual cleanups the migration left behind | **PARTIAL** — the dead-helper deletion and the article sweep landed in the round-3 pass; **the sentinel decision is now CLOSED (D-I: they leave root)**, its `errors.go` deletion, the two-arm sweep, and the capability-test widening remain |
| **9.6** | Reply-channel exclusivity probe (**D-J**, ADR 0030) | **NOT STARTED** — added in the D-I/D-J pass (`aae6160`); closes the residual three review lenses converged on |
| **9.7** | Classify the four shipped `ErrNilFunc` producers as `Permanent` (**D-M**) | **NOT STARTED** — added in the round-6 pass; the class Task 9's combinators are the newest members of |
| **10** | The `expr` provider module | **NOT STARTED** |
| **11** | Package docs + Spec 014 §8/§10 godoc obligations | **PARTIAL** — 11a (`doc.go` × 5) done; 11b/11c not started |
| **12** | `MIGRATION.md`, doc sync, whole-branch gate | **NOT STARTED** |

```
$ git log --oneline -3          # re-quoted 2026-07-28, before the D-I/D-J doc pass
dadc775 docs(handover): record the committed state, both review gates, and the two open decisions
3d0b87a docs(027): apply the round-3 audit corrections; commit the derivation tools
1d7fc80 fix(core): restore the goleak net, cover the poll-backoff cap, reject a nil Subscription
```

> **CORRECTED (round 3).** This section said Tasks 2 and 3 were *"DONE, UNCOMMITTED"* and instructed the next
> session that *"committing it is the first action of the resumed plan"*. **They landed in `b6ce7bb`.** The
> status was pinned to the tree as it stood when it was written and never requoted against `git log` — Global
> Constraint 0's shape again, and ADR 0027's status line carried the identical defect.
>
> **RE-QUOTED (D-I/D-J pass, 2026-07-28).** The block above had gone stale a *second* time, in exactly the
> same way: it still showed `0e2dcf0` as HEAD and the row above it still read "DONE, UNCOMMITTED" for the
> round-3 fixes, which landed in `1d7fc80` + `3d0b87a`. **The lesson is that a pasted `git log` is a
> measurement with a timestamp, and every editing pass must re-run it** — Global Constraint 0 applied to this
> document's own status block, which is the one place three rounds have failed to apply it.
>
> **The D-I/D-J documentation pass is COMMITTED as `aae6160`** (`docs(027): close D-I/D-J/D-K, add ADR 0030
> and Task 9.6, apply rounds 4-5`). Its nine files, derived rather than recalled:
>
> ```
> $ git diff --name-only aae6160~1..aae6160
> CLAUDE.md
> docs/HANDOVER.md
> docs/adrs/0027-core-package-restructure.md
> docs/adrs/0028-channel-interface-segregation.md
> docs/adrs/0029-eip-lexical-alignment.md
> docs/adrs/0030-reply-channel-exclusivity-probe.md
> docs/plans/027-core-package-layout.md
> docs/rfcs/0002-eip-alignment.md
> docs/specs/014-core-package-layout.md
> ```
>
> **No code has changed since `3d0b87a`**, which is why every count in §9.5.0 and Task 12 is labelled a
> projection.
>
> > **ROUND-6 CORRECTION (M-B1) — third recurrence, in the block that names itself as the one place three
> > rounds have failed to apply Global Constraint 0.** This paragraph read *"**Uncommitted at this moment:**
> > … this plan, Spec 014, ADR 0028, ADR 0029, **new** ADR 0030, and CLAUDE.md"*. It was `aae6160` and
> > `git status --short` was empty; the hand-typed file list was **also incomplete when it was written**, since
> > that pass modified `docs/adrs/0027-*` and `docs/rfcs/0002-*` as well.
> >
> > **INVARIANT (adopted here): no status block in this plan asserts working-tree state.** A status block names
> > the **commit that carries the change** and derives its file list from `git diff --name-only <sha>~1..<sha>`.
> > "Uncommitted at this moment" is unfalsifiable the instant the moment passes — it is `HEAD` wearing a
> > different hat (Global Constraint 0).

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

### Task 2 — segregate `MessageChannel` · **DONE** (`b6ce7bb`)

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
> the same grep at `dadc775` returns nothing, and all five `type` declarations are still present
> (`routing/aggregator_test.go:22,157`, `routing/aggregator_settlement_test.go:24`,
> `endpoint/gateway_test.go:19`, `endpoint/exchange_test.go:811`). See F13.

### Task 3 — `StreamingSource` → `EventDrivenSource` · **DONE** (`b6ce7bb`)

Renamed `StreamingSource` → `EventDrivenSource` across the workspace. The compiler-invisible site was renamed
with them: `errors.go:22`'s `ErrUnsupportedSource` message string.

> **ROUND-6 CORRECTION (C-M5).** This read *"30 occurrences across 12 `.go` files — **ADR 0029 §1's sizing is
> exactly right** — plus five more in `CLAUDE.md` (2) and `MESSAGING.md` (3) … Total **35 / 14 files**"*.
> **`30/12/35/14` reproduces in no single frame.** Spec §6 already carries the `ROUND-4 CORRECTION`; the plan
> did not. Re-measured, both pins, `.go` first and then `.go` + `CLAUDE.md` + `MESSAGING.md`:
>
> ```
> $ git grep -c 'EventDrivenSource' b6ce7bb -- '*.go' | awk -F: '{n+=$3} END{print n}'                  # 30
> $ git grep -l 'EventDrivenSource' b6ce7bb -- '*.go' | wc -l                                           # 12
> $ git grep -c 'EventDrivenSource' b6ce7bb -- '*.go' CLAUDE.md MESSAGING.md | awk -F: '{n+=$3} END{print n}'  # 30
> $ git grep -l 'EventDrivenSource' b6ce7bb -- '*.go' CLAUDE.md MESSAGING.md | wc -l                    # 12
>
> $ git grep -c 'EventDrivenSource' dadc775 -- '*.go' | awk -F: '{n+=$3} END{print n}'                  # 31
> $ git grep -l 'EventDrivenSource' dadc775 -- '*.go' | wc -l                                           # 13
> $ git grep -c 'EventDrivenSource' dadc775 -- '*.go' CLAUDE.md MESSAGING.md | awk -F: '{n+=$3} END{print n}'  # 36
> $ git grep -l 'EventDrivenSource' dadc775 -- '*.go' CLAUDE.md MESSAGING.md | wc -l                    # 15
> ```
>
> So `b6ce7bb` — the commit this task landed in — is **30/12 and 30/12**: `CLAUDE.md` and `MESSAGING.md`
> carried **zero** mentions at that pin, which makes the *"plus five more"* clause false there, not merely
> imprecise. The `35/14` pair belongs to `0e2dcf0` and `31/13/36/15` to `dadc775`. **Do not restate
> *"ADR 0029 §1's sizing is exactly right"*** — it was a sizing claim pinned to a tree that never existed.

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
> once, for the whole window, in Spec 014 §4.1 — measured **95 removals / 6 additions** at `dadc775`, projected
> **97 / 8** once D-I and D-J land.

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

The 44 test files were placed by SUT, **zero splits** (F8.2). Behavior identity was proved by the
**normalised per-file diff**, which shows exactly one intentional difference (the `order` duplication).

> **ROUND-5 CORRECTION (BLOCKER 2).** This also claimed *"211 `Test*`/`Example*` functions before and after
> with identical name sets"*. **211 is the count at `c83dde9` only** — it is 224 at `c83dde9~1` and 221 unique
> at `dadc775` — and the sets are not identical, because `c83dde9` carried Task 1's `*Expr` deletion (16 names
> out, 3 in) alongside the extraction. Spec 014 §2 carries the measurement and withdraws the argument.

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

      **Decided semantics (AMENDED by D-M, round 6) — degrade to the existing sentinel, classified
      `Permanent`, wrapped with its position, never panic:**

      | Expression | Result |
      |---|---|
      | `p.And(nil)` / `nil.And(q)` | a `Predicate[A]` returning `(false, msgin.Permanent(msgin.ErrNilFunc))`, wrapped with position |
      | `p.Or(nil)` / `nil.Or(q)` | a `Predicate[A]` returning `(false, msgin.Permanent(msgin.ErrNilFunc))`, wrapped with position |
      | `nil.Not()` | a `Predicate[A]` returning `(false, msgin.Permanent(msgin.ErrNilFunc))`, wrapped with position |

      **`msgin.Permanent` is not decoration — it is the retry classification (D-M).** `IsPermanent` is a
      **closed enumeration** and a bare `ErrNilFunc` is not in it, so without the wrap the most deterministic
      fault the library can produce consumes the whole retry budget, lands in the **dead-letter** sink rather
      than the **invalid-message** sink, and records an unhealthy signal that trips the circuit breaker
      (`endpoint/consumer.go:614`, `:733`). Measured at `aae6160` (root code byte-identical to `dadc775`), via
      a throwaway root `_test` file:

      ```
      IsPermanent(msgin: nil endpoint function                 ) = false
      IsPermanent(msgin: no route for message                  ) = false
      IsPermanent(msgin: payload is not of the expected type   ) = true
      IsPermanent(msgin: message has no correlation key        ) = false
      ```

      The in-tree precedent carries the identical rationale in its own godoc —
      `routing/aggregator.go:151-160` wraps `ErrNoCorrelation` in `msgin.Permanent` *"so the message would be
      retried to the dead-letter sink instead of diverted to the invalid-message channel"*. **`ErrNoRoute`
      stays transient and is NOT wrapped**: `routing/router.go:48-56`'s `pick` is caller-supplied and
      evaluated per message, so a message unroutable now may be routable after a config reload.

      **Wrap with the position, because the bare sentinel collapses six nil sites into one string.**
      `msgin: nil endpoint function` says nothing about `And` vs `Or` vs `Not`, receiver vs argument, or which
      link of `p.And(q).Or(r)` failed — and CLAUDE.md requires *"typed, wrapping errors that name the
      offending field/input"*. The shape:

      ```go
      fmt.Errorf("%w: routing.Predicate.And: nil argument", msgin.Permanent(msgin.ErrNilFunc))
      ```

      Each combinator's godoc states that **`errors.Is(err, msgin.ErrNilFunc)` still matches** and that
      `msgin.IsPermanent(err)` is true.

      **The error is returned at evaluation, not at construction** — combinators are pure and return a
      `Predicate`, not `(Predicate, error)`, so there is nowhere for a construction-time error to go. This is
      `nilFuncStep`'s shape (as amended by Task 9.7), and it reuses the **existing** `ErrNilFunc` rather than
      minting a sentinel, so a caller's `errors.Is` handling already covers it. **No short-circuit may skip
      the nil check**: `p.Or(nil)` must not silently return `(true, nil)` when `p` is true — a nil operand is
      a programming error and must surface even when the short-circuit would hide it. State this on each
      combinator's godoc.
- [ ] Every type's godoc **names its Spring equivalent** — this is the mitigation that justifies dropping the
      Spring names (ADR 0029 §4), so verify it **per type**, not sampled.
- [ ] **Note for Task 12 (E-M8): this task changes the expected shape of Spec §5.0's census.** Measured on the
      untouched tree, `grep -rn "msgin\.MessageChannel\|MessageChannel interface" --include="*.go" . | grep -v
      "_test.go" | grep -v "^./docs" | grep -v '// '` → **16 lines**, of which **two** are
      `routing/router.go:29` (the `Router.pick` **field**) and `:37` (the `NewRouter` **parameter**). Retyping
      the parameter to `RouteFunc` drops `:37` → **15**; retyping the field as well drops `:29` too → **14**.
      Decide which you are doing, record it in the ledger with the re-run command, and say so here — Task 12
      re-measures this census and, without this note, would report a correct new number as a regression.
- [ ] **`apidiff`: snapshot `./routing` and `./transform` BEFORE the edit, then diff.** The committed baseline
      at `docs/plans/027-root-api-baseline.txt` is **root-only** (`apidiff -w … .`; its header line is
      `github.com/kartaladev/msgin` and `grep -c 'routing\|transform'` over it → exit 1), and every symbol this
      task retypes lives in `routing`/`transform` and is already inside the window's 95 root removals. **Root
      `apidiff` for Task 9 is zero, legitimately** — so a checkbox reading *"do not claim zero output"* against
      the root baseline could only be discharged by inventing output.
      ```bash
      apidiff -w /tmp/task9-routing.api ./routing && apidiff -w /tmp/task9-transform.api ./transform   # BEFORE
      # …edit…
      apidiff /tmp/task9-routing.api ./routing ; apidiff /tmp/task9-transform.api ./transform          # AFTER
      ```
      Record the result as **reviewed, source-compatible** parameter-type changes. *(These two snapshots are
      task-local scratch, not a gate input — the no-gate-may-depend-on-`/tmp` rule binds the **committed**
      baseline, which is unchanged.)* The compile-only demonstration in **Verify** below is the actual
      source-compatibility proof; this checkbox exists to enumerate the surface delta, not to prove it safe.
      *(Round-6 E-M2.)*

**Hot-path branches needing a case each:** `And` short-circuit on false; `And` error propagation from the
left and from the right predicate; `Or` short-circuit on true; `Or` error propagation from each side; `Not`
inverting a true and a false; `Not` **propagating** an error rather than inverting it *(the case a naive
`Not` gets wrong)*; **`And`/`Or` with a nil argument → `ErrNilFunc`**; **`And`/`Or`/`Not` on a nil receiver →
`ErrNilFunc`**; **`Or` with a nil argument when the left side is `true`** — the short-circuit must not hide
the nil (the case a naive short-circuit gets wrong, and the reason the nil check precedes it); **and its
mirror, `And` with a nil argument when the left side is `false`** — a naive `And` short-circuits on `false`
and never sees the nil, which is the identical trap. *(Round-4 exec-M5: only the `Or` half was enumerated,
so the `And` half had no covering case under CLAUDE.md's hard gate.)*
**NEW in round 6 (D-M / D-B4):** a case asserting **`msgin.IsPermanent(err) == true`** on a combinator's nil
result, and — in the same case — that **`errors.Is(err, msgin.ErrNilFunc)` still matches** through both the
`msgin.Permanent` wrap and the positional `fmt.Errorf`. The classification *is* the behavior change; an
`errors.Is`-only assertion cannot see it, which is exactly how the bare sentinel survived unnoticed in the
four shipped producers Task 9.7 now fixes.

> **Do NOT re-verify "aggregator coverage returns to 100% on `NewAggregator` and `Handle`."** That criterion
> is void: **D-D deleted** the `NewAggregator` guard rather than rescuing it (F5, round-2 §B4), and the three
> `Handle`-side branches were already restored in Task 1 via D-E (F3). `routing` measures **100%** today.

**Verify:** existing tests compile unchanged with bare closures — that is the source-compatibility claim, so
demonstrate it rather than assume it (round-2 §E confirms bare closures still infer against named generic
func types on Go 1.25). `-coverpkg=./...` on both sides.

**Commit:** `feat(routing,transform): name the endpoint behavior types and add combinators`

---

## Task 9.5 — Residual cleanups the migration left behind · **M** · PARTIAL

Each item is invisible to `go build`, `go vet`, `go test`, and `gofmt`. None is cosmetic; each is a delivery
blocker under CLAUDE.md's godoc and dead-code expectations.

> **RE-SIZED S → M in round 6 (E-M7).** The **S** label counted the sentinel deletion and the sweep and missed
> the capability-test widening's scaffolding, which is not a copy of the existing three sites: **capability
> row 4** (`routing.WithOutputChannel`) needs a `MessageGroupStore` plus correlation/release strategies to
> build an Aggregator at all, and **row 5** (`routing.WithExpiredGroupChannel`) additionally needs
> `WithGroupTimeout`, a `clockwork` fake, a `go agg.Run(ctx)`, a tick advance and a goleak-clean teardown —
> **and the expired-group path returns no error**, so a case written on the `assert(err)` shape of rows 1–3 is
> vacuous and must assert the *delivery* instead. Two more sites live in two other modules
> (`adapter/http`, `adapter/http/stdlib`), each needing its own server fixture. Round 4 had already noted the
> HTTP pair; these two are additional.
>
> **Re-sized rather than split into 9.5a/9.5b — deliberate.** The two workstreams (D-I's deletion + sweep, and
> the capability widening) *are* independently green, so a split is defensible on its own terms. It is
> rejected here because the task **number** is load-bearing outside this file — measured, not assumed:
> `CLAUDE.md:236` (*"Plan 027 Task 9.5 deletes them"*), `docs/adrs/0029-eip-lexical-alignment.md:256`
> (*"Plan 027 Task 9.5 deletes them; Task 10 declares the **one** replacement"*) and
> `docs/specs/014-core-package-layout.md:1898` (*"Plan §9.5.1"*) all cite it by number. A renumber is therefore
> a coordinated three-document traceability edit, not a plan-local one. A size label is a scheduling signal; a
> task number is a link. If a future round wants the split, do it as one cross-document change, not here.

### 9.5.0 — DECIDED (D-I, 2026-07-28): the two orphaned expr sentinels LEAVE root

> **This was the blocking decision; it is closed. Option B was chosen.** The user was shown both options with
> their numeric consequences and the repo-precedent evidence, and chose B — the `expr` module mints its own.
>
> **The plan's recommended default was A, and the recommendation was based on an unverified premise.** It
> argued that §3.2's own rule "cuts the other way only for packages a consumer imports *instead of* root."
> Measured against the tree, the rule is **symmetric and both arms are already in use**: the three shipped
> adapters mint 51 sentinels of their own **and** return root's at 27 distinct file→sentinel pairs
> (`ErrNilAdapter`, `ErrInvalidCapacity`, `ErrReplyTimeout`, …). The discriminator is not *who imports what*
> but **whose fault it is**: an invalid expression is the provider's fault, and root — after Task 1 — has no
> code that can produce one. Spec 014 §3.2 carries the commands.

- [x] **Decided: B.** `ErrInvalidExpression` (`errors.go:168`) and `ErrExprResultType` (`errors.go:193`) are
      **deleted from root**; the `expr` module declares **`expr.ErrInvalidExpression` only**, with the
      `msgin/expr:` prefix (Spec 014 §7). **Not aliased** — an alias would keep the dead names in the closed
      contract and would have to reference the root vars this decision removes.

      > **REVISED D-K (round 6).** The `expr` module declares **one** sentinel, not two: a result-type
      > mismatch is returned as `fmt.Errorf("%w: expr result %T is not %T", msgin.ErrPayloadType, got, want)`,
      > reusing root's existing sentinel. **Root's numbers below are unchanged** — root loses both either way —
      > but the *`expr`-module* sentinel count is **1, not 2**, and Task 10 no longer wraps in
      > `msgin.Permanent` (`ErrPayloadType` is already inside `IsPermanent`). See Task 10.

      | | Root exported | Root sentinels | `apidiff` removals | `apidiff` additions |
      |---|--:|--:|--:|--:|
      | Measured at `dadc775` | 102 | 43 | 95 | 6 |
      | After **D-I** (this decision) | 100 | 41 | 97 | 6 |
      | After **D-J** (§9.6) too | **102** | **42** | **97** | **8** |

      The HEAD row is **measured** (`decls.go` and `apidiff`, both re-run 2026-07-28); the other two are
      **projections**. Task 12 re-runs the commands and treats their output as the truth.
- [ ] **Deletion is CODE, and it belongs to this task's commit** — remove both `var` blocks from `errors.go`
      (`ErrInvalidExpression` at `:180`, `ErrExprResultType` at `:206`, each with the godoc block above it,
      starting at `:168` and `:193` respectively). **Copy `ErrInvalidExpression`'s godoc (`:168-180`) out to
      Task 10 first** — it is the only surviving statement of the construction-vs-evaluation split, and under
      **revised D-K** it is the only one of the two that Task 10 re-declares. `ErrExprResultType`'s godoc
      (`:193-206`) is not carried forward: the `expr` module returns `msgin.ErrPayloadType` instead of minting
      a replacement, so there is no declaration for that comment to sit above.

      > **ROUND-4 CORRECTION (B3).** This bullet previously claimed the godoc being deleted *"is where 3 of
      > arm 2's 7 staleness survivors live (`errors.go:175,176,177`)"* and told the implementer those three
      > *"must disappear without a separate edit"*. **False on both counts**: arm 2 has no survivors in
      > `errors.go`, and lines 175–177 contain no matched token — they are ordinary sentinel godoc. An
      > implementer would delete the blocks, re-run the sweep, observe no delta, and reasonably conclude the
      > gate was broken. The deletion is still correct; only the justification was invented.
- [ ] **Propagate in the same commit:** the ledger; Spec 014 §3.2 / §4 / §4.1 / §7 (**done in the doc pass**);
      Task 10's provider set and its `RouteFunc`, whose **two construction validations wrap
      `ErrInvalidExpression`** and must now wrap the module's own; Task 12's assertion numbers.

### 9.5.1 — the rest

- [x] **Delete root's dead `boxMessage` and `nilFuncStep`.** **DONE** in the round-3 pass (F12.4); zero users
      in root and zero in root's tests after every package inlined its own copy, and `.golangci.yml`'s
      `linters.default: none` means `unused` is off, so nothing reported it (F11.6). Confirmed
      surface-neutral: `apidiff` still reports the same **95 removals**, because both were unexported. (The
      additions arm moved 5 → 6 in that same commit, for the unrelated `ErrNilSubscription`.)
- [x] **Fix the article-agreement class**, not its instances. **DONE** in the round-3 pass (F12.6), and the
      sweep was wrong in *both* directions before it: `routing`/`transform` are consonant-initial so
      "a routing.X" was always correct, while the real defects were three "an msgin.X" sites. The two-way
      check that is now empty:
      ```bash
      grep -rnE '\b[Aa] endpoint\.|\b[Aa]n (msgin|routing|transform|channel|resilience)\.' --include='*.go' .   # empty
      grep -rn --include='*.go' -E '\[(endpoint|routing|channel|transform|resilience)\.[A-Z]' adapter/          # empty
      ```
- [ ] **Run the two-arm staleness sweep to empty** (Spec 014 §8.1, **arm 2 redesigned in round 4**).
      Measured at `dadc775`, by running both arms:
      ```
      ARM 1 (moved symbols still qualified msgin.X) — 2 survivors:
            codec.go:33, routing/aggregator_test.go:21
      ARM 2 (names in comments that are declared nowhere) — 1 survivor:
            routing/aggregator.go:316   // "the WithRelease strategy failed"  → WithReleaseStrategy
      ```
      Regenerate `docs/plans/027-tools/symmap.tsv` before running arm 1 — it is derived and it went stale by
      one entry (`channel.WithSingleSubscriber`) between `c83dde9` and `b6ce7bb`.

      > **ROUND-4 CORRECTION (B4 / exec-B1).** This checkbox previously published **"arm 2 has 7 survivors"**
      > at seven named lines. **Arm 2's published command returns zero hits and always did** — it was a
      > hardcoded list of the six `*Expr` names, none of which survives anywhere. All seven named lines hold
      > unrelated live text. Spec §8.1 now defines arm 2 as an **invariant** (every name a comment mentions is
      > a name that exists) rather than a list of deleted names, which is what surfaces `WithRelease` — a name
      > that never existed at all, and therefore one that **no deleted-symbol enumeration could ever contain**.
      > Run the command; do not trust this list either.
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
HTTP sites** in the two adapter packages — 24 subtests total, not 9; the **seven**-module `GOWORK=off` loop
green (not eight — `expr` does not exist until Task 10);
`-coverpkg=./...` measured against a **named** tree (Global Constraint 0).

**Commit:** `refactor(core)!: move the expr sentinels out of root, clear the staleness sweep, widen the capability test`

> **ROUND-6 CORRECTION (E-M4) — two defects in one subject line.** It read
> `refactor(core): delete dead root helpers, clear the staleness sweep, widen the capability test`.
> (1) The **dead-helper deletion is already `[x] DONE`** in the round-3 pass (§9.5.1, F12.4, committed in
> `1d7fc80`), so the subject named work this commit does not contain and omitted the work it does — D-I's
> removal of `ErrInvalidExpression` and `ErrExprResultType` from root. (2) It carried **no `!`** while
> removing two exported symbols from the closed root contract (`apidiff` 95 → 97 removals, §9.5.0's table),
> where the *smaller* break in Task 9.6 is correctly typed `feat(core,channel,endpoint)!`. Conventional
> Commits' `!` is the machine-readable breaking-change marker and this is a breaking change.

---

## Task 9.6 — Reply-channel exclusivity probe (decision D-J) · **S** · NOT STARTED

> **NEW in this pass.** Realizes [ADR 0030](../adrs/0030-reply-channel-exclusivity-probe.md) and
> [Spec 014 §5.1](../specs/014-core-package-layout.md), which amend ADR 0028 §6.2's default posture. Three
> review lenses converged on the residual this closes; none of them individually blocked, so it was carried
> as an open decision rather than a finding. It is now decided.
>
> **Read ADR 0030 before writing anything** — the four rejected alternatives are the design, and two of them
> (require the interface; assert the concrete type) look cheaper than the chosen shape until you read why they
> were rejected.

**Skills:** start from `cc-skills-golang:golang-how-to`; TDD via `superpowers:test-driven-development`;
`gopls` for navigation; `table-test` for the branch table; blackbox `_test` packages only.

- [ ] **Root — `channel.go`:** add `ExclusiveSubscribable` (embedding `SubscribableChannel`, one method
      `SingleSubscriber() bool`). Godoc must state it is a report about **this channel in this process**, not
      a distributed guarantee, and cross-reference `channel.WithSingleSubscriber`.
- [ ] **Root — `errors.go`:** add `ErrSharedReplyChannel`. Godoc names both remedies
      (`channel.WithSingleSubscriber()` on the channel, or `endpoint.WithSharedReplyChannel()` on the
      exchange) and states the consequence being prevented — a full copy of every reply reaching another
      subscriber. **Do NOT reuse `ErrChannelSubscribed`**: it would report "already subscribed" for a channel
      that has no subscriber (ADR 0030 Consequences).
- [ ] **`channel` — two methods:** `(*DirectChannel).SingleSubscriber() → true`;
      `(*PublishSubscribeChannel).SingleSubscriber() → c.cfg.single`. Add the compile-time assertions
      (`var _ msgin.ExclusiveSubscribable = (*DirectChannel)(nil)`, same for pub-sub) next to the existing
      `_ msgin.SubscribableChannel` ones at `direct.go:29` / `pubsub.go:112`.
- [ ] **`channel`-package tests for both methods — REQUIRED, and not covered by the four-arm table.** A
      `table-test` in `package channel_test` asserting `DirectChannel.SingleSubscriber() == true`, and
      `PublishSubscribeChannel.SingleSubscriber()` **both** with `WithSingleSubscriber()` and without.

      > **ROUND-6 CORRECTION (E-M1).** This checkbox also demanded *"the `NewPubSub(WithSingleSubscriber())`
      > propagation path through `withConfig`"*. **That path is already covered** —
      > `TestPubSub_SingleSubscriberPropagatesToTopics` (`channel/pubsub_test.go:178-192`) constructs
      > `channel.NewPubSub(channel.WithSingleSubscriber())` and asserts the second `Subscribe` returns
      > `ErrChannelSubscribed`. And the *novel* reading this task would add — asking a `PubSub` about
      > `SingleSubscriber` — **is not expressible blackbox**: `PubSub`'s entire exported method set is
      > `Publish` / `Subscribe` / `TopicCount` (`channel/pubsub_registry.go:36,49,80` — verified, nothing
      > else), and ADR 0030 `:98` says so itself. Do not add a `PubSub` case; do not reach for whitebox access
      > to invent one.

      **Why this is a separate obligation.** All four arms of the probe table live in `endpoint`, so without
      this test *nothing in `channel` exercises either method* — and the equality
      `PublishSubscribeChannel.SingleSubscriber() == cfg.single` is the entire load-bearing link between D-F
      and D-J, currently asserted only transitively, from another package, through a constructor. The
      round-4 design audit implemented this task and measured `channel` falling **100.0% → 98.3%** with both
      methods at 0.0%, while `-coverpkg=./...` reported them at 100%. **`channel` returning to 100.0% on the
      per-package profile is the acceptance signal.**
- [ ] **`endpoint` — the guard and the opt-out:** `WithSharedReplyChannel()` sets `cfg.allowShared`; the probe
      runs in `NewChannelExchange` **before `reply.Subscribe`**, so a rejected exchange leaves no subscription
      behind. Order relative to the existing `ErrNilChannel` and `ErrInvalidReplyTimeout` checks: after both
      (a nil channel cannot be probed).
- [ ] **Rewrite `NewChannelExchange`'s reply godoc.** It currently says exclusivity "is documented rather than
      enforced here" (`endpoint/exchange.go:216`) — that sentence becomes false. **State FOUR outcomes, and
      let Task 11b own the final wording:** rejected when the channel reports non-exclusive; accepted when it
      reports exclusive; **accepted when the channel does not implement the probe at all** (the one a reader
      will otherwise assume away); and **accepted but exclusive only *within this process*** — a channel whose
      deliveries reach other processes must report `false` (D-L), and a truthful local `true` still carries no
      cross-instance guarantee.

      > **ROUND-6 CORRECTION (D-M1).** This said *"State the **three** arms"*, while ADR 0030 `:230-233` and
      > Spec `:1691` both require **four** outcomes and Spec AC-9 `:1881` says *"all three acceptance
      > outcomes"* — i.e. three **accept** arms plus the reject arm. An implementer writing three would then
      > have Task 11b's §8.12 gate rewrite the comment they just wrote. **Authority: Spec §8 obligation 12
      > (four). Final wording: Task 11b.** The four-arm truth table below is the *guard's* branch table, which
      > is a different four — the godoc's fourth outcome is a scope caveat on an accepted arm, not a fifth
      > branch.

      > **NOTE — the option godoc (§8.13) and the guard order (D-M2).** Task 11b's §8.13 requires
      > `WithSharedReplyChannel`'s godoc to say it **suppresses the probe**. That is only true if
      > `cfg.allowShared` is tested **first**: write
      > `if !cfg.allowShared { if ex, ok := reply.(msgin.ExclusiveSubscribable); ok && !ex.SingleSubscriber() { … } }`,
      > so the opt-out never calls a third-party `SingleSubscriber` at all. The reverse order (probe first,
      > then consult the flag) suppresses the *rejection* while still paying for the probe — and would make the
      > §8.13 godoc false. *(Round-6 D-M2; ADR 0030 and Spec §5.1 are being corrected to match.)*

**Hot-path branches — four arms, a truth table, one case each** (fold into one `table-test`):

| probe implemented | `SingleSubscriber()` | `WithSharedReplyChannel()` | result |
|---|---|---|---|
| no | — | — | accepted |
| yes | `true` | — | accepted |
| yes | `false` | no | **`ErrSharedReplyChannel`** |
| yes | `false` | yes | accepted |

The "no" row needs a **test-local `SubscribableChannel` that deliberately omits the method**, because no
in-tree type can drive it to an **accepted** outcome. Both production implementations
(`channel/direct.go:29`, `channel/pubsub.go:112` — the only two, verified) will implement the probe; the one
existing in-tree type that *omits* it, `nilSubChannel` (`endpoint/exchange_test.go:120`), returns
`(nil, nil)` from `Subscribe` and is therefore rejected by the `ErrNilSubscription` guard 20 lines later, so
it exercises the probe-absent branch but never reaches the accepted state the arm is asserting. Without a new
fake the arm is unreachable and the CLAUDE.md coverage gate fails.

> **ROUND-4 CORRECTION (exec-M12).** This read *"no in-tree type can produce it"*, full stop — false, since
> `nilSubChannel` is exactly such a type. The conclusion (a new fake is needed) survives; the reason had to
> narrow to "no in-tree type can produce an **accepted** probe-absent arm".

- [ ] **A FIFTH row, and a SECOND fake: the rejected arm leaves no subscription behind.** Spec AC-9 requires
      *"a case asserting the channel has no subscriber after a rejected construction"* — the property that
      makes "the probe runs **before** `reply.Subscribe`" observable rather than merely intended. It is **not
      expressible over the in-tree types**: the rejected arm needs `*channel.PublishSubscribeChannel` (the only
      non-exclusive in-tree channel), whose subscriber count is not public — `isEmpty()` is unexported
      (declared `channel/pubsub.go:158`) and a `Send` that reaches zero subscribers returns `nil` by
      documented design (`channel/pubsub.go:172-173`, *"may fan out to zero subscribers and return nil
      (delivered-to-none)"*), so neither a count nor a send can distinguish "no subscriber" from "one".
      Add a second test-local fake — `countingSharedChannel`: `SingleSubscriber() → false`, `Subscribe`
      increments a counter — and a **fifth table row** asserting the counter is **0** after the construction
      returns `ErrSharedReplyChannel`. This row is an ordering assertion, not a truth-table arm; keep it in the
      same `table-test` with an `assert` closure that reads the counter.

      > *(Round-6 E-B7/D-M5. The finding is partitioned to Spec 014, whose file cannot carry a plan checkbox;
      > recorded here so the acceptance criterion has an owning task. If Group B instead resolves AC-9 by
      > weakening it, delete this checkbox.)*

- [ ] **Update the test's own PROSE, not just its constructions — neither sweep arm can see prose of this
      shape.** `TestChannelExchange_sharedPubSubReplyChannel`'s doc comment and case names assert the
      pre-D-J world in three places, all verified present at `aae6160`:

      ```
      $ grep -n 'a legal program\|the second exchange is built\|NOT rejected\|turns that into a typed error' \
          endpoint/exchange_test.go
      410:// pub-sub reply channel" a legal program, and every reply then fans out to BOTH
      412:// channel.WithSingleSubscriber is the opt-in that turns that into a typed error.
      420:			name: "default fan-out: the second exchange is built and sees the first's reply",
      422:				require.NoError(t, secondErr, "sharing a plain pub-sub reply channel is NOT rejected")
      ```

      Under D-J *"a legal program"* is legal only **with the opt-out**, `channel.WithSingleSubscriber` is no
      longer *the* opt-in that turns sharing into a typed error (`ErrSharedReplyChannel` is, by default), and
      *"sharing a plain pub-sub reply channel is NOT rejected"* is true of this test only because the test now
      passes `endpoint.WithSharedReplyChannel()`. Rewrite all four lines to name the opt-out explicitly.
      *(Round-6 E-M9. Arm 1 of the staleness sweep matches moved-symbol qualifications and arm 2 matches names
      that are declared nowhere — a sentence that is merely **false** matches neither.)*

- [ ] **Fix the test D-J breaks — this is a required edit, not a discovery.**
      `TestChannelExchange_sharedPubSubReplyChannel` (`endpoint/exchange_test.go:413`) builds **both** its
      exchanges over one plain `NewPublishSubscribeChannel()`: `exA` at `:446` under `require.NoError`, and
      `exB` at `:453`. Both now return `ErrSharedReplyChannel`.

      **Add `endpoint.WithSharedReplyChannel()` UNCONDITIONALLY to both constructions.** Do not try to apply
      it per-case: the table's per-case field is `opts []channel.PubSubOption` — **channel** options, applied
      at `:444` — and both constructions sit in the `t.Run` body shared by the two cases, so "in the
      default-fan-out case" has no seam to hang on and would require restructuring the table.

      **Unconditional is correct, not a shortcut.** In the `WithSingleSubscriber` case the channel reports
      `true`, so the probe passes and the option is inert; `exB` is then rejected by `Subscribe` with
      `ErrChannelSubscribed`, exactly as that case asserts. One edit, both cases still pinning what they
      pinned before — the fan-out trade-off ADR 0028 §6.3 requires, now explicitly opted into.

      > **ROUND-4 CORRECTION (exec-B3).** This bullet said *"add it to `exA` **and** to `exB`'s **in the
      > default-fan-out case**"*, which is not expressible in the test as written and would have stalled an
      > implementer on whether restructuring the table is permitted (the plan forbids changing assertions
      > outside Spec §2.1's exceptions — **six** as of round 6, D-J being row 5; see the goal statement above
      > and E-B6).
- [ ] **The blast radius was swept, not assumed — it is ONE TEST, TWO CONSTRUCTIONS.** Measured across the
      whole workspace, not the pattern core (the §3.6 recurrence pattern is an inventory scoped too narrowly).

      **This is a DERIVED SUMMARY, not pasted output** — the raw command emits 25 lines and is reproduced
      below it so the summary can be checked:

      ```
      # scope: whole workspace, at dadc775
      $ grep -rn "endpoint\.NewChannelExchange(" --include='*.go' . | sed 's/:.*//' | sort | uniq -c
         2 adapter/http/inbound_test.go
         2 adapter/http/stdlib/inbound_test.go
         1 capability_test.go
        18 endpoint/exchange_test.go
         2 endpoint/gateway_test.go
      $ grep -rn "endpoint\.NewChannelExchange(" --include='*.go' . | wc -l
            25
      ```

      Of those 25: **24 bind `reply := channel.NewDirectChannel()`** (or a table-supplied `tc.reply`, or the
      `nilSubChannel{}` fake) and pass the probe unchanged. **Both rejected constructions are in the single
      test named above** — `exA` at `endpoint/exchange_test.go:446` and `exB` at `:453`, over the *same*
      plain `NewPublishSubscribeChannel`.

      > **ROUND-4 CORRECTION (B10 / exec-B2).** This block previously opened with a `$ grep …` prompt and
      > showed **11** annotated lines of a command that emits **25**, and concluded *"exactly one site"*.
      > Both the framing and the count were wrong: fourteen `endpoint/exchange_test.go` call sites were
      > silently dropped (all harmless, but that was not established by the block), and the one affected test
      > has **two** affected constructions, not one. Round 3's rule is that a pasted command must be pasted
      > *whole*; where a summary is genuinely more readable, it must be **labelled a summary** and carry the
      > command that regenerates it. Re-run both commands rather than trusting this block.

**Verify:**

- `go test ./... -race -shuffle=on` green across all root packages; the **seven**-module `GOWORK=off` loop
  (not eight — `expr` does not exist until Task 10).
- **BOTH coverage arms, and the per-package one is not optional.** `-coverpkg=./...` on both sides
  (Global Constraint 4) **and** per-package `GOWORK=off go test ./... -cover`, where **`channel` must stay at
  100.0%**. The aggregate arm alone cannot see this task's own gap: the two new `SingleSubscriber` methods
  live in `channel` while every test that exercises them lives in `endpoint`, so `-coverpkg` credits them at
  100% while the package-local profile shows them at 0% (Spec §3.4e's attribution effect, in the one task
  written after §3.4e). This was compile-proven in the round-4 design audit: `channel` falls 100.0% → 98.3%.
- The four-arm table shows four distinct subtests, **and** the `channel`-package test above pins
  `SingleSubscriber()` for both types directly.
- `apidiff` reports **two additions beyond the six measured at `dadc775`, for eight in total** —
  `ExclusiveSubscribable` and `ErrSharedReplyChannel`. `endpoint`'s `WithSharedReplyChannel` is **not** in
  root's diff, same as `channel.WithSingleSubscriber` was not. *(Round-4 correction, M5/exec-M7: this read
  "exactly two additions", which contradicts §9.5.0's table of 8 and is a delta stated as an absolute.)*

**Commit:** `feat(core,channel,endpoint)!: probe reply-channel exclusivity at construction`

```
Spec: 014
Plan: 027
ADR: 0030
RFC: 0002
```

> **ROUND-6 CORRECTION (E-M5).** The trailers were inlined as `(ADR: 0030 · Plan: 027 · Spec: 014)` — a
> parenthetical on the subject line, which is not a Conventional-Commit **footer** and is not greppable by the
> form Global Constraint 7 and CLAUDE.md mandate (*"prefer this over embedding references in the subject
> line"*). `RFC: 0002` was also missing, though ADR 0030's own header declares it.

---

## Task 9.7 — Classify the shipped deterministic endpoint faults as `Permanent` (decision D-M) · **S** · NOT STARTED

> **NEW in the round-6 pass.** Task 9's combinators adopt `msgin.Permanent(msgin.ErrNilFunc)` because D-M says
> so — but D-M is a **rule about a class**, not about the three new combinators, and the class already has
> **four shipped members**. Fixing only the new code would ship a library where `Predicate.And(nil)` is
> permanent and `transform.Transform(nil)` is not: the same fault, the same cause, two different delivery
> outcomes. That is worse than either uniform answer.
>
> **Why it is its own task, placed HERE (after 9.6, before 10).** Three reasons, stated because the placement
> is a judgement call:
> 1. **It touches shipped code and is a behavior change** (Spec §2.1 row 6), whereas Task 9's commit is a
>    pure-addition commit for a brand-new surface. Folding them would put a `§2.1` exception inside a commit
>    whose whole claim is "additive and source-compatible", and would make the Task 9 `apidiff` review
>    ambiguous. Separate green unit, separate commit, separate revert handle.
> 2. **Task 10 is the first consumer of the rule.** The revised D-K has the `expr` providers rely on root's
>    classification (`ErrPayloadType` is already permanent); a provider author reading a tree where the same
>    class is half-classified will copy the wrong half.
> 3. **Task 12 re-measures and re-reviews.** The classification must be settled before the whole-branch
>    `/code-review`, not discovered by it.
>
> It is placed *after* 9.6 rather than before 9 only to keep the two shipped-code behavior changes (D-J, D-M)
> adjacent in the log; nothing in 9.7 depends on 9, 9.5 or 9.6, so an executor may pull it earlier if the
> branch order demands it — say so in the ledger if you do.

**Skills:** start from `cc-skills-golang:golang-how-to`; TDD via `superpowers:test-driven-development`;
`gopls` for navigation; `table-test` for the branch table; blackbox `_test` packages only.

**RED FIRST — the baseline this task must invert.** Before editing anything, drop a throwaway
`package msgin_test` file at the repo root that prints the classification, run it, and paste the output into
the ledger. Measured at `aae6160` (root code byte-identical to `dadc775` — `git diff --name-only
dadc775..aae6160 | grep -v '^docs/'` → `CLAUDE.md` only):

```
$ go test -run TestDMPermanenceCensus -v .            # output verbatim; the ← notes are ANNOTATIONS
IsPermanent(msgin: nil endpoint function                 ) = false      ← must become true
IsPermanent(msgin: no route for message                  ) = false      ← must STAY false
IsPermanent(msgin: payload is not of the expected type   ) = true       ← unchanged
IsPermanent(msgin: message has no correlation key        ) = false      ← already wrapped at its producer
```

The last row is the shape to copy: `ErrNoCorrelation` is *not* in `IsPermanent`'s closed enumeration either —
its **producer** wraps it (`routing/aggregator.go:151-160`), with D-M's exact rationale already in its godoc.
Delete the throwaway file before committing; the ledger keeps the transcript.

- [ ] **Four edit sites — three `nilFuncStep` copies plus `Router.Handle`.** Verified at `aae6160`:

      | Site | What it is |
      |---|---|
      | `endpoint/helpers.go:21` | `nilFuncStep`'s returned handler |
      | `routing/helpers.go:23` | `nilFuncStep` (package-local copy) |
      | `transform/transformer.go:38` | `nilFuncStep` (package-local copy) |
      | `routing/router.go:48` | `Router.Handle`, the `r.pick == nil` early return |

      Each returns a bare `msgin.ErrNilFunc` today. Each becomes `msgin.Permanent(msgin.ErrNilFunc)`, wrapped
      with the position exactly as Task 9's combinators are:
      ```go
      fmt.Errorf("%w: routing.Router.Handle: nil pick", msgin.Permanent(msgin.ErrNilFunc))
      ```
      The three `nilFuncStep` copies are shared by five public constructors, so the wrap text must name the
      **caller**, not the helper — pass the position in:
      `nilFuncStep("transform.Transform: nil fn")`. A single `msgin: nil endpoint function` string across six
      positions is the debuggability defect CLAUDE.md's *"errors that name the offending field/input"* rule
      exists to prevent.
- [ ] **`ErrNoRoute` is NOT wrapped — this is a decision, not an omission.** `routing/router.go:48-56`'s
      `pick` is caller-supplied and evaluated **per message**; it may consult a routing table, feature flag or
      lookup service, so a message unroutable now may be routable after a config reload. `WithDefaultChannel`
      is the documented way to make that outcome deterministic. Leave it transient and add a **regression
      case** asserting `msgin.IsPermanent(err) == false` for `ErrNoRoute`, so a later sweep cannot "finish the
      job" by wrapping it.
- [ ] **Update every godoc that promises the old behavior.** Each says *"a nil X yields `ErrNilFunc`"* with no
      classification. The exhaustive list, from
      `grep -rn 'ErrNilFunc' --include='*.go' endpoint/activator.go routing/filter.go routing/splitter.go transform/transformer.go routing/router.go | grep '//'`
      at `aae6160`:

      ```
      routing/filter.go:26:// ErrPayloadType; a nil pred yields ErrNilFunc.
      routing/splitter.go:13:// an fn error propagates without forwarding; a nil fn yields ErrNilFunc (no
      endpoint/activator.go:13:// error propagates without forwarding; a nil svc yields ErrNilFunc. For a
      endpoint/activator.go:37:// svc yields ErrNilFunc.
      transform/transformer.go:14:// without forwarding. A nil fn yields ErrNilFunc (no panic on caller input).
      transform/transformer.go:35:// function: its handler returns ErrNilFunc instead of panicking on a nil call.
      routing/router.go:25:// returned channel is ignored). A nil pick yields ErrNilFunc. Router implements
      routing/router.go:36:// construction and surfaces as ErrNilFunc at Handle time (no panic on input).
      ```

      Seven exported-godoc lines across six declarations (`Filter`, `Split`, `Activate`, `Consume`,
      `Transform`, `Router`+`NewRouter`), plus `transform/transformer.go:35`, which is `nilFuncStep`'s own
      unexported comment and must move with them. Each must state that the error is **permanent** — routed to
      the **invalid-message** channel rather than retried to the dead-letter sink — and that
      `errors.Is(err, msgin.ErrNilFunc)` still matches. Re-run the grep after the edit and paste it: the
      godoc gate here is the grep's *content*, not its exit status.
- [ ] **Record it in the ledger** as an `F`-section with the RED transcript, the four sites, and the GREEN
      re-run, and note that Spec §2.1 carries it as row 6.

**Hot-path branches needing a case each** (fold into one `table-test` per package, `assert`-closure form):

- each of the **five** public nil entry points — `endpoint.Activate(nil)`, `endpoint.Consume(nil)`,
  `routing.Filter(nil)`, `routing.Split(nil)`, `transform.Transform(nil)` — asserting **all three** of
  `errors.Is(err, msgin.ErrNilFunc)`, `msgin.IsPermanent(err) == true`, and that the message names its
  position;
- `NewRouter(nil).Handle` — same three assertions, the one non-`nilFuncStep` site;
- **the negative:** `Router.Handle` with a `pick` that returns no channel and no default →
  `errors.Is(err, msgin.ErrNoRoute)` **and `msgin.IsPermanent(err) == false`**.

**Verify:**

- The RED transcript above, re-run, now printing `true` on row 1 and **still `false` on row 2**.
- `go test ./... -race -shuffle=on` green across all root packages; the **seven**-module `GOWORK=off` loop
  (not eight — `expr` does not exist until Task 10).
- **`apidiff` is expected to report NOTHING for this task.** No exported symbol is added, removed or retyped;
  the change is behavioral. Do not treat the empty diff as a failed run — say so in the commit body.
- Per-package coverage on `endpoint`, `routing` and `transform` does not fall (all three are at ≥99% today);
  `-coverpkg=./...` on both sides against a **named** tree (Global Constraint 0).

**Commit:** `fix(core,routing,transform)!: classify nil endpoint functions as Permanent`

```
Spec: 014
Plan: 027
ADR: 0029
RFC: 0002
```

The `!` is deliberate even though `apidiff` is empty: the change moves a message from the **dead-letter** sink
to the **invalid-message** sink and stops it recording an unhealthy breaker signal
(`endpoint/consumer.go:614`, `:733`). A caller who watches the DLQ sees a behavioral break; the exported
surface does not move. *(No tag exists — `git tag | wc -l` → 0 — so nothing downstream is affected today; the
marker is for the log's benefit.)*

---

## Task 10 — The `expr` provider module · **L** · NOT STARTED

> **RE-SIZED M → L in round 6 (E-M7).** The **M** label covered "write six providers and reinstate twelve
> tests". It did not cover **D-K's acceptance bar**, which requires a full consumer + `RetryPolicy` + DLQ
> pipeline *inside the `expr` module's own tests* to prove a result-type mismatch reaches the invalid-message
> sink without consuming a retry — a fixture the module has no other reason to own — nor the module scaffolding
> (`go.mod` + `replace` + `go.work` + three CI edits) that must be green before a single provider compiles.
>
> **Re-sized rather than split into 10a/10b — deliberate**, for the same reason Task 9.5 was: `Task 10` is
> cited by number in `CLAUDE.md:236` and `docs/adrs/0029-eip-lexical-alignment.md:256`, so a renumber is a
> cross-document traceability edit. A split is otherwise clean (10a = module skeleton, `go.work`, CI, and
> `expr/errors.go`; 10b = the six providers and the twelve reinstated tests, both green units) — do it as one
> coordinated change if a future round wants it.

- [ ] New module `expr` with its own `go.mod` (Go 1.25). **It needs BOTH:**
      ```
      require github.com/kartaladev/msgin v0.0.0
      replace github.com/kartaladev/msgin => ..
      ```
      `git tag | wc -l` → **0**, so without the `replace` the module cannot resolve the root module under
      `GOWORK=off` — which is exactly how CI's `module` job runs it. A `use` line in `go.work` is necessary
      but **not sufficient** (round-2 §C2).
- [ ] **`go.work`: add `./expr` to the `use` block — a PREREQUISITE of CI edit #2, not a nicety.** The
      workspace job runs `go build ./...` with the workspace **on**, and a module that is not in `use` cannot
      be built that way at all. Proven in isolation on the untouched tree (a throwaway module created inside
      the repo, then removed):
      ```
      $ (cd .tmp-ework && go build ./...)
      pattern ./...: directory prefix . does not contain modules listed in go.work or their selected dependencies
      exit=1
      $ (cd .tmp-ework && GOWORK=off go build ./...)
      exit=0
      ```
      `go.work` currently has **seven** `use` entries and `expr` is not among them. *(Round-6 E-B1: the `use`
      line appeared only inside the subordinate clause "necessary but not sufficient" above — a remark about
      `replace`, not an instruction — so nothing in this task told anyone to edit `go.work`, and CI edit #2
      would have gone red.)*
- [ ] **CI: three edits, not one.** `.github/workflows/ci.yml`'s `module` matrix lists six directories
      (`grep -c 'dir:' .github/workflows/ci.yml` → **6**) and the `workspace` job hard-codes a six-directory
      loop:
      1. add `expr` to the `module` matrix;
      2. add `expr` to the `workspace` job's loop — **after the `go.work` edit above**;
      3. **fix the pre-existing gap** — `adapter/cron/crontest` is missing from **both** and has been since
         it was created (`grep -n crontest .github/workflows/ci.yml` → no output).
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
      35:func compile[A any](expression string, kind exprOutputKind) (func(Message[A]) (any, error), error) {
      89:func FilterExpr[A any](expression string, opts ...FilterOption) (Step, error) {
      115:func RouterExpr[A any](keyExpr string, routes map[string]MessageChannel, opts ...RouterOption) (*Router, error) {
      167:func TransformExpr[A, B any](expression string) (Step, error) {
      217:func SplitExpr[A, B any](expression string) (Step, error) {
      262:func compileGroup[A any](expression string) (func(groupExprEnv[A]) (any, error), error) {
      277:func toGroupEnv[A any](g MessageGroup) (groupExprEnv[A], error) {
      321:func WithCorrelationExpr[A any](expression string) AggregatorOption {
      390:func WithReleaseExpr[A any](expression string) AggregatorOption {
      418:func exprSliceToChildren[A, B any](out any, parent Message[A]) ([]Message[B], error) {
      $ git show ab233d9:expr.go | grep -cE '^func '
      10
      ```

      > **ROUND-6 CORRECTION (C-B7).** This block was presented as pasted `grep -nE '^func '` output and
      > showed **six** lines where the command emits **ten** — the four unexported helpers (`compile:35`,
      > `compileGroup:262`, `toGroupEnv:277`, `exprSliceToChildren:418`) were dropped, and they are
      > **load-bearing in the very next paragraphs**: point 1 below cites `compile[A]` at `expr.go:35`, the
      > parity step tells the worker to read all four, and the M-1/M-6 cases are derived from `toGroupEnv`. A
      > reader checking the citation against the block would conclude the citation was invented. Round-4 B10
      > named this class and it was fixed at the Task 9.6 instance only. **Global Constraint 0's corollary: a
      > block presented as pasted output is pasted WHOLE, or it is labelled a derived summary and carries the
      > filter that produced it.**

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
- [ ] **`expr/errors.go` — the module declares exactly ONE sentinel (decision D-I, §9.5.0; revised D-K).**
      This is a **prerequisite for the providers compiling**, not a follow-up: every construction path below
      wraps `ErrInvalidExpression`, and root no longer has it.
      ```go
      var ErrInvalidExpression = errors.New("msgin/expr: invalid expression")
      ```
      It is a **new `errors.New` value, not an alias of the deleted root var** — the root var is gone, so an
      alias could not compile even if it were wanted.

      > **REVISED D-K (round 6) — there is NO `expr.ErrExprResultType`.** This checkbox used to declare a
      > second sentinel, `ErrExprResultType = errors.New("msgin/expr: expression result type mismatch")`.
      > It is withdrawn. A result-type mismatch is the **expression-domain twin of root's
      > `msgin.ErrPayloadType`** — ADR 0029 §5.0b said so and then minted a separate sentinel anyway — so the
      > provider returns root's:
      > ```go
      > fmt.Errorf("%w: expr result %T is not %T", msgin.ErrPayloadType, got, want)
      > ```
      > **What this buys:** one shared `errors.Is` target for every future expression provider (CEL,
      > starlark, …) instead of one per provider; the correct retry classification **for free**, because
      > `ErrPayloadType` is already inside root's `IsPermanent` — measured on the untouched tree,
      > `IsPermanent(msgin: payload is not of the expected type) = true` — so **no `msgin.Permanent` wrap is
      > needed**; no root change and no new import edge. `ErrPayloadType`'s godoc is already domain-generic
      > (*"a `Message[any]` payload cannot be asserted to T"*), which is exactly what a result-type mismatch
      > is. **D-I is unaffected** — `ErrInvalidExpression` still leaves root and is still minted here, because
      > it is a construction-time fault with no root twin.
      >
      > **Consequence for the counts:** root's projections in §9.5.0 and Task 12 are **unchanged** (root loses
      > both sentinels either way). The ***`expr`-module*** sentinel count is **1, not 2**.

      Every reinstated test's `errors.Is` target changes: the `ErrInvalidExpression` assertions move from
      `msgin.ErrInvalidExpression` to `expr.ErrInvalidExpression`, and the `ErrExprResultType` assertions
      become `msgin.ErrPayloadType`. `git show ab233d9:expr_test.go` asserts the root `msgin.Err*` form
      throughout, so **expect to rewrite the target in every one of the 12 functions** rather than copying
      them verbatim. Godoc `ErrInvalidExpression` with the construction-vs-evaluation split the deleted root
      godoc carried — it is the only surviving statement of that distinction, so recover it from
      `git show 3d0b87a:errors.go`, lines 168–180, **before Task 9.5 deletes it**.
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

      The twelve functions to reach parity with (`git show ab233d9:expr_test.go | grep -nE '^func (Test|Example)'`),
      **mapped old → new. Do NOT reinstate the old names.**

      | Old (`ab233d9:expr_test.go`) | New | Note |
      |---|---|---|
      | `TestFilterExpr` (`:45`) | `TestPredicate` | provider is `expr.Predicate[A]` |
      | `TestFilterExpr_Concurrent` (`:141`) | `TestPredicate_Concurrent` | |
      | `ExampleFilterExpr` (`:207`) | **`ExamplePredicate`** | |
      | `ExampleRouterExpr` (`:233`) | **`ExampleRouteFunc`** | |
      | `TestTransformExpr` (`:251`) | `TestTransformer` | |
      | `ExampleTransformExpr` (`:326`) | **`ExampleTransformer`** | |
      | `TestSplitExpr` (`:348`) | `TestSplitFunc` | |
      | `ExampleSplitExpr` (`:459`) | **`ExampleSplitFunc`** | |
      | `TestRouterExpr` (`:483`) | `TestRouteFunc` | |
      | `TestWithCorrelationExpr` (`:589`) | `TestCorrelation` | provider is `expr.Correlation[A]` |
      | `TestWithReleaseExpr` (`:676`) | `TestRelease` | |
      | `ExampleWithReleaseExpr` (`:843`) | **`ExampleRelease`** | |

      > **ROUND-6 CORRECTION (E-B3) — the old list was UNEXECUTABLE, not merely stale.** Five of the twelve
      > are `Example*` functions, and `go vet` — which Global Constraint 1 mandates after every move — **hard
      > fails** on an `Example` named for an identifier the package does not declare:
      > `ExampleFilterExpr refers to unknown identifier: FilterExpr`. Reinstating the names verbatim cannot
      > reach green. The five bolded rows are the ones `go vet` enforces; the seven `Test*` renames are for
      > consistency with the providers they now exercise.
      >
      > **The signatures changed too, and the options do not come with them.** The deleted constructors
      > carried option variadics the new providers do not — `FilterExpr[A](expr, opts ...FilterOption)
      > (Step, error)` vs `expr.Predicate[A](s string) (routing.Predicate[A], error)`, and likewise
      > `RouterExpr`'s `...RouterOption`. **Composition moved to the call site:** a case that exercised an
      > option now builds the provider and passes the option to the *base* constructor —
      > `routing.Filter(pred, routing.WithDiscardChannel(d))` where `pred, err := expr.Predicate[A]("…")` —
      > rather than threading it through the provider. Port each option-carrying case that way; do not
      > re-add variadics to the providers to make the old cases compile.

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

- [ ] **A result-type mismatch returns root's `msgin.ErrPayloadType` — decision D-K, AS REVISED in round 6**
      (ADR 0029 §5.0b). Every such site:
      ```go
      return msgin.Message[B]{}, fmt.Errorf("%w: expr result %T is not %T",
      	msgin.ErrPayloadType, out, *new(B))
      ```
      **No `msgin.Permanent` wrap, and no `expr` sentinel.** A result-type mismatch is deterministic — the
      same expression on the same payload yields the same wrong type on every redelivery — so it must not be
      retried `MaxAttempts` times and, per Spec 014 §10's per-instance attempt tracking, **`N × MaxAttempts`
      across N instances**. Reusing `ErrPayloadType` delivers exactly that, because root's `IsPermanent`
      already names it (measured: `IsPermanent(msgin: payload is not of the expected type) = true`), so the
      classification arrives for free and every future expression provider shares one `errors.Is` target.

      > **SUPERSEDED WORDING.** This checkbox previously read *"`ErrExprResultType` is wrapped in
      > `msgin.Permanent`"* over an `expr`-owned sentinel. Both halves are withdrawn — there is no such
      > sentinel, and no wrap is needed. See the `expr/errors.go` checkbox above.

      **`ErrInvalidExpression` is NOT wrapped either** — it is a construction-time fault that never reaches
      the retry path. The two faults remain asymmetric; do not treat them uniformly.

**Hot-path branches needing a case each:** invalid expression → `expr.ErrInvalidExpression` at construction;
valid expression, wrong result type **→ asserted `errors.Is(err, msgin.ErrPayloadType)` AND
`msgin.IsPermanent(err) == true`, and asserted to reach the INVALID-MESSAGE sink rather than the dead-letter
sink, with the retry count unchanged** (revised D-K — this is the acceptance bar that needs a real consumer +
`RetryPolicy` + DLQ fixture inside this module's tests, and it is why the task is sized **L**); runtime
evaluation error carrying the expression text; nil/empty expression string; `Release`'s runtime error
surfacing through `Handle` rather than returning `false`; **`RouteFunc`'s two construction validations**;
`toGroupEnv`'s empty-group and non-`A`-member guards.

**Verify:** ADR 0019's fail-at-construction contract holds — an invalid expression errors at the provider
call, never at first message. All **eight** modules green standalone under `GOWORK=off`.

**Commit:** `feat(expr): expression providers as a separate module`

---

## Task 11 — Package docs AND the unowned godoc obligations · **M** · PARTIAL

> **This task grew in round 3.** Spec 014 **§8's nine godoc bullets** and **§10's four multi-instance godoc
> obligations** were written in the indicative, as though they described the tree, and **had no owning task
> at all** — so nothing in this plan was ever going to produce them. Audited against HEAD, **five of the nine
> and two of the four were unmet.** They are Task 11 checkboxes now, each grep-verifiable.

### 11a — the five subpackage `doc.go` files · **DONE** (`1d7fc80`, F12.5)

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

Each line pairs the edit with the command that proves it. **Every gate below is RED on the untouched tree, and
the transcript proving it is in §11 RED baseline. Re-run the whole baseline block FIRST, paste it into the
ledger, and only then start editing.**

> **TASK 11 MUST RUN AFTER TASK 9.6.** Spec §8 obligations **10–13** document symbols that do not exist until
> 9.6 lands (`ExclusiveSubscribable`, `ErrSharedReplyChannel`, `WithSharedReplyChannel`). Ordering matters:
> running 11 first leaves four obligations permanently unowned, which is the exact §8 failure this task exists
> to close.

> ## ⛔ ROUND-6 GATE REWRITE (D-B5 / E-B4 / E-B5) — the four D-J gates were DECORATIVE; three passed with zero
> ## work done, and the fourth could not be satisfied by any correct edit.
>
> Measured in a worktree where `ExclusiveSubscribable` had been added with ADR 0030's godoc pasted verbatim
> and **no obligation text written at all**:
>
> - **§8.10** `grep -A20 'type SubscribableChannel' channel.go | grep -c ExclusiveSubscribable` → **2**.
>   `SubscribableChannel`'s doc comment sits at `channel.go:24-42`, **above** the declaration at `:43`, so
>   `-A20` reads `:43-63` — the gate cannot see the godoc it gates, and self-satisfies by matching the **new
>   interface's own declaration**, which Task 9.6 puts in this same file. Every sibling gate uses `-B`; this
>   one was the odd one out.
> - **§8.11** `grep -B12 'SingleSubscriber() bool' channel.go | grep -ci concurrent` → **1**, satisfied by the
>   word *"concurrent"* occurring incidentally in ADR 0030 §1's own pasted snippet (*"a second **concurrent**
>   subscriber"*). The obligation exists to impose a concurrency requirement **on implementers**.
> - **§8.12** `grep -B30 'func NewChannelExchange' endpoint/exchange.go | grep -c ErrChannelSubscribed` → **1
>   on the untouched tree**, matching pre-existing prose at `:210-211`. The obligation is that the godoc's
>   *error list* (`:221-224`) enumerate it. This is round-4 B6's *"a proof that cannot fail"* class,
>   reintroduced by the round-5 pass.
> - **§11c-1** `grep -A22 'func WithSingleSubscriber' channel/pubsub.go | grep -i 'single-process|per-process'`
>   → **exit 1, unsatisfiable**: the godoc is `:66-82` and the function body is one line at `:83`, so `-A22`
>   reads `:83-105`. No correct edit could ever turn it green.
>
> **A doc comment sits ABOVE its declaration, so every `grep -A` gate on a godoc reads the wrong lines — and a
> `grep -B` gate reads a window whose size is a guess.** All godoc gates below now use **`go doc`**, which
> extracts the comment by construction, so the direction bug cannot recur and no window needs sizing.
> `go doc` also fails loudly (`doc: no symbol X in package Y`, exit 1) when the symbol is absent, which is the
> correct RED for an obligation whose symbol Task 9.6 has not yet created.

#### §11 RED baseline — run this BEFORE any edit; every line must print `RED`

```bash
g() { if eval "$2" >/dev/null 2>&1; then echo "GREEN(bad, no work needed): $1"; else echo "RED: $1"; fi; }
M=github.com/kartaladev/msgin
g 8.10 "go doc \$M.SubscribableChannel | grep -q ExclusiveSubscribable"
g 8.11 "go doc \$M.ExclusiveSubscribable | grep -Eq 'safe for concurrent use' && \
        go doc \$M.ExclusiveSubscribable | grep -Eq 'constant for the lifetime'"
g 8.12 "[ \"\$(go doc \$M/endpoint.NewChannelExchange | grep -c 'ErrSharedReplyChannel\|ErrChannelSubscribed\|does not implement\|within this process')\" -ge 4 ]"
g 8.13 "go doc \$M/endpoint.WithSharedReplyChannel | grep -qi suppress"
g 8.1  "grep -rn -i 'correlation identifier' --include='*.go' ."
g 8.3  "grep -rn -i 'amqp' --include='*.go' . | grep -q 'spi.go'"
g 8.4a "go doc \$M/routing.CorrelationStrategy | grep -qi spring"
g 8.4b "go doc \$M/routing.ReleaseStrategy | grep -qi spring"
g 8.7  "grep -q -i QueueChannel adapter/http/inbound.go && grep -q -i QueueChannel adapter/http/stdlib/inbound.go"
g 11c1 "go doc \$M/channel.WithSingleSubscriber | grep -Eqi 'single-process|per-process'"
g 11c2 "go doc \$M.RetryPolicy | grep -qi instance"
```

**Pasted output of exactly that block, run on the untouched tree at `aae6160`:**

```
RED: 8.10
RED: 8.11
RED: 8.12
RED: 8.13
RED: 8.1
RED: 8.3
RED: 8.4a
RED: 8.4b
RED: 8.7
RED: 11c1
RED: 11c2
```

Two of them are RED for the *"symbol does not exist yet"* reason, which `go doc` reports explicitly —
`doc: no symbol ExclusiveSubscribable in package github.com/kartaladev/msgin` (8.11) and
`doc: no symbol WithSharedReplyChannel in package …/endpoint` (8.13). 8.12 is RED on the **threshold**: the
raw `grep -c` is **1** today, matching pre-existing prose at `endpoint/exchange.go:210-211`, which is exactly
why the bar is `>= 4` and not `>= 1`.

*(§8.12's threshold is `>= 4`, not `>= 1`, because the obligation is a **four-item error/outcome list**; a
`>= 1` count was already satisfied by unrelated prose. §8.11 is an **AND of two** property phrases, because a
single word can be matched incidentally.)*

- [ ] **§8.10 — `SubscribableChannel`'s godoc cross-references `ExclusiveSubscribable`** (D-J). Without it the
      optional capability is undiscoverable from its own supertype, and a third-party channel author never
      learns the probe exists — leaving the accept-unknown arm permanent for exactly the fan-out-capable
      channels ADR 0030 §Topology's second topology describes.
      → `go doc github.com/kartaladev/msgin.SubscribableChannel | grep -q ExclusiveSubscribable`
- [ ] **§8.11 — `SingleSubscriber`'s godoc states the END-TO-END definition (D-L), requires INVARIANCE, and
      requires concurrency safety.** All three, and the gate asserts two of them independently:
      - it reports whether **this exchange will be the sole recipient of every message sent to this channel** —
        a statement about the channel's **policy**, not its live subscriber count; an implementation **MUST
        NOT** compute it from a subscriber count, and a channel whose deliveries reach other processes (a
        broker subject, Redis pub/sub, an SSE stream) **MUST return `false`**;
      - the value **MUST be constant for the lifetime of the channel** — msgin calls it once, at construction,
        and treats it as an invariant. Concurrency-safety is the *weaker* property: a race-free
        `atomic.Load(&n) == 0` is concurrency-safe and still lies (TOCTOU). State **both**;
      - **implementations must be safe for concurrent use** — msgin never calls it concurrently, so a
        third-party implementer's data race is invisible to msgin's own `-race` suite;
      - **embedding cuts both ways.** A type embedding `*channel.DirectChannel` or
        `*channel.PublishSubscribeChannel` **inherits `SingleSubscriber` by method promotion** and keeps
        reporting on the embedded channel even when it overrides `Subscribe` with multi-subscriber dispatch —
        so promotion is a **hazard**, not only the remedy ADR 0030 §5 presents. Conversely, the idiomatic
        one-line decorator `struct{ msgin.SubscribableChannel }` promotes `Send` and `Subscribe` but **not**
        `SingleSubscriber`, so a wrapper silently opts out of the probe.
      → `go doc …msgin.ExclusiveSubscribable | grep -Eq 'safe for concurrent use'` **AND**
        `go doc …msgin.ExclusiveSubscribable | grep -Eq 'constant for the lifetime'`
- [ ] **§8.12 — `NewChannelExchange`'s godoc states FOUR outcomes and enumerates `ErrChannelSubscribed`.**
      rejected · accepted-exclusive · accepted-no-probe · **accepted but exclusive only within this process**.
      `ErrChannelSubscribed` is returned unwrapped from `reply.Subscribe` (`endpoint/exchange.go:250`) and is
      absent from the doc's error list (`:221-224`, which names only `ErrNilChannel`,
      `ErrInvalidReplyTimeout`, `ErrNilSubscription`), so a caller cannot write correct handling from the doc.
      The accepted-no-probe arm must also carry D-L's wrapper caveat: *a reply channel that wraps another by
      embedding the `msgin.SubscribableChannel` interface does not inherit `SingleSubscriber`, so it is
      accepted here even when the channel it wraps would be rejected.*
      → `[ "$(go doc …/endpoint.NewChannelExchange | grep -c 'ErrSharedReplyChannel\|ErrChannelSubscribed\|does not implement\|within this process')" -ge 4 ]`
- [ ] **§8.13 — `WithSharedReplyChannel`'s godoc says it SUPPRESSES THE PROBE, not that it confers
      shareability.** On a `DirectChannel` the second exchange still gets `ErrChannelSubscribed`; neither the
      option's name nor that sentinel's text hints the option cannot help. *(This wording is only true if the
      guard tests `cfg.allowShared` **first** — see the note in Task 9.6.)*
      → `go doc …/endpoint.WithSharedReplyChannel | grep -qi suppress`

- [ ] **§8.1 — name Correlation Identifier.** "Return Address" is present; the *in-process* pattern is never
      named. Add it to `endpoint`'s `ChannelExchange`/`doc.go` prose.
      → `grep -rn -i 'correlation identifier' --include='*.go' .` must be **non-empty**.
- [ ] **§8.3 — the AMQP disclaimer on `RequestReplyExchange`.** Currently **absent workspace-wide**, despite
      Spec 014 §6 and ADR 0029 §2 both asserting in the present tense that it exists.
      → `grep -rn -i 'amqp' --include='*.go' . | grep spi.go` must be **non-empty** (the earlier form only
      required a hit *somewhere*, and separately asserted "must hit `spi.go`" in prose — fold the assertion
      into the command).
- [ ] **§8.4 — every named behavior type names its Spring equivalent, per type.** Currently only the *package*
      docs name Spring; `routing.CorrelationStrategy` and `routing.ReleaseStrategy` do not. This is the
      mitigation that justifies dropping the Spring names (ADR 0029 §4), so it is **not** discharged by a
      package-level mention.
      → for each of `CorrelationStrategy`, `ReleaseStrategy`, and Task 9's `Predicate`, `RouteFunc`,
      `SplitFunc`, `Transformer`: `go doc github.com/kartaladev/msgin/<pkg>.<T> | grep -qi spring`.
      *(Was `grep -B10 'type <T>' <file>` — a guessed window over a hand-named file; `go doc` needs neither.)*
- [ ] **§8.7 — `msghttp.ServeAsync` and `stdlib.NewInbound` state the widened `target` contract.** Neither
      godoc mentions that any `MessageChannel` — a durable `QueueChannel`, a `PublishSubscribeChannel`, any
      `OutboundAdapter` — now qualifies, which is the whole user-visible payoff of §5.0 rows 7–8.
      → `grep -n -i 'QueueChannel' adapter/http/inbound.go adapter/http/stdlib/inbound.go` must hit **both**
      files (check the file list of the output, not just its exit status).

### 11c — Spec 014 §10's unmet multi-instance obligations (CLAUDE.md mandatory)

- [ ] **`channel.WithSingleSubscriber` states it is a SINGLE-PROCESS guard.** ADR 0028 §6.2 requires it in so
      many words — *"must not be documented as a distributed exclusivity guarantee"* — and the godoc
      (`channel/pubsub.go:66-82`) never mentions the process boundary. Two instances each holding their own
      `PublishSubscribeChannel` still each accept a subscriber.
      → `go doc github.com/kartaladev/msgin/channel.WithSingleSubscriber | grep -Eqi 'single-process|per-process'`
      *(Was `grep -A22 'func WithSingleSubscriber' channel/pubsub.go | grep -i …` — **unsatisfiable**: the
      body is one line, so `-A22` read 22 lines of the NEXT declarations. Also note the old text cited the
      godoc as `:69-83`; it is `:66-82`.)*
- [ ] **`RetryPolicy.MaxAttempts` states the per-instance bound.** `retry.go:37-41` says nothing about
      topology, so a caller sizing a poison-message threshold behind a load balancer gets `N ×` what they
      asked for. Say: attempts are tracked per instance (`endpoint/attempts.go:26`), so across N nodes the
      effective global bound is `N × MaxAttempts`; this applies **only** to sources without a native
      delivery-count header (a `NativeReliability` source is unaffected); the distributed answer is the
      broker's own redelivery count or a shared idempotency/dedup store.
      → `go doc github.com/kartaladev/msgin.RetryPolicy | grep -qi instance` *(`go doc` on the struct prints
      the field doc comments too — verified — so no `-B` window is needed.)*

**Verify:**
```bash
for p in . endpoint routing transform channel resilience; do
  n=$(grep -l '^// Package ' $p/*.go 2>/dev/null | wc -l | tr -d ' ')
  [ "$n" = 1 ] || { echo "FAIL $p has $n"; exit 1; }
done                                   # silent — this is the check, NOT `go vet` (Global Constraint 3)
```
Plus the **§11 RED baseline block re-run to all-GREEN**, with **both** transcripts (the RED before and the
GREEN after) pasted into the ledger. A gate with only an "after" transcript proves nothing — that is
round-6's counter-rule 4, and it is the reason three of these four gates shipped decorative.
`go vet ./...` clean.

**Commit:** `docs(core): package docs and the godoc obligations Spec 014 §8/§10 require`

---

## Task 12 — Migration guide, doc sync, and the whole-branch gate · **M** · NOT STARTED

- [ ] **`MIGRATION.md`**, covering at minimum:
      - every moved symbol old→new — **97 removals reconciled against Spec 014 §4.1's five classes** (the
        fifth being D-I's two deleted sentinels);
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
- [ ] **CLAUDE.md, same commit** (traceability, non-optional): the **"Commands" section's**
      *"`./...` does NOT mean 'the repo'"* block and the **"Architecture blueprint"** section must name the
      **eighth** module (`expr`) and its package. **Cite CLAUDE.md by SECTION, never by line number** — they
      rot on every edit, and this checkbox is the proof.

      > **ROUND-6 CORRECTION (M-1).** This checkbox was stale three separate ways at `aae6160`:
      > (1) *"dependency policy drops `expr-lang` and **keeps `robfig`**"* — **already done**; (2) *"the
      > `FilterExpr`/`RouterExpr` mention at `CLAUDE.md:235` goes with it"* — those names, and
      > `StreamingSource`, are **already gone**
      > (`grep -n 'FilterExpr\|RouterExpr\|StreamingSource' CLAUDE.md` → exit 1, no output), and `:235` is now
      > inside the dependency-policy bullet list, nowhere near what the checkbox described; (3) the
      > `./...`-is-not-the-repo block was **already updated in round 5** and correctly describes a 7-module
      > workspace today. Only the **eighth module** remains, and it lands with Task 10's `go.work` + CI edits.
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
        | awk -F'\t' '$5=="exported" && $3!="method"{print $4}' | sort -u | wc -l                        # 102 (projected)
      go run docs/plans/027-tools/decls.go . | grep -v _test.go \
        | awk -F'\t' '$3=="var" && $4 ~ /^Err/{print $1}' | sort | uniq -c                               # 42 errors.go (projected)
      ```
      > **NO LONGER CONTINGENT — both decisions are closed (2026-07-28).** D-I removes two sentinels from root
      > and D-J adds two symbols to it, so the expected end state is **102 exported / 42 sentinels /
      > apidiff 97 removals + 8 additions**:
      >
      > | | exported | sentinels | removals | additions |
      > |---|--:|--:|--:|--:|
      > | Measured at `dadc775` | 102 | 43 | 95 | 6 |
      > | − D-I (§9.5.0) | 100 | 41 | 97 | 6 |
      > | + D-J (§9.6) | **102** | **42** | **97** | **8** |
      >
      > **Every number in the right-hand rows is a projection.** This task's job is to **measure**, not to
      > confirm: run each command, paste its output, and if it disagrees with this table, **the table is the
      > defect** — find which symbol moved and why before touching either number. Three rounds of this plan
      > have been sunk by a transcribed number; do not add a fourth.
      Then diff root's exported surface against **Spec 014 §4's closed list** — every symbol accounted for,
      nothing extra.
- [ ] **Re-run the `MessageChannel` scope-rule census** (Spec 014 §5.0) rather than citing a number. Three
      documents have now quoted three different wrong counts; the check is the contract.
- [ ] Run the full per-module `GOWORK=off` loop across **eight** modules, `go vet`, `golangci-lint`,
      `test -z "$(gofmt -l .)"`, `govulncheck`, `go mod tidy` (no-op in every module), and
      `CGO_ENABLED=0 go build ./...`.
- [ ] `apidiff`/`gorelease` against the Task 0 baseline; reconcile **every** entry against Spec 014 §4.1's
      decomposition — **projected 97 removals / 8 additions**, i.e. the measured 87 + 6 + 1 + 1 = 95 partition
      **plus a fifth class**: `ErrInvalidExpression` and `ErrExprResultType`, removed by D-I. Add that row to
      §4.1's table when the measurement confirms it. An unexplained entry blocks the merge.
- [ ] Coverage with **`-coverpkg=./...` on both sides**. **SIX** accepted uncovered blocks are pre-recorded in
      Spec 014 §9 AC-7, each with a written disposition; none is a regression and none may be reported as a
      drop:

      | Block | Disposition |
      |---|---|
      | `endpoint/consumer.go:467.20,469.15` | accepted, **flaky** — the `case <-ctx.Done():` dispatch arm, covered ~1 run in 3 (F10.8) |
      | `endpoint/gateway.go:30.27,32.3` | accepted, pre-existing — byte-identical before and after the split |
      | `endpoint/nativereliability.go:9.52,9.68` | accepted, pre-existing — the `noNativeReliability` no-op |
      | `endpoint/poller.go:152.11,153.80` | accepted, pre-existing |
      | `endpoint/poller.go:164.12,166.3` | accepted, pre-existing |
      | `resilience/breaker.go:179.28,181.3` | accepted, pre-existing — `toHalfOpen` at **87.5%**, 87.5% before the split too |

      Anything **not** in this six-row list is a finding. Re-derive the list rather than trusting it:
      Spec §9 AC-7 carries the `awk`-over-`head.cov` command that produced it.

      > **ROUND-6 CORRECTION (C-M6).** This bullet said *"the **two** known non-regressions"* and named
      > `breaker.go:176` and `consumer.go:467`. Spec §9.7/AC-7 says **six** in so many words — *"The earlier
      > wording named two … found eleven … Five were fixed; six remain"* — so a Task 12 worker following this
      > plan would have reported the other **four** as regressions and blocked the merge. (The `breaker.go`
      > line number was also off: it is `:179.28,181.3`, not `:176`.) The Risks table carried the same
      > *"two"*; both are swept.
- [ ] Whole-branch `/code-review` and `/security-review` over `main..HEAD`; resolve or triage every finding.

**Commit:** `docs: migration guide and doc sync for the core restructure`

---

## Risks

| Risk | Mitigation |
|---|---|
| A count in a document rots and nobody notices | Every normative number in Spec 014 §3–§4 is generated with its command recorded in the ledger; Task 12 re-derives them rather than reading them. The `MessageChannel` census is stated as a **scope rule + command**, because three rounds of correcting the *number* produced three wrong numbers |
| A change is invisible to the compiler | `go vet ./...` (not `go build`) after every move; the two-arm staleness sweep in Task 9.5; `unused` is off, so dead code needs an explicit check |
| Coverage looks like it regressed when it did not | Global Constraint 4: `-coverpkg=./...` on both sides. The **six** accepted uncovered blocks are enumerated with dispositions in Spec 014 §9 AC-7 and restated in Task 12, so a worker does not chase them *(round-6 C-M6: this said "two", and a worker would have reported the other four as regressions)* |
| A satellite module is left red | The `GOWORK=off` loop — **seven** modules until Task 10, **eight** after (Global Constraint 5); **`harness` is checked with `go vet`, never `go test`** — it has no test files and `go test` reports a false pass |
| A "pure move" quietly changes behavior | No assertion may change outside Spec 014 §2.1's **six** exceptions (rows 5–6 are D-J and D-M); identity is proved by **the normalised per-file diff alone** |
| `apidiff` noise hides a real break | Read against Spec 014 §4.1's decomposition of the removals into named classes — 95 into four at `dadc775`, a projected 97 into five once D-I lands |
| RED cannot be evidenced for a compile-time failure | The transcript comes from `go test -c -o /dev/null .`, not `go vet` (which stops after one type-error batch) |
| `expr` cannot build standalone | Task 10 ships `require` + `replace` together, and CI gets all three edits |
| `gopls` unavailable in a subagent | No task depends on it; `go vet ./...` is the authoritative reference-finder and `grep` the fallback |
| Root loses its package doc | Done in Task 1's change; the five subpackage docs landed in the round-3 pass (F12.5). Task 11's verify **counts** them — `go vet` does NOT catch a duplicate (Global Constraint 3) |
| A godoc obligation has no owning task | Spec 014 §8's nine bullets and §10's four obligations were unowned and six were unmet; **Task 11b/11c owns all thirteen** |
| A godoc gate passes without the godoc being written | **Every 11b/11c godoc gate is a `go doc` command, not a `grep -A`/`-B` window** — a doc comment sits *above* its declaration, so `-A` reads the wrong lines and `-B` guesses a window size; `go doc` extracts the comment by construction. **And every gate is demonstrated RED before the edit** (§11 RED baseline), because a gate that is green on the untouched tree ticks with zero work. Round 6 found three of four D-J gates self-satisfying and the fourth unsatisfiable |
| A number is pinned to an intermediate state | **Global Constraint 0** — every pasted command carries its commit range and module scope. This is the signature behind every round-3 defect |
| Expression support absent mid-branch | Bounded to Tasks 1→10 within one branch. **Task 10's parity bar is git, not the ledger**: `git show ab233d9:expr_test.go` + `git show ab233d9:expr.go` *(round-6 C-B5 — this row said "Task 1 preserved the test cases in the ledger", which §Ledger and Task 10's own round-3 `CORRECTED` block each contradict: none of the twelve deleted test functions is recorded anywhere under `docs/`)* |
| Branch conflicts with in-flight feature work | Run this window **before** Plan 028 (gin) or any new adapter; blast radius grows with every adapter landed first |

> **ROUND-6 CORRECTION (M-B2 / C-B4) — a withdrawn argument survived where it was used as a MITIGATION.**
> The *"pure move"* row above prescribed *"identity is proved by identical `Test*` name sets plus a normalised
> per-file diff"*. Round-5 BLOCKER 2 **withdrew** that argument rather than repairing it, after measuring the
> `Test*`/`Example*` count in every frame. Re-derived here, scope stated (root module, `adapter/` excluded):
>
> ```bash
> for p in ab233d9 c83dde9~1 c83dde9 b6ce7bb dadc775; do
>   n=$(git grep -hE '^func (Test|Example)' $p -- '*_test.go' ':(exclude)adapter/*' \
>       | sed -E 's/^func ([A-Za-z0-9_]+).*/\1/' | sort -u | wc -l)
>   echo "$p: unique=$n"
> done
> # ab233d9: unique=224   c83dde9~1: unique=224   c83dde9: unique=211
> # b6ce7bb: unique=218   dadc775:   unique=221
> ```
>
> The sets are not identical in any adjacent pair, and Spec AC-5 now **forbids** using it (*"not by a
> `Test*`/`Example*` name-set
> identity, which §2 withdrew in round 5 as false in every frame"*). Round 5 swept the three places the proof
> was **stated as a fact** and missed the one place it was **prescribed as the thing to do** — and the Risks
> table is exactly where a worker looks for a mitigation to apply. **Counter-rule: when a claim is withdrawn,
> grep for it in every Risks and Consequences section too, not only where it was asserted.**
