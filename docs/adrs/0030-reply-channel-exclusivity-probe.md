# ADR 0030 — Reply-channel exclusivity is probed at construction, not left to godoc

- **Status:** **ACCEPTED (2026-07-28) — NOT YET IMPLEMENTED.** Decision **D-J**, settled with the user after
  the round-3 review cycle. Realized by [Plan 027 §9.6](../plans/027-core-package-layout.md); this ADR is
  written **before** the code, so every "is" below is a specification, not a description of the tree.
- **Amends** [ADR 0028 §6.2](0028-channel-interface-segregation.md) (decision D-F). It does **not** supersede
  ADR 0028: §6.2's channel-local `channel.WithSingleSubscriber()` is the mechanism this ADR makes
  load-bearing, and §6.2's rejection of a cross-exchange **registry** stands unchanged and unweakened. What
  changes is the *default posture* — from "documented, enforceable by opting in" to "**probed and rejected by
  default, with an explicit opt-out**". ADR 0028 §6.2 carries a forward pointer here.
- **Cites:** [ADR 0022 — Messaging gateway](0022-messaging-gateway.md) (the single-process correlator whose
  reply channel this protects), [ADR 0002 — Adapter SPI](0002-adapter-spi.md) (the optional-capability-probe
  precedent: `NativeReliability`, `ScheduledSender`, `LiveValueSource`).
- **Spec:** [014 §5.1](../specs/014-core-package-layout.md) · **Plan:** [027 §9.6](../plans/027-core-package-layout.md)
  · **RFC:** [0002](../rfcs/0002-eip-alignment.md)

## Context

ADR 0028 narrowed `MessageChannel` to `Send` and widened `NewChannelExchange`'s `reply` parameter to the new
`SubscribableChannel`. That widening **demoted a compile-time guarantee to a documentation request**, and the
demotion was silent:

- **Before this window**, at `main`, `DirectChannel` was the only *production* type in the tree satisfying the
  bundled `MessageChannel` (`aggregator_test.go:222`'s `failNthChannel` fake also did — Spec §1 carries the
  same caveat, and this ADR dropped it in an earlier draft), and `DirectChannel.Subscribe` returns
  `ErrChannelSubscribed` to a second subscriber. Two exchanges sharing one reply channel was therefore
  **structurally impossible** — it did not compile.
- **After it**, `reply` is any `SubscribableChannel`. Two exchanges sharing one plain
  `PublishSubscribeChannel` compiles, runs, and the non-owning exchange receives **a full copy of every reply
  belonging to the other exchange** into its `WithUnmatchedReplySink` — typically a dead-letter or audit sink,
  i.e. a place a payload is written down. Nothing errors, nothing logs.

**Three independent review lenses converged on this, and none of them individually blocked**, which is why it
is being decided rather than left in the residual pile:

| Lens | What it said |
|---|---|
| Round-3 design audit (M6) | The exclusivity guarantee is now unenforced for the pub-sub case |
| Adversarial code review | Listed as the one **design** residual after its five code findings were fixed |
| `/security-review` | Its only finding, scored **0.75** — below the reporting threshold, so it was not filed |

Convergence across three lenses that each individually scored it sub-threshold is the strongest signal the
whole review cycle produced. CLAUDE.md's sensible-defaults rule is explicit for exactly this shape: *when a
wrong default could silently mis-route, pick the value that fails safe, and prefer an explicit typed error
over a godoc warning*.

### What the tree can actually see

```
# scope: whole workspace, at dadc775
$ grep -rn "_ msgin.SubscribableChannel" --include='*.go' . | grep -v _test
channel/direct.go:29:	_ msgin.SubscribableChannel = (*DirectChannel)(nil)
channel/pubsub.go:112:	_ msgin.SubscribableChannel = (*PublishSubscribeChannel)(nil)
```

**Every in-tree `SubscribableChannel` is one of two types, both in `channel`.** A probe that those two
implement therefore covers 100% of what ships; only a third-party implementation can be unknown to it. That
ratio is what makes the "accept the unknown" arm below cheap rather than a loophole.

## Decision

### 1. Root gains an optional capability interface

```go
// ExclusiveSubscribable is the optional capability a SubscribableChannel
// implements to report whether it admits at most one subscriber at a time.
type ExclusiveSubscribable interface {
	SubscribableChannel
	// SingleSubscriber reports whether Subscribe rejects a second concurrent
	// subscriber (with ErrChannelSubscribed) while one is registered. It is a
	// report about THIS channel in THIS process, never a distributed guarantee.
	SingleSubscriber() bool
}
```

