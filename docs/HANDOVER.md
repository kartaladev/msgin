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
- **`SettleMembers` — cut Task 11?** Both auditors that examined it say yes, and Plan 027 pre-authorised the exit.
  This is a **scope reduction, so it is the user's call** — do not decide it unilaterally.
- **Four design decisions the revision pass needs** (audit record §H): `RetryPolicy.delayFor` (export vs move
  `RetryPolicy`); `MessageChannel` vs `OutboundAdapter` (collapse vs deliberate synonyms); `Chain`/`To` (move to
  `endpoint` vs restate §9.1); confirm `ReleaseStrategy` → `(bool, error)`.
- **Unanswered, unrelated to the audit: rename `Permanent` → `Terminal`?** The user proposed it; I recommended
  **keeping `Permanent`** and they have not replied. Reasons: `terminal` is already load-bearing in
  `handler.go`/`activator.go` meaning "end of chain"; NATS `Term` is on the roadmap and would collide; and
  `permanent`/`transient` is the established antonym pair across six files. `NonRetryable` offered as second
  choice. **If a rename happens it must ride this window** — `Permanent` is exported, so afterwards it costs a
  major bump.

## 5. Next actions

1. **Commit the uncommitted docs** (§2) — approval-gated, but do it before anything else.
2. **Get the user's answers to audit record §H** (the `SettleMembers` cut and the four design decisions). The
   revision pass cannot be done coherently without them.
3. **Do the revision pass** across Spec 014 + ADRs 0027–0029 + Plan 027. The heavy items:
   - Rewrite Spec 014 §3 with **four** file splits and add a **symbol-level table** (18 identifiers) plus a
     **45-row test-file table**. Add a Task 3.5 for shared-helper resolution before any extraction.
   - Fix the false claims (audit §E) and the missing ADR citations (audit §D).
   - Replace the §9.1 acceptance criterion with the scriptable one (`go list -deps .` has no subpackage).
   - Correct the plan header's non-existent "gopls Move"; make Task 1's tidy per-module.
4. **Run round 2** — same three-lens parallel Opus audit on the revised bundle. Round 2 is required, not
   optional: the fixes rewrite the normative move-list and change public signatures.
5. **Only after round 2 passes**, ask the user for the go-ahead and delegate implementation via SDD, one fresh
   implementer subagent per task.

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
