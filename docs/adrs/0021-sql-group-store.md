# ADR 0021 — `sql.GroupStore`: durable, multi-process aggregation (lease-claim, `GroupDialect`)

- **Status:** Proposed (2026-07-21) — records the Phase-3 decisions of
  [Spec 009 §3.4](../specs/009-splitter-aggregator-endpoints.md), settled with the user in brainstorming (full
  **multi-process** durability; uniform store-level lease-claim; `memory` reworked to the same shape; new segregated
  `GroupDialect`). **Depends on the ADR 0020 §8 revision** (the `ClaimGroup`/`SettleGroup`/`AbandonGroup` SPI + the
  claimed-set-fencing rule + the dropped per-key lock) — this ADR is the durable realization of that model.
  **Adversarial audit: 4 rounds (Opus), verdict SOUND-WITH-NITS — all findings folded.** R1: H1 completion-detection
  serialization (group-row lock), H2 dead-claim re-absorption, H3 id-requirement/prefix-fence, H4 unconditional
  memory lease. R2: **H-A** crash-recovery-to-OUTPUT via a reaper recovery sweep + `RecoverInterval`, H-B uniform
  group→member lock order, M-C live-only `Add`, M-D SQLite `BEGIN IMMEDIATE`. R3: H1 post-settle drain loop, M1 5m
  lease-TTL default (matches the Source). R4: SOUND-WITH-NITS (drain loop verified; doc-precision nits folded).
  Implementation-ready.
- **Spec:** [Spec 009 — Splitter + Aggregator endpoints](../specs/009-splitter-aggregator-endpoints.md) §3.4 / G4.
- **Plan:** [Plan 017 — `sql.GroupStore`](../plans/017-sql-group-store.md).
- **Depends on / builds on:** [ADR 0020 §8](0020-splitter-aggregator-group-store.md) (the aggregation model + the
  lease-claim SPI this store implements), [ADR 0010](0010-poller-sql-adapter.md) and
  [ADR 0011](0011-sql-engine-dialect-module-split.md) (the sql adapter's lease/claim/fence/epoch model, the
  `Dialect` seam, the per-engine module split, `ValidateIdent`/`Querier`/framing this store reuses),
  [ADR 0012](0012-sqlite-dialect.md) (the SQLite dialect module), [ADR 0018](0018-persistent-queue-channel.md) (the
  `sql.QueueStore` thin-facade precedent and the memory/sql store-swap pattern), and
  [ADR 0001](0001-message-payload-typing.md) (wire framing — `EmitsLiveValue()==false`).

## Context

ADR 0020 delivered the Aggregator and the `MessageGroupStore` SPI with a `memory` reference store: at-least-once
**within one process** (a per-process `[256]sync.Mutex` serialized same-key work). The user's stated requirement is
aggregation that survives a restart **and** is safe when **two processes share the store** — e.g. two aggregator
instances draining one `SELECT … FOR UPDATE SKIP LOCKED` sql source as Competing Consumers. A per-process lock
cannot serialize across processes, so cross-process safety must live **in the store**, transactionally.

ADR 0020 §8 resolves the *model* (a store-level lease-claim: `ClaimGroup`/`SettleGroup`/`AbandonGroup`, claim tags a
member set and never blind-deletes by key, lease-recovery gives at-least-once-not-loss, the Aggregator's per-key
lock is removed). This ADR resolves the **durable realization**: the schema, the new dialect seam, the exact SQL,
the safe defaults, and the four-engine conformance proof. It is *not* a thin facade over the existing sql machinery
(unlike `sql.QueueStore`, ADR 0018) — group-keyed idempotent add, a per-group lease, claimed-set fencing, and an
expiry scan are genuinely new SQL, so it gets its own segregated dialect.

## Decision

### 1. `sql.GroupStore` — a `MessageGroupStore` over `database/sql` + a new `GroupDialect`

```go
func NewGroupStore(db *stdsql.DB, dialect GroupDialect, opts ...GroupStoreOption) (*GroupStore, error)
```

