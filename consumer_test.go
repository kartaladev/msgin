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
	"testing/synctest"
	"time"

	"github.com/jonboulle/clockwork"
	"github.com/kartaladev/msgin"
	"github.com/kartaladev/msgin/adapter/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
)

func TestMain(m *testing.M) { goleak.VerifyTestMain(m) }

// stubPolling implements only PollingSource, used to prove NewConsumer resolves
// a poll-only source to the pull path (Plan 005) and constructs cleanly. Its
// Poll returns nothing, so it is safe for construction-only assertions.
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
		{"polling source constructs (resolved to the pull path)", stubPolling{}, nil,
			func(t *testing.T, err error) { assert.NoError(t, err) }},
		{"polling source constructs with valid poll options", stubPolling{},
			[]msgin.ConsumerOption[order]{
				msgin.WithPollInterval[order](2 * time.Second),
				msgin.WithPollMaxBatch[order](50),
			},
			func(t *testing.T, err error) { assert.NoError(t, err) }},
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

// TestConsumer_WithConsumerClockNil_IsNoOpNotPanic proves a nil
// WithConsumerClock is a no-op (the real-clock default stays in place) rather
// than a caller-triggered nil-panic once Run reaches its first clock use (the
// shutdown-timeout c.clock.After call) — no panic on caller input.
func TestConsumer_WithConsumerClockNil_IsNoOpNotPanic(t *testing.T) {
	b := memory.New()
	h := func(context.Context, msgin.Message[order]) error { return nil }
	c, err := msgin.NewConsumer[order](b, h,
		msgin.WithConsumerClock[order](nil),
		msgin.WithShutdownTimeout[order](10*time.Millisecond))
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- c.Run(ctx) }()
	cancel()

	select {
	case err := <-done:
		assert.ErrorIs(t, err, context.Canceled)
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return; a nil clock may have panicked instead of falling back to the real clock")
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

			require.NoError(t, clk.BlockUntilContext(t.Context(), 2)) // the drain's clock.After is registered; +1 waiter is the always-on tracker sweep ticker (registered at Run start, C1)
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
// deterministic deadline-expiry test with TWO deliveries — one in the handler
// (in-flight), one pulled-but-not-dispatched. On cancel the surplus is Nacked by
// the ingress stage WITHOUT ever entering the handler, and on deadline expiry the
// in-flight one is Nacked (cooperative abort) (M9 + I5).
//
// After the credit-gate rewrite (Plan 003 Task 2) the source→worker path is two
// unbuffered channels (rawCh, workerCh) rather than a buffered deliveries
// channel: with workers=1 the surplus "second" parks in ingest on the workerCh
// send (credit acquired, worker busy). synctest.Wait pins that steady state
// deterministically — first in the handler, second parked in ingest — before
// cancel, so there is no reliance on a happens-before between the source emitting
// "second" and the handler entering. Cancel makes ingest's admit take its
// ctx-done branch and Nack "second"; the deadline then releases "first". The
// consumer clock is a clockwork fake, so no real timers run inside the bubble.
func TestConsumer_ShutdownDeadlineExpiry_NacksInflightAndBuffered(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
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

		<-entered       // the first delivery is in the handler
		synctest.Wait() // steady state: "second" is parked in ingest on the workerCh send
		cancel()        // ingest's admit takes its ctx-done branch → Nacks "second"

		require.NoError(t, clk.BlockUntilContext(t.Context(), 2)) // drain reached clock.After(5s); +1 waiter is the always-on tracker sweep ticker (registered at Run start, C1)
		clk.Advance(5 * time.Second)                              // fire the deadline

		assert.ErrorIs(t, <-done, context.Canceled) // Run returns (always finite)

		_, n1, _ := st1.snapshot()
		_, n2, _ := st2.snapshot()
		assert.GreaterOrEqual(t, n1, 1, "in-flight message Nacked on deadline expiry")
		assert.GreaterOrEqual(t, n2, 1, "pulled-but-not-dispatched message Nacked by ingress on cancel (I5)")
		mu.Lock()
		defer mu.Unlock()
		assert.False(t, handled["second"], "surplus message must be Nacked by ingress, never handled (I5)")
	})
}

// TestConsumer_ShutdownDeadlineExpiry_DrainsWorkerChBufferResident proves the
// OTHER shutdown-drain path from TestConsumer_ShutdownDeadlineExpiry_NacksInflightAndBuffered:
// a delivery genuinely RESIDENT in the buffered workerCh (D3-fix, ADR 0008) at
// the deadline — not merely parked in ingest waiting for a credit or a worker —
// is drained (Nacked via process's drainCtx short-circuit) with no leak.
//
// WithMaxInFlight(3) (n >= 2) with the default single worker lets ingest admit
// "first", "second", and "third" all the way past the credit gate without
// blocking: workerCh's buffer (capacity == maxInFlight) has room for all three,
// so "second" and "third" land IN THE BUFFER — resident, not send-blocked — while
// the sole worker is busy running "first"'s handler. synctest.Wait pins that
// steady state deterministically before cancel. On cancel, ingest's admit loop
// has already finished (all three already emitted by the scripted source), so
// there is nothing left for ingest to Nack; the surplus is settled entirely by
// the worker draining the closed-but-still-buffered workerCh after the deadline
// cancels drainCtx, taking process's short-circuit branch for each.
func TestConsumer_ShutdownDeadlineExpiry_DrainsWorkerChBufferResident(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		st1, st2, st3 := &settle{}, &settle{}, &settle{}
		entered := make(chan struct{})
		var mu sync.Mutex
		handled := map[string]bool{}
		h := func(ctx context.Context, m msgin.Message[order]) error {
			id := m.Payload().ID
			mu.Lock()
			handled[id] = true
			mu.Unlock()
			if id == "first" {
				close(entered) // in-flight signal: the sole worker is now busy
				<-ctx.Done()   // block until cancelDrain (deadline expiry) releases it
				return ctx.Err()
			}
			return nil // never reached: "second"/"third" must be short-circuited, not dispatched
		}
		src := &scriptedSource{deliveries: []msgin.Delivery{
			newSettleDelivery(order{ID: "first"}, "m1", st1),
			newSettleDelivery(order{ID: "second"}, "m2", st2),
			newSettleDelivery(order{ID: "third"}, "m3", st3),
		}}
		clk := clockwork.NewFakeClock()

		c, err := msgin.NewConsumer[order](src, h,
			msgin.WithConsumerClock[order](clk),
			msgin.WithMaxInFlight[order](3), // n >= 2: room for "second"+"third" to sit IN workerCh's buffer
			msgin.WithShutdownTimeout[order](5*time.Second))
		require.NoError(t, err)

		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan error, 1)
		go func() { done <- c.Run(ctx) }()

		<-entered       // "first" is in the handler; the worker is busy
		synctest.Wait() // steady state: "second" and "third" are resident in workerCh's buffer
		cancel()        // parent cancel: ingest has nothing left to admit (all 3 already emitted)

		require.NoError(t, clk.BlockUntilContext(t.Context(), 2)) // drain reached clock.After(5s); +1 waiter is the always-on tracker sweep ticker (registered at Run start, C1)
		clk.Advance(5 * time.Second)                              // fire the deadline: cancelDrain short-circuits the buffered surplus

		assert.ErrorIs(t, <-done, context.Canceled) // Run always returns (C1)

		_, n1, _ := st1.snapshot()
		_, n2, _ := st2.snapshot()
		_, n3, _ := st3.snapshot()
		assert.GreaterOrEqual(t, n1, 1, "in-flight message Nacked on deadline expiry (cooperative abort)")
		assert.GreaterOrEqual(t, n2, 1, "buffer-resident message drained via process's drainCtx short-circuit")
		assert.GreaterOrEqual(t, n3, 1, "buffer-resident message drained via process's drainCtx short-circuit")
		mu.Lock()
		defer mu.Unlock()
		assert.False(t, handled["second"], "buffer-resident surplus must be Nacked without dispatch, never handled")
		assert.False(t, handled["third"], "buffer-resident surplus must be Nacked without dispatch, never handled")
	})
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

