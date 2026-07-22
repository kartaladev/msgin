package msghttp

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/kartaladev/msgin"
)

// This file holds the construction-time helpers shared by the O1 webhook
// OutboundAdapter (NewOutbound) and the O2 RequestReplyExchange (NewExchange).
// Those exported types and constructors arrive in a later task; here only
// validateURL and resolveClient exist, and neither is reachable from a blackbox
// test until the constructors call them — so both sit at 0% coverage until then,
// by construction (Plan 024 Task 1's coverage note).
//
// As with a hand-built &msgin.ChannelExchange{}, a hand-built &Outbound{} or
// &Exchange{} that skips its constructor is unsupported and will nil-deref: the
// resolved *Config those types read directly (httpClient, maxResponseBytes) is
// only ever the one NewConfig produced.

// EncodeRequest builds an *http.Request carrying msg's payload as the body, to
// be sent to url with the given method. It is the shared request encoder both
// outbound adapters (NewOutbound, NewExchange) build their single per-attempt
// request with.
//
// url is CALLER-configured and is NEVER derived from msg: the message payload
// or headers cannot steer the request to a different target (this is what makes
// message-driven SSRF impossible — see validateURL/ErrInvalidURL for the
// non-guarantees this does NOT provide, e.g. no private-IP egress filtering).
//
// The payload is extracted with payloadBytes: a []byte or string payload
// becomes the body, any other type returns ErrUnsupportedPayload and NO request
// is built. Headers are then written in a LOAD-BEARING order:
//
//   - allow-listed message headers FIRST (cfg's WithOutboundHeaders), each with
//     its value CRLF-sanitized. An entry whose name carries the reserved
//     "msgin." prefix (compared case-INSENSITIVELY, so "MSGIN.Foo" too) is
//     dropped, so internal flow metadata is never published to the remote
//     endpoint (decision 4 — symmetry with WithOutboundReplyHeaders and
//     DecodeRequest). The message-header VALUE lookup stays case-SENSITIVE (an
//     exact msg.Headers().String map lookup); a non-string header value is
//     skipped.
//   - Content-Type LAST, from contentTypeOf(msg), so an allow-list entry naming
//     Content-Type can never override the resolved request Content-Type.
//
// A nil cfg, or one built by hand rather than by NewConfig, is tolerated: the
// allow-list is empty and Content-Type resolves to its default.
func EncodeRequest(ctx context.Context, method, url string, msg msgin.Message[any], cfg *Config) (*http.Request, error) {
	body, err := payloadBytes(msg)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}

	for _, name := range cfg.allowedOutboundHeaders() {
		if strings.HasPrefix(strings.ToLower(name), reservedHeaderPrefix) {
			continue
		}
		if v, ok := msg.Headers().String(name); ok {
			req.Header.Set(name, sanitizeHeaderValue(v))
		}
	}

	req.Header.Set("Content-Type", contentTypeOf(msg))

	return req, nil
}

// validateURL rejects a target URL that is empty/whitespace (ErrEmptyURL), or
// that fails to parse, is not http/https, or has an empty host (ErrInvalidURL).
//
// It guards against a malformed or non-HTTP target only. It is NOT an SSRF
// egress filter: msgin performs no private-IP, link-local, loopback or
// metadata-endpoint filtering, so a caller-configured internal address is
// accepted verbatim (see ErrInvalidURL). Because the URL is caller-configured
// and never derived from a message payload, message-driven SSRF cannot arise.
func validateURL(raw string) error {
	if strings.TrimSpace(raw) == "" {
		return ErrEmptyURL
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ErrInvalidURL
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return ErrInvalidURL
	}
	if u.Host == "" {
		return ErrInvalidURL
	}
	return nil
}

// resolveClient returns the *http.Client to use for a single outbound attempt,
// applying the no-follow redirect policy (INV-1) without ever mutating the
// caller's client. It is resolved ONCE at construction, never per Send.
//
// When the caller left CheckRedirect nil (net/http's follow-up-to-10 default),
// a SHALLOW COPY is taken and the no-follow policy installed on the copy;
// Transport and Jar stay shared by pointer, which is intended — connection
// pooling and cookies are preserved. A caller who set WithFollowRedirects(), or
// supplied a client with its own CheckRedirect, made a deliberate choice and it
// is honored: the client is returned verbatim.
func resolveClient(cfg *Config) *http.Client {
	c := cfg.httpClient // never nil: NewConfig fills it
	if cfg.followRedirects || c.CheckRedirect != nil {
		return c
	}
	cp := *c
	cp.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &cp
}

