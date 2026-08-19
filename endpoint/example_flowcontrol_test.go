package endpoint_test

import (
	"context"
	"fmt"

	"github.com/kartaladev/msgin"
	"github.com/kartaladev/msgin/adapter/memory"
	"github.com/kartaladev/msgin/endpoint"
	"github.com/kartaladev/msgin/resilience"
)

// ExampleConsumer_flowControl composes the flood defenses: bounded in-flight, a
// rate limit, a handler timeout, a circuit breaker, and an overflow policy.
func ExampleConsumer_flowControl() {
	rl, _ := resilience.NewTokenBucket(100, 10)
	cb, _ := resilience.NewCircuitBreaker()
	src := memory.New(memory.WithBuffer(1))
	handler := func(context.Context, msgin.Message[string]) error { return nil }

	c, err := endpoint.NewConsumer[string](src, handler,
		endpoint.WithMaxInFlight[string](64),
		endpoint.WithRateLimit[string](rl),
		endpoint.WithHandlerTimeout[string](5_000_000_000), // 5s
		endpoint.WithCircuitBreaker[string](cb),
		endpoint.WithOverflow[string](msgin.OverflowDropNewest),
	)
	if err != nil {
		fmt.Println("construct:", err)
		return
	}
	_ = c
	fmt.Println("configured")
	// Output: configured
}
