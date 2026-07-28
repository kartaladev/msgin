# ADR 0027 — Restructure the core into EIP-chapter packages (C-full), with a clean break

- **Status:** **ACCEPTED — REGENERATED FROM A GREEN TREE (2026-07-28), pending round-3 audit.** Its decisions
  are **implemented** (commit `c83dde9` plus the uncommitted channel work) and every claim below is measured.
  > *Round-2 banner cleared 2026-07-28.* Each defect it named is gone and each fix is evidenced in
  > [`027-derivation-findings.md`](../plans/027-derivation-findings.md):
  > §2's *"no subpackage imports another"* is **restated as the invariant that is actually true** and holds —
  > `poller.go` no longer constructs `ExponentialBackoff` (decision **D-A**, §5a; F11.4 shows zero sibling
  > edges); §5's *"`resilience` is a true leaf"* is **withdrawn and replaced** by the correct claim (§2);
  > the Consequences' *"except for two sites"* is replaced by the **measured 28-file inventory** (§ Consequences;
  > F9.9); §6's *"no unexported helper crosses any boundary"* now covers the **field-access** class it was
  > blind to (§6a, decision **D-H**; F7); and "14 root files" is verified —
  > `ls *.go | grep -v _test.go | wc -l` → 14 (F11.3).
  *(Rounds 1 and 2 both returned `NEEDS-REVISION` from all three auditors; this ADR was revised in place both
  times rather than superseded because it had never been implemented. See
  **[audit round 1](../plans/027-audit-round-1.md)** §K and
  **[audit round 2](../plans/027-audit-round-2.md)** §F.)*
