package msgin_test

import (
	"context"
	"testing"
	"time"

	"github.com/kartaladev/msgin"
	"github.com/stretchr/testify/assert"
)

// nopSink is a minimal OutboundAdapter for policy tests (no recording needed).
type nopSink struct{}

func (nopSink) Send(context.Context, msgin.Message[any]) error { return nil }

func TestRetryPolicy_Validate(t *testing.T) {
	tests := []struct {
		name   string
		policy msgin.RetryPolicy
		assert func(t *testing.T, err error)
	}{
		{"zero value = retry forever, valid", msgin.RetryPolicy{},
			func(t *testing.T, err error) { assert.NoError(t, err) }},
		{"finite with dead-letter is valid",
			msgin.RetryPolicy{MaxAttempts: 3, DeadLetter: nopSink{}, Backoff: msgin.ExponentialBackoff{Initial: time.Millisecond}},
			func(t *testing.T, err error) { assert.NoError(t, err) }},
		{"finite without dead-letter is rejected",
			msgin.RetryPolicy{MaxAttempts: 3},
			func(t *testing.T, err error) { assert.ErrorIs(t, err, msgin.ErrNoDeadLetter) }},
		{"negative MaxAttempts is rejected",
			msgin.RetryPolicy{MaxAttempts: -1, DeadLetter: nopSink{}},
			func(t *testing.T, err error) { assert.ErrorIs(t, err, msgin.ErrInvalidMaxAttempts) }},
		{"retry-forever ignores absent dead-letter",
			msgin.RetryPolicy{MaxAttempts: 0},
			func(t *testing.T, err error) { assert.NoError(t, err) }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tc.assert(t, tc.policy.Validate())
		})
	}
}
