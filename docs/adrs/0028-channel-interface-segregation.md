# ADR 0028 — Segregate `MessageChannel` into send-only and subscribable contracts

- **Status:** **ACCEPTED — REGENERATED FROM A GREEN TREE (2026-07-28); round-3 audit returned
  `NEEDS-REVISION` 3/3 and its findings are folded in below.** Implemented and tested in commit **`b6ce7bb`**
  (Plan 027 Task 2; `channel` measures 100% statement coverage).
  > *Round-3 corrections in this ADR:* the *"five test fakes were **deleted**"* claim is false — all five
  > survive and it was their no-op `Subscribe` **stubs** that went (Consequences); the Topology section's
  > dangling sentence is closed; the Consequences no longer imply D-F improves exclusivity **by default** (it
  > is off by default and the default wiring still silently mis-routes); and the capability-test gap is
  > restated as **five** missing positions, including the one every enumeration dropped — `NewRouter`'s `pick`
  > return (§4a row 3). Evidence: F13.
  > *Round-2 banner cleared 2026-07-28.* Every defect it named is fixed **in this ADR**, not merely noted:
  > the Context's call-site census is now **nine, stated as a scope rule** (§Context, §4a) — round 1's
  > *four of five* and round 2's *six of seven* were both produced by searching the pattern core only;
  > the Consequences no longer claim `ChannelExchange` *gains* a `Close` — it **already existed** at
  > `exchange.go:356` and §6.1 **changes what it does**; the post-`Close` reply behavior change is promoted
  > from a status-block footnote to a **decided consequence** (§6.1a); §6.2's channel-local opt-in ships as
  > `channel.WithSingleSubscriber()`, off by default (**decision D-F**); and §7 gains the **sixth row** the
  > implementation forced (stale-handle `Cancel`). Evidence: [`027-derivation-findings.md`
  > §F10](../plans/027-derivation-findings.md), F11.5.
  *(Rounds 1 and 2 both returned `NEEDS-REVISION` from all three auditors; revised in place both times rather
  than superseded because this ADR had never been implemented. See
  **[audit round 1](../plans/027-audit-round-1.md)** §K.)*
- **Decision folded in 2026-07-28:** **D-F** (`channel.WithSingleSubscriber()`), settled with the user
  2026-07-27 ([audit round 2 §G.1](../plans/027-audit-round-2.md)).
- **AMENDED by [ADR 0030](0030-reply-channel-exclusivity-probe.md)** (decision **D-J**, 2026-07-28), which
  changes **§6.2's default posture only** — from "documented, opt-in" to "probed and rejected by default". The
  rest of this ADR, including §6.2's rejection of a cross-exchange registry, stands unchanged. See the banner
  in §6.2.
- **Cites and amends:** [ADR 0013 — In-process composition](0013-composition-endpoints.md), which decided the
  bundled `MessageChannel` contract and the `To`/`OutboundAdapter` distinction. Its **audit-F2 rationale is
  voided** by Decision §2 and is annotated in place there; the *decision* it justified survives on the new
  grounds in §5.
- **Cites:** [ADR 0014 — Publish-subscribe](0014-publish-subscribe.md), which introduced the
  `Subscribe → (Subscription, error)` handle lifecycle that Decision §2 unifies on, and whose
  `subscription.Cancel` idempotency (`pubsub.go:79`) Decision §7 adopts wholesale.
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

**Eight of the nine call sites use only `Send`.** Yet the interface demands `Subscribe`, so **a
`QueueChannel`, a `PublishSubscribeChannel`, or any `OutboundAdapter` could not be used as a discard target, a
default route, a router destination, an exchange request channel, or an HTTP inbound target.** That is a
capability defect.

The **ninth** call site — `NewChannelExchange`'s `reply`, `reply.Subscribe(e.receiver())` — is the sole
genuine subscriber. That is what proves a subscribable contract earns its keep rather than being symmetry for
its own sake, and it is why the answer is **two interfaces rather than one narrowing**.

