# Plan 026 — HTTP SSE Phase 4: S-in SSE client (`StreamingSource`)

> **For agentic workers:** REQUIRED SUB-SKILL: `superpowers:subagent-driven-development` (the project default) or
> `superpowers:executing-plans`. Steps use checkbox (`- [ ]`) syntax.
>
> **Go-skills hard rule (CLAUDE.md, restated because `writing-plans` omits it):** every task starts from
> **`cc-skills-golang:golang-how-to`** (here routing to: `golang-context` — the whole client is a cancellable
> loop — `golang-concurrency`, `golang-security`, `golang-error-handling`, `golang-safety`, `golang-testing`).
> **`superpowers:test-driven-development`** governs every task. **`gopls`** (via the `LSP` tool) for navigation and
> refactoring — not `grep`. Project-local **`table-test`** override (assert-closure form, `t.Context()`).
> `use-mockgen` / `use-testcontainers` do not apply — hermetic `httptest` only (Spec 011 §7).
>
> **This plan is deliberately thin** (Plan 024 precedent): signatures, names, invariants, branch coverage, commit
> boundaries — no embedded implementations. Write the code TDD-first from the tables below.

**Goal.** Ship Spec 011 Phase 4: `NewSSEClient(url, opts…)` — an `msgin.StreamingSource` that consumes an external
`text/event-stream` endpoint, emitting each event as a `Delivery`, with full-WHATWG response triage (C3),
auto-reconnect under a clamped exponential backoff, and best-effort `Last-Event-ID` resume.

