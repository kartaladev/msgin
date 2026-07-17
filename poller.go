package msgin

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/jonboulle/clockwork"
)

// pollLoop is the pull-path producer (ADR 0010 D1): the credit-at-fetch poll
// loop that drives a PollingSource. It is the exact analogue of ingest for the
// push path — it runs on ingressWG and its caller owns close(workerCh) on exit —
// but it acquires credit BEFORE fetching (so a huge backlog stays durably in the
// source rather than flooding an in-process buffer) and it is the SOLE acquirer
// on this path (workers only release, via the release-first manage wrapper).
//
// It takes ctx (the shutdown/parent context, driving the acquire, the Poll, and
// the handoff) and settleCtx (drain-surviving, driving the Nack of a held-but-
// unhanded delivery at shutdown). It never over-pulls: at most maxInFlight rows
// are ever claimed-but-unsettled, each carrying one credit released by its
// terminal settle.
func (c *consumer[T]) pollLoop(ctx, settleCtx context.Context, gate *creditGate, out chan<- managedDelivery) {
	errN := 0
	for {
		// 1. Acquire at least one credit, BLOCKING on the gate until a worker's
		//    release frees a slot (the natural "wait for capacity" primitive,
		//    missed-wakeup-free). At zero free credit the loop does not poll.
		if gate.acquire(ctx) != nil {
			return // ctx done → shutdown
		}
		held := 1
		// 2. Top up: grab additional FREE credits, non-blocking, up to pollMaxBatch.
		for held < c.pollMaxBatch && gate.tryAcquire() {
			held++
		}
		// 3. Fetch up to `held` rows. Poll's contract (spi.go): at most `held`
		//    deliveries, none alongside a non-nil error, and it owns rollback of
		//    any partial work on the error/cancel path.
		rows, err := c.safePoll(ctx, held)
		if err != nil {
			// The promised loud log (ADR 0010 D2); rows (if any, a contract
			// violation) are discarded — Poll owns their rollback.
			c.logger.Error("msgin: poll failed", "err", err)
			releaseN(gate, held) // release ALL held credits
			errN++
			if !sleepCtx(ctx, c.clock, c.pollErrorBackoff(errN)) {
				return // ctx done during backoff → shutdown
			}
			continue
		}
		// 3a. DEFENSIVE clamp (ADR 0010 D1): a buggy dialect returning
		//     len(rows) > held would corrupt the gate. Never wrap more rows than
		//     credits held: the excess hold no credit, so they are Nacked
		//     (unwrapped) and dropped, and the violation is logged at ERROR.
		if len(rows) > held {
			rows = c.clampExcess(settleCtx, rows, held)
		}
		errN = 0
		// 4. Release the surplus credits no row filled. held >= len(rows) now, so
		//    the count is >= 0; releaseN panics on a negative n (bug tripwire).
		releaseN(gate, held-len(rows))
		// 5. Hand each row to a worker, release-first-wrapped (manage). One
		//    acquired credit rides with each row and is released by its terminal
		//    settle.
		for i, d := range rows {
			md := manage(d, sync.OnceFunc(gate.release))
			select {
			case out <- md:
			case <-ctx.Done():
				// Shutdown mid-handoff. The wrapped Nack releases THIS row's
				// (rows[i]) credit; releaseN releases EXACTLY the un-handed
				// rows[i+1:] (rows[:i] rode off with earlier handoffs). An
				// over-count here blocks the loop before close(workerCh) → the
				// pool never joins (C1 violation), so the count must be exact.
				c.finish(c.safeNack(settleCtx, md.Delivery, true, 0))
				releaseN(gate, len(rows)-i-1)
				return
			}
		}
		// 6. Pace. Non-empty → loop immediately (drain the backlog while credit +
		//    rows last; the acquire in step 1 backpressures). Empty → idle-wait
		//    pollInterval so an empty backlog does not busy-poll the source.
		if len(rows) == 0 {
			if !sleepCtx(ctx, c.clock, c.pollInterval) {
				return // ctx done during idle → shutdown
			}
		}
	}
}

// safePoll invokes PollingSource.Poll, recovering a panic so a faulty pull
// adapter (a realistic failure mode for a wire adapter, e.g. the sql adapter
// this plan ships) cannot crash the process (fault isolation, ADR 0010 D6 —
// extending the same "guard adapter SPI calls" principle from the settlement
// path to the poll call itself). A recovered panic is mapped to an error, so
// the caller (pollLoop) treats it identically to a returned Poll error: it
// discards any rows (none, by construction of the recover — a panic never
// reaches the return statement), releases every held credit, and engages the
// existing poll-error backoff. safePoll does NOT log — pollLoop's existing
// error path already logs "msgin: poll failed" loudly (with this error, whose
// text carries the recovered panic value) and then backs off, which correctly
// handles a panicked poll without a second, duplicate ERROR log line.
func (c *consumer[T]) safePoll(ctx context.Context, max int) (rows []Delivery, err error) {
	defer func() {
		if r := recover(); r != nil {
			rows, err = nil, fmt.Errorf("msgin: PollingSource.Poll panicked: %v", r)
		}
	}()
	return c.pollSrc.Poll(ctx, max)
}

// clampExcess enforces the Poll(max) <= max contract defensively (ADR 0010 D1):
// it logs the violation at ERROR and Nacks (requeue) the excess rows[held:]
// UNWRAPPED — they hold no credit, so they must not release one — then returns
// rows[:held], the credited prefix the loop hands to workers.
func (c *consumer[T]) clampExcess(settleCtx context.Context, rows []Delivery, held int) []Delivery {
	c.logger.Error("msgin: PollingSource returned more deliveries than requested; dropping the excess",
		"returned", len(rows), "requested", held)
	for _, d := range rows[held:] {
		c.finish(c.safeNack(settleCtx, d, true, 0)) // unwrapped: these rows hold no credit
	}
	return rows[:held]
}

// pollErrorBackoff returns the idle wait after the nth (1-based) consecutive poll
// error: min(maxPollErrorBackoff, pollInterval * 2^(n-1)), reusing the stateless
// ExponentialBackoff (attempt-indexed, O(1), clockwork-testable). Reset to zero
// on the first successful poll by the caller.
func (c *consumer[T]) pollErrorBackoff(n int) time.Duration {
	return ExponentialBackoff{
		Initial: c.pollInterval,
		Max:     maxPollErrorBackoff,
		Mult:    2,
	}.Delay(n - 1)
}

// releaseN returns n credits to the gate — n plain <-tokens receives, NOT
// OnceFunc-wrapped (the poll loop is the sole releaser of surplus/held credits,
// so idempotency is neither needed nor wanted here). It PANICS on a negative n as
// a bug tripwire (ADR 0010 D1): a negative count means the credit accounting has
// drifted, which is a defect to surface loudly, not paper over.
func releaseN(g *creditGate, n int) {
	if n < 0 {
		panic("msgin: releaseN called with a negative count (credit-accounting bug)")
	}
	for i := 0; i < n; i++ {
		g.release()
	}
}

// sleepCtx waits for d on the injected clock or until ctx is done, returning true
// if the full duration elapsed and false if ctx was cancelled first. A
// non-positive d returns true immediately (no timer registered).
func sleepCtx(ctx context.Context, clock clockwork.Clock, d time.Duration) bool {
	if d <= 0 {
		return true
	}
	select {
	case <-clock.After(d):
		return true
	case <-ctx.Done():
		return false
	}
}
