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
// path. Use msginsql.PostgresDDL(table) / msginsql.MySQLDDL(table) to obtain the
// reference CREATE TABLE (+ index) for your migration tool, or call the optional,
// idempotent EnsureSchema in dev and tests.
//
// # Dialects
//
// The exported LeaseDialect SPI owns the complete SQL for every operation, so no
// cross-dialect statement ever runs. Every adapter constructor
// (NewPollingSource, NewOutboundAdapter, NewInboxDeduper) takes the dialect as
// an explicit, required argument — there is no driver auto-detect (ADR 0011): a
// nil dialect is refused at construction with ErrNilDialect, so the wrong SQL
// is never silently generated for an unrecognized or wire-compatible-derivative
// driver. The two built-ins are msginsql.PostgresDialect() and
// msginsql.MySQLDialect() (this task; forthcoming releases move them to their
// own postgres/mysql packages). They are behavior-identical — same lease/claim
// predicate, fence semantics, and delivery_count/lease_epoch bumps — over each
// engine's own SQL (Postgres has RETURNING; MySQL has none, so its claim runs an
// atomic two-step SELECT ... FOR UPDATE SKIP LOCKED + UPDATE in one transaction).
// All persisted timestamps use the DB server clock (Postgres now() / MySQL
// UTC_TIMESTAMP(6)); only scheduling (poll interval, backoff) uses the app clock.
// This split keeps lease-expiry and visibility comparisons free of app↔DB skew.
//
// # Dialect-author SPI
//
// A caller implementing their own LeaseDialect/InboxDialect (for a
// wire-compatible derivative, or a new engine) validates every table (or
// derived index) identifier with msginsql.ValidateIdent before dialect-quoting
// and interpolating it into SQL — the sole injection guard, since the
// identifier cannot be a bound parameter. A LockDialect implementation's
// ClaimLock/AckLock/NackLock builds its carried transaction with
// msginsql.BeginLockTx and settles it with msginsql.SettleLockTx, which
// guarantee the always-commit-on-success / rollback-on-error contract so a
// pooled connection is never leaked.
package sql
