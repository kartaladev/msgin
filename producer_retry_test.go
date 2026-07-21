package msgin_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/jonboulle/clockwork"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"

	"github.com/kartaladev/msgin"
)

// outboundFunc adapts a func to msgin.OutboundAdapter.
type outboundFunc func(context.Context, msgin.Message[any]) error

func (f outboundFunc) Send(ctx context.Context, msg msgin.Message[any]) error { return f(ctx, msg) }

// scriptedOutbound returns a scripted error per attempt and records every
// message it was handed. Safe for concurrent use: Send runs on the goroutine
// under test while a driver advances the clock from another.
type scriptedOutbound struct {
	mu      sync.Mutex
	script  []error // script[i] is attempt i+1's result; past the end the last entry repeats
	calls   int
	ctxs    []error // ctxs[i] is ctx.Err() as seen by attempt i+1
	gotMsgs []msgin.Message[any]
}

func newScriptedOutbound(script ...error) *scriptedOutbound {
	return &scriptedOutbound{script: script}
}

func (o *scriptedOutbound) Send(ctx context.Context, msg msgin.Message[any]) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.calls++
	o.ctxs = append(o.ctxs, ctx.Err())
	o.gotMsgs = append(o.gotMsgs, msg)
	switch {
	case len(o.script) == 0:
		return nil
	case o.calls <= len(o.script):
		return o.script[o.calls-1]
	default:
		return o.script[len(o.script)-1]
	}
}

func (o *scriptedOutbound) attempts() int {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.calls
}

func (o *scriptedOutbound) messages() []msgin.Message[any] {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]msgin.Message[any](nil), o.gotMsgs...)
}

// lastCtxErr reports ctx.Err() as observed by the most recent Send — how the
// detached-dead-letter branch (D1) is proven.
func (o *scriptedOutbound) lastCtxErr() error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if len(o.ctxs) == 0 {
		return nil
	}
	return o.ctxs[len(o.ctxs)-1]
}

// retryHarness runs p.Send on its own goroutine and lets the test step the fake
// clock deterministically. err is read only after done is closed, so there is no
// race and no require/t.Fatal off the test goroutine.
type retryHarness struct {
	clock *clockwork.FakeClock
	done  chan struct{}
	err   error
}

func runSend(t *testing.T, p msgin.Producer[[]byte], clock *clockwork.FakeClock, ctx context.Context) *retryHarness {
	t.Helper()
	h := &retryHarness{clock: clock, done: make(chan struct{})}
	go func() {
		defer close(h.done)
		h.err = p.Send(ctx, msgin.New[[]byte]([]byte("payload")))
	}()
	return h
}

// stepTo advances the clock past a single expected wait in TWO phases, so the
// assertion can detect UNDER-waiting as well as over-waiting: after the first
// advance the producer must still be parked. (A one-shot Advance(want) followed
// by "did it return?" is true by construction — Plan 021 lesson.)
func (h *retryHarness) stepTo(t *testing.T, want time.Duration) {
	t.Helper()
	// BOUNDED, deliberately. With t.Context() this blocks until the PACKAGE
	// timeout when a regression means the producer never parks, taking the whole
	// binary down with "panic: test timed out" instead of reporting the useful
	// message below. Verified: removing the minRetryDelay floor, or flipping
	// attempt >= MaxAttempts to >, produced a 60-90s hang rather than a failure.
	blockCtx, blockCancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer blockCancel()
	require.NoError(t, h.clock.BlockUntilContext(blockCtx, 1), "producer never parked on a timer")
	if want > 0 {
		h.clock.Advance(want - time.Nanosecond)
		select {
		case <-h.done:
			t.Fatalf("Send returned after %v, but the expected wait was %v — it under-waited", want-time.Nanosecond, want)
		case <-time.After(20 * time.Millisecond):
		}
		h.clock.Advance(time.Nanosecond)
	}
}

func (h *retryHarness) wait(t *testing.T) error {
	t.Helper()
	select {
	case <-h.done:
		return h.err
	case <-time.After(5 * time.Second):
		t.Fatal("Send did not return")
		return nil
	}
}

