# ADR 0029 — EIP lexical alignment, named behavior types, and the expr provider module

- **Status:** **ACCEPTED — REGENERATED FROM A GREEN TREE (2026-07-28); round-3 audit returned
  `NEEDS-REVISION` 3/3 and its findings are folded in below.** §1 and §3 are implemented (commit `b6ce7bb`
  for the rename); §5's `expr` module is Plan 027 Task 10 and not yet built.
  > *Round-3 corrections in this ADR:* §5a's provider signatures **did not compile** —
  > `Correlation`/`Release`/`RouteFunc` must be `[A any]`, because `compile[A]` type-checks `payload.Field`
  > and `PayloadOf[A]` **is** the M-6 `ErrPayloadType` branch §3b hands to Task 10; and §2's AMQP-disclaimer
  > line is written as though it exists — `grep -rn -i 'amqp' --include='*.go' .` returns nothing, so it is an
  > outstanding Spec 014 §8 obligation, now owned by Plan 027 Task 11b. Evidence: F13.
  > *Round-2 banner cleared 2026-07-28.* Every defect it named is fixed **in this ADR**:
  > §3's *"fixes all three"* is **withdrawn** — `cfg.optErr` is deleted instead (**decision D-D**, §3a), and
  > the three surviving branches are the `Handle`-side ones, restored by pulling **D-E** forward into Task 1
  > (§3b); `WithReleaseStrategy` now **takes** the named `ReleaseStrategy` (**decision D-E**, §3), shipped at
  > `routing/aggregator.go:82` with `WithReleaseWhen` as the sugar; and §5's provider shape is **no longer
  > claimed to be uniform** — `RouteFunc` takes a second parameter (§5a). Evidence:
  > [`027-derivation-findings.md`](../plans/027-derivation-findings.md) F3, F5, F10.4.
  *(Rounds 1 and 2 both returned `NEEDS-REVISION` from all three auditors; revised in place both times rather
  than superseded because this ADR had never been implemented. Folded in round 1: §3's `ReleaseStrategy` →
  `(bool, error)` (§H5), §2's citation **VERIFIED** (G), §1's rename scope corrected (F7). See
  **[audit round 1](../plans/027-audit-round-1.md)** §K.)*
- **Decisions folded in 2026-07-28:** **D-D** (delete `cfg.optErr` and its `NewAggregator` guard — §3a) and
  **D-E** (`WithReleaseStrategy(ReleaseStrategy)` + `WithReleaseWhen` — §3), both settled 2026-07-27
  ([audit round 2 §G.1](../plans/027-audit-round-2.md)); **D-I** (the two expr sentinels leave root — §5.0a)
  and **D-K**, both settled 2026-07-28.
- **Decisions folded in 2026-07-28 (round 6):** **D-K is REVISED** — the `expr` providers wrap
  **`msgin.ErrPayloadType`** and `expr.ErrExprResultType` is **not declared at all**, superseding the
  *"declare it, then wrap it in `msgin.Permanent`"* form recorded earlier the same day (§5.0b/§5.0c); and
  **D-M** — a **deterministic endpoint fault carries its own retry classification**, generalizing D-K from one
  sentinel to the class (§5.0b). Both from [round 6 §1](../plans/027-audit-round-6.md).
- **Decisions folded in 2026-07-28 (round 7):** **D-M gains a scope correction** — `ErrNilSink`
  (`handler.go:55`, `msgin.To`) is in the class, and §5.0b's invariant is restated **sentinel-agnostically**
  with a re-derivation command that is not scoped to one name and is not blind to unqualified producers inside
  package `msgin`; and **D-N** — **`divert` falls back to the DeadLetter sink before discarding**, closing the
  data-loss path D-M would otherwise open in every finite-retry consumer's *default* configuration (§5.0b).
  D-N **amends [ADR 0007 D7](0007-reliability-settlement-api.md#d7--no-invalid-sink-policy-tasks-45)**. Both
  from [round 7 §1](../plans/027-audit-round-7.md).
- **Decisions folded in 2026-07-30 (round 8):** **D-P** — **the invalid-path fallback is SINGLE-SHOT**: when
  the fallback target's `Send` fails, the settlement falls through to D7's discard (WARN naming both the
  classification cause and the sink error, then `Ack`) instead of `Nack`ing with requeue, which D-N as first
  recorded turned into an unbounded redelivery loop no counter, backoff or breaker could observe (§5.0b).
  D-P **amends D-N** and **re-amends [ADR 0007 D7](0007-reliability-settlement-api.md#d7--no-invalid-sink-policy-tasks-45)**.
  Recorded in the same pass: **D-N's two accepted costs** (dead-letter amplification of `ErrPayloadTooLarge`;
  poison-storm volume), **the DLQ's missing settlement-reason discriminator** (a multi-instance disclosure per
  CLAUDE.md), and **D-M's producer-side blast radius** (`endpoint/producer.go:453-455` —
  `errors.Is(err, ErrDeadLettered)` flips `true` → `false`; Spec §2.1 **row 8**). All from
  [round 8 §1–§2](../plans/027-audit-round-8.md).
- **Also folded 2026-07-30 (round 8, design B7): D-M's INVARIANT IS CORRECTED — the round-7 wording was
  compile-proven FALSE**, and Plan 027 Task 9.7 was about to write it verbatim into `errors.go:152`, a public
  godoc. `Chain(To(*DirectChannel)).Handle` → `ErrNoSubscriber` and `Chain(To(*QueueChannel[reject])).Handle`
  → `ErrOverflowDropped` are both typed errors msgin returns **from inside a `MessageHandler` body it
  constructs**, and both are correctly **not** `Permanent`. The discriminator is **immutability of the cause at
  construction**, not the undefined word *"deterministic"*; the corrected invariant has **four** arms and is
  checked line-by-line against all twelve swept sites (§5.0b). Three of the seven bare triages had their
  rationale corrected — *"no `MessageHandler` body"* → **mutability** — with **no conclusion changed**.
- **Cites:** [ADR 0002 — Adapter SPI](0002-adapter-spi.md), which defined the `StreamingSource` name that §1
  renames and the `PollingSource`/`Delivery` contracts that stay put.
- **Amends:** [ADR 0019 — Runtime expression evaluation](0019-runtime-expression-evaluation.md) — its decision
  to accept `expr-lang` as a core-module dependency is reversed; its fail-at-construction contract is preserved.
- **RFC:** [0002](../rfcs/0002-eip-alignment.md), [0003](../rfcs/0003-endpoint-behavior-types.md) ·
  **Spec:** [014 §6–§7](../specs/014-core-package-layout.md) · **Plan:** [027](../plans/027-core-package-layout.md)
- **Relates to:** [ADR 0027](0027-core-package-restructure.md) (the packages these names live in),
  [ADR 0016 — robfig/cron dependency](0016-robfig-cron-dependency.md) (**unchanged** — see the dependency rule
  below, which explains why `expr-lang` leaves and `robfig` stays).

## Context

An audit of all 35 implemented components against Hohpe & Woolf and Spring Integration found high fidelity, with
drift concentrated in a handful of names. Separately, each composition endpoint takes an anonymous closure and
has a mirror `*Expr` twin (`FilterExpr`, `RouterExpr`, `TransformExpr`, `SplitExpr`, `WithCorrelationExpr`,
`WithReleaseExpr`). Those six in-core twins are the **only** reason `go list -deps .` pulls in `expr-lang` — a
**7.1 MB** tree forced on all seven modules in the workspace, including a consumer using nothing but the SQL
adapter.

The Aggregator already models the target shape: `WithCorrelationStrategy` (a named strategy) plus
`WithCorrelationExpr` (an expr provider of it). The question was whether to generalise that shape, and if so
whether the behavior contracts should be **func types** or **interfaces** — a choice that cannot be deferred,
because widening func→interface later is itself breaking.

## Decision

### 1. `StreamingSource` → `EventDrivenSource`

The canonical EIP term is *Event-Driven Consumer* (Spring: `EventDrivenConsumer`), and "streaming" collides with
unrelated streaming-data vocabulary. This was the clearest drift in the register.

**Scope, measured:** `StreamingSource` appeared **30 times across 12 `.go` files, all inside the root
module** — no satellite module referenced it. *(Corrected 2026-07-27, audit finding F7: the plan had sized
this as a seven-module rename.)* The rename **as it stands at `dadc775`** is `36 occurrences across 15 files`:

```
# REGENERATED at dadc775 (round-4 fix pass); was 30/12/35/14, taken before 1d7fc80 added endpoint/doc.go
$ grep -rn 'EventDrivenSource' --include='*.go' . | wc -l ; grep -rl 'EventDrivenSource' --include='*.go' . | wc -l
      31      13
$ grep -rn 'EventDrivenSource' . --exclude-dir=.git --exclude-dir=docs | wc -l
      36                          # +2 in CLAUDE.md, +3 in MESSAGING.md
$ grep -rn 'StreamingSource' . --exclude-dir=.git --exclude-dir=docs
(no output, exit 1)
```

**`MESSAGING.md` is named nowhere in this bundle** and carries three of the five (F10.4). The Consequences
already require editing `CLAUDE.md`; `MESSAGING.md` joins it.

**The rename edits a user-visible string**, which no move-list would show (round-2 §D14):

```
$ grep -n 'EventDrivenSource' errors.go
22:	ErrUnsupportedSource = errors.New("msgin: source implements neither PollingSource nor EventDrivenSource")
```

It belongs in `MIGRATION.md`.

**The verification gate must be scoped to `.go` plus the two root narratives, and must exclude `docs/`**
(round-2 §C4): `grep -rn 'StreamingSource' --include='*.md' .` returns **129 hits across 29 files** there,
including shipped ADRs 0002/0006/0008/0009/0010/0017/0018/0023 and shipped specs, which CLAUDE.md forbids
rewriting (*"supersede rather than rewrite old ADRs"*).

**The `Stream` method keeps its name.** It describes the mechanism accurately, Spring offers no counterpart name
to align with, and renaming it would churn every adapter for no gain. Recorded so it reads as considered rather
than overlooked.

### 2. `Exchange` is kept, qualified

Root keeps `RequestReplyExchange`; the implementation is `endpoint.ChannelExchange`, and its godoc **must
carry** a line disclaiming AMQP's broker-side-routing-table meaning.

> **NOT YET SHIPPED — corrected round 3.** This sentence, and Spec 014 §6, were written in the present tense
> ("with a godoc line disclaiming…") as though the line already existed. `grep -rn -i 'amqp' --include='*.go' .`
> returns **nothing**, workspace-wide. It is an outstanding **Spec 014 §8** obligation owned by Plan 027
> Task 11, and it has a grep-verifiable checkbox there now.

