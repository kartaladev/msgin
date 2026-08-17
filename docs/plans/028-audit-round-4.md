# Plan 028 — adversarial design audit, round 4 (2026-08-17)

Independent Opus subagent, fresh, handed the revision-4 bundle plus all three prior audit records, **before any
implementation code existed**. Narrowly scoped by round 3: the census of 32, the R1/R2/R3 partition, **D-S** and
**D-T** were excluded from re-attack, having each held under three independent rounds. Evidence-primary.

## Verdict: **SAFE TO IMPLEMENT.** No BLOCKER. No round 5.

3 MAJORs + 9 MINORs, **all in the verification layer**, none requiring a user decision — one editing pass over
Spec §3.2/§3.5/§3.7/§4.3/AC-2/AC-3/AC-5d/AC-6 and Plan Tasks 0/1/2/7/8. *"A fifth round would be ceremony."*

---

## 1. D-U and D-V — COMPILE-PROVEN, the first time in four rounds a decision was verified by execution

The auditor patched a **clone** (repo unmodified) with D-U (`continue`) and D-V (latch-first + `Filter` realigned)
across `channel/pubsub.go`, `routing/router.go`, `routing/filter.go`, then ran the bundle's own acceptance cases:

```
AC-5c nil-SECOND    …nil endpoint function: channel.NewPublishSubscribeChannel: nil option at index 1
                    | Is(ErrNilFunc)=true | Is(ErrShared)=false | Permanent=true
AC-5c nil-FIRST     …nil endpoint function: channel.NewPublishSubscribeChannel: nil option at index 0
                    | Is(ErrNilFunc)=true | Is(ErrShared)=false | Permanent=true
nil-FIRST SingleSubscriber()=true                      ← D-U's whole point
AC-5d Subscribe(nil) on latched  → …nil option at index 0        (NOT ErrNilHandler)
AC-5d NewRouter(nil,nil).Handle  → …routing.NewRouter: nil option at index 0   (NOT nil pick)
AC-5d Filter[string](nil,nil)    → …routing.Filter: nil option at index 0
Filter[string](nil) no opts      → …routing.Filter: nil pred     (UNCHANGED)
unlatched Subscribe(nil)         → msgin: nil message handler    (UNCHANGED)
```

And, with the patch applied:

```
$ GOWORK=off go test ./... -race -count=1
ok msgin  ok adapter/cron  ok adapter/database/sql  ok adapter/http  ok adapter/http/stdlib
ok adapter/memory  ok channel  ok endpoint  ok resilience  ok routing  ok transform
```

**Zero existing tests or Examples break.** The `Filter` realignment needs one extra import (`fmt`) and is otherwise
behavior-preserving: `FilterOption`'s only obtainable non-nil value is `WithDiscardChannel`, a single field write.

Every D-U attack came back negative:

| Attack | Result |
|---|---|
| An option harmful or order-dependent on a partially-latched object | **No.** The only whole-config-overwriting option is `withConfig` (`pubsub.go:105`, `*c = cfg`) — unexported, passed only as a sole option by `pubsub_registry.go:56`. Every other option writes one field |
| An option that could clear the latch | **No.** Latches sit on the struct (round-2 m-J); `memory.Option` is the only caller-constructible one reaching a struct and cannot touch an unexported field |
| "Latch only when unlatched" preserves first-nil-wins | **Yes**, structurally |
| Non-error methods now misreported | **No.** `SingleSubscriber()` is the only option-derived non-error observable on any R2 product |
| D-U × D-S | **Orthogonal** — D-S is the error value, D-U is which options apply |
| Race / goleak | Latch written once before publication; no R2 constructor starts a goroutine; `-race` green |
| Same before/after asymmetry elsewhere | **None in R2** — `Filter` was its only member with a pre-loop argument check. R1 keeps argument-first by §3.5, deliberately |

---

## 2. MAJOR findings

### M-W — the position's **constructor name** was asserted for only 9 of 32 constructors

