package channel_test

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kartaladev/msgin"
	"github.com/kartaladev/msgin/channel"
	"github.com/kartaladev/msgin/endpoint"
)

func TestPublishSubscribeChannel_FanOut(t *testing.T) {
	tests := []struct {
		name   string
		policy channel.FanOutPolicy
		errs   []error // one per subscriber, nil = success
		assert func(t *testing.T, sendErr error, got []string)
	}{
		{
			name: "fans out to all subscribers in registration order",
			errs: []error{nil, nil, nil},
			assert: func(t *testing.T, sendErr error, got []string) {
				require.NoError(t, sendErr)
				assert.Equal(t, []string{"a", "b", "c"}, got)
			},
		},
		{
			name: "all-succeed: a subscriber error is joined, others still invoked",
			errs: []error{nil, errors.New("boom"), nil},
			assert: func(t *testing.T, sendErr error, got []string) {
				assert.ErrorContains(t, sendErr, "boom")
				assert.Equal(t, []string{"a", "b", "c"}, got) // every subscriber still ran
			},
		},
		{
			name:   "best-effort: a subscriber error is swallowed, Send returns nil",
			policy: channel.FanOutBestEffort,
			errs:   []error{nil, errors.New("boom"), nil},
			assert: func(t *testing.T, sendErr error, got []string) {
				require.NoError(t, sendErr)
				assert.Equal(t, []string{"a", "b", "c"}, got)
			},
		},
		{
			name: "no subscribers is a no-op",
			errs: nil,
			assert: func(t *testing.T, sendErr error, got []string) {
				require.NoError(t, sendErr)
				assert.Empty(t, got)
			},
		},
	}
	tags := []string{"a", "b", "c"}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ps := channel.NewPublishSubscribeChannel(channel.WithFanOut(tc.policy))
			var got []string
			for i, e := range tc.errs {
				tag, e := tags[i], e
				_, err := ps.Subscribe(msgin.HandlerFunc(func(context.Context, msgin.Message[any]) error {
					got = append(got, tag)
					return e
				}))
				require.NoError(t, err)
			}
			tc.assert(t, ps.Send(t.Context(), msgin.New[any]("x")), got)
		})
	}
}

func TestPublishSubscribeChannel_SubscribeAndCancel(t *testing.T) {
	t.Run("nil handler is ErrNilHandler", func(t *testing.T) {
		_, err := channel.NewPublishSubscribeChannel().Subscribe(nil)
		assert.ErrorIs(t, err, msgin.ErrNilHandler)
	})
	t.Run("cancel removes the subscriber and is idempotent", func(t *testing.T) {
		ps := channel.NewPublishSubscribeChannel()
		var count int
		sub, err := ps.Subscribe(msgin.HandlerFunc(func(context.Context, msgin.Message[any]) error { count++; return nil }))
		require.NoError(t, err)
		require.NoError(t, ps.Send(t.Context(), msgin.New[any](1)))
		assert.Equal(t, 1, count)
		sub.Cancel()
		sub.Cancel() // idempotent: no panic, no double-remove
		require.NoError(t, ps.Send(t.Context(), msgin.New[any](2)))
		assert.Equal(t, 1, count) // cancelled: not invoked again
	})
}