It is **optional**, in the established sense of `NativeReliability` / `ScheduledSender` / `LiveValueSource`
(ADR 0002): the core type-asserts for it and behaves correctly when the assertion fails. It is **not** added
to `SubscribableChannel`, which would break every third-party implementation.

### 2. Both in-tree channels implement it

- `channel.DirectChannel.SingleSubscriber() bool` → **always `true`**. Its `Subscribe` already returns
  `ErrChannelSubscribed`; the method makes an existing property machine-readable.
- `channel.PublishSubscribeChannel.SingleSubscriber() bool` → **`c.cfg.single`**, i.e. exactly whether D-F's
  `WithSingleSubscriber()` was passed.

> **`PubSub`'s topic channels are OUT OF SCOPE, and an earlier draft of this section got that wrong.** It read
> *"Topic channels created by `PubSub` inherit it through `withConfig`, so `NewPubSub(WithSingleSubscriber())`
> makes every topic report `true`"* — true as a statement about `withConfig`, but **useless and actively
> harmful as advice here**. `*channel.PubSub` has no `Send`, so it is not a `SubscribableChannel` and can
> never be passed to `NewChannelExchange`:
>
> ```
> vet: cannot use channel.NewPubSub(...) (value of type *channel.PubSub) as msgin.SubscribableChannel
>      value: *channel.PubSub does not implement msgin.SubscribableChannel (missing method Send)
> ```
>
> Its per-topic channels are unexported and unreachable from outside the package. Worse, a reader taking the
> sentence as a remedy would pass `WithSingleSubscriber()` to a whole registry and make **every topic**
> single-subscriber, so a second `PubSub.Subscribe` to any topic returns `ErrChannelSubscribed`
> (`pubsub_registry.go:65-73`) — breaking fan-out registry-wide to fix a reply-channel problem that registry
> never had. *(Round-4 design audit, MINOR 4.)*

### 3. `NewChannelExchange` rejects a channel that reports non-exclusive

```go
if ex, ok := reply.(msgin.ExclusiveSubscribable); ok && !ex.SingleSubscriber() && !cfg.allowShared {
	return nil, msgin.ErrSharedReplyChannel
}
```

with a new root sentinel:

```go
// ErrSharedReplyChannel is returned by endpoint.NewChannelExchange when the
// reply channel reports (via ExclusiveSubscribable) that its policy PERMITS
// more than one subscriber — not that a second subscriber exists. Such a channel
// delivers a full copy of every reply to every other subscriber that later
// attaches. Pass channel.WithSingleSubscriber() to the channel, or
// endpoint.WithSharedReplyChannel() to accept the fan-out deliberately.
ErrSharedReplyChannel = errors.New("msgin: reply channel permits multiple subscribers; it is not exclusive to this exchange")
```

> **The message says "permits", not "is shared", and that wording is decided.** The commonest trigger is a
> **sole** exchange over a plain `PublishSubscribeChannel` with **zero** other subscribers — a channel that
> demonstrably *is* exclusive in fact. An earlier draft read `"reply channel is not exclusive to this
> exchange"`, which contradicts this ADR's own Consequences (*"`ErrSharedReplyChannel` means this channel's
> policy permits sharing"*) and, under CLAUDE.md's debuggability criterion, sends the reader hunting for a
> second subscriber that does not exist. *(Round-4 design audit, MINOR 1.)*

**The check runs before `reply.Subscribe`**, so a rejected exchange leaves no subscription behind.

### 4. A channel that does not implement the probe is ACCEPTED

The `ok &&` is the whole design. A third-party `SubscribableChannel` — an adapter-supplied reply channel, a
consumer's own implementation — keeps working unchanged. **The core rejects what it can prove is wrong and
accepts what it cannot see**, rather than closing the SPI to everything that predates this interface. The
godoc on `NewChannelExchange` must say so plainly, because a caller reading "rejects a shared reply channel"
would otherwise assume a guarantee that does not extend to their own type.

### 5. `endpoint.WithSharedReplyChannel()` is the opt-out

Per CLAUDE.md's "every default is overridable": the fan-out case is legitimate — an audit or tap subscriber
alongside the exchange — and must remain expressible. The option's godoc states the consequence it is opting
into (every reply copied to every other subscriber) rather than merely naming the flag.

