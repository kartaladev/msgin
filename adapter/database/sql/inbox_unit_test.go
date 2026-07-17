package sql_test

// inbox_unit_test.go drives InboxDeduper against the in-memory fakeDialect
// (fakedialect_test.go) — no real database (Plan 006 Task 2). It covers
// MarkProcessed's nil-tx/first/dup branches, Ready's
// ok/not-ready/no-unique-constraint branches, and Purge's N/0/
// ErrInvalidRetention branches.

import (
	"errors"
	"testing"
	"time"

	msginsql "github.com/kartaladev/msgin/adapter/database/sql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestInboxDeduper_WithFakeDialect proves the fake satisfies InboxDialect at
// construction (the exhaustive nil/invalid-table matrix stays covered against
// the built-ins in inbox_dedup_test.go, Task 1).
func TestInboxDeduper_WithFakeDialect(t *testing.T) {
	t.Parallel()

	d, err := msginsql.NewInboxDeduper(openDB(t, fakeDriverName), newFakeDialect())
	require.NoError(t, err)
	assert.NotNil(t, d)
}

// TestInboxDeduper_MarkProcessed covers the nil-tx guard and the
// first-time/duplicate dedup branches (ADR 0010 D10).
func TestInboxDeduper_MarkProcessed(t *testing.T) {
	t.Parallel()

	t.Run("nil tx is refused before any dialect call", func(t *testing.T) {
		t.Parallel()
		fd := newFakeDialect()
		d, err := msginsql.NewInboxDeduper(openDB(t, fakeDriverName), fd)
		require.NoError(t, err)

		already, err := d.MarkProcessed(t.Context(), nil, "m-1")
		require.ErrorIs(t, err, msginsql.ErrNilTx)
		assert.False(t, already)
		assert.Zero(t, fd.processedCount())
	})

	t.Run("first time records the id and reports already=false", func(t *testing.T) {
		t.Parallel()
		fd := newFakeDialect()
		d, err := msginsql.NewInboxDeduper(openDB(t, fakeDriverName), fd)
		require.NoError(t, err)

		already, err := d.MarkProcessed(t.Context(), beginNoOpTx(t), "m-1")
		require.NoError(t, err)
		assert.False(t, already)
		assert.Equal(t, 1, fd.processedCount())
	})

	t.Run("a redelivered (already-processed) id reports already=true", func(t *testing.T) {
		t.Parallel()
		fd := newFakeDialect()
		d, err := msginsql.NewInboxDeduper(openDB(t, fakeDriverName), fd)
		require.NoError(t, err)

		_, err = d.MarkProcessed(t.Context(), beginNoOpTx(t), "m-1")
		require.NoError(t, err)

		already, err := d.MarkProcessed(t.Context(), beginNoOpTx(t), "m-1")
		require.NoError(t, err)
		assert.True(t, already)
		assert.Equal(t, 1, fd.processedCount(), "a duplicate must not add a second entry")
	})
}

// TestInboxDeduper_Ready covers the boot fail-fast check's two probes: the
// table missing (ErrSchemaNotReady), the table present but lacking the
// msg_id unique constraint (ErrInboxNoUniqueConstraint), and both satisfied.
func TestInboxDeduper_Ready(t *testing.T) {
	t.Parallel()

	const table = "msgin_inbox"

	type testCase struct {
		name   string
		setup  func(fd *fakeDialect)
		assert func(t *testing.T, err error)
	}

	cases := []testCase{
		{
			name:  "table not initialized: ErrSchemaNotReady",
			setup: func(fd *fakeDialect) {},
			assert: func(t *testing.T, err error) {
				require.ErrorIs(t, err, msginsql.ErrSchemaNotReady)
			},
		},
		{
			name: "table exists but msg_id has no unique constraint: ErrInboxNoUniqueConstraint",
			setup: func(fd *fakeDialect) {
				fd.markReady(table)
				fd.setUniqueIndex(table, false)
			},
			assert: func(t *testing.T, err error) {
				require.ErrorIs(t, err, msginsql.ErrInboxNoUniqueConstraint)
			},
		},
		{
			name: "table exists with a unique constraint: ready",
			setup: func(fd *fakeDialect) {
				fd.markReady(table)
				fd.setUniqueIndex(table, true)
			},
			assert: func(t *testing.T, err error) {
				require.NoError(t, err)
			},
		},
		{
			name: "the unique-constraint probe itself erroring surfaces raw, not masked",
			setup: func(fd *fakeDialect) {
				fd.markReady(table)
				fd.uniqueIndexErr = errors.New("probe boom")
			},
			assert: func(t *testing.T, err error) {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "probe boom")
				assert.NotErrorIs(t, err, msginsql.ErrInboxNoUniqueConstraint,
					"a genuine probe error must not be reported as the no-unique-constraint case")
			},
		},
		{
			// The FIRST probe (SchemaExists) erroring, as opposed to reporting the
			// table simply absent: Ready must return that raw error unchanged, never
			// masking a real infrastructure failure as ErrSchemaNotReady. Moved here
			// from root's mysql-backed TestInboxDeduper_ReadyPassesThroughProbeError
			// (Plan 006 Task 5) since no built-in dialect remains in root to
			// reproduce it against a real closed pool.
			name: "the schema-exists probe itself erroring surfaces raw, not masked as not-ready",
			setup: func(fd *fakeDialect) {
				fd.schemaExistsErr = errors.New("probe boom")
			},
			assert: func(t *testing.T, err error) {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "probe boom")
				assert.NotErrorIs(t, err, msginsql.ErrSchemaNotReady,
					"a genuine probe error must not be reported as a not-ready schema")
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fd := newFakeDialect()
			tc.setup(fd)
			d, err := msginsql.NewInboxDeduper(openDB(t, fakeDriverName), fd, msginsql.WithInboxTable(table))
			require.NoError(t, err)

			tc.assert(t, d.Ready(t.Context()))
		})
	}
}

