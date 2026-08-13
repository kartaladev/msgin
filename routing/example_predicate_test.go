package routing_test

import (
	"context"
	"fmt"

	"github.com/kartaladev/msgin"
	"github.com/kartaladev/msgin/routing"
)

// ExamplePredicate_And composes And, Or and Not into the single Predicate
// behind one Message Filter, showing that the combinators chain without a
// helper package: an order ships when it is paid AND not a test fixture, or
// when it is flagged priority regardless of the rest.
//
// It also shows the error contract. The last order trips a predicate that
// fails, and the composite reports (false, err) — never (true, err) — so a
// failed evaluation can never read as a passing decision.
func ExamplePredicate_And() {
	type order struct {
		id       string
		paid     bool
		test     bool
		priority bool
		corrupt  bool
	}

	field := func(get func(order) bool) routing.Predicate[order] {
		return func(_ context.Context, m msgin.Message[order]) (bool, error) {
			if m.Payload().corrupt {
				return true, fmt.Errorf("order %s: unreadable", m.Payload().id)
			}
			return get(m.Payload()), nil
		}
	}

	paid := field(func(o order) bool { return o.paid })
	isTest := field(func(o order) bool { return o.test })
	priority := field(func(o order) bool { return o.priority })

	// (paid AND NOT test) OR priority
	ship := paid.And(isTest.Not()).Or(priority)

	emit := msgin.HandlerFunc(func(_ context.Context, m msgin.Message[any]) error {
		fmt.Printf("ship %s\n", m.Payload().(order).id)
		return nil
	})

	// A Filter is a Step: Filter(pred)(next) yields the handler wired to next.
	h := routing.Filter(ship)(emit)

	for _, o := range []order{
		{id: "A", paid: true},
		{id: "B"},
		{id: "C", paid: true, test: true},
		{id: "D", priority: true},
		{id: "E", corrupt: true},
	} {
		if err := h.Handle(context.Background(), msgin.New[any](o)); err != nil {
			fmt.Printf("drop %s: %v\n", o.id, err)
		}
	}

	// Output:
	// ship A
	// ship D
	// drop E: order E: unreadable
}
