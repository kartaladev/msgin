# ADR 0015 — Scheduled / delayed send (ScheduledSender capability + producer sugar)

- **Status:** Proposed (2026-07-19) — pending the mandatory adversarial audit of the full spec + ADR + plan
  bundle and an explicit go-ahead before implementation (CLAUDE.md design-time gate).
- **Spec:** [Spec 005 — Scheduled / delayed send](../specs/005-scheduled-send.md).
- **Plan:** [Plan 010 — Scheduled / delayed send](../plans/010-scheduled-send.md).
- **Relates to:** [ADR 0002 — Adapter SPI](0002-adapter-spi.md) (OutboundAdapter + optional-capability pattern),
  [ADR 0010 — Poller + database/sql adapter](0010-poller-sql-adapter.md) (`visible_after` / `LeaseDialect.Insert(delay)`),
  [ADR 0007 D10](0007-composition-endpoints.md) (`WithConsumerClock` naming), [ADR 0004](0004-clockwork-dependency.md)
  (`clockwork`), [ADR 0003](0003-multi-module-repository-layout.md) (module layout, for the deferred O5-5 module).

## Context

The `database/sql` adapter already persists a `visible_after` timestamp per message and its claim query skips
rows not yet visible (`visible_after <= <db-now>`, ordered, `FOR UPDATE SKIP LOCKED`), across PostgreSQL, MySQL,
and SQLite. `LeaseDialect.Insert(ctx, q, table, msgID, headers, payload, delay)` writes
`visible_after = <db-now> + delay`. The **only** consumer of that `delay` parameter that discards it is
`Outbound.Send`, which passes literal `0`. So the durable delayed-delivery *storage primitive already ships*; the
gap is a public, typed producer-side surface to set a delay.

Two shaping questions were settled with the user:

1. **How the delay reaches the write.** The core SPI is `OutboundAdapter.Send(ctx, msg) error` — no delay
   parameter. Options considered: (A) an optional-capability interface discovered by type-assertion; (B) a
   behavior-changing message header (the EIP "Delayer" `delayHeaderName`); (C) a concrete method on `sql.Outbound`
   only. Existing-code research was decisive: the **optional-capability type-assertion pattern is used in ~4
   load-bearing sites** (`NativeReliability`, `LiveValueSource`, `ProbeGate`, `LockDialect`) with a documented
   "add-your-own" recipe and an exact typed-error precedent (`ErrLockStrategyUnsupported`); whereas a caller-set
   behavior-changing header has **no precedent** and would **leak onto the delivered message** (sql framing
   round-trips all headers, no strip mechanism). (C) does not compose with the typed producer / composition layer.

2. **Whether to use an in-process scheduler (`go-co-op/gocron`).** Rejected for durable delayed send after
   researching gocron v2 (2026-07-19): it persists nothing (a pending one-shot job is lost on process restart) and
   provides no cross-instance exactly-once without an external Redis/etcd/SQL locker. The DB `visible_after` +
   `SKIP LOCKED` primitive already provides **both** durability and cross-instance exactly-once from a single
   resource the sql/pgx adapters already own. gocron remains a good fit only for a *future, optional, isolated*
   recurring/cron **source** adapter (Spec 005 O5-5 / N7), where non-durability is acceptable.

## Decision

1. **Add an optional capability interface `ScheduledSender` to the core SPI** (`spi.go`), discovered by
   type-assertion, mirroring `NativeReliability`/`LockDialect`:

   ```go
   type ScheduledSender interface {
       SendAfter(ctx context.Context, msg Message[any], delay time.Duration) error
   }
   ```

   The SPI carries the **relative** primitive only — the skew-free operation an adapter honors against its own
   store (the DB computes `now + delay`). Absolute time is not an adapter concern.

2. **`sql.Outbound` implements `ScheduledSender`.** `SendAfter` threads `delay` into the existing
   `dialect.Insert(..., delay)`; `Send` is refactored to `SendAfter(ctx, msg, 0)` (behavior-preserving). A
   compile-time `var _ msgin.ScheduledSender = (*Outbound)(nil)` documents the capability. **No DDL, dialect-SQL,
   or framing change.**

