# Plan 029 — adversarial design audit, round 1 (2026-08-21)

Independent Opus subagent, handed the complete round-1 bundle (Spec 016 rev 1 + ADR 0032 rev 1 + Plan 029 rev 1)
**before any implementation code existed**, per CLAUDE.md's design-time gate. This record is **evidence-primary** —
it is the audit artifact, not a user-facing summary. Every structural claim in the bundle was re-derived on this
tree with `GOTOOLCHAIN=go1.25.13`, darwin/arm64; the commands and their output are pasted below.

**Verdict: NOT SAFE TO IMPLEMENT.** 2 BLOCKERs, 7 MAJORs, 10 MINORs.

The bundle's *measurements* are unusually good — §1 and §1.1 reproduce exactly, which is a first for this project's
audits. The failures are all in the **verification layer and the classification**, which is where Plan 028's audits
also concentrated: one acceptance criterion is unexecutable against the increment's own Task 1, the headline
contract's second clause is false for three knobs the spec certifies as safe, and two of D-Z's four ceiling
rationales are arithmetically or structurally wrong.

---

## BLOCKER-1 — AC-4's credit-gate half is unexecutable after Task 1, so `endpoint`'s zero-size-element property ships with zero coverage

**The claim.** Spec §6 AC-4: *"a test asserts … that `memory.NewQueueStore(WithCapacity(1<<62))` **and a consumer
built with the credit gate at that size** do not panic."* Plan 029's branch-coverage table repeats it
(`zero-size-element safety | WithCapacity(1<<62) and the credit gate at 1<<62 do not panic (AC-4) | memory,
endpoint`), and Task 4 calls its mutant *"the entire point of the task."*

**The evidence.** The credit gate's size is `maxInFlight`, and nothing else:

```
$ grep -n 'newCreditGate' endpoint/*.go | grep -v _test
endpoint/credit.go:20:func newCreditGate(n int) *creditGate {
endpoint/consumer.go:385:	gate := newCreditGate(c.maxInFlight)

$ sed -n '20,22p' endpoint/credit.go
func newCreditGate(n int) *creditGate {
	return &creditGate{tokens: make(chan struct{}, n)}
}
```

Task 1 caps `maxInFlight` at `1 << 20` inside `NewConsumer` (`consumer.go:272-273`). After Task 1 — which the plan
sequences **before** Task 4 — `NewConsumer(src, h, WithMaxInFlight(1<<62))` returns `msgin.ErrInvalidMaxInFlight`,
so **no consumer whose credit gate is `1<<62` can be constructed**. Global constraint 1 forbids a whitebox
fallback (`package endpoint` test calling `newCreditGate` directly).

**Why it matters — three compounding consequences.**

