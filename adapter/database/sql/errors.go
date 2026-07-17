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
)
