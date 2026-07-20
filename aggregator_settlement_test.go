package msgin_test

import (
	"context"
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
// per-key shard lock (H-1) must make this deterministic: exactly M/k
// aggregates are emitted, and every one of the M member ids appears exactly
// once across all emitted aggregates (no double-release, no lost/duplicated
// member). Run under -race.
func TestAggregator_NStress(t *testing.T) {
	const (
		k = 10
		m = 100 // multiple of k
	)

	store, err := memory.NewGroupStore(memory.WithMaxGroups(m + 1))
	require.NoError(t, err)
	out := &idsAggChannel{}

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
	)
	require.NoError(t, err)

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

	sent := out.snapshot()
	require.Len(t, sent, m/k, "exactly M/k aggregates must be emitted, no double-release")

	var got []string
	for _, ids := range sent {
		assert.Len(t, ids, k, "every released aggregate must hold exactly k members")
		got = append(got, ids...)
	}
	sort.Strings(got)

	want := make([]string, m)
	for i := 0; i < m; i++ {
		want[i] = fmt.Sprintf("member-%03d", i)
	}
	sort.Strings(want)

	assert.Equal(t, want, got, "every member must appear exactly once across all emitted aggregates")
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
