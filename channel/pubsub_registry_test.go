package channel_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kartaladev/msgin"
	"github.com/kartaladev/msgin/channel"
)

func TestPubSub_TopicScopedDelivery(t *testing.T) {
	ps := channel.NewPubSub()
	var a, b int
	_, err := ps.Subscribe("topic-a", msgin.HandlerFunc(func(context.Context, msgin.Message[any]) error { a++; return nil }))
	require.NoError(t, err)
	_, err = ps.Subscribe("topic-b", msgin.HandlerFunc(func(context.Context, msgin.Message[any]) error { b++; return nil }))
	require.NoError(t, err)

	require.NoError(t, ps.Publish(t.Context(), "topic-a", msgin.New[any]("x")))
	assert.Equal(t, 1, a)
	assert.Equal(t, 0, b) // topic-scoped: topic-b did not receive topic-a's message
}

func TestPubSub_CancelOneOfSeveralKeepsTopic(t *testing.T) {
	// F4: cancelling one of several subscribers keeps the topic alive (the
	// drop-on-empty KEEP branch) and the survivor still receives publishes.
	ps := channel.NewPubSub()
	var s1, s2 int
	sub1, err := ps.Subscribe("t", msgin.HandlerFunc(func(context.Context, msgin.Message[any]) error { s1++; return nil }))
	require.NoError(t, err)
	_, err = ps.Subscribe("t", msgin.HandlerFunc(func(context.Context, msgin.Message[any]) error { s2++; return nil }))
	require.NoError(t, err)

	sub1.Cancel()
	assert.Equal(t, 1, ps.TopicCount()) // topic survives: s2 is still subscribed

	require.NoError(t, ps.Publish(t.Context(), "t", msgin.New[any]("x")))
	assert.Equal(t, 0, s1) // cancelled: not invoked
	assert.Equal(t, 1, s2) // survivor received
}

func TestPubSub_Behaviors(t *testing.T) {
	tests := []struct {
		name   string
		run    func(t *testing.T, ps *channel.PubSub) error
		assert func(t *testing.T, err error, ps *channel.PubSub)
	}{
		{
			name: "publish to a topic with no subscribers is a no-op",
			run: func(t *testing.T, ps *channel.PubSub) error {
				return ps.Publish(t.Context(), "nobody", msgin.New[any](1))
			},
			assert: func(t *testing.T, err error, ps *channel.PubSub) {
				require.NoError(t, err)
				assert.Equal(t, 0, ps.TopicCount())
			},
		},
		{
			name: "nil handler is ErrNilHandler",
			run:  func(t *testing.T, ps *channel.PubSub) error { _, err := ps.Subscribe("t", nil); return err },
			assert: func(t *testing.T, err error, ps *channel.PubSub) {
				assert.ErrorIs(t, err, msgin.ErrNilHandler)
				assert.Equal(t, 0, ps.TopicCount()) // no topic created for a rejected subscribe
			},
		},
		{
			name: "subscribe lazily creates the topic; cancel drops it when empty",
			run: func(t *testing.T, ps *channel.PubSub) error {
				sub, err := ps.Subscribe("t", msgin.HandlerFunc(func(context.Context, msgin.Message[any]) error { return nil }))
				require.NoError(t, err)
				require.Equal(t, 1, ps.TopicCount()) // lazily created
				sub.Cancel()
				return nil
			},
			assert: func(t *testing.T, err error, ps *channel.PubSub) {
				require.NoError(t, err)
				assert.Equal(t, 0, ps.TopicCount()) // dropped on empty
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ps := channel.NewPubSub()
			tc.assert(t, tc.run(t, ps), ps)
		})
	}
}

func TestPubSub_SatisfiesSPI(t *testing.T) {
	var _ msgin.TopicPublisher = channel.NewPubSub()
	var _ msgin.TopicSubscriber = channel.NewPubSub()
}

