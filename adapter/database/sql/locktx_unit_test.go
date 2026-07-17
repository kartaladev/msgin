package sql_test

// locktx_unit_test.go covers the exported BeginLockTx/SettleLockTx helpers
// (Plan 006 Task 2, audit R2-1 correcting F6): the root fake LockDialect
// (fakedialect_test.go) always carries LockedRow{Tx: nil} and never routes
// through these helpers, and the dbtest conformance run (Tasks 4-5) only ever
// exercises their happy path — so the SettleLockTx rollback arms
// (Exec-error, Commit-error) and BeginLockTx's unsupported-Querier error would
// otherwise be uncovered anywhere. This file covers every arm directly, with
// a minimal, stdlib-only fake database/sql/driver registered via sql.Register
// and opened via sql.Open — zero new dependencies.

import (
	"context"
	stdsql "database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"

	msginsql "github.com/kartaladev/msgin/adapter/database/sql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// errStubExec/errStubCommit are the forced errors the stub driver returns,
// selected by the DSN a test opens its *sql.DB with.
var (
	errStubExec   = errors.New("stub driver: forced exec error")
	errStubCommit = errors.New("stub driver: forced commit error")
)

const (
	dsnOK        = "ok"
	dsnExecErr   = "execerr"
	dsnCommitErr = "commiterr"
)

// stubTx is a driver.Tx whose Commit/Rollback can be forced to fail (per the
// owning conn's DSN) and that records which one actually happened, so a test
// can assert the settle outcome precisely (not just "an error came back").
type stubTx struct {
	commitErr  error
	committed  atomic.Bool
	rolledBack atomic.Bool
}

func (tx *stubTx) Commit() error {
	if tx.commitErr != nil {
		return tx.commitErr
	}
	tx.committed.Store(true)
	return nil
}

func (tx *stubTx) Rollback() error {
	tx.rolledBack.Store(true)
	return nil
}

// stubStmt is a driver.Stmt whose Exec can be forced to fail (per the owning
// conn's DSN).
type stubStmt struct {
	execErr error
}

func (s *stubStmt) Close() error  { return nil }
func (s *stubStmt) NumInput() int { return -1 } // skip arg-count validation
func (s *stubStmt) Exec(_ []driver.Value) (driver.Result, error) {
	if s.execErr != nil {
		return nil, s.execErr
	}
	return driver.RowsAffected(1), nil
}
func (s *stubStmt) Query(_ []driver.Value) (driver.Rows, error) {
	return nil, errors.New("stub driver: Query not supported")
}

// stubConn is a driver.Conn (+ driver.ConnBeginTx, so *sql.DB never falls back
// to the legacy ctx-watcher-goroutine path) whose behavior is selected by the
// DSN it was Open'd with: dsnOK is a happy path, dsnExecErr forces the
// settle's Exec to fail, dsnCommitErr forces its Commit to fail. It reports
// itself closed on the owning stubDriver's counters (the no-leak assertion).
type stubConn struct {
	driver *stubDriver
	dsn    string
	closed atomic.Bool
	lastTx atomic.Pointer[stubTx]
}

func (c *stubConn) Prepare(_ string) (driver.Stmt, error) {
	var execErr error
	if c.dsn == dsnExecErr {
		execErr = errStubExec
	}
	return &stubStmt{execErr: execErr}, nil
}

func (c *stubConn) Close() error {
	if c.closed.CompareAndSwap(false, true) {
		c.driver.closeCount.Add(1)
	}
	return nil
}

func (c *stubConn) Begin() (driver.Tx, error) {
	var commitErr error
	if c.dsn == dsnCommitErr {
		commitErr = errStubCommit
	}
	tx := &stubTx{commitErr: commitErr}
	c.lastTx.Store(tx)
	return tx, nil
}

// BeginTx satisfies driver.ConnBeginTx so database/sql uses it directly
// instead of the legacy Begin()+ctx-watcher-goroutine fallback.
func (c *stubConn) BeginTx(_ context.Context, _ driver.TxOptions) (driver.Tx, error) {
	return c.Begin()
}

// stubDriver is the registered database/sql/driver.Driver; openCount/
// closeCount let a test assert no connection leak across a BeginLockTx/
// SettleLockTx round trip. Each test registers its OWN instance under a
// unique name (see newStubDB) so parallel subtests never share counters.
type stubDriver struct {
	openCount  atomic.Int64
	closeCount atomic.Int64
	lastConn   atomic.Pointer[stubConn]
}

func (d *stubDriver) Open(dsn string) (driver.Conn, error) {
	d.openCount.Add(1)
	c := &stubConn{driver: d, dsn: dsn}
	d.lastConn.Store(c)
	return c, nil
}

var stubDriverSeq atomic.Int64

// newStubDB registers a fresh, uniquely-named stubDriver and opens a *sql.DB
// on it with the given DSN (selecting ok/execerr/commiterr conn behavior),
// closing it via t.Cleanup. A fresh driver per call keeps parallel subtests'
// open/close counters independent.
func newStubDB(t *testing.T, dsn string) (*stdsql.DB, *stubDriver) {
	t.Helper()
	d := &stubDriver{}
	name := fmt.Sprintf("msgin-locktx-stub-%d", stubDriverSeq.Add(1))
	stdsql.Register(name, d)
	db, err := stdsql.Open(name, dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return db, d
}

// TestBeginLockTx covers all three of BeginLockTx's resolution branches: the
// txBeginner (*sql.DB) path begins a new transaction, an already-open *sql.Tx
// is carried unchanged, and neither is a clear, wrapped error (no panic, no
// silently-nil transaction).
func TestBeginLockTx(t *testing.T) {
	t.Parallel()

	type testCase struct {
		name   string
		setup  func(t *testing.T) (q msginsql.Querier, wantSame *stdsql.Tx)
		assert func(t *testing.T, tx *stdsql.Tx, err error, wantSame *stdsql.Tx)
	}

	cases := []testCase{
		{
			name: "txBeginner path (*sql.DB) begins a new transaction",
			setup: func(t *testing.T) (msginsql.Querier, *stdsql.Tx) {
				db, _ := newStubDB(t, dsnOK)
				return db, nil
			},
			assert: func(t *testing.T, tx *stdsql.Tx, err error, _ *stdsql.Tx) {
				require.NoError(t, err)
				require.NotNil(t, tx)
				assert.NoError(t, tx.Rollback())
			},
		},
		{
			name: "an already-open *sql.Tx is carried unchanged",
			setup: func(t *testing.T) (msginsql.Querier, *stdsql.Tx) {
				db, _ := newStubDB(t, dsnOK)
				outer, err := db.BeginTx(t.Context(), nil)
				require.NoError(t, err)
				t.Cleanup(func() { _ = outer.Rollback() })
				return outer, outer
			},
			assert: func(t *testing.T, tx *stdsql.Tx, err error, wantSame *stdsql.Tx) {
				require.NoError(t, err)
				assert.Same(t, wantSame, tx)
			},
		},
		{
			name: "neither *sql.DB nor *sql.Tx is a clear, wrapped error",
			setup: func(t *testing.T) (msginsql.Querier, *stdsql.Tx) {
				return fakeQuerier{}, nil
			},
			assert: func(t *testing.T, tx *stdsql.Tx, err error, _ *stdsql.Tx) {
				require.Error(t, err)
				assert.Nil(t, tx)
				assert.Contains(t, err.Error(), "requires a *sql.DB or *sql.Tx")
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			q, wantSame := tc.setup(t)
			tx, err := msginsql.BeginLockTx(t.Context(), q)
			tc.assert(t, tx, err, wantSame)
		})
	}
}

// TestBeginLockTx_NoConnectionLeak proves BeginLockTx's txBeginner path does
// not leak a pooled connection: every conn the stub driver opens during the
// round trip is closed once the pool (*sql.DB) is closed.
func TestBeginLockTx_NoConnectionLeak(t *testing.T) {
	t.Parallel()

	db, drv := newStubDB(t, dsnOK)
	tx, err := msginsql.BeginLockTx(t.Context(), db)
	require.NoError(t, err)
	require.NoError(t, tx.Rollback())
	require.NoError(t, db.Close())

	assert.Equal(t, drv.openCount.Load(), drv.closeCount.Load(),
		"BeginLockTx must not leak a pooled connection")
}

// TestSettleLockTx covers all three of SettleLockTx's outcomes: success
// (Exec ok + Commit ok), an Exec error (rollback, error returned), and a
// Commit error (rollback, error returned) — the two arms that neither the
// fake LockDialect (LockedRow.Tx is always nil) nor the dbtest happy-path
// conformance run ever reaches.
func TestSettleLockTx(t *testing.T) {
	t.Parallel()

	type testCase struct {
		name   string
		dsn    string
		assert func(t *testing.T, tx *stdsql.Tx, drv *stubDriver, err error)
	}

	cases := []testCase{
		{
			name: "success: Exec ok + Commit ok",
			dsn:  dsnOK,
			assert: func(t *testing.T, tx *stdsql.Tx, drv *stubDriver, err error) {
				require.NoError(t, err)
				assert.ErrorIs(t, tx.Rollback(), stdsql.ErrTxDone, "tx must already be settled (committed)")
				stub := drv.lastConn.Load().lastTx.Load()
				assert.True(t, stub.committed.Load())
				assert.False(t, stub.rolledBack.Load())
			},
		},
		{
			name: "Exec error rolls back and returns the error",
			dsn:  dsnExecErr,
			assert: func(t *testing.T, tx *stdsql.Tx, drv *stubDriver, err error) {
				require.ErrorIs(t, err, errStubExec)
				assert.ErrorIs(t, tx.Commit(), stdsql.ErrTxDone, "tx must already be settled (rolled back)")
				stub := drv.lastConn.Load().lastTx.Load()
				assert.True(t, stub.rolledBack.Load())
				assert.False(t, stub.committed.Load())
			},
		},
		{
			// database/sql marks a *sql.Tx "done" as soon as Commit is
			// CALLED, success or not (see Tx.Commit's done.CompareAndSwap),
			// so SettleLockTx's follow-up tx.Rollback() on a failed commit
			// is a documented no-op (SettleLockTx's own comment: "no-op
			// after a failed commit, but explicit: never leak the conn") —
			// it never reaches the driver's Rollback. The connection is
			// still released back to the pool by the failed Commit call
			// itself (proved by the no-leak assertion below), and the
			// driver-level tx is left neither committed nor rolled back.
			name: "Commit error returns the error; the conn is still released (no leak)",
			dsn:  dsnCommitErr,
			assert: func(t *testing.T, tx *stdsql.Tx, drv *stubDriver, err error) {
				require.ErrorIs(t, err, errStubCommit)
				assert.ErrorIs(t, tx.Rollback(), stdsql.ErrTxDone, "tx must already be settled by the failed Commit call")
				stub := drv.lastConn.Load().lastTx.Load()
				assert.False(t, stub.committed.Load())
				assert.False(t, stub.rolledBack.Load(),
					"the driver-level Rollback is never reached; database/sql already marked the tx done on the failed Commit")
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			db, drv := newStubDB(t, tc.dsn)
			tx, err := db.BeginTx(t.Context(), nil)
			require.NoError(t, err)

			settleErr := msginsql.SettleLockTx(t.Context(), tx, "DELETE FROM msgs WHERE id = 1")
			tc.assert(t, tx, drv, settleErr)

			require.NoError(t, db.Close())
			assert.Equal(t, drv.openCount.Load(), drv.closeCount.Load(), "SettleLockTx must not leak a pooled connection")
		})
	}
}
