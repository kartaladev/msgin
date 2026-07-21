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

// SplitExpr is a Splitter endpoint (see Split) whose fan-out is a runtime
// expr-lang expression evaluated against the payload (A) and headers, e.g.
// `payload.Items` or `filter(payload.Items, {.Amount > 100})`. The expression
// is compiled once at construction; an invalid or unparseable expression
// returns ErrInvalidExpression. At each evaluation the result must be a
// slice/array and each element is asserted to B; a non-slice result or a
// non-B element returns ErrExprResultType as the endpoint's handler error
// (into the runtime's retry/DLQ path) rather than panicking. An empty slice
// forwards nothing (a valid "nothing to split", like Split). Each child
// inherits the parent's headers via WithPayload and is then stamped for
// reassembly exactly as Split stamps its children — HeaderSequenceNumber
// (1-based), HeaderSequenceSize, a deterministic id, and correlation — so a
// SplitExpr->Aggregator round-trip reassembles with no extra configuration.
//
// Because it returns (Step, error), it cannot be passed inline to Chain like
// a bare Split — construct it first, check the error, then compose.
//
// SplitExpr takes two type parameters ([A, B]) where FilterExpr/TransformExpr
// take one (or two): A is the input payload the expression evaluates against,
// B is the per-element type each result entry is asserted to. Trade-offs vs a
// Go-func Split: same as FilterExpr (type safety only for concrete A, opaque
// to a Go debugger, expr's default node/memory limits, no ctx-cancellation) —
// see FilterExpr's godoc.
//
// Result-type ceilings on B: expr's filter/map builtins yield []interface{}
// (each element boxed as any), while a direct field like payload.Items yields
// the concrete []T — exprSliceToChildren handles both via reflection, but the
// exact-type assertion (elem.(B)) still applies per element, so it carries the
// same struct/numeric ceilings as TransformExpr's B (see TransformExpr's
// godoc) — B is realistically a scalar/slice/map/named-field type or a struct
// element already of the exact B type; a mismatched box (e.g. an int64
// element asserted to B=int) is ErrExprResultType.
func SplitExpr[A, B any](expression string) (Step, error) {
	eval, err := compile[A](expression, exprAny)
	if err != nil {
		return nil, err
	}
	return func(next MessageHandler) MessageHandler {
		return HandlerFunc(func(ctx context.Context, msg Message[any]) error {
			in, err := PayloadOf[A](msg)
			if err != nil {
				return err
			}
			out, err := eval(in)
			if err != nil {
				return err
			}
			children, err := exprSliceToChildren[A, B](out, in)
			if err != nil {
				return err
			}
			return forwardSplit(ctx, next, msg, children)
		})
	}, nil
}

// exprMember is one group member as seen by a group-scoped expression: the
// typed payload and a header(key) accessor, matching exprEnv's single-message
// shape (a raw Message[A] is unusable here — its Header accessor returns
// (any, bool), which expr cannot call, and its fields are unexported).
type exprMember[A any] struct {
	Payload A                `expr:"payload"`
	Header  func(string) any `expr:"header"`
}

// groupExprEnv is the environment a WithReleaseExpr expression evaluates
// against: the members in arrival order and the group's declared size
// (HeaderSequenceSize, 0 if absent). E.g. len(messages) >= size,
// all(messages, .payload.Amount > 0).
type groupExprEnv[A any] struct {
	Messages []exprMember[A] `expr:"messages"`
	Size     int             `expr:"size"`
}

// compileGroup compiles a bool expression over groupExprEnv[A] once,
// mirroring compile but for the group-scoped env. Empty/unparseable/non-bool
// → ErrInvalidExpression.
func compileGroup[A any](expression string) (func(groupExprEnv[A]) (any, error), error) {
	if strings.TrimSpace(expression) == "" {
		return nil, fmt.Errorf("%w: empty expression", ErrInvalidExpression)
	}
	program, err := expr.Compile(expression, expr.Env(groupExprEnv[A]{}), expr.AsBool())
	if err != nil {
		return nil, fmt.Errorf("%w %q: %w", ErrInvalidExpression, expression, err)
	}
	return func(env groupExprEnv[A]) (any, error) { return vm.Run(program, env) }, nil
}

