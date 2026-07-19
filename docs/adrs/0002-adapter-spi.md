# ADR 0002 — Adapter SPI: non-generic, dual inbound shapes, runtime-owned reliability

- **Status:** Accepted (2026-07-16)
- **Context source:** [Spec 001 — Messaging core](../specs/001-messaging-core.md) §6–§9; [`MESSAGING.md`](../../MESSAGING.md) §2.3–2.10
- **Related:** [ADR 0001 — Payload typing](0001-message-payload-typing.md), [ADR 0003 — Module layout](0003-multi-module-repository-layout.md)

## Context

The **Channel Adapter** (`MESSAGING.md` §2.3) is the reason `msgin` exists: a stable, public
contract that contributors implement to bridge external systems, without changing the core. The
research fixes several shapes: two inbound consumer styles (**Polling Consumer** §2.4 vs
**Event-Driven Consumer** §2.5), first-class **ack/nack** (**Guaranteed Delivery** §2.6,
**Transactional Client** §2.6), duplicate tolerance (**Idempotent Receiver** §2.10), a poison-message
path (**Dead Letter Channel** §2.7), and concurrency (**Competing Consumers** §2.9). Backends
differ: some own redelivery/DLQ natively (Redis Streams PEL, NATS JetStream, SQS), others do not
(in-memory, a plain SQL table).

## Decision

**1. Non-generic SPI over `Message[any]`, dispatched by type-switch.** The interfaces contributors
implement have no type parameter and carry `Message[any]`. `NewConsumer` accepts `Source = any` and
the runtime **type-switches** on the exported `PollingSource`/`StreamingSource` — there is **no
sealed `Source interface{ isSource() }` marker** (audit C2: an unexported marker method cannot be
satisfied by out-of-package adapters, i.e. every adapter, so it would not compile). The two
encodings follow ADR 0001: the **payload codec** (`T`⟷`[]byte`) lives in the typed runtime; the
adapter performs only **envelope framing** (`(headers, body-bytes)`⟷storage) and never sees `T`.
`Message[any].Payload` is `[]byte` for wire adapters, the live value for `memory` (zero-copy).

**2. Two inbound shapes.**
- `PollingSource.Poll(ctx, max) ([]Delivery, error)` — driven by a shared library **Poller**
  (interval/fixed-delay, max-per-poll, backoff, `clockwork`-driven, cancellable). Used by `sql`,
  `pgx` (poll fallback), NATS JetStream pull.
- `StreamingSource.Stream(ctx, out chan<- Delivery) error` — the adapter owns a blocking,
  cancellable loop. Used by `memory`, `redis`, `nats` push, `pgx` `LISTEN`/`NOTIFY`, and `http`
  inbound (the HTTP handler emits a `Delivery` per request).

**3. Settlement + runtime-owned reliability.** A `Delivery` carries `Ack`/`Nack` closures, where
`Nack(ctx, requeue bool, delay time.Duration)` — `delay` is the backoff wait before redelivery,
honored server-side by backends that support it (NATS `NakWithDelay`, SQS visibility, Redis `XCLAIM`
min-idle, the sql lease `visible_after` column) or by the runtime otherwise (never by holding a
lock/connection open — see the sql lease strategy). The **runtime** owns one configurable
`RetryPolicy`. The settlement branch order matters and was corrected after the audit (H1): the
`NativeReliability` capability exposes **two independent booleans** — `NativeRedelivery()` and
`NativeDeadLetter()` — and the dead-letter check (`attempts ≥ MaxAttempts && !NativeDeadLetter()`)
runs **before** the native-redelivery branch, so a JetStream/sql source (native redelivery, *no*
native DLQ) still reaches the runtime DLQ instead of redelivering forever.

- **Invalid-message channel (audit H6/M3).** A payload the runtime cannot decode into `T`, or a
  handler error wrapped `backoff.Permanent`, is **not retryable** — it is routed to a configured
  invalid-message `OutboundAdapter` (EIP *Invalid Message Channel*). Decode failures happen in the
  runtime (which owns the payload codec), so — unlike the earlier design where decoding sat in the
  adapter before a `Delivery` existed — the runtime always sees and diverts them.
- **Second-audit settlement corrections.** The DLQ branch is guarded `MaxAttempts > 0 && attempts ≥
  MaxAttempts && !NativeDeadLetter()` — `MaxAttempts == 0` means retry-forever and must **not** enter
  DLQ (NF-1: the unguarded `≥ 0` both broke retry-forever and nil-derefed the DeadLetter). Diverting to
  the invalid/DLQ sink **Acks only if the sink send succeeds**; on send failure the message is
  `Nack`ed (the send is itself retryable) — never Acked-and-lost (NF-3). The `NativeReliability` type
  assertion uses the two-value form with a both-false fallback (NF-11). Attempt counters evict only on
  terminal settle (never by capacity while in redelivery), and **every `NativeRedelivery()` backend
  must populate `msgin.delivery-count`** so counts survive the process-exit gap (NF-2).
