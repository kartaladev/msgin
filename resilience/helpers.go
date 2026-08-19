package resilience

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
// This mirrors endpoint.nilOptionAt and routing.nilOptionAt rather than
// sharing one exported helper: the body is two lines over exported API, and
// exporting an internal detail from root to spare eight packages a duplicate
// was rejected (ADR 0031 D-R, on the Spec 014 §3.3 precedent).
func nilOptionAt(ctor string, i int) error {
	return fmt.Errorf("%w: %s: nil option at index %d", msgin.ErrNilFunc, ctor, i)
}
