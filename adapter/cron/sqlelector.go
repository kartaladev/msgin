package cron

import (
	"context"
	"crypto/rand"
	stdsql "database/sql"
	"encoding/hex"
	"time"

	"github.com/kartaladev/msgin"
)

const (
	defaultLeaseTable = "msgin_cron_leader"
	defaultLeaseTTL   = 30 * time.Second
)

type electorConfig struct {
	table      string
	instanceID string
	leaseTTL   time.Duration
	ttlSet     bool
}

// ElectorOption configures an SQLElector.
type ElectorOption func(*electorConfig)

// WithElectorTable sets the lease table (default "msgin_cron_leader").
func WithElectorTable(table string) ElectorOption { return func(c *electorConfig) { c.table = table } }

// WithElectorInstanceID sets this instance's holder identity (default a
// per-process crypto-random id). Two instances MUST NOT share an id. This is
// Elector-only — the Locker carries no claimant identity (its WithInstanceID
// was removed as YAGNI, Task 3 review pass 2); the Elector's holder is
// correctness-bearing (it decides lease ownership), unlike the Locker's
// removed observability-only claimant.
func WithElectorInstanceID(id string) ElectorOption {
	return func(c *electorConfig) {
		if id != "" {
			c.instanceID = id
		}
	}
}

// WithLeaseTTL sets how long an acquired lease is valid before another instance
// may take over. Default 30s. Because renewal is on-demand (each IsLeader call is
// the renewal), single-fire holds at any TTL, but the TTL bounds the crash
// failover gap: after a leader crash, fires within [crash, lease-expiry] are
// missed by everyone. Smaller = faster failover, more re-election churn. This
// does NOT make leadership unconditionally "sticky": with an on-demand renewal
// and a TTL shorter than the fire interval, the lease has always expired by the
// next fire, so every call is a fresh election (single-fire correctness still
// holds — see the Elector interface godoc). Prefer the SQLLocker for
// grid-aligned schedules where the failover gap matters; the Elector is the
// coordinator for "@every" schedules (the Locker refuses that combination). A
// non-positive d is ErrInvalidLeaseTTL (a caller mistake, not a request for the
// default).
func WithLeaseTTL(d time.Duration) ElectorOption {
	return func(c *electorConfig) {
		c.leaseTTL = d
		c.ttlSet = true
	}
}

// SQLElector is the dependency-free, SQL-backed Elector (ADR 0017): IsLeader
// runs an atomic acquire-or-renew of a single lease row, scoped per call (Round-1
// audit M-1 — symmetric with SQLLocker.Claim; there is no scope baked at
// construction, so one SQLElector can gate many independent schedules). On-demand
// (no heartbeat goroutine). DB-server-clock throughout. Failover latency ≤
// WithLeaseTTL.
type SQLElector struct {
	db         *stdsql.DB
	table      string
	instanceID string
	leaseTTL   time.Duration
	dialect    ElectorDialect
}

var _ Elector = (*SQLElector)(nil)

// NewSQLElector builds an SQLElector over db using dialect (PostgresElector()/
// MySQLElector()/SQLiteElector() or your own). A nil db is msgin.ErrNilAdapter; a
// nil dialect is ErrNilDialect; an invalid table is ErrInvalidTableName; a
// non-positive WithLeaseTTL is ErrInvalidLeaseTTL — checked in that order
// (nil-arg checks before value validation, matching NewSQLLocker's db -> dialect
// -> table order).
func NewSQLElector(db *stdsql.DB, dialect ElectorDialect, opts ...ElectorOption) (*SQLElector, error) {
	if db == nil {
		return nil, msgin.ErrNilAdapter
	}
	if dialect == nil {
		return nil, ErrNilDialect
	}
	cfg := electorConfig{
		table:      defaultLeaseTable,
		instanceID: randomID(),
		leaseTTL:   defaultLeaseTTL,
	}
	for _, o := range opts {
		o(&cfg)
	}
	if err := validateIdent(cfg.table); err != nil {
		return nil, err
	}
	if cfg.ttlSet && cfg.leaseTTL <= 0 {
		return nil, ErrInvalidLeaseTTL
	}
	return &SQLElector{
		db: db, table: cfg.table,
		instanceID: cfg.instanceID, leaseTTL: cfg.leaseTTL, dialect: dialect,
	}, nil
}

// IsLeader implements Elector: it runs an atomic acquire-or-renew for scope and
// reports whether this instance now holds a valid lease for it. scope is the
// leadership domain (per call, not baked at construction — Round-1 audit M-1);
// the Source passes its own WithScope value.
func (e *SQLElector) IsLeader(ctx context.Context, scope string) (bool, error) {
	return e.dialect.AcquireOrRenew(ctx, e.db, e.table, scope, e.instanceID, e.leaseTTL)
}

// EnsureSchema idempotently creates the lease table (opt-in).
func (e *SQLElector) EnsureSchema(ctx context.Context) error {
	return e.dialect.EnsureLeaseSchema(ctx, e.db, e.table)
}

// randomID returns a per-process crypto-random 128-bit hex id (mirrors the
// core's message-id / lease-owner generation). The crypto/rand.Read error is
// deliberately ignored: it is practically impossible to fail on a supported Go
// 1.25 platform, and the core's own randomID makes the same trade-off — see
// message.go. (An all-zero id on a hypothetical failure would make two Elector
// instances treat each other as "self"; harmless in practice, tracked as a NIT.)
func randomID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
