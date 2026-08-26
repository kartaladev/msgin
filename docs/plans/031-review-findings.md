# Plan 031 — whole-branch review findings (Task 10 Steps 1–2)

> ## 🔴 LIVE STATUS — 15 of 15 FIXED (2026-08-27). ALL §6 ITEMS DISPOSITIONED. BOTH DELIVERY BLOCKERS ARE CLOSED.
>
> **Nothing remains un-dispositioned.** §6's five capped items are each **fixed or triaged with a written
> rationale** in §6 below, as CLAUDE.md requires before merge. Nothing merged, nothing pushed.
>
> **Every status line BELOW this block is an older stratum** — this file annotates rather than rewrites, so
> *"Status: OPEN — 5 of 15 fixed"* further down is a record of a past state, not a live claim. **This block is the
> live one.**
>
> | Finding | State |
> |---|---|
> | **R-7**, **R-15**, **R-10** | ✅ **DECIDED + FIXED** — [ADR 0033](../adrs/0033-group-member-bounds.md) D-AU / D-AV / D-AW, delivered by [Plan 031](031-group-member-bounds.md) Task 11 Step 2 |
> | **R-3**, **R-4** | ✅ **FIXED** with R-10 — the same twelve lines of `Handle`; repairing that branch three times would have been three rewrites of one contract block |
> | **R-6**, **R-9**, **R-12** | ✅ **FIXED** — Task 11 Step 3. R-9's hit count re-derived from the coverage profile as **0 → 1**, independently by implementer and coordinator |
> | **R-1**, **R-5**, **R-8**, **R-11**, **R-14** | ✅ **FIXED** — Task 11 Step 4, run as two agents on disjoint modules. R-1 and R-11 both **reproduced against live engines** before being fixed and are proven on all four runners; R-5's premise was **refuted** (see §5a) |
> | **R-2**, **R-13** | ✅ **FIXED** — Task 11 Step 4b, in `Handle`'s overflow branch. R-2 needed a decision: **[ADR 0033](../adrs/0033-group-member-bounds.md) D-AX**, ratified 2026-08-27. **They were RECOVERED, not planned** — the coordinator's first decomposition covered only 13 of the 15, and both were re-surfaced by the agent repairing the branch they sit in |
> | §6's five capped items | ✅ **DISPOSITIONED** — 3 fixed (items 2, 3, 5), 2 triaged with rationale (items 1, 4). See §6 |
>
> **🔴 READ THIS BEFORE ASSUMING R-13 CHANGED BEHAVIOUR — IT DID NOT.** R-2 is a **behaviour change**: the
> release-failure exit stopped returning the fault verbatim and now re-mints a fresh transient
> `ErrOverflowDropped`. **R-13 is not.** Its transient classification at `claim == nil` was **UPHELD ON CORRECTED
> GROUNDS, not reversed** — the *stated grounds* (*"positive evidence the group is being drained"*) were false,
> the *classification* was right, and what changed is the error **text** (`overflowRetryable` now takes a
> `reason`) and the prose in three artifacts. A reader who records R-13 as a reversal will go looking for a
> behavioural regression that does not exist.
>
> **Two corrections to THIS document, both found while fixing it — the record is evidence, so they are annotated,
> not rewritten:**
>
> 1. **R-7's site column wrongly lists `adapter/memory/groupstore.go` as a twin of the three dialects.** It is
>    not. `memory` has no lease TTL by construction, so `!g.leased` is a sound liveness test there. D-AU is
>    scoped to `sql`, and `memory` was deliberately left unchanged.
> 2. **R-2's citation of `groupstore.go:78-86` for *"may rely on this unconditionally"* is wrong.** `git log -S`
>    shows that phrase only ever lived in `routing/aggregator.go`'s DIRECTION RULE; the root SPI godoc never
>    contained it. **The substantive complaint was still correct** — the root godoc did make an unqualified
>    never-upgrades promise, in different words, and it has been qualified. *(The project's standing lesson that
>    a citation is stale when written, holding inside the document that records the lessons.)*
>
> **Status: OPEN — 5 of 15 fixed.** This is the disposition list the delivery gate produced; CLAUDE.md
> requires every entry to be **fixed** or **explicitly triaged with a written rationale** before the branch merges.
> **Do not merge `chore/backlog-sweep-post-029` while any row is un-dispositioned.**
>
> **🔴 2026-08-25 — the three findings that needed a DESIGN DECISION have been decided and recorded.** §7's
> sequencing step 1 is complete; **no code has been written yet.** The decisions are
> [ADR 0033](../adrs/0033-group-member-bounds.md) **D-AU / D-AV / D-AW** (revision 6), with spec twins at
> [Spec 017](../specs/017-group-member-bounds.md) §3.6a.1 / §3.6a.2 / §3.3b, and the execution steps are
> [Plan 031](031-group-member-bounds.md) **Task 11**.
>
> | Finding | Decided | Where |
> |---|---|---|
> | **R-7** | `AddMember` gains `leaseTTL`; classify on lease **expiry**, not on `locked_by` being non-NULL. **`sql` only** | D-AU · §3.6a.1 |
> | **R-15** | `UnboundedGroupMembers = -1`; reject anything outside `{-1} ∪ [1, 1<<20]` with `msgin.ErrInvalidCapacity`, in one shared helper | D-AV · §3.6a.2 |
> | **R-10** | **REVERSED** — `errors.Join(err, rerr)` on failure, bare `err` on decline; the `IsPermanent` escalation is intended and must be asserted | D-AW · §3.3b |
>
> **One correction to this document, made while deciding.** R-7's site column lists
> `adapter/memory/groupstore.go` as a twin of the three dialects. **It is not one.** `memory` has no lease TTL by
> construction (`leased` is documented *"UNCONDITIONAL — no wall-clock TTL"*), its holder is an in-process
> goroutine, and a panicking release is already covered by `releaseOnce`'s deferred abandon. `!g.leased` is a
> sound liveness test there and an unsound one in `sql`. **D-AU is scoped to `sql` and `memory` must not be
> changed** — see D-AU's scope paragraph.
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