> ### The census has been wrong three times. State the SCOPE RULE, not a number.
>
> An early draft asserted *"Verified: no call site subscribes through the interface"* and then named the
> subscriber four lines later (audit E3). Round 1 replaced it with **four of five**. Round 2 "corrected" that
> to **six of seven** (§A6), adding the two Aggregator options. **Both were still wrong**, and for the same
> reason: **both searched only the pattern core and stopped at the module boundary.** The measured answer is
> **nine** (F10.2, F11.5).
>
> **Normative:** this census covers **every non-test `MessageChannel` occurrence in the workspace,
> `adapter/` included**. Re-derive it; never cite a number from a document.
>
> ```bash
> grep -rn "msgin\.MessageChannel\|MessageChannel interface" --include="*.go" . \
>   | grep -v "_test.go" | grep -v "^./docs" | grep -v '// '
> ```
>
> This is the "fix the class, not the instance" failure in its purest form: round 2 corrected the *count*
> instead of asserting the *invariant*, and the count broke again within one increment.

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

`Subscription` **stays in root** with these interfaces — it is `SubscribableChannel.Subscribe`'s return type, so
relocating it to `channel` (as the file-level move-list originally implied) would make root import a subpackage.
See ADR 0027 §4.

### 2. Unify `Subscribe` on `(Subscription, error)`

`DirectChannel.Subscribe` changes from returning `error` to returning `(Subscription, error)`, so both
subscribable channels satisfy one contract and an exchange reply channel may be either. This is the breaking
half of the change and is the point of it: without unification, `ChannelExchange`'s reply channel can only ever
be a `DirectChannel`.

Unifying *upward* (on the richer signature) rather than downward was chosen because the `Subscription` handle is
the more capable contract and `PublishSubscribeChannel` already returns it (ADR 0014); forcing pub-sub to drop
it would lose an unsubscribe capability that already exists and works.

### 3. `PollableChannel` is deliberately **not** defined

Spring's third interface has no counterpart here. It would duplicate the existing `PollingSource` SPI's exact
method set — `Poll(ctx, max) ([]Delivery, error)`, already implemented by `QueueChannel` — and **no signature in
the library would take one**. Per CLAUDE.md's small-surface rule new public surface must earn its keep, and this
can be added later non-breakingly if a caller ever appears.

Recorded explicitly so it reads as a decision, not an oversight: this is a knowing divergence from Spring's
three-way split, and RFC-0002's success metric requires every drift item to be resolved *or* kept with a written
rationale.

### 4. Call sites narrow to what they actually use

Eight positions take `MessageChannel`; one takes `SubscribableChannel`.

### 4a. The nine call sites, enumerated (illustration — the Context's scope rule is the contract)

| # | Position | File | Takes |
|---|---|---|---|
| 1 | `routing.WithDiscardChannel` | `routing/filter.go:18` | `MessageChannel` |
| 2 | `routing.WithDefaultChannel` | `routing/router.go:18` | `MessageChannel` |
| 3 | `routing.NewRouter`'s `pick` return (`RouteFunc`) | `routing/router.go:29,37` | `MessageChannel` |
| 4 | `routing.WithOutputChannel` | `routing/aggregator.go:55` | `MessageChannel` |
| 5 | `routing.WithExpiredGroupChannel` | `routing/aggregator.go:133` | `MessageChannel` |
| 6 | `endpoint.NewChannelExchange`'s `request` | `endpoint/exchange.go:225` | `MessageChannel` |
| 7 | **`msghttp.ServeAsync`'s `target`** | `adapter/http/inbound.go:116` | `MessageChannel` |
| 8 | **`stdlib.NewInbound`'s `target`** | `adapter/http/stdlib/inbound.go:33` | `MessageChannel` |
| 9 | `endpoint.NewChannelExchange`'s `reply` | `endpoint/exchange.go:225` | **`SubscribableChannel`** |

> *Corrected (round 6, M-4).* Both rows cited `endpoint/exchange.go:223`, which is a **godoc tail**
> (*"…so it must be usable now."*). The declaration is `:225`:
> `grep -n 'func NewChannelExchange' endpoint/exchange.go` → `225:func NewChannelExchange(request msgin.MessageChannel, reply msgin.SubscribableChannel, opts ...ExchangeOption) (*ChannelExchange, error)`.
> Round-4 B7 swept the Spec's echo of this citation and not the ADR's.

**Rows 4–5 and 7–8 are what the two failed censuses missed, and each is a real capability the narrowing
unlocks:**

- **4–5 (round-2 §A6):** a durable `QueueChannel` as an Aggregator **output** or **expired-group** sink —
  exactly the multi-instance capability the Topology section argues for. The §9.4 capability test must cover
  these positions, not just discard/default/request.
