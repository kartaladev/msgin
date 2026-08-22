# Plan 029 — adversarial design audit, round 2 (2026-08-21)

Independent Opus subagent, handed the complete **revision-2** bundle (Spec 016 rev 2 + ADR 0032 rev 2 + Plan 029
rev 2) **before any implementation code exists**, per CLAUDE.md's design-time gate. Round 1
([`029-audit-round-1.md`](029-audit-round-1.md)) returned NOT SAFE TO IMPLEMENT with 2 BLOCKERs / 7 MAJORs /
10 MINORs; this record attacks the revision that folded them.

This record is **evidence-primary** — the audit artifact, not a user-facing summary. Every structural claim below
was re-derived on this tree at `48bbe83` with `GOTOOLCHAIN=go1.25.13`, darwin/arm64, from a throwaway module
outside the repository (`replace github.com/kartaladev/msgin => …`), deleted afterwards. No repository file was
modified except this report.

**Verdict: NOT SAFE TO IMPLEMENT.** 3 BLOCKERs, 6 MAJORs, 8 MINORs. **10 of the 19 round-1 findings did not land
cleanly** (8 LANDED-BUT-FLAWED, 2 REGRESSED).

The revision is a real improvement — the census widening (4 → 7) is correct, `WithMaxConnections` is now bounded,
the D-X wrap exists, `/simplify` and the multi-instance paragraph are in, and the AC-4 rewrite is genuinely
executable. The failures cluster in one place and it is the project's named scar: **three round-1 fixes repaired
the instance the finding named and left the same defect reachable through another door** — `m-9`'s code-shape
mismatch (fixed for `WithConcurrency`, still wrong for `WithMaxGroups` and `WithCapacity`), `M-5`'s
class-coverage hole (widened from "first parameter" to "any position", still blind to exported *methods*), and
`m-1`'s row/key arithmetic (credit-gate row removed, `burst` row added, count still printed as 16). On top of
that, `M-3`'s correction replaced one wrong verdict about `WithConcurrency` with a different wrong one: measured
end-to-end, **`Consumer.Run` panics**.

---

## BLOCKER-1 — AC-5's key set is **17**, the bundle says **16** in four places, and the function/method boundary is unstated (17 vs 44 keys)

**The claim.** Spec §6 AC-5.2: *"**All 16 keys** are constructible today, so every row is executable."*
ADR D-AA: *"All 16 keys are constructible, so all 16 are executable — which also gives the vacuity probe 16
plantable rows."* Plan Task 6: *"all 16 keys executable"*. Half 1 fails *"if that set differs from the
conformance table's key set in **either direction**"*.

**The evidence.** The set half 1 collects is not 16.

```
$ go run ./scan.go /Users/zakyalvan/Documents/RND/msgin      # go/ast, all 8 modules, non-test files
=== EXPORTED FUNCTIONS (non-method) with int/int64 param in ANY position: 17
  NewTokenBucket        burst  [resilience/ratelimit.go:42]
  WithBreakerThreshold  n      [resilience/breaker.go:51]
  WithBuffer            n      [adapter/memory/memory.go:38]
  WithCapacity          n      [adapter/memory/queuestore.go:58]
  WithCompletionSize    n      [routing/aggregator.go:132]
  WithConcurrency       n      [endpoint/consumer.go:53]
  WithConnectionBuffer  n      [adapter/http/options.go:883]
  WithMaxBodyBytes      n      [adapter/http/options.go:426]
  WithMaxConnections    n      [adapter/http/options.go:865]
  WithMaxEventBytes     n      [adapter/http/options.go:819]
  WithMaxGroups         n      [adapter/memory/groupstore.go:59]
  WithMaxInFlight       n      [endpoint/flowcontrol.go:39]
  WithMaxPayloadBytes   n      [endpoint/flowcontrol.go:130]
  WithMaxResponseBytes  n      [adapter/http/options.go:730]
  WithPollMaxBatch      n      [endpoint/flowcontrol.go:152]
  WithReplayBuffer      n      [adapter/http/options.go:926]
  WithSuccessStatus     code   [adapter/http/options.go:566]
=== EXPORTED METHODS with int/int64 param: 27
```

16 options **+** `NewTokenBucket` = **17**. Spec §2.1's own table already carries 17 rows (it says `7 + 9 = 16`
under a table with a 17th `+ NewTokenBucket's burst` row) and §2's note says *"`burst` carries a conformance
row"* — so the documents already know the answer is 17 and print 16 anyway.

**And the harder half: are methods in or out?** Neither Spec AC-5, ADR D-AA nor Plan Task 6 says. "Every exported
function with an `int`/`int64` parameter in any position" reads either way against `go/ast`, where a method is a
`*ast.FuncDecl` with a non-nil `Recv`. The two readings differ by 27 keys:

| Reading | Keys | Consequence |
|---|---|---|
| `Recv == nil` only | **17** | Maintainable — and **excludes three genuine members of the class** (below). |
| any `FuncDecl` | **44** | 27 method rows, of which 16 are on **unexported** receivers in leaf modules (`mysqlDialect.Claim`, `postgresGroupDialect.AddMember`, …) that a **root-module blackbox test cannot construct** ⇒ half 2 unsatisfiable ⇒ **verbatim repeat of round-1 BLOCKER-1**. |

