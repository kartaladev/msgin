package sql_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
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

// TestMain runs the package's goroutine-leak check. Locally the testcontainers,
// Docker-client, and HTTP-pool background goroutines settle within goleak's
// retry window (a bare VerifyTestMain passes), but those are timing-dependent
// and can linger past the window on a slower/busier CI host, which would flake
// the whole package. The ignore list below is a DEFENSIVE guard for exactly
// those known container-plumbing top-of-stack functions, so a real leaked msgin
// poll/worker/sweep goroutine is still caught while container plumbing is not.
// TestNoGoroutineLeakAfterCleanRun is the precise, container-plumbing-agnostic
// assertion that no msgin goroutine outlives Run (via an IgnoreCurrent
// baseline), and does not depend on this list (use-testcontainers / ADR 0010).
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m,
		// testcontainers Ryuk reaper connection keep-alive.
		goleak.IgnoreAnyFunction("github.com/testcontainers/testcontainers-go.(*Reaper).connect.func1"),
		// Docker/HTTP client idle connection pool (kept warm across calls).
		goleak.IgnoreAnyFunction("net/http.(*persistConn).readLoop"),
		goleak.IgnoreAnyFunction("net/http.(*persistConn).writeLoop"),
		// Underlying network poller blocking read for the above conns.
		goleak.IgnoreTopFunction("internal/poll.runtime_pollWait"),
	)
}

// ---- recording logger (concurrency-safe) --------------------------------

// syncBuffer is a mutex-guarded io.Writer so a slog.TextHandler written from
// multiple worker goroutines under -race stays safe, and the accumulated text
// can be inspected mid-test.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func newRecorder() (*slog.Logger, *syncBuffer) {
	sb := &syncBuffer{}
	return slog.New(slog.NewTextHandler(sb, &slog.HandlerOptions{Level: slog.LevelWarn})), sb
}

// ---- suite --------------------------------------------------------------

// SourceSuite provisions one container per engine for the whole suite and
// exercises the lease/claim Source end-to-end through the real msgin runtime
// (NewConsumer + Run). TestSourceSuite runs it once per engine (postgres, mysql)
// so the SAME end-to-end assertions prove the Source works over both dialects.
// Each test uses a freshly-named, freshly-provisioned table so cases stay
// isolated. Each constructor call passes the per-engine dialect explicitly.
type SourceSuite struct {
	suite.Suite
	engine  engine
	db      *sql.DB
	dialect msginsql.LeaseDialect
	counter atomic.Int64
}

func TestSourceSuite(t *testing.T) {
	for _, e := range engines {
		t.Run(e.name, func(t *testing.T) {
			suite.Run(t, &SourceSuite{engine: e})
		})
	}
}

func (s *SourceSuite) SetupSuite() {
	s.db = s.engine.openDB(s.T())
	s.dialect = s.engine.dialect
}

// freshTable returns a unique, schema-applied table name for a single test.
func (s *SourceSuite) freshTable(ctx context.Context) string {
	name := fmt.Sprintf("msgin_src_%d", s.counter.Add(1))
	require.NoError(s.T(), s.dialect.EnsureSchema(ctx, s.db, name))
	return name
}

// insertJSON frames headers carrying id and inserts one immediately-visible
// message whose payload is the JSON encoding of v (matching the consumer's
// default JSON codec).
func (s *SourceSuite) insertJSON(ctx context.Context, table, id string, v any) {
	t := s.T()
	headers, err := msginsql.EncodeHeaders(msgin.NewHeaders(map[string]any{msgin.HeaderID: id}))
	require.NoError(t, err)
	payload, err := json.Marshal(v)
	require.NoError(t, err)
	require.NoError(t, s.dialect.Insert(ctx, s.db, table, id, headers, payload, 0))
}

// rowCount returns the number of rows in table.
func (s *SourceSuite) rowCount(ctx context.Context, table string) int {
	var n int
	require.NoError(s.T(), s.db.QueryRowContext(ctx, fmt.Sprintf(`SELECT count(*) FROM %s`, s.engine.quote(table))).Scan(&n))
	return n
}

// ---- tests --------------------------------------------------------------

