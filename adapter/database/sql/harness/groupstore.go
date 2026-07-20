package harness

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	msgin "github.com/kartaladev/msgin"
	msginsql "github.com/kartaladev/msgin/adapter/database/sql"
	"github.com/stretchr/testify/require"
)

// RunGroupStore certifies the msginsql.GroupStore facade + kit.Group
// (GroupDialect) against a real database on the already-open db: the full
// msgin.MessageGroupStore contract (idempotent Add, growing snapshot, exclusive
// ClaimGroup, fenced Settle/Abandon, late-member survival with created_at reset,
// crashed-lease Expired) PLUS the cross-connection and crash-recovery invariants
// a durable multi-process aggregation store must uphold that the in-memory
// reference store cannot exhibit (ADR 0021 §6): H1 concurrent-first-add
// completion detection, the two-connection claim race, H2 stale-epoch recovery
// (re-absorbed members, no orphans, fenced stale settle), H-A crash-mid-release
// re-emitting to the OUTPUT channel via a real *msgin.Aggregator reaper, and H-B
// deadlock-freedom under a high-concurrency same-key add/settle loop.
//
// Cross-connection races drive concurrent goroutines over the SAME db pool: a
// *sql.DB hands each concurrent operation a distinct physical connection, so the
// group-row FOR UPDATE lock (or SQLite's BEGIN IMMEDIATE write lock) is genuinely
// exercised across connections — the transaction-scoped lock is what serializes
// them, identical to two processes. Each subtest provisions its own fresh table.
func RunGroupStore(t *testing.T, kit TestKit, db *sql.DB) {
	t.Helper()
	var counter atomic.Int64

	fresh := func(ctx context.Context) string {
		name := fmt.Sprintf("msgin_grp_%d", counter.Add(1))
		require.NoError(t, kit.Group.EnsureGroupSchema(ctx, db, name))
		return name
	}
	newStore := func(t *testing.T, table string, opts ...msginsql.GroupStoreOption) *msginsql.GroupStore {
		t.Helper()
		s, err := msginsql.NewGroupStore(db, table, kit.Group, opts...)
		require.NoError(t, err)
		return s
	}
	mkMsg := func(id string, seq int) msgin.Message[any] {
		return msgin.NewMessage[any]([]byte(`"payload"`),
			msgin.NewHeaders(map[string]any{msgin.HeaderID: id, msgin.HeaderSequenceNumber: seq}))
	}
	add := func(t *testing.T, ctx context.Context, s *msginsql.GroupStore, key, id string, seq int) (msgin.MessageGroup, error) {
		t.Helper()
		return s.Add(ctx, key, mkMsg(id, seq))
	}
	memberCount := func(t *testing.T, ctx context.Context, table, key string) int {
		t.Helper()
		var n int
		q := fmt.Sprintf("SELECT count(*) FROM %s WHERE group_key = %s", kit.Quote(table+"_member"), kit.Placeholder(1))
		require.NoError(t, db.QueryRowContext(ctx, q, key).Scan(&n))
		return n
	}

	t.Run("ReadyAndEnsureGroupSchema", func(t *testing.T) {
		ctx := t.Context()
		table := fmt.Sprintf("msgin_grp_missing_%d", counter.Add(1))
		s := newStore(t, table)
		require.ErrorIs(t, s.Ready(ctx), msginsql.ErrSchemaNotReady)
		require.NoError(t, s.EnsureSchema(ctx))
		require.NoError(t, s.Ready(ctx))
	})

	t.Run("GroupDDLValidatesIdentifier", func(t *testing.T) {
		_, err := kit.GroupDDL("bad name;drop")
		require.ErrorIs(t, err, msginsql.ErrInvalidTableName)
		ddl, err := kit.GroupDDL("msgin_grp_ddl")
		require.NoError(t, err)
		require.NotEmpty(t, ddl)
	})

	t.Run("EmptyMsgIDRejected", func(t *testing.T) {
		ctx := t.Context()
		table := fresh(ctx)
		s := newStore(t, table)
		msg := msgin.NewMessage[any]([]byte(`"p"`), msgin.NewHeaders(nil)) // no HeaderID
		_, err := s.Add(ctx, "k", msg)
		require.ErrorIs(t, err, msginsql.ErrMissingMsgID)
	})

	t.Run("IdempotentAddAndGrowingSnapshot", func(t *testing.T) {
		ctx := t.Context()
		table := fresh(ctx)
		s := newStore(t, table)

		g, err := add(t, ctx, s, "k", "a", 0)
		require.NoError(t, err)
		require.Len(t, g.Messages(), 1)

		g, err = add(t, ctx, s, "k", "a", 0) // same id → idempotent no-op
		require.NoError(t, err)
		require.Len(t, g.Messages(), 1, "re-adding the same id must not double-count")

		g, err = add(t, ctx, s, "k", "b", 1)
		require.NoError(t, err)
		require.Len(t, g.Messages(), 2)
		require.Equal(t, "a", g.Messages()[0].ID(), "members ordered by seq then msg_id")
		require.Equal(t, "b", g.Messages()[1].ID())
		require.Equal(t, 2, memberCount(t, ctx, table, "k"), "two distinct members persisted (the duplicate add was a no-op)")
		require.False(t, g.CreatedAt().IsZero())
	})

	t.Run("ClaimReturnsMembersAndEpoch_SecondClaimNil", func(t *testing.T) {
		ctx := t.Context()
		table := fresh(ctx)
		s := newStore(t, table)
		_, err := add(t, ctx, s, "k", "a", 0)
		require.NoError(t, err)
		_, err = add(t, ctx, s, "k", "b", 1)
		require.NoError(t, err)

		claim, err := s.ClaimGroup(ctx, "k")
		require.NoError(t, err)
		require.NotNil(t, claim)
		require.Len(t, claim.Messages(), 2)
		require.Positive(t, claim.Epoch())

		second, err := s.ClaimGroup(ctx, "k")
		require.NoError(t, err)
		require.Nil(t, second, "a second claim on an actively-leased group is held → (nil,nil)")
	})

	t.Run("SettleDeletesClaimedSet_LateMemberSurvives_CreatedAtReset", func(t *testing.T) {
		ctx := t.Context()
		table := fresh(ctx)
		s := newStore(t, table)
		_, err := add(t, ctx, s, "k", "a", 0)
		require.NoError(t, err)

		claim, err := s.ClaimGroup(ctx, "k")
		require.NoError(t, err)
		require.NotNil(t, claim)
		original := claim.CreatedAt()

		// A late member arrives during the lease; Add returns only the live set.
		live, err := add(t, ctx, s, "k", "b", 1)
		require.NoError(t, err)
		require.Len(t, live.Messages(), 1)
		require.Equal(t, "b", live.Messages()[0].ID())

		time.Sleep(10 * time.Millisecond) // ensure the reset created_at is measurably later
		require.NoError(t, s.SettleGroup(ctx, claim))

		// The claimed member is gone; the late member survives as a fresh group.
		require.Equal(t, 1, memberCount(t, ctx, table, "k"), "only the claimed member 'a' was deleted")
		residual, err := s.ClaimGroup(ctx, "k")
		require.NoError(t, err)
		require.NotNil(t, residual)
		require.Len(t, residual.Messages(), 1)
		require.Equal(t, "b", residual.Messages()[0].ID())
		require.True(t, residual.CreatedAt().After(original), "residual created_at is reset to a fresh clock (M2)")
		require.NoError(t, s.SettleGroup(ctx, residual))
		require.Equal(t, 0, memberCount(t, ctx, table, "k"))
	})

	t.Run("AbandonRestoresClaimedSetToLive", func(t *testing.T) {
		ctx := t.Context()
		table := fresh(ctx)
		s := newStore(t, table)
		_, err := add(t, ctx, s, "k", "a", 0)
		require.NoError(t, err)

		claim, err := s.ClaimGroup(ctx, "k")
		require.NoError(t, err)
		require.NotNil(t, claim)

		require.NoError(t, s.AbandonGroup(ctx, claim))

		g, err := add(t, ctx, s, "k", "b", 1)
		require.NoError(t, err)
		require.Len(t, g.Messages(), 2, "abandoned members are live again, plus the new one")
	})

	t.Run("ExpiredCrashedLeaseRegardlessOfAge_ExcludesLiveLeased", func(t *testing.T) {
		ctx := t.Context()
		table := fresh(ctx)
		const ttl = 300 * time.Millisecond
		s := newStore(t, table, msginsql.WithGroupLeaseTTL(ttl))

		// A crashed holder: claim "crashed" and never settle.
		_, err := add(t, ctx, s, "crashed", "a", 0)
		require.NoError(t, err)
		crashed, err := s.ClaimGroup(ctx, "crashed")
		require.NoError(t, err)
		require.NotNil(t, crashed)

		time.Sleep(ttl + 200*time.Millisecond) // let the lease age out

		// A live holder: claimed just now, lease not expired.
		_, err = add(t, ctx, s, "live", "b", 0)
		require.NoError(t, err)
		liveClaim, err := s.ClaimGroup(ctx, "live")
		require.NoError(t, err)
		require.NotNil(t, liveClaim)

		exp, err := s.Expired(ctx, time.Time{}) // zero before → crash-recovery sweep only
		require.NoError(t, err)
		keys := map[string]bool{}
		for _, g := range exp {
			keys[g.Key()] = true
		}
		require.True(t, keys["crashed"], "a crashed lease is returned regardless of age")
		require.False(t, keys["live"], "an actively-leased group is excluded")
	})

	t.Run("ExpiredAgeOldUnleasedWithNonZeroBefore", func(t *testing.T) {
		ctx := t.Context()
		table := fresh(ctx)
		s := newStore(t, table)

		old, err := add(t, ctx, s, "old", "a", 0)
		require.NoError(t, err)
		time.Sleep(10 * time.Millisecond)
		newG, err := add(t, ctx, s, "new", "b", 0)
		require.NoError(t, err)

		// Use the DB-clock-derived created_at of "new" as the cutoff so there is
		// no app<->DB skew: old < cutoff (included), new == cutoff (excluded).
		require.True(t, newG.CreatedAt().After(old.CreatedAt()))
		exp, err := s.Expired(ctx, newG.CreatedAt())
		require.NoError(t, err)
		keys := map[string]bool{}
		for _, g := range exp {
			keys[g.Key()] = true
		}
		require.True(t, keys["old"], "an unleased group older than the cutoff is returned")
		require.False(t, keys["new"], "an unleased group at/after the cutoff is excluded")
	})

	t.Run("ConcurrentFirstAddCompletionDetection_H1", func(t *testing.T) {
		ctx := t.Context()
		table := fresh(ctx)
		s1 := newStore(t, table)
		s2 := newStore(t, table)

		const rounds = 40
		for r := 0; r < rounds; r++ {
			key := fmt.Sprintf("h1-%d", r)
			var (
				wg    sync.WaitGroup
				start = make(chan struct{})
				sizes [2]int
				errs  [2]error
			)
			run := func(i int, s *msginsql.GroupStore, id string, seq int) {
				defer wg.Done()
				<-start
				g, err := s.Add(ctx, key, mkMsg(id, seq))
				if err != nil {
					errs[i] = err
					return
				}
				sizes[i] = len(g.Messages())
			}
			wg.Add(2)
			go run(0, s1, "m0", 0)
			go run(1, s2, "m1", 1)
			close(start)
			wg.Wait()

			require.NoError(t, errs[0])
			require.NoError(t, errs[1])
			got2 := 0
			for _, sz := range sizes {
				if sz == 2 {
					got2++
				}
			}
			require.Equalf(t, 1, got2,
				"round %d: exactly one concurrent add must observe the complete size-2 group (the group-row lock serialized them); sizes=%v", r, sizes)
		}
	})

	t.Run("TwoConnectionClaimRace", func(t *testing.T) {
		ctx := t.Context()
		table := fresh(ctx)
		s := newStore(t, table)
		_, err := add(t, ctx, s, "k", "a", 0)
		require.NoError(t, err)
		_, err = add(t, ctx, s, "k", "b", 1)
		require.NoError(t, err)

		s1 := newStore(t, table)
		s2 := newStore(t, table)
		var (
			wg     sync.WaitGroup
			start  = make(chan struct{})
			claims [2]msgin.MessageGroupClaim
			errs   [2]error
		)
		run := func(i int, store *msginsql.GroupStore) {
			defer wg.Done()
			<-start
			claims[i], errs[i] = store.ClaimGroup(ctx, "k")
		}
		wg.Add(2)
		go run(0, s1)
		go run(1, s2)
		close(start)
		wg.Wait()
		require.NoError(t, errs[0])
		require.NoError(t, errs[1])

		nonNil := 0
		for _, c := range claims {
			if c != nil {
				nonNil++
			}
		}
		require.Equal(t, 1, nonNil, "exactly one of two concurrent claims on the same complete group wins")
	})

	t.Run("StaleEpochCrashRecovery_H2", func(t *testing.T) {
		ctx := t.Context()
		table := fresh(ctx)
		const ttl = 300 * time.Millisecond

		// Drive the dialect directly so both the re-absorbed member set AND the
		// fenced stale-settle applied flag can be asserted with known lockedBy ids.
		addRaw := func(key, id string, seq int64) {
			headers, err := msginsql.EncodeHeaders(
				msgin.NewHeaders(map[string]any{msgin.HeaderID: id, msgin.HeaderSequenceNumber: int(seq)}))
			require.NoError(t, err)
			_, err = kit.Group.AddMember(ctx, db, table, key, id, seq, headers, []byte(`"p"`))
			require.NoError(t, err)
		}
		addRaw("k", "a", 0)
		addRaw("k", "b", 1)

		// Holder A claims and then "crashes" (never settles).
		claimA, err := kit.Group.ClaimGroup(ctx, db, table, "k", "holderA", ttl)
		require.NoError(t, err)
		require.NotNil(t, claimA)
		require.Len(t, claimA.Members, 2)

		time.Sleep(ttl + 200*time.Millisecond) // A's lease ages out

		// Holder B re-claims: the re-claim RE-ABSORBS A's members across the epoch
		// bump (all 2), never zero (H2).
		claimB, err := kit.Group.ClaimGroup(ctx, db, table, "k", "holderB", ttl)
		require.NoError(t, err)
		require.NotNil(t, claimB)
		require.Len(t, claimB.Members, 2, "re-claim must re-absorb ALL the dead holder's members")
		require.Greater(t, claimB.Epoch, claimA.Epoch)

		// A stale-epoch settle by the crashed holder A is a fenced no-op.
		appliedStale, err := kit.Group.SettleGroup(ctx, db, table, "k", "holderA", claimA.Epoch)
		require.NoError(t, err)
		require.False(t, appliedStale, "a stale-epoch settle must not apply (fence miss)")
		require.Equal(t, 2, memberCount(t, ctx, table, "k"), "the stale settle must delete nothing")

		// B's real settle applies and clears the group with no orphans.
		appliedB, err := kit.Group.SettleGroup(ctx, db, table, "k", "holderB", claimB.Epoch)
		require.NoError(t, err)
		require.True(t, appliedB)
		require.Equal(t, 0, memberCount(t, ctx, table, "k"), "no orphan member rows remain")
	})

	t.Run("CrashMidReleaseReEmitsToOutput_HA", func(t *testing.T) {
		ctx := t.Context()
		table := fresh(ctx)
		const ttl = 300 * time.Millisecond

		crashStore := newStore(t, table, msginsql.WithGroupLeaseTTL(ttl), msginsql.WithGroupLockedBy("crasher"))
		_, err := add(t, ctx, crashStore, "g", "m0", 0)
		require.NoError(t, err)
		_, err = add(t, ctx, crashStore, "g", "m1", 1)
		require.NoError(t, err)

		// Simulate a crash mid-release: claim the complete group and drop the
		// claim WITHOUT settling.
		crashed, err := crashStore.ClaimGroup(ctx, "g")
		require.NoError(t, err)
		require.NotNil(t, crashed)
		require.Len(t, crashed.Messages(), 2)

		// A second Aggregator over the same store recovers via its reaper.
		out := msgin.NewDirectChannel()
		var (
			mu       sync.Mutex
			outCount int
		)
		require.NoError(t, out.Subscribe(msgin.HandlerFunc(func(_ context.Context, _ msgin.Message[any]) error {
			mu.Lock()
			outCount++
			mu.Unlock()
			return nil
		})))

		recoverStore := newStore(t, table, msginsql.WithGroupLeaseTTL(ttl), msginsql.WithGroupLockedBy("recoverer"))
		agg, err := msgin.NewAggregator[[]byte, []byte](recoverStore,
			func(_ context.Context, group []msgin.Message[[]byte]) (msgin.Message[[]byte], error) {
				return msgin.New([]byte("aggregated")), nil
			},
			msgin.WithOutputChannel(out),
			msgin.WithCompletionSize(2),
		)
		require.NoError(t, err)

		runCtx, cancel := context.WithCancel(ctx)
		errCh := make(chan error, 1)
		go func() { errCh <- agg.Run(runCtx) }()

		require.Eventually(t, func() bool {
			mu.Lock()
			defer mu.Unlock()
			return outCount >= 1
		}, 15*time.Second, 50*time.Millisecond, "the crashed complete group must be re-emitted to OUTPUT by the reaper")

		// Give the reaper a moment to prove it does NOT double-emit, then stop it.
		time.Sleep(2 * ttl)
		cancel()
		<-errCh

		mu.Lock()
		require.Equal(t, 1, outCount, "the recovered aggregate reaches output exactly once (no double emit, no loss)")
		mu.Unlock()
		require.Equal(t, 0, memberCount(t, ctx, table, "g"), "no orphan member rows remain after recovery+settle")
	})

	t.Run("NoDeadlockUnderConcurrentAddSettle_HB", func(t *testing.T) {
		ctx := t.Context()
		table := fresh(ctx)

		const (
			workers = 4
			iters   = 40
			hotKeys = 5
		)
		var wg sync.WaitGroup
		errCh := make(chan error, workers*iters*3)
		for w := 0; w < workers; w++ {
			wg.Add(1)
			go func(w int) {
				defer wg.Done()
				store, err := msginsql.NewGroupStore(db, table, kit.Group)
				if err != nil {
					errCh <- err
					return
				}
				for i := 0; i < iters; i++ {
					key := fmt.Sprintf("hb-%d", i%hotKeys) // few hot keys → real group->member contention
					id := fmt.Sprintf("w%d-i%d", w, i)
					if _, err := store.Add(ctx, key, mkMsg(id, i)); err != nil {
						errCh <- err
						continue
					}
					claim, err := store.ClaimGroup(ctx, key)
					if err != nil {
						errCh <- err
						continue
					}
					if claim != nil {
						if err := store.SettleGroup(ctx, claim); err != nil {
							errCh <- err
						}
					}
				}
			}(w)
		}
		wg.Wait()
		close(errCh)
		for err := range errCh {
			require.NoError(t, err, "the uniform group->member lock order must keep the concurrent add/settle loop deadlock-free")
		}
	})
}
