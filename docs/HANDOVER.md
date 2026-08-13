# Session handover — msgin

> **READ FIRST.** Read `CLAUDE.md`, then this file. **Trust `git log` over this document**, and over any
> memory. Every SHA and count below was measured at the moment of writing; **re-derive before relying on
> one** — that has failed in nine consecutive handovers.
>
> ### ✅ PLAN 027 IS COMPLETE AND MERGED TO `main`. THERE IS NO ACTIVE INCREMENT.
> ### You are starting fresh. Pick the next piece of work (§6).
>
> | | State |
> |---|---|
> | `main` | **`c3c3950`** — the merge commit. Tree clean, 8/8 modules green |
> | Pushed? | **YES.** `origin/main` = `c3c3950`, confirmed by reading the real remote (`git ls-remote`), not the local tracking ref |
> | Branch `claude/repo-structure-refactor-jt79t1` | **merged and DELETED**, local and remote. Both tips were proven ancestors of `main` first |
> | Tags | **zero, as always.** Do NOT propose tagging |
> | You are on | **`main`** — there is no feature branch. Start the next increment from a fresh one |

## 1. What just landed

Plan 027 — the pre-v1 core restructure. The flat root package became five subpackages
(`endpoint`/`routing`/`transform`/`channel`/`resilience`), `MessageChannel` was segregated into a send-only
pipe plus `SubscribableChannel`, the EIP lexical alignment landed, and expression evaluation moved to a
separate **`expr` module** so `expr-lang/expr` is no longer a core dependency.

**The workspace is now EIGHT modules.** Root, `expr`, `adapter/database/sql/{harness,postgres,mysql,sqlite,dbtest}`,
and `adapter/cron/crontest`. CI covers all eight.

### The last four commits (Task 12 + its review fixes)

| SHA | What |
|---|---|
| `c3c3950` | the merge commit |
| `21ad5dc` | `ci(release):` — `expr` had **no `release.yml` trigger pattern and no title case**, so tagging it would have fired no workflow at all. Both closed |
| `e6e074e` | `docs:` — Task 12: **`MIGRATION.md`** (new), the `CLAUDE.md`/`MESSAGING.md`/`RELEASE.md` sync, Spec 014 §4.1's fifth removal class, §5.0's census corrected 16 → 17, ADR 0028's two stale status blocks |
| `4828e27` | `fix(expr,endpoint):` — the `/code-review` bugs (below) |

## 2. Read these before touching anything

1. **`CLAUDE.md`** — hard rules. The ones that bite: **ask before writing implementation code**, **SDD is the
   default execution mode**, **never commit/push without approval** (narrow per-task-commit exception),
   **brainstorm → spec → ADR → plan → adversarial audit → code**, and the **8-module** command loops.
2. **`MIGRATION.md`** (repo root, new) — the authoritative old→new symbol map. If you wonder where a symbol
   went, it is in §4.
3. **`docs/plans/027-core-package-layout.md`** — the completed plan; its Task 12 section carries the full
   `/code-review` and `/security-review` records.
4. **`docs/specs/014-core-package-layout.md`** — §2.1 (the ten-row behavior register), §4.1 (the five removal
   classes), §5.0 (the census scope rule), §9 AC-7 (the six accepted uncovered blocks).
5. `.superpowers/sdd/027-core-package-layout/progress.md` — the SDD ledger, §F20/§F20b for this session.
   **Git-ignored, so NOT in a fresh clone — but it IS on disk on this machine**, which is where you are.

## 3. Verification state — all green at `c3c3950`

Every number measured, none transcribed.

| Gate | Result |
|---|---|
| 8-module `GOWORK=off go test ./... -race -shuffle=on` | **8/8 green** (`harness` by `go vet` — it has no tests and `go test` reports a false pass) |
| `apidiff` vs `docs/plans/027-root-api-baseline.txt` | **97 removals / 9 additions**, partitioned 87+6+1+1+2 with an **empty residual** |
| Root surface | **103** exported · **43** sentinels · **14** root `.go` files |
| 16 godoc gates | **16/16 GREEN**, vacuity-probed |
| Staleness sweep (Spec §8.1), both arms | **EMPTY**, both vacuity-probed |
| Coverage `-coverpkg=./...` | **93.7%**; 5 uncovered blocks, all inside AC-7's accepted six |
| `govulncheck` · `golangci-lint` · `gofmt` · `tidy` · `CGO_ENABLED=0` | clean in all eight |
| `/code-review` `main..HEAD` | 5 findings, **all fixed** |
| `/security-review` `main..HEAD` | **0 findings** |

**Regenerate the 16-gate script — it does not survive a session:**
```bash
awk '/^# ==== CANONICAL GATE BLOCK/{p=1} p{print} /^g 11c2 /{exit}' \
  docs/plans/027-core-package-layout.md > /tmp/gate11.sh   # 44 lines, 16 `g ` ids
bash /tmp/gate11.sh                                        # expect 16 GREEN, 0 RED
```

## 4. The two bugs the review caught — read this before writing an `expr` provider

