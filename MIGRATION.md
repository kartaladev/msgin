# Migration guide — the pre-v1 core restructure

> **Governing artifacts.** Spec [014](docs/specs/014-core-package-layout.md) · Plan
> [027](docs/plans/027-core-package-layout.md) · ADRs
> [0027](docs/adrs/0027-core-package-restructure.md),
> [0028](docs/adrs/0028-channel-interface-segregation.md),
> [0029](docs/adrs/0029-eip-lexical-alignment.md),
> [0030](docs/adrs/0030-reply-channel-exclusivity-probe.md) · RFC
> [0002](docs/rfcs/0002-eip-alignment.md). Where this guide and the spec disagree, **the spec wins** — and the
> disagreement is a defect worth reporting.

## 1. Who needs this

**Nobody, strictly.** `msgin` has **zero git tags**, has never been released, and has no known consumers. This
window was taken *because* breaking changes are still free. The guide exists so that anyone holding a working
tree pinned to a pre-restructure commit — or reading code written against it — can mechanically translate it.

Every symbol below is accounted for. Nothing was dropped silently: the counts in §3 are **measured** from
`apidiff` against the committed pre-window baseline
([`docs/plans/027-root-api-baseline.txt`](docs/plans/027-root-api-baseline.txt)), not transcribed.

## 2. What changed, in one sentence

The flat root package was split into **five subpackages** — `endpoint`, `routing`, `transform`, `channel`,
`resilience` — the expression-backed constructors moved to a **separate `expr` module**, and
`MessageChannel` was **segregated** into a send-only pipe plus a `SubscribableChannel` for the subscribing
half.

Root keeps the dependency-free vocabulary: `Message`, `Headers`, the SPI, the error sentinels, `RetryPolicy`,
`Step`, `MessageHandler`. **Dependency points inward** — the five subpackages import root; root imports none of
them, and none imports another. Both directions are asserted mechanically in Plan 027 Task 12.

### The import shape

```go
// BEFORE — one import
import "github.com/kartaladev/msgin"

c, err := msgin.NewConsumer(src, h,
    msgin.WithMaxInFlight(100),
    msgin.WithRetryPolicy(msgin.RetryPolicy{
        MaxAttempts: 3,
        Backoff:     msgin.ExponentialBackoff{Initial: time.Second},
    }),
)

// AFTER — the type stays in root, the constructor and options move
import (
    "github.com/kartaladev/msgin"
    "github.com/kartaladev/msgin/endpoint"
    "github.com/kartaladev/msgin/resilience"
)

c, err := endpoint.NewConsumer(src, h,
    endpoint.WithMaxInFlight(100),
    endpoint.WithRetryPolicy(msgin.RetryPolicy{
        MaxAttempts: 3,
        Backoff:     resilience.ExponentialBackoff{Initial: time.Second},
    }),
)
```

**Note the two-import shape of a retry policy.** `RetryPolicy` is a root type; its `Backoff` field takes a
`resilience.ExponentialBackoff`. That split is deliberate — `RetryPolicy` is vocabulary the SPI speaks, while
a backoff *strategy* is a resilience mechanism.

## 3. The exported-surface diff (measured)

```
$ apidiff docs/plans/027-root-api-baseline.txt .
… 97 removals, 9 additions
```

The **97 removals** form a verified partition — checked by set arithmetic against
[`docs/plans/027-tools/symmap.tsv`](docs/plans/027-tools/symmap.tsv), with an **empty residual**:

| Class | n | What |
|---|--:|---|
| Relocated into a subpackage | **87** | §4 — mechanical, one import change each |
| Deleted outright (`*Expr`) | **6** | §5 — replaced by the `expr` module |
| Renamed | **1** | `StreamingSource` → `EventDrivenSource` (§6.1) |
| The segregation itself | **1** | `MessageChannel.Subscribe` (§6.2) |
| Sentinels moved out of root (**D-I**) | **2** | `ErrInvalidExpression`, `ErrExprResultType` (§5.2) |
| | **97** | |

87 + 6 + 1 + 1 + 2 = 97.

The **9 additions** are all in root:

| Symbol | Why |
|---|---|
| `IsPermanent` | was unexported `isPermanent`; forced — it inspects unexported wrapper types |
| `RetryAfterOf` | was unexported `retryAfterOf`; same reason |
| `NewID` | was unexported `randomID`; one id scheme for message and correlation ids |
| `SubscribableChannel` | the subscribing half of the segregated contract (§6.2) |
| `EventDrivenSource` | the EIP rename (§6.1) |
| `ErrNilSubscription` | rejects an SPI `Subscribe` returning `(nil, nil)` |
| `ExclusiveSubscribable` | the reply-channel exclusivity probe (**D-J**) |
| `ErrSharedReplyChannel` | its typed rejection |
| `ErrNilMessageGroup` | a `MessageGroupStore.Add` returning a nil snapshot with a nil error (**D-Q**) |

