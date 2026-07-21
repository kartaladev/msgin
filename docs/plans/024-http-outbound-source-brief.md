# Plan 024 source brief — HTTP outbound (O1/O2), condensed + defect-annotated

**Purpose.** Replaces reading lines 1602–3260 of the withdrawn combined plan
(`/private/tmp/claude-501/.../0113b927-.../scratchpad/SUPERSEDED-022-combined-source-material.md`).
Every code block below is the *drafted* content, condensed. **Nothing here is correct until the DEFECTS
list for its task is applied.** Defect priority: (a) `docs/HANDOVER.md` §5 audit findings, (b) my own
verification against the real `adapter/http` tree (file:line cited, marked VERIFIED).

**Governing artifacts:** `docs/specs/011-http-adapter.md` §3.4, §4; `docs/specs/013-producer-outbound-retry.md`;
`docs/adrs/0025-producer-outbound-retry.md`; ADR 0022 Addendum A3 / Spec 012 §7; `docs/HANDOVER.md` §5, §5.1, §8.

**Hard sequencing fact (VERIFIED).** `msgin.RetryAfter`, `msgin.WithProducerRetry`, `msgin.ErrDeadLettered` **do not
exist**. `producer.go:97-102` — `Send` is a bare passthrough; `producerConfig` (`producer.go:33-37`) has only
`codec`/`codecSet`/`clock`. All of that is **Plan 023**. Plan 024 cannot compile a `RetryAfter` call or an end-to-end
producer-retry test until 023 lands. See Open Decision F.

---

## Task 3 — outbound config surface + `EncodeRequest`

### What it builds

Files: modify `adapter/http/options.go`, `adapter/http/errors.go`; create `adapter/http/outbound.go`,
`adapter/http/outbound_test.go`. **Plus (see D3.1) `adapter/http/encode.go` must be modified and staged in the
same commit.**

Exported surface:

```go
func WithHTTPClient(c *http.Client) Option          // nil = no-op
func WithOutboundHeaders(headers ...string) Option  // slices.Clone
func WithReplyHeaders(headers ...string) Option     // slices.Clone
func WithMaxResponseBytes(n int64) Option           // set-flag; n<=0 explicit -> error
func WithClock(clk clockwork.Clock) Option          // nil = no-op
func EncodeRequest(ctx context.Context, method, url string, msg msgin.Message[any], cfg *Config) (*http.Request, error)

// sentinels in errors.go
ErrEmptyURL, ErrInvalidURL, ErrInvalidMaxResponseBytes, ErrReplyTooLarge
```

Unexported accessors: `client()`, `allowedOutboundHeaders()`, `allowedReplyHeaders()`, `maxResponse()`,
`clockOrDefault()`.

### Essential code (condensed)

```go
const defaultMaxResponseBytes int64 = 1 << 20        // 1 MiB
const defaultHTTPClientTimeout = 30 * time.Second    // finite ON PURPOSE: http.DefaultClient has NO timeout

// Config fields added
httpClient *http.Client; outboundHeaders, replyHeaders []string
maxResponseBytes int64; maxResponseBytesSet bool; clock clockwork.Clock

func (c *Config) client() *http.Client {
	if c == nil || c.httpClient == nil { return &http.Client{Timeout: defaultHTTPClientTimeout} }
	return c.httpClient
}
func (c *Config) maxResponse() int64 {
	if c == nil || c.maxResponseBytes <= 0 { return defaultMaxResponseBytes }
	return c.maxResponseBytes
}
func (c *Config) clockOrDefault() clockwork.Clock {
	if c == nil || c.clock == nil { return clockwork.NewRealClock() }
	return c.clock
}
// allowedOutboundHeaders/allowedReplyHeaders: nil-Config -> nil, else the slice.

// NewConfig additions, after the successStatus block:
if !cfg.maxResponseBytesSet { cfg.maxResponseBytes = defaultMaxResponseBytes
} else if cfg.maxResponseBytes <= 0 { return nil, ErrInvalidMaxResponseBytes }
if cfg.httpClient == nil { cfg.httpClient = &http.Client{Timeout: defaultHTTPClientTimeout} }
if cfg.clock == nil { cfg.clock = clockwork.NewRealClock() }
```

```go
func EncodeRequest(ctx context.Context, method, url string, msg msgin.Message[any], cfg *Config) (*http.Request, error) {
	body, err := payloadBytes(msg)          // <-- DOES NOT EXIST, see D3.1
	if err != nil { return nil, err }
	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(body))
	if err != nil { return nil, err }
	for _, name := range cfg.allowedOutboundHeaders() {
		v, ok := msg.Header(name); if !ok { continue }
		s, ok := v.(string); if !ok { continue }
		req.Header.Set(name, sanitizeHeaderValue(s))
	}
	req.Header.Set("Content-Type", sanitizeHeaderValue(contentTypeOf(msg)))  // AFTER the allow-list: cannot be overridden
	return req, nil
}

func contentTypeOf(msg msgin.Message[any]) string {
	v, ok := msg.Header(msgin.HeaderContentType); if !ok { return defaultResponseContentType }
	s, ok := v.(string); if !ok || s == "" { return defaultResponseContentType }
	return s
}
```