// StatusError reports that an outbound request completed but the remote returned
// a non-2xx HTTP status. It carries the status Code and, ONLY when
// WithErrorBodyExcerpt() is set, a bounded and fully-sanitized Excerpt of the
// response body (default: empty).
//
// It deliberately carries NEITHER the target URL — which may embed credentials
// in its userinfo (INV-5) — NOR the raw response body or resp.Status text, which
// are attacker-controlled (INV-4). Error() is the status code alone, plus the
// already-sanitized Excerpt when opted in. Unwrap returns ErrOutboundStatus, so
// errors.Is(err, ErrOutboundStatus) holds through any msgin.Permanent or
// msgin.RetryAfter wrapper ClassifyResponse adds around it.
type StatusError struct {
	Code    int
	Excerpt string
}

// Error renders the status code, appending the sanitized Excerpt only when it is
// non-empty. The Excerpt is produced by sanitizeExcerpt (strconv.Quote), so it
// is already quote-delimited and free of control characters.
func (e *StatusError) Error() string {
	if e.Excerpt == "" {
		return fmt.Sprintf("msghttp: outbound request returned status %d", e.Code)
	}
	return fmt.Sprintf("msghttp: outbound request returned status %d: body %s", e.Code, e.Excerpt)
}

// Unwrap returns ErrOutboundStatus.
func (e *StatusError) Unwrap() error { return ErrOutboundStatus }

// ClassifyResponse maps a completed outbound response onto the runtime's error
// vocabulary (ADR 0023 §5, ADR 0002). The adapter performs one attempt and only
// CLASSIFIES; the producer's RetryPolicy (WithProducerRetry) owns backoff,
// budget and dead-lettering.
//
//   - a nil response -> ErrNilResponse (decision 3);
//   - 2xx -> nil (the body is left untouched here — the caller drains it for
//     connection reuse);
//   - 408, 429 and every 5xx are TRANSIENT; within that set, 429 and 503 honour
//     a parseable Retry-After as a msgin.RetryAfter MINIMUM;
//   - everything else outside 2xx — including 1xx and 3xx — is msgin.Permanent.
//
// It NEVER derives a payload sentinel (ErrPayloadType / ErrPayloadDecode /
// ErrPayloadTooLarge) or any status-to-sentinel mapping from a remote-controlled
// status (INV-3): the only error it produces is a *StatusError, optionally
// wrapped in msgin.Permanent or msgin.RetryAfter.
//
// When WithErrorBodyExcerpt() is set, the excerpt is read from resp.Body BEFORE
// the classification branches, so every non-2xx path carries it; the read is
// bounded (readExcerpt caps at errorBodyExcerptMax) and best-effort — a read
// error never masks the status error. The caller must therefore classify BEFORE
// draining the body, or the excerpt is empty.
func ClassifyResponse(resp *http.Response, cfg *Config) error {
	if resp == nil {
		return ErrNilResponse
	}
	code := resp.StatusCode
	if code >= 200 && code < 300 {
		return nil
	}

	statusErr := &StatusError{Code: code}
	if cfg.errorBodyExcerptEnabled() {
		statusErr.Excerpt = readExcerpt(resp.Body)
	}

	// Everything outside 2xx that is not in the transient set {5xx, 408, 429} —
	// including 1xx and 3xx — is permanent (De Morgan of the transient predicate).
	if code < 500 && code != 408 && code != 429 {
		return msgin.Permanent(statusErr)
	}
	if code == 429 || code == 503 {
		if d, ok := parseRetryAfter(resp.Header.Get("Retry-After"), cfg.clockOrDefault().Now()); ok {
			return msgin.RetryAfter(statusErr, d)
		}
	}
	return statusErr
}

// maxRetryAfterSeconds is the largest delay-seconds value that does not overflow
// time.Duration when multiplied by time.Second (MaxInt64 / 1e9 ≈ 292 years). A
// value strictly greater overflows the multiply to a negative duration, so it is
// rejected BEFORE multiplying.
const maxRetryAfterSeconds = int64(math.MaxInt64) / int64(time.Second)

