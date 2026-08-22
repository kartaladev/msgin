package memory

import (
	"fmt"

	"github.com/kartaladev/msgin"
)

// nilOptionAt reports a nil ELEMENT of a constructor's variadic option slice,
// naming the constructor the CALLER invoked and the element's 0-based index.
//
// Deliberately NOT wrapped in msgin.Permanent — see [msgin.ErrNilFunc]'s
// constructor arm: the error is handed back to the caller at construction and
// never carried through a MessageHandler, so it never reaches a RetryPolicy
// and a retry classification would be meaningless on it. The exclusion is a
// decision, not an omission — do not "finish the job" by wrapping it.
// errors.Is(err, msgin.ErrNilFunc) matches either way.
//
// This mirrors endpoint.nilOptionAt, routing.nilOptionAt and
// resilience.nilOptionAt rather than sharing one of them: the body is two
// lines over exported API, and exporting an internal detail from root to
// spare a ninth package a duplicate was rejected (ADR 0031 D-R, on the
// Spec 014 §3.3 precedent).
func nilOptionAt(ctor string, i int) error {
	return fmt.Errorf("%w: %s: nil option at index %d", msgin.ErrNilFunc, ctor, i)
}

// checkRange reports a sizing option whose value falls outside [lo, hi],
// returning nil when it is in range. site names the OPTION the caller invoked
// (e.g. "memory.WithCapacity"), not the constructor that validated it.
//
// It renders Spec 016 §3.1's single shape, "%w: %s: %d not in [%d, %d]", true
// at BOTH ends — revision 2's "%d exceeds %d" lied on the lower arm, where 0
// exceeds nothing. Do not "improve" it back.
//
// The helper exists so the ENFORCED range and the PRINTED range are the same
// two values; written inline, each site spelled each bound twice and the two
// spellings had already drifted (this package had "cfg.capacity <= 0" guards
// printing a lower bound of 1).
//
// This package is the one with THREE callers and they are not uniform, which is
// exactly why the range must travel as data:
//   - NewQueueStore and NewGroupStore pass lo=1 with a BARE sentinel (R1).
//   - WithBuffer passes lo=0 — 0 is a legal unbuffered channel — with an
//     ALREADY-WRAPPED msgin.Permanent(...) sentinel (R2, Spec 016 §3.2).
//
// Keeping the sentinel a parameter is what lets that R1/R2 difference stay
// visible at the call site (ADR 0029 D-M's constructor arm) instead of becoming
// a flag in here.
//
// This mirrors endpoint.checkRange, routing.checkRange and msghttp.checkRange
// rather than sharing one of them — the same ADR 0031 D-R / Spec 014 §3.3
// precedent that governs nilOptionAt above.
func checkRange(sentinel error, site string, n, lo, hi int) error {
	if n >= lo && n <= hi {
		return nil
	}
	return fmt.Errorf("%w: %s: %d not in [%d, %d]", sentinel, site, n, lo, hi)
}