## 4. Relocated symbols — old → new

Every entry below is the **same symbol with the same behavior**; only its package qualifier changes.
`msgin.X` → `<pkg>.X`.

### 4.1 → `endpoint` (45)

`Activate` · `ChannelExchange` · `Consume` · `Consumer` · `ConsumerOption` · `ExchangeOption` · `Gateway` ·
`GatewayOption` · `Handler` · `NewChannelExchange` · `NewConsumer` · `NewGateway` · `NewProducer` ·
`OutboundGateway` · `Producer` · `ProducerOption` · `WithAttemptTTL` · `WithCircuitBreaker` ·
`WithConcurrency` · `WithConsumerClock` · `WithConsumerCodec` · `WithExchangeClock` · `WithExchangeLogger` ·
`WithHandlerTimeout` · `WithHooks` · `WithInvalidMessageSink` · `WithLogger` · `WithMaxInFlight` ·
`WithMaxPayloadBytes` · `WithOverflow` · `WithPollInterval` · `WithPollMaxBatch` · `WithProducerClock` ·
`WithProducerCodec` · `WithProducerDeadLetterTimeout` · `WithProducerHooks` · `WithProducerLogger` ·
`WithProducerRetry` · `WithProducerRetryAfterCap` · `WithProducerRetryBudget` · `WithRateLimit` ·
`WithReplyTimeout` · `WithRetryPolicy` · `WithShutdownTimeout` · `WithUnmatchedReplySink`

### 4.2 → `routing` (18)

`Aggregator` · `AggregatorOption` · `Filter` · `FilterOption` · `NewAggregator` · `NewRouter` · `Router` ·
`RouterOption` · `Split` · `WithAggregatorClock` · `WithCompletionSize` · `WithCorrelationStrategy` ·
`WithDefaultChannel` · `WithDiscardChannel` · `WithExpiredGroupChannel` · `WithGroupTimeout` ·
`WithOutputChannel` · `WithReleaseStrategy`

> **`WithReleaseStrategy` is BOTH relocated AND retyped** (decision **D-E**) — say both, because a caller who
> only fixes the import still will not compile. See §6.3.

### 4.3 → `channel` (14)

`DirectChannel` · `FanOutAllSucceed` · `FanOutBestEffort` · `FanOutPolicy` · `NewDirectChannel` ·
`NewPublishSubscribeChannel` · `NewPubSub` · `NewQueueChannel` · `PublishSubscribeChannel` · `PubSub` ·
`PubSubOption` · `QueueChannel` · `WithFanOut` · `WithPubSubLogger`

### 4.4 → `resilience` (9)

`CircuitBreakerOption` · `ExponentialBackoff` · `NewCircuitBreaker` · `NewTokenBucket` · `TokenBucketOption` ·
`WithBreakerClock` · `WithBreakerCooldown` · `WithBreakerThreshold` · `WithTokenBucketClock`

### 4.5 → `transform` (1)

`Transform`

### 4.6 New in the subpackages — never in root

These are **not** migrations; they are additions the restructure introduced, and `apidiff` on the *root*
package does not show them because they live elsewhere.

| Symbol | Package | What |
|---|---|---|
| `Predicate[A]` | `routing` | the named filter behavior, with `And`/`Or`/`Not` combinators |
| `RouteFunc` | `routing` | the named router behavior |
| `SplitFunc[A,B]` | `routing` | the named splitter behavior |
| `CorrelationStrategy` | `routing` | the named correlation behavior |
| `ReleaseStrategy` | `routing` | the named, **fallible** release behavior (§6.3) |
| `WithReleaseWhen` | `routing` | sugar for the *old* infallible release shape (§6.3) |
| `Transformer[A,B]` | `transform` | the named transformer behavior |
| `WithSingleSubscriber` | `channel` | opt-in single-subscriber pub-sub; **off by default** |
| `WithSharedReplyChannel` | `endpoint` | opts out of the reply-channel exclusivity probe (**D-J**) |

## 5. Expression support moved to its own module

### 5.1 The six deleted constructors

`expr-lang/expr` is **no longer a core dependency**. The six expression-backed constructors were deleted from
root and reappear as **providers** in the `github.com/kartaladev/msgin/expr` module, which owns that
dependency so no consumer of the core pays for it.

