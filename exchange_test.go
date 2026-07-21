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
	"fmt"
	"log/slog"
	"strconv"
	"sync"
	"sync/atomic"
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
			sink := msgin.NewDirectChannel()
			if err := sink.Subscribe(msgin.HandlerFunc(func(_ context.Context, _ msgin.Message[any]) error {
				sunk.Add(1)
				return nil
			})); err != nil {
				fail("iteration %d: sink subscribe: %v", i, err)
				return
			}

			request := msgin.NewDirectChannel()
			reply := msgin.NewDirectChannel()
			ex, err := msgin.NewChannelExchange(request, reply, msgin.WithUnmatchedReplySink(sink))
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
			if err := request.Subscribe(msgin.Chain(msgin.Consume(func(_ context.Context, m msgin.Message[any]) error {
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

func (c *scriptedChannel) Subscribe(_ msgin.MessageHandler) error { return nil }

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
	sink := msgin.NewDirectChannel()
	require.NoError(t, sink.Subscribe(msgin.HandlerFunc(func(_ context.Context, _ msgin.Message[any]) error {
		sunk.Add(1)
		return nil
	})))

	reply := msgin.NewDirectChannel()
	request := &scriptedChannel{}

	ex, err := msgin.NewChannelExchange(request, reply, msgin.WithUnmatchedReplySink(sink))
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
// Exchange — the exact defect path of Spec 012 §1.
func panicExchange(t *testing.T, panicVal any, opts ...msgin.ExchangeOption) (*msgin.ChannelExchange, msgin.MessageChannel) {
	t.Helper()
	request := msgin.NewDirectChannel()
	reply := msgin.NewDirectChannel()
	ex, err := msgin.NewChannelExchange(request, reply, opts...)
	require.NoError(t, err)
	require.NoError(t, request.Subscribe(msgin.Chain(msgin.Consume(func(_ context.Context, _ msgin.Message[any]) error {
		panic(panicVal)
	}))))
	return ex, reply
}

// exchangeRecoveringPanic calls ex.Exchange and returns the recovered panic
// value (nil if it did not panic), so a test can assert on the value WITHOUT
// the recover happening inside library code.
func exchangeRecoveringPanic(t *testing.T, ex *msgin.ChannelExchange, req msgin.Message[any]) (recovered any) {
	t.Helper()
	defer func() { recovered = recover() }()
	_, _ = ex.Exchange(t.Context(), req)
	return nil
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
			ex, _ := panicExchange(t, tt.panicVal, msgin.WithExchangeClock(fakeClock))
			const id = "corr-panic"
			req := msgin.New[any]("payload", msgin.WithHeaders(map[string]any{msgin.HeaderCorrelationID: id}))

			recovered := exchangeRecoveringPanic(t, ex, req)

			require.NotNil(t, recovered, "Exchange must not swallow the handler panic")
			tt.assert(t, recovered)

			// The reclamation probe (Spec 012 §6 case 2): the slot must be gone,
			// so REUSING the id must get past register(). It panics again (same
			// flow) rather than failing with ErrDuplicateCorrelation — which is
			// exactly the proof. Capture the error too, so a leaked slot fails
			// with the precise cause rather than a confusing "no panic".
			var (
				secondErr error
				second    any
			)
			func() {
				defer func() { second = recover() }()
				_, secondErr = ex.Exchange(t.Context(), req)
			}()
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
			ex, _, sinkHit := newBlockingExchange(t, msgin.WithExchangeClock(fakeClock))
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
