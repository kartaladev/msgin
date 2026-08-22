# Session handover — msgin

> **READ FIRST.** Read `CLAUDE.md`, then this file, then the governing spec/plan/ADR named in §3 — and **trust
> those files and `git log` over this document.** Every count below was measured when written; **re-derive before
> relying on one.** That instruction has now failed in fifteen consecutive handovers, including twice inside the
> session that wrote this one — where an adversarial audit corrected *this session's own* freshly-derived counts.
>
> ### ⚠️ THIS SESSION WROTE **NO GO CODE**. Two `docs:` commits sit on a feature branch. Nothing is merged or pushed.
> ### Next: the **Plan 031 adversarial audit** (§5). It is a hard CLAUDE.md gate and it has not run.
>
> | | State |
> |---|---|
> | Branch | **`chore/backlog-sweep-post-029`**, clean, **2 commits ahead of `main`** |
> | `main` | **`2b2dec1`** — unchanged this session; `origin/main` identical |
> | Working tree | **clean** |
> | Go files changed on branch | **ZERO** (`git diff --name-only main..HEAD -- '*.go' | wc -l` → 0) |
> | Suite | **11/11 root packages green** under `-race -shuffle=on`, measured at `2b2dec1`; no Go file has changed since, so it still holds |
> | Tags | **zero, as always.** Do NOT propose tagging |

## 1. What this session did

It worked the **backlog in the previous handover's §6** — the user asked for all of it, continuously, while away.
It produced **design and adjudication, not implementation**, and stopped at CLAUDE.md's ~60% context rule.

| Commit | What |
|---|---|
| `2c4aa98` | **Spec 017 + ADR 0033 + Plan 031** — the group-member bound (backlog item 7). **UNAUDITED.** |
| `b54fbbe` | **Plan 030 rev 2 + its immutable audit record** — backlog items 5, 8, 4; and **item 2 closed WONTFIX** |

### The one result worth reading even if you read nothing else

**Backlog item 2 is CLOSED-WONTFIX — it was never a defect.** Collapsing the seven duplicated delegator
nil-option pre-check loops turns the **Spec 015 AC-7 guard gate red at all seven sites**: `hasNilElementGuard`
(`option_guard_gate_test.go:219-238`) clears a parameter only on an `*ast.RangeStmt` over that parameter **inside
the constructor's own body**, and a helper call is invisible to it (the helper is non-variadic, so it is never
scanned). The gate already ships a committed probe — `TestOptionGuardRecognizer/PROBE qualified type —
msghttp.Option, unguarded delegator` — asserting exactly the post-refactor shape is unguarded.

