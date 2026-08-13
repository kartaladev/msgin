# ADR 0007 — Reliability & settlement API

- **Status:** Accepted (2026-07-17) · **D7 amended 2026-07-28 by decision D-N, and again 2026-07-30 by
  decision D-P** — see the amendment notes in D7 below. The amendments are recorded here rather than by
  superseding this ADR: D7's reasoning stands unchanged for the case it weighed (discard vs. keep-retrying);
  D-N adds a case it could not have weighed, created by decision **D-M** in
  [ADR 0029 §5.0b](0029-eip-lexical-alignment.md), and **D-P makes D-N's fallback single-shot** so that a
  **down** fallback sink falls through to D7's discard instead of re-creating the infinite-retry trap D7's own
  sentence rejects. **D7 amended a third time 2026-08-09** — `ErrPayloadTooLarge` is **exempt** from D-N's
  dead-letter fallback, so a byte cap's rejects are never persisted into the operator's durable store.
- **Context source:** [Spec 001 — Messaging core](../specs/001-messaging-core.md) §5–§8;
  [Plan 002 — Reliability](../plans/002-reliability.md)
- **Amended by:** [ADR 0029 §5.0b](0029-eip-lexical-alignment.md) (D-N and D-P, D7 only) ·
  **Spec:** [014 §2.1](../specs/014-core-package-layout.md) · **Plan:** [027](../plans/027-core-package-layout.md)
  Task 9.7
- **Related:** [ADR 0002 — Adapter SPI](0002-adapter-spi.md) (runtime-owned reliability, `Delivery`/
  `Ack`/`Nack`, `NativeReliability` escape hatch), [ADR 0004 — clockwork](0004-clockwork-dependency.md),
  [ADR 0005 — cenkalti/backoff](0005-cenkalti-backoff-dependency.md) (backoff placement)

## Context

Plan 001 shipped the messaging core (`Message[T]`, the adapter SPI, `Producer`/`Consumer`) with a
minimal dispatch loop. Plan 002 builds the **reliability engine** on top of it: permanent-vs-transient
error classification, retry backoff, attempt counting, dead-lettering, invalid-message routing,
observability hooks, and a bounded graceful shutdown. These decisions cut across many of Plan 002's
tasks, so they are recorded here as one ADR with per-decision (`D1`–`D10`) sections, each authored (or
fleshed out from a one-line stub) in the plan task that realizes it. This ADR is **living**: Task 1
writes `D1`–`D2` in full and stubs `D3`–`D10`; later tasks flesh out their stub in the same commit
that implements the decision.

Spec 001 §5 says a handler error is permanent when "wrapped with `backoff.Permanent`, or a permanent
sentinel." That phrasing assumed `cenkalti/backoff`'s marker would be reused directly. Implementing
Task 1 surfaced a conflict with the dependency policy (see Decision D1 below), so this ADR **refines**
that spec wording rather than following it verbatim — the spec is updated in the same commit to
cross-reference this ADR.

## Decision

### D1 — Permanent marker: `msgin.Permanent`, not `backoff.Permanent` (Task 1)

msgin marks a handler error as permanent (non-retryable) with its **own** wrapper: an unexported
`*permanentError` produced by the exported `func Permanent(err error) error`, detected via
`errors.As`. `Permanent(nil)` returns `nil` (a nil-in/nil-out identity, matching the rest of the
error-marking surface); `Permanent(err)` returns a value that remains `errors.Is`-comparable to
`err` through `Unwrap`, including after further `fmt.Errorf("...: %w", ...)` wrapping.

We deliberately do **not** import `cenkalti/backoff` to reuse `backoff.Permanent`. ADR 0005 (refined
by spec §8) places `cenkalti/backoff/v4` on the **outbound HTTP adapter's tight retry loop**
(`adapter/http`), not the runtime's closed-form redelivery path — the redelivery backoff is a
stateless computation (`BackoffStrategy.Delay(attempt)`, D4) that does not use `cenkalti` at all.
Importing `cenkalti` into the **core** module just to obtain a `Permanent` marker would pull a
non-stdlib, non-`clockwork` dependency into every consumer of the core, ahead of any actual need for
it there, contradicting the stdlib+clockwork constraint that governs the core module (CLAUDE.md →
Dependency policy). A native marker is a handful of lines and keeps the core's dependency graph
exactly as small as the constraint requires.

