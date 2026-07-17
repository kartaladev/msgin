# ADR 0008 — Resilience & flow-control API

- **Status:** Accepted (2026-07-17)
- **Context source:** [Spec 001 — Messaging core](../specs/001-messaging-core.md) §7.4;
  [Plan 003 — Resilience & flow control](../plans/003-resilience-flow-control.md)
- **Related:** [ADR 0002 — Adapter SPI](0002-adapter-spi.md) (runtime-owned reliability, `Delivery`/
  `Ack`/`Nack`, `NativeReliability`), [ADR 0004 — clockwork](0004-clockwork-dependency.md),
  [ADR 0006 — Resilience & flow control](0006-resilience-flow-control.md) (the high-level design this
  ADR realizes as a concrete API), [ADR 0007 — Reliability & settlement API](0007-reliability-settlement-api.md)
  (the settlement switch this plan wraps, not replaces)

## Context

ADR 0006 committed msgin to a layered, first-phase flow-control model — a mandatory credit gate plus
optional rate limiting, handler timeout, circuit breaker, and overflow policy — all `clockwork`-driven
interfaces with dependency-free defaults. Plan 003 turns that design into a concrete public API and
runtime wiring on top of Plan 001/002's `Consumer`/`StreamingSource` core. As with ADR 0007, these
decisions cut across many of the plan's tasks, so they are recorded here as one **living** ADR with
per-decision (`D1`–`D9`) sections: Task 1 (this commit) writes `D1`–`D2` in full and stubs `D3`–`D9`;
later tasks flesh out their stub in the same commit that implements the decision.

## Decision

### D1 — Always-on, finite credit gate (Task 1)

`WithMaxInFlight(n)` bounds claimed-but-unsettled messages to `n` (default `defaultMaxInFlight =
1024`, never unbounded — the credit gate is the load-bearing flood defense and is always active, spec
§7.4.1). `n < 1` is a caller error → `ErrInvalidMaxInFlight`.

**Unset-vs-explicit-0 (audit finding C2).** Because the default (1024) is non-zero, a bare `0` cannot
double as the "unset" sentinel: a `maxInFlightSet bool` flag on `consumerConfig[T]` (mirroring the
existing `codecSet` pattern) distinguishes "unset → default" from an explicit `WithMaxInFlight(0)`,
which must be *rejected*, not silently defaulted. `NewConsumer` resolves this as: not set →
`defaultMaxInFlight`; set → require `n >= 1`, else `ErrInvalidMaxInFlight`. The flag pattern is applied
**only** to `WithMaxInFlight` — the sole new option with a meaningful/ambiguous zero. The other new
options have no such conflation (see D2) and need no flag.

### D2 — Option ergonomics: nil-safe plug-ins, closed enum (Task 1)

- A nil `RateLimiter`/`CircuitBreaker` passed to `WithRateLimit`/`WithCircuitBreaker` is a **no-op**:
  the option guards `if r != nil` (resp. `b != nil`) before assigning, so the config field simply stays
  at its zero value (`nil`) rather than being set to a nil interface that would panic on first use.
  There is no error case here — nil is unambiguous, unlike `WithMaxInFlight`'s zero.
- `WithHandlerTimeout(d)` with `d <= 0` and "unset" are identical behavior (timeout disabled), so no
  flag is needed there either.
- `WithOverflow(OverflowBlock)` equals the zero-value default; the `OverflowPolicy` enum is **closed**
  (`OverflowBlock`, `OverflowDropNewest`, `OverflowDropOldest`, `OverflowReject`) and out-of-range
  values both **render** (`String()`) and **behave** as `OverflowBlock` — the Task 6 overflow `switch`
  enumerates the shed policies explicitly with a blocking `default`, so an out-of-range value
  backpressures rather than silently dropping (audit finding I1).

### D3 — Credit model = size-n semaphore, acquired at ingress (Task 2)

The credit pool is a **counting semaphore of capacity `n`** realized as a buffered `chan struct{}`:
`acquire` sends a token (blocks when full), `release` receives one. The buffer capacity **is** the whole
bound — there is no separate counter — so this realizes spec §7.4.1's "bounded buffer of size `n` as the
credit pool" exactly (`creditGate` in `credit.go`).

The old single `deliveries` channel is replaced by a **two-stage ingress pipeline**:
`Stream → rawCh → ingest goroutine → workerCh → workers`. `rawCh` is **unbuffered**; `ingest` reads one
delivery, acquires a credit (Task 2 ships only `OverflowBlock`, so `acquire` simply blocks = backpressure),
wraps it release-first (D4), and hands it to a worker over `workerCh`.

