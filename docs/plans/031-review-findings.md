# Plan 031 — whole-branch review findings (Task 10 Steps 1–2)

> **Status: OPEN. Nothing here is fixed.** This is the disposition list the delivery gate produced; CLAUDE.md
> requires every entry to be **fixed** or **explicitly triaged with a written rationale** before the branch merges.
> **Do not merge `chore/backlog-sweep-post-029` while any row is un-dispositioned.**
>
> Governing artifacts: [`docs/specs/017-group-member-bounds.md`](../specs/017-group-member-bounds.md) ·
> [`docs/adrs/0033-group-member-bounds.md`](../adrs/0033-group-member-bounds.md) ·
> [`docs/plans/031-group-member-bounds.md`](031-group-member-bounds.md) · [`docs/HANDOVER.md`](../HANDOVER.md)

## 0. What produced this list

| Gate | Scope | Result |
|---|---|---|
| `/code-review max` | `main..HEAD` whole-branch diff | **15 findings** (below) |
| `/security-review` | same range | **0 findings** — 5 candidates raised, 4 refuted at confidence 9, 1 at confidence 2 |
| 8 CI steps × 8 modules, `GOARCH=386` vet, both docs-link arms | at `a2cc568` | all green |

**The two reviews agree on the mechanisms and differ only on lens — that is corroboration, not conflict.** The
security review examined `selectLimit`'s overflow (R-15) and `Handle`'s new overflow branch (R-3/R-4) and
correctly found neither *exploitable*: the `LIMIT` value is a statically-typed `int` range-checked to
`[1, 1_048_576]` before reaching the format verb, and release stays mutex-serialised and epoch-fenced. They
remain **correctness** defects reachable through the public SPI. **No security blockers exist on this branch.**

**Every per-task adversarial review on this branch came back clean.** These 15 surfaced only at branch level —
the project's recorded lesson that a whole-branch gate catches what per-task cannot, holding for the second time.

## 1. Delivery blockers under CLAUDE.md as written

These two are not judgment calls; the project's own rules classify them as blocking.

| # | Site | Defect | Verified by |
|---|---|---|---|
| **R-9** | `adapter/database/sql/groupstore.go:424` | The `decodeErr != nil` early return on the new overflow path has **zero test coverage in every module** — a new typed-error branch on the hot path. CLAUDE.md: *"A hot-path branch with no test is a delivery blocker, regardless of the overall percentage."* | **Coordinator, independently**: `go test ./... -coverprofile` reports `groupstore.go:424.23,426.4 1 0` — hit count `0`. Reachable today with the fake dialect this increment already extended (`fd.addMemberErr` = a `Permanent` overflow + `fd.addMemberRows` carrying a corrupt `Headers` blob). |
| **R-15** | `adapter/database/sql/groupdialect.go:154` | `maxMembers` is an unvalidated `int` on an **exported SPI** with two silent fail-open modes: `<= 0` means UNBOUNDED by design, and `math.MaxInt` **wraps** `selectLimit` to `math.MinInt`, killing the cap check and the fetch bound together. | **Coordinator, independently**: `selectLimit(math.MaxInt)` = `-9223372036854775808`, and `limit > 0` is **false** → no `LIMIT` emitted, and `n > int64(maxMembers)` can never be true. A caller opting into the largest expressible bound silently gets *unbounded* — the exact failure this increment exists to close, delivered by its own escape hatch. |

**R-15 needs a design decision, not a patch.** Options: reject `maxMembers <= 0` with a typed error and add an
explicit `UnboundedGroupMembers = -1` sentinel; or clamp; or validate at the dialect boundary as every other
sizing knob in the repo does. **The `<= 0` fail-open contradicts CLAUDE.md's "Sensible defaults" convention**
(a set-flag plus a typed `ErrInvalidCapacity`), so whichever way this goes, ADR 0033 must record it.

## 2. Correctness defects, proven

