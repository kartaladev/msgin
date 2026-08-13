package expr

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/expr-lang/expr"
	"github.com/expr-lang/expr/vm"
	"github.com/kartaladev/msgin"
)

// messageEnv is the environment a single-message expression evaluates against:
// payload (the typed A, whose fields are type-checked at compile time) and a
// header(key) function returning the header value (or nil). Struct tags expose
// lowercase names for SpEL-like ergonomics: payload.Field, header("key").
type messageEnv[A any] struct {
	Payload A                `expr:"payload"`
	Header  func(string) any `expr:"header"`
}

// outputKind is the compile-time constraint placed on an expression's result.
type outputKind int

const (
	// outputBool requires a bool result (predicates, release strategies).
	outputBool outputKind = iota
	// outputString requires a string result (routing keys, correlation keys).
	outputString
	// outputAny places no constraint; the caller asserts the result at eval.
	outputAny
)

// compile parses and type-checks expression once against messageEnv[A],
// enforcing the requested output kind, and returns a per-message evaluator. A
// nil/empty/unparseable/mistyped expression is ErrInvalidExpression naming the
// expression — surfaced at construction, never per message (ADR 0019).
//
// The returned evaluator wraps any runtime failure with the SOURCE EXPRESSION
// TEXT, which is the debuggability mitigation ADR 0029 §3 traded the interface
// shape for: an expression is opaque to a Go debugger, so the error string is
// the only place its identity can survive.
func compile[A any](expression string, kind outputKind) (func(msgin.Message[A]) (any, error), error) {
	if strings.TrimSpace(expression) == "" {
		return nil, fmt.Errorf("%w: empty expression", ErrInvalidExpression)
	}
	opts := []expr.Option{expr.Env(messageEnv[A]{})}
	switch kind {
	case outputString:
		opts = append(opts, expr.AsKind(reflect.String))
	case outputAny:
		// no output constraint; the caller asserts the result type at eval.
	default:
		opts = append(opts, expr.AsBool())
	}
	program, err := expr.Compile(expression, opts...)
	if err != nil {
		return nil, fmt.Errorf("%w %q: %w", ErrInvalidExpression, expression, err)
	}
	return func(msg msgin.Message[A]) (any, error) {
		out, err := vm.Run(program, messageEnv[A]{
			Payload: msg.Payload(),
			Header:  func(k string) any { v, _ := msg.Header(k); return v },
		})
		if err != nil {
			return nil, evalError(expression, err)
		}
		return out, nil
	}, nil
}

// evalError wraps a runtime evaluation failure with the source expression text.
// It is deliberately NOT ErrInvalidExpression: that sentinel is the
// construction-time contract, and a fault raised per message is the endpoint's
// ordinary handler error (see ErrInvalidExpression's godoc).
func evalError(expression string, err error) error {
	return fmt.Errorf("msgin/expr: evaluating %q: %w", expression, err)
}

// payloadError attaches the source expression to a msgin.PayloadOf failure.
//
// Every provider that asserts a message payload to A must route its failure
// through here rather than returning PayloadOf's error bare. A bare
// "msgin: payload is not of the expected type: want main.Order, got int" tells
// an operator reading a dead-letter record nothing about WHICH endpoint
// produced it — and with several expression-backed endpoints in one flow that
// is the only distinguishing information there is. Naming the expression is the
// debuggability mitigation ADR 0029 §3 traded the interface shape for, so it
// applies to the payload-assertion faults exactly as it does to evaluation
// faults.
//
// errors.Is(err, msgin.ErrPayloadType) still matches through the wrap, and
// msgin.IsPermanent still classifies it.
func payloadError(expression string, err error) error {
	return fmt.Errorf("%w (expression %q)", err, expression)
}

// resultTypeError reports that a compiled expression evaluated to a value of
// the wrong Go type. It reuses ROOT's msgin.ErrPayloadType rather than a
// provider-owned twin (decision D-K, ADR 0029 §5.0b): one errors.Is target is
// shared by every present and future expression provider, and root's
// IsPermanent already names that sentinel, so the fault is classified permanent
// with NO msgin.Permanent wrap. The literal "expr result" is the discriminator
// msgin.ErrPayloadType's godoc directs callers to match on.
func resultTypeError(expression string, got, want any) error {
	return fmt.Errorf("%w: expr result %T is not %T (expression %q)",
		msgin.ErrPayloadType, got, want, expression)
}

// stringResult reads a string-typed provider result (a routing key, a
// correlation key) out of an evaluated expression value.
//
// It extracts BY KIND, not by an exact `out.(string)` assertion, because
// outputString compiles with expr.AsKind(reflect.String) — a KIND constraint.
// A named string type (type Region string) therefore compiles and evaluates
// successfully, and an exact assertion would silently yield "": a routing key
// of "" misses every route and the message diverts to the default channel or
// ErrNoRoute with NO diagnostic, and a correlation key of "" is misreported as
// ErrNoCorrelation. Accepting the named type is the intended contract; the
// exact assertion was what was out of step.
//
// A non-string-kind result is still possible — header("k") is typed any, so
// AsKind cannot reject it at compile time — and is reported as
// msgin.ErrPayloadType via resultTypeError. A nil result lands here too:
// reflect.ValueOf(nil).Kind() is reflect.Invalid, so it takes the error branch
// rather than degrading into an empty key.
func stringResult(expression string, out any) (string, error) {
	rv := reflect.ValueOf(out)
	if rv.Kind() != reflect.String {
		return "", resultTypeError(expression, out, "")
	}
	return rv.String(), nil
}

