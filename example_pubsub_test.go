package msgin_test

import (
	"context"
	"fmt"

	"github.com/kartaladev/msgin"
)

// ExamplePubSub shows topic-scoped fan-out: two subscribers on one topic both
// receive a published message; a subscriber on another topic does not.
func ExamplePubSub() {
	ps := msgin.NewPubSub()

	_, _ = ps.Subscribe("orders", msgin.HandlerFunc(func(_ context.Context, m msgin.Message[any]) error {
		fmt.Println("audit:", m.Payload())
		return nil
	}))
	_, _ = ps.Subscribe("orders", msgin.HandlerFunc(func(_ context.Context, m msgin.Message[any]) error {
		fmt.Println("ship:", m.Payload())
		return nil
	}))
	_, _ = ps.Subscribe("invoices", msgin.HandlerFunc(func(_ context.Context, m msgin.Message[any]) error {
		fmt.Println("invoice:", m.Payload())
		return nil
	}))

	_ = ps.Publish(context.Background(), "orders", msgin.New[any]("o-1"))
	// Output:
	// audit: o-1
	// ship: o-1
}
