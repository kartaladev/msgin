# RFC-0003 — Endpoint behavior types & provider model

- **Author:** kartaladev/msgin maintainers
- **Date:** 2026-07-22
- **Status:** Accepted (open questions settled 2026-07-27 — see §7)
- **Promoted to:** [Spec 014](../specs/014-core-package-layout.md) ·
  [ADR 0029](../adrs/0029-eip-lexical-alignment.md) · [Plan 027](../plans/027-core-package-layout.md).
  **Folded into the same increment as RFC-0001/0002** — all six `*Expr` constructors return types that move to
  `routing`/`transform`, so `expr.go` cannot survive the restructure; splitting it across two new packages only
  to delete it next increment would be throwaway work (Spec 014 §7).
- **Reviewers:** TBD

## 1. Summary

Give each composition endpoint's behavioral closure a **named func type** (`routing.Predicate[A]`,
`routing.RouteFunc`, …) with combinator methods, so expression support becomes a **provider of that type** passed
into the one base constructor. This kills the six parallel `*Expr` constructors and lets the pattern core shed
its `expr-lang` dependency.

## 2. Background & Motivation

Each endpoint takes an anonymous closure and has a mirror `*Expr` twin (`FilterExpr`, `RouterExpr`,
`TransformExpr`, `SplitExpr`, `WithCorrelationExpr`, `WithReleaseExpr`). Those in-core twins are *why* the core
imports `expr-lang/expr` (ADR 0019). The Aggregator already models the target shape: `WithCorrelationStrategy`
(named strategy) + `WithCorrelationExpr` (an expr provider of it). This RFC generalises that proven shape to
the other endpoints.

## 3. Proposal

### Overview

Name the behavioral types in `endpoint`; move the expr constructors to a provider package that yields those
types; drop `expr-lang` from the core.

### Detailed Design

The names are **package-qualified** (RFC-0001 puts them in `routing` and `transform`), so they drop the
`Message*`/`*Predicate` prefixes that would repeat what the package already says:

```go
// package routing  (EIP ch.7)
type Predicate[A any]      func(ctx context.Context, m Message[A]) (bool, error)
type RouteFunc             func(ctx context.Context, m Message[any]) (MessageChannel, error)
type SplitFunc[A, B any]   func(ctx context.Context, m Message[A]) ([]Message[B], error)
type CorrelationStrategy   func(m Message[any]) (string, error)   // already implicit
type ReleaseStrategy       func(g MessageGroup) (bool, error)     // already implicit

// package transform  (EIP ch.8)
type Transformer[A, B any] func(ctx context.Context, m Message[A]) (Message[B], error)
```

Base constructors just get typed (`func Filter[A any](p Predicate[A], opts ...FilterOption) Step`).
**Naming a func type is backward-compatible** — a bare closure stays assignable — so introducing the types is
non-breaking.

> **`ReleaseStrategy` is fallible — corrected 2026-07-27 (audit round 1, findings B2/B3; decision §H5).** An
> earlier draft wrote `func(g MessageGroup) bool`. That was **the sugar's shape mistaken for the contract's**:
> the aggregator's internal field is already `release func(MessageGroup) (bool, error)` (`aggregator.go:15`),
> and the error arm is load-bearing behavior with two existing tests asserting it
> (`TestAggregator_ReleaseExprReaperFallThrough` — the error propagates from `Handle`;
> `TestAggregator_ReleaseExprDrainCheckError` — the drain loop swallows it).
>
> A bool-only named type would force `expr.Release` to either **swallow** a runtime evaluation error (silently
> returning `false`, stranding the group forever) or **panic** on caller input, which CLAUDE.md forbids. It
> would also **orphan two measured hot-path branches** once the `*Expr` constructors are deleted:
> `NewAggregator` 100%→93.8% and `Handle` 100%→94.7%, the newly-uncovered blocks being
> `if cfg.optErr != nil { return nil, cfg.optErr }` and the release-decision error return — branches no
> remaining public API could reach, which CLAUDE.md's hot-path rule makes a delivery blocker.
>
> **Both problems are solved by the fallible type, plus sugar for the easy path** (CLAUDE.md's *sensible
> defaults, opinionated but overridable*):
>
> ```go
> type ReleaseStrategy func(g MessageGroup) (bool, error)          // the contract — carries the error
>
> func WithReleaseStrategy(fn ReleaseStrategy) AggregatorOption            // the named, fallible type
> func WithReleaseWhen(fn func(MessageGroup) bool) AggregatorOption        // sugar — wraps to (bool, nil)
> ```
>
> Consistent with `CorrelationStrategy`, which already returns `(string, error)`.
>
> > **Naming corrected 2026-07-28 (decision D-E, audit round 2 §D1/§G.1).** The shape above originally kept
> > `WithReleaseStrategy(func(MessageGroup) bool)` while naming the type `ReleaseStrategy`, so
> > `agg.WithReleaseStrategy(myReleaseStrategy)` **did not compile** and `expr.Release`'s return value had to
> > go to a differently-named `WithRelease`. The option now takes its own named type — exactly as
> > `WithCorrelationStrategy` already does — and the bool-only sugar is `WithReleaseWhen`. Shipped at
> > `routing/aggregator.go:82,89`. See [ADR 0029 §3](../adrs/0029-eip-lexical-alignment.md).

