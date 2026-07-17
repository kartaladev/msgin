# Plan 004 — Resilience hardening (clear the ADR 0008 D10 backlog)

- **Spec:** [001 — Messaging core](../specs/001-messaging-core.md) §7.4
- **ADR:** [0009 — Resilience hardening](../adrs/0009-resilience-hardening.md) (D1–D5); builds on
  [0007](../adrs/0007-reliability-settlement-api.md), [0008](../adrs/0008-resilience-flow-control-api.md)
- **Predecessor:** Plans 001/002/003 (landed on `main`). **Successor:** Plan 005 (Poller + `sql`).
- **Branch:** `feat/resilience-hardening`.

## Objective

Clear the tracked resilience follow-ups (HANDOVER §4 / ADR 0008 D10) that are testable against the
in-memory adapter (+ synthetic wire sources), so Plan 005 (the first *wire* adapter) starts from a
hardened base. **Five of six are cleared** (findings 1–5 → D1–D5); finding 6 (at-least-once overflow)
is the one genuine deferral (ADR 0009 D6 → Plan 006+, untestable without a real at-least-once source).

Every task is an independently green unit (`go test ./... -race` passes) and follows red→green TDD with
assert-closure table tests (`table-test` skill), blackbox (`package msgin_test`), goleak-clean. All the
adversarial-audit fixes are folded into the tasks below.

## Tasks

### Task 1 — `WithAttemptTTL` escape hatch (ADR 0009 D3)

- Add `WithAttemptTTL[T](d)` (`flowcontrol.go`), storing `attemptTTL` + `attemptTTLSet` on
  `consumerConfig`. `NewConsumer`: unset → `defaultAttemptTTL`; set → require `d > 0`, else
  `ErrInvalidAttemptTTL` (new sentinel). Thread into `newAttemptTracker(cfg.clock, ttl)`.
- Godoc states the **reframed** invariant: "TTL must comfortably exceed the worst-case redelivery
  round-trip **including handler execution** — not merely `Backoff.Max`; a too-small value sweeps
  in-flight ids mid-retry and restarts them at attempt 1, defeating `MaxAttempts` (reachable on the
  memory adapter, which ignores Nack delay)."
- **Hot-path branches:** unset→default; set-valid→used; set-`0`/negative→`ErrInvalidAttemptTTL`.
- **Tests:** `WithAttemptTTL(0)`→`ErrInvalidAttemptTTL`; tiny TTL reclaims a genuinely-idle id (fake
  clock advanced past TTL after last observe); NF-2 — a redelivering id re-observed within TTL is **not**
  swept (model the observe→advance(<ttl)→observe ordering explicitly on the fake clock).

### Task 2 — Drop `ErrNoPayloadCodec`; ratify JSON default (ADR 0009 D4)

- Remove `ErrNoPayloadCodec` from `errors.go`. **Update `errors_test.go:15`** (it lists the sentinel in
  `TestSentinels_WrapAndCompare` — removal breaks the build until edited).
- Strengthen the `resolveCodec` godoc: wire+no-codec→JSON is the deliberate, documented default (D4).
- **Tests:** existing codec/producer/consumer tests stay green; `errors_test` list updated.

### Task 3 — Untrusted-payload byte cap `WithMaxPayloadBytes` (ADR 0009 D5, finding 5)

- Add `WithMaxPayloadBytes[T](n)` (`flowcontrol.go`) → `maxPayloadBytes` on config/consumer. New sentinel
  **`ErrPayloadTooLarge`**; add it to `isPermanent` (`reliability.go`). In `consumer.decode`, wire path
  only (`!c.liveValue`): `if n > 0 && len(b) > n { return zero, ErrPayloadTooLarge }` **before**
  `codec.Decode`. Default `n <= 0` = unlimited.
- Godoc: untrusted wire sources SHOULD set this; complexity/nesting is codec-bounded (`encoding/json`).
- **Hot-path branches:** `n<=0` disabled; `n>0 && len<=n` pass; `n>0 && len>n` → `ErrPayloadTooLarge`
  → permanent → invalid sink.
- **Tests:** a `[]byte`-emitting `StreamingSource` double (not `LiveValueSource`) + `WithConsumerCodec`
  + `WithMaxPayloadBytes(small)`: over-size payload routed to the invalid sink (OnInvalidMessage,
  `errors.Is ErrPayloadTooLarge`); under-size processes normally; `n<=0` never caps.

