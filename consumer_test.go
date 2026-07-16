package msgin_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jonboulle/clockwork"
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

// TestConsumer_LiveValueTypeMismatchDiscards closes the live-value decode-error
// gap under the ratified spec §7 settlement switch: a live-value source
// delivering a value that is not T must never reach the handler, and — being a
// permanent (ErrPayloadType) invalid message — is diverted to the invalid sink.
// With no sink configured here, ADR 0007 D7 requires a loud-WARN discard (Ack),
// NOT a Nack-requeue that would loop the poison message forever. Uses
// fakeLiveStream so the single mistyped delivery and its exactly-one Ack are
// asserted deterministically instead of via a busy-spin absence check.
func TestConsumer_LiveValueTypeMismatchDiscards(t *testing.T) {
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
		acked, _, _ := srec.snapshot()
		return acked
	}, time.Second, 5*time.Millisecond)

	acked, nacked, _ := srec.snapshot()
	assert.True(t, acked, "undecodable message discarded (Ack) when no invalid sink is set")
	assert.False(t, nacked, "a permanent decode failure must not loop via Nack-requeue")
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

// TestNewConsumer_RetryPolicyValidation proves NewConsumer surfaces a bad
// RetryPolicy as a construction-time error (RetryPolicy.Validate(), spec §5)
// and that the full set of reliability options constructs cleanly together.
func TestNewConsumer_RetryPolicyValidation(t *testing.T) {
	h := func(context.Context, msgin.Message[order]) error { return nil }

	tests := []struct {
		name   string
		opts   []msgin.ConsumerOption[order]
		assert func(t *testing.T, err error)
	}{
		{"finite policy without dead-letter is rejected at construction",
			[]msgin.ConsumerOption[order]{msgin.WithRetryPolicy[order](msgin.RetryPolicy{MaxAttempts: 3})},
			func(t *testing.T, err error) { assert.ErrorIs(t, err, msgin.ErrNoDeadLetter) }},
		{"negative MaxAttempts is rejected",
			[]msgin.ConsumerOption[order]{msgin.WithRetryPolicy[order](msgin.RetryPolicy{MaxAttempts: -1})},
			func(t *testing.T, err error) { assert.ErrorIs(t, err, msgin.ErrInvalidMaxAttempts) }},
		{"valid reliability options construct cleanly",
			[]msgin.ConsumerOption[order]{
				msgin.WithRetryPolicy[order](msgin.RetryPolicy{}),
				msgin.WithLogger[order](slog.New(slog.NewTextHandler(io.Discard, nil))),
				msgin.WithHooks[order](msgin.Hooks{}),
				msgin.WithShutdownTimeout[order](time.Second),
				msgin.WithConsumerClock[order](clockwork.NewFakeClock()),
				msgin.WithInvalidMessageSink[order](memory.New()),
			},
			func(t *testing.T, err error) { assert.NoError(t, err) }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := msgin.NewConsumer[order](memory.New(), h, tc.opts...)
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
			// Under spec §7, a decode failure is a permanent invalid message:
			// with no invalid sink here it is discarded (Ack), not Nack-requeued
			// (ADR 0007 D7).
			name:    "wire non-[]byte payload discards (acks)",
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
				acked, nacked, _ := srec.snapshot()
				assert.True(t, acked)
				assert.False(t, nacked)
			},
		},
		{
			name:    "wire bad json payload discards (acks)",
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
				acked, nacked, _ := srec.snapshot()
				assert.True(t, acked)
				assert.False(t, nacked)
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

// TestConsumer_Settlement_PermanentAndTransient drives the guarded settlement
// switch (spec §7) through Run with a scriptedSource: success acks; a permanent
// error diverts to the invalid sink and acks the source; a transient error (or
// a handler panic) nacks for redelivery without touching the sink; a nil sink
// discards (acks) a permanent message rather than looping; and a handler that
// RETURNS ErrPayloadDecode is treated as permanent (invalid sink) — I7.
func TestConsumer_Settlement_PermanentAndTransient(t *testing.T) {
	type orderPayload = order

	tests := []struct {
		name    string
		handler msgin.Handler[orderPayload]
		sink    *recordingSink // invalid-message sink; nil => discard
		assert  func(t *testing.T, st *settle, sink *recordingSink)
	}{
		{"success acks",
			func(context.Context, msgin.Message[orderPayload]) error { return nil },
			&recordingSink{},
			func(t *testing.T, st *settle, sink *recordingSink) {
				acks, nacks, _ := st.snapshot()
				assert.Equal(t, 1, acks)
				assert.Equal(t, 0, nacks)
				assert.Equal(t, 0, sink.count())
			}},
		{"permanent error goes to invalid sink and acks source",
			func(context.Context, msgin.Message[orderPayload]) error { return msgin.Permanent(errors.New("bad")) },
			&recordingSink{},
			func(t *testing.T, st *settle, sink *recordingSink) {
				acks, nacks, _ := st.snapshot()
				assert.Equal(t, 1, sink.count(), "diverted to invalid sink")
				assert.Equal(t, 1, acks, "source acked after sink accepted")
				assert.Equal(t, 0, nacks)
			}},
		{"transient error nacks for redelivery",
			func(context.Context, msgin.Message[orderPayload]) error { return errors.New("temporary") },
			&recordingSink{},
			func(t *testing.T, st *settle, sink *recordingSink) {
				_, nacks, _ := st.snapshot()
				assert.GreaterOrEqual(t, nacks, 1)
				assert.Equal(t, 0, sink.count())
			}},
		{"panic is transient (nacked, not diverted)",
			func(context.Context, msgin.Message[orderPayload]) error { panic("kaboom") },
			&recordingSink{},
			func(t *testing.T, st *settle, sink *recordingSink) {
				_, nacks, _ := st.snapshot()
				assert.GreaterOrEqual(t, nacks, 1)
				assert.Equal(t, 0, sink.count(), "panic must not go to invalid sink")
			}},
		{"invalid with nil sink discards (acks) rather than looping",
			func(context.Context, msgin.Message[orderPayload]) error { return msgin.Permanent(errors.New("bad")) },
			nil,
			func(t *testing.T, st *settle, sink *recordingSink) {
				acks, nacks, _ := st.snapshot()
				assert.Equal(t, 1, acks)
				assert.Equal(t, 0, nacks)
			}},
		{"handler returning ErrPayloadDecode is permanent (invalid sink) — I7",
			func(context.Context, msgin.Message[orderPayload]) error {
				return fmt.Errorf("decode in handler: %w", msgin.ErrPayloadDecode)
			},
			&recordingSink{},
			func(t *testing.T, st *settle, sink *recordingSink) {
				acks, nacks, _ := st.snapshot()
				assert.Equal(t, 1, sink.count(), "ErrPayloadDecode from the handler diverts to invalid")
				assert.Equal(t, 1, acks)
				assert.Equal(t, 0, nacks, "permanent decode error must not be retried")
			}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			st := &settle{}
			src := &scriptedSource{deliveries: []msgin.Delivery{newSettleDelivery(order{ID: "o"}, "m1", st)}}
			opts := []msgin.ConsumerOption[orderPayload]{}
			if tc.sink != nil {
				opts = append(opts, msgin.WithInvalidMessageSink[orderPayload](tc.sink))
			}
			c, err := msgin.NewConsumer[orderPayload](src, tc.handler, opts...)
			require.NoError(t, err)

			ctx, cancel := context.WithCancel(t.Context())
			done := make(chan error, 1)
			go func() { done <- c.Run(ctx) }()

			// Let the single delivery settle, then stop.
			require.Eventually(t, func() bool {
				acks, nacks, _ := st.snapshot()
				return acks+nacks >= 1
			}, time.Second, 5*time.Millisecond)
			cancel()
			<-done

			sink := tc.sink
			if sink == nil {
				sink = &recordingSink{}
			}
			tc.assert(t, st, sink)
		})
	}
}

// TestConsumer_DivertSendFailure_NacksNotAcks proves NF-3/I6 and the ADR 0007 D6
// deviation: when the invalid sink rejects the send, the message was NOT
// diverted, so it is Nacked for retry (never Ack-and-lost) and the TERMINAL hook
// (OnInvalidMessage) must NOT fire — a false terminal-event signal. Instead
// OnRetry fires carrying the CLASSIFICATION cause (not the send error). The Nack
// carries the backoff delay for sink-attempt 1: delayFor(1) = Backoff.Delay(0).
// A non-nil Backoff yields a non-zero delay (so a down sink is not hammered); a
// nil Backoff (zero-value policy) yields 0 (immediate) — both arms of
// RetryPolicy.delayFor on the divert-failure path. Nack-not-Ack also evidences
// the not-settled return that keeps the caller's tracker entry (Fix 2/3).
func TestConsumer_DivertSendFailure_NacksNotAcks(t *testing.T) {
	const initial = 250 * time.Millisecond

	tests := []struct {
		name   string
		policy msgin.RetryPolicy
		assert func(t *testing.T, acks, nacks int, delays []time.Duration)
	}{
		{"non-nil backoff => non-zero delay (I6)",
			msgin.RetryPolicy{Backoff: msgin.ExponentialBackoff{Initial: initial, Mult: 2}},
			func(t *testing.T, acks, nacks int, delays []time.Duration) {
				require.NotEmpty(t, delays)
				assert.Equal(t, initial, delays[0], "divert-send-failure Nack must carry a non-zero backoff delay (I6)")
			}},
		{"nil backoff => immediate (delay 0)",
			msgin.RetryPolicy{},
			func(t *testing.T, acks, nacks int, delays []time.Duration) {
				require.NotEmpty(t, delays)
				assert.Equal(t, time.Duration(0), delays[0], "nil Backoff => immediate redelivery")
			}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			st := &settle{}
			src := &scriptedSource{deliveries: []msgin.Delivery{newSettleDelivery(order{ID: "o"}, "m1", st)}}
			sink := &recordingSink{failWith: errors.New("sink down")}
			h := func(context.Context, msgin.Message[order]) error { return msgin.Permanent(errors.New("bad")) }

			var (
				mu           sync.Mutex
				retryFired   int
				retryCause   error
				invalidFired int
			)
			hooks := msgin.Hooks{
				OnRetry: func(_ context.Context, _ msgin.Message[any], err error) {
					mu.Lock()
					retryFired++
					retryCause = err
					mu.Unlock()
				},
				OnInvalidMessage: func(context.Context, msgin.Message[any], error) {
					mu.Lock()
					invalidFired++
					mu.Unlock()
				},
			}

			c, err := msgin.NewConsumer[order](src, h,
				msgin.WithInvalidMessageSink[order](sink),
				msgin.WithHooks[order](hooks),
				msgin.WithRetryPolicy[order](tc.policy))
			require.NoError(t, err)

			ctx, cancel := context.WithCancel(t.Context())
			done := make(chan error, 1)
			go func() { done <- c.Run(ctx) }()
			require.Eventually(t, func() bool { _, nacks, _ := st.snapshot(); return nacks >= 1 }, time.Second, 5*time.Millisecond)
			cancel()
			<-done

			acks, nacks, delays := st.snapshot()
			assert.Equal(t, 0, acks, "must not Ack a message the sink rejected")
			assert.GreaterOrEqual(t, nacks, 1, "must Nack so the message is not lost")

			mu.Lock()
			defer mu.Unlock()
			assert.Equal(t, 0, invalidFired, "terminal OnInvalidMessage must NOT fire on a sink-send failure (no terminal event happened)")
			assert.GreaterOrEqual(t, retryFired, 1, "OnRetry must fire on a sink-send failure")
			require.Error(t, retryCause, "OnRetry carries the classification cause, not nil")
			assert.Contains(t, retryCause.Error(), "bad", "OnRetry carries the classification cause")
			assert.NotContains(t, retryCause.Error(), "sink down", "OnRetry must NOT carry the send error")
			tc.assert(t, acks, nacks, delays)
		})
	}
}

