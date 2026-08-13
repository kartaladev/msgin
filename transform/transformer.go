package transform

import (
	"context"
	"fmt"

	"github.com/kartaladev/msgin"
)

// Transformer maps a message of one payload type to another. It is the named
// behavior type behind the Message Translator pattern (EIP ch. 8); Spring
// Integration calls the equivalent contract
// org.springframework.integration.transformer.Transformer (its typed form,
// GenericTransformer<S, T>, is the closer analogue).
//
// It is fallible: a translation that parses, validates or enriches from an
// external source can fail on the message's data, and an error propagates
// without forwarding anything.
//
// An implementation MUST return a NEW message and owns header propagation — use
// [msgin.WithPayload] (which keeps id and correlation) rather than a bare
// msgin.New, which would stamp a fresh id and break correlation downstream.
//
// The name drops the qualifier the package already carries (ADR 0029 §4) and has
// direct precedent in golang.org/x/text/transform.
//
// ASSIGNABILITY — what naming the type does and does not accept. These call
// shapes all still work at [Transform]: a bare closure literal (which infers A
// and B), a variable or field of the equivalent UNNAMED func type, a func
// returning that unnamed type, a method value, and a plain top-level func
// declaration. The one shape that does NOT work is a caller's OWN NAMED func
// type: Go converts implicitly between two func types only when at least one
// side is unnamed, so `var x MyXform; Transform(x)` is rejected. Because
// Transformer is generic the rejection surfaces as an INFERENCE failure —
// "type MyXform of x does not match transform.Transformer[A, B] (cannot infer A
// and B)" — rather than a plain assignability error, and supplying explicit type
// arguments does not help. Convert at the call site:
// Transform(Transformer[A, B](x)).
type Transformer[A, B any] func(ctx context.Context, m msgin.Message[A]) (msgin.Message[B], error)

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
func Transform[A, B any](fn Transformer[A, B]) msgin.Step {
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