1. Task 4 cannot be completed as written. Under Plan constraint 3 (*"A task that appears to need one has hit a
   design fault: **stop and escalate***") the implementer must halt mid-plan.
2. `credit.go:21`'s zero-size-element property — one of only two the spec says AC-4 exists to pin — ends up with
   **no tripwire at all**. Change `chan struct{}` to `chan managedDelivery` there and, at the new ceiling,
   `1<<20 × 48 = 50,331,648` bytes allocates without a murmur (measured below), so *no* test in the increment can
   catch it. The spec's own §2.1 warning — *"Changing `chan struct{}` to a channel of any non-empty type in either
   place reintroduces the defect silently"* — is exactly what ships.
3. AC-4 is the Spec 015 `NewGateway`-exemption failure mode repeating: an acceptance criterion whose text cannot be
   turned into a running test, discovered only during implementation.

**Recommended fix.** Split the property from the value.
- Move the *observation* (`credit.go:21` is a zero-size element at `1<<62`) into **Task 0**, where the ceiling does
  not yet exist, and record it as evidence rather than as a shipped test.
- Replace AC-4's `endpoint` half with what is still executable after Task 1: assert `NewConsumer(…,
  WithMaxInFlight(maxInFlightCeiling))` + `Run` completes, **and** state in AC-4 that `credit.go:21`'s element type
  is henceforth protected by the ceiling rather than by a test — with the byte figure that makes it safe
  (`1<<20 × sizeof(managedDelivery)` = 48 MiB) written at the allocation site per §4.
- Do not delete the `memory` half; `WithCapacity` keeps no ceiling, so `NewQueueStore(WithCapacity(1<<62))` remains
  constructible and its mutant still fires.

---

## BLOCKER-2 — §3's contract has two clauses; §2.1's partition only establishes the first, and three "safe" knobs violate the second

**The claim.** Spec §3: *"**No exported msgin sizing option can panic or fatally exhaust memory**, at construction
or at any later use."* §2.1 then partitions all 16 into 4 defective / 12 safe **with no residual**, and §7 puts the
12 safe ones out of scope except for AC-4.

**The evidence.** `memory.WithCapacity` is classified *"safe — **zero-size element**"* on the strength of
`queuestore.go:108`. That is true of the semaphore and false of the store:

```
$ sed -n '130,135p' adapter/memory/queuestore.go
	s.mu.Lock()
	s.ready = append(s.ready, entry{msg: msg, visibleAt: s.clock.Now()})
	s.mu.Unlock()
	return nil
}
```

`s.ready` is bounded only by `s.sem`. With `WithCapacity(1<<62)` the semaphore never fills, so `ready` grows one
`append` at a time until the process dies — which is the **grow-by-append** class §2.1 explicitly carves out for
`WithReplayBuffer` (*"safe from panic, not from growth"*), applied here without the carve-out. The option's own
godoc says so verbatim:

```
$ sed -n '54,58p' adapter/memory/queuestore.go
// WithCapacity bounds the number of occupied messages (ready + in-flight);
// default 1024. A bounded buffer is the safe default — an unbounded in-memory
// queue is an OOM lever (CLAUDE.md fail-safe defaults). An explicit n <= 0 is
// msgin.ErrInvalidCapacity.
```

Verified constructible — no panic, no error:

```
$ go run ./zero
make(chan struct{}, 1<<62) cap = 4611686018427387904
  err=<nil> nil?false -> memory.NewQueueStore(WithCapacity(1<<62))            ok
  err=<nil> nil?false -> memory.NewGroupStore(WithMaxGroups(1<<62))           ok
  err=<nil> nil?false -> msghttp.NewConfig(WithMaxConnections(1<<62))         ok
