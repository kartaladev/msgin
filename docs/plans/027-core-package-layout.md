# Plan 027 — Core package layout, channel segregation, and behavior types

> ## Status: **REGENERATED 2026-07-28 from a green tree — pending round-3 audit**
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
> | No task migrates the adapter tree | **FIXED** | Task 7 below; F9 — 28 files, 115 code + 39 godoc, all seven modules green |
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
[`027-derivation-findings.md`](027-derivation-findings.md) (F0–F11). Branch:
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
- **What must be in it** (all present today): the apidiff baseline diff (F11.2), per-package coverage both
  ways (F10.8, F11.11), the six declaration-level split tables (F11.1), the test-placement census (F8.2), the
  cross-file test-identifier inventory (F8.4), the adapter change inventory (F9.3, F9.9), the RED compiler
  transcript for the capability test (F10.1), and the deleted `*Expr` test cases Task 10 must reinstate.

---

## Global constraints

1. **`go vet ./...` after every single move**, not `go build`, and not only at task end. `go vet` compiles
   test binaries; `go build` does not, and cannot see the satellite modules. An import cycle surfaces
   instantly; found late, it is expensive to unpick.
2. **Blackbox tests only** — every `_test.go` stays `package <pkg>_test` and drives the exported API. Moving a
   test must not tempt anyone into whitebox access to reach a now-unexported helper; if that happens, the
   symbol's placement is wrong, not the test.
3. **Exactly one package-doc file per package.** Each new package gets a `doc.go`; duplicate `// Package`
   comments after a merge are a `go vet` failure. `golangci-lint` will **not** catch a missing one:
   `.golangci.yml` sets `linters.default: none` and does not enable `ST1000`.
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
   go list -deps ./endpoint ./routing ./transform ./channel ./resilience | grep 'kartaladev/msgin/'     # EMPTY
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
| 0 | Baseline, move-list, apidiff baseline | **DONE** — `/tmp/msgin-derive/root.api`, F0 |
| 1 | Delete the `*Expr` constructors, drop `expr-lang`, root `doc.go`, **D-D**, **D-E pulled forward** | **DONE** — F2–F6 |
| 2 | Segregate `MessageChannel`; `SubscribableChannel`; `DirectChannel.Subscription`; **D-F** | **DONE, UNCOMMITTED** — F10 |
| 3 | `StreamingSource` → `EventDrivenSource` | **DONE, UNCOMMITTED** — F10.4 |
| 3.5 | Export `IsPermanent`/`RetryAfterOf`/`NewID`; delete `RetryPolicy.delayFor` | **DONE** — commit `c83dde9` |
| 4–8 | Extract `routing`, `transform`, `channel`, `resilience`, `endpoint`; place the 44 test files | **DONE** — commit `c83dde9`, F8 |
| 7a | Requalify `adapter/` + the six satellite modules | **DONE** — commit `c83dde9`, F9 |
| **9** | Named behavior types + combinators | **PARTIAL** — `CorrelationStrategy`/`ReleaseStrategy` shipped; 4 types + combinators remain |
| **9.5** | Residual cleanups the migration left behind | **NOT STARTED** — see below |
| **10** | The `expr` provider module | **NOT STARTED** |
| **11** | Package docs for the five subpackages | **NOT STARTED** |
| **12** | `MIGRATION.md`, doc sync, whole-branch gate | **NOT STARTED** |

```
$ git log --oneline -1
c83dde9 refactor(core)!: extract the flat core into endpoint/routing/transform/channel/resilience
```

**The Task 2/3 work is green but uncommitted.** Committing it is the first action of the resumed plan, and it
is *not* covered by the per-task pre-authorisation until the user approves this regenerated plan.

---

## Tasks 0–8 — COMPLETED, recorded for traceability

Do not re-execute. Recorded here because a plan whose completed tasks are missing cannot be read against a
diff, and because four of them carry findings later tasks depend on.

### Task 0 — baseline · **DONE**

`apidiff -w /tmp/msgin-derive/root.api .` before any change; per-module coverage baselines; all seven modules
green at baseline (F0). Root: 32 source + 45 test files; 42 error sentinels (F1); 245 exported / 138
unexported top-level declarations.

### Task 1 — remove the `*Expr` constructors; drop `expr-lang` · **DONE**

Deleted `FilterExpr`, `RouterExpr`, `TransformExpr`, `SplitExpr`, `WithCorrelationExpr`, `WithReleaseExpr`,
`expr.go`, `expr_test.go`, `doc_composition.go`; created root `doc.go` in the same change; ran `go mod tidy`
in all seven modules. `expr-lang` dropped cleanly; root direct deps 3 → 2 (F6).

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

