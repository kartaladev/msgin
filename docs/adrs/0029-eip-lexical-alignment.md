# ADR 0029 — EIP lexical alignment, named behavior types, and the expr provider module

- **Status:** **NEEDS-REVISION (2026-07-27) — DO NOT IMPLEMENT.** All three auditors returned `NEEDS-REVISION` —
  see **[Plan 027 audit round 1](../plans/027-audit-round-1.md)**. Unresolved in this ADR: **§3's
  `ReleaseStrategy func(g MessageGroup) bool` cannot express an evaluation error**, which makes the expr
  provider's stated parity bar impossible and would strand messages silently — it must become
  `(bool, error)`; and deleting the `*Expr` constructors **orphans two measured aggregator hot-path branches**
  (`NewAggregator` 100%→93.8%, `Handle` 100%→94.7%) that no remaining public API can reach.
  **§2's `RequestReplyExchanger` citation is VERIFIED and stands** — see the audit record §G; Plan 027 Task 3 is
  unblocked.
- **Must additionally cite:** [ADR 0002](0002-adapter-spi.md), which defined the `StreamingSource` name that §1
  renames.
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

**The `Stream` method keeps its name.** It describes the mechanism accurately, Spring offers no counterpart name
to align with, and renaming it would churn every adapter for no gain. Recorded so it reads as considered rather
than overlooked.

### 2. `Exchange` is kept, qualified

Root keeps `RequestReplyExchange`; the implementation is `endpoint.ChannelExchange`, with a godoc line
disclaiming AMQP's broker-side-routing-table meaning.

> **Open verification, for the audit.** The rationale is that Spring Integration names its own equivalent
> interface `RequestReplyExchanger`, making "exchange" the consensus term rather than drift. **This ADR asserts
> that from recall and it has not been verified against the Spring source or reference docs.** The audit must
> confirm it. If the citation does not hold, this decision reverts to a rename, because then the only argument
> left for keeping the term is inertia against a genuine AMQP collision.

### 3. Behavior types are named **func types**, with combinators as methods

```go
// package routing
type Predicate[A any]    func(ctx context.Context, m Message[A]) (bool, error)
type RouteFunc           func(ctx context.Context, m Message[any]) (MessageChannel, error)
type SplitFunc[A, B any] func(ctx context.Context, m Message[A]) ([]Message[B], error)
type CorrelationStrategy func(m Message[any]) (string, error)
type ReleaseStrategy     func(g MessageGroup) bool

// package transform
type Transformer[A, B any] func(ctx context.Context, m Message[A]) (Message[B], error)

func (p Predicate[A]) And(q Predicate[A]) Predicate[A]
func (p Predicate[A]) Or(q Predicate[A]) Predicate[A]
func (p Predicate[A]) Not() Predicate[A]
```

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
`go.mod` and deliver none of the benefit.

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

- **CLAUDE.md must be edited in the same commit** — its dependency policy currently ratifies `expr-lang` as one
  of three accepted core exceptions, and its architecture blueprint names `StreamingSource`. Leaving either
  stale is a traceability breach.
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
