package sql_test

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	msgin "github.com/kartaladev/msgin"
	msginsql "github.com/kartaladev/msgin/adapter/database/sql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// nonTxQuerier is a msginsql.Querier that is NEITHER a *sql.DB nor a *sql.Tx, so
// the MySQL dialect's Claim cannot obtain a transaction from it. Its methods are
// never actually reached — Claim rejects the type before issuing any query — so
// they may return zero values.
type nonTxQuerier struct{}

func (nonTxQuerier) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return nil, nil
}
func (nonTxQuerier) QueryContext(context.Context, string, ...any) (*sql.Rows, error) {
	return nil, nil
}
func (nonTxQuerier) QueryRowContext(context.Context, string, ...any) *sql.Row {
	return nil
}

// TestMySQLClaimRequiresTransactionalQuerier pins the defensive branch of the
// MySQL two-step claim: given a Querier that can neither begin a transaction
// (*sql.DB) nor already be one (*sql.Tx), Claim returns a clear error rather than
// running a non-atomic (double-claim-prone) two-step. No database is needed — the
// type check precedes any query.
func TestMySQLClaimRequiresTransactionalQuerier(t *testing.T) {
	t.Parallel()

	rows, err := msginsql.MySQLDialect().Claim(t.Context(), nonTxQuerier{}, "msgs", 1, "worker", time.Minute)
	require.Error(t, err)
	assert.Nil(t, rows)
	assert.Contains(t, err.Error(), "*sql.DB or *sql.Tx",
		"the error explains an atomic (transactional) Querier is required")
}

// TestMySQLDDLIdentifierValidation mirrors TestPostgresDDLIdentifierValidation
// for the MySQL reference DDL: a valid identifier produces a backtick-quoted
// CREATE TABLE with the inline claim index; a bad identifier is
// ErrInvalidTableName before any string is built.
func TestMySQLDDLIdentifierValidation(t *testing.T) {
	t.Parallel()

	type testCase struct {
		name   string
		table  string
		assert func(t *testing.T, ddl string, err error)
	}

	cases := []testCase{
		{
			name:  "valid identifier produces DDL",
			table: "msg_queue",
			assert: func(t *testing.T, ddl string, err error) {
				require.NoError(t, err)
				assert.Contains(t, ddl, "CREATE TABLE IF NOT EXISTS `msg_queue`")
				assert.Contains(t, ddl, "`msg_queue_claim_idx`")
				assert.Contains(t, ddl, "AUTO_INCREMENT")
			},
		},
		{
			name:  "leading digit rejected",
			table: "1bad",
			assert: func(t *testing.T, _ string, err error) {
				require.ErrorIs(t, err, msginsql.ErrInvalidTableName)
			},
		},
		{
			name:  "sql injection attempt rejected",
			table: "t`; DROP TABLE users; --",
			assert: func(t *testing.T, _ string, err error) {
				require.ErrorIs(t, err, msginsql.ErrInvalidTableName)
			},
		},
		{
			name:  "empty name rejected",
			table: "",
			assert: func(t *testing.T, _ string, err error) {
				require.ErrorIs(t, err, msginsql.ErrInvalidTableName)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ddl, err := msginsql.MySQLDDL(tc.table)
			tc.assert(t, ddl, err)
		})
	}
}

// TestMySQLClaimInExistingTransaction exercises the defensive *sql.Tx branch of
// the MySQL claim: when the Querier is already a transaction, Claim runs the
// two-step directly on it (no nested begin) and leaves commit to the caller who
// owns the tx. It provisions a real MySQL container (the branch runs actual SQL).
func TestMySQLClaimInExistingTransaction(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	db := RunTestMySQL(t)
	dialect := msginsql.MySQLDialect()

	const table = "msgin_tx_claim"
	require.NoError(t, dialect.EnsureSchema(ctx, db, table))

	headers, err := msginsql.EncodeHeaders(msgin.NewHeaders(map[string]any{msgin.HeaderID: "tx-1"}))
	require.NoError(t, err)
	require.NoError(t, dialect.Insert(ctx, db, table, "tx-1", headers, []byte("payload"), 0))

	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback() })

	// Claim with the *sql.Tx directly: the two-step runs on the caller's tx.
	rows, err := dialect.Claim(ctx, tx, table, 10, "worker-tx", time.Minute)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, "tx-1", rows[0].MsgID)
	require.Equal(t, 1, rows[0].DeliveryCount, "delivery_count post-increment computed in Go")
	require.Equal(t, int64(1), rows[0].LeaseEpoch, "lease_epoch post-increment computed in Go")

	// The caller owns the lifecycle: the claim is only durable once the caller
	// commits. After commit, the row is leased and not re-claimable on the pool.
	require.NoError(t, tx.Commit())

	again, err := dialect.Claim(ctx, db, table, 10, "worker-tx", time.Minute)
	require.NoError(t, err)
	require.Empty(t, again, "a live-leased row is not re-claimable after the caller's tx commits")

	var lockedBy string
	require.NoError(t, db.QueryRowContext(ctx,
		fmt.Sprintf("SELECT locked_by FROM `%s` WHERE msg_id = ?", table), "tx-1").Scan(&lockedBy))
	require.Equal(t, "worker-tx", lockedBy, "the committed claim stamped the lease owner")
}
