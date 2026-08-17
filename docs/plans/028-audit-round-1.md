# Plan 028 — adversarial design audit, round 1 (2026-08-14)

Independent Opus subagent, handed the complete round-1 bundle (Spec 015 rev 1 + ADR 0031 rev 1 + Plan 028 rev 1)
**before any implementation code existed**, per CLAUDE.md's design-time gate. This record is **evidence-primary** —
it is the audit artifact, not a user-facing summary.

**Verdict: DO NOT IMPLEMENT.** 2 BLOCKERs, 6 MAJORs, 6 MINORs. Both blockers were independently re-verified by the
coordinator before any edit; the commands and output are reproduced below.

---

## BLOCKER-1 — the census is 32, not 30; two exported constructors were invisible to the derivation

**Cause.** Revision 1's regenerating command used `\.\.\.[A-Za-z]*[Oo]ption`, which **cannot match a qualified type**
— the `.` in `msghttp.Option` breaks `[A-Za-z]*`.

```
$ grep -rn --include='*.go' '^func [A-Z].*\.\.\.[a-z][A-Za-z0-9]*\.[A-Za-z]*[Oo]ption' . | grep -v '_test.go'
adapter/http/stdlib/inbound.go:48:func NewInbound(target msgin.MessageChannel, opts ...msghttp.Option) (http.Handler, error) {
adapter/http/stdlib/inbound.go:96:func NewInboundGateway(exchange msgin.RequestReplyExchange, opts ...msghttp.Option) (http.Handler, error) {
```

Both are exported R1 **delegators** in the root module, forwarding to `msghttp.NewConfig`.

**Three compounding consequences:**

1. Both would have shipped **still panicking** — the spec's headline contract false on delivery.
2. Without a pre-check they report `msghttp.NewConfig: nil option at index 0` — *verbatim the failure mode D-R was
   invented to prevent.*
3. Revision 1 Task 7's pre-authorized fallback ("assert the constructor count is still **30**") would have **locked
   the false census into a passing test** — the project's "verify structural claims against code" scar, reproduced.

**Folded as:** census 30→32, delegators 6→8, R1 22→25 (incl. §3.6), helper packages 7→8, a corrected regeneration
command with both anchors explained, and Plan 028 Task 6b for `stdlib`.

**Corrected command, verified → 32:**
```
$ { grep -rn --include='*.go' '^func [A-Z].*\.\.\.\([a-z][A-Za-z0-9]*\.\)\?[A-Za-z]*[Oo]ption' . | grep -v '_test.go'
    grep -n '^func NewAggregator' routing/aggregator.go; } | sed 's/:func.*//' | sort -u | wc -l
      32
```
The `^func [A-Z]` anchor is load-bearing: it excludes the four variadic **func literals** in
`adapter/database/sql/harness/{lock,queuestore,groupstore,inbox}.go`, which are test-kit locals, not constructors
(`grep '^func [A-Z].*\.\.\..*[Oo]ption' adapter/database/sql/harness/ | wc -l` → **0**).

---

## BLOCKER-2 — the silent-skip mechanism contradicts, verbatim, the precedent the bundle cites for itself

Revision 1 §1.1 cited `handler.go:52-63` (the position format) as the precedent its R3 family follows. It did not
read **lines 44-51 of the same function**, verified verbatim:

```
$ sed -n '44,51p' handler.go
// A nil ELEMENT is not a panic (no panic on caller input) and is NOT skipped: it
// is replaced in place by a step whose handler fails with Permanent(ErrNilFunc)
// naming its index, so the flow degrades exactly as To(nil) or Activate(nil)
// does. Skipping was rejected — a step built conditionally
// (steps = append(steps, maybeStep())) that came back nil is a WIRING BUG, and
// deleting it would silently change what the flow does: a dropped Filter stops
// filtering, a dropped To discards the message. Steps BEFORE the nil still run;
// the chain stops at it.
```

The conditional-construction example is the **same shape** as the spec's own motivating example. Revision 1's
argument — *"nothing in the design distinguishes them"* — therefore cuts **against** the skip, not for it.

**And the consequences are not cosmetic.** Per-constructor enumeration of what a dropped option costs:

