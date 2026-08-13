# ADR 0030 — Reply-channel exclusivity is probed at construction, not left to godoc

- **Status:** **ACCEPTED (2026-07-28) — IMPLEMENTED (2026-08-10) by [Plan 027 §9.6](../plans/027-core-package-layout.md).**
  Decision **D-J**, settled with the user after the round-3 review cycle. This ADR was
  written **before** the code, so the "is" statements below were a specification when written; §1's godoc,
  §3's guard and §3a's helper are now copied verbatim into `channel.go`, `endpoint/exchange.go` and
  `errors.go`, and Spec 014 §8 gates 8.10/8.11/8.11a/8.12/8.13 are GREEN.
- **Decisions folded in 2026-07-28 (round 6):** **D-L** — `SingleSubscriber()` is an **end-to-end policy
  predicate** and is **lifetime-invariant**, superseding §1's original handle-local definition
  ([round 6 §1](../plans/027-audit-round-6.md), prompted by design blockers B2/B3, both **compile-proven** in a
  throwaway worktree at `aae6160`); and **D-M2** — the `cfg.allowShared` opt-out is evaluated **before** the
  probe, so it suppresses the probe rather than only the rejection (§3). D-L rewrites §1's godoc, restates §4's
  cheapness argument, adds the embedding/wrapper hazards to §5, deletes a Consequences bullet that D-L
  falsifies, and **narrows §Topology's conclusion about Topology 2**.
- **Decisions folded in 2026-07-28 (round 7):** **D-L (REVISED)** — the predicate counts **RECIPIENTS REACHED**,
  not **PROCESSES TRAVERSED**; round 6's two normative sentences gave opposite answers for a per-instance NATS
  `_INBOX` / exclusive auto-delete AMQP reply queue, which is the canonical **Return Address** implementation
  ([round 7 §1](../plans/027-audit-round-7.md), design B2). It rewrites §1's godoc a second time and **narrows
  §Topology's reversal claim to the BROADCAST case** — a private inbox is Topology 1 with a broker in the
  middle. **D-O** — `SingleSubscriber` **MUST NOT block and MUST NOT panic**, and msgin defends against the
  panic by recovering it and **failing closed** (§3a); the blocking case is undefendable and is a stated MUST
  (round 7 §1, design B3, compile-proven). **D-M5** — the wrapper invariant is stated **by shape**, not by
  mechanism (§4). **D-M6** — the soft opt-in alternative is weighed and rejected in the Alternatives table.
- **Decisions folded in 2026-07-30 (round 8):** **D-O2** — `safeSingleSubscriber` returns `(bool, error)` and
  the guard wraps the recovered panic into `ErrSharedReplyChannel`, because D-O as recorded **destroyed the
  evidence of the fault it recovers from and reported a false diagnosis**: a genuinely exclusive channel whose
  probe panics was rejected with *"it is not exclusive to this exchange"* and, under the default discard
  logger, nothing survived at all ([round 8 §2](../plans/027-audit-round-8.md), design B1, compile-proven).
  §3a carries the measurement, the fix, and the proof that **no gate moves**; §3's guard snippet and
  `ErrSharedReplyChannel`'s godoc (a **third** cause) are updated to match.
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
implement therefore covers 100% of what ships.

> **The sentence that used to follow is FALSE and is withdrawn — decision D-L (round 6, compile-proven).** It
> read: *"only a third-party implementation can be unknown to it. That ratio is what makes the 'accept the
> unknown' arm below cheap rather than a loophole."* **That counts declarations; the arm is reached by the
> value a caller passes.** `struct{ msgin.SubscribableChannel }` — the one-line decorator — wraps an in-tree
> fan-out channel and is **accepted** where the bare channel is **rejected**, with no third party anywhere.
> The corrected cheapness argument, and the godoc that carries the mitigation, are in **§4**.

## Decision

### 1. Root gains an optional capability interface

> **SUPERSEDED IN PLACE by decision D-L (round 6).** The original definition — *"reports whether `Subscribe`
> rejects a second concurrent subscriber … a report about THIS channel in THIS process"* — was **handle-local**,
> and three defeats were **compile-proven** against it by implementing D-J in a throwaway worktree at `aae6160`
> and running this ADR's own guard:
>
> ```
> bare plain pub-sub                      -> REJECTED ErrSharedReplyChannel
> A: struct{ msgin.SubscribableChannel }  -> ACCEPTED (probe absent)
> B: struct{ *PublishSubscribeChannel }   -> ACCEPTED (probe reports exclusive)
>    ...after 2 Subscribes: len(subs)=2, SingleSubscriber()=true
> C: state-reading probe (0 subscribers)  -> ACCEPTED (probe reports exclusive)
>    ...after 2 Subscribes: n=2, SingleSubscriber()=false (both accepted, no error)
> ```
>
> - **B** — a wrapper embedding a `*PublishSubscribeChannel` that was itself built with
>   `WithSingleSubscriber()` (so the *embedded* channel answers honestly) and **overriding `Subscribe` with its
>   own multi-subscriber dispatch**. It **inherits `SingleSubscriber` by method promotion**, so it reports
>   `true` about the embedded object while its own subscriber list grows to two. §5 below presented
>   embed-and-shadow as the *remedy* and never said promotion is also the *hazard*.
> - **C** — a third-party probe that reads its own live subscriber count is exactly what the old *"while one is
>   registered"* phrasing invited. It answers `true` at construction and then admits N with **no error at all**
>   (both `Subscribe` calls returned a nil error).
> - **A** — `struct{ msgin.SubscribableChannel }`, the idiomatic one-line decorator for logging/metrics/tracing,
>   promotes `Send` and `Subscribe` but **not** `SingleSubscriber` (the type assertion to the capability simply
>   fails). The *same* fan-out channel that is rejected bare is accepted when wrapped.
>
> *Independently re-derived in the round-6 fix pass* against the tree at `aae6160`, by declaring the D-J
> capability and §3's guard in a throwaway `msgin_test` file over the real `channel` package, and deleted
> afterwards — the four lines above reproduce **exactly**, including B's `len(subs)=2, SingleSubscriber()=true`
> and C's two nil errors.
>
> The definition below is the decided one. The three changes are all to the **contract**, not the
> implementation: an end-to-end predicate, a lifetime-invariance requirement *alongside* (not replacing)
> concurrency-safety, and both embedding directions documented.

