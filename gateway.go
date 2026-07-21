package msgin

import "context"

// Gateway is the inbound EIP Messaging Gateway: a typed, application-facing
// request-reply bridge into a message flow. Request turns a Req into a Message,
// drives it through a RequestReplyExchange, and returns the correlated Rep reply
// (or an error/timeout) — hiding the messaging from the caller. Build it with
// NewGateway.
type Gateway[Req, Rep any] struct {
	exchange RequestReplyExchange
}

type gatewayConfig struct{}

// GatewayOption configures a Gateway built by NewGateway. Reserved for future
// options (e.g. request-header seeding); none are defined yet.
type GatewayOption[Req, Rep any] func(*gatewayConfig)

// NewGateway builds an inbound Gateway over x. A nil exchange is ErrNilExchange.
func NewGateway[Req, Rep any](x RequestReplyExchange, opts ...GatewayOption[Req, Rep]) (*Gateway[Req, Rep], error) {
	if x == nil {
		return nil, ErrNilExchange
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
func OutboundGateway(x RequestReplyExchange) Step {
	return func(next MessageHandler) MessageHandler {
		return HandlerFunc(func(ctx context.Context, msg Message[any]) error {
			savedVal, had := msg.Header(HeaderCorrelationID) // raw presence — NOT Headers().String (audit G5)
			reply, err := x.Exchange(ctx, msg.WithHeader(HeaderCorrelationID, randomID()))
			if err != nil {
				return err
			}
			if had {
				reply = reply.WithHeader(HeaderCorrelationID, savedVal)
			} else {
				reply = reply.WithoutHeader(HeaderCorrelationID)
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
	msg := New(req).WithHeader(HeaderCorrelationID, randomID())
	reply, err := g.exchange.Exchange(ctx, boxMessage(msg))
	if err != nil {
		return zero, err
	}
	out, err := PayloadOf[Rep](reply)
	if err != nil {
		return zero, err
	}
	return out.Payload(), nil
}
