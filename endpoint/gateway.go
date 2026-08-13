package endpoint

import (
	"context"

	"github.com/kartaladev/msgin"
)

// Gateway is the inbound EIP Messaging Gateway: a typed, application-facing
// request-reply bridge into a message flow. Request turns a Req into a Message,
// drives it through a RequestReplyExchange, and returns the correlated Rep reply
// (or an error/timeout) — hiding the messaging from the caller. Build it with
// NewGateway.
type Gateway[Req, Rep any] struct {
	exchange msgin.RequestReplyExchange
}

type gatewayConfig struct{}

// GatewayOption configures a Gateway built by NewGateway. Reserved for future
// options (e.g. request-header seeding); none are defined yet.
type GatewayOption[Req, Rep any] func(*gatewayConfig)

// NewGateway builds an inbound Gateway over x. A nil exchange is ErrNilExchange.
func NewGateway[Req, Rep any](x msgin.RequestReplyExchange, opts ...GatewayOption[Req, Rep]) (*Gateway[Req, Rep], error) {
	if x == nil {
		return nil, msgin.ErrNilExchange
	}
	var cfg gatewayConfig
	for _, opt := range opts {
		opt(&cfg)
	}
	return &Gateway[Req, Rep]{exchange: x}, nil
}

// OutboundGateway is the in-flow EIP outbound gateway: a Step that performs a
// request-reply exchange on the in-flight message and forwards the reply to next.
// It reuses HeaderCorrelationID as the reply key, so it mints a FRESH id for its
// own exchange and RESTORES the incoming correlation state on the reply — if the
// message arrived carrying an id it is put back (so an upstream splitter/aggregator
// group key survives the round-trip); if it arrived with none, the transient id is
// stripped. The fresh id guarantees a unique registry key even when the entering
// messages share a correlation id (e.g. all children of one split). An Exchange
// error propagates to the driving Consumer (retry/dead-letter owns it).
//
// A nil x does NOT panic. Unlike its sibling NewGateway — a constructor, which
// returns ErrNilExchange to the caller — this returns a Step, so the fault has
// nowhere to surface until evaluation: the handler fails with
// Permanent(ErrNilExchange) naming its position, exactly as msgin.To(nil) and
// routing.NewRouter(nil) do. The Permanent wrap is load-bearing, not decorative
// (D-M): a bare panic here would be recovered by the Consumer's safeHandle into
// ErrHandlerPanic, which IsPermanent classifies TRANSIENT, so a pure wiring
// mistake would be Nacked and redelivered forever instead of surfacing.
// errors.Is(err, msgin.ErrNilExchange) still matches through both wraps.
func OutboundGateway(x msgin.RequestReplyExchange) msgin.Step {
	if x == nil {
		return nilExchangeStep("endpoint.OutboundGateway: nil exchange")
	}
	return func(next msgin.MessageHandler) msgin.MessageHandler {
		return msgin.HandlerFunc(func(ctx context.Context, msg msgin.Message[any]) error {
			savedVal, had := msg.Header(msgin.HeaderCorrelationID) // raw presence — NOT Headers().String (audit G5)
			reply, err := x.Exchange(ctx, msg.WithHeader(msgin.HeaderCorrelationID, msgin.NewID()))
			if err != nil {
				return err
			}
			if had {
				reply = reply.WithHeader(msgin.HeaderCorrelationID, savedVal)
			} else {
				reply = reply.WithoutHeader(msgin.HeaderCorrelationID)
			}
			return next.Handle(ctx, reply)
		})
	}
}

// Request sends req into the flow and blocks for the correlated reply, returning
// it as a Rep. It always mints a fresh correlation id (the caller passes a raw
// Req with no headers), so concurrent requests never collide. A reply whose
// payload is not a Rep yields ErrPayloadType; ctx cancellation, ErrReplyTimeout,
// and ErrGatewayClosed propagate from the exchange.
func (g *Gateway[Req, Rep]) Request(ctx context.Context, req Req) (Rep, error) {
	var zero Rep
	msg := msgin.New(req).WithHeader(msgin.HeaderCorrelationID, msgin.NewID())
	reply, err := g.exchange.Exchange(ctx, boxMessage(msg))
	if err != nil {
		return zero, err
	}
	out, err := msgin.PayloadOf[Rep](reply)
	if err != nil {
		return zero, err
	}
	return out.Payload(), nil
}
