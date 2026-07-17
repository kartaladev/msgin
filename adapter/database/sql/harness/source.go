package harness

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	msgin "github.com/kartaladev/msgin"
	msginsql "github.com/kartaladev/msgin/adapter/database/sql"
	"github.com/stretchr/testify/require"
)

// RunSource exercises the lease/claim msginsql.Source end-to-end (through the
// real msgin runtime, NewConsumer + Run) and at the fenced-settle level,
// against kit.Lease on the already-open db. It reproduces every assertion the
// pre-split SourceSuite made (Plan 006 Task 3): the happy at-least-once path,
// climbing delivery_count on redelivery, the stale-lease fence, Ready/Poll
// ErrSchemaNotReady, the corrupt-headers penalty-Nack skip, the
// requeue=false collapse, ErrStaleLease on both Ack and Nack, WithLockedBy,
// and raw-driver-error passthrough under a canceled context. Each subtest
// provisions its own fresh table (kit.Lease.EnsureSchema), so subtests never
// interfere with one another.
//
// Concurrency assertions here are CORRECTNESS-only (a fenced settle either
// applies or reports ErrStaleLease; a redelivered row is never lost) — never a
// non-blocking/throughput assumption — so a single-writer engine passes the
// same contract unchanged (audit F8).
func RunSource(t *testing.T, kit TestKit, db *sql.DB) {
	t.Helper()
	var counter atomic.Int64
	fresh := func(ctx context.Context) string { return freshTable(t, ctx, kit, db, &counter, "msgin_src") }

	t.Run("ConsumeAllRowsAckedAndDeleted", func(t *testing.T) {
		ctx := t.Context()
		table := fresh(ctx)

		const n = 6
		want := map[string]string{}
		for i := 0; i < n; i++ {
			id := fmt.Sprintf("m-%d", i)
			payload := fmt.Sprintf("payload-%d", i)
			want[id] = payload
			insertJSON(t, ctx, kit, db, table, id, payload)
		}

		src, err := msginsql.NewPollingSource(db, table, kit.Lease)
		require.NoError(t, err)

		var mu sync.Mutex
		got := map[string]string{}
		var once sync.Once
		done := make(chan struct{})
		handler := func(_ context.Context, msg msgin.Message[string]) error {
			mu.Lock()
			got[msg.ID()] = msg.Payload()
			full := len(got) == n
			mu.Unlock()
			if full {
				once.Do(func() { close(done) })
			}
			return nil
		}

		consumer, err := msgin.NewConsumer[string](src, handler,
			msgin.WithConcurrency[string](3),
			msgin.WithMaxInFlight[string](4),
			msgin.WithPollInterval[string](200*time.Millisecond),
			msgin.WithShutdownTimeout[string](10*time.Second),
		)
		require.NoError(t, err)

		runErr := runConsumerUntil(t, ctx, consumer, done, 30*time.Second)
		require.ErrorIs(t, runErr, context.Canceled)

		mu.Lock()
		require.Equal(t, want, got, "every produced message handled exactly once")
		mu.Unlock()
		require.Equal(t, 0, rowCount(t, ctx, kit, db, table), "all Acked rows must be DELETEd")
	})

	t.Run("HandlerErrorRequeuesAndRedeliversWithClimbingCount", func(t *testing.T) {
		ctx := t.Context()
		table := fresh(ctx)
		insertJSON(t, ctx, kit, db, table, "retry-1", "hello")

		src, err := msginsql.NewPollingSource(db, table, kit.Lease)
		require.NoError(t, err)

		var mu sync.Mutex
		var counts []int
		done := make(chan struct{})
		var once sync.Once
		handler := func(_ context.Context, msg msgin.Message[string]) error {
			dc, _ := msg.Headers().Int(msgin.HeaderDeliveryCount)
			mu.Lock()
			counts = append(counts, dc)
			mu.Unlock()
			if dc < 2 {
				return fmt.Errorf("transient failure on attempt %d", dc)
			}
			once.Do(func() { close(done) })
			return nil
		}

		consumer, err := msgin.NewConsumer[string](src, handler,
			msgin.WithRetryPolicy[string](msgin.RetryPolicy{
				Backoff: msgin.ExponentialBackoff{Initial: 300 * time.Millisecond, Mult: 1},
			}),
			msgin.WithPollInterval[string](200*time.Millisecond),
			msgin.WithShutdownTimeout[string](10*time.Second),
		)
		require.NoError(t, err)

		runErr := runConsumerUntil(t, ctx, consumer, done, 30*time.Second)
		require.ErrorIs(t, runErr, context.Canceled)

		mu.Lock()
		require.GreaterOrEqual(t, len(counts), 2, "message delivered at least twice")
		require.Equal(t, 1, counts[0], "first delivery reports delivery_count 1")
		require.Equal(t, 2, counts[1], "redelivery reports the climbed delivery_count 2 from the DB")
		mu.Unlock()
		require.Equal(t, 0, rowCount(t, ctx, kit, db, table), "row Acked after the successful redelivery")
	})

	t.Run("StaleLeaseFencePreventsDoubleSettle", func(t *testing.T) {
		ctx := t.Context()
		table := fresh(ctx)
		insertJSON(t, ctx, kit, db, table, "slow-1", "hello")

		logger, logs := newRecorder()
		src, err := msginsql.NewPollingSource(db, table, kit.Lease,
			msginsql.WithLeaseTTL(1*time.Second),
			msginsql.WithLogger(logger),
		)
		require.NoError(t, err)

		var acks atomic.Int64
		handler := func(_ context.Context, msg msgin.Message[string]) error {
			dc, _ := msg.Headers().Int(msgin.HeaderDeliveryCount)
			if dc == 1 {
				time.Sleep(4 * time.Second)
			}
			return nil
		}

		consumer, err := msgin.NewConsumer[string](src, handler,
			msgin.WithConcurrency[string](2),
			msgin.WithMaxInFlight[string](2),
			msgin.WithPollInterval[string](300*time.Millisecond),
			msgin.WithShutdownTimeout[string](10*time.Second),
			msgin.WithLogger[string](logger),
			msgin.WithHooks[string](msgin.Hooks{
				OnAck: func(_ context.Context, _ msgin.Message[any], _ error) { acks.Add(1) },
			}),
		)
		require.NoError(t, err)

		runCtx, cancel := context.WithCancel(ctx)
		errCh := make(chan error, 1)
		go func() { errCh <- consumer.Run(runCtx) }()

		require.Eventually(t, func() bool {
			return strings.Contains(logs.String(), "stale")
		}, 15*time.Second, 100*time.Millisecond, "expected a stale-lease WARN from the fenced Ack")

		cancel()
		require.ErrorIs(t, <-errCh, context.Canceled)

		require.Equal(t, int64(1), acks.Load(), "OnAck fires once (real settle); the phantom stale Ack is suppressed")
		require.Equal(t, 0, rowCount(t, ctx, kit, db, table), "row deleted exactly once; no double-delete")
	})

	t.Run("ReadyAndPollSurfaceSchemaNotReady", func(t *testing.T) {
		ctx := t.Context()
		table := fmt.Sprintf("msgin_src_missing_%d", counter.Add(1))
		src, err := msginsql.NewPollingSource(db, table, kit.Lease)
		require.NoError(t, err)

		require.ErrorIs(t, src.Ready(ctx), msginsql.ErrSchemaNotReady)

		deliveries, pollErr := src.Poll(ctx, 10)
		require.Nil(t, deliveries, "no deliveries alongside an error (Poll contract)")
		require.ErrorIs(t, pollErr, msginsql.ErrSchemaNotReady)
		require.Contains(t, pollErr.Error(), table, "error names the offending table")
	})

	t.Run("ReadyOKAndEnsureSchemaIdempotent", func(t *testing.T) {
		ctx := t.Context()
		table := fmt.Sprintf("msgin_src_ready_%d", counter.Add(1))
		src, err := msginsql.NewPollingSource(db, table, kit.Lease)
		require.NoError(t, err)

		require.NoError(t, src.EnsureSchema(ctx))
		require.NoError(t, src.Ready(ctx))
		require.NoError(t, src.EnsureSchema(ctx), "EnsureSchema must be idempotent")
		require.NoError(t, src.Ready(ctx))
	})

	t.Run("CorruptHeadersRowSkipped", func(t *testing.T) {
		ctx := t.Context()
		table := fresh(ctx)

		_, err := db.ExecContext(ctx,
			fmt.Sprintf(`INSERT INTO %s (msg_id, headers, payload, visible_after) VALUES (%s, %s, %s, %s)`,
				kit.Quote(table), kit.Placeholder(1), kit.Placeholder(2), kit.Placeholder(3), kit.NowExpr()),
			"corrupt-1", `[1,2,3]`, []byte(`"x"`),
		)
		require.NoError(t, err)

		logger, logs := newRecorder()
		src, err := msginsql.NewPollingSource(db, table, kit.Lease, msginsql.WithLogger(logger))
		require.NoError(t, err)

		deliveries, pollErr := src.Poll(ctx, 10)
		require.NoError(t, pollErr, "one bad row must not fail the whole Poll")
		require.Empty(t, deliveries, "the undecodable row is skipped, not delivered")
		require.Contains(t, logs.String(), "undecodable headers", "the skip is logged")

		var (
			lockedAt      sql.NullTime
			futureVisible bool
		)
		require.NoError(t, db.QueryRowContext(ctx,
			fmt.Sprintf(`SELECT locked_at, visible_after > %s FROM %s WHERE msg_id = %s`,
				kit.NowExpr(), kit.Quote(table), kit.Placeholder(1)), "corrupt-1").
			Scan(&lockedAt, &futureVisible))
		require.False(t, lockedAt.Valid, "the skipped row's lease must be released")
		require.True(t, futureVisible, "the corrupt row must be penalized into the future, not re-claimable now")

		insertJSON(t, ctx, kit, db, table, "good-1", "hello")
		next, err := src.Poll(ctx, 10)
		require.NoError(t, err)
		require.Len(t, next, 1, "the good row is claimed; the penalized corrupt row is skipped over")
		require.Equal(t, "good-1", next[0].Msg.ID(), "only the good row comes back, not the corrupt one")
	})

	t.Run("NackClosureRequeueFalseCollapsesToImmediate", func(t *testing.T) {
		ctx := t.Context()
		table := fresh(ctx)
		insertJSON(t, ctx, kit, db, table, "collapse-1", "hello")

		src, err := msginsql.NewPollingSource(db, table, kit.Lease)
		require.NoError(t, err)

		deliveries, err := src.Poll(ctx, 1)
		require.NoError(t, err)
		require.Len(t, deliveries, 1)

		require.NoError(t, deliveries[0].Nack(ctx, false, 10*time.Second))

		reclaimed, err := src.Poll(ctx, 1)
		require.NoError(t, err)
		require.Len(t, reclaimed, 1, "requeue=false must collapse to delay 0, making the row immediately visible")
		dc, ok := reclaimed[0].Msg.Headers().Int(msgin.HeaderDeliveryCount)
		require.True(t, ok)
		require.Equal(t, 2, dc, "the re-claim bumped delivery_count")
	})

	t.Run("SettleStaleLeaseReturnsErrStaleLease", func(t *testing.T) {
		ctx := t.Context()

		cases := []struct {
			name   string
			settle func(ctx context.Context, d msgin.Delivery) error
		}{
			{
				name:   "stale Ack",
				settle: func(ctx context.Context, d msgin.Delivery) error { return d.Ack(ctx) },
			},
			{
				name:   "stale Nack",
				settle: func(ctx context.Context, d msgin.Delivery) error { return d.Nack(ctx, true, 0) },
			},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				table := fresh(ctx)
				insertJSON(t, ctx, kit, db, table, "stale-settle", "hello")

				src, err := msginsql.NewPollingSource(db, table, kit.Lease, msginsql.WithLeaseTTL(500*time.Millisecond))
				require.NoError(t, err)

				deliveries, err := src.Poll(ctx, 1) // lease_epoch 1
				require.NoError(t, err)
				require.Len(t, deliveries, 1)

				time.Sleep(1 * time.Second)
				stolen, err := src.Poll(ctx, 1) // lease_epoch 2
				require.NoError(t, err)
				require.Len(t, stolen, 1, "expired lease is re-claimable")

				require.ErrorIs(t, tc.settle(ctx, deliveries[0]), msginsql.ErrStaleLease)

				require.Equal(t, 1, rowCount(t, ctx, kit, db, table))
			})
		}
	})

	t.Run("WithLockedByStampsOwner", func(t *testing.T) {
		ctx := t.Context()
		table := fresh(ctx)
		insertJSON(t, ctx, kit, db, table, "owned-1", "hello")

		const owner = "worker-alpha"
		src, err := msginsql.NewPollingSource(db, table, kit.Lease, msginsql.WithLockedBy(owner))
		require.NoError(t, err)

		deliveries, err := src.Poll(ctx, 1)
		require.NoError(t, err)
		require.Len(t, deliveries, 1)

		var lockedBy string
		require.NoError(t, db.QueryRowContext(ctx,
			fmt.Sprintf(`SELECT locked_by FROM %s WHERE msg_id = %s`, kit.Quote(table), kit.Placeholder(1)), "owned-1").Scan(&lockedBy))
		require.Equal(t, owner, lockedBy, "WithLockedBy must stamp the lease owner")

		require.NoError(t, deliveries[0].Ack(ctx))
		require.Equal(t, 0, rowCount(t, ctx, kit, db, table))
	})

	t.Run("SettleAndProbeUnderCanceledContext", func(t *testing.T) {
		ctx := t.Context()
		table := fresh(ctx)
		insertJSON(t, ctx, kit, db, table, "cancel-1", "hello")

		src, err := msginsql.NewPollingSource(db, table, kit.Lease)
		require.NoError(t, err)

		deliveries, err := src.Poll(ctx, 1)
		require.NoError(t, err)
		require.Len(t, deliveries, 1)

		cctx, cancel := context.WithCancel(ctx)
		cancel()

		ackErr := deliveries[0].Ack(cctx)
		require.Error(t, ackErr)
		require.NotErrorIs(t, ackErr, msginsql.ErrStaleLease, "a DB error is not a fence miss")

		nackErr := deliveries[0].Nack(cctx, true, 0)
		require.Error(t, nackErr)
		require.NotErrorIs(t, nackErr, msginsql.ErrStaleLease)

		require.Error(t, src.Ready(cctx), "Ready surfaces the probe's raw error")

		pollDeliveries, pollErr := src.Poll(cctx, 1)
		require.Nil(t, pollDeliveries)
		require.Error(t, pollErr)
		require.NotErrorIs(t, pollErr, msginsql.ErrSchemaNotReady,
			"a failed probe must not be mis-reported as a missing schema")
	})
}
