# Spec 009 — Splitter + Aggregator endpoints (durable group store, expr sugar)

- **Status:** Draft (2026-07-21) — brainstormed with the user; scope and the dominant design forks settled
  interactively (see below). **Phase 1 (Splitter, Plan 015) + Phase 2 (Aggregator + `MessageGroupStore` SPI +
  `memory.GroupStore`, Plan 016) SHIPPED & MERGED to `main`** (`e4b346d`, `94cda1f`). **Phase 3 (`sql.GroupStore`,
  full multi-process — Plan 017 / ADR 0021 + the ADR 0020 §8 revision) is implemented (Plan 017 Tasks 1-4 done:
  the lease-claim SPI reshape, `sql.GroupStore` + `GroupDialect`, per-engine dialects, 4-engine conformance, and
  this finalize task) on branch `feat/sql-groupstore`, pending the whole-branch `/code-review` + `/security-review`
  gate and merge to `main`**: the user chose full multi-process durability, which reopened the Phase-2 settlement
  into a store-level lease-claim (SPI `Remove` → `ClaimGroup`/`SettleGroup`/`AbandonGroup`; `memory` reworked;
  per-key lock dropped) — see §3.4 (revised). **Phase-1 (Splitter) adversarial audit round 1 folded** (Opus): H-1 deterministic
  child id `parentID#seq` (D2) — closes an id-collision that would silently drop the Splitter→Aggregator round-trip;
  M-1 durable `int`/`float64` sequence-header contract (D2); L-1/L-2/L-3 into Plan 015. **Round-2 re-audit: SOUND-WITH-NITS** — the deterministic
  child-id fix verified collision-free/idempotent and inert in the retry path; Phase-1 (Splitter) bundle ready to
  implement (Plan 015). Later phases' plans (016–018) audited when authored. Numbered plans phase the work.
- **Decisions settled with the user (2026-07-20):**
  - **Scope** = build the **Splitter (1→N)** and **Aggregator (N→1)** endpoints **and** their expr variants,
    co-designed in one governing spec ("endpoints + expr sugar together").
  - **Aggregator state/durability** = **durable now** — an in-core `MessageGroupStore` SPI with a `memory` reference
    impl **and** a `sql` durable impl, using **hold-until-release settlement** (true at-least-once across restart).
  - **Incomplete-group expiry** = **opt-in** `WithGroupTimeout(d)` → `WithExpiredGroupChannel(ch)`; **no default
    timeout** (documented "off" state). Never silent-drop, never partial-emit (CLAUDE.md safe-default gate).
  - **Expr breadth** = **full** — `TransformExpr`, `SplitExpr`, and the canonical SpEL aggregator use
    (correlation-by-expr, release-by-expr, aggregate-by-expr).
- **Governing product spec:** [Spec 001 — Messaging core](001-messaging-core.md) §1 (the deferred routing endpoints:
  "Splitter, Aggregator"). Directly continues [Spec 003 — Composition endpoints](003-composition-endpoints.md), which
  built the linear endpoints (Transformer/Filter/Router/Service Activator) and **explicitly deferred Splitter +
  Aggregator to a "next increment"** (Spec 003 §2 non-goals, §8). This spec is that increment.
- **Builds on:** [ADR 0013 — Composition endpoints](../adrs/0013-composition-endpoints.md) (the
  `MessageHandler`/`MessageChannel`/`Step`/`Chain` backbone these endpoints compose into),
  [ADR 0001 — Payload typing](../adrs/0001-message-payload-typing.md) (C-core/A-sugar), and
  [ADR 0018 — Persistent queue channel](../adrs/0018-persistent-queue-channel.md) / Spec 007 (the `ChannelStore`
  SPI + `memory`/`sql` split this spec's `MessageGroupStore` deliberately mirrors — `store.go` already anticipates
  "a future group-keyed `MessageGroupStore`"). Aggregator expr reuses the `compile[A]` primitive from
  [ADR 0019 — Runtime expression evaluation](../adrs/0019-runtime-expression-evaluation.md) / Spec 008.
