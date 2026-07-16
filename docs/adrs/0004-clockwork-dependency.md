# ADR 0004 — Use jonboulle/clockwork directly for injectable time

- **Status:** Accepted (2026-07-16)
- **Context source:** `CLAUDE.md` → Testing rules, Dependency policy; [Spec 001](../specs/001-messaging-core.md) §4, §7
- **Related:** [ADR 0003 — Module layout](0003-multi-module-repository-layout.md)

## Context

`msgin` needs **injectable time**: the Poller's interval/fixed-delay/backoff scheduling and
message timestamps must be deterministically controllable in tests (no real sleeping, no flaky
timing). The library also mandates a **stdlib-only core** for minimal consumer dependencies. These
two goals are in tension: deterministic time testing needs a fake clock, which is a small library
concern.

## Decision

Use **`github.com/jonboulle/clockwork` directly** as the time abstraction in the core — accept a
`clockwork.Clock` (default `clockwork.NewRealClock()`), inject via a `WithClock`-style option, and
drive tests with `clockwork.NewFakeClock()`. **Do not** wrap it in a first-party `Clock` interface.

`clockwork` is recorded as the **single, deliberate exception** to the core's stdlib-only rule. No
other third-party import is permitted in the pattern core or the `memory`/`database/sql` adapters.

## Consequences

**Positive**
- Deterministic, non-flaky testing of the Poller and any time-dependent logic (`NewFakeClock` +
  `Advance`), without real sleeps.
- No bespoke clock abstraction to design, document, and maintain; `clockwork` is a small, stable,
  widely-used, pure-Go library.
- Consumers get a familiar, standard clock type they can also fake in their own tests.

**Negative / costs**
- The core module carries one non-stdlib dependency (`clockwork`), and thus so does every consumer
  of the core. Accepted deliberately; it is small, pure Go, and has no heavy transitive deps.
- `clockwork.Clock` becomes part of the public API surface (the `WithClock` option), so a breaking
  change in clockwork could affect `msgin`'s API — mitigated by clockwork's stability and pinning.

**Rejected alternatives**
- **First-party `Clock` interface (stdlib-only core)** — keeps the core dependency-free, but
  reinvents an abstraction and its fake; explicitly rejected in favor of using the standard tool
  directly.
- **`func() time.Time` injection** — too thin; doesn't cover tickers/timers/`After` needed by the
  Poller.
