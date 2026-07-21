// Package msghttp is the framework-agnostic core of the msgin HTTP channel
// adapter (adapter/http): request<->msgin.Message[any] conversion
// (DecodeRequest/EncodeResponse) plus the shared Config/Option surface both
// the net/http binding (adapter/http/stdlib) and the future gin binding
// (adapter/http/gin, Phase 5) build their handlers on. The package is named
// msghttp, not http, so a consumer's import block never collides with
// net/http.
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
//     named WithTrustedCorrelationID opt-in; WithCorrelationID merely records
//     the client's id for the flow to read.
//   - EncodeResponse never lets the client choose the response media type: it
//     always sends X-Content-Type-Options: nosniff and a Content-Type taken
//     from the FLOW's msgin.HeaderContentType, defaulting to
//     application/octet-stream so net/http never sniffs one out of the body.
//     Response header values are CRLF-sanitized.
//   - The handler cores recover a panicking flow handler and answer 500 rather
//     than letting it escape into the server.
//
// What this package does NOT do: authentication, authorization, CSRF, CORS,
// rate limiting or method filtering. See the adapter/http/stdlib package
// godoc for the deployment checklist (including the http.Server timeouts the
// caller must set).
//
// # Phase
//
// Phase 1 (this package plus adapter/http/stdlib) ships the two inbound
// server modes: I1 async inbound (ServeAsync) and I2 sync request-reply
// gateway (ServeGateway, over any msgin.RequestReplyExchange). This file's
// siblings hold the Config/Option/WithX surface and DecodeRequest/
// EncodeResponse both handler cores share, plus the handler cores
// themselves. Outbound (webhook + client request-reply) and SSE are later
// phases still (Spec 011 §6).
package msghttp
