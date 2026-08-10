package endpoint_test

// NOTE on table-test skill compliance: TestNewChannelExchange_validation,
// TestChannelExchange_panickingFlow_propagatesAndReclaimsSlot, and
// TestChannelExchange_abandonedArmsReclaimSlot use the mandatory
// assert-closure table form — each folds two or more cases that share an
// identical construct/trigger+assert shape. Every other test below is a
// standalone TestXxx because each exercises a genuinely different
// concurrency/synchronization shape (fake-clock races, cross-goroutine
// delivery, Close/timeout races, panic-unwind draining) — forcing them into
// one table would hide the setup divergence the table-test skill's exception
// clause calls out.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jonboulle/clockwork"
	"github.com/kartaladev/msgin"
	"github.com/kartaladev/msgin/channel"
	"github.com/kartaladev/msgin/endpoint"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
)

// newLoopExchange builds a ChannelExchange over two fresh DirectChannels and
// subscribes a synchronous echo flow onto request (request -> reply), so
// Exchange returns the echoed request as its reply.
func newLoopExchange(t *testing.T, opts ...endpoint.ExchangeOption) (ex *endpoint.ChannelExchange, request, reply msgin.SubscribableChannel) {
	t.Helper()
	request = channel.NewDirectChannel()
	reply = channel.NewDirectChannel()
	ex, err := endpoint.NewChannelExchange(request, reply, opts...)
	require.NoError(t, err)
	mustSubscribe(t, request, msgin.Chain(msgin.To(reply)))
	return ex, request, reply
}

// newBlockingExchange builds a ChannelExchange whose request flow never
// replies: it only signals sinkHit (buffered, cap 1) once invoked. Because a
// DirectChannel runs the flow synchronously inside request.Send, receiving on
// sinkHit proves the waiter is registered and Exchange has reached (or is
// about to reach) its select before the test fires a timeout/cancel/Close.
func newBlockingExchange(t *testing.T, opts ...endpoint.ExchangeOption) (ex *endpoint.ChannelExchange, reply msgin.SubscribableChannel, sinkHit chan struct{}) {
	t.Helper()
	request := channel.NewDirectChannel()
	reply = channel.NewDirectChannel()
	ex, err := endpoint.NewChannelExchange(request, reply, opts...)
	require.NoError(t, err)
	hit := make(chan struct{}, 1)
	mustSubscribe(t, request, msgin.Chain(endpoint.Consume(func(_ context.Context, _ msgin.Message[any]) error {
		hit <- struct{}{}
		return nil
	})))
	return ex, reply, hit
}

// asyncEcho wires request -> a worker goroutine that echoes each request to reply.
// stop() drains and joins the worker (goleak-clean). Because reply.Send runs on
// the worker goroutine, the waiter's select genuinely races deliver.
func asyncEcho(t *testing.T, request msgin.SubscribableChannel, reply msgin.MessageChannel) (stop func()) {
	t.Helper()
	work := make(chan msgin.Message[any], 64)
	done := make(chan struct{})
	if _, err := request.Subscribe(msgin.HandlerFunc(func(_ context.Context, m msgin.Message[any]) error {
		work <- m
		return nil
	})); err != nil {
		t.Fatal(err)
	}
	go func() {
		defer close(done)
		for m := range work {
			_ = reply.Send(context.Background(), m) // m already carries HeaderCorrelationID
		}
	}()
	return func() { close(work); <-done }
}

// stubOutbound is a minimal OutboundAdapter double whose Send outcome (and,
// optionally, observed messages) is controlled by the test: used to exercise
// the sink-error branch of routeUnmatched, which a *DirectChannel sink cannot
// (its Send only fails on "no subscriber").
type stubOutbound struct {
	err  error
	recv chan msgin.Message[any]
}

func (s *stubOutbound) Send(_ context.Context, m msgin.Message[any]) error {
	if s.recv != nil {
		s.recv <- m
	}
	return s.err
}

// mustSubscribe registers h on ch and fails the test if Subscribe errors. Since
// ADR 0028 Subscribe returns (Subscription, error); these call sites do not need
// the handle, and this keeps the original require.NoError assertion intact.
func mustSubscribe(t *testing.T, ch msgin.SubscribableChannel, h msgin.MessageHandler) {
	t.Helper()
	_, err := ch.Subscribe(h)
	require.NoError(t, err)
}

// nilSubChannel is a minimal third-party SubscribableChannel that breaks the
// Subscribe contract: it returns a nil Subscription alongside a nil error.
// SubscribableChannel is public SPI, so this is caller input — the exchange must
// reject it with a typed error at construction rather than nil-deref later in
// Close (which unconditionally calls replySub.Cancel()).
type nilSubChannel struct{}

func (nilSubChannel) Send(context.Context, msgin.Message[any]) error { return nil }

func (nilSubChannel) Subscribe(msgin.MessageHandler) (msgin.Subscription, error) {
	return nil, nil //nolint:nilnil // deliberately contract-breaking test double
}

