# Spec 001 — Messaging core (v1)

- **Status:** Approved, revised after design audit (2026-07-16)
- **Author:** initial design session
- **Research input:** [`MESSAGING.md`](../../MESSAGING.md) — Enterprise Integration Patterns (EIP) study
- **Audit:** independent Opus review applied — findings C1–C2 (critical), H1–H7, M1–M7, L1–L3 resolved in this revision
- **Realized by plans:** _pending_ — see `docs/plans/` (to be authored via `superpowers:writing-plans`)
- **ADRs:** [0001](../adrs/0001-message-payload-typing.md) · [0002](../adrs/0002-adapter-spi.md) · [0003](../adrs/0003-multi-module-repository-layout.md) · [0004](../adrs/0004-clockwork-dependency.md) · [0005](../adrs/0005-cenkalti-backoff-dependency.md) · [0006](../adrs/0006-resilience-flow-control.md)

## 1. Purpose & scope

`msgin` is a Go library implementing the messaging patterns of *Enterprise Integration Patterns*
(Hohpe & Woolf) — the conceptual model of Spring Integration, reimplemented in idiomatic,
minimal-dependency Go. It provides the pattern **core** (messages, channels, endpoints) plus
**channel adapters** to external systems, and stays **open for extension** so new adapters can be
added without changing the core.

**The first release ships:**
- The messaging core: `Message[T]`, typed producer/consumer endpoints, and the runtime that drives
  them (poller, worker-pool dispatch, retry/dead-letter, invalid-message handling, observability,
  graceful shutdown).
- **Resilience & flow control** (§7.4, mandatory first phase): credit-based backpressure, rate
  limiting, handler timeout, circuit breaker, overflow policy — so a message flood cannot hammer the
  system or a downstream.
- The **adapter SPI** — the stable contract contributors implement.
- **Six adapters:** `memory`, `sql`, `pgx`, `redis`, `nats`, `http`.

**Not in the first release** (deferred as **non-breaking, SemVer-minor additions** the SPI already
accommodates — not gaps, and not "ship broken then fix"): publish-subscribe / multiple independent
subscriber groups; routing endpoints (Router, Filter, Splitter, Aggregator); Message Translator;
Messaging Gateway; a built-in idempotency/dedup helper; Wire Tap / Message History.

`msgin` is **not** a broker, ESB, BRMS, or workflow engine. No `main`/`cmd`/CLI — it is an
importable library.

## 2. Decisions summary

| # | Decision | Choice | ADR |
| --- | --- | --- | --- |
| A | Payload typing | Generics on the caller API (`Message[T]`); non-generic SPI over `Message[any]` | [0001](../adrs/0001-message-payload-typing.md) |
| A′ | **Two encodings** | **Payload codec** (`T`↔`[]byte`) owned by the typed runtime; **envelope framing** (headers+body↔storage) owned by the adapter | [0001](../adrs/0001-message-payload-typing.md) |
| B.1 | SPI dispatch | Runtime **type-switches** on exported `PollingSource`/`StreamingSource` (no sealed marker) | [0002](../adrs/0002-adapter-spi.md) |
| B.2 | Inbound driver model | `PollingSource` (library-driven Poller) + `StreamingSource` (self-driven) | [0002](../adrs/0002-adapter-spi.md) |
| B.3 | Settlement & reliability | `Delivery` w/ `Ack`/`Nack(…, delay)`; runtime-owned `RetryPolicy` + **invalid-message channel**; `NativeReliability` split into redelivery vs dead-letter | [0002](../adrs/0002-adapter-spi.md) |
| v1 | Channel scope & concurrency | Point-to-point + worker pool (default 1); consumer groups via adapters; pub-sub deferred | [0002](../adrs/0002-adapter-spi.md) |
| SQL | Consumption strategy | Selectable: **lease/claim** (default) + **lock/`FOR UPDATE`** | [0002](../adrs/0002-adapter-spi.md) |
| L | Repository/module layout | Multi-module monorepo: core (stdlib + clockwork + cenkalti/backoff) + separate modules for `database/pgx`, redis, nats | [0003](../adrs/0003-multi-module-repository-layout.md) |
| D | Time dependency | `github.com/jonboulle/clockwork` used directly | [0004](../adrs/0004-clockwork-dependency.md) |
| R | Retry/backoff | `github.com/cenkalti/backoff/v4` for in-process/outbound loops; closed-form exponential for native redelivery | [0005](../adrs/0005-cenkalti-backoff-dependency.md) |
| Res | Resilience & flow control | Credit-based backpressure (mandatory) + optional rate-limit/circuit-breaker/handler-timeout/overflow, as clockwork-driven interfaces with dep-free defaults | [0006](../adrs/0006-resilience-flow-control.md) |

