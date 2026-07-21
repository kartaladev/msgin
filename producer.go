package msgin

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/jonboulle/clockwork"
)

// Producer sends typed messages into a flow.
type Producer[T any] interface {
	Send(ctx context.Context, msg Message[T]) error
	// SendAfter delivers msg so it becomes consumable only after delay elapses,
	// when the underlying adapter supports scheduled delivery (ScheduledSender);
	// otherwise it returns ErrScheduledSendUnsupported. A delay <= 0 delivers
	// immediately (identical to Send) — a negative delay is normalized to 0. The
	// delay is relative and skew-free: the adapter's store computes visibility as
	// now+delay.
	SendAfter(ctx context.Context, msg Message[T], delay time.Duration) error
	// SendAt delivers msg so it becomes consumable no earlier than the wall-clock
	// time t, when the adapter supports scheduled delivery; otherwise it returns
	// ErrScheduledSendUnsupported. It is sugar over SendAfter, computing
	// delay = t - now with the producer's clock; a t already in the past delivers
	// immediately. Because the delay is realized on the adapter's (e.g. DB) clock,
	// the absolute target inherits any app-vs-store clock skew — use SendAfter when
	// the delay must be exact.
	SendAt(ctx context.Context, msg Message[T], t time.Time) error
}

// ProducerOption configures NewProducer.
type ProducerOption[T any] func(*producerConfig[T])

type producerConfig[T any] struct {
	codec                PayloadCodec[T]
	codecSet             bool
	clock                clockwork.Clock
	retry                RetryPolicy
	retrySet             bool
	retryAfterCap        time.Duration
	retryAfterCapSet     bool
	retryBudget          time.Duration
	retryBudgetSet       bool
	deadLetterTimeout    time.Duration
	deadLetterTimeoutSet bool
	hooks                Hooks
	logger               *slog.Logger
}

const (
	// defaultRetryAfterCap bounds the server-instructed delay a producer honours
	// from a RetryAfter marker when WithProducerRetryAfterCap is unset. 60s is
	// at the top of the plausible legitimate range — HTTP rate-limit windows are
	// typically <= 60s — and a hostile or misconfigured "Retry-After: 86400" is
	// clamped to it. Without a clamp, a deadline-less ctx (context.Background()
	// is common on a Send) would let a remote endpoint park the caller's
	// goroutine for as long as it likes.
	//
	// The default deliberately does NOT cover the longest legitimate case (a
	// maintenance 503 asking for 120s): a caller who needs that raises the cap
	// explicitly. Defaulting ABOVE the worst legitimate value would optimise for
	// obeying a remote-controlled instruction over bounding the caller, which is
	// the wrong side of the safe-defaults gate.
	defaultRetryAfterCap = 60 * time.Second

	// defaultRetryBudget bounds the CUMULATIVE wall-clock a Send may spend
	// retrying when WithProducerRetryBudget is unset. Two minutes sits above
	// defaultRetryAfterCap — a budget below the cap would silently defeat the
	// Retry-After compliance the cap exists to allow — while keeping "retry
	// forever" finite by default.
	//
	// It is ALSO chosen to sit well below the shortest plausible upstream lease:
	// adapter/database/sql defaults leaseTTL to 5 minutes. A Send blocking
	// longer than the lease that covers the message being handled lets the
	// source reclaim and redeliver it while the first attempt is still running,
	// turning one logical message into duplicate outbound calls that fan out
	// across instances.
	//
	// That budget < lease relation holds for the DEFAULTS as shipped; it is not
	// a guarantee. The budget bounds retry waits only, so a single blocking
	// adapter call can push a Send past any lease regardless (see
	// WithProducerRetryBudget), and a caller may raise the budget or shorten the
	// lease. Where the relation matters, bound the adapter's I/O and size the
	// two against each other explicitly.
	defaultRetryBudget = 2 * time.Minute

	// defaultDeadLetterTimeout bounds the divert. The DeadLetter send runs on a
	// ctx detached from the caller's, so nothing else would stop a hung sink
	// (blackholed TCP, wedged DB) from blocking the caller's goroutine FOREVER —
	// immune to their own cancel or deadline, which would be a strict regression
	// against the un-retried passthrough and a violation of the
	// everything-cancellable and graceful-shutdown constraints.
	defaultDeadLetterTimeout = 30 * time.Second

	// minRetryDelay floors every computed retry wait. A policy whose Backoff
	// yields 0 (an ExponentialBackoff with a non-positive Initial, say) would
	// otherwise re-attempt with no delay at all, hammering the target from the
	// caller's goroutine for as long as the budget allows. 100ms is below any
	// meaningful backoff yet high enough that a degenerate policy makes
	// hundreds, not hundreds of thousands, of attempts before the budget ends
	// it.
	//
	// This floor is deliberately NOT configurable. "Never hard-code a policy the
	// caller cannot change" governs behaviour a caller might legitimately need
	// to tune; a caller wanting a LONGER wait sets Backoff, and there is no
	// legitimate reason to want a SHORTER one than this on a loop that holds the
	// caller's own goroutine.
	minRetryDelay = 100 * time.Millisecond
)

