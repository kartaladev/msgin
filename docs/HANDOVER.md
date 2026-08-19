# Session handover — msgin

> **READ FIRST.** Read `CLAUDE.md`, then this file, then the artifacts in §3. **Trust `git log` and the tree over
> this document.** Every count below was measured when written; **re-derive before relying on one** — that has
> failed in eleven consecutive handovers.
>
> ### ✅ PLAN 028 IS IMPLEMENTED, REVIEWED AND GATE-GREEN. NOT MERGED, NOT PUSHED.
> ### Next action: ask the user to approve **push → merge → branch deletion**. None is pre-authorized.
>
> | | State |
> |---|---|
> | Branch | **`feat/nil-option-elements`**, 14 commits, off `main` at `2da63d9` — **re-derive with `git log --oneline main..HEAD \| wc -l`; two SHAs were rewritten late (see §6)** |
> | `main` | **`2da63d9`** — the design bundle. **Still not pushed** (`origin/main` is `d86ef3d`) |
> | Working tree | see `git status --short` — expected clean at handover time |
> | Suite | **8/8 modules green**, `-race -shuffle=on`, incl. Docker-backed `dbtest`/`crontest` |
> | Coverage | **93.9%** (`-coverpkg=./...`); Plan 027's figure was 93.7% |
> | Tags | **zero, as always.** Do NOT propose tagging |
>
> **SAME-MACHINE handover.** Anything git-ignored is still on disk — the `.superpowers/sdd/028-nil-option-elements/`
> ledger and per-task reports, `$(go env GOPATH)/bin` tools, the Docker images. A fresh *clone* would not have them.

## 1. What this session did

Executed **Plan 028** end to end via **subagent-driven development** (user-approved execution mode): a fresh
implementer subagent per task, controller verification, an adversarial reviewer per task, fix loops where needed,
then the whole-branch delivery gate.

**All 32 exported functional-option constructors now handle a nil option element instead of panicking**, across 10
packages, plus an AST class gate that stops the class from returning.

## 2. The delivered design, in one paragraph

A nil option **element** never panics. The constructor reports it through its own return if it has one; otherwise
through the first use of the object it produced; only where neither surface exists does it skip and say so in godoc.

| Family | Count | Mechanism |
|---|---|---|
| **R1 — reject at construction** | 25 | bare `ErrNilFunc`, position `"<pkg>.<Ctor>: nil option at index <i>"`, 0-based, first-nil-wins |
| **R2 — degrade at first use** | 5 | latched `Permanent(ErrNilFunc)`, reported by every error-returning method **before** that method's own argument checks |
| **R3 — skip, documented** | 2 | `msgin.New`, `sqlite.DSN` — the only two products with **no** error surface |

**Confirmed by counting shipped code, not by trusting the plan:** 8 `nilOptionAt` copies (one per R1 package,
none in `msgin`/`channel`/`sqlite`), **32** nil-element guards in non-test files, **30** distinct position strings
(32 − the 2 R3 that emit none). `32 = 25+5+2`; `30 = 25+5`.

## 3. Read these before acting

1. **`CLAUDE.md`** — hard rules. The ones that bite: **ask before writing implementation code**, **SDD is the
   default execution mode**, **never commit/push without approval**, and the 8-module command loops.
2. **`docs/specs/015-nil-option-elements.md`** — §2.1 inventory, §3 contract, §6 the eleven ACs.
3. **`docs/adrs/0031-nil-option-elements.md`** — decisions D-P…D-V.
4. **`docs/plans/028-nil-option-elements.md`** — Tasks 0–8.
5. **`.superpowers/sdd/028-nil-option-elements/progress.md`** — the execution ledger: every task's evidence,
   every reviewer's independent verification, every deferred minor, and the reasoning behind each deferral.
   Git-ignored, so it exists only on this machine.

## 4. Next actions, in order

1. **Ask the user to approve, as separate actions:** `git push -u origin feat/nil-option-elements`, the merge to
   `main`, and `git branch -d` + `git push origin --delete`. **None is pre-authorized.** `main` itself is also
   still unpushed (`origin/main` = `d86ef3d`), so pushing `main` is a further ask.
2. **SSH to GitHub is intermittent on this machine** — roughly 1 attempt in 5 succeeds. Retry 8–12 times ~4s
   apart rather than debugging, and **never** trust `git rev-parse origin/<branch>` as evidence a push landed;
   use `git ls-remote origin <branch>`.
3. After merge, delete the branch — CLAUDE.md requires it.

## 5. Delivery-gate evidence (all re-derived this session)

