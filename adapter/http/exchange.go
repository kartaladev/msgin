package msghttp

import (
	"context"
	"errors"
	"io"
	"maps"
	"net/http"
	"strings"

	"github.com/kartaladev/msgin"
)

// Exchange is the O2 RequestReplyExchange: it performs a synchronous HTTP
// request-reply — POST the request message's payload to a fixed,
// caller-configured URL and turn the response into a reply message. It is the
// FIRST external implementation of msgin.RequestReplyExchange, and therefore the
// first bound by that SPI's no-leak-on-unwind contract (see Exchange below).
//
// Return Address (multi-instance topology): satisfied BY CONSTRUCTION. The reply
// returns on the SAME TCP connection the sending instance holds open — the
// connection IS the return address — so no reply can arrive at a different
// instance and the cross-process correlation problem does not arise. Exchange
// holds no correlator, registry or cross-request state; it is the degenerate,
// stateless case of the SPI. The shape that WOULD need an explicit Return
// Address is the ASYNC-CALLBACK variant (the request carries a callback URL and
// the reply is delivered later to an inbound webhook that may land on a
// different instance); it is out of scope for v1. Such an exchange would mint an
// instance-addressable callback URL and correlate the late reply through a
// durable store — an ADAPTER change, not a core change.
//
// Gateway limitation (verified against msgin.Gateway): a msgin.Gateway over this
// Exchange works ONLY for a request payload that is []byte or string (what
// EncodeRequest accepts) and a reply type of []byte (what Exchange produces).
// msgin.Gateway carries NO codec — it type-asserts the reply — so
// NewGateway[MyStruct, MyStruct] fails with msgin.ErrPayloadType, and so does
// NewGateway[[]byte, string]. Only NewGateway[[]byte, []byte] (or a request of
// string) round-trips. msgin.OutboundGateway, which forwards the reply message
// itself, is not so restricted.
//
// SSRF non-guarantee (INV-1): the default no-follow redirect policy stops a
// response from steering the request to another host, UNLESS the caller opted in
// via WithFollowRedirects() or supplied a *http.Client whose own CheckRedirect
// follows redirects. Independently, msgin performs NO private-IP, link-local,
// loopback or metadata-endpoint filtering (see validateURL/ErrInvalidURL). The
// URL is never derived from a message, so message-driven SSRF cannot arise.
//
// A hand-built &Exchange{} that skips NewExchange is unsupported and will
// nil-deref, exactly as a hand-built &msgin.ChannelExchange{} does — the
// resolved *Config it reads directly (the no-follow client, maxResponseBytes) is
// only ever the one NewConfig produced.
type Exchange struct {
	url    string
	client *http.Client
	cfg    *Config
}

var _ msgin.RequestReplyExchange = (*Exchange)(nil)

// NewExchange builds an Exchange performing a synchronous HTTP request-reply
// against url. It runs the SAME three-step sequence as NewOutbound — NewConfig
// (validate the options), validateURL (non-empty, http/https, non-empty host,
// stored as x.url), then resolveClient ONCE (the no-follow redirect-safe client,
// stored as x.client) — so the no-follow policy that closes INV-1 for O2 is not
// silently skipped.
func NewExchange(url string, opts ...Option) (*Exchange, error) {
	cfg, err := NewConfig(opts...)
	if err != nil {
		return nil, err
	}
	if err := validateURL(url); err != nil {
		return nil, err
	}
	return &Exchange{url: url, client: resolveClient(cfg), cfg: cfg}, nil
}

