# Session handover — msgin

> # ⛔ THIS DOCUMENT IS STALE — regenerate before relying on ANY of it (2026-08-11).
>
> It was written before **Task 9.6** and **Task 10** landed, and every one of the following is now false:
> the banner *"THE NEXT STEP IS TASK 9.6"*; the §1 status table (9.6 and 10 both read `NOT STARTED`); the
> §2 `git log` transcript; the *"five commits of real code"* / *"23 commits unpushed"* counts; and every
> *"seven modules"* claim — the workspace has **eight** since Task 10 added `expr`.
>
> **Authority, in order:** `docs/plans/027-core-package-layout.md`'s Progress table, then `git log`. This
> file is a session artifact and is rewritten wholesale at the next handover, not patched task by task —
> partially updating it produces a document whose table and prose disagree, which is worse than one that
> announces its own staleness. Task 12 owns the regeneration.
>
> *(Recorded by Task 10's fix round 3, whose sweep found it. Task 10 did not rewrite it: a handover is
> written from a safepoint at handover time, per CLAUDE.md, and is not a task deliverable.)*

> **READ FIRST.** Read `CLAUDE.md`, then `docs/plans/027-core-package-layout.md` (its **Progress table** is
> the authority on what is done), then `docs/specs/014-core-package-layout.md`. **Trust those files and
> `git log` over this one**, and over any memory.
>
> ### ✅ IMPLEMENTATION IS UNDER WAY. FIVE TASKS DONE. THE NEXT STEP IS TASK 9.6.
>
> The design phase closed at `1c4f73e`. Since then **five commits of real code** have landed. Tasks
> **9.7, 9, 9.5** are done and committed, plus **two out-of-plan `fix:` commits** that closed a defect class
> the review gates surfaced. The tree is a **clean safepoint**: `git status --short` empty, 11/11 root
> packages green under `-race -shuffle=on`, all seven modules green standalone.
>
> **Nothing has been pushed this session. 23 commits are unpushed** — pushing needs explicit approval.

## 1. Objective & position

`msgin` is a Go 1.25 Enterprise Integration Patterns library. The active effort is the **pre-v1 core
refactor** (Plan 027): flatten-to-packages, channel interface segregation, EIP lexical alignment.

**Remaining: Task 9.6 → 10 → 11 → 12.** Execution order is load-bearing — **Task 11 must run after Task 9.6**
(it re-verifies gates 9.6 turns green).

| Task | Size | State |
|---|---|---|
| **9.6** — reply-channel exclusivity probe (D-J) | M | **NOT STARTED — START HERE** |
| 10 — the `expr` provider module | L | NOT STARTED |
| 11 — package docs + unowned godoc obligations | M | PARTIAL |
| 12 — migration guide, doc sync, whole-branch gate | M | NOT STARTED |

## 2. Exact state

```
$ git log --oneline -6
511cefa fix(core,endpoint): guard the nil constructor arguments the With* sweep missed
910e092 refactor(core,http)!: move the expr sentinels out of root, clear the staleness sweep, widen the capability test
b4d1a1a fix(endpoint,routing): reject nil caller input instead of panicking
544cb5b feat(routing,transform): name the endpoint behavior types and add combinators
64963ad fix(core,endpoint,routing,transform,sql)!: classify deterministic endpoint faults as Permanent
1c4f73e docs(handover): close the design phase; next step is implementation at Task 9

$ git status --short
(clean, apart from this file's own edit)
```

**VERIFY THESE, NEVER COPY THEM** — they have been wrong in five consecutive handovers:
`git rev-parse --short main @{u}` · `git rev-list --left-right --count @{u}...HEAD`.
Measured at `511cefa`: `main` = `0de54e9`, `@{u}` = `6f44db6`, **26 ahead of `main`**, **23 unpushed**, 0 behind.
Committing this file makes them 27 / 24.

**This file cannot state its own commit's SHA or counts** — committing it invalidates them. `HEAD` is
identified by *subject*: run `git log --oneline -6` and read the top line. Every SHA above is an ancestor and
safe to cite.

### What the five commits delivered