// WithProducerCodec sets the payload codec for a wire adapter (default JSON).
func WithProducerCodec[T any](c PayloadCodec[T]) ProducerOption[T] {
	return func(o *producerConfig[T]) { o.codec = c; o.codecSet = true }
}

// WithProducerClock injects the clock Producer.SendAt uses to convert an absolute
// delivery time into a relative delay. Defaults to a real clock; a nil clock is
// ignored (keeps the default). Named distinctly from the message-level WithClock
// to avoid collision (cf. WithConsumerClock, ADR 0007 D10).
func WithProducerClock[T any](c clockwork.Clock) ProducerOption[T] {
	return func(o *producerConfig[T]) {
		if c != nil {
			o.clock = c
		}
	}
}

// WithProducerRetry applies p to Producer.Send: a failing OutboundAdapter.Send is
// retried on the producer's injected clock until it succeeds, the policy's
// MaxAttempts are spent, the retry budget is exhausted, or ctx is cancelled.
// Default: unset — Send makes exactly ONE attempt and returns the adapter's
// error unchanged, so this option is purely additive.
//
// Classification is the adapter's job and the runtime's decision (ADR 0002):
//
//   - Permanent(err), ErrPayloadType, ErrPayloadDecode, ErrPayloadTooLarge —
//     returned immediately, consuming NO attempt budget and NEVER dead-lettered.
//     A permanent outbound failure is the caller's to see, not something to bury
//     in a DLQ after N pointless attempts. Permanent WINS over RetryAfter.
//   - RetryAfter(err, d) — waits at least d, clamped by
//     WithProducerRetryAfterCap. d is a FLOOR on the computed backoff, never a
//     replacement for it, so a server cannot shorten the client's own backoff.
//   - anything else — waits p.Backoff.Delay(attempt-1), floored to 100ms.
//
// On attempt exhaustion, budget exhaustion, OR cancellation during a backoff
// wait, the message is routed to p.DeadLetter and the returned error carries
// BOTH the causing error and ErrDeadLettered, so
// errors.Is(err, ErrDeadLettered) distinguishes "safely in the DLQ" from "failed
// outright". The divert runs on a ctx detached from the caller's
// (context.WithoutCancel), because the usual reason the loop is ending is that
// ctx was cancelled — diverting on it would mean the message reached neither its
// target nor the DLQ.
//
// TRADE — diverting on cancellation is not free. Because a cancel during backoff
// is terminal for the message, cancelling a Send does NOT return promptly: it
// returns only once the detached divert finishes, so a caller waits up to
// WithProducerDeadLetterTimeout (30s default) per in-flight Send, and a mass
// shutdown dumps EVERY in-flight message into the DLQ rather than dropping it.
// That is the deliberate choice — a message in the DLQ is recoverable, a lost one
// is not — but size WithProducerDeadLetterTimeout against your shutdown deadline,
// and expect DLQ volume proportional to in-flight sends at shutdown.
//
// DELIVERY GUARANTEE — retrying CHANGES it. A retry after a timeout or a
// connection reset re-sends a request the peer may already have received and
// committed; the producer cannot tell "never arrived" from "arrived, reply lost".
// So WithProducerRetry converts Producer.Send from the underlying adapter's
// guarantee to AT-LEAST-ONCE, with duplicates visible to the receiver. The
// receiver must be idempotent: carry a stable idempotency key in a header (the
// message ID is one) and deduplicate on it, or make the operation naturally
// idempotent. Without WithProducerRetry, Send makes exactly one attempt and the
// adapter's own guarantee is unchanged.
//
// p is validated here by RetryPolicy.Validate, so a finite MaxAttempts without a
// DeadLetter fails at construction with ErrNoDeadLetter and a negative
// MaxAttempts with ErrInvalidMaxAttempts — never at send time. Additionally, a
// policy unbounded in BOTH dimensions (MaxAttempts == 0 with a nil Backoff — the
// RetryPolicy zero value) is rejected with ErrUnboundedRetry: on the producer
// that is a zero-delay infinite loop on the CALLER'S goroutine, unlike the
// Consumer, where the same policy means broker redelivery.
//
// SCOPE — this governs Producer.Send ONLY. It does NOT apply to:
//
//   - SendAfter/SendAt, whose delivery is the adapter's durable store rather
//     than a live call (v1 decision, Spec 013 §3.1);
//   - an outbound reached as a To(sink) step inside a flow. There the Consumer's
//     own RetryPolicy already retries by full message redelivery, so a second
//     loop here would MULTIPLY attempts (inner x outer) and re-run every prior
//     step's side effects. A flow with no inbound Consumer gets no retry at all;
//     use a Producer if you need one (ADR 0025 §2).
//
// The loop holds the CALLER'S goroutine for the whole retry sequence — inherent
// to a synchronous Send. A caller wanting fire-and-forget composes this with
// their own concurrency.
//
// TOPOLOGY — this retry is PER-PROCESS by construction. Across N horizontally
// scaled instances, each retries independently, so a throttling endpoint receives
// N times the load it asked to shed and Retry-After compliance is per-instance,
// not fleet-wide. Coordinating a fleet-wide budget needs shared state the core
// cannot assume; the seam for it is ADR 0006's rate-limit and circuit-breaker
// interfaces, which a distributed (Redis- or DB-backed) limiter plugs into
// without a core change.
func WithProducerRetry[T any](p RetryPolicy) ProducerOption[T] {
	return func(o *producerConfig[T]) { o.retry = p; o.retrySet = true }
}

