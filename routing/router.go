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
	pick func(ctx context.Context, m msgin.Message[any]) (msgin.MessageChannel, error)
	cfg  routerConfig
}

var _ msgin.MessageHandler = (*Router)(nil)

// NewRouter builds a Router from pick and options. A nil pick is tolerated at
// construction and surfaces as a permanent ErrNilFunc at Handle time, named
// with its position (no panic on input) — pick is fixed here and cannot become
// non-nil later, so retrying can never succeed.
func NewRouter(pick func(ctx context.Context, m msgin.Message[any]) (msgin.MessageChannel, error), opts ...RouterOption) *Router {
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