// TestConsumeAllRowsAckedAndDeleted: N produced rows are all handled once and
// their rows DELETEd (Ack), proving the happy at-least-once path end-to-end.
func (s *SourceSuite) TestConsumeAllRowsAckedAndDeleted() {
	ctx := s.T().Context()
	t := s.T()
	table := s.freshTable(ctx)

	const n = 6
	want := map[string]string{}
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("m-%d", i)
		payload := fmt.Sprintf("payload-%d", i)
		want[id] = payload
		s.insertJSON(ctx, table, id, payload)
	}

	src, err := msginsql.NewPollingSource(s.db, table, s.dialect)
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

	runErr := s.runUntil(ctx, consumer, done, 30*time.Second)
	require.ErrorIs(t, runErr, context.Canceled)

	mu.Lock()
	require.Equal(t, want, got, "every produced message handled exactly once")
	mu.Unlock()
	require.Equal(t, 0, s.rowCount(ctx, table), "all Acked rows must be DELETEd")
}

// TestHandlerErrorRequeuesAndRedeliversWithClimbingCount: a handler that fails
// its first attempt causes a Nack (delayed by the RetryPolicy backoff); the row
// becomes re-visible and is redelivered with the delivery-count header climbing
// from the DB (native redelivery drives the count from the row, not the runtime
// tracker). The second attempt succeeds and the row is Acked away.
func (s *SourceSuite) TestHandlerErrorRequeuesAndRedeliversWithClimbingCount() {
	ctx := s.T().Context()
	t := s.T()
	table := s.freshTable(ctx)

	s.insertJSON(ctx, table, "retry-1", "hello")

	src, err := msginsql.NewPollingSource(s.db, table, s.dialect)
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
			// Constant 300ms backoff so the failed row idles briefly before
			// redelivery (not a hot DB loop) and the delay is observable.
			Backoff: msgin.ExponentialBackoff{Initial: 300 * time.Millisecond, Mult: 1},
		}),
		msgin.WithPollInterval[string](200*time.Millisecond),
		msgin.WithShutdownTimeout[string](10*time.Second),
	)
	require.NoError(t, err)

	runErr := s.runUntil(ctx, consumer, done, 30*time.Second)
	require.ErrorIs(t, runErr, context.Canceled)

	mu.Lock()
	require.GreaterOrEqual(t, len(counts), 2, "message delivered at least twice")
	require.Equal(t, 1, counts[0], "first delivery reports delivery_count 1")
	require.Equal(t, 2, counts[1], "redelivery reports the climbed delivery_count 2 from the DB")
	mu.Unlock()
	require.Equal(t, 0, s.rowCount(ctx, table), "row Acked after the successful redelivery")
}

