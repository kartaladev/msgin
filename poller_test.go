package msgin_test

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/jonboulle/clockwork"
	"github.com/kartaladev/msgin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// errPoll is the injected PollingSource failure used by the error-path tests.
var errPoll = errors.New("poll boom")

// fakePolling is a hand-written PollingSource + LiveValueSource test double. It
// records the largest max it is ever asked for and the number of Poll calls, and
// delegates the actual rows/error to an injected pollFn so each test scripts its
// own source behavior (backlog drain, empty, error injection, over-pull, …).
// EmitsLiveValue keeps the runtime on the live-value path so deliveries can carry
// order values directly (no codec), mirroring the streaming scriptedSource.
type fakePolling struct {
	mu     sync.Mutex
	maxK   int
	polls  int
	pollFn func(ctx context.Context, max int) ([]msgin.Delivery, error)
}

func (f *fakePolling) EmitsLiveValue() bool { return true }

func (f *fakePolling) Poll(ctx context.Context, max int) ([]msgin.Delivery, error) {
	f.mu.Lock()
	f.polls++
	if max > f.maxK {
		f.maxK = max
	}
	fn := f.pollFn
	f.mu.Unlock()
	return fn(ctx, max)
}

func (f *fakePolling) stats() (polls, maxK int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.polls, f.maxK
}

// backlog is a concurrency-safe FIFO a pollFn hands out up to max deliveries at a
// time, so a test can preload a source and observe credit-bounded draining.
type backlog struct {
	mu    sync.Mutex
	items []msgin.Delivery
}

func (b *backlog) add(d ...msgin.Delivery) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.items = append(b.items, d...)
}

func (b *backlog) take(max int) []msgin.Delivery {
	b.mu.Lock()
	defer b.mu.Unlock()
	if max > len(b.items) {
		max = len(b.items)
	}
	out := append([]msgin.Delivery(nil), b.items[:max]...)
	b.items = b.items[max:]
	return out
}

// TestPoller_DeliversBacklogToHandler is the happy path: a PollingSource now
// constructs and its backlog drains through the worker pool, each row Acked.
func TestPoller_DeliversBacklogToHandler(t *testing.T) {
	st := &settle{}
	bl := &backlog{}
	for i := 0; i < 5; i++ {
		bl.add(newSettleDelivery(order{ID: "o"}, fmt.Sprintf("m%d", i), st))
	}
	src := &fakePolling{pollFn: func(_ context.Context, max int) ([]msgin.Delivery, error) {
		return bl.take(max), nil
	}}
	var processed atomic.Int64
	h := func(context.Context, msgin.Message[order]) error { processed.Add(1); return nil }

	c, err := msgin.NewConsumer[order](src, h)
	require.NoError(t, err)
	stop := runConsumer(t, c)
	defer stop()

	require.Eventually(t, func() bool { return processed.Load() == 5 }, 2*time.Second, time.Millisecond)
	acks, _, _ := st.snapshot()
	assert.Equal(t, 5, acks, "every drained row was Acked")
	_, maxK := src.stats()
	assert.LessOrEqual(t, maxK, 100, "Poll never asked for more than pollMaxBatch")
}

