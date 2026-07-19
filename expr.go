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