Five test fakes were **deleted, not migrated**, because they existed only to satisfy the old bundled
interface: `fakeAggChannel`, `failNthChannel`, `idsAggChannel`, `collector`, `scriptedChannel`.

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
- [ ] Add `Predicate.And` / `Or` / `Not`.
- [ ] Every type's godoc **names its Spring equivalent** — this is the mitigation that justifies dropping the
      Spring names (ADR 0029 §4), so verify it **per type**, not sampled.
- [ ] Record the expected `apidiff` output as **reviewed, source-compatible** parameter-type changes. Do not
      claim zero output.

**Hot-path branches needing a case each:** `And` short-circuit on false; `And` error propagation from the
left and from the right predicate; `Or` short-circuit on true; `Or` error propagation from each side; `Not`
inverting a true and a false; `Not` **propagating** an error rather than inverting it *(the case a naive
`Not` gets wrong)*.

> **Do NOT re-verify "aggregator coverage returns to 100% on `NewAggregator` and `Handle`."** That criterion
> is void: **D-D deleted** the `NewAggregator` guard rather than rescuing it (F5, round-2 §B4), and the three
> `Handle`-side branches were already restored in Task 1 via D-E (F3). `routing` measures **100%** today.

**Verify:** existing tests compile unchanged with bare closures — that is the source-compatibility claim, so
demonstrate it rather than assume it (round-2 §E confirms bare closures still infer against named generic
func types on Go 1.25). `-coverpkg=./...` on both sides.

**Commit:** `feat(routing,transform): name the endpoint behavior types and add combinators`

---

## Task 9.5 — Residual cleanups the migration left behind · **S** · NOT STARTED

Four items, each invisible to `go build`, `go vet`, `go test`, and `gofmt`. None is cosmetic; each is a
delivery blocker under CLAUDE.md's godoc and dead-code expectations.

- [ ] **Delete root's dead `boxMessage` and `nilFuncStep`.** Zero users in root and zero in root's tests
      after every package inlined its own copy; `.golangci.yml`'s `linters.default: none` means `unused` is
      off, so nothing reports it (F11.6).
      ```
      payload.go:30  func boxMessage[T any](m Message[T]) Message[any]
      handler.go:66  func nilFuncStep() Step
      ```
- [ ] **Run the two-arm staleness sweep to empty** (Spec 014 §8.1). Arm 1 (moved symbols) currently has **2**
      survivors; arm 2 (deleted symbols — a class arm 1 structurally cannot see) has **7**:
      ```
      ARM 1: codec.go:33, routing/aggregator_test.go:21
      ARM 2: errors.go:156,175,176,177 · routing/splitter.go:52 · routing/aggregator.go:316
             routing/aggregator_test.go:1276
      ```
- [ ] **Decide the fate of the two orphaned expr sentinels** — `ErrInvalidExpression` (`errors.go:161`) and
      `ErrExprResultType` (`errors.go:183`) have zero users and their godoc names deleted constructors. Either
      the `expr` module imports them from root (and Task 10 wires that) or they are removed from root's closed
      contract. **This is a decision, not a cleanup** — record it in the ledger and, if it changes the root
      contract, in Spec 014 §4.
- [ ] **Fix the article-agreement class**, not its instances (F9.5) — `msgin` takes "a", `endpoint` takes
      "an", so every mechanically rewritten comment reading "a endpoint.Producer" is wrong:
      ```bash
      grep -rn --include='*.go' -E '\b[Aa] (endpoint|routing|transform)\.' .                            # empty
      grep -rn --include='*.go' -E '\[(endpoint|routing|channel|transform|resilience)\.[A-Z]' adapter/  # empty
      ```
- [ ] **Extend the capability test to the four uncovered send-only positions.** `capability_test.go`'s
      `TestSendOnlyCallSitesAcceptEveryChannel` covers **3 targets × 3 sites = 9 subtests** today
      (filter discard, router default, exchange request). Spec 014 §9.4 requires all eight send-only
      positions, and the four missing ones are precisely the ones the two failed censuses missed:
      | Missing site | Why it matters |
      |---|---|
      | `routing.WithOutputChannel` | a durable `QueueChannel` as the Aggregator **output** — round-2 §A6's point |
      | `routing.WithExpiredGroupChannel` | same, for the **expired-group** sink |
      | `msghttp.ServeAsync`'s `target` | an HTTP request parked in a durable queue channel |
      | `stdlib.NewInbound`'s `target` | same, via the stdlib binding |
      The two HTTP sites live in `adapter/http` and `adapter/http/stdlib`, so their cases belong in **those
      packages' tests**, not in root's `capability_test.go`.

