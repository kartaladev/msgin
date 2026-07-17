// Package sqlite is the built-in SQLite dialect for the msgin sql adapter
// engine (github.com/kartaladev/msgin/adapter/database/sql). It implements the
// engine's lease/claim source SPI (LeaseDialect) and the dedup-inbox SPI
// (InboxDialect); pass sqlite.LeaseDialect() to NewPollingSource/
// NewOutboundAdapter and sqlite.InboxDialect() to NewInboxDeduper.
//
// # Driver
//
// This module imports no driver — the caller opens the *sql.DB. The recommended
// driver is modernc.org/sqlite (pure-Go, cgo-free), which bundles SQLite >=3.45
// and satisfies this dialect's floor: RETURNING (>=3.35), ON CONFLICT (>=3.24),
// and unixepoch('now','subsec') (>=3.42). The DSN builder below emits
// modernc-flavored pragmas; other drivers need their own DSN.
//
// # Delivery guarantee
//
// At-least-once, identical to the postgres/mysql dialects. SQLite serializes
// writers (a single database-wide write lock), so there is no lock/FOR UPDATE
// strategy: sqlite.LeaseDialect() does NOT implement LockDialect, and
// WithStrategy(StrategyLockForUpdate) with it returns ErrLockStrategyUnsupported.
// A worker pool still runs, but write operations serialize; throughput is
// bounded by the single writer.
//
// # Connection configuration (important)
//
// Because writers serialize, concurrent consumers (multiple pollers, or a worker
// pool acking/nacking in parallel) MUST use WAL journal mode and a non-zero
// busy_timeout, or they fail with SQLITE_BUSY under contention. Use DSN to build
// a connection string with those defaults, e.g.:
//
//	dsn := sqlite.DSN("/var/lib/app/msgin.db")
//	// file:/var/lib/app/msgin.db?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)
//	db, err := sql.Open("sqlite", dsn) // caller imports modernc.org/sqlite
//	src, err := msginsql.NewPollingSource(db, "queue", sqlite.LeaseDialect())
//
// # Schema
//
// Timestamp columns are INTEGER epoch microseconds written from SQLite's own
// clock. Provision the schema with the reference DDL (see DDL / InboxDDL) or
// EnsureSchema; msgin never runs DDL implicitly on the production path.
package sqlite