func TestNewChannelExchange_validation(t *testing.T) {
	direct := channel.NewDirectChannel()
	tests := []struct {
		name    string
		request msgin.MessageChannel
		reply   msgin.SubscribableChannel
		opts    []endpoint.ExchangeOption
		assert  func(t *testing.T, ex *endpoint.ChannelExchange, err error)
	}{
		{
			name: "nil request", request: nil, reply: direct,
			assert: func(t *testing.T, ex *endpoint.ChannelExchange, err error) {
				if !errors.Is(err, msgin.ErrNilChannel) {
					t.Fatalf("want ErrNilChannel, got %v", err)
				}
			},
		},
		{
			name: "nil reply", request: direct, reply: nil,
			assert: func(t *testing.T, ex *endpoint.ChannelExchange, err error) {
				if !errors.Is(err, msgin.ErrNilChannel) {
					t.Fatalf("want ErrNilChannel, got %v", err)
				}
			},
		},
		{
			name: "explicit non-positive timeout", request: channel.NewDirectChannel(), reply: channel.NewDirectChannel(),
			opts: []endpoint.ExchangeOption{endpoint.WithReplyTimeout(0)},
			assert: func(t *testing.T, ex *endpoint.ChannelExchange, err error) {
				if !errors.Is(err, msgin.ErrInvalidReplyTimeout) {
					t.Fatalf("want ErrInvalidReplyTimeout, got %v", err)
				}
			},
		},
		{
			// A reply channel whose Subscribe returns (nil, nil). Before the guard
			// this constructed successfully and panicked on Close.
			name: "reply channel returns a nil subscription", request: direct, reply: nilSubChannel{},
			assert: func(t *testing.T, ex *endpoint.ChannelExchange, err error) {
				if !errors.Is(err, msgin.ErrNilSubscription) {
					t.Fatalf("want ErrNilSubscription, got %v", err)
				}
				if ex != nil {
					t.Fatal("want a nil exchange when construction fails")
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ex, err := endpoint.NewChannelExchange(tt.request, tt.reply, tt.opts...)
			tt.assert(t, ex, err)
		})
	}
}

// noopSubscription is a Subscription whose Cancel does nothing — enough for a
// fake reply channel whose subscription the exchange owns until Close.
type noopSubscription struct{}

func (noopSubscription) Cancel() {}

// noProbeChannel is a third-party SubscribableChannel that deliberately omits
// SingleSubscriber, so the type assertion to msgin.ExclusiveSubscribable fails
// and the accept-unknown arm (ADR 0030 §4) is taken. No in-tree type can drive
// that arm to an ACCEPTED outcome: both production channels implement the probe,
// and nilSubChannel — the one in-tree type that omits it — is rejected 20 lines
// later by the ErrNilSubscription guard.
type noProbeChannel struct{}

func (noProbeChannel) Send(context.Context, msgin.Message[any]) error { return nil }

func (noProbeChannel) Subscribe(msgin.MessageHandler) (msgin.Subscription, error) {
	return noopSubscription{}, nil
}

// countingSharedChannel reports non-exclusive and counts BOTH calls msgin makes
// into it, so a test can observe the guard's ORDER — which no in-tree type can
// show, because neither call leaves an observable trace on a real channel.
//
// subscribes pins that the probe rejected the exchange BEFORE subscribing
// (Spec 014 AC-9): PublishSubscribeChannel's subscriber count is unexported, and
// a Send reaching zero subscribers returns nil by documented design, so neither
// a count nor a send distinguishes "no subscriber" from "one".
//
// probes pins decision D-M2 — WithSharedReplyChannel suppresses the PROBE, not
// merely the rejection. That is a caller-facing guarantee on the option's godoc
// (a caller who opted out because their SingleSubscriber locks or does I/O must
// not pay for it), and reordering the guard to the pre-D-M2 shape
// `if ok { … if !single && !cfg.allowShared { … } }` leaves every other row of
// the table green.
type countingSharedChannel struct {
	subscribes atomic.Int64
	probes     atomic.Int64
}

func (*countingSharedChannel) Send(context.Context, msgin.Message[any]) error { return nil }

func (c *countingSharedChannel) Subscribe(msgin.MessageHandler) (msgin.Subscription, error) {
	c.subscribes.Add(1)
	return noopSubscription{}, nil
}

func (c *countingSharedChannel) SingleSubscriber() bool {
	c.probes.Add(1)
	return false
}

// probePanicLiteral is the distinctive value panickingProbeChannel panics with.
// The panicking row asserts it is present in err.Error(): asserting only
// errors.Is(err, ErrSharedReplyChannel) passes against the implementation that
// discards the recovered value, which is exactly the defect decision D-O2 exists
// to prevent.
const probePanicLiteral = "probe: nil map read in tenantExclusivity[tenant]"

// panickingProbeChannel is GENUINELY EXCLUSIVE — it embeds a *channel.DirectChannel,
// whose second Subscribe is ErrChannelSubscribed — but its probe panics. That
// shape is what exposes a lost diagnosis: the sentinel's message claims the
// channel is not exclusive, which is false here, so the recovered panic has to
// ride in the error for a reader to find the real fault.
type panickingProbeChannel struct{ *channel.DirectChannel }

func (panickingProbeChannel) SingleSubscriber() bool { panic(probePanicLiteral) }

// TestNewChannelExchange_replyExclusivityProbe is the truth table of the
// construction-time exclusivity probe (decision D-J, ADR 0030): four arms —
// probe absent, probe true, probe false, probe false plus the opt-out — plus
// AC-9's ordering row (nothing is subscribed after a rejection) and decision
// D-O/D-O2's panicking-probe row (recovered, failed closed, cause preserved).
func TestNewChannelExchange_replyExclusivityProbe(t *testing.T) {
	counting, optedOut := &countingSharedChannel{}, &countingSharedChannel{}
	tests := []struct {
		name   string
		reply  msgin.SubscribableChannel
		opts   []endpoint.ExchangeOption
		assert func(t *testing.T, ex *endpoint.ChannelExchange, err error)
	}{
		{
			name:  "probe absent: accepted, so the SPI stays open to channels predating it",
			reply: noProbeChannel{},
			assert: func(t *testing.T, ex *endpoint.ChannelExchange, err error) {
				require.NoError(t, err)
				assert.NotNil(t, ex)
			},
		},
		{
			name:  "probe reports exclusive: accepted",
			reply: channel.NewDirectChannel(),
			assert: func(t *testing.T, ex *endpoint.ChannelExchange, err error) {
				require.NoError(t, err)
				assert.NotNil(t, ex)
			},
		},
		{
			name:  "probe reports non-exclusive: ErrSharedReplyChannel",
			reply: channel.NewPublishSubscribeChannel(),
			assert: func(t *testing.T, ex *endpoint.ChannelExchange, err error) {
				require.ErrorIs(t, err, msgin.ErrSharedReplyChannel)
				assert.Nil(t, ex, "want a nil exchange when construction fails")
			},
		},
		{
			name:  "probe reports non-exclusive but WithSharedReplyChannel opts out: accepted",
			reply: channel.NewPublishSubscribeChannel(),
			opts:  []endpoint.ExchangeOption{endpoint.WithSharedReplyChannel()},
			assert: func(t *testing.T, ex *endpoint.ChannelExchange, err error) {
				require.NoError(t, err)
				assert.NotNil(t, ex)
			},
		},
		{
			name:  "a rejected construction leaves no subscription behind",
			reply: counting,
			assert: func(t *testing.T, ex *endpoint.ChannelExchange, err error) {
				require.ErrorIs(t, err, msgin.ErrSharedReplyChannel)
				assert.Nil(t, ex)
				assert.Zero(t, counting.subscribes.Load(),
					"the probe must run BEFORE reply.Subscribe")
			},
		},
		{
			name:  "a panicking probe is recovered, fails closed, and carries its cause",
			reply: panickingProbeChannel{channel.NewDirectChannel()},
			assert: func(t *testing.T, ex *endpoint.ChannelExchange, err error) {
				require.ErrorIs(t, err, msgin.ErrSharedReplyChannel)
				require.ErrorContains(t, err, probePanicLiteral,
					"the recovered panic must survive in the error: this channel IS exclusive, so the "+
						"sentinel's own message is a false diagnosis without it")
				assert.Nil(t, ex)
			},
		},
		{
			name:  "the opt-out suppresses the probe itself, not merely the rejection",
			reply: optedOut,
			opts:  []endpoint.ExchangeOption{endpoint.WithSharedReplyChannel()},
			assert: func(t *testing.T, ex *endpoint.ChannelExchange, err error) {
				require.NoError(t, err)
				assert.NotNil(t, ex)
				assert.Zero(t, optedOut.probes.Load(),
					"WithSharedReplyChannel is tested BEFORE the capability assertion (D-M2), so a "+
						"caller who opted out never pays for a SingleSubscriber that locks or does I/O")
				assert.Equal(t, int64(1), optedOut.subscribes.Load(),
					"the exchange is still built, so it still subscribes exactly once")
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var (
				ex  *endpoint.ChannelExchange
				err error
			)
			require.NotPanics(t, func() {
				ex, err = endpoint.NewChannelExchange(channel.NewDirectChannel(), tc.reply, tc.opts...)
			}, "a probe that panics must be recovered inside the constructor, not propagated out of it")
			if ex != nil {
				t.Cleanup(func() { require.NoError(t, ex.Close()) })
			}
			tc.assert(t, ex, err)
		})
	}
}

func TestChannelExchange_nilOptionGuards(t *testing.T) {
	// WithExchangeClock(nil)/WithExchangeLogger(nil) must be no-ops (default
	// stays in place), not a nil-panic on caller input.
	ex, err := endpoint.NewChannelExchange(channel.NewDirectChannel(), channel.NewDirectChannel(),
		endpoint.WithExchangeClock(nil),
		endpoint.WithExchangeLogger(nil),
	)
	require.NoError(t, err)
	require.NotNil(t, ex)
}

func TestChannelExchange_roundTrip(t *testing.T) {
	ex, _, _ := newLoopExchange(t)
	req := msgin.New[any]("hello", msgin.WithHeaders(map[string]any{msgin.HeaderCorrelationID: "corr-1"}))

	got, err := ex.Exchange(t.Context(), req)

	require.NoError(t, err)
	assert.Equal(t, "hello", got.Payload())
	corrID, ok := got.Headers().String(msgin.HeaderCorrelationID)
	require.True(t, ok)
	assert.Equal(t, "corr-1", corrID)
}

func TestChannelExchange_replyTimeout(t *testing.T) {
	defer goleak.VerifyNone(t)
	fakeClock := clockwork.NewFakeClock()
	ex, _, sinkHit := newBlockingExchange(t, endpoint.WithExchangeClock(fakeClock))
	req := msgin.New[any]("payload", msgin.WithHeaders(map[string]any{msgin.HeaderCorrelationID: "corr-timeout"}))

	errCh := make(chan error, 1)
	go func() {
		_, err := ex.Exchange(t.Context(), req)
		errCh <- err
	}()
	<-sinkHit
	require.NoError(t, fakeClock.BlockUntilContext(t.Context(), 1))
	fakeClock.Advance(30 * time.Second)

	err := <-errCh
	assert.ErrorIs(t, err, msgin.ErrReplyTimeout)
}

func TestChannelExchange_ctxCancel(t *testing.T) {
	defer goleak.VerifyNone(t)
	ex, _, sinkHit := newBlockingExchange(t)
	ctx, cancel := context.WithCancel(t.Context())
	req := msgin.New[any]("payload", msgin.WithHeaders(map[string]any{msgin.HeaderCorrelationID: "corr-cancel"}))

	errCh := make(chan error, 1)
	go func() {
		_, err := ex.Exchange(ctx, req)
		errCh <- err
	}()
	<-sinkHit
	cancel()

	err := <-errCh
	assert.ErrorIs(t, err, context.Canceled)
}

func TestChannelExchange_sendError(t *testing.T) {
	// A plain reply.Send returning nil is NOT proof of "no leak": receiver()
	// returns nil whether the late reply hit deliver() (a leaked waiter
	// silently absorbing it into an unread buffered channel) or fell through
	// to routeUnmatched. Wire an unmatched sink so only the genuinely-unmatched
	// path can make the late reply observable — proving deregister actually
	// removed the slot before any deliver, not just that Send didn't error.
	sink := channel.NewDirectChannel()
	received := make(chan msgin.Message[any], 1)
	mustSubscribe(t, sink, msgin.HandlerFunc(func(_ context.Context, m msgin.Message[any]) error {
		received <- m
		return nil
	}))
	request := channel.NewDirectChannel() // no subscriber -> Send fails with ErrNoSubscriber
	reply := channel.NewDirectChannel()
	ex, err := endpoint.NewChannelExchange(request, reply, endpoint.WithUnmatchedReplySink(sink))
	require.NoError(t, err)
	req := msgin.New[any]("payload", msgin.WithHeaders(map[string]any{msgin.HeaderCorrelationID: "corr-send-err"}))

	_, err = ex.Exchange(t.Context(), req)
	assert.ErrorIs(t, err, msgin.ErrNoSubscriber)

	// No waiter leak: deregister removed the slot before any deliver, so a
	// later reply sharing the same correlation id is unmatched and observably
	// lands in the sink, not routed to a stale waiter.
	late := msgin.New[any]("late", msgin.WithHeaders(map[string]any{msgin.HeaderCorrelationID: "corr-send-err"}))
	assert.NoError(t, reply.Send(t.Context(), late))
	select {
	case got := <-received:
		assert.Equal(t, "late", got.Payload())
	default:
		t.Fatal("expected the late reply to land in the unmatched sink, proving no waiter leak")
	}
}

func TestChannelExchange_closed_newExchangeAfterClose(t *testing.T) {
	ex, _, _ := newLoopExchange(t)
	require.NoError(t, ex.Close())
	req := msgin.New[any]("x", msgin.WithHeaders(map[string]any{msgin.HeaderCorrelationID: "corr-closed-new"}))

	_, err := ex.Exchange(t.Context(), req)

	assert.ErrorIs(t, err, msgin.ErrGatewayClosed)
}

func TestChannelExchange_closed_pendingWaiterUnblocked(t *testing.T) {
	defer goleak.VerifyNone(t)
	ex, _, sinkHit := newBlockingExchange(t)
	req := msgin.New[any]("x", msgin.WithHeaders(map[string]any{msgin.HeaderCorrelationID: "corr-closed-pending"}))

	errCh := make(chan error, 1)
	go func() {
		_, err := ex.Exchange(t.Context(), req)
		errCh <- err
	}()
	<-sinkHit
	require.NoError(t, ex.Close())

	err := <-errCh
	assert.ErrorIs(t, err, msgin.ErrGatewayClosed)
}

func TestChannelExchange_unmatchedReply_drop(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	_, _, reply := newLoopExchange(t, endpoint.WithExchangeLogger(logger))
	orphan := msgin.New[any]("orphan", msgin.WithHeaders(map[string]any{msgin.HeaderCorrelationID: "no-such-id"}))

	err := reply.Send(t.Context(), orphan)

	assert.NoError(t, err)
	assert.Contains(t, logs.String(), "msgin: dropping unmatched gateway reply")
}

func TestChannelExchange_unmatchedReply_sink(t *testing.T) {
	sink := channel.NewDirectChannel()
	received := make(chan msgin.Message[any], 1)
	mustSubscribe(t, sink, msgin.HandlerFunc(func(_ context.Context, m msgin.Message[any]) error {
		received <- m
		return nil
	}))
	_, _, reply := newLoopExchange(t, endpoint.WithUnmatchedReplySink(sink))
	orphan := msgin.New[any]("orphan", msgin.WithHeaders(map[string]any{msgin.HeaderCorrelationID: "no-such-id-2"}))

	err := reply.Send(t.Context(), orphan)

	assert.NoError(t, err)
	select {
	case got := <-received:
		assert.Equal(t, "orphan", got.Payload())
	default:
		t.Fatal("expected sink to receive the unmatched reply")
	}
}

func TestChannelExchange_unmatchedReply_sinkError(t *testing.T) {
	recv := make(chan msgin.Message[any], 1)
	sink := &stubOutbound{err: errors.New("sink boom"), recv: recv}
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	_, _, reply := newLoopExchange(t, endpoint.WithUnmatchedReplySink(sink), endpoint.WithExchangeLogger(logger))
	orphan := msgin.New[any]("orphan", msgin.WithHeaders(map[string]any{msgin.HeaderCorrelationID: "no-such-id-3"}))

	err := reply.Send(t.Context(), orphan)

	// A sink error is logged, never propagated to the reply sender.
	assert.NoError(t, err)
	select {
	case <-recv:
	default:
		t.Fatal("expected the sink's Send to be invoked")
	}
	assert.Contains(t, logs.String(), "msgin: unmatched-reply sink failed")
}

func TestNewChannelExchange_replySubscribeError(t *testing.T) {
	request := channel.NewDirectChannel()
	reply := channel.NewDirectChannel()
	// Pre-subscribe reply so NewChannelExchange's own Subscribe collides.
	mustSubscribe(t, reply, msgin.HandlerFunc(func(context.Context, msgin.Message[any]) error { return nil }))

	ex, err := endpoint.NewChannelExchange(request, reply)

	assert.Nil(t, ex)
	assert.ErrorIs(t, err, msgin.ErrChannelSubscribed)
}

func TestChannelExchange_closeIdempotent(t *testing.T) {
	ex, _, _ := newLoopExchange(t)
	require.NoError(t, ex.Close())

	err := ex.Close()

	assert.NoError(t, err)
}

// TestChannelExchange_closeCancelsReplySubscription pins ADR 0028 §6.1: the
// exchange OWNS the reply subscription it created, so Close releases it. Without
// this the widened reply contract would leak a subscription that did not exist
// while Subscribe returned only an error.
func TestChannelExchange_closeCancelsReplySubscription(t *testing.T) {
	request := channel.NewDirectChannel()
	reply := channel.NewDirectChannel()
	ex, err := endpoint.NewChannelExchange(request, reply)
	require.NoError(t, err)

	// While open the exchange holds the channel's single subscriber slot.
	_, err = reply.Subscribe(msgin.HandlerFunc(func(context.Context, msgin.Message[any]) error { return nil }))
	require.ErrorIs(t, err, msgin.ErrChannelSubscribed)
	orphan := msgin.New[any]("x", msgin.WithHeaders(map[string]any{msgin.HeaderCorrelationID: "corr-close-sub"}))
	require.NoError(t, reply.Send(t.Context(), orphan), "the receiver is subscribed and absorbs an unmatched reply")

	require.NoError(t, ex.Close())

	// Cancelled: the slot is free and the channel reports no subscriber. A reply
	// arriving after Close is the channel's concern, not the exchange's — it is
	// NOT routed to WithUnmatchedReplySink.
	assert.ErrorIs(t, reply.Send(t.Context(), orphan), msgin.ErrNoSubscriber)
	sub, err := reply.Subscribe(msgin.HandlerFunc(func(context.Context, msgin.Message[any]) error { return nil }))
	require.NoError(t, err)
	assert.NotNil(t, sub, "Close must release the reply channel for reuse")
}

// TestChannelExchange_sharedPubSubReplyChannel pins the trade-off ADR 0028 §6
// accepts: widening reply to SubscribableChannel makes "two exchanges over one
// pub-sub reply channel" expressible, and every reply then fans out to BOTH
// receivers — the non-owner handing a full copy to its unmatched-reply sink.
// Since ADR 0030 that costs an explicit endpoint.WithSharedReplyChannel() on
// both constructions: by default the probe rejects a plain pub-sub reply channel
// with msgin.ErrSharedReplyChannel, and the option is what asks for the fan-out.
// channel.WithSingleSubscriber remains the channel-side guard, which the opt-out
// cannot override — it suppresses msgin's probe, not the channel's own Subscribe.
func TestChannelExchange_sharedPubSubReplyChannel(t *testing.T) {
	tests := []struct {
		name   string
		opts   []channel.PubSubOption
		assert func(t *testing.T, secondErr error, ownReply msgin.Message[any], ownErr error, crossDelivered chan msgin.Message[any])
	}{
		{
			name: "default fan-out, opted into: the second exchange is built and sees the first's reply",
			assert: func(t *testing.T, secondErr error, ownReply msgin.Message[any], ownErr error, crossDelivered chan msgin.Message[any]) {
				require.NoError(t, secondErr,
					"WithSharedReplyChannel suppresses the probe, so sharing a plain pub-sub reply channel is accepted")
				require.NoError(t, ownErr)
				assert.Equal(t, "shared", ownReply.Payload(), "the owning exchange still gets its reply")
				select {
				case leaked := <-crossDelivered:
					assert.Equal(t, "shared", leaked.Payload(),
						"the documented consequence: a full copy reaches the other exchange's sink")
				default:
					t.Fatal("expected the reply to fan out to the second exchange's unmatched sink")
				}
			},
		},
		{
			name: "WithSingleSubscriber: the second exchange is rejected at construction",
			opts: []channel.PubSubOption{channel.WithSingleSubscriber()},
			assert: func(t *testing.T, secondErr error, _ msgin.Message[any], _ error, _ chan msgin.Message[any]) {
				assert.ErrorIs(t, secondErr, msgin.ErrChannelSubscribed)
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			reply := channel.NewPublishSubscribeChannel(tc.opts...)
			requestA := channel.NewDirectChannel()
			// WithSharedReplyChannel is applied UNCONDITIONALLY to both
			// constructions. It is the opt-out D-J requires for the default
			// fan-out case, and it is inert for the WithSingleSubscriber case:
			// there the option suppresses the probe outright and Subscribe still
			// rejects exB with ErrChannelSubscribed, exactly as that case asserts.
			exA, err := endpoint.NewChannelExchange(requestA, reply, endpoint.WithSharedReplyChannel())
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, exA.Close()) })
			mustSubscribe(t, requestA, msgin.Chain(msgin.To(reply)))

			crossDelivered := make(chan msgin.Message[any], 1)
			sink := &stubOutbound{recv: crossDelivered}
			exB, secondErr := endpoint.NewChannelExchange(
				channel.NewDirectChannel(), reply,
				endpoint.WithUnmatchedReplySink(sink), endpoint.WithSharedReplyChannel(),
			)
			if secondErr != nil {
				tc.assert(t, secondErr, msgin.Message[any]{}, nil, crossDelivered)
				return
			}
			t.Cleanup(func() { require.NoError(t, exB.Close()) })

			req := msgin.New[any]("shared", msgin.WithHeaders(map[string]any{msgin.HeaderCorrelationID: "corr-shared"}))
			ownReply, ownErr := exA.Exchange(t.Context(), req)
			tc.assert(t, secondErr, ownReply, ownErr, crossDelivered)
		})
	}
}