> **VERIFIED (audit round 1, finding G) — the decision stands.**
> `org.springframework.integration.gateway.RequestReplyExchanger` exists and is Spring Integration's default
> gateway service interface: *"When a gateway is declared with no `service-interface`, an internal framework
> interface `RequestReplyExchanger` is used"*
> (`docs.spring.io/spring-integration/reference/gateway.html`). Spring 6.5 adds `AsyncRequestReplyExchanger`.
> "Exchange" is therefore consensus vocabulary in this domain, not drift, and this decision does **not** revert
> to a rename. Plan 027 Task 3 is unblocked.
>
> **Recorded nit, decided.** Spring's is `RequestReplyExchang**er**` (agent noun, method `exchange`); ours is
> `RequestReplyExchange` with method `Exchange`. **We keep our form.** Go's single-method-interface convention
> points at the `-er` suffix, but our name is the *pattern* (Request-Reply), not the *agent*; the method is the
> verb; and `RequestReplyExchanger.Exchange` stutters agent-and-action. The citation was asked to establish that
> "exchange" is consensus vocabulary, and it does.
>
> *(One auditor reported this UNVERIFIED — it had no web access. The design auditor's verification, with the
> source above, is the one that stands.)*

### 3. Behavior types are named **func types**, with combinators as methods

```go
// package routing
type Predicate[A any]    func(ctx context.Context, m Message[A]) (bool, error)
type RouteFunc           func(ctx context.Context, m Message[any]) (MessageChannel, error)
type SplitFunc[A, B any] func(ctx context.Context, m Message[A]) ([]Message[B], error)
type CorrelationStrategy func(m Message[any]) (string, error)
type ReleaseStrategy     func(g MessageGroup) (bool, error)

// package transform
type Transformer[A, B any] func(ctx context.Context, m Message[A]) (Message[B], error)

func (p Predicate[A]) And(q Predicate[A]) Predicate[A]
func (p Predicate[A]) Or(q Predicate[A]) Predicate[A]
func (p Predicate[A]) Not() Predicate[A]
```

**`ReleaseStrategy` carries an error — corrected 2026-07-27 (findings B2/B3, decision §H5).** An earlier draft
wrote `func(g MessageGroup) bool`, which was **the sugar's shape mistaken for the contract's**. Three things
falsify the bool-only form:

1. **The internal field is already fallible.** `aggregator.go:15` declares `release func(MessageGroup) (bool, error)`.
2. **The error arm is load-bearing behavior with existing tests.**
   `TestAggregator_ReleaseExprReaperFallThrough` asserts the eval error propagates from `Handle`;
   `TestAggregator_ReleaseExprDrainCheckError` asserts the drain loop swallows it. An
   `expr.Release(s) (routing.ReleaseStrategy, error)` returning a bare `bool` would have to **swallow** runtime
   errors — silently returning `false`, stranding a group forever — or **panic** on caller input, which
   CLAUDE.md forbids. Either way §5's stated parity bar for the provider is unmeetable.
3. **It would orphan measured hot-path branches.** With the four `*Expr`-driven aggregator tests and
   `expr_test.go` excluded, coverage drops `NewAggregator` 100%→93.8% and `Handle` 100%→94.7%; the newly
   uncovered blocks are exactly `if cfg.optErr != nil { return nil, cfg.optErr }` and the release-decision
   error return. A bool-only `WithReleaseStrategy` cannot reach either — and CLAUDE.md makes an untestable
   hot-path branch a delivery blocker.

**The API that ships — decision D-E** (settled with the user 2026-07-27; shipped at
`routing/aggregator.go:82,89`):

```go
func WithReleaseStrategy(fn ReleaseStrategy) AggregatorOption            // the named, fallible type
func WithReleaseWhen(fn func(MessageGroup) bool) AggregatorOption        // sugar — wraps to (bool, nil)
```

> *Corrected (round-2 §D1).* An earlier draft kept `WithReleaseStrategy(func(MessageGroup) bool)` while
> naming the type `ReleaseStrategy` — so `agg.WithReleaseStrategy(myReleaseStrategy)` **did not compile**, and
> `expr.Release`'s return value had to go to a differently-named `WithRelease`. The option now takes its own
> named type, exactly as `WithCorrelationStrategy` already does, and the bool-only sugar is renamed
> `WithReleaseWhen`. This **renames** today's `WithReleaseStrategy` and **removes** the proposed `WithRelease`;
> both are breaking, which is free (zero tags, zero consumers), and both are in `MIGRATION.md` and Spec 014
> §4.1's `apidiff` expectations. `expr.Release` returns `routing.ReleaseStrategy`, so
> `WithReleaseStrategy(expr.Release(…))` now compiles.

It is also consistent with `CorrelationStrategy`, which already returns `(string, error)`.

### 3a. `cfg.optErr` is DELETED, not rescued — decision D-D

The claim that the fallible type *"fixes all three"* was **false**, and the compiler settled it. `cfg.optErr`
had **exactly one class of writer** — `expr.go`, which Task 1 deletes:

```
$ grep -n optErr aggregator.go          # AFTER deleting expr.go
256:  ...   257:  ...                    # the two READ sites only; every writer was in expr.go
```

So `aggregator.go`'s `if cfg.optErr != nil { return nil, cfg.optErr }` guard became **dead code no public API
could reach** — unreachable, not merely untested. **Decision: delete the field and the `NewAggregator` guard
in Task 1** (round-2 §B4; F5). `NewAggregator` is not "rescued to 100%"; the uncoverable branch is removed,
which is the correct fix for an unreachable branch.

Consequence for the plan: Task 9's old verification criterion — *"aggregator coverage returns to 100% on
`NewAggregator` **and** `Handle`"* — is **void as written**. `routing` measures 100% today with the guard
gone.

### 3b. D-E is a **Task 1 prerequisite**, not Task 9 work — a task-ordering fact, and it is load-bearing

`aggregator_test.go` held 10 references to the `*Expr` API. They split three ways, and the earlier plan
treated them as one undifferentiated group (F3):

| Case | Branch | Verdict |
|---|---|---|
| M-1 empty group snapshot | `toGroupEnv` guard (declared in `expr.go`) | genuinely leaves with expr → Task 10 |
| M-6 non-`A` member → `ErrPayloadType` | `toGroupEnv` guard (declared in `expr.go`) | genuinely leaves with expr → Task 10 |
| **H-1 reaper fall-through** | `reapGroup`, **core** | **must survive** |
| **H-2 drain-loop residual release-check error** | `release`, **core** | **must survive** |
| **H-3 drain-loop residual `releaseOnce` failure** | `release`, **core** | **must survive** |

H-1/H-2/H-3 are reachable **only** when the release check can return an error. Before this window the sole
fallible release strategy was `WithReleaseExpr`. **So Task 1 removes the only driver for three core hot-path
branches, and deferring D-E to Task 9 loses them silently** — a coverage loss no gate would attribute
correctly. Resolution taken: D-E was pulled forward into Task 1 and H-1/H-2/H-3 rewritten over a Go-func
`requireQtyRelease(min) msgin.ReleaseStrategy` helper. Coverage preserved.

*(Two fixtures died with M-6 and nothing reported it: `mixedTypeAddStore` and `mixedTypeGroup` existed only to
drive it, `unused` is off, and Go does not error on unused package-level declarations. Removed by hand.
`emptyGroupAddStore` **is** still live — the two are not symmetric despite looking it. F4.)*

**Interfaces + `XxxFunc` adapters were rejected.** That shape (`http.Handler`/`HandlerFunc`) would let a provider
implement `String()`, so a log line could name the expression that rejected a message — real value against
CLAUDE.md's debuggability criterion. But generics make the adapter conversion **explicit at every inline call
site** (`routing.PredicateFunc[Order](func(...){...})` — there is no inference on a generic type conversion),
taxing the common case to serve the rare one. The debuggability need is met instead by the expr provider
**wrapping runtime failures with the source expression text**, which covers the case that actually matters: a
predicate that errors.

### 4. Names drop the qualifier the package already carries

`routing.Predicate`, not `routing.FilterPredicate` (the draft) and not `routing.MessageSelector` (Spring).
Once ADR 0027 puts these in `routing` and `transform`, both candidates repeat what the package says, which is
exactly the stutter Go naming convention warns against; `transform.Transformer` has direct precedent in
`golang.org/x/text/transform`. **Each godoc names its Spring equivalent**, so recognition survives the
divergence — that mitigation is load-bearing, not decorative, and the plan must verify it on every type.

### 5. Expression support moves to its own module; the `*Expr` constructors are removed outright

No deprecated shims — nothing is tagged, so there is no consumer a shim would protect. Providers return the
§3 types, keeping the compile error at the provider call so the base constructors stay non-fallible and
inline-composable and ADR 0019's fail-at-construction contract survives:

```go
func Predicate[A any](s string) (routing.Predicate[A], error)
```

**A separate module, not a subpackage** — a subpackage of the root module would leave `expr-lang` in the root
`go.mod` and deliver none of the benefit. **It needs `require github.com/kartaladev/msgin v0.0.0` plus a
`replace … => ..`, exactly as every satellite module carries**: `git tag | wc -l` → 0, so without the
`replace` the module cannot resolve the root module under `GOWORK=off` — which is how CI's `module` job runs
it. A `use` line in `go.work` is necessary but not sufficient (round-2 §C2).

#### 5.0a The module owns its two error sentinels — decision D-I (2026-07-28)

> **STATUS: IMPLEMENTED (Plan 027 Task 9.5).** Both sentinels and their godoc are deleted from root:
> `grep -rn 'ErrInvalidExpression\|ErrExprResultType' --include='*.go' .` is empty workspace-wide, and root
> measures **100** exported symbols / **41** sentinels (`apidiff` **97** removals), down from 102 / 43 / 95.
> **The one replacement is now IN — Plan 027 Task 10** ships the `expr` module's own
> `expr.ErrInvalidExpression` with the `msgin/expr:` prefix, and it is the module's **only** sentinel
> (revised D-K withdrew the second — see §5.0b). Every present-tense sentence below now describes the tree.
>
> *Line numbers are deliberately not republished here.* The previous form cited `errors.go:180`/`:206` pinned
> at `dadc775`; by the time Task 9.5 ran, the real positions were `:196` and `:223`, and the plan and Spec 014
> each published a **third, different** pair. Three documents, three answers, all wrong — so the citation is
> struck rather than refreshed. Locate a sentinel by symbol.

The `*Expr` deletion left `msgin.ErrInvalidExpression` and `msgin.ErrExprResultType` with **zero producers
anywhere in the workspace** — the only two root sentinels in that position. **Both leave root.** This module
declares `expr.ErrInvalidExpression` (prefix `msgin/expr:`); it declares **no** replacement for
`ErrExprResultType`, which under **revised D-K** (§5.0b) is expressed by wrapping root's existing
`msgin.ErrPayloadType`. So the `expr` module mints **one** sentinel, not two.

This **narrows an earlier reading of §5 in this ADR**. §5 says providers keep "the compile error at the
provider call", and Plan 027 §9.5.0 originally read that as an argument for keeping the *sentinel* in root so
the fail-at-construction contract had one home. The two are separable: what §5 protects is **where the error
is raised** (at the provider call, not at first message), which is untouched. **Which package declares the
`error` value** is a different question, and the tree already answers it — `adapter/http`, `adapter/database/sql`
and `adapter/cron` each mint their own sentinels for their own faults (51 of them) while returning root's for
contract-level faults (27 file→sentinel pairs). Root has no notion of an expression and, after Task 1, no code
that could produce one; the fault is the provider's. Spec 014 §3.2 carries the measured evidence, §7 the
declaration.

