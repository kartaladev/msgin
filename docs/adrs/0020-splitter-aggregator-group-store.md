# ADR 0020 — Splitter, Aggregator & the `MessageGroupStore` model (sequence-header reassembly, hold-until-release settlement)

- **Status:** Proposed (2026-07-20) — records the architectural decisions of [Spec 009](../specs/009-splitter-aggregator-endpoints.md),
  settled with the user in brainstorming (durable group store now; opt-in expiry → discard channel; full expr sugar;
  `Run(ctx)`-owning Aggregator). **Phase-1 (Splitter) adversarial audit round 1 folded** (Opus, verified against the
  real API + sql framing): **H-1** deterministic child id `parentID#seq` added to the sequence-header convention
  (§1) — closes the id-collision that would have made the Splitter→Aggregator round-trip silently drop; **M-1** the
  durable `int`-vs-`float64` sequence-header contract recorded (§2); L-1/L-2/L-3 folded into Plan 015. **Round 2 re-audit: SOUND-WITH-NITS**
  — the child-id scheme verified collision-free/idempotent and inert in the retry path; only a plan-local godoc nit
  (L-1) folded. Phase-1 bundle ready to implement. Realized across Plans 015–018; the `sql.GroupStore` specifics are
  recorded separately in **ADR 0021**, expr-on-endpoints as an **ADR 0019 addendum**.
- **Spec:** [Spec 009 — Splitter + Aggregator endpoints](../specs/009-splitter-aggregator-endpoints.md).
- **Depends on / builds on:** [ADR 0013 — Composition endpoints](0013-composition-endpoints.md) (the
  `MessageHandler`/`MessageChannel`/`Step`/`Chain` backbone + synchronous-direct error model these endpoints extend),
  [ADR 0001 — Payload typing](0001-message-payload-typing.md) (C-core/A-sugar, `PayloadOf[T]`/`ErrPayloadType`),
  [ADR 0018 — Persistent queue channel](0018-persistent-queue-channel.md) (the `ChannelStore` memory/sql split and
  the "embed a `MessageGroupStore` later, non-breakingly" foresight this ADR cashes in), and
  [ADR 0019 — Runtime expression evaluation](0019-runtime-expression-evaluation.md) (the `compile[A]` primitive the
  aggregator/split exprs reuse).

## Context

Spec 003 / ADR 0013 built the **linear** composition endpoints and named the two **non-linear routing endpoints** —
**Splitter** (1→N) and **Aggregator** (N→1) — as the explicit next increment. ADR 0013 already chose the
channel-wired (forward-onward) primitive shape *specifically so* Splitter (N out) and Aggregator (emit-on-complete)
would fit without a core change. This ADR realizes them.

The Splitter is a straightforward stateless `Step`. The Aggregator is the hardest endpoint in the EIP set: it is
**stateful** — it must hold members of a group until the group is complete — which forces three sub-decisions the
core has otherwise avoided: **where partial groups live** (a store), **when a group is complete** (correlation +
release), and **what happens to a group that never completes** (expiry). It also raises the same **settlement**
question `QueueChannel` faced (ADR 0018): a *held* message looks consumed to the source, so *who Acks, and when* —
and if the source Acks while the member sits only in the aggregator's memory, a crash loses it. The user chose to
resolve this at the **durable** end: a real `MessageGroupStore` SPI with an in-memory reference and a `sql` durable
backend, and **hold-until-release settlement** giving documented at-least-once (within-process on `memory`, across
restart on `sql`). ADR 0018 explicitly anticipated this: it deferred a broader store base but noted "a
`MessageGroupStore` for Aggregator/Resequencer can later be added as a new interface — non-breaking for existing
implementers." This ADR adds exactly that.

## Decision

### 1. Splitter — `Split[A,B]`, stateless `Step`, stamps EIP sequence headers (Spec D1–D4)

```go
func Split[A, B any](fn func(ctx context.Context, m Message[A]) ([]Message[B], error)) Step
```

- Asserts payload→A (`ErrPayloadType`, permanent → invalid-message channel), calls `fn`, forwards **each child to
  `next` in order**. A nil `fn` → `ErrNilFunc` (shared `nilFuncStep`, no panic on input, per ADR 0013 §3).
- **Materialized slice, not an iterator** — simplest, matches the synchronous-direct model; an `iter.Seq` streaming
  split is a documented future, not v1.
