package sql_test

// Despite the _pg_test.go suffix (matching the naming convention of the other
// dialect-parameterized suites in this package), OutboxSuite runs once per
// engine via the shared `engines` table, so it exercises the transactional
// outbox (ADR 0010 D8: WithSharedTransaction / WithOpportunisticSharedTransaction)
// against both a real PostgreSQL and a real MySQL container.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"

	msgin "github.com/kartaladev/msgin"
	msginsql "github.com/kartaladev/msgin/adapter/database/sql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

// txCtxKey is the test's own context key for stashing the caller's business
// *sql.Tx — a stand-in for however a real caller stores its active
// transaction (raw database/sql, sqlx, a unit-of-work layer, ...). msgin never
// defines or depends on this key; the TransactionResolver is the only seam
// (ADR 0010 D8).
type txCtxKey struct{}

// withTx returns a context carrying tx, the shape a caller's request-scoped
// plumbing would produce after opening its business transaction.
func withTx(ctx context.Context, tx *sql.Tx) context.Context {
	return context.WithValue(ctx, txCtxKey{}, tx)
}

// sharedTxResolver is the TransactionResolver under test: it extracts the
// *sql.Tx withTx stashed in ctx, reporting (nil, nil) — "no shared
// transaction" — when none is present.
func sharedTxResolver(ctx context.Context) (msginsql.Querier, error) {
	tx, _ := ctx.Value(txCtxKey{}).(*sql.Tx)
	if tx == nil {
		return nil, nil
	}
	return tx, nil
}

// errResolverBoom is a fixed sentinel a failing TransactionResolver returns,
// so the test can assert Send propagates (wraps) it via errors.Is.
var errResolverBoom = errors.New("outbox_pg_test: resolver boom")

// OutboxSuite provisions one container per engine for the whole suite and
// exercises Outbound.Send's shared-transaction policy (ADR 0010 D8): the
// atomicity of a business write + the outbox INSERT under a caller-owned
// *sql.Tx, commit-gated visibility to a concurrent Source.Poll, the
// strict-vs-opportunistic no-shared-tx branches, and the borrowed-tx
// never-committed/rolled-back invariant. TestOutboxSuite runs it once per
// engine (postgres, mysql) so the SAME assertions prove the policy holds over
// both dialects. TestMain (goroutine-leak check) is declared once for the
// whole package in source_pg_test.go.
type OutboxSuite struct {
	suite.Suite
	engine  engine
	db      *sql.DB
	dialect msginsql.Dialect
	counter atomic.Int64
}

func TestOutboxSuite(t *testing.T) {
	for _, e := range engines {
		t.Run(e.name, func(t *testing.T) {
			suite.Run(t, &OutboxSuite{engine: e})
		})
	}
}

func (s *OutboxSuite) SetupSuite() {
	s.db = s.engine.openDB(s.T())
	s.dialect = s.engine.dialect
}

// freshTable returns a unique, schema-applied outbox table name for a single
// test.
func (s *OutboxSuite) freshTable(ctx context.Context) string {
	name := fmt.Sprintf("msgin_outbox_%d", s.counter.Add(1))
	require.NoError(s.T(), s.dialect.EnsureSchema(ctx, s.db, name))
	return name
}

// freshBizTable creates a unique, minimal "business" table (a stand-in for a
// caller's own domain table) that a business write lands in alongside the
// outbox INSERT, both under the same caller transaction.
func (s *OutboxSuite) freshBizTable(ctx context.Context) string {
	t := s.T()
	name := fmt.Sprintf("msgin_biz_%d", s.counter.Add(1))
	_, err := s.db.ExecContext(ctx, fmt.Sprintf("CREATE TABLE %s (id INTEGER PRIMARY KEY)", s.engine.quote(name)))
	require.NoError(t, err)
	return name
}

// rowCount returns the number of rows in table (used for both the outbox
// table and the business table — both are plain row counts).
func (s *OutboxSuite) rowCount(ctx context.Context, table string) int {
	var n int
	require.NoError(s.T(), s.db.QueryRowContext(ctx, fmt.Sprintf(`SELECT count(*) FROM %s`, s.engine.quote(table))).Scan(&n))
	return n
}

// ---- tests ----------------------------------------------------------------

