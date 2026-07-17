package sqlite

import (
	"strings"

	msginsql "github.com/kartaladev/msgin/adapter/database/sql"
)

// sqliteQuote double-quotes a SQLite identifier. The name must already be
// validated (ValidateIdent admits no double-quote), so wrapping is safe;
// doubling any embedded `"` is defense-in-depth in case this is ever reached
// without prior validation.
func sqliteQuote(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

// sqliteQuoteTable validates then quotes a table identifier for interpolation.
func sqliteQuoteTable(table string) (string, error) {
	if err := msginsql.ValidateIdent(table); err != nil {
		return "", err
	}
	return sqliteQuote(table), nil
}
