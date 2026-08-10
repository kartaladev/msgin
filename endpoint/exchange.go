package endpoint

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"time"

	"github.com/jonboulle/clockwork"
	"github.com/kartaladev/msgin"
)

// defaultReplyTimeout bounds how long Exchange waits for a correlated reply when
// the caller ctx carries no deadline. 30s is comfortably above any plausible
// in-process round-trip while still failing safe (a deadline-less ctx cannot
// hang a waiter — and its registry slot — forever). Override with WithReplyTimeout;
// the effective deadline is always min(ctx deadline, this).
const defaultReplyTimeout = 30 * time.Second

// replyCorrelator maps a request's correlation id to a one-shot reply slot and
// demuxes incoming replies back to the blocked waiter. It owns no goroutine: the
// reply receiver runs on the reply channel's driving goroutine; each waiter is
// the caller's own goroutine.
type replyCorrelator struct {
	mu      sync.Mutex
	waiters map[string]chan msgin.Message[any]
	closed  bool
}

func newReplyCorrelator() *replyCorrelator {
	return &replyCorrelator{waiters: make(map[string]chan msgin.Message[any])}
}

// register inserts a fresh cap-1 slot for id and returns it with a deregister
// func. err is ErrGatewayClosed if the correlator is closed, or
// ErrDuplicateCorrelation if id already has an in-flight waiter (audit G1 — the
// uniqueness the whole design leans on, enforced at the primitive).
//
// Uniqueness is required across the exchange LIFETIME, not just concurrently
// (audit N1): the guard blocks a concurrent duplicate, but a caller that REUSES
// an id sequentially after a prior request gave up — timeout, cancel, send
// error, OR A PANIC UNWINDING OUT OF THE FLOW — can have the prior request's
// genuinely-late reply delivered to the new waiter. The façades mint fresh
// 128-bit ids so they never hit this; direct ChannelExchange callers must use
// unique ids.
//
// SECURITY: the panic arm joins this window as of Spec 012. Before that fix a
// panic leaked the slot, so reuse of that id failed closed with
// ErrDuplicateCorrelation; it now succeeds. That is the intended trade (the
// leak was unbounded), but it means a CLIENT-KEYED exchange — msghttp's
// WithTrustedCorrelationID — must still treat values as unguessable and
// single-use. See ADR 0022 Addendum A4.
//
// deregister returns true only if it removed OUR slot (the waiter still owned
// it, so no delivery is in flight). It returns false if our slot was already
// gone — claimed by a concurrent deliver (a reply is committed to it) or closed
// by closeAll — INCLUDING the case where a different caller has since
// registered the same id. That identity check is what makes false imply
// deliver-or-closeAll, and therefore what makes giveUp's drain bounded
// (audit G4/H-1). On false the caller must drain the slot.
func (c *replyCorrelator) register(id string) (slot chan msgin.Message[any], deregister func() bool, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil, nil, msgin.ErrGatewayClosed
	}
	if _, exists := c.waiters[id]; exists {
		return nil, nil, msgin.ErrDuplicateCorrelation
	}
	slot = make(chan msgin.Message[any], 1)
	c.waiters[id] = slot
	deregister = func() bool {
		c.mu.Lock()
		defer c.mu.Unlock()
		// Identity, not just key: a reused id can have OUR entry already
		// removed by deliver and a DIFFERENT caller's slot registered under the
		// same key. Deleting that one would drop our committed reply silently
		// and orphan theirs — leaving its owner blocked forever in giveUp, on a
		// slot no deliver can find and closeAll cannot close (ADR 0022 A2).
		if s, ok := c.waiters[id]; ok && s == slot {
			delete(c.waiters, id)
			return true
		}
		return false
	}
	return slot, deregister, nil
}

// deliver routes reply to the waiter for id, returning true if one matched. The
// slot is removed under the lock before the (non-blocking, cap-1) send, so it can
// never race closeAll onto a closed channel.
func (c *replyCorrelator) deliver(id string, reply msgin.Message[any]) bool {
	c.mu.Lock()
	slot, ok := c.waiters[id]
	if ok {
		delete(c.waiters, id)
	}
	c.mu.Unlock()
	if !ok {
		return false
	}
	slot <- reply
	return true
}

// closeAll marks the correlator closed and fails every pending waiter by closing
// its slot (a waiter reading a closed slot observes ErrGatewayClosed). Idempotent.
func (c *replyCorrelator) closeAll() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return
	}
	c.closed = true
	for id, slot := range c.waiters {
		close(slot)
		delete(c.waiters, id)
	}
}

