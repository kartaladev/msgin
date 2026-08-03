package endpoint_test

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kartaladev/msgin"
	"github.com/kartaladev/msgin/channel"
	"github.com/kartaladev/msgin/endpoint"
)

// divertCase is the shared shape of every D-N/D-P case: one consumer, one
// message whose settlement lands on an invalid-message (or dead-letter) divert,
// with the sinks configured per case.
type divertCase struct {
	name string
	// wire drives dispatch's DECODE arm (its `derr != nil` branch, which settles
	// before the handler runs) instead of the permanent-handler arm (its
	// `msgin.IsPermanent(err)` branch): the source emits undecodable []byte.
	//
	// Every reference to a consumer.go arm in this file and in
	// settlement_doubles_test.go is anchored on the branch PREDICATE, never on a
	// line number. The line numbers these comments used to carry (`:688`,
	// `:716`, `:726`) had all drifted — `:688` landed inside handlerContext —
	// and a citation that silently points at unrelated code is worse than none.
	wire bool
	// dlq is the RetryPolicy.DeadLetter sink; nil means no dead-letter sink
	// (and therefore MaxAttempts must stay 0 — RetryPolicy.Validate).
	dlq *recordingSink
	// invalid is WithInvalidMessageSink; nil means unset.
	invalid *recordingSink
	// backoff, when set, makes the D8 send-failure Nack delay observably non-zero.
	backoff msgin.BackoffStrategy
	// handlerErr is what the handler returns on the live-value path.
	handlerErr error
	assert     func(t *testing.T, r divertResult)
}

// divertResult is everything a case can assert on.
type divertResult struct {
	acks, nacks  int
	delays       []time.Duration
	deliveries   int
	dlqSends     int // Send ATTEMPTS on the dead-letter sink (a failing sink still counts)
	invalidSends int
	hooks        *hookRec
	log          string
}

// fixedBackoff is a BackoffStrategy returning a constant, observably non-zero
// delay, so the D8 "non-zero backoff on send failure" arm can be asserted
// without depending on the default (nil Backoff → immediate).
type fixedBackoff struct{ d time.Duration }

func (b fixedBackoff) Delay(int) time.Duration { return b.d }

// divertMaxAttempts is the retry budget runDivert configures whenever a
// dead-letter sink is present. The dead-letter cases below arithmetic on it:
// exactly divertMaxAttempts-1 Nacks are ordinary TRANSIENT ones (attempts 1..n-1),
// so every Nack past that is necessarily a DIVERT-FAILURE Nack — which is what
// separates D8's contract from an ordinary retry.
const divertMaxAttempts = 3

// runDivert drives tc to settlement and returns the observed outcome.
func runDivert(t *testing.T, tc divertCase) divertResult {
	t.Helper()

	buf := &lockedBuffer{}
	st := &settle{}
	hk := newHookRec()
	policy := msgin.RetryPolicy{Backoff: tc.backoff}
	if tc.dlq != nil {
		policy = msgin.RetryPolicy{MaxAttempts: divertMaxAttempts, DeadLetter: tc.dlq, Backoff: tc.backoff}
	}
	opts := []endpoint.ConsumerOption[order]{
		endpoint.WithRetryPolicy[order](policy),
		endpoint.WithHooks[order](hk.hooks()),
		endpoint.WithLogger[order](slog.New(slog.NewTextHandler(buf, nil))),
	}
	if tc.invalid != nil {
		opts = append(opts, endpoint.WithInvalidMessageSink[order](tc.invalid))
	}

	var src any
	rs := &redeliverSource{st: st, id: "m1", max: 41}
	if tc.wire {
		src = &wireRedeliverSource{redeliverSource: rs}
	} else {
		src = &liveRedeliverSource{redeliverSource: rs}
	}
	c, err := endpoint.NewConsumer[order](src, func(context.Context, msgin.Message[order]) error {
		return tc.handlerErr
	}, opts...)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- c.Run(ctx) }()

	require.Eventually(t, func() bool {
		acks, nacks, _ := st.snapshot()
		return acks > 0 || nacks >= 40
	}, 3*time.Second, 2*time.Millisecond, "the message must reach a settlement")
	// Give a forbidden redelivery loop room to show itself before we stop.
	time.Sleep(50 * time.Millisecond)
	cancel()
	require.ErrorIs(t, <-done, context.Canceled)

	acks, nacks, delays := st.snapshot()
	r := divertResult{
		acks: acks, nacks: nacks, delays: delays,
		deliveries: rs.deliveryCount(), hooks: hk, log: buf.String(),
	}
	if tc.dlq != nil {
		r.dlqSends = tc.dlq.attemptCount()
	}
	if tc.invalid != nil {
		r.invalidSends = tc.invalid.attemptCount()
	}
	return r
}

