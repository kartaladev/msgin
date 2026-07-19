# ADR 0018 — Persistent queue channel + `ChannelStore` SPI

- **Status:** Proposed (2026-07-20) — **both adversarial audit rounds complete (the 2-round norm); bundle SOUND.**
  Round 1 (NEEDS-REVISION): B-1..B-4, H-1/H-2, M-1..M-3, L-1..L-3 + all placeholders folded. Round 2 verified every
  fix VERIFIED-FIXED and the rewritten material sound (shed token-accounting, `NativeReliability` forwarding with no
  runtime regression, memory-store concurrency, e2e determinism, SQL-facade option-inertness); the only residual
  (two stale `sql.ChannelStore` header refs in Plan 013) is fixed. Spec approved by the user 2026-07-20; O7-2
  resolved **narrow**; O7-4/O7-5 resolved. **Gated only on an explicit user go-ahead before implementation.**
- **Spec:** [Spec 007 — Persistent queue channel + `ChannelStore` SPI](../specs/007-persistent-queue-channel.md).
- **Plan:** [Plan 013 — Persistent queue channel](../plans/013-persistent-queue-channel.md) (planned).
- **Depends on:** [ADR 0001](0001-message-payload-typing.md) (payload typing; codec pairing via `LiveValueSource`),
  [ADR 0002](0002-adapter-spi.md) (`OutboundAdapter`/`PollingSource`/`Delivery` inbound-outbound SPI),
  [ADR 0006](0006-resilience-flow-control.md) (overflow-policy vocabulary), [ADR 0010](0010-poller-sql-adapter.md)
  and [ADR 0011](0011-sql-engine-dialect-module-split.md) (the sql adapter lease/claim/framing + dialect seam the
  SQL store reuses).

## Context

msgin has durable *external* messaging (the sql adapter's `Outbound`+`Source` over one table already form a
durable, at-least-once produce/consume queue) and an in-process at-most-once transport (`adapter/memory`'s
`Broker`, a `StreamingSource`). It has **no first-class in-flow queue channel**: an EIP **Point-to-Point Channel**
that a producer/endpoint sends into and a consumer drains, whose **buffer is a swappable store** — fast/in-memory
by default, persistent when that flow segment must survive a restart (**Guaranteed Delivery**). This is Spring
Integration's `QueueChannel` + `message-store`, which is a **pollable** channel (`PollableChannel`): you attach a
poller, you do not `Subscribe`.

The user wants async decoupling **and** crash-durability both first-class, with the persistence backend opt-in per
use case. The shaping problems: (1) how the channel integrates without a core runtime change; (2) what the
store-swap SPI is and how narrow; (3) how the two backends (memory, SQL) map onto existing machinery without
duplication; (4) safe defaults for capacity/overflow/ordering/guarantees.

## Decision

Ship a core `QueueChannel` + a narrow `ChannelStore` SPI + two stores (in-memory in core, SQL by reuse). **No core
runtime change** — the channel satisfies the *existing* `OutboundAdapter`+`PollingSource` contracts.

### 1. `QueueChannel` — a pollable channel = `OutboundAdapter` + `PollingSource` (Spec D1)

- Constructed over a `ChannelStore`: `msgin.NewQueueChannel(store ChannelStore) (*QueueChannel, error)` — no
  options; capacity/overflow belong to the store (part 4).
- Implements `msgin.OutboundAdapter` — `Send(ctx, msg)` calls `store.Enqueue`; the store applies any bounded-buffer
  overflow policy (part 4). Structurally identical to any outbound adapter, so `NewProducer[T](qc)` and any upstream
  endpoint can send into it.
- Implements `msgin.PollingSource` — `Poll(ctx, max)` forwards to `store.Claim(max)`, which returns ready messages
  **already as `[]msgin.Delivery`** (Ack = remove-from-store, Nack(requeue,delay) = return/redeliver). `Claim`
  reuses `msgin.Delivery` directly — there is no separate `Claimed` type (Spec O7-5, resolved during planning) — so
  `Poll` is a pure passthrough. The existing `Poller`/worker-pool/`RetryPolicy`/dead-letter/credit-backpressure
  drive it **verbatim**. The three `PollingSource` invariants (≤`max`; never deliveries alongside a non-nil error;
  own cleanup of claimed work on the error/cancel path) are the store's `Claim` contract.
- Implements `msgin.LiveValueSource` by **delegating** `EmitsLiveValue()` to the store (part 3) — memory-backed →
  live values (no codec), SQL-backed → `[]byte` (producer encodes). This is the exact mechanism `NewConsumer`/
  `NewProducer` already use to pair codecs (ADR 0001/0010 D8), so both backends compose with the typed runtime with
  zero special-casing.
