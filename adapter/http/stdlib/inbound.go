// Package stdlib's inbound.go composes msghttp's framework-neutral handler
// cores into ordinary net/http handlers.
package stdlib

import (
	"net/http"

	"github.com/kartaladev/msgin"
	msghttp "github.com/kartaladev/msgin/adapter/http"
)

// NewInbound builds the I1 async inbound http.Handler: it validates opts via
// msghttp.NewConfig and target, then returns an http.Handler that decodes
// each request into a message, sends it to target, and replies with cfg's
// WithSuccessStatus (default 202 Accepted) — see msghttp.ServeAsync for the
// full decode/send/status-mapping behavior this handler wraps.
//
// target MUST be non-nil: a nil target returns msghttp.ErrNilTarget rather
// than deferring the failure to the first request. An invalid opt (e.g.
// msghttp.WithMaxBodyBytes(0)) surfaces msghttp.NewConfig's typed
// construction error unchanged.
//
// The returned handler does NOT restrict the request's HTTP method — a GET
// reaches the same decode+send path as a POST/PUT. Mount it behind whatever
// method filtering the caller's mux/router applies (e.g.
// http.ServeMux.HandleFunc("POST /path", ...) on Go 1.22+, or Register plus
// a method check in the caller's own middleware); NewInbound itself has no
// opinion on method.
func NewInbound(target msgin.MessageChannel, opts ...msghttp.Option) (http.Handler, error) {
	cfg, err := msghttp.NewConfig(opts...)
	if err != nil {
		return nil, err
	}
	if target == nil {
		return nil, msghttp.ErrNilTarget
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		msghttp.ServeAsync(w, r, target, cfg)
	}), nil
}

// NewInboundGateway builds the I2 sync inbound gateway http.Handler: it
// validates opts via msghttp.NewConfig and exchange, then returns an
// http.Handler that decodes each request into a message, runs it through
// exchange's synchronous request/reply round-trip, and encodes the
// correlated reply as the response — see msghttp.ServeGateway for the full
// decode/exchange/status-mapping behavior this handler wraps. The response is
// ALWAYS http.StatusOK (200) on success; cfg's WithSuccessStatus does NOT
// apply here — that option governs the async I1 handler (NewInbound) only.
//
// On failure the response status is: 413 (oversize body) / 400 (any other
// decode fault) from a DecodeRequest failure; 504 (msgin.ErrReplyTimeout) /
// 503 (msgin.ErrGatewayClosed) / 409 (msgin.ErrDuplicateCorrelation) / 500
// (msgin.ErrNoCorrelation, msghttp.ErrUnsupportedPayload, or any other
// error) from an exchange.Exchange or encode failure — cfg's
// msghttp.WithErrorStatus overrides this mapping.
//
// exchange MUST be non-nil: a nil exchange returns msgin.ErrNilExchange
// rather than deferring the failure to the first request. An invalid opt
// (e.g. msghttp.WithMaxBodyBytes(0)) surfaces msghttp.NewConfig's typed
// construction error unchanged.
//
// The returned handler does NOT restrict the request's HTTP method — a GET
// reaches the same decode+exchange path as a POST/PUT. Mount it behind
// whatever method filtering the caller's mux/router applies (e.g.
// http.ServeMux.HandleFunc("POST /path", ...) on Go 1.22+, or Register plus
// a method check in the caller's own middleware); NewInboundGateway itself
// has no opinion on method.
func NewInboundGateway(exchange msgin.RequestReplyExchange, opts ...msghttp.Option) (http.Handler, error) {
	cfg, err := msghttp.NewConfig(opts...)
	if err != nil {
		return nil, err
	}
	if exchange == nil {
		return nil, msgin.ErrNilExchange
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		msghttp.ServeGateway(w, r, exchange, cfg)
	}), nil
}

// Register mounts h on mux at pattern via http.ServeMux.Handle — a thin,
// framework-neutral helper so a caller wiring either the I1 (NewInbound) or
// I2 (NewInboundGateway) handler does not need to remember ServeMux's
// registration method name. It does not restrict the request method itself;
// combine pattern with a Go 1.22+ method-prefixed pattern (e.g. "POST /path")
// if the caller wants method filtering at the mux level.
func Register(mux *http.ServeMux, pattern string, h http.Handler) {
	mux.Handle(pattern, h)
}
