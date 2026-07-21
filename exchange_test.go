package msgin_test

// NOTE on table-test skill compliance: TestNewChannelExchange_validation uses
// the mandatory assert-closure table form (its three cases share an identical
// construct+assert shape). Every other test below is a standalone TestXxx
// because each exercises a genuinely different concurrency/synchronization
// shape (fake-clock races, cross-goroutine delivery, Close/timeout races) —
// forcing them into one table would hide the setup divergence the table-test
// skill's exception clause calls out.

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/jonboulle/clockwork"
	"github.com/kartaladev/msgin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
)

// newLoopExchange builds a ChannelExchange over two fresh DirectChannels and
// subscribes a synchronous echo flow onto request (request -> reply), so
// Exchange returns the echoed request as its reply.
func newLoopExchange(t *testing.T, opts ...msgin.ExchangeOption) (ex *msgin.ChannelExchange, request, reply msgin.MessageChannel) {
	t.Helper()
	request = msgin.NewDirectChannel()
	reply = msgin.NewDirectChannel()
	ex, err := msgin.NewChannelExchange(request, reply, opts...)
	require.NoError(t, err)
	require.NoError(t, request.Subscribe(msgin.Chain(msgin.To(reply))))
	return ex, request, reply
}

// newBlockingExchange builds a ChannelExchange whose request flow never
// replies: it only signals sinkHit (buffered, cap 1) once invoked. Because a
// DirectChannel runs the flow synchronously inside request.Send, receiving on
// sinkHit proves the waiter is registered and Exchange has reached (or is
// about to reach) its select before the test fires a timeout/cancel/Close.
func newBlockingExchange(t *testing.T, opts ...msgin.ExchangeOption) (ex *msgin.ChannelExchange, reply msgin.MessageChannel, sinkHit chan struct{}) {
	t.Helper()
	request := msgin.NewDirectChannel()
	reply = msgin.NewDirectChannel()
	ex, err := msgin.NewChannelExchange(request, reply, opts...)
	require.NoError(t, err)
	hit := make(chan struct{}, 1)
	require.NoError(t, request.Subscribe(msgin.Chain(msgin.Consume(func(_ context.Context, _ msgin.Message[any]) error {
		hit <- struct{}{}
		return nil
	}))))
	return ex, reply, hit
}

