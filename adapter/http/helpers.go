package msghttp

import (
	"fmt"

	msgin "github.com/kartaladev/msgin"
)

// nilOptionAt reports a nil ELEMENT of a constructor's variadic option slice,
// naming the constructor the CALLER invoked and the element's 0-based index.
//
// The "constructor the caller invoked" clause carries more weight in this
// package than anywhere else in Plan 028: msghttp has ONE Option type
// (options.go) and SIX entry points over it — NewConfig, which actually applies
// the options, plus five delegators (NewExchange, NewOutbound, NewSSEServer,
// NewSSEClient, NewSSEParser) that each run their OWN nil pre-check first, then
// forward to NewConfig(opts...). That standalone pre-check — not the delegation
// — is what lets each delegator pass its own name; without it every one of them
// would report "msghttp.NewConfig: nil option at index 0" for a function the
// caller never invoked (Spec 015 §3.4).
//
// Deliberately NOT wrapped in msgin.Permanent — see [msgin.ErrNilFunc]'s
// constructor arm: the error is handed back to the caller at construction and
// never carried through a MessageHandler, so it never reaches a RetryPolicy
// and a retry classification would be meaningless on it. The exclusion is a
// decision, not an omission — do not "finish the job" by wrapping it.
// errors.Is(err, msgin.ErrNilFunc) matches either way.
//
// This mirrors endpoint.nilOptionAt, routing.nilOptionAt, resilience.nilOptionAt,
// memory.nilOptionAt, cron.nilOptionAt and sql.nilOptionAt rather than sharing
// one of them: the body is two lines over exported API, and exporting an
// internal detail from root to spare a seventh package a duplicate was rejected
// (ADR 0031 D-R, on the Spec 014 §3.3 precedent). adapter/http/stdlib gets its
// OWN copy for the same reason — it cannot reach this unexported one.
func nilOptionAt(ctor string, i int) error {
	return fmt.Errorf("%w: %s: nil option at index %d", msgin.ErrNilFunc, ctor, i)
}

// checkRange reports a sizing option whose value falls outside [lo, hi],
// returning nil when it is in range. site names the OPTION the caller invoked
// (e.g. "msghttp.WithMaxConnections"), not NewConfig — which matters more here
// than anywhere else for the same reason nilOptionAt's ctor does: six entry
// points funnel into NewConfig, so naming the validator would name a function
// the caller never called.
//
// It renders Spec 016 §3.1's single shape, "%w: %s: %d not in [%d, %d]", true
// at BOTH ends — revision 2's "%d exceeds %d" lied on the lower arm, where 0
// exceeds nothing. Do not "improve" it back.
//
// The helper exists so the ENFORCED range and the PRINTED range are the same
// two values; written inline, each of the three INT-TYPED sites this helper
// serves spelled each bound twice.
//
// The sentinel is a PARAMETER because each knob keeps its own errors.Is target
// (ADR 0032 D-X) — here they are msghttp's own ErrInvalidMaxConnections,
// ErrInvalidConnectionBuffer and ErrInvalidReplayBuffer rather than root's.
// All three of THOSE sites are R1, so all three pass a BARE sentinel; there is
// no msgin.Permanent wrap on a constructor return (ADR 0029 D-M).
//
// SCOPE — read the counts above as "the three int-typed sites this helper
// serves", never as a package total. Since Spec 018 this package range-checks
// SIX sizing options across TWO helpers: the three int-typed ones here, and
// the three int64-typed byte caps served by checkRangeInt64 immediately below.
// Reach for checkRangeInt64 whenever the option's parameter is an int64 —
// converting to call this one would truncate on a 32-bit GOARCH, which is the
// whole reason the sibling exists (ADR 0034 D-AP).
//
// This mirrors endpoint.checkRange, routing.checkRange and memory.checkRange
// rather than sharing one of them — the same ADR 0031 D-R / Spec 014 §3.3
// precedent that governs nilOptionAt above, and the same precedent that makes
// checkRangeInt64 a SIBLING rather than a generic. adapter/http/stdlib would
// get its OWN copy for the same reason, if it ever grew a sizing option.
func checkRange(sentinel error, site string, n, lo, hi int) error {
	if n >= lo && n <= hi {
		return nil
	}
	return fmt.Errorf("%w: %s: %d not in [%d, %d]", sentinel, site, n, lo, hi)
}

// checkRangeInt64 is checkRange's int64 sibling: it reports a sizing option
// whose value falls outside [lo, hi], returning nil when it is in range, and
// renders the IDENTICAL Spec 016 §3.1 shape — "%w: %s: %d not in [%d, %d]",
// true at both ends. site names the OPTION the caller invoked (e.g.
// "msghttp.WithMaxBodyBytes"), for the same reason checkRange's does.
//
// It serves the three int64-typed byte caps — WithMaxBodyBytes,
// WithMaxResponseBytes and WithMaxEventBytes — each bounded at
// byteCapCeiling (Spec 018 / ADR 0034 D-AP). All three are R1, so all three
// pass a BARE sentinel; no msgin.Permanent wrap on a constructor return
// (ADR 0029 D-M).
//
// WHY A SIBLING AND NOT A CONVERSION (D-AP(b)). Calling checkRange with
// int(n) would TRUNCATE on a 32-bit GOARCH: int is 32 bits there, so
// int(1<<62) is not 1<<62 and an absurd cap would be silently ACCEPTED —
// exactly the defect this ceiling exists to close, reintroduced by the
// conversion meant to avoid seven duplicated lines. On darwin/arm64 the
// truncation is lossless and every test still passes, so the bug would not be
// caught behaviorally; do not "simplify" this into checkRange(…, int(n), …).
//
// WHY NOT A GENERIC over ~int | ~int64 (D-AP(b)). A generic would collapse the
// pair, but this package already duplicates nilOptionAt and checkRange from
// six peer packages rather than exporting them from root — ADR 0031 D-R, on
// the Spec 014 §3.3 precedent. Per-package duplication of a four-line helper
// is this project's settled answer; a generic here would be a different answer
// for the same question, seven lines apart.
//
// WHY THE OPTIONS WERE NOT NARROWED TO int INSTEAD (D-AP(a)). Narrowing
// WithMaxBodyBytes and friends to (n int) would delete this helper outright —
// and it was tried and rejected. At byteCapCeiling = math.MaxInt32 the ceiling
// on a 32-bit build EQUALS math.MaxInt, so no int literal could exceed it: the
// above-ceiling test case becomes inexpressible, GOARCH=386 go vet goes red on
// the 2147483648 literal, and the root class gate's three moved rows would
// have to become architecture-conditional. The int64 signature and this
// sibling are one decision, not two.
func checkRangeInt64(sentinel error, site string, n, lo, hi int64) error {
	if n >= lo && n <= hi {
		return nil
	}
	return fmt.Errorf("%w: %s: %d not in [%d, %d]", sentinel, site, n, lo, hi)
}
