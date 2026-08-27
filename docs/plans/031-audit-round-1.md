# Plan 031 — adversarial design audit, round 1 (2026-08-22)

Independent Opus subagent, handed the **complete Plan 031 revision-1 bundle together** — [Spec
017](../specs/017-group-member-bounds.md) + [ADR 0033](../adrs/0033-group-member-bounds.md) +
[Plan 031](031-group-member-bounds.md) — **before any implementation code existed**, per
[`CLAUDE.md`](../../CLAUDE.md)'s design-time gate. All three artifacts declare themselves revision 1, pre-audit, and
carry the 🔴 *"decided WITHOUT USER RATIFICATION"* banner; that banner is not itself a finding, but every decision it
covers (**D-AC** … **D-AL**) was treated as open.

**Traceability.** Audits: [Spec 017](../specs/017-group-member-bounds.md),
[ADR 0033](../adrs/0033-group-member-bounds.md), [Plan 031](031-group-member-bounds.md). Origin:
[`docs/HANDOVER.md`](../HANDOVER.md) §6 backlog item **7**. Predecessors whose ratified decisions the bundle reuses:
[Spec 016](../specs/016-sizing-option-bounds.md), [ADR 0032](../adrs/0032-sizing-option-bounds.md),
[Plan 029](029-sizing-option-bounds.md), [ADR 0031](../adrs/0031-nil-option-elements.md) **D-R**. Colliding
concurrent work: [Plan 030](030-post-029-maintenance.md) (**landed during this audit**),
[Spec 018](../specs/018-byte-cap-ceilings.md) / [Plan 032](032-byte-cap-ceilings.md) (drafted, not started).

This record is **evidence-primary** — it is the audit artifact, not a user-facing summary. Every structural claim in
the bundle was re-derived on this tree with `GOTOOLCHAIN=go1.25.13`, darwin/arm64; the commands and their output are
pasted below. **Two trees are in play and the distinction is load-bearing:** the bundle's figures were measured at
`2b2dec1`, and **Plan 030 has since landed** (`7ab91cd`, `1a1c135`, `d2c69fe`), moving line numbers and rewriting one
of the bundle's target files. Unless a finding says otherwise, every measurement below is at **`d2c69fe`** — current
`main`.

> **⚠️ IMMUTABLE EXECUTION RECORD.** This file records what round 1 found, at the time it found it. Do not edit it to
> reflect later decisions, later measurements, or the revised bundle. Corrections belong in a later round's record or
> in the artifacts themselves. The coordinator's dispositions for these findings live in **Spec 017 / ADR 0033 /
> Plan 031 revision 2**, each of which cites this file.

**Verdict: NOT SAFE TO IMPLEMENT.** 3 BLOCKERs, 8 MAJORs, 10 MINORs.

The bundle's *design argument* is strong and mostly survives attack. §1.1's central claim — that three of the four
release paths are structurally unreachable from the option surface, so the bound must move to the store — reproduces
exactly and is not disturbed by anything below. What fails is **the two places where the design meets shipped
runtime behavior**: the error the store returns travels a settlement path the bundle never traced (B-1), and the
check's placement destroys a release opportunity the current code preserves (M-6). Both are runtime regressions that
no amount of option validation would surface, and both are invisible to the acceptance criteria as written.

---

## Finding index

| # | Rank | One line |
|---|---|---|
| **B-1** | BLOCKER | On the shipped default `RetryPolicy{}`, a rejected member hot-spins forever — no log, no dead-letter. D-AE/D-AJ/D-AK all rest on a sink the default config does not have |
| **B-2** | BLOCKER | The class gate is exact set equality in BOTH directions; six of nine tasks therefore commit a RED suite, violating the plan's own Global constraint 8 |
| **B-3** | BLOCKER | *"They share no file"* is false. Plan 030 was editing 4 of Plan 031's target files **during this audit**, and Plan 032 collides on the same `wantArms` map |
| **M-1** | MAJOR | The two new gate rows are specified with `1<<62`; post-030 the `fixed` arm uses `1<<30`. Following the plan verbatim re-breaks `GOARCH=386` |
| **M-2** | MAJOR | D-AG's *"nothing is committed"* is false for the `*sql.Tx` Querier branch — `pgRunInTx`/`mysqlRunInTx` `return fn(tx)` with no rollback. AC-4 never exercises it |
| **M-3** | MAJOR | sqlite has no `RunInTx` and no group row lock. Task 5 Step 2's ordering instruction has **no referent** there; §3.6/§7.1's blanket "group row lock" is postgres/mysql-only |
| **M-4** | MAJOR | D-AC's rationale is backwards: `store.Add` has exactly ONE caller, and `routing` ships NO stores. The conclusion is right; the stated reasons are not |
| **M-5** | MAJOR | *"No blackbox test can compare the two constants"* is **FALSE** — the class gate is a root blackbox test that already parses every non-test `.go` file in all 8 modules with `go/parser` |
| **M-6** | MAJOR | **NEW PERMANENT DEADLOCK** at the boundary: an id-less member at cap-1 whose release fails leaves a complete, releasable group that nothing will ever re-trigger. The current code has no such state |
| **M-7** | MAJOR | Task 8 Step 3 names 3 edit sites; the file states its counts in **ten**, two of them executable assertions |
| **M-8** | MAJOR | AC-10's vacuity probe is planted on a false premise: `adapter/database/sql` is **not a module** |
| **m-1** | MINOR | `IsPermanent` is `reliability.go:86-97`, not `:35-46` |
| **m-2** | MINOR | `MessageGroupStore.Add`'s godoc is `groupstore.go:38-45`, not `:41-52` |
| **m-3** | MINOR | `sql.GroupStore.Add` is `:250-276`, not `:249-280`; the `decodeGroupRows` call is `:275`, not `:279` |
| **m-4** | MINOR | §3.12.3 claims "different ceilings (`1<<20` and `1<<20`)" — the same number, twice |
| **m-5** | MINOR | "five call sites" omits the production site `sql/groupstore.go:271` **and** the interface declaration |
| **m-6** | MINOR | `classifyQueryErr` runs a `SchemaExists` probe on **every** dialect error — not a pass-through; it compounds B-1 |
| **m-7** | MINOR | `routing/example_test.go` does not exist |
| **m-8** | MINOR | The plan says five modules in one place and six in another |
| **m-9** | MINOR | Naming: `sql`'s `GroupStore` options are `WithGroup…`-prefixed by convention; `sql.WithMaxGroupMembers` breaks it |
| **m-10** | MINOR | `methodCount` stays **27** — say so; the gate hard-asserts it with `require.Equal` |

---

## BLOCKER B-1 — under the shipped defaults a rejected member hot-spins forever, with no log and no dead-letter

**The claim under attack.** Three decisions rest on the same premise. ADR 0033 **D-AE**: *"`Aggregator.Handle`
returns `store.Add`'s error unchanged, so the fault travels the runtime's ordinary `RetryPolicy`: retry with backoff,
dead-letter on exhaustion."* **D-AJ**: the default cap is safe because *"what changes at the boundary is the failure
mode, and D-AE makes it loud, typed, retryable and named."* **D-AK**'s consequence table, Observability row:
*"one typed, named `ErrOverflowDropped` per rejected member, into the operator's dead-letter store."*

**All three assume a `RetryPolicy` that has a `MaxAttempts` and a `DeadLetter`. The shipped zero value has neither.**