```

`WithMaxGroups` and `WithMaxConnections` are the same shape — §2.1 calls both *"safe — grow-by-insert, capped"*,
but at `1<<62` the cap is off and the map/slice grows without bound.

**Why it matters.** The increment's headline is that a stated contract stops being false for four knobs. As drafted
it delivers a contract that is *still* false for three others, certified "safe" in the same table — and this is the
finding that decides how AC-5's conformance half is written (BLOCKER-1 aside, the 12 rows' "reason" strings are
transcribed from §2.1). It repeats round-1 BLOCKER-1's shape from Plan 028: a partition asserted as complete that
is not.

**Recommended fix.** Pick one, explicitly:
- **(a) Narrow the contract** to *"no exported sizing option can **panic**"*, and move "fatally exhaust memory" into
  a separate, named, deferred class covering `WithCapacity`, `WithMaxGroups`, `WithMaxConnections` and
  `WithReplayBuffer` — with §3.6 stating that a caller who sets one of these enormous has asked for unbounded
  retention, exactly as §3.6.2 already does for `WithReplayBuffer`. **This is the recommended option**: it is a
  wording change, it keeps the increment's scope, and it makes §2.1's three classes honest.
- **(b) Extend the ceilings** to those three. Larger diff, three more policy numbers, and it contradicts §3.6.2's
  own reasoning about `WithReplayBuffer`.

Whichever is chosen, §2.1 must move `WithCapacity` out of the *zero-size-element* class — it is a growth lever
whose *semaphore* happens to be zero-size — and AC-5's declaration string for it must say so.

---

## MAJOR findings

| # | Finding | Evidence | Recommended fix |
|---|---|---|---|
| **M-1** | **`msgin.ErrInvalidCapacity` has THREE producers, not two**, and D-X's "widen the message to a stated range" is therefore unachievable — one sentinel, three sites, three different ranges. Spec §3.5: *"one `errors.Is` target … across `memory.NewQueueStore` and `memory.New`"*; ADR D-X: *"`memory` has **two** bounded constructors"*. | `grep -rn --include='*.go' 'ErrInvalidCapacity' . \| grep -v _test` → `errors.go:258` (decl), **`adapter/memory/queuestore.go:100`**, **`adapter/memory/groupstore.go:92`**. Ranges after the change: `NewQueueStore` `>0` unbounded above; `NewGroupStore` `>0` unbounded above; `New`/`WithBuffer` `[0, 1<<20]`. | Correct the count to three in both documents. Keep the sentinel's message **generic** (`"msgin: capacity out of range"`), put the per-site range in the *wrapping* message (as the R2 shape already does) and in each option's godoc — not in the shared sentinel. Add `NewGroupStore` to Task 3's godoc-widening bullet. |
| **M-2** | **D-Z's `concurrencyCeiling` rationale is arithmetically wrong.** Spec §3.4 / ADR D-Z: *"Goroutine stacks alone (**≥ 8 KiB**) put this near **512 MiB** of workers."* Go's minimum goroutine stack is 2 KiB. This number goes into the option's godoc (Spec §4), i.e. into the API contract. | Measured, 100 000 idle goroutines, `runtime.MemStats.StackSys`: `delta 205127680 bytes = **2051.3 bytes/goroutine**`. So `1<<16 × 2051 ≈ **134 MiB**`, not 512 MiB. Under `-race` the real consumer at the ceiling measured `StackSys 269221888` (~4.1 KiB/goroutine → 257 MiB). | Restate as *"~2 KiB/goroutine minimum (~4 KiB under `-race`), so 65 536 workers cost ~134–257 MiB of stack before any handler state."* The ceiling value itself still stands; only its justification is wrong. |
| **M-3** | **`WithConcurrency`'s "no panic" verdict is incomplete, and Task 0 is told to *assert* it.** §1's table and §1.2 say `WithConcurrency` produces *"**no panic** — spawns `n` goroutines"* and that `sync.WaitGroup.Add` is *"unbounded in Go 1.25"*. Measured: for `n >= 2^31` the counter **silently wraps**, `Wait()` returns immediately, and the first `Done()` panics. | `Add(1073741824) then Done() -> no panic` / `Add(2147483647) then Done() -> no panic` / **`Add(2147483648) then Done() -> PANIC: sync: negative WaitGroup counter`** / same at `1<<40`, `1<<62`. Separately: `wg.Add(1<<40)`/`(1<<62)` → *"Wait() returned IMMEDIATELY (counter wrapped to 0)"*, while `wg.Add(1<<20)` blocks. `consumer.go:457-459` is `wg.Add(c.workers)` then the spawn loop; `drainWorkers` joins on that `wg`. | Restate §1.2: the defect is a **silently corrupted `WaitGroup`** (wrap at `2^31`, latent `sync: negative WaitGroup counter` on the first `Done`, and a `Wait()` that returns while workers still run) *plus* unbounded spawn — not "a hang instead of a panic". **Delete Task 0's instruction to assert "it is *not* a panic"**; assert the wrap boundary instead, which is a real, cheap, deterministic observation. |
| **M-4** | **AC-5 half 2 is behavioral for 4 of 16 keys; for the other 12 the "declaration" is an inert string.** ADR D-AA: *"half 2 asserts the actual behavior rather than the presence of a shape, so it **cannot** be satisfied by a guard that does not guard."* True for the 4 defective keys only. Spec AC-6 probe (b) — *"a table row whose behavioral claim is false"* — can likewise only be planted on those 4. | Spec §6 AC-5.2: *"assert either *rejects `1<<62` with a typed error* or *is declared allocation-free with a stated reason*. The 12 safe rows carry their §2.1 reason as the declaration."* A reason string asserts nothing. | Make the 12 rows **executable**: assert each safe knob *accepts* `1<<62` and its product is usable — all 12 are constructible today (probe above). That single change turns half 2 into a real element-type tripwire for `queuestore.go:108`, `groupstore.go:94`, `sse_server.go:466-476` and the read-limit knobs, and gives AC-6 probe (b) 16 plantable rows instead of 4. Note the one exception found: `WithSuccessStatus(1<<62)` **rejects** (`msghttp: status code must be in [100,599]`), so it belongs in the first arm, not the "allocation-free" arm. |
| **M-5** | **The class gate does not gate the class the contract names.** Spec §3 states the invariant over *"reachability of an allocation"*; AC-5 half 1 collects *"every exported `func With…` whose first parameter is `int`/`int64`."* An exported **positional** sizing parameter is invisible to both the §2 regeneration command and the gate. One exists today. | `git ls-files '*.go' \| grep -v _test \| xargs grep -nE '^func [A-Z][A-Za-z0-9]*(\[[^]]*\])?\([^)]*\b(int\|int64)\b' \| grep -v 'func With'` → `resilience/ratelimit.go:42:func NewTokenBucket(rps float64, burst int, opts ...TokenBucketOption)`. (`burst` is stored as `float64` and reaches no allocation — but that is a *fact about today's body*, which is precisely what a class gate is supposed to stop depending on.) | Either (a) widen half 1 to *"every exported function with an `int`/`int64` parameter in any position"* and add `NewTokenBucket`'s `burst` to the conformance table with its reason, or (b) state the boundary explicitly in the ADR's "Accepted limitation" paragraph — the gate covers `With…`-named options only, and a positional sizing parameter is out of scope. (a) is preferred; the delta today is one row. |
| **M-6** | **ADR D-X's justification for sentinel reuse is false as planned.** D-X: *"The offending value and its bound appear in the message, which is where a debugger looks."* Plan 029's own R1 code shape returns the **bare** sentinel — no value, no bound — and Global constraint 4 keeps it bare. | Plan §"The two code shapes": `return nil, msgin.ErrInvalidMaxInFlight`. Existing behavior is identical (`consumer.go:263`, `:273`, `adapter/http/options.go:1165`). The value only appears in the **R2** shape, which is one of four knobs. | Either drop that sentence from D-X, or change the three R1 arms to `fmt.Errorf("%w: %d exceeds %d", msgin.ErrInvalidMaxInFlight, n, ceiling)` — which is still bare-of-`Permanent`, satisfies constraint 4, keeps every `errors.Is` working, and actually delivers the debuggability D-X claims. **Recommend the latter**, since CLAUDE.md's core criterion is debuggability and the wrap costs one line per arm. If taken, add a table case asserting the value appears, and note that the *lower* arm should be wrapped the same way for symmetry. |
| **M-7** | **D-Z's `WithConnectionBuffer` rationale is a product with an uncapped second factor.** ADR D-Z: *"4096× the default, and **per connection** — the real cost is this × `WithMaxConnections`."* `WithMaxConnections` gets no ceiling (§2.1 "safe"), and accepts `1<<62` (probe above). So the ceiling bounds one factor of the quantity its own rationale names. | `adapter/http/options.go:54: const defaultMaxConnections = 1024`; `options.go:1157-1160` validates `<= 0` only; `msghttp.NewConfig(WithMaxConnections(1<<62))` → `err=<nil>`. At the connBuffer ceiling each connection allocates `1<<16 × 24 = 1,572,864` bytes. | Resolve together with BLOCKER-2. Either bound `WithMaxConnections` too, or restate the rationale in terms of what *is* bounded ("64 MiB per connection at the ceiling; the process total is `WithMaxConnections` × that, and `WithMaxConnections` is the caller's to size"). Do not leave a rationale that reasons about an unbounded product. |

---

## MINOR findings

| # | Finding | Evidence | Recommended fix |
|---|---|---|---|
| **m-1** | **§2.1's table has 17 rows under a heading claiming 16**, and one row — *"(the credit gate)"* — is not an option at all. AC-5 half 1 fails *"in either direction"* against the conformance table's key set, so an implementer who transcribes §2.1 gets a guaranteed failure on a key that has no `func With…`. | §2.1 rows, counted: 4 defective + 13 "safe", of which `*(the credit gate)*` `endpoint` `credit.go:21` is an internal allocation site. `4 + 12 = 16` only after excluding it. | Move the credit-gate row out of the table into the prose beneath it (it is an allocation site, not an option), or mark it `— NOT AN OPTION, excluded from the AC-5 key set`. |
| **m-2** | **Task 2's `ServeHTTP` case hangs if written the obvious way.** `httptest.NewRequest` carries a background context and the SSE handler streams until the request context is done. | Measured with the naive shape: `RESULT: ServeHTTP BLOCKED for 3s (naive test HANGS)`. The repo already solves this: `adapter/http/sse_server_test.go:176 // serveInBackground runs s.ServeHTTP(w, req) on a goroutine cancellable via the …`. | Name `serveInBackground` in Task 2's bullet, and note that a naive version also leaks a goroutine (goleak). |
| **m-3** | **§5's alternatives omit the "don't allocate from the knob at all" class.** For `WithMaxInFlight` the actual bound is the credit gate (`chan struct{}` — safe at any `n`); `workerCh`'s buffer is sized to match only for the deadlock reason documented at `consumer.go:375-384`. Decoupling the two would let `maxInFlight` stay unbounded. | `consumer.go:384-385`: `workerCh := make(chan managedDelivery, c.maxInFlight)` immediately followed by `gate := newCreditGate(c.maxInFlight)`. | Add the row to §5 and reject it on the record (it perturbs a delicate, repro-proven deadlock fix, and does not help the other three knobs). An omitted alternative reads as an unconsidered one. |
| **m-4** | **No multi-instance statement.** CLAUDE.md's *"Multi-instance / distributed-deployment awareness (mandatory when designing any EIP component)"* requires the reasoning be **stated**, even when N/A. Repeat of Plan 028 audit **m-11**. | Neither Spec 016 nor ADR 0032 contains the words "multi-instance", "topology" or "instance". | One paragraph in Spec §7: the ceilings are per-process constants with no cross-instance state; a horizontally-scaled deployment multiplies the per-process cost by N, which is the operator's sizing concern. |
| **m-5** | **No `/simplify` step.** CLAUDE.md Development workflow §4. Repeat of Plan 028 audit **m-14**. | Plan 029 Task 6 lists code-review, security-review, the 8×8 matrix, coverage, AST diff, docs links, HANDOVER, CLAUDE.md — no `/simplify`. | Add to Task 6, before `/code-review`. |
| **m-6** | ADR D-W asserts the bound is *"not a number computed from `maxAlloc`, available memory, or **GOMEMLIMIT**"* without evidence that GOMEMLIMIT is unable to help. | Verified by the auditor: `GOMEMLIMIT=67108864 ./regime 41` → `runtime: out of memory: cannot allocate 140737492549632-byte block` / `fatal error: out of memory`. GOMEMLIMIT is a GC-pacing soft limit; a single oversized `mmap` still throws. | Fold the measured line into §Context so the rejection is evidenced rather than asserted. |
| **m-7** | **The ceilings are unexported, so a caller cannot pre-validate** its own config against them; exporting them (`const MaxInFlightCeiling = 1 << 20`) was not considered among §5's alternatives. | Plan §"The ceilings (D-Z)": all four declared unexported. | Add the row to §5. Rejecting it is fine (it is new exported surface, against constraint 3); leaving it unexamined is not, because it is the natural request from the first caller who hits a ceiling. |
| **m-8** | **The accepting-at-ceiling cases are heavy**, and nothing in the plan says so. | Measured under `-race`, real `NewConsumer`+`Run`: `WithMaxInFlight(1<<20)` → `HeapAlloc … -> 50590368` (the 48 MiB channel; `1<<20 × sizeof(managedDelivery)=48` = 50,331,648 bytes exactly). `WithConcurrency(1<<16)` → `Sys 323340616, StackSys 269221888` (~320 MB RSS). Both pass; 65 536 goroutines under `-race` is fine (separately probed). | State the figures in Task 1 so a CI memory spike is expected rather than investigated. Also record the 48 MiB number as the answer to *"does bounding at `1<<20` actually prevent the crash"* — **it does**: 48 MiB is four orders of magnitude below the fatal band. |
| **m-9** | **The plan shows only the `maxInFlightSet` R1 shape**, which does not transfer verbatim to `WithConcurrency`: there is no `concurrencySet` flag and the default is assigned in the config initializer. | `consumer.go:251 cfg := consumerConfig[T]{concurrency: 1, …}`; `consumer.go:262-264 if cfg.concurrency < 1 { return nil, msgin.ErrInvalidConcurrency }` — no `else if`, no set flag. | Show both shapes, or say "for `WithConcurrency`, extend the existing single-arm condition with `\|\| cfg.concurrency > concurrencyCeiling`". |
| **m-10** | **Three knobs share the unit "queued messages"; two get `1<<20` and one is left unbounded.** §3.4 justifies `memory.WithBuffer`'s ceiling as *"Same unit as `WithMaxInFlight` … so the two knobs agree"* — but `memory.WithCapacity` is the same unit and gets nothing. | `defaultMaxInFlight = 1024` (`endpoint/flowcontrol.go:12`), `defaultCapacity = 1024` (`adapter/memory/queuestore.go:40`), `memory.WithBuffer` default 0. | Resolve with BLOCKER-2: whichever way that goes, say *why* `WithCapacity` is treated differently from the other two knobs in its own unit. |