| SHA | Task | Substance |
|---|---|---|
| `64963ad` | **9.7** | **D-M** five producers return `Permanent(sentinel)` + position · **D-N** invalid messages fall back to the dead-letter sink · **D-P** that divert is single-shot |
| `544cb5b` | **9** | `Predicate`/`RouteFunc`/`SplitFunc`/`Transformer` + `And`/`Or`/`Not` |
| `b4d1a1a` | — | `WithLogger(nil)` no longer kills the process; `NewAggregator` rejects nil strategies |
| `910e092` | **9.5** | expr sentinels leave root (102→100 exported, 43→41 sentinels) · both sweep arms empty · capability test 9 → 24 subtests |
| `511cefa` | — | `NewConsumer(src,nil)`, `OutboundGateway(nil)`, `Chain(nil step)` |

## 3. Read in this order

1. `CLAUDE.md` — hard rules. **SDD is the default execution mode; ask before writing any implementation code.**
2. `docs/plans/027-core-package-layout.md` — **Task 9.6 is at line ~1097**. Its Progress table is authoritative.
3. `docs/adrs/0030-reply-channel-exclusivity-probe.md` — **read this before writing anything for 9.6.** Its
   four rejected alternatives *are* the design; two of them look cheaper until you read why they lost.
4. `docs/specs/014-core-package-layout.md` §5.1, §8 · ADRs `0028`, `0029` (§5.0a/§5.0b), `0007` (D7/D8).
5. `docs/plans/027-derivation-findings.md` — the execution ledger. **§F15–F18 are this session's record.**

## 4. Decisions made this session

| | Decision | Outcome |
|---|---|---|
| **D-P scope** | *User ruling.* Single-shot covers the **whole invalid path**, not just the D-N fallback — a configured `WithInvalidMessageSink` whose `Send` fails is also discarded, not Nacked. The plan said "the invalid path" while ADR 0007 D7 and Spec row 7 said "when `invalidSink == nil`"; the **documents were widened to match the code** | ADR 0007 D7, Spec §2.1 row 7 |
| **Oversize exemption** | *User ruling.* `ErrPayloadTooLarge` is **exempt** from D-N's fallback. Routing its rejects to the DLQ wrote attacker-supplied oversize bytes verbatim into the operator's durable store — the defence became the vector. Does not weaken D-N: that class was never *captured* before D-N, so the exemption **restores** prior behavior | `endpoint/consumer.go` `invalidTarget`, ADR 0007 D7, Spec row 7 |
| **Shutdown exception** | A `Send` that fails only because the settle context was cancelled by the shutdown deadline is **Nacked for redelivery**, not discarded — nothing was learned about the sink. `OnInvalidMessage` does not fire | `endpoint/consumer.go` `divertTerminal` |
| **`Chain` nil element** | **Substitute** the degradation step, do not skip. Skipping trades a visible failure for a silent one — a dropped `Filter` stops filtering | `handler.go` |
| **Census correction** | The `MessageChannel` census is **15**, not the plan's projected 14. Naming a type **relocates** an occurrence: `RouteFunc`'s own declaration is a census line. Task 12 re-measures — **expect 15** | Plan Task 9 |

**Open, deliberately not decided** — a dead-lettered message carries no settlement-reason header, so a DLQ
reader cannot distinguish *"retries exhausted"* from *"permanently invalid"*. Recorded as a limitation with
the SPI seam noted; the remedy today is to configure `WithInvalidMessageSink`.

## 5. Backlog — triaged, NOT fixed (ledger §F18)

- **24 nil-option-element sites.** Every `for _, opt := range opts { opt(&cfg) }` loop calls the element
  unguarded, so `NewConsumer(src, h, nil)` panics. Deliberately deferred: it is a **uniform class needing one
  uniform answer** (skip the element vs reject at construction), and fixing a subset would repeat the
  partial-sweep pattern that produced this session's findings. §F18 has the full site list, the reproduction,
  both candidate fixes (**B, reject at construction, recommended** — but 8 of the 24 return no error and need
  A as a local fallback) and the command that regenerates the list. **Re-derive; do not trust the list.**
- **`admit`'s ctx-done arm has no test forcing it** (§F18.5). Its coverage today is incidental.
- **Trailer-less `docs:` commits** — unchanged disposition; see the rule in §8 of the previous handover
  (recoverable via `git show 1c4f73e:docs/HANDOVER.md`).