**The evidence.** `msgin.RetryPolicy`'s zero value is `MaxAttempts: 0, Backoff: nil, DeadLetter: nil`. Trace a
transient error through `consumer.go`'s settlement switch:

```
$ grep -n "case c.policy.MaxAttempts > 0\|^	default:" endpoint/consumer.go
862:	case c.policy.MaxAttempts > 0 && n >= c.policy.MaxAttempts && !c.native.NativeDeadLetter():
866:	default:
```

```go
endpoint/consumer.go:861-869
	n := c.attempts(d)
	switch {
	case c.policy.MaxAttempts > 0 && n >= c.policy.MaxAttempts && !c.native.NativeDeadLetter():
		if c.divert(settleCtx, c.policy.DeadLetter, d, c.hooks.OnDeadLetter, err, n) {
			c.tracker.evict(id)
		}
	default:
		c.safeFire(c.hooks.OnRetry, settleCtx, d.Msg, err)
		c.finish(c.safeNack(settleCtx, d, true, retryDelay(c.policy, n)))
	}
```

`MaxAttempts == 0` makes the guard `c.policy.MaxAttempts > 0` **false**, so the dead-letter arm is unreachable and
every attempt takes `default`. `OnRetry` is nil by default, so `safeFire` is a no-op. The Nack requeues with
`retryDelay(c.policy, n)`:

```go
endpoint/consumer.go:1323-1328
func retryDelay(p msgin.RetryPolicy, attempt int) time.Duration {
	if p.Backoff == nil {
		return 0
	}
	return p.Backoff.Delay(attempt - 1)
}
```

`Backoff == nil` → **delay 0** → immediate redelivery → `Add` rejects again → `default` again. **An infinite,
zero-delay hot spin. No log line is emitted on this path, and no message is ever dead-lettered.**

The runtime already knows this shape and documents it, one file over, for a different arm:

```
$ sed -n '96p' endpoint/consumer.go
// HEALTHY on the circuit breaker, hot-spins when Backoff is nil (the default),
```

**It is worse for `sql` than the status quo.** Each spin iteration is a full `AddMember` transaction (`BEGIN`, group-row
upsert-and-lock, member upsert, live-member `SELECT`, `ROLLBACK`) **plus** an extra `SchemaExists` probe — see m-6 —
against the database, forever, at whatever rate one goroutine can drive it. Today the same message simply appends and
succeeds. **The remedy makes the failure mode strictly worse for the store the bundle claims to be fixing "for the
first time."**

**The permanent arm, by contrast, behaves correctly on the zero value — and that is the whole shape of the fix.**

```go
endpoint/consumer.go:843-857
	if msgin.IsPermanent(err) {
		// … Settled TERMINALLY: one attempt at the sink, never a Nack (D-P).
		// Note (M8): the attempt tracker is deliberately NOT consulted here.
		sink, fellBack := c.invalidTarget(err)
		if fellBack {
			c.warnInvalidFallback(id)
		}
		if c.divertTerminal(settleCtx, sink, d, c.hooks.OnInvalidMessage, err) {
			c.tracker.evict(id)
		}
		return err
	}
```

It **never consults `MaxAttempts`**, it falls back to the dead-letter sink when no invalid-message sink is
configured, and it emits a WARN on that fallback. It is terminal by construction, so it cannot spin.

**Why the "transient is correct because a retry can work" argument does not rescue it.** Spec 017 §3.3 and D-AE both
argue the classification from *"an over-cap `Add` **can** succeed later — when the group releases … or when the
reaper expires it."* Both escapes are conditional on configuration the bundle elsewhere insists is **opt-in**: §3.11
states plainly that the remedy for a stuck group *"remains **opt-in**"*, and §1.2 re-verifies that with no
`WithGroupTimeout` the reaper interval is `0` and `Aggregator.Run` blocks on `ctx.Done()` without ever sweeping. So
in the **default** configuration — no timeout, no expiry channel, zero-value `RetryPolicy` — neither escape exists,
and "can succeed later" is false. **The classification is derived from a configuration the decision does not
require.**

**Required fix.** The store must distinguish the two rejection causes, which it can do because it holds `g.leased`
(`adapter/memory/groupstore.go:43`). A group at cap that is **not** leased will not drain on its own and must be
reported **permanently**; a group at cap that **is** leased has a claim window that will drain it and stays
transient. Rewrite D-AE, D-AJ and D-AK so no argument depends on a dead-letter sink the default configuration does
not have.

---

## BLOCKER B-2 — the class gate is exact set equality; six of nine tasks commit a RED suite

**The claim under attack.** Plan 031 Global constraint 8: *"**Each task is a green unit** — `GOWORK=off go test ./…
-race -shuffle=on` passes in **every module it touched** before its commit. No WIP or broken-build commits."*
Task 8 Step 2 then instructs: *"Run it again after Tasks 1 and 4 have landed: half 1 must now **FAIL** … **That
failure is the gate working and must be observed before it is fixed**."*

**These two instructions are contradictory, and the contradiction is not a matter of degree.**

**The evidence.** Half 1's assertion is a set comparison, not a subset check:

```go
sizing_option_class_gate_test.go:321-324
	assert.Equal(t, want, found, "the AST-discovered set of exported sizing-shaped functions must match "+
		"Spec 016 §2's 17-key conformance set in BOTH directions (ADR 0032 D-AA). …")
