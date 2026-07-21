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
package stdlib
