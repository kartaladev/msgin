# ADR 0029 — EIP lexical alignment, named behavior types, and the expr provider module

- **Status:** **ACCEPTED — REGENERATED FROM A GREEN TREE (2026-07-28); round-3 audit returned
  `NEEDS-REVISION` 3/3 and its findings are folded in below.** §1 and §3 are implemented (commit `b6ce7bb`
  for the rename); §5's `expr` module is Plan 027 Task 10 and not yet built.
  > *Round-3 corrections in this ADR:* §5a's provider signatures **did not compile** —
  > `Correlation`/`Release`/`RouteFunc` must be `[A any]`, because `compile[A]` type-checks `payload.Field`
  > and `PayloadOf[A]` **is** the M-6 `ErrPayloadType` branch §3b hands to Task 10; and §2's AMQP-disclaimer
  > line is written as though it exists — `grep -rn -i 'amqp' --include='*.go' .` returns nothing, so it is an
  > outstanding Spec 014 §8 obligation, now owned by Plan 027 Task 11b. Evidence: F13.
  > *Round-2 banner cleared 2026-07-28.* Every defect it named is fixed **in this ADR**:
  > §3's *"fixes all three"* is **withdrawn** — `cfg.optErr` is deleted instead (**decision D-D**, §3a), and
  > the three surviving branches are the `Handle`-side ones, restored by pulling **D-E** forward into Task 1
  > (§3b); `WithReleaseStrategy` now **takes** the named `ReleaseStrategy` (**decision D-E**, §3), shipped at
  > `routing/aggregator.go:82` with `WithReleaseWhen` as the sugar; and §5's provider shape is **no longer
  > claimed to be uniform** — `RouteFunc` takes a second parameter (§5a). Evidence:
  > [`027-derivation-findings.md`](../plans/027-derivation-findings.md) F3, F5, F10.4.
  *(Rounds 1 and 2 both returned `NEEDS-REVISION` from all three auditors; revised in place both times rather
  than superseded because this ADR had never been implemented. Folded in round 1: §3's `ReleaseStrategy` →
  `(bool, error)` (§H5), §2's citation **VERIFIED** (G), §1's rename scope corrected (F7). See
  **[audit round 1](../plans/027-audit-round-1.md)** §K.)*
- **Decisions folded in 2026-07-28:** **D-D** (delete `cfg.optErr` and its `NewAggregator` guard — §3a) and
  **D-E** (`WithReleaseStrategy(ReleaseStrategy)` + `WithReleaseWhen` — §3), both settled 2026-07-27
  ([audit round 2 §G.1](../plans/027-audit-round-2.md)); **D-I** (the two expr sentinels leave root — §5.0a)
  and **D-K**, both settled 2026-07-28.
- **Decisions folded in 2026-07-28 (round 6):** **D-K is REVISED** — the `expr` providers wrap
  **`msgin.ErrPayloadType`** and `expr.ErrExprResultType` is **not declared at all**, superseding the
  *"declare it, then wrap it in `msgin.Permanent`"* form recorded earlier the same day (§5.0b/§5.0c); and
  **D-M** — a **deterministic endpoint fault carries its own retry classification**, generalizing D-K from one
  sentinel to the class and making `ErrNilFunc` `Permanent` at all five producers (§5.0b). Both from
  [round 6 §1](../plans/027-audit-round-6.md).
- **Cites:** [ADR 0002 — Adapter SPI](0002-adapter-spi.md), which defined the `StreamingSource` name that §1
  renames and the `PollingSource`/`Delivery` contracts that stay put.
- **Amends:** [ADR 0019 — Runtime expression evaluation](0019-runtime-expression-evaluation.md) — its decision
  to accept `expr-lang` as a core-module dependency is reversed; its fail-at-construction contract is preserved.
- **RFC:** [0002](../rfcs/0002-eip-alignment.md), [0003](../rfcs/0003-endpoint-behavior-types.md) ·
  **Spec:** [014 §6–§7](../specs/014-core-package-layout.md) · **Plan:** [027](../plans/027-core-package-layout.md)
- **Relates to:** [ADR 0027](0027-core-package-restructure.md) (the packages these names live in),
  [ADR 0016 — robfig/cron dependency](0016-robfig-cron-dependency.md) (**unchanged** — see the dependency rule
  below, which explains why `expr-lang` leaves and `robfig` stays).

## Context

An audit of all 35 implemented components against Hohpe & Woolf and Spring Integration found high fidelity, with
drift concentrated in a handful of names. Separately, each composition endpoint takes an anonymous closure and
has a mirror `*Expr` twin (`FilterExpr`, `RouterExpr`, `TransformExpr`, `SplitExpr`, `WithCorrelationExpr`,
`WithReleaseExpr`). Those six in-core twins are the **only** reason `go list -deps .` pulls in `expr-lang` — a
**7.1 MB** tree forced on all seven modules in the workspace, including a consumer using nothing but the SQL
adapter.

The Aggregator already models the target shape: `WithCorrelationStrategy` (a named strategy) plus
`WithCorrelationExpr` (an expr provider of it). The question was whether to generalise that shape, and if so
whether the behavior contracts should be **func types** or **interfaces** — a choice that cannot be deferred,
because widening func→interface later is itself breaking.

## Decision

### 1. `StreamingSource` → `EventDrivenSource`

