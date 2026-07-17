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
	attemptTTL      time.Duration
	attemptTTLSet   bool // distinguishes explicit WithAttemptTTL(0) (rejected) from unset (ADR 0009 D3)
	maxPayloadBytes int  // <= 0 disables the wire-decode size cap (ADR 0009 D5)
	pollInterval    time.Duration
	pollIntervalSet bool // distinguishes explicit WithPollInterval(0) (rejected) from unset (C2)
	pollMaxBatch    int
	pollMaxBatchSet bool // distinguishes explicit WithPollMaxBatch(0) (rejected) from unset (C2)
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
	// Exactly one of streamSrc / pollSrc is non-nil (resolved by NewConsumer).
	// streamSrc drives today's push path (Stream + ingest); pollSrc drives the
	// pull path (pollLoop, credit-at-fetch). Run branches on which is set.
	streamSrc       StreamingSource
	pollSrc         PollingSource
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
	probeGate       ProbeGate // non-nil when breaker also implements ProbeGate (ADR 0009 D2)
	overflow        OverflowPolicy
	maxPayloadBytes int
	pollInterval    time.Duration // pull path: idle wait after an empty poll (ADR 0010 D1)
	pollMaxBatch    int           // pull path: max rows/credits fetched per poll
	// panicLogged deduplicates the ERROR log for a panicking resilience governor
	// per method, so a deterministic panic under fail-open cannot flood the log
	// (ADR 0009 D1). Keyed by method name.
	panicLogged sync.Map
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
	// ADR 0009 D3: unset → default; explicitly set → must be > 0 (so
	// WithAttemptTTL(0) is a rejected caller error, not silently defaulted).
	if !cfg.attemptTTLSet {
		cfg.attemptTTL = defaultAttemptTTL
	} else if cfg.attemptTTL <= 0 {
		return nil, ErrInvalidAttemptTTL
	}
	// C2: unset → default; explicitly set → must be > 0 (so WithPollInterval(0)
	// is a rejected caller error, not silently defaulted). Poll fields are
	// validated for both source kinds (a StreamingSource simply ignores them; the
	// pull path consumes them in pollLoop — ADR 0010 D1).
	if !cfg.pollIntervalSet {
		cfg.pollInterval = defaultPollInterval
	} else if cfg.pollInterval <= 0 {
		return nil, ErrInvalidPollInterval
	}
	// C2: unset → default; explicitly set → must be >= 1 (so WithPollMaxBatch(0)
	// is a rejected caller error, not silently defaulted).
	if !cfg.pollMaxBatchSet {
		cfg.pollMaxBatch = defaultPollMaxBatch
	} else if cfg.pollMaxBatch < 1 {
		return nil, ErrInvalidPollMaxBatch
	}
	codec, live, err := resolveCodec[T](src, cfg.codec, cfg.codecSet)
	if err != nil {
		return nil, err
	}

	// Resolve the source kind once (ADR 0010 D1). StreamingSource wins the
	// precedence when a value implements both (the lower-latency event-driven
	// path); a value implementing only PollingSource takes the pull path; anything
	// else is unsupported. Exactly one of streamSrc/pollSrc is non-nil.
	var (
		streamSrc StreamingSource
		pollSrc   PollingSource
	)
	if s, ok := src.(StreamingSource); ok {
		streamSrc = s
	} else if p, ok := src.(PollingSource); ok {
		pollSrc = p
	} else {
		return nil, ErrUnsupportedSource
	}
	// NativeReliability is resolved from the underlying value regardless of kind.
	native, ok := src.(NativeReliability)
	if !ok {
		native = noNativeReliability{}
	}
	// ADR 0009 D2: resolve the optional single-probe capability once. When the
	// breaker also implements ProbeGate the dispatch gate uses TryProbe (bounded
	// half-open probes); otherwise it falls back to Allow.
	probeGate, _ := cfg.breaker.(ProbeGate)
	return &consumer[T]{
		streamSrc: streamSrc, pollSrc: pollSrc,
		handler: h, codec: codec, liveValue: live,
		workers: cfg.concurrency,
		policy:  cfg.policy, invalidSink: cfg.invalidSink, logger: cfg.logger,
		hooks: cfg.hooks, shutdownTimeout: cfg.shutdownTimeout, clock: cfg.clock,
		native: native, tracker: newAttemptTracker(cfg.clock, cfg.attemptTTL),
		maxInFlight: cfg.maxInFlight, rateLimiter: cfg.rateLimiter,
		handlerTimeout: cfg.handlerTimeout, breaker: cfg.breaker, probeGate: probeGate,
		overflow: cfg.overflow, maxPayloadBytes: cfg.maxPayloadBytes,
		pollInterval: cfg.pollInterval, pollMaxBatch: cfg.pollMaxBatch,
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
	// ADR 0009 D2: a breaker without ProbeGate admits the whole half-open state,
	// so under WithConcurrency(N>1) half-open lets every worker probe concurrently
	// (the probe storm). Warn once so a caller who plugged e.g. a sony/gobreaker
	// State()-mirror does not hit this silently — the shipped NewCircuitBreaker
	// implements ProbeGate and is exempt.
	if c.workers > 1 && c.breaker != nil && c.probeGate == nil {
		c.logger.Warn("msgin: circuit breaker does not implement ProbeGate; half-open admits concurrent probes under WithConcurrency>1",
			"workers", c.workers)
	}

	// ingress -> workers. Buffered to the credit-gate capacity (D3-fix, ADR
	// 0008): a delivery only reaches here after acquiring a credit, and the
	// gate never admits more than maxInFlight concurrently, so this buffer can
	// never overflow. Sizing it to maxInFlight (rather than leaving it
	// unbuffered) is load-bearing, not an optimization: an unbuffered workerCh
	// lets a producer's handoff block on worker availability, which — under a
	// backlog of >= 2 messages behind a busy worker whose Nack(requeue=true)
	// synchronously re-enters the source (e.g. adapter/memory's Send) — forms a
	// 3-way cyclic wait with nobody left to drain the source: a genuine
	// deadlock, confirmed by repro and fixed here. Buffering workerCh to the
	// gate's own capacity means the producer's ONLY blocking point is
	// gate.acquire (the intended, documented backpressure), which keeps the
	// source always drainable — exactly what a requeue's synchronous
	// re-injection needs to complete without a cycle. The pull path relies on
	// the same sizing so its ctx-done handoff arm is near-unreachable (ADR 0010 D1).
	workerCh := make(chan managedDelivery, c.maxInFlight)
	gate := newCreditGate(c.maxInFlight)

	// Both contexts are detached from the parent (so settles/handlers survive
	// parent cancel and can finish the drain) but cancellable, so the deadline
	// can release a settle stuck on a non-accepting backend (C1).
	settleCtx, cancelSettle := context.WithCancel(context.WithoutCancel(ctx))
	defer cancelSettle()
	drainCtx, cancelDrain := context.WithCancel(context.WithoutCancel(ctx))
	defer cancelDrain()

	// ingressWG joins the producer stage (ingest or pollLoop) AND the Run-lifetime
	// sweep; both own their exit so a single Wait() at the end joins both.
	var ingressWG sync.WaitGroup
	cancelSweep := c.startSweep(ctx, &ingressWG)
	defer cancelSweep() // belt-and-suspenders net if anything below panics before the explicit late cancel
	wg := c.startWorkers(drainCtx, settleCtx, workerCh)

	// Producer stage — the ONLY part that differs by source kind (ADR 0010 D1).
	// Both producers run on ingressWG, own close(workerCh) on exit, and stop the
	// main goroutine only once the source has stopped emitting, so the shared
	// drain below begins at (and is timed from) shutdown, identically for both.
	var runErr error
	if c.streamSrc != nil {
		rawCh := make(chan Delivery) // Stream writes here
		ingressWG.Add(1)
		go func() {
			defer ingressWG.Done()
			defer close(workerCh) // closing workerCh lets the workers finish their range
			c.ingest(ctx, settleCtx, gate, rawCh, workerCh)
		}()
		runErr = c.streamSrc.Stream(ctx, rawCh) // blocks until ctx is cancelled
		close(rawCh)                            // ingress drains remaining reads, then closes workerCh
	} else {
		ingressWG.Add(1)
		go func() {
			defer ingressWG.Done()
			defer close(workerCh) // pollLoop owns close(workerCh), mirroring ingest
			c.pollLoop(ctx, settleCtx, gate, workerCh)
		}()
		<-ctx.Done() // pollLoop stops on the same signal; block until shutdown begins
		runErr = ctx.Err()
	}

	c.drainWorkers(wg, cancelDrain, cancelSettle)
	// The drain has completed; stop the Run-lifetime sweep last so its ticker
	// stayed registered for the whole drain.
	cancelSweep()
	ingressWG.Wait() // joins the producer (exited) + sweep (exits on sweepCtx) — goleak-clean
	return runErr
}