- **Decisions folded in 2026-07-28:** **D-A** (`ExponentialBackoff` placement — §5a) and **D-B**
  (`TopicPublisher`/`TopicSubscriber` — §4a), both settled with the user 2026-07-27
  ([audit round 2 §G.1](../plans/027-audit-round-2.md)). **D-C** (`Subscription` → root `channel.go`) and
  **D-H** (`Message` field access — §6a) are recorded here too; **D-D**/**D-E** belong to ADR 0029 and
  **D-F** to ADR 0028.
- **Amends:** nothing. *(An earlier draft claimed to amend [ADR 0003](0003-multi-module-repository-layout.md)'s
  "the core is one package" premise. **That premise does not exist** — see Context. ADR 0003's multi-module
  decision is untouched.)*
- **RFC:** [0001](../rfcs/0001-core-package-restructure.md) · **Spec:** [014](../specs/014-core-package-layout.md)
  · **Plan:** [027](../plans/027-core-package-layout.md)
- **Relates to:** [ADR 0028](0028-channel-interface-segregation.md) and
  [ADR 0029](0029-eip-lexical-alignment.md), which land in the same window and the same `apidiff` review.
- **Cites:** [ADR 0002](0002-adapter-spi.md) (the SPI that stays in root and is what lets adapters compile
  against root unchanged), [ADR 0013](0013-composition-endpoints.md) (`Chain`/`Step`/`To`, which Decision §6
  keeps in root).

## Context

The core is a single flat `package msgin`: **32 source + 45 test files** with no grouping. Go makes a directory a
package, so introducing structure changes import identity (`msgin.Filter` → `routing.Filter`) — a breaking API
change, and a cycle risk, because the endpoints, channels, and engine are tightly coupled today.

**The flat core is an undocumented status quo, not a recorded decision.** `grep -r "core is one package" docs/`
returns only this bundle's own files. ADR 0003 decides **module** layout and describes a core module that
already contains several packages (`adapter/memory`, `adapter/database/sql`, …). So there is no prior decision
to overturn — only an accreted shape, never argued for and never weighed against alternatives. That is a
**stronger** basis for restructuring than the amendment claim it replaces, which was simply false
(audit finding E2).

Two questions had to be answered together, and the draft RFC answered only the first.

**Where does the engine live?** RFC-0001 proposed *C-lite*: split out the composable families but keep the
engine (`Consumer`/`Producer`/`Poller`) in root so `msgin.NewConsumer` survives, deferring *C-full* (extract the
engine too) "until the API stabilises". The problem is that the program's entire premise is **one** breaking
window. Deferring C-full schedules a second window for a change already known to be wanted — and the RFC index's
own audit observes that the "quiet `main`" to split from keeps receding, so "later" is the option least likely
to happen cleanly. C-lite also exempts the **single largest concrete implementation in the tree** from the RFC's
own organising principle ("interfaces and value types in root; implementations in subpackages"), which makes the
principle decorative.

The feared cost of C-full was breaking `msgin.NewConsumer` for the adapter tree. Two successive estimates of
that cost were wrong in the same direction, and the third is measured rather than estimated:

> *Corrected twice.* An early draft claimed "in `adapter/database/sql` every reference is godoc prose, not
> code" — the grep covered only `NewConsumer`/`NewProducer` (audit E5). The round-1 revision replaced it with
> *"the known non-test adapter code changes are **exactly** two sites"*, which round-2 §A2 (3/3 auditors)
> falsified by two orders of magnitude. **The migration measured it:** `git diff --stat -- adapter/` →
> **28 files changed, 181 insertions, 160 deletions**; `adaptscan` classified **115 CODE selectors + 39
> COMMENT mentions + 0 STRING**, of which **69 non-test selectors are in `adapter/database/sql/harness`, a
> separate module in the CI matrix that root's `go build ./...` cannot see**. The full per-file and per-module
> inventory is Spec 014 §3.6 (evidence: F9.2, F9.3, F9.9).

The cost is real, entirely mechanical, and **no `go.mod` needed an edit** — every satellite already `require`s
and `replace`s the root module, and the new packages live inside it (F9.6).

**What are the packages called?** RFC-0001 proposed `endpoint` for filter, router, splitter, aggregator, and
transformer. Under both the EIP book and Spring Integration those are **not** endpoints: they are ch.7 *Message
Routing* and ch.8 *Message Transformation* patterns, while ch.10 *Messaging Endpoints* covers Polling Consumer,
Event-Driven Consumer, Messaging Gateway, Service Activator, and Idempotent Receiver. Shipping that name would
have misfiled five patterns **inside the very program whose purpose is preventing lexical drift** — and
RFC-0005's five new components would have inherited the misfiling.

## Decision

### 1. C-full — the engine leaves root, in this window

Root holds **vocabulary and SPI only**. Spec 014 §4 enumerates the resulting contract as a **closed** list,
including the pure combinators (`Chain`, `To`, `PayloadOf`, …) deliberately kept there — see §6 below.

### 2. Packages are named for the EIP chapter that defines them

```
msgin/       vocabulary + SPI
  endpoint/    Consumer, Producer, Gateway, ChannelExchange, Activate/Consume   (ch.10)
  routing/     Filter, Router, Split, Aggregator                                (ch.7)
  transform/   Transform                                                        (ch.8)
  channel/     DirectChannel, QueueChannel, PublishSubscribeChannel, PubSub
  resilience/  ExponentialBackoff, NewCircuitBreaker, NewTokenBucket
```

A reader navigates by the book's own table of contents. The dependency graph, **measured against the migrated
tree** rather than asserted:

```
$ for p in . ./endpoint ./routing ./transform ./channel ./resilience; do
    echo "$(go list -f '{{.ImportPath}}' $p): $(go list -f '{{range .Imports}}{{.}} {{end}}' $p \
      | tr ' ' '\n' | grep '^github.com/kartaladev/msgin' | tr '\n' ' ')"; done
github.com/kartaladev/msgin:
github.com/kartaladev/msgin/endpoint:   github.com/kartaladev/msgin
github.com/kartaladev/msgin/routing:    github.com/kartaladev/msgin
github.com/kartaladev/msgin/transform:  github.com/kartaladev/msgin
github.com/kartaladev/msgin/channel:    github.com/kartaladev/msgin
github.com/kartaladev/msgin/resilience: github.com/kartaladev/msgin
```

That is the whole graph: **nothing in root imports a subpackage, and no core package imports another core
package.** Scriptable, with no allow-list entry:

```bash
go list -deps . | grep -E 'kartaladev/msgin/(endpoint|routing|transform|channel|resilience)'         # EMPTY
go list -deps ./endpoint ./routing ./transform ./channel ./resilience | grep 'kartaladev/msgin/'     # EMPTY
```

> *Corrected (audit finding E4).* The earlier text asserted an `endpoint → channel` edge, justified as "the
> Gateway builds its own reply channel". **That edge is fabricated** — no endpoint-bound file references any
> concrete channel type; `exchange.go` takes `MessageChannel`/`SubscribableChannel` interfaces and `gateway.go`
> takes `RequestReplyExchange`.

> *Corrected again (round-2 §A1, 3/3 auditors).* This section also said *"`resilience` is imported by no other
> package in the module"*. **That sentence conflated two different claims**, and only one of them is the
> invariant that matters:
>
> - **"No core package imports another core package"** — the acyclicity invariant. It holds, and decision
>   **D-A** (§5a) is what makes it hold rather than what excuses it.
> - **"Nothing at all imports `resilience`"** — **withdrawn as over-broad.** It swept in
>   `adapter/database/sql` and `adapter/database/sql/harness`, which are *consumers* of the library, not peers
>   of the core packages. `adapter/http` already imports `msgin`; an adapter importing `msgin/resilience`
>   violates nothing. The three adapter change sites are **expected mechanical churn, not an architectural
>   cost** — the earlier framing mispriced them.
>
> Adapters do gain package-level edges into the core subpackages, and that is fine; Spec 014 §3.6 enumerates
> the seven of them.

The alternative of `runtime` for the engine was rejected: it collides with the stdlib package name in a reader's
head, and `endpoint` is the term EIP and Spring already use for exactly these types
(`AbstractEndpoint`/`PollingConsumer`/`EventDrivenConsumer`).

### 3. Clean break — no compatibility facade

No deprecated re-export shim in root. Two reasons: a facade would have root re-exporting nearly everything,
which is precisely what C-full exists to eliminate; and **nothing is tagged**, so there is no external consumer a
facade could protect. `MIGRATION.md` is written as a record for us and the adapter tree, not as a shim.

### 4. **Six** files split rather than moved

Not two, and not five. Each mixes a root-contract declaration with its implementation, and three are
**cycle-forced** rather than stylistic. The declaration-level table — **80 declarations, generated, complete**
— is Spec 014 §3.2; in summary:

| File | Root keeps | Where root keeps it | Why it cannot move whole |
|---|---|---|---|
| `channel.go` | `MessageChannel`, `SubscribableChannel` | `channel.go` | Interface/implementation split (ADR 0028) |
| `pubsub.go` | `Subscription` | **`channel.go:49`** (decision D-C) | It is the return type of root's `SubscribableChannel.Subscribe` — moving it makes **root import `channel`** |
| `backoff.go` | `BackoffStrategy` | `backoff.go:10` | `retry.go`'s `RetryPolicy.Backoff` field is typed with it, and `retry.go` stays |
| `exchange.go` | `RequestReplyExchange` | `spi.go:118` | Root keeps the seam a future **external** exchange adapter implements (Return Address — Spec 014 §10) |
| `flowcontrol.go` | `RateLimiter`, `CircuitBreaker`, `ProbeGate`, `OverflowPolicy` (+ 4 consts + `String()`) | `flowcontrol.go` | Decision §5 |
| **`pubsub_registry.go`** | **`TopicPublisher`, `TopicSubscriber`** | **`spi.go:91,99`** | **Decision §4a** |

The `pubsub.go` cycle was reached independently by all three round-1 auditors (finding A1); `exchange.go` was
found during the round-1 revision, which is why the move-list became declaration-level; `pubsub_registry.go`
was found by round 2 (§A4).

**`Subscription`'s home is root `channel.go`, not a new `subscription.go`** — decision **D-C**. It sits beside
`SubscribableChannel`, the interface that returns it, and root stays at 14 files. Round-2 §A3's charge that
*"'exactly 14' is unsatisfiable"* is answered by placement, not by changing the number
(`ls *.go | grep -v _test.go | wc -l` → 14).

### 4a. `TopicPublisher`/`TopicSubscriber` are root SPI — decision D-B (settled, user, 2026-07-27)

`pubsub_registry.go` declared four kinds of thing: two **root SPI interfaces** and the in-process registry
that happens to implement them. The file-level move-list sent it whole to `channel`, so a root contract would
have moved into an implementation package with no decision recorded — and it **compiles either way**, which
is why nobody noticed (round-2 §A4).

**Decision: `TopicPublisher` and `TopicSubscriber` stay in root** (folded into `spi.go`); `PubSub`,
`NewPubSub`, `PubSub.Publish`/`Subscribe`/`TopicCount`, `topicSubscription` and its `Cancel` go to `channel`.

The reason is in `TopicPublisher`'s own godoc: *"Native-topic broker adapters (Kafka, NATS, Redis) implement
this using their own topics, so topic support is handled generically through one SPI."* Both
[ADR 0014](0014-publish-subscribe.md) and [Spec 004 §105](../specs/004-publish-subscribe.md) file the pair as
**Layer 1 — SPI (adapters implement this)**. Had the file moved whole, a future NATS/Redis topic adapter would
import `msgin/channel` to implement the seam — inverting this project's "adapters implement **root** SPI" rule.

Both interfaces are added to Spec 014 §4's closed root contract, where they had never appeared.

### 5. The governor interfaces stay in root; `resilience` holds implementations only

`RateLimiter`, `CircuitBreaker`, `ProbeGate`, and `OverflowPolicy` (with its four constants and `String()`)
**stay in root**. Only `ExponentialBackoff`+`jitter`, `NewCircuitBreaker`+`breaker`, and
`NewTokenBucket`+`tokenBucket` move to `resilience`.

This **reverses** an earlier draft that sent the interfaces to `resilience`. Three reasons, in order of weight:

1. **It contradicted this ADR's own organising principle** — "interfaces and value types in root".
2. **It broke a shipped adapter's *exported* API.** `adapter/memory/queuestore.go:74` declares
   `func WithOverflow(p msgin.OverflowPolicy) QueueStoreOption`, with five more uses in the same file. Moving
   `OverflowPolicy` would change a real exported adapter signature — a different and worse cost than the
   mechanical import churn the rest of this window imposes (audit finding A5). Verified after the migration:
   `adapter/memory` needed **one** changed line in total (F9.9).
3. **`ProbeGate` was invisible.** `flowcontrol.go:71` declares it and **no bundle document mentioned it** — the
   "interface half / option half" description did not classify all four kinds of declaration in the file.

`BackoffStrategy` is root-bound anyway (§4), so putting the three governor interfaces beside it is also the
consistent choice.

### 5a. `ExponentialBackoff` moves to `resilience`, and the edge it would create is REMOVED — decision D-A (settled, user, 2026-07-27)

Round-2 §A1 (3/3 auditors) found real code, not godoc, forcing an inter-subpackage edge:

```
poller.go:131-137
func (c *consumer[T]) pollErrorBackoff(n int) time.Duration {
	return ExponentialBackoff{Initial: c.pollInterval, Max: maxPollErrorBackoff, Mult: 2}.Delay(n - 1)
}
```

**Decision: `BackoffStrategy` (the contract) stays in root; `ExponentialBackoff` + `jitter` (the
implementation) move to `resilience`.** The organising principle applies without exception, and future backoff
strategies land beside the first one rather than splitting the family across two packages post-v1.

**The `endpoint → resilience` edge is removed, not accepted.** It existed for exactly one internal
convenience call, so `endpoint` declares its own bounded default:

```go
// endpoint/poller.go
//
// Deliberately NOT resilience.ExponentialBackoff: that type is public and
// unconstrained, so it carries float/NaN/overflow guards this call site cannot
// need. pollInterval is validated > 0 (ErrInvalidPollInterval) and the result is
// hard-capped at maxPollErrorBackoff (30s), so the doubling is bounded to at most
// six iterations. Keeping it local keeps endpoint free of any subpackage import.
func (c *consumer[T]) pollErrorBackoff(n int) time.Duration {
	d := c.pollInterval
	for i := 1; i < n && d < maxPollErrorBackoff; i++ {
		d *= 2
	}
	return min(d, maxPollErrorBackoff)
}
```

**Behavior-identical** to `ExponentialBackoff{Initial: pollInterval, Max: 30s, Mult: 2}.Delay(n-1)` on every
arm — `n<=0` → `pollInterval` (matching `Delay`'s `attempt<0` clamp); `n=1` → `pollInterval`; `n=2,3` → 2×,
4×; large `n` → capped. `RandomizationFactor` is zero here, so no jitter path is involved. This **removes**
float arithmetic from the poll loop rather than duplicating it.

Consequences:

- The acyclicity claim becomes the precise one in §2, and it is scriptable with **no allow-list entry**.
- `backoff.go` **remains a split** (§4): `BackoffStrategy` root, `ExponentialBackoff` + `jitter` → `resilience`.
- Callers write two imports for a retry policy —
  `msgin.RetryPolicy{Backoff: resilience.ExponentialBackoff{…}}`. Accepted; recorded in `MIGRATION.md`.
- The three adapter construction sites (`sql/source.go:175,235`, `harness/source.go:112`) are expected
  mechanical churn.

**Filed as a follow-up, deliberately NOT in this window:** `pollErrorBackoff` is a hard-coded policy with no
`WithPollErrorBackoff` override, which CLAUDE.md's *Sensible defaults (opinionated, but overridable)* rule
makes a latent gap. It is **additive and non-breaking**, so it does not need the breaking window, and this
increment is declared behavior-preserving. Add it as its own increment rather than smuggling it in here.

### 6. Shared unexported helpers: resolved symbol-by-symbol, with no `internal/` package

A file-level move-list is blind to unexported identifiers crossing the new boundaries, and a plan built on one
does not compile (audit finding A3, reached by two auditors). Verified by grepping every declaration and use
site across all 32 non-test root files: **8 crossings are genuine**, **10 dissolve** (4 when `expr.go` is
deleted, 6 with the `flowcontrol.go` split), and **2 were false positives** (`breaker` — a *field name*, not
the unexported type; and `jitter`, checked during the revision, which never leaves `backoff.go`).

> *Arithmetic corrected (round-2 §D10).* An earlier draft wrote *"8 genuine + 4 + 6 + 2 = 18"*, which sums to
> **20**, and mixed units — the "8" counted *table rows*, of which one row carries three symbols
> (`attemptTracker`/`attemptEntry`/`newAttemptTracker`). The counts above are **rows**: 8 + 10 + 2 = 20 rows,
> covering 22 symbols. Stated in one unit so the sum checks.

The full table is Spec 014 §3.3. The three resolution strategies, and why each:

- **Inline over public API** — `boxMessage`, `nilFuncStep`. Both are 1–5 lines composed entirely of exported
  symbols, so each destination package declares its own copy. A third party writing their own endpoint already
  has everything they need (`NewMessage`, `Step`, `HandlerFunc`, `ErrNilFunc`).
- **Move to the package that uses it** — `noNativeReliability`, `attemptTracker`/`attemptEntry`/
  `newAttemptTracker` → `endpoint`. Pure runtime machinery with no caller-facing meaning.
- **Export** — three symbols, §7 below.

**No `internal/` package is created.** CLAUDE.md mandates `internal/` for internals that must not be imported,
but every crossing here resolves without one, and adding a package for three one-line helpers would be more
structure than the problem has. Recorded explicitly so the omission reads as a decision, not an oversight.

`RetryPolicy.delayFor` is **deleted**, its computation moved to a package-local `retryDelay` in `endpoint`
(audit §H2; shipped at `endpoint/consumer.go:948`). It is private convenience over the **exported**
`Backoff BackoffStrategy` field, so `endpoint` can compute it directly. `RetryPolicy` itself stays in root —
it is caller-facing vocabulary written as a literal throughout `harness`, not an implementation. **`DelayFor`
is deliberately not exported**: no public API for internal convenience. *(It had **three** call sites, not the
two an earlier draft claimed — round-2 §D11. The resolution is unchanged; only the count was wrong.)*

### 6a. Unexported **struct-field access** is a second crossing class — decision D-H (forced)

Round-2 §B1 proved that §6's sweep, however careful, could not have found what actually broke the build: a
grep over *declarations* cannot see **field access on a type that stays in root**. `endpoint` read
`Message`'s unexported `payload`/`headers` at **six** sites:

```
$ grep -rn '\.payload\|\.headers' endpoint/           # BEFORE the fix
endpoint/producer.go:417   endpoint/producer.go:419   endpoint/producer.go:423
endpoint/consumer.go:694   endpoint/consumer.go:828   endpoint/consumer.go:835
```

The compiler reported only **five** of the six — Go caps at ten errors per package and truncated, hiding
`producer.go:423` entirely. **Enumerate with `grep`, then confirm with the compiler; never the reverse.**

**Decision (forced by the type system, not chosen): rewrite over the public API** —
`msgin.NewMessage[T](payload, m.Headers())` and `m.Payload()`. `NewMessage` is literally
`Message[T]{payload: payload, headers: headers}` (`message.go:184-186`) and `Headers` wraps a map, so passing
`m.Headers()` aliases the same map the struct literal did: **identity is preserved bit-for-bit**.

> **`msgin.New[T]` must never be used here.** It re-stamps `msgin.message-id` and `msgin.timestamp` on every
> consumed message, and **no existing assertion would catch the regression** — the behavior-preservation
> guard is blind to it. Verified absent: `grep -rn 'msgin\.New\[' endpoint/` → exit 1.

**D-H costs nothing structurally** (F7): the `endpoint → root` edge needed no widening and no new exported
symbol. §6's conclusion — *"after the extraction, no unexported helper crosses any package boundary"* — is
true again, and now covers both classes.

**One residual, recorded rather than papered over:** root's `boxMessage` (`payload.go:30`) and `nilFuncStep`
(`handler.go:66`) are now **dead code** — every package inlined its own copy and root has no user left. Go
does not error on unused package-level declarations and `.golangci.yml` sets `linters.default: none`, so
`unused` is off and nothing reports it. Plan 027 Task 9.5 deletes them.

### 7. Three symbols become exported

| New | Was | Why |
|---|---|---|
| `IsPermanent(err) bool` | `isPermanent` | **Forced.** `errors.As` over the *unexported* `*permanentError`; no other package can reimplement it. Also the natural public twin of the exported `Permanent`. |
| `RetryAfterOf(err) (time.Duration, bool)` | `retryAfterOf` | **Forced**, same reason (`*retryAfterError`); twin of exported `RetryAfter`. |
| `NewID() string` | `randomID` | **Chosen** (settled with the user 2026-07-27). Not forced — 4 lines of `crypto/rand`+hex could be duplicated in `endpoint` — but one id scheme keeps the message-id and correlation-id formats from drifting, and lets a future external exchange adapter mint a matching id. |

A caller can construct `Permanent`/`RetryAfter` markers today but cannot classify them; the first two exports
close that asymmetry. All three are pre-recorded `apidiff` entries (Spec 014 §4.1).

**One of the three needs its godoc widened on export** (round-2 §D16): **`IsPermanent` is a policy
classifier, not a marker inspector.** It returns true for `ErrPayloadType`, `ErrPayloadDecode`, and
`ErrPayloadTooLarge`, none of which ever passed through `Permanent`. Framing it as "the natural public twin of
`Permanent`" understates what exporting freezes into the contract, so the godoc must enumerate the policy.
Likewise `RetryAfterOf`'s internal rationale for skipping a nil guard (*"the only caller never passes nil"*)
is **void on export** (§D17) — the nil case is public surface and needs a test.

## Consequences

**Positive.** The organising principle holds without exception. Root becomes a small, stable contract of
**14 source files** and **101 exported non-method symbols**. Package names carry EIP meaning, so RFC-0005's
five components each have an obvious, non-arbitrary home. The dependency graph is a single fan-in to root —
**zero inter-subpackage edges**, verified by `go list`, not asserted. Only one breaking window is ever spent.

```
$ ls *.go | grep -v _test.go | wc -l
      14
$ decls . | grep -v _test.go | awk -F'\t' '$5=="exported" && $3!="method" {print $4}' | sort -u | wc -l
     101
```

> *Corrected (audit finding E1, then D8).* Earlier drafts said "~9 files". The enumerated move-list yields
> **14**. The `9` was attributed to RFC-0001 **Appendix A**, which is marked SUPERSEDED and in any case says
> **32→21**; the `9` actually lives in RFC-0001 **§5 Success Metrics** (and §7.4), neither superseded — and
> §5 *also* still carries the A6-killed *"no root file declares a constructor"* criterion and the
> *"use `gopls` move/rename"* instruction that F1 showed does not exist. The figure sat inside a normative
> acceptance criterion, which is now the explicit 14-file list plus a scriptable import check (Spec 014 §9.1).

**Adapters do NOT compile against root unchanged, and that estimate was wrong twice.** The measured cost is
**28 files, ±181 lines, across `adapter/` and two satellite modules** — 115 code selectors plus 39 godoc
mentions no compiler will ever flag (Spec 014 §3.6; F9.9). It is entirely mechanical, and **no `go.mod`
needed an edit**, but it is not zero and it is not two sites.

**Negative, accepted.**

- `msgin.NewConsumer` becomes `endpoint.NewConsumer` — the most-typed symbol in the library gains a package
  qualifier. Measured as cheap, but it is a real ergonomic loss and every example and README changes.
- **Three new exported symbols** (§7) in a window whose theme is *shrinking* the root surface. Two are forced by
  the type system; the third was a deliberate trade.
- **The "land the non-breaking slices early" mitigation dies.** RFC-0003's behavior types and RFC-0004's Poller
  extraction were separable only while they were going to live in a flat `msgin.` namespace; they are now born in
  packages that do not exist until this ADR lands. The compensating decision is to run the window **first**,
  ahead of the feature roadmap, rather than waiting for a quiet `main`.
- **`boxMessage` and `nilFuncStep` are duplicated** across three packages each (`endpoint/helpers.go`,
  `routing/helpers.go`, and inline in `transform/transformer.go`). Accepted over an `internal/` package or new
  public API; both are trivial adapters over exported symbols.
- Import churn across the adapter tree's test and example files, and a `MIGRATION.md` to maintain.
- **Coverage attribution shifts across the split, and the naive comparison reports a false regression.**
  Blackbox tests move to sibling packages, so credit follows the *test binary*: default per-package coverage
  puts root at **81.8%** (was 99.3%), below CLAUDE.md's 85% gate, while `-coverpkg=./...` shows the workspace
  at **93.3%** against a 93.23% baseline. Every comparison in this window uses `-coverpkg=./...` on both
  sides (Spec 014 §3.4e; round-2 §A8).
- **Cycle risk during the move.** Mitigated by construction (interfaces live in root), by the declaration-level
  move-list (§4), and by **`go vet ./...`** — not `go build` — after each individual move, with the engine
  extracted **last** so the cycle check is meaningful. `go build` does not compile test binaries and cannot
  see the six satellite modules; that gap is how round-2 §B2 survived.

**Neutral.** The 87 distinct `msgin.*` symbols referenced by `adapter/` are unchanged in path for the SPI and
vocabulary; the movers are enumerated in Spec 014 §3 and the `apidiff` review is read against §4.1's
decomposition of the 93 removals.