// toGroupEnv projects a MessageGroup into groupExprEnv[A]: each member's
// payload is re-asserted to A (a non-A member → ErrPayloadType) and its
// headers exposed via a per-member header() closure; size is the first
// member's HeaderSequenceSize.
func toGroupEnv[A any](g MessageGroup) (groupExprEnv[A], error) {
	msgs := g.Messages()
	if len(msgs) == 0 {
		return groupExprEnv[A]{}, nil // M-1: guard before firstHeader (mirrors defaultRelease's len==0 guard)
	}
	members := make([]exprMember[A], len(msgs))
	for i, m := range msgs {
		tm, err := PayloadOf[A](m)
		if err != nil {
			return groupExprEnv[A]{}, err // non-A member → ErrPayloadType (M-6: tested via a fake store)
		}
		member := m // Go 1.25 per-iteration var; bound explicitly for the closure's clarity
		members[i] = exprMember[A]{
			Payload: tm.Payload(),
			Header:  func(k string) any { v, _ := member.Header(k); return v },
		}
	}
	size, _ := asInt(firstHeader(msgs, HeaderSequenceSize)) // 0 if absent/non-numeric
	return groupExprEnv[A]{Messages: members, Size: size}, nil
}

// WithCorrelationExpr overrides an Aggregator's correlation strategy with a
// runtime expr-lang expression evaluated against the payload (A) and headers
// — the same environment FilterExpr/TransformExpr use — e.g.
// `header("tenant")` or `payload.Region`. It reuses the exprString compile
// path (ADR 0019 §3/§6). The expression is compiled once, eagerly, when this
// option is constructed; a nil/empty/unparseable/non-string expression does
// NOT return an error here (an AggregatorOption cannot) — it is stashed and
// surfaced by NewAggregator as ErrInvalidExpression, taking precedence over
// ErrNilOutput/ErrExpiryChannelRequired (L-5, deliberate: the expr fault is
// the more specific misconfiguration). If more than one expr option fails to
// compile, the FIRST one applied wins (aggregatorConfig.optErr).
//
// At each Handle call: the message is asserted to A, the expression
// evaluates to the correlation key, and — mirroring defaultCorrelate's
// empty-key handling (L-1) — an empty evaluated key is
// Permanent(ErrNoCorrelation) rather than a silently-empty group key. A
// runtime evaluation error (e.g. an ordering/typed comparison on a missing
// header) propagates as Handle's error, into the runtime's retry/DLQ path,
// exactly like FilterExpr's predicate.
//
// Same trade-offs and security posture as FilterExpr: type-safety only for a
// concrete A, opaque to a Go debugger, expr's default node/memory caps, no
// ctx-cancellation — see FilterExpr's godoc.
func WithCorrelationExpr[A any](expression string) AggregatorOption {
	eval, err := compile[A](expression, exprString)
	return func(c *aggregatorConfig) {
		if err != nil {
			if c.optErr == nil {
				c.optErr = err
			}
			return
		}
		c.correlate = func(m Message[any]) (string, error) {
			in, perr := PayloadOf[A](m)
			if perr != nil {
				return "", perr
			}
			out, eerr := eval(in)
			if eerr != nil {
				return "", eerr // raw eval error (retried), consistent with FilterExpr (L-2)
			}
			key, _ := out.(string) // AsKind(String) guarantees a string
			if key == "" {
				return "", Permanent(ErrNoCorrelation) // L-1: symmetry with defaultCorrelate
			}
			return key, nil
		}
	}
}

