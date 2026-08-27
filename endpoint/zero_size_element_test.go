package endpoint_test

import (
	"context"
	"math"
	"runtime"
	"testing"

	"github.com/kartaladev/msgin"
	"github.com/kartaladev/msgin/adapter/memory"
	"github.com/kartaladev/msgin/endpoint"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestZeroSizeElementChannel_AnyCapacitySucceeds pins the language-level
// property that two "safe" sizing sites silently rest on: a channel whose
// element type is the zero-size `struct{}` never trips runtime.makechan's
// size-out-of-range panic, at ANY capacity — including math.MaxInt, the
// largest capacity the language admits and far above every ceiling either
// site is ever configured with (Spec 016 §1.1). credit.go:21
// (endpoint's creditGate) and queuestore.go's `sem` field (adapter/memory,
// the QueueStore's occupancy semaphore) both rely on exactly this fact to
// justify shipping with NO upper bound of their own (Spec 016 §2.1, §4).
//
// This case is deliberately independent of both increment's own ceilings
// (maxInFlightCeiling, maxCapacityCeiling): it imports neither endpoint nor
// memory, so it cannot be made unexecutable by a future change to either
// (Spec 016 §6 AC-4 — revision 1's version of this test depended on a
// consumer whose credit gate WAS the ceiling-capped maxInFlight, and could
// never reach math.MaxInt after Task 1 landed).
//
// # Why math.MaxInt and not a fixed literal (Plan 030 Task 2)
//
// This case previously used the literal 1<<62 (and its decimal expansion
// 4611686018427387904), which does not fit an int on GOARCH=386 and made the
// whole endpoint test binary fail to COMPILE there. math.MaxInt is the
// architecture-widest capacity by definition, so it keeps the demonstration
// maximal on 64-bit while remaining a legal int on 32-bit — the one
// replacement that is both portable and does not weaken the claim.
//
// A smaller fixed literal (e.g. 1<<30) would be WRONG here: with a non-empty
// element type 1<<30 may still succeed, so the make would stop discriminating
// zero-size from non-zero-size elements and the test would pass for reasons
// unrelated to the property it exists to pin. With math.MaxInt the mutant is
// decisive — see below.
//
// MUTATION-PROVEN (Plan 030 Task 2): changing `chan struct{}` to `chan byte`
// makes runtime.makechan compute mem = 1 × math.MaxInt > maxAlloc and panic
// with "makechan: size out of range", failing this test. The panic is
// recoverable and immediate — no allocation is attempted — so the mutant is
// cheap to reproduce, unlike adapter/memory/sizing_bounds_test.go's (see that
// file's header).
func TestZeroSizeElementChannel_AnyCapacitySucceeds(t *testing.T) {
	ch := make(chan struct{}, math.MaxInt)
	assert.Equal(t, math.MaxInt, cap(ch))
}

// TestConsumer_MaxInFlightCeiling_AllocationDelta is Spec 016 §6 AC-4's
// in-situ half for credit.go:21. It builds a real Consumer at
// maxInFlightCeiling (1<<20) and runs it, then asserts the cumulative
// allocation delta lands in (40 MiB, 64 MiB).
//
// The bound is NOT "delta < 1 MiB", unlike the QueueStore twin below: this
// site's baseline at the ceiling is already ~48.0 MiB from workerCh
// (consumer.go:384, `make(chan managedDelivery, c.maxInFlight)`,
// sizeof(managedDelivery)=48), so a delta assertion here measures workerCh's
// allocation, not the credit gate's. What proves credit.go:21's OWN
// zero-size-element property is that the delta stays in this band rather
// than roughly doubling: mutating credit.go:21 from `chan struct{}` to `chan
// managedDelivery` adds a second ~48 MiB allocation, pushing the delta to
// ~96 MiB — well past the 64 MiB upper bound. The lower bound (40 MiB) is
// mandatory: without it, a measurement taken before Run allocates (e.g. right
// after NewConsumer returns) would pass vacuously.
//
// Measurement protocol (Spec 016 §6 AC-4, all three load-bearing): no
// t.Parallel (so no sibling test's allocations pollute the window);
// runtime.KeepAlive(c) after the second ReadMemStats (so the ceiling-sized
// channels are not collectible before the read); TotalAlloc, not HeapAlloc
// (cumulative and GC-independent — HeapAlloc reads ~3.3x lower on a related
// workload measured elsewhere in this spec, Spec 016 §1.4).
func TestConsumer_MaxInFlightCeiling_AllocationDelta(t *testing.T) {
	const mib = 1 << 20
	h := func(context.Context, msgin.Message[order]) error { return nil }

	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)

	c, err := endpoint.NewConsumer[order](stubPolling{}, h, endpoint.WithMaxInFlight[order](1<<20))
	require.NoError(t, err)
	stop := runConsumer(t, c)
	stop() // joins Run — workerCh's ceiling-sized allocation has happened by now

	runtime.ReadMemStats(&after)
	runtime.KeepAlive(c)

	delta := after.TotalAlloc - before.TotalAlloc
	assert.Greater(t, delta, uint64(40*mib),
		"delta must clear the lower bound, or a measurement taken before Run allocates would pass vacuously")
	assert.Less(t, delta, uint64(64*mib),
		"delta must stay below ~96 MiB (the credit.go:21 mutant's cost: a second ~48 MiB "+
			"chan managedDelivery replacing the zero-size chan struct{})")
}

// TestQueueStore_CapacityCeiling_AllocationDelta is Spec 016 §6 AC-4's
// in-situ half for queuestore.go's `sem` field. It builds a QueueStore at
// maxCapacityCeiling (1<<20) and asserts the cumulative allocation delta
// stays under 1 MiB.
//
// Unlike the consumer twin above, this site's baseline is tiny (288 B,
// measured) because `sem` is the ONLY ceiling-sized allocation NewQueueStore
// performs — so the delta bound directly distinguishes the real
// `chan struct{}` from a mutated 24-byte-element channel: mutating
// queuestore.go's `sem: make(chan struct{}, capacity)` to
// `chan msgin.Message[any]` costs `1<<20 * 24` = 25,165,824 B (~24 MiB),
// 24x past the 1 MiB bound.
//
// Measurement protocol: see TestConsumer_MaxInFlightCeiling_AllocationDelta's
// doc — the same three conditions apply (no t.Parallel, KeepAlive, TotalAlloc).
func TestQueueStore_CapacityCeiling_AllocationDelta(t *testing.T) {
	const mib = 1 << 20

	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)

	s, err := memory.NewQueueStore(memory.WithCapacity(1 << 20))
	require.NoError(t, err)

	runtime.ReadMemStats(&after)
	runtime.KeepAlive(s)

	delta := after.TotalAlloc - before.TotalAlloc
	assert.Less(t, delta, uint64(mib),
		"delta must stay far below the 25,165,824 B a mutated 24-byte-element sem channel would cost "+
			"(queuestore.go's sem field, zero-size-element property, Spec 016 §4)")
}