**Combinators are methods on the named types** (this is the payoff that distinguishes naming them from not):

```go
func (p Predicate[A]) And(q Predicate[A]) Predicate[A]
func (p Predicate[A]) Or(q Predicate[A]) Predicate[A]
func (p Predicate[A]) Not() Predicate[A]
```

**Decision — func types, not interfaces (2026-07-27).** An interface + `XxxFunc` adapter (the
`http.Handler`/`HandlerFunc` shape) was the alternative; it would let a provider implement `String()` so a log
line could name the expression that rejected a message. Rejected because generics make the adapter conversion
**explicit at every inline call site** (`routing.PredicateFunc[Order](func(...){...})` — no inference on a
generic type conversion), taxing the common case to serve the rare one. The debuggability need is met instead by
**the expr provider wrapping runtime failures with the source expression text**, which covers the case that
actually matters: a predicate that errors. Since widening func→interface later is itself breaking, this is
decided up front rather than deferred.

> **Audit (2026-07-24) — "non-breaking" is source-level; expect an apidiff signal.** Callers passing bare
> closures or func literals still compile (Go assignability), so phase 1 is **source-compatible**. But the
> library quality gate runs `apidiff`/`gorelease`, which **will report the parameter-type change** on each
> typed constructor. That is expected and benign — the plan should record it as a *reviewed, source-compatible*
> apidiff entry rather than claim zero apidiff output, so the gate's "only intended changes" check passes
> deliberately rather than by surprise.

Expr becomes a provider in **its own module** (`expr`, separate `go.mod` — settled 2026-07-27, see §7.3) that
returns `(T, error)`:

```go
func Predicate[A any](s string) (routing.Predicate[A], error)   // compiles once, fails at construction
```

The compile error lives at the provider call, so the base `Filter` stays non-fallible and inline-composable,
and the "invalid expression fails at construction" contract is preserved.

### Examples

```go
p, err := expr.Predicate[Order]("payload.Amount > 100")
if err != nil { /* handle */ }
step := routing.Filter(p.And(notCancelled), routing.WithDiscardChannel(dlq))
```

Third parties can supply other providers (regex, OPA, feature-flag) yielding the same types — endpoints are
open for extension with no new constructor.

## 4. Trade-offs & Alternatives

### Alternatives Considered

- **`WithPredicateFactory(...)` option** (user-proposed): a *fallible* factory forces `Filter` to return
  `(Step, error)` or defer the compile error to runtime — regressing either ergonomics or the
  fail-at-construction contract. Rejected in favour of the positional provider.
- **Interface instead of func type**: richer providers (stateful, `String()` for logging the expression) but
  more ceremony, and widening func→interface later is itself breaking — so the choice must be made up front.

### Trade-offs

Naming types + adding the provider is additive; the valuable-but-heavy part is *removing* `expr-lang` from the
core (amends ADR 0019), which is the breaking step.

## 5. Implementation Plan

### Phases

1. Introduce the named types + combinator methods, and type the base constructors — source-compatible, but
   **no longer separable from the window** (see the Timeline note). — S
2. Add the `expr` provider **module** producing those types — additive. — M
3. Remove the core `*Expr` constructors outright, drop `expr-lang` from core deps; amend
   ADR 0019 **and CLAUDE.md's Dependency policy** (which lists `expr-lang` as one of the three accepted
   core-module exceptions) in the same commit — **breaking**, in the window. — S

> **Audit (2026-07-24) — verified & scoped.** `go list -deps .` today pulls in the full `expr-lang` tree via
> `expr.go`/`doc_composition.go`, so the motivation is real and phase 3 genuinely removes a forced transitive
> dep. Two consequences to encode in the plan: (a) removing `expr-lang` from the **root module** requires the
> provider to live in a **separate module** (an `endpoint/expr` *subpackage* in the root module would leave
> `expr-lang` in the root `go.mod`); (b) the CLAUDE.md Dependency-policy edit is mandatory, not optional — that
> file currently ratifies `expr-lang` as a core exception, and traceability forbids leaving it stale.

### Timeline

