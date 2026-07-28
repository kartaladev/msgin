package endpoint

import (
	"context"
	"sync"
	"time"

	"github.com/jonboulle/clockwork"
)

// attemptEntry is one tracked message: its running attempt count plus the clock
// time of the most recent observe, used by the TTL sweep to age out idle ids.
type attemptEntry struct {
	count    int
	lastSeen time.Time
}

// attemptTracker counts delivery attempts per message id for sources without a
// native msgin.delivery-count header. Entries are evicted on terminal settle
// (Ack/DLQ/invalid) and, additionally, reclaimed by a periodic TTL sweep once an
// id has been idle for >= ttl (ADR 0008 D8) — bounding the map so a stream of
// distinct one-shot ids cannot grow it without limit. An id still being
// redelivered is re-observed each attempt (refreshing lastSeen), so it is never
// swept mid-flight: the poison count cannot reset while a message is in flight
// (NF-2).
type attemptTracker struct {
	clock clockwork.Clock
	ttl   time.Duration
	mu    sync.Mutex
	m     map[string]attemptEntry
}

func newAttemptTracker(clock clockwork.Clock, ttl time.Duration) *attemptTracker {
	return &attemptTracker{clock: clock, ttl: ttl, m: make(map[string]attemptEntry)}
}

// observe records one more attempt for id, refreshes its lastSeen, and returns
// the new count (1-based).
func (t *attemptTracker) observe(id string) int {
	t.mu.Lock()
	defer t.mu.Unlock()
	e := t.m[id]
	e.count++
	e.lastSeen = t.clock.Now()
	t.m[id] = e
	return e.count
}

// evict forgets id (call only on terminal settle).
func (t *attemptTracker) evict(id string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.m, id)
}

// sweep reclaims entries idle for >= ttl. An actively-redelivering id is
// re-observed each attempt (gap <= Backoff.Max << ttl), so it is never swept
// mid-flight (NF-2); only ids that stopped arriving age out.
func (t *attemptTracker) sweep() {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := t.clock.Now()
	for id, e := range t.m {
		if now.Sub(e.lastSeen) >= t.ttl {
			delete(t.m, id)
		}
	}
}

// sweepLoop runs the periodic sweep until ctx is done. ttl is always > 0:
// NewConsumer defaults the unset case to defaultAttemptTTL and validates an
// explicit WithAttemptTTL, rejecting a non-positive value with
// ErrInvalidAttemptTTL (ADR 0009 D3) — so the tracker always receives a
// positive ttl and sweepLoop needs no ttl<=0 guard (it would be uncoverable
// dead code under the blackbox coverage gate).
func (t *attemptTracker) sweepLoop(ctx context.Context) {
	ticker := t.clock.NewTicker(t.ttl)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.Chan():
			t.sweep()
		}
	}
}
