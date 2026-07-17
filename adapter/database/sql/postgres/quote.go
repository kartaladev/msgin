package postgres

import (
	"strings"

	msginsql "github.com/kartaladev/msgin/adapter/database/sql"
)

// pgQuote double-quotes a PostgreSQL identifier. The name must already be
// validated (ValidateIdent admits no double-quote), so wrapping is safe;
// doubling any embedded `"` is defense-in-depth (belt-and-suspenders) in case
// this is ever reached without prior validation.
func pgQuote(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

// pgQuoteTable validates then quotes a table identifier for interpolation.
func pgQuoteTable(table string) (string, error) {
	if err := msginsql.ValidateIdent(table); err != nil {
		return "", err
	}
	return pgQuote(table), nil
}
