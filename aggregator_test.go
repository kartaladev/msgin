package msgin_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/kartaladev/msgin"
	"github.com/kartaladev/msgin/adapter/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeAggChannel is a MessageChannel that records every Send and can be
// scripted to fail (sendErr), used as an Aggregator's WithOutputChannel in
// tests instead of a *msgin.DirectChannel + subscriber.
type fakeAggChannel struct {
	mu      sync.Mutex
	sent    []msgin.Message[any]
	sendErr error
}

func (c *fakeAggChannel) Send(_ context.Context, m msgin.Message[any]) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.sendErr != nil {
		return c.sendErr
	}
	c.sent = append(c.sent, m)
	return nil
}

func (c *fakeAggChannel) Subscribe(msgin.MessageHandler) error { return nil }

func (c *fakeAggChannel) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.sent)
}

func (c *fakeAggChannel) last() msgin.Message[any] {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.sent[len(c.sent)-1]
}

// failingAddStore is a MessageGroupStore whose Add always fails with addErr —
// used to cover the M-3 store.Add-error branch. Its other methods are never
// exercised by that scenario.
type failingAddStore struct {
	msgin.MessageGroupStore
	addErr error
}

func (s *failingAddStore) Add(context.Context, string, msgin.Message[any]) (msgin.MessageGroup, error) {
	return nil, s.addErr
}

var _ msgin.MessageGroupStore = (*failingAddStore)(nil)

// sumFn aggregates a group of int payloads into their sum.
func sumFn(_ context.Context, group []msgin.Message[int]) (msgin.Message[int], error) {
	sum := 0
	for _, m := range group {
		sum += m.Payload()
	}
	return msgin.New(sum), nil
}

// corrMsg builds a Message[any] carrying HeaderCorrelationID=corrID plus any
// extra headers, for driving an Aggregator with the default correlation
// strategy.
func corrMsg(payload any, id, corrID string, extra map[string]any) msgin.Message[any] {
	h := map[string]any{msgin.HeaderCorrelationID: corrID}
	for k, v := range extra {
		h[k] = v
	}
	return msgin.New[any](payload, msgin.WithID(id), msgin.WithHeaders(h))
}

func newIntStore(t *testing.T) *memory.GroupStore {
	t.Helper()
	s, err := memory.NewGroupStore()
	require.NoError(t, err)
	return s
}

func TestNewAggregator_Validation(t *testing.T) {
	validFn := func(_ context.Context, group []msgin.Message[int]) (msgin.Message[int], error) {
		return msgin.New(0), nil
	}

	tests := []struct {
		name   string
		build  func(t *testing.T) (*msgin.Aggregator, error)
		assert func(t *testing.T, agg *msgin.Aggregator, err error)
	}{
		{
			name: "nil store is ErrNilStore",
			build: func(t *testing.T) (*msgin.Aggregator, error) {
				return msgin.NewAggregator[int, int](nil, validFn, msgin.WithOutputChannel(&fakeAggChannel{}))
			},
			assert: func(t *testing.T, agg *msgin.Aggregator, err error) {
				assert.ErrorIs(t, err, msgin.ErrNilStore)
				assert.Nil(t, agg)
			},
		},
		{
			name: "nil fn is ErrNilFunc",
			build: func(t *testing.T) (*msgin.Aggregator, error) {
				return msgin.NewAggregator[int, int](newIntStore(t), nil, msgin.WithOutputChannel(&fakeAggChannel{}))
			},
			assert: func(t *testing.T, agg *msgin.Aggregator, err error) {
				assert.ErrorIs(t, err, msgin.ErrNilFunc)
				assert.Nil(t, agg)
			},
		},
		{
			name: "no WithOutputChannel is ErrNilOutput",
			build: func(t *testing.T) (*msgin.Aggregator, error) {
				return msgin.NewAggregator[int, int](newIntStore(t), validFn)
			},
			assert: func(t *testing.T, agg *msgin.Aggregator, err error) {
				assert.ErrorIs(t, err, msgin.ErrNilOutput)
				assert.Nil(t, agg)
			},
		},
		{
			name: "valid construction succeeds",
			build: func(t *testing.T) (*msgin.Aggregator, error) {
				return msgin.NewAggregator[int, int](newIntStore(t), validFn, msgin.WithOutputChannel(&fakeAggChannel{}))
			},
			assert: func(t *testing.T, agg *msgin.Aggregator, err error) {
				require.NoError(t, err)
				assert.NotNil(t, agg)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			agg, err := tc.build(t)
			tc.assert(t, agg, err)
		})
	}
}

