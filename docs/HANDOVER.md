# Session handover — msgin

> **READ FIRST, before doing anything.** Read `CLAUDE.md` (root), then **`docs/plans/027-audit-round-1.md`**, then
> the pointers in §3. Trust those files over this handover and over any memory.
>
> **⛔ DO NOT DELEGATE CODE WRITING YET.** The design bundle exists but **failed its round-1 adversarial audit —
> all three auditors returned `NEEDS-REVISION`, 18 HIGH findings.** Plan 027 Task 4 cannot compile as written.
> The next work is a **revision pass, then a round-2 audit** — not implementation. Spec 014, ADRs 0027–0029 and
> Plan 027 all carry DO-NOT-IMPLEMENT banners; do not remove them until round 2 passes.
>
> **Safepoint:** branch `claude/repo-structure-refactor-jt79t1` @ `28dd9e4` + uncommitted docs (see §2). No Go code
> has changed at any point — the entire branch is documentation. `go build ./...` green.

## 1. Objective & roadmap position

`msgin` is a Go 1.25 Enterprise Integration Patterns library (`github.com/kartaladev/msgin`). HTTP/SSE work is
complete (Plans 025/026 merged). **The active effort is the pre-v1 refactor program.**

All five RFCs in `docs/rfcs/` are **Accepted** — 22 open questions settled with the user, one at a time. The first
slice (RFC-0001 + 0002 + 0003) was promoted to **Spec 014 + ADRs 0027/0028/0029 + Plan 027**, then audited. The
audit failed it.

**Sequencing decision that still stands:** the breaking window runs **FIRST**, ahead of the feature roadmap. The
gin binding is renumbered off 027 (it becomes Plan 028) — but `docs/specs/011-http-adapter.md:94,677` still says
027, which is audit finding **D3** and must be fixed.

## 2. Exact state

Branch `claude/repo-structure-refactor-jt79t1`, four commits ahead of `main` (`6f44db6`), **not pushed**:

- `a54f023` — `docs(rfcs): settle all open questions; accept the refactor program`
- `2463a4f` — `docs: promote the first refactor slice to spec 014, ADRs 0027-0029, plan 027`
- `145c26c` — `docs(handover): refactor program accepted; first slice promoted, audit pending`
- `28dd9e4` — `docs(claude): refresh project status from greenfield to pre-v1`

**Uncommitted in the worktree** (the audit record + the DO-NOT-IMPLEMENT banners + this handover):

```
?? docs/plans/027-audit-round-1.md
 M docs/specs/014-core-package-layout.md
 M docs/adrs/0027-core-package-restructure.md
 M docs/adrs/0028-channel-interface-segregation.md
 M docs/adrs/0029-eip-lexical-alignment.md
 M docs/plans/027-core-package-layout.md
 M docs/HANDOVER.md
```

Commit these first — the audit record is the session's most valuable artifact and exists nowhere else. No code
changed; `GOTOOLCHAIN=go1.25.12 go build ./...` green.

## 3. Traceability pointers (read in this order)

1. `CLAUDE.md` — workflow, gates, conventions that OVERRIDE defaults.
2. **`docs/plans/027-audit-round-1.md`** — the full round-1 findings, grouped by theme, with the decisions the
   revision pass needs (§H) and what round 2 must re-audit (§I). **This is the working document.**
3. `docs/rfcs/README.md` — program index, decided package layout, promotion status, sequencing.
4. `docs/rfcs/0001`–`0005` — each RFC's **§7 Decisions**. Read §7 *before* §3; several §3 passages predate the
   decisions and carry dated resolution notes rather than being rewritten.
5. `docs/specs/014-core-package-layout.md`, `docs/adrs/0027`/`0028`/`0029`, `docs/plans/027-core-package-layout.md`
   — the bundle under revision.

## 4. Decisions, deviations, and what the audit overturned

