# Session handover — msgin

> **READ FIRST.** Read `CLAUDE.md`, then this file, then the governing spec/plan/ADR named in §3 — and **trust
> those files and `git log` over this document.** Every count below was measured when written; **re-derive before
> relying on one.** This session produced the sharpest evidence yet for why: a **comment-only** commit (`de38a95`)
> silently invalidated **41** `file:line` citations in an in-flight design bundle that had just passed a dedicated
> mechanical sweep — and the citation *of the finding about stale citations* was itself off by eleven.
>
> ### ✅ SEVEN OF EIGHT BACKLOG ITEMS SETTLED. 20 commits on a feature branch. NOTHING MERGED OR PUSHED.
> ### Next: **ratify or reverse D-AC…D-AT** (§6.1) — the only thing blocking the last item.
>
> | | State |
> |---|---|
> | Branch | **`chore/backlog-sweep-post-029`**, clean, **20 commits ahead of `main`** |
> | `main` / `origin/main` | **`2b2dec1`**, untouched — verified with `git ls-remote origin main` |
> | Working tree | **clean** |
> | Suite | **11/11 root packages green**, `-race -shuffle=on`, at `7abc9f8` |
> | `GOARCH=386` | **vet clean** (was 24 compile errors at the branch point) |
> | Other gates | `govulncheck` clean · `golangci-lint` 0 issues · `apidiff` 0/0 |
> | Tags | **zero, as always.** Do NOT propose tagging |

## 1. What this session did

It worked the backlog in the previous handover's §6 — all of it, continuously, while the user was away.

| # | Item | Outcome |
|---|---|---|
| 2 | Dedup the 7 delegator pre-check loops | **CLOSED-WONTFIX** — not a defect (§2) |
| 3 | Guard gate is syntactic | **Adjudicated**; rejection stands, folded into Plan 031 Task 9 |
| 4 | gin plan number + ADR 0024 | **DELIVERED** `7ab91cd` |
| 5 | False "first statement" godoc | **DELIVERED** `1a1c135`, corrected again by `de38a95` (§2) |
| 6 | The byte-ceiling class | **DELIVERED** `f39725d` + `a306241` — 4 audit rounds |
| 7 | Aggregator group growth | **HALF DELIVERED** `7abc9f8` — 5 audit rounds; SQL half awaits §6.1 |
| 8 | 32-bit test overflow | **DELIVERED** `d2c69fe` — 24 → 0 |

**Two increments shipped runtime behaviour:** the three `msghttp` byte caps now carry `byteCapCeiling =
math.MaxInt32`, and `memory.GroupStore` now bounds members per group. Both cleared adversarial design audits
before a line of code was written — **four rounds and five rounds** respectively.

## 2. The three results worth reading even if you read nothing else

**Backlog item 2 was never a defect.** Collapsing the seven duplicated delegator pre-check loops turns the
**Spec 015 AC-7 guard gate red at all seven sites** — `hasNilElementGuard` clears a parameter only on an
`*ast.RangeStmt` over it *inside the constructor's own body*, and a helper call is invisible (the helper is
non-variadic, so it is never scanned). The gate ships a committed probe asserting exactly the post-refactor shape
is unguarded. The duplication is the ratified consequence of **ADR 0031 D-R** (per-package duplication over
exporting an internal from root) **and** that gate, acting together. Repairing it means amending a shipped spec
*and* ADR to net ~14 lines — not the ~35 the backlog claimed, which was gross. **The gate is working as designed.**
Full chain: Plan 030 decision **D1**.

**A fix for a false statement introduced a false statement.** `1a1c135` replaced *"the loop is the first
statement"* with *"preceded only by the **zero-value** config initializer"* — true only of `msghttp.NewConfig`,
**false at the ten other sites it was applied to**. `memory.NewGroupStore` opens with `maxGroups: 1024`, the
default for *the very option the same sentence goes on to discuss*. Fixed in `de38a95`. That commit also
overcounted its own class: it claims twelve sites; the wrap-tolerant sweep finds eleven, one already correct.

**Selectors must match the property, not the string.** The same defect appeared four ways this session:
`grep "first statement"` misses ~5 sites because the phrase **wraps across comment lines**; `grep 'must be > 0'`
missed six sites spelling the same contract as `n<=0`; the class-gate site inventory was declared complete and
wrong in **three consecutive rounds**; and the fix for that inventory was itself a wider token list that missed
three more. The durable remedy now sits in Plan 031 Task 9 — **a script whose output *is* the table** — and round
5 verified it by extracting and re-running it.

## 3. Traceability — read before acting

- `CLAUDE.md` (binding). **Its project-status counts were self-falsifying and are now command-derived** — the
  command is the authority, never the number beside it.
