# Plan 024 — HTTP outbound: O1 webhook `OutboundAdapter`, O2 `RequestReplyExchange`

> **For agentic workers:** REQUIRED SUB-SKILL: `superpowers:subagent-driven-development` (the project default) or
> `superpowers:executing-plans`. Steps use checkbox (`- [ ]`) syntax.
>
> **Go-skills hard rule (CLAUDE.md, restated because `writing-plans` omits it):** every task starts from
> **`cc-skills-golang:golang-how-to`**, which routes to the applicable `golang-*` skills (here: `golang-security`,
> `golang-error-handling`, `golang-context`, `golang-safety`, `golang-design-patterns`, `golang-testing`).
> **`superpowers:test-driven-development`** governs every task (red → green → refactor). **`gopls`** (via the `LSP`
> tool) is mandatory for navigation, symbol lookup and refactoring — not `grep`. The project-local **`table-test`**
> override governs every table (assert-closure form, `t.Context()`). `use-mockgen` does not apply (every double here is
> a hand-written blackbox fake or an `httptest.Server`); `use-testcontainers` does **not** apply — Spec 011 §7 mandates
> fully hermetic HTTP tests.
>
> **This plan is deliberately thin.** It fixes signatures, names, invariants, branch coverage and commit boundaries.
> It does **not** embed finished implementations or test bodies: Plan 023 needed two audit rounds that mostly found
> *document drift* in ~2500 lines of embedded Go, while the first hour of implementation found three defects the
> audits had missed. Write the code TDD-first from the invariants and branch tables below.

**Goal.** Ship Spec 011 Phase 2: **O1** `NewOutbound(url, opts…)` — an `msgin.OutboundAdapter` that `POST`s a message
to a fixed webhook URL and classifies the response into the runtime's error vocabulary — and **O2**
`NewExchange(url, opts…)` — an `msgin.RequestReplyExchange` performing a synchronous HTTP request-reply, the **first
external implementation** of that SPI and therefore the first bound by its no-leak-on-unwind contract.

**Architecture.** Reliability stays runtime-owned (ADR 0002 / ADR 0025): the adapter performs **one** attempt and only
**classifies** (`nil` / plain-transient / `msgin.RetryAfter` / `msgin.Permanent`); `Producer.Send` under
`WithProducerRetry` — shipped in Plan 023 — owns backoff, budget, dead-letter and `Retry-After` combination. No new
dependency: stdlib + `clockwork` (already a root-module dep, ADR 0004).

**Tech stack.** Go 1.25 (`GOTOOLCHAIN=go1.25.12`), root module, package `msghttp` in `adapter/http`.

---

## Which ADR governs — recommendation

**ADR 0023** (HTTP channel adapter architecture) already covers most of this increment: §3 the per-mode → SPI mapping,
§4 Return Address by construction, §5 the classification rule, §6 the delivery guarantees, §7 the security posture.
**ADR 0025 §5** separately binds O2 to Spec 012's contract. Neither covers four decisions this plan makes:

1. **Redirect policy** — the default client follows up to 10 redirects, which makes ADR 0023 §7's "SSRF invariant"
   false in practice (see INV-1). Setting `CheckRedirect` to no-follow, and adding `WithFollowRedirects()`, is a new
   security decision with new exported API.
2. **The outbound-direction reflected-XSS reversal** — the remote server's `Content-Type` must land on the
   **non-reserved** `http.content-type`, exactly mirroring Addendum A1's inbound decision in the opposite direction.
3. **Reply header provenance** — the reply message is seeded from the *request's* headers rather than built empty.
4. **Package placement** — O1/O2 live in `adapter/http`, not the `adapter/http/stdlib` slot ADR 0023 §1 / Spec 011
   §3.0 reserved for them.

**Recommendation: an `## Addendum B — Phase 2 (outbound) delivery decisions` on ADR 0023, not a new ADR.** Reasons:
(a) ADR 0023 already carries `Addendum A — review-driven design changes (Phase 1 delivery)`, so this is the
established shape for per-phase deltas to the same architecture; (b) all four decisions **amend existing sections of
0023** (§1 layout, §5 classification, §7 security) rather than deciding something 0023 was silent about — a separate
ADR would leave 0023 stating things that are no longer true with no supersession link; (c) B2 is the exact mirror of
A1 and belongs beside it. Amend **ADR 0025 §5** only with a pointer to the delivered O2.

*Alternative, if the user prefers one-decision-per-file:* `ADR 0027 — Outbound HTTP SSRF posture and redirect policy`,
carrying items 1+2, with 3+4 staying as an Addendum B. Costs a cross-file read to understand the HTTP adapter; gains a
greppable standalone security decision. **Recommended default is the Addendum.**

