package expr_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/kartaladev/msgin"
	"github.com/kartaladev/msgin/endpoint"
	"github.com/kartaladev/msgin/expr"
	"github.com/kartaladev/msgin/transform"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// reemittingSource is an EventDrivenSource carrying ONE live-value delivery
// that it RE-EMITS whenever the Consumer requeues it, up to maxRedeliveries. It
// records how the delivery was settled each time.
//
// Re-emission is what makes the acceptance fixture's dead-letter assertion
// load-bearing. Against a source that emits once and stops, a retried message
// simply disappears, so dlq.count()==0 holds for a mis-classified fault too and
// the assertion proves nothing. Here a transient classification really does
// exhaust RetryPolicy.MaxAttempts and really does reach RetryPolicy.DeadLetter
// — mutation-proven (mutant M22: with IsPermanent no longer naming
// ErrPayloadType, dlq.count() becomes 1).
type reemittingSource struct {
	mu       sync.Mutex
	acks     int
	nacks    int
	pending  chan msgin.Delivery
	delivery msgin.Delivery
	cap      int
}

func newReemittingSource(payload any, id string, maxRedeliveries int) *reemittingSource {
	s := &reemittingSource{pending: make(chan msgin.Delivery, maxRedeliveries+1), cap: maxRedeliveries}
	s.delivery = msgin.Delivery{
		Msg:  msgin.New[any](payload, msgin.WithID(id)),
		Ack:  s.ack,
		Nack: s.nack,
	}
	s.pending <- s.delivery
	return s
}

func (s *reemittingSource) EmitsLiveValue() bool { return true }

func (s *reemittingSource) Stream(ctx context.Context, out chan<- msgin.Delivery) error {
	for {
		select {
		case d := <-s.pending:
			select {
			case out <- d:
			case <-ctx.Done():
				return ctx.Err()
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (s *reemittingSource) ack(context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.acks++
	return nil
}

func (s *reemittingSource) nack(_ context.Context, requeue bool, _ time.Duration) error {
	s.mu.Lock()
	s.nacks++
	n := s.nacks
	s.mu.Unlock()
	if requeue && n <= s.cap {
		select {
		case s.pending <- s.delivery:
		default: // capacity is sized to cap+1; a full buffer means the test is over
		}
	}
	return nil
}

func (s *reemittingSource) snapshot() (acks, nacks int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.acks, s.nacks
}

// recordingSink is an OutboundAdapter that counts what it receives and keeps
// the last message. Both the invalid-message sink and the dead-letter sink in
// the fixture are one of these, so "which sink" is an assertion, not an
// inference.
type recordingSink struct {
	mu   sync.Mutex
	msgs []msgin.Message[any]
}

func (r *recordingSink) Send(_ context.Context, m msgin.Message[any]) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.msgs = append(r.msgs, m)
	return nil
}

func (r *recordingSink) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.msgs)
}

// errRec captures the errors a Consumer's hooks report, per event class.
type errRec struct {
	mu   sync.Mutex
	seen map[string][]error
}

func newErrRec() *errRec { return &errRec{seen: map[string][]error{}} }

func (r *errRec) record(event string) func(context.Context, msgin.Message[any], error) {
	return func(_ context.Context, _ msgin.Message[any], err error) {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.seen[event] = append(r.seen[event], err)
	}
}

func (r *errRec) hooks() msgin.Hooks {
	return msgin.Hooks{
		OnRetry:          r.record("retry"),
		OnDeadLetter:     r.record("deadletter"),
		OnInvalidMessage: r.record("invalid"),
		OnAck:            r.record("ack"),
	}
}

func (r *errRec) count(event string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.seen[event])
}

func (r *errRec) first(event string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.seen[event]) == 0 {
		return nil
	}
	return r.seen[event][0]
}

