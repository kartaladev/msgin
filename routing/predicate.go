package routing

import (
	"context"
	"fmt"

	"github.com/kartaladev/msgin"
)

// Predicate decides whether a message passes a [Filter]. It is the named
// behavior type behind the Message Filter pattern (EIP ch. 7); Spring
// Integration calls the equivalent contract
// org.springframework.integration.core.MessageSelector.
//
// It is fallible: a predicate that reads headers, calls out to a service, or
// evaluates a runtime-defined rule can fail on the message's data, and that
// failure must be distinguishable from a plain false. A predicate error
// propagates from the endpoint rather than reading as "rejected".
//
// The name drops the qualifier the package already carries (ADR 0029 §4): it is
// routing.Predicate, not routing.FilterPredicate.
//
// And, Or and Not compose predicates without a helper package.
//
// ASSIGNABILITY — what naming the type does and does not accept. These call
// shapes all still work at [Filter] and anywhere else a Predicate is taken: a
// bare closure literal (which infers A), a variable or field of the equivalent
// UNNAMED func type, a func returning that unnamed type, a method value, and a
// plain top-level func declaration. The one shape that does NOT work is a
// caller's OWN NAMED func type: Go converts implicitly between two func types
// only when at least one side is unnamed, so `var p MyPred; Filter(p)` is
// rejected. Because Predicate is generic the rejection surfaces as an
// INFERENCE failure — "type MyPred of p does not match routing.Predicate[A]
// (cannot infer A)" — rather than a plain assignability error, and supplying
// explicit type arguments (Filter[int](p)) does not help. Convert at the call
// site instead: Filter(Predicate[int](p)).
type Predicate[A any] func(ctx context.Context, m msgin.Message[A]) (bool, error)

// nilPredicate returns a Predicate that always fails with
// Permanent(ErrNilFunc) — the combinator counterpart of nilFuncStep, for a
// composition whose receiver or argument is nil. Combinators are pure and
// return a Predicate rather than (Predicate, error), so the fault surfaces at
// EVALUATION; there is nowhere for a construction-time error to go.
//
// position names the offending site (e.g. "routing.Predicate.And: nil
// argument"). Without it every one of the five nil sites — And/Or receiver,
// And/Or argument, Not receiver — would collapse into the single string
// "msgin: nil endpoint function", which says nothing about which link of
// p.And(q).Or(r) is mis-wired.
//
// The Permanent wrap is decision D-M (ADR 0029 §5.0b, Spec 014 §2.1 row 6): the
// nil operand is captured when the combinator runs, so the fault cannot change
// for the message's lifetime and retrying it can never succeed.
// errors.Is(err, msgin.ErrNilFunc) still matches through both wraps.
func nilPredicate[A any](position string) Predicate[A] {
	return func(context.Context, msgin.Message[A]) (bool, error) {
		return false, fmt.Errorf("%w: %s", msgin.Permanent(msgin.ErrNilFunc), position)
	}
}

// And returns a Predicate that is true when p and q are both true, evaluating q
// only if p returned true. An error from EITHER side propagates immediately and
// the result is (false, err), never (true, err) — an operand that failed has no
// truth value to conjoin, so whatever bool it returned alongside its error is
// discarded rather than passed through. A caller that reads the bool before
// checking the error therefore cannot see a truthy decision from an evaluation
// that never completed.
//
// A nil receiver or a nil argument does not panic: the returned Predicate fails
// with ErrNilFunc naming its position — PERMANENT (D-M), so it is routed to the
// invalid-message channel rather than retried to the dead-letter sink.
// errors.Is(err, msgin.ErrNilFunc) still matches and msgin.IsPermanent(err) is
// true (see [msgin.ErrNilFunc], [msgin.Permanent]). THE NIL CHECK PRECEDES THE
// SHORT-CIRCUIT: p.And(nil) reports the nil even when p is false and a
// short-circuit would have hidden it — a nil operand is a programming error and
// must surface.
func (p Predicate[A]) And(q Predicate[A]) Predicate[A] {
	if p == nil {
		return nilPredicate[A]("routing.Predicate.And: nil receiver")
	}
	if q == nil {
		return nilPredicate[A]("routing.Predicate.And: nil argument")
	}
	return func(ctx context.Context, m msgin.Message[A]) (bool, error) {
		ok, err := p(ctx, m)
		if err != nil {
			return false, err
		}
		if !ok {
			return false, nil
		}
		ok, err = q(ctx, m)
		if err != nil {
			return false, err
		}
		return ok, nil
	}
}

// Or returns a Predicate that is true when either p or q is true, evaluating q
// only if p returned false. An error from EITHER side propagates immediately
// and the result is (false, err), never (true, err) — an operand that failed
// has no truth value to disjoin, so whatever bool it returned alongside its
// error is discarded rather than passed through. A caller that reads the bool
// before checking the error therefore cannot see a truthy decision from an
// evaluation that never completed.
//
// A nil receiver or a nil argument does not panic: the returned Predicate fails
// with ErrNilFunc naming its position — PERMANENT (D-M), so it is routed to the
// invalid-message channel rather than retried to the dead-letter sink.
// errors.Is(err, msgin.ErrNilFunc) still matches and msgin.IsPermanent(err) is
// true (see [msgin.ErrNilFunc], [msgin.Permanent]). THE NIL CHECK PRECEDES THE
// SHORT-CIRCUIT: p.Or(nil) reports the nil even when p is true and a
// short-circuit would have returned (true, nil) — a nil operand is a
// programming error and must surface.
func (p Predicate[A]) Or(q Predicate[A]) Predicate[A] {
	if p == nil {
		return nilPredicate[A]("routing.Predicate.Or: nil receiver")
	}
	if q == nil {
		return nilPredicate[A]("routing.Predicate.Or: nil argument")
	}
	return func(ctx context.Context, m msgin.Message[A]) (bool, error) {
		ok, err := p(ctx, m)
		if err != nil {
			return false, err
		}
		if ok {
			return true, nil
		}
		ok, err = q(ctx, m)
		if err != nil {
			return false, err
		}
		return ok, nil
	}
}

// Not returns a Predicate that inverts p. An error from p PROPAGATES rather
// than being inverted — an errored decision has no truth value to negate, so
// the result is (false, err), never (true, err).
//
// A nil receiver does not panic: the returned Predicate fails with ErrNilFunc
// naming its position — PERMANENT (D-M), so it is routed to the invalid-message
// channel rather than retried to the dead-letter sink.
// errors.Is(err, msgin.ErrNilFunc) still matches and msgin.IsPermanent(err) is
// true (see [msgin.ErrNilFunc], [msgin.Permanent]). The nil check precedes
// evaluation, so no result path can skip it.
func (p Predicate[A]) Not() Predicate[A] {
	if p == nil {
		return nilPredicate[A]("routing.Predicate.Not: nil receiver")
	}
	return func(ctx context.Context, m msgin.Message[A]) (bool, error) {
		ok, err := p(ctx, m)
		if err != nil {
			return false, err
		}
		return !ok, nil
	}
}
