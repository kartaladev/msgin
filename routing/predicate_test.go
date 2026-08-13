package routing_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kartaladev/msgin"
	"github.com/kartaladev/msgin/routing"
)

// TestPredicateCombinators covers every hot-path branch of And/Or/Not,
// including the two traps a naive implementation falls into: a short-circuit
// that hides a nil operand (Or with a true left, And with a false left), and a
// Not that inverts an error result instead of propagating it.
//
// Every nil case asserts all three of the contract's observables — the sentinel
// (errors.Is), the retry classification (IsPermanent, decision D-M) and the
// POSITION substring, which is what distinguishes the five nil sites from each
// other. It asserts the substring, never the whole error text: the full string
// embeds permanentError's rendering, which is not this package's contract.
func TestPredicateCombinators(t *testing.T) {
	t.Parallel()

	errBoom := errors.New("boom")

	yes := routing.Predicate[int](func(context.Context, msgin.Message[int]) (bool, error) { return true, nil })
	no := routing.Predicate[int](func(context.Context, msgin.Message[int]) (bool, error) { return false, nil })
	// The two failing fixtures differ ONLY in the bool they return alongside the
	// error, and each orientation is load-bearing for a different trap. Neither
	// is redundant: swap them and the corresponding case goes vacuous.
	//
	// bad returns FALSE alongside its error — the orientation that makes the Not
	// case non-vacuous: a naive `return !ok, err` would report TRUE here, while
	// the contract requires (false, err). Proven by mutation.
	bad := routing.Predicate[int](func(context.Context, msgin.Message[int]) (bool, error) { return false, errBoom })
	// badTrue returns TRUE alongside its error — an ordinary shape for a
	// service-backed predicate that records a decision and then fails. It is the
	// orientation that makes every And/Or error case non-vacuous: a combinator
	// ending in a bare `return q(ctx, m)` passes the right operand's bool
	// straight through, so only a fixture returning (true, err) can catch it.
	// Driven with bad — (false, err) — those cases assert False against a value
	// that was already false and prove nothing. Proven by mutation.
	badTrue := routing.Predicate[int](func(context.Context, msgin.Message[int]) (bool, error) { return true, errBoom })

	var nilPred routing.Predicate[int]

	// nilAssert builds the three-part assertion every nil case shares; position
	// is the only part that differs, which is precisely the point of the wrap.
	nilAssert := func(position string) func(*testing.T, bool, error) {
		return func(t *testing.T, got bool, err error) {
			t.Helper()
			require.ErrorIs(t, err, msgin.ErrNilFunc)
			assert.True(t, msgin.IsPermanent(err), "a nil operand is permanent (D-M)")
			assert.Contains(t, err.Error(), position)
			assert.False(t, got, "a degraded predicate must not report true")
		}
	}

	tests := []struct {
		name string
		// build returns the predicate under test. mark wraps an operand whose
		// evaluation the case wants to observe, so a short-circuit claim is
		// measured rather than assumed.
		build  func(mark func(routing.Predicate[int]) routing.Predicate[int]) routing.Predicate[int]
		assert func(t *testing.T, got bool, err error)
		// markRan is the expected value of "the marked operand was evaluated".
		// nil means the case marks nothing and makes no claim.
		markRan *bool
	}{
		{
			name:  "And is true only when both sides are true",
			build: func(func(routing.Predicate[int]) routing.Predicate[int]) routing.Predicate[int] { return yes.And(yes) },
			assert: func(t *testing.T, got bool, err error) {
				require.NoError(t, err)
				assert.True(t, got)
			},
		},
		{
			name: "And short-circuits on a false left — the right side is never evaluated",
			build: func(mark func(routing.Predicate[int]) routing.Predicate[int]) routing.Predicate[int] {
				return no.And(mark(yes))
			},
			assert: func(t *testing.T, got bool, err error) {
				require.NoError(t, err)
				assert.False(t, got)
			},
			markRan: ptr(false),
		},
		{
			name: "And evaluates the right side when the left is true",
			build: func(mark func(routing.Predicate[int]) routing.Predicate[int]) routing.Predicate[int] {
				return yes.And(mark(no))
			},
			assert: func(t *testing.T, got bool, err error) {
				require.NoError(t, err)
				assert.False(t, got)
			},
			markRan: ptr(true),
		},
		{
			name: "And propagates a left-side error without evaluating the right",
			build: func(mark func(routing.Predicate[int]) routing.Predicate[int]) routing.Predicate[int] {
				return bad.And(mark(yes))
			},
			assert: func(t *testing.T, got bool, err error) {
				require.ErrorIs(t, err, errBoom)
				assert.False(t, got)
			},
			markRan: ptr(false),
		},
		{
			// Driven with badTrue, so the assertion discriminates: a combinator
			// ending in a bare `return q(ctx, m)` yields (true, errBoom) here.
			name: "And propagates a right-side error without leaking its true result",
			build: func(func(routing.Predicate[int]) routing.Predicate[int]) routing.Predicate[int] {
				return yes.And(badTrue)
			},
			assert: func(t *testing.T, got bool, err error) {
				require.ErrorIs(t, err, errBoom)
				assert.False(t, got, "an errored And must report false, not the right operand's result")
			},
		},
		{
			// The left-side mirror: the error arrives with a TRUE result, which
			// And would otherwise carry into the q call and out of the composite.
			name: "And propagates a left-side error that arrived with a true result",
			build: func(mark func(routing.Predicate[int]) routing.Predicate[int]) routing.Predicate[int] {
				return badTrue.And(mark(yes))
			},
			assert: func(t *testing.T, got bool, err error) {
				require.ErrorIs(t, err, errBoom)
				assert.False(t, got, "an errored And must report false, not the left operand's result")
			},
			markRan: ptr(false),
		},
		{
			name: "Or short-circuits on a true left — the right side is never evaluated",
			build: func(mark func(routing.Predicate[int]) routing.Predicate[int]) routing.Predicate[int] {
				return yes.Or(mark(no))
			},
			assert: func(t *testing.T, got bool, err error) {
				require.NoError(t, err)
				assert.True(t, got)
			},
			markRan: ptr(false),
		},
		{
			name: "Or evaluates the right side when the left is false",
			build: func(mark func(routing.Predicate[int]) routing.Predicate[int]) routing.Predicate[int] {
				return no.Or(mark(no))
			},
			assert: func(t *testing.T, got bool, err error) {
				require.NoError(t, err)
				assert.False(t, got)
			},
			markRan: ptr(true),
		},
		{
			name: "Or propagates a left-side error without evaluating the right",
			build: func(mark func(routing.Predicate[int]) routing.Predicate[int]) routing.Predicate[int] {
				return bad.Or(mark(yes))
			},
			assert: func(t *testing.T, got bool, err error) {
				require.ErrorIs(t, err, errBoom)
				assert.False(t, got)
			},
			markRan: ptr(false),
		},
		{
			// Same discrimination as its And counterpart: only a (true, err)
			// right operand can catch a bare `return q(ctx, m)`.
			name: "Or propagates a right-side error without leaking its true result",
			build: func(func(routing.Predicate[int]) routing.Predicate[int]) routing.Predicate[int] {
				return no.Or(badTrue)
			},
			assert: func(t *testing.T, got bool, err error) {
				require.ErrorIs(t, err, errBoom)
				assert.False(t, got, "an errored Or must report false, not the right operand's result")
			},
		},
		{
			// The left-side mirror: a naive Or that tests ok before err returns
			// (true, nil) here and swallows the failure outright.
			name: "Or propagates a left-side error that arrived with a true result",
			build: func(mark func(routing.Predicate[int]) routing.Predicate[int]) routing.Predicate[int] {
				return badTrue.Or(mark(yes))
			},
			assert: func(t *testing.T, got bool, err error) {
				require.ErrorIs(t, err, errBoom)
				assert.False(t, got, "an errored Or must report false, not the left operand's result")
			},
			markRan: ptr(false),
		},
		{
			name:  "Not inverts true to false",
			build: func(func(routing.Predicate[int]) routing.Predicate[int]) routing.Predicate[int] { return yes.Not() },
			assert: func(t *testing.T, got bool, err error) {
				require.NoError(t, err)
				assert.False(t, got)
			},
		},
		{
			name:  "Not inverts false to true",
			build: func(func(routing.Predicate[int]) routing.Predicate[int]) routing.Predicate[int] { return no.Not() },
			assert: func(t *testing.T, got bool, err error) {
				require.NoError(t, err)
				assert.True(t, got)
			},
		},
		{
			// The case a naive Not gets wrong: it must PROPAGATE the error, not
			// invert the (meaningless) result that came with it.
			name:  "Not propagates an error rather than inverting the result",
			build: func(func(routing.Predicate[int]) routing.Predicate[int]) routing.Predicate[int] { return bad.Not() },
			assert: func(t *testing.T, got bool, err error) {
				require.ErrorIs(t, err, errBoom)
				assert.False(t, got, "an errored Not must report false, not the inverted result")
			},
		},
		{
			name:   "And with a nil argument degrades to a permanent ErrNilFunc",
			build:  func(func(routing.Predicate[int]) routing.Predicate[int]) routing.Predicate[int] { return yes.And(nil) },
			assert: nilAssert("routing.Predicate.And: nil argument"),
		},
		{
			// The trap: a naive And short-circuits on the false left and never
			// looks at the nil argument, silently returning (false, nil).
			name:   "And with a nil argument surfaces even when the left is FALSE",
			build:  func(func(routing.Predicate[int]) routing.Predicate[int]) routing.Predicate[int] { return no.And(nil) },
			assert: nilAssert("routing.Predicate.And: nil argument"),
		},
		{
			name: "And on a nil receiver degrades to a permanent ErrNilFunc",
			build: func(func(routing.Predicate[int]) routing.Predicate[int]) routing.Predicate[int] {
				return nilPred.And(yes)
			},
			assert: nilAssert("routing.Predicate.And: nil receiver"),
		},
		{
			name:   "Or with a nil argument degrades to a permanent ErrNilFunc",
			build:  func(func(routing.Predicate[int]) routing.Predicate[int]) routing.Predicate[int] { return no.Or(nil) },
			assert: nilAssert("routing.Predicate.Or: nil argument"),
		},
		{
			// The mirror trap: a naive Or short-circuits on the true left and
			// never looks at the nil argument, silently returning (true, nil).
			name:   "Or with a nil argument surfaces even when the left is TRUE",
			build:  func(func(routing.Predicate[int]) routing.Predicate[int]) routing.Predicate[int] { return yes.Or(nil) },
			assert: nilAssert("routing.Predicate.Or: nil argument"),
		},
		{
			name: "Or on a nil receiver degrades to a permanent ErrNilFunc",
			build: func(func(routing.Predicate[int]) routing.Predicate[int]) routing.Predicate[int] {
				return nilPred.Or(yes)
			},
			assert: nilAssert("routing.Predicate.Or: nil receiver"),
		},
		{
			name:   "Not on a nil receiver degrades to a permanent ErrNilFunc",
			build:  func(func(routing.Predicate[int]) routing.Predicate[int]) routing.Predicate[int] { return nilPred.Not() },
			assert: nilAssert("routing.Predicate.Not: nil receiver"),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var markRan bool
			mark := func(p routing.Predicate[int]) routing.Predicate[int] {
				return func(ctx context.Context, m msgin.Message[int]) (bool, error) {
					markRan = true
					return p(ctx, m)
				}
			}

			got, err := tc.build(mark)(t.Context(), msgin.New(7))
			tc.assert(t, got, err)
			if tc.markRan != nil {
				assert.Equal(t, *tc.markRan, markRan, "marked operand evaluated?")
			}
		})
	}
}

