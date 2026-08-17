# Plan 028 — adversarial design audit, round 2 (2026-08-14)

Independent Opus subagent, fresh (did not run round 1), handed the complete revision-2 bundle (Spec 015 + ADR 0031 +
Plan 028) plus [`028-audit-round-1.md`](028-audit-round-1.md), **before any implementation code existed**. Evidence-primary.

**Verdict: DO NOT IMPLEMENT — round 3 required.** 3 BLOCKERs, 3 MAJORs, 5 MINORs, **all in the verification layer**.
The design's contested core survived: the mechanism (R1/R2/R3), the census of 32, the family partition and decision
**D-S** were re-attacked and **held**. What failed is the layer that was supposed to prove the design correct — three
acceptance criteria **cannot be executed against the code they govern**, and each would have been discovered *inside*
a task running under per-task-commit pre-authorization, which is exactly what the design-time gate exists to prevent.

Every blocker below was independently re-verified by the coordinator before any edit.

---

## BLOCKER-A — AC-5b does not compile for one channel constructor and inverts the diagnosis for the other

Revision 2 AC-5b: *"For **both** `channel` constructors, a latched instance reports `SingleSubscriber() == false`."*

**(a) `PubSub` has no such method.**
```
$ grep -n '^func (p \*PubSub)' channel/pubsub_registry.go
36:func (p *PubSub) Publish(ctx context.Context, topic string, msg msgin.Message[any]) error {
49:func (p *PubSub) Subscribe(topic string, h msgin.MessageHandler) (msgin.Subscription, error) {
80:func (p *PubSub) TopicCount() int {
```
The AC contradicted Spec §3.2 four paragraphs above it, which already named `PubSub`'s non-error method as
`TopicCount`.

**(b) It fails against the plan's own R2 code shape.** The mandated shape applies options before the nil and
`break`s. `channel/pubsub.go:142` is `func (c *PublishSubscribeChannel) SingleSubscriber() bool { return c.cfg.single }`,
so `NewPublishSubscribeChannel(WithSingleSubscriber(), nil)` latches **and** reports `true`. Written instead as
`(nil)` alone the assertion is **vacuous** — `defaultPubSubConfig()` already yields `single == false`.

**(c) Forcing `false` launders the fault into a misleading diagnosis.** `endpoint/exchange.go:318-326`:
```go
if !cfg.allowShared {
    if ex, ok := reply.(msgin.ExclusiveSubscribable); ok {
        single, cause := safeSingleSubscriber(ex, cfg.logger)
        if !single { … return nil, msgin.ErrSharedReplyChannel }
    }
}
…
sub, err := reply.Subscribe(e.receiver())   // exchange.go:336-338
```
A caller passing `WithSingleSubscriber()` *and* a nil option would get `ErrSharedReplyChannel` — `errors.Is(err,
msgin.ErrNilFunc)` **false**, the nil option never mentioned. Truthful reporting lets the probe pass and
`reply.Subscribe` surface the latched fault verbatim.

The AC's rationale was wrong twice over: a latched channel's `Subscribe` always fails, so it can **never** accumulate
two subscribers — the exclusivity hazard is structurally impossible; and R2 exists for these two because of
`Send`/`Subscribe`/`Publish`, not `SingleSubscriber`.

**Folded as:** AC-5b restated per type with the actual reporting methods; non-error methods explicitly **not forced**;
new **AC-5c** asserts `NewChannelExchange` over a latched reply channel yields `ErrNilFunc`, not
`ErrSharedReplyChannel`; Spec §3.2 rewritten; Plan Task 3 carries all three.

---

## BLOCKER-B — AC-2 cannot be written for `endpoint.NewGateway`, blocking the reference task

AC-2 required, for each R1/R2 constructor, a case placing the nil at index 1 **after a valid option**.
`GatewayOption[Req, Rep]` is `func(*gatewayConfig)` over an **unexported, empty** struct with **zero exported
constructors**, so `package endpoint_test` cannot obtain a non-nil value. Blackbox-only (Global constraint 1) is not
negotiable, so the AC is unexecutable — in **Task 1**, the reference task.