```

`want` is `sizingConformanceKeys` sorted; `found` is the AST scan sorted. The moment `memory.WithMaxGroupMembers`
exists on disk as an exported, `Recv == nil`, `int`-parameter function, `found` gains an element `want` does not
have and `TestSizingOptionClass_Completeness` **fails — in the ROOT module**, which is the module Tasks 1, 2, 3, 7
and 8 all touch and the module Task 4 touches.

The gate lives at the repository root and walks the filesystem, so **no import boundary shields it**:

```go
sizing_option_class_gate_test.go:258-300  (scanSizingParamRepo)
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		…
		if fd.Recv == nil {
			funcs = append(funcs, pkg+"."+fd.Name.Name)
```

Confirmed green on this tree, and confirmed to be the exact-equality shape:

```
$ GOTOOLCHAIN=go1.25.13 go test -run TestSizingOptionClass -v .
    sizing_option_class_gate_test.go:317: === EXPORTED FUNCTIONS (Recv==nil) with int/int64 param in ANY position: 17
    sizing_option_class_gate_test.go:318: === EXPORTED METHODS with int/int64 param: 27 (excluded by the Recv==nil boundary, Spec 016 §2.0)
--- PASS: TestSizingOptionClass_Completeness (0.15s)
--- PASS: TestSizingOptionClass_Conformance (0.00s)
```

**The blast radius.** After Task 1's commit, root's suite is red. It stays red through Tasks 2, 3, 4+5, 6 and 7, and
only goes green at Task 8 Step 3. **Six of the plan's nine tasks would therefore commit a broken build** — the exact
thing Global constraint 8 forbids and the exact thing per-task commit pre-authorization (CLAUDE.md's plan-execution
exception) is conditioned on not doing (*"Each task must be a **green unit** … no WIP/broken-build commits"*).

**This is not fixable by reordering Task 8 earlier**, either: adding the two keys to `sizingConformanceKeys` before
the options exist inverts the failure — `want` then has elements `found` does not — and half 2's conformance rows
would not compile, since they call constructors that do not yet exist.

**Required fix.** The gate rows must land in the **same commit** as the option they describe: `sizingConformanceKeys`
+ the `memory` conformance row inside Task 1's commit, the `sql` pair inside Task 4+5's. "Observe the RED first"
becomes a **within-task TDD step** (write the row, watch it fail, write the option) rather than a cross-task
condition. Global constraint 8 and Task 8 Step 2 must be reconciled explicitly, not left for the implementer to
notice at commit time.

---

## BLOCKER B-3 — "they share no file" is false, and the real dependency is the reverse of the one stated

**The claim under attack.** Plan 031's header box: *"This plan is therefore **031**, and the two are **independent**
— 030 touches `adapter/http`, godoc wording and test-file constants; 031 touches `routing`, `adapter/memory` and
`adapter/database/sql`. **They share no file.**"*

**The evidence.** Plan 030 was in flight during this audit and has now landed. Its diff against the tree the bundle
was measured on:

```
$ git log --oneline 2b2dec1..HEAD
d2c69fe test(core): make the sizing tests compile on 32-bit
1a1c135 docs(core): correct the false "first statement" godoc class
7ab91cd docs: retire the stale gin plan number and reserve ADR 0024
af7ce31 docs: hand over the backlog sweep at a documents-only safepoint
b54fbbe docs: plan the post-029 maintenance sweep, and close backlog item 2
2c4aa98 docs: design the group-member bound for every aggregator release path
```

**Four files are in both plans' target sets:**

| File | Plan 030 touched it | Plan 031 targets it |
|---|---|---|
| `adapter/memory/groupstore.go` | `1a1c135` (+1 line at `:93`) | Task 1 — the whole task |
| `adapter/database/sql/groupstore.go` | `1a1c135` (+1 line at `:207`) | Task 4 |
| `adapter/memory/queuestore.go` | `1a1c135` (+1 line) | cited by Spec 017 §3.3 for the producer count |
| `sizing_option_class_gate_test.go` | `d2c69fe` (**135 lines changed**) | Task 8 — the whole task |

```
$ git diff --stat 2b2dec1..HEAD -- adapter/memory/groupstore.go adapter/database/sql/groupstore.go sizing_option_class_gate_test.go
 adapter/database/sql/groupstore.go |   5 +-
 adapter/memory/groupstore.go       |   3 +-
 sizing_option_class_gate_test.go   | 135 +++++++++++++++-------------
```

**Every line citation in the bundle for those two `groupstore.go` files is now off by one**, because `1a1c135`
inserted a line **above** all of them:

```
$ git diff 2b2dec1..HEAD -- adapter/memory/groupstore.go
-// — checked as opts is applied (the loop is the first statement), so it runs
+// — checked as opts is applied (the loop is the first statement that can fail,
+// preceded only by the zero-value config initializer), so it runs
```

Verified downstream: the bundle's `groupstore.go:122-124` (the group-count arm) is now `:123-125`; `:134` (the
append) is `:135`; `:104-107` (the `checkRange` call) is `:105-108`; `queuestore.go:170, :175` are `:171, :176`.

**And the collision the plan does not mention at all is with Plan 032.** [Spec 018](../specs/018-byte-cap-ceilings.md)
/ [Plan 032](032-byte-cap-ceilings.md) are already drafted and target **the same `wantArms` map and the same
`sizingConformanceKeys` slice** in `sizing_option_class_gate_test.go`. Two plans cannot each "re-derive the arm
table" from a baseline the other is moving.

**Required fix.** Delete the false independence claim. Replace it with the real dependency: **Plan 031 must be
re-derived against post-030 HEAD before Task 1 starts**, and **Plans 031 and 032 serialize on
`sizing_option_class_gate_test.go`** — whichever lands second re-derives the arm table rather than editing a
pre-computed one.

---

## MAJOR M-1 — the two new gate rows are specified with the pre-030 literal

**The claim under attack.** Plan 031 Task 8 Step 3: *"Add both keys to `sizingConformanceKeys` (17 → **19**) and one
conformance row each in the **`fixed`** arm (both constructors reject `1<<62`)."* ADR 0033 **D-AL** and Spec 017
AC-8.2 say the same.

**The evidence.** Plan 030 Task 2 (`d2c69fe`) split the oversized literal **by arm**, precisely because a blanket
value leaves rows green while probing nothing. The `fixed` arm no longer uses `1<<62`:

```
$ grep -n "1 << 30\|1<<30" sizing_option_class_gate_test.go | head
415:	endpoint.WithMaxInFlight[any](1<<30)
427:	endpoint.WithConcurrency[any](1<<30)
438:	msghttp.WithConnectionBuffer(1 << 30)
451:	memory.New(memory.WithBuffer(1 << 30))
464:	memory.WithCapacity(1 << 30)
475:	memory.WithMaxGroups(1 << 30)
486:	msghttp.WithMaxConnections(1 << 30)
502:	routing.WithCompletionSize(1<<30)
513:	msghttp.WithReplayBuffer(1 << 30)
533:	msghttp.WithSuccessStatus(1 << 30)
```

The file's own header states why, and states the consequence of getting it wrong:

```
sizing_option_class_gate_test.go:47-53
//   - "fixed" (9) and "rejects" (1) → 1<<30 = 1,073,741,824. These rows assert
//     an EqualError against a rendered decimal. 1<<30 fits an int32 yet still
//     exceeds every ceiling in the codebase (the largest is 1<<20 = 1,048,576),
//     so it selects the identical out-of-range branch on both architectures
//     while keeping the expected decimal — 1073741824 — architecture-INDEPENDENT.
```

`1<<62` does not fit an `int` on `GOARCH=386` and **made the whole test binary fail to compile there** — the defect
`d2c69fe` exists to fix. An implementer following Task 8 Step 3 verbatim adds two `fixed` rows with `1<<62` and
**re-breaks 32-bit compilation**, silently, since nothing in the plan's per-task gate builds for 386.

**The three `deferred` rows are the exception and must not be swept in with a global fix.** They keep `1<<62`
deliberately (they take `int64`, so they compile on 386 and were never part of the defect), and the file marks them:

```
sizing_option_class_gate_test.go:557-561
		// 🔴 THESE THREE ROWS KEEP THE 1<<62 LITERAL — DO NOT CONVERT THEM
		// … WithMaxResponseBytes are `func(n int64)`, so 1<<62 is in range on EVERY
```

**Required fix.** The two new `fixed` rows use **`1<<30`**, and their `EqualError` strings carry the decimal
**1073741824**, not 4611686018427387904. State the three-way arm split (`fixed`/`rejects` → `1<<30`, `deferred` →
`1<<62`, `safe` → `math.MaxInt`) in the plan so the next reader does not "finish the job."

---

## MAJOR M-2 — D-AG's "nothing is committed" is false for the caller-owned-transaction branch

**The claim under attack.** ADR 0033 **D-AG**: *"returns the D-AE error, letting the existing `RunInTx` wrapper roll
the transaction back. **Nothing is committed.**"* Spec 017 §3.6 repeats it: *"the existing `pgRunInTx` wrapper rolls
the transaction back, so **nothing is committed**."*

**The evidence.** `pgRunInTx` has **three** branches, not one, and only the first rolls back:

```go
adapter/database/sql/postgres/groupdialect.go:52-68
func pgRunInTx(ctx context.Context, q msginsql.Querier, fn func(tx msginsql.Querier) error) error {
	if b, ok := q.(txBeginner); ok {              // *sql.DB → we own the tx
		tx, err := b.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if err := fn(tx); err != nil {
			_ = tx.Rollback()                     // ← the branch D-AG describes
			return err
		}
		return tx.Commit()
	}
	if tx, ok := q.(*stdsql.Tx); ok {             // *sql.Tx → the CALLER owns it
		return fn(tx)                             // ← NO rollback. NO commit.
	}
	return fmt.Errorf("msgin/sql/postgres: group ops require a *sql.DB or *sql.Tx Querier, got %T", q)
}
```

`mysqlRunInTx` (`adapter/database/sql/mysql/groupdialect.go:48-64`) is byte-for-byte the same shape.

**Consequence.** When the caller passes a `*sql.Tx` — an explicitly supported Querier, named in the dialect's own
error message — the over-cap member row is **already inserted into the caller's open transaction** and the overflow
error is merely returned. If the caller then commits (which it will, unless it inspects and matches
`msgin.ErrOverflowDropped` itself), **the cap is exceeded durably**. Enforcement (C) degrades to something weaker
than the rejected enforcement (A) on that path, because (A) at least never claimed durability.

**AC-4 never exercises it.** Spec 017 §6 AC-4 runs *"against a real database via the existing `dbtest`/`harness`
Docker-backed conformance runner"*, and the harness drives the dialect with a `*sql.DB`
(`adapter/database/sql/harness/groupstore.go:345`, `kit.Group.AddMember(ctx, db, …)`). The `*sql.Tx` branch has no
coverage in the plan at all.

**Required fix.** State the precondition explicitly — in D-AG's enforcement-point table, in `AddMember`'s interface
godoc, and on `sql.WithMaxGroupMembers`'s godoc: **the in-transaction bound is enforced by rollback only when the
dialect owns the transaction.** Under a caller-supplied `*sql.Tx`, the caller owns rollback and MUST treat
`msgin.ErrOverflowDropped` as a rollback trigger. Add an acceptance criterion that drives that branch.

---

## MAJOR M-3 — sqlite has no `RunInTx` and no group row lock; Task 5's ordering instruction has no referent there

**The claim under attack.** Plan 031 Task 5 Step 2: *"Inside the existing transaction, **after** the statement that
takes the group row lock and **after** the member upsert, count the live members …"* and *"the existing `RunInTx`
wrapper **rolls back**."* Spec 017 §3.6: *"Every dialect's `AddMember` runs one transaction that **takes the group
row lock first**."* §7.1 part 2 rests the entire cross-instance atomicity argument on it.

**The evidence — sqlite does neither of those things.**

There is no `sqliteRunInTx`. There is `withImmediateConn`, which takes a **dedicated connection** and issues raw
`BEGIN IMMEDIATE` / `COMMIT` / `ROLLBACK` text:

```go
adapter/database/sql/sqlite/groupdialect.go:52-77
func withImmediateConn(ctx context.Context, q msginsql.Querier, fn func(conn msginsql.Querier) error) error {
	opener, ok := q.(connOpener)
	if !ok {
		return fmt.Errorf("msgin/sql/sqlite: group ops require a *sql.DB Querier (dedicated BEGIN IMMEDIATE conn), got %T", q)
	}
	…
	if _, err := conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil { return err }
	if err := fn(conn); err != nil {
		if _, rbErr := conn.ExecContext(ctx, "ROLLBACK"); rbErr != nil { discardConn(conn) }
		return err
	}
	…
}
```

And there is **no group row lock**. sqlite uses `DO NOTHING` plus a *separate* `SELECT`, exactly the shape the
postgres dialect's own comment says it rejected:

```go
adapter/database/sql/sqlite/groupdialect.go:112-124
		INSERT INTO %s (group_key, created_at, epoch) VALUES (?, %s, 0)
		ON CONFLICT (group_key) DO NOTHING
		…
		SELECT created_at FROM %s WHERE group_key = ?
```

versus postgres, whose `RETURNING` upsert is what locks:

```go
adapter/database/sql/postgres/groupdialect.go:107-110
		INSERT INTO %s (group_key, created_at, epoch) VALUES ($1, %s, 0)
		ON CONFLICT (group_key) DO UPDATE SET group_key = EXCLUDED.group_key
		RETURNING created_at
```

sqlite's serialization comes from `BEGIN IMMEDIATE`, which takes a **database-wide** write lock — stronger than a row
lock, but a different mechanism entirely, and one an implementer told to "sit after the group row lock" cannot
locate. mysql is a third shape again (`INSERT … ON DUPLICATE KEY UPDATE group_key = group_key`, X-locking the group
row, `adapter/database/sql/mysql/groupdialect.go:93-96`).

**Required fix.** Replace the blanket instruction with a per-dialect placement table. State §3.6/§7.1's atomicity
claim per engine — postgres: `RETURNING` upsert row lock; mysql: `ON DUPLICATE KEY UPDATE` X lock; sqlite:
`BEGIN IMMEDIATE` whole-database write lock — rather than asserting one mechanism for all three. Drop "the existing
`RunInTx` wrapper" from sqlite's step; the wrapper there is `withImmediateConn`.

---

## MAJOR M-4 — D-AC's rationale is backwards; the conclusion survives, the reasons do not

**The claim under attack.** ADR 0033 **D-AC**, reasons 1 and 3. Reason 1: *"The store, by contrast, observes **every**
member that joins a group, for every path"* — offered as the discriminator against enforcing in `routing`. Reason 3:
*"A bound stated in `routing` protects only the stores `routing` ships with."*

**The evidence — reason 1 does not discriminate.** `store.Add` has exactly **one** caller in the entire workspace:

```
$ grep -rn "store.Add(\|\.store\.Add(" --include='*.go' . | grep -v _test
routing/aggregator.go:412:	group, err := a.store.Add(ctx, key, msg)
```

`Aggregator.Handle` calls it unconditionally, before any release decision, for every member. **So `Handle` also
observes every member that joins a group.** The alternative D-AC is arguing against is not "enforce at the release
decision" (which §1.1 correctly demolishes); it is "enforce in `Handle`", and `Handle` sees exactly the same
population the store does. Reason 1, as written, is true of both sites and therefore selects neither.

**Reason 3 is inverted.** `routing` ships **no** stores:

```
$ ls routing/
aggregator.go  aggregator_settlement_test.go  aggregator_test.go  …  splitter.go
$ grep -rn "MessageGroupStore" routing/*.go | grep -v _test
routing/aggregator.go:  (interface parameter only — no implementation)
```

Both first-party stores live in `adapter/memory` and `adapter/database/sql`. A check placed in `routing.Aggregator`
would therefore cover **every** store an Aggregator is pointed at — including third-party ones — which is *more*
coverage than a per-store check, not less. Reason 3 states the opposite of the fact.

**The conclusion is nevertheless right, for a reason the ADR does not give.** By the time `Handle` sees `Add`'s
returned snapshot, **the member has already been appended and retained**. A check in `Handle` would observe the
over-cap condition only *after* the store had grown — bounding the reported size, not the retained memory. That is
the false-safety inversion §3.6 rejects for SQL enforcement (A), applied to the memory store. **Only the store can
refuse a member before retaining it.**

**Required fix.** Rewrite D-AC's rationale to that post-hoc argument. Delete reason 3, or restate it truthfully as
*"a store used directly, without an Aggregator, is otherwise unbounded"* — which is a real, if narrower, benefit.
Reason 2 (precedent inside the same function) survives unchanged.

---

## MAJOR M-5 — "no blackbox test can compare the two constants" is false, and the chosen defence is decorative

**The claim under attack.** Spec 017 §3.5: *"They live in **different packages** … and both are **unexported**, so a
blackbox test cannot compare them directly."* §6 AC-3's red box: *"both constants are unexported and in different
packages, so no blackbox test can compare them."* ADR 0033 Consequences: *"**The invariant … is NOT mechanically
enforced.** … The defence is a cross-reference comment on each constant plus a grep in the final task."* Spec 017 §8
item 4 lists it as an accepted open item.

**The evidence — the technique that refutes this is already shipped, in the very file Task 8 edits.**
`sizing_option_class_gate_test.go` is a **root blackbox test** (`package msgin_test`) that parses **every non-test
`.go` file in all eight modules** with `go/parser` and reads their declarations off the AST:

```go
sizing_option_class_gate_test.go:280-282
		f, perr := parser.ParseFile(fset, rel, src, parser.SkipObjectResolution)
```

Unexportedness and package boundaries are **irrelevant to a parser**. `go/ast` reads
`const completionSizeCeiling = 1 << 16` and `const maxGroupMembersDefault = 1 << 16` as `*ast.GenDecl` /
`*ast.ValueSpec` nodes with `*ast.BasicLit`/`*ast.BinaryExpr` values, in whatever package and with whatever case.
Both constants are single-file, single-line, literal declarations:

```
$ grep -n "completionSizeCeiling = " routing/aggregator.go
33:const completionSizeCeiling = 1 << 16
$ grep -n "maxGroupsCeiling = " adapter/memory/groupstore.go
62:const maxGroupsCeiling = 1 << 20
```

An AST invariant test — parse `routing/aggregator.go` and `adapter/memory/groupstore.go`, evaluate both shift
expressions, assert `defaultMaxGroupMembers >= completionSizeCeiling` — is **strictly less work than the
cross-reference comments the bundle proposes instead**, and unlike them it fails when someone edits one number.

**The chosen defence is decorative.** Two prose comments plus a one-time `grep` in Task 9 Step 5 protect nothing
after Task 9 Step 5 has run. The bundle's own framing concedes this (*"record the drift risk honestly rather than
claiming it is closed"*) — but the risk does not need to be recorded, it needs to be closed, and the tool to close
it is already in the repository and already in this increment's edit set.

**Required fix.** Add the AST invariant test as a task step, with a killing mutant (change either literal ⇒ the test
fails). Delete Spec 017 §8 item 4 and the "unenforceable"/"not mechanically enforced" claims from §3.5, §6 AC-3 and
ADR 0033's Consequences.

---

## MAJOR M-6 — the check's placement introduces a NEW permanent deadlock that the current code does not have

**The claim under attack.** Plan 031's "The counted set" box: *"**The `memory` check must sit BEFORE the append and
AFTER the dedup branch.**"* Spec 017 §3.5: the claim-window rejection is *"**Bounded** … **Recoverable** … **An
accepted consequence, not a defect**."* ADR 0033 **D-AK**: the worst case is *"bounded-but-stuck"*, and the liveness
row is *"**still never releases**"* — i.e. **unchanged** from today.

**D-AK's claim that liveness is unchanged is false. The change creates a stuck state that does not exist today.**

**The evidence.** Trace an **id-less** message (`msg.ID() == ""`) — a supported shape, explicitly branched on at
`adapter/memory/groupstore.go:127` — against a group at `cap-1`, post-change:

| Step | What happens | Site |
|---|---|---|
| 1 | `Add`: `len(g.msgs) == cap-1`, cap check passes, append → `len == cap` | `groupstore.go:135` |
| 2 | `Add` returns a snapshot of `cap` members, nil error | `groupstore.go:136` |
| 3 | `Handle`: release predicate **fires** on `cap` members | `routing/aggregator.go:426` |
| 4 | `ClaimGroup` → claim; `a.release` → `releaseOnce` **fails** (agg error, or output `Send` fails) | `aggregator.go:440-441`, `:451-467` |
| 5 | the deferred abandon runs: `AbandonGroup` sets `g.leased = false`, `g.claimedLen = 0` — **and does NOT shrink `g.msgs`** | `groupstore.go:196-197` |
| 6 | `Handle` returns the release error → Nack → **redelivery of the same message** | `aggregator.go:492` |
| 7 | `Add` again: id is `""`, so **the dedup branch is skipped entirely** | `groupstore.go:127` |
| 8 | cap check: `len(g.msgs) == cap >= cap` → **REJECT**, before the release decision is ever reached | new arm |

**The group now holds a complete, releasable set of `cap` members that nothing will ever re-trigger.** The release
predicate is satisfied. `SettleGroup` never runs (no successful release). The reaper is opt-in and off by default
(§1.2). `Add` rejects every future member for that key, including the redelivery of the one that would have
re-fired the release.

`AbandonGroup`'s own godoc states the invariant the new arm breaks:

```go
adapter/memory/groupstore.go:185-197
// AbandonGroup releases the lease WITHOUT deleting: the claimed members return
// to live (along with anything appended during the lease) so a retry / next
// member / next reaper tick re-releases.
	g.leased = false // members return to live (all of msgs, incl. any appended during the lease)
	g.claimedLen = 0 // epoch stays bumped so the abandoned holder's later settle is a no-op
```

*"so a retry / next member / next reaper tick re-releases"* — **the cap check removes exactly the "retry / next
member" half of that recovery**, and the reaper half is off by default.

**Before this change, step 8 appended and re-fired the release, which succeeded.** So this is a **regression**, not
an inherited limitation, and D-AK's "liveness unchanged" row is wrong.

**Scope, measured.** The deadlock is `memory`-only and id-less-only:

- With a **non-empty id**, the dedup branch at `groupstore.go:128-131` returns the snapshot with a **nil** error, so
  `Handle` reaches the release predicate and re-fires. No deadlock.
- `sql.GroupStore.Add` **rejects an empty msg id before any query runs**
  (`adapter/database/sql/groupstore.go:251-253`, `return nil, ErrMissingMsgID`), and every dialect's `AddMember`
  repeats the check. **The id-less path is unreachable for `sql`.**

**A second, narrower defect at the same site, found while tracing this.** Plan 031's instruction places the cap
check *after* the dedup branch — i.e. after `g.ids[id] = struct{}{}` at `groupstore.go:132`. That records the
member's id as *seen* and then rejects it. On redelivery the dedup branch returns the snapshot with a **nil** error,
`Handle` returns nil, and **the source Acks a message that was never appended** — silent data loss, which is
precisely what Spec 017 §5 rejects under *"Drop the over-cap member silently."* The cap check must sit **between**
the `seen` lookup and the `g.ids` insert.

**Required fix.** Do not accept this. On rejecting an over-cap member, `Add` must still **return the current live
group snapshot alongside the error**, so `Handle` can re-evaluate the release predicate rather than losing the
release opportunity. Reject the *member*, not the *release*. Specify exactly what `Handle` does with a non-nil
snapshot **and** a non-nil error, and add an acceptance criterion driving the id-less path explicitly. Separately,
move the cap check above the `g.ids` insert.

---

## MAJOR M-7 — Task 8 Step 3 names three edit sites; the file states its counts in ten

**The claim under attack.** Plan 031 Task 8 Step 3: *"Update every count the file states, in **all** of the header
comment, the arm table and the pairwise `arm` map."*

**The evidence.** Ten distinct sites state a count that this increment moves. Two of them are **executable
assertions**, not prose:

| # | Site | What it states | Executable? |
|---|---|---|---|
| 1 | `:19` | "Every one of the **17** AST-discovered keys" | no |
| 2 | `:38` | "9 + 1 + 3 + 6 = **19** rows = **17** AST keys + 2 manual rows" | no |
| 3 | `:47` / `:55` / `:61` | per-arm counts "(9)", "(3)", "(6)" in the by-arm literal split | no |
| 4 | `:83-85` | "Recv == nil yields **17** keys; ANY FuncDecl yields **44**, of which 22 sit on UNEXPORTED receivers" | no |
| 5 | `:92` | "applying it to the **27** excluded methods" | no |
| 6 | `:107-108` | "All **17** keys live in root-module packages today (**endpoint, adapter/http, adapter/memory, channel, resilience, routing**)" | no |
| 7 | `:176` / `:210` | "cross-check the full **17**" / "the key set is unchanged at **17**" | no |
| 8 | `:322` | `assert.Equal(t, want, found, "…Spec 016 §2's **17-key** conformance set…")` | **message only** |
| 9 | `:335` | `require.Equal(t, 27, methodCount, …)` | **YES** |
| 10 | `:753-754` | `require.Len(t, tests, 19, "**17** AST rows + 2 manual rows…")` | **YES** |

```
$ grep -n "require.Len(t, tests" -A 1 sizing_option_class_gate_test.go
753:	require.Len(t, tests, 19,
754:		"17 AST rows + 2 manual rows (memory.QueueStore.Claim, channel.QueueChannel.Poll — Spec 016 §2.0)")
```

**Site 6 is the one that matters most, and it is not a count at all — it is a claim this increment falsifies.** The
accepted-limitation header asserts that all keys live in the six packages it lists. `sql.WithMaxGroupMembers` lives
in `adapter/database/sql`, which is **not on that list**. Leaving the list unedited turns a stated limitation into a
false statement about the gate's own coverage — the exact failure mode of the project's stored lesson *"docs can
contradict the code they describe."*

Site 4's `44` is derived (`17 + 27`) and becomes `46`. Site 3's per-arm "(9)" becomes "(11)". Line `:691`'s inline
comment *"burst is the 17th key, positional"* is an **ordinal** into `sizingConformanceKeys`, and survives only if
the two new keys are appended after `resilience.NewTokenBucket`.

**Required fix.** Enumerate all ten sites in the task, flag sites 9 and 10 as executable, and single out site 6 as a
falsified claim rather than a stale number.

---

## MAJOR M-8 — AC-10's vacuity probe is planted on a false premise

**The claim under attack.** Spec 017 §6 AC-10: *"**Prove it COVERS, not just that it FIRES** — plant the half-1
probe in `adapter/database/sql`, **the module this increment newly touches**, not in root."* Plan 031 Task 8 Step 6
repeats it: *"plant a third `WithMaxGroupMembers`-shaped option in **`adapter/database/sql`** (the module this
increment newly touches — *not* root)."*

**The evidence. `adapter/database/sql` is not a module. It is a package in the root module.**

```
$ find . -name go.mod -not -path "./.git/*" | sort
./adapter/cron/crontest/go.mod
./adapter/database/sql/dbtest/go.mod
./adapter/database/sql/harness/go.mod
./adapter/database/sql/mysql/go.mod
./adapter/database/sql/postgres/go.mod
./adapter/database/sql/sqlite/go.mod
./expr/go.mod
./go.mod
$ ls adapter/database/sql/go.mod
ls: adapter/database/sql/go.mod: No such file or directory
```

CLAUDE.md's Commands section says so directly: the root `go test ./...` covers 11 packages including
`adapter/database/sql`.

**Why it matters — the probe as specified proves the wrong thing.** The precedent AC-10 invokes is Plan 029's
finding that *"Plan 028's `apidiff` blindness survived Task 0 because its probe was planted in root — proving the
gate **fires** is not proving it **covers**."* The coverage question for half 1 is whether the **filesystem walk
reaches leaf modules that have their own `go.mod`** — which the header's own ROOT-MODULE IMPORT BOUNDARY limitation
(`:105-112`) says is the interesting boundary. A probe in `adapter/database/sql` is a probe **in root**, dressed as
a probe outside it. It answers a question that was never in doubt.

**The modules this increment newly touches are `postgres`, `mysql`, `sqlite` and `harness`** — and none of them
currently contributes a key:

```
$ grep -rnE "^func [A-Z][A-Za-z]*\(.*\b(int|int64)\b" adapter/database/sql/harness/*.go
$   (no output)
```

**Required fix.** Plant the probe **twice** and record both outcomes: once in `adapter/database/sql/postgres` (a
real leaf module — proves the walk crosses a `go.mod`) and once in `adapter/database/sql` (a same-module package —
proves the ordinary path). Correct the "module" wording throughout Spec 017, ADR 0033 and Plan 031.

---

## MINOR m-1 — `IsPermanent`'s citation is wrong by 51 lines

Spec 017 §3.3 and ADR 0033 **D-AE** both cite *"`IsPermanent` (`reliability.go:35-46`)"*.

```
$ grep -n "func IsPermanent" -A 11 reliability.go
86:func IsPermanent(err error) bool {
…
94:	return errors.Is(err, ErrPayloadType) ||
95:		errors.Is(err, ErrPayloadDecode) ||
96:		errors.Is(err, ErrPayloadTooLarge)
97:}
```

It is `reliability.go:86-97`. The *content* of the claim (`ErrOverflowDropped` is not among the matched sentinels)
is correct. **Required fix:** cite `:86-97`. Note that CLAUDE.md's Dependency-policy paragraph carries the same
stale `reliability.go:46` citation.

---

## MINOR m-2 — the SPI godoc's line range is wrong

Spec 017 §3.7 and ADR 0033 **D-AH** cite `MessageGroupStore.Add`'s godoc as `groupstore.go:41-52`; Plan 031 Task 7
Step 2 repeats it.

```
$ grep -n "Add(ctx context.Context" groupstore.go
45:	Add(ctx context.Context, key string, msg Message[any]) (MessageGroup, error)
```

The doc comment runs `:38-44` and the method declaration is `:45`. **Required fix:** cite `groupstore.go:38-45`.

---

## MINOR m-3 — `sql.GroupStore.Add`'s range and the `decodeGroupRows` site are both wrong

Spec 017 §1.3 and ADR 0033's Context cite `adapter/database/sql/groupstore.go:249-280`, and §1.3 item 3 cites
`decodeGroupRows` at *"(`groupstore.go:270-280`)"*, with Spec 017 §1.3 item 1 putting the call at `:279`.

```
$ grep -n "func (s \*GroupStore) Add\|decodeGroupRows\|AddMember(" adapter/database/sql/groupstore.go
250:func (s *GroupStore) Add(ctx context.Context, key string, msg msgin.Message[any]) (msgin.MessageGroup, error)
271:	rows, err := s.dialect.AddMember(ctx, s.db, s.table, key, msgID, seq, headers, payload)
275:	return s.decodeGroupRows(rows)
365:func (s *GroupStore) decodeGroupRows(rows GroupRows) (groupSnapshot, error)
```

`Add` spans `:250-276`; the `decodeGroupRows` **call** is `:275`; the `decodeGroupRows` **definition** is `:365-…`,
nowhere near `:270-280`. **Required fix:** cite the call and the definition separately, at their real lines.

---

## MINOR m-4 — §3.12.3 contradicts itself inside one parenthesis

Spec 017 §3.12 item 3: *"group **count** and group **members** are different quantities with **different ceilings**
(`1<<20` and `1<<20`) and different defaults (1024 and 65,536)."*

`1<<20` and `1<<20` are the same number. D-AD in fact chooses `1<<20` for `WithMaxGroupMembers` **precisely because**
it matches `maxGroupsCeiling`, so the sentence asserts the opposite of the decision it describes.

**Required fix:** *"the same ceiling (`1<<20`, deliberately — D-AD) and different defaults (1024 and 65,536)."*

---

## MINOR m-5 — "five call sites" omits the production call site and the interface declaration

Spec 017 §3.6 and ADR 0033 **D-AG**: *"the change reaches five call sites the plan enumerates: `postgres`, `mysql`,
`sqlite`, `harness/groupstore.go:345`, and `groupdialect_fake_test.go:137`."*

```
$ grep -rn "AddMember(" --include='*.go' .
adapter/database/sql/groupdialect.go:126:	AddMember(ctx context.Context, q Querier, table, groupKey, msgID string, seq int64, headers, payload []byte) (GroupRows, error)
adapter/database/sql/groupdialect_fake_test.go:137:func (f *fakeGroupDialect) AddMember(…)
adapter/database/sql/groupstore.go:271:	rows, err := s.dialect.AddMember(ctx, s.db, s.table, key, msgID, seq, headers, payload)
adapter/database/sql/harness/groupstore.go:345:			_, err = kit.Group.AddMember(ctx, db, table, key, id, seq, headers, []byte(`"p"`))
adapter/database/sql/mysql/groupdialect.go:75:func (mysqlGroupDialect) AddMember(…)
adapter/database/sql/postgres/groupdialect.go:80:func (postgresGroupDialect) AddMember(…)
adapter/database/sql/sqlite/groupdialect.go:102:func (sqliteGroupDialect) AddMember(…)
```

Seven sites: the **interface declaration** (`groupdialect.go:126`), three dialect implementations, the **production
call** (`groupstore.go:271`), the harness call, and the test fake. The production call is the one that must thread
`s.maxGroupMembers`, and it is the only one the list omits that changes *behavior* rather than *signature*.

**Required fix:** enumerate all seven, and say which are declarations, which are implementations and which are calls.

---

## MINOR m-6 — `classifyQueryErr` is not a pass-through; it costs an extra round-trip on every overflow

Neither Spec 017 §3.6 nor D-AG mentions what happens to the dialect's overflow error on the way out of
`sql.GroupStore.Add`.

```go
adapter/database/sql/groupstore.go:271-274
	rows, err := s.dialect.AddMember(ctx, s.db, s.table, key, msgID, seq, headers, payload)
	if err != nil {
		return nil, s.classifyQueryErr(ctx, err)
	}

adapter/database/sql/groupstore.go:91-96
func (b groupBase) classifyQueryErr(ctx context.Context, err error) error {
	if exists, probeErr := b.dialect.SchemaExists(ctx, b.db, b.table); probeErr == nil && !exists {
		return b.schemaNotReady()
	}
	return err
}
```

**Every** dialect error — including a routine, expected overflow rejection — triggers a `SchemaExists` query against
the database before the error is returned. The `errors.Is` chain is preserved (the error is returned unchanged when
the table exists), so correctness is unaffected; the cost is not. **This compounds B-1**: each hot-spin iteration is
a rolled-back `AddMember` transaction **plus** a `SchemaExists` probe.

**Required fix:** name the extra round-trip as a stated cost of the overflow arm, and confirm in an AC that the
overflow error survives `classifyQueryErr` with its `errors.Is` target intact.

---

## MINOR m-7 — `routing/example_test.go` does not exist

Plan 031 Task 3 **Files:** *"`routing/aggregator.go` (godoc only), `headers.go` or wherever `msgin.HeaderSequenceSize`
is declared, `routing/example_test.go` (one runnable example)."*

```
$ ls routing/*_test.go
routing/aggregator_settlement_test.go     routing/example_splitter_test.go   routing/predicate_test.go
routing/aggregator_test.go                routing/filter_test.go             routing/router_test.go
routing/completion_size_bounds_test.go    routing/main_test.go               routing/splitter_test.go
routing/example_aggregator_test.go        routing/permanent_classification_test.go
routing/example_predicate_test.go
```

The convention is `example_<subject>_test.go`. **Required fix:** name the file `routing/example_aggregator_test.go`
(extend the existing one) or `routing/example_group_bound_test.go` (new). Separately, the plan's hedge *"`headers.go`
or wherever"* resolves to `message.go:24` — resolve it rather than hedging.

---

## MINOR m-8 — the module count is five in one place and six in another

Plan 031 **Tech stack**: *"Touches **five** of the eight modules — root … plus
`adapter/database/sql/{postgres,mysql,sqlite,harness}`"*. That enumeration is 1 + 4 = **five**. But Task 6 declares
**Modules:** `harness`, `dbtest`, and the Sizing table's Task 6 row says `harness, dbtest` — making **six**. Spec 017
§8 item 2 says the SPI change costs *"five call sites in **four** modules"*, a third figure for a related quantity.

**Required fix:** state it once — **six** modules touched (root, postgres, mysql, sqlite, harness, dbtest) — and
derive the "four modules" figure in §8 from the same list.

---

## MINOR m-9 — `sql.WithMaxGroupMembers` breaks the package's own option-naming convention

`adapter/database/sql`'s `GroupStore` options are uniformly `WithGroup…`-prefixed, which is how a reader tells a
group-store option from a queue-store or lease option in a package that has all three:

```
$ grep -n "^func WithGroup" adapter/database/sql/groupstore.go
	WithGroupLeaseTTL, WithGroupLockedBy   (plus the group logger option)
```

`WithMaxGroupMembers` would be the only `GroupStore` option in that package without the prefix. `adapter/memory` has
no such convention (`WithMaxGroups`, `WithGroupClock`), so the name is unobjectionable there.

**Required fix:** decide explicitly — either `sql.WithMaxGroupMembers` (one name across both packages, at the cost of
`sql`'s prefix convention) or `sql.WithGroupMaxMembers` (convention preserved, at the cost of a name mismatch) — and
write down which and why. Do not leave it to whoever writes the code.

---

## MINOR m-10 — `methodCount` stays 27, and the gate hard-asserts it

Neither Spec 017 AC-8 nor Plan 031 Task 8 says what happens to the excluded-method count. It matters, because it is
a `require`, not a log:

```go
sizing_option_class_gate_test.go:335-337
	require.Equal(t, 27, methodCount, "the excluded-method count moved — re-derive Spec 016 §2.0's "+
		"Recv == nil boundary … before updating this number; do not just bump it to make the gate pass")
```

**It does not move.** `GroupDialect.AddMember` gains an `int` parameter, but all three dialect implementations
**already** have `seq int64` and are therefore already counted — the file's header even names one of them:

```
sizing_option_class_gate_test.go:84-86
// ANY FuncDecl yields 44, of which 22 sit on UNEXPORTED receivers
// (mysqlDialect.Claim, postgresGroupDialect.AddMember, ... — 21 in
```

Adding a parameter to an already-matching method changes nothing. `27` stands.

**Required fix:** state it in AC-8 and in Task 8 — *"`methodCount` stays 27; the dialect `AddMember` methods already
match on `seq int64`"* — so the next reader does not re-derive it or, worse, bump it.

---

## What I checked and found CLEAN

Recorded so round 2 does not re-derive it. Every item below was attacked and survives.

**The core design argument.**

- **§1.1's four-release-path table is exactly right.** `WithCompletionSize` at `aggregator.go:154`,
  `WithReleaseStrategy` at `:116`, `WithReleaseWhen` at `:128`, `defaultRelease` at `:222`, and the guard gated on
  `cfg.completionSizeSet` at `:353-358` — all verified. The claim that paths 2–4 never set the flag reproduces.
- **§1.4's two-mechanism analysis is correct and is an improvement on the backlog entry.**
  `WithReleaseWhen(fn func(msgin.MessageGroup) bool)`'s parameter is an `*ast.FuncType` (falls through
  `isIntOrInt64`'s switch at `:231-243`); `WithReleaseStrategy(fn ReleaseStrategy)`'s is an `*ast.Ident` whose name
  is neither "int" nor "int64". Two different blind spots, only one of which the gate's header already covers.
- **The conclusion that no option-surface gate can reach path 4 is unassailable.** `defaultRelease` reads
  `msgin.HeaderSequenceSize` (`aggregator.go:227`) — there is no parameter to scan.
- **§1.2's measurement is correctly cited, not re-derived.** The 8.6 s / 48.3 GiB figures are Spec 016's, and the
  bundle's instruction *"Do not re-derive these figures for this increment — cite them"* is right.
- **The quadratic claim is real.** `slices.Clone` on both return paths (`adapter/memory/groupstore.go:131`, `:136`).
- **The `sql` asymmetry argument in §1.3 survives.** `AddMember`'s contract does return the whole live member set,
  and `decodeGroupRows` does a `DecodeHeaders` per member. The reasoning for bounding members in both stores while
  bounding group count in neither is sound.

**Facts the bundle asserts that reproduce exactly.**

- `maxGroupsCeiling = 1 << 20` at `adapter/memory/groupstore.go:62`. ✔
- `completionSizeCeiling = 1 << 16` at `routing/aggregator.go:33`. ✔
- **`checkRange` has exactly four copies today** — `endpoint`, `routing`, `adapter/memory`, `adapter/http` — and
  `adapter/database/sql` has none. The "gains a fifth" claim is accurate. ✔
- **`msgin.ErrInvalidCapacity` has exactly four producers today**, and reaches six: `memory.WithBuffer`
  (`adapter/memory/memory.go:82`), `memory.WithMaxGroups` (`groupstore.go:105`), `memory.WithCapacity`
  (`queuestore.go:114`), `routing.WithCompletionSize` (`aggregator.go:354`). ✔ *(Plan 031 Task 9 Step 6 names them
  by enclosing constructor rather than by option; the count is right either way.)*
- **`msgin.ErrOverflowDropped` has exactly four producer sites**, though only three are returns:
  `memory/groupstore.go:124`, `memory/queuestore.go:171`, `:176` (returns) and `endpoint/consumer.go:576`
  (an `OnRetry` hook argument, not a return). ✔
- **`grep -rn 'msginsql.GroupDialect'` finds only first-party implementers.** The breaking-change affordability
  argument holds. ✔
- **`GroupDialect`'s godoc does reserve the right to evolve** — *"a pre-1.0 (v0) contract that may still evolve"*,
  `adapter/database/sql/groupdialect.go:106`. ✔
- **`Aggregator.Handle` does return `store.Add`'s error unchanged** (`routing/aggregator.go:412-415`), and the
  nil-group guard at `:416-424` is real and correctly described. ✔
- **The five-part fixture note is correct and worth keeping.** `NewAggregator` needs `store`, `fn`,
  `WithOutputChannel`, `WithCorrelationStrategy`, plus `Subscribe`. ✔
- **`memory.GroupStore` is in-process only, and `groupState.leased` is unconditional** (no wall-clock TTL) —
  `groupstore.go:43`, `:141`. §7.1's topology reasoning is right. ✔
- **§7.1 part 3's configuration-coherence requirement is real and correctly framed** as unenforceable-by-the-library,
  in the same family as `WithGroupLeaseTTL`. ✔
- **AC-9 branch 2's warning is the sharpest thing in the bundle.** A cap check written as `> cap` after the append
  admits `cap+1` while every overflow test still passes. That branch genuinely is the one no other case catches. ✔

**Decisions attacked and left standing.**

- **D-AD's "no new sentinel."** The six-producer argument is thin but honest, and *"a seventh needs its own ADR"* is
  the right guard rail. No finding.
- **D-AF's counting asymmetry** (flagged by the bundle itself as most likely to be reversed). It survives: each
  store bounds what it actually retains, and the SPI godoc in §3.7 is written to admit both. **Not** a finding.
- **D-AJ's ceiling→default *a fortiori* argument.** The reasoning is valid **given** a loud, recoverable failure
  mode — which is exactly the premise B-1 destroys. Fix B-1 and D-AJ stands; leave B-1 and D-AJ falls with it.
- **D-AL's refusal to widen the AST scan.** Correct, and correctly argued: catching `*ast.FuncType` finds one of
  three and would read as complete.
- **The choice of enforcement (C) over (A) for SQL.** Sound — subject to M-2's precondition and M-3's per-dialect
  correction.
- **Spec 017 §5's rejected-alternatives table.** Every row is correctly reasoned; nothing there needs revisiting.
- **The plan's header note, Global constraints 1–7 and 9–11.** They restate CLAUDE.md's Go-skills hard rule, the
  blackbox/assert-closure rules, the fixed error shape and the docs-link relativity trap correctly. Only constraint
  8 is defective (B-2).

**Gates verified non-vacuous or verified clean.**

- **The docs-link gate baseline is clean** at this branch point: exactly the two known arm-1 false positives
  (`docs/plans/016-aggregator.md -> docs/plans/m` and `docs/specs/006-cron-source.md -> docs/specs/factory(fireTime`
  — both Go identifiers leaking from line-wrapped inline code, not links) and **zero** arm-2 hits.
- **All four bundle files' own internal links resolve** (checked by hand; the three artifacts were untracked when
  drafted, so `git ls-files` had not scanned them).
- **`harness` contributes no half-1 key**, so Task 6 cannot perturb the gate.

---

## Auditor's method note

Every command in this record was run on the tree at **`d2c69fe`** (current `main`, post-Plan-030) with
`GOTOOLCHAIN=go1.25.13` on darwin/arm64. The settlement-switch trace, the `RunInTx` three-branch read, the sqlite
`BEGIN IMMEDIATE` shape, the class gate's 17/27 output, the ten count sites, the seven `AddMember` sites, the
four-producer counts, the `go.mod` inventory and the docs-link baseline are all first-hand output, not transcription.
Where a bundle figure was measured at `2b2dec1`, the finding says so and gives both numbers. **No file in the
repository was modified.**

---

**VERDICT: NOT SAFE TO IMPLEMENT.**

*The design argument survives; the two places where it meets shipped runtime behavior do not. B-1 turns the remedy
into a production-down hot spin under the shipped defaults, and M-6 trades an unbounded group for a permanently
deadlocked one. Neither is visible to a single acceptance criterion as written.*
