package cron

import (
	"context"
	"time"
)

// Elector gates fires under a given scope: only the elected leader for that
// scope emits. IsLeader runs an atomic acquire-or-renew each call (checked
// synchronously per fire — no heartbeat goroutine); leadership holds while a
// call's lease is still valid and re-elects once it has expired — NOT
// unconditionally "sticky": with on-demand renewal and WithLeaseTTL shorter
// than the fire interval, every fire is a fresh election. A non-nil error
// causes the fire to be SKIPPED fail-safe (never N-fold firing). Configure it
// with WithElector; at most one of WithElector/WithLocker may be set. The
// scope passed on each call is the Source's own scope (WithScope), so one
// Elector instance naturally gates many independent schedules — symmetric with
// Locker.Claim. The SQL-backed implementation is SQLElector; it is the
// coordinator to use for "@every" schedules (the Locker is restricted to
// grid-aligned schedules — see Locker).
type Elector interface {
	IsLeader(ctx context.Context, scope string) (bool, error)
}

// Locker gates ONE fire of a Source: the instance that claims (scope, fire)
// emits; the rest skip. Claim is a deterministic per-fire dedup — every
// instance computes the same (scope, fire) key and exactly one wins. A
// non-nil error causes the fire to be SKIPPED fail-safe. Configure it with
// WithLocker; the Source supplies scope from WithScope (default: the spec
// string). The SQL-backed implementation is SQLLocker; it is the recommended
// primitive for GRID-ALIGNED schedules (standard 5-field cron and
// @daily/@hourly/... descriptors) — no failover gap. It is UNSUPPORTED for
// "@every" schedules: NewSource refuses a Locker paired with an "@every"
// schedule at construction (ErrLockerRequiresGridSchedule), because
// "@every"'s next-fire computation is relative to each instance's own
// last-fire/start time, so independent instances never converge on the same
// dedup key — the Locker would silently dedup nothing. Use the Elector for
// "@every".
type Locker interface {
	Claim(ctx context.Context, scope string, fire time.Time) (won bool, err error)
}
