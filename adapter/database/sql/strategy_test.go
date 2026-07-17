package sql_test

import (
	"context"
	stdsql "database/sql"
	"testing"
	"time"

	msginsql "github.com/kartaladev/msgin/adapter/database/sql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeQuerier is a Querier that is neither *sql.DB nor *sql.Tx, so ClaimLock's
// transaction resolution (beginLockTx) must reject it — the lock strategy needs a
// real transaction owner. Its methods are never reached (beginLockTx fails first).
type fakeQuerier struct{}

func (fakeQuerier) ExecContext(context.Context, string, ...any) (stdsql.Result, error) {
	return nil, nil
}
func (fakeQuerier) QueryContext(context.Context, string, ...any) (*stdsql.Rows, error) {
	return nil, nil
}
func (fakeQuerier) QueryRowContext(context.Context, string, ...any) *stdsql.Row { return nil }

// TestClaimLock_RequiresDBOrTxQuerier covers beginLockTx's guard on both built-in
// LockDialects: ClaimLock on a Querier that can neither begin nor be a
// transaction returns a clear error (no partial claim, no panic), for both
// dialects. No database connection is made.
func TestClaimLock_RequiresDBOrTxQuerier(t *testing.T) {
	t.Parallel()

	type testCase struct {
		name    string
		dialect msginsql.LockDialect
	}
	cases := []testCase{
		{name: "postgres", dialect: msginsql.PostgresDialect().(msginsql.LockDialect)},
		{name: "mysql", dialect: msginsql.MySQLDialect().(msginsql.LockDialect)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			lr, err := tc.dialect.ClaimLock(t.Context(), fakeQuerier{}, "msgs", "owner")
			require.Error(t, err)
			assert.Nil(t, lr, "no LockedRow (and no leaked tx) on the resolution-failure path")
			assert.Contains(t, err.Error(), "requires a *sql.DB or *sql.Tx")
		})
	}
}

// nonLockDialect implements msginsql.Dialect but deliberately NOT
// msginsql.LockDialect, so NewPollingSource(WithStrategy(StrategyLockForUpdate))
// must reject it with ErrLockStrategyUnsupported (ADR 0010 D5 — the segregated
// lock SPI). Every method is a no-op stub: construction fails before any is
// called.
type nonLockDialect struct{}

func (nonLockDialect) Claim(context.Context, msginsql.Querier, string, int, string, time.Duration) ([]msginsql.ClaimedRow, error) {
	return nil, nil
}
func (nonLockDialect) Ack(context.Context, msginsql.Querier, string, int64, string, int64) (bool, error) {
	return false, nil
}
func (nonLockDialect) Nack(context.Context, msginsql.Querier, string, int64, string, int64, time.Duration) (bool, error) {
	return false, nil
}
func (nonLockDialect) Insert(context.Context, msginsql.Querier, string, string, []byte, []byte, time.Duration) error {
	return nil
}
func (nonLockDialect) EnsureSchema(context.Context, msginsql.Querier, string) error { return nil }
func (nonLockDialect) SchemaExists(context.Context, msginsql.Querier, string) (bool, error) {
	return false, nil
}

// Compile-time proof of intent: nonLockDialect is a Dialect but not a LockDialect.
var _ msginsql.Dialect = nonLockDialect{}

// TestNewPollingSource_StrategyConstruction exercises the WithStrategy
// construction contract (ADR 0010 D5): the default is lease/claim; an
// out-of-range Strategy is ErrInvalidStrategy; StrategyLockForUpdate requires a
// Dialect that also implements LockDialect (ErrLockStrategyUnsupported when it
// does not). No database connection is made — sql.Open is lazy and the checks are
// construction-time only.
func TestNewPollingSource_StrategyConstruction(t *testing.T) {
	t.Parallel()

	type testCase struct {
		name   string
		opts   []msginsql.Option
		assert func(t *testing.T, src *msginsql.Source, err error)
	}

	cases := []testCase{
		{
			name: "default strategy (unset) constructs a lease Source",
			opts: nil,
			assert: func(t *testing.T, src *msginsql.Source, err error) {
				require.NoError(t, err)
				assert.NotNil(t, src)
			},
		},
		{
			name: "explicit StrategyLeaseClaim constructs",
			opts: []msginsql.Option{msginsql.WithStrategy(msginsql.StrategyLeaseClaim)},
			assert: func(t *testing.T, src *msginsql.Source, err error) {
				require.NoError(t, err)
				assert.NotNil(t, src)
			},
		},
		{
			name: "StrategyLockForUpdate with a lock-capable built-in dialect constructs",
			opts: []msginsql.Option{
				msginsql.WithDialect(msginsql.PostgresDialect()),
				msginsql.WithStrategy(msginsql.StrategyLockForUpdate),
			},
			assert: func(t *testing.T, src *msginsql.Source, err error) {
				require.NoError(t, err, "the built-in PostgresDialect implements LockDialect")
				assert.NotNil(t, src)
			},
		},
		{
			name: "StrategyLockForUpdate with the MySQL built-in dialect constructs",
			opts: []msginsql.Option{
				msginsql.WithDialect(msginsql.MySQLDialect()),
				msginsql.WithStrategy(msginsql.StrategyLockForUpdate),
			},
			assert: func(t *testing.T, src *msginsql.Source, err error) {
				require.NoError(t, err, "the built-in MySQLDialect implements LockDialect")
				assert.NotNil(t, src)
			},
		},
		{
			name: "StrategyLockForUpdate with a lease-only dialect is ErrLockStrategyUnsupported",
			opts: []msginsql.Option{
				msginsql.WithDialect(nonLockDialect{}),
				msginsql.WithStrategy(msginsql.StrategyLockForUpdate),
			},
			assert: func(t *testing.T, src *msginsql.Source, err error) {
				require.ErrorIs(t, err, msginsql.ErrLockStrategyUnsupported)
				assert.Nil(t, src)
			},
		},
		{
			name: "an out-of-range Strategy is ErrInvalidStrategy",
			opts: []msginsql.Option{msginsql.WithStrategy(msginsql.Strategy(99))},
			assert: func(t *testing.T, src *msginsql.Source, err error) {
				require.ErrorIs(t, err, msginsql.ErrInvalidStrategy)
				assert.Nil(t, src)
			},
		},
		{
			name: "a negative Strategy is ErrInvalidStrategy",
			opts: []msginsql.Option{msginsql.WithStrategy(msginsql.Strategy(-1))},
			assert: func(t *testing.T, src *msginsql.Source, err error) {
				require.ErrorIs(t, err, msginsql.ErrInvalidStrategy)
				assert.Nil(t, src)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// A lazy pgx handle (no connection); the built-in dialect is used or
			// auto-detected, and every assertion here is construction-time only.
			db := openDB(t, "pgx")
			src, err := msginsql.NewPollingSource(db, "msgs", tc.opts...)
			tc.assert(t, src, err)
		})
	}
}
