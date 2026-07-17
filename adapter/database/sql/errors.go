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
)
