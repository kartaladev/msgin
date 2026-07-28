# Session handover — msgin

> **READ FIRST.** Read `CLAUDE.md`, then `docs/specs/014-core-package-layout.md` and
> `docs/plans/027-core-package-layout.md` (both **regenerated** 2026-07-28), then
> `docs/plans/027-derivation-findings.md` (F0–F11, the evidence base). Trust those over this file and over
> any memory.
>
> **STATE (2026-07-28, end of session): the refactor is IMPLEMENTED, GREEN, and fully COMMITTED.**
> The working tree is **clean**. The round-3 audit has run (3/3 NEEDS-REVISION) and **all its findings are
> resolved**; `/security-review` and an adversarial code review have both run and their findings are
> resolved too.
>
> **What remains: the round-4 audit, Plan 027 Tasks 9–11, and two open decisions (§4.6).**
>
> **Nothing is pushed.** `main` is at `6f44db6`; this branch is 5 commits ahead, local only.
> Do not commit or push without explicit approval.

## 1. Objective & position

`msgin` is a Go 1.25 Enterprise Integration Patterns library. The active effort is the **pre-v1 core
refactor**: flatten-to-packages, channel interface segregation, and EIP lexical alignment.

Spec 014 + ADRs 0027/0028/0029 + Plan 027 **failed two adversarial audits** (3/3 auditors each), both times
because the move-list was hand-typed and asserted as verified. Round 2 §F changed the method: **migrate
first, let the compiler prove it, generate the move-list from the green tree, then write the documents.**

**That method has now been executed end to end.** The migration is done and green; the bundle has been
rewritten from generated evidence.

## 2. Exact state

Branch `claude/repo-structure-refactor-jt79t1`, **5 commits ahead of `main` (`6f44db6`), NOT pushed.
Working tree CLEAN.**

```
3d0b87a  docs(027): apply the round-3 audit corrections; commit the derivation tools
1d7fc80  fix(core): restore the goleak net, cover the poll-backoff cap, reject a nil Subscription
0e2dcf0  docs(027): regenerate the bundle from the verified tree; clear all round-2 banners
b6ce7bb  refactor(core)!: segregate MessageChannel; add WithSingleSubscriber; rename StreamingSource
c83dde9  refactor(core)!: extract the flat core into endpoint/routing/transform/channel/resilience
```

- `c83dde9` — the mechanical migration. 105 files, +2040/−3145. Root 32 → **14** non-test files.
- `b6ce7bb` — Plan 027 Tasks 2/3 + decision D-F.
- `0e2dcf0` — Spec 014 §3 + Plan 027 regenerated from the green tree; round-2 banners cleared.
- `1d7fc80` — the round-3 **code** blockers and the code-review findings M1–M5.
- `3d0b87a` — the round-3 **doc** corrections; derivation tools committed to `docs/plans/027-tools/`.

**Verified green at handover:** all seven modules `build` + `vet`; `go test ./... -race -shuffle=on` across
all 11 root packages; Docker-backed `dbtest` and `crontest` run for real. Coverage `-coverpkg=./...`
**93.26%** (baseline 93.23%).

The `../msgin-derive` worktree is **merged and redundant** — its branch `refactor/mechanical-derivation`
points at `c83dde9`. Safe to `git worktree remove`; left in place only because removal was not requested.

## 3. Traceability pointers (read in this order)

1. `CLAUDE.md` — hard rules. **SDD is the default execution mode; direct main-session implementation needs
   explicit per-task approval.**
2. `docs/specs/014-core-package-layout.md` + `docs/plans/027-core-package-layout.md` — regenerated; every
   table transcribes a pasted command.
3. **`docs/plans/027-derivation-findings.md`** — F0–F11, the evidence base. Treat as evidence, not
   scripture: it contains two self-corrections.
4. `docs/plans/027-derivation-brief.md` — env, tooling, the green gate, the coverage rule. Written for
   subagent consumption; hand it to every implementer.
5. `docs/plans/027-audit-round-2.md` — §E what is verified-sound (**do not re-open**), §G.1 the eight
   settled decisions. Historical record; its banner stays.
6. `docs/adrs/0027`, `0028`, `0029`.

## 4. Next actions

