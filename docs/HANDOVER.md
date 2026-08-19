# Session handover — msgin

> **READ FIRST.** Read `CLAUDE.md`, then this file, then the bundle in §3. **Trust `git log` and the tree over this
> document.** Every count below was measured when written; **re-derive before relying on one** — that has failed in
> ten consecutive handovers, and it failed twice inside this session (see §5).
>
> ### ✅ DESIGN COMPLETE AND AUDIT-CLEARED. NO CODE HAS BEEN WRITTEN YET.
> ### Four adversarial rounds are done; round 4 returned **SAFE TO IMPLEMENT, no round 5**.
> ### Next action: create the branch and run **Plan 028 Task 0**. Ask the user for the execution mode first.
>
> | | State |
> |---|---|
> | `main` | **`2da63d9`** — the design bundle. **Not pushed** (`origin/main` is still `d86ef3d`) |
> | Bundle commits | `7f0e5ba` `spec(core):` Spec 015 · `2da63d9` `docs(plan):` ADR 0031 + Plan 028 + 4 audit records |
> | Working tree | **clean**, verified by `git status --short` at the moment of writing |
> | Branch | **none yet.** You are on `main`; `feat/nil-option-elements` must be created off it |
> | Code written | **none.** Not one line |
> | Suite at `2da63d9` | **11/11 root packages green** — `GOWORK=off go test ./... -race -shuffle=on`. No code changed, so this is `main`'s own state |
> | Tags | **zero, as always.** Do NOT propose tagging |
>
> **This is a SAME-MACHINE handover.** Anything git-ignored is still on disk — the `.superpowers/` SDD ledgers, the
> `$(go env GOPATH)/bin` tools, the Docker images `dbtest`/`crontest` pull. A fresh *clone* would not have them.

## 1. What this session did

Took the top backlog item from the previous handover — **the unguarded `for _, opt := range opts` loops that panic on
caller input** — through brainstorm → spec → ADR → plan → **four adversarial audit rounds**, with no implementation
code, per CLAUDE.md's design-time gate.

The census turned out to be **32 exported constructors**, not the 24 loops the old backlog implied: 8 delegate their
options into another's loop.

## 2. The design, in one paragraph

A nil option **element** must never panic. The constructor reports it through its own return if it has one; otherwise
through the first use of the object it produced; only where neither surface exists does it skip and say so in godoc.

| Family | Count | Mechanism |
|---|---|---|
| **R1 — reject at construction** | 25 | bare `ErrNilFunc` naming `pkg.Ctor` + 0-based index |
| **R2 — degrade at first use** | 5 | latched `Permanent(ErrNilFunc)`, reported by every error-returning method, **before** that method's own argument checks |
| **R3 — skip, documented** | 2 | `msgin.New`, `sqlite.DSN` — the only two products with **no** error surface |

`24 apply loops = 17 R1-non-delegating + 5 R2 + 2 R3`; `25 R1 = 17 + 8 delegators`.

Seven decisions: **D-P** (three mechanisms), **D-Q** (reuse `ErrNilFunc`, mint nothing), **D-R** (all 32 guard
themselves), **D-S** (latched fault is `Permanent`-wrapped), **D-T** (`NewCircuitBreaker` gains an error return),
**D-U** (the R2 loop `continue`s), **D-V** (latch reported before a method's own argument checks; `routing.Filter`
realigned).

## 3. Read these before acting

1. **`CLAUDE.md`** — hard rules. The ones that bite: **ask before writing implementation code**, **SDD is the default
   execution mode**, **never commit/push without approval** (per-task commits inside an approved plan are the one
   narrow exception), and the 8-module command loops.
2. **`docs/specs/015-nil-option-elements.md`** — revision 5. §2.1 inventory, §3 contract, §6 the eleven ACs.
3. **`docs/adrs/0031-nil-option-elements.md`** — revision 5, decisions D-P…D-V.
4. **`docs/plans/028-nil-option-elements.md`** — revision 5, **Tasks 0–8**.
5. **`docs/plans/028-audit-round-{1,2,3,4}.md`** — the four audit records. Read round 4 first: it is the shortest and
   it compile-proves D-U/D-V.

## 4. Next actions, in order

1. **Ask the user for the go-ahead to implement AND the execution mode** — SDD subagents (the project default) vs.
   direct main-session. Plan approval is **not** execution-mode approval; this is a per-task decision, never standing.
2. `git checkout -b feat/nil-option-elements`.
3. **Plan 028 Task 0 first** — install `apidiff`, capture `docs/plans/028-root-api-baseline.txt` from clean `main`,
   prove it diffs 0/0 against its own source, commit it. **Nothing else can pass without it**: the `027` baseline is
   the pre-Plan-027 surface and reports 97/9 on an untouched tree.