// TestConsumer_PanickingHook_IsContainedAndFlowContinues proves the mandatory
// fault-isolation guarantee (CLAUDE.md): a user observability hook that panics
// is recovered so it never unwinds through the worker goroutine and crashes the
// process. After the recovered panic the flow MUST continue — a subsequent
// message is still processed and settled — Run returns cleanly on cancel, and no
// goroutine leaks (goleak via TestMain). Each case routes the FIRST message
// through a different hook (OnRetry/OnInvalidMessage/OnAck), all of which panic;
// the SECOND message is a plain success that must still be Acked afterwards.
func TestConsumer_PanickingHook_IsContainedAndFlowContinues(t *testing.T) {
	panicHooks := msgin.Hooks{
		OnRetry:          func(context.Context, msgin.Message[any], error) { panic("OnRetry boom") },
		OnAck:            func(context.Context, msgin.Message[any], error) { panic("OnAck boom") },
		OnInvalidMessage: func(context.Context, msgin.Message[any], error) { panic("OnInvalidMessage boom") },
		OnDeadLetter:     func(context.Context, msgin.Message[any], error) { panic("OnDeadLetter boom") },
	}

	tests := []struct {
		name         string
		firstHandler func() error
		assertFirst  func(t *testing.T, first *settle)
	}{
		{"OnRetry panic is contained",
			func() error { return errors.New("transient") },
			func(t *testing.T, first *settle) {
				_, nacks, _ := first.snapshot()
				assert.GreaterOrEqual(t, nacks, 1, "first message nacked despite the OnRetry panic")
			}},
		{"OnInvalidMessage panic is contained",
			func() error { return msgin.Permanent(errors.New("bad")) },
			func(t *testing.T, first *settle) {
				acks, _, _ := first.snapshot()
				assert.Equal(t, 1, acks, "first message acked after divert despite the OnInvalidMessage panic")
			}},
		{"OnAck panic is contained",
			func() error { return nil },
			func(t *testing.T, first *settle) {
				acks, _, _ := first.snapshot()
				assert.Equal(t, 1, acks, "first message acked despite the OnAck panic")
			}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			st1, st2 := &settle{}, &settle{}
			var handled2 atomic.Bool
			h := func(_ context.Context, m msgin.Message[order]) error {
				if m.Payload().ID == "second" {
					handled2.Store(true)
					return nil
				}
				return tc.firstHandler()
			}
			src := &scriptedSource{deliveries: []msgin.Delivery{
				newSettleDelivery(order{ID: "first"}, "m1", st1),
				newSettleDelivery(order{ID: "second"}, "m2", st2),
			}}
			c, err := msgin.NewConsumer[order](src, h,
				msgin.WithInvalidMessageSink[order](&recordingSink{}),
				msgin.WithHooks[order](panicHooks))
			require.NoError(t, err)

			ctx, cancel := context.WithCancel(t.Context())
			done := make(chan error, 1)
			go func() { done <- c.Run(ctx) }()

			// The second message settling proves the flow continued PAST the
			// recovered hook panic on the first message.
			require.Eventually(t, func() bool { acks, _, _ := st2.snapshot(); return acks >= 1 },
				time.Second, 5*time.Millisecond)
			cancel()
			assert.ErrorIs(t, <-done, context.Canceled, "Run returns cleanly despite the hook panic")

			assert.True(t, handled2.Load(), "flow continued: the second message was still handled")
			acks2, _, _ := st2.snapshot()
			assert.Equal(t, 1, acks2, "the second message was acked after the recovered hook panic")
			tc.assertFirst(t, st1)
		})
	}
}

