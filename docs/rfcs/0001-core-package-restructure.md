# RFC-0001 — Core package restructure

- **Author:** kartaladev/msgin maintainers
- **Date:** 2026-07-22
- **Status:** Draft
- **Reviewers:** TBD

## 1. Summary

The root module is a single flat `package msgin` with 85 files. This RFC splits the composable families into
concept subpackages (`channel`, `endpoint`, `resilience`) while keeping the vocabulary, SPI, and engine in the
root, so a reader navigates by folder and the core stays a narrow, stable contract.

## 2. Background & Motivation

Cognitive load is concentrated in one place: `ls` at the repo root is 32 source + 45 test files with no
grouping. The `adapter/` and `docs/` trees are already well-organised and out of scope.

Go constraint: **a directory is a package.** Moving types into subpackages changes their import identity
(`msgin.Filter` → `endpoint.Filter`) — a breaking API change (amends ADR 0003, "core is one package") and a
cycle risk, since the endpoints, channels, and engine are tightly coupled today. Pre-v1 (`v0.0.x`) makes the
break affordable; this RFC scopes it so cycles are avoided by construction.

## 3. Proposal

### Overview

Organising principle — **interfaces + value types in the root; concrete implementations in subpackages.**
Adapters reference ~40 `msgin.*` symbols, most of them SPI interfaces; keeping those in root leaves the adapter
tree almost untouched and preserves the "core is a narrow SPI" invariant.

```
msgin/       root: Message/Headers, errors, Delivery + SPI ifaces, MessageChannel/Handler ifaces,
             ChannelStore/MessageGroupStore ifaces, PayloadCodec, RetryPolicy, Consumer/Producer/Poller
  channel/     QueueChannel, PublishSubscribeChannel
  endpoint/    filter, router, splitter, aggregator, transformer, activator, gateway, expr
  resilience/  RateLimiter, CircuitBreaker, OverflowPolicy, backoff (interfaces + defaults)
```

Dependency direction (acyclic): `channel`, `endpoint` → `msgin`; `msgin` (engine options) → `resilience`
(a leaf). No cross-edges.

This is **C-lite** — the engine (`Consumer`/`Producer`/`Poller`) stays in root, so `msgin.NewConsumer` and the
adapter-facing SPI keep their names. **C-full** (extract the engine to a `runtime` package) is a strict
superset, deferred until the API stabilises (see Trade-offs). Other RFCs refer to "the engine" and cite this
RFC for its home.

### Detailed Design

- Move the seven endpoint files to `endpoint`, the two concrete channels to `channel`, the resilience
  interfaces+defaults to `resilience`.
- **The one sharp edge:** `flowcontrol.go` mixes the `RateLimiter`/`CircuitBreaker` interfaces (→ `resilience`)
  with the `WithMaxInFlight`-style **`ConsumerOption`** constructors (bound to `Consumer` → stay in root).
  Split it; the options keep accepting the `resilience` interfaces.
- Within each package, consolidate small sibling files into concept files (full mapping in Appendix A).
- Interface/impl split examples: `MessageChannel` interface in root, `QueueChannel` impl in `channel`;
  `MessageGroupStore` interface (SPI) in root, `Aggregator` logic in `endpoint`; `RequestReplyExchange`
  interface in root, `ChannelExchange` impl in `endpoint`.

### Examples

Root `ls` after (clusters obvious): `channel.go channel_pubsub.go channel_queue.go codec.go endpoint*.go
errors.go gateway*.go message.go resilience_*.go runtime_consumer.go runtime_producer.go spi.go`.

## 4. Trade-offs & Alternatives

### Alternatives Considered

- **A — stay one package, concept-prefix filenames** (`endpoint_*`, `channel_*`). Zero API break, high
  readability, but no folder-level boundaries.
- **B — consolidate files only.** Fewer files, still unsorted.
- **C-lite (chosen) — subpackages, engine+SPI in root.**
- **C-full — also extract the engine to `runtime`.** Purest thin root, but breaks `msgin.NewConsumer` for
  adapters/consumers; defer.

### Trade-offs

Blast radius is *not* contained to root: ~40 adapter symbols. Under C-lite, SPI/vocabulary/engine stay
`msgin.*`, so pure adapter code is untouched; only a few symbols move (`ChannelExchange`→`endpoint`,
`ExponentialBackoff`→`resilience`, concrete channels→`channel`). Recommend **no re-export facade** (clean
pre-v1 break with a `MIGRATION.md`) over a deprecated-alias facade.

## 5. Implementation Plan

### Phases

1. ADR (amends 0003) + plan; adversarial design audit.
2. Extract `endpoint` (most self-contained). — M
3. Extract `channel`. — S
4. Extract `resilience` (do the `flowcontrol.go` split). — M
5. In-root file consolidation (Appendix A). — S
6. *(optional, later)* C-full: extract `runtime`. — M

Use `gopls` move/rename; `go build ./...` after each move to catch a cycle instantly. Each phase is a green,
committed increment.

### Timeline

No calendar dates — sequenced by dependency (see [index](README.md)). Land inside the shared breaking window.

### Success Metrics

Root source files 32 → ~21; `go build`/`go test ./... -race` green across the workspace; **apidiff shows only
the intended symbol moves and nothing else** (proves no accidental break beyond the plan).

## 6. Risks & Mitigations

| Risk | Impact | Mitigation |
|---|---|---|
| Import cycle during a move | Build breaks | `go build` after each move; interfaces live in root by design |
| Duplicate `// Package` doc after merges | `go vet` failure | Ensure exactly one package-doc file (`doc.go`) |
| Coverage drop when relocating tests | Gate failure | Move tests behaviour-identical; re-check `-cover` per package |
| Blackbox-test rule violated | CLAUDE.md breach | Keep every `_test.go` as `package <pkg>_test` |

## 7. Open Questions

1. C-lite now, or commit to C-full in the same window?
2. Deprecated re-export facade, or clean break + `MIGRATION.md`?
3. `NewDirectChannel` — root (recommended) or `channel`?
4. Merge depth — moderate (recommended, ~21 files) vs aggressive.

## 8. Appendix

**Appendix A — file consolidation mapping (root 32→21):** `codec.go(+payload)`, `spi.go(+reliability)`,
`channel.go(+store, +handler/groupstore ifaces)`, `resilience_flowcontrol.go(+credit interfaces)`,
`resilience_retry.go(+backoff)`, `channel_pubsub.go(+registry)`, `endpoint.go(handler+transformer+filter+
activator+router)`, `endpoint_aggregator.go(+groupstore logic)`, `doc.go(doc_composition)`; the four large
files (`consumer`, `producer`, `aggregator`, `expr`) stay standalone. Tests mirror the prefixes.
