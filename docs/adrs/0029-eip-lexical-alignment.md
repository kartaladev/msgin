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
  ([audit round 2 §G.1](../plans/027-audit-round-2.md)).
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
this as a seven-module rename.)* **That figure is exactly right, and it is `.go`-only** — the rename is
`35 occurrences across 14 files`:

```
$ grep -rn 'EventDrivenSource' --include='*.go' . | wc -l ; grep -rl 'EventDrivenSource' --include='*.go' . | wc -l
      30      12
$ grep -rn 'EventDrivenSource' . --exclude-dir=.git --exclude-dir=docs | wc -l
      35                          # +2 in CLAUDE.md, +3 in MESSAGING.md
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
- **Two error sentinels are orphaned in root and need a decision, not a default.** `ErrInvalidExpression`
  (`errors.go:161`) and `ErrExprResultType` (`errors.go:183`) have **zero users** after the `*Expr` deletion,
  and their godoc names constructors that no longer exist (F11.7). Either the `expr` module imports them from
  root — keeping one error contract, consistent with §5's provider-side wrapping — or they leave root's closed
  contract. Task 10 decides and records it; leaving two unreferenced sentinels inside a *closed* root contract
  is not neutral.
- **The `*Expr` deletion leaves godoc mentions no compiler can see.** Seven survive today in `errors.go`,
  `routing/splitter.go`, and `routing/aggregator.go`. Spec 014 §8.1's staleness sweep needs a **second arm**
  for deleted symbols: the moved-symbol arm greps `msgin.<sym>` and structurally cannot find a mention of a
  symbol that no longer exists.
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