func TestChannelExchange_emptyCorrelation(t *testing.T) {
	// request has no subscriber: if Exchange attempted to send despite the
	// missing correlation id, we'd observe ErrNoSubscriber instead — proving
	// the empty-correlation guard short-circuits before any send (audit G1).
	request := channel.NewDirectChannel()
	reply := channel.NewDirectChannel()
	ex, err := endpoint.NewChannelExchange(request, reply)
	require.NoError(t, err)
	req := msgin.New[any]("no-corr")

	_, err = ex.Exchange(t.Context(), req)

	assert.ErrorIs(t, err, msgin.ErrNoCorrelation)
}

func TestChannelExchange_duplicateCorrelation(t *testing.T) {
	defer goleak.VerifyNone(t)
	fakeClock := clockwork.NewFakeClock()
	ex, _, sinkHit := newBlockingExchange(t, endpoint.WithExchangeClock(fakeClock))
	first := msgin.New[any]("first", msgin.WithHeaders(map[string]any{msgin.HeaderCorrelationID: "dup-id"}))

	firstErrCh := make(chan error, 1)
	go func() {
		_, err := ex.Exchange(t.Context(), first)
		firstErrCh <- err
	}()
	<-sinkHit

	second := msgin.New[any]("second", msgin.WithHeaders(map[string]any{msgin.HeaderCorrelationID: "dup-id"}))
	_, err := ex.Exchange(t.Context(), second)
	require.ErrorIs(t, err, msgin.ErrDuplicateCorrelation)

	// The first request must still complete/time out normally: the failed
	// duplicate registration must not have deleted the first's slot.
	require.NoError(t, fakeClock.BlockUntilContext(t.Context(), 1))
	fakeClock.Advance(30 * time.Second)
	firstErr := <-firstErrCh
	assert.ErrorIs(t, firstErr, msgin.ErrReplyTimeout)
}

