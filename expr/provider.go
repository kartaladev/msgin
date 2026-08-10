package expr

import (
	"context"
	"fmt"

	"github.com/kartaladev/msgin"
	"github.com/kartaladev/msgin/routing"
	"github.com/kartaladev/msgin/transform"
)

// Predicate compiles a runtime expr-lang expression into a
// [routing.Predicate], the bool decision behind a Message Filter. The
// expression is evaluated against the payload (A) and the message's headers,
// e.g. `payload.Amount > 100 && header("region") == "EU"`.
//
// It is compiled and type-checked ONCE, here: an empty, unparseable or non-bool
// expression returns [ErrInvalidExpression] at construction, never at the first
// message (ADR 0019). Pass the result to [routing.Filter] like any Go-func
// predicate; options such as [routing.WithDiscardChannel] go to Filter, not
// here.
//
// A IS NOT INFERABLE from a string argument, so instantiate it explicitly —
// expr.Predicate[Order]("…"). A caller who writes expr.Predicate("…") gets an
// unhelpful "cannot infer A" error rather than a message about the payload
// type.
//
// Trade-offs against a Go-func predicate:
//   - Type safety: a typo or a type mismatch is a construction ERROR, not a
//     build error. payload field access is type-checked against A only when A
//     is a concrete (non-interface) type; with A = any there is no
//     compile-time check and the fault surfaces at evaluation.
//   - Debuggability: an expression is opaque to a Go debugger. Every runtime
//     failure therefore carries the source expression text, which is the only
//     identity it has left. Prefer a Go func where the predicate is known at
//     build time.
//   - header("k") returns an untyped value (any) and a missing header is nil,
//     so an ordering or typed comparison against an absent header is a runtime
//     evaluation error, returned as the endpoint's handler error.
//
// Security: expr enforces safe defaults — a compile-time AST node cap
// (MaxNodes = 1e4) and a runtime MemoryBudget (1e6) — which reject the usual
// abuse vectors (huge ranges, repeat, deep nesting), so it is suitable for
// operator- or config-authored expressions. Caveat: there is NO time budget and
// evaluation is NOT context-cancellable (expr's vm.Run takes no context), so
// keep operator expressions bounded and do not evaluate arbitrary untrusted
// end-user input without your own additional limits.
func Predicate[A any](expression string) (routing.Predicate[A], error) {
	eval, err := compile[A](expression, outputBool)
	if err != nil {
		return nil, err
	}
	return func(_ context.Context, m msgin.Message[A]) (bool, error) {
		out, err := eval(m)
		if err != nil {
			return false, err
		}
		b, _ := out.(bool) // AsBool guarantees a bool result
		return b, nil
	}, nil
}

// RouteFunc compiles a runtime routing-key expression into a
// [routing.RouteFunc], the destination choice behind a Content-Based Router.
// The expression evaluates to a string key and the message routes to
// routes[key]; a miss returns a nil channel, which [routing.NewRouter] absorbs
// via [routing.WithDefaultChannel] or reports as [msgin.ErrNoRoute]. A ternary
// key (`payload.Amount >= 100 ? "big" : "small"`) gives first-match multi-way
// routing in one construct.
//
// It carries TWO construction validations of its own, beyond compiling the
// expression, and both return [ErrInvalidExpression]:
//
//  1. routes must be non-empty — a router with nowhere to route is a
//     misconfiguration, not a message that happens to miss.
//  2. no route may map to a nil channel. This catches a literal nil interface
//     value only; a TYPED-nil msgin.MessageChannel implementation passes
//     construction and fails at Send instead.
//
// A IS NOT INFERABLE from a string argument, so instantiate it explicitly —
// expr.RouteFunc[Order]("…", routes). Note the routes map is a second
// parameter: this provider's shape is deliberately not uniform with the others.
//
// A non-A payload is [msgin.ErrPayloadType]; an evaluation failure carries the
// source expression text. Same trade-offs and security posture as [Predicate].
func RouteFunc[A any](expression string, routes map[string]msgin.MessageChannel) (routing.RouteFunc, error) {
	if len(routes) == 0 {
		return nil, fmt.Errorf("%w: empty routes map", ErrInvalidExpression)
	}
	for k, ch := range routes {
		// Fail fast on a literal-nil channel value. NB: this catches a nil
		// interface, not a typed-nil sink — that still fails at Send.
		if ch == nil {
			return nil, fmt.Errorf("%w: nil channel for route %q", ErrInvalidExpression, k)
		}
	}
	eval, err := compile[A](expression, outputString)
	if err != nil {
		return nil, err
	}
	return func(_ context.Context, m msgin.Message[any]) (msgin.MessageChannel, error) {
		in, err := msgin.PayloadOf[A](m)
		if err != nil {
			return nil, payloadError(expression, err)
		}
		out, err := eval(in)
		if err != nil {
			return nil, err
		}
		key, _ := out.(string)  // AsKind(String) guarantees a string result
		return routes[key], nil // miss -> nil -> NewRouter's default/ErrNoRoute handling
	}, nil
}