**It suppresses the probe; it does not confer shareability.** On a channel that enforces exclusivity itself,
the option changes nothing and the second exchange still fails:

```go
a, _ := endpoint.NewChannelExchange(reqA, direct, endpoint.WithSharedReplyChannel())  // ok
b, err := endpoint.NewChannelExchange(reqB, direct, endpoint.WithSharedReplyChannel())
// err = msgin.ErrChannelSubscribed — from DirectChannel.Subscribe, not from the probe
```

The godoc must say this outright, because neither the option's name nor `ErrChannelSubscribed`'s text hints
that the option the caller just passed cannot help. **The channel side has its own escape hatch**: a wrapper
type that is exclusive by other means can embed `*channel.PublishSubscribeChannel` and shadow the promoted
method with `SingleSubscriber() bool { return true }` — worth one sentence on `ExclusiveSubscribable`, since
it is the correct remedy for the wrapper case. *(Round-4 design audit, MINOR 3.)*

## Alternatives rejected

| Alternative | Why not |
|---|---|
| **A cross-exchange registry** | Unchanged from ADR 0028 §6.2: the core cannot see other exchanges, and a registry to make it see them is exactly the in-process global state CLAUDE.md's multi-instance rule warns against — it would appear to guarantee exclusivity while guaranteeing nothing across N instances |
| **Require `ExclusiveSubscribable` on `reply`** (change the parameter type) | Closes the SPI. Every third-party reply channel stops compiling, for a guarantee the core cannot verify anyway. Contradicts "open for extension" |
| **Type-assert the concrete `*channel.PublishSubscribeChannel`** | Introduces an `endpoint` → `channel` import edge that Spec 014 §3's dependency rule forbids, and still sees nothing outside the tree |
| **Probe by trial: `Subscribe` a no-op handler and see if it succeeds** | Side-effecting and racy — it registers a real subscriber on a live channel, and a `DirectChannel` would report "exclusive" only by having its slot taken. Rejected on debuggability grounds too: the failure would surface as a phantom subscriber |
| **Flip the default: `WithSingleSubscriber` on, `WithFanOut()` to opt back in** | Fan-out to every subscriber **is** the Publish-Subscribe Channel pattern (EIP ch.4). A channel that rejects a second subscriber by default is not that pattern, and the change would silently alter every existing pub-sub flow, not just reply channels |
| **Leave it documented (status quo)** | The failure is silent and writes payloads to another exchange's sink. Three lenses independently flagged it. Godoc is what already failed here — ADR 0028 §6.2's `reply` godoc says "REPLY MUST BE DEDICATED TO THIS EXCHANGE" today, in the same file that permits the violation |

## Consequences

- **+3 exported symbols**, 2 in root (`ExclusiveSubscribable`, `ErrSharedReplyChannel`) and 1 in `endpoint`
  (`WithSharedReplyChannel`), plus 2 methods. Root's projected count is 102 and its sentinel count 42 — see
  Spec 014 §4, and note **those are projections until Task 12 measures them**.
- **A behavior change, and it has a known caller in the tree.**
  `TestChannelExchange_sharedPubSubReplyChannel` (`endpoint/exchange_test.go:413`) builds **both** its exchanges
  over a plain `NewPublishSubscribeChannel()` and asserts `require.NoError`. Under this ADR that construction
  fails. **The test must pass `endpoint.WithSharedReplyChannel()` for its default-fan-out case** — it is
  precisely the test that pins the fan-out trade-off ADR 0028 §6.3 requires, so it must keep asserting the
  fan-out, now via the explicit opt-out. Its second case (`WithSingleSubscriber` → `ErrChannelSubscribed`)
  is unaffected: that channel reports exclusive, passes the probe, and is rejected later by `Subscribe`.
- **Two typed errors now describe adjacent conditions, and the distinction is deliberate.**
  `ErrSharedReplyChannel` means *this channel's policy permits sharing* (raised at construction, before
  subscribing, by the probe). `ErrChannelSubscribed` means *this channel is exclusive and the slot is taken*
  (raised by `Subscribe`). A caller wiring two exchanges to one `WithSingleSubscriber` channel gets the
  second; a caller wiring two to a plain one gets the first, on both. Reusing `ErrChannelSubscribed` for both
  was considered and rejected: it would report "already subscribed" for a channel that has no subscriber.
