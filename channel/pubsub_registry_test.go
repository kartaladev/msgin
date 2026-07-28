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