// TestPublishSubscribeChannel_SingleSubscriber pins decision D-F: the opt-in
// exclusivity guard, which is OFF by default so fan-out stays the pattern's
// default behavior.
func TestPublishSubscribeChannel_SingleSubscriber(t *testing.T) {
	tests := []struct {
		name   string
		opts   []channel.PubSubOption
		run    func(t *testing.T, ps *channel.PublishSubscribeChannel) (msgin.Subscription, error)
		assert func(t *testing.T, sub msgin.Subscription, err error)
	}{
		{
			name: "option off: a second subscriber is accepted (fan-out is the default)",
			run: func(t *testing.T, ps *channel.PublishSubscribeChannel) (msgin.Subscription, error) {
				_, err := ps.Subscribe(noopHandler())
				require.NoError(t, err)
				return ps.Subscribe(noopHandler())
			},
			assert: func(t *testing.T, sub msgin.Subscription, err error) {
				require.NoError(t, err)
				assert.NotNil(t, sub)
			},
		},
		{
			name: "option on: the first subscriber is accepted",
			opts: []channel.PubSubOption{channel.WithSingleSubscriber()},
			run: func(t *testing.T, ps *channel.PublishSubscribeChannel) (msgin.Subscription, error) {
				return ps.Subscribe(noopHandler())
			},
			assert: func(t *testing.T, sub msgin.Subscription, err error) {
				require.NoError(t, err)
				assert.NotNil(t, sub)
			},
		},
		{
			name: "option on: a second subscriber is ErrChannelSubscribed",
			opts: []channel.PubSubOption{channel.WithSingleSubscriber()},
			run: func(t *testing.T, ps *channel.PublishSubscribeChannel) (msgin.Subscription, error) {
				_, err := ps.Subscribe(noopHandler())
				require.NoError(t, err)
				return ps.Subscribe(noopHandler())
			},
			assert: func(t *testing.T, sub msgin.Subscription, err error) {
				assert.ErrorIs(t, err, msgin.ErrChannelSubscribed)
				assert.Nil(t, sub, "the error path must return no handle")
			},
		},
		{
			name: "option on: cancelling frees the slot for a new subscriber",
			opts: []channel.PubSubOption{channel.WithSingleSubscriber()},
			run: func(t *testing.T, ps *channel.PublishSubscribeChannel) (msgin.Subscription, error) {
				first, err := ps.Subscribe(noopHandler())
				require.NoError(t, err)
				first.Cancel()
				return ps.Subscribe(noopHandler())
			},
			assert: func(t *testing.T, sub msgin.Subscription, err error) {
				require.NoError(t, err)
				assert.NotNil(t, sub)
			},
		},
		{
			name: "option on: a nil handler is still ErrNilHandler",
			opts: []channel.PubSubOption{channel.WithSingleSubscriber()},
			run: func(t *testing.T, ps *channel.PublishSubscribeChannel) (msgin.Subscription, error) {
				return ps.Subscribe(nil)
			},
			assert: func(t *testing.T, sub msgin.Subscription, err error) {
				assert.ErrorIs(t, err, msgin.ErrNilHandler)
				assert.Nil(t, sub)
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sub, err := tc.run(t, channel.NewPublishSubscribeChannel(tc.opts...))
			tc.assert(t, sub, err)
		})
	}
}

// TestPubSub_SingleSubscriberPropagatesToTopics proves the registry inherits the
// option, so its per-topic channels reject a second subscriber — and that the
// now-reachable error return leaves the live topic untouched.
func TestPubSub_SingleSubscriberPropagatesToTopics(t *testing.T) {
	ps := channel.NewPubSub(channel.WithSingleSubscriber())

	first, err := ps.Subscribe("orders", noopHandler())
	require.NoError(t, err)
	require.NotNil(t, first)
	assert.Equal(t, 1, ps.TopicCount())

	second, err := ps.Subscribe("orders", noopHandler())
	assert.ErrorIs(t, err, msgin.ErrChannelSubscribed)
	assert.Nil(t, second)
	assert.Equal(t, 1, ps.TopicCount(), "the rejected subscribe must not disturb the live topic")

	first.Cancel()
	assert.Equal(t, 0, ps.TopicCount(), "drop-on-empty still applies")
}