// TestChannelExchange_asyncRoundTrip is the primary cross-goroutine coverage
// (audit G2): reply.Send runs on a worker goroutine distinct from the waiter's,
// so the waiter's select genuinely races deliver.
func TestChannelExchange_asyncRoundTrip(t *testing.T) {
	defer goleak.VerifyNone(t)
	request := channel.NewDirectChannel()
	reply := channel.NewDirectChannel()
	ex, err := endpoint.NewChannelExchange(request, reply)
	require.NoError(t, err)
	stop := asyncEcho(t, request, reply)
	defer stop()
	req := msgin.New[any]("async-payload", msgin.WithHeaders(map[string]any{msgin.HeaderCorrelationID: "async-1"}))

	got, err := ex.Exchange(t.Context(), req)

	require.NoError(t, err)
	assert.Equal(t, "async-payload", got.Payload())
}

// TestChannelExchange_concurrentRequests_race is the primary proof of the
// concurrency claim: N=50 concurrent Exchange calls, each with a distinct
// correlation id, over the async worker. Run under -race.
func TestChannelExchange_concurrentRequests_race(t *testing.T) {
	defer goleak.VerifyNone(t)
	request := channel.NewDirectChannel()
	reply := channel.NewDirectChannel()
	ex, err := endpoint.NewChannelExchange(request, reply)
	require.NoError(t, err)
	stop := asyncEcho(t, request, reply)
	defer stop()

	const n = 50
	var wg sync.WaitGroup
	wg.Add(n)
	for i := range n {
		go func(i int) {
			defer wg.Done()
			id := strconv.Itoa(i)
			payload := "payload-" + id
			req := msgin.New[any](payload, msgin.WithHeaders(map[string]any{msgin.HeaderCorrelationID: id}))

			got, err := ex.Exchange(t.Context(), req)

			assert.NoError(t, err)
			assert.Equal(t, payload, got.Payload())
			gotID, ok := got.Headers().String(msgin.HeaderCorrelationID)
			assert.True(t, ok)
			assert.Equal(t, id, gotID)
		}(i)
	}
	wg.Wait()
}

