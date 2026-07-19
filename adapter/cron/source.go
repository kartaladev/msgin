// This file implements Source[T], the msgin channel adapter that ORIGINATES
// messages on a recurring / cron schedule. See doc.go for the package doc.

package cron

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/jonboulle/clockwork"
	robfig "github.com/robfig/cron/v3"

	"github.com/kartaladev/msgin"
)

// Source is a msgin.StreamingSource that emits a caller-defined message on each
// fire of a cron / recurring schedule. It carries LIVE Go values (no codec),
// like the memory adapter, so NewConsumer[T] pairs it with no codec.
//
// Delivery guarantee: AT-MOST-ONCE. A fire is an ephemeral trigger, not a
// durable row — Ack/Nack are no-ops. A transient handler failure is still
// retried in-process by the runtime's RetryPolicy (same delivery); a permanent
// failure routes to the invalid/DLQ sink. On overrun (a slow handler blocks the
// hand-off past a scheduled instant) the missed fire is SKIPPED, not queued.
//
// Multi-instance: with NO coordinator (WithElector/WithLocker), N replicas each
// fire on every tick (N-fold). Configure a coordinator for single-fire.
type Source[T any] struct {
	schedule robfig.Schedule
	factory  func(fire time.Time) T
	clock    clockwork.Clock
	location *time.Location
	scope    string
	gate     gate // nil unless a coordinator is configured (Task 2)
	logger   *slog.Logger
}

var (
	_ msgin.StreamingSource = (*Source[any])(nil)
	_ msgin.LiveValueSource = (*Source[any])(nil)
)

// gate reports whether THIS instance should emit the fire at fireTime — the
// coordination hook consulted once per fire (Task 2). nil means "always emit".
type gate func(ctx context.Context, fire time.Time) (won bool, err error)

// config accumulates Option settings before NewSource builds a Source.
type config struct {
	clock    clockwork.Clock
	location *time.Location
	scope    string
	scopeSet bool
	elector  Elector // Task 2
	locker   Locker  // Task 2
	logger   *slog.Logger
}

// Option configures a Source built by NewSource.
type Option func(*config)

// WithClock injects the clock the firing loop waits on. Default is the real wall
// clock; tests inject a clockwork.FakeClock. A nil clock is ignored (the default
// stays in place) rather than a deferred nil-panic.
func WithClock(c clockwork.Clock) Option {
	return func(o *config) {
		if c != nil {
			o.clock = c
		}
	}
}

// WithLocation sets the timezone the schedule is evaluated in. Cron specs are
// timezone-sensitive ("0 9 * * *" means 09:00 in WHICH zone); the default is
// UTC — the safe, explicit choice. A nil location is ignored (UTC kept). A
// spec-embedded "CRON_TZ=..."/"TZ=..." prefix (robfig-supported) OVERRIDES
// this option for that spec — robfig's parser bakes the prefix's zone into the
// parsed Schedule, which then ignores the input time's location. "@every"
// intervals ignore location entirely (they are relative durations, not
// wall-clock instants).
func WithLocation(loc *time.Location) Option {
	return func(o *config) {
		if loc != nil {
			o.location = loc
		}
	}
}

// WithScope sets the coordination scope passed to a Locker's Claim or an
// Elector's IsLeader (it has no effect without WithLocker/WithElector).
// Default is the raw spec string. All instances of the SAME scheduled job must
// share a scope (running the same spec, they do); two DISTINCT jobs that
// happen to share a spec MUST set distinct scopes, or one job's per-fire claim
// (Locker) or leadership (Elector) will suppress the other's fire. This
// collision risk is real precisely for the GRID-ALIGNED (cron/descriptor)
// specs the Locker supports — e.g. two unrelated jobs both scheduled
// "@hourly" collide by default — since the Locker's dedup key is only
// instance-invariant for that class of schedule (see Locker). An empty string
// is treated as unset (the spec-string default is used).
func WithScope(scope string) Option {
	return func(o *config) {
		o.scope = scope
		o.scopeSet = true
	}
}

