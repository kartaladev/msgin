# ADR 0023 — HTTP channel adapter architecture (framework-agnostic core; stdlib & gin bindings; Return Address by construction)

- **Status:** **Accepted** (2026-07-21) for Phase 1 (inbound server), which shipped per
  [Plan 020](../plans/020-http-adapter-inbound.md); Phases 2–5 remain **Proposed**. The architecture below held
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
- **Scoped follow-on ADR:** **ADR 0024** — the `gin` dependency justification (authored with Plan 025, Phase 5). This
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

**Open point (resolved in Plan 022, recorded here):** if the `Producer`/outbound path already applies a `RetryPolicy`
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
- ADR 0024 admits `github.com/gin-gonic/gin` to the `adapter/http/gin` module (Plan 025).
- The async-callback request-reply variant (cross-instance Return Address) is a named future increment.
- Plan 022 resolves the outbound-retry open point (§5).
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
