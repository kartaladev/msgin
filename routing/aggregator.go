package routing

import (
	"context"
	"fmt"
	"time"

	"github.com/jonboulle/clockwork"
	"github.com/kartaladev/msgin"
)

// aggregatorConfig accumulates AggregatorOption settings before NewAggregator
// builds an Aggregator.
type aggregatorConfig struct {
	output            msgin.MessageChannel
	correlate         CorrelationStrategy
	release           ReleaseStrategy
	timeout           time.Duration
	expired           msgin.MessageChannel
	clock             clockwork.Clock
	completionSize    int
	completionSizeSet bool
}

// completionSizeCeiling is the upper bound WithCompletionSize accepts (Spec
// 016 §3.4): group MEMBERS per group, 65,536 — far beyond any plausible
// aggregation. It is sized by TIME, not bytes: memory.GroupStore.Add clones
// the group snapshot per call, so the cost of growing a group is quadratic
// in its member count, not linear. Reaching this ceiling costs a measured
// 48.3 GiB of allocation churn and 8.6s (Plan 029 Task 4); a 400,000-member
// probe did not finish inside a 4m20s timeout. That churn is why the ceiling
// is where it is, and why no test grows a group to it.
const completionSizeCeiling = 1 << 16

// CorrelationStrategy derives a message's group key. It is one of the two named
// behavior types behind the Aggregator pattern (EIP ch. 7); Spring Integration
// calls the equivalent contract
// org.springframework.integration.aggregator.CorrelationStrategy.
//
// A returned error is a correlation failure: the message is routed to the
// invalid-message channel — or to the dead-letter sink when none is configured
// ([msgin.Permanent] states all three arms) — rather than grouped.
type CorrelationStrategy func(m msgin.Message[any]) (string, error)

// ReleaseStrategy decides whether a group is complete and ready to aggregate.
// It is the other named behavior type behind the Aggregator pattern (EIP
// ch. 7); Spring Integration calls the equivalent contract
// org.springframework.integration.aggregator.ReleaseStrategy — which returns a
// BARE BOOLEAN, where this one is deliberately fallible (decision D-E, ADR 0029
// §3), so a strategy that cannot decide says so instead of guessing "not yet".
//
// It is fallible: a strategy that reads member headers or evaluates a
// runtime-defined rule can fail on the group's data, and that failure must be
// distinguishable from a "not yet complete" false. A release-check error
// propagates from Handle so the message is retried or dead-lettered; it never
// silently reads as "not ready". Use [WithReleaseWhen] for the common case
// where the decision cannot fail.
type ReleaseStrategy func(g msgin.MessageGroup) (bool, error)

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
func WithOutputChannel(ch msgin.MessageChannel) AggregatorOption {
	return func(c *aggregatorConfig) { c.output = ch }
}

// WithCorrelationStrategy overrides how a message's group key is derived. The
// default (defaultCorrelate) reads HeaderCorrelationID; a missing or empty
// key is Permanent(ErrNoCorrelation) — routed to the invalid-message channel, or
// to the dead-letter sink when none is configured ([msgin.Permanent] states all
// three arms), rather than retried, since a missing correlation id will not
// appear on redelivery.
//
// A nil fn is REJECTED BY [NewAggregator] (a bare ErrNilFunc naming its
// position), not silently accepted: Handle calls the strategy unconditionally,
// so a nil one would panic on the first message — and inside a Consumer that
// panic is classified ErrHandlerPanic, i.e. TRANSIENT, so a pure
// misconfiguration would retry forever instead of surfacing.
func WithCorrelationStrategy(fn CorrelationStrategy) AggregatorOption {
	return func(c *aggregatorConfig) { c.correlate = fn }
}

