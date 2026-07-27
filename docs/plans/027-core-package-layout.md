# Plan 027 — Core package layout, channel segregation, and behavior types

> **For agentic workers:** REQUIRED SUB-SKILL: `superpowers:subagent-driven-development` (the project default) or
> `superpowers:executing-plans`. Steps use checkbox (`- [ ]`) syntax.
>
> **Go-skills hard rule (CLAUDE.md, restated because `writing-plans` omits it):** every task starts from
> **`cc-skills-golang:golang-how-to`** (here routing to: `golang-refactoring` — this is a at-scale
> behavior-preserving restructure — plus `golang-project-layout`, `golang-gopls`, `golang-naming`,
> `golang-structs-interfaces`, `golang-documentation`, `golang-testing`).
> **`superpowers:test-driven-development`** governs every task. **`gopls` is not optional here** — the moves are
> performed with `gopls` Rename/Move and find-references, never text search or hand-editing imports.
> Project-local **`table-test`** override (assert-closure form, `t.Context()`). `use-mockgen` does not apply;
> `use-testcontainers` applies only to Task 11's SQL conformance tests, via the existing
> `RunTestDatabase`/`dbtest` helpers — do not write new container plumbing.
>
> **This plan is deliberately thin:** signatures, names, invariants, branch coverage, commit boundaries — no
> embedded implementations. Write the code TDD-first from the tables below.

**Goal.** Ship [Spec 014](../specs/014-core-package-layout.md): restructure the flat core into EIP-chapter
packages with vocabulary and SPI in root (C-full), segregate `MessageChannel`, land the EIP renames and named
behavior types, move expression support to its own module, and add `MessageGroupStore.SettleMembers`.

**Architecture.** [ADR 0027](../adrs/0027-core-package-restructure.md) (layout, C-full, clean break),
[ADR 0028](../adrs/0028-channel-interface-segregation.md) (channel interfaces),
[ADR 0029](../adrs/0029-eip-lexical-alignment.md) (renames, behavior types, expr module).

**This increment is behavior-preserving by construction.** Every pre-existing test must pass unchanged except
where it names a signature that deliberately moved or narrowed. **No test's assertions change.** A task that
finds itself rewriting an assertion has either found a real defect (stop, report it) or is doing more than the
plan says (stop, re-read the task).

**Traceability.** Implements Spec 014; promoted from RFC-0001/0002/0003 (accepted 2026-07-27); amends ADR 0003
and ADR 0019. Branch: `refactor/core-package-layout`, split from a quiet `main` **before** Plan 028 (gin) or any
new adapter — blast radius grows with every adapter landed first.

**Tech stack.** Go 1.25 (`GOTOOLCHAIN=go1.25.12`), the root module plus a new `expr` module, and the six
existing satellite modules.

---

## Global constraints

1. **`go build ./...` after every single move**, not just at task end. An import cycle surfaces instantly; found
   late, it is expensive to unpick. Interfaces live in root by design, which is what makes the moves acyclic.
2. **Blackbox tests only** — every `_test.go` stays `package <pkg>_test` and drives the exported API. Moving a
   test must not tempt anyone into whitebox access to reach a now-unexported helper; if that happens, the
   symbol's placement is wrong, not the test.
3. **Exactly one package-doc file per package.** Each new package gets a `doc.go`; duplicate `// Package`
   comments after a merge are a `go vet` failure.
4. **Coverage is compared per package before and after.** A pure move that loses coverage means tests were
   dropped, not that the metric moved. Capture the baseline in Task 0.
5. **Seven modules, not one tree.** `./...` at root covers 6 packages only. The per-module `GOWORK=off` loop from
   CLAUDE.md is the gate, plus the new `expr` module from Task 10 onward.
6. **Nothing in root imports a subpackage.** This is the acyclicity invariant and the C-full check; assert it
   mechanically in Task 12, not by eye.

---

## Task 0 — Branch, baseline, and the move-list

**Goal.** Make the "only intended changes" gate checkable rather than aspirational.

