# Plan 027 — Adversarial design audit, ROUND 1 (2026-07-27)

> **STATUS: findings FOLDED IN (2026-07-27). The bundle is REVISED and now AWAITS ROUND 2.**
> Every finding below is dispositioned in **§K**, with the artifact and section that resolves it. The six §H
> decisions were settled with the user. **DO NOT IMPLEMENT until round 2 passes** — the revision rewrote the
> normative move-list and changed public signatures, which is exactly the kind of churn round 2 exists to catch.
>
> *Original round-1 verdict, retained:* three independent Opus auditors were handed the complete bundle
> (Spec 014 + ADR 0027 + ADR 0028 + ADR 0029 + Plan 027) on distinct lenses. **All three returned
> `NEEDS-REVISION`.**

| Lens | Verdict | Findings |
|---|---|---|
| Design & correctness of the target API | `NEEDS-REVISION` | 4 HIGH, 5 MED, 2 LOW |
| Plan-level execution | `NEEDS-REVISION` | 8 HIGH, 8 MED, 3 LOW |
| Cross-document consistency & completeness | `NEEDS-REVISION` | 6 HIGH, 10 MED, 5 LOW |

Findings are deduplicated and grouped by theme below. **Convergent** marks a finding reached independently by two
or three auditors — the highest-confidence class.

---

## A. The move-list is not executable (blocks Task 4 onward)

Spec 014 §3's file-level move-list is the normative artifact the whole plan is read against. It does not survive
contact with the compiler.

### A1 — `Subscription` creates a real import cycle · **HIGH · CONVERGENT (3/3)**

`pubsub.go:37` declares `type Subscription interface{ Cancel() }`. Spec 014 §3 moves `pubsub.go` wholesale to
`channel/`, while §4/§5 keep `Subscription` in root and have root's new `SubscribableChannel` return it. That is
**root → `channel`**, the one edge the design forbids by construction.

**Fix:** `pubsub.go` becomes a **split**, not a move. `Subscription` stays in root (relocate it in Task 2,
alongside `SubscribableChannel`, ideally into a root `subscription.go`); `PublishSubscribeChannel`,
`FanOutPolicy`, `PubSubOption`, `subscription` move to `channel`.

### A2 — `backoff.go` is a fourth file needing a split · **HIGH**

`retry.go` stays in root and references `BackoffStrategy` (`retry.go:43: Backoff BackoffStrategy`), declared in
`backoff.go:12`, which Task 7's table moves whole to `resilience`. Spec 014's prose says only that "the
`ExponentialBackoff` *implementation* moves" — but the plan's table says the file, and a subagent follows the
table.

**Fix:** `BackoffStrategy` stays in root; `ExponentialBackoff` + jitter → `resilience`. Spec 014 §3 must say
**four files split, not two**: `channel.go`, `flowcontrol.go`, `pubsub.go`, `backoff.go`.

### A3 — 18 unexported identifiers cross the new package boundaries · **HIGH · CONVERGENT (2/3)**

A file-level move-list is blind to them. Verified by AST parse of every non-test root file:

| Symbol | Declared (dest) | Used from (dest) |
|---|---|---|
| `boxMessage` | `payload.go` (root) | `transformer.go`[transform], `splitter.go`+`aggregator.go`[routing], `activator.go`+`gateway.go`[endpoint] |
| `nilFuncStep` | `transformer.go` (**transform**) | `filter.go`, `splitter.go`[routing], `activator.go`[endpoint] |
| `isPermanent`, `retryAfterOf`, `noNativeReliability` | `reliability.go` (root) | `consumer.go`, `producer.go`[endpoint] |
| `attemptTracker`, `newAttemptTracker` | `retry.go` (root) | `consumer.go`[endpoint] |
| `randomID` | `message.go` (root) | `gateway.go`[endpoint] |
| `RetryPolicy.delayFor` | `retry.go` (root) | `consumer.go`, `producer.go`[endpoint] |
| `breaker` | `breaker.go` (resilience) | `consumer.go`[endpoint], `flowcontrol.go` |
| `consumerConfig` | `consumer.go` (endpoint) | `flowcontrol.go` |
| `default{MaxInFlight,AttemptTTL,PollInterval,PollMaxBatch}`, `maxPollErrorBackoff` | `flowcontrol.go` | `consumer.go`, `poller.go` |
| `asInt`, `firstHeader`, `forwardSplit`, `aggregatorConfig` | `aggregator.go`, `splitter.go` | `expr.go` |

Two are structurally nasty:

- **`nilFuncStep` creates undeclared `routing → transform` and `endpoint → transform` edges.** ADR 0027 §2
  asserts `transform` is a leaf that only imports root. It is not.
- **`RetryPolicy.delayFor` is an unexported method on an exported root value type.** No `internal/` package can
  rescue a method. Options: export `RetryPolicy.DelayFor` (new public API absent from the contract), or move
  `RetryPolicy` to `endpoint` keeping only `BackoffStrategy` in root. This is an ADR-level decision.

