package routing

import (
	"context"
	"fmt"

	"github.com/kartaladev/msgin"
)

type routerConfig struct{ defaultCh msgin.MessageChannel }

// RouterOption configures a Router endpoint.
type RouterOption func(*routerConfig)

// WithDefaultChannel sets the channel a Router uses when pick resolves no
// destination (returns a nil channel). Without it, an unresolved message is
// ErrNoRoute — the safe, visible default (an unroutable message is usually a
// misconfiguration you want surfaced, not silently dropped).
func WithDefaultChannel(ch msgin.MessageChannel) RouterOption {
	return func(c *routerConfig) { c.defaultCh = ch }
}

// RouteFunc selects the destination channel for a message. It is the named
// behavior type behind the Content-Based Router pattern (EIP ch. 4); Spring
// Integration calls the equivalent contract
// org.springframework.integration.router.AbstractMessageRouter#determineTargetChannels.
//
// It is non-generic, over Message[any]: a router dispatches on content it
// inspects rather than on a payload type it asserts, and the destinations it
// chooses between are untyped [msgin.MessageChannel]s.
//
// A nil returned channel is not an error here — it means "no destination",
// which [WithDefaultChannel] absorbs and [msgin.ErrNoRoute] reports otherwise.
// Return an error only for a genuine routing failure; the channel is then
// ignored.
//
// The name drops the qualifier the package already carries (ADR 0029 §4).
//
// ASSIGNABILITY — what naming the type does and does not accept. These call
// shapes all still work at [NewRouter]: a bare closure literal, a variable or
// field of the equivalent UNNAMED func type, a func returning that unnamed
// type, a method value, and a plain top-level func declaration. The one shape
// that does NOT work is a caller's OWN NAMED func type: Go converts implicitly
// between two func types only when at least one side is unnamed, so
// `var r MyRoute; NewRouter(r)` is rejected — "cannot use r (variable of func
// type MyRoute) as routing.RouteFunc value". Being non-generic, RouteFunc at
// least names both types in that error, where the generic behavior types report
// an opaque inference failure. Convert at the call site: NewRouter(RouteFunc(r)).
type RouteFunc func(ctx context.Context, m msgin.Message[any]) (msgin.MessageChannel, error)

// Router is a Content-Based Router endpoint: pick selects the destination for
// each message. A resolved channel receives it; a nil channel routes to
// WithDefaultChannel if set, else ErrNoRoute; a pick error propagates (the
// returned channel is ignored). A nil pick yields ErrNilFunc naming its
// position — PERMANENT (D-M): routed to the invalid-message channel rather than
// retried to the dead-letter sink, with errors.Is(err, msgin.ErrNilFunc) still
// matching (see [msgin.ErrNilFunc]). ErrNoRoute, by contrast, stays TRANSIENT:
// pick is evaluated per message, so a message unroutable now may be routable
// after a config reload. Router implements
// MessageHandler — subscribe it to a channel to place it after a Chain, or use
// it as a flow head via NewConsumer[any](src, router.Handle).
type Router struct {
	pick RouteFunc
	cfg  routerConfig
}

var _ msgin.MessageHandler = (*Router)(nil)

// NewRouter builds a Router from pick and options. A nil pick is tolerated at
// construction and surfaces as a permanent ErrNilFunc at Handle time, named
// with its position (no panic on input) — pick is fixed here and cannot become
// non-nil later, so retrying can never succeed.
func NewRouter(pick RouteFunc, opts ...RouterOption) *Router {
	r := &Router{pick: pick}
	for _, opt := range opts {
		opt(&r.cfg)
	}
	return r
}

// Handle routes msg to the channel pick selects.
func (r *Router) Handle(ctx context.Context, msg msgin.Message[any]) error {
	if r.pick == nil {
		// D-M (ADR 0029 §5.0b): pick is set once in NewRouter, so the fault
		// cannot change for the message's lifetime — permanent, not retried.
		return fmt.Errorf("%w: routing.Router.Handle: nil pick", msgin.Permanent(msgin.ErrNilFunc))
	}
	ch, err := r.pick(ctx, msg)
	if err != nil {
		return err
	}
	if ch == nil {
		if r.cfg.defaultCh == nil {
			return msgin.ErrNoRoute
		}
		ch = r.cfg.defaultCh
	}
	return ch.Send(ctx, msg)
}
