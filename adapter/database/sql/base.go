package sql

import (
	"context"
	stdsql "database/sql"
	"fmt"
	"log/slog"

	msgin "github.com/kartaladev/msgin"
)

// adapterBase holds the fields and operations shared by every table-backed
// adapter in this package (Source, Outbound): the db handle, the target
// table, the caller-supplied LeaseDialect, and the injected logger, plus the
// fail-fast readiness check, the opt-in schema bootstrap, and the query-error
// classifier that all of them build on. Factoring this out keeps the
// ValidateIdent validation and the Ready/EnsureSchema/classifyQueryErr bodies
// defined exactly once (ADR 0010 D2/D3, ADR 0011).
type adapterBase struct {
	db      *stdsql.DB
	table   string
	dialect LeaseDialect
	logger  *slog.Logger
}

// newAdapterBase validates db, table, and dialect, shared by NewPollingSource
// and NewOutboundAdapter. A nil db is msgin.ErrNilAdapter; an invalid table
// identifier is ErrInvalidTableName (checked via ValidateIdent); a nil dialect
// is ErrNilDialect (ADR 0011 — the dialect is a required constructor argument,
// there is no driver auto-detect fallback). Checked in that order, so a nil
// dialect never masks an invalid db/table mistake.
func newAdapterBase(db *stdsql.DB, table string, dialect LeaseDialect, cfg config) (adapterBase, error) {
	if db == nil {
		return adapterBase{}, msgin.ErrNilAdapter
	}
	if err := ValidateIdent(table); err != nil {
		return adapterBase{}, err
	}
	if dialect == nil {
		return adapterBase{}, ErrNilDialect
	}

	return adapterBase{db: db, table: table, dialect: dialect, logger: cfg.logger}, nil
}

// EnsureSchema idempotently creates the table and its claim index. It is
// optional and opt-in (dev/test/opt-in callers); production callers provision
// the schema via the reference DDL (see postgres.DDL / mysql.DDL) instead —
// msgin never runs DDL implicitly (ADR 0010 D2).
func (b adapterBase) EnsureSchema(ctx context.Context) error {
	return b.dialect.EnsureSchema(ctx, b.db, b.table)
}

// Ready is the fail-fast boot check (ADR 0010 D2): it returns
// ErrSchemaNotReady (naming the table) when the table is not initialized, so a
// forgotten migration fails the deploy immediately instead of the caller
// sitting in a silent poll/insert-error loop. Call it once at startup before
// NewConsumer.Run or before the first Producer.Send.
func (b adapterBase) Ready(ctx context.Context) error {
	exists, err := b.dialect.SchemaExists(ctx, b.db, b.table)
	if err != nil {
		return err
	}
	if !exists {
		return b.schemaNotReady()
	}
	return nil
}

// classifyQueryErr wraps a query failure (Claim or Insert) as ErrSchemaNotReady
// iff a follow-up portable probe reports the table missing (diagnosing a table
// dropped mid-run without a driver import); otherwise the raw error propagates.
func (b adapterBase) classifyQueryErr(ctx context.Context, err error) error {
	if exists, probeErr := b.dialect.SchemaExists(ctx, b.db, b.table); probeErr == nil && !exists {
		return b.schemaNotReady()
	}
	return err
}

// schemaNotReady builds the ErrSchemaNotReady error naming the table.
func (b adapterBase) schemaNotReady() error {
	return fmt.Errorf("%w: table %q not initialized; run EnsureSchema or apply the DDL",
		ErrSchemaNotReady, b.table)
}
