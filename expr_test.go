package msgin_test

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kartaladev/msgin"
	"github.com/kartaladev/msgin/adapter/memory"
	"github.com/stretchr/testify/require"
)

type exprOrder struct {
	Amount   int
	Currency string
}

// exprItem is the element type SplitExpr fans a batch's Items into.
type exprItem struct{ Name string }

// exprBatch is the SplitExpr source payload: payload.Items is the
// slice-yielding expression under test.
type exprBatch struct{ Items []exprItem }

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

// filterExprRun drives a single message through a Step to a terminal recording
// handler and returns the pass/fail outcome plus any handler error. Unlike
// runStep, it takes no *testing.T and calls no require/t methods, so it is
// safe to invoke from worker goroutines (testing.T's Fatal-family methods —
// which require/require.NoError use internally — must only be called from the
// test's own goroutine).
func filterExprRun(ctx context.Context, step msgin.Step, m msgin.Message[any]) (passed bool, err error) {
	next := msgin.HandlerFunc(func(context.Context, msgin.Message[any]) error { passed = true; return nil })
	err = step(next).Handle(ctx, m)
	return passed, err
}

// TestFilterExpr_Concurrent proves the *vm.Program compiled once by FilterExpr
// is safe to invoke from multiple goroutines concurrently — the shape a
// Consumer with WithConcurrency(N>1) drives it in production. expr documents
// *vm.Program as concurrent-safe (vm.Run spins up a fresh VM per call); this
// test exercises that guarantee under -race with a shared, single Step.
func TestFilterExpr_Concurrent(t *testing.T) {
	step, err := msgin.FilterExpr[exprOrder](`payload.Amount > 100 && header("vip") == true`)
	require.NoError(t, err)

	// Fixed variants mixing payload and header combinations, some passing and
	// some failing, including a message with no "vip" header at all.
	variants := []struct {
		msg      msgin.Message[any]
		wantPass bool
	}{
		{msgin.New[any](exprOrder{Amount: 150, Currency: "USD"}).WithHeader("vip", true), true},
		{msgin.New[any](exprOrder{Amount: 50, Currency: "USD"}).WithHeader("vip", true), false},
		{msgin.New[any](exprOrder{Amount: 150, Currency: "USD"}).WithHeader("vip", false), false},
		{msgin.New[any](exprOrder{Amount: 150, Currency: "USD"}), false}, // no "vip" header at all
		{msgin.New[any](exprOrder{Amount: 1000, Currency: "EUR"}).WithHeader("vip", true), true},
	}

	const goroutines = 8
	const iterationsPerGoroutine = 200
	require.Zero(t, iterationsPerGoroutine%len(variants), "iterationsPerGoroutine must divide evenly across variants for an exact expected count")

	var wantPassTotal int64
	for _, v := range variants {
		if v.wantPass {
			wantPassTotal += int64(goroutines * (iterationsPerGoroutine / len(variants)))
		}
	}

	var (
		wg         sync.WaitGroup
		gotPass    atomic.Int64
		mismatches atomic.Int64
		unexpErrs  atomic.Int64
	)
	ctx := t.Context()
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < iterationsPerGoroutine; i++ {
				v := variants[i%len(variants)]
				passed, err := filterExprRun(ctx, step, v.msg)
				if err != nil {
					unexpErrs.Add(1)
					continue
				}
				if passed {
					gotPass.Add(1)
				}
				if passed != v.wantPass {
					mismatches.Add(1)
				}
			}
		}()
	}
	wg.Wait()

	require.Zero(t, unexpErrs.Load(), "no message in this table should produce a runtime eval error")
	require.Zero(t, mismatches.Load(), "every concurrent invocation must produce the same pass/fail outcome as a sequential call would")
	require.Equal(t, wantPassTotal, gotPass.Load())
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