// TestChannelExchange_timeoutRacesDelivery (audit G4) races a worker's
// reply.Send against the fake clock firing the reply timeout, repeated to
// exercise both outcomes. Either outcome must be safe: a returned reply, or
// ErrReplyTimeout with the raced-in reply drained to the unmatched sink
// (giveUp's ok==true arm) rather than vanishing.
func TestChannelExchange_timeoutRacesDelivery(t *testing.T) {
	defer goleak.VerifyNone(t)
	const iterations = 30
	for i := range iterations {
		fakeClock := clockwork.NewFakeClock()
		sinkRecv := make(chan msgin.Message[any], 1)
		sink := channel.NewDirectChannel()
		mustSubscribe(t, sink, msgin.HandlerFunc(func(_ context.Context, m msgin.Message[any]) error {
			sinkRecv <- m
			return nil
		}))

		request := channel.NewDirectChannel()
		reply := channel.NewDirectChannel()
		ex, err := endpoint.NewChannelExchange(request, reply,
			endpoint.WithExchangeClock(fakeClock),
			endpoint.WithUnmatchedReplySink(sink),
		)
		require.NoError(t, err)

		work := make(chan msgin.Message[any], 1)
		mustSubscribe(t, request, msgin.HandlerFunc(func(_ context.Context, m msgin.Message[any]) error {
			work <- m
			return nil
		}))

		id := "race-" + strconv.Itoa(i)
		req := msgin.New[any]("race-payload", msgin.WithHeaders(map[string]any{msgin.HeaderCorrelationID: id}))

		type result struct {
			msg msgin.Message[any]
			err error
		}
		resultCh := make(chan result, 1)
		go func() {
			got, err := ex.Exchange(t.Context(), req)
			resultCh <- result{got, err}
		}()

		require.NoError(t, fakeClock.BlockUntilContext(t.Context(), 1)) // the reply timer is registered
		m := <-work                                                     // the request reached the (non-replying-by-default) sink

		// Start the delivery and the clock advance from a shared barrier so
		// neither gets a head start: which one the runtime schedules/lands
		// first is what decides the outcome, genuinely racing giveUp.
		start := make(chan struct{})
		deliverDone := make(chan struct{})
		go func() {
			<-start
			defer close(deliverDone)
			_ = reply.Send(context.Background(), m)
		}()
		advanceDone := make(chan struct{})
		go func() {
			<-start
			defer close(advanceDone)
			fakeClock.Advance(30 * time.Second)
		}()
		close(start)
		<-advanceDone

		res := <-resultCh
		<-deliverDone

		switch {
		case res.err == nil:
			assert.Equal(t, "race-payload", res.msg.Payload())
		case errors.Is(res.err, msgin.ErrReplyTimeout):
			select {
			case sunk := <-sinkRecv:
				assert.Equal(t, "race-payload", sunk.Payload())
			case <-time.After(2 * time.Second):
				t.Fatal("expected the raced-in reply to land in the unmatched sink")
			}
		default:
			t.Fatalf("iteration %d: unexpected error: %v", i, res.err)
		}
	}
}

// TestChannelExchange_closeRacesGiveUp (audit N2) covers giveUp's ok==false
// drain arm: closeAll races the timeout firing, so deregister can find the
// slot already gone (closed by closeAll, not claimed by deliver). No panic,
// no leak, and — because the slot was closed rather than delivered — nothing
// reaches the unmatched sink.
func TestChannelExchange_closeRacesGiveUp(t *testing.T) {
	defer goleak.VerifyNone(t)
	fakeClock := clockwork.NewFakeClock()
	sinkRecv := make(chan msgin.Message[any], 1)
	sink := channel.NewDirectChannel()
	mustSubscribe(t, sink, msgin.HandlerFunc(func(_ context.Context, m msgin.Message[any]) error {
		sinkRecv <- m
		return nil
	}))

	ex, _, sinkHit := newBlockingExchange(t, endpoint.WithExchangeClock(fakeClock), endpoint.WithUnmatchedReplySink(sink))
	req := msgin.New[any]("payload", msgin.WithHeaders(map[string]any{msgin.HeaderCorrelationID: "close-race"}))

	errCh := make(chan error, 1)
	go func() {
		_, err := ex.Exchange(t.Context(), req)
		errCh <- err
	}()
	<-sinkHit
	require.NoError(t, fakeClock.BlockUntilContext(t.Context(), 1))

	closeDone := make(chan struct{})
	go func() {
		defer close(closeDone)
		_ = ex.Close()
	}()
	fakeClock.Advance(30 * time.Second)
	<-closeDone

	err := <-errCh
	if !errors.Is(err, msgin.ErrGatewayClosed) && !errors.Is(err, msgin.ErrReplyTimeout) {
		t.Fatalf("want ErrGatewayClosed or ErrReplyTimeout, got %v", err)
	}

	select {
	case <-sinkRecv:
		t.Fatal("no reply should reach the sink: the slot was closed, never delivered")
	default:
	}
}