// TestPoller_NeverOverPulls is the flood defense: with WithMaxInFlight(n) and a
// huge backlog, at most n rows are ever claimed-but-unsettled, and Poll is never
// asked for more than the free credit — even though pollMaxBatch is larger.
func TestPoller_NeverOverPulls(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		const n = 3
		const total = 20
		st := &settle{}
		bl := &backlog{}
		for i := 0; i < total; i++ {
			bl.add(newSettleDelivery(order{ID: "o"}, fmt.Sprintf("m%d", i), st))
		}
		src := &fakePolling{pollFn: func(_ context.Context, max int) ([]msgin.Delivery, error) {
			return bl.take(max), nil
		}}

		var inFlight, maxSeen, processed atomic.Int64
		release := make(chan struct{})
		h := func(_ context.Context, _ msgin.Message[order]) error {
			cur := inFlight.Add(1)
			for {
				m := maxSeen.Load()
				if cur <= m || maxSeen.CompareAndSwap(m, cur) {
					break
				}
			}
			<-release
			inFlight.Add(-1)
			processed.Add(1)
			return nil
		}
		c, err := msgin.NewConsumer[order](src, h,
			// A fake clock never advanced: the empty-poll sleepCtx and the sweep
			// ticker are fake timers that only fire on Advance, so they stay durably
			// blocked and invisible to synctest.Wait (mirrors the streaming test).
			msgin.WithConsumerClock[order](clockwork.NewFakeClock()),
			msgin.WithConcurrency[order](total), // many workers…
			msgin.WithMaxInFlight[order](n),     // …but credit caps in-flight at n
			msgin.WithPollMaxBatch[order](10))   // batch > n, so credit (not batch) is the cap
		require.NoError(t, err)

		ctx, cancel := context.WithCancel(t.Context())
		runDone := make(chan error, 1)
		go func() { runDone <- c.Run(ctx) }()

		synctest.Wait() // steady state: exactly n handlers entered, the rest gated
		assert.LessOrEqual(t, maxSeen.Load(), int64(n), "never more than n claimed-but-unsettled (no over-pull)")
		assert.Equal(t, int64(n), inFlight.Load(), "exactly n admitted; the surplus waits in the source")
		_, maxK := src.stats()
		assert.LessOrEqual(t, maxK, n, "Poll asked for at most the free credit (credit-at-fetch)")

		close(release) // let everything drain; credits recycle
		synctest.Wait()
		assert.Equal(t, int64(total), processed.Load(), "all messages eventually processed (no credit leak)")

		cancel()
		assert.ErrorIs(t, <-runDone, context.Canceled)
	})
}

// TestPoller_SurplusCreditReleased proves step-4 surplus release: a source that
// returns FEWER rows than requested must release the unfilled credits, or the
// gate starves after ~1 poll and the drain stalls short of the backlog.
func TestPoller_SurplusCreditReleased(t *testing.T) {
	st := &settle{}
	bl := &backlog{}
	for i := 0; i < 5; i++ {
		bl.add(newSettleDelivery(order{ID: "o"}, fmt.Sprintf("m%d", i), st))
	}
	// Return at most ONE row per poll even when asked for a full batch.
	src := &fakePolling{pollFn: func(_ context.Context, _ int) ([]msgin.Delivery, error) {
		return bl.take(1), nil
	}}
	var processed atomic.Int64
	h := func(context.Context, msgin.Message[order]) error { processed.Add(1); return nil }

	c, err := msgin.NewConsumer[order](src, h,
		msgin.WithMaxInFlight[order](4),
		msgin.WithPollMaxBatch[order](4))
	require.NoError(t, err)
	stop := runConsumer(t, c)
	defer stop()

	require.Eventually(t, func() bool { return processed.Load() == 5 }, 2*time.Second, time.Millisecond)
	_, maxK := src.stats()
	assert.Equal(t, 4, maxK, "each poll acquired the full batch of credits (surplus released, not leaked)")
}

