package transform

import (
	"context"

	"github.com/kartaladev/msgin"
)

// Transform is a Message Translator endpoint: it asserts the input payload to A,
// applies fn to produce a Message[B], and forwards it downstream. fn MUST return
// a new message and is responsible for header propagation — use WithPayload
// (keeps id/correlation) rather than bare New. A non-A payload yields
// ErrPayloadType (routed to the invalid-message channel); an fn error propagates
// without forwarding. A nil fn yields ErrNilFunc (no panic on caller input).
func Transform[A, B any](fn func(ctx context.Context, m msgin.Message[A]) (msgin.Message[B], error)) msgin.Step {
	if fn == nil {
		return nilFuncStep()
	}
	return func(next msgin.MessageHandler) msgin.MessageHandler {
		return msgin.HandlerFunc(func(ctx context.Context, msg msgin.Message[any]) error {
			in, err := msgin.PayloadOf[A](msg)
			if err != nil {
				return err
			}
			out, err := fn(ctx, in)
			if err != nil {
				return err
			}
			return next.Handle(ctx, boxMessage(out))
		})
	}
}

// nilFuncStep is the Step returned by an endpoint constructor given a nil
// function: its handler returns ErrNilFunc instead of panicking on a nil call.
func nilFuncStep() msgin.Step {
	return func(msgin.MessageHandler) msgin.MessageHandler {
		return msgin.HandlerFunc(func(context.Context, msgin.Message[any]) error { return msgin.ErrNilFunc })
	}
}

// boxMessage re-boxes a typed message as Message[any], preserving its headers
// (and therefore its id and timestamp — it is NOT msgin.New, which would stamp
// fresh ones). Inlined per Spec 014 §3.3: the body is one call to public API,
// so each endpoint package carries its own copy rather than the core exporting
// a helper.
func boxMessage[T any](m msgin.Message[T]) msgin.Message[any] {
	return msgin.NewMessage[any](m.Payload(), m.Headers())
}
