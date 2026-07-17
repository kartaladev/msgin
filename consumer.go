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
	maxInFlight     int
	maxInFlightSet  bool // C2: distinguishes explicit WithMaxInFlight(0) from unset
	rateLimiter     RateLimiter
	handlerTimeout  time.Duration
	breaker         CircuitBreaker
	overflow        OverflowPolicy
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
// (ADR 0007 D10). A nil clock is a no-op (leaves the real-clock default in
// place) rather than a caller-triggered nil-panic once Run starts (no panic on
// caller input).
func WithConsumerClock[T any](c clockwork.Clock) ConsumerOption[T] {
	return func(o *consumerConfig[T]) {
		if c != nil {
			o.clock = c
		}
	}
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
	maxInFlight     int
	rateLimiter     RateLimiter
	handlerTimeout  time.Duration
	breaker         CircuitBreaker
	overflow        OverflowPolicy
}

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
	// C2: unset → default; explicitly set → must be >= 1 (so WithMaxInFlight(0)
	// is a rejected caller error, not silently defaulted).
	if !cfg.maxInFlightSet {
		cfg.maxInFlight = defaultMaxInFlight
	} else if cfg.maxInFlight < 1 {
		return nil, ErrInvalidMaxInFlight
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
		native: native, tracker: newAttemptTracker(cfg.clock, defaultAttemptTTL),
		maxInFlight: cfg.maxInFlight, rateLimiter: cfg.rateLimiter,
		handlerTimeout: cfg.handlerTimeout, breaker: cfg.breaker, overflow: cfg.overflow,
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
	rawCh := make(chan Delivery) // Stream writes here
	// ingress -> workers. Buffered to the credit-gate capacity (D3-fix, ADR
	// 0008): a delivery only reaches here after acquiring a credit, and the
	// gate never admits more than maxInFlight concurrently, so this buffer can
	// never overflow. Sizing it to maxInFlight (rather than leaving it
	// unbuffered) is load-bearing, not an optimization: an unbuffered workerCh
	// lets ingest's admit→handoff block on worker availability, which — under a
	// backlog of >= 2 messages behind a busy worker whose Nack(requeue=true)
	// synchronously re-enters the source (e.g. adapter/memory's Send) — forms a
	// 3-way cyclic wait (Stream blocked on rawCh, ingest blocked on workerCh,
	// the worker blocked on the source's own inbound channel) with nobody left
	// to drain the source: a genuine deadlock, confirmed by repro and fixed
	// here. Buffering workerCh to the gate's own capacity means ingest's ONLY
	// blocking point is gate.acquire (the intended, documented backpressure),
	// so ingest always keeps draining rawCh (up to maxInFlight in flight),
	// which keeps the source always drainable, which is exactly what a
	// requeue's synchronous re-injection needs to complete without a cycle.
	workerCh := make(chan managedDelivery, c.maxInFlight)
	gate := newCreditGate(c.maxInFlight)

	// Both contexts are detached from the parent (so settles/handlers survive
	// parent cancel and can finish the drain) but cancellable, so the deadline
	// can release a settle stuck on a non-accepting backend (C1).
	settleCtx, cancelSettle := context.WithCancel(context.WithoutCancel(ctx))
	defer cancelSettle()
	drainCtx, cancelDrain := context.WithCancel(context.WithoutCancel(ctx))
	defer cancelDrain()

	// Ingress stage: read deliveries, acquire a credit (the flood defense), wrap
	// release-first, and forward to the workers. It stops on rawCh close (the
	// source stream ended) or parent-ctx cancel (shutdown: stop admitting).
	var ingressWG sync.WaitGroup
	ingressWG.Add(1)
	go func() {
		defer ingressWG.Done()
		defer close(workerCh) // closing workerCh lets the workers finish their range
		c.ingest(ctx, settleCtx, gate, rawCh, workerCh)
	}()

	// Run-lifetime attempt-tracker sweep (C1). Detached from the parent so a
	// parent cancel does NOT deregister its clock ticker mid-drain; cancelled
	// explicitly at Run's end (cancelSweep, just before ingressWG.Wait). The
	// ingest goroutine (also on ingressWG) has already exited by then (rawCh
	// closed), so the single ingressWG.Wait() joins both. Keeping the sweep alive
	// for Run's whole lifetime makes its ticker a stable, always-present waiter on
	// the consumer clock — a constant that fake-clock tests can count (see the
	// sweep-ticker ordering invariant in ADR 0008 D8).
	sweepCtx, cancelSweep := context.WithCancel(context.WithoutCancel(ctx))
	defer cancelSweep() // belt-and-suspenders: joins the explicit late cancelSweep() below on the
	// normal path (a second cancel is a harmless no-op); this defer is the net that
	// prevents a leaked sweep goroutine if Stream (or anything above) panics before
	// the explicit late cancel is reached.
	ingressWG.Add(1)
	go func() {
		defer ingressWG.Done()
		c.tracker.sweepLoop(sweepCtx) // registers the sweep ticker on c.clock at Run start
	}()

	var wg sync.WaitGroup
	wg.Add(c.workers)
	for i := 0; i < c.workers; i++ {
		go func() {
			defer wg.Done()
			for md := range workerCh {
				c.process(drainCtx, settleCtx, md)
			}
		}()
	}

	streamErr := c.src.Stream(ctx, rawCh) // blocks until ctx is cancelled
	close(rawCh)                          // ingress drains remaining reads, then closes workerCh

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
	// The drain has completed (either <-done or the deadline path above); stop the
	// Run-lifetime sweep last so its ticker stayed registered for the whole drain.
	cancelSweep()
	ingressWG.Wait() // joins ingest (exited on rawCh close / ctx done) + sweep (exits on sweepCtx) — goleak-clean
	return streamErr
}