// TestConsumer_FinishLogsSettleError proves finish's error branch: when a settle
// closure (here Ack) returns an error, dispatch surfaces it through the injected
// logger instead of swallowing it or panicking.
func TestConsumer_FinishLogsSettleError(t *testing.T) {
	buf := &lockedBuffer{}
	ackErr := errors.New("ack wire failure")
	st := &settle{}
	d := msgin.Delivery{
		Msg: msgin.New[any](order{ID: "o"}, msgin.WithID("m1")),
		Ack: func(ctx context.Context) error {
			_ = st.ack(ctx) // record that a terminal Ack was attempted
			return ackErr
		},
		Nack: st.nack,
	}
	src := &scriptedSource{deliveries: []msgin.Delivery{d}}
	h := func(context.Context, msgin.Message[order]) error { return nil } // success path

	c, err := msgin.NewConsumer[order](src, h,
		msgin.WithLogger[order](slog.New(slog.NewTextHandler(buf, nil))))
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- c.Run(ctx) }()
	require.Eventually(t, func() bool { acks, _, _ := st.snapshot(); return acks >= 1 }, time.Second, 5*time.Millisecond)
	cancel()
	<-done

	logged := buf.String()
	assert.Contains(t, logged, "settle failed", "finish must log a non-nil settle error")
	assert.Contains(t, logged, ackErr.Error(), "the underlying settle error is surfaced")
}