**Consequence:** `errors.Is(err, msgin.ErrInvalidExpression)` becomes `errors.Is(err, expr.ErrInvalidExpression)`,
and `errors.Is(err, msgin.ErrExprResultType)` becomes **`errors.Is(err, msgin.ErrPayloadType)`** (revised D-K,
§5.0b) — the second stays a root target, so that caller keeps the import they already had. The first caller
already imports `expr` to construct the endpoint, so no new import appears there either; `MIGRATION.md` carries
both lines. No alias is provided — an alias would have to reference the root vars this decision deletes.

#### 5.0b The two sentinels are NOT symmetric, and a deterministic fault needs a retry classification — decisions D-K (revised) and D-M

> **STATUS: IMPLEMENTED.** D-M landed in Plan 027 Task 9.7 (`64963ad`); **D-K landed in Task 10**. The `expr` module exists as the eighth module, and its result-type fault returns
> `fmt.Errorf("%w: expr result %T is not %T", msgin.ErrPayloadType, got, want)` with **no**
> `msgin.Permanent` wrap — proved end-to-end by an acceptance fixture in the module's own tests (a real
> Consumer + `RetryPolicy` + dead-letter sink + `WithInvalidMessageSink` over a re-emitting source), which
> asserts the fault reaches the invalid-message sink **without consuming a retry** and never reaches the
> dead-letter sink. `ErrPayloadType`'s godoc was widened in the same commit (§5.0c below).
> **D-M is a behavior change to shipped code** and is listed in the register Spec 014 §2.1 keeps.
>
> **Which task realizes which half — the two are NOT interchangeable** (round-7 R-B4/D-B7/X-B3: Task 9.7 was
> cited in no normative document, so a worker executing Task 9 against this ADR would fold 9.7's shipped-code
> edits into Task 9's additive commit):
>
> - **Plan 027 Task 9.7** owns the **shipped producers** — the flow-path sites in the table below. It is a
>   behavior change to existing code and carries the `!` marker.
> - **Plan 027 Task 9** owns the **combinators** (`Predicate.And`/`Or`/`Not`), which are new surface in this
>   window and simply author the rule rather than changing it.
> - **Plan 027 Task 10** owns the `expr` provider's use of revised D-K **and the `errors.go:6` godoc
>   widening D-K makes necessary** (round-7 D-B8). It is placed there, not in Task 9.5 — the task that
>   otherwise edits `errors.go` — because Task 10 writes the only producer the widened text describes;
>   landing it in 9.5 would leave root, for two commits, documenting a fault nothing in the workspace can
>   produce. **Task 10 therefore touches root**, and its commit scope is `feat(expr,core)`.
>
> **Task 9.7 executes FIRST, before Task 9** (round-7 D-M2/X-M2). Task 9.7's own rationale calls a
> half-classified tree *"worse than either uniform answer"*, and running 9 first creates exactly that state
> across three commits. Nothing in 9.7 depends on 9, 9.5 or 9.6 — the combinators return a `Predicate`, never
> a `Step`, so they never call `nilFuncStep`. The task **numbers** are unchanged (they are cross-document
> links); only the execution order is pinned.

**§5.0a's argument applies cleanly to `ErrInvalidExpression` and does NOT transfer to `ErrExprResultType`.**
The first is a **construction-time** fault raised at the provider call; ADR 0019's contract is untouched by
who declares it. The second is an **evaluation-time** fault that fires **per message, inside the flow**, and
therefore lands on the runtime's retry/dead-letter path:

```
$ git show ab233d9:expr.go | grep -n ErrExprResultType
179:  return Message[B]{}, fmt.Errorf("%w: result %T is not %T", ErrExprResultType, out, *new(B))
421:  return nil, fmt.Errorf("%w: SplitExpr result %T is not a slice", ErrExprResultType, out)
428:  return nil, fmt.Errorf("%w: SplitExpr element %d %T is not %T", ErrExprResultType, i, elem, *new(B))
```

Root's `IsPermanent` (`reliability.go:38`) enumerates `ErrPayloadType`, `ErrPayloadDecode` and
`ErrPayloadTooLarge` — faults that cannot fix themselves on redelivery. **A result-type mismatch is that same
class**, the expression-domain twin of `ErrPayloadType`, yet it is not in the list and the deleted originals
never wrapped it in `Permanent` either. So today it would be classified **transient**: retried `MaxAttempts`
times — and, per Spec 014 §10's per-instance attempt tracking, **`N × MaxAttempts` across N instances** — for
a deterministic fault that yields the identical wrong type every time.

That gap predates D-I. What D-I does is **close one door**: while the sentinel lived in root, `IsPermanent`
could be amended to name it; afterwards root cannot reference a provider-declared sentinel without re-creating
the import edge D-I exists to remove. An earlier draft called it *"the only door that could have fixed it in
one place"*, and **that was the false step** — the other door was always open and was never tried: **use a root
sentinel that `IsPermanent` already names.** Revised D-K walks through it.

**Decision D-K (REVISED, round 6 — supersedes the form settled earlier on 2026-07-28): the `expr` providers
wrap root's `msgin.ErrPayloadType`, and `expr.ErrExprResultType` is NOT declared at all.**

```go
return Message[B]{}, fmt.Errorf("%w: expr result %T is not %T", msgin.ErrPayloadType, out, *new(B))
```

> **What the earlier form was, and why it is withdrawn.** D-K as first recorded had the provider declare its
> own `expr.ErrExprResultType` and then wrap it —
> `fmt.Errorf("%w: result %T is not %T", msgin.Permanent(ErrExprResultType), out, *new(B))`. The paragraph
> above calls a result-type mismatch *"the expression-domain **twin** of `ErrPayloadType`"* and then mints a
> second sentinel for it anyway; §5.0c records the cost (every future CEL/starlark provider mints another, and
> callers get no shared `errors.Is` target) and offers two escapes, **neither of which was "wrap the twin"**.
> The obvious alternative was never weighed — the gap was the silence, not the reasoning.
> *(Round-6 design M4.)*

**Why the revised form is better on every axis that decided the original.**

- **One shared `errors.Is` target** for every present and future expression provider, which is exactly what
  §5.0c was written to lament.
- **The correct retry classification for free.** `ErrPayloadType` is already enumerated by `IsPermanent`
  (`reliability.go:38-49`), so **no `msgin.Permanent` wrap is needed** and D-K's whole classification concern
  dissolves rather than being paid for:

  ```
  IsPermanent(msgin: payload is not of the expected type   ) = true
  ```
- **No new root SYMBOL and no new import edge — but `ErrPayloadType`'s godoc IS widened.** The provider
  already imports root for `msgin.Message`, and no exported name is added or removed, so `apidiff` does not
  move. What does change is a doc comment, and it has an owning task: **Plan 027 Task 10** (round-7 D-B8).

  > **ROUND-7 CORRECTION (D-B8) — this bullet asserted there was nothing to do, and it was false on both
  > halves.** It read *"**No root change** and no new import edge. `ErrPayloadType`'s own godoc (`errors.go`)
  > is **domain-generic** — *a `Message[any]` payload cannot be asserted to `T`* — and a result-type mismatch
  > is that same statement about the expression's output."* Checked against the source rather than
  > paraphrased (run 2026-07-29):
  >
  > ```
  > $ sed -n '6,7p' errors.go
  > 	// ErrPayloadType is returned when a Message[any] payload cannot be asserted to T.
  > 	ErrPayloadType = errors.New("msgin: payload is not of the expected type")
  > ```
  >
  > That single line is the whole godoc, and it is **domain-narrow, not domain-generic**: it names a
  > `Message[any]` payload and the assertion to `T`. An expression's evaluated **result** is neither. The
  > paraphrase quoted in the withdrawn bullet was accurate — it was the *conclusion drawn from it* that
  > inverted. D-K stretches the sentinel past its stated contract, so the contract is widened to match, and
  > **no task did that until this round**.

**The cost of the revised form, recorded as an ACCEPTED TRADE-OFF rather than left silent.** After Task 10,
`errors.Is(err, msgin.ErrPayloadType)` no longer tells a caller **which** of two faults they have:

| Fault | Producer | Remedy |
|---|---|---|
| the inbound payload was not `T` | `PayloadOf`; the Consumer's live-value and wire decode; and, in `msgin/expr`, each provider's payload assertion (`RouteFunc`, `Correlation`, and the group projection behind `Release`) | fix the codec, or the adapter/producer that emitted the payload |
| the expression evaluated to the wrong type | the `msgin/expr` provider (and any future CEL/starlark provider) | fix the **expression** |

The remedies are **disjoint**, and one `errors.Is` target now covers both. That is the price of the shared
target §5.0c wanted, and it is paid deliberately: the discriminator moves from the **sentinel** to the
**error string**, and `ErrPayloadType`'s widened godoc says so explicitly so a caller is not left to
discover it. **The discriminator is ONE-SIDED — `"expr result"` present or absent.** Only the expression
side carries that marker, so the payload side is identified by its **ABSENCE**. State the rule that way and
only that way: **do not restate it as a count of which producers wrap**, because the payload side's producers
wrap inconsistently and their number is not stable. `PayloadOf` wraps with `"want %T, got %T"`; the Consumer's
two decode assertions return the sentinel **bare**; and `msgin/expr`'s three payload assertions wrap with
`(expression %q)`. None of those carries `"expr result"`, which is exactly why absence — not any particular
wrap — is the reliable test.

> **Two corrections, both to this paragraph, both the same defect.** *Round 8* struck an earlier form that
> named `"want %T, got %T"` as **the** payload-side discriminator — false, because two of the three producers
> then known returned bare. *Task 10 fix round 3* struck its replacement, which said *"just one of that
> side's **three** producers wraps at all"* — falsified **by its own commit**, which added three more
> payload-side producers in `msgin/expr`, all of which wrap. A rule phrased as an enumeration of producers
> has now been wrong twice; phrased as an invariant over the marker it is stable under any new provider.
> Line-number citations are struck here for the same reason §5.0a struck its own. A caller who genuinely must branch matches
the string; a caller who only needs the *classification* — the common case, and the one this whole decision
is about — gets it from `IsPermanent` either way.

> **Why this paragraph exists.** §5.0c's cost analysis was **narrowed to moot** for the evaluation-time half
> by revised D-K and **nothing replaced it**, which left this section listing only benefits. That is the
> identical silence-shaped defect that overturned the *first* D-K one round earlier — where the twin was
> minted without ever weighing "wrap the existing one". *(Round-7 design B8.)*

**D-I is unaffected.** `ErrInvalidExpression` still leaves root and the `expr` module still mints its own with
the `msgin/expr:` prefix: it is a **construction-time** fault with no root twin. What changes is the count —
root loses both sentinels, the `expr` module declares **one**, so **D-K does not move root's arithmetic at
all** (the `+1` in `43 − 2 + 1` is D-J's `ErrSharedReplyChannel`, not an expr sentinel) while the projected
`expr`-module sentinel count drops from 2 to **1**. Root's projected total is `43 − 2 + 1 + 1 = **43**`; the
final `+1` is **D-Q**'s `ErrNilMessageGroup` (§5.0d), a later and independent decision.

---

**Decision D-M (round 6): a deterministic endpoint fault carries its own retry classification, and
`ErrNilFunc` is `Permanent`.**

**The reasoning above is not specific to a result-type mismatch.** `IsPermanent` is a **closed enumeration**,
so *any* deterministic fault outside it is classified transient and retried. Measured at `aae6160` (root code
byte-identical to the `dadc775` pin — `git diff --name-only dadc775..aae6160 | grep -v '^docs/'` → `CLAUDE.md`
only):

```
IsPermanent(msgin: nil endpoint function              ) = false
IsPermanent(msgin: no route for message               ) = false
IsPermanent(msgin: payload is not of the expected type) = true
IsPermanent(msgin: message has no correlation key     ) = false
IsPermanent(msgin: nil outbound sink                  ) = false
```

> **This census is EVIDENCE, not a gate.** It measures a **sentinel**; D-M wraps at the **producer** and
> deliberately leaves `IsPermanent`'s closed enumeration alone, so every row above reads **identically before
> and after the change**. Re-measured at `fe86a12`:
>
> ```
> --- AFTER D-M (producer wraps; the SENTINEL is untouched) ---
> IsPermanent(msgin: nil endpoint function              ) = false
> IsPermanent(msgin: nil outbound sink                  ) = false
> --- what the producer now returns (the observable D-M actually moves) ---
> IsPermanent(producer error) = true ; errors.Is(err, ErrNilFunc) = true
> ```
>
> The observable D-M moves is the **producer path**, below. Anything that measures the bare sentinel and
> expects it to change is unsatisfiable by any correct implementation of this decision — that was round-7
> blocker X-B2 against Plan 027 Task 9.7's RED baseline. *(Counter-rule 7.)*

End-to-end, a `transform.Transform(nil)` step over a `memory` broker with
`RetryPolicy{MaxAttempts: 3, DeadLetter: dlq}` and **no** `WithInvalidMessageSink` — one message, hook tallies
and sink receipts, measured at `fe86a12`:

```
BEFORE  transform.Transform(nil)     OnRetry=2 OnDeadLetter=1 OnInvalidMessage=0 | dlqSink=1 invalidSink=0 discarded=false
AFTER   Permanent(ErrNilFunc)        OnRetry=0 OnDeadLetter=0 OnInvalidMessage=1 | dlqSink=0 invalidSink=0 discarded=true
```

> **`DeadLetter: dlq` is not an embellishment of the harness — it is the only legal shape.** An earlier
> revision of this block wrote `RetryPolicy{MaxAttempts: 3}`, which `NewConsumer` **rejects**:
>
> ```
> RetryPolicy{MaxAttempts: 3}.Validate()                  = msgin: finite MaxAttempts requires a DeadLetter sink
> RetryPolicy{MaxAttempts: 3, DeadLetter: sink}.Validate() = <nil>
> RetryPolicy{MaxAttempts: 0}.Validate()                   = <nil>
> ```
>
> (`retry.go:46-53`.) So **every consumer with finite retries necessarily has a DeadLetter sink**, and the
> `discarded=true` cell above is therefore not a corner case — it is what D-M does to the *default* finite-retry
> configuration. That is what **D-N** below exists to close.

A nil endpoint function — **the most deterministic fault the library can produce**, identical on every
redelivery for the process's lifetime — consumes the full retry budget, lands in the **dead-letter** sink
instead of the **invalid-message** sink, and, via
`endpoint/consumer.go:614` (`c.safeRecord(md.Msg.ID(), err == nil || msgin.IsPermanent(err))`) and
`endpoint/consumer.go:733`, **records an unhealthy signal that trips the circuit breaker**. One mis-wired
`Filter(nil)` opens the circuit for the whole consumer.

**The PRODUCER loses a durable capture — the same reclassification, the opposite sign** (round-8 A5). D-M's
blast radius above was measured on the consumer only. `endpoint/producer.go:453-455` returns on `IsPermanent`
**before** `p.deadLetter(...)`:

```go
// Permanent wins over every other classification, including RetryAfter:
// a delay is meaningless when the answer is "do not retry".
if msgin.IsPermanent(err) {
    return err
}
if p.retry.MaxAttempts > 0 && attempt >= p.retry.MaxAttempts {
    return p.deadLetter(ctx, boxed, err)
}
```

A `Producer` over a `*channel.DirectChannel` gets the subscriber's error back verbatim (`channel/direct.go:89`
returns `h.Handle(ctx, msg)`), so a mis-wired step downstream of a producer is exactly this error. Measured
(one message, `RetryPolicy{MaxAttempts: 3, DeadLetter: dlq}`, hooks and sink receipts counted; the AFTER rows
substitute the post-edit error shape, so the target is measured rather than predicted):

```
BEFORE D-M transform.Transform(nil) OnRetry=2 OnDeadLetter=1 | dlqSends=1 | Is(ErrDeadLettered)=true  Is(ErrNilFunc)=true  Is(ErrNilSink)=false IsPermanent=false
AFTER  D-M Permanent(ErrNilFunc)   OnRetry=0 OnDeadLetter=0 | dlqSends=0 | Is(ErrDeadLettered)=false Is(ErrNilFunc)=true  Is(ErrNilSink)=false IsPermanent=true
BEFORE D-M msgin.To(nil)           OnRetry=2 OnDeadLetter=1 | dlqSends=1 | Is(ErrDeadLettered)=true  Is(ErrNilFunc)=false Is(ErrNilSink)=true  IsPermanent=false
AFTER  D-M Permanent(ErrNilSink)   OnRetry=0 OnDeadLetter=0 | dlqSends=0 | Is(ErrDeadLettered)=false Is(ErrNilFunc)=false Is(ErrNilSink)=true  IsPermanent=true
```

**Two observable changes, one of them on an exported error contract:** `dlqSends` **1 → 0** (the producer's
dead-letter sink stops receiving these) and `errors.Is(err, msgin.ErrDeadLettered)` **`true` → `false`** on the
error `Producer.Send` returns. `OnDeadLetter` stops firing for this class too.

**Disposition — the new behavior is CORRECT and is kept; what was missing is the record.** Returning a
permanent fault to the caller *without* spending three attempts and *without* a dead-letter round-trip is the
right answer: `Producer.Send` is synchronous and hands the error back to caller code that can act on it, so
there is no message to lose — which is precisely why **D-N does not extend to the producer** and why the
producer has no `WithInvalidMessageSink` (`grep -n 'InvalidMessageSink\|invalidSink' endpoint/producer.go` →
no output). **But D-N's premise as recorded in round 7 — *"no configuration that previously captured a message
starts dropping it"* — is true of the consumer and FALSE of the producer**, and `errors.Is(err,
ErrDeadLettered)` flipping is exactly the kind of change the §2.1 register exists so that none rides in
silently. It is therefore recorded as **Spec 014 §2.1 row 8**, with a covering case in Plan 027 Task 9.7.

**The tree already contains the correct precedent, with D-K's exact rationale, written before this window.**
`routing/aggregator.go:151-160` wraps `ErrNoCorrelation`:

```go
// defaultCorrelate reads HeaderCorrelationID as the group key. A missing or
// empty header is Permanent(ErrNoCorrelation): the runtime's IsPermanent does
// not match a bare ErrNoCorrelation, so without the wrap the message would be
// retried to the dead-letter sink instead of diverted to the invalid-message
// channel.
```

**The discriminator — this is the rule, not the list.** Classify by **when the fault's inputs are fixed**:

- **Fixed at construction, or by the message itself → `Permanent`.** It cannot change on redelivery.
- **Evaluated per message against caller-supplied, possibly-mutable state → transient.** It may legitimately
  resolve on redelivery.

Applied to every in-tree instance:

| Sentinel | Inputs fixed | Classification | Rationale |
|---|---|---|---|
| `ErrNilFunc` (flow-path producers) | at construction — the nil is captured in the closure | **`Permanent`** | `nilFuncStep` closes over nothing; `Router.pick` is set once in `NewRouter`. Nothing can make it non-nil later |
| **`ErrNilSink`** (`handler.go:55`, `msgin.To`) | at construction — `sink` is captured by `To`'s closure | **`Permanent`** | Round-7 D-B1. Identical shape and identical discriminator arm to `nilFuncStep`: the returned `Step`'s handler body tests a value fixed when `To(sink)` was called. Measured `msgin.To(nil)` → `OnRetry=2 OnDeadLetter=1 OnInvalidMessage=0`, byte-for-byte the `transform.Transform(nil)` line |
| result-type mismatch (`expr`) | at construction (the expression) + by `T` | **`Permanent`** | Revised D-K above — inherited from `ErrPayloadType`, no wrap needed |
| `ErrNoCorrelation` | by the message's headers | **`Permanent`** (already) | `routing/aggregator.go:160` — the precedent quoted above |
| **`ErrNoRoute`** | **per message, by caller-supplied `pick`** | **transient — UNCHANGED** | `routing/router.go:48-56`: `pick` is a caller function evaluated per message; it may consult a routing table, feature flag, or lookup service that changes. A message unroutable now may be routable after a config reload. `WithDefaultChannel` is the documented way to make the outcome deterministic |

`ErrNoRoute` staying transient is the load-bearing half of the rule: it shows the discriminator **excludes**
as well as includes, so this reads as a classification rule rather than a sweep of everything unenumerated.

**The invariant — sentinel-agnostic, and it is the contract. Not a list, and not a quantifier over one name:**

> **Every typed error msgin returns from inside a `MessageHandler` body msgin itself constructs, *whose cause
> was fixed at construction and cannot change for the message's lifetime*, is `Permanent`; a fault that a
> later `Subscribe`, config reload or drain could resolve stays bare and transient; every typed error returned
> from a constructor is bare, because construction never reaches a `RetryPolicy`; and everything else — a
> typed error handed to a *caller* from a non-constructor API — is bare for the same reason.**

> ## ⛔ ROUND-8 CORRECTION (design B1/B7 — the D-M invariant as recorded in round 7 was FALSE)
>
> The withdrawn wording was: *"**Every deterministic typed error msgin returns from inside a `MessageHandler`
> body is `Permanent`; every one returned from a constructor is bare.**"* It is falsified by **two
> compile-proven counter-examples, both reachable from inside a `MessageHandler` body msgin constructs, in the
> tree as it stands** (`go run` in a throwaway worktree at `7ee3fd6`, output pasted whole):
>
> ```
> Chain(To(*DirectChannel)).Handle       err=msgin: channel has no subscriber
>   errors.Is(err, msgin.ErrNoSubscriber)=true  msgin.IsPermanent(err)=false
> Chain(To(*QueueChannel[reject])).Handle err=msgin: message dropped by overflow policy
>   errors.Is(err, msgin.ErrOverflowDropped)=true  msgin.IsPermanent(err)=false
> ```
>
> `To(sink)` returns a `Step` whose `HandlerFunc` body msgin owns (`handler.go:52-59`); `Chain` composes it
> into a `MessageHandler`. Both errors are therefore returned by msgin **from inside a `MessageHandler` body**,
> both are typed, both are as "deterministic" as the word can be made to mean without a definition — and
> neither is `Permanent`. **Both are also correct as they stand**, which is the point: the withdrawn invariant
> did not merely mis-describe an edge, it demanded a wrap that would be *wrong*.
>
> **The wording over-reached twice.** *"Deterministic"* was never defined and was carrying the whole load; the
> property that actually discriminates is **immutability of the cause at construction**, which is what the
> classification table two paragraphs above has said all along (*"Fixed at construction … it cannot change on
> redelivery"* vs *"Evaluated per message against caller-supplied, possibly-mutable state"*). And the two arms
> did **not partition the space** — the largest class, a typed error handed to a *caller* from a
> non-constructor API, fell through both while the paragraph below instructs a maintainer to *"triage the 63"*
> against it. The corrected form adds a mutability qualifier to arm 1, promotes the transient case to its own
> arm, and closes the space with a fourth.
>
> **The fourth arm is this pass's own addition** (round 8 §2's corrected wording carried three). Checking the
> three-arm form against all twelve swept lines left `endpoint/producer.go:589` outside every arm: it is not in
> a `MessageHandler` body, it is not resolved by a `Subscribe`/reload/drain, and `SendAfter` is not a
> constructor. Recorded as a deliberate one-clause extension rather than applied silently.
>
> **The conclusions of the seven bare triages all survive; three of them rested on a rationale that does not.**
> `queuestore.go:146`/`:151` and `direct.go:87` were triaged *"no `MessageHandler` body"* — false, as the
> transcript above shows. Their cells now cite **mutability**, which is both true and the actual reason.

**The corrected invariant, checked against all twelve swept lines** (the sweep is two paragraphs below):

| Line | Sentinel | Arm | Verdict | Matches the tree |
|---|---|---|---|---|
| `endpoint/helpers.go:21` | `ErrNilFunc` | 1 — handler body, nil captured at construction | **`Permanent`** | ✅ (Task 9.7 edit site) |
| `routing/helpers.go:23` | `ErrNilFunc` | 1 | **`Permanent`** | ✅ (edit site) |
| `transform/transformer.go:38` | `ErrNilFunc` | 1 | **`Permanent`** | ✅ (edit site) |
| `routing/router.go:48` | `ErrNilFunc` | 1 — `r.pick` set once in `NewRouter` | **`Permanent`** | ✅ (edit site) |
| `handler.go:55` | `ErrNilSink` | 1 — `sink` captured by `To`'s closure | **`Permanent`** | ✅ (edit site) |
| `routing/router.go:56` | `ErrNoRoute` | 2 — per-message, caller-supplied `pick` | bare/transient | ✅ |
| `channel/direct.go:87` | `ErrNoSubscriber` | 2 — a later `Subscribe` resolves it | bare/transient | ✅ |
| `adapter/memory/queuestore.go:146` | `ErrOverflowDropped` | 2 — a drain frees capacity | bare/transient | ✅ |
| `adapter/memory/queuestore.go:151` | `ErrOverflowDropped` | 2 — same | bare/transient | ✅ |
| `retry.go:48` | `ErrInvalidMaxAttempts` | 3 — `Validate`, reached only from `NewConsumer` | bare | ✅ |
| `retry.go:51` | `ErrNoDeadLetter` | 3 — same | bare | ✅ |
| `endpoint/producer.go:589` | `ErrScheduledSendUnsupported` | **4** — handed to the caller from `SendAfter` | bare | ✅ |

**Twelve for twelve, and no arm is vacuous.** Arm 2 is what the counter-examples above exercise; arm 4 exists
only because the check found `:589` unclassified.

Two round-7 findings forced the sentinel-agnostic *shape* of this wording (the round-8 correction above fixes
its *content*). **D-B7/X-B3:** an earlier Spec §2.1 row read *"**Every** producer of `ErrNilFunc` returns
`Permanent`"* — a universal quantifier over a sentinel name, which flatly contradicts the deliberate exclusion
of `routing/aggregator.go:251` two paragraphs below. **D-B1:** the invariant's previous form was also phrased
over `ErrNilFunc` alone, and the derivation command under it greped `msgin\.ErrNilFunc` — scoped to **one
name** *and* to the **qualified** form — so it was structurally blind to a producer inside package `msgin`.
That is exactly how `handler.go:55`'s bare `ErrNilSink` survived four audit rounds. *(Counter-rule 9: a
derivation command scoped to one name or one qualified form is not a class sweep.)*

**Scope — the flow-path producers that become `Permanent(…)`** (verified at the `dadc775` code pin, re-derived
at `fe86a12`):

| Site | Declaration | Sentinel |
|---|---|---|
| `endpoint/helpers.go:21` | `nilFuncStep` | `ErrNilFunc` |
| `routing/helpers.go:23` | `nilFuncStep` (package-local copy) | `ErrNilFunc` |
| `transform/transformer.go:38` | `nilFuncStep` (package-local copy) | `ErrNilFunc` |
| `routing/router.go:48` | `Router.Handle`, `r.pick == nil` | `ErrNilFunc` |
| **`handler.go:55`** | **`To`'s returned handler, `sink == nil`** | **`ErrNilSink`** |
| Plan 027 Task 9's `Predicate.And` / `Or` / `Not` | new in this window — see below | `ErrNilFunc` |

**`routing/aggregator.go:251` is deliberately EXCLUDED** (`NewAggregator`,
`if fn == nil { return nil, msgin.ErrNilFunc }`). It is **construction-time** — returned to the caller from a
constructor, never carried through `Handle` — so it never reaches a `RetryPolicy` and a retry classification
would be meaningless on it. It is not an omission; it is the constructor arm of the invariant above.

**The re-derivation command — derived from `errors.go`, not from a name typed into the grep.** The alternation
is generated, so a sentinel added later is swept automatically; the `(msgin\.)?` prefix is optional, so a
producer *inside* package `msgin` cannot hide; and the trailing class admits the `}` / `)` a one-line
`HandlerFunc` body ends with. Run at `fe86a12`, workspace root:

```
$ sentinels=$(grep -oE '^\s*Err[A-Za-z]+ =' errors.go | tr -d ' \t=' | paste -sd'|' -)   # 43 sentinels
$ grep -rnE "return (msgin\.)?($sentinels)[ })]*(//.*)?$" --include='*.go' . | sed 's,^\./,,' \
    | grep -v '_test\.go' | grep -vE '^[^:]+:[0-9]+:[[:space:]]*//' | grep -v 'Permanent(' | sort
