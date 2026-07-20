package msgin

import (
	"context"
	"fmt"
	"reflect"
	"strings"

	"github.com/expr-lang/expr"
	"github.com/expr-lang/expr/vm"
)

// exprEnv is the environment an expression evaluates against: payload (the typed
// A, whose fields are type-checked at compile time) and a header(key) function
// returning the header value (or nil). Struct tags expose lowercase names for
// SpEL-like ergonomics: payload.Field, header("key").
type exprEnv[A any] struct {
	Payload A                `expr:"payload"`
	Header  func(string) any `expr:"header"`
}

type exprOutputKind int

const (
	exprBool exprOutputKind = iota
	exprString
	exprAny // no AsBool/AsKind constraint; the result is asserted by the caller
)

// compile parses and type-checks expression once against exprEnv[A], enforcing the
// requested output kind (bool for predicates, string for routing keys), and
// returns a per-message evaluator. A nil/empty/unparseable/mistyped expression is
// ErrInvalidExpression (naming the expression) — surfaced at construction, never
// per message.
func compile[A any](expression string, kind exprOutputKind) (func(Message[A]) (any, error), error) {
	if strings.TrimSpace(expression) == "" {
		return nil, fmt.Errorf("%w: empty expression", ErrInvalidExpression)
	}
	opts := []expr.Option{expr.Env(exprEnv[A]{})}
	switch kind {
	case exprString:
		opts = append(opts, expr.AsKind(reflect.String))
	case exprAny:
		// no output constraint; caller asserts the result type at eval
	default:
		opts = append(opts, expr.AsBool())
	}
	program, err := expr.Compile(expression, opts...)
	if err != nil {
		return nil, fmt.Errorf("%w %q: %w", ErrInvalidExpression, expression, err)
	}
	return func(msg Message[A]) (any, error) {
		return vm.Run(program, exprEnv[A]{
			Payload: msg.Payload(),
			Header:  func(k string) any { v, _ := msg.Header(k); return v },
		})
	}, nil
}

// FilterExpr is a Message Filter (see Filter) whose bool predicate is a runtime
// expr-lang expression evaluated against the payload (A) and headers, e.g.
// `payload.Amount > 100 && header("region") == "EU"`. The expression is compiled
// and type-checked once; an invalid or non-bool expression returns
// ErrInvalidExpression at construction. It returns the same Step as Filter and
// honors WithDiscardChannel.
//
// Because it returns (Step, error), it cannot be passed inline to Chain like a
// bare Filter — construct it first, check the error, then compose (see the
// example).
//
// Trade-offs vs a Go-func Filter:
//   - Type safety: a string typo or type mismatch is a construction error, not a
//     build error. payload field access is type-checked against A ONLY when A is a
//     concrete (non-interface) type; with A = any there is no compile-time check
//     and errors surface at evaluation.
//   - Debuggability: an expression is opaque to a Go debugger. Prefer Filter for
//     compile-time predicates.
//   - header("k") returns an untyped value (any); a missing header is nil, so an
//     ordering/typed comparison on an absent or wrong-typed header is a runtime
//     evaluation error (returned as the handler error, into retry/DLQ).
//
// Security: expr enforces safe defaults — a compile-time AST node cap
// (MaxNodes = 1e4) and a runtime MemoryBudget (1e6) — which reject the usual
// abuse vectors (huge ranges, repeat, deep nesting). It is therefore suitable for
// operator/config-authored expressions. Caveat: there is NO time budget and
// evaluation is NOT context-cancellable (expr's vm.Run takes no context), so keep
// operator expressions bounded; do not evaluate arbitrary untrusted end-user
// input without your own additional limits.
func FilterExpr[A any](expression string, opts ...FilterOption) (Step, error) {
	eval, err := compile[A](expression, exprBool)
	if err != nil {
		return nil, err
	}
	return Filter(func(_ context.Context, m Message[A]) (bool, error) {
		out, err := eval(m)
		if err != nil {
			return false, err
		}
		b, _ := out.(bool) // AsBool guarantees a bool result
		return b, nil
	}, opts...), nil
}

