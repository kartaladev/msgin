package expr_test

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kartaladev/msgin"
	"github.com/kartaladev/msgin/adapter/memory"
	"github.com/kartaladev/msgin/endpoint"
	"github.com/kartaladev/msgin/expr"
	"github.com/kartaladev/msgin/routing"
	"github.com/kartaladev/msgin/transform"
	"github.com/stretchr/testify/require"
)

// exprOrder is the single-message payload type the Predicate/RouteFunc/
// Transformer cases evaluate their expressions against.
type exprOrder struct {
	Amount   int
	Currency string
}

// exprItem is the element type SplitFunc fans a batch's Items into.
type exprItem struct{ Name string }

// exprBatch is the SplitFunc source payload: payload.Items is the
// slice-yielding expression under test.
type exprBatch struct{ Items []exprItem }

// collector is a MessageChannel that records what it receives (blackbox
// helper). It is mutex-guarded because TestPredicate_Concurrent shares one
// Step across goroutines.
type collector struct {
	mu  sync.Mutex
	got []msgin.Message[any]
}

func (c *collector) Send(_ context.Context, m msgin.Message[any]) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.got = append(c.got, m)
	return nil
}

func (c *collector) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.got)
}

func (c *collector) last() msgin.Message[any] {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.got[len(c.got)-1]
}

// runStep drives a single message through a Step to a terminal recording
// handler.
func runStep(t *testing.T, step msgin.Step, m msgin.Message[any]) (passed bool) {
	t.Helper()
	next := msgin.HandlerFunc(func(context.Context, msgin.Message[any]) error { passed = true; return nil })
	require.NoError(t, step(next).Handle(t.Context(), m))
	return passed
}

// discardStep is a terminal handler that swallows whatever reaches it — the
// "next" a Step is wired to when the case asserts on the Step's own error.
func discardStep() msgin.MessageHandler {
	return msgin.HandlerFunc(func(context.Context, msgin.Message[any]) error { return nil })
}

// fakeGroup is a hand-built msgin.MessageGroup snapshot. It is the only way to
// drive Release's toGroupEnv guards blackbox: an empty group (M-1) and a
// member whose payload is not A (M-6) are both states a real GroupStore fed by
// a correctly-typed flow will not produce.
type fakeGroup struct {
	key     string
	msgs    []msgin.Message[any]
	created time.Time
}

func (g fakeGroup) Key() string                    { return g.key }
func (g fakeGroup) Messages() []msgin.Message[any] { return g.msgs }
func (g fakeGroup) CreatedAt() time.Time           { return g.created }

// requireNamesExpression asserts a runtime failure carries this module's
// evaluation wrap around the SOURCE EXPRESSION TEXT — the debuggability
// mitigation ADR 0029 §3 traded the interface shape for.
//
// It deliberately does NOT settle for require.Contains(err.Error(), src):
// expr's own evaluation error already echoes the offending source line, so
// that assertion PASSES with the wrap deleted (mutation-proven, mutant
// M5-evalerror-text). The wrap prefix is the part msgin/expr adds, so the wrap
// prefix is what gets asserted.
func requireNamesExpression(t *testing.T, err error, src string) {
	t.Helper()
	require.ErrorContains(t, err, fmt.Sprintf("msgin/expr: evaluating %q", src))
}

// requireNamesInvalidExpression asserts a CONSTRUCTION failure carries this
// module's own quoting of the offending source text.
//
// Same trap as requireNamesExpression, one layer earlier and missed by the
// first pass: expr.Compile's error ALSO echoes the offending source line, so
// require.ErrorContains(err, src) passes on the DEPENDENCY's text with
// msgin/expr's `%q` wrap deleted outright. Reviewer-proven, and reproduced
// here: replacing both `fmt.Errorf("%w %q: %w", ErrInvalidExpression,
// expression, err)` sites with `fmt.Errorf("%w: %w", ErrInvalidExpression,
// err)` left the whole suite green. Asserting the sentinel text followed by the
// QUOTED source is what distinguishes msgin's contribution from expr-lang's.
func requireNamesInvalidExpression(t *testing.T, err error, src string) {
	t.Helper()
	require.ErrorContains(t, err, fmt.Sprintf("%s %q", expr.ErrInvalidExpression.Error(), src))
}

