package msgin

import "context"

// Split is a Splitter endpoint (EIP): it asserts the input payload to A, calls
// fn to produce N child messages, and forwards each downstream IN ORDER. A
// non-A payload yields ErrPayloadType (routed to the invalid-message channel);
// an fn error propagates without forwarding; a nil fn yields ErrNilFunc (no
// panic on caller input). An empty/nil result forwards nothing and returns nil
// (a valid "nothing to split", like a Filter drop).
//
// (Reassembly headers — sequence number/size, a deterministic child id, and a
// correlation-id fallback — are stamped on each child in the next commit.)
//
// Settlement: all N children forward on the delivery goroutine before Handle
// returns, so a Consumer driving the flow Acks the source only after every
// child succeeds (end-to-end at-least-once, exactly like Chain). A child error
// aborts the remaining children and propagates, so the whole parent is
// redelivered — children must be idempotent downstream.
func Split[A, B any](fn func(ctx context.Context, m Message[A]) ([]Message[B], error)) Step {
	if fn == nil {
		return nilFuncStep()
	}
	return func(next MessageHandler) MessageHandler {
		return HandlerFunc(func(ctx context.Context, msg Message[any]) error {
			in, err := PayloadOf[A](msg)
			if err != nil {
				return err
			}
			children, err := fn(ctx, in)
			if err != nil {
				return err
			}
			for _, child := range children {
				if err := next.Handle(ctx, boxMessage(child)); err != nil {
					return err
				}
			}
			return nil
		})
	}
}