```
$ grep -rn 'GatewayOption' --include='*.go' . | grep -v _test.go
endpoint/gateway.go:20:// GatewayOption configures a Gateway built by NewGateway. Reserved for future
endpoint/gateway.go:22:type GatewayOption[Req, Rep any] func(*gatewayConfig)
endpoint/gateway.go:25:func NewGateway[Req, Rep any](x msgin.RequestReplyExchange, opts ...GatewayOption[Req, Rep]) …
$ sed -n '18p' endpoint/gateway.go
type gatewayConfig struct{}
```
Compile-proof from `package endpoint_test`:
```
vet: endpoint/zz_probe_test.go:10:60: name gatewayConfig not exported by package endpoint
```
Every other option type has ≥1 exported constructor; `msghttp.Option` is additionally satisfiable directly because
`Config` is exported. `GatewayOption` is the sole zero.

**This is round-1 M-4 recurring one level deeper:** M-4 fixed *"a package exports **one** option"*; nobody checked
for *"a package exports **none**"*.

**Also surfaced:** `NewGateway` applies its options into a `cfg` it then **discards** — `gateway.go:31` returns
`&Gateway[Req, Rep]{exchange: x}`. The parameter is inert today.

**Folded as:** AC-2 scoped to the 24 R1 + 5 R2 with an obtainable non-nil option; `NewGateway` exempted **with the
reason and the verbatim compile error**, so a later reader does not "fix" it by going whitebox; Plan Task 1 carries
the exemption and an explicit "do not reach for a whitebox test". The inert-parameter question is recorded in Spec
AC-2 as an **open decision** (keep-and-exempt is the default; removing `opts` would be a second signature change,
forbidden by Global constraint 3 without a stated decision).

---

## BLOCKER-C — AC-7's hazard list is blind to the generic option types carrying three flagship constructors

AC-7/Task 7 called their four hazards exhaustive. An AST walk over every `.go` file in the repo, tallying the node
kind of each option-named variadic's element type:

```
--- kind tally ---
  35  *ast.Ident            (bare:      Option, PubSubOption, …)
  11  *ast.SelectorExpr     (qualified: msghttp.Option — hazard 2)
   2  *ast.IndexExpr        ← ConsumerOption[T]        endpoint/consumer.go:240
                              ProducerOption[T]        endpoint/producer.go:336
   1  *ast.IndexListExpr    ← GatewayOption[Req,Rep]   endpoint/gateway.go:25
```

Neither `*ast.IndexExpr` nor `*ast.IndexListExpr` appeared anywhere in the bundle. A checker built to the stated list
**silently skips `NewConsumer`, `NewProducer` and `NewGateway`** — and Task 7's mandatory vacuity probe would very
likely plant a *non-generic* constructor, go RED, and certify a gate blind to generics. **BLOCKER-1's exact failure
mode, one abstraction level down.**

Two further corrections to the same table:

| Listed hazard | Reality |
|---|---|
| `//go:build ignore` files in `docs/plans/027-tools` | **0 instances** — they contain no variadic options. A future risk, not "instances in the tree today" |
| *(unlisted)* `_test.go` files | **13 instances**, 9 of them `FuncDecl`s — so "skip `FuncLit`" does not cover them: `endpoint/exchange_test.go:39,54,1158`, `adapter/http/exchange_test.go:69,590`, 8 × `RunTestX(…TestOption)` in `crontest`/`dbtest`. This is the real false-positive source |

**Third gap:** Task 7 required the checker to accept "folded and pre-check shapes" — **2 of the 4 shapes the plan
itself mandates**. R2's `break`-latch and R3's `continue` were unnamed, so a checker written to the plan would flag
`msgin.New`, both `channel` constructors, `memory.New` and `routing.NewRouter` — five false positives on
constructors this increment just fixed.

**Folded as:** a six-row hazard table with the node-kind tally; all four guard shapes enumerated; `027-tools`
demoted to future risk; the vacuity probe now plants **two** constructors — one bare-typed, one **generic** — and
requires both RED.

---

## MAJOR findings