**No over-pull (audit finding M5).** A delivery reaches a worker **only after** acquiring a credit, so
**claimed-but-unsettled (credit-holding) ≤ `n` strictly**, independent of worker count or burst size —
this is the load-bearing flood-defense bound. The weaker "pulled-from-source-but-not-yet-settled" figure
is *not* `n`: it can reach `n+2`, because two deliveries can sit parked **off** the credit pool at once —
one parked in `ingest` on `acquire` (pulled from `rawCh`, awaiting a credit) and one parked in the
source's `Stream` trying to send the next delivery onto the unbuffered `rawCh`. Both hold **no credit**,
so neither breaks the `≤ n` credit bound; they are the whole read-ahead slack, and the source still
cannot be drained faster than credits free.

**`workerCh` is buffered to capacity `n` — a Task 8 fix, not the original Task 2 design (audit finding,
whole-branch review gate).** Task 2 originally shipped `workerCh` unbuffered too. The whole-branch
`/code-review` pass before delivery **reproduced a genuine, confirmed deadlock** on the *default* consumer
(concurrency 1, unset `WithMaxInFlight`) over an unbuffered `memory.Broker`: with a backlog of ≥ 2
messages queued behind a busy worker, a transient failure whose `Nack(requeue=true)` synchronously
re-injects into the source (`adapter/memory`'s `Send`) forms a 3-way cyclic wait — `Stream` parked
sending to `rawCh`, `ingest` parked sending to the unbuffered `workerCh` (no worker free to receive), and
the sole worker parked re-injecting its `Nack` back into the source's own inbound channel — with **nobody
left to drain the source**. This reproduced on roughly 40–50% of runs of a 3-message repro (`go test -run
TestConsumer_Run_BacklogBehindBusyWorkerWithRequeue_NoDeadlock -count=20`, since fixed and committed as a
permanent regression test) and only ever resolved via the caller cancelling and waiting out the full
`shutdownTimeout` — a silent stall of the entire consumer, not a crash, so it was easy to miss without an
adversarial backlog scenario.

The fix: **`workerCh` is a buffered channel of capacity `c.maxInFlight`**, not unbuffered. Since a
delivery only ever reaches `workerCh` *after* acquiring a credit, and the gate never admits more than `n`
credits concurrently, the channel's occupancy (items handed off but not yet claimed by a worker) can never
exceed `n` — sizing the buffer to `n` can therefore never overflow; it is exactly sufficient, never more.
This makes `ingest`'s **only** blocking point `gate.acquire` (the intended, documented backpressure) —
never worker availability — so `ingest` always keeps draining `rawCh` up to the credit limit, which keeps
`Stream` always able to drain the source, which is exactly the property a synchronously re-injecting
`Nack` needs to complete without forming a cycle. This also closes a related whole-branch-review
observation (altitude/removed-behavior angles): with an unbuffered `workerCh`, the real "claimed-but-
unsettled" ceiling was `workers + 1` regardless of the configured `n` (e.g. the default `n=1024,
concurrency=1` never actually claimed more than ~2 messages ahead) — the buffered fix restores the
credit gate as the *actual*, not just nominal, bound described earlier in this decision. The "no over-pull
≤ n" and "weaker bound ≤ n+2" invariants above are unaffected: they were never about `workerCh`'s own
capacity, only about the credit gate's.

**Shutdown / drain.** `ingest` stops admitting on parent-ctx cancel (it returns, closing `workerCh`,
which lets the workers finish their `range`). A delivery `ingest` is holding at cancel — parked on
`acquire` or on the `workerCh` send — is Nacked (requeue) by `admit`'s ctx-done branch, releasing any
credit it held, so it is not lost. Because `ingest` is the admission gate and stops at parent-cancel
(strictly before `drainCtx` is cancelled at the shutdown deadline), no delivery reaches a worker after
`drainCtx` is cancelled; the worker-side drain short-circuit in `process` is a defensive parity guard for
that window rather than the primary drain path. `Run` joins the ingress goroutine (`ingressWG.Wait()`)
before returning so shutdown is leak-free.

### D4 — Release-first settlement wrapping (Task 2)

Each admitted delivery is wrapped by `manage(d, sync.OnceFunc(gate.release))` (`credit.go`): the wrapped
`Ack`/`Nack` call `release()` **before** invoking the original closure. This ordering is essential
because an adapter whose `Nack(requeue=true)` **synchronously re-injects** the message (the memory
adapter re-enqueues via `Send`, which re-enters the pipeline and needs a credit) would otherwise
deadlock at `n=1`: the redelivery would wait for the credit the Nacking delivery still holds. Releasing
first frees the slot for the re-injected message, so net in-flight never exceeds `n` and a single-credit
consumer keeps flowing (NF-4/NF-5). A Nacked message awaiting its backoff / redelivery therefore does
**not** pin a credit — fresh work proceeds.

`release` is idempotent (`sync.OnceFunc`), so it fires **exactly once** across every settle path — Ack,
Nack, DLQ-divert, invalid-divert, the ctx-done abandon in `admit`, and the worker's `defer md.release()`
panic-safe net — never over- or under-releasing (releases == acquires). A delivery whose ctx is done
**before** it acquires a credit never took one, so its ctx-done Nack releases nothing (no spurious
release). The Plan 002 settlement switch (`dispatch`) is **not touched**: it calls `d.Ack`/`d.Nack`
exactly as before, and the wrapping transparently makes those release credit.

### D5 — `RateLimiter` interface + token-bucket default (Task 3)

`RateLimiter` (`flowcontrol.go`) is the narrow SPI: `Wait(ctx) error` blocks until the caller may
proceed or `ctx` is done. `WithRateLimit[T](r)` wires it into the consumer; a `nil` r is a no-op (no
pacing) — the flag is unset by omission rather than a sentinel, matching `WithCircuitBreaker`.

**Default implementation — `NewTokenBucket`.** `NewTokenBucket(rps, burst, opts...)` (`ratelimit.go`)
returns the dependency-free default: a `clockwork.Clock`-driven bucket of `burst` tokens refilling at
`rps` tokens/sec, starting full. Construction validates eagerly: `rps <= 0` or `burst < 1` →
`ErrInvalidRateLimit` (mirrors `ErrInvalidMaxInFlight`'s construction-time-typed-error contract).
`WithTokenBucketClock(clockwork.Clock)` injects the clock (real by default; tests inject
`clockwork.NewFakeClock()` so `Wait`'s pacing is deterministic — no real `time.Sleep`).

**M1 fix — loop-until-consumed, not blind post-wait admit.** A naive `Wait` computes the delay until
one token accrues, `<-clock.After(delay)`, then unconditionally returns — but if a concurrent caller
already consumed the freed token while this one slept, that return hands out a token that was never
actually decremented (over-admit past the configured `rps`). `tokenBucket.Wait` instead **loops**:
lock, refill for elapsed time (capped at `burst`, never unbounded — a long idle period does not let a
subsequent burst exceed the configured cap), and only return once a token is genuinely available and
decremented; otherwise recompute the remaining deficit and wait again. In msgin, the limiter is driven
solely by the single serial ingest goroutine (`admit`), so the loop runs at most once in practice — but
correctness does not rely on that; the bucket is safe under concurrent callers too (`mu` guards
`tokens`/`last`).

**Composition order — rate-limit before the credit gate.** `admit` (`consumer.go`) runs the limiter's
`Wait(ctx)` **before** `gate.acquire(ctx)` (spec §7.4: rate-limit → credit gate → buffer → workers). A
limiter error (ctx done while parked) Nacks the delivery via `settleCtx` and returns `false` — no credit
was ever acquired for it, so nothing is released; this mirrors the existing ctx-done-before-acquire
path for the credit gate itself. Ordering it first means a rate-limited flood never occupies in-flight
credit while merely waiting its turn — credit is reserved for work actually admitted past the pacing
gate.

**Plug-in path.** `golang.org/x/time/rate.Limiter` already implements `Wait(ctx) error` and satisfies
`RateLimiter` directly — callers who want the real wall clock (rather than the deterministic-under-test
`clockwork` default) inject an `*rate.Limiter` via `WithRateLimit` with no adapter code required. This
keeps `x/time/rate` an optional caller dependency, never a forced one (Dependency policy).

### D6 — Handler timeout via clockwork context (Task 4)

`WithHandlerTimeout(d)` (spec §7.4.4) wraps only the handler invocation in `dispatch` with a
further-derived context, `handlerContext(parent)` (`consumer.go`): `d <= 0` (unset or an explicit
non-positive value, D2) returns `parent` unchanged with a no-op cancel — zero overhead, no timer
goroutine. Otherwise it derives `context.WithCancel(parent)` and arms a **`clock.AfterFunc(d, cancel)`**
timer, so the deadline is driven by the injected `clockwork.Clock` (deterministic under
`clockwork.NewFakeClock()` — no real `time.Sleep` in tests) rather than `context.WithTimeout`'s
wall-clock `time.AfterFunc`.

**Synchronous, no spawned handler goroutine.** The handler still runs inline on the worker under the
derived context; `WithHandlerTimeout` does not spawn a goroutine to run it. Spawning one to "enforce" the
deadline would orphan-leak on a **non-cooperative** handler (one that ignores `ctx.Done()` and never
returns) — the spawned goroutine would run forever with nothing to join it. Running inline means a
non-cooperative handler still blocks its worker past the deadline (an explicitly accepted residual, see
Consequences), but a **cooperative** handler — the case this decision targets — observes `ctx.Done()`,
returns promptly, and the worker is freed.

**Cancel joins cleanly either way.** The `cancel` returned by `handlerContext` always `timer.Stop()`s
before calling the underlying `context.CancelFunc`: on normal handler completion (before the deadline)
this cancels the fired-timer path so the timer never fires late and does nothing on an already-returned
handler; on timeout, the `AfterFunc` firing IS the cancel — the handler's `ctx.Done()` closes, and the
caller's subsequent `timer.Stop()` on the already-fired timer is a harmless no-op. Either order leaves no
pending timer and no leaked goroutine (goleak-clean).

**Classification — timeout is transient, not permanent.** A handler that returns `ctx.Err()`
(`context.Canceled`, since `handlerContext` uses `WithCancel` rather than `WithDeadline`) after observing
the timeout is **not** wrapped in `Permanent(...)`, so `isPermanent` (`reliability.go`) falls through to
the transient branch of the spec §7 settlement switch: `dispatch` Nacks it (redelivered, subject to
`RetryPolicy`/backoff) exactly like any other transient failure — never diverted to the invalid-message
sink. This is deliberate: a timeout reflects the *handler's* pace against a deadline, not a poison
message: the same message may well succeed on redelivery (e.g. once a downstream dependency recovers),
so it must stay in the normal retry path rather than being permanently rejected.

**Scope — bounds the Plan 002 cooperative-handler residual, not the non-cooperative one.** Plan 002's
`Run` doc (ADR 0007 D9) already noted: "a handler that ignores its context is still bounded [at shutdown]
... but a handler that never returns at all is bounded only by `WithHandlerTimeout` (Plan 003)." This
decision delivers exactly that: a **cooperative** handler (one that reads `ctx.Done()`) is now bounded by
`d` on every attempt, not just at shutdown. A handler that ignores its context **entirely** (never checks
`ctx.Err()`, e.g. blocked on an uninterruptible syscall) is still only bounded by the worker eventually
returning on its own — `WithHandlerTimeout` cannot preempt a goroutine mid-execution in Go. This residual
is accepted and documented, not hidden (ADR 0007 D9 / this ADR).

**Settle contexts are unaffected.** `handlerContext`'s `parent` is `drainCtx` (dispatch's `ctx` parameter,
per D4's two-context split), never `settleCtx`. A handler timeout cancels only the handler's context; the
subsequent `Ack`/`Nack` in `dispatch` still runs under the untouched `settleCtx`, so a settle is never
aborted merely because its handler ran long.

### D7 — `CircuitBreaker` gates ingress AND dispatch (Task 5)

**Interface.** `CircuitBreaker` is a three-method SPI (`flowcontrol.go`):

- `Allow() bool` — may work proceed now? True when closed or half-open (a probe is admitted), false
  only when open.
- `Record(success bool)` — feed the outcome of an *allowed* dispatch back to the breaker.
- `HalfOpen() <-chan struct{}` — the channel that is **closed** on the next open→half-open transition,
  the explicit wakeup that makes the ingress park missed-wakeup-free. A **fresh channel is minted each
  open cycle**, so a stale (already-closed) subscription only costs a harmless extra loop iteration.
  **Hardened contract (review fix, Task 5 fix pass):** the godoc on `CircuitBreaker.HalfOpen`
  (`flowcontrol.go`) now states this as a HARD REQUIREMENT, not an incidental note — an implementation
  that returns an already-closed channel while `Allow` still reports `false` makes `admitBreaker` busy-spin
  (no clock-driven backoff is added to `admitBreaker` to compensate; see the "Rejected alternatives" note
  below). The default `NewCircuitBreaker` satisfies this by construction: only `toHalfOpen` closes `wake`,
  and it does so in the same critical section that mints the replacement channel.

**Default breaker (`NewCircuitBreaker`, `breaker.go`).** A dependency-free, `clockwork`-driven state
machine: **closed** → `threshold` consecutive failures → **open** (arms `clock.AfterFunc(cooldown,
toHalfOpen)`) → **half-open** on the cooldown (admits probes) → **closed** on a probe success or
**open** on a probe failure (which re-arms the cooldown). Options: `WithBreakerClock` (default real;
tests inject a fake and advance it), `WithBreakerThreshold` (default 5, min 1), `WithBreakerCooldown`
(default 30s, min > 0). `Record(true)` resets the failure count and, from half-open, closes; from open
the state is left to the timer (only `toHalfOpen` leaves the open state). `toHalfOpen` guards on
`state == open` before transitioning — a defensive net against a real-clock fire/Stop race (unreachable
under the deterministic fake clock, so intentionally not unit-covered).

**`Record(true)`-from-open is reachable, not dead code (review fix, Task 5 fix pass).** Under
`WithConcurrency(N>1)` a straggler dispatch admitted just before another worker's failure trips the
breaker can still call `Record(true)` after the breaker is already open — the `if state == breakerHalfOpen`
guard means this zeroes `fails` but does **not** re-close the breaker (an open breaker must stay open
until its own cooldown/probe cycle decides otherwise). `breaker_test.go` pins this via the public API:
open the breaker, `Record(true)`, assert `Allow()` is still `false`.

**Gating BOTH ingress AND dispatch (NF-10).** An open breaker must not keep feeding a dead downstream,
so it gates on two sides (`consumer.go`):

- *Ingress park* — `admit` calls `admitBreaker`, which parks the ingress goroutine while open:
  `for { if Allow() { return true }; wake := HalfOpen(); if Allow() { return true }; select { <-wake;
  <-ctx.Done() → false } }`. The **subscribe-then-recheck** is the missed-wakeup fix: the `wake`
  channel is captured *before* the second `Allow`, so a transition landing between the two checks is
  caught either by the re-check (state already half-open → proceed) or by the already-closed `wake`
  (the `select` returns immediately) — never lost. `ctx.Done` (shutdown) unparks and Nacks the held
  delivery (requeue), releasing nothing (no credit acquired yet).
- *Dispatch gate* — `process` re-checks `Allow()` *after* dequeuing from the worker channel; if open it
  **Nacks the already-buffered message (requeue, releasing its credit) without dispatching**, so the
  surplus waits durably in the source rather than being driven into the failing downstream.

**Health signal — only transient failures trip it (M4).** `dispatch` returns its classification error
so `process` records `Record(err == nil || isPermanent(err))`: a successful Ack **and** any permanent
classification (a decode failure or a `Permanent(err)` handler error — the *message's* fault, diverted
to the invalid sink) are **healthy**; only the two genuinely transient tails (dead-letter and
Nack-redeliver) are **unhealthy**. This closes audit finding M4: a burst of poison messages would
otherwise open the breaker on threshold 1 and stall healthy traffic. `Record` is called only for an
*allowed* dispatch — a message the dispatch gate Nacked never records an outcome.

**gobreaker plug-in.** `sony/gobreaker` is an optional caller-injected implementation, not a dependency:
wrap it so `Allow` mirrors its `State()` (open → false), `Record` drives its success/failure counters,
and `HalfOpen` closes a freshly-minted channel from its `OnStateChange(open→half-open)` callback.

### D8 — Bounded attempt tracker via TTL sweep (Task 7; resolves ADR 0007 §D5)

The `attemptTracker` (ADR 0007 D5) evicts an entry on terminal settle, but never under the default
retry-forever policy — so a source that mints a **distinct id per delivery** and fails
permanently-transiently forever grows the map without bound. Capacity-based eviction is forbidden by
NF-2 (evicting a still-in-flight id resets its poison count and defeats `MaxAttempts`). The resolution
is **time-based**, not capacity-based:

- Each entry gains a `lastSeen time.Time`, refreshed on **every** `observe` (via the injected
  `clockwork.Clock`). Terminal `evict` is unchanged.
- A `sweepLoop` runs a `clock.NewTicker(defaultAttemptTTL)`; on each tick `sweep()` deletes exactly the
  entries whose age `now.Sub(lastSeen) >= ttl`. `defaultAttemptTTL = 5m`.

**NF-2 preserved — the TTL ≫ redelivery-cadence invariant.** An actively-redelivering id is re-observed
on each attempt, so its `lastSeen` is refreshed and its age stays small; the sweep only reclaims ids
that **stopped arriving**. For the only Plan-003 source (`memory`) redelivery is immediate; even a
future delay-parked source re-observes within `≤ Backoff.Max`, which is ≪ 5m. So a message still in
redelivery is never swept mid-flight — only the distinct-id-per-delivery garbage of ADR 0007 §D5 ages
out. The publicly observable effect: an id that reappears **after** a ≥ TTL gap of silence is treated
as attempt 1 again (fresh backoff), whereas one redelivered continuously keeps climbing to
`MaxAttempts`.

**No `ttl <= 0` guard (YAGNI).** `NewConsumer` is the only constructor and always passes the positive
`defaultAttemptTTL`; there is no public opt-out knob. A guard for a non-positive TTL would be
uncoverable dead code under the blackbox coverage gate, so it is deliberately omitted (a knob can be
added non-breakingly later if a need appears).

**Sweep-ticker ordering invariant (C1) — load-bearing for fake-clock test determinism.** The sweep
goroutine runs under a **Run-scoped context detached from the parent** (`context.WithoutCancel(ctx)`),
cancelled explicitly at the very end of `Run` and joined last (on the same `ingressWG` as the ingest
goroutine). It is *not* keyed off the parent `ctx`. Consequence: the sweep ticker is registered on the
consumer clock at `Run` start and **stays registered for `Run`'s entire lifetime**, including the drain
— a stable, always-present waiter. Had the sweep exited on parent-cancel, its ticker would deregister
from the fake clock *concurrently* with the drain registering its `clock.After`, so the waiter count
during the drain would race between 1 and 2 and no fixed `BlockUntilContext(N)` would be safe (it could
return early **or** hang). Keeping it alive makes the sweep ticker a **constant `+1`** every
consumer-fake-clock test accounts for: a test waiting for its own timer to register waits for
`N = 1 (sweep ticker) + (its own expected consumer-clock timers)`. Plan 003 Task 7 bumped every
pre-existing consumer-clock `BlockUntil` by +1 accordingly (the shutdown-drain, handler-timeout, and
breaker-cooldown tests). Tests that `BlockUntil` a *different* clock (the limiter's or breaker's own
fake clock, injected without `WithConsumerClock`) are unaffected.

The sweep goroutine is owned, cancellable, `ticker.Stop()`s on return, and is joined before `Run`
returns — no goroutine leak (proven by the `goleak` `TestMain`).

### D9 — Overflow semantics + streaming mapping (Task 6)

`admit`'s credit acquisition branches on `c.overflow` with an explicit `switch`:

```go
switch c.overflow {
case OverflowDropNewest, OverflowDropOldest, OverflowReject:
        if !gate.tryAcquire() {
                // shed
        }
default: // OverflowBlock and any out-of-range OverflowPolicy
        if err := gate.acquire(ctx); err != nil {
                // backpressure path
        }
}
```

**Unknown → Block, never drop (audit finding I1).** The shed policies are enumerated *explicitly* in
the `case`; the `default` arm — which also catches any out-of-range `OverflowPolicy` value, e.g. a
caller-constructed `OverflowPolicy(99)` — backpressures via the blocking `acquire`. This is the
inverse of the naive shape (`case OverflowBlock: acquire; default: shed`), which would silently *drop*
under an unrecognized value — the opposite of the documented `String()`/D2 contract that an
out-of-range policy renders and behaves as `OverflowBlock` (the safe default). Pinned by
`TestConsumer_Overflow_UnknownPolicy_BlocksNeverDrops`: `OverflowPolicy(99)` backpressures a burst
behind a held handler and every message is eventually processed, with `OnRetry(ErrOverflowDropped)`
never firing.

**`OverflowDropNewest` sheds via `tryAcquire`, genuinely dropped (audit finding I2).** On contention
(`tryAcquire` returns false — no credit acquired, so nothing to leak) the just-arrived delivery is
shed: `Nack(ctx, requeue=false, 0)` plus `OnRetry(ErrOverflowDropped)` for observability, then ingress
keeps reading (the shed does not stop the loop). `requeue=false` is deliberate: for Plan 003's only
source (`adapter/memory`, at-most-once) a `requeue=true` Nack re-`Send`s the message back onto the
broker channel, re-entering the pipeline — a respin, not a drop, defeating the entire point of
load-shedding. So the memory adapter's shed message is genuinely **lost** (at-most-once, as
documented per-adapter). This **supersedes** spec NF-14's at-least-once "redeliver on drop" wording
for Plan 003's memory-only scope: do not read `Nack(requeue=false)` here as "an at-least-once wire
source will redeliver independently" — `requeue=false` explicitly instructs the source *not to*.
Pinned by `TestConsumer_Overflow_DropNewest_ShedsWithoutLeak`: a burst behind a single held credit
sheds the surplus (`dropped >= 1`), the held delivery still completes once released
(`processed >= 1`), and a subsequent send after recovery still flows (`processed >= 2`) — proving the
credit was recycled, not leaked, by the drop path.

**Notes for Plan 006 (deferred).** The redeliver-on-drop semantics for a genuinely at-least-once
source (Redis PEL redelivery, JetStream `Nak`/`Term`) are **not** implemented here and must be
revisited when the first at-least-once source lands: such a source may want its *own* native
nack/requeue semantics on overflow (e.g. `Nack` with a native redeliver flag distinct from this
runtime's `requeue` bool) rather than inheriting the at-most-once memory adapter's "shed = lost"
behavior verbatim. Track this as an open item for the `adapter/redis` / `adapter/nats` plans.

`OverflowDropOldest`/`OverflowReject` are accepted now for API stability but **behave as
`OverflowDropNewest`** on a streaming source: `DropOldest` needs a discrete buffer stage to identify
and evict a victim other than the newest arrival (arrives with the Poller, Plan 004); `Reject` is
HTTP request/response semantics (503/429), which only make sense once `adapter/http` exists. This
mapping is a flagged, documented decision (this ADR + the `OverflowPolicy` godoc), not a silent
approximation.

### D10 — Task 8 whole-branch review gate: fixes and triaged findings

Per CLAUDE.md §5, `/code-review` and `/security-review` ran over the whole-branch diff (`main..HEAD`)
before delivery, using 8 parallel finder angles plus a security pass. The security pass found nothing
(expected — this is a pure in-process resilience layer with no network/SQL/crypto attack surface). The
code-review pass surfaced one **fixed, must-fix** correctness bug (D3's `workerCh` buffering, above) and
several lower-severity findings **explicitly triaged** rather than fixed, per the same gate's "resolve or
triage with written rationale" requirement:

- **Circuit-breaker half-open admits unlimited concurrent probes, not a single canary (converged,
  2 independent finder angles).** `Allow()` returns `true` for the *entire* half-open state; under
  `WithConcurrency(N>1)` combined with `WithCircuitBreaker`, every worker that reaches the dispatch gate
  while half-open is admitted, not just one probe — a textbook breaker limits half-open to a small,
  explicit number of trial requests. **Triaged, not fixed:** this is reachable only under the specific
  opt-in combination `WithConcurrency(N>1)` + `WithCircuitBreaker` (the default is `concurrency=1`, where
  half-open is already a single-probe-at-a-time by construction), so it does not affect the default
  configuration the way the `workerCh` deadlock did. A proper fix (tracking one in-flight probe and
  rejecting concurrent half-open admission until it settles) is a real breaker state-machine change
  deserving its own TDD cycle and tests, not a last-task patch. Tracked as a follow-up for a future
  `breaker.go` revision.
- **Important (tracked) — the TTL-sweep (D8) vs. caller `RetryPolicy.Backoff` invariant is documented
  but not enforced (converged, 3 independent finder angles).** D8 already states the sweep is safe *because* "an actively
  redelivering id is re-observed... within `≤ Backoff.Max`, which is ≪ 5m" — but nothing validates that a
  caller's own `RetryPolicy.Backoff` actually satisfies that inequality. A caller who configures
  `ExponentialBackoff` with `Max <= 0` (uncapped, per `backoff.go`'s own doc) or `Max` at/above
  `defaultAttemptTTL`, together with `MaxAttempts > 0`, can hit a redelivery gap ≥ 5 minutes; the sweep
  then evicts the entry and the next redelivery restarts at attempt 1 — silently defeating that
  `MaxAttempts`/dead-letter ceiling. **Triaged, not fixed:** `RetryPolicy.Backoff` is the `BackoffStrategy`
  *interface*, not just the shipped `ExponentialBackoff`, so a fully general construction-time check is not
  tractable (an arbitrary custom strategy's growth curve cannot be statically bounded); a narrower check
  against only the concrete `ExponentialBackoff` type would leave custom implementations uncovered while
  adding asymmetric validation surface late in the plan. The invariant already holds trivially for Plan
  003's only source (`memory`, immediate redelivery); the risk is forward-looking (a caller pairing a large
  custom `RetryPolicy` with a future wire adapter). Tracked as a follow-up: either expose a
  `WithAttemptTTL` escape hatch (reversing decision 7's YAGNI call) or add a best-effort `ExponentialBackoff`-only
  guard, whichever a concrete future need favors.
- **Important (tracked) — new SPI extension points (`RateLimiter.Wait`, `CircuitBreaker.Allow`/`Record`/
  `HalfOpen`) are not panic-recovered like the handler/hooks are.** `safeHandle`/`safeFire` (`consumer.go`)
  exist precisely so a panicking *caller-supplied* callback cannot take down a worker; the new plug-in
  SPIs are equally caller-supplied (the `x/time/rate`/`sony/gobreaker` adapters are the documented path,
  D5/D7) but are invoked directly. **Triaged, not fixed:** the shipped defaults (`NewTokenBucket`,
  `NewCircuitBreaker`) never panic (pure mutex-guarded arithmetic), so this is a defense-in-depth gap
  against a *misbehaving custom implementation*, not a defect in normal operation — unlike the `workerCh`
  deadlock, which was reachable through the default configuration with the shipped adapter. Wrapping
  three new methods with well-chosen panic-to-fallback semantics (what does a panicking `Allow()` safely
  default to?) is a real design decision, not a mechanical copy of `safeFire` — recovering a
  control-flow SPI call needs a deliberate fail-open/fail-closed fallback policy, unlike fire-and-forget
  hooks where "log and continue" is always safe — and deserves its own consideration rather than a
  rushed guess in this closing task. As of the whole-branch-review gate-fixes pass, the `RateLimiter` and
  `CircuitBreaker` interface godocs (`flowcontrol.go`) now state the no-panic contract as a hard
  requirement (doc-only; no recover logic added). **Tracked as a dedicated follow-up: parity with the
  Plan-002 `safeFire` recovery, to be fixed in a dedicated cycle before the first non-default
  `RateLimiter`/`CircuitBreaker` SPI plug-in ships.**
- **Fixed in this task (low-risk, high-value, done rather than triaged):** (1) `TestOverflowPolicy_String`
  used a `want string` table field instead of the mandated `assert` closure (CLAUDE.md's testing rules) —
  converted. (2) `WithClock` (`message.go`) and `WithConsumerClock` (`consumer.go`) — pre-existing Plan
  001/002 options, not part of this plan's diff — had no nil guard, so `WithClock(nil)` /
  `WithConsumerClock(nil)` would install a nil `clockwork.Clock` that panics on first use (`New`'s
  `cfg.clock.Now()`, `Run`'s `c.clock.After(...)`), violating the "no panic on caller input" constraint.
  This diff's own new clock options (`WithBreakerClock`, `WithTokenBucketClock`) already nil-guard; the two
  older options were brought into line for consistency and safety, with a regression test each
  (`TestNew_StampsIDAndTimestamp`'s new "nil clock is a no-op" case;
  `TestConsumer_WithConsumerClockNil_IsNoOpNotPanic`).

## Consequences

**Positive**
- The flow-control surface is available and validated from Task 1 onward, so later tasks only *wire*
  behavior into already-shaped, already-tested config — mirroring Plan 002's proven "widen then wire"
  pattern, keeping the suite green throughout.
- The C2 unset-vs-explicit-0 distinction closes a real caller-error gap: without the `maxInFlightSet`
  flag, `WithMaxInFlight(0)` would be silently defaulted to 1024 instead of rejected.
- Nil-safe `WithRateLimit`/`WithCircuitBreaker` and a closed, safely-defaulting `OverflowPolicy` mean
  the new options cannot be misused into a panic or a silent unbounded-drop at construction time.
- The mandatory whole-branch review gate (D10) earned its keep: it caught a genuine, confirmed,
  ~40-50%-reproducible deadlock in the *default* consumer configuration before it shipped — exactly the
  class of bug a single-message-focused test suite (Tasks 1–7's tests each used at most 1–2 messages in
  flight) would not surface. A permanent regression test now guards it at `-count=20`.

**Negative / costs**
- D10's triaged (not fixed) findings — half-open probe concurrency under `WithConcurrency(N>1)`, the
  unenforced TTL-vs-Backoff invariant, and un-recovered panics from custom `RateLimiter`/`CircuitBreaker`
  plug-ins — are real, documented gaps carried forward rather than closed in Plan 003. None are reachable
  through the shipped defaults/adapters in this plan's scope, but each deserves its own fix with proper
  tests in a follow-up rather than a rushed change to the plan's final task.
- A living multi-decision ADR (like ADR 0007) requires discipline to keep each stub in sync as its task
  lands — mitigated by fleshing out each `D3`–`D9` stub in the same commit as its implementing task.
- `OverflowDropOldest`/`OverflowReject` ship now as accepted-but-approximated (mapped to
  `OverflowDropNewest`) ahead of the infrastructure that would give them true semantics; this is
  documented, not silent, but is still an API-vs-implementation gap until Plan 004/`adapter/http` land.

**Rejected alternatives**
- **Wire the runtime behavior in Task 1 alongside the config surface** — rejected: it would couple the
  config-validation TDD cycle to the much larger credit-gate/breaker/overflow runtime changes, breaking
  the same "widen then wire" incremental-green pattern that kept Plan 002 reviewable task-by-task.
- **Let `WithMaxInFlight(0)` mean "unbounded"** — rejected outright: spec §7.4.1 and CLAUDE.md's
  production-robustness constraints require the credit gate to be **always** finite; there is no
  "unbounded" escape hatch to express.
- **Add a clock-driven backoff to `admitBreaker` as a defense against a non-conforming custom
  `CircuitBreaker`** (review fix, Task 5 fix pass) — rejected: it would register an extra timer on the
  consumer clock, changing the fake-clock `BlockUntil` counts several existing breaker/shutdown tests
  rely on (the C1 shutdown-bound hazard). Instead the fix is doc-only: `CircuitBreaker.HalfOpen`'s godoc
  now states the no-already-closed-channel contract as a hard requirement, and the default
  `NewCircuitBreaker` is verified to satisfy it by construction.