### 4.0 ROUND-3 AUDIT — 3/3 NEEDS-REVISION, **and the findings have now been WORKED** (2026-07-28)

> **STATUS: all six blockers resolved; every CI gate green.** Verified after the fixes:
>
> ```
> go mod tidy -diff        TIDY-CLEAN in all 7 modules (expr-lang gone from go.mod AND go.sum)
> golangci-lint run ./...  0 issues
> gofmt -l .               empty (whole tree)
> seven-module GOWORK=off  GREEN ×7 (build + vet)
> go test ./... -race -shuffle=on   11 packages, 0 failures
> coverage -coverpkg=./...          93.4%   (pre-refactor baseline 91.9%)
> acyclicity gate (corrected form)  EMPTY — the gate now actually works
> one `// Package` per package      1,1,1,1,1,1  (five subpackage doc.go files added)
> ```
>
> Code fixes are recorded in the findings file as **F12**, doc corrections as **F13**.
> **Everything below this box is the audit as originally reported** — kept because the *reasoning* is what
> matters for round 4, not the resolved status of each item.
>
> **All of the above is now COMMITTED** in `1d7fc80` (code) and `3d0b87a` (docs). The review gates have
> also run — see §4.5. **NOT yet done: the round-4 audit** (rounds 1–3 each found defects the previous
> round's fixes introduced, and this pass edited the same documents again).

### 4.5 Review gates — BOTH HAVE RUN (2026-07-28), findings resolved

- **`/security-review`: no qualifying vulnerabilities.** Verified clean: `NewID` (`crypto/rand`, 128-bit),
  header aliasing across the six rewritten `endpoint` sites (identical sharing to before, not more),
  the widened `ServeAsync`/`NewInbound` target (chosen at wiring time, never derived from request data),
  stale-handle eviction, and `Close()` failing *closed*. The `expr.go` deletion is a net **reduction** in
  attack surface — a tree-wide grep found zero substitute eval paths.
- **Adversarial code review: REQUEST CHANGES → all five findings fixed in `1d7fc80`.** It found **no
  semantic drift** in the move itself; it blocked because the split had silently removed two of the
  project's own mandatory gates (`goleak`, and coverage of the poll-backoff cap).
- **NOT run: `/code-review ultra`** — the multi-agent cloud review. It is user-triggered and billed; an
  assistant cannot launch it. What ran was an adversarial reviewer subagent, which is CLAUDE.md's
  SDD-mandated reviewer but **not** the same gate. Consider running it before merge.

### 4.6 TWO OPEN DECISIONS — both are the user's, neither should be assumed

1. **Reply-channel exclusivity.** Narrowing `MessageChannel` demoted it from a compile-time guarantee (at
   `main`, `DirectChannel` was the *only* type satisfying `MessageChannel`, so a pub-sub reply channel was
   structurally impossible) to godoc plus the off-by-default `channel.WithSingleSubscriber()`. Two
   exchanges sharing one pub-sub reply channel now compiles, and the non-owning one receives a full copy
   of every reply into its `WithUnmatchedReplySink`.
   **Three independent lenses converged here** — round-3 design (M6), the code reviewer's design residual,
   and the security scan's only (sub-threshold, 0.75) finding. None individually blocked; together this is
   the strongest signal of the whole review cycle. The question: *should `NewChannelExchange` reject a
   non-exclusive reply channel at construction?* CLAUDE.md's sensible-defaults rule argues yes.
2. **Plan 027 §9.5.0 — the orphaned sentinels.** `ErrInvalidExpression` and `ErrExprResultType` have no
   producer since `expr.go` left. They were **kept and re-documented** (the reversible choice); removing
   them is irreversible and still undecided. **Task 12's counts are contingent on it**, and `1d7fc80`
   moved them again by adding `ErrNilSubscription`: **101→102 exported, 42→43 sentinels**, deliberately
   NOT edited into the documents so they move once, together with this decision. Task 10 also consumes
   whatever is decided (`RouteFunc`'s two construction validations wrap `ErrInvalidExpression`).

#### The audit as originally reported

Run 2026-07-28 against `0e2dcf0`, three Opus lenses (consistency · design · executability). **The method
worked where it was applied** — all three independently confirmed the *generated* artifacts:

- **All 80 §3.2 declaration rows verify exactly** (source, destination, visibility), checked against an
  independently rebuilt AST dumper. **Zero defects.** This is the artifact that sank rounds 1 and 2.
- `apidiff` 95/5 reproduces and partitions as claimed (87+6+1+1) with an **empty residual**.
- Root 14 files · 101 exported symbols · 42 sentinels · the `MessageChannel` census · the dependency graph ·
  every coverage figure · every traceability link — all reproduced.

**Every surviving defect is in hand-written prose, or in a command that was pasted but never run.**

#### The two code blockers — the branch is NOT green in the sense CI means

| # | Defect | Evidence |
|---|---|---|
| **B1** | **`expr-lang` was never dropped from the satellites.** 6 of 7 `go.mod`s still require it; `go mod tidy` is **dirty in all 6**. CI runs per-module `go mod tidy` + `git diff --exit-code` → **5 of 6 matrix jobs red today.** Spec §9 AC-3/AC-6 both fail, and Spec §1.3's motivating defect ("a consumer using nothing but the SQL adapter still pulls `expr-lang`") is unfixed for exactly the consumer it names. | `grep -rn expr-lang --include='go.mod' .` |
| **B2** | **`golangci-lint` regressed 0 → 3** (ST1008 in `channel/channel_test.go:122,140,150`, introduced by `b6ce7bb`). CI's lint job is red. The plan defers lint to Task 12, so it went unseen across two committed tasks. | `golangci-lint run ./...` |

**Fix B1 with `GOWORK=off go mod tidy` in all seven modules; B2 by reordering the closure to `(int, error)`.**

#### The four route blockers — the plan is not executable as written

- **B3 · the acyclicity gate can never pass** (found by all three auditors). `go list -deps` includes its
  argument packages, so the command published in **four** documents as `# must be EMPTY` prints five lines on
  a correct tree — and would print six on a broken one, so it cannot even distinguish the cases. The
  invariant itself **holds**. Fix: append
  `| grep -vE '^github.com/kartaladev/msgin/(endpoint|routing|transform|channel|resilience)$'`.