// WithProducerRetryAfterCap clamps the delay honoured from a RetryAfter marker:
// the effective wait is max(computedBackoff, min(serverDelay, d)), always
// additionally bounded by ctx and by WithProducerRetryBudget. Default: 60 seconds
// — see defaultRetryAfterCap for why an unclamped server-supplied delay is a
// denial-of-service lever against the caller's own goroutine.
//
// A custom value is safe as long as it stays below what the caller is willing to
// block a Send for, and at or below the retry budget (a cap above the budget is
// legal but the budget wins). It has no effect without WithProducerRetry, and
// none on errors carrying no RetryAfter marker.
//
// d MUST be > 0: NewProducer returns ErrInvalidRetryAfterCap for an explicit
// d <= 0. Leaving the option unset is how a caller asks for the default.
func WithProducerRetryAfterCap[T any](d time.Duration) ProducerOption[T] {
	return func(o *producerConfig[T]) { o.retryAfterCap = d; o.retryAfterCapSet = true }
}

// WithProducerRetryBudget bounds the CUMULATIVE wall-clock, measured on the
// producer's injected clock, that one Send may spend retrying. Once the budget
// is spent the loop stops as if attempts were exhausted: the message is routed
// to the policy's DeadLetter (if any) and the causing error is returned with
// ErrDeadLettered joined.
//
// It exists because MaxAttempts == 0 means "retry forever", bounded otherwise
// only by ctx — and Producer.Send is routinely called with context.Background().
// The budget makes the safety property ADR 0025 claims actually true. Default:
// 2 minutes. A wait that would overrun the remaining budget ends the loop
// rather than being truncated, so the producer never makes an attempt it has no
// budget to back. The budget ALWAYS applies, including to a finite MaxAttempts;
// a stop caused by the budget rather than by spent attempts is marked with
// ErrRetryBudgetExhausted so the two are distinguishable.
//
// WHAT IT DOES NOT BOUND — the budget bounds the cumulative RETRY WAITS and the
// elapsed time BETWEEN attempts. It does NOT bound the duration of any single
// OutboundAdapter.Send: the loop checks the budget before parking, never during
// an adapter call, so one adapter call that blocks forever blocks Send forever.
// Worst-case wall clock for a Send is therefore
//
//	retryBudget + one unbounded adapter call + deadLetterTimeout
//
// Bound the adapter's own I/O yourself — pass a ctx with a deadline, or configure
// the client's timeouts — if you need a hard upper bound on Send.
//
// d MUST be > 0: NewProducer returns ErrInvalidRetryBudget for an explicit
// d <= 0. There is deliberately no "unlimited" value — an unbounded retry on a
// caller's goroutine is not a configuration this library offers.
func WithProducerRetryBudget[T any](d time.Duration) ProducerOption[T] {
	return func(o *producerConfig[T]) { o.retryBudget = d; o.retryBudgetSet = true }
}

