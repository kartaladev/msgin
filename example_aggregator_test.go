package msgin_test

import (
	"context"
	"fmt"

	"github.com/kartaladev/msgin"
	"github.com/kartaladev/msgin/adapter/memory"
)

// lineItem is one line of an order, correlated by orderID toward a released
// order total.
type lineItem struct {
	sku   string
	price int
}

// ExampleAggregator correlates 3 line-items of one order (via the default
// HeaderCorrelationID strategy) into a single released total (via
// WithCompletionSize), and prints the aggregate.
func ExampleAggregator() {
	store, err := memory.NewGroupStore()
	if err != nil {
		panic(err)
	}

	out := msgin.NewDirectChannel()
	if err := out.Subscribe(msgin.HandlerFunc(func(_ context.Context, m msgin.Message[any]) error {
		fmt.Printf("order total: %v\n", m.Payload())
		return nil
	})); err != nil {
		panic(err)
	}

	agg, err := msgin.NewAggregator[lineItem, int](store,
		func(_ context.Context, group []msgin.Message[lineItem]) (msgin.Message[int], error) {
			total := 0
			for _, m := range group {
				total += m.Payload().price
			}
			return msgin.New(total), nil
		},
		msgin.WithOutputChannel(out),
		msgin.WithCompletionSize(3),
	)
	if err != nil {
		panic(err)
	}

	items := []lineItem{
		{sku: "widget", price: 10},
		{sku: "gadget", price: 25},
		{sku: "gizmo", price: 15},
	}
	for i, it := range items {
		msg := msgin.New[any](it, msgin.WithID(fmt.Sprintf("line-%d", i)), msgin.WithHeaders(map[string]any{
			msgin.HeaderCorrelationID: "order-1",
		}))
		if err := agg.Handle(context.Background(), msg); err != nil {
			panic(err)
		}
	}

	// Output:
	// order total: 50
}
