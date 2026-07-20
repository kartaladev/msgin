package msgin

import (
	"context"
	"time"

	"github.com/jonboulle/clockwork"
)

// aggregatorConfig accumulates AggregatorOption settings before NewAggregator
// builds an Aggregator.
type aggregatorConfig struct {
	output    MessageChannel
	correlate func(Message[any]) (string, error)
	release   func(MessageGroup) bool
	timeout   time.Duration
	expired   MessageChannel
	clock     clockwork.Clock
}

// AggregatorOption configures an Aggregator built by NewAggregator.
type AggregatorOption func(*aggregatorConfig)

// WithOutputChannel sets the (required) channel a released aggregate is sent
// to. NewAggregator returns ErrNilOutput if this is never set.
//
// Never wire this channel back into the same Aggregator's input under the
// same correlation key: Handle's release path is not reentrant for that key
// (it is mid-claim on the store) — a cycle deadlocks/re-enters incorrectly.
//
// Settlement is a store-level lease-claim (MessageGroupStore.ClaimGroup/
// SettleGroup), giving at-least-once release both WITHIN and ACROSS
// processes sharing a durable store. A crash between claim and settle
// recovers via the reaper's recovery sweep, which re-emits the recovered
// group to THIS channel — so go agg.Run(ctx) is REQUIRED (not merely for
// expiry) whenever the store is durable (RecoverInterval() > 0). A member
// that arrives while a group is under lease forms a fresh residual group
// rather than joining the in-flight claim.
func WithOutputChannel(ch MessageChannel) AggregatorOption {
	return func(c *aggregatorConfig) { c.output = ch }
}

// WithCorrelationStrategy overrides how a message's group key is derived. The
// default (defaultCorrelate) reads HeaderCorrelationID; a missing or empty
// key is Permanent(ErrNoCorrelation) — routed to the invalid-message channel
// rather than retried, since a missing correlation id will not appear on
// redelivery.
func WithCorrelationStrategy(fn func(Message[any]) (string, error)) AggregatorOption {
	return func(c *aggregatorConfig) { c.correlate = fn }
}

// WithReleaseStrategy overrides when a group is complete and ready to
// aggregate. The default (defaultRelease) is len(group) >= HeaderSequenceSize,
// read number-tolerantly (int/int64/float64).
//
// The release decision is made against the LIVE snapshot Add returns, but
// ClaimGroup then freezes whatever the store holds at claim time — the same
// set for a monotonic strategy (one that only grows more true as members
// accumulate), but a non-monotonic strategy may end up aggregating a
// slightly different member set than the one it decided on. Prefer a
// monotonic strategy (e.g. >=, never <) for this reason.
func WithReleaseStrategy(fn func(MessageGroup) bool) AggregatorOption {
	return func(c *aggregatorConfig) { c.release = fn }
}

// WithCompletionSize releases a group once it holds n messages — sugar for a
// fixed-size release strategy when there is no HeaderSequenceSize to read.
// Exact-count release is now safe both WITHIN one process and ACROSS
// processes sharing a durable store: the store's atomic ClaimGroup is the
// sole serializer, not a per-key lock. It is not safe against id-less
// duplicate members (no dedup).
func WithCompletionSize(n int) AggregatorOption {
	return func(c *aggregatorConfig) {
		c.release = func(g MessageGroup) bool { return len(g.Messages()) >= n }
	}
}

// WithGroupTimeout enables the age-expiry side of the reaper (Run): a group
// whose CreatedAt is older than d, and still incomplete at a reap tick, is
// routed to the expired-group channel (WithExpiredGroupChannel) and settled
// rather than held forever. Unset (the default), no age-expiry applies; Run
// still starts a recovery sweep if the store is durable (RecoverInterval() >
// 0), or otherwise blocks until its context is cancelled without reaping
// anything.
//
// WithGroupTimeout without a paired WithExpiredGroupChannel makes
// NewAggregator return ErrExpiryChannelRequired: a partial group's members
// must go somewhere observable when they expire, rather than being silently
// dropped.
func WithGroupTimeout(d time.Duration) AggregatorOption {
	return func(c *aggregatorConfig) { c.timeout = d }
}

// WithExpiredGroupChannel sets the channel an age-expired, still-incomplete
// group's members are sent to by the reaper (Run), one message at a time, in
// the group's arrival order. Required whenever WithGroupTimeout is set. A
// group the reaper finds COMPLETE (a crashed or newly-complete release) goes
// to the output channel instead — see WithOutputChannel.
//
// Never wire this channel back into the same Aggregator's input under the
// same correlation key: the reaper's release path is not reentrant for that
// key (it is mid-claim on the store) — a cycle deadlocks/re-enters
// incorrectly (see WithOutputChannel's doc for the identical caveat).
func WithExpiredGroupChannel(ch MessageChannel) AggregatorOption {
	return func(c *aggregatorConfig) { c.expired = ch }
}

