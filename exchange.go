package msgin

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"time"

	"github.com/jonboulle/clockwork"
)

// defaultReplyTimeout bounds how long Exchange waits for a correlated reply when
// the caller ctx carries no deadline. 30s is comfortably above any plausible
// in-process round-trip while still failing safe (a deadline-less ctx cannot
// hang a waiter — and its registry slot — forever). Override with WithReplyTimeout;
// the effective deadline is always min(ctx deadline, this).
const defaultReplyTimeout = 30 * time.Second

// RequestReplyExchange is the narrow SPI a gateway delegates to: it sends a
// request and returns the correlated reply (or an error). ChannelExchange is the
// in-process implementation; a future HTTP/NATS adapter implements Exchange for
// a real external round-trip, so both gateway façades work over it unchanged.
type RequestReplyExchange interface {
	Exchange(ctx context.Context, req Message[any]) (Message[any], error)
}

// replyCorrelator maps a request's correlation id to a one-shot reply slot and
// demuxes incoming replies back to the blocked waiter. It owns no goroutine: the
// reply receiver runs on the reply channel's driving goroutine; each waiter is
// the caller's own goroutine.
type replyCorrelator struct {
	mu      sync.Mutex
	waiters map[string]chan Message[any]
	closed  bool
}

func newReplyCorrelator() *replyCorrelator {
	return &replyCorrelator{waiters: make(map[string]chan Message[any])}
}

