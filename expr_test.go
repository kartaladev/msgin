package msgin_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/kartaladev/msgin"
	"github.com/stretchr/testify/require"
)

type exprOrder struct {
	Amount   int
	Currency string
}

// collector is a MessageChannel that records what it receives (blackbox helper).
type collector struct{ got []msgin.Message[any] }

func (c *collector) Send(_ context.Context, m msgin.Message[any]) error {
	c.got = append(c.got, m)
	return nil
}
func (c *collector) Subscribe(msgin.MessageHandler) error { return nil }

// run drives a single message through a Step to a terminal recording handler.
func runStep(t *testing.T, step msgin.Step, m msgin.Message[any]) (passed bool) {
	t.Helper()
	next := msgin.HandlerFunc(func(context.Context, msgin.Message[any]) error { passed = true; return nil })
	require.NoError(t, step(next).Handle(t.Context(), m))
	return passed
}

func TestFilterExpr(t *testing.T) {
	tests := []struct {
		name   string
		assert func(t *testing.T)
	}{
		{
			name: "invalid expression is ErrInvalidExpression at construction",
			assert: func(t *testing.T) {
				_, err := msgin.FilterExpr[exprOrder]("payload.Amount >") // syntax error
				require.ErrorIs(t, err, msgin.ErrInvalidExpression)
			},
		},
		{
			name: "empty expression is ErrInvalidExpression",
			assert: func(t *testing.T) {
				_, err := msgin.FilterExpr[exprOrder]("   ")
				require.ErrorIs(t, err, msgin.ErrInvalidExpression)
			},
		},
		{
			name: "non-bool result is rejected at construction (AsBool)",
			assert: func(t *testing.T) {
				_, err := msgin.FilterExpr[exprOrder]("payload.Amount") // int, not bool
				require.ErrorIs(t, err, msgin.ErrInvalidExpression)
			},
		},
		{
			name: "unknown field is rejected at construction (type-check against A)",
			assert: func(t *testing.T) {
				_, err := msgin.FilterExpr[exprOrder]("payload.Nope > 1")
				require.ErrorIs(t, err, msgin.ErrInvalidExpression)
			},
		},
		{
			name: "true predicate passes the message through",
			assert: func(t *testing.T) {
				step, err := msgin.FilterExpr[exprOrder](`payload.Amount > 100 && payload.Currency == "USD"`)
				require.NoError(t, err)
				require.True(t, runStep(t, step, msgin.New[any](exprOrder{Amount: 150, Currency: "USD"})))
			},
		},
		{
			name: "false predicate drops (silent) and can divert to WithDiscardChannel",
			assert: func(t *testing.T) {
				disc := &collector{}
				step, err := msgin.FilterExpr[exprOrder](`payload.Amount > 100`, msgin.WithDiscardChannel(disc))
				require.NoError(t, err)
				require.False(t, runStep(t, step, msgin.New[any](exprOrder{Amount: 5})))
				require.Len(t, disc.got, 1)
			},
		},
		{
			name: "header access via header()",
			assert: func(t *testing.T) {
				step, err := msgin.FilterExpr[exprOrder](`header("vip") == true`)
				require.NoError(t, err)
				m := msgin.New[any](exprOrder{}).WithHeader("vip", true)
				require.True(t, runStep(t, step, m))
			},
		},
		{
			name: "runtime eval error propagates as the handler error",
			assert: func(t *testing.T) {
				// A missing header is nil; ordering a nil against an int is a real
				// expr eval error (verified v1.17.8: "invalid operation: <nil> > int").
				// NB: divide-by-zero does NOT error in expr — do not use it here.
				step, err := msgin.FilterExpr[exprOrder](`header("missing") > 100`)
				require.NoError(t, err) // compiles fine (header() is any)
				next := msgin.HandlerFunc(func(context.Context, msgin.Message[any]) error { return nil })
				err = step(next).Handle(t.Context(), msgin.New[any](exprOrder{}))
				require.Error(t, err) // the eval error surfaces to the runtime
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) { tt.assert(t) })
	}
}

// ExampleFilterExpr shows the two-line construct-then-check pattern:
// FilterExpr returns (Step, error) — unlike a bare Filter, it cannot be
// passed inline to Chain — so the caller constructs it, checks the error,
// then composes the resulting Step like any other.
func ExampleFilterExpr() {
	step, err := msgin.FilterExpr[exprOrder]("payload.Amount > 100")
	if err != nil {
		fmt.Println("invalid expression:", err)
		return
	}

	var passed []int
	flow := msgin.Chain(
		step,
		msgin.Consume(func(_ context.Context, m msgin.Message[exprOrder]) error {
			passed = append(passed, m.Payload().Amount)
			return nil
		}),
	)

	_ = flow.Handle(context.Background(), msgin.New[any](exprOrder{Amount: 150, Currency: "USD"}))
	_ = flow.Handle(context.Background(), msgin.New[any](exprOrder{Amount: 50, Currency: "USD"})) // filtered out

	fmt.Println(passed)
	// Output: [150]
}