### 3a. 🔴 DISPOSITION of R-2 and R-13 (2026-08-27) — annotations, the rows above stand as written

**These two closed last, in Task 11 Step 4b, and they closed differently from each other.** Recorded here because
the difference is the thing a later reader will get wrong.

| Finding | Disposition | What actually changed |
|---|---|---|
| **R-2** | ✅ **FIXED — behaviour changed.** Needed a design decision first: **[ADR 0033 D-AX](../adrs/0033-group-member-bounds.md)**, ratified 2026-08-27 | The release-failure exit no longer returns `relErr`. It returns `overflowRetryable(key, fmt.Sprintf("was claimed but its release FAILED (%v), so it did not drain", relErr))` — **`%v`, not `%w`**. The result is **transient by construction** (it wraps nothing markable), `errors.Is(err, msgin.ErrOverflowDropped)` still matches, and `relErr`'s text is preserved so the Nack still names the output channel. **Accepted cost:** `errors.Is`/`As` can no longer reach the release cause at that exit — *that is the mechanism*, the chain is what carried the permanence |
| **R-13** | ✅ **FIXED — 🔴 the downgrade was UPHELD ON CORRECTED GROUNDS, NOT REVERSED.** No behaviour changed at this exit | `overflowRetryable` gained a `reason string`; the three minting exits now each say what happened (`"drained by this release"` / `"is held by another holder, whose lease is draining it"` / the release-failure phrase above). `Handle`'s DIRECTION RULE clause 2 was corrected: a held lease is **not** proof of drainage. The transient classification stands on a **bounded-cost** argument instead — an abandoning holder leaves the group unleased, so the very next `Add` takes the store's `Permanent` arm and the cost of being wrong is **one redelivery**, against unrecoverable loss if a genuinely draining group's member is dead-lettered |

**Two corrections to R-2's own row, established by running things, not by re-reading.** Both leave the
substantive complaint intact.

1. **`groupstore.go:78-86` never contained *"may rely on this unconditionally."*** Already recorded at the head of
   this file: `git log -S` puts that phrase only in `routing/aggregator.go`'s DIRECTION RULE. The root SPI godoc
   did make an unqualified never-upgrades promise, in different words.
