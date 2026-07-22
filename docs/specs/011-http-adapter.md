# Spec 011 — HTTP channel adapter (inbound + outbound + SSE, stdlib & gin bindings)

- **Status:** **Phase 1 DELIVERED** (2026-07-21) — `adapter/http` (package `msghttp`) + `adapter/http/stdlib` shipped
  on branch `feat/http-adapter-inbound` per [Plan 020](../plans/020-http-adapter-inbound.md). **Phase 2 DELIVERED**
  (2026-07-22) — `adapter/http` outbound (O1 webhook `OutboundAdapter` + O2 `RequestReplyExchange`) merged to `main`
  per [Plan 024](../plans/024-http-outbound.md) / ADR 0023 Addendum B. Phases 3–5 remain Draft; the **Phases 3+4 SSE
  design is settled** (2026-07-22 brainstorm — §3.5, ADR 0023 Addendum C), plans 025/026 to follow.
  The whole-branch `/code-review` + `/security-review` gate forced **six design changes the audited design did not
  anticipate**, all folded back into §3.2/§3.3/§3.6/§4/§7 below and recorded with their driving attack/defect in
  [ADR 0023 — Addendum A](../adrs/0023-http-channel-adapter.md#addendum-a--review-driven-design-changes-phase-1-delivery):
  **(A1)** the client's `Content-Type` no longer reaches the reserved `msgin.content-type` — it lands on a
  non-reserved `http.content-type`, and `EncodeResponse` always sends `X-Content-Type-Options: nosniff` with an
  `application/octet-stream` default (proven reflected-XSS path); **(A2)** the exchange correlation key is **always
  server-minted**, `WithCorrelationID` is replaced by the advisory-only `WithAdvisoryCorrelationID` plus an explicit
  `WithTrustedCorrelationID` opt-in (proven cross-user reply hijack); **(A3)** the planned `statusFor` helper is
  replaced by nil-safe per-field `Config` accessors (a hand-built `&Config{}`/`nil *Config` is externally
  constructible and panicked); **(A4)** new exported symbols `DefaultErrorStatus`, `ErrDecodeRequest`,
  `ErrWriteResponse`; **(A5)** panic recovery at both handler cores (re-panicking `http.ErrAbortHandler`);
  **(A6)** an `EncodeResponse` write failure after the committed `200` is `ErrWriteResponse` and is logged only.
  One residual was recorded honestly, not buried, at delivery: a panicking flow handler leaked a `ChannelExchange`
  correlator slot — a **core** defect (non-`defer`red `giveUp`), deliberately scoped out of this branch and tracked as
  [Spec 012](012-exchange-panic-safe-cleanup.md). **Resolved** by Spec 012 / [Plan 021](../plans/021-exchange-panic-safe-cleanup.md):
  `ChannelExchange.Exchange` now reclaims the slot on every exit path, including a panic unwind.
- **Design status (pre-implementation):** Draft (2026-07-21) — brainstormed with the user; scope, phasing, and the dominant design forks settled
  interactively (see "Decisions settled with the user"). ADR 0023 (HTTP adapter architecture) authored; ADR 0024 (gin
  dependency) deferred to Phase 5; Plan 020 (Phase 1) authored, Plans 024–027 to follow per phase (021 was taken by Spec 012, and 022/023 by Spec 013's producer-retry increment). **Phase-1
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
- **Decisions settled with the user (2026-07-22 — Phases 3+4 combined brainstorm):** one design pass over both SSE
  halves (they share the `sse.go` core), split into **Plans 025/026** with one merged increment each, in phase order.
  **(C1)** server replay of `Last-Event-ID` is an **opt-in bounded ring** (`WithReplayBuffer(n)`, default off;
  per-process, best-effort); **(C2)** heartbeat is **off by default** (opt-in `WithHeartbeat`, with the documented-off
  proxy-idle-timeout warning); **(C3)** the client implements **full WHATWG EventSource semantics** (`retry:` honored
  clamped to the caller's backoff bounds, `204` → clean terminal stop, `Content-Type` validated → `ErrNotEventStream`);
  **(C4)** the server hub is a **mutex registry** with one writer goroutine per connection (central-hub-goroutine and
  lock-held synchronous fan-out rejected). Plus two design-time security decisions: **(C5)** SSE field **injection is
  rejected, not sanitized** (`ErrInvalidEventField`), and **(C6)** a **per-event byte cap** on the parser (default
  1 MiB). All recorded in
  [ADR 0023 Addendum C](../adrs/0023-http-channel-adapter.md#addendum-c--sse-design-decisions-phases-34).
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
    request-reply) **is** a `RequestReplyExchange`, usable behind `Gateway`/`OutboundGateway` (Plan 019) — subject to
    the `[]byte`/`string` payload limitation the codec-less `Gateway.Request` imposes (§3.4).
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
    ADR-justified and the heavy-client-adapter-as-separate-module precedent (ADR 0003). Authored with Plan 027 (Phase 5).

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
adapter/http/            framework-agnostic core   (ROOT module, package msghttp, stdlib only, NO new dep)
  encode.go     [Phase 1 SHIPPED] request ⇄ Message[any] (payload = []byte; HTTP header ⇄ Headers framing;
                correlation-id resolution) + the exported DefaultErrorStatus mapping
  inbound.go    [Phase 1 SHIPPED] the I1 (ServeAsync) + I2 (ServeGateway-over-RequestReplyExchange) handler
                cores, sharing a responseTracker + panic-recovery boundary
  outbound.go   [Phase 2 SHIPPED] the O1 webhook OutboundAdapter core: NewOutbound + EncodeRequest / ClassifyResponse /
                StatusError + validateURL / resolveClient helpers
  exchange.go   [Phase 2 SHIPPED] the O2 request-reply core: NewExchange (sync POST + response)
  sse.go        [Phase 3 lands it; Phase 4 consumes] SSE event framing (encode) + SSE stream parsing (decode) —
                shared by server & client; exported (blackbox tests + cross-package use)
  options.go    [Phase 1 SHIPPED] Config + WithX functional options shared across modes
  errors.go     [Phase 1 SHIPPED] typed sentinels (see §3.6)
  doc.go        [Phase 1 SHIPPED]
  (package name is NOT `http` — it would clash with net/http at import sites; the shipped name is `msghttp`)

adapter/http/stdlib/     net/http bindings         (ROOT module, stdlib only)
  inbound.go    [Phase 1 SHIPPED] NewInbound (I1) + NewInboundGateway (I2) → http.Handler, + Register(mux, …)
  doc.go        [Phase 1 SHIPPED] the deployment checklist (§4: the http.Server timeouts the caller MUST set)
  sse.go        [Phases 3/4] NewSSEServer (S-out; http.Handler + OutboundAdapter) + NewSSEClient (S-in; StreamingSource)

adapter/http/gin/        gin bindings              (SEPARATE go.mod: github.com/kartaladev/msgin/adapter/http/gin)
  gin.go        gin.HandlerFunc wrappers for I1/I2/S-out + RegisterRoutes(r gin.IRouter, …); reuses adapter/http core
```

Both bindings import `adapter/http`; neither reimplements decode/exchange/SSE logic. `adapter/http/gin` is added to
`go.work` and released under its own module-path-prefixed tag (ADR 0003).

**`outbound.go` + `exchange.go` live in `adapter/http`, NOT `adapter/http/stdlib` (ADR 0023 Addendum B4).** The
`adapter/http/stdlib` slot exists to bind the framework-neutral cores to `net/http` **server** types (`http.Handler`,
`*http.ServeMux`) — a place a gin binding has a peer for. An HTTP **client** has no framework variant: gin has no HTTP
client, so a `stdlib` outbound file would be an empty passthrough the gin module would then have to re-export. O1/O2 are
therefore complete in `adapter/http` (package `msghttp`) with no binding layer. (`exchange.go` was already listed here;
O1 joins it.)

### 3.1 Per-mode → core-SPI mapping (the core does not change)

| Mode | Constructor (sketch) | Implements / produces | Reused core seam |
|------|----------------------|-----------------------|------------------|
| **I1** async inbound | `NewInbound(target msgin.MessageChannel, opts…) http.Handler` | `http.Handler`; sends to a channel, replies `202` | `DirectChannel` |
| **I2** sync gateway | `NewInboundGateway(x msgin.RequestReplyExchange, opts…) http.Handler` | `http.Handler`; `x.Exchange()` → HTTP response | **`RequestReplyExchange` / `ChannelExchange`** |
| **O1** webhook | `NewOutbound(url string, opts…) (*Outbound, error)` | `msgin.OutboundAdapter` | `Producer` / `RetryPolicy` |
| **O2** request-reply | `NewExchange(url string, opts…) (*Exchange, error)` | **`msgin.RequestReplyExchange`** | `Gateway` / `OutboundGateway` |
| **S-out** SSE server | `NewSSEServer(opts…) *SSEServer` | `http.Handler` **and** `msgin.OutboundAdapter` | `PublishSubscribeChannel` |
| **S-in** SSE client | `NewSSEClient(url string, opts…) *SSEClient` | `msgin.StreamingSource` | `Consumer` / poller runtime |

### 3.2 Shared encode layer (`adapter/http/encode.go`) — Phase 1 SHIPPED

- **Request → `Message[any]`** — the request is **external input entering the system**, so it is built with
  **`msgin.New`** (fresh `HeaderMessageID` + timestamp), **not** `NewMessage` (which reconstructs a stored msgin envelope
  verbatim and stamps nothing — using it here would leave `ID()` empty and, since the correlation id is the
  message's own `ID()`, make every I2 request fail `ErrNoCorrelation`→500; audit H1). Payload = body read as `[]byte`,
  guarded by `http.MaxBytesReader` (default **1 MiB**, `WithMaxBodyBytes`); an **allow-listed** set of request headers
  copied into `Headers`; reserved `msgin.*` headers are **stripped from client input** (case-insensitively) before
  construction (§4).
- **Client-sourced values travel on NON-reserved `http.*` keys** (delivered; ADR 0023 Addendum A1/A2). `DecodeRequest`
  adds `http.method`, `http.path`, `http.query`, and — when present — `http.content-type` (the client's `Content-Type`
  **verbatim**) and `http.correlation-id` (the advisory client id). The client `Content-Type` deliberately does **not**
  land on the reserved `msgin.content-type`: that header is what `EncodeResponse` trusts as the *response* media type,
  so writing the client's value there let a client choose the media type its own echoed bytes were served under — a
  proven **reflected-XSS** path on any echo-shaped flow. Because these keys carry no `msgin.` prefix they are outside
  the reserved namespace by construction and can never be mistaken for core metadata.
- **Correlation id — server-minted by default, with an advisory/trusted split** (delivered; ADR 0023 Addendum A2).
  `msgin.HeaderCorrelationID` — the key a `RequestReplyExchange` matches the reply on — is **always** the message's own
  fresh server-minted `ID()` unless the caller explicitly opts in:
  - `WithAdvisoryCorrelationID(func(*http.Request) string)` records a client-advertised id on the **non-reserved**
    `http.correlation-id` header for the flow to read/log/echo. It carries **no authority** over the exchange key.
  - `WithTrustedCorrelationID(func(*http.Request) string)` is the sole path to a client-keyed exchange, and carries a
    `SECURITY WARNING` godoc: a guessable/replayable value is a **reply hijack** (a peer registers the waiter the
    victim's reply is delivered to) and a pre-registered value is a **targeted denial** (`ErrDuplicateCorrelation`→409).
    An empty resolved value falls back to the server-minted id — never to the advisory value.
  - The two are **orthogonal**: disjoint headers, disjoint trust, either/both settable, neither overriding the other.
    A `nil` resolver is a no-op on both (it never clobbers an earlier one in the same `Option` list).
- **Inbound payload is `[]byte` (no inbound codec seam).** I1/I2 send the `Message[any]` (payload `[]byte`) **straight
  to a `MessageChannel`/`RequestReplyExchange`** — unlike the `Consumer`-driven source adapters, there is **no
  `PayloadCodec[T]` decode step on the inbound side**. The flow's first step therefore receives raw bytes and must
  consume them (or begin with a decode/`Transform` step); wiring `Transform[MyStruct,…]` first yields `ErrPayloadType`
  at runtime (audit M4). This is the symmetric partner of the reply-must-be-`[]byte` constraint below.
- **`Message[any]` → response**: reply payload must be `[]byte` (or `string`) — non-bytes → `ErrUnsupportedPayload`→500
  (§3.6); an allow-listed set of `Headers` emitted as response headers (values CRLF-sanitized); `Content-Type` from
  the **flow's** `msgin.HeaderContentType` when present and non-empty, otherwise **`application/octet-stream`** —
  never left unset, so `net/http`'s `DetectContentType` can never sniff `text/html` out of a flow-controlled body; and
  **`X-Content-Type-Options: nosniff` is ALWAYS set**, after the allow-list, so an allow-listed message header cannot
  weaken it (delivered; ADR 0023 Addendum A1). Bytes are extracted **before** any header is written, so the `500` on a
  non-bytes reply never carries the flow's headers (audit L4). The status is **always `200`**.
- **Write failure after commit** (delivered; ADR 0023 Addendum A6): once the `200` is on the wire the only remaining
  failure is the body write; it is wrapped as **`ErrWriteResponse`** and is the structural signal that the response is
  already committed — the caller must **log only** and must never write a second status.
- **`DefaultErrorStatus(error) int` is exported** (delivered; ADR 0023 Addendum A4) so a custom `WithErrorStatus`
  mapper can classify the cases it cares about and **delegate the rest**, instead of losing the 413-vs-400-vs-5xx
  discrimination it cannot reimplement from outside the package.

### 3.3 Phase 1 — inbound server (I1, I2) — SHIPPED

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
- **Panic recovery at both handler cores** (delivered; ADR 0023 Addendum A5). A flow handler runs **on the request
  goroutine** (a `DirectChannel` subscriber under I1's `Send`, the exchange's request send under I2), so a panicking
  consumer handler would otherwise escape into `net/http`. Both cores install a deferred recover that logs the panic
  with its stack through the injected logger and answers a plain `500` **when the response is not yet committed** —
  commit is tracked by a `responseTracker` wrapper, which also drops a second `WriteHeader` (no "superfluous
  response.WriteHeader call"). **`http.ErrAbortHandler` is re-panicked**, honoring `net/http`'s documented
  silent-abort contract rather than converting a deliberate abort into a logged 500.
  - **Residual recorded at delivery, since resolved:** the recover contains the panic but — at Phase-1 delivery —
    could not reclaim the exchange's reply-waiter slot, because `msgin.ChannelExchange.Exchange` registered the
    waiter *before* it sent and its `giveUp` cleanup was **not `defer`red**, so a panicking flow leaked one correlator
    map entry + channel per request regardless of the recover. On the default path the impact was **memory-only**
    (each request has a fresh server-minted key, so no slot is ever re-keyed); a `409` poisoning variant required the
    opt-in `WithTrustedCorrelationID` **and** a reused client value. Both required a panicking handler — a bug in the
    consumer's own code. The root cause was **core-side** and is **resolved** by
    [Spec 012](012-exchange-panic-safe-cleanup.md) / [Plan 021](../plans/021-exchange-panic-safe-cleanup.md):
    `ChannelExchange.Exchange` now reclaims the slot on every exit path, including a panic unwind, and the recover
    here remains required — independent of the exchange — as the boundary that stops any panicking flow handler from
    taking down the server.
- **A hand-built `Config` must never panic** (delivered; ADR 0023 Addendum A3). `Config` is exported with unexported
  fields, so `&msghttp.Config{}` and a `nil *Config` are constructible from any package and reach every exported
  consumer. The planned single `statusFor(cfg, err)` helper assumed a `Config` could only come from `NewConfig`; that
  premise was false and the assumption was a nil-pointer panic. Every setting is therefore read through a **nil-safe
  per-field accessor** that back-fills the documented default (1 MiB cap, 202 async status, `DefaultErrorStatus`,
  discard logger, empty allow-lists). Only `NewConfig` validates; a hand-built `Config` is tolerated, not supported.

### 3.4 Phase 2 — outbound client (O1, O2) & the Return Address boundary

Shared `*http.Client` — `WithHTTPClient` (default `&http.Client{Timeout: 30s}` — finite **on purpose**, because
`http.DefaultClient` has no timeout at all). The target URL is **caller-configured and never derived from message
payload/headers**, which prevents **message-driven** SSRF only (SSRF invariant, §4).

**Redirect policy (INV-1).** `validateURL` runs **once, at construction**; the default `*http.Client` otherwise follows
up to 10 redirects, so an unbounded-follow client turns a remote `3xx` into a host-substitution lever — a
`302 → http://169.254.169.254/latest/meta-data/…` makes **O2 return cloud instance-metadata credentials into the flow
as the reply payload**, and a `307`/`308` **replays the POST body and every allow-listed header** to the attacker's
host. The adapter therefore installs a no-follow `CheckRedirect` (returning `http.ErrUseLastResponse`) on a **shallow
copy** of the client, so a `3xx` is classified (`Permanent`), never followed. **Two escapes, each an explicit caller
choice named at the option:** `WithFollowRedirects()`, or a caller-supplied `*http.Client` whose own `CheckRedirect`
follows. **NON-GUARANTEE:** msgin performs **no** private-IP, link-local, loopback or metadata-endpoint filtering —
`NewOutbound("http://169.254.169.254/…")` issues exactly that request (§4).

- **O1 webhook** — `NewOutbound(url, opts…)` is an `msgin.OutboundAdapter`. `Send` performs **one** `POST`: payload
  `[]byte` → body, `Content-Type` from `HeaderContentType`, allow-listed headers forwarded. **Response → error
  classification** is the reliability seam (runtime-owned, ADR 0002):
  - `2xx` → `nil`.
  - `4xx` **except `408`/`429`** → `msgin.Permanent(err)` — a request the runtime must **not** retry (dead-letter /
    invalid-message path); it is the caller's payload/target that is wrong.
  - `5xx`, `408`, `429`, network error, timeout → plain (transient) error — the runtime **retries** per `RetryPolicy`.
  - **Shipped by Plan 023.** `WithProducerRetry(p RetryPolicy)` applies a `RetryPolicy` to `OutboundAdapter.Send`, so
    "the runtime retries per `RetryPolicy`" is now true for a **direct `Producer.Send`**, not only when the send sits
    inside a Consumer-driven flow. The gap was closed at the producer, in core, by
    [Spec 013](013-producer-outbound-retry.md) / [ADR 0025](../adrs/0025-producer-outbound-retry.md) /
    [Plan 023](../plans/023-producer-outbound-retry.md), which also ships the `msgin.RetryAfter` marker this phase is
    the first producer of. ADR 0025 **supersedes ADR 0005's outbound-HTTP clause**: no adapter-side backoff loop, and
    `cenkalti/backoff` is **not** adopted. A flow with **no inbound source** driving `To(outbound)` still gets no retry
    (ADR 0025 §2).
  - **`Retry-After` (added by Spec 013).** A `429` or `503` carrying `Retry-After` — in either the delay-seconds or the
    HTTP-date form — is classified as `msgin.RetryAfter(err, d)`, so the producer's retry loop honors the server's
    instruction over its computed backoff (clamped, and bounded by `ctx`). Without the header it is a plain transient
    error.
- **O2 request-reply** — `NewExchange(url, opts…)` **is** an `msgin.RequestReplyExchange`. `Exchange(ctx, req)`:
  `POST` req body → read the **synchronous** HTTP response → build the reply `Message[any]` (body → payload; the reply
  message is seeded from the *request's* headers with the reserved `msgin.message-id`/`timestamp`/`content-type` dropped
  and re-stamped fresh, so `OutboundGateway`'s save/restore of `HeaderCorrelationID` is honored — ADR 0023 Addendum B3).
  - **The `Gateway` limitation (verified against `gateway.go:64-76`; recorded verbatim here and on `NewExchange`'s
    godoc).** `msgin.Gateway` exposes **`Request(ctx, req Req) (Rep, error)`**, not `Exchange`; it takes a **raw `Req`**
    (it mints the correlation id itself via `New(req).WithHeader(HeaderCorrelationID, randomID())`) and returns a
    **`Rep` value**, not a message. It has **no codec at all** — `boxMessage` in, `PayloadOf[Rep]` (a type assertion)
    out. Therefore `msghttp.Exchange` behind a `Gateway` works **only** for `Req ∈ {[]byte, string}` (what
    `EncodeRequest`'s payload extraction accepts) and `Rep == []byte` (what `Exchange` produces).
    `Gateway[MyStruct, MyStruct]` fails with `msgin.ErrPayloadType`, and so does `Gateway[[]byte, string]`. "Drops in
    unchanged" over-claims.
  - **Bound by the `RequestReplyExchange` no-leak-on-unwind contract** ([Spec 012](012-exchange-panic-safe-cleanup.md)
    §7 / [ADR 0022 Addendum A3](../adrs/0022-messaging-gateway.md)). `NewExchange` is the **first external
    implementation** of that SPI and holds its own request-scoped state — an in-flight `*http.Request`, a response body
    that must be closed — of exactly the class whose leak Spec 012 fixed in `ChannelExchange`. It MUST release that
    state on **every** exit path including a panic unwind (deferred cleanup), and MUST NOT recover a caller panic into
    an error return.
  - **Return Address — satisfied by construction.** The reply returns on the **same TCP connection** the sending
    instance holds; no reply arrives at a *different* instance, so the cross-process correlation problem the SPI exists
    to surface (CLAUDE.md multi-instance rule) **does not arise** for synchronous HTTP request-reply. This is stated
    explicitly (not left implied) per the multi-instance-awareness rule, and recorded in ADR 0023. The **async-callback
    variant** — where the reply is delivered later to an inbound webhook — is the shape that *would* need an
    instance-addressable callback URL as the Return Address; it is **out of scope for v1** (§2) and named as the future
    increment, keeping the SPI seam ready for it without a core change.

### 3.5 Phase 3 & 4 — SSE

**Design settled with the user (2026-07-22 brainstorm — one combined design pass over both phases, split into Plans
025/026 with one merged increment each; decisions C1–C6 recorded in
[ADR 0023 Addendum C](../adrs/0023-http-channel-adapter.md#addendum-c--sse-design-decisions-phases-34)).**

- **Shared SSE core** (`adapter/http/sse.go`, package `msghttp` — Phase 3 lands it, Phase 4 consumes it). Exported,
  because `stdlib` is a different package and tests are blackbox-only:
  - `SSEEvent{ID, Name string; Data []byte}` — `""` = omit the `id:`/`event:` line (a nameless event dispatches as
    `message` per the standard).
  - **Encode** — `EncodeSSEEvent(w io.Writer, ev SSEEvent) error`: `id:`/`event:`/`data:` lines + terminating blank
    line; `Data` is split on `\n` into one `data:` line each (multi-line-safe; the parser rejoins). **SSE-injection
    guard (C5):** an `ID`/`Name` containing CR, LF, or NUL could forge extra fields or whole events from a message
    header — encode **rejects** it with `ErrInvalidEventField` (reject, not sanitize — a silently altered id would
    break C1's identity-matched replay and mask the flow bug); never a corrupted stream.
  - **Decode** — an incremental parser (constructor + per-event pull API; exact shape finalized in Plan 025)
    implementing the **full WHATWG field rules**: comment lines (`:`), `data` accumulation joined with `\n`, `id`
    ignored if it contains NUL, an **empty `id:` line clears** the last-event-id, `retry` digits-only (surfaced to
    the reconnect loop), unknown fields ignored, CR/LF/CRLF line endings, a single leading BOM stripped. **Bounded
    (C6):** per-event byte cap — default **1 MiB** (the `WithMaxResponseBytes` precedent), overridable via
    `WithMaxEventBytes` — exceeded → `ErrEventTooLarge`. Implemented over `bufio.Reader` with explicit byte
    accounting (no `bufio.Scanner`: its token cap and line splitting fit neither CR-only endings nor the cap
    semantics). Both halves are goroutine-free state machines — unit-testable without a network; the parser is a
    fuzz target (§7).
- **S-out SSE server** (`adapter/http/stdlib`, Plan 025) — `NewSSEServer(opts…) (*SSEServer, error)` is **both** an
  `http.Handler` and an `OutboundAdapter`. **Hub model (C4): mutex registry** — a `sync.Mutex`-guarded connection
  set; each connection owns a bounded event channel (`WithConnectionBuffer`, default **16**) drained by its **one
  owned writer goroutine** (encode → `Flush`), all joined by `Close(ctx)` within ctx's deadline.
  - **Handler path (`GET` only; anything else → 405):** at `WithMaxConnections` (default **1024**) the request gets
    **503** (connection-exhaustion guard, §4); a non-`http.Flusher` `ResponseWriter` gets 500 + a typed error to the
    error hook; then `Content-Type: text/event-stream`, `Cache-Control: no-cache`, `Connection: keep-alive`, flush
    headers, register. **Replay+register is atomic (C1):** with `WithReplayBuffer(n)` (default **off**) and a
    `Last-Event-ID` request header, the ring tail *after* that id is snapshotted and the connection registered in the
    same critical section — no missed-event window between replay and live; an evicted/unknown id yields live-only
    (documented best-effort). Ids are matched by identity, not order (`HeaderMessageID` UUIDs are not orderable).
  - **`Send(ctx, msg)` (`OutboundAdapter`):** payload must be `[]byte` (else `ErrUnsupportedPayload`); `id:` ←
    `HeaderMessageID`; `event:` ← the `HeaderSSEEventName` message header, falling back to `WithEventName`'s fixed
    name (header wins; precedence documented). Field validation per C5 — a violation returns `ErrInvalidEventField`
    classified `msgin.Permanent` (a retry cannot fix a bad header). Then: append to the replay ring (if on) and
    **non-blocking enqueue** to every live connection — a full buffer triggers `WithSlowClientPolicy` (**default
    drop-and-continue**, counted via the observability hook; alternative disconnect). A slow client never blocks
    `Send` or grows memory unbounded. `Send` with zero connections returns nil (fire-and-forget fan-out, documented).
  - **Heartbeat (C2):** opt-in `WithHeartbeat(d)` — clockwork-driven `: ping` comment frames written from the same
    writer goroutine (no second goroutine per connection). **Off by default**; per the defaults policy's
    documented-off requirement, `NewSSEServer`'s godoc warns plainly that idle streams behind proxies/LBs with
    30–60 s idle timeouts are silently dropped unless heartbeat is enabled.
  - **Shutdown:** `Close(ctx)` stops accepting (new requests → 503), cancels every connection, joins all writer
    goroutines within ctx's deadline; goleak-proven.
  - **Wiring:** subscribe it to a `PublishSubscribeChannel` (or terminate a chain with `To(sseServer)`).
  - **Multi-instance (documented invariant):** connected clients **and the replay ring are per-process** — a client
    on instance A sees only messages that reach instance A, and a reconnect landing on instance B finds an empty
    ring, so replay is best-effort even when enabled. Cross-instance fan-out requires a shared pub/sub backbone
    (redis/nats) feeding every instance; the SSE server is the **last hop**, not the fan-out fabric. Delivery =
    **at-most-once** (no client ack; dropped on slow/disconnect).
- **S-in SSE client** (`adapter/http/stdlib`, Plan 026) — `NewSSEClient(url, opts…) (*SSEClient, error)`, an
  `msgin.StreamingSource`. Reuses Phase 2's `validateURL` (`ErrEmptyURL`/`ErrInvalidURL`); the caller injects the
  `*http.Client` via `WithHTTPClient` (TLS/proxy/auth stay caller-owned, the Phase-2 precedent); `WithConnectHeaders`
  sets static request headers (e.g. `Authorization`) — the client-owned `Last-Event-ID`, `Accept` and
  `Cache-Control` cannot be overridden.
  - **Connect loop (`Stream(ctx, out)`):** `GET` with `Accept: text/event-stream`, `Cache-Control: no-cache`, plus
    `Last-Event-ID` when held. **Full WHATWG triage (C3):** **`204` → clean terminal stop** (`Stream` returns nil —
    the server said done, no reconnect); a 2xx with a `Content-Type` other than `text/event-stream` →
    `ErrNotEventStream`, reconnect-with-backoff; any other non-2xx → reconnect-with-backoff. A never-succeeding
    endpoint backs off until ctx cancels — give-up policy belongs to the runtime, per the SPI.
  - **Parse loop:** each event → a `Delivery` — `Data` → payload `[]byte`, `Name` → `HeaderSSEEventName`, `ID` →
    `HeaderMessageID` (absent field → header omitted); `Ack`/`Nack` are **no-ops** (SSE has no ack protocol); emit
    respects ctx (no send after cancel). `id:` is tracked as the last-event-id (an empty `id:` clears it, per C3);
    **`retry:` is honored, clamped** into the caller's backoff `[min, max]` — a hostile server can neither force a
    hot loop (`retry: 0`) nor hang the source (`retry:` huge).
  - **Reconnect:** on EOF/read error, exponential backoff via `WithReconnectBackoff(min, max)` (default
    **500 ms → 30 s**, clockwork-driven; the `retry:` clamp shares the bounds); backoff resets after a connection
    that yields at least one event.
  - **Untrusted-remote posture (§4):** per-event cap (C6 — an oversized event is `ErrEventTooLarge` + reconnect, not
    an OOM); redirects default-off per Addendum B1 (`WithFollowRedirects` opts out); transport failures wrapped by
    the redacted `ErrOutboundTransport` (INV-5 — no URL/userinfo/query in error strings).
  - **Shutdown & contracts:** ctx cancel aborts the in-flight request and joins the loop (`goleak`-proven); `Stream`
    returns. `NativeRedelivery() = false` (resume is best-effort, not a redelivery guarantee);
    `NativeDeadLetter() = false`. Delivery = **at-most-once, best-effort resume** — godoc names the gap: resume only
    works against a server honoring `Last-Event-ID` (e.g. this package's own server with `WithReplayBuffer`).

### 3.6 Typed error sentinels

The adapter exports its own sentinels for the typed-error/debuggability surface (blackbox-testable, `errors.Is`):
construction-time validation (`ErrNilTarget`, `msgin.ErrNilExchange`, `ErrEmptyURL`, `ErrInvalidMaxBodyBytes`, …) and
runtime classification helpers. Outbound reuses `msgin.Permanent` for the non-retryable classification (no new
sentinel needed there). Exact set finalized per phase in each plan; every construction-time typed-error branch is a
hot-path branch requiring a covering test (CLAUDE.md test-coverage gate).

**Phase 1 shipped set** (`adapter/http/errors.go`): `ErrNilTarget` (async nil target — a local sentinel rather than
`msgin.ErrNilChannel`, whose "request or reply channel is nil" wording describes the gateway's two-channel exchange;
audit L3), `ErrInvalidMaxBodyBytes`, `ErrInvalidStatusCode`, `ErrUnsupportedPayload`, plus two added during delivery
(ADR 0023 Addendum A4): **`ErrDecodeRequest`**, wrapping every `DecodeRequest` read/decode failure so a status mapper
can tell a request fault (400) from an unclassified downstream error (500) — it replaced an unexported `decodeError`
struct that no external mapper could match on — and **`ErrWriteResponse`** (§3.2). The gateway's nil-exchange guard
reuses the exported `msgin.ErrNilExchange`.

**Phase 2 added set** (Plan 024 — `adapter/http/errors.go` + `options.go`). **Six new sentinels:** **`ErrEmptyURL`**
and **`ErrInvalidURL`** (`validateURL` at construction — empty/whitespace, and a parse failure / non-{http,https}
scheme / empty host respectively); **`ErrInvalidMaxResponseBytes`** (an explicit `WithMaxResponseBytes(n <= 0)`);
**`ErrReplyTooLarge`** (O2 reply body over the cap, INV-6); **`ErrOutboundStatus`** (the sentinel `StatusError` unwraps
to — a non-2xx classification, INV-3); and **`ErrOutboundTransport`** (the redacted wrapper for a `(*http.Client).Do`
transport failure, INV-5). Outbound reuses `msgin.Permanent` / `msgin.RetryAfter` for retry classification (no new
sentinel there). **Seven new functional options:** **`WithHTTPClient`**, **`WithFollowRedirects`** (INV-1 opt-out),
**`WithOutboundHeaders`** (request allow-list; reserved `msgin.*` names dropped — decision 4), **`WithOutboundReplyHeaders`**
(reply allow-list; reserved `msgin.*` names dropped — INV-2), **`WithMaxResponseBytes`** (INV-6), **`WithOutboundClock`**
(a `clockwork.Clock`, distinct name per the repo convention), and **`WithErrorBodyExcerpt`** (decision 5 — opt-in
bounded/sanitized body excerpt, §4). **`StatusError`** (`{Code int; Excerpt string}`) is the exported classification
error; its `Error()` carries the status code and — only when `WithErrorBodyExcerpt()` is set — a bounded, sanitized body
excerpt, **never a URL** (INV-4/INV-5).

**Phases 3/4 planned set** (settled at design time — §3.5 / Addendum C; finalized per plan). **Nine new sentinels:**
**`ErrInvalidEventField`** (CR/LF/NUL in an SSE `id`/`event` field — the injection guard, C5; also the
construction-time guard on a `WithEventName` value); **`ErrEventTooLarge`** (per-event cap exceeded, C6);
**`ErrInvalidMaxConnections`**, **`ErrInvalidConnectionBuffer`**, **`ErrInvalidReplayBuffer`**,
**`ErrInvalidHeartbeat`** (server construction-time validation — explicit non-positive values, the set-flag pattern);
**`ErrInvalidMaxEventBytes`** (an explicit `WithMaxEventBytes(n <= 0)` — the `ErrInvalidMaxResponseBytes` precedent);
**`ErrNotEventStream`** (a 2xx whose `Content-Type` is not `text/event-stream`, C3);
**`ErrInvalidReconnectBackoff`** (client construction-time — non-positive or `min > max`). Reused: `ErrUnsupportedPayload`
(non-`[]byte` payload on `Send`), `ErrEmptyURL`/`ErrInvalidURL` (client URL validation), `ErrOutboundTransport`
(redacted transport failures), `msgin.Permanent` (the `Send`-side classification of `ErrInvalidEventField`). **New
options** — server: `WithMaxConnections` (default **1024**), `WithConnectionBuffer` (**16**), `WithSlowClientPolicy`
(**drop-and-continue**), `WithReplayBuffer` (**off**), `WithHeartbeat` (**off**), `WithEventName`, `WithSSEClock`;
client: `WithHTTPClient`, `WithConnectHeaders`, `WithReconnectBackoff` (**500 ms → 30 s**), `WithMaxEventBytes`
(**1 MiB**), `WithFollowRedirects`, `WithSSEClock`. All follow the set-flag option pattern; every default is godoc'd
with its rationale (defaults policy).

## 4. Security — inbound is the untrusted boundary

A dedicated `/security-review` runs on the inbound work (Phase 1) and the SSE server (Phase 3), per the Return-Address
adapter being the untrusted-input boundary (Spec 010 §8.1).

- **Body cap** — `http.MaxBytesReader`, default **1 MiB**, `WithMaxBodyBytes`. Oversize → `413` (`*http.MaxBytesError`
  checked before `ErrDecodeRequest`, which also wraps it); any other read/decode fault → `400`. **Caveat, documented on
  the option:** the cap bounds the **body only** — the request line (`http.path`/`http.query`) and allow-listed headers
  copied onto each message are bounded instead by the caller's `http.Server.MaxHeaderBytes`, and on the async path that
  memory is retained for as long as the queued message lives.
- **Reserved-header stripping** — client-supplied `msgin.*` headers are removed (**case-insensitively**) before
  building the message; a client cannot forge `msgin.correlation-id`, `msgin.delivery-count`, `msgin.message-id`, etc., even
  through a misconfigured allow-list entry that names one.
- **Header allow-list** — inbound copies only an allow-listed set of request headers into `Headers`; outbound forwards
  only an allow-listed set; no blanket copy (prevents header injection/smuggling into downstream systems). The
  **response** allow-list is deliberately *not* reserved-namespace-guarded (asymmetric with the request side): it is
  trusted operator config, so listing `msgin.correlation-id`/`msgin.message-id`/… **does** publish internal flow metadata to
  every caller of the endpoint — documented as a `CAUTION` on `WithResponseHeaders`.
- **Correlation-id distrust — server-minted, always** (hardened during delivery; ADR 0023 Addendum A2). The exchange
  key is the message's own server-minted `ID()`; a client value keys the correlator **only** through the explicitly
  named `WithTrustedCorrelationID` opt-in, whose godoc states the reply-hijack and targeted-denial attacks and the
  unguessable/single-use/authenticated preconditions. `WithAdvisoryCorrelationID` carries the client's id with no
  authority, on the non-reserved `http.correlation-id` header. The original single `WithCorrelationID` — whose
  resolver *decided* the exchange key — is gone: routing a client-controlled string into the process-local correlator
  was a proven cross-user **reply hijack** (guess/replay another client's value and read its response) and a `409`
  **denial** variant (pre-register the victim's value).
- **The client never chooses the response media type** (hardened during delivery; ADR 0023 Addendum A1). The response
  `Content-Type` comes from the **flow's** `msgin.HeaderContentType`, defaulting to `application/octet-stream`;
  `X-Content-Type-Options: nosniff` is always set. The client's own `Content-Type` is carried only as the inert
  `http.content-type` metadata header. Without this, an echo-shaped flow was a **reflected-XSS** vector: the client
  posted `text/html` + a script body and had it served back executable.
- **CRLF sanitization** on all header values written to responses (including the `Content-Type` taken from the flow).
- **Fault isolation** — both handler cores recover a panicking flow handler into a clean `500` (§3.3), so one bad
  request cannot take down the server. `http.ErrAbortHandler` is re-panicked per `net/http`'s contract. The
  correlator-slot leak this path used to leave behind is detailed, and its resolution recorded, in §3.3
  ([Spec 012](012-exchange-panic-safe-cleanup.md)).
- **No panic on caller input** — a hand-built or `nil *Config` is tolerated via nil-safe accessors (§3.3), never a
  nil-pointer dereference (CLAUDE.md debuggability / "no panic on caller input").
- **SSRF — message-driven only** (INV-1). The outbound URL is **caller-configured, never derived from
  payload/headers**, so a message cannot steer a request; `validateURL` runs **once at construction** (scheme ∈
  {http, https}, non-empty host). **Redirect following is disabled by default** (`CheckRedirect` →
  `http.ErrUseLastResponse` on a shallow copy of the client), so a remote `3xx` cannot redirect a request to a host
  other than the one validated at construction — closing the `302 → metadata-endpoint` credential-exfiltration and the
  `307`/`308` body-replay vectors the default follow-up-to-10 client would open. **Two escapes, each an explicit caller
  choice documented at the option:** `WithFollowRedirects()`, or a caller-supplied `*http.Client` whose own
  `CheckRedirect` follows. **NON-GUARANTEE (stated plainly on godoc):** msgin performs **no** private-IP, link-local,
  loopback or metadata-endpoint filtering — `NewOutbound("http://169.254.169.254/…")` gets exactly that request. The
  invariant prevents **message-driven** SSRF only. Caller injects the `*http.Client` (TLS/proxy control). *(Phase 2.)*
- **The remote server never chooses a reserved media-type header** — outbound mirror of A1, opposite direction (INV-2).
  A `Content-Type` read from the untrusted remote server lands on the **non-reserved `http.content-type`**, **never** on
  the reserved `msgin.content-type` that `EncodeResponse` trusts as the response media type — otherwise an upstream
  replying `text/html`, driven back through an `OutboundGateway` → `EncodeResponse`, re-opens A1's **reflected XSS** from
  the outbound side (`X-Content-Type-Options: nosniff` does **not** stop an *explicit* `text/html`).
  `WithOutboundReplyHeaders` entries matching the reserved `msgin.` prefix (case-insensitively) are defensively dropped
  — the remote is untrusted input exactly as an inbound client is. *(Phase 2.)*
- **Outbound request header allow-list strips reserved names** — decision 4, symmetry with the reply path. `WithOutboundHeaders`
  copies only an allow-listed set of message headers onto the outbound request, and an entry whose name (lowercased)
  carries the reserved `msgin.` prefix is **silently dropped**, so internal flow metadata (`msgin.correlation-id`,
  `msgin.message-id`, …) is never published to the remote endpoint — matching `WithOutboundReplyHeaders` and the inbound
  `DecodeRequest`. The reserved-**name** guard is case-insensitive (a *security* filter, must not be casing-bypassable);
  the message-header **value** lookup is case-sensitive (a *functional* exact map key). `Content-Type` is written after
  the allow-list, so an entry naming it cannot override it. *(Phase 2.)*
- **Remote error-body excerpt is opt-in and sanitized** — decision 5 (INV-4/INV-5). By default `StatusError` carries
  **only** the `int` status code — no URL, no body, no `resp.Status` text — so no remote-controlled bytes reach caller
  logs. `WithErrorBodyExcerpt()` opts into embedding a **bounded, fully-sanitized** excerpt of the remote response body
  in the error string: at most `errorBodyExcerptMax` (**256**) bytes are **read** from the body (a DoS-amplification
  bound — cap the READ, not merely the output length), then sanitized at the **rune** level via `strconv.Quote`, which
  escapes every non-`unicode.IsPrint` rune (C0/C1 controls, ANSI `\x1b`, bidi overrides `U+202E`, `U+2028`/`U+2029`,
  NEL, BOM) and invalid UTF-8 to `\uXXXX`/`\xNN`, **preserves** printable accented/CJK/emoji runes, and delimits the
  excerpt in quotes. This is stricter than the inbound CR/LF-only `sanitizeHeaderValue`. A transport failure is wrapped
  as `ErrOutboundTransport` with the target URL's userinfo, path and query discarded (INV-5), so a webhook token in the
  query string never lands in a logged timeout. *(Phase 2.)*
- **Server hardening is the caller's** (they own the `http.Server`). This is now **discharged in code**: the
  `adapter/http/stdlib` **package godoc carries a "Deploying these handlers safely" section** with a concrete
  `&http.Server{…}` snippet mandating `ReadHeaderTimeout` (the one value with no safe default anywhere in the stack),
  `ReadTimeout`, `WriteTimeout`, `IdleTimeout` and `MaxHeaderBytes`, and both constructors' godoc point at it. The same
  section states plainly that these handlers apply **no authentication, no authorization, no CSRF defense, no CORS and
  no method filtering** — so an unauthenticated endpoint is world-writable by a simple cross-origin `POST` and a
  cookie-authenticated one is CSRF-vulnerable — and directs the caller to their own middleware. SSE's own timeout
  shape (no write timeout on the streaming response) and `WithMaxConnections` land with Phase 3.
- **SSE attack surfaces (enumerated at design time; verified by Plans 025/026 and the Phase-3 `/security-review`):**
  server — **SSE field injection** via message headers (`id:`/`event:` carrying CR/LF/NUL → rejected,
  `ErrInvalidEventField`, C5); **connection exhaustion** (`WithMaxConnections` → 503); **slow-client memory/blocking**
  (bounded per-connection buffer + drop/disconnect policy — `Send` never blocks); **replay-ring memory** (bounded `n`,
  opt-in, C1). Client — the remote is untrusted input exactly as Phase 2's: **unbounded-event OOM** (per-event cap →
  `ErrEventTooLarge`, C6); **hostile `retry:`** (clamped into the caller's backoff bounds, C3); **redirect SSRF**
  (Addendum B1's no-follow default); **error redaction** (`ErrOutboundTransport`, INV-5). *(Phases 3/4.)*
- **No secret logging** — injected `*slog.Logger` (default: discard, never `slog.Default()`); bodies/headers are not
  logged at info level.

## 5. Delivery guarantees (documented per mode)

| Mode | Guarantee | Ack model | Notes |
|------|-----------|-----------|-------|
| **I1** async in | at-least-once *iff client retries on `5xx`* | HTTP status | `202` = accepted, not processed |
| **I2** sync gateway | request-scoped; one response per request | reply / `504` timeout | process-local correlator suffices |
| **O1** webhook | at-least-once (runtime retries transient) | status classification (`4xx`=`Permanent`) | reliability runtime-owned |
| **O2** request-reply | request-scoped (synchronous response) | HTTP response | Return Address by construction |
| **S-out** SSE server | at-most-once (fan-out, no ack) | none | per-process; drop on slow/disconnect; opt-in per-process replay ring (best-effort, C1) |
| **S-in** SSE client | at-most-once, best-effort resume | none | `Last-Event-ID` resume, not a guarantee |

**Phase-1 status contract as shipped** (`DefaultErrorStatus`, overridable via `WithErrorStatus`): I1 success =
`WithSuccessStatus` (default `202`); I2 success = **always `200`**. Failures for both: `413` oversize body / `400` any
other decode fault / `504` `msgin.ErrReplyTimeout` / `503` `msgin.ErrGatewayClosed` / `409`
`msgin.ErrDuplicateCorrelation` (reachable only under `WithTrustedCorrelationID`) / `500` `msgin.ErrNoCorrelation`,
`ErrUnsupportedPayload`, `ErrWriteResponse`, a **recovered flow panic**, and anything unclassified (including a `nil`
error). Neither core retries: `ServeAsync` calls `Send` exactly once per request, so I1's at-least-once holds only if
the *client* retries on `5xx`. A body-write failure after the committed `200` is logged, never restated as a status.

## 6. Phasing (one plan / one merged increment per phase)

| Phase | Plan | Content | Depends on |
|-------|------|---------|------------|
| **1** ✅ **DELIVERED** | [020](../plans/020-http-adapter-inbound.md) | `adapter/http` shared encode core + `adapter/http/stdlib` inbound (I1, I2) → `http.Handler`; ADR 0023 (+ Addendum A) | ADR 0022 |
| **2** ✅ **DELIVERED** | [024](../plans/024-http-outbound.md) | `adapter/http` outbound (O1 webhook `OutboundAdapter`, O2 `RequestReplyExchange`); ADR 0023 (+ Addendum B) | Phase 1; Plan 023 (`WithProducerRetry`) |
| **3** | 025 | `adapter/http` shared SSE core (`sse.go`) + `adapter/http/stdlib` SSE server (S-out); ADR 0023 (+ Addendum C) | Phase 1 |
| **4** | 026 | `adapter/http/stdlib` SSE client (S-in, `StreamingSource`) | Phase 3 (the `sse.go` core + Addendum C) |
| **5** | 027 | `adapter/http/gin` module — gin bindings for I1/I2/S-out + `RegisterRoutes`; ADR 0024 (gin dependency) | Phases 1, 3 |

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

**Phase 1 as delivered:** both new packages are at **100% statement coverage**, blackbox `_test` packages with
assert-closure tables, `goleak.VerifyTestMain` in each, and `ExampleNewInbound`/`ExampleNewInboundGateway` doubling as
godoc. Beyond the plan's enumerated branches, the delivered suite pins the review-driven behavior: the client cannot
choose the response media type (an end-to-end `text/html` echo attempt over `httptest.Server` asserting the
`application/octet-stream` + `nosniff` response), advisory-vs-trusted correlation (advisory populates
`http.correlation-id` but not the exchange key; trusted does key the exchange; an empty trusted value falls back to
`msg.ID()`; a `nil` resolver never clobbers an earlier one), panic containment end-to-end plus the
`http.ErrAbortHandler` re-panic (asserting it is **not** logged), a post-commit panic that must not restate the
status, an `ErrWriteResponse` write failure via a failing `ResponseWriter`, the allow-list slices being cloned against
later caller mutation, `DefaultErrorStatus` per-arm, and both cores driven with a **hand-built `&Config{}`** to prove
the nil-safe accessors.

**Phases 3/4 additions (planned):** the shared parser gets exhaustive WHATWG table tests plus a **`FuzzSSEParser`**
fuzz target (an untrusted-input parser is the canonical fuzz surface); heartbeat and reconnect backoff are driven by
`clockwork.NewFakeClock` — no real sleeps; and — per the measure-interleaving rule — the **slow-client-drop arm** and
the **replay-snapshot/register race arm** get *instrumented* tests proving the target arm actually executes, not
merely that the test passes. SSE server and client are tested against each other over `httptest.Server` (the resume
path end-to-end: server ring + client `Last-Event-ID`). Coverage target: 100% on both packages (the Phase 1/2
precedent).

## 8. Traceability

- **Realizes:** [Spec 001 §9](001-messaging-core.md) (the HTTP adapter), extended with SSE + dual binding.
- **Depends on:** [ADR 0001](../adrs/0001-message-payload-typing.md), [ADR 0002](../adrs/0002-adapter-spi.md),
  [ADR 0003](../adrs/0003-multi-module-repository-layout.md), [ADR 0006](../adrs/0006-resilience-flow-control.md),
  [ADR 0022](../adrs/0022-messaging-gateway.md) / [Spec 010](010-messaging-gateway.md).
- **New ADRs:** [ADR 0023](../adrs/0023-http-channel-adapter.md) (HTTP adapter architecture — with Plan 020; **Addendum
  A** records the Phase-1 review-driven design changes, **Addendum B** the Phase-2 outbound delivery decisions,
  **Addendum C** the Phases-3+4 SSE design decisions C1–C6), ADR 0024 (gin dependency — with Plan 027).
- **Plans:** [020](../plans/020-http-adapter-inbound.md) (Phase 1 — **delivered**),
  [024](../plans/024-http-outbound.md) (Phase 2), 025 (Phase 3), 026 (Phase 4), 027 (Phase 5). Plan 021 is
  [Spec 012](012-exchange-panic-safe-cleanup.md) — the core `ChannelExchange` panic-safe-cleanup fix, which lands first
  and tightens the `RequestReplyExchange` contract Phase 2's O2 implements.
- **Spawned follow-up, now resolved:** [Spec 012 — panic-safe `ChannelExchange` cleanup](012-exchange-panic-safe-cleanup.md)
  — the core-side correlator-slot leak surfaced (and, at Phase-1 delivery, contained but not fixed) by Phase 1's panic
  recovery; fixed by [Plan 021](../plans/021-exchange-panic-safe-cleanup.md).
