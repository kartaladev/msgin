package msghttp

import (
	"errors"
	"net/http"
	"runtime/debug"

	"github.com/kartaladev/msgin"
)

// responseTracker wraps the http.ResponseWriter both handler cores write
// through, recording whether the response has been COMMITTED (a status line
// went out, or bytes were written). Two invariants depend on it:
//
//   - The panic recovery below may only synthesize a 500 when nothing has been
//     written yet; a panic after the reply's 200 is already on the wire can
//     only be logged.
//   - A second WriteHeader is dropped rather than passed through to net/http,
//     which would log "superfluous response.WriteHeader call" (audit F2).
//
// It deliberately does not forward http.Flusher/http.Hijacker: neither inbound
// mode streams. The SSE server mode (Spec 011 Phase 3) will need a
// flush-capable variant.
type responseTracker struct {
	http.ResponseWriter
	committed bool
}

func (t *responseTracker) WriteHeader(code int) {
	if t.committed {
		return
	}
	t.committed = true
	t.ResponseWriter.WriteHeader(code)
}

func (t *responseTracker) Write(b []byte) (int, error) {
	t.committed = true
	return t.ResponseWriter.Write(b)
}

// recoverHandler is the deferred fault-isolation boundary shared by both
// handler cores: a panic raised anywhere downstream (a flow handler invoked
// synchronously by a DirectChannel send, an exchange, a custom option
// callback) is recovered, logged with its stack, and answered with a plain 500
// when the response has not been committed yet.
//
// NOTE: this is load-bearing beyond ordinary robustness. msgin's
// ChannelExchange.Exchange calls its giveUp cleanup NON-deferred, so a panic
// unwinding through it leaves the reply waiter registered forever — a leaked
// map entry plus channel per panicking request, and a 409
// msgin.ErrDuplicateCorrelation for any later request reusing that correlation
// key. Recovering here keeps the panic inside a single request. Making the
// core's giveUp panic-safe is tracked as its own increment; until it lands,
// do not remove this recover.
func recoverHandler(w *responseTracker, cfg *Config, op string) {
	r := recover()
	if r == nil {
		return
	}

	cfg.log().Error("msghttp: handler panicked",
		"op", op,
		"panic", r,
		"stack", string(debug.Stack()),
	)
	w.WriteHeader(http.StatusInternalServerError)
}

// ServeAsync is the I1 async inbound handler core: it decodes r into a
// message via DecodeRequest and sends it to target, replying with cfg's
// WithSuccessStatus (default http.StatusAccepted, 202) on success — "accepted
// for asynchronous processing" is the honest status for a fire-and-forget
// handoff that has been queued, not yet processed.
//
// A DecodeRequest failure (e.g. an oversize body) or a target.Send failure is
// mapped to an HTTP status via cfg's WithErrorStatus override when set,
// otherwise DefaultErrorStatus — and logged through cfg's WithLogger; nothing
// beyond that status line is written to w on either failure path.
//
// A PANIC from the flow (target.Send runs a DirectChannel subscriber on this
// goroutine) is recovered and answered with a plain 500 — the panic never
// escapes the request (CLAUDE.md "Fault isolation & recovery"); see
// recoverHandler.
//
// Delivery is at-least-once ONLY if the caller (the HTTP client) retries on a
// 5xx response: ServeAsync itself calls target.Send exactly once per request
// and does not retry.
//
// ServeAsync does not restrict r's HTTP method — a GET reaches the same
// decode+send path as a POST. Method filtering is the caller's mux/router
// concern, not this handler core's. It applies NO authentication, NO
// authorization, NO CSRF and NO CORS defense either; see the
// adapter/http/stdlib package godoc.
//
// target is assumed non-nil: ServeAsync is a framework-neutral handler core
// meant to be wrapped by a constructor (e.g. adapter/http/stdlib.NewInbound)
// that validates it before ever building an http.Handler around this function.
// A nil or hand-built cfg IS tolerated: every setting falls back to its
// documented default (see Config).
func ServeAsync(w http.ResponseWriter, r *http.Request, target msgin.MessageChannel, cfg *Config) {
	tracked := &responseTracker{ResponseWriter: w}
	defer recoverHandler(tracked, cfg, "ServeAsync")

	msg, err := DecodeRequest(r, cfg)
	if err != nil {
		status := cfg.errStatus()(err)
		cfg.log().Warn("msghttp: decode request failed", "status", status, "error", err)
		tracked.WriteHeader(status)
		return
	}

	if err := target.Send(r.Context(), msg); err != nil {
		status := cfg.errStatus()(err)
		cfg.log().Error("msghttp: send to target failed", "status", status, "error", err)
		tracked.WriteHeader(status)
		return
	}

	tracked.WriteHeader(cfg.successStatusOrDefault())
}

