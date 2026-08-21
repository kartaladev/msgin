package memory_test

import (
	"testing"

	"github.com/kartaladev/msgin"
	"github.com/kartaladev/msgin/adapter/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSizingBoundsRejects proves WithCapacity and WithMaxGroups reject a
// value outside their documented [lo, hi] range with the EXISTING sentinel
// (msgin.ErrInvalidCapacity), non-Permanent (Spec 016 §3.1 R1, ADR 0029 D-M),
// and a rendered message that is true at BOTH ends of the range (Spec 016 §6
// AC-2b) — not merely an errors.Is match. The lower-bound (0) case is
// mandatory: it is the one that would have caught a format rendering "0
// exceeds 1048576" — false, since 0 exceeds nothing (Spec 016 §3.1's
// rejection of revision 2's "%d exceeds %d" shape).
func TestSizingBoundsRejects(t *testing.T) {
	tests := []struct {
		name   string
		build  func() error
		assert func(t *testing.T, err error)
	}{
		{
			name: "WithCapacity ceiling+1 is rejected",
			build: func() error {
				_, err := memory.NewQueueStore(memory.WithCapacity(1<<20 + 1))
				return err
			},
			assert: func(t *testing.T, err error) {
				require.ErrorIs(t, err, msgin.ErrInvalidCapacity)
				assert.False(t, msgin.IsPermanent(err), "R1 constructor error must stay bare (ADR 0029 D-M)")
				assert.EqualError(t, err,
					"msgin: capacity out of range: memory.WithCapacity: 1048577 not in [1, 1048576]")
			},
		},
		{
			name: "WithCapacity lower bound 0 is rejected",
			build: func() error {
				_, err := memory.NewQueueStore(memory.WithCapacity(0))
				return err
			},
			assert: func(t *testing.T, err error) {
				require.ErrorIs(t, err, msgin.ErrInvalidCapacity)
				assert.False(t, msgin.IsPermanent(err), "R1 constructor error must stay bare (ADR 0029 D-M)")
				assert.EqualError(t, err,
					"msgin: capacity out of range: memory.WithCapacity: 0 not in [1, 1048576]")
			},
		},
		{
			name: "WithMaxGroups ceiling+1 is rejected",
			build: func() error {
				_, err := memory.NewGroupStore(memory.WithMaxGroups(1<<20 + 1))
				return err
			},
			assert: func(t *testing.T, err error) {
				require.ErrorIs(t, err, msgin.ErrInvalidCapacity)
				assert.False(t, msgin.IsPermanent(err), "R1 constructor error must stay bare (ADR 0029 D-M)")
				assert.EqualError(t, err,
					"msgin: capacity out of range: memory.WithMaxGroups: 1048577 not in [1, 1048576]")
			},
		},
		{
			name: "WithMaxGroups lower bound 0 is rejected",
			build: func() error {
				_, err := memory.NewGroupStore(memory.WithMaxGroups(0))
				return err
			},
			assert: func(t *testing.T, err error) {
				require.ErrorIs(t, err, msgin.ErrInvalidCapacity)
				assert.False(t, msgin.IsPermanent(err), "R1 constructor error must stay bare (ADR 0029 D-M)")
				assert.EqualError(t, err,
					"msgin: capacity out of range: memory.WithMaxGroups: 0 not in [1, 1048576]")
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tc.assert(t, tc.build())
		})
	}
}

// TestNewQueueStore_CapacityCeilingAccepts proves WithCapacity(ceiling) is
// accepted (Spec 016 §6 AC-1/AC-2's "growth" family: at the ceiling only
// construction + acceptance is asserted — filling s.ready past 1<<20 costs
// ~484 MiB live under a realistic msgin.New fixture (Spec 016 §6, measured),
// so it is not exercised here; the cap-still-caps property is proven
// separately, at small n, by TestQueueStore_Capacity_CapStillCaps below).
func TestNewQueueStore_CapacityCeilingAccepts(t *testing.T) {
	t.Parallel()

	s, err := memory.NewQueueStore(memory.WithCapacity(1 << 20))
	require.NoError(t, err)
	require.NotNil(t, s)
}

// TestQueueStore_Capacity_CapStillCaps proves the cap still caps at a small n
// (Spec 016 §1.3 / §6's growth-knob split): with WithCapacity(1) and
// WithOverflow(OverflowReject), the second Enqueue is rejected with
// msgin.ErrOverflowDropped. This is the property the increment restores — an
// unbounded n silently stopped this cap from capping (queuestore.go's
// s.ready append, gated only by the now-huge s.sem).
//
// WithOverflow(OverflowReject) is MANDATORY here: QueueStore's default
// policy is OverflowBlock, so a second Enqueue against the default policy
// blocks on s.sem <- struct{}{} until ctx is done — never returning
// ErrOverflowDropped and hanging the test (Spec 016 §6, round-3 M3-1;
// groupstore_test.go's precedent does NOT transfer, since GroupStore.Add has
// no overflow policy at all).
func TestQueueStore_Capacity_CapStillCaps(t *testing.T) {
	t.Parallel()

	s, err := memory.NewQueueStore(memory.WithCapacity(1), memory.WithOverflow(msgin.OverflowReject))
	require.NoError(t, err)

	require.NoError(t, s.Enqueue(t.Context(), msgin.New[any]("a")))
	err = s.Enqueue(t.Context(), msgin.New[any]("b"))
	require.ErrorIs(t, err, msgin.ErrOverflowDropped)
}

// TestNewGroupStore_MaxGroupsCeilingAccepts proves WithMaxGroups(ceiling) is
// accepted (Spec 016 §6 AC-1/AC-2's "growth" family: at the ceiling only
// construction + acceptance is asserted — filling s.groups past 1<<20 costs
// ~853 MiB live under a realistic msgin.New fixture (Spec 016 §6, measured),
// so it is not exercised here; the cap-still-caps property is proven
// separately, at small n, by the existing groupstore_test.go:30-39 case).
func TestNewGroupStore_MaxGroupsCeilingAccepts(t *testing.T) {
	t.Parallel()

	s, err := memory.NewGroupStore(memory.WithMaxGroups(1 << 20))
	require.NoError(t, err)
	require.NotNil(t, s)
}
