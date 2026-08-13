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

// TestIsPermanent pins the public classification contract every branch of
// IsPermanent implements: the nil arm, the explicit Permanent marker (through an
// arbitrary wrap depth), each of the three sentinels the doc names as permanent,
// and the transient default.
func TestIsPermanent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		err    error
		assert func(t *testing.T, got bool)
	}{
		{
			name: "nil is not permanent",
			err:  nil,
			assert: func(t *testing.T, got bool) {
				t.Helper()
				assert.False(t, got, "a nil error is not a permanent failure")
			},
		},
		{
			name: "an explicit Permanent marker is permanent",
			err:  msgin.Permanent(errors.New("boom")),
			assert: func(t *testing.T, got bool) {
				t.Helper()
				assert.True(t, got)
			},
		},
		{
			name: "a Permanent marker survives an outer wrap",
			err:  fmt.Errorf("dispatch: %w", msgin.Permanent(errors.New("boom"))),
			assert: func(t *testing.T, got bool) {
				t.Helper()
				assert.True(t, got, "errors.As must traverse the wrap chain")
			},
		},
		{
			name: "ErrPayloadType is permanent",
			err:  msgin.ErrPayloadType,
			assert: func(t *testing.T, got bool) {
				t.Helper()
				assert.True(t, got)
			},
		},
		{
			name: "ErrPayloadDecode is permanent",
			err:  msgin.ErrPayloadDecode,
			assert: func(t *testing.T, got bool) {
				t.Helper()
				assert.True(t, got)
			},
		},
		{
			name: "ErrPayloadTooLarge is permanent",
			err:  msgin.ErrPayloadTooLarge,
			assert: func(t *testing.T, got bool) {
				t.Helper()
				assert.True(t, got)
			},
		},
		{
			name: "a wrapped sentinel is still permanent",
			err:  fmt.Errorf("decode column body: %w", msgin.ErrPayloadDecode),
			assert: func(t *testing.T, got bool) {
				t.Helper()
				assert.True(t, got, "errors.Is must traverse the wrap chain")
			},
		},
		{
			name: "ErrHandlerPanic is transient, not permanent",
			err:  msgin.ErrHandlerPanic,
			assert: func(t *testing.T, got bool) {
				t.Helper()
				assert.False(t, got, "a recovered panic is retried (reliability.go doc)")
			},
		},
		{
			name: "a plain transient error is not permanent",
			err:  errors.New("connection reset by peer"),
			assert: func(t *testing.T, got bool) {
				t.Helper()
				assert.False(t, got)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tt.assert(t, msgin.IsPermanent(tt.err))
		})
	}
}

// TestRetryAfterOf pins the reader half of the RetryAfter marker, including the
// documented normalization of a negative duration to 0 ("no server-instructed
// floor") — which TestRetryAfter cannot observe, having no way to read the
// stored delay back.
func TestRetryAfterOf(t *testing.T) {
	t.Parallel()

	cause := errors.New("boom")

	tests := []struct {
		name   string
		err    error
		assert func(t *testing.T, d time.Duration, ok bool)
	}{
		{
			name: "nil carries no delay",
			err:  nil,
			assert: func(t *testing.T, d time.Duration, ok bool) {
				t.Helper()
				assert.False(t, ok, "errors.As(nil, …) is false — no nil guard needed")
				assert.Zero(t, d)
			},
		},
		{
			name: "an unmarked error carries no delay",
			err:  cause,
			assert: func(t *testing.T, d time.Duration, ok bool) {
				t.Helper()
				assert.False(t, ok)
				assert.Zero(t, d)
			},
		},
		{
			name: "a marked error yields its delay",
			err:  msgin.RetryAfter(cause, 30*time.Second),
			assert: func(t *testing.T, d time.Duration, ok bool) {
				t.Helper()
				require.True(t, ok)
				assert.Equal(t, 30*time.Second, d)
			},
		},
		{
			name: "a wrapped marked error yields its delay",
			err:  fmt.Errorf("POST /hook: %w", msgin.RetryAfter(cause, 5*time.Second)),
			assert: func(t *testing.T, d time.Duration, ok bool) {
				t.Helper()
				require.True(t, ok, "errors.As must traverse the wrap chain")
				assert.Equal(t, 5*time.Second, d)
			},
		},
		{
			name: "a negative delay is normalized to zero, still marked",
			err:  msgin.RetryAfter(cause, -1*time.Second),
			assert: func(t *testing.T, d time.Duration, ok bool) {
				t.Helper()
				require.True(t, ok, "a negative Retry-After is normalized, never rejected")
				assert.Zero(t, d, "normalized to 0 — no server-instructed floor")
			},
		},
		{
			name: "a zero delay is marked with a zero floor",
			err:  msgin.RetryAfter(cause, 0),
			assert: func(t *testing.T, d time.Duration, ok bool) {
				t.Helper()
				require.True(t, ok)
				assert.Zero(t, d)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			d, ok := msgin.RetryAfterOf(tt.err)
			tt.assert(t, d, ok)
		})
	}
}

// TestNewID pins the shape and the uniqueness of the default message id: a
// 128-bit random value, hex-encoded, which is what makes it usable as a
// correlation key and as an at-least-once dedup key.
func TestNewID(t *testing.T) {
	t.Parallel()

	id := msgin.NewID()
	assert.Len(t, id, 32, "128 bits hex-encoded is 32 characters")
	assert.Regexp(t, "^[0-9a-f]{32}$", id, "lowercase hex only")
	assert.NotEqual(t, msgin.NewID(), id, "two calls must not collide")
}
