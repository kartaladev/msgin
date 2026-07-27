# Spec 014 — Core package layout, channel segregation, and behavior types

- **Status:** **NEEDS-REVISION (2026-07-27) — DO NOT IMPLEMENT.** The mandatory adversarial design audit ran on
  the assembled bundle (this spec + ADRs 0027–0029 + Plan 027) with three independent Opus auditors on distinct
  lenses. **All three returned `NEEDS-REVISION`** (18 HIGH findings, three of them reached independently by two
  or more auditors). See **[Plan 027 audit round 1](../plans/027-audit-round-1.md)**.
  **§3's move-list is known to be non-executable** (a real import cycle on `Subscription`, four files needing a
  split rather than two, 18 unexported identifiers crossing the new boundaries), **§6's `ReleaseStrategy`
  signature cannot express an eval error**, **§8 (`SettleMembers`) is recommended for removal**, and **§9.1's
  acceptance criterion is falsified by `handler.go`, which the spec keeps in root**. Several §1/§3 measurements
  are corrected in the audit record's §E. Fold the findings in, then re-audit before any code.
- **Promoted from:** [RFC-0001](../rfcs/0001-core-package-restructure.md) (package restructure),
  [RFC-0002](../rfcs/0002-eip-alignment.md) (lexical alignment + channel segregation),
  [RFC-0003](../rfcs/0003-endpoint-behavior-types.md) (behavior types + expr provider). All three were accepted
  2026-07-27 with every open question settled; read their §7 *Decisions* before this spec.
- **Governing decisions:** [ADR 0027](../adrs/0027-core-package-restructure.md) (layout, C-full, clean break),
  [ADR 0028](../adrs/0028-channel-interface-segregation.md) (channel interfaces),
  [ADR 0029](../adrs/0029-eip-lexical-alignment.md) (renames, behavior-type naming, expr provider).
- **Amends:** [ADR 0003](../adrs/0003-multi-module-repository-layout.md) ("core is one package"),
  [ADR 0019](../adrs/0019-runtime-expression-evaluation.md) (`expr-lang` as a core dependency).
- **Implementation:** [Plan 027](../plans/027-core-package-layout.md).
- **Deferred to later increments:** [RFC-0004](../rfcs/0004-trigger-scheduling.md) (Trigger/Poller — increment 2),
  [RFC-0005](../rfcs/0005-eip-gap-components.md) (the five EIP components), except
  `MessageGroupStore.SettleMembers`, which rides this window (§8).

## 1. Problem

Three defects share one cause — the core is a single flat package, so nothing forces a boundary.

1. **No navigable structure.** The root is `package msgin` with **32 source + 45 test files** and no grouping.
   A reader looking for the Splitter, the Consumer, and the circuit breaker searches the same undifferentiated
   list.
2. **`MessageChannel` is satisfied by exactly one type.** Measured: the only `var _ MessageChannel` assertion in
   the repository outside a test fake is `DirectChannel` (`channel.go:24`). `QueueChannel` has `Send`+`Poll`
   (it is a `PollingSource`); `PublishSubscribeChannel` has `Send`+`Subscribe`→`(Subscription, error)`. The
   interface bundles `Send`+`Subscribe`, but **every consumer of it calls only `Send`** —
   `WithDiscardChannel` (`filter.go:14`), `WithDefaultChannel` (`router.go:14`), the router's `pick` return
   (`router.go:25`), and `NewChannelExchange`'s `request` (`exchange.go:224`). Verified: no call site subscribes
   through the interface. **Consequence today: a `QueueChannel` or `PublishSubscribeChannel` cannot be used as a
   discard target, a default route, a router destination, or an exchange request channel.** This is a capability
   defect, not a naming preference.
3. **The core is forced to carry `expr-lang`.** Six `*Expr` constructors in `expr.go` are the only reason
   `go list -deps .` pulls in a **7.1 MB** dependency tree, which propagates to all seven modules in the
   workspace — including a consumer using nothing but the SQL adapter.

**The timing is what makes this affordable.** The repository has **zero tags**: nothing is released and no
consumer has imported any symbol, so every break in this spec costs a mechanical rename today and a major
version plus a deprecation cycle later.

## 2. Scope

**In scope:** the package restructure, the channel interface segregation, the EIP renames, the named behavior
types, and the extraction of expression support into its own module. These land as **one breaking window and one
`apidiff` review**, because they touch overlapping symbols and splitting them would cost two reviews of the same
surface.

