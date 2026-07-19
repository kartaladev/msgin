package msgin_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/kartaladev/msgin"
	"github.com/kartaladev/msgin/adapter/memory"
	"github.com/stretchr/testify/require"
)

// fakeStore is a minimal in-test ChannelStore: FIFO ready slice + inflight map.
type fakeStore struct {
	mu       sync.Mutex
	ready    []msgin.Message[any]
	inflight map[string]msgin.Message[any]
	live     bool
	claimErr error
}

func newFakeStore(live bool) *fakeStore {
	return &fakeStore{inflight: map[string]msgin.Message[any]{}, live: live}
}

func (f *fakeStore) Enqueue(_ context.Context, m msgin.Message[any]) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ready = append(f.ready, m)
	return nil
}

func (f *fakeStore) Claim(_ context.Context, max int) ([]msgin.Delivery, error) {
	if f.claimErr != nil {
		return nil, f.claimErr
	}
	if max <= 0 {
		return nil, nil
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	n := min(max, len(f.ready))
	out := make([]msgin.Delivery, 0, n)
	for _, m := range f.ready[:n] {
		id := m.ID()
		f.inflight[id] = m
		out = append(out, msgin.Delivery{
			Msg:  m,
			Ack:  func(context.Context) error { f.mu.Lock(); delete(f.inflight, id); f.mu.Unlock(); return nil },
			Nack: func(context.Context, bool, time.Duration) error { return nil },
		})
	}
	f.ready = f.ready[n:]
	return out, nil
}

func (f *fakeStore) EmitsLiveValue() bool { return f.live }

// nativeFake is a fakeStore that ALSO advertises native reliability, so the
// channel's forwarding (audit M-2) can be exercised in both directions.
type nativeFake struct{ *fakeStore }

func (nativeFake) NativeRedelivery() bool { return true }
func (nativeFake) NativeDeadLetter() bool { return false }

func TestQueueChannel(t *testing.T) {
	tests := []struct {
		name   string
		assert func(t *testing.T)
	}{
		{
			name: "nil store is ErrNilStore",
			assert: func(t *testing.T) {
				_, err := msgin.NewQueueChannel(nil)
				require.ErrorIs(t, err, msgin.ErrNilStore)
			},
		},
		{
			name: "Send enqueues; Poll returns it as a Delivery, capped at max",
			assert: func(t *testing.T) {
				qc, err := msgin.NewQueueChannel(newFakeStore(true))
				require.NoError(t, err)
				require.NoError(t, qc.Send(t.Context(), msgin.New[any]("a")))
				require.NoError(t, qc.Send(t.Context(), msgin.New[any]("b")))
				got, err := qc.Poll(t.Context(), 1)
				require.NoError(t, err)
				require.Len(t, got, 1)
				require.Equal(t, "a", got[0].Msg.Payload())
			},
		},
		{
			name: "Poll surfaces the store's error with no deliveries (invariant 2)",
			assert: func(t *testing.T) {
				fs := newFakeStore(true)
				fs.claimErr = errors.New("boom")
				qc, err := msgin.NewQueueChannel(fs)
				require.NoError(t, err)
				got, err := qc.Poll(t.Context(), 10)
				require.Error(t, err)
				require.Empty(t, got)
			},
		},
		{
			name: "EmitsLiveValue delegates to the store",
			assert: func(t *testing.T) {
				live, _ := msgin.NewQueueChannel(newFakeStore(true))
				wire, _ := msgin.NewQueueChannel(newFakeStore(false))
				require.True(t, live.EmitsLiveValue())
				require.False(t, wire.EmitsLiveValue())
			},
		},
		{
			name: "NativeReliability forwards when the store implements it, else false",
			assert: func(t *testing.T) {
				plain, _ := msgin.NewQueueChannel(newFakeStore(true)) // fakeStore is NOT NativeReliability
				require.False(t, plain.NativeRedelivery())
				require.False(t, plain.NativeDeadLetter())
				native, _ := msgin.NewQueueChannel(nativeFake{newFakeStore(false)})
				require.True(t, native.NativeRedelivery())
				require.False(t, native.NativeDeadLetter())
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) { tt.assert(t) })
	}
}

// ExampleQueueChannel demonstrates the pollable Send/Poll/Ack round-trip against
// a memory-backed store, driven directly (rather than via Consumer.Run) so the
// output ordering is deterministic.
func ExampleQueueChannel() {
	store, _ := memory.NewQueueStore()
	qc, _ := msgin.NewQueueChannel(store)

	_ = qc.Send(context.Background(), msgin.New[any]("hello"))
	_ = qc.Send(context.Background(), msgin.New[any]("world"))

	deliveries, _ := qc.Poll(context.Background(), 10)
	for _, d := range deliveries {
		fmt.Println(d.Msg.Payload())
		_ = d.Ack(context.Background())
	}
	// Output:
	// hello
	// world
}
