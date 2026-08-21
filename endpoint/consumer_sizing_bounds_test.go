package endpoint_test

import (
	"context"
	"testing"

	"github.com/kartaladev/msgin"
	"github.com/kartaladev/msgin/endpoint"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewConsumer_SizingBoundsRejects proves WithMaxInFlight and
// WithConcurrency reject a value outside their documented [lo, hi] range with
// the EXISTING sentinel, non-Permanent (Spec 016 §3.1 R1, ADR 0029 D-M), and a
// rendered message that is true at BOTH ends of the range (Spec 016 §6 AC-2b)
// — not merely an errors.Is match. The lower-bound (0) case is mandatory: it
// is the one that would have caught a format rendering "0 exceeds
// 1048576" — false, since 0 exceeds nothing (Spec 016 §3.1's rejection of
// revision 2's "%d exceeds %d" shape).
func TestNewConsumer_SizingBoundsRejects(t *testing.T) {
	h := func(context.Context, msgin.Message[order]) error { return nil }

	tests := []struct {
		name   string
		opt    endpoint.ConsumerOption[order]
		assert func(t *testing.T, err error)
	}{
		{
			name: "WithMaxInFlight ceiling+1 is rejected",
			opt:  endpoint.WithMaxInFlight[order](1<<20 + 1),
			assert: func(t *testing.T, err error) {
				require.ErrorIs(t, err, msgin.ErrInvalidMaxInFlight)
				assert.False(t, msgin.IsPermanent(err), "R1 constructor error must stay bare (ADR 0029 D-M)")
				assert.EqualError(t, err,
					"msgin: max in-flight out of range: endpoint.WithMaxInFlight: 1048577 not in [1, 1048576]")
			},
		},
		{
			name: "WithMaxInFlight lower bound 0 is rejected",
			opt:  endpoint.WithMaxInFlight[order](0),
			assert: func(t *testing.T, err error) {
				require.ErrorIs(t, err, msgin.ErrInvalidMaxInFlight)
				assert.False(t, msgin.IsPermanent(err), "R1 constructor error must stay bare (ADR 0029 D-M)")
				assert.EqualError(t, err,
					"msgin: max in-flight out of range: endpoint.WithMaxInFlight: 0 not in [1, 1048576]")
			},
		},
		{
			name: "WithConcurrency ceiling+1 is rejected",
			opt:  endpoint.WithConcurrency[order](1<<16 + 1),
			assert: func(t *testing.T, err error) {
				require.ErrorIs(t, err, msgin.ErrInvalidConcurrency)
				assert.False(t, msgin.IsPermanent(err), "R1 constructor error must stay bare (ADR 0029 D-M)")
				assert.EqualError(t, err,
					"msgin: concurrency out of range: endpoint.WithConcurrency: 65537 not in [1, 65536]")
			},
		},
		{
			name: "WithConcurrency lower bound 0 is rejected",
			opt:  endpoint.WithConcurrency[order](0),
			assert: func(t *testing.T, err error) {
				require.ErrorIs(t, err, msgin.ErrInvalidConcurrency)
				assert.False(t, msgin.IsPermanent(err), "R1 constructor error must stay bare (ADR 0029 D-M)")
				assert.EqualError(t, err,
					"msgin: concurrency out of range: endpoint.WithConcurrency: 0 not in [1, 65536]")
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := endpoint.NewConsumer[order](stubPolling{}, h, tc.opt)
			tc.assert(t, err)
		})
	}
}

// TestNewConsumer_MaxInFlightCeilingAccepts proves WithMaxInFlight(ceiling) is
// accepted AND that the hazardous path — sizing workerCh's backing array to
// the ceiling — actually runs (Spec 016 §6 AC-1's "allocating" family, split
// table). A construction-only assertion does not prove the panic is gone: the
// allocation happens inside Run, not NewConsumer. runConsumer starts Run and
// stop() cancels+joins it; Run's channel/gate setup runs synchronously before
// it ever waits on ctx, so this still exercises the ceiling-sized alloc.
//
// EXPECT A MEMORY SPIKE: at the ceiling this allocates ~48.0 MiB (measured;
// 1<<20 * sizeof(managedDelivery)=48 = 50,331,648 B) for workerCh. This is the
// ceiling's intended, documented cost (Spec 016 §3.4) — not a leak.
func TestNewConsumer_MaxInFlightCeilingAccepts(t *testing.T) {
	h := func(context.Context, msgin.Message[order]) error { return nil }
	c, err := endpoint.NewConsumer[order](stubPolling{}, h, endpoint.WithMaxInFlight[order](1<<20))
	require.NoError(t, err)

	stop := runConsumer(t, c)
	stop() // cancels + joins Run — proves the ceiling-sized alloc did not panic
}

// TestNewConsumer_ConcurrencyCeilingAccepts proves WithConcurrency(ceiling) is
// accepted AND Run actually spawns concurrencyCeiling worker goroutines
// without panicking (Spec 016 §6 AC-1 / AC-2's WithConcurrency row: this knob
// "spawns goroutines", distinct from the allocating and growth families).
//
// EXPECT A MEMORY SPIKE: 65,536 goroutines cost ~128 MiB of stack (~257 MiB
// under -race), measured at ~2,052 B/goroutine (~4,114 B under -race). This is
// the ceiling's intended, documented cost (Spec 016 §3.4) — not a leak; TestMain's
// goleak.VerifyTestMain proves every one of them is joined by process exit.
func TestNewConsumer_ConcurrencyCeilingAccepts(t *testing.T) {
	h := func(context.Context, msgin.Message[order]) error { return nil }
	c, err := endpoint.NewConsumer[order](stubPolling{}, h, endpoint.WithConcurrency[order](1<<16))
	require.NoError(t, err)

	stop := runConsumer(t, c)
	stop() // cancels + joins Run — proves the ceiling-sized worker pool did not panic
}