// parseRetryAfter parses an HTTP Retry-After header value (RFC 9110 §10.2.3) into
// a non-negative delay. It supports both forms:
//
//   - delay-seconds: a non-negative integer; a negative value, or one large
//     enough to overflow time.Duration (> maxRetryAfterSeconds), is rejected;
//   - HTTP-date: parsed with http.ParseTime; the delay is the date minus now, and
//     a date already in the past yields (0, true).
//
// It NEVER returns a negative or overflowed duration. A returned 0 is safe: the
// producer floors every wait at minRetryDelay, so a past date or Retry-After: 0
// degrades to the normal computed backoff rather than a hot spin. Semantics of
// how the delay combines with the backoff live in the producer, not here.
func parseRetryAfter(v string, now time.Time) (time.Duration, bool) {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0, false
	}
	if secs, err := strconv.ParseInt(v, 10, 64); err == nil {
		if secs < 0 || secs > maxRetryAfterSeconds {
			return 0, false
		}
		return time.Duration(secs) * time.Second, true
	}
	if t, err := http.ParseTime(v); err == nil {
		d := t.Sub(now)
		if d < 0 {
			return 0, true
		}
		return d, true
	}
	return 0, false
}

// readExcerpt reads at most errorBodyExcerptMax bytes from body — capping the
// READ, not just the output, so an unbounded error body cannot amplify into
// large read/allocate work — then sanitizes them. It is best-effort: a read
// error returns the sanitized bytes that did arrive (or the empty-string
// quoting), never an error, so a broken error body cannot mask the status error.
func readExcerpt(body io.Reader) string {
	b, _ := io.ReadAll(io.LimitReader(body, errorBodyExcerptMax))
	return sanitizeExcerpt(b)
}

// sanitizeExcerpt renders untrusted remote bytes safe to embed in an error
// string. strconv.Quote operates at the RUNE level: it escapes every
// non-unicode.IsPrint rune (C0/C1 controls, ANSI escapes, bidi overrides,
// BOM/zero-width, line/paragraph separators) and every invalid-UTF-8 byte to a
// printable \uXXXX / \xNN sequence, preserves printable accented/CJK/emoji runes
// verbatim, and wraps the whole in quotes so the untrusted text is unambiguously
// delimited. A byte-level control strip would be WRONG — it mangles multi-byte
// printable runes and lets some dangerous code points survive.
func sanitizeExcerpt(b []byte) string {
	return strconv.Quote(string(b))
}

// Outbound is the O1 webhook OutboundAdapter: it POSTs a message's payload to a
// fixed, caller-configured URL and classifies the response into the runtime's
// error vocabulary (nil / plain-transient / msgin.RetryAfter / msgin.Permanent).
// The adapter performs ONE attempt and only classifies; retry/backoff/dead-letter
// are owned by the producer (WithProducerRetry, ADR 0002 / ADR 0025).
//
// Delivery guarantee: AT-MOST-ONCE by itself — one POST, no retry. AT-LEAST-ONCE
// arises only under a retry authority (a Producer with WithProducerRetry, or a
// Consumer-driven flow's redelivery); a source-less flow gets neither. The
// receiver must therefore be idempotent — key on msgin.HeaderMessageID, which is
// forwarded to the endpoint ONLY if the caller lists it in WithOutboundHeaders.
//
// Multi-instance topology: per-process by construction. Under WithProducerRetry,
// N horizontally-scaled instances each retry independently, so a throttling
// endpoint sees N× the load it asked to shed and Retry-After compliance is
// per-instance. See the TOPOLOGY paragraph on WithProducerRetry (producer.go),
// which names the ADR 0006 rate-limit/circuit-breaker seam a distributed limiter
// plugs into — it is not restated here, to avoid a second driftable copy.
//
// SSRF non-guarantee (INV-1): the default no-follow redirect policy stops a
// response from steering the request to another host, UNLESS the caller opted in
// via WithFollowRedirects() or supplied a *http.Client whose own CheckRedirect
// follows redirects. Independently, msgin performs NO private-IP, link-local,
// loopback or metadata-endpoint filtering (see validateURL/ErrInvalidURL): a
// caller-configured internal address is requested verbatim. The URL is never
// derived from a message, so message-driven SSRF cannot arise.
//
// A hand-built &Outbound{} that skips NewOutbound is unsupported and will
// nil-deref — the resolved *Config it reads directly (httpClient via the
// no-follow client, maxResponseBytes) is only ever the one NewConfig produced,
// exactly as a hand-built &msgin.ChannelExchange{} is unsupported.
type Outbound struct {
	url    string
	client *http.Client
	cfg    *Config
}