- **New ADRs (to be authored):** **ADR 0020** (aggregation model: `MessageGroupStore` SPI, correlation/release/expiry
  strategies, hold-until-release settlement + at-least-once contract, the splitter sequence-header convention).
  **ADR 0021** (`sql.GroupStore` — schema + dialect coverage + at-least-once-across-restart proof). expr-on-endpoints
  is recorded as an **addendum to ADR 0019** (no new dependency; expr already in-core).

## 1. Motivation

Spec 003 delivered the **linear** composition endpoints and the `MessageHandler`/`MessageChannel`/`Chain` backbone,
and named the two **non-linear routing endpoints** as its explicit next-increment non-goal: the **Splitter** (one
message fans into many) and the **Aggregator** (many correlated messages collapse into one). Together they are the
EIP "scatter-gather" / "composed message processor" core — a Splitter breaks a batch into items, downstream steps
process each, and an Aggregator reassembles the results. Without them msgin can transform, filter, and route a
single message but cannot express **item-level processing of a batch** or **correlated fan-in**.

The Aggregator is the hardest endpoint in the EIP set: it is **stateful** (it holds messages until a group is
complete), which forces three sub-decisions msgin has otherwise avoided — **where partial groups live** (a message
store), **when a group is complete** (correlation + release strategy), and **what happens to a group that never
completes** (expiry). It also raises a **settlement** question the project already knows from `QueueChannel`
(Spec 007): a held message looks "consumed" to the source, so *who Acks and when*.