- **B4 · Task 10's provider signatures do not compile** (compile-proven). The plan and Spec §7 specify
  non-generic `Correlation`/`Release`/`RouteFunc`, but the deleted originals were all `[A any]`, and `A` is
  load-bearing twice: `compile[A]` type-checks `payload.Field`, and `PayloadOf[A]` **is** the M-6
  `ErrPayloadType` branch Task 10 mandates. Must be `Correlation[A any]` etc. in plan **and** spec.
- **B5 · Task 10's acceptance material does not exist.** The plan says to reinstate the deleted `*Expr` test
  cases "from the ledger, all present today". The ledger holds **two table rows**; none of the 12 deleted
  test functions are recorded anywhere in `docs/`. Only `git show ab233d9:expr_test.go` has them — name it.
- **B6 · three mandatory gates depend on untracked `/tmp` artifacts.** Task 12 invokes `decls` (a compiled
  binary at `/tmp/msgin-derive/decls`) and Spec §8.1 ARM 1 reads `symmap.tsv`. Neither is in the repo; a
  fresh clone or a `/tmp` reap makes them unrunnable, with no rebuild instructions. **Commit `decls`' source
  under `docs/plans/027-tools/` and commit `symmap.tsv`.**

#### False claims to correct (each convergent across ≥2 auditors)

- **"Five test fakes were deleted, not migrated"** — all five survive; what was deleted is their vestigial
  `Subscribe` **stubs**. The conclusion holds; the evidence cited to justify a ratified decision does not.
- **"No `Err*` var elsewhere in the workspace"** — there are **51**, in three shipped adapter packages. The
  paragraph's design rationale rests on this, and the repo's precedent argues the opposite way.
- **§4.1's prose** says "the 93" and "the 86" while its own table sums to **95/87**.
- **"A duplicate package comment is a `go vet` failure"** — *proven false by execution*: vet, build, gofmt
  and golangci-lint all pass on a duplicate. Task 11's only mechanical check is blind to the defect it names.