// WithReleaseStrategy overrides when a group is complete and ready to
// aggregate. The default (defaultRelease) is len(group) >= HeaderSequenceSize,
// read number-tolerantly (int/int64/float64).
//
// The strategy is fallible ([ReleaseStrategy]); a release-check error
// propagates from Handle rather than reading as "not ready". Use
// [WithReleaseWhen] when the decision cannot fail.
//
// The release decision is made against the LIVE snapshot Add returns, but
// ClaimGroup then freezes whatever the store holds at claim time — the same
// set for a monotonic strategy (one that only grows more true as members
// accumulate), but a non-monotonic strategy may end up aggregating a
// slightly different member set than the one it decided on. Prefer a
// monotonic strategy (e.g. >=, never <) for this reason.
//
// A nil fn is REJECTED BY [NewAggregator] (a bare ErrNilFunc naming its
// position) for the same reason as [WithCorrelationStrategy]: Handle calls the
// strategy unconditionally.
func WithReleaseStrategy(fn ReleaseStrategy) AggregatorOption {
	return func(c *aggregatorConfig) { c.release = fn }
}

// WithReleaseWhen overrides when a group is complete, for a decision that
// cannot fail — sugar over [WithReleaseStrategy] wrapping fn to return a nil
// error. The monotonicity guidance on WithReleaseStrategy applies unchanged.
//
// A nil fn leaves the release strategy UNSET rather than wrapping the nil, so
// [NewAggregator] rejects it exactly as it rejects WithReleaseStrategy(nil).
// Building the wrapper around a nil fn would produce a non-nil c.release that
// slips past the constructor and defers the nil-deref to release time.
func WithReleaseWhen(fn func(msgin.MessageGroup) bool) AggregatorOption {
	return func(c *aggregatorConfig) {
		if fn == nil {
			c.release = nil
			return
		}
		c.release = func(g msgin.MessageGroup) (bool, error) { return fn(g), nil }
	}
}