| # | Finding | Evidence | Folded as |
|---|---|---|---|
| **M-D** | **§3.2 reintroduced verbatim the false claim round-1 M-5 killed in §3.1** — *"Non-error methods … behave as if no option were applied."* Under the mandated `break` shape, options **before** the nil are applied. §3.2 was also silent on whether options **after** the nil apply, which is what made AC-5b indeterminate | The plan's own R2 shape; `channel/pubsub.go:142` | §3.2 rewritten to the property that holds: options before the nil apply, the scan stops, options after do not, non-error methods report the surviving prefix |
| **M-E** | **AC-6's mutant was dominated by the panic.** Reverting any guard makes the constructor panic on `(nil)`, so **every** case goes RED regardless of what it asserts — a case hardcoding `index 0`, or asserting the wrong `IsPermanent`, was killed by the panic and certified as evidence. The project's own "mutation-test every new assertion" scar | The specified mutant is guard-reversion for all assertions | Per-assertion mutant table (Spec AC-6): hardcode the index; report the last nil; flip the wrap **both** ways; remove one method's latch; delete a delegator pre-check and require failure on the **position string**. Guard-reversion is now permitted for **AC-1 alone** |
| **M-F** | **Precedence is per-constructor but the test budget was per-package**, and `adapter/database/sql` alone contains three different orders (loop-first ×3, validate-first ×1, delegator pre-check ×1); `adapter/cron` splits the same way. §3.5's rule was also **undefined for the 8 delegators**, which have no apply loop | `outbound.go:54`/`source.go:87`/`groupstore.go:205` loop-first; `inbox_dedup.go:74` validate-first; `sqlelector.go:88-93`, `sqllock.go:51-56` validate-first vs `cron/source.go:179` loop-first | §3.5 now carries a **derived** 17-row order table (8 validate-first / 9 loop-first) plus an explicit delegator clause — verified that all 8 delegators call their delegate as the first statement, so a top-of-function pre-check preserves existing precedence. Plan asserts **one case per distinct order**, not per package |

## MINOR findings

| # | Finding | Folded as |
|---|---|---|
| **m-G** | **"11 call sites" for `NewCircuitBreaker` is 10** — the count included the declaration. Same off-by-one throughout §5's "~107": `NewRouter` 24→23, `NewPublishSubscribeChannel` 21→20, `NewPubSub` 10→9 (`memory.New` 41 correct) | Corrected to **10** (listed by file) and **103** in §5; ADR D-T likewise |
| **m-H** | **D-T contradicted D-P's own membership rule.** §2.1 calls membership "structural, not preference"; by D-P's letter `NewCircuitBreaker` (no product error surface) belongs in R3. This also weakened AC-7's forward claim, since the spec gave no rule for choosing | Explicit tiebreak stated in Spec §3.6 and ADR D-T: *no product error surface → take an error return when wiring-time-only **and** the sibling already returns one; otherwise R3* |
| **m-I** | **All 10 `NewCircuitBreaker` sites are in `_test.go`, two are inline-composed**, and one is a **published godoc Example** (`example_flowcontrol_test.go:24`) that gains error-handling noise — the very inline-composability cost the bundle used to *reject* error returns for R2's five. Task 2's scope also omitted the three `endpoint` test files it must touch | Cost acknowledged in Spec §3.6 and ADR D-T; the four files named in Plan Task 2 |
| **m-J** | **Latch-placement footgun.** `withConfig` does `*c = cfg` (`pubsub.go:104`) and `PubSub.Subscribe` builds each topic channel via `NewPublishSubscribeChannel(withConfig(p.cfg))` (`pubsub_registry.go:60`) — latching into `pubSubConfig` would propagate the registry's fault into **every** topic channel | One line in Plan Task 3: latch onto the **struct**, never into the config. Also states that `channel`'s two inline copies of the expression are intended (no R1 constructor ⇒ no helper) |
| **m-K** | **`msgin.New`'s R3 godoc named 2 of 3 `MessageOption`s** — `WithClock` (`message.go:125`) omitted, and a dropped `WithClock` silently reverts to the real clock, which is what breaks a fake-clock test | All three named in Spec §3.3 |