Security-relevant godoc to keep verbatim in spirit:
- `WithOutboundHeaders`: "**NOT** guarded against the reserved `msgin.*` namespace — trusted operator config; listing
  `msgin.correlation-id` DOES publish internal flow metadata to the remote endpoint." Matching is **case-SENSITIVE**
  (exact message-header map lookup). `Content-Type` written after ⇒ un-overridable.
- `WithReplyHeaders`: reserved `msgin.*` entries are **defensively ignored** — the remote server is untrusted input
  exactly as a client is inbound. Matched case-insensitively (`http.Header.Get`), first value only.
- `EncodeRequest`: "`url` is CALLER-configured and never derived from `msg`."

Drafted hot-path branch table (14 rows) — payload `[]byte`/`string`/other, Content-Type from flow/default×3,
allow-list forward + CRLF-sanitize, non-allow-listed dropped, allow-list cannot override Content-Type,
`http.NewRequestWithContext` failure, plus the four option-validation rows.

### DEFECTS

- **D3.1 — `payloadBytes` does not exist. VERIFIED.** The logic is **inline** in `EncodeResponse`
  (`adapter/http/encode.go:177-185`, a `switch payload := msg.Payload().(type)` with `[]byte` / `string` / `default:
  return ErrUnsupportedPayload`). The draft's "find it with gopls and reuse it" is false. → Extract it (gopls
  Extract-function) into `encode.go`, and **add `adapter/http/encode.go` to the Task 3 commit stage list** — the draft's
  `git add` (source line 2051) omits it, which is a broken-build commit exactly like round-2 finding C1.
- **D3.2 — `contentTypeOf` is NOT the same logic `EncodeResponse` has. VERIFIED.** `encode.go:193-196` does
  `ct, _ := msg.Headers().String(...)` then `if ct = sanitizeHeaderValue(ct); ct == ""` — i.e. **sanitize-then-empty
  check** (so a value of `"\r\n"` collapses to the default), and it uses `Headers().String` (present+string in one
  call, `message.go:50`). The drafted `contentTypeOf` type-asserts manually and sanitizes at the call site. Reconcile to
  ONE helper with `Headers().String` + sanitize-then-check; do not ship two subtly different resolutions.
- **D3.3 — use `msg.Headers().String(name)` in the allow-list loop. VERIFIED.** The drafted `msg.Header(name)` +
  manual `.(string)` reimplements `Headers.String` (`message.go:50`) and diverges from the repo idiom at
  `encode.go:188`.
- **D3.4 — CRITICAL: the default client follows redirects → SSRF.** (HANDOVER §5 CRITICAL.) `&http.Client{Timeout:…}`
  follows up to 10 redirects; `302 → http://169.254.169.254/…` makes **O2 return IMDS credentials into the flow**, O1 a
  blind-SSRF port-scan oracle; 307/308 replays the POST body and allow-listed headers (Go strips only
  `Authorization`/`Cookie`); https→http downgrade is permitted. `validateURL` is a **construction-time** check only.
  **Fix:** default `CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }` — this
  also makes the `3xx → Permanent` arm live rather than dead code. Also handle the caller-supplied-client case
  (Open Decision B), and qualify the "SSRF invariant" wording in Spec 011 §4 / Spec 013 §4, which currently reads as a
  guarantee: msgin does **no** private-IP/metadata filtering.
- **D3.5 — `WithMaxResponseBytes` godoc over-claims.** (HANDOVER §5 MAJOR.) Response **headers** are outside the cap
  (bounded instead by `http.Transport.MaxResponseHeaderBytes`, default ~10 MiB). Its honest inbound twin
  `WithMaxBodyBytes` carries exactly such a CAVEAT paragraph (`options.go:130-135`) — mirror it. VERIFIED the caveat
  exists inbound and is absent from the draft.
- **D3.6 — the nil-guards in `client()` / `maxResponse()` / `clockOrDefault()` are partly unreachable blackbox.**
  `NewConfig` now fills all three, and `client()`/`maxResponse()` are **only** reached from `Outbound.Send` /
  `Exchange.Exchange`, whose `cfg` always came from `NewConfig` ⇒ their nil/zero arms are **dead**. VERIFIED by call-graph
  inspection. (`clockOrDefault()` and the two header accessors ARE reachable, because `ClassifyResponse`/`EncodeRequest`
  are **exported and take `*Config`**, and `Config` is exported-with-unexported-fields so `_test` can pass
  `&msghttp.Config{}` / `nil` — see `options.go:26-34`.) Consequence: the draft's "every `outbound.go` function at
  **100.0%**" gate is unachievable. Precedent for *declining* such guards: `retry.go`'s `sweepLoop`. → Open Decision E.