// WithAggregatorClock injects the clock the expiry reaper (Run) uses to drive
// its ticker and compute the expiry cutoff. The default is the real wall
// clock; tests inject a clockwork.FakeClock. A nil c leaves the default in
// place. Named distinctly from the package-level WithClock (a MessageOption)
// to avoid a same-package function collision — both configure a
// clockwork.Clock but for different option types.
func WithAggregatorClock(c clockwork.Clock) AggregatorOption {
	return func(cfg *aggregatorConfig) {
		if c != nil {
			cfg.clock = c
		}
	}
}

// defaultCorrelate reads HeaderCorrelationID as the group key. A missing or
// empty header is Permanent(ErrNoCorrelation): the runtime's isPermanent does
// not match a bare ErrNoCorrelation, so without the wrap the message would be
// retried to the dead-letter sink instead of diverted to the invalid-message
// channel.
func defaultCorrelate(m Message[any]) (string, error) {
	if s, ok := m.Headers().String(HeaderCorrelationID); ok && s != "" {
		return s, nil
	}
	return "", Permanent(ErrNoCorrelation)
}

// defaultRelease reports whether g holds at least as many messages as its
// first member's HeaderSequenceSize header (read number-tolerantly). A group
// with no members never releases; a group whose first member carries no
// HeaderSequenceSize never releases via this strategy (use WithCompletionSize
// or WithReleaseStrategy, or rely on expiry).
func defaultRelease(g MessageGroup) bool {
	msgs := g.Messages()
	if len(msgs) == 0 {
		return false
	}
	size, ok := asInt(firstHeader(msgs, HeaderSequenceSize))
	return ok && len(msgs) >= size
}

// firstHeader returns the raw header value for key on msgs' first member.
func firstHeader(msgs []Message[any], key string) any {
	v, _ := msgs[0].Header(key)
	return v
}

// asInt reads a header value as an int, tolerant of the numeric types a
// header may hold: a live in-process int/int64, or a float64 (the shape a
// number round-trips as through a JSON-framed sql store).
func asInt(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case float64:
		return int(n), true
	default:
		return 0, false
	}
}

// Aggregator is an EIP Aggregator endpoint: it correlates incoming messages
// into groups held in a MessageGroupStore, and when a group's release
// strategy is satisfied it claims, aggregates, and forwards the group's
// payloads (fn, supplied to NewAggregator) as one Message[B] sent to the
// output channel. It implements MessageHandler; place it as a chain's next
// (Subscribe) or a flow head (NewConsumer[any](src, agg.Handle)). Optionally
// it runs a recovery+expiry reaper via Run(ctx) — see WithGroupTimeout and
// WithOutputChannel's settlement note.
//
// Concurrency: Handle holds NO per-key lock. Instead, the store's
// MessageGroupStore.ClaimGroup is the sole serializer: only one caller (in
// this process or another, for a durable store) can hold a group's lease at
// a time, so concurrent Handle calls for the SAME key never double-release —
// this is what makes WithCompletionSize's exact-count release safe under
// WithConcurrency > 1 within a process AND across processes sharing a
// durable store. Handle never recurses into itself or blocks on another
// Handle call for the same key (no lock to deadlock on) — but see
// WithOutputChannel's caveat about wiring the output channel back into this
// Aggregator's own input.
//
// Settlement: store.Add persists a member before Handle returns (before the
// source Acks). A release claims the group (ClaimGroup), aggregates and
// forwards it to the output channel, then settles the claim
// (store.SettleGroup) — at-least-once, within a process AND, for a durable
// store, across a crash (see WithOutputChannel). A member redelivered after
// its group already settled forms a fresh group (idempotent downstream
// expected). A persistently-failing aggregate function leaves its claim
// abandoned (retried on the next Handle/reaper tick) until expiry reaps it —
// set WithGroupTimeout if that matters for your flow.
type Aggregator struct {
	store  MessageGroupStore
	assert func(Message[any]) error
	agg    func(ctx context.Context, group []Message[any]) (Message[any], error)
	cfg    aggregatorConfig
}

var _ MessageHandler = (*Aggregator)(nil)

