package msgin

import "context"

// Transform is a Message Translator endpoint: it asserts the input payload to A,
// applies fn to produce a Message[B], and forwards it downstream. fn MUST return
// a new message and is responsible for header propagation — use WithPayload
// (keeps id/correlation) rather than bare New. A non-A payload yields
// ErrPayloadType (routed to the invalid-message channel); an fn error propagates
// without forwarding. A nil fn yields ErrNilFunc (no panic on caller input).
func Transform[A, B any](fn func(ctx context.Context, m Message[A]) (Message[B], error)) Step {
	if fn == nil {
		return nilFuncStep()
	}
	return func(next MessageHandler) MessageHandler {
		return HandlerFunc(func(ctx context.Context, msg Message[any]) error {
			in, err := PayloadOf[A](msg)
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
func nilFuncStep() Step {
	return func(MessageHandler) MessageHandler {
		return HandlerFunc(func(context.Context, Message[any]) error { return ErrNilFunc })
	}
}