// ingest reads deliveries from in, applies the credit gate under the configured
// OverflowPolicy (Block backpressures; the shed policies drop on contention —
// Task 6, ADR 0008 D9), wraps each admitted delivery release-first, and forwards
// to the workers via out. It stops on in-close (the source stream ended) or ctx
// cancel (shutdown: stop admitting).
func (c *consumer[T]) ingest(ctx, settleCtx context.Context, gate *creditGate, in <-chan Delivery, out chan<- managedDelivery) {
	for {
		select {
		case <-ctx.Done():
			return
		case d, ok := <-in:
			if !ok {
				return
			}
			if !c.admit(ctx, settleCtx, gate, d, out) {
				return
			}
		}
	}
}

// admit paces ingress with the rate limiter (if configured), then applies the
// credit gate for d under the configured OverflowPolicy, wraps an admitted
// delivery release-first, and forwards it to a worker. Returns false when ctx
// is cancelled (shutdown) — the held delivery is Nacked (releasing any credit)
// so it is not lost. Composition order (spec §7.4): rate-limit, then the
// breaker ingress park (admitBreaker — NF-10, an open breaker stops admitting
// new work), then the credit gate.
//
// The credit gate itself branches on c.overflow (Task 6, ADR 0008 D9):
// OverflowBlock (default, and any out-of-range value — I1) blocks on acquire
// until a credit frees (backpressure); OverflowDropNewest/DropOldest/Reject use
// the non-blocking tryAcquire and, on contention, SHED the just-arrived
// delivery — Nack(requeue=false) so it is a genuine drop rather than a
// re-enqueue-and-respin (I2), firing OnRetry(ErrOverflowDropped) for
// observability — and keep ingesting (the loop is never broken by a shed).
//
// I4 — bounded, not leak: the ctx-done Nack(requeue=true) on the settleCtx can
// BLOCK until cancelSettle. For the memory adapter a requeue is a Send back onto
// the broker channel; once Run's Stream has stopped reading, that Send blocks
// until the shutdown deadline cancels settleCtx (Run's C1 bound), then returns.
// It never leaks — Run always reaches the deadline and cancels — but it can wait
// up to the full shutdownTimeout, which is the documented shutdown contract.
func (c *consumer[T]) admit(ctx, settleCtx context.Context, gate *creditGate, d Delivery, out chan<- managedDelivery) bool {
	if c.rateLimiter != nil {
		if err := c.rateLimiter.Wait(ctx); err != nil {
			// ctx done while parked on the limiter; no credit acquired yet, so
			// nothing to release.
			c.finish(d.Nack(settleCtx, true, 0))
			return false
		}
	}
	if !c.admitBreaker(ctx) {
		// ctx done while parked on an open breaker; no credit acquired yet, so
		// the held delivery is Nacked (requeue) rather than lost.
		c.finish(d.Nack(settleCtx, true, 0))
		return false
	}
	// Credit acquisition, per the overflow policy (composition order: after
	// rate-limit + breaker-admit). The shed policies are enumerated EXPLICITLY so
	// that default (OverflowBlock AND any out-of-range value) backpressures rather
	// than silently drops — matching the documented unknown→Block contract (I1).
	switch c.overflow {
	case OverflowDropNewest, OverflowDropOldest, OverflowReject:
		// Shed on contention (all three map to DropNewest on a streaming source —
		// ADR 0008 D9). requeue=false: a genuine drop, never a re-enqueue (I2).
		if !gate.tryAcquire() {
			c.safeFire(c.hooks.OnRetry, settleCtx, d.Msg, ErrOverflowDropped)
			c.finish(d.Nack(settleCtx, false, 0)) // genuine drop; no credit held to release
			return true                           // keep ingesting
		}
	default: // OverflowBlock and any out-of-range OverflowPolicy → backpressure
		if err := gate.acquire(ctx); err != nil {
			// ctx done before a credit was free; no credit held, so nothing to
			// release. The Nack may block until cancelSettle (I4).
			c.finish(d.Nack(settleCtx, true, 0))
			return false
		}
	}
	md := manage(d, sync.OnceFunc(gate.release))
	select {
	case out <- md:
		return true
	case <-ctx.Done():
		c.finish(md.Nack(settleCtx, true, 0)) // wrapped: releases the credit + re-enqueues
		return false
	}
}

