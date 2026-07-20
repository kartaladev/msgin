# `sql.GroupStore` + uniform lease-claim aggregation (Phase 3) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax.
>
> **Go skills are mandatory (CLAUDE.md writing-plans override):** every task starts from **`cc-skills-golang:golang-how-to`**, uses **`superpowers:test-driven-development`** (red→green→refactor), navigates/refactors via **`gopls`**, and obeys the project test overrides — **`table-test`** (assert-closure tables, `t.Context()`), **`use-mockgen`** (only if a mock is genuinely needed; the sql package uses hand-written fake dialects, not mockgen), **`use-testcontainers`** (`RunTestDatabase` for every DB test — never a mock/in-memory fake for the database).

**Goal:** Make msgin's Aggregator durable and **multi-process-safe** — ship `sql.GroupStore` (a `MessageGroupStore` over `database/sql`) and rework the aggregation settlement from a per-process lock to a **store-level lease-claim** (`ClaimGroup`/`SettleGroup`/`AbandonGroup`), so two aggregator processes sharing one store never double-emit or lose a group.

**Architecture:** The `MessageGroupStore` SPI replaces `Remove` with an atomic lease-claim that tags and settles a **member set** (never blind-deletes by key, so a member arriving during the lease survives as a fresh group — loss-free). `memory.GroupStore` is reworked to the same shape (lease under its mutex); the `Aggregator`'s `[256]sync.Mutex` is removed (the store claim is the single serialization point). `sql.GroupStore` realizes it durably over a new segregated `GroupDialect` (two tables: a per-group lease row + append-only members with a `claimed_epoch` marker), proven on PostgreSQL/MySQL/MariaDB/SQLite via `RunTestDatabase`.

**Tech Stack:** Go 1.25; core stdlib + `clockwork`; `adapter/database/sql` (+ `postgres`/`mysql`/`sqlite`/`harness`/`dbtest` modules); tests blackbox `_test`, testify, `goleak`, testcontainers-go.

## Global Constraints

