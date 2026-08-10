# Plan 027 — Core package layout, channel segregation, and behavior types

> ## Status: **REGENERATED 2026-07-28 from a green tree — AUDITED THROUGH ROUND 8 (bounded: design of the new decisions + the gate sets); verdict `NEEDS-REVISION` 2/2 (14 blockers, 11 minors), fix pass applied per [round 8](027-audit-round-8.md)**
>
> **Rounds 4–8 have run since this plan was regenerated.** [Round 8](027-audit-round-8.md) is the live one and
> is the **last audit round** — read its **§1** (D-P), **§2** (the blockers, by owner), **§3** (verified sound
> — do not re-open) and **§4** before executing anything. Round 8 produces **D-P** and corrections to **D-N**,
> **D-O**, **D-M** and **D-K**; round 7's **§0** (counter-rules 6–10) still governs, and its **§5** records
> which of *its* corrections landed. **Implementation begins at Task 9 once the round-8 fix pass is in.**
> *(Round-7 M-M9 / round-8 gate minor: this headline read "ROUND-3 AUDIT: `NEEDS-REVISION` 3/3" until round 7
> and "AUDITED THROUGH ROUND 7" until round 8, each time while the body cited a later round. Per-round detail
> stays in the body; the headline names the latest round only — and is part of every round's fix pass.)*
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
> D-A…D-H stand. **Later rounds:** [round 6](027-audit-round-6.md) — read its **§6**, which corrects its own
> §1–§4 — produced D-K/D-L/D-M; [round 7](027-audit-round-7.md) produced **D-L (revised)**, **D-N**, **D-O**
> and the `ErrNilSink` scope correction to D-M, and its **§5** is the fix-pass ledger this plan's remaining
> corrections are drawn from. Rounds 3–5 are recorded in [`docs/HANDOVER.md`](../HANDOVER.md). Round-7 M-M2:
> only rounds 1 and 2 were linked from here, so the two newest records had no reverse link at all.)*

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

