package cron

import (
	"context"
	stdsql "database/sql"
	"time"

	"github.com/kartaladev/msgin"
)

// defaultFiredTable is the per-fire dedup table used when WithLockerTable is
// unset — a sensible default (CLAUDE.md), so the common case needs no config.
const defaultFiredTable = "msgin_cron_fired"

type lockerConfig struct {
	table string
}

// LockerOption configures an SQLLocker.
type LockerOption func(*lockerConfig)

// WithLockerTable sets the fired-keys table name (default "msgin_cron_fired").
// An empty/invalid identifier is rejected by NewSQLLocker with ErrInvalidTableName.
func WithLockerTable(table string) LockerOption {
	return func(c *lockerConfig) { c.table = table }
}

// SQLLocker is the dependency-free, SQL-backed Locker (ADR 0017): each Claim
// idempotently inserts (scope, fire_ts); the inserter wins — there is no
// recorded claimant identity (removed as YAGNI: nothing reads it, and the
// winner is decided solely by whose INSERT succeeds). It reuses the proven
// InboxDeduper dedup mechanism keyed on the deterministic fire time. Recommended
// coordination primitive for GRID-ALIGNED schedules (standard cron / descriptors)
// — no failover gap. NewSource enforces this: a Locker paired with an "@every"
// schedule is refused at construction (ErrLockerRequiresGridSchedule) because
// the dedup key is not instance-invariant there — use an Elector instead.
// Starts no goroutine.
type SQLLocker struct {
	db      *stdsql.DB
	table   string
	dialect LockerDialect
}

var _ Locker = (*SQLLocker)(nil)

// NewSQLLocker builds an SQLLocker over db using dialect for the exact SQL (pass
// PostgresLocker()/MySQLLocker()/SQLiteLocker() or your own). A nil db is
// msgin.ErrNilAdapter; an invalid table is ErrInvalidTableName; a nil dialect is
// ErrNilDialect — all at construction.
func NewSQLLocker(db *stdsql.DB, dialect LockerDialect, opts ...LockerOption) (*SQLLocker, error) {
	if db == nil {
		return nil, msgin.ErrNilAdapter
	}
	if dialect == nil {
		return nil, ErrNilDialect
	}
	cfg := lockerConfig{table: defaultFiredTable}
	for _, o := range opts {
		o(&cfg)
	}
	if err := validateIdent(cfg.table); err != nil {
		return nil, err
	}
	return &SQLLocker{db: db, table: cfg.table, dialect: dialect}, nil
}

// Claim implements Locker: it inserts (scope, fire) and reports whether this
// instance won the fire. Runs on the pool (no tx) — the insert is autonomous.
func (l *SQLLocker) Claim(ctx context.Context, scope string, fire time.Time) (bool, error) {
	return l.dialect.ClaimFire(ctx, l.db, l.table, scope, fire)
}

// Purge deletes fired-keys rows whose claimed_at (DB clock) is older than
// olderThan and returns the count. It is manual (no background goroutine) — the
// caller schedules it. A non-positive olderThan is ErrInvalidRetention (a
// zero/negative cutoff would delete rows for fires still being claimed by lagging
// instances). Size olderThan comfortably above your longest fire interval plus
// cross-instance clock skew.
func (l *SQLLocker) Purge(ctx context.Context, olderThan time.Duration) (int64, error) {
	if olderThan <= 0 {
		return 0, ErrInvalidRetention
	}
	return l.dialect.PurgeFired(ctx, l.db, l.table, olderThan)
}

// EnsureSchema idempotently creates the fired-keys table (opt-in; production uses
// the *LockerDDL reference builder instead — msgin never runs DDL implicitly).
func (l *SQLLocker) EnsureSchema(ctx context.Context) error {
	return l.dialect.EnsureFiredSchema(ctx, l.db, l.table)
}
