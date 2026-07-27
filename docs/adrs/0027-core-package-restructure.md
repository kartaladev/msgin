# ADR 0027 — Restructure the core into EIP-chapter packages (C-full), with a clean break

- **Status:** Accepted (2026-07-27) — pending the mandatory adversarial design audit on the bundle
  (Spec 014 + this ADR + ADR 0028 + ADR 0029 + Plan 027) before any code is written.
- **Amends:** [ADR 0003 — Multi-module repository layout](0003-multi-module-repository-layout.md), specifically
  its "the core is one package" premise. The multi-module decision itself is untouched.
- **RFC:** [0001](../rfcs/0001-core-package-restructure.md) · **Spec:** [014](../specs/014-core-package-layout.md)
  · **Plan:** [027](../plans/027-core-package-layout.md)
- **Relates to:** [ADR 0028](0028-channel-interface-segregation.md) and
  [ADR 0029](0029-eip-lexical-alignment.md), which land in the same window and the same `apidiff` review.

## Context

The core is a single flat `package msgin`: **32 source + 45 test files** with no grouping. Go makes a directory a
package, so introducing structure changes import identity (`msgin.Filter` → `routing.Filter`) — a breaking API
change, and a cycle risk, because the endpoints, channels, and engine are tightly coupled today.

Two questions had to be answered together, and the draft RFC answered only the first.

**Where does the engine live?** RFC-0001 proposed *C-lite*: split out the composable families but keep the
engine (`Consumer`/`Producer`/`Poller`) in root so `msgin.NewConsumer` survives, deferring *C-full* (extract the
engine too) "until the API stabilises". The problem is that the program's entire premise is **one** breaking
window. Deferring C-full schedules a second window for a change already known to be wanted — and the RFC index's
own audit observes that the "quiet `main`" to split from keeps receding, so "later" is the option least likely
to happen cleanly. C-lite also exempts the **single largest concrete implementation in the tree** from the RFC's
own organising principle ("interfaces and value types in root; implementations in subpackages"), which makes the
principle decorative.

The feared cost of C-full was breaking `msgin.NewConsumer` for the adapter tree. Measured: **6 non-test adapter
files** reference the engine constructors, and in `adapter/database/sql` (`errors.go`, `outbound.go`,
`source.go`) **every reference is godoc prose, not code** — adapters *implement* the SPI, which stays in root,
and only *mention* the engine in documentation. Real code changes are confined to the `harness` test-kit module
and 6 `_test.go`/example files. The cost was over-estimated.

**What are the packages called?** RFC-0001 proposed `endpoint` for filter, router, splitter, aggregator, and
transformer. Under both the EIP book and Spring Integration those are **not** endpoints: they are ch.7 *Message
Routing* and ch.8 *Message Transformation* patterns, while ch.10 *Messaging Endpoints* covers Polling Consumer,
Event-Driven Consumer, Messaging Gateway, Service Activator, and Idempotent Receiver. Shipping that name would
have misfiled five patterns **inside the very program whose purpose is preventing lexical drift** — and
RFC-0005's five new components would have inherited the misfiling.

## Decision

### 1. C-full — the engine leaves root, in this window

Root holds **vocabulary and SPI only**. No root file declares a constructor for a running component; that is the
mechanical check that the move actually happened rather than stopping half way.

### 2. Packages are named for the EIP chapter that defines them

```
msgin/       vocabulary + SPI
  endpoint/    Consumer, Producer, Poller, Gateway, ChannelExchange, Activate   (ch.10)
  routing/     Filter, Router, Splitter, Aggregator                             (ch.7)
  transform/   Transform                                                        (ch.8)
  channel/     DirectChannel, QueueChannel, PublishSubscribeChannel
  resilience/  RateLimiter, CircuitBreaker, OverflowPolicy, backoff
```

A reader navigates by the book's own table of contents. Dependency direction is inward and acyclic:
`endpoint`, `routing`, `transform`, `channel` → `msgin`; `endpoint` → `channel` (the Gateway builds its own
reply channel) and → `resilience`; `resilience` is a leaf. **Nothing in root imports a subpackage.**

The alternative of `runtime` for the engine was rejected: it collides with the stdlib package name in a reader's
head, and `endpoint` is the term EIP and Spring already use for exactly these types
(`AbstractEndpoint`/`PollingConsumer`/`EventDrivenConsumer`).

### 3. Clean break — no compatibility facade

No deprecated re-export shim in root. Two reasons: a facade would have root re-exporting nearly everything,
which is precisely what C-full exists to eliminate; and **nothing is tagged**, so there is no external consumer a
facade could protect. `MIGRATION.md` is written as a record for us and the adapter tree, not as a shim.

### 4. Two files split rather than move

`channel.go` (the `MessageChannel` interface stays in root; `DirectChannel` goes to `channel`) and
`flowcontrol.go` (the `RateLimiter`/`CircuitBreaker` interfaces go to `resilience`; the `WithMaxInFlight`-style
`ConsumerOption` constructors travel with `Consumer` into `endpoint`). Under C-lite the latter would have been
stranded in root away from the type they configure — C-full removes the problem rather than managing it.

## Consequences

**Positive.** The organising principle holds without exception. Root becomes a small, stable contract (~9 files)
that adapters compile against unchanged. Package names carry EIP meaning, so RFC-0005's five components each
have an obvious, non-arbitrary home. Only one breaking window is ever spent.

**Negative, accepted.**

- `msgin.NewConsumer` becomes `endpoint.NewConsumer` — the most-typed symbol in the library gains a package
  qualifier. Measured as cheap (above), but it is a real ergonomic loss and every example and README changes.
- **The "land the non-breaking slices early" mitigation dies.** RFC-0003's behavior types and RFC-0004's Poller
  extraction were separable only while they were going to live in a flat `msgin.` namespace; they are now born in
  packages that do not exist until this ADR lands. The compensating decision is to run the window **first**,
  ahead of the feature roadmap, rather than waiting for a quiet `main`.
- Import churn across the adapter tree's test and example files, and a `MIGRATION.md` to maintain.
- **Cycle risk during the move.** Mitigated by construction (interfaces live in root) and by `go build ./...`
  after each extraction, with the engine extracted **last** so the cycle check is meaningful.

**Neutral.** The 87 distinct `msgin.*` symbols referenced by `adapter/` are unchanged in path for the SPI and
vocabulary; the movers are enumerated in Spec 014 §3 and the `apidiff` review is read against that list.