- **Hot-path branches this adds** (each needs a case, per CLAUDE.md's coverage gate): probe absent → accepted;
  probe present and `true` → accepted; probe present and `false` → `ErrSharedReplyChannel`; probe present and
  `false` **with** `WithSharedReplyChannel()` → accepted. The four are a truth table, not a list.
- The probe is a **report, not a proof**. A channel may answer `true` and still be shared by a caller who
  subscribes something else first — that path already yields `ErrChannelSubscribed`. The core's claim is
  bounded to: *it will not silently accept a channel that has told it sharing is permitted*.

## Topology (CLAUDE.md multi-instance review — mandatory)

**In-process only, by construction, and this ADR does not change that.** `ChannelExchange`'s correlator is a
map in one process's memory (ADR 0022, Spec 010 §8.1); the probe is a method call on an object in that same
process. Neither observes another instance.

**Two topologies must be reasoned about, not one.** An earlier draft of this section enumerated only the first
and concluded the probe *"adds no cross-instance state and makes no cross-instance claim"* — true of an
in-memory channel, **false of a local handle onto shared external state**, which is the only kind that will
ever exist outside `channel/`.

**Topology 1 — an in-memory reply channel per instance (the safe case).** N instances behind a load balancer
each hold their **own** `PublishSubscribeChannel`, each reports `SingleSubscriber() == true` under D-F, and
each accepts its exchange — correctly, because each *is* exclusive within its process. No cross-instance state,
no cross-instance claim.

**Topology 2 — a reply channel backed by shared external state (the case the probe CANNOT see).** A future or
third-party adapter wraps a broker subscription as a `SubscribableChannel` — a NATS subject, a Redis pub/sub
channel, an SSE stream — and implements `ExclusiveSubscribable` **honestly**: this local handle admits one
local subscriber, so `SingleSubscriber()` returns `true`. Every instance constructs its exchange; every probe
passes. The broker then fans each reply out to all N instances, and the N−1 non-owners each find no waiter for
the correlation id and hand **a full copy of another instance's reply** to their `WithUnmatchedReplySink`.

That is bit-for-bit the failure in this ADR's own Context — now cross-process, and now **endorsed by a passing
probe**. The design has no way to detect it: `ExclusiveSubscribable` is **root SPI**, i.e. precisely the
contract third-party adapters implement, and a truthful local answer is indistinguishable from a safe one.

**Consequences for the contract, all normative:**

1. `SingleSubscriber()`'s godoc states it reports **this channel, in this process** — a *report*, never a
   distributed guarantee — in the same terms ADR 0028 §6.2 uses for `WithSingleSubscriber`. Same for
   `ErrSharedReplyChannel`.
2. **`NewChannelExchange`'s godoc must state FOUR outcomes, not three**: rejected (probe reports
   non-exclusive) · accepted (probe reports exclusive) · accepted (no probe implemented) · **accepted, but
   exclusive only within this process** — the fourth is the one a caller will otherwise assume away, because
   "the core rejects a shared reply channel" reads as a guarantee. This is a Spec §8 godoc obligation, owned
   by Task 11.
3. `SingleSubscriber()` must be documented as **safe for concurrent use**. Nothing said so, and a third-party
   implementer computing exclusivity from a mutable subscriber count would introduce a data race that
   msgin's own `-race` suite can never observe, because msgin never calls it concurrently.

**The Return Address seam is unaffected — verified, not assumed.** `msgin.RequestReplyExchange` (`spi.go:118`)
is a one-method interface an external adapter implements **directly**, bypassing `NewChannelExchange`
entirely; `adapter/http/exchange.go:58` already does exactly this
(`var _ msgin.RequestReplyExchange = (*Exchange)(nil)`). D-J adds nothing to that path and constrains it in no
way. The leak is in the probe's **semantics**, not in the seam. *(Round-4 design audit, BLOCKER 1.)*

**The distributed case remains Return Address** (EIP ch.5): a reply addressed to the instance that sent the
request, carried in the message rather than resolved through a shared in-memory channel. The SPI seam for it
is the external `RequestReplyExchange` adapter named in Spec 010 §8.1 — unchanged by this ADR, and
deliberately not approximated by it. **A probe that returned `true` for a distributed channel would be a lie
of exactly the kind the registry alternative was rejected for**, which is why `SingleSubscriber`'s contract is
worded as a report about *this channel in this process*.