// TestConsumer_AckFailure_DoesNotEvictTracker proves a code-review-flagged
// regression: a settle-error on the success path must NOT evict the attempt
// tracker (mirroring divert's gated eviction), so a message whose handler
// succeeded but whose Ack failed keeps its climbing attempt count instead of
// silently resetting to 1 on the next redelivery. Sequence over three scripted
// deliveries sharing id "m1": (1) handler fails transiently -> Nack, tracker
// m1=1; (2) handler succeeds but Ack fails -> must NOT evict m1; (3) handler
// fails transiently again -> attempts() must observe n=2 (climbing from the
// still-present m1=1), reaching MaxAttempts=2 and diverting to DeadLetter. The
// pre-fix behavior evicted the tracker at step 2, so step 3 would see n=1 and
// Nack again instead of dead-lettering — this test hangs/fails under that bug.
func TestConsumer_AckFailure_DoesNotEvictTracker(t *testing.T) {
	st := &settle{}
	ackErr := errors.New("commit failed")

	var calls int32
	h := func(context.Context, msgin.Message[order]) error {
		n := atomic.AddInt32(&calls, 1)
		if n == 2 {
			return nil // succeeds on the 2nd delivery; its Ack fails below.
		}
		return errors.New("transient")
	}

	failingAckDelivery := msgin.Delivery{
		Msg:  msgin.New[any](order{ID: "o"}, msgin.WithID("m1")),
		Ack:  func(context.Context) error { return ackErr },
		Nack: st.nack,
	}
	src := &scriptedSource{deliveries: []msgin.Delivery{
		newSettleDelivery(order{ID: "o"}, "m1", st), // attempt 1: transient -> Nack, tracker m1=1
		failingAckDelivery,                          // attempt 2: success but Ack fails -> must NOT evict
		newSettleDelivery(order{ID: "o"}, "m1", st), // attempt 3: transient -> n must be 2 -> DLQ
	}}
	dlq := &recordingSink{}

	c, err := msgin.NewConsumer[order](src, h,
		msgin.WithRetryPolicy[order](msgin.RetryPolicy{MaxAttempts: 2, DeadLetter: dlq}))
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- c.Run(ctx) }()
	require.Eventually(t, func() bool { return dlq.count() == 1 }, time.Second, 5*time.Millisecond,
		"attempt count must survive the failed Ack and reach MaxAttempts on the 3rd delivery")
	cancel()
	<-done
}

