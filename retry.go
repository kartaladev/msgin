package msgin

import (
	"context"
	"sync"
	"time"
)

// RetryPolicy governs how the runtime settles a failed delivery (spec §7).
//
//   - MaxAttempts == 0 : retry forever (no dead-letter).
//   - MaxAttempts  > 0 : after that many delivery attempts a still-failing
//     message is diverted to DeadLetter (required); DeadLetter may be any
//     OutboundAdapter, including another msgin adapter.
//   - Backoff nil       : immediate redelivery (zero delay).
//
// The zero value is valid and means "retry forever, immediately, no DLQ".
type RetryPolicy struct {
	MaxAttempts int
	Backoff     BackoffStrategy
	DeadLetter  OutboundAdapter
}

// Validate reports whether the policy is internally consistent. A finite
// MaxAttempts requires a DeadLetter; a negative MaxAttempts is invalid. Called
// by NewConsumer so a bad policy fails at construction (spec §5).
func (p RetryPolicy) Validate() error {
	if p.MaxAttempts < 0 {
		return ErrInvalidMaxAttempts
	}
	if p.MaxAttempts > 0 && p.DeadLetter == nil {
		return ErrNoDeadLetter
	}
	return nil
}

// delayFor returns the redelivery delay for the given 1-based attempt count,
// converting to the 0-based retry index the BackoffStrategy expects. A nil
// Backoff means immediate redelivery.
func (p RetryPolicy) delayFor(attempt int) time.Duration {
	if p.Backoff == nil {
		return 0
	}
	return p.Backoff.Delay(attempt - 1)
}

// Hooks are optional, nil-safe callbacks fired on the operationally important
// settlement events (spec §7 observability). The error argument carries the
// triggering error (nil on a successful Ack).
type Hooks struct {
	OnRetry          func(ctx context.Context, msg Message[any], err error)
	OnDeadLetter     func(ctx context.Context, msg Message[any], err error)
	OnInvalidMessage func(ctx context.Context, msg Message[any], err error)
	OnAck            func(ctx context.Context, msg Message[any], err error)
}

// attemptTracker counts delivery attempts per message id for sources without a
// native msgin.delivery-count header. Entries are evicted only on terminal
// settle (Ack/DLQ/invalid), never while a message is still being redelivered
// (NF-2), so a poison count cannot reset mid-flight.
type attemptTracker struct {
	mu sync.Mutex
	m  map[string]int
}

func newAttemptTracker() *attemptTracker { return &attemptTracker{m: make(map[string]int)} }

// observe records one more attempt for id and returns the new count (1-based).
func (t *attemptTracker) observe(id string) int {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.m[id]++
	return t.m[id]
}

// evict forgets id (call only on terminal settle).
func (t *attemptTracker) evict(id string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.m, id)
}