func TestPublishSubscribeChannel_SubscriberPanicIsIsolated(t *testing.T) {
	tests := []struct {
		name   string
		policy channel.FanOutPolicy
		assert func(t *testing.T, sendErr error, laterRan bool)
	}{
		{
			name: "all-succeed: panic is a transient error, later subscribers still run",
			assert: func(t *testing.T, sendErr error, laterRan bool) {
				assert.ErrorIs(t, sendErr, msgin.ErrHandlerPanic)
				assert.True(t, laterRan)
			},
		},
		{
			name:   "best-effort: panic is logged, Send returns nil, later subscribers still run",
			policy: channel.FanOutBestEffort,
			assert: func(t *testing.T, sendErr error, laterRan bool) {
				require.NoError(t, sendErr)
				assert.True(t, laterRan)
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ps := channel.NewPublishSubscribeChannel(channel.WithFanOut(tc.policy))
			_, err := ps.Subscribe(msgin.HandlerFunc(func(context.Context, msgin.Message[any]) error { panic("boom") }))
			require.NoError(t, err)
			var laterRan bool
			_, err = ps.Subscribe(msgin.HandlerFunc(func(context.Context, msgin.Message[any]) error { laterRan = true; return nil }))
			require.NoError(t, err)
			tc.assert(t, ps.Send(t.Context(), msgin.New[any](1)), laterRan)
		})
	}
}

func TestPublishSubscribeChannel_PermanentErrorPropagates(t *testing.T) {
	// Unit-settlement (F2): a subscriber's permanent error makes the joined fan-out
	// permanent (errors.Join propagates it), even mixed with a transient failure —
	// so a Consumer-driven publish diverts the whole message to the invalid sink.
	ps := channel.NewPublishSubscribeChannel()
	_, err := ps.Subscribe(msgin.HandlerFunc(func(context.Context, msgin.Message[any]) error { return msgin.ErrPayloadType }))
	require.NoError(t, err)
	_, err = ps.Subscribe(msgin.HandlerFunc(func(context.Context, msgin.Message[any]) error { return errors.New("transient") }))
	require.NoError(t, err)
	assert.ErrorIs(t, ps.Send(t.Context(), msgin.New[any](1)), msgin.ErrPayloadType)
}

func TestPublishSubscribeChannel_BestEffortLogsToInjectedLogger(t *testing.T) {
	var buf strings.Builder
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))
	ps := channel.NewPublishSubscribeChannel(channel.WithFanOut(channel.FanOutBestEffort), channel.WithPubSubLogger(logger))
	_, err := ps.Subscribe(msgin.HandlerFunc(func(context.Context, msgin.Message[any]) error { return errors.New("sub-fail") }))
	require.NoError(t, err)
	require.NoError(t, ps.Send(t.Context(), msgin.New[any](1)))
	assert.Contains(t, buf.String(), "sub-fail")
	_ = channel.NewPublishSubscribeChannel(channel.WithPubSubLogger(nil)) // nil is a no-op (keeps default discard logger)
}

func TestPublishSubscribeChannel_IsOutboundAdapter(t *testing.T) {
	var _ msgin.OutboundAdapter = channel.NewPublishSubscribeChannel() // compiles => Send satisfies the SPI
}