type exchangeConfig struct {
	timeout     time.Duration
	timeoutSet  bool
	clock       clockwork.Clock
	logger      *slog.Logger
	unmatched   msgin.OutboundAdapter
	allowShared bool
}

// ExchangeOption configures a ChannelExchange built by NewChannelExchange.
type ExchangeOption func(*exchangeConfig)

// WithReplyTimeout overrides the default 30s reply timeout. The effective
// deadline is min(ctx deadline, this). A non-positive value is ErrInvalidReplyTimeout.
func WithReplyTimeout(d time.Duration) ExchangeOption {
	return func(c *exchangeConfig) { c.timeout, c.timeoutSet = d, true }
}

// WithUnmatchedReplySink routes replies with no pending waiter (already
// timed-out/cancelled, or an unknown correlation id) to out instead of logging
// and dropping them. A sink error is logged, never propagated to the reply sender.
//
// The sink's Send should be non-blocking or promptly bounded: on the giveUp
// drain path (a reply that raced a timeout/cancel) it runs on the abandoning
// caller's goroutine, so a slow synchronous sink delays that Exchange's return.
// A nil sink is a no-op (leaves the default log-and-drop behaviour).
//
// The sink must also neither panic nor block: it can run inside Exchange's
// deferred cleanup while a handler panic is already unwinding. A second panic
// would replace — and therefore mask — the consumer's original one, and a
// blocking sink stalls the unwind itself (Spec 012 §5.3). A caller parked in a
// blocking sink CANNOT be rescued by Close: it is not waiting on the reply slot
// or on ctx, so only the sink returning frees it.
func WithUnmatchedReplySink(out msgin.OutboundAdapter) ExchangeOption {
	return func(c *exchangeConfig) {
		if out != nil {
			c.unmatched = out
		}
	}
}

// WithExchangeClock injects the clock used for the reply timeout (tests use a
// clockwork.FakeClock). A nil clock leaves the real-clock default.
func WithExchangeClock(clock clockwork.Clock) ExchangeOption {
	return func(c *exchangeConfig) {
		if clock != nil {
			c.clock = clock
		}
	}
}

// WithExchangeLogger injects the structured logger (default: a discard logger).
//
// The logger must neither panic nor block: routeUnmatched can call it on
// either branch — the sink-error path and the default warn-log-and-drop
// path — and it can run inside Exchange's deferred cleanup while a handler
// panic is already unwinding, where a panicking slog.Handler would replace —
// and therefore mask — the consumer's original panic, and a blocking one would
// stall the unwind (Spec 012 §5.3).
func WithExchangeLogger(l *slog.Logger) ExchangeOption {
	return func(c *exchangeConfig) {
		if l != nil {
			c.logger = l
		}
	}
}

// WithSharedReplyChannel accepts a reply channel that reports itself
// non-exclusive, instead of rejecting it with msgin.ErrSharedReplyChannel.
//
// IT SUPPRESSES THE PROBE ENTIRELY. NewChannelExchange consults this flag
// BEFORE the msgin.ExclusiveSubscribable type assertion, so a construction
// carrying this option never calls the channel's SingleSubscriber at all.
//
// THE CONSEQUENCE BEING OPTED INTO. Every reply sent to a fan-out reply channel
// is copied to EVERY subscriber, so a second exchange over the same channel
// receives a full copy of every reply belonging to the first, finds no waiter
// for the correlation id, and hands that copy to its WithUnmatchedReplySink —
// typically a dead-letter or audit sink, i.e. a place a payload is written down.
// That is legitimate for a deliberate tap or audit subscriber alongside the
// exchange; it is a data leak when it was not intended.
//
// IT DOES NOT CONFER SHAREABILITY. The option suppresses msgin's check, not the
// channel's own. A channel that enforces exclusivity itself still rejects the
// second exchange, and it does so from Subscribe rather than from the probe:
//
//	a, _ := endpoint.NewChannelExchange(reqA, direct, endpoint.WithSharedReplyChannel())  // ok
//	b, err := endpoint.NewChannelExchange(reqB, direct, endpoint.WithSharedReplyChannel())
//	// err = msgin.ErrChannelSubscribed — from DirectChannel.Subscribe, not from the probe
//
// The same holds for a channel.PublishSubscribeChannel built with
// channel.WithSingleSubscriber(). Neither the option's name nor
// msgin.ErrChannelSubscribed's text hints that the option just passed cannot
// help, so it is stated here (ADR 0030 §5).
func WithSharedReplyChannel() ExchangeOption {
	return func(c *exchangeConfig) { c.allowShared = true }
}

