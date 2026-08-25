package harness

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	msgin "github.com/kartaladev/msgin"
	msginsql "github.com/kartaladev/msgin/adapter/database/sql"
	"github.com/kartaladev/msgin/channel"
	"github.com/kartaladev/msgin/routing"
	"github.com/stretchr/testify/require"
)

// groupMemberCap is the member cap every harness group case that drives
// kit.Group.AddMember directly runs under (Spec 017 §3.6 / Plan 031 Task 5):
// small enough for Spec 017 §6 AC-6 (no test grows a group past 16 members),
// large enough for the largest member count any harness case adds. It is
// UNEXPORTED on purpose — an exported harness function with an int parameter
// would be a conformance key the root class gate can discover but, being in a
// leaf module, cannot import to satisfy; a TestKit field would be a
// public-surface change to a leaf module. An unexported package constant is
// neither.
//
// The member-cap cases (Plan 031 Task 7) pass it BOTH ways: to
// kit.Group.AddMember directly, and to msginsql.NewGroupStore via
// msginsql.WithMaxGroupMembers, so the same bound is asserted at the dialect
// and through the shipped store. Every OTHER case that goes through
// NewGroupStore leaves the option unset and runs under the store's own 65,536
// default, which no harness case approaches.
const groupMemberCap = 4

// grownMemberCount is how far the CAP-LOWERED cases below grow a group before
// pointing a groupMemberCap-sized store at the same table — the rolling-deploy
// / two-instances-with-different-caps topology CLAUDE.md requires every
// component to reason about, expressed entirely through the public API by
// building two msginsql.GroupStore values on one table.
//
// Its value is load-bearing twice over, and both bounds are tight:
//
//   - It must EXCEED groupMemberCap+1, the fetch bound AddMember used to apply.
//     Every pre-existing member-cap case fills to EXACTLY the cap, which that
//     bound never truncated, so none of them could observe the truncation at
//     all (review finding R-1). At 10 against a cap of 4 the old bound returned
//     5 of 10 live rows — a mutant that restores it is arithmetically capable
//     of failing these cases, which the finding's own first attempt was not.
//   - It must stay at or under 16, so Spec 017 §6 AC-6 ("no test grows a group
//     past 16 members") still holds.
const grownMemberCount = 10

// directAddLeaseTTL is the leaseTTL every harness case that drives
// kit.Group.AddMember DIRECTLY passes (ADR 0033 D-AU / Spec 017 §3.6a.1).
//
// It only ever affects the over-cap rejection's Permanent/transient split — the
// dialect compares the group row's locked_at against now() - leaseTTL on the DB
// server clock — so it is deliberately LONG: none of the direct-add cases holds
// a lease at all, and a long TTL keeps them insensitive to how slowly a
// container runs. Cases that need the EXPIRED-lease arm set their own short TTL
// through msginsql.WithGroupLeaseTTL and drive the STORE, so the store and the
// dialect cannot disagree about which value was used.
//
// Unexported for the same reason groupMemberCap is.
const directAddLeaseTTL = 5 * time.Minute

// mustSubscribe registers h on ch and fails the test if Subscribe errors. Since
// ADR 0028 Subscribe returns (Subscription, error); these call sites do not need
// the handle, and this keeps the original require.NoError assertion intact.
func mustSubscribe(t *testing.T, ch msgin.SubscribableChannel, h msgin.MessageHandler) {
	t.Helper()
	_, err := ch.Subscribe(h)
	require.NoError(t, err)
}

