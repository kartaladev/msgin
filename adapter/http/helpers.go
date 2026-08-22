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
// two values; written inline, each of this package's three sites spelled each
// bound twice.
//
// The sentinel is a PARAMETER because each knob keeps its own errors.Is target
// (ADR 0032 D-X) — here they are msghttp's own ErrInvalidMaxConnections,
// ErrInvalidConnectionBuffer and ErrInvalidReplayBuffer rather than root's.
// All three sites are R1, so all three pass a BARE sentinel; there is no
// msgin.Permanent wrap on a constructor return (ADR 0029 D-M).
//
// This mirrors endpoint.checkRange, routing.checkRange and memory.checkRange
// rather than sharing one of them — the same ADR 0031 D-R / Spec 014 §3.3
// precedent that governs nilOptionAt above. adapter/http/stdlib would get its
// OWN copy for the same reason, if it ever grew a sizing option.
func checkRange(sentinel error, site string, n, lo, hi int) error {
	if n >= lo && n <= hi {
		return nil
	}
	return fmt.Errorf("%w: %s: %d not in [%d, %d]", sentinel, site, n, lo, hi)
}
