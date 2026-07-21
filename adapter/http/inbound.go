package msghttp

import (
	"net/http"

	"github.com/kartaladev/msgin"
)

// statusFor resolves err to an HTTP status: cfg's WithErrorStatus override
// when set, otherwise the package's defaultErrorStatus. Both inbound handler
// cores (ServeAsync and ServeGateway) share this helper so the
// cfg.errorStatus != nil branch is implemented — and covered by a test —
// exactly once (Plan 020 audit M2) rather than duplicated per handler.
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

// ServeGateway is the I2 sync inbound gateway handler core: it decodes r into
// a message via DecodeRequest, runs it through exchange's synchronous
// request/reply round-trip (msgin.RequestReplyExchange.Exchange), and encodes
// the correlated reply as the HTTP response via EncodeResponse — which always
// writes http.StatusOK (200) on success. cfg's WithSuccessStatus does NOT
// apply here: it governs ServeAsync only (see its godoc), because a gateway
// response IS the synchronous reply body, not an async 202 handoff.
//
// A DecodeRequest failure is mapped to a status via statusFor exactly as in
// ServeAsync (a *http.MaxBytesError maps to 413, any other read/decode
// failure to 400); exchange.Exchange never runs on that path. An
// exchange.Exchange failure is mapped via statusFor too — the package's
// defaultErrorStatus gives each gateway sentinel an honest status:
// msgin.ErrReplyTimeout -> 504 (no reply arrived in time),
// msgin.ErrGatewayClosed -> 503 (the exchange is shutting down),
// msgin.ErrDuplicateCorrelation -> 409 (only reachable when a caller opts
// into trusting client correlation ids via WithCorrelationID and a client
// reuses one concurrently), msgin.ErrNoCorrelation -> 500 (a wiring fault:
// DecodeRequest always stamps a non-empty correlation id, so this can only
// mean a caller of ServeGateway violated that invariant), and any other
// error -> 500. cfg's WithErrorStatus overrides this mapping for both
// failure sources.
//
// A reply whose payload is neither []byte nor string cannot be encoded:
// EncodeResponse returns ErrUnsupportedPayload WITHOUT writing anything to w
// in that case (see its godoc), so ServeGateway then writes
// statusFor(cfg, ErrUnsupportedPayload) (500 by default) as a clean, single
// response — never a half-written 200 followed by a second WriteHeader
// (Plan 020 audit L4). No flow header or body is leaked on this path.
//
// exchange and cfg are assumed non-nil: ServeGateway is a framework-neutral
// handler core meant to be wrapped by a constructor (e.g.
// adapter/http/stdlib.NewInboundGateway) that validates both before ever
// building an http.Handler around this function, so it does not re-validate
// them here.
func ServeGateway(w http.ResponseWriter, r *http.Request, exchange msgin.RequestReplyExchange, cfg *Config) {
	msg, err := DecodeRequest(r, cfg)
	if err != nil {
		status := statusFor(cfg, err)
		cfg.logger.Warn("msghttp: decode request failed", "status", status, "error", err)
		w.WriteHeader(status)
		return
	}

	reply, err := exchange.Exchange(r.Context(), msg)
	if err != nil {
		status := statusFor(cfg, err)
		cfg.logger.Error("msghttp: exchange failed", "status", status, "error", err)
		w.WriteHeader(status)
		return
	}

	if err := EncodeResponse(w, reply, cfg); err != nil {
		status := statusFor(cfg, err)
		cfg.logger.Error("msghttp: encode response failed", "status", status, "error", err)
		w.WriteHeader(status)
		return
	}
}