The duplication is the deliberate consequence of **two ratified decisions acting together**: ADR 0031 D-R chose
per-package duplication over exporting an internal from root, and the gate then enforces the inline loop shape.
Repairing it means amending a shipped spec **and** ADR, and weakening a gate whose syntactic strictness is the
point — to net **~14 lines** (the backlog's "~35" is gross, not net). **The gate is working as designed. Do not
re-propose this without first deciding to amend Spec 015 AC-7.** Full evidence chain: Plan 030 rev 2, decision
**D1**.

## 2. The backlog was wrong about almost everything — re-derive, don't trust

Two verification passes plus one adversarial audit corrected **three of the four** mechanical items, and the audit
then corrected *this session's* corrections. Both layers are recorded so the drift is auditable:

| Item | Backlog said | Session derived | Audit corrected to |
|---|---|---|---|
| 2 — dedup loops | 7 sites, ~35 lines | 7 sites, ~14 net | **CLOSED-WONTFIX** |
| 5 — false godoc | 4 sites | 11 sites | **16** (8 production + 8 test) |
| 8 — 32-bit overflow | "fails in 4 packages" | 24 compile errors | 24, but **split by arm**, not one value |
| 4 — gin ADR | ADR missing | + plan number wrong in 6 places | + `docs/rfcs/` never swept; more sites |

**Why item 5 kept growing:** `grep -rn "first statement"` returns 19 hits, but the phrase **wraps across comment
lines**; a wrap-tolerant scan returns 24. One missed site (`adapter/http/helpers.go:16-17`) is the **production
twin** of the very inversion this session's own plan called "the more serious error" while fixing only the test
copy. Plan 030 rev 2 Task 1 now states the invariant instead of enumerating: *no comment may assert a statement is
a function's first statement when `fd.Body.List[0]` is a different statement.*

**Why item 8 can't take one value:** the class gate's **six `safe`-arm rows** (`sizing_option_class_gate_test.go`
`:568, :587, :612, :634, :647, :666`) assert a knob *accepts* an absurd value and its product stays usable "past
the point where a buggy comparison (e.g. an int32 truncation) would misbehave" (`:544-547`). `1<<30` **is** an
int32 value, so converting them leaves the assertions passing while probing nothing. They take `math.MaxInt` —
legal there precisely because those rows assert `require.NoError` and carry **no** decimal string, which is the
objection the old handover recorded against a `math.MaxInt` constant. Reject-arm sites take `1<<30`.

## 3. Traceability — read these before acting

- `CLAUDE.md` (binding; workflow, gates, dependency policy, reporting format)
- **Item 7 (next up):** [`docs/specs/017-group-member-bounds.md`](specs/017-group-member-bounds.md) ·
  [`docs/adrs/0033-group-member-bounds.md`](adrs/0033-group-member-bounds.md) ·
  [`docs/plans/031-group-member-bounds.md`](plans/031-group-member-bounds.md)
- **Items 5/8/4:** [`docs/plans/030-post-029-maintenance.md`](plans/030-post-029-maintenance.md) rev 2 ·
  [`docs/plans/030-audit-round-1.md`](plans/030-audit-round-1.md) (**immutable**)
- **Predecessors:** [`docs/specs/016-sizing-option-bounds.md`](specs/016-sizing-option-bounds.md) ·
  [`docs/adrs/0032-sizing-option-bounds.md`](adrs/0032-sizing-option-bounds.md) ·
  [`docs/specs/015-nil-option-elements.md`](specs/015-nil-option-elements.md) ·
  [`docs/adrs/0031-nil-option-elements.md`](adrs/0031-nil-option-elements.md)

## 4. Item 7 — what the design says, and the four things it left open

**The defect.** `routing.WithCompletionSize`'s `1<<16` ceiling is the **only** bound on per-group member count,
and it is gated on `cfg.completionSizeSet`, a field only that option writes. The other three release paths bypass
it: `WithReleaseStrategy` and `WithReleaseWhen` are caller-supplied closures, and `defaultRelease` reads its
threshold from `msgin.HeaderSequenceSize` — **data, not code**. `memory.GroupStore.Add` appends with no member cap
and `slices.Clone`s per call, so an unreleased group grows monotonically at **quadratic** cost. `WithMaxGroups`
bounds the *number* of groups, never members. Only the opt-in `WithGroupTimeout` reaper mitigates it.

**The decision (D-AC…D-AL):** the bound goes at the **accumulation site** — the store — not the release decision,
because only the store observes every append. New `memory.WithMaxGroupMembers`, default `1<<16`, ceiling `1<<20`,
`msgin.ErrOverflowDropped` on overflow, mirroring `WithMaxGroups` exactly.

**Two facts the design work sharpened:**
1. `WithReleaseStrategy` is invisible to the class gate via the **named-type** path (`*ast.Ident{"ReleaseStrategy"}`),
   not the `*ast.FuncType` path — and **`defaultRelease` has no parameter at all**, so *no* widening of the AST
   scan can ever reach it. That is what makes store-side enforcement **necessary**, not merely convenient.
2. The **SQL group store is worse than memory**: it has no member cap *and* no group-count cap, and its `Add`
   re-fetches and re-decodes **every live member** on every arrival.

**🔴 The four open questions live in Spec 017 §8. One needs the user before Task 4:**

1. **SQL enforcement point.** Counting after `AddMember` returns bounds nothing — the row is committed and the
   bytes already materialised. In-transaction enforcement is the only atomic-across-instances option, but it adds
   a **`maxMembers int` parameter to `GroupDialect.AddMember` across 5 call sites in 4 modules**, roughly doubling
   the increment. **Confirm before Task 4 starts, not at Task 6.**
2. **memory/sql counting asymmetry** — `len(g.msgs)` (live+claimed) is right for memory, wrong for SQL. Flagged as
   the finding most likely to be reversed.
3. **Transient rejection at the boundary** — a group at exactly the cap rejects arrivals during the claim window.
   Bounded and retryable (`ErrOverflowDropped` is *not* in `IsPermanent`), accepted and documented.
4. **An unenforceable invariant** — `default maxGroupMembers >= completionSizeCeiling` cannot be tested (both
   constants unexported in different packages; proving it behaviourally costs 8.6 s and 48.3 GiB). Defended by
   cross-reference comments plus a grep. Stated open, not closed.

## 5. Next actions, in order

1. **🔴 Run the Plan 031 adversarial audit — HARD GATE, NOT OPTIONAL.** A fresh **Opus** subagent, handed
   **spec 017 + ADR 0033 + plan 031 together**, attacking the design before any code. It has **not** run. Plan 030's
   audit returned 3 BLOCKERs on a *far* simpler bundle, so expect findings. Two rounds is this project's norm.
2. **Get the user's answer on §4 question 1** (the `AddMember` SPI change). It roughly doubles the increment.
3. **Execute Plan 030** — three tasks, all prose/test-only, no production Go. Order: Task 1 (godoc, 16 sites) →
   Task 2 (32-bit, split by arm) → Task 3 (citations). Dispatch **SDD implementer subagents**; the main session
   must not self-implement without per-task user approval.
4. **Then Plan 031**, after its audit is clean and question 1 is answered.
5. **Whole-branch gate before any merge:** `/code-review` + `/security-review` over `main..HEAD`, findings resolved
   or triaged in writing, coverage gate, then the 8-module CI-parity loops.

## 6. Pending approvals — nothing here was decided for the user

1. **Adopting `github.com/gin-gonic/gin`.** Plan 030 Task 3 deliberately **does not write ADR 0024** — that would
   decide the dependency by side effect, and CLAUDE.md's Dependency policy makes it an architectural decision.
   Task 3 removes the *false citations* only, and states the gin increment as **unnumbered until written** (the
   auditor explicitly cleared that against the traceability rule). **The adopt-gin decision is still yours.**
2. **Merge, push, tag, branch deletion.** All still require per-action approval; none were taken. Nothing left
   this machine.
3. **Every design decision in ADR 0033 (D-AC…D-AL) was taken without user ratification** while the user was away.
   Each carries a **REVERSIBILITY** line for exactly this reason.

## 7. Carry-forward — what is still open

| # | Item | State |
|---|---|---|
| 2 | Dedup the delegator loops | **CLOSED-WONTFIX** (§1; Plan 030 decision **D1**). Not a defect. |
| 3 | Guard gate is syntactic, not a dominance proof | **🔴 Now load-bearing** — D1 rests on it. `go/analysis` promotion stays rejected pre-v1; Plan 031 should widen the gate's *stated limitations* to name func-typed and named-type options. |
| 4 | gin plan number + ADR 0024 | **Citations retired by Plan 030 Task 3** — the gin increment is now stated **unnumbered until written** and ADR 0024 as **reserved but unwritten** in Spec 011 (×4 sites), ADR 0023 (×3), Plan 020, Plan 027, `docs/rfcs/README.md` (×3) and CLAUDE.md. No concrete number was substituted — that is the class fix. **The adopt-gin dependency decision is still open and still yours** (§6.1). |
| 5 | False "first statement" godoc | Planned as Plan 030 Task 1, **16 sites**. |
| 6 | Byte-ceiling class | **CLOSED** by [Spec 018](specs/018-byte-cap-ceilings.md) / [ADR 0034](adrs/0034-byte-cap-ceilings.md) / [Plan 032](plans/032-byte-cap-ceilings.md). `msghttp.WithMaxBodyBytes`, `WithMaxEventBytes` and `WithMaxResponseBytes` are bounded at `byteCapCeiling = math.MaxInt32` (a **representability** ceiling, not a payload guess), each rejecting out-of-range values through its own existing sentinel. The open question was answered **no off-state** (ADR 0034 **D-AN(b)**): `-1` and `0` are already taken by the typed rejection, so one would cost new exported surface whose only purpose is to re-enable the hazard. The class gate's `deferred` arm is now **empty** — 12 fixed / 1 rejects / 0 deferred / 6 safe — and [Spec 016](specs/016-sizing-option-bounds.md) §3.8 item 2's never-scheduled **hazard-disclosure godoc** shipped in the same commit. |
| 7 | Aggregator group growth | **Designed (`2c4aa98`), unaudited, unimplemented.** |
| 8 | 32-bit test overflow | Planned as Plan 030 Task 2, split by arm. |
| 9 | **Derive the class gate's prose counts from `wantArms` at test time** | **OPEN, unscheduled.** `sizing_option_class_gate_test.go` restates the arm partition in ~10 prose locations — the header's arm list, its arithmetic identity, the per-arm literal block, the `arm` field's doc comment, two section banners, the `wantArms` rationale comment's illustrative map, and **two live assertion messages** — with **no mechanical link** to the `wantArms` map the test already computes from. Nothing fails when one drifts: four audit rounds each patched the instances they were shown and were overtaken (7 → 12 → 14 → 16 → **17** sites), and round 4's seventeenth (`:22`) carries no arm name, no literal and no digit, so **no selector can find it**. Designed at [Spec 018 §8 item 5](specs/018-byte-cap-ceilings.md). *Fix the class, not the instance.* |

## 8. Gotchas — these will bite

- **`GOTOOLCHAIN=go1.25.13`.** `harness` has no test files — `go test` there is a false pass; use `go vet`.
- **`govulncheck` lives in `$(go env GOPATH)/bin`**, not on `PATH` — `which` reports it missing while it exists.
- **The docs-link gate has exactly two known false positives** — `docs/plans/m` and
  `docs/specs/factory(fireTime`, both Go identifiers in wrapped code spans. **Verified clean at `b54fbbe`, both
  arms, and vacuity-probed on the NEW files rather than root** (planting a bad link and a bad anchor produced
  exactly one hit each; both vanished on revert). Anything else is a blocker.
- **`apidiff` is blind outside the root package** (Plan 028 proved it). Plan 030 rev 2's Global constraint 1 now
  prescribes an exported-symbol **AST set diff by name** across all packages, with the vacuity probe planted in
  `adapter/http` — *not* root. **Two baselines exist**; `028-root-api-baseline.txt` is the newer one.
- **`go vet` aborts after the first error per package.** For the full 386 list use
  `GOARCH=386 GOOS=linux go test -gcflags=all=-e -run=NONE ./...` — 24 errors, not 4.
- **Counts in a plan are not the definition of done.** Plan 030's "17 reject-arm sites" is 15 edits — two entries
  consume a shared `const n`. The 386 compile list is the authority.
- **`*-audit-round-*.md` and `*-derivation-findings.md` are IMMUTABLE** execution records — they correctly record
  what was true when written. Delivered plans (e.g. `020`, `027`) are *not* immutable and may be corrected in
  place; Plan 020 already carries such a correction.
- **A measurement is only as good as its fixture AND its protocol** — state both beside every figure.
- **An implementer subagent can invent scope and stall.** Check the tree, the commit **and** the report
  independently rather than trusting a status.
- **`.superpowers/` is git-ignored.** Never commit `.claude/settings.json`.
- The old handover's §5 housekeeping is **done**: `fix/sizing-option-bounds` no longer exists locally.
