package msgin_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/kartaladev/msgin"
	"github.com/kartaladev/msgin/adapter/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
)

func TestMain(m *testing.M) { goleak.VerifyTestMain(m) }

// stubPolling implements only PollingSource, used to prove NewConsumer rejects
// a poll-only source with ErrUnsupportedSource (PollingSource is wired in
// Plan 004).
type stubPolling struct{}

func (stubPolling) Poll(_ context.Context, _ int) ([]msgin.Delivery, error) { return nil, nil }

// stubNeither implements neither PollingSource nor StreamingSource.
type stubNeither struct{}

// fakeStream is a StreamingSource test double that is NOT a LiveValueSource
// (a "wire" source): it emits a fixed set of deliveries once, then blocks
// until ctx is cancelled and returns ctx.Err() — mirroring the memory
// adapter's cancellable Stream shape so it never leaks a goroutine.
type fakeStream struct {
	deliveries []msgin.Delivery
}

func (f *fakeStream) Stream(ctx context.Context, out chan<- msgin.Delivery) error {
	for _, d := range f.deliveries {
		select {
		case out <- d:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	<-ctx.Done()
	return ctx.Err()
}

// settleRecorder observes whether a fake Delivery was settled via Ack or
// Nack, and (for Nack) the requeue flag passed in.
type settleRecorder struct {
	mu      sync.Mutex
	acked   bool
	nacked  bool
	requeue bool
}

func (r *settleRecorder) snapshot() (acked, nacked, requeue bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.acked, r.nacked, r.requeue
}

// newDelivery builds a msgin.Delivery wrapping payload whose Ack/Nack
// closures record the settlement on the returned settleRecorder.
func newDelivery(payload any) (msgin.Delivery, *settleRecorder) {
	rec := &settleRecorder{}
	d := msgin.Delivery{
		Msg: msgin.New[any](payload),
		Ack: func(context.Context) error {
			rec.mu.Lock()
			rec.acked = true
			rec.mu.Unlock()
			return nil
		},
		Nack: func(_ context.Context, requeue bool, _ time.Duration) error {
			rec.mu.Lock()
			rec.nacked = true
			rec.requeue = requeue
			rec.mu.Unlock()
			return nil
		},
	}
	return d, rec
}

// handlerRecorder counts handler invocations and captures the last decoded
// payload, so tests can assert the handler ran (or didn't) independently of
// the Ack/Nack outcome.
type handlerRecorder struct {
	mu      sync.Mutex
	calls   int
	payload order
}

func (r *handlerRecorder) record(p order) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls++
	r.payload = p
}

func (r *handlerRecorder) snapshot() (calls int, payload order) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls, r.payload
}

// runConsumer starts c.Run(ctx) in its own goroutine and returns a stop func
// that cancels ctx and joins the Run goroutine, so every test that starts a
// consumer can guarantee goleak sees no leaked goroutine.
func runConsumer(t *testing.T, c msgin.Consumer[order]) (stop func()) {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- c.Run(ctx) }()
	return func() {
		cancel()
		<-done
	}
}

func TestConsumer_StreamingDeliversToHandler(t *testing.T) {
	b := memory.New(memory.WithBuffer(8))
	p, err := msgin.NewProducer[order](b)
	require.NoError(t, err)
	for i := 0; i < 3; i++ {
		require.NoError(t, p.Send(t.Context(), msgin.New(order{ID: "o"})))
	}

	var (
		mu   sync.Mutex
		seen int
	)
	h := func(_ context.Context, m msgin.Message[order]) error {
		mu.Lock()
		seen++
		mu.Unlock()
		return nil
	}

	c, err := msgin.NewConsumer[order](b, h) // memory ⇒ no codec (live value)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- c.Run(ctx) }()

	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return seen == 3
	}, time.Second, 5*time.Millisecond)

	cancel()
	assert.ErrorIs(t, <-done, context.Canceled)
}