// Spec 012 §5.1 / §6 case 6 (audit H-1): with two callers reusing one
// correlation id and replies delivered from another goroutine, a delete-by-id
// deregister can (a) delete the OTHER caller's slot and return true, dropping
// its own committed reply silently, and (b) orphan a slot so its owner's giveUp
// blocks on <-slot forever — unreachable by deliver (not in the map) and by
// closeAll (which iterates the map). Identity-checked deregister closes both.
//
// The window is a preemption between deliver's delete and its send, so this
// stresses rather than forces it. Two detectors, because the hang half is only
// probabilistically reachable: reply ACCOUNTING catches the silent drop
// deterministically whenever the window is hit, and the outer budget catches
// the hang. Bounded throughout: a regression must fail here, never wedge CI.
func TestChannelExchange_reusedIDConcurrentAbandon_neverHangs(t *testing.T) {
	defer goleak.VerifyNone(t)

	const (
		iterations = 200
		id         = "corr-reused-concurrent"
		budget     = 30 * time.Second
	)

	// Nothing inside the loop may call require/t.Fatal: t.FailNow outside the
	// test goroutine Goexits the worker, abandoning in-flight state and turning
	// the real failure into a goleak storm. Record and return instead.
	failures := make(chan string, 1)
	fail := func(format string, args ...any) {
		select {
		case failures <- fmt.Sprintf(format, args...):
		default:
		}
	}

	var totalSent atomic.Int64

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < iterations; i++ {
			var sunk atomic.Int64
			sink := channel.NewDirectChannel()
			if _, err := sink.Subscribe(msgin.HandlerFunc(func(_ context.Context, _ msgin.Message[any]) error {
				sunk.Add(1)
				return nil
			})); err != nil {
				fail("iteration %d: sink subscribe: %v", i, err)
				return
			}

			request := channel.NewDirectChannel()
			reply := channel.NewDirectChannel()
			ex, err := endpoint.NewChannelExchange(request, reply, endpoint.WithUnmatchedReplySink(sink))
			if err != nil {
				fail("iteration %d: new exchange: %v", i, err)
				return
			}

			// The flow hands the reply to a worker goroutine, so deliver races
			// the waiter's abandonment rather than running inline.
			var (
				workers sync.WaitGroup
				sent    atomic.Int64
			)
			if _, err := request.Subscribe(msgin.Chain(endpoint.Consume(func(_ context.Context, m msgin.Message[any]) error {
				workers.Add(1)
				go func() {
					defer workers.Done()
					sent.Add(1)
					totalSent.Add(1)
					_ = reply.Send(context.WithoutCancel(t.Context()), m)
				}()
				return nil
			}))); err != nil {
				fail("iteration %d: request subscribe: %v", i, err)
				return
			}

			// Two callers, SAME id, both abandoning via ctx cancel. Whichever
			// registers second only gets in once the first's slot has left the
			// map — precisely the reuse window.
			var (
				callers  sync.WaitGroup
				returned atomic.Int64
			)
			for c := 0; c < 2; c++ {
				callers.Add(1)
				go func() {
					defer callers.Done()
					ctx, cancel := context.WithCancel(t.Context())
					defer cancel()

					// Race the cancel against the exchange, but JOIN it so the
					// final iteration cannot leave a straggler for goleak.
					var canceller sync.WaitGroup
					canceller.Add(1)
					go func() {
						defer canceller.Done()
						cancel()
					}()

					req := msgin.New[any]("payload", msgin.WithHeaders(map[string]any{msgin.HeaderCorrelationID: id}))
					// Every error here is legitimate (ctx.Err,
					// ErrDuplicateCorrelation). What must hold is that this
					// RETURNS AT ALL — a hang is the H-1 regression — and that
					// a delivered reply is accounted for below.
					if _, err := ex.Exchange(ctx, req); err == nil {
						returned.Add(1)
					}
					canceller.Wait()
				}()
			}
			callers.Wait()
			workers.Wait()

			// H-1's SILENT-DROP half: every reply the flow produced was either
			// returned to its caller or routed to the unmatched sink. A
			// delete-by-id deregister drops one on the floor here — a direct
			// violation of ADR 0022 §2's G4 guarantee.
			if got, want := returned.Load()+sunk.Load(), sent.Load(); got != want {
				fail("iteration %d: %d replies accounted for but %d were sent — a committed reply was dropped (Spec 012 §5.1)", i, got, want)
				return
			}
			if err := ex.Close(); err != nil {
				fail("iteration %d: close: %v", i, err)
				return
			}
		}
	}()

	select {
	case <-done:
	case <-time.After(budget):
		t.Fatal("a caller blocked forever in giveUp: deregister deleted another caller's slot and orphaned it (Spec 012 §5.1)")
	}
	select {
	case msg := <-failures:
		t.Fatal(msg)
	default:
	}
	require.Positive(t, totalSent.Load(), "no iteration produced a reply — the accounting assertion was vacuous")
}

// scriptedChannel is a MessageChannel whose Send is supplied by the test, so a
// test can drive the exact ordering of register/deliver/give-up that a real
// DirectChannel leaves to the scheduler. Subscribe is a no-op: NewChannelExchange
// only subscribes onto the REPLY channel, never the request channel.
type scriptedChannel struct {
	send func(ctx context.Context, msg msgin.Message[any]) error
}

func (c *scriptedChannel) Send(ctx context.Context, msg msgin.Message[any]) error {
	return c.send(ctx, msg)
}

// Spec 012 §5.1 / ADR 0022 Addendum A2, deterministic counterpart to
// TestChannelExchange_reusedIDConcurrentAbandon_neverHangs: it forces
// deregister's `ok && s != slot` arm through the exported API alone, with no
// reliance on a scheduler preemption.
//
// The reuse window does NOT require a preemption inside deliver — only that
// deliver COMPLETE, a second register land under the same id, and the first
// caller then reach giveUp. Exchange's send-error arm reaches giveUp with no
// select race at all, so scripting the request channel makes the whole ordering
// deterministic:
//
//  1. A registers, then A's Send delivers the reply inline — deliver removes A's
//     map entry and commits the reply to A's cap-1 slot.
//  2. B registers the now-free id (getting a DIFFERENT slot) and parks inside its
//     own Send, so B's slot is still in the map.
//  3. A's Send returns an error, so A gives up: deregister finds B's slot under
//     A's id. Identity-checked, it returns false and A drains its own committed
//     reply to the unmatched sink. Delete-by-id would instead delete B's slot,
//     return true, drop A's reply silently, and orphan B's slot forever.
func TestChannelExchange_reusedIDAbandon_drainsOwnReply(t *testing.T) {
	defer goleak.VerifyNone(t)

	const (
		id       = "corr-reused-scripted"
		joinWait = 10 * time.Second
	)

	testCtx := t.Context()

	var sunk atomic.Int64
	sink := channel.NewDirectChannel()
	mustSubscribe(t, sink, msgin.HandlerFunc(func(_ context.Context, _ msgin.Message[any]) error {
		sunk.Add(1)
		return nil
	}))

	reply := channel.NewDirectChannel()
	request := &scriptedChannel{}

	ex, err := endpoint.NewChannelExchange(request, reply, endpoint.WithUnmatchedReplySink(sink))
	require.NoError(t, err)

	errSend := errors.New("scripted request-channel failure")

	var (
		calls        atomic.Int32
		replySendErr atomic.Value // error from step 1's inline reply delivery
		timedOut     atomic.Bool  // a bounded wait expired; asserted on the test goroutine
		bJoined      atomic.Bool  // B's result was already consumed by the main path
		bRegistered  = make(chan struct{})
		releaseB     = make(chan struct{})
		bDone        = make(chan error, 1) // buffered: B never blocks on the handoff
	)
	releaseOnce := sync.OnceFunc(func() { close(releaseB) })

	// Unpark and join B on every exit path (including a failed assertion), before
	// goleak runs: deferred funcs are LIFO, and goleak.VerifyNone was deferred first.
	defer func() {
		releaseOnce()
		if bJoined.Load() {
			return
		}
		select {
		case <-bDone:
		case <-time.After(joinWait):
			timedOut.Store(true)
		}
	}()

	request.send = func(ctx context.Context, msg msgin.Message[any]) error {
		if calls.Add(1) != 1 { // caller B: park with its slot still registered
			close(bRegistered)
			select {
			case <-releaseB:
			case <-time.After(joinWait):
				timedOut.Store(true)
			}
			return errSend
		}

		// Caller A, step 1: deliver the reply inline. deliver removes A's map
		// entry and commits the reply into A's slot. WithoutCancel so the
		// delivery does not depend on the caller ctx.
		if err := reply.Send(context.WithoutCancel(ctx), msg); err != nil {
			replySendErr.Store(err)
			return errSend
		}

		// Step 2: B registers the (now free) id and parks inside its own Send.
		go func() {
			_, err := ex.Exchange(testCtx, msg)
			bDone <- err
		}()
		select {
		case <-bRegistered:
		case <-time.After(joinWait):
			timedOut.Store(true)
		}

		// Step 3: A abandons via the send-error arm -> giveUp -> deregister sees
		// B's slot under A's id.
		return errSend
	}

	req := msgin.New[any]("payload", msgin.WithHeaders(map[string]any{msgin.HeaderCorrelationID: id}))
	_, aErr := ex.Exchange(testCtx, req)

	require.Nil(t, replySendErr.Load(), "step 1's inline reply delivery must succeed")
	require.False(t, timedOut.Load(), "a bounded wait expired: the scripted ordering did not complete")
	require.ErrorIs(t, aErr, errSend)

	// THE load-bearing assertion. Identity-checked deregister returns false, so A
	// drains its own committed reply to the unmatched sink. Delete-by-id returns
	// true and drops it (and orphans B's slot).
	require.Equal(t, int64(1), sunk.Load(), "A's committed reply must reach the unmatched sink")

	// B's slot was never orphaned: releasing it lets B settle normally.
	releaseOnce()
	select {
	case bErr := <-bDone:
		bJoined.Store(true)
		require.ErrorIs(t, bErr, errSend)
	case <-time.After(joinWait):
		t.Fatal("caller B never returned: its slot was orphaned by A's deregister (Spec 012 §5.1)")
	}
	require.NoError(t, ex.Close())
}