// startSweep launches the Run-lifetime attempt-tracker sweep on ingressWG and
// returns its cancel. The sweep is detached from the parent (via
// context.WithoutCancel) so a parent cancel does NOT deregister its clock ticker
// mid-drain; the caller cancels it explicitly at Run's end (after the drain).
// Keeping it alive for Run's whole lifetime makes its ticker a stable,
// always-present waiter on the consumer clock — a constant that fake-clock tests
// can count (see the sweep-ticker ordering invariant in ADR 0008 D8).
func (c *consumer[T]) startSweep(ctx context.Context, ingressWG *sync.WaitGroup) context.CancelFunc {
	sweepCtx, cancelSweep := context.WithCancel(context.WithoutCancel(ctx))
	ingressWG.Add(1)
	go func() {
		defer ingressWG.Done()
		c.tracker.sweepLoop(sweepCtx) // registers the sweep ticker on c.clock at Run start
	}()
	return cancelSweep
}

// startWorkers launches the worker pool draining workerCh and returns the
// WaitGroup that joins when every worker has finished its range (workerCh
// closed and drained).
func (c *consumer[T]) startWorkers(drainCtx, settleCtx context.Context, workerCh <-chan managedDelivery) *sync.WaitGroup {
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
	return &wg
}

// drainWorkers waits for the worker pool to finish, bounded by the shutdown
// timeout (C1 — ALWAYS finite; a non-positive timeout uses the default). On
// timeout it cancels both detached contexts so cooperative handlers abort
// (ctx.Err() -> transient -> Nack) and any settle stuck on a non-accepting
// backend is released, then joins the (now unblocked) pool.
func (c *consumer[T]) drainWorkers(wg *sync.WaitGroup, cancelDrain, cancelSettle context.CancelFunc) {
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()

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
		if err := c.safeLimiterWait(ctx, d.Msg.ID()); err != nil {
			// ctx done while parked on the limiter; no credit acquired yet, so
			// nothing to release. (A panicking limiter fails open — safeLimiterWait
			// returns nil — so it never reaches this branch, ADR 0009 D1.)
			c.finish(d.Nack(settleCtx, true, 0))
			return false
		}
	}
	if !c.admitBreaker(ctx, d.Msg.ID()) {
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
func (c *consumer[T]) admitBreaker(ctx context.Context, id string) bool {
	if c.breaker == nil {
		return true
	}
	for {
		if c.safeAllow(id) {
			return true
		}
		wake, ok := c.safeHalfOpen(id) // subscribe BEFORE re-checking
		if !ok {
			// HalfOpen panicked → the breaker is unusable. Fail open (proceed)
			// rather than park on a nil channel forever (ADR 0009 D1). Must NOT
			// fall through to the select below.
			return true
		}
		if c.safeAllow(id) { // re-check closes the missed-wakeup window
			return true
		}
		select {
		case <-wake:
		case <-ctx.Done():
			return false
		}
	}
}

// safeLimiterWait invokes the RateLimiter, recovering a panic to FAIL OPEN
// (proceed, err=nil) — a panicking limiter is a plug-in bug, not backpressure, and
// the credit gate still bounds in-flight (ADR 0009 D1). A RETURNED error (e.g.
// ctx.Err() at shutdown) is propagated unchanged; only a panic maps to nil.
func (c *consumer[T]) safeLimiterWait(ctx context.Context, id string) (err error) {
	defer func() {
		if r := recover(); r != nil {
			c.governorPanic("RateLimiter.Wait", id, r)
			err = nil // fail open: proceed as if unpaced
		}
	}()
	return c.rateLimiter.Wait(ctx)
}

// safeAllow invokes CircuitBreaker.Allow, recovering a panic to FAIL OPEN (treat
// as not-open → admit): a panicking breaker degrades to the no-breaker baseline
// (bounded by WithConcurrency), never a crash or an ingress wedge (ADR 0009 D1).
func (c *consumer[T]) safeAllow(id string) (ok bool) {
	defer func() {
		if r := recover(); r != nil {
			c.governorPanic("CircuitBreaker.Allow", id, r)
			ok = true
		}
	}()
	return c.breaker.Allow()
}

// safeTryProbe invokes ProbeGate.TryProbe, recovering a panic to FAIL OPEN
// (admit). Same rationale as safeAllow (ADR 0009 D1).
func (c *consumer[T]) safeTryProbe(id string) (ok bool) {
	defer func() {
		if r := recover(); r != nil {
			c.governorPanic("ProbeGate.TryProbe", id, r)
			ok = true
		}
	}()
	return c.probeGate.TryProbe()
}

// safeRecord feeds an outcome back to the breaker, recovering (and swallowing) a
// panic — Record returns nothing, so there is no fallback value (ADR 0009 D1).
func (c *consumer[T]) safeRecord(id string, success bool) {
	defer func() {
		if r := recover(); r != nil {
			c.governorPanic("CircuitBreaker.Record", id, r)
		}
	}()
	c.breaker.Record(success)
}

// safeHalfOpen invokes CircuitBreaker.HalfOpen, returning (ch, true) normally and
// (nil, false) on panic. admitBreaker reads ok=false as "breaker unusable → fail
// open, do NOT park" — parking on a nil channel would wedge forever (ADR 0009 D1).
func (c *consumer[T]) safeHalfOpen(id string) (ch <-chan struct{}, ok bool) {
	defer func() {
		if r := recover(); r != nil {
			c.governorPanic("CircuitBreaker.HalfOpen", id, r)
			ch, ok = nil, false
		}
	}()
	return c.breaker.HalfOpen(), true
}

// governorPanic logs a recovered panic from a caller-supplied resilience governor
// (RateLimiter/CircuitBreaker/ProbeGate) at ERROR — a governor panic silently
// removes a safety layer the caller configured, so it is louder than a hook panic
// (safeFire's WARN). It is deduplicated per method via panicLogged so a
// deterministic panic under fail-open cannot flood the log at one line per
// message. Message id only, never the payload (ADR 0009 D1).
func (c *consumer[T]) governorPanic(method, id string, r any) {
	if _, loaded := c.panicLogged.LoadOrStore(method, struct{}{}); loaded {
		return
	}
	c.logger.Error("msgin: resilience governor panicked; failing open (further occurrences for this method suppressed)",
		"method", method, "id", id, "panic", r)
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
	if c.breaker != nil && !c.dispatchAllowed(md.Msg.ID()) {
		// Open (or half-open with the single probe already taken): do not drive
		// this buffered message into the failing downstream — Nack it (the wrapped
		// Nack releases its credit) for later redelivery, so the surplus waits in
		// the source (NF-10). A false TryProbe consumes no probe, so no Record.
		c.finish(md.Nack(settleCtx, true, 0))
		return
	}
	err := c.dispatch(drainCtx, settleCtx, md.Delivery)
	if c.breaker != nil {
		// Health signal (M4): only a genuinely transient handler/downstream
		// failure is unhealthy. err == nil (Ack) or an isPermanent classification
		// (a poison/undecodable message — the message's fault, not the
		// downstream's) is healthy, so a burst of poison cannot trip the breaker.
		// This Record is the release paired with the dispatchAllowed acquire above.
		c.safeRecord(md.Msg.ID(), err == nil || isPermanent(err))
	}
}

// dispatchAllowed is the breaker's DISPATCH gate: TryProbe (bounded half-open
// probes) when the breaker implements ProbeGate, else the plain Allow open-check
// (ADR 0009 D2). Both fail open on a panic (ADR 0009 D1).
func (c *consumer[T]) dispatchAllowed(id string) bool {
	if c.probeGate != nil {
		return c.safeTryProbe(id)
	}
	return c.safeAllow(id)
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
	// ADR 0009 D5: cap untrusted wire bytes BEFORE decoding. An over-size payload
	// is permanent (it will not shrink on redelivery) → invalid sink, not retried.
	if c.maxPayloadBytes > 0 && len(b) > c.maxPayloadBytes {
		var zero T
		return zero, ErrPayloadTooLarge
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