// Exchange POSTs req's payload to the configured URL and returns the response as
// a reply message, or the classified error.
//
// INV-7 (no leak on unwind): exactly ONE deferred resp.Body.Close(), registered
// immediately after a nil-error Do, releases the request-scoped state (the
// in-flight request and the body) on EVERY exit path — the classified-error
// return, the read-failure return, the over-cap return, and a panic unwinding
// out of any of them (ADR 0022 Addendum A3 / Spec 012 §7 / ADR 0025 §5). It
// NEVER recover()s a caller panic into an error return: the panic belongs to the
// caller's code and propagates with its original value.
//
// A non-2xx reply is classified (ClassifyResponse) and its body is bounded-drained
// and DELIBERATELY discarded — the remote error body is attacker-controlled, so
// a caller debugging a 4xx/5xx gets the status code, not the body (opt into a
// bounded, sanitized excerpt with WithErrorBodyExcerpt on the StatusError). A 2xx
// body is read bounded by WithMaxResponseBytes; a body that exceeds the cap
// returns ErrReplyTooLarge.
func (x *Exchange) Exchange(ctx context.Context, req msgin.Message[any]) (msgin.Message[any], error) {
	var zero msgin.Message[any]

	httpReq, err := EncodeRequest(ctx, http.MethodPost, x.url, req, x.cfg)
	if err != nil {
		return zero, permanentEncode(err)
	}

	resp, err := x.client.Do(httpReq)
	if err != nil {
		return zero, redactTransport(err)
	}
	defer func() { _ = resp.Body.Close() }() // INV-7: the ONE cleanup site; no recover(), ever

	if err := ClassifyResponse(resp, x.cfg); err != nil {
		drainBounded(resp.Body, x.cfg.maxResponseBytes) // bounded; the error body is deliberately discarded
		return zero, err
	}

	max := x.cfg.maxResponseBytes
	body, err := io.ReadAll(io.LimitReader(resp.Body, max))
	if err != nil {
		return zero, err
	}
	if int64(len(body)) == max {
		// INV-6: read exactly max, then probe one further byte with io.ReadFull —
		// NOT a lone Read. io.Reader legally returns (0, nil), which a `Read`+`n>0`
		// check would misread as end-of-body and serve a truncated body as success
		// (the silent-data-loss INV-6 closes; round-2 audit F4). ReadFull loops
		// past (0, nil) until it fills the byte or hits a real error: ONLY n>0
		// proves there was more body than the cap allows.
		var probe [1]byte
		n, probeErr := io.ReadFull(resp.Body, probe[:])
		if n > 0 {
			return zero, ErrReplyTooLarge
		}
		if !errors.Is(probeErr, io.EOF) {
			// ONLY a clean io.EOF proves the body ended AT the cap; any other
			// (0, err) probe outcome is a genuine READ failure at the boundary
			// and surfaces raw, exactly as the pre-cap read-failure path above
			// does — never as ErrReplyTooLarge (whole-branch review F5). That
			// includes io.ErrUnexpectedEOF: io.ReadFull cannot manufacture it
			// for a 1-byte buffer, so it came from the body itself — a
			// TRUNCATED transfer (the connection died before Content-Length
			// was satisfied, or mid-chunk), which must not be served as an
			// at-cap success (the silent data loss INV-6 exists to prevent).
			return zero, probeErr
		}
	}

	return x.buildReply(req, resp, body), nil
}

// buildReply turns a successful response into the reply message. It is seeded
// from the REQUEST's headers so flow metadata survives the hop — a Splitter's
// msgin.sequence-number/size would otherwise be lost, silently breaking a
// downstream Aggregator, and msgin.correlation-id is carried automatically (what
// OutboundGateway's save/restore expects). Per-message identity is re-stamped
// fresh by msgin.New (a new msgin.message-id and msgin.timestamp); the request's
// media type (msgin.content-type) AND the request-describing http.* metadata
// (http.method, http.path, http.query, http.content-type) are dropped because
// they describe the request, not the reply — only a Content-Type the remote
// response actually carries lands on the reply's http.content-type.
//
// The remote response is untrusted input (INV-2): its Content-Type lands on the
// NON-reserved http.content-type key — never msgin.content-type, which
// EncodeResponse trusts as the served media type (a reflected-XSS vector from the
// outbound side) — and any allow-listed reply header whose name carries the
// reserved msgin. prefix is defensively skipped, so the remote can never forge a
// core header. Every copied value is CRLF-sanitized.
func (x *Exchange) buildReply(req msgin.Message[any], resp *http.Response, body []byte) msgin.Message[any] {
	h := maps.Collect(req.Headers().All())
	delete(h, msgin.HeaderMessageID)
	delete(h, msgin.HeaderTimestamp)
	delete(h, msgin.HeaderContentType)
	// The request-DESCRIBING http.* metadata DecodeRequest stamps on an
	// inbound-originated message describes the inbound request, not this reply
	// (whole-branch review F4): left in place, a stale request-side
	// http.content-type would masquerade as the reply's media type whenever the
	// remote response carries no Content-Type, and http.method/path/query would
	// misreport the reply's provenance. http.correlation-id is advisory FLOW
	// metadata, not request shape, so it survives the hop like
	// msgin.correlation-id does.
	delete(h, headerHTTPMethod)
	delete(h, headerHTTPPath)
	delete(h, headerHTTPQuery)
	delete(h, headerHTTPContentType)

	reply := msgin.New[any](body, msgin.WithHeaders(h))
	if ct := resp.Header.Get("Content-Type"); ct != "" {
		reply = reply.WithHeader(headerHTTPContentType, sanitizeHeaderValue(ct))
	}
	for _, name := range x.cfg.allowedReplyHeaders() {
		if strings.HasPrefix(strings.ToLower(name), reservedHeaderPrefix) {
			continue // the remote is untrusted (INV-2): it cannot forge a core header
		}
		if v := resp.Header.Get(name); v != "" {
			reply = reply.WithHeader(name, sanitizeHeaderValue(v))
		}
	}
	return reply
}
