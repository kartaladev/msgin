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
// # Phase
//
// Phase 1 (this package plus adapter/http/stdlib) ships the two inbound server
// modes: I1 async inbound (ServeAsync) and I2 sync request-reply gateway
// (ServeGateway, over any msgin.RequestReplyExchange). Phase 2 (this package)
// ships the two outbound modes: O1 NewOutbound (webhook) and O2 NewExchange
// (client request-reply). This file's siblings hold the shared Config/Option/
// WithX surface, DecodeRequest/EncodeResponse (inbound) and EncodeRequest/
// ClassifyResponse (outbound), plus the inbound handler cores. SSE is a later
// phase still (Spec 011 §6).
package msghttp