---

## §3.6.1 — the `WithBuffer(-1)` asymmetry: **VERDICT — FOLD IT IN**

The spec flags this to the audit rather than settling it. Attacked, the asymmetry is worse than the churn.

**1. The spec's own rejection rationale condemns the existing clamp.** §5 rejects *"Clamp silently to the ceiling"*
because *"the caller believes they configured a buffer they did not get, which is the 'silently-substituted
default' CLAUDE.md's Sensible-defaults gate forbids."* That sentence describes `WithBuffer(-1)` exactly. Shipping a
document that rejects silent clamping in one direction while preserving it in the other, in the same function, is a
contradiction a reader will find in one pass:

```
$ sed -n '34,45p' adapter/memory/memory.go
// WithBuffer sets the channel buffer size (default 0 — synchronous handoff).
// A negative n is clamped to 0 rather than panicking, honoring the library's
// no-panic-on-caller-input contract.
func WithBuffer(n int) Option {
	return func(b *Broker) {
		if n < 0 {
			n = 0
		}
		b.ch = make(chan msgin.Message[any], n)
	}
}
```

**2. It is the only sizing knob left asymmetric.** Every sibling rejects the negative end with a typed error:
`memory.NewQueueStore(WithCapacity(-1))` → `ErrInvalidCapacity` (`queuestore.go:100`);
`NewConsumer(WithMaxInFlight(-1))` → `ErrInvalidMaxInFlight` (`consumer.go:273`);
`NewConfig(WithConnectionBuffer(-1))` → `ErrInvalidConnectionBuffer` (`options.go:1165`).

