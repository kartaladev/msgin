package msgin

import (
	"context"
	"time"
)

// creditGate bounds claimed-but-unsettled messages: a buffered channel used as a
// counting semaphore of capacity n (acquire = send a token, release = receive
// one). The capacity IS the whole bound (spec §7.4.1) — for a streaming source
// this realizes "the bounded buffer of size n as the credit pool" (ADR 0008 D3).
type creditGate struct {
	tokens chan struct{}
}

// newCreditGate builds a credit gate holding n credits (n >= 1, validated by
// NewConsumer via ErrInvalidMaxInFlight).
func newCreditGate(n int) *creditGate {
	return &creditGate{tokens: make(chan struct{}, n)}
}

// acquire blocks until a credit is free or ctx is done, returning ctx.Err() in
// the latter case (so a delivery whose ctx cancels before it takes a credit
// never holds one — nothing to release).
func (g *creditGate) acquire(ctx context.Context) error {
	select {
	case g.tokens <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// tryAcquire takes a credit without blocking; false if none is free. Reserved
// for the non-blocking overflow policies wired in Task 6.
func (g *creditGate) tryAcquire() bool {
	select {
	case g.tokens <- struct{}{}:
		return true
	default:
		return false
	}
}

// release returns exactly one credit. Callers wrap this in sync.OnceFunc per
// delivery so it fires exactly once across every settle path (ADR 0008 D4, NF-5).
func (g *creditGate) release() { <-g.tokens }

// managedDelivery is a Delivery whose Ack/Nack release the delivery's credit
// (release-first, via manage). release is the same sync.OnceFunc, exposed so the
// worker can defer it as a panic-safe net.
type managedDelivery struct {
	Delivery
	release func()
}

// manage wraps d's settle closures so each releases the credit BEFORE invoking
// the original closure — essential so a Nack(requeue) that synchronously
// re-injects the message (memory) does not deadlock waiting for the credit it
// still holds (ADR 0008 D4). release is idempotent (sync.OnceFunc), so the
// wrapped settle plus the worker's deferred release net exactly one release.
func manage(d Delivery, release func()) managedDelivery {
	origAck, origNack := d.Ack, d.Nack
	d.Ack = func(ctx context.Context) error {
		release()
		return origAck(ctx)
	}
	d.Nack = func(ctx context.Context, requeue bool, delay time.Duration) error {
		release()
		return origNack(ctx, requeue, delay)
	}
	return managedDelivery{Delivery: d, release: release}
}
