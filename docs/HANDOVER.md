# Session handover — msgin

> **READ FIRST.** Read `CLAUDE.md`, then `docs/specs/014-core-package-layout.md` and
> `docs/plans/027-core-package-layout.md` (both **regenerated** 2026-07-28), then
> `docs/plans/027-derivation-findings.md` (F0–F11, the evidence base). Trust those over this file and over
> any memory.
>
> **STATE: the refactor is IMPLEMENTED and GREEN. The bundle is regenerated and all banners are cleared.**
> What remains is the **round-3 adversarial audit** and the **pre-merge review gates**.
>
> **⚠️ UNCOMMITTED WORK IN THE TREE.** The migration is committed (`c83dde9`); everything since — Task D's
> code and Task E's whole documentation regeneration — is **uncommitted**. Do not `git checkout`,
> `git stash`, `git reset`, or `git clean`. Do not commit without asking.

## 1. Objective & position

`msgin` is a Go 1.25 Enterprise Integration Patterns library. The active effort is the **pre-v1 core
refactor**: flatten-to-packages, channel interface segregation, and EIP lexical alignment.

Spec 014 + ADRs 0027/0028/0029 + Plan 027 **failed two adversarial audits** (3/3 auditors each), both times
because the move-list was hand-typed and asserted as verified. Round 2 §F changed the method: **migrate
first, let the compiler prove it, generate the move-list from the green tree, then write the documents.**

**That method has now been executed end to end.** The migration is done and green; the bundle has been
rewritten from generated evidence.

## 2. Exact state

Branch `claude/repo-structure-refactor-jt79t1`, **not pushed**. `main` is at `6f44db6`.

```
c83dde9  refactor(core)!: extract the flat core into endpoint/routing/transform/channel/resilience
ab233d9  docs(audit): settle all six round-1 decisions; record the governing criteria
```

**Committed** (`c83dde9`): the whole mechanical migration. 105 files, +2040/−3145. Root 32 → **14** non-test
files.

**Uncommitted in the working tree** — two logical units, both green:
- **Task D** — `MessageChannel` segregation + `SubscribableChannel`, `channel.WithSingleSubscriber()` (D-F),
  `StreamingSource` → `EventDrivenSource`.
- **Task E** — the regeneration of Spec 014, Plan 027, ADRs 0027/0028/0029, RFC-0002/0003,
  `docs/specs/011-http-adapter.md`, plus `CLAUDE.md`/`MESSAGING.md` rename touch-ups.

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

### 4.1 Round-3 adversarial audit — the immediate next step

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