**Fix:** add a **Task 3.5 — shared-helper resolution** before any extraction, resolving all 18 explicitly, and
add a **symbol-level** table to Spec 014 §3 alongside the file-level one. `internal/` is currently absent from
the normative layout despite CLAUDE.md mandating it for internals.

### A4 — ~12 root test files have no matching source file · **HIGH**

Tasks 4–8 say "move the matching `_test.go` files". Many have no single match:
`example_composition_test.go` spans **four** destinations (`Activate`/`Chain`/`Consume`/`Filter`/`Transform`);
`example_flowcontrol_test.go` spans `resilience` + `endpoint`; `settlement_doubles_test.go` is 300+ lines of
shared doubles used across consumer/producer/poller tests. Also unmatched:
`composition_integration_test.go`, `queuechannel_e2e_test.go`, `consumer_governor_panic_test.go`,
`consumer_probegate_wiring_test.go`, `pubsub_integration_test.go`, `groupstore_conformance_test.go`, and four
more `example_*_test.go`.

**Fix:** Task 0's ledger gains a **45-row test-file table** assigning each a destination, marking splits and
where the shared doubles land — before Task 4, not during it.

### A5 — `OverflowPolicy` and `ProbeGate` are unplaced; moving `OverflowPolicy` breaks an adapter API · **MED · CONVERGENT (2/3)**

`flowcontrol.go` declares four kinds of symbol, not two: `RateLimiter`, `CircuitBreaker`, **`ProbeGate`**
(mentioned nowhere in any of the five bundle documents), `OverflowPolicy` (a named int with a `String()`
method), and five unexported defaults. "Interface half / option half" does not classify them.

`adapter/memory/queuestore.go:74` has `func WithOverflow(p msgin.OverflowPolicy) QueueStoreOption` — real
exported adapter code. Moving `OverflowPolicy` changes that signature, and Task 0's expected-change list does
not include it.

### A6 — `handler.go`'s `Chain`/`To` falsify acceptance criterion §9.1 · **HIGH · CONVERGENT (3/3)**

Spec 014 §9.1 and Plan Task 12 make "no root file declares a constructor for a running component" *the*
mechanical check that C-full happened. `handler.go` stays in root and declares `Chain` (composes steps into a
live `MessageHandler`) and `To`. `codec.go` stays in root and declares two concrete codec implementations.
Neither `Chain`, `To`, `Step`, `HandlerFunc`, `New`, `NewMessage`, `Permanent`, nor `RetryAfter` appears in
§4's root-contract enumeration, so §4 is not a closed description of what §3 leaves behind.

**Fix:** replace §9.1 with a check that has teeth and is trivially scriptable — **`go list -deps .` contains no
`github.com/kartaladev/msgin/{endpoint,routing,transform,channel,resilience}`** — plus a named allow-list of the
root constructors deliberately kept. Or move `Chain`/`To`/`discardHandler` to `endpoint` and say so.

---

## B. Behavior changes smuggled into a "behavior-preserving" increment

The plan's own guard ("no assertion may change; a task that needs to has found a defect") will not catch these,
because no assertion changes.

### B1 — Widening the exchange reply channel voids its exclusivity invariant · **HIGH**

`ChannelExchange`'s dedicated-reply-channel guarantee is enforced **only** by `DirectChannel` returning
`ErrChannelSubscribed` (`channel.go:31-40`). `PublishSubscribeChannel.Subscribe` has no such guard
(`pubsub.go:103-111`). Accepting any `SubscribableChannel` for `reply` makes two exchanges over one pub-sub
reply channel a **valid program**: every reply fans out to both receivers, and the non-owner hands a full copy
of the other exchange's reply to its `WithUnmatchedReplySink` — typically a dead-letter or audit sink. Today
that wiring is a compile error.

Secondary: `ChannelExchange.Close`'s godoc says *"The reply receiver remains subscribed (channels have no
unsubscribe)"* — false once `Subscribe` returns a `Subscription`, which `NewChannelExchange` then discards. A
leak that did not previously exist, because there was nothing to leak.

**Fix:** store the `Subscription` and `Cancel()` it in `Close()`; add an explicit ADR 0028 decision on reply
exclusivity; add the two-exchanges-one-pubsub case to Task 2's branch list.

### B2 — `ReleaseStrategy` cannot express an eval error, making Task 10's parity bar impossible · **HIGH**

Spec 014 §6 and ADR 0029 §3 declare `type ReleaseStrategy func(g MessageGroup) bool`. Today's internal path is
`release func(MessageGroup) (bool, error)`, and `WithReleaseExpr`'s eval error is **load-bearing behavior**:
`TestAggregator_ReleaseExprReaperFallThrough` asserts it propagates from `Handle`;
`TestAggregator_ReleaseExprDrainCheckError` asserts the drain loop swallows it. An
`expr.Release(s) (routing.ReleaseStrategy, error)` returning a bare `bool` must swallow runtime errors
(silently returning `false` — a message stranded forever) or panic (forbidden on caller input).