func TestProducerRetry(t *testing.T) {
	defer goleak.VerifyNone(t)

	transient := errors.New("transient")

	tests := []struct {
		name   string
		script []error
		// waits are the delays the producer is EXPECTED to park for, in order.
		waits  []time.Duration
		policy func(dlq msgin.OutboundAdapter) msgin.RetryPolicy
		assert func(t *testing.T, out, dlq *scriptedOutbound, err error)
	}{
		{
			name:   "unset policy sends exactly once",
			script: []error{transient},
			policy: nil, // no WithProducerRetry at all
			assert: func(t *testing.T, out, dlq *scriptedOutbound, err error) {
				t.Helper()
				assert.ErrorIs(t, err, transient)
				assert.NotErrorIs(t, err, msgin.ErrDeadLettered)
				assert.Equal(t, 1, out.attempts(), "no policy must mean no retry")
				assert.Equal(t, 0, dlq.attempts())
			},
		},
		{
			name:   "first attempt succeeds",
			script: []error{nil},
			policy: func(dlq msgin.OutboundAdapter) msgin.RetryPolicy {
				return msgin.RetryPolicy{MaxAttempts: 3, DeadLetter: dlq}
			},
			assert: func(t *testing.T, out, dlq *scriptedOutbound, err error) {
				t.Helper()
				require.NoError(t, err)
				assert.Equal(t, 1, out.attempts())
				assert.Equal(t, 0, dlq.attempts())
			},
		},
		{
			name:   "transient then success",
			script: []error{transient, nil},
			waits:  []time.Duration{time.Second},
			policy: func(dlq msgin.OutboundAdapter) msgin.RetryPolicy {
				return msgin.RetryPolicy{
					MaxAttempts: 3,
					Backoff:     msgin.ExponentialBackoff{Initial: time.Second, Mult: 2},
					DeadLetter:  dlq,
				}
			},
			assert: func(t *testing.T, out, dlq *scriptedOutbound, err error) {
				t.Helper()
				require.NoError(t, err)
				assert.Equal(t, 2, out.attempts())
				assert.Equal(t, 0, dlq.attempts())
			},
		},
		{
			name:   "exhaustion dead-letters and returns the cause",
			script: []error{transient},
			waits:  []time.Duration{time.Second, 2 * time.Second},
			policy: func(dlq msgin.OutboundAdapter) msgin.RetryPolicy {
				return msgin.RetryPolicy{
					MaxAttempts: 3,
					Backoff:     msgin.ExponentialBackoff{Initial: time.Second, Mult: 2},
					DeadLetter:  dlq,
				}
			},
			assert: func(t *testing.T, out, dlq *scriptedOutbound, err error) {
				t.Helper()
				assert.ErrorIs(t, err, transient, "a successful divert must still surface the cause")
				assert.ErrorIs(t, err, msgin.ErrDeadLettered, "the caller must be able to tell DLQ from outright failure")
				assert.Equal(t, 3, out.attempts())
				require.Equal(t, 1, dlq.attempts())
				got := dlq.messages()
				require.Len(t, got, 1)
				assert.Equal(t, []byte("payload"), got[0].Payload())
			},
		},
		{
			name:   "permanent marker is not retried",
			script: []error{msgin.Permanent(transient)},
			policy: func(dlq msgin.OutboundAdapter) msgin.RetryPolicy {
				return msgin.RetryPolicy{MaxAttempts: 5, DeadLetter: dlq}
			},
			assert: func(t *testing.T, out, dlq *scriptedOutbound, err error) {
				t.Helper()
				assert.ErrorIs(t, err, transient)
				assert.NotErrorIs(t, err, msgin.ErrDeadLettered)
				assert.Equal(t, 1, out.attempts(), "permanent must consume no attempt budget")
				assert.Equal(t, 0, dlq.attempts(), "permanent must never dead-letter")
			},
		},
		{
			name:   "sentinel-permanent is not retried",
			script: []error{msgin.ErrPayloadTooLarge},
			policy: func(dlq msgin.OutboundAdapter) msgin.RetryPolicy {
				return msgin.RetryPolicy{MaxAttempts: 5, DeadLetter: dlq}
			},
			assert: func(t *testing.T, out, dlq *scriptedOutbound, err error) {
				t.Helper()
				assert.ErrorIs(t, err, msgin.ErrPayloadTooLarge)
				assert.Equal(t, 1, out.attempts())
				assert.Equal(t, 0, dlq.attempts())
			},
		},
		{
			name:   "a zero computed backoff is floored, not spun",
			script: []error{transient, nil},
			waits:  []time.Duration{100 * time.Millisecond}, // minRetryDelay
			policy: func(dlq msgin.OutboundAdapter) msgin.RetryPolicy {
				// Backoff present but always zero: Initial <= 0 makes Delay return 0.
				return msgin.RetryPolicy{
					MaxAttempts: 3,
					Backoff:     msgin.ExponentialBackoff{},
					DeadLetter:  dlq,
				}
			},
			assert: func(t *testing.T, out, dlq *scriptedOutbound, err error) {
				t.Helper()
				require.NoError(t, err)
				assert.Equal(t, 2, out.attempts())
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clock := clockwork.NewFakeClock()
			out := newScriptedOutbound(tt.script...)
			dlq := newScriptedOutbound(nil)

			opts := []msgin.ProducerOption[[]byte]{
				msgin.WithProducerClock[[]byte](clock),
				// REQUIRED, not decorative. scriptedOutbound is not a
				// LiveValueSource, so resolveCodec would install
				// JSONPayloadCodec[[]byte] and json.Marshal([]byte("payload"))
				// is []byte(`"cGF5bG9hZA=="`).
				msgin.WithProducerCodec[[]byte](msgin.BytesPayloadCodec{}),
			}
			if tt.policy != nil {
				opts = append(opts, msgin.WithProducerRetry[[]byte](tt.policy(dlq)))
			}

			p, err := msgin.NewProducer[[]byte](out, opts...)
			require.NoError(t, err)

			h := runSend(t, p, clock, t.Context())
			for _, w := range tt.waits {
				h.stepTo(t, w)
			}
			tt.assert(t, out, dlq, h.wait(t))
		})
	}
}