The exclusion is not harmless. Three of the 27 are exactly the class §3 names — *"reachability of an
allocation"* — and one of them **already had this bug and was already fixed by a prior audit**:

```
$ sed -n '160,173p' adapter/memory/queuestore.go
// Claim leases up to max visible ready entries in FIFO order. A non-positive max
// yields no deliveries (guards make(...) against a negative cap panic — Claim is
// exported and directly callable; audit M-1).
func (s *QueueStore) Claim(_ context.Context, max int) ([]msgin.Delivery, error) {
	if max <= 0 { return nil, nil }
	…
	out := make([]msgin.Delivery, 0, min(max, len(s.ready)))
```

`channel.QueueChannel.Poll(ctx, max int)` (`queuechannel.go:50`) forwards straight into it; `sql.QueueStore.Claim`
(`queuestore.go:71`) is the same shape. A gate that omits them is the **M-5 defect returning through the next
door**: M-5 said "a positional sizing parameter is invisible"; the fix widened *position* and left *declaration
kind* untouched.

**Why it matters.** Half 1 is specified to fail in either direction. As written it fails on its very first run
(17 ≠ 16), which is the failure mode m-1 flagged in round 1 and which guarantees the implementer either edits the
gate to match or edits the count under time pressure. And the unstated method boundary is a coin-flip between
"maintainable" and "unimplementable".

**Recommended fix.**
1. Replace **16 → 17** at ADR 0032:178 (three occurrences in one sentence), Plan 029:258, Spec 016:407, and make
   §2.1 print `7 defective + 9 safe + 1 positional = 17 conformance keys` explicitly.
2. **State the method boundary in D-AA, and choose "functions only"** — 17 keys, all constructible from root.
3. Because that choice excludes three real members, add a **named, evidenced limitation** to D-AA listing
   `memory.QueueStore.Claim`, `sql.QueueStore.Claim` and `channel.QueueChannel.Poll`, and cite the `audit M-1`
   comment as proof the class reaches methods. Better: add those three to the conformance table as manual rows
   (they are all constructible from a root test) so the exclusion is only about *discovery*, not *coverage*.

---

## BLOCKER-2 — `endpoint.WithConcurrency` **panics inside `Consumer.Run`**; three documents say it does not

**The claim.** Spec §1's table: `endpoint.WithConcurrency(n)` → *"Observed: **corrupts the `WaitGroup`** — see
§1.2."* Spec §1.2: *"So **above `2^31`** the defect is a **silently corrupted `WaitGroup`** … **plus a latent
panic on the first `Done()`**."* ADR Context: *"**Three panic**, one corrupts a `WaitGroup`, and three leave a
bounded structure unbounded."* Task 0 is instructed to record the boundary as *"`Add(2^31)`+`Done()` →
`sync: negative WaitGroup counter`"*.

**The evidence.** End-to-end through the shipped public API, with a deferred `recover`:

```
$ go test -run TestConcurrencyRunPanics -v .
WithConcurrency(2147483648): NewConsumer err=<nil>
WithConcurrency(2147483648): Run PANICKED: sync: negative WaitGroup counter
```

`sync.WaitGroup.Add` panics **in `Add` itself**, not on a later `Done()`, and the boundary is not "above `2^31`":

```
$ go test -run TestWGBand -v .
n=2147483648          Add PANICS: sync: negative WaitGroup counter
n=2147483649          Add PANICS: sync: negative WaitGroup counter
n=4294967295          Add PANICS: sync: negative WaitGroup counter
n=4294967296          Add ok (int32(n)=0)          ← above 2^31, does NOT panic
n=4294967301          Add ok (int32(n)=5)
n=3221225472          Add PANICS: sync: negative WaitGroup counter
n=1099511627776       Add ok (int32(n)=0)
n=4611686018427387904 Add ok (int32(n)=0)
```

The real predicate is **`int32(n) < 0`** — the counter lives in the high 32 bits of the state word, so `Add`
panics for every `n` whose low 32 bits land in `[2^31, 2^32)`. That is **half of all `n > 2^31`**, and
`consumer.go:458` (`wg.Add(c.workers)`, with `workers: cfg.concurrency` at `consumer.go:330`) is on the primary
execution path, reached before a single worker is spawned.

Separately confirmed: for the *other* half (`int32(n) >= 0`, e.g. `1<<40`) `Add` returns cleanly, `Wait()` returns
immediately, and the spawn loop `for i := 0; i < c.workers; i++` runs `2^40` times — my probe at `1<<40` had to be
killed at the 2-minute timeout. So **both** failure modes are real; the bundle describes only the second.

**Why it matters.** This is round-1 BLOCKER-2's shape repeating one revision later: a census row asserted as
complete that is not. `WithConcurrency` is a **fourth panic on caller input**, in `Run`, which is precisely the
severity class Spec §1 uses to justify the increment ("two of the three panics are not at construction" — it is
three of four). The ceiling (`1<<16`) fixes it either way, so the **decision stands**; what is wrong is the
evidence Task 0 is told to record and the rationale headed for the public godoc. CLAUDE.md's stored lesson
*"Docs can contradict the code they describe"* applies verbatim.

