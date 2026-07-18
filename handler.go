package msgin

import "context"

// MessageHandler is one processing step in an in-process flow: it consumes a
// message and MAY forward a (possibly transformed) message onward. For a
// DirectChannel it runs synchronously on the caller's goroutine, so an error it
// returns propagates back to the driving Consumer, which owns
// retry/dead-letter/invalid-message. A MessageHandler is structurally a
// Handler[any], so a composed flow drives off NewConsumer[any](src, flow.Handle, …).
type MessageHandler interface {
	Handle(ctx context.Context, msg Message[any]) error
}

// HandlerFunc adapts an ordinary function to a MessageHandler.
type HandlerFunc func(ctx context.Context, msg Message[any]) error

// Handle calls f(ctx, msg).
func (f HandlerFunc) Handle(ctx context.Context, msg Message[any]) error { return f(ctx, msg) }
