package msgin_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kartaladev/msgin"
	"github.com/kartaladev/msgin/adapter/memory"
)

// A composed flow driven by NewConsumer over a LIVE-VALUE source (memory)
// inherits the runtime and processes typed payloads end-to-end (Spec 003 D8 / F3).
func TestComposition_DrivesOffConsumer(t *testing.T) {
	type Order struct {
		ID   string
		Paid bool
	}
	type Invoice struct{ OrderID string }

	src := memory.New(memory.WithBuffer(4))
	got := make(chan Invoice, 4)

	flow := msgin.Chain(
		msgin.Filter(func(_ context.Context, m msgin.Message[Order]) (bool, error) { return m.Payload().Paid, nil }),
		msgin.Transform(func(_ context.Context, m msgin.Message[Order]) (msgin.Message[Invoice], error) {
			return msgin.WithPayload(m, Invoice{OrderID: m.Payload().ID}), nil
		}),
		msgin.Consume(func(_ context.Context, m msgin.Message[Invoice]) error { got <- m.Payload(); return nil }),
	)

	consumer, err := msgin.NewConsumer[any](src, flow.Handle, msgin.WithShutdownTimeout[any](2*time.Second))
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- consumer.Run(ctx) }()

	// Send the unpaid order FIRST, the paid order SECOND. The memory Broker is
	// a single Go channel (FIFO for a single reader/writer) and the consumer's
	// default concurrency is 1 (a single worker draining workerCh strictly in
	// order), so o-2 is fully dispatched — and, if the Filter is correct,
	// dropped with no Invoice produced — before o-1 is dispatched. That makes
	// the negative assertion below race-free rather than a lucky ordering: by
	// the time o-1's Invoice arrives on got, o-2 has already been processed to
	// completion.
	require.NoError(t, src.Send(ctx, msgin.New[any](Order{ID: "o-2", Paid: false}))) // filtered out
	require.NoError(t, src.Send(ctx, msgin.New[any](Order{ID: "o-1", Paid: true})))

	select {
	case inv := <-got:
		assert.Equal(t, "o-1", inv.OrderID)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for the flow to process")
	}
	// Non-blocking: prove the unpaid order produced NO Invoice. o-2 was
	// enqueued and processed (single worker, FIFO) strictly before o-1, so if
	// the Filter had let it through, its Invoice would already be sitting in
	// got by now.
	select {
	case extra := <-got:
		t.Fatalf("unpaid order was not filtered: unexpected invoice %+v", extra)
	default:
	}

	cancel()
	select {
	case err := <-done:
		assert.ErrorIs(t, err, context.Canceled)
	case <-time.After(2 * time.Second):
		t.Fatal("consumer did not shut down")
	}
	// goleak (root TestMain) verifies no goroutine leak.
}