// groupMember is one group member as seen by a group-scoped expression: the
// typed payload and a header(key) accessor, matching messageEnv's
// single-message shape (a raw msgin.Message[A] is unusable here — its Header
// accessor returns (any, bool), which expr cannot call, and its fields are
// unexported).
type groupMember[A any] struct {
	Payload A                `expr:"payload"`
	Header  func(string) any `expr:"header"`
}

// groupEnv is the environment a Release expression evaluates against: the
// members in arrival order and the group's declared size
// (msgin.HeaderSequenceSize, 0 if absent). E.g. len(messages) >= size,
// all(messages, .payload.Amount > 0).
type groupEnv[A any] struct {
	Messages []groupMember[A] `expr:"messages"`
	Size     int              `expr:"size"`
}

// compileGroup compiles a bool expression over groupEnv[A] once, mirroring
// compile but for the group-scoped environment. Empty, unparseable or non-bool
// is ErrInvalidExpression; a runtime failure carries the expression text.
func compileGroup[A any](expression string) (func(groupEnv[A]) (any, error), error) {
	if strings.TrimSpace(expression) == "" {
		return nil, fmt.Errorf("%w: empty expression", ErrInvalidExpression)
	}
	program, err := expr.Compile(expression, expr.Env(groupEnv[A]{}), expr.AsBool())
	if err != nil {
		return nil, fmt.Errorf("%w %q: %w", ErrInvalidExpression, expression, err)
	}
	return func(env groupEnv[A]) (any, error) {
		out, err := vm.Run(program, env)
		if err != nil {
			return nil, evalError(expression, err)
		}
		return out, nil
	}, nil
}

// toGroupEnv projects a msgin.MessageGroup into groupEnv[A]: each member's
// payload is re-asserted to A (a non-A member is msgin.ErrPayloadType) and its
// headers exposed via a per-member header() closure; size is the first member's
// msgin.HeaderSequenceSize, read number-tolerantly and 0 when absent.
func toGroupEnv[A any](expression string, g msgin.MessageGroup) (groupEnv[A], error) {
	if g == nil {
		// A nil group is CALLER INPUT here, not an internal state: Release
		// hands the caller a public routing.ReleaseStrategy value they hold and
		// may invoke directly, so expr.Release[Order]("…")(nil) must not
		// dereference a nil interface. It reads as the empty group below —
		// nothing to release — rather than as a fault.
		return groupEnv[A]{}, nil
	}
	msgs := g.Messages()
	if len(msgs) == 0 {
		// Guard BEFORE indexing msgs[0] below — an empty snapshot is a legal
		// group state, not a fault (it mirrors the Aggregator's own default
		// release strategy, which returns false for a group with no members).
		return groupEnv[A]{}, nil
	}
	members := make([]groupMember[A], len(msgs))
	for i, m := range msgs {
		tm, err := msgin.PayloadOf[A](m)
		if err != nil {
			return groupEnv[A]{}, payloadError(expression, err)
		}
		member := m // bound explicitly for the closure's clarity
		members[i] = groupMember[A]{
			Payload: tm.Payload(),
			Header:  func(k string) any { v, _ := member.Header(k); return v },
		}
	}
	size, _ := asInt(firstHeader(msgs, msgin.HeaderSequenceSize)) // 0 if absent/non-numeric
	return groupEnv[A]{Messages: members, Size: size}, nil
}

// firstHeader returns the raw header value for key on msgs' first member.
func firstHeader(msgs []msgin.Message[any], key string) any {
	v, _ := msgs[0].Header(key)
	return v
}

// asInt reads a header value as an int, tolerant of the numeric types a header
// may hold: a live in-process int/int64, or a float64 (the shape a number
// round-trips as through a JSON-framed sql store).
func asInt(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case float64:
		return int(n), true
	default:
		return 0, false
	}
}

// sliceToChildren reflects over a SplitFunc result: it must be a slice or
// array; each element is asserted to B and wrapped via msgin.WithPayload so the
// child inherits the parent's headers (routing.Split then stamps
// sequence/id/correlation). A non-slice result or a non-B element is
// msgin.ErrPayloadType (decision D-K).
func sliceToChildren[A, B any](expression string, out any, parent msgin.Message[A]) ([]msgin.Message[B], error) {
	rv := reflect.ValueOf(out)
	if rv.Kind() != reflect.Slice && rv.Kind() != reflect.Array {
		return nil, fmt.Errorf("%w: expr result %T is not a slice (expression %q)",
			msgin.ErrPayloadType, out, expression)
	}
	children := make([]msgin.Message[B], rv.Len())
	for i := range rv.Len() {
		elem := rv.Index(i).Interface()
		b, ok := elem.(B)
		if !ok {
			return nil, fmt.Errorf("%w: expr result element %d %T is not %T (expression %q)",
				msgin.ErrPayloadType, i, elem, *new(B), expression)
		}
		children[i] = msgin.WithPayload(parent, b)
	}
	return children, nil
}