// TestConsumer_DecodeFailure_DivertsToInvalidNotRetried proves I2: a live-value
// payload whose dynamic type is not T fails the type assert in decode ->
// permanent invalid, diverted to the sink and NOT retried.
func TestConsumer_DecodeFailure_DivertsToInvalidNotRetried(t *testing.T) {
	st := &settle{}
	bad := msgin.Delivery{Msg: msgin.New[any]("not-an-order", msgin.WithID("m1")), Ack: st.ack, Nack: st.nack}
	src := &scriptedSource{deliveries: []msgin.Delivery{bad}}
	sink := &recordingSink{}
	h := func(context.Context, msgin.Message[order]) error { return nil } // never reached

	c, err := msgin.NewConsumer[order](src, h, msgin.WithInvalidMessageSink[order](sink))
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- c.Run(ctx) }()
	require.Eventually(t, func() bool { acks, nacks, _ := st.snapshot(); return acks+nacks >= 1 }, time.Second, 5*time.Millisecond)
	cancel()
	<-done

	acks, nacks, _ := st.snapshot()
	assert.Equal(t, 1, sink.count(), "undecodable payload diverted to invalid sink")
	assert.Equal(t, 1, acks)
	assert.Equal(t, 0, nacks, "decode failure must not be retried")
}

// TestConsumer_Settlement_ConcurrentWorkers drives a mixed stream (success,
// permanent, transient) through a WithConcurrency(4) pool so the settlement
// switch, divert, the attempt tracker's evict, the shared invalid sink, and the
// hooks are all exercised concurrently — the race target under -race. Every
// delivery must settle exactly once and each outcome class must occur.
func TestConsumer_Settlement_ConcurrentWorkers(t *testing.T) {
	const n = 60
	st := &settle{}
	r := newHookRec()
	sink := &recordingSink{}

	deliveries := make([]msgin.Delivery, 0, n)
	for i := 0; i < n; i++ {
		deliveries = append(deliveries, newSettleDelivery(order{ID: "o", Total: i % 3}, fmt.Sprintf("m%d", i), st))
	}
	src := &scriptedSource{deliveries: deliveries}

	// Total%3: 0 => success (Ack); 1 => permanent (invalid sink); 2 => transient (Nack).
	h := func(_ context.Context, m msgin.Message[order]) error {
		switch m.Payload().Total {
		case 1:
			return msgin.Permanent(errors.New("bad"))
		case 2:
			return errors.New("temporary")
		default:
			return nil
		}
	}

	c, err := msgin.NewConsumer[order](src, h,
		msgin.WithConcurrency[order](4),
		msgin.WithInvalidMessageSink[order](sink),
		msgin.WithHooks[order](r.hooks()))
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- c.Run(ctx) }()
	require.Eventually(t, func() bool {
		acks, nacks, _ := st.snapshot()
		return acks+nacks >= n
	}, 2*time.Second, 5*time.Millisecond)
	cancel()
	<-done

	acks, nacks, _ := st.snapshot()
	assert.Equal(t, n, acks+nacks, "every delivery settled exactly once")
	assert.Greater(t, sink.count(), 0, "permanent messages diverted to the invalid sink")
	assert.Greater(t, r.count("ack"), 0, "OnAck fired for successes")
	assert.Greater(t, r.count("invalid"), 0, "OnInvalidMessage fired for permanents")
	assert.Greater(t, r.count("retry"), 0, "OnRetry fired for transients")
	assert.Positive(t, nacks, "transient deliveries were nacked")
}