// TestConsumer_StreamEndsNormally_RunReturnsNil proves the ingress "source
// stream ended" path: when the source's Stream returns nil on its own (a bounded
// source that runs dry rather than blocking on ctx), Run closes rawCh, ingest
// exits on the closed read (not via ctx cancel), the pool drains, and Run returns
// the nil stream error — all without the caller cancelling and with no leak.
func TestConsumer_StreamEndsNormally_RunReturnsNil(t *testing.T) {
	st := &settle{}
	src := &finiteSource{deliveries: []msgin.Delivery{
		newSettleDelivery(order{ID: "o"}, "m1", st),
		newSettleDelivery(order{ID: "o"}, "m2", st),
		newSettleDelivery(order{ID: "o"}, "m3", st),
	}}
	h := func(context.Context, msgin.Message[order]) error { return nil }

	c, err := msgin.NewConsumer[order](src, h)
	require.NoError(t, err)

	// No cancel: the source ends the stream itself, so Run returns on its own.
	assert.NoError(t, c.Run(t.Context()), "stream ended normally => nil error")

	acks, nacks, _ := st.snapshot()
	assert.Equal(t, 3, acks, "every delivery acked before the stream-ended shutdown")
	assert.Equal(t, 0, nacks)
}

// TestConsumer_MaxInFlight_BoundsClaimedUnsettled proves invariant 1 (no
// over-pull past n) and invariant 4 (credits recycle — no leak). A burst of 20
// deliveries hits a 20-worker pool, but WithMaxInFlight(3) means the credit gate
// admits at most 3 concurrently; maxSeen must never exceed n. Releasing the held
// handlers lets every message drain, proving credits are recycled exactly.
func TestConsumer_MaxInFlight_BoundsClaimedUnsettled(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		const n = 3
		const burst = 20
		b := memory.New(memory.WithBuffer(burst))
		src := &countingSource{broker: b}

		var inFlight, maxSeen, processed atomic.Int64
		release := make(chan struct{})
		h := func(_ context.Context, _ msgin.Message[order]) error {
			cur := inFlight.Add(1)
			for {
				m := maxSeen.Load()
				if cur <= m || maxSeen.CompareAndSwap(m, cur) {
					break
				}
			}
			<-release
			inFlight.Add(-1)
			processed.Add(1)
			return nil
		}
		c, err := msgin.NewConsumer[order](src, h,
			// I3: inject a fake consumer clock and NEVER advance it. Once Task 7
			// adds the always-on tracker sweep, the sweep goroutine creates a
			// ticker on the CONSUMER clock; with a real clock that would be a real
			// ticker inside the synctest bubble, perturbing synctest.Wait(). A
			// clockwork fake ticker fires ONLY on Advance, so — never advanced — it
			// stays durably blocked and is invisible to synctest.Wait().
			msgin.WithConsumerClock[order](clockwork.NewFakeClock()),
			msgin.WithConcurrency[order](burst), // many workers…
			msgin.WithMaxInFlight[order](n))     // …but credit caps in-flight at n
		require.NoError(t, err)

		ctx, cancel := context.WithCancel(t.Context())
		runDone := make(chan error, 1)
		go func() { runDone <- c.Run(ctx) }()
		for i := 0; i < burst; i++ {
			require.NoError(t, b.Send(ctx, msgin.New[any](order{ID: "x"})))
		}

		synctest.Wait() // steady state: exactly n handlers entered, the rest gated
		assert.LessOrEqual(t, maxSeen.Load(), int64(n), "never more than n claimed-but-unsettled (no over-pull)")
		assert.Equal(t, int64(n), inFlight.Load(), "exactly n admitted; the surplus waits in the source")

		close(release) // let everything drain; credits recycle
		synctest.Wait()
		assert.Equal(t, int64(burst), processed.Load(), "all messages eventually processed (no credit leak)")

		cancel()
		assert.ErrorIs(t, <-runDone, context.Canceled)
	})
}

// TestConsumer_MaxInFlight_NackReleasesSlot_NoDeadlock proves invariant 3 (n=1
// Nack-redeliver does not deadlock) and invariant 2's release-FIRST ordering.
// audit class 3: at n=1 a transient Nack must release its slot BEFORE the memory
// re-enqueue, or the redelivery deadlocks waiting for the credit it still holds.
func TestConsumer_MaxInFlight_NackReleasesSlot_NoDeadlock(t *testing.T) {
	b := memory.New()
	var attempts atomic.Int64
	done := make(chan struct{})
	h := func(context.Context, msgin.Message[order]) error {
		if attempts.Add(1) == 1 {
			return errors.New("transient once") // Nack(requeue) re-enqueues via Send
		}
		select {
		case <-done:
		default:
			close(done)
		}
		return nil
	}
	c, err := msgin.NewConsumer[order](b, h, msgin.WithMaxInFlight[order](1))
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	runDone := make(chan error, 1)
	go func() { runDone <- c.Run(ctx) }()
	require.NoError(t, b.Send(ctx, msgin.New[any](order{ID: "x"})))

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("redelivery deadlocked: the Nacked message pinned its only credit (NF-5)")
	}
	cancel()
	assert.ErrorIs(t, <-runDone, context.Canceled)
	assert.GreaterOrEqual(t, attempts.Load(), int64(2), "message was redelivered after the transient Nack")
}

// TestConsumer_Run_BacklogBehindBusyWorkerWithRequeue_NoDeadlock is a
// regression test for a 3-way cyclic-wait deadlock found in Task 8's
// whole-branch review: with an UNBUFFERED memory.Broker, the default
// consumer (concurrency 1, default maxInFlight), and >= 2 messages queued
// behind a busy worker, a transient failure whose Nack(requeue=true)
// synchronously re-enters the broker (adapter/memory's Send) could deadlock
// with nobody left to drain the broker's channel: Stream parked sending to
// the ingress rawCh, the ingest goroutine parked sending to workerCh (no
// worker free), and the sole worker parked re-injecting its Nack into the
// broker. The fix (this commit) buffers workerCh to the credit gate's own
// capacity so ingest's ONLY blocking point is the credit acquire (the
// intended backpressure), never worker availability, which keeps the
// broker always drainable. This reproduced on ~40-50% of runs before the
// fix and is 0/N-flaky after it — run at -count=20 in CI to guard the fix.
func TestConsumer_Run_BacklogBehindBusyWorkerWithRequeue_NoDeadlock(t *testing.T) {
	b := memory.New() // UNBUFFERED: forces the synchronous relay chain
	var failedOnce atomic.Bool
	var processed atomic.Int32
	done := make(chan struct{})
	h := func(_ context.Context, m msgin.Message[order]) error {
		if m.Payload().ID == "A" && failedOnce.CompareAndSwap(false, true) {
			return errors.New("transient fail on A's first attempt")
		}
		if processed.Add(1) == 3 {
			close(done)
		}
		return nil
	}
	c, err := msgin.NewConsumer[order](b, h)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	runDone := make(chan error, 1)
	go func() { runDone <- c.Run(ctx) }()

	// Send concurrently: the broker is unbuffered, so each Send blocks until
	// Stream reads it — sending from separate goroutines lets all three race
	// into the pipeline the way the original repro did, instead of the
	// caller itself serializing them one at a time.
	var wg sync.WaitGroup
	for _, id := range []string{"A", "B", "C"} {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			_ = b.Send(ctx, msgin.New[any](order{ID: id}))
		}(id)
	}

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("backlog behind the busy worker deadlocked: the requeue could not drain the broker")
	}
	wg.Wait()
	cancel()
	assert.ErrorIs(t, <-runDone, context.Canceled)
	assert.Equal(t, int32(3), processed.Load())
}

// TestConsumer_MaxInFlight_ShutdownWithIngestHeldSurplus_NoLeak proves invariant
// 5 (a delivery whose ctx is done before it acquires credit does not spuriously
// release) and the I4 bounded-shutdown contract. Shutdown must still return
// (bounded, goleak-clean) even when the ingress goroutine is holding a surplus
// delivery whose ctx-done Nack(requeue=true) blocks on the memory adapter's
// UNBUFFERED Send (the stopped Stream no longer reads). With n=1 and a handler
// that holds the single credit, a second message is pulled by ingest and parks
// on acquire; at cancel, admit's acquire returns ctx.Err and Nacks that held
// delivery via requeue → the memory Send blocks until cancelSettle fires at the
// shutdown deadline. Run must not hang past the (finite) deadline, and no
// goroutine may leak.
func TestConsumer_MaxInFlight_ShutdownWithIngestHeldSurplus_NoLeak(t *testing.T) {
	b := memory.New() // UNBUFFERED: Nack(requeue) Send blocks once Stream stops
	clk := clockwork.NewFakeClock()
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	h := func(context.Context, msgin.Message[order]) error {
		select {
		case entered <- struct{}{}:
		default:
		}
		<-release // hold the only credit so the 2nd msg parks in ingest on acquire
		return nil
	}
	c, err := msgin.NewConsumer[order](b, h,
		msgin.WithConsumerClock[order](clk),
		msgin.WithMaxInFlight[order](1))
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	runDone := make(chan error, 1)
	go func() { runDone <- c.Run(ctx) }()

	require.NoError(t, b.Send(ctx, msgin.New[any](order{ID: "a"})))
	require.NoError(t, b.Send(ctx, msgin.New[any](order{ID: "b"}))) // parks in ingest on acquire
	<-entered
	cancel() // Stream returns; ingest's acquire returns ctx.Err → Nack(requeue) blocks on Send

	require.NoError(t, clk.BlockUntilContext(t.Context(), 2)) // drain's clock.After registered; +1 waiter is the always-on tracker sweep ticker (registered at Run start, C1)
	clk.Advance(time.Hour)                                    // fire the deadline → cancelSettle unblocks the stuck Send
	close(release)

	select {
	case err := <-runDone:
		assert.ErrorIs(t, err, context.Canceled)
	case <-time.After(2 * time.Second):
		t.Fatal("Run hung: ingest-held surplus Nack blocked on the unbuffered Send past the deadline (I4)")
	}
}

