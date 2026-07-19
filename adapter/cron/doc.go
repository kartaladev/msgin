// Package cron is the msgin channel adapter that ORIGINATES messages on a
// recurring / cron schedule — the Enterprise Integration Scheduled Producer /
// Polling Consumer shape. A Source[T] fires on a wall-clock schedule (standard
// 5-field cron, "@every <duration>" intervals, or @daily/@hourly/... descriptors,
// parsed by robfig/cron/v3) and emits a caller-defined message into a flow that
// the existing runtime (retry/DLQ/flow-control/graceful shutdown) then carries.
//
// # Import alias
//
// The package is named cron and collides with github.com/robfig/cron/v3, which
// this package uses internally. Callers importing both should alias one; most
// callers need only this package:
//
//	import "github.com/kartaladev/msgin/adapter/cron"
//
// # Timezone
//
// WithLocation sets the zone the schedule is evaluated in (default UTC). A
// spec-embedded "CRON_TZ=..."/"TZ=..." prefix OVERRIDES WithLocation for that
// spec; "@every" intervals ignore location entirely (they are relative
// durations, not wall-clock instants).
//
// # Delivery guarantee
//
// At-most-once. A fire is an ephemeral trigger, not a durable row: Ack/Nack are
// no-ops, and a fire missed because a slow handler stalled the loop past its
// instant is SKIPPED, not queued (standard cron overrun behavior — no stampede
// after a pause). A transient handler failure is still retried in-process by the
// runtime's RetryPolicy; a permanent failure routes to the invalid/DLQ sink. A
// syntactically valid schedule with no future occurrence (e.g. "0 0 30 2 *") is
// refused at construction (ErrInvalidSchedule), never left to hot-loop.
//
// # Multi-instance single-fire
//
// With NO coordinator, N replicas each fire on every tick (N-fold — a documented
// footgun). For single-fire across replicas, configure exactly one of:
//
//   - WithLocker: a per-fire claim; exactly one instance wins each (scope, fire).
//     The SQL-backed SQLLocker reuses the InboxDeduper dedup-INSERT and has NO
//     failover gap. RECOMMENDED for GRID-ALIGNED schedules — standard 5-field
//     cron and @daily/@hourly/... descriptors. It is UNSUPPORTED for "@every"
//     schedules (ErrLockerRequiresGridSchedule at construction): "@every"'s next
//     fire is computed relative to each instance's own last-fire/start time, so
//     independent instances never converge on the same dedup key.
//   - WithElector: leader election; only the leader fires. The SQL-backed
//     SQLElector is an on-demand leader-lease, scoped per call so one instance
//     can gate many schedules; its failover latency is bounded by WithLeaseTTL.
//     REQUIRED for "@every" schedules; also usable for grid-aligned schedules
//     when the TTL failover gap is acceptable.
//
// Both SQL coordinators are dependency-free (database/sql, driver injected;
// PostgreSQL/MySQL/SQLite dialects) and DB-server-clock based (skew-free) and
// require an autocommitting Querier (*sql.DB, not *sql.Tx). A coordinator error
// skips the fire FAIL-SAFE — a coordination outage degrades to no fire, never to
// N-fold firing.
package cron
