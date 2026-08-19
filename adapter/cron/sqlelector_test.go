package cron_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	msgin "github.com/kartaladev/msgin"
	"github.com/kartaladev/msgin/adapter/cron"
)

type fakeElectorDialect struct {
	leader     bool
	err        error
	calls      int
	lastScope  string
	lastHolder string
	lastTTL    time.Duration
	ensureErr  error
}

func (f *fakeElectorDialect) AcquireOrRenew(_ context.Context, _ cron.Querier, _, scope, holder string, ttl time.Duration) (bool, error) {
	f.calls++
	f.lastScope, f.lastHolder, f.lastTTL = scope, holder, ttl
	return f.leader, f.err
}
func (f *fakeElectorDialect) EnsureLeaseSchema(context.Context, cron.Querier, string) error {
	return f.ensureErr
}

// TestSQLElector_Construction covers NewSQLElector's validation order — db==nil
// (msgin.ErrNilAdapter) → dialect==nil (ErrNilDialect) → invalid table
// (ErrInvalidTableName) → non-positive lease TTL (ErrInvalidLeaseTTL) —
// matching NewSQLLocker's db → dialect → table order.
func TestSQLElector_Construction(t *testing.T) {
	type testCase struct {
		name   string
		build  func() (*cron.SQLElector, error)
		assert func(t *testing.T, err error)
	}
	cases := []testCase{
		{
			name:   "nil db is ErrNilAdapter",
			build:  func() (*cron.SQLElector, error) { return cron.NewSQLElector(nil, cron.PostgresElector()) },
			assert: func(t *testing.T, err error) { assert.ErrorIs(t, err, msginErrNilAdapter()) },
		},
		{
			name:   "nil dialect is ErrNilDialect",
			build:  func() (*cron.SQLElector, error) { return cron.NewSQLElector(stubDB(t), nil) },
			assert: func(t *testing.T, err error) { assert.ErrorIs(t, err, cron.ErrNilDialect) },
		},
		{
			// Locks the construction-error priority: db==nil is checked first
			// (covered above), then dialect==nil, then table validity — so a
			// nil dialect co-occurring with an invalid table still reports
			// ErrNilDialect, never ErrInvalidTableName. Mirrors
			// sqllock_test.go's equivalent case.
			name: "nil dialect AND invalid table: dialect check wins (ErrNilDialect)",
			build: func() (*cron.SQLElector, error) {
				return cron.NewSQLElector(stubDB(t), nil, cron.WithElectorTable("bad;"))
			},
			assert: func(t *testing.T, err error) { assert.ErrorIs(t, err, cron.ErrNilDialect) },
		},
		{
			name: "invalid table is ErrInvalidTableName",
			build: func() (*cron.SQLElector, error) {
				return cron.NewSQLElector(stubDB(t), cron.PostgresElector(), cron.WithElectorTable("bad;"))
			},
			assert: func(t *testing.T, err error) { assert.ErrorIs(t, err, cron.ErrInvalidTableName) },
		},
		{
			name: "non-positive lease TTL is ErrInvalidLeaseTTL",
			build: func() (*cron.SQLElector, error) {
				return cron.NewSQLElector(stubDB(t), cron.PostgresElector(), cron.WithLeaseTTL(0))
			},
			assert: func(t *testing.T, err error) { assert.ErrorIs(t, err, cron.ErrInvalidLeaseTTL) },
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tc.build()
			tc.assert(t, err)
		})
	}
}