- **D3.7 — `client()` allocates a fresh `*http.Client` per call on the fallback path.** Harmless only because the arm is
  dead (D3.6); if the guard is kept, hoist it to a package-level var or it silently defeats connection reuse.
- **D3.8 — import list is wrong.** The draft says add `"strings"` to `options.go`; nothing in the drafted options
  additions uses it (`strings` is needed by `outbound.go`'s `validateURL`). Needed in `options.go`: `clockwork`, `time`.
  `slices` is already there (`options.go:7`). VERIFIED.
- **D3.9 — `ErrUnsupportedPayload`'s godoc says "reply message's payload" (`errors.go:47-53`) and `DefaultErrorStatus`
  maps it to 500** (`encode.go:249-251`). Reusing it for an outbound *request* payload requires the godoc widened.
  Separately (HANDOVER §5 core): **encode failures must be `msgin.Permanent`-wrapped** or a missing `Transform` burns
  the full attempt budget and dead-letters. That wrap belongs at the `Outbound.Send`/`Exchange` boundary (Tasks 5/6).
- **D3.10 — `msghttp.WithClock` shadows a core concept.** `msgin.WithClock` already exists as a *MessageOption*
  (`message.go:125`), and the repo's stated convention (`producer.go:44-46`) is to name clocks distinctly
  (`WithConsumerClock`, `WithProducerClock`). Different packages so no compile collision, but consider
  `WithOutboundClock`. Minor.
- **D3.11 — `Config` is a shared inbound+outbound bag.** (HANDOVER §5 MAJOR.) `NewOutbound(url, WithMaxBodyBytes(0))`
  errors on an irrelevant setting; inbound constructors silently accept inert outbound options. Security-relevant
  confusable pairs: `WithResponseHeaders` vs `WithReplyHeaders`, `WithMaxBodyBytes` vs `WithMaxResponseBytes`.
  → Open Decision A.
- **D3.12 — test hygiene.** The draft puts `defer goleak.VerifyNone(t)` in every test. `adapter/http` already has
  `TestMain` + `goleak.VerifyTestMain` (`encode_test.go:21` — VERIFIED); per-test `VerifyNone` around `httptest`
  servers is flake-prone (idle keep-alive conns). Drop it.

---

## Task 4 — `ClassifyResponse`, `StatusError`, `parseRetryAfter`

### What it builds

Files: modify `adapter/http/outbound.go`, `adapter/http/errors.go`; extend `outbound_test.go`.

```go
type StatusError struct{ Code int }
func (e *StatusError) Error() string  // "msghttp: outbound request returned status <n>"
func (e *StatusError) Unwrap() error  // ErrOutboundStatus
func ClassifyResponse(resp *http.Response, cfg *Config) error
// sentinels: ErrOutboundStatus, ErrNilResponse
```

### The rule (one rule, stated once)

`2xx` → `nil`. Transient set = **`408`, `429`, all `5xx`**. **Everything else outside 2xx — including `1xx` and
`3xx` — is `msgin.Permanent`.** Within the transient set, `429`/`503` **only** honour a parseable `Retry-After`
(RFC 9110 defines it for those two) → `msgin.RetryAfter(err, d)`.

```go
func ClassifyResponse(resp *http.Response, cfg *Config) error {
	if resp == nil { return ErrNilResponse }
	code := resp.StatusCode
	if code >= 200 && code < 300 { return nil }
	err := &StatusError{Code: code}
	transient := code >= 500 || code == http.StatusRequestTimeout || code == http.StatusTooManyRequests
	if !transient { return msgin.Permanent(err) }
	if code == http.StatusTooManyRequests || code == http.StatusServiceUnavailable {
		if d, ok := parseRetryAfter(resp.Header.Get("Retry-After"), cfg.clockOrDefault().Now()); ok {
			return msgin.RetryAfter(err, d)
		}
	}
	return err
}

func parseRetryAfter(v string, now time.Time) (time.Duration, bool) {
	v = strings.TrimSpace(v)
	if v == "" { return 0, false }
	if secs, err := strconv.Atoi(v); err == nil {
		if secs < 0 { return 0, false }
		return time.Duration(secs) * time.Second, true      // <-- OVERFLOWS, see D4.1
	}
	if t, err := http.ParseTime(v); err == nil {
		if d := t.Sub(now); d > 0 { return d, true }
		return 0, true                                       // past date clamps to 0 -> see D4.3
	}
	return 0, false
}
```

Security-relevant comment to keep: `StatusError` "deliberately carries NEITHER the target URL (which may embed
credentials in its userinfo) NOR any response body (attacker-controlled)".

Drafted branch table (16 rows): nil resp; 200/204/299; 400/404; 408; 429 w/o header; 429 `Retry-After: 30`;
503 HTTP-date (fake clock); 503 unparseable; `-5`; past date; 500; 301; 100; 502 with header ignored; `errors.As`
→ `*StatusError.Code`; `errors.Is(ErrOutboundStatus)` through both wrappers.

### DEFECTS