// TestConsumer_MaxInFlight_ShutdownDeadline_ShortCircuitsSurplusBeforeHandler
// covers process's shutdown short-circuit (consumer.go: `if drainCtx.Err() !=
// nil { ... }`), proving it is reachable and load-bearing: removing it would
// let handler work start on a delivery admitted AFTER the shutdown deadline
// already expired. A finiteSource emits two deliveries then ends the stream on
// its own (no parent cancel) with WithMaxInFlight(1): d1's cooperative handler
// blocks on <-ctx.Done() (ctx == drainCtx) holding the only credit, while
// ingest reads d2 and parks in admit's acquire — waiting on the PARENT ctx,
// which this test never cancels. Once the source ends, Run reaches the
// shutdown-deadline select; the injected fake clock fires it, cancelling
// drainCtx/settleCtx. d1's handler sees drainCtx done, returns, and its
// wrapped Nack releases the credit; ingest's parked acquire claims the freed
// credit and hands d2 to the (now free) worker, whose process() sees
// drainCtx.Err() != nil and Nacks d2 WITHOUT ever calling the handler.
func TestConsumer_MaxInFlight_ShutdownDeadline_ShortCircuitsSurplusBeforeHandler(t *testing.T) {
	st := &settle{}
	src := &finiteSource{deliveries: []msgin.Delivery{
		newSettleDelivery(order{ID: "o"}, "m1", st),
		newSettleDelivery(order{ID: "o"}, "m2", st),
	}}
	var handlerCalls atomic.Int64
	entered := make(chan struct{}, 1)
	clk := clockwork.NewFakeClock()
	h := func(ctx context.Context, _ msgin.Message[order]) error {
		handlerCalls.Add(1)
		select {
		case entered <- struct{}{}:
		default:
		}
		<-ctx.Done() // cooperative: only returns once the shutdown deadline cancels drainCtx
		return ctx.Err()
	}
	c, err := msgin.NewConsumer[order](src, h,
		msgin.WithConsumerClock[order](clk),
		msgin.WithMaxInFlight[order](1))
	require.NoError(t, err)

	runDone := make(chan error, 1)
	go func() { runDone <- c.Run(t.Context()) }()

	<-entered // d1 is in the handler holding the only credit; d2 is parked in ingest's admit

	require.NoError(t, clk.BlockUntilContext(t.Context(), 2)) // Run registered the shutdown deadline; +1 waiter is the always-on tracker sweep ticker (registered at Run start, C1)
	clk.Advance(time.Hour)                                    // fire it: drainCtx/settleCtx cancel

	select {
	case err := <-runDone:
		assert.NoError(t, err, "finiteSource ended the stream on its own; Run returns the nil stream error")
	case <-time.After(2 * time.Second):
		t.Fatal("Run hung past the shutdown deadline")
	}

	assert.Equal(t, int64(1), handlerCalls.Load(), "handler entered exactly once: d2 was short-circuited before dispatch")
	acks, nacks, _ := st.snapshot()
	assert.Equal(t, 0, acks)
	assert.Equal(t, 2, nacks, "d1 nacked after its cooperative ctx-done return; d2 nacked by process's shutdown short-circuit")
}

// TestConsumer_MaxInFlight_AcquireFailReleasesNothing strengthens invariant 5
// with a direct credit-balance check on the ctx-done-before-acquire path in
// admit (credit.go's gate.acquire failing before any token was claimed): that
// path must release NOTHING, since nothing was acquired. An over-release bug
// there would free a credit currently held by a DIFFERENT in-flight delivery
// — with n=1, that legitimate credit belongs to d1, so if the acquire-fail
// Nack for d2 spuriously drained it, d1's OWN later release (via its wrapped
// Ack) would find the credit gate's single-slot buffer already empty and
// block forever on the receive (a semaphore release with nothing to release),
// hanging Run past any deadline (nothing here is context-gated). This test
// proves that does NOT happen: d1's Ack completes promptly right after the
// acquire-fail event, and the settlement counts are exactly as expected (one
// raw, unwrapped Nack for d2; one wrapped Ack for d1) — anything else, or a
// hang, is the bug.
//
// Synchronization: a finiteSource (not scriptedSource) is used so that Stream
// returning is a hard channel-rendezvous guarantee that BOTH deliveries have
// already been forwarded into ingest — i.e. d2 is already parked in admit's
// gate.acquire (waiting on the still-live parent ctx) by the time this test's
// clk.BlockUntilContext call returns. Only then is the parent ctx cancelled,
// deterministically resolving that blocked acquire to ctx.Err() rather than
// racing ingest's outer select into skipping d2 entirely.
func TestConsumer_MaxInFlight_AcquireFailReleasesNothing(t *testing.T) {
	st := &settle{}
	src := &finiteSource{deliveries: []msgin.Delivery{
		newSettleDelivery(order{ID: "o"}, "m1", st),
		newSettleDelivery(order{ID: "o"}, "m2", st),
	}}
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	h := func(context.Context, msgin.Message[order]) error {
		select {
		case entered <- struct{}{}:
		default:
		}
		<-release // hold the only credit until the test says so
		return nil
	}
	clk := clockwork.NewFakeClock()
	c, err := msgin.NewConsumer[order](src, h,
		msgin.WithConsumerClock[order](clk),
		msgin.WithMaxInFlight[order](1))
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	runDone := make(chan error, 1)
	go func() { runDone <- c.Run(ctx) }()

	<-entered // d1 holds the only credit

	// finiteSource's Stream only returns — letting Run close rawCh and register
	// the shutdown deadline on the injected fake clock — once BOTH deliveries
	// are fully forwarded into ingest. So by the time this unblocks, d2 is
	// already parked in admit's gate.acquire. +1 waiter is the always-on tracker
	// sweep ticker (registered at Run start, C1), so we wait for BOTH it and the
	// shutdown deadline — waiting for only 1 would return on the sweep ticker
	// before the deadline registered and defeat this barrier.
	require.NoError(t, clk.BlockUntilContext(t.Context(), 2))

	cancel() // d2's acquire returns ctx.Err(): the acquire-fail path Nacks it RAW

	// Wait for that Nack to land BEFORE releasing d1: this fixes the ordering
	// so d1's legitimate release can never race the (hypothetical, buggy)
	// acquire-fail release — without this, closing release immediately after
	// cancel() lets the scheduler interleave d1's wakeup ahead of ingest's
	// blocked acquire noticing ctx.Done(), which can let d2 acquire for real
	// and defeat the test. Polling here only confirms the Nack already
	// happened; it asserts nothing about credit state yet.
	require.Eventually(t, func() bool {
		_, nacks, _ := st.snapshot()
		return nacks == 1
	}, 2*time.Second, time.Millisecond, "acquire-fail Nack for d2 never landed")

	// Now release d1. If the acquire-fail path above had spuriously released a
	// credit, it would have drained the ONE token that d1 (still in the
	// handler) legitimately holds. d1's own legitimate release would then find
	// the gate's buffer already empty and block forever on the receive — the
	// select below would time out instead of observing Run finish.
	close(release)
	select {
	case err := <-runDone:
		assert.NoError(t, err, "finiteSource ended the stream on its own before cancel; Run returns the nil stream error")
	case <-time.After(2 * time.Second):
		t.Fatal("d1's legitimate credit release hung: the acquire-fail path spuriously released a credit it never held")
	}

	acks, nacks, _ := st.snapshot()
	assert.Equal(t, 1, acks, "d1 completed and Acked normally")
	assert.Equal(t, 1, nacks, "d2 Nacked via the raw acquire-fail path, unwrapped (no credit to release)")
}

