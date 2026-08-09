package msghttp_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kartaladev/msgin"
	msghttp "github.com/kartaladev/msgin/adapter/http"
	"github.com/kartaladev/msgin/adapter/memory"
	"github.com/kartaladev/msgin/channel"
)

// capabilityPayload is the marker body every capability case posts. It arrives
// as a []byte payload: DecodeRequest reads the raw body and never decodes it.
const capabilityPayload = "capability-probe"

// capTarget pairs a send-only destination with a way to observe what reached
// it. The msgin.MessageChannel field is the point of the test: before ADR 0028
// only DirectChannel satisfied that interface, so none of these targets could
// be passed to ServeAsync at all (the RED artifact is a compile failure, not a
// FAIL line).
type capTarget struct {
	ch      msgin.MessageChannel
	waitFor func(t *testing.T) (msgin.Message[any], bool)
}

// newCapQueueTarget builds a QueueChannel over an in-memory store; arrival is
// observed by polling the channel (it is a PollingSource, not a subscriber).
func newCapQueueTarget(t *testing.T) capTarget {
	t.Helper()
	store, err := memory.NewQueueStore()
	require.NoError(t, err)
	qc, err := channel.NewQueueChannel(store)
	require.NoError(t, err)
	return capTarget{
		ch: qc,
		waitFor: func(t *testing.T) (msgin.Message[any], bool) {
			t.Helper()
			deadline := time.Now().Add(2 * time.Second)
			for time.Now().Before(deadline) {
				ds, pollErr := qc.Poll(t.Context(), 1)
				require.NoError(t, pollErr)
				if len(ds) == 1 {
					return ds[0].Msg, true
				}
				time.Sleep(time.Millisecond)
			}
			return msgin.Message[any]{}, false
		},
	}
}

// newCapPubSubTarget builds a PublishSubscribeChannel with one recording
// subscriber.
func newCapPubSubTarget(t *testing.T) capTarget {
	t.Helper()
	ps := channel.NewPublishSubscribeChannel()
	got := make(chan msgin.Message[any], 4)
	mustSubscribe(t, ps, msgin.HandlerFunc(func(_ context.Context, m msgin.Message[any]) error {
		got <- m
		return nil
	}))
	return capTarget{ch: ps, waitFor: capRecvWithin(got)}
}

// newCapBrokerTarget builds a shipped OutboundAdapter (memory.Broker). ADR 0028
// §5 makes every OutboundAdapter a legal send-only destination; that widening
// is what this case pins.
func newCapBrokerTarget(t *testing.T) capTarget {
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
	return capTarget{ch: b, waitFor: capRecvWithin(got)}
}

// capRecvWithin returns a waitFor that reads one message from got, or reports
// false after two seconds.
func capRecvWithin(got chan msgin.Message[any]) func(*testing.T) (msgin.Message[any], bool) {
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

// TestServeAsyncTargetAcceptsEveryChannel is this package's half of the ADR
// 0028 capability test (Spec 014 §9.4 row 7): ServeAsync's target parameter
// must accept a QueueChannel, a PublishSubscribeChannel, and a shipped
// OutboundAdapter — none of which could satisfy the pre-window bundled
// MessageChannel. Root's capability_test.go carries the six core positions;
// this file and adapter/http/stdlib's carry the two HTTP ones.
//
// It is a REGRESSION FENCE around the widening, not a red-to-green proof:
// ServeAsync already takes msgin.MessageChannel, so each subtest passes the
// first time it compiles. Its failure mode is a future NARROWING of that
// parameter, which would break the build here.
func TestServeAsyncTargetAcceptsEveryChannel(t *testing.T) {
	type targetCase struct {
		name   string
		build  func(t *testing.T) capTarget
		assert func(t *testing.T, got msgin.Message[any], arrived bool)
	}

	assertDelivered := func(what string) func(*testing.T, msgin.Message[any], bool) {
		return func(t *testing.T, got msgin.Message[any], arrived bool) {
			t.Helper()
			require.True(t, arrived, "request never reached the %s", what)
			assert.Equal(t, []byte(capabilityPayload), got.Payload())
		}
	}

	tests := []targetCase{
		{name: "QueueChannel", build: newCapQueueTarget, assert: assertDelivered("QueueChannel")},
		{name: "PublishSubscribeChannel", build: newCapPubSubTarget, assert: assertDelivered("PublishSubscribeChannel")},
		{name: "OutboundAdapter/memory.Broker", build: newCapBrokerTarget, assert: assertDelivered("OutboundAdapter")},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tgt := tc.build(t)
			cfg, err := msghttp.NewConfig()
			require.NoError(t, err)

			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/capability", strings.NewReader(capabilityPayload))
			msghttp.ServeAsync(rec, req, tgt.ch, cfg)

			require.Equal(t, http.StatusAccepted, rec.Code)
			got, arrived := tgt.waitFor(t)
			tc.assert(t, got, arrived)
		})
	}
}