- Implements the full ADR 0020 §8 SPI (`Add`/`ClaimGroup`/`SettleGroup`/`AbandonGroup`/`Expired`/`EmitsLiveValue`/
  `RecoverInterval`). **`RecoverInterval()` returns the store's lease TTL** so the Aggregator's reaper sweeps for
  crashed leases at that cadence even with no `WithGroupTimeout` (audit R2 H-A) — using `sql.GroupStore` for
  multi-process safety therefore **requires** running `go agg.Run(ctx)` (documented on `NewGroupStore`).
  A nil `db` → `ErrNilAdapter`; a nil `dialect` → `ErrNilDialect`; a bad table name → `ErrInvalidTableName`
  (reusing the existing sql errors and `ValidateIdent`). Call `Ready`/`EnsureSchema` once at boot, exactly like the
  Source (ADR 0010 D2) — msgin never runs DDL implicitly on the production path.
- **`EmitsLiveValue()==false`** — a wire store: headers are JSON-framed and payloads are the runtime-codec `[]byte`,
  identical to `sql.Outbound`/`sql.QueueStore`. The paired typed runtime encodes/decodes; the store is
  type-agnostic (ADR 0001).
- **At-least-once across restart AND across processes.** Durability hands off source→group-store on `Add` (before
  the source Acks); a crash between `ClaimGroup` and `SettleGroup` is recovered by lease expiry → re-claim →
  re-release (duplicate, never loss — ADR 0020 §8).

### 2. Schema — two tables (group + members), lease on the group row, claimed-epoch marker on members

```sql
-- one row per correlation group: identity, expiry clock, and the per-group lease
CREATE TABLE msgin_group (
    group_key   <text>      PRIMARY KEY,
    created_at  <timestamp> NOT NULL,      -- first-member arrival (DB server clock), for expiry
    epoch       BIGINT      NOT NULL,      -- fence token, bumped on each ClaimGroup
    locked_by   <text>      NULL,          -- lease holder (NULL = unleased)
    locked_at   <timestamp> NULL           -- lease acquired-at (DB clock); lease expired when now()-locked_at > ttl
);
-- append-only members; claimed_epoch NULL = live/unclaimed, non-NULL = frozen into an in-flight claim
CREATE TABLE msgin_group_member (
    group_key     <text>   NOT NULL,
    msg_id        <text>   NOT NULL,
    seq           BIGINT   NULL,           -- HeaderSequenceNumber, for ordered replay
    headers       <bytes>  NOT NULL,       -- JSON-framed headers (msgin sql framing)
    payload       <bytes>  NOT NULL,       -- runtime-codec wire body
    claimed_epoch BIGINT   NULL,           -- NULL = live; = msgin_group.epoch when claimed
    PRIMARY KEY (group_key, msg_id)        -- idempotent Add by (group_key, msg_id)
);
```

- **Why two tables.** The lease is a **per-group** fact; a per-member table cannot hold it without duplication. The
  group row also carries `created_at`, so the expiry scan is an indexed `WHERE created_at < ?` rather than a
  `MIN(created_at) … GROUP BY` aggregation over members. Members stay append-only (idempotent upsert), which is what
  makes `Add` cheap and redelivery-safe.
- **Why `claimed_epoch` on members (not a blind key-delete).** This is the ADR 0020 §8 claimed-set-fencing rule in
  SQL: `ClaimGroup` stamps `claimed_epoch = epoch` on exactly the live members it leases; a member arriving *during*
  the lease is inserted with `claimed_epoch = NULL` (still live); `SettleGroup` deletes only `claimed_epoch = epoch`
  rows, so the late member survives as a fresh live group. Deleting by `group_key` alone would silently lose it.
- **Message ids are required (audit R1 H3).** Members are keyed `PRIMARY KEY(group_key, msg_id)`, so id-less members
  (`msg_id=""`) collapse to a single row — durable aggregation therefore **requires message ids** (documented on
  `NewGroupStore`). This is not a real restriction: source-delivered messages carry `HeaderID`, and the Splitter
  stamps a deterministic `parentID#seq` child id (ADR 0020 §1). `AddMember` rejects an empty `msgID` with a typed
  error rather than silently collapsing. (The in-process `memory.GroupStore` tolerates id-less members via its
  prefix-length fence, but without idempotent-`Add`-by-id redelivery dedup — ADR 0020 §8.)
- **Ordering / types.** `seq` persists `HeaderSequenceNumber` so a replayed/aggregated group can be ordered by
  sequence, not physical row order. Per ADR 0020 §2 (M-1): `sql.DecodeHeaders` adds `HeaderSequenceNumber`/
  `HeaderSequenceSize` to its `int`-restoration special-case list (mirroring `HeaderDeliveryCount`) so a
  live-value-consuming default release still reads them as `int`; the default release is already `float64`-tolerant
  (`asInt`), so this is a clean-round-trip refinement, not a correctness dependency.