// TestAggregator_Handle covers the correlate -> add -> release-check ->
// aggregate -> forward -> remove hot path, and its error branches. Each case
// builds its own store/channel/Aggregator and drives Handle directly.
func TestAggregator_Handle(t *testing.T) {
	tests := []struct {
		name   string
		assert func(t *testing.T)
	}{
		{
			name: "default correlation + size releases on the Nth message and removes the group",
			assert: func(t *testing.T) {
				store := newIntStore(t)
				out := &fakeAggChannel{}
				agg, err := msgin.NewAggregator[int, int](store, sumFn, msgin.WithOutputChannel(out))
				require.NoError(t, err)

				m1 := corrMsg(1, "m1", "g", map[string]any{msgin.HeaderSequenceSize: 2})
				m2 := corrMsg(2, "m2", "g", map[string]any{msgin.HeaderSequenceSize: 2})

				require.NoError(t, agg.Handle(t.Context(), m1))
				assert.Equal(t, 0, out.count(), "held: only 1 of 2 members so far")

				require.NoError(t, agg.Handle(t.Context(), m2))
				require.Equal(t, 1, out.count())
				assert.Equal(t, 3, out.last().Payload())

				// group removed on release: a 3rd message to "g" starts a fresh
				// (again held) group rather than immediately re-releasing.
				m3 := corrMsg(3, "m3", "g", map[string]any{msgin.HeaderSequenceSize: 2})
				require.NoError(t, agg.Handle(t.Context(), m3))
				assert.Equal(t, 1, out.count(), "fresh group after removal is held again")
			},
		},
		{
			name: "missing correlation is Permanent(ErrNoCorrelation)",
			assert: func(t *testing.T) {
				store := newIntStore(t)
				out := &fakeAggChannel{}
				agg, err := msgin.NewAggregator[int, int](store, sumFn, msgin.WithOutputChannel(out))
				require.NoError(t, err)

				err = agg.Handle(t.Context(), msgin.New[any](1)) // no HeaderCorrelationID
				require.Error(t, err)
				assert.ErrorIs(t, err, msgin.ErrNoCorrelation)
				assert.Contains(t, err.Error(), "permanent",
					"must be Permanent-wrapped so the runtime routes it to the invalid-message channel, not the DLQ")
				assert.Equal(t, 0, out.count())
			},
		},
		{
			name: "hold: first of two does not release",
			assert: func(t *testing.T) {
				store := newIntStore(t)
				out := &fakeAggChannel{}
				agg, err := msgin.NewAggregator[int, int](store, sumFn, msgin.WithOutputChannel(out))
				require.NoError(t, err)

				m1 := corrMsg(1, "m1", "g", map[string]any{msgin.HeaderSequenceSize: 2})
				require.NoError(t, agg.Handle(t.Context(), m1))
				assert.Equal(t, 0, out.count())
			},
		},
		{
			name: "WithCompletionSize releases at n regardless of sequence headers",
			assert: func(t *testing.T) {
				store := newIntStore(t)
				out := &fakeAggChannel{}
				agg, err := msgin.NewAggregator[int, int](store, sumFn,
					msgin.WithOutputChannel(out), msgin.WithCompletionSize(2))
				require.NoError(t, err)

				m1 := corrMsg(1, "m1", "g", nil) // no HeaderSequenceSize at all
				m2 := corrMsg(2, "m2", "g", nil)
				require.NoError(t, agg.Handle(t.Context(), m1))
				assert.Equal(t, 0, out.count())
				require.NoError(t, agg.Handle(t.Context(), m2))
				require.Equal(t, 1, out.count())
				assert.Equal(t, 3, out.last().Payload())
			},
		},
		{
			name: "WithCorrelationStrategy overrides the default HeaderCorrelationID read",
			assert: func(t *testing.T) {
				store := newIntStore(t)
				out := &fakeAggChannel{}
				const orderKey = "order-key"
				agg, err := msgin.NewAggregator[int, int](store, sumFn,
					msgin.WithOutputChannel(out),
					msgin.WithCorrelationStrategy(func(m msgin.Message[any]) (string, error) {
						v, ok := m.Header(orderKey)
						if !ok {
							return "", msgin.Permanent(msgin.ErrNoCorrelation)
						}
						return v.(string), nil
					}),
					msgin.WithCompletionSize(2),
				)
				require.NoError(t, err)

				m1 := msgin.New[any](1, msgin.WithHeaders(map[string]any{orderKey: "o1"}))
				m2 := msgin.New[any](2, msgin.WithHeaders(map[string]any{orderKey: "o1"}))
				require.NoError(t, agg.Handle(t.Context(), m1))
				assert.Equal(t, 0, out.count())
				require.NoError(t, agg.Handle(t.Context(), m2))
				require.Equal(t, 1, out.count())
				assert.Equal(t, 3, out.last().Payload())

				// HeaderCorrelationID (the default strategy's key) is ignored now.
				err = agg.Handle(t.Context(), msgin.New[any](1, msgin.WithHeaders(map[string]any{
					msgin.HeaderCorrelationID: "g",
				})))
				assert.ErrorIs(t, err, msgin.ErrNoCorrelation)
			},
		},
		{
			name: "WithReleaseStrategy custom predicate",
			assert: func(t *testing.T) {
				store := newIntStore(t)
				out := &fakeAggChannel{}
				agg, err := msgin.NewAggregator[int, int](store, sumFn,
					msgin.WithOutputChannel(out),
					msgin.WithReleaseStrategy(func(g msgin.MessageGroup) bool {
						sum := 0
						for _, m := range g.Messages() {
							sum += m.Payload().(int)
						}
						return sum >= 5
					}),
				)
				require.NoError(t, err)

				m1 := corrMsg(3, "m1", "g", nil)
				m2 := corrMsg(4, "m2", "g", nil)
				require.NoError(t, agg.Handle(t.Context(), m1))
				assert.Equal(t, 0, out.count(), "sum 3 < 5: held")
				require.NoError(t, agg.Handle(t.Context(), m2))
				require.Equal(t, 1, out.count(), "sum 7 >= 5: released")
				assert.Equal(t, 7, out.last().Payload())
			},
		},
		{
			name: "number-tolerant size: HeaderSequenceSize as float64",
			assert: func(t *testing.T) {
				store := newIntStore(t)
				out := &fakeAggChannel{}
				agg, err := msgin.NewAggregator[int, int](store, sumFn, msgin.WithOutputChannel(out))
				require.NoError(t, err)

				m1 := corrMsg(1, "m1", "g", map[string]any{msgin.HeaderSequenceSize: float64(2)})
				m2 := corrMsg(2, "m2", "g", map[string]any{msgin.HeaderSequenceSize: float64(2)})
				require.NoError(t, agg.Handle(t.Context(), m1))
				assert.Equal(t, 0, out.count())
				require.NoError(t, agg.Handle(t.Context(), m2))
				require.Equal(t, 1, out.count())
			},
		},
		{
			name: "number-tolerant size: HeaderSequenceSize as int64",
			assert: func(t *testing.T) {
				store := newIntStore(t)
				out := &fakeAggChannel{}
				agg, err := msgin.NewAggregator[int, int](store, sumFn, msgin.WithOutputChannel(out))
				require.NoError(t, err)

				m1 := corrMsg(1, "m1", "g", map[string]any{msgin.HeaderSequenceSize: int64(2)})
				m2 := corrMsg(2, "m2", "g", map[string]any{msgin.HeaderSequenceSize: int64(2)})
				require.NoError(t, agg.Handle(t.Context(), m1))
				assert.Equal(t, 0, out.count())
				require.NoError(t, agg.Handle(t.Context(), m2))
				require.Equal(t, 1, out.count())
			},
		},
		{
			name: "default release strategy never fires when HeaderSequenceSize is absent or non-numeric",
			assert: func(t *testing.T) {
				store := newIntStore(t)
				out := &fakeAggChannel{}
				agg, err := msgin.NewAggregator[int, int](store, sumFn, msgin.WithOutputChannel(out))
				require.NoError(t, err)

				// No HeaderSequenceSize at all on the group's first member: asInt's
				// "not ok" (default-zero-value) branch via a missing header.
				require.NoError(t, agg.Handle(t.Context(), corrMsg(1, "m1", "g1", nil)))
				assert.Equal(t, 0, out.count())

				// A non-numeric HeaderSequenceSize on a DIFFERENT group's first
				// member: asInt's "not ok" branch via the switch's default case.
				require.NoError(t, agg.Handle(t.Context(),
					corrMsg(2, "m2", "g2", map[string]any{msgin.HeaderSequenceSize: "not-a-number"})))
				assert.Equal(t, 0, out.count())
			},
		},
		{
			name: "wrong payload type is ErrPayloadType and never added to the store",
			assert: func(t *testing.T) {
				store := newIntStore(t)
				out := &fakeAggChannel{}
				agg, err := msgin.NewAggregator[int, int](store, sumFn, msgin.WithOutputChannel(out))
				require.NoError(t, err)

				bad := corrMsg("not-an-int", "bad", "g", map[string]any{msgin.HeaderSequenceSize: 2})
				err = agg.Handle(t.Context(), bad)
				assert.ErrorIs(t, err, msgin.ErrPayloadType)
				assert.Equal(t, 0, out.count())

				// Probe: if the mistyped message had reached store.Add, this next
				// (correctly typed) member would make the group size 2 and release
				// it. It doesn't — the group is still size 1 (held), proving the
				// mistyped message never entered the store.
				probe := corrMsg(9, "probe", "g", map[string]any{msgin.HeaderSequenceSize: 2})
				require.NoError(t, agg.Handle(t.Context(), probe))
				assert.Equal(t, 0, out.count())
			},
		},
		{
			name: "agg error propagates and the group is NOT removed",
			assert: func(t *testing.T) {
				store := newIntStore(t)
				out := &fakeAggChannel{}
				errBoom := errors.New("agg boom")
				agg, err := msgin.NewAggregator[int, int](store,
					func(context.Context, []msgin.Message[int]) (msgin.Message[int], error) {
						return msgin.Message[int]{}, errBoom
					},
					msgin.WithOutputChannel(out), msgin.WithCompletionSize(1))
				require.NoError(t, err)

				m1 := corrMsg(1, "m1", "g", nil)
				err = agg.Handle(t.Context(), m1)
				assert.ErrorIs(t, err, errBoom)
				assert.Equal(t, 0, out.count())

				// group NOT removed: adding another member grows it to size 2,
				// proving the failed-release group survived.
				group, addErr := store.Add(t.Context(), "g", corrMsg(2, "probe", "g", nil))
				require.NoError(t, addErr)
				assert.Len(t, group.Messages(), 2)
			},
		},
		{
			name: "a wrong-typed member already in the store (bypassing Handle's ingress assert) fails release with ErrPayloadType and the group is NOT removed",
			assert: func(t *testing.T) {
				// A member added directly via store.Add (not through Handle) skips
				// the ingress PayloadOf[A] assert — e.g. a store shared with, or
				// previously written by, a differently-typed Aggregator. boxAggFn
				// re-asserts each group member before calling fn precisely to catch
				// this at release time rather than a panic deep in fn.
				store := newIntStore(t)
				_, err := store.Add(t.Context(), "g", msgin.New[any]("not-an-int", msgin.WithID("bogus")))
				require.NoError(t, err)

				out := &fakeAggChannel{}
				agg, err := msgin.NewAggregator[int, int](store, sumFn,
					msgin.WithOutputChannel(out), msgin.WithCompletionSize(1))
				require.NoError(t, err)

				err = agg.Handle(t.Context(), corrMsg(1, "m1", "g", nil))
				assert.ErrorIs(t, err, msgin.ErrPayloadType)
				assert.Equal(t, 0, out.count())

				// group NOT removed: adding another member grows it to size 3.
				group, addErr := store.Add(t.Context(), "g", corrMsg(2, "probe", "g", nil))
				require.NoError(t, addErr)
				assert.Len(t, group.Messages(), 3)
			},
		},
		{
			name: "store.Add error propagates without aggregating or sending",
			assert: func(t *testing.T) {
				addErr := errors.New("store boom")
				store := &failingAddStore{addErr: addErr}
				out := &fakeAggChannel{}
				called := false
				agg, err := msgin.NewAggregator[int, int](store,
					func(context.Context, []msgin.Message[int]) (msgin.Message[int], error) {
						called = true
						return msgin.New(0), nil
					},
					msgin.WithOutputChannel(out))
				require.NoError(t, err)

				m1 := corrMsg(1, "m1", "g", nil)
				err = agg.Handle(t.Context(), m1)
				assert.ErrorIs(t, err, addErr)
				assert.False(t, called, "agg fn must not run when store.Add fails")
				assert.Equal(t, 0, out.count())
			},
		},
		{
			name: "output.Send error propagates and the group is NOT removed",
			assert: func(t *testing.T) {
				store := newIntStore(t)
				sendErr := errors.New("send boom")
				out := &fakeAggChannel{sendErr: sendErr}
				agg, err := msgin.NewAggregator[int, int](store, sumFn,
					msgin.WithOutputChannel(out), msgin.WithCompletionSize(1))
				require.NoError(t, err)

				m1 := corrMsg(1, "m1", "g", nil)
				err = agg.Handle(t.Context(), m1)
				assert.ErrorIs(t, err, sendErr)
				assert.Equal(t, 0, out.count())

				// group NOT removed: adding another member grows it to size 2,
				// proving the group survived the Send failure (retry can re-release).
				group, addErr := store.Add(t.Context(), "g", corrMsg(2, "probe", "g", nil))
				require.NoError(t, addErr)
				assert.Len(t, group.Messages(), 2)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tc.assert(t)
		})
	}
}
