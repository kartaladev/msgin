package mysql

import (
	"strings"

	msginsql "github.com/kartaladev/msgin/adapter/database/sql"
)

// mysqlQuote back-quotes a MySQL identifier. The name must already be validated
// (ValidateIdent admits no backtick), so wrapping is safe; doubling any embedded
// backtick is defense-in-depth in case this is ever reached without prior
// validation (mirrors postgres's pgQuote).
func mysqlQuote(name string) string {
	return "`" + strings.ReplaceAll(name, "`", "``") + "`"
}

// mysqlQuoteTable validates then quotes a table identifier for interpolation.
func mysqlQuoteTable(table string) (string, error) {
	if err := msginsql.ValidateIdent(table); err != nil {
		return "", err
	}
	return mysqlQuote(table), nil
}

// placeholders returns "?, ?, ..." with n placeholders, for a MySQL IN (...)
// clause built from a known-length id slice.
func placeholders(n int) string {
	if n <= 0 {
		return ""
	}
	return strings.Repeat("?, ", n-1) + "?"
}
