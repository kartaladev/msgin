package msgin_test

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kartaladev/msgin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestConsumer_GovernorPanic_RateLimiterWait_FailsOpenAndLogsOnce proves the
// safeLimiterWait branch (ADR 0009 D1): a panicking RateLimiter.Wait must not
// crash the consumer or block ingress — it fails open (proceed as if
// unpaced), the message is still processed and Acked, and the panic is
// surfaced as exactly one ERROR log line naming the offending method.
func TestConsumer_GovernorPanic_RateLimiterWait_FailsOpenAndLogsOnce(t *testing.T) {
	buf := &lockedBuffer{}
	st := &settle{}
	src := &scriptedSource{deliveries: []msgin.Delivery{
		newSettleDelivery(order{ID: "a"}, "a", st),
	}}
	var handled atomic.Int64
	h := func(context.Context, msgin.Message[order]) error { handled.Add(1); return nil }

	c, err := msgin.NewConsumer[order](src, h,
		msgin.WithRateLimit[order](panicRateLimiter{}),
		msgin.WithLogger[order](slog.New(slog.NewTextHandler(buf, nil))))
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- c.Run(ctx) }()

	require.Eventually(t, func() bool { acks, _, _ := st.snapshot(); return acks >= 1 }, time.Second, 5*time.Millisecond,
		"a panicking limiter must fail open, not block the message forever")
	cancel()
	assert.ErrorIs(t, <-done, context.Canceled)

	assert.Equal(t, int64(1), handled.Load(), "panicking limiter fails open: the handler still runs")
	logged := buf.String()
	assert.Contains(t, logged, "RateLimiter.Wait", "the ERROR log names the panicking method")
	assert.Equal(t, 1, strings.Count(logged, "resilience governor panicked"),
		"the panic is logged exactly once (dedup gate)")
}

// TestConsumer_GovernorPanic_CircuitBreakerAllow_FailsOpen proves the safeAllow
// branch at the ingress admit (ADR 0009 D1): a panicking CircuitBreaker.Allow
// degrades to the no-breaker baseline (treat as not-open → admit) rather than
// crashing the process or wedging ingress.
func TestConsumer_GovernorPanic_CircuitBreakerAllow_FailsOpen(t *testing.T) {
	buf := &lockedBuffer{}
	st := &settle{}
	src := &scriptedSource{deliveries: []msgin.Delivery{
		newSettleDelivery(order{ID: "a"}, "a", st),
	}}
	var handled atomic.Int64
	h := func(context.Context, msgin.Message[order]) error { handled.Add(1); return nil }

	c, err := msgin.NewConsumer[order](src, h,
		msgin.WithCircuitBreaker[order](panicAllowBreaker{}),
		msgin.WithLogger[order](slog.New(slog.NewTextHandler(buf, nil))))
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- c.Run(ctx) }()

	require.Eventually(t, func() bool { acks, _, _ := st.snapshot(); return acks >= 1 }, time.Second, 5*time.Millisecond,
		"a panicking Allow must fail open, not wedge ingress")
	cancel()
	assert.ErrorIs(t, <-done, context.Canceled)

	assert.Equal(t, int64(1), handled.Load(), "panicking Allow fails open: the handler still runs")
	logged := buf.String()
	assert.Contains(t, logged, "CircuitBreaker.Allow", "the ERROR log names the panicking method")
	assert.Equal(t, 1, strings.Count(logged, "resilience governor panicked"),
		"the panic is logged exactly once even though Allow is consulted at both the ingress and dispatch gate")
}

// TestConsumer_GovernorPanic_ProbeGateTryProbe_FailsOpen proves the
// safeTryProbe branch at the dispatch gate (ADR 0009 D1, D2): Allow is benign
// (so the message clears the ingress admit and reaches the dispatch gate),
// but TryProbe panics — the dispatch gate must fail open (admit) rather than
// permanently Nack the message.
func TestConsumer_GovernorPanic_ProbeGateTryProbe_FailsOpen(t *testing.T) {
	buf := &lockedBuffer{}
	st := &settle{}
	src := &scriptedSource{deliveries: []msgin.Delivery{
		newSettleDelivery(order{ID: "a"}, "a", st),
	}}
	var handled atomic.Int64
	h := func(context.Context, msgin.Message[order]) error { handled.Add(1); return nil }

	c, err := msgin.NewConsumer[order](src, h,
		msgin.WithCircuitBreaker[order](panicProbeGateBreaker{}),
		msgin.WithLogger[order](slog.New(slog.NewTextHandler(buf, nil))))
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- c.Run(ctx) }()

	require.Eventually(t, func() bool { acks, _, _ := st.snapshot(); return acks >= 1 }, time.Second, 5*time.Millisecond,
		"a panicking TryProbe must fail open at the dispatch gate")
	cancel()
	assert.ErrorIs(t, <-done, context.Canceled)

	assert.Equal(t, int64(1), handled.Load(), "panicking TryProbe fails open: the handler still runs")
	logged := buf.String()
	assert.Contains(t, logged, "ProbeGate.TryProbe", "the ERROR log names the panicking method")
	assert.Equal(t, 1, strings.Count(logged, "resilience governor panicked"),
		"the panic is logged exactly once (dedup gate)")
}