// fakeLiveStream is a StreamingSource test double that IS a LiveValueSource
// (EmitsLiveValue() == true, so NewConsumer treats it as a live-value source
// needing no codec). It emits exactly one Delivery carrying a mistyped live
// payload, then blocks until ctx is cancelled and returns ctx.Err() —
// mirroring fakeStream's shape so it never leaks a goroutine.
type fakeLiveStream struct {
	delivery msgin.Delivery
}

func (f *fakeLiveStream) Stream(ctx context.Context, out chan<- msgin.Delivery) error {
	select {
	case out <- f.delivery:
	case <-ctx.Done():
		return ctx.Err()
	}
	<-ctx.Done()
	return ctx.Err()
}

func (f *fakeLiveStream) EmitsLiveValue() bool { return true }

// TestConsumer_LiveValueTypeMismatchNacks closes the live-value decode-error
// gap: a live-value source delivering a value that is not T must never reach
// the handler and must Nack(requeue=true) — decode's live-value
// type-assertion failure branch (ErrPayloadType). Uses fakeLiveStream so the
// single mistyped delivery, and its exactly-one Nack, are asserted
// deterministically instead of via a busy-spin absence check.
func TestConsumer_LiveValueTypeMismatchNacks(t *testing.T) {
	d, srec := newDelivery("not-an-order")
	src := &fakeLiveStream{delivery: d}
	hrec := &handlerRecorder{}
	h := func(_ context.Context, m msgin.Message[order]) error {
		hrec.record(m.Payload())
		return nil
	}

	c, err := msgin.NewConsumer[order](src, h)
	require.NoError(t, err)

	stop := runConsumer(t, c)
	defer stop()

	require.Eventually(t, func() bool {
		_, nacked, _ := srec.snapshot()
		return nacked
	}, time.Second, 5*time.Millisecond)

	acked, nacked, requeue := srec.snapshot()
	assert.False(t, acked)
	assert.True(t, nacked)
	assert.True(t, requeue)
	calls, _ := hrec.snapshot()
	assert.Equal(t, 0, calls, "handler must not run on decode failure")
}

func TestNewConsumer_Validation(t *testing.T) {
	tests := []struct {
		name   string
		src    any
		opts   []msgin.ConsumerOption[order]
		assert func(t *testing.T, err error)
	}{
		{"nil source", nil, nil,
			func(t *testing.T, err error) { assert.ErrorIs(t, err, msgin.ErrNilAdapter) }},
		{"concurrency < 1", memory.New(),
			[]msgin.ConsumerOption[order]{msgin.WithConcurrency[order](0)},
			func(t *testing.T, err error) { assert.ErrorIs(t, err, msgin.ErrInvalidConcurrency) }},
		{"live-value source with codec", memory.New(),
			[]msgin.ConsumerOption[order]{msgin.WithConsumerCodec[order](msgin.JSONPayloadCodec[order]{})},
			func(t *testing.T, err error) { assert.ErrorIs(t, err, msgin.ErrUnexpectedCodec) }},
		{"polling-only source is unsupported", stubPolling{}, nil,
			func(t *testing.T, err error) { assert.ErrorIs(t, err, msgin.ErrUnsupportedSource) }},
		{"source implementing neither is unsupported", stubNeither{}, nil,
			func(t *testing.T, err error) { assert.ErrorIs(t, err, msgin.ErrUnsupportedSource) }},
		{"wire streaming source constructs with default codec", &fakeStream{}, nil,
			func(t *testing.T, err error) { assert.NoError(t, err) }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := func(context.Context, msgin.Message[order]) error { return nil }
			_, err := msgin.NewConsumer[order](tc.src, h, tc.opts...)
			tc.assert(t, err)
		})
	}
}