// ServeGateway is the I2 sync inbound gateway handler core: it decodes r into
// a message via DecodeRequest, runs it through exchange's synchronous
// request/reply round-trip (msgin.RequestReplyExchange.Exchange), and encodes
// the correlated reply as the HTTP response via EncodeResponse — which always
// writes http.StatusOK (200) on success. cfg's WithSuccessStatus does NOT
// apply here: it governs ServeAsync only (see its godoc), because a gateway
// response IS the synchronous reply body, not an async 202 handoff.
//
// A DecodeRequest failure is mapped to a status exactly as in ServeAsync (a
// *http.MaxBytesError maps to 413, any other read/decode failure to 400);
// exchange.Exchange never runs on that path. An exchange.Exchange failure is
// mapped the same way — DefaultErrorStatus gives each gateway sentinel an
// honest status: msgin.ErrReplyTimeout -> 504 (no reply arrived in time),
// msgin.ErrGatewayClosed -> 503 (the exchange is shutting down),
// msgin.ErrDuplicateCorrelation -> 409 (only reachable when a caller opts into
// trusting client correlation ids via WithTrustedCorrelationID and a client
// reuses one), msgin.ErrNoCorrelation -> 500 (a wiring fault: DecodeRequest
// always stamps a non-empty correlation id, so this can only mean a caller of
// ServeGateway violated that invariant), and any other error -> 500. cfg's
// WithErrorStatus overrides this mapping for both failure sources.
//
// An EncodeResponse failure splits into two arms, because only one of them
// leaves w untouched:
//
//   - Nothing written yet (ErrUnsupportedPayload — a reply payload that is
//     neither []byte nor string): the mapped status (500 by default) is
//     written as a clean, single response, leaking no flow header or body.
//   - Already committed (ErrWriteResponse — the 200 went out and the body
//     write then failed, typically a client that hung up): the failure is
//     LOGGED ONLY. No second status is written; there is no way to signal the
//     failure to a client that is already gone.
//
// A PANIC raised by the flow — msgin.ChannelExchange runs a DirectChannel
// subscriber on this goroutine — is recovered and answered with a plain 500
// when the response is not yet committed. That recover is load-bearing: the
// core's reply-waiter cleanup is not panic-safe, so letting a panic escape
// would leak a correlator slot per request; see recoverHandler.
//
// ServeGateway does not restrict r's HTTP method, and applies NO
// authentication, NO authorization, NO CSRF and NO CORS defense; see the
// adapter/http/stdlib package godoc.
//
// exchange is assumed non-nil: ServeGateway is a framework-neutral handler
// core meant to be wrapped by a constructor (e.g.
// adapter/http/stdlib.NewInboundGateway) that validates it before ever
// building an http.Handler around this function. A nil or hand-built cfg IS
// tolerated: every setting falls back to its documented default (see Config).
func ServeGateway(w http.ResponseWriter, r *http.Request, exchange msgin.RequestReplyExchange, cfg *Config) {
	tracked := &responseTracker{ResponseWriter: w}
	defer recoverHandler(tracked, cfg, "ServeGateway")

	msg, err := DecodeRequest(r, cfg)
	if err != nil {
		status := cfg.errStatus()(err)
		cfg.log().Warn("msghttp: decode request failed", "status", status, "error", err)
		tracked.WriteHeader(status)
		return
	}

	reply, err := exchange.Exchange(r.Context(), msg)
	if err != nil {
		status := cfg.errStatus()(err)
		cfg.log().Error("msghttp: exchange failed", "status", status, "error", err)
		tracked.WriteHeader(status)
		return
	}

	if err := EncodeResponse(tracked, reply, cfg); err != nil {
		if errors.Is(err, ErrWriteResponse) {
			// The 200 status line and possibly part of the body are already on
			// the wire: a second WriteHeader would be a protocol error (and
			// net/http's "superfluous response.WriteHeader call"). Log only.
			cfg.log().Error("msghttp: write response body failed", "error", err)
			return
		}
		status := cfg.errStatus()(err)
		cfg.log().Error("msghttp: encode response failed", "status", status, "error", err)
		tracked.WriteHeader(status)
	}
}