// TestAtomicity proves the transactional outbox's core promise (ADR 0010 D8):
// a business write and the outbox INSERT, made under the SAME caller
// transaction via WithSharedTransaction, commit or roll back together.
func (s *OutboxSuite) TestAtomicity() {
	ctx := s.T().Context()

	type testCase struct {
		name     string
		finalize func(tx *sql.Tx) error
		assert   func(t *testing.T, bizCount, outboxCount int)
	}

	cases := []testCase{
		{
			name:     "rollback discards both the business row and the outbox row",
			finalize: func(tx *sql.Tx) error { return tx.Rollback() },
			assert: func(t *testing.T, bizCount, outboxCount int) {
				assert.Equal(t, 0, bizCount, "business row must not survive a rollback")
				assert.Equal(t, 0, outboxCount, "outbox row must not survive a rollback")
			},
		},
		{
			name:     "commit persists both the business row and the outbox row",
			finalize: func(tx *sql.Tx) error { return tx.Commit() },
			assert: func(t *testing.T, bizCount, outboxCount int) {
				assert.Equal(t, 1, bizCount, "business row must persist after commit")
				assert.Equal(t, 1, outboxCount, "outbox row must persist after commit")
			},
		},
	}

	for _, tc := range cases {
		s.Run(tc.name, func() {
			t := s.T()
			bizTable := s.freshBizTable(ctx)
			outTable := s.freshTable(ctx)

			out, err := msginsql.NewOutboundAdapter(s.db, outTable, msginsql.WithSharedTransaction(sharedTxResolver))
			require.NoError(t, err)

			tx, err := s.db.BeginTx(ctx, nil)
			require.NoError(t, err)
			txCtx := withTx(ctx, tx)

			_, err = tx.ExecContext(ctx, fmt.Sprintf("INSERT INTO %s (id) VALUES (1)", s.engine.quote(bizTable)))
			require.NoError(t, err)

			msg := msgin.NewMessage[any](any([]byte(`"atomicity-payload"`)),
				msgin.NewHeaders(map[string]any{msgin.HeaderID: "atomic-1"}))
			require.NoError(t, out.Send(txCtx, msg), "Send must enlist the outbox INSERT in the caller's tx")

			require.NoError(t, tc.finalize(tx))

			tc.assert(t, s.rowCount(ctx, bizTable), s.rowCount(ctx, outTable))
		})
	}
}

// TestCommitGatesVisibility proves the outbox needs no extra coordination for
// visibility: while the caller's transaction is still open (uncommitted), a
// concurrent Source.Poll (SELECT ... FOR UPDATE SKIP LOCKED, committed-reads
// only) does not see the outbox row; once the caller commits, the very next
// poll claims it — the end-to-end outbox relay (ADR 0010 D8 "commit gates
// visibility").
func (s *OutboxSuite) TestCommitGatesVisibility() {
	ctx := s.T().Context()
	t := s.T()

	outTable := s.freshTable(ctx)
	out, err := msginsql.NewOutboundAdapter(s.db, outTable, msginsql.WithSharedTransaction(sharedTxResolver))
	require.NoError(t, err)

	src, err := msginsql.NewPollingSource(s.db, outTable)
	require.NoError(t, err)

	tx, err := s.db.BeginTx(ctx, nil)
	require.NoError(t, err)
	txCtx := withTx(ctx, tx)

	msg := msgin.NewMessage[any](any([]byte(`"commit-gate-payload"`)),
		msgin.NewHeaders(map[string]any{msgin.HeaderID: "commit-gate-1"}))
	require.NoError(t, out.Send(txCtx, msg))

	uncommittedDeliveries, err := src.Poll(ctx, 10)
	require.NoError(t, err)
	assert.Empty(t, uncommittedDeliveries, "a concurrent poll must not see the row while the caller's tx is uncommitted")

	require.NoError(t, tx.Commit())

	deliveries, err := src.Poll(ctx, 10)
	require.NoError(t, err)
	require.Len(t, deliveries, 1, "the poll after commit must claim the now-visible row")
	assert.Equal(t, "commit-gate-1", deliveries[0].Msg.ID())
	require.NoError(t, deliveries[0].Ack(ctx))
}

