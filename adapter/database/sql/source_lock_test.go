package sql_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	msgin "github.com/kartaladev/msgin"
	msginsql "github.com/kartaladev/msgin/adapter/database/sql"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"go.uber.org/goleak"
)

// LockSuite exercises the lock/FOR UPDATE strategy (ADR 0010 D5) end-to-end and
// at the dialect level, over BOTH engines (postgres, mysql) so the same
// assertions prove the tx-carried lock delivery works across dialects. It
// provisions one container per engine for the whole suite (SetupSuite), each test
// using a freshly-named table for isolation. The lock strategy is selected via
// WithStrategy(StrategyLockForUpdate); the dialect auto-detects from the driver.
type LockSuite struct {
	suite.Suite
	engine  engine
	db      *sql.DB
	dialect msginsql.Dialect
	counter atomic.Int64
}

func TestLockSuite(t *testing.T) {
	for _, e := range engines {
		t.Run(e.name, func(t *testing.T) {
			suite.Run(t, &LockSuite{engine: e})
		})
	}
}

func (s *LockSuite) SetupSuite() {
	s.db = s.engine.openDB(s.T())
	s.dialect = s.engine.dialect
}

// freshTable returns a unique, schema-applied table for a single test.
func (s *LockSuite) freshTable(ctx context.Context) string {
	name := fmt.Sprintf("msgin_lock_%d", s.counter.Add(1))
	require.NoError(s.T(), s.dialect.EnsureSchema(ctx, s.db, name))
	return name
}

// insertJSON frames headers carrying id and inserts one immediately-visible
// message whose payload is the JSON encoding of v (matching the consumer codec).
func (s *LockSuite) insertJSON(ctx context.Context, db *sql.DB, table, id string, v any) {
	t := s.T()
	headers, err := msginsql.EncodeHeaders(msgin.NewHeaders(map[string]any{msgin.HeaderID: id}))
	require.NoError(t, err)
	payload, err := json.Marshal(v)
	require.NoError(t, err)
	require.NoError(t, s.dialect.Insert(ctx, db, table, id, headers, payload, 0))
}

// rowCount returns the number of rows in table on db.
func (s *LockSuite) rowCount(ctx context.Context, db *sql.DB, table string) int {
	var n int
	require.NoError(s.T(), db.QueryRowContext(ctx,
		fmt.Sprintf(`SELECT count(*) FROM %s`, s.engine.quote(table))).Scan(&n))
	return n
}

// deliveryCount reads the persisted delivery_count for a row by msg_id.
func (s *LockSuite) deliveryCount(ctx context.Context, table, msgID string) int {
	var dc int
	require.NoError(s.T(), s.db.QueryRowContext(ctx,
		fmt.Sprintf(`SELECT delivery_count FROM %s WHERE msg_id = %s`, s.engine.quote(table), s.engine.ph(1)),
		msgID).Scan(&dc))
	return dc
}

// requireNoConnLeak asserts the pool has no in-use connections at rest — every
// lock tx (commit or rollback) must return its pinned connection.
func (s *LockSuite) requireNoConnLeak(db *sql.DB) {
	require.Eventually(s.T(), func() bool {
		return db.Stats().InUse == 0
	}, 5*time.Second, 20*time.Millisecond, "a lock tx leaked a pooled connection (InUse never returned to 0)")
}

// newLockSource builds a lock-strategy Source over table on s.db (auto-detecting
// the dialect, which implements LockDialect), applying opts.
func (s *LockSuite) newLockSource(table string, opts ...msginsql.Option) *msginsql.Source {
	src, err := msginsql.NewPollingSource(s.db, table,
		append([]msginsql.Option{msginsql.WithStrategy(msginsql.StrategyLockForUpdate)}, opts...)...)
	require.NoError(s.T(), err)
	return src
}

// ---- tests --------------------------------------------------------------

