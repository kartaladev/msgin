package msgin

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"time"

	"github.com/jonboulle/clockwork"
)

// Handler consumes a typed message. nil = success (Ack); non-nil = failure.
type Handler[T any] func(ctx context.Context, msg Message[T]) error

// Consumer runs a flow until its context is cancelled.
type Consumer[T any] interface {
	Run(ctx context.Context) error
}

// ConsumerOption configures NewConsumer.
type ConsumerOption[T any] func(*consumerConfig[T])

type consumerConfig[T any] struct {
	concurrency     int
	codec           PayloadCodec[T]
	codecSet        bool
	policy          RetryPolicy
	invalidSink     OutboundAdapter
	logger          *slog.Logger
	hooks           Hooks
	shutdownTimeout time.Duration
	clock           clockwork.Clock
}

// WithConcurrency sets the worker-pool size (default 1).
func WithConcurrency[T any](n int) ConsumerOption[T] {
	return func(o *consumerConfig[T]) { o.concurrency = n }
}

// WithConsumerCodec sets the payload codec for a wire source (default JSON).
func WithConsumerCodec[T any](c PayloadCodec[T]) ConsumerOption[T] {
	return func(o *consumerConfig[T]) { o.codec = c; o.codecSet = true }
}

// WithRetryPolicy sets the settlement policy (default: retry forever, immediate).
func WithRetryPolicy[T any](p RetryPolicy) ConsumerOption[T] {
	return func(o *consumerConfig[T]) { o.policy = p }
}

// WithInvalidMessageSink sets where permanent/undecodable messages are diverted.
// If unset, such messages are logged and discarded (ADR 0007 D7).
func WithInvalidMessageSink[T any](out OutboundAdapter) ConsumerOption[T] {
	return func(o *consumerConfig[T]) { o.invalidSink = out }
}

// WithLogger injects the structured logger (default: a discard logger).
func WithLogger[T any](l *slog.Logger) ConsumerOption[T] {
	return func(o *consumerConfig[T]) { o.logger = l }
}

// WithHooks sets the observability callbacks (all nil-safe).
func WithHooks[T any](h Hooks) ConsumerOption[T] {
	return func(o *consumerConfig[T]) { o.hooks = h }
}

// WithShutdownTimeout bounds the drain on cancel. d <= 0 resolves to a finite
// default (ADR 0007 D9) rather than an unbounded wait.
func WithShutdownTimeout[T any](d time.Duration) ConsumerOption[T] {
	return func(o *consumerConfig[T]) { o.shutdownTimeout = d }
}

// WithConsumerClock injects the clock used for shutdown timing (default: real).
// Named to avoid colliding with the MessageOption WithClock in this package
// (ADR 0007 D10).
func WithConsumerClock[T any](c clockwork.Clock) ConsumerOption[T] {
	return func(o *consumerConfig[T]) { o.clock = c }
}

type consumer[T any] struct {
	src             StreamingSource
	handler         Handler[T]
	codec           PayloadCodec[T]
	liveValue       bool
	workers         int
	policy          RetryPolicy
	invalidSink     OutboundAdapter
	logger          *slog.Logger
	hooks           Hooks
	shutdownTimeout time.Duration
	clock           clockwork.Clock
	native          NativeReliability
	tracker         *attemptTracker
}

// defaultDeliveryBuffer sizes the hand-off channel between the source's
// Stream loop and the worker pool: bounded hand-off; a configurable buffer
// arrives with flow-control (Plan 003).
const defaultDeliveryBuffer = 1

// defaultShutdownTimeout bounds the drain when WithShutdownTimeout is unset or
// non-positive. The drain is ALWAYS finite (C1): an unbounded drain violates the
// "graceful shutdown within a deadline" constraint and can deadlock on an
// adapter whose Nack blocks on a channel the stopped Stream no longer reads.
const defaultShutdownTimeout = 30 * time.Second

