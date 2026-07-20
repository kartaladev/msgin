package msgin_test

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/kartaladev/msgin"
	"github.com/kartaladev/msgin/adapter/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// idsAggChannel is a MessageChannel that records, for each released aggregate,
// the sorted set of member ids it carries (via the aggregate function's ids
// payload) — used by the N>1 same-key stress test to check both "exactly M/k
// releases" and "every member appears exactly once across all releases",
// concurrency-safe under -race.
type idsAggChannel struct {
	mu   sync.Mutex
	sent [][]string
}

func (c *idsAggChannel) Send(_ context.Context, m msgin.Message[any]) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sent = append(c.sent, m.Payload().([]string))
	return nil
}

func (c *idsAggChannel) Subscribe(msgin.MessageHandler) error { return nil }

func (c *idsAggChannel) snapshot() [][]string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([][]string, len(c.sent))
	copy(out, c.sent)
	return out
}

// TestAggregator_NStress drives many goroutines concurrently calling Handle
// with M distinct correlated members (same HeaderCorrelationID, distinct
// ids), M a multiple of k, over one Aggregator/real memory.GroupStore. The
// store's atomic ClaimGroup (not a per-key lock — Phase 3 dropped it, ADR
// 0020 §8) is the sole serializer: no member is EVER lost or double-counted
// across two claims — every member appears exactly once across all sinks
// combined.
//
// It does NOT guarantee any particular batch size, in either direction.
// Handle does not re-check the release strategy against what it actually
// claimed (only the reaper's reapGroup does that): a claim freezes whatever
// is live at claim time, which — since nothing synchronizes the release
// check against the claim — can have grown past k (documented:
// WithReleaseStrategy's monotonic-strategy note) OR, under raw same-key
// hammering, can have been raced down to a small fresh residual by another
// caller that already released the batch this Handle call originally
// observed (documented: WithCompletionSize's "safe" — meaning single-winner,
// not size-floor — guarantee). Nor does it guarantee every member reaches a
// complete (>=k) batch on its own: a small leftover residual can be left
// live with no more members ever arriving to complete it (ADR 0020 §8's
// documented "concurrent-Add-vs-abandon interleave"). This is exactly why
// "count-based recurring-key aggregation under Competing Consumers should
// set WithGroupTimeout" (Aggregator.release's doc): Run's reaper recovers
// any stranded group, complete or not — complete residuals re-emitted to
// output, genuinely incomplete leftovers aged out to the expired sink — so
// every member is guaranteed to reach ONE of the two sinks exactly once.
// Run under -race.
func TestAggregator_NStress(t *testing.T) {
	const (
		k = 10
		m = 100 // multiple of k
	)

	store, err := memory.NewGroupStore(memory.WithMaxGroups(m + 1))
	require.NoError(t, err)
	out := &idsAggChannel{}
	expired := &fakeAggChannel{}

	agg, err := msgin.NewAggregator[string, []string](store,
		func(_ context.Context, group []msgin.Message[string]) (msgin.Message[[]string], error) {
			ids := make([]string, len(group))
			for i, mg := range group {
				ids[i] = mg.Payload()
			}
			return msgin.New(ids), nil
		},
		msgin.WithOutputChannel(out),
		msgin.WithCompletionSize(k),
		msgin.WithGroupTimeout(20*time.Millisecond),
		msgin.WithExpiredGroupChannel(expired),
	)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	var runWG sync.WaitGroup
	runWG.Add(1)
	go func() { defer runWG.Done(); _ = agg.Run(ctx) }()
	defer func() { cancel(); runWG.Wait() }()

	var wg sync.WaitGroup
	wg.Add(m)
	for i := 0; i < m; i++ {
		id := fmt.Sprintf("member-%03d", i)
		go func(id string) {
			defer wg.Done()
			msg := msgin.New[any](id, msgin.WithID(id), msgin.WithHeaders(map[string]any{
				msgin.HeaderCorrelationID: "stress-group",
			}))
			assert.NoError(t, agg.Handle(t.Context(), msg))
		}(id)
	}
	wg.Wait()

	var got []string
	require.Eventually(t, func() bool {
		got = nil
		for _, ids := range out.snapshot() {
			got = append(got, ids...)
		}
		got = append(got, expired.sentIDs()...)
		return len(got) == m
	}, 5*time.Second, 10*time.Millisecond,
		"every member must eventually reach a sink: the reaper recovers any group left stranded by the "+
			"concurrent-Add-vs-abandon interleave (ADR 0020 §8) — complete residuals to output, genuinely "+
			"incomplete leftovers to expired")

	sort.Strings(got)
	want := make([]string, m)
	for i := 0; i < m; i++ {
		want[i] = fmt.Sprintf("member-%03d", i)
	}
	sort.Strings(want)
	assert.Equal(t, want, got, "every member must appear exactly once across output+expired combined (no loss, no double-release)")
}