// panicExchange builds a ChannelExchange whose request flow panics with
// panicVal. Because a DirectChannel runs its subscriber chain synchronously on
// the caller's goroutine, the panic unwinds out of request.Send inside
// Exchange — the exact defect path of Spec 012 §1. Its only caller never
// replies before panicking, so it has no use for the reply channel and does
// not return one.
func panicExchange(t *testing.T, panicVal any, opts ...endpoint.ExchangeOption) *endpoint.ChannelExchange {
	t.Helper()
	request := channel.NewDirectChannel()
	reply := channel.NewDirectChannel()
	ex, err := endpoint.NewChannelExchange(request, reply, opts...)
	require.NoError(t, err)
	mustSubscribe(t, request, msgin.Chain(endpoint.Consume(func(_ context.Context, _ msgin.Message[any]) error {
		panic(panicVal)
	})))
	return ex
}

// exchangeRecoveringPanic calls ex.Exchange and returns the recovered panic
// value (nil if it did not panic) together with Exchange's returned error, so
// a test can assert on either without the recover happening inside library
// code. err is only meaningful when recovered is nil (no panic occurred): the
// reclamation probes below drive Exchange a second time on a reused
// correlation id, and if the fix under test regressed, that second call would
// fail registration with ErrDuplicateCorrelation instead of reaching the
// panicking flow at all — err carries that precise cause rather than leaving
// the failure as a confusing "no panic".
func exchangeRecoveringPanic(t *testing.T, ex *endpoint.ChannelExchange, req msgin.Message[any]) (recovered any, err error) {
	t.Helper()
	defer func() { recovered = recover() }()
	_, err = ex.Exchange(t.Context(), req)
	return recovered, err
}

// Spec 012 §6 cases 1 & 2: a panicking flow handler must propagate its panic
// UNCHANGED (no recover/re-panic laundering in the library) and must not leave
// the correlation id registered. The reclamation probe is ErrDuplicateCorrelation:
// replyCorrelator is unexported, so id reuse is the blackbox observable.
func TestChannelExchange_panickingFlow_propagatesAndReclaimsSlot(t *testing.T) {
	defer goleak.VerifyNone(t)

	tests := []struct {
		name     string
		panicVal any
		assert   func(t *testing.T, recovered any)
	}{
		{
			name:     "string panic value propagates identically",
			panicVal: "boom",
			assert: func(t *testing.T, recovered any) {
				assert.Equal(t, "boom", recovered)
			},
		},
		{
			name:     "error panic value propagates as the same error instance",
			panicVal: errors.New("handler exploded"),
			assert: func(t *testing.T, recovered any) {
				err, ok := recovered.(error)
				require.True(t, ok, "expected the recovered value to still be an error, got %T", recovered)
				assert.Equal(t, "handler exploded", err.Error())
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeClock := clockwork.NewFakeClock()
			ex := panicExchange(t, tt.panicVal, endpoint.WithExchangeClock(fakeClock))
			const id = "corr-panic"
			req := msgin.New[any]("payload", msgin.WithHeaders(map[string]any{msgin.HeaderCorrelationID: id}))

			recovered, _ := exchangeRecoveringPanic(t, ex, req)

			require.NotNil(t, recovered, "Exchange must not swallow the handler panic")
			tt.assert(t, recovered)

			// The reclamation probe (Spec 012 §6 case 2): the slot must be gone,
			// so REUSING the id must get past register(). It panics again (same
			// flow) rather than failing with ErrDuplicateCorrelation — which is
			// exactly the proof. Capture the error too, so a leaked slot fails
			// with the precise cause rather than a confusing "no panic".
			second, secondErr := exchangeRecoveringPanic(t, ex, req)
			require.NotErrorIs(t, secondErr, msgin.ErrDuplicateCorrelation,
				"the panicking first request leaked its correlator slot — Spec 012 §1")
			require.NotNil(t, second, "the reused correlation id must reach the flow again, not fail registration")
		})
	}
}

// Spec 012 §6 case 4: the ctx-cancel and reply-timeout arms lose their explicit
// giveUp call in this task and are reconciled by the deferred path instead.
// These pin that the slot is still reclaimed on both.
func TestChannelExchange_abandonedArmsReclaimSlot(t *testing.T) {
	defer goleak.VerifyNone(t)

	tests := []struct {
		name string
		// trigger drives the in-flight Exchange to its abandonment arm. It owns
		// everything arm-specific — cancelling the ctx, or advancing the clock —
		// so the shared body below needs no per-case branching.
		trigger func(t *testing.T, cancel context.CancelFunc, fakeClock *clockwork.FakeClock)
		assert  func(t *testing.T, err error)
	}{
		{
			name: "ctx cancel reclaims the slot",
			trigger: func(_ *testing.T, cancel context.CancelFunc, _ *clockwork.FakeClock) {
				cancel()
			},
			assert: func(t *testing.T, err error) { assert.ErrorIs(t, err, context.Canceled) },
		},
		{
			name: "reply timeout reclaims the slot",
			trigger: func(t *testing.T, _ context.CancelFunc, fakeClock *clockwork.FakeClock) {
				require.NoError(t, fakeClock.BlockUntilContext(t.Context(), 1))
				fakeClock.Advance(30 * time.Second)
			},
			assert: func(t *testing.T, err error) { assert.ErrorIs(t, err, msgin.ErrReplyTimeout) },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeClock := clockwork.NewFakeClock()
			ex, _, sinkHit := newBlockingExchange(t, endpoint.WithExchangeClock(fakeClock))
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			const id = "corr-abandon"

			// First request: registers the waiter, then abandons via tt.trigger.
			req := msgin.New[any]("payload", msgin.WithHeaders(map[string]any{msgin.HeaderCorrelationID: id}))
			errCh := make(chan error, 1)
			go func() {
				_, err := ex.Exchange(ctx, req)
				errCh <- err
			}()
			<-sinkHit // the flow ran, so the waiter is registered
			tt.trigger(t, cancel, fakeClock)
			tt.assert(t, <-errCh)

			// Reclamation probe: the id must be reusable. The second call hits
			// the same never-replying flow, so drive it to its own timeout on
			// a ctx the first case's cancel cannot affect.
			second := msgin.New[any]("second", msgin.WithHeaders(map[string]any{msgin.HeaderCorrelationID: id}))
			secondErrCh := make(chan error, 1)
			go func() {
				_, err := ex.Exchange(t.Context(), second)
				secondErrCh <- err
			}()
			<-sinkHit
			require.NoError(t, fakeClock.BlockUntilContext(t.Context(), 1))
			fakeClock.Advance(30 * time.Second)
			secondErr := <-secondErrCh
			require.NotErrorIs(t, secondErr, msgin.ErrDuplicateCorrelation, "the abandoned slot was not reclaimed")
			assert.ErrorIs(t, secondErr, msgin.ErrReplyTimeout)
		})
	}
}