- **Go 1.25 pinned:** build/test with `GOTOOLCHAIN=go1.25.12` on every command.
- **Module paths:** core `github.com/kartaladev/msgin` (`package msgin`); the sql engine `.../adapter/database/sql` (`package sql`, imported as `msginsql`); dialects `.../adapter/database/sql/{postgres,mysql,sqlite}`; the conformance driver `.../adapter/database/sql/harness`; the container runner `.../adapter/database/sql/dbtest`. **Dependency points inward** — core must NOT import any adapter; the sql engine must NOT import a driver (the caller injects it; the harness imports no driver either).
- **No new dependency.** `database/sql` is stdlib; `clockwork`/`goleak`/`testify`/`testcontainers-go` already present. `go mod tidy` must leave every `go.mod`/`go.sum` in the `go.work` unchanged.
- **msgin has NO release tag** — replacing the shipped `MessageGroupStore.Remove` is therefore *not* a SemVer break (unreleased API). Additive elsewhere → minor SemVer.
- **Blackbox tests only** (`package msgin_test`, `package memory_test`, `package sql_test`, `package dbtest_test`); assert-closure tables (`table-test`); `t.Context()`; **`goleak`** on anything starting a goroutine (the Aggregator reaper; the dbtest module's `TestMain` container-plumbing ignore-list). Inject time via `clockwork` (`clockwork.NewFakeClock()`).
- **`use-testcontainers`:** every `sql.GroupStore` behavior is proven against a REAL PostgreSQL/MySQL/MariaDB/SQLite via the existing `dbtest` `RunTestDatabase` — no mocks, no in-memory DB fake. The `sql` package's own unit tests use a hand-written **fake `GroupDialect`** (the established `fakedialect_test.go` pattern), not a database.
- **No panics on caller input:** every construction-time validation returns a typed error — core `ErrNilStore`; sql `ErrNilAdapter`/`ErrNilDialect`/`ErrInvalidTableName`/`ErrInvalidLeaseTTL`. All identifier interpolation goes through `msginsql.ValidateIdent` (the sole injection guard — the table name cannot be a bound parameter).
- **Server clock only (ADR 0010 D3/D4):** every persisted timestamp and lease/expiry comparison uses the DB server clock (`now()`), never the app clock; durations pass as interval-typed parameters. No app↔DB skew.
- **Hot-path branch coverage mandatory:** every construction-validation branch; the claim win/lose/absent branches; the claimed-set-fenced settle vs fence-miss; abandon-restores; the late-member-during-lease survival path; the Expired boundary + lease-exclusion; the Aggregator's held/release/claim-nil/send-error/settle branches; the reaper's claim-nil/still-expired/not-expired-abandon branches — each needs a covering test.
- **Full pre-commit gate** on the final task (across the whole `go.work`): `go build ./...`, `go test ./... -race`, `go vet ./...`, `golangci-lint run ./...`, `gofmt -l .` (empty), `CGO_ENABLED=0 go build ./...`, `go mod tidy` no-diff in every module, `govulncheck ./...` — all clean, on Go 1.25.
- **Commit trailers:** `Spec: 009`, `Plan: 017`, `ADR: 0021` (and `ADR: 0020` on the core-reshape commit, which realizes the ADR 0020 §8 revision).
- **Traceability:** [Spec 009 §3.4](../specs/009-splitter-aggregator-endpoints.md); [ADR 0021](../adrs/0021-sql-group-store.md); [ADR 0020 §8](../adrs/0020-splitter-aggregator-group-store.md) (the SPI/Handle revision this realizes); reuses [ADR 0010](../adrs/0010-poller-sql-adapter.md)/[0011](../adrs/0011-sql-engine-dialect-module-split.md)/[0012](../adrs/0012-sqlite-dialect.md) (lease/fence/dialect-module split) and the [Plan 013](013-persistent-queue-channel.md) `sql.QueueStore` facade precedent.

## Resolved design decisions (fold from ADR 0020 §8 + ADR 0021 + this plan)

- **Core SPI (`groupstore.go`):** drop `Remove`; add
  ```go
  type MessageGroupClaim interface { MessageGroup; Epoch() int64 }
  // in MessageGroupStore:
  ClaimGroup(ctx context.Context, key string) (MessageGroupClaim, error) // nil,nil if absent or already leased & unexpired
  SettleGroup(ctx context.Context, claim MessageGroupClaim) error         // fenced delete of the CLAIMED members only
  AbandonGroup(ctx context.Context, claim MessageGroupClaim) error        // release lease w/o delete (Send failed / not-expired)
  ```
  `Add`/`Expired`/`EmitsLiveValue` unchanged.
- **Claim tags a member set, never blind-deletes by key** (the correctness crux): a member arriving during the lease is untagged and survives `SettleGroup` as a fresh live group. Loss-free. **memory fences the claimed set by PREFIX LENGTH** (`Add` only appends, so claimed = `msgs[:claimedLen]`; audit R1 H3 — id-agnostic, unlike a broken id-map fence that leaked id-less members). **sql fences by a `claimed_epoch` marker and RE-ABSORBS a dead claim's members** on re-claim (`claimed_epoch IS NULL OR claimed_epoch < newEpoch`; audit R1 H2 — else a crashed holder's members orphan forever).
- **Lease ending differs by store (audit R1 H4/M1):** the in-process **memory lease is UNCONDITIONAL while held** — released only by Settle/Abandon (both synchronous in the claiming goroutine), no wall-clock steal, so `memory.GroupStore` has **NO lease TTL and no `WithGroupLeaseTTL`** (a panic mid-release leaks the lease until process exit — fine, memory is process-scoped). The durable **sql lease is TTL-bounded**: `WithGroupLeaseTTL(d)` on `sql.NewGroupStore`, default **5m** (matching the Source's `WithLeaseTTL`; a stolen live lease double-emits, so the heavier aggregate release gets an equal-or-more-generous margin — audit R3 M1; `≤0` → `ErrInvalidLeaseTTL`), so a crashed holder's lease ages out and another process re-claims (recovery latency ≈ one TTL).
- **Message ids required on the durable path (audit R1 H3):** sql keys members `PRIMARY KEY(group_key, msg_id)`, so `AddMember` rejects an empty `msgID` with a typed error (source messages + Splitter children always carry ids). memory tolerates id-less members (prefix fence) without redelivery dedup.
- **Multi-process completion detection (audit R1 H1):** sql `AddMember` takes a group-row `SELECT … FOR UPDATE` before snapshotting so same-key adds serialize across processes and the completing add sees all members. memory is immune (single mutex); therefore the memory cross-process stress test proves in-process claim ATOMICITY only, and the **sql conformance** must prove H1 (concurrent-first-add) + H2 (stale-epoch crash recovery) — the races memory cannot exhibit (audit R1 M4).
- **Aggregator:** remove `locks [256]sync.Mutex` + `keyLock`; `Handle` = `assert → correlate → Add → release-check → ClaimGroup → (nil ⇒ held) → agg → output.Send → SettleGroup` (Send/agg error ⇒ `AbandonGroup` + return err); reaper = `Expired → ClaimGroup(each) → nil?skip → still-expired? route+SettleGroup : AbandonGroup`. No lock held across `Send` (the F2 cyclic-deadlock footgun is gone; the godoc keeps a recursion warning — audit R1 L2).
- **`sql.GroupStore`:** `NewGroupStore(db *stdsql.DB, dialect GroupDialect, opts ...GroupStoreOption) (*GroupStore, error)`; `EmitsLiveValue()==false` (wire); at-least-once across restart AND processes; `Ready`/`EnsureSchema` at boot.
- **Schema (ADR 0021 §2):** `msgin_group(group_key PK, created_at, epoch, locked_by NULL, locked_at NULL)` + `msgin_group_member(group_key, msg_id, seq NULL, headers, payload, claimed_epoch NULL, PK(group_key,msg_id))`.
- **`GroupDialect`** is a NEW segregated SPI (like `InboxDialect`), built-ins `postgres.GroupDialect()`/`mysql.GroupDialect()`/`sqlite.GroupDialect()`; reference DDL via package-level `postgres.GroupDDL`/`mysql.GroupDDL`/`sqlite.GroupDDL` (validate-then-build, never an interface method).
- **Framing (ADR 0021 §2 / ADR 0020 §2 M-1):** `DecodeHeaders` adds `HeaderSequenceNumber`/`HeaderSequenceSize` to its `int`-restoration list (like `HeaderDeliveryCount`). Belt-and-suspenders — `defaultRelease` already reads size via `asInt` (float64-tolerant).
- **Capacity (O9-3):** memory keeps `WithMaxGroups`; sql is DB-bounded (no in-store cap in v1).

---

## File Structure

- **Modify `groupstore.go`** (core) — replace `Remove` with `ClaimGroup`/`SettleGroup`/`AbandonGroup`; add `MessageGroupClaim`.
- **Modify `aggregator.go`** (core) — remove `locks`/`keyLock`; rework `Handle` + `reap`/`reapGroup` to the claim protocol; update godoc (drop the per-key-lock + cyclic-deadlock caveats, add the claim/lease + loss-free wording).
- **Modify `adapter/memory/groupstore.go`** — add lease state (`epoch`, `leased bool`, `claimedLen int` — prefix fence, NO wall-clock `lockedAt`/TTL); implement `ClaimGroup`/`SettleGroup`/`AbandonGroup` + `RecoverInterval()→0`; make `Add` return live-only members; delete `Remove`. **Do NOT add `WithGroupLeaseTTL` to memory** (unconditional in-process lease — see Step 2).
- **Modify `groupstore_conformance_test.go` + `aggregator*_test.go` + `adapter/memory/groupstore_test.go`** — retarget to the new SPI; add the two-`*Aggregator`-one-store cross-process stress test + the late-member-during-lease test.
- **Create `adapter/database/sql/groupdialect.go`** — the `GroupDialect` interface + `GroupRows`/`ClaimedGroup` carriers.
- **Create `adapter/database/sql/groupstore.go`** — `sql.GroupStore` + `GroupStoreOption`/`WithGroupLeaseTTL`/`WithGroupLockedBy`, over an `adapterBase`-like base (or reuse a small local base).
- **Modify `adapter/database/sql/errors.go`** — add `ErrMissingMsgID = errors.New("msgin/sql: group member requires a non-empty message id")` (H3).
- **Modify `adapter/database/sql/framing.go`** — `DecodeHeaders` int-restores the two sequence headers.
- **Create `adapter/database/sql/groupstore_unit_test.go` + `groupdialect_fake_test.go`** — fake-`GroupDialect` unit tests (no DB).
- **Create `adapter/database/sql/{postgres,mysql,sqlite}/groupdialect.go` + `groupddl.go`** — per-engine `GroupDialect()` + `GroupDDL`.
- **Modify `adapter/database/sql/harness/testkit.go`** — add `Group msginsql.GroupDialect` + `GroupDDL func(string)(string,error)` fields.
- **Create `adapter/database/sql/harness/groupstore.go`** — `RunGroupStore(t, kit, db)` conformance driver.
- **Modify `adapter/database/sql/dbtest/conformance_{pg,mysql,sqlite}_test.go`** — add `Group`/`GroupDDL` to each kit; run `harness.RunGroupStore`.
- **Create `example_sql_groupstore_test.go`** (or under `dbtest`) — an `Example`/godoc showing the memory→sql swap (illustrative; no live DB in the Output-checked example).

Existing symbols reused: core `Message`/`PayloadOf`/`boxMessage`/`MessageHandler`/`MessageChannel`/`HeaderCorrelationID`/`HeaderSequenceNumber`/`HeaderSequenceSize`/`ErrNilStore`/`ErrOverflowDropped`/`asInt`/`clockwork.Clock`; sql `Querier`/`ValidateIdent`/`EncodeHeaders`/`DecodeHeaders`/`ErrNilAdapter`/`ErrNilDialect`/`ErrInvalidTableName`/`ErrInvalidLeaseTTL`/`ErrSchemaNotReady`; harness `TestKit`/`RunTestDatabase`.

---

## Task 1: Core SPI reshape + Aggregator claim rework + memory lease store + cross-process proof

Deliver the uniform lease-claim model in the core and memory store, proven by the two-`*Aggregator`-one-`memory.GroupStore` cross-process stress test and the late-member-during-lease survival test. **No DB in this task.**

**Files:** Modify `groupstore.go`, `aggregator.go`, `adapter/memory/groupstore.go`; modify `groupstore_conformance_test.go`, `aggregator_test.go`, `aggregator_settlement_test.go`, `adapter/memory/groupstore_test.go`.

**Interfaces:**
- Produces (core `groupstore.go`):
```go
type MessageGroupClaim interface {
    MessageGroup     // Key() string; Messages() []Message[any]; CreatedAt() time.Time
    Epoch() int64    // fence token
}
type MessageGroupStore interface {
    Add(ctx context.Context, key string, msg Message[any]) (MessageGroup, error) // returns LIVE (unclaimed) members
    ClaimGroup(ctx context.Context, key string) (MessageGroupClaim, error)
    SettleGroup(ctx context.Context, claim MessageGroupClaim) error
    AbandonGroup(ctx context.Context, claim MessageGroupClaim) error
    // Expired returns groups the reaper must re-examine: crashed-lease groups (regardless of age) PLUS
    // (when before is non-zero) unleased groups older than before; excludes live-leased groups. (R2 H-A)
    Expired(ctx context.Context, before time.Time) ([]MessageGroup, error)
    // RecoverInterval is the reaper's crash-recovery sweep cadence: 0 for memory (unconditional lease),
    // the lease TTL for sql. (R2 H-A)
    RecoverInterval() time.Duration
    EmitsLiveValue() bool
}
```
- Produces (`adapter/memory`): unchanged `NewGroupStore(opts ...GroupStoreOption)`; `Remove` deleted. **Do NOT add `WithGroupLeaseTTL` to memory** — the memory lease is unconditional (no TTL); `RecoverInterval()` returns 0 (audit R2 L-H).

- [ ] **Step 1: Rewrite the core SPI** (`groupstore.go`) — replace the `Remove` method with `ClaimGroup`/`SettleGroup`/`AbandonGroup` and add `MessageGroupClaim`. Full file:

```go
package msgin

import (
    "context"
    "time"
)

// MessageGroup is a snapshot of one correlation group held by a MessageGroupStore:
// its key, members in arrival order, and the arrival time of the first member
// (used for expiry). The Messages slice is a copy — mutating it does not affect
// the store.
type MessageGroup interface {
    Key() string
    Messages() []Message[any]
    CreatedAt() time.Time
}

// MessageGroupClaim is an exclusive lease over the members of a group that were
// present at claim time. An Aggregator claims a complete group, aggregates and
// forwards it, then SettleGroups the claim (fenced by Epoch). Members that arrive
// during the lease are NOT part of the claim and survive settlement as a fresh
// group (loss-free — ADR 0020 §8).
type MessageGroupClaim interface {
    MessageGroup
    // Epoch is the fence token: SettleGroup/AbandonGroup take effect only while
    // the store's lease for the key still carries this epoch. A lease that
    // expired and was re-claimed (epoch bumped) makes a stale holder's settle a
    // no-op — no phantom delete.
    Epoch() int64
}

// MessageGroupStore is the swappable state behind an Aggregator: correlation-keyed
// groups of held messages, with a store-level atomic lease-claim that makes
// release exactly-once within AND across processes. Implementations live in
// adapter packages (adapter/memory, adapter/database/sql); the core never imports
// them. It mirrors ChannelStore (ADR 0018) one level up.
type MessageGroupStore interface {
    // Add durably appends msg to group key and returns the resulting group
    // snapshot of the LIVE (unclaimed) members — the residual when a claim is in
    // flight, else all members (audit R2 M-C: memory and sql must agree, so the
    // release check sees the same set; a member arriving during a lease is a
    // fresh-residual member, not part of the in-flight claim). Idempotent by msg
    // id: re-adding an already-stored member is a no-op (a redelivered member
    // does not double-count toward release — at-least-once).
    Add(ctx context.Context, key string, msg Message[any]) (MessageGroup, error)
    // ClaimGroup atomically leases the members present now for key and returns
    // them plus a fence epoch. It returns (nil, nil) when key is absent or is
    // already leased by another holder whose lease has not expired — the caller
    // then treats the group as held (someone else is releasing it). The lease
    // TTL is store-owned (WithGroupLeaseTTL); a crash before SettleGroup lets the
    // lease age out so another holder re-claims (duplicate, never loss).
    ClaimGroup(ctx context.Context, key string) (MessageGroupClaim, error)
    // SettleGroup deletes exactly the claimed member set (fenced on claim.Epoch)
    // after a successful release. Members added during the lease survive as a
    // fresh live group. A fence miss (the lease was stolen) is a no-op, not an
    // error.
    SettleGroup(ctx context.Context, claim MessageGroupClaim) error
    // AbandonGroup releases the lease WITHOUT deleting (the release Send failed,
    // or the reaper found a not-actually-expired group): the claimed members
    // return to live so a retry / next member / next reaper tick re-releases.
    // Fenced on claim.Epoch; a fence miss is a no-op.
    AbandonGroup(ctx context.Context, claim MessageGroupClaim) error
    // Expired returns groups the reaper's settlement sweep must re-examine: any
    // group whose LEASE has expired (a crashed holder — sql) regardless of age,
    // PLUS (when before is non-zero) unleased groups whose CreatedAt is before the
    // cutoff. Excludes groups under a live lease. (audit R2 H-A: the crashed-lease
    // case is how a durable store's stuck complete group is found and re-released.)
    Expired(ctx context.Context, before time.Time) ([]MessageGroup, error)
    // RecoverInterval is the cadence at which the reaper sweeps for crashed leases,
    // independent of WithGroupTimeout (audit R2 H-A). memory returns 0 (unconditional
    // lease — no crash-recovery sweep needed); sql returns its lease TTL, so a
    // crashed holder's group is recovered within ~one TTL even with no expiry timeout.
    RecoverInterval() time.Duration
    // EmitsLiveValue reports live Go values (memory) vs []byte (wire, sql).
    EmitsLiveValue() bool
}
```

- [ ] **Step 2: Rework the memory store** (`adapter/memory/groupstore.go`). Add lease state and the three claim methods; delete `Remove`. **The memory lease is UNCONDITIONAL while held (no TTL — audit R1 H4/M1) and the claimed set is fenced by PREFIX LENGTH (audit R1 H3).** Key body (fold into the existing file — keep `Add`/`Expired`/`EmitsLiveValue`/`NewGroupStore`/`WithMaxGroups`/`WithGroupClock`/snapshot; do NOT add `WithGroupLeaseTTL` to memory):

```go
type groupState struct {
    msgs       []msgin.Message[any]
    ids        map[string]struct{}
    createdAt  time.Time
    epoch      int64 // bumped on each ClaimGroup, fences Settle/Abandon
    leased     bool  // true between ClaimGroup and Settle/Abandon (UNCONDITIONAL — no wall-clock TTL)
    claimedLen int   // # members frozen into the active claim; Add only appends, so claimed = msgs[:claimedLen]
}

func (s *GroupStore) ClaimGroup(_ context.Context, key string) (msgin.MessageGroupClaim, error) {
    s.mu.Lock(); defer s.mu.Unlock()
    g, ok := s.groups[key]
    if !ok || g.leased {
        return nil, nil // absent, or held by a live holder (no wall-clock steal in-process)
    }
    g.epoch++
    g.leased = true
    g.claimedLen = len(g.msgs)
    claimed := slices.Clone(g.msgs[:g.claimedLen])
    return claimGroup{snapshot{key: key, msgs: claimed, createdAt: g.createdAt}, g.epoch}, nil
}

func (s *GroupStore) SettleGroup(_ context.Context, claim msgin.MessageGroupClaim) error {
    s.mu.Lock(); defer s.mu.Unlock()
    g, ok := s.groups[claim.Key()]
    if !ok || !g.leased || g.epoch != claim.Epoch() { // fence miss / stolen / already settled
        return nil
    }
    // delete exactly the claimed PREFIX; anything appended during the lease survives.
    for _, m := range g.msgs[:g.claimedLen] {
        if id := m.ID(); id != "" {
            delete(g.ids, id) // so a post-completion redelivery forms a fresh group, not a dedup no-op
        }
    }
    residual := slices.Clone(g.msgs[g.claimedLen:])
    if len(residual) == 0 {
        delete(s.groups, claim.Key())
        return nil
    }
    g.msgs = residual
    g.leased = false
    g.claimedLen = 0
    g.createdAt = s.clock.Now() // residual is a fresh group for expiry (matches sql — audit R1 M2)
    return nil
}

func (s *GroupStore) AbandonGroup(_ context.Context, claim msgin.MessageGroupClaim) error {
    s.mu.Lock(); defer s.mu.Unlock()
    g, ok := s.groups[claim.Key()]
    if !ok || !g.leased || g.epoch != claim.Epoch() {
        return nil
    }
    g.leased = false    // members return to live (all of msgs, incl. any appended during the lease)
    g.claimedLen = 0    // epoch stays bumped so the abandoned holder's later settle is a no-op
    return nil
}

// claimGroup is a snapshot + fence epoch implementing MessageGroupClaim.
type claimGroup struct {
    snapshot
    epoch int64
}
func (c claimGroup) Epoch() int64 { return c.epoch }
```
> **Implementer notes.** (1) `Add` to a leased group appends as normal (beyond `claimedLen`), which is the late-member survival path — the prefix fence keeps it. **`Add` MUST return the LIVE members only (audit R2 M-C / R3 nit) at BOTH its return sites — the idempotent-no-op branch AND the append branch: `slices.Clone(g.msgs[g.claimedLen:])`** (when unleased `claimedLen==0` → all; when leased → the residual), matching sql's `claimed_epoch IS NULL` snapshot so the release-check sees the same set on both stores. (The completing `Add` is always on an unleased group — a claim is taken only *after* a completing Add triggers it — so live==all there and the normal release path is unbroken.) (2) `Expired` must exclude leased groups: add `&& !g.leased` to its `if g.createdAt.Before(before)` guard (a crashed-lease never occurs in-process, so memory's `Expired` only surfaces age-old unleased groups when `before` is non-zero). (3) `RecoverInterval() time.Duration { return 0 }` — memory needs no crash-recovery sweep (audit R2 H-A/L-H). (4) The prefix fence is correct for id-less members too (no id needed to fence); document that id-less members still lack idempotent-`Add`-by-id redelivery dedup (rare — source messages carry ids). (5) NO `leaseTTL` field, NO `WithGroupLeaseTTL` on memory — a held claim is released only by Settle/Abandon (synchronous in the claiming goroutine); a panic mid-release is handled by the Aggregator's defer-abandon (Step 3, audit R2 M-G). Keep `WithGroupClock` (do not reintroduce a `WithClock` collision). `slices` is already imported.

