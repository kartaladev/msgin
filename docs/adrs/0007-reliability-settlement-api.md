# ADR 0007 — Reliability & settlement API

- **Status:** Accepted (2026-07-17)
- **Context source:** [Spec 001 — Messaging core](../specs/001-messaging-core.md) §5–§8;
  [Plan 002 — Reliability](../plans/002-reliability.md)
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

#### Known limitation — `attemptTracker` unbounded growth under retry-forever (triaged; deferred to Plan 003)

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

**Resolution is deferred to Plan 003**, which must add a bounded / TTL-based tracker that still respects
NF-2 (no mid-redelivery reset), paired with the credit / delay-park flow-control work. We deliberately do
**not** add capacity-based eviction now: at Plan-002 scope it would be dead code that could only violate
NF-2 if it ever fired. Recorded here as an explicit, accepted limitation with this rationale so the
residual is traceable rather than silent.

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

### D8 — `divert` settlement contract (Task 5)

*Stub — fleshed out in Task 5.* `divert` acks the original delivery only if the target sink accepts the
message; otherwise it fires the relevant hook and `Nack`s the original (never Ack-and-lose). A
send-failure `Nack` carries a non-zero backoff delay (not `0`) so a down sink is not hammered.

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
