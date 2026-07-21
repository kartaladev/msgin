package msgin_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jonboulle/clockwork"
	"github.com/kartaladev/msgin"
	"github.com/kartaladev/msgin/adapter/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
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

// sentIDs returns the ID() of every message Send has received, in order —
// used by concurrency tests that need to inspect every member routed to a
// channel (e.g. the expired-group sink, which receives one message per
// member rather than a grouped aggregate), not just the count/last.
func (c *fakeAggChannel) sentIDs() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	ids := make([]string, len(c.sent))
	for i, m := range c.sent {
		ids[i] = m.ID()
	}
	return ids
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

// settleErrStore wraps a real msgin.MessageGroupStore and makes every
// SettleGroup call fail with settleErr — used to cover releaseOnce's
// SettleGroup-error branch (its own defer-abandon-unless-settled keeps the
// claimed members retryable rather than wedging the key).
type settleErrStore struct {
	msgin.MessageGroupStore
	settleErr error
}

func (s *settleErrStore) SettleGroup(context.Context, msgin.MessageGroupClaim) error {
	return s.settleErr
}

var _ msgin.MessageGroupStore = (*settleErrStore)(nil)

// recoverIntervalStore wraps a real msgin.MessageGroupStore, overrides
// RecoverInterval to report interval, and counts Expired calls — used to
// prove Run's reaper ticks at the MIN of WithGroupTimeout and the store's
// RecoverInterval (a stand-in for a durable store's lease TTL), not just
// WithGroupTimeout alone.
type recoverIntervalStore struct {
	msgin.MessageGroupStore
	interval time.Duration
	mu       sync.Mutex
	calls    int
}

func (s *recoverIntervalStore) RecoverInterval() time.Duration { return s.interval }

func (s *recoverIntervalStore) Expired(ctx context.Context, before time.Time) ([]msgin.MessageGroup, error) {
	s.mu.Lock()
	s.calls++
	s.mu.Unlock()
	return s.MessageGroupStore.Expired(ctx, before)
}

func (s *recoverIntervalStore) expiredCalls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

var _ msgin.MessageGroupStore = (*recoverIntervalStore)(nil)

// mixedTypeGroup is a hand-rolled msgin.MessageGroup snapshot holding a
// member whose payload does NOT match the Aggregator's type parameter — the
// MessageGroupStore SPI is exported, so a blackbox test can construct a
// store returning any such snapshot; no conformant store reachable through
// Handle's own ingress PayloadOf[A] assert can produce this (a wrong-typed
// member must already be resident in the store, as here, not freshly added
// via Handle).
type mixedTypeGroup struct {
	key  string
	msgs []msgin.Message[any]
}

func (g mixedTypeGroup) Key() string                    { return g.key }
func (g mixedTypeGroup) Messages() []msgin.Message[any] { return g.msgs }
func (g mixedTypeGroup) CreatedAt() time.Time           { return time.Time{} }

var _ msgin.MessageGroup = mixedTypeGroup{}

// mixedTypeAddStore's Add always returns a group with a wrong-typed member (a
// string payload) injected ahead of the just-added message — used to drive
// toGroupEnv's non-A-member branch (M-6) for WithReleaseExpr.
type mixedTypeAddStore struct {
	msgin.MessageGroupStore
}

func (s *mixedTypeAddStore) Add(_ context.Context, key string, msg msgin.Message[any]) (msgin.MessageGroup, error) {
	return mixedTypeGroup{key: key, msgs: []msgin.Message[any]{msgin.New[any]("not-an-int"), msg}}, nil
}

var _ msgin.MessageGroupStore = (*mixedTypeAddStore)(nil)

// failNthClaimStore wraps a real msgin.MessageGroupStore and makes the Nth
// ClaimGroup call (1-based, across the store's whole lifetime) fail with err,
// delegating on every other call — used to deterministically drive release's
// drain-loop transient ClaimGroup-error swallow, a branch otherwise
// unreachable through Handle's own MAIN claim (which must succeed for the
// drain loop to run at all).
type failNthClaimStore struct {
	msgin.MessageGroupStore
	mu    sync.Mutex
	n     int
	calls int
	err   error
}

func (s *failNthClaimStore) ClaimGroup(ctx context.Context, key string) (msgin.MessageGroupClaim, error) {
	s.mu.Lock()
	s.calls++
	call := s.calls
	s.mu.Unlock()
	if call == s.n {
		return nil, s.err
	}
	return s.MessageGroupStore.ClaimGroup(ctx, key)
}

var _ msgin.MessageGroupStore = (*failNthClaimStore)(nil)

// failNthChannel is a MessageChannel that succeeds through call n-1 and fails
// every Send from call n onward — used to make a LATER release's Send fail
// while an EARLIER release on the same Aggregator instance succeeds,
// deterministically covering release's drain-loop residual-releaseOnce-
// failure branch (H-3) via a Send error (releaseOnce's agg/Send/Settle order
// means a Send failure is reached before Settle — distinct from
// TestAggregator_ReleaseDrainLoopReleaseError's Settle-error flavor of the
// same swallow branch).
type failNthChannel struct {
	mu       sync.Mutex
	failFrom int // 1-based Send() call number at which failures begin
	calls    int
	sent     []msgin.Message[any]
	err      error
}