| Removed from root | Replacement | Compose with |
|---|---|---|
| `FilterExpr[A](expr)` | `expr.Predicate[A](expr) (routing.Predicate[A], error)` | `routing.Filter` |
| `RouterExpr[A](expr, routes)` | `expr.RouteFunc[A](expr, routes) (routing.RouteFunc, error)` | `routing.NewRouter` |
| `TransformExpr[A,B](expr)` | `expr.Transformer[A,B](expr) (transform.Transformer[A,B], error)` | `transform.Transform` |
| `SplitExpr[A,B](expr)` | `expr.SplitFunc[A,B](expr) (routing.SplitFunc[A,B], error)` | `routing.Split` |
| `WithCorrelationExpr[A](expr)` | `expr.Correlation[A](expr) (routing.CorrelationStrategy, error)` | `routing.WithCorrelationStrategy` |
| `WithReleaseExpr[A](expr)` | `expr.Release[A](expr) (routing.ReleaseStrategy, error)` | `routing.WithReleaseStrategy` |

**The shape changed, not just the name.** Each old constructor took an expression and returned an endpoint
directly. Each new provider **compiles the expression eagerly** and returns `(behavior, error)`, which you then
hand to the routing/transform constructor. A bad expression is now a **construction-time** error naming the
offending source, instead of a per-message surprise.

```go
// BEFORE
step := msgin.FilterExpr[Order]("payload.Total > 100")

// AFTER
import (
    "github.com/kartaladev/msgin/expr"
    "github.com/kartaladev/msgin/routing"
)

pred, err := expr.Predicate[Order]("payload.Total > 100")
if err != nil {
    return err // expr.ErrInvalidExpression, naming the expression
}
step := routing.Filter(pred)
```

Add the module explicitly — it is not pulled in by the core:

```bash
go get github.com/kartaladev/msgin/expr
```

### 5.2 The two sentinels that left root (D-I)

| Old | New |
|---|---|
| `msgin.ErrInvalidExpression` | **`expr.ErrInvalidExpression`** — the `expr` module declares its own, prefixed `msgin/expr:`, exactly as every shipped adapter mints sentinels for its own faults |
| `msgin.ErrExprResultType` | **deleted with no replacement.** A result-type mismatch now returns `msgin.ErrPayloadType`, wrapped as `"%w: expr result %T is not %T"` |

**`ErrExprResultType` deliberately has no successor** (decision **D-K**, revised). Reusing root's existing
`ErrPayloadType` gives every future expression provider — CEL, starlark, … — **one** `errors.Is` target. No
`msgin.Permanent` wrap is needed: `ErrPayloadType` is already inside `IsPermanent`.

```go
// BEFORE
if errors.Is(err, msgin.ErrExprResultType) { … }

// AFTER
if errors.Is(err, msgin.ErrPayloadType) { … }
```

## 6. Signature and behavior changes

These are the entries a pure find-and-replace will **not** fix.

### 6.1 `StreamingSource` → `EventDrivenSource`

An EIP lexical alignment: EIP names the pattern *Event-Driven Consumer*, and "streaming" collided with the
SSE adapters. The interface is otherwise unchanged.

**The user-visible error string changed too.** `msgin.ErrUnsupportedSource` now reads:

```
msgin: source implements neither PollingSource nor EventDrivenSource
```

Any test or log assertion matching the old `StreamingSource` wording will fail. Match the sentinel with
`errors.Is`, not the string.

### 6.2 `MessageChannel` is segregated — `Subscribe` moved off it

`MessageChannel` is now **send-only** — the EIP *Pipe*. The subscribing half is a separate interface:

```go
// BEFORE — one interface did both
type MessageChannel interface {
    Send(ctx context.Context, msg Message[any]) error
    Subscribe(h MessageHandler) error
}

// AFTER
type MessageChannel interface {
    Send(ctx context.Context, msg Message[any]) error
}
type SubscribableChannel interface {
    MessageChannel
    Subscribe(h MessageHandler) (Subscription, error)
}
```

If you accept a channel **and subscribe to it**, widen the parameter to `msgin.SubscribableChannel`. If you
only send, `msgin.MessageChannel` is correct and you now accept strictly more implementations.

**`Subscription` itself is not new** — it already existed in root (declared in `pubsub.go`, returned by
`PubSub.Subscribe`) and merely moved to `channel.go`. That is why `apidiff` does not list it as an addition.

**`DirectChannel.Subscribe`'s signature also changed** — it already returned an error; what it *gained* is the
cancel handle:

```go
// BEFORE — no way to unsubscribe
func (c *DirectChannel) Subscribe(h MessageHandler) error
// AFTER — returns the handle that releases the slot
func (c *DirectChannel) Subscribe(h msgin.MessageHandler) (msgin.Subscription, error)
```

