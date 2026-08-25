package routing_test

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/jonboulle/clockwork"
	"github.com/kartaladev/msgin"
	"github.com/kartaladev/msgin/adapter/memory"
	"github.com/kartaladev/msgin/routing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
)

// fakeAggChannel is a MessageChannel that records every Send and can be
// scripted to fail (sendErr), used as an Aggregator's WithOutputChannel in
// tests instead of a *channel.DirectChannel + subscriber.
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

// snapshotErrStore is a MessageGroupStore whose Add returns a scripted
// (group, err) PAIR — the shape Spec 017 §3.3a made legal — while delegating
// every other method to the embedded store. It exists to drive the two
// snapshot-with-error cases a first-party store cannot produce on demand: a
// NON-overflow error carrying a live snapshot (finding R-3's primary hole, the
// shape sql.GroupStore.Add takes on a corrupt stored header) and a TYPED-NIL
// snapshot (R-3's secondary hole).
type snapshotErrStore struct {
	msgin.MessageGroupStore
	group msgin.MessageGroup
	err   error
}

func (s *snapshotErrStore) Add(context.Context, string, msgin.Message[any]) (msgin.MessageGroup, error) {
	return s.group, s.err
}

var _ msgin.MessageGroupStore = (*snapshotErrStore)(nil)

// valueGroup is a minimal VALUE-typed msgin.MessageGroup a scripted store can
// hand back beside an error — the shape sql.groupSnapshot takes when
// decodeGroupRows fails: a zero value, implicitly converted to a NON-nil
// interface (R-3).
type valueGroup struct {
	key  string
	msgs []msgin.Message[any]
	at   time.Time
}

func (g valueGroup) Key() string                    { return g.key }
func (g valueGroup) Messages() []msgin.Message[any] { return g.msgs }
func (g valueGroup) CreatedAt() time.Time           { return g.at }

var _ msgin.MessageGroup = valueGroup{}

// ptrGroup is a POINTER-typed msgin.MessageGroup whose every method
// dereferences the receiver, so a typed-nil (*ptrGroup)(nil) placed in the
// interface is `!= nil` yet panics on first use. That is R-3's secondary hole
// and it is not hypothetical: msgin.MessageGroupStore's own godoc tells
// implementers to assert conformance as `var _ msgin.MessageGroup =
// (*yourGroup)(nil)`, so pointer-typed snapshots are the invited shape.
type ptrGroup struct {
	key  string
	msgs []msgin.Message[any]
	at   time.Time
}

func (g *ptrGroup) Key() string                    { return g.key }
func (g *ptrGroup) Messages() []msgin.Message[any] { return g.msgs }
func (g *ptrGroup) CreatedAt() time.Time           { return g.at }

var _ msgin.MessageGroup = (*ptrGroup)(nil)

// nilAddStore is a MessageGroupStore whose Add returns a NIL group snapshot
// together with a NIL error, breaking the SPI's Add contract. It exists because
// MessageGroupStore is the PUBLIC adapter SPI: the store is caller-injected, so
// a nil-nil pair is caller input reaching library code and must not be deferred
// into a nil-pointer panic.
type nilAddStore struct {
	msgin.MessageGroupStore
}

func (s *nilAddStore) Add(context.Context, string, msgin.Message[any]) (msgin.MessageGroup, error) {
	return nil, nil
}

var _ msgin.MessageGroupStore = (*nilAddStore)(nil)

// typedNilAddStore is nilAddStore's twin for the TYPED-nil case: its Add
// returns a (*ptrGroup)(nil) — an interface that is `!= nil` and panics on
// first use — together with a nil error. The interface-nil test that catches
// nilAddStore does not catch this one (Plan 031 finding R-3, whose secondary
// hole is the same defect on Handle's OTHER snapshot guard).
type typedNilAddStore struct {
	msgin.MessageGroupStore
}

func (s *typedNilAddStore) Add(context.Context, string, msgin.Message[any]) (msgin.MessageGroup, error) {
	return (*ptrGroup)(nil), nil
}

var _ msgin.MessageGroupStore = (*typedNilAddStore)(nil)

// TestAggregator_NilGroupFromStoreIsPermanentTypedError covers the choke-point
// guard in Handle, across EVERY release strategy the Aggregator can be built
// with.
//
// The table is the whole point. Handle chooses among four release values — the
// default, WithCompletionSize, WithReleaseWhen, and a caller's own
// WithReleaseStrategy — and all four dereference the group snapshot. A guard
// placed inside any one of them (the shape this test previously asserted) left
// the other three panicking on the same input; only a guard BEFORE the call
// covers the set. Three of these four cases panicked before the choke-point
// guard existed.
//
// The fifth row adds the OTHER half of "nil": a typed nil. The store dimension
// does not need crossing with the strategy dimension, because the guard is one
// choke point — the first four rows prove it precedes all four strategies, and
// the fifth proves it catches both spellings of nil.
//
// It asserts a typed error rather than a silent hold: on a hold Handle returns
// nil and the source Acks, for a message the store just said it cannot read
// back — which risks a message that is durable nowhere. The Permanent wrap is
// D-M's reasoning (a deterministic implementation fault cannot be fixed by
// redelivery, so retrying it only burns MaxAttempts).
func TestAggregator_NilGroupFromStoreIsPermanentTypedError(t *testing.T) {
	tests := []struct {
		name  string
		opt   routing.AggregatorOption
		store msgin.MessageGroupStore // nil means &nilAddStore{}
	}{
		{name: "default release strategy"},
		{name: "WithCompletionSize", opt: routing.WithCompletionSize(2)},
		{
			name: "WithReleaseWhen",
			opt:  routing.WithReleaseWhen(func(g msgin.MessageGroup) bool { return len(g.Messages()) >= 1 }),
		},
		{
			name: "WithReleaseStrategy",
			opt: routing.WithReleaseStrategy(func(g msgin.MessageGroup) (bool, error) {
				return len(g.Messages()) >= 1, nil
			}),
		},
		{
			name:  "a TYPED-nil snapshot is caught by the same guard",
			store: &typedNilAddStore{},
			opt: routing.WithReleaseStrategy(func(g msgin.MessageGroup) (bool, error) {
				return len(g.Messages()) >= 1, nil // panics on a typed nil that reaches it
			}),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			out := &fakeAggChannel{}
			opts := []routing.AggregatorOption{routing.WithOutputChannel(out)}
			if tc.opt != nil {
				opts = append(opts, tc.opt)
			}
			var store msgin.MessageGroupStore = &nilAddStore{}
			if tc.store != nil {
				store = tc.store
			}
			agg, err := routing.NewAggregator[int, int](store, sumFn, opts...)
			require.NoError(t, err)

			msg := msgin.New[any](1, msgin.WithHeaders(map[string]any{msgin.HeaderCorrelationID: "g"}))
			require.NotPanics(t, func() {
				err = agg.Handle(t.Context(), msg)
			})
			require.ErrorIs(t, err, msgin.ErrNilMessageGroup)
			require.True(t, msgin.IsPermanent(err),
				"a store that returns nil snapshots cannot be fixed by redelivery, so it must not be retried")
			require.ErrorContains(t, err, "MessageGroupStore.Add",
				"the error names the SPI contract that was broken")
			require.Equal(t, 0, out.count(), "nothing may be released from a group that could not be read")
		})
	}
}

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

// nilClaimStore wraps a real msgin.MessageGroupStore and reports EVERY
// ClaimGroup as "already held by another holder" — (nil, nil) — while
// delegating Add. It drives Handle's over-cap `claim == nil` exit, which is a
// DELIBERATE divergence from the success path: there the same condition
// returns nil (the group is held, the member is stored), here the member was
// never stored, so nil would Ack an unstored message.
type nilClaimStore struct {
	msgin.MessageGroupStore
}

func (s *nilClaimStore) ClaimGroup(context.Context, string) (msgin.MessageGroupClaim, error) {
	return nil, nil
}

