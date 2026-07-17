package msgin_test

import (
	"context"
	"testing"
	"time"

	"github.com/kartaladev/msgin"
	"github.com/kartaladev/msgin/adapter/memory"
	"github.com/stretchr/testify/assert"
)

// stubRateLimiter is a no-op RateLimiter test double, used only to exercise
// the WithRateLimit non-nil assignment branch at construction time (runtime
// wiring lands in a later task).
type stubRateLimiter struct{}

func (stubRateLimiter) Wait(context.Context) error { return nil }

// stubCircuitBreaker is a no-op CircuitBreaker test double, used only to
// exercise the WithCircuitBreaker non-nil assignment branch at construction
// time (runtime wiring lands in a later task).
type stubCircuitBreaker struct{}

func (stubCircuitBreaker) Allow() bool               { return true }
func (stubCircuitBreaker) Record(bool)               {}
func (stubCircuitBreaker) HalfOpen() <-chan struct{} { return nil }

func TestNewConsumer_FlowControlValidation(t *testing.T) {
	h := func(context.Context, msgin.Message[order]) error { return nil }

	tests := []struct {
		name   string
		opts   []msgin.ConsumerOption[order]
		assert func(t *testing.T, err error)
	}{
		{"max-in-flight below 1 is rejected",
			[]msgin.ConsumerOption[order]{msgin.WithMaxInFlight[order](0)},
			func(t *testing.T, err error) { assert.ErrorIs(t, err, msgin.ErrInvalidMaxInFlight) }},
		{"negative max-in-flight is rejected",
			[]msgin.ConsumerOption[order]{msgin.WithMaxInFlight[order](-4)},
			func(t *testing.T, err error) { assert.ErrorIs(t, err, msgin.ErrInvalidMaxInFlight) }},
		{"all flow-control options construct cleanly",
			[]msgin.ConsumerOption[order]{
				msgin.WithMaxInFlight[order](8),
				msgin.WithRateLimit[order](nil),      // nil is a no-op, must not panic later
				msgin.WithCircuitBreaker[order](nil), // nil is a no-op
				msgin.WithHandlerTimeout[order](time.Second),
				msgin.WithOverflow[order](msgin.OverflowDropNewest),
			},
			func(t *testing.T, err error) { assert.NoError(t, err) }},
		{"non-nil rate limiter and circuit breaker are accepted",
			[]msgin.ConsumerOption[order]{
				msgin.WithRateLimit[order](stubRateLimiter{}),
				msgin.WithCircuitBreaker[order](stubCircuitBreaker{}),
			},
			func(t *testing.T, err error) { assert.NoError(t, err) }},
		{"zero attempt TTL is rejected",
			[]msgin.ConsumerOption[order]{msgin.WithAttemptTTL[order](0)},
			func(t *testing.T, err error) { assert.ErrorIs(t, err, msgin.ErrInvalidAttemptTTL) }},
		{"negative attempt TTL is rejected",
			[]msgin.ConsumerOption[order]{msgin.WithAttemptTTL[order](-time.Second)},
			func(t *testing.T, err error) { assert.ErrorIs(t, err, msgin.ErrInvalidAttemptTTL) }},
		{"positive attempt TTL is accepted",
			[]msgin.ConsumerOption[order]{msgin.WithAttemptTTL[order](time.Minute)},
			func(t *testing.T, err error) { assert.NoError(t, err) }},
		{"negative max payload bytes disables the cap (accepted)",
			[]msgin.ConsumerOption[order]{msgin.WithMaxPayloadBytes[order](-1)},
			func(t *testing.T, err error) { assert.NoError(t, err) }},
		{"zero poll interval is rejected",
			[]msgin.ConsumerOption[order]{msgin.WithPollInterval[order](0)},
			func(t *testing.T, err error) { assert.ErrorIs(t, err, msgin.ErrInvalidPollInterval) }},
		{"negative poll interval is rejected",
			[]msgin.ConsumerOption[order]{msgin.WithPollInterval[order](-time.Second)},
			func(t *testing.T, err error) { assert.ErrorIs(t, err, msgin.ErrInvalidPollInterval) }},
		{"poll max batch below 1 is rejected",
			[]msgin.ConsumerOption[order]{msgin.WithPollMaxBatch[order](0)},
			func(t *testing.T, err error) { assert.ErrorIs(t, err, msgin.ErrInvalidPollMaxBatch) }},
		{"negative poll max batch is rejected",
			[]msgin.ConsumerOption[order]{msgin.WithPollMaxBatch[order](-1)},
			func(t *testing.T, err error) { assert.ErrorIs(t, err, msgin.ErrInvalidPollMaxBatch) }},
		{"valid poll options construct cleanly over a streaming source (ignored, no behavior change)",
			[]msgin.ConsumerOption[order]{
				msgin.WithPollInterval[order](5 * time.Second),
				msgin.WithPollMaxBatch[order](50),
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

func TestOverflowPolicy_String(t *testing.T) {
	tests := []struct {
		name   string
		policy msgin.OverflowPolicy
		assert func(t *testing.T, got string)
	}{
		{"block",
			msgin.OverflowBlock,
			func(t *testing.T, got string) { assert.Equal(t, "OverflowBlock", got) }},
		{"drop newest",
			msgin.OverflowDropNewest,
			func(t *testing.T, got string) { assert.Equal(t, "OverflowDropNewest", got) }},
		{"drop oldest",
			msgin.OverflowDropOldest,
			func(t *testing.T, got string) { assert.Equal(t, "OverflowDropOldest", got) }},
		{"reject",
			msgin.OverflowReject,
			func(t *testing.T, got string) { assert.Equal(t, "OverflowReject", got) }},
		{"unknown renders as block",
			msgin.OverflowPolicy(99),
			func(t *testing.T, got string) { assert.Equal(t, "OverflowBlock", got) }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tc.assert(t, tc.policy.String())
		})
	}
}