| Constructor | Dropped option | Silent consequence |
|---|---|---|
| `msgin.New` | `WithID(id)` | A **fresh random id** is stamped (`message.go:165-168`: `if cfg.id == "" { cfg.id = NewID() }`). `HeaderMessageID` is the SQL inbox dedup key — a caller's idempotency key silently becomes random |
| `msgin.New` | `WithHeaders(m)` | Correlation headers vanish |
| `channel.NewPublishSubscribeChannel`, `.NewPubSub` | `WithSingleSubscriber()` | The `ExclusiveSubscribable` contract **voids**; a second subscriber that must be rejected with `ErrChannelSubscribed` is accepted → every message processed twice |
| `sqlite.DSN` | `WithJournalMode`, `WithBusyTimeout` | Different durability/concurrency behavior under load |
| `resilience.NewCircuitBreaker` | threshold/cooldown | Silently reverts to defaults |
| `memory.New`, `routing.NewRouter` | buffer / `WithDefaultChannel` | Sync handoff; `ErrNoRoute` per message (this one at least fails loud) |

Revision 1 §3.3 claimed *"the nil is inert, not fatal"* — false for at least three of the seven. §5 rejected
"skip everywhere" because *"a mis-wired option produces no signal anywhere"*, then accepted exactly that for seven
constructors, naming none of these consequences.

**Folded as:** the skip family was **replaced for five of seven**. Family R2 ("degrade at first use") now latches
the fault and surfaces it from the product's first error-returning call — which is what `Chain` itself does. R3
retains exactly the two constructors whose product has **no** error-returning surface (`Message[T]`, `string`), and
their godoc now names the concrete loss. Decision **D-S** (the `Permanent` wrap on latched faults) is new and is
flagged in Spec §3.2 as the most contestable point for round 2.

---

## MAJOR findings

| # | Finding | Evidence | Folded as |
|---|---|---|---|
| **M-3** | **False claim:** revision 1 named `resilience.NewCircuitBreaker` among constructors with "later error-returning methods". `CircuitBreaker` has **none**; `memory.New`'s `Broker` does and was omitted | `flowcontrol.go:42-46` → `Allow() bool`, `Record(success bool)`, `HalfOpen() <-chan struct{}`. `adapter/memory/memory.go:48,59` → `Send`, `Stream` both return `error` | Membership corrected. The correction is load-bearing: it is **why** `NewCircuitBreaker` cannot join R2 and takes the §3.6 error return instead, and why `memory.New` **can** join R2 |
| **M-4** | **AC-4 was unexecutable** for `memory` and `routing`: it demanded `(realOpt, nil, realOpt)` prove "both took effect", but each package exports exactly **one** option, and two applications overwrite each other | `grep -c ') Option {' adapter/memory/memory.go` → 1; `grep -c ') RouterOption {' routing/router.go` → 1 | AC-4 restated as **two** cases: `(realOpt, nil)` proves the before-option applied, `(nil, realOpt)` the after-option |
| **M-5** | **§3.1's "No option is applied" was false** under the plan's own code shape — `opts[0..i-1]` are applied before the loop hits the nil. Matters because `TokenBucketOption`/`CircuitBreakerOption`/`memory.Option` apply to the **live object**, not a config struct | The mandated F1 loop applies then checks. *(Mitigating, verified: every sampled option only sets fields and nil-guards its own argument, so the leak is currently unobservable — a false stated property, not a live bug)* | §3.1 reworded to the property that actually holds, plus the constraint any new option must respect |
| **M-6** | **Error precedence unspecified.** The apply loop sits at different points relative to other validation across constructors, so "fold into the existing loop" makes precedence an accident of pre-existing code order | `endpoint/consumer.go:241` checks `src`/`h` **before** the loop; `sql/outbound.go:53-58` loops **first**. `cron/source.go:184-186` carries an explicit comment — *"Parse before the nil-factory check to preserve the prior error precedence"* — proving the project already treats precedence as designed | New §3.5 decides it: the check runs where the loop runs; earlier-validated faults win; per-constructor and deliberate; plan asserts the decided order |
| **M-7** | **Task 7's AST gate under-specified against four hazards, and its fallback gated nothing.** The fallback asserted a **census**, not a guard — it would pass green against a tree with all 32 guards deleted | (1) `go/parser` alone cannot decide func-ness; (2) qualified types = BLOCKER-1's two; (3) four variadic **func literals** in `harness`; (4) two `//go:build ignore` files in `docs/plans/027-tools` are reachable by a filesystem walk but outside `go list ./...` | AC-7 now enumerates all four as required handling and permits "match by name suffix, document the limitation". **Fallback replaced** with a hand-enumerated per-package table calling every constructor with `(nil)` — weaker, but it tests the invariant |
| **M-8** | Branch-coverage table applied index-bearing rows to all 30, including the 7 that produce no message | Revision 1's table rows "asserts `index 0`" / "asserts `index 1`" marked "all 30" | Scoped to the 25 R1 + 5 R2; R3 gets the corrected AC-4 pair |

