package msgin

import "context"

type filterConfig struct{ discard MessageChannel }

// FilterOption configures a Filter endpoint.
type FilterOption func(*filterConfig)

// WithDiscardChannel routes messages a Filter rejects (predicate false) to ch
// instead of silently dropping them (the default). The default — silent drop —
// matches the pattern's intent (a filter's job is to drop); set this when you
// need to audit or dead-letter filtered-out messages.
func WithDiscardChannel(ch MessageChannel) FilterOption {
	return func(c *filterConfig) { c.discard = ch }
}

// Filter is a Message Filter endpoint: it asserts the payload to A, evaluates
// pred, and forwards downstream when true. When false the message is dropped —
// silently by default, or sent to WithDiscardChannel if set. A predicate error
// (or a discard-channel send error) propagates; a non-A payload yields
// ErrPayloadType; a nil pred yields ErrNilFunc.
func Filter[A any](pred func(ctx context.Context, m Message[A]) (bool, error), opts ...FilterOption) Step {
	if pred == nil {
		return nilFuncStep()
	}
	var cfg filterConfig
	for _, opt := range opts {
		opt(&cfg)
	}
	return func(next MessageHandler) MessageHandler {
		return HandlerFunc(func(ctx context.Context, msg Message[any]) error {
			in, err := PayloadOf[A](msg)
			if err != nil {
				return err
			}
			pass, err := pred(ctx, in)
			if err != nil {
				return err
			}
			if pass {
				return next.Handle(ctx, msg)
			}
			if cfg.discard != nil {
				return cfg.discard.Send(ctx, msg)
			}
			return nil
		})
	}
}