| Gate | Result |
|---|---|
| 8 modules × 8 CI steps | **all green** — build, vet, gofmt, CGO_ENABLED=0, `go mod tidy` + no diff, govulncheck, golangci-lint, `go test -race -shuffle=on` |
| Docker-backed modules | `dbtest` and `crontest` **really ran** — the Task 5 m-13 deferral is discharged |
| Workspace coherence | 8/8 directories build with `GOWORK` unset |
| Coverage | **93.9%**, up from 93.7% |
| `apidiff` vs `docs/plans/028-root-api-baseline.txt` | **0/0** — *root package only; see §6* |
| Exported surface, **all** packages, `main` vs `HEAD` | **649 decls each side, differing by exactly one line** — `NewCircuitBreaker` gaining its error return (D-T, the one sanctioned break) |
| `govulncheck` | clean on all 8 modules after the `go1.25.13` bump |
| Docs-link gate | both arms clean; arm 2 vacuity-proved by planting a bad anchor |

## 6. Gotchas discovered this session — these will bite the next one

- **`apidiff` is structurally BLIND to non-root packages.** The baseline is root-package export data
  (`apidiff -w <file> .` captures one package, not the module), so a green `apidiff` certifies **nothing** for
  `adapter/*`, `channel`, `routing`, `resilience`. Proven by planting an exported symbol in
  `adapter/database/sql` — no output; the same symbol in root **is** reported. **A vacuity probe that plants its
  symbol in root proves the gate FIRES, not that it COVERS** — that is how this survived Task 0.
  **Use an exported-surface AST diff instead** (§5 row 6) for any non-root claim.
- **Traceability trailers are not machine-parseable.** `Spec:/Plan:/ADR:` are present as text and
  `git log --grep` finds them, but `git interpret-trailers`/`%(trailers)` sees only the final paragraph, which is
  the co-authorship block. **Pre-existing** — `d86ef3d` and `21ad5dc` on `main` have the identical shape — and
  **no CI check enforces it**. User decision: **accept and record, do not rewrite.**
- **Do not run whole-suite verification while a reviewer is planting mutants.** A controller run reported `FAIL`
  that was purely a reviewer's in-flight probe files. Check `git status --short` first.
- **A subagent's `git commit --amend` can hit the WRONG commit if the controller commits concurrently.** That
  happened here: the Task 7 implementer amended while I was committing the toolchain bump, so its `--amend`
  landed on **my** commit. It detected and repaired the damage itself — history is intact and both commits were
  verified afterwards to hold exactly their own files, with the chore message byte-identical — but **two SHAs
  changed** (`f9d5a82`→`52334fe`, `c053165`→`cf3063d`). **Never let a subagent amend while the controller may be
  committing**, and tell every implementer to run `git log -1` immediately before any `--amend`.
- **`gofmt -l .` at the repo root flags git-ignored scratch** under `.superpowers/`. CI only ever sees tracked
  files — check with `git ls-files '*.go' | xargs gofmt -l`.
- **`harness` has no test files**, so `go test` there is a false pass. Use `go vet`.
- **`GOTOOLCHAIN=go1.25.13`** now (bumped from `.12` this session, user-approved, commit `f9d5a82`, clearing four
  reachable stdlib CVEs). `docs/plans/*` and `docs/specs/*` still say `go1.25.12` **on purpose** — they are
  immutable execution records.
- `gopls` has **no Move refactoring**. Never commit `.claude/settings.json`.

## 7. Review outcomes

- **Security review over `main..HEAD`: SAFE TO MERGE.** No Critical/High/Medium. It verified in shipped code
  that D-S's claim holds — the latched fault is `Permanent`-wrapped, `IsPermanent` uses `errors.As` so it
  traverses the wrap, and `producer.go:462` returns **before any backoff**, so there is no hot-retry loop. Zero
  new `panic(` in the diff; all 25 `nilOptionAt` sites pass a compile-time literal, so no DSN, URL, header or
  payload can reach an error string.
  - **Low, documented, no action:** `cb, _ := resilience.NewCircuitBreaker(nil)` yields a nil breaker, and
    `endpoint/flowcontrol.go:62` treats nil as "no breaker" — a caller who *actively discards* the error runs
    with the protective control absent. ADR 0031 D-T documents this; `errcheck` catches the discard.