var _ msgin.MessageGroupStore = (*nilClaimStore)(nil)

// failOnceChannel is a MessageChannel whose FIRST Send fails and whose later
// Sends succeed — the AC-1b fixture, where the release must fail once so the
// group is left complete-but-abandoned and the redelivery has something to
// re-trigger.
type failOnceChannel struct {
	mu     sync.Mutex
	failed bool
	sent   []msgin.Message[any]
	err    error
}

func (c *failOnceChannel) Send(_ context.Context, m msgin.Message[any]) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.failed {
		c.failed = true
		return c.err
	}
	c.sent = append(c.sent, m)
	return nil
}

func (c *failOnceChannel) sentCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.sent)
}

var _ msgin.MessageChannel = (*failOnceChannel)(nil)

// declineThenRelease returns a ReleaseStrategy that DECLINES its first n calls
// and from call n+1 onward returns (true, err).
//
// The staging is what makes Handle's over-cap branch reachable: a strategy
// that fires (or errors) on the way UP would release — or fail — before the
// group ever reaches its member cap, so the branch under test would never run.
func declineThenRelease(n int, err error) routing.ReleaseStrategy {
	var mu sync.Mutex
	calls := 0
	return func(msgin.MessageGroup) (bool, error) {
		mu.Lock()
		defer mu.Unlock()
		calls++
		if calls <= n {
			return false, nil
		}
		return true, err
	}
}

// releaseAt returns a count-based ReleaseStrategy that fires once the group
// holds n members, with no header dependency.
func releaseAt(n int) routing.ReleaseStrategy {
	return func(g msgin.MessageGroup) (bool, error) { return len(g.Messages()) >= n, nil }
}

// fixedKey correlates every message to one group, so a test need not stamp a
// correlation header on an id-less fixture.
func fixedKey(msgin.Message[any]) (string, error) { return "k", nil }

// cappedStore builds a memory.GroupStore whose per-group member cap is
// maxMembers.
func cappedStore(t *testing.T, maxMembers int) *memory.GroupStore {
	t.Helper()
	s, err := memory.NewGroupStore(memory.WithMaxGroupMembers(maxMembers))
	require.NoError(t, err)
	return s
}

// cappedAggOpts is the Aggregator fixture Spec 017 §6 AC-1 pins, and its shape
// is MEASURED rather than assumed: NewAggregator needs store, fn,
// WithOutputChannel AND WithCorrelationStrategy — a bare NewAggregator(store,
// fn) returns "msgin: aggregator output channel is nil", and the default
// correlator returns Permanent(msgin.ErrNoCorrelation) for a message carrying
// no correlation header.
//
// opts is applied ON TOP, so passing NO release option deliberately leaves
// routing's own default (HeaderSequenceSize-driven) strategy in place. That is
// the fourth release path, and it is the one no ceiling can constrain, because
// its threshold arrives as DATA.
func cappedAggOpts(
	t *testing.T,
	store msgin.MessageGroupStore,
	out msgin.MessageChannel,
	opts ...routing.AggregatorOption,
) *routing.Aggregator {
	t.Helper()
	agg, err := routing.NewAggregator[int, int](store, sumFn,
		append([]routing.AggregatorOption{
			routing.WithOutputChannel(out),
			routing.WithCorrelationStrategy(fixedKey),
		}, opts...)...)
	require.NoError(t, err)
	return agg
}

// cappedAgg builds an Aggregator over a memory.GroupStore capped at
// maxMembers, correlating everything to one key and releasing per fn.
func cappedAgg(
	t *testing.T,
	maxMembers int,
	out msgin.MessageChannel,
	fn routing.ReleaseStrategy,
	wrap func(msgin.MessageGroupStore) msgin.MessageGroupStore,
) *routing.Aggregator {
	t.Helper()
	var store msgin.MessageGroupStore = cappedStore(t, maxMembers)
	if wrap != nil {
		store = wrap(store)
	}
	return cappedAggOpts(t, store, out, routing.WithReleaseStrategy(fn))
}

// handleScriptedAdd drives ONE Handle call against a store whose Add returns
// the scripted (group, addErr) pair, and reports Handle's error, how many
// times the release strategy was reached, and the output channel.
//
// The strategy is the greediest one possible — it always RELEASES — so any
// gate that fails to stop an ineligible snapshot is visible twice over: as a
// non-zero call count, and as an error that is no longer addErr (the embedded
// store holds no group for the key, so a release attempt returns Handle's own
// overflowRetryable instead). The embedded store is real, not nil, so a gate
// failure surfaces as a wrong ERROR rather than as a nil-interface panic.
func handleScriptedAdd(t *testing.T, group msgin.MessageGroup, addErr error) (int, *fakeAggChannel, error) {
	t.Helper()

	out := &fakeAggChannel{}
	calls := 0
	store := &snapshotErrStore{MessageGroupStore: newIntStore(t), group: group, err: addErr}
	agg, err := routing.NewAggregator[int, int](store, sumFn,
		routing.WithOutputChannel(out),
		routing.WithCorrelationStrategy(fixedKey),
		routing.WithReleaseStrategy(func(msgin.MessageGroup) (bool, error) {
			calls++
			return true, nil
		}))
	require.NoError(t, err)

	return calls, out, agg.Handle(t.Context(), msgin.New[any](1))
}

