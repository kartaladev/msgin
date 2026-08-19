package routing

import (
	"context"
	"fmt"

	"github.com/kartaladev/msgin"
)

// boxMessage re-boxes a typed message as Message[any], preserving its headers
// (and therefore its id and timestamp — it is NOT msgin.New, which would stamp
// fresh ones). Inlined per Spec 014 §3.3: the body is one call to public API,
// so each endpoint package carries its own copy rather than the core exporting
// a helper.
func boxMessage[T any](m msgin.Message[T]) msgin.Message[any] {
	return msgin.NewMessage[any](m.Payload(), m.Headers())
}

// nilFuncStep returns a Step whose handler always fails with
// Permanent(ErrNilFunc) — the shared "constructed with a nil function"
// degradation. Inlined per Spec 014 §3.3; the body is a few lines over the
// exported Step/HandlerFunc/ErrNilFunc/Permanent.
//
// position names the CALLER (e.g. "routing.Filter: nil pred"), because this
// helper is shared by several public constructors and a single
// "msgin: nil endpoint function" string across all of them would not tell a
// caller which endpoint is mis-wired.
//
// The wrap is decision D-M (ADR 0029 §5.0b, Spec 014 §2.1 row 6): the nil is
// captured at construction, so the fault cannot change for the message's
// lifetime and retrying it can never succeed. errors.Is(err, msgin.ErrNilFunc)
// still matches through both wraps.
func nilFuncStep(position string) msgin.Step {
	return func(msgin.MessageHandler) msgin.MessageHandler {
		return msgin.HandlerFunc(func(context.Context, msgin.Message[any]) error {
			return fmt.Errorf("%w: %s", msgin.Permanent(msgin.ErrNilFunc), position)
		})
	}
}

// nilFuncAt is the CONSTRUCTION-time counterpart of nilFuncStep: an ErrNilFunc
// naming which of a constructor's several function arguments was nil, returned
// straight to the caller. position names the site exactly as nilFuncStep's does
// (e.g. "routing.NewAggregator: nil release strategy").
//
// It is deliberately NOT wrapped in msgin.Permanent, unlike nilFuncStep. That
// is the constructor arm of the invariant on [msgin.ErrNilFunc]: the error is
// handed back here and never carried through a handler, so it never reaches a
// RetryPolicy and a retry classification would be meaningless on it. The
// exclusion is a decision, not an omission — do not "finish the job" by
// wrapping it. errors.Is(err, msgin.ErrNilFunc) matches either way.
func nilFuncAt(position string) error {
	return fmt.Errorf("%w: %s", msgin.ErrNilFunc, position)
}

// nilOptionAt reports a nil ELEMENT of a constructor's variadic option slice,
// naming the constructor the CALLER invoked and the element's 0-based index.
//
// Deliberately NOT wrapped in msgin.Permanent — see [msgin.ErrNilFunc]'s
// constructor arm, and the same warning on nilFuncAt.
func nilOptionAt(ctor string, i int) error {
	return fmt.Errorf("%w: %s: nil option at index %d", msgin.ErrNilFunc, ctor, i)
}