**Fix:** `type ReleaseStrategy func(g MessageGroup) (bool, error)`, with the bool-only `WithReleaseStrategy` kept
as a wrapper. Decide before implementation; pre-record the `apidiff` entry.

### B3 — Deleting `*Expr` orphans two aggregator hot-path branches · **HIGH**

Measured, not argued. Running the root suite with only the four `*Expr`-driven aggregator tests and
`expr_test.go` excluded:

```
aggregator.go:223 NewAggregator  93.8%   (was 100%)
aggregator.go:286 Handle         94.7%   (was 100%)
```

The newly-uncovered blocks are exactly `if cfg.optErr != nil { return nil, cfg.optErr }` and the release-decision
error return. `WithReleaseStrategy` **cannot** produce either (`aggregator.go:70` wraps a bool-only func), so
after Task 1 no remaining public API can reach them — they become permanently untestable branches that Task 4
then carries into `routing`, where Task 12's per-package gate blocks on them. CLAUDE.md makes an untested
hot-path branch a delivery blocker.

**Fix:** resolved by B2 (a fallible `ReleaseStrategy` keeps the branches reachable). Otherwise Task 1 must
delete the error arm and retire `optErr`, and Task 10 re-derives it as an `expr`-module concern.

### B4 — Narrowed `MessageChannel` becomes byte-identical to `OutboundAdapter` · **HIGH · CONVERGENT (2/3)**

`spi.go:41` already declares `OutboundAdapter { Send(ctx, msg) error }`. The narrowed `MessageChannel` is the
same method set. ADR 0028 §3 declines to define `PollableChannel` precisely because "it would duplicate the
existing `PollingSource` SPI's exact method set" — and then creates this duplicate without noticing.

Consequence the bundle misses: this **voids ADR 0013's F2 rationale**, which reads *"`To` takes
`OutboundAdapter`, not `MessageChannel` … a `*memory.Broker` satisfies `OutboundAdapter` but not
`MessageChannel` (no `Subscribe`)."* After Task 2 that distinction does not exist, so every shipped
`OutboundAdapter` silently becomes a legal discard target, default route, and exchange request channel — a much
larger capability widening than §5's three-row table advertises, untested and undocumented.

**Fix:** an explicit ADR 0028 decision — collapse the two, or keep both with godoc stating they are deliberately
identical and why — plus a recorded amendment to ADR 0013.

### B5 — `DirectChannel`'s new `Subscription` has undefined semantics · **MED · CONVERGENT (2/3)**

Task 2 lists test cases for behavior the design never specifies: after `Cancel()`, does a second `Subscribe`
succeed or still return `ErrChannelSubscribed`? Does `Send` between them return `ErrNoSubscriber`? Does `Cancel`
race an in-flight `Send` the way `PublishSubscribeChannel`'s documented behavior does? One listed case
("`ChannelExchange` reply delivery after unsubscribe") is **unwritable** — `ChannelExchange` has no `Close` and
no field for the subscription. Missing cases: `Subscribe(nil)` now returns a `Subscription` too — what value?

---

## C. `SettleMembers` — both auditors that examined it recommend CUTTING Task 11

