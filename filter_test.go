package msgin_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kartaladev/msgin"
)

func TestFilter(t *testing.T) {
	tests := []struct {
		name        string
		pred        func(context.Context, msgin.Message[int]) (bool, error)
		withDiscard bool
		discardErr  error
		assert      func(t *testing.T, err error, forwarded, discarded bool)
	}{
		{
			name: "true forwards downstream",
			pred: func(_ context.Context, m msgin.Message[int]) (bool, error) { return m.Payload() > 0, nil },
			assert: func(t *testing.T, err error, forwarded, discarded bool) {
				require.NoError(t, err)
				assert.True(t, forwarded)
				assert.False(t, discarded)
			},
		},
		{
			name: "false with no discard channel is silently dropped",
			pred: func(context.Context, msgin.Message[int]) (bool, error) { return false, nil },
			assert: func(t *testing.T, err error, forwarded, discarded bool) {
				require.NoError(t, err)
				assert.False(t, forwarded)
				assert.False(t, discarded)
			},
		},
		{
			name:        "false with discard channel routes the drop",
			pred:        func(context.Context, msgin.Message[int]) (bool, error) { return false, nil },
			withDiscard: true,
			assert: func(t *testing.T, err error, forwarded, discarded bool) {
				require.NoError(t, err)
				assert.False(t, forwarded)
				assert.True(t, discarded)
			},
		},
		{
			name:        "discard channel send error propagates",
			pred:        func(context.Context, msgin.Message[int]) (bool, error) { return false, nil },
			withDiscard: true,
			discardErr:  errors.New("discard-fail"),
			assert:      func(t *testing.T, err error, _, _ bool) { assert.ErrorContains(t, err, "discard-fail") },
		},
		{
			name:   "predicate error propagates",
			pred:   func(context.Context, msgin.Message[int]) (bool, error) { return false, errors.New("boom") },
			assert: func(t *testing.T, err error, _, _ bool) { assert.ErrorContains(t, err, "boom") },
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var forwarded, discarded bool
			next := msgin.HandlerFunc(func(context.Context, msgin.Message[any]) error { forwarded = true; return nil })
			var opts []msgin.FilterOption
			if tc.withDiscard {
				discard := msgin.NewDirectChannel()
				_ = discard.Subscribe(msgin.HandlerFunc(func(context.Context, msgin.Message[any]) error { discarded = true; return tc.discardErr }))
				opts = append(opts, msgin.WithDiscardChannel(discard))
			}
			step := msgin.Filter(tc.pred, opts...)
			tc.assert(t, step(next).Handle(t.Context(), msgin.New[any](1)), forwarded, discarded)
		})
	}
}

func TestFilter_Guards(t *testing.T) {
	next := msgin.HandlerFunc(func(context.Context, msgin.Message[any]) error { return nil })
	t.Run("type mismatch is ErrPayloadType", func(t *testing.T) {
		step := msgin.Filter(func(context.Context, msgin.Message[int]) (bool, error) { return true, nil })
		assert.ErrorIs(t, step(next).Handle(t.Context(), msgin.New[any]("nope")), msgin.ErrPayloadType)
	})
	t.Run("nil predicate is ErrNilFunc", func(t *testing.T) {
		step := msgin.Filter[int](nil)
		assert.ErrorIs(t, step(next).Handle(t.Context(), msgin.New[any](1)), msgin.ErrNilFunc)
	})
}