**Traceability.** Implements [Spec 011](../specs/011-http-adapter.md) Phase 2 (§3.0 layout delta, §3.1, §3.4, §3.6,
§4, §5). Decided by [ADR 0023](../adrs/0023-http-channel-adapter.md) + **Addendum B** (this increment). Bound by
[ADR 0022 Addendum A3](../adrs/0022-messaging-gateway.md) / [Spec 012 §7](../specs/012-exchange-panic-safe-cleanup.md)
(O2's no-leak-on-unwind contract) and [ADR 0025](../adrs/0025-producer-outbound-retry.md) §3/§5/§6. Follows
[Plan 023](023-producer-outbound-retry.md), which shipped the `RetryAfter` marker this plan is the first producer of.

---

## Baseline — verified against the tree while writing this plan

Plan 023 and Plan 022 are **merged into `main`** (`a22ec29`, `32e9d10`). The source brief's "hard sequencing fact"
(that `msgin.RetryAfter` / `WithProducerRetry` / `ErrDeadLettered` do not exist) is **stale**. Verified now:

| Claim | Status | Citation |
|---|---|---|
| `msgin.RetryAfter(err, d)` exists | **EXISTS** | `reliability.go:100` |
| `msgin.Permanent(err)` / `isPermanent` | EXISTS | `reliability.go:26` / `:38` |
| `WithProducerRetry`, `ErrDeadLettered`, `ErrRetryBudgetExhausted` | EXIST | `producer.go:205`, `errors.go:111`, `errors.go:78` |
| `Retry-After` combined as a MINIMUM: `max(computed, min(server, cap))` | SHIPPED | `producer.go:487` (`nextDelay`) |
| Every retry wait floored to `minRetryDelay` (100ms) | SHIPPED | `producer.go:111`, `:489` |
| `msgin.BytesPayloadCodec` (identity `[]byte` codec) | **EXISTS** | `codec.go` |
| `headerHTTPContentType = "http.content-type"` (non-reserved) | EXISTS | `adapter/http/encode.go:25` |
| `reservedHeaderPrefix = "msgin."` | EXISTS | `adapter/http/encode.go:36` |
| `sanitizeHeaderValue` (strips `\r`/`\n`) | EXISTS | `adapter/http/encode.go:211` |
| `Headers.String(name) (string, bool)` | EXISTS | `message.go:50` |
| `Headers.All() iter.Seq2[string, any]` | EXISTS | `message.go:69` |
| `payloadBytes` helper | **DOES NOT EXIST** — the `[]byte`/`string`/`default` switch is inline | `adapter/http/encode.go:177-185` |
| `TestMain` + `goleak.VerifyTestMain` in `adapter/http` | EXISTS | `adapter/http/encode_test.go:21` |
| `WithMaxBodyBytes`' honest CAVEAT paragraph (the model to mirror) | EXISTS | `adapter/http/options.go:129-135` |
| `Gateway.Request(ctx, Req) (Rep, error)` — **not** `Exchange`, and **no codec** | CONFIRMED | `gateway.go:64-76` |
| `HeaderMessageID = "msgin.message-id"` (post Plan 022 rename) | CONFIRMED | `message.go:15` |
| Repo git tags | **ZERO** — `gorelease` cannot verify SemVer | — |

Capture before Task 1:

```bash
cd /Users/zakyalvan/Documents/RND/msgin
GOTOOLCHAIN=go1.25.12 go test ./... -race >/dev/null && echo GREEN
GOTOOLCHAIN=go1.25.12 go test ./... -cover -count=1 2>&1 | grep coverage
```

`adapter/http` is at **100.0%** today. The gate below holds it there.

---

## Global Constraints

- **Go 1.25 only.** `GOTOOLCHAIN=go1.25.12` on every invocation.
- **No new dependency.** `go.mod`/`go.sum` byte-identical at the end. `clockwork` becomes a new *import* of
  `adapter/http`; it is already a root-module direct dependency (ADR 0004), so the module graph does not change —
  verify with `go mod tidy && git diff --exit-code go.mod go.sum`.
- **Blackbox only** — `package msghttp_test`, exported API only. No whitebox test, ever. If a branch needs a
  whitebox test it is either reachable through the public surface or it must not exist (see the coverage gate).
- **Assert-closure tables always** (`table-test`): every case carries `assert func(t *testing.T, …)`, never
  `want`/`wantErr` fields. `t.Context()`, never `context.Background()`, outside `Example` functions.
- **Hermetic HTTP only** (Spec 011 §7): `httptest.Server` / `httptest.NewUnstartedServer`, hand-built
  `*http.Response`, and a `roundTripperFunc` for transport-error injection. **No testcontainers. No network.**
- **Do NOT add `defer goleak.VerifyNone(t)`.** `adapter/http` already has `goleak.VerifyTestMain`
  (`encode_test.go:21` — verified). Per-test `VerifyNone` around `httptest` servers is flake-prone (idle keep-alive
  connections) and redundant. `httptest.Server` cleanup is `t.Cleanup(srv.Close)` / `defer srv.Close()`.
- **Fake clock for every time-dependent assertion.** `clockwork.NewFakeClock()` via `WithOutboundClock`; the only
  clock-sensitive path here is `parseRetryAfter`'s HTTP-date arm. Use `BlockUntilContext`, never the deprecated
  `BlockUntil`.
- **Never call `require`/`t.Fatal`/`t.FailNow` from a spawned goroutine** (`runtime.Goexit` → goleak straggler storm).
  Buffer and assert on the test goroutine.
- **Every task ends in a green commit** — `GOTOOLCHAIN=go1.25.12 go test ./... -race` passes — carrying
  `Spec:` / `Plan:` / `ADR:` trailers.
- **Coverage gate: `adapter/http` stays at 100.0% statement coverage**, `go tool cover -func` per function.
  This is achievable *only because* the plan declines every blackbox-unreachable defensive guard (see
  "Coverage realism" below). If an arm proves unreachable during implementation, **DELETE it** — repo precedent is
  Plan 023 Task 1, which deleted `retryAfterOf`'s redundant nil guard rather than carrying it. Never `//nolint`, never
  relax the gate to hide a dead arm.

### Coverage realism — which guards exist and which are deleted

`Config` is documented (`options.go:26-34`) as nil-safe: a hand-built `&Config{}` or a `nil *Config` must reach every
**exported `*Config`-taking function** without panicking. That contract is **scoped to `*Config`**, and this plan
states the scope explicitly rather than extending it:

- **KEEP + TEST** the nil-guards on `allowedOutboundHeaders()`, `allowedReplyHeaders()`, `clockOrDefault()`,
  `errorBodyExcerptEnabled()`. They are genuinely reachable, because `EncodeRequest` and `ClassifyResponse` are
  exported and take `*Config` — a `_test` package can pass `nil` or `&msghttp.Config{}`. `errorBodyExcerptEnabled()` on
  a `nil`/zero `*Config` must return `false` (default posture = INV-4 "code only").
- **DO NOT WRITE** `client()` / `maxResponse()` nil-fallback accessors. They would be reachable only from
  `Outbound.Send` / `Exchange.Exchange`, whose `cfg` always came from `NewConfig` — a dead arm and (for `client()`) a
  per-call `*http.Client` allocation that silently defeats connection reuse. Read `o.cfg.httpClient` /
  `o.cfg.maxResponseBytes` directly. A hand-built `&msghttp.Outbound{}` is unsupported and will nil-deref, exactly as
  a hand-built `&msgin.ChannelExchange{}` does at `exchange.go:296`. Say so in the type godoc.
- **DO NOT WRITE** a `body == nil` guard in the drain helper: `(*http.Client).Do` never returns a response with a nil
  `Body` (net/http contract).
- **`redactTransport` has NO reachable non-`*url.Error` arm** (round-2 audit F1): `(*http.Client).Do` always wraps
  failures in `*url.Error`, so an `if errors.As {…} / return <fallthrough>` shape leaves the fallthrough `return` dead
  and below 100%. Write it as the single-return conditional-assignment form in Task 4 (the `op, inner` fallback labels
  carry no separate statement, so the false arm is covered without a dead `return`). Do not `//nolint` a fallthrough.

---

## Security invariants — stated as invariants, with how to verify each

These are the gate. A task is not done until its invariants have a covering test that would fail if the invariant were
removed. Each is an **invariant over a class**, not an enumeration of known-bad cases.

**INV-1 — No HTTP response may cause a request to a host other than the one validated at construction, UNLESS the
caller opted in via `WithFollowRedirects()` or supplied a `*http.Client` whose own `CheckRedirect` follows redirects**
(round-2 audit F5 — the invariant is not absolute, and its two escape hatches must be named wherever it is stated, incl.
Spec 011 §4, ADR 0023 Addendum B, and the `WithHTTPClient` godoc, so the escape lives at the option, not only in an
open-decision list). `validateURL` runs once, at construction; the default `*http.Client` follows up to 10 redirects, so
without this invariant a `302 → http://169.254.169.254/latest/meta-data/iam/security-credentials/` makes **O2 return
cloud instance metadata credentials into the flow as the reply payload**, and a `307`/`308` **replays the POST body and
every allow-listed header** to the attacker's host (Go strips only `Authorization`/`Cookie`), including an
`https → http` downgrade. *Realized by:* every client the adapter uses has a non-nil `CheckRedirect` returning
`http.ErrUseLastResponse`, unless the caller passed `WithFollowRedirects()` or set their own `CheckRedirect`.
*Verify:* two `httptest.Server`s, the first `302`-ing to the second; assert the second recorded **zero** hits and that
`Send` returned a `Permanent` error carrying `StatusError{Code: 302}`. *(This is also what makes the `3xx → Permanent`
arm live code rather than reachable only via a hand-built `*http.Response`.)*
**NON-GUARANTEE, to be stated plainly in godoc, Spec 011 §4 and ADR 0023 Addendum B:** msgin performs **no**
private-IP, link-local, loopback or metadata-endpoint filtering. A caller who configures
`NewOutbound("http://169.254.169.254/…")` gets exactly that request. Spec 011 §4's current "SSRF — outbound URL is
caller-configured, never derived from payload; documented invariant" reads as a *guarantee* and must be qualified: it
prevents **message-driven** SSRF only.

**INV-2 — No value read from an `*http.Response` may be written to a message header key carrying the reserved
`msgin.` prefix.** The remote server is untrusted input exactly as an inbound client is. Writing its `Content-Type`
onto `msgin.HeaderContentType` — the exact key `EncodeResponse` trusts as the response media type
(`encode.go:193-197`) and which `DecodeRequest` deliberately refuses to let a client set (`encode.go:66-70`) —
re-opens Addendum A1's **reflected XSS** from the outbound side: an upstream chooses `text/html`, an
`OutboundGateway` → `EncodeResponse` flow serves its bytes back executable, and `X-Content-Type-Options: nosniff`
does not stop an *explicit* `text/html`. *Realized by:* the response `Content-Type` lands on the non-reserved
`headerHTTPContentType` (`encode.go:25`), mirroring `DecodeRequest`; `WithOutboundReplyHeaders` entries matching
`reservedHeaderPrefix` (case-insensitively) are defensively dropped. *Verify:* (a) unit — an upstream replying
`Content-Type: text/html` produces a reply with no `msgin.content-type` and `http.content-type == "text/html"`;
(b) end-to-end — that reply driven through `OutboundGateway` into `msghttp.EncodeResponse` is served as
`application/octet-stream` + `nosniff`; (c) `msgin.content-type` cannot be forged through
`WithOutboundReplyHeaders("msgin.content-type")`.

**INV-3 — Classification must never derive `Permanent` from a remote-controlled status alone, and must never return
a core sentinel that `isPermanent` recognizes.** (Carried from Plan 023's round-1 audit, sec #10.) `isPermanent`
(`reliability.go:38`) short-circuits `sendRetrying` with **no dead-letter and no attempt consumed**, so mapping e.g.
`413 → msgin.ErrPayloadTooLarge` would hand a hostile endpoint a one-response "make the producer give up and record
nothing" switch. *Realized by:* `ClassifyResponse` returns only `nil`, `*StatusError`, `msgin.Permanent(*StatusError)`
or `msgin.RetryAfter(*StatusError, d)` — never `ErrPayloadType`, `ErrPayloadDecode` or `ErrPayloadTooLarge`, and never
a status-to-sentinel table. *Verify:* a table over 400/404/413/415/422 asserting `errors.Is(err, msgin.ErrPayloadTooLarge)`
and `errors.Is(err, msgin.ErrPayloadType)` are **false** for every one, while `errors.Is(err, ErrOutboundStatus)` is
true. The `Permanent` marker itself is legitimate here — it is the runtime's "do not retry a 404" signal and it *does*
still dead-letter-free short-circuit, which is the documented Spec 011 §3.4 behaviour; the invariant is that no
*additional* semantics (payload-invalid, decode-failure) is inferred from a status.

**INV-4 — No remote-controlled bytes appear in any error string UNSANITIZED, and by default none appear at all.** This
increment is what makes remote-influenced text reach caller logs (Plan 023 round-1 audit, sec #14). *Realized by (default):*
`StatusError.Excerpt` is `""` unless `WithErrorBodyExcerpt()` is set, so the error string carries **only** the `int`
code — no URL, no body, no `resp.Status` text. *Realized by (opt-in, decision 5):* when `WithErrorBodyExcerpt()` is set,
`StatusError.Excerpt` carries a **bounded, fully-sanitized** slice of the response body, produced by `sanitizeExcerpt`,
whose contract is: (1) **cap the READ first** — read at most `errorBodyExcerptMax` (256) bytes from the body, so an
unbounded/huge error body cannot force 256 bytes+ of read/allocate/scan work (a DoS-amplification bound, NOT merely an
output-length bound); (2) **sanitize at the RUNE level, not the byte level** — a byte-level C0/C1 strip on a UTF-8
stream is WRONG (round-3 audit F1): it destroys legitimate printable runes whose UTF-8 continuation byte lands in
`0x80`–`0x9f` (`À` = `C3 80`, and most Latin-1/CJK/emoji), AND lets dangerous code points whose bytes miss the stripped
ranges survive (`U+FEFF` BOM = `EF BB BF`, some bidi overrides). The correct, minimal primitive is
**`strconv.Quote(string(b))`**: it escapes every non-`unicode.IsPrint` rune to `\uXXXX` (catching C0/C1 controls,
ANSI `\x1b`, bidi overrides `U+202E`, line/paragraph separators `U+2028`/`U+2029`, NEL `U+0085`, BOM/zero-width
`U+FEFF`), escapes invalid UTF-8 to `\xNN` (subsuming any `ToValidUTF8` step — no separate coercion, no strip-then-rejoin
re-introduction hazard), **preserves** printable accented/CJK/emoji runes, and wraps the result in quotes so the
untrusted excerpt is unambiguously delimited in the error string. `sanitizeHeaderValue` (`encode.go:211`, CR/LF-only) is
**insufficient** here — `sanitizeExcerpt` is a distinct, stricter helper:
`func sanitizeExcerpt(b []byte) string { return strconv.Quote(string(b)) }`. *Verify:* (a) default — a `500` with a
hostile body + hostile `Retry-After` yields an error string containing neither the body nor `\r`/`\n`; (b) opt-in — the
same with `WithErrorBodyExcerpt()`, fed a body mixing `\x1b[31m`, `\r`, `\n`, `\t`, NUL, `U+FEFF`, `U+202E`, NEL
`U+0085`, invalid UTF-8, **and** a printable `À`/CJK char, yields an excerpt where **every rune is `unicode.IsPrint`**
(after unquoting) — the printable `À`/CJK **survives** while every dangerous code point is escaped/gone; (c) opt-in with
a 1 MiB hostile body — the **bytes CONSUMED from the body are ≤256** (a byte-counting reader proves cap-before-read,
NOT just `len(excerpt) <= 256`, which a broken sanitize-then-truncate would also satisfy).

**INV-5 — No error returned from `Send`/`Exchange` exposes the target URL.** `(*http.Client).Do` returns `*url.Error`,
whose `Error()` redacts **only the password** — username, host, path and query survive, so a webhook token in the
query string lands in every timeout the caller logs. `StatusError`'s careful non-leakage is undone one line away.
*Realized by:* both call sites route the transport error through one shared redaction helper (`redactTransport`, sketched
in Task 4 as a single-return conditional assignment so it hits the 100% gate — round-2 audit F1) that discards `ue.URL`
and re-wraps `ue.Op` + `ue.Err` under `ErrOutboundTransport`. *Verify:* a `roundTripperFunc` failing a request to
`https://user:pw@example.invalid/hook?token=SECRET`; assert the error string contains neither `SECRET`, nor `user`,
nor the path — and that `errors.Is(err, context.Canceled)` still holds on the cancellation arm (the wrap must be `%w`
on `ue.Err`). *Residual to document:* the underlying `net` error may still name `host:port`; userinfo, path and query
never survive.

**INV-6 — Every response-body read is bounded, and no arithmetic on the cap can overflow.**
`io.ReadAll(io.LimitReader(body, max+1))` wraps to `MinInt64` when `max == math.MaxInt64` (a legal
`WithMaxResponseBytes` value); `io.LimitReader` with `N < 0` EOFs immediately, so an **empty payload is returned as a
success** — silent data loss. *Realized by:* read exactly `max` bytes, then probe one further byte **with `io.ReadFull`,
not a lone `Read`** (round-2 audit F4), to distinguish "exactly at cap" from "over cap" (sketch in Task 5). A lone
`Read` can legally return `(0, nil)` — which a naïve `n > 0` check would misread as end-of-body and serve a truncated
payload as success, re-opening the very silent-data-loss this invariant closes; `io.ReadFull` loops past `(0, nil)`
until it fills the byte or hits a real EOF. *Verify:* `WithMaxResponseBytes(math.MaxInt64)` against a server
returning a non-empty body returns that body intact; at-cap succeeds; cap+1 returns `ErrReplyTooLarge`.
*Documented non-guarantee:* the cap bounds the **body only**. Response **headers** are bounded instead by
`http.Transport.MaxResponseHeaderBytes` (default ~10 MiB) — mirror the CAVEAT paragraph at `options.go:129-135`.

**INV-7 — O2 releases all request-scoped state on every exit path, including a panic unwind, and never recovers a
caller panic into an error return.** (ADR 0022 Addendum A3 / Spec 012 §7 / ADR 0025 §5.) Request-scoped state = the
in-flight `*http.Request` (released by the response body being closed and the ctx being the caller's) and
`resp.Body`. *Realized by:* exactly **one** `defer` closing the body, registered immediately after `Do` returns nil
error, covering the classified-error return, the read-failure return, the over-cap return and a panic unwinding out of
any of them. Never per-return-site cleanup. Never `recover()`. *Verify:* a body whose `Read` panics; assert
`recover()` in the test is non-nil **and preserves the original panic value** **and** the tracking body's `closed`
flag is true.

---

## Multi-instance topology (CLAUDE.md mandatory rule — must be in the godoc, not implied)

- **O1 `Outbound`** — **per-process by construction.** Under `WithProducerRetry`, N horizontally-scaled instances each
  retry independently, so a throttling endpoint receives **N× the load it asked to shed** and `Retry-After` compliance
  is per-instance, not fleet-wide. Stated, not solved; the named seam is
  [ADR 0006](../adrs/0006-resilience-flow-control.md)'s rate-limit and circuit-breaker interfaces, into which a
  Redis/DB-backed distributed limiter plugs without a core change. Identical wording to `WithProducerRetry`'s
  TOPOLOGY paragraph (`producer.go:198-204`) — point at it rather than restating a second, driftable copy.
- **O2 `Exchange`** — **Return Address is satisfied by construction.** The reply returns on the **same TCP connection**
  the sending instance holds open: the connection *is* the return address, so no reply can arrive at a different
  instance and the cross-process correlation problem **does not arise**. `Exchange` holds no correlator, no registry
  and no cross-request state — it is the degenerate, stateless case of the SPI. The shape that **would** need an
  explicit Return Address is the **async-callback variant** (the request carries a callback URL; the reply is
  delivered later to an inbound webhook that may land on a different instance); it is out of scope for v1 and must be
  **named** in the godoc so the constraint is recorded, with the seam noted: an async-callback exchange mints an
  instance-addressable callback URL and correlates the late reply through a durable store — an adapter change, not a
  core change.

---

## Final exported surface (the API contract this plan commits to)

```go
// adapter/http/options.go  (additions)
func WithHTTPClient(c *http.Client) Option            // nil is a no-op (mirrors WithLogger)
func WithFollowRedirects() Option                     // opt out of INV-1; loud SSRF warning in godoc
func WithOutboundHeaders(names ...string) Option      // slices.Clone; case-SENSITIVE lookup; reserved msgin.* DROPPED (decision 4)
func WithOutboundReplyHeaders(names ...string) Option // slices.Clone; case-INSENSITIVE (http.Header.Get); reserved msgin.* DROPPED
func WithMaxResponseBytes(n int64) Option             // set-flag; explicit n <= 0 -> ErrInvalidMaxResponseBytes
func WithOutboundClock(clk clockwork.Clock) Option    // nil is a no-op
func WithErrorBodyExcerpt() Option                    // decision 5: opt-in; StatusError carries a bounded, sanitized body excerpt

// adapter/http/errors.go  (additions — six sentinels)
ErrEmptyURL, ErrInvalidURL, ErrInvalidMaxResponseBytes,
ErrReplyTooLarge, ErrOutboundStatus, ErrOutboundTransport

// adapter/http/outbound.go  (new)
func EncodeRequest(ctx context.Context, method, url string, msg msgin.Message[any], cfg *Config) (*http.Request, error)
type StatusError struct{ Code int; Excerpt string } // Excerpt is "" unless WithErrorBodyExcerpt() is set (decision 5)
func (e *StatusError) Error() string   // "msghttp: outbound request returned status <n>"; appends the sanitized Excerpt iff non-empty — never a URL (INV-4/INV-5)
func (e *StatusError) Unwrap() error   // ErrOutboundStatus
func ClassifyResponse(resp *http.Response, cfg *Config) error
type Outbound struct{ /* unexported */ }
var _ msgin.OutboundAdapter = (*Outbound)(nil)
func NewOutbound(url string, opts ...Option) (*Outbound, error)
func (o *Outbound) Send(ctx context.Context, msg msgin.Message[any]) error

// adapter/http/exchange.go  (new)
type Exchange struct{ /* unexported */ }
var _ msgin.RequestReplyExchange = (*Exchange)(nil)
func NewExchange(url string, opts ...Option) (*Exchange, error)
func (x *Exchange) Exchange(ctx context.Context, req msgin.Message[any]) (msgin.Message[any], error)
```

Unexported, shared: `payloadBytes(msg) ([]byte, error)` and `contentTypeOf(msg) string` (both in `encode.go`),
`validateURL(raw) error`, `resolveClient(cfg) *http.Client`, `parseRetryAfter(v string, now time.Time) (time.Duration, bool)`,
`redactTransport(err) error`, `permanentEncode(err) error`, `drainBounded(body io.Reader, max int64)`,
`readExcerpt(body io.Reader) string` (decision 5 — reads ≤`errorBodyExcerptMax` bytes, then `sanitizeExcerpt`s them),
`sanitizeExcerpt(b []byte) string` (decision 5 — see the excerpt-sanitization contract under decision 5 below).

**`drainBounded`'s contract is DRAIN-ONLY — it NEVER closes** (round-2 audit F2). It reads up to `max` bytes into
`io.Discard` (`io.CopyN(io.Discard, io.LimitReader(body, max), …)`) so the connection can be reused, and nothing more.
Closing the body is the caller's job and belongs to **exactly one** `defer resp.Body.Close()` per call site (INV-7).
This is the single contract that satisfies BOTH adapters: O1 (Task 4) must add its own `defer resp.Body.Close()` and
then drain — because `drainBounded` is its ONLY other touch of the body, a drain-that-also-closed would be fine for O1
but would double-close O2 (whose non-2xx arm drains under an already-registered `defer Close`). Making the helper
drain-only removes that contradiction: every path in both adapters closes via one deferred `Close`, and `drainBounded`
is a pure reuse-drain. Its signature takes `io.Reader`, not `io.ReadCloser`, to make "never closes" unforgeable.

**Naming notes, decided:** `WithOutboundClock` (not `WithClock` — `msgin.WithClock` is a `MessageOption` at
`message.go:125`, and the repo convention is a distinct clock name per component: `WithConsumerClock`,
`WithProducerClock`, `WithExchangeClock`). `WithOutboundReplyHeaders` (not `WithReplyHeaders` — too confusable with
the inbound `WithResponseHeaders`). Both godocs must cross-reference their confusable twin
(`WithMaxResponseBytes` ⇄ `WithMaxBodyBytes`, `WithOutboundReplyHeaders` ⇄ `WithResponseHeaders`), naming which
direction each governs.

**`Config` stays one shared inbound+outbound bag.** Splitting it would duplicate `NewConfig`'s plumbing and add a
second exported option type for no safety gain. Consequence to **document**, not to reject at runtime:
`NewOutbound(url, WithMaxBodyBytes(0))` errors on an irrelevant inbound setting, and an inbound constructor silently
accepts inert outbound options.

---

## Tasks

### Task 0 — artifact delta, committed ahead of the code

**Files:** `docs/specs/011-http-adapter.md`, `docs/adrs/0023-http-channel-adapter.md`,
`docs/adrs/0025-producer-outbound-retry.md`, `docs/plans/024-http-outbound.md` (this file),
delete `docs/plans/024-http-outbound-source-brief.md`.

Committing the design **ahead of** the code is the documented CLAUDE.md exception; it is invoked deliberately here so
that every subsequent `feat` commit's `Spec:`/`Plan:`/`ADR:` trailers point at artifacts that already exist in history
(Plan 023's HANDOVER §5.1 lesson: the withdrawn plan committed all docs last, so every trailer dangled).

- [ ] Spec 011 **§3.0** — move `outbound.go` + `exchange.go` from the `adapter/http/stdlib/` block into the
      `adapter/http/` block. Rationale to record: `adapter/http/stdlib` exists to bind the framework-neutral cores to
      `net/http` **server** types (`http.Handler`, `*http.ServeMux`); an HTTP **client** has no framework variant —
      gin has no HTTP client — so a `stdlib` outbound file would be an empty passthrough that the gin module would
      then have to re-export. `exchange.go` was already listed under `adapter/http` in §3.0; O1 joins it.
- [ ] Spec 011 **§3.1** — real signatures: `NewOutbound(url string, opts…) (*Outbound, error)` and
      `NewExchange(url string, opts…) (*Exchange, error)` (both return an error: URL and option validation).
- [ ] Spec 011 **§3.4** — (a) delete the stale "Open point RESOLVED (2026-07-21): it does NOT" block and replace it
      with "shipped by Plan 023: `WithProducerRetry` applies a `RetryPolicy` to `OutboundAdapter.Send`"; (b) record
      the redirect policy and the SSRF non-guarantee; (c) replace "Drops into `Gateway`/`OutboundGateway` unchanged"
      with the real limitation (below).
- [ ] Spec 011 **§3.6** — add the six new sentinels and the seven new options (incl. `WithErrorBodyExcerpt`, decision 5).
- [ ] Spec 011 **§4** — qualify the SSRF bullet per INV-1 (name the two escapes); add the outbound-`Content-Type`
      bullet per INV-2; record that `WithOutboundHeaders` strips reserved names (decision 4 symmetry); record the
      opt-in `WithErrorBodyExcerpt` and its bounded/sanitized contract (decision 5, INV-4).
- [ ] Spec 011 **§6** — Phase 2's plan number is **024**, not 022 (Plan 021 took the number; Spec 011's phases shifted
      to 024/025/026/027). Fix the phasing table and §8's plan list.
- [ ] ADR 0023 — add **Addendum B** with the four decisions listed at the top of this plan, each stating what it
      amends (§1, §5, §7), **plus the two settled deviations**: B5 `WithOutboundHeaders` strips reserved `msgin.*` names
      (decision 4 — symmetry with the reply path, amends §7); B6 the opt-in `WithErrorBodyExcerpt` and its
      stricter-than-`sanitizeHeaderValue` sanitization (decision 5 — amends §7's INV-4 posture from "code only" to
      "code only, or opt-in bounded+sanitized excerpt").
- [ ] ADR 0025 §5 — one line pointing at the delivered O2.
- [ ] Commit: `docs: Spec 011 Phase 2 delta, ADR 0023 Addendum B, and the audited Plan 024`
      Trailers: `Spec: 011` / `Plan: 024` / `ADR: 0023`.

**The `Gateway` limitation, verified against `gateway.go:64-76` — record it verbatim in Spec 011 §3.4 and in
`NewExchange`'s godoc.** `msgin.Gateway` exposes **`Request(ctx, req Req) (Rep, error)`**, not `Exchange`; it takes a
**raw `Req`** (it mints the correlation id itself via `New(req).WithHeader(HeaderCorrelationID, randomID())`) and
returns a **`Rep` value**, not a message. It has **no codec at all** — `boxMessage` in, `PayloadOf[Rep]` (a type
assertion) out. Therefore `msghttp.Exchange` behind a `Gateway` works **only** for `Req ∈ {[]byte, string}` (what
`EncodeRequest`'s payload extraction accepts) and `Rep == []byte` (what `Exchange` produces).
`Gateway[MyStruct, MyStruct]` fails with `msgin.ErrPayloadType`, and so does `Gateway[[]byte, string]`. "Drops in
unchanged" over-claims.

---

### Task 1 — outbound config surface, sentinels, and the redirect policy

**Files:** modify `adapter/http/options.go`, `adapter/http/errors.go`; create `adapter/http/outbound.go` (the
`resolveClient`/`validateURL` helpers only), `adapter/http/outbound_test.go`.
**Imports added to `options.go`:** `time`, `github.com/jonboulle/clockwork`. (`slices` is already there,
`options.go:7`. The source brief's "add `strings`" is wrong — `strings` belongs to `outbound.go`.)

New `Config` fields: `httpClient *http.Client`, `followRedirects bool`, `outboundHeaders []string`,
`replyHeaders []string`, `maxResponseBytes int64`, `maxResponseBytesSet bool`, `clock clockwork.Clock`,
`errorBodyExcerpt bool` (decision 5; default `false` — opt-in, so the default posture stays INV-4's "code only").

New constants, with the rationale in the comment:

```go
const defaultMaxResponseBytes int64 = 1 << 20      // 1 MiB, mirroring defaultMaxBodyBytes; also caps O1's reuse-drain (F6)
const defaultHTTPClientTimeout = 30 * time.Second  // finite ON PURPOSE: http.DefaultClient has NO timeout at all
const errorBodyExcerptMax = 256                    // decision 5: bytes of remote body embedded in StatusError.Excerpt when opted in
```

**Why 30s, and how it relates to Plan 023's `cap 60s < budget 2m` ordering (round-2 audit F6).** The client timeout is
the **per-attempt I/O bound** — it caps a *single* `client.Do`, which ADR 0025 §1.1 explicitly could NOT bound from the
producer side (the producer treats one adapter call as opaque and unbounded). It is therefore **orthogonal to, and well
below,** the producer's 2m retry *budget* and 60s `Retry-After` *cap*, which govern the sum and spacing of retries
across attempts, not the duration of any one attempt. No interaction, no ordering constraint to satisfy: 30s bounds one
hop; 2m/60s bound the retry envelope around many hops. State this in the constant's comment so a reviewer does not
misread the 30s as competing with the 2m budget.

`NewConfig` additions, after the `successStatus` block: set-flag validation for `maxResponseBytes`
(`ErrInvalidMaxResponseBytes` on an explicit `<= 0`); default `httpClient` to `&http.Client{Timeout: defaultHTTPClientTimeout}`;
default `clock` to `clockwork.NewRealClock()`.

The redirect policy (the whole of the non-obvious logic — resolve **once** at construction, never per-`Send`):

```go
// resolveClient never mutates the caller's *http.Client. When the caller left
// CheckRedirect nil (net/http's follow-up-to-10 default), a SHALLOW COPY is
// taken and the no-follow policy set on the copy; Transport and Jar stay shared
// by pointer, which is intended — connection pooling and cookies are preserved.
func resolveClient(cfg *Config) *http.Client {
    c := cfg.httpClient                                   // never nil: NewConfig fills it
    if cfg.followRedirects || c.CheckRedirect != nil {    // caller's explicit choice wins
        return c
    }
    cp := *c
    cp.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
    return &cp
}
```

`validateURL(raw)`: empty/whitespace → `ErrEmptyURL`; `url.Parse` failure, scheme ∉ {http, https}, or empty host →
`ErrInvalidURL`. State the invariant it does **and does not** provide (INV-1's non-guarantee).

**Godoc obligations (security-relevant, keep in spirit):**
- `WithOutboundHeaders`: reserved `msgin.*` entries **ARE dropped** (decision 5 — user chose SYMMETRY with the reply
  path over the plan's original asymmetry recommendation). An entry whose name, lowercased, has the `msgin.` prefix is
  silently ignored, so internal flow metadata (`msgin.correlation-id`, `msgin.message-id`, …) is never published to the
  remote endpoint through this option — matching `WithOutboundReplyHeaders` and `DecodeRequest`. Message-header **value
  lookup** stays **case-SENSITIVE** (an exact map lookup, `msg.Headers().String`); only the reserved-**name** guard is
  case-insensitive (`strings.HasPrefix(strings.ToLower(name), reservedHeaderPrefix)`), so `MSGIN.Foo` is dropped too.
  `Content-Type` is written *after* the allow-list, so an entry naming it cannot override it. Names cloned.
  **Rationale for the reserved-name guard being case-insensitive while the value lookup is case-sensitive:** the guard
  is a *security* filter (must not be bypassable by casing), the lookup is a *functional* map key (must match the
  header exactly as stored).
- `WithOutboundReplyHeaders`: reserved `msgin.*` entries **are** defensively ignored — the remote server is untrusted
  input exactly as an inbound client is (INV-2). Matched case-insensitively (`http.Header.Get`), first value only.
- `WithMaxResponseBytes`: the CAVEAT paragraph (INV-6), mirroring `options.go:129-135`. **Also note (round-2 audit F6)
  that the same cap bounds O1's post-`Send` connection-reuse drain, not only the O2 reply body** — an operator tuning
  it for large O2 replies is also raising how much of an oversized O1 webhook response gets drained before the
  connection is returned to the pool. With `WithErrorBodyExcerpt()` enabled, a non-2xx O1 response consumes up to
  `errorBodyExcerptMax` (256) bytes for the excerpt **in addition to** the `maxResponseBytes` drain (round-3 NOTE) —
  still bounded, but the two budgets are separate.
- `WithHTTPClient`: states that a client with a nil `CheckRedirect` is shallow-copied with no-follow installed, that a
  client with its own `CheckRedirect` is used verbatim (the caller's choice wins), and that `http.DefaultClient` has
  **no timeout**, which is why the default here does. **Must also state the INV-1 escape (round-2 audit F5): supplying a
  client whose `CheckRedirect` follows redirects re-opens the SSRF INV-1 otherwise closes — the caller owns that risk,
  and it is documented here at the option rather than only in the open-decision list.**
- `WithFollowRedirects`: the loud SSRF warning — restate INV-1's attack in two sentences.
- `WithErrorBodyExcerpt` (decision 5): states that it is **opt-in** (default: `StatusError` carries the status code
  only, INV-4); that when set, up to `errorBodyExcerptMax` (256) bytes of the **remote, attacker-controlled** response
  body are embedded in the error string after being cap-bounded, control-character-stripped and coerced to valid UTF-8
  (`sanitizeExcerpt`); and the caveat that this *deliberately* surfaces remote-influenced bytes into caller logs — safe
  to render, but the caller should still treat them as untrusted when parsing.

**Hot-path branches (Task 1):**

| # | Branch | Test |
|---|---|---|
| 1 | `WithMaxResponseBytes` unset → 1 MiB default | `NewConfig()` then `EncodeRequest`-independent probe via Task 5's behaviour; assert via `NewOutbound` succeeding |
| 2 | `WithMaxResponseBytes(0)` and `(-1)` → `ErrInvalidMaxResponseBytes` | construction table |
| 3 | `WithMaxResponseBytes(1)` → accepted | construction table |
| 4 | `WithHTTPClient(nil)` → no-op, default client kept | construction table |
| 5 | `WithOutboundClock(nil)` → no-op, real clock kept | construction table |
| 6 | `resolveClient`: default client (nil `CheckRedirect`) → no-follow copy installed | 302 test in Task 4 (**not gated here**) |
| 7 | `resolveClient`: caller client with non-nil `CheckRedirect` → returned verbatim, not copied | 302 test in Task 4 (**not gated here**) |
| 8 | `resolveClient`: `WithFollowRedirects()` → redirect followed | 302 test in Task 4 (**not gated here**) |
| 9 | `validateURL`: `""` and `"   "` → `ErrEmptyURL` | construction table |
| 10 | `validateURL`: `"://bad"` (parse failure) → `ErrInvalidURL` | construction table |
| 11 | `validateURL`: `"file:///etc/passwd"`, `"ftp://h/"` (bad scheme) → `ErrInvalidURL` | construction table |
| 12 | `validateURL`: `"http:///path"` (empty host) → `ErrInvalidURL` | construction table |
| 13 | `allowedOutboundHeaders`/`allowedReplyHeaders`/`clockOrDefault` on a `nil *Config` | via `EncodeRequest(…, nil)` / `ClassifyResponse(…, nil)` in Tasks 2/3 (**not gated here**) |
| 14 | allow-list slices are cloned against later caller mutation | construction table (mirrors the Phase 1 test) |

> **Only row 2 is a genuine BEHAVIORAL assertion at Task 1** (round-2 audit F3). `Config`'s fields are unexported with
> no getters, so a blackbox `package msghttp_test` can observe only `NewConfig`'s returned `error` — never a resolved
> *value*. Row 2 (`WithMaxResponseBytes(0)/(-1) → ErrInvalidMaxResponseBytes`) asserts through that error and genuinely
> can fail. Rows **1, 3, 4, 5, 14** are **line-covered** at Task 1 (the option body executes) but their *behavioral*
> correctness is **not observable** here: "assert via `NewOutbound` succeeding" cannot distinguish a 1 MiB default from
> a 5-byte one (row 1), a kept default client from a nil one (row 4), a real clock from none (row 5), or a cloned
> allow-list from an aliased one (row 14) — that is exactly the "100%-covered test that asserts nothing" trap the
> project has been burned by. **Defer each behavioral assertion to where the value becomes observable:** row 1 → Task 5
> (INV-6 at-cap/over-cap proves the 1 MiB default), rows 4/5 → Task 4 (redirect behavior / `parseRetryAfter` fake-clock
> proves the client/clock defaults), row 14 → Task 2 (`EncodeRequest` header forwarding proves the clone). At Task 1,
> assert only that construction succeeds/fails as the *error* contract dictates — do not dress a construction-success
> check up as a value check. Rows 6–8, 9–12 and 13 are likewise **observable only in a later task** and gated there
> (Plan 023 round-2 lesson: never gate a branch in Task N that is first reachable in Task N+k):
> - **Rows 9–12 (`validateURL`) are NOT reachable in Task 1.** `validateURL` is unexported, blackbox tests are
>   `package msghttp_test`, and `NewConfig` does not call it — only `NewOutbound`/`NewExchange` do, in Tasks 4/5. So
>   `outbound.go`'s `validateURL` and `resolveClient` lines sit at **0% at the end of Task 1, by construction**, and
>   Task 4 branch 1 is where rows 9–12 are actually proven.
> - **Therefore the "adapter/http at 100.0%" constraint is a DELIVERY gate (Task 6), not a literal per-task gate for
>   this package.** Do not attempt to reach 100% on `outbound.go` at Task 1; that is impossible without a whitebox test,
>   which the project forbids. State the Task-1 profile as "every line `NewConfig` and the options can reach is covered;
>   `validateURL`/`resolveClient` are reconciled at Task 4."

- [ ] Commit: `feat(http): outbound config surface, six sentinels, and a no-follow redirect policy`
      Trailers: `Spec: 011` / `Plan: 024` / `ADR: 0023`.

---

### Task 2 — `EncodeRequest`, over one shared payload/content-type extraction

**Files:** modify `adapter/http/encode.go` (**must be staged in this commit** — omitting it is a broken-build commit),
`adapter/http/outbound.go`; extend `adapter/http/outbound_test.go`.

**Refactor first (gopls Extract-function, then re-run the Phase 1 suite green):** the `[]byte`/`string`/`default`
switch is currently **inline** in `EncodeResponse` at `encode.go:177-185`. There is no `payloadBytes` helper to
"find and reuse" — extract one and have `EncodeResponse` call it. Likewise, resolve the request `Content-Type` through
**one** helper matching `EncodeResponse`'s existing shape at `encode.go:193-196`: `msg.Headers().String(…)` then
**sanitize-then-check-empty** (so a value of `"\r\n"` collapses to `defaultResponseContentType`). Do not ship two
subtly different resolutions.

`ErrUnsupportedPayload`'s godoc (`errors.go:47-53`) says "reply message's payload"; widen it to cover an outbound
*request* payload too, keeping its 500 mapping in `DefaultErrorStatus` (`encode.go:249-251`) unchanged.

`EncodeRequest` order of operations is load-bearing: allow-listed headers first (each **reserved-name-dropped** then
value CRLF-sanitized), then `Content-Type` **last** so an allow-list entry can never override it. In the loop, skip any
`name` where `strings.HasPrefix(strings.ToLower(name), reservedHeaderPrefix)` (decision 5 symmetry), then
`msg.Headers().String(name)` (the repo idiom at `encode.go:188`), never a manual `.(string)` assertion. Godoc must
state: **`url` is CALLER-configured and never derived from `msg`.**

**Hot-path branches (Task 2):**

| # | Branch | Assertion |
|---|---|---|
| 1 | `[]byte` payload → body verbatim | body bytes equal |
| 2 | `string` payload → body is `[]byte(s)` | body bytes equal |
| 3 | other payload type → `ErrUnsupportedPayload`, **no request built** | `errors.Is` + nil request |
| 4 | `msgin.HeaderContentType` present and non-empty → used | header equals |
| 5 | header absent → `application/octet-stream` | header equals |
| 6 | header present but non-string → default | header equals |
| 7 | header present but sanitizes to empty (`"\r\n"`) → default | header equals |
| 8 | allow-listed message header forwarded, CRLF-sanitized | header equals, no `\r`/`\n` |
| 9 | non-allow-listed header dropped | header absent |
| 9b | **decision 4**: the test message MUST carry `msgin.correlation-id` = non-empty AND a header keyed exactly `MSGIN.Correlation-Id` = non-empty (plain `msgin.New` stamps neither, and the value lookup is case-sensitive — round-3 F6); both names appear in the allow-list; assert **both are absent** from the outbound request. Without the seeded values, removing the reserved-name guard changes nothing and the test asserts nothing. | request header absent ×2, guard-mutation-sensitive |
| 10 | allow-listed non-string header value skipped | header absent |
| 11 | allow-list entry naming `Content-Type` cannot override it | header equals the resolved CT |
| 12 | `http.NewRequestWithContext` failure (bad method, e.g. `"BAD METHOD"`) → error, nil request | non-nil error |
| 13 | `EncodeRequest(ctx, …, nil)` — nil `*Config` → no panic, empty allow-list, default CT | INV over `Config` nil-safety |
| 14 | the returned request carries the caller's ctx | `req.Context()` identity |

- [ ] Commit: `feat(http): EncodeRequest over a shared payload and content-type extraction`
      Trailers: `Spec: 011` / `Plan: 024` / `ADR: 0023`.

---

### Task 3 — `ClassifyResponse`, `StatusError`, `parseRetryAfter`

**Files:** modify `adapter/http/outbound.go`, `adapter/http/errors.go`; extend `adapter/http/outbound_test.go`.

**The rule, stated once (ADR 0023 §5, as amended by Addendum B):**
`2xx` → `nil`. The **transient set is `408`, `429`, and all `5xx`**. **Everything else outside 2xx — including `1xx`
and `3xx` — is `msgin.Permanent`.** Within the transient set, `429` and `503` **only** (the two RFC 9110 defines it
for) honour a parseable `Retry-After` → `msgin.RetryAfter(err, d)`.

```go
func ClassifyResponse(resp *http.Response, cfg *Config) error {
    if resp == nil { return ErrNilResponse }            // decision 3 (open list): KEPT — typed error over nil-deref
    code := resp.StatusCode
    if code >= 200 && code < 300 { return nil }         // 2xx: body untouched here, drained by the caller for reuse
    err := &StatusError{Code: code}                     // INV-4: the code, plus (opt-in) a sanitized Excerpt only
    if cfg.errorBodyExcerptEnabled() {                  // decision 5; nil-safe accessor, default false
        err.Excerpt = readExcerpt(resp.Body)            // reads ≤256B THEN sanitizeExcerpt; caller drains the remainder
    }
    if !(code >= 500 || code == 408 || code == 429) { return msgin.Permanent(err) }
    if code == 429 || code == 503 {
        if d, ok := parseRetryAfter(resp.Header.Get("Retry-After"), cfg.clockOrDefault().Now()); ok {
            return msgin.RetryAfter(err, d)             // a MINIMUM; the producer combines it (producer.go:487)
        }
    }
    return err                                          // plain transient
}
```

**`ClassifyResponse` now reads (a bounded prefix of) the body when the excerpt is enabled** — so the caller MUST classify
**before** draining, or the excerpt is empty (O2 already does: it classifies then `drainBounded`s the remainder; O1's
`Send` order is fixed to match — see Task 4). `errorBodyExcerptEnabled()` mirrors the other nil-safe `*Config` accessors
(`allowedOutboundHeaders`/`clockOrDefault`): a `nil *Config` or a hand-built `&Config{}` returns `false`, so the nil-safe
contract holds and the default posture is INV-4's "code only". `readExcerpt` is best-effort: a read error yields whatever
bytes arrived (sanitized) or `""`, never a returned error — a broken error body must not mask the real status error.

**`readExcerpt` / `sanitizeExcerpt` — the excerpt contract (decision 5, INV-4; corrected by round-3 audit F1).**
`readExcerpt(body)` reads **at most** `errorBodyExcerptMax` (256) bytes — `io.ReadAll(io.LimitReader(body, errorBodyExcerptMax))`
— so **the READ is capped, not just the output**: a 1 MiB hostile body forces ≤256 bytes of read/allocate work, and the
remainder is still bounded by `drainBounded(…, maxResponseBytes)` in the caller. It then returns
`sanitizeExcerpt(bytesRead)`. **`sanitizeExcerpt` is exactly `strconv.Quote(string(b))`** — nothing hand-rolled. Do NOT
strip control **bytes**: a byte-level C0/C1 strip on a UTF-8 stream mangles legitimate printable runes (`À` = `C3 80`
loses its `0x80` continuation byte) and still lets `U+FEFF`/bidi survive (round-3 audit F1). `strconv.Quote` operates on
decoded **runes**: it escapes every non-`unicode.IsPrint` rune (all C0/C1 controls, ANSI `\x1b`, bidi `U+202E`,
`U+2028`/`U+2029`, NEL `U+0085`, BOM `U+FEFF`) to `\uXXXX`, escapes invalid UTF-8 to `\xNN` (so no separate
`ToValidUTF8` and no strip-then-rejoin hazard), preserves printable accented/CJK/emoji, and quote-delimits the untrusted
text. `readExcerpt` is best-effort — a read error returns the sanitized bytes that did arrive (or `""`), never an error.
This is a distinct helper from `sanitizeHeaderValue`; do NOT reuse the latter here.

**`parseRetryAfter` — the two arms and their guards.** Delay-seconds form: `strconv.ParseInt`; reject negative; and
**reject an overflowing value** — `time.Duration(secs) * time.Second` wraps negative for `secs > MaxInt64/1e9`
(≈ 9.22e9), and the `secs < 0` guard sits *before* the multiply, so it returns a negative duration with `ok == true`.
Clamp against `int64(math.MaxInt64 / time.Second)` **before** multiplying. HTTP-date form: `http.ParseTime`; a date in
the past yields `0, true`. Guarantee: **`parseRetryAfter` never returns a negative duration, and never returns an
overflowed one.** A returned `0` is safe and needs no floor here — `producer.nextDelay` computes
`max(computed, min(server, cap))` and floors every wait at `minRetryDelay` (100ms), so a `Retry-After: 0` or a past
date degrades to the normal computed backoff rather than a hot spin. **Do not re-specify `Retry-After` semantics in
this adapter** — O1's only job is to emit the marker.

**`StatusError` godoc, verbatim in spirit:** it deliberately carries **neither** the target URL (which may embed
credentials in its userinfo) **nor** any response body or status text (attacker-controlled) — INV-4/INV-5.

**Hot-path branches (Task 3):**

| # | Branch | Assertion |
|---|---|---|
| 1 | `nil` response → `ErrNilResponse` | `errors.Is` |
| 2 | 200 / 204 / 299 → `nil` | nil error ×3 |
| 3 | 400 / 404 → `Permanent` | `msgin` permanence observed via a `Producer` (see below) |
| 4 | 100 → `Permanent` (below-2xx arm) | as above |
| 5 | 301 → `Permanent` (hand-built `*http.Response`; the live path is Task 4's 302 test) | as above |
| 6 | 408 → plain transient, no `RetryAfter` | not permanent, no marker |
| 7 | 429 without `Retry-After` → plain transient | no marker |
| 8 | 429 with `Retry-After: 30` → `RetryAfter(30s)` | producer waits 30s (two-phase) |
| 9 | 503 with an HTTP-date, fake clock → `RetryAfter(delta)` | producer waits delta |
| 10 | 503 with an unparseable `Retry-After` → plain transient | no marker |
| 11 | 500 with `Retry-After: 30` → header **ignored** (not 429/503) | no marker |
| 12 | `Retry-After: -5` → rejected, plain transient | no marker |
| 13a | `Retry-After: 9223372036` (**largest VALID** delay-seconds = `MaxInt64/1e9`) → `RetryAfter(~292y)` **accepted** | marker present, positive duration |
| 13b | `Retry-After: 9223372037` (**first OVERFLOWING** value; the multiply would wrap negative) → **rejected**, no marker, **never a negative duration** | no marker; INV over the overflow class |
| 14 | `Retry-After` an HTTP-date in the past → `0, true`, degrades to computed backoff | producer waits the computed backoff |
| 15 | `Retry-After: ""` / whitespace → rejected | no marker |
| 16 | `errors.As(err, &statusErr)` yields the code | code equals |
| 17 | `errors.Is(err, ErrOutboundStatus)` through **both** wrappers (`Permanent` and `RetryAfter`) | true ×2 |
| 18 | INV-3: 400/404/413/415/422 satisfy **neither** `msgin.ErrPayloadTooLarge` nor `msgin.ErrPayloadType` | false ×5 ×2 |
| 19 | INV-4 default: a 500 with a hostile body + hostile status text, **no** `WithErrorBodyExcerpt()` → `StatusError.Excerpt == ""`, error string carries neither, and no `\r`/`\n` | substring assertions + empty excerpt |
| 19b | **decision 5**: `WithErrorBodyExcerpt()` set, 500 body mixing `\x1b[31m`, `\r`, `\n`, `\t`, NUL, `U+FEFF`, `U+202E`, NEL `U+0085`, invalid UTF-8, **and** a printable `À`/CJK → after unquoting the `Excerpt`, **every rune is `unicode.IsPrint`**, the printable `À`/CJK **survives**, and each dangerous code point is escaped/absent | per-**rune** `unicode.IsPrint` scan + printable-survives (NOT a byte scan — round-3 F3) |
| 19c | **decision 5**: `WithErrorBodyExcerpt()` set, 500 with a **1 MiB** hostile body → **bytes consumed from the body ≤ 256** via a byte-counting `io.Reader` wrapper (proves cap-before-**read**, which a sanitize-then-truncate would fail; `len(Excerpt) <= 256` alone does NOT — round-3 F2) | counter ≤ `errorBodyExcerptMax` |
| 19d | **decision 5**: `WithErrorBodyExcerpt()` set, `readExcerpt` hits a mid-read error → no returned error, the status error still surfaces (best-effort excerpt) | `errors.As` `*StatusError` still holds |
| 19e | **decision 5 × INV-7**: `WithErrorBodyExcerpt()` set, a body whose `Read` **panics** during `readExcerpt` → `recover()` non-nil, original panic value preserved (not converted), `closed == true` (round-3 F7 — the excerpt-on panic arm branch 24 does not cover) | recover + closed |
| 20 | `ClassifyResponse(resp, nil)` — nil `*Config` **on the `429`+`Retry-After` path** (so `clockOrDefault()`'s nil branch is reached) → no panic, real clock | INV over `Config` nil-safety |

> **Branch 13 must exercise BOTH sides of the guard, and the boundary is exact.** Verified: `clamp := int64(math.MaxInt64 / time.Second)` is `9223372036`, and `time.Duration(9223372036) * time.Second` = a valid `2562047h47m16s` (~292 years). So `9223372036` is the **largest accepted** value and `9223372037` is the **first that overflows** the multiply to a negative. The guard is `secs > clamp` (strictly greater), and `parseRetryAfter` must apply it **before** the `* time.Second`. A test that fed `9223372036` and asserted "rejected" would fail against the correct implementation and, worse, would never drive an actually-overflowing multiply — so the "never a negative duration" invariant would be unproven. `msgin.RetryAfter` normalizes `d<0 → 0` (`reliability.go:104`) as a backstop, which is exactly why the guard itself needs a red-first test rather than leaning on that backstop. Resolve the prose to a single rule: **clamp/reject at `secs > MaxInt64/1e9`, before multiplying.**

> **How to assert permanence, blackbox.** `msgin.isPermanent` is unexported and `Permanent`'s marker type is not
> introspectable from outside. Assert the **observable consequence** through a `Producer` configured with
> `WithProducerRetry` + a counting `DeadLetter`: a permanent error yields **attempt count == 1**, **DLQ count == 0**
> and **zero clock advance**; a transient one yields ≥ 2 attempts. This is Plan 023's D6 lesson —
> `errors.Is(err, cause)` is true of either marker independently and proves nothing.

- [ ] Commit: `feat(http): classify an outbound response into the runtime's error vocabulary`
      Trailers: `Spec: 011` / `Plan: 024` / `ADR: 0023` / `ADR: 0025`.

---

### Task 4 — O1 `NewOutbound`

**Files:** modify `adapter/http/outbound.go`; extend `adapter/http/outbound_test.go`; create
`adapter/http/testhelpers_test.go` (`trackingBody` with `closed atomic.Bool` + `read atomic.Int64`,
`roundTripperFunc`, a counting `msgin.OutboundAdapter` sink — all shared with Task 5).

`NewOutbound` → `NewConfig(opts…)`, then `validateURL(url)`, then `resolveClient(cfg)` stored on the struct.
`Send` = `EncodeRequest(POST)` → `client.Do` → `defer resp.Body.Close()` (the ONE close, registered immediately after
a nil-error `Do`) → **`ClassifyResponse(resp, cfg)` FIRST** → **then** `drainBounded(resp.Body, max)` (drain-only, for
connection reuse — it does NOT close; see the `drainBounded` contract above, round-2 audit F2) → return the classified
error. **Order is load-bearing (decision 5):** `ClassifyResponse` reads the bounded error-body excerpt when
`WithErrorBodyExcerpt()` is set, so it MUST run before `drainBounded` consumes the body — draining first would empty
the excerpt. On a 2xx, `ClassifyResponse` returns `nil` without touching the body, and the subsequent `drainBounded`
drains it for reuse. The deferred `Close` runs on every return path incl. panic; the classify-then-drain pair runs
inline so a reused keep-alive connection is not abandoned mid-body. Sketch:
```go
resp, err := o.client.Do(req)
if err != nil { return redactTransport(err) }
defer func() { _ = resp.Body.Close() }()
classifyErr := ClassifyResponse(resp, o.cfg)   // reads the excerpt (if enabled) before the drain
drainBounded(resp.Body, o.cfg.maxResponseBytes) // drain the remainder for connection reuse; never closes
return classifyErr
```

**The two error-boundary helpers, extracted once and used by BOTH `Outbound.Send` and `Exchange.Exchange`** (fix the
class, not the instance — Plan 023's meta-lesson; these are duplicated code paths):

- `permanentEncode(err) error` — wraps an `EncodeRequest` failure in `msgin.Permanent`. Without it a missing
  `Transform` (a non-`[]byte`/`string` payload) burns the entire attempt budget and dead-letters, when nothing was
  ever sent and no retry can help.
- `redactTransport(err) error` — INV-5. Must stay **transient** (never `Permanent`-wrapped) so the runtime retries,
  and must preserve `errors.Is(err, context.Canceled)` / `context.DeadlineExceeded` through the `%w` on `ue.Err`.
  **Structure it as a single-return with a conditional assignment, NOT an `if errors.As { return } / return` pair**
  (round-2 audit F1): `(*http.Client).Do` *always* wraps its failures in `*url.Error`, so a trailing
  non-`*url.Error` `return` is blackbox-unreachable dead code that fails the mandatory 100% gate (§ "Coverage realism")
  — and it may not be `//nolint`'d. Use:
  ```go
  func redactTransport(err error) error {
      op, inner := "Post", err          // fallback labels if Do ever returns a non-*url.Error
      var ue *url.Error
      if errors.As(err, &ue) { op, inner = ue.Op, ue.Err }
      return fmt.Errorf("%w: %s: %w", ErrOutboundTransport, op, inner)
  }
  ```
  The single conditional statement is covered by the `*url.Error` test; its false arm carries no statement, so
  `go tool cover -func` reports `redactTransport` at 100% without any dead `return`.

**Godoc contract:** `Outbound` is **AT-MOST-ONCE by itself** — one POST, no retry. AT-LEAST-ONCE arises only under a
retry authority: a `Producer` with `WithProducerRetry`, or a Consumer-driven flow's redelivery. **A source-less flow
gets neither.** The receiver must be idempotent — key on `msgin.HeaderMessageID`, which is forwarded **only** if the
caller lists it in `WithOutboundHeaders`. Plus the multi-instance paragraph above, and INV-1's non-guarantee.

**Hot-path branches (Task 4):**

| # | Branch | Assertion |
|---|---|---|
| 1 | construction: empty/whitespace URL, bad scheme, parse failure, empty host | four sentinels (Task 1's rows 9–12, now via `NewOutbound`) |
| 2 | construction: a bad `Option` (`WithMaxBodyBytes(0)`) propagates `NewConfig`'s error | `errors.Is` |
| 3 | 2xx → `nil`, server saw exactly one POST with the expected body | count == 1 |
| 4 | non-2xx → the classified error | `errors.As` `*StatusError` |
| 4b | **decision 5**: `WithErrorBodyExcerpt()`, non-2xx with a hostile body **containing ≥1 printable byte** (so "non-empty" genuinely distinguishes classify-first from drain-first — round-3 NOTE) → `Send`'s error, `errors.As`'d to `*StatusError`, has a **non-empty** sanitized `Excerpt`; drain-first would yield `""` → red | non-empty + per-rune `IsPrint` scan |
| 5 | transport error → redacted, transient, `ErrOutboundTransport` | INV-5 substring assertions |
| 6 | ctx cancelled mid-request → error satisfies `errors.Is(err, context.Canceled)` | INV-5 preservation |
| 7 | encode failure (non-bytes payload) → `Permanent`, and the server saw **zero** requests | count == 0 + permanence via a Producer |
| 8 | response body drained **and closed** on the 2xx path | `trackingBody.closed` |
| 9 | response body drained **and closed** on the non-2xx path | `trackingBody.closed` |
| 10 | the drain is bounded by `WithMaxResponseBytes` | `trackingBody.read` ≤ cap |
| 11 | **INV-1**: default client, `302` → second server; second server hit count == 0, error is `Permanent` `StatusError{302}` | the SSRF regression test |
| 12 | **INV-1**: `WithFollowRedirects()` → the redirect IS followed, second server hit count == 1 | opt-out works |
| 13 | **INV-1**: a caller `*http.Client` with its own `CheckRedirect` is used verbatim (their policy observed) | policy invoked |
| 14 | **INV-1**: the caller's `*http.Client` value is **not mutated** (its `CheckRedirect` is still nil after `NewOutbound`) | shallow-copy proof |
| 15 | allow-listed headers reach the server; non-allow-listed do not | server-side assertion |
| 16 | end-to-end: `NewProducer[[]byte](out, WithProducerCodec[[]byte](BytesPayloadCodec{}), WithProducerRetry(…))` retries a `503` then succeeds | fake clock, two-phase advance |
| 17 | end-to-end: `429` + `Retry-After: 30` under a 1s computed backoff → the producer waits **30s** (the server's minimum wins because it is larger — *not* because it overrides) | two-phase advance |

> **Do not assert "the server's `Retry-After` overrides the computed backoff".** It is a **minimum**:
> `max(computed, min(server, cap))` (`producer.go:487`). Branch 17 works because 30s > 1s. Add no branch asserting a
> server can *shorten* a backoff — that behaviour does not exist and asserting it would fail.

> **Two-phase waits only.** `Advance(want - 1ns)` → assert **not yet returned** → `Advance(1ns)` → assert returned.
> A one-shot `Advance(want)` followed by `clock.Now().Sub(start) == want` is true by construction and cannot detect
> *under*-waiting (Plan 023 Global Constraints).

> **Every `Producer` composition in tests, examples and godoc MUST pair `msgin.BytesPayloadCodec{}` explicitly**:
> `NewProducer[[]byte](out)` resolves to `JSONPayloadCodec` (`producer.go:620-628`), and `json.Marshal([]byte("event"))`
> is `"ZXZlbnQ="` — the flagship composition would POST base64. This is not hypothetical: it is the exact defect Plan
> 023's round-1 audit found in its own DLQ assertion.

- [ ] Commit: `feat(http): NewOutbound — the O1 webhook OutboundAdapter, redirect-safe by default`
      Trailers: `Spec: 011` / `Plan: 024` / `ADR: 0023` / `ADR: 0025`.

---

### Task 5 — O2 `NewExchange`

**Files:** create `adapter/http/exchange.go`, `adapter/http/exchange_test.go`; extend
`adapter/http/testhelpers_test.go`.

**Construction — restate it, do NOT abbreviate to "as Task 4".** `NewExchange` MUST run the identical sequence
`NewConfig(opts…)` → `validateURL(url)` (store the validated URL as `x.url`) → `resolveClient(cfg)` (store the
**no-follow** client as `x.client`). The `Exchange` body below reads `x.url`/`x.client`, so if an implementer omits
`resolveClient` here, **O2 silently follows redirects — a direct INV-1 (SSRF) breach for O2**, and the only thing that
would catch it is branch 26 (302-not-followed). Spelling the three steps out removes the trap rather than relying on
one downstream test to detect it. Branches 22–23 gate the URL-validation rows for O2 the same way Task 4 branch 1 does
for O1.

**Write the panic-unwind test FIRST** (INV-7 is the contract this task exists to honour): a `trackingBody` wrapping a
reader whose `Read` panics with a sentinel value; assert `recover()` is non-nil, that the **original panic value is
preserved** (not converted to an error), and that `closed` is true. Note honestly in the test comment what it does
**not** cover: there is no window between `Do` returning and the `defer` registering, so the test proves the deferred
cleanup covers the reads, not a broader class.

`Exchange` control flow (the only non-obvious part — everything else is mechanical):

```go
func (x *Exchange) Exchange(ctx context.Context, req msgin.Message[any]) (msgin.Message[any], error) {
    var zero msgin.Message[any]
    httpReq, err := EncodeRequest(ctx, http.MethodPost, x.url, req, x.cfg)
    if err != nil { return zero, permanentEncode(err) }
    resp, err := x.client.Do(httpReq)
    if err != nil { return zero, redactTransport(err) }
    defer func() { _ = resp.Body.Close() }()   // INV-7: the ONE cleanup site; no recover(), ever
    if err := ClassifyResponse(resp, x.cfg); err != nil {
        drainBounded(resp.Body, x.cfg.maxResponseBytes)   // bounded; the error body is deliberately discarded
        return zero, err
    }
    max := x.cfg.maxResponseBytes
    body, err := io.ReadAll(io.LimitReader(resp.Body, max))
    if err != nil { return zero, err }
    if int64(len(body)) == max {                          // INV-6: probe instead of max+1, which overflows
        var probe [1]byte
        // io.ReadFull, NOT a single Read: io.Reader legally permits (0, nil), and a lone
        // `if n, _ := Read(); n > 0` would treat that as "no more data" and serve a truncated
        // body as success — the exact silent-data-loss INV-6 exists to prevent (round-2 audit F4).
        // ReadFull loops until it fills the byte or hits a real error; n > 0 (or a non-EOF error) = over cap.
        if n, err := io.ReadFull(resp.Body, probe[:]); n > 0 || !errors.Is(err, io.EOF) {
            return zero, ErrReplyTooLarge
        }
    }
    return x.buildReply(req, resp, body), nil
}
```

`buildReply` — **reply header provenance** (the second non-obvious part):

```go
// The reply is seeded from the REQUEST's headers so flow metadata survives the
// hop — a Splitter's msgin.sequence-number/size would otherwise be lost, which
// silently breaks a downstream Aggregator. Per-message identity is re-stamped
// fresh by msgin.New; the request's media type is dropped because it describes
// the request, not the reply.
h := maps.Collect(req.Headers().All())         // Headers.All() is an iter.Seq2 (message.go:69)
delete(h, msgin.HeaderMessageID); delete(h, msgin.HeaderTimestamp); delete(h, msgin.HeaderContentType)
reply := msgin.New[any](body, msgin.WithHeaders(h))
if ct := resp.Header.Get("Content-Type"); ct != "" {
    reply = reply.WithHeader(headerHTTPContentType, sanitizeHeaderValue(ct))  // INV-2: NON-reserved key
}
for _, name := range x.cfg.allowedReplyHeaders() {
    if strings.HasPrefix(strings.ToLower(name), reservedHeaderPrefix) { continue }   // remote is untrusted
    if v := resp.Header.Get(name); v != "" { reply = reply.WithHeader(name, sanitizeHeaderValue(v)) }
}
```

**Verify while implementing** (do not assume): whether `msgin.WithHeaders` applied to `msgin.New` can clobber the
freshly-stamped `HeaderMessageID`/`HeaderTimestamp`. The three `delete`s make the question moot, but the test must
still pin that `reply.ID() != req.ID()` and is non-empty.

**`msgin.HeaderCorrelationID` is carried automatically** by the seed, which is what `OutboundGateway`'s save/restore
(`gateway.go:44-53`) expects — no special case needed. Add a branch proving it.

**Godoc obligations:** INV-7's contract with its ADR/Spec citation; the Return Address / async-callback paragraph
above; the `Gateway` limitation from Task 0; and the deliberate discarding of a non-2xx error body (correct security
posture — attacker-controlled — but currently undocumented, so a caller debugging a 4xx gets only the status code).

**Hot-path branches (Task 5):**

| # | Branch | Assertion |
|---|---|---|
| 1–2 | construction: bad URL (×4 sentinels), bad `Option` | as Task 4 row 1–2 |
| 3 | 200 with a body → reply payload is that `[]byte` | bytes equal |
| 4 | request `msgin.correlation-id` propagated onto the reply | header equals |
| 5 | request with **no** correlation id → reply has none (no empty header materialized) | header absent |
| 6 | request `msgin.sequence-number`/`size` survive the hop | headers equal (the Aggregator-correctness branch) |
| 7 | reply gets a **fresh** `msgin.message-id` ≠ the request's, and a fresh timestamp | not-equal + non-empty |
| 8 | request's `msgin.content-type` does **not** leak onto the reply | header absent |
| 9 | **INV-2**: response `Content-Type: text/html` → `http.content-type == "text/html"`, `msgin.content-type` **absent** | ×2 |
| 10 | **INV-2 e2e**: that reply through `OutboundGateway` → `EncodeResponse` is served `application/octet-stream` + `nosniff` | ×2 |
| 11 | **INV-2**: `WithOutboundReplyHeaders("msgin.content-type")` (and `"MSGIN.Content-Type"`) cannot forge the reserved key | header absent ×2 |
| 12 | empty response `Content-Type` → no `http.content-type` set | header absent |
| 13 | allow-listed response header lands on the reply, CRLF-sanitized | header equals |
| 14 | non-allow-listed response header does not | header absent |
| 15 | non-2xx → the classified error and a **zero-value** reply message | error + zero message |
| 16 | transport error → redacted (INV-5) | substring assertions |
| 17 | encode failure → `Permanent`, **zero** server hits | count == 0 |
| 18 | **INV-6**: body exactly at cap succeeds with the full body | bytes equal |
| 19 | **INV-6**: body at cap+1 → `ErrReplyTooLarge` | `errors.Is` |
| 20 | **INV-6**: `WithMaxResponseBytes(math.MaxInt64)` returns a non-empty body intact (the overflow regression) | bytes equal, non-empty |
| 20b | **INV-6**: over-cap body whose boundary `Read` returns `(0, nil)` before delivering the next byte → still `ErrReplyTooLarge` (round-2 audit F4; a `trackingBody` that injects a `(0,nil)` read at the probe) | `errors.Is` — not a truncated success |
| 21 | a `Read` failure mid-body → error, body closed | error + `closed` |
| 22 | **INV-7**: body closed on success | `closed` |
| 23 | **INV-7**: body closed on the non-2xx path | `closed` |
| 24 | **INV-7**: body closed on a **panic unwind**, original panic value preserved, not recovered | the first test written |
| 25 | ctx cancellation propagates | `errors.Is(err, context.Canceled)` |
| 26 | **INV-1**: `302` is not followed; second server hit count == 0 | the SSRF regression test for O2 |
| 27 | e2e through `msgin.NewGateway[[]byte, []byte]` → `g.Request(t.Context(), []byte("hi"))` returns the reply bytes | payload equals |
| 28 | e2e `msgin.NewGateway[[]byte, string]` → `msgin.ErrPayloadType` (the recorded `Gateway` codec limitation) | `errors.Is` |
| 29 | e2e through `msgin.OutboundGateway` — incoming correlation id restored on the forwarded reply | header equals |

> **`Gateway`'s API, verified (`gateway.go:64-76`):** the call is `reply, err := g.Request(t.Context(), []byte("hi"))`
> with `g` of type `*msgin.Gateway[[]byte, []byte]`. It is **not** `g.Exchange(...)`, it does **not** take a
> `msgin.Message[…]`, and its result is a `Rep` value — `reply.Payload()` does not compile. The source brief's drafted
> integration test was wrong on all three counts.

- [ ] Commit: `feat(http): NewExchange — the O2 RequestReplyExchange, leak-free on unwind`
      Trailers: `Spec: 011` / `Plan: 024` / `ADR: 0022` / `ADR: 0023` / `ADR: 0025`.

---

### Task 6 — examples, package doc, and the delivery gate

**Files:** `adapter/http/doc.go`, `adapter/http/outbound_test.go` + `exchange_test.go` (`Example` functions),
plus any artifact correction the implementation forced. **Land any such correction as a follow-up `docs:` commit** —
by Task 6, Task 0's commit is several commits behind HEAD, and amending a non-HEAD commit needs an interactive rebase,
which this environment blocks (`git rebase -i` is unsupported). Do NOT plan to amend Task 0.

- [ ] `ExampleNewOutbound` — the flagship composition, **with `msgin.BytesPayloadCodec{}` paired explicitly**, over an
      `httptest.Server`, with an `// Output:` block.
- [ ] `ExampleNewExchange` — over `msgin.NewGateway[[]byte, []byte]`, likewise.
- [ ] `adapter/http/doc.go` — an outbound section: the SSRF non-guarantee (INV-1), the at-most-once/at-least-once
      split, and a pointer to `stdlib`'s "Deploying these handlers safely" for the inbound side.
- [ ] Verification (all must pass, from the repo root):

```bash
export GOTOOLCHAIN=go1.25.12
go build ./... && CGO_ENABLED=0 go build ./...
go test ./... -race -count=1
go test ./adapter/http/ -coverprofile=/tmp/http.out -count=1 && go tool cover -func=/tmp/http.out | tail -40
go vet ./... && golangci-lint run ./... && test -z "$(gofmt -l .)"
go mod tidy && git diff --exit-code go.mod go.sum && go mod verify
govulncheck ./...
go test -run '^Example' ./adapter/http/...
```

- [ ] **Whole-branch gate** — `/code-review` and `/security-review` over `main..HEAD` (**not** just the last commit),
      every finding fixed or triaged with a written rationale, then re-run `-race`. Standing lesson: Plan 021's
      whole-branch pass found two proven vulnerabilities that every clean per-task review had cleared. Re-examine the
      settled adjudications, especially INV-1 and INV-2.
- [ ] **SemVer:** additive only — no exported symbol is removed or changed; six new sentinels, **seven** new options
      (incl. `WithErrorBodyExcerpt`), four new exported types/functions, and a new exported field `StatusError.Excerpt`
      (additive — callers only read `StatusError`) ⇒ a **minor** bump. `gorelease` **cannot run: the repo has zero git
      tags**, so
      compatibility is established by inspection. Repeat the standing recommendation to cut `v0.1.0` to close that
      blind spot.
- [ ] Commit: `docs(http): outbound examples, package doc, and the Phase 2 delivery gate`
      Trailers: `Spec: 011` / `Plan: 024` / `ADR: 0023`.

---

## Open decisions — SETTLED (user, 2026-07-22)

All five are resolved. Three followed the plan's recommendation; **two deviated** (marked ⚠) and are folded into the
tasks/invariants above.

1. **Explicit caller `CheckRedirect` wins over the no-follow default → YES** (matches recommendation). Only a nil
   `CheckRedirect` gets the no-follow policy installed on a shallow copy; a caller who set their own policy made a
   deliberate choice and it is honored. Documented as an INV-1 escape at `WithHTTPClient` (round-2 audit F5).
   `resolveClient`'s `if cfg.followRedirects || c.CheckRedirect != nil { return c }` already realizes this.
2. **Type name `Exchange`/`NewExchange` → KEPT** (matches recommendation). The `Exchange.Exchange` stutter is accepted;
   it reads fine at the call site and is spec-ratified (§3.1/§3.4). Free to rename only pre-tag, but no rename.
3. **`ErrNilResponse` → KEPT** (matches recommendation). `ClassifyResponse` is exported and takes `*http.Response`, so
   `nil` is caller-reachable; a typed error beats a nil-deref (CLAUDE.md "never panic on caller input").
4. ⚠ **`WithOutboundHeaders` strips reserved `msgin.*` names → SYMMETRY** (DEVIATES from the plan's "keep asymmetry"
   recommendation). The user chose to drop reserved names on the outbound request path too, matching
   `WithOutboundReplyHeaders`/`DecodeRequest`. Internal flow metadata is never published to the remote endpoint via this
   option. Folded into: the exported-surface comment, `WithOutboundHeaders`' godoc, `EncodeRequest`'s loop guard, Task 2
   branch 9b, and ADR 0023 Addendum B (which now records symmetry, not asymmetry).
5. ⚠ **Ship `WithErrorBodyExcerpt()` NOW → YES** (DEVIATES from the plan's "defer" recommendation). The opt-in,
   bounded (≤256B), control-char-stripped, valid-UTF-8 excerpt ships in this increment. Because it opens a **new
   remote-controlled-bytes → error-string path** — the exact boundary INV-4/INV-5 guard — it is specified with a
   stricter-than-`sanitizeHeaderValue` sanitization contract (`sanitizeExcerpt`, see Task 3 and INV-4) and gets a
   **focused round-3 adversarial audit of just this path before code** (recorded below). Folded into: the option,
   `StatusError.Excerpt`, `Config.errorBodyExcerpt`, `errorBodyExcerptMax`, `readExcerpt`/`sanitizeExcerpt`,
   `ClassifyResponse`, the O1 `Send` classify-before-drain reorder, INV-4, and Task 3/4 branches 19b–19e/4b. The
   round-3 audit (below) then corrected the sanitizer itself.

---

## Round-3 focused audit (independent Opus, excerpt path only). Verdict: APPROVE-WITH-FIXES → folded in. No re-audit.

Scoped to the `WithErrorBodyExcerpt()` deviation (decision 5) + the reserved-header symmetry (decision 4), since those
were the post-round-2 additions and open a new remote-bytes→error-string boundary. The design **structure** was
confirmed sound and its cross-invariant interactions genuinely closed: the excerpt is body-only (INV-5 — never a URL,
populated only after a nil-error `Do`), orthogonal to classification (INV-3), covered by the single deferred `Close` on
every path incl. a panic **inside** `readExcerpt` (INV-7), bounded in bytes (`io.LimitReader(body, 256)`) and time
(client timeout), opt-in and fail-safe by default, and the reserved-name guard is non-bypassable. **The one real defect
was the sanitizer algorithm**, plus two "asserts-nothing" tests and a missing panic-arm test.

| # | Sev | Finding | Fix folded in |
|---|---|---|---|
| F1 | MAJOR | The specified **byte-level** C0/C1 strip on a UTF-8 stream is the wrong primitive: it corrupts legitimate printable runes whose continuation byte is `0x80`–`0x9f` (`À` = `C3 80`) AND lets dangerous code points survive (`U+FEFF` BOM has no byte in the stripped ranges), while INV-4's byte-scan test passes anyway. | Sanitizer replaced with **rune-level `strconv.Quote(string(b))`** — escapes every non-`IsPrint` rune (C0/C1, ANSI, bidi `U+202E`, `U+2028/9`, NEL, BOM) + invalid UTF-8, preserves printable accented/CJK, subsumes `ToValidUTF8`. INV-4 + Task 3 `sanitizeExcerpt` contract rewritten. |
| F2 | MAJOR | Branch 19c asserted `len(Excerpt) <= 256` — satisfied equally by a broken **sanitize-1MiB-then-truncate**; it did not prove cap-before-**read** (the DoS-amplification bound). | 19c now asserts **bytes consumed from the body ≤ 256** via a byte-counting reader; `readExcerpt` contract states cap-the-READ. |
| F3 | MAJOR | Branch 19b scanned **byte** classes, so it passed while `U+FEFF`/bidi (dangerous **runes**) survived — the "100%-covered, asserts nothing" trap on the exact threat class. | 19b now asserts **every rune `unicode.IsPrint`** after unquoting, feeds BOM/`U+202E`/NEL/invalid-UTF-8 + a printable `À`/CJK, and asserts the printable one **survives**. |
| F4 | MINOR | Order `strip → ToValidUTF8` could rejoin fragments into a new dangerous rune (`E2 80 <ESC> A8` → `U+2028`). | Moot under `strconv.Quote` (rune-level, no strip-then-rejoin); noted in the contract. |
| F5 | MINOR | `WithErrorBodyExcerpt` godoc "safe to render" overclaimed under the byte-strip. | True under the rune-level sanitizer; wording kept, now accurate. |
| F6 | MINOR | Branch 9b was a no-op unless the message actually carried `msgin.correlation-id` (value) and `MSGIN.Correlation-Id` under the exact case-sensitive key — `msgin.New` stamps neither. | 9b now mandates seeding both reserved headers with non-empty values so removing the guard turns it red. |
| F7 | MINOR | No test covered a panic **inside** `readExcerpt` with the excerpt enabled (branch 24 tests the main-read path, excerpt off). | New branch **19e**: excerpt on + panicking body → `recover()` non-nil, value preserved, `closed == true`. |
| — | NOTE | 4b's "non-empty" needs a printable byte to distinguish classify-first from drain-first; excerpt adds ≤256B atop the O1 drain budget. | 4b now requires a printable byte; `WithMaxResponseBytes` godoc notes the separate ≤256B excerpt budget. |

---

## Whole-branch review (pre-merge gate). `/security-review` clean; all 5 `/code-review` findings fixed, mutation verdicts all RED.

| # | Finding | Disposition |
|---|---|---|
| F1 | `ClassifyResponse` panics on a hand-built nil `resp.Body` with `WithErrorBodyExcerpt()` on — never panic on caller input. | **Fixed**: excerpt read guarded on `resp.Body != nil` (Excerpt stays empty); covering nil-Body test. |
| F2 | No behavioral proof of the 1 MiB `defaultMaxResponseBytes` — every INV-6 test passed an explicit cap. | **Fixed**: default-config test through `Exchange` (exactly `1<<20` succeeds, `+1` → `ErrReplyTooLarge`); mutating the default to `1<<10` verified RED. |
| F3 | `ErrUnsupportedPayload` text stale ("reply payload") — since Task 2 it also covers outbound request payloads. | **Fixed**: text now "message payload is not []byte or string"; message pinned by test. |
| F4 | Request-describing `http.*` metadata (content-type/method/path/query) rode from the request seed onto the reply. | **Fixed**: `buildReply` deletes the four request-descriptive `http.*` keys before seeding; provenance tests both ways. |
| F5 | A genuine read error at the INV-6 probe boundary was misreported as `ErrReplyTooLarge`. | **Fixed**: `(1,_)` → over-cap; `(0, EOF)` → clean end at cap; any other `(0, err)` — incl. `io.ErrUnexpectedEOF`, a truncated transfer — → raw read error, mirroring the pre-cap read-failure path. |

---

## Self-review against the defect catalogue

Every VERIFIED defect in `024-http-outbound-source-brief.md` is addressed: D3.1 (Task 2 extraction + staging), D3.2/D3.3
(one helper, `Headers().String`), D3.4/D5.1 (INV-1, Task 1 + Task 4 branches 11–14), D3.5 (INV-6 caveat), D3.6/D5.11
(Coverage realism — guards deleted, not relaxed), D3.7 (resolve once at construction), D3.8 (import list corrected),
D3.9/D5.7/D6.8 (`permanentEncode`, one shared helper), D3.10 (`WithOutboundClock`), D3.11 (documented, not rejected),
D3.12/D5.10/D6.11 (no per-test `goleak`), D4.1 (Task 3 overflow clamp, branch 13), D4.2 (branch 17's note),
D4.3 (resolved by 023's floor), D4.4 (branch 5 hand-built + branch 11 live), D4.5 (assert-closure constraint),
D4.7/D5.8 (INV-5, `redactTransport`), D5.2 (`BytesPayloadCodec` everywhere), D5.3 (`BlockUntilContext`),
D5.4 (two-phase), D5.5/D5.6 (no unbounded example policy, no cap assumption), D5.9/D6.10 (multi-instance section),
D6.1 (INV-2), D6.2 (INV-6 probe), D6.3 (Task 5 provenance), D6.4/D6.5 (Task 0's `Gateway` limitation),
D6.6 (documented), D6.7 (note rewritten — the real hazard is INV-2), D6.9 (open decision 2), D6.12 (honest test note).

**Defects in the brief that are WRONG against the tree**, and must not be "fixed" back: Open Decisions **F** (Plan 023
has landed — `msgin.RetryAfter`/`WithProducerRetry` exist), **G** (`msgin.BytesPayloadCodec` already exists in
`codec.go`) and **H** (the floor is shipped: `producer.go:487-489`) are all **already resolved**; the brief's
`producer.go` line citations (`33-37`, `97-102`, `138-147`) are stale post-Plan-023.

---

## Round-1 design audit (independent Opus, against the tree). Verdict: NOT READY → folded in.

The seven security invariants (INV-1..INV-7) were **verified sound** — redirect no-follow survives a caller-supplied
client, the shallow client copy is copylocks-clean, INV-2's non-reserved key holds, classification never infers a
payload sentinel from a remote status, the `*url.Error` redaction drops the query secret while keeping both `errors.Is`
targets, `LimitReader(N<0)` EOF is real and the O2 read-then-probe design avoids it, and O2's single deferred close
with no `recover()` matches the contract. A re-audit was judged unwarranted; the fixes are localized.

| # | Sev | Finding | Fix folded in |
|---|---|---|---|
| 1 | MAJOR | Branch 13's `9223372036` is the **largest VALID** delay-seconds (`= MaxInt64/1e9`, → ~292y), not an overflow, so the "rejected" assertion fails red-first against the correct guard AND the overflow multiply is never exercised. | Split into 13a (`…036` accepted) + 13b (`…037` first overflow, rejected); boundary and `secs > clamp`-before-multiply rule stated and arithmetically verified. |
| 2 | MAJOR | Task 1's coverage note was self-contradictory — it claimed rows 9–12 (`validateURL`) both are and are not gated in Task 1, but `validateURL` is unreachable blackbox until `NewOutbound` (Task 4), so Task 1 cannot hit 100% on `outbound.go`. | Task 1 gate scoped to rows 1–5 + 14; the "100%" restated as a Task-6 delivery gate; `validateURL`/`resolveClient` explicitly 0% at Task 1, reconciled at Task 4. |
| 3 | MINOR | Task 6 "amend Task 0's commit" needs a non-HEAD rebase, which the environment blocks. | Changed to a follow-up `docs:` commit; amend option removed. |
| 4 | MINOR | Row 20's nil-`Config` test only reaches `clockOrDefault()`'s nil branch on the `429`/`503`+`Retry-After` path. | Row 20 now specifies that path. |
| 5 | MINOR | Task 5 abbreviated `NewExchange` construction to "as Task 4", risking an omitted `resolveClient` → O2 silently follows redirects (INV-1 breach). | Task 5 now restates the three construction steps and names the trap. |

---

## Round-2 design audit (independent Opus, against the tree). Verdict: APPROVE-WITH-FIXES → folded in. No re-audit.

Baseline factual accuracy re-verified **HIGH**: all 22 "verified against the tree" rows and every inline `file:line`
citation re-confirmed correct; INV-1..INV-7 mechanisms sound (`ErrUseLastResponse` returns `(resp,nil)` body-unclosed;
`*url.Error` drops `URL`, `%w` preserves `errors.Is`; read-exactly-`max`-then-probe avoids the `max+1` wrap; reserved
skip blocks the forge; the classification chain never matches a payload sentinel). SPI conformance, `Gateway`
limitation honesty, retry composition, INV-7/Spec-012 contract, multi-instance topology and `BytesPayloadCodec`
discipline all checked SOUND. The findings are **localized plan/coverage defects**, not design holes; the auditor judged
a full re-audit unwarranted, so the fixes below are folded and implementation proceeds.

| # | Sev | Finding | Fix folded in |
|---|---|---|---|
| F1 | MAJOR | `redactTransport`'s `if errors.As {return} / return` sketch has a blackbox-**unreachable** fallthrough (`Client.Do` always wraps in `*url.Error`) → fails the mandatory 100% gate, and `//nolint` is forbidden. | Task 4 sketch rewritten to a **single-return conditional-assignment** form (`op, inner` fallback, no dead `return`); added to "Coverage realism"; INV-5 realizer note updated. |
| F2 | MAJOR | The shared `drainBounded` contract was contradictory: O1 used it as its **sole** body-closer (branches 8/9) while O2 called it under an already-registered `defer Close` — one reading **leaks the O1 connection**, the other **double-closes O2**. | Pinned `drainBounded` to **drain-only, never closes** (signature `io.Reader`); O1 `Send` now adds its own `defer resp.Body.Close()` then drains; every path in both adapters closes via exactly one deferred `Close` (INV-7 literally true). |
| F3 | MAJOR | Task 1's gate ("rows 1–5 + 14") greenlit **value-blind** tests: `Config` fields are unexported, so rows 1/3/4/5/14 could assert only "construction succeeded" — the project's recurring "100%-covered, asserts nothing" trap. | Only **row 2** kept as a genuine Task-1 behavioral assertion; rows 1/3/4/5/14 marked line-covered-only, their behavioral assertions **deferred** to where the value is observable (row 1→Task 5, 4/5→Task 4, 14→Task 2). |
| F4 | MINOR | INV-6's one-byte probe used a lone `Read`; `io.Reader` legally returns `(0, nil)`, which `n > 0` would misread as end-of-body → truncated success (silent data loss). | Probe switched to **`io.ReadFull`** (loops past `(0,nil)`); INV-6 statement updated; new Task-5 branch **20b** injects a `(0,nil)` boundary read. |
| F5 | MINOR | INV-1 was stated as an absolute but is defeatable by a caller-supplied redirect-following client. | INV-1 headline qualified with its two named escapes; `WithHTTPClient` godoc obligation now states the escape at the option. |
| F6 | MINOR | `defaultMaxResponseBytes` silently also caps O1's reuse-drain (undocumented); the 30s client timeout was never reasoned against Plan 023's `cap 60s < budget 2m` ordering. | `WithMaxResponseBytes` godoc notes the O1-drain coupling; the constant's comment explains the 30s is the per-attempt I/O bound (closing ADR 0025 §1.1's "single unbounded call" gap), orthogonal to the retry budget. |