| # | Site | Defect and proof |
|---|---|---|
| **R-1** | `postgres/groupdialect.go:150` (twins: `mysql:142`, `sqlite:165`) | The over-cap live fetch is capped at `selectLimit(maxMembers)` = `maxMembers+1`, so a group already holding **more rows than the current cap** returns a silently **truncated** snapshot to the release predicate. **Proven against real SQLite**: a group grown to 10 live members, cap 4, 11th add → overflow fires, but the `SELECT` runs `LIMIT 5` and `withoutMember` drops the refused one, so `Add` returns a **4-member snapshot of a 10-member group**. `Handle` evaluates `WithCompletionSize(10)` against 4 → false → the member is dead-lettered **and the genuinely complete group never releases** — exactly the deadlock the snapshot-alongside-error branch was added to prevent. Reachable via a rolling deploy that lowers the cap, or two instances with different caps (**the multi-instance topology CLAUDE.md mandates reasoning about**). `memory.GroupStore` never truncates, so **the two first-party stores now disagree on the SPI's snapshot contract.** Every harness case fills to *exactly* the cap, so none can observe it. |
| **R-3** | `routing/aggregator.go:501` | `Handle` gates its new branch on the **structural accident `group == nil`** rather than the semantic contract `errors.Is(err, msgin.ErrOverflowDropped)`, so **any** store error paired with a non-nil snapshot claims, aggregates, emits and settles. **Proven**: `sql.GroupStore.Add` ends `return s.decodeGroupRows(rows)`, and `decodeGroupRows` returns `(groupSnapshot{}, err)` on a corrupt stored header — Go's implicit conversion makes that a **non-nil** `msgin.MessageGroup`. Measured: `group == nil ? false (type sql.groupSnapshot)`. The caller's release strategy then runs on a 0-member group for a header-decode fault. R-6 is a second live instance. **Secondary hole on the same line**: `group == nil` is an interface-nil test, so a store returning a typed-nil `(*myGroup)(nil)` — the pattern `groupstore.go:64` now invites — passes the guard and **nil-derefs at line 510**. |
| **R-4** | `routing/aggregator.go:510` | `a.cfg.release(group)` is now routinely called with a **zero-member group** — a precondition neither `ReleaseStrategy`, `WithReleaseStrategy` nor `WithReleaseWhen` documents. **Proven**: `WithReleaseWhen(func(g) bool { return g.Messages()[0].Header("x") != nil })` — a shape no godoc forbids, and what `defaultRelease` itself does modulo a `len == 0` guard — **panics** `index out of range [0] with length 0`. Reachable on the **intended** path: when every other member is claimed the live residual is empty, and the shipped harness asserts exactly that. Inside a Consumer the panic is recovered as `ErrHandlerPanic`, which `IsPermanent` deliberately **excludes**, so it is transient and **retried forever** against the same claimed group. |
| **R-6** | `adapter/database/sql/groupstore.go:420` | The overflow discriminator tests the **raw** dialect error but line 427 returns `classified`, so a `classifyQueryErr` rewrite yields `(non-nil snapshot, ErrSchemaNotReady)` — carrying neither the sentinel nor the `Permanent` marker — which `Handle` then treats as a member rejection. The member table dropped by a bad migration concurrently with an over-cap `Add` gives the operator a raw driver *"relation does not exist"* instead of the typed `ErrSchemaNotReady` naming the table — the entire purpose of `classifyQueryErr`. **The godoc added in this same diff claims the opposite.** Fix: re-test `classified`, not `err`. |
| **R-11** | `postgres/groupdialect.go:157` (twins: `mysql:149`, `sqlite:172`) | `withoutMember` is applied **unconditionally**, but the upsert is `ON CONFLICT DO NOTHING` — so an **idempotent re-add of an already-stored member** of an over-cap group strips that durably-present, never-refused member from the snapshot. `memory.GroupStore` for the identical input takes its dedup branch **first** and returns `(full snapshot, nil)`. **One store Acks the redelivery; the other terminally discards it** — while the SPI guarantees at-least-once redelivery is a no-op. |

