package endpoint

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"time"

	"github.com/jonboulle/clockwork"
	"github.com/kartaladev/msgin"
)

// Handler consumes a typed message. nil = success (Ack); non-nil = failure.
type Handler[T any] func(ctx context.Context, msg msgin.Message[T]) error

// Consumer runs a flow until its context is cancelled.
type Consumer[T any] interface {
	Run(ctx context.Context) error
}

// ConsumerOption configures NewConsumer.
type ConsumerOption[T any] func(*consumerConfig[T])

type consumerConfig[T any] struct {
	concurrency     int
	codec           msgin.PayloadCodec[T]
	codecSet        bool
	policy          msgin.RetryPolicy
	invalidSink     msgin.OutboundAdapter
	logger          *slog.Logger
	hooks           msgin.Hooks
	shutdownTimeout time.Duration
	clock           clockwork.Clock
	maxInFlight     int
	maxInFlightSet  bool // C2: distinguishes explicit WithMaxInFlight(0) from unset
	rateLimiter     msgin.RateLimiter
	handlerTimeout  time.Duration
	breaker         msgin.CircuitBreaker
	overflow        msgin.OverflowPolicy
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
func WithConsumerCodec[T any](c msgin.PayloadCodec[T]) ConsumerOption[T] {
	return func(o *consumerConfig[T]) { o.codec = c; o.codecSet = true }
}

// WithRetryPolicy sets the settlement policy (default: retry forever, immediate).
func WithRetryPolicy[T any](p msgin.RetryPolicy) ConsumerOption[T] {
	return func(o *consumerConfig[T]) { o.policy = p }
}

// WithInvalidMessageSink sets where permanent/undecodable messages are diverted.
//
// EVERY arm of the invalid path is SINGLE-SHOT (ADR 0007 D7 as amended by
// decisions D-N and D-P): one attempt at one sink, then a terminal Ack. An
// invalid message is never Nacked back into the flow, whichever sink it was
// headed for and whether or not one is configured.
//
// WHEN IT IS SET, and the sink is healthy: the message is sent there,
// OnInvalidMessage fires, the delivery is Acked.
//
// WHEN IT IS SET AND THE SINK IS DOWN — the case an operator most needs to
// know: that Send is NOT retried. The message is logged at WARN (naming the
// message id, the classification cause and the sink error), OnInvalidMessage
// fires, and the delivery is Acked — i.e. the message is DISCARDED for the
// duration of the outage. The trade-off is deliberate: this path never consults
// the attempt tracker, so a requeue here is invisible to MaxAttempts, records
// HEALTHY on the circuit breaker, hot-spins when Backoff is nil (the default),
// and holds its WithMaxInFlight credit indefinitely — a down invalid sink would
// starve VALID traffic while a permanent message that cannot succeed on
// redelivery cycles forever. Terminating is the lesser loss, and it is loud. If
// that window matters, point this option at a durable-on-write sink (a local
// spool, an outbox table) rather than a remote service.
//
// WHEN IT IS UNSET the divert still happens, in four arms:
//
//  1. RetryPolicy.DeadLetter is configured → the message goes THERE. A finite
//     MaxAttempts requires a DeadLetter sink, so this is the default shape of
//     every finite-retry consumer that has not opted in.
//  2. That sink's Send fails → same single-shot discard as above (WARN naming
//     both the classification cause and the sink error, then Ack).
//  3. Neither sink is configured → logged at WARN and discarded (the original
//     ADR 0007 D7 behavior). The WARN names BOTH options, since reaching this
//     arm means neither is set.
//  4. EXCEPT for an over-size payload (ErrPayloadTooLarge from
//     WithMaxPayloadBytes), which does NOT take arm 1: it is discarded (WARN +
//     OnInvalidMessage + Ack) even when a DeadLetter sink is configured. The
//     byte cap defends memory and durable storage against untrusted wire input,
//     so persisting its rejects into the operator's dead-letter store would make
//     the defence the vector. Setting THIS option opts back in — the sink-set
//     arm above covers over-size messages like any other.
//
// OnInvalidMessage fires on every arm; OnRetry and OnDeadLetter fire on none.
// The DEAD-LETTER path is the opposite and unchanged (ADR 0007 D8): a
// retry-exhausted message IS transient, so a failed dead-letter Send is Nacked
// with backoff, never discarded.
//
// THE ONE EXCEPTION TO "NEVER NACKED" IS SHUTDOWN. If the Send fails only
// because the consumer's settle context was already cancelled by the shutdown
// deadline, nothing was learned about the sink, so the message is Nacked for
// redelivery instead of Acked away, and OnInvalidMessage does NOT fire. This is
// bounded by the shutdown deadline and cannot loop; in normal operation the
// single shot above is exactly as described.
//
// THE CLASSIFICATION CAUSE IS LOGGED VERBATIM — keep payload and PII out of it.
// The WARN on the sink-down arm renders the cause in full for every class
// except one, so a handler returning
// msgin.Permanent(fmt.Errorf("invalid email %q", m.Payload().Email)) puts that
// email in the log. The SOLE redacted class is the payload-decode one
// (ErrPayloadDecode, where msgin would otherwise leak a caller-supplied codec's
// quoting of untrusted wire bytes); it renders as its bare sentinel text.
// Nothing else is scrubbed: msgin extracts no payload into that string, but it
// does not inspect the error you compose either. If a fault must carry
// sensitive detail, put the detail behind OnInvalidMessage — which always
// receives the UNREDACTED cause — and keep the error's own message generic.
//
// CONFIGURE IT when your dead-letter store is operationally meaningful: msgin
// stamps no settlement-reason header, so once an invalid message lands in the
// same store as a retries-exhausted one, nothing downstream can tell them
// apart — and they need opposite handling (replaying a permanently-invalid
// message is pointless). A separate invalid-message sink is what preserves the
// distinction.
func WithInvalidMessageSink[T any](out msgin.OutboundAdapter) ConsumerOption[T] {
	return func(o *consumerConfig[T]) { o.invalidSink = out }
}

// WithLogger injects the structured logger the consumer uses to report a
// discarded invalid message, a panicking hook or governor, and a failed settle.
// Default: a discard logger, so a library consumer who wants no output gets
// none. A nil logger is ignored (keeps the default) rather than a
// caller-triggered nil-panic at the first settlement that logs — which would be
// raised on a WORKER goroutine, where safeHandle does not recover it, and would
// therefore take the process down (no panic on caller input). Mirrors
// WithProducerLogger, from which it is named distinctly (ADR 0007 D10).
func WithLogger[T any](l *slog.Logger) ConsumerOption[T] {
	return func(o *consumerConfig[T]) {
		if l != nil {
			o.logger = l
		}
	}
}

// WithHooks sets the observability callbacks (all nil-safe).
func WithHooks[T any](h msgin.Hooks) ConsumerOption[T] {
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
	streamSrc       msgin.EventDrivenSource
	pollSrc         msgin.PollingSource
	handler         Handler[T]
	codec           msgin.PayloadCodec[T]
	liveValue       bool
	workers         int
	policy          msgin.RetryPolicy
	invalidSink     msgin.OutboundAdapter
	logger          *slog.Logger
	hooks           msgin.Hooks
	shutdownTimeout time.Duration
	clock           clockwork.Clock
	native          msgin.NativeReliability
	tracker         *attemptTracker
	maxInFlight     int
	rateLimiter     msgin.RateLimiter
	handlerTimeout  time.Duration
	breaker         msgin.CircuitBreaker
	probeGate       msgin.ProbeGate // non-nil when breaker also implements ProbeGate (ADR 0009 D2)
	overflow        msgin.OverflowPolicy
	maxPayloadBytes int
	pollInterval    time.Duration // pull path: idle wait after an empty poll (ADR 0010 D1)
	pollMaxBatch    int           // pull path: max rows/credits fetched per poll
	// panicLogged deduplicates the ERROR log for a panicking resilience governor
	// per method, so a deterministic panic under fail-open cannot flood the log
	// (ADR 0009 D1). Keyed by method name.
	panicLogged sync.Map
	// invalidFallbackLogged deduplicates D-N's fallback WARN, which reports a
	// configuration fact constant for this consumer's lifetime (ADR 0007 D7 as
	// amended). Same rationale as panicLogged; a Once rather than a keyed map
	// because there is exactly one such event.
	invalidFallbackLogged sync.Once
}

// defaultShutdownTimeout bounds the drain when WithShutdownTimeout is unset or
// non-positive. The drain is ALWAYS finite (C1): an unbounded drain violates the
// "graceful shutdown within a deadline" constraint and can deadlock on an
// adapter whose Nack blocks on a channel the stopped Stream no longer reads.
const defaultShutdownTimeout = 30 * time.Second

// NewConsumer validates the source, the handler and the options, and builds a
// Consumer. A nil src is [msgin.ErrNilAdapter]; a nil h is a bare
// [msgin.ErrNilFunc] naming its position ("endpoint.NewConsumer: nil handler")
// — checked in that order, both at construction, before opts is ever applied.
// A nil ELEMENT of opts is also a bare [msgin.ErrNilFunc], naming the
// element's 0-based index ("endpoint.NewConsumer: nil option at index 1"),
// checked as opts is applied — so it runs AFTER the src/h checks above and
// loses to either of them.
//
// Rejecting a nil handler HERE is load-bearing, not tidiness. Left unguarded,
// the nil is not seen until the first message, where calling it nil-derefs on a
// worker goroutine; safeHandle recovers that panic into ErrHandlerPanic, which
// [msgin.IsPermanent] classifies TRANSIENT, so the message is Nacked and
// redelivered forever — a pure wiring mistake becomes an unbounded hot retry
// loop (measured at ~46k retries on a single message in 200ms) instead of an
// error at the call the caller can see. Same reasoning as the Permanent wrap on
// the Step-returning constructors (D-M, ADR 0029 §5.0b), reached by the other
// arm of the invariant: this one is a constructor, so the sentinel stays bare.
func NewConsumer[T any](src any, h Handler[T], opts ...ConsumerOption[T]) (Consumer[T], error) {
	if src == nil {
		return nil, msgin.ErrNilAdapter
	}
	if h == nil {
		return nil, nilFuncAt("endpoint.NewConsumer: nil handler")
	}
	cfg := consumerConfig[T]{
		concurrency: 1,
		clock:       clockwork.NewRealClock(),
		logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	for i, opt := range opts {
		if opt == nil {
			return nil, nilOptionAt("endpoint.NewConsumer", i)
		}
		opt(&cfg)
	}
	if cfg.concurrency < 1 {
		return nil, msgin.ErrInvalidConcurrency
	}
	if err := cfg.policy.Validate(); err != nil {
		return nil, err
	}
	// C2: unset → default; explicitly set → must be >= 1 (so WithMaxInFlight(0)
	// is a rejected caller error, not silently defaulted).
	if !cfg.maxInFlightSet {
		cfg.maxInFlight = defaultMaxInFlight
	} else if cfg.maxInFlight < 1 {
		return nil, msgin.ErrInvalidMaxInFlight
	}
	// ADR 0009 D3: unset → default; explicitly set → must be > 0 (so
	// WithAttemptTTL(0) is a rejected caller error, not silently defaulted).
	if !cfg.attemptTTLSet {
		cfg.attemptTTL = defaultAttemptTTL
	} else if cfg.attemptTTL <= 0 {
		return nil, msgin.ErrInvalidAttemptTTL
	}
	// C2: unset → default; explicitly set → must be > 0 (so WithPollInterval(0)
	// is a rejected caller error, not silently defaulted). Poll fields are
	// validated for both source kinds (a EventDrivenSource simply ignores them; the
	// pull path consumes them in pollLoop — ADR 0010 D1).
	if !cfg.pollIntervalSet {
		cfg.pollInterval = defaultPollInterval
	} else if cfg.pollInterval <= 0 {
		return nil, msgin.ErrInvalidPollInterval
	}
	// C2: unset → default; explicitly set → must be >= 1 (so WithPollMaxBatch(0)
	// is a rejected caller error, not silently defaulted).
	if !cfg.pollMaxBatchSet {
		cfg.pollMaxBatch = defaultPollMaxBatch
	} else if cfg.pollMaxBatch < 1 {
		return nil, msgin.ErrInvalidPollMaxBatch
	}
	codec, live, err := resolveCodec[T](src, cfg.codec, cfg.codecSet)
	if err != nil {
		return nil, err
	}

	// Resolve the source kind once (ADR 0010 D1). EventDrivenSource wins the
	// precedence when a value implements both (the lower-latency event-driven
	// path); a value implementing only PollingSource takes the pull path; anything
	// else is unsupported. Exactly one of streamSrc/pollSrc is non-nil.
	var (
		streamSrc msgin.EventDrivenSource
		pollSrc   msgin.PollingSource
	)
	if s, ok := src.(msgin.EventDrivenSource); ok {
		streamSrc = s
	} else if p, ok := src.(msgin.PollingSource); ok {
		pollSrc = p
	} else {
		return nil, msgin.ErrUnsupportedSource
	}
	// NativeReliability is resolved from the underlying value regardless of kind.
	native, ok := src.(msgin.NativeReliability)
	if !ok {
		native = noNativeReliability{}
	}
	// ADR 0009 D2: resolve the optional single-probe capability once. When the
	// breaker also implements ProbeGate the dispatch gate uses TryProbe (bounded
	// half-open probes); otherwise it falls back to Allow.
	probeGate, _ := cfg.breaker.(msgin.ProbeGate)
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
		rawCh := make(chan msgin.Delivery) // Stream writes here
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
func (c *consumer[T]) ingest(ctx, settleCtx context.Context, gate *creditGate, in <-chan msgin.Delivery, out chan<- managedDelivery) {
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
func (c *consumer[T]) admit(ctx, settleCtx context.Context, gate *creditGate, d msgin.Delivery, out chan<- managedDelivery) bool {
	if c.rateLimiter != nil {
		if err := c.safeLimiterWait(ctx, d.Msg.ID()); err != nil {
			// ctx done while parked on the limiter; no credit acquired yet, so
			// nothing to release. (A panicking limiter fails open — safeLimiterWait
			// returns nil — so it never reaches this branch, ADR 0009 D1.)
			c.finish(c.safeNack(settleCtx, d, true, 0))
			return false
		}
	}
	if !c.admitBreaker(ctx, d.Msg.ID()) {
		// ctx done while parked on an open breaker; no credit acquired yet, so
		// the held delivery is Nacked (requeue) rather than lost.
		c.finish(c.safeNack(settleCtx, d, true, 0))
		return false
	}
	// Credit acquisition, per the overflow policy (composition order: after
	// rate-limit + breaker-admit). The shed policies are enumerated EXPLICITLY so
	// that default (OverflowBlock AND any out-of-range value) backpressures rather
	// than silently drops — matching the documented unknown→Block contract (I1).
	switch c.overflow {
	case msgin.OverflowDropNewest, msgin.OverflowDropOldest, msgin.OverflowReject:
		// Shed on contention (all three map to DropNewest on a streaming source —
		// ADR 0008 D9). requeue=false: a genuine drop, never a re-enqueue (I2).
		if !gate.tryAcquire() {
			c.safeFire(c.hooks.OnRetry, settleCtx, d.Msg, msgin.ErrOverflowDropped)
			c.finish(c.safeNack(settleCtx, d, false, 0)) // genuine drop; no credit held to release
			return true                                  // keep ingesting
		}
	default: // OverflowBlock and any out-of-range OverflowPolicy → backpressure
		if err := gate.acquire(ctx); err != nil {
			// ctx done before a credit was free; no credit held, so nothing to
			// release. The Nack may block until cancelSettle (I4).
			c.finish(c.safeNack(settleCtx, d, true, 0))
			return false
		}
	}
	md := manage(d, sync.OnceFunc(gate.release))
	select {
	case out <- md:
		return true
	case <-ctx.Done():
		c.finish(c.safeNack(settleCtx, md.Delivery, true, 0)) // wrapped: releases the credit + re-enqueues
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
		c.finish(c.safeNack(settleCtx, md.Delivery, true, 0))
		return
	}
	if c.breaker != nil && !c.dispatchAllowed(md.Msg.ID()) {
		// Open (or half-open with the single probe already taken): do not drive
		// this buffered message into the failing downstream — Nack it (the wrapped
		// Nack releases its credit) for later redelivery, so the surplus waits in
		// the source (NF-10). A false TryProbe consumes no probe, so no Record.
		c.finish(c.safeNack(settleCtx, md.Delivery, true, 0))
		return
	}
	err := c.dispatch(drainCtx, settleCtx, md.Delivery)
	if c.breaker != nil {
		// Health signal (M4): only a genuinely transient handler/downstream
		// failure is unhealthy. err == nil (Ack) or an IsPermanent classification
		// (a poison/undecodable message — the message's fault, not the
		// downstream's) is healthy, so a burst of poison cannot trip the breaker.
		// This Record is the release paired with the dispatchAllowed acquire above.
		c.safeRecord(md.Msg.ID(), err == nil || msgin.IsPermanent(err))
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
// the fired AfterFunc instead, which IsPermanent classifies as transient
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
//  3. IsPermanent(err) → invalid via divert (Permanent(err), or a handler that
//     returned ErrPayloadDecode/ErrPayloadType).
//  4. finite-exhausted (MaxAttempts > 0 && n >= MaxAttempts && !native DLQ) →
//     DeadLetter divert (+ OnDeadLetter), after a lazy n := c.attempts(d).
//  5. default (transient) → Nack redeliver (+ OnRetry), delayed by
//     retryDelay(c.policy, n).
//
// It returns the classification error of the settled outcome — nil on Ack, the
// decode error on the decode-failed path, the handler error on the permanent,
// dead-letter, and Nack-redeliver paths — SOLELY so process can derive the
// breaker health signal (M4). The settlement itself already happened inside this
// call exactly as before; the return is additive and changes no settling
// behavior. Because the decode and permanent errors are IsPermanent-true, only
// the two genuinely transient tails (dead-letter, Nack-redeliver) feed
// Record(false) — a poison message never trips the breaker.
func (c *consumer[T]) dispatch(ctx, settleCtx context.Context, d msgin.Delivery) error {
	id := d.Msg.ID()

	payload, derr := c.decode(d.Msg)
	if derr != nil {
		// Decode failure = permanent invalid message → the invalid-message sink,
		// or the dead-letter sink when none is configured (D-N). Settled
		// TERMINALLY: one attempt at the sink, never a Nack (D-P).
		sink, fellBack := c.invalidTarget(derr)
		if fellBack {
			c.warnInvalidFallback(id)
		}
		if c.divertTerminal(settleCtx, sink, d, c.hooks.OnInvalidMessage, derr) {
			c.tracker.evict(id)
		}
		return derr // permanent (IsPermanent) → healthy signal (M4)
	}

	msg := msgin.NewMessage[T](payload, d.Msg.Headers())
	hctx, cancelHandler := c.handlerContext(ctx)
	err := c.safeHandle(hctx, msg)
	cancelHandler()

	if err == nil {
		// Evict/fire only if the Ack itself succeeded (mirrors divert's gated
		// eviction below): an Ack that fails must NOT drop the attempt count,
		// so a source that redelivers an unacked message keeps climbing toward
		// MaxAttempts instead of silently restarting from 1.
		ackErr := c.safeAck(settleCtx, d)
		c.finish(ackErr)
		if ackErr == nil {
			c.safeFire(c.hooks.OnAck, settleCtx, d.Msg, nil)
			c.tracker.evict(id)
		}
		return nil // healthy signal (M4)
	}
	if msgin.IsPermanent(err) {
		// Permanent (Permanent(err), or a handler that returned
		// ErrPayloadDecode/ErrPayloadType) → the invalid-message sink, or the
		// dead-letter sink when none is configured (D-N). Settled TERMINALLY:
		// one attempt at the sink, never a Nack (D-P).
		// Note (M8): the attempt tracker is deliberately NOT consulted here.
		sink, fellBack := c.invalidTarget(err)
		if fellBack {
			c.warnInvalidFallback(id)
		}
		if c.divertTerminal(settleCtx, sink, d, c.hooks.OnInvalidMessage, err) {
			c.tracker.evict(id)
		}
		return err // permanent (IsPermanent) → healthy signal (M4)
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
		c.finish(c.safeNack(settleCtx, d, true, retryDelay(c.policy, n)))
	}
	return err // transient → unhealthy signal (M4): the only path that trips the breaker
}

// attempts returns the 1-based delivery-attempt count for d: the native
// msgin.delivery-count header if present, else the runtime tracker (incremented
// once per delivery). Called once per failed dispatch (NF-2), and only on the
// transient path (M8) so the success/permanent paths never touch the tracker.
func (c *consumer[T]) attempts(d msgin.Delivery) int {
	if dc, ok := d.Msg.Headers().Int(msgin.HeaderDeliveryCount); ok {
		return dc
	}
	return c.tracker.observe(d.Msg.ID())
}

// invalidTarget returns where an invalid message with classification cause is
// diverted: the configured invalid-message sink, or — when none is configured
// (D-N) — the dead-letter sink, so a fault previously captured durably is never
// downgraded to a discard by D-M's reclassification. fellBack reports the second
// case, so the call site can emit D-N's WARN with the message id. Both are
// nil/false when neither sink is configured — and when the byte-cap exemption
// below applies — where ADR 0007 D7's logged discard remains the terminal
// behavior.
//
// It is a pure config read and is deliberately NOT folded into divertTerminal:
// the dead-letter call site's sink IS c.policy.DeadLetter, so a fallback inside
// the settlement helper would make that site fall back to itself.
//
// THE MISSING !c.native.NativeDeadLetter() GUARD IS DELIBERATE — do not "restore"
// it. The transient dead-letter arm in dispatch does apply that guard, and the
// discriminator is Nack-vs-Ack, not which sink is being written to:
//
//   - The TRANSIENT path NACKS on exhaustion, so a source with a native
//     dead-letter pipeline captures the message itself. Writing to
//     c.policy.DeadLetter as well would DOUBLE-WRITE it, which is why that arm
//     is guarded.
//   - The INVALID path ACKS — divertTerminal always settles with safeAck, on
//     every arm — so a native-DLQ source never sees a Nack for this message and
//     never dead-letters it. The fallback write here is the message's ONLY
//     capture; there is nothing to double-write, and adding the guard would
//     silently return a nil sink and restore ADR 0007 D7's discard for exactly
//     the callers whose broker looked most capable of retaining it.
//
// TestDivertInvalidFallbackUnderNativeDeadLetter pins this: a native-DLQ source
// with no invalid sink must still reach c.policy.DeadLetter.
func (c *consumer[T]) invalidTarget(cause error) (sink msgin.OutboundAdapter, fellBack bool) {
	if c.invalidSink != nil {
		return c.invalidSink, false
	}
	// THE BYTE-CAP EXEMPTION TO D-N. WithMaxPayloadBytes exists to bound memory
	// and storage against UNTRUSTED wire input. Routing its rejects to the
	// dead-letter sink would write each oversize payload VERBATIM into the
	// operator's durable store — so an attacker posting oversize bodies to a
	// capped consumer fills that store with exactly the bytes the cap declared
	// illegitimate, and the defence becomes the vector. The fallback is
	// therefore not applied to this one class; the message is discarded (loudly)
	// instead.
	//
	// THIS DOES NOT VIOLATE D-N'S PREMISE. D-N exists so that "no configuration
	// that previously captured a message starts dropping it" — the D-M
	// reclassification had moved faults OFF the retry-exhaustion path that used
	// to deposit them in the DeadLetter sink. ErrPayloadTooLarge was never on
	// that path: it was permanent (IsPermanent, reliability.go) before D-N too,
	// so it was already DISCARDED under ADR 0007 D7. Exempting it RESTORES the
	// pre-D-N behavior for this class rather than losing anything D-N promised.
	//
	// An operator who does want the bytes captured opts in with
	// WithInvalidMessageSink, handled above: that sink is a deliberate choice
	// aimed at invalid messages, whereas the dead-letter fallback is inherited
	// by every finite-retry consumer that never asked for it.
	if errors.Is(cause, msgin.ErrPayloadTooLarge) {
		return nil, false
	}
	return c.policy.DeadLetter, c.policy.DeadLetter != nil
}

// warnInvalidFallback announces D-N's fallback — no invalid-message sink is
// configured, so the invalid message was sent to the dead-letter sink instead —
// so a caller never discovers by inspection that their DLQ started receiving
// invalid messages.
//
// It fires ONCE per consumer. "No invalid-message sink is configured" is
// constant for the consumer's lifetime, so an undeduplicated WARN would emit
// one identical line per invalid message and a poison storm would flood the
// log. This is governorPanic's rationale; sync.Once is used instead of its
// panicLogged sync.Map because there is exactly one event and no key to
// deduplicate by. Suppressing it costs nothing an operator needs: the fact it
// reports is about the CONFIGURATION, identical for every message, and the
// per-message record — where one is warranted — is the terminal hook
// (OnInvalidMessage), which fires unsuppressed on every invalid message.
//
// Note that on the HEALTHY fallback (the sink accepts) that hook is the ONLY
// per-message record: divertTerminal emits no WARN when the send succeeds, and
// OnInvalidMessage is nil by default — so a consumer with a reachable
// dead-letter sink and no hooks wired emits exactly this one line for its whole
// lifetime. That is the intended volume, not an oversight; the messages
// themselves are in the dead-letter sink. divertTerminal's two FAILURE arms
// (sink down, and neither sink configured) do log one line per message, because
// there the log is the only record the message ever existed.
func (c *consumer[T]) warnInvalidFallback(id string) {
	c.invalidFallbackLogged.Do(func() {
		c.logger.Warn("msgin: no invalid-message sink configured; sending the invalid message to the dead-letter sink instead (further occurrences suppressed)",
			"id", id)
	})
}

// divertTerminal sends d.Msg to sink and settles it TERMINALLY, upholding NF-3
// (no message loss). It is the INVALID-message path's settlement: its messages
// are permanent by classification, so every outcome ends in a terminal event
// and an Ack and none of them re-enters the flow. Four outcomes:
//
//   - nil sink → discarding IS the terminal invalid event (ADR 0007 D7): log a
//     loud WARN naming the id and the class, fire terminalHook, Ack. Two ways to
//     get here, and the WARN distinguishes them: neither an invalid-message sink
//     nor a DeadLetter is configured, or invalidTarget's BYTE-CAP EXEMPTION
//     withheld the DeadLetter fallback from an ErrPayloadTooLarge reject.
//   - sink.Send FAILS WHILE ctx IS ALREADY DONE → this is SHUTDOWN, not a sink
//     refusal, and it is the ONE arm that does not settle terminally. See the
//     dedicated paragraph below.
//   - sink.Send FAILS (ctx still live) → the attempt is SINGLE-SHOT (D-P): log
//     a WARN naming BOTH the classification cause (via causeForLog, which keeps
//     a caller-supplied codec's free text out of the log) and the sink error,
//     fire terminalHook with the UNREDACTED cause JOINED to the sink error, Ack
//     — i.e. fall through to ADR 0007 D7's discard. There is deliberately no
//     Nack and no OnRetry: requeueing a permanent message against a down sink
//     is the unbounded-redelivery trap D7's own reasoning rejects, and no
//     counter, backoff or breaker can observe it (MaxAttempts is bypassed on
//     this path, retryDelay never escalates, and IsPermanent records healthy).
//   - sink accepts → the terminal divert happened: fire terminalHook with
//     cause, Ack.
//
// THE ctx-DONE ARM IS NOT A D-P EXCEPTION. ctx here is Run's settleCtx, which
// drainWorkers cancels when the shutdown deadline expires (C1). A Send that
// returns once that happens has learned NOTHING about the sink — a healthy sink
// whose Send merely observed the cancelled context returns an error
// indistinguishable, at this call site, from a refusal. Taking D-P's discard
// arm there would Ack a message the sink may never have received, and — because
// a Delivery.Ack is free to ignore its context (adapter/memory's does) — that
// Ack can SUCCEED and evict the tracker entry, losing the message with the sink
// perfectly healthy. So this arm Nacks with requeue and returns false (not
// settled, tracker entry kept), leaving redelivery to the source on the next
// run, exactly as every other ctx-done settle arm in this file does (see I4 on
// admit). It does NOT weaken D-P: during normal operation ctx.Err() is nil, so
// the single shot is untouched; the Nack happens only while the consumer is
// already shutting down, which is bounded by C1 and cannot loop. terminalHook
// deliberately does not fire — no terminal event happened.
//
// THE SEND-FAILURE ARM IS THE ONLY ONE WHOSE HOOK ERROR IS NOT THE BARE CAUSE.
// Everything D-P's discard does — Ack, one terminal hook, no OnRetry, no
// OnDeadLetter — is what a HEALTHY divert does, so at the hook the two are
// indistinguishable: same message, same cause. A caller who wired both an
// invalid-message sink and OnInvalidMessage would see a sink outage as a
// perfectly ordinary stream of diverts while every message in the window was
// discarded, the loss recorded only in a WARN string nothing can count. So that
// arm fires errors.Join(cause, sendErr): errors.Is against the CAUSE still
// matches — the join is deliberately non-breaking for anyone classifying on it
// — while errors.Is against the SINK's error is the machine-detectable "we lost
// one". The nil-sink arm keeps the bare cause on purpose: no sink configured is
// a STATIC misconfiguration, knowable at construction, not a transient
// mid-flight loss, and its WARN already names the option to set.
//
// The bool return gates the caller's tracker eviction (D8) and is therefore
// gated on the Ack succeeding in every settling arm, mirroring the dispatch
// success path: a failed source-Ack must not drop the attempt count.
func (c *consumer[T]) divertTerminal(ctx context.Context, sink msgin.OutboundAdapter, d msgin.Delivery, terminalHook func(context.Context, msgin.Message[any], error), cause error) bool {
	hookErr := cause
	if sink == nil {
		// No sink to divert to: discarding is the terminal invalid event (ADR
		// 0007 D7). One line per message — the id is the only record the message
		// ever existed — and it names the CLASS, so an operator can tell the two
		// ways this arm is reached apart without reading the code.
		if errors.Is(cause, msgin.ErrPayloadTooLarge) {
			// The byte-cap exemption (invalidTarget): a DeadLetter sink may well
			// be configured, so the WARN must not claim nothing was.
			c.logger.Warn("msgin: discarding over-size message; the byte cap's rejects are never written to the dead-letter sink (set WithInvalidMessageSink to capture them)",
				"id", d.Msg.ID(), "cause", causeForLog(cause))
		} else {
			// Neither sink configured. Name BOTH — after D-N this arm needs both
			// to be unset, so naming only the invalid one sends an operator who
			// HAS a dead-letter sink to check the wrong option.
			c.logger.Warn("msgin: discarding message; neither an invalid-message sink (WithInvalidMessageSink) nor a dead-letter sink (RetryPolicy.DeadLetter) is configured",
				"id", d.Msg.ID(), "cause", causeForLog(cause))
		}
	} else if err := c.safeSend(ctx, sink, d.Msg); err != nil {
		if ctx.Err() != nil {
			// Shutdown, not a sink refusal (see the paragraph above): keep the
			// message rather than Ack it away on a sink that was never proven
			// down. No terminal hook, no eviction — the source redelivers.
			c.logger.Warn("msgin: invalid-message divert aborted by shutdown; Nacking for redelivery",
				"id", d.Msg.ID(), "cause", causeForLog(cause), "err", err)
			c.finish(c.safeNack(ctx, d, true, 0))
			return false
		}
		// Sink down (including a panicking sink, recovered by safeSend). The
		// message is permanent, so there is no second attempt (D-P) — it is
		// discarded, loudly, naming both why it was invalid and why the sink
		// refused it.
		c.logger.Warn("msgin: discarding invalid message; the sink rejected it and the divert is single-shot",
			"id", d.Msg.ID(), "cause", causeForLog(cause), "err", err)
		// The join is this discard's ONLY machine-detectable record (see above):
		// the WARN is a string, and no other hook fires.
		hookErr = errors.Join(cause, err)
	}
	c.safeFire(terminalHook, ctx, d.Msg, hookErr)
	ackErr := c.safeAck(ctx, d)
	c.finish(ackErr)
	return ackErr == nil
}

// causeForLog RENDERS a settlement's classification cause for the log — it
// returns a string, not an error, precisely because the value is for display
// only and must never be mistaken for something to errors.Is against or to
// settle on.
//
// D-P requires the single-shot discard WARN to name the classification cause,
// and every cause is safe to render verbatim EXCEPT one: safeDecode builds a
// decode failure as fmt.Errorf("%w: %v", msgin.ErrPayloadDecode, err) around a
// CALLER-SUPPLIED PayloadCodec's error, and such an error is free to quote the
// bytes it choked on (encoding/json quotes the offending character; a custom
// codec may quote far more). Rendering it would contradict the contract
// safeDecode and safeSend both state for their OWN logs — "the message id only,
// never the payload" — and would do so on untrusted wire input, the one place
// it matters. So the ErrPayloadDecode class renders as its BARE SENTINEL text:
// the classification survives (that is what D-P asks the WARN to name, and what
// an operator triages on), the codec's free text does not.
//
// EXACTLY ONE CLASS IS REDACTED, AND THAT IS THE WHOLE CONTRACT. Every other
// cause — Permanent(...) from a handler, ErrPayloadType, ErrPayloadTooLarge —
// is rendered IN FULL, verbatim. For msgin's own sentinels that is trivially
// safe (they are fixed strings). For a CALLER-COMPOSED cause it is not a claim
// about content at all: a handler is free to write
// Permanent(fmt.Errorf("invalid email %q", m.Payload().Email)), an ordinary
// validation shape, and that text — payload and all — reaches the WARN. The
// distinction being drawn is authorship, not sensitivity: msgin does not
// EXTRACT payload into this string, but it does not scrub what the caller chose
// to put in their own error either. Redacting every cause is not the fix — it
// would erase the classification detail D-P requires the WARN to name. The
// contract is stated on the caller's side instead, on WithInvalidMessageSink
// and msgin.Permanent: an error you hand to msgin is logged as written, so keep
// payload and PII out of it.
//
// The codec detail is not lost to the caller, only to the log: the UNREDACTED
// cause is still what OnInvalidMessage receives, so an operator who wants it can
// take it under their own disclosure policy.
//
// %v (not .Error()) so a nil cause renders "<nil>" instead of panicking; this
// helper is never called with nil today, and must not become the reason a
// future call site does.
func causeForLog(cause error) string {
	if errors.Is(cause, msgin.ErrPayloadDecode) {
		return msgin.ErrPayloadDecode.Error()
	}
	return fmt.Sprintf("%v", cause)
}

// divert sends d.Msg to the DEAD-LETTER sink and settles it safely, upholding
// NF-3 (no message loss). Its messages are TRANSIENT by classification — they
// exhausted a finite retry budget — so, unlike divertTerminal, a failed send is
// retried rather than discarded. Two outcomes:
//
//   - sink.Send FAILS → the message was NOT diverted, so it is retried, not
//     terminally settled: fire OnRetry (with the CLASSIFICATION cause, never the
//     terminal hook and never the send error — no terminal event happened) and
//     Nack with a NON-ZERO backoff delay retryDelay(c.policy, attempt) (spec §7
//     backoff(sinkAttempts), I6) so a down sink is not hammered. Returns false so
//     the caller keeps the tracker entry (the message is still in flight).
//   - sink accepts → the terminal divert happened: fire terminalHook with cause,
//     Ack. Eviction is gated on the Ack succeeding.
//
// sink is never nil here: it is c.policy.DeadLetter, and RetryPolicy.Validate
// rejects a finite MaxAttempts without one — so the nil-sink discard lives on
// divertTerminal, the only path that can reach it.
//
// The bool return gates the caller's tracker eviction (D8): true = terminally
// settled AND source-Acked, false = still in flight (kept for redelivery).
func (c *consumer[T]) divert(ctx context.Context, sink msgin.OutboundAdapter, d msgin.Delivery, terminalHook func(context.Context, msgin.Message[any], error), cause error, attempt int) bool {
	if err := c.safeSend(ctx, sink, d.Msg); err != nil {
		// Sink down (including a panicking sink, recovered by safeSend): on THIS,
		// the dead-letter path, the message was NOT diverted → retry it. (The
		// invalid path settles the same failure terminally — see divertTerminal.) Do NOT fire the terminal hook (no
		// terminal event happened) and do NOT surface the send error to a hook;
		// fire OnRetry with the classification cause instead.
		c.safeFire(c.hooks.OnRetry, ctx, d.Msg, cause)
		c.finish(c.safeNack(ctx, d, true, retryDelay(c.policy, attempt))) // non-zero backoff (I6)
		return false                                                      // not terminally settled → keep the tracker entry
	}
	// Sink accepted → the terminal divert happened.
	c.safeFire(terminalHook, ctx, d.Msg, cause)
	ackErr := c.safeAck(ctx, d)
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
func (c *consumer[T]) safeFire(hook func(context.Context, msgin.Message[any], error), ctx context.Context, msg msgin.Message[any], cause error) {
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

// decode resolves m's payload to T: the live-value type assertion (memory
// adapter) or, for a wire ([]byte) payload, the size cap (ADR 0009 D5)
// followed by the codec decode — the only branch that calls into
// caller-supplied adapter/codec code, so it is the one routed through
// safeDecode (ADR 0010 D6). The live-value type assertion above it cannot
// panic and is therefore left unguarded.
func (c *consumer[T]) decode(m msgin.Message[any]) (T, error) {
	if c.liveValue {
		v, ok := m.Payload().(T)
		if !ok {
			var zero T
			return zero, msgin.ErrPayloadType
		}
		return v, nil
	}
	b, ok := m.Payload().([]byte)
	if !ok {
		var zero T
		return zero, msgin.ErrPayloadType
	}
	// ADR 0009 D5: cap untrusted wire bytes BEFORE decoding. An over-size payload
	// is permanent (it will not shrink on redelivery) → invalid divert (invalid
	// sink, else DeadLetter, else discard), not retried.
	if c.maxPayloadBytes > 0 && len(b) > c.maxPayloadBytes {
		var zero T
		return zero, msgin.ErrPayloadTooLarge
	}
	return c.safeDecode(m.ID(), b)
}

// safeDecode invokes the payload codec's Decode, recovering a panic so a
// faulty PayloadCodec cannot crash the process (fault isolation, ADR 0010 D6,
// fold-in #4 — the adapter-SPI call sites in the settlement path). A
// recovered panic is mapped to the SAME classification a returned decode
// error already gets — ErrPayloadDecode wrapping the cause — so the
// settlement switch treats a panicking codec exactly like a real decode
// failure: PERMANENT → the invalid divert (invalid sink, else DeadLetter, else
// discard), never retried against a codec that will panic again on redelivery.
// Logs ERROR with the message id only, never the payload.
func (c *consumer[T]) safeDecode(id string, b []byte) (v T, err error) {
	defer func() {
		if r := recover(); r != nil {
			var zero T
			c.logger.Error("msgin: PayloadCodec.Decode panicked", "id", id, "panic", r)
			v, err = zero, fmt.Errorf("%w: %v", msgin.ErrPayloadDecode, r)
		}
	}()
	v, err = c.codec.Decode(b)
	if err != nil {
		var zero T
		return zero, fmt.Errorf("%w: %v", msgin.ErrPayloadDecode, err)
	}
	return v, nil
}

// safeSend invokes sink.Send, recovering a panic so a faulty outbound adapter
// (an invalid-message sink or DeadLetter) cannot crash the process (fault
// isolation, ADR 0010 D6). A recovered panic is synthesized into an error and
// routed EXACTLY as a real sink.Send error is: the message was NOT diverted, so
// divert retries it (OnRetry + Nack with backoff) on the dead-letter path, and
// divertTerminal discards it single-shot on the invalid path (D-P) — never
// treating it as silently lost on either. Logs ERROR with the message id only, never the
// payload.
func (c *consumer[T]) safeSend(ctx context.Context, sink msgin.OutboundAdapter, msg msgin.Message[any]) (err error) {
	defer func() {
		if r := recover(); r != nil {
			c.logger.Error("msgin: OutboundAdapter.Send panicked", "id", msg.ID(), "panic", r)
			err = fmt.Errorf("msgin: OutboundAdapter.Send panicked: %v", r)
		}
	}()
	return sink.Send(ctx, msg)
}

// safeAck invokes d.Ack, recovering a panic so a faulty source adapter's
// settlement call cannot crash the process (fault isolation, ADR 0010 D6).
// d is always a managedDelivery's wrapped Delivery here (dispatch/divert
// operate on the delivery handed to process, whose Ack/Nack already release
// this delivery's flow-control credit BEFORE invoking the original closure —
// the manage wrapper in credit.go), so a panicking Ack has already released
// its credit; the worker's deferred release is an idempotent net, never a
// leak. A recovered panic is synthesized into an error, settled by the
// caller's c.finish exactly like a real Ack error (ERROR-logged there too),
// and the caller's success-gating (tracker eviction, OnAck) is skipped just
// as it is for a real Ack failure. Deliberately NOT deduplicated per method
// (unlike governorPanic): an adapter panic is not the deterministic
// per-message resilience-governor case (ADR 0009 D1), so each occurrence is
// logged. Logs ERROR with the message id only, never the payload.
func (c *consumer[T]) safeAck(ctx context.Context, d msgin.Delivery) (err error) {
	defer func() {
		if r := recover(); r != nil {
			c.logger.Error("msgin: Delivery.Ack panicked", "id", d.Msg.ID(), "panic", r)
			err = fmt.Errorf("msgin: Delivery.Ack panicked: %v", r)
		}
	}()
	return d.Ack(ctx)
}

// safeNack invokes d.Nack, recovering a panic with the same rationale, credit
// ordering, and no-dedup logging as safeAck.
func (c *consumer[T]) safeNack(ctx context.Context, d msgin.Delivery, requeue bool, delay time.Duration) (err error) {
	defer func() {
		if r := recover(); r != nil {
			c.logger.Error("msgin: Delivery.Nack panicked", "id", d.Msg.ID(), "panic", r)
			err = fmt.Errorf("msgin: Delivery.Nack panicked: %v", r)
		}
	}()
	return d.Nack(ctx, requeue, delay)
}

// safeHandle recovers a panicking handler so a fault in application code never
// takes down the flow (fault isolation, CLAUDE.md's mandatory robustness
// constraint). The recovered value is wrapped in ErrHandlerPanic, which the
// settlement switch classifies as TRANSIENT — a panicking handler is retried
// (Nacked), never diverted to the invalid-message sink (nor to the dead-letter
// sink standing in for it).
func (c *consumer[T]) safeHandle(ctx context.Context, msg msgin.Message[T]) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("%w: %v", msgin.ErrHandlerPanic, r)
		}
	}()
	return c.handler(ctx, msg)
}

// retryDelay returns the redelivery delay for the given 1-based attempt count,
// converting to the 0-based retry index the BackoffStrategy expects. A nil
// Backoff means immediate redelivery.
//
// This was RetryPolicy.delayFor, an unexported method on a root type. The
// computation is private convenience over the EXPORTED Backoff field, so it
// travels with its callers rather than becoming public API on RetryPolicy.
func retryDelay(p msgin.RetryPolicy, attempt int) time.Duration {
	if p.Backoff == nil {
		return 0
	}
	return p.Backoff.Delay(attempt - 1)
}
