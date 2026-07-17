package msgin_test

import (
	"context"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jonboulle/clockwork"
	"github.com/kartaladev/msgin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestConsumer_ProbeGate_NonProbeGateBreakerFallsBackToAllowAtDispatch proves
// the D2 runtime-wiring fallback (ADR 0009 D2, dispatchAllowed): a
// CircuitBreaker that does NOT implement the optional ProbeGate capability
// must have the dispatch gate keep gating on Allow — normal dispatch/Nack
// behavior is unchanged from the pre-D2 runtime. scriptedBreaker implements
// only Allow/Record/HalfOpen (no TryProbe), so any admit/dispatch decision
// here is necessarily routed through Allow.
func TestConsumer_ProbeGate_NonProbeGateBreakerFallsBackToAllowAtDispatch(t *testing.T) {
	tests := []struct {
		name   string
		allows []bool
		assert func(t *testing.T, handled *atomic.Int64, st *settle, b *scriptedBreaker)
	}{
		{"dispatch gate denies (Allow=false at process) blocks without dispatch",
			[]bool{true, false},
			func(t *testing.T, handled *atomic.Int64, st *settle, b *scriptedBreaker) {
				_, nacks, _ := st.snapshot()
				assert.GreaterOrEqual(t, nacks, 1, "dispatch-gated message is Nacked")
				assert.Equal(t, int64(0), handled.Load(), "the Allow-fallback dispatch gate must block on Allow=false")
				assert.Empty(t, b.recorded(), "a gated (never-dispatched) message must not record a breaker outcome")
			}},
		{"dispatch gate admits (Allow=true at process) dispatches and Acks",
			[]bool{true, true},
			func(t *testing.T, handled *atomic.Int64, st *settle, b *scriptedBreaker) {
				acks, _, _ := st.snapshot()
				assert.Equal(t, 1, acks, "admitted message is Acked")
				assert.Equal(t, int64(1), handled.Load())
				assert.Equal(t, []bool{true}, b.recorded(), "a healthy dispatch records success via Allow-fallback Record")
			}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			b := newScriptedBreaker(tc.allows...)
			st := &settle{}
			src := &scriptedSource{deliveries: []msgin.Delivery{
				newSettleDelivery(order{ID: "x"}, "x", st),
			}}
			var handled atomic.Int64
			h := func(context.Context, msgin.Message[order]) error { handled.Add(1); return nil }

			c, err := msgin.NewConsumer[order](src, h,
				msgin.WithConsumerClock[order](clockwork.NewFakeClock()),
				msgin.WithCircuitBreaker[order](b))
			require.NoError(t, err)

			ctx, cancel := context.WithCancel(t.Context())
			done := make(chan error, 1)
			go func() { done <- c.Run(ctx) }()

			require.Eventually(t, func() bool {
				acks, nacks, _ := st.snapshot()
				return acks >= 1 || nacks >= 1
			}, time.Second, 5*time.Millisecond)

			cancel()
			assert.ErrorIs(t, <-done, context.Canceled)
			tc.assert(t, &handled, st, b)
		})
	}
}

// TestConsumer_ProbeGate_GobreakerCliffWarn proves the one-time gobreaker-
// cliff WARN (ADR 0009 D2): Run logs a warning, once, at the very top of the
// call — strictly before it ever invokes the source's Stream — when
// WithConcurrency(N>1) is combined with a non-nil breaker that does NOT
// implement ProbeGate. It must NOT fire for the default NewCircuitBreaker
// (which implements ProbeGate) under N>1, nor for a non-ProbeGate breaker at
// N=1. signalingSource's ready channel gives a happens-before guarantee that
// Run's synchronous pre-loop WARN check has already executed by the time the
// test inspects the log, without needing an actual message to flow.
func TestConsumer_ProbeGate_GobreakerCliffWarn(t *testing.T) {
	const warnSubstr = "does not implement ProbeGate"
	h := func(context.Context, msgin.Message[order]) error { return nil }

	tests := []struct {
		name   string
		opts   func(logger *slog.Logger) []msgin.ConsumerOption[order]
		assert func(t *testing.T, logged string)
	}{
		{"N>1 + non-ProbeGate breaker warns",
			func(logger *slog.Logger) []msgin.ConsumerOption[order] {
				return []msgin.ConsumerOption[order]{
					msgin.WithConcurrency[order](2),
					msgin.WithCircuitBreaker[order](newScriptedBreaker(true)),
					msgin.WithLogger[order](logger),
				}
			},
			func(t *testing.T, logged string) {
				assert.Contains(t, logged, warnSubstr, "N>1 with a non-ProbeGate breaker must warn once")
			}},
		{"N>1 + default breaker (implements ProbeGate) does not warn",
			func(logger *slog.Logger) []msgin.ConsumerOption[order] {
				return []msgin.ConsumerOption[order]{
					msgin.WithConcurrency[order](2),
					msgin.WithCircuitBreaker[order](msgin.NewCircuitBreaker()),
					msgin.WithLogger[order](logger),
				}
			},
			func(t *testing.T, logged string) {
				assert.NotContains(t, logged, warnSubstr, "the shipped default breaker implements ProbeGate; N>1 must not warn")
			}},
		{"N=1 + non-ProbeGate breaker does not warn",
			func(logger *slog.Logger) []msgin.ConsumerOption[order] {
				return []msgin.ConsumerOption[order]{
					msgin.WithCircuitBreaker[order](newScriptedBreaker(true)),
					msgin.WithLogger[order](logger),
				}
			},
			func(t *testing.T, logged string) {
				assert.NotContains(t, logged, warnSubstr, "at N=1 a single worker is already single-probe by construction; must not warn")
			}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			buf := &lockedBuffer{}
			src := newSignalingSource()
			c, err := msgin.NewConsumer[order](src, h, tc.opts(slog.New(slog.NewTextHandler(buf, nil)))...)
			require.NoError(t, err)

			ctx, cancel := context.WithCancel(t.Context())
			done := make(chan error, 1)
			go func() { done <- c.Run(ctx) }()

			<-src.ready // happens-before: Run's synchronous WARN check already ran
			cancel()
			assert.ErrorIs(t, <-done, context.Canceled)

			tc.assert(t, buf.String())
		})
	}
}
