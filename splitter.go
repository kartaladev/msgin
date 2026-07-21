package msgin

import (
	"context"
	"strconv"
)

// Split is a Splitter endpoint (EIP): it asserts the input payload to A, calls
// fn to produce N child messages, and forwards each downstream IN ORDER. A
// non-A payload yields ErrPayloadType (routed to the invalid-message channel);
// an fn error propagates without forwarding; a nil fn yields ErrNilFunc (no
// panic on caller input). An empty/nil result forwards nothing and returns nil
// (a valid "nothing to split", like a Filter drop).
//
// Each child is stamped for reassembly by a downstream Aggregator:
// HeaderSequenceNumber (1-based) and HeaderSequenceSize (N); a deterministic
// child id (HeaderMessageID = parentID#seq — unique within the split yet stable across
// a redelivery of the same parent, so the Aggregator's id-dedup holds); and
// HeaderCorrelationID set to the parent's id UNLESS the child already carries a
// correlation id (a caller-set/inherited one is preserved). With these, a
// Splitter->Aggregator round-trip reassembles with no extra configuration.
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
			return forwardSplit(ctx, next, msg, children)
		})
	}
}

// forwardSplit stamps each child for reassembly (see stampSequence) and forwards
// it to next IN ORDER, aborting on the first error (remaining children not sent).
// An empty children slice forwards nothing and returns nil. Shared by Split and
// SplitExpr.
func forwardSplit[B any](ctx context.Context, next MessageHandler, parent Message[any], children []Message[B]) error {
	n := len(children)
	for i, child := range children {
		stamped := stampSequence(child, parent, i+1, n)
		if err := next.Handle(ctx, boxMessage(stamped)); err != nil {
			return err
		}
	}
	return nil
}

// stampSequence returns child stamped for reassembly by a downstream Aggregator:
//   - HeaderSequenceNumber (1-based num) and HeaderSequenceSize (total).
//   - A deterministic child HeaderMessageID = parentID#num (only when the parent has an
//     id). It is unique within one split (so the group fills to size) AND stable
//     across a redelivery of the same parent (so the Aggregator's id-dedup
//     upholds at-least-once). This overwrites the id WithPayload copied from the
//     parent — children built via WithPayload would otherwise all share it.
//   - HeaderCorrelationID = the parent's id, but ONLY if the child carries no
//     correlation id (a caller-set / inherited correlation is preserved, so
//     nested split/aggregate keeps its outer group key).
//
// With an id-less parent (ID()==""), no id/correlation is derived — sequence
// headers are still stamped; such a split is not redelivery-idempotent (rare:
// source-delivered messages carry an id).
func stampSequence[B any](child Message[B], parent Message[any], num, size int) Message[B] {
	out := child.WithHeader(HeaderSequenceNumber, num).WithHeader(HeaderSequenceSize, size)
	pid := parent.ID()
	if pid != "" {
		out = out.WithHeader(HeaderMessageID, pid+"#"+strconv.Itoa(num))
		if _, ok := out.Header(HeaderCorrelationID); !ok {
			out = out.WithHeader(HeaderCorrelationID, pid)
		}
	}
	return out
}