## 3. Layered architecture and the two encodings

```
Layer 1 — CALLER (typed, generic)     Message[T] · Producer[T] · Consumer[T] · Handler[T]
   ⟂ PAYLOAD CODEC (T ⟷ []byte)   performed by the runtime, which KNOWS T
Layer 2 — SPI (untyped, monomorphic)  Message[any] (Payload = live value | []byte) · PollingSource · StreamingSource · OutboundAdapter · Delivery
   ⟂ ENVELOPE FRAMING (headers+body ⟷ storage)   performed by the ADAPTER, type-agnostic
Layer 3 — BACKEND                     memory (live value, no codec) · sql · pgx · redis · nats · http
```

**Dependency points inward:** Layer 3 → Layer 2 → Layer 1. The core (L1+L2) never imports an
adapter.

The key correction from the audit (finding C1): a message on the wire is **two independent
encodings**, and they live in different layers:

- **Payload codec — `T` ⟷ `[]byte`.** Business (de)serialization (JSON, proto, …). This **requires
  `T`**, so it lives in the **typed runtime/endpoint layer**, which knows `T`. On send, the runtime
  encodes `T`→`[]byte`; on receive, it decodes `[]byte`→`T`. The adapter never sees `T`.
- **Envelope framing — `(headers, body-bytes)` ⟷ storage.** How a message's headers and opaque body
  are laid into the backend (Redis stream fields, SQL columns, NATS headers+data, an HTTP
  request/body). This is **type-agnostic** and lives in the **adapter**.

The SPI payload is `any`:
- For **wire adapters** it carries `[]byte` (the runtime pre-encoded the body; the adapter frames
  and stores it).
- For the **in-memory adapter** it carries the **live Go value** — no payload codec, no framing,
  zero-copy. Memory consumers configure no payload codec; the runtime lifts via a direct assertion.

The runtime decides decode-vs-assert by whether a `PayloadCodec` is configured for the endpoint
(required for wire adapters, absent for memory); constructors validate the pairing (§5), so a
mismatch is a typed construction error, not a runtime surprise.

## 4. Core types (`package msgin`)

```go
// Message is the immutable EIP envelope: payload (body) + headers (metadata).
// The backing header map is unexported and never handed out mutably.
type Message[T any] struct {
    payload T
    headers Headers
}

func New[T any](payload T, opts ...MessageOption) Message[T] // sets msgin.id + msgin.timestamp
func (m Message[T]) Payload() T                 { return m.payload }
func (m Message[T]) ID() string                 { return m.headers.id }
func (m Message[T]) Header(key string) (any, bool)
func (m Message[T]) WithHeader(key string, v any) Message[T] // copy-on-write; returns a new Message
func (m Message[T]) Headers() iter.Seq2[string, any]         // read-only iteration (no mutable map handed out)

// Headers is an immutable metadata set with typed accessors. Reserved keys ("msgin." namespace):
//   msgin.id, msgin.timestamp, msgin.content-type, msgin.correlation-id, msgin.delivery-count
type Headers struct { /* unexported backing map + id/timestamp */ }
func (h Headers) String(key string) (string, bool)
func (h Headers) Int(key string) (int, bool)
func (h Headers) Time(key string) (time.Time, bool)
```

Immutability is **enforced**, not assumed (audit H7): the backing map is unexported, no accessor
hands out a mutable map, and enrichment is copy-on-write (`WithHeader`). One message can therefore be
read by the whole worker pool (or a future pub-sub broadcast) with no copy and no lock, and
`go test -race` stays clean. Immutability is **shallow**, so the documented contract is that a
reference-typed payload `T` or reference-typed header *value* (a slice/map/pointer) must be treated as
**read-only** by handlers once sent — the library does not deep-copy it (audit NF-15). The injected
`clockwork.Clock` (ADR 0004) stamps `msgin.timestamp`; `New` always sets a stable `msgin.id`.

## 5. Caller-facing API (Layer 1, typed)

```go
type Producer[T any] interface {
    Send(ctx context.Context, msg Message[T]) error
}

// Handler returns nil (success → Ack), a transient error (retry per policy), or a permanent error
// (backoff.Permanent(err) or a permanent sentinel → straight to the invalid-message channel, no retry).
type Handler[T any] func(ctx context.Context, msg Message[T]) error

type Consumer[T any] interface {
    Run(ctx context.Context) error // blocks; drains and joins on cancel (within the shutdown deadline)
}

// Constructors return typed errors (audit M1): nil adapter, WithConcurrency(0), finite MaxAttempts
// with no DeadLetter, or a wire adapter with no PayloadCodec all fail here, not at runtime.
func NewProducer[T any](out OutboundAdapter, opts ...ProducerOption) (Producer[T], error)
func NewConsumer[T any](src Source, h Handler[T], opts ...ConsumerOption) (Consumer[T], error)
```