// TestSharedTransactionPolicyBranches exercises every ADR 0010 D8 branch of
// Outbound.resolveQuerier reachable through Send: no resolver configured
// (Task 6 baseline, unchanged), a resolver error, strict mode with no shared
// tx present, and opportunistic mode with no shared tx present (fallback +
// WARN log).
func (s *OutboxSuite) TestSharedTransactionPolicyBranches() {
	ctx := s.T().Context()

	type testCase struct {
		name   string
		build  func(t *testing.T, table string) (*msginsql.Outbound, *syncBuffer)
		assert func(t *testing.T, sendErr error, rowCount int, logs *syncBuffer)
	}

	cases := []testCase{
		{
			name: "resolver unset: standalone pool insert, unchanged from Task 6",
			build: func(t *testing.T, table string) (*msginsql.Outbound, *syncBuffer) {
				out, err := msginsql.NewOutboundAdapter(s.db, table)
				require.NoError(t, err)
				return out, nil
			},
			assert: func(t *testing.T, sendErr error, rowCount int, _ *syncBuffer) {
				require.NoError(t, sendErr)
				assert.Equal(t, 1, rowCount)
			},
		},
		{
			name: "resolver error: Send propagates the wrapped error, no insert",
			build: func(t *testing.T, table string) (*msginsql.Outbound, *syncBuffer) {
				resolver := func(context.Context) (msginsql.Querier, error) { return nil, errResolverBoom }
				out, err := msginsql.NewOutboundAdapter(s.db, table, msginsql.WithSharedTransaction(resolver))
				require.NoError(t, err)
				return out, nil
			},
			assert: func(t *testing.T, sendErr error, rowCount int, _ *syncBuffer) {
				require.ErrorIs(t, sendErr, errResolverBoom)
				assert.Equal(t, 0, rowCount)
			},
		},
		{
			name: "strict + no shared tx in ctx: ErrNoSharedTransaction, no insert (never dual-writes)",
			build: func(t *testing.T, table string) (*msginsql.Outbound, *syncBuffer) {
				resolver := func(context.Context) (msginsql.Querier, error) { return nil, nil }
				out, err := msginsql.NewOutboundAdapter(s.db, table, msginsql.WithSharedTransaction(resolver))
				require.NoError(t, err)
				return out, nil
			},
			assert: func(t *testing.T, sendErr error, rowCount int, _ *syncBuffer) {
				require.ErrorIs(t, sendErr, msginsql.ErrNoSharedTransaction)
				assert.Equal(t, 0, rowCount)
			},
		},
		{
			name: "opportunistic + no shared tx in ctx: standalone insert AND one WARN log line",
			build: func(t *testing.T, table string) (*msginsql.Outbound, *syncBuffer) {
				resolver := func(context.Context) (msginsql.Querier, error) { return nil, nil }
				logger, logs := newRecorder()
				out, err := msginsql.NewOutboundAdapter(s.db, table,
					msginsql.WithOpportunisticSharedTransaction(resolver), msginsql.WithLogger(logger))
				require.NoError(t, err)
				return out, logs
			},
			assert: func(t *testing.T, sendErr error, rowCount int, logs *syncBuffer) {
				require.NoError(t, sendErr)
				assert.Equal(t, 1, rowCount)
				require.NotNil(t, logs)
				assert.Contains(t, logs.String(), "no shared transaction in context")
				assert.Contains(t, logs.String(), "atomicity NOT achieved")
				assert.Contains(t, logs.String(), "policy-1", "the WARN line carries the message id")
			},
		},
	}

	for _, tc := range cases {
		s.Run(tc.name, func() {
			t := s.T()
			table := s.freshTable(ctx)
			out, logs := tc.build(t, table)

			msg := msgin.NewMessage[any](any([]byte(`"policy-payload"`)),
				msgin.NewHeaders(map[string]any{msgin.HeaderID: "policy-1"}))
			// ctx carries no tx (withTx was never applied), so any configured
			// resolver either errors or reports "no shared transaction".
			sendErr := out.Send(ctx, msg)

			tc.assert(t, sendErr, s.rowCount(ctx, table), logs)
		})
	}
}

// TestBorrowedTxNotCommittedOrRolledBackByMsgin proves the borrowed-tx
// invariant directly: after Send inserts on the caller's shared tx, msgin has
// NOT committed or rolled it back — the caller's tx is still fully usable for
// a further statement, and the caller alone decides when it finally commits
// (ADR 0010 D8).
func (s *OutboxSuite) TestBorrowedTxNotCommittedOrRolledBackByMsgin() {
	ctx := s.T().Context()
	t := s.T()

	bizTable := s.freshBizTable(ctx)
	outTable := s.freshTable(ctx)

	out, err := msginsql.NewOutboundAdapter(s.db, outTable, msginsql.WithSharedTransaction(sharedTxResolver))
	require.NoError(t, err)

	tx, err := s.db.BeginTx(ctx, nil)
	require.NoError(t, err)
	txCtx := withTx(ctx, tx)

	msg := msgin.NewMessage[any](any([]byte(`"borrow-payload"`)),
		msgin.NewHeaders(map[string]any{msgin.HeaderID: "borrow-1"}))
	require.NoError(t, out.Send(txCtx, msg))

	// If msgin had already committed or rolled back the borrowed tx, this
	// second statement on the very same tx would fail (database/sql returns
	// sql.ErrTxDone) — proving msgin left the tx's lifecycle entirely to the
	// caller.
	_, err = tx.ExecContext(ctx, fmt.Sprintf("INSERT INTO %s (id) VALUES (2)", s.engine.quote(bizTable)))
	require.NoError(t, err, "the caller's tx must still be usable after Send — msgin must not have committed or rolled it back")
	require.NotErrorIs(t, err, sql.ErrTxDone)

	require.NoError(t, tx.Commit())

	assert.Equal(t, 1, s.rowCount(ctx, bizTable))
	assert.Equal(t, 1, s.rowCount(ctx, outTable))
}
