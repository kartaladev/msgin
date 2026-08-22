package memory_test

// Spec: 016  Plan: 029 (Task 3), 030 (Task 2)  ADR: 0032
//
// # The oversized-buffer literal is 1<<30, not 1<<62 (Plan 030 Task 2)
//
// Every out-of-range WithBuffer literal in this file is 1<<30. It exceeds
// memory.WithBuffer's ceiling (1<<20) by three orders of magnitude, so it
// selects the identical out-of-range branch, while fitting an int32 so this
// package compiles under GOARCH=386. The rendered decimal in
// assertFirstFaultIsSizing below is 1073741824 and must move with it.
//
// 🔴 ACCEPTED TRADE — DO NOT RE-RUN THE `makechan` MUTANT OF
// TestNew_SizingGuardIsIndependentOfTheLatch. That test's doc describes a
// wrong implementation shape which, at the OLD 1<<62, reached
// `make(chan msgin.Message[any], n)` and PANICKED immediately:
// runtime.makechan raises "size out of range" only when
// elemsize × cap > maxAlloc (≈1<<48 on 64-bit), and 16 × 1<<62 overflows it.
// At 1<<30 the product is ~16 GiB — UNDER that threshold — so reproducing the
// mutation now attempts a real allocation and will likely OOM-kill the test
// binary instead of producing a recoverable panic.
//
// THE SHIPPED TEST IS UNAFFECTED: the ceiling check rejects 1<<30 and returns
// long before any make is reached, so require.NotPanics holds exactly as it
// did at 1<<62. Only *reproducing* the mutant became expensive. The guard's
// mutation evidence stands from Plan 029 Task 3, where it was cheap.

import (
	"context"
	"testing"
	"time"

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

// sendFault reports what Broker.Send returns under a SHORT deadline.
//
// The deadline is mandatory, not hygiene (the same reasoning memory_test.go's
// TestNew_NilOptionElement records): a Broker whose fault was NOT latched still
// holds an unbuffered channel with no reader, so Send blocks in its select and
// a mutant that removes the guard would HANG the binary for 10 minutes instead
// of failing. The deadline converts that into a fast, observable red.
func sendFault(t *testing.T, b *memory.Broker) error {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	return b.Send(ctx, msgin.New[any]("x"))
}

// streamFault reports what Broker.Stream returns under a SHORT deadline — see
// sendFault for why the deadline is load-bearing (an unlatched Stream blocks on
// <-ctx.Done()).
func streamFault(t *testing.T, b *memory.Broker) error {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	return b.Stream(ctx, make(chan msgin.Delivery))
}

// TestWithBuffer_OutOfRangeIsLatchedAndReported proves memory.WithBuffer's
// range [0, maxBufferCeiling] is enforced at BOTH ends and reported through
// family R2 (Spec 016 §3.2/§3.3/§3.6): memory.New has no error return, so the
// fault is LATCHED on the *Broker and reported by BOTH Broker.Send and
// Broker.Stream, before either method's own logic (ADR 0031 D-V).
//
// Two things separate this from every R1 knob in this file:
//
//   - the R2 error IS msgin.Permanent-wrapped (Spec 016 §3.2) — the opposite of
//     the R1 rows above, which must stay bare (ADR 0029 D-M). A reflex in either
//     direction fails one of the two;
//   - the lower bound is 0, not 1 — an unbuffered Broker is legal, and only
//     n < 0 is out of range (Spec 016 §3.6, which FOLDS IN the silent clamp
//     WithBuffer used to apply to a negative n).
//
// The rendered message is asserted at both ends (Spec 016 §6 AC-2b): the "%d
// not in [%d, %d]" shape is true at the lower end, where "exceeds" would render
// the falsehood "-1 exceeds 1048576".
func TestWithBuffer_OutOfRangeIsLatchedAndReported(t *testing.T) {
	const wantCeilingPlusOne = "msgin: permanent: msgin: capacity out of range: " +
		"memory.WithBuffer: 1048577 not in [0, 1048576]"
	const wantNegative = "msgin: permanent: msgin: capacity out of range: " +
		"memory.WithBuffer: -1 not in [0, 1048576]"

	assertLatched := func(want string) func(t *testing.T, err error) {
		return func(t *testing.T, err error) {
			t.Helper()
			require.ErrorIs(t, err, msgin.ErrInvalidCapacity)
			assert.True(t, msgin.IsPermanent(err), "the R2 sizing fault must be Permanent-wrapped (Spec 016 §3.2)")
			assert.EqualError(t, err, want)
		}
	}

	tests := []struct {
		name   string
		report func(t *testing.T, b *memory.Broker) error
		n      int
		assert func(t *testing.T, err error)
	}{
		{
			name:   "ceiling+1, reported by Send",
			report: sendFault,
			n:      1<<20 + 1,
			assert: assertLatched(wantCeilingPlusOne),
		},
		{
			name:   "ceiling+1, reported by Stream",
			report: streamFault,
			n:      1<<20 + 1,
			assert: assertLatched(wantCeilingPlusOne),
		},
		{
			name:   "negative n, reported by Send",
			report: sendFault,
			n:      -1,
			assert: assertLatched(wantNegative),
		},
		{
			name:   "negative n, reported by Stream",
			report: streamFault,
			n:      -1,
			assert: assertLatched(wantNegative),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tc.assert(t, tc.report(t, memory.New(memory.WithBuffer(tc.n))))
		})
	}
}

