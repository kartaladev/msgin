# Spec 007 — Persistent Queue Channel + `ChannelStore` SPI

- **Status:** Approved by the user (2026-07-20). **O7-2 resolved narrow** (ChannelStore-only, no id-addressable
  base); O7-4/O7-5 resolved. **Both adversarial audit rounds complete (2-round norm) — bundle SOUND**; round 1
  (NEEDS-REVISION) findings folded into Plan 013 + ADR 0018, round 2 verified every fix and the rewritten material.
  Awaiting the explicit user go-ahead to implement (CLAUDE.md design-time gate). Brainstormed 2026-07-19.
- **Governing product spec:** [Spec 001 — Messaging core](001-messaging-core.md) §3 (the two encodings), §7 (the
  runtime / `PollingSource` path), and the inbound/outbound adapter SPI ([ADR 0002](../adrs/0002-adapter-spi.md)).
- **Records decisions in:** [ADR 0018 — Persistent queue channel + store SPI](../adrs/0018-persistent-queue-channel.md)
  (planned). (No ADR 0019 — the SQL store is a thin facade recorded in ADR 0018 §3; O7-4 resolved.)
- **Realized by:** [Plan 013 — Persistent queue channel](../plans/013-persistent-queue-channel.md) (planned).

## 1. Motivation