// TestResultTypeMismatch_ReachesInvalidSinkWithoutConsumingARetry is decision
// D-K's acceptance bar, and it is why this module owns a Consumer fixture at
// all. An expression that compiles but EVALUATES to the wrong Go type returns
//
//	fmt.Errorf("%w: expr result %T is not %T", msgin.ErrPayloadType, got, want)
//
// with NO msgin.Permanent wrap. The claim being proved is that the wrap is
// unnecessary rather than merely omitted: msgin.IsPermanent already names
// ErrPayloadType, so a real Consumer with BOTH a RetryPolicy (MaxAttempts 3,
// DeadLetter sink) and a WithInvalidMessageSink must divert the message to the
// invalid sink on the FIRST attempt — never to the dead-letter sink, and
// without spending a retry.
//
// The negative half is the load-bearing half: dlq.count()==0 and
// hooks.count("retry")==0. A same-shaped test that asserted only
// invalid.count()==1 would pass against an implementation that retried the
// message three times first.
func TestResultTypeMismatch_ReachesInvalidSinkWithoutConsumingARetry(t *testing.T) {
	// A=any, so expr's compiler cannot type-check the result: `1 + 1` yields a
	// Go int while B is string. The fault is therefore deferred to EVALUATION,
	// which is exactly the class under test.
	tf, err := expr.Transformer[any, string]("1 + 1")
	require.NoError(t, err)

	// A real endpoint pipeline: the Consumer's handler drives a transform.Transform
	// Step, so the error travels the shipped endpoint path into settlement.
	forwarded := &recordingSink{}
	step := transform.Transform(tf)
	handler := func(ctx context.Context, m msgin.Message[any]) error {
		return step(msgin.HandlerFunc(forwarded.Send)).Handle(ctx, m)
	}

	src := newReemittingSource(0, "m1", 5)
	invalid, dlq := &recordingSink{}, &recordingSink{}
	rec := newErrRec()

	c, err := endpoint.NewConsumer[any](src, handler,
		endpoint.WithRetryPolicy[any](msgin.RetryPolicy{MaxAttempts: 3, DeadLetter: dlq}),
		endpoint.WithInvalidMessageSink[any](invalid),
		endpoint.WithHooks[any](rec.hooks()))
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- c.Run(ctx) }()
	// Terminate on either terminal outcome, so a WRONG classification is
	// observed rather than timed out: the invalid sink is the expected one, the
	// dead-letter sink is what a mis-classified fault reaches after exhausting
	// MaxAttempts.
	require.Eventually(t, func() bool { return invalid.count()+dlq.count() >= 1 },
		2*time.Second, 5*time.Millisecond, "the delivery must reach a terminal sink")
	cancel()
	<-done

	acks, nacks := src.snapshot()

	// The error itself, as the runtime saw it.
	got := rec.first("invalid")
	require.Error(t, got, "the invalid-message hook must have fired with the mismatch error")
	assert.ErrorIs(t, got, msgin.ErrPayloadType, "D-K: the result-type fault reuses root's sentinel")
	assert.True(t, msgin.IsPermanent(got),
		"D-K: IsPermanent already names ErrPayloadType, so no msgin.Permanent wrap is needed")
	assert.Contains(t, got.Error(), "expr result",
		"the error string carries the discriminator that separates the expression side from the payload side")

	// Where it went, and what it did NOT cost.
	assert.Equal(t, 1, invalid.count(), "diverted to the invalid-message sink")
	assert.Zero(t, dlq.count(), "never dead-lettered")
	assert.Zero(t, rec.count("retry"), "a deterministic fault must not consume a retry attempt")
	assert.Zero(t, rec.count("deadletter"))
	assert.Equal(t, 1, rec.count("invalid"))
	assert.Equal(t, 1, acks, "a diverted message is Acked at the source, not left in flight")
	assert.Zero(t, nacks, "a Nack would be a redelivery request — the whole thing D-K forbids here")
	assert.Zero(t, forwarded.count(), "nothing reached the downstream handler")
}
