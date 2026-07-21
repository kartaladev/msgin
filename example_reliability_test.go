package msgin_test

import (
	"context"
	"errors"
	"fmt"
	"time"

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

// ExampleWithProducerRetry shows a Producer retrying a transient outbound
// failure. The first attempt fails with a plain (transient) error, so the
// runtime waits the policy's backoff and re-sends; the second attempt succeeds,
// so nothing is dead-lettered and Send returns nil.
//
// It also shows the pairing BytesPayloadCodec exists for: with T == []byte and a
// wire adapter, the codec resolution would otherwise default to JSON and put the
// payload on the wire as a quoted base64 string ("aGVsbG8=") rather than the raw
// bytes the caller handed in.
//
// Unlike the rest of the suite, this example runs on a REAL clock with a 1 ms
// Initial — an Example cannot inject a fake clock through an // Output: block.
// The outcome is deterministic on any machine regardless: nothing here asserts
// elapsed time.
func ExampleWithProducerRetry() {
	dlq := &recordingSink{}
	attempts := 0
	flaky := outboundFunc(func(context.Context, msgin.Message[any]) error {
		attempts++
		if attempts < 2 {
			return errors.New("connection reset")
		}
		return nil
	})

	p, err := msgin.NewProducer[[]byte](flaky,
		msgin.WithProducerCodec[[]byte](msgin.BytesPayloadCodec{}),
		msgin.WithProducerRetry[[]byte](msgin.RetryPolicy{
			MaxAttempts: 3,
			Backoff:     msgin.ExponentialBackoff{Initial: time.Millisecond, Mult: 2},
			DeadLetter:  dlq,
		}),
	)
	if err != nil {
		fmt.Println("construct:", err)
		return
	}

	if err := p.Send(context.Background(), msgin.New[[]byte]([]byte("hello"))); err != nil {
		fmt.Println("send:", err)
		return
	}

	fmt.Println("attempts:", attempts)
	fmt.Println("dead-lettered:", dlq.count())
	// Output:
	// attempts: 2
	// dead-lettered: 0
}