- [ ] **Step 3: Rework the Aggregator** (`aggregator.go`). Remove `locks [numShardLocks]sync.Mutex`, `keyLock`, and `numShardLocks`. New `Handle` + reaper:

```go
func (a *Aggregator) Handle(ctx context.Context, msg Message[any]) error {
    if err := a.assert(msg); err != nil {
        return err // ErrPayloadType: fail fast, never added to the store
    }
    key, err := a.cfg.correlate(msg)
    if err != nil {
        return err // e.g. Permanent(ErrNoCorrelation)
    }
    group, err := a.store.Add(ctx, key, msg)
    if err != nil {
        return err
    }
    if !a.cfg.release(group) {
        return nil // held; source Acks — durability now on the store
    }
    claim, err := a.store.ClaimGroup(ctx, key)
    if err != nil {
        return err
    }
    if claim == nil {
        return nil // another Handle/process is releasing this group; held
    }
    return a.release(ctx, claim)
}

// releaseOnce aggregates a claimed group, forwards it to the output channel, and
// settles the claim. It DEFERS an abandon-unless-settled (audit R2 M-G) so that
// an agg/Send error OR a PANIC (recovered by the driving Consumer) always frees
// the lease — else a panic mid-release would wedge the correlation key forever on
// the memory store (no TTL to age it out). at-least-once, never loss.
func (a *Aggregator) releaseOnce(ctx context.Context, claim MessageGroupClaim) (err error) {
    settled := false
    defer func() {
        if !settled {
            _ = a.store.AbandonGroup(ctx, claim) // runs on error return AND on panic unwind
        }
    }()
    out, aggErr := a.agg(ctx, claim.Messages())
    if aggErr != nil {
        return aggErr
    }
    if sendErr := a.cfg.output.Send(ctx, out); sendErr != nil {
        return sendErr
    }
    if err = a.store.SettleGroup(ctx, claim); err != nil {
        return err
    }
    settled = true
    return nil
}

// release settles the claim, THEN drains any already-complete residual left at the
// key (audit R3 H1). A residual forms when members arrive during a holder's lease;
// after the settle it may itself be complete, but nothing else re-checks it (the
// recovery sweep surfaces only crashed *leases*, not unleased-complete residuals),
// so under count-based release + a recurring key + no expiry timeout it would be
// silently stranded. The drain loop claims and releases the key until the residual
// is incomplete or gone — store-agnostic, immediate, and it terminates (each
// iteration either emits a complete batch or abandons an incomplete residual).
func (a *Aggregator) release(ctx context.Context, claim MessageGroupClaim) error {
    if err := a.releaseOnce(ctx, claim); err != nil {
        return err
    }
    for {
        next, err := a.store.ClaimGroup(ctx, claim.Key())
        if err != nil || next == nil {
            return err // nothing more to drain (empty / leased by another / gone)
        }
        if !a.cfg.release(next) {
            _ = a.store.AbandonGroup(ctx, next) // residual not yet complete; leave it live
            return nil
        }
        if err := a.releaseOnce(ctx, next); err != nil {
            return err // failed re-release already abandoned its claim; retry later
        }
    }
}
```
> **Residual-strand tail — WithGroupTimeout is the guarantee-bearer under concurrency (audit R3 H1 / R4 NIT).** The drain closes the *common* case immediately, but not a full guarantee alone: a residual can still be left complete-but-untriggered by (a) a crash mid-drain, or (b) a concurrent `Add(m5)` landing during the drain's speculative claim-then-abandon of an *incomplete* residual (drainer holds {m3,m4}, abandons it, live group becomes {m3,m4,m5}=complete, both callers returned). Neither loses data (at-least-once holds); both are recovered by the reaper's age-scan release-check→OUTPUT. So **count-based recurring-key aggregation under Competing Consumers should set `WithGroupTimeout`** — document on `WithCompletionSize`/`NewAggregator` that it is the residual guarantee-bearer, the drain just makes the common path immediate. Canonical Splitter→Aggregator (exactly-N-once) forms no residual and needs none. **Fairness (godoc note):** under a pathological same-key feeder the drain can keep one worker/tick busy emitting — inherent (each iteration is a real, required emit), acceptable, no artificial bound (a bound would re-open the strand). The Step 5 regression test covers the clean single-drain path, not the concurrent-Add-vs-abandon interleave (that one is a `WithGroupTimeout`-recovered no-loss case).
Reaper — a **RECOVERY + expiry sweep** (audit R2 H-A). For each candidate from `Expired(cutoff)` (crashed-lease groups always; age-old unleased groups when a timeout is set), re-claim and re-drive: a **complete** group (crashed mid-release, or newly complete) is re-aggregated and sent to the **output** channel (recovery, via the same `release`); an **age-expired incomplete** group goes to the expired channel; otherwise the lease is abandoned:
```go
func (a *Aggregator) reapGroup(ctx context.Context, g MessageGroup, cutoff time.Time) {
    claim, err := a.store.ClaimGroup(ctx, g.Key())
    if err != nil || claim == nil {
        return // released/leased concurrently, or gone
    }
    if a.cfg.release(claim) {
        _ = a.release(ctx, claim) // RECOVERY: re-emit a crashed/complete group to OUTPUT (+ settle)
        return
    }
    if !cutoff.IsZero() && claim.CreatedAt().Before(cutoff) {
        // genuinely expired incomplete group → route members to the expired sink.
        for _, m := range claim.Messages() {
            if sendErr := a.cfg.expired.Send(ctx, m); sendErr != nil {
                _ = a.store.AbandonGroup(ctx, claim) // retry next tick rather than drop (audit R2 L-I)
                return
            }
        }
        _ = a.store.SettleGroup(ctx, claim)
        return
    }
    _ = a.store.AbandonGroup(ctx, claim) // fresh residual re-formed since the Expired() scan, or not yet due
}
```
> **`Run` gating (audit R2 H-A).** Start the sweep when **either** `WithGroupTimeout>0` **or** `store.RecoverInterval()>0`; tick at the **min positive** of the two; pass `cutoff = timeout>0 ? clock.Now().Add(-timeout) : time.Time{}` to `reap`. So a durable store (RecoverInterval = lease TTL) gets crash-recovery sweeps even with no expiry timeout, while memory (RecoverInterval 0) still blocks-until-cancel when no timeout is set. Guard the `WithGroupTimeout`-without-`WithExpiredGroupChannel` construction error as before (an expiry sink is only required when a timeout is set; the pure recovery sweep routes complete groups to the always-present output channel, so it needs no expired channel).
> **Godoc updates (mandatory).** On the `Aggregator` type + `WithOutputChannel`/`WithExpiredGroupChannel`/`Run`: **replace** the "per-key lock deadlocks" caveat with a **recursion** warning (audit R1 L2). **Add**: settlement is a store-level lease-claim giving at-least-once within AND across processes; a crash in claim→settle recovers via the **reaper's recovery sweep re-emitting to the OUTPUT channel** (audit R2 H-A) — so **`go agg.Run(ctx)` is REQUIRED when using a durable store (`RecoverInterval()>0`) for multi-process/crash safety**, not just for expiry; a member arriving during a lease forms a fresh group. On `WithReleaseStrategy` (audit R1 L1): the release decision is made on the `Add` live snapshot but `ClaimGroup` freezes whatever is present at claim time, so a **non-monotonic** strategy may aggregate a slightly different set than it decided on — prefer monotonic (`>=`). On `WithGroupLeaseTTL` (sql; audit R2 L-K): a stolen *live* lease → a double emit to output, and aggregation's release (agg fn + `output.Send` driving a whole `DirectChannel` sub-flow) is heavier than a Source handler — size the TTL above it. Update `WithCompletionSize`'s doc: exact-count is now safe across processes too. Keep the `Permanent(ErrNoCorrelation)` + ingress-`ErrPayloadType` routing behavior.