**3. The cost is three lines in a function the increment already rewrites.** The mechanism — latch +
unconditional `return` — is being built in this exact closure by Task 3. Adding `n < 0` costs one condition, one
table case and one mutant. Deferring costs a second increment that reopens the same ten lines and re-widens the
same sentinel's godoc a second time.

**4. It resolves M-1 rather than compounding it.** Fold it in and `memory.New`'s range becomes `[0, 1<<20]`, so
`ErrInvalidCapacity` means "out of range" at all three producers instead of "≤ 0" at two and "> ceiling" at one.

**5. The breaking-change objection does not apply here.** msgin is unreleased with no consumers; ADR 0032's
"Bad, accepted" bullet treats this as expensive, and at pre-v1 it is free.

**Verified safe to do.** `New` initialises `b := &Broker{ch: make(chan msgin.Message[any])}` (`memory.go:58`)
*before* the apply loop, so an early `return` from `WithBuffer` leaves `b.ch` non-nil — no nil-channel deadlock,
and D-V reports the latch before `Send`/`Stream` touch it anyway.

**If the user prefers not to fold it in**, the spec must at minimum stop citing the silent-substitution rule as its
reason for rejecting the ceiling clamp, since the same rule indicts the behavior it is preserving.

---

## Verified-negative (attacked, found sound) — these are the bundle's strengths

