# ADR 0023 — HTTP channel adapter architecture (framework-agnostic core; stdlib & gin bindings; Return Address by construction)

- **Status:** **Accepted** (2026-07-21) for Phase 1 (inbound server), which shipped per
  [Plan 020](../plans/020-http-adapter-inbound.md); **Accepted** (2026-07-22) for Phase 2 (outbound O1/O2), which
  shipped per [Plan 024](../plans/024-http-outbound.md) with
  **[Addendum B](#addendum-b--phase-2-outbound-delivery-decisions)**; the Phases-3+4 SSE design decisions are
  **Accepted** (2026-07-22, design-time) in **[Addendum C](#addendum-c--sse-design-decisions-phases-34)**, with
  Phases 3–5 otherwise **Proposed** pending their plans. The architecture below held
  unchanged through implementation, but the whole-branch `/code-review` + `/security-review` gate forced six
  decisions the audited design did not anticipate — recorded in **[Addendum A](#addendum-a--review-driven-design-changes-phase-1-delivery)**
  rather than by silently editing the Decision section (Nygard convention: supersede/append, never rewrite). One of
  the six (**A2**, correlation-id trust) is an **architectural reversal**, not a refinement, and is labelled as such.
  Originally recorded as: Proposed (2026-07-21) — records the architectural decisions of
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
- **Scoped follow-on ADR:** **ADR 0024** — the `gin` dependency justification (authored with Plan 027, Phase 5). This
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
  (fresh `HeaderMessageID` + timestamp), **not** `NewMessage` (which is for reconstructing a stored msgin envelope verbatim).
  Payload = request body as **`[]byte`**; ~~`HeaderContentType` ← `Content-Type`~~ (⚠️ **superseded by
  [Addendum A1](#a1--the-client-never-chooses-the-response-media-type-security)**: the client's `Content-Type` lands on
  the non-reserved `http.content-type`); method/path/query recorded under non-reserved `http.*` header keys; only
  **allow-listed** client headers are copied.
- **Correlation id** — ⚠️ **SUPERSEDED BY [ADDENDUM A2](#a2--correlation-id-always-server-minted-advisory-and-trusted-split-security-reversal)**;
  `WithCorrelationID` does not exist in the shipped API. As originally decided: resolved by
  `WithCorrelationID(func(*http.Request) string)`; **default = the message's own fresh
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
  before construction, so a client cannot forge `msgin.correlation-id`, `msgin.message-id`, `msgin.delivery-count`, etc.

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

**Open point (RESOLVED in [Plan 023](../plans/023-producer-outbound-retry.md), recorded here):** the producer path now
applies a `RetryPolicy` to `OutboundAdapter.Send` via `WithProducerRetry` ([Spec 013](../specs/013-producer-outbound-retry.md)
/ [ADR 0025](0025-producer-outbound-retry.md)), so the adapter adds **no** backoff (reliability stays runtime-owned) and
`cenkalti/backoff` is not imported into the adapter. Backoff is never adapter-private state. (Plan 024's O1/O2 are the
first adapters driven by this producer retry; see Addendum B.)

### 6. Delivery guarantees, documented per mode

I1 at-least-once *iff the client retries on `5xx`* (`202` = accepted, not processed); I2 request-scoped; O1
at-least-once (runtime retries transient); O2 request-scoped; S-out at-most-once (fan-out, no ack, per-process); S-in
at-most-once with best-effort `Last-Event-ID` resume. (Spec 011 §5.)

### 7. Security posture — inbound is the untrusted boundary

Body cap (`http.MaxBytesReader`, default 1 MiB), reserved-header stripping, header allow-listing (both directions),
correlation-id distrust by default, CRLF-sanitized response headers, SSRF invariant (outbound URL never derived from
payload), caller-owned server hardening (documented), `WithMaxConnections` on the SSE server, no secret logging. A
dedicated `/security-review` runs on Phase 1 (inbound) and Phase 3 (SSE server). (Spec 011 §4.)

Phase 1's security review **strengthened** this posture beyond what was decided here — see
[Addendum A1](#a1--the-client-never-chooses-the-response-media-type-security) (nosniff + `application/octet-stream`
default; the client's `Content-Type` demoted to inert `http.content-type` metadata),
[A2](#a2--correlation-id-always-server-minted-advisory-and-trusted-split-security-reversal) (correlation key always
server-minted; advisory/trusted split), [A3](#a3--nil-safe-config-accessors-replace-the-planned-statusfor-helper) (no
panic on a hand-built `Config`) and [A5](#a5--panic-recovery-at-both-handler-cores-fault-isolation-and-the-residual-it-cannot-fix)
(panic containment; its correlator-slot residual is now resolved — see A5). "Caller-owned server hardening
(documented)" is now discharged concretely: the `adapter/http/stdlib` **package godoc** carries a "Deploying these
handlers safely" section with the required `http.Server` timeouts and the explicit no-authn/authz/CSRF/CORS/method
statement, and both constructors' godoc point at it.

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
- ADR 0024 admits `github.com/gin-gonic/gin` to the `adapter/http/gin` module (Plan 027).
- The async-callback request-reply variant (cross-instance Return Address) is a named future increment.
- [Plan 023](../plans/023-producer-outbound-retry.md) resolved the outbound-retry open point (§5); Plan 024 (Addendum B)
  delivers the O1/O2 adapters that consume it.
- [Spec 012 — panic-safe `ChannelExchange` cleanup](../specs/012-exchange-panic-safe-cleanup.md): the core-side
  correlator-slot leak that Phase 1's panic recovery contained but could not fix — **resolved**, see the
  resolution note appended to Addendum A5.

---

## Addendum A — review-driven design changes (Phase 1 delivery)

- **Status:** Accepted (2026-07-21), appended after Plan 020's Phase-1 implementation.
- **Prompted by:** the mandatory whole-branch `/code-review` + `/security-review` gate over `main..HEAD`
  (CLAUDE.md Development workflow §5), run on branch `feat/http-adapter-inbound`. Each item below is a decision the
  **audited** design (spec + this ADR + Plan 020, two-stage Opus audit) did not anticipate, recorded with the proven
  attack or defect that forced it.
- **Scope:** Phase 1 only (`adapter/http`, `adapter/http/stdlib`). The architecture in the Decision section above —
  framework-agnostic core + thin bindings (§1), per-mode SPI mapping (§3), Return Address by construction (§4),
  runtime-owned outbound reliability (§5) — is **unchanged**; nothing here is a core change. Spec 011 §3.2/§3.3/§3.6/
  §4/§5/§7 carry the same content in specification form.
- **Reversal vs refinement:** **A2 is a genuine architectural reversal** of §2's correlation-id decision — the shipped
  API removes the ability the ADR granted (a resolver that *decides* the exchange key) and replaces it with two
  differently-named options of different trust. A1 and A3–A6 are refinements/hardening within the existing decision.

### A1 — the client never chooses the response media type (security)

**Decision.** The client's request `Content-Type` is recorded on the **non-reserved `http.content-type`** header, never
on the reserved `msgin.HeaderContentType`. `EncodeResponse` takes the response `Content-Type` from the **flow's**
`msgin.HeaderContentType`, defaulting to **`application/octet-stream`** when absent/empty, and **always** sets
`X-Content-Type-Options: nosniff` — after the response allow-list, so an allow-listed message header cannot weaken it.

**Why.** The security review proved a **reflected-XSS** path on the (canonical, documented) echo-shaped gateway flow:
the client `POST`ed `Content-Type: text/html` with a `<script>` body; decode copied that value into
`msgin.content-type`; the echo reply carried it back; `EncodeResponse` trusted it and served the client's own script
as executable HTML from the endpoint's origin. Leaving `Content-Type` unset was not an option either — `net/http`'s
`DetectContentType` would then sniff `text/html` out of the same body.

**Consequences.** The response media type is now a **flow** decision, never a client one; a flow that wants a specific
type sets `msgin.HeaderContentType` on the reply. A flow that sets nothing gets a non-renderable
`application/octet-stream` — a deliberately conservative default per CLAUDE.md's safe-default gate. Client
`Content-Type` remains available to the flow (as inert metadata) for routing/logging.

### A2 — correlation id: always server-minted; advisory and trusted split (security **reversal**)

**Decision.** `msgin.HeaderCorrelationID` — the key the exchange correlates the reply on — is **always** the message's
own server-minted `msg.ID()`. The planned `WithCorrelationID` is **removed** and replaced by two orthogonal options:

- `WithAdvisoryCorrelationID(f)` — **advisory only**: populates the non-reserved `http.correlation-id` header for the
  flow to read/log/echo. No authority over the exchange key.
- `WithTrustedCorrelationID(f)` — the **sole** path to a client-keyed exchange, carrying a `SECURITY WARNING` godoc
  naming the attacks and the preconditions (values unguessable, single-use per client, endpoint authenticated).

They are disjoint in header, disjoint in trust, may both be set, and neither overrides the other. An empty trusted
value falls back to the server-minted id — never to the advisory value. A `nil` resolver is a no-op on both.

**Why.** The security review proved two attacks against the original single-option design, in which the resolver's
return value *became* the exchange key. **(1) Reply hijack:** a peer that can guess or replay another client's
correlation value registers the waiter the victim's reply is delivered to, and reads the victim's response body — the
correlator has no notion of *which* connection owns a key. **(2) Targeted denial:** a peer that pre-registers a
victim's value makes every victim request fail `msgin.ErrDuplicateCorrelation` → `409`. The original design's
"client-supplied ids are trusted only when the caller opts in" was true but insufficient: one option served both the
harmless use case (*surface the client's id to the flow*) and the dangerous one (*let the client key the correlator*),
so a caller reaching for the former silently got the latter.

**Consequences.** The common need — the client's id visible to the flow — is now served by an option that **cannot**
be a security bug. The dangerous capability survives, deliberately, behind a longer name and an explicit warning
(msgin does not remove capabilities its users may legitimately need). This is a **breaking change against the audited
design only**, not against any released API: the option had not shipped. `409`/`ErrDuplicateCorrelation` becomes
unreachable on the default path, which the delivered godoc states.

### A3 — nil-safe `Config` accessors replace the planned `statusFor` helper

**Decision.** The planned single `statusFor(cfg, err)` helper is dropped in favor of **per-field, nil-safe accessors**
on `*Config` (`maxBody`, `successStatusOrDefault`, `log`, `errStatus`, `advisoryCorrelationID`,
`trustedCorrelationID`, `allowedRequestHeaders`, `allowedResponseHeaders`), each back-filling the documented default
for a `nil` receiver or a zero/nonsensical field. `NewConfig` remains the only validating constructor.

**Why.** `Config` is exported with unexported fields, so `&msghttp.Config{}` and a `nil *Config` are constructible
from **any** package and reach every exported consumer (`DecodeRequest`, `EncodeResponse`, `ServeAsync`,
`ServeGateway`). The plan's design read `cfg.errorStatus`/`cfg.successStatus` directly, on the premise that a `Config`
could only come from `NewConfig` — a false premise, and a **nil-pointer panic** on caller input, which CLAUDE.md's
debuggability rule forbids outright. (A zero `successStatus` would additionally have panicked inside `net/http`.)

**Consequences.** More small methods than the plan's one helper, but every read site is panic-proof and the default is
stated exactly once per setting. A hand-built `Config` is **tolerated, not supported** — no validation runs — and the
godoc says so.

### A4 — new exported symbols: `DefaultErrorStatus`, `ErrDecodeRequest`, `ErrWriteResponse`

**Decision.** The planned unexported `defaultErrorStatus` is exported as **`DefaultErrorStatus(error) int`**; the
planned unexported `decodeError` struct is replaced by the sentinel **`ErrDecodeRequest`**; a new sentinel
**`ErrWriteResponse`** marks a post-commit body-write failure (see A6).

**Why.** `WithErrorStatus` hands the caller total responsibility for the mapping, but the discriminations that matter —
413-vs-400 (an oversize `*http.MaxBytesError` vs any other decode fault) and the gateway sentinel arms — were
**unreachable from outside the package**: the decode error type was unexported, and the default mapping was too. A
caller adding a single `myapp.ErrForbidden → 403` rule had to reimplement (and drift from) everything else. This is
also CLAUDE.md's "export what a test must assert" rule: these sentinels are the debuggability surface.

**Consequences.** Three more exported symbols on a still-small surface; `WithErrorStatus`'s godoc now shows the
delegate-to-`DefaultErrorStatus` pattern as the recommended form. Purely additive → minor SemVer.

### A5 — panic recovery at both handler cores (fault isolation), and the residual it cannot fix

> **Heading retained as the stable anchor target** (ADR 0022 Addendum A and Spec 012 both link to it). The residual it
> names is **RESOLVED** — see the "Resolved." paragraph below. The `recover()` boundary itself is still required.

**Decision.** `ServeAsync` and `ServeGateway` each install a deferred `recover()` boundary over a `responseTracker`
wrapper: a panic is logged with its stack through the injected logger and answered with a plain `500` **when the
response has not been committed**; the tracker also drops a second `WriteHeader`. **`http.ErrAbortHandler` is
re-panicked**, honoring `net/http`'s documented silent-abort contract.

**Why.** CLAUDE.md's fault-isolation constraint ("a panicking handler must not take down the flow; recover at endpoint
boundaries") applies here because the flow runs **on the request goroutine** — I1's `target.Send` runs a
`DirectChannel` subscriber inline, and I2's `Exchange` sends on the request channel inline. Neither the spec nor the
plan named a recovery boundary. Code review also found that a naive recover would produce a superfluous second
`WriteHeader` when the reply's `200` was already out — hence the commit tracking.

**Consequences.** A consumer's panicking handler now costs one failed request instead of the process. **Residual,
recorded honestly:** the recover **cannot reclaim the exchange's reply-waiter slot**. `msgin.ChannelExchange.Exchange`
registers the waiter *before* it sends, and its `giveUp` cleanup is **not `defer`red**, so a panic unwinding through
the send leaves the correlator map entry and its channel behind — with or without this recover. Impact and limits: on
the default path it is **memory-only** (A2 guarantees a fresh key per request, so no slot is ever re-keyed and no
later request can collide); the `409`-poisoning variant additionally requires the opt-in `WithTrustedCorrelationID`
with a reused client value. Either way it takes a panicking handler — a bug in the consumer's own code. The fix is
**core-side** (`exchange.go`), was deliberately scoped out of this branch by the user, and is tracked as
[Spec 012](../specs/012-exchange-panic-safe-cleanup.md). Until it lands, **the recover must not be removed** — the
adapter godoc says so at the recovery site.

**Resolved.** `ChannelExchange.Exchange` now reclaims its reply-waiter slot on every exit path, including a panic
unwind, per [ADR 0022 Addendum A](0022-messaging-gateway.md#addendum-a--panic-safe-cleanup-2026-07-21) (Spec 012 /
Plan 021). The recover at this boundary was never *only* about the slot — it is the fault-isolation guard that stops
a panicking flow handler from taking down the server regardless of what the exchange does with its own state — so it
stays, unchanged, now with no correlator-slot caveat attached to it.

### A6 — post-commit write failure is `ErrWriteResponse`, and is logged only

**Decision.** In `EncodeResponse`, a body-write failure occurring **after** the `200` status line is committed is
wrapped as `ErrWriteResponse` and returned; `ServeGateway` recognizes it via `errors.Is` and **logs only**, never
writing a second status. Every other `EncodeResponse` error (e.g. `ErrUnsupportedPayload`) is returned **before**
anything is written, leaving the `ResponseWriter` untouched so a clean status can still be sent.

**Why.** The plan's `ServeGateway` mapped *any* `EncodeResponse` error to a status write. Audit item L4 had already
established that the pre-write errors leave `w` clean — but the post-write case (typically a client that hung up
mid-body) has no clean status to write: doing so is a protocol error and produces `net/http`'s "superfluous
response.WriteHeader call". The error's *identity* is the structural signal of which arm the caller is in.

**Consequences.** A caller writing its own binding gets an unambiguous, `errors.Is`-testable "the response is already
committed" marker. `ErrWriteResponse` maps to `500` in `DefaultErrorStatus` for completeness, but on the delivered
path that status is never actually written.

---

## Addendum B — Phase 2 (outbound) delivery decisions

- **Status:** Accepted (2026-07-22), appended with [Plan 024](../plans/024-http-outbound.md) (Phase 2 — the O1 webhook
  `OutboundAdapter` and O2 `RequestReplyExchange`), authored ahead of the code and audited across three adversarial
  rounds before implementation.
- **Prompted by:** four decisions Phase 2 makes that the Decision section above did not anticipate, plus two settled
  user deviations (B5, B6). Each **amends an existing section of this ADR** rather than deciding something it was silent
  about — recorded here as an Addendum (the established shape, mirroring Addendum A) rather than silently editing the
  Decision section or minting a separate ADR that would leave §1/§4/§5/§7 stating things no longer true.
- **Scope:** Phase 2 outbound only (`adapter/http`; O1/O2). The architecture in the Decision section is **unchanged** —
  reliability stays runtime-owned (§5, as delivered by [Plan 023](../plans/023-producer-outbound-retry.md) /
  [ADR 0025](0025-producer-outbound-retry.md)); the adapter performs one attempt and only classifies. Spec 011
  §3.0/§3.1/§3.4/§3.6/§4/§6 carry the same content in specification form; Plan 024's INV-1..INV-7 are the verifying
  invariants.

### B1 — outbound redirect policy: no-follow by default (amends §7)

**Decision.** Every client the adapter uses has a non-nil `CheckRedirect` returning `http.ErrUseLastResponse`, so a
remote `3xx` is **classified** (→ `Permanent`), never followed. It is installed on a **shallow copy** of the caller's
`*http.Client` (Transport and Jar stay shared by pointer — connection pooling and cookies preserved) resolved **once at
construction**, never per-`Send`. `WithFollowRedirects()` opts out; a caller-supplied client whose own `CheckRedirect`
is already non-nil **wins** (their explicit choice is honored — decision 1).

**Why.** §7's one-line "SSRF invariant (outbound URL never derived from payload)" is **false in practice** against the
default `net/http` client, which follows up to 10 redirects: `validateURL` runs once at construction, so a
`302 → http://169.254.169.254/latest/meta-data/iam/security-credentials/` makes **O2 return instance-metadata
credentials into the flow as the reply payload**, and a `307`/`308` **replays the POST body and every allow-listed
header** to the attacker's host (Go strips only `Authorization`/`Cookie`), including an `https → http` downgrade. This is
new exported security API (a new option) and a genuine hardening of §7, not a refinement (Plan 024 INV-1).

**Consequences / non-guarantee.** The invariant is **not absolute** and its two escapes must be named wherever it is
stated (godoc, Spec 011 §4, here): the caller owns the SSRF risk they re-open with `WithFollowRedirects()` or a
following `CheckRedirect`. Independently, msgin performs **no** private-IP/link-local/loopback/metadata-endpoint
filtering — it prevents **message-driven** SSRF only. `NewOutbound("http://169.254.169.254/…")` issues exactly that
request.

### B2 — the outbound reflected-XSS reversal (amends §2 and §7; mirror of A1, opposite direction)

**Decision.** A `Content-Type` read from an untrusted remote `*http.Response` lands on the **non-reserved
`http.content-type`** header, **never** on the reserved `msgin.content-type`. This is the exact mirror of Addendum A1 —
which demoted the *client's* request `Content-Type` to `http.content-type` on the inbound side — applied to the
*remote server's* response on the outbound side.

**Why.** The remote server is untrusted input exactly as an inbound client is. Writing its `Content-Type` onto
`msgin.HeaderContentType` — the key `EncodeResponse` trusts as the response media type, and which `DecodeRequest`
deliberately refuses to let a client set — re-opens A1's **reflected XSS** from the outbound side: an upstream chooses
`text/html`, an `OutboundGateway` → `EncodeResponse` flow serves its bytes back executable, and
`X-Content-Type-Options: nosniff` does not stop an *explicit* `text/html` (Plan 024 INV-2).

**Consequences.** The response media type of a downstream `EncodeResponse` stays a flow decision, never a remote one.
The remote `Content-Type` remains available to the flow as inert `http.content-type` metadata.

### B3 — reply header provenance (amends §4)

**Decision.** O2's reply `Message[any]` is **seeded from the request message's headers** rather than built empty, so a
correlation id an `OutboundGateway` stamped on the request survives onto the reply and its save/restore is honored. The
reserved `msgin.message-id`, `msgin.timestamp` and `msgin.content-type` are **dropped and re-stamped fresh** (a reply
is a new envelope, and its media type is B2's `http.content-type`, never the request's).

**Why.** §4 established Return-Address-by-construction for synchronous request-reply but was silent on how the reply
message is constructed; building it empty would lose the correlation key `OutboundGateway` relies on, and copying the
request's `msgin.*` verbatim would forge a stale message id / timestamp and re-open B2.

**Consequences.** O2 slots into `OutboundGateway`'s correlate-and-restore contract without the caller re-threading the
correlation id, while the reserved-namespace hygiene of B2 is preserved on the reply path.

### B4 — package placement: O1/O2 live in `adapter/http`, not `adapter/http/stdlib` (amends §1)

**Decision.** `outbound.go` and `exchange.go` (O1 + O2) live in the framework-agnostic **`adapter/http`** package,
**not** the `adapter/http/stdlib` binding slot §1 reserved for the server-side constructors.

**Why.** §1's `adapter/http/stdlib` exists to bind the framework-neutral cores to `net/http` **server** types
(`http.Handler`, `*http.ServeMux`) — the place a gin binding has a peer for. An HTTP **client** has no framework
variant: gin has no HTTP client, so a `stdlib` outbound file would be an empty passthrough the gin module would then
have to re-export. O1/O2 are complete in `adapter/http` (package `msghttp`) with no binding layer.

**Consequences.** The three-package layout (§1) is unchanged in principle; the outbound files simply have no binding
tier. Spec 011 §3.0's layout block is corrected to match.

### B5 — `WithOutboundHeaders` strips reserved `msgin.*` names (decision 4; amends §7)

**Decision.** `WithOutboundHeaders` copies only an allow-listed set of message headers onto the outbound request, and
an entry whose name, lowercased, carries the reserved `msgin.` prefix is **silently dropped** — internal flow metadata
(`msgin.correlation-id`, `msgin.message-id`, …) is never published to the remote endpoint through this option. The
reserved-**name** guard is case-insensitive (a security filter, not casing-bypassable); the message-header **value**
lookup stays case-sensitive (an exact map key). `Content-Type` is written after the allow-list, so an entry naming it
cannot override it.

**Why (settled deviation).** §7 made the inbound **response** allow-list deliberately *asymmetric* — trusted operator
config, not reserved-guarded (`WithResponseHeaders`' `CAUTION`). Plan 024 originally carried that asymmetry to the
outbound request path. **The user chose SYMMETRY instead** (decision 4): reserved names are dropped on the outbound
request path too, matching `WithOutboundReplyHeaders` (B2/INV-2) and the inbound `DecodeRequest`. Recorded here as
symmetry, superseding the plan's original asymmetry recommendation.

**Consequences.** One consistent rule — reserved `msgin.*` names are never publishable to a remote peer through any
outbound allow-list option. A caller who genuinely needs to forward flow metadata does so under a non-reserved header
name of their own choosing.

### B6 — the opt-in `WithErrorBodyExcerpt` (decision 5; amends §7's INV-4 posture)

**Decision.** §7's default outbound-error posture is **"status code only"** — `StatusError` carries only the `int` code,
no URL, no body, no `resp.Status` text — so no remote-controlled bytes reach caller logs. `WithErrorBodyExcerpt()` opts
into a **bounded, fully-sanitized** excerpt of the remote response body on `StatusError.Excerpt`: at most
`errorBodyExcerptMax` (**256**) bytes are **read** from the body (cap the READ — a DoS-amplification bound, not merely
an output-length bound), then sanitized at the **rune** level with `strconv.Quote`, which escapes every
non-`unicode.IsPrint` rune (C0/C1, ANSI `\x1b`, bidi `U+202E`, `U+2028`/`U+2029`, NEL, BOM) and invalid UTF-8 to
`\uXXXX`/`\xNN`, **preserves** printable accented/CJK/emoji runes, and delimits the excerpt in quotes.

**Why (settled deviation).** Plan 024 originally recommended **deferring** this capability. **The user chose to ship it
now** (decision 5). Because it opens a **new remote-controlled-bytes → error-string path** — the exact boundary INV-4
and INV-5 guard — it is specified with a **stricter-than-`sanitizeHeaderValue`** sanitizer (`sanitizeExcerpt`, which is
`strconv.Quote`, not the inbound CR/LF-only strip) and was subjected to a focused round-3 adversarial audit before code.
That audit corrected the sanitizer from a byte-level C0/C1 strip (which corrupts legitimate printable runes whose UTF-8
continuation byte lands in `0x80`–`0x9f` and lets `U+FEFF`/bidi survive) to the rune-level `strconv.Quote` primitive.

**Consequences.** INV-4's §7 posture is amended from **"code only"** to **"code only, or opt-in bounded+sanitized
excerpt"**. The excerpt deliberately surfaces remote-influenced bytes into caller logs — safe to render under the
rune-level sanitizer, but still untrusted when parsed. The transport-error path (INV-5) independently discards the
target URL's userinfo/path/query via `ErrOutboundTransport`, so a webhook token in the query string never leaks into a
logged timeout.

## Addendum C — SSE design decisions (Phases 3+4)

- **Status:** Accepted (2026-07-22), authored at design time from the Phases-3+4 **combined brainstorm** (one design
  pass over both SSE halves — they share the `sse.go` core — split into Plans 025/026 with one merged increment each,
  in phase order), **ahead of the plans and the adversarial audit**. The audit and delivery may append here, mirroring
  Addenda A/B.
- **Prompted by:** four decisions settled with the user (C1–C4) plus two design-time security decisions (C5, C6) that
  the Decision section (§3's mode table, §6, §7) did not anticipate. The architecture is **unchanged**: S-out stays
  `OutboundAdapter` **and** `http.Handler`, S-in stays `StreamingSource` (§3); §6's at-most-once contracts stand.
  [Spec 011 §3.5/§3.6/§4/§7](../specs/011-http-adapter.md) carry the same content in specification form.

### C1 — server replay: opt-in bounded ring, per-process, best-effort (amends §6)

**Decision.** `WithReplayBuffer(n)` (default **off**; `n <= 0` explicit → `ErrInvalidReplayBuffer`) keeps a bounded
ring of the last `n` sent events. A `GET` carrying `Last-Event-ID` has the ring tail *after* that id snapshotted and
the connection registered **in the same hub critical section** — no missed-event window between replay and live. An
evicted/unknown id yields live-only. Ids are matched by **identity** (the ring is scanned for the exact id), never by
order — `HeaderMessageID` UUIDs are not orderable.

**Why (settled with the user).** §6 declared S-in "best-effort `Last-Event-ID` resume", but the spec'd server held
**no history** — resume against our own server was silently a no-op, an undecided cross-component contract between
Plans 025 and 026. The alternatives — ignore-and-document, or defer to the plan audit — were rejected in favor of a
real, bounded, opt-in resume for short reconnect blips.

**Consequences.** Delivery stays **at-most-once**: the ring is per-process, so a reconnect landing on another instance
finds an empty buffer — the CLAUDE.md multi-instance rule's documented invariant (a gap-free deployment needs a
durable/shared backbone feeding every instance; the SSE server is the last hop). Memory is bounded at `n` events of
locally-produced size (the flow, not a remote, controls event size on this side).

### C2 — heartbeat off by default (amends the defaults posture in §7's spirit)

**Decision.** `WithHeartbeat(d)` (`d <= 0` explicit → `ErrInvalidHeartbeat`) writes `: ping` comment frames from the
connection's existing writer goroutine (clockwork-driven; no extra goroutine per connection). **Off by default** — a
user-settled deviation from the default-to-safe leaning (an always-on ~15 s ping). Per the defaults policy's
documented-off requirement, `NewSSEServer`'s godoc must state plainly that idle streams behind proxies/LBs with
30–60 s idle timeouts are silently dropped unless heartbeat is enabled.

### C3 — client conformance: full WHATWG EventSource semantics (amends §3's S-in row)

**Decision.** The client honors the standard's processing model: **`retry:`** sets the reconnect delay, **clamped**
into `WithReconnectBackoff`'s `[min, max]` — a hostile server can neither force a hot loop (`retry: 0`) nor hang the
source (`retry:` huge); **HTTP `204`** is the terminal stop signal (`Stream` returns nil, no reconnect); a 2xx with a
`Content-Type` other than `text/event-stream` is `ErrNotEventStream` (reconnect-with-backoff, never a parse of
garbage); an **empty `id:` line clears** the held last-event-id. Rejected: a reconnect-only subset (diverges from
every standard SSE producer and can loop forever against a server that said stop) and per-rule-configurable
conformance (three options and test axes nobody asked for — YAGNI).

### C4 — server hub: mutex registry + one writer goroutine per connection (amends §3's S-out row)

**Decision.** A `sync.Mutex`-guarded connection set; each connection owns a bounded channel (`WithConnectionBuffer`,
default **16**) drained by its single writer goroutine (encode → `Flush`; heartbeat frames from the same goroutine).
`Send` **non-blocking-enqueues** to every connection — a full buffer triggers `WithSlowClientPolicy` (**default
drop-and-continue**, counted via the observability hook; alternative disconnect). `Close(ctx)` stops intake (new
requests → 503), cancels connections, joins all writers within the deadline. Rejected: a central hub goroutine (an
always-on goroutine, an extra hop per event, and C1's atomic snapshot+register would need a synchronous round-trip
into it) and lock-held synchronous fan-out (one slow client blocks the whole broadcast — violates the backpressure
invariant outright).

**Amendment (audit rounds 1–3, 2026-07-23).** SSE forces `http.Server.WriteTimeout: 0`, so a writer blocked inside
`Write` on a stalled reader's socket cannot be reaped by cancelling the connection's derived ctx — `Close`'s join
and the disconnect policy would leak the goroutine + connection slot (audit BLOCKER-1). Added **`WithWriteTimeout`**
(default **30 s**; `d <= 0` → `ErrInvalidWriteTimeout`): before every `Write`/`Flush` the writer sets a per-write
OS deadline via `http.ResponseController.SetWriteDeadline` — a real physical-time deadline (distinct from the
logical heartbeat; the socket deadline cannot use `clockwork`). A deadline-exceeded write ends the writer loop and
unregisters; a `ResponseWriter` returning `errors.ErrUnsupported` proceeds best-effort with a one-time latched WARN
(F5) so a voided protection is observable. The cap is **process-global**, not per-IP (audit NIT-2).

### C5 — SSE field injection: reject, not sanitize (new security decision; extends §7)

**Decision.** `EncodeSSEEvent` **rejects** an `id`/`event` value containing CR, LF, or NUL with
`ErrInvalidEventField`; on the `Send` path the error is classified `msgin.Permanent` (a retry cannot fix a bad
header). A message header reaching `id:`/`event:` unchecked could forge extra fields or whole events into every
subscriber's stream — the SSE analog of the header-injection boundary A1/B2 guard, in the outbound-to-subscriber
direction. Rejection over sanitization: a silently altered id would break C1's identity-matched replay and mask the
flow bug that produced it.

**Amendment (plan time / audit rounds 1–3, 2026-07-23).** The message→event conversion **`SSEEventFromMessage`** is
**exported** (Phase 5's gin S-out binding reuses it without importing `stdlib`; blackbox-testable). `Send`'s
payload accepts `[]byte` **or** `string` (the shared `payloadBytes` contract), not `[]byte` only. Encode emits
`data:` + one space + line uniformly (the parser strips one leading space, so a space-leading payload round-trips).
Added sentinels `ErrInvalidSlowClientPolicy` (unknown enum) and `ErrSSEServerClosed` (`Send` after `Close`,
`msgin.Permanent`).

### C6 — per-event byte cap on the parser, default 1 MiB (new security decision; extends §7)

**Decision.** The shared parser enforces a per-event byte cap — default **1 MiB**, overridable via `WithMaxEventBytes`
(the `WithMaxResponseBytes` precedent; an explicit `n <= 0` is a typed construction error) — returning
`ErrEventTooLarge` when exceeded. The client treats it as a connection fault (reconnect-with-backoff), never an OOM:
the remote is untrusted input, and without the cap a single endless `data:` line grows memory unbounded.

**Amendment (audit rounds 1–3, 2026-07-23).** The cap bounds `max(current-line-bytes, accumulated-data-buffer-bytes)`
— the only two things that actually buffer — not "bytes since a boundary" (audit MAJOR-4/F4). The line counter
resets at each line ending, the data buffer at each dispatch; comments, ignored fields, and blank lines never
accumulate, so a long-lived idle stream of `: ping` keep-alives never false-trips, while a single unterminated line
or an oversized multi-`data:` event still trips.