// TestNewPubSub_NilOptionElement proves a nil ELEMENT of opts is LATCHED at
// construction instead of panicking. NewPubSub has no error return, so it is
// family R2 (Spec 015 §3.2, ADR 0031 D-P/D-S): the registry is still returned
// and BOTH its error-returning methods — Publish and Subscribe — report the
// fault as a PERMANENT ErrNilFunc naming the computed 0-based index. PubSub has
// no SingleSubscriber method; TopicCount is a non-error method and is not
// forced. The last row pins D-V: the latch is checked at the TOP of Subscribe,
// above its own nil-handler check.
func TestNewPubSub_NilOptionElement(t *testing.T) {
	publish := func(t *testing.T, p *channel.PubSub) error {
		return p.Publish(t.Context(), "topic", msgin.New[any](1))
	}
	subscribe := func(_ *testing.T, p *channel.PubSub) error {
		_, err := p.Subscribe("topic", msgin.HandlerFunc(func(context.Context, msgin.Message[any]) error { return nil }))
		return err
	}

	tests := []struct {
		name     string
		registry func() *channel.PubSub
		call     func(t *testing.T, p *channel.PubSub) error
		assert   func(t *testing.T, err error)
	}{
		{
			name:     "AC-1/AC-5b: a nil element alone is reported by Publish",
			registry: func() *channel.PubSub { return channel.NewPubSub(nil) },
			call:     publish,
			assert: func(t *testing.T, err error) {
				require.ErrorIs(t, err, msgin.ErrNilFunc)
				assert.True(t, msgin.IsPermanent(err), "R2 nil-option error must be Permanent-wrapped")
				assert.Contains(t, err.Error(), "channel.NewPubSub: nil option at index 0")
			},
		},
		{
			name:     "AC-5b: the same latch is reported by Subscribe",
			registry: func() *channel.PubSub { return channel.NewPubSub(nil) },
			call:     subscribe,
			assert: func(t *testing.T, err error) {
				require.ErrorIs(t, err, msgin.ErrNilFunc)
				assert.True(t, msgin.IsPermanent(err))
				assert.Contains(t, err.Error(), "channel.NewPubSub: nil option at index 0")
			},
		},
		{
			name: "AC-2: the index is COMPUTED, and the position names this constructor (Publish)",
			registry: func() *channel.PubSub {
				return channel.NewPubSub(channel.WithFanOut(channel.FanOutBestEffort), nil)
			},
			call: publish,
			assert: func(t *testing.T, err error) {
				require.ErrorIs(t, err, msgin.ErrNilFunc)
				assert.True(t, msgin.IsPermanent(err))
				assert.Contains(t, err.Error(), "channel.NewPubSub: nil option at index 1")
			},
		},
		{
			name: "AC-2: the computed index is reported by Subscribe too",
			registry: func() *channel.PubSub {
				return channel.NewPubSub(channel.WithFanOut(channel.FanOutBestEffort), nil)
			},
			call: subscribe,
			assert: func(t *testing.T, err error) {
				require.ErrorIs(t, err, msgin.ErrNilFunc)
				assert.Contains(t, err.Error(), "channel.NewPubSub: nil option at index 1")
			},
		},
		{
			// AC-3's R2 half: D-U's "latch only when unlatched" is what preserves
			// first-nil-wins; latching the LAST nil passes every other row here.
			name:     "AC-3: the FIRST of two nils wins",
			registry: func() *channel.PubSub { return channel.NewPubSub(nil, nil) },
			call:     publish,
			assert: func(t *testing.T, err error) {
				require.ErrorIs(t, err, msgin.ErrNilFunc)
				assert.Contains(t, err.Error(), "channel.NewPubSub: nil option at index 0")
			},
		},
		{
			// AC-5d / D-V: the option fault happened at construction, so it is
			// chronologically earlier than the nil handler and is reported first.
			name:     "AC-5d: a nil option is reported BEFORE Subscribe's own nil-handler check",
			registry: func() *channel.PubSub { return channel.NewPubSub(nil) },
			call: func(_ *testing.T, p *channel.PubSub) error {
				_, err := p.Subscribe("topic", nil)
				return err
			},
			assert: func(t *testing.T, err error) {
				require.ErrorIs(t, err, msgin.ErrNilFunc)
				assert.True(t, msgin.IsPermanent(err))
				assert.Contains(t, err.Error(), "channel.NewPubSub: nil option at index 0")
				assert.NotErrorIs(t, err, msgin.ErrNilHandler)
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := tc.registry()
			tc.assert(t, tc.call(t, p))
			assert.Zero(t, p.TopicCount(), "TopicCount is a non-error method: it is not forced, and a rejected Subscribe creates no topic")
		})
	}
}