- Implements `msgin.NativeReliability` by **forwarding** to the store when the store implements it (the SQL store
  does, via its embedded `Source`), else `false`/`false`. Without this, a durable SQL-backed channel would silently
  report *no* native reliability, so a future store whose `NativeDeadLetter()` is true could be double dead-lettered
  by the runtime; forwarding keeps the channel behaviorally identical to its backing `Source` (audit M-2). It is a
  no-op today for the memory store (not native-reliable → `false`/`false`, identical to the runtime's default).
- **It is NOT a `Subscribe`-based `msgin.MessageChannel`** (Spec N2). A `QueueChannel` is pollable (Spring
  `PollableChannel`); `DirectChannel` remains the synchronous `Subscribe` push model. You attach a `Consumer`.
- **Starts no goroutine of its own.** `Send` is a synchronous enqueue; `Poll` is a synchronous claim. Goroutine
  ownership and graceful-drain stay entirely in the `Consumer`/`Poller`, so the channel is goleak-clean by
  construction.

### 2. The store SPI — a single narrow `ChannelStore` (Spec D2/D3, O7-2 resolved narrow)

```go
// ChannelStore is the swappable buffer behind a QueueChannel: a durable FIFO
// with lease/claim settlement. Sole store SPI shipped this increment. Claim
// returns the EXISTING msgin.Delivery (no new Claimed type — Spec O7-5), so the
// SQL store's Claim is literally Source.Poll and the channel's Poll is a
// passthrough.
type ChannelStore interface {
    Enqueue(ctx context.Context, msg msgin.Message[any]) error     // durable FIFO append
    Claim(ctx context.Context, max int) ([]msgin.Delivery, error)  // lease next ≤max, FIFO
    EmitsLiveValue() bool                                          // live Go values vs []byte
}
```

**Narrow by decision, not omission.** The queue path exercises only `Enqueue`/`Claim`/`Ack`/`Nack`. An
id-addressable `MessageStore` base (`Get`/`Remove`/`Count`) — Spring's general `MessageStore` — would have **no
in-library consumer** today (only tests + a hypothetical future Claim Check), so it is deferred (Spec D3). Because
Go interfaces compose by **embedding**, that base (and a `MessageGroupStore` for Aggregator/Resequencer) can later
be added as a *new* interface that `ChannelStore` embeds — **non-breaking** for existing implementers. Shipping it
now would be speculative surface against the ruthless-YAGNI + small-public-API library gates; waiting costs nothing.
(O7-5 resolved during planning: `Claim` reuses `msgin.Delivery` directly — no `Claimed` type.)

### 3. Two backends, no duplication

- **In-memory `ChannelStore`** (`adapter/memory`, new — Spec D4). The existing `Broker` is a `StreamingSource` with
  at-most-once and *no* claim/ack, so it does not fit the pollable model — the memory store is **genuinely new**, not
  a wrapper. A bounded FIFO + an in-flight (claimed-but-unsettled) set; `Ack` drops the in-flight entry, `Nack(
  requeue=true, delay)` returns it to the queue after `delay` (via injected `clockwork.Clock`, testable),
  `Nack(requeue=false)` per the documented store contract. Carries **live values** (`EmitsLiveValue()→true`).
  **Delivery guarantee: at-least-once within the process lifetime; buffered/in-flight messages are lost on process
  exit** (at-most-once across a restart) — documented on the constructor. No goroutine; all work is caller-goroutine
  under a mutex + a `clockwork` timer created and stopped within the call.
- **SQL store `sql.QueueStore`** (`adapter/database/sql`, by reuse — Spec D5; named `QueueStore` to mirror
  `memory.QueueStore` and not shadow the `ChannelStore` interface, audit H-2). `Enqueue` = `Outbound.Send` (INSERT,
  immediately visible); `Claim` = `Source.Poll` (lease/claim, fenced Ack/Nack, `ErrStaleLease` on fence miss);
  carries **`[]byte`** (`EmitsLiveValue()→false`); **forwards** `NativeReliability`/`Ready`/`EnsureSchema` from the
  embedded `Source` (explicit forwarders — named fields do not promote, audit B-3/M-2). Dialects PG/MySQL/SQLite via
  the existing `LeaseDialect`. **No new durability code** — a thin `struct{ out *Outbound; src *Source }` facade.
  **Delivery guarantee: at-least-once across restarts/crashes** (Guaranteed Delivery). **O7-4 resolved (audit):
  keep it** — it is the *only* `ChannelStore` a durable `QueueChannel` can be backed by (neither `Source` nor
  `Outbound` is a `ChannelStore`), so it earns its ~40 lines as the swap adapter; no separate ADR 0019.

### 4. Defaults: bounded, `Block` overflow, FIFO — options on the store (Spec D6/D7/D9)

Capacity and overflow describe a bounded *buffer*, which only the in-memory backend has — so they are options on the
**in-memory store constructor** (`memory.NewQueueStore`), not on the channel (a refinement over the earlier
`QueueOption` framing; the `QueueChannel` stays store-agnostic and thin). The SQL store is DB-bounded and has no such
knob.

- `memory.WithCapacity(n)` bounds ready+in-flight; **default bounded 1024** — never unbounded (CLAUDE.md
  no-unbounded-growth / fail-safe defaults; an unbounded in-memory buffer is a documented OOM lever). Explicit
  `n<=0` → `msgin.ErrInvalidCapacity`.
- `memory.WithOverflow(policy)` at a full buffer on `Enqueue`, reusing the **existing** `msgin.OverflowPolicy` +
  `OverflowBlock`/`OverflowDropNewest`/`OverflowDropOldest`/`OverflowReject` (ADR 0006, `flowcontrol.go`): **`Block`
  (default — backpressure; `Enqueue` waits for a slot or `ctx` cancel)**, `Reject`/`DropNewest`/`DropOldest`. The
  `QueueChannel`'s discrete buffer is where `DropOldest`/`Reject` finally get their **true** semantics (on a
  streaming source they degrade to `DropNewest`). `Reject` returns the **existing** `msgin.ErrOverflowDropped` (no
  new `ErrQueueFull`); an unknown policy falls back to `Block` (matching the runtime convention — so no
  `ErrInvalidOverflowPolicy` either). This keeps the added error surface to exactly two sentinels.