4. Then Tasks 1 → 8 in order. Per-task commits are pre-authorized once the user picks an execution mode; `git push`,
   merge and branch deletion are **not**.

## 5. Decisions & corrections made across four audit rounds — do not re-litigate

- **The skip-everywhere mechanism was killed** (round-1 BLOCKER-2). `handler.go:44-51` says verbatim that skipping
  *"was rejected"* for a nil `Chain` element, for a reason that transfers exactly. Revision 1 had cited lines 52-63 of
  that same function as its justification.
- **D-S held under a dedicated attack** (round 2) and was *strengthened*: a latched fault left bare would be
  **dead-lettered** by `producer.go:447-453` and would record the circuit breaker **unhealthy** at `consumer.go:716`.
  The original "cost is cosmetic" claim was wrong in D-S's own favour.
- **`apidiff` baseline — the round-3 BLOCKER.** `027-root-api-baseline.txt` is the **pre-Plan-027** surface and
  reports **97 removals / 9 additions** on the untouched tree (this handover's own predecessor recorded that number).
  AC-8 had demanded 0/0 against it, making Task 1 unsatisfiable on arrival. **Plan Task 0 now captures
  `docs/plans/028-root-api-baseline.txt` from `main` and must run first.**
- **Two derivation scripts produced false tables**, both caught by audit: the census grep could not match a
  **qualified** option type (`...msghttp.Option`) and missed 2 constructors; the precedence script matched only
  `== nil` / `== ""` and misclassified `resilience.NewTokenBucket`, whose guard is numeric. **Derive by reading, or
  by AST — not by pattern-matching, and never cite a table you have not re-read.**
- **AC-6's mutant was vacuous** (round-2 M-E): reverting a guard makes the constructor *panic*, so every case went
  red regardless of what it asserted. Now a per-assertion mutant table.
- Counts corrected along the way: `NewCircuitBreaker` **10** call sites (not 11 — the declaration was counted);
  the all-error-returns alternative is **103** call sites (not ~107); precedence split is **9 validate-first /
  8 loop-first** (not 8/9); `FuncLit`s in the AC-7 hazard table are **5** (not 4).

## 6. What the four rounds settled — and what they cost

Round 1: 2 blockers (census 30→32; the skip mechanism contradicted `handler.go:44-51` verbatim). Round 2: 3 blockers,
all in the **verification layer** (an AC that did not compile, one unexecutable for `NewGateway`, an AST gate blind to
generics). Round 3: 1 blocker (AC-8 gated on a baseline that is red on arrival) + the two decisions now called D-U and
D-V. Round 4: **no blocker** — D-U and D-V compile-proven, full suite green under them.

**Every round found something the previous one missed, and three of the four blockers were in the layer meant to
*prove* the design correct rather than in the design itself.** Do not treat the ACs as boilerplate.

## 7. Gotchas & environment

- **`export GOTOOLCHAIN=go1.25.13`** always; **`export PATH="$(go env GOPATH)/bin:$PATH"`** — `apidiff`, `gopls`,
  `govulncheck`, `gofumpt` all live there and none is on `PATH`.
- **`./...` is not the repo.** Root `go list ./...` → 11 packages; `expr` is a separate module. Use CLAUDE.md's
  8-directory loops.
- **`dbtest` and `crontest` need Docker** (~110 s and ~50 s).
- **SSH to GitHub is intermittent on this machine** — roughly 1 attempt in 5 succeeds. Retry (8–12 attempts, ~4 s
  apart) rather than debugging, and **never** trust `git rev-parse origin/<branch>` as evidence a push landed; use
  `git ls-remote origin <branch>`.
- The docs-link gate emits exactly **two** known false positives (`docs/plans/m`, `docs/specs/factory(fireTime`) —
  both Go identifiers, not links. Arm 1 is **clean** on all **seven** bundle files as of this writing.
- **`apidiff` is NOT installed** (`which apidiff` → not found). Task 0 step 0 installs it; do not skip that.
- **Nothing is pushed.** `origin/main` is still `d86ef3d`. Pushing needs explicit per-action approval, and this
  machine's SSH to GitHub is intermittent (see above).
- `.golangci.yml` sets `linters.default: none` — `ST1000` and `unused` are both off.
- `gofumpt -l .` reports 43 files repo-wide and always has; CI gates `gofmt`, which is clean.
- `gopls` has **no Move refactoring**. Never commit `.claude/settings.json`.
