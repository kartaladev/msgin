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

	// ErrConflictingCoordinator is returned by NewSource when BOTH WithElector and
	// WithLocker are configured. The two coordination strategies are mutually
	// exclusive — a fire is gated by leadership OR by per-fire claim, never both —
	// so configuring both is a caller mistake refused at construction.
	ErrConflictingCoordinator = errors.New("msgin/cron: at most one of WithElector/WithLocker may be set")

	// ErrLockerRequiresGridSchedule is returned by NewSource when a Locker is
	// configured against an "@every <duration>" schedule. The Locker's dedup key
	// (scope, fire_ts) is instance-invariant only for grid-aligned schedules
	// (5-field cron / @daily.../ descriptors) — robfig's ConstantDelaySchedule
	// (the "@every" implementation) computes the next fire relative to EACH
	// instance's own last-fire/start time, so independent instances never
	// converge on the same key and the Locker would silently dedup nothing
	// (Round-1 audit B-1). Use an Elector for "@every" schedules instead.
	// errors.Is-able.
	ErrLockerRequiresGridSchedule = errors.New("msgin/cron: a Locker requires a grid-aligned schedule (cron or descriptor); @every is unsupported — use an Elector instead")
)