// assertDeadLetterSendFailureNacks is the shared assertion for the NEGATIVE
// cases that pin D-P's scope: the DEAD-LETTER call site keeps ADR 0007 D8's
// Nack-with-backoff, because a retry-exhausted message is transient by
// classification and requeueing it is not a loop.
//
// It is deliberately not satisfiable by a consumer that never dead-letters at
// all. Three claims, each of which an earlier, weaker version of this case
// missed:
//
//  1. dlqSends > 0 — the dead-letter divert was actually REACHED. Without it the
//     case passed on any consumer that merely Nacked transiently forever.
//  2. The last Nack is a DIVERT-FAILURE Nack, not an ordinary transient one.
//     Exactly divertMaxAttempts-1 Nacks precede budget exhaustion, so every Nack
//     past that came from the divert arm — and dlqSends must account for all of
//     them, one Send attempt per Nack.
//  3. Only then is a positive delay evidence of D8/I6's non-zero send-failure
//     backoff rather than of the ordinary retry backoff.
//
// It deliberately asserts NOTHING about the log. Unlike the invalid path, a
// failed dead-letter Send emits no log line at all — divert reports it only
// through OnRetry, and OnRetry carries the CLASSIFICATION cause, never the sink
// error (that is D8's own contract: no terminal event happened, so no terminal
// record is made). The panicking-sink case below is the one exception, and it
// asserts its own log text, because safeSend's recovery logs at ERROR.
func assertDeadLetterSendFailureNacks() func(t *testing.T, r divertResult) {
	return func(t *testing.T, r divertResult) {
		t.Helper()

		assert.Zero(t, r.acks, "a failed dead-letter Send must not Ack")
		assert.Positive(t, r.dlqSends, "the dead-letter divert must be REACHED, not merely retried transiently")
		require.Greater(t, r.nacks, divertMaxAttempts-1,
			"the run must get past the transient budget, or nothing here is about the divert arm")
		assert.Equal(t, r.nacks-(divertMaxAttempts-1), r.dlqSends,
			"every Nack past the retry budget is a DIVERT-FAILURE Nack, one per Send attempt")
		assert.Positive(t, r.hooks.count("retry"), "OnRetry still fires on the dead-letter path")
		assert.Zero(t, r.hooks.count("deadletter"), "no terminal event happened — the sink refused the message")
		require.NotEmpty(t, r.delays)
		assert.Positive(t, r.delays[len(r.delays)-1], "the send-failure Nack carries a non-zero backoff (D8/I6)")
	}
}