- **D4.1 — `parseRetryAfter` int64 overflow. VERIFIED by inspection.** `strconv.Atoi` accepts up to `MaxInt64`;
  `time.Duration(secs) * time.Second` overflows for `secs > ~9.22e9` → **negative duration returned with `ok == true`**
  (the `secs < 0` guard is *before* the multiply) → immediate retry / negative timer. **Fix:** clamp — reject
  `secs > int64(math.MaxInt64/time.Second)`, or compute in `int64` and clamp to the configured cap.
- **D4.2 — CRITICAL semantics: `Retry-After` must be a MINIMUM, not an override.** (HANDOVER §5, RFC 9110 §10.2.3.)
  The drafted design lets a server **shorten** the client's computed backoff to zero. Correct combination (in Plan 023's
  `nextDelay`): `max(computed, min(serverDelay, cap))`. **Consequence for Plan 024:** the Task 5 integration test's
  assertion *"the server's Retry-After must win over the 1s computed backoff"* (source line 2558-2559) is **wrong** and
  must be rewritten.
- **D4.3 — `Retry-After: 0` and past HTTP-dates yield a 0 delay → hot spin**, remote-triggerable; the cap clamps only
  the upper bound. A floor belongs in Plan 023, but Plan 024 must not *emit* an unusable 0 without saying so.
- **D4.4 — `3xx → Permanent` is dead code until D3.4's `CheckRedirect` fix lands.** Ordering dependency: land the
  redirect fix in the same or an earlier task, or branch 12 (`301 → Permanent`) is only reachable via a hand-built
  `*http.Response` (which is in fact how the drafted table builds it — acceptable, but say so).
- **D4.5 — test tables violate the project's assert-closure rule.** The retry-after table uses `want`/`wantDelay`
  fields and an invented `-1` sentinel meaning "no marker" (source 2148-2185), plus an invented `dateAhead` field.
  CLAUDE.md + the `table-test` skill mandate an `assert func(t, …)` closure per case, never `want`/`wantErr`. Rewrite.
- **D4.6 — `isPermanentForTest` depends on `msgin.WithProducerRetry`, which does not exist. VERIFIED**
  (`producer.go:33-77`). Either sequence Plan 024 after Plan 023, or assert permanence some other way. Note also the
  HANDOVER finding that **"`Permanent` beats `RetryAfter`" is documented everywhere and tested nowhere** — a test
  asserting only `errors.Is(err, cause)` is true of both markers independently.
- **D4.7 — transport errors leak credentials.** (HANDOVER §5 MAJOR.) `client.Do` returns `*url.Error`, which redacts
  only the **password** — username, host, path and query survive. A webhook token in the query string therefore lands
  in every timeout/dial error the caller logs. `StatusError`'s careful non-leakage is undone one line away in
  `Outbound.Send` / `Exchange` (Tasks 5/6), which return `err` raw. **Fix:** wrap/redact at those two sites.
- **D4.8 — the "100.0% per function on `outbound.go`" coverage claim** (Step 6) is unachievable per D3.6.

---

## Task 5 — O1 `NewOutbound`

### What it builds

Files: modify `adapter/http/outbound.go`; extend `outbound_test.go`.

```go
type Outbound struct{ url string; cfg *Config }
var _ msgin.OutboundAdapter = (*Outbound)(nil)          // Send(ctx, Message[any]) error — spi.go:42
func NewOutbound(url string, opts ...Option) (*Outbound, error)
func (o *Outbound) Send(ctx context.Context, msg msgin.Message[any]) error
func drainAndClose(body io.ReadCloser, max int64)        // unexported
func validateURL(raw string) error                       // unexported, reused by Task 6
```

```go
func NewOutbound(url string, opts ...Option) (*Outbound, error) {
	cfg, err := NewConfig(opts...); if err != nil { return nil, err }
	if err := validateURL(url); err != nil { return nil, err }
	return &Outbound{url: url, cfg: cfg}, nil
}

func (o *Outbound) Send(ctx context.Context, msg msgin.Message[any]) error {
	req, err := EncodeRequest(ctx, http.MethodPost, o.url, msg, o.cfg)
	if err != nil { return err }                          // <-- must be msgin.Permanent-wrapped (D3.9)
	resp, err := o.cfg.client().Do(req)
	if err != nil { return err }                          // <-- *url.Error leak (D4.7)
	defer drainAndClose(resp.Body, o.cfg.maxResponse())
	return ClassifyResponse(resp, o.cfg)
}

func drainAndClose(body io.ReadCloser, max int64) {
	if body == nil { return }                             // unreachable for a Client.Do response (D3.6 class)
	_, _ = io.Copy(io.Discard, io.LimitReader(body, max))
	_ = body.Close()
}

func validateURL(raw string) error {
	if strings.TrimSpace(raw) == "" { return ErrEmptyURL }
	u, err := url.Parse(raw); if err != nil { return ErrInvalidURL }
	if u.Scheme != "http" && u.Scheme != "https" { return ErrInvalidURL }
	if u.Host == "" { return ErrInvalidURL }
	return nil
}
```