- **7–8 (missed by BOTH the ADR and the round-2 correction):** the **HTTP inbound handler's `target` now
  accepts a `QueueChannel`, a `PublishSubscribeChannel`, and any `OutboundAdapter`** — so an HTTP request can
  be parked in a durable queue channel instead of requiring a synchronous subscriber. That is desirable and
  **user-visible**, and no artifact recorded it until now. It belongs in both functions' godoc and in
  `MIGRATION.md`.

### 5. `MessageChannel` and `OutboundAdapter` are both kept, and the identity is documented as deliberate

The narrowed `MessageChannel` is **method-identical** to the existing `OutboundAdapter` (`spi.go:56`) — exactly
the duplication §3 refuses to create for `PollableChannel`. Two auditors caught this independently
(finding B4). Three options were weighed: collapse them, alias them, or keep both.

**Decision: keep both** (settled with the user 2026-07-27, audit §H3).

> **Governing rationale — consistency with Pipes and Filters.** EIP ch.3's foundational pattern is *filters*
> (processing steps) connected by *pipes* (channels). **`MessageChannel` IS the Pipe** — a first-class concept
> in the pattern this library's composition model is built on. `OutboundAdapter` is a **Channel Adapter** at the
> system boundary (EIP ch.4). They are **two different patterns that happen to share a method signature**, not
> two names for one thing. Collapsing them would erase the Pipe from the type system of a pipes-and-filters
> library.

Supporting, weaker arguments pointing the same way: Spring draws the same line independently (its outbound
adapters are `MessageHandler`s, distinct from `MessageChannel`), and Go's structural typing already makes the
two interchangeable at every call site, so keeping both names costs nothing in flexibility.

A **type alias** (`type MessageChannel = OutboundAdapter`) was considered and **declined**: it would prevent the
two roles ever diverging, a constraint the design does not want to commit to.

§3's "must earn its keep" rule **does not apply here** — it governs *adding* a new interface with no consumer,
whereas `MessageChannel` is existing surface with nine call sites that narrowed into coincidence. Both godocs
must state the identity explicitly, naming the other type and its pattern, so it reads as intended rather than
accidental (Spec 014 §8).

**This voids ADR 0013's F2 rationale** — *"`To` takes `OutboundAdapter`, not `MessageChannel` … a
`*memory.Broker` satisfies `OutboundAdapter` but not `MessageChannel` (no `Subscribe`)"*. After this ADR a
`*memory.Broker` satisfies both. The decision stands on the new grounds above; ADR 0013 carries an amendment
note in place.

**Consequence that must be tested:** every shipped `OutboundAdapter` silently becomes a legal discard target,
default route, router destination, and exchange request channel. That is a materially larger capability widening
than the three-row satisfaction table advertises, and it is currently untested — Spec 014 §9.4 extends the
capability test to cover `OutboundAdapter`-as-route-destination.

### 6. The exchange reply channel is exclusive — documented, and the `Subscription` is now owned

`ChannelExchange`'s dedicated-reply-channel guarantee is enforced **only** by `DirectChannel` returning
`ErrChannelSubscribed` on a second subscriber (`channel.go:31-40`). `PublishSubscribeChannel.Subscribe` has no
such guard (`pubsub.go:103-111`). Widening `reply` to `SubscribableChannel` therefore turns **two exchanges over
one pub-sub reply channel** into a valid program: every reply fans out to both receivers, and the non-owner
hands a full copy of the other exchange's reply to its `WithUnmatchedReplySink` — typically a dead-letter or
audit sink. Today that wiring is a compile error (finding B1).

1. **`NewChannelExchange` stores the `Subscription` and `Close()` cancels it.** Without this, widening
   introduces a leak that did not previously exist — there was nothing to leak while `Subscribe` returned only
   an `error`. `ChannelExchange.Close`'s godoc read *"The reply receiver remains subscribed (channels
   have no unsubscribe)"*; that sentence becomes false and is rewritten.

   > **`Close` already existed** (`exchange.go:356`, pre-window). This section **does not create it** — it
   > changes what it does. §6.1a is the consequence, and it is a decision, not an implementation detail.

#### 6.1a `Close`'s new behavior moves a failure from a silent sink to the sender — decided, not incidental

- **Before:** the reply receiver stayed subscribed forever, so a reply arriving after `Close` was absorbed by
  the receiver and routed to `WithUnmatchedReplySink`.
