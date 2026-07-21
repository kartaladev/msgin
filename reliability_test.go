package msgin_test

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/kartaladev/msgin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPermanent_WrapAndUnwrap(t *testing.T) {
	base := errors.New("boom")

	tests := []struct {
		name   string
		in     error
		assert func(t *testing.T, got error)
	}{
		{"nil stays nil", nil, func(t *testing.T, got error) {
			assert.NoError(t, got)
		}},
		{"wraps and unwraps to base", base, func(t *testing.T, got error) {
			require.Error(t, got)
			assert.ErrorIs(t, got, base, "Permanent must remain Is-comparable to its cause")
		}},
		{"survives an outer wrap", fmt.Errorf("ctx: %w", base), func(t *testing.T, got error) {
			assert.ErrorIs(t, got, base)
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tc.assert(t, msgin.Permanent(tc.in))
		})
	}
}

func TestRetryAfter(t *testing.T) {
	t.Parallel()

	cause := errors.New("boom")

	tests := []struct {
		name   string
		err    error
		delay  time.Duration
		assert func(t *testing.T, got error)
	}{
		{
			name:  "nil error stays nil",
			err:   nil,
			delay: 5 * time.Second,
			assert: func(t *testing.T, got error) {
				t.Helper()
				require.NoError(t, got)
			},
		},
		{
			name:  "wraps transparently for errors.Is",
			err:   cause,
			delay: 5 * time.Second,
			assert: func(t *testing.T, got error) {
				t.Helper()
				require.Error(t, got)
				assert.ErrorIs(t, got, cause)
				assert.Contains(t, got.Error(), "boom")
			},
		},
		{
			name:  "negative delay is normalized, still wraps",
			err:   cause,
			delay: -1 * time.Second,
			assert: func(t *testing.T, got error) {
				t.Helper()
				require.Error(t, got)
				assert.ErrorIs(t, got, cause)
			},
		},
		{
			name:  "zero delay still wraps",
			err:   cause,
			delay: 0,
			assert: func(t *testing.T, got error) {
				t.Helper()
				require.Error(t, got)
				assert.ErrorIs(t, got, cause)
			},
		},
		{
			name:  "a sentinel cause stays matchable",
			err:   msgin.ErrPayloadTooLarge,
			delay: time.Second,
			assert: func(t *testing.T, got error) {
				t.Helper()
				assert.ErrorIs(t, got, msgin.ErrPayloadTooLarge)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tt.assert(t, msgin.RetryAfter(tt.err, tt.delay))
		})
	}
}