## 3. Error-classification contract

Several of these contradict godoc shipped **in this same branch**.

| # | Site | Defect |
|---|---|---|
| **R-2** | `routing/aggregator.go:521` | The overflow branch returns a release failure **verbatim**, so a `Permanent`-marked aggregate or `Send` error **terminally settles a member that was never stored**. **This contradicts the godoc written in Task 8** (`02c7804`), which says such faults are *"unmarked, hence transient"* — false when the aggregate itself returns `msgin.Permanent(...)`, or when the error wraps `ErrPayloadTooLarge`, which `IsPermanent` matches (`reliability.go:46`). Because the member was never persisted, the runtime's single-shot divert **loses** it where the documented transient path would Nack and redeliver. `groupstore.go:78-86` tells third-party store authors they *"may rely on this unconditionally."* |
| **R-7** | `postgres/groupdialect.go:144` (twins: `mysql:136`, `sqlite:159`; `memory/groupstore.go:258` via `g.leased`) | The `Permanent`/transient split tests only `lockedBy.Valid` and **never lease expiry**, so a crashed or stranded lease keeps every over-cap rejection **transient forever** — the hot spin the `Permanent` arm exists to prevent. `ClaimGroup` **in the same file** already uses `locked_by IS NULL OR locked_at <= now - ttl`; `AddMember` is the one `GroupDialect` method not given `leaseTTL`, so **the dialect structurally cannot make the test**. Fixing this changes the SPI signature → **ADR-worthy**. |
| **R-13** | `routing/aggregator.go:519` | The `claim == nil` exit downgrades the store's `Permanent` rejection on the stated grounds that *"another holder is provably draining it"* — but `ClaimGroup` returning nil proves only that a lease is **held**, and a lease ending in `AbandonGroup` leaves the group exactly as full. `Handle`'s own DIRECTION RULE clause 2 asserts *"positive evidence"* this exit does not have. Compounding it, `overflowRetryable` hard-codes *"group %q drained by this release"* — false here, **sending the investigation at the wrong process**. |
| **R-10** | `routing/aggregator.go:511` | `if rerr != nil \|\| !ok { return err }` merges *"the strategy DECLINED"* with *"the strategy FAILED"* and **discards `rerr` entirely**, so a release-strategy fault is reported as the store's cap rejection — diverging from the success path 25 lines below, which propagates it. `errors.Join(err, rerr)` preserves both and is transparent to `errors.Is`/`As`. **Note: `routing/aggregator_test.go:1067` deliberately asserts `assert.NotErrorIs(t, err, strategyErr)`** — so this is a *designed choice being re-litigated* against CLAUDE.md's debuggability gate, not an oversight. **Decide explicitly; do not silently flip it.** |

## 4. Tests that cannot fail

| # | Site | Defect |
|---|---|---|
| **R-8** | `routing/aggregator_test.go:2176` | `TestAggregator_CeilingLevelCompletionSizeConstructs` is **vacuous**: both rows assert only that `NewAggregator` returns no error, and `NewAggregator` never consults the store's member cap. **Mutation-verified twice** — setting `WithMaxGroupMembers(1)` against `WithCompletionSize(65536)` (precisely the deadlock it names) leaves both subtests **passing**; and making `defaultMaxGroupMembers = 1<<2` fails the AST gate correctly while this test still reports `ok`. The two rows differ only in a `store` argument that is **never read**. Its stated purpose — *"this pins the pairings that invariant exists to protect"* — is false. **This is the project's own recorded lesson (a gate that has never failed proves nothing) shipping inside the increment that added it.** |
| **R-5** | `adapter/memory/groupstore.go:261` | The **leased** over-cap arm returns a snapshot that is **always empty** (`msgs[claimedLen:]` with `claimedLen == len(msgs)`), making the snapshot-alongside-error mechanism **inert on that arm** — dead exactly where a concurrent claim makes it matter, which is the case it was written for. And **nothing asserts the arm**: replacing `return live, err` with `return nil, err` leaves all 11 root packages green. The `sql` harness asserts the equivalent; `memory` has no counterpart. |

