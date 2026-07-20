package msgin_test

import (
	"context"
	"fmt"

	"github.com/kartaladev/msgin"
)

// ExampleSplit fans a batch into its items, printing each item with its 1-based
// sequence position out of the total — the headers a downstream Aggregator uses
// to reassemble the group.
func ExampleSplit() {
	type batch struct{ items []string }

	split := msgin.Split(func(_ context.Context, m msgin.Message[batch]) ([]msgin.Message[string], error) {
		var out []msgin.Message[string]
		for _, it := range m.Payload().items {
			out = append(out, msgin.WithPayload(m, it))
		}
		return out, nil
	})

	emit := msgin.HandlerFunc(func(_ context.Context, m msgin.Message[any]) error {
		num, _ := m.Header(msgin.HeaderSequenceNumber)
		size, _ := m.Header(msgin.HeaderSequenceSize)
		fmt.Printf("item %v/%v: %v\n", num, size, m.Payload())
		return nil
	})

	// A Splitter is a Step: split(next) yields the handler wired to next.
	h := split(emit)
	_ = h.Handle(context.Background(), msgin.New[any](batch{items: []string{"a", "b", "c"}}))

	// Output:
	// item 1/3: a
	// item 2/3: b
	// item 3/3: c
}
