package memory

import (
	"context"
	"sync"
	"time"

	"github.com/jonboulle/clockwork"
	"github.com/kartaladev/msgin"
)

// QueueStore is an in-memory msgin.ChannelStore: a bounded FIFO with lease/claim
// settlement, carrying live Go values (no codec). It is the fast, DB-free default
// buffer behind a msgin.QueueChannel.
//
// Delivery guarantee: at-least-once within the process lifetime — a Nacked or
// never-settled claim is re-claimable. Buffered and in-flight messages are LOST
// on process exit (at-most-once across a restart). For durability across a
// restart, back the QueueChannel with the SQL ChannelStore.
//
// It starts no goroutine: delayed redelivery is timestamp-based (visibleAt), and
// Block backpressure uses a ctx-aware semaphore.
type QueueStore struct {
	sem      chan struct{} // buffered to capacity; one slot per occupied (ready or inflight) message
	overflow msgin.OverflowPolicy
	clock    clockwork.Clock

	mu       sync.Mutex
	ready    []entry         // FIFO; may include not-yet-visible entries
	inflight map[int64]entry // claimed, keyed by epoch (fence token)
	epoch    int64
}

type entry struct {
	msg       msgin.Message[any]
	visibleAt time.Time
	epoch     int64
}

const defaultCapacity = 1024

var _ msgin.ChannelStore = (*QueueStore)(nil)

// QueueStoreOption configures a QueueStore.
type QueueStoreOption func(*config)

type config struct {
	capacity    int
	capacitySet bool
	overflow    msgin.OverflowPolicy
	clock       clockwork.Clock
}

// WithCapacity bounds the number of occupied messages (ready + in-flight);
// default 1024. A bounded buffer is the safe default — an unbounded in-memory
// queue is an OOM lever (CLAUDE.md fail-safe defaults). An explicit n <= 0 is
// msgin.ErrInvalidCapacity.
func WithCapacity(n int) QueueStoreOption {
	return func(c *config) { c.capacity = n; c.capacitySet = true }
}

// WithOverflow sets the behavior when Enqueue meets a full buffer: OverflowBlock
// (default — backpressure until a slot frees or ctx cancels), OverflowReject
// (returns msgin.ErrOverflowDropped), OverflowDropNewest (silently drops the
// arriving message, returns nil), or OverflowDropOldest (evicts the head to admit
// the newcomer). An unknown value falls back to OverflowBlock (the safe default),
// matching the runtime's convention.
//
// Overflow is evaluated non-atomically against concurrent settlement: under a
// shed policy, an Enqueue racing a concurrent Ack may shed even though that Ack
// frees a slot microseconds later (audit M-3). This is inherent to lock-free
// capacity checks and is acceptable for a shedding policy; use OverflowBlock when
// no message may be dropped.
func WithOverflow(p msgin.OverflowPolicy) QueueStoreOption {
	return func(c *config) { c.overflow = p }
}

// WithClock injects the clock used for delayed-requeue visibility; nil selects a
// real clock. Tests pass clockwork.NewFakeClock().
func WithClock(c clockwork.Clock) QueueStoreOption { return func(cfg *config) { cfg.clock = c } }

// NewQueueStore builds an in-memory ChannelStore with the given options.
func NewQueueStore(opts ...QueueStoreOption) (*QueueStore, error) {
	cfg := config{clock: clockwork.NewRealClock()}
	for _, o := range opts {
		o(&cfg)
	}
	capacity := defaultCapacity
	if cfg.capacitySet {
		if cfg.capacity <= 0 {
			return nil, msgin.ErrInvalidCapacity
		}
		capacity = cfg.capacity
	}
	if cfg.clock == nil {
		cfg.clock = clockwork.NewRealClock()
	}
	return &QueueStore{
		sem:      make(chan struct{}, capacity),
		overflow: cfg.overflow,
		clock:    cfg.clock,
		inflight: map[int64]entry{},
	}, nil
}