// TestPoller_EmptyPollIdlesAndPinsNoCredit proves the empty-poll idle: after an
// empty poll the loop waits pollInterval on the injected clock, and it holds NO
// credit during the idle (every poll acquires — and releases — full credit).
func TestPoller_EmptyPollIdlesAndPinsNoCredit(t *testing.T) {
	clk := clockwork.NewFakeClock()
	var polls atomic.Int64
	src := &fakePolling{pollFn: func(context.Context, int) ([]msgin.Delivery, error) {
		polls.Add(1)
		return nil, nil // always empty
	}}
	h := func(context.Context, msgin.Message[order]) error { return nil }

	c, err := msgin.NewConsumer[order](src, h,
		msgin.WithConsumerClock[order](clk),
		msgin.WithMaxInFlight[order](4),
		msgin.WithPollMaxBatch[order](4),
		msgin.WithPollInterval[order](time.Second))
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	runDone := make(chan error, 1)
	go func() { runDone <- c.Run(ctx) }()

	// The first empty poll runs immediately, then the loop sleeps pollInterval.
	// Two fake-clock waiters: the always-on sweep ticker + the poll idle timer.
	require.NoError(t, clk.BlockUntilContext(ctx, 2))
	assert.Equal(t, int64(1), polls.Load(), "one immediate empty poll, then idle")
	_, maxK := src.stats()
	assert.Equal(t, 4, maxK, "empty poll acquired full credit and released it (none pinned during idle)")

	clk.Advance(time.Second) // fire the idle timer → one more poll, then idle again
	require.NoError(t, clk.BlockUntilContext(ctx, 2))
	assert.Equal(t, int64(2), polls.Load(), "poll paced by pollInterval on the fake clock")

	cancel()
	assert.ErrorIs(t, <-runDone, context.Canceled)
}

// TestPoller_ErrorBackoffAndReset proves the built-in error backoff:
// pollInterval*2^(errN-1) growth (1s, 2s, 4s), a reset to zero on the first
// successful poll (so the next error backs off from 1s again), and that every
// held credit is released between attempts.
func TestPoller_ErrorBackoffAndReset(t *testing.T) {
	clk := clockwork.NewFakeClock()
	var polls atomic.Int64
	// Fail polls 1-3, empty-succeed on 4 (reset), fail on 5, empty thereafter.
	src := &fakePolling{pollFn: func(context.Context, int) ([]msgin.Delivery, error) {
		switch polls.Add(1) {
		case 1, 2, 3, 5:
			return nil, errPoll
		default:
			return nil, nil
		}
	}}
	h := func(context.Context, msgin.Message[order]) error { return nil }

	c, err := msgin.NewConsumer[order](src, h,
		msgin.WithConsumerClock[order](clk),
		msgin.WithMaxInFlight[order](4),
		msgin.WithPollMaxBatch[order](4),
		msgin.WithPollInterval[order](time.Second))
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	runDone := make(chan error, 1)
	go func() { runDone <- c.Run(ctx) }()

	// backoff after the nth consecutive error = pollInterval * 2^(n-1).
	want := int64(1)
	for _, d := range []time.Duration{time.Second, 2 * time.Second, 4 * time.Second} {
		require.NoError(t, clk.BlockUntilContext(ctx, 2))
		assert.Equal(t, want, polls.Load())
		clk.Advance(d)
		want++
	}
	// poll 4 empty-succeeds → idle pollInterval and reset errN to 0.
	require.NoError(t, clk.BlockUntilContext(ctx, 2))
	assert.Equal(t, int64(4), polls.Load())
	clk.Advance(time.Second)
	// poll 5 fails → because errN reset, it backs off 1s (not 8s): 1s reaches poll 6.
	require.NoError(t, clk.BlockUntilContext(ctx, 2))
	assert.Equal(t, int64(5), polls.Load())
	clk.Advance(time.Second)
	require.NoError(t, clk.BlockUntilContext(ctx, 2))
	assert.Equal(t, int64(6), polls.Load(), "errN reset on success: the post-success error backs off from 1s")

	_, maxK := src.stats()
	assert.Equal(t, 4, maxK, "every poll (error or success) acquired and released full credit")

	cancel()
	assert.ErrorIs(t, <-runDone, context.Canceled)
}