**Architecture.** [ADR 0023 Addendum C](../adrs/0023-http-channel-adapter.md#addendum-c--sse-design-decisions-phases-34)
C3 + C6, over Plan 025's `SSEParser`. The remote server is **untrusted input** (the Phase 2 posture): no-follow
redirects by default (INV-1 class), redacted transport errors (INV-5 class), per-event byte cap, and no
remote-influenced value on a reserved `msgin.*` header key (INV-2 class). Delivery is **at-most-once, best-effort
resume**; `Ack`/`Nack` are no-ops (SSE has no ack protocol). **The SSE client resolves a *no-`Timeout`* default
`*http.Client`** — it must NOT inherit outbound's 30 s `defaultHTTPClientTimeout`, which interrupts body reads and
would force-abort every long-lived stream (audit MAJOR-1). **Dead-peer detection (audit F1):** because the reconnect
loop only fires *after* a connection ends with an error, a `Timeout: 0` client with no read deadline would let a
*silently* dead / half-open TCP connection block `Next()` forever. Three defenses, in order: (1) `ctx` cancel
always aborts; (2) the default transport's **TCP keepalive** (~30 s) surfaces a dead peer as a read error → the
common case self-heals; (3) an **opt-in `WithReadTimeout(d)`** (off by default, the C2-heartbeat-off symmetry) — a
per-read idle deadline whose expiry aborts the connection → reconnect, for callers who inject a keepalive-disabled
transport or want faster/guaranteed detection (INV-C7). No new dependency.

**Tech stack.** Go 1.25 (`GOTOOLCHAIN=go1.25.12`), root module, package `msghttp` in `adapter/http` (see the
placement decision below).

**Traceability.** Implements [Spec 011](../specs/011-http-adapter.md) §3.5 (S-in), §3.6, §4, §7; decided by
ADR 0023 Addendum C (C3, C6 + the C7 placement note this plan adds). Depends on **Plan 025 merged** (the
`SSEParser`, `HeaderSSEEventName`/`HeaderSSEEventID`, `WithMaxEventBytes`, `WithSSEClock`). Branch:
`feat/http-sse-client`, after 025's branch is merged and deleted.

---

## Two plan-level corrections (fold via Task 0; flagged for user review at plan approval)

1. **Placement: the client lives in `adapter/http` (package `msghttp`), not `adapter/http/stdlib` — Addendum B4's
   own logic, recorded as Addendum C7.** `stdlib` exists to bind framework-neutral cores to net/http **server**
   types; an HTTP **client** has no framework variant (gin has no SSE client), so a `stdlib` client would be the
   empty passthrough B4 rejected for O1/O2. Decisively: the client must reuse `validateURL`, `resolveClient`
   (the no-follow default — INV-C6) and `redactTransport` (INV-C2), all deliberately unexported in `msghttp` —
   a `stdlib` placement would force exporting security helpers or duplicating them.
2. **A `200` whose `Content-Type` is not `text/event-stream` is TERMINAL: `Stream` returns `ErrNotEventStream`**
   (amends the committed §3.5/C3 text, which said reconnect-with-backoff). WHATWG "fail the connection" is
   terminal; a misconfigured URL (pointing at a JSON API) should fail loud through `Consumer.Run`'s return value
   — the sentinel gets a real `errors.Is` surface (the "export what a test must assert" rule) instead of being
   observable only in logs. The **known trade-off to document**: a proxy that serves a `200 text/html` error page
   ends the source; the operator restarts it (the runtime owns give-up/restart policy). Conversely, **non-200
   statuses (≠204) reconnect-with-backoff** — a deliberate, documented deviation from WHATWG's fail-on-any-non-200
   (transient 5xx during deploys are the norm for a long-lived consumer; WHATWG wrote for browsers). **Documented
   asymmetry (audit MINOR-2):** a permanent wrong-URL that returns `404`/`301`/`410` therefore **loops silently
   forever** (reconnect-with-backoff), whereas `200`+wrong-Content-Type fails loud through `Consumer.Run` — the
   godoc + Task-0 spec fold must state plainly that a non-2xx misconfiguration is only visible in the WARN log, not
   the return value, so operators know a 404 does not terminate the source.

## Baseline — verified while writing this plan (post-Plan-025 items marked ◆ = re-verify at execution)

| Claim | Status | Citation |
|---|---|---|
| `msgin.StreamingSource` = `Stream(ctx, out chan<- Delivery) error`; `Consumer.Run` returns `Stream`'s error | VERIFIED | `spi.go:37`, `consumer.go:305` |
| `Delivery{Msg, Ack func(ctx) error, Nack func(ctx, bool, time.Duration) error}` | VERIFIED | `spi.go:10` |
| `validateURL` / `resolveClient` / `redactTransport` / `drainBounded` unexported in `msghttp` | VERIFIED | `adapter/http/outbound.go` |
| `resolveClient` yields a client with the **30 s `defaultHTTPClientTimeout`** when unset (`NewConfig` back-fills `cfg.httpClient`, no set-flag) — **must NOT be reused for SSE** (MAJOR-1); the client needs its own `resolveSSEClient` | VERIFIED | `options.go:42,604-605`, `outbound.go:118-119` |
| `msgin.New[any](payload)` mints fresh id+timestamp; `WithHeader` returns a copy | VERIFIED | `message.go:156,213` |
| ◆ `SSEParser.Next/RetryDelay/LastEventID`, `HeaderSSEEventName`, `HeaderSSEEventID`, `WithMaxEventBytes`, `WithSSEClock` | Plan 025 | 025 surface block |
| ◆ `SSEServer` + `WithReplayBuffer` (the Task 4 e2e resume peer) | Plan 025 | 025 surface block |
| Parser line-splitting on CR/LF + NUL-id-ignore ⇒ a captured `LastEventID` can never contain CR/LF/NUL | Plan 025 Task 2 rules | (INV-C2's header-injection proof) |

## Global Constraints

Identical to Plan 025's block (Go 1.25 via `GOTOOLCHAIN=go1.25.12`; no new dependency; blackbox `package
msghttp_test` only; assert-closure tables + `t.Context()`; hermetic `httptest` only; no per-test `goleak.VerifyNone`
— TestMain covers it; fake clock via `WithSSEClock` + `BlockUntilContext`, **no real sleeps**; no
`t.Fatal` off the test goroutine; every task a green `-race` commit with `Spec: 011` / `Plan: 026` / `ADR: 0023`
trailers; **100.0% coverage is the Task 4 delivery gate**, unreachable arms are deleted, not `//nolint`ed).

---

## Security invariants — each needs a test that fails if the invariant is removed

**INV-C1 — Response triage is exactly:** `200` + media type `text/event-stream` (via `mime.ParseMediaType`;
parameters like `charset=utf-8` allowed) → stream; **`204`** → drain-bounded, close, **return nil** (the server's
terminal stop); **`200` + any other/missing media type** → drain-bounded, close, **return `ErrNotEventStream`**
(terminal — correction 2); **any other status** (including 3xx, which the no-follow client surfaces as a
response) → drain-bounded, close, WARN log, backoff, reconnect. A non-event-stream body is **never** handed to the
parser. *Verify:* one `httptest.Server` per arm; the reconnect arm asserts a second connection attempt occurs
(after a fake-clock advance) and the terminal arms assert `Stream` returned with **no** second attempt.

**INV-C2 — No remote-influenced value lands on a reserved `msgin.*` header key, in any error string unredacted, or
in our own request headers uninspected.** *Realized by:* event `Name` → `HeaderSSEEventName` (`http.sse-event`),
event `ID` → `HeaderSSEEventID` (`http.sse-event-id`) — never `msgin.message-id`; the emitted message's own
id/timestamp are freshly minted by `msgin.New`; transport failures wrapped by `redactTransport` (no URL/userinfo/
query — INV-5); status-arm WARN logs carry the status code only, never body bytes; the `Last-Event-ID` echo is
header-safe **by construction** (parser line-splitting strips CR/LF; NUL ids are ignored per WHATWG). *Verify:*
a hostile server emitting `event`/`id` values and a body on a 500; assert the delivery's header keys, that
`msgin.HeaderMessageID ≠` the remote id, the redacted transport error, and a reconnect request whose
`Last-Event-ID` round-trips a `"weird but line-safe"` id verbatim.

**INV-C3 — Memory per connection is bounded by the per-event cap.** An oversized event surfaces `ErrEventTooLarge`
from the parser → treated as a mid-stream fault: close, backoff, reconnect (never OOM, never terminal — the next
event may be fine). *Verify:* server sends an over-cap event (small `WithMaxEventBytes`), then on reconnect a
valid one; assert the valid one is delivered.

**INV-C4 — Two connection attempts are never closer than the current backoff delay, and the delay never falls
below `min` — under `retry: 0`, instant EOF, instant refusal, or any combination (no hot loop).** *Realized by:*
delay starts at `min`; each failed/ended connection waits `delay` on the fake clock then doubles (capped at
`max`); a valid `retry: R` **replaces** the next delay with `clamp(R, min, max)` (doubling resumes from there); a
connection that yielded ≥1 event resets the delay to `min`. *Verify (instrumented — the measure-interleaving
rule):* fake clock; a server that EOFs instantly; assert attempt N+1 requires an explicit advance of exactly the
expected delay (attempt count observed via server hit counter — the test FAILS if an attempt occurs without its
advance); table over: doubling sequence min→max cap, `retry: 0` → clamped to `min`, `retry:` huge → clamped to
`max`, reset-after-event.

**INV-C5 — `Stream` honors ctx everywhere:** cancel during connect, during a blocked read, during a backoff wait,
and during a blocked emit (`out` full) each return promptly with `ctx.Err()`; the in-flight request is aborted
(the request carries ctx); no goroutine remains (TestMain goleak); nothing is sent on `out` after return.
*Verify:* one test per arm; the blocked-read arm uses a server that writes headers then holds the connection open.

**INV-C6 — No response may steer a request to another host unless the caller opted in** (INV-1 class, reused
machinery): the resolved client has the no-follow `CheckRedirect`; a `302` is triaged as "other status" →
reconnect **to the configured URL**, never followed. *Verify:* redirecting server + target server; target records
zero hits; `WithFollowRedirects()` flips it (one arm suffices — the mechanism is Plan 024's, already proven).

**INV-C7 — A silently-dead connection cannot wedge `Stream` forever (audit F1 — the client dual of the server's
INV-S8).** The reconnect loop fires only on a connection that *ends with an error*, so a `Timeout: 0` client on a
half-open socket that never errors would block `Next()` indefinitely. *Realized by:* (1) `ctx` (the **parent**)
cancel always aborts terminally (INV-C5); (2) the **default `*http.Client` uses `http.DefaultTransport`**, whose
`net.Dialer{KeepAlive: 30s}` surfaces a dead peer as a read error → reconnect — document this reliance, and that it
*evaporates* if a caller injects a transport with keepalive disabled; (3) opt-in **`WithReadTimeout(d)`** (default
**off**; `d <= 0` explicit → `ErrInvalidReadTimeout`) installs a per-connection idle watchdog.

**Watchdog mechanism (pinned — F1/F2/F3/F6):**
- **A per-connection derived ctx `cctx` (fresh each loop iteration)** scopes ONLY the request / `client.Do` / body
  read. `cctx = context.WithCancel(ctx)`; `defer ccancel()` per iteration.
- **`time.AfterFunc(d, ccancel)`** — the watchdog action IS a cancel, so use `AfterFunc(d, ccancel)`, not a
  `Timer`+`select`. Arm it **before `client.Do`** (F6 — so a peer that completes the TCP handshake but never sends
  headers is also reaped, making detection *guaranteed*, not post-header-only); `Reset(d)` it when headers arrive
  and after **every underlying `resp.Body.Read` that returns `n > 0`**, via a `readTimeoutReader` wrapping
  `resp.Body` that is what `NewSSEParser` reads from — **reset per byte-yielding `Read`, NOT per parser event**
  (F2: an SSE server keeps a connection alive with `: ping\n` *comment* heartbeats that produce reads but no
  events; a reset-on-event watchdog would false-reap a healthy comment-only stream). `Stop` the timer on every
  connection-exit path. The timer is **real physical-time** (it must abort a real socket read; it cannot use
  `cfg.sseClock` — document the split, as with the server's `WithWriteTimeout`).
- **Classification (F1 — the catastrophic seam):** a read error, a watchdog fire, and TCP-keepalive death are ALL
  *reconnectable* → route every read/connection error to the backoff arm. **Terminal (`Stream` returns
  `ctx.Err()`) iff the PARENT `ctx` is done** — never off `cctx` (which the watchdog also cancels, so `cctx.Err()`
  cannot distinguish a watchdog reap from a real shutdown). The backoff wait, the emit `select`, and the
  terminal-vs-reconnect decision select **exclusively on the parent `ctx`**.
- No goroutine leak: `context.WithCancel` spawns none; a pending/`Stop`ped `AfterFunc` is a heap timer, not a
  goroutine (goleak TestMain is the backstop).
- **Godoc note (NIT):** the watchdog measures *read* idleness, which a blocked emit (a slow downstream consumer
  holding `out` full) also stalls — so a too-small `d` can force an unnecessary post-stall reconnect (benign under
  `Last-Event-ID` resume). Document that `WithReadTimeout` should exceed the plausible consumer-side stall.

*Verify (real short timeout):* (a) `WithReadTimeout(<short>)` + a server that writes headers then goes silent → the
client reconnects (attempt 2 observed) via the *reconnect* path, `Stream` does **not** return; (b) **reset-on-read
proof (F2):** a server that, after headers, emits only `: ping\n` comments at an interval `< d` (reads, no events)
→ **no** reconnect across several `d` (fails on a reset-on-event impl); (c) **off by default** → the same silent
server does *not* trigger a read-timeout reconnect (only keepalive/ctx would); (d) a parent-`ctx` cancel while a
read is blocked → `Stream` returns `ctx.Err()` **terminally** (not a reconnect — the F1 classification).

## Multi-instance topology (CLAUDE.md rule — godoc obligations)

`NewSSEClient` godoc: delivery is **at-most-once, best-effort resume** — resume works only against a server that
honors `Last-Event-ID` (e.g. this package's `SSEServer` with `WithReplayBuffer`), and the resume position is held
**in memory per `Stream` call**: a restarted process resumes from the live stream, not from the last-seen id.
`NativeRedelivery() = false` (resume is not a redelivery guarantee), `NativeDeadLetter() = false`. `Ack`/`Nack`
are no-ops. Competing consumers do not apply (each client receives the full stream).

---

## Final exported surface (the API contract this plan commits to)

```go
// adapter/http (package msghttp) — sseclient.go
type SSEClient struct{ /* unexported; no mutable state — resume position is Stream-local */ }
func NewSSEClient(url string, opts ...Option) (*SSEClient, error) // ErrEmptyURL / ErrInvalidURL / option errors
func (c *SSEClient) Stream(ctx context.Context, out chan<- msgin.Delivery) error
func (c *SSEClient) NativeRedelivery() bool // false
func (c *SSEClient) NativeDeadLetter() bool // false
var _ msgin.StreamingSource  = (*SSEClient)(nil)
var _ msgin.NativeReliability = (*SSEClient)(nil)

// adapter/http (package msghttp) — options.go additions
func WithConnectHeaders(h http.Header) Option            // cloned (h.Clone()); client-owned Last-Event-ID, Accept,
                                                         // Cache-Control dropped case-insensitively; nil = no-op
func WithReconnectBackoff(min, max time.Duration) Option // default 500ms → 30s; explicit min<=0 or max<min →
                                                         // ErrInvalidReconnectBackoff (set-flag pattern)
func WithReadTimeout(d time.Duration) Option             // default off; explicit d<=0 → ErrInvalidReadTimeout;
                                                         // per-read idle deadline → reconnect (INV-C7, dead-peer)

// adapter/http (package msghttp) — errors.go additions (3)
ErrInvalidReconnectBackoff, ErrNotEventStream, ErrInvalidReadTimeout
```

Constants: `defaultReconnectMin = 500 * time.Millisecond`, `defaultReconnectMax = 30 * time.Second` — godoc'd
rationale: min bounds the hot-loop floor (no server can push attempts below it — C3's clamp), max bounds how long
an outage delays recovery; both overridable. Request headers the client always sets: `Accept: text/event-stream`,
`Cache-Control: no-cache`, plus `Last-Event-ID` when held. `WithConnectHeaders` is trusted operator config
(the `WithResponseHeaders` CAUTION precedent) — values are not sanitized; net/http's transport rejects invalid
header values at request time.

**Client resolution — `resolveSSEClient(cfg)`, distinct from outbound's `resolveClient` (MAJOR-1).** The SSE client
must NOT carry a finite `http.Client.Timeout` (it interrupts streaming body reads). Mechanism: add an
`httpClientSet` flag, set **only inside `WithHTTPClient`'s existing `if c != nil` guard** (audit F3 — setting it
unconditionally would make `WithHTTPClient(nil)` flag a nil client, letting `NewConfig`'s 30 s back-fill leak back
in and silently break the documented nil-is-no-op contract). When the caller supplied a non-nil client
(`httpClientSet`) → shallow-copy it and install the no-follow `CheckRedirect` (honoring `WithFollowRedirects` —
INV-C6), keeping the caller's `Timeout`; when unset (incl. `WithHTTPClient(nil)`) → build a fresh `&http.Client{}`
(`Timeout: 0`, `http.DefaultTransport` → TCP keepalive for dead-peer detection, INV-C7) + the no-follow
`CheckRedirect`. This leaves outbound's `resolveClient`/`NewConfig` 30 s default untouched (SSE keys off the flag
and ignores the back-filled default). `WithHTTPClient`'s godoc gains an SSE note: **a client with a finite
`Timeout` breaks streaming — leave it 0 for SSE; keep TCP keepalive enabled (or set `WithReadTimeout`) so a dead
peer is detected.**

**Stream loop (normative):** each iteration derives a **fresh per-connection `cctx, ccancel = context.WithCancel(ctx)`**
(`defer ccancel()`) that scopes ONLY this connection's request/read; when `WithReadTimeout(d)` is set, arm
`w := time.AfterFunc(d, ccancel)` **before** `client.Do` (F6) and `defer w.Stop()`. Resolve request on `cctx` →
`client.Do` → transport error ⇒ `redactTransport`-wrapped WARN log, backoff arm; else triage per INV-C1 → on
stream: `w.Reset(d)` (headers arrived); read via a `readTimeoutReader` wrapping `resp.Body` that calls `w.Reset(d)`
after each `Read` returning `n > 0` (F2 — per read, NOT per event); `NewSSEParser(readTimeoutReader, …)` loop
`Next()`: event ⇒ build `msgin.New[any](ev.Data)` (+ `HeaderSSEEventName`/`HeaderSSEEventID` headers when
non-empty), no-op `Ack`/`Nack`, emit via `select` on `out <-` / **`ctx.Done()`** (the parent — a blocked emit is a
terminal-cancellable point, not a per-connection one); `io.EOF`/`io.ErrUnexpectedEOF`/`ErrEventTooLarge`/read error
(incl. a watchdog-cancelled read) ⇒ **backoff arm — every connection-end error is reconnectable**. After every
connection: capture `parser.LastEventID()` (persists across reconnects within this `Stream` call) and
`parser.RetryDelay()` (clamped, replaces next delay). **Terminal decision (F1):** `Stream` returns `ctx.Err()`
**iff the PARENT `ctx` is done** — checked in the backoff `select` (`cfg.sseClock` timer vs **`ctx.Done()`**, never
`cctx.Done()`, which the watchdog also fires). `resp.Body` closed on **every** path (`defer` per connection
iteration).

---

## Tasks

### Task 0 — artifact delta, committed ahead of the code

**Files:** `docs/specs/011-http-adapter.md`, `docs/adrs/0023-http-channel-adapter.md`.

- [ ] Spec 011: §3.0 layout — S-in moves to `adapter/http/sseclient.go` (correction 1); §3.5 S-in — wrong-CT is
      terminal `ErrNotEventStream`, non-200 (≠204) reconnects (correction 2, with the proxy-trade-off sentence
      **and the MINOR-2 asymmetry note: a non-2xx wrong-URL loops silently, visible only in the WARN log**); the
      SSE client resolves a **no-`Timeout` default `*http.Client`** (audit MAJOR-1 — correct the shipped S-in row's
      "caller injects the `*http.Client`" to add "and the default carries no `Timeout`; a finite one breaks
      streaming") **with the dead-peer story: TCP keepalive by default + opt-in `WithReadTimeout` (F1/INV-C7)**;
      resume position is per-`Stream`-call memory (multi-instance text above). §3.6: the client sentinel set is
      **3** — `ErrInvalidReconnectBackoff`, `ErrNotEventStream`, `ErrInvalidReadTimeout` (bringing the Phases-3/4
      total to **13**, reconciling with Plan 025 Task 0's count).
- [ ] ADR 0023 **Addendum C7 — S-in placement** (mirrors B4, two paragraphs: Decision + Why citing the unexported
      security helpers) and a one-line amendment under **C3** for the terminal wrong-CT arm, **plus a dated
      audit-rounds-1–3 note under C3 for: the no-`Timeout` client default (MAJOR-1), the non-2xx-loops-silently
      asymmetry (MINOR-2), and the dead-peer detection / opt-in `WithReadTimeout` (F1)**.
- [ ] Commit: `docs(http): Phase 4 plan-time corrections and audit fixes — client placement (C7), terminal ErrNotEventStream, no-Timeout client, read-timeout`
      Trailers: `Spec: 011` / `Plan: 026` / `ADR: 0023`.

### Task 1 — client option surface + constructor

**Files:** modify `adapter/http/options.go` (`connectHeaders http.Header`; `reconnectMin/reconnectMax` +
set-flag; **`httpClientSet` flag set inside `WithHTTPClient`'s `if c != nil` guard — MAJOR-1/F3**; `readTimeout` +
set-flag; the three `WithX` options — `WithConnectHeaders`, `WithReconnectBackoff`, `WithReadTimeout`; `NewConfig`
validation), `adapter/http/errors.go` (the three sentinels); create `adapter/http/sseclient.go` (`SSEClient` type +
`NewSSEClient` + the two `NativeReliability` methods + interface assertions — `Stream` returns a
not-yet-implemented panic? **No**: `Stream` lands in Task 2; until then the file declares the type, constructor and
reliability methods only, and the `StreamingSource` assertion is added in Task 2), `adapter/http/sseclient_test.go`.
Also add the `resolveSSEClient(cfg)` helper (unexported; the no-`Timeout` resolution — MAJOR-1) and the SSE note on
`WithHTTPClient`'s godoc.

**Interfaces — Produces:** `NewSSEClient` (validated), `WithConnectHeaders`, `WithReconnectBackoff`,
`WithReadTimeout` + sentinels, `resolveSSEClient`. **Consumes:** `validateURL`, the no-follow `CheckRedirect`
machinery (Plan 024), `NewConfig` (**not** `resolveClient` — it carries the 30 s default; MAJOR-1).

**Hot-path branches (Task 1):**

| # | Branch | Test |
|---|---|---|
| 1 | `NewSSEClient("")` / whitespace → `ErrEmptyURL`; bad scheme/parse/empty-host → `ErrInvalidURL` | construction table (the Plan 024 class) |
| 2 | `WithReconnectBackoff(0, x)`, `(-1, x)`, `(x, x-1)` → `ErrInvalidReconnectBackoff`; `(1ns, 1ns)` accepted | construction table |
| 2b | `WithReadTimeout(0)` / `(-1)` → `ErrInvalidReadTimeout`; `(1ns)` accepted; unset → off (INV-C7) | construction table |
| 3 | unset backoff → defaults (behaviorally proven in Task 2's pacing table — deferral note) | — |
| 4 | `WithConnectHeaders(nil)` → no-op; reserved names dropped case-insensitively (`last-event-id`, `ACCEPT`, `Cache-Control`); clone proven (caller mutation after construction has no effect) | Task 2 e2e asserts the actually-sent headers (deferral note — not observable at Task 1) |
| 5 | `NativeRedelivery()` / `NativeDeadLetter()` → false, false | unit |
| 6 | `resolveSSEClient`: caller-supplied client (`WithHTTPClient`) → same client (copy) with no-follow, caller `Timeout` kept; unset **and `WithHTTPClient(nil)` (F3)** → fresh `Timeout: 0` client with no-follow (nil-is-no-op contract preserved — the flag is set only inside the `c != nil` guard). **Behaviorally**: an injected `WithHTTPClient(&http.Client{Timeout: <short>})` force-reconnects a held-open stream (Timeout honored — Task 2 e2e); the *default is 0* is correctness-by-construction — a fast blackbox test cannot distinguish 0 from 30 s (both hold a sub-30 s stream), so it is guarded by construction + the Task-4 gate mutation (set the default to a finite Timeout → the long-stream/injected-timeout test must go RED), documented as a known testability limit | Task 2 e2e (deferral) + gate mutation |

- [ ] Red → green (rows 1, 2, 2b, 5 assertable now; 3–4, 6 carry the Plan 024 deferral note in the test comment).
- [ ] Commit: `feat(http): SSE client option surface and constructor validation`
      Trailers: `Spec: 011` / `Plan: 026` / `ADR: 0023`.

### Task 2 — `Stream`: triage, emission, reconnect, resume

**Files:** modify `adapter/http/sseclient.go` (+`sseclient_test.go`).

**Interfaces — Consumes:** `SSEParser` (`Next`/`RetryDelay`/`LastEventID`), `redactTransport`, `drainBounded`,
`cfg.sseClock`. **Produces:** the full `Stream` contract + `var _ msgin.StreamingSource` assertion.

Normative behavior: the Stream-loop block above. Every arm below is a hot-path branch:

| # | Branch | Test |
|---|---|---|
| 1 | 200 + `text/event-stream` (+ `;charset=utf-8` variant) → events emitted as Deliveries; payload/headers exact; fresh `msg.ID()` ≠ remote id | e2e table |
| 2 | 204 → `Stream` returns nil, no reconnect (INV-C1) | e2e |
| 3 | 200 + `application/json` / missing CT → `ErrNotEventStream`, no reconnect | e2e table |
| 4 | 500 (and 404) → WARN (code only), reconnect after backoff advance | e2e + fake clock |
| 5 | transport refusal (closed port) → redacted wrap logged, reconnect | e2e |
| 6 | mid-stream EOF → reconnect; `Last-Event-ID` header on attempt 2 = last seen id | e2e |
| 7 | empty `id:` line mid-stream → subsequent reconnect sends **no** `Last-Event-ID` (cleared, C3/WHATWG) | e2e |
| 8 | `retry: R` honored: next delay = clamp(R) — `retry: 0` → min; huge → max (INV-C4 rows) | fake-clock table |
| 9 | doubling min→…→max cap; reset to min after a connection with ≥1 event | fake-clock table |
| 10 | `Ack`/`Nack` no-ops return nil | unit on an emitted Delivery |
| 11 | connect headers actually sent; reserved ones absent; clone proven (Task 1 rows 3–4 discharge) | e2e recording server |
| 12 | **no-`Timeout` default (MAJOR-1):** default client streams events past a real-time margin without the request being aborted; an injected `WithHTTPClient(&http.Client{Timeout: <short>})` force-reconnects the same held-open stream (Timeout honored ⇒ a nonzero default would break streaming) — discharges Task 1 row 6 | e2e, short real-time margin |

- [ ] Red → green per the table (the fake-clock pacing rows are the INV-C4 instrumented test — attempt counter +
      exact advances, failing on any un-advanced attempt).
- [ ] Commit: `feat(http): SSE client Stream — WHATWG triage, clamped reconnect, Last-Event-ID resume`
      Trailers: `Spec: 011` / `Plan: 026` / `ADR: 0023`.

### Task 3 — hardening: cancellation, bounds, redaction

**Files:** modify `adapter/http/sseclient.go` (+`sseclient_test.go`) — expected mostly test additions; fix what
they catch.

| # | Branch / invariant | Test |
|---|---|---|
| 1 | ctx cancel during blocked read → prompt `ctx.Err()` return, request aborted (INV-C5) | header-then-hang server |
| 2 | ctx cancel during backoff wait → prompt return | fake clock (no advance needed) |
| 3 | ctx cancel while emit blocked (`out` unbuffered, no reader) → prompt return, nothing sent after | e2e |
| 4 | over-cap event → `ErrEventTooLarge` handled as reconnect; next connection's valid event delivered (INV-C3) | e2e |
| 5 | hostile `event`/`id` values → land only on `http.sse-*` keys; 500-body bytes appear in no error/log string (INV-C2) | e2e + capturing handler |
| 6 | 302 → target zero hits, reconnect to configured URL; `WithFollowRedirects()` follows (INV-C6) | two-server e2e |
| 7 | `WithReadTimeout(<short>)` + server that writes headers then goes silent → aborted on the idle deadline, client **reconnects** (attempt 2), `Stream` does not return; **off by default** → same silent server does NOT read-timeout-reconnect (INV-C7 arms a+c) | real-short-timeout e2e |
| 7b | **reset-on-read (F2):** server emits only `: ping\n` comments at interval `< d` (reads, no events) → **no** reconnect across several `d` (fails on a reset-on-event impl) | real-short-timeout e2e |
| 7c | **F1 classification:** parent-`ctx` cancel during a blocked read → `Stream` returns `ctx.Err()` **terminally** (not a reconnect); a watchdog fire under the same blocked read → **reconnect** (contrast proves the terminal decision keys off the parent ctx, not `cctx`) | real-short-timeout e2e, both arms |
| 7d | **F6 pre-header:** `WithReadTimeout(<short>)` + peer that accepts the connection but never sends headers → watchdog (armed before `client.Do`) aborts and reconnects | real-short-timeout e2e |

- [ ] Red → green; commit: `feat(http): SSE client hardening — cancellation, caps, redaction, no-follow, read-timeout`
      Trailers: `Spec: 011` / `Plan: 026` / `ADR: 0023`.

### Task 4 — cross-phase e2e, example, docs, delivery gate

**Files:** create `adapter/http/example_sse_client_test.go` (`ExampleNewSSEClient` — against a tiny
`httptest.Server` emitting fixed events, deterministic `// Output:`); modify `adapter/http/doc.go` (S-in
paragraph: triage contract, at-most-once + resume caveat, multi-instance godoc text); test additions in
`adapter/http/stdlib/sse_test.go` or a new `adapter/http/stdlib/sse_e2e_test.go`.

- [ ] **The resume e2e the two plans exist for:** `SSEServer` (`WithReplayBuffer(n)`) ↔ `SSEClient` over
      `httptest.Server` — client receives `1..k`, connection killed server-side, client reconnects (fake-clock
      advance) with `Last-Event-ID: k`, receives exactly `k+1..m` contiguous (INV-S5 × INV-C4 end to end). Also
      the negative: replay off → post-reconnect events only.
- [ ] `Consumer.Run` integration smoke: `NewConsumer` over the `SSEClient` (the first *networked/untrusted-remote*
      `StreamingSource` wiring — `adapter/cron/Source` is an earlier in-process one; audit MINOR-4) — events reach
      a handler; a 204 ends `Run` with nil.
- [ ] Gate, in order (CLAUDE.md §5): `/code-review` → fix; `/security-review` (untrusted-remote surfaces:
      unbounded event OOM, hostile `retry:`, redirect SSRF, header/error hygiene) → fix; coverage 100.0% both
      packages; **mutation spot-checks**: remove the clamp (INV-C4 test must go RED), route remote id to
      `msgin.message-id` (INV-C2 RED), make wrong-CT reconnect (INV-C1 RED), **give the default client a finite
      `Timeout` (the MAJOR-1 injected-timeout/long-stream test must go RED)**, **disarm the read-idle watchdog (the
      INV-C7 read-timeout-on test must go RED)**; `go test ./... -race` green.
- [ ] Commit: `docs(http): SSE client example, docs, and the Phase 4 gate record`
      Trailers: `Spec: 011` / `Plan: 026` / `ADR: 0023`. Then whole-branch review + merge per CLAUDE.md
      (approval-gated; delete `feat/http-sse-client` after merge).

---

## Open decisions — none. The two Task-0 corrections plus audit-rounds-1–3 findings (MAJOR-1, MINOR-2/4, F1, F2,
F3, F6, NIT) are folded above and via Task 0; the design forks (no-`Timeout` default client; opt-in `WithReadTimeout`
for dead-peer detection) were user-approved 2026-07-23. Three adversarial audit rounds complete
(APPROVE-WITH-FIXES → all folded); the F1 watchdog mechanism is pinned (parent-ctx classification, reset-on-read,
per-iteration scoping, arm-before-`Do`).
