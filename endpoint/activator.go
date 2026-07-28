package endpoint

import (
	"context"

	"github.com/kartaladev/msgin"
)

// Activate is a request-reply Service Activator: the boundary where a flow
// invokes your domain service. It asserts the payload to A, calls svc, and
// forwards svc's reply (Message[B]) downstream. Use WithPayload in svc to keep
// the id/correlation headers. A non-A payload yields ErrPayloadType; an svc
// error propagates without forwarding; a nil svc yields ErrNilFunc. For a
// one-way service with no reply, use Consume.
func Activate[A, B any](svc func(ctx context.Context, m msgin.Message[A]) (msgin.Message[B], error)) msgin.Step {
	if svc == nil {
		return nilFuncStep()
	}
	return func(next msgin.MessageHandler) msgin.MessageHandler {
		return msgin.HandlerFunc(func(ctx context.Context, msg msgin.Message[any]) error {
			in, err := msgin.PayloadOf[A](msg)
			if err != nil {
				return err
			}
			reply, err := svc(ctx, in)
			if err != nil {
				return err
			}
			return next.Handle(ctx, boxMessage(reply))
		})
	}
}

// Consume is a one-way Service Activator: it asserts the payload to A and calls
// svc for its side effect, forwarding nothing (a terminal step — next never
// runs). A non-A payload yields ErrPayloadType; an svc error propagates; a nil
// svc yields ErrNilFunc.
func Consume[A any](svc func(ctx context.Context, m msgin.Message[A]) error) msgin.Step {
	if svc == nil {
		return nilFuncStep()
	}
	return func(msgin.MessageHandler) msgin.MessageHandler {
		return msgin.HandlerFunc(func(ctx context.Context, msg msgin.Message[any]) error {
			in, err := msgin.PayloadOf[A](msg)
			if err != nil {
				return err
			}
			return svc(ctx, in)
		})
	}
}