2. **The Task 8 godoc's *"unmarked, hence transient"* was not wholly false — it was false of `relErr` and TRUE of
   `cerr`.** The `ClaimGroup`-failure exit still returns the store fault verbatim, still unmarked, still
   transient, and that is deliberate. Only the release exit was wrong, and it is now the one exit that does **not**
   propagate a fault's own classification. The godoc has been split accordingly at all three sites that carried
   the merged claim (`groupstore.go`, Spec 017 §3.3a.1 and §3.7).

**🔴 TWIN OF R-13's OVERSTATEMENT — three sites, TWO fixed, ONE still OPEN.** The same *"an empty live residual
means the claim holder is already draining the group"* claim sits at **gate 3** of the overflow branch, and gate 3
is described in three places. It is **harmless in effect** everywhere — gate 3 returns the store's error unchanged
and makes no classification decision — but *"fix the class, not the instance"* is a standing project rule and this
would be caught at the next review.

| Site | State |
|---|---|
| `groupstore.go`'s gate-3 bullet (root SPI godoc) | ✅ **FIXED** — now says the empty residual is evidence the group is **LEASED**, not proof it drains, and notes that nothing turns on the difference *at a gate that returns the error unchanged either way* |
| [Spec 017](../specs/017-group-member-bounds.md) §3.3a.1's exit table + §3.7 | ✅ **FIXED** — same correction |
| **`routing/aggregator.go`'s gate 3 inline comment** — `// an empty residual means the claim holder is already draining it` (locate with `grep -n 'empty residual' routing/aggregator.go`) | ⚠️ **OPEN.** It was outside the scope of the doc pass that fixed the other two. **Suggested fix — comment only, no logic:** *"an empty residual means another holder's claim covers every member, so there is nothing left HERE to release; that is evidence the group is leased, not proof it drains (R-13), but nothing turns on it at a gate that returns `err` unchanged either way."* |

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

## 5a. 🔴 ANNOTATIONS to §4 — what was true when fixed (2026-08-26)

**Per this file's convention the rows above are left as written; corrections are recorded here.** Every item below
was established by **running** something, not by re-reading.

- **R-5's central premise is REFUTED.** The row says the leased over-cap snapshot is *"always empty
  (`msgs[claimedLen:]` with `claimedLen == len(msgs)`)"*. **That equality holds only at the instant `ClaimGroup`
  returns.** `Add` appends beyond `claimedLen` for the width of the lease, so a group claimed *before* it filled
  reports a real residual — claim at 3 of cap 4, admit a 4th in-lease, and the 5th arrival's over-cap snapshot
  carries one live member. **The mechanism is live exactly where the row calls it dead.**
- **R-5's second claim is refuted AT HEAD but was true in the owning package.** *"Replacing `return live, err`
  with `return nil, err` leaves all 11 root packages green"* — no longer, because Step 2's D-AW join tests in
  `routing` now kill it. It **still survived `go test ./adapter/memory/`**, and *a cross-package accident is not a
  contract*, so **the missing assertion was the real defect** and is now added in the owning package. §6 item 5's
  group-**count** twin reproduced fully and is also asserted now.
- **R-8 reproduced, and had two details the review did not report.** Row 1 was a **verbatim duplicate** of
  `TestNewAggregator_CompletionSizeCeilingAccepts` — it could never fail because another test already owned that
  ground. Row 2 was **not entirely dead**: its `require.NoError` inside the store closure accidentally pinned
  `memory.maxGroupMembersCeiling >= routing.completionSizeCeiling`, a relation nothing else checks. The test was
  **kept and rewritten**, not deleted: row 1 dropped, the accidental assertion promoted to a deliberate one, and
  all rows now cross-check through **one shared constant** so it cannot drift onto an arbitrary passing value —
  which is how the predecessor went vacuous. **It is renamed `TestAggregator_CeilingLevelCompletionSizePairing`;
  the name in §4 no longer exists.**
- **R-14 is CLOSED STRUCTURALLY, not merely documented.** A new **half 3** reads `msgin.MessageGroupStore`'s
  method set **out of the AST** and asserts that the set of packages declaring a covering type equals the
  known-sites list, both directions. **A new store is caught by what it IS, not by what it names a constant** —
  the project's *"assert the property, not the string"* lesson. The discriminator's margin was **measured, not
  assumed**: exactly 2 types match repo-wide and the nearest non-match reaches 3 of 7, so it cannot false-red,
  which is the failure mode that gets a gate deleted. Probed all three ways — a new package under
  `defaultMaxMembersPerGroup` is **green on halves 1+2 and RED on half 3**; a 6-of-7 near-miss is correctly **not**
  flagged. **Two residuals survive and are stated on the test after being verified: embedding, and generated
  methods.** Spec 017 §6 AC-3.3 and ADR 0033 D-AQ still describe the *old, wider* residual — they are not wrong,
  but they now understate the gate.