// register inserts a fresh cap-1 slot for id and returns it with a deregister
// func. err is ErrGatewayClosed if the correlator is closed, or
// ErrDuplicateCorrelation if id already has an in-flight waiter (audit G1 — the
// uniqueness the whole design leans on, enforced at the primitive).
//
// Uniqueness is required across the exchange LIFETIME, not just concurrently
// (audit N1): the guard blocks a concurrent duplicate, but a caller that REUSES
// an id sequentially after a prior request gave up (timeout/cancel) can have the
// prior request's genuinely-late reply delivered to the new waiter. The façades
// mint fresh 128-bit ids so they never hit this; direct ChannelExchange callers
// must use unique ids.
//
// deregister returns true only if it removed OUR slot (the waiter still owned
// it, so no delivery is in flight). It returns false if our slot was already
// gone — claimed by a concurrent deliver (a reply is committed to it) or closed
// by closeAll — INCLUDING the case where a different caller has since
// registered the same id. That identity check is what makes false imply
// deliver-or-closeAll, and therefore what makes giveUp's drain bounded
// (audit G4/H-1). On false the caller must drain the slot.
func (c *replyCorrelator) register(id string) (slot chan Message[any], deregister func() bool, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil, nil, ErrGatewayClosed
	}
	if _, exists := c.waiters[id]; exists {
		return nil, nil, ErrDuplicateCorrelation
	}
	slot = make(chan Message[any], 1)
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
func (c *replyCorrelator) deliver(id string, reply Message[any]) bool {
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
	timeout    time.Duration
	timeoutSet bool
	clock      clockwork.Clock
	logger     *slog.Logger
	unmatched  OutboundAdapter
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
func WithUnmatchedReplySink(out OutboundAdapter) ExchangeOption {
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
func WithExchangeLogger(l *slog.Logger) ExchangeOption {
	return func(c *exchangeConfig) {
		if l != nil {
			c.logger = l
		}
	}
}

// ChannelExchange is the in-process RequestReplyExchange: it sends requests to a
// request channel and correlates replies received on a reply channel (which it
// owns as the sole subscriber). Construct it with NewChannelExchange.
type ChannelExchange struct {
	request   MessageChannel
	corr      *replyCorrelator
	timeout   time.Duration
	clock     clockwork.Clock
	logger    *slog.Logger
	unmatched OutboundAdapter
}

var _ RequestReplyExchange = (*ChannelExchange)(nil)

// NewChannelExchange builds a ChannelExchange over request/reply channels. It
// subscribes its reply receiver onto reply, so reply must be dedicated to this
// exchange (a second subscriber is ErrChannelSubscribed). A nil channel is
// ErrNilChannel; an explicit non-positive WithReplyTimeout is ErrInvalidReplyTimeout.
func NewChannelExchange(request, reply MessageChannel, opts ...ExchangeOption) (*ChannelExchange, error) {
	if request == nil || reply == nil {
		return nil, ErrNilChannel
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
		return nil, ErrInvalidReplyTimeout
	}
	e := &ChannelExchange{
		request:   request,
		corr:      newReplyCorrelator(),
		timeout:   cfg.timeout,
		clock:     cfg.clock,
		logger:    cfg.logger,
		unmatched: cfg.unmatched,
	}
	if err := reply.Subscribe(e.receiver()); err != nil {
		return nil, err
	}
	return e, nil
}

// receiver is the MessageHandler subscribed to the reply channel. It demuxes each
// reply to its waiter by HeaderCorrelationID, or handles an unmatched reply. It
// always returns nil so a slow/absent waiter never fails the reply producer.
func (e *ChannelExchange) receiver() MessageHandler {
	return HandlerFunc(func(ctx context.Context, reply Message[any]) error {
		id, _ := reply.Headers().String(HeaderCorrelationID)
		if e.corr.deliver(id, reply) {
			return nil
		}
		e.routeUnmatched(ctx, reply)
		return nil
	})
}

// routeUnmatched sends an unmatched reply to the configured sink, or warn-logs
// and drops it. A sink error is logged, never propagated (audit G4/§3.5).
func (e *ChannelExchange) routeUnmatched(ctx context.Context, reply Message[any]) {
	if e.unmatched != nil {
		if err := e.unmatched.Send(ctx, reply); err != nil {
			e.logger.Warn("msgin: unmatched-reply sink failed", "id", reply.ID(), "err", err)
		}
		return
	}
	id, _ := reply.Headers().String(HeaderCorrelationID)
	e.logger.Warn("msgin: dropping unmatched gateway reply", "id", reply.ID(), "correlation-id", id)
}

// Exchange sends req to the request channel and blocks for the reply correlated
// by req's HeaderCorrelationID, returning it or ctx.Err()/ErrReplyTimeout/
// ErrGatewayClosed. An empty correlation id is ErrNoCorrelation and a duplicate
// in-flight id is ErrDuplicateCorrelation (audit G1). Both are direct-caller
// guards: the Gateway/OutboundGateway façades always set a fresh non-empty id,
// so they never surface them (audit N3). A request-channel send error propagates
// (waiter deregistered).
func (e *ChannelExchange) Exchange(ctx context.Context, req Message[any]) (Message[any], error) {
	id, _ := req.Headers().String(HeaderCorrelationID)
	if id == "" {
		return Message[any]{}, ErrNoCorrelation
	}
	slot, deregister, err := e.corr.register(id)
	if err != nil {
		return Message[any]{}, err // ErrGatewayClosed | ErrDuplicateCorrelation
	}
	if err := e.request.Send(ctx, req); err != nil {
		e.giveUp(ctx, slot, deregister)
		return Message[any]{}, err
	}
	timer := e.clock.NewTimer(e.timeout)
	defer timer.Stop()
	select {
	case reply, open := <-slot:
		if !open {
			return Message[any]{}, ErrGatewayClosed // closeAll closed our slot
		}
		return reply, nil
	case <-ctx.Done():
		e.giveUp(ctx, slot, deregister)
		return Message[any]{}, ctx.Err()
	case <-timer.Chan():
		e.giveUp(ctx, slot, deregister)
		return Message[any]{}, ErrReplyTimeout
	}
}

// giveUp reconciles a waiter that is abandoning its slot (send error, ctx,
// timeout) with a possibly-concurrent deliver. If deregister removed the slot,
// no reply was in flight and we are done. Otherwise a deliver already claimed
// the slot and is committed to sending (or closeAll closed it): we block on the
// slot and route any delivered reply to the unmatched path rather than dropping
// it silently (audit G4). context.WithoutCancel is used so the sink send is not
// itself cancelled by the ctx that just fired.
func (e *ChannelExchange) giveUp(ctx context.Context, slot chan Message[any], deregister func() bool) {
	if deregister() {
		return
	}
	if reply, ok := <-slot; ok {
		e.routeUnmatched(context.WithoutCancel(ctx), reply)
	}
}

// Close stops the exchange: subsequent Exchange calls return ErrGatewayClosed and
// every waiter pending at Close time is failed with it. Idempotent. The reply
// receiver remains subscribed (channels have no unsubscribe); it simply finds no
// waiters after Close. Close returns nil today; the signature allows a future
// adapter-backed exchange to report a teardown error.
func (e *ChannelExchange) Close() error {
	e.corr.closeAll()
	return nil
}