**Settled with the user this session** (all recorded in the RFCs' §7): C-full; EIP-chapter package names
(`endpoint`/`routing`/`transform` + `channel`, `resilience`); `MessageChannel` split with `PollableChannel`
omitted; named func types with combinator methods, names dropping the package's qualifier; `expr-lang` to its own
module while `robfig/cron` stays (rule: *weight material to non-users*, 7.1 MB vs 144 KB); two-phase `DedupStore`
Claim/Settle with a lease. **User overrode my recommendation on four:** `Poller` exported, `Once` ships, Enricher
is a full endpoint (S→M), `robfig` in-module (which deleted RFC-0004's own success metric).

**What the audit overturned — see the audit record for evidence.** Five structural claims I wrote into the bundle
are verified **false**: the "ADR 0003 says the core is one package" quote (that phrase exists nowhere but my own
files), the `endpoint → channel` import edge, "no call site subscribes through the interface", "root 32 → 9" (the
table yields 12), and "every reference is godoc prose, not code". Four of these sat inside acceptance criteria.

**Verified and settled for good:** Spring Integration does name its equivalent interface `RequestReplyExchanger`,
so "keep `Exchange`, qualified" stands and Plan 027 Task 3's blocker is cleared.

**Pending approvals / open questions:**

- **Nothing is pushed.** Pushing needs explicit approval.
- **All six §H decisions are SETTLED** (user, 2026-07-27) — see audit record §H, which now records each with its
  rationale, and §J for the execution order. In brief: **cut `SettleMembers`** from Plan 027; **delete
  `delayFor`** and inline it in `endpoint` (`RetryPolicy` stays in root as vocabulary); **keep both
  `MessageChannel` and `OutboundAdapter`**; **keep `Chain`/`To` in root** and make §9.1 a scriptable check;
  **`ReleaseStrategy` → `(bool, error)`** with the bool-only form kept as sugar; **keep `Permanent`** (no rename).
- **Two standing criteria the user set, which govern the whole revision:**
  1. *"Make the library as flexible as possible with sensible defaults / opinionated — higher quality, ready for
     production use."* Where a choice is balanced, resolve toward **an easy default path plus a fully capable
     escape hatch** (CLAUDE.md's *Sensible defaults*), never trading one away for the other.
  2. **Consistency with Pipes and Filters.** `MessageChannel` is the EIP **Pipe**, `Step` is the **filter**, and
     `Chain` assembles the pipeline — that is why the channel type is not collapsed into `OutboundAdapter` (a
     Channel Adapter, a different pattern) and why the assembler stays in root with the vocabulary.
     `doc_composition.go:4` already states this model and **Task 1 deletes that file**, so the revision must carry
     the Pipes-and-Filters framing into the new package docs rather than parking it.

## 5. Next actions

1. **Do the revision pass** — this is the whole job, and it is a fresh-session-sized piece of work. All decisions
   are settled (§4); **follow audit record §J's execution order** and do it as **one atomic pass**, because most
   consistency findings are "document A now disagrees with document B" and partial integration manufactures
   exactly that defect. Touches ~15 files across `docs/rfcs`, `docs/specs`, `docs/adrs`, `docs/plans`. The heavy
   items:
   - Rewrite Spec 014 §3 with **four** file splits and add a **symbol-level table** (18 identifiers) plus a
     **45-row test-file table**. Add a Task 3.5 for shared-helper resolution before any extraction.
   - Fix the false claims (audit §E) and the missing ADR citations (audit §D).
   - Replace the §9.1 acceptance criterion with the scriptable one (`go list -deps .` has no subpackage).
   - Correct the plan header's non-existent "gopls Move"; make Task 1's tidy per-module.
   - Carry the **Pipes-and-Filters framing** out of the deleted `doc_composition.go` into the new package docs.
2. **Run round 2** — the same three-lens parallel Opus audit on the revised bundle (design/API correctness;
   plan-level execution; cross-document consistency), each handed the complete bundle with an explicit
   evidence-or-discarded output contract. Round 2 is required, not optional: the fixes rewrite the normative
   move-list and change public signatures.
3. **Only after round 2 passes**, ask the user for the go-ahead and delegate implementation via SDD — one fresh
   implementer subagent per task, coordinator verifies green and commits, adversarial reviewer before delivery.

## 6. Gotchas & environment

- **`export GOTOOLCHAIN=go1.25.12`** always (bare `go1.25` is rejected).
- **`./...` is not the repo.** Seven modules today, eight once `expr` lands; the gate is the per-module
  `GOWORK=off` loop in CLAUDE.md's Commands section. **Every satellite `go.mod` carries `expr-lang // indirect`
  under a `replace` to the local root**, so dropping the dep makes six modules need `go mod tidy` at once.
- **`gopls` has NO Move refactoring** (`api-json` v0.23.0 exposes rename options only), it is not on PATH (only
  `$(go env GOPATH)/bin/gopls`), and it has been unavailable inside subagents in past sessions. Package moves are
  `git mv` + package clause + `goimports -w`, with `go build ./...` as the authoritative reference-finder. Do not
  write "use gopls Move" back into the plan.
- Tooling: `govulncheck` at `$(go env GOPATH)/bin/govulncheck`; **`gofumpt` NOT installed** — use
  `test -z "$(gofmt -l .)"`; `golangci-lint` on PATH. `.golangci.yml` sets `linters.default: none` and does not
  enable ST1000, so a missing package doc will not be caught.
- `.github/workflows/ci.yml` has a **pre-existing gap**: the `module` matrix and the `workspace` job both omit
  `adapter/cron/crontest`. Fix it in the same edit that adds `expr`.
- `dbtest` and `crontest` need a running Docker daemon.
- Repo has **zero git tags** — do NOT propose tagging. This is what makes every break in Spec 014 affordable, and
  it is also why the `SettleMembers` "must ride the window" argument fails.
- `.claude/settings.json` has been permanently dirty in past sessions — never commit it; stage explicit pathspecs,
  never `git add .`. (It is clean right now.)
