// Package stdlib is the net/http binding for the msgin HTTP channel adapter:
// a thin peer of the framework-agnostic core (adapter/http, package msghttp)
// that composes msghttp's handler cores (ServeAsync via NewInbound,
// ServeGateway via NewInboundGateway) into ordinary http.Handlers. All
// decode/encode/status-mapping logic lives in msghttp on the
// framework-neutral (http.ResponseWriter, *http.Request) pair; this
// package's constructors do nothing more than validate their arguments and
// close over the appropriate msghttp core — the thinness is deliberate (ADR
// 0023 §1), so a future gin binding (adapter/http/gin, Phase 5) reuses
// msghttp with no logic duplication.
//
// # Deploying these handlers safely
//
// An inbound HTTP endpoint is an UNTRUSTED boundary, and these constructors
// return a bare http.Handler: everything below is the CALLER's responsibility,
// because a library handler cannot make the decision for you.
//
// Set the http.Server timeouts. Go's zero-value http.Server has NO read,
// write or idle timeout, so a handful of idle or slow-loris connections can
// pin server resources indefinitely. Always construct the server explicitly:
//
//	srv := &http.Server{
//		Addr:              ":8080",
//		Handler:           mux,
//		ReadHeaderTimeout: 5 * time.Second,  // slow-loris defense
//		ReadTimeout:       30 * time.Second, // whole request, incl. body
//		WriteTimeout:      30 * time.Second,
//		IdleTimeout:       120 * time.Second,
//		MaxHeaderBytes:    1 << 20,
//	}
//
// ReadHeaderTimeout in particular is the one with no safe default anywhere in
// the stack. MaxHeaderBytes matters too: msghttp.WithMaxBodyBytes bounds only
// the request BODY, while the request line and allow-listed headers copied
// onto each message are bounded by MaxHeaderBytes (and on the async path that
// memory lives as long as the queued message).
//
// These handlers apply NO authentication, NO authorization, NO CSRF defense
// and NO CORS policy, and (like msghttp's cores) they do not filter the
// request METHOD — a GET reaches the same decode path as a POST. In
// particular, a browser on any origin can reach a mounted endpoint with a
// simple cross-origin POST (text/plain, form or multipart content types need
// no preflight), so an unauthenticated endpoint is world-writable and an
// endpoint authenticated by cookie alone is CSRF-vulnerable. Mount these
// handlers behind your own middleware for authn/authz, CSRF tokens (or
// SameSite cookies), CORS, rate limiting and method filtering — e.g. a Go
// 1.22+ method-prefixed pattern, http.ServeMux.HandleFunc("POST /path", h).
//
// Request headers are copied onto the message only from an explicit
// allow-list, response headers only from another; the correlation key is
// server-minted unless you opt into msghttp.WithTrustedCorrelationID (read its
// warning first). See the msghttp package godoc for the full boundary
// contract.
package stdlib