// TestWithBuffer_InRangeIsAccepted pins the two ends that must NOT be rejected.
//
// The ceiling case is AC-1's "allocating" family (Spec 016 §6): the allocation
// IS the hazard, so the hazardous path is RUN at the ceiling — the buffered
// channel is really made (24.0 MiB, measured) and a Send into it succeeds with
// no reader, which an unbuffered Broker could not do. Asserting only that
// construction returned would pass against a WithBuffer that silently ignored n.
//
// The 0 case is the lower end of [0, maxBufferCeiling]: an unbuffered Broker is
// legal, so the guard must be n < 0, never n < 1. Its evidence is that Send
// times out (no reader on an unbuffered channel) rather than reporting a latched
// ErrInvalidCapacity.
func TestWithBuffer_InRangeIsAccepted(t *testing.T) {
	tests := []struct {
		name   string
		n      int
		assert func(t *testing.T, b *memory.Broker)
	}{
		{
			name: "ceiling is accepted and the buffer is really allocated",
			n:    1 << 20,
			assert: func(t *testing.T, b *memory.Broker) {
				ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
				defer cancel()
				require.NoError(t, b.Send(ctx, msgin.New[any]("x")),
					"a Send with no reader can only succeed into a real buffer")
			},
		},
		{
			name: "0 is accepted — an unbuffered Broker is legal",
			n:    0,
			assert: func(t *testing.T, b *memory.Broker) {
				err := sendFault(t, b)
				require.ErrorIs(t, err, context.DeadlineExceeded,
					"an unbuffered Send with no reader blocks until ctx is done")
				require.NotErrorIs(t, err, msgin.ErrInvalidCapacity,
					"0 is inside [0, maxBufferCeiling] and must not latch a fault")
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tc.assert(t, memory.New(memory.WithBuffer(tc.n)))
		})
	}
}