// ExampleRouterExpr routes on a runtime routing-key expression instead of a
// Go func, dispatching each message to the MessageChannel mapped from
// payload.Currency.
func ExampleRouterExpr() {
	eu, us := &collector{}, &collector{}
	router, err := msgin.RouterExpr[exprOrder]("payload.Currency", map[string]msgin.MessageChannel{
		"EUR": eu,
		"USD": us,
	})
	if err != nil {
		fmt.Println("invalid expression:", err)
		return
	}

	_ = router.Handle(context.Background(), msgin.New[any](exprOrder{Amount: 10, Currency: "EUR"}))
	_ = router.Handle(context.Background(), msgin.New[any](exprOrder{Amount: 20, Currency: "USD"}))

	fmt.Println(len(eu.got), len(us.got))
	// Output: 1 1
}

func TestRouterExpr(t *testing.T) {
	eu, us, def := &collector{}, &collector{}, &collector{}
	tests := []struct {
		name   string
		assert func(t *testing.T)
	}{
		{
			name: "bad key expression is ErrInvalidExpression",
			assert: func(t *testing.T) {
				// Non-empty routes so the keyExpr COMPILE-error branch is reached
				// (an empty map would trip the len(routes)==0 guard first — NEW-1).
				_, err := msgin.RouterExpr[exprOrder]("payload.", map[string]msgin.MessageChannel{"x": eu})
				require.ErrorIs(t, err, msgin.ErrInvalidExpression)
			},
		},
		{
			name: "nil/empty routes is ErrInvalidExpression",
			assert: func(t *testing.T) {
				_, err := msgin.RouterExpr[exprOrder]("payload.Currency", nil)
				require.ErrorIs(t, err, msgin.ErrInvalidExpression)
			},
		},
		{
			name: "a nil channel value in routes is ErrInvalidExpression",
			assert: func(t *testing.T) {
				_, err := msgin.RouterExpr[exprOrder]("payload.Currency",
					map[string]msgin.MessageChannel{"EUR": nil})
				require.ErrorIs(t, err, msgin.ErrInvalidExpression)
			},
		},
		{
			name: "PayloadOf error propagates from Handle",
			assert: func(t *testing.T) {
				r, err := msgin.RouterExpr[exprOrder]("payload.Currency",
					map[string]msgin.MessageChannel{"EUR": eu})
				require.NoError(t, err)
				// Wrong payload type: A = exprOrder, message carries an int.
				require.ErrorIs(t, r.Handle(t.Context(), msgin.New[any](42)), msgin.ErrPayloadType)
			},
		},
		{
			name: "eval error propagates from Handle",
			assert: func(t *testing.T) {
				// header("missing") is nil; nil + string is a real expr eval error
				// (verified v1.17.8: "invalid operation: <nil> + string").
				r, err := msgin.RouterExpr[exprOrder](`header("missing") + payload.Currency`,
					map[string]msgin.MessageChannel{"EUR": eu})
				require.NoError(t, err) // compiles fine (header() is any)
				require.Error(t, r.Handle(t.Context(), msgin.New[any](exprOrder{Currency: "EUR"})))
			},
		},
		{
			name: "key hit routes to the mapped channel",
			assert: func(t *testing.T) {
				r, err := msgin.RouterExpr[exprOrder]("payload.Currency",
					map[string]msgin.MessageChannel{"EUR": eu, "USD": us})
				require.NoError(t, err)
				require.NoError(t, r.Handle(t.Context(), msgin.New[any](exprOrder{Currency: "EUR"})))
				require.Len(t, eu.got, 1)
				require.Empty(t, us.got)
			},
		},
		{
			name: "miss with default routes to default; without default is ErrNoRoute",
			assert: func(t *testing.T) {
				withDef, _ := msgin.RouterExpr[exprOrder]("payload.Currency",
					map[string]msgin.MessageChannel{"EUR": eu}, msgin.WithDefaultChannel(def))
				require.NoError(t, withDef.Handle(t.Context(), msgin.New[any](exprOrder{Currency: "GBP"})))
				require.Len(t, def.got, 1)

				noDef, _ := msgin.RouterExpr[exprOrder]("payload.Currency", map[string]msgin.MessageChannel{"EUR": eu})
				require.ErrorIs(t, noDef.Handle(t.Context(), msgin.New[any](exprOrder{Currency: "GBP"})), msgin.ErrNoRoute)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) { tt.assert(t) })
	}
}
