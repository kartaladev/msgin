package transform_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kartaladev/msgin"
	"github.com/kartaladev/msgin/transform"
)

func TestTransform(t *testing.T) {
	capture := func(got *msgin.Message[any]) msgin.MessageHandler {
		return msgin.HandlerFunc(func(_ context.Context, m msgin.Message[any]) error { *got = m; return nil })
	}
	tests := []struct {
		name   string
		fn     func(context.Context, msgin.Message[int]) (msgin.Message[string], error)
		input  any
		next   func(got *msgin.Message[any]) msgin.MessageHandler
		assert func(t *testing.T, err error, forwarded msgin.Message[any])
	}{
		{
			name: "maps A to B, forwards, preserves headers via WithPayload",
			fn: func(_ context.Context, m msgin.Message[int]) (msgin.Message[string], error) {
				return msgin.WithPayload(m, "n"), nil
			},
			input: 5,
			next:  capture,
			assert: func(t *testing.T, err error, forwarded msgin.Message[any]) {
				require.NoError(t, err)
				assert.Equal(t, "n", forwarded.Payload())
			},
		},
		{
			name: "wrong input payload type is ErrPayloadType",
			fn: func(_ context.Context, m msgin.Message[int]) (msgin.Message[string], error) {
				return msgin.WithPayload(m, ""), nil
			},
			input:  "not-int",
			next:   capture,
			assert: func(t *testing.T, err error, _ msgin.Message[any]) { assert.ErrorIs(t, err, msgin.ErrPayloadType) },
		},
		{
			name: "fn error propagates and nothing is forwarded",
			fn: func(context.Context, msgin.Message[int]) (msgin.Message[string], error) {
				return msgin.Message[string]{}, errors.New("boom")
			},
			input: 1,
			next:  capture,
			assert: func(t *testing.T, err error, forwarded msgin.Message[any]) {
				assert.ErrorContains(t, err, "boom")
				assert.Nil(t, forwarded.Payload())
			},
		},
		{
			name: "downstream (next) error propagates",
			fn: func(_ context.Context, m msgin.Message[int]) (msgin.Message[string], error) {
				return msgin.WithPayload(m, "ok"), nil
			},
			input: 1,
			next: func(*msgin.Message[any]) msgin.MessageHandler {
				return msgin.HandlerFunc(func(context.Context, msgin.Message[any]) error { return errors.New("downstream") })
			},
			assert: func(t *testing.T, err error, _ msgin.Message[any]) { assert.ErrorContains(t, err, "downstream") },
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var forwarded msgin.Message[any]
			step := transform.Transform(tc.fn)
			tc.assert(t, step(tc.next(&forwarded)).Handle(t.Context(), msgin.New[any](tc.input)), forwarded)
		})
	}
}

// xformShapes supplies the accepted call shapes for Transformer that a closure
// literal cannot express (method value, plain declaration, returned unnamed).
type xformShapes struct{}

func (xformShapes) to(_ context.Context, m msgin.Message[int]) (msgin.Message[string], error) {
	return msgin.WithPayload(m, "ok"), nil
}

func xformDecl(_ context.Context, m msgin.Message[int]) (msgin.Message[string], error) {
	return msgin.WithPayload(m, "ok"), nil
}

func xformReturned() func(context.Context, msgin.Message[int]) (msgin.Message[string], error) {
	return xformDecl
}

// TestTransformerType_AcceptedCallShapes is the compile-level statement of
// WHICH call shapes Transformer accepts — the precise property, not the
// over-broad "source-compatible".
//
// ACCEPTED, and pinned here: a bare closure literal (inferring A and B), a
// variable of the equivalent UNNAMED func type, a func returning that unnamed
// type, a method value, a plain top-level func declaration, and a value of
// msgin's OWN named Transformer.
//
// REJECTED, and therefore NOT pinned here — it cannot be, since a test that
// asserts it would not compile: a caller's OWN NAMED func type. Go converts
// implicitly between func types only when at least one side is unnamed, so
// `type MyXform func(...); transform.Transform(x)` fails, and because
// Transformer is generic it fails as an opaque inference error ("cannot infer A
// and B"), not a plain assignability error. Measured against a throwaway
// package; the remedy is an explicit conversion at the call site, which the
// last shape below is exactly. See the ASSIGNABILITY paragraph on
// [transform.Transformer].
func TestTransformerType_AcceptedCallShapes(t *testing.T) {
	t.Parallel()

	next := msgin.HandlerFunc(func(context.Context, msgin.Message[any]) error { return nil })
	// A variable whose type is the UNNAMED func type (inferred from a plain
	// declaration, which has exactly that type).
	unnamedVar := xformDecl
	var h xformShapes

	steps := map[string]msgin.Step{
		"bare closure literal": transform.Transform(func(_ context.Context, m msgin.Message[int]) (msgin.Message[string], error) {
			return msgin.WithPayload(m, "ok"), nil
		}),
		"unnamed-type variable":  transform.Transform(unnamedVar),
		"func returning unnamed": transform.Transform(xformReturned()),
		"method value":           transform.Transform(h.to),
		"plain func declaration": transform.Transform(xformDecl),
		"msgin's named type":     transform.Transform(transform.Transformer[int, string](xformDecl)),
	}
	for name, step := range steps {
		t.Run(name, func(t *testing.T) {
			require.NoError(t, step(next).Handle(t.Context(), msgin.New[any](1)))
		})
	}
}

func TestTransform_NilFn(t *testing.T) {
	step := transform.Transform[int, int](nil)
	next := msgin.HandlerFunc(func(context.Context, msgin.Message[any]) error { return nil })
	assert.ErrorIs(t, step(next).Handle(t.Context(), msgin.New[any](1)), msgin.ErrNilFunc)
}