The canonical EIP term is *Event-Driven Consumer* (Spring: `EventDrivenConsumer`), and "streaming" collides with
unrelated streaming-data vocabulary. This was the clearest drift in the register.

**Scope, measured:** `StreamingSource` appeared **30 times across 12 `.go` files, all inside the root
module** — no satellite module referenced it. *(Corrected 2026-07-27, audit finding F7: the plan had sized
this as a seven-module rename.)* The rename **as it stands at `dadc775`** is `36 occurrences across 15 files`:

```
# REGENERATED at dadc775 (round-4 fix pass); was 30/12/35/14, taken before 1d7fc80 added endpoint/doc.go
$ grep -rn 'EventDrivenSource' --include='*.go' . | wc -l ; grep -rl 'EventDrivenSource' --include='*.go' . | wc -l
      31      13
$ grep -rn 'EventDrivenSource' . --exclude-dir=.git --exclude-dir=docs | wc -l
      36                          # +2 in CLAUDE.md, +3 in MESSAGING.md
$ grep -rn 'StreamingSource' . --exclude-dir=.git --exclude-dir=docs
(no output, exit 1)
```

**`MESSAGING.md` is named nowhere in this bundle** and carries three of the five (F10.4). The Consequences
already require editing `CLAUDE.md`; `MESSAGING.md` joins it.

**The rename edits a user-visible string**, which no move-list would show (round-2 §D14):

```
$ grep -n 'EventDrivenSource' errors.go
22:	ErrUnsupportedSource = errors.New("msgin: source implements neither PollingSource nor EventDrivenSource")
```

It belongs in `MIGRATION.md`.

**The verification gate must be scoped to `.go` plus the two root narratives, and must exclude `docs/`**
(round-2 §C4): `grep -rn 'StreamingSource' --include='*.md' .` returns **129 hits across 29 files** there,
including shipped ADRs 0002/0006/0008/0009/0010/0017/0018/0023 and shipped specs, which CLAUDE.md forbids
rewriting (*"supersede rather than rewrite old ADRs"*).

**The `Stream` method keeps its name.** It describes the mechanism accurately, Spring offers no counterpart name
to align with, and renaming it would churn every adapter for no gain. Recorded so it reads as considered rather
than overlooked.

### 2. `Exchange` is kept, qualified

Root keeps `RequestReplyExchange`; the implementation is `endpoint.ChannelExchange`, and its godoc **must
carry** a line disclaiming AMQP's broker-side-routing-table meaning.

> **NOT YET SHIPPED — corrected round 3.** This sentence, and Spec 014 §6, were written in the present tense
> ("with a godoc line disclaiming…") as though the line already existed. `grep -rn -i 'amqp' --include='*.go' .`
> returns **nothing**, workspace-wide. It is an outstanding **Spec 014 §8** obligation owned by Plan 027
> Task 11, and it has a grep-verifiable checkbox there now.

> **VERIFIED (audit round 1, finding G) — the decision stands.**
> `org.springframework.integration.gateway.RequestReplyExchanger` exists and is Spring Integration's default
> gateway service interface: *"When a gateway is declared with no `service-interface`, an internal framework
> interface `RequestReplyExchanger` is used"*
> (`docs.spring.io/spring-integration/reference/gateway.html`). Spring 6.5 adds `AsyncRequestReplyExchanger`.
> "Exchange" is therefore consensus vocabulary in this domain, not drift, and this decision does **not** revert
> to a rename. Plan 027 Task 3 is unblocked.
>
> **Recorded nit, decided.** Spring's is `RequestReplyExchang**er**` (agent noun, method `exchange`); ours is
> `RequestReplyExchange` with method `Exchange`. **We keep our form.** Go's single-method-interface convention
> points at the `-er` suffix, but our name is the *pattern* (Request-Reply), not the *agent*; the method is the
> verb; and `RequestReplyExchanger.Exchange` stutters agent-and-action. The citation was asked to establish that
> "exchange" is consensus vocabulary, and it does.
>
> *(One auditor reported this UNVERIFIED — it had no web access. The design auditor's verification, with the
> source above, is the one that stands.)*

### 3. Behavior types are named **func types**, with combinators as methods

```go
// package routing
type Predicate[A any]    func(ctx context.Context, m Message[A]) (bool, error)
type RouteFunc           func(ctx context.Context, m Message[any]) (MessageChannel, error)
type SplitFunc[A, B any] func(ctx context.Context, m Message[A]) ([]Message[B], error)
type CorrelationStrategy func(m Message[any]) (string, error)
type ReleaseStrategy     func(g MessageGroup) (bool, error)

// package transform
type Transformer[A, B any] func(ctx context.Context, m Message[A]) (Message[B], error)

func (p Predicate[A]) And(q Predicate[A]) Predicate[A]
func (p Predicate[A]) Or(q Predicate[A]) Predicate[A]
func (p Predicate[A]) Not() Predicate[A]
```

**`ReleaseStrategy` carries an error — corrected 2026-07-27 (findings B2/B3, decision §H5).** An earlier draft
wrote `func(g MessageGroup) bool`, which was **the sugar's shape mistaken for the contract's**. Three things
falsify the bool-only form:

1. **The internal field is already fallible.** `aggregator.go:15` declares `release func(MessageGroup) (bool, error)`.
2. **The error arm is load-bearing behavior with existing tests.**
   `TestAggregator_ReleaseExprReaperFallThrough` asserts the eval error propagates from `Handle`;
   `TestAggregator_ReleaseExprDrainCheckError` asserts the drain loop swallows it. An
   `expr.Release(s) (routing.ReleaseStrategy, error)` returning a bare `bool` would have to **swallow** runtime
   errors — silently returning `false`, stranding a group forever — or **panic** on caller input, which
   CLAUDE.md forbids. Either way §5's stated parity bar for the provider is unmeetable.
3. **It would orphan measured hot-path branches.** With the four `*Expr`-driven aggregator tests and
   `expr_test.go` excluded, coverage drops `NewAggregator` 100%→93.8% and `Handle` 100%→94.7%; the newly
   uncovered blocks are exactly `if cfg.optErr != nil { return nil, cfg.optErr }` and the release-decision
   error return. A bool-only `WithReleaseStrategy` cannot reach either — and CLAUDE.md makes an untestable
   hot-path branch a delivery blocker.

**The API that ships — decision D-E** (settled with the user 2026-07-27; shipped at
`routing/aggregator.go:82,89`):

```go
func WithReleaseStrategy(fn ReleaseStrategy) AggregatorOption            // the named, fallible type
func WithReleaseWhen(fn func(MessageGroup) bool) AggregatorOption        // sugar — wraps to (bool, nil)
```

> *Corrected (round-2 §D1).* An earlier draft kept `WithReleaseStrategy(func(MessageGroup) bool)` while
> naming the type `ReleaseStrategy` — so `agg.WithReleaseStrategy(myReleaseStrategy)` **did not compile**, and
> `expr.Release`'s return value had to go to a differently-named `WithRelease`. The option now takes its own
> named type, exactly as `WithCorrelationStrategy` already does, and the bool-only sugar is renamed
> `WithReleaseWhen`. This **renames** today's `WithReleaseStrategy` and **removes** the proposed `WithRelease`;
> both are breaking, which is free (zero tags, zero consumers), and both are in `MIGRATION.md` and Spec 014
> §4.1's `apidiff` expectations. `expr.Release` returns `routing.ReleaseStrategy`, so
> `WithReleaseStrategy(expr.Release(…))` now compiles.

It is also consistent with `CorrelationStrategy`, which already returns `(string, error)`.

### 3a. `cfg.optErr` is DELETED, not rescued — decision D-D

The claim that the fallible type *"fixes all three"* was **false**, and the compiler settled it. `cfg.optErr`
had **exactly one class of writer** — `expr.go`, which Task 1 deletes:

```
$ grep -n optErr aggregator.go          # AFTER deleting expr.go
256:  ...   257:  ...                    # the two READ sites only; every writer was in expr.go
```

So `aggregator.go`'s `if cfg.optErr != nil { return nil, cfg.optErr }` guard became **dead code no public API
could reach** — unreachable, not merely untested. **Decision: delete the field and the `NewAggregator` guard
in Task 1** (round-2 §B4; F5). `NewAggregator` is not "rescued to 100%"; the uncoverable branch is removed,
which is the correct fix for an unreachable branch.

Consequence for the plan: Task 9's old verification criterion — *"aggregator coverage returns to 100% on
`NewAggregator` **and** `Handle`"* — is **void as written**. `routing` measures 100% today with the guard
gone.

### 3b. D-E is a **Task 1 prerequisite**, not Task 9 work — a task-ordering fact, and it is load-bearing

`aggregator_test.go` held 10 references to the `*Expr` API. They split three ways, and the earlier plan
treated them as one undifferentiated group (F3):

| Case | Branch | Verdict |
|---|---|---|
| M-1 empty group snapshot | `toGroupEnv` guard (declared in `expr.go`) | genuinely leaves with expr → Task 10 |
| M-6 non-`A` member → `ErrPayloadType` | `toGroupEnv` guard (declared in `expr.go`) | genuinely leaves with expr → Task 10 |
| **H-1 reaper fall-through** | `reapGroup`, **core** | **must survive** |
| **H-2 drain-loop residual release-check error** | `release`, **core** | **must survive** |
| **H-3 drain-loop residual `releaseOnce` failure** | `release`, **core** | **must survive** |

H-1/H-2/H-3 are reachable **only** when the release check can return an error. Before this window the sole
fallible release strategy was `WithReleaseExpr`. **So Task 1 removes the only driver for three core hot-path
branches, and deferring D-E to Task 9 loses them silently** — a coverage loss no gate would attribute
correctly. Resolution taken: D-E was pulled forward into Task 1 and H-1/H-2/H-3 rewritten over a Go-func
`requireQtyRelease(min) msgin.ReleaseStrategy` helper. Coverage preserved.

*(Two fixtures died with M-6 and nothing reported it: `mixedTypeAddStore` and `mixedTypeGroup` existed only to
drive it, `unused` is off, and Go does not error on unused package-level declarations. Removed by hand.
`emptyGroupAddStore` **is** still live — the two are not symmetric despite looking it. F4.)*

**Interfaces + `XxxFunc` adapters were rejected.** That shape (`http.Handler`/`HandlerFunc`) would let a provider
implement `String()`, so a log line could name the expression that rejected a message — real value against
CLAUDE.md's debuggability criterion. But generics make the adapter conversion **explicit at every inline call
site** (`routing.PredicateFunc[Order](func(...){...})` — there is no inference on a generic type conversion),
taxing the common case to serve the rare one. The debuggability need is met instead by the expr provider
**wrapping runtime failures with the source expression text**, which covers the case that actually matters: a
predicate that errors.