- [ ] Branch `refactor/core-package-layout` from `main`.
- [ ] Capture the **apidiff baseline** of the pre-window root package to a scratch file.
- [ ] Capture **per-package `go test ./... -cover`** for all seven modules; record in the plan's progress ledger.
- [ ] Transcribe Spec 014 §3's file→package table into the ledger as the **enumerated move-list**. Every later
      task's diff is read against it; a symbol that moves without appearing here is a finding, not a detail.
- [ ] Record the 87 `msgin.*` symbols referenced by `adapter/` and which of them are expected to change path
      (per ADR 0027: the engine constructors, `ChannelExchange`, `ExponentialBackoff`, the concrete channels).

**Commit:** `docs(plan): baseline and move-list for the core restructure`

---

## Task 1 — Remove the `*Expr` constructors from core; drop `expr-lang`

**Goal.** Delete the six in-core expr twins **before** anything moves, so `expr.go` is never split across two new
packages only to be deleted (ADR 0029, sequencing consequence).

- [ ] Delete `FilterExpr`, `RouterExpr`, `TransformExpr`, `SplitExpr`, `WithCorrelationExpr`, `WithReleaseExpr`
      and their tests. Preserve the deleted tests' *cases* in the ledger — Task 10 reinstates them against the
      provider, and they are the regression evidence that the provider behaves identically.
- [ ] Delete `expr.go`; redistribute `doc_composition.go`'s prose (it is godoc only) toward each package's
      forthcoming `doc.go`, parking it in the ledger for now.
- [ ] `go mod tidy`; confirm `go list -deps .` no longer mentions `expr-lang` and the root `go.mod` no longer
      requires it.

**Note.** Expression support is **absent from the branch** between this task and Task 10. That is deliberate and
recorded in Spec 014 §7; it is not scope loss.

**Verify:** `go build ./...`; full `-race` suite green; `go list -deps . | grep expr-lang` empty.

**Commit:** `refactor(core)!: remove the in-core *Expr constructors and the expr-lang dependency`

---

## Task 2 — Segregate `MessageChannel` (the capability fix)

**Goal.** ADR 0028. This is the one task with genuinely new behavior, so it is TDD-first in the strict sense.

- [ ] **RED first:** write the capability test — a `QueueChannel` and a `PublishSubscribeChannel` each used as a
      discard target, a default route, and an exchange request channel. **It must fail to compile against the
      current interface.** A test that passes before the change is not testing the fix.
- [ ] Narrow `MessageChannel` to `Send` only; add `SubscribableChannel` embedding it with
      `Subscribe(h MessageHandler) (Subscription, error)`.
- [ ] Change `DirectChannel.Subscribe` to return `(Subscription, error)`.
- [ ] Narrow call sites: `WithDiscardChannel`, `WithDefaultChannel`, the router's `pick` return, and
      `NewChannelExchange`'s `request` → `MessageChannel`; `NewChannelExchange`'s `reply` → `SubscribableChannel`.
- [ ] Godoc: state that `SubscribableChannel` is an **in-process contract**, and name **Return Address** as the
      distributed answer (CLAUDE.md multi-instance rule; Spec 014 §10). Do not narrow the external
      `RequestReplyExchange` seam.

**Hot-path branches needing a case each:** `DirectChannel.Subscribe` second-subscriber rejection; the returned
`Subscription`'s unsubscribe path; unsubscribe called twice (idempotent, not a panic); `ChannelExchange` reply
delivery after unsubscribe.

**Verify:** the capability test goes RED→GREEN; every pre-existing channel/filter/router/exchange test passes
with **no assertion changed**.

**Commit:** `refactor(core)!: split MessageChannel into send-only and SubscribableChannel`

---

## Task 3 — EIP renames and godoc fixes

- [ ] `gopls` rename `StreamingSource` → `EventDrivenSource` across all seven modules. The `Stream` method keeps
      its name (ADR 0029 §1).
- [ ] Godoc: name the in-process request-reply pattern **Correlation Identifier**, with **Return Address** as the
      distributed seam; document `DirectChannel`'s deliberate single-subscriber restriction versus Spring's
      load-balanced multi-subscriber, noting competing consumers come from the worker pool.
