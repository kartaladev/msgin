package msgin_test

import (
	"context"
	"fmt"

	"github.com/kartaladev/msgin"
)

// ExampleChain shows a linear in-process flow: filter unpaid orders, translate
// Order->Invoice (preserving headers via WithPayload), charge, and deliver.
func ExampleChain() {
	type Order struct {
		ID   string
		Paid bool
	}
	type Invoice struct{ OrderID string }
	type Receipt struct{ OrderID string }

	receipts := make(chan Receipt, 1)

	flow := msgin.Chain(
		msgin.Filter(func(_ context.Context, m msgin.Message[Order]) (bool, error) { return m.Payload().Paid, nil }),
		msgin.Transform(func(_ context.Context, m msgin.Message[Order]) (msgin.Message[Invoice], error) {
			return msgin.WithPayload(m, Invoice{OrderID: m.Payload().ID}), nil
		}),
		msgin.Activate(func(_ context.Context, m msgin.Message[Invoice]) (msgin.Message[Receipt], error) {
			return msgin.WithPayload(m, Receipt{OrderID: m.Payload().OrderID}), nil
		}),
		msgin.Consume(func(_ context.Context, m msgin.Message[Receipt]) error { receipts <- m.Payload(); return nil }),
	)

	_ = flow.Handle(context.Background(), msgin.New[any](Order{ID: "o-1", Paid: true}))
	_ = flow.Handle(context.Background(), msgin.New[any](Order{ID: "o-2", Paid: false})) // filtered

	fmt.Println((<-receipts).OrderID)
	// Output: o-1
}