// WithCronLogger injects the structured logger the Source uses to report a
// coordinator error before it skips a fire (fail-safe). Default is a discard
// logger — the Source never logs to a package global. A nil logger is ignored.
func WithCronLogger(l *slog.Logger) Option {
	return func(o *config) {
		if l != nil {
			o.logger = l
		}
	}
}

// WithElector gates every fire behind leader election (see Elector): only the
// leader instance emits. Mutually exclusive with WithLocker (ErrConflictingCoordinator).
func WithElector(e Elector) Option {
	return func(o *config) { o.elector = e }
}

// WithLocker gates each fire behind a per-fire claim (see Locker): exactly one
// instance wins each (scope, fire) and emits. Mutually exclusive with WithElector
// (ErrConflictingCoordinator). Requires a GRID-ALIGNED schedule (5-field cron or
// a @daily/@hourly/... descriptor) — an "@every" schedule is refused with
// ErrLockerRequiresGridSchedule (Round-1 audit B-1; use WithElector for
// "@every"). Pair with WithScope when distinct jobs share a spec.
func WithLocker(l Locker) Option {
	return func(o *config) { o.locker = l }
}

// NewSource parses spec once (cron 5-field + "@every" + descriptors) and builds
// a Source emitting factory(fireTime) on each fire. An unparseable spec, OR a
// syntactically valid spec with no future occurrence (e.g. "0 0 30 2 *"), is
// ErrInvalidSchedule; a nil factory is ErrNilFactory; both a WithElector and a
// WithLocker set is ErrConflictingCoordinator (Task 2); a WithLocker paired
// with an "@every" schedule is ErrLockerRequiresGridSchedule (Task 2) — all at
// construction, so misuse fails loudly up front rather than on the first fire
// (or, for the unsatisfiable-schedule case, never — see the Stream guard).
func NewSource[T any](spec string, factory func(fire time.Time) T, opts ...Option) (*Source[T], error) {
	schedule, err := robfig.ParseStandard(spec)
	if err != nil {
		return nil, fmt.Errorf("%w: %q: %v", ErrInvalidSchedule, spec, err)
	}
	if factory == nil {
		return nil, ErrNilFactory
	}

	cfg := config{
		clock:    clockwork.NewRealClock(),
		location: time.UTC,
		logger:   discardLogger(),
	}
	for _, opt := range opts {
		opt(&cfg)
	}

	// Probe satisfiability (Round-1 audit B-2): ParseStandard accepts specs with
	// no future occurrence (e.g. Feb 30). An unguarded Stream loop would compute
	// a zero Next, a hugely negative Sub, and hot-spin on an immediately-firing
	// real clock.After. Catch it here rather than at runtime.
	if schedule.Next(cfg.clock.Now().In(cfg.location)).IsZero() {
		return nil, fmt.Errorf("%w: %q has no future occurrence", ErrInvalidSchedule, spec)
	}

	scope := spec
	if cfg.scopeSet && cfg.scope != "" {
		scope = cfg.scope
	}

	s := &Source[T]{
		schedule: schedule,
		factory:  factory,
		clock:    cfg.clock,
		location: cfg.location,
		scope:    scope,
		logger:   cfg.logger,
	}
	if err := s.wireGate(&cfg); err != nil {
		return nil, err
	}
	return s, nil
}

