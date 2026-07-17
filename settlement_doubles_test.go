package msgin_test

import (
	"bytes"
	"context"
	"sync"
	"time"

	"github.com/kartaladev/msgin"
	"github.com/kartaladev/msgin/adapter/memory"
)

// lockedBuffer is a goroutine-safe io.Writer for capturing slog output emitted
// from a worker goroutine and reading it back after the worker has joined.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// recordingSink is an OutboundAdapter that records sends and can be forced to fail.
type recordingSink struct {
	mu       sync.Mutex
	sent     []msgin.Message[any]
	failWith error
}

func (s *recordingSink) Send(_ context.Context, m msgin.Message[any]) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failWith != nil {
		return s.failWith
	}
	s.sent = append(s.sent, m)
	return nil
}

func (s *recordingSink) count() int { s.mu.Lock(); defer s.mu.Unlock(); return len(s.sent) }

// settle records how a Delivery was settled.
type settle struct {
	mu      sync.Mutex
	acks    int
	nacks   int
	requeue []bool
	delays  []time.Duration
}

func (s *settle) ack(context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.acks++
	return nil
}

func (s *settle) nack(_ context.Context, requeue bool, d time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nacks++
	s.requeue = append(s.requeue, requeue)
	s.delays = append(s.delays, d)
	return nil
}

func (s *settle) snapshot() (acks, nacks int, delays []time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.acks, s.nacks, append([]time.Duration(nil), s.delays...)
}

// scriptedSource is a StreamingSource that emits preset deliveries once, then
// blocks until ctx is done. It declares EmitsLiveValue so the live-value path
// is used and no codec is required. Nack-requeue does not re-emit here; the
// settle recorder simply records the settlement outcome.
type scriptedSource struct {
	deliveries []msgin.Delivery
}

func (s *scriptedSource) EmitsLiveValue() bool { return true }

func (s *scriptedSource) Stream(ctx context.Context, out chan<- msgin.Delivery) error {
	for _, d := range s.deliveries {
		select {
		case out <- d:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	<-ctx.Done()
	return ctx.Err()
}

// newSettleDelivery builds a live-value Delivery whose Ack/Nack are recorded by st.
func newSettleDelivery(payload any, id string, st *settle) msgin.Delivery {
	m := msgin.New[any](payload, msgin.WithID(id))
	return msgin.Delivery{Msg: m, Ack: st.ack, Nack: st.nack}
}

// reemittingSource re-emits its single delivery whenever it is Nacked, capped so
// the test terminates once the message is dead-lettered.
type reemittingSource struct {
	st      *settle
	id      string
	payload any
}

func (s *reemittingSource) EmitsLiveValue() bool { return true }

func (s *reemittingSource) Stream(ctx context.Context, out chan<- msgin.Delivery) error {
	for {
		before := func() int { _, n, _ := s.st.snapshot(); return n }()
		d := msgin.Delivery{
			Msg:  msgin.New[any](s.payload, msgin.WithID(s.id)),
			Ack:  s.st.ack,
			Nack: s.st.nack,
		}
		select {
		case out <- d:
		case <-ctx.Done():
			return ctx.Err()
		}
		// Wait until this delivery is settled (ack or nack) before re-emitting.
		for {
			acks, nacks, _ := s.st.snapshot()
			if acks > 0 || nacks > before {
				break
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Millisecond):
			}
		}
		if acks, _, _ := s.st.snapshot(); acks > 0 {
			<-ctx.Done() // terminal (dead-lettered/acked): stop re-emitting
			return ctx.Err()
		}
	}
}

// controllableSource is a StreamingSource the test drives one delivery at a
// time: deliver blocks until Stream has forwarded the delivery downstream, so a
// test can withhold an id across a TTL sweep and then re-deliver it, making the
// sweep-vs-redeliver ordering deterministic (used by the Task 7 sweep tests).
type controllableSource struct {
	emit chan msgin.Delivery
}

func newControllableSource() *controllableSource {
	return &controllableSource{emit: make(chan msgin.Delivery)}
}

func (s *controllableSource) EmitsLiveValue() bool { return true }

func (s *controllableSource) Stream(ctx context.Context, out chan<- msgin.Delivery) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case d := <-s.emit:
			select {
			case out <- d:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
}

func (s *controllableSource) deliver(ctx context.Context, d msgin.Delivery) {
	select {
	case s.emit <- d:
	case <-ctx.Done():
	}
}

// nativeScriptedSource emits one delivery then blocks; declares no native reliability.
type nativeScriptedSource struct{ d msgin.Delivery }

func (s *nativeScriptedSource) EmitsLiveValue() bool { return true }
func (s *nativeScriptedSource) Stream(ctx context.Context, out chan<- msgin.Delivery) error {
	select {
	case out <- s.d:
	case <-ctx.Done():
		return ctx.Err()
	}
	<-ctx.Done()
	return ctx.Err()
}

// nativeDLQSource re-emits like reemittingSource but reports a native dead-letter
// pipeline, so the runtime DLQ branch must be skipped.
type nativeDLQSource struct {
	st      *settle
	id      string
	payload any
}

func (s *nativeDLQSource) EmitsLiveValue() bool   { return true }
func (s *nativeDLQSource) NativeRedelivery() bool { return true }
func (s *nativeDLQSource) NativeDeadLetter() bool { return true }
func (s *nativeDLQSource) Stream(ctx context.Context, out chan<- msgin.Delivery) error {
	select {
	case out <- msgin.Delivery{Msg: msgin.New[any](s.payload, msgin.WithID(s.id)), Ack: s.st.ack, Nack: s.st.nack}:
	case <-ctx.Done():
		return ctx.Err()
	}
	<-ctx.Done()
	return ctx.Err()
}

// scriptedBreaker is a deterministic CircuitBreaker test double: Allow returns a
// preset sequence of values (the last repeats once exhausted) so a test can pin
// the exact open/closed decision each caller sees — the real breaker's clockwork
// transitions are async, which is fine for the state-machine tests but too racy
// to deterministically place a message between the ingress admit and the worker
// dispatch gate. It records every Record(success) call for assertion.
type scriptedBreaker struct {
	mu      sync.Mutex
	allows  []bool
	idx     int
	records []bool
	wake    chan struct{}
}

func newScriptedBreaker(allows ...bool) *scriptedBreaker {
	return &scriptedBreaker{allows: allows, wake: make(chan struct{})}
}

func (b *scriptedBreaker) Allow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.idx >= len(b.allows) {
		return b.allows[len(b.allows)-1] // last scripted value repeats
	}
	v := b.allows[b.idx]
	b.idx++
	return v
}