// TestAggregator_IdempotentRedelivery feeds the same k correlated members
// twice (identical ids, simulating a source redelivery) through Handle and
// asserts the group releases exactly once — the store's idempotent-by-id Add
// (memory.GroupStore) prevents the redelivered copies from double-counting
// toward release.
func TestAggregator_IdempotentRedelivery(t *testing.T) {
	store := newIntStore(t)
	out := &fakeAggChannel{}
	agg, err := msgin.NewAggregator[int, int](store, sumFn,
		msgin.WithOutputChannel(out), msgin.WithCompletionSize(2))
	require.NoError(t, err)

	m1 := corrMsg(1, "m1", "g", nil)
	m2 := corrMsg(2, "m2", "g", nil)

	require.NoError(t, agg.Handle(t.Context(), m1))
	require.NoError(t, agg.Handle(t.Context(), m2))
	require.Equal(t, 1, out.count())
	assert.Equal(t, 3, out.last().Payload())

	// Redelivery: the exact same two messages (same ids) arrive again. Since
	// the first group already released and was removed, this forms a FRESH
	// group at the same key — still only one more release, not two, and no
	// growth beyond k for identical-id redelivery within the same group life.
	require.NoError(t, agg.Handle(t.Context(), m1))
	require.NoError(t, agg.Handle(t.Context(), m2))
	require.Equal(t, 2, out.count(), "redelivery forms a fresh group and releases exactly once more")
	assert.Equal(t, 3, out.last().Payload())

	// Now prove idempotency WITHIN one still-open group: redeliver m1 before
	// m2 arrives — the duplicate id must not grow the held group or release it
	// early.
	m3 := corrMsg(1, "m1", "h", nil) // reuses id "m1" under a fresh key "h"
	require.NoError(t, agg.Handle(t.Context(), m3))
	assert.Equal(t, 2, out.count(), "first member of group h: held")
	require.NoError(t, agg.Handle(t.Context(), m3)) // exact redelivery, same id, same key
	assert.Equal(t, 2, out.count(), "duplicate redelivery must not double-count toward release")

	group, addErr := store.Add(t.Context(), "h", corrMsg(99, "probe", "h", nil))
	require.NoError(t, addErr)
	assert.Len(t, group.Messages(), 2, "group h holds only the one deduped member plus the probe")
}

// TestAggregator_SplitterRoundTrip proves the zero-config Splitter->Aggregator
// round-trip: Split fans a batch into N children stamped with
// HeaderCorrelationID (parent id) and HeaderSequenceSize (N); feeding each
// child through Handle with the DEFAULT correlation/release strategies
// reassembles exactly one aggregate holding all N items, with no extra
// configuration on either side.
func TestAggregator_SplitterRoundTrip(t *testing.T) {
	store := newIntStore(t)
	out := &fakeAggChannel{}
	agg, err := msgin.NewAggregator[int, int](store, sumFn, msgin.WithOutputChannel(out))
	require.NoError(t, err)

	split := msgin.Split(func(_ context.Context, m msgin.Message[[]int]) ([]msgin.Message[int], error) {
		var children []msgin.Message[int]
		for _, v := range m.Payload() {
			children = append(children, msgin.WithPayload(m, v))
		}
		return children, nil
	})

	feed := msgin.HandlerFunc(func(ctx context.Context, m msgin.Message[any]) error {
		return agg.Handle(ctx, m)
	})

	h := split(feed)
	parent := msgin.New[any]([]int{1, 2, 3, 4}, msgin.WithID("order-1"))
	require.NoError(t, h.Handle(t.Context(), parent))

	require.Equal(t, 1, out.count(), "exactly one aggregate emitted for the whole batch")
	assert.Equal(t, 10, out.last().Payload())
}