- **After:** the receiver is unsubscribed, so a post-`Close` reply is the **channel's** problem — a
  `DirectChannel` returns `ErrNoSubscriber` **to the reply sender**, and a `PublishSubscribeChannel` delivers
  it to whoever else is subscribed. **It never reaches `WithUnmatchedReplySink`.**

This is not a leak fix with no downside. A caller who wired `WithUnmatchedReplySink` to a dead-letter or audit
sink specifically to catch late replies will stop seeing them there, and the reply *sender* will start seeing
an error it previously never got. **That is the better behavior** — a late reply to a closed exchange is a
sender-side problem and should surface as one — but it is an **observable behavior change inside an increment
declared behavior-preserving**, so it is listed among the exceptions Spec 014 §2.1 **enumerates** — cite the
table, never a count: it grew with D-J (ADR 0030) and again with D-M (ADR 0029 §5.0b), and every prior
statement of its cardinality has gone stale within one increment. It is stated on `Close`'s
godoc, recorded in `MIGRATION.md`, and pinned by `TestChannelExchange_closeCancelsReplySubscription`
(round-2 §D5; F10.3).
2. **Exclusivity is documented by default, and enforceable by opting in (decision D-F).**

   > **AMENDED by [ADR 0030](0030-reply-channel-exclusivity-probe.md) (decision D-J, 2026-07-28).** The
   > *default posture* below — documented, opt-in — was reviewed by three independent lenses (this ADR's
   > round-3 design audit, the adversarial code review, and `/security-review`) and **all three flagged the
   > same residual**: the default wiring still silently mis-routes. ADR 0030 keeps everything in this section
   > and changes only the default: `NewChannelExchange` now **probes** the reply channel through a new
   > optional `msgin.ExclusiveSubscribable` capability and **rejects** one that reports non-exclusive
   > (`msgin.ErrSharedReplyChannel`), with `endpoint.WithSharedReplyChannel()` as the opt-out. **The
   > registry rebuttal immediately below is unchanged and is cited by ADR 0030 as still binding**;
   > `WithSingleSubscriber()` is unchanged and becomes the mechanism a `PublishSubscribeChannel` uses to pass
   > the probe. Read this section as the *reasoning*, and ADR 0030 as the *current default*.

   The `reply` godoc states that the channel must be dedicated to one exchange and names the cross-delivery
   consequence of sharing it. That much is unchanged. What an earlier draft of this section got wrong was the
   *scope of the rejection*: it rejected "a runtime guard" while only rebutting one specific design — a
   **cross-exchange registry**. That rebuttal still stands and is unchanged:

   > A registry-based guard is **rejected**. The core cannot see other exchanges, and a registry to make it
   > see them would be exactly the in-process global state CLAUDE.md's multi-instance rule warns against: it
   > would appear to guarantee exclusivity while guaranteeing nothing across N instances.

   But a registry is not the only shape a guard can take, and the draft never considered the one that costs
   nothing: a **channel-local** guard. `DirectChannel` already has exactly this — it returns
   `ErrChannelSubscribed` from its own `Subscribe`, using only state it owns, with no registry and no global.
   Rejecting *that* class of guard for `PublishSubscribeChannel` was never argued, only implied.

   **Decision: add `channel.WithSingleSubscriber()`, off by default.** It is a `PubSubOption` that makes a
   second `Subscribe` return the **existing** `msgin.ErrChannelSubscribed` sentinel while a subscriber is
   registered; the slot frees on that subscriber's `Cancel`.

   - **Why it exists.** §6's own analysis shows the failure mode is *silent*: sharing a pub-sub reply channel
     compiles, runs, and quietly hands every correlated reply to a second exchange's unmatched-reply sink.
     CLAUDE.md's sensible-defaults rule is explicit that when a wrong wiring can silently mis-route, the
     library should offer *"an explicit typed error"* rather than rely on a godoc warning. §6.3's test pins the
     behavior; it does not give the caller a way to *prevent* it. This does.
   - **Why it is off by default.** Fan-out to every subscriber **is** the Publish-Subscribe Channel pattern
     (EIP ch.4); a channel that rejects a second subscriber by default would not be that pattern. Off by
     default also means **zero behavior change** for every existing flow and every existing test — the option
     is purely additive surface.
   - **Why an option rather than a new type.** A `SingleSubscriberPubSub` type would duplicate the fan-out,
     policy, and logger surface for one boolean, and would not compose with `NewPubSub`'s per-topic
     construction. The option flows through `withConfig` to every topic channel a `PubSub` registry creates.
   - **Why it reuses `ErrChannelSubscribed`.** The condition is the same one `DirectChannel` reports, so a
     caller's existing `errors.Is` handling covers both. A new sentinel would split one contract in two.
   - **What it does NOT claim.** It is a **single-process** guard, exactly like `DirectChannel`'s. Two
     instances each holding their own `PublishSubscribeChannel` still each accept a subscriber; the
     cross-process case remains **Return Address** (see Topology below). The option must not be documented as
     a distributed exclusivity guarantee.
   - **It falsifies a "defensive, unreachable" comment, and that branch is now live.**
     `pubsub_registry.go:65`'s error arm carried *"defensive: `ch.Subscribe` only errors on a nil handler,
     already guarded above"*. Under D-F a second subscribe to an existing topic returns
     `ErrChannelSubscribed`, so the branch is reachable; the comment is rewritten and the branch is covered by
     `TestPubSub_SingleSubscriberPropagatesToTopics`. *(A guard that dropped a "created but rejected" topic
     was written and then **removed as dead code**: a topic this call creates is empty, so the `single` guard
     cannot reject the subscriber that created it. Keeping it would have added an untestable hot-path branch —
     a CLAUDE.md delivery blocker.)*