// WithProducerDeadLetterTimeout bounds the dead-letter divert. The divert runs
// on a ctx DETACHED from the caller's — otherwise a cancelled caller could not
// be dead-lettered at all, which is the loss this design exists to prevent — so
// this timeout is the ONLY thing standing between a hung DeadLetter sink
// (blackholed TCP, wedged DB) and a caller goroutine blocked forever, immune to
// its own cancel. Default: 30 seconds. d MUST be > 0
// (ErrInvalidDeadLetterTimeout); there is deliberately no "unlimited" value.
//
// It is also what a CANCELLING caller waits for. A cancel during backoff diverts
// (see WithProducerRetry), and the divert is detached, so Send returns only after
// the sink answers or this timeout expires: a cancelled Send can take up to d to
// return, and a mass shutdown pays that per in-flight Send while dumping each
// in-flight message into the DLQ. Size d against your shutdown deadline, not just
// against the sink's latency.
func WithProducerDeadLetterTimeout[T any](d time.Duration) ProducerOption[T] {
	return func(o *producerConfig[T]) { o.deadLetterTimeout = d; o.deadLetterTimeoutSet = true }
}

// WithProducerHooks installs optional, nil-safe observability callbacks on the
// retry loop. Only OnRetry (fired before each wait, with the causing error) and
// OnDeadLetter (fired once, after a divert) are used by a Producer; the other
// Hooks fields belong to the Consumer's settlement path and are ignored here.
//
// Hooks run synchronously on the caller's goroutine, so a slow hook slows the
// send. A panicking hook is recovered and logged — an observability callback
// must not be able to take down the flow (the fault-isolation constraint) — so
// do not use a hook for control flow.
func WithProducerHooks[T any](h Hooks) ProducerOption[T] {
	return func(o *producerConfig[T]) { o.hooks = h }
}

// WithProducerLogger injects the structured logger the producer uses to report
// a panicking hook. Default: a discard logger, so a library consumer who wants
// no output gets none. A nil logger is ignored (keeps the default). Named
// distinctly from the Consumer's WithLogger to avoid collision on the shared
// option vocabulary (cf. WithProducerClock, ADR 0007 D10).
func WithProducerLogger[T any](l *slog.Logger) ProducerOption[T] {
	return func(o *producerConfig[T]) {
		if l != nil {
			o.logger = l
		}
	}
}

type producer[T any] struct {
	out               OutboundAdapter
	codec             PayloadCodec[T]
	liveValue         bool
	clock             clockwork.Clock
	retry             RetryPolicy
	retrySet          bool
	retryAfterCap     time.Duration
	retryBudget       time.Duration
	deadLetterTimeout time.Duration
	hooks             Hooks
	logger            *slog.Logger
}