### 3. `GroupDialect` — a new segregated SPI (interface-segregation, like `InboxDialect`)

The existing `LeaseDialect` is **row-oriented** (single-message queue rows). Group aggregation needs group-keyed SQL
that a queue-dialect author must not be forced to implement — and vice versa — so it is a **separate** interface,
exactly as `InboxDialect` is segregated from `LeaseDialect` (ADR 0010 D10, ADR 0011). Built-ins:
`postgres.GroupDialect()`, `mysql.GroupDialect()` (covers MariaDB, wire-compatible), `sqlite.GroupDialect()`, each
in its existing module. Every method owns its full SQL and any transaction orchestration; all timestamps use the DB
server clock (`now()`), durations are interval-typed parameters — no app↔DB skew (ADR 0010 D3/D4). Every method
`ValidateIdent`s the table before quoting/interpolating (the sole injection guard; the identifier cannot be a bound
parameter).

```go
type GroupDialect interface {
    // AddMember, in ONE transaction: upserts the group row (created_at set once via server now()),
    //   takes a group-row lock (SELECT … FOR UPDATE) so same-key adds serialize across processes
    //   (audit R1 H1), upserts the member row (ON CONFLICT(group_key,msg_id) DO NOTHING / INSERT
    //   IGNORE), and returns the current live-member snapshot (for the Aggregator's release check).
    //   An empty msgID is rejected with a typed error (audit R1 H3 — durable aggregation requires ids).
    //   Server clock only; no now param.
    AddMember(ctx, q Querier, table, groupKey, msgID string, seq int64, headers, payload []byte) (GroupRows, error)
    // ClaimGroup atomically leases the group in one transaction: bump epoch + set locked_by/locked_at on the
    //   group row, FENCED on (locked_by IS NULL OR now()-locked_at > leaseTTL) so exactly one worker wins
    //   (winner = rowsAffected==1); RE-ABSORB a superseded claim's members — stamp claimed_epoch=newEpoch on
    //   (claimed_epoch IS NULL OR claimed_epoch < newEpoch) so a crashed holder's members are re-claimed, never
    //   orphaned (audit R1 H2); return the claimed members + the epoch. (nil, nil) if absent or actively leased.
    ClaimGroup(ctx, q Querier, table, groupKey, lockedBy string, leaseTTL time.Duration) (*ClaimedGroup, error)
    // SettleGroup, LOCKING THE GROUP ROW FIRST (SELECT … FOR UPDATE — audit R2 H-B deadlock-order), deletes the
    //   claimed member set (claimed_epoch = epoch, fenced on locked_by); if members remain (late arrivals) it
    //   clears the lease AND resets created_at=now() (residual is a fresh group — M2); else it deletes the group
    //   row — all one tx. Fence miss (re-claimed) → applied=false.
    SettleGroup(ctx, q Querier, table, groupKey, lockedBy string, epoch int64) (applied bool, err error)
    // AbandonGroup, LOCKING THE GROUP ROW FIRST (H-B), releases the lease without deleting: NULL the claimed_epoch
    //   (members return to live) and clear locked_by/locked_at, fenced on (locked_by, epoch). Fence miss → false.
    AbandonGroup(ctx, q Querier, table, groupKey, lockedBy string, epoch int64) (applied bool, err error)
    // ExpiredGroups returns groups the reaper must re-examine (audit R2 H-A): any group whose LEASE has expired
    //   (locked_by IS NOT NULL AND now()-locked_at > leaseTTL — a crashed holder) regardless of age, PLUS (when
    //   before is non-zero) unleased groups with created_at < before; EXCLUDES live-leased groups. With their live
    //   members, for the reaper's recovery+expiry sweep.
    ExpiredGroups(ctx, q Querier, table string, before time.Time, leaseTTL time.Duration, limit int) ([]GroupRows, error)
    EnsureGroupSchema(ctx, q Querier, table string) error
    SchemaExists(ctx, q Querier, table string) (bool, error)
}
```

(Exact signatures finalized in Plan 017 against the real `Querier`/framing; `GroupRows`/`ClaimedGroup` are the raw
framed carriers the store decodes, mirroring `ClaimedRow`.) Reference DDL is a per-dialect package-level builder
(`postgres.GroupDDL`/`mysql.GroupDDL`/`sqlite.GroupDDL`) that validates the table first — **not** an interface
method, for the same identifier-injection reason `LeaseDialect` keeps DDL off the interface.