// TestFailAtConstruction is ADR 0019's contract, asserted UNIFORMLY across all
// six providers rather than once per provider table: an invalid expression is
// rejected at the PROVIDER CALL, and the behavior value returned alongside the
// error is nil — so there is nothing left that could defer the fault to the
// first message. The per-provider tables cover each provider's own invalid
// forms; this one covers the invariant they share.
func TestFailAtConstruction(t *testing.T) {
	const bad = "payload." // unparseable for every environment shape

	// Each case carries its own assert closure rather than a shared construct
	// factory: the six providers have six different signatures, and the project
	// table-test rule states the closure form without exception, so the
	// repetition is deliberate.
	tests := []struct {
		name   string
		assert func(t *testing.T)
	}{
		{
			name: "Predicate",
			assert: func(t *testing.T) {
				got, err := expr.Predicate[exprOrder](bad)
				require.ErrorIs(t, err, expr.ErrInvalidExpression)
				requireNamesInvalidExpression(t, err, bad)
				require.Nil(t, got, noValueEscapes)
			},
		},
		{
			name: "RouteFunc",
			assert: func(t *testing.T) {
				got, err := expr.RouteFunc[exprOrder](bad, map[string]msgin.MessageChannel{"x": &collector{}})
				require.ErrorIs(t, err, expr.ErrInvalidExpression)
				requireNamesInvalidExpression(t, err, bad)
				require.Nil(t, got, noValueEscapes)
			},
		},
		{
			name: "Transformer",
			assert: func(t *testing.T) {
				got, err := expr.Transformer[exprOrder, int](bad)
				require.ErrorIs(t, err, expr.ErrInvalidExpression)
				requireNamesInvalidExpression(t, err, bad)
				require.Nil(t, got, noValueEscapes)
			},
		},
		{
			name: "SplitFunc",
			assert: func(t *testing.T) {
				got, err := expr.SplitFunc[exprBatch, exprItem](bad)
				require.ErrorIs(t, err, expr.ErrInvalidExpression)
				requireNamesInvalidExpression(t, err, bad)
				require.Nil(t, got, noValueEscapes)
			},
		},
		{
			name: "Correlation",
			assert: func(t *testing.T) {
				got, err := expr.Correlation[exprOrder](bad)
				require.ErrorIs(t, err, expr.ErrInvalidExpression)
				requireNamesInvalidExpression(t, err, bad)
				require.Nil(t, got, noValueEscapes)
			},
		},
		{
			name: "Release",
			assert: func(t *testing.T) {
				got, err := expr.Release[exprOrder](bad)
				require.ErrorIs(t, err, expr.ErrInvalidExpression)
				requireNamesInvalidExpression(t, err, bad)
				require.Nil(t, got, noValueEscapes)
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) { tc.assert(t) })
	}
}

// noValueEscapes is the shared failure message for TestFailAtConstruction's nil
// check — the invariant every provider shares.
const noValueEscapes = "no behavior value escapes construction, so the fault cannot be deferred to first message"

func TestPredicate(t *testing.T) {
	tests := []struct {
		name   string
		assert func(t *testing.T)
	}{
		{
			name: "invalid expression is ErrInvalidExpression at construction",
			assert: func(t *testing.T) {
				const src = "payload.Amount >" // syntax error
				_, err := expr.Predicate[exprOrder](src)
				require.ErrorIs(t, err, expr.ErrInvalidExpression)
				requireNamesInvalidExpression(t, err, src)
			},
		},
		{
			name: "empty expression is ErrInvalidExpression",
			assert: func(t *testing.T) {
				_, err := expr.Predicate[exprOrder]("   ")
				require.ErrorIs(t, err, expr.ErrInvalidExpression)
			},
		},
		{
			name: "non-bool result is rejected at construction (AsBool)",
			assert: func(t *testing.T) {
				_, err := expr.Predicate[exprOrder]("payload.Amount") // int, not bool
				require.ErrorIs(t, err, expr.ErrInvalidExpression)
			},
		},
		{
			name: "unknown field is rejected at construction (type-check against A)",
			assert: func(t *testing.T) {
				_, err := expr.Predicate[exprOrder]("payload.Nope > 1")
				require.ErrorIs(t, err, expr.ErrInvalidExpression)
			},
		},
		{
			name: "true predicate passes the message through",
			assert: func(t *testing.T) {
				pred, err := expr.Predicate[exprOrder](`payload.Amount > 100 && payload.Currency == "USD"`)
				require.NoError(t, err)
				require.True(t, runStep(t, routing.Filter(pred), msgin.New[any](exprOrder{Amount: 150, Currency: "USD"})))
			},
		},
		{
			name: "false predicate drops (silent) and can divert to WithDiscardChannel",
			assert: func(t *testing.T) {
				// Composition moved to the CALL SITE: the provider takes no
				// option variadic, so the option goes to routing.Filter.
				disc := &collector{}
				pred, err := expr.Predicate[exprOrder](`payload.Amount > 100`)
				require.NoError(t, err)
				step := routing.Filter(pred, routing.WithDiscardChannel(disc))
				require.False(t, runStep(t, step, msgin.New[any](exprOrder{Amount: 5})))
				require.Equal(t, 1, disc.count())
			},
		},
		{
			name: "header access via header()",
			assert: func(t *testing.T) {
				pred, err := expr.Predicate[exprOrder](`header("vip") == true`)
				require.NoError(t, err)
				m := msgin.New[any](exprOrder{}).WithHeader("vip", true)
				require.True(t, runStep(t, routing.Filter(pred), m))
			},
		},
		{
			name: "runtime eval error propagates as the handler error, naming the expression",
			assert: func(t *testing.T) {
				// A missing header is nil; ordering a nil against an int is a real
				// expr eval error. NB: divide-by-zero does NOT error in expr.
				const src = `header("missing") > 100`
				pred, err := expr.Predicate[exprOrder](src)
				require.NoError(t, err) // compiles fine (header() is any)
				err = routing.Filter(pred)(discardStep()).Handle(t.Context(), msgin.New[any](exprOrder{}))
				require.Error(t, err) // the eval error surfaces to the runtime
				requireNamesExpression(t, err, src)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) { tt.assert(t) })
	}
}

