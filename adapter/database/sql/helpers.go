package sql

import (
	"fmt"

	msgin "github.com/kartaladev/msgin"
)

// nilOptionAt reports a nil ELEMENT of a constructor's variadic option slice,
// naming the constructor the CALLER invoked and the element's 0-based index.
//
// The "constructor the caller invoked" clause is load-bearing here: this is the
// package that owns Plan 028's first delegator. NewQueueStore forwards opts to
// both NewOutboundAdapter and NewPollingSource, so it runs its own pre-check and
// passes its own name — without it the caller would be told
// "sql.NewOutboundAdapter: nil option at index 0" for a function they never
// called (Spec 015 §3.4).
//
// Deliberately NOT wrapped in msgin.Permanent — see [msgin.ErrNilFunc]'s
// constructor arm: the error is handed back to the caller at construction and
// never carried through a MessageHandler, so it never reaches a RetryPolicy
// and a retry classification would be meaningless on it. The exclusion is a
// decision, not an omission — do not "finish the job" by wrapping it.
// errors.Is(err, msgin.ErrNilFunc) matches either way.
//
// This mirrors endpoint.nilOptionAt, routing.nilOptionAt, resilience.nilOptionAt,
// memory.nilOptionAt and cron.nilOptionAt rather than sharing one of them: the
// body is two lines over exported API, and exporting an internal detail from
// root to spare a sixth package a duplicate was rejected (ADR 0031 D-R, on the
// Spec 014 §3.3 precedent).
func nilOptionAt(ctor string, i int) error {
	return fmt.Errorf("%w: %s: nil option at index %d", msgin.ErrNilFunc, ctor, i)
}

// checkRange reports a sizing option whose value falls outside [lo, hi],
// returning nil when it is in range. site names the OPTION the caller invoked
// (e.g. "sql.WithMaxGroupMembers"), not the constructor that validated it.
//
// It renders Spec 016 §3.1's single shape, "%w: %s: %d not in [%d, %d]", true
// at BOTH ends — a "%d exceeds %d" form lies on the lower arm, where 0 exceeds
// nothing. Do not "improve" it back.
//
// The helper exists so the ENFORCED range and the PRINTED range are the same
// two values; written inline, each site spells each bound twice and the two
// spellings drift.
//
// This is the FIFTH independent int copy — it mirrors endpoint.checkRange,
// routing.checkRange, memory.checkRange and msghttp.checkRange rather than
// sharing one of them, on the same ADR 0031 D-R / Spec 014 §3.3 precedent that
// governs nilOptionAt above: the body is three lines over exported API, and
// exporting an internal detail from root to spare a fifth package a duplicate
// was rejected. (msghttp additionally carries an int64 twin, checkRangeInt64,
// for Spec 018's byte caps; this package needs only the int form.)
func checkRange(sentinel error, site string, n, lo, hi int) error {
	if n >= lo && n <= hi {
		return nil
	}
	return fmt.Errorf("%w: %s: %d not in [%d, %d]", sentinel, site, n, lo, hi)
}