### 4. Names drop the qualifier the package already carries

`routing.Predicate`, not `routing.FilterPredicate` (the draft) and not `routing.MessageSelector` (Spring).
Once ADR 0027 puts these in `routing` and `transform`, both candidates repeat what the package says, which is
exactly the stutter Go naming convention warns against; `transform.Transformer` has direct precedent in
`golang.org/x/text/transform`. **Each godoc names its Spring equivalent**, so recognition survives the
divergence — that mitigation is load-bearing, not decorative, and the plan must verify it on every type.

### 5. Expression support moves to its own module; the `*Expr` constructors are removed outright

No deprecated shims — nothing is tagged, so there is no consumer a shim would protect. Providers return the
§3 types, keeping the compile error at the provider call so the base constructors stay non-fallible and
inline-composable and ADR 0019's fail-at-construction contract survives:

```go
func Predicate[A any](s string) (routing.Predicate[A], error)
```

**A separate module, not a subpackage** — a subpackage of the root module would leave `expr-lang` in the root
`go.mod` and deliver none of the benefit. **It needs `require github.com/kartaladev/msgin v0.0.0` plus a
`replace … => ..`, exactly as every satellite module carries**: `git tag | wc -l` → 0, so without the
`replace` the module cannot resolve the root module under `GOWORK=off` — which is how CI's `module` job runs
it. A `use` line in `go.work` is necessary but not sufficient (round-2 §C2).

#### 5.0a The module owns its two error sentinels — decision D-I (2026-07-28)

> **STATUS: DECIDED, NOT YET IMPLEMENTED.** Both sentinels still exist in root at `dadc775`
> (`errors.go:180`, `:206`). Plan 027 Task 9.5 deletes them; Task 10 declares the **one** replacement. Every
> present-tense sentence below describes the decided end state, not the tree.

The `*Expr` deletion left `msgin.ErrInvalidExpression` and `msgin.ErrExprResultType` with **zero producers
anywhere in the workspace** — the only two root sentinels in that position. **Both leave root.** This module
declares `expr.ErrInvalidExpression` (prefix `msgin/expr:`); it declares **no** replacement for
`ErrExprResultType`, which under **revised D-K** (§5.0b) is expressed by wrapping root's existing
`msgin.ErrPayloadType`. So the `expr` module mints **one** sentinel, not two.

This **narrows an earlier reading of §5 in this ADR**. §5 says providers keep "the compile error at the
provider call", and Plan 027 §9.5.0 originally read that as an argument for keeping the *sentinel* in root so
the fail-at-construction contract had one home. The two are separable: what §5 protects is **where the error
is raised** (at the provider call, not at first message), which is untouched. **Which package declares the
`error` value** is a different question, and the tree already answers it — `adapter/http`, `adapter/database/sql`
and `adapter/cron` each mint their own sentinels for their own faults (51 of them) while returning root's for
contract-level faults (27 file→sentinel pairs). Root has no notion of an expression and, after Task 1, no code
that could produce one; the fault is the provider's. Spec 014 §3.2 carries the measured evidence, §7 the
declaration.

**Consequence:** `errors.Is(err, msgin.ErrInvalidExpression)` becomes `errors.Is(err, expr.ErrInvalidExpression)`,
and `errors.Is(err, msgin.ErrExprResultType)` becomes **`errors.Is(err, msgin.ErrPayloadType)`** (revised D-K,
§5.0b) — the second stays a root target, so that caller keeps the import they already had. The first caller
already imports `expr` to construct the endpoint, so no new import appears there either; `MIGRATION.md` carries
both lines. No alias is provided — an alias would have to reference the root vars this decision deletes.

#### 5.0b The two sentinels are NOT symmetric, and a deterministic fault needs a retry classification — decisions D-K (revised) and D-M

> **STATUS: DECIDED, NOT YET IMPLEMENTED.** At `dadc775` no `expr` module exists, `IsPermanent` enumerates
> three sentinels (`reliability.go:38-49`), and all five `ErrNilFunc` producers return the **bare** sentinel.
> Every present-tense sentence below describes the decided end state, not the tree. **D-M is a behavior change
> to shipped code** (four existing producers) and is listed among the exceptions Spec 014 §2.1 enumerates;
> Plan 027 Task 9 (combinators + producers) and Task 10 (the provider) own the edits.

**§5.0a's argument applies cleanly to `ErrInvalidExpression` and does NOT transfer to `ErrExprResultType`.**
The first is a **construction-time** fault raised at the provider call; ADR 0019's contract is untouched by
who declares it. The second is an **evaluation-time** fault that fires **per message, inside the flow**, and
therefore lands on the runtime's retry/dead-letter path:

```
$ git show ab233d9:expr.go | grep -n ErrExprResultType
179:  return Message[B]{}, fmt.Errorf("%w: result %T is not %T", ErrExprResultType, out, *new(B))
421:  return nil, fmt.Errorf("%w: SplitExpr result %T is not a slice", ErrExprResultType, out)
428:  return nil, fmt.Errorf("%w: SplitExpr element %d %T is not %T", ErrExprResultType, i, elem, *new(B))
```

