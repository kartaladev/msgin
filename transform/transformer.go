package transform

import (
	"context"
	"fmt"

	"github.com/kartaladev/msgin"
)

// Transform is a Message Translator endpoint: it asserts the input payload to A,
// applies fn to produce a Message[B], and forwards it downstream. fn MUST return
// a new message and is responsible for header propagation — use WithPayload
// (keeps id/correlation) rather than bare New. A non-A payload yields
// ErrPayloadType (routed to the invalid-message channel, or the dead-letter sink
// when none is configured — see [msgin.Permanent]); an fn error propagates
// without forwarding. A nil fn yields ErrNilFunc naming its position (no panic
// on caller input) — PERMANENT (D-M): routed to the invalid-message channel
// rather than retried to the dead-letter sink, with
// errors.Is(err, msgin.ErrNilFunc) still matching (see [msgin.ErrNilFunc]).
func Transform[A, B any](fn func(ctx context.Context, m msgin.Message[A]) (msgin.Message[B], error)) msgin.Step {
	if fn == nil {
		return nilFuncStep("transform.Transform: nil fn")
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
// function: its handler returns Permanent(ErrNilFunc) instead of panicking on a
// nil call.
//
// position names the CALLER (e.g. "transform.Transform: nil fn"), because this
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

// boxMessage re-boxes a typed message as Message[any], preserving its headers
// (and therefore its id and timestamp — it is NOT msgin.New, which would stamp
// fresh ones). Inlined per Spec 014 §3.3: the body is one call to public API,
// so each endpoint package carries its own copy rather than the core exporting
// a helper.
func boxMessage[T any](m msgin.Message[T]) msgin.Message[any] {
	return msgin.NewMessage[any](m.Payload(), m.Headers())
}