Per the user, this increment resolves all of that at the durable end of the spectrum — a real `MessageGroupStore`
SPI with a `memory` reference and a `sql` durable backend, hold-until-release settlement, and opt-in expiry — plus
the **runtime-expression** variants (`expr`) of all three transformation-group endpoints, so item projection,
splitting, and correlation/release can be authored as config strings (Spring's SpEL role), reusing the `compile[A]`
primitive from Spec 008.

## 2. Goals / Non-goals

### Goals

- **G1 — Splitter (`Split[A,B]`).** A 1→N endpoint: assert payload→A, call the user split function, forward each
  child message to `next` **in order**, stamping EIP sequence headers so a downstream Aggregator can reassemble.
  Stateless; synchronous-direct (end-to-end at-least-once for free). Ships **first** (Phase 1), usable standalone.
- **G2 — `MessageGroupStore` SPI (core).** A narrow, swappable interface for durable partial-group storage —
  the extension seam that makes aggregation in-memory or persistent — mirroring `ChannelStore`. `memory` reference
  impl in Phase 2; `sql` durable impl in Phase 3. The core never imports an impl.
- **G3 — Aggregator (`NewAggregator[A,B]`).** An N→1 endpoint with correlation, release, aggregation, and expiry
  strategies (opinionated safe defaults, all overridable). **Hold-until-release settlement** giving documented
  at-least-once (within-process on `memory`, across-restart on `sql`). Owns its expiry-reaper goroutine via
  `Run(ctx)` (cancellable, joined, goleak-clean).
- **G4 — `sql.GroupStore` (durable).** A `MessageGroupStore` over `database/sql` with the existing `Dialect` seam;
  PostgreSQL + MySQL + MariaDB + SQLite; idempotent add, key-scoped remove, expiry query. At-least-once across
  process restart.
- **G5 — expr sugar (reuses `compile[A]`).** `TransformExpr[A,B]` (payload projection), `SplitExpr[A]`
  (expr → collection to split), and Aggregator `WithCorrelationExpr[A]` / `WithReleaseExpr` / aggregate-by-expr
  — the canonical SpEL aggregator use. Each compiles once at construction; a bad expr is `ErrInvalidExpression`.
- **G6 — Zero new dependencies.** All of the above uses only the current core dep set (`expr` already in-core;
  `clockwork` for the reaper). `sql.GroupStore` adds no dep beyond the existing `sql` module.
- **G7 — Full reliability reuse.** Every endpoint is a `MessageHandler`/`Step`, so a composed flow driven by the
  existing `Consumer` inherits retry/DLQ/invalid-message/flow-control/worker-pool unchanged. No per-endpoint error
  handling is duplicated.

### Non-goals (this increment)

- **Resequencer** (order-restoring sibling of the Aggregator) — deferred; the sequence headers this spec stamps make
  it a clean future addition.
- **Messaging Gateway** (synchronous request-reply bridge) — still the increment after (Spec 003 §2 ordering).
- **A `redis`/`pgx`/`nats` `MessageGroupStore`** — the SPI is the seam; other backends are future modules (like the
  deferred `ChannelStore` backends, Spec 007 O7-1).
- **Exactly-once / dedup-of-late-members after completion** — v1 is at-least-once with a **documented** "late
  message after completion" caveat (§4.3); a completed-group marker/dedup layer is a future refinement.
- **`WithExprEnv` custom functions/variables** (Spec 008 O8-3) — remains deferred; the aggregator group-env is a
  fixed, documented shape, not a general extension point.
- No change to the adapter SPI, the external-adapter settlement contract, or message immutability.

## 3. Decisions

### 3.1 Splitter (Phase 1)

- **D1 — Shape.** `func Split[A, B any](fn func(ctx context.Context, m Message[A]) ([]Message[B], error)) Step`.
  **Option-free in v1** (matching `Transform`) — sequence-stamping is unconditional (D2/O9-5), so there is no genuine
  option; an unused `SplitOption` type would be dead surface (YAGNI). Materialized slice (not an iterator) for v1 —
  simplest, matches the synchronous-direct model; an `iter.Seq`-based streaming split is a documented future. A nil
  `fn` → `ErrNilFunc` (no panic on input).
- **D2 — Sequence-header + child-identity convention (EIP; audit R1 H-1/M-1).** New reserved keys
  `HeaderSequenceNumber` ("msgin.sequence-number", 1-based `int`) and `HeaderSequenceSize` ("msgin.sequence-size",
  `int` = N). On every child the Splitter stamps both sequence headers, a **deterministic child `HeaderID` =
  `parentID#seq`** (only when the parent has an id), and `HeaderCorrelationID` = the **parent's id** *only if the
  child does not already carry one* (never overwrites a caller-set/inherited correlation).
  - **Child id is load-bearing:** the canonical constructor `WithPayload(parent, x)` copies the parent's headers
    verbatim *including `HeaderID`*, so absent this every child would share the parent id and the Aggregator (which
    dedups members by `msg.ID()`, D8) would collapse the group to one member → it never releases → silent loss. A
    `parentID#seq` id is unique within a split (group fills) yet stable across a parent redelivery (idempotent Add).
  - **Durable type contract (M-1):** the default release reads `HeaderSequenceSize` **number-tolerantly**
    (`int`/`int64`/`float64`) and `sql.DecodeHeaders` restores the two sequence headers to `int` (Phase 3), because
    the sql JSON framing otherwise returns them as `float64` — else groups never reach the size check on the durable
    store.
  These make the Splitter↔Aggregator round-trip work with zero extra config on both the memory and sql paths.
- **D3 — Empty split.** `fn` returning an empty/nil slice forwards nothing and returns nil (a valid "nothing to
  split", the Filter-drop analogue) — not an error.
- **D4 — Settlement.** All N children are forwarded on the delivery goroutine, in order, before `Handle` returns.
  The driving `Consumer` therefore Acks the source only after **every** child succeeds → end-to-end at-least-once,
  exactly like `Chain`. A child error propagates immediately (remaining children not sent) → source Nack → the
  **whole parent** is redelivered → children reprocessed. Children must be idempotent downstream (**documented**).

### 3.2 `MessageGroupStore` SPI (Phase 2, core)

- **D5 — Interface (new `groupstore.go`, mirrors `ChannelStore`).**
  ```go
  type MessageGroup interface {
      Key() string                 // the correlation key
      Messages() []Message[any]    // members in arrival order
      CreatedAt() time.Time        // first-member arrival, for expiry
  }
  type MessageGroupStore interface {
      // Add durably persists msg into group key and returns the current group.
      // Idempotent by msg id: re-adding an already-stored member is a no-op
      // (upholds at-least-once under source redelivery).
      Add(ctx context.Context, key string, msg Message[any]) (MessageGroup, error)
      // Remove deletes an entire group (called after a successful release).
      Remove(ctx context.Context, key string) error
      // Expired returns groups whose CreatedAt precedes before (for the reaper).
      Expired(ctx context.Context, before time.Time) ([]MessageGroup, error)
      // EmitsLiveValue reports live Go values (memory) vs []byte (wire), for codec pairing.
      EmitsLiveValue() bool
  }
  ```
- **D6 — `memory.GroupStore`.** `map[string]*group` under a mutex; idempotent add by `msg.ID()`; expiry via
  `CreatedAt` scan; carries live values (`EmitsLiveValue() == true`); **starts no goroutine** (the reaper lives in
  the Aggregator, so the store stays goleak-trivial). Within-process at-least-once (partial groups lost on crash).

### 3.3 Aggregator (Phase 2)

- **D7 — Lifecycle struct, not a bare Step.** `func NewAggregator[A, B any](fn func(ctx, []Message[A]) (Message[B],
  error), opts ...AggOption) (*Aggregator, error)`. It **implements `MessageHandler`** (place it as a chain's `next`
  via `Subscribe`, or as a flow head). It owns an **expiry-reaper goroutine started by `Run(ctx)`** — spawned only
  when `WithGroupTimeout` is set, cancellable, and joined on ctx cancel (`goleak`-clean; consistent with
  `Consumer`/`Poller` lifecycle). Without a timeout, no goroutine is started and `Handle` works standalone. A nil
  `fn` → construction error. **Rejected alternative:** a pure `Step` with lazy opportunistic reaping (expiry checked
  inline on each `Add`) — zero goroutines and simpler, but an idle aggregator never reaps a stale group; the
  lifecycle struct reaps on a timer regardless of traffic.
- **D8 — Per-message algorithm.**
  1. Assert payload→A → `ErrPayloadType` (permanent → invalid-message channel) on mismatch.
  2. **Correlation key** via `WithCorrelationStrategy(func(Message[any]) (string, error))`; **default** = the
     `HeaderCorrelationID` value; a missing/empty correlation id → `ErrNoCorrelation` (permanent → invalid channel;
     the safe, visible default — an uncorrelatable message is a misconfiguration, not silently grouped).
  3. `store.Add(ctx, key, msg)` — **durably persists before anything downstream**.
  4. **Release check** via `WithReleaseStrategy(func(MessageGroup) bool)`; **default** = `len(group.Messages()) >=`
     the group's `HeaderSequenceSize` (from the Splitter). `WithCompletionSize(n)` is sugar for a fixed count when
     there is no sequence-size header. If neither applies, the group relies on expiry.
  5. **Release** → `fn(ctx, group)` → forward `Message[B]` to `next`; on success `store.Remove(key)`. **Not yet** →
     return nil (message held; the source will Ack — durability now rests on the store).
- **D9 — Hold-until-release settlement / at-least-once (the crux).** Ordering guarantees the contract:
  - `store.Add` **commits before** `Handle` returns nil (before the source Ack). Crash in between → the source
    redelivers → idempotent `Add` (no duplicate member). **Durability hands off source→group-store.**
  - Release **forwards before** `store.Remove`. Crash in between → the group survives → it re-releases on restart
    (or on the next member / reaper tick) → **duplicate downstream** message. This is at-least-once, documented.
- **D10 — Expiry (safe-default, user's choice).** **No default timeout** — a documented "off" state (groups held in
  the store until complete, bounded by store capacity; per CLAUDE.md, no timeout value can be safe for an unknown
  caller, so it is explicit/opt-in). `WithGroupTimeout(d)` (via `clockwork`) + `WithExpiredGroupChannel(ch)`: the
  reaper periodically calls `store.Expired(now-d)` and routes each expired **partial** group to `ch`, then removes
  it. Never a silent drop; never a partial emit downstream. If `WithGroupTimeout` is set without
  `WithExpiredGroupChannel`, that is a **construction error** (an expiry that drops data without a visible sink is a
  footgun) — expiry demands a destination.

### 3.4 `sql.GroupStore` (Phase 3) — FULL multi-process (revised 2026-07-21)

The user chose **full multi-process** durability (not merely single-process-durable). This **reopens the Phase-2
settlement**: a per-process lock cannot serialize two aggregator processes sharing one store, so atomicity moves
**into the store** as a lease-claim. The full model is [ADR 0020 §8](../adrs/0020-splitter-aggregator-group-store.md);
the durable realization is [ADR 0021](../adrs/0021-sql-group-store.md). Summary:

- **D11 (revised) — SPI reshape: `Remove` → lease-claim.** The `MessageGroupStore` SPI drops `Remove` and gains
  `ClaimGroup(ctx, key) (MessageGroupClaim, error)` (atomic lease of the members present at claim time; nil if
  absent/already-leased; the lease TTL is store-owned via `WithGroupLeaseTTL`, sql default 5m, memory moot),
  `SettleGroup(ctx, claim)` (fenced delete of **only the claimed set**), and
  `AbandonGroup(ctx, claim)` (release the lease without deleting). **`memory` is reworked to the same shape** so
  there is one Handle path and the concurrency proof is a DB-free two-`*Aggregator`-one-`memory.GroupStore` stress
  test. The Aggregator's per-key `[256]sync.Mutex` is **removed** — the store's atomic claim is the single
  serialization point (within and across processes). msgin has **no release tag**, so replacing `Remove` is not a
  SemVer break. **The claim tags a member set and never blind-deletes by key** — a member arriving during the lease
  survives as a fresh group (loss-free; the Phase-3 correctness crux, ADR 0020 §8).
- **D11a — `sql.GroupStore` schema + `GroupDialect` (ADR 0021).** Two tables: `msgin_group(group_key PK, created_at,
  epoch, locked_by, locked_at)` (per-group lease + expiry clock) and `msgin_group_member(group_key, msg_id, seq,
  headers, payload, claimed_epoch, PK(group_key,msg_id))` (append-only; `claimed_epoch NULL` = live). `Add` =
  idempotent upsert; `ClaimGroup` = fenced lease on the group row + tag members with the epoch; `SettleGroup` =
  delete `claimed_epoch = epoch` rows (fenced); `Expired` = `created_at < ? AND not-leased`. A **new segregated
  `GroupDialect`** (like `InboxDialect`, not the row-oriented `LeaseDialect`) with built-ins
  `postgres`/`mysql`(MariaDB)/`sqlite`, proven via `RunTestDatabase` on all four engines. `EmitsLiveValue() ==
  false` (wire). Lease TTL default 5m (`WithGroupLeaseTTL`). At-least-once **across restart and across processes**;
  crash in the claim→settle window recovers via lease expiry → re-claim → duplicate (never loss).

### 3.5 expr sugar (Phase 4)

- **D12 — `TransformExpr[A, B](expression string, opts ...) (Step, error)`.** Projection expr evaluated to a value
  asserted to B. Adds an `exprAny` output kind to `compile` (no `AsKind` constraint; the result is asserted to B at
  eval, a non-B result → evaluation error → retry/DLQ). Reuses `WithPayload` for header propagation.
- **D13 — `SplitExpr[A](expression string, opts ...) (Step, error)`.** Expr evaluated to a slice; each element is
  wrapped (via `WithPayload` off the parent) into a child, then handed to the Phase-1 Split machinery (sequence
  headers included).
- **D14 — Aggregator exprs.** `WithCorrelationExpr[A](expr)` reuses the existing `exprString` compile (per-message
  string key). `WithReleaseExpr(expr)` and aggregate-by-expr evaluate against a **new fixed group-scoped env**
  `{ messages []A, size int }` (`expr:"messages"`, `expr:"size"`) — the one genuinely new expr surface; documented,
  not a general extension point (`WithExprEnv` stays deferred, Spec 008 O8-3).

### 3.6 Cross-cutting

- **D15 — Reliability reuse.** Splitter and the expr steps are `Step`s; the Aggregator is a `MessageHandler`. All
  compose into the existing `Chain`/`Consumer` runtime — retry/DLQ/invalid-message/flow-control/worker-pool apply
  unchanged, and permanent errors (`ErrPayloadType`, `ErrNoCorrelation`, `ErrInvalidExpression` at eval) route to
  the invalid-message channel exactly as the linear endpoints do.
- **D16 — Concurrency (corrected, audit R1 H-1; Phase-2 guarantee — SUPERSEDED for Phase 3 by D11 revised / §3.4).**
  The per-key lock and its "multi-process needs a transactional atomic release" caveat below describe the Phase-2
  (`memory`-only) mechanism; Phase 3 replaced it entirely with the store-level lease-claim
  (`ClaimGroup`/`SettleGroup`/`AbandonGroup`, §3.4/ADR 0020 §8), which serializes both within AND across processes
  and removed the per-key lock — see §3.4 for the shipped guarantee. Kept below for history. Serializing the
  store's *individual* calls is **not** enough —
  `Handle`'s `Add → release-check → aggregate → Send → Remove` is a multi-call sequence, so under a worker pool two
  `Handle`s for the **same key** could both release a complete group (double-emit) or lose a member that arrived
  mid-release (a logical bug **invisible to `-race`**). The Aggregator therefore holds a **sharded per-correlation-key
  lock** across the whole `Handle` sequence, and the reaper acquires the same per-key lock around its re-check +
  `Remove`-returns-group + route (so a group can never be both released and expired). Different keys run
  concurrently → Competing Consumers preserved across groups. **Guarantee: correct under `WithConcurrency > 1`
  within a single process** (memory store); **multi-process durable aggregation (Phase 3 `sql.GroupStore`) requires
  a transactional atomic release** (`DELETE … RETURNING`), a per-process lock being insufficient across processes.
  Exact-count (`== n`) release is safe in a single process WITH the per-key lock (racy only multi-process/id-less);
  `>=`/`WithCompletionSize` remain recommended. `-race` remains a gate, but a dedicated **N>1 same-key stress test**
  (single emit, no loss) is the correctness proof `-race` cannot provide.

## 4. Architecture

```
Phase 1  Splitter (stateless)         Split[A,B](fn) Step  ── stamps sequence headers ── forwards N children to next
Phase 2  Aggregator core + SPI        NewAggregator[A,B](store, fn, opts) (*Aggregator, error)   (MessageHandler + Run(ctx) reaper)
             │ correlation/release/expiry strategies (opinionated defaults, all WithX-overridable)
             └── MessageGroupStore SPI (core) ── memory.GroupStore (reference, within-process)
Phase 3  sql.GroupStore (durable)     database/sql + Dialect ── PG/MySQL/MariaDB/SQLite ── at-least-once across restart
Phase 4  expr sugar (reuses compile)  TransformExpr[A,B] · SplitExpr[A] · WithCorrelationExpr/WithReleaseExpr/aggregate-expr
```

### 4.1 Splitter data flow (Phase 1)
```go
flow := msgin.Chain(
    msgin.Split(func(ctx context.Context, m msgin.Message[Batch]) ([]msgin.Message[Item], error) {
        out := make([]msgin.Message[Item], 0, len(m.Payload().Items))
        for _, it := range m.Payload().Items {
            out = append(out, msgin.WithPayload(m, it)) // correlation-id/headers preserved
        }
        return out, nil                                 // Split stamps sequence-number/size + correlation-id
    }),
    msgin.Activate(processItem),                        // each item processed independently
    msgin.To(sink),
)
```

### 4.2 Aggregator data flow (Phase 2/3)
```go
store, err := memory.NewGroupStore()                    // or sql.NewGroupStore(db, table, dialect) for durability
agg, err := msgin.NewAggregator(
    store,                                               // required: the MessageGroupStore (memory or sql)
    func(ctx context.Context, group []msgin.Message[Item]) (msgin.Message[Result], error) {
        return msgin.New(reduce(group)), nil            // N→1
    },
    msgin.WithOutputChannel(sink),                      // required: where a released aggregate is sent
    msgin.WithGroupTimeout(30*time.Second),             // opt-in expiry …
    msgin.WithExpiredGroupChannel(timeoutCh),           // … with a visible sink (required together)
)
_ = agg.Handle                                          // it IS a MessageHandler (chain next / flow head)
go agg.Run(ctx)                                         // reaper; joined on ctx cancel
```

### 4.3 Documented at-least-once caveat (D9)
A member redelivered **after** its group has already completed and been removed forms a *fresh* group under the same
key (the classic "late message after completion"). v1 is at-least-once, so downstream must be idempotent or the
release strategy must tolerate a singleton/partial re-group. A completed-group dedup marker is a future refinement
(§2 non-goals).

## 5. Testing posture (per CLAUDE.md + custom skills)

- **Blackbox `_test` packages**, exported surface only; **assert-closure tables** (`table-test`); `t.Context()`;
  `clockwork.NewFakeClock()` to drive the reaper deterministically; **`goleak`** on the Aggregator (`Run`/reaper
  start/stop, and the timeout-unset no-goroutine path); `Example…` tests double as godoc for `Split`, the
  Aggregator, and each expr constructor.
- **Hot-path branch coverage (mandatory, enumerated per endpoint):**
  - *Splitter:* payload ok/`ErrPayloadType`; nil `fn`→`ErrNilFunc`; N=0 (forward nothing); N>0 order + sequence
    headers stamped; child-preexisting correlation-id preserved vs parent-id fallback; mid-child error propagates
    (remaining not sent).
  - *Aggregator:* correlation default hit / missing→`ErrNoCorrelation`; custom correlation strategy; release
    default (size reached) / not-reached (held) / `WithCompletionSize`; custom release strategy; aggregate `fn`
    error propagates (group NOT removed); `store.Add` error; `store.Remove` after successful forward; idempotent
    re-Add (redelivery); expiry off (held) vs on→routed to expired channel + removed; `WithGroupTimeout` without
    `WithExpiredGroupChannel`→construction error; nil `fn`→construction error; `Run`/reaper start+stop+join.
  - *`MessageGroupStore` (memory + sql, shared conformance table):* Add returns growing group; idempotent Add by
    id; Remove; Expired boundary (`before` inclusive/exclusive); EmitsLiveValue value; empty-group Remove no-op.
  - *expr:* `TransformExpr` compile error→`ErrInvalidExpression`; eval→B / non-B result error; `SplitExpr`
    →slice / non-slice error; `WithCorrelationExpr`/`WithReleaseExpr` compile + eval branches.
- **Settlement / durability tests.** Driven through the existing `Consumer` so the reused reliability path is
  exercised end-to-end: hold-then-release Ack timing (`memory`); crash-simulation across restart (`sql` — a fresh
  store over the same DB re-releases a persisted group); expiry reaping under a fake clock.
- **`use-testcontainers`** — `sql.GroupStore` conformance runs against real PG/MySQL/MariaDB/SQLite via
  `RunTestDatabase`; no mocks/in-memory fakes for the DB.
- **Gates:** `-race` clean; `CGO_ENABLED=0`; `go vet`/`golangci-lint`/`gofmt` clean; `govulncheck` clean;
  `go mod tidy` stable (**no new core dep**; `sql` module graph unchanged). Additive API → **minor SemVer**.

## 6. Sequencing (one plan per phase, each ADR-linked + audited)

1. **Phase 1 — Splitter** (`splitter.go`; `HeaderSequenceNumber`/`HeaderSequenceSize`; sequence-header + settlement
   tests). Ships value first. Plan 015.
2. **Phase 2 — Aggregator core + `MessageGroupStore` SPI + `memory.GroupStore`** (`groupstore.go`, `aggregator.go`;
   correlation/release/expiry strategies; reaper lifecycle; ADR 0020). Plan 016.
3. **Phase 3 — `sql.GroupStore`** (durable; ADR 0021; testcontainers conformance across four dialects). Plan 017.
4. **Phase 4 — expr sugar** (`TransformExpr`/`SplitExpr`/aggregator exprs; ADR 0019 addendum). Plan 018.

Each phase: this spec → its ADR → **adversarial Opus audit of the full spec+ADR+plan bundle** (two rounds is the
project norm) → SDD (fresh implementer per task + adversarial reviewer). **No implementation before the audited
plan is approved and the user gives an explicit per-phase go-ahead** (CLAUDE.md).

## 7. Risks

- **Hold-until-release settlement (D9)** is the genuinely hard design point — the source-Ack-while-held window and
  the release-before-remove duplicate window. Mitigation: the ordering contract is explicit, idempotent `Add`
  closes the first window, and the "late message after completion" caveat is documented, not hidden (§4.3).
- **Reaper goroutine lifecycle** — a leaked or unjoined reaper fails the no-goroutine-leak gate. Mitigation:
  `Run(ctx)` ownership, `goleak` on start/stop/join and on the timeout-unset no-goroutine path.
- **Splitter↔Aggregator coupling via headers** — a fragile string contract. Mitigation: reserved `msgin.`-namespaced
  keys, a default release strategy that reads them, and a round-trip integration test.
- **Surface creep** — held down by deferring Resequencer/Gateway/other group-store backends/late-member dedup (§2),
  and by the fixed (non-extensible) aggregator group-env (D14).
- **sql idempotency/dialect drift** — the upsert semantics differ per dialect. Mitigation: the shared conformance
  table runs on all four real engines (§5).

## 8. Open items (to close in ADR 0020/0021 + the plans)

- **O9-1** — Aggregator lifecycle final shape: `Run(ctx)`-owning struct (recommended, this spec) vs a `Step` with
  lazy opportunistic reaping. Confirm in ADR 0020. *(User leaned to the lifecycle struct in brainstorming.)*
- **O9-2** — Whether `Split`/aggregate expose a channel-wired (branch) form in addition to the `Step`/`Handle`
  form, or the `Step`/`MessageHandler` forms are sufficient (lean: sufficient; channels reached via `Subscribe`).
- **O9-3** (RESOLVED, ADR 0021 §5) — capacity lives on the **store**: `memory.GroupStore` keeps `WithMaxGroups`
  (a new key beyond the cap → `ErrOverflowDropped`, never evicting a partial group); `sql.GroupStore` is
  **DB-bounded** (no in-store cap in v1 — a never-completing-group DoS is handled by expiry reaping + table
  monitoring, matching ADR 0018 §4's sql-is-DB-bounded stance). No `OverflowPolicy` knob on the sql store.
- **O9-4** — expr `SplitExpr` element typing: `[]any` elements asserted per-item to A/B vs a homogeneous `[]A`
  expr result. Confirm in the Phase-4 plan.
- **O9-5** — Whether `Split` should offer a `WithoutSequenceHeaders()` escape for callers who don't aggregate
  downstream, or always stamp (lean: always stamp; the headers are harmless if unused). Confirm in ADR 0020.
- **O9-6** (Phase 2, from audit R2) — **Nested split→aggregate is not covered by the default correlation.** Because
  `Split` never overwrites an existing correlation id, an inner split's grandchildren keep the *outer* correlation,
  so a default-correlation inner Aggregator can't distinguish inner groups (and inner `HeaderSequenceSize` collides
  with outer siblings under the shared key). The escape hatch is `WithCorrelationStrategy` (D8); the Phase-2 plan
  must document this and cover it with a test. Not a Phase-1 concern.

## 9. Traceability

Spec 009 realizes [Spec 001](001-messaging-core.md) §1 (deferred Splitter/Aggregator) and continues
[Spec 003](003-composition-endpoints.md) (its explicit next-increment non-goal). It builds on
[ADR 0013](../adrs/0013-composition-endpoints.md) (composition backbone), [ADR 0001](../adrs/0001-message-payload-typing.md)
(payload typing), [ADR 0018](../adrs/0018-persistent-queue-channel.md) (the `ChannelStore` memory/sql split the
`MessageGroupStore` mirrors), and [ADR 0019](../adrs/0019-runtime-expression-evaluation.md) (the `compile[A]`
primitive the expr sugar reuses). New **ADR 0020** (aggregation model + `MessageGroupStore` SPI + settlement) and
**ADR 0021** (`sql.GroupStore`) record the decisions and cite this spec; expr-on-endpoints is an ADR 0019 addendum.
Plans 015–018 (one per phase) reference Spec 009 + the ADRs; code/commits carry `Spec: 009` / `Plan:` / `ADR:`
trailers so the spec→plan→ADR→commit chain stays greppable.