// TestInboxDeduper_Purge covers the retention guard (ErrInvalidRetention on
// a non-positive olderThan, before any dialect call) and the dialect-driven
// delete count (N removed, 0 when nothing is old enough).
func TestInboxDeduper_Purge(t *testing.T) {
	t.Parallel()

	fixedNow := time.Date(2032, 1, 1, 0, 0, 0, 0, time.UTC)

	type testCase struct {
		name      string
		olderThan time.Duration
		seed      func(fd *fakeDialect)
		assert    func(t *testing.T, n int64, err error)
	}

	cases := []testCase{
		{
			name:      "zero retention is refused before any dialect call",
			olderThan: 0,
			seed:      func(fd *fakeDialect) {},
			assert: func(t *testing.T, n int64, err error) {
				require.ErrorIs(t, err, msginsql.ErrInvalidRetention)
				assert.Zero(t, n)
			},
		},
		{
			name:      "negative retention is refused",
			olderThan: -time.Minute,
			seed:      func(fd *fakeDialect) {},
			assert: func(t *testing.T, n int64, err error) {
				require.ErrorIs(t, err, msginsql.ErrInvalidRetention)
				assert.Zero(t, n)
			},
		},
		{
			name:      "purges N rows older than the cutoff",
			olderThan: time.Hour,
			seed: func(fd *fakeDialect) {
				fd.now = func() time.Time { return fixedNow }
				fd.seedProcessed("old-1", fixedNow.Add(-2*time.Hour))
				fd.seedProcessed("old-2", fixedNow.Add(-3*time.Hour))
				fd.seedProcessed("fresh-1", fixedNow.Add(-time.Minute))
			},
			assert: func(t *testing.T, n int64, err error) {
				require.NoError(t, err)
				assert.EqualValues(t, 2, n)
			},
		},
		{
			name:      "purges 0 when nothing is old enough",
			olderThan: time.Hour,
			seed: func(fd *fakeDialect) {
				fd.now = func() time.Time { return fixedNow }
				fd.seedProcessed("fresh-1", fixedNow.Add(-time.Minute))
			},
			assert: func(t *testing.T, n int64, err error) {
				require.NoError(t, err)
				assert.Zero(t, n)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fd := newFakeDialect()
			tc.seed(fd)
			d, err := msginsql.NewInboxDeduper(openDB(t, fakeDriverName), fd)
			require.NoError(t, err)

			n, err := d.Purge(t.Context(), tc.olderThan)
			tc.assert(t, n, err)
		})
	}
}