A nil handler is `(nil, ErrNilHandler)`; a second `Subscribe` while one is registered is
`(nil, ErrChannelSubscribed)`. The error path returns **no** handle, so a caller never nil-checks one it did
not obtain. **After the returned `Subscription`'s `Cancel`, the slot is free and a later `Subscribe`
succeeds** — previously the registration was permanent.

**Capability widening — this is a gain, not a break.** Because the send-only side is now the narrower
contract, several positions that previously demanded a subscriber accept **any** `MessageChannel`, including a
durable `QueueChannel`:

- `msghttp.ServeAsync`'s `target` and `stdlib.NewInbound`'s `target` — **an HTTP request can now be parked in
  a durable queue channel** instead of requiring a synchronous subscriber;
- `routing.NewRouter`'s `pick` **return** — caller-supplied code can choose a durable destination *per
  message*, which was a compile error before;
- `routing.WithOutputChannel` and `routing.WithExpiredGroupChannel` — a durable aggregator output or
  expired-group sink.

### 6.3 `WithReleaseStrategy` is retyped as well as relocated (D-E)

```go
// BEFORE — root, infallible, unnamed
func WithReleaseStrategy(fn func(MessageGroup) bool) AggregatorOption

// AFTER — routing, named, FALLIBLE
type ReleaseStrategy func(g msgin.MessageGroup) (bool, error)
func WithReleaseStrategy(fn ReleaseStrategy) AggregatorOption
```

A release decision can now **fail** rather than being forced to answer true/false. To keep the old infallible
shape verbatim, use the new sugar:

```go
// keeps a func(msgin.MessageGroup) bool exactly as written
routing.WithReleaseWhen(func(g msgin.MessageGroup) bool { return len(g.Messages()) >= 10 })
```

`WithReleaseWhen` is the **new** name. There has never been a `WithRelease`.

### 6.4 `ChannelExchange.Close` — an observable behavior change

`Close` already existed; **what it does changed**. It now cancels the reply subscription.

- **Before:** the reply receiver stayed subscribed forever, so a reply arriving after `Close` was absorbed and
  routed to `WithUnmatchedReplySink`.
- **After:** the receiver is unsubscribed, so a post-`Close` reply is the **channel's** problem — a
  `DirectChannel` returns `ErrNoSubscriber` **to the reply sender**; a `PublishSubscribeChannel` delivers it to
  whoever else is subscribed. **It never reaches `WithUnmatchedReplySink`.**

This moves a failure from a silent sink to the sender's error return. If you relied on
`WithUnmatchedReplySink` catching post-`Close` replies, that path is gone by design.

### 6.5 The exchange reply channel must be exclusive (D-J)

`NewChannelExchange` now **probes** its reply channel at construction, via the new
`msgin.ExclusiveSubscribable` interface, and rejects one with `ErrSharedReplyChannel`.

**Read the condition precisely.** The probe rejects a channel whose **policy** permits recipients other than
this exchange — *not* a channel that currently happens to have another subscriber. `SingleSubscriber` is a
statement about the channel's contract, counted across every process, and an implementation must not compute
it from a live subscriber count. A fan-out `PublishSubscribeChannel` is rejected even with zero other
subscribers attached.

The fault this catches was previously visible only as intermittent reply timeouts: every reply on a fan-out
channel is copied to *every* subscriber, so a second exchange over the same channel receives copies of replies
belonging to the first, finds no waiter for the correlation id, and diverts them.

Opt out with `endpoint.WithSharedReplyChannel()` if the sharing is deliberate. It **suppresses the probe
entirely** — consulted before the type assertion, so `SingleSubscriber` is never called.

## 7. Mechanical translation

For the 87 pure relocations, the change is the import plus the qualifier. `gopls` has **no** Move refactoring,
so drive it with the compiler:

```bash
go build ./...   # each undefined: msgin.X names the symbol; §4 names its package
```

The symbol→package map is committed and machine-readable:

```bash
grep -P '\tNewConsumer$' docs/plans/027-tools/symmap.tsv   # -> endpoint
```

## 8. What did NOT change

- **`Message[T]`, `Headers`, and every header key** — untouched.
- **The adapter SPI** — `PollingSource`, `OutboundAdapter`, `Delivery`, `NativeReliability` keep their shapes;
  only `StreamingSource`'s name moved (§6.1).
- **Every error sentinel not listed in §5.2** — same name, same package, same message.
- **`RetryPolicy`'s semantics** — only `ExponentialBackoff`'s package moved.
- **Delivery guarantees** — no adapter's at-least-once/at-most-once contract changed in this window.
- **The `adapter/` packages' own import paths** — `adapter/memory`, `adapter/cron`, `adapter/database/sql`,
  `adapter/http`, `adapter/http/stdlib` are where they were.