// TestPoller_ClampsExcessDeliveries proves the defensive clamp (a buggy dialect
// returning len(rows) > held): the excess rows are Nacked unwrapped and logged,
// only `held` rows are handled, and the gate is not corrupted.
func TestPoller_ClampsExcessDeliveries(t *testing.T) {
	var logbuf lockedBuffer
	logger := slog.New(slog.NewTextHandler(&logbuf, &slog.HandlerOptions{Level: slog.LevelError}))
	st := &settle{}
	clk := clockwork.NewFakeClock()
	var polls atomic.Int64
	src := &fakePolling{pollFn: func(_ context.Context, max int) ([]msgin.Delivery, error) {
		if polls.Add(1) == 1 {
			rows := make([]msgin.Delivery, 0, max*2) // TWICE the requested rows
			for i := 0; i < max*2; i++ {
				rows = append(rows, newSettleDelivery(order{ID: "o"}, fmt.Sprintf("m%d", i), st))
			}
			return rows, nil
		}
		return nil, nil
	}}
	var processed atomic.Int64
	h := func(context.Context, msgin.Message[order]) error { processed.Add(1); return nil }

	c, err := msgin.NewConsumer[order](src, h,
		msgin.WithLogger[order](logger),
		msgin.WithConsumerClock[order](clk),
		msgin.WithMaxInFlight[order](2),
		msgin.WithPollMaxBatch[order](2))
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	runDone := make(chan error, 1)
	go func() { runDone <- c.Run(ctx) }()

	require.Eventually(t, func() bool { return processed.Load() == 2 }, 2*time.Second, time.Millisecond)
	require.Eventually(t, func() bool {
		_, nacks, _ := st.snapshot()
		return nacks == 2
	}, 2*time.Second, time.Millisecond)
	acks, nacks, _ := st.snapshot()
	assert.Equal(t, 2, acks, "the 2 credited rows were processed and Acked")
	assert.Equal(t, 2, nacks, "the 2 excess rows were Nacked (requeue) unwrapped")
	assert.Contains(t, logbuf.String(), "more deliveries than requested", "the clamp violation is logged at ERROR")

	cancel()
	assert.ErrorIs(t, <-runDone, context.Canceled)
}

// TestPoller_RowsWithErrorDiscardedNoLeak proves the Poll no-partial-result
// contract: rows returned alongside a non-nil error are discarded (never handled
// or settled by the loop — Poll owns rollback) and every held credit is released,
// so the loop recovers cleanly on the next poll.
func TestPoller_RowsWithErrorDiscardedNoLeak(t *testing.T) {
	st := &settle{}
	clk := clockwork.NewFakeClock()
	var polls atomic.Int64
	src := &fakePolling{pollFn: func(_ context.Context, _ int) ([]msgin.Delivery, error) {
		if polls.Add(1) == 1 {
			return []msgin.Delivery{
				newSettleDelivery(order{ID: "o"}, "x0", st),
				newSettleDelivery(order{ID: "o"}, "x1", st),
			}, errPoll
		}
		return nil, nil
	}}
	var processed atomic.Int64
	h := func(context.Context, msgin.Message[order]) error { processed.Add(1); return nil }

	c, err := msgin.NewConsumer[order](src, h,
		msgin.WithConsumerClock[order](clk),
		msgin.WithMaxInFlight[order](4),
		msgin.WithPollMaxBatch[order](4),
		msgin.WithPollInterval[order](time.Second))
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	runDone := make(chan error, 1)
	go func() { runDone <- c.Run(ctx) }()

	require.NoError(t, clk.BlockUntilContext(ctx, 2)) // poll 1 errored → backoff timer armed
	assert.Equal(t, int64(1), polls.Load())
	clk.Advance(time.Second)
	require.NoError(t, clk.BlockUntilContext(ctx, 2)) // recovered → empty poll → idle timer armed
	assert.Equal(t, int64(2), polls.Load(), "recovered after the errored poll (credit not leaked)")

	_, maxK := src.stats()
	assert.Equal(t, 4, maxK, "errored poll released all held credit")
	assert.Equal(t, int64(0), processed.Load(), "rows returned with an error are never handled")
	acks, nacks, _ := st.snapshot()
	assert.Equal(t, 0, acks)
	assert.Equal(t, 0, nacks, "discarded rows are not settled by the loop (Poll owns rollback)")

	cancel()
	assert.ErrorIs(t, <-runDone, context.Canceled)
}