// TestPredicateCombinators_NilPositionsAreDistinct pins the debuggability
// property the positional wrap exists for: the five nil sites must not collapse
// into one indistinguishable string.
func TestPredicateCombinators_NilPositionsAreDistinct(t *testing.T) {
	t.Parallel()

	ok := routing.Predicate[int](func(context.Context, msgin.Message[int]) (bool, error) { return true, nil })
	var nilPred routing.Predicate[int]

	built := []routing.Predicate[int]{
		ok.And(nil), nilPred.And(ok),
		ok.Or(nil), nilPred.Or(ok),
		nilPred.Not(),
	}

	seen := make(map[string]struct{}, len(built))
	for _, p := range built {
		_, err := p(t.Context(), msgin.New(1))
		require.Error(t, err)
		seen[err.Error()] = struct{}{}
	}
	assert.Len(t, seen, len(built), "each nil position must produce a distinct message: %v", seen)
}

// behaviorShapes supplies the accepted call shapes that a closure literal
// cannot express: the METHOD VALUE shape for each of the three routing behavior
// types. The `*Decl` funcs below are the PLAIN DECLARATION shape, and the
// `*Returned` funcs the FUNC-RETURNING-THE-UNNAMED-TYPE shape.
type behaviorShapes struct{}

func (behaviorShapes) pass(context.Context, msgin.Message[int]) (bool, error) { return true, nil }

