package cron

import "errors"

var (
	// ErrInvalidSchedule is returned by NewSource when spec cannot be parsed as a
	// 5-field cron expression, an "@every <duration>" interval, or a descriptor
	// (@daily/@hourly/@weekly/@monthly/@yearly/@midnight), OR when spec parses but
	// has NO future occurrence (e.g. "0 0 30 2 *" — Feb 30 never happens; robfig's
	// Schedule.Next returns the zero time after a 5-year search). Construction
	// probes satisfiability, not just parseability, so an unsatisfiable schedule
	// never reaches the firing loop (Round-1 audit B-2 — an unguarded zero Next
	// would hot-spin). It wraps the parser's error (or names the unsatisfiable
	// spec), naming the offending spec — the construction-time debuggability
	// surface — rather than deferring the failure to Stream. errors.Is-able.
	ErrInvalidSchedule = errors.New("msgin/cron: invalid schedule spec")

	// ErrNilFactory is returned by NewSource when the message factory is nil. The
	// factory is the required source of every emitted payload, so a nil one is
	// refused up front rather than dereferenced into a panic on the first fire.
	ErrNilFactory = errors.New("msgin/cron: nil message factory")
)