// NewAggregator builds an Aggregator from store, the typed aggregate function
// fn, and opts. store and an output channel (WithOutputChannel) are required;
// a nil store is ErrNilStore, a nil fn is ErrNilFunc, and no WithOutputChannel
// is ErrNilOutput — no panic on caller input. WithGroupTimeout without a
// paired WithExpiredGroupChannel is ErrExpiryChannelRequired.
func NewAggregator[A, B any](
	store MessageGroupStore,
	fn func(ctx context.Context, group []Message[A]) (Message[B], error),
	opts ...AggregatorOption,
) (*Aggregator, error) {
	if store == nil {
		return nil, ErrNilStore
	}
	if fn == nil {
		return nil, ErrNilFunc
	}
	cfg := aggregatorConfig{correlate: defaultCorrelate, release: defaultRelease, clock: clockwork.NewRealClock()}
	for _, opt := range opts {
		opt(&cfg)
	}
	if cfg.output == nil {
		return nil, ErrNilOutput
	}
	if cfg.timeout > 0 && cfg.expired == nil {
		return nil, ErrExpiryChannelRequired
	}
	return &Aggregator{
		store:  store,
		assert: func(m Message[any]) error { _, err := PayloadOf[A](m); return err },
		agg:    boxAggFn(fn),
		cfg:    cfg,
	}, nil
}

// boxAggFn wraps a typed aggregate function fn into a non-generic closure the
// (non-generic) Aggregator struct can store: it re-asserts each group member
// to A (the members were already ingress-asserted by Handle; this produces
// the typed slice fn consumes), calls fn, and boxes the Message[B] result
// back into Message[any]. Mirrors how Transform/Router keep a non-generic
// core behind a typed constructor (ADR 0001/0013).
func boxAggFn[A, B any](fn func(ctx context.Context, group []Message[A]) (Message[B], error)) func(ctx context.Context, group []Message[any]) (Message[any], error) {
	return func(ctx context.Context, group []Message[any]) (Message[any], error) {
		typed := make([]Message[A], len(group))
		for i, m := range group {
			tm, err := PayloadOf[A](m)
			if err != nil {
				return Message[any]{}, err
			}
			typed[i] = tm
		}
		out, err := fn(ctx, typed)
		if err != nil {
			return Message[any]{}, err
		}
		return boxMessage(out), nil
	}
}

// Handle correlates msg, adds it to its group, and — once the group's
// release strategy is satisfied against the live snapshot — atomically
// claims, aggregates, and forwards the group to the output channel. A held
// (not-yet-released) group, or a release-ready group whose claim another
// Handle/process is already releasing, returns nil (the source Acks;
// durability now rests on the store). See the Aggregator doc for the
// concurrency and settlement contract.
func (a *Aggregator) Handle(ctx context.Context, msg Message[any]) error {
	if err := a.assert(msg); err != nil {
		return err // ErrPayloadType: fail fast, never added to the store
	}
	key, err := a.cfg.correlate(msg)
	if err != nil {
		return err // e.g. Permanent(ErrNoCorrelation)
	}
	group, err := a.store.Add(ctx, key, msg)
	if err != nil {
		return err
	}
	if !a.cfg.release(group) {
		return nil // held; source Acks — durability now on the store
	}
	claim, err := a.store.ClaimGroup(ctx, key)
	if err != nil {
		return err
	}
	if claim == nil {
		return nil // another Handle/process is releasing this group; held
	}
	return a.release(ctx, claim)
}

// releaseOnce aggregates a claimed group, forwards it to the output channel,
// and settles the claim. It DEFERS an abandon-unless-settled so that an
// agg/Send error OR a PANIC (recovered by the driving Consumer) always frees
// the lease — else a panic mid-release would wedge the correlation key
// forever on the memory store (no TTL to age it out). at-least-once, never
// loss.
func (a *Aggregator) releaseOnce(ctx context.Context, claim MessageGroupClaim) (err error) {
	settled := false
	defer func() {
		if !settled {
			_ = a.store.AbandonGroup(ctx, claim) // runs on error return AND on panic unwind
		}
	}()
	out, aggErr := a.agg(ctx, claim.Messages())
	if aggErr != nil {
		return aggErr
	}
	if sendErr := a.cfg.output.Send(ctx, out); sendErr != nil {
		return sendErr
	}
	if err = a.store.SettleGroup(ctx, claim); err != nil {
		return err
	}
	settled = true
	return nil
}