// NewProducer builds a Producer, validating codec pairing at construction.
func NewProducer[T any](out OutboundAdapter, opts ...ProducerOption[T]) (Producer[T], error) {
	if out == nil {
		return nil, ErrNilAdapter
	}
	cfg := producerConfig[T]{
		clock:  clockwork.NewRealClock(),
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	codec, live, err := resolveCodec[T](out, cfg.codec, cfg.codecSet)
	if err != nil {
		return nil, err
	}
	if cfg.retrySet {
		if err := cfg.retry.Validate(); err != nil {
			return nil, err
		}
		// Reject the structurally unbounded policy: MaxAttempts == 0 (forever)
		// with a nil Backoff (no delay) — the RetryPolicy zero value.
		//
		// This is deliberately a STRUCTURAL check and NOT a probe of the
		// strategy's actual output. Evaluating cfg.retry.delayFor(1) here would
		// invoke CALLER CODE inside a constructor: BackoffStrategy is an
		// exported public interface, so an implementation may panic (never panic
		// on caller input) or block; it is NON-DETERMINISTIC whenever
		// RandomizationFactor > 0, since jitter can truncate the sampled delay
		// to 0 and intermittently reject a valid config; and sampling one index
		// false-rejects the legitimate "first retry immediate, then back off"
		// shape (Delay(0) == 0, Delay(n>0) == 5s).
		//
		// The output-based hazard it was reaching for — ExponentialBackoff{},
		// whose Delay is always 0 — is fully handled at runtime instead, by the
		// minRetryDelay floor and the always-on budget below.
		if cfg.retry.MaxAttempts == 0 && cfg.retry.Backoff == nil {
			return nil, ErrUnboundedRetry
		}
	}
	if !cfg.retryAfterCapSet {
		cfg.retryAfterCap = defaultRetryAfterCap
	} else if cfg.retryAfterCap <= 0 {
		return nil, ErrInvalidRetryAfterCap
	}
	// The budget ALWAYS applies. Defaulting it only when MaxAttempts == 0 would
	// leave MaxAttempts > 0 completely unbudgeted, so
	// RetryPolicy{MaxAttempts: 1_000_000, Backoff: ExponentialBackoff{}} runs
	// 10^6 x the 100ms floor = ~27.7 HOURS on the caller's goroutine — the same
	// flood the attempt-unbounded case is rejected for, respelled with a large
	// MaxAttempts instead of 0. The "it silently truncates an explicit attempt
	// count" objection is answered by making the two stops DISTINGUISHABLE
	// (ErrRetryBudgetExhausted) rather than by removing the bound.
	if !cfg.retryBudgetSet {
		cfg.retryBudget = defaultRetryBudget
	} else if cfg.retryBudget <= 0 {
		return nil, ErrInvalidRetryBudget
	}
	if !cfg.deadLetterTimeoutSet {
		cfg.deadLetterTimeout = defaultDeadLetterTimeout
	} else if cfg.deadLetterTimeout <= 0 {
		return nil, ErrInvalidDeadLetterTimeout
	}
	return &producer[T]{
		out:               out,
		codec:             codec,
		liveValue:         live,
		clock:             cfg.clock,
		retry:             cfg.retry,
		retrySet:          cfg.retrySet,
		retryAfterCap:     cfg.retryAfterCap,
		retryBudget:       cfg.retryBudget,
		deadLetterTimeout: cfg.deadLetterTimeout,
		hooks:             cfg.hooks,
		logger:            cfg.logger,
	}, nil
}

// box lifts msg from Message[T] to Message[any]: live-value adapters keep the
// payload unencoded, wire adapters get it encoded to []byte via the codec.
func (p *producer[T]) box(msg Message[T]) (Message[any], error) {
	if p.liveValue {
		return Message[any]{payload: msg.payload, headers: msg.headers}, nil
	}
	b, err := p.codec.Encode(msg.payload)
	if err != nil {
		return Message[any]{}, fmt.Errorf("msgin: producer encode failed: %w", err)
	}
	return Message[any]{payload: any(b), headers: msg.headers}, nil
}

// Send writes msg to the outbound adapter for immediate delivery, applying the
// WithProducerRetry policy when one is configured.
func (p *producer[T]) Send(ctx context.Context, msg Message[T]) error {
	boxed, err := p.box(msg)
	if err != nil {
		return err
	}
	if !p.retrySet {
		return p.out.Send(ctx, boxed)
	}
	return p.sendRetrying(ctx, boxed)
}

// sendRetrying runs the configured RetryPolicy over a single outbound send.
// attempt is 1-based, matching RetryPolicy.delayFor's contract. deadline bounds
// the cumulative wall-clock spent retrying (WithProducerRetryBudget).
func (p *producer[T]) sendRetrying(ctx context.Context, boxed Message[any]) error {
	// The budget always applies (NewProducer defaults it and rejects a
	// non-positive explicit value), so there is no unbudgeted mode and no flag.
	deadline := p.clock.Now().Add(p.retryBudget)
	for attempt := 1; ; attempt++ {
		err := p.out.Send(ctx, boxed)
		if err == nil {
			return nil
		}
		// Permanent wins over every other classification, including RetryAfter:
		// a delay is meaningless when the answer is "do not retry".
		if isPermanent(err) {
			return err
		}
		if p.retry.MaxAttempts > 0 && attempt >= p.retry.MaxAttempts {
			return p.deadLetter(ctx, boxed, err)
		}
		wait := p.nextDelay(attempt, err)
		if p.clock.Now().Add(wait).After(deadline) {
			// The next wait would overrun the budget. Stop now rather than make
			// an attempt the budget cannot back. ErrRetryBudgetExhausted marks
			// this stop so the caller can tell it from genuine attempt
			// exhaustion — the budget is a safety bound, and hitting it usually
			// means the policy wanted more time than the caller allowed, which
			// is a different operational signal from "we tried N times".
			return p.deadLetter(ctx, boxed, fmt.Errorf("%w: %w", ErrRetryBudgetExhausted, err))
		}
		p.fireHook(p.hooks.OnRetry, ctx, boxed, err)
		if waitErr := p.wait(ctx, wait); waitErr != nil {
			// The caller went away mid-backoff. This is the COMMON cancellation
			// path, and it is terminal for this message: diverting here is what
			// stops it being lost. The divert is detached and timed, so it still
			// completes after the caller's ctx is dead.
			return errors.Join(p.deadLetter(ctx, boxed, err), waitErr)
		}
	}
}

// nextDelay picks the wait before the next attempt. A RetryAfter marker supplies
// a server-instructed MINIMUM (RFC 9110 §10.2.3), clamped to the configured cap,
// so a server may only lengthen the computed backoff — never shorten it, which
// would hand a remote endpoint a hot-spin lever. Every result is floored to
// minRetryDelay so a zero-or-negative computed backoff cannot spin.
func (p *producer[T]) nextDelay(attempt int, err error) time.Duration {
	d := p.retry.delayFor(attempt)
	if server, ok := retryAfterOf(err); ok {
		d = max(d, min(server, p.retryAfterCap))
	}
	return max(d, minRetryDelay)
}

// wait blocks for d on the injected clock, aborting on ctx cancellation so a
// cancelled caller is never parked (everything cancellable).
func (p *producer[T]) wait(ctx context.Context, d time.Duration) error {
	timer := p.clock.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.Chan():
		return nil
	}
}