`expr` compiles with `expr.AsKind(reflect.String)`, which constrains the **kind**. Both key-reading call sites
used `key, _ := out.(string)` — an **exact**-type assertion, which fails for a named type like
`type Region string`:

- **`RouteFunc` silently misrouted.** `routes[""]` is nil, `NewRouter` reads nil as "no destination", so every
  message routed on a domain-typed string took the default branch with **no error, no log, no hook**.
- **`Correlation` reported the wrong cause** — `Permanent(ErrNoCorrelation)` for a perfectly correlatable
  message.

Fixed by `stringResult` in `expr/compile.go`, which extracts **by kind**. **Both bugs sat behind a
100%-coverage module, 16/16 green godoc gates and a clean 8-module `-race` suite.** Coverage proves lines
RAN, not that they ran with the input that breaks them. A four-line probe test found both.

## 5. Delivery — all actioned, nothing outstanding

| Action | Status |
|---|---|
| Merge to `main` | ✅ `c3c3950`, `--no-ff`. Verified the merge tree is byte-identical to the branch tip |
| `git push origin main` | ✅ `0de54e9..c3c3950`, fast-forward, 36 commits |
| Delete the feature branch | ✅ local **and** remote |

**There are no pending approvals.** The next increment starts from a fresh branch off `main`.

### ⚠️ SSH TO GITHUB IS INTERMITTENT ON THIS MACHINE — retry, don't debug

Roughly **1 attempt in 5 succeeds**; the rest die with `git@github.com: Permission denied (publickey)`. It is
**not** a broken key and **not** the sandbox — measured: `ssh -T git@github.com` authenticates fine, the agent
holds the key (`ssh-add -l` → one ED25519), and a `-v` trace shows *"Server accepts key … Authenticated to
github.com"* on the attempts that work. Five identical `git ls-remote` calls in a row gave OK, FAIL, FAIL,
FAIL, FAIL. The remote branch delete took **4 attempts**.

**Consequences for the next session:**
- Wrap any remote git operation in a retry loop (8–12 attempts, ~4s apart) rather than concluding auth is
  broken.
- **Never trust `git rev-parse origin/<branch>` as evidence a push landed** — that reads a *local tracking
  ref*, which a failed fetch leaves stale. Confirm with `git ls-remote origin <branch>`, retried until it
  answers.

## 6. Backlog — triaged, nothing in flight

- **24 nil-option-element sites.** Every `for _, opt := range opts { opt(&cfg) }` calls the element unguarded,
  so `NewConsumer(src, h, nil)` **panics** on caller input — which CLAUDE.md forbids. A **uniform class
  wanting one uniform answer**, not 24 patches. **Re-derive the list; do not trust any written count.** This
  is the largest coherent piece of work left and the natural next increment.
- **`admit`'s ctx-done arm has no test forcing it** — coverage there is incidental.
- **`endpoint/consumer.go:467`'s ctx-done arm is genuinely flaky** (~1 run in 3). Do NOT gate on it; it is
  AC-7's accepted sixth block.
- **The no-sink discard arm is not machine-detectable** (Spec §2.1 row 10 explains why that is deliberate).
- **Plan 028 (`gin`)** was the pencilled-in next increment; its ADR is a forward reference and **there is no
  ADR 0024**.

## 7. Gotchas & environment

- **`export GOTOOLCHAIN=go1.25.12`** always — the local default is newer and the module must not build on
  1.26+. **`export PATH="$(go env GOPATH)/bin:$PATH"`** — `apidiff`, `gopls`, `govulncheck`, `gofumpt` all
  live there and none is on `PATH`.
- **`./...` is not the repo.** Root `go list ./...` → **11 packages**; `expr` is a **separate module** and is
  not among them. Use the 8-directory loops in CLAUDE.md's Commands section.
- **`dbtest` and `crontest` need a running Docker daemon** (testcontainers-go); they take ~110s and ~50s.
- **A coverage block id rots when ANY line above it moves**, including a pure comment edit. Diff the *source*
  at the two revisions before calling a shifted id a new gap.
- **`43 ≠ 43`.** Root's sentinel count read 43 before *and* after Task 9.5, but the **sets differ**.
  Reconcile by **name**, never by count.
- **`docs/plans/027-tools/symmap.tsv` is DERIVED** (96 entries). Regenerate before any staleness sweep.
- `.golangci.yml` sets `linters.default: none` — **`ST1000` and `unused` are both off**, so a missing package
  doc and dead code after a move are reported by nothing.
- `gofumpt -l .` reports **43 files repo-wide** and always has — pre-existing, zero delta. **CI gates
  `gofmt`**, which is clean.
- The docs-link gate emits exactly **two** known false positives (`docs/plans/m`,
  `docs/specs/factory(fireTime`) — both Go identifiers, not links. Anything else is real.
- **Never quote a SHA you have not checked with `git merge-base --is-ancestor`.** An amend orphans the old
  one, and it survives in *this machine's* reflog while being absent from a fresh clone — that exact trap
  cost a round this session. And do not write a dead SHA into the prose *explaining* the correction: the
  reachability sweep then reports your own explanation.
- `gopls` has **no Move refactoring**. Never commit `.claude/settings.json`; stage explicit pathspecs.
