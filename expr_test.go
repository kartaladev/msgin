package msgin_test

import (
	"context"
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