## 6. Next actions

1. **START HERE: Task 9.6** (plan line ~1097). Read **ADR 0030 first**. It ships, in one commit: 2 root
   exported symbols (`ExclusiveSubscribable`, `ErrSharedReplyChannel`) whose godoc is **verbatim normative
   text from ADR 0030 §1** — *copy, do not retype*, gate 8.11 is a seven-conjunct phrase match; 2 `channel`
   methods + assertions + a table test; `endpoint.WithSharedReplyChannel`; a four-outcome `NewChannelExchange`
   godoc rewrite; two test fakes; `safeSingleSubscriber` (D-O) with its sixth truth-table row.
2. **Ask before writing implementation code, and default to SDD.** Plan approval does **not** authorize the
   execution mode.
3. **Run the §11 gate baseline for your task and confirm RED first.** Gates are pinned per task, not to the
   untouched tree.
4. **Before merge:** `/code-review` and `/security-review` over `main..HEAD`. **The assistant cannot launch
   `/code-review` — the user must run it.**

## 7. What this session's gates actually caught — read before trusting a green suite

Six defects reached committed-quality code and **not one was visible to the test suite**, which was green at
100% coverage throughout:

| Defect | Found by |
|---|---|
| D-P scope contradicted ADR 0007 + Spec row 7 | adversarial review |
| Payload bytes leaked into a WARN, 3 lines from a "never the payload" godoc | adversarial review |
| Shutdown cancellation misread as a sink refusal → healthy-sink message discarded | `/code-review` |
| `And`/`Or` returned `(true, err)`, contradicting their own godoc | adversarial review |
| `WithLogger(nil)` / nil aggregator strategies → panic on caller input | whole-branch `/code-review` |
| `NewConsumer(src, nil)` → **46,106 retries in 200 ms** | the class sweep ordered after the above |

**Three lessons, all earned this session:**
- **A test that has never failed proves nothing.** Three separate cases were *vacuous* — the plan's `Not`
  trap, the `And`/`Or` right-operand cases, and capability row 5 — each passing against the very
  implementation it existed to reject. **Mutation-test every new assertion.**
- **Scope the sweep to the class, not the symptom.** `b4d1a1a` swept `With*` options; that scoping is exactly
  why `OutboundGateway`, `Chain` and `NewConsumer` survived it.
- **Coverage numbers lie in two ways here.** `-coverpkg=./...` reports *"of statements in `./...`"* — a
  whole-module figure, not a package one (`routing` reads 58.2% that way and 100.0% isolated). And
  `endpoint`'s headline swings **99.44% / 99.16% across runs on an unchanged tree**, because `admit`'s
  ctx-done arm is a scheduler coin-flip. Measure per package, isolated; prove deltas arithmetically.

## 8. Gotchas & environment

- **`export GOTOOLCHAIN=go1.25.12`** always; **`export PATH="$(go env GOPATH)/bin:$PATH"`** — `apidiff`,
  `gopls`, `govulncheck`, `gofumpt` live there and none are on `PATH`.
- **`./...` is not the repo** — seven modules. CI covers only six (`adapter/cron/crontest` is missing from
  both jobs; **Task 10 fixes it**), and CI runs **eight** steps per module to the local loop's two. Passing
  locally does not mean CI passes.
- `go tool cover -func` resolves line numbers against the **current** source — running it on a profile from
  another revision silently mis-attributes every block.
- `.golangci.yml` sets `linters.default: none` — **`ST1000` and `unused` are both off**, so missing package
  docs and dead code after a move are reported by nothing.
- `gopls` has **no Move refactoring**.
- The docs-link gate's arm 1 emits exactly **two** known false positives (`docs/plans/m`,
  `docs/specs/factory(fireTime`) — both Go identifiers, not links. Anything else is real.
- Repo has **zero git tags** — do NOT propose tagging.
- Never commit `.claude/settings.json`; stage explicit pathspecs.
- **Per-task commits are pre-authorized** by the approved plan; `git push`, merges, tags and branch deletion
  are **not**. A commit the plan does not spell out (like `b4d1a1a` and `511cefa`) needs its own approval.