**Payload codec** is configured per endpoint (`WithPayloadCodec[T]`), default `JSONPayloadCodec[T]`.
For a **wire** adapter it is required (encode/decode `T`↔`[]byte`); for the **memory** adapter it is
omitted (the runtime asserts `Payload.(T)`), and construction rejects the wrong pairing.

**Permanent vs transient (audit M3).** A payload that cannot decode into `T`, or a handler error
wrapped with `backoff.Permanent`, is not retryable — it goes straight to the **invalid-message
channel** (§6/§7), never consuming retry attempts and never looping forever (which `MaxAttempts=0`
would otherwise cause). Sentinels: `ErrPayloadDecode`, `ErrPayloadType`.

> Refined by ADR 0007: msgin uses msgin.Permanent(err) (not backoff.Permanent) as the permanent
> marker; the runtime redelivery path stays closed-form and cenkalti-free.

## 6. Adapter SPI (Layer 2 — the contract contributors implement)

```go
// A Delivery is one received message plus the means to settle it.
type Delivery struct {
    Msg  Message[any]                                                   // Payload = []byte (wire) or live value (memory)
    Ack  func(ctx context.Context) error                               // success → remove/commit
    Nack func(ctx context.Context, requeue bool, delay time.Duration) error // failure → redeliver after delay, or drop
}
// Undecodable messages are NOT modeled here (the adapter cannot know): payload decode happens in the
// runtime, so an undecodable payload is a runtime state routed to the invalid-message channel (§7).

// PULL: the library's Poller calls Poll on a schedule (sql lease, nats JetStream pull).
type PollingSource interface {
    Poll(ctx context.Context, max int) ([]Delivery, error)
}

// PUSH: the adapter owns a blocking, cancellable loop (memory, redis, nats push, pgx LISTEN, http).
type StreamingSource interface {
    Stream(ctx context.Context, out chan<- Delivery) error // returns when ctx is done
}

// OUTBOUND: write a message to the external system. Msg.Payload is already []byte for wire adapters.
type OutboundAdapter interface {
    Send(ctx context.Context, msg Message[any]) error
}

// Optional capability: two INDEPENDENT booleans (audit H1) — a backend may own redelivery but not DLQ.
type NativeReliability interface {
    NativeRedelivery() bool // backend re-delivers unacked (redis PEL, JetStream, SQS visibility, sql lease)
    NativeDeadLetter() bool  // backend has a real DLQ pipeline (SQS redrive) — false for JetStream/sql
}

// Optional capability: a source that emits LIVE Go values (memory) rather than []byte declares it, so
// NewConsumer can enforce codec pairing at construction (audit NF-6). Wire adapters do not implement it.
type LiveValueSource interface { EmitsLiveValue() bool }
```

There is **no sealed `Source` marker** (audit C2 — an unexported marker method cannot be satisfied by
out-of-package adapters). `NewConsumer` accepts `Source = any` and the runtime **type-switches** on
the exported `PollingSource`/`StreamingSource`, returning a typed error if it is neither. A source that
implements **both** (e.g. an adapter offering pull and push) is not passed as one value; instead
dual-mode adapters expose mode-specific constructor views (e.g. `pgx.NewNotifySource(...)` →
`StreamingSource`, `pgx.NewPollSource(...)` → `PollingSource`), so there is no precedence ambiguity
(audit NF-7).

**Codec pairing (audit NF-6/C1/M1).** `NewConsumer` type-switches on `LiveValueSource`: a live-value
source (memory) must have **no** `PayloadCodec` (the runtime asserts `Payload.(T)`); any other source is
a wire adapter and **requires** a `PayloadCodec` (the runtime decodes `Payload.([]byte)`→`T`). The wrong
pairing is a typed **construction** error, not a runtime surprise.

`Nack`'s `delay` is the backoff wait before redelivery. Backends with native delayed redelivery honor
it server-side (NATS `NakWithDelay`, SQS visibility, Redis `XCLAIM` min-idle, the sql lease
`visible_after` column); if a backend cannot delay, the runtime is responsible (it applies the delay
before the message is eligible again — never by holding a lock/connection open, see §9 sql).

The SPI is non-generic and payload is `any`; the adapter performs **envelope framing** only and never
sees `T`. A new adapter = implement one inbound interface + `OutboundAdapter` (+ optionally
`NativeReliability`) + envelope framing.