## 5b. Raised WHILE FIXING, not by the review — recorded so they are not lost

**New in the fix session.** Neither is a regression; both are pre-existing and were surfaced by an implementer
working in the neighbourhood.

1. **`decodeErr` is discarded entirely on the overflow path** (`adapter/database/sql/groupstore.go`, the branch
   R-9 now covers — locate with `grep -n 'decodeErr' adapter/database/sql/groupstore.go`). The operator never
   learns that a **stored header is corrupt**; the rejection is reported and the decode fault vanishes. The inline
   comment shows the *suppression* is deliberate (*"a corrupt stored header must not mask the rejection"*) — but
   suppressing the error's **effect** on control flow is not the same as discarding its **content**, and
   CLAUDE.md makes the typed-error surface a first-class debuggability constraint. Candidate fix: log it at WARN
   through the injected `*slog.Logger`, or join it. **Un-triaged.**
2. **`harness.DialectEngine` is newly EXPORTED** (was `dialectEngine`), because `harness` ships no other route to
   that code — every `RunXxx` needs a live `*sql.DB` — and CLAUDE.md forbids the whitebox fallback while directing
   *"export what a test must assert."* Verified not to collide with the root class gate, whose
   `scanGroupBoundStoreDecls` looks only for a **const** named `defaultMaxGroupMembers` in non-test files. It is
   also independently useful: the harness godoc already warns third-party dialect authors to reconcile their site
   prefix. **Flagged because `groupMemberCap`'s godoc argues against public-surface growth in a leaf module** —
   that argument is scoped to an `int` sizing knob the class gate can discover, which this is not. **Accepted; not
   escalated to an ADR.**

## 6. Dropped at the reviewer's 15-finding cap — DISPOSITIONED 2026-08-27

Correctness outranks cleanup, so these were cut from the 15. **CLAUDE.md requires every one fixed or explicitly
triaged with a written rationale before the branch merges.** The five original lines are preserved verbatim; the
disposition follows each.

1. **~120 lines of triplicated dialect logic across `postgres`/`mysql`/`sqlite` with a prose-only contract.**

   ⏸️ **TRIAGED — deliberately not fixed on this branch.** Three reasons, in order of weight:

   - **The three dialects are separate Go modules**, so "de-duplicate" means either moving logic up into the
     shared `adapter/database/sql` package — enlarging what every dialect must conform to — or creating a new
     shared module. Both are structural changes with their own ADR, not a cleanup.
   - **[ADR 0031](../adrs/0031-nil-option-elements.md) D-R already tolerates independent copies across packages**
     rather than growing shared surface to reach them — its `nilOptionAt` helper is *"duplicated, not shared"* in
     eight packages, precisely because sharing it would mean new exported surface reachable from eight modules.
     The same shape of argument applies here, and most of the ~120 lines are **engine-specific SQL that cannot be
     shared anyway**. ⚠️ **Cite it honestly: D-R's copy is 3 lines and this is ~120**, and D-R's own text warns
     that Spec 014 §3.3 is *"cited as precedent, not as a stated general rule."* It is a supporting argument, not
     the deciding one — the deciding one is the module boundary above.
   - **Task 11 already made the down-payment where it counts.** D-AV moved `maxMembers` validation out of the
     three copies into one shared `sql.ValidateMaxMembers`, so the parameter with the actual fail-open defect
     stopped being prose-only. That is the pattern to repeat: **lift the CONTRACT, leave the SQL.**

   **What would justify revisiting:** a fourth dialect (the planned `pgx` group store is the likely trigger); a
   correctness bug that has to be fixed three times — **R-1 and R-11 on this branch were each exactly that**, and a
   third occurrence is the signal; or any further shared *semantics* (not syntax) landing in `AddMember`. Until one
   of those, the reviewer's own ordering holds: correctness outranks cleanup.

