# ADR 0023 — HTTP channel adapter architecture (framework-agnostic core; stdlib & gin bindings; Return Address by construction)

- **Status:** Proposed (2026-07-21) — records the architectural decisions of
  [Spec 011](../specs/011-http-adapter.md), settled with the user in brainstorming (one spec / five phased plans; both
  SSE directions; framework-agnostic core + separate gin module placed at `adapter/http/stdlib` and `adapter/http/gin`;
  Return Address satisfied by construction for synchronous HTTP request-reply). **Phase-1 adversarial design audit
  round 1 folded** (Opus, SOUND-WITH-NITS — architecture verified SOUND against the code): §2 clarified that inbound
  builds `msgin.New` (audit H1 was a spec-only slip), the inbound-`[]byte` no-codec-seam constraint documented
  symmetrically (M4), the I2 `200`/`WithSuccessStatus`-is-I1-only status model and the honest error→status mapping
  pinned (M1/L2/L4). No re-audit warranted; plan-level coverage fixes (M2/M3) folded into Plan 020.
- **Spec:** [Spec 011 — HTTP channel adapter](../specs/011-http-adapter.md).
- **Depends on / builds on:**
  - [ADR 0002 — Adapter SPI](0002-adapter-spi.md) — `PollingSource`/`StreamingSource` inbound seams,
    `OutboundAdapter` outbound seam, `Delivery`/`NativeReliability`, and the **runtime-owned reliability** boundary
    (adapters classify; `Consumer`/`Producer`/`RetryPolicy` decide).
  - [ADR 0022 — Messaging Gateway / `RequestReplyExchange`](0022-messaging-gateway.md) — the SPI both HTTP request-reply
    sides bind to; the multi-instance / Return-Address obligation this ADR discharges for the synchronous case.
  - [ADR 0001 — Payload typing](0001-message-payload-typing.md) — adapters see `Message[any]` with `[]byte` payloads;
    the typed runtime owns `PayloadCodec[T]`; header framing is the type-agnostic adapter concern.
  - [ADR 0003 — Multi-module layout](0003-multi-module-repository-layout.md) — the heavy-client-adapter-as-separate-module
    precedent (`database/pgx`) applied to the gin binding.
- **Scoped follow-on ADR:** **ADR 0024** — the `gin` dependency justification (authored with Plan 024, Phase 5). This
  ADR fixes the *architecture* that keeps gin isolated; ADR 0024 fixes the *dependency admission*.

## Context

msgin bridges to brokers/stores (memory, SQL; designed pgx/redis/nats) and a clock (cron), but not to **HTTP** — the
most common integration surface, and the concrete external transport the `RequestReplyExchange` SPI (ADR 0022) was
built to receive. Building it raises four architectural questions the one-way store adapters never did:

