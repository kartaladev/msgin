package msgin_test

import (
	"context"
	"fmt"

	"github.com/kartaladev/msgin"
	"github.com/kartaladev/msgin/adapter/memory"
)

// ExampleConsumer_flowControl composes the flood defenses: bounded in-flight, a
// rate limit, a handler timeout, a circuit breaker, and an overflow policy.
func ExampleConsumer_flowControl() {
	rl, _ := msgin.NewTokenBucket(100, 10)
	src := memory.New(memory.WithBuffer(1))
	handler := func(context.Context, msgin.Message[string]) error { return nil }

	c, err := msgin.NewConsumer[string](src, handler,
		msgin.WithMaxInFlight[string](64),
		msgin.WithRateLimit[string](rl),
		msgin.WithHandlerTimeout[string](5_000_000_000), // 5s
		msgin.WithCircuitBreaker[string](msgin.NewCircuitBreaker()),
		msgin.WithOverflow[string](msgin.OverflowDropNewest),
	)
	if err != nil {
		fmt.Println("construct:", err)
		return
	}
	_ = c
	fmt.Println("configured")
	// Output: configured
}