Plan 027 Task 11 already offers the exit ("If the audit rejects that argument, this task moves out of the plan
rather than being defended"). **Two independent auditors took it.** · **HIGH · CONVERGENT (2/3)**

1. **Underspecified on four points a real caller must pin:** lease state after partial settle; whether
   `createdAt` resets; empty-residual handling; ordering of un-named claimed members versus members added during
   the lease. **Both branches of the `createdAt` question are wrong for a Resequencer** — reset it and the gap
   timeout can never fire; don't and the reaper re-releases every tick.
2. **It needs a second, unenumerated breaking change to a different module's exported SPI.** `sql.GroupStore`
   delegates to `GroupDialect` (`postgres/groupdialect.go:179`, `mysql:177`, `sqlite:193`), whose godoc invites
   third-party implementers. Neither Spec 014 §8 nor acceptance criterion §9.2 accounts for it.
3. **Uncapped `IN` list.** SQLite's default `SQLITE_MAX_VARIABLE_NUMBER` is 999; no cap, no chunking, no typed
   error — against CLAUDE.md's sensible-defaults rule.
4. **The justification does not survive the project's own facts.** Spec 014 §8 rests entirely on "adding a method
   to a shipped interface breaks every implementer, so it belongs in this window". But nothing is tagged, there
   are no consumers, and **every implementer is in this repository**. The Resequencer increment is *also* pre-v1,
   so deferring costs one more mechanical break in an unreleased library — against the certain cost of guessing
   four semantics with no caller and freezing the guess into conformance tests across five modules.
5. Sizing: Task 11 is unlabelled but is the **largest task in the plan** (six modules, hand-written SQL ×3,
   Docker-backed conformance).

**Recommendation: cut Task 11 from Plan 027; land `SettleMembers` with the Resequencer that consumes it.**
This is a scope reduction and therefore the user's call.

---

## D. Traceability and ADR gaps

- **D1 · HIGH — `SettleMembers` has no ADR at all.** CLAUDE.md: "a decision with no ADR is incomplete."
- **D2 · HIGH — ADR 0028 never cites ADR 0013 or ADR 0014**, which decided the `MessageChannel` contract and the
  `Subscription`-handle lifecycle it rewrites. ADR 0029 renames `StreamingSource` without citing **ADR 0002**,
  which defined it.
- **D3 · HIGH — plan-number collision.** `docs/specs/011-http-adapter.md:94` and `:677` still assign **Plan 027**
  to the gin binding. The renumber exists only in `HANDOVER.md` and inside Plan 027. (Also: `docs/adrs/0024-*.md`
  is forward-referenced by Spec 011 but does not exist.)
- **D4 · MED — RFC-0004 and RFC-0005 carry no "Promoted to" line**, though RFC-0005 §7.3 is realized in Spec 014
  §8; Spec 014's "Promoted from" omits RFC-0005.
- **D5 · MED — Plan 027 specifies 13 commits with zero traceability trailers**, breaking CLAUDE.md's trailer
  convention that every prior plan follows (cf. Plan 026: `Spec: 011 / Plan: 026 / ADR: 0023`).
- **D6 · LOW — ADR 0019's Status is still "Proposed"** and gains no supersession marker although ADR 0029
  reverses its central decision. Nothing in the plan edits it.
- **D7 · LOW — ADR 0003's dependency statement** ("only non-stdlib dependencies are `clockwork` and
  `cenkalti/backoff/v4`") has been wrong since ADR 0016 and gets wronger here; the bundle opens ADR 0003 as
  amended and leaves it untouched.

---

## E. Factual errors in the bundle (all authored this session, all verified false)

- **E1 · HIGH — "root 32 → 9" is arithmetically false; the table yields 12.** The `9` was inherited from
  RFC-0001 Appendix A, which RFC-0001 itself marks SUPERSEDED, and Spec 014 does no consolidation. It is also a
  *normative acceptance criterion* (§9.1) and ADR 0027 repeats it ("~9 files"). Note `doc.go` is listed as
  "stays in root" but **does not exist** — see F4.
- **E2 · HIGH — ADR 0003 does not contain the premise being amended.** `grep "core is one package"` across
  `docs/` returns only the new bundle. ADR 0003 decides *module* layout and describes a core module holding
  several packages. The flat core is an **undocumented status quo, not a recorded decision** — which is a
  stronger argument for restructuring than the one written.
- **E3 · MED — "Verified: no call site subscribes through the interface"** (Spec 014 §1.2, ADR 0028 Context) is
  contradicted two sentences later by the bundle itself naming `exchange.go:247`.
- **E4 · MED — ADR 0027's `endpoint → channel` edge is fabricated.** Its justification ("the Gateway builds its
  own reply channel") is false: no endpoint-bound file references any concrete channel type. Harmless for
  acyclicity — the real graph is simpler — but it sits inside the load-bearing acyclicity argument.
- **E5 · LOW — ADR 0027's "every reference is godoc prose, not code" is wrong.**
  `adapter/database/sql/source.go:175,235` use `msgin.ExponentialBackoff` in real code, as does
  `harness/source.go:112`; `adapter/http/inbound.go:116` takes `msgin.MessageChannel` in a real signature. The
  original grep covered only `NewConsumer`/`NewProducer`.

---

## F. Tooling and CI reality

- **F1 · HIGH — `gopls` has no Move refactoring.** The plan header mandates "gopls Rename/**Move**"; `gopls
  api-json` (v0.23.0) exposes only rename-related options. Moving files between packages is `git mv` + package
  clause + import rewrite. `gopls` is also **not on PATH** (only at `$(go env GOPATH)/bin/gopls`) and has been
  unavailable inside subagents in past sessions.
  **Fix:** state the real mechanics (`git mv` → package clause → `goimports -w` → `go build ./...` as the
  authoritative reference-finder), the absolute gopls path, and a no-gopls fallback.
- **F2 · HIGH — `go mod tidy` must run in all seven modules, not just root.** Every satellite `go.mod` carries
  `github.com/expr-lang/expr // indirect` with a `replace` to the local root, so Task 1 makes it vanish
  everywhere at once. CI runs tidy + `git diff --exit-code` per module — Task 1 as written leaves **six modules
  red**.
- **F3 · MED — the CI workflow needs two edits and already has a hole.** `.github/workflows/ci.yml`'s `module`
  matrix omits `adapter/cron/crontest`, and the `workspace` job hard-codes a six-directory loop. Task 10 must add
  `expr` to **both**, and fix the pre-existing `crontest` gap.
- **F4 · MED — root loses its package doc.** `doc_composition.go` holds the only `// Package msgin` comment and
  Task 1 deletes it; no task creates a root `doc.go`, and `golangci-lint` will not catch it (ST1000 is excluded).
- **F5 · MED — Task 2's RED cannot be evidenced.** All root tests are one `package msgin_test` binary, so a
  capability test that fails to compile takes the whole binary down and leaves no artifact proving it was red.
  **Fix:** capture the compiler output against stashed production code into the ledger before implementing.
- **F6 · LOW — `breaker.go:175 toHalfOpen` drops to 87.5%** in `resilience` after the split; its remaining arm is
  reached only from consumer/probe-gate tests that land in `endpoint`. Task 7 must add a direct case.
- **F7 · LOW — Task 3's scope overstated.** `StreamingSource` appears 30 times across 12 files, **all in the root
  module** — no satellite references it.

---

## G. Resolved — the bundle's one explicit open question

**ADR 0029 §2's `RequestReplyExchanger` citation HOLDS.** Verified against the Spring Integration reference:
`org.springframework.integration.gateway.RequestReplyExchanger` exists and is the framework's default gateway
service interface ("When a gateway is declared with no `service-interface`, an internal framework interface
`RequestReplyExchanger` is used"). Spring 6.5 adds `AsyncRequestReplyExchanger`.

**Consequence:** "keep `Exchange`, qualified" **stands**; it does not revert to a rename, and Plan 027 Task 3 is
unblocked. Sources: `docs.spring.io/spring-integration/reference/gateway.html`.

*Nit:* Spring's is `RequestReplyExchang**er**` (agent noun, method `exchange`); ours is `RequestReplyExchange`
with method `Exchange`. If the citation is the justification for keeping the term, the `-er` form is both closer
to Spring and what Go's single-method-interface convention points at. Worth one recorded line either way.

*(The plan auditor independently reported this as UNVERIFIED — it had no web access. The design auditor's
verification, with sources, is the one that stands.)*

---

## H. Decisions — ALL RESOLVED (user, 2026-07-27)

> **Standing criterion the user set for this revision:** *"make the product library as flexible as possible with
> sensible defaults / opinionated — a higher-quality library ready for production use."* Where a revision choice
> is otherwise balanced, resolve it toward **an easy default path plus a fully capable escape hatch**, per
> CLAUDE.md's *Sensible defaults (opinionated, but overridable)*. Do not trade the escape hatch away for surface
> minimalism, and do not trade the simple default away for generality.

**H1 — `SettleMembers`: CUT from Plan 027.** Delete Task 11 and Spec 014 §8; re-file the method into RFC-0005's
Resequencer increment, where a real caller pins its four undecided semantics. The "must ride the breaking window"
argument is void — nothing is tagged and every implementer is in this repo, so the Resequencer increment is
equally free. RFC-0005 §7.3 and its §5 step 0 must be updated to say the method lands **with** the Resequencer,
not ahead of it. *(Quality: semantics decided by a caller, not guessed and then frozen by conformance tests
across five modules.)*

**H2 — `RetryPolicy.delayFor`: DELETE the method; inline an unexported helper in `endpoint`.** Neither option the
audit offered is needed. `delayFor` is a 5-line private convenience over **exported** fields
(`Backoff BackoffStrategy`), so `endpoint` can compute it directly once `backoff.go` is split (A2) and
`BackoffStrategy` stays in root. **`RetryPolicy` itself stays in root** — it is caller-facing vocabulary written
as a literal (`msgin.WithRetryPolicy[string](msgin.RetryPolicy{MaxAttempts: 3, DeadLetter: dlq})` throughout
`harness`), not an implementation. Do **not** export `DelayFor`: no public API for internal convenience.
Update A3's table row accordingly.

**H3 — `MessageChannel` and `OutboundAdapter`: KEEP BOTH, with the identity documented as deliberate.**

> **Governing rationale (user, 2026-07-27): consistency with Pipes and Filters.** EIP ch.3's foundational pattern
> is *filters* (processing steps) connected by *pipes* (channels). **`MessageChannel` IS the Pipe** — a
> first-class concept in the pattern this library's composition model is built on. `OutboundAdapter` is a
> **Channel Adapter** at the system boundary (EIP ch.4). They are **two different patterns that happen to share a
> method signature**, not two names for one thing — so collapsing them, or aliasing them together, would erase
> the Pipe from the type system of a pipes-and-filters library. This supersedes the weaker "different roles /
> Spring draws the same line" argument, which pointed the same way for thinner reasons.
>
> This is not a retrofit: `doc_composition.go:4` already states the model — *"endpoints wired as pipes and
> filters. A `MessageHandler` is one step"*. **That file is deleted by Plan 027 Task 1**, so the revision must
> ensure the Pipes-and-Filters framing survives into the new package docs (root `doc.go` plus
> `routing`/`transform`/`channel`), not just get "parked in the ledger". Losing the pattern's name from the
> godoc while keeping its shape would be its own kind of drift.

Spring draws the same line independently (its outbound adapters are `MessageHandler`s, distinct from
`MessageChannel`), and Go's structural typing already makes the two interchangeable at call sites, so keeping
both names costs nothing in flexibility. ADR 0028 must:
- state the identity explicitly in **both** godocs, so it reads as intended rather than accidental;
- record that ADR 0028 §3's "must earn its keep" rule **does not apply** here — that rule governs adding a *new*
  interface with no consumer, whereas `MessageChannel` is existing surface with four call sites that narrowed
  into coincidence;
- **amend ADR 0013**, whose F2 rationale ("`To` takes `OutboundAdapter`, not `MessageChannel`, because a
  `*memory.Broker` satisfies the former but not the latter") is now void;
- extend Spec 014 §9.4's capability test to cover **`OutboundAdapter`-as-route-destination**, which is newly
  reachable and currently untested. *(Flexibility: every shipped outbound adapter becomes a legal discard target,
  default route, and exchange request channel.)*

A type alias (`type MessageChannel = OutboundAdapter`) was considered and **declined**: it would prevent the two
roles ever diverging, which is a constraint the design does not want to commit to.

**H4 — `Chain`/`To`: KEEP in root; restate the acceptance criterion.** `Chain` is a pure combinator — it starts no
goroutine, holds no state, owns no lifecycle — so "a constructor for a running component" was mis-specified, not
mis-placed. Moving the composition entry point that appears in every example, to satisfy a badly-worded gate, is
the worse trade.

**Reinforced by the Pipes-and-Filters rationale (H3):** `Chain` *is* the pipeline assembler and `Step` is the
filter — together with `MessageChannel` (the pipe) they are the pattern's vocabulary, and vocabulary is precisely
what root holds. Pushing the assembler into `endpoint` while the pipe stays in root would split one pattern across
two packages. Replace Spec 014 §9.1 and Plan 027 Task 12 with the scriptable check:

```bash
go list -deps . | grep -E 'msgin/(endpoint|routing|transform|channel|resilience)'   # must be empty
```

plus a **named allow-list** of root constructors deliberately kept — `New`, `NewMessage`, `NewHeaders`, `Chain`,
`To`, `HandlerFunc`, `PayloadOf`, `WithPayload`, `Permanent`, `RetryAfter`, `JSONPayloadCodec`,
`BytesPayloadCodec` — recorded in the Task 0 ledger. Reconcile Spec 014 §4 to that list so the root contract is
**closed** (it currently enumerates none of them). Note the check must exclude `_test` packages: root's
`package msgin_test` files legitimately import the new subpackages.

**H5 — `ReleaseStrategy`: CONFIRMED as `func(MessageGroup) (bool, error)`.** This is the shape the internal field
already has (`aggregator.go:15`); the bool-only form was the *sugar's* shape mistaken for the contract's.
**Keep `WithReleaseStrategy(func(MessageGroup) bool)` as sugar** that wraps it — the easy default path — while the
named type carries the error. Consistent with `CorrelationStrategy`, which already returns `(string, error)`.
This also **resolves B3**: the two orphaned aggregator hot-path branches become reachable through a public API
again, so no coverage is lost when the `*Expr` constructors go. Update Spec 014 §6, ADR 0029 §3, RFC-0003 §3 and
its Appendix A. *(Flexible + sensible default, exactly the standing criterion.)*

**H6 — `Permanent`: KEPT, not renamed.** `terminal` is already load-bearing in `handler.go`/`activator.go` for
"end of chain"; the planned NATS adapter brings `Term` (stop redelivery) — three meanings for one root; and
`permanent`/`transient` is the established antonym pair across six files, which `terminal` has no relationship
with. `reliability.go:20` already records that the marker deliberately mirrors `cenkalti/backoff.Permanent`.
Record in **RFC-0002's drift register** as *considered and kept, with rationale*, which its success metric
requires. `NonRetryable` was the runner-up and is not adopted.

---

## J. Revision execution order

Do the revision as **one atomic pass** — the consistency auditor's findings are largely "document A now disagrees
with document B", so partial integration manufactures exactly that class of defect. Suggested order:

1. **Decisions first** (§H above) into the four owning artifacts: RFC-0002 (H6), RFC-0003 (H5), RFC-0005 (H1),
   ADR 0013 amendment note (H3).
2. **Spec 014** — the heavy one. §1/§3 corrections (§E), the **four** file splits (A1, A2), the new
   **symbol-level table** (A3, 18 identifiers, with H2 applied), the **45-row test-file table** (A4),
   `OverflowPolicy`/`ProbeGate` placement (A5), delete §8 (H1), §6 `ReleaseStrategy` (H5) and the missing
   godoc-alignment subsection (M5), §9.1 rewrite (H4), §9.6 eight modules (M4), §4 closed root contract (H4).
3. **ADRs 0027/0028/0029** — the false claims (§E), the missing citations (D2), H3's decision section, B1's
   exchange-exclusivity decision, B5's `DirectChannel` `Subscription` semantics.
4. **Plan 027** — delete Task 11 (H1); add **Task 3.5** shared-helper resolution before any extraction (A3); fix
   the `gopls` Move claim and state the real mechanics (F1); per-module `go mod tidy` (F2); root `doc.go` (F4);
   Task 2's RED-evidence rule (F5) and its corrected branch list (B5); `breaker.toHalfOpen` case (F6); CI edits
   incl. the pre-existing `crontest` gap (F3); traceability trailers per task (D5); size labels (F7).
5. **Housekeeping** — Spec 011's Plan 027→028 renumber (D3), ADR 0019 supersession status (D6), ADR 0003 amended
   note (D7), RFC-0004/0005 "Promoted to" lines (D4), RFC index layout annotations (L4).
6. **Then round 2** — same three-lens parallel Opus audit on the revised bundle.

## K. Disposition — every round-1 finding (revision pass, 2026-07-27)

Legend: **FIXED** = folded into the named artifact · **DISCARDED** = verified false, with evidence ·
**RE-FILED** = moved to a later increment · **SUPERSEDED** = a better fix than the one the audit proposed.

| # | Disposition | Where |
|---|---|---|
| A1 `Subscription` import cycle | **FIXED** | Spec 014 §3.2 (declaration-level split), ADR 0027 §4, ADR 0028 §1, Plan Task 6 |
| A2 `backoff.go` split | **FIXED** | Spec 014 §3.2, ADR 0027 §4, Plan Task 7 |
| A3 18 crossing identifiers | **FIXED + CORRECTED** | Spec 014 §3.3, ADR 0027 §6–§7, Plan Task 3.5. **Only 8 are genuine**: 4 dissolve with `expr.go`'s deletion, 6 with the `flowcontrol.go` split, **2 are false positives** (below) |
| A4 ~12 unmatched test files | **SUPERSEDED** | Spec 014 §3.4 — the audit's premise (many need splitting) is wrong: a `package X_test` binary may import any package, so a spanning test needs a *home*, not a split. All 45 assigned; **exactly one** splits (`retry_test.go`), and it is not one the audit named |
| A5 `OverflowPolicy`/`ProbeGate` unplaced | **FIXED** | Spec 014 §3.2, ADR 0027 §5 — all four flowcontrol interfaces + `OverflowPolicy` **stay in root**, which also keeps `adapter/memory/queuestore.go:74` compiling unchanged |
| A6 §9.1 falsified by `handler.go` | **FIXED** (§H4) | Spec 014 §4 (closed contract + allow-list) and §9.1 (scriptable `go list -deps .` check); Plan Task 11 |
| B1 exchange reply exclusivity | **FIXED** | ADR 0028 §6 (store + `Close()` the `Subscription`; document exclusivity; test the fan-out case), Spec 014 §5.1, Plan Task 2 |
| B2 `ReleaseStrategy` cannot carry an error | **FIXED** (§H5) | RFC-0003 §3/§7.5, ADR 0029 §3, Spec 014 §6, Plan Tasks 9–10 |
| B3 orphaned aggregator branches | **FIXED** | Resolved by B2 — the fallible type keeps both branches publicly reachable. Plan Task 9 verifies coverage returns to baseline |
| B4 `MessageChannel` ≡ `OutboundAdapter` | **FIXED** (§H3) | ADR 0028 §5 (keep both; Pipe vs Channel Adapter), Spec 014 §5.3, **ADR 0013 amended in place**, Spec 014 §9.4 extends the capability test |
| B5 `DirectChannel` `Subscription` semantics | **FIXED** | ADR 0028 §7 (five-row semantics table), Spec 014 §5.2, Plan Task 2's corrected branch list. The unwritable case is now writable via B1's `Close` |
| C `SettleMembers` | **RE-FILED** (§H1) | Plan Task 11 deleted; Spec 014's old §8 (`SettleMembers`) deleted — **§8 is now "Godoc alignment"**, see M5; RFC-0005 §5/§7.3 + header; RFC index |
| D1 no ADR for `SettleMembers` | **MOOT** | Re-filed with C — the Resequencer increment writes its own ADR |
| D2 missing ADR citations | **FIXED** | ADR 0028 cites + amends 0013 and cites 0014; ADR 0029 cites 0002 |
| D3 plan-number collision | **FIXED** | `docs/specs/011-http-adapter.md:92-94, 675-680` — gin is **Plan 028**; ADR 0024's non-existence stated |
| D4 missing "Promoted to" | **FIXED** | RFC-0004 and RFC-0005 headers |
| D5 no traceability trailers | **FIXED** | Plan Global constraint 7 |
| D6 ADR 0019 status | **FIXED** | ADR 0019 header — Accepted, then partially superseded by ADR 0029 |
| D7 ADR 0003 stale dep list | **FIXED** | ADR 0003 annotation (verified against `go.mod`: `cenkalti/backoff/v4` is not a root dep at all) |
| E1 "root 32 → 9" | **FIXED** | Spec 014 §3.1 and ADR 0027 Consequences — the honest figure is **14**, enumerated |
| E2 ADR 0003 premise false | **FIXED** | ADR 0027 Context + header ("Amends: nothing"), Spec 014 §1.4, RFC index |
| E3 "no call site subscribes" | **FIXED** | RFC-0002 §3, Spec 014 §1.2, ADR 0028 Context — four-of-five, with the fifth named |
| E4 fabricated `endpoint → channel` | **FIXED** | ADR 0027 §2, Spec 014 §3, Plan Tasks 4–8 "Watch for" |
| E5 "godoc prose, not code" | **FIXED** | ADR 0027 Context — the three real code sites named; Plan Task 0 records them |
| F1 `gopls` Move does not exist | **FIXED** | Plan header "How the moves are actually performed" |
| F2 per-module `go mod tidy` | **FIXED** | Plan Task 1 (explicit seven-module loop), Spec 014 §7 |
| F3 CI needs two edits + has a hole | **FIXED** | Plan Task 10 — three edits, including the pre-existing `crontest` gap (verified: the matrix lists 6 dirs) |
| F4 root loses its package doc | **FIXED** | Spec 014 §3.5, Plan Task 1 (`doc.go` created in the same commit) + Global constraint 3 |
| F5 Task 2's RED unevidenced | **FIXED** | Plan Task 2 — compiler transcript against unmodified code goes in the ledger |
| F6 `toHalfOpen` coverage | **FIXED** | Plan Task 7 |
| F7 Task 3 scope overstated | **FIXED** | ADR 0029 §1, Plan Task 3 — 30 occurrences, 12 files, root module only |
| G `RequestReplyExchanger` | **CONFIRMED** | ADR 0029 §2, RFC-0002 §7.1 — verified with source; the `-er` nit decided in favour of our form |
| M4 §9.6 seven modules | **FIXED** | Spec 014 §9.6 — **eight** |
| M5 godoc-alignment subsection | **FIXED** | Spec 014 §8 (new) |
| L4 RFC index layout unannotated | **FIXED** | `docs/rfcs/README.md` — every line marked `[1]` / `4` / `5` by increment |

### Discarded — two A3 rows verified false

- **`breaker`** — the audit lists `breaker.go`'s unexported struct as crossing into `consumer.go`/`flowcontrol.go`.
  It does not. `consumer.go:39,121` declare a **field** named `breaker` of type `CircuitBreaker`;
  `flowcontrol.go:161` is `o.breaker = b`, a field assignment. Every remaining hit is prose. **No crossing.**
- **`RetryPolicy.delayFor`'s framing** — the audit called it an ADR-level dilemma ("export `DelayFor`, or move
  `RetryPolicy` to `endpoint`"). Neither is needed: it is private convenience over the **exported** `Backoff`
  field, so `endpoint` computes it directly once `backoff.go` splits. Deleted and inlined (§H2).

### New findings from the revision pass (not raised by any auditor)

1. **`exchange.go` is a fifth file needing a split.** It declares `RequestReplyExchange` (`exchange.go:35`),
   which Spec 014 §4 keeps in root, while §3 moved the file whole to `endpoint` — the same class of defect as
   A1/A2. Fixed in Spec 014 §3.2 and ADR 0027 §4; it is why the move-list is now declaration-level.
2. **`jitter` is not a crossing** (checked because A3's method missed `exchange.go`). Declared and used only in
   `backoff.go`; `producer.go:362` mentions it in a comment. It travels to `resilience` cleanly.
3. **`nilFuncStep` has a task-ordering wrinkle.** It is declared in `transformer.go`, which Task 5 moves out,
   while `activator.go` uses it and does not leave root until Task 8. Plan Tasks 4–8 now carry an explicit
   three-row sequence for it and `boxMessage`.
4. **ADR 0013's link to ADR 0014 was broken** (`0014-channel-settlement.md`; the file is
   `0014-publish-subscribe.md`). Fixed.
5. **ADR 0013's Status is still "Proposed … pending round-2 re-audit + user go-ahead"** although its decisions
   shipped long ago. **Not fixed here** — out of this bundle's scope, and correcting a shipped ADR's status is a
   separate housekeeping pass. Recorded so it is not lost. *(ADR 0019 had the same defect and was corrected
   because D6 already required editing that header.)*

## I. What round 2 must re-audit

The A-group fixes rewrite Spec 014 §3 (the normative move-list) and add a symbol-level table plus a test-file
table; B2/B4 change public signatures; C potentially removes a task. That is enough to destabilise the design,
so a **full round-2 audit on the revised bundle is required** — two rounds is this project's established norm.