- [ ] Add the `RequestReplyExchange` godoc line disclaiming AMQP's meaning of "exchange".

**Blocked on the audit:** ADR 0029 §2 asserts, from recall, that Spring names its equivalent
`RequestReplyExchanger`. **Verify that citation before writing the disclaiming godoc.** If it does not hold, stop
and escalate — the decision reverts to a rename and this task changes shape.

**Commit:** `refactor(core)!: rename StreamingSource to EventDrivenSource; EIP godoc alignment`

---

## Tasks 4–8 — The package extractions

Each task is identical in shape: create the package with a `doc.go`, move files with `gopls`, `go build ./...`
after each individual move, move the matching `_test.go` files **behavior-identical**, and re-check `-cover` for
the new package against the Task 0 baseline. Each is its own green commit.

| Task | Package | Files (per Spec 014 §3) | Size |
|---|---|---|---|
| 4 | `routing` | `filter.go`, `router.go`, `splitter.go`, `aggregator.go` | M |
| 5 | `transform` | `transformer.go` | S |
| 6 | `channel` | `DirectChannel` (from `channel.go`), `queuechannel.go`, `pubsub.go`, `pubsub_registry.go` | S |
| 7 | `resilience` | `backoff.go`, `breaker.go`, `ratelimit.go`, + the **interface half** of `flowcontrol.go` | M |
| 8 | `endpoint` | `activator.go`, `consumer.go`, `credit.go`, `exchange.go`, `gateway.go`, `poller.go`, `producer.go`, + the **option half** of `flowcontrol.go` | M |

**Order matters.** `endpoint` is extracted **last**: it is the most entangled, and with everything else already
out of root the cycle check is meaningful rather than incidental. `routing` is first because it is the most
self-contained.

**The two splits** (Task 6 and Tasks 7/8) are the sharp edges — `channel.go`'s interface stays in root while
`DirectChannel` leaves, and `flowcontrol.go` divides along the interface/option line. Do each split as an
explicit step with its own build check, not as part of a bulk move.

**Watch for:** `endpoint` → `channel` is expected (the Gateway builds its own reply channel) and is acyclic. Any
proposed edge **from root into a subpackage** is a design error — stop and escalate rather than working around it.

**Commits:** `refactor(core)!: extract the <name> package` ×5

---

## Task 9 — Named behavior types and combinators

- [ ] Declare the six types in their packages (Spec 014 §6) and type the base constructors.
- [ ] Add `Predicate.And`/`Or`/`Not`.
- [ ] Every type's godoc **names its Spring equivalent** — this is the mitigation that justifies dropping the
      Spring names (ADR 0029 §4), so it is verified per type, not sampled.
- [ ] Record the expected `apidiff` output as **reviewed, source-compatible** parameter-type changes. Do not
      claim zero output.

**Hot-path branches needing a case each:** `And` short-circuit on false; `And` error propagation from the left
and from the right predicate; `Or` short-circuit on true; `Or` error propagation from each side; `Not` inverting
a true and a false; `Not` propagating an error rather than inverting it. *(The error-vs-inversion case is the
one a naive `Not` gets wrong.)*

**Verify:** existing endpoint tests compile unchanged with bare closures — that is the source-compatibility claim,
so it needs to be demonstrated, not assumed.

**Commit:** `feat(routing,transform): name the endpoint behavior types and add combinators`

---

## Task 10 — The `expr` provider module

- [ ] New module `expr` with its own `go.mod` (Go 1.25); add to `go.work`; add a CI job matching the existing
      per-module pattern.
- [ ] Providers returning the Task 9 types: `Predicate`, `RouteFunc`, `SplitFunc`, `Transformer`, `Correlation`,
      `Release` — each `(T, error)`, compiling the expression once at construction.
- [ ] Reinstate the Task 1 test cases against the providers. Behavioral parity with the deleted `*Expr`
      constructors is the acceptance bar.