func TestConsumer_RateLimit_GatesIngress(t *testing.T) {
	clk := clockwork.NewFakeClock()
	rl, err := msgin.NewTokenBucket(10, 1, msgin.WithTokenBucketClock(clk))
	require.NoError(t, err)

	b := memory.New(memory.WithBuffer(4))
	var processed atomic.Int64
	h := func(context.Context, msgin.Message[order]) error { processed.Add(1); return nil }
	c, err := msgin.NewConsumer[order](b, h, msgin.WithRateLimit[order](rl))
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	runDone := make(chan error, 1)
	go func() { runDone <- c.Run(ctx) }()
	require.NoError(t, b.Send(ctx, msgin.New[any](order{ID: "a"})))
	require.NoError(t, b.Send(ctx, msgin.New[any](order{ID: "b"})))

	require.Eventually(t, func() bool { return processed.Load() == 1 }, time.Second, 5*time.Millisecond)
	// The second is gated on the limiter (parked on clock.After) — advancing a
	// refill interval lets it through.
	require.NoError(t, clk.BlockUntilContext(ctx, 1))
	clk.Advance(100 * time.Millisecond)
	require.Eventually(t, func() bool { return processed.Load() == 2 }, time.Second, 5*time.Millisecond)

	cancel()
	assert.ErrorIs(t, <-runDone, context.Canceled)
}

// admit's rate-limit Wait runs BEFORE the credit acquire (composition order):
// a ctx cancel while parked on the limiter must Nack without ever touching the
// credit gate, and the message must never reach the handler.
func TestConsumer_RateLimit_CtxCancelWhileParkedNacksBeforeCredit(t *testing.T) {
	clk := clockwork.NewFakeClock()
	rl, err := msgin.NewTokenBucket(1, 1, msgin.WithTokenBucketClock(clk))
	require.NoError(t, err)

	b := memory.New(memory.WithBuffer(4))
	var processed atomic.Int64
	h := func(context.Context, msgin.Message[order]) error { processed.Add(1); return nil }
	c, err := msgin.NewConsumer[order](b, h, msgin.WithRateLimit[order](rl))
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	runDone := make(chan error, 1)
	go func() { runDone <- c.Run(ctx) }()
	require.NoError(t, b.Send(ctx, msgin.New[any](order{ID: "a"})))
	require.NoError(t, b.Send(ctx, msgin.New[any](order{ID: "b"})))

	require.Eventually(t, func() bool { return processed.Load() == 1 }, time.Second, 5*time.Millisecond)
	require.NoError(t, clk.BlockUntilContext(ctx, 1)) // the second is parked on the limiter, holding no credit
	cancel()                                          // shutdown while parked on the rate limiter, not the credit gate

	select {
	case err := <-runDone:
		assert.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("Run did not return after ctx cancel while parked on the rate limiter")
	}
	assert.Equal(t, int64(1), processed.Load(), "the rate-limited second message must never reach the handler")
}

// TestConsumer_HandlerTimeout_CancelsStuckHandlerAndRetries proves spec §7.4.4 /
// ADR 0008 D6: a handler that ignores ctx.Err() is not left running forever
// (the Plan 002 residual) — WithHandlerTimeout derives a clockwork-driven
// deadline around the handler call, so a stuck cooperative handler is
// cancelled, classified transient, and Nacked (retried) rather than pinning
// the worker.
func TestConsumer_HandlerTimeout_CancelsStuckHandlerAndRetries(t *testing.T) {
	st := &settle{}
	src := &reemittingSource{st: st, id: "m1", payload: order{ID: "o"}}
	clk := clockwork.NewFakeClock()

	var calls atomic.Int64
	proceeded := make(chan struct{}, 1)
	h := func(ctx context.Context, _ msgin.Message[order]) error {
		n := calls.Add(1)
		if n == 1 {
			<-ctx.Done() // first attempt: stuck until the timeout cancels us
			return ctx.Err()
		}
		select {
		case proceeded <- struct{}{}:
		default:
		}
		return nil
	}
	c, err := msgin.NewConsumer[order](src, h,
		msgin.WithConsumerClock[order](clk),
		msgin.WithHandlerTimeout[order](2*time.Second))
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	runDone := make(chan error, 1)
	go func() { runDone <- c.Run(ctx) }()

	require.NoError(t, clk.BlockUntilContext(t.Context(), 2)) // the first handler's timeout is registered; +1 waiter is the always-on tracker sweep ticker (registered at Run start, C1)
	clk.Advance(2 * time.Second)                              // fire it → ctx.Done → transient Nack → redelivery
	select {
	case <-proceeded: // the redelivery ran and succeeded
	case <-time.After(2 * time.Second):
		t.Fatal("stuck handler was not cancelled by the timeout")
	}
	_, nacks, _ := st.snapshot()
	assert.GreaterOrEqual(t, nacks, 1, "timed-out handler is a transient failure (Nacked, retried)")

	cancel()
	assert.ErrorIs(t, <-runDone, context.Canceled)
}

// TestConsumer_CircuitBreaker_GatesDispatchAndRecovers proves NF-10: an open
// breaker pauses ingress AND makes workers Nack already-buffered work instead of
// driving it into the failing downstream, and the flow recovers on half-open.
func TestConsumer_CircuitBreaker_GatesDispatchAndRecovers(t *testing.T) {
	clk := clockwork.NewFakeClock()
	b := msgin.NewCircuitBreaker(msgin.WithBreakerClock(clk), msgin.WithBreakerThreshold(1), msgin.WithBreakerCooldown(5*time.Second))

	broker := memory.New(memory.WithBuffer(8))
	var ok atomic.Int64
	fail := true
	var mu sync.Mutex
	h := func(context.Context, msgin.Message[order]) error {
		mu.Lock()
		f := fail
		mu.Unlock()
		if f {
			return errors.New("downstream down") // trips the breaker (threshold 1)
		}
		ok.Add(1)
		return nil
	}
	c, err := msgin.NewConsumer[order](broker, h,
		msgin.WithConsumerClock[order](clk),
		msgin.WithCircuitBreaker[order](b))
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	runDone := make(chan error, 1)
	go func() { runDone <- c.Run(ctx) }()

	require.NoError(t, broker.Send(ctx, msgin.New[any](order{ID: "a"})))
	require.Eventually(t, func() bool { return !b.Allow() }, time.Second, 5*time.Millisecond) // opened

	// While open, more messages must NOT be dispatched into the failing handler.
	require.NoError(t, broker.Send(ctx, msgin.New[any](order{ID: "b"})))
	// Fix the downstream, then let the cooldown half-open and drain.
	mu.Lock()
	fail = false
	mu.Unlock()
	require.NoError(t, clk.BlockUntilContext(t.Context(), 2)) // breaker cooldown timer registered; +1 waiter is the always-on tracker sweep ticker (registered at Run start, C1)
	clk.Advance(5 * time.Second)                              // half-open wakes the parked ingress
	require.Eventually(t, func() bool { return ok.Load() >= 1 }, 2*time.Second, 5*time.Millisecond)

	cancel()
	assert.ErrorIs(t, <-runDone, context.Canceled)
}

// TestConsumer_CircuitBreaker_PermanentErrorsDoNotTrip proves M4: a stream of
// PERMANENT-error (poison) messages must NOT open the breaker — a bad message is
// not a downstream failure. With threshold 1, even one transient failure would
// open it; here every message is permanent, so the breaker stays closed and all
// messages keep flowing to the invalid sink.
func TestConsumer_CircuitBreaker_PermanentErrorsDoNotTrip(t *testing.T) {
	clk := clockwork.NewFakeClock()
	b := msgin.NewCircuitBreaker(msgin.WithBreakerClock(clk), msgin.WithBreakerThreshold(1), msgin.WithBreakerCooldown(5*time.Second))

	broker := memory.New(memory.WithBuffer(8))
	sink := &recordingSink{}
	h := func(context.Context, msgin.Message[order]) error {
		return msgin.Permanent(errors.New("poison")) // permanent → invalid sink, NOT a breaker failure
	}
	c, err := msgin.NewConsumer[order](broker, h,
		msgin.WithConsumerClock[order](clk),
		msgin.WithCircuitBreaker[order](b),
		msgin.WithInvalidMessageSink[order](sink))
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	runDone := make(chan error, 1)
	go func() { runDone <- c.Run(ctx) }()

	const total = 5
	for i := 0; i < total; i++ {
		require.NoError(t, broker.Send(ctx, msgin.New[any](order{ID: "p"})))
	}
	// All must reach the invalid sink; the breaker must never open on poison.
	require.Eventually(t, func() bool { return sink.count() == total }, 2*time.Second, 5*time.Millisecond)
	assert.True(t, b.Allow(), "permanent-error messages must not trip the breaker (M4)")

	cancel()
	assert.ErrorIs(t, <-runDone, context.Canceled)
}