## 7. Runtime (Layer 1 internals — the reusable engine)

```go
type RetryPolicy struct {
    MaxAttempts int                    // 0 = retry forever (no dead-letter); finite requires DeadLetter
    Backoff     BackoffStrategy        // §8 — cenkalti/backoff for in-process; closed-form for native
    DeadLetter  OutboundAdapter        // where poison messages go (may be another msgin adapter)
}

type ConsumerOption // WithConcurrency(n), WithPayloadCodec[T](c), WithClock(c),
                    // WithRetryPolicy(p), WithInvalidMessageSink(out), WithLogger(*slog.Logger),
                    // WithHooks(Hooks), WithShutdownTimeout(d)

type Hooks struct { OnRetry, OnDeadLetter, OnInvalidMessage, OnAck func(context.Context, Message[any], error) }
```

The consumer runtime:

1. **Ingest.** Type-switch the `Source`: a `PollingSource` is driven by the shared **Poller**
   (interval/fixed-delay, `max` per poll, backoff on empty/error, `clockwork`-driven, cancellable); a
   `StreamingSource` runs `Stream` in an owned goroutine. Both feed a **bounded** internal
   `chan Delivery` (backpressure, not unbounded buffering).
2. **Decode + dispatch.** A **worker pool** of `WithConcurrency(N)` goroutines (default **1**). Each
   worker: payload-decode `Payload`→`T` (or assert for memory); on decode failure → **invalid-message
   channel** (not retried). Then invoke `Handler[T]` (panics recovered → treated as a transient
   failure). Then settle. The branch order and guards were corrected after the second audit (NF-1,
   NF-3, NF-11):
   ```go
   n, ok := any(src).(NativeReliability)
   if !ok { n = noNativeReliability{} }            // NF-11: two-value assert; never nil-call
   switch {
   case err == nil:
       finish(d.Ack(ctx))                          // release 1 credit on terminal settle
   case isPermanent(err):                          // ErrPayloadType, backoff.Permanent(…) — never retried
       divert(ctx, invalidSink, d)                 // NF-3: Ack only if sink send succeeds, else Nack
   case policy.MaxAttempts > 0 &&                   // NF-1: finite only — MaxAttempts==0 = retry forever
        attempts(d) >= policy.MaxAttempts &&
        !n.NativeDeadLetter():
       divert(ctx, policy.DeadLetter, d)           // runtime DLQ BEFORE native-redelivery (H1)
   default:                                        // native redelivery OR in-process requeue
       finish(d.Nack(ctx, true, delay(d)))         // release credit; redelivery re-acquires on re-fetch
   }

   // divert sends a poison/invalid message to a sink and settles safely (NF-3):
   func divert(ctx, sink OutboundAdapter, d Delivery) {
       if err := sink.Send(ctx, d.Msg); err != nil {
           hooks.OnDeadLetter(ctx, d.Msg, err)     // surface it
           finish(d.Nack(ctx, true, backoff(sinkAttempts)))  // DON'T Ack a lost message → retry the send
           return
       }
       finish(d.Ack(ctx))                          // sink accepted it → safe to remove from source
   }
   ```
   `finish` releases exactly one credit (§7.4.1) on every terminal settle, guarded against
   double-release.
   `attempts(d)` prefers the native `msgin.delivery-count` header; else a runtime tracker keyed by
   message id. **The tracker only evicts on terminal settle (Ack/DLQ/invalid) — never by capacity while
   a message is still in redelivery (NF-2)**, so a poison count cannot reset mid-flight. Consequently
   **every `NativeRedelivery()` backend must populate `msgin.delivery-count`** (the sql/pgx lease
   schema carries a `delivery_count` column incremented on claim — §9), so the runtime never relies on
   an ephemeral tracker for a backend whose messages leave the process. `delay(d)` uses the
   `BackoffStrategy` (§8).
3. **Shutdown (audit H5/M7).** `WithShutdownTimeout(d)` bounds the drain. On `ctx` cancel: stop
   accepting new work (stop the poller; stop the stream loop / HTTP listener), then let in-flight
   workers finish settling every in-flight `Delivery`, then join every goroutine (goleak-clean). If
   the deadline expires, remaining in-flight messages are `Nack`ed (redeliver later) and the drain is
   abandoned deterministically.

**Ordering under retry (audit M7).** At `N=1` order is preserved for *successful* messages; a failed
message does **not** block the single worker for its backoff delay (that would collapse throughput) —
it is redelivered later, so strict FIFO is **not** guaranteed once redelivery+delay is involved. This
is documented, not implied. At `N>1` ordering is sacrificed (Competing Consumers).