This decision **refines** spec §5's "`backoff.Permanent(err)`" phrasing to "`msgin.Permanent(err)`";
the spec is updated (in the same commit as this ADR) with a cross-reference note so the two documents
do not silently disagree.

In addition to the explicit marker, two construction-time/decode-time failures are also classified
permanent without needing `Permanent(...)`: `ErrPayloadType` (a `Message[any]` payload that cannot be
asserted to `T`) and `ErrPayloadDecode` (a wire payload that cannot decode into `T`). Both indicate the
message itself is malformed, not that the handler failed transiently — retrying will never succeed, so
they route straight to the invalid-message sink (§6/§7) rather than consuming retry attempts.

### D2 — `ErrHandlerPanic` is transient, not permanent (Task 1)

`ErrHandlerPanic` is a new public sentinel (`errors.New("msgin: handler panicked")`) that wraps the
value recovered from a panicking handler. It is deliberately classified **transient**: it is *not*
passed through `Permanent(...)`, so a panicking handler is retried per the configured `RetryPolicy`
like any other transient failure, rather than being diverted straight to the invalid-message sink.

The rationale: a panic is evidence the *handler* failed unexpectedly for this invocation (a nil
dereference on a transient nil dependency, a slice index bug triggered by unusual but not necessarily
malformed input, etc.) — it is not proof the *message* is unprocessable. Treating every panic as
permanent would risk quietly discarding messages that a subsequent, non-panicking attempt (or a fixed
downstream dependency) could still process correctly. Retrying preserves the existing
`MaxAttempts`/dead-letter safety net (D3): a message whose handler panics every single time still
terminates via the normal exhausted-retries path instead of looping forever, because `ErrHandlerPanic`
participates in the same attempt-counting as any other transient error.

### D3 — `RetryPolicy` zero-value + default semantics (Task 3; revised for C1)

`RetryPolicy{ MaxAttempts int; Backoff BackoffStrategy; DeadLetter OutboundAdapter }` is the
consumer's per-flow settlement configuration (spec §7). Its **zero value** — `{0, nil, nil}` —
validates OK and means "retry forever, immediately, no DLQ": exactly Plan 001's original requeue
behavior, so a `NewConsumer` call that omits `WithRetryPolicy` keeps working unchanged.

`Validate()` checks, in this order:

1. `MaxAttempts < 0` → `ErrInvalidMaxAttempts`. A negative attempt count has no meaning, so it is
   rejected before anything else.
2. `MaxAttempts > 0 && DeadLetter == nil` → `ErrNoDeadLetter`. A **finite** policy commits to giving
   up after that many attempts, and a message that has exhausted retries has to go *somewhere* —
   silently dropping it would violate the "no lost messages beyond the documented guarantee"
   robustness constraint. A nil `DeadLetter` under a finite `MaxAttempts` is therefore a construction
   error, not a runtime one.
3. Otherwise `nil`. This covers both `MaxAttempts == 0` (retry-forever — a `DeadLetter` is irrelevant
   and never required, so one may be set or left nil without effect) and `MaxAttempts > 0` with a
   non-nil `DeadLetter` (the well-formed finite case).

`Backoff == nil` is valid in every case above and means **immediate** redelivery (zero delay) — it is
never itself a validation failure, only ever combined with the `MaxAttempts` checks. `Validate()` is
exported (rather than enforced only inside a constructor) so callers can pre-check a policy
standalone and so `NewConsumer` surfaces a typed construction error immediately (spec §5) instead of
failing later, mid-flow, the first time a message actually exhausts its attempts.

Note (C1): the retry-forever *redelivery* default does not extend to shutdown — see D9.

### D4 — Backoff indexing and shape (Task 2)