**§1's four defects — all four reproduced verbatim on this tree.** Every line of §1's table is true.

```
### maxinflight
NewConsumer err = <nil>
Run PANICKED: makechan: size out of range
### connbuffer
NewConfig err = <nil> cfg nil? false
NewSSEServer err = <nil> server nil? false
ServeHTTP PANICKED: makechan: size out of range
### buffer
memory.New PANICKED: makechan: size out of range
### buffer-du                                       ← D-Y's premise
memory.New(nil, WithBuffer(1<<62)) PANICKED: makechan: size out of range
### concurrency
NewConsumer err = <nil> consumer nil? false
```

**§1.1's two-regime table — reproduced exactly, including the unrecoverable arm.** A 64-byte element,
darwin/arm64, with a deferred `recover` installed:

```
n=2^44 elem=64 => RECOVERED PANIC: makechan: size out of range
n=2^43 elem=64 => RECOVERED PANIC: makechan: size out of range
n=2^42 elem=64 => RECOVERED PANIC: makechan: size out of range
n=2^41 → runtime: out of memory: cannot allocate 140737492549632-byte block (3932160 in use)
         fatal error: out of memory
         goroutine 1 … runtime.throw({0x102819e9a?, 0x9?})
```

The `recover` printed nothing on the `2^41` run — **`runtime.throw` is genuinely uninterceptable**, so §1.1's
consequences 1–3 and §5's rejection of both the `maxAlloc`-mirroring guard and the `recover()` conversion are
correct. This is the load-bearing measurement of the whole bundle and it holds.