// TestNewSQLElector_NilOptionElement proves a nil ELEMENT of opts is a bare
// ErrNilFunc naming the computed 0-based index (Spec 015 §3.1, family R1).
// NewSQLElector is VALIDATE-FIRST: the nil-db and nil-dialect checks precede
// the apply loop and beat a nil option, but the loop itself precedes the
// table-name and lease-TTL validation, so a nil option beats both of those.
func TestNewSQLElector_NilOptionElement(t *testing.T) {
	tests := []struct {
		name    string
		dialect cron.ElectorDialect
		opts    []cron.ElectorOption
		assert  func(t *testing.T, err error)
	}{
		{
			name:    "nil element alone",
			dialect: cron.PostgresElector(),
			opts:    []cron.ElectorOption{nil},
			assert: func(t *testing.T, err error) {
				require.ErrorIs(t, err, msgin.ErrNilFunc)
				assert.False(t, msgin.IsPermanent(err), "R1 nil-option error must be BARE, not Permanent-wrapped")
				assert.Contains(t, err.Error(), "cron.NewSQLElector: nil option at index 0")
			},
		},
		{
			name:    "nil element after a valid option asserts the COMPUTED index and the FULL position",
			dialect: cron.PostgresElector(),
			opts:    []cron.ElectorOption{cron.WithElectorTable("custom_leader"), nil},
			assert: func(t *testing.T, err error) {
				require.ErrorIs(t, err, msgin.ErrNilFunc)
				assert.Contains(t, err.Error(), "cron.NewSQLElector: nil option at index 1")
			},
		},
		{
			// AC-3: first-nil-wins.
			name:    "first of two nils wins",
			dialect: cron.PostgresElector(),
			opts:    []cron.ElectorOption{nil, nil},
			assert: func(t *testing.T, err error) {
				require.ErrorIs(t, err, msgin.ErrNilFunc)
				assert.Contains(t, err.Error(), "cron.NewSQLElector: nil option at index 0")
			},
		},
		{
			// Precedence (Spec 015 §3.5): VALIDATE-FIRST — the loop runs AFTER
			// the db/dialect checks, so a nil option loses to a nil dialect.
			name:    "nil dialect wins over a nil option (dialect check precedes the loop)",
			dialect: nil,
			opts:    []cron.ElectorOption{nil},
			assert: func(t *testing.T, err error) {
				assert.ErrorIs(t, err, cron.ErrNilDialect)
				assert.NotErrorIs(t, err, msgin.ErrNilFunc)
			},
		},
		{
			// Precedence (Spec 015 §3.5): the loop precedes the table-name
			// validation, so a nil option wins over an invalid explicit table name.
			name:    "nil option precedes the table-name validation",
			dialect: cron.PostgresElector(),
			opts:    []cron.ElectorOption{cron.WithElectorTable("bad;"), nil},
			assert: func(t *testing.T, err error) {
				require.ErrorIs(t, err, msgin.ErrNilFunc)
				assert.NotErrorIs(t, err, cron.ErrInvalidTableName)
				assert.Contains(t, err.Error(), "cron.NewSQLElector: nil option at index 1")
			},
		},
		{
			// Precedence (Spec 015 §3.5): the loop precedes the lease-TTL
			// validation, so a nil option wins over a non-positive TTL.
			name:    "nil option precedes the lease-TTL validation",
			dialect: cron.PostgresElector(),
			opts:    []cron.ElectorOption{cron.WithLeaseTTL(0), nil},
			assert: func(t *testing.T, err error) {
				require.ErrorIs(t, err, msgin.ErrNilFunc)
				assert.NotErrorIs(t, err, cron.ErrInvalidLeaseTTL)
				assert.Contains(t, err.Error(), "cron.NewSQLElector: nil option at index 1")
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := cron.NewSQLElector(stubDB(t), tc.dialect, tc.opts...)
			tc.assert(t, err)
		})
	}
}

func TestSQLElector_IsLeaderDelegates(t *testing.T) {
	fd := &fakeElectorDialect{leader: true}
	e, err := cron.NewSQLElector(stubDB(t), fd,
		cron.WithElectorInstanceID("inst-1"), cron.WithLeaseTTL(45*time.Second))
	require.NoError(t, err)

	// scope is a per-call argument (Round-1 audit M-1), not baked at
	// construction — pass it explicitly, symmetric with SQLLocker.Claim.
	leader, err := e.IsLeader(t.Context(), "job-x")
	require.NoError(t, err)
	assert.True(t, leader)
	assert.Equal(t, "job-x", fd.lastScope)
	assert.Equal(t, "inst-1", fd.lastHolder)
	assert.Equal(t, 45*time.Second, fd.lastTTL)
}

func TestSQLElector_IsLeaderErrorPropagates(t *testing.T) {
	fd := &fakeElectorDialect{err: errors.New("db down")}
	e, err := cron.NewSQLElector(stubDB(t), fd)
	require.NoError(t, err)
	_, err = e.IsLeader(t.Context(), "s")
	assert.ErrorContains(t, err, "db down")
}

// TestSQLElector_DefaultTTL proves the 30s default reaches the dialect when
// WithLeaseTTL is unset.
func TestSQLElector_DefaultTTL(t *testing.T) {
	fd := &fakeElectorDialect{leader: true}
	e, err := cron.NewSQLElector(stubDB(t), fd)
	require.NoError(t, err)
	_, err = e.IsLeader(t.Context(), "s")
	require.NoError(t, err)
	assert.Equal(t, 30*time.Second, fd.lastTTL)
}

// TestSQLElector_EnsureSchema covers EnsureSchema's sole branch: it delegates
// to the dialect's EnsureLeaseSchema and passes through whatever it returns.
func TestSQLElector_EnsureSchema(t *testing.T) {
	type testCase struct {
		name   string
		fd     *fakeElectorDialect
		assert func(t *testing.T, err error)
	}
	cases := []testCase{
		{
			name:   "delegates to dialect and returns nil on success",
			fd:     &fakeElectorDialect{},
			assert: func(t *testing.T, err error) { assert.NoError(t, err) },
		},
		{
			name:   "dialect error propagates",
			fd:     &fakeElectorDialect{ensureErr: assert.AnError},
			assert: func(t *testing.T, err error) { assert.ErrorIs(t, err, assert.AnError) },
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e, err := cron.NewSQLElector(stubDB(t), tc.fd)
			require.NoError(t, err)
			err = e.EnsureSchema(t.Context())
			tc.assert(t, err)
		})
	}
}

func TestElectorDDL(t *testing.T) {
	for _, build := range []func(string) (string, error){
		cron.PostgresElectorDDL, cron.MySQLElectorDDL, cron.SQLiteElectorDDL,
	} {
		ddl, err := build("msgin_cron_leader")
		require.NoError(t, err)
		assert.Contains(t, ddl, "CREATE TABLE IF NOT EXISTS")
		assert.Contains(t, ddl, "holder")
		assert.Contains(t, ddl, "expires_at")
		_, err = build("bad; drop")
		assert.ErrorIs(t, err, cron.ErrInvalidTableName)
	}
}