// WithReleaseExpr overrides an Aggregator's release strategy with a runtime
// expr-lang expression evaluated over the group-scoped environment
// {messages []{payload, header(k)}, size int} — messages is the group's
// members in arrival order (each exposing payload and header(k), the same
// shape exprEnv uses per-message); size is the first member's
// HeaderSequenceSize, 0 if absent. Because size is 0 when absent (M-4), the
// canonical release form is size-GATED — `size > 0 && len(messages) >=
// size` — the un-gated `len(messages) >= size` releases a group-of-one the
// moment no HeaderSequenceSize header is present, a documented footgun.
//
// The expression is compiled once, eagerly, when this option is
// constructed; a nil/empty/unparseable/non-bool expression does NOT return
// an error here (an AggregatorOption cannot) — it is stashed and surfaced by
// NewAggregator as ErrInvalidExpression, taking precedence over
// ErrNilOutput/ErrExpiryChannelRequired (L-5, deliberate). If more than one
// expr option fails to compile, the FIRST one applied wins
// (aggregatorConfig.optErr).
//
// A runtime evaluation error propagates through Handle into the runtime's
// retry/DLQ path (D14c). A group that is PERSISTENTLY unevaluable is never
// silently stranded: pair WithReleaseExpr with WithGroupTimeout so the
// reaper's expiry fall-through surfaces it to WithExpiredGroupChannel once it
// ages out — without WithGroupTimeout (and a non-durable store,
// RecoverInterval()==0), such a group is HELD, not lost, bounded only by
// store capacity (L-7). With WithGroupTimeout set, a poison member may
// appear at BOTH the DLQ (via Handle's retry-exhaustion of the member that
// triggered the error) and the expired channel (the reaper routes the whole
// aged group there) — no double-emit on the output side, no loss, within the
// at-least-once contract (L-8).
//
// Poison-member caveat (M-5): unlike the O(1) size-header defaultRelease
// (which reads only the first member's size header), a release expression
// evaluates over ALL members, so one member whose data makes the expression
// error poisons the whole group's release check — every subsequent member on
// that key retries/DLQs until expiry surfaces the group. Write TOTAL
// expressions (guard nil/typed headers) and prefer the Go-func
// WithReleaseStrategy for large groups: WithReleaseExpr is O(n) per Add
// (O(n²) over a group's lifetime) vs. the default's O(1) (L-3).
//
// Same trade-offs and security posture as FilterExpr: type-safety only for a
// concrete A, opaque to a Go debugger, expr's default node/memory caps, no
// ctx-cancellation — see FilterExpr's godoc.
func WithReleaseExpr[A any](expression string) AggregatorOption {
	eval, err := compileGroup[A](expression)
	return func(c *aggregatorConfig) {
		if err != nil {
			if c.optErr == nil {
				c.optErr = err
			}
			return
		}
		c.release = func(g MessageGroup) (bool, error) {
			env, berr := toGroupEnv[A](g)
			if berr != nil {
				return false, berr
			}
			out, eerr := eval(env)
			if eerr != nil {
				return false, eerr
			}
			b, _ := out.(bool) // AsBool guarantees a bool
			return b, nil
		}
	}
}

// exprSliceToChildren reflects over a SplitExpr result: it must be a slice/array;
// each element is asserted to B and wrapped via WithPayload(parent, elem) so the
// child inherits the parent's headers (Split then stamps sequence/id/correlation).
// A non-slice result or a non-B element is ErrExprResultType.
func exprSliceToChildren[A, B any](out any, parent Message[A]) ([]Message[B], error) {
	rv := reflect.ValueOf(out)
	if rv.Kind() != reflect.Slice && rv.Kind() != reflect.Array {
		return nil, fmt.Errorf("%w: SplitExpr result %T is not a slice", ErrExprResultType, out)
	}
	children := make([]Message[B], rv.Len())
	for i := 0; i < rv.Len(); i++ {
		elem := rv.Index(i).Interface()
		b, ok := elem.(B)
		if !ok {
			return nil, fmt.Errorf("%w: SplitExpr element %d %T is not %T", ErrExprResultType, i, elem, *new(B))
		}
		children[i] = WithPayload(parent, b)
	}
	return children, nil
}
