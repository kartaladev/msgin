package crontest_test

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/kartaladev/msgin/adapter/cron"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLockerConformance runs the shared runLockerConformance body against
// every engine dialectFor pairs (PostgreSQL, MySQL, MariaDB, SQLite),
// certifying the per-engine LockerDialect SQL in adapter/cron/dialect.go
// under real concurrency — the exactly-one-winner property a driver-free fake
// cannot prove.
func TestLockerConformance(t *testing.T) {
	for _, eng := range dialectFor() {
		t.Run(eng.name, func(t *testing.T) {
			db := eng.runDB(t)
			runLockerConformance(t, db, eng.locker)
		})
	}
}

// runLockerConformance exercises cron.SQLLocker end to end against a real db,
// using dialect. Every engine (postgres/mysql/mariadb/sqlite) runs this exact
// body, so the won/lost verdict branches in dialect.go execute on real SQL for
// all four.
func runLockerConformance(t *testing.T, db *sql.DB, dialect cron.LockerDialect) {
	t.Helper()

	ctx := t.Context()

	locker, err := cron.NewSQLLocker(db, dialect, cron.WithLockerTable("msgin_cron_fired_conformance"))
	require.NoError(t, err)

	require.NoError(t, locker.EnsureSchema(ctx), "EnsureSchema")
	require.NoError(t, locker.EnsureSchema(ctx), "EnsureSchema must be idempotent")

	// Shared fire: a fixed value so every concurrent goroutine computes the
	// identical (scope, fire) dedup key.
	fire := time.Date(2026, 1, 2, 3, 0, 0, 0, time.UTC)

	t.Run("exactly one winner under concurrency", func(t *testing.T) {
		const n = 8
		scope := "job-exactly-one-winner"

		wons := make([]bool, n)
		errs := make([]error, n)
		var wg sync.WaitGroup
		for i := range n {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				// Each goroutine writes only its own slice index — race-safe
				// without extra synchronization.
				wons[i], errs[i] = locker.Claim(ctx, scope, fire)
			}(i)
		}
		wg.Wait()

		wins := 0
		for i := range n {
			require.NoError(t, errs[i])
			if wons[i] {
				wins++
			}
		}
		assert.Equal(t, 1, wins, "exactly one of %d concurrent claims for the same (scope, fire) must win", n)
	})

	type bothWinCase struct {
		name   string
		scopeA string
		fireA  time.Time
		scopeB string
		fireB  time.Time
		assert func(t *testing.T, wonA, wonB bool, errA, errB error)
	}
	bothWin := func(t *testing.T, wonA, wonB bool, errA, errB error) {
		require.NoError(t, errA)
		require.NoError(t, errB)
		assert.True(t, wonA, "first claim should win")
		assert.True(t, wonB, "second claim should win")
	}
	bothWinCases := []bothWinCase{
		{
			name:   "different fire, same scope: both win",
			scopeA: "job-diff-fire",
			fireA:  fire,
			scopeB: "job-diff-fire",
			fireB:  fire.Add(time.Hour),
			assert: bothWin,
		},
		{
			name:   "different scope, same fire: both win",
			scopeA: "job-scope-a",
			fireA:  fire,
			scopeB: "job-scope-b",
			fireB:  fire,
			assert: bothWin,
		},
	}
	for _, tc := range bothWinCases {
		t.Run(tc.name, func(t *testing.T) {
			wonA, errA := locker.Claim(ctx, tc.scopeA, tc.fireA)
			wonB, errB := locker.Claim(ctx, tc.scopeB, tc.fireB)
			tc.assert(t, wonA, wonB, errA, errB)
		})
	}

	t.Run("purge removes an aged claim so a later claim wins again", func(t *testing.T) {
		scope := "job-purge"
		purgeFire := time.Date(2026, 1, 3, 4, 0, 0, 0, time.UTC)

		won, err := locker.Claim(ctx, scope, purgeFire)
		require.NoError(t, err)
		require.True(t, won, "first claim should win")

		won, err = locker.Claim(ctx, scope, purgeFire)
		require.NoError(t, err)
		require.False(t, won, "an un-purged claim must still be claimed")

		// Let claimed_at (DB clock) age past a tiny retention window.
		time.Sleep(50 * time.Millisecond)
		n, err := locker.Purge(ctx, 10*time.Millisecond)
		require.NoError(t, err)
		assert.GreaterOrEqual(t, n, int64(1), "purge should remove the aged row")

		won, err = locker.Claim(ctx, scope, purgeFire)
		require.NoError(t, err)
		assert.True(t, won, "after purge, the same fire should be re-claimable")
	})

	type nonPositiveRetentionCase struct {
		name   string
		older  time.Duration
		assert func(t *testing.T, err error)
	}
	nonPositiveRetentionCases := []nonPositiveRetentionCase{
		{
			name:   "zero retention is ErrInvalidRetention",
			older:  0,
			assert: func(t *testing.T, err error) { assert.ErrorIs(t, err, cron.ErrInvalidRetention) },
		},
		{
			name:   "negative retention is ErrInvalidRetention",
			older:  -time.Second,
			assert: func(t *testing.T, err error) { assert.ErrorIs(t, err, cron.ErrInvalidRetention) },
		},
	}
	for _, tc := range nonPositiveRetentionCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := locker.Purge(ctx, tc.older)
			tc.assert(t, err)
		})
	}
}

