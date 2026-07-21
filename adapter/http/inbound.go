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
// EXCEPTION: http.ErrAbortHandler is re-panicked rather than recovered, so
// net/http's own silent-abort contract for that sentinel still holds.
//
// NOTE: this is load-bearing beyond ordinary robustness, independent of what
// the exchange does with its own state. A flow handler is caller code
// (custom `msgin.HandlerFunc`s, `msgin.RequestReplyExchange` implementations,
// option callbacks) running synchronously on this goroutine, so its panic
// would otherwise take the whole server down with it. Recovering here keeps
// the panic inside a single request regardless of what runs downstream — do
// not remove this recover. (As of Spec 012 / ADR 0022 Addendum A,
// msgin.ChannelExchange.Exchange also reclaims its reply-waiter slot on a
// panic unwind, closing a former residual of this boundary; that is a
// property of the exchange, not a reason to drop the recover.)
func recoverHandler(w *responseTracker, cfg *Config, op string) {
	r := recover()
	if r == nil {
		return
	}
	// net/http's documented contract for this exact sentinel: abort the
	// connection SILENTLY, without logging a stack trace. Re-panic so net/
	// http's own top-level recover handles it, rather than turning a
	// deliberate silent abort into a full ERROR log + synthesized 500. No
	// msgin flow step raises this today; the guard is contract correctness
	// for a caller whose flow embeds something like httputil.ReverseProxy,
	// which does.
	if r == http.ErrAbortHandler {
		panic(r)
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
// when the response is not yet committed, so the panic never escapes the
// request and the server keeps serving subsequent ones.
//
// The recover contains the panic to this request and yields a clean 500
// instead of crashing the process — that fault isolation is required
// regardless of what the exchange does with its own state. As of Spec 012 /
// ADR 0022 Addendum A, msgin.ChannelExchange.Exchange also reclaims its
// reply-waiter slot on a panic unwind (RequestReplyExchange's godoc now
// states this as part of the SPI contract), closing a former residual of
// this boundary: a panicking flow used to leak one correlator-map entry per
// attempt regardless of whether the panic was recovered here; it no longer
// does. See recoverHandler.
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
