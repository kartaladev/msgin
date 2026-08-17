# Plan 028 — Nil option elements must not panic

> **For agentic workers:** REQUIRED SUB-SKILL: `superpowers:subagent-driven-development` (the project default) or
> `superpowers:executing-plans`. Steps use checkbox (`- [ ]`) syntax.
>
> **Go-skills hard rule (CLAUDE.md, restated because `writing-plans` omits it):** every task starts from
> **`cc-skills-golang:golang-how-to`** (here routing to: `golang-error-handling` — the increment is one error
> contract — `golang-safety` (nil-safety), `golang-design-patterns` (functional options), `golang-testing`,
> `golang-documentation`). **`superpowers:test-driven-development`** governs every task: red → green → refactor.
> **`gopls`** (via the `LSP` tool) for navigation and refactoring — not `grep` — when reasoning about Go symbols.
> Project-local **`table-test`** override applies to every test (assert-closure form, never `want`/`wantErr`;
> `t.Context()`). **`use-mockgen` / `use-testcontainers` do not apply** — every test here is a constructor call with
> no external resource.
>
> **This plan is deliberately thin** (Plan 024/026 precedent): signatures, positions, branch coverage and commit
> boundaries — no embedded implementations. Write the code TDD-first from the tables below.

**Revision 5.** Round 4 ([`028-audit-round-4.md`](028-audit-round-4.md)): SAFE TO IMPLEMENT — 3 MAJORs, 9 MINORs,
no blocker; D-U and D-V were **compile-proven** and the whole suite stayed green under them. Round 3 ([`028-audit-round-3.md`](028-audit-round-3.md)): 1 BLOCKER (AC-8 gated on a baseline that is
red on arrival → new **Task 0**), 4 MAJORs, 7 MINORs; decisions **D-U**/**D-V** settled with the user. Round 1 ([`028-audit-round-1.md`](028-audit-round-1.md)): census **30 → 32**, a new `stdlib` task,
the skip family cut from 7 to 2, Task 7's fallback replaced. Round 2
([`028-audit-round-2.md`](028-audit-round-2.md)): 3 BLOCKERs, 3 MAJORs, 5 MINORs — **all in the verification layer**;
the mechanism, the census and D-S were re-attacked and held. Narrow round 4 pending.

**Goal.** Deliver [Spec 015](../specs/015-nil-option-elements.md): all **32** exported functional-option constructors
stop panicking on a nil option element, and instead reject, degrade, or skip-and-document it.

**Architecture.** [ADR 0031](../adrs/0031-nil-option-elements.md) — **D-P** (report through the constructor's return,
else the product's first use, else skip), **D-Q** (reuse `msgin.ErrNilFunc`), **D-R** (all 32 guard their own slice;
`nilOptionAt` duplicated across 8 packages), **D-S** (the latched fault is `Permanent`-wrapped), **D-T**
(`NewCircuitBreaker` gains an error return).

**Tech stack.** Go 1.25 (`GOTOOLCHAIN=go1.25.12`). Touches **two modules** — root and
`adapter/database/sql/sqlite` — but the delivery gate is all **eight**.

**Traceability.** Implements Spec 015; decided by ADR 0031. Every commit carries `Spec: 015`, `Plan: 028`,
`ADR: 0031` trailers. Branch: `feat/nil-option-elements`, off `main`.

---

## Global constraints

1. **Blackbox tests only** — `package <pkg>_test`, exercising the exported API. No whitebox fallback.
2. **Assert-closure tables** — every case carries `assert func(t *testing.T, …)`.
3. **One signature change only** — D-T's `NewCircuitBreaker`. Any *other* task that appears to need one has hit a
   design fault: **stop and escalate**, do not proceed.
4. **R1 errors are BARE; R2 errors are `Permanent`-wrapped.** Never wrap R1 — `nilFuncAt`'s godoc in
   `endpoint/helpers.go` and `routing/helpers.go` warns against exactly this "finish the job" instinct. AC-5 asserts
   both directions, so a reflex in either direction fails the suite.
5. **Mutation-prove every assertion (AC-6) with a mutant that targets THAT assertion.** Guard-reversion makes the
   constructor **panic**, so it turns every case red regardless of what it asserts — it is permitted for **AC-1
   alone**. Use Spec AC-6's mutant table for the rest (hardcode the index to `0`; report the last nil; flip the wrap
   in **both** directions; remove one method's latch check; delete a delegator pre-check and require the case to fail
   on the **position string**). Record the killed mutant per case in the task's Evidence block. A case that survives
   its own mutant is rewritten.
6. **Per-task commit is pre-authorized** by CLAUDE.md's plan-execution exception once this plan is approved and an
   execution mode is chosen. `git push`, merge and branch deletion still need explicit per-action approval.
7. **Each task is a green unit** — `GOWORK=off go test ./... -race -shuffle=on` passes in every module it touched
   before its commit.

## The position format (one shape, used 32 times)

```
"<pkg>.<Ctor>: nil option at index <i>"
```

0-based, first-nil-wins, mirroring `handler.go:59`. `<pkg>` is the **import name a caller writes** — `msgin`,
`endpoint`, `routing`, `resilience`, `channel`, `memory`, `msghttp`, `stdlib`, `cron`, `sql`, `sqlite` — not the
directory.

## The helper (R1 packages only, duplicated eight times — D-R)

```go
// nilOptionAt reports a nil ELEMENT of a constructor's variadic option slice,
// naming the constructor the CALLER invoked and the element's 0-based index.
//
// Deliberately NOT wrapped in msgin.Permanent — see [msgin.ErrNilFunc]'s
// constructor arm, and the same warning on nilFuncAt.
func nilOptionAt(ctor string, i int) error {
	return fmt.Errorf("%w: %s: nil option at index %d", msgin.ErrNilFunc, ctor, i)
}
```

Packages: `endpoint`, `routing`, `resilience`, `adapter/memory`, `adapter/http`, **`adapter/http/stdlib`**,
`adapter/cron`, `adapter/database/sql`. `stdlib` needs its own — it cannot reach `msghttp`'s unexported helper.
Packages with no R1 constructor (`msgin` root, `channel`, `sqlite`) get **no helper**.

## The four code shapes

```go
// R1 — folded into the existing apply loop
for i, opt := range opts {
	if opt == nil {
		return nil, nilOptionAt("endpoint.NewConsumer", i)
	}
	opt(&cfg)
}

// R1 delegator — standalone pre-check, then forward unchanged
for i, opt := range opts {
	if opt == nil {
		return nil, nilOptionAt("stdlib.NewInbound", i)
	}
}
cfg, err := msghttp.NewConfig(opts...)

// R2 — latch the FIRST nil, CONTINUE applying the rest (D-U)
for i, opt := range opts {
	if opt == nil {
		if r.err == nil { // first-nil-wins
			r.err = fmt.Errorf("%w: %s: nil option at index %d",
				msgin.Permanent(msgin.ErrNilFunc), "routing.NewRouter", i)
		}
		continue
	}
	opt(&cfg)
}
// …and at the TOP of Handle / Send / Subscribe / Publish / Stream —
// BEFORE that method's own argument checks (D-V):
if r.err != nil { return r.err }

// R3 — skip
for _, opt := range opts {
	if opt == nil {
		continue
	}
	opt(&cfg)
}
```

`routing.Filter` (R2) **returns** `nilFuncStep(fmt.Sprintf("routing.Filter: nil option at index %d", i))` from inside
its loop — and per **D-V** that loop moves **above** the existing `pred == nil` check, so `Filter(nil, nil)` reports
the nil option, matching its existing nil-`pred` degradation rather than latching a field — so it discards the applied
prefix entirely. That is a **fifth** shape, unobservable (a `Step` has no config surface) but real: Task 7's checker
must accept **any `return` inside the loop** as guarded, not only the R1 form (round-3 m-R).

## Branch coverage — the hot-path enumeration (CLAUDE.md test-coverage gate)

Each constructor introduces **one** new `if` with **two** arms; both are hot-path (a typed-error branch, and the
construction path itself), so both need a covering case.

| Arm / property | Covering case | Applies to |
|---|---|---|
| element **is** nil | `(nil)` alone → family outcome asserted | all **32** |
| element is **not** nil | any pre-existing constructor test | all 32 (already covered) |
| index is **computed** AND the ctor name is right | `(realOpt, nil)` → asserts the **full** position string, not an `index 1` substring (AC-2, round-4 M-W) | **24 R1 + 5 R2** — *not R3 (no message), not `endpoint.NewGateway` (no obtainable non-nil option — Spec AC-2)* |
| **first**-nil-wins | `(nil, nil)` → asserts the full position ending `index 0` (AC-3) | ≥1 per R1 package **and ≥1 per R2 type** — D-U's "latch only when unlatched" has no other enforcement (round-4 M-X) |
| survivors still apply | `(realOpt, nil)` **and** `(nil, realOpt)` as two cases (AC-4) | the **2 R3** |
| wrap is right | `IsPermanent == false` (R1) / `== true` (R2) (AC-5) | 25 R1 + 5 R2 |
| every reporting surface reports | each latched method returns the fault; non-error methods **not** forced (AC-5b) | the 5 R2 |
| the fault is not laundered | `NewChannelExchange` over a latched reply channel yields `ErrNilFunc`, **not** `ErrSharedReplyChannel` (AC-5c) | `channel` + `endpoint` |
| precedence | assert Spec §3.5's `yields` column for **one constructor per distinct order** — **9** validate-first / **8** loop-first / **8** delegator; not one per package (`adapter/database/sql` alone has all three). *Loop-first constructors have no earlier-validated fault by definition, so their case pairs a nil option with a **later**-validated one (e.g. `sql.NewOutboundAdapter(nil, "t", d, nil)` → nil-option error, since `db` is checked after the loop).* | one per order |

**Coverage caution (project scar):** a package split re-attributes blackbox coverage to the package the test lives in.
These tests sit beside their constructors, so no re-attribution is expected — verify with `-coverpkg=./...`, not the
per-package default, before claiming a number.

---

## Task 0 — capture a usable `apidiff` baseline (**do this first; nothing else can pass without it**)

Revisions 2–3 gated AC-8 on `027-root-api-baseline.txt`, which is the **pre-Plan-027** surface and reports
**97 removals / 9 additions** against the current tree before this increment touches a line — `docs/HANDOVER.md` §3
records that exact number. Task 1's checklist was therefore unsatisfiable on arrival (round-3 audit, BLOCKER-D).

- [ ] **`apidiff` is not installed on this machine** (`which apidiff` → not found): `go install golang.org/x/exp/cmd/apidiff@latest`, and `export PATH="$(go env GOPATH)/bin:$PATH"`.
- [ ] From a clean `main`: `apidiff -w docs/plans/028-root-api-baseline.txt .`
- [ ] Prove it is usable: `apidiff docs/plans/028-root-api-baseline.txt .` → **empty output** (0/0) on the untouched
      tree. A baseline that does not diff clean against its own source is not a baseline.
- [ ] **Commit it.** No gate may depend on `/tmp` (Plan 027's rule).
- [ ] Leave `027-root-api-baseline.txt` untouched — it is Plan 027's historical record, not this increment's gate.
- [ ] Commit `chore(api): capture the Plan 028 apidiff baseline`.

## Task 1 — `endpoint` (4 × R1) + the `ErrNilFunc` godoc contract

**Reference task. Do it first; later tasks copy its shape.**

- [ ] Read Spec 015 §3.1/§3.4/§3.5 and ADR 0031 D-P/D-Q/D-R.
- [ ] **Instantiate the generics explicitly** — `NewConsumer[string](nil, nil, nil)`, `NewGateway[string, string](nil)`.
      Bare `NewConsumer(nil, nil, nil)` does **not** compile (`cannot infer T`); `NewGateway` can never infer.
- [ ] **Red:** table cases for `NewConsumer`, `NewProducer`, `NewGateway`, `NewChannelExchange` — nil case, index-1
      case, one `(nil, nil)` first-wins case, one precedence case (`NewConsumer(nil, nil, nil)` must yield
      `ErrNilAdapter` — all four are **validate-first**, Spec §3.5), and `IsPermanent == false` on all four.
- [ ] **`NewGateway` is AC-2-exempt** — `GatewayOption[Req, Rep]` has zero exported constructors and
      `gatewayConfig` is unexported and empty, so a blackbox test cannot build a non-nil one (Spec AC-2 carries the
      compile error). Write AC-1/AC-3/AC-5 for it and **do not** attempt AC-2, and **do not** reach for a whitebox
      test to make it possible.
- [ ] **Green:** `nilOptionAt` in `endpoint/helpers.go`, beside `nilFuncAt` and sharing its godoc's reasoning; fold
      the guard into all four apply loops.
- [ ] Amend `msgin.ErrNilFunc`'s godoc (`errors.go:207-230`) per Spec §4.1. **Do not touch the governing invariant
      paragraph** (`errors.go:212-221`) — D-S's extension lives in ADR 0031, not here.
- [ ] Per-constructor godoc sentence on all four (Spec §4.3).
- [ ] Mutation-prove each case; verify root green; `apidiff` vs **`028-root-api-baseline.txt`** (Task 0) → 0/0.
      **Not** the `027` baseline — that one is red on arrival (round-3 BLOCKER-D).
- [ ] Commit `fix(endpoint,core): reject a nil option element instead of panicking` — carries ADR 0031 + this plan.

## Task 2 — `routing` (R1 + 2 × R2) + `resilience` (R1 + D-T)

The task that exercises **all three families and the one signature change**, so it is where the design is really
tested.

| Constructor | Family | Outcome |
|---|---|---|
| `routing.NewAggregator` | R1 | bare `ErrNilFunc`, `IsPermanent == false` |
| `routing.Filter` | R2 (Step) | `Permanent(ErrNilFunc)` at dispatch |
| `routing.NewRouter` | R2 (latched) | `Handle` returns `Permanent(ErrNilFunc)`, **before** its `pick == nil` check (D-V) |
| `resilience.NewTokenBucket` | R1 | bare `ErrNilFunc` |
| `resilience.NewCircuitBreaker` | R1 **via D-T** | signature → `(msgin.CircuitBreaker, error)` |

- [ ] **Red** first. `Filter` needs a *dispatch* assertion (build the Step, run a message, assert). `NewRouter` needs
      a *Handle* assertion. Both assert `IsPermanent == true`.
- [ ] **Green:** `nilOptionAt` in `routing/helpers.go` and in `resilience`; `Filter` reuses `routing.nilFuncStep`;
      `Router` latches an `err` field checked at the top of `Handle`.
- [ ] **D-V realignment:** move `Filter`'s option loop **above** its `pred == nil` check (`routing/filter.go:31`), so
      `Filter[string](nil, nil)` (instantiate — bare `Filter(nil, nil)` fails with `cannot infer A`) reports `routing.Filter: nil option at index 0` rather than `nil pred`. **AC-5d's `Filter`
      case fails against today's code** — that failure is the point; it is what proves the realignment happened.
      `Filter(nil)` with no options still reports `nil pred`, unchanged.
- [ ] **D-T:** change `NewCircuitBreaker`'s signature and update its **10** call sites (re-derive with `gopls`
      references; do not trust this number). They span **four** files, three of which are outside this task's stated
      scope — name them so the change is not a surprise under Global constraint 3: `resilience/breaker_test.go` (5),
      `endpoint/consumer_test.go` (3), `endpoint/consumer_probegate_wiring_test.go` (1),
      `endpoint/example_flowcontrol_test.go` (1). The last is a **published godoc Example** and will gain error
      handling — keep it idiomatic, it is documentation.
- [ ] Godoc on all five (R2's names the permanence and the reporting method).
- [ ] Mutation-prove; verify; commit `fix(routing,resilience)!: handle a nil option element`.

## Task 3 — root `msgin.New` + `channel` (1 × R3 + 2 × R2)

- [ ] **Red:** `msgin.New` — the AC-4 **pair**: `(WithID("x"), nil)` asserts the id landed; `(nil, WithID("x"))`
      asserts it landed too. `PublishSubscribeChannel` — latched fault from **`Send` and `Subscribe`**. `PubSub` —
      from **`Publish` and `Subscribe`**. **`PubSub` has no `SingleSubscriber` method**; do not assert one.
- [ ] **AC-5c — BOTH orderings.** `NewChannelExchange` over `NewPublishSubscribeChannel(WithSingleSubscriber(), nil)`
      **and** over `NewPublishSubscribeChannel(nil, WithSingleSubscriber())` must each return an error matching
      `msgin.ErrNilFunc`, **not** `msgin.ErrSharedReplyChannel`. The **nil-first** case is the executable proof of
      **D-U** — it fails under a `break` loop and passes under `continue`. Write it even though the first looks
      sufficient.
- [ ] **AC-5d:** `latched.Subscribe(nil)` reports the nil **option**, not `msgin.ErrNilHandler` (D-V).
- [ ] **Green:** `continue` in `msgin.New`; latched `err` in both channel types. **No helper in either package** —
      write the 3-line `Permanent(ErrNilFunc)` expression inline twice; that is intended, not an oversight.
- [ ] **Latch onto the STRUCT, never into `pubSubConfig`.** `withConfig` does `*c = cfg` (`channel/pubsub.go:104`)
      and `PubSub.Subscribe` builds each topic channel with `NewPublishSubscribeChannel(withConfig(p.cfg))`
      (`pubsub_registry.go:60`) — a latch inside the config would propagate the registry's fault into **every** topic
      channel.
- [ ] Godoc: Spec §3.3's **verbatim** `msgin.New` sentence — it must name the random-id consequence, not merely say
      "ignored".
- [ ] Mutation-prove; verify; commit `fix(core,channel): handle a nil option element`.

## Task 4 — `adapter/memory` (1 × R2 + 2 × R1) + `adapter/cron` (3 × R1)

`memory.New` is **R2** (latched; `Broker.Send`/`Stream` report it). `memory.NewGroupStore`, `.NewQueueStore`,
`cron.NewSource`, `.NewSQLElector`, `.NewSQLLocker` are R1.

- [ ] Red → green per the shapes; `nilOptionAt` in both packages. Both already import root (`memory` returns
      `msgin.ErrInvalidCapacity` at `queuestore.go:91`) — **confirm with `gopls`, do not assume**.
- [ ] **`memory.New` is R2, and its AC-5b mutant HANGS rather than fails** — without the latch, `Broker.Send` blocks
      on the unbuffered channel and `Stream` blocks on `ctx.Done()`, so the mutant run dies at the 10-minute test
      timeout instead of reporting. Drive both cases with a short deadline —
      `ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)` — so the mutant fails fast and
      unambiguously (round-3 m-T).
- [ ] Godoc; mutation-prove; verify; commit `fix(memory,cron): handle a nil option element`.
- [ ] **Do not** run `crontest` here (Docker, ~50 s) — it has no option constructors. Task 8.

## Task 5 — `adapter/database/sql` (5 × R1) + `sqlite.DSN` (R3, second module)

- [ ] `NewOutboundAdapter`, `NewPollingSource`, `NewGroupStore`, `NewInboxDeduper` — R1, folded guard.
- [ ] `NewQueueStore` — **the first delegator**: pre-check naming `sql.NewQueueStore`, then forward to **both**
      `NewOutboundAdapter` and `NewPollingSource` unchanged. Assert the position names `NewQueueStore`, **not** the
      delegate — that assertion is the whole point of D-R and must die if the pre-check is removed.
- [ ] `sqlite.DSN` — R3, skip, Spec §3.3's verbatim godoc sentence. **Separate module**: run its suite standalone
      with `GOWORK=off`.
- [ ] Mutation-prove; verify both modules; commit `fix(sql,sqlite): handle a nil option element`.
- [ ] **Module-greenness note (audit m-13):** this task changes `adapter/database/sql` while `dbtest`, `postgres` and
      `mysql` (which consume it) run only in Task 8. Global constraint 7 is read as "every module whose *sources*
      changed". The risk is low — the change is construction-only and adds no new failure mode to an existing valid
      call — but it is a **narrow** reading, stated rather than assumed. Docker-backed, ~110 s.

## Task 6 — `adapter/http` (6 × R1: `NewConfig` + 5 delegators)

- [ ] **Red:** for each of the five delegators, assert the position names the **delegator**
      (`msghttp.NewOutbound`), and for `NewConfig` called directly, `msghttp.NewConfig`. Six distinct positions from
      one option type.
- [ ] **Green:** `nilOptionAt` in `msghttp`; one folded guard in `NewConfig` + five pre-checks.
- [ ] **Carried from the round-1 audit (suspected, unproven):** `msghttp.Option` is `func(*Config)` over an
      **exported** `Config` with ~700 lines of options (`options.go:407-1106`). Sweep them for any option whose effect
      escapes the `*Config` it receives. If one exists, Spec §3.1's "no partial configuration is observable" fails
      for these six — **report it, do not paper over it**.
- [ ] Godoc; mutation-prove; verify; commit `fix(http): handle a nil option element at every entry point`.

## Task 6b — `adapter/http/stdlib` (2 × R1 delegators) — **the round-1 BLOCKER-1 task**

`stdlib.NewInbound` and `stdlib.NewInboundGateway` (`adapter/http/stdlib/inbound.go:48,96`) take
`opts ...msghttp.Option` and forward to `msghttp.NewConfig`. Revision 1 of this plan had **no task covering them**.

- [ ] **Red:** both, asserting the position names `stdlib.NewInbound` / `stdlib.NewInboundGateway` — **not**
      `msghttp.NewConfig`. Without a pre-check these tests fail, which is exactly D-R's point.
- [ ] **Green:** `nilOptionAt` in `stdlib` (its own copy — it cannot reach `msghttp`'s) + two pre-checks.
- [ ] Godoc; mutation-prove; verify; commit `fix(http/stdlib): handle a nil option element`.

## Task 7 — the class gate (AC-7), with an honest fallback

**The task with real implementation risk. The fallback is pre-approved — take it rather than shipping a gate that
cannot fail.**

- [ ] Write a blackbox test in root that parses every `.go` file in the repo (root *is* the repo root, so a walk from
      `.` reaches all eight modules), finds every function whose signature has a variadic `…Option` parameter, and
      fails when the body lacks a nil-element guard over it.
- [ ] **Handle all six hazards (Spec AC-7)** — none is optional. The node-kind tally is measured over all eight
      modules:

      | # | Hazard | Instances today |
      |---|---|---|
      | 1 | `*ast.Ident` — bare types | 35 |
      | 2 | `*ast.SelectorExpr` — qualified (`msghttp.Option`) | 11 — incl. `stdlib/inbound.go:48,96`, the BLOCKER-1 miss |
      | 3 | **`*ast.IndexExpr`** — `ConsumerOption[T]`, `ProducerOption[T]` | 2 |
      | 4 | **`*ast.IndexListExpr`** — `GatewayOption[Req, Rep]` | 1 |
      | 5 | skip `*ast.FuncLit` | **5** — `harness/{lock:45,queuestore:28,groupstore:54,inbox:33}` **plus `adapter/http/exchange_test.go:590`** |
      | 6 | skip `_test.go` | **13 — 12 `FuncDecl` + 1 `FuncLit`**, so "skip `FuncLit`" covers only one — `endpoint/exchange_test.go:39,54,1158`, `adapter/http/exchange_test.go:69,590`, 8 × `RunTestX` in `crontest`/`dbtest` |

      **Hazards 3–4 carry `NewConsumer`, `NewProducer` and `NewGateway`** — a checker handling only kinds 1–2 skips
      the increment's flagship constructors in silence. Func-ness is undecidable from `go/parser` alone: load
      `go/types` per module, **or** match by name suffix and **document that as the limitation**.
      *(`docs/plans/027-tools`'s `//go:build ignore` files were listed in revision 2 with instances; re-derivation
      found **0**. Future risk, not a required case.)*
- [ ] **Accept all four guard shapes**, or the gate flags the constructors this increment just fixed: R1-folded,
      R1-pre-check (a standalone loop with no `opt(&cfg)`), R2-latch (`{ x.err = …; break }`), R3-skip
      (`{ continue }`).
- [ ] **Vacuity-probe it (mandatory): plant TWO unguarded constructors — one bare-typed, one GENERIC — and require
      BOTH RED**; remove them → **GREEN**. A probe that plants only a non-generic constructor certifies a gate blind
      to hazards 3–4. Record all observations verbatim.
- [ ] Confirm **0 false positives** across all 32 real constructors, the **5** `harness`/`exchange_test` literals, and
      the 13 `_test.go` variadics.
- [ ] **Cross-check against the pre-branch tree (round-3 m-Q).** The probe above is author-written — the same agent
      writes the checker and the planted constructors, so shape bias is uncontrolled. Run the finished checker against
      `main` in a throwaway worktree (`git worktree add /tmp/… main`) and require it to flag **all 32** real
      constructors there, then **0** on `HEAD`. One command; it catches a checker that only recognises its own
      planted shapes.
- [ ] **Fallback, pre-authorized:** if the checker cannot separate guarded from unguarded without false positives
      within the task's budget, delete it and ship a **hand-enumerated table per package that calls every constructor
      with `(nil)` and asserts non-panic**. Weaker than the AST gate, but it tests the **invariant**. Record the
      decision and reason here and in Spec AC-7. *(Revision 1's fallback — asserting the census is still 30 — is
      withdrawn: it gated a count, not a guard, would pass green against a tree with every guard deleted, and
      enshrined a number the audit proved wrong.)*
- [ ] **Do not ship a checker you had to weaken until it always passes.**
- [ ] Commit `test(core): gate the nil-option-element class`.

## Task 8 — whole-branch delivery gate

- [ ] **`/simplify`** over the branch diff first (CLAUDE.md §4 — a 32-site change across 10 packages qualifies as a
      big feature; audit m-14).
- [ ] All **eight** modules: `GOWORK=off go test ./... -race -shuffle=on` (`harness` via `go vet` — it has no tests
      and `go test` reports a false pass). Docker up for `dbtest` and `crontest`.
- [ ] Per touched module, the other six CI steps: `go build`, `go vet`, `gofmt -l .`, `CGO_ENABLED=0 go build`,
      `go mod tidy` + `git diff --exit-code`, `govulncheck`, `golangci-lint`.
- [ ] Workspace coherence loop (8 directories, `GOWORK` unset).
- [ ] `apidiff` vs **`028-root-api-baseline.txt`** (Task 0) → **0 removals / 0 additions**. Note it covers the
      **root package only**, so D-T's deliberate `resilience` break is invisible to it and is verified by the 10
      updated call sites compiling. Re-derive root's exported-symbol and sentinel counts; reconcile sentinels
      **by name, never by count** (the `43 ≠ 43` trap).
- [ ] Coverage with `-coverpkg=./...`; no regression against Plan 027's 93.7%.
- [ ] Docs-link gate, both arms, repo-wide. Expect exactly the two known false positives (`docs/plans/m`,
      `docs/specs/factory(fireTime`); anything else is real.
- [ ] `/code-review` and `/security-review` over **`main..HEAD`**, not the last commit. Fix or explicitly triage every
      finding; re-run the affected review and the `-race` suite.
- [ ] **Update CLAUDE.md's artifact counts** — they are **already stale in the working tree**: it says *14 specs /
      29 ADRs*, while `ls docs/specs/[0-9]*.md | wc -l` → **15** and `ls docs/adrs/[0-9]*.md | wc -l` → **30**.
      Re-derive and bump to 28 plans / 30 ADRs / 15 specs; CLAUDE.md mandates keeping itself in sync (round-4 m-Z8).
- [ ] Update `docs/HANDOVER.md`: Spec 015 delivered, the nil-option item struck from §6, `memory.WithBuffer`'s
      overflow panic **added** to §6 as the deferred sibling, and the `gin` increment noted as needing a plan number
      after 028.
- [ ] Ask for approval to merge, push, and delete the branch. **Not pre-authorized.**

---

## Evidence blocks

Each task appends its Evidence block here on completion: the commit SHA, the verbatim verification commands with
output, and the killed mutant per new assertion. **Verify the commit, not the worktree** — check out the SHA and
re-run; `git checkout <sha> -- file` silently stages and has hidden a three-file revert on this project before.

*(empty until execution begins)*

## Risks

| Risk | Mitigation |
|---|---|
| Another constructor is invisible to the census, as `stdlib` was | Spec §2's command now matches qualified types and anchors on `^func [A-Z]`; Task 7's gate catches the class independently of any count |
| An R1 guard gets `Permanent`-wrapped, or an R2 one left bare | Global constraint 4; AC-5 asserts both directions, so either reflex fails the suite |
| D-S is wrong and the wrap harms a direct caller | Flagged in Spec §3.2 as the bundle's most contestable point; round-2 audit target |
| A delegator's position silently names the delegate | Tasks 5/6/6b assert the delegator's own name; the assertion dies if the pre-check is removed |
| An `msghttp` option's effect escapes its `*Config` | Task 6 sweeps for it explicitly and reports rather than papers over |
| The two R3 skips stay invisible | AC-4 proves survivors apply; the godoc must **name the consequence** — that wording is a review item, not boilerplate |
| Coverage misread after new test files land | `-coverpkg=./...`; compare against 93.7% |
