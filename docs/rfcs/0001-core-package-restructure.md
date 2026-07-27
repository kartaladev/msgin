# RFC-0001 — Core package restructure

- **Author:** kartaladev/msgin maintainers
- **Date:** 2026-07-22
- **Status:** Accepted (open questions settled 2026-07-27 — see §7)
- **Promoted to:** [Spec 014](../specs/014-core-package-layout.md) ·
  [ADR 0027](../adrs/0027-core-package-restructure.md) · [Plan 027](../plans/027-core-package-layout.md)
- **Reviewers:** TBD

## 1. Summary

The root module is a single flat `package msgin` with 77 `.go` files (32 source + 45 test). This RFC splits it so
the root holds **only the vocabulary and the SPI**, and every implementation lives in a subpackage named for the
**EIP chapter it belongs to** (`endpoint`, `routing`, `transform`, plus `channel` and `resilience`) — a reader
navigates by the book's own table of contents, and the core is a narrow, stable contract.

## 2. Background & Motivation

Cognitive load is concentrated in one place: `ls` at the repo root is 32 source + 45 test files with no
grouping. The `adapter/` and `docs/` trees are already well-organised and out of scope.

Go constraint: **a directory is a package.** Moving types into subpackages changes their import identity
(`msgin.Filter` → `routing.Filter`) — a breaking API change (amends ADR 0003, "core is one package") and a
cycle risk, since the endpoints, channels, and engine are tightly coupled today. Pre-v1 (`v0.0.x`) makes the
break affordable; this RFC scopes it so cycles are avoided by construction.

## 3. Proposal

### Overview

Organising principle — **interfaces + value types in the root; concrete implementations in subpackages.**
Adapters reference `msgin.*` symbols, most of them SPI interfaces; keeping those in root leaves the adapter
tree almost untouched and preserves the "core is a narrow SPI" invariant.

> **Audit (2026-07-24) — correct the count and enumerate the movers.** The adapter tree references **87
> distinct `msgin.*` symbols** today (`grep -rhoE '\bmsgin\.[A-Z][A-Za-z0-9]*' adapter -r | sort -u | wc -l`),
> not the ~40 originally estimated. The *conclusion* (SPI/vocabulary/engine stay in root → adapters barely
> move) still holds, but the estimate is 2.1× low, so the "narrow SPI" framing must be **shown, not asserted**:
> the plan MUST enumerate exactly which of the 87 relocate (currently `ChannelExchange`→`endpoint`, concrete
> channels→`channel`, `ExponentialBackoff`→`resilience`) and prove the remaining ~84 keep the `msgin.*` path,
> and the apidiff success-metric below must be read against that enumerated move-list. This count also **grows
> with every new adapter landed before the window** — see the sequencing caveat in the [index](README.md).

```
msgin/       root (vocabulary + SPI ONLY): Message/Headers, errors, Delivery, Subscription,
             MessageChannel/SubscribableChannel/MessageHandler, PollingSource/EventDrivenSource,
             ChannelStore/MessageGroupStore/DedupStore, PayloadCodec, RetryPolicy,
             Trigger + TriggerContext, Elector/Locker
  endpoint/    Consumer, Producer, Poller, Gateway, ChannelExchange, Activate,
               ScheduledSource, IdempotentReceiver      (EIP ch.10 Messaging Endpoints)
  routing/     Filter, Router, Splitter, Aggregator, Resequencer, RecipientList
                                                        (EIP ch.7 Message Routing)
  transform/   Transform, Enrich                        (EIP ch.8 Message Transformation)
  channel/     DirectChannel, QueueChannel, PublishSubscribeChannel
  resilience/  RateLimiter, CircuitBreaker, OverflowPolicy, backoff (interfaces + defaults)
  trigger/cron/  CronTrigger (robfig — RFC-0004)
```

Dependency direction (acyclic): `endpoint`, `routing`, `transform`, `channel` → `msgin`; `endpoint` →
`channel` (the Gateway builds its own reply channel) and → `resilience`; `resilience` is a leaf. No
cross-edges, and **nothing in root imports a subpackage**.

**Decision — C-full (2026-07-27).** The engine (`Consumer`/`Producer`/`Poller`) moves out of root **in this
window**, not later. Rationale: the program's premise is *one* breaking window, and deferring C-full schedules a
second one for a change we already know we want — while pre-v1 with zero consumers is the cheapest it will ever
be. It also removes the exception that C-lite carved into this RFC's own organising principle: the engine is the
single largest concrete implementation in the tree, and exempting it would have made the principle decorative.
Other RFCs refer to "the engine" and cite this RFC for its home (`endpoint`).

