package sql_test

import (
	"context"
	"testing"
	"time"

	msginsql "github.com/kartaladev/msgin/adapter/database/sql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The BeginLockTx unsupported-Querier guard (a Querier that is neither *sql.DB
// nor *sql.Tx is rejected) is covered directly against the engine in
// locktx_unit_test.go (Plan 006 Task 2, audit R2-4), superseding the former
// dialect-driven TestClaimLock_RequiresDBOrTxQuerier.

// nonLockDialect implements msginsql.LeaseDialect but deliberately NOT
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

// Compile-time proof of intent: nonLockDialect is a LeaseDialect but not a LockDialect.
var _ msginsql.LeaseDialect = nonLockDialect{}

// TestNewPollingSource_StrategyConstruction exercises the WithStrategy
// construction contract (ADR 0010 D5): the default is lease/claim; an
// out-of-range Strategy is ErrInvalidStrategy; StrategyLockForUpdate requires a
// LeaseDialect that also implements LockDialect (ErrLockStrategyUnsupported when it
// does not). No database connection is made — sql.Open is lazy and the checks are
// construction-time only.
func TestNewPollingSource_StrategyConstruction(t *testing.T) {
	t.Parallel()

	type testCase struct {
		name    string
		dialect msginsql.LeaseDialect
		opts    []msginsql.Option
		assert  func(t *testing.T, src *msginsql.Source, err error)
	}

	cases := []testCase{
		{
			name:    "default strategy (unset) constructs a lease Source",
			dialect: newFakeDialect(),
			opts:    nil,
			assert: func(t *testing.T, src *msginsql.Source, err error) {
				require.NoError(t, err)
				assert.NotNil(t, src)
			},
		},
		{
			name:    "explicit StrategyLeaseClaim constructs",
			dialect: newFakeDialect(),
			opts:    []msginsql.Option{msginsql.WithStrategy(msginsql.StrategyLeaseClaim)},
			assert: func(t *testing.T, src *msginsql.Source, err error) {
				require.NoError(t, err)
				assert.NotNil(t, src)
			},
		},
		{
			name:    "StrategyLockForUpdate with a lock-capable dialect constructs",
			dialect: newFakeDialect(),
			opts: []msginsql.Option{
				msginsql.WithStrategy(msginsql.StrategyLockForUpdate),
			},
			assert: func(t *testing.T, src *msginsql.Source, err error) {
				require.NoError(t, err, "the fake dialect implements LockDialect")
				assert.NotNil(t, src)
			},
		},
		{
			name:    "StrategyLockForUpdate with a lease-only dialect is ErrLockStrategyUnsupported",
			dialect: nonLockDialect{},
			opts: []msginsql.Option{
				msginsql.WithStrategy(msginsql.StrategyLockForUpdate),
			},
			assert: func(t *testing.T, src *msginsql.Source, err error) {
				require.ErrorIs(t, err, msginsql.ErrLockStrategyUnsupported)
				assert.Nil(t, src)
			},
		},
		{
			name:    "an out-of-range Strategy is ErrInvalidStrategy",
			dialect: newFakeDialect(),
			opts:    []msginsql.Option{msginsql.WithStrategy(msginsql.Strategy(99))},
			assert: func(t *testing.T, src *msginsql.Source, err error) {
				require.ErrorIs(t, err, msginsql.ErrInvalidStrategy)
				assert.Nil(t, src)
			},
		},
		{
			name:    "a negative Strategy is ErrInvalidStrategy",
			dialect: newFakeDialect(),
			opts:    []msginsql.Option{msginsql.WithStrategy(msginsql.Strategy(-1))},
			assert: func(t *testing.T, src *msginsql.Source, err error) {
				require.ErrorIs(t, err, msginsql.ErrInvalidStrategy)
				assert.Nil(t, src)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// A lazy fake-driver handle (no connection); every assertion here
			// is construction-time only.
			db := openDB(t, fakeDriverName)
			src, err := msginsql.NewPollingSource(db, "msgs", tc.dialect, tc.opts...)
			tc.assert(t, src, err)
		})
	}
}