// TestDivertInvalidFallback covers Spec 014 §2.1 row 7 — decisions D-N (an
// invalid message falls back to the dead-letter sink when no invalid-message
// sink is configured) and D-P (the WHOLE invalid path is SINGLE-SHOT: a failed
// Send logs, fires OnInvalidMessage and Acks; it never Nacks — whether the sink
// was the D-N fallback or one the caller configured with
// WithInvalidMessageSink). The trailing cases are the negatives that fix D-P's
// scope: the DEAD-LETTER call site is untouched and still Nacks with backoff, on
// a returned error and on a recovered panic alike.
func TestDivertInvalidFallback(t *testing.T) {
	permanent := msgin.Permanent(msgin.ErrNilFunc)

	tests := []divertCase{
		{
			// D-N, the permanent-handler arm: no invalid sink, DeadLetter present
			// (the shape of EVERY finite-retry consumer that has not opted in).
			name:       "no invalid sink falls back to the dead-letter sink",
			dlq:        &recordingSink{},
			handlerErr: permanent,
			assert: func(t *testing.T, r divertResult) {
				assert.Equal(t, 1, r.dlqSends, "the dead-letter sink receives the invalid message")
				assert.Equal(t, 1, r.hooks.count("invalid"), "OnInvalidMessage reports the CLASSIFICATION")
				assert.Zero(t, r.hooks.count("deadletter"), "OnDeadLetter must NOT fire — the budget was not exhausted")
				assert.Zero(t, r.hooks.count("retry"))
				assert.Equal(t, 1, r.acks)
				assert.Zero(t, r.nacks)
				assert.Contains(t, r.log, "no invalid-message sink configured")
				assert.Contains(t, r.log, "dead-letter sink")
				assert.Contains(t, r.log, "m1", "the fallback WARN names the message id")
			},
		},
		{
			// The ADR 0007 D7 discard survives when NEITHER sink is configured.
			// Without this case a later sweep deletes the discard arm as dead code.
			name:       "neither sink configured keeps the ADR 0007 D7 discard",
			handlerErr: permanent,
			assert: func(t *testing.T, r divertResult) {
				assert.Equal(t, 1, r.hooks.count("invalid"))
				assert.Zero(t, r.hooks.count("deadletter"))
				assert.Zero(t, r.hooks.count("retry"))
				assert.Equal(t, 1, r.acks)
				assert.Zero(t, r.nacks)
				assert.Contains(t, r.log, "discarding message; no invalid-message sink configured")
			},
		},
		{
			// The fallback must NOT fire when an invalid sink IS configured.
			name:       "configured invalid sink is unchanged and the dead-letter sink gets nothing",
			dlq:        &recordingSink{},
			invalid:    &recordingSink{},
			handlerErr: permanent,
			assert: func(t *testing.T, r divertResult) {
				assert.Equal(t, 1, r.invalidSends, "the configured invalid sink still receives it")
				assert.Zero(t, r.dlqSends, "the dead-letter sink receives NOTHING — the fallback must not fire")
				assert.Equal(t, 1, r.hooks.count("invalid"))
				assert.Zero(t, r.hooks.count("deadletter"))
				assert.Equal(t, 1, r.acks)
				assert.NotContains(t, r.log, "no invalid-message sink configured")
			},
		},
		{
			// D-N's DECODE arm (dispatch's `derr != nil` branch) — discard → dead-letter.
			name: "decode failure falls back to the dead-letter sink",
			wire: true,
			dlq:  &recordingSink{},
			assert: func(t *testing.T, r divertResult) {
				assert.Equal(t, 1, r.dlqSends, "the decode arm reaches the dead-letter sink too")
				assert.Equal(t, 1, r.hooks.count("invalid"))
				assert.Zero(t, r.hooks.count("deadletter"))
				assert.Zero(t, r.hooks.count("retry"))
				assert.Equal(t, 1, r.acks)
				assert.Zero(t, r.nacks)
				assert.Contains(t, r.log, "no invalid-message sink configured")
			},
		},
		{
			// D-P: the state D-N created and left uncovered. Without the
			// delivery-count and OnRetry assertions this case passes against the
			// very implementation D-P exists to forbid (round 8 measured
			// deliveries=41 nacks=40 OnRetry=40).
			name:       "fallback sink Send FAILS — single shot, no redelivery loop",
			dlq:        &recordingSink{failWith: errors.New("dlq-down")},
			handlerErr: permanent,
			assert: func(t *testing.T, r divertResult) {
				assert.Equal(t, 1, r.acks, "the original delivery is Acked, never Nacked")
				assert.Zero(t, r.nacks)
				assert.Equal(t, 1, r.deliveries, "exactly one delivery — no redelivery loop")
				assert.Equal(t, 1, r.dlqSends, "ONE attempt at the sink — there is no second")
				assert.Equal(t, 1, r.hooks.count("invalid"))
				assert.Zero(t, r.hooks.count("retry"), "OnRetry must not fire — no retry follows")
				assert.Contains(t, r.log, "nil endpoint function", "the WARN names the classification cause")
				assert.Contains(t, r.log, "dlq-down", "the WARN names the sink error")
			},
		},
		{
			// D-P's SCOPE, positively stated: the single shot is a property of the
			// MESSAGE (permanent by classification), not of which sink refused it,
			// so a CONFIGURED invalid-message sink that is down is discarded
			// exactly like the D-N fallback. Without this case the whole D-P
			// contract is pinned only on the unset-sink arm, and an implementation
			// that Nacks whenever WithInvalidMessageSink IS set passes the suite —
			// the redelivery loop returning through the option an operator set
			// precisely because they cared about invalid messages.
			name:       "CONFIGURED invalid sink Send FAILS — single shot too, not just the fallback",
			invalid:    &recordingSink{failWith: errors.New("invalid-sink-down")},
			handlerErr: permanent,
			assert: func(t *testing.T, r divertResult) {
				assert.Equal(t, 1, r.acks, "the original delivery is Acked, never Nacked")
				assert.Zero(t, r.nacks)
				assert.Equal(t, 1, r.deliveries, "exactly one delivery — no redelivery loop")
				assert.Equal(t, 1, r.invalidSends, "ONE attempt at the configured sink — there is no second")
				assert.Equal(t, 1, r.hooks.count("invalid"), "OnInvalidMessage fires exactly once")
				assert.Zero(t, r.hooks.count("retry"), "OnRetry must NOT fire — nothing is retried")
				assert.Zero(t, r.hooks.count("deadletter"))
				assert.Contains(t, r.log, "single-shot", "the WARN says why there is no retry")
				assert.Contains(t, r.log, "nil endpoint function", "the WARN names the classification cause")
				assert.Contains(t, r.log, "invalid-sink-down", "the WARN names the sink error")
				assert.NotContains(t, r.log, "no invalid-message sink configured",
					"the D-N fallback WARN must not fire — a sink IS configured")
			},
		},
		{
			// D-P at the OTHER invalid call site, so single-shot is not
			// implemented at one of the two only.
			name: "decode failure with a failing fallback sink is single shot too",
			wire: true,
			dlq:  &recordingSink{failWith: errors.New("dlq-down")},
			assert: func(t *testing.T, r divertResult) {
				assert.Equal(t, 1, r.acks)
				assert.Zero(t, r.nacks)
				assert.Equal(t, 1, r.deliveries)
				assert.Equal(t, 1, r.dlqSends)
				assert.Equal(t, 1, r.hooks.count("invalid"))
				assert.Zero(t, r.hooks.count("retry"))
				assert.Contains(t, r.log, "dlq-down")
				// loggableCause: the WARN names the CLASSIFICATION (the sentinel),
				// not the codec's free text. encoding/json quotes the byte it
				// choked on ("invalid character '}' ..."), and a caller-supplied
				// PayloadCodec may quote far more — the runtime's own contract for
				// this path is the message id, never the payload.
				assert.Contains(t, r.log, "payload decode failed",
					"the classification cause is still named, as D-P requires")
				assert.NotContains(t, r.log, "invalid character",
					"the codec's free text — which can quote the payload — is NOT logged")
			},
		},
		{
			// The negative that proves D-P's SCOPE: the dead-letter call site
			// (dispatch's finite-exhausted `MaxAttempts` case, which calls divert
			// rather than divertTerminal) keeps ADR 0007 D8's Nack-with-backoff, because a
			// retry-exhausted message is transient by classification. Without this
			// case a later sweep makes the transient path terminal too.
			name:       "dead-letter call site still Nacks with backoff when its sink fails",
			dlq:        &recordingSink{failWith: errors.New("dlq-down")},
			backoff:    fixedBackoff{d: time.Millisecond},
			handlerErr: errors.New("transient boom"),
			assert:     assertDeadLetterSendFailureNacks(),
		},
		{
			// Finding 3: retargeting the safeSend panic test onto the invalid arm
			// left the RETRYABLE half of safeSend's godoc ("divert retries it —
			// OnRetry + Nack with backoff — on the dead-letter path") unpinned on
			// the consumer. A panicking DEAD-LETTER sink must reach the same
			// outcome a returned error does: recovered, Nacked with backoff, never
			// Ack-and-lose.
			name:       "dead-letter call site Nacks with backoff when its sink PANICS",
			dlq:        &recordingSink{panicWith: "dlq-sink boom"},
			backoff:    fixedBackoff{d: time.Millisecond},
			handlerErr: errors.New("transient boom"),
			assert: func(t *testing.T, r divertResult) {
				assertDeadLetterSendFailureNacks()(t, r)
				assert.Contains(t, r.log, "OutboundAdapter.Send panicked",
					"safeSend RECOVERED the panic rather than letting it unwind the worker")
				assert.Contains(t, r.log, "dlq-sink boom", "the recovered panic value is logged")
				assert.Contains(t, r.log, "id=m1", "the ERROR log names the message id, never the payload")
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tc.assert(t, runDivert(t, tc))
		})
	}
}