// Transformer compiles a runtime expr-lang expression into a
// [transform.Transformer], the projection behind a Message Translator, e.g.
// `payload.Amount * 2`. The child message carries the parent's headers via
// [msgin.WithPayload], so id and correlation propagate.
//
// The expression is compiled once here; an empty or unparseable expression is
// [ErrInvalidExpression] at construction. At each evaluation the result is
// asserted to B, and a mismatch is [msgin.ErrPayloadType] wrapped as
// "expr result %T is not %T" (decision D-K) — root's IsPermanent already names
// that sentinel, so the fault is never retried and needs no [msgin.Permanent]
// wrap.
//
// A AND B ARE NOT INFERABLE from a string argument, so instantiate both
// explicitly — expr.Transformer[Order, int]("…").
//
// Result-type ceilings on B:
//   - Struct ceiling: expr cannot construct an arbitrary Go struct, so B is
//     realistically a scalar, slice, map or named-field type. Use a Go-func
//     [transform.Transform] for struct projection.
//   - Numeric ceiling: expr integer math yields Go int and float math yields
//     float64, and the assertion to B is EXACT — so B must be exactly
//     int/float64/string/bool or a concrete element type. An int64, uint or
//     float32 B fails at evaluation with msgin.ErrPayloadType even though the
//     expression's value is numerically representable in B.
//
// Same trade-offs and security posture as [Predicate].
func Transformer[A, B any](expression string) (transform.Transformer[A, B], error) {
	eval, err := compile[A](expression, outputAny)
	if err != nil {
		return nil, err
	}
	return func(_ context.Context, m msgin.Message[A]) (msgin.Message[B], error) {
		out, err := eval(m)
		if err != nil {
			return msgin.Message[B]{}, err
		}
		b, ok := out.(B)
		if !ok {
			return msgin.Message[B]{}, resultTypeError(expression, out, *new(B))
		}
		return msgin.WithPayload(m, b), nil
	}, nil
}

// SplitFunc compiles a runtime expr-lang expression into a [routing.SplitFunc],
// the fan-out behind a Splitter, e.g. `payload.Items` or
// `filter(payload.Items, {.Amount > 100})`.
//
// The expression is compiled once here; an empty or unparseable expression is
// [ErrInvalidExpression] at construction. At each evaluation the result must be
// a slice or array and each element is asserted to B; a non-slice result or a
// non-B element is [msgin.ErrPayloadType] (decision D-K) rather than a panic.
// An empty slice forwards nothing — a valid "nothing to split". Each child
// inherits the parent's headers via [msgin.WithPayload], and [routing.Split]
// then stamps the reassembly headers, so a SplitFunc -> Aggregator round-trip
// needs no extra configuration.
//
// A AND B ARE NOT INFERABLE from a string argument, so instantiate both
// explicitly — expr.SplitFunc[Batch, Item]("…"). A is the input payload the
// expression evaluates against; B is the per-element type each result entry is
// asserted to.
//
// Result-type ceilings on B: expr's filter/map builtins yield []interface{}
// (each element boxed as any) while a direct field like payload.Items yields
// the concrete []T — both are handled — but the exact-type assertion still
// applies per element, so B carries the same struct and numeric ceilings as
// [Transformer]'s B. A mismatched box (an int64 element asserted to B = int) is
// msgin.ErrPayloadType.
//
// Same trade-offs and security posture as [Predicate].
func SplitFunc[A, B any](expression string) (routing.SplitFunc[A, B], error) {
	eval, err := compile[A](expression, outputAny)
	if err != nil {
		return nil, err
	}
	return func(_ context.Context, m msgin.Message[A]) ([]msgin.Message[B], error) {
		out, err := eval(m)
		if err != nil {
			return nil, err
		}
		return sliceToChildren[A, B](expression, out, m)
	}, nil
}