// TestConsumer_CircuitBreaker_ShutdownWhileOpen proves the parked-ingress ctx-done
// path: an open breaker parks ingress, and cancelling the parent context releases
// the park (admitBreaker returns false), draining cleanly with no goroutine leak.
// The cooldown is long enough that the breaker stays open for the whole test, so
// shutdown happens while ingress is parked.
func TestConsumer_CircuitBreaker_ShutdownWhileOpen(t *testing.T) {
	clk := clockwork.NewFakeClock()
	b := msgin.NewCircuitBreaker(msgin.WithBreakerClock(clk), msgin.WithBreakerThreshold(1), msgin.WithBreakerCooldown(time.Hour))

	broker := memory.New(memory.WithBuffer(8))
	h := func(context.Context, msgin.Message[order]) error {
		return errors.New("downstream down") // trips the breaker (threshold 1)
	}
	c, err := msgin.NewConsumer[order](broker, h,
		msgin.WithConsumerClock[order](clk),
		msgin.WithCircuitBreaker[order](b))
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	runDone := make(chan error, 1)
	go func() { runDone <- c.Run(ctx) }()

	require.NoError(t, broker.Send(ctx, msgin.New[any](order{ID: "a"})))
	require.Eventually(t, func() bool { return !b.Allow() }, time.Second, 5*time.Millisecond) // opened, ingress parks

	cancel() // release the parked ingress via ctx.Done (admitBreaker → false)
	assert.ErrorIs(t, <-runDone, context.Canceled)
	assert.False(t, b.Allow(), "breaker stayed open through shutdown")
}

// TestConsumer_CircuitBreaker_ProcessGateNacksWithoutDispatch pins NF-10's
// dispatch-side gate deterministically: a delivery that passed the ingress admit
// (breaker Allow → true) must, if the breaker has since opened, be Nacked by the
// worker WITHOUT reaching the handler and WITHOUT recording a breaker outcome —
// so an already-buffered message is never driven into a failing downstream. The
// scripted breaker returns true to the admit Allow, then false to the process
// Allow, placing the message exactly in the gated window.
func TestConsumer_CircuitBreaker_ProcessGateNacksWithoutDispatch(t *testing.T) {
	b := newScriptedBreaker(true, false) // admit: allow; process: open → gate

	st := &settle{}
	src := &scriptedSource{deliveries: []msgin.Delivery{
		newSettleDelivery(order{ID: "gated"}, "gated", st),
	}}
	var handled atomic.Int64
	h := func(context.Context, msgin.Message[order]) error {
		handled.Add(1)
		return nil
	}
	c, err := msgin.NewConsumer[order](src, h,
		msgin.WithConsumerClock[order](clockwork.NewFakeClock()),
		msgin.WithCircuitBreaker[order](b))
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	runDone := make(chan error, 1)
	go func() { runDone <- c.Run(ctx) }()

	// The delivery is Nacked (requeue) by the process gate; no dispatch happens.
	require.Eventually(t, func() bool {
		_, nacks, _ := st.snapshot()
		return nacks >= 1
	}, time.Second, 5*time.Millisecond)
	assert.Equal(t, int64(0), handled.Load(), "gated message must NOT reach the handler (NF-10)")
	acks, _, _ := st.snapshot()
	assert.Equal(t, 0, acks, "gated message is Nacked, not Acked")
	assert.Empty(t, b.recorded(), "a gated (never-dispatched) message must not record a breaker outcome")

	cancel()
	assert.ErrorIs(t, <-runDone, context.Canceled)
}

// TestConsumer_CircuitBreaker_AdmitRecheckAdmits pins the missed-wakeup-free
// re-check branch: the first ingress Allow reports open (false), but the re-check
// AFTER subscribing to HalfOpen reports allowed (true) — so the message proceeds
// without parking, closing the window where a transition between the two checks
// would otherwise be lost. The message must then be dispatched normally.
func TestConsumer_CircuitBreaker_AdmitRecheckAdmits(t *testing.T) {
	b := newScriptedBreaker(false, true) // admit Allow #1 open, re-check #2 allowed

	st := &settle{}
	src := &scriptedSource{deliveries: []msgin.Delivery{
		newSettleDelivery(order{ID: "recheck"}, "recheck", st),
	}}
	var handled atomic.Int64
	h := func(context.Context, msgin.Message[order]) error {
		handled.Add(1)
		return nil
	}
	c, err := msgin.NewConsumer[order](src, h,
		msgin.WithConsumerClock[order](clockwork.NewFakeClock()),
		msgin.WithCircuitBreaker[order](b))
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	runDone := make(chan error, 1)
	go func() { runDone <- c.Run(ctx) }()

	require.Eventually(t, func() bool { return handled.Load() >= 1 }, time.Second, 5*time.Millisecond)
	acks, _, _ := st.snapshot()
	assert.Equal(t, 1, acks, "re-check-admitted message is dispatched and Acked")
	assert.Equal(t, []bool{true}, b.recorded(), "a healthy dispatch records success")

	cancel()
	assert.ErrorIs(t, <-runDone, context.Canceled)
}

// TestConsumer_Overflow_DropNewest_ShedsWithoutLeak: with n=1 and DropNewest, a
// burst behind a blocked handler sheds the surplus (Nack, no requeue) without
// leaking credit; once the handler releases, later sends still flow (credit
// recycled, not leaked).
func TestConsumer_Overflow_DropNewest_ShedsWithoutLeak(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		b := memory.New(memory.WithBuffer(16))
		var processed atomic.Int64
		var dropped atomic.Int64
		release := make(chan struct{})
		h := func(context.Context, msgin.Message[order]) error {
			<-release // hold the single credit so the burst overflows
			processed.Add(1)
			return nil
		}
		hooks := msgin.Hooks{
			OnRetry: func(_ context.Context, _ msgin.Message[any], err error) {
				if errors.Is(err, msgin.ErrOverflowDropped) {
					dropped.Add(1)
				}
			},
		}
		c, err := msgin.NewConsumer[order](b, h,
			// I3: fake consumer clock, NEVER advanced — the Task-7 sweep ticker is
			// then a clockwork fake ticker that never fires, so it cannot perturb
			// synctest.Wait() (a real ticker inside the bubble would).
			msgin.WithConsumerClock[order](clockwork.NewFakeClock()),
			msgin.WithMaxInFlight[order](1),
			msgin.WithOverflow[order](msgin.OverflowDropNewest),
			msgin.WithHooks[order](hooks))
		require.NoError(t, err)

		ctx, cancel := context.WithCancel(t.Context())
		runDone := make(chan error, 1)
		go func() { runDone <- c.Run(ctx) }()
		for i := 0; i < 6; i++ {
			require.NoError(t, b.Send(ctx, msgin.New[any](order{ID: "x"})))
		}

		synctest.Wait() // one handler holds the credit; the rest are shed
		assert.GreaterOrEqual(t, dropped.Load(), int64(1), "surplus dropped by DropNewest")

		close(release) // free the credit
		synctest.Wait()
		// The one that held the credit completed; the credit was not leaked.
		assert.GreaterOrEqual(t, processed.Load(), int64(1))

		// A fresh send after recovery still flows → credit recycled, not leaked.
		require.NoError(t, b.Send(ctx, msgin.New[any](order{ID: "y"})))
		synctest.Wait()
		assert.GreaterOrEqual(t, processed.Load(), int64(2))

		cancel()
		assert.ErrorIs(t, <-runDone, context.Canceled)
	})
}

// TestConsumer_Overflow_UnknownPolicy_BlocksNeverDrops (I1): an out-of-range
// OverflowPolicy must BLOCK (backpressure), never drop — the documented
// unknown→OverflowBlock contract. With n=1 and a held handler, a burst behind an
// unknown policy backpressures (nothing shed); once released, every message is
// processed and OnRetry(ErrOverflowDropped) never fires.
func TestConsumer_Overflow_UnknownPolicy_BlocksNeverDrops(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		b := memory.New(memory.WithBuffer(16))
		var processed atomic.Int64
		var dropped atomic.Int64
		release := make(chan struct{})
		h := func(context.Context, msgin.Message[order]) error {
			<-release
			processed.Add(1)
			return nil
		}
		hooks := msgin.Hooks{
			OnRetry: func(_ context.Context, _ msgin.Message[any], err error) {
				if errors.Is(err, msgin.ErrOverflowDropped) {
					dropped.Add(1)
				}
			},
		}
		c, err := msgin.NewConsumer[order](b, h,
			msgin.WithConsumerClock[order](clockwork.NewFakeClock()), // I3
			msgin.WithMaxInFlight[order](1),
			msgin.WithOverflow[order](msgin.OverflowPolicy(99)), // out of range → must behave as Block
			msgin.WithHooks[order](hooks))
		require.NoError(t, err)

		ctx, cancel := context.WithCancel(t.Context())
		runDone := make(chan error, 1)
		go func() { runDone <- c.Run(ctx) }()
		const total = 5
		for i := 0; i < total; i++ {
			require.NoError(t, b.Send(ctx, msgin.New[any](order{ID: "x"})))
		}

		synctest.Wait() // one holds the credit; the rest backpressure (buffered), NONE shed
		assert.Equal(t, int64(0), dropped.Load(), "unknown policy must block, never drop (I1)")

		close(release)
		synctest.Wait()
		assert.Equal(t, int64(total), processed.Load(), "all processed under backpressure — nothing was shed")

		cancel()
		assert.ErrorIs(t, <-runDone, context.Canceled)
	})
}