> **Revised 2026-07-27 — phase 1 is no longer "anytime".** The draft treated phase 1 as non-breaking work that
> could land ahead of the window, which was true only while the types were going to live in a flat `msgin.`
> namespace. RFC-0001 chose C-full, so these types are **born in `routing` and `transform`** — packages that do
> not exist until the restructure lands. All three phases now sit inside the window, in order. This removes the
> mitigation the [index](README.md) relied on for deferral risk; the compensating decision is to run the window
> **first**, ahead of the feature roadmap.

### Success Metrics

`go list -deps` on the core package no longer includes `expr-lang`; `*Expr` API removed or shimmed; predicate
combinators (`And`/`Or`/`Not`) available.

## 6. Risks & Mitigations

| Risk | Impact | Mitigation |
|---|---|---|
| Wrong D1 choice (func vs interface) locks in a breaking change | Future churn | **Decided 2026-07-27: func types** (§7.1). Accepted consequence: no `String()`/`Describe()` on a behavior, so introspection must come from provider-side error wrapping. Revisiting this after v1 is a major bump |
| Compile error leaks to per-message | Debuggability regression | Provider returns `(T, error)`; test build-time failure |
| Over-abstraction (a type per callback) | API bloat | Scope to the six args that have alternate providers |

## 7. Decisions (settled 2026-07-27)

1. **Func type vs interface → named func types, with combinators as methods.** Rationale and the rejected
   interface shape are in §3. Decided up front because widening func→interface later is breaking.
2. **Naming → drop the qualifier the package already carries.** Final set: `routing.Predicate[A]`,
   `routing.RouteFunc`, `routing.SplitFunc[A,B]`, `routing.CorrelationStrategy`, `routing.ReleaseStrategy`,
   `transform.Transformer[A,B]`. Neither `FilterPredicate` nor Spring's `MessageSelector` survives contact with
   the package split; `transform.Transformer` has direct precedent in `golang.org/x/text/transform`. Each
   godoc names its Spring equivalent so a Spring-trained reader still finds the type.
3. **Expr → its own module** (separate `go.mod`), not a subpackage. The audit note in §5 is decisive: a
   subpackage of the root module leaves `expr-lang` in the root `go.mod` and delivers none of the benefit.
   **Weight is what justifies the module:** `expr-lang` is 7.1 MB on disk and propagates to all seven modules,
   even for a consumer using only the SQL adapter.
4. **`*Expr` → removed outright, no deprecated shims.** Consistent with RFC-0001's clean-break decision;
   nothing is tagged, so there is no consumer a shim would protect.
5. **`ReleaseStrategy` → `func(MessageGroup) (bool, error)`** (settled 2026-07-27; audit round 1 §H5,
   naming settled 2026-07-27 as **decision D-E**). The named type carries the error — matching the internal
   field it names and `CorrelationStrategy`'s existing shape — and
   **`WithReleaseStrategy` takes that named type**, with **`WithReleaseWhen(func(MessageGroup) bool)`** as the
   sugar for the common infallible case. Full rationale in §3.
   *Easy default path plus a fully capable escape hatch, per CLAUDE.md's sensible-defaults rule.*
   **Sequencing note:** this lands in Plan 027 **Task 1**, not Task 9 — deleting the `*Expr` constructors
   removes the only driver for three core aggregator hot-path branches, so a fallible release strategy must
   exist in the same change ([ADR 0029 §3b](../adrs/0029-eip-lexical-alignment.md);
   [`027-derivation-findings.md` F3](../plans/027-derivation-findings.md)).

> **Dependency-policy rule adopted here (2026-07-27), to keep this coherent with RFC-0004.** RFC-0004 keeps
> `robfig/cron` *inside* the root module while this RFC pushes `expr-lang` *out* — both are zero-transitive
> leaves, so "purity" cannot distinguish them. The stated rule is: **a zero-transitive dependency is pushed to
> its own module when its weight is material to consumers who do not use it.** `expr-lang` at 7.1 MB is;
> `robfig/cron` at 144 KB (≈50× smaller) is not. Without this rule the two decisions read as arbitrary, and an
> adversarial audit would flag them.

## 8. Appendix

**Appendix A — endpoint → behavior type → provider map** (names updated 2026-07-27 for the package split):

| Endpoint | Behavior type | Provider |
|---|---|---|
| `routing.Filter` | `routing.Predicate[A]` | `expr.Predicate` |
| `routing.Router` | `routing.RouteFunc` | `expr.RouteFunc` |
| `routing.Split` | `routing.SplitFunc[A,B]` | `expr.SplitFunc` |
| `routing.Aggregator` (correlation) | `routing.CorrelationStrategy` | `expr.Correlation` |
| `routing.Aggregator` (release) | `routing.ReleaseStrategy` | `expr.Release` |
| `transform.Transform` | `transform.Transformer[A,B]` | `expr.Transformer` |

Third parties can supply other providers (regex, OPA, feature-flag) yielding the same types — endpoints stay
open for extension with no new constructor.