- **FIFO** ordering; strict only at worker=1 (N>1 = Competing Consumers, same semantic as the sql `Source`). Priority
  ordering is out of scope (Spec N3). The capacity option uses the established `set`-flag pattern (unset → default
  1024; explicit invalid → `ErrInvalidCapacity`). New exported errors total two: `ErrNilStore`,
  `ErrInvalidCapacity` (the sql store keeps its existing `ErrNilAdapter`/`ErrNilDialect`/`ErrInvalidTableName`).

## Consequences

**Positive.**
- A first-class in-flow queue channel with **swappable durability** and **zero core runtime change** — the channel
  is just an `OutboundAdapter`+`PollingSource`, drained by the machinery that already exists. Goleak-clean by
  construction (starts no goroutine).
- A flow segment can be made durable by swapping the store, **without rewiring** the producer/consumer — the value
  of the uniform `ChannelStore` seam.
- **Narrow SPI** (one interface, one struct) keeps the public surface small and honors YAGNI, while embedding leaves
  the door open to a broader `MessageStore`/`MessageGroupStore` non-breakingly.
- The SQL backend adds no new durability code (reuse), and the in-memory backend fills a real gap (a pollable
  claim/ack in-process queue the `Broker` never provided).

**Negative / trade-offs.**
- **SQL store overlaps the existing sql adapter almost totally** — a distinct type is thin sugar over
  `Outbound`+`Source`. Accepted (O7-4 resolved: keep `sql.QueueStore`): it is the only `ChannelStore` a durable
  channel can use, and the seam's value is uniform memory↔durable swap without rewiring.
- **`Block` overflow + no attached consumer looks like a hang.** Intended backpressure; mitigated by `Send` always
  honoring `ctx` cancellation and by `Reject`/`Drop` policies for non-blocking callers. Documented.
- **FIFO is best-effort under N>1 workers** — a known, documented semantic (same as the sql `Source`); default
  worker=1 preserves order.
- **In-memory durability is process-scoped** — buffered messages are lost on crash; only the SQL store survives a
  restart. Documented per store so the guarantee is never implied.

**Neutral.**
- `Claim` reuses `msgin.Delivery` (O7-5 resolved) — one fewer exported type.
- Capacity/overflow reuse the existing `OverflowPolicy` + `ErrOverflowDropped`; the increment adds exactly two new
  error sentinels (`ErrNilStore`, `ErrInvalidCapacity`).
- Redis/pgx/NATS `ChannelStore`s and a priority channel are deferred, non-breaking additions via the same SPI
  (Spec O7-1/O7-3).

## Alternatives considered

- **`QueueChannel` owns an internal draining goroutine + `Subscribe` (push).** Rejected (Spec D1/N2): re-implements
  settlement/backpressure/shutdown the `Poller` already provides, adds a goroutine to own, and diverges from
  Spring's pollable-channel model.
- **Thin facade over the sql adapter only (no store SPI, no memory store).** Rejected: couples channels to the sql
  module, cannot be in-memory, and gives no swap seam — the user wants memory *and* durable behind one channel.
- **Broad id-addressable `MessageStore` base now** (the user's initial lean). Rejected at spec review (Spec
  D3/O7-2): zero in-library consumers today; embedding makes the later addition non-breaking, so it is pure
  speculative surface now.
- **Reuse the existing memory `Broker` as the in-memory store.** Rejected (Spec D4): it is at-most-once streaming
  with no claim/ack — the wrong settlement model for a pollable claim/ack queue.
