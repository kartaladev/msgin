# Spec 011 — HTTP channel adapter (inbound + outbound + SSE, stdlib & gin bindings)

- **Status:** Draft (2026-07-21) — brainstormed with the user; scope, phasing, and the dominant design forks settled
  interactively (see "Decisions settled with the user"). ADR 0023 (HTTP adapter architecture) authored; ADR 0024 (gin
  dependency) deferred to Phase 5; Plan 020 (Phase 1) authored, Plans 021–024 to follow per phase. **Phase-1
  adversarial design audit round 1 folded** (Opus, SOUND-WITH-NITS): **H1** the request→message path uses `msgin.New`
  (not `NewMessage`) — a spec-only error that would have failed every I2 request with 400, corrected in §3.2; **M1**
  I2 success status pinned to `200`, `WithSuccessStatus` scoped to I1 only (§3.3); **M4** the inbound-payload-is-`[]byte`
  (no inbound codec seam) constraint documented symmetrically with the reply constraint (§3.2); **L1** the I1
  `DirectChannel`-is-synchronous / `reqCtx`-coupling caveat + `QueueChannel` steer (§3.3); **L2** the error→status
  mapping made honest (`ErrNoCorrelation`→500 server-fault, `ErrDuplicateCorrelation`→409; §3.3); **L4** response bytes
  extracted before headers are written. Architecture verified SOUND against the code (SPI reuse, Return-Address-by-
  construction, every load-bearing API); no re-audit round warranted. Plan-level fixes (M2/M3 test coverage) folded
  into Plan 020.
- **Decisions settled with the user (2026-07-21):**
  - **One spec, phased plans.** This spec covers the whole HTTP surface; it is realized **phase by phase**, one plan
    and one merged increment per phase, mirroring the Spec 009 → Plans 015–018 precedent. Phase order is fixed
    (§6): **1** inbound server → **2** outbound client → **3** SSE server → **4** SSE client → **5** gin binding.
  - **Both SSE directions.** An SSE **server** (msgin serves `text/event-stream`, streams channel messages to
    connected subscribers) *and* an SSE **client** (msgin consumes an external SSE stream as a `StreamingSource`).
  - **Framework-agnostic core + separate gin module.** The stdlib (`net/http`) binding lives at `adapter/http/stdlib`
    and the gin binding at `adapter/http/gin`; **all real logic** (request⇄`Message` encode/decode, the request-reply
    core, SSE framing) lives in the framework-agnostic **parent package `adapter/http`** (root module, stdlib-only),
    which both bindings import. `gin` is a third-party dependency and **cannot** live in the root module (Dependency
    policy) — `adapter/http/gin` is therefore its **own Go module** (the pgx/redis/nats precedent), so only gin users
    pay for it. The rejected alternatives (stdlib-only + `gin.WrapH`; a bespoke framework-abstraction interface) are
    recorded in ADR 0023.
  - **Return Address is satisfied by construction for synchronous HTTP request-reply** (§3.4, ADR 0023). The reply to
    an outbound `POST` returns on the **same TCP connection** held by the sending instance, so the multi-instance
    cross-process correlation problem the `RequestReplyExchange` SPI was designed to expose (CLAUDE.md
    multi-instance rule; Spec 010 §8.1 / ADR 0022) does **not** arise for the synchronous variant. The async-callback
    variant (reply arrives later via an inbound webhook), which *would* require an instance-addressable callback URL as
    the Return Address, is **out of scope for v1** and named as the future increment.
- **Governing product spec:** [Spec 001 — Messaging core](001-messaging-core.md) §9, which names **HTTP**
  (`adapter/http`, core module) among the six shipped adapters — "sync request-reply / async / outbound webhook".
  This spec realizes that adapter and **extends** it with SSE (server + client) and the dual stdlib/gin binding, both
  beyond Spec 001's original HTTP scope.