// Stream drives the firing loop until ctx is cancelled (msgin.StreamingSource).
// It starts NO goroutine — the loop runs on the caller's (runtime Run) goroutine
// — so it is goleak-clean.
//
// The loop grid-tracks the schedule: it holds a single "next" pointer, seeded
// once from the clock's current time, and advances it by exactly one
// schedule step (schedule.Next(next)) each time that instant is reached —
// NEVER by recomputing from the clock's current time on every iteration.
// Recomputing from "now" would be wrong for a non-grid schedule like
// "@every <duration>" (robfig's ConstantDelaySchedule.Next(t) is simply
// t+duration, not aligned to a fixed grid): reseeding from an arbitrary
// "now" after an overrun would silently redefine the grid's phase instead of
// preserving it. Grid-tracking from the prior "next" keeps the schedule's
// phase fixed regardless of when the loop happens to observe it.
//
// Before waiting on each pointer, the loop checks whether it has ALREADY
// elapsed (a slow hand-off — the blocking `out <-` send below — let one or
// more scheduled instants pass while unread) and, if so, advances past every
// such elapsed instant WITHOUT emitting, one schedule step at a time, until
// it reaches one still in the future. This is the "skip missed, not queued"
// guarantee: an overrun collapses any number of missed instants into zero
// deliveries for them, rather than queuing or firing them late.
func (s *Source[T]) Stream(ctx context.Context, out chan<- msgin.Delivery) error {
	next := s.schedule.Next(s.clock.Now().In(s.location))
	for {
		if next.IsZero() {
			// Belt-and-suspenders for a schedule that becomes unsatisfiable only
			// after construction validated it (NewSource already refuses an
			// unsatisfiable spec up front, Round-1 audit B-2) — never fire, exit
			// cleanly on cancel rather than hot-spin on a hugely negative Sub.
			<-ctx.Done()
			return ctx.Err()
		}
		for !next.After(s.clock.Now().In(s.location)) {
			// next is already due or past by the time we got back here: an
			// overrun on the previous fire's hand-off ate it. Skip it (never
			// deliver it, never queue it) and grid-track to the following step.
			next = s.schedule.Next(next)
			if next.IsZero() {
				<-ctx.Done()
				return ctx.Err()
			}
		}
		select {
		case <-s.clock.After(next.Sub(s.clock.Now())):
			fire := next
			next = s.schedule.Next(fire) // advance the grid pointer for the next iteration
			if !s.win(ctx, fire) {
				continue // lost the fire, or a coordinator error → skip fail-safe
			}
			msg := msgin.New[any](s.factory(fire), msgin.WithClock(s.clock))
			select {
			case out <- msgin.Delivery{Msg: msg, Ack: noopAck, Nack: noopNack}:
			case <-ctx.Done():
				return ctx.Err()
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// win reports whether this instance should emit the fire at fireTime. With no
// gate it always wins. A coordinator ERROR is logged and treated as a LOSS
// (fail-safe skip) — a coordination outage must degrade to NO fire, never to
// N-fold firing.
func (s *Source[T]) win(ctx context.Context, fireTime time.Time) bool {
	if s.gate == nil {
		return true
	}
	won, err := s.gate(ctx, fireTime)
	if err != nil {
		s.logger.Error("msgin/cron: coordinator error; skipping fire (fail-safe)",
			"err", err, "fire", fireTime)
		return false
	}
	return won
}

// EmitsLiveValue reports that this source carries live Go values (no codec).
func (s *Source[T]) EmitsLiveValue() bool { return true }

// wireGate resolves the configured coordinator into s.gate: an Elector wraps
// IsLeader(ctx, scope) with the Source's own scope; a Locker wraps
// Claim(scope, fire); neither leaves s.gate nil (always emit). Both set is
// ErrConflictingCoordinator. A Locker paired with a ConstantDelaySchedule
// ("@every") is ErrLockerRequiresGridSchedule (checked BEFORE the delegation
// switch, alongside the conflicting-coordinator check, so both construction-time
// refusals live in one place).
func (s *Source[T]) wireGate(cfg *config) error {
	switch {
	case cfg.elector != nil && cfg.locker != nil:
		return ErrConflictingCoordinator
	case cfg.locker != nil:
		if _, isEvery := s.schedule.(robfig.ConstantDelaySchedule); isEvery {
			return ErrLockerRequiresGridSchedule
		}
	}
	switch {
	case cfg.elector != nil:
		e, scope := cfg.elector, s.scope
		s.gate = func(ctx context.Context, _ time.Time) (bool, error) { return e.IsLeader(ctx, scope) }
	case cfg.locker != nil:
		l, scope := cfg.locker, s.scope
		s.gate = func(ctx context.Context, fire time.Time) (bool, error) { return l.Claim(ctx, scope, fire) }
	}
	return nil
}

// noopAck / noopNack settle an at-most-once fire: there is no durable row to
// delete or requeue, so both are no-ops.
func noopAck(context.Context) error                       { return nil }
func noopNack(context.Context, bool, time.Duration) error { return nil }

// discardLogger is the default logger: it drops every record.
func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