func TestTransformExpr(t *testing.T) {
	tests := []struct {
		name   string
		assert func(t *testing.T)
	}{
		{
			name: "empty expression is ErrInvalidExpression at construction",
			assert: func(t *testing.T) {
				_, err := msgin.TransformExpr[exprOrder, int]("   ")
				require.ErrorIs(t, err, msgin.ErrInvalidExpression)
			},
		},
		{
			name: "unparseable (non-empty) expression is ErrInvalidExpression (L-4: the Compile-failure branch)",
			assert: func(t *testing.T) {
				_, err := msgin.TransformExpr[exprOrder, int]("payload.")
				require.ErrorIs(t, err, msgin.ErrInvalidExpression)
			},
		},
		{
			name: "scalar projection propagates parent headers via WithPayload",
			assert: func(t *testing.T) {
				step, err := msgin.TransformExpr[exprOrder, int]("payload.Amount * 2")
				require.NoError(t, err)
				var got msgin.Message[any]
				next := msgin.HandlerFunc(func(_ context.Context, m msgin.Message[any]) error { got = m; return nil })
				parent := msgin.New[any](exprOrder{Amount: 21}).WithHeader(msgin.HeaderCorrelationID, "c1")
				require.NoError(t, step(next).Handle(t.Context(), parent))
				require.Equal(t, 42, got.Payload())
				cid, _ := got.Header(msgin.HeaderCorrelationID)
				require.Equal(t, "c1", cid) // WithPayload copied the parent's headers
			},
		},
		{
			name: "non-B result is ErrExprResultType at eval",
			assert: func(t *testing.T) {
				// A=any → no compile-time result type-check; expr yields int, B=string.
				step, err := msgin.TransformExpr[any, string]("1 + 1")
				require.NoError(t, err)
				next := msgin.HandlerFunc(func(context.Context, msgin.Message[any]) error { return nil })
				err = step(next).Handle(t.Context(), msgin.New[any](0))
				require.ErrorIs(t, err, msgin.ErrExprResultType)
			},
		},
		{
			name: "numeric ceiling (M-3): int-yielding expr with B=int64 is ErrExprResultType",
			assert: func(t *testing.T) {
				// expr integer math yields Go int, not int64; out.(int64) fails.
				// Documents the numeric-B ceiling as a tested contract.
				step, err := msgin.TransformExpr[exprOrder, int64]("payload.Amount")
				require.NoError(t, err)
				next := msgin.HandlerFunc(func(context.Context, msgin.Message[any]) error { return nil })
				err = step(next).Handle(t.Context(), msgin.New[any](exprOrder{Amount: 7}))
				require.ErrorIs(t, err, msgin.ErrExprResultType)
			},
		},
		{
			name: "runtime eval error propagates",
			assert: func(t *testing.T) {
				step, err := msgin.TransformExpr[exprOrder, any](`header("missing") > 100`)
				require.NoError(t, err)
				next := msgin.HandlerFunc(func(context.Context, msgin.Message[any]) error { return nil })
				require.Error(t, step(next).Handle(t.Context(), msgin.New[any](exprOrder{})))
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) { tt.assert(t) })
	}
}

// ExampleTransformExpr projects a payload field via a runtime expression
// instead of a Go func Transform. Like FilterExpr, it returns (Step, error):
// construct it, check the error, then compose the resulting Step like any
// other — here via Chain into a terminal Consume that records the result.
func ExampleTransformExpr() {
	step, err := msgin.TransformExpr[exprOrder, int]("payload.Amount * 2")
	if err != nil {
		fmt.Println("invalid expression:", err)
		return
	}

	var doubled []int
	flow := msgin.Chain(
		step,
		msgin.Consume(func(_ context.Context, m msgin.Message[int]) error {
			doubled = append(doubled, m.Payload())
			return nil
		}),
	)

	_ = flow.Handle(context.Background(), msgin.New[any](exprOrder{Amount: 21, Currency: "USD"}))

	fmt.Println(doubled)
	// Output: [42]
}

