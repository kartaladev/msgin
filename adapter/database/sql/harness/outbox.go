package harness

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
)

// outboxTxCtxKey is the harness's own context key for stashing the caller's
// business *sql.Tx — a stand-in for however a real caller stores its active
// transaction. msgin never defines or depends on this key; the
// TransactionResolver is the only seam (ADR 0010 D8).
type outboxTxCtxKey struct{}

func outboxWithTx(ctx context.Context, tx *sql.Tx) context.Context {
	return context.WithValue(ctx, outboxTxCtxKey{}, tx)
}

// outboxSharedTxResolver is the TransactionResolver under test: it extracts
// the *sql.Tx outboxWithTx stashed in ctx, reporting (nil, nil) — "no shared
// transaction" — when none is present.
func outboxSharedTxResolver(ctx context.Context) (msginsql.Querier, error) {
	tx, _ := ctx.Value(outboxTxCtxKey{}).(*sql.Tx)
	if tx == nil {
		return nil, nil
	}
	return tx, nil
}

// errOutboxResolverBoom is a fixed sentinel a failing TransactionResolver
// returns, so the harness can assert Send propagates (wraps) it via errors.Is.
var errOutboxResolverBoom = errors.New("harness/outbox: resolver boom")

// RunOutbox exercises Outbound.Send's shared-transaction policy (ADR 0010 D8)
// against kit.Lease on the already-open db, reproducing every assertion the
// pre-split OutboxSuite made (Plan 006 Task 3): the atomicity of a business
// write + the outbox INSERT under a caller-owned *sql.Tx (both commit and
// rollback branches), commit-gated visibility to a concurrent Source.Poll,
// every resolveQuerier branch (no resolver / resolver error / strict no-tx /
// opportunistic no-tx), and the borrowed-tx never-committed/rolled-back
// invariant.
func RunOutbox(t *testing.T, kit TestKit, db *sql.DB) {
	t.Helper()
	var counter atomic.Int64
	freshOutbox := func(ctx context.Context) string { return freshTable(t, ctx, kit, db, &counter, "msgin_outbox") }
	freshBiz := func(t *testing.T, ctx context.Context) string {
		t.Helper()
		name := fmt.Sprintf("msgin_biz_%d", counter.Add(1))
		_, err := db.ExecContext(ctx, fmt.Sprintf("CREATE TABLE %s (id INTEGER PRIMARY KEY)", kit.Quote(name)))
		require.NoError(t, err)
		return name
	}

	t.Run("Atomicity", func(t *testing.T) {
		ctx := t.Context()

		cases := []struct {
			name     string
			finalize func(tx *sql.Tx) error
			assert   func(t *testing.T, bizCount, outboxCount int)
		}{
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
			t.Run(tc.name, func(t *testing.T) {
				bizTable := freshBiz(t, ctx)
				outTable := freshOutbox(ctx)

				out, err := msginsql.NewOutboundAdapter(db, outTable, kit.Lease, msginsql.WithSharedTransaction(outboxSharedTxResolver))
				require.NoError(t, err)

				tx, err := db.BeginTx(ctx, nil)
				require.NoError(t, err)
				txCtx := outboxWithTx(ctx, tx)

				_, err = tx.ExecContext(ctx, fmt.Sprintf("INSERT INTO %s (id) VALUES (1)", kit.Quote(bizTable)))
				require.NoError(t, err)

				msg := msgin.NewMessage[any](any([]byte(`"atomicity-payload"`)),
					msgin.NewHeaders(map[string]any{msgin.HeaderID: "atomic-1"}))
				require.NoError(t, out.Send(txCtx, msg), "Send must enlist the outbox INSERT in the caller's tx")

				require.NoError(t, tc.finalize(tx))

				tc.assert(t, rowCount(t, ctx, kit, db, bizTable), rowCount(t, ctx, kit, db, outTable))
			})
		}
	})

	t.Run("CommitGatesVisibility", func(t *testing.T) {
		ctx := t.Context()

		outTable := freshOutbox(ctx)
		out, err := msginsql.NewOutboundAdapter(db, outTable, kit.Lease, msginsql.WithSharedTransaction(outboxSharedTxResolver))
		require.NoError(t, err)

		src, err := msginsql.NewPollingSource(db, outTable, kit.Lease)
		require.NoError(t, err)

		tx, err := db.BeginTx(ctx, nil)
		require.NoError(t, err)
		txCtx := outboxWithTx(ctx, tx)

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
	})

	t.Run("SharedTransactionPolicyBranches", func(t *testing.T) {
		ctx := t.Context()

		cases := []struct {
			name   string
			build  func(t *testing.T, table string) (*msginsql.Outbound, *syncBuffer)
			assert func(t *testing.T, sendErr error, rowCount int, logs *syncBuffer)
		}{
			{
				name: "resolver unset: standalone pool insert, unchanged baseline",
				build: func(t *testing.T, table string) (*msginsql.Outbound, *syncBuffer) {
					out, err := msginsql.NewOutboundAdapter(db, table, kit.Lease)
					require.NoError(t, err)
					return out, nil
				},
				assert: func(t *testing.T, sendErr error, rc int, _ *syncBuffer) {
					require.NoError(t, sendErr)
					assert.Equal(t, 1, rc)
				},
			},
			{
				name: "resolver error: Send propagates the wrapped error, no insert",
				build: func(t *testing.T, table string) (*msginsql.Outbound, *syncBuffer) {
					resolver := func(context.Context) (msginsql.Querier, error) { return nil, errOutboxResolverBoom }
					out, err := msginsql.NewOutboundAdapter(db, table, kit.Lease, msginsql.WithSharedTransaction(resolver))
					require.NoError(t, err)
					return out, nil
				},
				assert: func(t *testing.T, sendErr error, rc int, _ *syncBuffer) {
					require.ErrorIs(t, sendErr, errOutboxResolverBoom)
					assert.Equal(t, 0, rc)
				},
			},
			{
				name: "strict + no shared tx in ctx: ErrNoSharedTransaction, no insert (never dual-writes)",
				build: func(t *testing.T, table string) (*msginsql.Outbound, *syncBuffer) {
					resolver := func(context.Context) (msginsql.Querier, error) { return nil, nil }
					out, err := msginsql.NewOutboundAdapter(db, table, kit.Lease, msginsql.WithSharedTransaction(resolver))
					require.NoError(t, err)
					return out, nil
				},
				assert: func(t *testing.T, sendErr error, rc int, _ *syncBuffer) {
					require.ErrorIs(t, sendErr, msginsql.ErrNoSharedTransaction)
					assert.Equal(t, 0, rc)
				},
			},
			{
				name: "opportunistic + no shared tx in ctx: standalone insert AND one WARN log line",
				build: func(t *testing.T, table string) (*msginsql.Outbound, *syncBuffer) {
					resolver := func(context.Context) (msginsql.Querier, error) { return nil, nil }
					logger, logs := newRecorder()
					out, err := msginsql.NewOutboundAdapter(db, table, kit.Lease,
						msginsql.WithOpportunisticSharedTransaction(resolver), msginsql.WithLogger(logger))
					require.NoError(t, err)
					return out, logs
				},
				assert: func(t *testing.T, sendErr error, rc int, logs *syncBuffer) {
					require.NoError(t, sendErr)
					assert.Equal(t, 1, rc)
					require.NotNil(t, logs)
					assert.Contains(t, logs.String(), "no shared transaction in context")
					assert.Contains(t, logs.String(), "atomicity NOT achieved")
					assert.Contains(t, logs.String(), "policy-1", "the WARN line carries the message id")
				},
			},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				table := freshOutbox(ctx)
				out, logs := tc.build(t, table)

				msg := msgin.NewMessage[any](any([]byte(`"policy-payload"`)),
					msgin.NewHeaders(map[string]any{msgin.HeaderID: "policy-1"}))
				// ctx carries no tx (outboxWithTx was never applied), so any
				// configured resolver either errors or reports "no shared transaction".
				sendErr := out.Send(ctx, msg)

				tc.assert(t, sendErr, rowCount(t, ctx, kit, db, table), logs)
			})
		}
	})

	t.Run("BorrowedTxNotCommittedOrRolledBackByMsgin", func(t *testing.T) {
		ctx := t.Context()

		bizTable := freshBiz(t, ctx)
		outTable := freshOutbox(ctx)

		out, err := msginsql.NewOutboundAdapter(db, outTable, kit.Lease, msginsql.WithSharedTransaction(outboxSharedTxResolver))
		require.NoError(t, err)

		tx, err := db.BeginTx(ctx, nil)
		require.NoError(t, err)
		txCtx := outboxWithTx(ctx, tx)

		msg := msgin.NewMessage[any](any([]byte(`"borrow-payload"`)),
			msgin.NewHeaders(map[string]any{msgin.HeaderID: "borrow-1"}))
		require.NoError(t, out.Send(txCtx, msg))

		_, err = tx.ExecContext(ctx, fmt.Sprintf("INSERT INTO %s (id) VALUES (2)", kit.Quote(bizTable)))
		require.NoError(t, err, "the caller's tx must still be usable after Send — msgin must not have committed or rolled it back")
		require.NotErrorIs(t, err, sql.ErrTxDone)

		require.NoError(t, tx.Commit())

		assert.Equal(t, 1, rowCount(t, ctx, kit, db, bizTable))
		assert.Equal(t, 1, rowCount(t, ctx, kit, db, outTable))
	})
}