// TestConsumer_Hooks_Fire proves I3: hooks configured as recording closures fire
// on their events. OnAck, OnRetry, OnInvalidMessage are covered here;
// OnDeadLetter is covered in Task 6.
func TestConsumer_Hooks_Fire(t *testing.T) {
	tests := []struct {
		name    string
		handler msgin.Handler[order]
		assert  func(t *testing.T, r *hookRec)
	}{
		{"OnAck fires on success",
			func(context.Context, msgin.Message[order]) error { return nil },
			func(t *testing.T, r *hookRec) {
				assert.GreaterOrEqual(t, r.count("ack"), 1)
				assert.Equal(t, 0, r.count("invalid"))
			}},
		{"OnRetry fires on transient error",
			func(context.Context, msgin.Message[order]) error { return errors.New("temporary") },
			func(t *testing.T, r *hookRec) { assert.GreaterOrEqual(t, r.count("retry"), 1) }},
		{"OnInvalidMessage fires on permanent error",
			func(context.Context, msgin.Message[order]) error { return msgin.Permanent(errors.New("bad")) },
			func(t *testing.T, r *hookRec) {
				assert.Equal(t, 1, r.count("invalid"))
				assert.Equal(t, 0, r.count("ack"))
			}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			st := &settle{}
			r := newHookRec()
			src := &scriptedSource{deliveries: []msgin.Delivery{newSettleDelivery(order{ID: "o"}, "m1", st)}}
			c, err := msgin.NewConsumer[order](src, tc.handler,
				msgin.WithInvalidMessageSink[order](&recordingSink{}),
				msgin.WithHooks[order](r.hooks()))
			require.NoError(t, err)

			ctx, cancel := context.WithCancel(t.Context())
			done := make(chan error, 1)
			go func() { done <- c.Run(ctx) }()
			require.Eventually(t, func() bool { acks, nacks, _ := st.snapshot(); return acks+nacks >= 1 }, time.Second, 5*time.Millisecond)
			cancel()
			<-done
			tc.assert(t, r)
		})
	}
}

// TestConsumer_DeadLetterAfterMaxAttempts proves the finite-DLQ branch (spec §7):
// a message that keeps failing is redelivered until MaxAttempts is reached, then
// diverted to RetryPolicy.DeadLetter (firing OnDeadLetter, I3) instead of nacked
// again.
func TestConsumer_DeadLetterAfterMaxAttempts(t *testing.T) {
	st := &settle{}
	// Re-emit the same failing delivery each time it is Nacked, so attempts climb.
	src := &reemittingSource{st: st, id: "m1", payload: order{ID: "o"}}
	dlq := &recordingSink{}
	r := newHookRec() // I3: assert OnDeadLetter fires
	h := func(context.Context, msgin.Message[order]) error { return errors.New("always fails") }

	c, err := msgin.NewConsumer[order](src, h,
		msgin.WithHooks[order](r.hooks()),
		msgin.WithRetryPolicy[order](msgin.RetryPolicy{
			MaxAttempts: 3,
			DeadLetter:  dlq,
			Backoff:     msgin.ExponentialBackoff{}, // zero => immediate; keeps the test fast
		}))
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- c.Run(ctx) }()

	require.Eventually(t, func() bool { return dlq.count() == 1 }, time.Second, 5*time.Millisecond)
	cancel()
	<-done

	acks, nacks, _ := st.snapshot()
	assert.Equal(t, 1, dlq.count(), "diverted to DLQ once exhausted")
	assert.Equal(t, 2, nacks, "attempts 1 and 2 nacked; attempt 3 dead-lettered")
	assert.Equal(t, 1, acks, "DLQ send acked the source")
	assert.Equal(t, 1, r.count("deadletter"), "OnDeadLetter fired once on divert to DLQ (I3)")
}

// TestConsumer_RetryBackoffDelay_ReachesNack proves M10: the computed backoff
// delay actually reaches Nack. Deterministic (RandomizationFactor=0,
// MaxAttempts=0 retry-forever, non-nil Backoff): the n-th transient Nack must
// carry Initial × Mult^(n-1).
func TestConsumer_RetryBackoffDelay_ReachesNack(t *testing.T) {
	st := &settle{}
	src := &reemittingSource{st: st, id: "m1", payload: order{ID: "o"}}
	const initial = 10 * time.Millisecond
	const mult = 2.0
	h := func(context.Context, msgin.Message[order]) error { return errors.New("always fails") }

	c, err := msgin.NewConsumer[order](src, h, msgin.WithRetryPolicy[order](msgin.RetryPolicy{
		MaxAttempts: 0,                                                      // retry forever
		Backoff:     msgin.ExponentialBackoff{Initial: initial, Mult: mult}, // RandomizationFactor 0 => exact
	}))
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- c.Run(ctx) }()
	require.Eventually(t, func() bool { _, nacks, _ := st.snapshot(); return nacks >= 3 }, 2*time.Second, 5*time.Millisecond)
	cancel()
	<-done

	_, _, delays := st.snapshot()
	require.GreaterOrEqual(t, len(delays), 3)
	assert.Equal(t, initial, delays[0], "attempt 1 -> Delay(0) = Initial")
	assert.Equal(t, time.Duration(float64(initial)*mult), delays[1], "attempt 2 -> Initial*Mult")
	assert.Equal(t, time.Duration(float64(initial)*mult*mult), delays[2], "attempt 3 -> Initial*Mult^2")
}