**Per-engine group-row locking (H1/H-B).** PG/MySQL acquire the group row via `SELECT … FOR UPDATE` at the top of
every multi-statement operation (group → member order). **SQLite has no `SELECT … FOR UPDATE`, and `database/sql`'s
`BeginTx` opens a DEFERRED transaction** — under WAL a deferred tx takes its read snapshot at first read, so two
concurrent `AddMember` txns can each miss the other's committed member, **reintroducing the H1 race** (audit R2
M-D). The `sqlite.GroupDialect` therefore runs each multi-statement operation on a **dedicated `*sql.Conn` with a
raw `BEGIN IMMEDIATE` / `COMMIT`** (not `BeginTx`), acquiring the database write lock up front so all same-key (and
indeed all) writers serialize — the single-writer analog of the group-row lock. A conformance test proves concurrent
same-key adds serialize on SQLite (M-D).

### 4. Lease / fence semantics (mirrors the Source, ADR 0010 D3–D5)

- **`AddMember` serializes same-key adds via a group-row lock (audit R1 H1 — completion-detection race).** Under
  READ COMMITTED (PG/MySQL default), two processes each adding one member of a size-N group would each snapshot only
  their own member (the other's `INSERT` uncommitted) → neither observes completion → the complete group is
  mis-expired or stuck. `AddMember` therefore runs, in ONE tx: upsert the group row (`INSERT … ON CONFLICT
  (group_key) DO NOTHING`) → `SELECT epoch FROM msgin_group WHERE group_key = ? FOR UPDATE` (take the group-row lock,
  so a concurrent same-key add blocks until this commits) → upsert the member → `SELECT` the live members. The
  completing add then observes ALL committed members. Cross-group adds stay parallel; only same-key adds serialize
  (a group's members are logically serial anyway). SQLite serializes writers already; MySQL/PG use the `FOR UPDATE`
  row lock.
- **Claim** is fenced on the group row: `UPDATE msgin_group SET epoch = epoch + 1, locked_by = ?, locked_at = now()
  WHERE group_key = ? AND (locked_by IS NULL OR locked_at <= now() - <ttl>)`. A second claimant of an active lease
  matches no row → `(nil, nil)` (held). Winner-detection is **`rowsAffected == 1`** (portable — PG can also
  `RETURNING epoch`; MySQL/MariaDB have no `RETURNING`, so read the bumped epoch via a `SELECT … WHERE group_key = ?`
  **inside the same tx** while the row lock is held — audit R1 M3, never a separate-tx read that another claimant
  could bump between).
- **Claim RE-ABSORBS a superseded claim's members (audit R1 H2 — crash-recovery orphan/loss).** After winning the
  (possibly stolen) lease, tag the claimed set with the new epoch:
  `UPDATE msgin_group_member SET claimed_epoch = <newEpoch> WHERE group_key = ? AND (claimed_epoch IS NULL OR
  claimed_epoch < <newEpoch>)`. Tagging only `claimed_epoch IS NULL` would leave a **crashed holder's** members
  (tagged with the old epoch, never settled) orphaned forever — the re-claim would return zero members, emit empty,
  and the real members are never emitted and never deleted (permanent loss). The group-row lease fence guarantees a
  single active claim, so every `claimed_epoch < newEpoch` is a dead claim and safe to re-absorb — this is what makes
  the "lease ages out → re-claim → duplicate, never loss" guarantee actually hold.
- **Settle / Abandon are fenced AND lock the group row FIRST (audit R2 H-B — deadlock-order).** They are fenced on
  `(group_key, locked_by, epoch)`: a lease that expired and was re-claimed (epoch bumped) makes the original holder's
  settle/abandon a no-op (`applied=false`) — no phantom delete, exactly the Source's Ack/Nack fence. To avoid an
  InnoDB ABBA deadlock, they must acquire the **group row first** (`SELECT … FROM msgin_group WHERE group_key=? FOR
  UPDATE`) before touching member rows, so the lock order is uniformly **group → member** across `AddMember`,
  `ClaimGroup`, `SettleGroup`, and `AbandonGroup` (a member-first delete racing a group-first claim/add otherwise
  deadlocks under load, aborting one tx → a stuck lease/stall). `SettleGroup` then deletes `claimed_epoch = epoch`
  members; if members remain (late arrivals), it clears the lease AND **resets `created_at = now()`** so the residual
  is a fresh group for expiry, matching memory's residual semantics (audit R1 M2); else it deletes the group row.
- **Expiry / crash-recovery vs lease.** `ExpiredGroups` returns groups the reaper's settlement sweep must
  re-examine: a **crashed holder's lease-expired group regardless of age** (audit R2 H-A — this is how a stuck
  complete group is found and re-released to output), plus (when a timeout cutoff is passed) unleased groups older
  than the cutoff; it **excludes currently-live-leased groups**. The reaper re-claims each and routes complete →
  output (recovery), age-expired-incomplete → expired channel, else abandon (ADR 0020 §8). `sql.GroupStore` exposes
  `RecoverInterval()` = its lease TTL so `Run` sweeps at that cadence even with no `WithGroupTimeout`.
- **`WithGroupLeaseTTL` default = 5m (safe-default gate, CLAUDE.md — audit R2 L-K / R3 M1).** The lease TTL must sit
  **comfortably above** a plausible aggregate-fn + `output.Send` round-trip, because a stolen *live* lease → a
  **double emit to output** every recovery tick (not merely a duplicate-on-crash), unbounded under persistent
  slowness. Aggregation's release is **heavier than the Source's** single-message handler (it runs the user aggregate
  fn *and* `output.Send`, which on a `DirectChannel` drives a whole synchronous downstream sub-flow), so it must get
  an **equal-or-more generous** default than the Source's `WithLeaseTTL` (5m). The default is therefore **5m**, matching
  the Source precedent — *not* the 30s an earlier draft proposed, whose 10×-tighter value contradicted the safe-default
  "generous margin" gate. Trade-off (documented on the option): because the reaper's `RecoverInterval()` = the lease
  TTL, a longer TTL means a crashed group is recovered ~one TTL later; a caller who needs snappier crash-recovery and
  whose release is fast may lower `WithGroupLeaseTTL`, accepting the tighter steal window. Overridable; `≤0` →
  `ErrInvalidLeaseTTL`. (The in-process `memory.GroupStore` has NO TTL — its lease is unconditional while held; ADR
  0020 §8.)

### 5. Capacity / overflow (Spec O9-3, resolved)

Group-store capacity is **DB-bounded** for `sql.GroupStore` — the database is the durable substrate, and a
never-completing-group DoS is mitigated operationally (expiry reaping + monitoring the table), not by an in-store
cap. The `memory.GroupStore` keeps its `WithMaxGroups` bound (ADR 0020 / Plan 016: a new key beyond the cap returns
`ErrOverflowDropped` rather than evicting a partial group — never silent loss). This matches ADR 0018 §4 (memory
store bounded, sql store DB-bounded); no `OverflowPolicy` knob is added to `sql.GroupStore` in v1.

### 6. Testing — four-engine conformance + cross-process races (use-testcontainers)

- **Shared conformance table** run against **PostgreSQL, MySQL, MariaDB, SQLite** via the existing
  `harness`/`dbtest` `RunTestDatabase` (no mocks, no in-memory fakes for the DB — `use-testcontainers`): idempotent
  `Add`; growing snapshot; `ClaimGroup` returns members + epoch and is exclusive; `SettleGroup` fenced-deletes only
  the claimed set; **late-member-during-lease survives** (add after claim, settle, assert it remains as a fresh
  group); `AbandonGroup` restores; `Expired` boundary + lease-exclusion; fence miss after re-claim.
- **Concurrent first-add completion detection (audit R1 H1 / R2 M-F determinism):** two separate connections add the
  two final members of a size-2 group → **exactly one** `AddMember` snapshot observes completion. Because two
  goroutines calling the one-shot `AddMember` cannot be deterministically interleaved at the lock point, prove it
  either by a **high-repetition barrier-synchronized loop** (many rounds, a shared start barrier) or a **manual-tx
  drive** (open one tx, do the INSERT + `SELECT … FOR UPDATE`, hold it, start the second add in a goroutine, then
  commit and assert exactly one sees size 2) — a naive two-goroutine call could pass even with the lock removed.
- **Real two-connection claim race:** two `sql.GroupStore` handles (separate connections) claim the same complete
  group concurrently → exactly one non-nil claim; the loser gets `(nil, nil)`.
- **Crash-mid-release re-emits to the OUTPUT channel (audit R2 H-A — the headline recovery guarantee):** drive a
  complete group through the Aggregator so it `ClaimGroup`s, then simulate a crash (do NOT settle); a second
  Aggregator over the same store, with `Run` sweeping at `RecoverInterval`, after the lease ages out re-claims →
  **re-aggregates → sends to OUTPUT** (not the expired channel) → settles. Assert the aggregate reaches **output**
  exactly once net, and no orphan rows remain. This is the case the earlier design silently failed.
- **Crash / restart + stale-epoch recovery (audit R1 H2):** a fresh `sql.GroupStore` over the same DB, after the
  lease ages out, re-claims → the re-claim returns **all** the dead holder's members (re-absorbed across the epoch
  bump, not zero); a settled group does not re-emit; a stale-epoch settle is `applied=false`.