// TestProducerPermanentBeatsRetryAfter proves the precedence rule by its
// OBSERVABLE consequence, not by errors.Is: a cause is matchable through either
// marker independently, so an errors.Is assertion proves nothing about which one
// won. What only Permanent produces is: exactly one attempt, no dead-letter, and
// no clock advance at all.
func TestProducerPermanentBeatsRetryAfter(t *testing.T) {
	defer goleak.VerifyNone(t)

	cause := errors.New("boom")

	// Both nesting orders must produce the identical observable outcome, so the
	// assert closure is shared deliberately: the expectation still belongs to
	// the case, not to the loop body.
	assertPermanentWon := func(t *testing.T, out, dlq *scriptedOutbound, elapsed time.Duration, err error) {
		t.Helper()
		assert.ErrorIs(t, err, cause)
		assert.Equal(t, 1, out.attempts(), "Permanent must stop after one attempt despite the RetryAfter marker")
		assert.Equal(t, 0, dlq.attempts(), "Permanent must never dead-letter")
		assert.Equal(t, time.Duration(0), elapsed, "Permanent must not park for the RetryAfter delay")
	}

	tests := []struct {
		name   string
		err    error
		assert func(t *testing.T, out, dlq *scriptedOutbound, elapsed time.Duration, err error)
	}{
		{
			name:   "permanent outside retry-after",
			err:    msgin.Permanent(msgin.RetryAfter(cause, time.Minute)),
			assert: assertPermanentWon,
		},
		{
			name:   "retry-after outside permanent",
			err:    msgin.RetryAfter(msgin.Permanent(cause), time.Minute),
			assert: assertPermanentWon,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clock := clockwork.NewFakeClock()
			start := clock.Now()
			out := newScriptedOutbound(tt.err)
			dlq := newScriptedOutbound(nil)

			p, err := msgin.NewProducer[[]byte](out,
				msgin.WithProducerClock[[]byte](clock),
				msgin.WithProducerRetry[[]byte](msgin.RetryPolicy{MaxAttempts: 5, DeadLetter: dlq}),
			)
			require.NoError(t, err)

			sendErr := p.Send(t.Context(), msgin.New[[]byte]([]byte("payload")))

			tt.assert(t, out, dlq, clock.Now().Sub(start), sendErr)
		})
	}
}

// TestProducerDeadLetterFailure covers both failure arms of the divert: a sink
// returning an error and a sink panicking. Both join onto the cause, never
// swallow it, and the panic must not escape to the caller. Neither arm carries
// ErrDeadLettered — nothing was successfully dead-lettered.
func TestProducerDeadLetterFailure(t *testing.T) {
	defer goleak.VerifyNone(t)

	transient := errors.New("transient")
	dlqErr := errors.New("dlq down")

	tests := []struct {
		name   string
		dlq    msgin.OutboundAdapter
		assert func(t *testing.T, err error)
	}{
		{
			name: "dead-letter error is joined",
			dlq:  outboundFunc(func(context.Context, msgin.Message[any]) error { return dlqErr }),
			assert: func(t *testing.T, err error) {
				t.Helper()
				assert.ErrorIs(t, err, transient)
				assert.ErrorIs(t, err, dlqErr)
				assert.NotErrorIs(t, err, msgin.ErrDeadLettered)
			},
		},
		{
			name: "dead-letter panic is recovered and joined",
			dlq:  outboundFunc(func(context.Context, msgin.Message[any]) error { panic("dlq exploded") }),
			assert: func(t *testing.T, err error) {
				t.Helper()
				assert.ErrorIs(t, err, transient)
				assert.Contains(t, err.Error(), "dlq exploded")
				assert.Contains(t, err.Error(), "panicked")
				assert.NotErrorIs(t, err, msgin.ErrDeadLettered)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clock := clockwork.NewFakeClock()
			out := newScriptedOutbound(transient)
			p, err := msgin.NewProducer[[]byte](out,
				msgin.WithProducerClock[[]byte](clock),
				msgin.WithProducerRetry[[]byte](msgin.RetryPolicy{MaxAttempts: 1, DeadLetter: tt.dlq}),
			)
			require.NoError(t, err)

			tt.assert(t, p.Send(t.Context(), msgin.New[[]byte]([]byte("payload"))))
		})
	}
}

