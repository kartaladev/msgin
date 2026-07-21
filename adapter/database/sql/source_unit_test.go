package sql_test

// source_unit_test.go drives Source (both the lease/claim and lock/FOR-UPDATE
// strategies) against the in-memory fakeDialect (fakedialect_test.go) — no
// real database (Plan 006 Task 2). It covers Poll's empty/batching/framing/
// corrupt-row branches, the Ack/Nack closures' applied-vs-stale fencing, and
// the classifyQueryErr wrap-vs-passthrough branch, for both strategies.

import (
	"errors"
	"testing"
	"time"

	msgin "github.com/kartaladev/msgin"
	msginsql "github.com/kartaladev/msgin/adapter/database/sql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---- construction (smoke-tests the fake as a LeaseDialect/LockDialect) ---

// TestNewPollingSource_WithFakeDialect proves the fake satisfies the
// LeaseDialect (and, under the lock strategy, LockDialect) contract at
// construction — the exhaustive nil-db/nil-dialect/invalid-table matrix is
// already covered against the built-ins in source_test.go (Task 1); this is
// deliberately not duplicated here.
func TestNewPollingSource_WithFakeDialect(t *testing.T) {
	t.Parallel()

	t.Run("lease strategy (default) constructs", func(t *testing.T) {
		t.Parallel()
		src, err := msginsql.NewPollingSource(openDB(t, fakeDriverName), "msgs", newFakeDialect())
		require.NoError(t, err)
		assert.NotNil(t, src)
	})

	t.Run("lock strategy constructs (the fake also implements LockDialect)", func(t *testing.T) {
		t.Parallel()
		src, err := msginsql.NewPollingSource(openDB(t, fakeDriverName), "msgs", newFakeDialect(),
			msginsql.WithStrategy(msginsql.StrategyLockForUpdate))
		require.NoError(t, err)
		assert.NotNil(t, src)
	})
}

// ---- pollLease -------------------------------------------------------------

func TestSource_PollLease(t *testing.T) {
	t.Parallel()

	t.Run("no claimable rows returns an empty batch, nil error", func(t *testing.T) {
		t.Parallel()
		fd := newFakeDialect()
		src, err := msginsql.NewPollingSource(openDB(t, fakeDriverName), "msgs", fd)
		require.NoError(t, err)

		dels, err := src.Poll(t.Context(), 10)
		require.NoError(t, err)
		assert.Empty(t, dels)
	})

	t.Run("claims up to max rows, framing each as a Delivery", func(t *testing.T) {
		t.Parallel()
		fd := newFakeDialect()
		src, err := msginsql.NewPollingSource(openDB(t, fakeDriverName), "msgs", fd)
		require.NoError(t, err)

		fd.insertRow("m-1", encodeHeadersT(t, "m-1"), []byte("p1"))
		fd.insertRow("m-2", encodeHeadersT(t, "m-2"), []byte("p2"))
		fd.insertRow("m-3", encodeHeadersT(t, "m-3"), []byte("p3"))

		dels, err := src.Poll(t.Context(), 2)
		require.NoError(t, err)
		require.Len(t, dels, 2, "Poll must respect max (batching)")

		for _, d := range dels {
			id, ok := d.Msg.Header(msgin.HeaderMessageID)
			require.True(t, ok)
			assert.Contains(t, []string{"m-1", "m-2", "m-3"}, id)
			dc, ok := d.Msg.Header(msgin.HeaderDeliveryCount)
			require.True(t, ok)
			assert.Equal(t, 1, dc, "first claim's live delivery-count overlay is 1")
			assert.NotNil(t, d.Ack)
			assert.NotNil(t, d.Nack)
		}
	})

	t.Run("a row with undecodable headers is skipped (not delivered) and penalized", func(t *testing.T) {
		t.Parallel()
		fixedNow := time.Date(2031, 1, 1, 0, 0, 0, 0, time.UTC)
		fd := newFakeDialect()
		fd.now = func() time.Time { return fixedNow }
		src, err := msginsql.NewPollingSource(openDB(t, fakeDriverName), "msgs", fd)
		require.NoError(t, err)

		id := fd.insertRow("m-corrupt", []byte("not-json"), []byte("p"))

		dels, err := src.Poll(t.Context(), 10)
		require.NoError(t, err)
		assert.Empty(t, dels, "a corrupt row must not be delivered")

		row := fd.row(id)
		assert.Equal(t, fixedNow.Add(30*time.Second), row.visibleAfter,
			"the first corrupt claim is penalized with the 30s initial backoff")
		assert.Empty(t, row.lockedBy, "the lease is released (Nacked), not held")
	})

	t.Run("a corrupt row whose penalty Nack itself fails is still skipped, not surfaced as a Poll error", func(t *testing.T) {
		t.Parallel()
		fd := newFakeDialect()
		src, err := msginsql.NewPollingSource(openDB(t, fakeDriverName), "msgs", fd)
		require.NoError(t, err)

		fd.insertRow("m-corrupt", []byte("not-json"), []byte("p"))
		fd.nackErr = errors.New("penalty nack boom")

		dels, err := src.Poll(t.Context(), 10)
		require.NoError(t, err, "the secondary Nack failure is logged, not returned")
		assert.Empty(t, dels)
	})

	t.Run("a Claim error is wrapped ErrSchemaNotReady when the table is missing", func(t *testing.T) {
		t.Parallel()
		fd := newFakeDialect()
		fd.claimErr = errors.New("boom")
		src, err := msginsql.NewPollingSource(openDB(t, fakeDriverName), "msgs", fd)
		require.NoError(t, err)

		_, err = src.Poll(t.Context(), 10)
		require.ErrorIs(t, err, msginsql.ErrSchemaNotReady)
	})

	t.Run("a Claim error propagates raw when the table exists", func(t *testing.T) {
		t.Parallel()
		fd := newFakeDialect()
		fd.markReady("msgs")
		fd.claimErr = errors.New("boom")
		src, err := msginsql.NewPollingSource(openDB(t, fakeDriverName), "msgs", fd)
		require.NoError(t, err)

		_, err = src.Poll(t.Context(), 10)
		require.Error(t, err)
		assert.NotErrorIs(t, err, msginsql.ErrSchemaNotReady)
		assert.Contains(t, err.Error(), "boom")
	})
}

// TestSource_AckClosure covers the lease Ack closure's fencing: applied
// (fence matches, row settled) vs stale (another worker reclaimed the lease
// in between -> ErrStaleLease, per ADR 0010 D4).
func TestSource_AckClosure(t *testing.T) {
	t.Parallel()

	type testCase struct {
		name       string
		forceStale bool
		assert     func(t *testing.T, fd *fakeDialect, id int64, err error)
	}

	cases := []testCase{
		{
			name: "applied: fence matches, row deleted",
			assert: func(t *testing.T, fd *fakeDialect, id int64, err error) {
				require.NoError(t, err)
				assert.False(t, fd.exists(id), "Ack must delete the settled row")
			},
		},
		{
			name:       "stale: fence mismatch (reclaimed elsewhere) returns ErrStaleLease",
			forceStale: true,
			assert: func(t *testing.T, fd *fakeDialect, id int64, err error) {
				require.ErrorIs(t, err, msginsql.ErrStaleLease)
				assert.True(t, fd.exists(id), "a stale settle must not delete the row owned by the reclaiming worker")
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fd := newFakeDialect()
			src, err := msginsql.NewPollingSource(openDB(t, fakeDriverName), "msgs", fd)
			require.NoError(t, err)

			id := fd.insertRow("m-1", encodeHeadersT(t, "m-1"), []byte("p"))
			dels, err := src.Poll(t.Context(), 1)
			require.NoError(t, err)
			require.Len(t, dels, 1)

			if tc.forceStale {
				fd.bumpEpoch(id)
			}

			ackErr := dels[0].Ack(t.Context())
			tc.assert(t, fd, id, ackErr)
		})
	}
}

// TestSource_NackClosure covers the lease Nack closure: applied-with-delay,
// the requeue=false -> delay-0 collapse (ADR 0010 D4), and stale fencing.
func TestSource_NackClosure(t *testing.T) {
	t.Parallel()

	fixedNow := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)

	type testCase struct {
		name       string
		forceStale bool
		requeue    bool
		delay      time.Duration
		assert     func(t *testing.T, fd *fakeDialect, id int64, err error)
	}

	cases := []testCase{
		{
			name:    "applied: requeue with delay pushes visibility out",
			requeue: true,
			delay:   time.Minute,
			assert: func(t *testing.T, fd *fakeDialect, id int64, err error) {
				require.NoError(t, err)
				row := fd.row(id)
				assert.Equal(t, fixedNow.Add(time.Minute), row.visibleAfter)
				assert.Empty(t, row.lockedBy, "Nack must clear the lock")
			},
		},
		{
			name:    "requeue=false collapses to an immediate delayed requeue (delay 0)",
			requeue: false,
			delay:   time.Hour, // must be ignored
			assert: func(t *testing.T, fd *fakeDialect, id int64, err error) {
				require.NoError(t, err)
				row := fd.row(id)
				assert.Equal(t, fixedNow, row.visibleAfter, "requeue=false must collapse the delay to 0")
			},
		},
		{
			name:       "stale: fence mismatch returns ErrStaleLease",
			forceStale: true,
			requeue:    true,
			delay:      time.Minute,
			assert: func(t *testing.T, fd *fakeDialect, id int64, err error) {
				require.ErrorIs(t, err, msginsql.ErrStaleLease)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fd := newFakeDialect()
			fd.now = func() time.Time { return fixedNow }
			src, err := msginsql.NewPollingSource(openDB(t, fakeDriverName), "msgs", fd)
			require.NoError(t, err)

			id := fd.insertRow("m-1", encodeHeadersT(t, "m-1"), []byte("p"))
			dels, err := src.Poll(t.Context(), 1)
			require.NoError(t, err)
			require.Len(t, dels, 1)

			if tc.forceStale {
				fd.bumpEpoch(id)
			}

			nackErr := dels[0].Nack(t.Context(), tc.requeue, tc.delay)
			tc.assert(t, fd, id, nackErr)
		})
	}
}

