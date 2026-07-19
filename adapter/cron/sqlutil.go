package cron

import (
	"context"
	stdsql "database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// Querier is the subset of *sql.DB and *sql.Tx the coordinator dialects use.
// Both *sql.DB and *sql.Tx satisfy it. It is the dialect-author SPI surface — a
// custom LockerDialect/ElectorDialect names it — mirroring adapter/database/sql's
// Querier, but declared here so adapter/cron carries no dependency on the queue
// adapter (Plan 011 KD-1).
type Querier interface {
	ExecContext(ctx context.Context, query string, args ...any) (stdsql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*stdsql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *stdsql.Row
}

// ErrInvalidTableName is returned when a table identifier fails validation
// against ^[A-Za-z_][A-Za-z0-9_]*$. The name cannot be a bound parameter, so it
// is validated and dialect-quoted before interpolation; an invalid identifier is
// refused up front (the sole injection guard). errors.Is-able.
var ErrInvalidTableName = errors.New("msgin/cron: invalid table name")

var identPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// validateIdent returns ErrInvalidTableName unless name matches identPattern. A
// dialect method or DDL builder MUST call it before quoting/interpolating a table
// name.
func validateIdent(name string) error {
	if !identPattern.MatchString(name) {
		return fmt.Errorf("%w: %q", ErrInvalidTableName, name)
	}
	return nil
}

// pgQuote / mysqlQuote / sqliteQuote double the engine's identifier quote char.
func pgQuote(n string) string     { return `"` + strings.ReplaceAll(n, `"`, `""`) + `"` }
func mysqlQuote(n string) string  { return "`" + strings.ReplaceAll(n, "`", "``") + "`" }
func sqliteQuote(n string) string { return `"` + strings.ReplaceAll(n, `"`, `""`) + `"` }

// quoteTable validates then quotes via q; used at the entry of every dialect
// method and DDL builder.
func quoteTable(q func(string) string, table string) (string, error) {
	if err := validateIdent(table); err != nil {
		return "", err
	}
	return q(table), nil
}

// nowMicrosSQLite is the SQLite DB-clock expression in epoch microseconds
// (mirrors adapter/database/sql/sqlite). It is constant within a single SQL
// statement/step, so two uses in one statement compare equal.
const nowMicrosSQLite = `CAST(unixepoch('now','subsec')*1000000 AS INTEGER)`