// TestClaimIsSingleRowAndAckCommits: ClaimLock returns at most ONE row even when
// two are claimable and max is large (single-row cap, one conn per in-flight
// message); Ack DELETEs the row and COMMITS the carried tx (an independent
// connection then sees the deletion), leaving no leaked connection.
func (s *LockSuite) TestClaimIsSingleRowAndAckCommits() {
	ctx := s.T().Context()
	t := s.T()
	table := s.freshTable(ctx)
	s.insertJSON(ctx, s.db, table, "lock-a", "hello")
	s.insertJSON(ctx, s.db, table, "lock-b", "world")

	src := s.newLockSource(table)

	deliveries, err := src.Poll(ctx, 10)
	require.NoError(t, err)
	require.Len(t, deliveries, 1, "lock ClaimLock returns at most one row regardless of max")

	require.NoError(t, deliveries[0].Ack(ctx))
	require.Equal(t, 1, s.rowCount(ctx, s.db, table),
		"the Acked row is DELETEd and committed; the other remains")
	s.requireNoConnLeak(s.db)
}

// TestDeliveryCountProgressesAcrossDefaultNackRedeliverLoop is the CRITICAL-2
// regression: with the DEFAULT RetryPolicy (Backoff == nil → delay 0), a handler
// that always fails is redelivered with delivery_count CLIMBING (1, 2, 3), and a
// finite MaxAttempts dead-letters it. This proves NackLock ALWAYS COMMITS (so the
// in-tx delivery_count++ persists across redeliveries) — if Nack rolled back at
// delay 0, the count would stick at 1 forever and MaxAttempts would never advance
// (infinite poison loop, no DLQ).
func (s *LockSuite) TestDeliveryCountProgressesAcrossDefaultNackRedeliverLoop() {
	ctx := s.T().Context()
	t := s.T()
	table := s.freshTable(ctx)
	s.insertJSON(ctx, s.db, table, "poison-1", "boom")

	src := s.newLockSource(table)

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
		// DEFAULT Backoff (nil → delay 0): the exact path that the pre-fix
		// tx-rollback Nack broke. MaxAttempts 3 → dead-letter on the 3rd attempt.
		msgin.WithRetryPolicy[string](msgin.RetryPolicy{MaxAttempts: 3, DeadLetter: dlq}),
		msgin.WithHooks[string](msgin.Hooks{
			OnDeadLetter: func(_ context.Context, _ msgin.Message[any], _ error) {
				once.Do(func() { close(done) })
			},
		}),
	)
	require.NoError(t, err)

	require.ErrorIs(t, s.runUntil(ctx, consumer, done, 30*time.Second), context.Canceled)

	mu.Lock()
	require.Equal(t, []int{1, 2, 3}, counts,
		"delivery_count must CLIMB across default-backoff Nack redeliveries (always-commit fix)")
	mu.Unlock()
	require.Equal(t, 1, dlq.count(), "the exhausted poison message is dead-lettered exactly once")
	require.Equal(t, 0, s.rowCount(ctx, s.db, table), "the dead-lettered row is Acked away")
	s.requireNoConnLeak(s.db)
}

// TestCrashRevertsInFlightAttemptOnly documents the lock strategy's crash
// semantics: a process crash (modelled at the dialect level by rolling back the
// carried tx instead of Ack/Nack) reverts ONLY the in-flight attempt — the
// in-tx delivery_count++ is undone and the row is immediately re-claimable —
// rather than corrupting the persisted count. Contrast the committed-Nack path
// (above), which DOES persist the ++.
func (s *LockSuite) TestCrashRevertsInFlightAttemptOnly() {
	ctx := s.T().Context()
	t := s.T()
	table := s.freshTable(ctx)
	s.insertJSON(ctx, s.db, table, "crash-1", "hello")

	ld, ok := s.dialect.(msginsql.LockDialect)
	require.True(t, ok, "the built-in dialect must implement LockDialect")

	lr1, err := ld.ClaimLock(ctx, s.db, table, "owner")
	require.NoError(t, err)
	require.NotNil(t, lr1)
	require.Equal(t, 1, lr1.DeliveryCount, "first claim bumps delivery_count to 1 (in the tx)")

	// Crash: the carried tx is rolled back (never Ack/Nack), undoing the ++.
	require.NoError(t, lr1.Tx.Rollback())
	s.requireNoConnLeak(s.db)

	// The row is re-claimable and its delivery_count reverted to the pre-claim
	// value, so the next claim again reports 1 (not 2).
	lr2, err := ld.ClaimLock(ctx, s.db, table, "owner")
	require.NoError(t, err)
	require.NotNil(t, lr2, "the crashed row is immediately re-claimable")
	require.Equal(t, 1, lr2.DeliveryCount, "the crashed ++ was reverted; re-claim reports 1, not 2")

	require.NoError(t, ld.AckLock(ctx, lr2, table)) // clean up + release the conn
	require.Equal(t, 0, s.rowCount(ctx, s.db, table))
	s.requireNoConnLeak(s.db)
}