- **Builds on:**
  - [ADR 0002 — Adapter SPI](../adrs/0002-adapter-spi.md) — the `PollingSource`/`StreamingSource` inbound seam,
    `OutboundAdapter` outbound seam, `Delivery` (`Ack`/`Nack`), `NativeReliability`, and the **runtime-owned
    reliability** boundary (adapters classify; the `Consumer`/`Producer`/`RetryPolicy` decide). The SSE client is a
    `StreamingSource`; O1 webhook is an `OutboundAdapter`.
  - [ADR 0022 — Messaging Gateway / `RequestReplyExchange`](../adrs/0022-messaging-gateway.md) — the SPI this adapter
    plugs into on **both** sides: **I2** (inbound sync gateway) is an `http.Handler` over any `RequestReplyExchange`
    (so a `ChannelExchange` gives HTTP-in → in-process request/reply → HTTP-out for free); **O2** (outbound
    request-reply) **is** a `RequestReplyExchange`, dropping into `Gateway`/`OutboundGateway` (Plan 019) unchanged.
  - [ADR 0001 — Payload typing](../adrs/0001-message-payload-typing.md) — adapters see `Message[any]` whose payload is
    `[]byte`; the typed runtime owns the `PayloadCodec[T]`. HTTP bodies are `[]byte` at the adapter boundary; header
    framing (HTTP header ⇄ `Headers`) is the type-agnostic adapter concern.
  - [ADR 0003 — Multi-module layout](../adrs/0003-multi-module-repository-layout.md) — `adapter/http` +
    `adapter/http/stdlib` are root-module packages (stdlib-only); `adapter/http/gin` is a separate module with its own
    tag line and `go.work` entry, exactly like `database/pgx`.
  - [ADR 0006 — Resilience & flow control](../adrs/0006-resilience-flow-control.md) — inbound handlers honor request
    `context` cancellation; the SSE server/client obey graceful-shutdown and no-goroutine-leak constraints.
- **New ADRs (to be authored):**
  - **ADR 0023 — HTTP channel adapter architecture:** the framework-agnostic-core + stdlib/gin split, the per-mode SPI
    mapping, the **Return-Address-by-construction** reasoning for synchronous HTTP request-reply and the multi-instance
    boundary, the outbound response→error classification (`4xx = Permanent`), and the per-mode delivery guarantees.
  - **ADR 0024 — `gin` dependency (isolated module):** justifies adding `github.com/gin-gonic/gin` as a direct
    dependency of the **`adapter/http/gin` module only**, following the Dependency-policy rule that every dependency is
    ADR-justified and the heavy-client-adapter-as-separate-module precedent (ADR 0003). Authored with Plan 024 (Phase 5).

## 1. Motivation

Every adapter msgin ships so far bridges to a **broker or store** (in-memory, SQL, and — designed — pgx/redis/nats) or
a **clock** (cron). None bridges to the single most common integration surface: **HTTP**. Real message flows are
triggered by inbound HTTP requests and must call out over HTTP; browsers and services increasingly consume **live
event streams** over Server-Sent Events. This spec adds the HTTP channel adapter in both directions, plus SSE in both
directions, and exposes each through **two idiomatic bindings** — the stdlib `net/http` handler and the `gin`
framework — without duplicating logic or forcing a web-framework dependency on any consumer who does not want it.

Crucially, HTTP is where the **`RequestReplyExchange` SPI** shipped in Plan 019 pays off: the inbound sync gateway and
the outbound request-reply exchange are the concrete external transport that SPI was built to receive, so most of this
adapter is *wiring existing core seams to `net/http`*, not new core machinery. The core does **not** change.

## 2. Goals / Non-goals

**Goals**

- `adapter/http` (root module, stdlib-only, **zero new dependency**): a framework-agnostic core plus a `net/http`
  binding covering six modes — **I1** async inbound, **I2** sync inbound gateway, **O1** outbound webhook, **O2**
  outbound request-reply, **S-out** SSE server, **S-in** SSE client.
- `adapter/http/gin` (separate module): idiomatic gin bindings for the **server-side** modes (I1, I2, S-out), reusing
  the core; a `RegisterRoutes` convenience.
