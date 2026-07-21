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
// robustness"): DecodeRequest caps the body (WithMaxBodyBytes, default 1
// MiB), copies request headers onto the decoded message only from an
// explicit allow-list (WithRequestHeaders — empty by default, so nothing is
// copied unless opted in), and defensively strips any copied header whose
// name carries the reserved "msgin." prefix so a client can never forge a
// core header (e.g. msgin.delivery-count) even if an operator's allow-list
// names one by mistake. The correlation id defaults to the message's own
// server-minted ID() — a client-supplied value is used only if the caller
// opts in via WithCorrelationID, and even then an empty resolved value falls
// back to the server-minted id rather than an empty correlation key.
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