// TestProducerDeadLetterDetachedContext is branch 10 (audit D1): the divert must
// run on a ctx detached from the caller's, because the usual reason the loop is
// ending is that ctx was cancelled. If the DLQ send saw the cancelled ctx, a
// real sink would fail and the message would reach neither target nor DLQ.
func TestProducerDeadLetterDetachedContext(t *testing.T) {
	defer goleak.VerifyNone(t)

	transient := errors.New("transient")
	clock := clockwork.NewFakeClock()
	out := newScriptedOutbound(transient)
	dlq := newScriptedOutbound(nil)

	p, err := msgin.NewProducer[[]byte](out,
		msgin.WithProducerClock[[]byte](clock),
		msgin.WithProducerCodec[[]byte](msgin.BytesPayloadCodec{}),
		msgin.WithProducerRetry[[]byte](msgin.RetryPolicy{MaxAttempts: 1, DeadLetter: dlq}),
	)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	sendErr := p.Send(ctx, msgin.New[[]byte]([]byte("payload")))

	assert.ErrorIs(t, sendErr, msgin.ErrDeadLettered)
	require.Equal(t, 1, dlq.attempts(), "the divert must still happen on a cancelled caller ctx")
	assert.NoError(t, dlq.lastCtxErr(), "the DeadLetter sink must NOT observe the caller's cancellation")
}

// TestProducerDeadLetterTimeout is branch 37: the divert's ctx must actually
// CARRY the configured timeout, so a hung sink cannot hold the caller's
// goroutine forever. It is the only bound on a detached ctx.
//
// The timeout is a context deadline, NOT a clockwork wait (see the note on
// deadLetter), so the fake clock cannot drive it. A 1ns timeout reaches the
// expiry arm; the sink WAITS for ctx.Done rather than sampling ctx.Err, which
// makes the arm deterministic even if the deadline has not yet fired when the
// sink is entered.
func TestProducerDeadLetterTimeout(t *testing.T) {
	defer goleak.VerifyNone(t)

	transient := errors.New("transient")
	clock := clockwork.NewFakeClock()
	out := newScriptedOutbound(transient)

	// A hung sink: it never completes on its own, so only the divert's own
	// deadline can release it.
	dlq := outboundFunc(func(ctx context.Context, _ msgin.Message[any]) error {
		<-ctx.Done()
		return ctx.Err()
	})

	p, err := msgin.NewProducer[[]byte](out,
		msgin.WithProducerClock[[]byte](clock),
		msgin.WithProducerDeadLetterTimeout[[]byte](time.Nanosecond),
		msgin.WithProducerRetry[[]byte](msgin.RetryPolicy{MaxAttempts: 1, DeadLetter: dlq}),
	)
	require.NoError(t, err)

	sendErr := p.Send(t.Context(), msgin.New[[]byte]([]byte("payload")))

	assert.ErrorIs(t, sendErr, transient, "the cause must survive a failed divert")
	assert.ErrorIs(t, sendErr, context.DeadlineExceeded,
		"the divert must run under the configured deadLetterTimeout, and its expiry must surface")
	assert.NotErrorIs(t, sendErr, msgin.ErrDeadLettered, "nothing was successfully dead-lettered")
}

// TestProducerRetryContextCancel covers the two cancellation arms. Both drive
// the SAME call (Producer.Send under a cancellable ctx) with different timing, so
// the mandatory table-test rule applies: drive-inputs are fields, every outcome
// assertion lives in a per-case assert closure.
func TestProducerRetryContextCancel(t *testing.T) {
	defer goleak.VerifyNone(t)

	transient := errors.New("transient")

	tests := []struct {
		name string
		// preCancel cancels before Send is ever entered; otherwise the driver
		// waits for the producer to park on the backoff timer and cancels there.
		preCancel bool
		// withDLQ gives the policy a DeadLetter sink, so the divert on the
		// cancellation path is OBSERVABLE. Without one, deadLetter returns the
		// cause unchanged and the divert cannot be told from a bare return.
		withDLQ bool
		assert  func(t *testing.T, out, dlq *scriptedOutbound, err error)
	}{
		{
			name:      "cancel during backoff",
			preCancel: false,
			assert: func(t *testing.T, out, dlq *scriptedOutbound, err error) {
				t.Helper()
				assert.ErrorIs(t, err, context.Canceled)
				assert.ErrorIs(t, err, transient, "the last attempt's error must stay visible")
				assert.Equal(t, 1, out.attempts())
			},
		},
		{
			// THE pin for the cancellation divert. A whole-branch code review
			// proved this behaviour was unverified: replacing the divert with a
			// bare `return errors.Join(err, waitErr)` — i.e. silently DROPPING
			// the message on the common cancellation path, the exact loss the
			// divert exists to prevent — left the entire suite green, because
			// the only cancel case ran with DeadLetter: nil. 100% line coverage,
			// zero behaviour pinned.
			name:      "cancel during backoff still dead-letters",
			preCancel: false,
			withDLQ:   true,
			assert: func(t *testing.T, out, dlq *scriptedOutbound, err error) {
				t.Helper()
				assert.ErrorIs(t, err, context.Canceled)
				assert.ErrorIs(t, err, transient)
				assert.ErrorIs(t, err, msgin.ErrDeadLettered,
					"a cancelled send must report that the message was diverted, not merely that it was cancelled")
				require.Equal(t, 1, dlq.attempts(), "the message must reach the DLQ, not be dropped")
				assert.NoError(t, dlq.lastCtxErr(),
					"the divert must run detached: the sink must not observe the caller's cancellation")
			},
		},
		{
			name:      "a pre-cancelled ctx stops after one attempt",
			preCancel: true,
			assert: func(t *testing.T, out, dlq *scriptedOutbound, err error) {
				t.Helper()
				assert.ErrorIs(t, err, context.Canceled)
				assert.Equal(t, 1, out.attempts(), "an already-cancelled ctx must stop after one attempt")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clock := clockwork.NewFakeClock()
			out := newScriptedOutbound(transient)
			dlq := newScriptedOutbound(nil)

			policy := msgin.RetryPolicy{Backoff: msgin.ExponentialBackoff{Initial: time.Minute}}
			if tt.withDLQ {
				policy.DeadLetter = dlq
			}
			p, err := msgin.NewProducer[[]byte](out,
				msgin.WithProducerClock[[]byte](clock),
				msgin.WithProducerCodec[[]byte](msgin.BytesPayloadCodec{}),
				msgin.WithProducerRetry[[]byte](policy), // MaxAttempts 0 = bounded by budget and ctx
			)
			require.NoError(t, err)

			ctx, cancel := context.WithCancel(t.Context())
			if tt.preCancel {
				cancel()
				tt.assert(t, out, dlq, p.Send(ctx, msgin.New[[]byte]([]byte("payload"))))
				return
			}

			h := runSend(t, p, clock, ctx)
			blockCtx, blockCancel := context.WithTimeout(t.Context(), 2*time.Second)
			defer blockCancel()
			require.NoError(t, clock.BlockUntilContext(blockCtx, 1),
				"producer never parked on the backoff timer")
			cancel()
			tt.assert(t, out, dlq, h.wait(t))
		})
	}
}