// TestClaimEmptyAndErrorRollBackNoConnLeak proves the tx is closed (rolled back)
// and the pooled connection released on BOTH the empty-claim path and the
// error path — the Poll no-partial-result / owns-cleanup contract. A discarded
// lock claim must never leak a connection.
func (s *LockSuite) TestClaimEmptyAndErrorRollBackNoConnLeak() {
	ctx := s.T().Context()
	t := s.T()

	s.Run("empty claim rolls back", func() {
		table := s.freshTable(ctx) // provisioned but empty
		src := s.newLockSource(table)

		deliveries, err := src.Poll(ctx, 10)
		require.NoError(t, err)
		require.Empty(t, deliveries, "nothing claimable → empty batch")
		s.requireNoConnLeak(s.db)
	})

	s.Run("claim error rolls back", func() {
		// A valid identifier that was never created: ClaimLock's SELECT fails, the
		// tx is rolled back, and Poll surfaces ErrSchemaNotReady (portable probe) —
		// with no leaked connection.
		table := fmt.Sprintf("msgin_lock_missing_%d", s.counter.Add(1))
		src := s.newLockSource(table)

		deliveries, err := src.Poll(ctx, 10)
		require.Nil(t, deliveries, "no deliveries alongside an error (Poll contract)")
		require.ErrorIs(t, err, msginsql.ErrSchemaNotReady)
		s.requireNoConnLeak(s.db)
	})
}