- **Code review over `main..HEAD`: NO CRITICAL, NO IMPORTANT. SAFE TO MERGE.** It attacked the code rather than
  the documents: confirmed both R3 assignments are structurally correct (`Message[T]`'s six methods and
  `NewMessage` return no error; `sqlite`'s `DDL`/`InboxDDL`/`GroupDDL` take no `DSNOption`); read **all 25 R1
  precedence directions body-by-body** and found **zero** contradictions, so the Task 1/4 class did not recur;
  proved **no R1 delegator forwards into an R2 product**, so no bare/wrapped crossover exists; and
  **mutation-tested the class gate across a module boundary** — deleting the R3 `continue` from
  `sqlite/dsn.go` (a *separate* `go.mod`) and the R1 guard from `endpoint/gateway.go`, both caught with
  `file:line:col`. That last check closes the one place `apidiff`'s blindness could have hidden a regression.
  - **Its one Minor is FIXED** (`4ce4d84`, comment-only): `adapter/memory`'s latch comment claimed
    "set once, read-only, needs no lock" unconditionally. `type Option func(*Broker)` is **exported and hands
    the option the live product**, so the guarantee holds only while no caller-supplied `Option` lets the
    `*Broker` escape `New`. It is the only one of the five R2 types where the option's parameter *is* the
    product — visible only by comparing all five, which is why per-task review could not see it.

## 8. Deferred / backlog — carried forward deliberately

1. **`memory.WithBuffer` overflow panic.** `WithBuffer(n)` does `make(chan …, n)` unbounded, so
   `WithBuffer(1<<62)` panics on the `make`. Round-4 audit **m-Z6** records that D-U's `continue` *widens* the
   window: under a `break` loop `memory.New(nil, WithBuffer(1<<62))` stopped at the nil; under `continue` it now
   proceeds into the `make`. **Owned by Spec §3.7 as out of scope for Plan 028.** The headline contract says *no
   constructor panics on a nil option element*, and this combination still does — **strongest candidate for the
   next increment.**
2. **Seven copies of the delegator pre-check loop** in `adapter/http` (×5) and `adapter/http/stdlib` (×2). A
   package-local `nilOptionPreCheck(ctor, opts)` helper collapses each to one line (~35 lines saved),
   within-package so D-R is untouched. Deferred at the gate rather than edit seven individually mutation-proven
   constructors for line count.
3. **The AST gate is syntactic, not a dominance proof.** Two contrived shapes defeat it — a vacuous pre-check
   loop followed by a second unguarded apply loop, and a guard inside a never-called closure. Both are named in
   the file header. Promoting the recognizer to a `go/analysis` analyzer driven by `go vet -vettool` was
   considered and rejected as out of scope pre-v1 (it costs a ninth module and a CI step across 8 modules).
4. **The `gin` increment** still needs a plan number after 028, and its ADR is still a forward reference to an
   unwritten artifact (CLAUDE.md §"Project status" says so).
5. Minor godoc wording, deferred across tasks: four sites say the apply loop is "this constructor's first
   statement" when a `cfg := …` initializer precedes it (direction and subject are correct — this is *not* the
   reversed-precedence defect Tasks 1 and 4 hit); and `adapter/http/options.go:1111` has a line break leaving a
   dangling `(` before `ErrInvalidMaxBodyBytes`. **Fix the class in one pass**, not instance by instance.

## 9. What the execution cost, and what caught what

Nine implementation tasks, **three fix rounds** (Tasks 1, 4, 7). **Every fix round was a defect a per-task review
caught that the implementer had not** — and two of the three were **documentation stating the opposite of the code
it described**, in a library whose stated quality criterion is debuggability:

- **Task 1** — the `ErrNilFunc` godoc claimed the nil-option error was `Permanent`-wrapped and came from "any
  exported functional-option constructor". Both false: R1 is bare (25 of 32), and the 2 R3 never return it.
- **Task 4** — two godocs said the nil-option check "runs BEFORE X **and loses to it**". Running before means it
  *wins*; the task's own test asserted the opposite of its own documentation.
- **Task 7** — the class gate accepted a guard that did not prevent the panic, and (found by `/simplify` at the
  gate) accepted `continue` from a constructor that could have reported the fault — the exact BLOCKER-2 class the
  plan's first audit round killed.

**Reviewers independently re-ran mutants in every task** and twice found what implementers missed — including that
`apidiff` was blind to most of what it was believed to cover. **The decision the whole plan rested on (D-U,
`continue` not `break`) is observable on exactly ONE surface in the entire library**, and Task 3's AC-5c nil-first
test is that one guard; two reviewers reproduced it failing with the predicted wrong sentinel.

**Two agents stalled** on infrastructure faults (stream watchdog, no progress for 600s). Neither lost work — the
second had already written and verified its code, and was resumed with a **commit-first ordering** (commit the
green unit, *then* mutation-prove) so a third stall could not lose it. Worth reusing.