**Observability (audit M2).** The runtime takes an injected `*slog.Logger` (never a global) and
`Hooks`/metrics for the operationally important events — retry, dead-letter, invalid-message, ack,
shutdown drain. Nothing logs to a package global.

### 7.4 Resilience & flow control (first phase, mandatory) — ADR 0006

A production messaging library must not let a burst of inbound messages hammer the system or a fragile
downstream. The **pull model is the primary defense**: for a `PollingSource` (sql/pgx) the runtime
controls the pull rate, so a flood in the source cannot overrun msgin — it pulls only what it can
process and the surplus stays **durably in the source** (e.g. the SQL message table) until msgin is
ready. Backpressure is intrinsic to pull; the following governors make it explicit and add ingress
protection. All are `clockwork`-driven (testable) and either dependency-free defaults or pluggable
interfaces (no new forced dependency — ADR 0006).

1. **Credit-based flow control — `WithMaxInFlight(n)` (the flood defense).** The runtime bounds
   **claimed-but-unsettled** messages to `n`. **Credit is acquired at fetch/claim, released at every
   terminal settle** (audit NF-4/NF-5):
   - A `PollingSource` fetches `k = min(maxBatch, n − inFlight)` and **acquires `k` credits at claim
     time** (`inFlight += k`); it does **not** poll at zero credit. Because credit is counted at *fetch*
     (not dispatch), messages sitting in the internal buffer are already counted — so the runtime never
     over-pulls past `n`, even before workers pick them up (this was the NF-4 hole). A burst of `N` rows
     yields at most `n` in flight regardless of `N`; the rest wait durably in the source.
   - **Every terminal settle releases exactly one credit** — `Ack`, DLQ, invalid, **and `Nack`**. A
     Nacked message *leaves* the credit pool (it either returns to the source for native redelivery, or
     enters a **separately-bounded delay-park** for in-process requeue) and **re-acquires** credit when
     re-fetched/re-injected. This prevents the NF-5 stall where messages sleeping their backoff would
     otherwise pin all `n` credits and starve the poller of fresh, possibly-healthy work. Release is
     idempotent (guarded against double-release).
   - A `StreamingSource` uses the internal bounded buffer of size `n` as the credit pool (no
     double-count): a delivered message occupies a slot until terminal settle. When full, the stream
     loop blocks (redis `BRPOP` waits, nats push pauses) or applies the overflow policy (6).
2. **Rate limiting — `WithRateLimit(RateLimiter)` (optional).** `RateLimiter interface { Wait(ctx) error }`;
   default a small `clockwork`-driven token bucket (rps + burst), no new dependency; users may plug
   `golang.org/x/time/rate`. Caps throughput even when capacity exists, to protect a fragile
   downstream.
3. **Concurrency limiting — `WithConcurrency(n)`** (§7 step 2) — bounds concurrent handler execution.
4. **Handler timeout — `WithHandlerTimeout(d)`.** Each handler runs under a context derived from the
   consumer context with deadline `d`; a stuck handler is cancelled → transient failure (retry), not a
   pinned worker. (For the `sql`/`pgx` lease strategy the **lease TTL must exceed `d`** — §9, NF-8.)
5. **Circuit breaker — `WithCircuitBreaker(CircuitBreaker)` (optional).** When the handler/outbound
   keeps failing, the breaker opens and the runtime **gates dispatch, not just ingress** (audit NF-10):
   open ⇒ the poller/stream pauses **and** workers stop pulling from the buffer (buffered messages are
   `Nack`ed for later redelivery, releasing their credit) — so an open breaker does not keep draining
   the buffer into a failing downstream. On the `clockwork` cool-down timer it half-opens and **signals
   the parked ingress goroutine** (explicit wakeup — no missed-wakeup stuck-open) to probe. Interface
   with a small `clockwork`-driven default; users may plug `sony/gobreaker`.
6. **Overflow policy (push sources) — `WithOverflow(policy)`.** For un-pausable push firehoses that
   exceed the buffer: `Block` (default — backpressure), `DropNewest`/`DropOldest`, or `Reject`
   (e.g. HTTP `503`/`429`). A **dropped message is `Nack`ed** (so an at-least-once source can redeliver)
   **and its credit released** (audit NF-14); the at-most-once loss case is documented per adapter.

**Composition (ingress → egress):** rate-limit → credit gate (acquire at fetch) → bounded buffer →
concurrency-limited workers under a handler timeout; the circuit breaker gates both ingress and
dispatch. The governors are independent options; the credit gate (1) is the load-bearing flood defense
and is always active (default `n` bounded, never unbounded). Every settle path releases credit exactly
once, which is a specific test target (audit NF-4/NF-5, ADR 0006).

