# HANDOVER — msgin

> **Next session: read this first, then trust the referenced files over any memory.** Read, in order:
> `CLAUDE.md`, then the **audited, implementation-ready** design bundle for the latest increment —
> `docs/specs/007-persistent-queue-channel.md`, `docs/adrs/0018-persistent-queue-channel.md`, and
> `docs/plans/013-persistent-queue-channel.md`. **Plan 013 is DONE and MERGED to `main`** (merge `26f16b8`,
> `--no-ff`; pushed; `feat/persistent-queue-channel` deleted, was local-only). Start the NEXT increment from a
> fresh branch off `main`. The SDD progress ledger `.superpowers/sdd/progress.md` (gitignored, local) holds
> per-task history; trust it + `git log` over memory.

## Latest increment — Plan 013: persistent queue channel + `ChannelStore` SPI (2026-07-20, MERGED)

**What shipped.** A pollable, point-to-point **`QueueChannel`** (EIP Point-to-Point Channel + Guaranteed
Delivery; Spring Integration `QueueChannel`) whose buffer is a swappable **`ChannelStore`** — **no core runtime
change**, no new dependency.

- **Core (`msgin`)** — `store.go`: the narrow `ChannelStore` SPI (`Enqueue` / `Claim(ctx,max) ([]Delivery,error)` /
  `EmitsLiveValue`; `Claim` reuses the existing `msgin.Delivery`, no new type). `queuechannel.go`:
  `QueueChannel` + `NewQueueChannel(store)` implementing `OutboundAdapter`(Send→Enqueue) + `PollingSource`
  (Poll→Claim) + `LiveValueSource` + `NativeReliability` (forwarded to the store). Starts no goroutine. New
  sentinels `ErrNilStore` / `ErrInvalidCapacity`; `ErrOverflowDropped` godoc corrected.
- **In-memory store (`adapter/memory`)** — `memory.QueueStore` / `NewQueueStore(opts ...QueueStoreOption)`: bounded
  FIFO, ctx-aware **Block** backpressure via a counting semaphore, timestamp-based (`visibleAt`) delayed requeue
  (**no timer, no goroutine — goleak-clean**), epoch-fenced settlement (no double-settle). Options `WithCapacity`
  (default 1024) / `WithOverflow` (reuses `msgin.OverflowPolicy`; Block default, Reject→`ErrOverflowDropped`,
  DropNewest/DropOldest) / `WithClock`. Guarantee: at-least-once **within the process**; lost on exit.
  (Option type is `QueueStoreOption`, not `Option`, to avoid colliding with the existing `Broker`'s `Option`.)
- **Durable store (`adapter/database/sql`)** — `sql.QueueStore` / `NewQueueStore(db, table, dialect, opts...)`:
  a thin facade pairing the existing `Outbound` (Enqueue=INSERT) + `Source` (Claim=lease/claim) over one table;
  forwards `NativeReliability`/`Ready`/`EnsureSchema`; `EmitsLiveValue()=false` (wire). Guarantee: at-least-once
  across restarts/crashes. Conformance runner `harness.RunQueueStore` wired into the `dbtest` PG/MySQL/MariaDB/
  SQLite conformance suites.

**Key resolved decisions** (see spec/ADR): O7-2 → narrow `ChannelStore`-only SPI (no id-addressable `MessageStore`
base — zero consumers today; embedding makes a later add non-breaking). O7-4 → keep `sql.QueueStore` (the only
`ChannelStore` a durable channel can use). O7-5 → reuse `msgin.Delivery` (no `Claimed` type). D6 → capacity/overflow
live on the memory store, not the channel.

**Process & gate.** Design **audited 2 rounds → SOUND**. Implemented via `superpowers:subagent-driven-development`
(4 tasks: core SPI+channel `b0cfb0a`; memory store `c82a4a4`; e2e+Example `3ec81f1`; sql facade `10cd787`), each
task-reviewed (Task 2 concurrency reviewed on Opus). **Whole-branch gate PASSED:** `/code-review` (5 Minors all
FIXED, `59a2007` — a concurrent `-race` stress test, `min()` alloc cap, tail-clear memory hygiene, `native()`
dedup) + `/security-review` (**no findings**); `go test ./... -race` green; **dbtest conformance green on
PG/MySQL/MariaDB/SQLite** (testcontainers, Docker); `golangci-lint` 0 issues; `govulncheck` clean; `CGO_ENABLED=0`
build ok; `go vet`/`gofmt`/`go mod tidy` clean in all 3 modules. New exported surface is additive → minor SemVer.

**Exact state.** `main` @ merge `26f16b8`. Working tree: only `.claude/settings.json` is modified (pre-existing,
unrelated, intentionally never staged). Nothing else uncommitted.

## Roadmap / next actions

- **Spec 006 O6-1 — Redis/etcd-backed cron coordinators** (`Elector`/`Locker` as optional modules) remains the
  main open item from the prior increment.
- **Spec 007 future (non-breaking, deferred):** other `ChannelStore` backends (Redis/pgx/NATS) via the same SPI
  (O7-1); a priority channel (O7-3); the id-addressable `MessageStore` base when a real consumer (Claim Check /
  Aggregator) lands.
- Other long-standing deferrals: EIP Delayer, `memory` delayed-send, pgx/redis/nats/http adapters, Plan 005 T11.

## Prior increments (for reference)

- **Plan 012 — `cron.WithSeconds()`** (opt-in 6-field seconds schedule): DONE and MERGED (merge `d315f52`).
  Closes Spec 006 O6-2/N4 (D13); ADR 0017 addendum.
- **Plan 011 — cron / recurring source + distributed coordination**: DONE and MERGED (merge `c62da27`).
  `adapter/cron` Source[T] + Elector/Locker + SQL-backed coordinators + `crontest` leaf module. Spec 006, ADR
  0016/0017.

_Updated 2026-07-20: Plan 013 (persistent queue channel) merged to `main` (`26f16b8`)._