// WithCompletionSize releases a group once it holds n messages — sugar for a
// fixed-size release strategy when there is no HeaderSequenceSize to read.
// Exact-count release is now safe both WITHIN one process and ACROSS
// processes sharing a durable store: the store's atomic ClaimGroup is the
// sole serializer, not a per-key lock. It is not safe against id-less
// duplicate members (no dedup).
//
// n must be in [1, completionSizeCeiling] (65,536); [NewAggregator] returns
// a construction-time msgin.ErrInvalidCapacity naming this option's site and
// n's value/range otherwise, rather than accepting a value that would let a
// single group grow without limit — see completionSizeCeiling's godoc for
// why the bound is sized by the quadratic cost of growing a group, not by
// bytes. The bound is validated on the value n itself, whether or not a
// later option (WithReleaseStrategy or WithReleaseWhen) goes on to overwrite
// the release strategy this option installs — an oversized n is rejected
// even though it would otherwise have no effect.
func WithCompletionSize(n int) AggregatorOption {
	return func(c *aggregatorConfig) {
		c.completionSize, c.completionSizeSet = n, true
		c.release = func(g msgin.MessageGroup) (bool, error) { return len(g.Messages()) >= n, nil }
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
func WithExpiredGroupChannel(ch msgin.MessageChannel) AggregatorOption {
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
// empty header is Permanent(ErrNoCorrelation): the runtime's IsPermanent does
// not match a bare ErrNoCorrelation, so without the wrap the message would be
// retried to the dead-letter sink instead of diverted to the invalid-message
// channel.
func defaultCorrelate(m msgin.Message[any]) (string, error) {
	if s, ok := m.Headers().String(msgin.HeaderCorrelationID); ok && s != "" {
		return s, nil
	}
	return "", msgin.Permanent(msgin.ErrNoCorrelation)
}

// defaultRelease reports whether g holds at least as many messages as its
// first member's HeaderSequenceSize header (read number-tolerantly). A group
// with no members never releases; a group whose first member carries no
// HeaderSequenceSize never releases via this strategy (use WithCompletionSize
// or WithReleaseStrategy, or rely on expiry).
func defaultRelease(g msgin.MessageGroup) (bool, error) {
	msgs := g.Messages()
	if len(msgs) == 0 {
		return false, nil
	}
	size, ok := asInt(firstHeader(msgs, msgin.HeaderSequenceSize))
	return ok && len(msgs) >= size, nil
}

// firstHeader returns the raw header value for key on msgs' first member.
func firstHeader(msgs []msgin.Message[any], key string) any {
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
	store  msgin.MessageGroupStore
	assert func(msgin.Message[any]) error
	agg    func(ctx context.Context, group []msgin.Message[any]) (msgin.Message[any], error)
	cfg    aggregatorConfig
}

var _ msgin.MessageHandler = (*Aggregator)(nil)

// NewAggregator builds an Aggregator from store, the typed aggregate function
// fn, and opts. store and an output channel (WithOutputChannel) are required;
// a nil store is ErrNilStore and no WithOutputChannel is ErrNilOutput — no
// panic on caller input. WithGroupTimeout without a paired
// WithExpiredGroupChannel is ErrExpiryChannelRequired. [WithCompletionSize]'s
// n, if set, must be in [1, completionSizeCeiling]; out of range is a
// construction-time msgin.ErrInvalidCapacity naming the site and n's
// value/range (see that option's godoc) — checked AFTER the strategy checks
// below and BEFORE the output-channel and expiry-channel checks.
//
// EVERY FUNCTION Handle CALLS UNCONDITIONALLY IS REJECTED HERE, as a BARE
// ErrNilFunc naming its position so the caller knows which one:
//
//   - a nil fn                            → "routing.NewAggregator: nil fn"
//   - [WithCorrelationStrategy](nil)      → "…: nil correlation strategy"
//   - [WithReleaseStrategy](nil) or
//     [WithReleaseWhen](nil)              → "…: nil release strategy"
//
// A nil ELEMENT of opts is a bare [msgin.ErrNilFunc] too, naming the element's
// 0-based index ("routing.NewAggregator: nil option at index 1"), checked as
// opts is applied — so it runs AFTER the store and fn checks above and loses to
// either of them, but BEFORE the strategy, completion-size, output-channel and
// expiry-channel checks below, which run after the loop and so lose to it.
//
// Rejecting at construction is what keeps a misconfiguration VISIBLE: Handle
// calls correlate and release unconditionally, so a nil one would panic on the
// first message, and inside a Consumer that panic is recovered and classified
// ErrHandlerPanic — TRANSIENT — so the flow would retry the same doomed
// configuration forever rather than failing fast here.
//
// These are CONSTRUCTION-time errors and are returned BARE, deliberately: they
// are handed to the caller here and never carried through Handle, so they never
// reach a RetryPolicy and a retry classification would be meaningless on them.
// This is the constructor arm of the invariant on [msgin.ErrNilFunc] — the
// exclusion is a decision, not an omission, so do not wrap them in
// [msgin.Permanent] to match the flow-path producers.
func NewAggregator[A, B any](
	store msgin.MessageGroupStore,
	fn func(ctx context.Context, group []msgin.Message[A]) (msgin.Message[B], error),
	opts ...AggregatorOption,
) (*Aggregator, error) {
	if store == nil {
		return nil, msgin.ErrNilStore
	}
	if fn == nil {
		return nil, nilFuncAt("routing.NewAggregator: nil fn")
	}
	cfg := aggregatorConfig{correlate: defaultCorrelate, release: defaultRelease, clock: clockwork.NewRealClock()}
	for i, opt := range opts {
		if opt == nil {
			return nil, nilOptionAt("routing.NewAggregator", i)
		}
		opt(&cfg)
	}
	// Handle calls both strategies unconditionally; a nil one must not survive
	// construction (see the doc comment).
	if cfg.correlate == nil {
		return nil, nilFuncAt("routing.NewAggregator: nil correlation strategy")
	}
	if cfg.release == nil {
		return nil, nilFuncAt("routing.NewAggregator: nil release strategy")
	}
	if cfg.completionSizeSet {
		if err := checkRange(msgin.ErrInvalidCapacity, "routing.WithCompletionSize",
			cfg.completionSize, 1, completionSizeCeiling); err != nil {
			return nil, err
		}
	}
	if cfg.output == nil {
		return nil, msgin.ErrNilOutput
	}
	if cfg.timeout > 0 && cfg.expired == nil {
		return nil, msgin.ErrExpiryChannelRequired
	}
	return &Aggregator{
		store:  store,
		assert: func(m msgin.Message[any]) error { _, err := msgin.PayloadOf[A](m); return err },
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
func boxAggFn[A, B any](fn func(ctx context.Context, group []msgin.Message[A]) (msgin.Message[B], error)) func(ctx context.Context, group []msgin.Message[any]) (msgin.Message[any], error) {
	return func(ctx context.Context, group []msgin.Message[any]) (msgin.Message[any], error) {
		typed := make([]msgin.Message[A], len(group))
		for i, m := range group {
			tm, err := msgin.PayloadOf[A](m)
			if err != nil {
				return msgin.Message[any]{}, err
			}
			typed[i] = tm
		}
		out, err := fn(ctx, typed)
		if err != nil {
			return msgin.Message[any]{}, err
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
//
// # A store error that ARRIVES WITH a group snapshot
//
// MessageGroupStore.Add MAY return a non-nil group alongside a non-nil error.
// That pair means "this MEMBER was refused, but here is the group as it
// stands" — a bounded store rejecting a member past its cap is the shipped
// case. Handle then re-evaluates the release predicate against that snapshot,
// because the group may be COMPLETE and merely untriggered (its previous
// release failed and was abandoned), and refusing the redelivery that would
// have re-fired it would wedge the key forever. The member is rejected; the
// release is not. A store returning (nil, err) keeps the previous behavior
// exactly — the error is propagated unchanged.
//
// DIRECTION RULE for the six exits of that branch, in two clauses:
//
//  1. Handle NEVER UPGRADES the store's classification. A transient rejection
//     is never turned permanent, on any exit. A store author may rely on this
//     unconditionally.
//  2. Handle either DOWNGRADES on positive evidence of drainage — the group
//     provably shrank, or another holder is provably draining it, so a fresh
//     TRANSIENT ErrOverflowDropped is minted — or REPLACES the overflow error
//     entirely with a distinct fault (a ClaimGroup failure, a release
//     failure) carrying that fault's own classification. Those faults are
//     unmarked, hence transient, so a persistently failing claim/release path
//     RETRIES rather than terminating. That is deliberate: marking a store or
//     channel fault permanent because it was reached through an overflow
//     would dead-letter messages for a cause that has nothing to do with the
//     cap, and would misattribute it in the operator's sink. Configure
//     RetryPolicy.Backoff if the retry rate matters.
//
// WHY `claim == nil` RETURNS AN ERROR HERE while the success path below
// returns nil for the identical condition: on the success path the member is
// already stored, so a nil (an Ack) is safe — durability rests on the store.
// In this branch the member was never stored, so a nil would Ack a message
// that was never aggregated. The divergence is required, not an oversight.
func (a *Aggregator) Handle(ctx context.Context, msg msgin.Message[any]) error {
	if err := a.assert(msg); err != nil {
		return err // ErrPayloadType: fail fast, never added to the store
	}
	key, err := a.cfg.correlate(msg)
	if err != nil {
		return err // e.g. Permanent(ErrNoCorrelation)
	}
	group, err := a.store.Add(ctx, key, msg)
	if err != nil {
		if group == nil {
			return err // unchanged for every store that returns (nil, err)
		}
		// The store rejected the MEMBER and handed back the group's live
		// snapshot anyway, so the release is still evaluable. Reject the
		// member, not the release: without this, a store cap turns a group
		// that is complete-but-untriggered (its last release failed and was
		// abandoned) into a permanent deadlock, because the redelivery that
		// would have re-fired the release is now refused at the door.
		ok, rerr := a.cfg.release(group)
		if rerr != nil || !ok {
			return err // nothing to drain — the store's classification stands
		}
		claim, cerr := a.store.ClaimGroup(ctx, key)
		if cerr != nil {
			return cerr
		}
		if claim == nil {
			return overflowRetryable(key) // another holder is releasing it
		}
		if relErr := a.release(ctx, claim); relErr != nil {
			return relErr
		}
		return overflowRetryable(key) // drained — the retry WILL be admitted
	}
	if group == nil {
		// SPI contract violation: Add returned a nil snapshot AND a nil error.
		// Guarded HERE, at the single choke point, rather than inside a release
		// strategy — a.cfg.release is one of four values (the default,
		// WithCompletionSize, WithReleaseWhen, or a caller's own
		// WithReleaseStrategy), and only this site precedes all four. A guard in
		// any one of them leaves the other three dereferencing nil.
		return fmt.Errorf("%w: routing.Aggregator.Handle: MessageGroupStore.Add returned a nil group with a nil error",
			msgin.Permanent(msgin.ErrNilMessageGroup))
	}
	ok, err := a.cfg.release(group)
	if err != nil {
		return err // release-decision error (the WithReleaseStrategy fn failed) → retry/DLQ;
		// the reaper's expiry fall-through surfaces a stuck group (ADR 0019 A3).
	}
	if !ok {
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

// overflowRetryable mints the FRESH, transient msgin.ErrOverflowDropped that
// Handle returns when a rejected member's group provably drained (or is
// provably being drained by another holder).
//
// It deliberately takes no cause and wraps nothing. The store's rejection is
// msgin.Permanent-wrapped whenever the group was unleased, and wrapping it
// would carry that marker straight through errors.As — turning the downgrade
// into a no-op and dead-lettering a member the retry is now guaranteed to
// admit. A fresh error is the only shape that can be transient.
//
// An error rather than nil is returned because the member was never stored:
// nil would make the source Ack a message that was never aggregated.
func overflowRetryable(key string) error {
	return fmt.Errorf(
		"%w: routing.Aggregator.Handle: group %q drained by this release; retry to admit the rejected member",
		msgin.ErrOverflowDropped, key)
}

// releaseOnce aggregates a claimed group, forwards it to the output channel,
// and settles the claim. It DEFERS an abandon-unless-settled so that an
// agg/Send error OR a PANIC (recovered by the driving Consumer) always frees
// the lease — else a panic mid-release would wedge the correlation key
// forever on the memory store (no TTL to age it out). at-least-once, never
// loss.
func (a *Aggregator) releaseOnce(ctx context.Context, claim msgin.MessageGroupClaim) (err error) {
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
func (a *Aggregator) release(ctx context.Context, claim msgin.MessageGroupClaim) error {
	if err := a.releaseOnce(ctx, claim); err != nil {
		return err // MAIN release failed → NOT settled (defer abandoned the claim) →
		// Nack+redeliver is correct (idempotent Add re-groups, no double-count).
	}
	// Main group settled (incl. the current member). The drain below is a
	// best-effort optimization that claims/emits any already-complete residual
	// formed during the lease; it must NEVER return a non-nil error (H-2/H-3),
	// or it would Nack the already-settled member. Residuals it leaves are
	// recovered by the reaper (durable store) / a later Add.
	for {
		next, err := a.store.ClaimGroup(ctx, claim.Key())
		if err != nil || next == nil {
			return nil // no residual, leased by another, or a transient claim error → leave for reaper
		}
		ok, rerr := a.cfg.release(next)
		if rerr != nil || !ok {
			_ = a.store.AbandonGroup(ctx, next) // residual not evaluable / not complete; leave it live
			return nil
		}
		if err := a.releaseOnce(ctx, next); err != nil {
			return nil // residual re-release failed (its claim already abandoned by the defer) → reaper
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
func (a *Aggregator) reapGroup(ctx context.Context, g msgin.MessageGroup, cutoff time.Time) {
	claim, err := a.store.ClaimGroup(ctx, g.Key())
	if err != nil || claim == nil {
		return // released/leased concurrently, or gone
	}
	ok, rerr := a.cfg.release(claim)
	if rerr == nil && ok {
		_ = a.release(ctx, claim) // RECOVERY: re-emit a crashed/complete group to OUTPUT (+ settle)
		return
	}
	// H-1: a release ERROR or a not-yet-complete group both fall through to
	// age-expiry so an unevaluable group is surfaced to the visible sink,
	// never stranded/busy-spinning.
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