**Verify:** both sweep arms empty; the capability test covers 3 targets × 5 core sites plus the two HTTP
sites; the eight-module `GOWORK=off` loop green; `-coverpkg=./...` unchanged.

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
- [ ] Providers returning the Task 9 types. **The shape is NOT uniform, and the earlier plan assumed it was**
      (round-2 §D2):
      ```go
      func Predicate[A any](s string)   (routing.Predicate[A], error)
      func SplitFunc[A, B any](s string)(routing.SplitFunc[A, B], error)
      func Transformer[A, B any](s string) (transform.Transformer[A, B], error)
      func Correlation(s string)       (routing.CorrelationStrategy, error)
      func Release(s string)           (routing.ReleaseStrategy, error)
      func RouteFunc(s string, routes map[string]msgin.MessageChannel) (routing.RouteFunc, error)   // ← extra param
      ```
      `RouteFunc` additionally carries the **two construction validations** `RouterExpr` had. Do not force it
      into a `(string) → (T, error)` mould it cannot fit.
- [ ] `Release` returns `routing.ReleaseStrategy`, whose `(bool, error)` shape is what lets an evaluation
      failure propagate instead of being swallowed into a permanent `false`. `WithReleaseStrategy(expr.Release(…))`
      now compiles, which is the point of D-E.
- [ ] Reinstate the Task 1 test cases from the ledger against the providers. Behavioral parity with the
      deleted `*Expr` constructors is the acceptance bar. **Two of them are `toGroupEnv` guard cases that
      genuinely belong here** — M-1 (empty group snapshot) and M-6 (non-`A` member → `ErrPayloadType`) —
      while H-1/H-2/H-3 already returned in Task 1 and must **not** be re-added (F3).
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

## Task 11 — Package documentation for the five subpackages · **S** · NOT STARTED

None of the five has a `doc.go`; only root does (F11.8). `ST1000` is not enabled, so lint will never flag it.

- [ ] `endpoint/doc.go` — EIP ch.10 *Messaging Endpoints*; Spring `org.springframework.integration.endpoint`.
- [ ] `routing/doc.go` — EIP ch.7 *Message Routing*; Spring `…integration.router`.
- [ ] `transform/doc.go` — EIP ch.8 *Message Transformation*; Spring `…integration.transformer`.
- [ ] `channel/doc.go` — EIP ch.3 (Pipe) and ch.4 (Publish-Subscribe Channel); Spring `…integration.channel`.
- [ ] `resilience/doc.go` — **state explicitly that it has no EIP chapter and no Spring counterpart**, and
      cite [ADR 0006](../adrs/0006-resilience-flow-control.md) instead. Spec 014 §3.5's *"each subpackage doc
      names its EIP chapter and its Spring counterpart"* is unsatisfiable for this one (round-2 §D15);
      inventing a chapter would be exactly the lexical drift this program exists to prevent.
- [ ] Confirm root's `doc.go` states the **Pipes-and-Filters** model in Spec 014 §3.5's terms: a
      `MessageChannel` is the **Pipe**, a `Step` is the **filter**, `Chain` assembles the pipeline.

**Verify:** `for p in . endpoint routing transform channel resilience; do grep -c '^// Package ' $p/*.go; done`
→ exactly one per package. `go vet ./...` clean (a duplicate package comment is a vet failure).

**Commit:** `docs(core): package documentation for the five extracted packages`

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
      go list -deps ./endpoint ./routing ./transform ./channel ./resilience | grep 'kartaladev/msgin/'    # EMPTY
      ls *.go | grep -v _test.go | wc -l                                                                 # 14
      decls . | grep -v _test.go | awk -F'\t' '$5=="exported" && $3!="method"{print $4}' | sort -u | wc -l  # 101
      decls . | grep -v _test.go | awk -F'\t' '$3=="var" && $4 ~ /^Err/{print $1}' | sort | uniq -c        # 42 errors.go
      ```
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
| Root loses its package doc | Done in Task 1's change; the five subpackage docs are Task 11, and Task 11's verify counts them |
| Expression support absent mid-branch | Bounded to Tasks 1→10 within one branch; Task 1 preserved the test cases in the ledger that Task 10 must satisfy |
| Branch conflicts with in-flight feature work | Run this window **before** Plan 028 (gin) or any new adapter; blast radius grows with every adapter landed first |
