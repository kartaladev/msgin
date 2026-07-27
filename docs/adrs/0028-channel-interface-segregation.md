# ADR 0028 — Segregate `MessageChannel` into send-only and subscribable contracts

- **Status:** **NEEDS-REVISION (2026-07-27) — DO NOT IMPLEMENT.** All three auditors returned `NEEDS-REVISION` —
  see **[Plan 027 audit round 1](../plans/027-audit-round-1.md)**. Unresolved in this ADR: **the narrowed
  `MessageChannel` is byte-identical to the existing `OutboundAdapter`** (`spi.go:41`), which is exactly the
  duplication §3 refuses to create for `PollableChannel` — and it **voids ADR 0013's F2 rationale**; **widening
  the exchange reply channel silently voids `ChannelExchange`'s exclusivity invariant**, making two exchanges
  over one `PublishSubscribeChannel` a valid program that cross-delivers replies; **`Close()` now leaks the
  `Subscription` it discards**; **`DirectChannel`'s new `Subscription` has no defined semantics** (re-subscribe
  after `Cancel`? `Send` in between? in-flight race?); and the "no call site subscribes through the interface"
  claim in Context is contradicted four lines later by `exchange.go:247`.
- **Must additionally cite:** [ADR 0013](0013-composition-endpoints.md) (which decided the `MessageChannel`
  contract and the `To`/`OutboundAdapter` distinction this ADR voids) and
  [ADR 0014](0014-publish-subscribe.md) (which decided the `Subscription`-handle lifecycle it adopts).
- **RFC:** [0002](../rfcs/0002-eip-alignment.md) · **Spec:** [014 §5](../specs/014-core-package-layout.md)
  · **Plan:** [027](../plans/027-core-package-layout.md)
- **Relates to:** [ADR 0027](0027-core-package-restructure.md) (same window, same `apidiff` pass),
  [ADR 0022 — Messaging gateway](0022-messaging-gateway.md) (the exchange whose reply channel this unlocks),
  [Spec 004 — Publish-subscribe](../specs/004-publish-subscribe.md).

## Context

`MessageChannel` bundles two responsibilities:

```go
type MessageChannel interface {
    Send(ctx context.Context, msg Message[any]) error
    Subscribe(h MessageHandler) error
}
```

RFC-0002 filed this as lexical drift from Spring, which splits `MessageChannel` (send) /
`SubscribableChannel` (subscribe) / `PollableChannel` (receive). Measuring it against the code showed something
worse than a naming problem.

**The interface is satisfied by exactly one type in the repository.** The only `var _ MessageChannel` assertion
outside a test fake is `DirectChannel` (`channel.go:24`). `QueueChannel` has `Send`+`Poll` — it is a
`PollingSource`, not a `MessageChannel`. `PublishSubscribeChannel` has `Send`+`Subscribe`, but its `Subscribe`
returns `(Subscription, error)`, so it satisfies neither. `MessageChannel` is not a channel abstraction; it is
`DirectChannel`'s shape wearing the general name.

**Every consumer of the type calls only `Send`** — `WithDiscardChannel` (`filter.go:14`), `WithDefaultChannel`
(`router.go:14`), the router's `pick` return (`router.go:25`), and `NewChannelExchange`'s `request`
(`exchange.go:224`). Verified: no call site subscribes through the interface. Yet the interface demands
`Subscribe`, so **a `QueueChannel` or `PublishSubscribeChannel` cannot today be used as a discard target, a
default route, a router destination, or an exchange request channel.** That is a capability defect.

The single genuine subscriber is `exchange.go:247`, `reply.Subscribe(e.receiver())` — which is what proves a
subscribable contract earns its keep rather than being symmetry for its own sake.

## Decision

### 1. Two interfaces

```go
type MessageChannel interface {
    Send(ctx context.Context, msg Message[any]) error
}

type SubscribableChannel interface {
    MessageChannel
    Subscribe(h MessageHandler) (Subscription, error)
}
```

### 2. Unify `Subscribe` on `(Subscription, error)`

`DirectChannel.Subscribe` changes from returning `error` to returning `(Subscription, error)`, so both
subscribable channels satisfy one contract and an exchange reply channel may be either. This is the breaking
half of the change and is the point of it: without unification, `ChannelExchange`'s reply channel can only ever
be a `DirectChannel`.

Unifying *upward* (on the richer signature) rather than downward was chosen because the `Subscription` handle is
the more capable contract and `PublishSubscribeChannel` already returns it; forcing pub-sub to drop it would
lose an unsubscribe capability that already exists and works.

### 3. `PollableChannel` is deliberately **not** defined

Spring's third interface has no counterpart here. It would duplicate the existing `PollingSource` SPI's exact
method set — `Poll(ctx, max) ([]Delivery, error)`, already implemented by `QueueChannel` — and **no signature in
the library would take one**. Per CLAUDE.md's small-surface rule new public surface must earn its keep, and this
can be added later non-breakingly if a caller ever appears.

Recorded explicitly so it reads as a decision, not an oversight: this is a knowing divergence from Spring's
three-way split, and RFC-0002's success metric requires every drift item to be resolved *or* kept with a written
rationale.

### 4. Call sites narrow to what they actually use

`routing.WithDiscardChannel`, `routing.WithDefaultChannel`, `routing.RouteFunc`'s return, and
`endpoint.NewChannelExchange`'s `request` take `MessageChannel`. Only `NewChannelExchange`'s `reply` takes
`SubscribableChannel`.

## Consequences

**Positive.** The capability defect is fixed: any channel implementation can be a discard target, a default
route, a router destination, or an exchange request channel. `MessageChannel` becomes an honest name — every
channel satisfies it. The interfaces state what each call site actually requires, which is both better Go and
better documentation. **It also improves the multi-instance story**: a durable `QueueChannel` now qualifies
everywhere a `DirectChannel` was previously mandatory, so a distributed deployment gains options rather than
losing them.

**Negative, accepted.**

- `DirectChannel.Subscribe`'s signature change breaks every implementer and caller. Pre-v1 with zero tags, so
  the cost is mechanical; recorded in `MIGRATION.md`.
- Two interfaces where there was one — a marginally larger surface, justified by the defect it fixes.
- Any third-party type that implemented the old bundled `MessageChannel` now satisfies only the send half. No
  such type exists (nothing is released), but the migration note must say so plainly.

**Risk to test for.** A narrowed parameter type silently accepts more implementations, so a regression here is
invisible to the compiler. Spec 014 §9.4 therefore requires a test proving a `QueueChannel` **and** a
`PublishSubscribeChannel` each work as a discard target, a default route, and an exchange request channel — a
test that must fail against the pre-window code, or it is not testing the fix.

**Topology.** `SubscribableChannel` remains an **in-process contract**: a Go channel cannot cross a process
boundary, so a reply arriving at instance B for a request made on instance A is not reachable through it. The
distributed answer stays the **Return Address** pattern via a future external `RequestReplyExchange` adapter
(Spec 010 §8.1, ADR 0022); this ADR must not narrow that seam shut, and the godoc must say so.