- **Deadlock-order (audit R2 H-B):** a concurrent add-during-settle (and the H2 re-claim racing a stale holder's
  settle) does not InnoDB-deadlock — the uniform group→member lock order holds (a high-concurrency same-key
  add/settle loop must stay deadlock-free under `-race` + real MySQL).
- **SQLite two-connection wiring (audit R2 M-E):** `sqliteKit` must expose `OpenDB` backed by a **file (or
  `cache=shared`) DB** — a private `:memory:` is per-connection-distinct, so the two-connection claim/first-add cases
  need a shared on-disk database; and the `sqlite.GroupDialect` must use the raw `BEGIN IMMEDIATE` conn path (M-D).
- The **core** cross-process proof (ADR 0020 §8) is the fast, DB-free two-`*Aggregator`-one-`memory.GroupStore`
  stress test (in-process claim atomicity only); this ADR's DB tests confirm the multi-process + crash-recovery
  invariants memory cannot exhibit.
- Gates: `-race`, `CGO_ENABLED=0`, `go vet`/`golangci-lint`/`gofmt`, `govulncheck`, `go mod tidy` stable
  (**no new dependency** — `database/sql` is stdlib; the driver is the caller's). Additive API → **minor SemVer**.

## Consequences

**Positive**
- Aggregation is now durable and **multi-process-safe** with the same memory↔sql store-swap ergonomics
  `ChannelStore` gave queue channels — flip `memory.NewGroupStore()` to `sql.NewGroupStore(db, dialect)` and the
  flow survives restarts and scales to Competing Consumers, no rewiring.
- The lease-claim is **loss-free at-least-once** end to end; the claimed-set fencing keeps the "late member forms a
  fresh group" semantics honest under early-release strategies.
- Reuses the proven sql adapter machinery (lease/fence/epoch, `Dialect` module split, framing, `ValidateIdent`,
  `RunTestDatabase`), so the durable backend inherits the same identifier-safety and four-engine coverage.

**Negative / costs**
- **Genuinely new SQL, not a facade** — a two-table schema, a segregated `GroupDialect` with three per-engine
  implementations, and the claimed-epoch marker are real surface and per-dialect quirk risk (mitigated by the
  four-engine conformance table).
- **The Aggregator's `Handle`/reaper are reworked** (per-key lock removed, claim-before-send) — a behavior change to
  the merged Phase-2 core, re-audited and re-proven by the cross-process stress test.
- **Two extra framing touchpoints** (the `seq` column, and sequence-header `int` restoration in `DecodeHeaders`).

**Rejected alternatives** (the model-level rejections — naive `DELETE … RETURNING`, blind key-delete, per-process
lock — are recorded in [ADR 0020 §8 / Consequences](0020-splitter-aggregator-group-store.md)).
- **A thin facade over the existing `LeaseDialect`** (as `sql.QueueStore` is over Source+Outbound). Rejected:
  group-keyed idempotent add + per-group lease + claimed-set fencing + expiry scan are not expressible as the
  row-oriented queue operations; forcing them through `LeaseDialect` would fatten a segregated interface and still
  need new SQL. A dedicated `GroupDialect` is cleaner and keeps both dialects cohesive.
- **One table with denormalized lease columns on every member row.** Rejected: a per-group lease duplicated across N
  member rows means an N-row UPDATE per claim and a `MIN(created_at) GROUP BY` expiry scan; the group row is one
  indexed row per group.
- **A single global `WithMaxGroups`-style cap on `sql.GroupStore`.** Rejected for v1: the DB is the durable
  substrate and an in-store cap that rejects new groups is an operational policy better served by expiry + table
  monitoring (matches ADR 0018 §4's sql-is-DB-bounded stance).
```