**Attack D answered: `1<<20` does prevent the crash.** `sizeof(managedDelivery)` = `sizeof(msgin.Delivery)` (40) +
one `func()` (8) = **48**, so the ceiling allocation is `50,331,648` bytes = 48 MiB — confirmed at runtime
(`HeapAlloc … -> 50590368`). `memory.WithBuffer`'s ceiling is `1<<20 × 24 = 25,165,824` bytes; the connBuffer
ceiling is `1<<16 × 24 = 1,572,864` bytes per connection. None is near the fatal band. No ceiling interacts badly
with `WithPollMaxBatch` either — `poller.go:36` bounds `held` by *free credits*, so `pollMaxBatch` can never exceed
`maxInFlight` in effect.

**The inventory command yields exactly 16, and all 16 are in root-module packages.**

```
$ git ls-files '*.go' | grep -v _test | xargs grep -hnE \
    '^func With[A-Za-z]+(\[[^]]*\])?\([a-z]+ (int|int64)\)' | wc -l
      16
```
The 16 sit in `adapter/http` (7), `adapter/memory` (3), `endpoint` (4), `resilience` (1), `routing` (1) — all root
module. `expr`, `sqlite`, `harness`, `dbtest`, `crontest`, `postgres`, `mysql` contribute **zero**, so AC-5's
"the behavioral half reaches all of them today" is TRUE. (See M-5 for what the command cannot see.)

**All 12 "safe" verdicts hold against *panic*** — probed behaviorally, not read (output in BLOCKER-2). The
allocation inventory is complete: every `make(` with a non-constant size in a non-test file was enumerated, and the
only caller-controlled ones are the five §2.1 names.

**§2.1's zero-size-element figure is exact**: `make(chan struct{}, 1<<62)` → `cap = 4611686018427387904`.

**D-Y's "fix the class" claim is TRUE — `memory.WithBuffer` really is the only one.** All six apply loops in the
workspace that `continue` past a nil (i.e. where a *later* option still runs) were enumerated, with their option
sets:

| Loop | Option type | Members | Can any fault? |
|---|---|---|---|
| `adapter/memory/memory.go:60` | `memory.Option` | `WithBuffer` **only** (`grep -c ') Option {' adapter/memory/memory.go` → **1**) | **yes** |
| `channel/pubsub.go:174` | `PubSubOption` | `WithFanOut`, `WithPubSubLogger`, `WithSingleSubscriber` | no |
| `channel/pubsub_registry.go:50` | `PubSubOption` | same three | no |
| `routing/router.go:87` | `RouterOption` | `WithDefaultChannel` only | no |
| `adapter/database/sql/sqlite/dsn.go:62` | `DSNOption` | `WithJournalMode`, `WithBusyTimeout`, `WithSharedMemory` | no |
| `message.go:171` | `MessageOption` | `WithClock`, `WithID`, `WithHeaders` | no |

Every other constructor `return`s at the nil (25 `nilOptionAt` sites), so no later option runs.