> **SUPERSEDED IN PLACE A SECOND TIME by decision D-L (REVISED) — round 7, design B2.** The round-6 godoc
> **gave opposite answers for the same channel**, because its two normative sentences measured different
> things. The withdrawn text read:
>
> > *"SingleSubscriber reports whether **THIS exchange will be the sole recipient** of every message sent to
> > this channel"* … and … *"A channel whose deliveries **reach other processes** (a broker subject, a Redis
> > pub/sub channel, an SSE stream) MUST return false even when its local handle admits one subscriber — the
> > core has no other way to learn that replies fan out beyond this process."*
>
> A per-instance NATS `_INBOX.<nuid>` reply subject — or an exclusive auto-delete AMQP reply queue — answers
> **`true`** to sentence 1 (this exchange *is* the sole recipient of everything sent there) and **`false`** to
> sentence 2 (it is literally *"a broker subject"*, sentence 2's own first example). That channel is **the
> canonical Return Address implementation**, the pattern ADR 0022 and §Topology below both name as *the*
> distributed answer, so the round-6 wording **made a correct implementation unrepresentable** — and it worked
> under the superseded handle-local definition, which is how the regression got in.
>
> **Secondary defect (round-7 design M3): *"THIS exchange"* is unanswerable by the method it is written on.**
> `SingleSubscriber()` is nullary, lives on the **channel**, and is called **before** the exchange subscribes,
> so no implementation can observe which exchange is asking.
>
> **The root error was the unit of measure: the predicate was worded about PROCESSES TRAVERSED when the
> property that matters is RECIPIENTS REACHED.** Everything else D-L decided — MUST NOT compute from a live
> subscriber count, lifetime-invariance, concurrency-safety, EMBEDDING CUTS BOTH WAYS — is **unchanged** and
> carries forward verbatim into the text below.

> **D-O (round 7, design B3, compile-proven) adds one further normative clause below:** `SingleSubscriber`
> **MUST NOT block and MUST NOT panic**. §3a carries the guard-side defence and the four in-repo authorities
> that already forbid calling caller code unguarded inside a constructor.

```go
// ExclusiveSubscribable is the optional capability a SubscribableChannel
// implements to report whether every message sent to it reaches at most one
// recipient.
type ExclusiveSubscribable interface {
	SubscribableChannel
	// SingleSubscriber reports whether every message sent to this channel
	// reaches at most one recipient, counted across every process. It is a
	// statement about the channel's POLICY, not its current subscriber count:
	// an implementation MUST NOT compute it from a live subscriber count.
	//
	// A channel MUST return false whenever a message sent to it can be received by
	// any recipient other than the single subscriber registered on it — INCLUDING a
	// recipient in another process. A broadcast broker subject, a Redis pub/sub
	// channel, or an SSE stream fanned out to N instances MUST therefore return
	// false even when its local handle admits one subscriber. A broker-backed
	// channel MAY return true only when the broker guarantees the destination is
	// private to this process's subscription — a per-instance NATS _INBOX reply
	// subject, an exclusive auto-delete AMQP reply queue. That is the Return
	// Address pattern, and it is what an honest true means here.
	//
	// SingleSubscriber MUST NOT block and MUST NOT panic. msgin calls it inside
	// NewChannelExchange, on the caller's goroutine, with no context and no
	// timeout; it must be a constant-time accessor over state fixed at
	// construction. A panic is recovered and read as false (fail closed), and a
	// blocking implementation hangs the constructor with nothing to cancel.
	//
	// The value MUST be constant for the lifetime of the channel. msgin calls it
	// once, at construction, and treats the answer as an invariant; a value that can
	// change afterwards makes the check a TOCTOU race the core cannot detect.
	// Implementations must also be safe for concurrent use.
	//
	// EMBEDDING CUTS BOTH WAYS. A type that embeds a *channel.DirectChannel or a
	// *channel.PublishSubscribeChannel inherits SingleSubscriber by method
	// promotion, so it reports on the EMBEDDED channel even when it overrides
	// Subscribe with its own multi-subscriber dispatch. A wrapper that changes
	// subscription behavior MUST declare its own SingleSubscriber.
	SingleSubscriber() bool
}
```

**That godoc text is normative and must be copied VERBATIM**, not paraphrased: Plan 027 Task 9.6 writes it
and gates it with `go doc`-extracted property assertions (a `grep -A`/`-B` gate on a doc comment reads the
wrong lines — [round 6 §0](../plans/027-audit-round-6.md), counter-rule 3). **Its line breaks are no longer
load-bearing for the gate** — see the normalizer below — but its *wording* is: every conjunct of gate 8.11 is
a literal span of the text above.

> **The gate normalizes `go doc`'s output before matching, and it has to.** Measured in a probe module,
> re-measured 2026-07-30: interface **method** comments print **verbatim**, `//` markers and source line breaks
> intact, so `grep -q 'INCLUDING a recipient in another process'` fails on the text above (it reads
> `… INCLUDING a // recipient in another process`); **func**, **type** and **var** comments are **re-wrapped**
> at ~76 columns. Both can split a gate phrase and produce a **false RED**. The canonical gate block
> (Spec 014 §8.0b ≡ Plan 027 §11) therefore pipes every `go doc` through
> `sed 's,//, ,g' | tr -s '[:space:]' ' '` first. *(Round-7 owner 1; generalizes D-M1/R-M5, which had
> corrected only the count-lines-not-matches shape of obligation 12's gate.)*
>
> > **ROUND-8 CORRECTION (C3) — the pipe is now ACTUALLY IN both gate blocks, and half of the reason above was
> > wrong.** When round 8 audited this sentence it had **one hit repo-wide: itself.** Neither gate document
> > used the form the ADR published as canonical, and the sentence read as though they did. It has been
> > **adopted** in both blocks rather than dropped, because the interface-method half is real and measured, and
> > because adopting it flips no verdict: with ADR 0030 §1's godoc pasted verbatim into a probe module,
> > **all 14 conjuncts of gates 8.10/8.11/8.11a/8.12/8.13 MATCH under both the piped and the un-piped form**,
> > while line-break-spanning phrases match only under the pipe:
> >
> > ```
> > INCLUDING a recipient in another process     raw=NO     piped=MATCH     (interface method comment)
> > MUST therefore return false                  raw=NO     piped=MATCH     (interface method comment)
> > the probe at all; any wrapper                raw=NO     piped=MATCH     (func comment, re-wrapped)
> > ```
> >
> > **The func/type half's stated trigger was half-wrong.** It claimed the gate *"flips MATCH→NO-MATCH when the
> > **preceding sentence** changes length"*. `go doc` re-wraps **each block independently**, so a length change
> > in a preceding paragraph or list item cannot move a later phrase's line breaks: **0 of 46** perturbations of
> > the intro paragraph flipped `grep -q 'does not implement'`. Perturbing text **inside the phrase's own
> > wrapped block** flips it **18 of 46** (first at a 23-character shift). The hazard is real and the pipe is
> > the right fix; the trigger is *same-block* edits, not preceding ones.

**Invariance and concurrency-safety are two requirements, and neither replaces the other.** Concurrency-safety
is the *weaker* property: a race-free `atomic.Load(&n) == 0` is concurrency-safe and still lies, because the
failure mode is TOCTOU, not a data race.

It is **optional**, in the established sense of `NativeReliability` / `ScheduledSender` / `LiveValueSource`
(ADR 0002): the core type-asserts for it and behaves correctly when the assertion fails. It is **not** added
to `SubscribableChannel`, which would break every third-party implementation.

### 2. Both in-tree channels implement it

- `channel.DirectChannel.SingleSubscriber() bool` → **always `true`**. Its `Subscribe` already returns
  `ErrChannelSubscribed`; the method makes an existing property machine-readable.
- `channel.PublishSubscribeChannel.SingleSubscriber() bool` → **`c.cfg.single`**, i.e. exactly whether D-F's
  `WithSingleSubscriber()` was passed.

**Both satisfy D-L's two new requirements without any extra work, which is the point of it.** Each answer is a
**policy** fixed at construction (`DirectChannel`'s by type, `PublishSubscribeChannel`'s by option), so each is
constant for the channel's lifetime and trivially safe for concurrent use; neither reads a live subscriber
count. D-L constrains third-party implementations, not these two.

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
if !cfg.allowShared {
	if ex, ok := reply.(msgin.ExclusiveSubscribable); ok {
		single, cause := safeSingleSubscriber(ex, cfg.logger)
		if !single {
			if cause != nil {
				return nil, fmt.Errorf("%w: %w", msgin.ErrSharedReplyChannel, cause)
			}
			return nil, msgin.ErrSharedReplyChannel
		}
	}
}
```

*(The call goes through `safeSingleSubscriber` — §3a, decision **D-O**, amended by **D-O2** (round 8, B1) to
return `(bool, error)` so a recovered panic survives in the returned error. Everything else about the guard,
including D-M2's ordering below, is unchanged.)*

> **The opt-out is tested FIRST, and the order is decided — decision D-M2 (round 6).** An earlier draft wrote
> the conjunction as `ok && !ex.SingleSubscriber() && !cfg.allowShared`, which calls `SingleSubscriber()`
> **before** consulting the opt-out. `WithSharedReplyChannel()` would then suppress the *rejection* but not the
> *probe* — contradicting Plan 027's requirement that the option's godoc say it **suppresses the probe**, and
> making a caller who has explicitly opted out still pay for a third-party implementation that locks or does
> work in the method. Under D-L the method is a constant-returning accessor for both in-tree types, so the cost
> is nil there; the ordering matters for the implementations the core cannot see, which is the same population
> §4 is written for.

with a new root sentinel:

```go
// ErrSharedReplyChannel is returned by endpoint.NewChannelExchange when the
// reply channel reports (via ExclusiveSubscribable) that its POLICY permits
// recipients other than this exchange — not that another subscriber exists.
// That covers a local fan-out channel and a channel whose deliveries reach
// other processes alike. Such a channel delivers a full copy of every reply to
// every other recipient. Pass channel.WithSingleSubscriber() to the channel, or
// endpoint.WithSharedReplyChannel() to accept the fan-out deliberately.
//
// THERE IS A THIRD CAUSE, and it is not a policy report at all: the channel's
// SingleSubscriber PANICKED. msgin recovers it and fails closed (the probe
// proved nothing, so the conservative answer is non-exclusive), and the
// recovered value is WRAPPED INTO THIS ERROR — read err.Error(), or errors.As
// past this sentinel, before hunting for a second subscriber. The channel may
// well be exclusive; what failed is the probe. See ExclusiveSubscribable's
// "MUST NOT block and MUST NOT panic" clause.
ErrSharedReplyChannel = errors.New("msgin: reply channel permits multiple subscribers; it is not exclusive to this exchange")
```

> **The message says "permits", not "is shared", and that wording is decided.** The commonest trigger is a
> **sole** exchange over a plain `PublishSubscribeChannel` with **zero** other subscribers — a channel that
> demonstrably *is* exclusive in fact. An earlier draft read `"reply channel is not exclusive to this
> exchange"`, which contradicts this ADR's own Consequences (*"`ErrSharedReplyChannel` means this channel's
> policy permits sharing"*) and, under CLAUDE.md's debuggability criterion, sends the reader hunting for a
> second subscriber that does not exist. *(Round-4 design audit, MINOR 1.)*
>
> **D-L widens the predicate; the string is kept, and the godoc carries the width.** Under D-L a broker-backed
> channel answers `false` because its deliveries leave the process, not because a second *local* `Subscribe`
> would succeed — so *"permits multiple subscribers"* names the commonest instance rather than the whole
> condition. The message is retained as decided (round-4 MINOR 1 settled it, and it is right for every in-tree
> case), and the **godoc above** is what states the general condition; a caller who lands on the cross-process
> case reaches it through `SingleSubscriber()`'s own contract. Recorded here so the narrowness is a known,
> accepted limit of one error string rather than a contradiction found by the next audit.

**The check runs before `reply.Subscribe`**, so a rejected exchange leaves no subscription behind.

### 3a. The probe MUST NOT block or panic, and the guard FAILS CLOSED on a panic — decision D-O

**Compile-proven** in a throwaway worktree (round 7, design B3): a probe that panics and a probe that blocks
are both reachable through the SPI, and neither is defended against today.

```
D1 panicking probe  -> PANIC ESCAPES NewChannelExchange
D2 blocking probe   -> NewChannelExchange HUNG (no ctx, no timeout)
    goleak: Goroutine in state select (no cases), blockProbe.SingleSubscriber
            on top of endpoint.NewChannelExchange(...) exchange.go:249
```

*(Transcript quoted verbatim from [round 7 §1](../plans/027-audit-round-7.md). The `exchange.go:249` frame is
the line number **in the D-J-applied worktree**, not in the committed tree — at `c4582ba`, `exchange.go:249`
is `if err != nil {`. Do not use it as a citation into `main`.)*

**Four authorities already in this repo forbid the unguarded call**, which is why this is a correction rather
than a new policy:

1. **CLAUDE.md** — *"library code must not call `os.Exit`, `log.Fatal`, or `panic` on caller input (return
   errors instead)"*, plus the "Fault isolation & recovery" constraint.
2. **`ErrUnboundedRetry`'s own godoc**, `errors.go:53-56` verbatim (`sed -n '53,56p' errors.go` at `c4582ba`):

   ```
   	// The check is deliberately STRUCTURAL — it tests Backoff for nil rather than
   	// evaluating it — because BackoffStrategy is a public interface and calling
   	// caller code inside a constructor may panic, may block, and is
   	// non-deterministic for a jittered strategy. Two consequences follow, both
   ```

   That paragraph exists to forbid exactly what an unguarded `SingleSubscriber()` call in
   `NewChannelExchange` would do. The difference is that `BackoffStrategy` had a **structural** substitute (test
   the field for nil); `ExclusiveSubscribable` has none — the answer *is* the method — so the defence has to be
   a recover, not an avoidance.
3. **The `safeX` class** — `endpoint/consumer.go` carries **eleven** recover-wrappers for third-party interface
   methods (`safeLimiterWait`, `safeAllow`, `safeTryProbe`, `safeRecord`, `safeHalfOpen`, `safeFire`,
   `safeDecode`, `safeSend`, `safeAck`, `safeNack`, `safeHandle` — verified, `grep -c 'recover()'
   endpoint/consumer.go` → 11). `SingleSubscriber` would be a twelfth call site into caller code, sitting
   outside the class.
4. **`ErrNilSubscription`'s godoc** (`errors.go:143-148`), 20 lines from this very guard: *"SubscribableChannel
   is public SPI that third-party adapters implement, so a faulty implementation is caller input: it is
   rejected at CONSTRUCTION with this typed error … rather than deferred into a nil-pointer panic."*

**(a) Wrap the call and FAIL CLOSED.** A probe that panics has **not proven exclusivity**, so the recovered
value is `false` — CLAUDE.md's *"default to the safe, conservative value, not the permissive one"*. The
channel is then rejected with `ErrSharedReplyChannel`, which is the honest outcome: msgin could not obtain an
exclusivity report.

> ### D-O2 (round 8, design B1, compile-proven) — the recovered panic RIDES IN THE ERROR, not only in a log
>
> D-O as recorded in round 7 returned a bare `bool` and surfaced `r` **only** through `cfg.logger`. Measured
> in a throwaway worktree at `7ee3fd6`, with that helper implemented exactly as it was written and a
> **genuinely exclusive** channel (embedding `*channel.DirectChannel`, whose second `Subscribe` returns
> `ErrChannelSubscribed`) whose `SingleSubscriber` panics:
>
> ```
> channel is genuinely exclusive: second Subscribe -> msgin: channel already has a subscriber (Is ErrChannelSubscribed=true)
>
> err                                      = "msgin: reply channel permits multiple subscribers; it is not exclusive to this exchange"
> errors.Is(err, ErrSharedReplyChannel)    = true
> panic value recoverable from err         = false
> errors.Unwrap(err)                       = <nil>
> anything on stderr/stdout from the logger= (nothing: cfg.logger defaults to io.Discard)
> ```
>
> **The error states the opposite of the truth and the evidence is gone.** The channel *is* exclusive; the
> error says *"it is not exclusive to this exchange"*; and under `NewChannelExchange`'s **default** logger —
> `slog.New(slog.NewTextHandler(io.Discard, nil))`, `exchange.go:232` — the panic value survives nowhere at
> all. That is a direct hit on CLAUDE.md's debuggability criterion (*"prefer typed, wrapping errors that name
> the offending field/input over opaque failures"*).
>
> **The repo already states the rule this violated**, in the godoc of the one `safeX` member that deliberately
> does *not* log (`endpoint/poller.go:100-105`): *"safePoll does NOT log — pollLoop's existing error path
> already logs … with this error, **whose text carries the recovered panic value**"*. The log is allowed to be
> redundant with the error; it is not allowed to be the error's only carrier. Measured across the workspace,
> **every** recover-wrapper around caller code that returns an error embeds `%v` of the recovered value in it —
> eight of eight: `endpoint/consumer.go:863` (`safeDecode`), `:885` (`safeSend`), `:909` (`safeAck`), `:921`
> (`safeNack`), `:935` (`safeHandle`), `endpoint/poller.go:109` (`safePoll`), `endpoint/producer.go:563`
> (`safeDeadLetter`), `channel/pubsub.go:203` (`safeFanOut`). The ninth, `safeLimiterWait`
> (`endpoint/consumer.go:514`), returns `err = nil` **deliberately** (fail *open*, unpaced) and surfaces `r`
> through `governorPanic`; it is the exception that has no error to carry anything. Round-7's
> `safeSingleSubscriber` would have been the only member that **has** an error path and discards the cause.
>
> **Decision.** `safeSingleSubscriber` returns `(bool, error)`; the guard wraps with
> `fmt.Errorf("%w: %w", msgin.ErrSharedReplyChannel, cause)`. Re-measured with the fix applied, same worktree,
> same channel:
>
> ```
> err                                      = "msgin: reply channel permits multiple subscribers; it is not exclusive to this exchange: SingleSubscriber panicked: probe: nil map read in tenantExclusivity[tenant]"
> errors.Is(err, ErrSharedReplyChannel)    = true
> panic value recoverable from err         = true
> ```
>
> **Fail-closed is unchanged, no new sentinel is introduced, the WARN stays, and NO GATE MOVES** — `errors.Is`
> still reports `true` (the double-`%w` wrap keeps the sentinel in the chain), `safeSingleSubscriber` is
> unexported so no `go doc` gate can see its signature, and the five D-J/D-L/D-O gates (§8.10, 8.11, 8.11a,
> 8.12, 8.13) were run against **both** implementations in that worktree with §1's godoc pasted verbatim and
> printed `GREEN` five-for-five under each. *(One caveat for a future gate author: `errors.Unwrap(err)`
> returns `nil` on a two-verb `%w` wrap — the chain is `Unwrap() []error`. Assert with `errors.Is` /
> `errors.As`, never `errors.Unwrap`.)*

```go
// safeSingleSubscriber invokes the caller-supplied probe, recovering a panic to
// FAIL CLOSED (report non-exclusive) AND returning the recovered value as an
// error so the guard can wrap it into ErrSharedReplyChannel. A probe that
// panics has not proven exclusivity, so the conservative answer is false and
// the exchange is rejected — but the panic is what the caller has to fix, and a
// channel that is genuinely exclusive would otherwise be reported as "not
// exclusive to this exchange" with the cause discarded. Mirrors the eight
// recover-wrappers that embed %v of the recovered value in the error they
// return (consumer.go, poller.go, producer.go, channel/pubsub.go).
func safeSingleSubscriber(ex msgin.ExclusiveSubscribable, log *slog.Logger) (b bool, cause error) {
	defer func() {
		if r := recover(); r != nil {
			b, cause = false, fmt.Errorf("SingleSubscriber panicked: %v", r)
			log.Warn("msgin: reply channel's SingleSubscriber panicked; treating it as non-exclusive",
				"panic", r)
		}
	}()
	return ex.SingleSubscriber(), nil
}
```

> *The `log *slog.Logger` parameter is how the round-7 record's "surface the recovered value through the
> exchange logger" is realized; the record's §1 snippet shows the recover mechanic only, and its two-line body
> has nowhere to surface `r`. `cfg.logger` is already populated at this point in `NewChannelExchange`
> (`exchange.go:232` defaults it to a discard handler) and `WithExchangeLogger`'s godoc already requires that
> the logger neither panic nor block, so no new constraint is introduced. Recorded as a deliberate,
> one-parameter deviation rather than applied silently. **The WARN is retained under D-O2 and is now
> redundant-by-design rather than load-bearing** — exactly the relationship `safePoll`'s godoc describes.*

**(b) Blocking cannot be defended against — it is a stated MUST.** Moving the probe onto a goroutine with a
timeout would leak that goroutine for the process's lifetime (the blocked call never returns) and would trade
a visible hang for a silent one. §1's godoc therefore states outright: *"SingleSubscriber MUST NOT block and
MUST NOT panic. msgin calls it inside NewChannelExchange, on the caller's goroutine, with no context and no
timeout; it must be a constant-time accessor over state fixed at construction."* Under **D-M2** a caller who
has passed `WithSharedReplyChannel()` never reaches the call at all, which is the only mitigation available
for the blocking case.

### 4. A channel that does not implement the probe is ACCEPTED

The `ok &&` is the whole design. A third-party `SubscribableChannel` — an adapter-supplied reply channel, a
consumer's own implementation — keeps working unchanged. **The core rejects what it can prove is wrong and
accepts what it cannot see**, rather than closing the SPI to everything that predates this interface. The
godoc on `NewChannelExchange` must say so plainly, because a caller reading "rejects a shared reply channel"
would otherwise assume a guarantee that does not extend to their own type.

> **The cheapness argument is RESTATED — decision D-L (round 6, design B3, compile-proven.)** The Context's
> *"What the tree can actually see"* section used to conclude, from a grep of `var _ msgin.SubscribableChannel`
> **declarations**, that *"only a third-party implementation can be unknown to it"* and therefore that the
> accept-unknown arm is cheap. That sentence is withdrawn there and its replacement points here, because
> **it counts the wrong thing.** The arm is reached by the **value a caller passes**, and an in-tree channel
> reaches it through one line of ordinary Go:
>
> ```go
> type logged struct{ msgin.SubscribableChannel }   // the idiomatic logging/metrics/tracing decorator
> // promotes Send and Subscribe; does NOT promote SingleSubscriber
> ```
>
> `logged{plainPubSub}` is **accepted** where `plainPubSub` is **rejected**, with no third party involved
> (case **A** of §1's transcript). Embedding an *interface* forwards only that interface's method set, so every
> such decorator silently opts out of the probe.
>
> **The arm is still the right design, on a corrected and narrower claim.** What it buys is unchanged — the SPI
> stays open, and closing it would break every third-party reply channel for a guarantee the core cannot verify
> anyway (see the Alternatives table). What it costs is **larger than stated**: the accept-unknown arm is
> reachable **from in-tree types via wrapping**, not only from outside the repo. The mitigation is
> documentation, and it is normative: §1's godoc carries the EMBEDDING CUTS BOTH WAYS paragraph, and
> `NewChannelExchange`'s godoc gains, as part of the accepted-no-probe arm —
>
> > A reply channel that WRAPS another channel and **does not itself declare `SingleSubscriber`** does not
> > inherit the probe, so it is accepted under this arm even when the channel it wraps would be rejected —
> > **however it holds the wrapped channel**. A decorator over a fan-out channel must declare
> > `SingleSubscriber` itself (forwarding, or embedding the concrete type and shadowing) if it wants the check
> > applied.

> **STATE THE INVARIANT BY SHAPE, NOT BY MECHANISM — decision D-M5 (round 7, design M5, compile-proven).**
> The paragraph above previously named only *"embedding the `msgin.SubscribableChannel` interface"*. That is
> **one instance of the class, not the class.** A generic wrapper that holds the **concrete** type in a named
> field with hand-written forwarders —
>
> ```go
> type logged struct{ inner *channel.PublishSubscribeChannel }
> func (l logged) Send(ctx context.Context, m msgin.Message[any]) error { return l.inner.Send(ctx, m) }
> func (l logged) Subscribe(h msgin.MessageHandler) (msgin.Subscription, error) { return l.inner.Subscribe(h) }
> ```
>
> — strips the probe **identically**, with no interface embedding anywhere. Compile-proven in a throwaway
> module at `c4582ba` (`go run .`, the four shapes type-asserted to the capability interface):
>
> ```
> A interface-embedding        probe visible: false
> B named field + forwarders   probe visible: false
> C concrete embedding         probe visible: true
> bare                         probe visible: true
> ```
>
> The governing property is *"does the wrapper's own method set contain `SingleSubscriber`?"*, and the answer
> is no for every wrapper that does not declare it. Interface embedding is merely the shortest way to get
> there; **B has no embedding at all and lands in the same arm.**
>
> **The restated invariant, which is what §8 obligation 12 gates:** *any* wrapper that does not itself declare
> `SingleSubscriber` is accepted under this arm, **however it holds the channel it wraps**. The one exception
> is embedding the **concrete** channel type, which promotes the method — and that is a hazard in its own
> right (§5's METHOD PROMOTION IS ALSO THE HAZARD block), not a remedy unless the wrapper shadows it.

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

> **METHOD PROMOTION IS ALSO THE HAZARD — decision D-L (round 6, design B2, compile-proven).** The paragraph
> above presents embed-and-shadow as the *remedy* and stops there. The **unshadowed** case is the defect, and
> it is the more likely one to be written by accident (case **B** of §1's transcript): a type that embeds
> `*channel.PublishSubscribeChannel` and overrides only `Subscribe` with its own multi-subscriber dispatch
> **inherits `SingleSubscriber` by promotion** and reports on the *embedded* channel. Measured, it reported
> `true` while its own `Subscribe` had fanned out to two handlers, and `NewChannelExchange` accepted it.
>
> Both directions are therefore normative on `ExclusiveSubscribable`'s godoc — see §1's EMBEDDING CUTS BOTH
> WAYS paragraph:
>
> - **Embed the concrete type and shadow** → the intended remedy, when the wrapper really is exclusive.
> - **Embed the concrete type and forget to shadow** → an inherited answer about the wrong object. *A wrapper
>   that changes subscription behavior MUST declare its own `SingleSubscriber`.*
> - **Anything else — embed the `msgin.SubscribableChannel` interface, or hold the channel in a named field
>   with hand-written forwarders** → no promotion at all, so the probe is silently absent and the
>   accept-unknown arm (§4) takes the channel. *(Widened by **D-M5**, round 7: the earlier third bullet named
>   only interface embedding, which is one instance of the class. §4 carries the compile proof that the
>   named-field form behaves identically.)*

## Alternatives rejected

| Alternative | Why not |
|---|---|
| **A cross-exchange registry** | Unchanged from ADR 0028 §6.2: the core cannot see other exchanges, and a registry to make it see them is exactly the in-process global state CLAUDE.md's multi-instance rule warns against — it would appear to guarantee exclusivity while guaranteeing nothing across N instances |
| **Require `ExclusiveSubscribable` on `reply`** (change the parameter type) | Closes the SPI. Every third-party reply channel stops compiling, for a guarantee the core cannot verify anyway. Contradicts "open for extension" |
| **Type-assert the concrete `*channel.PublishSubscribeChannel`** | Introduces an `endpoint` → `channel` import edge that Spec 014 §3's dependency rule forbids, and still sees nothing outside the tree |
| **Probe by trial: `Subscribe` a no-op handler and see if it succeeds** | Side-effecting and racy — it registers a real subscriber on a live channel, and a `DirectChannel` would report "exclusive" only by having its slot taken. Rejected on debuggability grounds too: the failure would surface as a phantom subscriber |
| **Flip the default: `WithSingleSubscriber` on, `WithFanOut()` to opt back in** | Fan-out to every subscriber **is** the Publish-Subscribe Channel pattern (EIP ch.4). A channel that rejects a second subscriber by default is not that pattern, and the change would silently alter every existing pub-sub flow, not just reply channels |
| **Leave it documented (status quo)** | The failure is silent and writes payloads to another exchange's sink. Three lenses independently flagged it. Godoc is what already failed here — ADR 0028 §6.2's `reply` godoc says "REPLY MUST BE DEDICATED TO THIS EXCHANGE" today, in the same file that permits the violation |
| **Harden the accept-unknown arm: a soft opt-in `endpoint.WithRequireExclusiveReply()`, or a warn-level log whenever the probe is absent** (decision **D-M6**, round 7) | **Rejected — both halves, and the reasons differ.** The *warn log* is rejected on noise: the arm fires for **every** channel that predates this interface, including every correct third-party adapter, so it would warn on well-configured production flows on every `NewChannelExchange` call — a log line a caller cannot act on and will filter, which is worse than silence. The *strict option* is rejected as **redundant with what already ships**: a caller who wants "reject anything I cannot verify" gets it by passing a `*channel.DirectChannel` (always `true`) or a `PublishSubscribeChannel` built `WithSingleSubscriber()`, and a caller who is handed an unknown channel by an adapter cannot fix it with an option on the exchange — the fix is on the channel. Adding a third mutually-exclusive flag beside `WithSharedReplyChannel` also makes the guard a 3-state policy, and CLAUDE.md's sensible-defaults rule asks for the safe default plus **one** override, not a lattice. **D-L is what changed the calculus and it changed it the other way**: the arm's cost is now known to be reachable from in-tree types (§4), so the honest response is the **normative documentation** §1/§8-obligation-12 now carry, not a knob that shifts the burden back to a caller who has no more information than the core does. **Revisit if and when a real adapter-supplied `SubscribableChannel` exists** — today there are none, so the option would ship with no caller |

## Consequences

- **+3 exported symbols**, 2 in root (`ExclusiveSubscribable`, `ErrSharedReplyChannel`) and 1 in `endpoint`
  (`WithSharedReplyChannel`), plus 2 methods. Root's projected count is 102 and its sentinel count 42 — see
  Spec 014 §4, and note **those are projections until Task 12 measures them**.
- **A behavior change, and it has a known caller in the tree.**
  `TestChannelExchange_sharedPubSubReplyChannel` (`endpoint/exchange_test.go:413`) builds **both** its exchanges
  in the **shared `t.Run` body** (`exA` at `:446`, `exB` at `:453`) over the channel constructed at `:444` from
  the per-case `opts []channel.PubSubOption` field (`:416`). Under this ADR the default-fan-out case's `exB`
  no longer constructs, so **`exB` must be passed `endpoint.WithSharedReplyChannel()`** — it is precisely the
  test that pins the fan-out trade-off ADR 0028 §6.3 requires, so it must keep asserting the fan-out, now via
  the explicit opt-out. Its second case (`WithSingleSubscriber` → `ErrChannelSubscribed`) is unaffected: the
  opt-out **suppresses the probe outright** and `Subscribe` rejects the second exchange exactly as before.

  > *Corrected (round 7, X-M8 — the reason, not the conclusion.)* This read *"that channel reports exclusive,
  > **passes the probe**, and is rejected later by `Subscribe`"*. Under **D-M2** the guard is
  > `if !cfg.allowShared { … }`, so a construction carrying `WithSharedReplyChannel()` **never calls
  > `SingleSubscriber` at all** — there is no probe to pass. The conclusion (the option is inert for that case,
  > and `ErrChannelSubscribed` still lands) survives unchanged; the stated mechanism was falsified by the very
  > ordering decision recorded three sections above it. Spec §5.1 item 3 and Plan Task 9.6 carried the same
  > sentence and are corrected identically.

  > *Corrected (round 6, C-M7 / E-M6 — propagating the round-4 correction Spec §5.1 and Plan Task 9.6 already
  > carry, into the ADR that Task 9.6 orders read **first**.)* Two claims here were **inexpressible as
  > written**. (1) *"The test must pass `endpoint.WithSharedReplyChannel()` **for its default-fan-out case**"*
  > cannot be done through the per-case field: that field is `opts []channel.PubSubOption`, consumed by
  > `channel.NewPublishSubscribeChannel(tc.opts...)` at `:444`, while `exA`/`exB` are built unconditionally in
  > the shared body — an `endpoint.ExchangeOption` has nowhere per-case to go. The edit is to `exB`'s call
  > site, made safe by the fact that the `WithSingleSubscriber` case returns early at `:455` on `secondErr`.
  > (2) *"builds both its exchanges … and asserts `require.NoError`"* — only `exA` does (`:447`); `exB`'s error
  > is **captured** into `secondErr` (`:453`) and handed to the case's `assert` closure.
- **Two typed errors now describe adjacent conditions, and the distinction is deliberate.**
  `ErrSharedReplyChannel` means *this channel's policy permits sharing* (raised at construction, before
  subscribing, by the probe). `ErrChannelSubscribed` means *this channel is exclusive and the slot is taken*
  (raised by `Subscribe`). A caller wiring two exchanges to one `WithSingleSubscriber` channel gets the
  second; a caller wiring two to a plain one gets the first, on both. Reusing `ErrChannelSubscribed` for both
  was considered and rejected: it would report "already subscribed" for a channel that has no subscriber.
- **Hot-path branches this adds** (each needs a case, per CLAUDE.md's coverage gate), in the order §3's guard
  evaluates them under **D-M2**: `WithSharedReplyChannel()` set → accepted, **probe not consulted**; otherwise
  probe absent → accepted; probe present and `true` → accepted; probe present and `false` →
  `ErrSharedReplyChannel`; **probe present and PANICKING → recovered as `false` → `ErrSharedReplyChannel`
  WRAPPING the recovered value** (decision **D-O**, amended by **D-O2**, §3a). The five are a truth table, not
  a list. Plan 027 Task 9.6 carries them as rows 1–4 plus a **sixth** row; its fifth row is the ordering
  assertion (no subscription left behind after a rejection), which is not a truth-table arm. **The sixth row
  must assert the panic text is in `err.Error()`, not merely that `errors.Is` matches the sentinel** — the
  sentinel-only assertion passes against the diagnosis-losing implementation D-O2 replaces, which is how that
  defect would have shipped green.
- The probe is a **report, not a proof**, and D-L narrows what it reports on rather than widening it. The core's
  claim is bounded to: *it will not silently accept a channel that has told it sharing is permitted*. A channel
  can still be shared in ways the probe never sees — an interface-embedding decorator strips it (§4), a caller
  may subscribe something else to an exclusive channel first, and no local answer binds another process
  (§Topology).

  > *Deleted (round 6, D-L, compile-proven.)* This bullet used to close on *"— that path already yields
  > `ErrChannelSubscribed`"*, offered as the mitigation for a channel that answers `true` and is shared anyway.
  > **It is false for the two cases that matter.** In §1's transcript, case **B** (embedded concrete type,
  > unshadowed) and case **C** (state-reading probe) each accept **two** `Subscribe` calls with **no error at
  > all** — nothing yields `ErrChannelSubscribed`. Under D-L, case C is now a contract violation
  > (*MUST NOT compute it from a live subscriber count*) and case B is named as a hazard on the godoc; neither
  > is repaired by a sentinel the channel never returns. A withdrawn argument must be swept where it is used as
  > a live **mitigation**, not only where it is stated as a fact
  > ([round 6 §0](../plans/027-audit-round-6.md), counter-rule 2).

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

**Topology 2 — a reply channel backed by shared external state, BROADCAST to N instances.** A future or
third-party adapter wraps a broker subscription as a `SubscribableChannel` — a NATS **subject subscribed by
every instance**, a Redis pub/sub channel, an SSE stream fanned out to N. The broker fans each reply out to
all N instances, and the N−1 non-owners each find no waiter for the correlation id and hand **a full copy of
another instance's reply** to their `WithUnmatchedReplySink`. That is bit-for-bit the failure in this ADR's own
Context, now cross-process.

**Topology 2 is NOT "any broker-backed channel".** A **per-instance private destination** — a NATS
`_INBOX.<nuid>` reply subject, an exclusive auto-delete AMQP reply queue — is reached by exactly one recipient,
this process's subscription, and is therefore **Topology 1 with a broker in the middle**. It is also the
canonical **Return Address** implementation. Under D-L (revised) it honestly answers `true`, and it must, or
the pattern this ADR points at as *the* distributed answer would be unrepresentable.

> **THIS SECTION'S CONCLUSION IS NARROWED — D-L (round 6), REVISED (round 7, design B2).** Under §1's original
> **handle-local** definition, a broadcast adapter's *honest* answer was `true` — this local handle does admit
> one local subscriber — so every instance's probe passed, the failure was **endorsed by a passing probe**, and
> this section concluded: *"The design has no way to detect it … a truthful local answer is indistinguishable
> from a safe one."*
>
> **Under the recipients-reached definition that conclusion no longer holds FOR THE BROADCAST CASE**, which was
> always the real claim. `SingleSubscriber()` now reports whether *every message sent to this channel reaches
> at most one recipient, counted across every process*, and the godoc says outright that a channel MUST return
> `false` whenever a message sent to it can be received by any recipient other than its single subscriber,
> **including one in another process**. A **broadcast** broker-backed adapter's honest answer is therefore
> `false`, `NewChannelExchange` returns `ErrSharedReplyChannel`, and **the broadcast half of Topology 2 becomes
> detectable by the very design this section said could not detect it** — at no added cost, with the same two
> in-tree implementations answering the same way (`DirectChannel` → `true`; `PublishSubscribeChannel` +
> `WithSingleSubscriber` → `true`).
>
> **What the round-6 wording overreached on, and round 7 withdraws.** It said *"a channel whose deliveries
> **reach other processes** … MUST return false"* — measuring **processes traversed**. That answers `false` for
> a private `_INBOX`, which traverses a broker and another process's memory but is **received by exactly one
> recipient**, making the canonical Return Address channel unrepresentable and contradicting the same
> paragraph's first sentence (*"THIS exchange will be the sole recipient"* → `true`). The predicate now counts
> **recipients reached**, so the two sentences agree: broadcast → `false`, private inbox → `true`. §1 carries
> the labelled withdrawal.
>
> **What is detected is honesty, not topology.** The core still cannot observe another instance; it relies on
> the adapter answering its own contract truthfully, exactly as `NativeReliability` and `ScheduledSender` do.
> A **broadcast** broker adapter that returns `true` is now **violating a stated MUST** rather than giving a
> defensible local answer — which is the whole difference, because a contract violation is reviewable and a
> truthful-but-useless answer is not. The core still cannot tell a private inbox from a broadcast subject; it
> can only hold the adapter to a predicate that has one right answer for each.

**Consequences for the contract, all normative:**

1. `SingleSubscriber()`'s godoc states the **end-to-end policy** predicate of §1 — every message sent to this
   channel reaches **at most one recipient, counted across every process**, so `false` whenever a message can
   be received by any recipient other than the single subscriber registered on it, **including one in another
   process** — and that the value is **constant for the channel's lifetime**. It is a *report*, never a
   distributed guarantee that the core verified; the core's guarantee is bounded to *"it will not silently
   accept a channel that has told it sharing is permitted"*. `ErrSharedReplyChannel`'s godoc is worded in the
   same terms. *(Reworded by **D-L (revised)**, round 7: this bullet previously read* "`false` whenever
   deliveries reach another process"*, which is the processes-traversed measure §1 withdraws — it answers*
   `false` *for a per-instance NATS `_INBOX`, the canonical Return Address channel.)*
2. **`NewChannelExchange`'s godoc must state FOUR outcomes, not three**: rejected (probe reports
   non-exclusive) · accepted (probe reports exclusive) · accepted (**no probe implemented — including any
   wrapper that does not itself declare `SingleSubscriber`, however it holds the channel it wraps**, §4 /
   D-M5) · **accepted, but exclusive only within this process** — the fourth is the one a caller will
   otherwise assume away, because "the core rejects a shared reply channel" reads as a guarantee. This is a
   Spec §8 godoc obligation, owned by Task 11.
3. `SingleSubscriber()` must be documented as **both lifetime-invariant and safe for concurrent use** — two
   requirements, neither replacing the other (§1). Concurrency-safety alone was the earlier requirement and is
   insufficient: a third-party implementer computing exclusivity from a mutable subscriber count introduces a
   data race msgin's own `-race` suite can never observe (msgin never calls it concurrently), *and* — even
   made race-free with an atomic — a TOCTOU lie the core cannot detect, because msgin calls the method once,
   at construction, and treats the answer as an invariant. This is case **C** of §1's transcript, where a
   state-reading probe answered `true` at construction and then admitted two subscribers with no error.

**The Return Address seam is unaffected — verified, not assumed.** `msgin.RequestReplyExchange` (`spi.go:118`)
is a one-method interface an external adapter implements **directly**, bypassing `NewChannelExchange`
entirely; `adapter/http/exchange.go:58` already does exactly this
(`var _ msgin.RequestReplyExchange = (*Exchange)(nil)`). D-J adds nothing to that path and constrains it in no
way. The leak is in the probe's **semantics**, not in the seam. *(Round-4 design audit, BLOCKER 1.)*

**The distributed case remains Return Address** (EIP ch.5): a reply addressed to the instance that sent the
request, carried in the message rather than resolved through a shared in-memory channel. The SPI seam for it
is the external `RequestReplyExchange` adapter named in Spec 010 §8.1 — unchanged by this ADR, and
deliberately not approximated by it. **A probe that returned `true` for a channel BROADCAST across instances
would be a lie of exactly the kind the registry alternative was rejected for** — which, under **D-L**, is no
longer a defensible reading of the contract but a violation of a stated MUST: `SingleSubscriber()` counts
**recipients reached** precisely so that answer has no honest defence. Detecting the lie is still beyond the
core; naming it is not, and that is the whole of what D-L buys here.

> *Narrowed by **D-L (revised)**, round 7.* This paragraph previously said *"a probe that returned `true` for a
> **distributed** channel would be a lie"*. That is false as stated, and it is the same conflation §1
> withdraws: a **per-instance private reply destination** is distributed *and* honestly `true`, and it is
> precisely the Return Address implementation this paragraph is recommending. The lie is **broadcast**, not
> distribution. A design whose contract cannot express its own recommended pattern is the defect, not the
> pattern.
