package sql_test

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	msgin "github.com/kartaladev/msgin"
	msginsql "github.com/kartaladev/msgin/adapter/database/sql"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

const (
	testLockedBy = "worker-1"
	// leaseTTL comfortably longer than any test operation, so a freshly
	// claimed row is NOT re-claimable while the test still holds its lease.
	longLeaseTTL = 5 * time.Minute
)

// PostgresDialectSuite provisions one PostgreSQL container for the whole suite
// (per use-testcontainers) and exercises PostgresDialect() against it. Each
// test method uses a freshly-named table so cases stay isolated.
type PostgresDialectSuite struct {
	suite.Suite
	db      *sql.DB
	dialect msginsql.Dialect
	counter atomic.Int64
}

func TestPostgresDialectSuite(t *testing.T) {
	suite.Run(t, new(PostgresDialectSuite))
}

func (s *PostgresDialectSuite) SetupSuite() {
	s.db = RunTestDatabase(s.T())
	s.dialect = msginsql.PostgresDialect()
}

// freshTable returns a unique, schema-applied table name for a single test.
func (s *PostgresDialectSuite) freshTable(ctx context.Context) string {
	name := fmt.Sprintf("msgin_msgs_%d", s.counter.Add(1))
	require.NoError(s.T(), s.dialect.EnsureSchema(ctx, s.db, name))
	return name
}

// insert frames empty headers and inserts one immediately-visible message.
func (s *PostgresDialectSuite) insert(ctx context.Context, table, msgID string, payload []byte) {
	headers, err := msginsql.EncodeHeaders(msgin.NewHeaders(map[string]any{msgin.HeaderID: msgID}))
	require.NoError(s.T(), err)
	require.NoError(s.T(), s.dialect.Insert(ctx, s.db, table, msgID, headers, payload, 0))
}

func (s *PostgresDialectSuite) TestSchemaExistsAndEnsureSchemaIdempotent() {
	ctx := s.T().Context()
	t := s.T()

	name := fmt.Sprintf("msgin_ensure_%d", s.counter.Add(1))

	exists, err := s.dialect.SchemaExists(ctx, s.db, name)
	require.NoError(t, err)
	require.False(t, exists, "table must not exist before EnsureSchema")

	require.NoError(t, s.dialect.EnsureSchema(ctx, s.db, name))

	exists, err = s.dialect.SchemaExists(ctx, s.db, name)
	require.NoError(t, err)
	require.True(t, exists, "table must exist after EnsureSchema")

	// Second call is a no-op (IF NOT EXISTS), not an error.
	require.NoError(t, s.dialect.EnsureSchema(ctx, s.db, name), "EnsureSchema must be idempotent")
}

func (s *PostgresDialectSuite) TestReferenceDDLApplies() {
	ctx := s.T().Context()
	t := s.T()

	name := fmt.Sprintf("msgin_refddl_%d", s.counter.Add(1))
	ddl, err := msginsql.PostgresDDL(name)
	require.NoError(t, err)

	// The reference DDL is two statements; apply each (pgx's extended protocol
	// rejects multi-statement Exec), mirroring what a migration tool does.
	for _, stmt := range splitStatements(ddl) {
		_, err := s.db.ExecContext(ctx, stmt)
		require.NoError(t, err, "apply reference DDL statement")
	}

	exists, err := s.dialect.SchemaExists(ctx, s.db, name)
	require.NoError(t, err)
	require.True(t, exists)
}

func (s *PostgresDialectSuite) TestClaimBumpsCountsAndLocks() {
	ctx := s.T().Context()
	t := s.T()

	table := s.freshTable(ctx)
	s.insert(ctx, table, "m-1", []byte("hello"))

	rows, err := s.dialect.Claim(ctx, s.db, table, 10, testLockedBy, longLeaseTTL)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, "m-1", rows[0].MsgID)
	require.Equal(t, []byte("hello"), rows[0].Payload)
	require.Equal(t, 1, rows[0].DeliveryCount, "delivery_count post-increment")
	require.Equal(t, int64(1), rows[0].LeaseEpoch, "lease_epoch post-increment")

	// Headers round-trip through the jsonb column.
	hdrs, err := msginsql.DecodeHeaders(rows[0].Headers)
	require.NoError(t, err)
	id, ok := hdrs.String(msgin.HeaderID)
	require.True(t, ok)
	require.Equal(t, "m-1", id)

	// The row is now leased (not expired), so an immediate re-claim sees nothing.
	rows, err = s.dialect.Claim(ctx, s.db, table, 10, testLockedBy, longLeaseTTL)
	require.NoError(t, err)
	require.Empty(t, rows, "a live-leased row is not re-claimable")
}

func (s *PostgresDialectSuite) TestClaimEmptyWhenNoRows() {
	ctx := s.T().Context()
	t := s.T()

	table := s.freshTable(ctx)
	rows, err := s.dialect.Claim(ctx, s.db, table, 10, testLockedBy, longLeaseTTL)
	require.NoError(t, err)
	require.Empty(t, rows)
}