func TestSplitExpr(t *testing.T) {
	tests := []struct {
		name   string
		assert func(t *testing.T)
	}{
		{
			name: "empty expression is ErrInvalidExpression at construction",
			assert: func(t *testing.T) {
				_, err := msgin.SplitExpr[exprBatch, exprItem]("   ")
				require.ErrorIs(t, err, msgin.ErrInvalidExpression)
			},
		},
		{
			name: "unparseable expression is ErrInvalidExpression at construction",
			assert: func(t *testing.T) {
				_, err := msgin.SplitExpr[exprBatch, exprItem]("payload.")
				require.ErrorIs(t, err, msgin.ErrInvalidExpression)
			},
		},
		{
			name: "PayloadOf error propagates from Handle",
			assert: func(t *testing.T) {
				step, err := msgin.SplitExpr[exprBatch, exprItem]("payload.Items")
				require.NoError(t, err)
				next := msgin.HandlerFunc(func(context.Context, msgin.Message[any]) error { return nil })
				err = step(next).Handle(t.Context(), msgin.New[any](42)) // wrong payload type
				require.ErrorIs(t, err, msgin.ErrPayloadType)
			},
		},
		{
			name: "slice-yielding expr fans out N children in order with sequence headers stamped",
			assert: func(t *testing.T) {
				step, err := msgin.SplitExpr[exprBatch, exprItem]("payload.Items")
				require.NoError(t, err)
				var forwarded []msgin.Message[any]
				next := msgin.HandlerFunc(func(_ context.Context, m msgin.Message[any]) error {
					forwarded = append(forwarded, m)
					return nil
				})
				parent := msgin.New[any](exprBatch{Items: []exprItem{{Name: "a"}, {Name: "b"}}}, msgin.WithID("parent-1"))
				require.NoError(t, step(next).Handle(t.Context(), parent))
				require.Len(t, forwarded, 2)
				require.Equal(t, exprItem{Name: "a"}, forwarded[0].Payload())
				require.Equal(t, exprItem{Name: "b"}, forwarded[1].Payload())
				require.Equal(t, "parent-1#1", forwarded[0].ID())
				require.Equal(t, "parent-1#2", forwarded[1].ID())
				for i, m := range forwarded {
					num, _ := m.Header(msgin.HeaderSequenceNumber)
					size, _ := m.Header(msgin.HeaderSequenceSize)
					require.Equal(t, i+1, num)
					require.Equal(t, 2, size)
				}
			},
		},
		{
			name: "empty slice forwards nothing",
			assert: func(t *testing.T) {
				step, err := msgin.SplitExpr[exprBatch, exprItem]("payload.Items")
				require.NoError(t, err)
				var forwarded []msgin.Message[any]
				next := msgin.HandlerFunc(func(_ context.Context, m msgin.Message[any]) error {
					forwarded = append(forwarded, m)
					return nil
				})
				require.NoError(t, step(next).Handle(t.Context(), msgin.New[any](exprBatch{})))
				require.Empty(t, forwarded)
			},
		},
		{
			name: "non-slice result is ErrExprResultType",
			assert: func(t *testing.T) {
				step, err := msgin.SplitExpr[exprBatch, exprItem]("payload.Items[0]") // yields a struct, not a slice
				require.NoError(t, err)
				next := msgin.HandlerFunc(func(context.Context, msgin.Message[any]) error { return nil })
				parent := msgin.New[any](exprBatch{Items: []exprItem{{Name: "a"}}})
				err = step(next).Handle(t.Context(), parent)
				require.ErrorIs(t, err, msgin.ErrExprResultType)
			},
		},
		{
			name: "non-B element is ErrExprResultType",
			assert: func(t *testing.T) {
				// A=any → no compile-time result type-check; the literal yields a
				// mixed []interface{} whose 2nd element cannot be asserted to B=int.
				step, err := msgin.SplitExpr[any, int](`[1, "two"]`)
				require.NoError(t, err)
				next := msgin.HandlerFunc(func(context.Context, msgin.Message[any]) error { return nil })
				err = step(next).Handle(t.Context(), msgin.New[any](0))
				require.ErrorIs(t, err, msgin.ErrExprResultType)
			},
		},
		{
			name: "runtime eval error propagates",
			assert: func(t *testing.T) {
				step, err := msgin.SplitExpr[exprBatch, exprItem](`header("missing") > 100`)
				require.NoError(t, err) // compiles fine (header() is any)
				next := msgin.HandlerFunc(func(context.Context, msgin.Message[any]) error { return nil })
				err = step(next).Handle(t.Context(), msgin.New[any](exprBatch{}))
				require.Error(t, err)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) { tt.assert(t) })
	}
}

