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