func (behaviorShapes) route(context.Context, msgin.Message[any]) (msgin.MessageChannel, error) {
	return nil, nil
}

func (behaviorShapes) split(_ context.Context, m msgin.Message[int]) ([]msgin.Message[int], error) {
	return []msgin.Message[int]{m}, nil
}

func passDecl(context.Context, msgin.Message[int]) (bool, error) { return true, nil }

func passReturned() func(context.Context, msgin.Message[int]) (bool, error) { return passDecl }

func routeDecl(context.Context, msgin.Message[any]) (msgin.MessageChannel, error) { return nil, nil }

func routeReturned() func(context.Context, msgin.Message[any]) (msgin.MessageChannel, error) {
	return routeDecl
}

func splitDecl(_ context.Context, m msgin.Message[int]) ([]msgin.Message[int], error) {
	return []msgin.Message[int]{m}, nil
}

func splitReturned() func(context.Context, msgin.Message[int]) ([]msgin.Message[int], error) {
	return splitDecl
}

// TestBehaviorTypes_AcceptedCallShapes is the compile-level statement of WHICH
// call shapes the three routing behavior types accept — the precise property,
// not the over-broad "source-compatible".
//
// ACCEPTED, and pinned here: a bare closure literal (inferring the type
// parameters), a variable of the equivalent UNNAMED func type, a func returning
// that unnamed type, a method value, a plain top-level func declaration, and a
// value of msgin's OWN named type.
//
// REJECTED, and therefore NOT pinned here — it cannot be, since a test that
// asserts it would not compile: a caller's OWN NAMED func type. Go converts
// implicitly between func types only when at least one side is unnamed, so
// `type MyPred func(...); routing.Filter(p)` fails, and for the two generic
// types it fails as an opaque inference error ("cannot infer A"), not a plain
// assignability error. Measured against a throwaway package; the remedy is an
// explicit conversion at the call site, which shape 6 below is exactly. See the
// ASSIGNABILITY paragraph on each type's godoc.
func TestBehaviorTypes_AcceptedCallShapes(t *testing.T) {
	t.Parallel()

	next := msgin.HandlerFunc(func(context.Context, msgin.Message[any]) error { return nil })

	t.Run("Filter", func(t *testing.T) {
		t.Parallel()
		// A variable whose type is the UNNAMED func type (inferred from a plain
		// declaration, which has exactly that type) — the shape a caller reaches
		// for before defining a named type of their own.
		unnamedVar := passDecl
		var h behaviorShapes
		steps := map[string]msgin.Step{
			"bare closure literal":   routing.Filter(func(context.Context, msgin.Message[int]) (bool, error) { return true, nil }),
			"unnamed-type variable":  routing.Filter(unnamedVar),
			"func returning unnamed": routing.Filter(passReturned()),
			"method value":           routing.Filter(h.pass),
			"plain func declaration": routing.Filter(passDecl),
			"msgin's named type":     routing.Filter(routing.Predicate[int](passDecl)),
		}
		for name, step := range steps {
			t.Run(name, func(t *testing.T) {
				require.NoError(t, step(next).Handle(t.Context(), msgin.New[any](1)))
			})
		}
	})

	t.Run("NewRouter", func(t *testing.T) {
		t.Parallel()
		// A variable of the UNNAMED func type (see the Filter subtest).
		unnamedVar := routeDecl
		var h behaviorShapes
		routers := map[string]*routing.Router{
			"bare closure literal": routing.NewRouter(func(context.Context, msgin.Message[any]) (msgin.MessageChannel, error) {
				return nil, nil
			}),
			"unnamed-type variable":  routing.NewRouter(unnamedVar),
			"func returning unnamed": routing.NewRouter(routeReturned()),
			"method value":           routing.NewRouter(h.route),
			"plain func declaration": routing.NewRouter(routeDecl),
			"msgin's named type":     routing.NewRouter(routing.RouteFunc(routeDecl)),
		}
		for name, r := range routers {
			t.Run(name, func(t *testing.T) {
				require.ErrorIs(t, r.Handle(t.Context(), msgin.New[any](1)), msgin.ErrNoRoute)
			})
		}
	})

	t.Run("Split", func(t *testing.T) {
		t.Parallel()
		// A variable of the UNNAMED func type (see the Filter subtest).
		unnamedVar := splitDecl
		var h behaviorShapes
		steps := map[string]msgin.Step{
			"bare closure literal": routing.Split(func(_ context.Context, m msgin.Message[int]) ([]msgin.Message[int], error) {
				return []msgin.Message[int]{m}, nil
			}),
			"unnamed-type variable":  routing.Split(unnamedVar),
			"func returning unnamed": routing.Split(splitReturned()),
			"method value":           routing.Split(h.split),
			"plain func declaration": routing.Split(splitDecl),
			"msgin's named type":     routing.Split(routing.SplitFunc[int, int](splitDecl)),
		}
		for name, step := range steps {
			t.Run(name, func(t *testing.T) {
				require.NoError(t, step(next).Handle(t.Context(), msgin.New[any](1)))
			})
		}
	})
}

func ptr[T any](v T) *T { return &v }