// Enqueue appends msg, applying the overflow policy when the buffer is full.
func (s *QueueStore) Enqueue(ctx context.Context, msg msgin.Message[any]) error {
	switch s.overflow {
	case msgin.OverflowDropNewest, msgin.OverflowDropOldest, msgin.OverflowReject:
		select {
		case s.sem <- struct{}{}: // acquired a slot
		default:
			return s.shed(msg) // buffer full — apply the shed policy
		}
	default: // OverflowBlock and any unknown value → backpressure
		select {
		case s.sem <- struct{}{}:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	s.mu.Lock()
	s.ready = append(s.ready, entry{msg: msg, visibleAt: s.clock.Now()})
	s.mu.Unlock()
	return nil
}

// shed handles a full-buffer Enqueue for the non-Block policies (the caller
// reached the `default` select arm — no slot was acquired). DropNewest silently
// drops the newcomer (returns nil); Reject reports ErrOverflowDropped; DropOldest
// evicts the FIFO head (its slot transfers to the newcomer, so occupancy is
// unchanged and the semaphore is untouched) and falls back to a drop when nothing
// is evictable (every entry in-flight). Note: DropOldest evicts s.ready[0], which
// after requeues may be a not-yet-visible (mid-retry) entry rather than the oldest
// visible one — benign, it still frees capacity (audit L-2).
func (s *QueueStore) shed(msg msgin.Message[any]) error {
	if s.overflow == msgin.OverflowDropOldest {
		s.mu.Lock()
		if len(s.ready) > 0 {
			s.ready = s.ready[1:] // evict head; its slot is reused by the newcomer
			s.ready = append(s.ready, entry{msg: msg, visibleAt: s.clock.Now()})
			s.mu.Unlock()
			return nil
		}
		s.mu.Unlock()
		return msgin.ErrOverflowDropped // nothing evictable (all in-flight) → drop
	}
	if s.overflow == msgin.OverflowDropNewest {
		return nil // silently drop the arriving message (distinguishes it from Reject)
	}
	return msgin.ErrOverflowDropped // OverflowReject
}

// Claim leases up to max visible ready entries in FIFO order. A non-positive max
// yields no deliveries (guards make(...) against a negative cap panic — Claim is
// exported and directly callable; audit M-1).
func (s *QueueStore) Claim(_ context.Context, max int) ([]msgin.Delivery, error) {
	if max <= 0 {
		return nil, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.clock.Now()
	out := make([]msgin.Delivery, 0, max)
	kept := s.ready[:0]
	for _, e := range s.ready {
		if len(out) < max && !e.visibleAt.After(now) {
			s.epoch++
			e.epoch = s.epoch
			s.inflight[e.epoch] = e
			out = append(out, msgin.Delivery{
				Msg:  e.msg,
				Ack:  s.ackClosure(e.epoch),
				Nack: s.nackClosure(e.epoch),
			})
			continue
		}
		kept = append(kept, e)
	}
	s.ready = kept
	return out, nil
}

func (s *QueueStore) ackClosure(epoch int64) func(context.Context) error {
	return func(context.Context) error {
		s.mu.Lock()
		_, ok := s.inflight[epoch]
		delete(s.inflight, epoch)
		s.mu.Unlock()
		if ok {
			<-s.sem // release the slot
		}
		return nil
	}
}

func (s *QueueStore) nackClosure(epoch int64) func(context.Context, bool, time.Duration) error {
	return func(_ context.Context, requeue bool, delay time.Duration) error {
		s.mu.Lock()
		e, ok := s.inflight[epoch]
		delete(s.inflight, epoch)
		if !ok {
			s.mu.Unlock()
			return nil // already settled (fence)
		}
		if !requeue {
			s.mu.Unlock()
			<-s.sem // genuine drop → free the slot
			return nil
		}
		e.visibleAt = s.clock.Now().Add(delay)
		e.epoch = 0
		s.ready = append(s.ready, e) // keeps its slot
		s.mu.Unlock()
		return nil
	}
}

// EmitsLiveValue reports true: the store carries live Go values (no codec).
func (s *QueueStore) EmitsLiveValue() bool { return true }
