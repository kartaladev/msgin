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

// TestElectorConformance runs the shared runElectorConformance body against
// every engine dialectFor pairs (PostgreSQL, MySQL, MariaDB, SQLite). This is
// the ONLY place the MySQL three-step acquire-or-renew and the
// PG/SQLite ON CONFLICT ... WHERE ... RETURNING are proven correct under real
// concurrency, on real SQL, for all four engines.
func TestElectorConformance(t *testing.T) {
	for _, eng := range dialectFor() {
		t.Run(eng.name, func(t *testing.T) {
			db := eng.runDB(t)
			runElectorConformance(t, db, eng.elector)
		})
	}
}

// runElectorConformance exercises cron.SQLElector end to end against a real
// db, using dialect. Every engine runs this exact body.
func runElectorConformance(t *testing.T, db *sql.DB, dialect cron.ElectorDialect) {
	t.Helper()

	ctx := t.Context()
	const table = "msgin_cron_leader_conformance"

	newElector := func(t *testing.T, instanceID string, ttl time.Duration) *cron.SQLElector {
		t.Helper()
		e, err := cron.NewSQLElector(db, dialect,
			cron.WithElectorTable(table),
			cron.WithElectorInstanceID(instanceID),
			cron.WithLeaseTTL(ttl))
		require.NoError(t, err)
		return e
	}

	seed := newElector(t, "seed-instance", time.Minute)
	require.NoError(t, seed.EnsureSchema(ctx), "EnsureSchema")
	require.NoError(t, seed.EnsureSchema(ctx), "EnsureSchema must be idempotent")

	// DB-clock proof (item 6): SQLElector's constructor (NewSQLElector) and
	// its ElectorOptions (WithElectorTable/WithElectorInstanceID/
	// WithLeaseTTL) carry no clock parameter or WithClock-style option — every
	// AcquireOrRenew time computation goes through the DB server clock
	// (now()/UTC_TIMESTAMP(6)/unixepoch('now')), never an app-injected clock.
	// This is a compile-time property of the exported API (see
	// adapter/cron/sqlelector.go), not something a runtime assertion can add
	// to; verified here by inspection of the option set constructed above.

	t.Run("single leader under concurrency", func(t *testing.T) {
		const n = 8
		scope := "job-single-leader"

		electors := make([]*cron.SQLElector, n)
		for i := range n {
			electors[i] = newElector(t, fmt.Sprintf("single-leader-instance-%d", i), time.Minute)
		}

		results := make([]bool, n)
		errs := make([]error, n)
		var wg sync.WaitGroup
		for i := range n {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				results[i], errs[i] = electors[i].IsLeader(ctx, scope)
			}(i)
		}
		wg.Wait()

		leaders := 0
		for i := range n {
			require.NoError(t, errs[i])
			if results[i] {
				leaders++
			}
		}
		assert.Equal(t, 1, leaders, "exactly one of %d concurrent instances must become leader", n)
	})

	t.Run("renewal within TTL: winner stays leader, other instance is not", func(t *testing.T) {
		scope := "job-renewal"
		winner := newElector(t, "renewal-winner", time.Minute)
		other := newElector(t, "renewal-other", time.Minute)

		isLeader, err := winner.IsLeader(ctx, scope)
		require.NoError(t, err)
		require.True(t, isLeader, "the first caller for an unheld scope must become leader")

		// Renewal within TTL (long TTL, called again immediately): still leader.
		isLeader, err = winner.IsLeader(ctx, scope)
		require.NoError(t, err)
		assert.True(t, isLeader, "the incumbent renewing within its TTL must stay leader")

		// A different instance, same scope, lease not yet expired: not leader.
		isLeader, err = other.IsLeader(ctx, scope)
		require.NoError(t, err)
		assert.False(t, isLeader, "a non-incumbent must not acquire a live, unexpired lease")
	})

	t.Run("failover after lease expiry", func(t *testing.T) {
		scope := "job-failover"
		const shortTTL = 200 * time.Millisecond
		leader := newElector(t, "failover-leader", shortTTL)
		challenger := newElector(t, "failover-challenger", shortTTL)

		isLeader, err := leader.IsLeader(ctx, scope)
		require.NoError(t, err)
		require.True(t, isLeader)

		// The leader stops calling; wait for the lease to expire on the real
		// DB clock (a real wall-clock wait is legitimate here — the lease
		// uses the DB server clock, not an injected one) then a different
		// instance must take over. require.Eventually runs this closure on a
		// SPAWNED goroutine, so it returns false on mismatch rather than
		// calling require/t.Fatal itself.
		require.Eventually(t, func() bool {
			ok, err := challenger.IsLeader(ctx, scope)
			if err != nil {
				return false
			}
			return ok
		}, time.Second, 20*time.Millisecond,
			"a different instance should become leader once the short lease expires")
	})

	t.Run("distinct scopes are independent from one Elector instance", func(t *testing.T) {
		multi := newElector(t, "multi-scope-instance", time.Minute)

		leaderA, err := multi.IsLeader(ctx, "job-multi-scope-a")
		require.NoError(t, err)
		leaderB, err := multi.IsLeader(ctx, "job-multi-scope-b")
		require.NoError(t, err)

		// Both true from the SAME instance proves one SQLElector gates many
		// independent schedules (Round-1 audit M-1) — leadership of one scope
		// does not block leadership of another.
		assert.True(t, leaderA)
		assert.True(t, leaderB)
	})
}