*Stub — fleshed out in Task 2.* `BackoffStrategy.Delay(attempt int) time.Duration` is 0-based (0 =
first retry). The default `ExponentialBackoff` computes `min(Max, Initial × Mult^attempt)` with guards
for non-positive/overflow inputs, plus optional non-deterministic jitter (`RandomizationFactor`); exact
tests set `RandomizationFactor = 0`. Stateless and `clockwork`-agnostic — pure computation, no clock.

### D5 — Attempt counting (Tasks 4/6)

*Stub — fleshed out in Task 4/6.* Prefer a source's native `msgin.delivery-count` header (must be
stored as a Go `int`, not `int64`); otherwise the runtime tracks attempts in-process via an
`attemptTracker` keyed by message id, evicted only on terminal settle.

#### Known limitation — `attemptTracker` unbounded growth under retry-forever (RESOLVED by Plan 003 / ADR 0008 D8)

The id-keyed `attemptTracker` evicts an entry **only on terminal settle** (Ack / dead-letter / invalid
divert), because NF-2 forbids capacity-based eviction while a message is still being redelivered — a
mid-flight eviction would reset a poison message's attempt count and defeat `MaxAttempts`. A logical
consequence: under the **default retry-forever policy** (`RetryPolicy{}`, `MaxAttempts == 0`), a
transient failure never terminally settles, so its tracker entry is never evicted. With unboundedly many
**distinct** message ids that each fail permanently-transiently forever, the map grows without bound.

This is **not reachable by any Plan-002 shipped adapter.** The only source wired in Plan 002 is
`memory`, whose tracker key space is bounded by the finite set of produced messages — a Nack-requeue
re-enqueues the *same* id (not a fresh one), so redelivery reuses the existing entry rather than adding a
new one. Triggering unbounded growth requires a future **wire streaming source** (e.g. `redis`/`nats`,
Plan 006+) that mints a distinct id per delivery, run under retry-forever with a permanently-failing
handler.