**Out of scope:** any behavior change. This increment is **behavior-preserving by construction** — every
existing test must pass unchanged except where a signature it names has deliberately moved or narrowed. New
behavior (triggers, the five EIP components) belongs to later increments.

## 3. Target package layout (normative)

Organising principle: **vocabulary + interfaces in the root; every implementation in a package named for the EIP
chapter that defines it.** Nothing in root imports a subpackage.

```
msgin/       root — vocabulary + SPI ONLY
  endpoint/    Consumer, Producer, Poller, Gateway, ChannelExchange, Activate   (EIP ch.10)
  routing/     Filter, Router, Splitter, Aggregator                             (EIP ch.7)
  transform/   Transform                                                        (EIP ch.8)
  channel/     DirectChannel, QueueChannel, PublishSubscribeChannel
  resilience/  RateLimiter, CircuitBreaker, OverflowPolicy, backoff
expr/  ← its own module — Predicate / RouteFunc / Transformer / … providers
```

**File-level mapping** (root 32 → 9). This is the enumerated move-list RFC-0001's success metric is read
against; the plan must prove nothing else moves.

| Destination | Files |
|---|---|
| `endpoint/` | `activator.go`, `consumer.go`, `credit.go`, `exchange.go`, `gateway.go`, `poller.go`, `producer.go`, + the option half of `flowcontrol.go` |
| `routing/` | `filter.go`, `router.go`, `splitter.go`, `aggregator.go` |
| `transform/` | `transformer.go` |
| `channel/` | `DirectChannel` (from `channel.go`), `queuechannel.go`, `pubsub.go`, `pubsub_registry.go` |
| `resilience/` | `backoff.go`, `breaker.go`, `ratelimit.go`, + the interface half of `flowcontrol.go` |
| **stays in root** | `message.go`, `errors.go`, `handler.go`, `spi.go`, `codec.go`, `payload.go`, `store.go`, `groupstore.go`, `reliability.go`, `retry.go`, the interface half of `channel.go`, `doc.go` |
| **deleted** | `expr.go` (reborn in the `expr` module), `doc_composition.go` (its prose redistributes to each package's `doc.go`) |

**Two files split rather than move**, because each mixes an interface with its consumer:

- **`channel.go`** — the `MessageChannel` interface stays in root; `DirectChannel` moves to `channel`.
- **`flowcontrol.go`** — the `RateLimiter`/`CircuitBreaker` interfaces go to `resilience`; the
  `WithMaxInFlight`-style `ConsumerOption` constructors travel with `Consumer` into `endpoint`. Under the
  rejected C-lite these options would have been stranded in root away from their type; C-full removes the
  problem rather than managing it.

`retry.go` (`RetryPolicy`, `Hooks`) stays in root: they are configuration value types, not implementations. The
`ExponentialBackoff` implementation they reference moves to `resilience`.

## 4. Root contract after the move

Root declares types, interfaces, and constants — and **no constructor that builds a running component**. That
is the mechanical, checkable statement of C-full (§9).

Vocabulary: `Message[T]`, `Headers`, the `Header*` constants, `Delivery`, `Subscription`, `MessageGroup`,
`MessageGroupClaim`, the error sentinels. SPI: `MessageChannel`, `SubscribableChannel`, `MessageHandler`,
`PollingSource`, `EventDrivenSource`, `OutboundAdapter`, `NativeReliability`, `LiveValueSource`,
`ScheduledSender`, `RequestReplyExchange`, `ChannelStore`, `MessageGroupStore`, `PayloadCodec`. Policy value
types: `RetryPolicy`, `Hooks`.

## 5. Channel interface segregation (normative)

```go
// MessageChannel is a send-only conduit. Every endpoint that merely forwards a
// message — a discard target, a default route, a router destination, an
// exchange request channel — takes this, so any channel implementation qualifies.
type MessageChannel interface {
    Send(ctx context.Context, msg Message[any]) error
}

// SubscribableChannel additionally accepts handlers. Spring calls this
// SubscribableChannel; the Subscription return is msgin's addition, giving the
// caller an unsubscribe handle.
type SubscribableChannel interface {
    MessageChannel
    Subscribe(h MessageHandler) (Subscription, error)
}
```

Satisfaction after the change — the point of the exercise:

| Type | `MessageChannel` | `SubscribableChannel` | Today |
|---|---|---|---|
| `DirectChannel` | ✅ | ✅ | satisfied `MessageChannel` only |
| `PublishSubscribeChannel` | ✅ | ✅ | satisfied **neither** |
| `QueueChannel` | ✅ | — (is a `PollingSource`) | satisfied **neither** |

**`DirectChannel.Subscribe` changes signature** from `error` to `(Subscription, error)` so both subscribable
channels satisfy one contract. This is the breaking half of the change and is deliberate: without it,
`ChannelExchange`'s reply channel can only ever be a `DirectChannel`.

**`PollableChannel` is deliberately not defined.** It would duplicate the existing `PollingSource` SPI's exact
method set (`Poll(ctx, max) ([]Delivery, error)`, already implemented by `QueueChannel`), and no signature in the
library would take one. It can be added later non-breakingly. This is a documented divergence from Spring's
three-way split — see ADR 0028.

**Call-site narrowing** (all become send-only): `routing.WithDiscardChannel`, `routing.WithDefaultChannel`, the
router's `RouteFunc` return, and `endpoint.NewChannelExchange`'s `request`. Only `NewChannelExchange`'s `reply`
takes `SubscribableChannel` — it is the sole genuine subscriber (`exchange.go:247`).

## 6. Renames and behavior types

**Breaking renames.** `StreamingSource` → **`EventDrivenSource`**: the canonical EIP term is *Event-Driven
Consumer* (Spring: `EventDrivenConsumer`), and "streaming" collides with unrelated streaming-data vocabulary.

**The `Stream` method keeps its name** on the renamed interface. It describes the mechanism accurately (the
implementation pushes deliveries onto a channel until the context ends), Spring offers no counterpart name to
align with, and renaming it would churn every adapter for no gain. Recorded as a decision so it reads as
considered — flagged for the audit in ADR 0029.

**`Exchange` is kept, qualified.** Root keeps `RequestReplyExchange`; the implementation is
`endpoint.ChannelExchange`, with a godoc line disclaiming the AMQP meaning. **The ADR must verify the claim that
Spring Integration names its equivalent `RequestReplyExchanger`** rather than assert it; if that citation does
not hold, this decision reverts to a rename.

**Behavior types.** Each endpoint's behavioral closure gets a named func type, with the package carrying the
qualifier rather than the type repeating it:

```go
// package routing
type Predicate[A any]    func(ctx context.Context, m Message[A]) (bool, error)
type RouteFunc           func(ctx context.Context, m Message[any]) (MessageChannel, error)
type SplitFunc[A, B any] func(ctx context.Context, m Message[A]) ([]Message[B], error)
type CorrelationStrategy func(m Message[any]) (string, error)
type ReleaseStrategy     func(g MessageGroup) bool

// package transform
type Transformer[A, B any] func(ctx context.Context, m Message[A]) (Message[B], error)
```

Combinators are methods (`Predicate.And`/`Or`/`Not`) — the payoff that distinguishes naming these types from
leaving them anonymous. Each godoc names its Spring equivalent so a Spring-trained reader still finds it.

A bare closure remains assignable, so **call sites are source-compatible**. `apidiff` will nonetheless report the
parameter-type change on each typed constructor; that is expected and benign, and the plan records it as a
*reviewed, source-compatible* entry rather than claiming zero output.

## 7. Expression support moves out of the core

The six `*Expr` constructors are **removed from the core outright** — no deprecated shims, since nothing is
tagged and there is no consumer a shim would protect. They are reborn in a separate **`expr` module** whose
providers return the §6 types:

```go
func Predicate[A any](s string) (routing.Predicate[A], error)  // compiles once; fails at construction
```

The compile error lives at the provider call, so the base constructors stay non-fallible and inline-composable
and the "invalid expression fails at construction" contract of ADR 0019 is preserved.

**A separate module is required, not a subpackage** — a subpackage of the root module would leave `expr-lang` in
the root `go.mod` and deliver none of the benefit. The rule this follows, stated so it is not arbitrary against
RFC-0004's opposite conclusion for `robfig`: *a zero-transitive dependency is pushed to its own module when its
weight is material to consumers who do not use it* — `expr-lang` at 7.1 MB is; `robfig/cron` at 144 KB is not.

**Sequencing consequence (load-bearing).** All six `*Expr` constructors return `Step`, `*Router`, or
`AggregatorOption` — types that move to `routing`/`transform`. So `expr.go` cannot remain in root once those
move (root would have to import a subpackage, violating §3), and splitting it across two new packages only to
delete it afterwards is throwaway work. **The `*Expr` deletion is therefore sequenced first**, before any package
extraction. Between that task and the `expr` module's arrival, expression support is absent from the branch —
acceptable within a branch that restores it before merge, and called out here so it is not mistaken for scope
loss.

## 8. `MessageGroupStore.SettleMembers` rides this window

RFC-0005's Resequencer needs partial release, which `MessageGroupStore` cannot express: `SettleGroup` deletes
exactly the claimed member set, and the obvious workaround (re-`Add` the tail during the lease) **silently loses
messages**, because `Add` is documented idempotent by msg id, so re-adding a still-leased member is a no-op and
`SettleGroup` then deletes it.

Adding a method to a shipped interface breaks every implementer, so it belongs in this window rather than in the
"additive" increment that consumes it:

```go
// SettleMembers deletes exactly the listed members of a claim and returns the
// rest to live, fenced on claim.Epoch like SettleGroup. A fence miss is a no-op.
SettleMembers(ctx context.Context, claim MessageGroupClaim, msgIDs []string) error
```

Implemented here for `memory` and `sql` (plus the three dialect modules) with conformance tests; **consumed**
later by the Resequencer. Defining a method with no in-tree caller is a deliberate exception to the
small-surface rule, justified solely by the breaking-window timing — the audit should challenge it.

## 9. Acceptance criteria

1. **Layout** — root holds ~9 source files, and **no root file declares a constructor for a running component**.
   This is the mechanical check that C-full happened rather than a partial move.
2. **Move-list fidelity** — `apidiff`/`gorelease` against the pre-window tree reports **only** the moves
   enumerated in §3 and the narrowings in §5–§6, and nothing else.
3. **Dependency** — `go list -deps .` on the root module excludes `expr-lang`, and the root `go.mod` no longer
   requires it. (`robfig/cron` **stays** — that is RFC-0004's settled decision, not an oversight.)
4. **Capability** — a test proves a `QueueChannel` and a `PublishSubscribeChannel` can each serve as a discard
   target, a default route, and an exchange request channel. This is the defect in §1.2, so it needs a test that
   fails against the pre-window code.
5. **Behavior preservation** — every pre-existing test passes, modified only where it names a moved or narrowed
   signature. No test's assertions change.
6. **Gates** — `go test ./... -race` green in **every one of the seven modules standalone** (`GOWORK=off`, as CI
   runs it), `go vet`, `golangci-lint`, `gofmt`, `govulncheck` clean, `go mod tidy` a no-op, and
   `CGO_ENABLED=0 go build ./...` succeeds.
7. **Coverage** — ≥85% on every changed package, and every hot-path and typed-error branch covered. A pure move
   must not lose coverage; per-package `-cover` is compared before and after.
8. **Docs** — `MIGRATION.md` written; CLAUDE.md's architecture blueprint and dependency policy updated in the
   same commit (it currently names `StreamingSource` and ratifies `expr-lang`); `MESSAGING.md` reconciled.

## 10. Multi-instance topology (CLAUDE.md mandatory review)

This increment moves and narrows existing components; it introduces no new cross-instance state. The
topology statements it must **preserve and now state explicitly in godoc**, per CLAUDE.md's multi-instance rule:

- **`DirectChannel`, `PublishSubscribeChannel`, `ChannelExchange`'s correlator — in-process only.** A Go channel
  and an in-memory correlator map cannot cross a process boundary. `SubscribableChannel` is therefore an
  **in-process contract**: a reply arriving at instance B for a request made on instance A is not reachable
  through it. The distributed answer is the **Return Address** pattern via a future external
  `RequestReplyExchange` adapter — the seam already exists in root and this increment must not narrow it shut
  (cf. [Spec 010 §8.1](010-messaging-gateway.md), [ADR 0022](../adrs/0022-messaging-gateway.md)).
- **`QueueChannel` over a durable `ChannelStore`** is the multi-instance-safe conduit; competing consumers come
  from the worker pool plus the store's claim semantics, unchanged here.
- **`SettleMembers`** inherits `MessageGroupStore`'s existing epoch fencing, which is precisely the
  multi-instance mechanism; the conformance test must cover a stolen-lease fence miss being a no-op.

Narrowing `MessageChannel` to send-only **widens** what a distributed deployment can plug in — a durable
`QueueChannel` now qualifies everywhere a `DirectChannel` was previously required — so this change moves in the
right direction for the topology rule rather than against it.
