// Package mysql is the built-in MySQL/MariaDB dialect for the msgin sql
// adapter engine (github.com/kartaladev/msgin/adapter/database/sql). It owns
// the exact MySQL SQL for the lease/claim source, the lock/FOR UPDATE
// strategy, the outbound INSERT, and the idempotent-consumer dedup inbox —
// behavior-identical to the postgres dialect, expressed in MySQL SQL
// (LOCK IN SHARE MODE, UTC_TIMESTAMP(6), the two-step atomic claim MySQL's
// lack of RETURNING requires).
//
// It is a leaf-test dialect module (ADR 0011): it requires the engine ONLY —
// no SQL driver, no testcontainers, no testify — so a production consumer that
// imports it pays only the engine's (stdlib + clockwork) dependency cost. The
// caller injects the MySQL database/sql driver (e.g. go-sql-driver/mysql) and
// provisions the *sql.DB; msgin does not import a driver. It also covers
// MariaDB (wire-compatible).
//
// The public entry points are:
//
//   - LeaseDialect() — the msginsql.LeaseDialect passed to
//     NewPollingSource/NewOutboundAdapter (the single stateless value also
//     satisfies msginsql.LockDialect for WithStrategy(StrategyLockForUpdate)).
//   - InboxDialect() — the msginsql.InboxDialect passed to NewInboxDeduper.
//   - DDL(table) / InboxDDL(table) — the reference CREATE TABLE statements for
//     callers to fold into their own migration tool (msgin never runs DDL on
//     the production path; ADR 0010 D2).
//
// Behavior is identical to the pre-split built-in: this package MOVED verbatim
// out of the engine (Plan 006 Task 5); only its module boundary changed.
package mysql