// TestConsumer_NativeDeliveryCount_UsesHeaderNotTracker proves attempts prefers
// the native msgin.delivery-count header over the runtime tracker.
func TestConsumer_NativeDeliveryCount_UsesHeaderNotTracker(t *testing.T) {
	st := &settle{}
	// Header says this is already attempt 3; with MaxAttempts=3 it must DLQ on
	// the FIRST delivery (no reliance on the runtime tracker).
	m := msgin.New[any](order{ID: "o"}, msgin.WithID("m1"),
		msgin.WithHeaders(map[string]any{msgin.HeaderDeliveryCount: 3}))
	src := &nativeScriptedSource{d: msgin.Delivery{Msg: m, Ack: st.ack, Nack: st.nack}}
	dlq := &recordingSink{}
	h := func(context.Context, msgin.Message[order]) error { return errors.New("fail") }

	c, err := msgin.NewConsumer[order](src, h,
		msgin.WithRetryPolicy[order](msgin.RetryPolicy{MaxAttempts: 3, DeadLetter: dlq}))
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- c.Run(ctx) }()
	require.Eventually(t, func() bool { return dlq.count() == 1 }, time.Second, 5*time.Millisecond)
	cancel()
	<-done

	_, nacks, _ := st.snapshot()
	assert.Equal(t, 0, nacks, "header count drove straight to DLQ, no requeue")
}

// TestConsumer_NativeDeadLetter_FallsThroughToRedelivery proves that when the
// source declares NativeDeadLetter()=true, the runtime DLQ branch is skipped
// even though MaxAttempts is reached — the message falls through to Nack for
// native redelivery instead.
func TestConsumer_NativeDeadLetter_FallsThroughToRedelivery(t *testing.T) {
	st := &settle{}
	src := &nativeDLQSource{st: st, id: "m1", payload: order{ID: "o"}} // NativeDeadLetter()=true
	dlq := &recordingSink{}
	h := func(context.Context, msgin.Message[order]) error { return errors.New("fail") }

	c, err := msgin.NewConsumer[order](src, h,
		msgin.WithRetryPolicy[order](msgin.RetryPolicy{MaxAttempts: 1, DeadLetter: dlq}))
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- c.Run(ctx) }()
	require.Eventually(t, func() bool { _, n, _ := st.snapshot(); return n >= 1 }, time.Second, 5*time.Millisecond)
	cancel()
	<-done

	assert.Equal(t, 0, dlq.count(), "native DLQ backend must not use the runtime DLQ")
}

// TestConsumer_Shutdown_TransientNackDuringDrain is the CRITICAL C1 regression
// test. A transient failure during drain makes the REAL memory adapter's
// Nack(requeue=true) block on Send to a channel the stopped Stream no longer
// reads. With the old uncancellable settleCtx this deadlocks (wg.Wait never
// returns -> Run never returns -> goroutine leak). The fixed, always-finite
// drain must let Run return and stay goleak-clean, for both the default-timeout
// path and an explicit small WithShutdownTimeout. A fake clock keeps it fast and
// deterministic; the real memory adapter (not a stub Nack) is essential — a stub
// would hide the blocking re-enqueue.
func TestConsumer_Shutdown_TransientNackDuringDrain(t *testing.T) {
	tests := []struct {
		name string
		opts func(clk *clockwork.FakeClock) []msgin.ConsumerOption[order]
	}{
		{"default shutdown timeout (finite, not infinite)", func(clk *clockwork.FakeClock) []msgin.ConsumerOption[order] {
			return []msgin.ConsumerOption[order]{msgin.WithConsumerClock[order](clk)}
		}},
		{"explicit small shutdown timeout", func(clk *clockwork.FakeClock) []msgin.ConsumerOption[order] {
			return []msgin.ConsumerOption[order]{
				msgin.WithConsumerClock[order](clk),
				msgin.WithShutdownTimeout[order](50 * time.Millisecond),
			}
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			b := memory.New() // default: unbuffered, synchronous handoff
			entered := make(chan struct{}, 1)
			h := func(context.Context, msgin.Message[order]) error {
				select {
				case entered <- struct{}{}:
				default:
				}
				return errors.New("transient") // memory Nack(requeue) re-enqueues via Send
			}
			clk := clockwork.NewFakeClock()
			c, err := msgin.NewConsumer[order](b, h, tc.opts(clk)...)
			require.NoError(t, err)

			ctx, cancel := context.WithCancel(t.Context())
			done := make(chan error, 1)
			go func() { done <- c.Run(ctx) }()

			require.NoError(t, b.Send(t.Context(), msgin.New[any](order{ID: "o"})))
			<-entered // a message is in flight
			cancel()  // parent cancel: Stream returns; the transient Nack will block on Send

			require.NoError(t, clk.BlockUntilContext(t.Context(), 1)) // the drain's clock.After is registered
			clk.Advance(time.Hour)                                    // fire it (>= any configured timeout): cancelSettle releases the stuck Send

			select {
			case err := <-done:
				assert.ErrorIs(t, err, context.Canceled)
			case <-time.After(2 * time.Second):
				t.Fatal("Run did not return: shutdown deadlocked on a transient Nack during drain (C1)")
			}
		})
	}
}

