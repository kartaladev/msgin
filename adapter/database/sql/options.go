package sql

import (
	"context"
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

	txResolver    TransactionResolver
	txResolverSet bool // distinguishes "no shared-tx option given" from an explicit-but-nil resolver (ErrNilResolver)
	txStrict      bool // meaningful only when txResolverSet: true = WithSharedTransaction, false = WithOpportunisticSharedTransaction
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

// TransactionResolver resolves the caller's active business transaction from
// ctx as a Querier — a *sql.Tx satisfies Querier, so a caller storing a raw
// *sql.Tx, an sqlx wrapper, or a unit-of-work/repository layer's tx can all be
// adapted to msgin's Querier with a one-line resolver. Returning (nil, nil)
// means no shared transaction is present in ctx (the outbox falls back to its
// configured no-tx policy — see WithSharedTransaction /
// WithOpportunisticSharedTransaction); a non-nil error aborts Send, which
// wraps and returns it without attempting any insert (ADR 0010 D8).
type TransactionResolver func(ctx context.Context) (Querier, error)

// WithSharedTransaction enlists Outbound.Send's INSERT in the caller's active
// business transaction, resolved from ctx by r — the transactional-outbox
// pattern (ADR 0010 D8): a business write and the outbox INSERT then commit or
// roll back atomically, so a crash between them can neither lose the message
// nor publish one for a change that never committed. This is the STRICT,
// recommended default: if r reports no transaction present in ctx
// ((nil, nil)), Send returns ErrNoSharedTransaction rather than silently
// falling back to a standalone (non-atomic) insert — it never silently
// dual-writes. Use WithOpportunisticSharedTransaction for the explicit
// fallback-and-log variant instead.
//
// A nil r is a construction error (ErrNilResolver) from NewOutboundAdapter —
// never a deferred nil-func panic on the first Send.
//
// # msgin never commits or rolls back the borrowed transaction
//
// The Querier r returns is BORROWED: Outbound only ever calls
// ExecContext/QueryContext/QueryRowContext on it for the INSERT. It never
// calls Commit or Rollback — the caller retains full ownership of the
// transaction's lifecycle and decides when (and whether) it commits.
//
// # Same-database invariant (unenforceable — read before using)
//
// The transaction r resolves MUST be open on the SAME database as this
// Outbound's table. A *sql.Tx does not expose the *sql.DB it was opened
// against, so this cannot be verified at runtime; a mismatched database
// silently defeats the atomicity this option exists to provide. There is no
// way to make this safe automatically — verify it by construction (e.g. build
// the Outbound's *sql.DB and the caller's transaction from the same pool).
//
// # Do not use as a DLQ / invalid-message sink (LOW audit)
//
// A strict shared-transaction Outbound must NOT be passed as a
// msgin.RetryPolicy.DeadLetter or msgin.WithInvalidMessageSink target. The
// runtime's divert path calls Send on a settlement-scoped context that never
// carries the caller's business transaction, so r would report (nil, nil) on
// every poison message, Send would return ErrNoSharedTransaction, and the
// divert would treat that as "sink failed" and retry forever — the poison
// message never actually reaches the dead-letter table. Use a plain,
// non-shared-transaction Outbound (built without this option) as a DLQ/invalid
// sink.
func WithSharedTransaction(r TransactionResolver) Option {
	return func(c *config) {
		c.txResolver = r
		c.txResolverSet = true
		c.txStrict = true
	}
}

// WithOpportunisticSharedTransaction is the explicitly-named UNSAFE variant of
// WithSharedTransaction: Send uses the caller's transaction (resolved from ctx
// by r) when present, but falls back to a standalone, auto-commit insert on
// the pool when r reports none present — logging the fallback at WARN
// ("no shared transaction in context; standalone insert — atomicity NOT
// achieved", with the message id only, never the payload) so the atomicity
// loss is observable rather than silent. Prefer WithSharedTransaction (strict)
// unless a caller genuinely needs Send to succeed even when no business
// transaction is active; opportunistic fallback reintroduces the exact
// dual-write the outbox pattern exists to prevent, so it is deliberately named
// to make that trade-off visible at the call site rather than hidden behind a
// boolean flag.
//
// A nil r is a construction error (ErrNilResolver) from NewOutboundAdapter.
// The same "never Commit/Rollback the borrowed tx", same-database invariant,
// and DLQ-sink caveat documented on WithSharedTransaction apply here too.
func WithOpportunisticSharedTransaction(r TransactionResolver) Option {
	return func(c *config) {
		c.txResolver = r
		c.txResolverSet = true
		c.txStrict = false
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