// TestProducerHooks is branches 21–23: the loop's observability surface must
// fire for every retry and once for the divert, and a panicking hook must not
// break the send. Both cases drive the same call, so they are one table with
// per-case assert closures (table-test rule).
func TestProducerHooks(t *testing.T) {
	defer goleak.VerifyNone(t)

	transient := errors.New("transient")

	// counters is the only shared mutable state; each case gets its own.
	type counters struct {
		mu            sync.Mutex
		retries, dlqs int
	}

	tests := []struct {
		name   string
		script []error
		policy func(dlq msgin.OutboundAdapter) msgin.RetryPolicy
		hooks  func(c *counters) msgin.Hooks
		// waits are the delays the producer is expected to park for, in order.
		waits  []time.Duration
		assert func(t *testing.T, c *counters, out *scriptedOutbound, logs *bytes.Buffer, err error)
	}{
		{
			name:   "hooks observe retries and the divert",
			script: []error{transient},
			waits:  []time.Duration{time.Second, 2 * time.Second},
			policy: func(dlq msgin.OutboundAdapter) msgin.RetryPolicy {
				return msgin.RetryPolicy{
					MaxAttempts: 3,
					Backoff:     msgin.ExponentialBackoff{Initial: time.Second, Mult: 2},
					DeadLetter:  dlq,
				}
			},
			hooks: func(c *counters) msgin.Hooks {
				return msgin.Hooks{
					OnRetry: func(context.Context, msgin.Message[any], error) {
						c.mu.Lock()
						c.retries++
						c.mu.Unlock()
					},
					OnDeadLetter: func(context.Context, msgin.Message[any], error) {
						c.mu.Lock()
						c.dlqs++
						c.mu.Unlock()
					},
				}
			},
			assert: func(t *testing.T, c *counters, out *scriptedOutbound, logs *bytes.Buffer, err error) {
				t.Helper()
				require.ErrorIs(t, err, msgin.ErrDeadLettered)
				c.mu.Lock()
				defer c.mu.Unlock()
				assert.Equal(t, 2, c.retries, "one OnRetry per wait, not per attempt")
				assert.Equal(t, 1, c.dlqs)
			},
		},
		{
			name:   "a panicking hook is contained and logged",
			script: []error{transient, nil},
			waits:  []time.Duration{time.Second},
			policy: func(msgin.OutboundAdapter) msgin.RetryPolicy {
				return msgin.RetryPolicy{Backoff: msgin.ExponentialBackoff{Initial: time.Second}}
			},
			hooks: func(*counters) msgin.Hooks {
				return msgin.Hooks{
					OnRetry: func(context.Context, msgin.Message[any], error) { panic("hook exploded") },
				}
			},
			assert: func(t *testing.T, _ *counters, out *scriptedOutbound, logs *bytes.Buffer, err error) {
				t.Helper()
				assert.NoError(t, err, "a panicking observability hook must not break the send")
				assert.Equal(t, 2, out.attempts())
				assert.Contains(t, logs.String(), "hook panicked",
					"the recovered panic must be logged, not silently discarded")
				assert.Contains(t, logs.String(), "hook exploded")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var c counters
			var logs bytes.Buffer

			clock := clockwork.NewFakeClock()
			out := newScriptedOutbound(tt.script...)
			dlq := newScriptedOutbound(nil)

			p, err := msgin.NewProducer[[]byte](out,
				msgin.WithProducerClock[[]byte](clock),
				msgin.WithProducerLogger[[]byte](slog.New(slog.NewTextHandler(&logs, nil))),
				msgin.WithProducerRetry[[]byte](tt.policy(dlq)),
				msgin.WithProducerHooks[[]byte](tt.hooks(&c)),
			)
			require.NoError(t, err)

			h := runSend(t, p, clock, t.Context())
			for _, w := range tt.waits {
				h.stepTo(t, w)
			}
			tt.assert(t, &c, out, &logs, h.wait(t))
		})
	}
}