// TestShutdownDoesNotRedeliverAllInFlight is the HIGH-3 regression: the claim tx
// is begun on a cancellation-DETACHED context, so graceful shutdown does NOT
// auto-rollback every in-flight lock tx before the drain deadline. It drives a
// real Run with blocking handlers, cancels mid-flight, then releases the
// handlers: every in-flight message Acks (its row DELETEd) within the drain
// deadline rather than rolling back and redelivering. If the tx were begun on the
// poll/parent ctx, the cancel would roll them all back and the rows would remain.
func (s *LockSuite) TestShutdownDoesNotRedeliverAllInFlight() {
	ctx := s.T().Context()
	t := s.T()
	table := s.freshTable(ctx)

	const n = 3
	for i := 0; i < n; i++ {
		s.insertJSON(ctx, s.db, table, fmt.Sprintf("inflight-%d", i), "hello")
	}

	// Generous pool: n pinned claim conns + poll headroom.
	s.db.SetMaxOpenConns(n + 4)
	t.Cleanup(func() { s.db.SetMaxOpenConns(0) }) // restore unlimited for later tests

	src := s.newLockSource(table)

	var invocations atomic.Int64
	inflight := make(chan struct{}, n)
	release := make(chan struct{})
	handler := func(_ context.Context, _ msgin.Message[string]) error {
		invocations.Add(1)
		inflight <- struct{}{} // signal: this delivery is dispatched and about to block
		<-release              // block until the test releases us (after cancel)
		return nil             // success → Ack (DELETE + commit) on settleCtx
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

	// Wait until all n handlers are dispatched and blocked (in flight).
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

	// Shut down WHILE all n are in flight, then release them. With the detached
	// claim tx, the in-flight settles survive the parent-cancel and Ack within the
	// drain deadline.
	cancel()
	close(release)
	require.ErrorIs(t, <-errCh, context.Canceled)

	require.Equal(t, int64(n), invocations.Load(),
		"each in-flight message handled exactly once — none rolled back and redelivered")
	require.Equal(t, 0, s.rowCount(ctx, s.db, table),
		"every in-flight row was Acked (DELETEd) across shutdown, not redelivered (detached tx)")
	s.requireNoConnLeak(s.db)
}

// TestSeparatePoolDLQDrainsWithoutDeadlock is the same-pool-DLQ stall regression
// (ADR 0010 D5). A lock consumer pins its claim connection for the message
// lifetime and the runtime calls the DLQ sink's Send BEFORE it Acks (freeing the
// conn), so a sql DLQ on the SAME pool would stall an exhausted pool. Giving the
// DLQ a SEPARATE *sql.DB (here, a separate container/pool) lets a poison message
// dead-letter without deadlock, even with the source pool capped at one
// connection.
func (s *LockSuite) TestSeparatePoolDLQDrainsWithoutDeadlock() {
	ctx := s.T().Context()
	t := s.T()

	// Source pool capped at ONE connection: the single claim tx pins it, so a
	// same-pool sql DLQ would 100% stall. The separate DLQ pool must avoid it.
	s.db.SetMaxOpenConns(1)
	t.Cleanup(func() { s.db.SetMaxOpenConns(0) })

	srcTable := s.freshTable(ctx)
	s.insertJSON(ctx, s.db, srcTable, "poison-dlq", "boom")

	// A SEPARATE pool (separate container) for the DLQ sink, per the D5 mandate.
	dlqDB := s.engine.openDB(t)
	dlqTable := fmt.Sprintf("msgin_lock_dlq_%d", s.counter.Add(1))
	require.NoError(t, s.dialect.EnsureSchema(ctx, dlqDB, dlqTable))
	dlq, err := msginsql.NewOutboundAdapter(dlqDB, dlqTable, msginsql.WithDialect(s.dialect))
	require.NoError(t, err)

	src := s.newLockSource(srcTable)

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

	require.ErrorIs(t, s.runUntil(ctx, consumer, done, 30*time.Second), context.Canceled)

	require.Equal(t, 0, s.rowCount(ctx, s.db, srcTable), "the poison row is Acked away after dead-letter")
	require.Equal(t, 1, s.rowCount(ctx, dlqDB, dlqTable), "the message landed in the separate-pool DLQ")
	s.requireNoConnLeak(s.db)
	s.requireNoConnLeak(dlqDB)
}

// TestCorruptHeadersLockRowSkippedAndTxClosed: an undecodable-headers row under
// the lock strategy is NackLocked (which COMMITS/closes its carried tx — no conn
// leak) with a penalty, then skipped (empty batch), and a subsequent good row is
// still claimable past the penalized one. Mirrors the lease corrupt-row path but
// additionally asserts the carried tx is closed (InUse → 0).
func (s *LockSuite) TestCorruptHeadersLockRowSkippedAndTxClosed() {
	ctx := s.T().Context()
	t := s.T()
	table := s.freshTable(ctx)

	// A JSON array in the headers column: valid JSON the DB accepts, but
	// DecodeHeaders cannot unmarshal into a header map.
	_, err := s.db.ExecContext(ctx,
		fmt.Sprintf(`INSERT INTO %s (msg_id, headers, payload, visible_after) VALUES (%s, %s, %s, %s)`,
			s.engine.quote(table), s.engine.ph(1), s.engine.ph(2), s.engine.ph(3), s.engine.nowExpr()),
		"corrupt-lock-1", `[1,2,3]`, []byte(`"x"`))
	require.NoError(t, err)

	logger, logs := newRecorder()
	src := s.newLockSource(table, msginsql.WithLogger(logger))

	deliveries, pollErr := src.Poll(ctx, 10)
	require.NoError(t, pollErr, "one bad row must not fail the whole Poll")
	require.Empty(t, deliveries, "the undecodable row is skipped, not delivered")
	require.Contains(t, logs.String(), "undecodable headers", "the skip is logged")
	s.requireNoConnLeak(s.db) // the NackLock committed/closed the carried tx

	// The row still exists (not dropped), its lock released, visible_after pushed
	// into the future by the penalty.
	var (
		lockedAt      sql.NullTime
		futureVisible bool
	)
	require.NoError(t, s.db.QueryRowContext(ctx,
		fmt.Sprintf(`SELECT locked_at, visible_after > %s FROM %s WHERE msg_id = %s`,
			s.engine.nowExpr(), s.engine.quote(table), s.engine.ph(1)), "corrupt-lock-1").
		Scan(&lockedAt, &futureVisible))
	require.False(t, lockedAt.Valid, "the skipped row's lock must be released")
	require.True(t, futureVisible, "the corrupt row is penalized into the future")

	// A good row inserted after is claimed on the next poll; the penalized corrupt
	// row does not block it and does not re-appear.
	s.insertJSON(ctx, s.db, table, "good-lock-1", "hello")
	next, err := src.Poll(ctx, 10)
	require.NoError(t, err)
	require.Len(t, next, 1, "the good row is claimed past the penalized corrupt one")
	require.Equal(t, "good-lock-1", next[0].Msg.ID())
	require.NoError(t, next[0].Ack(ctx))
	s.requireNoConnLeak(s.db)
}

// TestNackLockRequeueFalseCollapsesToImmediate proves the D4/D5 collapse on the
// lock delivery: an at-least-once source cannot drop, so Nack(requeue=false,
// delay) collapses to Nack(requeue=true, 0) — the row is made immediately
// re-claimable regardless of the (ignored) delay. It also reconfirms the
// always-commit persistence: the re-claim reports delivery_count 2 (the first
// committed Nack persisted the ++), and the carried tx is closed (no conn leak).
func (s *LockSuite) TestNackLockRequeueFalseCollapsesToImmediate() {
	ctx := s.T().Context()
	t := s.T()
	table := s.freshTable(ctx)
	s.insertJSON(ctx, s.db, table, "collapse-lock-1", "hello")

	src := s.newLockSource(table)

	deliveries, err := src.Poll(ctx, 1)
	require.NoError(t, err)
	require.Len(t, deliveries, 1)

	// requeue=false with a large delay: the delay MUST be ignored (collapsed to 0),
	// so the row is immediately re-claimable rather than hidden for 10s.
	require.NoError(t, deliveries[0].Nack(ctx, false, 10*time.Second))
	s.requireNoConnLeak(s.db)

	reclaimed, err := src.Poll(ctx, 1)
	require.NoError(t, err)
	require.Len(t, reclaimed, 1, "requeue=false collapses to delay 0, making the row immediately visible")
	dc, ok := reclaimed[0].Msg.Headers().Int(msgin.HeaderDeliveryCount)
	require.True(t, ok)
	require.Equal(t, 2, dc, "the committed Nack persisted the first ++, so the re-claim reports 2")
	require.NoError(t, reclaimed[0].Ack(ctx))
	s.requireNoConnLeak(s.db)
}

// TestNoGoroutineLeakAfterCleanLockRun drives a full lock-strategy consumer
// lifecycle and asserts (via a baseline snapshot taken after the container is up)
// that no msgin goroutine — and no pinned connection — outlives Run.
func (s *LockSuite) TestNoGoroutineLeakAfterCleanLockRun() {
	ctx := s.T().Context()
	t := s.T()
	table := s.freshTable(ctx)
	s.insertJSON(ctx, s.db, table, "lock-leak-1", "hello")

	baseline := goleak.IgnoreCurrent()

	src := s.newLockSource(table)

	done := make(chan struct{})
	var once sync.Once
	handler := func(_ context.Context, _ msgin.Message[string]) error {
		once.Do(func() { close(done) })
		return nil
	}
	consumer, err := msgin.NewConsumer[string](src, handler,
		msgin.WithPollMaxBatch[string](1),
		msgin.WithPollInterval[string](100*time.Millisecond),
		msgin.WithShutdownTimeout[string](10*time.Second),
	)
	require.NoError(t, err)

	require.ErrorIs(t, s.runUntil(ctx, consumer, done, 30*time.Second), context.Canceled)

	opts := []goleak.Option{baseline}
	if s.engine.isMySQL() {
		opts = append(opts, goleak.IgnoreAnyFunction("github.com/go-sql-driver/mysql.(*mysqlConn).startWatcher.func1"))
	}
	goleak.VerifyNone(t, opts...)
	s.requireNoConnLeak(s.db)
}

// TestDeadlineNackRollsBackAndReleasesConn is the path-5 regression: it FORCES
// the shutdown-deadline Nack arm that TestShutdownDoesNotRedeliverAllInFlight
// never reaches (that test releases its handlers so they Ack BEFORE the
// deadline). Here the handler blocks on its own ctx.Done() and unblocks only
// when the drain deadline cancels drainCtx; the runtime then Nacks the in-flight
// delivery on the ALREADY-cancelled settleCtx, so NackLock's ExecContext fails on
// the cancelled ctx and settleLockTx must tx.Rollback and release the pinned
// pooled connection. This exercises the settleLockTx exec-error → rollback arm.
//
// The deadline rollback is crash-equivalent: the FOR UPDATE lock is released, the
// in-flight delivery_count++ is reverted (never committed), and the row is neither
// DELETEd nor its Nack committed — so it stays present and immediately
// re-claimable, and the re-claim reports delivery_count 1 (not 2). Crucially,
// db.Stats().InUse returns to 0: no connection leaks at the deadline.
func (s *LockSuite) TestDeadlineNackRollsBackAndReleasesConn() {
	ctx := s.T().Context()
	t := s.T()
	table := s.freshTable(ctx)
	s.insertJSON(ctx, s.db, table, "deadline-1", "hello")

	// Generous pool so the single claim conn + poll headroom never starve; the
	// point is the deadline rollback, not pool exhaustion.
	s.db.SetMaxOpenConns(4)
	t.Cleanup(func() { s.db.SetMaxOpenConns(0) })

	src := s.newLockSource(table)

	var invocations atomic.Int64
	inflight := make(chan struct{}, 1)
	handler := func(hctx context.Context, _ msgin.Message[string]) error {
		invocations.Add(1)
		inflight <- struct{}{} // signal: dispatched and about to block
		<-hctx.Done()          // unblocks only when the drain deadline cancels drainCtx
		return hctx.Err()      // transient → runtime Nacks on the (now-cancelled) settleCtx
	}

	consumer, err := msgin.NewConsumer[string](src, handler,
		msgin.WithConcurrency[string](1),
		msgin.WithMaxInFlight[string](1),
		msgin.WithPollMaxBatch[string](1),
		msgin.WithPollInterval[string](100*time.Millisecond),
		// SHORT shutdown timeout: the drain deadline fires while the handler is
		// still in flight, forcing the deadline-Nack-on-cancelled-settleCtx arm.
		msgin.WithShutdownTimeout[string](200*time.Millisecond),
	)
	require.NoError(t, err)

	runCtx, cancel := context.WithCancel(ctx)
	errCh := make(chan error, 1)
	go func() { errCh <- consumer.Run(runCtx) }()

	// Wait until the handler is dispatched and blocked (in flight).
	select {
	case <-inflight:
	case <-time.After(20 * time.Second):
		cancel()
		<-errCh
		t.Fatal("the handler never reached in-flight")
	}

	// Cancel Run WHILE the handler is in flight; the 200ms deadline then fires,
	// cancels drainCtx (unblocking the handler) and settleCtx, and the transient
	// Nack runs on the cancelled settleCtx → settleLockTx rollback.
	cancel()
	require.ErrorIs(t, <-errCh, context.Canceled)
	require.Equal(t, int64(1), invocations.Load(), "the handler ran exactly once")

	// The deadline rollback released the pinned conn — no leak at the deadline.
	s.requireNoConnLeak(s.db)

	// Crash-equivalent: neither DELETEd nor committed-Nacked — the row is still
	// present, its lock released, and immediately re-claimable with the ++ reverted.
	require.Equal(t, 1, s.rowCount(ctx, s.db, table),
		"the deadline Nack rolled back: the row is neither DELETEd nor its Nack committed")

	reclaimed, err := src.Poll(ctx, 1)
	require.NoError(t, err)
	require.Len(t, reclaimed, 1, "the row is immediately re-claimable (lock released, visible_after not pushed)")
	dc, ok := reclaimed[0].Msg.Headers().Int(msgin.HeaderDeliveryCount)
	require.True(t, ok)
	require.Equal(t, 1, dc, "the in-flight ++ was rolled back at the deadline (crash-equivalent); re-claim reports 1, not 2")
	require.NoError(t, reclaimed[0].Ack(ctx))
	require.Equal(t, 0, s.rowCount(ctx, s.db, table))
	s.requireNoConnLeak(s.db)
}

// runUntil delegates to the shared runConsumerUntil helper.
func (s *LockSuite) runUntil(ctx context.Context, consumer msgin.Consumer[string], done <-chan struct{}, deadline time.Duration) error {
	return runConsumerUntil(s.T(), ctx, consumer, done, deadline)
}

// recordingSink is a concurrency-safe in-memory OutboundAdapter used as a DLQ in
// the CRITICAL-2 test, where the sink must not add a pool dependency.
type recordingSink struct {
	mu   sync.Mutex
	msgs []msgin.Message[any]
}

func (r *recordingSink) Send(_ context.Context, msg msgin.Message[any]) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.msgs = append(r.msgs, msg)
	return nil
}

func (r *recordingSink) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.msgs)
}