// ---- pollLock ---------------------------------------------------------

func TestSource_PollLock(t *testing.T) {
	t.Parallel()

	t.Run("no claimable rows returns nil, nil", func(t *testing.T) {
		t.Parallel()
		fd := newFakeDialect()
		src, err := msginsql.NewPollingSource(openDB(t, fakeDriverName), "msgs", fd,
			msginsql.WithStrategy(msginsql.StrategyLockForUpdate))
		require.NoError(t, err)

		dels, err := src.Poll(t.Context(), 10)
		require.NoError(t, err)
		assert.Empty(t, dels)
	})

	t.Run("claims exactly one row regardless of max, with a nil LockedRow.Tx never dereferenced", func(t *testing.T) {
		t.Parallel()
		fd := newFakeDialect()
		src, err := msginsql.NewPollingSource(openDB(t, fakeDriverName), "msgs", fd,
			msginsql.WithStrategy(msginsql.StrategyLockForUpdate))
		require.NoError(t, err)

		fd.insertRow("m-1", encodeHeadersT(t, "m-1"), []byte("p1"))
		fd.insertRow("m-2", encodeHeadersT(t, "m-2"), []byte("p2"))

		dels, err := src.Poll(t.Context(), 5) // max ignored by the lock strategy
		require.NoError(t, err)
		require.Len(t, dels, 1, "the lock strategy claims at most one row per Poll")

		dc, ok := dels[0].Msg.Header(msgin.HeaderDeliveryCount)
		require.True(t, ok)
		assert.Equal(t, 1, dc)
	})

	t.Run("a row with undecodable headers is NackLock'd (penalized) and skipped", func(t *testing.T) {
		t.Parallel()
		fixedNow := time.Date(2031, 6, 1, 0, 0, 0, 0, time.UTC)
		fd := newFakeDialect()
		fd.now = func() time.Time { return fixedNow }
		src, err := msginsql.NewPollingSource(openDB(t, fakeDriverName), "msgs", fd,
			msginsql.WithStrategy(msginsql.StrategyLockForUpdate))
		require.NoError(t, err)

		id := fd.insertRow("m-corrupt", []byte("not-json"), []byte("p"))

		dels, err := src.Poll(t.Context(), 10)
		require.NoError(t, err)
		assert.Empty(t, dels)

		row := fd.row(id)
		assert.False(t, row.locked, "NackLock releases the lock")
		assert.Equal(t, fixedNow.Add(30*time.Second), row.visibleAfter)
	})

	t.Run("a corrupt row whose penalty NackLock itself fails is still skipped, not surfaced as a Poll error", func(t *testing.T) {
		t.Parallel()
		fd := newFakeDialect()
		src, err := msginsql.NewPollingSource(openDB(t, fakeDriverName), "msgs", fd,
			msginsql.WithStrategy(msginsql.StrategyLockForUpdate))
		require.NoError(t, err)

		fd.insertRow("m-corrupt", []byte("not-json"), []byte("p"))
		fd.nackLockErr = errors.New("penalty nacklock boom")

		dels, err := src.Poll(t.Context(), 10)
		require.NoError(t, err, "the secondary NackLock failure is logged, not returned")
		assert.Empty(t, dels)
	})

	t.Run("a ClaimLock error is wrapped ErrSchemaNotReady when the table is missing", func(t *testing.T) {
		t.Parallel()
		fd := newFakeDialect()
		fd.claimLockErr = errors.New("boom")
		src, err := msginsql.NewPollingSource(openDB(t, fakeDriverName), "msgs", fd,
			msginsql.WithStrategy(msginsql.StrategyLockForUpdate))
		require.NoError(t, err)

		_, err = src.Poll(t.Context(), 10)
		require.ErrorIs(t, err, msginsql.ErrSchemaNotReady)
	})

	t.Run("a ClaimLock error propagates raw when the table exists", func(t *testing.T) {
		t.Parallel()
		fd := newFakeDialect()
		fd.markReady("msgs")
		fd.claimLockErr = errors.New("boom")
		src, err := msginsql.NewPollingSource(openDB(t, fakeDriverName), "msgs", fd,
			msginsql.WithStrategy(msginsql.StrategyLockForUpdate))
		require.NoError(t, err)

		_, err = src.Poll(t.Context(), 10)
		require.Error(t, err)
		assert.NotErrorIs(t, err, msginsql.ErrSchemaNotReady)
		assert.Contains(t, err.Error(), "boom")
	})
}