- **Sequence-header + child-identity convention (audit R1 H-1).** New reserved keys `HeaderSequenceNumber`
  (`"msgin.sequence-number"`, 1-based `int`) and `HeaderSequenceSize` (`"msgin.sequence-size"`, `int` = N). On every
  child `Split` stamps:
  - both sequence headers;
  - a **deterministic child `HeaderID` = `parentID#seq`** (only when the parent has an id). This is the load-bearing
    fix: the canonical child constructor `WithPayload(parent, x)` copies the parent's headers *verbatim including
    `HeaderID`*, so without this every child would share the parent id — and the Aggregator (which dedups group
    members by `msg.ID()`, §2) would collapse all N into one member and the group would **never release** (silent
    loss). A `parentID#seq` id is unique within one split (the group fills to N) **and** stable across a redelivery
    of the same parent (so the Aggregator's idempotent `Add` recognizes redelivered members — the §4 guarantee).
  - `HeaderCorrelationID` = the **parent's id** *only if the child does not already carry a correlation id* (a
    caller-set/inherited correlation is preserved, so nested split/aggregate keeps its outer group key).

  With an id-less parent (`ID()==""`) no id/correlation is derived (sequence headers still stamped; such a split is
  not redelivery-idempotent — rare, since source-delivered messages carry an id). Stamping is unconditional (no
  `WithoutSequenceHeaders` escape in v1, Spec O9-5). Together these make the Splitter→…→Aggregator round-trip work
  with zero extra config.
- **Empty split** (`fn` returns empty/nil slice) forwards nothing and returns nil — a valid "nothing to split", the
  Filter-drop analogue, not an error (Spec D3).