## MINOR findings

| # | Finding | Folded as |
|---|---|---|
| **m-9** | `memory.WithBuffer(1<<62)` panics inside `make(chan)` — a **msgin-provided option** that panics on caller input, the class the spec claims to close. Compile-proven by the auditor | Named in Spec §3.7 and deferred in §7 with a reason: it needs an increment covering **all** options, not a one-file patch |
| **m-10** | AC-8's apidiff baseline covers the **root package only** (`head 027-root-api-baseline.txt` → `github.com/kartaladev/msgin`), so "0 removals for root" proves less than claimed | AC-8 now states the limitation, and explains why §3.6's deliberate `resilience` break is invisible to it |
| **m-11** | CLAUDE.md's multi-instance mandate requires the reasoning be **stated**; the bundle was silent | New §7 opening paragraph states it and why it is N/A |
| **m-12** | Spec 014 §3.3 is a **resolution table for 8 named symbols**, not a stated general rule; calling it "the inline-don't-share rule" is interpretation presented as quotation | Re-cited as **precedent**, explicitly labelled as such, in Spec §5 and the ADR |
| **m-13** | Task 5 commits an `adapter/database/sql` change while deferring `dbtest`/`postgres`/`mysql` to Task 8, satisfying "green in every module it touched" only on a narrow reading | Plan Task 5 states the reading and why the risk is low (construction-only change) |
| **m-14** | CLAUDE.md §4 (`/simplify` on big features) appears nowhere in the plan; a 32-site change across 10 packages qualifies | Added to Plan Task 8 |

## Verified-negative (attacked, found sound)

- **Typed-nil is not a hazard.** All 22 option types are declared `func`, every variadic parameter is a concrete func
  type, so there is no interface boxing and `opt == nil` is exact. Generic instantiations (`ConsumerOption[T]`,
  `GatewayOption[Req,Rep]`) compare to nil identically.
- **Nil *inside* a non-nil option is already handled** — `WithLogger`, `WithProducerLogger`, `WithPubSubLogger`,
  `WithConsumerClock`, `WithTokenBucketClock` all guard their argument.
- **Nil variadic *slice* vs nil *element*** — correct as specified; `NewConsumer(src,h,nil...)` yields zero iterations.
- **All of these verified TRUE:** the 24 apply loops; the 22 func-typed option types; `handler.go:52-63`;
  `errors.go:207-230` (207-210 causes / 212-221 invariant / 223+ applied-here); `nilFuncAt`/`nilFuncStep` in **both**
  `endpoint/helpers.go` and `routing/helpers.go` with the verbatim *"do not 'finish the job'"* warning; `memory`
  returning `msgin.ErrInvalidCapacity`; the `sql.NewQueueStore` → `NewOutboundAdapter` + `NewPollingSource` double
  delegation; the 5 HTTP → `NewConfig` delegation; Plan 028's 93.7% coverage figure; docs-link arm 1 on all three
  files.

## Suspected, not proven — carried into round 2

- `msghttp.Option` is `func(*Config)` over an **exported** `Config` with ~700 lines of options
  (`options.go:407-1106`), not exhaustively audited for an option whose effect escapes the `*Config` it receives. If
  one exists, §3.1's "unobservable" no longer holds for the 8 HTTP/stdlib R1 constructors. **Targeted sweep required
  in Plan 028 Task 6.**
- The AST gate may additionally need to skip `expr/` and the four Docker-backed satellite modules. `expr`'s six
  exported constructors take **no** variadic options, so it is likely inert, but this was not exhaustively checked.

## Coordinator's independent re-verification

Both blockers and M-3/M-4 were re-run by the coordinator before any edit — `sed -n '40,63p' handler.go`, the
qualified-type grep, `grep -n -A5 'type CircuitBreaker interface' flowcontrol.go`, the two option-count greps, and
`sed -n '163,170p' message.go`. **All confirmed.** Spec 014:1879's *"base constructors stay non-fallible and
inline-composable"* was also read in context (`sed -n '1872,1884p'`): it is a **local** justification for the `expr`
provider pattern, not a general rule — weaker as a counter-argument than the audit implied, and recorded as such in
Spec §5.
