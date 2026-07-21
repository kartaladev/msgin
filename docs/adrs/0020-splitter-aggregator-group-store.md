# ADR 0020 — Splitter, Aggregator & the `MessageGroupStore` model (sequence-header reassembly, hold-until-release settlement)

- **Status:** Proposed (2026-07-20) — records the architectural decisions of [Spec 009](../specs/009-splitter-aggregator-endpoints.md),
  settled with the user in brainstorming (durable group store now; opt-in expiry → discard channel; full expr sugar;
  `Run(ctx)`-owning Aggregator). **Phase-1 (Splitter) adversarial audit round 1 folded** (Opus, verified against the
  real API + sql framing): **H-1** deterministic child id `parentID#seq` added to the sequence-header convention
  (§1) — closes the id-collision that would have made the Splitter→Aggregator round-trip silently drop; **M-1** the
  durable `int`-vs-`float64` sequence-header contract recorded (§2); L-1/L-2/L-3 folded into Plan 015. **Round 2 re-audit: SOUND-WITH-NITS**
  — the child-id scheme verified collision-free/idempotent and inert in the retry path; only a plan-local godoc nit
  (L-1) folded. **Phase 1 (Splitter) SHIPPED & MERGED to `main` (`e4b346d`).**
  **Phase 2 (Aggregator) round-1 audit: NEEDS-REVISION — folded** (H-1 concurrent double-release → per-key lock +
  `Remove`-returns-group, §2/§3; M-1 `Permanent(ErrNoCorrelation)`, §3; M-2 ingress fail-fast already in §3; M-2/M-3
  + L into Plan 016). **Round-2 re-audit: SOUND-WITH-NITS — folded** (F1 reaper
  re-check-the-removed-group; F2 cyclic-Send self-deadlock doc; F3 `==n` caveat scoped to multi-process; F4 de-stale
  inline snippets; F5 expired-Send error; F6 clone `Remove` snapshot). Per-key lock verified to close H-1 with no
  ABBA deadlock; M-1/M-2 verified to route to the invalid-message channel. **Phase-2 design implementation-ready.**
  Realized across Plans 015–018; the `sql.GroupStore` specifics (incl. the multi-process transactional atomic
  release H-1 needs) are recorded separately in **ADR 0021**, expr-on-endpoints as an **ADR 0019 addendum**.
  **Phase 2 (Aggregator) SHIPPED & MERGED to `main` (`94cda1f`).**
  **Phase-3 revision (2026-07-21): §8 below SUPERSEDES §2's `Remove` method and §3's per-process per-key lock.**
  The user chose full **multi-process** durability, which the per-key lock cannot deliver (it does not serialize
  across processes). The settlement model becomes a **store-level lease-claim** (`ClaimGroup`/`SettleGroup`/
  `AbandonGroup`) that is the single serialization point for both memory (in-process) and sql (cross-process); the
  Aggregator's `[256]sync.Mutex` is removed. Because msgin has **no release tag** (the Phase-2 SPI is unreleased),
  this reshape is not a SemVer break. See §8; the `sql.GroupStore` realization is **ADR 0021**.
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
  - a **deterministic child `HeaderMessageID` = `parentID#seq`** (only when the parent has an id). This is the load-bearing
    fix: the canonical child constructor `WithPayload(parent, x)` copies the parent's headers *verbatim including
    `HeaderMessageID`*, so without this every child would share the parent id — and the Aggregator (which dedups group
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
    // Remove deletes group key and RETURNS the group it removed (nil if the key
    // was absent). Release calls it after forwarding; the reaper calls it and
    // routes the returned members to the expired channel — so the reaper routes
    // exactly what it atomically removed, never a stale Expired snapshot (audit
    // R1 H-1).
    Remove(ctx context.Context, key string) (MessageGroup, error)
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
func NewAggregator[A, B any](
    store MessageGroupStore,                                              // positional (mirrors NewQueueChannel(store)); nil → ErrNilStore
    fn func(ctx context.Context, group []Message[A]) (Message[B], error), // N→1; nil → ErrNilFunc
    opts ...AggregatorOption,
) (*Aggregator, error)
func (a *Aggregator) Handle(ctx context.Context, msg Message[any]) error // MessageHandler (called by upstream)
func (a *Aggregator) Run(ctx context.Context) error                     // expiry reaper; joined on ctx cancel
```

- **Store is positional, output is a required option** (resolved Spec O9-2 during planning). The `MessageGroupStore`
  is a positional constructor arg — mirroring `NewQueueChannel(store ChannelStore)`, the established "store is the
  substrate" precedent (there is no default in-core store; `memory.GroupStore` lives in `adapter/memory`, which the
  core cannot import). The **downstream destination** for a released aggregate is `WithOutputChannel(ch
  MessageChannel)` — **required** (a released group must have somewhere to go); absent/nil → a construction error
  (`ErrNilOutput`). Keeping *all* the aggregator's channels as options (`WithOutputChannel`, `WithExpiredGroupChannel`)
  and only the store positional is the consistent shape. Rejected: a `Step` form (`Aggregate(...) Step` capturing
  `next`) — it cannot own the reaper's `Run(ctx)` lifecycle (a `Step` has no lifetime hook; a per-message `Handle`
  ctx is request-scoped, wrong for a long-lived ticker), so the struct + explicit output channel is the only clean
  shape (O9-1).
- **Lifecycle struct, not a bare `Step`** — because the expiry reaper is an owned goroutine (no-goroutine-leak
  gate). It implements `MessageHandler` (place as a chain's `next` via `Subscribe`, or as a flow head via
  `NewConsumer[any](src, agg.Handle)`). `Run(ctx)` starts the reaper **only when `WithGroupTimeout` is set**,
  cancellable and joined on ctx cancel (consistent with `Consumer`/`Poller`). With no timeout, no goroutine is
  started and `Handle` works standalone. A nil `fn`, a nil `store`, a missing output channel, or `WithGroupTimeout`
  set without `WithExpiredGroupChannel` (§5), is a **construction error** (returned from `NewAggregator`, the
  debuggability surface).
- **Per-message algorithm (`Handle`):** **assert payload→A** (`ErrPayloadType`, permanent → invalid channel —
  fail-fast on ingress so a mistyped message never pollutes a group) · compute correlation key
  (`WithCorrelationStrategy`, default = `HeaderCorrelationID`; missing/empty → `ErrNoCorrelation` **wrapped
  `Permanent(...)`** — `isPermanent` does not match it by default, so without the wrap the runtime would *retry*
  it to the DLQ instead of routing it to the invalid-message channel, audit R1 M-1 → permanent → invalid channel) · `store.Add(ctx, key, msg)` (durable persist of the `Message[any]`) · release check
  (`WithReleaseStrategy`, default = `len(group) >= HeaderSequenceSize` read **number-tolerantly** per §2's M-1
  contract; `WithCompletionSize(n)` sugar for a fixed count) · **release** → convert the group's stored
  `[]Message[any]` back to `[]Message[A]` (each via `PayloadOf[A]`; re-assert is safe — they passed ingress) →
  `fn(ctx, group)` → **send `Message[B]` to the output channel** → on success `store.Remove(key)` (the removed group
  is ignored on the release path); a `fn`/`Send` error returns **without** removing, so a retry re-releases; **not
  yet** → return nil (message held; source Acks — durability now on the store). All of this runs under the
  per-key lock (Concurrency, below).
- **Concurrency — per-correlation-key serialization (audit R1 H-1).** Serializing the store's individual calls is
  NOT enough: `Handle`'s `Add → release-check → agg → output.Send → Remove` is a multi-call sequence, and under
  `WithConcurrency>1` two `Handle`s for the **same key** could both observe a complete group and both release
  (double-emit), or the coarse key-`Remove` could drop a member that arrived mid-release (data loss). This is a
  **logical** atomicity bug **invisible to `-race`**. The Aggregator therefore holds a **sharded per-key lock**
  (`[N]sync.Mutex`, `key → fnv%N`) across the WHOLE `Handle` sequence for a key; different keys proceed
  concurrently, so Competing Consumers is preserved *across* groups (the useful parallelism — one group's members
  are inherently serial anyway). The **expiry reaper acquires the same per-key lock**, then — because the SPI has no
  keyed read — `Remove`s the key (which returns the removed group) and re-checks the *returned* group: if it is
  `nil` (released concurrently) it skips; if it is no longer expired (a fresh group re-formed at that key since the
  lock-free `Expired()` snapshot) it re-`Add`s the members (idempotent) and skips; otherwise it routes the removed
  members to the expired channel (audit R2 F1). So release and expiry can never both settle one group, and a live
  refilled group is never prematurely expired.
  - **Guarantee (honest):** correct under N>1 **within a single process** (the memory store). **Multi-process
    durable aggregation (Phase 3 `sql.GroupStore`) needs a transactional atomic release** (`DELETE … RETURNING` in
    one tx) — a per-process lock does NOT serialize across processes; recorded as an ADR 0021 requirement.
  - **Footgun (document, audit R2 F2):** the per-key lock is held across `output.Send`/`expired.Send`, which for a
    `DirectChannel` runs synchronously — so wiring an aggregator's output/expired channel back into its own input
    under the **same** correlation key self-deadlocks (non-reentrant mutex). Godoc forbids the cycle.
  - **`==n` caveat (scoped, audit R2 F3):** WITH the per-key lock, exact-count (`== n`) release is **safe in a
    single process** (members serialize per key; the group is removed the instant it hits `n`; idempotent `Add`
    blocks redelivery double-count) — same as `>=`. It is racy only for **multi-process durable** aggregation
    (Phase 3, no cross-process lock) or **id-less** over-delivery; `>=`/`WithCompletionSize` remain the recommended
    default. Documented on `WithReleaseStrategy`.
  - `-race` clean remains a gate, but the **concurrency correctness proof is a dedicated N>1 same-key stress test**
    (single emit, no loss), since `-race` cannot see this class of bug.

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

### 8. Phase-3 revision — uniform lease-claim settlement (SUPERSEDES §2 `Remove` + §3 per-key lock)

The user chose **full multi-process** durability for the durable backend (ADR 0021): two aggregator processes
sharing one `sql.GroupStore` (e.g. Competing Consumers over a `SELECT … FOR UPDATE SKIP LOCKED` source) must never
both emit a group, nor lose one. §3's per-process `[256]sync.Mutex` cannot deliver that — it does not serialize
across processes. This section revises the settlement model accordingly; it applies to **both** stores (the memory
store is reworked to the same shape so there is one Handle path and the concurrency proof needs no DB — two
`*Aggregator` instances sharing one `memory.GroupStore` *is* a faithful cross-process simulation, and a per-instance
lock could never serialize them). Because msgin carries **no release tag**, replacing the shipped `Remove` is not a
SemVer break (unreleased API).

- **SPI: `Remove` → `ClaimGroup`/`SettleGroup`/`AbandonGroup` (a lease).** The store gains an atomic **lease-claim**
  and the coarse key-`Remove` is dropped:
  ```go
  type MessageGroupClaim interface { MessageGroup; Epoch() int64 } // Key/Messages/CreatedAt + fence token
  // ClaimGroup atomically leases the members present at claim time (one winner), returning them + a
  //   fence epoch; nil (no error) if key is absent or already leased. memory and sql differ in how a
  //   lease ends (audit R1 H4/M1): the in-process MEMORY lease is UNCONDITIONAL while held — it is
  //   released only by SettleGroup/AbandonGroup (both synchronous in the claiming goroutine), never by
  //   wall-clock, so it has NO TTL and cannot be stolen from a live holder (a panic mid-release leaks
  //   the lease until process exit, consistent with memory's process-scoped guarantee). The durable SQL
  //   lease is TTL-bounded (store-owned WithGroupLeaseTTL, default 5m) so a CRASHED holder's lease ages
  //   out and another process re-claims — the crash-recovery path memory does not need. Expired excludes
  //   currently-leased groups in both.
  ClaimGroup(ctx context.Context, key string) (MessageGroupClaim, error)
  // SettleGroup fenced-deletes exactly the CLAIMED member set (epoch-fenced); members added during the
  //   lease survive as a fresh live group. Called after a successful output Send.
  SettleGroup(ctx context.Context, claim MessageGroupClaim) error
  // AbandonGroup releases the lease WITHOUT deleting (Send failed, or the reaper found a not-actually-expired
  //   group): the claimed members return to live so a retry / next tick re-releases. Un-tags, never drops.
  AbandonGroup(ctx context.Context, claim MessageGroupClaim) error
  // Expired returns groups the reaper's settlement sweep should re-examine: any group whose LEASE has expired
  //   (a crashed holder — sql) regardless of age, PLUS (when before is non-zero) unleased groups whose CreatedAt
  //   is before the cutoff. Excludes groups under a live lease. Redefined for crash recovery (audit R2 H-A):
  //   the crashed-lease case is how a durable store's stuck complete group is found and re-released.
  Expired(ctx context.Context, before time.Time) ([]MessageGroup, error)
  // RecoverInterval is the cadence at which the reaper must sweep for crashed leases, independent of
  //   WithGroupTimeout (audit R2 H-A). memory returns 0 (unconditional lease — no crash-recovery sweep needed;
  //   a panic mid-release is handled by the Aggregator's defer-abandon). sql returns its lease TTL, so a crashed
  //   holder's group is recovered within ~one TTL even with no expiry timeout configured.
  RecoverInterval() time.Duration
  ```
  `Add` and `EmitsLiveValue` are unchanged (both stores). **`Add` returns the LIVE (unclaimed) members only —
  audit R2 M-C:** memory returns `msgs[claimedLen:]` (the residual when a claim is in flight, else all), matching
  sql's `claimed_epoch IS NULL` snapshot, so the release-check sees the same set on both stores (a member arriving
  during a lease is a fresh-residual member, not part of the in-flight claim).
- **The claim tags a MEMBER SET, it never blind-deletes by key (the Phase-3 correctness crux, analogous to
  Phase-2 H-1 — logical, invisible to `-race`).** After a group is leased but before `SettleGroup`, a **new distinct
  member can still arrive for the same key** — real under an early-release strategy (`WithCompletionSize(3)` when 5
  will come) or an id-less over-delivery (a redelivery is an idempotent `Add` no-op and does not grow the group). If
  settlement deleted the whole `group_key`, that late member — already `Add`-committed and **Acked to the source** —
  would be **silently lost**, a worse-than-documented at-least-once violation (§4). Therefore: `ClaimGroup` freezes
  and tags exactly the members present at claim time; `Add`s during the lease stay **live/untagged**; `SettleGroup`
  deletes **only the tagged claimed set**; a member that arrived during the lease **survives as a fresh live group**
  → re-releases later or expires. This preserves the §4 "late member forms a fresh group" semantics *loss-free*.
  - **memory fences the claimed set by PREFIX LENGTH, not by id (audit R1 H3).** Because `Add` only ever appends,
    the members present at claim time are exactly `msgs[:claimedLen]`; `SettleGroup` deletes that prefix and keeps
    `msgs[claimedLen:]`. An id-map fence (an earlier sketch) never deleted **id-less** members (`ID()==""` was never
    tagged), leaking + re-emitting an all-id-less group — a silent regression from Phase-2's whole-key `Remove`. The
    prefix fence is id-agnostic and correct for both.
  - **sql fences by a `claimed_epoch` marker, and MUST re-absorb a superseded claim's members (audit R1 H2).** A
    re-claim after a crashed holder's lease expiry bumps the group epoch and tags members
    `WHERE claimed_epoch IS NULL OR claimed_epoch < <new group epoch>` — the group-row lease fence guarantees a
    single active claim, so any `claimed_epoch` below the current epoch is a **dead claim** safe to re-absorb.
    Tagging only `claimed_epoch IS NULL` would orphan the dead holder's members forever (loss). See ADR 0021 §4.
  - **Multi-process completion detection must serialize same-key adds (audit R1 H1).** On sql under READ COMMITTED,
    two processes each adding one member of a size-N group see only their own member (the other's `INSERT` is
    uncommitted) → **neither observes completion** → the group sits complete-but-undetected (mis-expired, or stuck).
    `AddMember` therefore takes a **row lock on the group row** (`SELECT … FOR UPDATE`) before snapshotting, so the
    completing add observes all committed members. memory is immune (its single mutex serializes adds), which is why
    the memory cross-process stress test cannot prove this — the **sql conformance** must (ADR 0021 §6). See ADR 0021 §4.
  - **Message ids are REQUIRED for durable/multi-process aggregation.** The sql schema keys members by
    `PRIMARY KEY(group_key, msg_id)`, so id-less members (`msg_id=""`) collapse to one row — unsupported on the
    durable path (documented; source-delivered messages and Splitter children always carry ids). memory tolerates
    id-less members (prefix fence) but without redelivery-dedup (idempotent `Add` is by id).
- **Aggregator: drop the per-key lock; the store's atomic claim is the single serialization point.** `Handle`
  becomes `assert → correlate → Add → release-check(live snapshot) → ClaimGroup → (nil ⇒ held, return nil) → release`.
  **`release` uses a `defer`-abandon-unless-settled (audit R2 M-G):** it runs `agg(claim members) → output.Send →
  SettleGroup` and defers `if !settled { AbandonGroup }`, so an `agg`/`Send` **error** OR a **panic** (recovered by
  the driving `Consumer`) always releases the lease — otherwise a panic mid-release would wedge the correlation key
  forever on memory (no TTL to age it out). **After a successful settle, `release` DRAINS the key (audit R3 H1):** it
  loops `ClaimGroup(key)` and re-releases while a *complete* residual remains (stopping when the claim is nil or not
  release-satisfied). A residual forms when members arrive during a holder's lease; after the settle it may itself be
  complete, yet nothing else re-checks it (the recovery sweep surfaces only crashed *leases*, not unleased-complete
  residuals), so under count-based release + a recurring key + no expiry timeout a completed residual would be
  **silently stranded** (both members Acked, never emitted). The drain closes the common case, but it is **not** a
  full guarantee on its own under concurrency (audit R4 NIT): a residual can still be left complete-but-untriggered
  by a crash mid-drain, OR by a concurrent `Add` landing during the drain's speculative claim-then-abandon of an
  *incomplete* residual (m5 arrives while the drainer holds {m3,m4}; the drainer abandons {m3,m4}, the live group is
  now {m3,m4,m5} = complete but both callers have returned). Neither loses data (at-least-once holds — every member
  survives, un-Acked or Acked-into-the-store), and both are recovered by the reaper's age-scan (release-check →
  **OUTPUT**) — so **count-based recurring-key aggregation under Competing Consumers should set `WithGroupTimeout`**,
  which is the actual guarantee-bearer there; the drain just makes the common path immediate. The canonical
  Splitter→Aggregator (exactly-N-once) never forms a residual and needs none of this.
  The `[256]sync.Mutex`/`keyLock` is **removed** — it could not serialize
  two Aggregator instances over one store, and the store claim now provides exact-once emit within *and* across
  processes. **Bonus:** no lock is held across `output.Send`, so §3's "cyclic wiring self-deadlocks" footgun (F2)
  disappears (a cycle may still recurse, but no longer deadlocks a non-reentrant mutex).
- **The reaper is a RECOVERY + expiry sweep, and running it is REQUIRED for durable crash-recovery (audit R2 H-A —
  the central fix).** A lease-based durable store, exactly like the sql `Source`, needs a periodic sweep to reclaim a
  **crashed holder's** lease — there is no other trigger (the crashed group's members are all tagged, so the
  release-check live snapshot is empty and a redelivered final member is Acked-and-dropped as a trigger; nothing
  re-fires `Handle`). So `Run(ctx)`'s sweep, per tick, for each group from `Expired(cutoff)` (crashed-lease groups
  always + age-old unleased groups when a timeout is set): `ClaimGroup → nil⇒skip → release-strategy satisfied?
  **re-aggregate + Send to the OUTPUT channel + SettleGroup (RECOVERY)** : createdAt<cutoff? route members to the
  EXPIRED channel + SettleGroup : AbandonGroup`. This is the fix for the round-2 hole: a crashed **complete** group is
  re-emitted to **output** (not mis-routed to the expired sink, and not stuck forever). Tick cadence =
  min-positive of `WithGroupTimeout` and `store.RecoverInterval()`; the reaper runs whenever **either** is set, so a
  durable store (RecoverInterval = its lease TTL > 0) gets recovery sweeps even with no expiry timeout. **Using the
  durable `sql.GroupStore` for multi-process safety therefore REQUIRES `go agg.Run(ctx)`** (documented) — for the
  memory store `Run` is only needed for expiry (RecoverInterval = 0). A fresh residual re-formed at the key since the
  lock-free `Expired()` snapshot is abandoned (its `createdAt` is not before the cutoff), never expired-routed.
- **Lease recovery = at-least-once, never loss (the trap the naive design falls into).** A crash between
  `ClaimGroup` and `SettleGroup` leaves the group **leased but present**; the lease expires (`leaseTTL`) → the
  **reaper's recovery sweep** (above) on another process (or a restart) re-claims → re-releases to output → a
  **duplicate** downstream message. A naive `DELETE … RETURNING`-before-Send would instead **lose** the group
  (deleted, its members' source messages already Acked, crash before Send) — rejected. The lease is the loss-free
  shape, mirroring the sql Source's lease/fence/epoch model (ADR 0010/0018).
- **`==n` / exact-count caveat (revised):** WITH the store-level atomic claim, exact-count release is safe **across
  processes** too (the claim admits one winner; the claimed set is fenced; a redelivery is an idempotent no-op) —
  strictly stronger than §3's single-process-only claim. `>=`/`WithCompletionSize` remain the recommended default
  for id-less streams.
- **Guarantee (final):** at-least-once **across processes and restarts** — exactly-one emit under concurrent
  completion (store claim), **duplicate-on-crash via the reaper's recovery sweep re-emitting to output** (lease
  re-claim; requires `Run`), **never loss** (lease not delete; late members survive; dead-claim members re-absorbed).
  The `-race` gate stays; the correctness proof is **split by concern** (audit R1 M4 / R2 H-A): the
  **two-Aggregators-one-`memory.GroupStore` stress test** proves in-process claim/settle **atomicity** (single emit,
  no loss, late-member survives) — it is NOT a faithful proxy for the sql add/claim/recovery behaviour (memory's
  mutex-serialized `Add` and in-process lease hide H1/H2/H-A), so the **sql testcontainers conformance** MUST
  additionally cover the multi-process/durable-only cases: concurrent-first-add completion detection (H1),
  stale-epoch crash-recovery re-claim (H2), and **a crashed-mid-release complete group re-emitted to the OUTPUT
  channel by the recovery sweep** (H-A — assert output, not just "no orphan rows"). Both stores share one case list
  where semantics are meant to be identical (live-only Add snapshot, late-member survival,
  claim-returns-nil-when-leased, Expired-excludes-live-leased).
- **Documented edges (audit R1 L1/L2):** the release decision is made on the `Add` snapshot but `ClaimGroup` freezes
  whatever is present at claim time, so a **non-monotonic** custom `WithReleaseStrategy` (true only transiently) may
  aggregate a slightly different set than it "decided" on — documented on `WithReleaseStrategy`. Dropping the per-key
  lock removes the F2 deadlock, but wiring an output/expired channel back to the same input under the same key still
  **recurses unboundedly** on a `DirectChannel` — the godoc keeps a warning (recursion, no longer a deadlock).

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
- **(Phase 3) Per-process per-key lock for multi-process safety** (§3's Phase-2 shape). Rejected: a per-process
  mutex does not serialize two processes sharing a durable store, so it cannot prevent a cross-process double-emit.
  Superseded by the store-level lease-claim (§8).
- **(Phase 3) Atomic `DELETE … RETURNING`-before-Send as the claim** (multi-process-safe, one statement). Rejected:
  it trades the duplicate-on-crash for **data loss** — a crash in the delete→Send window loses a group whose
  members' source messages were already Acked. The lease (§8) keeps it at-least-once (crash ⇒ lease expiry ⇒
  re-claim ⇒ duplicate), never loss.
- **(Phase 3) Blind `SettleGroup` by `group_key`** (delete the whole group on release). Rejected: a distinct member
  arriving during the lease window would be deleted after being Acked — silent loss. The claim tags and settles only
  the claimed member set; late members survive as a fresh group (§8).