**Resolved by Plan 003, Task 7** ([ADR 0008 D8](0008-resilience-flow-control-api.md#d8--bounded-attempt-tracker-via-ttl-sweep-task-7-resolves-adr-0007-d5)).
The tracker now carries a `lastSeen` timestamp per entry (refreshed on every `observe`) and a
Run-lifetime `clockwork` ticker sweeps entries idle for ≥ `defaultAttemptTTL` (5m). This bounds the map
while **preserving NF-2**: an actively-redelivering id is re-observed each attempt (gap ≤ `Backoff.Max`
≪ 5m), so it is never swept mid-flight — only ids that stopped arriving (the distinct-id-per-delivery
garbage above) age out. We deliberately did **not** add capacity-based eviction (which could only
violate NF-2 if it fired); TTL sweep evicts strictly on *idleness*, never under pressure. See ADR 0008
D8 for the full design, the NF-2 argument, and the sweep-ticker ordering invariant.

### D6 — Observability `Hooks` shape (Task 4)

`Hooks` is a plain struct of four independently optional callbacks, all sharing one signature —
`func(ctx context.Context, msg Message[any], err error)` — for `OnRetry`, `OnDeadLetter`,
`OnInvalidMessage`, and `OnAck`. A single shared signature (rather than one bespoke signature per
event) keeps the surface small and lets a caller wire the same instrumentation function to several
hooks if it only cares about "something happened to this message" rather than which event. `err`
carries the triggering failure (the handler error for `OnRetry`/`OnDeadLetter`, the decode/permanent
error for `OnInvalidMessage`) and is `nil` on a plain `OnAck` — there is no failure to report on a
clean success.

`Hooks{}` (the zero value, all four fields `nil`) is the default and is valid: every hook is optional.
Internally, the runtime never calls a `Hooks` field directly; it goes through an unexported `fire(hook,
ctx, msg, err)` helper that no-ops when `hook == nil`. Centralizing the nil-check in one helper (rather
than repeating `if h.OnX != nil { h.OnX(...) }` at every call site in Task 5/6's settlement switch)
means the nil-safety guarantee has exactly one place to get right and one place to test, instead of
four duplicated call sites that could individually regress.

### D7 — No-invalid-sink policy (Tasks 4/5)

`WithInvalidMessageSink` is optional. When unset (`invalidSink == nil`), a permanent/undecodable
message is not retried forever and not silently dropped: the runtime logs it at WARN via the injected
`*slog.Logger`, fires `OnInvalidMessage`, and `Ack`s the original delivery (removing it from the
source) rather than leaving it to loop through the redelivery path. An absent sink deliberately does
not fall back to "keep retrying" — for a `Permanent`/decode error, retrying can never succeed (D1), so
retrying anyway would only convert a configuration gap (no sink configured) into an infinite-retry
trap, which is worse than a logged, observable discard. Task 5 implements the call site; this decision
fixes the *policy* so Task 5 has nothing left to decide.

> **AMENDED 2026-07-28 by decision D-N** — [ADR 0029 §5.0b](0029-eip-lexical-alignment.md#50b-the-two-sentinels-are-not-symmetric-and-a-deterministic-fault-needs-a-retry-classification--decisions-d-k-revised-and-d-m),
> [Spec 014 §2.1](../specs/014-core-package-layout.md#21-the-deliberate-behavior-changes-the-register), realized
> by [Plan 027](../plans/027-core-package-layout.md) Task 9.7. **The discard is no longer the immediate
> fallback: the dead-letter sink is tried first.**
>
> When `invalidSink == nil` **and** `policy.DeadLetter != nil`, the invalid-message divert routes to the
> **DeadLetter sink**. The discard above remains the terminal behavior only when **neither** sink is
> configured. `OnInvalidMessage` still fires in both cases — the hook reports the *classification*, the sink is
> only the *destination* — and the fallback is announced with its own WARN naming both facts (no invalid-message
> sink configured; message sent to the dead-letter sink instead).
>
> **Why D7's original reasoning does not cover this case.** D7 weighed *discard* against *keep retrying* and
> chose discard, correctly — but it never weighed *discard* against *a durable sink the caller already
> configured*, because when it was written no permanent fault could arrive at a consumer that had a DeadLetter
> and no invalid sink. Decision **D-M** creates exactly that arrival: it reclassifies deterministic endpoint
> faults (`ErrNilFunc`, `ErrNilSink`) as `Permanent`, which moves them off the retry-exhaustion path that had
> been depositing them in the DeadLetter sink. Measured, one message,
> `RetryPolicy{MaxAttempts: 3, DeadLetter: dlq}`, no `WithInvalidMessageSink`:
>
> ```
> BEFORE D-M       bare ErrNilFunc         OnRetry=2 OnDeadLetter=1 OnInvalidMessage=0 | dlqSink=1 discarded=false
> AFTER  D-M       Permanent(ErrNilFunc)   OnRetry=0 OnDeadLetter=0 OnInvalidMessage=1 | dlqSink=0 discarded=true
> AFTER  D-M + D-N Permanent(ErrNilFunc)   OnRetry=0 OnDeadLetter=0 OnInvalidMessage=1 | dlqSink=1 discarded=false
> ```
>
> Without D-N a message this library previously captured **durably** would start being **dropped**. That is not
> a corner case: a finite `MaxAttempts` **requires** a `DeadLetter` (`RetryPolicy.Validate`, `retry.go:46-53`)
> while `WithInvalidMessageSink` is optional and unset by default, so "DeadLetter present, invalid sink absent"
> is the shape of every finite-retry consumer that has not opted in. CLAUDE.md: *"When a wrong default could
> silently corrupt (… lose data …), pick the value that fails safe."*
>
> **Scope:** both invalid-message divert call sites — the decode-failure path and the permanent-handler-error
> path. The decode arm's change (discard → dead-letter) is a behavior change in its own right and a strict
> improvement over the discard; it is recorded rather than inherited silently.
>
> ---
>
> **AMENDED AGAIN 2026-07-30 by decision D-P — the fallback is SINGLE-SHOT** ([round 8 §1](../plans/027-audit-round-8.md),
> [ADR 0029 §5.0b](0029-eip-lexical-alignment.md), [Spec 014 §2.1](../specs/014-core-package-layout.md#21-the-deliberate-behavior-changes-the-register)
> row 7, realized by [Plan 027](../plans/027-core-package-layout.md) Task 9.7). **When the fallback target's
> `Send` fails, this decision's discard is what happens next.**
>
> The amendment above said only *where* the message goes. It never said what happens when that target is
> **down** — and D8's send-failure arm (`endpoint/consumer.go:774-782`) `Nack`s with `requeue=true`. A
> **permanent** message therefore re-entered the flow, forever, and **every bound this library owns is
> structurally blind to that loop**:
>
> - **MaxAttempts** — never consulted. The permanent arm deliberately bypasses `c.attempts(d)` (M8) and passes
>   `attempt = 1`.
> - **Backoff** — `retryDelay(policy, 1)` is `p.Backoff.Delay(0)` (`endpoint/consumer.go:948-953`), the *first*
>   step, on every iteration. It never escalates.
> - **Circuit breaker** — `endpoint/consumer.go:614` records `err == nil || msgin.IsPermanent(err)` as
>   **healthy**, so the breaker is told the flow is fine while the message spins.
>
> Measured in round 8 in the default configuration, against a dead-letter sink whose `Send` returns an error:
>
> ```
> BEFORE D-N: deliveries=1   acks=1  nacks=0   dlqSends=0   OnInvalid=1  OnDeadLetter=0  OnRetry=0
> AFTER  D-N: deliveries=41  acks=0  nacks=40  dlqSends=40  OnInvalid=0  OnDeadLetter=0  OnRetry=40
> ```
>
> (41 was the harness's redelivery cap, reached in under 10 ms. The loop is **unbounded** in reality.)
>
> **This is the trap D7's own sentence rejects,** verbatim: *"retrying anyway would only convert a
> configuration gap (no sink configured) into an infinite-retry trap, which is **worse** than a logged,
> observable discard."* D-N re-created it because its note argued only the discard-vs-durable-sink axis and
> never re-weighed the axis D7 had actually decided.
>
> **The amended behavior, stated in full.** The scope is the **whole invalid-message path** — every divert of
> a message classified permanent, whatever sink it targets — not only the `invalidSink == nil` case that
> exposed the defect:
>
> 1. `invalidSink != nil` → send there. On success: fire `OnInvalidMessage`, `Ack`.
> 2. `invalidSink == nil` and `policy.DeadLetter != nil` → send THERE instead (**D-N**). On success: fire
>    `OnInvalidMessage`, `Ack`. This is **one** attempt — there is no second.
> 3. **Either** of those `Send`s **fails** → **do not `Nack`.** Log a WARN naming **both** the classification
>    cause *and* the sink error, fire `OnInvalidMessage`, and `Ack` — i.e. fall through to this decision's
>    discard (**D-P**). The single shot applies to a **configured** invalid-message sink exactly as it does to
>    the D-N fallback.
> 4. Neither sink configured → the original D7 discard, unchanged.
>
> **Why arm 3 covers the configured sink too.** The reasoning that forbids the requeue is a property of the
> *message*, not of *which sink* refused it. The invalid path deliberately never consults the attempt tracker
> (M8), so a `Nack` here is invisible to `MaxAttempts`, invisible to the circuit breaker (`IsPermanent`
> records **healthy**), and — with the default `nil` `Backoff` — hot-spins; worse, the redelivery holds its
> `WithMaxInFlight` credit indefinitely, so a down invalid sink starves **valid** traffic. A permanent message
> cannot succeed on redelivery, so the loop buys nothing to pay for that.
>
> **The accepted cost, stated plainly:** a *transient* outage of a **configured** `WithInvalidMessageSink`
> discards that window's invalid messages rather than holding them until the sink recovers. This is a
> disclosed limitation, not an oversight — it is the price of the termination guarantee, and it is paid
> loudly: every discard emits a WARN naming the message id, the classification cause and the sink error, and
> fires `OnInvalidMessage`. An operator who must not lose them should point `WithInvalidMessageSink` at a sink
> that is durable-on-write (a local spool, an outbox table) rather than at a remote service whose availability
> the consumer cannot assume.
>
> **The one exception to arm 3 — a `Send` that fails only because the settle context is ALREADY cancelled**
> (added by the Task 9.7 code review; `divertTerminal`'s `ctx.Err() != nil` branch). `ctx` there is `Run`'s
> `settleCtx`, which `drainWorkers` cancels when the **shutdown deadline** expires (D9/C1). Once that happens
> a **healthy** sink's `Send` returns an error too — its context was cancelled — and that error is
> indistinguishable at the call site from a refusal. Routing it into arm 3 would `Ack` a message the sink may
> never have received, and because a `Delivery.Ack` is free to ignore its context (`adapter/memory`'s is
> `func(context.Context) error { return nil }`) that `Ack` **succeeds** and the tracker evicts: the message is
> lost with the sink perfectly healthy. So this arm **`Nack`s with requeue**, fires **no** terminal hook, and
> returns *not settled* (the tracker entry is kept) — matching every other ctx-done settle arm in the consumer
> (see the I4 note on `admit`).
>
> **This does not weaken D-P.** In normal operation `ctx.Err()` is `nil`, so the single shot is exactly as
> stated in arms 1–4; the `Nack` is reachable only while the consumer is already shutting down, a window D9
> bounds by construction, so it cannot loop. The unbounded-redelivery trap D7 rejects needs a *live* consumer
> to spin in, and this arm exists only when there isn't one.
>
> D-N's gain (a reachable dead-letter sink captures the message) is kept; D7's guarantee (**an invalid message
> always terminates**) is not surrendered. The rejected alternative — threading a real attempt count into the
> invalid-path `divert` to bound the loop — is more code and more state, and still spends retry budget on a
> fault that cannot resolve.
>
> **This does NOT change D8** for the dead-letter and transient paths: a `divert` whose sink is
> `c.policy.DeadLetter` (`endpoint/consumer.go:726`) still `Nack`s with backoff on send failure, because that
> message is retryable and requeueing it is not a loop. The single-shot rule is specific to the **invalid**
> path, whose messages are permanent by classification.
>
> **The class this violated — record it, not just the instance:** *a settlement path that is terminal by
> construction must not become non-terminal without a bound.* Before D-N the no-invalid-sink path had exactly
> one outcome (Ack); D-N gave it a retry loop that no counter, no backoff and no breaker could observe. Any
> future amendment that adds a `Send` to a terminal path must state what happens when that `Send` fails.
>
> **Operational disclosure (D-N/D-P consequence):** a dead-letter sink now receives **two operationally
> distinct classes** — retries-exhausted (may be replayable) and permanently-invalid (replaying is pointless) —
> and msgin stamps **no settlement-reason header**, so the distinction does not survive the process. See
> [ADR 0029 §5.0b](0029-eip-lexical-alignment.md) (decision D-N's Consequences) for the full record and the
> remedy available to operators today.
>
> ---
>
> **AMENDED A THIRD TIME 2026-08-09 — `ErrPayloadTooLarge` is EXEMPT from D-N's fallback** (whole-branch
> `/code-review` of Task 9.5; [Spec 014 §2.1](../specs/014-core-package-layout.md#21-the-deliberate-behavior-changes-the-register)
> row 7). **Arm 2 above (`invalidSink == nil` → DeadLetter) does not apply to the byte-cap reject.**
>
> `endpoint.WithMaxPayloadBytes` (ADR 0009 D5) caps a wire payload's length *before* the codec sees it,
> specifically to bound **memory and storage** consumed by **untrusted** input. D-N routes every invalid
> message to `RetryPolicy.DeadLetter` when no invalid sink is configured — and "DeadLetter set, invalid sink
> unset" is, by D-N's own argument above, **the default shape of every finite-retry consumer**. The composition
> of the two is that an attacker posting over-size bodies to a capped consumer gets each rejected payload
> written **verbatim** into the operator's durable dead-letter store. **The defence becomes the vector**: the
> cap stops the decode-time allocation it was written for and then pays for it with unbounded durable writes,
> which is the larger of the two costs it was protecting.
>
> | configuration | oversize (`ErrPayloadTooLarge`) | every other permanent class |
> |---|---|---|
> | no invalid sink | **WARN + `Ack` (discard)** — the pre-D-N behavior | dead-letter fallback (D-N, unchanged) |
> | invalid sink set | invalid sink (unchanged — the operator opted in) | invalid sink (unchanged) |
>
> **Why this does not violate D-N's premise.** D-N exists so that *"no configuration that previously captured a
> message starts dropping it"* — its measured table above shows a fault moving **off** the retry-exhaustion path
> that had been depositing it in the DeadLetter sink. `ErrPayloadTooLarge` was never on that path: `IsPermanent`
> has always been true for it (`reliability.go`), so before D-N it was **discarded** under this decision's
> original text, not captured. Exempting it **restores** the prior behavior for this class rather than losing
> anything D-N promised. The exemption is scoped to the *fallback*, not to the class: a caller who sets
> `WithInvalidMessageSink` has deliberately asked for invalid messages and still receives over-size ones there.
>
> The discard stays **loud** — a WARN naming the message id and the classification, `OnInvalidMessage` fires,
> the delivery is `Ack`ed — and its WARN is distinct from arm 4's, because a DeadLetter sink may well be
> configured on this arm and a message claiming none was would send the operator to the wrong option.
>
> **The class, not the instance:** *a defence that bounds a resource must not be routed into a path that
> re-spends that resource.* Any future settlement arm added downstream of a cap, quota or filter must state
> what it does with the rejects, or it will re-create this.

### D8 — `divert` settlement contract (Task 5)

*Stub — fleshed out in Task 5; **scoped to `divert` in Task 9.7**, see the amendment below.* `divert` — the
**dead-letter** settlement helper (`endpoint/consumer.go`, `func (c *consumer[T]) divert`) — acks the original
delivery only if the target sink accepts the message; otherwise it fires the relevant hook and `Nack`s the
original (never Ack-and-lose). A send-failure `Nack` carries a non-zero backoff delay (not `0`) so a down sink
is not hammered.

> **Amended by D-P (Plan 027 Task 9.7) — this contract is `divert`'s alone.** As originally written, D8 spoke
> for *both* settlement helpers, because there was only one: the same `divert` served the invalid-message path
> and the dead-letter path. Task 9.7 split them, and **`divertTerminal` — the invalid-message path — does NOT
> follow the sentence above**. There, a failed `Send` is settled TERMINALLY: WARN + `OnInvalidMessage` + `Ack`,
> single-shot, never a `Nack` (see D7's amendment, arm 3). "Never Ack-and-lose" holds only where the message is
> **transient by classification** — a retry-exhausted message *can* succeed on redelivery, so requeueing it is
> not a loop; a **permanent** one cannot, so requeueing it is exactly the unbounded trap D7 rejects.
>
> Read D8 as: **`divert` (dead-letter) → Nack-with-backoff on send failure. `divertTerminal` (invalid) →
> discard, loudly, on send failure.** The dead-letter contract above is otherwise unchanged.

### D9 — Shutdown is always finite (Task 7; revises the earlier "wait fully" default — C1)

`Run`'s drain is **always finite** and `Run` **always returns**. On parent-cancel the source's
`Stream` returns and the delivery channel is closed; the worker pool drains it under two contexts,
both **detached from the parent** (`context.WithoutCancel`) yet **cancellable**:

- `settleCtx, cancelSettle` — carries the settle closures (`Ack`/`Nack`/divert `Send`) so they survive
  the parent cancel and can still reach a durable backend while the flow drains.
- `drainCtx, cancelDrain` — carries the **handler**, so an in-flight handler finishes gracefully on
  shutdown rather than being cut off by the parent cancel.

The drain then waits on the injected clock for at most a finite timeout: `WithShutdownTimeout(d)` with
`d > 0`, else `defaultShutdownTimeout` (30s). `d <= 0` no longer means "wait fully" — an unbounded
drain violates the "graceful shutdown within a deadline" robustness constraint **and** re-introduces
the C1 deadlock, so there is no infinite-wait path.

**C1 — why `settleCtx` must be cancellable.** The default `memory` adapter's `Nack(requeue=true)` calls
`Send(settleCtx, m)`, which blocks on a channel the now-stopped `Stream` no longer reads. The earlier
Task 7 shape used a bare, *uncancellable* `context.WithoutCancel(ctx)` for settles, so a single
transient failure during drain blocked a worker forever → `wg.Wait()` never returned → `Run` never
returned → goroutine leak (and this deadlocked even the old `d <= 0` "wait fully" path). The fix: on
deadline expiry the drain calls **both** `cancelDrain()` (cooperative handlers observe `ctx.Err()`,
classified transient → Nacked) **and** `cancelSettle()` (any settle stuck on a non-accepting backend
returns `ctx.Err()`, `finish` proceeds, the worker exits), then joins the pool. This guarantees `Run`
returns with no goroutine leak for **every** adapter, memory included. A buffered delivery the deadline
caught before dispatch is **short-circuit Nacked** (`if drainCtx.Err() != nil`) without entering the
handler. Limitation: a handler that *ignores* its context is still bounded (the deadline+`cancelSettle`
release its subsequent settle), but a handler that **never returns at all** is bounded only by
`WithHandlerTimeout` (Plan 003).

### D10 — `WithConsumerClock` naming (Task 4)

The consumer's clock-injection option is named `WithConsumerClock[T any](c clockwork.Clock)
ConsumerOption[T]`, not `WithClock`. `message.go` already exports a `MessageOption` named `WithClock`
(for stamping a message's timestamp from an injected clock); both options are generic functional
options returning different option types (`ConsumerOption[T]` vs `MessageOption`), so a same-named
`WithClock` in both would not be a compile error, but it would be a footgun at call sites and in
godoc search — a reader (or an IDE's autocomplete) sees two same-named top-level functions with
unrelated purposes and no textual cue which one binds where. `WithConsumerClock` costs nothing (it is
called once per `NewConsumer`, not repeatedly) and removes the ambiguity outright. The default,
absent this option, is `clockwork.NewRealClock()` — consistent with `message.go`'s own default and
with ADR 0004.

## Consequences

**Positive**
- The core module stays exactly stdlib + `clockwork` (Task 1); `cenkalti/backoff` is confined to
  `adapter/http` as ADR 0005 already intended — no dependency creep triggered by the reliability work.
- `Permanent`/`ErrHandlerPanic` give callers a small, explicit, debuggable vocabulary for error
  classification: `errors.Is`/`errors.As` work through arbitrary wrapping, and the panic-is-transient
  choice means a flaky handler failure never silently and permanently discards a message.
- Recording all ten decisions under one ADR keeps the reliability engine's design traceable to a single
  document that grows with the plan, instead of ten disconnected micro-ADRs.

**Negative / costs**
- This ADR is authored incrementally across Plan 002's tasks; between tasks it necessarily contains
  stubs for not-yet-implemented decisions. A reader consulting it mid-plan sees partial content for
  `D3`–`D10` until the corresponding task lands.
- `msgin.Permanent` duplicates, in miniature, functionality `cenkalti/backoff.Permanent` already
  provides — an accepted, deliberate redundancy in exchange for keeping the core free of that
  dependency.

**Rejected alternatives**
- **Reuse `backoff.Permanent`** — would have imported `cenkalti/backoff` into the core module purely
  for a marker type, violating the stdlib+clockwork constraint ahead of any genuine need (the genuine
  need — a tight retry loop — lives only in `adapter/http`, per ADR 0005).
- **Classify a handler panic as permanent** — considered and rejected: a panic proves the handler
  invocation failed, not that the message is unprocessable; permanent-classifying it would risk
  discarding recoverable messages and remove the existing retry/dead-letter safety net as the
  mechanism that eventually terminates a persistently panicking handler.