Godoc contract to keep: **AT-MOST-ONCE by itself**; AT-LEAST-ONCE only under a retry authority (a `Producer` with
`WithProducerRetry`, or a Consumer-driven flow's redelivery); a source-less flow gets **neither**. Receiver must be
idempotent — key on `msgin.HeaderMessageID`, forwarded only when listed in `WithOutboundHeaders`.

Test helpers introduced here (moved to a shared `testhelpers_test.go` in Task 6): `trackingBody` (`closed
atomic.Bool`, `read atomic.Int64`) and `roundTripperFunc`.

Drafted branch table (12): empty/whitespace URL; `"://bad"`; `file:///etc/passwd`; a bad `Option`; 2xx; non-2xx;
transport error; encode failure with **0 server hits**; body drained+closed on both paths; drain bounded by the cap;
ctx cancellation; end-to-end retry of a 503.

### DEFECTS

- **D5.1 — CRITICAL: `validateURL` is a construction-time check that redirects bypass.** Same as D3.4; this is the
  place the "SSRF invariant" is asserted and where it is false. The godoc paragraph beginning "The URL is fixed at
  construction and NEVER derived from a message" must be qualified: it prevents *message-driven* SSRF, not
  *server-driven* SSRF, and msgin does no private-IP/metadata filtering.
- **D5.2 — the flagship composition POSTs base64. VERIFIED.** `NewProducer[[]byte](out)` → `resolveCodec`
  (`producer.go:138-147`) sees a non-`LiveValueSource` adapter and defaults to `JSONPayloadCodec[[]byte]{}`
  (`codec.go:12-16`); `json.Marshal([]byte("event"))` is `"ZXZlbnQ="`. `JSONPayloadCodec` is the **only** codec in the
  repo (VERIFIED by grep). The drafted `TestOutboundRetriesUnderProducerRetry` and the doc examples therefore do not do
  what they claim. → Open Decision G.
- **D5.3 — `clock.BlockUntil(1)` is deprecated and deadlocks. VERIFIED via HANDOVER §5/§8.** `clockwork.Advance` never
  appends waiters (`fc.waiters = fc.waiters[1:]`); `BlockUntil` is `// Deprecated:` in v0.5.0 in favour of
  `BlockUntilContext`. The repo already uses `BlockUntilContext` (~10 sites in `aggregator_test.go`). Rewrite.
- **D5.4 — the "measured wait" assertion is true by construction.** `Advance(30s)` then asserting
  `clock.Now().Sub(start) == 30s` cannot detect *under*-waiting. Use the two-phase advance from HANDOVER §5.1:
  `Advance(want - 1ns)` → assert not-yet-returned → `Advance(1ns)` → assert returned.
- **D5.5 — the drafted policy `RetryPolicy{Backoff: ExponentialBackoff{Initial: time.Second}}` has
  `MaxAttempts == 0`,** which `RetryPolicy` documents as **retry forever, no DLQ** (`retry.go:11-24`, `Validate` at
  `:27-36` accepts it). VERIFIED. On the caller's own goroutine that is an unbounded synchronous wait. Fine for the
  happy-path test, dangerous as an example; and per HANDOVER §5 the nil-`Backoff` zero value is a **zero-delay infinite
  hot loop**.
- **D5.6 — `ExponentialBackoff` field names verified** (`backoff.go:19-24`: `Initial`, `Max`, `Mult`,
  `RandomizationFactor`; `Max <= 0` means **uncapped**). The HANDOVER MAJOR about the cap not being applied to the
  computed backoff and the float→int out-of-range conversion is Plan 023's, but Plan 024's tests must not assume a cap.
- **D5.7 — encode failure must be `msgin.Permanent`-wrapped** here (D3.9), otherwise branch 8's "no request issued"
  case still burns the whole attempt budget upstream.
- **D5.8 — transport error returned raw** → D4.7 leak. Also: the godoc claims a transport failure "is returned
  unwrapped as a plain transient error"; that is deliberate for retryability but must not stay unredacted.
- **D5.9 — multi-instance load amplification.** (HANDOVER §5 MAJOR, CLAUDE.md mandatory rule.) N horizontally-scaled
  instances each retry a throttling endpoint independently, delivering **N× the load** the server asked to shed. Must
  be stated in the godoc with the seam named (ADR 0006 rate-limit / circuit-breaker), not left implied.
- **D5.10 — `defer goleak.VerifyNone(t)` + `httptest`** → flaky (D3.12).
- **D5.11 — coverage:** `drainAndClose`'s `body == nil` arm and `client()`'s fallback are unreachable → the "100.0% per
  function" gate again. Open Decision E.

---

## Task 6 — O2 `NewExchange`

### What it builds

Files: create `adapter/http/exchange.go`, `adapter/http/exchange_test.go`, `adapter/http/testhelpers_test.go`.

```go
type Exchange struct{ url string; cfg *Config }
var _ msgin.RequestReplyExchange = (*Exchange)(nil)   // Exchange(ctx, Message[any]) (Message[any], error) — exchange.go:35
func NewExchange(url string, opts ...Option) (*Exchange, error)
func (x *Exchange) Exchange(ctx context.Context, req msgin.Message[any]) (msgin.Message[any], error)
func (x *Exchange) buildReply(req msgin.Message[any], resp *http.Response, body []byte) msgin.Message[any]
```

**The contract (ADR 0022 Addendum A3 / Spec 012 §7):** release **all** request-scoped state on **every** exit path
including a panic unwind — **deferred cleanup only**, never per-return-site — and **never** recover a panic into an
error return. Request-scoped state here = the in-flight `*http.Request` and the response body.

**Return Address:** satisfied **by construction** — the reply returns on the same TCP connection the sending instance
holds, so no reply can arrive at a different instance. The **async-callback variant** (reply delivered later to an
inbound webhook) is what *would* need an instance-addressable callback URL + durable correlation store; out of scope
for v1 and must be **named** in the godoc so the constraint is recorded rather than implied.

```go
func (x *Exchange) Exchange(ctx context.Context, req msgin.Message[any]) (msgin.Message[any], error) {
	var zero msgin.Message[any]
	httpReq, err := EncodeRequest(ctx, http.MethodPost, x.url, req, x.cfg)
	if err != nil { return zero, err }
	resp, err := x.cfg.client().Do(httpReq)
	if err != nil { return zero, err }
	// The ONLY cleanup site: covers the classified-error return, the read-failure
	// return, the over-cap return, and a panic unwinding out of any of them.
	defer func() { _ = resp.Body.Close() }()

	if err := ClassifyResponse(resp, x.cfg); err != nil {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, x.cfg.maxResponse()))
		return zero, err
	}
	max := x.cfg.maxResponse()
	body, err := io.ReadAll(io.LimitReader(resp.Body, max+1))   // <-- OVERFLOWS at MaxInt64, see D6.2
	if err != nil { return zero, err }
	if int64(len(body)) > max { return zero, ErrReplyTooLarge }
	return x.buildReply(req, resp, body), nil
}

func (x *Exchange) buildReply(req msgin.Message[any], resp *http.Response, body []byte) msgin.Message[any] {
	reply := msgin.New[any](body)                               // <-- DISCARDS all request headers, see D6.3
	if v, ok := req.Header(msgin.HeaderCorrelationID); ok { reply = reply.WithHeader(msgin.HeaderCorrelationID, v) }
	if ct := resp.Header.Get("Content-Type"); ct != "" {
		reply = reply.WithHeader(msgin.HeaderContentType, ct)    // <-- REFLECTED XSS, see D6.1
	}
	for _, name := range x.cfg.allowedReplyHeaders() {
		if strings.HasPrefix(strings.ToLower(name), reservedHeaderPrefix) { continue }  // remote is untrusted
		if v := resp.Header.Get(name); v != "" { reply = reply.WithHeader(name, v) }
	}
	return reply
}
```

The panic-unwind test (write FIRST): a `trackingBody` wrapping a `panickingReader` whose `Read` panics
`"read exploded"`; assert `recover()` is non-nil **and** the original value is preserved **and** `tb.closed` is true.

Drafted branch table (19): construction ×3; 200 body → `[]byte` payload; correlation id propagated; absent
correlation id yields no empty header; response Content-Type → header; empty Content-Type sets nothing; allow-listed
response header lands; non-allow-listed does not; allow-listed **reserved** name stripped; non-2xx → classified error
+ zero reply; transport error; encode failure with no request issued; over-cap → `ErrReplyTooLarge`; exactly-at-cap
succeeds; body closed on success / on non-2xx / on panic unwind; ctx cancellable; end-to-end through
`msgin.NewGateway`.

### DEFECTS

- **D6.1 — CRITICAL: reflected XSS reopened from the outbound side. VERIFIED.** `buildReply` writes the **remote
  server's** `Content-Type` onto the reserved `msgin.HeaderContentType` — the exact key `EncodeResponse` trusts as the
  response media type (`encode.go:193-197`), which `DecodeRequest` deliberately refuses to let a client set
  (`encode.go:66-70`). `X-Content-Type-Options: nosniff` does **not** stop an explicit `text/html`. So an untrusted
  upstream chooses the media type its own bytes are served back under through an `OutboundGateway` → `EncodeResponse`
  flow. **Fix:** land it on the **non-reserved** `headerHTTPContentType = "http.content-type"` constant, which already
  exists at **`encode.go:25`** (VERIFIED), mirroring `DecodeRequest`. Any trusted variant must be an explicit, warned
  opt-in option.
- **D6.2 — `io.ReadAll(io.LimitReader(resp.Body, max+1))` overflows. VERIFIED by inspection.**
  `WithMaxResponseBytes(math.MaxInt64)` is a legal caller value; `max+1` wraps to `MinInt64`; `io.LimitReader` with
  `N < 0` returns EOF immediately → `body` is empty → `len(body) > max` is false → an **empty payload is returned as a
  success**. Silent data loss. **Fix:** guard `max == math.MaxInt64` (or read `max` bytes and probe one more byte
  separately).
- **D6.3 — `buildReply` discards every request header. VERIFIED.** It starts from `msgin.New[any](body)` and copies
  back only `msgin.HeaderCorrelationID`. `OutboundGateway` (`gateway.go:41-56`) forwards that reply to `next`, so a
  Splitter's `msgin.sequence-number` / `msgin.sequence-size` headers are **lost across the hop**, breaking a downstream
  Aggregator. **Fix:** seed the reply from the request's headers (`req.Headers().All()` is an `iter.Seq2`,
  `message.go:69`; `msgin.NewHeaders`/`msgin.WithHeaders` take a `map[string]any`) and then overlay the
  response-derived ones. Decide explicitly which reserved request headers are re-stamped vs. carried
  (`msgin.id`/`msgin.timestamp` should be fresh).