Root's `IsPermanent` (`reliability.go:38`) enumerates `ErrPayloadType`, `ErrPayloadDecode` and
`ErrPayloadTooLarge` — faults that cannot fix themselves on redelivery. **A result-type mismatch is that same
class**, the expression-domain twin of `ErrPayloadType`, yet it is not in the list and the deleted originals
never wrapped it in `Permanent` either. So today it would be classified **transient**: retried `MaxAttempts`
times — and, per Spec 014 §10's per-instance attempt tracking, **`N × MaxAttempts` across N instances** — for
a deterministic fault that yields the identical wrong type every time.

That gap predates D-I. What D-I does is **close one door**: while the sentinel lived in root, `IsPermanent`
could be amended to name it; afterwards root cannot reference a provider-declared sentinel without re-creating
the import edge D-I exists to remove. An earlier draft called it *"the only door that could have fixed it in
one place"*, and **that was the false step** — the other door was always open and was never tried: **use a root
sentinel that `IsPermanent` already names.** Revised D-K walks through it.

**Decision D-K (REVISED, round 6 — supersedes the form settled earlier on 2026-07-28): the `expr` providers
wrap root's `msgin.ErrPayloadType`, and `expr.ErrExprResultType` is NOT declared at all.**

```go
return Message[B]{}, fmt.Errorf("%w: expr result %T is not %T", msgin.ErrPayloadType, out, *new(B))
```

> **What the earlier form was, and why it is withdrawn.** D-K as first recorded had the provider declare its
> own `expr.ErrExprResultType` and then wrap it —
> `fmt.Errorf("%w: result %T is not %T", msgin.Permanent(ErrExprResultType), out, *new(B))`. The paragraph
> above calls a result-type mismatch *"the expression-domain **twin** of `ErrPayloadType`"* and then mints a
> second sentinel for it anyway; §5.0c records the cost (every future CEL/starlark provider mints another, and
> callers get no shared `errors.Is` target) and offers two escapes, **neither of which was "wrap the twin"**.
> The obvious alternative was never weighed — the gap was the silence, not the reasoning.
> *(Round-6 design M4.)*

**Why the revised form is better on every axis that decided the original.**

- **One shared `errors.Is` target** for every present and future expression provider, which is exactly what
  §5.0c was written to lament.
- **The correct retry classification for free.** `ErrPayloadType` is already enumerated by `IsPermanent`
  (`reliability.go:38-49`), so **no `msgin.Permanent` wrap is needed** and D-K's whole classification concern
  dissolves rather than being paid for:

  ```
  IsPermanent(msgin: payload is not of the expected type   ) = true
  ```
- **No root change and no new import edge.** The provider already imports root for `msgin.Message`;
  `ErrPayloadType`'s own godoc (`errors.go`) is domain-generic — *a `Message[any]` payload cannot be asserted
  to `T`* — and a result-type mismatch is that same statement about the expression's output.

