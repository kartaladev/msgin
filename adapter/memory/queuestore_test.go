package memory_test

import (
	"context"
	"testing"
	"time"

	"github.com/jonboulle/clockwork"
	"github.com/kartaladev/msgin"
	"github.com/kartaladev/msgin/adapter/memory"
	"github.com/stretchr/testify/require"
)

func TestQueueStore(t *testing.T) {
	tests := []struct {
		name   string
		assert func(t *testing.T)
	}{
		{
			name: "explicit non-positive capacity is ErrInvalidCapacity",
			assert: func(t *testing.T) {
				_, err := memory.NewQueueStore(memory.WithCapacity(0))
				require.ErrorIs(t, err, msgin.ErrInvalidCapacity)
			},
		},
		{
			name: "enqueue then claim returns FIFO, capped at max",
			assert: func(t *testing.T) {
				s, err := memory.NewQueueStore()
				require.NoError(t, err)
				require.NoError(t, s.Enqueue(t.Context(), msgin.New[any]("a")))
				require.NoError(t, s.Enqueue(t.Context(), msgin.New[any]("b")))
				got, err := s.Claim(t.Context(), 5)
				require.NoError(t, err)
				require.Len(t, got, 2)
				require.Equal(t, "a", got[0].Msg.Payload())
				require.Equal(t, "b", got[1].Msg.Payload())
			},
		},
		{
			name: "ack removes; a claimed-then-acked message is not re-claimed",
			assert: func(t *testing.T) {
				s, _ := memory.NewQueueStore()
				_ = s.Enqueue(t.Context(), msgin.New[any]("a"))
				got, _ := s.Claim(t.Context(), 1)
				require.NoError(t, got[0].Ack(t.Context()))
				again, _ := s.Claim(t.Context(), 1)
				require.Empty(t, again)
			},
		},
		{
			name: "nack requeue with delay: invisible until the clock advances",
			assert: func(t *testing.T) {
				clk := clockwork.NewFakeClock()
				s, _ := memory.NewQueueStore(memory.WithClock(clk))
				_ = s.Enqueue(t.Context(), msgin.New[any]("a"))
				got, _ := s.Claim(t.Context(), 1)
				require.NoError(t, got[0].Nack(t.Context(), true, time.Minute))
				// still within the delay window → not claimable
				pending, _ := s.Claim(t.Context(), 1)
				require.Empty(t, pending)
				clk.Advance(time.Minute)
				ready, _ := s.Claim(t.Context(), 1)
				require.Len(t, ready, 1)
			},
		},
		{
			name: "overflow Reject: Enqueue past capacity returns ErrOverflowDropped",
			assert: func(t *testing.T) {
				s, _ := memory.NewQueueStore(memory.WithCapacity(1), memory.WithOverflow(msgin.OverflowReject))
				require.NoError(t, s.Enqueue(t.Context(), msgin.New[any]("a")))
				require.ErrorIs(t, s.Enqueue(t.Context(), msgin.New[any]("b")), msgin.ErrOverflowDropped)
			},
		},
		{
			name: "overflow DropOldest: newest survives, oldest evicted",
			assert: func(t *testing.T) {
				s, _ := memory.NewQueueStore(memory.WithCapacity(1), memory.WithOverflow(msgin.OverflowDropOldest))
				_ = s.Enqueue(t.Context(), msgin.New[any]("old"))
				require.NoError(t, s.Enqueue(t.Context(), msgin.New[any]("new")))
				got, _ := s.Claim(t.Context(), 5)
				require.Len(t, got, 1)
				require.Equal(t, "new", got[0].Msg.Payload())
			},
		},
		{
			name: "overflow DropNewest: oldest survives, newest dropped (no error)",
			assert: func(t *testing.T) {
				s, _ := memory.NewQueueStore(memory.WithCapacity(1), memory.WithOverflow(msgin.OverflowDropNewest))
				_ = s.Enqueue(t.Context(), msgin.New[any]("old"))
				require.NoError(t, s.Enqueue(t.Context(), msgin.New[any]("new")))
				got, _ := s.Claim(t.Context(), 5)
				require.Len(t, got, 1)
				require.Equal(t, "old", got[0].Msg.Payload())
			},
		},
		{
			name: "overflow DropOldest with nothing evictable (all in-flight) drops the newcomer",
			assert: func(t *testing.T) {
				s, _ := memory.NewQueueStore(memory.WithCapacity(1), memory.WithOverflow(msgin.OverflowDropOldest))
				_ = s.Enqueue(t.Context(), msgin.New[any]("a"))
				got, _ := s.Claim(t.Context(), 1) // "a" now in-flight; ready is empty
				require.Len(t, got, 1)
				// buffer full (the in-flight "a" holds the only slot), ready empty →
				// nothing to evict → the newcomer is dropped
				require.ErrorIs(t, s.Enqueue(t.Context(), msgin.New[any]("b")), msgin.ErrOverflowDropped)
			},
		},
		{
			name: "overflow Block: Enqueue unblocks when a slot frees via Ack; honors ctx cancel",
			assert: func(t *testing.T) {
				s, _ := memory.NewQueueStore(memory.WithCapacity(1)) // default Block
				_ = s.Enqueue(t.Context(), msgin.New[any]("a"))
				// full: a cancelled ctx must make Enqueue return ctx.Err(), not hang
				ctx, cancel := context.WithCancel(t.Context())
				cancel()
				require.ErrorIs(t, s.Enqueue(ctx, msgin.New[any]("b")), context.Canceled)
				// free the slot, then Enqueue succeeds
				got, _ := s.Claim(t.Context(), 1)
				require.NoError(t, got[0].Ack(t.Context()))
				require.NoError(t, s.Enqueue(t.Context(), msgin.New[any]("b")))
			},
		},
		{
			name: "nack requeue=false drops the message and frees a slot",
			assert: func(t *testing.T) {
				s, _ := memory.NewQueueStore(memory.WithCapacity(1))
				_ = s.Enqueue(t.Context(), msgin.New[any]("a"))
				got, _ := s.Claim(t.Context(), 1)
				require.NoError(t, got[0].Nack(t.Context(), false, 0)) // genuine drop
				// dropped → not re-claimable, and the freed slot admits a new Enqueue
				empty, _ := s.Claim(t.Context(), 1)
				require.Empty(t, empty)
				require.NoError(t, s.Enqueue(t.Context(), msgin.New[any]("b")))
			},
		},
		{
			name: "fence: a second settle after the first is a no-op (no panic, no double-free)",
			assert: func(t *testing.T) {
				s, _ := memory.NewQueueStore(memory.WithCapacity(1))
				_ = s.Enqueue(t.Context(), msgin.New[any]("a"))
				got, _ := s.Claim(t.Context(), 1)
				require.NoError(t, got[0].Ack(t.Context()))
				// stale settles on the same delivery must be inert (epoch fence)
				require.NoError(t, got[0].Ack(t.Context()))
				require.NoError(t, got[0].Nack(t.Context(), true, time.Minute))
				// the slot was freed exactly once: a fresh Enqueue still succeeds
				require.NoError(t, s.Enqueue(t.Context(), msgin.New[any]("b")))
			},
		},
		{
			name: "claim caps at max and leaves the remainder claimable",
			assert: func(t *testing.T) {
				s, _ := memory.NewQueueStore()
				for _, p := range []string{"a", "b", "c"} {
					_ = s.Enqueue(t.Context(), msgin.New[any](p))
				}
				first, _ := s.Claim(t.Context(), 2)
				require.Len(t, first, 2)
				require.Equal(t, "a", first[0].Msg.Payload())
				require.Equal(t, "b", first[1].Msg.Payload())
				rest, _ := s.Claim(t.Context(), 2)
				require.Len(t, rest, 1)
				require.Equal(t, "c", rest[0].Msg.Payload())
			},
		},
		{
			name: "WithClock(nil) falls back to a real clock (no panic)",
			assert: func(t *testing.T) {
				s, err := memory.NewQueueStore(memory.WithClock(nil))
				require.NoError(t, err)
				require.NoError(t, s.Enqueue(t.Context(), msgin.New[any]("a")))
				got, _ := s.Claim(t.Context(), 1)
				require.Len(t, got, 1)
			},
		},
		{
			name: "EmitsLiveValue is true",
			assert: func(t *testing.T) {
				s, _ := memory.NewQueueStore()
				require.True(t, s.EmitsLiveValue())
			},
		},
		{
			name: "claim with non-positive max returns no deliveries (no panic)",
			assert: func(t *testing.T) {
				s, _ := memory.NewQueueStore()
				_ = s.Enqueue(t.Context(), msgin.New[any]("a"))
				got, err := s.Claim(t.Context(), 0)
				require.NoError(t, err)
				require.Empty(t, got)
				gotNeg, err := s.Claim(t.Context(), -1)
				require.NoError(t, err)
				require.Empty(t, gotNeg)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) { tt.assert(t) })
	}
}