## 8. Encodings: payload codec + envelope framing + backoff

**Payload codec (runtime, typed).**
```go
type PayloadCodec[T any] interface {
    Encode(T) ([]byte, error)
    Decode([]byte) (T, error)
}
```
Default `JSONPayloadCodec[T]`. Configured per endpoint via `WithPayloadCodec[T]`. Required for wire
adapters; omitted for memory (live value).

**Envelope framing (adapter).** Each wire adapter maps `(Headers, body []byte)` to/from its storage
shape (Redis stream fields, SQL columns, NATS headers+data, HTTP headers+body). Framing is internal
to the adapter and type-agnostic; it is the *only* serialization the adapter performs.

**Backoff (audit H2, refined NF-5).** Two distinct backoff sites, deliberately not forced through one
mechanism:
```go
type BackoffStrategy interface { Delay(attempt int) time.Duration } // stateless, attempt-indexed
```
- **Redelivery backoff (the runtime, all sources).** Whether the message leaves to a native backend or
  enters the in-process delay-park, the runtime always knows the *attempt count* (native
  `delivery-count`, else its tracker), so it uses a **closed-form** `Delay(attempt)` —
  `min(Max, Initial × Mult^attempt)` with optional deterministic jitter seeded by message id.
  Stateless, O(1), reproducible per attempt. This is `RetryPolicy.Backoff`, and it is why we do **not**
  need cenkalti's stateful `NextBackOff()` here (which cannot be indexed by attempt).
- **Tight in-process retry loops (adapters).** The **outbound HTTP** adapter's `POST`-retry loop is a
  genuine tight loop and uses **`cenkalti/backoff/v4`** directly — `ExponentialBackOff.Clock` set to the
  injected `clockwork.Clock`, honoring `Permanent`/`RetryAfter` (`429`). cenkalti is therefore a
  **core-module** dependency via `adapter/http`, used where it actually fits, not shoehorned into the
  redelivery path (ADR 0005).

## 9. The six v1 adapters

| Adapter | Import path | Module | Implements | Inbound shape(s) | Delivery guarantee |
| --- | --- | --- | --- | --- | --- |
| **memory** | `adapter/memory` | core | `StreamingSource` + `OutboundAdapter` | push | at-most-once |
| **sql** | `adapter/database/sql` | core | `PollingSource` + `OutboundAdapter` | pull | at-least-once |
| **pgx** | `adapter/database/pgx` | own | `PollingSource` **+** `StreamingSource` + `OutboundAdapter` (+`NativeReliability`) | **pull and push** | at-least-once |
| **redis** | `adapter/redis` | own | list / streams (+`NativeReliability`) | push | list: at-most-once · streams: at-least-once |
| **nats** | `adapter/nats` | own | core / JetStream (+`NativeReliability`) | **push and pull** | core: at-most-once · JetStream: at-least-once |
| **http** | `adapter/http` | core | `StreamingSource` + `OutboundAdapter` | push | inbound: async at-most-once / sync at-least-once *iff the client retries* · outbound: at-least-once (retry) |

- **memory** — Go channel; live value (no codec/framing), zero-copy; `Ack`/`Nack` no-op or in-process
  requeue; reference adapter and test double.