// TestNewPublishSubscribeChannel_NilOptionElement proves a nil ELEMENT of opts
// is LATCHED at construction instead of panicking. NewPublishSubscribeChannel
// has no error return, so it is family R2 (Spec 015 §3.2, ADR 0031 D-P/D-S):
// the channel is still returned, and EVERY error-returning method — Send and
// Subscribe — reports the fault as a PERMANENT ErrNilFunc naming the computed
// 0-based index. The last row pins D-V: the latch is checked at the TOP of
// Subscribe, above its own nil-handler check.
func TestNewPublishSubscribeChannel_NilOptionElement(t *testing.T) {
	send := func(t *testing.T, c *channel.PublishSubscribeChannel) error {
		return c.Send(t.Context(), msgin.New[any](1))
	}
	subscribe := func(_ *testing.T, c *channel.PublishSubscribeChannel) error {
		_, err := c.Subscribe(msgin.HandlerFunc(func(context.Context, msgin.Message[any]) error { return nil }))
		return err
	}

	tests := []struct {
		name   string
		ch     func() *channel.PublishSubscribeChannel
		call   func(t *testing.T, c *channel.PublishSubscribeChannel) error
		assert func(t *testing.T, err error)
	}{
		{
			name: "AC-1/AC-5b: a nil element alone is reported by Send",
			ch:   func() *channel.PublishSubscribeChannel { return channel.NewPublishSubscribeChannel(nil) },
			call: send,
			assert: func(t *testing.T, err error) {
				require.ErrorIs(t, err, msgin.ErrNilFunc)
				assert.True(t, msgin.IsPermanent(err), "R2 nil-option error must be Permanent-wrapped")
				assert.Contains(t, err.Error(), "channel.NewPublishSubscribeChannel: nil option at index 0")
			},
		},
		{
			name: "AC-5b: the same latch is reported by Subscribe",
			ch:   func() *channel.PublishSubscribeChannel { return channel.NewPublishSubscribeChannel(nil) },
			call: subscribe,
			assert: func(t *testing.T, err error) {
				require.ErrorIs(t, err, msgin.ErrNilFunc)
				assert.True(t, msgin.IsPermanent(err))
				assert.Contains(t, err.Error(), "channel.NewPublishSubscribeChannel: nil option at index 0")
			},
		},
		{
			name: "AC-2: the index is COMPUTED, and the position names this constructor (Send)",
			ch: func() *channel.PublishSubscribeChannel {
				return channel.NewPublishSubscribeChannel(channel.WithFanOut(channel.FanOutBestEffort), nil)
			},
			call: send,
			assert: func(t *testing.T, err error) {
				require.ErrorIs(t, err, msgin.ErrNilFunc)
				assert.True(t, msgin.IsPermanent(err))
				assert.Contains(t, err.Error(), "channel.NewPublishSubscribeChannel: nil option at index 1")
			},
		},
		{
			name: "AC-2: the computed index is reported by Subscribe too",
			ch: func() *channel.PublishSubscribeChannel {
				return channel.NewPublishSubscribeChannel(channel.WithFanOut(channel.FanOutBestEffort), nil)
			},
			call: subscribe,
			assert: func(t *testing.T, err error) {
				require.ErrorIs(t, err, msgin.ErrNilFunc)
				assert.Contains(t, err.Error(), "channel.NewPublishSubscribeChannel: nil option at index 1")
			},
		},
		{
			// AC-3's R2 half: D-U's "latch only when unlatched" is what preserves
			// first-nil-wins; an implementation latching the LAST nil passes every
			// other assertion in this table.
			name: "AC-3: the FIRST of two nils wins",
			ch:   func() *channel.PublishSubscribeChannel { return channel.NewPublishSubscribeChannel(nil, nil) },
			call: send,
			assert: func(t *testing.T, err error) {
				require.ErrorIs(t, err, msgin.ErrNilFunc)
				assert.Contains(t, err.Error(), "channel.NewPublishSubscribeChannel: nil option at index 0")
			},
		},
		{
			// AC-5d / D-V: the option fault happened at construction, so it is
			// chronologically earlier than the nil handler and is reported first.
			name: "AC-5d: a nil option is reported BEFORE Subscribe's own nil-handler check",
			ch:   func() *channel.PublishSubscribeChannel { return channel.NewPublishSubscribeChannel(nil) },
			call: func(_ *testing.T, c *channel.PublishSubscribeChannel) error {
				_, err := c.Subscribe(nil)
				return err
			},
			assert: func(t *testing.T, err error) {
				require.ErrorIs(t, err, msgin.ErrNilFunc)
				assert.True(t, msgin.IsPermanent(err))
				assert.Contains(t, err.Error(), "channel.NewPublishSubscribeChannel: nil option at index 0")
				assert.NotErrorIs(t, err, msgin.ErrNilHandler)
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tc.assert(t, tc.call(t, tc.ch()))
		})
	}
}