msgin can bridge *external* systems durably (the `sql` adapter's `Outbound`+`Source` over one table is already a
durable, at-least-once produce/consume queue — see `outbound.go`), and it has an in-process point-to-point transport
(`adapter/memory`'s `Broker`, an at-most-once `StreamingSource`). What it lacks is a **first-class in-flow queue
channel** — an EIP **Point-to-Point Channel** that a producer/endpoint sends into and a consumer drains, whose
**buffer is a swappable store**: fast/in-memory by default, or persistent when that flow segment must survive a
restart (**Guaranteed Delivery**).

This is the Spring Integration `QueueChannel` + `message-store` shape, re-expressed idiomatically in Go. A
`QueueChannel` is a **pollable channel** (Spring's `PollableChannel`): you attach a poller/consumer that drains it —
which maps *exactly* onto msgin's existing `PollingSource` + `Poller`/worker-pool/retry/dead-letter/backpressure
machinery. No core runtime change is needed; the channel is a `PollingSource` on the read side and an
`OutboundAdapter` on the write side, and the **store** is the seam that makes the buffer memory-or-durable.

The user's driver: **async decoupling AND crash-durability are both first-class** — in-memory for speed, a
persistent store when the segment must survive a restart.

## 2. Goals / Non-goals

### Goals

- **G1. A pollable `QueueChannel`** (core module): implements `msgin.OutboundAdapter` (`Send` = durable **enqueue**)
  **and** `msgin.PollingSource` (`Poll` = **claim** ≤`max` settleable deliveries). Drained verbatim by the existing
  `NewConsumer`/`Poller`/worker-pool/retry/DLQ/backpressure — **no core runtime change**.
- **G2. A store SPI** as the swap seam: a narrow **`ChannelStore`** (FIFO enqueue/claim/settle) — exactly what the
  queue exercises. Contributors implement this one interface to add a new backend without touching the core. It is
  *shaped* (via Go interface embedding, à la Spring's `MessageStore` → `ChannelMessageStore` hierarchy) so a future
  id-addressable `MessageStore` base or `MessageGroupStore` can be added **non-breakingly** when a real consumer
  (Claim Check, Aggregator) arrives — but none of that ships now (O7-2 resolved narrow).
- **G3. Two shipped stores.** (a) An **in-memory `ChannelStore`** (core module) — bounded, poll/claim/ack, overflow
  policy; the fast default. (b) A **SQL `ChannelStore`** (`adapter/database/sql`, PG/MySQL/SQLite) that **reuses the
  existing `Outbound`+`Source` lease/claim/framing machinery** (ADR 0010/0011) — durable, at-least-once across
  crashes: the actual Guaranteed Delivery.
- **G4. Safe, opinionated, overridable defaults** — bounded in-memory capacity (no unbounded-growth footgun),
  `Block` overflow (backpressure) by default, FIFO ordering, delivery guarantees documented per store — all
  `WithX`-overridable.
- **G5. Production-robust:** goleak-clean (the channel starts no goroutine of its own; the `Consumer` owns it),
  graceful shutdown via the existing drain path, `-race` clean, injected `*slog.Logger`, typed errors on the
  debuggability surface.

### Non-goals

- **N1. No core runtime change.** `consumer.go`/`poller.go`/`flowcontrol.go`/`spi.go` are untouched. `QueueChannel`
  satisfies the *existing* `OutboundAdapter`+`PollingSource` contracts.
- **N2. Not a `Subscribe`-based channel.** A `QueueChannel` is a **pollable** channel (Spring `PollableChannel`),
  not a `SubscribableChannel`. It deliberately does **not** implement the `Subscribe`-based `msgin.MessageChannel`
  (that is `DirectChannel`'s synchronous push model). You attach a `Consumer`, you do not `Subscribe`.
- **N3. No priority channel** this increment. FIFO only. A priority-ordered variant (Spring's `PriorityChannel`) is
  a future, non-breaking addition (O7-3).
- **N4. No Redis/other-backend store** this increment. Only in-memory + SQL. Redis/NATS/pgx `ChannelStore`s are
  future optional modules (O7-1), enabled by the same SPI.
- **N5. Not a new durability mechanism.** The SQL store **reuses** the existing `sql` adapter's proven table + lease
  + framing; it does not introduce a second, parallel persistence scheme.
- **N6. No general aggregator/claim-check functionality, and no id-addressable `MessageStore` base** here. The
  `ChannelStore` SPI is *shaped* so those can be added later as new embedding interfaces (G2), but this increment
  ships only the narrow queue seam — see D3 and O7-2 (resolved narrow).

## 3. Decisions (settled with the user, 2026-07-19)

- **D1 — `QueueChannel` = `OutboundAdapter` + `PollingSource` (the pollable-channel model).** One core type wraps a
  `ChannelStore`. `Send(ctx, msg)` enqueues (the store applies any overflow policy); `Poll(ctx, max)` forwards to
  `store.Claim`, which returns ≤`max` messages already as `[]msgin.Delivery`, each `Ack` = remove-from-store,
  `Nack(requeue, delay)` = return/redeliver.
  The existing `Poller` drives it — credit-at-fetch backpressure, retry, dead-letter, graceful drain all apply
  unchanged. The channel starts **no goroutine of its own**; `Send` is a synchronous enqueue, so goleak-cleanliness
  is inherited from the `Consumer`. Usage: `NewProducer[T](qc)` on the write side, `NewConsumer[T](qc, handler)` on
  the read side (or any endpoint `Send`s into `qc` as an `OutboundAdapter`).

- **D2 — Narrow store SPI: a single `ChannelStore` (queue seam).** No id-addressable base ships now (D3/O7-2). The
  SPI is exactly what the pollable channel exercises:
  ```go
  // ChannelStore is the swappable buffer behind a QueueChannel: a durable FIFO
  // with lease/claim settlement. It is the sole store SPI shipped in this
  // increment. A future id-addressable MessageStore or MessageGroupStore can be
  // ADDED as a separate interface that ChannelStore embeds, when a real consumer
  // (Claim Check / Aggregator) lands — a non-breaking change for existing
  // implementers, which is why narrow-now is safe.
  //
  // Claim returns the EXISTING msgin.Delivery (O7-5, resolved in planning) — no
  // new Claimed type — so the SQL store's Claim is literally Source.Poll and the
  // channel's Poll is a passthrough.
  type ChannelStore interface {
      Enqueue(ctx context.Context, msg msgin.Message[any]) error           // durable FIFO append
      Claim(ctx context.Context, max int) ([]msgin.Delivery, error)        // lease next ≤max, FIFO
      // Does this store carry live Go values (in-memory) or []byte (wire)?
      // Mirrored on the channel to drive NewConsumer/NewProducer codec pairing.
      EmitsLiveValue() bool
  }
  ```
  Exact method set ratified in ADR 0018. Reusing `msgin.Delivery` (rather than a parallel `Claimed` struct) removes
  a type and lets the SQL store hand the sql `Source`'s deliveries straight through.

- **D3 — Narrow SPI now; id-addressable base deferred to its first real consumer (O7-2, resolved 2026-07-20).** The
  queue path needs only `Enqueue`/`Claim`/`Ack`/`Nack`. Grounding the design in the code showed the id-addressable
  `Get`/`Remove`/`Count` base would have **no in-library consumer** (only tests + a hypothetical future Claim
  Check) — speculative surface against CLAUDE.md's ruthless-YAGNI + the "small, stable public surface" library
  gate. The user (who had initially leaned broad) chose **narrow** at spec review given this evidence. Because Go
  interfaces compose by *embedding*, the base can be added later as a new interface `ChannelStore` embeds **without
  breaking existing implementers**, so nothing is lost by waiting. ADR 0018 records this.

- **D4 — In-memory `ChannelStore` is genuinely new (not the existing `Broker`).** `adapter/memory`'s `Broker` is a
  `StreamingSource` with **at-most-once**, no claim/ack settlement — it does not fit the pollable claim/ack model.
  The new in-memory store is poll-based: a bounded FIFO with an in-flight (claimed-but-unsettled) set, `Ack` removes,
  `Nack(requeue,delay)` returns after `delay` (via `clockwork` for testability). Carries **live values** (no codec,
  `EmitsLiveValue()→true`, like `Broker`). Delivery guarantee: **at-least-once within the process lifetime**;
  buffered/in-flight messages are **lost on process exit** (at-most-once across a restart) — documented on the
  constructor. An optional lease-TTL reclaim of a claim whose settlement never arrives is a defensive nicety, not
  required for correctness within one process (confirm scope in ADR 0018 — lean YAGNI: omit unless the audit shows a
  real drop path).

- **D5 — SQL store (`sql.QueueStore`) reuses the existing `sql` adapter wholesale.** (Named `QueueStore` to mirror
  `memory.QueueStore` and avoid shadowing the `ChannelStore` interface name — audit H-2.) `Enqueue` = `Outbound.Send` (INSERT,
  immediately visible); `Claim` = `Source.Poll` (lease/claim, `FOR UPDATE SKIP LOCKED` or the lease strategy);
  `Ack`/`Nack` = the sql `Source`'s fenced settlement closures verbatim (delete-on-ack, requeue-with-delay-on-nack,
  `ErrStaleLease` on fence miss). Carries **`[]byte`** (`EmitsLiveValue()→false`); the paired `Producer` JSON-encodes
  `T`, exactly as with `sql.Outbound` today. Dialects: PG/MySQL/SQLite via the existing `LeaseDialect` seam.
  Delivery guarantee: **at-least-once across restarts/crashes** (the actual Guaranteed Delivery). With the narrow
  SPI (D3), there is **no genuinely-new SQL** — the store is pure reuse, so it may be a thin `struct{ *Outbound;
  *Source }` facade rather than new machinery. Confirm in ADR 0018/0019 whether such a facade adds enough over the
  two existing types to be worth a separate exported type at all (O7-4/R1).

- **D6 — Capacity + overflow live on the in-memory store, bounded + `Block` by default (reuses the existing
  `OverflowPolicy`).** Capacity and overflow describe a bounded *buffer* — only the in-memory backend has one — so
  they are options on `memory.NewQueueStore`, **not** on the channel (the `QueueChannel` stays store-agnostic; a
  refinement settled in planning). `memory.WithCapacity(n)` bounds ready+in-flight; **default bounded 1024** —
  never unbounded (CLAUDE.md's no-unbounded-growth / fail-safe rule; an unbounded in-memory buffer is a documented
  OOM footgun); explicit `n<=0` → `ErrInvalidCapacity` (the `set`-flag pattern). `memory.WithOverflow(policy)`
  governs a full buffer at `Enqueue`, reusing the **existing** `msgin.OverflowPolicy` (`flowcontrol.go`, ADR 0006):
  **`OverflowBlock` (default — backpressure; `Enqueue` waits until a slot frees or `ctx` cancels)**,
  `OverflowReject`, `OverflowDropNewest`, `OverflowDropOldest`. The channel's real discrete buffer is where
  `DropOldest`/`Reject` finally get their **true** semantics. `Reject` returns the **existing**
  `msgin.ErrOverflowDropped` (no new `ErrQueueFull`); an unknown policy → `Block` (matching the runtime convention,
  so no `ErrInvalidOverflowPolicy`). For the **SQL** store, "capacity" is the DB's concern: **off/unbounded**
  (DB-bounded). Net new error surface: exactly `ErrNilStore` + `ErrInvalidCapacity`.

- **D7 — FIFO ordering; strict only at worker=1.** The store claims in enqueue order (FIFO). With the worker pool at
  `N>1` (Competing Consumers), strict global order is not guaranteed — identical to the existing sql `Source`
  semantics. Default worker=1 preserves order. Priority ordering is out of scope (N3/O7-3). Documented.

- **D8 — Codec pairing via `EmitsLiveValue()` delegation.** `QueueChannel` exposes `msgin.LiveValueSource` by
  delegating to its store's `EmitsLiveValue()`: memory-backed → live values (no codec); SQL-backed → `[]byte`
  (producer encodes). This is the exact mechanism `NewConsumer`/`NewProducer` already use to pair codecs (ADR 0001/
  0010 D8), so the two backends compose with the typed runtime with zero special-casing.

- **D9 — Construction-time validation, typed errors.** A nil store → `ErrNilStore` (channel); an invalid capacity
  (explicit `≤0`) → `ErrInvalidCapacity` (memory store). An unknown overflow policy is **not** an error — it falls
  back to `OverflowBlock`, matching the runtime convention (`flowcontrol.go`), so there is no
  `ErrInvalidOverflowPolicy`. The SQL store's constructor keeps the existing `sql` typed errors
  (`ErrNilAdapter`/`ErrNilDialect`/`ErrInvalidTableName`) and `Ready`/`EnsureSchema` boot checks. Every such branch
  is a hot-path/typed-error branch requiring a covering test.

## 4. Architecture

Units, dependency pointing inward (core runtime unchanged; the channel satisfies the existing SPI, the store is the
extension seam):

1. **Core — the store SPI** (`store.go`, package `msgin`): `ChannelStore` (D2) — a single pure interface returning
   `[]msgin.Delivery`. No new struct, no third-party import.
2. **Core — `QueueChannel`** (`queuechannel.go`, package `msgin`): wraps a `ChannelStore`; implements
   `OutboundAdapter`(`Send`→`Enqueue`), `PollingSource`(`Poll`→`Claim`, passthrough `[]Delivery`), `LiveValueSource`
   (delegates `EmitsLiveValue`); no options — `NewQueueChannel(store)`; `ErrNilStore`. Starts no goroutine.
3. **In-memory `ChannelStore`** (`adapter/memory`, `queuestore.go`): bounded FIFO + in-flight set + `clockwork`
   `visibleAt`-based `Nack` delay (no timer/goroutine); live values; `WithCapacity`/`WithOverflow`/`WithClock` +
   `ErrInvalidCapacity` (D4/D6). New `QueueStore` alongside the existing `Broker`.
4. **SQL store `sql.QueueStore`** (`adapter/database/sql`, `queuestore.go`): reuses `Outbound`(Enqueue) + `Source`
   (Claim/settle) over one table; forwards `NativeReliability`/`Ready`/`EnsureSchema`; PG/MySQL/SQLite via the
   existing `LeaseDialect` (D5). Thin facade (O7-4 resolved: kept as the only `ChannelStore` a durable channel can use).

Data flow (write→read): `Producer.Send(T)` → codec (iff wire) → `QueueChannel.Send(Message[any])` →
`ChannelStore.Enqueue` (durable) → … → `Poller` acquires credits → `QueueChannel.Poll(max)` →
`ChannelStore.Claim(max)` → `[]Delivery` → runtime ingest → handler → `Ack`(remove)/`Nack`(requeue). Shutdown:
`ctx` cancel → `Poll` returns promptly; in-flight settled or nacked within the drain deadline (existing path).

## 5. Testing posture (per CLAUDE.md + custom skills)

- **Blackbox `_test` packages**; **assert-closure tables** (`table-test`); `t.Context()`; `gopls` for navigation/
  refactor; start each task from `cc-skills-golang:golang-how-to`; strict TDD (red→green→refactor). Injectable time
  via **`clockwork.NewFakeClock()`** for `Nack` delay / any lease TTL — no first-party clock. Mocks (if any store
  seam is mocked) via **`use-mockgen`** (`--typed`, alongside the interface).
- **`QueueChannel` unit tests** (against a fake `ChannelStore`): `Send`→`Enqueue`; `Poll`→`Claim` passthrough,
  ≤`max` FIFO; `Poll` surfaces a store error with no deliveries (invariant 2); `LiveValueSource` delegates;
  `ErrNilStore`. **In-memory store tests** (the buffer behavior): `Ack` removes (not re-claimed); `Nack(requeue=true,
  delay)` re-appears only after `clock.Advance(delay)`; `Nack(requeue=false)` drops; overflow — `Block` blocks until
  a claim/Ack frees a slot or `ctx` cancels, `Reject`→`ErrOverflowDropped`, `DropNewest`/`DropOldest` drop the right
  end; `ErrInvalidCapacity`. The three `PollingSource` invariants (≤`max`; no deliveries with a non-nil error; own
  cleanup on error/cancel) explicitly asserted.
- **End-to-end** through `NewProducer[T]` + `NewConsumer[T]` + `Run` over the in-memory store (fake clock, a handler
  recording payloads, **`goleak`** on shutdown) — proves the `T`→`Message[any]`→enqueue→claim→handler `T` round-trip
  and clean drain, not just direct `Poll`.
- **SQL `ChannelStore` conformance via testcontainers** (real PG/MySQL/MariaDB/SQLite — `use-testcontainers`, the
  existing `database.RunTestDatabase`/`harness` split): enqueue→claim→ack round-trip is durable; a nacked/lease-
  expired row is redelivered; two concurrent consumers get disjoint claims (competing consumers). The dialect SQL
  is exercised through the reused `Outbound`/`Source` paths and is covered **only** by this real-DB run (mirrors
  how `adapter/database/sql`'s dialect SQL is covered only by `dbtest`); CI MUST run it.
- **Coverage** ≥85% on every changed package; **every hot-path/typed-error branch covered** — enqueue, claim, ack,
  nack, each overflow policy, each construction-time typed error, the `EmitsLiveValue` delegation, and the fence-
  miss/`ErrStaleLease` path inherited from the sql `Source`.
- **No goroutine leaks** — the channel starts none; the in-memory store starts none (all work is caller-goroutine
  under a mutex + condition/`clockwork` timer that is created and stopped within the call). `-race` clean.
- **`Example…` tests** double as godoc: a memory-backed `QueueChannel` and a note pointing at the sql-backed variant.

## 6. Sequencing (the plan may split this)

1. **ADR 0018** + the core **store SPI** (`ChannelStore`) + **`QueueChannel`** (Send/Poll/
   overflow/codec-delegation/typed errors) + the **in-memory `ChannelStore`** — all fake-clock/goleak unit-tested.
   The default, DB-free path ships and is provable end-to-end here.
2. **SQL `ChannelStore`** (pure reuse of `Outbound`+`Source`; PG/MySQL/SQLite) — testcontainers conformance.
   Proves the durability half.
3. End-to-end example + package docs + whole-branch gate (`/code-review` + `/security-review` over `main..HEAD`,
   `-race`, coverage, `go mod tidy`/`vet`/lint/`govulncheck`, `gorelease`/apidiff for the new exported surface).

Each phase is a green, committable unit (per-task commits pre-authorized once the plan is approved).

## 7. Risks

- **R1 — SQL store overlaps the existing `sql` adapter almost totally (D5).** If it is *pure* reuse, a separate
  `ChannelStore` type risks being redundant sugar over `Outbound`+`Source`. Mitigation: scope it as a thin facade;
  the *value* is the uniform `ChannelStore` seam that lets a flow swap memory↔durable without rewiring. If the audit
  finds it adds nothing over the two existing types, drop the SQL `ChannelStore` and document "use `Outbound`+
  `Source` for a durable queue" — confirm in ADR 0018/0019.
- **R2 — (Resolved) id-addressable base scope.** Resolved narrow at spec review (D3/O7-2): no `MessageStore` base
  ships; only `ChannelStore`. Embedding keeps a later addition (with Claim Check) non-breaking, so the risk of
  premature surface is retired.
- **R3 — In-memory `Block` overflow + a slow/absent consumer deadlocks the producer.** `Send` blocking forever is
  the intended backpressure, but a misconfigured flow (no consumer attached) looks like a hang. Mitigation:
  `Send` always honors `ctx` cancellation; document that a `QueueChannel` needs an attached `Consumer`; `Reject`/
  `Drop` policies exist for callers that must not block.
- **R4 — Ordering under `N>1` workers (D7).** FIFO is best-effort once competing consumers run — a known,
  documented semantic (same as sql `Source`), but a footgun for callers assuming strict order. Mitigation: default
  worker=1; document prominently.
- **R5 — Increment size.** Core SPI + channel + in-memory store + SQL store × 3 dialects. Mitigation: §6 phases it;
  Phase 1 (DB-free) is independently shippable and valuable; the plan may split Phase 2 into its own increment.
- **R6 — (Resolved) `Delivery` vs `Claimed`.** Resolved in planning (O7-5): `ChannelStore.Claim` reuses
  `msgin.Delivery` directly; no `Claimed` type ships. One fewer exported type; the SQL store's `Claim` is literally
  `Source.Poll`.

## 8. Open items (to close in the ADR + at the spec-review gate)

- **O7-1 — Redis/pgx/NATS `ChannelStore`.** Future optional-module backends via the same SPI. Deferred (N4).
- **O7-2 — (RESOLVED 2026-07-20) Ship the id-addressable `MessageStore` base now, or with Claim Check? (D3).**
  Resolved **narrow now** (ChannelStore-only); broaden with the first real consumer. See D3.
- **O7-3 — Priority channel.** Spring's `PriorityChannel` equivalent. Future, non-breaking (N3).
- **O7-4 — (RESOLVED by audit, 2026-07-20) Does the SQL store warrant its own type?** Yes — kept as `sql.QueueStore`
  (a thin facade). It is the *only* `ChannelStore` a durable `QueueChannel` can be backed by (neither `Source` nor
  `Outbound` is a `ChannelStore`), so it earns its ~40 lines as the swap adapter. No separate ADR 0019 needed;
  recorded in ADR 0018 §3.
- **O7-5 — (RESOLVED in planning) `Claimed` vs reusing `msgin.Delivery`.** Resolved: **reuse `msgin.Delivery`**;
  no `Claimed` type. See D2 / R6.
- **O7-6 — In-memory store lease-TTL reclaim (D4).** Include only if the audit shows a real in-process drop path;
  else omit (YAGNI).

## 9. Traceability

- **Realized by:** Plan 013 → `Spec: 007` / `Plan: 013` / `ADR: 0018[,0019]` commit trailers.
- **Records decisions in:** ADR 0018 (channel + store SPI + SQL facade; no separate ADR 0019 — O7-4 resolved).
- **Builds on:** ADR 0001 (payload typing / codec pairing via `LiveValueSource`), ADR 0002 (`OutboundAdapter`/
  `PollingSource`/`Delivery` SPI), ADR 0006 (overflow-policy vocabulary), ADR 0010/0011 (sql adapter lease/claim/
  framing + dialect seam reused by the SQL store).
- **Governing product spec:** Spec 001 §3, §7.
