package msghttp

import (
	"net/http"
	"net/url"
	"strings"
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