// TestDivertFallbackWarnIsDeduplicated proves the fallback WARN is emitted once
// per consumer, not once per message: "no invalid-message sink is configured"
// is constant for the consumer's lifetime, so a poison storm would otherwise
// flood the log with an identical line (mirrors governorPanic's dedup).
func TestDivertFallbackWarnIsDeduplicated(t *testing.T) {
	buf := &lockedBuffer{}
	st := &settle{}
	hk := newHookRec()
	dlq := &recordingSink{}
	src := &scriptedSource{deliveries: []msgin.Delivery{
		newSettleDelivery(order{ID: "a"}, "a", st),
		newSettleDelivery(order{ID: "b"}, "b", st),
	}}

	c, err := endpoint.NewConsumer[order](src, func(context.Context, msgin.Message[order]) error {
		return msgin.Permanent(msgin.ErrNilFunc)
	},
		endpoint.WithRetryPolicy[order](msgin.RetryPolicy{MaxAttempts: 3, DeadLetter: dlq}),
		endpoint.WithHooks[order](hk.hooks()),
		endpoint.WithLogger[order](slog.New(slog.NewTextHandler(buf, nil))),
	)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- c.Run(ctx) }()
	require.Eventually(t, func() bool { acks, _, _ := st.snapshot(); return acks >= 2 },
		3*time.Second, 2*time.Millisecond, "both messages must settle")
	cancel()
	require.ErrorIs(t, <-done, context.Canceled)

	assert.Equal(t, 2, dlq.count(), "both messages reach the fallback sink")
	assert.Equal(t, 2, hk.count("invalid"), "the per-message terminal record still fires for each")
	assert.Equal(t, 1, strings.Count(buf.String(), "no invalid-message sink configured"),
		"the fallback WARN is emitted exactly once per consumer")
}