// RunGroupStore certifies the msginsql.GroupStore facade + kit.Group
// (GroupDialect) against a real database on the already-open db: the full
// msgin.MessageGroupStore contract (idempotent Add, growing snapshot, exclusive
// ClaimGroup, fenced Settle/Abandon, late-member survival with created_at reset,
// crashed-lease Expired) PLUS the cross-connection and crash-recovery invariants
// a durable multi-process aggregation store must uphold that the in-memory
// reference store cannot exhibit (ADR 0021 §6): H1 concurrent-first-add
// completion detection, the two-connection claim race, H2 stale-epoch recovery
// (re-absorbed members, no orphans, fenced stale settle), H-A crash-mid-release
// re-emitting to the OUTPUT channel via a real *routing.Aggregator reaper, and H-B
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
			msgin.NewHeaders(map[string]any{msgin.HeaderMessageID: id, msgin.HeaderSequenceNumber: seq}))
	}
	add := func(t *testing.T, ctx context.Context, s *msginsql.GroupStore, key, id string, seq int) (msgin.MessageGroup, error) {
		t.Helper()
		return s.Add(ctx, key, mkMsg(id, seq))
	}
	memberCount := func(t *testing.T, ctx context.Context, table, key string) int {
		t.Helper()
		var n int
		require.NoError(t, db.QueryRowContext(ctx, memberCountSQL(kit, table), key).Scan(&n))
		return n
	}
	// overflowRender builds Spec 017 AC-2c's EXACT member-cap render for this
	// engine: the msgin.ErrOverflowDropped sentinel text, the site
	// "msgin/sql/<engine>: AddMember" (which names the ENGINE, never the store
	// — the error is minted inside the dialect, which cannot know whether it
	// was reached through GroupStore.Add or through a direct
	// kit.Group.AddMember caller; ADR 0033 D-AG/D-AE, audit NEW-5), the key,
	// the count AT THE MOMENT OF THE CHECK (cap+1 — the dialects check AFTER
	// the member upsert) and the limit. permanent adds the shipped
	// permanentError prefix, which D-AM attaches iff the group is NOT leased.
	//
	// The engine token is the DIALECT IMPLEMENTATION's package name, NOT
	// kit.Name: kit.Name is a display label the runner is free to override,
	// and the shipped MariaDB kit does exactly that — it runs the mysql
	// dialect under Name "mariadb", so the site reads "msgin/sql/mysql" while
	// kit.Name reads "mariadb". Deriving it from the dialect's own package is
	// what "the site names the engine that MINTED the error" actually means.
	// A dialect outside this module prefixes its own module path instead of
	// "msgin/sql/", and must reconcile that before running this suite.
	engine := DialectEngine(kit.Group)
	overflowRender := func(key string, n, limit int, permanent bool) string {
		s := fmt.Sprintf("%s: msgin/sql/%s: AddMember: group %q holds %d members, limit %d",
			msgin.ErrOverflowDropped.Error(), engine, key, n, limit)
		if permanent {
			return "msgin: permanent: " + s
		}
		return s
	}
	// fillToCap adds exactly groupMemberCap distinct live members to key
	// through s, asserting each one is admitted — the shared fixture of every
	// member-cap case below, and the assertion that the bound admits everything
	// UP TO the cap (Plan 031 Task 6 B6-2).
	fillToCap := func(t *testing.T, ctx context.Context, s *msginsql.GroupStore, key string) {
		t.Helper()
		for i := 0; i < groupMemberCap; i++ {
			g, err := add(t, ctx, s, key, fmt.Sprintf("m%d", i), i)
			require.NoErrorf(t, err, "member %d of %d must be admitted", i+1, groupMemberCap)
			require.Len(t, g.Messages(), i+1)
		}
	}
	// grownMemberID is the id of the i-th member the cap-lowered fixture stores.
	// Zero-padded so the lexical msg_id tiebreak in the members' ORDER BY can
	// never reorder them relative to their seq.
	grownMemberID := func(i int) string { return fmt.Sprintf("g%02d", i) }
	// growThenLowerTheCap stores grownMemberCount live members under a HIGH cap
	// and returns a SECOND store on the SAME table running at groupMemberCap —
	// a group holding strictly more rows than the cap now in force. That is the
	// state a rolling deploy that lowers WithMaxGroupMembers reaches, and the
	// state two concurrently-deployed instances configured differently are
	// permanently in; nothing about it is exotic, and no pre-existing case
	// reaches it (review findings R-1 and R-11).
	growThenLowerTheCap := func(t *testing.T, ctx context.Context, table, key string) *msginsql.GroupStore {
		t.Helper()
		grown := newStore(t, table, msginsql.WithMaxGroupMembers(grownMemberCount))
		for i := 0; i < grownMemberCount; i++ {
			g, err := add(t, ctx, grown, key, grownMemberID(i), i)
			require.NoErrorf(t, err, "member %d of %d must be admitted under the HIGHER cap", i+1, grownMemberCount)
			require.Len(t, g.Messages(), i+1)
		}
		require.Equal(t, grownMemberCount, memberCount(t, ctx, table, key),
			"the fixture must actually reach the over-cap-with-more-rows-than-cap state")
		return newStore(t, table, msginsql.WithMaxGroupMembers(groupMemberCap))
	}
	// memberIDs renders a group snapshot's member ids in order, for assertions
	// that must name WHICH members are missing rather than only how many.
	memberIDs := func(g msgin.MessageGroup) []string {
		out := make([]string, 0, len(g.Messages()))
		for _, m := range g.Messages() {
			out = append(out, m.ID())
		}
		return out
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
		msg := msgin.NewMessage[any]([]byte(`"p"`), msgin.NewHeaders(nil)) // no HeaderMessageID
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

	// ---- the member cap (Spec 017 §6 AC-4/AC-4b/AC-4c/AC-5; ADR 0033
	// D-AG/D-AM/D-AN/D-AP/D-AS). A dialect's in-transaction bound is only
	// observable against a real engine, so these four cases are what make Plan
	// 031 Task 6's branches real. They assert BEHAVIOR, never statement order:
	// the three dialects enforce at three different points (Spec 017 §3.6.1).

	t.Run("MemberCapRejectsAndRollsBack", func(t *testing.T) {
		ctx := t.Context()
		table := fresh(ctx)
		s := newStore(t, table, msginsql.WithMaxGroupMembers(groupMemberCap))
		fillToCap(t, ctx, s, "k")

		// AC-4.1/.5/.6 + AC-4c: the cap+1-th LIVE member is refused with the
		// shared sentinel, PERMANENTLY (the group row's locked_by IS NULL, so
		// nothing will drain it — D-AM), rendered exactly as AC-2c pins it, and
		// both the sentinel and the Permanent marker survive GroupStore.Add's
		// classifyQueryErr pass-through with the table present.
		over, err := add(t, ctx, s, "k", "overflow", groupMemberCap)
		require.ErrorIs(t, err, msgin.ErrOverflowDropped)
		require.True(t, msgin.IsPermanent(err), "an UNLEASED over-cap rejection is permanent (D-AM)")
		require.NotErrorIs(t, err, msginsql.ErrSchemaNotReady,
			"classifyQueryErr must pass the rejection through untouched while the table exists (AC-4c)")
		require.Equal(t, overflowRender("k", groupMemberCap+1, groupMemberCap, true), err.Error())

		// AC-4.4: the post-rollback LIVE snapshot rides out WITH the error, the
		// refused member filtered out of it (D-AN) — and classifyQueryErr's
		// call site did not discard it.
		require.NotNil(t, over)
		require.Len(t, over.Messages(), groupMemberCap, "the rejection carries the live set, not an empty one")
		for _, m := range over.Messages() {
			require.NotEqual(t, "overflow", m.ID(), "the refused member must not appear in its own rejection snapshot")
		}

		// AC-4.2: the rollback ASSERTED, not assumed. Without this half,
		// enforcement (C) is indistinguishable from enforcement (A).
		require.Equal(t, groupMemberCap, memberCount(t, ctx, table, "k"), "the over-cap transaction committed nothing")

		// AC-4.3: an idempotent re-add of an EXISTING id while the group sits
		// at exactly the cap is a no-op returning the unchanged snapshot, not
		// an overflow — which is why the check runs AFTER the member upsert.
		readd, err := add(t, ctx, s, "k", "m0", 0)
		require.NoError(t, err, "re-adding an existing id at exactly the cap is a no-op, not an overflow")
		require.Len(t, readd.Messages(), groupMemberCap, "the unchanged snapshot")
		require.Equal(t, groupMemberCap, memberCount(t, ctx, table, "k"))

		// AC-4.2, second half: the group a later claimer sees is exactly the cap.
		claim, err := s.ClaimGroup(ctx, "k")
		require.NoError(t, err)
		require.NotNil(t, claim)
		require.Len(t, claim.Messages(), groupMemberCap)
	})

	t.Run("MemberCapCountsClaimedMembers_ClaimSetIsComplete", func(t *testing.T) {
		ctx := t.Context()
		table := fresh(ctx)
		s := newStore(t, table, msginsql.WithMaxGroupMembers(groupMemberCap))
		fillToCap(t, ctx, s, "k")

		// D-AS: ClaimGroup passes limit = 0 to the dialect's member SELECT, so
		// the claimed set is COMPLETE. A LIMIT leaking into that call site
		// releases a partial aggregate — silent data corruption no other case
		// notices, because every other group here is smaller than the cap.
		claim, err := s.ClaimGroup(ctx, "k")
		require.NoError(t, err)
		require.NotNil(t, claim)
		require.Len(t, claim.Messages(), groupMemberCap, "ClaimGroup must return EVERY claimed member (limit = 0, D-AS)")

		// AC-4.7: the claim stamped its epoch on every live member, so the LIVE
		// count is now 0 — and the add is STILL refused, because the bound
		// counts the durable table (live + claimed). This is the case that
		// proves the table is bounded at all: a live-only count would readmit
		// groupMemberCap more rows per claim cycle, forever.
		over, err := add(t, ctx, s, "k", "overflow", groupMemberCap)
		require.ErrorIs(t, err, msgin.ErrOverflowDropped, "the CLAIMED set still counts against the cap")
		// ...and because a LIVE claim is in flight — the store's default 5m
		// lease was stamped moments ago, so locked_at is far newer than
		// now() - leaseTTL — the rejection is TRANSIENT: the in-flight release
		// will drain the group, so dead-lettering here would discard healthy
		// traffic in a routine claim window (D-AM). This is the LIVE half of
		// D-AU's split; MemberCapExpiredLeaseIsPermanent below is the other.
		require.False(t, msgin.IsPermanent(err), "a LIVE-LEASED over-cap rejection is transient (D-AM/D-AU)")
		require.Equal(t, overflowRender("k", groupMemberCap+1, groupMemberCap, false), err.Error())
		require.NotNil(t, over)
		require.Empty(t, over.Messages(), "the post-rollback LIVE set is empty here: every other member is claimed")
		require.Equal(t, groupMemberCap, memberCount(t, ctx, table, "k"), "nothing was committed")
	})

	// D-AU / Spec 017 §3.6a.1 — whole-branch review finding R-7, and the case
	// the shipped code could not express at all: AddMember had no leaseTTL, so
	// its discriminator was `locked_by IS NOT NULL`, which is TRUE of a
	// CRASHED holder's stranded stamp forever. Every over-cap add for that key
	// was then classified transient, which under the shipped zero-value
	// msgin.RetryPolicy is an unlogged, zero-delay, infinite Nack loop — the
	// exact B-1 hot spin D-AM exists to prevent, reintroduced by D-AM's own
	// discriminator.
	//
	// 🔴 THE MUTANT THIS CASE KILLS: revert AddMember's predicate to
	// `locked_by IS NOT NULL` (dropping the locked_at comparison) and this
	// subtest fails on IsPermanent while EVERY other member-cap case stays
	// green — including MemberCapCountsClaimedMembers above, whose lease is
	// genuinely live. The two together assert the SPLIT, not one side of it.
	t.Run("MemberCapExpiredLeaseIsPermanent", func(t *testing.T) {
		ctx := t.Context()
		table := fresh(ctx)
		// A short TTL, threaded to the dialect by GroupStore.Add itself — so
		// the value the classification uses is provably the CONFIGURED one and
		// not a dialect default.
		const ttl = 300 * time.Millisecond
		s := newStore(t, table,
			msginsql.WithMaxGroupMembers(groupMemberCap), msginsql.WithGroupLeaseTTL(ttl))
		fillToCap(t, ctx, s, "k")

		claim, err := s.ClaimGroup(ctx, "k")
		require.NoError(t, err)
		require.NotNil(t, claim)
		require.Len(t, claim.Messages(), groupMemberCap)

		// The holder "crashes": it never settles and never abandons, so
		// locked_by stays stamped and the group cannot drain itself.
		time.Sleep(ttl + 200*time.Millisecond) // the lease ages out on the DB clock

		over, err := add(t, ctx, s, "k", "overflow", groupMemberCap)
		require.ErrorIs(t, err, msgin.ErrOverflowDropped, "the CLAIMED set still counts against the cap")
		require.True(t, msgin.IsPermanent(err),
			"a STRANDED lease drains nothing: the rejection must be permanent, or it hot-spins forever (D-AU)")
		require.Equal(t, overflowRender("k", groupMemberCap+1, groupMemberCap, true), err.Error(),
			"the render must carry the shipped permanentError prefix")
		require.NotNil(t, over)
		require.Equal(t, groupMemberCap, memberCount(t, ctx, table, "k"), "nothing was committed")
	})

	// Spec 017 §3.3a/§3.6.3 — whole-branch review finding R-1. The over-cap
	// snapshot exists so a group that is COMPLETE but full is not deadlocked by
	// its own bound: routing.Aggregator.Handle re-evaluates the release
	// predicate against it. A TRUNCATED snapshot inverts that — the predicate
	// (WithCompletionSize(grownMemberCount), say) sees fewer members than the
	// group holds, declines, and the group never releases while the refused
	// member is dead-lettered. It is the exact deadlock the branch was added to
	// prevent, delivered by the branch itself.
	//
	// 🔴 THE MUTANT THIS CASE KILLS: restore the old bound by passing
	// maxMembers+1 — 5 here — to the live-member SELECT instead of 0. Every
	// OTHER member-cap case stays green, because every other one fills to
	// exactly the cap and a LIMIT of 5 cannot truncate 4 rows.
	t.Run("MemberCapLoweredBelowTheStoredCount_SnapshotIsNotTruncated", func(t *testing.T) {
		ctx := t.Context()
		table := fresh(ctx)
		lowered := growThenLowerTheCap(t, ctx, table, "k")

		over, err := add(t, ctx, lowered, "k", "overflow", grownMemberCount)
		require.ErrorIs(t, err, msgin.ErrOverflowDropped)
		require.True(t, msgin.IsPermanent(err), "no lease is draining this group (D-AM/D-AU)")
		require.Equal(t, overflowRender("k", grownMemberCount+1, groupMemberCap, true), err.Error(),
			"the count is the one the transaction observed: every stored row plus the refused one")

		require.NotNil(t, over)
		// Logged so a vacuous run is visible: if the fixture ever stops
		// reaching the over-cap state, this line says so before the assertion
		// below does.
		t.Logf("R-1: over-cap snapshot carried %d of %d stored live members "+
			"(cap in force %d; the old maxMembers+1 bound would have carried %d)",
			len(over.Messages()), grownMemberCount, groupMemberCap, groupMemberCap+1)
		require.Len(t, over.Messages(), grownMemberCount,
			"the rejection must carry EVERY live member, not the first maxMembers+1 of them (R-1)")
		ids := memberIDs(over)
		require.NotContains(t, ids, "overflow", "the refused member must not appear in its own rejection snapshot")
		for i := 0; i < grownMemberCount; i++ {
			require.Containsf(t, ids, grownMemberID(i), "member %d is missing from the over-cap snapshot", i)
		}
		require.Equal(t, grownMemberCount, memberCount(t, ctx, table, "k"), "the over-cap transaction committed nothing")
	})

	// Spec 017 §3.6.1 / ADR 0033 D-AP — whole-branch review finding R-11. The
	// cap check runs AFTER the member upsert precisely so an idempotent re-add
	// is a no-op; that reasoning holds for a group ABOVE the cap exactly as it
	// does for one AT it, because a re-add stores nothing and so cannot grow
	// the group past any bound. adapter/memory's GroupStore takes its dedup
	// branch FIRST and returns (full snapshot, nil) for this input — the two
	// first-party stores implement ONE SPI contract, so a redelivery that one
	// Acks must not be terminally discarded by the other.
	//
	// 🔴 THE MUTANT THIS CASE KILLS: drop the `inserted &&` guard on the cap
	// check (or apply withoutMember unconditionally). Either one refuses, or
	// strips from its own snapshot, a member that is durably present and was
	// never refused.
	t.Run("MemberCapIdempotentReAddAboveTheCapIsNotRefused", func(t *testing.T) {
		ctx := t.Context()
		table := fresh(ctx)
		lowered := growThenLowerTheCap(t, ctx, table, "k")

		// A REDELIVERY of a member stored long before the cap was lowered. The
		// upsert is a no-op, so nothing is dropped and nothing is refused.
		readd, err := add(t, ctx, lowered, "k", grownMemberID(0), 0)
		require.NoError(t, err,
			"re-adding an already-stored id cannot overflow a cap it does not grow (R-11)")
		require.NotNil(t, readd)
		t.Logf("R-11: idempotent re-add above the cap returned %d of %d stored live members",
			len(readd.Messages()), grownMemberCount)
		require.Len(t, readd.Messages(), grownMemberCount, "the unchanged snapshot, in full")
		require.Contains(t, memberIDs(readd), grownMemberID(0),
			"the re-added member is durably present and must not be stripped from its own snapshot (R-11)")
		require.Equal(t, grownMemberCount, memberCount(t, ctx, table, "k"), "the re-add wrote nothing")
	})

	// D-AV / Spec 017 §3.6a.2 — whole-branch review finding R-15, a CLAUDE.md
	// delivery blocker. The bound is validated at the SPI boundary, against a
	// REAL engine, so "before any statement runs" is asserted by observing that
	// nothing was written rather than by reading the source.
	t.Run("MemberCapRejectsAnInvalidBoundBeforeAnyStatement", func(t *testing.T) {
		ctx := t.Context()
		table := fresh(ctx)
		headers, err := msginsql.EncodeHeaders(
			msgin.NewHeaders(map[string]any{msgin.HeaderMessageID: "m0", msgin.HeaderSequenceNumber: 0}))
		require.NoError(t, err)
		addRaw := func(maxMembers int) error {
			_, err := kit.Group.AddMember(ctx, db, table, "k", "m0", 0, headers, []byte(`"p"`),
				maxMembers, directAddLeaseTTL)
			return err
		}

		// 0 is the value that used to mean UNBOUNDED — the ZERO VALUE was the
		// dangerous one. math.MaxInt is the other half of R-15: no count can
		// ever exceed it, so `n > int64(maxMembers)` never fires and the largest
		// expressible bound means NO bound. (It also used to wrap the
		// maxMembers+1 fetch LIMIT to math.MinInt; that half is now structurally
		// gone — AddMember's live fetch carries no LIMIT at all since R-1.)
		for _, bad := range []int{0, -2, (1 << 20) + 1, math.MaxInt} {
			err := addRaw(bad)
			require.ErrorIsf(t, err, msgin.ErrInvalidCapacity, "maxMembers %d must be refused", bad)
			require.Truef(t, msgin.IsPermanent(err), "maxMembers %d: the reject is permanent", bad)
			require.Equalf(t, 0, memberCount(t, ctx, table, "k"),
				"maxMembers %d: validation must precede I/O — nothing may be written", bad)
		}

		// The documented opt-out still works, and it is the ONLY non-positive
		// value that does.
		require.NoError(t, addRaw(msginsql.UnboundedGroupMembers))
		require.Equal(t, 1, memberCount(t, ctx, table, "k"),
			"UnboundedGroupMembers admits the member with no cap taken")
	})

	t.Run("MemberCapUnderCallerOwnedTx", func(t *testing.T) {
		ctx := t.Context()
		table := fresh(ctx)
		addRaw := func(q msginsql.Querier, key, id string, seq int64) (msginsql.GroupRows, error) {
			t.Helper()
			headers, err := msginsql.EncodeHeaders(
				msgin.NewHeaders(map[string]any{msgin.HeaderMessageID: id, msgin.HeaderSequenceNumber: int(seq)}))
			require.NoError(t, err)
			return kit.Group.AddMember(ctx, q, table, key, id, seq, headers, []byte(`"p"`),
				groupMemberCap, directAddLeaseTTL)
		}
		for i := 0; i < groupMemberCap; i++ {
			_, err := addRaw(db, "k", fmt.Sprintf("m%d", i), int64(i))
			require.NoError(t, err)
		}

		// AC-4b: the cap+1-th member goes through kit.Group.AddMember on a
		// CALLER-OWNED *sql.Tx — the DIALECT, directly. GroupStore cannot reach
		// this branch: NewGroupStore takes a concrete *sql.DB and Add always
		// passes it, so the *sql.Tx Querier is a GroupDialect-level contract and
		// the harness is the direct dialect caller that exercises it.
		tx, err := db.BeginTx(ctx, nil)
		require.NoError(t, err)
		defer func() { _ = tx.Rollback() }()

		_, err = addRaw(tx, "k", "overflow", groupMemberCap)
		require.Error(t, err, "the cap+1-th member is refused on a caller-owned transaction too")

		if kit.SingleWriter {
			// ASYMMETRY, RECORDED RATHER THAN FORCED AWAY: a single-writer
			// engine serializes group ops on a dedicated connection running a
			// raw BEGIN IMMEDIATE (SQLite), which needs the pool's Conn — a
			// *sql.DB capability. Such a dialect rejects a *sql.Tx Querier
			// OUTRIGHT, before any SQL runs, so there is no in-transaction
			// state to observe and the D-AP precondition is moot for it.
			require.NotErrorIs(t, err, msgin.ErrOverflowDropped,
				"a single-writer dialect refuses the Querier itself, so no cap check is ever reached")
			require.ErrorContains(t, err, "*sql.DB", "the error must name the Querier the dialect requires")
			require.NoError(t, tx.Rollback())
		} else {
			require.ErrorIs(t, err, msgin.ErrOverflowDropped)
			require.Equal(t, overflowRender("k", groupMemberCap+1, groupMemberCap, true), err.Error(),
				"the render names the ENGINE; naming a store would name one this path never touched")

			// D-AP, as tested behavior rather than as a hope: the dialect did
			// NOT roll back a transaction it does not own, so the refused
			// member row IS still visible inside it...
			var n int
			require.NoError(t, tx.QueryRowContext(ctx, memberCountSQL(kit, table), "k").Scan(&n))
			require.Equal(t, groupMemberCap+1, n, "the caller's transaction is untouched: the dialect only returns the error")

			// ...and is gone once the CALLER rolls back, which is the caller's
			// responsibility on this path.
			require.NoError(t, tx.Rollback())
		}
		require.Equal(t, groupMemberCap, memberCount(t, ctx, table, "k"))

		// AC-4b.4 — the contrapositive for the SHIPPED store: driven to the
		// same overflow through a real GroupStore, the bound is unconditionally
		// durable, because that path owns its transaction and rolls it back.
		stTable := fresh(ctx)
		st := newStore(t, stTable, msginsql.WithMaxGroupMembers(groupMemberCap))
		fillToCap(t, ctx, st, "k")
		_, err = add(t, ctx, st, "k", "overflow", groupMemberCap)
		require.ErrorIs(t, err, msgin.ErrOverflowDropped)
		require.Equal(t, groupMemberCap, memberCount(t, ctx, stTable, "k"),
			"through GroupStore the rollback is the store's own, never the caller's")
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

	t.Run("ExpiredReturnsEveryLiveMember", func(t *testing.T) {
		ctx := t.Context()
		table := fresh(ctx)
		s := newStore(t, table)

		// D-AS's OTHER half: ExpiredGroups passes limit = 0 to the dialect's
		// member SELECT, so the reaper's recovery set is COMPLETE. A LIMIT
		// leaking into THAT call site makes the reaper silently drop every
		// member of an expired group past the first — and no other case here
		// notices, because no other Expired case asserts a group with more than
		// one member. The group is UNLEASED and aged past the cutoff, so its
		// members are live (claimed_epoch IS NULL) and must all come back.
		const members = 3
		var old msgin.MessageGroup
		for i := 0; i < members; i++ {
			g, err := add(t, ctx, s, "old", fmt.Sprintf("m%d", i), i)
			require.NoError(t, err)
			old = g
		}
		require.Len(t, old.Messages(), members)

		time.Sleep(10 * time.Millisecond)
		newG, err := add(t, ctx, s, "new", "n0", 0)
		require.NoError(t, err)
		require.True(t, newG.CreatedAt().After(old.CreatedAt()))

		// The DB-clock-derived created_at of "new" is the cutoff, so there is no
		// app<->DB skew: old < cutoff (included), new == cutoff (excluded).
		exp, err := s.Expired(ctx, newG.CreatedAt())
		require.NoError(t, err)
		var got msgin.MessageGroup
		for _, g := range exp {
			if g.Key() == "old" {
				got = g
			}
		}
		require.NotNil(t, got, "the aged-out unleased group must be returned")
		require.Len(t, got.Messages(), members, "the reaper's recovery set must hold EVERY member (limit = 0, D-AS)")
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
				msgin.NewHeaders(map[string]any{msgin.HeaderMessageID: id, msgin.HeaderSequenceNumber: int(seq)}))
			require.NoError(t, err)
			_, err = kit.Group.AddMember(ctx, db, table, key, id, seq, headers, []byte(`"p"`),
				groupMemberCap, directAddLeaseTTL)
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

		// A second Aggregator over the same store recovers via its reaper. An
		// expired channel is wired in too (though WithGroupTimeout is unset, so
		// the reaper's age-expiry path never fires) so the recovery can be
		// asserted directly: the crashed COMPLETE group must reach OUTPUT, not
		// EXPIRED — asserting expiredCount==0 makes that explicit rather than
		// merely inferring it from outCount==1 alone.
		out := channel.NewDirectChannel()
		expiredCh := channel.NewDirectChannel()
		var (
			mu           sync.Mutex
			outCount     int
			expiredCount int
		)
		mustSubscribe(t, out, msgin.HandlerFunc(func(_ context.Context, _ msgin.Message[any]) error {
			mu.Lock()
			outCount++
			mu.Unlock()
			return nil
		}))
		mustSubscribe(t, expiredCh, msgin.HandlerFunc(func(_ context.Context, _ msgin.Message[any]) error {
			mu.Lock()
			expiredCount++
			mu.Unlock()
			return nil
		}))

		recoverStore := newStore(t, table, msginsql.WithGroupLeaseTTL(ttl), msginsql.WithGroupLockedBy("recoverer"))
		agg, err := routing.NewAggregator[[]byte, []byte](recoverStore,
			func(_ context.Context, group []msgin.Message[[]byte]) (msgin.Message[[]byte], error) {
				return msgin.New([]byte("aggregated")), nil
			},
			routing.WithOutputChannel(out),
			routing.WithExpiredGroupChannel(expiredCh),
			routing.WithCompletionSize(2),
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
		require.Equal(t, 0, expiredCount, "the recovered COMPLETE group must reach OUTPUT, never EXPIRED")
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

// DialectEngine returns the last element of the GroupDialect implementation's
// package path — "postgres", "mysql" or "sqlite" for the built-ins — which is
// the engine token each dialect substitutes into the member-cap rejection's
// site string ("msgin/sql/<engine>: AddMember"). It is derived from the
// dialect rather than from TestKit.Name because Name is a display label a
// runner may override (the shipped MariaDB runner sets Name "mariadb" while
// running the mysql dialect), and the site names the package that minted the
// error.
//
// A POINTER-typed dialect derives the same token as its value form. That is
// not a nicety: GroupDialect's own godoc tells implementers to assert
// conformance as `var _ msginsql.GroupDialect = (*yourDialect)(nil)`, so a
// TestKit whose Group is a *yourDialect is the documented shape, and
// reflect.Type.PkgPath is "" for a pointer type. A nil dialect, or a nil
// pointer of a named type, is handled without panicking for the same reason
// (Plan 031 Task 11, review finding R-12).
//
// It is exported so the derivation is assertable from a blackbox test (this
// module ships no other way to reach it) and so a dialect author outside this
// module can see the exact token the member-cap assertions will expect.
func DialectEngine(d msginsql.GroupDialect) string {
	rt := reflect.TypeOf(d)
	if rt == nil {
		return "" // TestKit.Group unset; let the caller's own nil fail, not a reflect panic
	}
	// Unwrap on the TYPE, not through reflect.Indirect(reflect.ValueOf(d)):
	// Indirect of a nil pointer yields the zero Value, whose Type() panics —
	// and (*yourDialect)(nil) is exactly the value a stateless dialect is
	// legitimately built from.
	for rt.Kind() == reflect.Pointer {
		rt = rt.Elem()
	}
	p := rt.PkgPath()
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[i+1:]
	}
	return p
}

// memberCountSQL renders the harness's own raw member-row count for table's
// derived member table, bound on group_key. It is shared by the pool-scoped
// count and the transaction-scoped one AC-4b issues on the caller's own
// *sql.Tx, so both observe the same rows through the same statement.
func memberCountSQL(kit TestKit, table string) string {
	return fmt.Sprintf("SELECT count(*) FROM %s WHERE group_key = %s",
		kit.Quote(table+"_member"), kit.Placeholder(1))
}