// TestConsumer_GovernorPanic_CircuitBreakerRecord_SwallowedAfterSettle proves
// the safeRecord branch (ADR 0009 D1): Allow is benign so the message is
// dispatched and settled normally; Record (called AFTER the settlement
// already happened) panics and must be swallowed without affecting the
// already-completed Ack.
func TestConsumer_GovernorPanic_CircuitBreakerRecord_SwallowedAfterSettle(t *testing.T) {
	buf := &lockedBuffer{}
	st := &settle{}
	src := &scriptedSource{deliveries: []msgin.Delivery{
		newSettleDelivery(order{ID: "a"}, "a", st),
	}}
	var handled atomic.Int64
	h := func(context.Context, msgin.Message[order]) error { handled.Add(1); return nil }

	c, err := msgin.NewConsumer[order](src, h,
		msgin.WithCircuitBreaker[order](panicRecordBreaker{}),
		msgin.WithLogger[order](slog.New(slog.NewTextHandler(buf, nil))))
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- c.Run(ctx) }()

	require.Eventually(t, func() bool { acks, _, _ := st.snapshot(); return acks >= 1 }, time.Second, 5*time.Millisecond,
		"a panicking Record must not prevent (or unwind) the already-completed settlement")
	cancel()
	assert.ErrorIs(t, <-done, context.Canceled)

	assert.Equal(t, int64(1), handled.Load())
	logged := buf.String()
	assert.Contains(t, logged, "CircuitBreaker.Record", "the ERROR log names the panicking method")
	assert.Equal(t, 1, strings.Count(logged, "resilience governor panicked"),
		"the panic is logged exactly once (dedup gate)")
}

// TestConsumer_GovernorPanic_CircuitBreakerHalfOpen_NoParkFailsOpen is the
// mandatory no-park landmine test (ADR 0009 D1): the scripted breaker denies
// the first ingress Allow (forcing admitBreaker to subscribe via HalfOpen),
// and HalfOpen panics. admitBreaker must read the recovered (nil, false) as
// "breaker unusable" and fail open WITHOUT parking on the nil channel — if it
// wrongly parked, Run would hang forever and this test would time out instead
// of observing the Ack. The dispatch gate then consumes the second scripted
// Allow value (true, benign) so the message is dispatched normally.
func TestConsumer_GovernorPanic_CircuitBreakerHalfOpen_NoParkFailsOpen(t *testing.T) {
	buf := &lockedBuffer{}
	st := &settle{}
	src := &scriptedSource{deliveries: []msgin.Delivery{
		newSettleDelivery(order{ID: "a"}, "a", st),
	}}
	var handled atomic.Int64
	h := func(context.Context, msgin.Message[order]) error { handled.Add(1); return nil }

	b := newPanicHalfOpenBreaker(false, true)
	c, err := msgin.NewConsumer[order](src, h,
		msgin.WithCircuitBreaker[order](b),
		msgin.WithLogger[order](slog.New(slog.NewTextHandler(buf, nil))))
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- c.Run(ctx) }()

	// A wrong implementation that parks on the nil channel returned by a
	// panicking HalfOpen would hang here until the Eventually timeout fires
	// and the test fails — that failure mode IS the deadlock assertion.
	require.Eventually(t, func() bool { acks, _, _ := st.snapshot(); return acks >= 1 }, time.Second, 5*time.Millisecond,
		"message must be processed, proving admitBreaker fails open without parking on a nil channel")
	cancel()
	assert.ErrorIs(t, <-done, context.Canceled)

	assert.Equal(t, int64(1), handled.Load())
	logged := buf.String()
	assert.Contains(t, logged, "CircuitBreaker.HalfOpen", "the ERROR log names the panicking method")
}

// TestConsumer_GovernorPanic_DedupAcrossManyMessages proves governorPanic's
// per-method dedup gate (ADR 0009 D1): a PERSISTENTLY panicking RateLimiter
// hit once per admitted message must still log exactly one ERROR line for
// that method — not one per message — so a deterministic plug-in bug cannot
// flood the log. Because the runtime fails open, every message is still
// processed (no flood of dropped work either).
func TestConsumer_GovernorPanic_DedupAcrossManyMessages(t *testing.T) {
	const n = 5
	buf := &lockedBuffer{}
	st := &settle{}
	deliveries := make([]msgin.Delivery, n)
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("m%d", i)
		deliveries[i] = newSettleDelivery(order{ID: id}, id, st)
	}
	src := &scriptedSource{deliveries: deliveries}
	h := func(context.Context, msgin.Message[order]) error { return nil }

	c, err := msgin.NewConsumer[order](src, h,
		msgin.WithRateLimit[order](panicRateLimiter{}),
		msgin.WithLogger[order](slog.New(slog.NewTextHandler(buf, nil))))
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- c.Run(ctx) }()

	require.Eventually(t, func() bool { acks, _, _ := st.snapshot(); return acks >= n }, 2*time.Second, 5*time.Millisecond,
		"every message must still be processed under fail-open — no flood of dropped work either")
	cancel()
	assert.ErrorIs(t, <-done, context.Canceled)

	logged := buf.String()
	assert.Equal(t, 1, strings.Count(logged, "resilience governor panicked"),
		"a deterministic per-message panic must log exactly once (per-method dedup), not once per message")
	assert.Contains(t, logged, "RateLimiter.Wait")
}
