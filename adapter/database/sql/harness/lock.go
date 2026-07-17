package harness

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	msgin "github.com/kartaladev/msgin"
	msginsql "github.com/kartaladev/msgin/adapter/database/sql"
	"github.com/stretchr/testify/require"
)

// RunLock exercises the lock/FOR UPDATE strategy (ADR 0010 D5) end-to-end and
// at the dialect level against kit.Lease (asserted to also implement
// msginsql.LockDialect — the segregated lock SPI) on the already-open db. It
// reproduces every assertion the pre-split LockSuite made (Plan 006 Task 3):
// the single-row claim cap, the always-commit Nack (delivery_count climbs
// across redeliveries under the DEFAULT retry policy), the crash-reverts-only-
// the-in-flight-attempt semantics, empty/error claim rollback with no
// connection leak, shutdown NOT redelivering in-flight lock deliveries
// (detached claim tx), a separate-pool DLQ draining without deadlock, the
// corrupt-headers penalty-NackLock skip, the requeue=false collapse, and the
// deadline-Nack rollback arm. kit.OpenDB is required here (and only here) for
// the separate-pool DLQ assertion.
//
// Every assertion here is CORRECTNESS-only (a claim exclusively owns its row
// until settle; a settle either commits or rolls back the WHOLE in-flight
// attempt; no message is lost or double-delivered) — never a
// non-blocking/throughput assumption — so a single-writer engine passes the
// same contract unchanged (audit F8).
func RunLock(t *testing.T, kit TestKit, db *sql.DB) {
	t.Helper()
	lockDialect, ok := kit.Lease.(msginsql.LockDialect)
	require.True(t, ok, "TestKit.Lease must implement msginsql.LockDialect for RunLock")
	require.NotNil(t, kit.OpenDB, "TestKit.OpenDB is required for RunLock (separate-pool DLQ test)")

	var counter atomic.Int64
	fresh := func(ctx context.Context) string { return freshTable(t, ctx, kit, db, &counter, "msgin_lock") }

	newLockSource := func(t *testing.T, table string, opts ...msginsql.Option) *msginsql.Source {
		src, err := msginsql.NewPollingSource(db, table, kit.Lease,
			append([]msginsql.Option{msginsql.WithStrategy(msginsql.StrategyLockForUpdate)}, opts...)...)
		require.NoError(t, err)
		return src
	}

	t.Run("ClaimIsSingleRowAndAckCommits", func(t *testing.T) {
		ctx := t.Context()
		table := fresh(ctx)
		insertJSON(t, ctx, kit, db, table, "lock-a", "hello")
		insertJSON(t, ctx, kit, db, table, "lock-b", "world")

		src := newLockSource(t, table)

		deliveries, err := src.Poll(ctx, 10)
		require.NoError(t, err)
		require.Len(t, deliveries, 1, "lock ClaimLock returns at most one row regardless of max")

		require.NoError(t, deliveries[0].Ack(ctx))
		require.Equal(t, 1, rowCount(t, ctx, kit, db, table),
			"the Acked row is DELETEd and committed; the other remains")
		requireNoConnLeak(t, db)
	})

	t.Run("DeliveryCountProgressesAcrossDefaultNackRedeliverLoop", func(t *testing.T) {
		ctx := t.Context()
		table := fresh(ctx)
		insertJSON(t, ctx, kit, db, table, "poison-1", "boom")

		src := newLockSource(t, table)

		var mu sync.Mutex
		var counts []int
		handler := func(_ context.Context, msg msgin.Message[string]) error {
			dc, _ := msg.Headers().Int(msgin.HeaderDeliveryCount)
			mu.Lock()
			counts = append(counts, dc)
			mu.Unlock()
			return fmt.Errorf("permanent-ish transient failure at attempt %d", dc)
		}

		dlq := &recordingSink{}
		done := make(chan struct{})
		var once sync.Once
		consumer, err := msgin.NewConsumer[string](src, handler,
			msgin.WithConcurrency[string](1),
			msgin.WithMaxInFlight[string](1),
			msgin.WithPollMaxBatch[string](1),
			msgin.WithPollInterval[string](100*time.Millisecond),
			msgin.WithShutdownTimeout[string](10*time.Second),
			msgin.WithRetryPolicy[string](msgin.RetryPolicy{MaxAttempts: 3, DeadLetter: dlq}),
			msgin.WithHooks[string](msgin.Hooks{
				OnDeadLetter: func(_ context.Context, _ msgin.Message[any], _ error) {
					once.Do(func() { close(done) })
				},
			}),
		)
		require.NoError(t, err)

		require.ErrorIs(t, runConsumerUntil(t, ctx, consumer, done, 30*time.Second), context.Canceled)

		mu.Lock()
		require.Equal(t, []int{1, 2, 3}, counts,
			"delivery_count must CLIMB across default-backoff Nack redeliveries (always-commit fix)")
		mu.Unlock()
		require.Equal(t, 1, dlq.count(), "the exhausted poison message is dead-lettered exactly once")
		require.Equal(t, 0, rowCount(t, ctx, kit, db, table), "the dead-lettered row is Acked away")
		requireNoConnLeak(t, db)
	})

	t.Run("CrashRevertsInFlightAttemptOnly", func(t *testing.T) {
		ctx := t.Context()
		table := fresh(ctx)
		insertJSON(t, ctx, kit, db, table, "crash-1", "hello")

		lr1, err := lockDialect.ClaimLock(ctx, db, table, "owner")
		require.NoError(t, err)
		require.NotNil(t, lr1)
		require.Equal(t, 1, lr1.DeliveryCount, "first claim bumps delivery_count to 1 (in the tx)")

		require.NoError(t, lr1.Tx.Rollback())
		requireNoConnLeak(t, db)

		lr2, err := lockDialect.ClaimLock(ctx, db, table, "owner")
		require.NoError(t, err)
		require.NotNil(t, lr2, "the crashed row is immediately re-claimable")
		require.Equal(t, 1, lr2.DeliveryCount, "the crashed ++ was reverted; re-claim reports 1, not 2")

		require.NoError(t, lockDialect.AckLock(ctx, lr2, table))
		require.Equal(t, 0, rowCount(t, ctx, kit, db, table))
		requireNoConnLeak(t, db)
	})

	t.Run("ClaimEmptyAndErrorRollBackNoConnLeak", func(t *testing.T) {
		ctx := t.Context()

		t.Run("empty claim rolls back", func(t *testing.T) {
			table := fresh(ctx)
			src := newLockSource(t, table)

			deliveries, err := src.Poll(ctx, 10)
			require.NoError(t, err)
			require.Empty(t, deliveries, "nothing claimable → empty batch")
			requireNoConnLeak(t, db)
		})

		t.Run("claim error rolls back", func(t *testing.T) {
			table := fmt.Sprintf("msgin_lock_missing_%d", counter.Add(1))
			src := newLockSource(t, table)

			deliveries, err := src.Poll(ctx, 10)
			require.Nil(t, deliveries, "no deliveries alongside an error (Poll contract)")
			require.ErrorIs(t, err, msginsql.ErrSchemaNotReady)
			requireNoConnLeak(t, db)
		})
	})

	t.Run("ShutdownDoesNotRedeliverAllInFlight", func(t *testing.T) {
		ctx := t.Context()
		table := fresh(ctx)

		const n = 3
		for i := 0; i < n; i++ {
			insertJSON(t, ctx, kit, db, table, fmt.Sprintf("inflight-%d", i), "hello")
		}

		db.SetMaxOpenConns(n + 4)
		t.Cleanup(func() { db.SetMaxOpenConns(0) })

		src := newLockSource(t, table)

		var invocations atomic.Int64
		inflight := make(chan struct{}, n)
		release := make(chan struct{})
		handler := func(_ context.Context, _ msgin.Message[string]) error {
			invocations.Add(1)
			inflight <- struct{}{}
			<-release
			return nil
		}

		consumer, err := msgin.NewConsumer[string](src, handler,
			msgin.WithConcurrency[string](n),
			msgin.WithMaxInFlight[string](n),
			msgin.WithPollMaxBatch[string](1),
			msgin.WithPollInterval[string](100*time.Millisecond),
			msgin.WithShutdownTimeout[string](10*time.Second),
		)
		require.NoError(t, err)

		runCtx, cancel := context.WithCancel(ctx)
		errCh := make(chan error, 1)
		go func() { errCh <- consumer.Run(runCtx) }()

		for i := 0; i < n; i++ {
			select {
			case <-inflight:
			case <-time.After(20 * time.Second):
				cancel()
				close(release)
				<-errCh
				t.Fatalf("only %d of %d handlers reached in-flight", i, n)
			}
		}

		cancel()
		close(release)
		require.ErrorIs(t, <-errCh, context.Canceled)

		require.Equal(t, int64(n), invocations.Load(),
			"each in-flight message handled exactly once — none rolled back and redelivered")
		require.Equal(t, 0, rowCount(t, ctx, kit, db, table),
			"every in-flight row was Acked (DELETEd) across shutdown, not redelivered (detached tx)")
		requireNoConnLeak(t, db)
	})

	t.Run("SeparatePoolDLQDrainsWithoutDeadlock", func(t *testing.T) {
		ctx := t.Context()

		db.SetMaxOpenConns(1)
		t.Cleanup(func() { db.SetMaxOpenConns(0) })

		srcTable := fresh(ctx)
		insertJSON(t, ctx, kit, db, srcTable, "poison-dlq", "boom")

		dlqDB := kit.OpenDB(t)
		dlqTable := fmt.Sprintf("msgin_lock_dlq_%d", counter.Add(1))
		require.NoError(t, kit.Lease.EnsureSchema(ctx, dlqDB, dlqTable))
		dlq, err := msginsql.NewOutboundAdapter(dlqDB, dlqTable, kit.Lease)
		require.NoError(t, err)

		src := newLockSource(t, srcTable)

		handler := func(_ context.Context, _ msgin.Message[string]) error {
			return fmt.Errorf("always fails → dead-letter")
		}
		done := make(chan struct{})
		var once sync.Once
		consumer, err := msgin.NewConsumer[string](src, handler,
			msgin.WithConcurrency[string](1),
			msgin.WithMaxInFlight[string](1),
			msgin.WithPollMaxBatch[string](1),
			msgin.WithPollInterval[string](100*time.Millisecond),
			msgin.WithShutdownTimeout[string](10*time.Second),
			msgin.WithRetryPolicy[string](msgin.RetryPolicy{MaxAttempts: 1, DeadLetter: dlq}),
			msgin.WithHooks[string](msgin.Hooks{
				OnDeadLetter: func(_ context.Context, _ msgin.Message[any], _ error) {
					once.Do(func() { close(done) })
				},
			}),
		)
		require.NoError(t, err)

		require.ErrorIs(t, runConsumerUntil(t, ctx, consumer, done, 30*time.Second), context.Canceled)

		require.Equal(t, 0, rowCount(t, ctx, kit, db, srcTable), "the poison row is Acked away after dead-letter")
		require.Equal(t, 1, rowCount(t, ctx, kit, dlqDB, dlqTable), "the message landed in the separate-pool DLQ")
		requireNoConnLeak(t, db)
		requireNoConnLeak(t, dlqDB)
	})

	t.Run("CorruptHeadersLockRowSkippedAndTxClosed", func(t *testing.T) {
		ctx := t.Context()
		table := fresh(ctx)

		_, err := db.ExecContext(ctx,
			fmt.Sprintf(`INSERT INTO %s (msg_id, headers, payload, visible_after) VALUES (%s, %s, %s, %s)`,
				kit.Quote(table), kit.Placeholder(1), kit.Placeholder(2), kit.Placeholder(3), kit.NowExpr()),
			"corrupt-lock-1", `[1,2,3]`, []byte(`"x"`))
		require.NoError(t, err)

		logger, logs := newRecorder()
		src := newLockSource(t, table, msginsql.WithLogger(logger))

		deliveries, pollErr := src.Poll(ctx, 10)
		require.NoError(t, pollErr, "one bad row must not fail the whole Poll")
		require.Empty(t, deliveries, "the undecodable row is skipped, not delivered")
		require.Contains(t, logs.String(), "undecodable headers", "the skip is logged")
		requireNoConnLeak(t, db)

		var (
			lockedAt      sql.NullTime
			futureVisible bool
		)
		require.NoError(t, db.QueryRowContext(ctx,
			fmt.Sprintf(`SELECT locked_at, visible_after > %s FROM %s WHERE msg_id = %s`,
				kit.NowExpr(), kit.Quote(table), kit.Placeholder(1)), "corrupt-lock-1").
			Scan(&lockedAt, &futureVisible))
		require.False(t, lockedAt.Valid, "the skipped row's lock must be released")
		require.True(t, futureVisible, "the corrupt row is penalized into the future")

		insertJSON(t, ctx, kit, db, table, "good-lock-1", "hello")
		next, err := src.Poll(ctx, 10)
		require.NoError(t, err)
		require.Len(t, next, 1, "the good row is claimed past the penalized corrupt one")
		require.Equal(t, "good-lock-1", next[0].Msg.ID())
		require.NoError(t, next[0].Ack(ctx))
		requireNoConnLeak(t, db)
	})

	t.Run("NackLockRequeueFalseCollapsesToImmediate", func(t *testing.T) {
		ctx := t.Context()
		table := fresh(ctx)
		insertJSON(t, ctx, kit, db, table, "collapse-lock-1", "hello")

		src := newLockSource(t, table)

		deliveries, err := src.Poll(ctx, 1)
		require.NoError(t, err)
		require.Len(t, deliveries, 1)

		require.NoError(t, deliveries[0].Nack(ctx, false, 10*time.Second))
		requireNoConnLeak(t, db)

		reclaimed, err := src.Poll(ctx, 1)
		require.NoError(t, err)
		require.Len(t, reclaimed, 1, "requeue=false collapses to delay 0, making the row immediately visible")
		dc, ok := reclaimed[0].Msg.Headers().Int(msgin.HeaderDeliveryCount)
		require.True(t, ok)
		require.Equal(t, 2, dc, "the committed Nack persisted the first ++, so the re-claim reports 2")
		require.NoError(t, reclaimed[0].Ack(ctx))
		requireNoConnLeak(t, db)
	})

	t.Run("DeadlineNackRollsBackAndReleasesConn", func(t *testing.T) {
		ctx := t.Context()
		table := fresh(ctx)
		insertJSON(t, ctx, kit, db, table, "deadline-1", "hello")

		db.SetMaxOpenConns(4)
		t.Cleanup(func() { db.SetMaxOpenConns(0) })

		src := newLockSource(t, table)

		var invocations atomic.Int64
		inflight := make(chan struct{}, 1)
		handler := func(hctx context.Context, _ msgin.Message[string]) error {
			invocations.Add(1)
			inflight <- struct{}{}
			<-hctx.Done()
			return hctx.Err()
		}

		consumer, err := msgin.NewConsumer[string](src, handler,
			msgin.WithConcurrency[string](1),
			msgin.WithMaxInFlight[string](1),
			msgin.WithPollMaxBatch[string](1),
			msgin.WithPollInterval[string](100*time.Millisecond),
			msgin.WithShutdownTimeout[string](200*time.Millisecond),
		)
		require.NoError(t, err)

		runCtx, cancel := context.WithCancel(ctx)
		errCh := make(chan error, 1)
		go func() { errCh <- consumer.Run(runCtx) }()

		select {
		case <-inflight:
		case <-time.After(20 * time.Second):
			cancel()
			<-errCh
			t.Fatal("the handler never reached in-flight")
		}

		cancel()
		require.ErrorIs(t, <-errCh, context.Canceled)
		require.Equal(t, int64(1), invocations.Load(), "the handler ran exactly once")

		requireNoConnLeak(t, db)

		require.Equal(t, 1, rowCount(t, ctx, kit, db, table),
			"the deadline Nack rolled back: the row is neither DELETEd nor its Nack committed")

		reclaimed, err := src.Poll(ctx, 1)
		require.NoError(t, err)
		require.Len(t, reclaimed, 1, "the row is immediately re-claimable (lock released, visible_after not pushed)")
		dc, ok := reclaimed[0].Msg.Headers().Int(msgin.HeaderDeliveryCount)
		require.True(t, ok)
		require.Equal(t, 1, dc, "the in-flight ++ was rolled back at the deadline (crash-equivalent); re-claim reports 1, not 2")
		require.NoError(t, reclaimed[0].Ack(ctx))
		require.Equal(t, 0, rowCount(t, ctx, kit, db, table))
		requireNoConnLeak(t, db)
	})
}
