package sql

import "errors"

var (
	// ErrInvalidTableName is returned when a table identifier fails validation
	// against ^[A-Za-z_][A-Za-z0-9_]*$. The table name cannot be a bound
	// parameter, so it is validated and dialect-quoted before interpolation;
	// an invalid identifier is refused up front rather than interpolated into
	// SQL (ADR 0010 D3, closing the only injection surface).
	ErrInvalidTableName = errors.New("msgin/sql: invalid table name")

	// ErrSchemaNotReady reports that the adapter's table is not initialized —
	// the caller has not applied the reference DDL (see PostgresDDL) or called
	// EnsureSchema. It is exported so callers can errors.Is it; the source and
	// outbound wrap it naming the offending table (ADR 0010 D2).
	ErrSchemaNotReady = errors.New("msgin/sql: schema not ready")

	// ErrDialectUndetected is a construction error from NewPollingSource (and the
	// outbound constructor) when no WithDialect was given and the driver type
	// could not be matched to a built-in dialect. The wrapped message names the
	// driver type so the caller knows to pass WithDialect explicitly (ADR 0010
	// D3). Auto-detect is a best-effort convenience; WithDialect is the
	// guaranteed-correct path.
	ErrDialectUndetected = errors.New("msgin/sql: could not auto-detect a SQL dialect from the driver")

	// ErrInvalidLeaseTTL is a construction error from NewPollingSource when
	// WithLeaseTTL is given a non-positive duration. Unset leaves the safe 5m
	// default in place; an explicit non-positive value is a caller mistake, not a
	// request for the default (ADR 0010 D4).
	ErrInvalidLeaseTTL = errors.New("msgin/sql: lease TTL must be > 0")

	// ErrStaleLease is returned by a Source delivery's Ack/Nack closure when the
	// fenced settle matched no row — the handler outran the lease TTL and another
	// worker re-claimed the message (a different lease_epoch). It is NON-nil by
	// design so the runtime suppresses the phantom OnAck/eviction for a settle
	// this worker did not actually perform; the row is safely owned by the worker
	// that re-leased it and will be settled there. It is exported so callers can
	// errors.Is it in logs/hooks (ADR 0010 D4).
	ErrStaleLease = errors.New("msgin/sql: stale lease; message was re-claimed by another worker")

	// ErrInvalidStrategy is a construction error from NewPollingSource when
	// WithStrategy is given a value outside the defined Strategy range
	// (StrategyLeaseClaim, StrategyLockForUpdate). Unset leaves the safe
	// lease/claim default in place; an out-of-range value is a caller mistake,
	// not a request for the default (ADR 0010 D5).
	ErrInvalidStrategy = errors.New("msgin/sql: invalid strategy")

	// ErrLockStrategyUnsupported is a construction error from NewPollingSource
	// when WithStrategy(StrategyLockForUpdate) is selected but the resolved
	// LeaseDialect does not also implement LockDialect (the segregated lock/FOR UPDATE
	// SPI, ADR 0010 D5). The built-in PostgresDialect/MySQLDialect implement it; a
	// custom lease-only LeaseDialect must add LockDialect to be usable with the lock
	// strategy. The wrapped message names the offending dialect type.
	ErrLockStrategyUnsupported = errors.New("msgin/sql: lock strategy requires a LeaseDialect that implements LockDialect")

	// ErrInvalidPayload is returned by Outbound.Send when the message's payload
	// is not []byte. The Outbound is a wire adapter, not a LiveValueSource, so
	// msgin.NewProducer always JSON-encodes T to []byte before calling Send; a
	// non-[]byte payload here means Send was invoked directly (bypassing the
	// producer) with the wrong payload shape — a defensive case trusted
	// producers do not hit (ADR 0010 D8).
	ErrInvalidPayload = errors.New("msgin/sql: outbound payload must be []byte")

	// ErrNoSharedTransaction is returned by Outbound.Send, under
	// WithSharedTransaction (the strict mode), when the injected
	// TransactionResolver reports no caller transaction present in ctx
	// ((nil, nil)). Strict mode never silently dual-writes: it refuses the
	// standalone insert and returns this error instead, so a caller that
	// forgot to thread its business tx into ctx fails loudly rather than
	// silently losing the outbox's atomicity guarantee. Use
	// WithOpportunisticSharedTransaction to fall back to a standalone insert
	// instead (ADR 0010 D8).
	ErrNoSharedTransaction = errors.New("msgin/sql: no shared transaction in context")

	// ErrNilResolver is a construction error from NewOutboundAdapter when
	// WithSharedTransaction or WithOpportunisticSharedTransaction is given a
	// nil TransactionResolver. Caught at construction time rather than
	// deferred to the first Send, which would otherwise panic on a nil-func
	// call (ADR 0010 D8).
	ErrNilResolver = errors.New("msgin/sql: nil TransactionResolver")

	// ErrNilTx is returned by InboxDeduper.MarkProcessed when the caller's
	// business transaction is nil. MarkProcessed records the dedup row inside
	// the caller's tx by design (ADR 0010 D10) — the concrete *sql.Tx type
	// already bars passing the auto-committing pool — so a nil tx is refused up
	// front with this typed error rather than dereferenced into a panic
	// (upholding the no-panic-on-caller-input rule).
	ErrNilTx = errors.New("msgin/sql: nil transaction")

	// ErrInvalidRetention is returned by InboxDeduper.Purge when olderThan is
	// non-positive. A non-positive retention would set the delete cutoff to
	// now() (or the future), purging the ENTIRE inbox — after which every id
	// reads already=false on redelivery and is double-processed, the exact
	// failure the deduper exists to prevent. So it is refused up front (before
	// any DB call) rather than executed. A positive olderThan is still the
	// caller's responsibility to size above the source's max redelivery window
	// (see Purge's godoc). It is exported so callers can errors.Is it (ADR 0010
	// D10 MEDIUM, hardened per the Task 10 review).
	ErrInvalidRetention = errors.New("msgin/sql: retention (olderThan) must be > 0")

	// ErrInboxNoUniqueConstraint is returned by InboxDeduper.Ready when the inbox
	// table exists but its msg_id column carries NO unique/primary-key
	// constraint. Without it, MySQL/MariaDB's INSERT IGNORE never detects a
	// conflict, so the dedup silently never works (Postgres would already fail
	// loudly on ON CONFLICT). Ready probes for the constraint and fails fast with
	// this error (naming the table) so a mis-provisioned schema is caught at boot,
	// not after messages have been silently double-processed. The reference DDL
	// (see InboxDDL) provides the constraint. It is exported so callers can
	// errors.Is it (ADR 0010 D10, added per the Task 10 review, the user's robust
	// choice).
	ErrInboxNoUniqueConstraint = errors.New("msgin/sql: inbox msg_id column has no unique constraint")

	// ErrInboxInsertFailed is returned by the MySQL InboxDialect when an inbox
	// INSERT IGNORE affected no row AND a verifying SELECT finds no matching row
	// — meaning INSERT IGNORE demoted a genuine (non-duplicate) data error to a
	// warning (ADR 0010 D10 MEDIUM 6). The message is NOT deduplicated (which
	// would silently drop it); the error surfaces so the caller retries. It is
	// exported so callers can errors.Is it. Postgres has no equivalent path (its
	// ON CONFLICT ... RETURNING distinguishes insert from conflict exactly).
	ErrInboxInsertFailed = errors.New("msgin/sql: inbox insert did not take effect and is not a duplicate")
)