// TestConsumer_DecodeAndDispatchSettle exercises every wire decode branch
// (success, non-[]byte payload, bad-JSON payload) plus the handler-settle
// branches (nil ⇒ Ack, error ⇒ Nack(requeue=true), panic ⇒ recovered +
// Nack(requeue=true)) against a single-delivery fakeStream, observed via
// settleRecorder/handlerRecorder.
func TestConsumer_DecodeAndDispatchSettle(t *testing.T) {
	validJSON := []byte(`{"id":"o1","total":5}`)
	badJSON := []byte(`{not-json`)

	tests := []struct {
		name       string
		payload    any
		newHandler func(rec *handlerRecorder) msgin.Handler[order]
		assert     func(t *testing.T, hrec *handlerRecorder, srec *settleRecorder)
	}{
		{
			name:    "wire success decodes and acks",
			payload: validJSON,
			newHandler: func(rec *handlerRecorder) msgin.Handler[order] {
				return func(_ context.Context, m msgin.Message[order]) error {
					rec.record(m.Payload())
					return nil
				}
			},
			assert: func(t *testing.T, hrec *handlerRecorder, srec *settleRecorder) {
				calls, payload := hrec.snapshot()
				assert.Equal(t, 1, calls)
				assert.Equal(t, order{ID: "o1", Total: 5}, payload)
				acked, nacked, _ := srec.snapshot()
				assert.True(t, acked)
				assert.False(t, nacked)
			},
		},
		{
			name:    "wire non-[]byte payload nacks requeue",
			payload: 42, // not []byte ⇒ ErrPayloadType
			newHandler: func(rec *handlerRecorder) msgin.Handler[order] {
				return func(_ context.Context, m msgin.Message[order]) error {
					rec.record(m.Payload())
					return nil
				}
			},
			assert: func(t *testing.T, hrec *handlerRecorder, srec *settleRecorder) {
				calls, _ := hrec.snapshot()
				assert.Equal(t, 0, calls, "handler must not run on decode failure")
				acked, nacked, requeue := srec.snapshot()
				assert.False(t, acked)
				assert.True(t, nacked)
				assert.True(t, requeue)
			},
		},
		{
			name:    "wire bad json payload nacks requeue",
			payload: badJSON, // []byte but invalid JSON ⇒ ErrPayloadDecode
			newHandler: func(rec *handlerRecorder) msgin.Handler[order] {
				return func(_ context.Context, m msgin.Message[order]) error {
					rec.record(m.Payload())
					return nil
				}
			},
			assert: func(t *testing.T, hrec *handlerRecorder, srec *settleRecorder) {
				calls, _ := hrec.snapshot()
				assert.Equal(t, 0, calls, "handler must not run on decode failure")
				acked, nacked, requeue := srec.snapshot()
				assert.False(t, acked)
				assert.True(t, nacked)
				assert.True(t, requeue)
			},
		},
		{
			name:    "handler error nacks requeue",
			payload: validJSON,
			newHandler: func(rec *handlerRecorder) msgin.Handler[order] {
				return func(_ context.Context, m msgin.Message[order]) error {
					rec.record(m.Payload())
					return errors.New("handler boom")
				}
			},
			assert: func(t *testing.T, hrec *handlerRecorder, srec *settleRecorder) {
				calls, _ := hrec.snapshot()
				assert.Equal(t, 1, calls)
				acked, nacked, requeue := srec.snapshot()
				assert.False(t, acked)
				assert.True(t, nacked)
				assert.True(t, requeue)
			},
		},
		{
			name:    "handler panic recovers and nacks requeue",
			payload: validJSON,
			newHandler: func(rec *handlerRecorder) msgin.Handler[order] {
				return func(_ context.Context, m msgin.Message[order]) error {
					rec.record(m.Payload())
					panic("handler exploded")
				}
			},
			assert: func(t *testing.T, hrec *handlerRecorder, srec *settleRecorder) {
				calls, _ := hrec.snapshot()
				assert.Equal(t, 1, calls)
				acked, nacked, requeue := srec.snapshot()
				assert.False(t, acked)
				assert.True(t, nacked)
				assert.True(t, requeue)
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d, srec := newDelivery(tc.payload)
			src := &fakeStream{deliveries: []msgin.Delivery{d}}
			hrec := &handlerRecorder{}

			c, err := msgin.NewConsumer[order](src, tc.newHandler(hrec))
			require.NoError(t, err)

			stop := runConsumer(t, c)
			defer stop()

			require.Eventually(t, func() bool {
				acked, nacked, _ := srec.snapshot()
				return acked || nacked
			}, time.Second, 5*time.Millisecond)

			tc.assert(t, hrec, srec)
		})
	}
}