// TestNewPublishSubscribeChannel_NilOptionElement_SurvivorsApply pins decision
// D-U on the ONE surface in the library where it is directly observable
// (Spec 015 §3.2): the latch loop CONTINUEs, so every non-nil option applies
// whether it sits before or after the nil, and SingleSubscriber — a non-error
// method — reports exactly what those survivors set rather than being forced.
func TestNewPublishSubscribeChannel_NilOptionElement_SurvivorsApply(t *testing.T) {
	tests := []struct {
		name   string
		ch     func() *channel.PublishSubscribeChannel
		assert func(t *testing.T, single bool)
	}{
		{
			name: "the option AFTER the nil still applies (D-U: continue, not break)",
			ch: func() *channel.PublishSubscribeChannel {
				return channel.NewPublishSubscribeChannel(nil, channel.WithSingleSubscriber())
			},
			assert: func(t *testing.T, single bool) { assert.True(t, single) },
		},
		{
			name: "the option BEFORE the nil still applies",
			ch: func() *channel.PublishSubscribeChannel {
				return channel.NewPublishSubscribeChannel(channel.WithSingleSubscriber(), nil)
			},
			assert: func(t *testing.T, single bool) { assert.True(t, single) },
		},
		{
			name: "a latched channel is not FORCED to exclusive either",
			ch:   func() *channel.PublishSubscribeChannel { return channel.NewPublishSubscribeChannel(nil) },
			assert: func(t *testing.T, single bool) {
				assert.False(t, single, "SingleSubscriber reports what the survivors set, not a fixed value")
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tc.assert(t, tc.ch().SingleSubscriber())
		})
	}
}

// TestNewChannelExchange_LatchedReplyChannel_ReportsNilOption is AC-5c: the
// latched fault must not be LAUNDERED into a different diagnosis. A reply
// channel built with a nil option element must make NewChannelExchange report
// msgin.ErrNilFunc — never msgin.ErrSharedReplyChannel — in EITHER option
// ordering.
//
// The nil-FIRST row is the executable proof of D-U, and the only one in the
// whole bundle: NewChannelExchange runs its exclusivity probe (which consults
// SingleSubscriber) BEFORE it calls reply.Subscribe, where the latch surfaces.
// Under a `break` loop the surviving WithSingleSubscriber never applies, the
// probe sees a shared channel and rejects it with ErrSharedReplyChannel — the
// nil option never mentioned. Under D-U's `continue` the option applies, the
// probe passes, and Subscribe reports the latch.
func TestNewChannelExchange_LatchedReplyChannel_ReportsNilOption(t *testing.T) {
	tests := []struct {
		name  string
		reply func() msgin.SubscribableChannel
		index string
	}{
		{
			name: "nil option SECOND",
			reply: func() msgin.SubscribableChannel {
				return channel.NewPublishSubscribeChannel(channel.WithSingleSubscriber(), nil)
			},
			index: "channel.NewPublishSubscribeChannel: nil option at index 1",
		},
		{
			name: "nil option FIRST — fails under `break`, passes under D-U's `continue`",
			reply: func() msgin.SubscribableChannel {
				return channel.NewPublishSubscribeChannel(nil, channel.WithSingleSubscriber())
			},
			index: "channel.NewPublishSubscribeChannel: nil option at index 0",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ex, err := endpoint.NewChannelExchange(channel.NewDirectChannel(), tc.reply())
			assert.Nil(t, ex)
			require.ErrorIs(t, err, msgin.ErrNilFunc, "the nil option must survive as the diagnosis")
			assert.NotErrorIs(t, err, msgin.ErrSharedReplyChannel, "the fault must not be laundered into a shared-reply rejection")
			assert.True(t, msgin.IsPermanent(err))
			assert.Contains(t, err.Error(), tc.index)
		})
	}
}