// NewConsumer validates the source and options, and builds a Consumer.
func NewConsumer[T any](src any, h Handler[T], opts ...ConsumerOption[T]) (Consumer[T], error) {
	if src == nil {
		return nil, ErrNilAdapter
	}
	cfg := consumerConfig[T]{
		concurrency: 1,
		clock:       clockwork.NewRealClock(),
		logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	if cfg.concurrency < 1 {
		return nil, ErrInvalidConcurrency
	}
	if err := cfg.policy.Validate(); err != nil {
		return nil, err
	}
	codec, live, err := resolveCodec[T](src, cfg.codec, cfg.codecSet)
	if err != nil {
		return nil, err
	}

	stream, ok := src.(StreamingSource)
	if !ok {
		// PollingSource is wired in Plan 004; anything else is unsupported.
		if _, isPoll := src.(PollingSource); isPoll {
			return nil, ErrUnsupportedSource // TODO(Plan 004): drive via the Poller
		}
		return nil, ErrUnsupportedSource
	}
	native, ok := any(stream).(NativeReliability)
	if !ok {
		native = noNativeReliability{}
	}
	return &consumer[T]{
		src: stream, handler: h, codec: codec, liveValue: live,
		workers: cfg.concurrency,
		policy:  cfg.policy, invalidSink: cfg.invalidSink, logger: cfg.logger,
		hooks: cfg.hooks, shutdownTimeout: cfg.shutdownTimeout, clock: cfg.clock,
		native: native, tracker: newAttemptTracker(),
	}, nil
}

// Run starts the worker pool, then blocks streaming deliveries from the source
// until ctx is cancelled, draining in-flight deliveries before it returns.
//
// Shutdown is ALWAYS bounded and Run ALWAYS returns (C1, ADR 0007 D9). On
// cancel the source's Stream returns and the drain begins under two contexts
// detached from the parent (via context.WithoutCancel) so in-flight handlers
// and their settle closures (Ack/Nack) can complete against a durable backend
// even though the parent is done. The drain waits at most a finite timeout —
// WithShutdownTimeout(d) with d > 0, else defaultShutdownTimeout — measured on
// the injected clock. If the timeout expires first, both detached contexts are
// cancelled: cooperative handlers abort (their ctx.Err() is classified transient
// and Nacked) and any settle blocked on a non-accepting backend (e.g. the memory
// adapter's Nack re-enqueue Send onto a channel the stopped Stream no longer
// reads) is released, so the pool always joins with no goroutine leak. A handler
// that ignores its context is still bounded — the deadline releases its
// subsequent settle — but a handler that never returns at all is bounded only by
// WithHandlerTimeout (Plan 003).
func (c *consumer[T]) Run(ctx context.Context) error {
	deliveries := make(chan Delivery, defaultDeliveryBuffer)

	// Both contexts are detached from the parent (so settles/handlers survive
	// parent cancel and can finish the drain) but cancellable, so the deadline
	// can release a settle stuck on a non-accepting backend (C1).
	settleCtx, cancelSettle := context.WithCancel(context.WithoutCancel(ctx))
	defer cancelSettle()
	drainCtx, cancelDrain := context.WithCancel(context.WithoutCancel(ctx))
	defer cancelDrain()

	var wg sync.WaitGroup
	wg.Add(c.workers)
	for i := 0; i < c.workers; i++ {
		go func() {
			defer wg.Done()
			for d := range deliveries {
				if drainCtx.Err() != nil {
					// Deadline already expired: don't start new work, Nack it.
					// settleCtx is cancelled too, so this returns promptly.
					c.finish(d.Nack(settleCtx, true, 0))
					continue
				}
				c.dispatch(drainCtx, settleCtx, d)
			}
		}()
	}

	streamErr := c.src.Stream(ctx, deliveries) // blocks until ctx is cancelled
	close(deliveries)

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()

	// The drain is ALWAYS finite (C1): a non-positive timeout uses the default.
	timeout := c.shutdownTimeout
	if timeout <= 0 {
		timeout = defaultShutdownTimeout
	}
	select {
	case <-done:
	case <-c.clock.After(timeout):
		cancelDrain()  // cooperative handlers abort (ctx.Err() -> transient -> Nack)
		cancelSettle() // release any settle stuck on a non-accepting backend (e.g. memory Send)
		<-done
	}
	return streamErr
}

// dispatch runs the guarded settlement switch (spec §7) for one delivery. It
// takes two contexts: ctx drives the handler (drainCtx — cancelled at
// shutdown so a cooperative handler aborts), while settleCtx drives the
// settle closures (Ack/Nack/sink) and stays alive (but cancellable) during
// drain (ADR 0007 D9) — Run passes these as two genuinely distinct contexts.
//
// The spec §7 branch order is expressed with early returns (not a single switch)
// so the transient path can lazily consult the attempt tracker (M8: the tracker
// is never touched on the success/decode/permanent paths). Branch order:
//
//  1. decode failed (before the handler) → invalid via divert (OnInvalidMessage).
//  2. err == nil → Ack; OnAck fires and the tracker evicts only if the Ack
//     itself succeeds (gated, like divert below) so a failed Ack does not
//     drop the attempt count.
//  3. isPermanent(err) → invalid via divert (Permanent(err), or a handler that
//     returned ErrPayloadDecode/ErrPayloadType).
//  4. finite-exhausted (MaxAttempts > 0 && n >= MaxAttempts && !native DLQ) →
//     DeadLetter divert (+ OnDeadLetter), after a lazy n := c.attempts(d).
//  5. default (transient) → Nack redeliver (+ OnRetry), delayed by
//     c.policy.delayFor(n).
func (c *consumer[T]) dispatch(ctx, settleCtx context.Context, d Delivery) {
	id := d.Msg.ID()

	payload, derr := c.decode(d.Msg)
	if derr != nil {
		// Decode failure = permanent invalid message. Sink-attempt 1.
		if c.divert(settleCtx, c.invalidSink, d, c.hooks.OnInvalidMessage, derr, 1) {
			c.tracker.evict(id)
		}
		return
	}

	msg := Message[T]{payload: payload, headers: d.Msg.headers}
	err := c.safeHandle(ctx, msg)

	if err == nil {
		// Evict/fire only if the Ack itself succeeded (mirrors divert's gated
		// eviction below): an Ack that fails must NOT drop the attempt count,
		// so a source that redelivers an unacked message keeps climbing toward
		// MaxAttempts instead of silently restarting from 1.
		ackErr := d.Ack(settleCtx)
		c.finish(ackErr)
		if ackErr == nil {
			c.safeFire(c.hooks.OnAck, settleCtx, d.Msg, nil)
			c.tracker.evict(id)
		}
		return
	}
	if isPermanent(err) {
		// Permanent (Permanent(err), or a handler that returned
		// ErrPayloadDecode/ErrPayloadType) → invalid sink. Sink-attempt 1.
		// Note (M8): the attempt tracker is deliberately NOT consulted here.
		if c.divert(settleCtx, c.invalidSink, d, c.hooks.OnInvalidMessage, err, 1) {
			c.tracker.evict(id)
		}
		return
	}
	// Transient path only: NOW compute the attempt count (M8 — lazy; the
	// tracker is never touched on the success/decode/permanent paths).
	n := c.attempts(d)
	switch {
	case c.policy.MaxAttempts > 0 && n >= c.policy.MaxAttempts && !c.native.NativeDeadLetter():
		if c.divert(settleCtx, c.policy.DeadLetter, d, c.hooks.OnDeadLetter, err, n) {
			c.tracker.evict(id)
		}
	default:
		c.safeFire(c.hooks.OnRetry, settleCtx, d.Msg, err)
		c.finish(d.Nack(settleCtx, true, c.policy.delayFor(n)))
	}
}

// attempts returns the 1-based delivery-attempt count for d: the native
// msgin.delivery-count header if present, else the runtime tracker (incremented
// once per delivery). Called once per failed dispatch (NF-2), and only on the
// transient path (M8) so the success/permanent paths never touch the tracker.
func (c *consumer[T]) attempts(d Delivery) int {
	if dc, ok := d.Msg.Headers().Int(HeaderDeliveryCount); ok {
		return dc
	}
	return c.tracker.observe(d.Msg.ID())
}

// divert sends d.Msg to sink and settles it safely, upholding NF-3 (no message
// loss). Three outcomes:
//
//   - nil sink → discarding IS the terminal invalid event (ADR 0007 D7): log a
//     loud WARN, fire terminalHook, Ack. Eviction is gated on the Ack succeeding.
//   - sink.Send FAILS → the message was NOT diverted, so it is retried, not
//     terminally settled: fire OnRetry (with the CLASSIFICATION cause, never the
//     terminal hook and never the send error — no terminal event happened) and
//     Nack with a NON-ZERO backoff delay c.policy.delayFor(attempt) (spec §7
//     backoff(sinkAttempts), I6) so a down sink is not hammered. Returns false so
//     the caller keeps the tracker entry (the message is still in flight).
//   - sink accepts → the terminal divert happened: fire terminalHook with cause,
//     Ack. Eviction is gated on the Ack succeeding.
//
// The bool return gates the caller's tracker eviction (D8): true = terminally
// settled AND source-Acked, false = still in flight (kept for redelivery).
func (c *consumer[T]) divert(ctx context.Context, sink OutboundAdapter, d Delivery, terminalHook func(context.Context, Message[any], error), cause error, attempt int) bool {
	if sink == nil {
		// nil sink: discarding is the terminal invalid event (ADR 0007 D7).
		c.logger.Warn("msgin: discarding message; no invalid-message sink configured", "id", d.Msg.ID())
		c.safeFire(terminalHook, ctx, d.Msg, cause)
		ackErr := d.Ack(ctx)
		c.finish(ackErr)
		// Gate eviction on the Ack, mirroring the dispatch success-path Ack-gating:
		// a failed source-Ack must not drop the attempt count.
		return ackErr == nil
	}
	if err := sink.Send(ctx, d.Msg); err != nil {
		// Sink down: the message was NOT diverted → retry it. Do NOT fire the
		// terminal hook (no terminal event happened) and do NOT surface the send
		// error to a hook; fire OnRetry with the classification cause instead.
		c.safeFire(c.hooks.OnRetry, ctx, d.Msg, cause)
		c.finish(d.Nack(ctx, true, c.policy.delayFor(attempt))) // non-zero backoff (I6)
		return false                                            // not terminally settled → keep the tracker entry
	}
	// Sink accepted → the terminal divert happened.
	c.safeFire(terminalHook, ctx, d.Msg, cause)
	ackErr := d.Ack(ctx)
	c.finish(ackErr)
	// Gate eviction on source-Ack success, mirroring the dispatch success-path
	// Ack-gating: worst case a duplicate-to-sink on redelivery (acceptable
	// at-least-once), never message loss.
	return ackErr == nil
}

// finish records a non-nil settle error via the injected logger. Plan 003
// releases a flow-control credit here.
func (c *consumer[T]) finish(err error) {
	if err != nil {
		c.logger.Error("msgin: settle failed", "err", err)
	}
}

// safeFire nil-safely invokes an observability hook, recovering any panic so a
// faulty user hook cannot unwind through the worker goroutine and crash the
// whole process (fault isolation — CLAUDE.md's mandatory robustness constraint).
// A nil hook is a no-op. A panicking hook is logged at WARN via the injected
// logger (message id only — never the payload) and swallowed, so the settlement
// flow continues to the next message.
func (c *consumer[T]) safeFire(hook func(context.Context, Message[any], error), ctx context.Context, msg Message[any], cause error) {
	if hook == nil {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			c.logger.Warn("msgin: hook panicked", "id", msg.ID(), "panic", r)
		}
	}()
	hook(ctx, msg, cause)
}

func (c *consumer[T]) decode(m Message[any]) (T, error) {
	if c.liveValue {
		v, ok := m.payload.(T)
		if !ok {
			var zero T
			return zero, ErrPayloadType
		}
		return v, nil
	}
	b, ok := m.payload.([]byte)
	if !ok {
		var zero T
		return zero, ErrPayloadType
	}
	v, err := c.codec.Decode(b)
	if err != nil {
		var zero T
		return zero, fmt.Errorf("%w: %v", ErrPayloadDecode, err)
	}
	return v, nil
}

// safeHandle recovers a panicking handler so a fault in application code never
// takes down the flow (fault isolation, CLAUDE.md's mandatory robustness
// constraint). The recovered value is wrapped in ErrHandlerPanic, which the
// settlement switch classifies as TRANSIENT — a panicking handler is retried
// (Nacked), not diverted to the invalid sink.
func (c *consumer[T]) safeHandle(ctx context.Context, msg Message[T]) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("%w: %v", ErrHandlerPanic, r)
		}
	}()
	return c.handler(ctx, msg)
}