// TestMySQLLocker_ClaimDemotedError deterministically drives ClaimFire's
// demoted-error branch (ErrLockerClaimFailed) on a real MySQL server (Round-1
// audit H-2): a driver-free fake replaces mysqlLocker.ClaimFire outright and
// cannot reach it. The malformed table carries a row CHECK the Locker's
// INSERT never satisfies (guard defaults to 0, CHECK requires 1), so
// INSERT IGNORE demotes the CHECK violation to a warning inserting NO row; the
// verifying SELECT then finds nothing and ClaimFire falls through to
// ErrLockerClaimFailed (Round-2 audit NEW-HIGH-1 — a CHECK violation, not an
// omitted NOT-NULL column, which MySQL/MariaDB fill with an implicit default
// and insert anyway).
func TestMySQLLocker_ClaimDemotedError(t *testing.T) {
	runLockerClaimDemotedError(t, RunTestMySQL(t))
}

// TestMariaDBLocker_ClaimDemotedError is the MariaDB peer of
// TestMySQLLocker_ClaimDemotedError, proving the same demoted-error branch on
// MariaDB (CHECK is enforced from MariaDB 10.2.1+; the pinned mariadb:11.4
// image qualifies).
func TestMariaDBLocker_ClaimDemotedError(t *testing.T) {
	runLockerClaimDemotedError(t, RunTestMariaDB(t))
}

// runLockerClaimDemotedError creates the malformed fired-keys table directly
// (bypassing EnsureSchema, which would create the well-formed schema) then
// asserts Claim surfaces cron.ErrLockerClaimFailed.
func runLockerClaimDemotedError(t *testing.T, db *sql.DB) {
	t.Helper()

	ctx := t.Context()
	const table = "msgin_cron_fired_malformed"

	_, err := db.ExecContext(ctx, fmt.Sprintf(`CREATE TABLE %s (
  scope VARCHAR(255) NOT NULL,
  fire_ts DATETIME(6) NOT NULL,
  claimed_at DATETIME(6) NOT NULL DEFAULT (UTC_TIMESTAMP(6)),
  guard INT NOT NULL DEFAULT 0 CHECK (guard = 1),
  PRIMARY KEY (scope, fire_ts)
)`, table))
	require.NoError(t, err, "create malformed fired table")
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_, _ = db.ExecContext(cleanupCtx, "DROP TABLE IF EXISTS "+table)
	})

	locker, err := cron.NewSQLLocker(db, cron.MySQLLocker(), cron.WithLockerTable(table))
	require.NoError(t, err)

	_, err = locker.Claim(ctx, "job-demoted", time.Now())
	assert.ErrorIs(t, err, cron.ErrLockerClaimFailed)
}
