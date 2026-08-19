package stdlib

import (
	"fmt"

	msgin "github.com/kartaladev/msgin"
)

// nilOptionAt reports a nil ELEMENT of a constructor's variadic option slice,
// naming the constructor the CALLER invoked and the element's 0-based index.
//
// stdlib forwards opts to msghttp.NewConfig (adapter/http), which guards its
// own apply loop and would otherwise report "msghttp.NewConfig: nil option at
// index N" — a function the caller never called. Both entry points here
// (NewInbound, NewInboundGateway) therefore run a standalone pre-check above
// the delegation and pass their OWN name (Spec 015 §3.4, ADR 0031 D-R).
//
// Deliberately NOT wrapped in msgin.Permanent — see [msgin.ErrNilFunc]'s
// constructor arm: the error is handed back to the caller at construction and
// never carried through a MessageHandler, so it never reaches a RetryPolicy
// and a retry classification would be meaningless on it. The exclusion is a
// decision, not an omission — do not "finish the job" by wrapping it.
// errors.Is(err, msgin.ErrNilFunc) matches either way.
//
// This mirrors msghttp.nilOptionAt (and endpoint/routing/resilience/memory/
// cron/sql's copies) rather than sharing it: stdlib cannot reach msghttp's
// unexported helper, and the body is two lines over exported API — exporting
// an internal detail from msghttp to spare this package a duplicate was
// rejected (ADR 0031 D-R, on the Spec 014 §3.3 precedent).
func nilOptionAt(ctor string, i int) error {
	return fmt.Errorf("%w: %s: nil option at index %d", msgin.ErrNilFunc, ctor, i)
}