// deadLetter routes a terminal message — attempt-exhausted, budget-exhausted, or
// cancelled during a backoff wait — to the policy's DeadLetter sink and returns
// cause joined with ErrDeadLettered — the caller must learn the send never
// reached its target, so a successful divert does NOT become a nil error, and
// must be able to tell it from an outright failure.
//
// The divert runs on a ctx DETACHED from the caller's (context.WithoutCancel,
// the precedent at exchange.go:347): the usual reason the loop is ending is that
// ctx was cancelled or its deadline passed, and diverting on that same ctx would
// fail too, leaving the message in neither the target nor the DLQ.
//
// With no DeadLetter configured (MaxAttempts == 0, budget-exhausted), there is
// nowhere to divert: the cause is returned WITHOUT ErrDeadLettered, because
// nothing was dead-lettered.
func (p *producer[T]) deadLetter(ctx context.Context, boxed Message[any], cause error) error {
	if p.retry.DeadLetter == nil {
		return cause
	}
	// Detached so the caller's cancellation cannot defeat the divert, but TIMED
	// so a hung sink cannot block the caller's goroutine forever. Detaching
	// without bounding would be strictly worse than the un-retried passthrough.
	dlCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), p.deadLetterTimeout)
	defer cancel()

	// Fire on BOTH arms — "the DLQ is down and the message is lost" is the single
	// most operationally important event this loop can produce — and hand the
	// hook EXACTLY the error Send is about to return, so the hook can tell the
	// two apart the same way the caller does.
	//
	// Do NOT pass errors.Join(cause, dlErr): with dlErr == nil that is a non-nil
	// joinError wrapping only cause, carrying no ErrDeadLettered, so the hook
	// would receive the same shape on success and on failure — unable to
	// distinguish "safely in the DLQ" from "lost", which is the whole reason
	// this hook fires.
	//
	// The hook receives dlCtx, which MAY ALREADY BE EXPIRED: the sink can consume
	// the whole deadLetterTimeout before returning. It is passed for
	// trace/value propagation, not as a work budget — a hook that performs I/O
	// must not rely on it and should derive its own bounded ctx.
	var out error
	if dlErr := p.safeDeadLetter(dlCtx, boxed); dlErr != nil {
		out = errors.Join(cause, dlErr)
	} else {
		out = fmt.Errorf("%w: %w", ErrDeadLettered, cause)
	}
	p.fireHook(p.hooks.OnDeadLetter, dlCtx, boxed, out)
	return out
}