// TestConsumer_AttemptTracker_TTLSweepResetsIdleIds proves the bounded-tracker
// behaviour (ADR 0008 D8): an id that goes idle past defaultAttemptTTL is swept,
// so when it reappears it is treated as attempt 1 again (fresh backoff),
// observable via the delay carried on the Nack.
//
// Runs under synctest for DETERMINISM: the sweep runs on a goroutine, so after
// clk.Advance fires the sweep ticker we must be sure sweep() has completed (and
// re-blocked) BEFORE re-delivering — otherwise a sweep-vs-deliver race would
// make it flaky under -count. synctest.Wait() gives exactly that barrier. The
// clockwork fake clock is advanced INSIDE the bubble but is independent of
// synctest's own time; the sweep ticker fires ONLY on these explicit Advances,
// and clockwork fake tickers use non-blocking sends with no background
// goroutine, so they compose cleanly with synctest.
func TestConsumer_AttemptTracker_TTLSweepResetsIdleIds(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		clk := clockwork.NewFakeClock()
		src := newControllableSource()
		const initial = 10 * time.Millisecond
		h := func(context.Context, msgin.Message[order]) error { return errors.New("always transient") }

		c, err := msgin.NewConsumer[order](src, h,
			msgin.WithConsumerClock[order](clk),
			msgin.WithRetryPolicy[order](msgin.RetryPolicy{
				Backoff: msgin.ExponentialBackoff{Initial: initial, Mult: 2.0}, // RandomizationFactor 0 => exact
			}))
		require.NoError(t, err)

		ctx, cancel := context.WithCancel(t.Context())
		runDone := make(chan error, 1)
		go func() { runDone <- c.Run(ctx) }()

		st := &settle{}
		deliverX := func() {
			src.deliver(ctx, msgin.Delivery{Msg: msgin.New[any](order{ID: "o"}, msgin.WithID("x")), Ack: st.ack, Nack: st.nack})
		}

		// First delivery of x → attempt 1 → Nack delay = Initial.
		deliverX()
		synctest.Wait()
		_, n, delays := st.snapshot()
		require.Equal(t, 1, n)
		assert.Equal(t, initial, delays[0], "first ever attempt for x → Delay(0) = Initial")

		// Advance past the TTL; synctest.Wait() makes the sweep deterministic
		// (it runs and re-blocks before we re-deliver — no sweep-vs-deliver race).
		clk.Advance(6 * time.Minute) // > defaultAttemptTTL (5m) → sweep evicts idle x
		synctest.Wait()

		// Re-deliver x: with the entry swept, it must be attempt 1 again (fresh delay).
		deliverX()
		synctest.Wait()
		_, n, delays = st.snapshot()
		require.Equal(t, 2, n)
		assert.Equal(t, initial, delays[1], "after a TTL gap x is treated as attempt 1 again (idle entry swept)")

		cancel()
		assert.ErrorIs(t, <-runDone, context.Canceled)
	})
}

// TestConsumer_AttemptTracker_TTLSweepKeepsRefreshedId covers the sweep's KEEP
// branch (M3): an id refreshed within the TTL survives a sweep that evicts an
// idle sibling. Two ids a and b start at attempt 1; b is re-delivered (refreshed)
// inside the TTL window; when the 5m sweep fires, a (idle since t0) is evicted
// but b (age 2m) is kept — so a restarts at attempt 1 while b keeps climbing to
// attempt 3. Covers sweep()'s age>=ttl (a) AND age<ttl (b) branches in one test.
func TestConsumer_AttemptTracker_TTLSweepKeepsRefreshedId(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		clk := clockwork.NewFakeClock()
		src := newControllableSource()
		const initial = 10 * time.Millisecond
		const mult = 2.0
		h := func(context.Context, msgin.Message[order]) error { return errors.New("always transient") }

		c, err := msgin.NewConsumer[order](src, h,
			msgin.WithConsumerClock[order](clk),
			msgin.WithRetryPolicy[order](msgin.RetryPolicy{
				Backoff: msgin.ExponentialBackoff{Initial: initial, Mult: mult},
			}))
		require.NoError(t, err)

		ctx, cancel := context.WithCancel(t.Context())
		runDone := make(chan error, 1)
		go func() { runDone <- c.Run(ctx) }()

		stA, stB := &settle{}, &settle{}
		deliver := func(st *settle, id string) {
			src.deliver(ctx, msgin.Delivery{
				Msg: msgin.New[any](order{ID: "o"}, msgin.WithID(id)),
				Ack: st.ack, Nack: st.nack,
			})
		}

		deliver(stA, "a") // a attempt 1, lastSeen = t0
		deliver(stB, "b") // b attempt 1, lastSeen = t0
		synctest.Wait()

		clk.Advance(3 * time.Minute) // < TTL: sweep ticker (5m) does NOT fire
		synctest.Wait()
		deliver(stB, "b") // b attempt 2, lastSeen(b) = t0+3m (refreshed within TTL)
		synctest.Wait()

		clk.Advance(2 * time.Minute) // total 5m → sweep fires: a age 5m evicted, b age 2m kept
		synctest.Wait()

		deliver(stA, "a") // a was swept → attempt 1 again → delay Initial
		synctest.Wait()
		_, nA, delaysA := stA.snapshot()
		require.Equal(t, 2, nA)
		assert.Equal(t, initial, delaysA[1], "a idled past TTL → swept → attempt 1 again")

		deliver(stB, "b") // b was kept → attempt 3 → delay Initial*Mult^2 (never reset)
		synctest.Wait()
		_, nB, delaysB := stB.snapshot()
		require.Equal(t, 3, nB)
		assert.Equal(t, time.Duration(float64(initial)*mult*mult), delaysB[2], "b refreshed within TTL → kept → attempt 3 keeps climbing")

		cancel()
		assert.ErrorIs(t, <-runDone, context.Canceled)
	})
}

// TestConsumer_AttemptTracker_NF2_ActiveIdNotSwept proves NF-2: a
// continuously-redelivering id is NEVER reset by the sweep — it climbs to the
// dead-letter after MaxAttempts even though sweeps fire during redelivery.
func TestConsumer_AttemptTracker_NF2_ActiveIdNotSwept(t *testing.T) {
	st := &settle{}
	src := &reemittingSource{st: st, id: "poison", payload: order{ID: "o"}}
	dlq := &recordingSink{}
	h := func(context.Context, msgin.Message[order]) error { return errors.New("always transient") }

	c, err := msgin.NewConsumer[order](src, h,
		msgin.WithRetryPolicy[order](msgin.RetryPolicy{MaxAttempts: 3, DeadLetter: dlq}))
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	runDone := make(chan error, 1)
	go func() { runDone <- c.Run(ctx) }()

	// memory-style immediate redelivery keeps the id fresh; it must dead-letter at
	// attempt 3, proving the sweep never reset a still-in-redelivery id.
	require.Eventually(t, func() bool { return dlq.count() == 1 }, 2*time.Second, 5*time.Millisecond)
	_, nacks, _ := st.snapshot()
	assert.Equal(t, 2, nacks, "attempts 1 and 2 Nacked; 3 dead-lettered — count never reset (NF-2)")

	cancel()
	assert.ErrorIs(t, <-runDone, context.Canceled)
}

