# Spec 015 — Nil option elements must not panic

- **Status:** **DRAFT (2026-08-17), revision 5 — AUDIT-CLEARED** — written before any code, per CLAUDE.md's design-time gate.
  - **Revision 2** folded round-1 findings ([`028-audit-round-1.md`](../plans/028-audit-round-1.md)): 2 BLOCKERs,
    6 MAJORs. The census moved **30 → 32**, and the silent-skip mechanism was **replaced for five of seven
    constructors** after the audit proved it contradicts `handler.go:44-51`.
  - **Revision 3** folded round-2 findings ([`028-audit-round-2.md`](../plans/028-audit-round-2.md)): 3 BLOCKERs,
    3 MAJORs, 5 MINORs — **all in the verification layer**. The mechanism (R1/R2/R3), the census of 32 and decision
    D-S were re-attacked and **held**. AC-2, AC-5b, AC-6 and AC-7 were each proven to certify less than they claimed;
    §3.2's applied-prefix property and §3.5's precedence table are new. 
  - **Revision 4** folds round-3 findings ([`028-audit-round-3.md`](../plans/028-audit-round-3.md)): 1 BLOCKER,
    4 MAJORs, 7 MINORs. Two needed a user decision and are now **D-U** (the R2 loop `continue`s) and **D-V** (the
    latch is reported before a method's own argument checks; `routing.Filter` realigned).
  - **Revision 5** folds round-4 findings ([`028-audit-round-4.md`](../plans/028-audit-round-4.md)): **SAFE TO
    IMPLEMENT**, no blocker, no round 5. **D-U and D-V were compile-proven** in a throwaway clone and the whole
    8-module suite stayed green under them. 3 MAJORs (the position's constructor name was asserted for only 9 of 32;
    R2 first-nil-wins had no AC; Task 7's hazard table lagged the spec's) + 9 MINORs, all verification-layer.
- **Realized by:** [Plan 028](../plans/028-nil-option-elements.md).
- **Decided by:** [ADR 0031](../adrs/0031-nil-option-elements.md).
- **Builds on:** [ADR 0029 §5.0b](../adrs/0029-eip-lexical-alignment.md) (decision **D-M** — the `Permanent` wrap and
  its constructor-arm exclusion), the `msgin.ErrNilFunc` invariant (`errors.go:212-221`), and the `Chain` nil-element
  precedent (`handler.go:44-63`). [Spec 014 §3.3](014-core-package-layout.md) is cited as **precedent** for
  duplicating small helpers per package, not as a stated general rule (§5).
- **Origin:** the triaged backlog item in [`docs/HANDOVER.md`](../HANDOVER.md) §6.

## 1. The gap

**Every functional-option constructor in the workspace panics on a nil option element.** The application loop is
unguarded in all 24 places it appears:

```go
for _, opt := range opts {
	opt(&cfg)          // ← nil opt ⇒ runtime panic: invalid memory address
}
```

so `endpoint.NewConsumer(src, h, nil)` panics on **caller input**, which CLAUDE.md forbids without qualification:
*"Library code must not call `os.Exit`, `log.Fatal`, or `panic` on caller input (return errors instead)."*

The nil arrives by the ordinary route — a helper that builds an option conditionally and returns the zero value on the
negative branch:

```go
func retryOpt(cfg AppConfig) endpoint.ConsumerOption[Order] {
	if cfg.Retries == 0 {
		return nil                       // ← the zero value of a func type
	}
	return endpoint.WithRetryPolicy(...)
}

c, err := endpoint.NewConsumer(src, h, retryOpt(appCfg))   // panics when Retries == 0
```

The caller has an `err` in hand and never gets to read it.

### 1.1 The precedent — quoted in full, because half of it was the round-1 blocker

`msgin.Chain` already handles this fault for the sibling variadic — a nil `Step` **element**. Its godoc
(`handler.go:44-51`) states both halves of the decision:

> A nil ELEMENT is not a panic (no panic on caller input) and **is NOT skipped**: it is replaced in place by a step
> whose handler fails with `Permanent(ErrNilFunc)` naming its index, so the flow degrades exactly as `To(nil)` or
> `Activate(nil)` does. **Skipping was rejected** — a step built conditionally
> (`steps = append(steps, maybeStep())`) that came back nil is a **WIRING BUG**, and deleting it would silently change
> what the flow does: a dropped `Filter` stops filtering, a dropped `To` discards the message. Steps BEFORE the nil
> still run; the chain stops at it.

The conditional-construction example in that comment is the same shape as §1's. **Revision 1 of this spec quoted only
lines 52-63 — the position format — and adopted, for seven constructors, the very mechanism lines 47-51 reject.** The
round-1 audit proved the omission; §3 below is rewritten around it.

`ErrNilFunc`'s godoc already generalises the sentinel (`errors.go:207-210`): it is returned *"by an endpoint …
constructed with a nil function — and by [Chain] for a nil Step **ELEMENT**, which is a nil function too — **instead of
panicking at dispatch**."* An option is a nil function too.

## 2. The inventory (measured, not transcribed)

**32 exported constructors take a variadic option parameter.**

> **Regenerate before citing — and use THIS command.** Revision 1's census read 30 because its pattern could not match
> a **qualified** option type (`...msghttp.Option`: the `.` breaks `[A-Za-z]*`), silently missing the two
> `adapter/http/stdlib` constructors. The `^func [A-Z]` anchor is also load-bearing — it excludes the four variadic
> **func literals** in `adapter/database/sql/harness`, which are test-kit locals, not constructors.

```bash
{ grep -rn --include='*.go' '^func [A-Z].*\.\.\.\([a-z][A-Za-z0-9]*\.\)\?[A-Za-z]*[Oo]ption' . | grep -v '_test.go'
  grep -n '^func NewAggregator' routing/aggregator.go; } | sed 's/:func.*//' | sort -u | wc -l   # → 32
grep -rn --include='*.go' 'range opts' . | grep -v '_test.go' | wc -l                            # → 24 apply loops
grep -rn --include='*.go' '^type [A-Za-z]*[Oo]ption[A-Za-z0-9[:space:],\[\]]* func' . | grep -v '_test.go' | wc -l  # → 22
```

`32 = 24 + 8`: eight constructors do not apply their own options, they **forward** them into one of the 24 loops.

**All 22 option types are `func` types** — none is an interface — so "nil function" is literally accurate for every
element this spec governs, and `opt == nil` is an exact comparison with no typed-nil hazard (round-1 audit, verified
negative).

### 2.1 Classification by what the constructor can report

The 32 partition into three **reporting families** with no residual. The family is decided by what the constructor
can surface a fault through — first its own return, then its product's.

| Family | Count | Members |
|---|---|---|
| **R1 — Reject at construction** | **25** | `endpoint.NewConsumer`, `.NewProducer`, `.NewGateway`, `.NewChannelExchange`; `routing.NewAggregator`; `resilience.NewTokenBucket`, **`resilience.NewCircuitBreaker`**‡; `memory.NewGroupStore`, `.NewQueueStore`; `cron.NewSource`, `.NewSQLElector`, `.NewSQLLocker`; `sql.NewOutboundAdapter`, `.NewPollingSource`, `.NewGroupStore`, `.NewInboxDeduper`, `.NewQueueStore`†; `msghttp.NewConfig`, `.NewExchange`†, `.NewOutbound`†, `.NewSSEClient`†, `.NewSSEParser`†, `.NewSSEServer`†; `stdlib.NewInbound`†, `.NewInboundGateway`† |
| **R2 — Degrade at first use** | **5** | `routing.Filter` (→ `msgin.Step`), `routing.NewRouter` (→ `Router.Handle`), `channel.NewPublishSubscribeChannel` (→ `Send`/`Subscribe`), `channel.NewPubSub` (→ `Publish`/`Subscribe`), `memory.New` (→ `Broker.Send`/`Stream`) |
| **R3 — Skip, documented** | **2** | `msgin.New` (→ `Message[T]`), `sqlite.DSN` (→ `string`) |

† = one of the **8 delegators**. ‡ = **gains an error return** in this increment (§3.6).

Reconciliation, no residual: `24 apply loops = 17 (R1, non-delegating) + 5 (R2) + 2 (R3)`; `25 R1 = 17 + 8 delegators`.

**R3 has exactly two members because exactly two products have no error-returning surface at all.** `Message[T]`'s
six methods (`Payload`, `Headers`, `Header`, `ID`, `WithHeader`, `WithoutHeader`) return no error; `DSN` returns a
`string`. Membership is structural, not preference — verify with `go doc`, not by reading this table.

## 3. The contract

> **No msgin constructor panics on a nil option element. It reports the fault through its own return if it has one,
> otherwise through the first use of the object it produced; only where neither surface exists does it skip the
> element, and then its godoc names what was lost.**

The invariant is stated over *reportability*, not over a file list, so a constructor added later is governed without
this spec being edited (§6 AC-7). A constructor that gains an error return for unrelated reasons moves R2→R1 with no
revision here.

### 3.1 R1 — construction fails with a bare `ErrNilFunc`

On the **first** nil element (left to right) the constructor returns:

```go
fmt.Errorf("%w: %s: nil option at index %d", msgin.ErrNilFunc, ctor, i)
```

- `errors.Is(err, msgin.ErrNilFunc)` matches; `msgin.IsPermanent(err)` is **false**.
- **Bare — never wrapped in `msgin.Permanent`.** This is the constructor arm of the `ErrNilFunc` invariant
  (`errors.go:216-221`, restated on `nilFuncAt` in both `endpoint/helpers.go` and `routing/helpers.go`): the error is
  handed to the caller and never carried through a `MessageHandler`, so it never reaches a `RetryPolicy` and a retry
  classification would be meaningless. **The exclusion is a decision, not an omission — do not "finish the job" by
  wrapping it.**
- **First-nil-wins.** Two nils ⇒ the lower index is reported. One fault, not a list.
- **Options before the nil ARE applied**, to a config the constructor then discards. Because no object is returned,
  no partial configuration is observable. *(Revision 1 claimed "no option is applied"; the round-1 audit proved that
  false against this spec's own code shape. The property that actually holds is the one stated here.)* This holds
  only while every option's effect is confined to the value it receives — true today for all 22 types, and a
  constraint any new option must respect. `TokenBucketOption`, `CircuitBreakerOption` and `memory.Option` apply to
  the **live object** rather than a config struct, so they are the ones to watch.

### 3.2 R2 — the product carries the fault to its first use

The constructor has no error return, but its product has an error-returning method. It **latches** the fault and the
first such call returns it, exactly as `Chain` replaces a nil step with a failing one:

```go
fmt.Errorf("%w: %s: nil option at index %d", msgin.Permanent(msgin.ErrNilFunc), ctor, i)
```

- **Decision D-U — every non-nil option applies.** The loop latches the **first** nil and **`continue`s**; options
  before *and after* it are all applied. The object is returned, does not panic, and every error-returning method
  reports the latched fault. Latch only when unlatched, which is what preserves first-nil-wins.
- **Non-error methods therefore report exactly what the caller's non-nil options set** — they are not forced to any
  value, and they no longer depend on where the nil sat.

  > **Why `continue`, not `break`** (round-3 audit, M-M, compile-proven). Under `break`,
  > `NewPublishSubscribeChannel(nil, WithSingleSubscriber())` left `single == false`, so the exclusivity probe at
  > `endpoint/exchange.go:318-326` rejected the channel with **`ErrSharedReplyChannel`** — `errors.Is(err,
  > ErrNilFunc)` false, the nil option never mentioned. The same call with the nil *second* reported correctly. A
  > property that holds for one option ordering and not the other is the **third** recurrence of the round-1 M-5 /
  > round-2 M-D class, so it is fixed **structurally** rather than documented around. `Chain`'s *"the chain stops at
  > it"* does **not** transfer: a `Step` is sequential behavior, where a skipped element changes what runs; an option
  > is configuration, where applying the caller's other stated intent is strictly better than discarding it.

> **D-U's footprint is observable on exactly ONE surface**, and that is a coverage fact worth stating rather than
> discovering: `PublishSubscribeChannel.SingleSubscriber()`. For `Router`, `PubSub`, `Broker` and `Filter` the latch
> dominates every method, so a `break` implementation would pass every AC. AC-5c's nil-first case is therefore the
> **only** executable test in the bundle that distinguishes `continue` from `break` (round-4 audit, m-Z5).

- **Decision D-V — the latch is reported BEFORE the method's own argument checks, uniformly.** A latched
  `Subscribe(nil)` reports the nil **option**, not `ErrNilHandler`; `NewRouter(nil, nil).Handle(…)` reports the nil
  **option**, not the nil `pick`. The option fault is chronologically earlier — it happened at construction — and one
  rule across the family beats a per-method accident. **`routing.Filter` is realigned to match**: its option loop
  moves **above** its `pred == nil` check, so `Filter(nil, nil)` reports `nil option at index 0`. *(Without the
  realignment `Filter` would answer the opposite way from `NewRouter` for the same fault pair in the same package —
  round-3 audit, M-N.)*
- Which methods report, per type — `Router.Handle`; `PublishSubscribeChannel.Send`/`Subscribe`;
  `PubSub.Publish`/`Subscribe`; `Broker.Send`/`Stream`. **`PubSub` has no `SingleSubscriber` method at all**
  (`channel/pubsub_registry.go` declares only `Publish`, `Subscribe`, `TopicCount`).
- `routing.Filter` uses the package's existing `nilFuncStep`; the other four latch an `err` field.

**Decision D-S — the latched fault IS `Permanent`-wrapped, on every surface.** The `ErrNilFunc` invariant's third
clause says an error "handed to a caller from a non-constructor API" stays bare, which would leave
`channel.Send`/`Broker.Send` bare. This spec wraps them anyway, because those methods are reachable **from inside a
flow** — `To(sink)` calls `OutboundAdapter.Send` from a handler body — and a bare permanent fault there is retried by
the `RetryPolicy` forever (the unbounded hot-retry loop `endpoint/consumer.go:235-239` documents at ~46k retries in
200 ms). The wrap's only cost is cosmetic: a caller invoking `Send` directly sees `IsPermanent(err) == true` on an
error they were handed synchronously. **This is the most contestable point in the bundle** and is flagged for the
round-2 audit.

### 3.3 R3 — the element is skipped, and the godoc says what that costs

`msgin.New` and `sqlite.DSN` have no surface to report through. Each skips the nil element, applies the rest, and
carries a godoc sentence **naming the concrete consequence** — not merely that it is ignored:

- **`msgin.New`:** *"A nil element in opts is ignored — New has no error to return. A dropped `WithID` therefore
  means New stamps a fresh random id, a dropped `WithHeaders` means those headers are absent, and a dropped
  `WithClock` means the timestamp comes from the real clock; if the id is an idempotency key, build the option
  unconditionally."* **All three** `MessageOption` constructors must be named (`message.go:125,136,143`) — a dropped
  `WithClock` is what silently breaks a fake-clock test.
- **`sqlite.DSN`:** *"A nil element in opts is ignored — DSN returns a string and has no error to return. The
  resulting DSN carries **msgin's** defaults for whatever the dropped option would have set: WAL journal mode, a 5s
  busy_timeout, and a **file-backed** database — so a dropped `WithSharedMemory` silently targets a different
  database."*

  > Revision 3 said "**SQLite's** defaults", which is the opposite of what happens: `dsn.go:9-12` sets
  > `defaultJournalMode = "WAL"` and `defaultBusyTimeout = 5 * time.Second`, where SQLite's own are `delete` and `0`.
  > A caller passing `WithJournalMode("")` to *omit* the pragma silently gets WAL. It also omitted
  > **`WithSharedMemory`** (`dsn.go:38`), the third and most consequential `DSNOption` — dropping it turns
  > `file::memory:?cache=shared` into a file database. R3 exists only because the godoc names the *concrete*
  > consequence; naming a false one is worse than boilerplate (round-3 audit, M-O).

Options before and after the nil apply normally. The skip is silent — neither has a logger, and neither is a place to
introduce one.

### 3.4 Guard placement — the position never names a function the caller did not call

All **32** constructors validate their own `opts`. The 24 that apply their own options fold the check into the
existing loop; the **8 delegators** run a standalone pre-check before forwarding:

```go
// stdlib.NewInbound, before delegating to msghttp.NewConfig
for i, opt := range opts {
	if opt == nil {
		return nil, nilOptionAt("stdlib.NewInbound", i)
	}
}
cfg, err := msghttp.NewConfig(opts...)
```

The delegate re-checks and finds nothing. **The duplicated pass is deliberate** — O(len(opts)) over a slice
essentially always under ten elements, off the message path entirely, and it buys a truthful position at every entry
point. Without it, `stdlib.NewInbound(ch, nil)` would report `msghttp.NewConfig: nil option at index 0`, naming a
function the caller never called. A future delegator that forgets its guard degrades to the delegate's less-precise
position rather than to a panic.

### 3.5 Precedence against other construction faults

**This section governs R1 only.** R2's method-entry order is D-V's, and is deliberately the other way round: R1
preserves each constructor's *existing, per-constructor* precedence because those orders are already designed and
documented, whereas R2's five products share one newly-introduced latch and get **one uniform rule** so a reader can
predict them without reading each method. §3.2 carries D-V; this section does not apply to it.

**The nil-option check runs where the existing apply loop runs; a fault validated before that loop is reported
first.** This is per-constructor and **deliberate**: it preserves each constructor's existing error precedence, which
this project already treats as a designed property (`adapter/cron/source.go:184-186` carries an explicit comment to
that effect).

**For the 8 delegators, which have no apply loop**, the pre-check goes at the **top of the function**. This preserves
existing precedence because all eight already call their delegate as their first statement —
`stdlib/inbound.go:49` (`NewInbound`), `stdlib/inbound.go:97` (`NewInboundGateway`), `msghttp/outbound.go:317`,
`exchange.go:67`, `sseclient.go:58`, `sse.go:216`, `sse_server.go:119`, `sql/queuestore.go:37` — **all eight**.
**Stated rather than left to luck** (round-2 audit, M-F; the missing eighth ref, round-3 m-U).

The decided order for all 17 non-delegating R1 constructors — **derived, not assigned**; regenerate before citing:

| Order | Count | Constructors | `(nil args, nil opt)` yields |
|---|---|---|---|
| **VALIDATE-FIRST** — an argument check precedes the loop | **9** | `endpoint.NewConsumer` (`consumer.go:241`), `.NewProducer` (`producer.go:337`), `.NewGateway` (`gateway.go:26`), `.NewChannelExchange` (`exchange.go:298`); `routing.NewAggregator` (`aggregator.go:299`); **`resilience.NewTokenBucket` (`ratelimit.go:38`)**; `cron.NewSQLElector` (`sqlelector.go:88`), `.NewSQLLocker` (`sqllock.go:51`); `sql.NewInboxDeduper` (`inbox_dedup.go:74`) | the **existing** argument error (e.g. `ErrNilAdapter`, `ErrInvalidRateLimit`) |
| **LOOP-FIRST** — the apply loop is the first statement | **8** | `resilience.NewCircuitBreaker` (`breaker.go:77`); `memory.NewGroupStore` (`groupstore.go:78`), `.NewQueueStore` (`queuestore.go:84`); `cron.NewSource` (`source.go:174`); `sql.NewOutboundAdapter` (`outbound.go:54`), `.NewPollingSource` (`source.go:87`), `.NewGroupStore` (`groupstore.go:204`); `msghttp.NewConfig` (`options.go:1107`) | the **nil-option** error |

> **`resilience.NewTokenBucket` is VALIDATE-FIRST** — `if rps <= 0 || burst < 1 { return nil, ErrInvalidRateLimit }`
> precedes its loop (`ratelimit.go:38-40`), so `NewTokenBucket(0, 0, nil)` yields `ErrInvalidRateLimit`. Revision 3
> classified it loop-first because the derivation script matched only `== nil` / `== ""` patterns and missed a
> numeric guard (round-3 audit, M-L). **Re-derive by reading each constructor, not by pattern-matching.**

**Faults validated AFTER the loop lose to the nil option.** The two-way label above is lossy: several constructors
validate both before and after the loop (`validateIdent(cfg.table)`, `ErrInvalidLeaseTTL`, `newAdapterBase`). The
governing rule is the prose one — *a fault validated **before** the loop is reported first; everything validated
after it is not reached, because the nil-option error returns at the loop.*

Plan 028 asserts one case per **distinct order**, not one per package — `adapter/database/sql` alone contains three
shapes (loop-first, validate-first, and the delegator pre-check), so a per-package budget would leave two of them
unasserted and free to drift.

### 3.6 The one signature change

**`resilience.NewCircuitBreaker` gains an error return**, `msgin.CircuitBreaker` → `(msgin.CircuitBreaker, error)`,
moving it from R3 to R1. Three reasons, in order: its product cannot carry a fault (the `CircuitBreaker` interface is
`Allow() bool`, `Record(bool)`, `HalfOpen() <-chan struct{}` — `flowcontrol.go:42-46`, no error anywhere); its
sibling `resilience.NewTokenBucket` **already** returns `(msgin.RateLimiter, error)`, so the current asymmetry is
arbitrary; and it is constructed once at wiring time, never per message.

**10 call sites** move to the two-value form — all in `_test.go`, in **four** files: `resilience/breaker_test.go`
(5), `endpoint/consumer_test.go` (3), `endpoint/consumer_probegate_wiring_test.go` (1),
`endpoint/example_flowcontrol_test.go` (1). *(Revision 2 said 11; that counted the declaration. Re-derive with
`gopls` references before relying on it.)*

**The cost this decision must own:** two of the ten are **inline-composed** —
`endpoint.WithCircuitBreaker[string](resilience.NewCircuitBreaker())` — and one of those is a **published godoc
Example** (`example_flowcontrol_test.go:24`), which gains error-handling noise. That is the same inline-composability
cost §5 uses to *reject* error returns for R2's five; D-T pays it here only because R2 is structurally impossible.

**Membership tiebreak, stated so §2.1's "structural, not preference" stays true:** a constructor with no product
error surface takes an error return (R1) when it is **wiring-time-only and its sibling already returns one**;
otherwise it is R3. `msgin.New` (per-message) and `sqlite.DSN` (no sibling, returns a primitive) fail that test and
stay R3.

Nothing is tagged and there are no consumers, so the break is free — but it **is** a break, recorded in ADR 0031 and
subject to the SemVer gate if that ever changes.

### 3.7 What this spec does not change

- **No new sentinel.** `ErrNilFunc` is reused (ADR 0031 D-Q); root's sentinel set is unchanged.
- **No other signature change.** Apart from §3.6, no constructor gains, loses or reorders a parameter or return.
- **A nil variadic *slice* is not a nil element.** `NewConsumer(src, h)` and `NewConsumer(src, h, nil...)` both yield
  zero iterations; both are valid, unchanged, no-error calls.
- **Nil *behavior* arguments** (`pred`, `fn`, `h`, `svc`) keep their existing positions and errors.
- **Nothing about a non-nil option that panics when applied.** **D-U widens this**: under `break`,
  `memory.New(nil, WithBuffer(1<<62))` stopped at the nil; under `continue` it proceeds into `make(chan)` and
  panics. That is not a regression — it panicked without the nil before too — but the headline contract says *no
  constructor panics on a nil option element*, and this combination does. Owned here, deferred below (round-4
  audit, m-Z6). `memory.WithBuffer(1<<62)` panics inside `make(chan)`
  — a msgin-provided option that panics on caller input, proven by the round-1 audit. It is a real defect of the same
  family and is **explicitly deferred** (§7), not fixed here.

## 4. Documentation contract

1. **`ErrNilFunc` godoc** (`errors.go:207-230`): add the nil **option element** to the enumerated causes and the new
   position shape to the "Applied here" paragraph. **Do not touch the governing invariant paragraph**
   (`errors.go:212-221`) — this increment is an instance of it. D-S's extension is recorded in ADR 0031, not by
   editing the invariant.
2. **The 2 R3 constructors:** the verbatim sentences in §3.3.
3. **The 25 R1 + 5 R2 constructors:** one sentence naming the error and that the index is reported. **Where a
constructor's godoc already enumerates its check order** — `cron/sqllock.go:49`, `cron/sqlelector.go:84`,
`sql/inbox_dedup.go:71`, and `cron/source.go:184-186`'s precedence comment — the new sentence must say **where the
option check sits** in that order, not merely that it exists; the guard interposes into a sequence those comments
describe, leaving them literally true but incomplete (round-4 audit, m-Z7). R2's says the
   degradation is **permanent** and names the methods that report it.

## 5. Alternatives rejected

| Alternative | Why rejected |
|---|---|
| **Skip everywhere** (revision 1's R3, all 7) | **Contradicts `handler.go:47-51` verbatim** for the identical fault shape. Concretely: a dropped `WithID` makes `msgin.New` stamp a random id where the caller supplied an idempotency key (`message.go:165`); a dropped `WithSingleSubscriber` voids the `ExclusiveSubscribable` contract so a second subscriber is accepted and every message is processed twice. Killed by the round-1 audit. |
| **Error returns for all 7** | Two mechanisms and simplest to explain, but **103 call sites** churn (`memory.New` 41, `NewRouter` 23, `NewPublishSubscribeChannel` 20, `NewCircuitBreaker` 10, `NewPubSub` 9 — declarations excluded), and it costs the inline composability Spec 014:1879 values for base constructors. R2 achieves the same signal at **zero** call-site churn; §3.6 takes the error return only where R2 is structurally impossible, and owns the same cost there. |
| **Reject everywhere, including `msgin.New`** | Would make `msgin.New(payload)` return `(Message[T], error)` — a per-message ergonomic cost on the library's most-called constructor (40 call sites in-repo) to report a static wiring mistake. |
| **Guard only at the 24 apply loops** | 8 fewer guards, but `stdlib.NewInbound(ch, nil)` and `http.NewOutbound(url, nil)` would name `msghttp.NewConfig` — a function the caller never called. |
| **A shared exported `msgin.ApplyOptions` helper** | One implementation, truthful positions. Rejected: reachable from 8 modules only as **new exported surface in root**, to solve an internal detail. [Spec 014 §3.3](014-core-package-layout.md) resolved exactly this way for eight shared unexported identifiers (`boxMessage`, `nilFuncStep`, …) — *precedent for duplication, cited as such, not a stated general rule.* |
| **A dedicated `msgin.ErrNilOption` sentinel** | Would let a caller distinguish nil-config from nil-behavior programmatically. Rejected: the fault *is* "a nil function reached a constructor", `ErrNilFunc`'s stated scope already reaches a nil element, and the position string separates the cases in the message. ADR 0031 D-Q. |
| **Fail-closed `CircuitBreaker` (`Allow()` always false)** instead of §3.6's error return | Needs no signature change, but a breaker that denies every call is indistinguishable from a real outage — it converts a wiring bug into a silent total denial, which is worse than the panic it replaces. |

## 6. Verification

**AC-1 — no panic.** Each of the **32** constructors, called with a nil option element, produces its family's outcome
and does not panic. Blackbox (`package <pkg>_test`), assert-closure tables.

**AC-2 — the index is computed, not hardcoded.** For each of the **24 R1 + 5 R2** whose option type has an
obtainable non-nil value, a case placing the nil at **index 1, after a valid option**, asserts the **full** position
string — `"<pkg>.<Ctor>: nil option at index 1"` — not an `index 1` substring.

> **Assert the whole string, including the constructor name.** D-R's entire cost — an eighth duplicated helper and a
> redundant pass in 8 delegators — is bought for *"a truthful position at every entry point"*, yet a substring
> assertion verifies the index and never the name. Task 1 is explicitly the template later tasks copy, and
> `msghttp`'s six constructors share one option type, so a copy-paste leaving `"endpoint.NewConsumer"` in
> `NewProducer` would pass every AC and ship exactly the fault D-R exists to prevent (round-4 audit, M-W). *(Not applicable to R3, which produces no message.)*

**`endpoint.NewGateway` is exempt, and the exemption is load-bearing — do not "fix" it.** `GatewayOption[Req, Rep]`
is `func(*gatewayConfig)` over an **unexported, empty** `gatewayConfig struct{}` (`endpoint/gateway.go:18-22`) and
has **zero exported constructors**, so no non-nil value of that type can be obtained from `package endpoint_test`.
Compile-proven by the round-2 audit:

```
vet: endpoint/zz_probe_test.go:10:60: name gatewayConfig not exported by package endpoint
```

Blackbox-only testing (Global constraint 1) is not negotiable, so AC-2 cannot be written for it; AC-1, AC-3 and AC-5
still apply and are executable with `(nil)` and `(nil, nil)`. **Open decision, deliberately not settled here:**
`NewGateway`'s `opts` are applied into a `cfg` it then discards (`gateway.go:31` returns
`&Gateway[Req, Rep]{exchange: x}`), so the parameter is inert today. Removing it would be a **second** signature
change, currently forbidden by Plan 028 Global constraint 3. Default is keep-and-exempt.

**AC-3 — first-nil-wins.** ≥1 case **per R1 package and ≥1 per R2 type** passes `(nil, nil)` and asserts the full
position string ending `index 0`.

> **The R2 half is not optional.** D-U's *"latch only when unlatched"* clause is what preserves first-nil-wins, and
> nothing else enforces it: an implementation latching the **last** nil passes AC-1, AC-2, AC-5, AC-5b, AC-5c and
> AC-5d unchanged. AC-6 already carries the killing mutant; it just had no R2 case to kill (round-4 audit, M-X).
> `NewRouter(nil, nil).Handle(…)` must report `index 0`.

**AC-4 — survivors still apply.** For each R3 constructor, **two** cases: `(realOpt, nil)` asserts the option
**before** the nil applied, and `(nil, realOpt)` asserts the one **after** applied. *(Revision 1 demanded
`(realOpt, nil, realOpt)` with "both took effect", which is unexecutable where a package exports exactly one option —
`memory.WithBuffer` and `routing.WithDefaultChannel` are the only options of their type and overwrite each other.)*

**AC-5 — the wrap is right.** R1 cases assert `errors.Is(err, msgin.ErrNilFunc)` **and** `IsPermanent(err) == false`.
R2 cases assert `errors.Is` **and** `IsPermanent(err) == true`. Asserting only `errors.Is` passes against a wrong
wrap, which is the distinction §3.1/§3.2 exist to preserve.

**AC-5b — every reporting surface of a latched object reports.** A latched `PublishSubscribeChannel` returns the
fault from **`Send` and `Subscribe`**; a latched `PubSub` from **`Publish` and `Subscribe`**; a latched `Router` from
`Handle`; a latched `Broker` from `Send` and `Stream`. `SingleSubscriber()`, `TopicCount()` and `EmitsLiveValue()`
are **not** forced to any value — they report whatever the surviving prefix set (§3.2).

**AC-5c — the fault is not laundered into a different diagnosis, in EITHER option ordering.** `NewChannelExchange(req,
reply)` must return an error matching `msgin.ErrNilFunc` — **not** `msgin.ErrSharedReplyChannel` — for **both**:

- `reply = channel.NewPublishSubscribeChannel(WithSingleSubscriber(), nil)` — nil second
- `reply = channel.NewPublishSubscribeChannel(nil, WithSingleSubscriber())` — **nil first**

The second case is the one that fails under a `break` loop and passes under D-U's `continue`; it is the executable
proof of D-U and must be written even though the first looks sufficient. *(Revision 3 asserted only the first
ordering, which held while the design was still broken — round-3 audit, M-M.)*

**AC-5d — D-V's precedence is asserted, not assumed.** One case per R2 type where a nil option is combined with the
method's own nil-argument fault: `latched.Subscribe(nil)` → the nil **option**, not `ErrNilHandler`;
`NewRouter(nil, nil).Handle(…)` → the nil **option**, not the nil `pick`; `Filter[string](nil, nil)` → the nil
**option**, not `nil pred`.

> **Instantiate every generic constructor explicitly in these cases.** `Filter(nil, nil)` does not compile —
> `cannot infer A` — and neither does `NewConsumer(nil, nil, nil)` (`cannot infer T`); `NewGateway` can never infer.
> Write `Filter[string](nil, nil)`, `NewConsumer[string](nil, nil, nil)`, `NewGateway[string, string](nil)`
> (round-4 audit, m-Z1). The `Filter` case fails against today's code and is what forces its realignment.

**AC-6 — every assertion is mutation-proven, by a mutant that targets THAT assertion.** Reverting the guard makes the
constructor **panic**, which turns every case red regardless of what it asserts — so guard-reversion certifies only
that a guard exists. It is permitted for **AC-1 alone**. Every other assertion needs a mutant it is specifically able
to kill:

| Assertion | Killing mutant |
|---|---|
| AC-1 (no panic) | revert the guard |
| AC-2 (index computed) | hardcode the reported index to `0` |
| AC-2/AC-3 (constructor name) | swap the `ctor` literal for a sibling constructor's — killable **only** by a full-string assertion |
| AC-3 (first-nil-wins) | report the **last** nil instead of the first |
| AC-5 (wrap) | flip the wrap — add `Permanent` to an R1 site, remove it from an R2 site (**both directions**) |
| AC-5b (every surface) | remove the latch check from **one** method at a time |
| AC-5c (no laundering) | force `SingleSubscriber()` to `false` on a latched channel — revision 2's rule |
| D-R (delegator position) | delete the delegator's pre-check; the case must fail on the **position string**, not on the absence of an error |

A case that survives its own mutant is not evidence and is rewritten. Plan 028 records the killed mutant per case.
*(Revision 2 specified guard-reversion for everything, which the round-2 audit proved dominated by the panic — the
project's own "mutation-test every new assertion" scar.)*

**AC-7 — the class cannot regress.** A gate fails when any function with a variadic `…Option` parameter lacks a nil
guard, so a constructor added later is caught without editing this spec.

**It must handle six hazards. The node-kind tally below is measured over all eight modules — a checker built to
fewer kinds is silently blind, which is BLOCKER-1's failure mode one abstraction level down:**

| # | Hazard | Instances today |
|---|---|---|
| 1 | `*ast.Ident` — bare types (`Option`, `PubSubOption`, …) | 35 |
| 2 | `*ast.SelectorExpr` — **qualified** types (`msghttp.Option`) | 11 |
| 3 | **`*ast.IndexExpr`** — single-param generics: `ConsumerOption[T]` (`consumer.go:240`), `ProducerOption[T]` (`producer.go:336`) | 2 |
| 4 | **`*ast.IndexListExpr`** — multi-param generics: `GatewayOption[Req, Rep]` (`gateway.go:25`) | 1 |
| 5 | Skip `*ast.FuncLit` | **5** — the four in `adapter/database/sql/harness/{lock:45,queuestore:28,groupstore:54,inbox:33}` **plus `adapter/http/exchange_test.go:590`** |
| 6 | Skip `_test.go` | **13 — 12 `FuncDecl` + 1 `FuncLit`**, so "skip `FuncLit`" covers only one of them (`endpoint/exchange_test.go:39,54,1158`, `adapter/http/exchange_test.go:69,590`, 8 × `RunTestX` in `crontest`/`dbtest`) |

Hazards 3 and 4 carry `NewConsumer`, `NewProducer` and `NewGateway` — **the increment's flagship constructors**. A
checker handling only kinds 1–2 skips all three silently. *(`//go:build ignore` files in `docs/plans/027-tools` were
listed in revision 2 as a hazard with instances; re-derivation found **0** — they contain no variadic options. Kept
as a future risk, demoted from the required list.)*

**Func-ness is undecidable from `go/parser` alone** — either load `go/types` per module, or match by name suffix and
**document that as the limitation**.

**It must accept all four guard shapes** the design mandates, or it flags the very constructors this increment fixes:
R1-folded (`if opt == nil { return … }` inside the loop), R1-pre-check (a standalone loop, no `opt(&cfg)`),
R2-latch (`{ x.err = …; break }`), and R3-skip (`{ continue }`).

**Vacuity probe (mandatory): plant TWO unguarded constructors — one bare-typed, one GENERIC — and require both RED**;
remove them, require GREEN. A probe that plants only a non-generic constructor would certify a gate blind to hazards
3–4.

**Fallback if the gate cannot be made honest:** a hand-enumerated table per package that calls **every** constructor
with `(nil)` and asserts non-panic. That is weaker than the AST gate but it **tests the invariant**. *(Revision 1's
fallback — asserting the constructor count is still 30 — is withdrawn: it gates a census, not a guard, and would pass
green against a tree with every guard deleted. It also enshrined the wrong number.)*

**AC-8 — no unintended surface change.** `apidiff` against **`docs/plans/028-root-api-baseline.txt`** — a **new**
baseline captured from `main` in Plan 028 Task 0 — reports **0 removals / 0 additions**.

> **Do NOT use `027-root-api-baseline.txt`.** It is the **pre-Plan-027** surface and reports **97 removals / 9
> additions** against the current tree *before this increment touches a line* — `docs/HANDOVER.md` §3 records exactly
> that number. Revisions 2 and 3 of this spec demanded 0/0 against it, which made the reference task's checklist
> **unsatisfiable on arrival** (round-3 audit, BLOCKER-D). The baseline must be captured, committed (no gate may
> depend on `/tmp`), and diffed against itself before Task 1 begins.

The baseline covers the **root package only** — `endpoint`, `routing`, `channel`, `resilience` and the adapters are
not in it — so §3.6's deliberate `resilience` break is invisible to it and is verified by the 10 updated call sites
compiling instead.

**AC-9 — green in all eight modules.** `GOWORK=off go test ./... -race -shuffle=on` per module, plus the other six
per-module CI steps from CLAUDE.md's Library quality gates.

## 7. Scope

**Multi-instance / topology (CLAUDE.md mandate).** This increment adds **construction-time validation only**. It
introduces no state spanning a message's or request's lifetime, no correlation, coordination, dedup, registry or
sequencing. The latched R2 fault is per-object, set once at construction, read-only thereafter, and never crosses a
process boundary. **The mandate is therefore N/A here — stated rather than assumed, as CLAUDE.md requires.**

**Out of scope**, all recorded in [`docs/HANDOVER.md`](../HANDOVER.md) §6 or by the round-1 audit:

- `memory.WithBuffer(1<<62)` panicking inside `make(chan)` (§3.7) — a msgin-provided option that panics on caller
  input. Same family, different fault; needs its own increment covering **all** options, not a one-file patch.
- `admit`'s untested ctx-done arm; `endpoint/consumer.go:467`'s flaky ctx-done arm; the non-machine-detectable
  no-sink discard arm (Spec 014 §2.1 row 10).
- The `gin` increment, which this spec **displaces to a plan number after 028**.