- **D6.4 — the Gateway integration test is wrong on three counts. VERIFIED.**
  - `Gateway` exposes **`Request(ctx, req Req) (Rep, error)`** (`gateway.go:64`), **not `Exchange`**.
  - `Request` takes a **raw `Req`**, not a `msgin.Message[Req]`; it mints the correlation id itself
    (`New(req).WithHeader(HeaderCorrelationID, randomID())`). The drafted `g.Exchange(t.Context(),
    msgin.New[[]byte](...))` is wrong on both argument and method.
  - It returns a `Rep` value, not a message — `reply.Payload()` does not compile.
  Correct call: `reply, err := g.Request(t.Context(), []byte(`"hi"`))` with `g` of type `*msgin.Gateway[[]byte, []byte]`.
- **D6.5 — "drops into `Gateway`/`OutboundGateway` unchanged" over-claims. VERIFIED.** `Gateway` has **no codec at
  all** — it uses `boxMessage` on the way in and `PayloadOf[Rep]` (a type assertion) on the way out
  (`gateway.go:64-76`). So `msghttp.Exchange` behind a `Gateway` works **only** for `[]byte`/`string` `Rep`;
  `Gateway[MyStruct, MyStruct]` fails with `ErrPayloadType`. No artifact records this limitation — Spec 011 §3.4 and
  the drafted godoc both need the caveat.