// Spec 012 §5.3 / §6 case 3: when the flow sends its reply and THEN panics, a
// deliver is already committed to the slot when the unwind reaches the deferred
// reconciler. giveUp's deregister()==false arm must drain that reply to the
// unmatched sink — identical treatment to the timeout/cancel arms — while the
// panic still propagates unchanged.
func TestChannelExchange_panickingFlowAfterReply_drainsToUnmatchedSink(t *testing.T) {
	defer goleak.VerifyNone(t)

	sink := channel.NewDirectChannel()
	received := make(chan msgin.Message[any], 1)
	mustSubscribe(t, sink, msgin.HandlerFunc(func(_ context.Context, m msgin.Message[any]) error {
		received <- m
		return nil
	}))

	request := channel.NewDirectChannel()
	reply := channel.NewDirectChannel()
	ex, err := endpoint.NewChannelExchange(request, reply, endpoint.WithUnmatchedReplySink(sink))
	require.NoError(t, err)

	const id = "corr-reply-then-panic"
	// The flow replies (delivering into the waiter's slot) and only then panics.
	mustSubscribe(t, request, msgin.Chain(endpoint.Consume(func(ctx context.Context, m msgin.Message[any]) error {
		if sendErr := reply.Send(ctx, msgin.WithPayload(m, any("echo"))); sendErr != nil {
			return sendErr
		}
		panic("boom after reply")
	})))

	req := msgin.New[any]("payload", msgin.WithHeaders(map[string]any{msgin.HeaderCorrelationID: id}))
	recovered, _ := exchangeRecoveringPanic(t, ex, req)

	require.Equal(t, "boom after reply", recovered, "the panic must propagate unchanged through the drain")

	select {
	case got := <-received:
		assert.Equal(t, "echo", got.Payload())
	default:
		t.Fatal("expected the raced-in reply to be drained to the unmatched sink, not dropped")
	}

	// And the slot is still reclaimed: the id is reusable.
	reused, reusedErr := exchangeRecoveringPanic(t, ex, req)
	require.NotNil(t, reused, "the reused correlation id must reach the flow again (err=%v)", reusedErr)
}

// Spec 012 §6 case 5 (audit H-2): the flow hands the message to a worker
// goroutine and THEN panics, so deliver genuinely races the deferred
// reconciler rather than completing before it. close(ready) only makes the
// worker goroutine RUNNABLE — left to the scheduler, the panic unwind almost
// always reaches the deferred reconciler on THIS goroutine before the worker
// is ever scheduled, so the drain arm below would go essentially untested.
// runtime.Gosched() on every other iteration FORCES the split so both
// orderings are actually exercised, not merely hoped for:
//   - worker wins  -> deregister()==false -> giveUp drains to the sink
//   - unwind wins  -> deregister()==true  -> the late reply is unmatched
//
// Either way the panic must propagate unchanged, the slot must be reclaimed,
// and NOTHING may block. Bounded so a regression fails instead of wedging CI.
func TestChannelExchange_panicRacesDelivery(t *testing.T) {
	defer goleak.VerifyNone(t)

	const (
		iterations = 30
		id         = "corr-panic-race"
		budget     = 30 * time.Second
		grace      = 2 * time.Second
	)

	// Same discipline as Task 1: no require/t.Fatal off the test goroutine.
	failures := make(chan string, 1)
	fail := func(format string, args ...any) {
		select {
		case failures <- fmt.Sprintf(format, args...):
		default:
		}
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < iterations; i++ {
			// cap 2: this iteration drives the flow twice (the probe re-enters
			// it), so up to two replies can land on the unmatched path.
			received := make(chan msgin.Message[any], 2)
			sink := channel.NewDirectChannel()
			if _, err := sink.Subscribe(msgin.HandlerFunc(func(_ context.Context, m msgin.Message[any]) error {
				received <- m
				return nil
			})); err != nil {
				fail("iteration %d: sink subscribe: %v", i, err)
				return
			}

			request := channel.NewDirectChannel()
			reply := channel.NewDirectChannel()
			ex, err := endpoint.NewChannelExchange(request, reply, endpoint.WithUnmatchedReplySink(sink))
			if err != nil {
				fail("iteration %d: new exchange: %v", i, err)
				return
			}

			var workers sync.WaitGroup
			if _, err := request.Subscribe(msgin.Chain(endpoint.Consume(func(_ context.Context, m msgin.Message[any]) error {
				// ready is per-INVOCATION, not per-iteration: this handler runs
				// TWICE per iteration (the probe re-enters it), and an
				// iteration-scoped channel would be closed twice — a "close of
				// closed channel" panic masquerading as the flow's own panic
				// (audit H-1n).
				ready := make(chan struct{})
				workers.Add(1)
				go func() {
					defer workers.Done()
					<-ready // release the worker and the panic together
					_ = reply.Send(context.WithoutCancel(t.Context()), msgin.WithPayload(m, any("echo")))
				}()
				close(ready)
				if i%2 == 0 {
					runtime.Gosched() // force the worker's deliver to win, exercising giveUp's drain arm
				}
				panic("boom racing delivery")
			}))); err != nil {
				fail("iteration %d: request subscribe: %v", i, err)
				return
			}

			req := msgin.New[any]("payload", msgin.WithHeaders(map[string]any{msgin.HeaderCorrelationID: id}))

			// First drive: the panic must propagate unchanged through the drain.
			got, firstErr := exchangeRecoveringPanic(t, ex, req)
			if got != "boom racing delivery" {
				fail("iteration %d: first call recovered %#v, want the flow's own panic value (err=%v)", i, got, firstErr)
				return
			}
			workers.Wait()

			// The reply is accounted for on whichever arm won — drained by
			// giveUp, or routed as unmatched by the receiver. Never lost.
			select {
			case got := <-received:
				if got.Payload() != "echo" {
					fail("iteration %d: unmatched sink got payload %#v, want \"echo\"", i, got.Payload())
					return
				}
			case <-time.After(grace):
				fail("iteration %d: the raced reply reached neither the drain nor the unmatched path", i)
				return
			}

			// Second drive on the SAME id: proves the slot was reclaimed, and
			// its panic value must be intact too (a "close of closed channel"
			// regression would surface right here).
			got2, secondErr := exchangeRecoveringPanic(t, ex, req)
			if got2 != "boom racing delivery" {
				fail("iteration %d: reused id recovered %#v — the slot was not reclaimed, or the barrier double-closed (err=%v)", i, got2, secondErr)
				return
			}
			workers.Wait()

			select {
			case got := <-received:
				if got.Payload() != "echo" {
					fail("iteration %d: second unmatched sink got payload %#v, want \"echo\"", i, got.Payload())
					return
				}
			case <-time.After(grace):
				fail("iteration %d: the second raced reply was lost", i)
				return
			}
			if err := ex.Close(); err != nil {
				fail("iteration %d: close: %v", i, err)
				return
			}
		}
	}()

	select {
	case <-done:
	case <-time.After(budget):
		t.Fatal("the deferred reconciler blocked during a panic unwind (Spec 012 §5.3)")
	}
	select {
	case msg := <-failures:
		t.Fatal(msg)
	default:
	}
}