1. **Two framework idioms** (`net/http` and `gin`) must both be supported, but **`gin` is a third-party dependency the
   root module may not carry** (Dependency policy: core = stdlib + four ADR'd exceptions).
2. **Request-reply crosses a process boundary**, so the CLAUDE.md multi-instance rule demands an explicit Return-Address
   analysis: a reply must reach the instance that is blocked on the request.
3. **Inbound HTTP is untrusted input** — the first adapter whose *source* is an arbitrary network client, not a trusted
   store the operator provisioned.
4. **Outbound reliability** must stay runtime-owned (ADR 0002), not re-implemented as adapter-side backoff.

## Decision

### 1. Framework-agnostic core + two thin bindings; gin isolated in its own module

- **`adapter/http`** (root module, **stdlib only, zero new dependency**) — package **`msghttp`** (deliberately *not*
  `http`, which would shadow `net/http` at import sites). Holds **all real logic**: the `Config`/`Option`/`WithX`
  surface, `DecodeRequest`/`EncodeResponse`, and the framework-neutral `Serve*` handler cores that operate on
  `(w http.ResponseWriter, r *http.Request)` — the lingua franca both bindings already hold (`gin.Context` wraps the
  same two `net/http` values). It exposes an exported `Config` struct built once via `NewConfig(opts...)` so a binding
  validates options a single time and reuses the result.
- **`adapter/http/stdlib`** (root module, stdlib only) — package **`stdlib`**. Idiomatic `net/http` constructors
  (`NewInbound`/`NewInboundGateway` → `http.Handler`) plus a `ServeMux` registration helper. Thin by design: it
  composes `msghttp` cores into `http.Handler`s. It is a *peer binding*, not a duplicate — the parallel of the gin
  binding, and the home for stdlib-specific conveniences and a stable stdlib-facing surface.
- **`adapter/http/gin`** (**separate Go module**, own `go.mod` + `go.work` entry + module-path-prefixed release tag,
  exactly like `database/pgx`) — idiomatic `gin.HandlerFunc` constructors + `RegisterRoutes`, composing the **same**
  `msghttp` cores. Only gin users import it and pay gin's transitive cost.

**Why not the alternatives:**
- *Stdlib-only, gin via `gin.WrapH`* — zero gin dependency, but no idiomatic gin surface (no `*gin.Context`, params,
  middleware ergonomics). Rejected: it does not satisfy "implement using the gin handler", and offers no home for
  gin-native route registration.
- *A bespoke framework-abstraction interface* both frameworks implement — over-engineered for two frameworks;
  `net/http`'s `Handler`/`(w, r)` **is** already the abstraction, and `gin.Context` already exposes both. Rejected
  (YAGNI): the neutral currency is `(http.ResponseWriter, *http.Request)`, not a new interface.

### 2. `Message` ⇄ HTTP mapping and the type-agnostic boundary (ADR 0001)

- **Request → `Message[any]`**: the request is *external input entering the system*, so it is built with **`msgin.New`**
  (fresh `HeaderID` + timestamp), **not** `NewMessage` (which is for reconstructing a stored msgin envelope verbatim).
  Payload = request body as **`[]byte`**; `HeaderContentType` ← `Content-Type`; method/path/query recorded under
  non-reserved `http.*` header keys; only **allow-listed** client headers are copied.
- **Correlation id**: resolved by `WithCorrelationID(func(*http.Request) string)`; **default = the message's own fresh
  `ID()`** (reusing `New`'s generation — the adapter never needs msgin's unexported id generator, and every inbound
  request gets a unique, non-client-controlled correlation key). Client-supplied ids are trusted **only** when the
  caller opts in via the resolver.
- **Inbound payload is `[]byte`; there is no inbound codec seam (symmetric with the reply constraint).** I1/I2 hand the
  `Message[any]` (payload `[]byte`) **directly** to a `MessageChannel`/`RequestReplyExchange` — the `PayloadCodec[T]`
  decode step only exists on the `Consumer`-driven source path, not here. The flow's first step receives raw bytes and
  must consume them or begin with a decode/`Transform`; wiring a typed transform first yields `ErrPayloadType` at
  runtime. Documented on both sides so the boundary is not surprising in one direction only (audit M4).
- **`Message[any]` → response**: reply payload must be **`[]byte`** (or `string`) at the HTTP boundary — the adapter is
  type-agnostic (ADR 0001), so the flow encodes its reply to bytes (a terminal Transformer/codec). A non-bytes reply
  payload is a wiring fault → `ErrUnsupportedPayload` → `500`. **Bytes are extracted before any response header/status
  is written**, so the `500` never carries the flow's headers (audit L4).
- **I2 status model**: a successful request-reply is **`200`** (never `202`); `WithSuccessStatus` governs **I1 only**
  (audit M1). Error → status: `ErrReplyTimeout`→504, `ErrGatewayClosed`→503, `ErrDuplicateCorrelation`→409 (only when
  the caller opts into trusting client ids), decode→400 / oversize→413, and `ErrNoCorrelation`/`ErrUnsupportedPayload`/
  other→500 (server/wiring faults — the adapter always mints a non-empty id, so `ErrNoCorrelation` is not a client
  error; audit L2). Overridable via `WithErrorStatus`.
- **Reserved-header stripping**: client-supplied headers whose key has the reserved **`msgin.`** prefix are removed
  before construction, so a client cannot forge `msgin.correlation-id`, `msgin.id`, `msgin.delivery-count`, etc.

### 3. Per-mode → existing core SPI (the pattern core does not change)

| Mode | Binds to | Produces |
|------|----------|----------|
| I1 async inbound | `MessageChannel` | `http.Handler` (`202`) |
| I2 sync gateway | **`RequestReplyExchange`** | `http.Handler` (reply → response) |
| O1 webhook | `OutboundAdapter` | `POST`, runtime-retried |
| O2 request-reply | **`RequestReplyExchange`** | `Exchange` = sync `POST`+response |
| S-out SSE server | `OutboundAdapter` **and** `http.Handler` | fan-out `text/event-stream` |
| S-in SSE client | `StreamingSource` | reconnecting event stream |

I2 is an HTTP front door onto **any** `RequestReplyExchange` (a `ChannelExchange` yields HTTP-in → in-process
request/reply → HTTP-out with no new correlation code). O2 **is** a `RequestReplyExchange`, dropping into
`Gateway`/`OutboundGateway` (ADR 0022) unchanged. No new core seam is introduced.

### 4. Return Address — satisfied by construction for synchronous HTTP request-reply

The multi-instance rule (CLAUDE.md; ADR 0022 §8.1) requires that any request-reply component whose reply could arrive
at a *different* instance than the one blocked on the request must implement the EIP **Return Address** so the reply is
routed back. For **synchronous** HTTP request-reply — the only request-reply shape in scope (I2, O2) — the reply
returns **on the same TCP connection** the blocked instance holds open: the connection *is* the return address. The
cross-process correlation problem therefore **does not arise**, and the process-local gateway correlator (ADR 0022) is
sufficient. This is stated explicitly rather than left implied, per the multi-instance-awareness rule.

The shape that *would* need an explicit cross-instance Return Address — **async callback** request-reply (the request
carries a callback URL; the reply is delivered later to an inbound webhook that may land on a different instance) — is
**out of scope for v1** (Spec 011 §2) and named as the future increment. The SPI seam (`RequestReplyExchange`) is
already shaped to accept it without a core change: an async-callback exchange would mint an instance-addressable
callback URL as the Return Address and correlate the late reply through a durable store, exactly as ADR 0022
anticipated for external transports.

### 5. Outbound reliability stays runtime-owned; the adapter only classifies (ADR 0002)

`O1`/`O2` perform **one** HTTP attempt per `Send`/`Exchange` and translate the response into the error vocabulary the
runtime already understands:

- `2xx` → `nil`.
- `4xx` except `408`/`429` → **`msgin.Permanent(err)`** — the runtime must not retry (bad request/target); dead-letter
  or invalid-message path owns it.
- `5xx`, `408`, `429`, network error, timeout → **transient** error — the runtime retries per `RetryPolicy`.

**Open point (resolved in Plan 021, recorded here):** if the `Producer`/outbound path already applies a `RetryPolicy`
to `OutboundAdapter.Send`, the adapter adds **no** backoff (reliability stays runtime-owned). If it does not, Phase 2
adds a thin producer-side retry rather than importing `cenkalti/backoff` into the adapter. Either way, backoff is never
adapter-private state.

### 6. Delivery guarantees, documented per mode

I1 at-least-once *iff the client retries on `5xx`* (`202` = accepted, not processed); I2 request-scoped; O1
at-least-once (runtime retries transient); O2 request-scoped; S-out at-most-once (fan-out, no ack, per-process); S-in
at-most-once with best-effort `Last-Event-ID` resume. (Spec 011 §5.)

### 7. Security posture — inbound is the untrusted boundary

Body cap (`http.MaxBytesReader`, default 1 MiB), reserved-header stripping, header allow-listing (both directions),
correlation-id distrust by default, CRLF-sanitized response headers, SSRF invariant (outbound URL never derived from
payload), caller-owned server hardening (documented), `WithMaxConnections` on the SSE server, no secret logging. A
dedicated `/security-review` runs on Phase 1 (inbound) and Phase 3 (SSE server). (Spec 011 §4.)

## Consequences

**Positive**
- The pattern core is untouched — every mode is wiring to an existing SPI (Open/Closed). ADR 0022's `RequestReplyExchange`
  is validated by its first external transport.
- gin's dependency weight is confined to one opt-in module; stdlib users and the core stay stdlib-only, zero new
  transitive deps in the root module.
- Both bindings share one tested logic core; a bug fixed in `msghttp` is fixed for stdlib and gin at once.
- Fully hermetic, Docker-free tests (`httptest`) — faster and simpler than the SQL adapter's conformance kit.
- The Return-Address analysis is explicit and recorded, so a future async-callback increment inherits a clear seam and
  a documented starting point rather than a silent assumption.

**Negative / trade-offs**
- Three packages for what a naïve design would put in one (`adapter/http` + `stdlib` + `gin`); the stdlib binding is
  intentionally thin. Justified by symmetry with gin and the dependency-isolation requirement, but it is more surface.
- The `msghttp` package name differs from its directory (`adapter/http`) to avoid the `net/http` clash — a documented
  wrinkle at import sites.
- The type-agnostic reply constraint (reply payload must be `[]byte`/`string`) pushes response encoding onto the flow;
  convenient typed-response sugar is deferred (could be a later typed helper over the adapter, not in it).
- `gin` remains a *future* admitted dependency (ADR 0024); this ADR only guarantees it can be isolated when admitted.

**Follow-ups**
- ADR 0024 admits `github.com/gin-gonic/gin` to the `adapter/http/gin` module (Plan 024).
- The async-callback request-reply variant (cross-instance Return Address) is a named future increment.
- Plan 021 resolves the outbound-retry open point (§5).