- **§3.6's adapter inventory** is a `c83dde9`-only snapshot presented as the whole-window figure; 5 of 7 rows
  are stale at HEAD. **Recurrence of round-2 §A2.**
- **The coverage "baseline" (93.23%) is the post-extraction tree**, not pre-split. True `ab233d9` → HEAD is
  93.5% → 93.3%, a real −0.26pp, entirely explained by the newly-dead root helpers.
- **F10.8 lists 6 uncovered blocks; there are 11.** The one that matters: `reliability.go:39`, the `err==nil`
  arm of the **newly exported** `IsPermanent`. `IsPermanent` and `RetryAfterOf` have **zero direct tests**,
  and Task 3.5 enumerates no hot-path branches at all — a CLAUDE.md delivery blocker.
- **Spec §8/§10's godoc obligations have no owning task** — five unmet, two asserted as already done.
- Plan §Progress says Tasks 2/3 are "DONE, UNCOMMITTED" and tells the next session to commit first; they
  landed in `b6ce7bb`, one commit *before* the regeneration wrote that they hadn't.

#### The rule that would have caught most of this

The consistency lens named the shared signature, and it is the right frame for round 4:

> **Every surviving defect is a number or command pinned to an intermediate state** — Task 7a, the derivation
> working tree, the root module — **and then presented as a property of the finished branch.**

**Adopt one rule: every pasted command carries its explicit commit range and module scope.** That single
change catches B1, B3, §3.6, the coverage baseline, and the 93/86 arithmetic in one pass. Do **not** work
round 4 as "fix these ~20 items" — that is what has failed three times.

### 4.1 (superseded — the round-3 audit above has run)

CLAUDE.md mandates an independent adversarial audit of the **complete bundle together** (spec + ADRs + plan)
before implementation. Implementation has in this case *preceded* the audit by design (that was the whole
method change), so the round-3 audit is now checking a bundle that describes **code that actually exists** —
tell the auditors that explicitly, and that they may verify any claim against the tree.

Use fresh Opus subagents, three lenses as in rounds 1–2. **The consistency lens should have little to find**
— that is the test of whether the method worked. Prime them with:
- Every number should be reproducible by the command printed beside it. Re-run a sample.
- §E of round 2 lists what is settled; re-opening it is out of scope.
- The eight decisions D-A…D-H are **user decisions**, not open questions.

### 4.2 Pre-merge gates — NOT YET RUN

**`/code-review` and `/security-review` over `main..HEAD` have never run on this branch.** CLAUDE.md makes
both hard preconditions before merging, and the branch carries a large breaking refactor plus new public API
(`SubscribableChannel`, `WithSingleSubscriber`, `IsPermanent`, `RetryAfterOf`, `NewID`, `EventDrivenSource`).
The round-3 design audit does **not** substitute for either. `c83dde9`'s commit body records that it was
committed without them, at the user's explicit direction.

### 4.3 Open items the regeneration surfaced (F11) — small, real

- **F11.6** root's `boxMessage` and `nilFuncStep` are now **dead code**. `unused` is disabled, so nothing
  reports it. Delete them.
- **F11.7** the staleness sweep needs a **second arm for deleted symbols**: 7 `*Expr` godoc mentions survive
  that a moved-symbol grep structurally cannot see. Related: `ErrInvalidExpression` and `ErrExprResultType`
  are **orphaned in root** — a decision Task 10 (the `expr` module) owes.
- **F11.8** **none of the five subpackages has a `doc.go`**, though Spec 014 §3.5 makes them normative.
  `ST1000` is off, so the gate is blind to it.
- **F11.5** the capability test covers 3 targets × **3** sites; Spec §9.4 requires **8**. The four missing
  are exactly the sites both `MessageChannel` censuses missed.
- Spec 011 §6 Phases 3/4 carry no ✅ DELIVERED marker although SSE shipped in Plans 025/026. Deliberately
  left — marking a phase delivered is a claim needing its own verification.

### 4.4 Then

Plan 027's remaining tasks: **Task 9** (named behavior types/combinators — note D-E already landed early),
**Task 10** (the `expr` provider module, which must adopt the orphaned sentinels), **Task 11** (migration
guide, doc sync, whole-branch gate).

## 5. What this method proved (the point of the exercise)