// TestStaleLeaseFencePreventsDoubleSettle: a handler that outruns a tiny lease
// TTL lets another poll reclaim the row (new lease_epoch). When the slow worker
// finally Acks, the fence matches no row -> ErrStaleLease, the phantom OnAck is
// suppressed (fires only for the worker that actually re-leased and settled),
// and the row is deleted exactly once (no double-delete).
func (s *SourceSuite) TestStaleLeaseFencePreventsDoubleSettle() {
	ctx := s.T().Context()
	t := s.T()
	table := s.freshTable(ctx)

	s.insertJSON(ctx, table, "slow-1", "hello")

	logger, logs := newRecorder()
	src, err := msginsql.NewPollingSource(s.db, table, s.dialect,
		msginsql.WithLeaseTTL(1*time.Second), // tiny: the slow handler will outrun it
		msginsql.WithLogger(logger),
	)
	require.NoError(t, err)

	var acks atomic.Int64
	handler := func(_ context.Context, msg msgin.Message[string]) error {
		dc, _ := msg.Headers().Int(msgin.HeaderDeliveryCount)
		if dc == 1 {
			// First claim: sleep well past the lease TTL so another poll reclaims
			// the row before this handler returns and (stale-)Acks.
			time.Sleep(4 * time.Second)
		}
		return nil
	}

	consumer, err := msgin.NewConsumer[string](src, handler,
		msgin.WithConcurrency[string](2),
		msgin.WithMaxInFlight[string](2), // spare credit so the poll loop can reclaim while worker 1 is busy
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

	// Wait until the slow worker's Ack has been fenced out (the Source logs the
	// stale-lease WARN), proving the fence fired, then shut down.
	require.Eventually(t, func() bool {
		return strings.Contains(logs.String(), "stale")
	}, 15*time.Second, 100*time.Millisecond, "expected a stale-lease WARN from the fenced Ack")

	cancel()
	require.ErrorIs(t, <-errCh, context.Canceled)

	require.Equal(t, int64(1), acks.Load(), "OnAck fires once (real settle); the phantom stale Ack is suppressed")
	require.Equal(t, 0, s.rowCount(ctx, table), "row deleted exactly once; no double-delete")
}

// TestReadyAndPollSurfaceSchemaNotReady: an un-provisioned table fails fast at
// Ready and, defensively, Poll surfaces the same portable ErrSchemaNotReady.
func (s *SourceSuite) TestReadyAndPollSurfaceSchemaNotReady() {
	ctx := s.T().Context()
	t := s.T()

	// A valid identifier that was never created.
	table := fmt.Sprintf("msgin_missing_%d", s.counter.Add(1))
	src, err := msginsql.NewPollingSource(s.db, table, s.dialect)
	require.NoError(t, err)

	require.ErrorIs(t, src.Ready(ctx), msginsql.ErrSchemaNotReady)

	deliveries, pollErr := src.Poll(ctx, 10)
	require.Nil(t, deliveries, "no deliveries alongside an error (Poll contract)")
	require.ErrorIs(t, pollErr, msginsql.ErrSchemaNotReady)
	require.Contains(t, pollErr.Error(), table, "error names the offending table")
}

// TestReadyOKAndEnsureSchemaIdempotent: once provisioned, Ready returns nil and
// EnsureSchema is a no-op on a second call.
func (s *SourceSuite) TestReadyOKAndEnsureSchemaIdempotent() {
	ctx := s.T().Context()
	t := s.T()

	table := fmt.Sprintf("msgin_ready_%d", s.counter.Add(1))
	src, err := msginsql.NewPollingSource(s.db, table, s.dialect)
	require.NoError(t, err)

	require.NoError(t, src.EnsureSchema(ctx))
	require.NoError(t, src.Ready(ctx))
	require.NoError(t, src.EnsureSchema(ctx), "EnsureSchema must be idempotent")
	require.NoError(t, src.Ready(ctx))
}

// TestCorruptHeadersRowSkipped: a row whose JSONB headers are valid JSON but not
// a JSON object (so DecodeHeaders fails) is skipped by Poll — no crash, no
// stall, not returned as a delivery — and, per the coordinator-review fix,
// Nacked with a NON-zero penalty visible_after so it does not immediately
// re-churn. At-least-once still holds (the row persists, not dropped) and good
// rows keep flowing past the penalized one.
func (s *SourceSuite) TestCorruptHeadersRowSkipped() {
	ctx := s.T().Context()
	t := s.T()
	table := s.freshTable(ctx)

	// Raw INSERT of a JSON array into the JSON/JSONB headers column: valid JSON
	// (so the DB accepts it), but DecodeHeaders cannot unmarshal it into a
	// map[string]any — the defensive corrupt/foreign-row path. visible_after uses
	// the engine's UTC-now expression so the row is immediately claimable under
	// the dialect's own (UTC) visibility predicate.
	_, err := s.db.ExecContext(ctx,
		fmt.Sprintf(`INSERT INTO %s (msg_id, headers, payload, visible_after) VALUES (%s, %s, %s, %s)`,
			s.engine.quote(table), s.engine.ph(1), s.engine.ph(2), s.engine.ph(3), s.engine.nowExpr()),
		"corrupt-1", `[1,2,3]`, []byte(`"x"`),
	)
	require.NoError(t, err)

	logger, logs := newRecorder()
	src, err := msginsql.NewPollingSource(s.db, table, s.dialect, msginsql.WithLogger(logger))
	require.NoError(t, err)

	deliveries, pollErr := src.Poll(ctx, 10)
	require.NoError(t, pollErr, "one bad row must not fail the whole Poll")
	require.Empty(t, deliveries, "the undecodable row is skipped, not delivered")
	require.Contains(t, logs.String(), "undecodable headers", "the skip is logged")

	// At-least-once: the row still exists (not dropped), its lease is released
	// (locked_at cleared), and its visible_after was pushed into the future by the
	// escalating penalty — so it is NOT immediately re-claimable (no hot churn).
	var (
		lockedAt      sql.NullTime
		futureVisible bool
	)
	require.NoError(t, s.db.QueryRowContext(ctx,
		fmt.Sprintf(`SELECT locked_at, visible_after > %s FROM %s WHERE msg_id = %s`,
			s.engine.nowExpr(), s.engine.quote(table), s.engine.ph(1)), "corrupt-1").
		Scan(&lockedAt, &futureVisible))
	require.False(t, lockedAt.Valid, "the skipped row's lease must be released")
	require.True(t, futureVisible, "the corrupt row must be penalized into the future, not re-claimable now")

	// A GOOD row inserted after the corrupt one is claimed on the very next poll
	// (the penalized corrupt row does not block it), and the corrupt row does NOT
	// re-appear (it is invisible for its penalty window).
	s.insertJSON(ctx, table, "good-1", "hello")
	next, err := src.Poll(ctx, 10)
	require.NoError(t, err)
	require.Len(t, next, 1, "the good row is claimed; the penalized corrupt row is skipped over")
	require.Equal(t, "good-1", next[0].Msg.ID(), "only the good row comes back, not the corrupt one")
}

// TestNoGoroutineLeakAfterCleanRun explicitly drives a full consumer lifecycle
// and asserts, via a baseline snapshot taken AFTER the container is up, that no
// msgin goroutine outlives Run — complementing the package TestMain check by
// isolating the assertion from container plumbing entirely.
func (s *SourceSuite) TestNoGoroutineLeakAfterCleanRun() {
	ctx := s.T().Context()
	t := s.T()
	table := s.freshTable(ctx)
	s.insertJSON(ctx, table, "leak-1", "hello")

	// Snapshot now: the container/docker goroutines already exist, so any
	// goroutine still alive after Run beyond this set is a genuine msgin leak.
	baseline := goleak.IgnoreCurrent()

	src, err := msginsql.NewPollingSource(s.db, table, s.dialect)
	require.NoError(t, err)

	done := make(chan struct{})
	var once sync.Once
	handler := func(_ context.Context, _ msgin.Message[string]) error {
		once.Do(func() { close(done) })
		return nil
	}
	consumer, err := msgin.NewConsumer[string](src, handler,
		msgin.WithPollInterval[string](200*time.Millisecond),
		msgin.WithShutdownTimeout[string](10*time.Second),
	)
	require.NoError(t, err)

	require.ErrorIs(t, s.runUntil(ctx, consumer, done, 30*time.Second), context.Canceled)

	opts := []goleak.Option{baseline}
	if s.engine.isMySQL() {
		// go-sql-driver/mysql starts a per-connection context-watcher goroutine
		// that lives as long as its pooled connection. msgin does NOT (and must
		// not) close the caller-owned *sql.DB pool on Run exit, and the pool may
		// open connections during Run that outlive the baseline snapshot — so this
		// watcher is driver connection-pool plumbing, not a msgin leak. Ignore it
		// precisely (a real leaked msgin poll/worker/sweep goroutine is still
		// caught by the baseline), mirroring the TestMain container-plumbing guard.
		opts = append(opts, goleak.IgnoreAnyFunction("github.com/go-sql-driver/mysql.(*mysqlConn).startWatcher.func1"))
	}
	goleak.VerifyNone(t, opts...)
}

// TestNackClosureRequeueFalseCollapsesToImmediate proves the D4 contract on the
// exported Source: an at-least-once source cannot drop, so a delivery's
// Nack(requeue=false, delay) collapses to Nack(requeue=true, 0) — the row is
// made immediately claimable regardless of the (ignored) delay. Driven through
// the public Poll/Delivery surface (blackbox).
func (s *SourceSuite) TestNackClosureRequeueFalseCollapsesToImmediate() {
	ctx := s.T().Context()
	t := s.T()
	table := s.freshTable(ctx)
	s.insertJSON(ctx, table, "collapse-1", "hello")

	src, err := msginsql.NewPollingSource(s.db, table, s.dialect)
	require.NoError(t, err)

	deliveries, err := src.Poll(ctx, 1)
	require.NoError(t, err)
	require.Len(t, deliveries, 1)

	// requeue=false with a large delay: the delay MUST be ignored (collapsed to
	// 0), so the row is immediately re-claimable rather than hidden for 10s.
	require.NoError(t, deliveries[0].Nack(ctx, false, 10*time.Second))

	reclaimed, err := src.Poll(ctx, 1)
	require.NoError(t, err)
	require.Len(t, reclaimed, 1, "requeue=false must collapse to delay 0, making the row immediately visible")
	dc, ok := reclaimed[0].Msg.Headers().Int(msgin.HeaderDeliveryCount)
	require.True(t, ok)
	require.Equal(t, 2, dc, "the re-claim bumped delivery_count")
}

// TestSettleStaleLeaseReturnsErrStaleLease proves both fenced settle closures
// (Ack and Nack) return the non-nil ErrStaleLease sentinel when the lease was
// expired and re-claimed by another owner, so the runtime suppresses a phantom
// success for a settle that did not happen. Driven blackbox through Poll.
func (s *SourceSuite) TestSettleStaleLeaseReturnsErrStaleLease() {
	ctx := s.T().Context()

	type testCase struct {
		name   string
		settle func(ctx context.Context, d msgin.Delivery) error
	}

	cases := []testCase{
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
		s.Run(tc.name, func() {
			t := s.T()
			table := s.freshTable(ctx)
			s.insertJSON(ctx, table, "stale-settle", "hello")

			// A tiny lease TTL so the first claim's lease expires quickly and can be
			// stolen by a second claim (a different lease_epoch).
			src, err := msginsql.NewPollingSource(s.db, table, s.dialect, msginsql.WithLeaseTTL(500*time.Millisecond))
			require.NoError(t, err)

			deliveries, err := src.Poll(ctx, 1) // lease_epoch 1
			require.NoError(t, err)
			require.Len(t, deliveries, 1)

			// Let the lease expire, then a second poll re-claims it (lease_epoch 2),
			// invalidating the first delivery's fence.
			time.Sleep(1 * time.Second)
			stolen, err := src.Poll(ctx, 1)
			require.NoError(t, err)
			require.Len(t, stolen, 1, "expired lease is re-claimable")

			// The first delivery now settles against a stale fence.
			require.ErrorIs(t, tc.settle(ctx, deliveries[0]), msginsql.ErrStaleLease)

			// The row is still owned by the second claimant (not deleted by the
			// stale Ack, not re-hidden past its own control): exactly one row remains.
			require.Equal(t, 1, s.rowCount(ctx, table))
		})
	}
}

// TestWithLockedByStampsOwner verifies WithLockedBy threads the lease-owner id
// into the locked_by column on claim (and thus into the fence), rather than the
// random default.
func (s *SourceSuite) TestWithLockedByStampsOwner() {
	ctx := s.T().Context()
	t := s.T()
	table := s.freshTable(ctx)
	s.insertJSON(ctx, table, "owned-1", "hello")

	const owner = "worker-alpha"
	src, err := msginsql.NewPollingSource(s.db, table, s.dialect, msginsql.WithLockedBy(owner))
	require.NoError(t, err)

	deliveries, err := src.Poll(ctx, 1)
	require.NoError(t, err)
	require.Len(t, deliveries, 1)

	var lockedBy string
	require.NoError(t, s.db.QueryRowContext(ctx,
		fmt.Sprintf(`SELECT locked_by FROM %s WHERE msg_id = %s`, s.engine.quote(table), s.engine.ph(1)), "owned-1").Scan(&lockedBy))
	require.Equal(t, owner, lockedBy, "WithLockedBy must stamp the lease owner")

	// The fenced Ack under the same owner settles cleanly.
	require.NoError(t, deliveries[0].Ack(ctx))
	require.Equal(t, 0, s.rowCount(ctx, table))
}

// TestSettleAndProbeUnderCanceledContext exercises the raw driver-error
// passthrough arms: with a pre-canceled context, database/sql returns ctx.Err()
// before executing, so the Source's Ack/Nack closures, Ready, and Poll all
// surface the underlying error (never a phantom success, never a mis-classified
// ErrSchemaNotReady when the probe itself failed).
func (s *SourceSuite) TestSettleAndProbeUnderCanceledContext() {
	ctx := s.T().Context()
	t := s.T()
	table := s.freshTable(ctx)
	s.insertJSON(ctx, table, "cancel-1", "hello")

	src, err := msginsql.NewPollingSource(s.db, table, s.dialect)
	require.NoError(t, err)

	// Claim a delivery on a healthy context so its Ack/Nack closures are bound.
	deliveries, err := src.Poll(ctx, 1)
	require.NoError(t, err)
	require.Len(t, deliveries, 1)

	cctx, cancel := context.WithCancel(ctx)
	cancel() // pre-cancel: every DB call below returns ctx.Err() before executing.

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
}

// runUntil delegates to the package-level runConsumerUntil (shared with
// OutboundSuite's round-trip test).
func (s *SourceSuite) runUntil(ctx context.Context, consumer msgin.Consumer[string], done <-chan struct{}, deadline time.Duration) error {
	return runConsumerUntil(s.T(), ctx, consumer, done, deadline)
}