## 5. Blind spots in gates added by this increment

| # | Site | Defect |
|---|---|---|
| **R-12** | `harness/groupstore.go:765` | `dialectEngine` uses `reflect.TypeOf(d).PkgPath()`, which returns **`""` for a pointer-typed dialect** — the exact form `GroupDialect`'s own godoc tells implementers to use (`var _ msginsql.GroupDialect = (*yourDialect)(nil)`). **Measured**: value type → `"main"`, pointer type → `""`. A contributor on pointer receivers gets the whole member-cap conformance suite asserting against `msgin/sql/: AddMember`, failing with a diff blaming *their* error text rather than the harness's derivation. All three shipped dialects use value receivers, so nothing in-repo exercises it. **This is the helper Task 7 added to fix the MariaDB gap.** Fix: `reflect.Indirect(reflect.ValueOf(d)).Type().PkgPath()`, or an explicit `TestKit` field. |
| **R-14** | `group_member_bound_invariant_test.go:452` | The completeness walk keys on the **literal identifier** `defaultMaxGroupMembers`, so a new first-party store spelling its default any other way is invisible to **both** halves. **Probed both directions**: planting `defaultMaxGroupMembers` in a leaf module correctly turns the gate red; planting `defaultMaxMembersPerGroup` leaves the root package **green**. Renaming an *existing* constant IS caught; adding a *new* one under a new name is not. **This residual was documented in Spec 017 §6 AC-3.3 and ADR 0033 D-AQ at Task 3, but NOT in the file itself**, which otherwise records every limitation exhaustively. The planned `pgx`/`redis`/`nats` adapters each grow a GroupStore. |

## 6. Dropped at the reviewer's 15-finding cap — recorded so they are not lost

Correctness outranks cleanup, so these were cut. **They are un-triaged, not dismissed.**

1. ~120 lines of triplicated dialect logic across `postgres`/`mysql`/`sqlite` with a **prose-only** contract.
2. `ErrOverflowDropped`'s root godoc names **2 of 5** producers.
3. `docs/specs/017` still reads **"NOT approved for implementation"** while its `feat` commits shipped (Task 10 Step 5 owns this).
4. `ExampleWithReleaseWhen`'s hard-coded `false` predicate leaves its channel wiring **dead**.
5. The `memory` group-**count** arm carries the same asymmetry as R-5.

## 7. Suggested sequencing for the fix session

1. **Decide the three design questions first**, and record each in ADR 0033 before touching code: does `AddMember` gain `leaseTTL` (R-7)? Does the SPI reject `maxMembers <= 0` with a typed error plus an explicit unbounded sentinel (R-15)? Is R-10's merge of *declined* and *failed* upheld or reversed (there is a test asserting the current behaviour)?
2. **Then the mechanical fixes**: R-6 (test `classified`), R-3 (gate on the sentinel, and handle typed-nil), R-12 (`reflect.Indirect`), R-9 (cover the branch).
3. **Then the ones that need new test fixtures**: R-1, R-11, R-4, R-5, R-8, R-14.
4. **Re-run the whole-branch gate afterwards** — `/code-review` and `/security-review` are **user-invocable only**; the model cannot run them (see [`docs/HANDOVER.md`](../HANDOVER.md) §5).

> **Mutation discipline applies to every fix here.** A mutant that does not compile reports as a KILL; one that
> fails to apply reports as a SURVIVAL. Both happened repeatedly on this branch. Require the mutation to **apply**
> and the tree to **build** before recording either outcome — see [`docs/HANDOVER.md`](../HANDOVER.md) §8.