// TestProducerRetryAfter measures the ACTUAL wait, two-phase, so an UNDER-wait
// fails as loudly as an over-wait.
func TestProducerRetryAfter(t *testing.T) {
	defer goleak.VerifyNone(t)

	transient := errors.New("transient")

	// wantWait is a DRIVE-INPUT only — it tells stepTo how far to advance the
	// fake clock. Every outcome assertion lives in the case's assert closure
	// (table-test rule); nothing is asserted in the loop body.
	assertParked := func(want time.Duration) func(*testing.T, *scriptedOutbound, time.Duration, error) {
		return func(t *testing.T, out *scriptedOutbound, elapsed time.Duration, err error) {
			t.Helper()
			require.NoError(t, err)
			assert.Equal(t, 2, out.attempts())
			assert.Equal(t, want, elapsed, "the producer must park for exactly the expected delay")
		}
	}

	tests := []struct {
		name     string
		first    error
		extra    []msgin.ProducerOption[[]byte]
		wantWait time.Duration // drive-input for stepTo, not an expectation field
		assert   func(t *testing.T, out *scriptedOutbound, elapsed time.Duration, err error)
	}{
		{
			name:     "no marker takes the computed backoff",
			first:    transient,
			wantWait: time.Second,
			assert:   assertParked(time.Second),
		},
		{
			name:     "retry-after floors the wait above the computed backoff",
			first:    msgin.RetryAfter(transient, 30*time.Second),
			wantWait: 30 * time.Second,
			assert:   assertParked(30 * time.Second),
		},
		{
			name:     "retry-after cannot shorten the computed backoff",
			first:    msgin.RetryAfter(transient, time.Millisecond),
			wantWait: time.Second, // the computed backoff wins; the server may only lengthen
			assert:   assertParked(time.Second),
		},
		{
			name:     "a zero retry-after cannot shorten it either",
			first:    msgin.RetryAfter(transient, 0),
			wantWait: time.Second,
			assert:   assertParked(time.Second),
		},
		{
			// The 2m wait is exactly the 2m default budget, and would survive
			// only because the budget test is a strict .After. Pin it by DESIGN:
			// raise the budget so this case tests the cap, not the budget.
			name:  "retry-after is clamped by an explicit cap",
			first: msgin.RetryAfter(transient, 10*time.Minute),
			extra: []msgin.ProducerOption[[]byte]{
				msgin.WithProducerRetryAfterCap[[]byte](2 * time.Minute),
				msgin.WithProducerRetryBudget[[]byte](10 * time.Minute),
			},
			wantWait: 2 * time.Minute,
			assert:   assertParked(2 * time.Minute),
		},
		{
			name:     "retry-after is clamped by the 60s default cap",
			first:    msgin.RetryAfter(transient, 10*time.Minute),
			wantWait: 60 * time.Second,
			assert:   assertParked(60 * time.Second),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clock := clockwork.NewFakeClock()
			start := clock.Now()
			out := newScriptedOutbound(tt.first, nil)

			opts := append([]msgin.ProducerOption[[]byte]{
				msgin.WithProducerClock[[]byte](clock),
				msgin.WithProducerRetry[[]byte](msgin.RetryPolicy{
					Backoff: msgin.ExponentialBackoff{Initial: time.Second, Mult: 2},
				}),
			}, tt.extra...)

			p, err := msgin.NewProducer[[]byte](out, opts...)
			require.NoError(t, err)

			h := runSend(t, p, clock, t.Context())
			h.stepTo(t, tt.wantWait)

			tt.assert(t, out, clock.Now().Sub(start), h.wait(t))
		})
	}
}

