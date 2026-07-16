package msgin_test

import (
	"context"
	"errors"
	"fmt"

	"github.com/kartaladev/msgin"
	"github.com/kartaladev/msgin/adapter/memory"
)

// ExampleConsumer_deadLetter demonstrates the reliability engine end-to-end
// over the in-memory adapter: a handler that always returns a msgin.Permanent
// error is never retried — the runtime classifies it as permanent (spec §7,
// ADR 0007 D1) and routes the message straight to the configured
// invalid-message sink, firing OnInvalidMessage, without consuming any
// redelivery attempt.
//
// The run is bounded and deterministic: the OnInvalidMessage hook signals
// completion over a channel (no time.Sleep gating), and Run always returns
// once its context is cancelled and the drain completes (ADR 0007 D9), so
// the example shuts down cleanly and stays goleak-clean.
func ExampleConsumer_deadLetter() {
	src := memory.New(memory.WithBuffer(1))
	invalid := memory.New(memory.WithBuffer(1))

	handler := func(context.Context, msgin.Message[string]) error {
		return msgin.Permanent(errors.New("cannot process"))
	}

	routed := make(chan struct{})
	c, err := msgin.NewConsumer[string](src, handler,
		msgin.WithInvalidMessageSink[string](invalid),
		msgin.WithHooks[string](msgin.Hooks{
			OnInvalidMessage: func(context.Context, msgin.Message[any], error) {
				close(routed)
			},
		}),
	)
	if err != nil {
		fmt.Println("construct:", err)
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- c.Run(ctx) }()

	if err := src.Send(context.Background(), msgin.New[any]("order-1")); err != nil {
		fmt.Println("send:", err)
		cancel()
		<-done
		return
	}

	<-routed // deterministic: wait for the OnInvalidMessage hook to fire.
	cancel()
	<-done // Run always returns once the drain completes (ADR 0007 D9).

	fmt.Println("routed to invalid-message sink")
	// Output: routed to invalid-message sink
}
