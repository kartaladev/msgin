package routing_test

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/kartaladev/msgin"
	"github.com/kartaladev/msgin/channel"
	"github.com/kartaladev/msgin/routing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewAggregator_CompletionSizeBoundsRejects proves WithCompletionSize
// rejects a value outside its documented [1, 65536] range with the EXISTING
// sentinel (msgin.ErrInvalidCapacity), non-Permanent (Spec 016 §3.1 R1, ADR
// 0029 D-M), and a rendered message that is true at BOTH ends of the range
// (Spec 016 §6 AC-2b) — not merely an errors.Is match. Unlike every other R1
// knob, WithCompletionSize has NO pre-existing lower-bound arm (Spec 016
// §3.1): both ends are new here.
func TestNewAggregator_CompletionSizeBoundsRejects(t *testing.T) {
	tests := []struct {
		name   string
		n      int
		assert func(t *testing.T, err error)
	}{
		{
			name: "ceiling+1 is rejected",
			n:    1<<16 + 1,
			assert: func(t *testing.T, err error) {
				require.ErrorIs(t, err, msgin.ErrInvalidCapacity)
				assert.False(t, msgin.IsPermanent(err), "R1 constructor error must stay bare (ADR 0029 D-M)")
				assert.EqualError(t, err,
					"msgin: capacity out of range: routing.WithCompletionSize: 65537 not in [1, 65536]")
			},
		},
		{
			name: "lower bound 0 is rejected",
			n:    0,
			assert: func(t *testing.T, err error) {
				require.ErrorIs(t, err, msgin.ErrInvalidCapacity)
				assert.False(t, msgin.IsPermanent(err), "R1 constructor error must stay bare (ADR 0029 D-M)")
				assert.EqualError(t, err,
					"msgin: capacity out of range: routing.WithCompletionSize: 0 not in [1, 65536]")
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			store := newIntStore(t)
			_, err := routing.NewAggregator[int, int](store, sumFn,
				routing.WithOutputChannel(&fakeAggChannel{}),
				routing.WithCompletionSize(tc.n),
			)
			tc.assert(t, err)
		})
	}
}

// TestNewAggregator_CompletionSizeCeilingAccepts proves WithCompletionSize
// (ceiling) is accepted (Spec 016 §6 AC-1/AC-2's "growth" family: at the
// ceiling only construction + acceptance is asserted). Growing a real group
// to 65,536 members costs a measured 48.3 GiB of allocation churn and 8.6s
// (memory.GroupStore.Add clones the group snapshot per call — quadratic in
// member count, Plan 029 Task 4), so it is NEVER exercised here; the
// cap-still-caps property is proven separately, at small n, by
// TestAggregator_CompletionSize_CapStillCaps below.
func TestNewAggregator_CompletionSizeCeilingAccepts(t *testing.T) {
	t.Parallel()

	store := newIntStore(t)
	agg, err := routing.NewAggregator[int, int](store, sumFn,
		routing.WithOutputChannel(&fakeAggChannel{}),
		routing.WithCompletionSize(1<<16),
	)
	require.NoError(t, err)
	require.NotNil(t, agg)
}

// TestNewAggregator_CompletionSizeValidatedEvenWhenReleaseStrategyOverwritten
// pins the deliberate "inert n" rejection (Plan 029 Task 4, round-4 m4-9):
// WithReleaseStrategy/WithReleaseWhen overwrite the release closure
// WithCompletionSize installs, so an oversized n would otherwise have NO
// runtime effect once overwritten — accepted before this task, REJECTED
// after it. The bound is validated on the value n itself, independent of
// whether a later option goes on to replace the release strategy.
func TestNewAggregator_CompletionSizeValidatedEvenWhenReleaseStrategyOverwritten(t *testing.T) {
	t.Parallel()

	// 1<<30 (not 1<<62): it exceeds every ceiling in the codebase (the largest
	// is 1<<20) so it selects the identical out-of-range branch, while fitting
	// an int32 so this file compiles on GOARCH=386 (Plan 030 Task 2, reject
	// arm). :103 renders n with %d, so the expected string follows the constant.
	const n = 1 << 30
	store := newIntStore(t)
	_, err := routing.NewAggregator[int, int](store, sumFn,
		routing.WithOutputChannel(&fakeAggChannel{}),
		routing.WithCompletionSize(n),
		routing.WithReleaseWhen(func(msgin.MessageGroup) bool { return false }),
	)
	require.ErrorIs(t, err, msgin.ErrInvalidCapacity)
	assert.False(t, msgin.IsPermanent(err), "R1 constructor error must stay bare (ADR 0029 D-M)")
	assert.EqualError(t, err,
		fmt.Sprintf("msgin: capacity out of range: routing.WithCompletionSize: %d not in [1, 65536]", n))
}

// TestAggregator_CompletionSize_CapStillCaps proves the cap still caps at a
// small n (Spec 016 §1.3 / §6's growth-knob split): WithCompletionSize(2)
// over 6 messages to the SAME correlation key releases exactly 3 times —
// the property the increment restores, since an unbounded n left
// WithCompletionSize as the one knob with no upper bound at all (Spec 016
// §1.4).
//
// The fixture needs a REAL, subscribed MessageChannel — not merely a
// WithOutputChannel that never errors — because a release's forward-to-
// output Send is on Handle's hot path (release -> releaseOnce ->
// cfg.output.Send): channel.DirectChannel.Send with no subscriber returns
// msgin.ErrNoSubscriber, which would make every release fail instead of
// being counted. WithCorrelationStrategy pins all 6 messages to one group
// regardless of id.
func TestAggregator_CompletionSize_CapStillCaps(t *testing.T) {
	t.Parallel()

	store := newIntStore(t)
	out := channel.NewDirectChannel()

	var mu sync.Mutex
	released := 0
	_, err := out.Subscribe(msgin.HandlerFunc(func(context.Context, msgin.Message[any]) error {
		mu.Lock()
		released++
		mu.Unlock()
		return nil
	}))
	require.NoError(t, err)

	fixedKey := func(msgin.Message[any]) (string, error) { return "g", nil }

	agg, err := routing.NewAggregator[int, int](store, sumFn,
		routing.WithOutputChannel(out),
		routing.WithCorrelationStrategy(fixedKey),
		routing.WithCompletionSize(2),
	)
	require.NoError(t, err)

	for i := range 6 {
		msg := msgin.New[any](i, msgin.WithID(fmt.Sprintf("m%d", i)))
		require.NoError(t, agg.Handle(t.Context(), msg))
	}

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, 3, released,
		"WithCompletionSize(2) over 6 messages to one group must release exactly 3 times")
}