// TestPoller_ShutdownDuringErrorBackoff proves the ctx-done branch of the
// error-backoff wait: a source that keeps erroring parks the loop in the backoff
// sleepCtx, and a cancel there returns promptly (sleepCtx returns false → exit).
func TestPoller_ShutdownDuringErrorBackoff(t *testing.T) {
	clk := clockwork.NewFakeClock()
	src := &fakePolling{pollFn: func(context.Context, int) ([]msgin.Delivery, error) {
		return nil, errPoll // always error → the loop backs off
	}}
	h := func(context.Context, msgin.Message[order]) error { return nil }

	c, err := msgin.NewConsumer[order](src, h,
		msgin.WithConsumerClock[order](clk),
		msgin.WithMaxInFlight[order](2),
		msgin.WithPollInterval[order](time.Second))
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	runDone := make(chan error, 1)
	go func() { runDone <- c.Run(ctx) }()

	require.NoError(t, clk.BlockUntilContext(ctx, 2)) // first poll errored → parked in backoff sleepCtx
	cancel()                                          // ctx-done during backoff → sleepCtx false → exit
	select {
	case err := <-runDone:
		assert.ErrorIs(t, err, context.Canceled)
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after cancel during error backoff")
	}
}

// TestPoller_ShutdownDrainsInFlightAndBounded proves the shared drain on the pull
// path: cancel while a handler holds credit and a backlog waits → pollLoop exits,
// workerCh closes, workers join, and Run returns within the shutdown deadline.
func TestPoller_ShutdownDrainsInFlightAndBounded(t *testing.T) {
	st := &settle{}
	bl := &backlog{}
	for i := 0; i < 50; i++ {
		bl.add(newSettleDelivery(order{ID: "o"}, fmt.Sprintf("m%d", i), st))
	}
	src := &fakePolling{pollFn: func(_ context.Context, max int) ([]msgin.Delivery, error) {
		return bl.take(max), nil
	}}
	entered := make(chan struct{}, 1)
	h := func(ctx context.Context, _ msgin.Message[order]) error {
		select {
		case entered <- struct{}{}:
		default:
		}
		<-ctx.Done() // cooperative: hold the credit until the shutdown deadline cancels drainCtx
		return ctx.Err()
	}
	c, err := msgin.NewConsumer[order](src, h,
		msgin.WithMaxInFlight[order](4),
		msgin.WithConcurrency[order](4),
		msgin.WithShutdownTimeout[order](500*time.Millisecond))
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	runDone := make(chan error, 1)
	go func() { runDone <- c.Run(ctx) }()

	<-entered // at least one handler runs, holding credit; pollLoop is parked on acquire
	cancel()
	select {
	case err := <-runDone:
		assert.ErrorIs(t, err, context.Canceled)
	case <-time.After(3 * time.Second):
		t.Fatal("Run hung past the shutdown deadline on the poll-path drain")
	}
}