3. **Typed producer entry points + absolute sugar — widen the `Producer[T]` interface.** The exported
   `Producer[T]` interface gains two methods (implemented on `*producer[T]`):
   - `SendAfter(ctx, msg Message[T], delay time.Duration) error` — encodes `T→[]byte` (identically to `Send`),
     type-asserts the wrapped sink to `ScheduledSender`, and calls it.
   - `SendAt(ctx, msg Message[T], t time.Time) error` — core-side sugar: `delay = t.Sub(clock.Now())`, then
     `SendAfter`. Not in the SPI.

   They live **on the interface** (not a separate `ScheduledProducer[T]` type) deliberately: scheduling support
   is a *runtime* property of the wrapped sink, so a separate compile-time-typed producer would give false
   assurance (it could still return `ErrScheduledSendUnsupported` at run time). A single producer that always
   *offers* the methods and *fails loud* when the sink can't schedule is the honest shape.

   **SemVer note.** Adding methods to the exported `Producer[T]` interface is a breaking change for any *external
   implementer* of that interface (callers who only *use* it are unaffected). This is acceptable pre-1.0 (v0.x):
   `Producer[T]` was introduced recently (Plan 008) and has no known external implementers; the change ships in a
   minor bump. Post-1.0 the same change would require a major bump. Any in-repo `mockgen` mock of `Producer[T]`
   is regenerated as part of the change.

4. **Clock injection for `SendAt`.** New producer option `WithProducerClock(clockwork.Clock)` (distinct name from
   the message-level `WithClock`, following the `WithConsumerClock` precedent, ADR 0007 D10). Default
   `clockwork.NewRealClock()`. The sql adapter remains DB-clock-pure (no clock added there).

5. **Unsupported sink → typed error.** New core sentinel `ErrScheduledSendUnsupported`
   (`errors.New("msgin: ...")`, `errors.Is`-able), produced by the producer's type-assert miss — the same shape as
   `ErrLockStrategyUnsupported`. Never a silent immediate delivery.

6. **Negative / past delay clamps to 0 (deliver now), documented.** `SendAfter(delay<=0)` and `SendAt(t)` with a
   past `t` deliver immediately. "Scheduled in the past = send now" avoids footgunning a slightly-late `SendAt`
   into a typed error. Documented on both godocs.

7. **Skew is a documented contract.** `SendAfter(delay)` is fully skew-free (DB computes `now_db + delay`).
   `SendAt(t)` converts absolute→relative with the app clock, so it is realized as `t + (db_clock − app_clock)` —
   it inherits app↔DB skew on the absolute anchor. Documented on `SendAt`; callers needing exactness use
   `SendAfter`.

8. **sql-only this increment; memory fails loud.** The in-memory `Broker` does not implement `ScheduledSender`;
   scheduling to it returns `ErrScheduledSendUnsupported` (never silent-immediate). Memory delayed send and the
   EIP-named `Delayer` composition step are deferred (Spec 005 O5-1, O5-2). The recurring/cron source adapter is
   deferred (O5-5).

## Consequences

**Positive.**
- Durable, restart-safe, cross-instance-exactly-once delayed delivery with **no new storage machinery** — the
  `visible_after` column, index, and DB-clock arithmetic are reused unchanged.
- **Zero new dependency**; the producer's clock uses the already-approved `clockwork` (ADR 0004). `go mod tidy`
  stays clean.
- Idiomatic to the codebase: the capability + typed-error shape mirrors four existing capabilities and a
  documented recipe.
- Fail-loud on unsupported sinks (no silent delay-drop), matching the sensible-defaults rule.
- Purely additive and backward compatible: `Send`/`NewProducer` signatures unchanged; absent any scheduling call,
  behavior is identical to today.

**Negative / trade-offs.**
- `SendAt` inherits app↔DB clock skew on the absolute anchor (Decision 7) — a documented caveat, not a bug; the
  skew-free `SendAfter` is offered for exactness.
- Producer API grows by two methods and one option (small, additive).
- Scheduling to the memory adapter is unsupported this increment (fails loud) — a caller wanting in-memory
  delayed delivery must wait for O5-2 or use the sql adapter.
- The `Send`→`SendAfter(...,0)` refactor in the sql adapter is behavior-preserving but touches the hot outbound
  path; guarded by the existing round-trip tests plus a `delay=0` parity assertion.

**Neutral.**
- Absolute-time scheduling stays out of the SPI (relative-only), keeping every adapter's capability simple and
  the app clock confined to the core.
- Rich recurring/cron triggers are a separate, later, optional-module concern (O5-5) — this ADR deliberately does
  not couple one-shot durable delay to a recurring in-process scheduler.

## Alternatives considered

- **Behavior-changing header (EIP Delayer `delayHeaderName`).** More EIP-canonical and composes through the
  pipeline for free, but no existing precedent for a caller-set behavior header, and the delay header would leak
  onto the delivered message (sql framing strips nothing). Rejected as the mechanism; the EIP-named `Delayer`
  *endpoint* may still be layered over the capability later (O5-1).
- **Concrete method on `sql.Outbound` only.** Simplest, but does not compose with the typed producer / `Chain` /
  `To`. Rejected.
- **In-process scheduler (`go-co-op/gocron`).** Not durable across restart, no cross-instance exactly-once without
  external infra, hard dependency-policy cost. Rejected for durable delayed send; reserved for the optional
  recurring-source module (O5-5).