- **D6.6 — the non-2xx path discards the server's error body** (bounded drain, then return). Correct security posture
  (attacker-controlled), but currently undocumented; a caller debugging a 4xx gets only the status code. State it.
- **D6.7 — the draft's "Ordering note" about `Content-Type` is confused.** An allow-listed literal `"Content-Type"`
  reply header sets the message key `"Content-Type"`, which is a *different* key from `msgin.content-type`; it is not
  reserved, so the strip does not catch it, and it does not overwrite anything. The real hazard is D6.1. Rewrite the
  note; keep the test that `msgin.content-type` cannot be forged through `WithReplyHeaders`.
- **D6.8 — encode failure must be `msgin.Permanent`-wrapped** and the `*url.Error` transport return redacted, exactly
  as in Task 5 (D3.9, D4.7). Both are duplicated code paths — extract one helper so the class is fixed, not the
  instance (HANDOVER §5.1 meta-lesson).
- **D6.9 — the type is `Exchange` and its method is `Exchange`.** Legal Go, but `msghttp.Exchange` reads ambiguously
  against `msgin.RequestReplyExchange.Exchange`. Consider `HTTPExchange` / `RequestReply`. Minor; a rename is free only
  before the first tag.
- **D6.10 — multi-instance:** same as D5.9, plus the Return Address statement above must be in the godoc.
- **D6.11 — coverage/goleak:** as D3.6 / D3.12.
- **D6.12 — the panic test's placement is sound** (the panic fires inside `io.ReadAll`, i.e. *after* the `defer` is
  registered), but it does **not** cover a panic between `Do` returning and the `defer` executing — there is no such
  window in the drafted code, so say so rather than implying broader coverage.

---

## Task 7 (docs/gate) — carry-over notes

- All commit trailers in the source say `Plan: 022`; Plan 024 must renumber. `Spec: 013` belongs to Plan 023's core
  work — only cite it from 024 where the HTTP side actually realizes a Spec 013 clause.
- The withdrawn plan committed **all docs last**, so every `feat` commit carried `Plan:`/`ADR:` trailers pointing at
  artifacts not yet in history (HANDOVER §5.1). Either invoke the documented docs-ahead-of-code exception explicitly,
  or couple the plan/ADR edits per task.
- Spec 011 §3.0 layout delta (O1/O2 in `adapter/http`, not `adapter/http/stdlib`), §3.4 real signatures
  (`(*Outbound, error)` / `(*Exchange, error)`), §3.1 mapping table, §3.6 sentinel set — all still needed.