- **sql** (`database/sql`, generic; v1 dialects **PostgreSQL + MySQL** via a `Dialect` seam) — two
  **selectable consumption strategies** (audit H3/H4):
  - **lease/claim (default)** — columns `locked_by`, `locked_at`, `visible_after`, **`delivery_count`**,
    and a **`lease_epoch`** fence token. *(Refined by [ADR 0010](../adrs/0010-poller-sql-adapter.md) D4:
    the earlier `status` column is dropped — readiness is encoded by `locked_at` + `visible_after` +
    lease expiry, one source of truth; the "reaper" is inlined into the claim predicate, not a separate
    goroutine.)* A batch is claimed in a **short** transaction (`… FOR UPDATE SKIP LOCKED LIMIT max` →
    `UPDATE … SET lease_epoch = lease_epoch+1, delivery_count = delivery_count+1, locked_at = now() …
    RETURNING`), committed, then fanned out to the worker pool. `Ack`/`Nack` are separate short
    transactions and are **fenced** (audit NF-8): `… WHERE id = ? AND locked_by = ? AND lease_epoch = ?`
    — if an expired lease was already re-claimed (because the handler outran the lease TTL), the
    conditional settle is a no-op (returns `ErrStaleLease`, suppressing a phantom `OnAck`) + logged, so a
    stale worker cannot delete/settle a row another worker now owns. `Nack(delay)` sets
    `visible_after = now()+delay` (server-side delay, no held lock/connection). Expired leases are
    reclaimed **inline by the claim predicate** (`locked_at <= now() − leaseTTL`), no reaper goroutine.
    `delivery_count` populates the `msgin.delivery-count` header (audit NF-2) — for **both** strategies
    (ADR 0010 D5: the lock strategy persists it in its per-message tx too), so attempt counting never
    relies on the ephemeral runtime tracker. **Invariant: lease TTL must exceed the handler round-trip
    (handler + settle latency + margin), not merely `WithHandlerTimeout`; default 5m** (ADR 0010 D4).
  - **lock/`FOR UPDATE`** — one `SELECT … FOR UPDATE SKIP LOCKED LIMIT 1` transaction **carried in the
    `Delivery` and owned exclusively by the worker** after hand-off; the tx (and its pooled connection +
    row lock) lives from claim to `Ack`. Crash-auto-releases via tx rollback (no reaper); minimal
    schema. Single-row, no batch, cannot honor a backoff delay. **Coupling (audit NF-9):** each in-flight
    message holds one pooled connection, so effective in-flight is `min(WithMaxInFlight, poolSize −
    headroom)`; `WithMaxInFlight` and the DB pool size must be sized together or the poller deadlocks on
    an exhausted pool. Config validation enforces the relationship.
  - `Dialect` abstracts placeholder style (`$1` vs `?`), `RETURNING` vs `LAST_INSERT_ID`, upsert; and
    it **validates/quotes table and column identifiers** (allow-list / `^[A-Za-z_][A-Za-z0-9_]*$` +
    dialect quoting) — identifiers cannot be bound as parameters, so this closes the injection surface
    (audit M5). Adding another SQL database = supply a `Dialect`.
- **pgx** (PostgreSQL + wire-compatible derivatives: CockroachDB, YugabyteDB, Timescale) — the
  Postgres-native counterpart via `github.com/jackc/pgx/v5`. It exposes the two inbound modes as
  **separate constructor views**, not one value implementing both (audit NF-7, avoiding type-switch
  precedence ambiguity): `pgx.NewNotifySource(...)` → `StreamingSource` (**`LISTEN`/`NOTIFY`** wakes a
  lease claim → event-driven, no poll latency) and `pgx.NewPollSource(...)` → `PollingSource` (poll
  fallback). Adds `pgxpool` and `COPY` bulk outbound; same lease/lock strategies (incl. fence token and
  `delivery_count`) and at-least-once model as `sql`.
- **redis** — *list* `LPUSH`/`BRPOP` (at-most-once); *streams* `XADD`/`XREADGROUP`/`XACK`, consumer
  groups, `delivery-count` header, native redelivery via the Pending Entries List
  (`NativeRedelivery()=true`, `NativeDeadLetter()=false`); `Nack` delay → `XCLAIM` min-idle.
- **nats** — *core* subject pub/sub (`StreamingSource`+`OutboundAdapter`, at-most-once, queue groups);
  *JetStream* pull (`PollingSource`) or push (`StreamingSource`); `Ack`/`Nak`/`Term` map to
  `Ack`/`Nack(requeue=true)`/`Nack(requeue=false)`; `Nack` delay → `NakWithDelay`; `NumDelivered` →
  `delivery-count`; `NativeRedelivery()=true`, `NativeDeadLetter()=false` (runtime DLQ policy applies,
  e.g. to a DLQ subject).
- **http** (`net/http`, core module) — messaging over HTTP; the body is payload-encoded, headers map
  to HTTP headers with a `Content-Type`.
  - **Inbound** → `StreamingSource`. Provides a mountable `http.Handler` **and** a ready server; each
    request becomes a `Delivery`. Two modes:
    - **sync request-reply (fully specified, audit H5)** — the handler blocks until the message is
      settled: `Ack` → `2xx` (optional reply body), transient `Nack` → `5xx` (client may retry →
      at-least-once *iff it retries*), permanent/undecodable → `4xx` (Invalid Message). The handler runs
      under a context that is the **merge of the request context and the consumer context** (audit
      NF-15): a **client disconnect** cancels the handler and abandons the reply (nothing is written to
      a dead `ResponseWriter`, and the message is `Nack`ed for redelivery if the source is at-least-once);
      a **consumer shutdown** likewise cancels it. **Ownership**: when mounted in the caller's server the
      request goroutines
      belong to that server; the adapter tracks in-flight requests via a `sync.WaitGroup` so shutdown
      accounting stays goleak-clean. **Shutdown** ordering: stop accepting → unblock/settle every
      in-flight request (Ack/Nack) within the deadline → then release; expiry → `5xx` + `Nack`.
    - **async** — respond `202 Accepted` immediately; settlement decoupled (at-most-once unless the
      client retries).
    - **auth & limits (audit M6)** — authentication/authorization (bearer/HMAC/mTLS) is the caller's
      responsibility via middleware; the adapter documents this and applies `http.MaxBytesReader` and
      read/write timeouts by default.
  - **Outbound** → `OutboundAdapter`: POSTs each message to a URL (webhook); transient failures
    (`5xx`, network) retried with **cenkalti/backoff**, honoring `Retry-After` on `429`; receiver
    idempotency via `msgin.id`.

