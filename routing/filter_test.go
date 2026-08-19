package routing_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kartaladev/msgin"
	"github.com/kartaladev/msgin/channel"
	"github.com/kartaladev/msgin/routing"
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
			var opts []routing.FilterOption
			if tc.withDiscard {
				discard := channel.NewDirectChannel()
				_, subErr := discard.Subscribe(msgin.HandlerFunc(func(context.Context, msgin.Message[any]) error { discarded = true; return tc.discardErr }))
				require.NoError(t, subErr)
				opts = append(opts, routing.WithDiscardChannel(discard))
			}
			step := routing.Filter(tc.pred, opts...)
			tc.assert(t, step(next).Handle(t.Context(), msgin.New[any](1)), forwarded, discarded)
		})
	}
}

func TestFilter_Guards(t *testing.T) {
	next := msgin.HandlerFunc(func(context.Context, msgin.Message[any]) error { return nil })
	t.Run("type mismatch is ErrPayloadType", func(t *testing.T) {
		step := routing.Filter(func(context.Context, msgin.Message[int]) (bool, error) { return true, nil })
		assert.ErrorIs(t, step(next).Handle(t.Context(), msgin.New[any]("nope")), msgin.ErrPayloadType)
	})
	t.Run("nil predicate is ErrNilFunc", func(t *testing.T) {
		step := routing.Filter[int](nil)
		assert.ErrorIs(t, step(next).Handle(t.Context(), msgin.New[any](1)), msgin.ErrNilFunc)
	})
}

// TestFilter_NilOptionElement proves a nil ELEMENT of opts degrades the Step
// exactly as a nil pred does — a PERMANENT ErrNilFunc naming the computed
// 0-based index, raised at DISPATCH (Filter has no error return, so it is
// family R2; Spec 015 §3.2, ADR 0031 D-P/D-S). The last two rows pin D-V
// (ADR 0031): the option loop sits ABOVE the pred check, so a nil option and a
// nil pred together report the OPTION, while a nil pred with no options at all
// still reports "nil pred", unchanged.
func TestFilter_NilOptionElement(t *testing.T) {
	pred := func(context.Context, msgin.Message[string]) (bool, error) { return true, nil }

	tests := []struct {
		name   string
		step   func() msgin.Step
		assert func(t *testing.T, err error)
	}{
		{
			name: "nil element alone",
			step: func() msgin.Step { return routing.Filter[string](pred, nil) },
			assert: func(t *testing.T, err error) {
				require.ErrorIs(t, err, msgin.ErrNilFunc)
				assert.True(t, msgin.IsPermanent(err), "R2 nil-option error must be Permanent-wrapped")
				assert.Contains(t, err.Error(), "routing.Filter: nil option at index 0")
			},
		},
		{
			name: "nil element after a valid option asserts the COMPUTED index and the FULL position",
			step: func() msgin.Step {
				return routing.Filter[string](pred, routing.WithDiscardChannel(channel.NewDirectChannel()), nil)
			},
			assert: func(t *testing.T, err error) {
				require.ErrorIs(t, err, msgin.ErrNilFunc)
				assert.True(t, msgin.IsPermanent(err))
				assert.Contains(t, err.Error(), "routing.Filter: nil option at index 1")
			},
		},
		{
			name: "first of two nils wins",
			step: func() msgin.Step { return routing.Filter[string](pred, nil, nil) },
			assert: func(t *testing.T, err error) {
				require.ErrorIs(t, err, msgin.ErrNilFunc)
				assert.Contains(t, err.Error(), "routing.Filter: nil option at index 0")
			},
		},
		{
			// D-V: the option fault happened at construction, so it is
			// chronologically earlier than the nil pred and is reported first.
			name: "D-V: a nil option is reported BEFORE the nil pred",
			step: func() msgin.Step { return routing.Filter[string](nil, nil) },
			assert: func(t *testing.T, err error) {
				require.ErrorIs(t, err, msgin.ErrNilFunc)
				assert.True(t, msgin.IsPermanent(err))
				assert.Contains(t, err.Error(), "routing.Filter: nil option at index 0")
				assert.NotContains(t, err.Error(), "nil pred")
			},
		},
		{
			// The other half of D-V: no option means nothing displaces the
			// pred check, so the pre-existing diagnosis is unchanged.
			name: "nil pred with NO options still reports nil pred",
			step: func() msgin.Step { return routing.Filter[string](nil) },
			assert: func(t *testing.T, err error) {
				require.ErrorIs(t, err, msgin.ErrNilFunc)
				assert.True(t, msgin.IsPermanent(err))
				assert.Contains(t, err.Error(), "routing.Filter: nil pred")
				assert.NotContains(t, err.Error(), "nil option")
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			forwarded := false
			next := msgin.HandlerFunc(func(context.Context, msgin.Message[any]) error { forwarded = true; return nil })

			err := tc.step()(next).Handle(t.Context(), msgin.New[any]("x"))

			assert.False(t, forwarded, "a degraded Filter must not forward downstream")
			tc.assert(t, err)
		})
	}
}