2. **`ErrOverflowDropped`'s root godoc names 2 of 5 producers.**

   ✅ **FIXED** in `errors.go`. **The finding UNDERCOUNTS — re-derived at HEAD there are six producing sites**
   (`grep -rn 'ErrOverflowDropped' --include='*.go' . | grep -v _test.go`): the streaming-source overflow policy in
   `endpoint`'s consumer; `memory.QueueStore.Enqueue` under `OverflowReject`; the **same** `Enqueue` under
   `OverflowDropOldest` when nothing is evictable; `memory.GroupStore`'s **group-count** arm; its **member-cap**
   arm; the three SQL dialects' `AddMember` via `sql.GroupStore.Add`; and `routing.Aggregator`'s
   `overflowRetryable`.

   **The godoc now names CLASSES, not a count** — consumer flow control, bounded channel stores, bounded group
   stores, the Aggregator — with the locating `grep` inline, because a count in a doc comment rots on the next
   store. **Two things it calls out that the old text got wrong or omitted:**
   - The old *"It is NOT returned for the silent Drop\* policies"* was **false**. `OverflowDropOldest` returns it
     when nothing is evictable (every entry in flight). Only `DropNewest` is silent.
   - **The Aggregator is the one producer that MINTS the sentinel fresh** rather than propagating a store's, and
     it is **always transient by construction** (D-AX). A caller reading the sentinel as "the store said so" would
     be wrong at that one site.

3. **`docs/specs/017` still reads "NOT approved for implementation" while its `feat` commits shipped.**

   ✅ **DONE — confirmed at HEAD.** Fixed in Spec 017 revision 6 (Task 10 Step 5). Verify with
   `grep -n 'NOT approved' docs/specs/017-group-member-bounds.md` — the only hits are historical annotations
   describing the old state, not the live status line.

4. **`ExampleWithReleaseWhen`'s hard-coded `false` predicate leaves its channel wiring dead.**

   ⏸️ **TRIAGED — real, cheap, and deferred only because it lives in `routing/`, outside the scope of the
   documentation pass that dispositioned this section.** It is a **vacuity defect in a runnable example**, which is
   the project's own recurring lesson: an example whose `// Output:` cannot change is documentation that compiles
   rather than documentation that demonstrates. Nothing about the fix is contested.

   **The fix, for whoever picks it up:** give the predicate a real condition so the release actually fires — e.g.
   release when the group reaches a stated size, or when a header marks the last member — and extend `// Output:`
   to show the aggregate arriving on the output channel. That exercises the wiring the example currently only
   declares. **Not a merge blocker under CLAUDE.md** (no untested hot-path branch, no incorrect exported
   behaviour), but it should not survive the next `routing` increment.

5. **The `memory` group-count arm carries the same asymmetry as R-5.**

   ✅ **FIXED — confirmed.** Closed in Task 11 Step 4 alongside R-5 itself, in the owning package. §5a records it:
   *"§6 item 5's group-**count** twin reproduced fully and is also asserted now."* The missing **assertion** was the
   real defect in both arms; a cross-package accident is not a contract.

## 7. Suggested sequencing for the fix session

1. **Decide the three design questions first**, and record each in ADR 0033 before touching code: does `AddMember` gain `leaseTTL` (R-7)? Does the SPI reject `maxMembers <= 0` with a typed error plus an explicit unbounded sentinel (R-15)? Is R-10's merge of *declined* and *failed* upheld or reversed (there is a test asserting the current behaviour)?
2. **Then the mechanical fixes**: R-6 (test `classified`), R-3 (gate on the sentinel, and handle typed-nil), R-12 (`reflect.Indirect`), R-9 (cover the branch).
3. **Then the ones that need new test fixtures**: R-1, R-11, R-4, R-5, R-8, R-14.
4. **Re-run the whole-branch gate afterwards** — `/code-review` and `/security-review` are **user-invocable only**; the model cannot run them (see [`docs/HANDOVER.md`](../HANDOVER.md) §5).

> **Mutation discipline applies to every fix here.** A mutant that does not compile reports as a KILL; one that
> fails to apply reports as a SURVIVAL. Both happened repeatedly on this branch. Require the mutation to **apply**
> and the tree to **build** before recording either outcome — see [`docs/HANDOVER.md`](../HANDOVER.md) §8.