// release settles claim, THEN drains any already-complete residual left at
// the key. A residual forms when members arrive during a holder's lease;
// after the settle it may itself be complete, but nothing else re-checks it
// (the recovery sweep surfaces only crashed *leases*, not unleased-complete
// residuals), so under count-based release + a recurring key + no expiry
// timeout it would be silently stranded. The drain loop claims and releases
// the key until the residual is incomplete or gone — store-agnostic,
// immediate, and it terminates (each iteration either emits a complete batch
// or abandons an incomplete residual).
//
// The drain closes the common case immediately, but WithGroupTimeout is
// still the guarantee-bearer under concurrency: a residual can be left
// complete-but-untriggered by a crash mid-drain, or a concurrent Add landing
// during the drain's speculative claim-then-abandon of an incomplete
// residual. Neither loses data (at-least-once holds); both are recovered by
// the reaper's age-scan release-check→output. Count-based recurring-key
// aggregation under Competing Consumers should set WithGroupTimeout; the
// canonical Splitter→Aggregator (exactly-N-once) forms no residual and needs
// none. Fairness note: under a pathological same-key feeder the drain can
// keep one worker/tick busy emitting — inherent (each iteration is a real,
// required emit), acceptable, with no artificial bound (a bound would
// re-open the strand).
func (a *Aggregator) release(ctx context.Context, claim MessageGroupClaim) error {
	if err := a.releaseOnce(ctx, claim); err != nil {
		return err
	}
	for {
		next, err := a.store.ClaimGroup(ctx, claim.Key())
		if err != nil || next == nil {
			return err // nothing more to drain (empty / leased by another / gone)
		}
		if !a.cfg.release(next) {
			_ = a.store.AbandonGroup(ctx, next) // residual not yet complete; leave it live
			return nil
		}
		if err := a.releaseOnce(ctx, next); err != nil {
			return err // failed re-release already abandoned its claim; retry later
		}
	}
}

// Run drives the reaper: a RECOVERY + expiry sweep. It starts a ticker when
// EITHER WithGroupTimeout>0 OR the store's RecoverInterval()>0, ticking at
// the min positive of the two; otherwise (neither set) it blocks until ctx
// is cancelled without reaping anything, starting no ticker goroutine.
//
// On each tick, every group from store.Expired(cutoff) is re-examined: a
// COMPLETE group (crashed mid-release, or newly complete) is re-aggregated
// and sent to the OUTPUT channel (recovery — via the same release path
// Handle uses); an age-expired INCOMPLETE group (cutoff is non-zero and the
// group predates it) is routed to WithExpiredGroupChannel; otherwise the
// claim is abandoned (a fresh residual re-formed since the Expired() scan,
// or not yet due). A durable store (RecoverInterval() = its lease TTL) gets
// crash-recovery sweeps even with no expiry timeout set — so go agg.Run(ctx)
// is REQUIRED for multi-process/crash safety whenever the store is durable,
// not only when WithGroupTimeout is set. A transient Expired error is
// skipped and retried on the next tick. Run returns ctx.Err() when ctx is
// cancelled; callers that want reaping typically start Run in its own
// goroutine alongside the flow that calls Handle.
func (a *Aggregator) Run(ctx context.Context) error {
	interval := a.reapInterval()
	if interval <= 0 {
		<-ctx.Done()
		return ctx.Err()
	}

	ticker := a.cfg.clock.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.Chan():
			a.reap(ctx)
		}
	}
}

// reapInterval is the reaper's tick cadence: the min positive of
// WithGroupTimeout and the store's RecoverInterval(), or 0 (no sweep) if
// neither is set.
func (a *Aggregator) reapInterval() time.Duration {
	interval := a.cfg.timeout
	if storeInterval := a.store.RecoverInterval(); storeInterval > 0 && (interval <= 0 || storeInterval < interval) {
		interval = storeInterval
	}
	return interval
}

// reap runs one recovery+expiry sweep: see Run's doc for the full contract.
func (a *Aggregator) reap(ctx context.Context) {
	var cutoff time.Time
	if a.cfg.timeout > 0 {
		cutoff = a.cfg.clock.Now().Add(-a.cfg.timeout)
	}
	groups, err := a.store.Expired(ctx, cutoff)
	if err != nil {
		return // transient; next tick
	}
	for _, g := range groups {
		a.reapGroup(ctx, g, cutoff)
	}
}

// reapGroup runs the sweep's per-group body: see Run's doc for the full
// recovery/expiry/abandon contract.
func (a *Aggregator) reapGroup(ctx context.Context, g MessageGroup, cutoff time.Time) {
	claim, err := a.store.ClaimGroup(ctx, g.Key())
	if err != nil || claim == nil {
		return // released/leased concurrently, or gone
	}
	if a.cfg.release(claim) {
		_ = a.release(ctx, claim) // RECOVERY: re-emit a crashed/complete group to OUTPUT (+ settle)
		return
	}
	if !cutoff.IsZero() && claim.CreatedAt().Before(cutoff) {
		// genuinely expired incomplete group → route members to the expired sink.
		for _, m := range claim.Messages() {
			if sendErr := a.cfg.expired.Send(ctx, m); sendErr != nil {
				_ = a.store.AbandonGroup(ctx, claim) // retry next tick rather than drop (audit R2 L-I)
				return
			}
		}
		_ = a.store.SettleGroup(ctx, claim)
		return
	}
	_ = a.store.AbandonGroup(ctx, claim) // fresh residual re-formed since the Expired() scan, or not yet due
}