**Recommended fix.**
- Spec §1 table: `Observed` for `WithConcurrency` becomes **`Run PANICKED: sync: negative WaitGroup counter`
  (n = 1<<31)**, with §1.2 keeping the wrap/silent-corruption case as the *second* mode.
- Spec §1.2: restate the boundary as **`int32(n) < 0`**, not "above `2^31`", with the band table above; note
  `1<<32` as the counter-example that kills the "above 2^31" formulation.
- ADR Context: *"Three panic"* → **"Four panic"**, and drop "one corrupts a `WaitGroup`" from the disjunction —
  `WithConcurrency` does both.
- Task 0: assert **`NewConsumer(WithConcurrency(1<<31))` + `Run` panics** (cheap, deterministic, one line) and the
  `int32` band; keep the "do NOT assert it is *not* a panic" instruction, which is now doubly right.

---

## BLOCKER-3 — the branch-coverage table requires "the hazardous path runs at the ceiling" for **all 7**; it is not executable for `WithMaxConnections`, and Task 2 silently contradicts it

**The claim.** Plan §"Branch coverage": `| n <= ceiling | ceiling exactly → accepted **and the hazardous path
runs** | all 7 |`. Spec AC-1 says the same for the six R1 knobs: *"a second case constructs with a valid size and
confirms the hazardous path still runs. Asserting only the error passes against a constructor that rejects
everything."*

**The evidence.** For `msghttp.WithMaxConnections` the hazardous path is the admission check
`sse_server.go:182 if len(s.conns) >= s.cfg.maxConns()`. Running it at `maxConnectionsCeiling = 1<<16` requires
**65,536 concurrently registered SSE connections**, each holding a live `ServeHTTP` goroutine, a
`make(chan []byte, connBuffer)` and a request/response pair. Measured cost of the goroutines alone on this tree:

```
$ go test -race -run TestStack -v .
goroutines=100000 StackSys delta=410353664 => 4103.5 B/goroutine
1<<16 goroutines => 268894208 B = 256.4 MiB
```

…before any connection state, under a package whose root sibling runs `goleak.VerifyTestMain` (`main_test.go:14`)
and whose `ServeHTTP` **blocks until the request context is done** (round-1 **m-2**, still true). Task 2's own
bullets ask for no such thing — they ask only for *"a `ServeHTTP` case at the accepted **connBuffer** ceiling"*.
So the plan's coverage table and its task text disagree about what Task 2 must deliver.

The same row is expensive-but-possible for the other two growth levers, and nobody has costed it:

```
$ go test -race -run TestGroupGrowth -v .        # WithMaxGroups(1<<20), fill past the cap
shed at i=1048576 err=msgin: message dropped by overflow policy
groups=1048576 elapsed=2.042188417s totalDelta=1043025752 (994.7 MiB) heapAlloc=932667920 (889.5 MiB)

$ go test -race -run TestEnqueuePastCap -v .     # WithCapacity(1<<20), fill past the cap
shed at i=1048576 err=msgin: message dropped by overflow policy
enqueued=1048576 elapsed=489.947166ms heapDelta=113451016 bytes (108.2 MiB)
```

**Why it matters.** This is BLOCKER-1 of round 1 in a new place: *"an acceptance criterion whose text cannot be
turned into a running test, discovered only during implementation"*. Plan constraint 3 then forces a mid-plan
stop-and-escalate. And the ~1 GB `WithMaxGroups` case, if written literally, lands in a `-race` suite that already
allocates 48 MiB (Task 1) and 108 MiB (Task 3) in sibling tests.

**Recommended fix.** Split the row into what each knob's hazard actually needs:
- **Allocating knobs** (`WithMaxInFlight`, `WithConnectionBuffer`, `WithBuffer`) — run the hazardous path at the
  ceiling; the allocation *is* the hazard and it is cheap enough (48 / 1.5 / 24 MiB, all measured).
- **Growth knobs** (`WithCapacity`, `WithMaxGroups`, `WithMaxConnections`) — the hazard is *"the cap stops
  capping"*, which is a **property of the comparison, not of the ceiling value**. Prove it at small `n`, where the
  repo already proves it (`memory.WithMaxGroups(1)` → `ErrOverflowDropped`, `groupstore_test.go:30-39`), and at
  the ceiling assert only **construction + acceptance**. State this explicitly so the reader does not read "all 7"
  and go looking for 65,536 connections.
- **`WithConcurrency`** — the hazardous path at `1<<16` is 65,536 goroutines (~256 MiB under `-race`, measured);
  keep it but state the figure, as Task 1 already does.

---

## MAJOR findings