func (s *PostgresDialectSuite) TestAckFenced() {
	ctx := s.T().Context()

	type testCase struct {
		name   string
		mutate func(id int64, epoch int64) (int64, string, int64) // -> id, lockedBy, epoch to Ack with
		assert func(t *testing.T, applied bool, err error)
	}

	cases := []testCase{
		{
			name: "correct fence deletes the row",
			mutate: func(id, epoch int64) (int64, string, int64) {
				return id, testLockedBy, epoch
			},
			assert: func(t *testing.T, applied bool, err error) {
				require.NoError(t, err)
				require.True(t, applied)
			},
		},
		{
			name: "stale epoch is a no-op",
			mutate: func(id, epoch int64) (int64, string, int64) {
				return id, testLockedBy, epoch + 99
			},
			assert: func(t *testing.T, applied bool, err error) {
				require.NoError(t, err)
				require.False(t, applied)
			},
		},
		{
			name: "wrong owner is a no-op",
			mutate: func(id, epoch int64) (int64, string, int64) {
				return id, "someone-else", epoch
			},
			assert: func(t *testing.T, applied bool, err error) {
				require.NoError(t, err)
				require.False(t, applied)
			},
		},
	}

	for _, tc := range cases {
		s.Run(tc.name, func() {
			t := s.T()
			table := s.freshTable(ctx)
			s.insert(ctx, table, "ack-msg", []byte("x"))

			claimed, err := s.dialect.Claim(ctx, s.db, table, 1, testLockedBy, longLeaseTTL)
			require.NoError(t, err)
			require.Len(t, claimed, 1)

			id, owner, epoch := tc.mutate(claimed[0].ID, claimed[0].LeaseEpoch)
			applied, err := s.dialect.Ack(ctx, s.db, table, id, owner, epoch)
			tc.assert(t, applied, err)
		})
	}
}

func (s *PostgresDialectSuite) TestNackDelaysVisibility() {
	ctx := s.T().Context()
	t := s.T()

	table := s.freshTable(ctx)
	s.insert(ctx, table, "nack-msg", []byte("y"))

	claimed, err := s.dialect.Claim(ctx, s.db, table, 1, testLockedBy, longLeaseTTL)
	require.NoError(t, err)
	require.Len(t, claimed, 1)

	const delay = 1500 * time.Millisecond
	applied, err := s.dialect.Nack(ctx, s.db, table, claimed[0].ID, testLockedBy, claimed[0].LeaseEpoch, delay)
	require.NoError(t, err)
	require.True(t, applied)

	// Not yet visible: visible_after is in the future.
	rows, err := s.dialect.Claim(ctx, s.db, table, 1, testLockedBy, longLeaseTTL)
	require.NoError(t, err)
	require.Empty(t, rows, "nacked row must stay invisible until the delay elapses")

	// Real wait past the delay (DB-clock behavior can't use a fake clock).
	time.Sleep(delay + 1*time.Second)

	rows, err = s.dialect.Claim(ctx, s.db, table, 1, testLockedBy, longLeaseTTL)
	require.NoError(t, err)
	require.Len(t, rows, 1, "nacked row becomes claimable after the delay")
	require.Equal(t, 2, rows[0].DeliveryCount, "re-claim bumps delivery_count again")
	require.Equal(t, int64(2), rows[0].LeaseEpoch)
}

func (s *PostgresDialectSuite) TestExpiredLeaseReclaim() {
	ctx := s.T().Context()
	t := s.T()

	table := s.freshTable(ctx)
	s.insert(ctx, table, "lease-msg", []byte("z"))

	const shortLeaseTTL = 1 * time.Second

	claimed, err := s.dialect.Claim(ctx, s.db, table, 1, testLockedBy, shortLeaseTTL)
	require.NoError(t, err)
	require.Len(t, claimed, 1)
	require.Equal(t, 1, claimed[0].DeliveryCount)

	// Before expiry, the lease still holds: no re-claim.
	rows, err := s.dialect.Claim(ctx, s.db, table, 1, testLockedBy, shortLeaseTTL)
	require.NoError(t, err)
	require.Empty(t, rows, "lease not yet expired")

	// Wait past the lease TTL with a generous margin (real time; DB clock).
	time.Sleep(shortLeaseTTL + 1500*time.Millisecond)

	rows, err = s.dialect.Claim(ctx, s.db, table, 1, testLockedBy, shortLeaseTTL)
	require.NoError(t, err)
	require.Len(t, rows, 1, "expired lease is reclaimable")
	require.Equal(t, 2, rows[0].DeliveryCount, "reclaim bumps delivery_count")
	require.Equal(t, int64(2), rows[0].LeaseEpoch, "reclaim bumps lease_epoch")
}

func (s *PostgresDialectSuite) TestInsertDelayHidesUntilVisible() {
	ctx := s.T().Context()
	t := s.T()

	table := s.freshTable(ctx)
	headers, err := msginsql.EncodeHeaders(msgin.NewHeaders(nil))
	require.NoError(t, err)

	const delay = 1500 * time.Millisecond
	require.NoError(t, s.dialect.Insert(ctx, s.db, table, "delayed", headers, []byte("d"), delay))

	rows, err := s.dialect.Claim(ctx, s.db, table, 1, testLockedBy, longLeaseTTL)
	require.NoError(t, err)
	require.Empty(t, rows, "delayed insert is invisible until visible_after")

	time.Sleep(delay + 1*time.Second)

	rows, err = s.dialect.Claim(ctx, s.db, table, 1, testLockedBy, longLeaseTTL)
	require.NoError(t, err)
	require.Len(t, rows, 1)
}

// splitStatements splits the reference DDL into individual statements on the
// ";" boundary, dropping empty pieces — what a migration tool does when it runs
// each statement on its own (pgx's extended protocol rejects multi-statement
// Exec).
func splitStatements(ddl string) []string {
	var out []string
	for _, part := range strings.Split(ddl, ";") {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}
