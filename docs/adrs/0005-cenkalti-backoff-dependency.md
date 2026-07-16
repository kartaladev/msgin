# ADR 0005 — Use cenkalti/backoff for retry backoff

- **Status:** Accepted (2026-07-16)
- **Context source:** [Spec 001 — Messaging core](../specs/001-messaging-core.md) §6–§7; `CLAUDE.md` → Dependency policy
- **Related:** [ADR 0002 — Adapter SPI](0002-adapter-spi.md) (runtime-owned reliability), [ADR 0004 — clockwork](0004-clockwork-dependency.md)

## Context

The runtime-owned `RetryPolicy` (ADR 0002) needs a **backoff strategy** to space out redelivery
attempts: exponential growth, jitter to avoid thundering herds, a maximum interval, honoring
server-provided `Retry-After` (e.g. HTTP `429`), and a way to mark an error permanent (no retry).
These are individually simple but collectively easy to get subtly wrong (jitter distribution,
overflow, off-by-one on caps). `msgin` also mandates **minimal dependencies**, so any addition must
be justified against rolling our own.

We assessed the options rather than defaulting:
- **Roll our own** — ~15 lines for basic exponential+jitter, but re-implements a solved problem and
  invites subtle bugs; no `Retry-After`/`Permanent` semantics without more code.
- **`github.com/cenkalti/backoff`** — the de-facto Go standard (Google's algorithm; 345+ importers;
  MIT license, Apache-2.0 compatible; pure Go; deliberately tiny). Provides the `BackOff` interface
  (`NextBackOff`/`Reset`), `ExponentialBackOff`, `Permanent`, and `RetryAfter`.
- **`avast/retry-go` / others** — capable, but less ubiquitous and oriented around their own retry
  loop, which we do not use.

Version detail: **v4** exposes an injectable `Clock` field (`interface { Now() time.Time }`), which
`clockwork.Clock` (ADR 0004) satisfies directly; **v5+** modernized to a generic `Retry[T]` loop and
reportedly dropped the exported clock. We use the library only as a **delay strategy**
(`NextBackOff`), not its retry loop.

## Decision

Use a **closed-form exponential** for **all redelivery backoff** in the runtime, and adopt
**`github.com/cenkalti/backoff/v4`** only for **genuine tight in-process retry loops** in adapters
(audit H2, refined NF-5):

- **Redelivery backoff (runtime, all sources).** The runtime always knows the *attempt count* — native
  `msgin.delivery-count`, else its tracker — whether the message leaves to a native backend or enters
  the in-process delay-park. So `RetryPolicy.Backoff` is a stateless `BackoffStrategy { Delay(attempt
  int) time.Duration }` computing `min(Max, Initial × Mult^attempt)` with optional deterministic jitter
  seeded by message id. `backoff.BackOff` is stateful (`NextBackOff()` mutates and re-jitters) and
  **cannot be indexed by attempt**, so it is deliberately **not** used here — an earlier draft that
  routed in-process requeue through cenkalti was corrected (NF-5): in-process requeue is not a tight
  loop either (the message parks and re-injects), so it uses the same closed-form path.
- **Tight in-process retry loops (adapters).** The **outbound HTTP** adapter's `POST`-retry loop is a
  real tight loop and uses `cenkalti/backoff/v4` directly — `ExponentialBackOff.Clock` set to the
  injected `clockwork.Clock`, honoring `Permanent`/`RetryAfter` (`429`). This is where the library
  genuinely fits.

`cenkalti/backoff/v4` therefore enters the **core module via `adapter/http`** (in-core). Its `Retry()`
loop's `Permanent`/`RetryAfter` helpers are used for the outbound HTTP call; the redelivery path is
closed-form.

`cenkalti/backoff/v4` is recorded as the **second** accepted non-stdlib dependency of the core module
(after `clockwork`). Its `Retry()` loop is **not** used — the runtime owns retry orchestration.

## Consequences

**Positive**
- A correct, well-tested, ubiquitous backoff (jitter, caps, `Permanent`, `Retry-After`) instead of a
  bespoke re-implementation — aligns with "leverage well-known general solutions."
- Composes with ADR 0004: the same `clockwork` clock drives the Poller *and* backoff elapsed-time →
  deterministic retry tests. (Jitter uses `math/rand`, not the clock — audit L1 — so exact-delay
  assertions set `RandomizationFactor=0` or a seeded rand; only elapsed-time semantics are asserted
  via the fake clock.)
- Small, pure-Go, MIT-licensed; no heavy transitive dependencies.

**Negative / costs**
- A second non-stdlib dependency in the core module, inherited by every core consumer. Accepted
  deliberately; it is tiny and pure Go.
- Pinning **v4** (not the latest major) is a conscious choice for the injectable clock + minimal
  `BackOff` interface; revisit if a future need pulls us to v5's generic API.
- `backoff.BackOff` is stateful and not thread-safe; the runtime uses one instance per in-flight
  retry sequence (in-process/outbound only). It **cannot** be indexed by attempt, which is why the
  stateless native-redelivery path uses the closed-form exponential instead — two backoff code paths
  to keep consistent (mitigated by both reading the same `Initial`/`Mult`/`Max` parameters).

**Rejected alternatives**
- **Hand-rolled backoff** — needless re-implementation of a solved, subtle problem.
- **cenkalti/backoff v5+** — generic `Retry[T]` loop we don't use; lost the injectable clock that
  makes ADR-0004 composition clean.