// TestSource_LockSettlement covers the lock strategy's Ack (AckLock: DELETE +
// commit) and Nack (NackLock: clear-lock + ALWAYS commit) closures, including
// the requeue=false -> delay-0 collapse. There is no fence/ErrStaleLease under
// this strategy (the open FOR UPDATE tx exclusively owns the row).
func TestSource_LockSettlement(t *testing.T) {
	t.Parallel()

	fixedNow := time.Date(2030, 3, 1, 0, 0, 0, 0, time.UTC)

	t.Run("Ack deletes the row", func(t *testing.T) {
		t.Parallel()
		fd := newFakeDialect()
		src, err := msginsql.NewPollingSource(openDB(t, fakeDriverName), "msgs", fd,
			msginsql.WithStrategy(msginsql.StrategyLockForUpdate))
		require.NoError(t, err)

		id := fd.insertRow("m-1", encodeHeadersT(t, "m-1"), []byte("p"))
		dels, err := src.Poll(t.Context(), 1)
		require.NoError(t, err)
		require.Len(t, dels, 1)

		require.NoError(t, dels[0].Ack(t.Context()))
		assert.False(t, fd.exists(id))
	})

	type nackCase struct {
		name    string
		requeue bool
		delay   time.Duration
		assert  func(t *testing.T, row fakeRow)
	}

	cases := []nackCase{
		{
			name:    "Nack requeues with delay, clearing the lock",
			requeue: true,
			delay:   time.Minute,
			assert: func(t *testing.T, row fakeRow) {
				assert.False(t, row.locked)
				assert.Equal(t, fixedNow.Add(time.Minute), row.visibleAfter)
			},
		},
		{
			name:    "requeue=false collapses to an immediate delayed requeue (delay 0)",
			requeue: false,
			delay:   time.Hour, // must be ignored
			assert: func(t *testing.T, row fakeRow) {
				assert.Equal(t, fixedNow, row.visibleAfter)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fd := newFakeDialect()
			fd.now = func() time.Time { return fixedNow }
			src, err := msginsql.NewPollingSource(openDB(t, fakeDriverName), "msgs", fd,
				msginsql.WithStrategy(msginsql.StrategyLockForUpdate))
			require.NoError(t, err)

			id := fd.insertRow("m-1", encodeHeadersT(t, "m-1"), []byte("p"))
			dels, err := src.Poll(t.Context(), 1)
			require.NoError(t, err)
			require.Len(t, dels, 1)

			require.NoError(t, dels[0].Nack(t.Context(), tc.requeue, tc.delay))
			tc.assert(t, fd.row(id))
		})
	}
}
