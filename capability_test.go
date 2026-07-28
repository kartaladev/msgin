package msgin_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kartaladev/msgin"
	"github.com/kartaladev/msgin/adapter/memory"
	"github.com/kartaladev/msgin/channel"
	"github.com/kartaladev/msgin/endpoint"
	"github.com/kartaladev/msgin/routing"
)

// capabilityPayload is the marker payload every capability case drives through
// its send-only call site.
const capabilityPayload = "capability-probe"

// sendTarget pairs a send-only destination with a way to observe what reached
// it. The msgin.MessageChannel field is the point of the test: before ADR 0028
// only DirectChannel satisfied that interface, so none of these targets could be
// assigned to it at all (the RED artifact is a compile failure, not a FAIL line).
type sendTarget struct {
	ch      msgin.MessageChannel
	waitFor func(t *testing.T) (msgin.Message[any], bool)
}

// newQueueTarget builds a QueueChannel over an in-memory store; arrival is
// observed by polling the channel (it is a PollingSource, not a subscriber).
func newQueueTarget(t *testing.T) sendTarget {
	t.Helper()
	store, err := memory.NewQueueStore()
	require.NoError(t, err)
	qc, err := channel.NewQueueChannel(store)
	require.NoError(t, err)
	return sendTarget{
		ch: qc,
		waitFor: func(t *testing.T) (msgin.Message[any], bool) {
			t.Helper()
			deadline := time.Now().Add(2 * time.Second)
			for time.Now().Before(deadline) {
				ds, err := qc.Poll(t.Context(), 1)
				require.NoError(t, err)
				if len(ds) == 1 {
					return ds[0].Msg, true
				}
				time.Sleep(time.Millisecond)
			}
			return msgin.Message[any]{}, false
		},
	}
}

// newPubSubTarget builds a PublishSubscribeChannel with one recording
// subscriber.
func newPubSubTarget(t *testing.T) sendTarget {
	t.Helper()
	ps := channel.NewPublishSubscribeChannel()
	got := make(chan msgin.Message[any], 4)
	_, err := ps.Subscribe(msgin.HandlerFunc(func(_ context.Context, m msgin.Message[any]) error {
		got <- m
		return nil
	}))
	require.NoError(t, err)
	return sendTarget{ch: ps, waitFor: recvWithin(got)}
}

// newBrokerTarget builds a shipped OutboundAdapter (memory.Broker). ADR 0028 §5
// makes every OutboundAdapter a legal send-only destination; that widening is
// what this case pins.
func newBrokerTarget(t *testing.T) sendTarget {
	t.Helper()
	b := memory.New(memory.WithBuffer(4))
	out := make(chan msgin.Delivery, 4)
	go func() { _ = b.Stream(t.Context(), out) }()
	got := make(chan msgin.Message[any], 4)
	go func() {
		for {
			select {
			case d := <-out:
				got <- d.Msg
			case <-t.Context().Done():
				return
			}
		}
	}()
	return sendTarget{ch: b, waitFor: recvWithin(got)}
}

// recvWithin returns a waitFor that reads one message from got, or reports false
// after two seconds.
func recvWithin(got chan msgin.Message[any]) func(*testing.T) (msgin.Message[any], bool) {
	return func(t *testing.T) (msgin.Message[any], bool) {
		t.Helper()
		select {
		case m := <-got:
			return m, true
		case <-time.After(2 * time.Second):
			return msgin.Message[any]{}, false
		}
	}
}

// TestSendOnlyCallSitesAcceptEveryChannel is the ADR 0028 capability test: each
// send-only call site must accept a QueueChannel, a PublishSubscribeChannel, and
// a shipped OutboundAdapter — none of which could satisfy the pre-window bundled
// MessageChannel.
func TestSendOnlyCallSitesAcceptEveryChannel(t *testing.T) {
	type targetCase struct {
		name   string
		build  func(t *testing.T) sendTarget
		assert func(t *testing.T, got msgin.Message[any], arrived bool)
	}
	type siteCase struct {
		name   string
		run    func(t *testing.T, target msgin.MessageChannel) error
		assert func(t *testing.T, err error)
	}

	targets := []targetCase{
		{
			name:  "QueueChannel",
			build: newQueueTarget,
			assert: func(t *testing.T, got msgin.Message[any], arrived bool) {
				require.True(t, arrived, "message never reached the QueueChannel")
				assert.Equal(t, capabilityPayload, got.Payload())
			},
		},
		{
			name:  "PublishSubscribeChannel",
			build: newPubSubTarget,
			assert: func(t *testing.T, got msgin.Message[any], arrived bool) {
				require.True(t, arrived, "message never reached the PublishSubscribeChannel")
				assert.Equal(t, capabilityPayload, got.Payload())
			},
		},
		{
			name:  "OutboundAdapter/memory.Broker",
			build: newBrokerTarget,
			assert: func(t *testing.T, got msgin.Message[any], arrived bool) {
				require.True(t, arrived, "message never reached the OutboundAdapter")
				assert.Equal(t, capabilityPayload, got.Payload())
			},
		},
	}

	sites := []siteCase{
		{
			name: "filter discard channel",
			run: func(t *testing.T, target msgin.MessageChannel) error {
				flow := msgin.Chain(routing.Filter(
					func(context.Context, msgin.Message[string]) (bool, error) { return false, nil },
					routing.WithDiscardChannel(target),
				))
				return flow.Handle(t.Context(), msgin.New[any](capabilityPayload))
			},
			assert: func(t *testing.T, err error) { assert.NoError(t, err) },
		},
		{
			name: "router default channel",
			run: func(t *testing.T, target msgin.MessageChannel) error {
				r := routing.NewRouter(
					func(context.Context, msgin.Message[any]) (msgin.MessageChannel, error) { return nil, nil },
					routing.WithDefaultChannel(target),
				)
				return r.Handle(t.Context(), msgin.New[any](capabilityPayload))
			},
			assert: func(t *testing.T, err error) { assert.NoError(t, err) },
		},
		{
			name: "exchange request channel",
			run: func(t *testing.T, target msgin.MessageChannel) error {
				reply := channel.NewDirectChannel()
				ex, err := endpoint.NewChannelExchange(target, reply,
					endpoint.WithReplyTimeout(50*time.Millisecond))
				require.NoError(t, err)
				t.Cleanup(func() { require.NoError(t, ex.Close()) })
				req := msgin.New[any](capabilityPayload).WithHeader(msgin.HeaderCorrelationID, "capability-1")
				_, err = ex.Exchange(t.Context(), req)
				return err
			},
			// No reply is ever produced, so the request is proven delivered by the
			// target's own assertion; the exchange itself times out.
			assert: func(t *testing.T, err error) { assert.ErrorIs(t, err, msgin.ErrReplyTimeout) },
		},
	}

	for _, target := range targets {
		for _, site := range sites {
			t.Run(target.name+"/"+site.name, func(t *testing.T) {
				tgt := target.build(t)
				site.assert(t, site.run(t, tgt.ch))
				got, arrived := tgt.waitFor(t)
				target.assert(t, got, arrived)
			})
		}
	}
}
