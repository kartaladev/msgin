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
// NewSSEClient, NewSSEParser) that each call NewConfig(opts...) as their first
// statement. Each delegator therefore runs its own pre-check and passes its own
// name; without it every one of them would report "msghttp.NewConfig: nil option
// at index 0" for a function the caller never invoked (Spec 015 §3.4).
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