D-R's whole cost — an eighth duplicated helper, a redundant pass in 8 delegators — is bought verbatim *"for a
truthful position at every entry point"*. The ACs never checked that truth outside the delegators: **AC-2** asserted
only that the message *contains* `index 1`, **AC-3** only `index 0`, and AC-6's position mutant was scoped to
delegators. Tasks 1–4 asserted no constructor name at all — roughly **20 constructors** unverified.

Why it bites: Task 1 is explicitly the template later tasks copy, and `msghttp`'s six constructors share one option
type. A copy-paste leaving `"endpoint.NewConsumer"` in `NewProducer`, or `"msghttp.NewConfig"` in `NewSSEParser`,
passes every AC and ships a position naming a function the caller never called — **the exact failure D-R exists to
prevent.**

**Folded as:** AC-2/AC-3 assert the **full** position string; a new AC-6 mutant row — *swap the `ctor` literal for a
sibling's* — which only a full-string assertion can kill.

### M-X — R2's first-nil-wins had no AC; D-U's own clause was unenforced

D-U states *"latch only when unlatched, which is what preserves first-nil-wins"*, but AC-3 was scoped `≥1 per R1
package`, and AC-5d asserts only *which* fault a Router reports, not its index. An implementation omitting the
`if x.err == nil` guard and latching the **last** nil passed AC-1, AC-2, AC-3, AC-5, AC-5b, AC-5c and AC-5d. AC-6
already carried the killing mutant; it simply had no R2 case to kill.

**Folded as:** AC-3 extended to *"≥1 per R1 package **and ≥1 per R2 type**"* — `NewRouter(nil, nil).Handle(…)` must
report `index 0`.

### M-Y — round-3's m-P reached the spec but not the plan; Task 7's table contradicted itself

An independent AST walk over all 8 modules reproduces the **spec's** corrected figures exactly:

```
kind  *ast.Ident 35 | *ast.SelectorExpr 11 | *ast.IndexExpr 2 | *ast.IndexListExpr 1   (= 49)
node  FuncDecl 44 | FuncLit 5      test rows: FuncDecl 12 + FuncLit 1 = 13
exported non-test FuncDecls: 32
```

Plan Task 7's table still carried the **pre-round-3** numbers (4 FuncLits; "13, 9 of them FuncDecls") and then
contradicted itself three bullets later by requiring 0 false positives across *"the 5 harness/exchange_test
literals"*. **The plan is the operative document for Task 7** — and BLOCKER-C was precisely *"a checker built to a
wrong tally is silently blind"*.

**Folded as:** the spec's corrected rows copied into Plan Task 7.

---

## 3. Round-3 fixes — verified