### Detailed Design

- Move by EIP chapter: `filter`/`router`/`splitter`/`aggregator` → `routing`; `transformer` → `transform`;
  `activator`/`gateway`/`exchange` → `endpoint`; `consumer`/`producer`/`poller`/`credit` → `endpoint`;
  `channel`/`queuechannel`/`pubsub`/`pubsub_registry` → `channel`; the resilience interfaces+defaults →
  `resilience`.
- **`flowcontrol.go` is no longer a sharp edge under C-full.** It mixes the `RateLimiter`/`CircuitBreaker`
  interfaces (→ `resilience`) with the `WithMaxInFlight`-style **`ConsumerOption`** constructors (bound to
  `Consumer`). Under C-lite those options had to stay in root away from their type; under C-full they simply
  travel with `Consumer` into `endpoint`. Split the file along that line; the options keep accepting the
  `resilience` interfaces.
- Interface/impl split examples: `MessageChannel`/`SubscribableChannel` interfaces in root, all three channel
  impls in `channel`; `MessageGroupStore` interface (SPI) in root, `Aggregator` logic in `routing`;
  `RequestReplyExchange` interface in root, `ChannelExchange` impl in `endpoint`.

> **Audit (2026-07-27) — C-full's blast radius on adapter code, measured.** Under C-full the engine
> constructors change import path (`msgin.NewConsumer` → `endpoint.NewConsumer`), which appeared to threaten the
> "adapters barely move" claim. Measured: **6 non-test adapter files** reference the engine constructors, and in
> `adapter/database/sql` (`errors.go`, `outbound.go`, `source.go`) **every reference is godoc prose, not code** —
> adapters *implement* the SPI (which stays in root) and only *mention* the engine in documentation. Real code
> changes are confined to the `harness` test-kit module and 6 `_test.go`/example files that actually construct
> engines. The claim survives C-full; the plan must still enumerate these files rather than assert it.

### Examples

Root `ls` after — vocabulary and SPI only, no implementations: `channel.go codec.go errors.go groupstore.go
handler.go message.go spi.go store.go trigger.go`. Every `.go` file in root declares types and interfaces;
none declares a constructor that builds a running component.

## 4. Trade-offs & Alternatives

### Alternatives Considered

- **A — stay one package, concept-prefix filenames** (`endpoint_*`, `channel_*`). Zero API break, high
  readability, but no folder-level boundaries.
- **B — consolidate files only.** Fewer files, still unsorted.
- **C-lite — subpackages, engine+SPI in root.** Rejected: exempts the largest implementation in the tree from
  the RFC's own organising principle, and defers a known break to a second window.
- **C-full (chosen) — also extract the engine, into `endpoint`.** Purest thin root. The feared cost (breaking
  `msgin.NewConsumer` for adapters) was measured and is small — see the 2026-07-27 audit note in §3.
- **Package naming — EIP chapters (chosen) over `runtime`+`endpoint`.** The draft filed
  filter/router/splitter/aggregator/transformer under a package named `endpoint`; under both the EIP book and
  Spring those are **Message Routing** (ch.7) and **Message Transformation** (ch.8) patterns, while ch.10
  *Messaging Endpoints* is Polling Consumer, Event-Driven Consumer, Gateway, Service Activator, and Idempotent
  Receiver. Shipping the draft's name would have misfiled five patterns inside the very program whose purpose is
  lexical alignment — and RFC-0005's new components would have inherited the misfiling.

### Trade-offs

Blast radius is *not* contained to root: **87 distinct `msgin.*` symbols** are referenced by `adapter/` (audit
count, 2026-07-24). Under C-full the SPI and vocabulary stay `msgin.*`, so adapter *code* is essentially
untouched (measured: §3 audit note, 2026-07-27); the movers are the engine constructors, `ChannelExchange`
→`endpoint`, `ExponentialBackoff`→`resilience`, and the concrete channels→`channel`.

**Decision — clean break, no facade (2026-07-27).** A deprecated re-export facade would have root re-exporting
nearly everything, which is precisely what C-full exists to eliminate; and **nothing is tagged, so there are no
external consumers a facade could protect**. `MIGRATION.md` is still written — as a record for us and the
adapter tree, not as a compatibility shim.

## 5. Implementation Plan

### Phases