3. The two-exchanges-over-one-pub-sub case becomes a **test** asserting the documented fan-out behavior, so the
   trade-off is pinned rather than discovered in production.

### 7. `DirectChannel`'s `Subscription` semantics

The handle is new on `DirectChannel`, so its behavior is specified here rather than left to the implementation
(finding B5):

| Question | Decision |
|---|---|
| Second `Subscribe` after `Cancel()` | **Succeeds** — `Cancel` releases the slot and the channel returns to its unsubscribed state |
| `Send` between `Cancel` and the next `Subscribe` | **`ErrNoSubscriber`**, exactly as before any subscriber existed |
| `Cancel` racing an in-flight `Send` | The in-flight `Handle` **runs to completion**; `Cancel` prevents only *subsequent* dispatch — matching `PublishSubscribeChannel` |
| `Cancel` called twice | **Idempotent, never panics** (`sync.Once`, as `subscription.Cancel` already does — ADR 0014) |
| `Subscribe(nil)` | **`(nil, ErrNilHandler)`** — the error path returns no handle, so a caller never nil-checks a handle it did not obtain |
| **A stale handle's `Cancel` after a resubscribe** | **It must NOT evict the current subscriber.** The slot is released on **identity** (`if c.sub == s`) |

**The sixth row is new and was forced by the implementation, not chosen** (F10.5.1). The five rows above were
complete for the questions they asked; they did not ask this one. With a naive `sync.Once` + "clear the
handler", the sequence `Cancel` → `Subscribe` → old-handle `Cancel` **silently evicts the new subscriber** and
breaks a live flow. The identity check is the same defence `PublishSubscribeChannel.remove` and
`replyCorrelator.deregister` already use, so this is consistency rather than novelty. Covered by
`TestDirectChannel_SubscriptionLifecycle/"a stale handle's cancel does not evict the current subscriber"`.

### 8. Call-form churn is unavoidable, and it reaches two satellite modules

`DirectChannel.Subscribe` returning `(Subscription, error)` forces a **call-form** edit at every site.
Assertion *semantics* are unchanged everywhere; the edit is one of three shapes:
`require.NoError(t, ch.Subscribe(h))` → `mustSubscribe(t, ch, h)`, `if err := ch.Subscribe(h)` →
`if _, err := ch.Subscribe(h)`, or `_ = ch.Subscribe(h)` → `_, subErr := …; require.NoError(t, subErr)`.

**Two of those sites are in satellite modules, which neither this ADR nor Plan 027 Task 2 anticipated**
(F10.6): `adapter/database/sql/harness/groupstore.go:402,408` — a **non-test** file in a module with **zero
test files of its own**, so `go test` on it reports a false pass — and
`adapter/database/sql/postgres/example_sql_groupstore_test.go:54`. **This is not a root-module-only change.**

## Consequences