// TestDivertTerminalShutdownCancellationNacks pins the ONE arm of the invalid
// path that is not single-shot: a Send that fails only because Run's settle
// context was already cancelled by the shutdown deadline (C1).
//
// The regression it guards against is a silent message LOSS, not a policy
// nicety. A cancelled settleCtx makes even a perfectly healthy sink's Send
// return an error; routed into D-P's discard arm that becomes WARN →
// OnInvalidMessage → Ack, and because a Delivery.Ack may ignore its context
// (adapter/memory's does: `func(context.Context) error { return nil }`), the Ack
// SUCCEEDS and the tracker evicts. The message is gone with the sink up. So the
// arm must Nack with requeue instead and fire no terminal hook.
//
// It does not weaken D-P: ctx.Err() is nil in normal operation, which
// TestDivertInvalidFallback's send-failure cases assert directly.
func TestDivertTerminalShutdownCancellationNacks(t *testing.T) {
	buf := &lockedBuffer{}
	st := &settle{}
	hk := newHookRec()
	sink := newCtxBlockingSink()

	src := &scriptedSource{deliveries: []msgin.Delivery{newSettleDelivery(order{ID: "o"}, "m1", st)}}
	c, err := endpoint.NewConsumer[order](src, func(context.Context, msgin.Message[order]) error {
		return msgin.Permanent(msgin.ErrNilFunc)
	},
		endpoint.WithInvalidMessageSink[order](sink),
		// Short, so the deadline that cancels settleCtx arrives promptly once the
		// worker is parked in the sink. This is the ONLY way to reach the arm.
		endpoint.WithShutdownTimeout[order](20*time.Millisecond),
		endpoint.WithHooks[order](hk.hooks()),
		endpoint.WithLogger[order](slog.New(slog.NewTextHandler(buf, nil))),
	)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- c.Run(ctx) }()

	select {
	case <-sink.entered:
	case <-time.After(3 * time.Second):
		t.Fatal("the invalid divert never reached the sink")
	}
	cancel() // begins the drain; 20ms later drainWorkers cancels settleCtx
	require.ErrorIs(t, <-done, context.Canceled)

	acks, nacks, _ := st.snapshot()
	assert.Zero(t, acks, "a divert aborted by shutdown must NOT Ack — the sink was never proven down")
	assert.Equal(t, 1, nacks, "the message is Nacked so the source redelivers it on the next run")
	assert.Equal(t, []bool{true}, st.requeues(), "the Nack REQUEUES; a non-requeue Nack would drop it just as surely")
	assert.Equal(t, 1, sink.sendCount(), "one Send attempt, aborted mid-flight")
	assert.Zero(t, hk.count("invalid"), "OnInvalidMessage must NOT fire — no terminal event happened")
	assert.Zero(t, hk.count("deadletter"))
	assert.Contains(t, buf.String(), "aborted by shutdown", "the WARN says the divert was cut short, not refused")
	assert.NotContains(t, buf.String(), "discarding invalid message",
		"the single-shot discard WARN must not claim a refusal the sink never made")
}

