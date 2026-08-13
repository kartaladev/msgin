// Package expr provides runtime-expression-backed EIP endpoint constructors
// (predicates, route functions, transformers, correlation/release strategies)
// on top of the msgin core, without forcing an expression engine dependency
// onto every consumer of that core.
package expr

import "errors"

// ErrInvalidExpression is the construction-time fault every provider in this
// package reports: whatever the caller passed cannot be turned into a working
// endpoint. That covers the expression itself — empty, unparseable, or failing
// to type-check against the declared payload type A — AND a provider's other
// construction arguments, which is why the name is broader than "bad
// expression": RouteFunc also returns it for an empty routes map and for a
// route mapped to a nil channel.
//
// The wrapped error always says which of those it was. For the parse and
// type-check failures it QUOTES the offending source text, so a caller can
// locate the bad expression without re-running the provider; the other cases
// have no source text to quote and name the offending argument instead
// ("empty expression", "empty routes map", "nil channel for route %q").
//
// It is NOT an evaluation-time fault. Once a provider has compiled an
// expression successfully, a failure while evaluating it against a specific
// message is a per-message fault, not a construction fault: it surfaces
// through the endpoint's ordinary handler-error path rather than this
// sentinel, and it never comes back as ErrInvalidExpression. In particular, a
// compiled expression that evaluates to a value of the wrong Go type reports
// that mismatch via msgin.ErrPayloadType, not a sentinel from this package —
// see msgin.ErrPayloadType's own godoc for that contract (the "EXPRESSION
// SIDE" producer class). Reusing root's sentinel there — rather than minting
// an expr-side twin — gives every current and future expression provider
// (this one, and any CEL/starlark provider added later) one shared
// errors.Is target and root's existing IsPermanent classification for free.
//
// This sentinel is declared here, in the provider module, rather than in the
// msgin core: after the core's expression-backed endpoints were removed, the
// core has no code path that can produce an invalid-expression fault, so the
// fault belongs to whichever package compiles expressions. ADR 0019's
// fail-at-construction contract governs WHEN this error is raised (at the
// provider call, never at first message); it says nothing about which
// package must declare the sentinel that carries it.
var ErrInvalidExpression = errors.New("msgin/expr: invalid expression")