- **Wire-vs-memory discriminator (audit NF-6/C1/M1).** An optional `LiveValueSource { EmitsLiveValue()
  bool }` lets `memory` declare it emits live Go values; `NewConsumer` type-switches on it to *enforce*
  the codec pairing at construction (live-value ⇒ no codec/assert; else ⇒ codec required/decode) —
  making the promised construction-time typed error mechanizable.
- **Dual-mode adapters (audit NF-7).** An adapter offering both pull and push exposes **mode-specific
  constructor views** (e.g. `pgx.NewNotifySource`/`pgx.NewPollSource`) rather than one value
  implementing both interfaces, so the runtime's type-switch has no precedence ambiguity.
- **Attempt counting** reads the native `msgin.delivery-count` header, else a **bounded** runtime
  tracker keyed by message id and **evicted on terminal settle** (audit M4 — no unbounded growth).
- **Backoff** is `cenkalti/backoff/v4` for in-process/outbound loops and a closed-form exponential for
  native redelivery ([ADR 0005](0005-cenkalti-backoff-dependency.md)).
- **Observability (audit M2)**: the runtime takes an injected `*slog.Logger` (never a global) plus
  hooks/metrics for retry, dead-letter, invalid-message, ack, and shutdown-drain events.
- **Constructors return typed errors (audit M1)**: nil adapter, `WithConcurrency(0)`, finite
  `MaxAttempts` with no `DeadLetter`, or a wire adapter with no `PayloadCodec` fail at construction.

**4. v1 channel scope.** Point-to-point channels with a **worker pool** (`WithConcurrency(N)`,
default **1** = order-preserving; `N>1` = in-process Competing Consumers). **Consumer groups** are
delivered by adapters/backends (Redis Streams, NATS JetStream durables, SQL `SKIP LOCKED`), needing
no SPI change. **Publish-subscribe** (multiple independent subscriber groups) is **deferred**.

**5. Graceful shutdown (audit H5/M7).** A `WithShutdownTimeout` deadline bounds the drain. On cancel:
stop accepting (poller/stream/HTTP listener) → settle every in-flight `Delivery` → join every
goroutine (goleak-clean); on deadline expiry, in-flight messages are `Nack`ed and the drain is
abandoned deterministically. Handler panics are recovered at the worker boundary and treated as
transient failures. Ordering under retry is documented, not implied: strict FIFO does **not** hold
once redelivery+delay is involved, even at `N=1` (a failed message is redelivered later rather than
blocking the worker for its delay).

**6. SQL consumption strategies (audit H3/H4).** The `sql`/`pgx` adapters offer two selectable
`PollingSource` strategies, because `FOR UPDATE` + `Ack`-deletes-in-tx across a worker pool is unsafe
(`*sql.Tx` is not concurrent; the lock/connection would be held across the whole batch's handler
time):
- **lease/claim (default)** — `status`/`locked_by`/`locked_at`/`visible_after`/`delivery_count` columns
  plus a `lease_epoch` **fence token**; claim a batch in a short tx (bumping `lease_epoch` and
  `delivery_count`), commit, fan out; `Ack`/`Nack` are separate short txns, **fenced** on
  `lease_epoch`/`locked_by` so a worker that outran the lease TTL cannot settle a row the reaper
  re-leased (audit NF-8; lease TTL must exceed `WithHandlerTimeout`). `Nack(delay)` sets
  `visible_after`; a reaper reclaims expired leases; `delivery_count` feeds `msgin.delivery-count`
  (NF-2). Batches, honors delay, holds no connection across handlers.
- **lock/`FOR UPDATE`** — one tx held across processing, carried in the `Delivery` and owned by the
  worker; crash-auto-releases via rollback (no reaper), minimal schema; no batching, cannot honor delay.
  Each in-flight message holds one pooled connection, so in-flight is `min(WithMaxInFlight, poolSize −
  headroom)` and the two are config-validated together to avoid pool-exhaustion deadlock (audit NF-9).
The `Dialect` also validates/quotes table & column identifiers (audit M5 — they cannot be
parameter-bound).

**7. HTTP sync request-reply (audit H5).** The `http` inbound sync mode is fully specified: the
request `context` is wired into the `Delivery` (client disconnect cancels settlement), in-flight
requests are tracked for goleak-clean shutdown whether the handler is mounted or server-owned, and
`Ack`/transient-`Nack`/permanent map to `2xx`/`5xx`/`4xx`.

## Consequences

**Positive**
- Lowest possible contributor bar: a new adapter implements one inbound interface + `OutboundAdapter`
  (+ optionally `NativeReliability`). Reliability, retry, backoff, dead-lettering, concurrency, and
  shutdown come from the runtime — uniform across all adapters.