**Positive.** The capability defect is fixed: any channel implementation can be a discard target, a default
route, a router destination, or an exchange request channel. `MessageChannel` becomes an honest name — every
channel satisfies it — while remaining the named EIP **Pipe** rather than being folded into an adapter concept.
The interfaces state what each call site actually requires, which is both better Go and better documentation.
**It also improves the multi-instance story**: a durable `QueueChannel` now qualifies everywhere a
`DirectChannel` was previously mandatory — including as an **HTTP inbound target** (§4a rows 7–8) and as an
Aggregator **output/expired-group** sink (rows 4–5) — so a distributed deployment gains options rather than
losing them. `ChannelExchange.Close` — which **already existed** — now releases its reply subscription.

> **AMENDED by [ADR 0030](0030-reply-channel-exclusivity-probe.md) (decision D-J, 2026-07-28) — this
> paragraph's residual is CLOSED, and this window is the one that closes it.** *(Round-6 C-B6: the `AMENDED`
> banners were placed at the header and at §6.2 and stopped before the Consequences, so the three sentences
> below asserted the exact opposite of the decision this same bundle had already taken. This block is the
> amendment surface the counter-rule in [round 6 §0](../plans/027-audit-round-6.md) exists to reach.)*
>
> **Say the limit of §6's D-F plainly, because the Consequences above overstate it.** `WithSingleSubscriber`
> is **off by default and nothing in this library turns it on**, so **D-F alone** does not fix the
> mis-routing; it gives a caller who already suspects the problem a way to make it a typed error. Framing D-F
> as *"improving exclusivity"* reads as though the failure mode were addressed by default. It was not.
>
> **What is no longer true.** An earlier form of this paragraph continued: *"the default wiring is unchanged:
> two exchanges sharing one `PublishSubscribeChannel` still compiles, still runs, and still silently hands
> every correlated reply to the second exchange's unmatched-reply sink"*, and that *"`NewChannelExchange` does
> **not** opt its reply channel in — deliberately … A future increment may add an exchange-side option that
> opts in; that is not this window."* **ADR 0030 makes it this window.** `NewChannelExchange` **probes** the
> reply channel through the optional `msgin.ExclusiveSubscribable` capability and **rejects** one that reports
> non-exclusive with `msgin.ErrSharedReplyChannel`; `endpoint.WithSharedReplyChannel()` is the opt-out. The
> default wiring **is** changed: that two-exchange program no longer constructs. See ADR 0030 §3/§5,
> [Spec 014 §5.1](../specs/014-core-package-layout.md), and Plan 027 Task 9.6.
>
> **NOT YET IMPLEMENTED.** ADR 0030 is written before its code (its own status block says so); at `dadc775`
> the probe does not exist and the default wiring above still holds in the tree. Task 9.6 is what makes this
> paragraph's replacement true.
>
> **What survives unchanged.** The reasoning: the exchange does not own the channel it is handed and cannot
> know whether the caller wants fan-out — which is exactly why ADR 0030 rejects rather than silently opts in,
> and why the fan-out case stays expressible through an explicit option rather than being removed.

**Corroborating evidence the segregation was right, from the migration itself.** Five test fakes —
`fakeAggChannel`, `failNthChannel`, `idsAggChannel`, `collector`, `scriptedChannel` — each carried a no-op
`Subscribe(msgin.MessageHandler) error { return nil }` that existed **only** to satisfy the old bundled
interface. **All five of those stubs are gone; all five fakes remain**, migrated with their tests into
`routing` and `endpoint`. **Five of the six `MessageChannel` implementations in the test suite never wanted
`Subscribe` at all** (F10.6, corrected F13).

> *Corrected (round 3).* This paragraph previously said the five **fakes** were *"deleted, not migrated"*.
> They were not — only their `Subscribe` stubs were. Two greps settle it:
> `git grep -n 'func (.*) Subscribe(.*msgin.MessageHandler) error' ab233d9 -- '*_test.go'` lists exactly those
> five stubs; `grep -rn --include='*_test.go' 'Subscribe(.*MessageHandler) error' .` at `dadc775` returns nothing,
> while all five type declarations are still present. The stub deletion is the **better** evidence, since it
> isolates the unwanted method rather than confounding it with the fake's disposal.

**Negative, accepted.**

- `DirectChannel.Subscribe`'s signature change breaks every implementer and caller. Pre-v1 with zero tags, so
  the cost is mechanical; recorded in `MIGRATION.md`.
