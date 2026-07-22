package msghttp

import (
	"bytes"
	"context"
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