- [ ] Runtime failures wrap the **source expression text** into the error — this is the debuggability mitigation
      that ADR 0029 §3 traded the interface shape for, so it is a requirement, not a nicety.

**Hot-path branches needing a case each:** invalid expression → typed error at construction; valid expression,
wrong result type; runtime evaluation error carrying the expression text; nil/empty expression string.

**Verify:** ADR 0019's fail-at-construction contract holds — an invalid expression errors at the provider call,
never at first message.

**Commit:** `feat(expr): expression providers as a separate module`

---

## Task 11 — `MessageGroupStore.SettleMembers`

**Goal.** Spec 014 §8 — add the partial-settlement method during the breaking window so the Resequencer can land
later as additive work.

- [ ] Add to the interface: `SettleMembers(ctx, claim MessageGroupClaim, msgIDs []string) error`, fenced on
      `claim.Epoch` exactly like `SettleGroup`.
- [ ] Implement for `memory` and `sql`, plus the three dialect modules.
- [ ] Conformance tests in `harness`, run against every dialect via the existing `dbtest` runner.

**Hot-path branches needing a case each:** empty `msgIDs` (no-op, not an error); a subset settled while the rest
return to live; ids not in the claim (ignored, no error); **fence miss after a stolen lease → no-op**; a member
added *during* the lease surviving a `SettleMembers` that does not name it.

**Justify or drop.** This method has **no in-tree caller** until RFC-0005's Resequencer. That is a deliberate
exception to the small-surface rule, justified solely by breaking-window timing. If the audit rejects that
argument, this task moves out of the plan rather than being defended.

**Commit:** `feat(core)!: add MessageGroupStore.SettleMembers for partial release`

---

## Task 12 — Migration, doc sync, and the whole-branch gate

- [ ] `MIGRATION.md`: every moved symbol old→new, the `DirectChannel.Subscribe` signature change, the
      `*Expr`→`expr` module move, and the `EventDrivenSource` rename.
- [ ] **CLAUDE.md, same commit** (traceability, non-optional): dependency policy drops `expr-lang` and **keeps
      `robfig`**; the architecture blueprint's `StreamingSource` becomes `EventDrivenSource`; the package layout
      and commands reflect the new tree and the eighth module.
- [ ] `MESSAGING.md` reconciled against the new package names.
- [ ] **Assert the C-full invariant mechanically:** no root file declares a constructor for a running component,
      and no root file imports a subpackage. A script in the ledger, not an eyeball.
- [ ] Run the full per-module `GOWORK=off` loop, `go vet`, `golangci-lint`, `gofmt`, `govulncheck`,
      `go mod tidy` (no-op), and `CGO_ENABLED=0 go build ./...`.
- [ ] `apidiff`/`gorelease` against the Task 0 baseline; reconcile **every** entry against the move-list. An
      unexplained entry blocks the merge.
- [ ] Whole-branch `/code-review` and `/security-review` over `main..HEAD`; resolve or triage every finding.

**Commit:** `docs: migration guide and doc sync for the core restructure`

---

## Risks

| Risk | Mitigation |
|---|---|
| Import cycle mid-move | `go build` after every individual move; interfaces in root by design; `endpoint` extracted last |
| Coverage silently drops when relocating tests | Task 0 baseline compared per package at every extraction |
| A "pure move" quietly changes behavior | No assertion may change; a task that needs to has found a defect — stop and report |
| `apidiff` noise hides a real break | Read against Task 0's enumerated move-list; Task 9's parameter-type entries pre-recorded as expected |
| `RequestReplyExchanger` citation is wrong | Task 3 verifies before writing the godoc; if false, ADR 0029 §2 reverts to a rename |
| `SettleMembers` ships with no caller | Explicitly flagged for the audit to reject (Task 11) |
| Branch conflicts with in-flight feature work | Split from a quiet `main`; run this window **before** Plan 028 (gin) or any new adapter |
| Expression support absent mid-branch | Bounded to Tasks 1→10 within one branch; Task 1 preserves the test cases that Task 10 must satisfy |
