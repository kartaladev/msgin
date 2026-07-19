package harness

import (
	"context"
	"database/sql"
	"fmt"
	"sync/atomic"
	"testing"

	msgin "github.com/kartaladev/msgin"
	msginsql "github.com/kartaladev/msgin/adapter/database/sql"
	"github.com/stretchr/testify/require"
)

// RunQueueStore exercises the msginsql.QueueStore facade (Enqueue = INSERT via
// the paired Outbound; Claim = lease/claim via the paired Source, both over the
// SAME table) against kit.Lease on the already-open db. It certifies the facade
// is durable across the Enqueue/Claim/Ack lifecycle, redelivers a Nacked
// (requeue=true) claim, and gives two competing QueueStores over the same table
// disjoint claims — the properties a durable msgin.ChannelStore backing a
// msgin.QueueChannel must uphold (Spec 007, ADR 0018). Each subtest provisions
// its own fresh table (kit.Lease.EnsureSchema), so subtests never interfere.
func RunQueueStore(t *testing.T, kit TestKit, db *sql.DB) {
	t.Helper()
	var counter atomic.Int64
	fresh := func(ctx context.Context) string { return freshTable(t, ctx, kit, db, &counter, "msgin_qs") }

	newStore := func(t *testing.T, table string, opts ...msginsql.Option) *msginsql.QueueStore {
		t.Helper()
		s, err := msginsql.NewQueueStore(db, table, kit.Lease, opts...)
		require.NoError(t, err)
		return s
	}

	enqueue := func(t *testing.T, ctx context.Context, s *msginsql.QueueStore, id string, payload []byte) {
		t.Helper()
		msg := msgin.NewMessage[any](any(payload), msgin.NewHeaders(map[string]any{msgin.HeaderID: id}))
		require.NoError(t, s.Enqueue(ctx, msg))
	}

	t.Run("EnqueueClaimAckIsDurable", func(t *testing.T) {
		ctx := t.Context()
		table := fresh(ctx)
		s := newStore(t, table)

		enqueue(t, ctx, s, "durable-1", []byte(`"payload"`))
		require.Equal(t, 1, rowCount(t, ctx, kit, db, table), "the enqueued row is persisted")

		deliveries, err := s.Claim(ctx, 10)
		require.NoError(t, err)
		require.Len(t, deliveries, 1)
		require.Equal(t, "durable-1", deliveries[0].Msg.ID())

		require.NoError(t, deliveries[0].Ack(ctx))
		require.Equal(t, 0, rowCount(t, ctx, kit, db, table), "Ack deletes the row")

		again, err := s.Claim(ctx, 10)
		require.NoError(t, err)
		require.Empty(t, again, "a second Claim after Ack must be empty — the settle is durable")
	})

	t.Run("NackRequeueRedeliversOnNextClaim", func(t *testing.T) {
		ctx := t.Context()
		table := fresh(ctx)
		s := newStore(t, table)

		enqueue(t, ctx, s, "requeue-1", []byte(`"payload"`))

		deliveries, err := s.Claim(ctx, 10)
		require.NoError(t, err)
		require.Len(t, deliveries, 1)

		require.NoError(t, deliveries[0].Nack(ctx, true, 0))

		redelivered, err := s.Claim(ctx, 10)
		require.NoError(t, err)
		require.Len(t, redelivered, 1, "a Nack(requeue=true, 0)'d delivery must be redelivered")
		require.Equal(t, "requeue-1", redelivered[0].Msg.ID())

		require.NoError(t, redelivered[0].Ack(ctx))
		require.Equal(t, 0, rowCount(t, ctx, kit, db, table))
	})

	t.Run("CompetingQueueStoresGetDisjointClaims", func(t *testing.T) {
		ctx := t.Context()
		table := fresh(ctx)

		// Two independent QueueStores over the SAME table, distinct lease owners —
		// mirrors two competing-consumer workers.
		a := newStore(t, table, msginsql.WithLockedBy("worker-a"))
		b := newStore(t, table, msginsql.WithLockedBy("worker-b"))

		const n = 10
		for i := 0; i < n; i++ {
			enqueue(t, ctx, a, fmt.Sprintf("competing-%d", i), []byte(`"payload"`))
		}
		require.Equal(t, n, rowCount(t, ctx, kit, db, table))

		// A claims half the rows, leaving the rest visible; B then claims up to n
		// (the remainder). Both workers are thereby guaranteed a non-empty claim —
		// a full Claim(n) by A alone would starve B, which would not exercise
		// disjointness at all — while the assertion below still catches a bug that
		// lets the same row be claimed by both.
		claimedA, err := a.Claim(ctx, n/2)
		require.NoError(t, err)
		claimedB, err := b.Claim(ctx, n)
		require.NoError(t, err)

		require.NotEmpty(t, claimedA, "worker A must claim at least one row")
		require.NotEmpty(t, claimedB, "worker B must claim at least one row")
		require.Equal(t, n, len(claimedA)+len(claimedB), "every row is claimed exactly once across both workers")

		seen := make(map[string]bool, n)
		for _, d := range claimedA {
			require.False(t, seen[d.Msg.ID()], "no id claimed twice across workers")
			seen[d.Msg.ID()] = true
		}
		for _, d := range claimedB {
			require.False(t, seen[d.Msg.ID()], "no id claimed by both workers (disjoint claims)")
			seen[d.Msg.ID()] = true
		}
		require.Len(t, seen, n)

		for _, d := range claimedA {
			require.NoError(t, d.Ack(ctx))
		}
		for _, d := range claimedB {
			require.NoError(t, d.Ack(ctx))
		}
		require.Equal(t, 0, rowCount(t, ctx, kit, db, table))
	})

	t.Run("NativeReliabilityForwardsFromSource", func(t *testing.T) {
		table := fresh(t.Context())
		s := newStore(t, table)
		require.True(t, s.NativeRedelivery(), "the DB row redelivers on nack/lease-expiry")
		require.False(t, s.NativeDeadLetter(), "dead-lettering is the runtime's RetryPolicy")
	})

	t.Run("EmitsLiveValueIsFalse", func(t *testing.T) {
		table := fresh(t.Context())
		s := newStore(t, table)
		require.False(t, s.EmitsLiveValue(), "a wire store carries []byte, no live Go values")
	})

	t.Run("ReadyAndEnsureSchemaForwardToTheSharedTable", func(t *testing.T) {
		table := fmt.Sprintf("msgin_qs_missing_%d", counter.Add(1))
		s := newStore(t, table)

		require.ErrorIs(t, s.Ready(t.Context()), msginsql.ErrSchemaNotReady)
		require.NoError(t, s.EnsureSchema(t.Context()))
		require.NoError(t, s.Ready(t.Context()))
	})
}
