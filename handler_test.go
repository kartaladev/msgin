package msgin_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kartaladev/msgin"
	"github.com/kartaladev/msgin/adapter/memory"
)

func appendStep(log *[]string, tag string) msgin.Step {
	return func(next msgin.MessageHandler) msgin.MessageHandler {
		return msgin.HandlerFunc(func(ctx context.Context, m msgin.Message[any]) error {
			*log = append(*log, tag)
			return next.Handle(ctx, m)
		})
	}
}

func TestChain(t *testing.T) {
	tests := []struct {
		name   string
		steps  func(log *[]string) []msgin.Step
		assert func(t *testing.T, err error, log []string)
	}{
		{
			name:  "steps run in declaration order then terminal consumes",
			steps: func(log *[]string) []msgin.Step { return []msgin.Step{appendStep(log, "a"), appendStep(log, "b")} },
			assert: func(t *testing.T, err error, log []string) {
				require.NoError(t, err)
				assert.Equal(t, []string{"a", "b"}, log)
			},
		},
		{
			name:  "single step",
			steps: func(log *[]string) []msgin.Step { return []msgin.Step{appendStep(log, "only")} },
			assert: func(t *testing.T, err error, log []string) {
				require.NoError(t, err)
				assert.Equal(t, []string{"only"}, log)
			},
		},
		{
			name:   "empty chain is a no-op consume",
			steps:  func(*[]string) []msgin.Step { return nil },
			assert: func(t *testing.T, err error, log []string) { require.NoError(t, err); assert.Empty(t, log) },
		},
		{
			// A nil element is a WIRING mistake — steps built conditionally
			// (`steps = append(steps, maybeStep())`) can produce one. Chain must
			// not panic on it (CLAUDE.md: no panic on caller input) and must not
			// silently skip it either: skipping would delete a step the caller
			// wrote, so a nil Filter would stop filtering and a nil To would
			// discard. It degrades exactly like To(nil)/Activate(nil) instead.
			name: "a nil step is a permanent ErrNilFunc naming its index",
			steps: func(log *[]string) []msgin.Step {
				return []msgin.Step{appendStep(log, "a"), nil, appendStep(log, "c")}
			},
			assert: func(t *testing.T, err error, log []string) {
				require.ErrorIs(t, err, msgin.ErrNilFunc)
				assert.True(t, msgin.IsPermanent(err), "must be permanent — the nil is fixed at composition")
				assert.Contains(t, err.Error(), "msgin.Chain: nil step at index 1")
				assert.Equal(t, []string{"a"}, log, "the chain stops AT the nil step, not before it")
			},
		},
		{
			name:  "a chain of only a nil step degrades rather than panicking",
			steps: func(*[]string) []msgin.Step { return []msgin.Step{nil} },
			assert: func(t *testing.T, err error, log []string) {
				require.ErrorIs(t, err, msgin.ErrNilFunc)
				assert.Contains(t, err.Error(), "msgin.Chain: nil step at index 0")
				assert.Empty(t, log)
			},
		},
		{
			name: "a mid-chain error stops the chain and propagates",
			steps: func(log *[]string) []msgin.Step {
				boom := func(next msgin.MessageHandler) msgin.MessageHandler {
					return msgin.HandlerFunc(func(context.Context, msgin.Message[any]) error { return errors.New("boom") })
				}
				return []msgin.Step{appendStep(log, "a"), boom, appendStep(log, "c")}
			},
			assert: func(t *testing.T, err error, log []string) {
				assert.ErrorContains(t, err, "boom")
				assert.Equal(t, []string{"a"}, log)
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var log []string
			tc.assert(t, msgin.Chain(tc.steps(&log)...).Handle(t.Context(), msgin.New[any](1)), log)
		})
	}
}

func TestTo(t *testing.T) {
	tests := []struct {
		name   string
		run    func(t *testing.T) error
		assert func(t *testing.T, err error)
	}{
		{
			name: "To sends the message to an OutboundAdapter sink",
			run: func(t *testing.T) error {
				sink := memory.New(memory.WithBuffer(1)) // *memory.Broker is an OutboundAdapter
				return msgin.Chain(msgin.To(sink)).Handle(t.Context(), msgin.New[any]("x"))
			},
			assert: func(t *testing.T, err error) { require.NoError(t, err) },
		},
		{
			// Spec 014 §2.1 row 6 (decision D-M): sink is captured by To's closure
			// at construction, so the fault cannot change for the message's
			// lifetime — it is Permanent and names its position.
			name: "To(nil) is a permanent ErrNilSink naming its position",
			run:  func(t *testing.T) error { return msgin.Chain(msgin.To(nil)).Handle(t.Context(), msgin.New[any](1)) },
			assert: func(t *testing.T, err error) {
				require.ErrorIs(t, err, msgin.ErrNilSink)
				assert.True(t, msgin.IsPermanent(err), "must be permanent")
				assert.Contains(t, err.Error(), "msgin.To: nil sink")
			},
		},
		{
			name: "To propagates the sink send error",
			run: func(t *testing.T) error {
				return msgin.Chain(msgin.To(errSink{})).Handle(t.Context(), msgin.New[any](1))
			},
			assert: func(t *testing.T, err error) { assert.ErrorContains(t, err, "sink-fail") },
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) { tc.assert(t, tc.run(t)) })
	}
}

// errSink is an OutboundAdapter whose Send always fails (covers the To send-error branch).
type errSink struct{}

func (errSink) Send(context.Context, msgin.Message[any]) error { return errors.New("sink-fail") }
