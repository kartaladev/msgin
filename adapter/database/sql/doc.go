// Package sql is the msgin channel adapter for any database/sql-compatible SQL
// database, using a table as a durable, at-least-once message queue: a polling
// SELECT ... FOR UPDATE SKIP LOCKED inbound and an INSERT outbound. The generic
// database/sql package is stdlib and the SQL driver is caller-injected, so this
// adapter imports no driver and lives in the core module (ADR 0003).
//
// # Import alias
//
// The package is named sql and collides with the standard library's
// database/sql, which callers also need (to hold the *sql.DB they inject).
// Import this package under an alias — the convention throughout msgin is
// msginsql:
//
//	import (
//		"database/sql"
//
//		msginsql "github.com/kartaladev/msgin/adapter/database/sql"
//	)
//
// # Schema ownership
//
// The caller provisions the schema; msgin never runs DDL on the production
// path. Use msginsql.PostgresDDL(table) to obtain the reference CREATE TABLE
// (+ index) for your migration tool, or call the optional, idempotent
// EnsureSchema in dev and tests.
//
// # Dialects
//
// The exported Dialect SPI owns the complete SQL for every operation, so no
// cross-dialect statement ever runs. The built-in is msginsql.PostgresDialect().
// All persisted timestamps use the DB server clock (now()); only scheduling
// (poll interval, backoff) uses the app clock. This split keeps lease-expiry
// and visibility comparisons free of app↔DB skew.
package sql