adapter/memory/queuestore.go:146:		return msgin.ErrOverflowDropped // nothing evictable (all in-flight) → drop
adapter/memory/queuestore.go:151:	return msgin.ErrOverflowDropped // OverflowReject
channel/direct.go:87:		return msgin.ErrNoSubscriber
endpoint/helpers.go:21:		return msgin.HandlerFunc(func(context.Context, msgin.Message[any]) error { return msgin.ErrNilFunc })
endpoint/producer.go:589:		return msgin.ErrScheduledSendUnsupported
handler.go:55:				return ErrNilSink
retry.go:48:		return ErrInvalidMaxAttempts
retry.go:51:		return ErrNoDeadLetter
routing/helpers.go:23:		return msgin.HandlerFunc(func(context.Context, msgin.Message[any]) error { return msgin.ErrNilFunc })
routing/router.go:48:		return msgin.ErrNilFunc
routing/router.go:56:			return msgin.ErrNoRoute
transform/transformer.go:38:		return msgin.HandlerFunc(func(context.Context, msgin.Message[any]) error { return msgin.ErrNilFunc })
```

> **Both `| sort` and `| sed 's,^\./,,'` are load-bearing, not tidiness** (Plan 027 Global Constraint 9).
> - **Order:** `grep -r`'s directory traversal order is **not stable between runs** — three consecutive runs of
>   this command on the *unchanged* tree at `fe86a12` emitted the same twelve lines in two different orders
>   (`routing/helpers.go:23` moved across the two `routing/router.go` lines). Any gate that diffs this block
>   against a re-run would fail on that alone. With `sort`, three consecutive runs are byte-identical.
> - **Path prefix (round 8):** system `grep -rn … .` prefixes every path with `./`; the **ugrep** wrapper on at
>   least one machine used for this bundle strips it, and the twelve lines above are pasted in the **stripped**
>   form. Without the `sed`, a diff against a re-run on a normal shell fails on **all twelve** lines for a
>   reason unrelated to the code. Re-verified 2026-07-30 with `/usr/bin/grep`: the sed-pinned form reproduces
>   this block byte-for-byte. Plan 027 Task 9.7 publishes the identical command and was corrected in the same
>   pass, so the two copies stay diffable against each other.

**Twelve lines. Five are the flow-path producers above; seven are triaged and stay bare:**

| Line | Why it stays bare |
|---|---|
| `retry.go:48`, `:51` | `RetryPolicy.Validate` — construction-time validation, returned to `NewConsumer`, never to a consumer's settlement path |
| `adapter/memory/queuestore.go:146`, `:151` | **MUTABLE cause — arm 2.** The store is at capacity *right now*; a drain frees a slot and the very next redelivery succeeds. Nothing about it is fixed at construction |
| `channel/direct.go:87` | **MUTABLE cause — arm 2.** `Send` with no subscriber: a later `Subscribe` resolves it, and a startup-order race is the commonest way to hit it. Retrying is the correct response |
| `endpoint/producer.go:589` | **Arm 4** — `SendAfter` (and `SendAt`, its sugar) on an adapter with no scheduled-send capability; handed to the caller, never through a `RetryPolicy` |

> **ROUND-8 CORRECTION (design B7).** The first two cells read *"Returned to the **sender** … **No
> `MessageHandler` body**, no `RetryPolicy`"* and *"sender-side, same reason"*. **The stated reason is false**
> — `Chain(To(ch))` puts both sentinels inside a `MessageHandler` body msgin constructs, compile-proven in the
> block above ( `ErrNoSubscriber` and `ErrOverflowDropped`, `IsPermanent=false` for both). Their
> **conclusions** are unchanged and correct; only the rationale moves, from a structural claim that does not
> hold to the mutability test that does. *(Third cell: `:589` sits in `SendAfter`, not `SendAt` — verified,
> `awk` on the enclosing `func` at `7ee3fd6` → `586: func (p *producer[T]) SendAfter(…)`. `SendAt` is
> one-line sugar over it at `:602`, so the fault surfaces through both; the site is `SendAfter`.)*
| `routing/router.go:56` | **`ErrNoRoute` — the load-bearing exclusion.** Evaluated per message against caller-supplied `pick`; see the classification table above |

**The narrowing to single-result returns is a shortlist, not the class.** The complete sweep — every `return`
of a root sentinel in non-test, non-comment source, without a `Permanent` wrap — is the same command without
the trailing `[ })]*(//.*)?$` anchor; it reports **63 lines** at `fe86a12`, the extra 51 being two-result
constructor returns of the `return nil, msgin.ErrX` shape. *(Derived summary; the command is the one above
with `return .*\b(msgin\.)?($sentinels)\b` as the pattern.)* Constructor arity is a strong proxy for the
invariant's **third** arm but it is **not** the invariant — when a new sentinel lands, triage the 63, not the
12, and triage them against the four arms above, not against arity. *(Round-8 B7: this said "second arm"
against the two-arm form; the constructor arm is now the third of four.)*

The two sites already compliant, for completeness:

```
$ grep -rnE "return .*Permanent\(.*\b(msgin\.)?($sentinels)\b" --include='*.go' . | grep -v '_test\.go'
routing/aggregator.go:160:	return "", msgin.Permanent(msgin.ErrNoCorrelation)
routing/aggregator.go:308:		return err // e.g. Permanent(ErrNoCorrelation)
```

**Task 9's combinators are amended by this decision.** They were decided to degrade to
`(false, msgin.ErrNilFunc)` **at evaluation, per message** — three *new* producers of a bare, deterministic
`ErrNilFunc` on the retry hot path, authored after D-K had already identified the class. They now return
**`(false, msgin.Permanent(msgin.ErrNilFunc))`**, and Task 9's hot-path branch list gains a case asserting
`msgin.IsPermanent(err)` on a combinator's nil result.

**Debuggability.** `errors.Is` is preserved by the wrap, but the bare sentinel collapses **every** nil
position — receiver and argument, across `And`, `Or` and `Not`, and across all five shipped producers — into
the single string `msgin: nil endpoint function`, with no indication of which link of `p.And(q).Or(r)` failed.
CLAUDE.md requires *"typed, wrapping errors that name the offending field/input"*, so each producer wraps with
positional context.

**The wrap order is DECIDED as cause-first, and the produced string is published here because five tests will
be written against it** (round-7 D-M1/X-M9). Both orders were measured at `fe86a12`:

```
cause-first (as decided) "msgin: permanent: msgin: nil endpoint function: routing.Predicate.And: nil argument"
                         errors.Is=true IsPermanent=true
context-first            "routing.Predicate.And: nil argument: msgin: permanent: msgin: nil endpoint function"
                         errors.Is=true IsPermanent=true
```

```go
fmt.Errorf("%w: routing.Predicate.And: nil argument", msgin.Permanent(msgin.ErrNilFunc))
```

**Both forms are `errors.Is`- and `IsPermanent`-clean, so the choice is presentational, and both carry the
doubled `msgin:` prefix** — it comes from `permanentError.Error()` (`reliability.go:13`,
`"msgin: permanent: " + e.err.Error()`) concatenating onto a sentinel whose own text is already
`msgin:`-prefixed. That duplication is a property of `Permanent` itself, present on every existing
`Permanent(msgin.ErrX)` in the tree, and is **not** repaired here: changing `permanentError`'s format is a
separate decision touching every permanent error msgin has ever produced. Recorded, not fixed.

Cause-first is retained over the arguably better-reading context-first for one reason: it matches the wrap
order of every existing positional wrap in the tree (`payload.go:15`, `endpoint/consumer.go:869`,
`endpoint/producer.go:467`), so a reader who has learned to read one msgin error reads them all the same way.
Each producer's godoc states that `errors.Is(err, msgin.ErrNilFunc)` still matches and that
`msgin.IsPermanent(err)` is true.

**This is a class, not a case.** The instances above are the complete in-tree list *at this window*, and the
list is not the contract — **the discriminator is**. Any endpoint that returns a typed error whose inputs are
fixed at construction, or determined by the message itself, must classify it `Permanent` at the producer,
because `IsPermanent`'s enumeration is closed and root cannot be amended for every subpackage and provider
fault (that is exactly the door D-I closes for `expr`). Recording only `ErrExprResultType` was the original
defect: it fixed the named instance while the same defect stayed live in every shipped producer the scope
table lists and was about to be re-authored in three new ones. *(Round-4 design audit BLOCKER 3 opened this;
round-6 design B4 and M4
generalized and corrected it. Round-7 D-B1 added `ErrNilSink` to the class and D-B7 replaced the universal
quantifier with the invariant.)*

---

**Decision D-N (round 7): `divert` falls back to the DeadLetter sink before discarding. Amends
[ADR 0007 D7](0007-reliability-settlement-api.md#d7--no-invalid-sink-policy-tasks-45).**

**D-M as decided introduces an unacknowledged data-loss path**, and it is the *default* configuration rather
than a corner case. Measured at `fe86a12`, one message, `RetryPolicy{MaxAttempts: 3, DeadLetter: dlq}`, no
`WithInvalidMessageSink`:

```
BEFORE D-M       bare ErrNilFunc         OnRetry=2 OnDeadLetter=1 OnInvalidMessage=0 | dlqSink=1 invalidSink=0 discarded=false
AFTER  D-M       Permanent(ErrNilFunc)   OnRetry=0 OnDeadLetter=0 OnInvalidMessage=1 | dlqSink=0 invalidSink=0 discarded=true
AFTER  D-M + D-N Permanent(ErrNilFunc)   OnRetry=0 OnDeadLetter=0 OnInvalidMessage=1 | dlqSink=1 invalidSink=0 discarded=false
```

*(The third row was produced by pointing the invalid-message sink at the same sink instance the DeadLetter
names — that is exactly the destination the fallback selects, so the row is the fallback's observable, measured
without editing shipped code.)*

Without D-N, **a message that this library previously captured durably in the dead-letter sink is now
dropped**, with a WARN log and `OnInvalidMessage` as the only trace. D-M's framing — *"diverted to the
invalid-message channel instead of … landing in the dead-letter sink"* — reads as pure improvement and never
states the loss. CLAUDE.md is explicit: *"When a wrong default could silently corrupt (… lose data …), pick the
value that fails safe."* And because a finite `MaxAttempts` **requires** a `DeadLetter` (`retry.go:50-52`,
transcript above) while `WithInvalidMessageSink` is optional and **unset by default**, "DeadLetter present,
invalid sink absent" is the shape of *every* finite-retry consumer that has not opted in.

**Decision.** On the invalid-message divert path, when `invalidSink == nil` **and**
`c.policy.DeadLetter != nil`, route to the DeadLetter sink rather than discarding. Discard remains the terminal
behavior only when **neither** sink is configured, exactly as ADR 0007 D7 specifies for that case.

**Three points the round-7 statement left open, settled here** *(flagged as owner-2 specification, not as
prior decision — the plan cannot be executed without them)*:

1. **Which hook fires: `OnInvalidMessage`, not `OnDeadLetter`.** The hook reports the **classification**; the
   sink is merely the **destination**. Firing `OnDeadLetter` would assert that the message exhausted its retry
   budget, which under D-M it explicitly did not — that is the whole point of the reclassification. A caller
   watching `OnInvalidMessage` sees every invalid message regardless of how their sinks are wired.
2. **Both invalid-path call sites get the fallback** — `endpoint/consumer.go:688` (decode failure) and `:716`
   (permanent handler error). Scoping it to the permanent arm alone would leave two invalid-message paths with
   different fallback behavior, which is incoherent. The decode arm's change is discard → DeadLetter, a strict
   improvement over D7's discard, and it is **a behavior change in its own right** — recorded here rather than
   inherited silently.
3. **The fallback is announced, not silent — and the announcement is DEDUPLICATED.** The existing loud WARN at
   `endpoint/consumer.go:766` is kept in the neither-sink case and joined by a WARN on the fallback case naming
   both facts — that no invalid-message sink is configured and that the message went to the dead-letter sink
   instead. A caller must not discover by inspection that their DLQ is receiving invalid messages.
   *Round-8 correction:* that condition is **constant for the consumer's lifetime**, so an undeduplicated WARN
   emits one identical line per invalid message and a poison storm floods the log. The in-tree precedent is
   `governorPanic` (`endpoint/consumer.go:573-585`), which deduplicates via a `sync.Map` `LoadOrStore` for
   exactly this reason and says so in the message itself. The per-message WARN at `:766` is **not**
   deduplicated: it is a terminal discard, and its message id is the only record that the message existed.

**Consequences.** On the **consumer** path, no configuration that previously captured a message starts
dropping it. This joins the register Spec 014 §2.1 keeps, as **row 7**, and lands in **Plan 027 Task 9.7**, in
the same commit as the reclassification — splitting them would leave a tree between commits with the loss path
live. It needs its own covering case **and** a case for the neither-sink-configured discard that remains, so a
later sweep cannot delete the discard arm as dead code.

> **That sentence is scoped to the CONSUMER, and the scope is load-bearing** (round-8 A5). D-M's blast radius
> was measured on the consumer only; on the **producer** path the same reclassification *does* remove a durable
> capture, because `endpoint/producer.go:453-455` returns on `IsPermanent` **before** `p.deadLetter(...)`. See
> the producer-side consequence recorded under **D-M** above and Spec 014 §2.1 **row 8**. Read unqualified,
> *"no configuration that previously captured a message starts dropping it"* is false.

**Two countervailing costs, ACCEPTED and stated — not absent** (round-8 A2). The Consequences above weigh only
the data-loss axis. The trade is still worth taking, but silence about its costs is the same defect round 7
ruled on for D-K, so both are recorded with their disposition:

1. **`ErrPayloadTooLarge` becomes dead-letter amplification.** `WithMaxPayloadBytes` exists to stop the runtime
   *decoding* untrusted oversize bytes (`endpoint/flowcontrol.go:95-108`, ADR 0009 D5). `divert` sends
   **`d.Msg`** — the message with those bytes — so after D-N msgin **persists into the operator's durable
   dead-letter store exactly the bytes the cap declared illegitimate**, where before it logged and dropped
   them. CLAUDE.md names this shape verbatim: *"a wrong default could … be a DoS lever"*.
   **Disposition — accepted, with a named remedy.** The cap's purpose is bounding *decode-time memory*, not
   bounding storage, and the alternative (silently dropping the payload D-N exists to preserve) re-opens the
   loss path. An operator who must not durably store oversize input has two in-tree levers today:
   `endpoint.WithInvalidMessageSink` pointed at a bounded or truncating sink, which takes the dead-letter sink
   out of the path entirely, or leaving `WithMaxPayloadBytes` off and rejecting oversize input at the adapter.
   Both are stated on the option's godoc by Task 9.7's sweep.
2. **Poison-storm volume.** A mis-configured codec, or a mis-wired step under D-M, turns **100% of the stream**
   into durable dead-letter writes where it was previously N log lines. The volume is bounded only by the
   source's throughput.
   **Disposition — accepted.** The pre-D-N behavior for the same storm was *silent loss at the same volume*,
   which is strictly worse operationally, and the fallback is announced by its own WARN so the condition is
   visible on the first message rather than discovered in a storage bill. **D-P** additionally bounds what the
   storm costs when the sink itself gives way: the fallback is single-shot, so a failing dead-letter sink
   degrades to a logged discard instead of an unbounded redelivery loop.

**Operational limitation — the dead-letter sink now carries two classes with no discriminator that survives
the process** (round-8 A3). After D-N a single sink receives both *"retries exhausted, may be replayable"* and
*"permanently invalid, replaying is pointless"*. The only discriminator is **which in-process hook fired**
(`OnDeadLetter` vs `OnInvalidMessage`), and msgin stamps **no settlement-reason header** — the reserved header
namespace declares seven keys and none of them is one:

```
$ grep -n 'Header[A-Za-z]* *=' message.go
15:	HeaderMessageID     = "msgin.message-id"
16:	HeaderTimestamp     = "msgin.timestamp"
17:	HeaderContentType   = "msgin.content-type"
18:	HeaderCorrelationID = "msgin.correlation-id"
19:	HeaderDeliveryCount = "msgin.delivery-count"
23:	HeaderSequenceNumber = "msgin.sequence-number"
24:	HeaderSequenceSize   = "msgin.sequence-size"
```

**CLAUDE.md's multi-instance rule applies directly:** the classification lives in **one process's memory** (a
hook closure) while the artifact lives in a **store another process reads**. A replay tool written against the
pre-D-N assumption — *"everything in the DLQ was retried to exhaustion, so re-submitting it is reasonable"* —
will re-submit poison forever.

**Disposition — the limitation is DISCLOSED, and stamping a header is deliberately NOT taken in this window.**
Stamping a `HeaderSettlementReason` would add a new exported root symbol **and** make `divert` rewrite the
diverted message's headers — a second behavior change, on the message contract, in a restructure window whose
scope §2.1 fixes; it needs its own decision, its own audit and its own covering cases, and it belongs with the
Resequencer/settlement increment that will have a real consumer to pin its vocabulary. Until then:

- **What operators get today:** an operator who needs the distinction **must configure
  `endpoint.WithInvalidMessageSink`** — with an invalid sink present the two classes go to two different
  destinations and the discriminator is the destination itself. This is the documented remedy and it must
  appear on `WithInvalidMessageSink`'s godoc (Task 9.7).
- **What is recorded for the next increment:** a durable settlement-reason marker is the missing piece; the SPI
  seam that would carry it is the message headers, not a `Delivery` field, because it must survive the store.

---

**Decision D-P (round 8): the invalid-path fallback is SINGLE-SHOT. Amends D-N; re-amends
[ADR 0007 D7](0007-reliability-settlement-api.md#d7--no-invalid-sink-policy-tasks-45).**

D-N settled *where* an invalid message goes when no invalid sink is configured. It never settled what happens
when that destination is **down** — and `divert`'s send-failure arm (ADR 0007 D8, `endpoint/consumer.go:774-782`)
`Nack`s with `requeue=true`. Implemented exactly as D-N specified and run in the **default** configuration
against a dead-letter sink whose `Send` returns an error (round 8, compile-proven):

```
BEFORE D-N: deliveries=1   acks=1  nacks=0   dlqSends=0   OnInvalid=1  OnDeadLetter=0  OnRetry=0
AFTER  D-N: deliveries=41  acks=0  nacks=40  dlqSends=40  OnInvalid=0  OnDeadLetter=0  OnRetry=40
```

(41 was the harness's redelivery cap, reached in under 10 ms. The loop is **unbounded** in reality.)

A **permanent** message re-enters the flow forever, and **every bound this library owns is structurally blind
to it**:

- **MaxAttempts** — never consulted: the permanent arm deliberately bypasses `c.attempts(d)` (M8) and passes
  `attempt = 1`.
- **Backoff** — `retryDelay(policy, 1)` is `p.Backoff.Delay(0)` (`endpoint/consumer.go:948-953`), the *first*
  step, on every iteration. It never escalates.
- **Circuit breaker** — `endpoint/consumer.go:614` records `err == nil || msgin.IsPermanent(err)` as
  **healthy**, so the breaker is told the flow is fine while the message spins.

**ADR 0007 D7 is the authority D-N violated**, in D7's own words: *"retrying anyway would only convert a
configuration gap (no sink configured) into an infinite-retry trap, which is **worse** than a logged,
observable discard."* D-N's amendment note argued only the discard-vs-durable-sink axis and never re-weighed
the axis D7 had actually decided.

**Decision.** The fallback is **single-shot**. If the fallback target's `Send` fails, **fall through to D7's
discard**: WARN naming **both** the classification cause *and* the sink error, fire `OnInvalidMessage`, `Ack`.
Do **not** `Nack`. This keeps D-N's gain (a reachable dead-letter sink captures the message) without
surrendering D7's guarantee (**an invalid message always terminates**). One extra branch, one extra covering
case.

> **Amended by the Task 9.7 code review — one exception: shutdown cancellation.** A `Send` that fails only
> because the consumer's **settle context was already cancelled** by the shutdown deadline (D9/C1) proved
> nothing about the sink — a *healthy* sink returns an error there too — so it is `Nack`ed with requeue, fires
> no terminal hook, and keeps its tracker entry. Without it the discard arm `Ack`s a message the sink may
> never have seen, and since a `Delivery.Ack` may ignore its context (`adapter/memory`'s does), that `Ack`
> succeeds and the message is **lost with the sink healthy**. In normal operation `ctx.Err()` is `nil`, so the
> single shot above is unchanged; the arm is reachable only inside a shutdown D9 bounds, so it cannot loop.
> Normative statement: [ADR 0007 D7](0007-reliability-settlement-api.md#d7--no-invalid-sink-policy-tasks-45)'s
> shutdown-exception note.

**Scope — the INVALID path only.** A `divert` whose sink is `c.policy.DeadLetter` at the dead-letter call site
(`endpoint/consumer.go:726`) keeps D8's `Nack`-with-backoff on send failure: that message is *transient* by
classification, so requeueing it is a retry, not a loop. Single-shot applies where the message is permanent by
construction.

**Rejected alternative.** Bounding the loop by threading a real attempt count into the invalid-path `divert`:
more code, more state, and it still leaves a permanent fault consuming retry budget for no benefit.

**The class violated — recorded as a class, not as an instance:** *a settlement path that is terminal by
construction must not become non-terminal without a bound.* Pre-D-N the no-invalid-sink path had exactly one
outcome (Ack); D-N gave it a retry loop no counter, backoff or breaker could see. **Any future amendment that
introduces a `Send` onto a terminal path must state what happens when that `Send` fails** — the omission, not
the fallback, was the defect.

**D-N item (b) — the decode-arm widening — is CONFIRMED and kept as specified**, conditional on D-P and on the
consequence records above. Scoping it back is not cleanly implementable: `endpoint/consumer.go:716` receives
*any* `IsPermanent` error, so restricting the fallback to nil-endpoint faults would require an `errors.Is`
against a **closed list** of endpoint sentinels — exactly the closed-enumeration anti-pattern D-M rejects — and
`WithInvalidMessageSink`'s own godoc already unifies the two arms (*"where permanent/undecodable messages are
diverted"*), so honoring it for one arm and not the other makes that option's godoc unwritable. **But (b) is a
NEW policy, not a restoration:** D-N's justification (*"D-M moved messages off a path that used to reach the
dead-letter sink"*) is true only of `:716`. The decode arm was **never** on that path — it discarded before
D-M and it discards today. It must be justified on its own merits (a durable capture beats a discard), never
by D-M's premise.

**Consequences of D-P.** One added branch in the invalid-path settlement, one added WARN carrying two facts,
and one added covering case — *"fallback target configured, its `Send` fails"* — which is the **newly reachable
state** and which D-N's own case list did not contain. It joins Spec 014 §2.1 **row 7** (amended, not a new
row: D-P completes D-N rather than changing a different observable) and lands in **Plan 027 Task 9.7**, in the
same commit as D-N. `OnRetry` must **not** fire on this arm — no retry follows it.

#### 5.0c A second expression provider would mint a third `ErrInvalidExpression` — recorded, not solved

> **NARROWED by revised D-K (round 6).** This section was written when the `expr` module declared **two**
> sentinels, and it applied to both. Under revised D-K the evaluation-time fault is expressed by wrapping
> root's `msgin.ErrPayloadType`, which **is** the shared `errors.Is` target this section wanted — so the
> problem below, and both escapes offered for it, are **moot for that half** and are retained **only for
> `ErrInvalidExpression`**.

A future CEL or starlark provider mints `cel.ErrInvalidExpression` / `starlark.ErrInvalidExpression` for the
same conceptual fault, so a caller with generic handling ("the user's expression was bad → HTTP 400") must
enumerate every provider, with no shared `errors.Is` target — exactly what root's sentinel gave for free.
**The evaluation-time counterpart no longer has this shape**: every provider's result-type mismatch is
`msgin.ErrPayloadType`, one target, forever.

**That is a narrowing, not a cost of zero.** Revised D-K trades *"one sentinel per provider, no shared
target"* for *"one shared target that no longer distinguishes two faults with disjoint remedies"* — the
opposite over-collapse. **§5.0b records that trade-off in full**; do not read this section's *"moot for that
half"* as *"free for that half"*. Reading it that way is exactly what left §5.0b listing only benefits until
round 7 (D-B8).

**This does not reverse D-I.** Nothing is released, the fault genuinely is the provider's, and the repo
precedent (51 adapter-minted sentinels alongside 27 root-sentinel returns) supports it. But it is a
foreseeable consequence with two known escapes, and it is recorded here so a future increment does not
rediscover it as a surprise: every provider can wrap with `msgin.Permanent` (giving a shared *classification*
even without a shared sentinel), or root can later add an `ErrInvalidConfig`-class sentinel that providers
wrap. **Read both escapes as scoped to `ErrInvalidExpression` alone.** The second escape is, in effect, what
revised D-K already did for the other sentinel — wrap an existing root target instead of minting a new one —
and it is worth noting that the general fix and the specific one are the same move.
*(Round-4 design audit MINOR 6; narrowed by round-6 design M4.)*

#### 5.0d A broken `MessageGroupStore` is caller input, not an internal state — decision D-Q (2026-08-11)

> **STATUS: IMPLEMENTED (Plan 027 Task 10, fix round 2).** Registered as Spec 014 §2.1 row 9.

**Context.** `msgin.MessageGroupStore` is public adapter SPI: consumers inject their own, and two ship in
this repo. Its `Add` contract is *"returns the resulting group snapshot"* — either a usable
`MessageGroup` **or** a non-nil error. A third-party store that returns **both nil** breaks it.

`routing.Aggregator.Handle` passed that value straight into the configured release strategy. **All four
strategies dereferenced it** — the default, `WithCompletionSize`, `WithReleaseWhen`, and a caller's own
`WithReleaseStrategy` — so the flow died on a nil-pointer panic. Measured on the pre-fix tree with a store
whose `Add` returns `(nil, nil)`: four of four panicked.

**Decision.** Reject it **once, at the choke point** in `Handle` immediately after `store.Add`, with a new
root sentinel `ErrNilMessageGroup` wrapped in `Permanent`.

1. **At the choke point, not in a strategy.** `a.cfg.release` is one of four values and only the call site
   precedes all four; a guard inside any one leaves the other three panicking. An earlier revision of this
   task guarded `defaultRelease` locally and closed **one** of the four — the same fix-the-instance failure
   this branch has now hit four times.
2. **A typed error, not a "hold".** Returning `nil` reads as *"group held"*, which makes `Handle` succeed and
   the source **Ack** — for a message the store just said it cannot read back, so it may be durable nowhere.
   An error routes it to a visible sink instead. This is CLAUDE.md's fail-safe-default rule.
3. **`Permanent`, by wrapping at the producing site.** A store that returns nil snapshots is fixed at
   construction and cannot change for the message's lifetime, so redelivery cannot fix it and retrying only
   burns `MaxAttempts` — **D-M's invariant** (§5.0b) applied to an SPI-violation fault. The wrap is used
   rather than adding the sentinel to `IsPermanent`'s enumeration: that enumeration is documented as a closed
   set of three (Spec 014 §4.1), and **D-K's argument in §5.0b depends on its stability** — D-K reuses
   `ErrPayloadType` precisely *because* it is already inside `IsPermanent`. Widening the set to solve an
   unrelated problem would weaken that. The wrap gives the identical classification with no such coupling.
4. **Nil interface only.** `group == nil` catches a nil *interface*; a store returning a typed nil
   (`(*myGroup)(nil)`) still panics inside the strategy. Documented on the sentinel rather than solved —
   Go cannot express the distinction without reflection on every message, on the Aggregator's hot path. No
   shipped store is affected. This mirrors the caveat `expr.RouteFunc` already carries for a typed-nil
   channel.

**Precedent.** `ErrNilSubscription` is the same shape one level up: a `SubscribableChannel` returning a
nil-nil pair from `Subscribe`. Its godoc's framing — *"public SPI that third-party adapters implement, so a
faulty implementation is caller input"* — is reused verbatim in structure.

**Consequences.** One new root sentinel: root goes to **43** sentinels / **103** exported, `apidiff`
additions **8 → 9**, removals unchanged at 97 (Spec 014 §4, §4.1). **Not breaking** — a symbol is added, none
removed or retyped, so no major-version implication. A panic becomes a typed, diverted error, which is why it
is registered in Spec 014 §2.1 rather than treated as a pure bugfix.

**Alternative considered — a standalone ADR.** Rejected. This decision is a member of §5.0b's
retry-classification family (it *is* D-M's invariant, applied to a new fault class) and depends on D-K's
reasoning about `IsPermanent`'s closed set; splitting it into its own ADR would separate three decisions that
must be read together, and ADR 0029 already owns the `expr` work that surfaced it.

### 5a. The provider shape is NOT uniform, and it is NOT non-generic

Two separate errors, corrected in two rounds.

**Not uniform** (round-2 §D2). An earlier draft asserted a uniform `(string) → (T, error)` shape for all six.
**`RouterExpr` never had that shape**: it also took `routes map[string]MessageChannel`, and it carried **two**
construction validations of its own.

**Not non-generic** (round 3, compile-proven by an auditor). The corrected draft then wrote
`Correlation(s string)`, `Release(s string)` and `RouteFunc(s string, routes …)` **without a type parameter**,
and that does not compile. Every deleted original was `[A any]`:

```
$ git show ab233d9:expr.go | grep -nE '^func '
 89:func FilterExpr[A any](expression string, opts ...FilterOption) (Step, error)
115:func RouterExpr[A any](keyExpr string, routes map[string]MessageChannel, opts ...RouterOption) (*Router, error)
167:func TransformExpr[A, B any](expression string) (Step, error)
217:func SplitExpr[A, B any](expression string) (Step, error)
321:func WithCorrelationExpr[A any](expression string) AggregatorOption
390:func WithReleaseExpr[A any](expression string) AggregatorOption
```

`A` is load-bearing twice, and both uses are things this ADR elsewhere insists on:

1. **`compile[A]` (`expr.go:35`) type-checks `payload.Field` against `A`.** That is what makes §5's
   preservation of ADR 0019's *fail-at-construction* contract real rather than nominal.
2. **`PayloadOf[A]` (`expr.go:129,224,284,331`) IS the M-6 `ErrPayloadType` branch** §3b hands to Task 10. A
   non-generic provider has nothing to assert the member payload to, so that branch cannot exist and §5's
   parity bar is unmeetable — the same failure mode the bool-only `ReleaseStrategy` had.

The provider set, stated correctly:

```go
func Predicate[A any](s string)      (routing.Predicate[A], error)
func SplitFunc[A, B any](s string)   (routing.SplitFunc[A, B], error)
func Transformer[A, B any](s string) (transform.Transformer[A, B], error)
func Correlation[A any](s string)    (routing.CorrelationStrategy, error)
func Release[A any](s string)        (routing.ReleaseStrategy, error)
func RouteFunc[A any](s string, routes map[string]msgin.MessageChannel) (routing.RouteFunc, error)
```

**`A` is not inferable from a `string` argument**, so every call site instantiates explicitly —
`expr.Release[Order]("qty > 10")`. This is an ergonomic cost of the split, it is unavoidable given (1) and
(2), and each provider's godoc must state it. Plan 027 Task 10's branch list carries `RouteFunc`'s two extra
construction validations, and the two `toGroupEnv` guard cases (M-1, M-6) that genuinely belong in this module
rather than in the core.

### 6. The dependency rule that makes this coherent with ADR 0016

> **A zero-transitive dependency is pushed to its own module when its weight is material to consumers who do not
> use it.**

`expr-lang` (7.1 MB, zero transitive deps) is material — it leaves. `robfig/cron` (144 KB, zero transitive deps,
≈50× smaller) is not — it stays in the root module, and **ADR 0016 stands unchanged**. Both are dependency-free
leaves, so "purity" cannot distinguish them; without this stated rule the two decisions read as arbitrary and an
adversarial audit would rightly flag them.

## Consequences

**Positive.** The root module sheds a 7.1 MB forced dependency. Third parties can supply other providers (regex,
OPA, feature-flag) yielding the same types, so endpoints are open for extension with no new constructor. Six
parallel `*Expr` constructors collapse into one base constructor per endpoint. Predicate combinators arrive free.

**Negative, accepted.**

- **CLAUDE.md and `MESSAGING.md` must be edited in the same commit** — CLAUDE.md's dependency policy ratifies
  `expr-lang` as one of three accepted core exceptions **and names `FilterExpr`/`RouterExpr` at `CLAUDE.md:235`
  as its justification** (both deleted), its architecture blueprint named `StreamingSource`, and `MESSAGING.md`
  carries three more rename sites. Leaving any of them stale is a traceability breach.
- **Two error sentinels were orphaned in root — DECIDED, and now IMPLEMENTED.** `ErrInvalidExpression` and
  `ErrExprResultType` had **zero producers** after the `*Expr` deletion, and their godoc named constructors
  that no longer exist (F11.7). **Decision D-I (§5.0a): both leave root.** The `expr` module declares
  `expr.ErrInvalidExpression`; the evaluation-time fault is expressed by wrapping root's existing
  `msgin.ErrPayloadType` (**revised D-K**, §5.0b), so no second `expr` sentinel is minted. **Plan 027 Task 9.5
  deleted them** (root 102→100 exported, 43→41 sentinels, `apidiff` 95→97 removals — figures scoped to that
  task, not to the branch; §5.0d and Spec 014 §4 carry the running totals); **Task 10 declared the one
  replacement** — `expr.ErrInvalidExpression`, shipped.

  > *Corrected (round 6, C-B1 + C-B3).* This bullet survived the D-I pass unamended and was the worst kind of
  > stale: it presented a **settled** decision as open, framed both options as live, and assigned it to the
  > **wrong task** — an implementer reading only the Consequences would defer a Task 9.5 checkbox and re-open
  > D-I. It also carried a **third** distinct line-number pair for the same two declarations (`:161`/`:183`,
  > after `:156`-era and other variants); both cited lines hold unrelated godoc. Verified:
  > `grep -n 'ErrInvalidExpression\|ErrExprResultType' errors.go` → `180:	ErrInvalidExpression = errors.New("msgin: invalid expression")`
  > and `206:	ErrExprResultType = errors.New("msgin: expression result type mismatch")`. §5.0a and this ADR's
  > header record the settlement; **this section is where a reader looks for it and is the surface a decision
  > must be swept into** ([round 6 §0](../plans/027-audit-round-6.md), counter-rule 1).
- **The `*Expr` deletion leaves godoc mentions no compiler can see**, and Spec 014 §8.1's staleness sweep needs
  a **second arm** for deleted symbols: the moved-symbol arm greps `msgin.<sym>` and structurally cannot find a
  mention of a symbol that no longer exists. **Arm 2 returns exactly ONE survivor at the `dadc775` code pin**
  — `WithRelease`, at `routing/aggregator.go:316` (`// release-decision error (the WithRelease strategy
  failed)`; the option is `WithReleaseStrategy`). `errors.go` and `routing/splitter.go` contribute **zero**.
  Re-derived by running [Spec 014 §8.1](../specs/014-core-package-layout.md)'s arm-2 `comm -23` pipeline
  **unmodified** (it is 12 lines and lives there; this ADR deliberately does not fork a copy that can drift).
  Its complete stdout, observed at `aae6160`:

  ```
  WithRelease
  ```

  and the site: `grep -rn 'WithRelease strategy' routing/aggregator.go` →
  `routing/aggregator.go:316:		return err // release-decision error (the WithRelease strategy failed) → retry/DLQ;`

  > *Corrected (round 6, C-B2).* This bullet read *"Seven survive today in `errors.go`, `routing/splitter.go`,
  > and `routing/aggregator.go`"*. That figure is **fiction** — it came from the pre-round-4 arm 2, a hardcoded
  > list of the six `*Expr` names that returned **zero hits at every frame it was ever run against**, published
  > alongside a survivor list at named lines that hold unrelated text. Rounds 4 and 5 established this and wrote
  > `ROUND-4 CORRECTION` blocks into **Spec §8.1 and Plan §9.5.1**; the ADR that both cite was never opened.
  > Arm 2's reach is also narrower than "seven godoc mentions" implies: it matches only `With|Err|New` names, so
  > it covers 2 of the 6 `*Expr` names and misses `StreamingSource` entirely — see Spec §8.1's stated blind
  > spots.
- `apidiff` **will** report the parameter-type change on every typed constructor. Call sites are
  source-compatible (a bare closure stays assignable), so this is expected and benign — the plan records it as a
  *reviewed, source-compatible* entry rather than claiming zero output, so the "only intended changes" gate
  passes deliberately rather than by surprise.
- An eighth module to build, test, tag, and keep in `go.work`.
- **No `String()`/`Describe()` on a behavior.** Introspection must come from provider-side error wrapping;
  revisiting this after v1 is a major bump. This is the accepted cost of §3.

**Sequencing consequence.** All six `*Expr` constructors return `Step`, `*Router`, or `AggregatorOption` — types
that move to `routing`/`transform` under ADR 0027. `expr.go` therefore cannot remain in root once those move,
and splitting it across two new packages only to delete it afterwards is throwaway work. The `*Expr` deletion is
sequenced **first** in Plan 027, which leaves expression support absent from the branch until the `expr` module
lands. That is acceptable within a branch that restores it before merge, and is called out so it is not mistaken
for dropped scope.
