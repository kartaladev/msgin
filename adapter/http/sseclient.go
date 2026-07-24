package msghttp

import (
	"net/http"

	"github.com/kartaladev/msgin"
)

// SSEClient is the S-in msgin.StreamingSource (Spec 011 §3.5, ADR 0023
// Addendum C): it connects to a caller-configured text/event-stream
// endpoint, emits each parsed event as a msgin.Delivery, and auto-reconnects
// under a clamped exponential backoff (WithReconnectBackoff), carrying
// Last-Event-ID across reconnects on a best-effort basis. Stream — the
// actual connect/parse/reconnect loop — lands in a later task; this file
// declares only the type, its constructor, and its (fixed) NativeReliability
// answers.
//
// Delivery guarantee: AT-MOST-ONCE, best-effort resume. Resume works only
// against a server that honors a Last-Event-ID request header (e.g. this
// package's own NewSSEServer with WithReplayBuffer); a server that ignores
// it simply replays nothing extra. Ack/Nack on the delivered messages are
// no-ops — SSE has no acknowledgement protocol on the wire.
//
// MULTI-INSTANCE TOPOLOGY (CLAUDE.md "Production robustness —
// multi-instance awareness"): the resume position (the last-seen event id)
// is held ONLY in memory for the lifetime of a single Stream call — a
// restarted process, or a Stream call on a different instance, resumes from
// the live stream, not from the last id a prior instance saw. Competing
// consumers do not apply here: every SSEClient receives the full stream
// independently, there is no consumer-group partitioning.
//
// SSEClient carries no other mutable state: it is safe to call Stream
// again after it returns, and a single instance is not shared for
// concurrent Stream calls in the documented usage (mirrors the other
// adapters' one-Stream-caller-at-a-time contract).
type SSEClient struct {
	url    string
	client *http.Client
	cfg    *Config
}

var _ msgin.NativeReliability = (*SSEClient)(nil)

// NewSSEClient builds an SSEClient targeting url. It validates the options
// (NewConfig), then the URL (validateURL — non-empty, http/https, non-empty
// host, reused verbatim from the O1/O2 outbound adapters), then resolves the
// *http.Client ONCE via resolveSSEClient — deliberately NOT outbound's
// resolveClient, which would carry NewConfig's 30s defaultHTTPClientTimeout
// back-fill and abort every long-lived stream (Plan 026 audit MAJOR-1).
func NewSSEClient(url string, opts ...Option) (*SSEClient, error) {
	cfg, err := NewConfig(opts...)
	if err != nil {
		return nil, err
	}
	if err := validateURL(url); err != nil {
		return nil, err
	}
	return &SSEClient{url: url, client: resolveSSEClient(cfg), cfg: cfg}, nil
}

// NativeRedelivery reports false: an SSEClient's best-effort Last-Event-ID
// resume is not a redelivery guarantee — a server that discards its replay
// buffer, or a client that never resumes (no Last-Event-ID held yet), simply
// loses whatever was missed. Callers needing at-least-once delivery must
// layer it themselves (e.g. an idempotent consumer reading a durable
// upstream log the SSE endpoint fronts).
func (c *SSEClient) NativeRedelivery() bool { return false }

// NativeDeadLetter reports false: there is no dead-letter concept for a
// streamed SSE connection — an event that fails downstream processing is
// simply gone; the runtime's own dead-letter policy (RetryPolicy) is what a
// caller relies on instead.
func (c *SSEClient) NativeDeadLetter() bool { return false }

// resolveSSEClient returns the *http.Client Stream uses for its per-connection
// requests, resolved ONCE at construction (mirrors resolveClient's
// once-at-construction discipline) but with a DELIBERATELY DIFFERENT default
// from outbound's resolveClient (Plan 026 audit MAJOR-1): the returned
// client's Timeout is never forced to a finite value.
//
// http.Client.Timeout bounds the ENTIRE request, including the response body
// read — exactly what a long-lived text/event-stream connection needs to
// stay open past. Outbound's resolveClient reuses cfg.httpClient, which
// NewConfig back-fills to a client with the 30s defaultHTTPClientTimeout
// when unset; reusing that here would silently abort every SSE stream after
// 30 seconds regardless of how healthy it is. resolveSSEClient therefore
// reads cfg.httpClientSet (set ONLY inside WithHTTPClient's `if c != nil`
// guard) rather than cfg.httpClient's resolved value:
//
//   - httpClientSet (the caller supplied a non-nil *http.Client): a SHALLOW
//     COPY is taken, preserving the caller's Timeout untouched (including a
//     caller who deliberately wants a finite one), and the no-follow
//     CheckRedirect (INV-C6, mirrors resolveClient) is installed on the copy
//     unless the caller opted into WithFollowRedirects or already set their
//     own CheckRedirect.
//   - unset (including an explicit WithHTTPClient(nil), which is a
//     documented no-op and therefore never sets the flag): a FRESH
//     &http.Client{} is built with Timeout left at its zero value (no
//     timeout) so http.DefaultTransport's net.Dialer{KeepAlive: 30s} is what
//     detects a dead peer (INV-C7) — WithReadTimeout is the opt-in, stronger
//     alternative for a caller who needs faster or guaranteed detection. The
//     no-follow CheckRedirect is installed unless WithFollowRedirects was set.
func resolveSSEClient(cfg *Config) *http.Client {
	if cfg.httpClientSet {
		c := cfg.httpClient
		if cfg.followRedirects || c.CheckRedirect != nil {
			return c
		}
		cp := *c
		cp.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
		return &cp
	}
	c := &http.Client{}
	if !cfg.followRedirects {
		c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	}
	return c
}
