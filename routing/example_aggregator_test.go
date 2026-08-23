package routing_test

import (
	"context"
	"errors"
	"fmt"

	"github.com/kartaladev/msgin"
	"github.com/kartaladev/msgin/adapter/memory"
	"github.com/kartaladev/msgin/channel"
	"github.com/kartaladev/msgin/routing"
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

	out := channel.NewDirectChannel()
	if _, err := out.Subscribe(msgin.HandlerFunc(func(_ context.Context, m msgin.Message[any]) error {
		fmt.Printf("order total: %v\n", m.Payload())
		return nil
	})); err != nil {
		panic(err)
	}

	agg, err := routing.NewAggregator[lineItem, int](store,
		func(_ context.Context, group []msgin.Message[lineItem]) (msgin.Message[int], error) {
			total := 0
			for _, m := range group {
				total += m.Payload().price
			}
			return msgin.New(total), nil
		},
		routing.WithOutputChannel(out),
		routing.WithCompletionSize(3),
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

// ExampleWithReleaseWhen shows what bounds a group when the release decision
// never fires. WithReleaseWhen bypasses routing's completionSizeCeiling
// entirely — that ceiling only validates WithCompletionSize's own n — so the
// group is bounded by the STORE's member cap instead. The cap is 65,536 by
// default; this example lowers it with memory.WithMaxGroupMembers so the
// boundary is reachable in a doc example, and the third member is refused with
// msgin.ErrOverflowDropped rather than growing the group.
func ExampleWithReleaseWhen() {
	store, err := memory.NewGroupStore(memory.WithMaxGroupMembers(2))
	if err != nil {
		panic(err)
	}

	out := channel.NewDirectChannel()
	if _, err := out.Subscribe(msgin.HandlerFunc(func(_ context.Context, m msgin.Message[any]) error {
		fmt.Println("released:", m.Payload())
		return nil
	})); err != nil {
		panic(err)
	}

	agg, err := routing.NewAggregator[int, int](store,
		func(_ context.Context, group []msgin.Message[int]) (msgin.Message[int], error) {
			return msgin.New(len(group)), nil
		},
		routing.WithOutputChannel(out),
		// A predicate that can never be satisfied: nothing here bounds the group.
		routing.WithReleaseWhen(func(msgin.MessageGroup) bool { return false }),
	)
	if err != nil {
		panic(err)
	}

	for i := range 3 {
		msg := msgin.New[any](i, msgin.WithID(fmt.Sprintf("m-%d", i)), msgin.WithHeaders(map[string]any{
			msgin.HeaderCorrelationID: "never-releases",
		}))
		err := agg.Handle(context.Background(), msg)
		fmt.Printf("member %d: overflow=%t permanent=%t\n",
			i, errors.Is(err, msgin.ErrOverflowDropped), msgin.IsPermanent(err))
		if err != nil {
			fmt.Printf("  %v\n", err)
		}
	}

	// Output:
	// member 0: overflow=false permanent=false
	// member 1: overflow=false permanent=false
	// member 2: overflow=true permanent=true
	//   msgin: permanent: msgin: message dropped by overflow policy: memory.GroupStore.Add: group "never-releases" holds 2 members, limit 2
}