- Two interfaces where there was one, **and** one of them is method-identical to a third (`OutboundAdapter`).
  Justified by §5's pattern argument, at the cost of a reader having to learn why two names share a shape.
- **Reply-channel exclusivity degrades from compile-enforced to documented** for pub-sub reply channels, and
  from documented to **opt-in-enforceable** with D-F. That was the real price of the widening — mitigated by
  §6's godoc, its test, and `WithSingleSubscriber()` rather than eliminated.
  > **AMENDED by ADR 0030 (D-J) — the residual is no longer "mitigated rather than eliminated" for the case
  > the core can see.** `NewChannelExchange` rejects a reply channel that reports non-exclusive, so the
  > degradation is repaired by default for both in-tree `SubscribableChannel` types. What remains is
  > **bounded and stated**: a channel that does not implement `msgin.ExclusiveSubscribable` is accepted
  > (ADR 0030 §4), a decorator that embeds the `msgin.SubscribableChannel` *interface* does not inherit the
  > probe (ADR 0030 §1/§4, decision **D-L**), and no local answer can speak for another process (ADR 0030
  > §Topology). **NOT YET IMPLEMENTED** — Plan 027 Task 9.6.
- **`Close`'s post-close reply behavior changes** (§6.1a) — a late reply now errors at the sender instead of
  landing in `WithUnmatchedReplySink`.
- **Call-form churn at every `Subscribe` site, including two satellite-module files** (§8).
- Any third-party type that implemented the old bundled `MessageChannel` now satisfies only the send half. No
  such type exists (nothing is released), but the migration note must say so plainly.

**Risk to test for.** A narrowed parameter type silently accepts more implementations, so a regression here is
invisible to the compiler. Spec 014 §9.4 therefore requires a test proving a `QueueChannel`, a
`PublishSubscribeChannel`, **and** a shipped `OutboundAdapter` each work at **all eight** send-only positions
§4a enumerates. The shipped test covers **three** (rows 1, 2, 6); rows **3, 4, 5, 7, 8** are missing.
**Row 3 — `NewRouter`'s `pick` return — has been dropped from every enumeration of the gap so far**, and it is
the position that matters most: it is the only one where the channel is chosen at **message time** by
*caller-supplied* code, so it is where a widened `MessageChannel` actually buys a durable destination per
message. The test must fail against the pre-window code, or it is not testing the fix.

**And the RED artifact needs its own technique.** All root tests are one `package msgin_test` binary, so a
capability test that fails to *compile* takes the whole binary down and produces **no `FAIL` line**. The
transcript must come from `go test -c -o /dev/null .` — `go vet .` shows only the **first** of the three
type errors, because it stops after one error batch (F10.1).

**Topology.** `SubscribableChannel` remains an **in-process contract**: a Go channel cannot cross a process
boundary, so a reply arriving at instance B for a request made on instance A is not reachable through it.
*(Round-3 fix: this sentence previously ended on a dangling "The", orphaned by the blockquote below; the
continuation is the paragraph that follows it.)*

> **`channel.PubSub` is in-process too, and the mandatory review omitted it** (round-2 §D3). It is a
> `map[string]*PublishSubscribeChannel` guarded by a local mutex (`channel/pubsub_registry.go:13`): two
> instances each hold their own registry, and a `Publish` on instance A never reaches a subscriber on
> instance B. Its godoc says "in-process" but does not name the distributed answer, which is **a native-topic
> broker adapter implementing the root `TopicPublisher`/`TopicSubscriber` SPI** (Kafka/NATS/Redis topics).
> ADR 0027 §4a (decision D-B) keeps that seam in root **precisely so** the adapter can supply it without a
> core change — which is the concrete payoff of D-B, not just a filing preference.
>
> **`channel.WithSingleSubscriber()` is likewise a single-process guard** and must never be documented as a
> distributed exclusivity guarantee.

The distributed answer stays the **Return Address** pattern via a future external `RequestReplyExchange` adapter
(Spec 010 §8.1, ADR 0022) — whose interface ADR 0027 §4 deliberately keeps in root so the implementation can
live outside. This ADR must not narrow that seam shut, and the godoc must say so. §6's exclusivity rule is
likewise an in-process statement: the cross-process equivalent (two instances sharing a durable reply topic) is
what Return Address solves by carrying the reply destination in the message.
