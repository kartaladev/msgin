package routing

import (
	"context"
	"fmt"

	"github.com/kartaladev/msgin"
)

type filterConfig struct{ discard msgin.MessageChannel }

// FilterOption configures a Filter endpoint.
type FilterOption func(*filterConfig)

// WithDiscardChannel routes messages a Filter rejects (predicate false) to ch
// instead of silently dropping them (the default). The default — silent drop —
// matches the pattern's intent (a filter's job is to drop); set this when you
// need to audit or dead-letter filtered-out messages.
func WithDiscardChannel(ch msgin.MessageChannel) FilterOption {
	return func(c *filterConfig) { c.discard = ch }
}

// Filter is a Message Filter endpoint: it asserts the payload to A, evaluates
// pred, and forwards downstream when true. When false the message is dropped —
// silently by default, or sent to WithDiscardChannel if set. A predicate error
// (or a discard-channel send error) propagates; a non-A payload yields
// ErrPayloadType; a nil pred yields ErrNilFunc naming its position — PERMANENT
// (D-M): routed to the invalid-message channel rather than retried to the
// dead-letter sink, with errors.Is(err, msgin.ErrNilFunc) still matching (see
// [msgin.ErrNilFunc]).
//
// A nil ELEMENT of opts degrades the Step the same way — a PERMANENT ErrNilFunc
// naming the element's 0-based index ("routing.Filter: nil option at index 1"),
// reported by the Step's handler at dispatch, since Filter has no error return
// of its own. It is checked BEFORE the nil-pred check above (ADR 0031 D-V: the
// option fault happened at construction, so it is chronologically earlier and
// is reported uniformly first across this family), so Filter[A](nil, nil)
// reports the nil option; Filter[A](nil) with no options still reports
// "nil pred".
func Filter[A any](pred Predicate[A], opts ...FilterOption) msgin.Step {
	var cfg filterConfig
	for i, opt := range opts {
		if opt == nil {
			return nilFuncStep(fmt.Sprintf("routing.Filter: nil option at index %d", i))
		}
		opt(&cfg)
	}
	if pred == nil {
		return nilFuncStep("routing.Filter: nil pred")
	}
	return func(next msgin.MessageHandler) msgin.MessageHandler {
		return msgin.HandlerFunc(func(ctx context.Context, msg msgin.Message[any]) error {
			in, err := msgin.PayloadOf[A](msg)
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