- **Item 7 (the live one):** [`docs/specs/017-group-member-bounds.md`](specs/017-group-member-bounds.md) ·
  [`docs/adrs/0033-group-member-bounds.md`](adrs/0033-group-member-bounds.md) ·
  [`docs/plans/031-group-member-bounds.md`](plans/031-group-member-bounds.md) rev 5 · audit rounds
  [1](plans/031-audit-round-1.md) [2](plans/031-audit-round-2.md) [3](plans/031-audit-round-3.md)
  [4](plans/031-audit-round-4.md) — **immutable**
- **Item 6 (delivered):** [`docs/specs/018-byte-cap-ceilings.md`](specs/018-byte-cap-ceilings.md) ·
  [`docs/adrs/0034-byte-cap-ceilings.md`](adrs/0034-byte-cap-ceilings.md) ·
  [`docs/plans/032-byte-cap-ceilings.md`](plans/032-byte-cap-ceilings.md) · audit rounds
  [1](plans/032-audit-round-1.md) [2](plans/032-audit-round-2.md) [3](plans/032-audit-round-3.md)
  [4](plans/032-audit-round-4.md)
- **Items 4/5/8 (delivered):** [`docs/plans/030-post-029-maintenance.md`](plans/030-post-029-maintenance.md) ·
  [`docs/plans/030-audit-round-1.md`](plans/030-audit-round-1.md)

## 4. Where Plan 031 stands — half delivered, half held

**Delivered (`7abc9f8`), needing no ratification:** `memory.WithMaxGroupMembers` (default `1<<16`, ceiling
`1<<20`), the cap check, the classification split, `Handle`'s six-exit snapshot branch, and the class-gate rows.
**21 mutants, 21 killed.** Three details are load-bearing, each proven by a killing mutant:

1. **The cap check sits BETWEEN the dedup `seen` lookup and the `g.ids` insert.** After the insert, a rejected
   member is recorded as seen, so on redelivery the dedup branch reports success, `Handle` returns `nil`, and the
   source **Acks a message that was never appended**.
2. **`Add` returns the live snapshot ALONGSIDE the error**, so `Handle` re-evaluates the release. Without it an
   id-less message arriving at a full group after a failed release deadlocks permanently — a case that **worked
   before the change**.
3. **Classification is by cause, on `g.leased`.** Not-leased ⇒ `Permanent` (settles terminally, never consults
   `MaxAttempts`, so it is correct on the shipped zero-value `RetryPolicy`); leased ⇒ transient. Without the
   split, the default configuration hot-spins with no log and no dead-letter.

> **One mutant survived its first pass and the FIXTURE was rewritten, not the count.** The obvious way to drain a
> group is to claim and settle all of it — but `SettleGroup` deletes the whole group when the residual is empty,
> taking `g.ids` with it, so the dedup set vanished regardless of where the cap check sat and the case passed
> against the mutant it exists to kill. It now leaves a residual.

**Held for ratification — Tasks 4–8.** They change `GroupDialect.AddMember`'s signature across **four modules**
and alter runtime behaviour in three dialect modules. Execution order is `1 → 2 → 4 → 5+6 → 3 → 7 → 8 → 9 → 9b →
10`. Task 1 is done, so **Task 2 is next** and is `routing`-only.

## 5. Gates run, and what they found

- **`/code-review` over `main..HEAD`** — safe to merge; 0 blockers, 0 majors, 3 documentation minors, **all
  fixed** in `de38a95`. It mutation-tested the byte-cap gate on a scratch copy: three mutants, all killed.
- **`/security-review`** — **no HIGH or MEDIUM findings.** Verified no `int64`→`int` narrowing exists on any
  remote-driven path; on 32-bit `byteCapCeiling == math.MaxInt`, so the accepted range is exactly the
  representable range.
- **8 modules × 8 CI-parity steps** — all green, nothing skipped; Docker was up, so `dbtest` and `crontest` ran
  real containers rather than being waived.
- Coverage: `adapter/http` **100%**, `routing` **100%**, `adapter/memory` **74% → 88%**, root **95.6%**.

## 6. Pending approvals — nothing here was decided for you