func (b *scriptedBreaker) Record(success bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.records = append(b.records, success)
}

func (b *scriptedBreaker) HalfOpen() <-chan struct{} {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.wake
}

func (b *scriptedBreaker) recorded() []bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]bool(nil), b.records...)
}

// countingSource is a StreamingSource backed by a real memory.Broker, used by
// the credit-gate tests to catch over-pull: it forwards the broker's live
// deliveries (whose Nack-requeue synchronously re-injects via Send) unchanged,
// so the runtime's credit accounting is exercised against a real re-injecting
// adapter rather than a stub.
type countingSource struct {
	broker *memory.Broker
}

func (s *countingSource) EmitsLiveValue() bool { return true }
func (s *countingSource) Stream(ctx context.Context, out chan<- msgin.Delivery) error {
	return s.broker.Stream(ctx, out)
}

// finiteSource emits its preset deliveries once and then returns nil — the
// stream ends on its own rather than blocking until ctx is cancelled. It drives
// the ingest "source stream ended" exit (rawCh closed while ctx is still live)
// and Run's normal, cancel-free completion returning a nil error.
type finiteSource struct {
	deliveries []msgin.Delivery
}

func (s *finiteSource) EmitsLiveValue() bool { return true }

func (s *finiteSource) Stream(ctx context.Context, out chan<- msgin.Delivery) error {
	for _, d := range s.deliveries {
		select {
		case out <- d:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil // stream complete; Run closes rawCh and ingest exits on the closed read
}

// hookRec records which observability hooks fired, keyed by event name.
type hookRec struct {
	mu sync.Mutex
	n  map[string]int
}

func newHookRec() *hookRec { return &hookRec{n: map[string]int{}} }

func (r *hookRec) inc(event string)       { r.mu.Lock(); defer r.mu.Unlock(); r.n[event]++ }
func (r *hookRec) count(event string) int { r.mu.Lock(); defer r.mu.Unlock(); return r.n[event] }

// hooks returns a msgin.Hooks whose callbacks record into r.
func (r *hookRec) hooks() msgin.Hooks {
	return msgin.Hooks{
		OnAck:            func(context.Context, msgin.Message[any], error) { r.inc("ack") },
		OnRetry:          func(context.Context, msgin.Message[any], error) { r.inc("retry") },
		OnInvalidMessage: func(context.Context, msgin.Message[any], error) { r.inc("invalid") },
		OnDeadLetter:     func(context.Context, msgin.Message[any], error) { r.inc("deadletter") },
	}
}