1. ADR (amends 0003) + plan; adversarial design audit.
2. Extract `routing` (filter/router/splitter/aggregator — most self-contained). — M
3. Extract `transform`. — S
4. Extract `channel`. — S
5. Extract `resilience` (do the `flowcontrol.go` split). — M
6. Extract `endpoint` — the engine plus gateway/exchange/activator. Last, because everything else must be out
   of root first for the cycle check to be meaningful. — M

Use `gopls` move/rename; `go build ./...` after each move to catch a cycle instantly. Each phase is a green,
committed increment.

### Timeline

No calendar dates — sequenced by dependency (see [index](README.md)). Land inside the shared breaking window.

### Success Metrics

Root source files 32 → **~9**, and **no root file declares a constructor for a running component** (the
mechanical check that C-full actually happened); `go build`/`go test ./... -race` green across the workspace;
**apidiff shows only the intended symbol moves and nothing else**, read against the plan's enumerated move-list
(proves no accidental break beyond the plan).

## 6. Risks & Mitigations

| Risk | Impact | Mitigation |
|---|---|---|
| Import cycle during a move | Build breaks | `go build` after each move; interfaces live in root by design |
| Duplicate `// Package` doc after merges | `go vet` failure | Ensure exactly one package-doc file (`doc.go`) |
| Coverage drop when relocating tests | Gate failure | Move tests behaviour-identical; re-check `-cover` per package |
| Blackbox-test rule violated | CLAUDE.md breach | Keep every `_test.go` as `package <pkg>_test` |

## 7. Decisions (settled 2026-07-27)

1. **C-lite vs C-full → C-full, in this window.** Deferring schedules a second breaking window for a change we
   already want, and pre-v1 with zero consumers is the cheapest it will ever be. Rationale in §3; measured blast
   radius in the §3 audit note.
2. **Facade vs clean break → clean break, no facade;** `MIGRATION.md` written as a record, not a shim. Rationale
   in §4 — a facade would re-export nearly everything from root, defeating C-full, and protects nobody while
   untagged.
3. **`NewDirectChannel` → `channel`.** The draft leaned root, but that was a C-lite compromise; under the chosen
   principle a concrete channel implementation has exactly one home.
4. **Merge depth → moot as posed.** Appendix A's 32→21 consolidation map assumed a flat root; the three-way
   split redistributes those files entirely (root lands at ~9). File granularity within each new package is a
   plan-level detail, not an RFC decision.

Additionally settled here because the package split determined it: the **EIP-chapter package naming** (§4), and
the homes for RFC-0003's behavior types (`routing`, `transform`), RFC-0004's `Poller` and coordination SPI
(`endpoint` and root respectively), and RFC-0005's five components.

## 8. Appendix

> **SUPERSEDED by the C-full decision (2026-07-27).** Appendix A below was written for C-lite, where the engine
> and the endpoint logic both stayed in root and only needed consolidating. Under C-full those files *leave*
> root entirely, so the mapping no longer describes the target. It is retained for the reasoning it records
> (which files are large, which concepts cluster) — the plan derives the real per-package file layout from §3.
> The one resolution below that still stands is the Aggregator's home, now `routing` rather than `endpoint`.

**Appendix A — file consolidation mapping (root 32→21):** `codec.go(+payload)`, `spi.go(+reliability)`,
`channel.go(+store, +handler/groupstore ifaces)`, `resilience_flowcontrol.go(+credit interfaces)`,
`resilience_retry.go(+backoff)`, `channel_pubsub.go(+registry)`, `endpoint.go(handler+transformer+filter+
activator+router)`, `endpoint_aggregator.go(+groupstore logic)`, `doc.go(doc_composition)`; the three large
files that **stay standalone in root** are `consumer`, `producer`, `expr` (the last only until RFC-0003 phase 3
removes `expr-lang`). Tests mirror the prefixes.

> **Audit (2026-07-24) — Aggregator home, resolved.** An earlier draft listed `aggregator` among the "four
> large files [that] stay standalone" in root, contradicting §3 ("`Aggregator` logic in `endpoint`") and this
> appendix's own `endpoint_aggregator.go(+groupstore logic)`. Resolution: the **`MessageGroupStore` interface
> (SPI) stays in root**; the **`Aggregator` implementation moves out of root**. Hence three (not four) large
> files remain standalone in root.
>
> **Amended 2026-07-27:** the Aggregator's destination is **`routing`** (EIP ch.7 Message Routing), not
> `endpoint`. `MessageGroupStore` still stays in root, and RFC-0005 adds `SettleMembers` to it for the
> Resequencer, which lands in `routing` beside the Aggregator.