Hand-derivation failed twice; mechanical derivation caught all of the following, each compiler- or
command-verified. Full detail in the findings file.

- **`apidiff`: 95 removals, 5 additions.** Decomposition verified as a partition by set arithmetic
  (87 relocated + 6 `*Expr` deleted + 1 rename + 1 `MessageChannel.Subscribe`), empty residual.
- **§3.2 splits: 80 declarations across six files, zero unlocated.** Round-2 §B3 charged the hand-written
  table with omitting five declarations — **it omitted thirty-one**.
- **The `MessageChannel` census is NINE**, not ADR 0028's "four of five" nor round-2's corrected "six of
  seven". The two both missed — `adapter/http/inbound.go:116` and `adapter/http/stdlib/inbound.go:33` — are
  in the HTTP adapter; both censuses searched only the pattern core. **Now written as an invariant with a
  check command, not a count** — round 2 corrected the number and the number broke again.
- **42 error sentinels**, not 89.
- **ADR 0028 §7's semantics table was missing a row**: a stale handle's `Cancel` after a resubscribe would
  have silently evicted the *new* subscriber — a latent bug the "decided" table would have shipped.
- **`endpoint` read `Message`'s unexported fields at 6 sites, not the 4 the compiler printed** — Go's
  10-error cap hid `producer.go:423`. **Never enumerate a change set from build stderr.** Likewise `go vet`
  showed only 1 of 3 capability failures; `go test -c` showed all three.
- **Coverage attribution:** default per-package coverage shows root falling 99.3% → 81.8%, *below* the 85%
  gate, purely because blackbox tests moved to sibling packages. `-coverpkg=./...` shows 93.2%+ against a
  91.9% baseline. **Every extraction task must verify with `-coverpkg`** or the gate fails falsely.
- **Task ordering (F3):** Task 1 destroys the only driver for three *core* aggregator branches; their sole
  fallible release strategy was `WithReleaseExpr`. **D-E is a Task 1 prerequisite, not Task 9 work.**
- **Test identifiers cross package boundaries** (F2): deleting `expr_test.go` left the root test binary red.
  `go build` cannot see it. §3.4 must inventory test *identifiers*, not only test *files*.
- **Placement cannot be reference-counted:** `queuechannel_e2e_test.go` has 3 `endpoint` refs vs 1 `channel`
  ref and belongs to `channel` — the endpoint symbols are the harness, not the SUT.
- **`retry_test.go` does not split** (confirms §A7); zero case-level splits.
- **Adapter blast radius: 28 files, 154 occurrences = 115 code + 39 godoc-comment-only.** The comment-only
  ones break nothing, so a task that stops at "green" leaves them silently wrong. Zero new module edges.

## 6. Gotchas & environment

- **`export GOTOOLCHAIN=go1.25.12`** always; **`export PATH="$(go env GOPATH)/bin:$PATH"`** — `goimports`,
  `gofumpt`, `gopls`, `govulncheck`, `apidiff`, `gorelease` all live there and none are on `PATH`.
- **`./...` is not the repo** — seven modules. Use the per-module `GOWORK=off` loop in the brief.
- **`go build ./...` does not compile tests**; `go vet` does, but stops after one type-error batch — use
  `go test -c` for a full transcript.
- **The `apidiff` baseline is at `/tmp/msgin-derive/root.api`** (NOT in `msgin-derive-artifacts/`, which an
  earlier brief claimed). `/tmp` is not durable — **re-capture it into the repo or a durable path before
  relying on it again.**
- Reusable tooling from the derivation run: `decls` (AST decl dump) and `qualify` (AST-based
  requalification; **a regex version corrupted EIP pattern names in godoc — never go back to regex**).
  Sources in the session scratchpad; rebuild if `/tmp` was cleared.
- `.golangci.yml` sets `linters.default: none` — **`ST1000` and `unused` are both off**, so missing package
  docs and dead code after a move are reported by nothing.
- `gopls` has **no Move refactoring**.
- `.github/workflows/ci.yml` omits `adapter/cron/crontest` from both jobs — pre-existing gap.
- Repo has **zero git tags** — do NOT propose tagging.
- Never commit `.claude/settings.json`; stage explicit pathspecs.