- ADR 0025 Addendum A1–A6 as drafted, **but A3/A4 must be amended** for D4.2 (Retry-After is a minimum) and D3.4
  (`CheckRedirect` default is what makes A3's `3xx → Permanent` meaningful).
- `gorelease` cannot run — **zero git tags** (VERIFIED via HANDOVER §8). Additive-only ⇒ minor bump by inspection.
  Repeat the standing recommendation to cut `v0.1.0`.
- Whole-branch gate: `/code-review` + `/security-review` over `main..HEAD`. Standing lesson — a clean per-task review
  is not a substitute; Plan 021's whole-branch pass found two proven vulns every per-task review had cleared.

---

## Open decisions for the plan author

**A. One `Config` for inbound+outbound, or split?** (D3.11)
*Options:* (1) keep the single `Config`/`Option` — no duplication of `NewConfig`, no new exported type;
(2) a separate `OutboundOption`/`outboundConfig` — precise validation, but duplicates plumbing and is a bigger API.
**Recommend (1) plus disambiguating names**: `WithOutboundHeaders` → keep; `WithReplyHeaders` →
`WithOutboundReplyHeaders`; `WithMaxResponseBytes` → keep but cross-reference `WithMaxBodyBytes` in both godocs
("inbound request body" vs "outbound response body"). Names are free only before the first tag. Document that
inbound-only options are inert on an outbound constructor rather than trying to reject them.

**B. `CheckRedirect` and caller-supplied clients.** (D3.4)
*Options:* (1) set the no-follow `CheckRedirect` only on the **default** client — leaves every `WithHTTPClient` caller
vulnerable; (2) **shallow-copy** the caller's `*http.Client` in `NewOutbound`/`NewExchange` and set `CheckRedirect` if
it is nil (Transport/Jar still shared by pointer, which is intended); (3) reject a caller client with a nil
`CheckRedirect` unless an explicit opt-in option is passed.
**Recommend (2) + a `WithFollowRedirects()` opt-in** that restores default following, with a godoc SSRF warning. It is
fail-safe by default, does not mutate the caller's object, and keeps the escape hatch explicit.

**C. Where the remote `Content-Type` lands on the reply.** (D6.1)
**Recommend: the non-reserved `http.content-type`** (`encode.go:25`), unconditionally — mirroring `DecodeRequest`.
Offer a separate, loudly-warned `WithTrustedReplyContentType()` opt-in for callers who control the upstream, exactly
as `WithTrustedCorrelationID` (`options.go:173-219`) is the awkward opt-in on the inbound side.

**D. Reply header provenance.** (D6.3)
**Recommend: seed the reply from the request's headers**, then overlay response-derived ones, re-stamping
`msgin.id`/`msgin.timestamp` fresh and preserving `msgin.correlation-id` + any sequence headers. Add a branch-table row
proving a Splitter sequence header survives an `OutboundGateway` hop. Alternative (status quo) loses Aggregator
correctness silently — reject it.

**E. Unreachable defensive nil-guards vs. the 100%-per-function gate.** (D3.6, D5.11)
*Options:* (1) keep the guards and relax the coverage claim to "≥85% package, 100% on every *reachable* branch, with
the unreachable guards enumerated"; (2) drop the guards on `client()`/`maxResponse()`/`drainAndClose` since their
callers always hold a `NewConfig`-built `*Config`.
**Recommend (1)** — the guards match `options.go:26-34`'s stated hand-built-`Config` policy, and `retry.go`'s
`sweepLoop` is the recorded precedent for *declining* a guard purely for coverage. Enumerate the dead arms in the plan
so the gate is honest rather than gamed.

**F. Sequencing against Plan 023.** (D4.6, Task 5 branch 12)
`msgin.RetryAfter` / `WithProducerRetry` do not exist. *Options:* (1) Plan 024 strictly follows Plan 023 on the same
branch; (2) Plan 024 ships classification returning a plain transient error and a follow-up task adds the `RetryAfter`
call once 023 lands.
**Recommend (1)** — 023's `nextDelay` also owns the `max(computed, min(server, cap))` fix (D4.2), which Plan 024's
tests must assert against. Gate Plan 024's Task 4 on 023 being green.

**G. The `[]byte` base64 problem.** (D5.2)
`NewProducer[[]byte](msghttp.NewOutbound(url))` — the increment's flagship composition — POSTs
`"ZXZlbnQ="`. *Options:* (1) ship a `msgin.BytesPayloadCodec[[]byte]` (identity codec) in core and use it in every
example/test; (2) make `Outbound` advertise `LiveValueSource` — **wrong**, it is a wire adapter and this would defeat
codec pairing for real typed payloads; (3) document only.
**Recommend (1)**, as a small core addition in Plan 023 (or a 024 Task 0), with every example and the
`ExampleNewOutbound`/`ExampleNewExchange` output blocks using it. Option (3) alone ships a headline example that lies.

**H. Retry-After floor.** (D4.3) `Retry-After: 0` / past dates → zero wait. **Recommend** a documented floor
(the computed backoff, per D4.2's `max(...)`) so the zero case degrades to normal backoff rather than a hot loop; then
Plan 024 need only guarantee `parseRetryAfter` returns a non-negative, non-overflowing value.