// admitBreaker parks the ingress goroutine while the breaker is open (NF-10:
// stop admitting new work into a failing downstream), returning true once the
// breaker allows a probe (closed/half-open) or false when ctx is cancelled
// (shutdown). A nil breaker is a no-op (always true).
//
// The park is missed-wakeup-free (spec §7.4.5, ADR 0008 D7): after Allow reports
// open it SUBSCRIBES to the transition channel via HalfOpen and re-checks Allow
// BEFORE parking, so an open→half-open transition landing between the first
// Allow and the park is caught by the re-check (state already half-open) or by
// the already-closed wake channel — never lost. The wake channel is captured
// before the re-check, so it is exactly the one toHalfOpen closes.
func (c *consumer[T]) admitBreaker(ctx context.Context) bool {
	if c.breaker == nil {
		return true
	}
	for {
		if c.breaker.Allow() {
			return true
		}
		wake := c.breaker.HalfOpen() // subscribe BEFORE re-checking
		if c.breaker.Allow() {       // re-check closes the missed-wakeup window
			return true
		}
		select {
		case <-wake:
		case <-ctx.Done():
			return false
		}
	}
}

// process runs one delivery on a worker: the drain short-circuit (Nack without
// dispatch once the shutdown deadline expired) or the full settlement switch. The
// deferred release is a panic-safe net; the wrapped Ack/Nack already release
// exactly once (OnceFunc), so this is idempotent.
func (c *consumer[T]) process(drainCtx, settleCtx context.Context, md managedDelivery) {
	defer md.release()
	if drainCtx.Err() != nil {
		// Deadline already expired: don't start new work, Nack it. settleCtx is
		// cancelled too, so this returns promptly.
		c.finish(md.Nack(settleCtx, true, 0))
		return
	}
	if c.breaker != nil && !c.breaker.Allow() {
		// Open: do not drive an already-buffered message into the failing
		// downstream — Nack it (the wrapped Nack releases its credit) for later
		// redelivery, so the surplus waits durably in the source (NF-10).
		c.finish(md.Nack(settleCtx, true, 0))
		return
	}
	err := c.dispatch(drainCtx, settleCtx, md.Delivery)
	if c.breaker != nil {
		// Health signal (M4): only a genuinely transient handler/downstream
		// failure is unhealthy. err == nil (Ack) or an isPermanent classification
		// (a poison/undecodable message — the message's fault, not the
		// downstream's) is healthy, so a burst of poison cannot trip the breaker.
		c.breaker.Record(err == nil || isPermanent(err))
	}
}