- Each mode plugs into an **existing** core SPI (table §3); the pattern core is untouched (Open/Closed).
- Every mode's **delivery guarantee** is documented (§5); every default is production-safe and overridable (`WithX`).
- Inbound is treated as the **untrusted boundary** — body caps, header allow-listing, reserved-header stripping,
  correlation-id distrust — and gets a dedicated `/security-review` in Phases 1 and 3 (§4).
- No goroutine leaks; graceful shutdown for the long-lived SSE connections; `goleak` in every package. Fully hermetic
  tests via `httptest` (no testcontainers).

**Non-goals**

- **Async-callback request-reply** (fire request now, reply later via an inbound webhook to an instance-addressable
  callback URL). This is the *only* HTTP request-reply shape that needs an explicit Return Address across instances;
  synchronous request-reply satisfies it by construction (§3.4). Deferred; named as the future increment in ADR 0023.
- **A managed `http.Server` lifecycle.** msgin returns `http.Handler`s the caller mounts on **their own** server; the
  caller owns TLS, timeouts, and `Server.Shutdown`. msgin documents the required server hardening (§4) but does not own
  the listener. (The SSE *server* owns only its per-connection goroutines and a `Close()`; the SSE *client* owns its
  connection loop.)
- **WebSockets / HTTP/2 server push / long-polling** — SSE is the streaming surface in scope; others are future specs.
- **A routing/mux DSL.** Callers use their own mux/router (stdlib `ServeMux`, gin, chi, …); msgin supplies handlers and
  client adapters, not a router.
- **Content negotiation / body transformation.** The adapter moves bytes + headers; payload shaping is the typed
  runtime's `PayloadCodec[T]` and the existing Transformer/expr endpoints, not the adapter's job.

## 3. Design

### 3.0 Module & package layout

```
adapter/http/            framework-agnostic core   (ROOT module, stdlib only, NO new dep)
  encode.go     request ⇄ Message[any]  (payload = []byte; HTTP header ⇄ Headers framing; correlation-id resolution)
  exchange.go   the O2 request-reply core + the I2 gateway-over-RequestReplyExchange core
  sse.go        SSE event framing (encode) + SSE stream parsing (decode) — shared by server & client
  options.go    WithX functional options shared across modes
  errors.go     typed sentinels (see §3.6)
  doc.go
  (package name is NOT `http` — it would clash with net/http at import sites; use a non-clashing name, e.g. `msghttp`)

adapter/http/stdlib/     net/http bindings         (ROOT module, stdlib only)
  inbound.go    NewInbound (I1) + NewInboundGateway (I2)  → http.Handler
  outbound.go   NewOutbound (O1) + NewExchange (O2)
  sse.go        NewSSEServer (S-out; http.Handler + OutboundAdapter) + NewSSEClient (S-in; StreamingSource)

adapter/http/gin/        gin bindings              (SEPARATE go.mod: github.com/kartaladev/msgin/adapter/http/gin)
  gin.go        gin.HandlerFunc wrappers for I1/I2/S-out + RegisterRoutes(r gin.IRouter, …); reuses adapter/http core
```

Both bindings import `adapter/http`; neither reimplements decode/exchange/SSE logic. `adapter/http/gin` is added to
`go.work` and released under its own module-path-prefixed tag (ADR 0003).

### 3.1 Per-mode → core-SPI mapping (the core does not change)

