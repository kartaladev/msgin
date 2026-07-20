package msgin

import (
	"context"
	"hash/fnv"
	"sync"
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
// same correlation key: Handle holds a per-key lock across the call to
// Send, and that lock is not reentrant — a cycle deadlocks.
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
func WithReleaseStrategy(fn func(MessageGroup) bool) AggregatorOption {
	return func(c *aggregatorConfig) { c.release = fn }
}

// WithCompletionSize releases a group once it holds n messages — sugar for a
// fixed-size release strategy when there is no HeaderSequenceSize to read.
// Exact-count (>= n, effectively == n once release fires and the group is
// immediately removed) is safe WITHIN one process because Handle serializes
// same-key work under the per-key lock; it is not a safe boundary across
// processes sharing a durable store with no cross-process lock (Phase 3 sql),
// nor against id-less duplicate members (no dedup).
func WithCompletionSize(n int) AggregatorOption {
	return func(c *aggregatorConfig) {
		c.release = func(g MessageGroup) bool { return len(g.Messages()) >= n }
	}
}

// WithGroupTimeout enables the optional expiry reaper (Run): a group whose
// CreatedAt is older than d, and still unreleased at a reap tick, is routed
// to the expired-group channel (WithExpiredGroupChannel) and removed rather
// than held forever. Unset (the default), no timeout applies and Run blocks
// until its context is cancelled without reaping anything.
//
// WithGroupTimeout without a paired WithExpiredGroupChannel makes
// NewAggregator return ErrExpiryChannelRequired: a partial group's members
// must go somewhere observable when they expire, rather than being silently
// dropped.
func WithGroupTimeout(d time.Duration) AggregatorOption {
	return func(c *aggregatorConfig) { c.timeout = d }
}

// WithExpiredGroupChannel sets the channel an expired partial group's members
// are sent to by the reaper (Run), one message at a time, in the group's
// arrival order. Required whenever WithGroupTimeout is set.
//
// Never wire this channel back into the same Aggregator's input under the
// same correlation key: the reaper holds a per-key lock across the call to
// Send, and that lock is not reentrant — a cycle deadlocks (see
// WithOutputChannel's doc for the identical Handle-side caveat).
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

// numShardLocks is the number of correlation-key shards an Aggregator locks
// over. Handle/the expiry reaper hold locks[hash(key)%numShardLocks] across
// their whole store-mutating sequence for that key, so distinct keys hashing
// to distinct shards proceed concurrently while same-key work serializes.
const numShardLocks = 256

// Aggregator is an EIP Aggregator endpoint: it correlates incoming messages
// into groups held in a MessageGroupStore, and when a group's release
// strategy is satisfied it aggregates the group's payloads (fn, supplied to
// NewAggregator) into one Message[B] sent to the output channel. It
// implements MessageHandler; place it as a chain's next (Subscribe) or a
// flow head (NewConsumer[any](src, agg.Handle)). Optionally it reaps expired
// partial groups via Run(ctx) — see WithGroupTimeout.
//
// Concurrency: Handle holds a lock sharded by the message's correlation key
// (hash/fnv over the key, mod numShardLocks) across the entire
// correlate-result→Add→release-check→aggregate→Send→Remove sequence for that
// key, so concurrent Handle calls for DIFFERENT keys (mostly) proceed in
// parallel while calls for the SAME key serialize — this is what makes
// WithCompletionSize's exact-count release safe under WithConcurrency > 1
// within one process. It is correct for a single process only; multi-process
// durability (sharing one store across processes with no cross-process lock)
// is a Phase 3 sql concern (transactional release).
//
// Settlement: store.Add persists a member before Handle returns (before the
// source Acks), and a release forwards to the output channel before
// store.Remove — documented at-least-once (within-process on
// memory.GroupStore; across restart on a future sql.GroupStore). A member
// redelivered after its group already completed forms a fresh group
// (idempotent downstream expected). A persistently-failing aggregate function
// orphans its group until expiry reaps it — set WithGroupTimeout if that
// matters for your flow.
type Aggregator struct {
	store  MessageGroupStore
	assert func(Message[any]) error
	agg    func(ctx context.Context, group []Message[any]) (Message[any], error)
	cfg    aggregatorConfig
	locks  [numShardLocks]sync.Mutex
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

// keyLock returns the shard lock for key (hash/fnv fnv32a mod numShardLocks).
func (a *Aggregator) keyLock(key string) *sync.Mutex {
	h := fnv.New32a()
	_, _ = h.Write([]byte(key))
	return &a.locks[h.Sum32()%numShardLocks]
}

// Handle correlates msg, adds it to its group, and — once the group's
// release strategy is satisfied — aggregates and forwards it to the output
// channel, then removes the group. A held (not-yet-released) group returns
// nil (the source Acks; durability now rests on the store). See the
// Aggregator doc for the concurrency and settlement contract.
func (a *Aggregator) Handle(ctx context.Context, msg Message[any]) error {
	if err := a.assert(msg); err != nil {
		return err // ErrPayloadType: fail fast, never added to the store
	}
	key, err := a.cfg.correlate(msg)
	if err != nil {
		return err // e.g. Permanent(ErrNoCorrelation)
	}

	mu := a.keyLock(key)
	mu.Lock()
	defer mu.Unlock()

	group, err := a.store.Add(ctx, key, msg)
	if err != nil {
		return err
	}
	if !a.cfg.release(group) {
		return nil // held; source Acks — durability now on the store
	}
	out, err := a.agg(ctx, group.Messages())
	if err != nil {
		return err // group NOT removed → a retry can re-release
	}
	if err := a.cfg.output.Send(ctx, out); err != nil {
		return err // NOT removed → retry
	}
	_, err = a.store.Remove(ctx, key)
	return err
}

// Run drives the optional expiry reaper: it periodically finds groups whose
// CreatedAt is older than WithGroupTimeout and are still unreleased, routes
// their held members to WithExpiredGroupChannel, and removes them, so a
// partial group with no matching final member does not wait forever.
//
// If WithGroupTimeout is unset (timeout <= 0), Run has nothing to reap: it
// blocks until ctx is cancelled and returns ctx.Err(), starting no ticker
// goroutine. Otherwise it ticks every timeout (via the clock injected by
// WithAggregatorClock, or the real clock by default) and, on each tick, reaps
// every group Expired as of clock.Now().Add(-timeout). A transient store
// error from Expired is skipped and retried on the next tick. Run returns
// ctx.Err() when ctx is cancelled; callers that want reaping typically start
// Run in its own goroutine alongside the flow that calls Handle.
//
// Each expired group is re-checked after being atomically removed: the SPI
// has no keyed read, so Run cannot check-then-remove atomically. Instead it
// removes first (under the same per-key lock Handle uses for that
// correlation key) and inspects what it actually removed. If that group's
// CreatedAt is no longer before the cutoff — a concurrent Handle released and
// re-formed a fresh group at the same key between Run's Expired() snapshot
// and this Remove — the removed members are restored (re-Added) rather than
// expired-routed, so a live, still-fillable group is never prematurely
// diverted. A group that failed to send to the expired channel is dropped
// after removal (at-most-once to the expired sink): Remove already
// committed, so there is nothing left to retry from.
func (a *Aggregator) Run(ctx context.Context) error {
	if a.cfg.timeout <= 0 {
		<-ctx.Done()
		return ctx.Err()
	}

	ticker := a.cfg.clock.NewTicker(a.cfg.timeout)
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

// reap runs one expiry sweep: see Run's doc for the full re-check contract.
func (a *Aggregator) reap(ctx context.Context) {
	cutoff := a.cfg.clock.Now().Add(-a.cfg.timeout)
	groups, err := a.store.Expired(ctx, cutoff)
	if err != nil {
		return // transient; next tick
	}
	for _, g := range groups {
		a.reapGroup(ctx, g, cutoff)
	}
}

// reapGroup runs the expiry sweep's per-group body under g's shard lock,
// released via defer so a panic from a downstream Send (via
// WithExpiredGroupChannel) cannot leak the lock — see Run's doc for the full
// re-check contract.
func (a *Aggregator) reapGroup(ctx context.Context, g MessageGroup, cutoff time.Time) {
	mu := a.keyLock(g.Key())
	mu.Lock()
	defer mu.Unlock()

	removed, err := a.store.Remove(ctx, g.Key())
	if err != nil || removed == nil {
		return // released concurrently, or already gone
	}
	if !removed.CreatedAt().Before(cutoff) {
		// A fresh group re-formed at this key since Expired(): restore it
		// rather than expired-route a live, still-fillable group.
		for _, m := range removed.Messages() {
			_, _ = a.store.Add(ctx, g.Key(), m)
		}
		return
	}
	for _, m := range removed.Messages() {
		_ = a.cfg.expired.Send(ctx, m)
	}
}