**AC-3 kills its mutant; AC-3b is a genuinely different property.** Traced by hand against both shapes:
`New(nil, WithBuffer(1<<62))` — correct shape → latch holds `ErrNilFunc`, `WithBuffer` returns before the `make`;
wrong shape (`return` nested in `if b.err == nil`) → falls through to the `make` → panic → AC-3 red. ✔
`New(WithBuffer(1<<62), nil)` behaves identically under both shapes, so AC-3b is not redundant with AC-3 — it
tests first-**fault**-wins, exactly as the spec says. ✔ Task 3's mutants (a)/(b)/(c) each kill their own target.

**The `Permanent` wrap on the R2 latch is required, not redundant.** `ErrInvalidCapacity` is not in `IsPermanent`'s
list — `reliability.go:94-96` covers only `ErrPayloadType`, `ErrPayloadDecode`, `ErrPayloadTooLarge` — so unlike
decision D-K's `ErrPayloadType` case, the explicit wrap is load-bearing.

**Message widening is test-safe.** No test, Example or `// Output:` block asserts the four sentinels' text:
`grep -rn --include='*_test.go' 'must be >= 1\|capacity must be > 0\|connection buffer must be > 0…' .` → **no
hits**. The only matches repo-wide are in `docs/plans/001`, `003`, `013` — immutable execution records.

**`Broker`'s error-returning surface is exactly `Send` and `Stream`** (`memory.go:74`, `:90`; the only other method
is `EmitsLiveValue() bool`), so §3.2 is right.

**AC-5 half 2 is importable from root.** `capability_test.go:13-16` already imports `adapter/memory`, `channel`,
`endpoint`, `routing` from `package msgin_test` — no cycle.

**The ceiling multipliers are right.** `defaultMaxInFlight = 1024` → `1<<20` is 1024×;
`defaultConnectionBuffer = 16` → `1<<16` is 4096×; `WithConcurrency` default 1; `WithBuffer` default 0. ✔

**Housekeeping figures check out.** `docs/specs/[0-9]*.md` → **16**; `docs/adrs/[0-9]*.md` → **31**; plan distinct
numbers → **29** (files → 40). The 93.9% coverage baseline matches `docs/HANDOVER.md:16` and `:74` (Plan 028's own
plan text says 93.7%, which was Plan 027's figure — Plan 029 cites the right one). Docs-link gate **arms 1 and 2
are clean** on all three new files, and the `-race` detector handles 65 536 live goroutines without dying.

---

## Suspected, not proven — carried into round 2

- **`memory.Option` is exported and takes the live `*Broker`**, so `memory.WithBuffer(1<<62)(b)` is a legal call on
  an already-constructed, already-in-use `Broker`. Today that races `b.ch`; after Task 3 it also writes `b.err`
  from an arbitrary goroutine, and `Send`/`Stream` read `b.err` unsynchronised. The field comment
  (`memory.go:17-23`, added by commit `4ce4d84`) already scopes its lock-free justification to *"as long as no
  caller-supplied Option lets the live `*Broker` escape `New`"* — Task 3 should re-read that comment and confirm it
  still holds verbatim, or widen it. Not proven to be reachable from any shipped code path.
- **`time.Duration` knobs are outside both the inventory and the gate by construction** (the AST sees
  `time.Duration`, not `int64`). Not audited for a duration that reaches a `time.NewTicker` with a non-positive or
  overflowed value; `resilience.NewTokenBucket`'s `time.Duration(float64(time.Second) / rps)` for a sub-normal
  `rps` overflows to a negative duration. Adjacent class, deliberately not folded in — but the ADR should say the
  boundary is deliberate.

## Auditor's method note

Every command in this record was run by the auditor on the tree at `48bbe83` with `GOTOOLCHAIN=go1.25.13`,
`GOWORK=off`, from a throwaway module outside the repository (`replace github.com/kartaladev/msgin => …`), which
was deleted afterwards. No file in the repository was modified. The three `makechan` regimes, the four defect
reproductions, the `WaitGroup` boundary, the goroutine-stack measurement, the `-race` ceiling runs, the naive
`ServeHTTP` hang and the twelve safe-knob probes are all first-hand output, not transcription.

---

**VERDICT: NOT SAFE TO IMPLEMENT.**