| Fix | Verdict |
|---|---|
| **Task 0 baseline (BLOCKER-D)** | **Correct, procedure proven.** Installed `apidiff`, cloned `main`, `apidiff -w` then `apidiff` → **empty output**. Re-diffed from a second path → still empty, so the committed baseline is **path-independent** despite embedding absolute paths. 97/9 against the 027 baseline re-confirmed |
| **§3.5 table, 9/8 (M-L)** | **Correct**, re-derived by *reading* all 17 constructors. Every classification right |
| **`sqlite.DSN` godoc (M-O)** | **Correct** — names msgin's defaults (WAL, 5 s) and all three `DSNOption`s accurately |
| **AC-5c both orderings** | **Correct and necessary** — the nil-first case is the *only* test in the bundle that distinguishes `continue` from `break` |
| **AC-5d** | **Correct**, all three cases executable (modulo m-Z1's instantiation) |
| **m-U, 8 delegators** | **Correct** — all eight verified calling their delegate as the **first statement** |
| **Census / D-T** | **Exact** — 32 / 24 / 22; `NewCircuitBreaker` 10 sites in 4 files, 2 inline-composed, 1 a published Example |

---

## 4. MINOR findings

| # | Finding | Folded as |
|---|---|---|
| **m-Z1** | **PROVEN:** the mandated cases for generic constructors **do not compile** — `cannot infer A` for `Filter(nil, nil)`, `cannot infer T` for `NewConsumer(nil, nil, nil)`; `NewGateway` can never infer | Explicit instantiation mandated in Spec AC-5d and Plan Tasks 1–2 |
| **m-Z2** | `apidiff` is **not installed** (`which apidiff` → not found) and Task 0 — *"do this first"* — had no install step | `go install golang.org/x/exp/cmd/apidiff@latest` + `PATH` export added as Task 0 step 0 |
| **m-Z3** | Two §3.5 line refs questioned. **Coordinator re-checked: only one was wrong.** `producer.go:339` → the check is at **337**. The auditor's second claim (`aggregator.go:299` → 300) is **incorrect** — `sed -n '299p'` is `if store == nil`, so 299 was already right | `producer.go` ref corrected; `aggregator.go` left as-is |
| **m-Z4** | §3.5's opening rule was **unscoped**, and D-V contradicts it by moving `Filter`'s loop. §3.5 also never argued why R1 keeps argument-first while R2 is option-first | §3.5 scoped to **R1 only**, with the reason stated: R1 preserves designed per-constructor precedence; R2's five products share one newly-introduced latch and get one uniform rule |
| **m-Z5** | D-U's footprint is observable on **exactly one** surface (`SingleSubscriber()`); elsewhere the latch dominates every method, so a `break` implementation would pass every AC | Stated in §3.2 as a coverage fact — blackbox cannot observe it elsewhere |
| **m-Z6** | D-U **widens** the deferred `WithBuffer` defect: under `break`, `memory.New(nil, WithBuffer(1<<62))` stopped at the nil; under `continue` it proceeds into `make(chan)` and panics | Owned in §3.7. Not a regression (it panicked without the nil before), but the headline contract says *no constructor panics on a nil option element*, and this combination does |
| **m-Z7** | Three caller-facing godocs enumerate a check order the new guard interposes into (`cron/sqllock.go:49`, `cron/sqlelector.go:84`, `sql/inbox_dedup.go:71`), leaving them literally true but incomplete | §4.3 now requires the new sentence to say **where** the option check sits in that order |
| **m-Z8** | Task 8 did not update CLAUDE.md's artifact counts — **already stale in the working tree**: it says *14 specs / 29 ADRs*; the tree has **15** and **30** | Task 8 bullet added: re-derive and bump to 28 plans / 30 ADRs / 15 specs |
| **m-Z9** | Task 0 was the only task with no commit message or Conventional-Commit type, while Global constraint 6 pre-authorizes *"the per-task commits enumerated in that plan"* | Named `chore(api): capture the Plan 028 apidiff baseline` |

---

## 5. Attacked, found sound

- **`msgin.Chain` is the only other variadic func-typed parameter in the workspace** (AST-derived, excluding
  `*Option` names) — and it is already guarded. **There is no residual class outside the 32.**
- **Task ordering holds.** All 10 D-T call sites are in the root module, so Task 2 stays a green unit despite
  touching three `endpoint` test files; `channel/*_test.go` is `package channel_test` and already imports
  `endpoint`, so AC-5c is placeable in Task 3 with no import cycle.
- **Docs-link gate, both arms, clean**; census commands reproduce 32 / 24 / 22 exactly as written.

## Coordinator's independent re-verification

m-Z8 (`ls docs/specs/[0-9]*.md | wc -l` → **15**, `ls docs/adrs/[0-9]*.md | wc -l` → **30**, vs CLAUDE.md's 14/29),
m-Z2 (`which apidiff` → not found) and m-Z3 (`sed -n '337,339p' endpoint/producer.go`, `sed -n '299,300p'
routing/aggregator.go`) were re-run before folding. **All confirmed except the `aggregator.go` half of m-Z3, which
the audit got wrong** — recorded above rather than silently applied. Four rounds in, the auditors have been right
about essentially everything; that is not a reason to stop checking.