- [ ] **Step 4: Retarget the conformance + aggregator + memory tests** to the new SPI (they currently call `Remove`). In `groupstore_conformance_test.go`: replace the "Remove drops the group" case with **claim/settle** and **claim/abandon** cases, plus **claim returns nil when already leased**, **late-member-during-lease survives settle**, and **Expired excludes a leased group**. Retarget `aggregator_test.go`/`aggregator_settlement_test.go` release cases to assert via the claim protocol (behavior is the same single-process: one emit, group settled). Delete/replace any assertion that pokes `Remove`.

- [ ] **Step 5: Add the in-process claim-atomicity stress test** (the concurrency proof `-race` cannot give — the Phase-3 analog of Phase-2's N>1 test). **Scope honestly (audit R1 M4 / R2 H-A): this proves in-process claim/settle ATOMICITY, NOT the sql multi-process races (H1 completion detection, H2 crash re-absorb, H-A crash-recovery-to-output) — memory's single mutex + unconditional in-process lease cannot exhibit them; they are proven only by the sql testcontainers conformance in Task 3.** In `aggregator_settlement_test.go`:
```go
func TestAggregator_ConcurrentClaim_SingleEmitNoLoss(t *testing.T) {
    // ONE shared memory.GroupStore, TWO *Aggregator instances (no shared per-key
    // lock — so the store's atomic claim is the ONLY serializer), each fed the
    // SAME correlated members concurrently from many goroutines. Assert: exactly
    // ONE aggregate emitted per group, every member present (no loss). This
    // exercises the unconditional-lease guard (H4): a second concurrent ClaimGroup
    // on an in-flight claim must return nil. Count emits via a concurrency-safe
    // recording MessageChannel; run under -race.
}
```
Plus a **late-member-survives-settle** test (drives the prefix fence): with `WithCompletionSize(n)`, `Add` n members (release fires), then before the test lets settle complete, `Add` a distinct (n+1)th member for the same key; assert the emitted aggregate has exactly n and the late member remains as a fresh group (re-releases or expires — never lost). Drive the interleave deterministically via a small fake `MessageGroupStore` wrapping `memory.GroupStore` whose `ClaimGroup`/`SettleGroup` block on a channel the test releases after injecting the late `Add` (do NOT rely on timing).

Plus a **complete-residual drains without a new member or timeout** test (audit R3 H1): with `WithCompletionSize(2)`, **no** `WithGroupTimeout`, and the same fake-store interleave — while a holder's claim of `{m1,m2}` is blocked mid-release, inject `m3` and `m4` for the same key (forming a complete residual `{m3,m4}`), then let the holder's `SettleGroup` proceed; assert the `release` **drain loop** emits `{m3,m4}` to output too (two aggregates total), with **no** further `Add` and **no** reaper/timeout running. This is the H1 strand regression guard.

- [ ] **Step 6: Run** — `GOTOOLCHAIN=go1.25.12 go test ./... -race` (core + memory green; sql not yet touched). Confirm `go vet`/`gofmt` clean on the touched files.

- [ ] **Step 7: Commit** — stage `groupstore.go aggregator.go adapter/memory/groupstore.go` + the retargeted tests + `docs/plans/017-sql-group-store.md docs/adrs/0021-sql-group-store.md docs/adrs/0020-splitter-aggregator-group-store.md docs/specs/009-splitter-aggregator-endpoints.md` (design bundle rides with the first Phase-3 code commit). Message `feat(core): uniform lease-claim MessageGroupStore SPI + Aggregator claim rework`; trailers `Spec: 009 / Plan: 017 / ADR: 0020 / ADR: 0021`. Do NOT stage `.claude/settings.json`.

---

## Task 2: `GroupDialect` SPI + `sql.GroupStore` facade + framing (fake-dialect unit tests, no DB)

Deliver the sql-side surface: the `GroupDialect` interface, the `sql.GroupStore` implementing the core SPI over it, and the `DecodeHeaders` sequence-header int restoration — all unit-tested with a hand-written fake `GroupDialect` (no database; the `fakedialect_test.go` pattern). Real DB conformance is Task 3.

**Files:** Create `adapter/database/sql/groupdialect.go`, `adapter/database/sql/groupstore.go`; modify `adapter/database/sql/framing.go`; create `adapter/database/sql/groupdialect_fake_test.go`, `adapter/database/sql/groupstore_unit_test.go`.

**Interfaces:**
- Consumes (core, Task 1): `msgin.MessageGroupStore`, `msgin.MessageGroup`, `msgin.MessageGroupClaim`.
- Produces (`package sql`):
```go
// GroupRows is a group + its live members as raw framed bytes (the store decodes).
type GroupRows struct {
    GroupKey  string
    CreatedAt time.Time
    Members   []MemberRow
}
type MemberRow struct {
    MsgID   string
    Seq     int64 // HeaderSequenceNumber (0 if none)
    Headers []byte
    Payload []byte
}
// ClaimedGroup is GroupRows plus the fence epoch set by ClaimGroup.
type ClaimedGroup struct {
    GroupRows
    Epoch int64
}
type GroupDialect interface {
    AddMember(ctx context.Context, q Querier, table, groupKey, msgID string, seq int64, headers, payload []byte) (GroupRows, error)
    ClaimGroup(ctx context.Context, q Querier, table, groupKey, lockedBy string, leaseTTL time.Duration) (*ClaimedGroup, error) // nil,nil if absent/leased
    SettleGroup(ctx context.Context, q Querier, table, groupKey, lockedBy string, epoch int64) (applied bool, err error)
    AbandonGroup(ctx context.Context, q Querier, table, groupKey, lockedBy string, epoch int64) (applied bool, err error)
    ExpiredGroups(ctx context.Context, q Querier, table string, before time.Time, leaseTTL time.Duration, limit int) ([]GroupRows, error)
    EnsureGroupSchema(ctx context.Context, q Querier, table string) error
    SchemaExists(ctx context.Context, q Querier, table string) (bool, error)
}
func NewGroupStore(db *stdsql.DB, dialect GroupDialect, opts ...GroupStoreOption) (*GroupStore, error)
func WithGroupLeaseTTL(d time.Duration) GroupStoreOption // default 5m (matches Source; R3 M1); <=0 -> ErrInvalidLeaseTTL
func WithGroupLockedBy(id string) GroupStoreOption        // default a generated unique id (mirrors source WithLockedBy)
// GroupStore methods: Add / ClaimGroup / SettleGroup / AbandonGroup / Expired / RecoverInterval / EmitsLiveValue / Ready / EnsureSchema.
// RecoverInterval() returns the store's lease TTL (audit R2 H-A) so the Aggregator's reaper sweeps for crashed leases at that cadence.
```

- [ ] **Step 1: Write the `GroupDialect` interface + carriers** (`groupdialect.go`) with full godoc, encoding the audit-hardened contract on each method: every method `ValidateIdent`s `table`; DB server clock only; `leaseTTL` passed as an interval param. **`AddMember`** runs in one tx, takes a group-row `SELECT … FOR UPDATE` before snapshotting so same-key adds serialize across processes (H1), and rejects an empty `msgID` with a typed error (H3). **`ClaimGroup`** returns `(nil,nil)` on absent/leased, detects the winner by `rowsAffected==1` (PG may `RETURNING`; MySQL reads the bumped epoch via a same-tx `SELECT`, M3), and **re-absorbs a superseded claim's members** (`claimed_epoch IS NULL OR claimed_epoch < newEpoch`, H2). **`SettleGroup`** deletes only `claimed_epoch=epoch` members and, on a residual, resets `created_at=now()` + clears the lease (M2); fenced on `(group_key, locked_by, epoch)`. **`AbandonGroup`** un-tags (`claimed_epoch=NULL`) + clears the lease, fenced. Add a compile-time `var _ GroupDialect = ...` note for implementers. Reference-DDL is NOT a method (identifier-injection reason — mirror the `LeaseDialect` note).

- [ ] **Step 2: Write the failing `sql.GroupStore` unit tests** (`groupstore_unit_test.go`, `package sql_test`) against a fake `GroupDialect` (`groupdialect_fake_test.go`, a struct with function fields, mirroring `fakedialect_test.go`). Cases (assert-closure table):
  - construction: nil db → `ErrNilAdapter`; nil dialect → `ErrNilDialect`; bad table → `ErrInvalidTableName`; `WithGroupLeaseTTL(0)` → `ErrInvalidLeaseTTL`.
  - `Add` frames headers+payload via `EncodeHeaders`/codec and calls `dialect.AddMember`, returning a `MessageGroup` decoded from `GroupRows` (members in seq/arrival order; `EmitsLiveValue()==false`).
  - `ClaimGroup` maps a non-nil `*ClaimedGroup` → a `MessageGroupClaim` (Epoch wired) and a `nil` → `(nil,nil)`; passes the store's `leaseTTL`/`lockedBy` through.
  - `SettleGroup`/`AbandonGroup` pass `claim.Epoch()` + `lockedBy` through; a dialect `applied=false` is not an error.
  - `Expired` maps `[]GroupRows` → `[]MessageGroup` (decoded), passing the store `leaseTTL`.
  - `Ready` → dialect `SchemaExists`; `EnsureSchema` → `EnsureGroupSchema`.
  - a dialect error from any method propagates (wrapped, table named).
  > Decode: reuse `DecodeHeaders` + the runtime codec pairing — since `EmitsLiveValue()==false`, the store returns `Message[any]` whose payload is the raw `[]byte` (the typed runtime decodes downstream), exactly as `sql.Source` deliveries do. Verify how `sql.Source` builds its `Message[any]` from `ClaimedRow` (headers via `DecodeHeaders`, payload bytes as the `any`) and mirror it.

- [ ] **Step 3: Run — verify fail** (`undefined: sql.NewGroupStore`). `GOTOOLCHAIN=go1.25.12 go test ./adapter/database/sql/ -run 'TestGroupStore' -v` → FAIL (compile).

- [ ] **Step 4: Implement `sql.GroupStore`** (`groupstore.go`) — a small base (reuse `newAdapterBase` if its `LeaseDialect` field can be generalized, else a local `groupBase` with db/table/logger + `ValidateIdent`/`Ready`/`EnsureSchema` mirroring `base.go`), holding `dialect GroupDialect`, `leaseTTL`, `lockedBy`. Methods frame/decode and delegate to the dialect on `s.db` (the pool), passing `s.leaseTTL`/`s.lockedBy` to `ClaimGroup`/`Expired`/settle/abandon. `Add` rejects a message with an empty id (`msg.ID()==""`) with `ErrMissingMsgID` **at the facade** (belt-and-suspenders with the dialect's own check — H3, unit-testable with the fake dialect). **`RecoverInterval() time.Duration { return s.leaseTTL }`** (audit R2 H-A — a fake-dialect unit test asserts it equals the configured TTL / the 5m default). `EmitsLiveValue()` → false. A generated default `lockedBy` — reuse the Source's `randomLockedBy()` generator (confirmed present in `options.go`).

- [ ] **Step 5: Implement the framing touchpoint** (`framing.go`) — in `DecodeHeaders`, extend the reserved-key switch so `msgin.HeaderSequenceNumber` and `msgin.HeaderSequenceSize` restore a `float64` to `int` (exactly like `HeaderDeliveryCount`). Add a `framing_test.go` case asserting both round-trip as `int`.

- [ ] **Step 6: Run — verify pass** `GOTOOLCHAIN=go1.25.12 go test ./adapter/database/sql/ -race`. **Step 7: Commit** `feat(sql): GroupDialect SPI + sql.GroupStore facade + sequence-header int framing`; trailers `Spec: 009 / Plan: 017 / ADR: 0021`.

---

## Task 3: Per-engine `GroupDialect` + 4-engine testcontainers conformance

Implement `GroupDialect` for PostgreSQL, MySQL/MariaDB, and SQLite; add the shared `harness.RunGroupStore` conformance driver; wire it into `dbtest` for all four real engines (including the two-connection claim race, late-member survival, and crash/lease-expiry sims).

**Files:** Create `adapter/database/sql/{postgres,mysql,sqlite}/groupdialect.go` + `groupddl.go`; modify `adapter/database/sql/harness/testkit.go`; create `adapter/database/sql/harness/groupstore.go`; modify `adapter/database/sql/dbtest/conformance_{pg,mysql,sqlite}_test.go`.

**Interfaces:**
- Produces: `postgres.GroupDialect() msginsql.GroupDialect`, `postgres.GroupDDL(table string) (string, error)` (and mysql/sqlite peers); `harness.RunGroupStore(t *testing.T, kit harness.TestKit, db *sql.DB)`; `TestKit.Group msginsql.GroupDialect`, `TestKit.GroupDDL func(string)(string,error)`.

- [ ] **Step 1: PostgreSQL `GroupDialect`** (`postgres/groupdialect.go`). Reuse `pgQuoteTable`/`pgQuote`. Reference SQL (finalize against real PG in the TDD loop; the fence/lock semantics below are NOT negotiable — they are the audit fixes):
  - `AddMember`: reject `msgID==""` (typed error, H3). In one tx — `INSERT INTO <group>(group_key, created_at, epoch) VALUES ($1, now(), 0) ON CONFLICT(group_key) DO NOTHING`; **`SELECT epoch FROM <group> WHERE group_key=$1 FOR UPDATE`** (lock the group row so a concurrent same-key add blocks until commit — H1 completion-detection serialization); `INSERT INTO <member>(group_key, msg_id, seq, headers, payload) VALUES (...) ON CONFLICT(group_key,msg_id) DO NOTHING`; then `SELECT` the group's `created_at` + live members (`claimed_epoch IS NULL`) ordered by `seq`, `msg_id`. Return `GroupRows`. Commit.
  - `ClaimGroup`: in one tx — `UPDATE <group> SET epoch = epoch + 1, locked_by = $2, locked_at = now() WHERE group_key = $1 AND (locked_by IS NULL OR locked_at <= now() - ($3 * interval '1 microsecond')) RETURNING epoch`; if `rowsAffected==0`/no row → `(nil, nil)` (absent or actively leased). Then **re-absorb any dead claim (H2): `UPDATE <member> SET claimed_epoch = <newEpoch> WHERE group_key = $1 AND (claimed_epoch IS NULL OR claimed_epoch < <newEpoch>)`** (NOT just `IS NULL` — else a crashed holder's members orphan); `SELECT` the claimed members (`claimed_epoch = <newEpoch>`, ordered). Return `*ClaimedGroup`. Commit.
  - `SettleGroup`: one tx, **LOCK THE GROUP ROW FIRST (H-B): `SELECT 1 FROM <group> WHERE group_key=$1 FOR UPDATE`** (uniform group→member order — else ABBA-deadlocks the member-first delete against a group-first add/claim); then `DELETE FROM <member> WHERE group_key=$1 AND claimed_epoch=$3`; then if members remain, **`UPDATE <group> SET locked_by=NULL, locked_at=NULL, created_at=now() WHERE group_key=$1 AND locked_by=$2 AND epoch=$3`** (reset `created_at` so the residual is a fresh group — M2); else `DELETE FROM <group> WHERE group_key=$1 AND locked_by=$2 AND epoch=$3`. `applied` = fence matched (rowsAffected on the group statement).
  - `AbandonGroup`: one tx, **LOCK THE GROUP ROW FIRST (H-B): `SELECT 1 FROM <group> WHERE group_key=$1 FOR UPDATE`**; then `UPDATE <member> SET claimed_epoch=NULL WHERE group_key=$1 AND claimed_epoch=$3`; `UPDATE <group> SET locked_by=NULL, locked_at=NULL WHERE group_key=$1 AND locked_by=$2 AND epoch=$3`. `applied` = fence matched.
  - `ExpiredGroups` (crashed-lease + age, R2 H-A): `SELECT g.group_key, g.created_at FROM <group> g WHERE (g.locked_by IS NOT NULL AND g.locked_at <= now() - ($2 * interval '1 microsecond')) /* crashed lease, any age */ OR ($1 <> '0001-01-01'::timestamp AND g.locked_by IS NULL AND g.created_at < $1) /* age-old unleased */ ORDER BY g.created_at LIMIT $3` + fetch each group's live members. (Pass the zero time when no expiry cutoff; the query then returns only crashed-lease groups. Finalize the zero-time predicate shape in the TDD loop.)
  - `EnsureGroupSchema`: two `CREATE TABLE IF NOT EXISTS` (group, member) + any index, executed separately (pgx extended protocol). `SchemaExists`: reuse the information_schema probe (or delegate to the existing method).
  - `GroupDDL(table)` (`postgres/groupddl.go`): validate-then-build reference DDL string (both tables), unexported builder like `postgresCreateTable`.
- [ ] **Step 2: MySQL/MariaDB `GroupDialect`** (`mysql/groupdialect.go`) — backtick quoting; `INSERT ... ON CONFLICT`→`INSERT IGNORE`/`ON DUPLICATE KEY UPDATE` for idempotent add; `SELECT … FOR UPDATE` group-row lock in `AddMember` (H1, InnoDB). **Claim (no `RETURNING`, M3): `UPDATE <group> SET epoch=epoch+1, locked_by=?, locked_at=now() WHERE group_key=? AND (locked_by IS NULL OR locked_at <= now() - INTERVAL ? MICROSECOND)`; the winner is `rowsAffected==1`; read the bumped `epoch` via `SELECT epoch FROM <group> WHERE group_key=?` INSIDE THE SAME TX (row lock still held — never a separate-tx read another claimant could bump).** Then the same H2 re-absorb `UPDATE … WHERE claimed_epoch IS NULL OR claimed_epoch < newEpoch` and member SELECT. **Settle/Abandon lock the group row FIRST (`SELECT … FOR UPDATE` — H-B), Expired includes crashed leases (H-A)** — mirror PG with MySQL interval syntax (`now() - INTERVAL ? MICROSECOND`, per `mysql/dialect.go`). MariaDB rides on this dialect.
- [ ] **Step 3: SQLite `GroupDialect`** (`sqlite/groupdialect.go`) — SQLite `now()` equivalent + interval arithmetic per the existing sqlite dialect (read `sqlite/dialect.go`; it already handles single-writer + time expressions). `INSERT ... ON CONFLICT DO NOTHING`. **H1/M-D completion-serialization:** SQLite has no `SELECT … FOR UPDATE`, and `database/sql` `BeginTx` opens a DEFERRED tx (a deferred WAL tx takes its read snapshot at first read → two concurrent `AddMember`s can each miss the other's committed member, reintroducing the H1 race). So `AddMember` (and the other multi-statement ops) must run on a **dedicated `*sql.Conn` via a raw `BEGIN IMMEDIATE` / `COMMIT`** (acquire the write lock up front), NOT `BeginTx` — the single-writer analog of the group-row lock. Obtain the conn by type-asserting `q.(interface{ Conn(context.Context) (*stdsql.Conn, error) })` (the `*sql.DB`-only assertion pattern of `BeginLockTx`'s `txBeginner`, `lock.go`). **Rollback discipline (audit R3):** a raw `BEGIN IMMEDIATE` over a pooled `*sql.Conn` is invisible to `database/sql`, so every error path MUST `Exec("ROLLBACK")` before `conn.Close()` (else a leftover open tx returns to the pool). Same H2 re-absorb, `rowsAffected==1` claim winner, M2 `created_at` reset, H-A crashed-lease `Expired`. A conformance case proves concurrent same-key adds serialize on SQLite (M-D).
- [ ] **Step 4: `harness.RunGroupStore`** (`harness/groupstore.go`) — a conformance driver (no driver import) exercising a `sql.NewGroupStore(db, kit.Group, ...)` (or the dialect directly) through the core `msgin.MessageGroupStore` contract, plus the DB-specific races. Cases:
  - `EnsureGroupSchema` via `kit.GroupDDL`/the dialect; idempotent `Add` (same `(key,msgID)` twice → one member); growing snapshot ordered by seq; empty `msgID` → typed error (H3).
  - `ClaimGroup` returns members + epoch; a **second** `ClaimGroup` on the leased group → `(nil,nil)`; **two independent `*sql.DB` connections** (`kit.OpenDB`) claiming the same complete group concurrently → exactly one non-nil.
  - **H1 — concurrent first-add completion detection (the race memory cannot exhibit):** two `kit.OpenDB` connections `AddMember` the two final members of a size-2 group **simultaneously**; assert **exactly one** returned snapshot reports size 2 (the group-row lock serialized them), so the complete group is detected, not stuck/mis-expired.
  - **H2 — stale-epoch crash recovery:** claim a group (members tagged epoch e), do NOT settle (simulate crash); after the lease ages out, a fresh `sql.GroupStore` `ClaimGroup`s → assert the re-claim returns **ALL** the dead holder's members (re-absorbed across the epoch bump, not zero) → settle; assert net exactly-once and **no orphan rows** remain (query the member table). A stale-epoch settle by the original holder → `applied=false`, no phantom delete.
  - **H-A — crash-mid-release re-emits to the OUTPUT channel (the headline recovery guarantee):** drive a complete group through a real `*msgin.Aggregator` over `sql.GroupStore` so it `ClaimGroup`s, then simulate a crash (do NOT settle — e.g. abandon the first Aggregator without settling, or claim directly and drop it); a **second** Aggregator over the same store with `go agg.Run(ctx)` (its reaper sweeping at `RecoverInterval`), after the lease ages out (short `WithGroupLeaseTTL`), re-claims → **re-aggregates → sends to the OUTPUT channel** (not the expired channel) → settles. Assert the aggregate reaches **output exactly once** net and no orphan rows remain. This is the case the pre-audit design silently failed (stuck-forever / mis-routed-to-expired).
  - `SettleGroup` deletes only the claimed set; **late member added after claim survives** (assert it remains, re-claimable, and its group `created_at` reset — M2); `AbandonGroup` restores the claimed set to live.
  - `Expired` returns a **crashed-lease group regardless of age** (claim, don't settle, age out the lease → it appears) and **excludes an actively-leased group**; with a non-zero `before` also returns age-old unleased groups.
  - **H-B — no deadlock:** a high-concurrency same-key add/settle loop (two `kit.OpenDB` connections) stays deadlock-free under real MySQL (`-race`), proving the uniform group→member lock order.
  Extend `TestKit` with `Group msginsql.GroupDialect` + `GroupDDL func(string)(string,error)`; document the fields.
- [ ] **Step 5: Wire `dbtest`** — add `Group: postgres.GroupDialect(), GroupDDL: postgres.GroupDDL` (and mysql/sqlite) to each `*Kit()`; add `t.Run("GroupStore", func(t){ harness.RunGroupStore(t, kit, db) })` to each `Test*Conformance`. **SQLite (audit R2 M-E / R3):** `sqliteKit` must set `OpenDB` to a **shared** DB. `RunTestSQLite` already uses a temp *file* (shared across connections) but **hides its DSN/path** (returns only `*sql.DB`), so the plan must either expose the DSN/path from `RunTestSQLite` (add a variant returning it) or build both the primary DB and `OpenDB` from one shared `file::memory:?cache=shared` DSN (`sqlite.DSN` offers `WithSharedMemory()`); a private `:memory:` is per-connection-distinct and won't work. Check `sqlite/dsn.go` for the DSN shape and whether `_txlock=immediate` belongs there too (M-D). Run each: `GOTOOLCHAIN=go1.25.12 go test ./adapter/database/sql/dbtest/ -race -run 'TestPostgresConformance/GroupStore'` (and mysql/sqlite) → PASS against real containers.
- [ ] **Step 6: Commit** `feat(sql): per-engine GroupDialect + 4-engine GroupStore conformance` (stage the dialect files, harness, dbtest wiring); trailers `Spec: 009 / Plan: 017 / ADR: 0021`.

---

## Task 4: Example, docs finalize, whole-branch gate

Prove the memory→sql swap in a doc, finalize the artifacts, and run the full pre-merge gate over `main..HEAD`.

**Files:** create the `Example`; touch docs as needed; no new production code.

- [ ] **Step 1** — `Example` (godoc): show `memory.NewGroupStore()` vs `sql.NewGroupStore(db, postgres.GroupDialect())` behind the same `msgin.NewAggregator` wiring (illustrative — the `// Output:` example uses the memory store so it runs without a container). Show **`go agg.Run(ctx)`** in the example (audit R3 L2 — recovery rides entirely on the reaper for a durable store, so a caller who forgets `Run` silently never crash-recovers; make the required call visible), and a comment noting the store swap is the only change to go durable + multi-process, but `Run` is required for the durable store's crash-recovery.
- [ ] **Step 2** — Confirm the design bundle is coherent: Spec 009 §3.4, ADR 0020 §8, ADR 0021 all cross-link and match the shipped signatures (`ClaimGroup(ctx, key)`, store-owned `WithGroupLeaseTTL`, the two-table schema). Fix any drift. Update `docs/HANDOVER.md` to reflect Phase 3 complete-and-merged (at delivery time).
- [ ] **Step 3** — Coverage: `groupstore.go`/`aggregator.go` (core), `groupstore.go`/`groupdialect.go` (sql), and each dialect's group methods — hot-path branches at/near 100% (`go test ./... -cover`; inspect the changed packages).
- [ ] **Step 4** — **Whole-branch gate** (`main..HEAD`): run `/code-review` and `/security-review` over the full branch diff, resolve/triage every finding; then the full pre-commit gate (all 8 commands from Global Constraints across the `go.work`) clean on Go 1.25.
- [ ] **Step 5** — Commit `test(sql): GroupStore example + coverage; docs finalize` (if any code/docs changed); trailers. (Merge to `main` + branch delete happen after the gate, on explicit user approval — NOT part of this plan's per-task pre-authorization.)

---

## Self-Review

**Spec/ADR coverage:** core SPI reshape `Remove`→claim (ADR 0020 §8) → Task 1; claimed-set fencing / late-member survival (ADR 0020 §8, ADR 0021 §2) → Task 1 (memory PREFIX fence) + Task 3 (sql `claimed_epoch`); Aggregator claim rework + drop per-key lock (ADR 0020 §8) → Task 1; `GroupDialect` segregated SPI (ADR 0021 §3) → Task 2/3; two-table schema + lease/fence (ADR 0021 §2/§4) → Task 3; `WithGroupLeaseTTL` default 5m safe-default, sql-only (ADR 0021 §4 / R3 M1) → Task 2; memory unconditional lease / no TTL (ADR 0020 §8 H4/M1) → Task 1; sequence-header int framing (ADR 0021 §2 / ADR 0020 §2 M-1) → Task 2; 4-engine testcontainers conformance (ADR 0021 §6) → Task 3; capacity O9-3 (memory bounded, sql DB-bounded) → Task 1/Task 2. **Audit-fix coverage:** H1 completion-serialization (group-row `FOR UPDATE`/`BEGIN IMMEDIATE`) → Task 3 Steps 1-3 + the concurrent-first-add conformance case (Step 4); H2 dead-claim re-absorption (`claimed_epoch < newEpoch`) → Task 3 + the stale-epoch crash-recovery case (Step 4); H3 prefix fence (memory) + id requirement (sql `ErrMissingMsgID`) → Task 1 + Task 2/3; H4 unconditional memory lease → Task 1 + the concurrent-claim atomicity test (Step 5); M2 residual `created_at` reset (both stores) → Task 1 + Task 3; M3 MySQL rows-affected+same-tx-SELECT claim → Task 3 Step 2; M4 honest proof split → Task 1 Step 5 (in-process atomicity) + Task 3 Step 4 (multi-process races); L1/L2 godoc → Task 1 Step 3. **Round-2 audit-fix coverage:** H-A crash-recovery-to-output (reaper is a recovery+expiry sweep; `RecoverInterval`; `Run` required for durable; Add live-only) → Task 1 Steps 1-3 + the H-A output-recovery conformance case (Task 3 Step 4); H-B group→member lock order (group-row `FOR UPDATE` first in Settle/Abandon) → Task 3 Steps 1-3 + the deadlock-free conformance case; M-C unified live-only Add snapshot → Task 1 Steps 1-2; M-D SQLite raw `BEGIN IMMEDIATE` conn → Task 3 Step 3; M-E SQLite shared-DB `OpenDB` → Task 3 Step 5; M-F deterministic H1 test (barrier/manual-tx) → Task 3 Step 4; M-G memory panic-safe `release` (defer-abandon) → Task 1 Step 3; L-H plan de-contradiction (no memory `WithGroupLeaseTTL`) → Task 1 Interfaces/Step 2; L-I expired-Send failure abandons (retry) → Task 1 Step 3 reaper; L-K TTL-vs-release-weight godoc → Task 1 Step 3 + ADR 0021 §4. ✅

**Open items to confirm during implementation (flag to coordinator, don't guess):** (a) whether `newAdapterBase` generalizes to a `GroupDialect` field or a local `groupBase` is cleaner — either, keep `ValidateIdent`/`Ready`/`EnsureSchema` parity with `base.go`; (b) the exact default-`lockedBy` generator (reuse the Source's — read `options.go`); (c) MySQL's no-`RETURNING` claim confirmation shape (claim-UPDATE then fenced SELECT, or SELECT…FOR UPDATE then UPDATE) — pick the one the existing mysql dialect models; (d) SQLite time/interval expressions (mirror `sqlite/dialect.go`); (e) how `sql.Source` builds `Message[any]` from a claimed row (headers via `DecodeHeaders`, payload bytes) — mirror it exactly for `GroupStore` decode; (f) the late-member deterministic interleave in Task 1 Step 5 (fake-store channel hook vs a real timing) — prefer the deterministic fake.

**Placeholder scan:** the SQL in Task 3 is reference SQL to finalize in the TDD loop against real engines (explicitly flagged), not a placeholder — the schema, fence columns, and claim/settle/abandon semantics are fully specified. The memory `SettleGroup`/`ClaimGroup` bodies in Task 1 are complete. No "TODO"/"handle errors"/"similar to" placeholders.

**Type consistency:** `MessageGroupClaim`/`ClaimGroup`/`SettleGroup`/`AbandonGroup`/`Add`/`Expired`/`EmitsLiveValue` (core); `GroupDialect`/`GroupRows`/`MemberRow`/`ClaimedGroup`/`NewGroupStore`/`WithGroupLeaseTTL`/`WithGroupLockedBy` (sql); `GroupDialect()`/`GroupDDL` (dialects); `RunGroupStore`/`TestKit.Group`/`TestKit.GroupDDL` (harness) — used consistently across tasks. `ClaimGroup(ctx, key)` (core, no TTL) vs `GroupDialect.ClaimGroup(ctx, q, table, groupKey, lockedBy, leaseTTL)` (dialect, store passes its TTL) is deliberate and consistent.
```