// TestProducerRetryBudget is branches 19/20 (audit D2): a MaxAttempts == 0
// policy is bounded by the cumulative budget, and the loop stops BEFORE a wait
// that would overrun it rather than truncating one.
func TestProducerRetryBudget(t *testing.T) {
	defer goleak.VerifyNone(t)

	transient := errors.New("transient")

	// waits and withDLQ are DRIVE-INPUTS. Everything else — attempt count, DLQ
	// count, error shape — is asserted inside the case's assert closure.
	tests := []struct {
		name    string
		withDLQ bool
		waits   []time.Duration
		assert  func(t *testing.T, out, dlq *scriptedOutbound, err error)
	}{
		{
			name:    "budget exhaustion with no dead-letter returns the cause alone",
			withDLQ: false,
			waits:   []time.Duration{time.Second, 2 * time.Second},
			assert: func(t *testing.T, out, dlq *scriptedOutbound, err error) {
				t.Helper()
				assert.ErrorIs(t, err, transient)
				assert.ErrorIs(t, err, msgin.ErrRetryBudgetExhausted,
					"a budget stop must be distinguishable from spent attempts")
				assert.NotErrorIs(t, err, msgin.ErrDeadLettered, "nothing was dead-lettered — there is no sink")
				assert.Equal(t, 3, out.attempts())
				assert.Equal(t, 0, dlq.attempts())
			},
		},
		{
			name:    "budget exhaustion with a dead-letter diverts",
			withDLQ: true,
			waits:   []time.Duration{time.Second, 2 * time.Second},
			assert: func(t *testing.T, out, dlq *scriptedOutbound, err error) {
				t.Helper()
				assert.ErrorIs(t, err, transient)
				assert.ErrorIs(t, err, msgin.ErrRetryBudgetExhausted)
				assert.ErrorIs(t, err, msgin.ErrDeadLettered)
				assert.Equal(t, 3, out.attempts())
				assert.Equal(t, 1, dlq.attempts())
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clock := clockwork.NewFakeClock()
			out := newScriptedOutbound(transient)
			dlq := newScriptedOutbound(nil)

			// Budget 6s: waits of 1s and 2s fit (cumulative 3s); the third wait
			// would be 4s, landing at t0+7s, which is after the t0+6s deadline.
			policy := msgin.RetryPolicy{Backoff: msgin.ExponentialBackoff{Initial: time.Second, Mult: 2}}
			if tt.withDLQ {
				policy.DeadLetter = dlq
			}

			p, err := msgin.NewProducer[[]byte](out,
				msgin.WithProducerClock[[]byte](clock),
				msgin.WithProducerRetry[[]byte](policy),
				msgin.WithProducerRetryBudget[[]byte](6*time.Second),
			)
			require.NoError(t, err)

			h := runSend(t, p, clock, t.Context())
			for _, w := range tt.waits {
				h.stepTo(t, w)
			}

			tt.assert(t, out, dlq, h.wait(t))
		})
	}
}