## Verified sound — attacked, held (do not re-litigate)

**D-S is correct, and the bundle under-argued it.** Attacked hardest per instruction; survived, and the audit
supplied two arguments stronger than the ADR's own:

- **The hot-retry hazard is real:** bare → `consumer.go:820`'s `IsPermanent` false → transient path → default
  `RetryPolicy{}` has `MaxAttempts == 0`, which `retry.go` documents as *"retry forever (no dead-letter)"*.
- **A second, unnamed hazard:** `producer.go:447-453` — `if msgin.IsPermanent(err) { return err }` is the only early
  exit, so bare, a latched fault is retried to exhaustion and the message is **dead-lettered**.
- **"Cosmetic cost" was wrong, in D-S's favour:** `consumer.go:716` records
  `safeRecord(id, err == nil || IsPermanent(err))` — bare records the circuit breaker **unhealthy**, so a wiring bug
  trips the breaker and looks like an outage.
- The "wrap only `Router.Handle`" alternative leaves all three hazards open on `Send`.

**Also verified sound:**

1. **Census = 32**, re-derived independently by AST walk over all 8 modules — not the bundle's grep. `NewAggregator`
   is the only multi-line signature; no method-with-options, no alias-typed option, no other spelling.
2. **Round 1's open `expr` suspicion is now PROVEN CLOSED** — zero option-typed variadics anywhere in `expr/`; the
   gate needs no special case.
3. **24 apply loops / 22 option func types** re-run exact; R1/R2/R3 = 25/5/2 verified against actual return types;
   `24 = 17 + 5 + 2` and `25 = 17 + 8` hold.
4. `harness`'s 4 variadics are `FuncLit`s, confirmed by node kind rather than grep.
5. **Every code citation exact:** `flowcontrol.go:42-46`, `errors.go:212-221`, `consumer.go:235-239`,
   `cron/source.go:184-186`, `handler.go:44-51`, `message.go:165`, root-only baseline.
6. **R2 is implementable** for `Router` (single method) and `memory.Broker`. `Stream` is the right surface:
   `consumer.go:408` assigns `runErr = c.streamSrc.Stream(ctx, rawCh)` and returns it from `Run` — no retry loop, no
   spin; a latched broker fails `Run` immediately. `EmitsLiveValue` violates no SPI contract.
7. **The latch is race-free and allocation-free** — written once in the constructor before the object escapes, no R2
   constructor starts a goroutine, and the error value is built once and stored (errors here are immutable, so the
   many-`Send` case has no aliasing hazard).
8. **Round-1 fixes spot-checked and correct:** the corrected census command, the `handler.go:44-51` quotation, M-3's
   membership swap, M-4's AC-4 pair (both R3 option types have 3 constructors — executable), m-10's baseline caveat.
9. **Docs-link gate, both arms, clean** on all four bundle files.

## Round-3 scope (narrow, per the auditor)

§3.2's applied-prefix property; the restated AC-2 / AC-5b / AC-5c / AC-6 / AC-7; the per-constructor precedence
table; the corrected `NewCircuitBreaker` count. **The census, the family partition and D-S need not be re-attacked.**

## Coordinator's independent re-verification

All three blockers and m-G re-run before any edit: `grep -n '^func (p \*PubSub)' channel/pubsub_registry.go` (no
`SingleSubscriber`); the `GatewayOption` grep plus `sed -n '18,22p' endpoint/gateway.go` (unexported empty config,
zero constructors); `grep -n 'opts \.\.\.[A-Za-z]*Option\[' endpoint/*.go` (three generic option variadics);
`retry.go` (`MaxAttempts == 0` ⇒ retry forever); `producer.go:445-455` (`IsPermanent` the only early exit);
`endpoint/exchange.go:316-340` (the probe precedes `Subscribe`); `message.go:125,136,143` (three `MessageOption`
constructors); and the de-duplicated call-site recount — **41 / 23 / 20 / 9 / 10 = 103**. The 17-row precedence table
was derived mechanically, not assigned. **All confirmed.**