// TestMySQLElector_AcquireDemotedError deterministically drives
// AcquireOrRenew's demoted-error branch (ErrElectorAcquireFailed) on a real
// MySQL server (Round-1 audit H-2, mirrors Step 4b for the Locker): a
// driver-free fake replaces mysqlElector.AcquireOrRenew outright and cannot
// reach it. The malformed lease table carries a row CHECK the Elector's
// INSERT never satisfies; with no existing row for the scope, Step 1's UPDATE
// affects 0 rows, Step 2's locking SELECT finds none, and Step 3's
// INSERT IGNORE demotes the CHECK violation to a warning inserting NO row —
// the re-check SELECT finds nothing and AcquireOrRenew falls through to
// ErrElectorAcquireFailed (Round-2 audit NEW-HIGH-1 — CHECK, not an omitted
// NOT-NULL column).
func TestMySQLElector_AcquireDemotedError(t *testing.T) {
	runElectorAcquireDemotedError(t, RunTestMySQL(t))
}

// TestMariaDBElector_AcquireDemotedError is the MariaDB peer of
// TestMySQLElector_AcquireDemotedError (CHECK enforced from MariaDB 10.2.1+;
// the pinned mariadb:11.4 image qualifies).
func TestMariaDBElector_AcquireDemotedError(t *testing.T) {
	runElectorAcquireDemotedError(t, RunTestMariaDB(t))
}

// runElectorAcquireDemotedError creates the malformed leader-lease table
// directly (bypassing EnsureSchema, which would create the well-formed
// schema) then asserts IsLeader surfaces cron.ErrElectorAcquireFailed.
func runElectorAcquireDemotedError(t *testing.T, db *sql.DB) {
	t.Helper()

	ctx := t.Context()
	const table = "msgin_cron_leader_malformed"

	_, err := db.ExecContext(ctx, fmt.Sprintf(`CREATE TABLE %s (
  scope VARCHAR(255) PRIMARY KEY,
  holder VARCHAR(255) NOT NULL,
  expires_at DATETIME(6) NOT NULL,
  guard INT NOT NULL DEFAULT 0 CHECK (guard = 1)
)`, table))
	require.NoError(t, err, "create malformed lease table")
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_, _ = db.ExecContext(cleanupCtx, "DROP TABLE IF EXISTS "+table)
	})

	elector, err := cron.NewSQLElector(db, cron.MySQLElector(), cron.WithElectorTable(table))
	require.NoError(t, err)

	_, err = elector.IsLeader(ctx, "job-demoted")
	assert.ErrorIs(t, err, cron.ErrElectorAcquireFailed)
}
