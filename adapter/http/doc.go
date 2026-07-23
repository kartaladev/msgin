// Package msghttp is the framework-agnostic core of the msgin HTTP channel
// adapter (adapter/http). Inbound, it converts an HTTP request to and from an
// msgin.Message[any] (DecodeRequest/EncodeResponse) and supplies the shared
// Config/Option surface both the net/http binding (adapter/http/stdlib) and the
// future gin binding (adapter/http/gin, Phase 5) build their handlers on.
// Outbound, it ships two client-side adapters directly (there is no framework
// variant of an HTTP client): NewOutbound, a webhook msgin.OutboundAdapter, and
// NewExchange, a synchronous request-reply msgin.RequestReplyExchange, over the
// shared EncodeRequest/ClassifyResponse encoder. The package is named msghttp,
// not http, so a consumer's import block never collides with net/http.
//
// # Untrusted boundary
//
// An inbound HTTP request is UNTRUSTED input (CLAUDE.md "Production
// robustness"):
//
//   - DecodeRequest caps the body (WithMaxBodyBytes, default 1 MiB) and copies
//     request headers onto the decoded message only from an explicit
//     allow-list (WithRequestHeaders — empty by default, so nothing is copied
//     unless opted in).
//   - Any copied header whose name carries the reserved "msgin." prefix is
//     defensively stripped (case-insensitively), so a client can never forge a
//     core header (e.g. msgin.delivery-count) even if an operator's allow-list
//     names one by mistake. Client-sourced values that DO travel — the request
//     Content-Type and any advisory correlation id — land on NON-reserved
//     "http.*" headers precisely so they can never be mistaken for core
//     metadata.
//   - The exchange correlation key is the message's own server-minted ID(). A
//     client value keys the exchange only through the explicit, separately
//     named WithTrustedCorrelationID opt-in; WithAdvisoryCorrelationID merely
//     records the client's id for the flow to read.
//   - EncodeResponse never lets the client choose the response media type: it
//     always sends X-Content-Type-Options: nosniff and a Content-Type taken
//     from the FLOW's msgin.HeaderContentType, defaulting to
//     application/octet-stream so net/http never sniffs one out of the body.
//     Response header values are CRLF-sanitized.
//   - The handler cores recover a panicking flow handler and answer 500 rather
//     than letting it escape into the server.
//
// What this package does NOT do (inbound): authentication, authorization,
// CSRF, CORS, rate limiting or method filtering. See the adapter/http/stdlib
// package godoc's "Deploying these handlers safely" section for the inbound
// deployment checklist (including the http.Server timeouts the caller must
// set).
//
// # Outbound
//
// The outbound direction sends to a remote endpoint, so the remote is the
// untrusted party (the mirror of the inbound boundary above). NewOutbound and
// NewExchange share these guarantees; see their type godoc and the WithX option
// godoc for the full contract.
//
//   - SSRF non-guarantee (INV-1): the default redirect policy is NO-FOLLOW, so a
//     3xx response cannot steer the request to another host (e.g. a cloud
//     metadata endpoint) — UNLESS the caller opts in via WithFollowRedirects()
//     or supplies a *http.Client whose own CheckRedirect follows redirects.
//     Independently, msgin performs NO private-IP, link-local, loopback or
//     metadata-endpoint filtering: a caller-configured internal address is
//     requested verbatim. The target URL is never derived from a message, so
//     message-driven SSRF cannot arise; that is the only SSRF property this
//     package guarantees.
//   - The remote response is untrusted input (INV-2): its Content-Type lands on
//     the NON-reserved http.content-type key, never on msgin.content-type, so an
//     upstream cannot re-open reflected XSS from the outbound side. Reply headers
//     copied via WithOutboundReplyHeaders are allow-listed, reserved-name
//     stripped, and CRLF-sanitized.
//   - Errors never leak the target URL, which may embed credentials, nor the raw
//     response body (INV-4/INV-5). A non-2xx yields a StatusError carrying only
//     the status code, unless WithErrorBodyExcerpt() opts into a bounded,
//     fully-sanitized body excerpt.
//
// Delivery guarantee: AT-MOST-ONCE on its own — each adapter performs ONE
// attempt and only CLASSIFIES the outcome (nil / plain-transient /
// msgin.RetryAfter / msgin.Permanent). AT-LEAST-ONCE arises only under a retry
// authority: a msgin.Producer configured with WithProducerRetry (which owns
// backoff, budget, Retry-After combination and dead-lettering), or a
// Consumer-driven flow's redelivery. A source-less flow gets neither, so the
// receiving endpoint must be idempotent.
//
// # Server-Sent Events (SSE)
//
// SSEEvent, EncodeSSEEvent, and SSEEventFromMessage are the encoding half of
// the SSE framing core. EncodeSSEEvent writes a single event (an optional id,
// an optional event name, and a data payload) in the WHATWG
// text/event-stream wire format; before any byte is written it rejects a CR,
// LF, or NUL embedded in the id or event name, so a hostile field value can
// never inject additional, unintended SSE fields into the frame.
// SSEEventFromMessage bridges a msgin.Message[any] into that shape: the
// message's ID becomes the event id, its payload (a []byte or string; any
// other type is ErrUnsupportedPayload) becomes the data, and an optional
// HeaderSSEEventName message header — or, if absent, the WithEventName
// construction-time default — becomes the event name.
//
// SSEParser is the reverse direction: it reads a text/event-stream byte
// stream and yields one SSEEvent per call to Next, implementing the full
// WHATWG field-processing algorithm (a leading BOM, comment lines,
// multi-line data, the persistent last-event-id and retry buffers) with a
// per-event byte cap (WithMaxEventBytes) so an unterminated line or a
// pathologically large event cannot grow the parser's memory without bound.
// A future NewSSEClient in this package will build on SSEParser to turn a
// subscribed stream into a channel of messages; today SSEParser is directly
// usable by any caller reading an SSE response body, exactly as this
// package's own example does.
//
// # Serving SSE
//
// NewSSEServer builds an http.Handler that registers each GET request as a
// long-lived streaming connection and, via Send (which also makes SSEServer a
// msgin.OutboundAdapter), fans a message out to every registered connection as
// one SSE event. See NewSSEServer's own doc comment for its full delivery,
// topology, and heartbeat contract; this section covers what changes in the
// "Deploying these handlers safely" checklist (adapter/http/stdlib's package
// doc) once an SSEServer is one of the routes being served.
//
// Give the route (or the whole server, if nothing else shares it)
// WriteTimeout: 0:
//
//	srv := &http.Server{
//		Addr:              ":8080",
//		Handler:           mux,
//		ReadHeaderTimeout: 5 * time.Second,  // unchanged — still the slow-loris defense
//		IdleTimeout:       120 * time.Second, // unchanged
//		WriteTimeout:      0,                 // NOT 30s here — an SSE stream would die at it
//	}
//
// net/http's http.Server.WriteTimeout bounds the WHOLE response, and an SSE
// response is, by design, a single write that never completes until the
// client disconnects — the stdlib package's ordinary WriteTimeout: 30s
// recommendation would silently cut every SSE stream off at 30 seconds.
// ReadHeaderTimeout and IdleTimeout are unaffected by SSE and should still be
// set exactly as documented there.
//
// Setting WriteTimeout to 0 does NOT give up stalled-writer protection: it
// moves to a different, complementary layer. NewSSEServer applies its OWN
// per-write deadline (WithWriteTimeout, default 30s) via
// http.ResponseController.SetWriteDeadline before every Write/Flush. The two
// timeouts are not redundant — http.Server.WriteTimeout: 0 is what lets a
// HEALTHY stream stay open past any fixed duration, while WithWriteTimeout is
// what aborts an individual write that wedges on a stalled reader's full TCP
// window, which is in turn what lets Close and WithSlowClientPolicy's
// disconnect ever join a stuck connection's writer goroutine. Getting either
// one wrong fails differently: a nonzero http.Server.WriteTimeout kills every
// healthy SSE stream at that duration regardless of client behavior, while an
// absent (or absurdly large) per-write deadline lets one stalled reader wedge
// its writer — and anything waiting on Close — indefinitely.
//
// WARNING — heartbeat vs. proxies: an idle SSE connection sitting behind a
// reverse proxy or load balancer with its own 30-60s idle timeout is SILENTLY
// DROPPED unless WithHeartbeat is enabled, with an interval comfortably below
// the shortest idle timeout anywhere in the path. Most production deployments
// sit behind such a proxy. Heartbeat defaults OFF because the library cannot
// know your network topology; enabling it is on you.
//
// MULTI-INSTANCE NOTE (CLAUDE.md "Production robustness — multi-instance
// awareness"): an SSEServer's registered connections, and its optional replay
// ring, are PER-PROCESS in-memory state — it is the LAST hop from one process
// to its own directly-connected clients, not a message bus. In a
// horizontally-scaled deployment (N instances behind a load balancer), a Send
// on instance B never reaches a subscriber connected to instance A on its
// own; cross-instance fan-out needs a shared pub/sub backbone (Redis, NATS,
// …) feeding every instance, each of which then re-fans-out to its own local
// subscribers via Send. WithMaxConnections is likewise a PROCESS-GLOBAL cap
// on one instance's own connections, not a cluster-wide or per-IP one —
// per-IP/per-tenant limiting is a reverse-proxy/load-balancer concern, not
// something this package attempts.
//
// # Phase
//
// Phase 1 (this package plus adapter/http/stdlib) ships the two inbound server
// modes: I1 async inbound (ServeAsync) and I2 sync request-reply gateway
// (ServeGateway, over any msgin.RequestReplyExchange). Phase 2 (this package)
// ships the two outbound modes: O1 NewOutbound (webhook) and O2 NewExchange
// (client request-reply). This file's siblings hold the shared Config/Option/
// WithX surface, DecodeRequest/EncodeResponse (inbound) and EncodeRequest/
// ClassifyResponse (outbound), the inbound handler cores, the SSE framing core
// (SSEEvent/EncodeSSEEvent/SSEEventFromMessage/SSEParser), and NewSSEServer —
// see "Server-Sent Events (SSE)" above for both.
package msghttp
