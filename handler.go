package msgin

import (
	"context"
	"fmt"
)

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

// Step is a composable pipeline stage: it wraps next and returns a handler that
// does its work then (usually) forwards to next. The Go middleware idiom; the
// typed endpoint constructors (Transform/Filter/Activate) return a Step.
type Step = func(next MessageHandler) MessageHandler

// discardHandler is the terminal no-op reached at the end of a Chain that has no
// To/Consume terminal: it CONSUMES the message and returns nil. See Chain's doc —
// a producing flow MUST end in To or Consume, or its final message is discarded.
type discardHandler struct{}

func (discardHandler) Handle(context.Context, Message[any]) error { return nil }

// Chain composes steps into one MessageHandler, running them in order (steps[0]
// first). The innermost next is a no-op consume, so:
//
// CONTRACT: a flow whose last producing step (Transform/Filter-pass/Activate) has
// no downstream terminal will DISCARD its final message silently. Always end a
// producing flow with a terminal — To(sink) to deliver outward, or Consume for a
// side-effect sink. Chain() with no steps is a no-op consume.
//
// A nil ELEMENT is not a panic (no panic on caller input) and is NOT skipped: it
// is replaced in place by a step whose handler fails with Permanent(ErrNilFunc)
// naming its index, so the flow degrades exactly as To(nil) or Activate(nil)
// does. Skipping was rejected — a step built conditionally
// (steps = append(steps, maybeStep())) that came back nil is a WIRING BUG, and
// deleting it would silently change what the flow does: a dropped Filter stops
// filtering, a dropped To discards the message. Steps BEFORE the nil still run;
// the chain stops at it.
func Chain(steps ...Step) MessageHandler {
	next := MessageHandler(discardHandler{})
	for i := len(steps) - 1; i >= 0; i-- {
		s := steps[i]
		if s == nil {
			// D-M (ADR 0029 §5.0b): the nil is fixed at composition and cannot
			// change for the message's lifetime — permanent, not retried.
			position := fmt.Sprintf("msgin.Chain: nil step at index %d", i)
			s = func(MessageHandler) MessageHandler {
				return HandlerFunc(func(context.Context, Message[any]) error {
					return fmt.Errorf("%w: %s", Permanent(ErrNilFunc), position)
				})
			}
		}
		next = s(next)
	}
	return next
}

// To is a terminal Step that sends the message to sink (any OutboundAdapter — a
// *DirectChannel, a *memory.Broker, or a real outbound adapter) and ignores next.
// A nil sink yields ErrNilSink at send time (no panic on caller input), named
// with its position. sink is captured here at construction, so that fault is
// PERMANENT (D-M): the message is routed to the invalid-message channel rather
// than retried to the dead-letter sink, and errors.Is(err, ErrNilSink) still
// matches. See [ErrNilFunc] for the governing invariant.
func To(sink OutboundAdapter) Step {
	return func(MessageHandler) MessageHandler {
		return HandlerFunc(func(ctx context.Context, m Message[any]) error {
			if sink == nil {
				// D-M (ADR 0029 §5.0b): sink is captured by this closure at
				// construction, so the fault cannot change for the message's
				// lifetime — permanent, not retried.
				return fmt.Errorf("%w: msgin.To: nil sink", Permanent(ErrNilSink))
			}
			return sink.Send(ctx, m)
		})
	}
}