// driveToLeasedOverflow fills store (cap 4, key "k") to its member cap with a
// lease held over the FIRST claimedBeforeCap members, then offers one more
// member and returns Handle's error for it.
//
// The lease is what selects the arm under test. memory.GroupStore.Add
// classifies an over-cap rejection BY CAUSE: Permanent when the group is
// unleased (nothing will drain it), plain/TRANSIENT while a claim is in
// flight. Only the transient arm can show that a Permanent release-strategy
// error ESCALATES the joined classification (D-AW) rather than merely agreeing
// with a store error that was permanent anyway.
//
// claimedBeforeCap also sets how many members are LIVE in the snapshot that
// rides out with the error: memory returns msgs[claimedLen:], so claiming 2 of
// 4 leaves a 2-member residual, and claiming all 4 leaves an EMPTY one — the
// zero-member snapshot finding R-4 is about. The claim is deliberately never
// settled; the test ends with it still held.
func driveToLeasedOverflow(t *testing.T, store *memory.GroupStore, agg *routing.Aggregator, claimedBeforeCap int) error {
	t.Helper()

	for i := range claimedBeforeCap {
		require.NoError(t, agg.Handle(t.Context(), corrMsg(i, "m"+strconv.Itoa(i), "k", nil)))
	}
	claim, err := store.ClaimGroup(t.Context(), "k")
	require.NoError(t, err)
	require.NotNil(t, claim, "the lease must be taken, or the overflow arm below is the unleased/Permanent one")
	require.Len(t, claim.Messages(), claimedBeforeCap)

	for i := claimedBeforeCap; i < memberCapFixture; i++ {
		require.NoError(t, agg.Handle(t.Context(), corrMsg(i, "m"+strconv.Itoa(i), "k", nil)))
	}
	return agg.Handle(t.Context(), corrMsg(9, "m9", "k", nil))
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
		build  func(t *testing.T) (*routing.Aggregator, error)
		assert func(t *testing.T, agg *routing.Aggregator, err error)
	}{
		{
			name: "nil store is ErrNilStore",
			build: func(t *testing.T) (*routing.Aggregator, error) {
				return routing.NewAggregator[int, int](nil, validFn, routing.WithOutputChannel(&fakeAggChannel{}))
			},
			assert: func(t *testing.T, agg *routing.Aggregator, err error) {
				assert.ErrorIs(t, err, msgin.ErrNilStore)
				assert.Nil(t, agg)
			},
		},
		{
			// The CONSTRUCTOR arm of D-M's invariant (ADR 0029 §5.0b): unlike the
			// flow-path producers, NewAggregator returns ErrNilFunc BARE and
			// deliberately transient — construction never reaches a RetryPolicy, so
			// a retry classification would be meaningless on it. The exclusion is a
			// decision, not an omission; a later sweep must not "finish the job".
			name: "nil fn is a BARE, non-permanent ErrNilFunc (construction-time)",
			build: func(t *testing.T) (*routing.Aggregator, error) {
				return routing.NewAggregator[int, int](newIntStore(t), nil, routing.WithOutputChannel(&fakeAggChannel{}))
			},
			assert: func(t *testing.T, agg *routing.Aggregator, err error) {
				assert.ErrorIs(t, err, msgin.ErrNilFunc)
				assert.False(t, msgin.IsPermanent(err), "construction-time ErrNilFunc must stay bare")
				assert.Contains(t, err.Error(), "routing.NewAggregator: nil fn")
				assert.Nil(t, agg)
			},
		},
		{
			// A nil STRATEGY is rejected here rather than at the option, and
			// rather than being left to deref inside Handle: a nil correlate/
			// release would panic on the first message, and inside a Consumer
			// safeHandle classifies that panic as ErrHandlerPanic — TRANSIENT —
			// so a pure misconfiguration would retry forever instead of
			// surfacing. Same bare, non-permanent shape as the nil-fn arm above.
			name: "nil WithCorrelationStrategy is a BARE ErrNilFunc naming its position",
			build: func(t *testing.T) (*routing.Aggregator, error) {
				return routing.NewAggregator[int, int](newIntStore(t), validFn,
					routing.WithOutputChannel(&fakeAggChannel{}),
					routing.WithCorrelationStrategy(nil))
			},
			assert: func(t *testing.T, agg *routing.Aggregator, err error) {
				assert.ErrorIs(t, err, msgin.ErrNilFunc)
				assert.False(t, msgin.IsPermanent(err), "construction-time ErrNilFunc must stay bare")
				assert.Contains(t, err.Error(), "routing.NewAggregator: nil correlation strategy")
				assert.Nil(t, agg)
			},
		},
		{
			name: "nil WithReleaseStrategy is a BARE ErrNilFunc naming its position",
			build: func(t *testing.T) (*routing.Aggregator, error) {
				return routing.NewAggregator[int, int](newIntStore(t), validFn,
					routing.WithOutputChannel(&fakeAggChannel{}),
					routing.WithReleaseStrategy(nil))
			},
			assert: func(t *testing.T, agg *routing.Aggregator, err error) {
				assert.ErrorIs(t, err, msgin.ErrNilFunc)
				assert.False(t, msgin.IsPermanent(err), "construction-time ErrNilFunc must stay bare")
				assert.Contains(t, err.Error(), "routing.NewAggregator: nil release strategy")
				assert.Nil(t, agg)
			},
		},
		{
			// WithReleaseWhen(nil) must NOT build its bool->(bool, error)
			// wrapper around the nil fn: a non-nil wrapper would slip past the
			// constructor check and deref the nil at release time instead.
			name: "nil WithReleaseWhen is rejected too (no wrapper is built around the nil)",
			build: func(t *testing.T) (*routing.Aggregator, error) {
				return routing.NewAggregator[int, int](newIntStore(t), validFn,
					routing.WithOutputChannel(&fakeAggChannel{}),
					routing.WithReleaseWhen(nil))
			},
			assert: func(t *testing.T, agg *routing.Aggregator, err error) {
				assert.ErrorIs(t, err, msgin.ErrNilFunc)
				assert.False(t, msgin.IsPermanent(err), "construction-time ErrNilFunc must stay bare")
				assert.Contains(t, err.Error(), "routing.NewAggregator: nil release strategy")
				assert.Nil(t, agg)
			},
		},
		{
			// The default arm of the same check: passing NO strategy option
			// leaves defaultCorrelate/defaultRelease in place, both non-nil, so
			// the new checks must not reject the default configuration.
			name: "no strategy option leaves the non-nil defaults in place",
			build: func(t *testing.T) (*routing.Aggregator, error) {
				return routing.NewAggregator[int, int](newIntStore(t), validFn,
					routing.WithOutputChannel(&fakeAggChannel{}))
			},
			assert: func(t *testing.T, agg *routing.Aggregator, err error) {
				require.NoError(t, err)
				require.NotNil(t, agg)
				// Non-vacuous: drive a message through so the default correlate
				// and release are actually CALLED, not merely stored.
				assert.NoError(t, agg.Handle(t.Context(), corrMsg(1, "m1", "k1", nil)))
			},
		},
		{
			name: "explicit non-nil strategies construct cleanly",
			build: func(t *testing.T) (*routing.Aggregator, error) {
				return routing.NewAggregator[int, int](newIntStore(t), validFn,
					routing.WithOutputChannel(&fakeAggChannel{}),
					routing.WithCorrelationStrategy(func(msgin.Message[any]) (string, error) { return "k", nil }),
					routing.WithReleaseWhen(func(msgin.MessageGroup) bool { return false }))
			},
			assert: func(t *testing.T, agg *routing.Aggregator, err error) {
				require.NoError(t, err)
				require.NotNil(t, agg)
				assert.NoError(t, agg.Handle(t.Context(), corrMsg(1, "m1", "k1", nil)))
			},
		},
		{
			name: "no WithOutputChannel is ErrNilOutput",
			build: func(t *testing.T) (*routing.Aggregator, error) {
				return routing.NewAggregator[int, int](newIntStore(t), validFn)
			},
			assert: func(t *testing.T, agg *routing.Aggregator, err error) {
				assert.ErrorIs(t, err, msgin.ErrNilOutput)
				assert.Nil(t, agg)
			},
		},
		{
			name: "valid construction succeeds",
			build: func(t *testing.T) (*routing.Aggregator, error) {
				return routing.NewAggregator[int, int](newIntStore(t), validFn, routing.WithOutputChannel(&fakeAggChannel{}))
			},
			assert: func(t *testing.T, agg *routing.Aggregator, err error) {
				require.NoError(t, err)
				assert.NotNil(t, agg)
			},
		},
		{
			name: "WithGroupTimeout without WithExpiredGroupChannel is ErrExpiryChannelRequired",
			build: func(t *testing.T) (*routing.Aggregator, error) {
				return routing.NewAggregator[int, int](newIntStore(t), validFn,
					routing.WithOutputChannel(&fakeAggChannel{}),
					routing.WithGroupTimeout(30*time.Second))
			},
			assert: func(t *testing.T, agg *routing.Aggregator, err error) {
				assert.ErrorIs(t, err, msgin.ErrExpiryChannelRequired)
				assert.Nil(t, agg)
			},
		},
		{
			name: "WithGroupTimeout with WithExpiredGroupChannel succeeds",
			build: func(t *testing.T) (*routing.Aggregator, error) {
				return routing.NewAggregator[int, int](newIntStore(t), validFn,
					routing.WithOutputChannel(&fakeAggChannel{}),
					routing.WithGroupTimeout(30*time.Second),
					routing.WithExpiredGroupChannel(&fakeAggChannel{}))
			},
			assert: func(t *testing.T, agg *routing.Aggregator, err error) {
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
				agg, err := routing.NewAggregator[int, int](store, sumFn, routing.WithOutputChannel(out))
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
				agg, err := routing.NewAggregator[int, int](store, sumFn, routing.WithOutputChannel(out))
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
				agg, err := routing.NewAggregator[int, int](store, sumFn, routing.WithOutputChannel(out))
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
				agg, err := routing.NewAggregator[int, int](store, sumFn,
					routing.WithOutputChannel(out), routing.WithCompletionSize(2))
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
				agg, err := routing.NewAggregator[int, int](
					store, sumFn,
					routing.WithOutputChannel(out),
					routing.WithCorrelationStrategy(func(m msgin.Message[any]) (string, error) {
						v, ok := m.Header(orderKey)
						if !ok {
							return "", msgin.Permanent(msgin.ErrNoCorrelation)
						}
						return v.(string), nil
					}),
					routing.WithCompletionSize(2),
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
				agg, err := routing.NewAggregator[int, int](
					store, sumFn,
					routing.WithOutputChannel(out),
					routing.WithReleaseWhen(func(g msgin.MessageGroup) bool {
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
				agg, err := routing.NewAggregator[int, int](
					store, sumFn,
					routing.WithOutputChannel(out),
					routing.WithReleaseWhen(func(g msgin.MessageGroup) bool {
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
				agg, err := routing.NewAggregator[int, int](store, sumFn, routing.WithOutputChannel(out))
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
				agg, err := routing.NewAggregator[int, int](store, sumFn, routing.WithOutputChannel(out))
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
				agg, err := routing.NewAggregator[int, int](store, sumFn, routing.WithOutputChannel(out))
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
				agg, err := routing.NewAggregator[int, int](store, sumFn, routing.WithOutputChannel(out))
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
				agg, err := routing.NewAggregator[int, int](store,
					func(context.Context, []msgin.Message[int]) (msgin.Message[int], error) {
						return msgin.Message[int]{}, errBoom
					},
					routing.WithOutputChannel(out), routing.WithCompletionSize(1))
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
				agg, err := routing.NewAggregator[int, int](store, sumFn,
					routing.WithOutputChannel(out), routing.WithCompletionSize(1))
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
				agg, err := routing.NewAggregator[int, int](store,
					func(context.Context, []msgin.Message[int]) (msgin.Message[int], error) {
						called = true
						return msgin.New(0), nil
					},
					routing.WithOutputChannel(out))
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
				agg, err := routing.NewAggregator[int, int](store, sumFn,
					routing.WithOutputChannel(out), routing.WithCompletionSize(1))
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
				agg, err := routing.NewAggregator[int, int](store, sumFn,
					routing.WithOutputChannel(out), routing.WithCompletionSize(1))
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
				agg, err := routing.NewAggregator[int, int](store, sumFn,
					routing.WithOutputChannel(out), routing.WithCompletionSize(1))
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

		// ---- Spec 017 §3.3a / Plan 031 Task 1: store.Add returned an error
		// TOGETHER WITH a live snapshot. Six exits, one case each. ----
		{
			// B1-11 + B1-12 / AC-1b — THE case the branch exists for, and the
			// only one that catches the deadlock the member cap would
			// otherwise introduce. The fixture must be ID-LESS: with an id the
			// dedup branch returns a NIL error, Handle reaches the predicate
			// anyway and the deadlock is never entered.
			name: "over cap: an id-less redelivery re-fires the release and the retry is admitted",
			assert: func(t *testing.T) {
				out := &failOnceChannel{err: errors.New("send boom")}
				agg := cappedAgg(t, 4, out, releaseAt(4), nil)

				idless := msgin.NewMessage[any](1, msgin.Headers{})
				require.Empty(t, idless.ID(),
					"the fixture must be id-LESS: msgin.New always stamps an id (message.go's NewID fallback)")

				// 1-3: held. 4th: the release fires and its Send FAILS, so the
				// claim is abandoned and the group is left complete, unleased
				// and with nothing to re-trigger it.
				for range 3 {
					require.NoError(t, agg.Handle(t.Context(), idless))
				}
				require.Error(t, agg.Handle(t.Context(), idless))
				require.Equal(t, 0, out.sentCount())

				// The redelivery is REJECTED by the cap — and the rejection
				// re-evaluates the release instead of dead-ending.
				err := agg.Handle(t.Context(), idless)
				require.ErrorIs(t, err, msgin.ErrOverflowDropped)
				assert.False(t, msgin.IsPermanent(err),
					"the group provably just drained, so the retry provably succeeds")
				assert.Contains(t, err.Error(), "routing.Aggregator.Handle")
				assert.Equal(t, 1, out.sentCount(), "the release re-fired and succeeded")

				// And the retry IS admitted — an error was still returned
				// above precisely so the source would redeliver.
				require.NoError(t, agg.Handle(t.Context(), idless))
			},
		},
		{
			// B1-13 / exit 2a — the strategy DECLINED, so nothing will drain
			// the group and the store's permanent classification stands.
			name: "over cap: an unreleasable group keeps the store's Permanent classification",
			assert: func(t *testing.T) {
				out := &fakeAggChannel{}
				never := func(msgin.MessageGroup) (bool, error) { return false, nil }
				agg := cappedAgg(t, 4, out, never, nil)

				for i := range 4 {
					require.NoError(t, agg.Handle(t.Context(), corrMsg(i, "m"+strconv.Itoa(i), "k", nil)))
				}
				err := agg.Handle(t.Context(), corrMsg(9, "m9", "k", nil))
				require.ErrorIs(t, err, msgin.ErrOverflowDropped)
				assert.True(t, msgin.IsPermanent(err), "no drainage happened, so no downgrade is earned")
				assert.Equal(t, 0, out.count())
			},
		},
		{
			// B1-13b / exit 2b — the strategy FAILED. Two things are asserted
			// here and they are independent:
			//
			//  1. A strategy's error is still not a "yes": a mutant dropping
			//     `rerr != nil` claim-and-releases a group the strategy just
			//     rejected, so out.count() must stay 0.
			//  2. 🔴 The error is JOINED, not swallowed (D-AW / Spec 017 §3.3b,
			//     finding R-10). This line asserted NotErrorIs until
			//     2026-08-25: the branch merged "declined" with "failed" and
			//     discarded rerr, so a fault in the CALLER's own strategy
			//     reached the operator as the store's cap rejection while the
			//     success path 25 lines below propagated it. That is a
			//     debuggability defect, not a contract; the assertion was
			//     REVERSED deliberately and must not be restored. errors.Is
			//     traverses join trees, so the store's ErrOverflowDropped
			//     classification (asserted above) survives the join unchanged.
			name: "over cap: a release-strategy failure is JOINED with the store's error, not swallowed",
			assert: func(t *testing.T) {
				out := &fakeAggChannel{}
				strategyErr := errors.New("strategy boom")
				agg := cappedAgg(t, 4, out, declineThenRelease(4, strategyErr), nil)

				for i := range 4 {
					require.NoError(t, agg.Handle(t.Context(), corrMsg(i, "m"+strconv.Itoa(i), "k", nil)))
				}
				err := agg.Handle(t.Context(), corrMsg(9, "m9", "k", nil))
				require.ErrorIs(t, err, msgin.ErrOverflowDropped)
				assert.True(t, msgin.IsPermanent(err))
				assert.ErrorIs(t, err, strategyErr,
					"D-AW: both causes must reach the operator — see Spec 017 §3.3b before flipping this back")
				assert.Equal(t, 0, out.count(), "a strategy error must NOT claim-and-release the group")
			},
		},
		{
			// 🔴 D-AW's sub-decision, asserted rather than merely tolerated:
			// joining ESCALATES classification. msgin.IsPermanent uses
			// errors.As, which walks Unwrap() []error, so a Permanent-marked
			// rerr makes the JOINED error permanent even though the store's
			// arm here is the LIVE-LEASE one, which is deliberately transient.
			//
			// An escalation nobody asserts is an accident waiting to be
			// "fixed" by the next reader (Spec 017 §3.3b; ADR 0033 D-AW).
			name: "over cap: a Permanent strategy error escalates a TRANSIENT live-lease overflow",
			assert: func(t *testing.T) {
				store := cappedStore(t, memberCapFixture)
				out := &fakeAggChannel{}
				strategyErr := errors.New("strategy boom")
				agg := cappedAggOpts(t, store, out,
					routing.WithReleaseStrategy(declineThenRelease(memberCapFixture, msgin.Permanent(strategyErr))))

				err := driveToLeasedOverflow(t, store, agg, 2)
				require.ErrorIs(t, err, msgin.ErrOverflowDropped)
				require.ErrorIs(t, err, strategyErr)
				assert.True(t, msgin.IsPermanent(err),
					"the Permanent rerr must reach IsPermanent THROUGH the join")
				assert.Equal(t, 0, out.count())
			},
		},
		{
			// The control for the row above: the SAME fixture with an unmarked
			// strategy error stays transient. Without this pair the escalation
			// row proves nothing — the store's own arm could have been the
			// permanent one all along.
			name: "over cap: an unmarked strategy error leaves a transient live-lease overflow transient",
			assert: func(t *testing.T) {
				store := cappedStore(t, memberCapFixture)
				out := &fakeAggChannel{}
				strategyErr := errors.New("strategy boom")
				agg := cappedAggOpts(t, store, out,
					routing.WithReleaseStrategy(declineThenRelease(memberCapFixture, strategyErr)))

				err := driveToLeasedOverflow(t, store, agg, 2)
				require.ErrorIs(t, err, msgin.ErrOverflowDropped)
				require.ErrorIs(t, err, strategyErr)
				assert.False(t, msgin.IsPermanent(err),
					"a claim is in flight, so the store's arm is transient and nothing escalates it")
				assert.Equal(t, 0, out.count())
			},
		},
		{
			// B1-13c / exit 3 — ClaimGroup failed. The store fault is returned
			// VERBATIM, deliberately discarding the overflow classification:
			// masking a store fault behind ErrOverflowDropped would point an
			// operator at the cap. Note it is thereby TRANSIENT (unmarked).
			name: "over cap: a ClaimGroup failure is returned instead of the overflow error",
			assert: func(t *testing.T) {
				out := &fakeAggChannel{}
				claimErr := errors.New("claim boom")
				agg := cappedAgg(t, 4, out, declineThenRelease(4, nil),
					func(s msgin.MessageGroupStore) msgin.MessageGroupStore {
						return &failNthClaimStore{MessageGroupStore: s, n: 1, err: claimErr}
					})

				for i := range 4 {
					require.NoError(t, agg.Handle(t.Context(), corrMsg(i, "m"+strconv.Itoa(i), "k", nil)))
				}
				err := agg.Handle(t.Context(), corrMsg(9, "m9", "k", nil))
				require.ErrorIs(t, err, claimErr)
				assert.NotErrorIs(t, err, msgin.ErrOverflowDropped)
				assert.False(t, msgin.IsPermanent(err))
			},
		},
		{
			// B1-13d / exit 4 — another holder is releasing the group. The
			// success path returns NIL for this exact condition
			// (aggregator.go's "another Handle/process is releasing this
			// group; held"); here the member was never stored, so nil would
			// Ack an unstored message. The divergence is deliberate.
			name: "over cap: a claim taken by another holder is a transient overflow, never nil",
			assert: func(t *testing.T) {
				out := &fakeAggChannel{}
				agg := cappedAgg(t, 4, out, declineThenRelease(4, nil),
					func(s msgin.MessageGroupStore) msgin.MessageGroupStore {
						return &nilClaimStore{MessageGroupStore: s}
					})

				for i := range 4 {
					require.NoError(t, agg.Handle(t.Context(), corrMsg(i, "m"+strconv.Itoa(i), "k", nil)))
				}
				err := agg.Handle(t.Context(), corrMsg(9, "m9", "k", nil))
				require.ErrorIs(t, err, msgin.ErrOverflowDropped)
				assert.False(t, msgin.IsPermanent(err))
				assert.Contains(t, err.Error(), "routing.Aggregator.Handle")
			},
		},
		{
			// B1-13e / exit 5 — the release itself failed. The Nack must name
			// the OUTPUT CHANNEL, not the cap, or an operator debugging a full
			// group is pointed at the wrong subsystem.
			name: "over cap: a release failure returns the release error, not the overflow error",
			assert: func(t *testing.T) {
				sendErr := errors.New("send boom")
				out := &fakeAggChannel{sendErr: sendErr}
				agg := cappedAgg(t, 4, out, declineThenRelease(4, nil), nil)

				for i := range 4 {
					require.NoError(t, agg.Handle(t.Context(), corrMsg(i, "m"+strconv.Itoa(i), "k", nil)))
				}
				err := agg.Handle(t.Context(), corrMsg(9, "m9", "k", nil))
				require.ErrorIs(t, err, sendErr)
				assert.NotErrorIs(t, err, msgin.ErrOverflowDropped)
			},
		},
		{
			// B1-14 / exit 1 — the compatibility arm. A store returning
			// (nil, err) — every third-party store shipped before this
			// increment — keeps the old path EXACTLY. A mutant dropping the
			// nil guard nil-derefs here.
			name: "a store error without a snapshot is returned verbatim",
			assert: func(t *testing.T) {
				addErr := errors.New("add boom")
				out := &fakeAggChannel{}
				agg, err := routing.NewAggregator[int, int](&failingAddStore{addErr: addErr}, sumFn,
					routing.WithOutputChannel(out),
					routing.WithCorrelationStrategy(fixedKey))
				require.NoError(t, err)

				err = agg.Handle(t.Context(), msgin.New[any](1))
				require.ErrorIs(t, err, addErr)
				assert.Equal(t, 0, out.count())
			},
		},

		// ---- Plan 031 review findings R-3 / R-4: what may ENTER the branch
		// above, and what the release strategy may be handed once inside. ----
		{
			// R-3, primary — and the row that pins the SENTINEL gate on its
			// own. The snapshot here holds a member, so gates 2 and 3
			// (typed-nil, zero-member) both pass it through; only the
			// errors.Is(ErrOverflowDropped) gate can stop it. A mutant
			// reverting that gate to `group == nil` claims, aggregates, emits
			// and settles a group over an unrelated store fault, and this row
			// is what catches it.
			name: "a NON-overflow store error with a POPULATED snapshot is returned verbatim, never released",
			assert: func(t *testing.T) {
				decodeErr := errors.New("decode boom")
				populated := valueGroup{key: "k", msgs: []msgin.Message[any]{msgin.New[any](7)}}

				calls, out, err := handleScriptedAdd(t, populated, decodeErr)
				require.ErrorIs(t, err, decodeErr)
				assert.NotErrorIs(t, err, msgin.ErrOverflowDropped,
					"a decode fault must not be reported as — or replaced by — a cap rejection")
				assert.Zero(t, calls, "the release strategy must never see a non-overflow fault's snapshot")
				assert.Equal(t, 0, out.count())
			},
		},
		{
			// R-3's PROVEN shape, kept alongside the row above because it is
			// the one measured in the field: sql.GroupStore.Add ends `return
			// s.decodeGroupRows(rows)`, which yields a ZERO-VALUE
			// sql.groupSnapshot — implicitly converted to a NON-nil
			// msgin.MessageGroup — beside a header-decode error.
			name: "a NON-overflow store error with a zero-value snapshot is returned verbatim, never released",
			assert: func(t *testing.T) {
				decodeErr := errors.New("decode boom")

				calls, out, err := handleScriptedAdd(t, valueGroup{key: "k"}, decodeErr)
				require.ErrorIs(t, err, decodeErr)
				assert.NotErrorIs(t, err, msgin.ErrOverflowDropped)
				assert.Zero(t, calls)
				assert.Equal(t, 0, out.count())
			},
		},
		{
			// R-3, secondary. `group == nil` is an INTERFACE-nil test, so a
			// typed-nil snapshot walks straight past it and nil-derefs on the
			// first method call. Handle must treat a typed nil exactly as it
			// treats an untyped one.
			name: "over cap: a TYPED-NIL snapshot is returned verbatim, never dereferenced",
			assert: func(t *testing.T) {
				overErr := fmt.Errorf("%w: fake.GroupStore.Add: group %q is full", msgin.ErrOverflowDropped, "k")
				var typedNil msgin.MessageGroup = (*ptrGroup)(nil) //nolint:staticcheck // SA4023: see below
				// staticcheck's SA4023 ("this comparison is always true") is not a
				// defect here — it is the PROOF, delivered statically, that a typed
				// nil in an interface is != nil and so walks past a bare == nil
				// guard. testify's NotNil reports the opposite (it unwraps with
				// reflection), which is why the raw comparison is used.
				require.True(t, typedNil != nil, //nolint:staticcheck // SA4023: "always true" IS the property under test
					"the fixture is only meaningful while a typed nil is `!= nil`")

				calls, out, err := handleScriptedAdd(t, typedNil, overErr)
				require.ErrorIs(t, err, overErr)
				assert.Zero(t, calls, "a typed-nil snapshot must not reach the release strategy")
				assert.Equal(t, 0, out.count())
			},
		},
		{
			// R-4. When every member is claimed, memory returns
			// msgs[claimedLen:] — an EMPTY live snapshot — beside the
			// rejection. A release strategy that indexes Messages()[0] is a
			// shape no godoc forbids (defaultRelease does it, modulo a len == 0
			// guard), and inside a Consumer the resulting panic is recovered as
			// ErrHandlerPanic, which IsPermanent deliberately EXCLUDES — so it
			// would be retried forever against the same claimed group.
			//
			// Handle therefore skips the release attempt outright: an empty
			// residual means the claim holder is already draining the group, so
			// there is nothing to release and the store's transient
			// classification is exactly right.
			name: "over cap: a ZERO-MEMBER live snapshot skips the release attempt",
			assert: func(t *testing.T) {
				store := cappedStore(t, memberCapFixture)
				out := &fakeAggChannel{}
				calls := 0
				indexFirst := func(g msgin.MessageGroup) (bool, error) {
					calls++
					_, ok := g.Messages()[0].Header("never-set") // panics on an empty group
					return ok, nil
				}
				agg := cappedAggOpts(t, store, out, routing.WithReleaseStrategy(indexFirst))

				err := driveToLeasedOverflow(t, store, agg, memberCapFixture)
				require.ErrorIs(t, err, msgin.ErrOverflowDropped)
				assert.Equal(t, memberCapFixture, calls,
					"the strategy saw the four non-empty snapshots and must NOT have seen the empty one")
				assert.False(t, msgin.IsPermanent(err), "a claim is in flight: the store's arm is transient")
				assert.Contains(t, err.Error(), "memory.GroupStore.Add",
					"the store's error is returned UNCHANGED")
				assert.NotContains(t, err.Error(), "routing.Aggregator.Handle",
					"nothing drained, so no fresh overflowRetryable may be minted")
				assert.Equal(t, 0, out.count())
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
func runAndJoin(t *testing.T, agg *routing.Aggregator, ctx context.Context, cancel context.CancelFunc) {
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
				agg, err := routing.NewAggregator[int, int](store, sumFn, routing.WithOutputChannel(out))
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
				agg, err := routing.NewAggregator[int, int](
					store, sumFn,
					routing.WithOutputChannel(out),
					routing.WithGroupTimeout(30*time.Second),
					routing.WithExpiredGroupChannel(expired),
					routing.WithAggregatorClock(clock),
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
				agg, err := routing.NewAggregator[int, int](
					store, sumFn,
					routing.WithOutputChannel(out),
					routing.WithGroupTimeout(30*time.Second),
					routing.WithExpiredGroupChannel(expired),
					routing.WithAggregatorClock(clock),
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
				agg, err := routing.NewAggregator[int, int](
					store, sumFn,
					routing.WithOutputChannel(out),
					routing.WithGroupTimeout(30*time.Second),
					routing.WithExpiredGroupChannel(expired),
					routing.WithAggregatorClock(clock),
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
				agg, err := routing.NewAggregator[int, int](
					store, sumFn,
					routing.WithOutputChannel(out),
					routing.WithGroupTimeout(30*time.Second),
					routing.WithExpiredGroupChannel(expired),
					routing.WithAggregatorClock(clock),
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
				agg, err := routing.NewAggregator[int, int](
					store, sumFn,
					routing.WithOutputChannel(out),
					routing.WithGroupTimeout(30*time.Second),
					routing.WithExpiredGroupChannel(expired),
					routing.WithAggregatorClock(clock),
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
				agg, err := routing.NewAggregator[int, int](
					store, sumFn,
					routing.WithOutputChannel(out),
					routing.WithGroupTimeout(100*time.Second), // far larger than the store's 10s RecoverInterval
					routing.WithExpiredGroupChannel(expired),
					routing.WithAggregatorClock(clock),
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
				agg, err := routing.NewAggregator[int, int](
					base, sumFn,
					routing.WithOutputChannel(out),
					routing.WithCompletionSize(2),
					routing.WithGroupTimeout(30*time.Second),
					routing.WithExpiredGroupChannel(expired),
					routing.WithAggregatorClock(clock),
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
				agg, err := routing.NewAggregator[int, int](
					base, sumFn,
					routing.WithOutputChannel(out),
					routing.WithGroupTimeout(30*time.Second),
					routing.WithExpiredGroupChannel(expired),
					routing.WithAggregatorClock(clock),
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

// requireQtyRelease is a fallible ReleaseStrategy: it errors on any group
// whose first member carries no "qty" header, so a group of such messages can
// never complete via the normal release check. It replaces the
// `messages[0].header("qty") > 0` expression these cases used before the
// expr-backed options left the core, and drives the same branches — the
// release check's error return is what makes them reachable.
func requireQtyRelease(min int) routing.ReleaseStrategy {
	return func(g msgin.MessageGroup) (bool, error) {
		msgs := g.Messages()
		if len(msgs) == 0 {
			return false, nil
		}
		qty, ok := msgs[0].Headers().Int("qty")
		if !ok {
			return false, errors.New("no usable qty header on the group's first member")
		}
		return len(msgs) >= min && qty > 0, nil
	}
}

// TestAggregator_ReleaseErrorReaperFallThrough covers reapGroup's H-1
// fall-through: a release strategy that ALWAYS errors on this group's data (no
// "qty" header on its only member) means the group can never complete via the
// normal release check. Paired with WithGroupTimeout, one reaper tick after
// the group ages past the timeout must route it to the expired sink instead
// of busy-spinning claim->error->abandon forever.
func TestAggregator_ReleaseErrorReaperFallThrough(t *testing.T) {
	defer goleak.VerifyNone(t)

	clock := clockwork.NewFakeClock()
	store, err := memory.NewGroupStore(memory.WithGroupClock(clock))
	require.NoError(t, err)
	out := &fakeAggChannel{}
	expired := &fakeAggChannel{}
	agg, err := routing.NewAggregator[int, int](
		store, sumFn,
		routing.WithOutputChannel(out),
		routing.WithReleaseStrategy(requireQtyRelease(1)),
		routing.WithGroupTimeout(30*time.Second),
		routing.WithExpiredGroupChannel(expired),
		routing.WithAggregatorClock(clock),
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

// TestAggregator_ReleaseErrorDrainCheckError covers release's drain-loop
// residual release-check error branch (H-2), reachable because the release
// decision itself can error (unlike WithReleaseStrategy's bool-only wrapper,
// which never can): the main group {m1,m2} releases normally, but a
// residual {m3,m4} formed during its lease has no "qty" header on its first
// member, so the residual's release check errors. Handle must still return
// nil (the main member already settled) and the residual must be left live
// for the reaper/a later Add, not lost.
func TestAggregator_ReleaseErrorDrainCheckError(t *testing.T) {
	base := newIntStore(t)
	settling := make(chan struct{})
	unblock := make(chan struct{})
	store := &blockingGroupStore{MessageGroupStore: base, settling: settling, unblock: unblock}

	out := &fakeAggChannel{}
	agg, err := routing.NewAggregator[int, int](store, sumFn,
		routing.WithOutputChannel(out),
		routing.WithReleaseStrategy(requireQtyRelease(2)))
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

// TestAggregator_ReleaseErrorDrainSendError covers release's drain-loop
// residual releaseOnce-failure branch (H-3) via a Send error rather than the
// Settle-error flavor TestAggregator_ReleaseDrainLoopReleaseError already
// covers: the main group {m1,m2} releases and sends fine, but the residual
// {m3,m4}'s Send (the drain loop's re-release) fails. Handle must still
// return nil and the residual must be left live for a retry.
func TestAggregator_ReleaseErrorDrainSendError(t *testing.T) {
	base := newIntStore(t)
	settling := make(chan struct{})
	unblock := make(chan struct{})
	store := &blockingGroupStore{MessageGroupStore: base, settling: settling, unblock: unblock}

	sendErr := errors.New("send boom")
	out := &failNthChannel{failFrom: 2, err: sendErr}
	agg, err := routing.NewAggregator[int, int](store, sumFn,
		routing.WithOutputChannel(out),
		routing.WithReleaseWhen(func(g msgin.MessageGroup) bool { return len(g.Messages()) >= 2 }))
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
	agg, err := routing.NewAggregator[int, int](store, sumFn,
		routing.WithOutputChannel(out), routing.WithCompletionSize(2))
	require.NoError(t, err)

	m1 := corrMsg(1, "m1", "g", nil)
	m2 := corrMsg(2, "m2", "g", nil)
	require.NoError(t, agg.Handle(t.Context(), m1))
	err = agg.Handle(t.Context(), m2)
	assert.NoError(t, err, "the drain loop's transient ClaimGroup error must be swallowed, not propagated")
	require.Equal(t, 1, out.count())
	assert.Equal(t, 3, out.last().Payload())
}

// TestNewAggregator_NilOptionElement proves a nil ELEMENT of opts (as opposed
// to a nil options SLICE, which is a normal zero-option call) is a BARE
// ErrNilFunc naming the computed 0-based index, not a panic (Spec 015 §3.1,
// family R1; ADR 0031 D-P/D-Q/D-R). store and fn are both valid here — this is
// about the THIRD argument, opts, never about the two already covered by
// TestNewAggregator_Validation.
func TestNewAggregator_NilOptionElement(t *testing.T) {
	tests := []struct {
		name   string
		opts   []routing.AggregatorOption
		assert func(t *testing.T, err error)
	}{
		{
			name: "nil element alone",
			opts: []routing.AggregatorOption{nil},
			assert: func(t *testing.T, err error) {
				require.ErrorIs(t, err, msgin.ErrNilFunc)
				assert.False(t, msgin.IsPermanent(err), "R1 nil-option error must stay bare")
				assert.Contains(t, err.Error(), "routing.NewAggregator: nil option at index 0")
			},
		},
		{
			name: "nil element after a valid option asserts the COMPUTED index and the FULL position",
			opts: []routing.AggregatorOption{routing.WithOutputChannel(&fakeAggChannel{}), nil},
			assert: func(t *testing.T, err error) {
				require.ErrorIs(t, err, msgin.ErrNilFunc)
				assert.Contains(t, err.Error(), "routing.NewAggregator: nil option at index 1")
			},
		},
		{
			name: "first of two nils wins",
			opts: []routing.AggregatorOption{nil, nil},
			assert: func(t *testing.T, err error) {
				require.ErrorIs(t, err, msgin.ErrNilFunc)
				assert.Contains(t, err.Error(), "routing.NewAggregator: nil option at index 0")
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			agg, err := routing.NewAggregator[int, int](newIntStore(t), sumFn, tc.opts...)
			assert.Nil(t, agg)
			tc.assert(t, err)
		})
	}
}

// TestNewAggregator_NilOptionElement_ValidateFirst proves NewAggregator is
// VALIDATE-FIRST (Spec 015 §3.5): its existing store/fn argument checks
// precede the option loop, so an earlier-validated fault wins over a nil
// option sitting at the SAME index 0 the nil-option guard would otherwise
// report. Standalone rather than folded into the table above because its setup
// (a nil store) is a different shape than "store/fn valid, an option is nil" —
// the table-test skill's documented divergence exception.
func TestNewAggregator_NilOptionElement_ValidateFirst(t *testing.T) {
	_, err := routing.NewAggregator[int, int](nil, sumFn, nil)

	require.ErrorIs(t, err, msgin.ErrNilStore)
	assert.NotErrorIs(t, err, msgin.ErrNilFunc)
}

// ---------------------------------------------------------------------------
// Spec 017 §6 AC-1 / AC-3 — Plan 031 Task 2. The store's member cap bounds
// EVERY release path, not merely the one routing already had a ceiling on.
// ---------------------------------------------------------------------------

// memberCapFixture is the cap every case below builds its store with. It is
// deliberately tiny: Spec 017 §6 AC-6 forbids growing a group toward any
// ceiling, because memory.GroupStore.Add clones the live member slice per call
// and reaching 65,536 costs a measured 8.6s and 48.3 GiB of churn (Spec 016
// §1.4).
const memberCapFixture = 4

// requireOverCapPermanent asserts the FULL over-cap render of the memory store
// at memberCapFixture (Spec 017 §6 AC-2c) together with its Permanent
// classification.
//
// The render doubles as the fixture's OWN proof, which is why these cases
// assert the string rather than settling for errors.Is: "holds 4 members,
// limit 4" can only be produced by a group that actually reached exactly the
// cap, and the "msgin: permanent: " prefix only by an UNLEASED one. A case
// that quietly released early, or that was still under lease when the cap+1-th
// member arrived, cannot reach this assertion — so a fixture that fails to
// arrive at the state its name claims fails here rather than passing vacuously.
func requireOverCapPermanent(t *testing.T, err error) {
	t.Helper()
	require.ErrorIs(t, err, msgin.ErrOverflowDropped)
	assert.True(t, msgin.IsPermanent(err),
		"an unleased full group cannot drain itself, so a transient rejection would hot-spin under the shipped zero-value RetryPolicy")
	assert.EqualError(t, err,
		`msgin: permanent: msgin: message dropped by overflow policy: memory.GroupStore.Add: group "k" holds 4 members, limit 4`)
}

// TestAggregator_MemberCapHoldsForEveryReleasePath is Spec 017 §6 AC-1, and it
// is the reason Plan 031 exists.
//
// routing.WithCompletionSize's ceiling bounded exactly ONE of the Aggregator's
// four release paths, because it is gated on a config field only that option
// writes. The other three bypass it entirely: WithReleaseStrategy and
// WithReleaseWhen install caller-supplied closures the library never inspects,
// and the default strategy reads its threshold from the HeaderSequenceSize
// HEADER — i.e. from DATA, which no compile-time ceiling can constrain. A test
// that exercised only WithCompletionSize would therefore pass against an
// implementation that bounds nothing new.
//
// Each path contributes TWO rows, and the second is not padding. A "never
// releases" row cannot, on its own, distinguish the path it names from a
// fixture in which that path was never installed at all: an option that
// silently failed to apply, or a HeaderSequenceSize stamped under the wrong
// key, leaves routing's default strategy reading a header that is not there,
// which also never fires — and the row passes for entirely the wrong reason.
// The paired CONTROL row configures the same path to fire WITHIN the cap and
// asserts it does, which is what makes its partner a test of that path rather
// than of a typo.
//
// The IsPermanent half inside requireOverCapPermanent is not decoration
// either: without the Permanent wrap every over-cap row still passes against a
// store that rejects the member and then hot-spins forever on redelivery.
func TestAggregator_MemberCapHoldsForEveryReleasePath(t *testing.T) {
	t.Parallel()

	type testCase struct {
		name string
		opts []routing.AggregatorOption
		// firstHeaders is stamped on the group's FIRST member only, because
		// routing's default release strategy reads HeaderSequenceSize from
		// msgs[0] and nowhere else.
		firstHeaders map[string]any
		assert       func(t *testing.T, err error, released int)
	}

	neverReleases := func(t *testing.T, err error, released int) {
		t.Helper()
		requireOverCapPermanent(t, err)
		assert.Equal(t, 0, released,
			"this release path never fires, so the group can only grow — which is precisely why the bound must live at the store")
	}

	// firesWithinTheCap is the control assertion: the 4th member satisfies the
	// path, so the group releases and drains, and the 5th member starts a
	// fresh group instead of being refused.
	firesWithinTheCap := func(t *testing.T, err error, released int) {
		t.Helper()
		require.NoError(t, err,
			"the group released at the cap and drained, so the next member starts a fresh group")
		assert.Equal(t, 1, released,
			"this release path IS installed and IS read — which is what makes its over-cap twin meaningful")
	}

	tests := []testCase{
		{
			name:   "WithCompletionSize above the cap: the cap+1-th member is refused",
			opts:   []routing.AggregatorOption{routing.WithCompletionSize(1000)},
			assert: neverReleases,
		},
		{
			name:   "WithCompletionSize within the cap: it fires (fixture control)",
			opts:   []routing.AggregatorOption{routing.WithCompletionSize(memberCapFixture)},
			assert: firesWithinTheCap,
		},
		{
			name: "WithReleaseStrategy that never releases: the cap+1-th member is refused",
			opts: []routing.AggregatorOption{
				routing.WithReleaseStrategy(func(msgin.MessageGroup) (bool, error) { return false, nil }),
			},
			assert: neverReleases,
		},
		{
			name:   "WithReleaseStrategy that releases within the cap: it fires (fixture control)",
			opts:   []routing.AggregatorOption{routing.WithReleaseStrategy(releaseAt(memberCapFixture))},
			assert: firesWithinTheCap,
		},
		{
			name: "WithReleaseWhen that never releases: the cap+1-th member is refused",
			opts: []routing.AggregatorOption{
				routing.WithReleaseWhen(func(msgin.MessageGroup) bool { return false }),
			},
			assert: neverReleases,
		},
		{
			name: "WithReleaseWhen that releases within the cap: it fires (fixture control)",
			opts: []routing.AggregatorOption{
				routing.WithReleaseWhen(func(g msgin.MessageGroup) bool { return len(g.Messages()) >= memberCapFixture }),
			},
			assert: firesWithinTheCap,
		},
		{
			// No release option at all: routing's default strategy, whose
			// threshold is a header value. This is the path whose bound cannot
			// be expressed as a constructor ceiling under any design, because
			// the threshold is DATA that arrives at runtime.
			name:         "the default header-driven path above the cap: the cap+1-th member is refused",
			firstHeaders: map[string]any{msgin.HeaderSequenceSize: 1000},
			assert:       neverReleases,
		},
		{
			name:         "the default header-driven path within the cap: it fires (fixture control)",
			firstHeaders: map[string]any{msgin.HeaderSequenceSize: memberCapFixture},
			assert:       firesWithinTheCap,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			out := &fakeAggChannel{}
			agg := cappedAggOpts(t, cappedStore(t, memberCapFixture), out, tc.opts...)

			mk := func(i int) msgin.Message[any] {
				h := map[string]any{}
				if i == 0 {
					for k, v := range tc.firstHeaders {
						h[k] = v
					}
				}
				return msgin.New[any](i, msgin.WithID("m"+strconv.Itoa(i)), msgin.WithHeaders(h))
			}

			for i := range memberCapFixture {
				require.NoError(t, agg.Handle(t.Context(), mk(i)),
					"member %d sits below the cap and must be admitted", i)
			}
			tc.assert(t, agg.Handle(t.Context(), mk(memberCapFixture)), out.count())
		})
	}
}

// TestAggregator_MemberCapCompletionSizeBoundary is Spec 017 §6 AC-3.1: the
// off-by-one between the store's member cap and the Aggregator's completion
// size, pinned in BOTH directions at small n.
//
// The two rows are the two sides of one boundary. At completionSize == cap the
// completing member is the LAST one the store admits, so the release fires and
// the group drains; at completionSize == cap+1 the completing member is the
// first one the store refuses, so the group can never complete and the
// rejection is permanent. Together they fix the arithmetic — an implementation
// off by one in either direction fails exactly one row.
func TestAggregator_MemberCapCompletionSizeBoundary(t *testing.T) {
	t.Parallel()

	type testCase struct {
		name           string
		completionSize int
		assert         func(t *testing.T, errs []error, out *fakeAggChannel)
	}

	tests := []testCase{
		{
			name:           "completion size EQUAL to the cap releases at the cap",
			completionSize: memberCapFixture,
			assert: func(t *testing.T, errs []error, out *fakeAggChannel) {
				for i, err := range errs {
					require.NoError(t, err, "message %d", i)
				}
				require.Equal(t, 1, out.count(), "the 4th member completes the group")
				assert.Equal(t, 0+1+2+3, out.last().Payload(),
					"the released aggregate is the first four payloads, so the group released at exactly the cap")
				// The 5th message returning NIL is itself the proof that a
				// FRESH group was started: had the release not fired, the
				// group would still hold 4 members at a cap of 4 and the 5th
				// would have been refused at the door.
			},
		},
		{
			name:           "completion size ONE ABOVE the cap can never release",
			completionSize: memberCapFixture + 1,
			assert: func(t *testing.T, errs []error, out *fakeAggChannel) {
				for i, err := range errs[:memberCapFixture] {
					require.NoError(t, err, "message %d", i)
				}
				requireOverCapPermanent(t, errs[memberCapFixture])
				assert.Equal(t, 0, out.count(),
					"nothing may be released: the member that would have completed the group is the one the store refuses")
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			out := &fakeAggChannel{}
			agg := cappedAggOpts(t, cappedStore(t, memberCapFixture), out,
				routing.WithCompletionSize(tc.completionSize))

			errs := make([]error, memberCapFixture+1)
			for i := range errs {
				errs[i] = agg.Handle(t.Context(), msgin.New[any](i, msgin.WithID("m"+strconv.Itoa(i))))
			}
			tc.assert(t, errs, out)
		})
	}
}

// TestAggregator_CeilingLevelCompletionSizeConstructs is Spec 017 §6 AC-3.2:
// the two endpoints of the "default member cap >= completionSizeCeiling"
// invariant, exercised as CONSTRUCTION ONLY.
//
// A caller may legally set routing.WithCompletionSize to its ceiling (65,536),
// and the store's member cap must not then refuse that group's completing
// member — a smaller cap would turn a documented configuration into a silent
// deadlock. Both the store's DEFAULT cap and an explicit cap at that same
// value must therefore pair with a ceiling-level completion size.
//
// No members are added: growing one group to 65,536 costs a measured 8.6s and
// 48.3 GiB of allocation churn (Spec 016 §1.4), and Spec 017 §6 AC-6 forbids
// it outright. The invariant ITSELF is enforced mechanically over the
// declarations, by Plan 031 Task 3's AST test; this pins the pairings that
// invariant exists to protect.
func TestAggregator_CeilingLevelCompletionSizeConstructs(t *testing.T) {
	t.Parallel()

	type testCase struct {
		name   string
		store  func(t *testing.T) *memory.GroupStore
		assert func(t *testing.T, agg *routing.Aggregator, err error)
	}

	constructs := func(t *testing.T, agg *routing.Aggregator, err error) {
		t.Helper()
		require.NoError(t, err)
		assert.NotNil(t, agg)
	}

	tests := []testCase{
		{
			name: "the DEFAULT member cap pairs with a ceiling-level completion size",
			store: func(t *testing.T) *memory.GroupStore {
				t.Helper()
				s, err := memory.NewGroupStore()
				require.NoError(t, err)
				return s
			},
			assert: constructs,
		},
		{
			name: "an EXPLICIT member cap at the completion-size ceiling constructs",
			store: func(t *testing.T) *memory.GroupStore {
				t.Helper()
				s, err := memory.NewGroupStore(memory.WithMaxGroupMembers(1 << 16))
				require.NoError(t, err)
				return s
			},
			assert: constructs,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			agg, err := routing.NewAggregator[int, int](tc.store(t), sumFn,
				routing.WithOutputChannel(&fakeAggChannel{}),
				routing.WithCompletionSize(1<<16))
			tc.assert(t, agg, err)
		})
	}
}