func TestNewProducerRetryValidation(t *testing.T) {
	t.Parallel()

	sink := outboundFunc(func(context.Context, msgin.Message[any]) error { return nil })

	tests := []struct {
		name   string
		opts   []msgin.ProducerOption[[]byte]
		assert func(t *testing.T, p msgin.Producer[[]byte], err error)
	}{
		{
			name: "finite MaxAttempts without a DeadLetter is rejected",
			opts: []msgin.ProducerOption[[]byte]{
				msgin.WithProducerRetry[[]byte](msgin.RetryPolicy{MaxAttempts: 3}),
			},
			assert: func(t *testing.T, p msgin.Producer[[]byte], err error) {
				t.Helper()
				assert.Nil(t, p)
				assert.ErrorIs(t, err, msgin.ErrNoDeadLetter)
			},
		},
		{
			name: "negative MaxAttempts is rejected",
			opts: []msgin.ProducerOption[[]byte]{
				msgin.WithProducerRetry[[]byte](msgin.RetryPolicy{MaxAttempts: -1, DeadLetter: sink}),
			},
			assert: func(t *testing.T, p msgin.Producer[[]byte], err error) {
				t.Helper()
				assert.Nil(t, p)
				assert.ErrorIs(t, err, msgin.ErrInvalidMaxAttempts)
			},
		},
		{
			name: "the RetryPolicy zero value is rejected on a producer",
			opts: []msgin.ProducerOption[[]byte]{
				msgin.WithProducerRetry[[]byte](msgin.RetryPolicy{}),
			},
			assert: func(t *testing.T, p msgin.Producer[[]byte], err error) {
				t.Helper()
				assert.Nil(t, p)
				assert.ErrorIs(t, err, msgin.ErrUnboundedRetry)
			},
		},
		{
			name: "MaxAttempts 0 with a Backoff is accepted",
			opts: []msgin.ProducerOption[[]byte]{
				msgin.WithProducerRetry[[]byte](msgin.RetryPolicy{
					Backoff: msgin.ExponentialBackoff{Initial: time.Second},
				}),
			},
			assert: func(t *testing.T, p msgin.Producer[[]byte], err error) {
				t.Helper()
				require.NoError(t, err)
				assert.NotNil(t, p)
			},
		},
		{
			name: "explicit zero retry-after cap is rejected",
			opts: []msgin.ProducerOption[[]byte]{msgin.WithProducerRetryAfterCap[[]byte](0)},
			assert: func(t *testing.T, p msgin.Producer[[]byte], err error) {
				t.Helper()
				assert.Nil(t, p)
				assert.ErrorIs(t, err, msgin.ErrInvalidRetryAfterCap)
			},
		},
		{
			name: "explicit negative retry-after cap is rejected",
			opts: []msgin.ProducerOption[[]byte]{msgin.WithProducerRetryAfterCap[[]byte](-time.Second)},
			assert: func(t *testing.T, p msgin.Producer[[]byte], err error) {
				t.Helper()
				assert.Nil(t, p)
				assert.ErrorIs(t, err, msgin.ErrInvalidRetryAfterCap)
			},
		},
		{
			name: "explicit zero retry budget is rejected",
			opts: []msgin.ProducerOption[[]byte]{msgin.WithProducerRetryBudget[[]byte](0)},
			assert: func(t *testing.T, p msgin.Producer[[]byte], err error) {
				t.Helper()
				assert.Nil(t, p)
				assert.ErrorIs(t, err, msgin.ErrInvalidRetryBudget)
			},
		},
		{
			name: "explicit negative retry budget is rejected",
			opts: []msgin.ProducerOption[[]byte]{msgin.WithProducerRetryBudget[[]byte](-time.Second)},
			assert: func(t *testing.T, p msgin.Producer[[]byte], err error) {
				t.Helper()
				assert.Nil(t, p)
				assert.ErrorIs(t, err, msgin.ErrInvalidRetryBudget)
			},
		},
		{
			name: "explicit zero dead-letter timeout is rejected",
			opts: []msgin.ProducerOption[[]byte]{msgin.WithProducerDeadLetterTimeout[[]byte](0)},
			assert: func(t *testing.T, p msgin.Producer[[]byte], err error) {
				t.Helper()
				assert.Nil(t, p)
				assert.ErrorIs(t, err, msgin.ErrInvalidDeadLetterTimeout)
			},
		},
		{
			name: "explicit negative dead-letter timeout is rejected",
			opts: []msgin.ProducerOption[[]byte]{msgin.WithProducerDeadLetterTimeout[[]byte](-time.Second)},
			assert: func(t *testing.T, p msgin.Producer[[]byte], err error) {
				t.Helper()
				assert.Nil(t, p)
				assert.ErrorIs(t, err, msgin.ErrInvalidDeadLetterTimeout)
			},
		},
		{
			name: "an explicit positive dead-letter timeout is accepted",
			opts: []msgin.ProducerOption[[]byte]{msgin.WithProducerDeadLetterTimeout[[]byte](5 * time.Second)},
			assert: func(t *testing.T, p msgin.Producer[[]byte], err error) {
				t.Helper()
				require.NoError(t, err)
				assert.NotNil(t, p)
			},
		},
		{
			// A nil logger must NOT install nil and must NOT be an error: the
			// option keeps the discard default, so fireHook's recover arm always
			// has a logger to write to.
			name: "a nil logger keeps the default and constructs",
			opts: []msgin.ProducerOption[[]byte]{msgin.WithProducerLogger[[]byte](nil)},
			assert: func(t *testing.T, p msgin.Producer[[]byte], err error) {
				t.Helper()
				require.NoError(t, err)
				assert.NotNil(t, p)
			},
		},
		{
			name: "a non-nil logger is accepted",
			opts: []msgin.ProducerOption[[]byte]{
				msgin.WithProducerLogger[[]byte](slog.New(slog.NewTextHandler(io.Discard, nil))),
			},
			assert: func(t *testing.T, p msgin.Producer[[]byte], err error) {
				t.Helper()
				require.NoError(t, err)
				assert.NotNil(t, p)
			},
		},
		{
			name: "unset cap and budget take their defaults and construct",
			opts: nil,
			assert: func(t *testing.T, p msgin.Producer[[]byte], err error) {
				t.Helper()
				require.NoError(t, err)
				assert.NotNil(t, p)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p, err := msgin.NewProducer[[]byte](sink, tt.opts...)
			tt.assert(t, p, err)
		})
	}
}

// TestProducerScheduledSendIsNotRetried pins the documented v1 scope: retry
// governs Send only, never SendAfter/SendAt.
func TestProducerScheduledSendIsNotRetried(t *testing.T) {
	defer goleak.VerifyNone(t)

	transient := errors.New("transient")
	sched := &scriptedScheduled{scriptedOutbound: newScriptedOutbound(transient)}

	clock := clockwork.NewFakeClock()
	p, err := msgin.NewProducer[[]byte](sched,
		msgin.WithProducerClock[[]byte](clock),
		msgin.WithProducerRetry[[]byte](msgin.RetryPolicy{
			Backoff: msgin.ExponentialBackoff{Initial: time.Minute},
		}),
	)
	require.NoError(t, err)

	assert.ErrorIs(t, p.SendAfter(t.Context(), msgin.New[[]byte]([]byte("x")), time.Minute), transient)
	assert.Equal(t, 1, sched.scheduledCalls())
}

// scriptedScheduled adds ScheduledSender to scriptedOutbound.
type scriptedScheduled struct {
	*scriptedOutbound
	mu   sync.Mutex
	sent int
}

func (s *scriptedScheduled) SendAfter(ctx context.Context, msg msgin.Message[any], _ time.Duration) error {
	s.mu.Lock()
	s.sent++
	s.mu.Unlock()
	// s has no Send of its own; this is the embedded scriptedOutbound's.
	return s.Send(ctx, msg)
}

func (s *scriptedScheduled) scheduledCalls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sent
}