| Mode | Constructor (sketch) | Implements / produces | Reused core seam |
|------|----------------------|-----------------------|------------------|
| **I1** async inbound | `NewInbound(target msgin.MessageChannel, opts…) http.Handler` | `http.Handler`; sends to a channel, replies `202` | `DirectChannel` |
| **I2** sync gateway | `NewInboundGateway(x msgin.RequestReplyExchange, opts…) http.Handler` | `http.Handler`; `x.Exchange()` → HTTP response | **`RequestReplyExchange` / `ChannelExchange`** |
| **O1** webhook | `NewOutbound(url string, opts…) *Outbound` | `msgin.OutboundAdapter` | `Producer` / `RetryPolicy` |
| **O2** request-reply | `NewExchange(url string, opts…) *Exchange` | **`msgin.RequestReplyExchange`** | `Gateway` / `OutboundGateway` |
| **S-out** SSE server | `NewSSEServer(opts…) *SSEServer` | `http.Handler` **and** `msgin.OutboundAdapter` | `PublishSubscribeChannel` |
| **S-in** SSE client | `NewSSEClient(url string, opts…) *SSEClient` | `msgin.StreamingSource` | `Consumer` / poller runtime |

### 3.2 Shared encode layer (`adapter/http/encode.go`)

- **Request → `Message[any]`** — the request is **external input entering the system**, so it is built with
  **`msgin.New`** (fresh `HeaderID` + timestamp), **not** `NewMessage` (which reconstructs a stored msgin envelope
  verbatim and stamps nothing — using it here would leave `ID()` empty and, since the default correlation id is the
  message's own `ID()`, make every I2 request fail `ErrNoCorrelation`→400; audit H1). Payload = body read as `[]byte`,
  guarded by `http.MaxBytesReader` (default **1 MiB**, `WithMaxBodyBytes`); `HeaderContentType` ← `Content-Type`;
  method, path, raw query, and an **allow-listed** set of request headers copied into `Headers`; reserved `msgin.*`
  headers are **stripped from client input** before construction (§4). Correlation id resolved via
  `WithCorrelationID(func(*http.Request) string)` — **default = the message's own fresh `ID()`** (server-minted,
  unique, never client-controlled; client-supplied correlation ids are trusted only when the caller opts in; §4).
- **Inbound payload is `[]byte` (no inbound codec seam).** I1/I2 send the `Message[any]` (payload `[]byte`) **straight
  to a `MessageChannel`/`RequestReplyExchange`** — unlike the `Consumer`-driven source adapters, there is **no
  `PayloadCodec[T]` decode step on the inbound side**. The flow's first step therefore receives raw bytes and must
  consume them (or begin with a decode/`Transform` step); wiring `Transform[MyStruct,…]` first yields `ErrPayloadType`
  at runtime (audit M4). This is the symmetric partner of the reply-must-be-`[]byte` constraint below.
- **`Message[any]` → response**: reply payload must be `[]byte` (or `string`) — non-bytes → `ErrUnsupportedPayload`→500
  (§3.6); an allow-listed set of `Headers` emitted as response headers (values CRLF-sanitized); `Content-Type` from
  `HeaderContentType` when present. Bytes are extracted **before** any header is written, so the `500` on a non-bytes
  reply never carries the flow's headers (audit L4).

### 3.3 Phase 1 — inbound server (I1, I2)

- **I1 async** — `NewInbound(target, opts…)`: decode → `target.Send(reqCtx, msg)` → **success status** on success
  (`WithSuccessStatus`, default **`202`**; this option governs **I1 only**), **`5xx`** on `Send` error. **`Send` runs
  on the request goroutine under `reqCtx`**: with a `DirectChannel` the downstream chain runs *synchronously* — a
  client disconnect mid-flow cancels in-flight work and a downstream error surfaces as the `5xx`, so `202` means
  "accepted", not "processed". Callers who want a hard accept-then-process boundary (and true at-least-once decoupled
  from the request lifetime) should send to a durable `QueueChannel`, not a `DirectChannel` (audit L1). Delivery =
  at-least-once *iff the client retries on `5xx`* (§5).