// TestDivertInvalidFallbackUnderNativeDeadLetter pins the DELIBERATE absence of
// the !NativeDeadLetter() guard on invalidTarget.
//
// dispatch's transient dead-letter arm DOES apply that guard, so the omission
// here reads like an oversight and invites a sweep to "restore" it. It must not
// be: the discriminator is Nack-vs-Ack. The transient path NACKS, so a native
// DLQ captures the message itself and a runtime write would double it. The
// invalid path ACKS on every arm, so a native DLQ never sees this message at
// all — the fallback write is its ONLY capture, and adding the guard would
// return a nil sink and silently restore ADR 0007 D7's discard.
func TestDivertInvalidFallbackUnderNativeDeadLetter(t *testing.T) {
	st := &settle{}
	hk := newHookRec()
	dlq := &recordingSink{}
	src := &nativeDLQSource{st: st, id: "m1", payload: order{ID: "a"}}

	c, err := endpoint.NewConsumer[order](src, func(context.Context, msgin.Message[order]) error {
		return msgin.Permanent(msgin.ErrNilFunc)
	},
		endpoint.WithRetryPolicy[order](msgin.RetryPolicy{MaxAttempts: 3, DeadLetter: dlq}),
		endpoint.WithHooks[order](hk.hooks()),
	)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- c.Run(ctx) }()
	require.Eventually(t, func() bool { acks, _, _ := st.snapshot(); return acks >= 1 },
		3*time.Second, 2*time.Millisecond, "the message must settle")
	cancel()
	require.ErrorIs(t, <-done, context.Canceled)

	acks, nacks, _ := st.snapshot()
	assert.Equal(t, 1, dlq.count(),
		"the D-N fallback still writes under a native DLQ — the source never sees a Nack for this message, so its own pipeline cannot capture it")
	assert.Equal(t, 1, hk.count("invalid"))
	assert.Zero(t, hk.count("deadletter"), "the budget was not exhausted — this is the invalid path")
	assert.Zero(t, hk.count("retry"))
	assert.Equal(t, 1, acks)
	assert.Zero(t, nacks)
}

// TestProducerPermanentFaultIsNotDeadLettered covers Spec 014 §2.1 row 8: D-M
// reaching the PRODUCER. endpoint/producer.go returns on IsPermanent BEFORE
// dead-lettering, so a mis-wired step downstream of a producer over a
// DirectChannel stops reaching the producer's dead-letter sink and
// errors.Is(err, ErrDeadLettered) flips true → false. This is a DOCUMENTED
// LOSS, not a regression: Send is synchronous and hands the error to the
// caller, so there is no message to lose — which is why D-N's fallback does not
// extend to the producer.
func TestProducerPermanentFaultIsNotDeadLettered(t *testing.T) {
	dc := channel.NewDirectChannel()
	sub, err := dc.Subscribe(msgin.HandlerFunc(func(context.Context, msgin.Message[any]) error {
		return msgin.Permanent(msgin.ErrNilFunc)
	}))
	require.NoError(t, err)
	t.Cleanup(sub.Cancel)

	dlq := &recordingSink{}
	hk := newHookRec()
	p, err := endpoint.NewProducer[order](dc,
		endpoint.WithProducerRetry[order](msgin.RetryPolicy{MaxAttempts: 3, DeadLetter: dlq}),
		endpoint.WithProducerHooks[order](hk.hooks()),
	)
	require.NoError(t, err)

	sendErr := p.Send(t.Context(), msgin.New[order](order{ID: "a"}))

	require.Error(t, sendErr)
	assert.ErrorIs(t, sendErr, msgin.ErrNilFunc, "the cause still reaches the caller")
	assert.False(t, errors.Is(sendErr, msgin.ErrDeadLettered),
		"the exported contract flips: a permanent fault is NOT dead-lettered (Spec 014 §2.1 row 8)")
	assert.Zero(t, dlq.count(), "the producer's dead-letter sink receives nothing")
	assert.Zero(t, hk.count("deadletter"))
	assert.Zero(t, hk.count("retry"))
}