// TestConsumer_MaxPayloadBytes_CapsWirePayload proves WithMaxPayloadBytes (ADR
// 0009 D5): an over-size wire payload is settled as a PERMANENT invalid message
// (ErrPayloadTooLarge) and diverted to the invalid sink BEFORE the codec runs,
// while an under-size payload decodes and dispatches normally, and a disabled cap
// (n<=0) never rejects. Driven over a real []byte StreamingSource.
func TestConsumer_MaxPayloadBytes_CapsWirePayload(t *testing.T) {
	tests := []struct {
		name    string
		max     int
		payload []byte
		assert  func(t *testing.T, sink *recordingSink, hooks *hookRec, handled *atomic.Int64)
	}{
		{"over-size payload is diverted to the invalid sink without dispatching",
			5, []byte(`{"id":"way too long to fit"}`),
			func(t *testing.T, sink *recordingSink, hooks *hookRec, handled *atomic.Int64) {
				assert.Equal(t, 1, sink.count(), "over-size payload diverted to invalid sink")
				assert.Equal(t, 1, hooks.count("invalid"), "OnInvalidMessage fired")
				assert.Equal(t, int64(0), handled.Load(), "handler never ran on the over-size message")
			}},
		{"under-size payload decodes and dispatches",
			1000, []byte(`{"id":"o"}`),
			func(t *testing.T, sink *recordingSink, hooks *hookRec, handled *atomic.Int64) {
				assert.Equal(t, int64(1), handled.Load(), "under-size payload handled")
				assert.Equal(t, 0, sink.count(), "under-size payload not diverted")
			}},
		{"disabled cap never rejects",
			0, []byte(`{"id":"o"}`),
			func(t *testing.T, sink *recordingSink, hooks *hookRec, handled *atomic.Int64) {
				assert.Equal(t, int64(1), handled.Load(), "cap disabled → payload handled regardless")
				assert.Equal(t, 0, sink.count())
			}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var handled atomic.Int64
			sink := &recordingSink{}
			hooks := newHookRec()
			src := &byteStreamSource{id: "m1", payload: tc.payload, st: &settle{}}
			h := func(context.Context, msgin.Message[order]) error { handled.Add(1); return nil }

			c, err := msgin.NewConsumer[order](src, h,
				msgin.WithMaxPayloadBytes[order](tc.max),
				msgin.WithInvalidMessageSink[order](sink),
				msgin.WithHooks[order](hooks.hooks()))
			require.NoError(t, err)

			ctx, cancel := context.WithCancel(t.Context())
			runDone := make(chan error, 1)
			go func() { runDone <- c.Run(ctx) }()

			// Wait on the terminal signal: the invalid hook fires AFTER sink.Send on
			// the divert path (so sink.count()==1 is already visible), and handled
			// increments before the Ack on the dispatch path — either is race-free.
			require.Eventually(t, func() bool {
				return hooks.count("invalid") >= 1 || handled.Load() >= 1
			}, 2*time.Second, 5*time.Millisecond)

			tc.assert(t, sink, hooks, &handled)
			cancel()
			assert.ErrorIs(t, <-runDone, context.Canceled)
		})
	}
}

// TestConsumer_WithAttemptTTL_UsesConfiguredTTL proves WithAttemptTTL (ADR 0009
// D3) threads the configured TTL into the sweep ticker: a 1-minute TTL sweeps an
// idle id after a 90s gap — a gap the default 5m TTL would NOT sweep — so the id
// reappears as attempt 1 (fresh backoff). Same synctest determinism as the
// defaultAttemptTTL sweep tests above.
func TestConsumer_WithAttemptTTL_UsesConfiguredTTL(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		clk := clockwork.NewFakeClock()
		src := newControllableSource()
		const initial = 10 * time.Millisecond
		h := func(context.Context, msgin.Message[order]) error { return errors.New("always transient") }

		c, err := msgin.NewConsumer[order](src, h,
			msgin.WithConsumerClock[order](clk),
			msgin.WithAttemptTTL[order](time.Minute), // << far below the 5m default
			msgin.WithRetryPolicy[order](msgin.RetryPolicy{
				Backoff: msgin.ExponentialBackoff{Initial: initial, Mult: 2.0},
			}))
		require.NoError(t, err)

		ctx, cancel := context.WithCancel(t.Context())
		runDone := make(chan error, 1)
		go func() { runDone <- c.Run(ctx) }()

		st := &settle{}
		deliverX := func() {
			src.deliver(ctx, msgin.Delivery{Msg: msgin.New[any](order{ID: "o"}, msgin.WithID("x")), Ack: st.ack, Nack: st.nack})
		}

		deliverX()
		synctest.Wait()
		_, n, delays := st.snapshot()
		require.Equal(t, 1, n)
		assert.Equal(t, initial, delays[0], "first attempt for x → Delay(0) = Initial")

		clk.Advance(90 * time.Second) // > configured 1m TTL, < default 5m → sweep evicts idle x
		synctest.Wait()

		deliverX()
		synctest.Wait()
		_, n, delays = st.snapshot()
		require.Equal(t, 2, n)
		assert.Equal(t, initial, delays[1], "configured 1m TTL swept idle x within 90s → attempt 1 again (default 5m would not have)")

		cancel()
		assert.ErrorIs(t, <-runDone, context.Canceled)
	})
}

// TestConsumer_SafeDecode_CodecPanicRoutesToInvalidWithErrPayloadDecode proves
// safeDecode's panic recovery (ADR 0010 D6, fold-in #4): a codec whose Decode
// panics on a WIRE ([]byte) payload must be classified EXACTLY like a real
// decode error — ErrPayloadDecode, permanent → invalid sink — never crash the
// process. byteStreamSource is a genuine wire StreamingSource (not a
// LiveValueSource), so the codec-decode branch of c.decode (not the
// live-value type assert) is the one exercised.
func TestConsumer_SafeDecode_CodecPanicRoutesToInvalidWithErrPayloadDecode(t *testing.T) {
	buf := &lockedBuffer{}
	st := &settle{}
	src := &byteStreamSource{id: "x", payload: []byte(`{"id":"o1"}`), st: st}

	var causeMu sync.Mutex
	var cause error
	var invalidCalls atomic.Int64
	hooks := msgin.Hooks{
		OnInvalidMessage: func(_ context.Context, _ msgin.Message[any], err error) {
			causeMu.Lock()
			cause = err
			causeMu.Unlock()
			invalidCalls.Add(1)
		},
	}
	var handlerCalls atomic.Int64
	h := func(context.Context, msgin.Message[order]) error { handlerCalls.Add(1); return nil }

	c, err := msgin.NewConsumer[order](src, h,
		msgin.WithConsumerCodec[order](panicDecodeCodec{}),
		msgin.WithHooks[order](hooks),
		msgin.WithLogger[order](slog.New(slog.NewTextHandler(buf, nil))))
	require.NoError(t, err)

	stop := runConsumer(t, c)
	defer stop()

	require.Eventually(t, func() bool { return invalidCalls.Load() >= 1 }, time.Second, 5*time.Millisecond,
		"a panicking codec must still settle the message (invalid sink), not hang or crash")

	causeMu.Lock()
	gotCause := cause
	causeMu.Unlock()
	assert.ErrorIs(t, gotCause, msgin.ErrPayloadDecode, "a panicking codec classifies exactly like a real decode error")
	assert.Equal(t, int64(0), handlerCalls.Load(), "the handler must not run: decode failed before dispatch")

	acks, nacks, _ := st.snapshot()
	assert.Equal(t, 1, acks, "nil invalid sink discards (acks) the permanent decode failure")
	assert.Equal(t, 0, nacks)

	logged := buf.String()
	assert.Contains(t, logged, "PayloadCodec.Decode panicked", "the panic is logged")
	assert.Contains(t, logged, "id=x", "the ERROR log names the message id, never the payload")
}

// TestConsumer_SafeSend_SinkPanicRetriesInsteadOfLosingMessage proves
// safeSend's panic recovery (ADR 0010 D6): a configured invalid-message sink
// whose Send panics must be routed EXACTLY as a real sink.Send error is today
// — the message was NOT diverted, so it is retried (Nack), never lost.
func TestConsumer_SafeSend_SinkPanicRetriesInsteadOfLosingMessage(t *testing.T) {
	buf := &lockedBuffer{}
	st := &settle{}
	src := &scriptedSource{deliveries: []msgin.Delivery{newSettleDelivery(order{ID: "bad"}, "x", st)}}
	h := func(context.Context, msgin.Message[order]) error { return msgin.Permanent(errors.New("bad")) }

	c, err := msgin.NewConsumer[order](src, h,
		msgin.WithInvalidMessageSink[order](panicSendSink{}),
		msgin.WithLogger[order](slog.New(slog.NewTextHandler(buf, nil))))
	require.NoError(t, err)

	stop := runConsumer(t, c)
	defer stop()

	require.Eventually(t, func() bool { _, nacks, _ := st.snapshot(); return nacks >= 1 }, time.Second, 5*time.Millisecond,
		"a panicking sink must retry (Nack) the message, not lose it")

	acks, nacks, _ := st.snapshot()
	assert.Equal(t, 0, acks, "the message was never terminally diverted (sink panicked, not accepted)")
	assert.GreaterOrEqual(t, nacks, 1)

	logged := buf.String()
	assert.Contains(t, logged, "OutboundAdapter.Send panicked", "the panic is logged")
	assert.Contains(t, logged, "id=x", "the ERROR log names the message id, never the payload")
}