// emptyGroup is a degenerate msgin.MessageGroup snapshot with no members —
// returned by emptyGroupAddStore to drive the default release strategy's
// defensive len(msgs)==0 guard, which no conformant store (Add always
// includes the just-added member) exercises through the normal Handle path.
type emptyGroup struct{}

func (emptyGroup) Key() string                    { return "g" }
func (emptyGroup) Messages() []msgin.Message[any] { return nil }
func (emptyGroup) CreatedAt() time.Time           { return time.Time{} }

var _ msgin.MessageGroup = emptyGroup{}

// emptyGroupAddStore wraps a real msgin.MessageGroupStore but makes Add
// always return an empty group snapshot regardless of what was added —
// simulating a non-conformant/edge-case store implementation, purely to reach
// defaultRelease's defensive empty-group branch.
type emptyGroupAddStore struct {
	msgin.MessageGroupStore
}

func (s *emptyGroupAddStore) Add(context.Context, string, msgin.Message[any]) (msgin.MessageGroup, error) {
	return emptyGroup{}, nil
}

var _ msgin.MessageGroupStore = (*emptyGroupAddStore)(nil)

// TestAggregator_DefaultReleaseNeverFiresOnEmptyGroup covers defaultRelease's
// len(msgs)==0 guard: even a store that (incorrectly) hands back an empty
// group from Add must never be treated as release-ready by the default
// strategy.
func TestAggregator_DefaultReleaseNeverFiresOnEmptyGroup(t *testing.T) {
	store := &emptyGroupAddStore{}
	out := &fakeAggChannel{}
	agg, err := msgin.NewAggregator[int, int](store, sumFn, msgin.WithOutputChannel(out))
	require.NoError(t, err)

	require.NoError(t, agg.Handle(t.Context(), corrMsg(1, "m1", "g", nil)))
	assert.Equal(t, 0, out.count(), "an empty group must never release")
}