**This increment is behavior-preserving by construction, with the decided exceptions enumerated by
[Spec 014 §2.1's table](../specs/014-core-package-layout.md#21-the-deliberate-behavior-changes-the-register)
— that table is the register, and this plan deliberately does not restate its length.** As of round 8 it
holds: the channel segregation; `ChannelExchange.Close` cancelling its reply subscription;
`channel.WithSingleSubscriber()` (off by default); `WithReleaseStrategy`'s retyping; **the reply-channel
exclusivity probe (D-J, Task 9.6)**; **deterministic endpoint faults becoming `Permanent` (D-M, Task 9.7 for
the shipped producers, Task 9 for the combinators)**; **`divert` falling back to the dead-letter sink
before discarding, single-shot (D-N + D-P, Task 9.7)**; and **the producer returning a permanent outbound
error without dead-lettering it (D-M's producer-side consequence, Task 9.7 — gate 3, no code change)**.
Everywhere else, a task that finds itself rewriting an assertion has
either found a real defect (stop, report it) or is doing more than the plan says (stop, re-read the task).

> **ROUND-6 CORRECTION (E-B6), EXTENDED IN ROUND 7.** This read *"exactly four decided exceptions"* while D-J
> (Task 9.6) already required changing an assertion; round 6 re-typed it as *"six"*, and round 7's D-N made it
> seven within the day. **The count is not swept again — it is deleted.** Every site that referred to the
> register by length now cites the table, which is the convention ADR 0028 adopted in round 6. A cardinality
> word here is load-bearing (an implementer executing Task 9.6 was literally instructed by the old sentence to
> *stop and report it*) and is therefore the wrong shape of fact to write down at all.

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
5. **Eight modules at the end — reached as of Task 10's `expr` scaffolding half.** `./...` at root covers 11
   packages only. The per-module `GOWORK=off` loop is the gate. **Copy this block verbatim:**
   ```bash
   for d in . adapter/database/sql/{harness,postgres,mysql,sqlite,dbtest} adapter/cron/crontest expr; do
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

   > **PIN THE PATH PREFIX AND THE ORDER ON EVERY PASTED SWEEP — `grep -r` is not portable between shells.**
   > Two properties of `grep -r <pattern> .` are environment-dependent, and both break the "diff the pasted
   > block" gate on pure noise:
   > - **The `./` prefix.** GNU/BSD `grep -rn … .` emits `./adapter/memory/queuestore.go:146:`; the **ugrep**
   >   wrapper installed on at least one machine used for this bundle emits `adapter/memory/queuestore.go:146:`
   >   — no prefix. Measured 2026-07-30 on the tree, same pattern, same directory:
   >   ```
   >   $ grep -rn "return msgin.ErrNoSubscriber" --include='*.go' . | head -1
   >   channel/direct.go:87:		return msgin.ErrNoSubscriber
   >   $ /usr/bin/grep -rn "return msgin.ErrNoSubscriber" --include='*.go' . | head -1
   >   ./channel/direct.go:87:		return msgin.ErrNoSubscriber
   >   ```
   >   **Every pasted `grep -r … .` transcript in this bundle is in the prefix-stripped form.** So: append
   >   **`| sed 's,^\./,,'`** to any `grep -r … .` whose output is pasted or diffed, and paste the output of
   >   *that* command. This is a documentation gate, not a code gate — it never changes what a sweep finds.
   > - **Traversal order** (round-7 owner 2): three runs on an unchanged tree emitted the same twelve lines in
   >   two different orders. Every pasted sweep is additionally **`| sort`-pinned**.
   >
   > A sweep that is pasted but neither prefix- nor order-pinned is not reproducible, which under Global
   > Constraint 0 makes it a wish rather than a claim.

10. **The canonical gate block has exactly ONE definition — Plan §11 ≡ Spec 014 §8.0b on the six shared ids.**
    The two documents each carry a copy (the Spec's is normative, the Plan's is the executable transcript), and
    in round 7 they diverged silently: the Spec had seven conjuncts on obligation 11 where the Plan had two, an
    8.11a gate the Plan lacked entirely, and two different shapes for obligation 12. Both sets were RED, so
    nothing caught it until an auditor built the comparison table by hand.

    **Both blocks open with the marker comment `# ==== CANONICAL GATE BLOCK` at column 0 and use the
    identical `g <id> "<command>"` form**, so the check is a mechanical `diff` rather than a reading. Run it
    whenever either block is touched:

    ```bash
    gates() {   # every `g <id> "<cmd>"` inside a document's canonical block, continuations joined
      sed -n '/^# ==== CANONICAL GATE BLOCK/,/^```$/p' "$1" \
        | awk '/\\$/ { sub(/\\$/,""); printf "%s", $0; next } { print }' \
        | grep -E '^g ' | sed 's/[[:space:]][[:space:]]*/ /g'
    }
    diff <(gates docs/plans/027-core-package-layout.md \
             | grep -E '^g (8\.10|8\.11|8\.11a|8\.12|8\.13|11c1) ') \
         <(gates docs/specs/014-core-package-layout.md)                     # MUST BE EMPTY
    ```

    The **Plan's block is a superset** — it also carries 8.1, 8.3, 8.4a–8.4f, 8.7 and 11c2, which have no Spec
    §8.0b counterpart; the `grep -E` above selects the shared six. The Spec block carries exactly those six and
    nothing else, so its side needs no filter, and a gate added to it with no Plan counterpart fails the diff
    in the other direction.

    **The marker pattern is anchored `^# ` deliberately.** An unanchored `/==== CANONICAL GATE BLOCK/` also
    matches *this constraint's own prose*, which sits earlier in the file than the block it describes — so the
    `sed` range would open here and run to the block's closing fence, making the extractor self-referential.
    Caught by running it (a `parse error near '}'` when the extracted range was `eval`ed), not by reading it.

    *(Round-8 C2. The round-7 pass wrote "Standing check, now a Global Constraint" into Task 11b and never
    created the constraint — the list ran 0–9. A mechanism that exists only as a sentence describing itself is
    the same absence it was meant to close.)*

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
| **9** | Named behavior types + combinators | **DONE** — committed `544cb5b`, ledger §F16. All four types + `And`/`Or`/`Not`; gates 8.4c–8.4f RED→GREEN. The `MessageChannel` census is **15**, not the projected 14: naming a type RELOCATES an occurrence (`RouteFunc`'s own declaration is a census line) rather than removing one |
| **9.5** | Residual cleanups the migration left behind | **DONE** — committed `910e092`, ledger §F17. The dead-helper deletion and the article sweep had landed in the round-3 pass; this commit adds **D-I's `errors.go` deletion** (root 102→100 exported, 43→41 sentinels, `apidiff` 95→97 removals — the projections held exactly), both sweep arms empty, and the capability test widened 9 → **24** subtests (18 root + 3 `adapter/http` + 3 `adapter/http/stdlib`) |
| **9.6** | Reply-channel exclusivity probe (**D-J**, ADR 0030) | **DONE** — all five gates 8.10/8.11/8.11a/8.12/8.13 RED→GREEN (11c1 still RED, Task 11c's); `apidiff` 97 removals / **8** additions, exactly the D-J projection; `channel` back to **100.0%** on the per-package profile; **seven** subtests in the `endpoint` truth table (the seventh pins D-M2, execution finding I-1) plus the three-case `channel` table, every assertion mutation-killed |
| **9.7** | Classify the **five** shipped producers (`ErrNilFunc` ×4 + `ErrNilSink`) as `Permanent` (**D-M**), add the dead-letter fallback (**D-N**) and make it single-shot (**D-P**) | **DONE** — committed `64963ad`, ledger §F15. Ran **FIRST**, before Task 9, per the round-7 correction. All five gates green; `apidiff` empty on all four packages; zero net-new uncovered blocks |
| **10** | The `expr` provider module | **DONE** — the eighth module ships with its own `go.mod` (`require` + `replace`), `go.work` `use` entry, and **three** CI edits (`expr` in both jobs, plus the pre-existing `adapter/cron/crontest` gap closed in both; `dir:` 6 → 8). **One** sentinel (`ErrInvalidExpression`), six providers, the **12** reinstated `*Expr` test functions under their new names, and **D-K's acceptance fixture** — a real `Consumer` + `RetryPolicy{MaxAttempts: 3, DeadLetter}` + `WithInvalidMessageSink` over a **re-emitting** source, so `dlq.count()==0` is load-bearing rather than vacuous. Root's `ErrPayloadType` godoc widened (**AC-10's fifth arm**, four `go doc` phrase gates RED → GREEN). **Fix round 1** closed a second vacuity of the same class in the CONSTRUCTION path (expr-lang echoes the offending source line in its *compile* error too, so `Contains(err.Error(), src)` passed with msgin's `%q` wrap deleted), and routed all three `msgin.PayloadOf` sites through one `payloadError` helper so a dead-letter record names WHICH expression rejected the payload. **Fix round 2** added **decision D-Q** (ADR 0029 §5.0d, Spec 014 §2.1 row 9): a `MessageGroupStore` whose `Add` returns a nil snapshot with a nil error is caller input, and it panicked **all four** release strategies; it is now rejected once at the choke point in `Handle` with `Permanent(ErrNilMessageGroup)`. That **supersedes** the `defaultRelease`-local guard of round 1, which closed only 1 of 4 and was removed. Root: **103** exported / **43** sentinels; `apidiff` **97 removals / 9 additions**, the ninth being `ErrNilMessageGroup`; `./routing` apidiff empty. `expr` and `routing` both at **100.0%** coverage, **37 mutants planted, 37 killed** — two of which caught vacuous assertions (`M5`, `M31`) and one of which (`M38`) proves each of the four release-strategy cases is necessary rather than redundant |
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
> **97 / 9** once D-I, D-J and D-Q land.

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

## Task 9 — Named behavior types and combinators · **M** · DONE (`544cb5b`)

> **EXECUTE TASK 9.7 FIRST** (round-7 D-M2/X-M2). Task 9.7 classifies the **shipped** producers; this task
> authors three **new** producers under the same rule. Running this task first creates, across three commits,
> exactly the half-classified tree Task 9.7's own rationale calls *"worse than either uniform answer"* —
> `Predicate.And(nil)` permanent while `transform.Transform(nil)` is not. Nothing in 9.7 depends on 9, 9.5 or
> 9.6: the combinators return a `Predicate`, never a `Step`, so they never call `nilFuncStep`. **The task
> numbers do not change** — they are cross-document links (ADR 0029 §5.0b, Spec §2.1, AC-5 all cite them by
> number); only the execution order is pinned. Record the order you actually ran in the ledger.

**Shipped already** (pulled forward with D-E, Task 1): `routing.CorrelationStrategy`
(`routing/aggregator.go:25`), `routing.ReleaseStrategy` (`:35`), `WithReleaseStrategy(ReleaseStrategy)`
(`:82`), `WithReleaseWhen(func(MessageGroup) bool)` (`:89`).

> **EXECUTED after Task 9.7 (`64963ad`), as pinned.** Execution record, with every transcript this task's
> Verify calls for: [`027-derivation-findings.md` §F16](027-derivation-findings.md). **Two plan defects found
> and corrected in place below** — the census arithmetic (§F16.2) and the `Not` fixture orientation that made
> the plan's own trap case vacuous until mutation testing caught it (§F16.4).

- [x] Declare the remaining four types and type their base constructors:
      ```go
      // package routing
      type Predicate[A any]    func(ctx context.Context, m msgin.Message[A]) (bool, error)
      type RouteFunc           func(ctx context.Context, m msgin.Message[any]) (msgin.MessageChannel, error)
      type SplitFunc[A, B any] func(ctx context.Context, m msgin.Message[A]) ([]msgin.Message[B], error)
      // package transform
      type Transformer[A, B any] func(ctx context.Context, m msgin.Message[A]) (msgin.Message[B], error)
      ```
- [x] Add `Predicate.And` / `Or` / `Not` — **with nil semantics specified, because the naive version panics.**

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
      (`endpoint/consumer.go:614`, `:733`).

      > **The sentinel census below is EVIDENCE, not this task's gate** (round-7 X-B2, counter-rule 7). D-M
      > wraps at the **producer** and deliberately leaves `IsPermanent`'s enumeration alone, so every row reads
      > identically before and after. Task 9.7 carries the gate that measures the observable D-M actually
      > moves — the producer path. Re-measured at `fe86a12`:
      >
      > ```
      > IsPermanent(msgin: nil endpoint function              ) = false
      > IsPermanent(msgin: no route for message               ) = false
      > IsPermanent(msgin: payload is not of the expected type) = true
      > IsPermanent(msgin: message has no correlation key     ) = false
      > IsPermanent(msgin: nil outbound sink                  ) = false
      > ```

      The in-tree precedent carries the identical rationale in its own godoc —
      `routing/aggregator.go:151-160` wraps `ErrNoCorrelation` in `msgin.Permanent` *"so the message would be
      retried to the dead-letter sink instead of diverted to the invalid-message channel"*. **`ErrNoRoute`
      stays transient and is NOT wrapped**: `routing/router.go:48-56`'s `pick` is caller-supplied and
      evaluated per message, so a message unroutable now may be routable after a config reload.

      **Wrap with the position, because the bare sentinel collapses every nil position into one string** —
      receiver and argument, across `And`, `Or` and `Not`, and across every shipped producer Task 9.7 fixes.
      *(Round-7 X-M4: this read "six nil sites", which is **five** in this task's frame — `Not` takes no
      argument, so `And`-arg, `And`-recv, `Or`-arg, `Or`-recv, `Not`-recv is the whole set. Stated as the
      invariant rather than re-typed as five.)* `msgin: nil endpoint function` says nothing about `And` vs `Or`
      vs `Not`, receiver vs argument, or which link of `p.And(q).Or(r)` failed — and CLAUDE.md requires
      *"typed, wrapping errors that name the offending field/input"*. The shape, **and the exact string it
      produces** — five tests are written against this text, so it is published rather than inferred
      (round-7 D-M1/X-M9; measured at `fe86a12`):

      ```go
      fmt.Errorf("%w: routing.Predicate.And: nil argument", msgin.Permanent(msgin.ErrNilFunc))
      ```
      ```
      "msgin: permanent: msgin: nil endpoint function: routing.Predicate.And: nil argument"
      errors.Is=true IsPermanent=true
      ```

      The doubled `msgin:` comes from `permanentError.Error()` (`reliability.go:13`) prefixing a sentinel whose
      own text is already `msgin:`-prefixed. It is a property of `Permanent` itself, present on every existing
      `Permanent(msgin.ErrX)` in the tree, and is **recorded, not repaired here** — changing that format is a
      separate decision touching every permanent error msgin produces. The context-first alternative
      (`"routing.Predicate.And: nil argument: msgin: permanent: msgin: nil endpoint function"`) was measured
      equally `errors.Is`/`IsPermanent`-clean and rejected only for consistency with the tree's existing
      cause-first wraps (`payload.go:15`, `endpoint/consumer.go:869`, `endpoint/producer.go:467`). See ADR 0029
      §5.0b.

      Each combinator's godoc states that **`errors.Is(err, msgin.ErrNilFunc)` still matches** and that
      `msgin.IsPermanent(err)` is true.

      **The error is returned at evaluation, not at construction** — combinators are pure and return a
      `Predicate`, not `(Predicate, error)`, so there is nowhere for a construction-time error to go. This is
      `nilFuncStep`'s shape (as amended by Task 9.7), and it reuses the **existing** `ErrNilFunc` rather than
      minting a sentinel, so a caller's `errors.Is` handling already covers it. **No short-circuit may skip
      the nil check**: `p.Or(nil)` must not silently return `(true, nil)` when `p` is true — a nil operand is
      a programming error and must surface even when the short-circuit would hide it. State this on each
      combinator's godoc.
- [x] Every type's godoc **names its Spring equivalent** — this is the mitigation that justifies dropping the
      Spring names (ADR 0029 §4), so verify it **per type**, not sampled. **This task owns Spec §8 obligation
      4 for the four types it creates** (`Predicate`, `RouteFunc`, `SplitFunc`, `Transformer`); the two shipped
      types are Task 11b's. → **gates 8.4c–8.4f, §11 block**, RED at this task's start and GREEN at its end.
      *(Round-8 C4, same class: the §11 pinning table already pinned these four to Task 9 while Task 9's Verify
      never ran them. An obligation is owned by the task that creates the symbol.)*
- [x] **Note for Task 12 (E-M8): this task changes the expected shape of Spec §5.0's census.** Measured on the
      untouched tree, `grep -rn "msgin\.MessageChannel\|MessageChannel interface" --include="*.go" . | grep -v
      "_test.go" | grep -v "^./docs" | grep -v '// '` → **16 lines**, of which **two** are the `Router.pick`
      **field** and the `NewRouter` **parameter**.

      > ⛔ **CORRECTED AT EXECUTION — the census is 15, and the 14 below was arithmetically unreachable**
      > (ledger [§F16.2](027-derivation-findings.md)). **Decision taken: retype BOTH** the field and the
      > parameter to `RouteFunc`. **Task 12 must expect 15**, re-measured with the command above:
      >
      > ```
      > 16    # BEFORE
      > 15    # AFTER — two lines removed, ONE ADDED
      > ```
      >
      > The plan predicted 14 by counting only removals. It missed that **the `RouteFunc` declaration is
      > itself a census line** — `type RouteFunc func(…) (msgin.MessageChannel, error)` — and necessarily so:
      > the type exists to return a `msgin.MessageChannel`, so no declaration of it can avoid the token the
      > census greps for. 16 − 2 + 1 = 15. The census counts **occurrences of the token**, not anonymous
      > signatures; naming a type **relocates** an occurrence rather than eliminating it. 14 is reachable only
      > by declaring `RouteFunc` outside the census's file set, which no layout in this plan does.
      >
      > *Also stale:* this checkbox cited the two lines as `routing/router.go:29` and `:37`; at `64963ad` they
      > were **`:35`** and **`:45`** (Task 9.7's D-M godoc paragraph shifted them). The two lines identified
      > are the right two — only the numbers drifted.
- [x] **`apidiff`: snapshot `./routing` and `./transform` BEFORE the edit, then diff.** The committed baseline
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

> ⚠️ **EXECUTION CORRECTION — EVERY error case needs its FIXTURE ORIENTATION pinned, or it is vacuous**
> (ledger [§F16.4](027-derivation-findings.md), [§F16.8](027-derivation-findings.md)). *"propagates an error"*
> does not constrain what the failing predicate returns **alongside** its error, and for each combinator one
> of the two choices makes the case pass against the very implementation it exists to reject. **Stated as the
> invariant, because pinning it per-instance is what let the defect through:** *the failing fixture must be
> oriented so that the naive implementation and the correct one disagree on the BOOL, then killed by
> mutation.* Per combinator:
>
> | Case | Naive implementation | Fixture that discriminates | Why the other orientation is vacuous |
> |---|---|---|---|
> | `Not` propagates | `return !ok, err` | **`(false, err)`** | with `(true, err)` the naive yields `(false, err)` — exactly what the case asserts |
> | `And`/`Or` propagate a **right**-side error | `return q(ctx, m)` (bare tail call) | **`(true, err)`** | with `(false, err)` the assertion checks False against a value that was already false |
> | `And`/`Or` propagate a **left**-side error | `ok` tested before `err` | **`(true, err)`** | with `(false, err)` a swallowed error still yields false and only `ErrorIs` fires |
>
> Same class as the `measure-interleaving-tests` lesson: a covering case is not a discriminating one.
> **Verify by mutation**, not by inspection. *(Round-9 review: the first execution pinned only the `Not` row
> and left the `And`/`Or` rows driven by the `Not` fixture, so both right-side cases shipped green and
> vacuous over a real defect — the bare `return q(ctx, m)` leaked the right operand's `true` alongside its
> error, contradicting these combinators' own godoc. Fixed in code, not in the godoc; see §F16.8.)*
**NEW in round 6 (D-M / D-B4):** a case asserting **`msgin.IsPermanent(err) == true`** on a combinator's nil
result, and — in the same case — that **`errors.Is(err, msgin.ErrNilFunc)` still matches** through both the
`msgin.Permanent` wrap and the positional `fmt.Errorf`. The classification *is* the behavior change; an
`errors.Is`-only assertion cannot see it, which is exactly how the bare sentinel survived unnoticed in the
shipped producers Task 9.7 now fixes.
**NEW in round 7 (X-M5) — the THIRD assertion, on the positional text, which this list omitted while Task 9.7
requires it.** Every nil case above asserts **all three** of `errors.Is(err, msgin.ErrNilFunc)`,
`msgin.IsPermanent(err) == true`, **and** that the message names its position — distinctly per case, so
`And`-argument, `And`-receiver, `Or`-argument, `Or`-receiver and `Not`-receiver each produce a different
string. Assert on the position substring (e.g. `routing.Predicate.And: nil argument`), **not** on the whole
error text: the full string embeds `permanentError`'s format, and pinning that here would make an unrelated
future change to `Permanent`'s rendering fail five tests in this package. Without this assertion the five
cases are indistinguishable, which is the debuggability defect the wrap exists to fix and is precisely what
the two-assertion form could not detect.

> **Do NOT re-verify "aggregator coverage returns to 100% on `NewAggregator` and `Handle`."** That criterion
> is void: **D-D deleted** the `NewAggregator` guard rather than rescuing it (F5, round-2 §B4), and the three
> `Handle`-side branches were already restored in Task 1 via D-E (F3). `routing` measures **100%** today.

**Verify:** existing tests compile unchanged with bare closures — that is the source-compatibility claim, so
demonstrate it rather than assume it (round-2 §E confirms bare closures still infer against named generic
func types on Go 1.25). `-coverpkg=./...` on both sides. **Plus gates 8.4c, 8.4d, 8.4e and 8.4f from the
[§11 canonical block](#11-gate-block--the-one-source-for-every-11b11c-gate-red-at-each-gates-own-tasks-start)
— RED before (the four types do not exist yet, so `go doc` reports `no symbol …`) and GREEN after, both
transcripts in the ledger.** These four gates are Plan-only (no Spec §8.0b counterpart), so run them from the
Plan's block:
```bash
eval "$(sed -n '/^# ==== CANONICAL GATE BLOCK/,/^```$/p' \
          docs/plans/027-core-package-layout.md | grep -v '^```')" | grep '8\.4[cdef]'
```

**Commit:** `feat(routing,transform): name the endpoint behavior types and add combinators`

```
Spec: 014
Plan: 027
ADR: 0029
RFC: 0002
RFC: 0003
```

*(Round-7 X-M12: only Tasks 9.6 and 9.7 carried a trailer block; the other five tasks left the worker to
reconstruct the footer from Global Constraint 7. The ADR set is per task — this one realizes ADR 0029's
behavior-type naming and combinators, whose own header declares RFC 0002 + 0003.)*

---

## Task 9.5 — Residual cleanups the migration left behind · **M** · DONE (`910e092`)

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

- [x] **Decided: B.** `ErrInvalidExpression` and `ErrExprResultType` are
      **deleted from root** (done; line numbers struck rather than refreshed — see the deletion bullet below,
      where all three documents published a different and wrong pair); the `expr` module declares
      **`expr.ErrInvalidExpression` only**, with the
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

      > **THIS TABLE STOPS AT D-J — it is NOT the branch end state.** Task 10's fix round 2 adds **D-Q**
      > (`ErrNilMessageGroup`), taking root to **103 exported / 43 sentinels / 97 removals / 9 additions**.
      > The running total lives in Task 12's table and Spec 014 §4; this one is scoped to D-I + D-J.

      > **MEASURED AT EXECUTION (`b4d1a1a` → this task's tree). ALL FOUR D-I PROJECTIONS HELD EXACTLY.**
      > The BEFORE row was re-measured at `b4d1a1a` first, because Tasks 9.7 and 9 landed after `dadc775`
      > and the table could not be assumed still valid: it was **unchanged** — 102 / 43 / 95 / 6 — confirming
      > that neither task added a root exported symbol (Task 9's four behavior types are in `routing` and
      > `transform`). AFTER the two deletions: **100 exported / 41 sentinels / 97 removals / 6 additions**,
      > with the two new `apidiff` lines being exactly `- ErrExprResultType: removed` and
      > `- ErrInvalidExpression: removed`. The `After D-J` row remains a projection until Task 9.6 lands.
- [x] **Deletion is CODE, and it belongs to this task's commit** — remove both `var` blocks from `errors.go`
      (each with the godoc block above it). **Delete both godoc blocks outright — do NOT copy either
      one forward.** Task 10 writes `expr.ErrInvalidExpression`'s godoc **fresh**, from the three-point
      content spec in its own checkbox, and the reason is not stylistic: the root text names
      `ErrExprResultType` (a sentinel revised D-K abolishes, and one Spec AC-10 arm 2 requires to be empty
      **workspace-wide**, `errors.go` included) and closes with *"It is exported **here, not in the
      provider**"*, the exact premise D-I reversed. `ErrExprResultType`'s godoc has no destination at all:
      the `expr` module returns `msgin.ErrPayloadType` rather than minting a replacement, so there is no
      declaration for that comment to sit above.

      > **EVERY LINE NUMBER THIS TASK PUBLISHED FOR THE TWO BLOCKS WAS STALE, IN ALL THREE DOCUMENTS.**
      > At execution (`b4d1a1a`) the real positions were `ErrInvalidExpression` godoc `errors.go:196-207`,
      > declaration `:208`; `ErrExprResultType` godoc `:223-235`, declaration `:236`. This task published
      > `:168`/`:180` and `:193`/`:206`; Spec 014 §3.2 (L522-523) published `:168` and `:193`; ADR 0029
      > (L1081-1082) published `:180` and `:206`. Three documents, three different pairs, none of them right
      > — the blocks had drifted ~28 lines since they were measured. The task text already warned to locate
      > by symbol rather than by line, which is what made this harmless; the numbers are struck rather than
      > refreshed, because a deleted declaration has no line number worth carrying.

      > **ROUND-7 CORRECTION (X-B5, this task's half).** This bullet read *"Copy `ErrInvalidExpression`'s
      > godoc (`:168-180`) out to Task 10 first"*, and Task 10 read *"recover it from
      > `git show 3d0b87a:errors.go`, lines 168–180"*. **Two documents, one instruction, and executing it
      > breaks the delivered tree** — see Task 10's correction block, which pastes the thirteen lines the
      > command actually emits. The *content* still crosses to Task 10; the *text* does not.

      > **ROUND-4 CORRECTION (B3).** This bullet previously claimed the godoc being deleted *"is where 3 of
      > arm 2's 7 staleness survivors live (`errors.go:175,176,177`)"* and told the implementer those three
      > *"must disappear without a separate edit"*. **False on both counts**: arm 2 has no survivors in
      > `errors.go`, and lines 175–177 contain no matched token — they are ordinary sentinel godoc. An
      > implementer would delete the blocks, re-run the sweep, observe no delta, and reasonably conclude the
      > gate was broken. The deletion is still correct; only the justification was invented.
- [x] **Propagate in the same commit:** the ledger; Spec 014 §3.2 / §4 / §4.1 / §7 (**done in the doc pass**);
      Task 10's provider set and its `RouteFunc`, whose **two construction validations wrap
      `ErrInvalidExpression`** and must now wrap the module's own; Task 12's assertion numbers.

      > **VERIFIED AT EXECUTION, NOT ASSUMED.** All four downstream sites already carried D-I correctly and
      > needed **no edit**: Spec 014 §3.2 (L522-531), §4's arithmetic row (L1091), §4.1's fifth-class note
      > (L1177-1191) and §7 (L1740-1818, incl. the `msgin/expr:` prefix and the "deleted, not aliased" rule);
      > Task 10's `expr/errors.go` checkbox (L2199-2206) and its `RouteFunc` two-construction-validations
      > bullet (L2197-2198); Task 12's projection table (L3011-3024) and its `apidiff` fifth-class bullet
      > (L3048-3051). Spec §4/§4.1's D-I numbers are deliberately LEFT as projections: the end state they
      > describe is post-D-J (102/42), which Task 9.6 has not landed yet, and Task 12 owns that measurement.

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
- [x] **Run the two-arm staleness sweep to empty** (Spec 014 §8.1, **arm 2 redesigned in round 4**).
      Measured at `dadc775`, by running both arms:
      ```
      ARM 1 (moved symbols still qualified msgin.X) — 2 survivors:
            codec.go:33, routing/aggregator_test.go:21
      ARM 2 (names in comments that are declared nowhere) — 1 survivor:
            routing/aggregator.go:316   // "the WithRelease strategy failed"  → WithReleaseStrategy
      ```
      Regenerate `docs/plans/027-tools/symmap.tsv` before running arm 1 — it is derived and it went stale by
      one entry (`channel.WithSingleSubscriber`) between `c83dde9` and `b6ce7bb`.

      > **RUN AT EXECUTION (`b4d1a1a`). Both arms are now EMPTY.** `symmap.tsv` was **stale by four more
      > entries**, not the one this checkbox names: Task 9 added `routing.Predicate`, `routing.RouteFunc`,
      > `routing.SplitFunc` and `transform.Transformer`, taking it **91 → 95**. The three survivors were all
      > real and all still present, at shifted lines:
      > ```
      > ARM 1  codec.go:33                  msgin.NewProducer / msgin.WithProducerCodec  -> endpoint.*
      >        routing/aggregator_test.go:21  "*msgin.DirectChannel"                     -> *channel.DirectChannel
      > ARM 2  routing/aggregator.go:366    "the WithRelease strategy failed"            -> WithReleaseStrategy fn
      > ```
      > Note arm 2's line: **366, not the 316 published here and in Spec §8.1** — the file grew 50 lines since
      > the measurement, which is exactly why the checkbox says to run the command rather than trust the list.
      > Both arms were then **probed for vacuity**, since a gate that reports empty because it matches nothing
      > is the failure mode round 4 found: planting `// Probe: msgin.NewQueueChannel …` made arm 1 report one
      > line, and planting `// Probe: WithNonexistentOption and ErrNeverDeclared …` made arm 2 report two.
      > Both probes were reverted.

      > **ROUND-4 CORRECTION (B4 / exec-B1).** This checkbox previously published **"arm 2 has 7 survivors"**
      > at seven named lines. **Arm 2's published command returns zero hits and always did** — it was a
      > hardcoded list of the six `*Expr` names, none of which survives anywhere. All seven named lines hold
      > unrelated live text. Spec §8.1 now defines arm 2 as an **invariant** (every name a comment mentions is
      > a name that exists) rather than a list of deleted names, which is what surfaces `WithRelease` — a name
      > that never existed at all, and therefore one that **no deleted-symbol enumeration could ever contain**.
      > Run the command; do not trust this list either.
- [x] **Extend the capability test to ALL EIGHT send-only positions.** `capability_test.go`'s
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

      > **THERE IS NO RED FOR THIS CHECKBOX, AND THAT IS CORRECT — round-7 X-M10.** Do not hunt for one.
      > The widening these five subtests assert **already landed in `b6ce7bb`**, which segregated
      > `MessageChannel` down to send-only:
      > ```
      > $ git show b6ce7bb -- channel.go | grep -E '^-' | grep Subscribe
      > -	Subscribe(h MessageHandler) error
      > $ go doc github.com/kartaladev/msgin.MessageChannel | sed -n '3,5p'
      > type MessageChannel interface {
      > 	Send(ctx context.Context, msg Message[any]) error
      > }
      > ```
      > All five sites already take `msgin.MessageChannel` — `routing/router.go:29,37`,
      > `routing/aggregator.go:55,133`, `adapter/http/inbound.go:116`,
      > `adapter/http/stdlib/inbound.go:33` — so every new subtest **passes the first time it compiles**.
      > These cases are a **regression fence around ADR 0028's widening**, not a red-to-green proof of new
      > behavior. Their failure mode is a future narrowing, and a test that has never been red is exactly
      > what catches that. TDD's red step is satisfied for this checkbox by *deleting* the widening locally
      > and watching the case fail, if a worker wants the evidence; it is not a precondition for the commit.

      > **DONE AT EXECUTION — 24 subtests, counted, not projected.** 3 targets × 6 core sites = **18** in
      > root's `capability_test.go`, plus **3** in the new `adapter/http/capability_test.go`
      > (`TestServeAsyncTargetAcceptsEveryChannel`) and **3** in the new
      > `adapter/http/stdlib/capability_test.go` (`TestNewInboundTargetAcceptsEveryChannel`). The two HTTP
      > files each carry their own copy of the three target fixtures — unavoidable, since all three test
      > packages are **blackbox** (`package X_test`) and there is no shared test-helper package.
      >
      > **The row-5 trap was handled and then PROVEN handled.** A `NoError` assertion on the expired-group
      > path is vacuous on its own (`Handle` holds the member and returns nil; the reaper delivers later, on
      > `Run`'s goroutine), so the load-bearing assertion is the harness's own delivery check. To prove that
      > check is real rather than assumed, all three new core sites were **mutation-probed**: each was
      > re-pointed at a decoy `channel.NewPublishSubscribeChannel()` instead of `target`, and **9 of 9**
      > subtests (3 targets × 3 new sites) failed, row 5 included. Mutation reverted.
      >
      > Row 5's `Run` goroutine is joined deterministically via a `t.Cleanup` registered **before** the clock
      > tick, so the reaper is joined even when the delivery assertion fails; root's package-level
      > `goleak.VerifyTestMain` (`main_test.go`) is the leak gate. Row 3 uses a bare closure literal, which
      > is assignable to Task 9's named `routing.RouteFunc` (only a caller's OWN named func type is not —
      > see `RouteFunc`'s ASSIGNABILITY godoc).

**Verify:** the sentinel decision recorded in the ledger with its three downstream numbers propagated; both
sweep arms empty; the capability test covers **3 targets × 6 core sites** in `capability_test.go` **plus 3 × 2
HTTP sites** in the two adapter packages — 24 subtests total, not 9; the **seven**-module `GOWORK=off` loop
green (not eight — `expr` does not exist until Task 10);
`-coverpkg=./...` measured against a **named** tree (Global Constraint 0).

**Commit:** `refactor(core,http)!: move the expr sentinels out of root, clear the staleness sweep, widen the capability test`

```
Spec: 014
Plan: 027
ADR: 0029
RFC: 0002
RFC: 0003
```

*(Round-7 X-M12. ADR 0029 §5.0a is D-I — the expr sentinels leaving root — which is this task's headline
edit.)*

> **ROUND-6 CORRECTION (E-M4) — two defects in one subject line.** It read
> `refactor(core): delete dead root helpers, clear the staleness sweep, widen the capability test`.
> (1) The **dead-helper deletion is already `[x] DONE`** in the round-3 pass (§9.5.1, F12.4, committed in
> `1d7fc80`), so the subject named work this commit does not contain and omitted the work it does — D-I's
> removal of `ErrInvalidExpression` and `ErrExprResultType` from root. (2) It carried **no `!`** while
> removing two exported symbols from the closed root contract (`apidiff` 95 → 97 removals, §9.5.0's table),
> where the *smaller* break in Task 9.6 is correctly typed `feat(core,channel,endpoint)!`. Conventional
> Commits' `!` is the machine-readable breaking-change marker and this is a breaking change.
>
> **ROUND-7 CORRECTION (X-M3) — a third defect: the scope was `(core)` alone.** Two of this task's own
> capability-widening sites are **`adapter/http/inbound.go:116`** and **`adapter/http/stdlib/inbound.go:33`**,
> and the checkbox above says in terms that their cases *"belong in those packages' tests, not in root's
> `capability_test.go`"* — so the commit touches two modules' worth of files under a scope naming one.
> Corrected to `refactor(core,http)!`; `http` is the scope this repo already uses for `adapter/http`
> (`git log --oneline` → `feat(http): SSE client hardening …`, `docs(http): SSE client example …`).

---

## Task 9.6 — Reply-channel exclusivity probe (decision D-J) · **M** · DONE

> **RE-SIZED `S` → `M` in round 7 (X-M1). Do NOT split it** — `Task 9.6` is a cross-document link
> (ADR 0030, Spec 014 §5.1/§8, Plan §11's gate groups), and renumbering breaks joins the last two rounds were
> spent repairing. The `S` label was written when this task was "add one interface and one guard". It now
> ships, in one commit: **2 root exported symbols** (`ExclusiveSubscribable`, `ErrSharedReplyChannel`) whose
> godoc is **normative text copied verbatim** from ADR 0030 §1 and phrase-gated by Task 11b; **2 `channel`
> methods** plus their compile-time assertions and a `channel_test` table; an **`endpoint` option**
> (`WithSharedReplyChannel`) with its config field and the constructor guard; a **full `NewChannelExchange`
> godoc rewrite** to four outcomes; **two test fakes**; a **truth table**; a test-prose rewrite; **both
> coverage arms**; and — added in round 7 by **D-O** — the `safeSingleSubscriber` recover helper with its
> **sixth** truth-table row. That is `M` work by every other task's yardstick in this plan.

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

- [x] **Root — `channel.go`:** add `ExclusiveSubscribable` (embedding `SubscribableChannel`, one method
      `SingleSubscriber() bool`). **Its godoc is the VERBATIM normative text of
      [ADR 0030 §1](../adrs/0030-reply-channel-exclusivity-probe.md) — copy it, do not paraphrase.** Add the
      `channel.WithSingleSubscriber` cross-reference.

      > **ROUND-7 CORRECTION (R-B1 / D-B5 / X-B1).** This checkbox previously read *"Godoc must state it is a
      > report about **this channel in this process**, not a distributed guarantee"*. That is D-L's
      > **superseded handle-local** wording, and it was the last live use of it in the bundle — ADR 0030 §1
      > carries the same phrase only inside a labelled `SUPERSEDED IN PLACE` block. The round-6 pass rewrote
      > the ADR and the spec and never opened **the one checkbox that actually writes the text**.
      >
      > It is not a paraphrase difference. Under **D-L as revised in round 7** the predicate counts
      > **recipients reached, not processes traversed**: a per-instance NATS `_INBOX` reply subject — the
      > canonical Return Address channel — answers `true`, while a broadcast subject answers `false`. The
      > withdrawn wording ("this channel in this process") answers `true` for **both**.
      >
      > **Gate 8.11 is a seven-conjunct phrase match against ADR 0030 §1's exact wording** (Spec §8.0b ≡ the
      > §11 canonical block), so a paraphrase fails it with no diagnosis. **Line breaks no longer matter** —
      > round 8 (C3) adopted the `d` normalizer, which folds `go doc`'s output to one line before matching, so
      > a phrase may span a break. **Wording still matters exactly.** Copy, do not retype.
- [x] **Root — `errors.go`:** add `ErrSharedReplyChannel`. Godoc names both remedies
      (`channel.WithSingleSubscriber()` on the channel, or `endpoint.WithSharedReplyChannel()` on the
      exchange) and states the consequence being prevented — a full copy of every reply reaching another
      subscriber. **Do NOT reuse `ErrChannelSubscribed`**: it would report "already subscribed" for a channel
      that has no subscriber (ADR 0030 Consequences).

      **It must also name the THIRD cause — decision D-O2 (round 8, B1):** the channel's `SingleSubscriber`
      **panicked**, was recovered, and the exchange failed closed. That case is *not* a policy report — the
      channel may well be exclusive — and the sentinel's message says the opposite, so the godoc has to send
      the reader to `err.Error()` for the wrapped panic rather than off hunting for a second subscriber
      (CLAUDE.md's debuggability criterion). **Copy the godoc from
      [ADR 0030 §3](../adrs/0030-reply-channel-exclusivity-probe.md)**, which carries the decided text.
- [x] **`channel` — two methods:** `(*DirectChannel).SingleSubscriber() → true`;
      `(*PublishSubscribeChannel).SingleSubscriber() → c.cfg.single`. Add the compile-time assertions
      (`var _ msgin.ExclusiveSubscribable = (*DirectChannel)(nil)`, same for pub-sub) next to the existing
      `_ msgin.SubscribableChannel` ones at `direct.go:29` / `pubsub.go:112`.
- [x] **`channel`-package tests for both methods — REQUIRED, and not covered by the four-arm table.** A
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
- [x] **`endpoint` — the guard and the opt-out:** `WithSharedReplyChannel()` sets `cfg.allowShared`; the probe
      runs in `NewChannelExchange` **before `reply.Subscribe`**, so a rejected exchange leaves no subscription
      behind. Order relative to the existing `ErrNilChannel` and `ErrInvalidReplyTimeout` checks: after both
      (a nil channel cannot be probed).
- [x] **Rewrite `NewChannelExchange`'s reply godoc — THIS TASK owns the final wording (round-8 C4).** It
      currently says exclusivity "is documented rather than enforced here" (`endpoint/exchange.go:216`) — that
      sentence becomes false. The content spec is **Spec §8 obligation 12**, and **gate 8.12 (§11 block) must
      be GREEN before this task commits**; Task 11b re-runs it as a no-regression check and does not write it.
      *(This checkbox used to say "let Task 11b own the final wording", which is unexecutable: this task
      declares the symbols and cannot commit a green unit that leaves their godoc unwritten.)*
      **State FOUR outcomes:** rejected when the channel reports non-exclusive; accepted when it
      reports exclusive; **accepted when the channel does not implement the probe at all** (the one a reader
      will otherwise assume away); and **accepted on the channel's own word, which the core cannot verify** —
      under **D-L as REVISED in round 7** a channel must report `false` whenever a message sent to it can reach
      any recipient other than its single registered subscriber, *including one in another process*; a
      broker-backed channel may report `true` **only** when the broker guarantees the destination is private to
      this process's subscription (a per-instance NATS `_INBOX`, an exclusive auto-delete AMQP reply queue).
      The core takes that answer on trust.

      > **ROUND-8 CORRECTION (join check).** This read *"accepted but exclusive only within this process — a
      > channel whose deliveries **reach other processes** must report `false` (D-L)"*. That is D-L's
      > **superseded** round-6 wording, which measured *processes traversed*; revised D-L measures *recipients
      > reached*. Under the old form a per-instance NATS `_INBOX` — the canonical Return Address channel —
      > must report `false` and is rejected by default, which is the exact defect the revision exists to
      > remove. Found mechanically by `docs/plans/027-tools/joincheck.py` plus a withdrawn-phrase grep, not by
      > inspection.

      > **ROUND-6 CORRECTION (D-M1).** This said *"State the **three** arms"*, while ADR 0030 `:230-233` and
      > Spec `:1691` both require **four** outcomes and Spec AC-9 `:1881` says *"all three acceptance
      > outcomes"* — i.e. three **accept** arms plus the reject arm. An implementer writing three would then
      > have gate 8.12 fail at the end of this very task. **Authority: Spec §8 obligation 12 (four). Final
      > wording: THIS task** *(round-8 C4 — this line used to say "Final wording: Task 11b", which contradicted
      > the same task's own gate)*. The four-arm truth table below is the *guard's* branch table, which
      > is a different four — the godoc's fourth outcome is a scope caveat on an accepted arm, not a fifth
      > branch.

      > **NOTE — the option godoc (§8.13) and the guard order (D-M2).** Spec §8 obligation 13 requires
      > `WithSharedReplyChannel`'s godoc to say it **suppresses the probe**. That is only true if
      > `cfg.allowShared` is tested **first**: write
      > `if !cfg.allowShared { if ex, ok := reply.(msgin.ExclusiveSubscribable); ok && !ex.SingleSubscriber() { … } }`,
      > so the opt-out never calls a third-party `SingleSubscriber` at all. The reverse order (probe first,
      > then consult the flag) suppresses the *rejection* while still paying for the probe — and would make the
      > §8.13 godoc false. *(Round-6 D-M2; ADR 0030 and Spec §5.1 are being corrected to match.)*

**Hot-path branches — five arms, a truth table, one case each** (fold into one `table-test`):

| probe implemented | `SingleSubscriber()` | `WithSharedReplyChannel()` | result |
|---|---|---|---|
| no | — | — | accepted |
| yes | `true` | — | accepted |
| yes | `false` | no | **`ErrSharedReplyChannel`** |
| yes | `false` | yes | accepted |
| yes | **panics** | no | **`ErrSharedReplyChannel` wrapping the recovered value** (fail closed — **D-O**/**D-O2**) |

- [x] **`safeSingleSubscriber` — the probe is caller code called inside a constructor (decision D-O, amended
      by D-O2).** Wrap every call, and **return the recovered value as an error so the guard can wrap it**:

      ```go
      func safeSingleSubscriber(ex msgin.ExclusiveSubscribable, log *slog.Logger) (b bool, cause error) {
      	defer func() {
      		if r := recover(); r != nil {
      			// fail closed: a probe that panicked has not proven exclusivity …
      			b, cause = false, fmt.Errorf("SingleSubscriber panicked: %v", r)
      			// … and log at WARN with the recovered value (redundant with cause, by design)
      		}
      	}()
      	return ex.SingleSubscriber(), nil
      }
      ```

      and, in `NewChannelExchange`, wrap `cause` into the sentinel so `errors.Is` is unchanged **and** the
      diagnosis survives:

      ```go
      if !cfg.allowShared {
      	if ex, ok := reply.(msgin.ExclusiveSubscribable); ok {
      		single, cause := safeSingleSubscriber(ex, cfg.logger)
      		if !single {
      			if cause != nil {
      				return nil, fmt.Errorf("%w: %w", msgin.ErrSharedReplyChannel, cause)
      			}
      			return nil, msgin.ErrSharedReplyChannel
      		}
      	}
      }
      ```

      > **ROUND-8 CORRECTION (design B1) — D-O2. The round-7 form destroyed the evidence of the fault it
      > recovers from and reported a false diagnosis.** Compile-proven at `7ee3fd6` with the round-7 helper
      > implemented exactly as it was written and a **genuinely exclusive** channel (embedding
      > `*channel.DirectChannel`) whose `SingleSubscriber` panics:
      >
      > ```
      > err                                      = "msgin: reply channel permits multiple subscribers; it is not exclusive to this exchange"
      > errors.Is(err, ErrSharedReplyChannel)    = true
      > panic value recoverable from err         = false
      > anything on stderr/stdout from the logger= (nothing: cfg.logger defaults to io.Discard)
      > ```
      >
      > With the wrap above, same worktree, same channel:
      >
      > ```
      > err                                      = "msgin: reply channel permits multiple subscribers; it is not exclusive to this exchange: SingleSubscriber panicked: probe: nil map read in tenantExclusivity[tenant]"
      > errors.Is(err, ErrSharedReplyChannel)    = true
      > panic value recoverable from err         = true
      > ```
      >
      > **Fail-closed unchanged; no new sentinel; the WARN stays; NO GATE MOVES** — the helper is unexported,
      > so no `go doc` gate can see its signature, and §8.10/8.11/8.11a/8.12/8.13 were run against **both**
      > implementations with ADR 0030 §1's godoc pasted verbatim and printed `GREEN` five-for-five under each.
      > The rule this restores is written down in-repo, at `endpoint/poller.go:100-105`: *"safePoll does NOT
      > log — pollLoop's existing error path already logs … with this error, **whose text carries the recovered
      > panic value**"*. Eight of eight recover-wrappers that return an error embed `%v` of the recovered value
      > (`consumer.go:863,885,909,921,935`, `poller.go:109`, `producer.go:563`, `channel/pubsub.go:203`); the
      > ninth, `safeLimiterWait` (`consumer.go:514`), sets `err = nil` deliberately (fail *open*) and has no
      > error to carry anything. **Caveat for the test:** `errors.Unwrap` returns `nil` on a two-verb `%w`
      > wrap — assert with `errors.Is` / `errors.As` / `err.Error()`, never `errors.Unwrap`.

      **Fail closed, not open.** A panicking probe has proven nothing, and CLAUDE.md's sensible-defaults rule
      says to pick the value that fails safe when a wrong default could silently corrupt — here, a full copy of
      every reply reaching another exchange's unmatched-reply sink. Returning `true` on panic would accept the
      channel the probe exists to reject.

      > **Why this is not optional.** Compile-proven in round 7: a panicking probe **escapes
      > `NewChannelExchange`** and a blocking one **hangs it**, with `goleak` catching the stuck goroutine on
      > top of `endpoint.NewChannelExchange`. Four authorities in this repo already forbade the pattern — most
      > directly **`ErrUnboundedRetry`'s own godoc**, which says its check is *"deliberately **STRUCTURAL** …
      > because `BackoffStrategy` is a public interface and **calling caller code inside a constructor may
      > panic, may block**, and is non-deterministic"* — and `endpoint/consumer.go` already carries **eleven**
      > `safeX` recover-wrappers for exactly this class. D-J introduced a twelfth call site outside it.
      >
      > **Blocking cannot be defended against** (there is no context and no timeout at construction), so it is
      > a stated MUST in the godoc, not a guard. See ADR 0030 §1 part (e).

      **The sixth row above is its covering case**: a test-local `ExclusiveSubscribable` whose
      `SingleSubscriber()` panics, asserted to yield `ErrSharedReplyChannel` — *not* a propagated panic. Assert
      with `require.NotPanics` around the constructor, since the pre-D-O behavior is a panic escaping it.

      **Row 6 must ALSO assert the panic text is in `err.Error()` — `errors.Is` alone is not enough (D-O2).**
      Panic with a distinctive literal from the fake and assert `require.ErrorContains(t, err, thatLiteral)`
      alongside `require.ErrorIs(t, err, msgin.ErrSharedReplyChannel)`. **A sentinel-only assertion passes
      against the diagnosis-losing round-7 implementation**, which is exactly how that defect would have
      shipped green; the substring assertion is the only thing that pins D-O2. Do **not** assert through
      `errors.Unwrap` — it returns `nil` on a two-verb `%w` wrap.

      **Make the fake GENUINELY EXCLUSIVE**, e.g. `struct{ *channel.DirectChannel }` with its own panicking
      `SingleSubscriber`. That is the shape that exposes the false diagnosis (the error claims non-exclusivity
      about a channel whose second `Subscribe` returns `ErrChannelSubscribed`), and it costs nothing over a
      bare fake.

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

- [x] **A FIFTH row, and a SECOND fake: the rejected arm leaves no subscription behind.** Spec AC-9 requires
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

- [x] **Update the test's own PROSE, not just its constructions — neither sweep arm can see prose of this
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

- [x] **Fix the test D-J breaks — this is a required edit, not a discovery.**
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
      > outside the rows of Spec §2.1's table, D-J being row 5; see the goal statement above and E-B6.
      > *Round 7: the cardinality word that stood here has been removed rather than re-typed — cite the table.*)
- [x] **The blast radius was swept, not assumed — it is ONE TEST, TWO CONSTRUCTIONS.** Measured across the
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
- **The table shows SEVEN distinct subtests — this is a FLOOR, not a cap** — the four truth-table arms,
  **plus** AC-9's ordering row (`countingSharedChannel`, asserting `n == 0` after a rejected construction)
  **plus** D-O's panicking-probe row (asserting `ErrSharedReplyChannel` **and** the panic literal in
  `err.Error()`) **plus** **D-M2's ordering row** — **and** the `channel`-package test above pins
  `SingleSubscriber()` for both types directly.
  *(Round-8 C6: this said *"the four-arm table shows four distinct subtests"*, while the task body above
  requires six and Spec AC-9 requires the same six. A worker satisfying the Verify as written ships without
  the ordering assertion **and** without D-O's covering case — the two rows added precisely because nothing
  else observes them.)*

  > **EXECUTION FINDING I-1 (2026-08-10, Task 9.6 code review) — the SEVENTH row, and why "six" was a floor.**
  > **D-M2's ordering was documented normatively and observed by no test.** `WithSharedReplyChannel`'s godoc
  > states it as a caller-facing guarantee (*"NewChannelExchange consults this flag BEFORE the
  > `msgin.ExclusiveSubscribable` type assertion, so a construction carrying this option never calls the
  > channel's `SingleSubscriber` at all"*), and gate 8.13 only greps for *"suppress"*. **Measured:** restoring
  > the pre-D-M2 shape the plan explicitly rejects —
  > `if ex, ok := …; ok { single, cause := safe…; if !single && !cfg.allowShared { … } }` — leaves **rows 1–6
  > all PASS** and gate 8.13 GREEN, so the regression ships silently. The caller it hurts is exactly D-M2's
  > population: someone who opted out *because* their third-party `SingleSubscriber` locks or does I/O now pays
  > for it on every construction, and a panicking probe emits a spurious WARN.
  >
  > **Row 7:** a SECOND `countingSharedChannel` instance (row 5 keeps its own) gains a `probes atomic.Int64`
  > incremented in `SingleSubscriber`; the row constructs with `endpoint.WithSharedReplyChannel()` and asserts
  > `NoError` **and** `probes == 0` **and** `subscribes == 1`. Row 4 deliberately keeps a real
  > `*channel.PublishSubscribeChannel`, so it stays a truth-table arm rather than a fake-only assertion.
  > Mutation-killed by the shape above; **that mutant is not the same as flipping the option to a no-op**,
  > which changes *whether* the flag is honored rather than *when* it is read.
- **THE FIVE `go doc` GATES FOR THIS TASK'S OWN GODOC — 8.10, 8.11, 8.11a, 8.12, 8.13, run from the
  [§11 canonical block](#11-gate-block--the-one-source-for-every-11b11c-gate-red-at-each-gates-own-tasks-start),
  RED before and GREEN after, both transcripts in the ledger.** This task **declares all three symbols and
  rewrites `NewChannelExchange`'s godoc**, so Spec §8 obligations 10–13 are its acceptance criteria, not Task
  11's (Spec §8's owner table; round-8 C4). Fourteen conjuncts in total; obligation 11 alone is seven.
  ```bash
  # Run the canonical block straight out of the Spec — no retyping, no second copy.
  # Before this task's edit: 8.10 8.11 8.11a 8.12 8.13 RED, 11c1 RED.
  # After:                   8.10 8.11 8.11a 8.12 8.13 GREEN, 11c1 STILL RED (Task 11c's).
  eval "$(sed -n '/^# ==== CANONICAL GATE BLOCK/,/^```$/p' \
            docs/specs/014-core-package-layout.md | grep -v '^```')"
  ```
  *(Round-8 C4, the structural cause of the four-way ownership contradiction: this Verify had **no `go doc`
  gate at all**, so nothing measured the two root symbols or the normative godoc this task is the sole writer
  of. Every other document then had to guess who owned them.)*
- **Gate 11c1 is NOT this task's** — `channel.WithSingleSubscriber`'s single-process clause is Task 11c's, and
  it is expected to stay **RED** through this task. Do not "fix" it here.
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

## Task 9.7 — Classify the shipped deterministic endpoint faults as `Permanent`, and stop discarding them (decisions D-M, D-N, D-P) · **M** · DONE (`64963ad`); execution record and every gate transcript in ledger §F15

> **RE-SIZED S → M in the round-7 pass (X-M1).** The `S` label covered "wrap four producers". It does not cover
> the round-7 scope: **five** producers (D-B1 adds root's `handler.go:55`), the **D-N** fallback in
> `endpoint/consumer.go` at two call sites plus a new accessor and a WARN, **15** godoc lines including two
> sentinel godocs and the deliberate-exclusion note, `nilFuncStep`'s **signature** change in three packages
> rippling to five call sites, and ~12 test cases across four packages. The task **number** is unchanged.

> **NEW in the round-6 pass.** Task 9's combinators adopt `msgin.Permanent(msgin.ErrNilFunc)` because D-M says
> so — but D-M is a **rule about a class**, not about the three new combinators, and the class already has
> **four shipped members**. Fixing only the new code would ship a library where `Predicate.And(nil)` is
> permanent and `transform.Transform(nil)` is not: the same fault, the same cause, two different delivery
> outcomes. That is worse than either uniform answer.
>
> **Why it is its own task — and where it actually runs.** ⚠️ **Read the ROUND-7 CORRECTION three paragraphs
> down before acting on this one: the ordering below is SUPERSEDED.** The three reasons argue that 9.7 must be
> a **separate commit** from Task 9, which still holds; the *position* they originally concluded ("after 9.6,
> before 10") does not — round 7 moved it to **first, before Task 9**. The reasons are kept because reason 2
> and reason 3 are still live constraints on where it may **not** go. *(Round-8, gate minor: this header said
> both "placed HERE (after 9.6, before 10)" and "this task RUNS FIRST" ninety lines apart, with no marker on
> the first.)* Three reasons, stated because the placement is a judgement call:
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
> **ROUND-7 CORRECTION (D-M2 / X-M2) — this task RUNS FIRST, before Task 9.** The paragraph above argues that
> a half-classified tree is *"worse than either uniform answer"* and the ordering that followed it created
> exactly that state across three commits. Nothing in 9.7 depends on 9, 9.5 or 9.6 — the combinators return a
> `Predicate`, never a `Step`, so they never call `nilFuncStep` — so running 9.7 first is free and makes Task
> 9's new producers land into an already-uniform tree. **The task NUMBER is unchanged** (ADR 0029 §5.0b, Spec
> §2.1 row 6 and Spec AC-5 all cite it by number; renumbering is a coordinated three-document edit for no
> gain). Record the order you actually ran in the ledger.
>
> **This task also carries decisions D-N and D-P** (Spec §2.1 row 7) — the `divert` dead-letter fallback and
> the rule that **the fallback is single-shot** — **in the same commit**. Splitting them would leave a tree,
> between commits, in which a mis-wired step's message is silently dropped where it was previously captured
> durably (without D-N), or spins through redelivery forever when the fallback sink is down (without D-P).
>
> **ROUND-8 ADDITION (A5) — it also carries the PRODUCER-side consequence** (Spec §2.1 **row 8**). D-M's blast
> radius had only ever been measured on the consumer; on the producer the same reclassification **removes** a
> durable capture and flips an exported error contract. No code changes for it — the behavior is correct as it
> stands — but it needs its gate (**gate 3**), its covering case, and its register row, so it does not ride in
> silently.

**Skills:** start from `cc-skills-golang:golang-how-to`; TDD via `superpowers:test-driven-development`;
`gopls` for navigation; `table-test` for the branch table; blackbox `_test` packages only.

**RED FIRST — the baseline this task must invert.** Before editing anything, drop a throwaway
`package msgin_test` file at the repo root, run it, and paste the output into the ledger. Delete the throwaway
before committing; the ledger keeps the transcript. Measured at `fe86a12` (root `.go` byte-identical to the
`dadc775` code pin).

> **ROUND-7 CORRECTION (X-B2) — the previous baseline was UNSATISFIABLE BY ANY CORRECT IMPLEMENTATION and has
> been replaced.** It measured `IsPermanent(bare msgin.ErrNilFunc)` and annotated it *"← must become true"*.
> D-M wraps at the **producer** and deliberately does not touch `IsPermanent`'s closed enumeration, so that row
> reads `false` before **and** after — there is no correct edit that turns it green. The block disproved itself
> three lines down, where `ErrNoCorrelation` is noted as *already wrapped at its producer* and still prints
> `false`. The only way to satisfy it was to add `ErrNilFunc` to the enumeration, which D-M rejects and which
> would make `NewAggregator`'s deliberately-bare constructor return permanent.
> **Counter-rule 7: a gate must measure the observable the change actually moves.** That observable is the
> **producer path**, gate 1 below. Both forms were run at `fe86a12`; the transcripts are pasted verbatim.

**Gate 1 (the RED→GREEN gate) — the producer path.** One message through a `memory` broker with
`RetryPolicy{MaxAttempts: 3, DeadLetter: dlq}` and **no** `WithInvalidMessageSink`, counting hook fires and
sink receipts. The GREEN rows were produced by substituting a step that returns the post-edit error shape, so
the target is measured rather than predicted:

```
$ go test -run TestR7ProducerPath -v .
RED  transform.Transform(nil)      [dlq, no invalid sink] OnRetry=2 OnDeadLetter=1 OnInvalidMessage=0 | dlqSink=1 invalidSink=0 discarded=false
GREEN Permanent(ErrNilFunc)        [dlq, no invalid sink] OnRetry=0 OnDeadLetter=0 OnInvalidMessage=1 | dlqSink=0 invalidSink=0 discarded=true   ← D-M ONLY; see the note below
GREEN Permanent(ErrNilFunc)        [dlq + invalid sink]   OnRetry=0 OnDeadLetter=0 OnInvalidMessage=1 | dlqSink=0 invalidSink=1 discarded=false
RED  msgin.To(nil)                 [dlq, no invalid sink] OnRetry=2 OnDeadLetter=1 OnInvalidMessage=0 | dlqSink=1 invalidSink=0 discarded=false
GREEN Permanent(ErrNilSink)        [dlq, no invalid sink] OnRetry=0 OnDeadLetter=0 OnInvalidMessage=1 | dlqSink=0 invalidSink=0 discarded=true   ← D-M ONLY; see the note below
GREEN Permanent(ErrNilSink)        [dlq + invalid sink]   OnRetry=0 OnDeadLetter=0 OnInvalidMessage=1 | dlqSink=0 invalidSink=1 discarded=false
```

**`OnRetry=2 OnDeadLetter=1 OnInvalidMessage=0` → `OnRetry=0 OnDeadLetter=0 OnInvalidMessage=1` is the gate.**
Note `msgin.To(nil)` produces the **byte-identical** RED line — that is decision D-B1's evidence, and the
reason `handler.go:55` is the fifth edit site below.

> **ROUND-8 (gate minor) — rows 2 and 5 publish a state THIS TASK'S COMMIT NEVER LEAVES BEHIND. Read them as
> D-M-only intermediates, not as the target.** They were measured by substituting D-M's error shape alone,
> **before** D-N and D-P were folded into the same commit. D-N routes an invalid message with **no**
> `WithInvalidMessageSink` to `RetryPolicy.DeadLetter` instead of discarding it, so at this commit's end those
> two rows read **`dlqSink=1 … discarded=false`**, not `dlqSink=0 … discarded=true`. `discarded=true` survives
> only in the configuration these rows do **not** measure — no invalid sink **and** no reachable dead-letter
> sink (a `Send` that fails, which under **D-P** falls through single-shot to ADR 0007 D7's logged discard;
> or `MaxAttempts: 0`, which needs no `DeadLetter`). **Do not re-measure and "correct" rows 1, 3, 4 and 6** —
> they are unaffected: rows 1 and 4 are the pre-edit RED, and rows 3 and 6 configure an invalid sink, which
> D-N leaves untouched. The gate's own arrow (`OnRetry`/`OnDeadLetter`/`OnInvalidMessage`) is unchanged in
> every row; only the `dlqSink`/`discarded` columns of rows 2 and 5 move, and they move **because of a
> decision in this same commit**. Task 9.7's D-N/D-P section below carries the post-fallback expectations.

> **`DeadLetter: dlq` is mandatory in the harness, not decoration.** `RetryPolicy{MaxAttempts: 3}` alone is
> **rejected by `NewConsumer`** — an earlier revision of this block and of ADR 0029 §5.0b both wrote it that
> way, and it cannot be run:
>
> ```
> RetryPolicy{MaxAttempts: 3}.Validate()                   = msgin: finite MaxAttempts requires a DeadLetter sink
> RetryPolicy{MaxAttempts: 3, DeadLetter: sink}.Validate() = <nil>
> RetryPolicy{MaxAttempts: 0}.Validate()                   = <nil>
> ```
>
> (`retry.go:46-53`.) A finite `MaxAttempts` therefore **always** has a DeadLetter sink, which is why row 2's
> `discarded=true` is the *default* outcome rather than a corner case — and why D-N is in this task.

**Gate 2 (no-regression guard, NOT a RED→GREEN gate) — the sentinel census.** Labelled so no one mistakes it
for gate 1 again. Every row must read **the same after the edit as before**; a row that changes means someone
amended `IsPermanent`'s enumeration, which D-M rejects:

```
$ go test -run TestR7SentinelCensus -v .
IsPermanent(msgin: nil endpoint function              ) = false      ← must STAY false (producer wraps, sentinel does not)
IsPermanent(msgin: no route for message               ) = false      ← must STAY false (deliberate transient)
IsPermanent(msgin: payload is not of the expected type) = true       ← unchanged (already enumerated)
IsPermanent(msgin: message has no correlation key     ) = false      ← already wrapped at its producer
IsPermanent(msgin: nil outbound sink                  ) = false      ← must STAY false (producer wraps)
```

Row 4 is the shape to copy: `ErrNoCorrelation` is *not* in the enumeration either — its **producer** wraps it
(`routing/aggregator.go:151-160`), with D-M's exact rationale already in its godoc.

**Gate 3 (ROUND-8, A5 — the PRODUCER path). A RED→GREEN gate whose GREEN is a DOCUMENTED LOSS, not a fix.**
D-M's blast radius was measured on the consumer only. `endpoint/producer.go:453-455` returns on `IsPermanent`
**before** `p.deadLetter(...)`, so the producer's dead-letter sink stops receiving this class and the exported
sentinel `errors.Is(err, msgin.ErrDeadLettered)` flips. One message through a `Producer` over a
`*channel.DirectChannel` whose subscriber is the mis-wired step (`channel/direct.go:89` returns `h.Handle`
verbatim), `RetryPolicy{MaxAttempts: 3, DeadLetter: dlq}`; the AFTER rows substitute the post-edit error shape,
so the target is **measured, not predicted**:

```
$ GOWORK=off go run .        # throwaway module, replace => this tree; measured at 7ee3fd6
BEFORE D-M transform.Transform(nil) OnRetry=2 OnDeadLetter=1 | dlqSends=1 | Is(ErrDeadLettered)=true  Is(ErrNilFunc)=true  Is(ErrNilSink)=false IsPermanent=false
AFTER  D-M Permanent(ErrNilFunc)   OnRetry=0 OnDeadLetter=0 | dlqSends=0 | Is(ErrDeadLettered)=false Is(ErrNilFunc)=true  Is(ErrNilSink)=false IsPermanent=true
BEFORE D-M msgin.To(nil)           OnRetry=2 OnDeadLetter=1 | dlqSends=1 | Is(ErrDeadLettered)=true  Is(ErrNilFunc)=false Is(ErrNilSink)=true  IsPermanent=false
AFTER  D-M Permanent(ErrNilSink)   OnRetry=0 OnDeadLetter=0 | dlqSends=0 | Is(ErrDeadLettered)=false Is(ErrNilFunc)=false Is(ErrNilSink)=true  IsPermanent=true
```

**`dlqSends 1 → 0` and `Is(ErrDeadLettered) true → false` is the gate.** Do **not** "fix" it: `Producer.Send`
is synchronous and hands the error to caller code that can act on it, so nothing is lost, which is also why
**D-N's fallback does not extend to the producer** — the producer has no invalid-message sink at all
(`grep -n 'InvalidMessageSink\|invalidSink' endpoint/producer.go` → **no output**). Its purpose is to make the
change **observable and covered** (Spec §2.1 row 8), because round 7 recorded D-N's premise as *"no
configuration that previously captured a message starts dropping it"* — true of the consumer, false here.

- [x] **Five edit sites — three `nilFuncStep` copies, `Router.Handle`, and `msgin.To`.** Re-derived at
      `fe86a12` by the class sweep below (**not** by grepping a sentinel name):

      | Site | What it is | Sentinel |
      |---|---|---|
      | `endpoint/helpers.go:21` | `nilFuncStep`'s returned handler | `ErrNilFunc` |
      | `routing/helpers.go:23` | `nilFuncStep` (package-local copy) | `ErrNilFunc` |
      | `transform/transformer.go:38` | `nilFuncStep` (package-local copy) | `ErrNilFunc` |
      | `routing/router.go:48` | `Router.Handle`, the `r.pick == nil` early return | `ErrNilFunc` |
      | **`handler.go:55`** | **`To`'s returned handler, the `sink == nil` early return** | **`ErrNilSink`** |

      Each returns a bare sentinel today. Each becomes `msgin.Permanent(<sentinel>)`, wrapped with the position
      exactly as Task 9's combinators are:
      ```go
      fmt.Errorf("%w: routing.Router.Handle: nil pick", msgin.Permanent(msgin.ErrNilFunc))
      fmt.Errorf("%w: msgin.To: nil sink", msgin.Permanent(msgin.ErrNilSink))   // handler.go:55
      ```
      The three `nilFuncStep` copies are shared by five public constructors, so the wrap text must name the
      **caller**, not the helper — pass the position in:
      `nilFuncStep("transform.Transform: nil fn")`. A single `msgin: nil endpoint function` string across every
      position is the debuggability defect CLAUDE.md's *"errors that name the offending field/input"* rule
      exists to prevent.

      > **`handler.go:55` is the ROUND-7 addition (D-B1), and why it was missed is mechanical.** ADR 0029
      > §5.0b's old derivation command greped `msgin\.ErrNilFunc` — scoped to **one sentinel name** *and* to
      > the **qualified** form — so it was structurally blind to a producer inside package `msgin`. `To(sink)`
      > captures `sink` at construction and its handler body tests it per message: the same shape and the same
      > discriminator arm as `nilFuncStep`, and gate 1 above measures the **byte-identical** RED line for it.
      > *(Counter-rule 9.)*

- [x] **Re-derive the site list with the class sweep, not with a sentinel name.** Run this before editing and
      again after; the alternation is generated from `errors.go`, the `(msgin\.)?` prefix is optional so an
      unqualified root producer cannot hide, and the trailing class admits the `}` / `)` a one-line
      `HandlerFunc` body ends with. Output at `fe86a12`, pasted whole:

      ```
      $ sentinels=$(grep -oE '^\s*Err[A-Za-z]+ =' errors.go | tr -d ' \t=' | paste -sd'|' -)   # 43 sentinels
      $ grep -rnE "return (msgin\.)?($sentinels)[ })]*(//.*)?$" --include='*.go' . | sed 's,^\./,,' \
          | grep -v '_test\.go' | grep -vE '^[^:]+:[0-9]+:[[:space:]]*//' | grep -v 'Permanent(' | sort
      adapter/memory/queuestore.go:146:		return msgin.ErrOverflowDropped // nothing evictable (all in-flight) → drop
      adapter/memory/queuestore.go:151:	return msgin.ErrOverflowDropped // OverflowReject
      channel/direct.go:87:		return msgin.ErrNoSubscriber
      endpoint/helpers.go:21:		return msgin.HandlerFunc(func(context.Context, msgin.Message[any]) error { return msgin.ErrNilFunc })
      endpoint/producer.go:589:		return msgin.ErrScheduledSendUnsupported
      handler.go:55:				return ErrNilSink
      retry.go:48:		return ErrInvalidMaxAttempts
      retry.go:51:		return ErrNoDeadLetter
      routing/helpers.go:23:		return msgin.HandlerFunc(func(context.Context, msgin.Message[any]) error { return msgin.ErrNilFunc })
      routing/router.go:48:		return msgin.ErrNilFunc
      routing/router.go:56:			return msgin.ErrNoRoute
      transform/transformer.go:38:		return msgin.HandlerFunc(func(context.Context, msgin.Message[any]) error { return msgin.ErrNilFunc })
      ```

      > **`| sort` AND `| sed 's,^\./,,'` are both load-bearing, not tidiness — this checkbox's gate is a
      > DIFF, so both matter** (Global Constraint 9).
      > - **Order:** `grep -r`'s traversal order is **not stable between runs** — three consecutive runs on the
      >   *unchanged* tree at `fe86a12` emitted the same twelve lines in two different orders. Sorted, three
      >   runs are byte-identical.
      > - **Prefix:** system `grep -rn … .` prefixes every path with `./`; the **ugrep** wrapper on at least one
      >   machine used for this bundle does not, and the block above is pasted in the **stripped** form. Without
      >   the `sed`, this diff fails all twelve lines on a normal shell for a reason that has nothing to do with
      >   the code. Re-verified 2026-07-30 with `/usr/bin/grep`: **the sed-pinned form reproduces the twelve
      >   lines above byte-for-byte; the un-pinned form differs on every line.**

      **Twelve lines: the five edit sites plus seven that stay bare.** ADR 0029 §5.0b carries the per-line
      triage, and **round 8 (B7) corrected three of the seven rationales without changing any conclusion**:
      `retry.go` ×2 = construction-time validation (arm 3); `queuestore.go` ×2 and `direct.go:87` = a
      **MUTABLE** cause (arm 2 — a drain or a later `Subscribe` resolves it), **not** *"no `MessageHandler`
      body"*, which is compile-proven false for both; `producer.go:589` = handed to the caller from
      `SendAfter` (arm 4); `router.go:56` = `ErrNoRoute`, the deliberate transient exclusion (arm 2).
      **After the edit, re-run and confirm the five edit-site lines are gone and the seven survivors are
      unchanged** — that is this checkbox's gate.

      The single-result anchor is a **shortlist, not the class**. Dropping the `[ })]*(//.*)?$` anchor (pattern
      `return .*\b(msgin\.)?($sentinels)\b`) sweeps every return of a root sentinel and reports **63 lines** at
      `fe86a12`; the extra 51 are two-result constructor returns of the `return nil, msgin.ErrX` shape,
      including the deliberately-excluded `routing/aggregator.go:251`. *(Derived summary — the command is the
      one above with that pattern.)* Constructor arity is a strong proxy for the invariant's **third** arm but
      is **not** the invariant: when a new sentinel lands, triage the 63 against all four arms.
      *(Round-8 B7: "second arm" was written against the withdrawn two-arm form.)*
- [x] **`ErrNoRoute` is NOT wrapped — this is a decision, not an omission.** `routing/router.go:48-56`'s
      `pick` is caller-supplied and evaluated **per message**; it may consult a routing table, feature flag or
      lookup service, so a message unroutable now may be routable after a config reload. `WithDefaultChannel`
      is the documented way to make that outcome deterministic. Leave it transient and add a **regression
      case** asserting `msgin.IsPermanent(err) == false` for `ErrNoRoute`, so a later sweep cannot "finish the
      job" by wrapping it.
- [x] **Update every godoc that promises the old behavior.** Each says *"a nil X yields `ErrNilFunc`"* with no
      classification. The list is derived by an **unrestricted** sweep — round-7 X-B4: the previous form
      hard-coded five filenames and thereby missed two sites, one of them `ErrNilFunc`'s **own sentinel
      godoc**. Output at `fe86a12`, pasted whole:

      ```
      $ grep -rn 'ErrNilFunc\|ErrNilSink' --include='*.go' . | grep -v '_test\.go' | grep '//' | sort
      endpoint/activator.go:13:// error propagates without forwarding; a nil svc yields ErrNilFunc. For a
      endpoint/activator.go:37:// svc yields ErrNilFunc.
      endpoint/helpers.go:16:// nilFuncStep returns a Step whose handler always fails with ErrNilFunc — the
      errors.go:150:	// ErrNilSink is returned by To when its OutboundAdapter sink is nil.
      errors.go:152:	// ErrNilFunc is returned by an endpoint (Transform/Filter/Activate/Consume/
      handler.go:50:// A nil sink yields ErrNilSink at send time (no panic on caller input).
      routing/aggregator.go:239:// a nil store is ErrNilStore, a nil fn is ErrNilFunc, and no WithOutputChannel
      routing/filter.go:26:// ErrPayloadType; a nil pred yields ErrNilFunc.
      routing/helpers.go:18:// nilFuncStep returns a Step whose handler always fails with ErrNilFunc — the
      routing/helpers.go:20:// §3.3; the body is five lines over the exported Step/HandlerFunc/ErrNilFunc.
      routing/router.go:25:// returned channel is ignored). A nil pick yields ErrNilFunc. Router implements
      routing/router.go:36:// construction and surfaces as ErrNilFunc at Handle time (no panic on input).
      routing/splitter.go:13:// an fn error propagates without forwarding; a nil fn yields ErrNilFunc (no
      transform/transformer.go:14:// without forwarding. A nil fn yields ErrNilFunc (no panic on caller input).
      transform/transformer.go:35:// function: its handler returns ErrNilFunc instead of panicking on a nil call.
      ```

      **15 lines, pasted whole and `sort`-pinned** *(round-7 X-M6 flagged a sibling block presented as pasted
      output but re-typed in a different order; `grep -r`'s own order is not reproducible, so `| sort` is what
      makes "pasted whole" checkable at all).*

      Explicit checkboxes, because two of these are the sites the hard-coded grep could not see:

      - [x] **`errors.go:152` — `ErrNilFunc`'s own sentinel godoc.** The single statement every caller reads,
            and the natural home for the governing invariant, which the godoc states in these words
            (ADR 0029 §5.0b, Spec §2.1 row 6 — a phrase match, so do not paraphrase):

            > **every typed error msgin returns from inside a `MessageHandler` body msgin itself constructs,
            > whose cause was fixed at construction and cannot change for the message's lifetime, is
            > `Permanent`; a fault a later `Subscribe`, config reload or drain could resolve stays bare and
            > transient; every one returned from a constructor is bare, because construction never reaches a
            > `RetryPolicy`; and everything else — handed to a caller from a non-constructor API — is bare
            > too.**

            Applied here: a producer inside a `MessageHandler` body returns it wrapped in `msgin.Permanent`
            with positional context, `errors.Is(err, msgin.ErrNilFunc)` still matches, and a **constructor**
            (`NewAggregator`) returns it **bare**.

            > **⛔ ROUND-8 CORRECTION (design B7) — DO NOT WRITE THE ROUND-7 SENTENCE INTO THIS PUBLIC GODOC.**
            > This checkbox previously ordered the words *"every **deterministic** typed error msgin returns
            > from inside a `MessageHandler` body is `Permanent`; every one returned from a constructor is
            > bare"*. **That sentence is false, and this checkbox would have shipped it as `msgin`'s public
            > contract.** Two counter-examples, both compile-proven at `7ee3fd6` and both reachable from a
            > `MessageHandler` body msgin constructs (`To`'s returned `Step`, composed by `Chain`):
            >
            > ```
            > Chain(To(*DirectChannel)).Handle       err=msgin: channel has no subscriber
            >   errors.Is(err, msgin.ErrNoSubscriber)=true  msgin.IsPermanent(err)=false
            > Chain(To(*QueueChannel[reject])).Handle err=msgin: message dropped by overflow policy
            >   errors.Is(err, msgin.ErrOverflowDropped)=true  msgin.IsPermanent(err)=false
            > ```
            >
            > Both are **correctly** transient (a `Subscribe` or a drain resolves them), so the old wording did
            > not merely mis-describe an edge — it demanded a wrap that would be wrong. *"Deterministic"* was
            > undefined and carried the whole load; the discriminator is **immutability of the cause at
            > construction**. ADR 0029 §5.0b carries the correction and the twelve-line check.
      - [x] **`errors.go:150` — `ErrNilSink`'s sentinel godoc**, same treatment (D-B1).
      - [x] **`routing/aggregator.go:239` — the deliberate EXCLUSION.** Say so explicitly: `NewAggregator`
            returns `ErrNilFunc` bare because it is construction-time and never reaches a `RetryPolicy`. Left
            unsaid, the next sweep reads it as an omission and "finishes the job".
      - [x] `handler.go:50` (`To`), `endpoint/activator.go:13` + `:37` (`Activate`, `Consume`),
            `routing/splitter.go:13` (`Split`), `routing/filter.go:26` (`Filter`),
            `routing/router.go:25` + `:36` (`Router`, `NewRouter`), `transform/transformer.go:14`
            (`Transform`) — each states that the error is **permanent**, routed to the **invalid-message**
            channel rather than retried to the dead-letter sink, and that `errors.Is(err, msgin.ErrNilFunc)`
            (resp. `ErrNilSink`) still matches.
      - [x] The three `nilFuncStep` internal comments (`endpoint/helpers.go:16`, `routing/helpers.go:18` +
            `:20`, `transform/transformer.go:35`) move with them, and gain the position parameter's meaning.

      Re-run the sweep after the edit and paste it: the godoc gate here is the grep's *content*, not its exit
      status.
### D-N + D-P — the `divert` dead-letter fallback, and its SINGLE SHOT (Spec §2.1 row 7), SAME COMMIT

> **Why it is here and not a separate task.** D-M as decided opens an unacknowledged data-loss path, and it is
> the *default* configuration rather than a corner case: a finite `MaxAttempts` **requires** a `DeadLetter`
> (`RetryPolicy.Validate`, `retry.go:46-53` — transcript in gate 1 above) while `WithInvalidMessageSink` is
> optional and unset by default. Gate 1's row 2 is the loss, measured: `dlqSink=1 discarded=false` becomes
> `dlqSink=0 discarded=true`. Landing D-M without D-N would leave a tree, between commits, in which a
> mis-wired step's message is silently dropped where it was previously captured durably. **CLAUDE.md:** *"When
> a wrong default could silently corrupt (… lose data …), pick the value that fails safe."*

**RED baseline for D-N, at THIS task's start** (counter-rule 8 — gate 1's transcript above is the same run, so
no separate pin is needed). Row 3 was produced by pointing the invalid-message sink at the same sink instance
the DeadLetter names, which is exactly the destination the fallback selects, so the target is **measured, not
predicted**:

```
$ go test -run TestR7DivertFallback -v .
BEFORE D-M  bare ErrNilFunc          [dlq only]           OnRetry=2 OnDeadLetter=1 OnInvalidMessage=0 | dlqSink=1 invalidSink=0 discarded=false
AFTER  D-M  Permanent(ErrNilFunc)    [dlq only, NO D-N]   OnRetry=0 OnDeadLetter=0 OnInvalidMessage=1 | dlqSink=0 invalidSink=0 discarded=true
AFTER  D-M+D-N Permanent(ErrNilFunc) [dlq only, fallback] OnRetry=0 OnDeadLetter=0 OnInvalidMessage=1 | dlqSink=1 invalidSink=1 discarded=false
```

*(Row 3's `invalidSink=1` is the same counter as `dlqSink` — one sink instance, counted twice by the harness.
One message, one sink receipt.)* **The gate: row 2 must become row 3.**

- [x] **Read `endpoint/consumer.go`'s `divert` path before writing anything** — `:747-791` (the three-outcome
      contract) and its two invalid-path call sites, `:688` (decode failure) and `:716` (permanent handler
      error). The dead-letter call site is `:726`.
- [x] **Implement the fallback at the CALL SITES, not inside `divert`.** `divert` is shared by all three
      paths; putting the fallback in its `sink == nil` arm would make the dead-letter call site
      (`:726`, whose `sink` *is* `c.policy.DeadLetter`) fall back to itself — a no-op today, but a trap for the
      next reader. Add one small accessor and use it at `:688` and `:716`:
      ```go
      // invalidTarget returns where an invalid message is diverted: the configured
      // invalid-message sink, or — when none is configured (D-N) — the dead-letter
      // sink, so a fault previously captured durably is never downgraded to a
      // discard by D-M's reclassification. fellBack reports the second case, so the
      // call site can emit D-N's WARN with the message id. Both are nil/false only
      // when neither sink is configured, where ADR 0007 D7's logged discard remains
      // the terminal behavior.
      func (c *consumer[T]) invalidTarget() (sink msgin.OutboundAdapter, fellBack bool)
      ```

      > **The nullary signature drafted in round 7 could not satisfy its own WARN checkbox** (round-8 design
      > minor). The WARN below must name the message — its sibling at `endpoint/consumer.go:766` logs
      > `"id", d.Msg.ID()` — and `invalidTarget()` returning only the sink leaves the call site unable to tell
      > *"the caller configured this sink"* from *"we fell back to it"*. The `fellBack` bool is the smallest fix
      > and keeps the accessor a pure config read, with `d` staying at the call site where it already is.
- [x] **`OnInvalidMessage` fires, NOT `OnDeadLetter`** — the hook reports the **classification**, the sink is
      only the **destination**. Firing `OnDeadLetter` would assert the message exhausted its retry budget,
      which under D-M it explicitly did not. No change to `divert`'s `terminalHook` argument at either site.
- [x] **Announce the fallback, and DEDUPLICATE it.** Keep the existing loud WARN at
      `endpoint/consumer.go:766` for the neither-sink case, and add a WARN on the fallback naming **both**
      facts — no invalid-message sink configured, message sent to the dead-letter sink instead — plus
      `"id", d.Msg.ID()`. A caller must not discover by inspection that their DLQ has started receiving
      invalid messages.

      > **Deduplicate it, per the in-tree precedent** (round-8 design minor). *"No invalid-message sink is
      > configured"* is **constant for the consumer's lifetime**, so an undeduplicated WARN emits one line per
      > invalid message — a poison storm floods the log with a line that says the same thing every time. That
      > is exactly the reason `governorPanic` deduplicates (`endpoint/consumer.go:573-585`: a `sync.Map`
      > `LoadOrStore` keyed by method, and the message itself says *"further occurrences for this method
      > suppressed"*). Reuse that shape — `c.panicLogged` is keyed by an arbitrary string, so a distinct key
      > (e.g. `"divert.fallback"`) needs no new field; if a new field reads clearer, say so in the commit body.
      > **The one-line-per-message form is only correct for the neither-sink WARN at `:766`**, which is a
      > terminal discard and whose per-message id is the only record that the message existed.
- [x] **Both invalid-path call sites, not just the permanent one.** `:688` (decode failure) gets the fallback
      too; two invalid-message paths with different fallback behavior would be incoherent. The decode arm's
      change is **discard → dead-letter**, a strict improvement over ADR 0007 D7's discard and a behavior
      change in its own right — say so in the commit body rather than letting it ride in silently.
- [x] **Amend [ADR 0007 D7](../adrs/0007-reliability-settlement-api.md#d7--no-invalid-sink-policy-tasks-45)**
      — already written; verify the note still matches what you implemented and correct the ADR if it does not.
- [x] Update `divert`'s own godoc (`:747-762`): its first outcome is no longer *"nil sink → discarding IS the
      terminal invalid event"* unconditionally.

#### D-P — the fallback is SINGLE-SHOT (round 8, amends D-N)

> **Why this exists.** D-N said *where* the message goes and never said what happens when that target is
> **down**. `divert`'s send-failure arm (`:774-782`) `Nack`s with `requeue=true`, so a **permanent** message
> re-enters the flow forever. ADR 0007 D7 rejects exactly this in its own words: *"retrying anyway would only
> convert a configuration gap (no sink configured) into an infinite-retry trap, which is **worse** than a
> logged, observable discard."* **The class:** *a settlement path that is terminal by construction must not
> become non-terminal without a bound.*

**RED baseline for D-P, at THIS task's start.** D-N implemented exactly as specified, default configuration,
dead-letter sink whose `Send` returns an error (measured in round 8):

```
BEFORE D-N: deliveries=1   acks=1  nacks=0   dlqSends=0   OnInvalid=1  OnDeadLetter=0  OnRetry=0
AFTER  D-N: deliveries=41  acks=0  nacks=40  dlqSends=40  OnInvalid=0  OnDeadLetter=0  OnRetry=40
```

*(41 was the harness's redelivery cap, reached in under 10 ms; the loop is unbounded in reality. `OnRetry=40`
is `divert`'s failure arm firing once per redelivery; `OnInvalid=0` because no terminal event ever happens.)*
**The gate: with D-P, the same harness must read `deliveries=1 acks=1 nacks=0 dlqSends=1 OnInvalid=1
OnRetry=0`** — one attempt at the sink, one WARN, one `Ack`.

- [x] **On the INVALID path, a failed sink `Send` must NOT `Nack`.** Settle it as ADR 0007 D7's discard: WARN
      naming **both** the classification cause *and* the sink error, fire the terminal hook
      (`OnInvalidMessage`), `Ack`, and evict gated on the `Ack` exactly as the other terminal arms do.
      **`OnRetry` must not fire** — no retry follows.
- [x] **Scope it to the invalid path.** The dead-letter call site (`:726`) keeps D8's `Nack`-with-backoff on
      send failure: that message is *transient* by classification, so requeueing it is a retry, not a loop.
      Two shapes are viable — **pick one and say which in the commit body**:
      1. **(recommended) split the function** — `divert` keeps the retryable contract for `:726`, and the two
         invalid call sites (`:688`, `:716`) get a terminal sibling that has no `Nack` arm and no `attempt`
         parameter at all. Smaller cognitive surface per function, and each one's godoc becomes true again
         without a conditional.
      2. a `singleShot bool` (or equivalent) parameter on `divert`. One function, but its three-outcome godoc
         has to branch, and the `attempt` parameter becomes meaningless on one of the two paths — the shape
         that produced the next finding.
- [x] **Delete the hard-coded `attempt` on the invalid path — do not leave a misleading `1`** (round-8 design
      minor). Both invalid call sites pass `1` today (`:688`, `:716`), and `retryDelay(policy, 1)` is
      `p.Backoff.Delay(0)` (`:948-953`) — the **first** backoff step, on every iteration, never escalating.
      Under D-P that value is **unreachable**: the invalid path no longer `Nack`s, so nothing consumes it. It
      must disappear with the branch, not survive as a constant a later reader has to reason about.
- [x] **Re-verify the [ADR 0007 D7](../adrs/0007-reliability-settlement-api.md#d7--no-invalid-sink-policy-tasks-45)
      note** — the D-P amendment is already written there; confirm it matches what you implemented and correct
      the ADR if it does not.

**Godoc sweep for D-N/D-P — derived from the BEHAVIOR CHANGED, not from D-M's sentinel names** (round-8 A4).

> **Class:** *a behavior change's godoc sweep must be derived from the behavior changed, not from the sentinels
> that motivated it.* The sweep above this section greps `ErrNilFunc\|ErrNilSink`, which is right for D-M and
> **structurally blind** to D-N/D-P: the one godoc that states the old behavior verbatim —
> `endpoint/consumer.go:67`, *"If unset, such messages are logged and discarded (ADR 0007 D7)"*, which becomes
> **false for every finite-retry consumer** — contains neither sentinel name. Three arms, because the behavior
> moved in three ways: **where an invalid message goes**, **whether it is discarded**, and **what a failing
> sink causes**. Output at `7ee3fd6`, pasted whole:

```
$ { grep -rnE '^[[:space:]]*//.*invalid[ -]?(message[ -]?)?(sink|channel)' --include='*.go' . ;
    grep -rniE '^[[:space:]]*//.*discard' --include='*.go' . \
      | grep -iE 'message|delivery|invalid event' | grep -viE 'discard(ing)? logger|discardLogger' ;
    grep -rniE '^[[:space:]]*//.*(sink (failed|down)|retry forever|retried forever)' --include='*.go' . ;
  } | grep -v '_test\.go' | sed 's,^\./,,' | sort -u
adapter/database/sql/inbox_dedup.go:183:// prevent. Under the DEFAULT MaxAttempts==0 (retry forever) the redelivery
adapter/database/sql/options.go:186:// # Do not use as a DLQ / invalid-message sink (LOW audit)
adapter/database/sql/options.go:193:// divert would treat that as "sink failed" and retry forever — the poison
adapter/database/sql/options.go:78://     DLQ/invalid sink's Send BEFORE it Acks (freeing the claim connection), so a
adapter/database/sql/options.go:82://     deadline. A lock-strategy consumer whose DLQ/invalid sink is a sql adapter
adapter/database/sql/source.go:16:// invalid-sink, because Poll drops it before building a Delivery. So the Source
adapter/http/sseclient.go:69:// resume is not a redelivery guarantee — a server that discards its replay
channel/pubsub.go:26:	// the WHOLE message to the invalid-message sink (observable, not retried);
doc.go:15:// To(sink) or Consume, or its final message is discarded.
doc.go:24:// routed to the invalid-message channel.
endpoint/consumer.go:61:// WithRetryPolicy sets the settlement policy (default: retry forever, immediate).
endpoint/consumer.go:67:// If unset, such messages are logged and discarded (ADR 0007 D7).
endpoint/consumer.go:714:		// ErrPayloadDecode/ErrPayloadType) → invalid sink. Sink-attempt 1.
endpoint/consumer.go:750://   - nil sink → discarding IS the terminal invalid event (ADR 0007 D7): log a
endpoint/consumer.go:765:		// nil sink: discarding is the terminal invalid event (ADR 0007 D7).
endpoint/consumer.go:775:		// Sink down (including a panicking sink, recovered by safeSend): the
endpoint/consumer.go:841:	// is permanent (it will not shrink on redelivery) → invalid sink, not retried.
endpoint/consumer.go:855:// failure: PERMANENT → invalid sink (never retried against a codec that will
endpoint/consumer.go:875:// (an invalid-message sink or DeadLetter) cannot crash the process (fault
endpoint/consumer.go:931:// (Nacked), not diverted to the invalid sink.
endpoint/flowcontrol.go:99:// invalid-message sink like a decode failure, never retried — since an over-size
endpoint/producer.go:233:// It exists because MaxAttempts == 0 means "retry forever", bounded otherwise
errors.go:14:	// shrink on redelivery), so the message is diverted to the invalid sink like
errors.go:184:	// routes it to the invalid-message channel rather than retrying.
errors.go:46:	// that is unbounded in BOTH dimensions: MaxAttempts == 0 (retry forever) with
errors.go:50:	// NewConsumer, where "retry forever, immediately" means broker redelivery,
handler.go:28:// a producing flow MUST end in To or Consume, or its final message is discarded.
handler.go:37:// no downstream terminal will DISCARD its final message silently. Always end a
payload.go:8:// permanent, the driving Consumer routes it to the invalid-message channel (never
reliability.go:17:// Permanent(err) sends the message to the invalid-message sink without
reliability.go:9:// straight to the invalid-message sink instead of retrying. Wrapping is
retry.go:15:// The zero value is valid and means "retry forever, immediately, no DLQ".
retry.go:9://   - MaxAttempts == 0 : retry forever (no dead-letter).
routing/aggregator.go:23:// correlation failure: the message is routed to the invalid-message channel
routing/aggregator.go:61:// key is Permanent(ErrNoCorrelation) — routed to the invalid-message channel
routing/doc.go:7:// [WithDiscardChannel]) a message that does not qualify. [Router] is the
routing/filter.go:14:// WithDiscardChannel routes messages a Filter rejects (predicate false) to ch
routing/splitter.go:12:// non-A payload yields ErrPayloadType (routed to the invalid-message channel);
transform/transformer.go:13:// ErrPayloadType (routed to the invalid-message channel); an fn error propagates
```

> **39 lines, `sed`-normalized and `sort -u`-pinned.** The `sed 's,^\./,,'` is **not cosmetic**: a shell whose
> `grep` is a `ugrep` wrapper (Claude Code installs one) omits the `./` prefix that system `grep -r .` emits,
> so without it the pasted block does not reproduce across shells. Content was verified identical under both;
> only the prefix differed. `sort -u` pins the order, which `grep -r` does not guarantee between runs.
>
> **This sweep is a class, so it must be TRIAGED, not blanket-edited. 39 lines = 13 triaged out + 3
> verdict-only + 23 edited**, and the checkboxes below account for all 23 (1 + 2 + 12 + 8).
> **Thirteen hits are a different feature and stay untouched:**
> `routing/doc.go:7`, `routing/filter.go:14` (`WithDiscardChannel` — a Filter's rejection route),
> `doc.go:15`, `handler.go:28`, `:37` (a Chain with no terminal), `adapter/http/sseclient.go:69` (SSE replay),
> `retry.go:9`, `:15`, `endpoint/consumer.go:61`, `endpoint/producer.go:233`, `errors.go:46`, `:50`,
> `adapter/database/sql/inbox_dedup.go:183` (`MaxAttempts == 0` — a *different* "retry forever").
>
> **Three more need a stated verdict rather than an edit, because they are about the sink's DEPLOYMENT, not
> its selection:** `adapter/database/sql/options.go:78` + `:82` already say *"DLQ/invalid sink"* as one target,
> so D-N does not falsify the separate-pool mandate — it makes it bind **more often** (a consumer with no
> invalid sink now diverts invalid messages down the DLQ connection too); confirm the wording still reads
> correctly and leave it if so. `adapter/database/sql/source.go:16` is unaffected: a corrupt row is dropped by
> `Poll` before a `Delivery` exists, so it never reaches `divert` on any arm.

- [x] **`endpoint/consumer.go:67` — `WithInvalidMessageSink`'s godoc, the one this sweep exists to catch.**
      Today: *"If unset, such messages are logged and discarded (ADR 0007 D7)."* That sentence becomes **false
      for every finite-retry consumer** the moment D-N lands. It must state all three arms — dead-letter
      fallback, single-shot discard when that sink fails, discard when neither is configured — **and** the
      operational remedy from ADR 0029 §5.0b: configuring this sink is what lets an operator tell
      *retries-exhausted* from *permanently-invalid* in a shared dead-letter store, because msgin stamps no
      settlement-reason header.
- [x] **`adapter/database/sql/options.go:186` + `:193` — the `WithSharedTransaction` warning becomes half
      false.** (`:186` is the heading of the same godoc block; edit them together.) `:193` reads *"the divert
      would treat that as 'sink failed' and retry forever — the poison message never actually reaches the
      dead-letter table."*
      Under D-P that is no longer true on the **invalid** path (one attempt, then a logged discard); it stays
      true on the **dead-letter** path. The advice — *"do not use a strict shared-transaction Outbound as a
      DLQ/invalid sink"* — is unchanged and gets **stronger** under D-N, since a plain consumer's dead-letter
      sink now receives invalid messages too. Correct the mechanism, keep the advice.
- [x] **The invalid-message destination is no longer unconditional** in `reliability.go:9` and `:17`
      (`permanentError` / `Permanent` — *"routes it straight to the invalid-message sink"*), `errors.go:14`
      (`ErrPayloadTooLarge`), `errors.go:184`, `payload.go:8`, `endpoint/flowcontrol.go:99`
      (`WithMaxPayloadBytes`), `channel/pubsub.go:26`, `doc.go:24`, `routing/aggregator.go:23`, `:61`,
      `routing/splitter.go:12`, `transform/transformer.go:13`. Each states or implies *the* invalid-message
      sink; after D-N the destination is *the invalid-message sink, or the dead-letter sink when none is
      configured*. **Twelve lines.** Prefer one canonical sentence, cross-referenced, over twelve paraphrases.
- [x] **`endpoint/flowcontrol.go:99` additionally carries an accepted cost** (ADR 0029 §5.0b): after D-N a
      payload rejected by `WithMaxPayloadBytes` is **persisted** into the operator's durable dead-letter store
      — msgin storing the very bytes the cap declared illegitimate. Say so on the option, with the two levers
      (point `WithInvalidMessageSink` elsewhere; or reject oversize input at the adapter).
- [x] **`endpoint/consumer.go:714`, `:750`, `:765`, `:775`, `:841`, `:855`, `:875`, `:931`** — the internal
      comments on the settlement path itself. `:750`/`:765` state the nil-sink discard as unconditional;
      `:775`'s *"Sink down … the message was NOT diverted → retry it"* is the arm D-P removes from the invalid
      path; `:714`'s *"Sink-attempt 1"* is the hard-coded `attempt` above.
- [x] **Re-run the sweep after the edits and paste it.** Like D-M's, this checkbox's gate is the grep's
      **content**, not its exit status: the 39 lines must still be 39 (no site dropped) and every non-triaged
      line must have changed.

**Hot-path branches needing a case each** (fold into one `table-test` per package, `assert`-closure form):

*D-M — the classification:*

- each of the **five** public nil entry points — `endpoint.Activate(nil)`, `endpoint.Consume(nil)`,
  `routing.Filter(nil)`, `routing.Split(nil)`, `transform.Transform(nil)` — asserting **all three** of
  `errors.Is(err, msgin.ErrNilFunc)`, `msgin.IsPermanent(err) == true`, and that the message names its
  position (assert the position **substring**, not the whole error text — the full string embeds
  `permanentError`'s format);
- **`msgin.To(nil)`** (D-B1, root `handler.go`) — same three assertions over `msgin.ErrNilSink`. This is a
  **root**-package case and needs a root `_test` file, which is why `core` is in the commit scope;
- `NewRouter(nil).Handle` — same three assertions, the one non-`nilFuncStep` site;
- **the negative:** `Router.Handle` with a `pick` that returns no channel and no default →
  `errors.Is(err, msgin.ErrNoRoute)` **and `msgin.IsPermanent(err) == false`**;
- **the constructor arm of the invariant:** `NewAggregator` with a nil fn → `errors.Is(err, ErrNilFunc)` **and
  `msgin.IsPermanent(err) == false`**, so the deliberate exclusion has a test naming it as deliberate.

*D-N — the fallback (all three in `endpoint`, driving a real consumer):*

- **invalid sink nil, DeadLetter configured** → the DeadLetter sink receives the message, `OnInvalidMessage`
  fires, `OnDeadLetter` does **not**, and the fallback WARN is emitted (assert via an injected `*slog.Logger`
  writing to a buffer);
- **neither sink configured** → the ADR 0007 D7 discard survives: `OnInvalidMessage` fires, nothing is sent,
  the original delivery is Acked, the existing WARN is emitted. *Without this case a later sweep deletes the
  discard arm as dead code;*
- **invalid sink configured** → unchanged, and the DeadLetter sink receives **nothing** (proves the fallback
  is not firing when it must not);
- **the decode arm** with invalid sink nil and DeadLetter configured → the DeadLetter sink receives it, so
  `:688`'s change is covered too and not only `:716`'s.

*D-P — the single shot (round 8, A1). This is the branch D-N created and left uncovered:*

- **fallback target configured, its `Send` FAILS** → **the newly reachable state, which D-N's case list did
  not contain at all.** Drive a real consumer with `invalidSink == nil` and a `DeadLetter` sink whose `Send`
  returns an error, and assert **all five**: the original delivery is **`Ack`ed** (not `Nack`ed), the
  source sees **exactly one** delivery (no redelivery loop), `OnInvalidMessage` fires **once**, **`OnRetry`
  does not fire at all**, and the WARN names **both** the classification cause *and* the sink error (assert
  both substrings against an injected `*slog.Logger` writing to a buffer). *Without the delivery-count and
  `OnRetry` assertions this case passes against the very implementation D-P exists to forbid* — the round-8
  measurement was `nacks=40 OnRetry=40 dlqSends=40` on a harness that capped redelivery at 41.
- **the same failure on the DECODE arm** (`:688`) → identical outcome, so single-shot is not implemented at
  one call site only.
- **the negative that proves the scope:** the **dead-letter** call site (`:726`) with a `DeadLetter` sink
  whose `Send` fails → **still `Nack`s with a non-zero backoff delay and fires `OnRetry`** (ADR 0007 D8,
  unchanged). Without this case a later sweep "finishes the job" and makes the transient path terminal too.
- **fallback WARN deduplication** → two invalid messages through one consumer with the fallback active emit
  the fallback WARN **once**, while the per-message terminal records still appear (mirrors
  `governorPanic`'s dedup test).

*D-M's producer-side consequence (round 8, A5) — one case, in `endpoint`:*

- a `Producer` over a `*channel.DirectChannel` whose subscriber returns `msgin.Permanent(msgin.ErrNilFunc)`,
  with `RetryPolicy{MaxAttempts: 3, DeadLetter: dlq}` → the dead-letter sink receives **nothing**,
  `OnDeadLetter` does **not** fire, `OnRetry` does **not** fire, and the returned error satisfies
  `errors.Is(err, msgin.ErrNilFunc) == true` **and `errors.Is(err, msgin.ErrDeadLettered) == false`**. That
  last assertion is the register row: it is the exported contract that flips, and gate 3 is its transcript.

**Verify:**

- **Gate 1 re-run**, now printing `OnRetry=0 OnDeadLetter=0 OnInvalidMessage=1` on the two former RED rows.
- **Gate 2 (the census) re-run, every row UNCHANGED** — a row that moved means `IsPermanent`'s enumeration was
  amended, which D-M rejects.
- **Gate 3 (the producer path) re-run**, showing `dlqSends 1 → 0` and `Is(ErrDeadLettered) true → false`. It
  is a **documented loss, not a regression** (Spec §2.1 row 8) — do not "fix" it; confirm the covering case
  asserts it.
- **D-N's gate re-run**, row 2 now reading row 3's values.
- **D-P's gate re-run**: the D-N-only transcript `deliveries=41 acks=0 nacks=40 dlqSends=40 OnInvalid=0
  OnRetry=40` must become `deliveries=1 acks=1 nacks=0 dlqSends=1 OnInvalid=1 OnRetry=0`.
- The class sweep, D-M's godoc sweep, **and D-N/D-P's behavior-derived godoc sweep** re-run and pasted (their
  checkboxes above).
- `go test ./... -race -shuffle=on` green across all root packages; the **seven**-module `GOWORK=off` loop
  (not eight — `expr` does not exist until Task 10).
- **`apidiff` is expected to report NOTHING for this task.** No exported symbol is added, removed or retyped;
  the change is behavioral. Do not treat the empty diff as a failed run — say so in the commit body.
- **Coverage: no NEW uncovered block outside AC-7's enumerated six**, in `endpoint`, `routing`, `transform`
  and root; `-coverpkg=./...` on both sides against a **named** tree (Global Constraint 0).

  > **Not a percentage floor — round-7 X-M14.** The previous wording was *"per-package coverage does not fall
  > (all three are at ≥99% today)"*. `endpoint` measured **99.4%** and **99.1%** on two runs of the *unchanged*
  > tree, so a floor expressed to one decimal fails on noise alone. The block-level criterion is stable and is
  > what AC-7 already uses.

**Commit:** `fix(core,endpoint,routing,transform,sql)!: classify deterministic endpoint faults as Permanent`

```
Spec: 014
Plan: 027
ADR: 0007
ADR: 0029
RFC: 0002
```

> **The scope was wrong in both directions and is corrected here (round-7 X-M2).** It read
> `fix(core,routing,transform)` — `core` was **unearned** (no root file was touched) and `endpoint` was
> **missing** (`endpoint/helpers.go:21` is an edit site). Both are now earned: D-B1 adds `handler.go:55` and
> its root `_test` case (**`core`**), and D-N adds `endpoint/consumer.go` on top of `endpoint/helpers.go`
> (**`endpoint`**). `ADR: 0007` is new — D-N amends its D7.
>
> **`sql` was added in round 8 (A4).** D-N/D-P's behavior-derived godoc sweep reaches
> `adapter/database/sql/options.go:186` + `:193`, whose `WithSharedTransaction` warning describes the
> retry-forever mechanism D-P removes from the invalid path. `adapter/database/sql` is in the **root module**,
> so this stays one commit and one `go test ./... -race` run — no extra module joins the loop. **ADR 0007 is
> now amended TWICE by this commit** (D-N and D-P); the trailer already names it.

The `!` is deliberate even though `apidiff` is empty: the change moves a message from the **dead-letter** sink
to the **invalid-message** sink and stops it recording an unhealthy breaker signal
(`endpoint/consumer.go:614`, `:733`), and D-N changes where an invalid message lands when no invalid-message
sink is configured. A caller who watches the DLQ sees a behavioral break; the exported surface does not move.
*(No tag exists — `git tag | wc -l` → 0 — so nothing downstream is affected today; the marker is for the log's
benefit.)*

---

## Task 10 — The `expr` provider module · **L** · DONE

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

- [x] New module `expr` with its own `go.mod` (Go 1.25). **It needs BOTH:**
      ```
      require github.com/kartaladev/msgin v0.0.0
      replace github.com/kartaladev/msgin => ..
      ```
      `git tag | wc -l` → **0**, so without the `replace` the module cannot resolve the root module under
      `GOWORK=off` — which is exactly how CI's `module` job runs it. A `use` line in `go.work` is necessary
      but **not sufficient** (round-2 §C2).
- [x] **`go.work`: add `./expr` to the `use` block — a PREREQUISITE of CI edit #2, not a nicety.** The
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
- [x] **CI: three edits, not one.** `.github/workflows/ci.yml`'s `module` matrix lists six directories
      (`grep -c 'dir:' .github/workflows/ci.yml` → **6**) and the `workspace` job hard-codes a six-directory
      loop:
      1. add `expr` to the `module` matrix;
      2. add `expr` to the `workspace` job's loop — **after the `go.work` edit above**;
      3. **fix the pre-existing gap** — `adapter/cron/crontest` is missing from **both** and has been since
         it was created. **Check it with the comment-stripped form** — `ci.yml` now *documents* the gap in
         three comment lines, so a bare `grep -n crontest` hits them and reads as "already done":
         ```
         $ grep -v '^\s*#' .github/workflows/ci.yml | grep -c crontest
         0
         $ grep -c 'dir:' .github/workflows/ci.yml
         6
         ```
         Both re-run on the working tree (2026-07-29). **The RED is the `0`**: today `crontest` appears in
         `ci.yml` only in comments. Edit #3 is done when the comment-stripped grep is non-zero on **both**
         sides — a `module`-matrix entry *and* the `workspace` job's `for dir in …` loop (`:129`) — and the
         `dir:` count has gone **6 → 8** (edit #1 adds `expr`, edit #3 adds `crontest`).

      > **ROUND-7 CORRECTION (X-B6, counter-rule 10).** This checkbox published
      > *"`grep -n crontest .github/workflows/ci.yml` → no output"*, and **the very commit that carries this
      > plan (`c4582ba`) falsified it**: that commit added the three `crontest` KNOWN-GAP comments to `ci.yml`,
      > so the command now returns **3** hits and a worker running it concludes edit #3 is already done. It is
      > not — no `dir:` entry and no loop entry exists. The comment-stripped form above is the one CLAUDE.md's
      > own quality-gate block already uses. **Counter-rule 10: when a pass edits a file, re-run every pasted
      > command in the bundle that reads that file.**
- [x] Providers returning the Task 9 types. **The shape is NOT uniform, and it is NOT non-generic** — the
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
- [x] **`expr/errors.go` — the module declares exactly ONE sentinel (decision D-I, §9.5.0; revised D-K).**
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
      > needed**; **no new root symbol and no new import edge — but `ErrPayloadType`'s godoc IS widened**
      > (see the `errors.go:6` checkbox below). **D-I is unaffected** — `ErrInvalidExpression` still leaves
      > root and is still minted here, because it is a construction-time fault with no root twin.
      >
      > **ROUND-7 CORRECTION (D-B8).** This paragraph read *"no root change and no new import edge.
      > `ErrPayloadType`'s godoc is already domain-generic (*"a `Message[any]` payload cannot be asserted to
      > T"*), which is exactly what a result-type mismatch is."* Both halves are wrong. The quoted godoc is
      > `errors.go:6` verbatim and it is **specifically about a `Message[any]` payload** — an expression's
      > evaluated result is not one — so the godoc is not domain-generic, it is domain-*narrow*, and D-K
      > stretches the sentinel past it. Widening the comment **is** a root change; it is small, it is not
      > breaking, and it now has an owning checkbox instead of a claim that there was nothing to do.
      >
      > **Consequence for the counts:** root's projections in §9.5.0 and Task 12 are **unchanged** (root loses
      > both sentinels either way). The ***`expr`-module*** sentinel count is **1, not 2**.

      Every reinstated test's `errors.Is` target changes: the `ErrInvalidExpression` assertions move from
      `msgin.ErrInvalidExpression` to `expr.ErrInvalidExpression`, and the `ErrExprResultType` assertions
      become `msgin.ErrPayloadType`. `git show ab233d9:expr_test.go` asserts the root `msgin.Err*` form
      throughout, so **expect to rewrite the target in every one of the 12 functions** rather than copying
      them verbatim.

      **Godoc `expr.ErrInvalidExpression` with FRESH TEXT — do NOT copy the deleted root godoc.** What must
      survive is the *content*: the **construction-vs-evaluation split**, restated in the `msgin/expr:`
      module's own voice. Three things the comment must say:

      1. **What it is** — a **construction-time** fault raised at the provider call: the expression is
         empty, unparseable, or fails type-checking against `A`. The wrapped error carries the offending
         source text.
      2. **What it is NOT** — an evaluation-time fault. Those surface **per message**, as the endpoint's
         handler error. **Name the counterpart, and it is `msgin.ErrPayloadType`, not an `expr` sentinel**
         (revised D-K): an expression that evaluates to the wrong Go type returns
         `fmt.Errorf("%w: expr result %T is not %T", msgin.ErrPayloadType, got, want)`, which root's
         `IsPermanent` already classifies, so it is **never retried** — diverted to the invalid-message sink
         when one is configured, else single-shot to `RetryPolicy.DeadLetter`, else logged and discarded
         (ADR 0007 D7 as amended by **D-N**/**D-P**). *(Round-8 B8: this said only "the invalid-message sink",
         which is the non-default arm.)*
      3. **Why it is declared here and not in root** — the fault is the provider's; root has no notion of an
         expression and, after Task 1, no code that can produce one (D-I, §9.5.0). ADR 0019's
         *fail-at-construction* contract is about **where the error is raised**, not about who declares the
         value.

      > **ROUND-7 CORRECTION (X-B5) — the verbatim-recovery instruction is WITHDRAWN.** This step read
      > *"recover it from `git show 3d0b87a:errors.go`, lines 168–180, **before Task 9.5 deletes it**"*.
      > Executed literally it produces **two defects**. Both are visible in the command's own output, run
      > 2026-07-29:
      >
      > ```
      > $ git show 3d0b87a:errors.go | sed -n '168,180p'
      > 	// ErrInvalidExpression is the root error contract's construction-time fault
      > 	// for a runtime-authored expression: the expression is empty, unparseable, or
      > 	// fails type-checking against the payload type. The wrapped error names the
      > 	// offending expression. Runtime evaluation errors are NOT this — they
      > 	// propagate as the endpoint's handler error into the runtime's retry/DLQ path
      > 	// (see ErrExprResultType for the evaluation-time counterpart).
      > 	//
      > 	// It has no producer in this module. Expression-backed endpoints are supplied
      > 	// by the forthcoming separate msgin/expr provider module, which returns this
      > 	// sentinel so callers have a single, errors.Is-able construction-fault
      > 	// contract regardless of which package builds the endpoint. It is exported
      > 	// here, not in the provider, to keep that contract in one place.
      > 	ErrInvalidExpression = errors.New("msgin: invalid expression")
      > ```
      >
      > 1. **Line 6 names `ErrExprResultType`.** Pasting the block reintroduces that identifier into shipped
      >    `.go` source and **fails Spec 014 AC-10 arm 2** — `grep -rn --include='*.go' 'ErrExprResultType' .`
      >    *"must become EMPTY workspace-wide"*. Under revised D-K that sentinel does not exist anywhere; a
      >    godoc cross-reference to it is exactly the dangling-name staleness §8.1 arm 2 was built to catch.
      > 2. **The closing clause — *"It is exported here, not in the provider"* — is the exact premise D-I
      >    reversed.** Pasted into the provider, the comment asserts the opposite of the decision that put it
      >    there.
      >
      > **Moot as of this correction (round-7 X-M13):** the old *"before Task 9.5 deletes it"* urgency.
      > `git show 3d0b87a:` reads **history**, so nothing Task 9.5 does to the working tree can make that
      > text unreadable. The deadline was fiction, and it is removed rather than reworded.
- [x] `Release` returns `routing.ReleaseStrategy`, whose `(bool, error)` shape is what lets an evaluation
      failure propagate instead of being swallowed into a permanent `false`. `WithReleaseStrategy(expr.Release(…))`
      now compiles, which is the point of D-E.
- [x] **Reinstate the deleted `*Expr` test cases against the providers. The parity source of truth is git,
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
- [x] Runtime failures wrap the **source expression text** — the debuggability mitigation ADR 0029 §3 traded
      the interface shape for, so it is a requirement, not a nicety.

- [x] **A result-type mismatch returns root's `msgin.ErrPayloadType` — decision D-K, AS REVISED in round 6**
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

- [x] **THIS TASK OWNS THE `errors.go:6` GODOC WIDENING — decision D-K, round-7 D-B8.** Revised D-K stretches
      `msgin.ErrPayloadType` over a second producer class that its one-line godoc does not describe, and
      until this round **no task amended it**. Measured on the untouched tree:

      ```
      $ sed -n '6,7p' errors.go
      	// ErrPayloadType is returned when a Message[any] payload cannot be asserted to T.
      	ErrPayloadType = errors.New("msgin: payload is not of the expected type")
      ```

      An expression's evaluated **result** is not *"a `Message[any]` payload"*. The claim in
      [ADR 0029 §5.0b](../adrs/0029-eip-lexical-alignment.md) and [Spec 014 §7](../specs/014-core-package-layout.md)
      that the godoc *"is already domain-generic"* was **false against the source**; both are corrected in
      this pass and both now point here.

      **Why Task 10 and not Task 9.5, although 9.5 is the task that already edits `errors.go`.** D-K is
      Task 10's decision and Task 10 writes its only producer. If the widening landed in 9.5 the tree would
      sit, for two commits, with root documenting an expression-domain fault that nothing in the workspace
      can produce — precisely the orphaned-statement shape D-I exists to remove (and the shape the *old*
      `ErrInvalidExpression` godoc had: *"It has no producer in this module"*). Counter-rule 6: one owner
      holds a decision's statement, its consequence, its gate and its task. **Consequence:** Task 10's
      commit touches **root**, which is why its scope line below is no longer `feat(expr)` alone.

      Write it as:

      ```go
      	// ErrPayloadType is returned when a value cannot be asserted to the type the
      	// caller declared for it. It has TWO producer classes and errors.Is cannot
      	// tell them apart:
      	//
      	//   - PAYLOAD SIDE (this module) — a Message[any] payload cannot be asserted
      	//     to T: PayloadOf (payload.go), which wraps it as "want %T, got %T", and
      	//     the consumer's live-value and wire type assertions
      	//     (endpoint/consumer.go), which return it BARE.
      	//   - EXPRESSION SIDE (the msgin/expr provider module, and any future
      	//     CEL/starlark provider) — a compiled expression EVALUATED to a value
      	//     that is not the declared result type. Wrapped as
      	//     "expr result %T is not %T" (ADR 0029 §5.0b, decision D-K).
      	//
      	// Both classes are deterministic: the same input yields the same wrong type
      	// on every redelivery. IsPermanent names this sentinel (reliability.go), so
      	// neither is ever retried, and each is diverted WITHOUT a Permanent wrap —
      	// to WithInvalidMessageSink if one is configured, else single-shot to
      	// RetryPolicy.DeadLetter, else logged and discarded (ADR 0007 D7, as
      	// amended by D-N and D-P). That is D-K's whole reason for reusing this
      	// sentinel.
      	//
      	// ACCEPTED TRADE-OFF, not an absence: errors.Is(err, ErrPayloadType) does
      	// not separate the two, which buys callers ONE target instead of one per
      	// expression provider — but the two remedies are disjoint (fix the codec or
      	// the producing adapter, versus fix the expression). Match the string
      	// "expr result" to tell them apart: only the expression side carries a
      	// discriminator, so its ABSENCE is what identifies the payload side.
      	ErrPayloadType = errors.New("msgin: payload is not of the expected type")
      ```

      > **⛔ ROUND-8 CORRECTION (design B8) — the round-7 draft over-claimed on one clause and was falsified by
      > a same-round decision on another.**
      >
      > **(i) "Wrapped as `want %T, got %T`" was attached to all three payload-side producers; only ONE of the
      > three does it.** Verified at `7ee3fd6` (`grep -rn 'ErrPayloadType' --include='*.go' . | grep -v _test`,
      > then read each site):
      >
      > ```
      > payload.go:15:          return Message[T]{}, fmt.Errorf("%w: want %T, got %T", ErrPayloadType, *new(T), m.Payload())
      > endpoint/consumer.go:831:                       return zero, msgin.ErrPayloadType
      > endpoint/consumer.go:838:                       return zero, msgin.ErrPayloadType
      > ```
      >
      > `:831` (live-value assertion) and `:838` (wire `[]byte` assertion) return the sentinel **bare**, and the
      > trade-off's entire remedy is *"the error string carries the discriminator"*. A godoc promising a wrap
      > that two of three producers do not perform is the same shape of false claim this whole audit program
      > exists to catch. **RESOLUTION CHOSEN: narrow the sentence, do NOT wrap the two sites.** Wrapping them is
      > a behavior change to shipped code — a new error string, its own cases, its own Spec §2.1 row — inside a
      > task scoped to the `expr` module plus one root comment, and it buys D-K nothing: the discriminator that
      > separates the two classes is `"expr result"`, whose **absence** identifies the payload side regardless of
      > what the payload side wraps. *(Backlog, not silently absorbed: `consumer.go:831`/`:838` carry no type
      > information at all in their error string, which is a pre-existing debuggability gap on the decode path —
      > it predates D-K, is not created by it, and wants its own finding rather than a ride in this godoc.)*
      >
      > **(ii) "diverted to the invalid-message sink" is falsified by D-N, decided in the SAME round.** With no
      > `WithInvalidMessageSink` — **the default** — the message goes to `RetryPolicy.DeadLetter`, **single-shot**
      > under **D-P**, and only then to a logged discard ([ADR 0007 D7](../adrs/0007-reliability-settlement-api.md),
      > amended twice). The sentence now names the full ladder. *A round-7-drafted godoc contradicting a
      > round-7 decision — the join failure, now between two decisions inside one round.*
      >
      > **No gate moves.** All four gated phrases (`PAYLOAD SIDE`, `EXPRESSION SIDE`, `ACCEPTED TRADE-OFF`,
      > `expr result`) survive verbatim and each still sits within a single godoc line — constraint 2 below.

      Four constraints on the text, each load-bearing:
      1. **It must not name `ErrExprResultType`** — Spec AC-10 arm 2 requires that identifier to be empty
         workspace-wide, and `errors.go` is inside the sweep.
      2. **Each gated phrase must sit within one godoc line.** `go doc` reproduces the comment's own line
         breaks, so a `grep -q` for a phrase that spans one can never match (the same defect the coordinator
         fixed in Spec §8.0b's obligation-11 gate this round).
      3. **The cost is stated as an accepted trade-off, not omitted.** §5.0c's cost analysis was narrowed to
         moot for this half and nothing replaced it, leaving §5.0b listing only benefits — the identical
         silence-shaped defect that overturned the first D-K one round earlier.
      4. **`errors.Is` behavior is unchanged** — this is a documentation edit to a `//` comment. `apidiff`
         is unaffected (comments are not surface), so §9.5.0's and Task 12's arithmetic do **not** move.

      **RED at THIS task's start** (counter-rule 8 — nothing before Task 10 writes this text). This block is
      **diff-identical to Spec 014 AC-10's fifth arm**, which is the normative set; re-run verbatim
      2026-07-29 on the untouched tree:
      ```
      $ for p in 'PAYLOAD SIDE' 'EXPRESSION SIDE' 'ACCEPTED TRADE-OFF' 'expr result'; do
          go doc github.com/kartaladev/msgin.ErrPayloadType | grep -q "$p"; printf '%-20s exit=%s\n' "$p" "$?"
        done
      PAYLOAD SIDE         exit=1
      EXPRESSION SIDE      exit=1
      ACCEPTED TRADE-OFF   exit=1
      expr result          exit=1
      ```
      All four must print `exit=0` when the task is done. Four ANDed `grep -q`s, not a line count and not a
      single match. *(`go doc` on a `var` prints the whole `var` block, so these are package-scoped phrase
      matches; after Task 9.5 no other declaration in the block contains any of the four strings — the only
      other `expr`-flavoured godoc in it is the pair 9.5 deletes.)*

- [x] **Extend Spec §8.1 arm 2's DECLARED-SIDE loop with `expr` — round-7 X-B7 (Task-10 half).** Spec
      §8.1's command block already carries the instruction (*"TASK 10 MUST EXTEND THE LOOP WITH `expr` the
      moment `expr/` exists"*, round-6 E-B2) and §8.1's allow-list table records `ErrInvalidExpression` as
      **deliberately not allow-listed** for this reason — but the instruction landed only in the Spec's
      comment block and **nowhere executable**. This checkbox is the executable half. Append `expr` to the
      declared-side `for p in …` list, which is published in exactly **one** place — measured, not assumed
      (run 2026-07-29; the tail of the ten-directory list is what makes the loop unique):
      ```
      $ grep -rn 'adapter/http/stdlib; do' docs/specs/*.md docs/plans/*.md | sort
      docs/specs/014-core-package-layout.md:2018:        adapter/memory adapter/cron adapter/database/sql adapter/http adapter/http/stdlib; do \
      ```
      *(Other `for p in` hits in these files are the `symmap.tsv` regeneration loop and per-package loops
      over the five core packages. `expr` does **not** belong in those — `symmap.tsv` maps symbols that
      MOVED OUT OF ROOT, and no `expr` symbol was ever in root.)* The declared side goes from **eleven**
      package directories to **twelve**:

      ```
      for p in endpoint routing transform channel resilience \
               adapter/memory adapter/cron adapter/database/sql adapter/http adapter/http/stdlib expr; do
      ```

      **It cannot be added earlier**, which is why it is a Task 10 checkbox and not a Task 9.5 one —
      `decls.go` panics on a directory that does not exist (run 2026-07-29):
      ```
      $ go run docs/plans/027-tools/decls.go ./expr
      panic: open ./expr: no such file or directory
      exit status 2
      ```

      **Proof the extension is REQUIRED and SUFFICIENT** — measured 2026-07-29 by emulating this task's end
      state on the real tree (root's two expr sentinel blocks deleted with
      `sed -i '' '193,206d;168,180d' errors.go`, a stub `expr/errors.go` whose godoc opens
      `// ErrInvalidExpression is …` added, then both restored):
      ```
      === arm 2, loop WITHOUT expr (as published today) ===
      ErrInvalidExpression
      WithRelease
      === arm 2, loop WITH expr appended (the Task 10 extension) ===
      WithRelease
      ```
      Without the extension the sweep reports `ErrInvalidExpression` as a survivor **on a correct tree** — a
      false positive from the arm's known comment-side-tree-wide / declared-side-fixed-list asymmetry, not a
      real staleness. **Do NOT allow-list it instead**; that would hide every future staleness in the `expr`
      module too (Spec §8.1's allow-list table, `ErrInvalidExpression` row).

      *(`WithRelease` is `routing/aggregator.go:316`, the one genuine survivor, and it is Task 9.5's to
      clear — it is shown here only to prove the command ran and the rest of the sweep was unperturbed.)*

**Hot-path branches needing a case each:** invalid expression → `expr.ErrInvalidExpression` at construction;
valid expression, wrong result type **→ asserted `errors.Is(err, msgin.ErrPayloadType)` AND
`msgin.IsPermanent(err) == true`, and asserted to reach the INVALID-MESSAGE sink rather than the dead-letter
sink, with the retry count unchanged** (revised D-K — this is the acceptance bar that needs a real consumer +
`RetryPolicy` + DLQ fixture inside this module's tests, and it is why the task is sized **L**); runtime
evaluation error carrying the expression text; nil/empty expression string; `Release`'s runtime error
surfacing through `Handle` rather than returning `false`; **`RouteFunc`'s two construction validations**;
`toGroupEnv`'s empty-group and non-`A`-member guards.

**Verify:** ADR 0019's fail-at-construction contract holds — an invalid expression errors at the provider
call, never at first message. All **eight** modules green standalone under `GOWORK=off`. **Spec 014 AC-10's
four grep arms all pass**, including arm 2 (`ErrExprResultType` empty workspace-wide) — the arm the withdrawn
verbatim-recovery instruction would have broken — plus AC-10's fifth arm, the **four** `go doc … ErrPayloadType`
phrase gates (`PAYLOAD SIDE`, `EXPRESSION SIDE`, `ACCEPTED TRADE-OFF`, `expr result`), each an independent
`grep -q`, all four `exit=0`. *(Round-8, gate minor: this said **three**, while the checkbox above, the RED
transcript beside it and Spec AC-10 all publish four. The fourth — `expr result` — is deliberately the
error-string discriminator, i.e. the one a shortened list drops and the one the trade-off's remedy depends
on.)* **Both arms of the §8.1 staleness sweep empty with the twelve-directory declared-side loop.**

**Commit:** `feat(expr,core): expression providers as a separate module; widen ErrPayloadType's contract`

```
Spec: 014
Plan: 027
ADR: 0029
RFC: 0002
RFC: 0003
```

*(Round-7 X-M12. ADR 0029 §5.0a–c owns both halves of this task — the `expr` module and `ErrPayloadType`'s
widened contract.)*

> **ROUND-7 (D-B8) — the scope gained `core` and the subject gained a second clause.** This task now edits
> **root's `errors.go`** (the `ErrPayloadType` godoc widening above), so a `feat(expr)`-only scope would name
> one of the two modules the commit touches. It is **not** breaking: widening a doc comment adds no symbol
> and removes none, so no `!` — verify with `apidiff`, which must report the same counts §9.5.0's table
> projects for the post-D-I/post-D-J tree.

---

## Task 11 — Package docs AND the unowned godoc obligations · **M** · PARTIAL

> **This task grew in round 3.** Spec 014 **§8's nine godoc bullets** and **§10's four multi-instance godoc
> obligations** were written in the indicative, as though they described the tree, and **had no owning task
> at all** — so nothing in this plan was ever going to produce them. Audited against HEAD, **five of the nine
> and two of the four were unmet.** Every one now has an owning task and a `go doc` gate.
>
> **Round-8 C4 — Task 11 does NOT own all of them, and this task's checkbox list says which are its.** A godoc
> obligation is owned by the task that **creates the symbol it documents**: §8's obligations **10, 11, 11a, 12,
> 13** belong to **Task 9.6** and obligation **4's four new behavior types** to **Task 9**, both of which now
> run those gates in their own Verify. Task 11 writes the seven whose symbols already exist and **re-verifies**
> the other nine. Spec §8 carries the owner table; the §11 pinning table carries the same split by gate id.

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

Each line pairs the edit with the **id of the gate** that proves it. **A checkbox never restates a command**
— the [§11 gate block](#11-gate-block--the-one-source-for-every-11b11c-gate-red-at-each-gates-own-tasks-start)
below is the single source, and every id cited here resolves there. *(Round-8 C1: the checkboxes used to carry
their own `→ <command>` arrows, which made them a second, silently-diverging copy. Round 7 fixed the block and
left five arrows on the pre-round-7 forms. Cite ids, never commands.)*

**Run the §11 gate block FIRST and paste it into the ledger, then start editing** — but read the per-task
pinning table under it before reading anything into a GREEN: eleven of the sixteen gates are turned green by
Tasks 9 and 9.6, not here.

> **TASK 11 MUST RUN AFTER TASK 9.6.** Spec §8 obligations **10–13** document symbols that do not exist until
> 9.6 lands (`ExclusiveSubscribable`, `ErrSharedReplyChannel`, `WithSharedReplyChannel`). Ordering matters:
> running 11 first leaves five obligations (10, 11, 11a, 12, 13) unverifiable — `go doc` cannot read a symbol
> that has not been declared.

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

#### §11 gate block — the ONE source for every 11b/11c gate; RED at each gate's OWN task's start

> **ROUND-8 CORRECTION (C5) — this heading used to read *"run this BEFORE any edit; every line must print
> `RED`"*, which contradicts the per-task pinning table directly beneath it.** Eleven of the sixteen gates are
> **not** Task 11's to turn green, and five of those (8.10–8.13) are **expected GREEN on arrival** — written by
> Task 9.6. A worker taking "every line must print RED" literally at Task 11's start cannot proceed. The
> all-16-RED transcript below is pinned to the **untouched** tree and is a historical baseline for the whole
> program, not an entry condition for Task 11. **Read the pinning table below it before running anything.**

```bash
# ==== CANONICAL GATE BLOCK (Global Constraint 10) — keep the six shared ids diff-identical to Spec 014 §8.0b
g() { if eval "$2" >/dev/null 2>&1; then echo "GREEN: $1"; else echo "RED: $1"; fi; }
M=github.com/kartaladev/msgin
# `d` NORMALIZES `go doc` output before matching, and it has to — measured, see ADR 0030 §1:
#   * interface METHOD comments print VERBATIM, `//` markers and source line breaks intact;
#   * func / type / var comments are RE-WRAPPED at ~76 columns, per block.
# Either shape can split a gate phrase across a line, producing a FALSE RED. `d` strips the
# markers and folds all whitespace to single spaces, so no phrase can be split by a line break.
d() { go doc "$1" 2>/dev/null | sed 's,//, ,g' | tr -s '[:space:]' ' '; }
g 8.10 "d \$M.SubscribableChannel | grep -q 'ExclusiveSubscribable'"
g 8.11 "d \$M.ExclusiveSubscribable | grep -q 'MUST NOT compute it from a live subscriber count' && \
        d \$M.ExclusiveSubscribable | grep -q 'reaches at most one recipient' && \
        d \$M.ExclusiveSubscribable | grep -q 'any recipient other than the single subscriber registered on it' && \
        d \$M.ExclusiveSubscribable | grep -q 'recipient in another process' && \
        d \$M.ExclusiveSubscribable | grep -q 'constant for the lifetime' && \
        d \$M.ExclusiveSubscribable | grep -q 'safe for concurrent use' && \
        d \$M.ExclusiveSubscribable | grep -q 'MUST NOT block and MUST NOT panic'"
g 8.11a "d \$M.ExclusiveSubscribable | grep -q 'promotion'"
g 8.12 "d \$M/endpoint.NewChannelExchange | grep -q 'ErrSharedReplyChannel' && \
        d \$M/endpoint.NewChannelExchange | grep -q 'ErrChannelSubscribed' && \
        d \$M/endpoint.NewChannelExchange | grep -q 'does not implement' && \
        d \$M/endpoint.NewChannelExchange | grep -q 'within this process'"
g 8.13 "d \$M/endpoint.WithSharedReplyChannel | grep -qi 'suppress' && \
        d \$M/endpoint.WithSharedReplyChannel | grep -q 'ErrChannelSubscribed'"
g 11c1 "d \$M/channel.WithSingleSubscriber | grep -Eqi 'single-process|per-process'"
# ---- the ten gates below are Plan-only (no Spec §8.0b counterpart); the six above are the shared set ----
g 8.1  "grep -rn -i 'correlation identifier' --include='*.go' ."
g 8.3  "grep -rn -i 'amqp' --include='*.go' . | grep -q 'spi.go'"
g 8.4a "d \$M/routing.CorrelationStrategy | grep -qi 'spring'"
g 8.4b "d \$M/routing.ReleaseStrategy | grep -qi 'spring'"
g 8.4c "d \$M/routing.Predicate | grep -qi 'spring'"
g 8.4d "d \$M/routing.RouteFunc | grep -qi 'spring'"
g 8.4e "d \$M/routing.SplitFunc | grep -qi 'spring'"
g 8.4f "d \$M/transform.Transformer | grep -qi 'spring'"
g 8.7  "grep -q -i QueueChannel adapter/http/inbound.go && grep -q -i QueueChannel adapter/http/stdlib/inbound.go"
g 11c2 "d \$M.RetryPolicy | grep -Eq 'per instance|N × MaxAttempts'"
```

**Pasted output of exactly that block, re-run 2026-07-30 on the untouched tree at `7ee3fd6`** (code
byte-identical to the `dadc775` pin) — the historical all-RED baseline, **not** Task 11's entry condition:

```
RED: 8.10
RED: 8.11
RED: 8.11a
RED: 8.12
RED: 8.13
RED: 11c1
RED: 8.1
RED: 8.3
RED: 8.4a
RED: 8.4b
RED: 8.4c
RED: 8.4d
RED: 8.4e
RED: 8.4f
RED: 8.7
RED: 11c2
```

> **ROUND-8 CORRECTION (C3) — the `d` normalizer is ADOPTED here and in Spec §8.0b, and ADR 0030's stated
> reason for it is CORRECTED.** ADR 0030 §1 published this pipe as though both gate documents already used it;
> neither did (one hit repo-wide: the sentence itself). It is adopted because **the first half of its reason is
> true and measured**, and dropped-in it changes no verdict. Measured 2026-07-30 in a throwaway probe module
> with ADR 0030 §1's godoc pasted verbatim and a four-outcome `NewChannelExchange` godoc written to satisfy
> obligation 12 — **all 14 conjuncts of 8.10/8.11/8.11a/8.12/8.13 MATCH under BOTH forms**, so adopting it
> flips nothing, while phrases that span a source line break match only under the pipe:
>
> ```
> INCLUDING a recipient in another process     raw=NO     piped=MATCH     (interface method comment)
> MUST therefore return false                  raw=NO     piped=MATCH     (interface method comment)
> the probe at all; any wrapper                raw=NO     piped=MATCH     (func comment, re-wrapped)
> ```
>
> **The ADR's stated trigger was half-wrong, and the corrected one is this:** it claimed a func-comment gate
> *"flips MATCH→NO-MATCH when the **preceding sentence** changes length"*. `go doc` re-wraps **each block
> independently**, so a length change in a preceding paragraph or list item cannot move the phrase's line
> breaks — **0 of 46** perturbations of the intro paragraph flipped `grep -q 'does not implement'`, reproducing
> round 8's measurement. Perturbing text **inside the phrase's own wrapped block** flips it **18 of 46**
> (first at a 23-character shift). The hazard is real; it is *same-block* edits, not preceding ones. ADR 0030
> §1 is corrected to say so.

> **ROUND-8 CORRECTION (C1) — the per-checkbox gate arrows are DELETED; each checkbox now cites a gate id.**
> Task 11b's preamble says *"each line pairs the edit with the command that proves it"*, so the arrows were the
> **instruction** while this block was the transcript — **two copies of one artifact**, and round 7 fixed only
> the copy it was looking at. The arrows were left on the pre-round-7 set: the line-counting obligation-12
> form, 8.11 with **two** conjuncts where the obligation has **seven**, 8.13 missing `ErrChannelSubscribed`,
> `grep -qi instance` for 11c2, and **no 8.11a checkbox at all**. None self-satisfied, so this was never a
> false GREEN — it was a **weaker-instruction path**: write the two phrases the arrow names, then fail the
> Verify with no diagnosis. **There is now exactly one source.** A checkbox cites `→ gate <id>, §11 block`;
> it never restates a command.

Most are RED for the *"symbol does not exist yet"* reason, which `go doc` reports explicitly —
`doc: no symbol ExclusiveSubscribable in package github.com/kartaladev/msgin` (8.11, 8.11a),
`doc: no symbol WithSharedReplyChannel in package …/endpoint` (8.13), and the same for the four Task 9 types
(8.4c–8.4f). 8.12 is RED because `does not implement` and `within this process` are absent from
`NewChannelExchange`'s godoc today; the pre-existing `ErrChannelSubscribed` prose at
`endpoint/exchange.go:210-211` satisfies only one conjunct.

> **ROUND-7 CORRECTION (R-B3 / X-B8 / D-M1 / X-M7).** This block is now **diff-identical in coverage to Spec
> §8.0b**, which is the normative set. Four defects were fixed:
>
> - **8.11 had two conjuncts where the spec has seven**, and the two it dropped were *precisely D-L's
>   substance* — the end-to-end predicate and the cross-process MUST. An implementer could have written the
>   **superseded handle-local godoc**, added "constant for the lifetime" and "safe for concurrent use", and
>   turned every gate they actually ran GREEN while shipping a contract that contradicts D-L. Combined with
>   Task 9.6's stale checkbox (R-B1, corrected above), that was a complete green-but-wrong path.
> - **8.11a did not exist here at all** while the Risks table claimed Task 11b/11c owned all thirteen
>   obligations — §8's founding failure mode (an obligation with no owning gate) reproduced inside the fix
>   for it.
> - **8.12 counted LINES, not matches.** `grep -c 'A\|B\|C\|D' … -ge 4` yields a **false RED** whenever two of
>   the four phrases wrap onto one `go doc` line, and a **false GREEN** if one phrase appears on four lines.
>   It is now an AND of four independent `grep -q`s, the shape 8.11 already used. **Spec §8.0b is corrected to
>   match.**
> - **11c2 matched a single incidental word** — `grep -qi instance` is satisfied by *"for instance"* or *"this
>   instance"*. That is the exact class round 6 rejected for the old §8.11 ("concurrent" matching
>   incidentally). Tightened to `per instance|N × MaxAttempts`, the phrase Spec §8.0a(d) already uses.
>
> **Standing check — now [Global Constraint 10](#global-constraints), with an executable `diff`:** this block
> and Spec §8.0b must stay **diff-identical on the six shared gate ids**. They diverged silently because both
> sets were RED, so nothing caught it until an auditor built the comparison table by hand. *(Round-8 C2: the
> round-7 pass wrote this sentence and never created the constraint — the list ran 0–9. It exists now, and it
> carries the command.)*

> **ROUND-7 CORRECTION (X-B8), AS AMENDED BY ROUND 8 (C4/C5) — the gates are pinned PER TASK, and the tasks
> that turn them green are the tasks that WRITE the godoc.** The block above is RED on the untouched tree. But
> Task 11 runs **after Task 9.6** (stated at the head of this task), and Task 9.6 writes the very godoc that
> 8.10, 8.11, 8.11a, 8.12 and 8.13 check. **At Task 11's actual start those five are expected to be GREEN.**
>
> **The rule (round-7 counter-rule 8): a RED baseline is pinned to the tree at ITS OWN task's start, not to
> the untouched tree.** Split accordingly — and note the **owner** column, which round 8 (C4) had to add
> because four documents disagreed about who owns obligations 10–13:
>
> | Gate | RED at | Turned GREEN by — the task that WRITES the godoc | Task 11's role |
> |---|---|---|---|
> | 8.10 · 8.11 · 8.11a · 8.12 · 8.13 | **Task 9.6's** start | **Task 9.6** — it creates all three symbols and rewrites `NewChannelExchange`'s godoc; §8 obligations 10–13 are its acceptance criteria, and its Verify runs these five gates | re-run as a **no-regression check** |
> | 8.4c · 8.4d · 8.4e · 8.4f | **Task 9's** start | **Task 9** — the four named behavior types do not exist before it, and its "every type's godoc names its Spring equivalent" checkbox is what writes them; its Verify runs these four gates | re-run as a **no-regression check** |
> | 8.1 · 8.3 · 8.4a · 8.4b · 8.7 · 11c1 · 11c2 | **Task 11's** start | **Task 11** — the only gates Task 11 itself turns green | RED → GREEN, both transcripts |
>
> **The ownership resolution (round-8 C4), stated once so every other document can cite it:** a godoc
> obligation is owned by the task that **creates the symbol it documents**, because Go has no state in which an
> exported symbol exists without its doc comment — Task 11 cannot be "the writer" of a godoc that Task 9.6 must
> already have written in order to commit a green unit. Task 11 owns the obligations whose symbols **already
> exist** (1, 3, 4-for-the-two-shipped-types, 7) plus §10's two, and **re-verifies** the rest. The structural
> cause of the contradiction was that **Task 9.6's Verify contained no `go doc` gate at all**, so nothing
> measured the two root symbols and the normative godoc it is the sole writer of; that is fixed in Task 9.6.

> **OBLIGATIONS 10–13 ARE WRITTEN BY TASK 9.6, NOT HERE (round-8 C4).** The five checkboxes below are Task
> 11's **no-regression re-verification** of godoc Task 9.6 already committed — the symbols cannot exist
> without it. They are listed here because §8 is audited per obligation and this is where a reader looks for
> the obligation set; the *writing* checkbox for each lives in Task 9.6, and its content spec is Spec §8
> obligation 10/11/11a/12/13. **If a gate is RED on arrival, that is a Task 9.6 regression — do not write the
> godoc here.** Task 11's own RED → GREEN work is the eight checkboxes under "§8.1" onward plus 11c.

- [ ] **§8.10 — `SubscribableChannel`'s godoc cross-references `ExclusiveSubscribable`** (D-J). Without it the
      optional capability is undiscoverable from its own supertype, and a third-party channel author never
      learns the probe exists — leaving the accept-unknown arm permanent for exactly the fan-out-capable
      channels ADR 0030 §Topology's second topology describes.
      **Written by Task 9.6** → verify with **gate 8.10, §11 block** (expected GREEN on arrival).
- [ ] **§8.11 — `SingleSubscriber`'s godoc states the END-TO-END definition (D-L revised), requires
      INVARIANCE, requires concurrency safety, and forbids blocking/panicking (D-O).** **Five parts (a)–(e)**,
      per Spec §8 obligation 11; the gate asserts **seven phrases independently**. *(Round-8 correction: this
      said "All three, and the gate asserts two of them" — a count left over from before D-L was revised and
      D-O added. Cite the obligation, never a count.)*
      - it reports whether **every message sent to this channel reaches at most one recipient, counted across
        every process** — a statement about the channel's **policy**, not its live subscriber count; an
        implementation **MUST NOT** compute it from a subscriber count. A channel **MUST return `false`**
        whenever a message can be received by any recipient other than its single registered subscriber,
        **including one in another process** — a broadcast broker subject, a Redis pub/sub channel or an SSE
        stream fanned out to N instances therefore reports `false` even when its local handle admits one
        subscriber. A broker-backed channel **MAY** report `true` only when the broker guarantees the
        destination is private to this process's subscription (a per-instance NATS `_INBOX`, an exclusive
        auto-delete AMQP reply queue) — that is Return Address, and it is what an honest `true` means;
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
      **Written by Task 9.6** (verbatim from ADR 0030 §1) → verify with **gate 8.11, §11 block** — **seven**
      conjuncts, expected GREEN on arrival.
- [ ] **§8.11a — the same godoc states that EMBEDDING CUTS BOTH WAYS** (D-L; Spec §8 obligation **11a**).
      Method promotion is the **hazard** as well as ADR 0030 §5's remedy: a type embedding
      `*channel.DirectChannel` or `*channel.PublishSubscribeChannel` reports on the **embedded** channel even
      when it overrides `Subscribe` with its own fan-out. Compile-proven: `struct{ *PublishSubscribeChannel }`
      reports `true` while its own `Subscribe` fans out to 2.
      **Written by Task 9.6** → verify with **gate 8.11a, §11 block** (expected GREEN on arrival).
      *(Round-8 C1: this obligation had **no checkbox in this task at all**, while the Risks table claimed
      Task 11 owned every one — §8's founding failure mode reproduced inside the fix for it. The gate existed
      in the §11 block from round 7; only the checkbox was missing.)*
- [ ] **§8.12 — `NewChannelExchange`'s godoc states FOUR outcomes and enumerates `ErrChannelSubscribed`.**
      rejected · accepted-exclusive · accepted-no-probe · **accepted but exclusive only within this process**.
      `ErrChannelSubscribed` is returned unwrapped from `reply.Subscribe` (`endpoint/exchange.go:250`) and is
      absent from the doc's error list (`:221-224`, which names only `ErrNilChannel`,
      `ErrInvalidReplyTimeout`, `ErrNilSubscription`), so a caller cannot write correct handling from the doc.
      The accepted-no-probe arm must also carry D-L's wrapper caveat: *a reply channel that wraps another by
      embedding the `msgin.SubscribableChannel` interface does not inherit `SingleSubscriber`, so it is
      accepted here even when the channel it wraps would be rejected.*
      **Written by Task 9.6** → verify with **gate 8.12, §11 block** — **four** conjuncts, expected GREEN on
      arrival. *(The arrow deleted here was the line-counting `grep -c … -ge 4` form that round 7 replaced in
      the block and did not replace here — round-8 C1.)*
- [ ] **§8.13 — `WithSharedReplyChannel`'s godoc says it SUPPRESSES THE PROBE, not that it confers
      shareability.** On a `DirectChannel` the second exchange still gets `ErrChannelSubscribed`; neither the
      option's name nor that sentinel's text hints the option cannot help. *(This wording is only true if the
      guard tests `cfg.allowShared` **first** — see the note in Task 9.6.)*
      **Written by Task 9.6** → verify with **gate 8.13, §11 block** — **two** conjuncts (the arrow deleted
      here named only `suppress` and dropped `ErrChannelSubscribed`), expected GREEN on arrival.

**Task 11's own RED → GREEN work starts here.**

- [ ] **§8.1 — name Correlation Identifier.** "Return Address" is present; the *in-process* pattern is never
      named. Add it to `endpoint`'s `ChannelExchange`/`doc.go` prose.
      → **gate 8.1, §11 block.**
- [ ] **§8.3 — the AMQP disclaimer on `RequestReplyExchange`.** Currently **absent workspace-wide**, despite
      Spec 014 §6 and ADR 0029 §2 both asserting in the present tense that it exists. The gate requires the hit
      to be in `spi.go`, not merely *somewhere* — that assertion used to live in prose beside a weaker command.
      → **gate 8.3, §11 block.**
- [ ] **§8.4 — every named behavior type names its Spring equivalent, per type.** Currently only the *package*
      docs name Spring; `routing.CorrelationStrategy` and `routing.ReleaseStrategy` do not. This is the
      mitigation that justifies dropping the Spring names (ADR 0029 §4), so it is **not** discharged by a
      package-level mention. **Task 11 writes the two shipped types** (`CorrelationStrategy`,
      `ReleaseStrategy`); **Task 9 writes its own four** (`Predicate`, `RouteFunc`, `SplitFunc`,
      `Transformer`) and its Verify runs their gates.
      → **gates 8.4a–8.4b, §11 block** (this task) and **gates 8.4c–8.4f** (Task 9's, re-run here as a
      no-regression check). *(The old arrow's `grep -B10 'type <T>' <file>` window is long gone; `go doc`
      needs neither a file name nor a window.)*
- [ ] **§8.7 — `msghttp.ServeAsync` and `stdlib.NewInbound` state the widened `target` contract.** Neither
      godoc mentions that any `MessageChannel` — a durable `QueueChannel`, a `PublishSubscribeChannel`, any
      `OutboundAdapter` — now qualifies, which is the whole user-visible payoff of §5.0 rows 7–8. The gate
      requires **both** files to hit, which is why it is an `&&` of two file-scoped greps rather than one
      `grep -rn`.
      → **gate 8.7, §11 block.**

### 11c — Spec 014 §10's unmet multi-instance obligations (CLAUDE.md mandatory)

- [ ] **`channel.WithSingleSubscriber` states it is a SINGLE-PROCESS guard.** ADR 0028 §6.2 requires it in so
      many words — *"must not be documented as a distributed exclusivity guarantee"* — and the godoc
      (`channel/pubsub.go:66-82`) never mentions the process boundary. Two instances each holding their own
      `PublishSubscribeChannel` still each accept a subscriber.
      → **gate 11c1, §11 block** — the one gate id shared with Spec §8.0b's block that is *not* a D-J
      obligation (there it is labelled §8.0a obligation (c); the id is the same in both, by Global
      Constraint 10).
      *(Was `grep -A22 'func WithSingleSubscriber' channel/pubsub.go | grep -i …` — **unsatisfiable**: the
      body is one line, so `-A22` read 22 lines of the NEXT declarations. Also note the old text cited the
      godoc as `:69-83`; it is `:66-82`.)*
- [ ] **`RetryPolicy.MaxAttempts` states the per-instance bound.** `retry.go:37-41` says nothing about
      topology, so a caller sizing a poison-message threshold behind a load balancer gets `N ×` what they
      asked for. Say: attempts are tracked per instance (`endpoint/attempts.go:26`), so across N nodes the
      effective global bound is `N × MaxAttempts`; this applies **only** to sources without a native
      delivery-count header (a `NativeReliability` source is unaffected); the distributed answer is the
      broker's own redelivery count or a shared idempotency/dedup store.
      → **gate 11c2, §11 block** — `per instance|N × MaxAttempts`, **not** the old arrow's
      `grep -qi instance`, which *"for instance"* or *"this instance"* satisfies incidentally (round-7 X-M7;
      the tightening reached the block and not this checkbox — round-8 C1). *(`go doc` on the struct prints
      the field doc comments too — verified — so no `-B` window is needed.)*

**Verify:**
```bash
for p in . endpoint routing transform channel resilience; do
  n=$(grep -l '^// Package ' $p/*.go 2>/dev/null | wc -l | tr -d ' ')
  [ "$n" = 1 ] || { echo "FAIL $p has $n"; exit 1; }
done                                   # silent — this is the check, NOT `go vet` (Global Constraint 3)
```
Plus the **§11 gate block re-run to all-GREEN**, with **both** transcripts pasted into the ledger. Per the
pinning table above, the two transcripts differ by group:

| Group | Before Task 11 | After Task 11 |
|---|---|---|
| 8.10 · 8.11 · 8.11a · 8.12 · 8.13 (Task 9.6's) | **GREEN** — a RED here is a Task 9.6 regression, not work for this task | GREEN |
| 8.4c · 8.4d · 8.4e · 8.4f (Task 9's) | **GREEN** — same | GREEN |
| 8.1 · 8.3 · 8.4a · 8.4b · 8.7 · 11c1 · 11c2 (Task 11's own) | **RED** — this is Task 11's RED baseline | GREEN |

*(Round-8 C5: this said *"the §11 RED baseline block re-run to all-GREEN … the RED before"*, i.e. all sixteen
RED on arrival — which the pinning table directly above it contradicts. A "before" transcript still proves
what round-6's counter-rule 4 wants it to prove; it just is not all-RED.)* A gate with only an "after"
transcript proves nothing — that is round-6's counter-rule 4, and it is the reason three of these four gates
shipped decorative. `go vet ./...` clean.

**Commit:** `docs(core): package docs and the godoc obligations Spec 014 §8/§10 require`

```
Spec: 014
Plan: 027
ADR: 0027
ADR: 0028
ADR: 0030
RFC: 0001
RFC: 0002
```

*(Round-7 X-M12. This task writes the godoc that Spec §8/§10's obligations demand: the package docs of the
ADR-0027 layout, ADR 0028's segregated channel interfaces, and ADR 0030's exclusivity probe. RFC 0001 backs
ADR 0027; RFC 0002 backs ADRs 0028 and 0030.)*

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
- [ ] **CLAUDE.md, same commit** (traceability, non-optional). **FIVE sites, enumerated — the first two were
      the whole checkbox until Task 10's fix rounds found sites three through five unowned** (see the round-6
      correction below, which is about the first two only):
      1. the **"Commands" section's** *"`./...` does NOT mean 'the repo'"* block — it must name the
         **eighth** module (`expr`) and its package, and its two per-module loops must run **eight**
         directories, not seven. The **"Until Task 10 lands, run seven locally"** preamble is now false and
         goes with it.
      2. the **"Architecture blueprint"** section — `expr` joins the shipped-adapter list, and the
         *"26 plans / 25 ADRs / 13 specs"* counts move with Spec 014 / ADRs 0027–0030 / Plan 027.
      3. **the "Dependency policy" section's *"Still outstanding"* sentence** — *"Task 10 has the `expr`
         module declare **`ErrInvalidExpression`** itself"*. **Task 10 DID that**, so the
         sentence is false as written; replace it with the delivered state (one sentinel, `msgin/expr:`
         prefix, `expr-lang` confined to `expr/go.mod` + `expr/go.sum`). *(Task 10's worker correctly
         deferred this edit here rather than reaching into CLAUDE.md — but this checkbox did not cover it, so
         a Task 12 worker would have skipped it. Fix round 1.)*
      4. the same section's CI-gap paragraph — *"**Plan 027 Task 10 adds `crontest` to both CI jobs**"* — is
         delivered; `grep -c 'dir:' .github/workflows/ci.yml` is now **8** and the comment-stripped
         `crontest` grep is non-zero on both jobs.
      5. **the same section's SENTINEL and EXPORTED-SYMBOL counts** — *"taking root from **43 → 41**
         sentinels and **102 → 100** exported symbols (`apidiff` removals 95 → 97)"*. That sentence is
         accurate **for Task 9.5 in isolation** and stale as a statement about the branch: Task 9.6 (D-J) then
         added `ErrSharedReplyChannel` + `ExclusiveSubscribable`, and Task 10's fix round 2 added
         `ErrNilMessageGroup`. The delivered tree measures **103 exported / 43 sentinels / apidiff 97
         removals + 9 additions** — re-measure with the §Task-12 commands rather than transcribing this
         figure, and reconcile the sentinels **by name**, since 43 at `dadc775` and 43 today are different
         sets.

      **Cite CLAUDE.md by SECTION, never by line number** — they rot on every edit, and this checkbox is the
      proof.

      **Also re-check every ADR STATUS block in the same pass.** Task 10 falsified three in ADR 0029 and
      fixed them (§5.0a's *"Task 10 still owes the one replacement"*, §5.0b's *"DECIDED, NOT YET
      IMPLEMENTED"*, and §5.0c's cost table naming `"want %T, got %T"` as the payload-side discriminator).
      **Task 10's round-3 sweep also found TWO stale blocks it does NOT own — they belong to Task 9.6, which
      is DONE, and they were already stale before Task 10 began** (last touched at `c4582ba`):

      - [`docs/adrs/0028-channel-interface-segregation.md:364`](../adrs/0028-channel-interface-segregation.md)
        — *"**NOT YET IMPLEMENTED.** … at `dadc775` the probe does not exist"*. The probe shipped with
        Task 9.6.
      - the same file at `:401` — *"**NOT YET IMPLEMENTED** — Plan 027 Task 9.6"*.

      Sweep command below. **Three things in it are deliberate; do not "simplify" any of them.**

      1. **The vacuity probe.** A bare `grep` over an unquoted shell variable does **not** word-split in zsh:
         `grep` receives one long non-existent filename, fails, and an `|| echo CLEAN` fallback prints a
         pass. That false CLEAN is how this class hides. `set --` + `"$@"` fixes it; the probe proves it.
      2. **Frozen files are EXCLUDED.** `docs/plans/027-audit-round-*.md` and the derivation
         ledger/brief record what was true **at a past commit** and must never be "fixed" — editing them
         destroys the audit trail. Run without the exclusion, `027-derivation-findings.md:3737` hits on a
         quoted historical status and reads as a live defect.
      3. **`DECIDED, NOT` and `still panics` were REMOVED from the pattern.** `DECIDED, NOT` is fully
         redundant with `NOT YET IMPLEMENTED` (the status block reads `STATUS: DECIDED, NOT YET
         IMPLEMENTED`) and, case-insensitively, matches ordinary English — *"decided, not incidental"*
         (`docs/adrs/0028…:222`) and *"(decided, not open)"* (`docs/plans/011-cron-source.md:2457`).
         `still panics` matches correct prose documenting a real limitation (ADR 0029 §5.0d's typed-nil
         caveat). Both produced pure noise; the pattern is case-SENSITIVE for the same reason.

      ```bash
      set -- $(ls docs/plans/*.md docs/specs/*.md docs/adrs/*.md docs/HANDOVER.md \
                 | grep -vE 'docs/plans/027-(audit-round-[0-9]+|derivation-findings|derivation-brief)\.md')
      grep -c "Task 10" "$@" | grep -v ':0$' | head -3    # PROBE: must print at least one line
      grep -nE "NOT YET IMPLEMENTED|still owes|still declares" "$@"
      ```

      **EXPECTED OUTPUT — how to tell a real hit from a known one.** A hit in
      **`docs/plans/027-core-package-layout.md` is THIS CHECKBOX quoting its own search strings** — ignore
      it, and expect it to move as the file is edited. A hit **anywhere else is real.** Today that is
      exactly two, both listed above (`docs/adrs/0028-channel-interface-segregation.md:364` and `:401`).
      When those two are fixed, every remaining line should be in this file.

      > **ROUND-6 CORRECTION (M-1).** This checkbox was stale three separate ways at `aae6160`:
      > (1) *"dependency policy drops `expr-lang` and **keeps `robfig`**"* — **already done**; (2) *"the
      > `FilterExpr`/`RouterExpr` mention at `CLAUDE.md:235` goes with it"* — those names, and
      > `StreamingSource`, are **already gone**
      > (`grep -n 'FilterExpr\|RouterExpr\|StreamingSource' CLAUDE.md` → exit 1, no output), and `:235` is now
      > inside the dependency-policy bullet list, nowhere near what the checkbox described; (3) the
      > `./...`-is-not-the-repo block was **already updated in round 5** and correctly describes a 7-module
      > workspace today. Only the **eighth module** remains, and it lands with Task 10's `go.work` + CI edits.
- [ ] **Backfill the final commit SHAs, and cite by TASK until then.** Task 10 was amended three times, and
      each amend destroyed the SHA the previous round's prose had cited, and both are now unreachable
      (`git merge-base --is-ancestor <sha> HEAD` fails; a fresh clone cannot resolve either). The two values
      are recorded in Task 10's fix-round report and are deliberately NOT repeated here — quoting a dead SHA
      inside the checkbox makes the sweep below report the checkbox's own prose, which is the `grep crontest`
      trap in `ci.yml` all over again.
      **A commit cannot cite its own SHA**, so every in-bundle reference to Task 10's commit now names the
      TASK. Once the branch is final, sweep `docs/` for task-named citations that should carry a SHA and add
      them — and check reachability for every SHA already written down, not just the ones you add:

      ```bash
      for sha in $(grep -rhoE '\b[0-9a-f]{7,40}\b' \
                     docs/*.md docs/*/*.md docs/*/*/*.md | grep -E '[a-f]' | sort -u); do
        if ! git cat-file -e "$sha^{commit}" 2>/dev/null; then
          echo "UNRESOLVABLE: $sha"     # not in this clone AT ALL — the fresh-clone case
        elif ! git merge-base --is-ancestor "$sha" HEAD 2>/dev/null; then
          echo "ORPHAN: $sha"           # object present (reflog) but not on this branch
        fi
      done
      ```

      **Three deliberate details.** The glob reaches **three** levels — two misses
      `docs/plans/027-tools/README.md`. `grep -E '[a-f]'` drops decimal literals that are also valid hex
      (`1000000`, `9223372036`). And **`UNRESOLVABLE` is reported, not skipped**: the earlier form used
      `git cat-file -e … && …`, so a SHA absent from the object store was silently passed over — which is
      exactly what happens in a **fresh clone**, the case this gate exists for. A dropped commit would have
      produced a clean sweep.

      **EXPECTED OUTPUT: one line**, and it is not a defect —
      `UNRESOLVABLE: 416f41e34fe32840c5634a660df790e1`, a GitHub **gist id** inside a URL in
      `docs/rfcs/README.md`. Any `ORPHAN:` line, or any other `UNRESOLVABLE:`, is real.
- [ ] **Regenerate `docs/HANDOVER.md` wholesale.** It predates Tasks 9.6 and 10 and carries a stale status
      table, a stale `git log`, stale commit counts and *"seven modules"* throughout; Task 10's fix round 3
      added a staleness banner rather than patching it, because a partially-updated handover contradicts
      itself. Rewrite from the delivered safepoint per CLAUDE.md's handover contract.
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
        | awk -F'\t' '$5=="exported" && $3!="method"{print $4}' | sort -u | wc -l                        # 103 (projected)
      go run docs/plans/027-tools/decls.go . | grep -v _test.go \
        | awk -F'\t' '$3=="var" && $4 ~ /^Err/{print $1}' | sort | uniq -c                               # 43 errors.go (projected)
      ```
      > **NO LONGER CONTINGENT — every contributing decision is closed.** D-I removes two sentinels from root,
      > D-J adds two symbols, and Task 10's fix round 2 adds one more sentinel
      > (**`ErrNilMessageGroup`** — the `MessageGroupStore.Add` SPI-violation fault), so the expected end
      > state is **103 exported / 43 sentinels / apidiff 97 removals + 9 additions**:
      >
      > | | exported | sentinels | removals | additions |
      > |---|--:|--:|--:|--:|
      > | Measured at `dadc775` | 102 | 43 | 95 | 6 |
      > | − D-I (§9.5.0) | 100 | 41 | 97 | 6 |
      > | + D-J (§9.6) | 102 | 42 | 97 | 8 |
      > | + `ErrNilMessageGroup` (Task 10, fix round 2) | **103** | **43** | **97** | **9** |
      >
      > *(The `dadc775` and post-Task-10 rows both read 43 sentinels; they are NOT the same 43 — D-I removed
      > `ErrInvalidExpression` and `ErrExprResultType`, while D-J added `ErrSharedReplyChannel` and fix round 2
      > added `ErrNilMessageGroup`. Reconcile by NAME, never by count.)*
      >
      > **Every number in the right-hand rows is a projection.** This task's job is to **measure**, not to
      > confirm: run each command, paste its output, and if it disagrees with this table, **the table is the
      > defect** — find which symbol moved and why before touching either number. Three rounds of this plan
      > have been sunk by a transcribed number; do not add a fourth.
      Then diff root's exported surface against **Spec 014 §4's closed list** — every symbol accounted for,
      nothing extra.
- [ ] **Re-run the `MessageChannel` scope-rule census** (Spec 014 §5.0) rather than citing a number. Three
      documents have now quoted three different wrong counts; the check is the contract.
- [ ] **Re-run Spec §8.1's two-arm staleness sweep to empty — AGAIN, on the delivered tree** (round-7 X-B7).
      **Regenerate `docs/plans/027-tools/symmap.tsv` first** — it is derived, and Tasks 9–11 add symbols
      (`SubscribableChannel`, `ExclusiveSubscribable`, `channel.WithSingleSubscriber`,
      `endpoint.WithSharedReplyChannel`, the `expr` providers), so an un-regenerated map makes arm 1 report
      a stale answer. Run arm 2's declared-side loop over **twelve** directories, i.e. including `expr`
      (Task 10 extends it; `decls.go` panics on a missing dir, which is why it cannot be extended earlier).
      Both arms must be **empty**; a survivor is a finding, not a note.

      > **WHY THIS RUNS TWICE — round-7 X-B7.** The sweep was owned **only** by Task 9.5, and Tasks 9.6, 9.7,
      > 10 and 11 each write godoc *after* it: 9.6 the `SingleSubscriber`/`ExclusiveSubscribable` comments,
      > 9.7 the `nilFuncStep`/`ErrNilFunc`/`ErrNilSink` godoc sweep, 10 the whole `expr` module, 11 the §8/§10
      > obligations. A sweep that runs before those edits **cannot certify the tree that ships** — and this is
      > not hypothetical: the arm-2 invariant is exactly what would have caught Task 10's dangling
      > `ErrExprResultType` cross-reference (round-7 X-B5). The Risks table names this sweep as the mitigation
      > for *"a change is invisible to the compiler"*, so the mitigation has to sit at the end of the branch,
      > not in its middle.
- [ ] Run the full per-module `GOWORK=off` loop across **eight** modules, `go vet`, `golangci-lint`,
      `test -z "$(gofmt -l .)"`, `govulncheck`, `go mod tidy` (no-op in every module), and
      `CGO_ENABLED=0 go build ./...`.
- [ ] `apidiff`/`gorelease` against the Task 0 baseline; reconcile **every** entry against Spec 014 §4.1's
      decomposition — **projected 97 removals / 9 additions** (the ninth is `ErrNilMessageGroup`, added by
      Task 10's fix round 2), i.e. the measured 87 + 6 + 1 + 1 = 95 partition
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

```
Spec: 014
Plan: 027
ADR: 0007
ADR: 0027
ADR: 0028
ADR: 0029
ADR: 0030
RFC: 0001
RFC: 0002
RFC: 0003
```

*(Round-7 X-M12. Task 12 is the whole-branch gate, so it carries **every** ADR the branch realizes —
including ADR 0007, which D-N amends. ADR 0007 declares no RFC; the other four contribute RFC 0001/0002/0003.)*

---

## Risks

| Risk | Mitigation |
|---|---|
| A count in a document rots and nobody notices | Every normative number in Spec 014 §3–§4 is generated with its command recorded in the ledger; Task 12 re-derives them rather than reading them. The `MessageChannel` census is stated as a **scope rule + command**, because three rounds of correcting the *number* produced three wrong numbers |
| A change is invisible to the compiler | `go vet ./...` (not `go build`) after every move; the two-arm staleness sweep **in Task 9.5 and again in Task 12** — Tasks 9.6, 9.7, 10 and 11 all write godoc after 9.5, so only the Task 12 run certifies the delivered tree *(round-7 X-B7)*; `unused` is off, so dead code needs an explicit check |
| Coverage looks like it regressed when it did not | Global Constraint 4: `-coverpkg=./...` on both sides. The **six** accepted uncovered blocks are enumerated with dispositions in Spec 014 §9 AC-7 and restated in Task 12, so a worker does not chase them *(round-6 C-M6: this said "two", and a worker would have reported the other four as regressions)* |
| A satellite module is left red | The `GOWORK=off` loop — **seven** modules until Task 10, **eight** after (Global Constraint 5); **`harness` is checked with `go vet`, never `go test`** — it has no test files and `go test` reports a false pass |
| A "pure move" quietly changes behavior | No assertion may change outside **the rows of Spec 014 §2.1's table** — cite the table, never a count (it has grown three times: rows 5 D-J, 6 D-M, 7 D-N). Identity is proved by **the normalised per-file diff alone** |
| `apidiff` noise hides a real break | Read against Spec 014 §4.1's decomposition of the removals into named classes — 95 into four at `dadc775`, a projected 97 into five once D-I lands |
| RED cannot be evidenced for a compile-time failure | The transcript comes from `go test -c -o /dev/null .`, not `go vet` (which stops after one type-error batch) |
| `expr` cannot build standalone | Task 10 ships `require` + `replace` together, and CI gets all three edits |
| `gopls` unavailable in a subagent | No task depends on it; `go vet ./...` is the authoritative reference-finder and `grep` the fallback |
| Root loses its package doc | Done in Task 1's change; the five subpackage docs landed in the round-3 pass (F12.5). Task 11's verify **counts** them — `go vet` does NOT catch a duplicate (Global Constraint 3) |
| A godoc obligation has no owning task | Spec 014 §8's nine bullets and §10's four obligations were unowned and six were unmet. **Every one now has an owning task, and the owner is the task that CREATES the symbol** (round-8 C4, resolving a four-way contradiction): **Task 9.6** owns §8's 10, 11, 11a, 12, 13; **Task 9** owns obligation 4 for the four behavior types it creates; **Task 11b/11c** owns the remaining seven and **re-verifies** the other nine as no-regression checks. The owner mapping is stated in Spec §8's table, in the §11 pinning table, and in each task's Verify — and each is measured by a gate id from the one canonical block |
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
