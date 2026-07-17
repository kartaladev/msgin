package sql

import (
	"crypto/rand"
	"encoding/hex"
	"io"
	"log/slog"
	"time"
)

// defaultLeaseTTL is the lease duration applied when WithLeaseTTL is unset. It
// matches the runtime's defaultAttemptTTL (5m): comfortably above any plausible
// handler round-trip, so a slow handler is not silently double-processed while
// WithHandlerTimeout is disabled by default. See WithLeaseTTL for the full
// invariant (ADR 0010 D4).
const defaultLeaseTTL = 5 * time.Minute

// Strategy selects how a Source claims and settles rows (ADR 0010 D4/D5). The
// zero value is StrategyLeaseClaim, so an unset strategy is the safe default.
type Strategy int

const (
	// StrategyLeaseClaim (the default) claims rows with a short, committed lease
	// tx (bumping delivery_count and a fence token) and settles each delivery in
	// a separate fenced tx. It scales to batches without pinning a connection per
	// in-flight message; an expired lease is reclaimed by the next poll (the
	// inlined reaper). See WithLeaseTTL (ADR 0010 D4).
	StrategyLeaseClaim Strategy = iota

	// StrategyLockForUpdate claims a single row inside a SELECT ... FOR UPDATE
	// SKIP LOCKED transaction that is CARRIED in the Delivery and owned by the
	// worker from claim to settle: Ack = DELETE + commit; Nack = clear-lock
	// UPDATE + commit (always commits — it persists delivery_count++ and releases
	// the lock; there are no business writes to roll back). A process crash (only)
	// auto-releases the row via tx rollback — no lease-expiry reaper, no fence
	// token. It gives stronger crash safety than the lease strategy at the cost of
	// one pinned pooled connection per in-flight message, so it requires care with
	// the DB pool topology — see WithStrategy for the mandatory pool invariants.
	// Requires a Dialect that also implements LockDialect (ErrLockStrategyUnsupported
	// otherwise); the built-ins satisfy it. Under this strategy WithLeaseTTL is
	// inert (there is no lease). (ADR 0010 D5.)
	StrategyLockForUpdate
)

// config accumulates Option settings before NewPollingSource builds a Source.
type config struct {
	dialect     Dialect
	strategy    Strategy
	leaseTTL    time.Duration
	leaseTTLSet bool // distinguishes explicit WithLeaseTTL(0) (rejected) from unset (default)
	lockedBy    string
	logger      *slog.Logger
}

// Option configures a Source built by NewPollingSource.
type Option func(*config)

// WithDialect selects the Dialect explicitly, bypassing driver auto-detect. It
// is the guaranteed-correct path (auto-detect is heuristic and may mis-detect a
// wire-compatible derivative) and the escape hatch for a derivative's quirks:
// pass WithDialect(sql.PostgresDialect()) or your own Dialect implementation
// (ADR 0010 D3). A nil dialect is ignored (leaves auto-detect in place) rather
// than deferring a nil-panic to the first Poll.
func WithDialect(d Dialect) Option {
	return func(c *config) {
		if d != nil {
			c.dialect = d
		}
	}
}

// WithStrategy selects the claim/settle strategy (ADR 0010 D4/D5). Unset, it is
// StrategyLeaseClaim — the default that scales to batches without pinning a
// connection. Pass StrategyLockForUpdate for stronger crash safety (a crash
// rolls back the carried FOR UPDATE tx, releasing the row immediately, with no
// lease-expiry double-processing window). An out-of-range value is a
// construction error (ErrInvalidStrategy); StrategyLockForUpdate additionally
// requires the resolved Dialect to implement LockDialect (ErrLockStrategyUnsupported).
//
// # Lock strategy: mandatory DB-pool topology (read before choosing it)
//
// StrategyLockForUpdate pins ONE pooled connection per in-flight message for the
// whole message lifetime (claim → settle). Two invariants follow, neither
// statically enforceable (the Source cannot see the consumer's pool sizing or a
// sink's separate pool), so both are the caller's responsibility:
//
//   - Separate DLQ pool (mandate). On a permanent failure the runtime calls the
//     DLQ/invalid sink's Send BEFORE it Acks (freeing the claim connection), so a
//     sink that is itself a sql adapter on the SAME *sql.DB needs an ADDITIONAL
//     connection while every claim connection is pinned — the divert INSERT then
//     blocks on the exhausted pool and the flow stalls until the shutdown
//     deadline. A lock-strategy consumer whose DLQ/invalid sink is a sql adapter
//     MUST give that sink a SEPARATE *sql.DB (separate pool), or size one shared
//     pool with headroom poolSize >= maxInFlight + divert-concurrency.
//   - Pool-coupling. Effective in-flight = min(WithMaxInFlight, WithConcurrency,
//     poolSize - headroom). If the requested in-flight exceeds the pool, the
//     poller stalls on an exhausted pool. Pair the lock strategy with
//     db.SetMaxOpenConns(...), WithPollMaxBatch(1) (the lock claim returns at most
//     one row regardless), and a separate DLQ pool.
//
// WithLeaseTTL is inert under StrategyLockForUpdate (there is no lease).
func WithStrategy(s Strategy) Option {
	return func(c *config) { c.strategy = s }
}

// WithLeaseTTL sets how long a claim's lease is held before it is treated as
// expired and the message becomes claimable again by another poll (the inlined
// reaper). Unset, it defaults to 5m.
//
// # Invariant (read before overriding)
//
// leaseTTL MUST exceed the worst-case redelivery round-trip: handler execution
// + settle/Ack latency + a margin — NOT merely WithHandlerTimeout. If a
// handler (plus its Ack) can take longer than leaseTTL, another worker reclaims
// the message mid-flight and it is processed twice (the fenced Ack of the first
// worker then no-ops with ErrStaleLease). The lease expiry is evaluated on the
// DB server clock while WithHandlerTimeout is evaluated on the app clock, so
// account for app<->DB clock skew in the margin. The default is deliberately
// generous (5m) for exactly this reason; shrink it only when you know your
// handler round-trip is well under the new value.
//
// A non-positive d is a construction error (ErrInvalidLeaseTTL) rather than a
// silent default: an explicit zero/negative is a caller mistake, not a request
// for the default.
func WithLeaseTTL(d time.Duration) Option {
	return func(c *config) {
		c.leaseTTL = d
		c.leaseTTLSet = true
	}
}

// WithLockedBy sets the lease-owner id stamped into locked_by on claim and
// matched by the fenced Ack/Nack. Unset, it defaults to a random 128-bit hex
// id, which is the safe choice: each Source instance owns a distinct id, so two
// instances (or two processes) never mistake each other's leases. Override it
// only when you need a stable, human-readable owner for observability and you
// guarantee uniqueness per running Source. An empty string is treated as unset
// (the random default is used).
func WithLockedBy(id string) Option {
	return func(c *config) { c.lockedBy = id }
}

// WithLogger injects the structured logger the Source uses for its WARN/ERROR
// diagnostics (a stale-lease settle, a corrupt/foreign row skipped on Poll).
// The default is a discard logger, so the Source never logs to a package global
// (observability via injection, not globals — CLAUDE.md). A nil logger is
// ignored (the discard default stays in place).
func WithLogger(l *slog.Logger) Option {
	return func(c *config) {
		if l != nil {
			c.logger = l
		}
	}
}

// randomLockedBy returns a random 128-bit hex id, the default lease owner when
// WithLockedBy is unset (mirrors the core's message-id generation).
func randomLockedBy() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// discardLogger is the default logger: it drops every record.
func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