// handlerContext derives the per-handler context from parent (drainCtx):
// handlerTimeout <= 0 (unset/disabled) returns parent unchanged with a
// no-op cancel — zero overhead, no timer goroutine (ADR 0008 D6). Otherwise
// it derives a cancellable child and arms a clock.AfterFunc(handlerTimeout,
// cancel) so the deadline is clockwork-driven (deterministic under a fake
// clock, spec §7.4.4). The returned cancel always Stop()s the timer before
// cancelling, so a handler that completes before the deadline leaves no
// pending timer running (no leak); a handler that overruns is cancelled by
// the fired AfterFunc instead, which isPermanent classifies as transient
// (context.Canceled) → Nack/retry, freeing the worker instead of pinning it.
func (c *consumer[T]) handlerContext(parent context.Context) (context.Context, context.CancelFunc) {
	if c.handlerTimeout <= 0 {
		return parent, func() {}
	}
	hctx, cancel := context.WithCancel(parent)
	timer := c.clock.AfterFunc(c.handlerTimeout, cancel)
	return hctx, func() {
		timer.Stop()
		cancel()
	}
}

// dispatch runs the guarded settlement switch (spec §7) for one delivery. It
// takes two contexts: ctx drives the handler (drainCtx — cancelled at
// shutdown so a cooperative handler aborts), while settleCtx drives the
// settle closures (Ack/Nack/sink) and stays alive (but cancellable) during
// drain (ADR 0007 D9) — Run passes these as two genuinely distinct contexts.
// The handler itself runs under a further-derived handlerContext(ctx) (ADR
// 0008 D6, Plan 003 Task 4) so WithHandlerTimeout bounds only the handler
// call, never the settle closures below it.
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
//
// It returns the classification error of the settled outcome — nil on Ack, the
// decode error on the decode-failed path, the handler error on the permanent,
// dead-letter, and Nack-redeliver paths — SOLELY so process can derive the
// breaker health signal (M4). The settlement itself already happened inside this
// call exactly as before; the return is additive and changes no settling
// behavior. Because the decode and permanent errors are isPermanent-true, only
// the two genuinely transient tails (dead-letter, Nack-redeliver) feed
// Record(false) — a poison message never trips the breaker.
func (c *consumer[T]) dispatch(ctx, settleCtx context.Context, d Delivery) error {
	id := d.Msg.ID()

	payload, derr := c.decode(d.Msg)
	if derr != nil {
		// Decode failure = permanent invalid message. Sink-attempt 1.
		if c.divert(settleCtx, c.invalidSink, d, c.hooks.OnInvalidMessage, derr, 1) {
			c.tracker.evict(id)
		}
		return derr // permanent (isPermanent) → healthy signal (M4)
	}

	msg := Message[T]{payload: payload, headers: d.Msg.headers}
	hctx, cancelHandler := c.handlerContext(ctx)
	err := c.safeHandle(hctx, msg)
	cancelHandler()

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
		return nil // healthy signal (M4)
	}
	if isPermanent(err) {
		// Permanent (Permanent(err), or a handler that returned
		// ErrPayloadDecode/ErrPayloadType) → invalid sink. Sink-attempt 1.
		// Note (M8): the attempt tracker is deliberately NOT consulted here.
		if c.divert(settleCtx, c.invalidSink, d, c.hooks.OnInvalidMessage, err, 1) {
			c.tracker.evict(id)
		}
		return err // permanent (isPermanent) → healthy signal (M4)
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
	return err // transient → unhealthy signal (M4): the only path that trips the breaker
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

// finish records a non-nil settle error via the injected logger. The
// flow-control credit is released by the release-first wrapper on the settle
// closure itself (see manage), not here.
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
