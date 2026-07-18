package msgin

import "context"

type routerConfig struct{ defaultCh MessageChannel }

// RouterOption configures a Router endpoint.
type RouterOption func(*routerConfig)

// WithDefaultChannel sets the channel a Router uses when pick resolves no
// destination (returns a nil channel). Without it, an unresolved message is
// ErrNoRoute — the safe, visible default (an unroutable message is usually a
// misconfiguration you want surfaced, not silently dropped).
func WithDefaultChannel(ch MessageChannel) RouterOption {
	return func(c *routerConfig) { c.defaultCh = ch }
}

// Router is a Content-Based Router endpoint: pick selects the destination for
// each message. A resolved channel receives it; a nil channel routes to
// WithDefaultChannel if set, else ErrNoRoute; a pick error propagates (the
// returned channel is ignored). A nil pick yields ErrNilFunc. Router implements
// MessageHandler — subscribe it to a channel to place it after a Chain, or use
// it as a flow head via NewConsumer[any](src, router.Handle).
type Router struct {
	pick func(ctx context.Context, m Message[any]) (MessageChannel, error)
	cfg  routerConfig
}

var _ MessageHandler = (*Router)(nil)

// NewRouter builds a Router from pick and options. A nil pick is tolerated at
// construction and surfaces as ErrNilFunc at Handle time (no panic on input).
func NewRouter(pick func(ctx context.Context, m Message[any]) (MessageChannel, error), opts ...RouterOption) *Router {
	r := &Router{pick: pick}
	for _, opt := range opts {
		opt(&r.cfg)
	}
	return r
}

// Handle routes msg to the channel pick selects.
func (r *Router) Handle(ctx context.Context, msg Message[any]) error {
	if r.pick == nil {
		return ErrNilFunc
	}
	ch, err := r.pick(ctx, msg)
	if err != nil {
		return err
	}
	if ch == nil {
		if r.cfg.defaultCh == nil {
			return ErrNoRoute
		}
		ch = r.cfg.defaultCh
	}
	return ch.Send(ctx, msg)
}