// Correlation compiles a runtime expr-lang expression into a
// [routing.CorrelationStrategy], the group key behind an Aggregator, e.g.
// `header("tenant")` or `payload.Region`. It evaluates against the same
// per-message environment [Predicate] uses and must yield a string; an empty,
// unparseable or non-string expression is [ErrInvalidExpression] at
// construction. Pass the result to [routing.WithCorrelationStrategy].
//
// A IS NOT INFERABLE from a string argument, so instantiate it explicitly —
// expr.Correlation[Order]("…").
//
// At each call the message is asserted to A (a non-A payload is
// [msgin.ErrPayloadType]) and the expression evaluated. An EMPTY evaluated key
// is Permanent([msgin.ErrNoCorrelation]) rather than a silently-empty group
// key, mirroring the Aggregator's own default strategy: a message that cannot
// be correlated will not become correlatable on redelivery. An evaluation
// failure propagates as Handle's error, carrying the source expression text,
// and is retried like any other transient handler fault.
//
// Same trade-offs and security posture as [Predicate].
func Correlation[A any](expression string) (routing.CorrelationStrategy, error) {
	eval, err := compile[A](expression, outputString)
	if err != nil {
		return nil, err
	}
	return func(m msgin.Message[any]) (string, error) {
		in, err := msgin.PayloadOf[A](m)
		if err != nil {
			return "", payloadError(expression, err)
		}
		out, err := eval(in)
		if err != nil {
			return "", err
		}
		key, _ := out.(string) // AsKind(String) guarantees a string
		if key == "" {
			return "", msgin.Permanent(msgin.ErrNoCorrelation)
		}
		return key, nil
	}, nil
}

// Release compiles a runtime expr-lang expression into a
// [routing.ReleaseStrategy], the completeness decision behind an Aggregator.
// The expression evaluates over the GROUP-scoped environment
// {messages []{payload, header(k)}, size int}: messages holds the group's
// members in arrival order, each exposing payload and header(k) exactly as the
// per-message environment does, and size is the first member's
// [msgin.HeaderSequenceSize], 0 when absent. Pass the result to
// [routing.WithReleaseStrategy].
//
// BECAUSE size IS 0 WHEN ABSENT, the canonical release form is size-GATED —
// `size > 0 && len(messages) >= size`. The un-gated `len(messages) >= size`
// releases a group-of-one the moment no HeaderSequenceSize header is present; it
// is a documented footgun, not a shorthand.
//
// A IS NOT INFERABLE from a string argument, so instantiate it explicitly —
// expr.Release[Order]("…").
//
// ReleaseStrategy's (bool, error) shape is what lets an evaluation failure
// PROPAGATE instead of collapsing into a permanent false (decision D-E): an
// empty or unparseable expression is [ErrInvalidExpression] here at
// construction, a member whose payload is not A is [msgin.ErrPayloadType], and
// an evaluation failure carries the source expression text out through Handle
// into the runtime's retry/dead-letter path. An EMPTY group snapshot evaluates
// against a zero environment (no members, size 0) rather than erroring.
//
// Poison-member caveat: a release expression evaluates over ALL members, so one
// member whose data makes the expression fail poisons the whole group's release
// check — every subsequent member on that key fails until the group ages out.
// Write TOTAL expressions (guard nil and typed headers), and prefer a Go-func
// [routing.WithReleaseStrategy] for large groups: this is O(n) per Add, where
// the Aggregator's size-header default is O(1). Pair it with
// [routing.WithGroupTimeout] so a persistently unevaluable group is surfaced to
// [routing.WithExpiredGroupChannel] rather than held.
//
// Same trade-offs and security posture as [Predicate].
func Release[A any](expression string) (routing.ReleaseStrategy, error) {
	eval, err := compileGroup[A](expression)
	if err != nil {
		return nil, err
	}
	return func(g msgin.MessageGroup) (bool, error) {
		env, err := toGroupEnv[A](expression, g)
		if err != nil {
			return false, err
		}
		out, err := eval(env)
		if err != nil {
			return false, err
		}
		b, _ := out.(bool) // AsBool guarantees a bool
		return b, nil
	}, nil
}