// TestPoller_HandoffCtxDoneBoundedNoLeak forces the near-unreachable ctx-done arm
// of the hand-off select. workerCh is buffered to maxInFlight and credit-bounded,
// so `out <- md` never blocks under correct accounting; the arm is only entered
// when ctx is already done and the select coin-flips to it. Poll blocks until the
// test cancels ctx, then returns a FULL batch of maxInFlight rows, so across the
// batch the arm is hit with probability 1 - 2^-maxInFlight. An OVER-release there
// (more than the exact len(rows)-i-1 un-handed rows) would block pollLoop on a
// non-cancellable <-tokens BEFORE close(workerCh) → workers never finish range →
// wg.Wait hangs → Run hangs past the deadline. This asserts it does not, and that
// no delivery is settled twice.
func TestPoller_HandoffCtxDoneBoundedNoLeak(t *testing.T) {
	const n = 12
	st := &settle{}
	var once sync.Once
	started := make(chan struct{})
	src := &fakePolling{pollFn: func(pctx context.Context, max int) ([]msgin.Delivery, error) {
		once.Do(func() { close(started) })
		<-pctx.Done() // block until the test cancels ctx…
		rows := make([]msgin.Delivery, 0, max)
		for i := 0; i < max; i++ {
			rows = append(rows, newSettleDelivery(order{ID: "o"}, fmt.Sprintf("m%d", i), st))
		}
		return rows, nil // …then drive the hand-off with ctx already done
	}}
	h := func(context.Context, msgin.Message[order]) error { return nil }

	c, err := msgin.NewConsumer[order](src, h,
		msgin.WithMaxInFlight[order](n),
		msgin.WithPollMaxBatch[order](n),
		msgin.WithConcurrency[order](1),
		msgin.WithShutdownTimeout[order](500*time.Millisecond))
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	runDone := make(chan error, 1)
	go func() { runDone <- c.Run(ctx) }()

	<-started
	cancel() // unblocks Poll; the hand-off loop then runs with ctx already done
	select {
	case err := <-runDone:
		assert.ErrorIs(t, err, context.Canceled)
	case <-time.After(3 * time.Second):
		t.Fatal("Run hung: a mis-counted hand-off release blocked pollLoop before close(workerCh)")
	}
	acks, nacks, _ := st.snapshot()
	assert.LessOrEqual(t, acks+nacks, n, "no double-settle across the hand-off ctx-done arm")
}

// TestPoller_SafePoll_PollPanicEngagesBackoffNoCrash proves safePoll's panic
// recovery (ADR 0010 D6 extension): a PollingSource whose Poll panics — a
// realistic failure mode for a wire adapter (e.g. the sql adapter this plan
// ships) — must not crash the process. safePoll recovers the panic into an
// error, and pollLoop's EXISTING error path (the "poll failed" ERROR log +
// backoff) handles it identically to a returned error: no second/duplicate
// ERROR log is added by safePoll itself.
func TestPoller_SafePoll_PollPanicEngagesBackoffNoCrash(t *testing.T) {
	var logbuf lockedBuffer
	logger := slog.New(slog.NewTextHandler(&logbuf, &slog.HandlerOptions{Level: slog.LevelError}))
	clk := clockwork.NewFakeClock()
	var polls atomic.Int64
	src := &fakePolling{pollFn: func(context.Context, int) ([]msgin.Delivery, error) {
		polls.Add(1)
		panic("fakePolling.Poll boom")
	}}
	h := func(context.Context, msgin.Message[order]) error { return nil }

	c, err := msgin.NewConsumer[order](src, h,
		msgin.WithLogger[order](logger),
		msgin.WithConsumerClock[order](clk),
		msgin.WithMaxInFlight[order](2),
		msgin.WithPollInterval[order](time.Second))
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	runDone := make(chan error, 1)
	go func() { runDone <- c.Run(ctx) }()

	require.NoError(t, clk.BlockUntilContext(ctx, 2)) // first poll panicked → parked in the error backoff sleepCtx
	assert.Equal(t, int64(1), polls.Load())
	logged := logbuf.String()
	assert.Contains(t, logged, "poll failed", "pollLoop's existing error path logs the recovered panic loudly")
	assert.Contains(t, logged, "Poll panicked", "the recovered panic detail is carried in the error")
	assert.Equal(t, 1, strings.Count(logged, "poll failed"), "safePoll adds no second/duplicate ERROR log of its own")

	clk.Advance(time.Second) // fire the backoff timer → poll again → panics again → still no crash
	require.NoError(t, clk.BlockUntilContext(ctx, 2))
	assert.Equal(t, int64(2), polls.Load())

	cancel()
	select {
	case err := <-runDone:
		assert.ErrorIs(t, err, context.Canceled)
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not return after cancel during poll-panic backoff — safePoll must recover, not crash or hang")
	}
}