// TestNew_SizingGuardIsIndependentOfTheLatch is Spec 016 §6 AC-3 and AC-3b —
// the one case that separates the required shape from the plausible wrong one
// (ADR 0032 D-Y).
//
// ADR 0031 D-U makes New's apply loop `continue` past a nil option, so a LATER
// WithBuffer(1<<30) still runs when an earlier nil already took the latch. The
// latch is first-fault-wins, so at that point `b.err == nil` is FALSE and the
// sizing fault is not recorded. An implementation that gates the allocation on
// the latch taking — i.e. that nests the `return` inside `if b.err == nil` —
// therefore reaches `make(chan msgin.Message[any], 1<<30)` and blows up — at
// the original 1<<62 that was an immediate makechan panic; at 1<<30 it is a
// ~16 GiB allocation attempt instead (see this file's header for why the
// literal changed and why that mutant must not be re-run). Either way the
// wrong shape is fatal and the right one never reaches the make.
//
// So the guard is the UNCONDITIONAL `return`; the latch only decides WHICH
// error the caller reads:
//
//   - AC-3  New(nil, WithBuffer(1<<30)) → no panic, and the reported fault is
//     ErrNilFunc at index 0 (the first fault), NOT ErrInvalidCapacity;
//   - AC-3b New(WithBuffer(1<<30), nil) → the reported fault is
//     ErrInvalidCapacity, because index 0 wins there instead.
//
// Both orderings are asserted through BOTH reporting surfaces. Every other test
// in this file passes against the wrong shape; the NotPanics is what makes this
// one fail against it (mutation-proven — see the task report).
//
// AC-3b's third clause, New(WithBuffer(-1)) → ErrInvalidCapacity, is covered by
// TestWithBuffer_OutOfRangeIsLatchedAndReported's negative rows above, which
// also keep WithBuffer's godoc range honest.
func TestNew_SizingGuardIsIndependentOfTheLatch(t *testing.T) {
	tests := []struct {
		name   string
		opts   []memory.Option
		report func(t *testing.T, b *memory.Broker) error
		assert func(t *testing.T, err error)
	}{
		{
			name:   "AC-3: nil first, oversized buffer second — the NIL is reported (Send)",
			opts:   []memory.Option{nil, memory.WithBuffer(1 << 30)},
			report: sendFault,
			assert: assertFirstFaultIsNilOption,
		},
		{
			name:   "AC-3: nil first, oversized buffer second — the NIL is reported (Stream)",
			opts:   []memory.Option{nil, memory.WithBuffer(1 << 30)},
			report: streamFault,
			assert: assertFirstFaultIsNilOption,
		},
		{
			name:   "AC-3b: oversized buffer first, nil second — the SIZE is reported (Send)",
			opts:   []memory.Option{memory.WithBuffer(1 << 30), nil},
			report: sendFault,
			assert: assertFirstFaultIsSizing,
		},
		{
			name:   "AC-3b: oversized buffer first, nil second — the SIZE is reported (Stream)",
			opts:   []memory.Option{memory.WithBuffer(1 << 30), nil},
			report: streamFault,
			assert: assertFirstFaultIsSizing,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var b *memory.Broker
			require.NotPanics(t, func() { b = memory.New(tc.opts...) },
				"New must not panic on caller input: the range check's `return` prevents the make "+
					"whether or not the latch took (ADR 0032 D-Y)")
			tc.assert(t, tc.report(t, b))
		})
	}
}

// assertFirstFaultIsNilOption asserts AC-3's discriminating outcome: the nil
// option at index 0 is the fault the caller reads, and the sizing fault of the
// later, still-applied WithBuffer is NOT — first-fault-wins is intact even
// though the guard returned unconditionally.
func assertFirstFaultIsNilOption(t *testing.T, err error) {
	t.Helper()
	require.ErrorIs(t, err, msgin.ErrNilFunc)
	require.NotErrorIs(t, err, msgin.ErrInvalidCapacity,
		"the latch is first-fault-wins: the sizing fault must not overwrite the nil at index 0")
	assert.True(t, msgin.IsPermanent(err))
	assert.Contains(t, err.Error(), "memory.New: nil option at index 0")
}

// assertFirstFaultIsSizing asserts AC-3b's mirror: with the oversized buffer at
// index 0 the sizing fault takes the latch, and the later nil does not overwrite
// it.
func assertFirstFaultIsSizing(t *testing.T, err error) {
	t.Helper()
	require.ErrorIs(t, err, msgin.ErrInvalidCapacity)
	require.NotErrorIs(t, err, msgin.ErrNilFunc,
		"the latch is first-fault-wins: the nil at index 1 must not overwrite the sizing fault")
	assert.True(t, msgin.IsPermanent(err))
	assert.Contains(t, err.Error(), "memory.WithBuffer: 1073741824 not in [0, 1048576]")
}