// ChannelExchange is the in-process RequestReplyExchange: it sends requests to a
// request channel and correlates replies received on a reply channel (which it
// owns as the sole subscriber). Construct it with NewChannelExchange.
type ChannelExchange struct {
	request   msgin.MessageChannel
	replySub  msgin.Subscription
	corr      *replyCorrelator
	timeout   time.Duration
	clock     clockwork.Clock
	logger    *slog.Logger
	unmatched msgin.OutboundAdapter
}

var _ msgin.RequestReplyExchange = (*ChannelExchange)(nil)

// NewChannelExchange builds a ChannelExchange over a send-only request channel
// and a subscribable reply channel. request may be any MessageChannel — a
// DirectChannel, a pollable QueueChannel, a PublishSubscribeChannel, or any
// OutboundAdapter.
//
// REPLY MUST BE DEDICATED TO THIS EXCHANGE. The exchange subscribes its reply
// receiver onto reply and owns that subscription until Close. Sharing one reply
// channel between two exchanges makes every reply fan out to BOTH receivers —
// the non-owner finds no waiter for the correlation id and hands a full copy of
// the other exchange's reply to its WithUnmatchedReplySink (typically a
// dead-letter or audit sink). Exclusivity is therefore PROBED at construction,
// through the optional msgin.ExclusiveSubscribable capability, with FOUR
// outcomes:
//
//  1. REJECTED — the channel implements the probe and reports non-exclusive:
//     msgin.ErrSharedReplyChannel. The probe runs BEFORE reply.Subscribe, so a
//     rejected exchange leaves no subscription behind. A plain
//     channel.PublishSubscribeChannel is this case; pass
//     channel.WithSingleSubscriber() to the channel, or WithSharedReplyChannel()
//     here to accept the fan-out deliberately. A probe that PANICS is recovered
//     and read as non-exclusive (fail closed), and is rejected with the same
//     sentinel wrapping the recovered value — read err.Error() for the cause.
//  2. ACCEPTED — the channel implements the probe and reports exclusive. A
//     channel.DirectChannel always does; so does a
//     channel.PublishSubscribeChannel built with channel.WithSingleSubscriber().
//  3. ACCEPTED UNPROBED — the channel does not implement the probe at all. The
//     capability is optional so the SPI stays open to third-party reply channels
//     that predate it: msgin rejects what it can prove is wrong rather than
//     everything it cannot see. THIS ARM IS ALSO REACHED FROM IN-TREE CHANNELS.
//     Any wrapper that does not itself declare SingleSubscriber is accepted
//     here, HOWEVER IT HOLDS THE CHANNEL IT WRAPS — so it is accepted even when
//     the channel it wraps would be rejected. The one-line decorator
//     struct{ msgin.SubscribableChannel } is the commonest instance, not the
//     boundary: a wrapper holding the concrete channel in a named field with
//     hand-written Send/Subscribe forwarders strips the probe identically. A
//     decorator over a fan-out channel must declare or forward SingleSubscriber
//     itself for the check to apply.
//  4. ACCEPTED ON THE CHANNEL'S OWN WORD, which msgin cannot verify. The probe
//     is a REPORT, not a proof, and the guarantee is bounded to "msgin will not
//     silently accept a channel that has told it sharing is permitted". A
//     channel that reports exclusive is exclusive only within this process:
//     nothing here observes another instance, and a registry that could would be
//     exactly the in-process global state a multi-instance deployment must not
//     depend on (ADR 0028 §6). A channel MUST report non-exclusive whenever a
//     message sent to it can reach any recipient other than its single
//     registered subscriber, INCLUDING one in another process; a broker-backed
//     channel may report exclusive only when the broker guarantees the
//     destination is private to this process's subscription — a per-instance
//     NATS _INBOX reply subject, an exclusive auto-delete AMQP reply queue, i.e.
//     the Return Address pattern. msgin takes that answer on trust.
//
// A nil channel is ErrNilChannel; an explicit non-positive WithReplyTimeout is
// ErrInvalidReplyTimeout. A reply channel whose Subscribe breaks its contract by
// returning a nil Subscription alongside a nil error is ErrNilSubscription — the
// exchange owns that subscription until Close, so it must be usable now. An
// error from reply.Subscribe is returned unwrapped, so subscribing to a channel
// whose slot is already taken surfaces as msgin.ErrChannelSubscribed. That is
// the adjacent condition to msgin.ErrSharedReplyChannel and the distinction is
// deliberate: the shared-reply sentinel means the channel's POLICY permits other
// recipients, msgin.ErrChannelSubscribed means the channel is exclusive and its
// one slot is taken.
func NewChannelExchange(request msgin.MessageChannel, reply msgin.SubscribableChannel, opts ...ExchangeOption) (*ChannelExchange, error) {
	if request == nil || reply == nil {
		return nil, msgin.ErrNilChannel
	}
	cfg := exchangeConfig{
		timeout: defaultReplyTimeout,
		clock:   clockwork.NewRealClock(),
		logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	if cfg.timeoutSet && cfg.timeout <= 0 {
		return nil, msgin.ErrInvalidReplyTimeout
	}
	// The opt-out is tested FIRST, so it suppresses the probe rather than only
	// the rejection: a caller who has explicitly opted out never pays for a
	// third-party SingleSubscriber that locks or does work (ADR 0030 §3, D-M2).
	// Both nil-channel and timeout checks precede this — a nil channel cannot be
	// probed — and the probe precedes reply.Subscribe, so a rejected exchange
	// leaves no subscription behind.
	if !cfg.allowShared {
		if ex, ok := reply.(msgin.ExclusiveSubscribable); ok {
			single, cause := safeSingleSubscriber(ex, cfg.logger)
			if !single {
				if cause != nil {
					return nil, fmt.Errorf("%w: %w", msgin.ErrSharedReplyChannel, cause)
				}
				return nil, msgin.ErrSharedReplyChannel
			}
		}
	}
	e := &ChannelExchange{
		request:   request,
		corr:      newReplyCorrelator(),
		timeout:   cfg.timeout,
		clock:     cfg.clock,
		logger:    cfg.logger,
		unmatched: cfg.unmatched,
	}
	sub, err := reply.Subscribe(e.receiver())
	if err != nil {
		return nil, err
	}
	if sub == nil {
		// reply is caller-supplied SPI: a third-party SubscribableChannel that
		// returns (nil, nil) breaks Subscribe's contract. Reject it here, where the
		// offending input is still named, instead of letting Close nil-deref on
		// e.replySub.Cancel() much later (CLAUDE.md: never panic on caller input).
		return nil, msgin.ErrNilSubscription
	}
	e.replySub = sub
	return e, nil
}

// safeSingleSubscriber invokes the caller-supplied probe, recovering a panic to
// FAIL CLOSED (report non-exclusive) AND returning the recovered value as an
// error so the guard can wrap it into ErrSharedReplyChannel. A probe that
// panics has not proven exclusivity, so the conservative answer is false and
// the exchange is rejected — but the panic is what the caller has to fix, and a
// channel that is genuinely exclusive would otherwise be reported as "not
// exclusive to this exchange" with the cause discarded. Mirrors the eight
// recover-wrappers that embed %v of the recovered value in the error they
// return (consumer.go, poller.go, producer.go, channel/pubsub.go).
func safeSingleSubscriber(ex msgin.ExclusiveSubscribable, log *slog.Logger) (b bool, cause error) {
	defer func() {
		if r := recover(); r != nil {
			b, cause = false, fmt.Errorf("SingleSubscriber panicked: %v", r)
			log.Warn("msgin: reply channel's SingleSubscriber panicked; treating it as non-exclusive",
				"panic", r)
		}
	}()
	return ex.SingleSubscriber(), nil
}

// receiver is the MessageHandler subscribed to the reply channel. It demuxes each
// reply to its waiter by HeaderCorrelationID, or handles an unmatched reply. It
// always returns nil so a slow/absent waiter never fails the reply producer.
func (e *ChannelExchange) receiver() msgin.MessageHandler {
	return msgin.HandlerFunc(func(ctx context.Context, reply msgin.Message[any]) error {
		id, _ := reply.Headers().String(msgin.HeaderCorrelationID)
		if e.corr.deliver(id, reply) {
			return nil
		}
		e.routeUnmatched(ctx, reply)
		return nil
	})
}

// routeUnmatched sends an unmatched reply to the configured sink, or warn-logs
// and drops it. A sink error is logged, never propagated (audit G4/§3.5).
func (e *ChannelExchange) routeUnmatched(ctx context.Context, reply msgin.Message[any]) {
	if e.unmatched != nil {
		if err := e.unmatched.Send(ctx, reply); err != nil {
			e.logger.Warn("msgin: unmatched-reply sink failed", "id", reply.ID(), "err", err)
		}
		return
	}
	id, _ := reply.Headers().String(msgin.HeaderCorrelationID)
	e.logger.Warn("msgin: dropping unmatched gateway reply", "id", reply.ID(), "correlation-id", id)
}

// Exchange sends req to the request channel and blocks for the reply correlated
// by req's HeaderCorrelationID, returning it or ctx.Err()/ErrReplyTimeout/
// ErrGatewayClosed. An empty correlation id is ErrNoCorrelation and a duplicate
// in-flight id is ErrDuplicateCorrelation (audit G1). Both are direct-caller
// guards: the Gateway/OutboundGateway façades always set a fresh non-empty id,
// so they never surface them (audit N3).
//
// A request-channel send error propagates, and any abandonment — send error,
// ctx, reply timeout, or a PANIC unwinding out of the flow — releases the
// waiter via a single deferred reconciler. The panic is never recovered here:
// it propagates unchanged, with its slot already reclaimed (ADR 0022 Addendum A1).
func (e *ChannelExchange) Exchange(ctx context.Context, req msgin.Message[any]) (msgin.Message[any], error) {
	id, _ := req.Headers().String(msgin.HeaderCorrelationID)
	if id == "" {
		return msgin.Message[any]{}, msgin.ErrNoCorrelation
	}
	slot, deregister, err := e.corr.register(id)
	if err != nil {
		return msgin.Message[any]{}, err // ErrGatewayClosed | ErrDuplicateCorrelation
	}
	// settled is false on every exit that abandons the slot — send error, ctx,
	// timeout, AND a panic unwinding out of request.Send (a DirectChannel runs
	// the flow synchronously on this goroutine). The deferred reconciler is the
	// SINGLE give-up site; it deliberately does not recover, so a consumer panic
	// keeps unwinding with its original value and stack (ADR 0022 Addendum A1).
	settled := false
	defer func() {
		if !settled {
			e.giveUp(ctx, slot, deregister)
		}
	}()
	if err := e.request.Send(ctx, req); err != nil {
		return msgin.Message[any]{}, err
	}
	timer := e.clock.NewTimer(e.timeout)
	defer timer.Stop()
	select {
	case reply, open := <-slot:
		// The ONLY state in which the slot is provably no longer ours: a
		// deliver consumed it, or closeAll removed and closed it. Running
		// giveUp here would deadlock — deregister returns false and the drain
		// blocks on an emptied, never-closed channel (Spec 012 §5, arm 3).
		settled = true
		if !open {
			return msgin.Message[any]{}, msgin.ErrGatewayClosed // closeAll closed our slot
		}
		return reply, nil
	case <-ctx.Done():
		return msgin.Message[any]{}, ctx.Err()
	case <-timer.Chan():
		return msgin.Message[any]{}, msgin.ErrReplyTimeout
	}
}

// giveUp reconciles a waiter that is abandoning its slot — send error, ctx,
// reply timeout, or a PANIC unwinding out of request.Send — with a
// possibly-concurrent deliver. If deregister removed the slot, no reply was in
// flight and we are done. Otherwise a deliver already claimed the slot and is
// committed to sending (or closeAll closed it): we block on the slot and route
// any delivered reply to the unmatched path rather than dropping it silently
// (audit G4). context.WithoutCancel is used so the sink send is not itself
// cancelled by the ctx that just fired.
func (e *ChannelExchange) giveUp(ctx context.Context, slot chan msgin.Message[any], deregister func() bool) {
	if deregister() {
		return
	}
	if reply, ok := <-slot; ok {
		e.routeUnmatched(context.WithoutCancel(ctx), reply)
	}
}

// Close stops the exchange: subsequent Exchange calls return ErrGatewayClosed,
// every waiter pending at Close time is failed with it, and the reply
// subscription this exchange owns is cancelled, releasing the reply channel.
// Idempotent — both the correlator shutdown and Subscription.Cancel are.
//
// Cancelling the subscription is what keeps the widened reply contract
// leak-free: the receiver closure holds the exchange (and its correlator) alive
// for as long as it stays registered, and on a DirectChannel it also holds the
// channel's single subscriber slot, so the channel could never be reused
// (ADR 0028 §6.1).
//
// BEHAVIOR AFTER CLOSE. Because the receiver is unsubscribed, a reply arriving
// later is the CHANNEL's problem, not the exchange's: a DirectChannel reports
// ErrNoSubscriber to its sender, and a PublishSubscribeChannel delivers to
// whoever else is subscribed. It is no longer routed to WithUnmatchedReplySink —
// that sink covers replies with no pending WAITER on a live exchange. Close
// after the last in-flight request settles, or accept that late replies surface
// at the sender.
//
// Close returns nil today; the signature allows a future adapter-backed exchange
// to report a teardown error.
func (e *ChannelExchange) Close() error {
	e.corr.closeAll()
	e.replySub.Cancel()
	return nil
}