// RouterExpr is a content-based Router (see NewRouter) whose channel is chosen by
// a routing-key expression: keyExpr is compiled to a string key and the message
// routes to routes[key], falling back to WithDefaultChannel or ErrNoRoute. An
// empty/nil routes map, a nil channel value in routes, or a bad keyExpr is
// ErrInvalidExpression at construction. The nil-channel check catches only a
// literal nil interface value in routes; a typed-nil MessageChannel implementation
// passes construction and fails at Send instead. Mirrors Spring Integration's
// <router expression="…">; a ternary key (`payload.Amount >= 100 ? "big" : "small"`)
// gives first-match multi-way routing in one construct. Same trade-offs and
// security posture as FilterExpr (type-safety only for concrete A, no Go stack
// trace in an expression, expr's default node/memory limits, no ctx-cancellation).
func RouterExpr[A any](keyExpr string, routes map[string]MessageChannel, opts ...RouterOption) (*Router, error) {
	if len(routes) == 0 {
		return nil, fmt.Errorf("%w: empty routes map", ErrInvalidExpression)
	}
	for k, ch := range routes { // fail fast on a literal-nil channel value (audit L2).
		if ch == nil { // NB: catches a nil interface, not a typed-nil sink — that still fails at Send.
			return nil, fmt.Errorf("%w: nil channel for route %q", ErrInvalidExpression, k)
		}
	}
	eval, err := compile[A](keyExpr, exprString)
	if err != nil {
		return nil, err
	}
	return NewRouter(func(_ context.Context, m Message[any]) (MessageChannel, error) {
		in, err := PayloadOf[A](m)
		if err != nil {
			return nil, err
		}
		out, err := eval(in)
		if err != nil {
			return nil, err
		}
		key, _ := out.(string)  // AsKind(String) guarantees a string result
		return routes[key], nil // miss → nil → NewRouter's default/ErrNoRoute handling
	}, opts...), nil
}

// TransformExpr is a Transformer (see Transform) whose projection is a runtime
// expr-lang expression evaluated against the payload (A) and headers, e.g.
// `payload.Amount * 2`. The expression is compiled once at construction; an
// invalid or unparseable expression returns ErrInvalidExpression. At each
// evaluation the result is asserted to B; a mismatch returns ErrExprResultType
// as the endpoint's handler error (into the runtime's retry/DLQ path) rather
// than panicking. It returns the same Step as Transform, and the child message
// carries the parent's headers via WithPayload (id/correlation propagation).
//
// Because it returns (Step, error), it cannot be passed inline to Chain like a
// bare Transform — construct it first, check the error, then compose.
//
// Trade-offs vs a Go-func Transform: same as FilterExpr (type safety only for
// concrete A, opaque to a Go debugger, expr's default node/memory limits, no
// ctx-cancellation) — see FilterExpr's godoc.
//
// Result-type ceilings on B:
//   - Struct ceiling: expr cannot construct an arbitrary Go struct; B is
//     realistically a scalar/slice/map/named-field type — use Transform for
//     struct projection.
//   - Numeric ceiling: expr integer math yields Go int and float math yields
//     float64; out.(B) is an exact type assertion, so B must be exactly
//     int/float64/string/bool or a concrete element type — an int64/uint/
//     float32 B fails at eval with ErrExprResultType even though the
//     expression's value is numerically representable in B.
func TransformExpr[A, B any](expression string) (Step, error) {
	eval, err := compile[A](expression, exprAny)
	if err != nil {
		return nil, err
	}
	return Transform(func(_ context.Context, m Message[A]) (Message[B], error) {
		out, err := eval(m)
		if err != nil {
			return Message[B]{}, err
		}
		b, ok := out.(B)
		if !ok {
			return Message[B]{}, fmt.Errorf("%w: result %T is not %T", ErrExprResultType, out, *new(B))
		}
		return WithPayload(m, b), nil
	}), nil
}