// predicateRun drives a single message through a Step to a terminal recording
// handler and returns the pass/fail outcome plus any handler error. Unlike
// runStep it takes no *testing.T and calls no require/t methods, so it is safe
// to invoke from worker goroutines (testing.T's Fatal-family methods, which
// require uses internally, must only be called from the test's own goroutine).
func predicateRun(ctx context.Context, step msgin.Step, m msgin.Message[any]) (passed bool, err error) {
	next := msgin.HandlerFunc(func(context.Context, msgin.Message[any]) error { passed = true; return nil })
	err = step(next).Handle(ctx, m)
	return passed, err
}

// TestPredicate_Concurrent proves the *vm.Program compiled once by
// expr.Predicate is safe to invoke from multiple goroutines concurrently — the
// shape a Consumer with WithConcurrency(N>1) drives it in production. expr
// documents *vm.Program as concurrent-safe (vm.Run spins up a fresh VM per
// call); this test exercises that guarantee under -race with a shared, single
// Predicate.
func TestPredicate_Concurrent(t *testing.T) {
	pred, err := expr.Predicate[exprOrder](`payload.Amount > 100 && header("vip") == true`)
	require.NoError(t, err)
	step := routing.Filter(pred)

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
				passed, err := predicateRun(ctx, step, v.msg)
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

// ExamplePredicate shows the two-line construct-then-check pattern: the
// provider returns (routing.Predicate[A], error), so the caller constructs it,
// checks the error, then hands it to routing.Filter like any Go-func
// predicate. A is NOT inferable from the string, so it is supplied explicitly.
func ExamplePredicate() {
	pred, err := expr.Predicate[exprOrder]("payload.Amount > 100")
	if err != nil {
		fmt.Println("invalid expression:", err)
		return
	}

	var passed []int
	flow := msgin.Chain(
		routing.Filter(pred),
		endpoint.Consume(func(_ context.Context, m msgin.Message[exprOrder]) error {
			passed = append(passed, m.Payload().Amount)
			return nil
		}),
	)

	_ = flow.Handle(context.Background(), msgin.New[any](exprOrder{Amount: 150, Currency: "USD"}))
	_ = flow.Handle(context.Background(), msgin.New[any](exprOrder{Amount: 50, Currency: "USD"})) // filtered out

	fmt.Println(passed)
	// Output: [150]
}

// ExampleRouteFunc routes on a runtime routing-key expression instead of a Go
// func, dispatching each message to the msgin.MessageChannel mapped from
// payload.Currency.
func ExampleRouteFunc() {
	eu, us := &collector{}, &collector{}
	pick, err := expr.RouteFunc[exprOrder]("payload.Currency", map[string]msgin.MessageChannel{
		"EUR": eu,
		"USD": us,
	})
	if err != nil {
		fmt.Println("invalid expression:", err)
		return
	}
	router := routing.NewRouter(pick)

	_ = router.Handle(context.Background(), msgin.New[any](exprOrder{Amount: 10, Currency: "EUR"}))
	_ = router.Handle(context.Background(), msgin.New[any](exprOrder{Amount: 20, Currency: "USD"}))

	fmt.Println(eu.count(), us.count())
	// Output: 1 1
}

func TestTransformer(t *testing.T) {
	tests := []struct {
		name   string
		assert func(t *testing.T)
	}{
		{
			name: "empty expression is ErrInvalidExpression at construction",
			assert: func(t *testing.T) {
				_, err := expr.Transformer[exprOrder, int]("   ")
				require.ErrorIs(t, err, expr.ErrInvalidExpression)
			},
		},
		{
			name: "unparseable (non-empty) expression is ErrInvalidExpression (the Compile-failure branch)",
			assert: func(t *testing.T) {
				_, err := expr.Transformer[exprOrder, int]("payload.")
				require.ErrorIs(t, err, expr.ErrInvalidExpression)
			},
		},
		{
			name: "scalar projection propagates parent headers via WithPayload",
			assert: func(t *testing.T) {
				tf, err := expr.Transformer[exprOrder, int]("payload.Amount * 2")
				require.NoError(t, err)
				var got msgin.Message[any]
				next := msgin.HandlerFunc(func(_ context.Context, m msgin.Message[any]) error { got = m; return nil })
				parent := msgin.New[any](exprOrder{Amount: 21}).WithHeader(msgin.HeaderCorrelationID, "c1")
				require.NoError(t, transform.Transform(tf)(next).Handle(t.Context(), parent))
				require.Equal(t, 42, got.Payload())
				cid, _ := got.Header(msgin.HeaderCorrelationID)
				require.Equal(t, "c1", cid) // WithPayload copied the parent's headers
			},
		},
		{
			name: "non-B result is msgin.ErrPayloadType at eval, permanent without a Permanent wrap (D-K)",
			assert: func(t *testing.T) {
				// A=any -> no compile-time result type-check; expr yields int, B=string.
				const src = "1 + 1"
				tf, err := expr.Transformer[any, string](src)
				require.NoError(t, err)
				err = transform.Transform(tf)(discardStep()).Handle(t.Context(), msgin.New[any](0))
				require.ErrorIs(t, err, msgin.ErrPayloadType)
				require.True(t, msgin.IsPermanent(err), "ErrPayloadType is already inside IsPermanent — D-K needs no Permanent wrap")
				require.ErrorContains(t, err, "expr result int is not string",
					"the error string carries the expression-side discriminator and both type names")
				require.ErrorContains(t, err, fmt.Sprintf("(expression %q)", src))
			},
		},
		{
			name: "numeric ceiling: int-yielding expr with B=int64 is msgin.ErrPayloadType",
			assert: func(t *testing.T) {
				// expr integer math yields Go int, not int64; out.(int64) fails.
				// Documents the numeric-B ceiling as a tested contract.
				tf, err := expr.Transformer[exprOrder, int64]("payload.Amount")
				require.NoError(t, err)
				err = transform.Transform(tf)(discardStep()).Handle(t.Context(), msgin.New[any](exprOrder{Amount: 7}))
				require.ErrorIs(t, err, msgin.ErrPayloadType)
			},
		},
		{
			name: "runtime eval error propagates, naming the expression",
			assert: func(t *testing.T) {
				const src = `header("missing") > 100`
				tf, err := expr.Transformer[exprOrder, any](src)
				require.NoError(t, err)
				err = transform.Transform(tf)(discardStep()).Handle(t.Context(), msgin.New[any](exprOrder{}))
				require.Error(t, err)
				requireNamesExpression(t, err, src)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) { tt.assert(t) })
	}
}

// ExampleTransformer projects a payload field via a runtime expression instead
// of a Go func. Like every provider here it returns (T, error): construct it,
// check the error, then hand it to transform.Transform. A and B are NOT
// inferable from the string, so both are supplied explicitly.
func ExampleTransformer() {
	tf, err := expr.Transformer[exprOrder, int]("payload.Amount * 2")
	if err != nil {
		fmt.Println("invalid expression:", err)
		return
	}

	var doubled []int
	flow := msgin.Chain(
		transform.Transform(tf),
		endpoint.Consume(func(_ context.Context, m msgin.Message[int]) error {
			doubled = append(doubled, m.Payload())
			return nil
		}),
	)

	_ = flow.Handle(context.Background(), msgin.New[any](exprOrder{Amount: 21, Currency: "USD"}))

	fmt.Println(doubled)
	// Output: [42]
}

func TestSplitFunc(t *testing.T) {
	tests := []struct {
		name   string
		assert func(t *testing.T)
	}{
		{
			name: "empty expression is ErrInvalidExpression at construction",
			assert: func(t *testing.T) {
				_, err := expr.SplitFunc[exprBatch, exprItem]("   ")
				require.ErrorIs(t, err, expr.ErrInvalidExpression)
			},
		},
		{
			name: "unparseable expression is ErrInvalidExpression at construction",
			assert: func(t *testing.T) {
				_, err := expr.SplitFunc[exprBatch, exprItem]("payload.")
				require.ErrorIs(t, err, expr.ErrInvalidExpression)
			},
		},
		{
			name: "a non-A payload is ErrPayloadType from routing.Split",
			assert: func(t *testing.T) {
				sf, err := expr.SplitFunc[exprBatch, exprItem]("payload.Items")
				require.NoError(t, err)
				err = routing.Split(sf)(discardStep()).Handle(t.Context(), msgin.New[any](42)) // wrong payload type
				require.ErrorIs(t, err, msgin.ErrPayloadType)
			},
		},
		{
			name: "slice-yielding expr fans out N children in order with sequence headers stamped",
			assert: func(t *testing.T) {
				sf, err := expr.SplitFunc[exprBatch, exprItem]("payload.Items")
				require.NoError(t, err)
				var forwarded []msgin.Message[any]
				next := msgin.HandlerFunc(func(_ context.Context, m msgin.Message[any]) error {
					forwarded = append(forwarded, m)
					return nil
				})
				parent := msgin.New[any](exprBatch{Items: []exprItem{{Name: "a"}, {Name: "b"}}}, msgin.WithID("parent-1"))
				require.NoError(t, routing.Split(sf)(next).Handle(t.Context(), parent))
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
				sf, err := expr.SplitFunc[exprBatch, exprItem]("payload.Items")
				require.NoError(t, err)
				var forwarded []msgin.Message[any]
				next := msgin.HandlerFunc(func(_ context.Context, m msgin.Message[any]) error {
					forwarded = append(forwarded, m)
					return nil
				})
				require.NoError(t, routing.Split(sf)(next).Handle(t.Context(), msgin.New[any](exprBatch{})))
				require.Empty(t, forwarded)
			},
		},
		{
			name: "non-slice result is msgin.ErrPayloadType",
			assert: func(t *testing.T) {
				const src = "payload.Items[0]" // yields a struct, not a slice
				sf, err := expr.SplitFunc[exprBatch, exprItem](src)
				require.NoError(t, err)
				parent := msgin.New[any](exprBatch{Items: []exprItem{{Name: "a"}}})
				err = routing.Split(sf)(discardStep()).Handle(t.Context(), parent)
				require.ErrorIs(t, err, msgin.ErrPayloadType)
				require.True(t, msgin.IsPermanent(err))
				require.ErrorContains(t, err, "expr result expr_test.exprItem is not a slice")
				require.ErrorContains(t, err, fmt.Sprintf("(expression %q)", src))
			},
		},
		{
			name: "non-B element is msgin.ErrPayloadType",
			assert: func(t *testing.T) {
				// A=any -> no compile-time result type-check; the literal yields a
				// mixed []interface{} whose 2nd element cannot be asserted to B=int.
				const src = `[1, "two"]`
				sf, err := expr.SplitFunc[any, int](src)
				require.NoError(t, err)
				err = routing.Split(sf)(discardStep()).Handle(t.Context(), msgin.New[any](0))
				require.ErrorIs(t, err, msgin.ErrPayloadType)
				require.True(t, msgin.IsPermanent(err))
				require.ErrorContains(t, err, "expr result element 1 string is not int",
					"the offending element INDEX is named, not just the type")
				require.ErrorContains(t, err, fmt.Sprintf("(expression %q)", src))
			},
		},
		{
			name: "runtime eval error propagates, naming the expression",
			assert: func(t *testing.T) {
				const src = `header("missing") > 100`
				sf, err := expr.SplitFunc[exprBatch, exprItem](src)
				require.NoError(t, err) // compiles fine (header() is any)
				err = routing.Split(sf)(discardStep()).Handle(t.Context(), msgin.New[any](exprBatch{}))
				require.Error(t, err)
				requireNamesExpression(t, err, src)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) { tt.assert(t) })
	}
}

// ExampleSplitFunc fans a batch into its items via a runtime expression instead
// of a Go func, forwarding each item with its 1-based sequence position out of
// the total — the headers a downstream Aggregator uses to reassemble the group.
func ExampleSplitFunc() {
	sf, err := expr.SplitFunc[exprBatch, exprItem]("payload.Items")
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

	// routing.Split returns a msgin.Step: step(next) yields the handler wired to next.
	h := routing.Split(sf)(emit)
	batch := exprBatch{Items: []exprItem{{Name: "a"}, {Name: "b"}}}
	_ = h.Handle(context.Background(), msgin.New[any](batch))

	// Output:
	// item 1/2: {a}
	// item 2/2: {b}
}

func TestRouteFunc(t *testing.T) {
	tests := []struct {
		name   string
		assert func(t *testing.T)
	}{
		{
			name: "bad key expression is ErrInvalidExpression",
			assert: func(t *testing.T) {
				eu := &collector{}
				// Non-empty routes so the keyExpr COMPILE-error branch is reached
				// (an empty map would trip the len(routes)==0 guard first).
				_, err := expr.RouteFunc[exprOrder]("payload.", map[string]msgin.MessageChannel{"x": eu})
				require.ErrorIs(t, err, expr.ErrInvalidExpression)
			},
		},
		{
			name: "a non-string key expression is rejected at construction (AsKind(String))",
			assert: func(t *testing.T) {
				eu := &collector{}
				_, err := expr.RouteFunc[exprOrder]("payload.Amount", map[string]msgin.MessageChannel{"x": eu})
				require.ErrorIs(t, err, expr.ErrInvalidExpression)
			},
		},
		{
			name: "construction validation 1: nil/empty routes is ErrInvalidExpression",
			assert: func(t *testing.T) {
				_, err := expr.RouteFunc[exprOrder]("payload.Currency", nil)
				require.ErrorIs(t, err, expr.ErrInvalidExpression)
				require.Contains(t, err.Error(), "routes")
			},
		},
		{
			name: "construction validation 2: a nil channel value in routes is ErrInvalidExpression",
			assert: func(t *testing.T) {
				_, err := expr.RouteFunc[exprOrder]("payload.Currency",
					map[string]msgin.MessageChannel{"EUR": nil})
				require.ErrorIs(t, err, expr.ErrInvalidExpression)
				require.Contains(t, err.Error(), `"EUR"`, "the offending route key is named")
			},
		},
		{
			name: "a non-A payload is ErrPayloadType from the RouteFunc",
			assert: func(t *testing.T) {
				eu := &collector{}
				const src = "payload.Currency"
				pick, err := expr.RouteFunc[exprOrder](src, map[string]msgin.MessageChannel{"EUR": eu})
				require.NoError(t, err)
				// Wrong payload type: A = exprOrder, message carries an int.
				_, err = pick(t.Context(), msgin.New[any](42))
				require.ErrorIs(t, err, msgin.ErrPayloadType)
				require.True(t, msgin.IsPermanent(err))
				require.ErrorContains(t, err, fmt.Sprintf("(expression %q)", src),
					"a dead-letter record must say WHICH router rejected the payload")
			},
		},
		{
			name: "eval error propagates from Handle, naming the expression",
			assert: func(t *testing.T) {
				eu := &collector{}
				// header("missing") is nil; nil + string is a real expr eval error.
				const src = `header("missing") + payload.Currency`
				pick, err := expr.RouteFunc[exprOrder](src, map[string]msgin.MessageChannel{"EUR": eu})
				require.NoError(t, err) // compiles fine (header() is any)
				err = routing.NewRouter(pick).Handle(t.Context(), msgin.New[any](exprOrder{Currency: "EUR"}))
				require.Error(t, err)
				requireNamesExpression(t, err, src)
			},
		},
		{
			name: "key hit routes to the mapped channel",
			assert: func(t *testing.T) {
				eu, us := &collector{}, &collector{}
				pick, err := expr.RouteFunc[exprOrder]("payload.Currency",
					map[string]msgin.MessageChannel{"EUR": eu, "USD": us})
				require.NoError(t, err)
				r := routing.NewRouter(pick)
				require.NoError(t, r.Handle(t.Context(), msgin.New[any](exprOrder{Currency: "EUR"})))
				require.Equal(t, 1, eu.count())
				require.Zero(t, us.count())
			},
		},
		{
			name: "miss with default routes to default; without default is ErrNoRoute",
			assert: func(t *testing.T) {
				eu, def := &collector{}, &collector{}
				pick, err := expr.RouteFunc[exprOrder]("payload.Currency",
					map[string]msgin.MessageChannel{"EUR": eu})
				require.NoError(t, err)

				withDef := routing.NewRouter(pick, routing.WithDefaultChannel(def))
				require.NoError(t, withDef.Handle(t.Context(), msgin.New[any](exprOrder{Currency: "GBP"})))
				require.Equal(t, 1, def.count())

				noDef := routing.NewRouter(pick)
				require.ErrorIs(t, noDef.Handle(t.Context(), msgin.New[any](exprOrder{Currency: "GBP"})), msgin.ErrNoRoute)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) { tt.assert(t) })
	}
}

// exprGroupItem is the Aggregator group-member payload type the Correlation/
// Release cases correlate and release against: Region drives
// correlation-by-field, Amount is summed by sumAmountFn.
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

// sumFn aggregates a group of int payloads into their sum.
func sumFn(_ context.Context, group []msgin.Message[int]) (msgin.Message[int], error) {
	sum := 0
	for _, m := range group {
		sum += m.Payload()
	}
	return msgin.New(sum), nil
}

// corrMsg builds a correlated group member: payload, id, correlation key and
// any extra headers.
func corrMsg(payload any, id, corr string, headers map[string]any) msgin.Message[any] {
	h := map[string]any{msgin.HeaderCorrelationID: corr}
	for k, v := range headers {
		h[k] = v
	}
	return msgin.New[any](payload, msgin.WithID(id), msgin.WithHeaders(h))
}

func newGroupStore(t *testing.T) msgin.MessageGroupStore {
	t.Helper()
	store, err := memory.NewGroupStore()
	require.NoError(t, err)
	return store
}

// TestCorrelation covers the provider's construction-time compile error, the
// grouping-by-evaluated-key happy path, the non-A payload branch, a runtime
// eval error propagating from Handle, and the empty-evaluated-key ->
// Permanent(ErrNoCorrelation) symmetry with the Aggregator's default strategy.
func TestCorrelation(t *testing.T) {
	tests := []struct {
		name   string
		assert func(t *testing.T)
	}{
		{
			name: "bad expression is ErrInvalidExpression at the provider call, not at NewAggregator",
			assert: func(t *testing.T) {
				corr, err := expr.Correlation[exprGroupItem]("payload.")
				require.ErrorIs(t, err, expr.ErrInvalidExpression)
				require.Nil(t, corr, "ADR 0019: an invalid expression fails at CONSTRUCTION, never at first message")
			},
		},
		{
			name: "empty expression is ErrInvalidExpression",
			assert: func(t *testing.T) {
				_, err := expr.Correlation[exprGroupItem]("   ")
				require.ErrorIs(t, err, expr.ErrInvalidExpression)
			},
		},
		{
			name: "a non-string key expression is rejected at construction (AsKind(String))",
			assert: func(t *testing.T) {
				_, err := expr.Correlation[exprGroupItem]("payload.Amount")
				require.ErrorIs(t, err, expr.ErrInvalidExpression)
			},
		},
		{
			name: "a good expression groups messages by the evaluated key",
			assert: func(t *testing.T) {
				corr, err := expr.Correlation[exprGroupItem]("payload.Region")
				require.NoError(t, err)
				out := &collector{}
				agg, err := routing.NewAggregator[exprGroupItem, int](newGroupStore(t), sumAmountFn,
					routing.WithOutputChannel(out),
					routing.WithCorrelationStrategy(corr),
					routing.WithCompletionSize(2))
				require.NoError(t, err)

				eu1 := msgin.New[any](exprGroupItem{Amount: 1, Region: "EU"})
				us1 := msgin.New[any](exprGroupItem{Amount: 10, Region: "US"})
				eu2 := msgin.New[any](exprGroupItem{Amount: 2, Region: "EU"})

				require.NoError(t, agg.Handle(t.Context(), eu1))
				require.NoError(t, agg.Handle(t.Context(), us1))
				require.Zero(t, out.count(), "each key holds only 1 member so far")

				require.NoError(t, agg.Handle(t.Context(), eu2))
				require.Equal(t, 1, out.count(), "the EU group reached size 2 and released; US is untouched")
				require.Equal(t, 3, out.last().Payload(), "EU group: 1 + 2")
			},
		},
		{
			name: "a non-A payload is ErrPayloadType",
			assert: func(t *testing.T) {
				const src = "payload.Region"
				corr, err := expr.Correlation[exprGroupItem](src)
				require.NoError(t, err)
				_, err = corr(msgin.New[any](42))
				require.ErrorIs(t, err, msgin.ErrPayloadType)
				require.True(t, msgin.IsPermanent(err))
				require.ErrorContains(t, err, fmt.Sprintf("(expression %q)", src),
					"a dead-letter record must say WHICH correlation expression rejected the payload")
			},
		},
		{
			name: "a runtime eval error surfaces from Handle, naming the expression",
			assert: func(t *testing.T) {
				// header("missing") is nil; nil + string is a real expr eval error.
				const src = `header("missing") + payload.Region`
				corr, err := expr.Correlation[exprGroupItem](src)
				require.NoError(t, err) // compiles fine (header() is any)
				out := &collector{}
				agg, err := routing.NewAggregator[exprGroupItem, int](newGroupStore(t), sumAmountFn,
					routing.WithOutputChannel(out),
					routing.WithCorrelationStrategy(corr))
				require.NoError(t, err)

				err = agg.Handle(t.Context(), msgin.New[any](exprGroupItem{Region: "EU"}))
				require.Error(t, err)
				require.NotErrorIs(t, err, msgin.ErrNoCorrelation)
				requireNamesExpression(t, err, src)
			},
		},
		{
			name: "an empty evaluated key is Permanent(ErrNoCorrelation)",
			assert: func(t *testing.T) {
				corr, err := expr.Correlation[exprGroupItem](`""`)
				require.NoError(t, err)
				out := &collector{}
				agg, err := routing.NewAggregator[exprGroupItem, int](newGroupStore(t), sumAmountFn,
					routing.WithOutputChannel(out),
					routing.WithCorrelationStrategy(corr))
				require.NoError(t, err)

				err = agg.Handle(t.Context(), msgin.New[any](exprGroupItem{Region: "EU"}))
				require.ErrorIs(t, err, msgin.ErrNoCorrelation)
				require.True(t, msgin.IsPermanent(err),
					"must be Permanent so the runtime diverts it rather than retrying a key that cannot change")
				require.Zero(t, out.count())
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) { tc.assert(t) })
	}
}

// TestRelease covers the provider's construction-time compile error, the
// size-gated canonical form vs. the un-gated footgun, a member header()
// predicate, a runtime eval error propagating from Handle instead of
// collapsing into a permanent false (which is what routing.ReleaseStrategy's
// (bool, error) shape buys — decision D-E), and toGroupEnv's two guards
// (empty group; non-A member).
func TestRelease(t *testing.T) {
	tests := []struct {
		name   string
		assert func(t *testing.T)
	}{
		{
			name: "empty expression is ErrInvalidExpression at the provider call",
			assert: func(t *testing.T) {
				rel, err := expr.Release[int]("   ")
				require.ErrorIs(t, err, expr.ErrInvalidExpression)
				require.Nil(t, rel)
			},
		},
		{
			name: "unparseable (non-empty) expression is ErrInvalidExpression",
			assert: func(t *testing.T) {
				_, err := expr.Release[int]("payload.")
				require.ErrorIs(t, err, expr.ErrInvalidExpression)
			},
		},
		{
			name: "a non-bool group expression is rejected at construction (AsBool)",
			assert: func(t *testing.T) {
				_, err := expr.Release[int]("len(messages)")
				require.ErrorIs(t, err, expr.ErrInvalidExpression)
			},
		},
		{
			name: "size-gated canonical form releases at the sequence size",
			assert: func(t *testing.T) {
				rel, err := expr.Release[int]("size > 0 && len(messages) >= size")
				require.NoError(t, err)
				out := &collector{}
				agg, err := routing.NewAggregator[int, int](newGroupStore(t), sumFn,
					routing.WithOutputChannel(out),
					routing.WithReleaseStrategy(rel))
				require.NoError(t, err)

				m1 := corrMsg(1, "m1", "g", map[string]any{msgin.HeaderSequenceSize: 2})
				m2 := corrMsg(2, "m2", "g", map[string]any{msgin.HeaderSequenceSize: 2})
				require.NoError(t, agg.Handle(t.Context(), m1))
				require.Zero(t, out.count(), "held: only 1 of 2 members so far")
				require.NoError(t, agg.Handle(t.Context(), m2))
				require.Equal(t, 1, out.count())
				require.Equal(t, 3, out.last().Payload())
			},
		},
		{
			name: "un-gated form (the documented footgun) releases a singleton with no HeaderSequenceSize present",
			assert: func(t *testing.T) {
				rel, err := expr.Release[int]("len(messages) >= size")
				require.NoError(t, err)
				out := &collector{}
				agg, err := routing.NewAggregator[int, int](newGroupStore(t), sumFn,
					routing.WithOutputChannel(out),
					routing.WithReleaseStrategy(rel))
				require.NoError(t, err)

				// size defaults to 0 (no HeaderSequenceSize header): len(messages)
				// >= 0 is trivially true from the very first member.
				require.NoError(t, agg.Handle(t.Context(), corrMsg(1, "m1", "g", nil)))
				require.Equal(t, 1, out.count(), "released a group-of-one")
				require.Equal(t, 1, out.last().Payload())
			},
		},
		{
			name: "a member header() predicate releases",
			assert: func(t *testing.T) {
				rel, err := expr.Release[int](`messages[0].header("priority") == "high"`)
				require.NoError(t, err)
				out := &collector{}
				agg, err := routing.NewAggregator[int, int](newGroupStore(t), sumFn,
					routing.WithOutputChannel(out),
					routing.WithReleaseStrategy(rel))
				require.NoError(t, err)

				require.NoError(t, agg.Handle(t.Context(), corrMsg(5, "m1", "g", map[string]any{"priority": "high"})))
				require.Equal(t, 1, out.count())
				require.Equal(t, 5, out.last().Payload())
			},
		},
		{
			name: "a runtime eval error propagates from Handle instead of reading as a permanent false (D-E)",
			assert: func(t *testing.T) {
				// no "qty" header on m1: header() is nil, nil > 0 is a real expr
				// eval error.
				const src = `messages[0].header("qty") > 0`
				rel, err := expr.Release[int](src)
				require.NoError(t, err)
				out := &collector{}
				agg, err := routing.NewAggregator[int, int](newGroupStore(t), sumFn,
					routing.WithOutputChannel(out),
					routing.WithReleaseStrategy(rel))
				require.NoError(t, err)

				err = agg.Handle(t.Context(), corrMsg(1, "m1", "g", nil))
				require.Error(t, err)
				requireNamesExpression(t, err, src)
				require.Zero(t, out.count())
			},
		},
		{
			name: "an empty group snapshot evaluates against a zero env and never releases",
			assert: func(t *testing.T) {
				rel, err := expr.Release[int]("size > 0 && len(messages) >= size")
				require.NoError(t, err)
				ok, err := rel(fakeGroup{key: "g"})
				require.NoError(t, err, "the empty-group guard returns before any member is asserted")
				require.False(t, ok)
			},
		},
		{
			name: "the sequence-size header is read number-tolerantly: int, int64, float64, else 0",
			assert: func(t *testing.T) {
				// size comes from the FIRST member's HeaderSequenceSize, and a
				// durable GroupStore round-trips it through JSON, so a float64
				// is as ordinary a shape as a live in-process int. A header
				// this reader cannot parse must degrade to 0, not error — 0 is
				// what makes the canonical `size > 0 && …` gate hold closed.
				rel, err := expr.Release[int]("size == 2")
				require.NoError(t, err)
				sized := func(v any) fakeGroup {
					return fakeGroup{key: "g", msgs: []msgin.Message[any]{
						msgin.New[any](1, msgin.WithHeaders(map[string]any{msgin.HeaderSequenceSize: v})),
					}}
				}
				for _, v := range []any{2, int64(2), float64(2)} {
					ok, err := rel(sized(v))
					require.NoError(t, err)
					require.True(t, ok, "size must read as 2 from a %T header", v)
				}
				zero, err := expr.Release[int]("size == 0")
				require.NoError(t, err)
				for _, v := range []any{"2", nil, uint(2)} {
					ok, err := zero(sized(v))
					require.NoError(t, err)
					require.True(t, ok, "an unparseable %T header must degrade to size 0", v)
				}
			},
		},
		{
			name: "a NIL group does not panic: the strategy is a public value a caller can invoke directly",
			assert: func(t *testing.T) {
				// expr.Release hands the caller a routing.ReleaseStrategy they
				// hold, so rel(nil) is caller input reaching library code — and
				// CLAUDE.md forbids panicking on caller input. It reads as
				// "nothing to release", the same answer as an empty group.
				rel, err := expr.Release[int]("size > 0 && len(messages) >= size")
				require.NoError(t, err)
				require.NotPanics(t, func() {
					ok, err := rel(nil)
					require.NoError(t, err)
					require.False(t, ok)
				})
			},
		},
		{
			name: "a non-A member is msgin.ErrPayloadType, permanent, naming the expression",
			assert: func(t *testing.T) {
				const src = "len(messages) >= 1"
				rel, err := expr.Release[int](src)
				require.NoError(t, err)
				g := fakeGroup{key: "g", msgs: []msgin.Message[any]{msgin.New[any]("not-an-int")}}
				ok, err := rel(g)
				require.ErrorIs(t, err, msgin.ErrPayloadType)
				require.True(t, msgin.IsPermanent(err))
				require.ErrorContains(t, err, fmt.Sprintf("(expression %q)", src),
					"a member-assertion failure names the expression that would have consumed it")
				require.False(t, ok)
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) { tc.assert(t) })
	}
}

// ExampleRelease correlates messages by a runtime expression
// (expr.Correlation on payload.Region) and releases each group with a runtime
// release expression (expr.Release). The release expression uses the canonical
// size-GATED form — size > 0 && len(messages) >= size — never the un-gated
// len(messages) >= size, which releases a singleton the moment no
// HeaderSequenceSize header is present (a documented footgun; see Release's
// godoc). Both providers need an explicit type argument: A is not inferable
// from a string.
func ExampleRelease() {
	corr, err := expr.Correlation[exprGroupItem]("payload.Region")
	if err != nil {
		panic(err)
	}
	rel, err := expr.Release[exprGroupItem]("size > 0 && len(messages) >= size")
	if err != nil {
		panic(err)
	}

	store, err := memory.NewGroupStore()
	if err != nil {
		panic(err)
	}

	out := &collector{}
	agg, err := routing.NewAggregator[exprGroupItem, int](
		store, sumAmountFn,
		routing.WithOutputChannel(out),
		routing.WithCorrelationStrategy(corr),
		routing.WithReleaseStrategy(rel),
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

	fmt.Printf("region total: %v\n", out.last().Payload())
	// Output:
	// region total: 35
}