func (c *failNthChannel) Send(_ context.Context, m msgin.Message[any]) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls++
	if c.calls >= c.failFrom {
		return c.err
	}
	c.sent = append(c.sent, m)
	return nil
}

func (c *failNthChannel) Subscribe(msgin.MessageHandler) error { return nil }

func (c *failNthChannel) sentCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.sent)
}

func (c *failNthChannel) lastSent() msgin.Message[any] {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.sent[len(c.sent)-1]
}

var _ msgin.MessageChannel = (*failNthChannel)(nil)

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
		{
			name: "WithGroupTimeout without WithExpiredGroupChannel is ErrExpiryChannelRequired",
			build: func(t *testing.T) (*msgin.Aggregator, error) {
				return msgin.NewAggregator[int, int](newIntStore(t), validFn,
					msgin.WithOutputChannel(&fakeAggChannel{}),
					msgin.WithGroupTimeout(30*time.Second))
			},
			assert: func(t *testing.T, agg *msgin.Aggregator, err error) {
				assert.ErrorIs(t, err, msgin.ErrExpiryChannelRequired)
				assert.Nil(t, agg)
			},
		},
		{
			name: "WithGroupTimeout with WithExpiredGroupChannel succeeds",
			build: func(t *testing.T) (*msgin.Aggregator, error) {
				return msgin.NewAggregator[int, int](newIntStore(t), validFn,
					msgin.WithOutputChannel(&fakeAggChannel{}),
					msgin.WithGroupTimeout(30*time.Second),
					msgin.WithExpiredGroupChannel(&fakeAggChannel{}))
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
			// Regression for the internal release seam evolving to
			// func(MessageGroup) (bool, error) (the public
			// WithReleaseStrategy(func(MessageGroup) bool) signature is
			// unchanged): proves the always-nil-error wrapper releases
			// across multiple cycles exactly as the pre-refactor bool-only
			// seam did — held, released-and-removed, held again for a
			// fresh group at the same key.
			name: "WithReleaseStrategy bool wrapper is transparent across release cycles",
			assert: func(t *testing.T) {
				store := newIntStore(t)
				out := &fakeAggChannel{}
				agg, err := msgin.NewAggregator[int, int](store, sumFn,
					msgin.WithOutputChannel(out),
					msgin.WithReleaseStrategy(func(g msgin.MessageGroup) bool {
						return len(g.Messages()) >= 2
					}),
				)
				require.NoError(t, err)

				m1 := corrMsg(1, "m1", "g", nil)
				m2 := corrMsg(2, "m2", "g", nil)
				require.NoError(t, agg.Handle(t.Context(), m1))
				assert.Equal(t, 0, out.count(), "1 of 2: held")
				require.NoError(t, agg.Handle(t.Context(), m2))
				require.Equal(t, 1, out.count(), "2 of 2: released")
				assert.Equal(t, 3, out.last().Payload())

				// group removed on release: a 3rd message to "g" starts a
				// fresh (again held) group rather than immediately
				// re-releasing — identical to the pre-refactor bool-only
				// seam's behavior.
				m3 := corrMsg(3, "m3", "g", nil)
				require.NoError(t, agg.Handle(t.Context(), m3))
				assert.Equal(t, 1, out.count(), "fresh group after removal is held again")
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
		{
			name: "store.ClaimGroup error propagates and the group is left untouched",
			assert: func(t *testing.T) {
				base := newIntStore(t)
				claimErr := errors.New("claim boom")
				store := &reapFaultStore{MessageGroupStore: base, claimErr: claimErr}
				out := &fakeAggChannel{}
				agg, err := msgin.NewAggregator[int, int](store, sumFn,
					msgin.WithOutputChannel(out), msgin.WithCompletionSize(1))
				require.NoError(t, err)

				m1 := corrMsg(1, "m1", "g", nil)
				err = agg.Handle(t.Context(), m1)
				assert.ErrorIs(t, err, claimErr)
				assert.Equal(t, 0, out.count())

				// group untouched (the claim attempt itself failed before
				// tagging anything): adding another member grows it to size 2.
				group, addErr := base.Add(t.Context(), "g", corrMsg(2, "probe", "g", nil))
				require.NoError(t, addErr)
				assert.Len(t, group.Messages(), 2)
			},
		},
		{
			name: "store.SettleGroup error propagates and the claim is abandoned (member returns live for retry)",
			assert: func(t *testing.T) {
				base := newIntStore(t)
				settleErr := errors.New("settle boom")
				store := &settleErrStore{MessageGroupStore: base, settleErr: settleErr}
				out := &fakeAggChannel{}
				agg, err := msgin.NewAggregator[int, int](store, sumFn,
					msgin.WithOutputChannel(out), msgin.WithCompletionSize(1))
				require.NoError(t, err)

				m1 := corrMsg(1, "m1", "g", nil)
				err = agg.Handle(t.Context(), m1)
				assert.ErrorIs(t, err, settleErr)
				// agg+Send already succeeded before the failed settle —
				// at-least-once, not lost: the member reaches output regardless.
				require.Equal(t, 1, out.count())

				// releaseOnce's defer-abandon-unless-settled ran: the member
				// returns to live so a retry can re-release it.
				group, addErr := base.Add(t.Context(), "g", corrMsg(2, "probe", "g", nil))
				require.NoError(t, addErr)
				assert.Len(t, group.Messages(), 2, "m1 (abandoned back to live) plus this probe")
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tc.assert(t)
		})
	}
}

// expiredHookStore wraps a real msgin.MessageGroupStore and runs hook (if set)
// synchronously inside Expired, after delegating to the wrapped store but
// before returning to the caller — used to deterministically simulate a
// concurrent Handle racing the reaper between its Expired() snapshot and its
// own ClaimGroup (the F1 re-check window), without relying on goroutine
// timing.
type expiredHookStore struct {
	msgin.MessageGroupStore
	t    *testing.T
	hook func(t *testing.T, s msgin.MessageGroupStore)
}

func (s *expiredHookStore) Expired(ctx context.Context, before time.Time) ([]msgin.MessageGroup, error) {
	groups, err := s.MessageGroupStore.Expired(ctx, before)
	if err == nil && s.hook != nil {
		s.hook(s.t, s.MessageGroupStore)
	}
	return groups, err
}

var _ msgin.MessageGroupStore = (*expiredHookStore)(nil)

// reapFaultStore wraps a real msgin.MessageGroupStore and can be scripted to
// fail Expired once (simulating a transient store error, cleared after one
// call) and/or make ClaimGroup report a miss (already gone/leased: (nil,
// nil)) or error — covering reap's Expired-error ("skip this tick") and
// ClaimGroup-miss/error ("skip this group") branches, which the project's
// hot-path coverage gate requires a test for.
type reapFaultStore struct {
	msgin.MessageGroupStore
	mu         sync.Mutex
	expiredErr error // returned once, then cleared
	claimErr   error
	claimNil   bool
}

func (s *reapFaultStore) Expired(ctx context.Context, before time.Time) ([]msgin.MessageGroup, error) {
	s.mu.Lock()
	err := s.expiredErr
	s.expiredErr = nil
	s.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return s.MessageGroupStore.Expired(ctx, before)
}

func (s *reapFaultStore) ClaimGroup(ctx context.Context, key string) (msgin.MessageGroupClaim, error) {
	if s.claimErr != nil {
		return nil, s.claimErr
	}
	if s.claimNil {
		return nil, nil
	}
	return s.MessageGroupStore.ClaimGroup(ctx, key)
}

var _ msgin.MessageGroupStore = (*reapFaultStore)(nil)

// runAndJoin starts agg.Run(ctx) in a goroutine, cancels ctx, and asserts Run
// joins within a generous deadline, returning ctx.Err(). Used by every
// TestAggregator_Run case so a hung Run fails the test instead of the whole
// suite.
func runAndJoin(t *testing.T, agg *msgin.Aggregator, ctx context.Context, cancel context.CancelFunc) {
	t.Helper()
	errCh := make(chan error, 1)
	go func() { errCh <- agg.Run(ctx) }()

	cancel()
	select {
	case err := <-errCh:
		assert.ErrorIs(t, err, context.Canceled)
	case <-time.After(5 * time.Second):
		t.Fatal("Aggregator.Run did not return after ctx cancel")
	}
}

// TestAggregator_Run covers the recovery+expiry reaper: no-timeout-and-no-
// durable-store blocks until cancel with no ticker goroutine, timeout ticks
// route an expired partial group to the expired channel and settle it (F1's
// happy path), and a group that is re-formed between the reaper's Expired()
// snapshot and its own ClaimGroup is abandoned (left live) rather than
// prematurely expired-routed (F1's re-check). Each case wraps
// goleak.VerifyNone(t) to prove Run starts no leaked goroutine and joins
// cleanly on cancel, in both the timeout and no-timeout paths.
func TestAggregator_Run(t *testing.T) {
	tests := []struct {
		name   string
		assert func(t *testing.T)
	}{
		{
			name: "no timeout: Run blocks until ctx cancel, returns ctx.Err(), and starts no ticker goroutine",
			assert: func(t *testing.T) {
				defer goleak.VerifyNone(t)

				store := newIntStore(t)
				out := &fakeAggChannel{}
				agg, err := msgin.NewAggregator[int, int](store, sumFn, msgin.WithOutputChannel(out))
				require.NoError(t, err)

				ctx, cancel := context.WithCancel(t.Context())
				runAndJoin(t, agg, ctx, cancel)
			},
		},
		{
			name: "timeout: reaper routes an expired partial group to the expired channel and removes it",
			assert: func(t *testing.T) {
				defer goleak.VerifyNone(t)

				clock := clockwork.NewFakeClock()
				store, err := memory.NewGroupStore(memory.WithGroupClock(clock))
				require.NoError(t, err)
				out := &fakeAggChannel{}
				expired := &fakeAggChannel{}
				agg, err := msgin.NewAggregator[int, int](store, sumFn,
					msgin.WithOutputChannel(out),
					msgin.WithGroupTimeout(30*time.Second),
					msgin.WithExpiredGroupChannel(expired),
					msgin.WithAggregatorClock(clock),
				)
				require.NoError(t, err)

				// Hold 1 of a would-be-2-member group.
				m1 := corrMsg(1, "m1", "g", map[string]any{msgin.HeaderSequenceSize: 2})
				require.NoError(t, agg.Handle(t.Context(), m1))
				assert.Equal(t, 0, out.count())

				ctx, cancel := context.WithCancel(t.Context())
				errCh := make(chan error, 1)
				go func() { errCh <- agg.Run(ctx) }()

				require.NoError(t, clock.BlockUntilContext(t.Context(), 1)) // reaper's ticker is now waiting on the fake clock
				clock.Advance(31 * time.Second)

				require.Eventually(t, func() bool { return expired.count() == 1 }, 2*time.Second, 10*time.Millisecond,
					"expired member was never routed to the expired channel")
				assert.Equal(t, 1, expired.last().Payload())
				assert.Equal(t, 0, out.count(), "never released via the normal size strategy")

				cancel()
				select {
				case err := <-errCh:
					assert.ErrorIs(t, err, context.Canceled)
				case <-time.After(5 * time.Second):
					t.Fatal("Aggregator.Run did not return after ctx cancel")
				}

				// group removed: a fresh member to "g" starts a new (held) group of
				// size 1, rather than growing an orphaned size-1 group to 2.
				group, addErr := store.Add(t.Context(), "g", corrMsg(2, "probe", "g", map[string]any{msgin.HeaderSequenceSize: 2}))
				require.NoError(t, addErr)
				assert.Len(t, group.Messages(), 1, "the expired group was removed; this starts fresh")
			},
		},
		{
			name: "F1 re-check: a group re-formed between Expired() and the reaper's ClaimGroup is abandoned, not expired-routed",
			assert: func(t *testing.T) {
				defer goleak.VerifyNone(t)

				clock := clockwork.NewFakeClock()
				base, err := memory.NewGroupStore(memory.WithGroupClock(clock))
				require.NoError(t, err)
				store := &expiredHookStore{MessageGroupStore: base, t: t}
				// Simulate a concurrent Handle that, between the reaper's Expired()
				// snapshot and its own ClaimGroup, claimed+settled (released) the
				// stale group and started a fresh one at the same key.
				store.hook = func(t *testing.T, s msgin.MessageGroupStore) {
					claim, _ := s.ClaimGroup(t.Context(), "g")
					if claim != nil {
						_ = s.SettleGroup(t.Context(), claim)
					}
					_, _ = s.Add(t.Context(), "g", corrMsg(9, "fresh", "g", nil))
				}

				out := &fakeAggChannel{}
				expired := &fakeAggChannel{}
				agg, err := msgin.NewAggregator[int, int](store, sumFn,
					msgin.WithOutputChannel(out),
					msgin.WithGroupTimeout(30*time.Second),
					msgin.WithExpiredGroupChannel(expired),
					msgin.WithAggregatorClock(clock),
				)
				require.NoError(t, err)

				require.NoError(t, agg.Handle(t.Context(), corrMsg(1, "m1", "g", map[string]any{msgin.HeaderSequenceSize: 2})))

				ctx, cancel := context.WithCancel(t.Context())
				errCh := make(chan error, 1)
				go func() { errCh <- agg.Run(ctx) }()

				require.NoError(t, clock.BlockUntilContext(t.Context(), 1))
				clock.Advance(31 * time.Second)

				// Poll (idempotent by id "fresh") until the reaper has finished
				// processing the tick: the fresh group is abandoned (left live)
				// holding only "fresh".
				require.Eventually(t, func() bool {
					g, addErr := base.Add(t.Context(), "g", corrMsg(9, "fresh", "g", nil))
					require.NoError(t, addErr)
					return len(g.Messages()) == 1 && g.Messages()[0].ID() == "fresh"
				}, 2*time.Second, 10*time.Millisecond, "the re-formed group was not left live")

				assert.Equal(t, 0, expired.count(), "a group re-formed after Expired() must not be expired-routed")
				assert.Equal(t, 0, out.count())

				cancel()
				select {
				case err := <-errCh:
					assert.ErrorIs(t, err, context.Canceled)
				case <-time.After(5 * time.Second):
					t.Fatal("Aggregator.Run did not return after ctx cancel")
				}
			},
		},
		{
			name: "a transient Expired error is skipped this tick and the group is reaped on the next",
			assert: func(t *testing.T) {
				defer goleak.VerifyNone(t)

				clock := clockwork.NewFakeClock()
				base, err := memory.NewGroupStore(memory.WithGroupClock(clock))
				require.NoError(t, err)
				expiredErr := errors.New("expired boom")
				store := &reapFaultStore{MessageGroupStore: base, expiredErr: expiredErr}

				out := &fakeAggChannel{}
				expired := &fakeAggChannel{}
				agg, err := msgin.NewAggregator[int, int](store, sumFn,
					msgin.WithOutputChannel(out),
					msgin.WithGroupTimeout(30*time.Second),
					msgin.WithExpiredGroupChannel(expired),
					msgin.WithAggregatorClock(clock),
				)
				require.NoError(t, err)

				require.NoError(t, agg.Handle(t.Context(), corrMsg(1, "m1", "g", map[string]any{msgin.HeaderSequenceSize: 2})))

				ctx, cancel := context.WithCancel(t.Context())
				errCh := make(chan error, 1)
				go func() { errCh <- agg.Run(ctx) }()

				// First tick: Expired errors (scripted once) — skipped, nothing reaped.
				require.NoError(t, clock.BlockUntilContext(t.Context(), 1))
				clock.Advance(30 * time.Second)
				time.Sleep(50 * time.Millisecond) // let the errored tick be processed
				assert.Equal(t, 0, expired.count(), "an Expired error must not route or remove anything this tick")

				// Second tick: the scripted error is cleared — the group is now
				// (still) expired and gets reaped normally.
				require.NoError(t, clock.BlockUntilContext(t.Context(), 1))
				clock.Advance(30 * time.Second)
				require.Eventually(t, func() bool { return expired.count() == 1 }, 2*time.Second, 10*time.Millisecond,
					"the group was never reaped after the transient error cleared")

				cancel()
				select {
				case err := <-errCh:
					assert.ErrorIs(t, err, context.Canceled)
				case <-time.After(5 * time.Second):
					t.Fatal("Aggregator.Run did not return after ctx cancel")
				}
			},
		},
		{
			name: "ClaimGroup reporting the group already gone/leased ((nil, nil)) skips it without routing or restoring",
			assert: func(t *testing.T) {
				defer goleak.VerifyNone(t)

				clock := clockwork.NewFakeClock()
				base, err := memory.NewGroupStore(memory.WithGroupClock(clock))
				require.NoError(t, err)
				store := &reapFaultStore{MessageGroupStore: base, claimNil: true}

				out := &fakeAggChannel{}
				expired := &fakeAggChannel{}
				agg, err := msgin.NewAggregator[int, int](store, sumFn,
					msgin.WithOutputChannel(out),
					msgin.WithGroupTimeout(30*time.Second),
					msgin.WithExpiredGroupChannel(expired),
					msgin.WithAggregatorClock(clock),
				)
				require.NoError(t, err)

				require.NoError(t, agg.Handle(t.Context(), corrMsg(1, "m1", "g", map[string]any{msgin.HeaderSequenceSize: 2})))

				ctx, cancel := context.WithCancel(t.Context())
				errCh := make(chan error, 1)
				go func() { errCh <- agg.Run(ctx) }()

				require.NoError(t, clock.BlockUntilContext(t.Context(), 1))
				clock.Advance(31 * time.Second)
				time.Sleep(50 * time.Millisecond) // let the tick be processed

				assert.Equal(t, 0, expired.count(), "a (nil, nil) ClaimGroup must not be treated as a group to route")
				assert.Equal(t, 0, out.count())

				cancel()
				select {
				case err := <-errCh:
					assert.ErrorIs(t, err, context.Canceled)
				case <-time.After(5 * time.Second):
					t.Fatal("Aggregator.Run did not return after ctx cancel")
				}
			},
		},
		{
			name: "a ClaimGroup error skips the group without routing or restoring",
			assert: func(t *testing.T) {
				defer goleak.VerifyNone(t)

				clock := clockwork.NewFakeClock()
				base, err := memory.NewGroupStore(memory.WithGroupClock(clock))
				require.NoError(t, err)
				claimErr := errors.New("claim boom")
				store := &reapFaultStore{MessageGroupStore: base, claimErr: claimErr}

				out := &fakeAggChannel{}
				expired := &fakeAggChannel{}
				agg, err := msgin.NewAggregator[int, int](store, sumFn,
					msgin.WithOutputChannel(out),
					msgin.WithGroupTimeout(30*time.Second),
					msgin.WithExpiredGroupChannel(expired),
					msgin.WithAggregatorClock(clock),
				)
				require.NoError(t, err)

				require.NoError(t, agg.Handle(t.Context(), corrMsg(1, "m1", "g", map[string]any{msgin.HeaderSequenceSize: 2})))

				ctx, cancel := context.WithCancel(t.Context())
				errCh := make(chan error, 1)
				go func() { errCh <- agg.Run(ctx) }()

				require.NoError(t, clock.BlockUntilContext(t.Context(), 1))
				clock.Advance(31 * time.Second)
				time.Sleep(50 * time.Millisecond) // let the tick be processed

				assert.Equal(t, 0, expired.count(), "a ClaimGroup error must not be treated as a group to route")
				assert.Equal(t, 0, out.count())

				cancel()
				select {
				case err := <-errCh:
					assert.ErrorIs(t, err, context.Canceled)
				case <-time.After(5 * time.Second):
					t.Fatal("Aggregator.Run did not return after ctx cancel")
				}
			},
		},
		{
			name: "store.RecoverInterval, when smaller than WithGroupTimeout, sets the reaper's tick cadence",
			assert: func(t *testing.T) {
				defer goleak.VerifyNone(t)

				clock := clockwork.NewFakeClock()
				base, err := memory.NewGroupStore(memory.WithGroupClock(clock))
				require.NoError(t, err)
				store := &recoverIntervalStore{MessageGroupStore: base, interval: 10 * time.Second}

				out := &fakeAggChannel{}
				expired := &fakeAggChannel{}
				agg, err := msgin.NewAggregator[int, int](store, sumFn,
					msgin.WithOutputChannel(out),
					msgin.WithGroupTimeout(100*time.Second), // far larger than the store's 10s RecoverInterval
					msgin.WithExpiredGroupChannel(expired),
					msgin.WithAggregatorClock(clock),
				)
				require.NoError(t, err)

				ctx, cancel := context.WithCancel(t.Context())
				errCh := make(chan error, 1)
				go func() { errCh <- agg.Run(ctx) }()

				require.NoError(t, clock.BlockUntilContext(t.Context(), 1)) // ticker armed at the MIN interval
				clock.Advance(10 * time.Second)                             // the store's RecoverInterval, NOT the 100s timeout

				require.Eventually(t, func() bool { return store.expiredCalls() >= 1 }, 2*time.Second, 10*time.Millisecond,
					"the reaper must tick at the store's RecoverInterval (10s), not wait for the far larger WithGroupTimeout (100s)")

				cancel()
				select {
				case err := <-errCh:
					assert.ErrorIs(t, err, context.Canceled)
				case <-time.After(5 * time.Second):
					t.Fatal("Aggregator.Run did not return after ctx cancel")
				}
			},
		},
		{
			name: "a complete-but-unclaimed group found by the age sweep is recovered to OUTPUT, not routed to expired",
			assert: func(t *testing.T) {
				defer goleak.VerifyNone(t)

				clock := clockwork.NewFakeClock()
				base, err := memory.NewGroupStore(memory.WithGroupClock(clock))
				require.NoError(t, err)

				out := &fakeAggChannel{}
				expired := &fakeAggChannel{}
				agg, err := msgin.NewAggregator[int, int](base, sumFn,
					msgin.WithOutputChannel(out),
					msgin.WithCompletionSize(2),
					msgin.WithGroupTimeout(30*time.Second),
					msgin.WithExpiredGroupChannel(expired),
					msgin.WithAggregatorClock(clock),
				)
				require.NoError(t, err)

				// Both members land directly in the store (bypassing Handle), so
				// the group is COMPLETE (size 2) but never claimed/released —
				// modeling a Handle call that completed the group then crashed
				// before reaching ClaimGroup.
				_, err = base.Add(t.Context(), "g", corrMsg(1, "m1", "g", nil))
				require.NoError(t, err)
				_, err = base.Add(t.Context(), "g", corrMsg(2, "m2", "g", nil))
				require.NoError(t, err)

				ctx, cancel := context.WithCancel(t.Context())
				errCh := make(chan error, 1)
				go func() { errCh <- agg.Run(ctx) }()

				require.NoError(t, clock.BlockUntilContext(t.Context(), 1))
				clock.Advance(31 * time.Second)

				require.Eventually(t, func() bool { return out.count() == 1 }, 2*time.Second, 10*time.Millisecond,
					"the recovery sweep must re-emit a complete-but-unclaimed group to OUTPUT")
				assert.Equal(t, 3, out.last().Payload())
				assert.Equal(t, 0, expired.count(), "a COMPLETE group must never be routed to the expired sink")

				cancel()
				select {
				case err := <-errCh:
					assert.ErrorIs(t, err, context.Canceled)
				case <-time.After(5 * time.Second):
					t.Fatal("Aggregator.Run did not return after ctx cancel")
				}
			},
		},
		{
			name: "an expired-sink Send failure abandons the group for a retry next tick, rather than dropping it",
			assert: func(t *testing.T) {
				defer goleak.VerifyNone(t)

				clock := clockwork.NewFakeClock()
				base, err := memory.NewGroupStore(memory.WithGroupClock(clock))
				require.NoError(t, err)

				out := &fakeAggChannel{}
				sendErr := errors.New("expired send boom")
				expired := &fakeAggChannel{sendErr: sendErr}
				agg, err := msgin.NewAggregator[int, int](base, sumFn,
					msgin.WithOutputChannel(out),
					msgin.WithGroupTimeout(30*time.Second),
					msgin.WithExpiredGroupChannel(expired),
					msgin.WithAggregatorClock(clock),
				)
				require.NoError(t, err)

				require.NoError(t, agg.Handle(t.Context(), corrMsg(1, "m1", "g", map[string]any{msgin.HeaderSequenceSize: 2})))

				ctx, cancel := context.WithCancel(t.Context())
				errCh := make(chan error, 1)
				go func() { errCh <- agg.Run(ctx) }()

				require.NoError(t, clock.BlockUntilContext(t.Context(), 1))
				clock.Advance(31 * time.Second)
				time.Sleep(50 * time.Millisecond) // let the tick be processed

				assert.Equal(t, 0, expired.count(), "the failed Send must not be counted as delivered")
				assert.Equal(t, 0, out.count())

				cancel()
				select {
				case err := <-errCh:
					assert.ErrorIs(t, err, context.Canceled)
				case <-time.After(5 * time.Second):
					t.Fatal("Aggregator.Run did not return after ctx cancel")
				}

				// AbandonGroup (not SettleGroup) ran: the member remains live
				// for a retry rather than being dropped.
				group, addErr := base.Add(t.Context(), "g", corrMsg(2, "probe", "g", map[string]any{msgin.HeaderSequenceSize: 2}))
				require.NoError(t, addErr)
				assert.Len(t, group.Messages(), 2, "m1 (still live after the abandon) plus this probe")
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tc.assert(t)
		})
	}
}

// TestWithReleaseExpr_FakeStore drives the two toGroupEnv guards (expr.go)
// via hand-rolled MessageGroupStore fakes — the exported MessageGroupStore
// SPI makes this blackbox-legal: no conformant store reachable through
// Handle's normal ingress can produce either scenario.
func TestWithReleaseExpr_FakeStore(t *testing.T) {
	tests := []struct {
		name   string
		assert func(t *testing.T)
	}{
		{
			name: "an empty group snapshot from a non-conformant store does not panic the release check (M-1)",
			assert: func(t *testing.T) {
				store := &emptyGroupAddStore{}
				out := &fakeAggChannel{}
				agg, err := msgin.NewAggregator[int, int](store, sumFn,
					msgin.WithOutputChannel(out),
					msgin.WithReleaseExpr[int]("len(messages) >= 1"))
				require.NoError(t, err)

				require.NotPanics(t, func() {
					err = agg.Handle(t.Context(), corrMsg(1, "m1", "g", nil))
				})
				require.NoError(t, err)
				assert.Equal(t, 0, out.count(), "an empty group snapshot never releases")
			},
		},
		{
			name: "a non-A member in the group snapshot is ErrPayloadType from toGroupEnv (M-6)",
			assert: func(t *testing.T) {
				store := &mixedTypeAddStore{}
				out := &fakeAggChannel{}
				agg, err := msgin.NewAggregator[int, int](store, sumFn,
					msgin.WithOutputChannel(out),
					msgin.WithReleaseExpr[int]("len(messages) >= 1"))
				require.NoError(t, err)

				err = agg.Handle(t.Context(), corrMsg(1, "m1", "g", nil))
				assert.ErrorIs(t, err, msgin.ErrPayloadType)
				assert.Equal(t, 0, out.count())
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) { tc.assert(t) })
	}
}

// TestAggregator_ReleaseExprReaperFallThrough covers reapGroup's H-1
// fall-through: a WithReleaseExpr that ALWAYS errors on this group's data (no
// "qty" header on its only member) means the group can never complete via the
// normal release check. Paired with WithGroupTimeout, one reaper tick after
// the group ages past the timeout must route it to the expired sink instead
// of busy-spinning claim->error->abandon forever.
func TestAggregator_ReleaseExprReaperFallThrough(t *testing.T) {
	defer goleak.VerifyNone(t)

	clock := clockwork.NewFakeClock()
	store, err := memory.NewGroupStore(memory.WithGroupClock(clock))
	require.NoError(t, err)
	out := &fakeAggChannel{}
	expired := &fakeAggChannel{}
	agg, err := msgin.NewAggregator[int, int](store, sumFn,
		msgin.WithOutputChannel(out),
		msgin.WithReleaseExpr[int](`messages[0].header("qty") > 0`),
		msgin.WithGroupTimeout(30*time.Second),
		msgin.WithExpiredGroupChannel(expired),
		msgin.WithAggregatorClock(clock),
	)
	require.NoError(t, err)

	m1 := corrMsg(1, "m1", "g", nil) // no "qty" header: the release expr always errors on this group
	err = agg.Handle(t.Context(), m1)
	require.Error(t, err, "the release-check error propagates from Handle (D14c)")
	assert.Equal(t, 0, out.count())

	ctx, cancel := context.WithCancel(t.Context())
	errCh := make(chan error, 1)
	go func() { errCh <- agg.Run(ctx) }()

	require.NoError(t, clock.BlockUntilContext(t.Context(), 1))
	clock.Advance(31 * time.Second)

	require.Eventually(t, func() bool { return expired.count() == 1 }, 2*time.Second, 10*time.Millisecond,
		"H-1: a persistently-erroring release check must fall through to age-expiry, routing the group to the expired sink rather than being stranded")
	assert.Equal(t, 1, expired.last().Payload())
	assert.Equal(t, 0, out.count(), "never released via the always-erroring release expr")

	cancel()
	select {
	case err := <-errCh:
		assert.ErrorIs(t, err, context.Canceled)
	case <-time.After(5 * time.Second):
		t.Fatal("Aggregator.Run did not return after ctx cancel")
	}

	// group settled: a fresh member to "g" starts a new (held) group.
	group, addErr := store.Add(t.Context(), "g", corrMsg(2, "probe", "g", nil))
	require.NoError(t, addErr)
	assert.Len(t, group.Messages(), 1, "the reaped group was settled; this starts fresh")
}

// TestAggregator_ReleaseExprDrainCheckError covers release's drain-loop
// residual release-check error branch (H-2), newly reachable now that
// WithReleaseExpr can itself error at eval (WithReleaseStrategy's bool-only
// wrapper never can): the main group {m1,m2} releases normally, but a
// residual {m3,m4} formed during its lease has no "qty" header on its first
// member, so the residual's release check errors. Handle must still return
// nil (the main member already settled) and the residual must be left live
// for the reaper/a later Add, not lost.
func TestAggregator_ReleaseExprDrainCheckError(t *testing.T) {
	base := newIntStore(t)
	settling := make(chan struct{})
	unblock := make(chan struct{})
	store := &blockingGroupStore{MessageGroupStore: base, settling: settling, unblock: unblock}

	out := &fakeAggChannel{}
	agg, err := msgin.NewAggregator[int, int](store, sumFn,
		msgin.WithOutputChannel(out),
		msgin.WithReleaseExpr[int](`len(messages) >= 2 && messages[0].header("qty") > 0`))
	require.NoError(t, err)

	m1 := corrMsg(1, "m1", "g", map[string]any{"qty": 1})
	m2 := corrMsg(2, "m2", "g", map[string]any{"qty": 1})
	require.NoError(t, agg.Handle(t.Context(), m1))

	handleDone := make(chan error, 1)
	go func() { handleDone <- agg.Handle(t.Context(), m2) }()

	<-settling // release of {m1,m2} is blocked just before its SettleGroup delegates

	// A residual {m3,m4} forms at the same key while {m1,m2} is under lease;
	// m3 carries no "qty" header, so the residual's release check errors.
	m3 := corrMsg(3, "m3", "g", nil)
	m4 := corrMsg(4, "m4", "g", nil)
	_, err = base.Add(t.Context(), "g", m3)
	require.NoError(t, err)
	_, err = base.Add(t.Context(), "g", m4)
	require.NoError(t, err)

	close(unblock)

	handleErr := <-handleDone
	assert.NoError(t, handleErr, "H-2: the drain loop's residual release-check error must be swallowed, not propagated")
	require.Equal(t, 1, out.count(), "only the main group emits; the errored residual is left live, not aggregated")
	assert.Equal(t, 3, out.last().Payload())

	// {m3,m4} left live (abandoned): a probe grows it to 3.
	group, addErr := base.Add(t.Context(), "g", corrMsg(5, "probe", "g", map[string]any{"qty": 1}))
	require.NoError(t, addErr)
	assert.Len(t, group.Messages(), 3)
}

// TestAggregator_ReleaseExprDrainSendError covers release's drain-loop
// residual releaseOnce-failure branch (H-3) via a Send error rather than the
// Settle-error flavor TestAggregator_ReleaseDrainLoopReleaseError already
// covers: the main group {m1,m2} releases and sends fine, but the residual
// {m3,m4}'s Send (the drain loop's re-release) fails. Handle must still
// return nil and the residual must be left live for a retry.
func TestAggregator_ReleaseExprDrainSendError(t *testing.T) {
	base := newIntStore(t)
	settling := make(chan struct{})
	unblock := make(chan struct{})
	store := &blockingGroupStore{MessageGroupStore: base, settling: settling, unblock: unblock}

	sendErr := errors.New("send boom")
	out := &failNthChannel{failFrom: 2, err: sendErr}
	agg, err := msgin.NewAggregator[int, int](store, sumFn,
		msgin.WithOutputChannel(out),
		msgin.WithReleaseExpr[int]("len(messages) >= 2"))
	require.NoError(t, err)

	m1 := corrMsg(1, "m1", "g", nil)
	m2 := corrMsg(2, "m2", "g", nil)
	require.NoError(t, agg.Handle(t.Context(), m1))

	handleDone := make(chan error, 1)
	go func() { handleDone <- agg.Handle(t.Context(), m2) }()

	<-settling // release of {m1,m2} is blocked just before its SettleGroup delegates

	m3 := corrMsg(3, "m3", "g", nil)
	m4 := corrMsg(4, "m4", "g", nil)
	_, err = base.Add(t.Context(), "g", m3)
	require.NoError(t, err)
	_, err = base.Add(t.Context(), "g", m4)
	require.NoError(t, err)

	close(unblock) // {m1,m2}'s SettleGroup succeeds; the drain loop's residual Send then fails

	handleErr := <-handleDone
	assert.NoError(t, handleErr, "H-3: the drain loop's residual releaseOnce failure must be swallowed, not propagated")
	require.Equal(t, 1, out.sentCount(), "only the main group's aggregate was actually sent; the residual's Send failed")
	assert.Equal(t, 3, out.lastSent().Payload())

	// {m3,m4} left live (releaseOnce's own defer-abandon-unless-settled ran):
	// a probe grows it to 3.
	group, addErr := base.Add(t.Context(), "g", corrMsg(5, "probe", "g", nil))
	require.NoError(t, addErr)
	assert.Len(t, group.Messages(), 3)
}

// TestAggregator_ReleaseDrainLoopTransientClaimError covers release's
// drain-loop transient ClaimGroup-error swallow branch: the main group
// {m1,m2} releases and settles normally, but the drain loop's own ClaimGroup
// call (checking for a residual) fails transiently. Handle must still return
// nil — the main member already settled, so a non-nil return here would
// wrongly Nack it.
func TestAggregator_ReleaseDrainLoopTransientClaimError(t *testing.T) {
	base := newIntStore(t)
	claimErr := errors.New("claim boom")
	store := &failNthClaimStore{MessageGroupStore: base, n: 2, err: claimErr}

	out := &fakeAggChannel{}
	agg, err := msgin.NewAggregator[int, int](store, sumFn,
		msgin.WithOutputChannel(out), msgin.WithCompletionSize(2))
	require.NoError(t, err)

	m1 := corrMsg(1, "m1", "g", nil)
	m2 := corrMsg(2, "m2", "g", nil)
	require.NoError(t, agg.Handle(t.Context(), m1))
	err = agg.Handle(t.Context(), m2)
	assert.NoError(t, err, "the drain loop's transient ClaimGroup error must be swallowed, not propagated")
	require.Equal(t, 1, out.count())
	assert.Equal(t, 3, out.last().Payload())
}