| # | Finding | Evidence | Recommended fix |
|---|---|---|---|
| **M2-1** | **The merged R1 condition renders a false message on the *lower* arm.** Plan §"The three code shapes" R1-a/R1-b fold both bounds into one condition returning `fmt.Errorf("%w: %d exceeds %d", …)`. `WithMaxInFlight(0)` then reads **`msgin: max in-flight must be >= 1: 0 exceeds 1048576`** — the debuggability payload M-6 was fixed to add now *lies* about which bound was violated, on a branch that shipped correct. It also **silently changes an existing branch's error text**, which Spec §3.1 (*"each existing lower-bound check **gains an upper arm**"*) does not acknowledge. Separately, R1's `"%d exceeds %d"` and R2's `"%d not in [0, %d]"` say the same thing two ways in one increment. | Plan lines 70-81 vs 85-96. Both arms are reachable: `endpoint.WithMaxInFlight(0)` and `(1<<20 + 1)` hit the same `return`. | Adopt **R2's shape everywhere**: `fmt.Errorf("%w: %s: %d not in [%d, %d]", sentinel, "endpoint.WithMaxInFlight", n, 1, maxInFlightCeiling)`. One shape, true at both ends, names the site (which M-1's three-producer resolution needs anyway). Add an AC-2b case per knob at the **lower** end asserting the rendered range, not only at the upper end. |
| **M2-2** | **The R1-a/R1-b assignment is wrong for two of the six knobs — `m-9` through another door.** Plan: *"R1-a … (`WithMaxInFlight`, `WithConnectionBuffer`, `WithCapacity`, `WithMaxGroups`, `WithMaxConnections`)"* and *"R1-b — … **`WithConcurrency` only**"*. Measured, only three of the five are R1-a. | `WithMaxGroups` is **R1-b**: no `set` flag, default in the literal (`groupstore.go:88 cfg := groupStoreConfig{…, maxGroups: 1024}`), single arm at `:91`. `WithCapacity` is a **third shape** the plan does not name — a *nested* `if`, not an `else if`: `queuestore.go:97-103` `capacity := defaultCapacity; if cfg.capacitySet { if cfg.capacity <= 0 { return … }; capacity = cfg.capacity }`. Confirmed sites: `queuestore.go:99`, `groupstore.go:91`, `consumer.go:262`, `consumer.go:272`, `options.go:1158`, `options.go:1164`. | Add **R1-c** (nested, `capacitySet`-gated) for `WithCapacity`, move `WithMaxGroups` to R1-b, and paste the three-line context of each of the six sites into the plan. m-9's fix showed one shape and stopped; the class is "each site has its own shape — read it". |
| **M2-3** | **AC-4's in-situ half rests on a premise that is false for its `endpoint` arm, and `credit.go:21` still has no mutant.** Spec AC-4: *"`1<<20` of a zero-byte element is **nothing**, of a 48-byte element is 48 MiB, so the delta is what distinguishes them."* True for the `QueueStore`. **False for the consumer**, whose baseline at `maxInFlightCeiling` is *already* 48 MiB from `workerCh` (`consumer.go:384`), so a `credit.go:21` mutation moves the delta 48 → 96 MiB, not 0 → 48. And Task 5's mutant bullet names **only** `queuestore.go:108` — so the site round-1 BLOCKER-1 was entirely about ships **unmutated** for the second revision running. | Measured, 5 runs, `WithMaxInFlight(1<<20)` + `Run`: `totalDelta=50367560 / 50334648 / 50334344 / 50336344 / 50334376` plain; `50367480 / 50334040 / 50334984 / 50333656 / 50333576` under `-race` — 48.0 MiB, spread < 0.07 %. `make(chan struct{}, 1<<20)` inside `NewQueueStore`: `288` bytes, 8/8 runs (one `304` under `-race`); no-op noise floor `0`. | Restate AC-4's in-situ arm per site: **`QueueStore` — assert `delta < 1 MiB`** (288 B measured vs 50,331,648 B mutant: 3,600× headroom above noise, 48× below the mutant). **Consumer — assert `40 MiB < delta < 64 MiB`**; the *lower* bound is mandatory, because without it a measurement taken before `Run` allocates passes vacuously. Add the missing mutant: `credit.go:21 chan struct{}` → `chan managedDelivery` must break the consumer arm. |
| **M2-4** | **The `time.Duration` exclusion is justified by a claim that is FALSE on this project's reference platform.** Spec §3.7.4 and ADR D-AA both state as fact: *"`NewTokenBucket`'s `time.Duration(float64(time.Second) / rps)` **overflows negative** for a sub-normal `rps`."* Round 1 filed it as *suspected*; revision 2 promoted it to an asserted fact in two documents **without running it**. | `$ go test -run TestTBInterval -v .` → `rps=1e-300 float=+Inf Duration=2562047h47m16.854775807s (int64=9223372036854775807)`; same for `1e-310` and `5e-324`. The value saturates to **+MaxInt64**, not negative (float→int conversion out of range is implementation-defined; arm64 saturates, amd64 would give `-MaxInt64-1` — so the claim is *platform-specific* and wrong on the platform every other figure in this bundle was measured on). Also `ratelimit.go:45` already rejects `rps <= 0` with `ErrInvalidRateLimit`, and `b.interval` never reaches a ticker — `:81` and `:101` use it arithmetically, so the real failure mode is a `Wait` that blocks ~292 years, not an overflow. | Either delete the sentence or replace it with the measured one: *"a sub-normal `rps` saturates `interval` to `MaxInt64` on darwin/arm64 (`-MaxInt64-1` on amd64), making `Wait` block indefinitely rather than rate-limiting."* And **evidence the exclusion positively** rather than by AST mechanism: all five `NewTicker`/`NewTimer` sites in the workspace validate or guard their duration (`sse_server.go:262` ← `heartbeat > 0`; `attempts.go:77` ← `attemptTTL > 0`; `exchange.go:449` ← `options.go`-style `timeoutSet && <= 0` check at `exchange.go:317`; `aggregator.go:505-509` guards `interval <= 0` explicitly; `producer.go:505`). That turns "outside the gate by construction" into "outside the gate **and** currently safe, checked". |
| **M2-5** | **`m-8`'s "state the cost" fix was applied to Task 1 only; Tasks 3 and 5 are heavier and say nothing.** Task 1 dutifully records 48 MiB / ~320 MB RSS. Task 3's *"enqueue past the nominal cap and assert the shed/evict policy fires"* at the ceiling costs **~1 GB** for `WithMaxGroups`. Task 5's in-situ half adds another 48 MiB. Nothing in the plan says so, and `go test ./...` runs those packages **concurrently**. **Worse: Task 3's growth exercise HANGS if written the obvious way** — `QueueStore`'s default overflow policy is `OverflowBlock`, so enqueue #1,048,577 blocks on `s.sem <- struct{}{}` until the test binary's 10-minute panic. | `WithMaxGroups(1<<20)` growth: `totalDelta=1043025752 (994.7 MiB) heapAlloc=932667920 (889.5 MiB)`, 2.04 s under `-race`. `WithCapacity(1<<20)` growth: 108.2 MiB, 0.49 s under `-race` — only reachable because my probe passed `memory.WithOverflow(msgin.OverflowReject)`. `queuestore.go:124-129` is the blocking arm. | Name `WithOverflow(OverflowReject)` in Task 3's bullet exactly as Task 2 names `serveInBackground` (this is the same class as m-2). Record the measured cost of every heavy case in the task that owns it, not only Task 1. Adopt BLOCKER-3's fix and the ~1 GB case disappears entirely. |
| **M2-6** | **AC-5 half 2's "product is usable" is unspecified work the plan does not size, and two of its keys are not one-liners from root.** All 17 keys are constructible at `1<<62` (probe below), so M-4's fix is directionally right — but *usable* is where the cost is. Six keys are `msghttp` knobs whose product is an `SSEServer`; exercising one from a **root** blackbox test means re-implementing `serveInBackground` (unexported, `adapter/http/sse_server_test.go:176`, not importable) and cleaning up every goroutine under root's `goleak.VerifyTestMain` (`main_test.go:14`). `WithCompletionSize` needs a `MessageGroupStore` **and** a release func before `NewAggregator` returns non-nil. | `$ go test -run TestAllKeys -v .` — 15 of 17 return `err=<nil>` at `1<<62`; `WithSuccessStatus` → `msghttp: status code must be in [100,599]`; `WithCompletionSize` → `msgin: aggregator output channel is nil` (a **fixture** gap, not a rejection — the option itself is accepted). | Add a Task 6 bullet naming both: a root-local `serveInBackground` equivalent with explicit teardown, and the `NewAggregator(store, release, …)` fixture. State that `WithCompletionSize`'s row asserts *acceptance*, not the bare-option error, so an implementer does not file it in the "rejects" arm alongside `WithSuccessStatus`. |

---

## MINOR findings

| # | Finding | Evidence | Recommended fix |
|---|---|---|---|
| **m2-1** | **A `file:line` citation is off by one.** Spec §1.3 and §2.1 both cite `queuestore.go:131` for `s.ready = append(s.ready, …)`. | `131: s.mu.Lock()` / `132: s.ready = append(s.ready, entry{…})`. All 20 other citations in the bundle were checked and are correct (`queuestore.go:108`, `groupstore.go:94`/`:108`, `sse_server.go:182`/`:201`, `credit.go:21`, `consumer.go:384`/`:385`/`:457-459`, `poller.go:36`, `aggregator.go:134`, `ratelimit.go:42`/`:48-49`, `memory.go:43`/`:58`, `sse_server_test.go:176`, and the six §3.1 validation sites). | `:131` → `:132`. |
| **m2-2** | **`concurrencyCeiling`'s rationale mixes MB and MiB inside one range**, and it is headed for the public godoc (Spec §4). *"~134–257 MiB"*. | Re-measured: `2051.3 B/goroutine` plain, `4103.5 B/goroutine` under `-race`. `1<<16 ×2051 = 134,414,336 B` = **128.2 MiB** (= 134.4 **MB**); `1<<16 ×4103 = 268,894,208 B` = **256.4 MiB**. So "134" is MB and "257" is MiB. | *"~**128–256 MiB** of stack (2,051 B/goroutine, 4,103 B under `-race`, measured)"*. M-2 fixed the 4× error and left a unit error behind it. |
| **m2-3** | **`memory.WithBuffer` is the only D-Z row with no measured cost** — *"Queued messages."* Every sibling row states one, and Spec §3.4 explicitly frames the ceilings as "sized far below the unrecoverable band", which is a claim about bytes. | `memory.New(WithBuffer(1<<20))` → `totalDelta=25166072` = **24.0 MiB** (`sizeof(msgin.Message[any]) = 24`, measured). | Add the figure. It is the cheapest of the three allocating ceilings and makes the D-Z table uniform. |
| **m2-4** | **Sentinel-message treatment is now inconsistent.** Task 3 genericises `msgin.ErrInvalidCapacity` to *"capacity out of range"* because it has three producers — correct — but `ErrInvalidMaxInFlight` (*"must be >= 1"*), `ErrInvalidConcurrency` (*"must be >= 1"*), `ErrInvalidConnectionBuffer` and `ErrInvalidMaxConnections` keep lower-bound-only text under a range that is now two-ended. Rendered: `"msgin: max in-flight must be >= 1: 1048577 exceeds 1048576"`. | `errors.go:51`, `:144`, `:258`; Task 1 widens only the **godoc**, Task 3 widens the **message**. | Genericise all six messages in the same increment (`"msgin: max in-flight out of range"`, …). Verified test-safe — see Verified-negative; nothing asserts these strings. |
| **m2-5** | **Task 3 edits the root module but commits as `fix(memory)`.** `msgin.ErrInvalidCapacity`'s message lives in root's `errors.go`, so Task 3 touches two modules while Global constraint 7 says *"every module it touched"*. | Plan Task 3 bullet 3; `errors.go:258`. | Say so in the task (`root + adapter/memory`), or split the sentinel-message edit into Task 4 where `WithBuffer` becomes the third producer. |
| **m2-6** | **Task 4's *"confirm the comment still holds verbatim, or widen it"* has a determinate answer the plan should just state: widen.** The comment scopes the latch to *"the **first nil ELEMENT of opts**"*; after Task 4 the same field also latches a sizing fault. | `memory.go:16-23`. Note the escape hazard itself is **already** covered verbatim (*"a hostile option could stash `b` and read `err` concurrently with `New`'s write"*), and `WithBuffer` already writes `b.ch` on a live `*Broker` today — so Task 4 escalates the consequence (a `Permanent` error from `Send`/`Stream`) but creates no new race class. Round-1's carried-forward item is therefore **answered, not open**. | Change the bullet to *"widen `memory.go:16-23` to name both latch sources; the `Option`-escape sentence already covers the second write site and needs no change."* |
| **m2-7** | **`WithBuffer`'s existing godoc contradicts §3.6 and no task bullet retires it.** *"A negative `n` is **clamped to 0** rather than panicking, honoring the library's no-panic-on-caller-input contract."* §3.6 folds the negative end into `ErrInvalidCapacity`; that sentence becomes false the moment Task 4 lands. | `adapter/memory/memory.go:34-37`. This is exactly the class CLAUDE.md's stored lesson names (*"all three fix rounds in Plan 028 were godoc, not logic"*). | Add an explicit Task 4 bullet: delete/replace that sentence, and add an assertion that `WithBuffer(-1)` produces `ErrInvalidCapacity` (AC-3b already does — cross-reference it so the godoc edit is not orphaned). |
| **m2-8** | **AC-4's delta measurement needs three stated conditions or it silently rots**: not `t.Parallel()`, `runtime.KeepAlive` on the product (else the ceiling channel is collectible before `ReadMemStats`), and `TotalAlloc` rather than `HeapAlloc` (cumulative, GC-independent). | Measured with all three: `QueueStore` delta `288` B on 8/8 runs plain and 7/8 under `-race`; noise floor `0` B. Without `KeepAlive` the measurement is not reliable across `-shuffle=on`. | Put the three conditions in Task 5's bullet. The measurement is **not** flaky when written this way — see Verified-negative. |

---

## Verified-negative (attacked, found sound) — what revision 2 got right

**The census command still yields exactly 16 options, unchanged.**

```
$ git ls-files '*.go' | grep -v _test | xargs grep -hnE \
    '^func With[A-Za-z]+(\[[^]]*\])?\([a-z]+ (int|int64)\)' | wc -l
      16
```

**M-4's central claim holds: every key is constructible at `1<<62` today.** Probed behaviorally, not read:

```
WithMaxInFlight err=<nil>      WithConcurrency err=<nil>       WithMaxPayloadBytes err=<nil>
WithPollMaxBatch err=<nil>     WithCapacity err=<nil>          WithMaxGroups err=<nil>
WithMaxConnections err=<nil>   WithConnectionBuffer err=<nil>  WithReplayBuffer err=<nil>
WithMaxBodyBytes err=<nil>     WithMaxResponseBytes err=<nil>  WithMaxEventBytes err=<nil>
WithBreakerThreshold err=<nil> NewTokenBucket.burst err=<nil>
WithSuccessStatus err=msghttp: status code must be in [100,599]        ← the "rejects" arm, as §2.1 says
```

**AC-4's allocation delta is *not* the flaky heap measurement it looks like** — this was attacked specifically and
it survived. Eight repeats plain, eight under `-race`, with a no-op noise floor of exactly `0` bytes; the
`QueueStore` figure is `288` B against a `50,331,648` B mutant. See M2-3 for the assertion shape that makes it
non-vacuous.

**Filling past a `1<<20` cap is *fast*, contrary to the pre-audit hypothesis.** `489.947166 ms` under `-race` for
1,048,577 enqueues. The cost of Task 3's accepting case is **memory, not time** (108 MiB / 995 MiB — see M2-5).

**The D-X wrap and the message widening are test-safe.** No test asserts an exact message, and every assertion on
the six sentinels goes through `errors.Is`/`require.ErrorIs`:

```
$ grep -rn --include='*_test.go' -E 'ErrInvalidCapacity|ErrInvalidMaxInFlight|ErrInvalidConcurrency|ErrInvalidConnectionBuffer|ErrInvalidMaxConnections' . | grep -vE 'errors\.Is|ErrorIs'
errors_test.go:16:		msgin.ErrInvalidConcurrency, msgin.ErrUnsupportedSource,       ← a slice literal, then ErrorIs
adapter/memory/queuestore_test.go:23:			name: "explicit non-positive capacity is ErrInvalidCapacity",   ← a subtest name
adapter/memory/groupstore_test.go:22:			name: "non-positive WithMaxGroups is ErrInvalidCapacity",       ← a subtest name
```

**The R2 `Permanent` wrap survives the `fmt.Errorf` layer.** `IsPermanent` uses `errors.As(err, &pe)`
(`reliability.go:91-94`), so `fmt.Errorf("%w: …", msgin.Permanent(msgin.ErrInvalidCapacity), …)` still reports
`true`, and `ErrInvalidCapacity` is still an `errors.Is` target. Constraint 4's asymmetry is implementable.

**`WithPollMaxBatch`'s "safe" verdict holds, and holds *because* Task 1 lands.** `poller.go:35-36` tops up only
from **free credits** (`gate.tryAcquire()`), so `held <= maxInFlight` regardless of `pollMaxBatch`; and
`memory.QueueStore.Claim` caps its own `make` at `min(max, len(s.ready))`. Worth stating in §2.1: this row's
safety is *derived from* `maxInFlightCeiling`, not independent of it.

**`WithReplayBuffer` allocates no ring upfront** — `appendRing` (`sse_server.go:469-476`) is `append` + `copy`
only; there is no `make([]ringEntry, 0, n)` anywhere in the file. §2.1's "bounded retention" verdict is correct.

**Every ceiling multiplier and byte figure in D-Z checks out.** `defaultMaxInFlight = 1024` → `1<<20` is 1024×;
`defaultConnectionBuffer = 16` → `1<<16` is 4096×; `defaultMaxConnections = 1024` → `1<<16` is 64×;
`defaultCapacity = 1024` and `maxGroups: 1024` → `1<<20` is 1024×. `sizeof(msgin.Delivery) = 40` + one `func()` =
48 → `1<<20 × 48 = 50,331,648` ✔; `sizeof([]byte) = 24` → `1<<16 × 24 = 1,572,864` ✔.

**`ErrInvalidCapacity` has exactly two producers today, three after.** `queuestore.go:100`, `groupstore.go:92`
(+`errors.go:258` decl). M-1 landed correctly.

**Housekeeping is clean and Task 7's numbers are right.** Docs-link gate **arms 1 and 2 report nothing** over the
three bundle files plus the two modified ones. `ls docs/specs/[0-9]*.md | wc -l` → **16**;
`ls docs/adrs/[0-9]*.md | wc -l` → **31**; plan distinct numbers → **29** — matching Task 7's stated targets
exactly. The tree is green: `GOWORK=off go test ./... -race` → 11/11 `ok`.

**The two working-tree edits are consistent with the bundle.** ADR 0031 gains a *"Known open edge, resolved later
by ADR 0032 D-Y"* block that does **not** touch D-U itself, and `docs/HANDOVER.md` §8.1 is struck through and
re-pointed at Spec 016/ADR 0032/Plan 029 with the 4 → 7 widening recorded. Both are traceability-correct.

---

## Round-1 fix verification

One row per round-1 finding. **LANDED** = fixed correctly and nothing else broke. **LANDED-BUT-FLAWED** = addressed
but the fix is wrong, incomplete, or reintroduces the class elsewhere. **REGRESSED** = the named instance is fixed
and the same defect is back through another door.

| R1 id | Round-1 finding (one line) | Verdict | Why |
|---|---|---|---|
| **B-1** | AC-4 unexecutable after Task 1 | **LANDED-BUT-FLAWED** | AC-4 rewritten into two halves that *are* executable (verified) — but the justification is false for the `endpoint` arm (baseline is already 48 MiB, not 0), the assertion direction is unspecified (an upper-bound-only assert is vacuous), and Task 5's mutant list still omits `credit.go:21`, the very site B-1 was about. → **M2-3** |
| **B-2** | Contract's 2nd clause unestablished; 3 "safe" knobs are OOM levers | **LANDED-BUT-FLAWED** | Census correctly moved 4 → 7 and option (b) (extend the ceilings) was taken. But the verification of the three new knobs is not executable as written: `all 7` accepting-at-ceiling is impossible for `WithMaxConnections` and ~1 GB for `WithMaxGroups`. → **BLOCKER-3**, **M2-5** |
| **M-1** | `ErrInvalidCapacity` has three producers | **LANDED** | Corrected in Spec §3.5 and ADR D-X; generic message adopted; Task 3 widens the godoc to name all three. Producer count re-verified. |
| **M-2** | `concurrencyCeiling` rationale 4× out | **LANDED-BUT-FLAWED** | The 4× error is fixed (2,051 B re-measured). A unit error replaced it: "134–257 MiB" mixes MB and MiB. → **m2-2** |
| **M-3** | `WithConcurrency`'s "no panic" verdict wrong | **LANDED-BUT-FLAWED** | Revision 2 replaced one wrong verdict with another. Measured: `Run` **panics** at `1<<31`; the boundary is `int32(n) < 0`, not "above `2^31`" (`1<<32` does not panic). ADR's "Three panic" is still wrong. → **BLOCKER-2** |
| **M-4** | Half 2 inert for 12 of 16 keys | **LANDED-BUT-FLAWED** | Every row is now behavioral and every key is constructible (verified). But the key count is 17 not 16, and "product is usable" is unsized work for 7 of the keys. → **BLOCKER-1**, **M2-6** |
| **M-5** | The gate doesn't gate the class the contract names | **LANDED-BUT-FLAWED** | Widened from "first parameter" to "any position" and `burst` gets a row — but only over `func`s. 27 exported **methods** carry `int` params, three of them genuine sizing parameters reaching a `make`, one with an `audit M-1` comment recording that exact bug. Same defect, next door. → **BLOCKER-1** |
| **M-6** | D-X's justification false as planned | **LANDED-BUT-FLAWED** | The wrap is in. But folding both bounds into one condition makes the *lower* arm render `"0 exceeds 1048576"`, and R1's shape disagrees with R2's. → **M2-1** |
| **M-7** | `connBuffer` rationale reasons about an unbounded product | **LANDED** | `WithMaxConnections` bounded at `1<<16`; both D-Z rows restated; the "we bound each factor, not the product" paragraph is explicit in Spec §3.4 and ADR D-Z. |
| **m-1** | 17 rows under a heading claiming 16 | **REGRESSED** | The credit-gate row was correctly demoted to prose — and a `+ NewTokenBucket's burst` row was added in its place. The table is 17 rows again, the AC-5 key set is 17, and four places still print 16. → **BLOCKER-1** |
| **m-2** | Naive `ServeHTTP` hangs | **LANDED** | Task 2 names `serveInBackground` with its file:line and the goleak note. (The same hazard now reappears in Task 6's root gate, where the helper is not importable — that is **M2-6**, a new site, not a regression of m-2.) |
| **m-3** | "Decouple `workerCh`" alternative omitted | **LANDED** | Added to Spec §5 with the deadlock-fix rejection rationale and the `consumer.go:375-384` citation. |
| **m-4** | No multi-instance statement | **LANDED** | Spec §7 closing paragraph: per-process constants, no cross-instance state, no SPI seam affected. Meets CLAUDE.md's mandatory-statement rule. |
| **m-5** | No `/simplify` step | **LANDED** | Task 7 bullet 1, before the reviews. |
| **m-6** | GOMEMLIMIT rejection asserted, not evidenced | **LANDED** | Measured line folded into Spec §1.1, ADR §Context and Spec §5's alternatives table. |
| **m-7** | Exporting the ceilings not considered | **LANDED** | Spec §5 row added and rejected on the record, with "revisit if a consumer asks". |
| **m-8** | Accepting-at-ceiling cases are heavy and nothing says so | **LANDED-BUT-FLAWED** | Task 1 states 48 MiB and ~320 MB RSS. Tasks 3 and 5 are heavier (108 MiB, ~995 MiB, +48 MiB) and say nothing — the fix was applied to the named instance only. → **M2-5** |
| **m-9** | The `maxInFlightSet` shape doesn't transfer to `WithConcurrency` | **REGRESSED** | R1-b was added for `WithConcurrency` — and `WithMaxGroups` (also `set`-flagless) is filed under R1-a, while `WithCapacity` has a *third*, nested shape the plan does not name. → **M2-2** |
| **m-10** | Three knobs share "queued messages"; one unbounded | **LANDED** | `WithCapacity` gets `1 << 20`; §3.4 and D-Z both say why the three agree. |

**Totals: LANDED 9 · LANDED-BUT-FLAWED 8 · NOT LANDED 0 · REGRESSED 2. Ten of nineteen did not land cleanly.**

---

## Carried into round 3 — suspected, not proven

- **`go test ./...` peak RSS across the whole increment.** Individually measured: Task 1 ≈ 48 MiB + 256 MiB
  (goroutines), Task 3 ≈ 108 MiB (+ ~995 MiB if BLOCKER-3's fix is not taken), Task 5 ≈ 48 MiB. Go runs packages
  in parallel, so the peak is a *sum*, not a max. Not measured end-to-end because the code does not exist yet;
  Task 7 should measure it and record the number rather than discover it in CI.
- **`WithConcurrency`'s second failure mode was not run to completion.** `NewConsumer(WithConcurrency(1<<40))` +
  `Run` was killed at a 2-minute timeout while the spawn loop ran; whether it OOMs, thrashes, or eventually
  returns is unknown. Not needed for the decision (the ceiling forecloses it) but Task 0 should state that it
  timed out rather than implying it was observed.

## Auditor's method note

Every command in this record was run by the auditor on the tree at `48bbe83` with `GOTOOLCHAIN=go1.25.13`,
`GOWORK=off`, darwin/arm64, from a throwaway module outside the repository. The `go/ast` scan, the 17-key
constructibility probe, the `WaitGroup` band, the end-to-end `Run` panic, the goroutine-stack measurement, the
allocation-delta repeats, the two growth fills, the sub-normal-`rps` conversion and the `sizeof` figures are all
first-hand output, not transcription. All probe files were deleted.

---

**VERDICT: NOT SAFE TO IMPLEMENT.**