// TestAggregator_ConcurrentClaim_SingleEmitNoLoss proves in-process
// claim/settle ATOMICITY (the Phase-3 analog of TestAggregator_NStress): ONE
// shared memory.GroupStore, TWO independent *Aggregator instances (no shared
// per-key lock — the store's atomic ClaimGroup is the ONLY serializer), each
// fed a share of the SAME M correlated members concurrently from many
// goroutines. No member is EVER lost or double-counted across two claims —
// this exercises the unconditional-lease guard: a second concurrent
// ClaimGroup on an in-flight claim returns nil, so the two Aggregator
// instances never both claim the same members. As in TestAggregator_NStress,
// batch size is not otherwise guaranteed (Handle does not re-check the
// release strategy against what it actually claimed), and each Aggregator
// runs its reaper (WithGroupTimeout + Run) so every member reaches a sink
// exactly once even if the two instances' concurrent claim/settle/drain
// activity strands a residual (ADR 0020 §8's documented
// "concurrent-Add-vs-abandon interleave" — MORE likely here than in
// TestAggregator_NStress precisely because two independent Aggregators race
// the same key). Run under -race.
//
// Scope (audit R1 M4 / R2 H-A): this proves in-process claim/settle
// atomicity only, NOT the sql multi-process races (completion detection,
// crash re-absorb, crash-recovery-to-output) — memory's single mutex +
// unconditional in-process lease cannot exhibit those; they are proven only
// by the sql testcontainers conformance in Task 3.
func TestAggregator_ConcurrentClaim_SingleEmitNoLoss(t *testing.T) {
	const (
		k = 10
		m = 100 // multiple of k
	)

	store, err := memory.NewGroupStore(memory.WithMaxGroups(m + 1))
	require.NoError(t, err)
	out := &idsAggChannel{}
	expired := &fakeAggChannel{}

	aggFn := func(_ context.Context, group []msgin.Message[string]) (msgin.Message[[]string], error) {
		ids := make([]string, len(group))
		for i, mg := range group {
			ids[i] = mg.Payload()
		}
		return msgin.New(ids), nil
	}
	opts := []msgin.AggregatorOption{
		msgin.WithOutputChannel(out),
		msgin.WithCompletionSize(k),
		msgin.WithGroupTimeout(20 * time.Millisecond),
		msgin.WithExpiredGroupChannel(expired),
	}

	agg1, err := msgin.NewAggregator[string, []string](store, aggFn, opts...)
	require.NoError(t, err)
	agg2, err := msgin.NewAggregator[string, []string](store, aggFn, opts...)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	var runWG sync.WaitGroup
	runWG.Add(2)
	go func() { defer runWG.Done(); _ = agg1.Run(ctx) }()
	go func() { defer runWG.Done(); _ = agg2.Run(ctx) }()
	defer func() { cancel(); runWG.Wait() }()

	var wg sync.WaitGroup
	wg.Add(m)
	for i := 0; i < m; i++ {
		id := fmt.Sprintf("member-%03d", i)
		agg := agg1
		if i%2 == 0 {
			agg = agg2
		}
		go func(id string, agg *msgin.Aggregator) {
			defer wg.Done()
			msg := msgin.New[any](id, msgin.WithID(id), msgin.WithHeaders(map[string]any{
				msgin.HeaderCorrelationID: "stress-group",
			}))
			assert.NoError(t, agg.Handle(t.Context(), msg))
		}(id, agg)
	}
	wg.Wait()

	var got []string
	require.Eventually(t, func() bool {
		got = nil
		for _, ids := range out.snapshot() {
			got = append(got, ids...)
		}
		got = append(got, expired.sentIDs()...)
		return len(got) == m
	}, 5*time.Second, 10*time.Millisecond,
		"every member must eventually reach a sink: either instance's reaper recovers a stranded group")

	sort.Strings(got)
	want := make([]string, m)
	for i := 0; i < m; i++ {
		want[i] = fmt.Sprintf("member-%03d", i)
	}
	sort.Strings(want)
	assert.Equal(t, want, got, "every member must appear exactly once across output+expired combined (no loss, no double-release)")
}

// blockingGroupStore wraps a real msgin.MessageGroupStore and, on the FIRST
// call only, blocks inside SettleGroup — after signaling settling, before
// delegating to the wrapped store — until the test sends on unblock. This
// lets a test deterministically inject a concurrent Add (or otherwise drive
// a specific interleave) in the middle of a release's settle step, without
// relying on goroutine timing. Only the first call blocks (via sync.Once) so
// a subsequent settle in the same release's drain loop is not itself wedged
// waiting for a second, never-arriving signal. If failAfterBlock is set,
// every SettleGroup call AFTER the first (blocked) one fails with that error
// instead of delegating — used to drive release's drain-loop re-release-error
// branch deterministically.
type blockingGroupStore struct {
	msgin.MessageGroupStore
	once           sync.Once
	settling       chan struct{} // signaled once, right before the first SettleGroup delegates
	unblock        chan struct{} // the first SettleGroup blocks reading this before delegating
	failAfterBlock error         // if set, every SettleGroup call AFTER the first fails with this error
}

