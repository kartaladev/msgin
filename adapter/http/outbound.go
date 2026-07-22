package msghttp

import (
	"bytes"
	"context"
	"net/http"
	"net/url"
	"strings"

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