// ExampleSplitExpr fans a batch into its items via a runtime expression
// instead of a Go func Split, forwarding each item with its 1-based sequence
// position out of the total — the headers a downstream Aggregator uses to
// reassemble the group.
func ExampleSplitExpr() {
	step, err := msgin.SplitExpr[exprBatch, exprItem]("payload.Items")
	if err != nil {
		fmt.Println("invalid expression:", err)
		return
	}

	emit := msgin.HandlerFunc(func(_ context.Context, m msgin.Message[any]) error {
		num, _ := m.Header(msgin.HeaderSequenceNumber)
		size, _ := m.Header(msgin.HeaderSequenceSize)
		fmt.Printf("item %v/%v: %v\n", num, size, m.Payload())
		return nil
	})

	// A SplitExpr is a Step: step(next) yields the handler wired to next.
	h := step(emit)
	batch := exprBatch{Items: []exprItem{{Name: "a"}, {Name: "b"}}}
	_ = h.Handle(context.Background(), msgin.New[any](batch))

	// Output:
	// item 1/2: {a}
	// item 2/2: {b}
}

func TestRouterExpr(t *testing.T) {
	tests := []struct {
		name   string
		assert func(t *testing.T)
	}{
		{
			name: "bad key expression is ErrInvalidExpression",
			assert: func(t *testing.T) {
				eu := &collector{}
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
				eu := &collector{}
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
				eu := &collector{}
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
				eu, us := &collector{}, &collector{}
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
				eu, def := &collector{}, &collector{}
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

// exprGroupItem is the Aggregator group-member payload type
// WithCorrelationExpr/WithReleaseExpr tests correlate/release against: Region
// drives correlation-by-field, Amount is summed by sumAmountFn.
type exprGroupItem struct {
	Amount int
	Region string
}

// sumAmountFn aggregates a group of exprGroupItem payloads into the sum of
// their Amount fields.
func sumAmountFn(_ context.Context, group []msgin.Message[exprGroupItem]) (msgin.Message[int], error) {
	sum := 0
	for _, m := range group {
		sum += m.Payload().Amount
	}
	return msgin.New(sum), nil
}

// TestWithCorrelationExpr covers WithCorrelationExpr's construction-time
// compile error, the grouping-by-evaluated-key happy path, a runtime eval
// error propagating from Handle, and the empty-evaluated-key ->
// Permanent(ErrNoCorrelation) symmetry with defaultCorrelate (L-1).
func TestWithCorrelationExpr(t *testing.T) {
	tests := []struct {
		name   string
		assert func(t *testing.T)
	}{
		{
			name: "bad expression is ErrInvalidExpression at NewAggregator",
			assert: func(t *testing.T) {
				store := newIntStore(t)
				out := &fakeAggChannel{}
				agg, err := msgin.NewAggregator[exprGroupItem, int](store, sumAmountFn,
					msgin.WithOutputChannel(out),
					msgin.WithCorrelationExpr[exprGroupItem]("payload."))
				require.ErrorIs(t, err, msgin.ErrInvalidExpression)
				require.Nil(t, agg)
			},
		},
		{
			name: "a good expression groups messages by the evaluated key",
			assert: func(t *testing.T) {
				store := newIntStore(t)
				out := &fakeAggChannel{}
				agg, err := msgin.NewAggregator[exprGroupItem, int](store, sumAmountFn,
					msgin.WithOutputChannel(out),
					msgin.WithCorrelationExpr[exprGroupItem]("payload.Region"),
					msgin.WithCompletionSize(2))
				require.NoError(t, err)

				eu1 := msgin.New[any](exprGroupItem{Amount: 1, Region: "EU"})
				us1 := msgin.New[any](exprGroupItem{Amount: 10, Region: "US"})
				eu2 := msgin.New[any](exprGroupItem{Amount: 2, Region: "EU"})

				require.NoError(t, agg.Handle(t.Context(), eu1))
				require.NoError(t, agg.Handle(t.Context(), us1))
				require.Equal(t, 0, out.count(), "each key holds only 1 member so far")

				require.NoError(t, agg.Handle(t.Context(), eu2))
				require.Equal(t, 1, out.count(), "the EU group reached size 2 and released; US is untouched")
				require.Equal(t, 3, out.last().Payload(), "EU group: 1 + 2")
			},
		},
		{
			name: "a runtime eval error surfaces from Handle",
			assert: func(t *testing.T) {
				store := newIntStore(t)
				out := &fakeAggChannel{}
				agg, err := msgin.NewAggregator[exprGroupItem, int](store, sumAmountFn,
					msgin.WithOutputChannel(out),
					// header("missing") is nil; nil + string is a real expr eval
					// error (same shape verified for RouterExpr above).
					msgin.WithCorrelationExpr[exprGroupItem](`header("missing") + payload.Region`))
				require.NoError(t, err) // compiles fine (header() is any)

				err = agg.Handle(t.Context(), msgin.New[any](exprGroupItem{Region: "EU"}))
				require.Error(t, err)
				require.NotErrorIs(t, err, msgin.ErrNoCorrelation)
			},
		},
		{
			name: "an empty evaluated key is Permanent(ErrNoCorrelation) (L-1)",
			assert: func(t *testing.T) {
				store := newIntStore(t)
				out := &fakeAggChannel{}
				agg, err := msgin.NewAggregator[exprGroupItem, int](store, sumAmountFn,
					msgin.WithOutputChannel(out),
					msgin.WithCorrelationExpr[exprGroupItem](`""`))
				require.NoError(t, err)

				err = agg.Handle(t.Context(), msgin.New[any](exprGroupItem{Region: "EU"}))
				require.ErrorIs(t, err, msgin.ErrNoCorrelation)
				require.Contains(t, err.Error(), "permanent",
					"must be Permanent-wrapped so the runtime routes it to the invalid-message channel, not the DLQ")
				require.Equal(t, 0, out.count())
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) { tc.assert(t) })
	}
}

// TestWithReleaseExpr covers WithReleaseExpr's construction-time compile
// error, the size-gated canonical form vs. the un-gated footgun (M-4), a
// member header() predicate, a runtime eval error propagating from Handle
// (D14c), first-expr-option-error-wins in both option orders, and the
// expr-option compile error taking precedence over ErrNilOutput/
// ErrExpiryChannelRequired (L-5).
func TestWithReleaseExpr(t *testing.T) {
	tests := []struct {
		name   string
		assert func(t *testing.T)
	}{
		{
			name: "empty expression is ErrInvalidExpression at NewAggregator (compileGroup's empty guard)",
			assert: func(t *testing.T) {
				store := newIntStore(t)
				out := &fakeAggChannel{}
				agg, err := msgin.NewAggregator[int, int](store, sumFn,
					msgin.WithOutputChannel(out),
					msgin.WithReleaseExpr[int]("   "))
				require.ErrorIs(t, err, msgin.ErrInvalidExpression)
				require.Nil(t, agg)
			},
		},
		{
			name: "unparseable (non-empty) expression is ErrInvalidExpression at NewAggregator",
			assert: func(t *testing.T) {
				store := newIntStore(t)
				out := &fakeAggChannel{}
				agg, err := msgin.NewAggregator[int, int](store, sumFn,
					msgin.WithOutputChannel(out),
					msgin.WithReleaseExpr[int]("payload."))
				require.ErrorIs(t, err, msgin.ErrInvalidExpression)
				require.Nil(t, agg)
			},
		},
		{
			name: "size-gated form (M-4 canonical) releases at the sequence size",
			assert: func(t *testing.T) {
				store := newIntStore(t)
				out := &fakeAggChannel{}
				agg, err := msgin.NewAggregator[int, int](store, sumFn,
					msgin.WithOutputChannel(out),
					msgin.WithReleaseExpr[int]("size > 0 && len(messages) >= size"))
				require.NoError(t, err)

				m1 := corrMsg(1, "m1", "g", map[string]any{msgin.HeaderSequenceSize: 2})
				m2 := corrMsg(2, "m2", "g", map[string]any{msgin.HeaderSequenceSize: 2})
				require.NoError(t, agg.Handle(t.Context(), m1))
				require.Equal(t, 0, out.count(), "held: only 1 of 2 members so far")
				require.NoError(t, agg.Handle(t.Context(), m2))
				require.Equal(t, 1, out.count())
				require.Equal(t, 3, out.last().Payload())
			},
		},
		{
			name: "un-gated form (M-4 footgun) releases a singleton with no HeaderSequenceSize present",
			assert: func(t *testing.T) {
				store := newIntStore(t)
				out := &fakeAggChannel{}
				agg, err := msgin.NewAggregator[int, int](store, sumFn,
					msgin.WithOutputChannel(out),
					msgin.WithReleaseExpr[int]("len(messages) >= size"))
				require.NoError(t, err)

				// size defaults to 0 (no HeaderSequenceSize header): len(messages)
				// >= 0 is trivially true from the very first member — a documented
				// footgun of the un-gated form.
				m1 := corrMsg(1, "m1", "g", nil)
				require.NoError(t, agg.Handle(t.Context(), m1))
				require.Equal(t, 1, out.count(), "released a group-of-one")
				require.Equal(t, 1, out.last().Payload())
			},
		},
		{
			name: "a member header() predicate releases",
			assert: func(t *testing.T) {
				store := newIntStore(t)
				out := &fakeAggChannel{}
				agg, err := msgin.NewAggregator[int, int](store, sumFn,
					msgin.WithOutputChannel(out),
					msgin.WithReleaseExpr[int](`messages[0].header("priority") == "high"`))
				require.NoError(t, err)

				m1 := corrMsg(5, "m1", "g", map[string]any{"priority": "high"})
				require.NoError(t, agg.Handle(t.Context(), m1))
				require.Equal(t, 1, out.count())
				require.Equal(t, 5, out.last().Payload())
			},
		},
		{
			name: "a runtime eval error propagates from Handle (D14c)",
			assert: func(t *testing.T) {
				store := newIntStore(t)
				out := &fakeAggChannel{}
				agg, err := msgin.NewAggregator[int, int](store, sumFn,
					msgin.WithOutputChannel(out),
					// no "qty" header on m1: header() is nil, nil > 0 is a real
					// expr eval error (same shape verified for FilterExpr).
					msgin.WithReleaseExpr[int](`messages[0].header("qty") > 0`))
				require.NoError(t, err)

				m1 := corrMsg(1, "m1", "g", nil)
				err = agg.Handle(t.Context(), m1)
				require.Error(t, err)
				require.Equal(t, 0, out.count())
			},
		},
		{
			name: "first-expr-option-error-wins: WithCorrelationExpr applied before WithReleaseExpr",
			assert: func(t *testing.T) {
				store := newIntStore(t)
				out := &fakeAggChannel{}
				_, err := msgin.NewAggregator[int, int](store, sumFn,
					msgin.WithOutputChannel(out),
					msgin.WithCorrelationExpr[int]("corrBad("),
					msgin.WithReleaseExpr[int]("relBad("),
				)
				require.ErrorIs(t, err, msgin.ErrInvalidExpression)
				require.Contains(t, err.Error(), "corrBad")
			},
		},
		{
			name: "first-expr-option-error-wins: WithReleaseExpr applied before WithCorrelationExpr",
			assert: func(t *testing.T) {
				store := newIntStore(t)
				out := &fakeAggChannel{}
				_, err := msgin.NewAggregator[int, int](store, sumFn,
					msgin.WithOutputChannel(out),
					msgin.WithReleaseExpr[int]("relBad("),
					msgin.WithCorrelationExpr[int]("corrBad("),
				)
				require.ErrorIs(t, err, msgin.ErrInvalidExpression)
				require.Contains(t, err.Error(), "relBad")
			},
		},
		{
			name: "an expr-option compile error takes precedence over ErrNilOutput (L-5)",
			assert: func(t *testing.T) {
				store := newIntStore(t)
				_, err := msgin.NewAggregator[int, int](store, sumFn,
					msgin.WithReleaseExpr[int]("payload."), // no WithOutputChannel either
				)
				require.ErrorIs(t, err, msgin.ErrInvalidExpression)
				require.NotErrorIs(t, err, msgin.ErrNilOutput)
			},
		},
		{
			name: "an expr-option compile error takes precedence over ErrExpiryChannelRequired (L-5)",
			assert: func(t *testing.T) {
				store := newIntStore(t)
				out := &fakeAggChannel{}
				_, err := msgin.NewAggregator[int, int](store, sumFn,
					msgin.WithOutputChannel(out),
					msgin.WithGroupTimeout(30*time.Second), // no WithExpiredGroupChannel either
					msgin.WithReleaseExpr[int]("payload."),
				)
				require.ErrorIs(t, err, msgin.ErrInvalidExpression)
				require.NotErrorIs(t, err, msgin.ErrExpiryChannelRequired)
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) { tc.assert(t) })
	}
}

// ExampleWithReleaseExpr correlates messages by a runtime expression
// (WithCorrelationExpr on payload.Region) and releases each group with a
// runtime release expression (WithReleaseExpr). The release expression uses
// the canonical size-GATED form — size > 0 && len(messages) >= size — never
// the un-gated len(messages) >= size, which releases a singleton the moment
// no HeaderSequenceSize header is present (a documented footgun; see
// WithReleaseExpr's godoc).
func ExampleWithReleaseExpr() {
	store, err := memory.NewGroupStore()
	if err != nil {
		panic(err)
	}

	out := msgin.NewDirectChannel()
	if err := out.Subscribe(msgin.HandlerFunc(func(_ context.Context, m msgin.Message[any]) error {
		fmt.Printf("region total: %v\n", m.Payload())
		return nil
	})); err != nil {
		panic(err)
	}

	agg, err := msgin.NewAggregator[exprGroupItem, int](store, sumAmountFn,
		msgin.WithOutputChannel(out),
		msgin.WithCorrelationExpr[exprGroupItem]("payload.Region"),
		msgin.WithReleaseExpr[exprGroupItem]("size > 0 && len(messages) >= size"),
	)
	if err != nil {
		panic(err)
	}

	items := []exprGroupItem{
		{Amount: 10, Region: "EU"},
		{Amount: 25, Region: "EU"},
	}
	for i, it := range items {
		msg := msgin.New[any](it, msgin.WithID(fmt.Sprintf("item-%d", i)), msgin.WithHeaders(map[string]any{
			msgin.HeaderSequenceSize: 2,
		}))
		if err := agg.Handle(context.Background(), msg); err != nil {
			panic(err)
		}
	}

	// Output:
	// region total: 35
}