func (s *blockingGroupStore) SettleGroup(ctx context.Context, claim msgin.MessageGroupClaim) error {
	first := false
	s.once.Do(func() {
		first = true
		if s.settling != nil {
			s.settling <- struct{}{}
		}
		if s.unblock != nil {
			<-s.unblock
		}
	})
	if !first && s.failAfterBlock != nil {
		return s.failAfterBlock
	}
	return s.MessageGroupStore.SettleGroup(ctx, claim)
}

var _ msgin.MessageGroupStore = (*blockingGroupStore)(nil)

// TestAggregator_LateMemberSurvivesSettle drives the prefix fence (audit R1
// H3): while a release's claim of {m1,m2} (WithCompletionSize(2)) is blocked
// just before its SettleGroup delegates, a distinct 3rd member arrives for
// the same key. Asserts the emitted aggregate holds exactly the claimed two
// (m3 must NOT leak into it — the prefix fence), and that m3 survives
// settlement as a fresh, still-incomplete group rather than being lost.
func TestAggregator_LateMemberSurvivesSettle(t *testing.T) {
	base := newIntStore(t)
	settling := make(chan struct{})
	unblock := make(chan struct{})
	store := &blockingGroupStore{MessageGroupStore: base, settling: settling, unblock: unblock}

	out := &fakeAggChannel{}
	agg, err := msgin.NewAggregator[int, int](store, sumFn,
		msgin.WithOutputChannel(out), msgin.WithCompletionSize(2))
	require.NoError(t, err)

	m1 := corrMsg(1, "m1", "g", nil)
	m2 := corrMsg(2, "m2", "g", nil)
	require.NoError(t, agg.Handle(t.Context(), m1))
	assert.Equal(t, 0, out.count(), "held: only 1 of 2 members so far")

	handleDone := make(chan error, 1)
	go func() { handleDone <- agg.Handle(t.Context(), m2) }()

	<-settling // the release of {m1,m2} is now blocked just before SettleGroup delegates

	// Inject a distinct late (3rd) member for the same key while {m1,m2} is
	// under lease.
	late := corrMsg(3, "m3", "g", nil)
	_, err = base.Add(t.Context(), "g", late)
	require.NoError(t, err)

	close(unblock) // let SettleGroup — and the rest of release — proceed

	require.NoError(t, <-handleDone)
	require.Equal(t, 1, out.count(), "exactly one aggregate emitted for the first two members")
	assert.Equal(t, 3, out.last().Payload(), "1+2: the late member m3 must NOT be in this aggregate")

	// The late member survives settlement as a fresh, still-incomplete group.
	group, addErr := base.Add(t.Context(), "g", corrMsg(4, "probe", "g", nil))
	require.NoError(t, addErr)
	require.Len(t, group.Messages(), 2, "the late member m3 plus this probe")
	assert.Equal(t, "m3", group.Messages()[0].ID())
}

// TestAggregator_ReleaseDrainsCompleteResidualWithoutTimeoutOrNewMember is
// the H1 strand regression guard (audit R3 H1): with WithCompletionSize(2),
// NO WithGroupTimeout, and the same blocking-store interleave, a complete
// residual {m3,m4} forms at the same key while {m1,m2}'s release is blocked
// mid-settle. Asserts release's drain loop (not a reaper, not a new member,
// not a timeout — none are involved here) emits the residual too: two
// aggregates total.
func TestAggregator_ReleaseDrainsCompleteResidualWithoutTimeoutOrNewMember(t *testing.T) {
	base := newIntStore(t)
	settling := make(chan struct{})
	unblock := make(chan struct{})
	store := &blockingGroupStore{MessageGroupStore: base, settling: settling, unblock: unblock}

	out := &fakeAggChannel{}
	agg, err := msgin.NewAggregator[int, int](store, sumFn,
		msgin.WithOutputChannel(out), msgin.WithCompletionSize(2))
	require.NoError(t, err)

	m1 := corrMsg(1, "m1", "g", nil)
	m2 := corrMsg(2, "m2", "g", nil)
	require.NoError(t, agg.Handle(t.Context(), m1))

	handleDone := make(chan error, 1)
	go func() { handleDone <- agg.Handle(t.Context(), m2) }()

	<-settling // release of {m1,m2} is blocked just before SettleGroup delegates

	// Form a complete residual {m3,m4} at the same key while {m1,m2} is under lease.
	m3 := corrMsg(3, "m3", "g", nil)
	m4 := corrMsg(4, "m4", "g", nil)
	_, err = base.Add(t.Context(), "g", m3)
	require.NoError(t, err)
	_, err = base.Add(t.Context(), "g", m4)
	require.NoError(t, err)

	close(unblock) // let {m1,m2}'s SettleGroup — and then the drain loop — proceed

	require.NoError(t, <-handleDone)

	require.Equal(t, 2, out.count(), "the drain loop must emit the now-complete residual too, with no new member or timeout")
	assert.Equal(t, 3, out.sent[0].Payload(), "first aggregate: m1+m2")
	assert.Equal(t, 7, out.sent[1].Payload(), "second aggregate (drained residual): m3+m4")
}

