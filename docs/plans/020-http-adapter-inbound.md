# HTTP channel adapter — Phase 1: inbound server (stdlib) — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax.
>
> **Go skills are mandatory (CLAUDE.md writing-plans override):** every task starts from **`cc-skills-golang:golang-how-to`**, uses **`superpowers:test-driven-development`** (red→green→refactor), navigates/refactors via **`gopls`**, and obeys the project testing overrides: **`table-test`** (assert-closure tables, `t.Context()`), **`use-mockgen`**, **`use-testcontainers`** (not needed here — HTTP tests are hermetic via `httptest`). Blackbox `_test` packages only.

**Goal:** Ship Phase 1 of the HTTP channel adapter — the framework-agnostic core package `adapter/http` (`msghttp`) and the `net/http` binding `adapter/http/stdlib`, delivering the two inbound server modes: **I1** async inbound (`http.Handler` that emits a message onto a channel and replies `202`) and **I2** sync inbound gateway (`http.Handler` over any `RequestReplyExchange`, turning a request into a flow round-trip and encoding the reply as the HTTP response).

**Architecture:** All logic lives in `adapter/http` (`msghttp`), operating on `(w http.ResponseWriter, r *http.Request)` — the currency both the stdlib and (future) gin bindings share. `msghttp` exposes `Config`/`Option`/`WithX`, `DecodeRequest`/`EncodeResponse`, and two framework-neutral handler cores `ServeAsync`/`ServeGateway`. `adapter/http/stdlib` is a thin peer binding: `NewInbound`/`NewInboundGateway` compose those cores into `http.Handler`s, plus a `Register` mux helper. Inbound is the untrusted boundary — body cap, reserved-`msgin.*`-header stripping, header allow-listing, correlation-id distrust by default. No background goroutines are started (handlers run on the server's request goroutines), so there is nothing to leak; `goleak` still guards the tests.

**Tech Stack:** Go 1.25, stdlib only (`net/http`, `net/http/httptest`, `context`, `io`, `log/slog`, `strings`). The `msghttp` and `stdlib` packages are part of the **root module** (like `adapter/memory`, `adapter/cron`) — **no new `go.mod`, no `go.work` change, no new dependency.** (The separate gin module + its dependency land in Phase 5 / Plan 024 / ADR 0024.)

**Spec:** [Spec 011 — HTTP channel adapter](../specs/011-http-adapter.md) (§3.0/§3.2/§3.3, §4, §5, §6 Phase 1). **ADR:** [ADR 0023 — HTTP channel adapter architecture](../adrs/0023-http-channel-adapter.md). Builds on [ADR 0002 — Adapter SPI](../adrs/0002-adapter-spi.md), [ADR 0022 — Messaging Gateway](../adrs/0022-messaging-gateway.md) (`RequestReplyExchange`), [ADR 0001 — Payload typing](../adrs/0001-message-payload-typing.md).

## Global Constraints

- **Go 1.25 only** (`go.mod` `go 1.25`; build/test `GOTOOLCHAIN=go1.25.12`). No features newer than 1.25.
- **No new dependency.** `msghttp` + `stdlib` are stdlib-only, root-module packages. The whole-branch gate re-asserts `go mod tidy`/`go.sum` show **no** new module and `go mod verify` passes.
- **Purely additive public API** → **minor SemVer** (two new root-module packages; no change to any shipped symbol). Verify additions-only.
- **Adapter is type-agnostic (ADR 0001).** `msghttp` never mentions a payload type `T`; request bodies and reply payloads are `[]byte` (`string` tolerated on the reply). A non-bytes reply payload is a wiring fault → `ErrUnsupportedPayload` → `500`.
- **Untrusted-input boundary (Spec 011 §4).** Body cap via `http.MaxBytesReader`; strip client `msgin.*` headers before construction; allow-list request/response headers; correlation-id defaults to a server-minted id (the message's own `ID()`), never a client value unless the caller opts in; CRLF-sanitize response header values. A dedicated `/security-review` runs in Task 4.
- **No logging to a package global.** Default logger `slog.New(slog.NewTextHandler(io.Discard, nil))`; never `slog.Default()`. Never `os.Exit`/`log.Fatal`/`panic` on caller/request input — construction faults return typed errors (set-flag option pattern per `WithMaxInFlight`); request faults become HTTP status codes.
- **Blackbox tests** (`package msghttp_test`, `package stdlib_test`), **assert-closure tables**, `t.Context()`. `Example…` tests double as godoc. **`goleak`** `VerifyTestMain` in both packages (proves the handlers spawn no stray goroutines).
- **Coverage** ≥ 85% on both new packages; **every hot-path/typed-error branch has a covering test** (enumerated per task).
- **`go test ./... -race`** green; `go vet`/`gofmt`/`gofumpt`/`golangci-lint`/`govulncheck`/`CGO_ENABLED=0 go build ./...` clean; `go mod tidy`/`go mod verify` stable.
- Every exported symbol has a godoc comment stating defaults + rationale (the 1 MiB body cap, the 202 success status, the correlation-id-distrust default).
- **Traceability:** every commit carries `Spec: 011` / `Plan: 020` / `ADR: 0023` trailers + the standard Co-Authored-By / Claude-Session footer.

---

## File Structure

**`adapter/http/` (package `msghttp`, root module — create):**
- `doc.go` — package doc: the framework-agnostic core; the `msghttp` name / `net/http`-clash note; the untrusted-boundary posture.
- `errors.go` — sentinels: `ErrNilTarget`, `ErrInvalidMaxBodyBytes`, `ErrInvalidStatusCode`, `ErrUnsupportedPayload`. (Gateway nil-exchange **reuses** the exported `msgin.ErrNilExchange`; the async nil-channel gets an adapter-local `ErrNilTarget` rather than reusing `msgin.ErrNilChannel` because the latter's message — "request or reply channel is nil" — reads wrong for an async *target*; the clearer message is the reason for the local sentinel, audit L3.)
- `options.go` — `Config`, `Option`, `NewConfig`, and `WithMaxBodyBytes`/`WithCorrelationID`/`WithRequestHeaders`/`WithResponseHeaders`/`WithSuccessStatus`/`WithErrorStatus`/`WithLogger`.
- `encode.go` — `DecodeRequest(r, *Config) (msgin.Message[any], error)` + `EncodeResponse(w, msg, *Config) error` + `defaultErrorStatus(error) int` + reserved-header stripping + CRLF sanitization.
- `inbound.go` — `ServeAsync(w, r, target, *Config)` (I1) + `ServeGateway(w, r, x, *Config)` (I2).
- `encode_test.go`, `inbound_test.go` (`package msghttp_test`) + `goleak` `TestMain`.

**`adapter/http/stdlib/` (package `stdlib`, root module — create):**
- `doc.go` — the net/http binding.
- `inbound.go` — `NewInbound(target, opts...) (http.Handler, error)`, `NewInboundGateway(x, opts...) (http.Handler, error)`, `Register(mux *http.ServeMux, pattern string, h http.Handler)`.
- `inbound_test.go` (`package stdlib_test`) — `httptest.Server` round-trips + `goleak` `TestMain`.

---

### Task 1: `msghttp` core — `Config`/options, sentinels, `DecodeRequest`/`EncodeResponse`

**Files:** Create `adapter/http/{doc.go,errors.go,options.go,encode.go,encode_test.go}`.

**Interfaces:**
- Consumes (existing core): `msgin.Message[any]`, `msgin.New`, `msgin.WithHeaders`, `msgin.Headers`, `msgin.HeaderContentType`, `msgin.HeaderCorrelationID`, the reserved `msgin.` header prefix (all core header constants start with it).
- Produces:
  - `type Config struct { … }` (unexported fields) + `func NewConfig(opts ...Option) (*Config, error)`.
  - `type Option func(*Config)`; `WithMaxBodyBytes(int64)`, `WithCorrelationID(func(*http.Request) string)`, `WithRequestHeaders(...string)`, `WithResponseHeaders(...string)`, `WithSuccessStatus(int)`, `WithErrorStatus(func(error) int)`, `WithLogger(*slog.Logger)`.
  - `func DecodeRequest(r *http.Request, cfg *Config) (msgin.Message[any], error)`; `func EncodeResponse(w http.ResponseWriter, msg msgin.Message[any], cfg *Config) error`.
  - sentinels `ErrNilTarget`, `ErrInvalidMaxBodyBytes`, `ErrInvalidStatusCode`, `ErrUnsupportedPayload`.

**Design notes (bind these in the implementation):**
- `Config` defaults: `maxBodyBytes = 1 << 20` (1 MiB); `successStatus = http.StatusAccepted` (202); `correlationID = nil` (→ default to the message's own `ID()`); `requestHeaders`/`responseHeaders` empty allow-lists; `errorStatus = nil` (→ `defaultErrorStatus`); `logger` = discard. Option **set-flags** distinguish "unset → default" from "explicit invalid → typed error": `WithMaxBodyBytes(n<=0)` → `NewConfig` returns `ErrInvalidMaxBodyBytes`; `WithSuccessStatus(code)` outside `[100,599]` → `ErrInvalidStatusCode`.
- `DecodeRequest`:
  1. `body, err := io.ReadAll(http.MaxBytesReader(nil, r.Body, cfg.maxBodyBytes))` — cap first. On `*http.MaxBytesError` or read error → return a wrapped error (the caller maps it: oversize → `413`, other read error → `400`). (Pass `nil` for the `ResponseWriter` arg; `MaxBytesReader` only uses it to abort the connection on overflow, which is a server hardening the caller owns — the cap on bytes read is what matters and works with `nil`.)
  2. Build the header map: copy only **allow-listed** request headers (`cfg.requestHeaders`, matched case-insensitively via `r.Header.Get`); add `HeaderContentType` from `Content-Type` when present; add `http.method`/`http.path`/`http.query` (non-reserved keys). **Strip** any key with the reserved `msgin.` prefix defensively (an allow-list entry naming a `msgin.*` header is ignored).
  3. `msg := msgin.New(body, msgin.WithHeaders(headerMap))` — external input entering the system ⇒ `New` (fresh `HeaderID` + timestamp), not `NewMessage`.
  4. Correlation id: `cid := ""; if cfg.correlationID != nil { cid = cfg.correlationID(r) }; if cid == "" { cid = msg.ID() }; msg = msg.WithHeader(HeaderCorrelationID, cid)`.
- `EncodeResponse` (**extract bytes FIRST — audit L4**): compute the body bytes from the payload *before touching `w`* — `[]byte` → directly, `string` → `[]byte(s)`, else return `ErrUnsupportedPayload` with **no** header/status written (so the caller's `500` never carries the flow's headers). Only after a successful extraction: emit the **allow-listed** response headers (`cfg.responseHeaders`), values **CRLF-sanitized** (reject/trim `\r`/`\n`); set `Content-Type` from `HeaderContentType` when present; `w.WriteHeader(http.StatusOK)`; write body. **`EncodeResponse` always writes `200`** — a request-reply body is `200`, never `202`; `WithSuccessStatus` does **not** flow through here (audit M1).
- **`WithSuccessStatus` governs I1 only** (audit M1): `ServeAsync` writes `cfg.successStatus` (default `202`) directly via `w.WriteHeader` on a successful `Send`; `ServeGateway` uses `EncodeResponse` (always `200`). The option's godoc must say it applies to the async inbound handler, not the gateway.
- `defaultErrorStatus(err) int` (audit L2 — honest mapping): `ErrReplyTimeout`→504; `ErrGatewayClosed`→503; `ErrDuplicateCorrelation`→**409** (Conflict — only reachable when a caller opts into trusting client correlation ids and a client reuses one concurrently); `ErrNoCorrelation`→**500** and `ErrUnsupportedPayload`→**500** (server/wiring faults — the adapter always mints a non-empty `msg.ID()`, so a missing correlation id is never the client's fault); an oversize marker (`*http.MaxBytesError` via `errors.As`)→413; any other decode/read error→400; default→500. Uses `errors.Is`/`errors.As` against the `msgin.*` sentinels and `*http.MaxBytesError`.

- [ ] **Step 1: Write failing tests** (`encode_test.go`, `package msghttp_test`, `goleak` `TestMain`). Assert-closure tables:
  - `TestNewConfig_validation` — cases: default (no opts) ok; `WithMaxBodyBytes(0)`/`(-1)` → `ErrInvalidMaxBodyBytes`; `WithSuccessStatus(99)`/`(600)` → `ErrInvalidStatusCode`; valid overrides ok.
  - `TestDecodeRequest` — cases (each builds an `*http.Request` via `httptest.NewRequest`): body becomes `[]byte` payload; oversize body (`WithMaxBodyBytes(4)` + 5-byte body) → error that maps to 413 (`errors.As(*http.MaxBytesError)`); **a non-oversize read error → maps to 400 (audit M3):** back the request with an `io.Reader` that returns a non-`MaxBytesError` on `Read` (`errReader{}`), assert `DecodeRequest` surfaces it and `defaultErrorStatus` maps it to 400 (distinct from the 413 arm); `Content-Type` → `HeaderContentType`; **a client `msgin.delivery-count` header is stripped (audit L5 — a reserved header nothing else overwrites, unlike `correlation-id` which the resolver overwrites anyway)** → assert `msg.Header(HeaderDeliveryCount)` is absent; allow-listed header copied, non-allow-listed header **absent**; default correlation id equals `msg.ID()`; `WithCorrelationID` returning `"x"` sets `"x"`; `WithCorrelationID` returning `""` falls back to `msg.ID()`.
  - `TestEncodeResponse` — cases (via `httptest.ResponseRecorder`): `[]byte` payload → body + `200`; `string` payload → body; non-bytes payload (e.g. `int`) → `ErrUnsupportedPayload` **and the recorder has no headers/status written (audit L4)** — assert `rec.Header()` is empty and the body is empty; allow-listed header emitted; a header value containing `\n` is sanitized (no CRLF in output); `HeaderContentType` → `Content-Type`.
- [ ] **Step 2: Run to verify it fails.** `GOTOOLCHAIN=go1.25.12 go test ./adapter/http/ -run 'Config|Decode|Encode'` → `undefined: msghttp.NewConfig` etc.
- [ ] **Step 3: Implement** `doc.go`, `errors.go`, `options.go`, `encode.go`.
- [ ] **Step 4: Run to verify it passes.** `GOTOOLCHAIN=go1.25.12 go test ./adapter/http/ -run 'Config|Decode|Encode' -race -cover`; `go vet ./adapter/http/`.
- [ ] **Step 5: Commit** (ADR 0023 + Spec 011 already committed standalone for handover; this task commits the code + Plan 020 reference).

```
feat(http): msghttp core — Config/options, request⇄Message encode/decode

The framework-agnostic core of the HTTP adapter (adapter/http, package
msghttp): Config/Option/WithX, DecodeRequest (body cap, reserved-msgin.*
stripping, header allow-list, server-minted correlation id) and
EncodeResponse ([]byte/string payloads, allow-listed CRLF-safe headers).
Stdlib-only, root module, no new dependency.

Spec: 011
Plan: 020
ADR: 0023
```

**Hot-path branches covered:** default config; invalid max-body; invalid status; body→payload; oversize body→413 (`*http.MaxBytesError`); **non-oversize read error→400**; content-type mapping; reserved-header strip (`delivery-count`); allow-list include/exclude; default vs custom vs empty-custom correlation id; []byte/string/non-bytes reply; **non-bytes reply writes nothing before erroring**; header CRLF sanitization; `defaultErrorStatus` per-sentinel arms (504/503/409/500).

---

### Task 2: I1 async inbound — `ServeAsync` core + `stdlib.NewInbound`

**Files:** Create `adapter/http/inbound.go` (`ServeAsync`) + `adapter/http/inbound_test.go`; create `adapter/http/stdlib/{doc.go,inbound.go}` + `adapter/http/stdlib/inbound_test.go`.

**Interfaces:**
- Consumes: `msgin.MessageChannel` (`Send`), `Config`, `DecodeRequest`, `msgin.DirectChannel`/`memory.Broker` (tests).
- Produces: `func ServeAsync(w http.ResponseWriter, r *http.Request, target msgin.MessageChannel, cfg *Config)`; `func (stdlib) NewInbound(target msgin.MessageChannel, opts ...msghttp.Option) (http.Handler, error)`.

**Design:** `ServeAsync`: `msg, err := DecodeRequest(r, cfg)`; on decode error → `statusFor(cfg, err)` (see below), log, return. Else `err := target.Send(r.Context(), msg)`; on error → `statusFor(cfg, err)`, log; on success → `w.WriteHeader(cfg.successStatus)` (default `202`). `statusFor(cfg, err)` is the shared helper both `Serve*` use: `if cfg.errorStatus != nil { return cfg.errorStatus(err) }; return defaultErrorStatus(err)` — factoring it means the `cfg.errorStatus != nil` branch is exercised once and tested once (audit M2). `NewInbound`: `cfg, err := msghttp.NewConfig(opts...); if err != nil { return nil, err }; if target == nil { return nil, msghttp.ErrNilTarget }; return http.HandlerFunc(func(w,r){ msghttp.ServeAsync(w,r,target,cfg) }), nil`. **Method note (audit L6):** the handler does not restrict the HTTP method — a `GET` reaches the same decode+send path; method filtering is the caller's mux concern. State this in the `NewInbound` godoc.

- [ ] **Step 1: Write failing tests.**
  - `msghttp_test.TestServeAsync` (assert-closure table, driven via `httptest.NewRecorder` + `httptest.NewRequest`, target = an in-memory capture channel or `memory.New()`): success → default `202` and the target received a message whose payload is the body bytes and whose `HeaderCorrelationID` is set; **`WithSuccessStatus(201)` → success returns `201` (proves the option is read on I1)**; `Send` returns an error → `500`; **`WithErrorStatus(func(error) int{ return 418 })` + a `Send` error → `418` (covers the `cfg.errorStatus != nil` branch — audit M2)**; oversize body → `413`; the received message carries the request `Content-Type`.
  - `stdlib_test.TestNewInbound` (over `httptest.NewServer(h)`, real `http.Client`): `POST` a body → `202`; `NewInbound(nil, …)` → `ErrNilTarget`; an invalid option (`WithMaxBodyBytes(0)`) → error from `NewInbound`. `goleak` `TestMain`.
- [ ] **Step 2: Run to verify it fails.** `GOTOOLCHAIN=go1.25.12 go test ./adapter/http/... -run 'ServeAsync|NewInbound'`.
- [ ] **Step 3: Implement** `adapter/http/inbound.go` (`ServeAsync`) + `adapter/http/stdlib/{doc.go,inbound.go}` (`NewInbound`).
- [ ] **Step 4: Run to verify it passes.** `GOTOOLCHAIN=go1.25.12 go test ./adapter/http/... -run 'ServeAsync|NewInbound' -race -cover`; `go vet ./adapter/http/...`.
- [ ] **Step 5: Commit.**

```
feat(http): I1 async inbound handler (ServeAsync + stdlib.NewInbound)

An http.Handler that decodes a request into a message, sends it to a
MessageChannel, and replies 202 Accepted (5xx on send failure). Delivery is
at-least-once iff the client retries on 5xx.

Spec: 011
Plan: 020
ADR: 0023
```

**Hot-path branches covered:** async success `202`; custom success `201` (`WithSuccessStatus` on I1); send error → `500`; **custom error status via `WithErrorStatus` (the `cfg.errorStatus != nil` arm)**; oversize → `413`; decode error → `400`; `NewInbound` nil target; `NewInbound` invalid option; end-to-end over `httptest.Server`.

---

### Task 3: I2 sync gateway — `ServeGateway` core + `stdlib.NewInboundGateway` + `Register`

**Files:** Modify `adapter/http/inbound.go` (add `ServeGateway`) + `adapter/http/inbound_test.go`; modify `adapter/http/stdlib/inbound.go` (add `NewInboundGateway`, `Register`) + `adapter/http/stdlib/inbound_test.go`.

**Interfaces:**
- Consumes: `msgin.RequestReplyExchange` (`Exchange`), `msgin.ErrNilExchange`, `msgin.NewChannelExchange`/`NewDirectChannel`/`To`/`Chain` (integration tests), the gateway sentinels (`ErrReplyTimeout`/`ErrGatewayClosed`/`ErrNoCorrelation`/`ErrDuplicateCorrelation`).
- Produces: `func ServeGateway(w, r, x msgin.RequestReplyExchange, cfg *Config)`; `func (stdlib) NewInboundGateway(x msgin.RequestReplyExchange, opts ...msghttp.Option) (http.Handler, error)`; `func (stdlib) Register(mux *http.ServeMux, pattern string, h http.Handler)`.

**Design:** `ServeGateway`: `msg, err := DecodeRequest(r, cfg)` → decode error mapped via `statusFor` (413/400). Else `reply, err := x.Exchange(r.Context(), msg)`; on error → `statusFor(cfg, err)` (default: `ErrReplyTimeout`→504, `ErrGatewayClosed`→503, `ErrDuplicateCorrelation`→409, `ErrNoCorrelation`→500, else→500), log. On success → `EncodeResponse(w, reply, cfg)` (writes `200`); if `EncodeResponse` returns `ErrUnsupportedPayload` (non-bytes reply), it has written **nothing** (audit L4), so `ServeGateway` then writes `statusFor(cfg, ErrUnsupportedPayload)` (=500) cleanly. `NewInboundGateway`: `NewConfig` → nil-exchange guard (`msgin.ErrNilExchange`) → `http.HandlerFunc` wrapping `ServeGateway`.

- [ ] **Step 1: Write failing tests.**
  - `msghttp_test.TestServeGateway` (table): happy path over a **real** `ChannelExchange` echo flow (`request := NewDirectChannel()`, `reply := NewDirectChannel()`, `request.Subscribe(To(reply))`, `x, _ := NewChannelExchange(request, reply)`) — the response body equals the request body and status `200`; error mapping via a `fakeExchange` returning each sentinel → asserts `ErrReplyTimeout`→504, `ErrGatewayClosed`→503, `ErrDuplicateCorrelation`→409, `ErrNoCorrelation`→500, a generic error→500; a `fakeExchange` returning a reply with a non-bytes payload → `500` (`ErrUnsupportedPayload` path) **and the response body is empty (no flow headers leaked — audit L4)**; **`WithErrorStatus` custom mapper → the custom code (audit M2)**. Use a **fake clock** on the real `ChannelExchange` where a timeout is asserted so it fails fast (no 30s wall wait), mirroring Plan 019's guidance.
  - `stdlib_test.TestNewInboundGateway` (over `httptest.Server`): `POST` → echoed body `200`; `NewInboundGateway(nil, …)` → `msgin.ErrNilExchange`; `Register(mux, "/rr", h)` mounts and serves. `goleak` `TestMain`. Close the `ChannelExchange` in a `t.Cleanup` so no waiter lingers.
- [ ] **Step 2: Run to verify it fails.**
- [ ] **Step 3: Implement** `ServeGateway`, `NewInboundGateway`, `Register`.
- [ ] **Step 4: Run to verify it passes** (`-race -cover`, `go vet`).
- [ ] **Step 5: Commit.**

```
feat(http): I2 sync inbound gateway (ServeGateway + stdlib.NewInboundGateway)

An http.Handler over any RequestReplyExchange: decode the request, run the
exchange, encode the correlated reply as the response. Error→status mapping
(ErrReplyTimeout→504, ErrGatewayClosed→503, no/dup-correlation→400). A
ChannelExchange gives HTTP-in → in-process request/reply → HTTP-out with no
new correlation code. Adds a ServeMux Register helper.

Spec: 011
Plan: 020
ADR: 0023
```

**Hot-path branches covered:** gateway happy round-trip (real exchange) `200`; `ErrReplyTimeout`→504; `ErrGatewayClosed`→503; `ErrDuplicateCorrelation`→409; `ErrNoCorrelation`→500; other error→500; non-bytes reply→500 (empty body); custom `WithErrorStatus`; decode error→413/400; `NewInboundGateway` nil exchange; `Register` mounts.

---

### Task 4: `Example` tests, package docs, and the whole-branch delivery + security gate

**Files:** Extend `inbound_test.go` in both packages (Examples); confirm `doc.go`s; no production code beyond doc/example fixes.

- [ ] **Step 1: Write `Example` tests** (double as godoc, deterministic output — no ids/timestamps printed):
  - `stdlib_test.ExampleNewInbound` — mount on a `httptest.Server`, `POST`, print the status (`202`).
  - `stdlib_test.ExampleNewInboundGateway` — wire a `ChannelExchange` over a doubling/echo flow that **ends with `To(reply)` preserving `HeaderCorrelationID`** (use `WithPayload`, not `New`, in any activator so the reply stays correlated — the Plan 019 G6 lesson), `POST`, print the response body.
- [ ] **Step 2: Run examples.** `GOTOOLCHAIN=go1.25.12 go test ./adapter/http/... -run '^Example' -v`.
- [ ] **Step 3: Full suite, race + leak.** `GOTOOLCHAIN=go1.25.12 go test ./... -race` (goleak clean in both new packages).
- [ ] **Step 4: Coverage.** `GOTOOLCHAIN=go1.25.12 go test ./adapter/http/... -coverprofile=/tmp/http.cov && go tool cover -func=/tmp/http.cov | tail -1` → ≥ 85% each; inspect `encode.go`/`inbound.go` for any uncovered branch and add a case.
- [ ] **Step 5: Lint / fmt / vet / vuln / cgo / module hygiene.**

```bash
GOTOOLCHAIN=go1.25.12 go vet ./...
gofmt -l . ; gofumpt -l .
golangci-lint run ./...
govulncheck ./...
CGO_ENABLED=0 GOTOOLCHAIN=go1.25.12 go build ./...
GOTOOLCHAIN=go1.25.12 go mod tidy && git diff --exit-code go.mod go.sum   # no new dependency
GOTOOLCHAIN=go1.25.12 go mod verify
```

- [ ] **Step 6: Whole-branch review + security gate** (CLAUDE.md §5, over `main..HEAD`). Run `/code-review` **and** `/security-review` on the branch diff — this is the untrusted-input boundary (Spec 011 §4), so security is a first-class gate, not advisory. Resolve or triage every finding (re-run the affected review + `-race` after fixes). Confirm additions-only API → minor SemVer.
- [ ] **Step 7: Commit** any gate fixes / examples.

```
test(http): inbound Example tests + whole-branch delivery/security gate

Runnable Example tests for NewInbound and NewInboundGateway (godoc) and the
whole-branch quality gate (race/leak/lint/vet/govulncheck/CGO0/tidy +
/code-review + /security-review over the untrusted inbound boundary).

Spec: 011
Plan: 020
ADR: 0023
```

---

## Self-review notes (author)

- **Spec coverage:** Task 1 → Spec §3.0/§3.2 + §4 (decode/encode, boundary hardening); Task 2 → §3.3 (I1) + §5 (I1 delivery); Task 3 → §3.3 (I2) + §5 (I2 delivery); Task 4 → §7 (tests) + §4 (security gate). Out-of-Phase-1 modes (O1/O2/SSE/gin) intentionally absent — later plans.
- **Deviation from a naïve layout:** the stdlib binding is deliberately thin (ADR 0023 §1). The value split is real — `msghttp` holds all logic on `(w, r)`, so Phase 5's gin binding reuses it with no logic duplication. The thinness is the point, not an accident.
- **Type-agnostic boundary (ADR 0001):** the reply-must-be-`[]byte` constraint is enforced by `ErrUnsupportedPayload`→`500` and documented, keeping `T` out of the adapter. A typed-response convenience is a deliberate non-goal (ADR 0023 Consequences).
- **No goroutines started:** handlers run on the caller's server goroutines; there is nothing to leak (Phase 3's SSE server is the first mode that owns goroutines). `goleak` still guards against accidental leaks in helpers.
- **Return Address:** not exercised in Phase 1 (inbound gateway's correlator is process-local and request-scoped — ADR 0023 §4); it becomes material in Phase 2's O2. Noted so the auditor checks the boundary is not silently assumed.
- **Adversarial audit round 1 folded** (Opus, **SOUND-WITH-NITS** — architecture verified SOUND against the code: SPI reuse, Return-Address-by-construction, and every load-bearing API confirmed). Must-fix items folded: **H1** the request→message path uses `msgin.New` not `NewMessage` (a spec-only slip that would have 400'd every I2 request — corrected in Spec §3.2/ADR §2); **M1** I2 success = `200`, `WithSuccessStatus` scoped to I1 (Task 1/2/3); **M2** the `cfg.errorStatus != nil` custom-mapper branch factored into `statusFor` and given covering tests on I1 and I2 (Task 2/3); **M3** an `errReader` fixture covers the non-oversize read-error→400 branch (Task 1). Folded nits: **M4** inbound-`[]byte`/no-codec-seam documented symmetrically (spec/ADR); **L1** I1 `DirectChannel`-synchronous/`reqCtx` caveat + `QueueChannel` steer (spec §3.3); **L2** honest error→status (`ErrNoCorrelation`→500, `ErrDuplicateCorrelation`→409; Task 1/3); **L3** `ErrNilTarget` justified vs `msgin.ErrNilChannel`; **L4** `EncodeResponse` extracts bytes before writing anything (Task 1); **L5** reserved-strip test uses `delivery-count` (Task 1); **L6** method-not-restricted godoc note (Task 2). **P1** (1 MiB default) confirmed defensible — fail-safe, overridable, godoc rationale mandated. No re-audit warranted (the status-model change did not ripple into new option surface).