// asyncEcho wires request -> a worker goroutine that echoes each request to reply.
// stop() drains and joins the worker (goleak-clean). Because reply.Send runs on
// the worker goroutine, the waiter's select genuinely races deliver.
func asyncEcho(t *testing.T, request, reply msgin.MessageChannel) (stop func()) {
	t.Helper()
	work := make(chan msgin.Message[any], 64)
	done := make(chan struct{})
	if err := request.Subscribe(msgin.HandlerFunc(func(_ context.Context, m msgin.Message[any]) error {
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

func TestNewChannelExchange_validation(t *testing.T) {
	direct := msgin.NewDirectChannel()
	tests := []struct {
		name    string
		request msgin.MessageChannel
		reply   msgin.MessageChannel
		opts    []msgin.ExchangeOption
		assert  func(t *testing.T, ex *msgin.ChannelExchange, err error)
	}{
		{
			name: "nil request", request: nil, reply: direct,
			assert: func(t *testing.T, ex *msgin.ChannelExchange, err error) {
				if !errors.Is(err, msgin.ErrNilChannel) {
					t.Fatalf("want ErrNilChannel, got %v", err)
				}
			},
		},
		{
			name: "nil reply", request: direct, reply: nil,
			assert: func(t *testing.T, ex *msgin.ChannelExchange, err error) {
				if !errors.Is(err, msgin.ErrNilChannel) {
					t.Fatalf("want ErrNilChannel, got %v", err)
				}
			},
		},
		{
			name: "explicit non-positive timeout", request: msgin.NewDirectChannel(), reply: msgin.NewDirectChannel(),
			opts: []msgin.ExchangeOption{msgin.WithReplyTimeout(0)},
			assert: func(t *testing.T, ex *msgin.ChannelExchange, err error) {
				if !errors.Is(err, msgin.ErrInvalidReplyTimeout) {
					t.Fatalf("want ErrInvalidReplyTimeout, got %v", err)
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ex, err := msgin.NewChannelExchange(tt.request, tt.reply, tt.opts...)
			tt.assert(t, ex, err)
		})
	}
}

func TestChannelExchange_nilOptionGuards(t *testing.T) {
	// WithExchangeClock(nil)/WithExchangeLogger(nil) must be no-ops (default
	// stays in place), not a nil-panic on caller input.
	ex, err := msgin.NewChannelExchange(msgin.NewDirectChannel(), msgin.NewDirectChannel(),
		msgin.WithExchangeClock(nil),
		msgin.WithExchangeLogger(nil),
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
	ex, _, sinkHit := newBlockingExchange(t, msgin.WithExchangeClock(fakeClock))
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
	sink := msgin.NewDirectChannel()
	received := make(chan msgin.Message[any], 1)
	require.NoError(t, sink.Subscribe(msgin.HandlerFunc(func(_ context.Context, m msgin.Message[any]) error {
		received <- m
		return nil
	})))
	request := msgin.NewDirectChannel() // no subscriber -> Send fails with ErrNoSubscriber
	reply := msgin.NewDirectChannel()
	ex, err := msgin.NewChannelExchange(request, reply, msgin.WithUnmatchedReplySink(sink))
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
	_, _, reply := newLoopExchange(t, msgin.WithExchangeLogger(logger))
	orphan := msgin.New[any]("orphan", msgin.WithHeaders(map[string]any{msgin.HeaderCorrelationID: "no-such-id"}))

	err := reply.Send(t.Context(), orphan)

	assert.NoError(t, err)
	assert.Contains(t, logs.String(), "msgin: dropping unmatched gateway reply")
}

func TestChannelExchange_unmatchedReply_sink(t *testing.T) {
	sink := msgin.NewDirectChannel()
	received := make(chan msgin.Message[any], 1)
	require.NoError(t, sink.Subscribe(msgin.HandlerFunc(func(_ context.Context, m msgin.Message[any]) error {
		received <- m
		return nil
	})))
	_, _, reply := newLoopExchange(t, msgin.WithUnmatchedReplySink(sink))
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
	_, _, reply := newLoopExchange(t, msgin.WithUnmatchedReplySink(sink), msgin.WithExchangeLogger(logger))
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
	request := msgin.NewDirectChannel()
	reply := msgin.NewDirectChannel()
	// Pre-subscribe reply so NewChannelExchange's own Subscribe collides.
	require.NoError(t, reply.Subscribe(msgin.HandlerFunc(func(context.Context, msgin.Message[any]) error { return nil })))

	ex, err := msgin.NewChannelExchange(request, reply)

	assert.Nil(t, ex)
	assert.ErrorIs(t, err, msgin.ErrChannelSubscribed)
}

func TestChannelExchange_closeIdempotent(t *testing.T) {
	ex, _, _ := newLoopExchange(t)
	require.NoError(t, ex.Close())

	err := ex.Close()

	assert.NoError(t, err)
}

func TestChannelExchange_emptyCorrelation(t *testing.T) {
	// request has no subscriber: if Exchange attempted to send despite the
	// missing correlation id, we'd observe ErrNoSubscriber instead — proving
	// the empty-correlation guard short-circuits before any send (audit G1).
	request := msgin.NewDirectChannel()
	reply := msgin.NewDirectChannel()
	ex, err := msgin.NewChannelExchange(request, reply)
	require.NoError(t, err)
	req := msgin.New[any]("no-corr")

	_, err = ex.Exchange(t.Context(), req)

	assert.ErrorIs(t, err, msgin.ErrNoCorrelation)
}

func TestChannelExchange_duplicateCorrelation(t *testing.T) {
	defer goleak.VerifyNone(t)
	fakeClock := clockwork.NewFakeClock()
	ex, _, sinkHit := newBlockingExchange(t, msgin.WithExchangeClock(fakeClock))
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
	request := msgin.NewDirectChannel()
	reply := msgin.NewDirectChannel()
	ex, err := msgin.NewChannelExchange(request, reply)
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
	request := msgin.NewDirectChannel()
	reply := msgin.NewDirectChannel()
	ex, err := msgin.NewChannelExchange(request, reply)
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
		sink := msgin.NewDirectChannel()
		require.NoError(t, sink.Subscribe(msgin.HandlerFunc(func(_ context.Context, m msgin.Message[any]) error {
			sinkRecv <- m
			return nil
		})))

		request := msgin.NewDirectChannel()
		reply := msgin.NewDirectChannel()
		ex, err := msgin.NewChannelExchange(request, reply,
			msgin.WithExchangeClock(fakeClock),
			msgin.WithUnmatchedReplySink(sink),
		)
		require.NoError(t, err)

		work := make(chan msgin.Message[any], 1)
		require.NoError(t, request.Subscribe(msgin.HandlerFunc(func(_ context.Context, m msgin.Message[any]) error {
			work <- m
			return nil
		})))

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
	sink := msgin.NewDirectChannel()
	require.NoError(t, sink.Subscribe(msgin.HandlerFunc(func(_ context.Context, m msgin.Message[any]) error {
		sinkRecv <- m
		return nil
	})))

	ex, _, sinkHit := newBlockingExchange(t, msgin.WithExchangeClock(fakeClock), msgin.WithUnmatchedReplySink(sink))
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