// TestConsumer_ShutdownDeadlineExpiry_NacksInflightAndBuffered is a
// deterministic (handlerEntered channel, no time.Sleep) deadline-expiry test
// with TWO deliveries — one in the handler (in-flight), one buffered. On expiry
// the in-flight one is Nacked (cooperative abort) and the buffered one is
// short-circuit Nacked WITHOUT ever entering the handler (M9 + I5).
func TestConsumer_ShutdownDeadlineExpiry_NacksInflightAndBuffered(t *testing.T) {
	st1, st2 := &settle{}, &settle{}
	entered := make(chan struct{})
	var mu sync.Mutex
	handled := map[string]bool{}
	h := func(ctx context.Context, m msgin.Message[order]) error {
		id := m.Payload().ID
		mu.Lock()
		handled[id] = true
		mu.Unlock()
		if id == "first" {
			close(entered) // in-flight signal
			<-ctx.Done()   // block until cancelDrain (deadline expiry) releases it
			return ctx.Err()
		}
		return nil
	}
	// scriptedSource emits both; with workers=1 and defaultDeliveryBuffer=1 the
	// second sits buffered while the first blocks in the handler.
	src := &scriptedSource{deliveries: []msgin.Delivery{
		newSettleDelivery(order{ID: "first"}, "m1", st1),
		newSettleDelivery(order{ID: "second"}, "m2", st2),
	}}
	clk := clockwork.NewFakeClock()

	c, err := msgin.NewConsumer[order](src, h,
		msgin.WithConsumerClock[order](clk),
		msgin.WithShutdownTimeout[order](5*time.Second))
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- c.Run(ctx) }()

	<-entered // deterministic: the first delivery is in the handler
	cancel()

	require.NoError(t, clk.BlockUntilContext(t.Context(), 1)) // drain reached clock.After(5s)
	clk.Advance(5 * time.Second)                              // fire the deadline

	assert.ErrorIs(t, <-done, context.Canceled) // Run returns (always finite)

	_, n1, _ := st1.snapshot()
	_, n2, _ := st2.snapshot()
	assert.GreaterOrEqual(t, n1, 1, "in-flight message Nacked on deadline expiry")
	assert.GreaterOrEqual(t, n2, 1, "buffered message Nacked after expiry (I5)")
	mu.Lock()
	defer mu.Unlock()
	assert.False(t, handled["second"], "buffered message must be short-circuit Nacked, never handled (I5)")
}

// TestConsumer_GracefulDrain_ProcessesBuffered leaves work in flight at cancel()
// time (does NOT wait for all handled first), so it exercises POST-cancel
// draining rather than pre-cancel completion (M12).
func TestConsumer_GracefulDrain_ProcessesBuffered(t *testing.T) {
	b := memory.New(memory.WithBuffer(8))
	p, err := msgin.NewProducer[order](b)
	require.NoError(t, err)
	for i := 0; i < 5; i++ {
		require.NoError(t, p.Send(t.Context(), msgin.New(order{ID: "o"})))
	}

	var seen atomic.Int64
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	h := func(context.Context, msgin.Message[order]) error {
		select {
		case entered <- struct{}{}:
		default:
		}
		<-release // hold the first handler so we can cancel with work still queued
		seen.Add(1)
		return nil
	}
	c, err := msgin.NewConsumer[order](b, h) // shutdownTimeout 0 => finite default drain
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- c.Run(ctx) }()

	<-entered      // a handler is in flight and more messages are still queued
	cancel()       // cancel BEFORE the in-flight handler completes (post-cancel drain)
	close(release) // let the drain finish the in-flight work

	assert.ErrorIs(t, <-done, context.Canceled)
	assert.GreaterOrEqual(t, seen.Load(), int64(1), "in-flight work drained after cancel, not dropped")
}