1. **🔴 Ratify or reverse D-AC…D-AT.** Eighteen decisions taken while you were away, each carrying a written
   **REVERSIBILITY** line. This is the only thing blocking Plan 031's SQL half. The two with real operational
   weight, both in Spec 017 §8:
   - **A `COUNT(*)` on every `AddMember`, not only on overflow.** Forced, not chosen: the `LIMIT maxMembers+1`
     SELECT is live-only and cannot see claimed members under any limit. It reuses the already-shipped
     `pgCountMembers`/`mysqlCountMembers`/`sqliteCountMembers`, so it is zero new SQL. On **sqlite** it extends a
     database-wide write lock, so the cost is global rather than per-key.
   - **A crashed releaser holds the lease for up to `2 × WithGroupLeaseTTL` ≈ 10 minutes** (eligibility at
     `t₀ + leaseTTL`, discovery at the first reaper tick after it), during which rejection is a retry loop paying a
     full rolled-back transaction plus a `SchemaExists` probe per iteration. Kept **transient** deliberately —
     classifying it permanent would dead-letter messages the default configuration is about to admit.
2. **Adopting `github.com/gin-gonic/gin`.** Untouched. Plan 030 Task 3 fixed the *false citations* around ADR 0024
   and deliberately did **not** write the ADR, because writing it decides the dependency by side effect.
3. **Merge, push, tag, branch deletion.** None taken. Nothing left this machine.

## 7. Carry-forward — still open

| # | Item | State |
|---|---|---|
| 2 | Dedup the delegator loops | **CLOSED-WONTFIX** (§2; Plan 030 **D1**). Do not re-propose without first deciding to amend Spec 015 AC-7. |
| 7 | Group-member bound, **SQL half** | Designed, audited ×5, **SAFE TO IMPLEMENT**, blocked on §6.1 alone. Next task: **Task 2** (`routing`-only). |
| — | gin increment | Unnumbered until written; ADR 0024 reserved but unwritten. Dependency decision open (§6.2). |
| — | **AST checker → permanent gate** | Plan 030 Task 1's throwaway checker pairs each "first statement" comment against its function's `fd.Body.List[0]`. The class grew **4 → 11 → 16** sites across three counts, each time because the detection method was weaker than the defect. A committed AST invariant ends it; another grep will not. |
| — | **`WithPollMaxBatch`'s safe-arm gate row is magnitude-insensitive** | **Pre-existing, verified not a regression** — it survives an `int32(n)` truncation mutant at *both* `1<<62` and `math.MaxInt`, because polling one message at a time still delivers both. Fix: assert on batch **size**, not eventual arrival. |
| — | **Derive the class gate's prose counts from `wantArms` at test time** | `sizing_option_class_gate_test.go` restates the arm partition in ~10 prose locations — including **two live assertion messages** — with **no mechanical link** to the map the test already computes. Nothing fails when one drifts: four rounds each patched the instances they were shown and were overtaken (7 → 12 → 14 → 16 → **17** sites), and the seventeenth (`:22`) carries no arm name, no literal and no digit, so **no selector can find it**. Designed at [Spec 018 §8 item 5](specs/018-byte-cap-ceilings.md). |

## 8. Gotchas — these will bite

- **Any artifact citing `file:line` is stale from the moment it is written.** Two instances one commit apart this
  session. Cite the **symbol and the grep that locates it**, not a coordinate.
- **`git log --oneline | grep -i <plan>` misses commits** whose subject lacks the number. Derive delivery from the
  trailer: `git log --format='%h %s' --grep='Plan: 030'`. Plan 030's unticked checkboxes are a **bookkeeping
  artefact** — a delivery banner at the top of that file now says so, after an audit was misled by them.
- **`apidiff` exits 0 even when it reports changes** — the OUTPUT is the signal, never `$?`. It is also blind
  outside the root package.
- **`go vet` aborts after the first error per package.** For the full 386 list use
  `GOARCH=386 GOOS=linux go test -gcflags=all=-e -run=NONE ./...`.
- **Two modules give false passes in opposite directions:** `harness` has no test files (`go test` passes
  vacuously — use `go vet`); `dbtest` has **only** `_test.go` files (`go build` compiles nothing — use `go vet`).
- **`govulncheck` lives in `$(go env GOPATH)/bin`**, not on `PATH` — `which` reports it missing while it exists.
- **The docs-link gate has exactly two known false positives** (`docs/plans/m`, `docs/specs/factory(fireTime` —
  Go identifiers in wrapped code spans). Verified at `7abc9f8`, both arms, vacuity-probed.
- **`*-audit-round-*.md` and `*-derivation-findings.md` are IMMUTABLE.** Delivered plans are **not** — Plans 020,
  029 and 030 all carry in-place supersession notes.
- **An auditor's summary can disagree with its own table.** It happened in two consecutive rounds, both by one row
  in the same direction. Fixed by instructing round 4 to *generate the score by counting the table*; recorded
  unreconciled where it occurred, per the `43 ≠ 43` rule.
- `GOTOOLCHAIN=go1.25.13`. `.superpowers/` is git-ignored. Never commit `.claude/settings.json`.