- **I2 sync gateway** — `NewInboundGateway(x, opts…)`: decode → `reply, err := x.Exchange(reqCtx, msg)` → encode
  `reply` as the response with status **`200`** (`WithSuccessStatus` does **not** apply to I2 — a request-reply body is
  `200`, never `202`; audit M1). Error → status mapping (overridable via `WithErrorStatus(func(error) int)`):
  `ErrReplyTimeout` → **`504`**; `ErrGatewayClosed` → **`503`**; `ErrDuplicateCorrelation` → **`409`** (only reachable
  when the caller opts into trusting client correlation ids and a client reuses one); body-decode → **`400`** /
  oversize → **`413`**; `ErrNoCorrelation`, `ErrUnsupportedPayload`, and any other → **`500`** (server/wiring faults —
  the adapter always mints a non-empty id, so `ErrNoCorrelation` is never the client's fault; audit L2). Because `x` is
  any `RequestReplyExchange`, a `ChannelExchange` over a request+reply channel pair yields synchronous HTTP-in →
  in-process flow → HTTP-out with no new correlation code. The gateway correlator is process-local (Spec 010) — fine
  here: a single request's exchange begins and ends inside one instance's handler invocation.

### 3.4 Phase 2 — outbound client (O1, O2) & the Return Address boundary

Shared `*http.Client` — `WithHTTPClient` (default: explicit, finite timeouts; not `http.DefaultClient`). The target
URL is **caller-configured and never derived from message payload/headers** (SSRF invariant, §4).

- **O1 webhook** — `NewOutbound(url, opts…)` is an `msgin.OutboundAdapter`. `Send` performs **one** `POST`: payload
  `[]byte` → body, `Content-Type` from `HeaderContentType`, allow-listed headers forwarded. **Response → error
  classification** is the reliability seam (runtime-owned, ADR 0002):
  - `2xx` → `nil`.
  - `4xx` **except `408`/`429`** → `msgin.Permanent(err)` — a request the runtime must **not** retry (dead-letter /
    invalid-message path); it is the caller's payload/target that is wrong.
  - `5xx`, `408`, `429`, network error, timeout → plain (transient) error — the runtime **retries** per `RetryPolicy`.
  - **Open point (resolve in Plan 021):** confirm the outbound/`Producer` retry path applies a `RetryPolicy` to
    `OutboundAdapter.Send`. If it does, this phase adds no adapter-side backoff (keep reliability runtime-owned). If it
    does **not**, Phase 2 adds a thin producer-side retry rather than baking `cenkalti/backoff` into the adapter — the
    decision is recorded in ADR 0023, not left implicit.
- **O2 request-reply** — `NewExchange(url, opts…)` **is** an `msgin.RequestReplyExchange`. `Exchange(ctx, req)`:
  `POST` req body → read the **synchronous** HTTP response → build the reply `Message[any]` (body → payload;
  incoming `HeaderCorrelationID` propagated onto the reply so `OutboundGateway`'s save/restore is honored). Drops into
  `Gateway`/`OutboundGateway` (Plan 019) unchanged.
  - **Return Address — satisfied by construction.** The reply returns on the **same TCP connection** the sending
    instance holds; no reply arrives at a *different* instance, so the cross-process correlation problem the SPI exists
    to surface (CLAUDE.md multi-instance rule) **does not arise** for synchronous HTTP request-reply. This is stated
    explicitly (not left implied) per the multi-instance-awareness rule, and recorded in ADR 0023. The **async-callback
    variant** — where the reply is delivered later to an inbound webhook — is the shape that *would* need an
    instance-addressable callback URL as the Return Address; it is **out of scope for v1** (§2) and named as the future
    increment, keeping the SPI seam ready for it without a core change.

### 3.5 Phase 3 & 4 — SSE

- **S-out SSE server** — `NewSSEServer(opts…) *SSEServer` is **both** an `http.Handler` and an `OutboundAdapter`:
  - **`http.Handler` (`GET`)**: sets `Content-Type: text/event-stream`, `Cache-Control: no-cache`, `Connection:
    keep-alive`; flushes headers; registers the connection; streams events until the client disconnects or the request
    `context` cancels. **One owned, cancellable goroutine per connection**, all joined on `Close()` within a deadline.
    `WithMaxConnections` bounds concurrent connections (default a safe finite cap; connection-exhaustion guard, §4).
    Optional keep-alive comment pings via `WithHeartbeat`.
  - **`OutboundAdapter` (`Send`)**: formats the message as an SSE event — `id:` from `HeaderID`, `event:` from
    `WithEventName` (or a header), `data:` from payload `[]byte` (multi-line-safe framing via `sse.go`) — and **fans
    out to all currently-connected clients**.
  - **Backpressure**: bounded per-connection buffer; `WithSlowClientPolicy` (**default drop-and-continue**, alt
    disconnect). A slow client can never block the sender or grow memory unbounded.
  - **Wiring**: subscribe it to a `PublishSubscribeChannel` (or terminate a chain with `To(sseServer)`).
  - **Multi-instance (documented invariant):** connected clients are **per-process** — a client on instance A sees only
    messages that reach instance A. Cross-instance fan-out requires a shared pub/sub backbone (redis/nats) feeding every
    instance; the SSE server is the **last hop**, not the fan-out fabric. Delivery = **at-most-once** (no client ack;
    dropped on slow/disconnect).
- **S-in SSE client** — `NewSSEClient(url, opts…) *SSEClient` is an `msgin.StreamingSource`. `Stream(ctx, out)`: opens
  the connection, parses the `id:`/`event:`/`data:` stream (shared `sse.go` parser), emits each event as a `Delivery`
  (data → payload `[]byte`; `event`/`id` → headers; `Ack`/`Nack` are **no-ops** — SSE has no ack protocol). On
  disconnect: **auto-reconnect with backoff** (`WithReconnectBackoff`), resuming via **`Last-Event-ID`** (last emitted
  id). Fully ctx-cancellable; the reconnect goroutine is joined on shutdown (`goleak`). Delivery = **at-most-once**,
  best-effort resume. `NativeReliability`: `NativeRedelivery() = false` (resume is best-effort, not a redelivery
  guarantee); `NativeDeadLetter() = false`.

### 3.6 Typed error sentinels

The adapter exports its own sentinels for the typed-error/debuggability surface (blackbox-testable, `errors.Is`):
construction-time validation (`ErrNilChannel`, `ErrNilExchange`, `ErrEmptyURL`, `ErrInvalidMaxBodyBytes`, …) and
runtime classification helpers. Outbound reuses `msgin.Permanent` for the non-retryable classification (no new
sentinel needed there). Exact set finalized per phase in each plan; every construction-time typed-error branch is a
hot-path branch requiring a covering test (CLAUDE.md test-coverage gate).

## 4. Security — inbound is the untrusted boundary

A dedicated `/security-review` runs on the inbound work (Phase 1) and the SSE server (Phase 3), per the Return-Address
adapter being the untrusted-input boundary (Spec 010 §8.1).

- **Body cap** — `http.MaxBytesReader`, default **1 MiB**, `WithMaxBodyBytes`. Oversize → `413`/`400` + `Permanent`.
- **Reserved-header stripping** — client-supplied `msgin.*` headers are removed before building the message; a client
  cannot forge `msgin.correlation-id`, `msgin.delivery-count`, `msgin.id`, etc.
- **Header allow-list** — inbound copies only an allow-listed set of request headers into `Headers`; outbound forwards
  only an allow-listed set; no blanket copy (prevents header injection/smuggling into downstream systems).
- **Correlation-id distrust** — `WithCorrelationID` default **generates fresh**; opt in to trusting a client header
  explicitly. Prevents correlation-id spoofing / cross-talk into the (process-local) gateway correlator.
- **CRLF sanitization** on all header values written to responses.
- **SSRF** — outbound URL is **caller-configured, never derived from payload**; documented invariant. Caller injects
  the `*http.Client` (TLS/proxy control).
- **Server hardening is the caller's** (they own the `http.Server`): msgin's godoc requires `ReadHeaderTimeout`,
  `IdleTimeout`, and a read/write timeout appropriate to the mode (SSE needs no write timeout on the streaming
  response); `WithMaxConnections` on the SSE server bounds connection exhaustion.
- **No secret logging** — injected `*slog.Logger`; bodies/headers are not logged at info level.

## 5. Delivery guarantees (documented per mode)

| Mode | Guarantee | Ack model | Notes |
|------|-----------|-----------|-------|
| **I1** async in | at-least-once *iff client retries on `5xx`* | HTTP status | `202` = accepted, not processed |
| **I2** sync gateway | request-scoped; one response per request | reply / `504` timeout | process-local correlator suffices |
| **O1** webhook | at-least-once (runtime retries transient) | status classification (`4xx`=`Permanent`) | reliability runtime-owned |
| **O2** request-reply | request-scoped (synchronous response) | HTTP response | Return Address by construction |
| **S-out** SSE server | at-most-once (fan-out, no ack) | none | per-process; drop on slow/disconnect |
| **S-in** SSE client | at-most-once, best-effort resume | none | `Last-Event-ID` resume, not a guarantee |

## 6. Phasing (one plan / one merged increment per phase)

| Phase | Plan | Content | Depends on |
|-------|------|---------|------------|
| **1** | 020 | `adapter/http` shared encode core + `adapter/http/stdlib` inbound (I1, I2) → `http.Handler`; ADR 0023 | ADR 0022 |
| **2** | 021 | `adapter/http/stdlib` outbound (O1 webhook, O2 `RequestReplyExchange`); resolve the outbound-retry open point | Phase 1 |
| **3** | 022 | `adapter/http/stdlib` SSE server (S-out) | Phase 1 |
| **4** | 023 | `adapter/http/stdlib` SSE client (S-in, `StreamingSource`) | (encode core) Phase 1 |
| **5** | 024 | `adapter/http/gin` module — gin bindings for I1/I2/S-out + `RegisterRoutes`; ADR 0024 (gin dependency) | Phases 1, 3 |

Each phase: its plan is authored with the driving ADR content, the **spec + ADR + plan are adversarially audited by a
fresh Opus subagent before any code** (two rounds if fixes destabilize the design), then implemented via
subagent-driven development (fresh implementer per task, coordinator commits green tasks, adversarial reviewer),
gated by `/code-review` + `/security-review` over the whole-branch diff and a race-clean suite, merged, and the branch
deleted — all per CLAUDE.md.

## 7. Testing strategy (fully hermetic — no testcontainers)

- **stdlib**: `httptest.Server` (real server + real client over loopback) and `httptest.ResponseRecorder` for handlers.
  SSE server and SSE client are tested **against each other** via `httptest.Server`.
- **gin**: `gin.TestMode` + `httptest`.
- Blackbox `_test` packages only (exercise the exported API); assert-closure tables (`table-test`); `Example` tests
  that double as godoc; `goleak` in every package (SSE connection goroutines are the prime target); mocks via
  `use-mockgen` where an interface must be substituted.
- **Coverage gate**: ≥85% on every changed package; **every hot-path logic branch and every typed-error branch has a
  covering test** — enumerated per task in each plan (CLAUDE.md).
- A deliberate property vs the SQL adapter: HTTP tests are fast, deterministic, and Docker-free.

## 8. Traceability

- **Realizes:** [Spec 001 §9](001-messaging-core.md) (the HTTP adapter), extended with SSE + dual binding.
- **Depends on:** [ADR 0001](../adrs/0001-message-payload-typing.md), [ADR 0002](../adrs/0002-adapter-spi.md),
  [ADR 0003](../adrs/0003-multi-module-repository-layout.md), [ADR 0006](../adrs/0006-resilience-flow-control.md),
  [ADR 0022](../adrs/0022-messaging-gateway.md) / [Spec 010](010-messaging-gateway.md).
- **New ADRs:** ADR 0023 (HTTP adapter architecture — with Plan 020), ADR 0024 (gin dependency — with Plan 024).
- **Plans:** 020 (Phase 1), 021 (Phase 2), 022 (Phase 3), 023 (Phase 4), 024 (Phase 5).