- **Settlement.** All N children forward on the delivery goroutine before `Handle` returns, so the driving
  `Consumer` Acks the source only after **every** child succeeds → end-to-end at-least-once for free, exactly like
  `Chain`. A child error propagates immediately (remaining children not sent) → source Nack → the **whole parent**
  is redelivered → children reprocessed; children must be idempotent downstream (documented on `Split`'s godoc).

### 2. `MessageGroupStore` — a narrow core SPI (Spec D5/D6), mirroring `ChannelStore`

```go
type MessageGroup interface {
    Key() string                 // the correlation key
    Messages() []Message[any]    // members in arrival order
    CreatedAt() time.Time        // first-member arrival, for expiry
}
type MessageGroupStore interface {
    // Add durably persists msg into group key and returns the resulting group.
    // Idempotent by msg id: re-adding an already-stored member is a no-op that
    // returns the unchanged group — this upholds at-least-once under source
    // redelivery (a redelivered member does not double-count toward release).
    Add(ctx context.Context, key string, msg Message[any]) (MessageGroup, error)
    // Remove deletes an entire group (called after a successful release).
    Remove(ctx context.Context, key string) error
    // Expired returns groups whose CreatedAt is strictly before the cutoff (reaper).
    Expired(ctx context.Context, before time.Time) ([]MessageGroup, error)
    // EmitsLiveValue reports live Go values (memory) vs []byte (wire, sql), for
    // codec pairing — the same mechanism ChannelStore uses.
    EmitsLiveValue() bool
}
```

- **Narrow by decision.** The aggregation path exercises only `Add`/`Remove`/`Expired`. This is a *new* interface,
  not an extension of `ChannelStore` (their operations barely overlap — group-keyed idempotent add vs FIFO
  claim/lease); shipping it separately keeps both SPIs cohesive. A future Resequencer reuses this same store.
- **`memory.GroupStore`** (core `adapter/memory`, Plan 016): `map[string]*group` under a mutex; idempotent add by
  `msg.ID()`; expiry via `CreatedAt` scan; carries live values (`EmitsLiveValue()→true`); **starts no goroutine**
  (the reaper lives in the Aggregator, so the store is goleak-trivial). Guarantee: **at-least-once within the
  process; partial groups lost on crash** — documented on the constructor.
- **Durable sequence-header type contract (audit R1 M-1).** `HeaderSequenceSize`/`HeaderSequenceNumber` are `int`
  on the live-value (`memory`) path, but the `sql` store frames headers as JSON and `DecodeHeaders` restores plain
  numbers as `float64` (it hand-restores `int` only for special-cased keys like `HeaderDeliveryCount`). So the
  durable path must handle both: **(a)** the default release strategy reads `HeaderSequenceSize`
  **number-tolerantly** (accepts `int`/`int64`/`float64` → int), and **(b)** `sql.DecodeHeaders` adds the two
  sequence headers to its `int`-restoration special-case list (Plan 017 / ADR 0021, mirroring the
  `HeaderDeliveryCount` precedent). Without this, groups on `sql.GroupStore` never reach the size check and fall
  through to expiry — the "works across restart, zero config" claim (G4) would silently fail. Recorded now so Phase
  1's `int` choice is safe end-to-end.

### 3. Aggregator — `NewAggregator[A,B]`, a `Run(ctx)`-owning `MessageHandler` (Spec D7/D8)

```go
func NewAggregator[A, B any](fn func(ctx context.Context, group []Message[A]) (Message[B], error),
    opts ...AggOption) (*Aggregator, error)
func (a *Aggregator) Handle(ctx context.Context, msg Message[any]) error // MessageHandler
func (a *Aggregator) Run(ctx context.Context) error                     // expiry reaper; joined on ctx cancel
```

- **Lifecycle struct, not a bare `Step`** — because the expiry reaper is an owned goroutine (no-goroutine-leak
  gate). It implements `MessageHandler` (place as a chain's `next` via `Subscribe`, or as a flow head via
  `NewConsumer[any](src, agg.Handle)`). `Run(ctx)` starts the reaper **only when `WithGroupTimeout` is set**,
  cancellable and joined on ctx cancel (consistent with `Consumer`/`Poller`). With no timeout, no goroutine is
  started and `Handle` works standalone. A nil `fn`, or `WithGroupTimeout` set without `WithExpiredGroupChannel`
  (§5), is a **construction error** (returned from `NewAggregator`, the debuggability surface).
- **Per-message algorithm (`Handle`):** assert→A (`ErrPayloadType`) · compute correlation key
  (`WithCorrelationStrategy`, default = `HeaderCorrelationID`; missing/empty → `ErrNoCorrelation`, permanent →
  invalid channel) · `store.Add(ctx, key, msg)` (durable persist) · release check (`WithReleaseStrategy`, default =
  `len(group) >= HeaderSequenceSize` (read **number-tolerantly** per §2's M-1 contract); `WithCompletionSize(n)`
  sugar for a fixed count) · **release** → `fn(ctx,
  group)` → forward `Message[B]` to `next` → on success `store.Remove(key)`; **not yet** → return nil (message held;
  source Acks — durability now on the store).
- **Concurrency.** Group operations serialize through the store (the store owns its locking / DB transactions), the
  single synchronization point between concurrent `Handle` calls (worker pool) and the reaper. `-race` clean is a
  gate.

### 4. Hold-until-release settlement & the at-least-once contract (Spec D9) — the crux

The ordering guarantees the guarantee:

- `store.Add` **commits before** `Handle` returns nil (before the source Ack). Crash in between → source redelivers
  → **idempotent `Add`** (no duplicate member; a redelivered member does not double-count). Durability hands off
  source→group-store; the source may Ack safely because the member is now in the store.
- Release **forwards to `next` before** `store.Remove`. Crash in between → the group survives in the store → it
  **re-releases** on restart (next member for the key, or the reaper) → a **duplicate downstream** message. This is
  at-least-once, documented — not hidden.
- **Documented v1 caveat — "late message after completion":** a member redelivered *after* its group already
  completed and was removed forms a *fresh* group under the same key. v1 accepts this (at-least-once); downstream
  must be idempotent, or the release strategy must tolerate a singleton/partial re-group. A completed-group
  dedup marker is a deferred refinement (Spec §2 non-goals).

### 5. Expiry — opt-in, visible, never silent (Spec D10; user's safe-default choice)

- **No default timeout.** A documented "off" state: groups are held in the store until complete, bounded by store
  capacity (§6). Per CLAUDE.md, no single timeout value is safe for an unknown caller (too-tight footguns a slow
  group into data loss; any value is a guess), so expiry is **explicit/opt-in**, not a guessed default.
- `WithGroupTimeout(d time.Duration)` (driven by an injected `clockwork.Clock`) + `WithExpiredGroupChannel(ch
  MessageChannel)`: the reaper periodically calls `store.Expired(now-d)` and routes each expired **partial** group
  to `ch` (visible for audit/DLQ), then `store.Remove`s it. **Never a silent drop; never a partial emit
  downstream.**
- **`WithGroupTimeout` without `WithExpiredGroupChannel` is a construction error** — an expiry that discards data
  without a visible sink is a footgun; expiry demands a destination.

### 6. `sql.GroupStore` (Plan 017 / ADR 0021) and store capacity (Spec O9-3)

- The durable backend is `sql.GroupStore` over `database/sql` + the existing `Dialect` seam (PG/MySQL/MariaDB/
  SQLite), idempotent upsert / key-scoped delete / expiry query; `EmitsLiveValue()→false`; at-least-once **across
  restart**. Full schema/dialect specifics are ADR 0021 (kept out of this ADR to stay cohesive).
- **Group-store capacity** (bounding total groups/members to avoid a slow-motion DoS from never-completing groups)
  is resolved in ADR 0021 by reusing the established `OverflowPolicy` vocabulary on the store constructor (as
  `ChannelStore` capacity did, ADR 0018 §4), not on the Aggregator. Recorded here as the shared principle; the
  memory store gets a bounded-groups default, the sql store is DB-bounded.

### 7. expr sugar (Plan 018 / ADR 0019 addendum) (Spec D12–D14)

`TransformExpr[A,B]` (projection → asserted to B; new `exprAny` output kind on `compile`), `SplitExpr[A]` (expr →
slice → Phase-1 Split), and the aggregator exprs — `WithCorrelationExpr[A]` (reuses `exprString` compile) plus
`WithReleaseExpr`/aggregate-by-expr over a **new fixed group-scoped env** `{messages []A, size int}`. Reuses
`compile[A]`; a bad expr is `ErrInvalidExpression` at construction. No new dependency (expr already in-core). The
group-env is fixed and documented, not a general extension point (`WithExprEnv` stays deferred, Spec 008 O8-3).

## Consequences

**Positive**
- Completes the EIP routing group (Splitter/Aggregator) on the **existing** channel-wired backbone with **no core
  runtime change** — the endpoints are a `Step` and a `MessageHandler`, inheriting retry/DLQ/invalid-message/
  flow-control/worker-pool through the driving `Consumer` (ADR 0013 §4 payoff, one more level up).
- **Swappable durability** for aggregation state via the narrow `MessageGroupStore` — memory reference, sql durable
  — with the same memory↔durable swap-without-rewiring value `ChannelStore` gave queue channels, cashing in ADR
  0018's non-breaking-embedding foresight.
- Splitter↔Aggregator reassembly works out of the box via the reserved sequence headers; the same store serves a
  future Resequencer with no new SPI.
- Expiry is safe-by-construction: opt-in, and it *cannot* be configured to silently lose data (the missing-sink
  construction error).

**Negative / costs**
- **Hold-until-release settlement is at-least-once, not exactly-once** — the release-before-remove duplicate window
  and the "late message after completion" re-group are real, documented caveats; exactly-once/dedup is deferred.
- **Aggregator carries a lifecycle** (`Run(ctx)`) unlike the linear `Step` endpoints — a slightly larger mental
  model, accepted because a timer-driven reaper cannot be goroutine-free without giving up idle-group reaping.
- **A new public SPI** (`MessageGroupStore` + `MessageGroup`) and a new endpoint struct enlarge the surface; held
  minimal by narrowness (three methods) and by deferring Resequencer/late-member-dedup/other backends.
- **`sql.GroupStore` overlaps existing sql machinery** but, unlike `sql.QueueStore`, needs genuinely new group-keyed
  SQL (idempotent upsert, expiry query) — it is not a thin facade; that is ADR 0021's subject.

**Rejected alternatives**
- **In-memory-only aggregation (no SPI).** Simplest, but adding durability later is a breaking core change; the
  narrow SPI (the `ChannelStore` playbook) costs little now and keeps sql/redis backends non-breaking. (User
  rejected in brainstorming.)
- **Aggregator as a pure `Step` with lazy opportunistic reaping** (expiry checked inline on each `Add`, zero
  goroutines). Simpler and goleak-trivial, but an *idle* aggregator never reaps a stale group — expiry that only
  fires on traffic is a footgun. The `Run(ctx)`-owning struct reaps on a timer regardless of traffic (Spec O9-1).
- **Force-release / silent-drop expiry defaults.** Rejected at the safe-default gate: force-release silently emits
  incomplete (corruption-class) data; a default-timeout drop silently loses data for a slow/large group. Opt-in +
  visible channel is the only fail-safe shape (Spec D10).
- **Ack-on-receive with an in-memory-only store** (the simplest settlement). Rejected with the durable-store
  choice: it cannot survive a restart, which was the user's explicit requirement.