- Production-robust by default even on backends with no native DLQ (memory, SQL).
- The SPI accommodated `nats` (both inbound shapes), consumer groups, and `http` (request/response
  as `Ack`/`Nack`) with zero changes — evidence it sits at the right altitude.
- Altitude-correct: one general mechanism, not a special case per backend.

**Negative / costs**
- The runtime must reconcile native vs. tracked attempt counts (handled by the `delivery-count`
  header convention + `NativeReliability`).
- `Nack`/redelivery semantics vary by backend; the declared per-adapter **delivery guarantee** (§9)
  is a documented, tested contract to manage that.
- Deferring pub-sub means multi-subscriber fan-out waits for a later version (acceptable: the four
  v1 adapters are queue-like; the SPI is forward-compatible).

**Rejected alternatives**
- **Generic SPI `Adapter[T]`** — full static typing to the edge, but every adapter becomes generic
  (steep contributor bar, awkward heterogeneous registries, no generic methods in Go).
- **`[]byte` SPI** — simplest signatures, but forces the in-memory adapter to serialize needlessly
  and turns type mismatches into opaque decode failures.
- **Adapter-owned or handler-owned retry/DLQ** — inconsistent semantics across adapters, or pushes
  production essentials (poison-message handling) onto every user.
- **Single unified inbound interface** — either makes polled batch fetch (`LIMIT N`) awkward or
  forces every polled adapter to re-implement scheduling the shared Poller could own.
- **Sealed `Source interface{ isSource() }`** (an earlier draft) — an unexported marker method cannot
  be implemented by out-of-package adapters, so it fails to compile with any real adapter and defeats
  the multi-module extensibility goal. Replaced by a runtime type-switch on the exported interfaces.
- **Codec inside wire adapters / decode in the adapter** (an earlier draft) — the adapter has `[]byte`
  but not `T`, so it cannot produce a typed payload; `msg.Payload.(T)` fails on every wire message.
  Replaced by the payload-codec-in-runtime split (ADR 0001).

## Addendum — how these interfaces map to EIP / Spring Integration "Channel Adapter" (2026-07-20)

**Status:** Accepted (clarification only; no design change). Recorded while evaluating the `adapter/database/sql`
package against Spring Integration during the Spec 007 / Plan 013 (queue channel) work.

This ADR's SPI **is** the EIP **Channel Adapter** contract, in the strict Spring Integration sense — a
*unidirectional* bridge between a message channel and a non-messaging external system, doing transport + envelope
framing only, with **no request-reply** (a request-reply bridge would be a *Gateway*, which this project does not
yet ship). Three points worth pinning down so the term is used consistently:

1. **The inbound channel-adapter responsibility is split.** A `PollingSource` corresponds to Spring's
   `MessageSource` (the pollable source of messages); the **runtime's `Poller`/`NewConsumer`** corresponds to
   Spring's `SourcePollingChannelAdapter` (the driver that schedules the poll and pushes into the flow). Together
   they form the complete inbound channel adapter. Symmetrically, `OutboundAdapter.Send` is the outbound
   channel-adapter *handler*; it becomes a full channel-subscribed outbound adapter once wired behind a channel via
   a `Producer`/`Consumer`. A `StreamingSource` is the event-driven (push) inbound variant.

2. **The `sql` adapter adapts the database *as a message transport*, not as an arbitrary business table.** Its
   `Source`/`Outbound` own their schema (`headers`/`payload`/`visible_after`/lease columns) and impose queue
   semantics (lease/claim, at-least-once, `SKIP LOCKED`). That places them closer to Spring's JMS/Kafka-style
   channel adapters over a message transport — and to `JdbcChannelMessageStore` behind a `QueueChannel` — than to a
   `jdbc:inbound-channel-adapter` running a caller's `SELECT` over a caller-owned business table. Both roles are
   legitimate channel adapters (the DB is the external system/transport); the distinction is what the "external
   system" is. **Consequence:** a true *data* inbound adapter (poll a caller-owned table with a caller-supplied
   `SELECT`, adapting external application data) would be a **new, different** adapter this SPI accommodates but the
   `sql` package does not currently provide. The same `Source`+`Outbound` pair over one table also doubles as a
   durable in-flow **Channel** (the persistence behind `sql.QueueStore`, Spec 007 / ADR 0018) — a channel role,
   distinct from the channel-adapter role, depending on whether the far end is an external system or another
   endpoint in the same flow.

3. **Not every component in an adapter package is a channel adapter.** `sql.InboxDeduper` is an **Idempotent
   Receiver** (EIP), not a channel adapter; the `Dialect`/framing/`adapterBase` types are adapter *internals* (the
   SPI's implementation seams), not messaging endpoints.

No interface changes follow from this addendum; it fixes the vocabulary so "channel adapter", "message source",
and "durable channel" are applied consistently across the docs.