// TestConsumer_SafeSend_NilSinkPassthroughUnaffected proves the safeSend
// rewiring left the nil-sink discard path (which never calls sink.Send)
// unaffected: a permanent classification with NO invalid sink configured
// still discards (Acks) exactly as before.
func TestConsumer_SafeSend_NilSinkPassthroughUnaffected(t *testing.T) {
	st := &settle{}
	src := &scriptedSource{deliveries: []msgin.Delivery{newSettleDelivery(order{ID: "bad"}, "x", st)}}
	h := func(context.Context, msgin.Message[order]) error { return msgin.Permanent(errors.New("bad")) }

	c, err := msgin.NewConsumer[order](src, h) // no WithInvalidMessageSink → nil sink
	require.NoError(t, err)

	stop := runConsumer(t, c)
	defer stop()

	require.Eventually(t, func() bool { acks, _, _ := st.snapshot(); return acks >= 1 }, time.Second, 5*time.Millisecond)
	acks, nacks, _ := st.snapshot()
	assert.Equal(t, 1, acks, "nil sink discards (acks) unaffected by the safeSend rewiring")
	assert.Equal(t, 0, nacks)
}

// TestConsumer_SafeAck_PanicLogsAndReleasesCredit proves safeAck's panic
// recovery (ADR 0010 D6): a Delivery whose Ack panics on the success path
// must be ERROR-logged (id only, never the payload), settled via finish, and
// — crucially — must still have released its credit BEFORE the panicking call
// (the manage wrapper releases first), so the flood defense is never
// compromised by a misbehaving adapter. WithMaxInFlight(1) makes the second
// message's admission a direct proof of the first credit's release: if the
// panic pinned the credit, the second message would never be admitted and
// the test would time out. It also proves NO per-method dedup (ADR 0009 D1's
// governor dedup does NOT apply here — the brief says an adapter panic is not
// the deterministic-per-message governor case): both ids are logged.
func TestConsumer_SafeAck_PanicLogsAndReleasesCredit(t *testing.T) {
	buf := &lockedBuffer{}
	ps := &panicSettle{ackPanics: true}
	deliveries := []msgin.Delivery{
		{Msg: msgin.New[any](order{ID: "o"}, msgin.WithID("a")), Ack: ps.ack, Nack: ps.nack},
		{Msg: msgin.New[any](order{ID: "o"}, msgin.WithID("b")), Ack: ps.ack, Nack: ps.nack},
	}
	src := &scriptedSource{deliveries: deliveries}
	var handled atomic.Int64
	h := func(context.Context, msgin.Message[order]) error { handled.Add(1); return nil }

	c, err := msgin.NewConsumer[order](src, h,
		msgin.WithMaxInFlight[order](1),
		msgin.WithLogger[order](slog.New(slog.NewTextHandler(buf, nil))))
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- c.Run(ctx) }()

	require.Eventually(t, func() bool { return handled.Load() >= 2 }, 2*time.Second, 5*time.Millisecond,
		"a panicking Ack must still release its credit (release-first, via manage) so the 2nd message is admitted")

	cancel()
	select {
	case runErr := <-done:
		assert.ErrorIs(t, runErr, context.Canceled)
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not return: a panicking Ack must not hang the drain")
	}

	logged := buf.String()
	assert.Contains(t, logged, "Delivery.Ack panicked", "the panic is logged")
	assert.Contains(t, logged, "id=a", "both messages' panics are logged (no per-method dedup)")
	assert.Contains(t, logged, "id=b", "both messages' panics are logged (no per-method dedup)")
	assert.Contains(t, logged, "settle failed", "finish logs the synthetic settle error too")
}

// TestConsumer_SafeNack_PanicLogsAndReleasesCredit is safeAck's sibling for
// the transient (Nack) path (ADR 0010 D6): a Delivery whose Nack panics must
// be ERROR-logged and must still release its credit, proven the same way —
// WithMaxInFlight(1) plus a 2nd message that can only be admitted if the
// 1st's credit was released despite the panic.
func TestConsumer_SafeNack_PanicLogsAndReleasesCredit(t *testing.T) {
	buf := &lockedBuffer{}
	ps := &panicSettle{nackPanics: true}
	deliveries := []msgin.Delivery{
		{Msg: msgin.New[any](order{ID: "o"}, msgin.WithID("a")), Ack: ps.ack, Nack: ps.nack},
		{Msg: msgin.New[any](order{ID: "o"}, msgin.WithID("b")), Ack: ps.ack, Nack: ps.nack},
	}
	src := &scriptedSource{deliveries: deliveries}
	var handled atomic.Int64
	h := func(context.Context, msgin.Message[order]) error {
		handled.Add(1)
		return errors.New("transient boom")
	}

	c, err := msgin.NewConsumer[order](src, h,
		msgin.WithMaxInFlight[order](1),
		msgin.WithLogger[order](slog.New(slog.NewTextHandler(buf, nil))))
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- c.Run(ctx) }()

	require.Eventually(t, func() bool { return handled.Load() >= 2 }, 2*time.Second, 5*time.Millisecond,
		"a panicking Nack must still release its credit so the 2nd message is admitted")

	cancel()
	select {
	case runErr := <-done:
		assert.ErrorIs(t, runErr, context.Canceled)
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not return: a panicking Nack must not hang the drain")
	}

	logged := buf.String()
	assert.Contains(t, logged, "Delivery.Nack panicked", "the panic is logged")
	assert.Contains(t, logged, "id=a", "both messages' panics are logged (no per-method dedup)")
	assert.Contains(t, logged, "id=b", "both messages' panics are logged (no per-method dedup)")
	assert.Contains(t, logged, "settle failed", "finish logs the synthetic settle error too")
}

// TestConsumer_Overflow_DropNewest_ShedPanicRecovers proves review finding I-1's
// fix: admit's overflow-shed Nack (the OverflowDropNewest/DropOldest/Reject
// branch in admit, consumer.go) is now routed through safeNack. A shed
// delivery whose Nack panics — a realistic adapter bug — must not crash the
// process. WithMaxInFlight(1) plus a handler that blocks after signaling entry
// makes the shed deterministic: by the time the 2nd delivery is forwarded, the
// 1st has already acquired (and is holding) the sole credit, so the 2nd
// unconditionally takes the tryAcquire-fails shed path in admit.
func TestConsumer_Overflow_DropNewest_ShedPanicRecovers(t *testing.T) {
	buf := &lockedBuffer{}
	src := newControllableSource()
	release := make(chan struct{})
	entered := make(chan struct{})
	var processed atomic.Int64
	h := func(context.Context, msgin.Message[order]) error {
		close(entered) // signals the sole credit is now held (past admit's acquire)
		<-release
		processed.Add(1)
		return nil
	}
	c, err := msgin.NewConsumer[order](src, h,
		msgin.WithMaxInFlight[order](1),
		msgin.WithOverflow[order](msgin.OverflowDropNewest),
		msgin.WithLogger[order](slog.New(slog.NewTextHandler(buf, nil))))
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- c.Run(ctx) }()

	held := &settle{}
	src.deliver(ctx, msgin.Delivery{Msg: msgin.New[any](order{ID: "o"}, msgin.WithID("held")), Ack: held.ack, Nack: held.nack})

	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("handler for the held delivery never entered; the sole credit was never observably acquired")
	}

	var shedAcked atomic.Bool
	nackCalled := make(chan struct{})
	shedNack := func(context.Context, bool, time.Duration) error {
		close(nackCalled)
		panic("shed-nack boom")
	}
	src.deliver(ctx, msgin.Delivery{
		Msg:  msgin.New[any](order{ID: "o"}, msgin.WithID("shed")),
		Ack:  func(context.Context) error { shedAcked.Store(true); return nil },
		Nack: shedNack,
	})

	select {
	case <-nackCalled:
	case <-time.After(2 * time.Second):
		t.Fatal("the shed delivery's (panicking) Nack was never invoked")
	}
	assert.False(t, shedAcked.Load(), "a shed delivery is Nacked, never Acked")

	close(release) // let the held handler finish
	require.Eventually(t, func() bool { return processed.Load() >= 1 }, time.Second, 5*time.Millisecond)

	cancel()
	select {
	case runErr := <-done:
		assert.ErrorIs(t, runErr, context.Canceled)
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not return: a panicking overflow-shed Nack must not hang the drain")
	}

	logged := buf.String()
	assert.Contains(t, logged, "Delivery.Nack panicked", "the panic is logged")
	assert.Contains(t, logged, "id=shed", "the ERROR log names the message id, never the payload")
}