// TestAggregator_ReleaseDrainLoopReleaseError covers release's drain-loop
// re-release-error branch: the first batch {m1,m2} settles normally, but the
// drain loop's re-release of the complete residual {m3,m4} fails at
// SettleGroup. Both batches are still aggregated and SENT (at-least-once —
// the Send already happened before the second settle failed), the error
// propagates out of Handle, and releaseOnce's defer-abandon leaves {m3,m4}
// live for a retry rather than losing them.
func TestAggregator_ReleaseDrainLoopReleaseError(t *testing.T) {
	base := newIntStore(t)
	settleErr := errors.New("settle boom")
	settling := make(chan struct{})
	unblock := make(chan struct{})
	store := &blockingGroupStore{MessageGroupStore: base, settling: settling, unblock: unblock, failAfterBlock: settleErr}

	out := &fakeAggChannel{}
	agg, err := msgin.NewAggregator[int, int](store, sumFn,
		msgin.WithOutputChannel(out), msgin.WithCompletionSize(2))
	require.NoError(t, err)

	m1 := corrMsg(1, "m1", "g", nil)
	m2 := corrMsg(2, "m2", "g", nil)
	require.NoError(t, agg.Handle(t.Context(), m1))

	handleDone := make(chan error, 1)
	go func() { handleDone <- agg.Handle(t.Context(), m2) }()

	<-settling // release of {m1,m2} is blocked just before its (first, successful) SettleGroup delegates

	// Form a complete residual {m3,m4} at the same key while {m1,m2} is under lease.
	m3 := corrMsg(3, "m3", "g", nil)
	m4 := corrMsg(4, "m4", "g", nil)
	_, err = base.Add(t.Context(), "g", m3)
	require.NoError(t, err)
	_, err = base.Add(t.Context(), "g", m4)
	require.NoError(t, err)

	close(unblock) // {m1,m2}'s SettleGroup succeeds; the drain loop's re-release of {m3,m4} then fails at SettleGroup

	handleErr := <-handleDone
	assert.ErrorIs(t, handleErr, settleErr, "the drain loop's failed re-release error must propagate out of Handle")

	require.Equal(t, 2, out.count(), "the residual {m3,m4} WAS aggregated and sent before its SettleGroup failed — at-least-once, not lost")
	assert.Equal(t, 3, out.sent[0].Payload(), "first: m1+m2")
	assert.Equal(t, 7, out.sent[1].Payload(), "second: m3+m4 (sent OK, but its settle failed — a documented at-least-once duplicate on retry)")

	// releaseOnce's defer-abandon-unless-settled ran for the failed second
	// claim: {m3,m4} return to live for a retry, rather than being lost.
	group, addErr := base.Add(t.Context(), "g", corrMsg(5, "probe", "g", nil))
	require.NoError(t, addErr)
	assert.Len(t, group.Messages(), 3, "m3, m4 (abandoned back to live after the failed settle) plus this probe")
}
