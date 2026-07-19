package msgin_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jonboulle/clockwork"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kartaladev/msgin"
)

// schedSink is a fake OutboundAdapter that ALSO implements ScheduledSender,
// recording the last delay and payload it received.
type schedSink struct {
	lastDelay   time.Duration
	lastPayload any
	sendErr     error
	afterErr    error
}

func (s *schedSink) Send(_ context.Context, m msgin.Message[any]) error {
	s.lastPayload = m.Payload()
	return s.sendErr
}

func (s *schedSink) SendAfter(_ context.Context, m msgin.Message[any], d time.Duration) error {
	s.lastDelay = d
	s.lastPayload = m.Payload()
	return s.afterErr
}

// plainSink implements only OutboundAdapter (no ScheduledSender).
type plainSink struct{}

func (plainSink) Send(context.Context, msgin.Message[any]) error { return nil }

func TestProducer_SendAfter(t *testing.T) {
	epoch := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name   string
		run    func(t *testing.T, p msgin.Producer[string], sink *schedSink) error
		assert func(t *testing.T, err error, sink *schedSink)
	}{
		{
			name: "forwards the exact delay and encodes the payload",
			run: func(t *testing.T, p msgin.Producer[string], _ *schedSink) error {
				return p.SendAfter(t.Context(), msgin.New("hi"), 30*time.Minute)
			},
			assert: func(t *testing.T, err error, sink *schedSink) {
				require.NoError(t, err)
				assert.Equal(t, 30*time.Minute, sink.lastDelay)
				assert.Equal(t, []byte(`"hi"`), sink.lastPayload) // JSON-encoded
			},
		},
		{
			name: "negative delay clamps to 0 (deliver now)",
			run: func(t *testing.T, p msgin.Producer[string], _ *schedSink) error {
				return p.SendAfter(t.Context(), msgin.New("hi"), -5*time.Second)
			},
			assert: func(t *testing.T, err error, sink *schedSink) {
				require.NoError(t, err)
				assert.Equal(t, time.Duration(0), sink.lastDelay)
			},
		},
		{
			name: "SendAt computes delay = t - now via the injected clock",
			run: func(t *testing.T, p msgin.Producer[string], _ *schedSink) error {
				return p.SendAt(t.Context(), msgin.New("hi"), epoch.Add(2*time.Hour))
			},
			assert: func(t *testing.T, err error, sink *schedSink) {
				require.NoError(t, err)
				assert.Equal(t, 2*time.Hour, sink.lastDelay)
			},
		},
		{
			name: "SendAt with a past time clamps to 0",
			run: func(t *testing.T, p msgin.Producer[string], _ *schedSink) error {
				return p.SendAt(t.Context(), msgin.New("hi"), epoch.Add(-time.Hour))
			},
			assert: func(t *testing.T, err error, sink *schedSink) {
				require.NoError(t, err)
				assert.Equal(t, time.Duration(0), sink.lastDelay)
			},
		},
		{
			name: "the sink's SendAfter error is propagated",
			run: func(t *testing.T, p msgin.Producer[string], sink *schedSink) error {
				sink.afterErr = errors.New("insert boom")
				return p.SendAfter(t.Context(), msgin.New("hi"), time.Minute)
			},
			assert: func(t *testing.T, err error, _ *schedSink) {
				assert.ErrorContains(t, err, "insert boom")
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sink := &schedSink{}
			p, err := msgin.NewProducer[string](sink, msgin.WithProducerClock[string](clockwork.NewFakeClockAt(epoch)))
			require.NoError(t, err)
			tc.assert(t, tc.run(t, p, sink), sink)
		})
	}
}

func TestProducer_ScheduledSendUnsupported(t *testing.T) {
	p, err := msgin.NewProducer[string](plainSink{})
	require.NoError(t, err)
	t.Run("SendAfter on a non-ScheduledSender sink", func(t *testing.T) {
		assert.ErrorIs(t, p.SendAfter(t.Context(), msgin.New("x"), time.Minute), msgin.ErrScheduledSendUnsupported)
	})
	t.Run("SendAt on a non-ScheduledSender sink", func(t *testing.T) {
		assert.ErrorIs(t, p.SendAt(t.Context(), msgin.New("x"), time.Now().Add(time.Hour)), msgin.ErrScheduledSendUnsupported)
	})
}

// TestProducer_SendAfter_EncodeErrorPropagates covers SendAfter's OWN
// encode-error forwarding branch (`boxed, err := p.box(msg); if err != nil`),
// distinct from Send's — audit round-2 MINOR. Reuses the existing failingCodec /
// errEncode declared in producer_test.go (same msgin_test package). failingCodec
// is a PayloadCodec[order] (its Encode/Decode are typed to the order struct, not
// generic), so this case is built with NewProducer[order] rather than [string]
// to satisfy WithProducerCodec[order](failingCodec{}) — adapted from the brief's
// literal [string] form to match the real (non-generic-method) failingCodec type.
// The sink is a *schedSink (implements ScheduledSender) so the type-assert
// passes and control reaches box(); the failing codec then makes box return a
// wrapped encode error.
func TestProducer_SendAfter_EncodeErrorPropagates(t *testing.T) {
	p, err := msgin.NewProducer[order](&schedSink{}, msgin.WithProducerCodec[order](failingCodec{}))
	require.NoError(t, err)
	assert.ErrorIs(t, p.SendAfter(t.Context(), msgin.New(order{ID: "o1", Total: 3}), time.Minute), errEncode)
}
