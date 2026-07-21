package msghttp

import (
	"net/http"

	"github.com/kartaladev/msgin"
)

// statusFor resolves err to an HTTP status: cfg's WithErrorStatus override
// when set, otherwise the package's defaultErrorStatus. Both inbound handler
// cores (ServeAsync here; ServeGateway in a later increment) share this
// helper so the cfg.errorStatus != nil branch is implemented — and covered by
// a test — exactly once (Plan 020 audit M2) rather than duplicated per
// handler.
func statusFor(cfg *Config, err error) int {
	if cfg.errorStatus != nil {
		return cfg.errorStatus(err)
	}
	return defaultErrorStatus(err)
}

// ServeAsync is the I1 async inbound handler core: it decodes r into a
// message via DecodeRequest and sends it to target, replying with cfg's
// WithSuccessStatus (default http.StatusAccepted, 202) on success — "accepted
// for asynchronous processing" is the honest status for a fire-and-forget
// handoff that has been queued, not yet processed.
//
// A DecodeRequest failure (e.g. an oversize body) or a target.Send failure is
// mapped to an HTTP status via statusFor — cfg's WithErrorStatus override
// when set, otherwise the package's defaultErrorStatus — and logged through
// cfg's WithLogger; nothing beyond that status line is written to w on
// either failure path.
//
// Delivery is at-least-once ONLY if the caller (the HTTP client) retries on a
// 5xx response: ServeAsync itself calls target.Send exactly once per request
// and does not retry.
//
// ServeAsync does not restrict r's HTTP method — a GET reaches the same
// decode+send path as a POST. Method filtering is the caller's mux/router
// concern, not this handler core's.
//
// target and cfg are assumed non-nil: ServeAsync is a framework-neutral
// handler core meant to be wrapped by a constructor (e.g.
// adapter/http/stdlib.NewInbound) that validates both before ever building an
// http.Handler around this function, so it does not re-validate them here.
func ServeAsync(w http.ResponseWriter, r *http.Request, target msgin.MessageChannel, cfg *Config) {
	msg, err := DecodeRequest(r, cfg)
	if err != nil {
		status := statusFor(cfg, err)
		cfg.logger.Warn("msghttp: decode request failed", "status", status, "error", err)
		w.WriteHeader(status)
		return
	}

	if err := target.Send(r.Context(), msg); err != nil {
		status := statusFor(cfg, err)
		cfg.logger.Error("msghttp: send to target failed", "status", status, "error", err)
		w.WriteHeader(status)
		return
	}

	w.WriteHeader(cfg.successStatus)
}