### Task 4 — SPI plug-in panic isolation (ADR 0009 D1)

- Add consumer methods `safeLimiterWait`, `safeAllow`, `safeTryProbe`, `safeRecord`, `safeHalfOpen`,
  each `recover()`-wrapping the SPI call with the D1 fail-open fallback. **Panic is logged at ERROR,
  deduplicated per method** via a per-consumer `sync.Map` gate (id + method + recovered value; never the
  payload). Landmines: `safeLimiterWait` maps **panic→nil** but propagates a returned `ctx.Err()`;
  `safeHalfOpen`→`(nil,false)` and `admitBreaker` returns **true without parking** (never `select` on a
  nil channel).
- Rewire `admit` (limiter + `admitBreaker`) and `process` (dispatch gate + `Record`) through the wrappers.
- **Hot-path branches:** each fail-open fallback (panicking `Wait`→proceed; `Allow`→admit;
  `TryProbe`→admit; `Record`→swallow; `HalfOpen`→no-park-proceed); the dedup gate (first-logs / repeat-suppressed).
- **Tests:** selective-panicking `RateLimiter`/`CircuitBreaker`/`ProbeGate` doubles through a real `Run`
  over memory: no crash, message settles per fail-open, one ERROR log captured (injected `slog` handler);
  the `safeHalfOpen`-no-park case uses a scripted breaker (`Allow` returns `[false,true]`, `HalfOpen`
  panics) and asserts the message ultimately processes (no deadlock).

### Task 5 — Single-probe half-open via optional `ProbeGate` (ADR 0009 D2)

- Add `ProbeGate` interface (`TryProbe() bool`) to `flowcontrol.go` (opt-in, dispatch-only,
  `Record`-paired, consuming). Resolve `breaker.(ProbeGate)` once in `NewConsumer` → `probeGate` field;
  dispatch gate uses `safeTryProbe()` when set, else `safeAllow()`. Ingress `admitBreaker` **always**
  uses `Allow`.
- Default `breaker` (`breaker.go`) implements `TryProbe`: closed→true; open→false; half-open→single
  (`probeInFlight`). **`toHalfOpen` MUST unconditionally reset `probeInFlight=false`** (the wedge
  landmine); the half-open `Record` branches also clear it.
- One-time WARN in `Run` when `workers > 1` && breaker non-nil && not `ProbeGate` (gobreaker cliff).
- **Hot-path branches:** `TryProbe` closed/open/half-open-first/half-open-subsequent; `Record` clears
  `probeInFlight` on success/failure; `toHalfOpen` reset; dispatch with-ProbeGate vs fallback; the WARN
  fires (N>1+non-ProbeGate) / does not (default breaker, N=1).
- **Tests:** breaker-level blackbox (`NewCircuitBreaker()` asserted to `msgin.ProbeGate`):
  open→half-open→`TryProbe` true once / false subsequently; `Record(true)`→close, `Record(false)`→reopen.
  **The mandatory wedge test:** open → half-open → probe acquired → a straggler `Record(false)` reopens →
  straggler `Record` from open → advance to a new half-open cycle → assert `TryProbe` admits again (proves
  `toHalfOpen` reset). Fallback path proven by a non-`ProbeGate` double; the WARN by an injected logger.

## Whole-branch delivery gate (CLAUDE.md §5)

`/code-review` + `/security-review` over `main..HEAD`; resolve/triage every finding; `-race` suite
green (`-count` to shake fake-clock counts); `go vet`, `gofmt`, `CGO_ENABLED=0 build`, `go mod tidy`
(no-op), `go mod verify`, `govulncheck`, `golangci-lint` all clean; coverage ≥ 85% on changed files
with every listed hot-path branch covered. Consolidate to logical-feature commits (amend in-branch),
update `docs/HANDOVER.md` + memory, then fast-forward + push `main` (user-authorized).

## Traceability

Commits carry `Spec: 001`, `Plan: 004`, `ADR: 0009` trailers. This plan clears ADR 0008 D10 findings
1–4; findings 5–6 re-scoped to Plans 005 / 006+ (ADR 0009 D5).