// safeDeadLetter invokes the DeadLetter sink, recovering a panic so a faulty
// sink cannot crash the caller's goroutine — the same fault-isolation boundary
// the consumer applies to its divert sinks. The producer's own out.Send is
// deliberately NOT wrapped: it runs on the caller's goroutine and its panic
// belongs to the caller, propagating with its original value and stack.
func (p *producer[T]) safeDeadLetter(ctx context.Context, msg Message[any]) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("msgin: producer DeadLetter.Send panicked: %v", r)
		}
	}()
	return p.retry.DeadLetter.Send(ctx, msg)
}

// fireHook invokes an optional observability callback, tolerating both a nil
// hook and a panicking one: telemetry must never break the send. It mirrors
// consumer.safeFire, including logging the recovered panic with the message ID
// only — NEVER the payload.
func (p *producer[T]) fireHook(h func(context.Context, Message[any], error), ctx context.Context, msg Message[any], err error) {
	if h == nil {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			p.logger.Warn("msgin: hook panicked", "id", msg.ID(), "panic", r)
		}
	}()
	h(ctx, msg, err)
}

// SendAfter writes msg for delivery after delay, if the sink supports scheduling.
func (p *producer[T]) SendAfter(ctx context.Context, msg Message[T], delay time.Duration) error {
	sched, ok := p.out.(ScheduledSender)
	if !ok {
		return ErrScheduledSendUnsupported
	}
	if delay < 0 {
		delay = 0
	}
	boxed, err := p.box(msg)
	if err != nil {
		return err
	}
	return sched.SendAfter(ctx, boxed, delay)
}

// SendAt writes msg for delivery no earlier than t (sugar over SendAfter).
func (p *producer[T]) SendAt(ctx context.Context, msg Message[T], t time.Time) error {
	return p.SendAfter(ctx, msg, t.Sub(p.clock.Now()))
}

// isLiveValue reports whether an adapter emits/consumes live Go values.
func isLiveValue(a any) bool {
	lv, ok := a.(LiveValueSource)
	return ok && lv.EmitsLiveValue()
}

// resolveCodec applies the codec-pairing rules shared by NewProducer and
// NewConsumer: a live-value adapter must not be given an explicit codec
// (ErrUnexpectedCodec); a wire adapter with no codec defaults to JSON — this
// JSON default is a deliberate, documented convenience (matching the
// WithConsumerCodec/WithProducerCodec "default JSON" contract), NOT an error, so
// there is no ErrNoPayloadCodec (ADR 0009 D4). A future adapter that needs
// require-codec semantics would signal it via a capability (e.g. RequiresCodec)
// plus a returning error path, added with that adapter. It returns the resolved
// codec and whether adapter is a live-value adapter.
func resolveCodec[T any](adapter any, codec PayloadCodec[T], codecSet bool) (PayloadCodec[T], bool, error) {
	live := isLiveValue(adapter)
	if live && codecSet {
		return nil, live, ErrUnexpectedCodec
	}
	if !live && codec == nil {
		codec = JSONPayloadCodec[T]{}
	}
	return codec, live, nil
}