**D-I is unaffected.** `ErrInvalidExpression` still leaves root and the `expr` module still mints its own with
the `msgin/expr:` prefix: it is a **construction-time** fault with no root twin. What changes is the count —
root loses both sentinels, the `expr` module declares **one**, so Task 12's projected root arithmetic
`43 − 2 + 1 = 42` is **unchanged** (the `+1` is D-J's `ErrSharedReplyChannel`, not an expr sentinel) while the
projected `expr`-module sentinel count drops from 2 to **1**.

---

**Decision D-M (round 6): a deterministic endpoint fault carries its own retry classification, and
`ErrNilFunc` is `Permanent`.**

**The reasoning above is not specific to a result-type mismatch.** `IsPermanent` is a **closed enumeration**,
so *any* deterministic fault outside it is classified transient and retried. Measured at `aae6160` (root code
byte-identical to the `dadc775` pin — `git diff --name-only dadc775..aae6160 | grep -v '^docs/'` → `CLAUDE.md`
only):

```
IsPermanent(msgin: nil endpoint function                 ) = false
IsPermanent(msgin: no route for message                  ) = false
IsPermanent(msgin: payload is not of the expected type   ) = true
IsPermanent(msgin: message has no correlation key        ) = false
```

End-to-end, a `transform.Transform(nil)` step over a `memory` broker with `RetryPolicy{MaxAttempts: 3}`:

```
nil-func step: OnRetry=2  OnDeadLetter=1  OnInvalidMessage=0  (IsPermanent=false)
```

A nil endpoint function — **the most deterministic fault the library can produce**, identical on every
redelivery for the process's lifetime — consumes the full retry budget, lands in the **dead-letter** sink
instead of the **invalid-message** sink, and, via
`endpoint/consumer.go:614` (`c.safeRecord(md.Msg.ID(), err == nil || msgin.IsPermanent(err))`) and
`endpoint/consumer.go:733`, **records an unhealthy signal that trips the circuit breaker**. One mis-wired
`Filter(nil)` opens the circuit for the whole consumer.

**The tree already contains the correct precedent, with D-K's exact rationale, written before this window.**
`routing/aggregator.go:151-160` wraps `ErrNoCorrelation`:

```go
// defaultCorrelate reads HeaderCorrelationID as the group key. A missing or
// empty header is Permanent(ErrNoCorrelation): the runtime's IsPermanent does
// not match a bare ErrNoCorrelation, so without the wrap the message would be
// retried to the dead-letter sink instead of diverted to the invalid-message
// channel.
```

**The discriminator — this is the rule, not the list.** Classify by **when the fault's inputs are fixed**:

- **Fixed at construction, or by the message itself → `Permanent`.** It cannot change on redelivery.
- **Evaluated per message against caller-supplied, possibly-mutable state → transient.** It may legitimately
  resolve on redelivery.

Applied to every in-tree instance:

| Sentinel | Inputs fixed | Classification | Rationale |
|---|---|---|---|
| `ErrNilFunc` (all producers) | at construction — the nil is captured in the closure | **`Permanent`** | `nilFuncStep` closes over nothing; `Router.pick` is set once in `NewRouter`. Nothing can make it non-nil later |
| result-type mismatch (`expr`) | at construction (the expression) + by `T` | **`Permanent`** | Revised D-K above — inherited from `ErrPayloadType`, no wrap needed |
| `ErrNoCorrelation` | by the message's headers | **`Permanent`** (already) | `routing/aggregator.go:160` — the precedent quoted above |
| **`ErrNoRoute`** | **per message, by caller-supplied `pick`** | **transient — UNCHANGED** | `routing/router.go:48-56`: `pick` is a caller function evaluated per message; it may consult a routing table, feature flag, or lookup service that changes. A message unroutable now may be routable after a config reload. `WithDefaultChannel` is the documented way to make the outcome deterministic |

`ErrNoRoute` staying transient is the load-bearing half of the rule: it shows the discriminator **excludes**
as well as includes, so this reads as a classification rule rather than a sweep of everything unenumerated.

**Scope — five producers become `msgin.Permanent(msgin.ErrNilFunc)`** (verified at the `dadc775` code pin):

| Site | Declaration |
|---|---|
| `endpoint/helpers.go:21` | `nilFuncStep` |
| `routing/helpers.go:23` | `nilFuncStep` (package-local copy) |
| `transform/transformer.go:38` | `nilFuncStep` (package-local copy) |
| `routing/router.go:48` | `Router.Handle`, `r.pick == nil` |
| Plan 027 Task 9's `Predicate.And` / `Or` / `Not` | new in this window — see below |

**A sixth `ErrNilFunc` producer exists and is deliberately EXCLUDED:** `routing/aggregator.go:251`
(`NewAggregator`, `if fn == nil { return nil, msgin.ErrNilFunc }`). It is **construction-time** — returned to
the caller from a constructor, never carried through `Handle` — so it never reaches a `RetryPolicy` and a
retry classification would be meaningless on it. The five in the table are the **flow-path** producers, the
ones whose error a consumer classifies. Stated so the next re-derivation of this list does not read the
exclusion as an omission:

```
# scope: root module, at the dadc775 code pin (verified byte-identical at aae6160)
$ grep -rn 'msgin\.ErrNilFunc' --include='*.go' . | grep -v _test | grep -v '//'
endpoint/helpers.go:21:		return msgin.HandlerFunc(func(context.Context, msgin.Message[any]) error { return msgin.ErrNilFunc })
routing/aggregator.go:251:		return nil, msgin.ErrNilFunc
routing/router.go:48:		return msgin.ErrNilFunc
routing/helpers.go:23:		return msgin.HandlerFunc(func(context.Context, msgin.Message[any]) error { return msgin.ErrNilFunc })
transform/transformer.go:38:		return msgin.HandlerFunc(func(context.Context, msgin.Message[any]) error { return msgin.ErrNilFunc })
```

**Five hits, four of which are flow-path** — the `nil, msgin.ErrNilFunc` shape is the constructor return and
is the one excluded; the fifth flow-path producer (`Predicate.And`/`Or`/`Not`) does not exist yet. The
invariant to re-derive against, not the count: **every `ErrNilFunc` returned from a `MessageHandler` body is
`Permanent`; every one returned from a constructor is bare.**

**Task 9's combinators are amended by this decision.** They were decided to degrade to
`(false, msgin.ErrNilFunc)` **at evaluation, per message** — three *new* producers of a bare, deterministic
`ErrNilFunc` on the retry hot path, authored after D-K had already identified the class. They now return
**`(false, msgin.Permanent(msgin.ErrNilFunc))`**, and Task 9's hot-path branch list gains a case asserting
`msgin.IsPermanent(err)` on a combinator's nil result.

**Debuggability.** `errors.Is` is preserved by the wrap, but the bare sentinel collapses six distinct nil
positions into the single string `msgin: nil endpoint function` — no indication of `And` vs `Or` vs `Not`,
receiver vs argument, or which link of `p.And(q).Or(r)`. CLAUDE.md requires *"typed, wrapping errors that name
the offending field/input"*, so each combinator wraps with positional context:

```go
fmt.Errorf("%w: routing.Predicate.And: nil argument", msgin.Permanent(msgin.ErrNilFunc))
```

and each combinator's godoc states that `errors.Is(err, msgin.ErrNilFunc)` still matches.

**This is a class, not a case.** The instances above are the complete in-tree list *at this window*, and the
list is not the contract — **the discriminator is**. Any endpoint that returns a typed error whose inputs are
fixed at construction, or determined by the message itself, must classify it `Permanent` at the producer,
because `IsPermanent`'s enumeration is closed and root cannot be amended for every subpackage and provider
fault (that is exactly the door D-I closes for `expr`). Recording only `ErrExprResultType` was the original
defect: it fixed the named instance while the same defect stayed live in four shipped producers and was about
to be re-authored in three new ones. *(Round-4 design audit BLOCKER 3 opened this; round-6 design B4 and M4
generalized and corrected it.)*

#### 5.0c A second expression provider would mint a third `ErrInvalidExpression` — recorded, not solved

> **NARROWED by revised D-K (round 6).** This section was written when the `expr` module declared **two**
> sentinels, and it applied to both. Under revised D-K the evaluation-time fault is expressed by wrapping
> root's `msgin.ErrPayloadType`, which **is** the shared `errors.Is` target this section wanted — so the
> problem below, and both escapes offered for it, are **moot for that half** and are retained **only for
> `ErrInvalidExpression`**.

A future CEL or starlark provider mints `cel.ErrInvalidExpression` / `starlark.ErrInvalidExpression` for the
same conceptual fault, so a caller with generic handling ("the user's expression was bad → HTTP 400") must
enumerate every provider, with no shared `errors.Is` target — exactly what root's sentinel gave for free.
**The evaluation-time counterpart no longer has this shape**: every provider's result-type mismatch is
`msgin.ErrPayloadType`, one target, forever.

**This does not reverse D-I.** Nothing is released, the fault genuinely is the provider's, and the repo
precedent (51 adapter-minted sentinels alongside 27 root-sentinel returns) supports it. But it is a
foreseeable consequence with two known escapes, and it is recorded here so a future increment does not
rediscover it as a surprise: every provider can wrap with `msgin.Permanent` (giving a shared *classification*
even without a shared sentinel), or root can later add an `ErrInvalidConfig`-class sentinel that providers
wrap. **Read both escapes as scoped to `ErrInvalidExpression` alone.** The second escape is, in effect, what
revised D-K already did for the other sentinel — wrap an existing root target instead of minting a new one —
and it is worth noting that the general fix and the specific one are the same move.
*(Round-4 design audit MINOR 6; narrowed by round-6 design M4.)*

### 5a. The provider shape is NOT uniform, and it is NOT non-generic

Two separate errors, corrected in two rounds.

**Not uniform** (round-2 §D2). An earlier draft asserted a uniform `(string) → (T, error)` shape for all six.
**`RouterExpr` never had that shape**: it also took `routes map[string]MessageChannel`, and it carried **two**
construction validations of its own.

**Not non-generic** (round 3, compile-proven by an auditor). The corrected draft then wrote
`Correlation(s string)`, `Release(s string)` and `RouteFunc(s string, routes …)` **without a type parameter**,
and that does not compile. Every deleted original was `[A any]`:

```
$ git show ab233d9:expr.go | grep -nE '^func '
 89:func FilterExpr[A any](expression string, opts ...FilterOption) (Step, error)
115:func RouterExpr[A any](keyExpr string, routes map[string]MessageChannel, opts ...RouterOption) (*Router, error)
167:func TransformExpr[A, B any](expression string) (Step, error)
217:func SplitExpr[A, B any](expression string) (Step, error)
321:func WithCorrelationExpr[A any](expression string) AggregatorOption
390:func WithReleaseExpr[A any](expression string) AggregatorOption
```

`A` is load-bearing twice, and both uses are things this ADR elsewhere insists on:

1. **`compile[A]` (`expr.go:35`) type-checks `payload.Field` against `A`.** That is what makes §5's
   preservation of ADR 0019's *fail-at-construction* contract real rather than nominal.
2. **`PayloadOf[A]` (`expr.go:129,224,284,331`) IS the M-6 `ErrPayloadType` branch** §3b hands to Task 10. A
   non-generic provider has nothing to assert the member payload to, so that branch cannot exist and §5's
   parity bar is unmeetable — the same failure mode the bool-only `ReleaseStrategy` had.

The provider set, stated correctly:

```go
func Predicate[A any](s string)      (routing.Predicate[A], error)
func SplitFunc[A, B any](s string)   (routing.SplitFunc[A, B], error)
func Transformer[A, B any](s string) (transform.Transformer[A, B], error)
func Correlation[A any](s string)    (routing.CorrelationStrategy, error)
func Release[A any](s string)        (routing.ReleaseStrategy, error)
func RouteFunc[A any](s string, routes map[string]msgin.MessageChannel) (routing.RouteFunc, error)
```

**`A` is not inferable from a `string` argument**, so every call site instantiates explicitly —
`expr.Release[Order]("qty > 10")`. This is an ergonomic cost of the split, it is unavoidable given (1) and
(2), and each provider's godoc must state it. Plan 027 Task 10's branch list carries `RouteFunc`'s two extra
construction validations, and the two `toGroupEnv` guard cases (M-1, M-6) that genuinely belong in this module
rather than in the core.

### 6. The dependency rule that makes this coherent with ADR 0016

> **A zero-transitive dependency is pushed to its own module when its weight is material to consumers who do not
> use it.**

`expr-lang` (7.1 MB, zero transitive deps) is material — it leaves. `robfig/cron` (144 KB, zero transitive deps,
≈50× smaller) is not — it stays in the root module, and **ADR 0016 stands unchanged**. Both are dependency-free
leaves, so "purity" cannot distinguish them; without this stated rule the two decisions read as arbitrary and an
adversarial audit would rightly flag them.

## Consequences

**Positive.** The root module sheds a 7.1 MB forced dependency. Third parties can supply other providers (regex,
OPA, feature-flag) yielding the same types, so endpoints are open for extension with no new constructor. Six
parallel `*Expr` constructors collapse into one base constructor per endpoint. Predicate combinators arrive free.

**Negative, accepted.**

- **CLAUDE.md and `MESSAGING.md` must be edited in the same commit** — CLAUDE.md's dependency policy ratifies
  `expr-lang` as one of three accepted core exceptions **and names `FilterExpr`/`RouterExpr` at `CLAUDE.md:235`
  as its justification** (both deleted), its architecture blueprint named `StreamingSource`, and `MESSAGING.md`
  carries three more rename sites. Leaving any of them stale is a traceability breach.
- **Two error sentinels are orphaned in root — DECIDED, not open.** `ErrInvalidExpression` (declared
  `errors.go:180`) and `ErrExprResultType` (declared `errors.go:206`) have **zero producers** after the `*Expr`
  deletion, and their godoc names constructors that no longer exist (F11.7). **Decision D-I (§5.0a): both leave
  root.** The `expr` module declares `expr.ErrInvalidExpression`; the evaluation-time fault is expressed by
  wrapping root's existing `msgin.ErrPayloadType` (**revised D-K**, §5.0b), so no second `expr` sentinel is
  minted. **Plan 027 Task 9.5 deletes them; Task 10 declares the one replacement** — it is not Task 10's
  decision to make. **NOT YET IMPLEMENTED:** both vars still exist in the tree at the `dadc775` code pin.

  > *Corrected (round 6, C-B1 + C-B3).* This bullet survived the D-I pass unamended and was the worst kind of
  > stale: it presented a **settled** decision as open, framed both options as live, and assigned it to the
  > **wrong task** — an implementer reading only the Consequences would defer a Task 9.5 checkbox and re-open
  > D-I. It also carried a **third** distinct line-number pair for the same two declarations (`:161`/`:183`,
  > after `:156`-era and other variants); both cited lines hold unrelated godoc. Verified:
  > `grep -n 'ErrInvalidExpression\|ErrExprResultType' errors.go` → `180:	ErrInvalidExpression = errors.New("msgin: invalid expression")`
  > and `206:	ErrExprResultType = errors.New("msgin: expression result type mismatch")`. §5.0a and this ADR's
  > header record the settlement; **this section is where a reader looks for it and is the surface a decision
  > must be swept into** ([round 6 §0](../plans/027-audit-round-6.md), counter-rule 1).
- **The `*Expr` deletion leaves godoc mentions no compiler can see**, and Spec 014 §8.1's staleness sweep needs
  a **second arm** for deleted symbols: the moved-symbol arm greps `msgin.<sym>` and structurally cannot find a
  mention of a symbol that no longer exists. **Arm 2 returns exactly ONE survivor at the `dadc775` code pin**
  — `WithRelease`, at `routing/aggregator.go:316` (`// release-decision error (the WithRelease strategy
  failed)`; the option is `WithReleaseStrategy`). `errors.go` and `routing/splitter.go` contribute **zero**.
  Re-derived by running [Spec 014 §8.1](../specs/014-core-package-layout.md)'s arm-2 `comm -23` pipeline
  **unmodified** (it is 12 lines and lives there; this ADR deliberately does not fork a copy that can drift).
  Its complete stdout, observed at `aae6160`:

  ```
  WithRelease
  ```

  and the site: `grep -rn 'WithRelease strategy' routing/aggregator.go` →
  `routing/aggregator.go:316:		return err // release-decision error (the WithRelease strategy failed) → retry/DLQ;`

  > *Corrected (round 6, C-B2).* This bullet read *"Seven survive today in `errors.go`, `routing/splitter.go`,
  > and `routing/aggregator.go`"*. That figure is **fiction** — it came from the pre-round-4 arm 2, a hardcoded
  > list of the six `*Expr` names that returned **zero hits at every frame it was ever run against**, published
  > alongside a survivor list at named lines that hold unrelated text. Rounds 4 and 5 established this and wrote
  > `ROUND-4 CORRECTION` blocks into **Spec §8.1 and Plan §9.5.1**; the ADR that both cite was never opened.
  > Arm 2's reach is also narrower than "seven godoc mentions" implies: it matches only `With|Err|New` names, so
  > it covers 2 of the 6 `*Expr` names and misses `StreamingSource` entirely — see Spec §8.1's stated blind
  > spots.
- `apidiff` **will** report the parameter-type change on every typed constructor. Call sites are
  source-compatible (a bare closure stays assignable), so this is expected and benign — the plan records it as a
  *reviewed, source-compatible* entry rather than claiming zero output, so the "only intended changes" gate
  passes deliberately rather than by surprise.
- An eighth module to build, test, tag, and keep in `go.work`.
- **No `String()`/`Describe()` on a behavior.** Introspection must come from provider-side error wrapping;
  revisiting this after v1 is a major bump. This is the accepted cost of §3.

**Sequencing consequence.** All six `*Expr` constructors return `Step`, `*Router`, or `AggregatorOption` — types
that move to `routing`/`transform` under ADR 0027. `expr.go` therefore cannot remain in root once those move,
and splitting it across two new packages only to delete it afterwards is throwaway work. The `*Expr` deletion is
sequenced **first** in Plan 027, which leaves expression support absent from the branch until the `expr` module
lands. That is acceptable within a branch that restores it before merge, and is called out so it is not mistaken
for dropped scope.