var _ msgin.OutboundAdapter = (*Outbound)(nil)

// NewOutbound builds an Outbound webhook adapter targeting url. It validates the
// options (NewConfig), then the URL (validateURL — non-empty, http/https,
// non-empty host), then resolves the redirect-safe *http.Client ONCE
// (resolveClient), so the no-follow policy is not re-derived on every Send.
func NewOutbound(url string, opts ...Option) (*Outbound, error) {
	cfg, err := NewConfig(opts...)
	if err != nil {
		return nil, err
	}
	if err := validateURL(url); err != nil {
		return nil, err
	}
	return &Outbound{url: url, client: resolveClient(cfg), cfg: cfg}, nil
}

// Send POSTs msg's payload to the configured URL and classifies the response.
//
// The order of operations is LOAD-BEARING (decision 5): ClassifyResponse runs
// BEFORE drainBounded because, with WithErrorBodyExcerpt() set, it reads the
// bounded error-body excerpt from resp.Body — draining first would empty it. On
// a 2xx, ClassifyResponse returns nil without touching the body and the
// subsequent drainBounded drains it (bounded by maxResponseBytes) so the
// keep-alive connection can be reused. The single deferred Close, registered
// immediately after a nil-error Do, is the ONLY close and covers every return
// path including a panic unwind (INV-7); drainBounded never closes (round-2 F2).
//
// An encode failure returns msgin.Permanent (permanentEncode) — a non-bytes
// payload can never succeed on retry — and NO request is sent. A transport
// failure returns a redacted, still-transient error (redactTransport, INV-5)
// carrying no target URL.
func (o *Outbound) Send(ctx context.Context, msg msgin.Message[any]) error {
	req, err := EncodeRequest(ctx, http.MethodPost, o.url, msg, o.cfg)
	if err != nil {
		return permanentEncode(err)
	}
	resp, err := o.client.Do(req)
	if err != nil {
		return redactTransport(err)
	}
	defer func() { _ = resp.Body.Close() }()
	classifyErr := ClassifyResponse(resp, o.cfg)
	drainBounded(resp.Body, o.cfg.maxResponseBytes)
	return classifyErr
}

// permanentEncode wraps an EncodeRequest failure in msgin.Permanent. A payload
// that is neither []byte nor string is a wiring fault that no retry can fix, so
// it must short-circuit the producer's retry budget and dead-letter path rather
// than burning attempts on a request that was never sent.
func permanentEncode(err error) error {
	return msgin.Permanent(err)
}

// redactTransport re-wraps the error (*http.Client).Do returns so that NO target
// URL reaches the error string (INV-5): (*url.Error).Error() redacts only the
// password, leaking username, host, path and query (e.g. a webhook token). It
// discards ue.URL and re-wraps ue.Op + ue.Err under ErrOutboundTransport.
//
// It stays TRANSIENT (never msgin.Permanent) so the runtime retries a transport
// blip, and preserves the underlying cause with %w on ue.Err, so
// errors.Is(err, context.Canceled) / context.DeadlineExceeded still hold. It is
// written as a single return with a conditional assignment rather than an
// `if errors.As { return } / return` pair: Do always wraps its failures in
// *url.Error, so a trailing non-*url.Error return would be unreachable dead code
// (round-2 audit F1). The "Post"/err fallbacks carry no separate statement, so
// the false arm is covered without one.
func redactTransport(err error) error {
	op, inner := "Post", err
	var ue *url.Error
	if errors.As(err, &ue) {
		op, inner = ue.Op, ue.Err
	}
	return fmt.Errorf("%w: %s: %w", ErrOutboundTransport, op, inner)
}

// drainBounded reads and discards up to max bytes from body so the underlying
// keep-alive connection can be returned to the pool for reuse. It is DRAIN-ONLY
// and NEVER closes (round-2 audit F2): the io.Reader signature makes "never
// closes" unforgeable, and the sole deferred resp.Body.Close() at the call site
// is what closes. The drain is best-effort — a read error is ignored — because
// the classified status is the real outcome and the drain only affects reuse.
func drainBounded(body io.Reader, max int64) {
	_, _ = io.CopyN(io.Discard, body, max)
}
