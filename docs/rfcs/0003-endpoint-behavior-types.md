# RFC-0003 — Endpoint behavior types & provider model

- **Author:** kartaladev/msgin maintainers
- **Date:** 2026-07-22
- **Status:** Draft
- **Reviewers:** TBD

## 1. Summary

Give each composition endpoint's behavioral closure a **named type** (`FilterPredicate[A]`, `RoutingFunction`,
…) so expression support becomes a **provider of that type** passed into the one base constructor. This kills
the six parallel `*Expr` constructors and lets the pattern core shed its `expr-lang` dependency.

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

```go
type FilterPredicate[A any]  func(ctx context.Context, m Message[A]) (bool, error)
type RoutingFunction         func(ctx context.Context, m Message[any]) (MessageChannel, error)
type MessageTransformer[A,B any] func(ctx context.Context, m Message[A]) (Message[B], error)
type MessageSplitter[A,B any]    func(ctx context.Context, m Message[A]) ([]Message[B], error)
type CorrelationStrategy    func(m Message[any]) (string, error)   // already implicit
type ReleaseStrategy        func(g MessageGroup) bool              // already implicit
```

Base constructors just get typed (`func Filter[A any](p FilterPredicate[A], opts ...FilterOption) Step`).
**Naming a func type is backward-compatible** — a bare closure stays assignable — so introducing the types is
non-breaking.

Expr becomes a provider (in `endpoint/expr`, or its own module) that returns `(T, error)`:

```go
func Predicate[A any](s string) (endpoint.FilterPredicate[A], error)   // compiles once, fails at build
```

The compile error lives at the provider call, so the base `Filter` stays non-fallible and inline-composable,
and the "invalid expression fails at construction" contract is preserved.

### Examples

```go
p, err := expr.Predicate[Order]("payload.Amount > 100")
if err != nil { /* handle */ }
step := endpoint.Filter(p, endpoint.WithDiscardChannel(dlq))
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

1. Introduce the named types + type the base constructors — **non-breaking**, ship first. — S
2. Add the `endpoint/expr` provider package producing those types — additive. — M
3. Deprecate the core `*Expr` (thin shims over the provider), drop `expr-lang` from core deps; amend
   ADR 0019 — **breaking**, in the window. — S

### Timeline

Phase 1 anytime; phase 3 in the shared breaking window (see [index](README.md)).

### Success Metrics

`go list -deps` on the core package no longer includes `expr-lang`; `*Expr` API removed or shimmed; predicate
combinators (`And`/`Or`/`Not`) available.

## 6. Risks & Mitigations

| Risk | Impact | Mitigation |
|---|---|---|
| Wrong D1 choice (func vs interface) locks in a breaking change | Future churn | Decide interface+adapter now if introspection is wanted |
| Compile error leaks to per-message | Debuggability regression | Provider returns `(T, error)`; test build-time failure |
| Over-abstraction (a type per callback) | API bloat | Scope to the six args that have alternate providers |

## 7. Open Questions

1. Func type vs interface(+adapter) — the pivotal call.
2. `FilterPredicate` vs Spring's `MessageSelector` (coordinate with RFC-0002).
3. Expr as `endpoint/expr` subpackage vs own module (how far out to push the dep).
4. Remove `*Expr` outright, or keep deprecated shims?

## 8. Appendix

**Appendix A — endpoint → behavior type → provider map:** Filter→`FilterPredicate[A]`→`expr.Predicate`;
Router→`RoutingFunction`→`expr.RoutingFunc`; Transform→`MessageTransformer[A,B]`→`expr.Transformer`;
Split→`MessageSplitter[A,B]`→`expr.Splitter`; Aggregator correlation→`CorrelationStrategy`→`expr.Correlation`;
Aggregator release→`ReleaseStrategy`→`expr.Release`.
