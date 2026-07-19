# ADR 0016 — Accept `github.com/robfig/cron/v3` as a core dependency

- **Status:** Accepted (2026-07-19) — both adversarial audit rounds complete; the dependency choice was verified
  sound (robfig/cron/v3 confirmed genuinely zero-dependency against its `go.mod`, so the `go mod graph`
  no-transitive acceptance gate will pass). Gated only on an explicit user go-ahead before implementation
  (CLAUDE.md design-time gate).
- **Spec:** [Spec 006 — Cron source + coordination](../specs/006-cron-source.md) (D1).
- **Companion:** [ADR 0017 — Cron source + distributed coordination](0017-cron-source.md) (the design this dep serves).
- **Precedent:** [ADR 0004 — clockwork core dependency](0004-clockwork-dependency.md), [ADR 0005 — cenkalti/backoff
  core dependency](0005-cenkalti-backoff-dependency.md) (the two prior blessed core-dependency exceptions).

## Context

msgin's dependency policy is a **hard rule** (CLAUDE.md, [ADR 0003](0003-multi-module-repository-layout.md)): the
**core / root module** depends on the Go standard library only, with two accepted exceptions —
`github.com/jonboulle/clockwork` (ADR 0004) and `github.com/cenkalti/backoff/v4` (ADR 0005). Every other
third-party client is isolated in its own separate module so a consumer pays only for the adapters it imports.

Spec 006 adds a **cron / recurring message source**. Its defining capability is parsing and evaluating
**cron expressions** (`0 9 * * MON-FRI`), `@every <duration>` intervals, and `@daily/@hourly/...` descriptors, and
computing the next fire time. Two ways to obtain that:

1. **Isolate it in a separate module** (`adapter/cron` with its own `go.mod` requiring a cron library), per the
   default ADR 0003 pattern — so only cron users pay for the dependency.
2. **Write an in-house stdlib-only cron parser** and keep the source in the root module.
3. **Accept a cron library as a core dependency** and keep the source in the root module.

The user chose **option 3** (Spec 006 D1): the cron `Source` lives in the root module (`adapter/cron` package,
like `adapter/memory`/`adapter/database/sql`), and a cron library is accepted as a core dependency, so the source
is used exactly like the other in-module adapters (no `go.work`/`replace` ceremony, no module split).

The candidate is **`github.com/robfig/cron/v3`** — the de-facto standard Go cron library (and the very parser
`go-co-op/gocron` builds on). Verified against source (2026-07-19):

- **Zero dependencies.** Its `go.mod` (`module github.com/robfig/cron/v3`, `go 1.12`) declares **no `require`
  lines at all** — it is pure standard library. Adding it to the root `go.mod` therefore adds exactly one
  package to the module graph and **nothing transitive**.
- **License: MIT** (© 2012 Rob Figueiredo) — compatible with msgin's Apache-2.0 (add attribution to `NOTICE`).
- **Pure Go, no cgo** — preserves the cross-compile / debuggability invariant.

## Decision

**Accept `github.com/robfig/cron/v3` as the third blessed core dependency of the root module**, alongside
`clockwork` (ADR 0004) and `cenkalti/backoff/v4` (ADR 0005). The cron `Source` (Spec 006 / ADR 0017) imports it
from the root module's `adapter/cron` package.

Scope of use: **schedule parsing and next-fire computation only** — `cron.ParseStandard` (or an equivalent parser
configured for cron + `@every` + descriptors) to obtain a `cron.Schedule`, then `schedule.Next(t)`. msgin owns the
firing loop itself (driven by the injected `clockwork.Clock`, ADR 0004) and does **not** use any scheduler-runtime,
goroutine, or job-registry from the library — only its pure, side-effect-free parser + `Next` computation. This
keeps the firing deterministic/testable and the dependency surface minimal.

The `go.mod` `require` must pin a specific released version; `go mod verify` and `go mod tidy` must stay clean, and
`go mod graph` must confirm **no transitive dependency** is introduced (the acceptance test for this ADR).

## Consequences

**Positive.**
- The cron source is a first-class in-module adapter (root module), consistent with `memory`/`http`/`database/sql`
  — no separate-module `go.work`/`replace` overhead for a pure-Go, dependency-free library.
- The transitive burden on every consumer is a **single, dependency-free, pure-Go, MIT** package — comparable to
  the already-accepted `clockwork`, and far lighter than a heavy client (which ADR 0003 rightly isolates).
- Avoids re-implementing (and maintaining) a correct cron parser in-house — cron has real edge cases (ranges,
  steps, descriptors, DST) that a battle-tested library already handles.

**Negative / trade-offs.**
- **Universal transitive dependency:** every msgin consumer now depends on `robfig/cron/v3`, including those who
  never use the cron source. This is the cost of the in-module (vs separate-module) choice; mitigated by the
  library being dependency-free and tiny.
- Grows the blessed-core-dependency set from two to three. Each such exception must clear this same bar (tiny,
  pure-Go, permissive-license, near-zero transitive cost); this ADR is that justification for `robfig/cron/v3`.
- A future desire to drop the dependency (e.g. an in-house parser) would be a module-graph change to the root
  module — an architectural decision requiring a superseding ADR.

**Neutral.**
- Only the parser + `Next` are used; the library's scheduler runtime is deliberately unused, so the coupling
  surface is small and the firing behavior stays under msgin's control (and its `clockwork` clock).

## Alternatives considered

- **Separate `adapter/cron` module (ADR 0003 default).** Isolates the dependency so only cron users pay. Declined
  by the user in favor of root-module ergonomics; acceptable here specifically because the library is
  dependency-free (the isolation an ADR-0003 module provides buys little when the dep has zero transitive cost).
- **In-house stdlib-only cron parser.** Zero dependency, keeps the source in root. Declined: re-implementing a
  correct cron parser (ranges/steps/descriptors/DST) is real risk and maintenance for little gain versus a
  proven, dependency-free library.
- **`go-co-op/gocron`.** Richer scheduler, but 3 runtime deps and an in-process scheduler goroutine that fights
  msgin's goleak/`clockwork` conventions (researched 2026-07-19); rejected as the engine — msgin drives its own
  loop over `robfig/cron`'s parser instead.