**Delivery guarantee is a documented, tested contract per adapter** (§11), not an implied behavior.

## 10. Repository & module layout (see ADR 0003)

```
msgin/                             one git repository
├── go.mod                         module github.com/kartaladev/msgin   (CORE: stdlib + clockwork + cenkalti/backoff)
├── msgin.go, runtime, SPI …
├── adapter/
│   ├── memory/                    in CORE module (stdlib; reference test double)
│   ├── http/                      in CORE module (net/http; stdlib)
│   ├── database/
│   │   ├── sql/                   in CORE module (database/sql only; Postgres + MySQL dialects)
│   │   └── pgx/   └─ go.mod        module …/adapter/database/pgx  (imports jackc/pgx/v5)
│   ├── redis/     └─ go.mod        module …/adapter/redis         (imports go-redis)
│   └── nats/      └─ go.mod        module …/adapter/nats          (imports nats.go)
├── go.work                        ties modules together for local dev/test (committed; ignored by `go get`)
└── docs/, MESSAGING.md, LICENSE, NOTICE
```

Four Go modules: **core** (+ memory + http + sql), plus **pgx**, **redis**, **nats** as separate
modules. The core's only non-stdlib deps are **`clockwork`** (ADR 0004) and **`cenkalti/backoff/v4`**
(ADR 0005). Releases use **module-path-prefixed tags** (`v0.1.0` for core,
`adapter/database/pgx/v0.1.0`, `adapter/redis/v0.1.0`, `adapter/nats/v0.1.0`); local dev uses
`go.work`; CI tests each module.

## 11. Testing posture (per `CLAUDE.md` + custom skills)

- **Blackbox `_test` packages** only; exported API exercised through the public surface.
- **`table-test`** assert-closure tables (no `want`/`wantErr` fields).
- **`use-mockgen`** for the SPI interfaces and the `redis`/`nats` client interfaces.
- **`use-testcontainers`** for wire-adapter integration tests, no fakes:
  - `sql` — against **both PostgreSQL and MySQL** (each dialect + both consumption strategies).
  - `pgx` — **PostgreSQL** (`LISTEN`/`NOTIFY` streaming, lease + lock, the reaper).
  - `redis` — Redis (list + streams); `nats` — NATS/JetStream. Reuse the shared
    `RunTestDatabase(t, opts...)`-style helper per the `use-testcontainers` skill.
- **`http`** — `net/http/httptest`: both inbound modes, request-cancel, shutdown drain, outbound
  retry/backoff (stub server + `clockwork`).
- **`goleak`** on every consumer test — proves shutdown joins all goroutines (incl. http in-flight).
- **`clockwork.NewFakeClock()`** drives the Poller and backoff timing. **Backoff jitter (audit L1):**
  exact-delay assertions set `RandomizationFactor=0` (or a seeded rand); only elapsed-time semantics
  are asserted with the fake clock.
- Every adapter's **declared delivery guarantee**, the **invalid-message path**, the **DLQ path**,
  and the **reaper** (sql lease) are asserted by test.
- **Resilience (§7.4)** is tested deterministically with `clockwork`: a flood test asserts
  `WithMaxInFlight(n)` caps in-flight at ~`n` while the sql table holds a large backlog; rate-limit,
  handler-timeout, circuit-breaker open/half-open/close transitions, and each overflow policy are
  covered as table cases.

## 12. Requirements traceability

- Realizes the EIP research in [`MESSAGING.md`](../../MESSAGING.md) (Datatype Channel → generics;
  Channel Adapter → SPI; Polling/Event-Driven Consumer → two inbound shapes; Guaranteed
  Delivery/Transactional Client/Idempotent Receiver → reliability + at-least-once; **Invalid Message
  Channel + Dead Letter Channel → the invalid-message and DLQ paths**; Competing Consumers → worker
  pool; Request-Reply → http sync).
- Decisions recorded in ADRs 0001–0005; the design audit's findings C1–C2/H1–H7/M1–M7/L1–L3 are
  resolved in this revision.
- Next: `superpowers:writing-plans` produces the implementation plan(s) in `docs/plans/`, each
  referencing this spec.
