package msghttp

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/kartaladev/msgin"
)

// Non-reserved metadata keys DecodeRequest adds to every decoded message, for
// observability (e.g. a router keying off the original HTTP method/path) and
// for the two values that must NEVER be trusted as core metadata: the client's
// Content-Type and its advertised correlation id.
// They deliberately do NOT carry the reserved "msgin." prefix, so the
// defensive reserved-header strip (below) never touches them AND they can
// never become the source of a response Content-Type or of the exchange
// correlation key.
const (
	headerHTTPMethod        = "http.method"
	headerHTTPPath          = "http.path"
	headerHTTPQuery         = "http.query"
	headerHTTPContentType   = "http.content-type"
	headerHTTPCorrelationID = "http.correlation-id"
)

// reservedHeaderPrefix is msgin's reserved header namespace: every core
// header constant (msgin.HeaderID, msgin.HeaderTimestamp,
// msgin.HeaderContentType, msgin.HeaderCorrelationID,
// msgin.HeaderDeliveryCount, msgin.HeaderSequenceNumber/Size, and any future
// one) starts with it. DecodeRequest strips any allow-listed request header
// whose name carries this prefix — compared case-INSENSITIVELY — so a client
// can never forge a core header.
const reservedHeaderPrefix = "msgin."

// defaultResponseContentType is the response Content-Type EncodeResponse
// writes when the reply message carries no msgin.HeaderContentType. It is set
// unconditionally (never left empty) so net/http's DetectContentType never
// sniffs a media type out of a flow-controlled response body — a body that
// happens to start with "<html" or "<script" would otherwise be served as
// text/html and execute in a browser. A flow that genuinely wants a different
// media type sets msgin.HeaderContentType on the reply itself.
const defaultResponseContentType = "application/octet-stream"

// DecodeRequest reads and caps r's body (cfg's WithMaxBodyBytes, default 1
// MiB) and converts the request into a msgin.Message[any]: the body becomes
// the payload ([]byte) and allow-listed request headers (cfg's
// WithRequestHeaders) are copied onto the message's headers. Any copied key
// carrying the reserved "msgin." prefix (case-insensitively) is defensively
// stripped BEFORE the message is built, so a client can never forge a core
// header (e.g. msgin.delivery-count) even via a misconfigured allow-list
// entry. Only the FIRST value of a multi-valued request header is copied
// (http.Header.Get semantics); the allow-list itself is matched
// case-INSENSITIVELY, again per http.Header.Get.
//
// Four non-reserved metadata headers are added for observability and for
// values that must not be trusted as core metadata:
//
//   - http.method, http.path, http.query — the request line. NOTE these are
//     NOT bounded by WithMaxBodyBytes: their ceiling is the server's
//     http.Server.MaxHeaderBytes (1 MiB by default), and on the async path
//     (ServeAsync) that memory is retained for the queued message's lifetime.
//     Lower MaxHeaderBytes if that matters.
//   - http.content-type — the client's Content-Type verbatim. It is
//     deliberately NOT written to msgin.HeaderContentType: that reserved
//     header is what EncodeResponse trusts as the response media type, and a
//     client must never be able to choose the media type its own bytes are
//     served back under (reflected-XSS vector).
//   - http.correlation-id — the client's advertised correlation id, when
//     cfg's WithAdvisoryCorrelationID resolves a non-empty one. Advisory
//     only: it never becomes the exchange correlation key.
//
// The message is built with msgin.New — never msgin.NewMessage — because a
// decoded HTTP request is freshly-produced input entering the system, not a
// message reconstructed from a persisted/previously-framed one; New always
// stamps a fresh msgin.id and msgin.timestamp.
//
// The exchange correlation key (msgin.HeaderCorrelationID) is ALWAYS the
// message's own server-minted ID() unless the caller explicitly opts into
// client-keyed correlation via WithTrustedCorrelationID — see that option's
// warning before using it.
//
// A body read/decode failure (including an oversize body) returns an error
// wrapping ErrDecodeRequest and a zero-value Message; the caller maps it to an
// HTTP status via DefaultErrorStatus or a custom cfg.WithErrorStatus mapper —
// an oversize body (*http.MaxBytesError) maps to 413, any other read/decode
// error to 400.
//
// A nil cfg, or one built by hand rather than by NewConfig, is tolerated:
// every setting falls back to its documented default (see Config).
func DecodeRequest(r *http.Request, cfg *Config) (msgin.Message[any], error) {
	// r.Body is never nil for a request delivered by a net/http server, but
	// DecodeRequest is exported and a caller (or a non-stdlib binding) may
	// hand over a hand-built *http.Request, where a nil Body is legal.
	body := r.Body
	if body == nil {
		body = http.NoBody
	}

	raw, err := io.ReadAll(http.MaxBytesReader(nil, body, cfg.maxBody()))
	if err != nil {
		return msgin.Message[any]{}, fmt.Errorf("%w: %w", ErrDecodeRequest, err)
	}

	allowed := cfg.allowedRequestHeaders()
	headers := make(map[string]any, len(allowed)+5)
	for _, name := range allowed {
		if v := r.Header.Get(name); v != "" {
			headers[name] = v
		}
	}
	for k := range headers {
		if strings.HasPrefix(strings.ToLower(k), reservedHeaderPrefix) {
			delete(headers, k)
		}
	}

	if ct := r.Header.Get("Content-Type"); ct != "" {
		headers[headerHTTPContentType] = ct
	}
	headers[headerHTTPMethod] = r.Method
	headers[headerHTTPPath] = r.URL.Path
	headers[headerHTTPQuery] = r.URL.RawQuery

	if resolve := cfg.advisoryCorrelationID(); resolve != nil {
		if v := resolve(r); v != "" {
			headers[headerHTTPCorrelationID] = v
		}
	}

	msg := msgin.New[any](raw, msgin.WithHeaders(headers))

	cid := msg.ID()
	if resolve := cfg.trustedCorrelationID(); resolve != nil {
		if v := resolve(r); v != "" {
			cid = v
		}
	}

	return msg.WithHeader(msgin.HeaderCorrelationID, cid), nil
}

// EncodeResponse writes msg as the HTTP response on w. The payload is
// extracted to bytes BEFORE anything is written to w: a []byte payload is
// used directly, a string payload is converted with []byte(s), and any other
// payload type returns ErrUnsupportedPayload WITHOUT writing any header or
// status to w — extracting first means a caller reacting to the error
// (typically by writing its own 500) is guaranteed an untouched
// ResponseWriter, never a half-written response carrying the flow's headers
// under the caller's own error status.
//
// On successful extraction:
//
//   - Allow-listed response headers (cfg's WithResponseHeaders) are emitted
//     with their values CRLF-sanitized (\r and \n stripped, defending against
//     HTTP response splitting from a header value sourced from message
//     metadata). An allow-listed name whose header value is not a string is
//     silently skipped (a header value formatting policy is the caller's
//     concern, not this package's to guess).
//   - Content-Type is set from msgin.HeaderContentType when it is present and
//     non-empty, otherwise to application/octet-stream — never left unset, so
//     net/http never sniffs a media type out of the body.
//   - X-Content-Type-Options: nosniff is ALWAYS set, after the allow-list, so
//     an allow-listed message header can never weaken it.
//   - The status is ALWAYS http.StatusOK (200) — a request-reply response body
//     is a synchronous reply, never the async inbound handler's 202. cfg's
//     WithSuccessStatus does NOT apply here; see its godoc.
//
// A failure of the body write (the only failure possible once the 200 has gone
// out) is returned wrapping ErrWriteResponse: the response is already
// committed, so the caller must log it and must NOT write a second status.
//
// A nil cfg, or one built by hand rather than by NewConfig, is tolerated.
func EncodeResponse(w http.ResponseWriter, msg msgin.Message[any], cfg *Config) error {
	var body []byte
	switch payload := msg.Payload().(type) {
	case []byte:
		body = payload
	case string:
		body = []byte(payload)
	default:
		return ErrUnsupportedPayload
	}

	for _, name := range cfg.allowedResponseHeaders() {
		if v, ok := msg.Headers().String(name); ok {
			w.Header().Set(name, sanitizeHeaderValue(v))
		}
	}

	ct, _ := msg.Headers().String(msgin.HeaderContentType)
	if ct = sanitizeHeaderValue(ct); ct == "" {
		ct = defaultResponseContentType
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("X-Content-Type-Options", "nosniff")

	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(body); err != nil {
		return fmt.Errorf("%w: %w", ErrWriteResponse, err)
	}
	return nil
}

// sanitizeHeaderValue strips \r and \n from v before it is written as an HTTP
// header value, defending against HTTP response splitting when v is sourced
// from message metadata (application-controlled, but not necessarily
// HTTP-header-safe) rather than a literal in this package.
func sanitizeHeaderValue(v string) string {
	v = strings.ReplaceAll(v, "\r", "")
	return strings.ReplaceAll(v, "\n", "")
}

// DefaultErrorStatus is the default error->HTTP-status mapping used by the
// inbound handler cores (ServeAsync/ServeGateway) when WithErrorStatus is
// unset. It is exported so a custom WithErrorStatus mapper can classify the
// cases it cares about and DELEGATE the rest here, instead of losing the
// 413-vs-400-vs-5xx discrimination it cannot reimplement from outside the
// package. It gives an HONEST status per failure cause (audit L2) rather than
// a blanket 500:
//
//   - msgin.ErrReplyTimeout -> 504 Gateway Timeout (no reply arrived in time).
//   - msgin.ErrGatewayClosed -> 503 Service Unavailable (shutting down).
//   - msgin.ErrDuplicateCorrelation -> 409 Conflict (only reachable when a
//     caller opts into trusting client correlation ids via
//     WithTrustedCorrelationID, and a client reuses one).
//   - msgin.ErrNoCorrelation, ErrUnsupportedPayload, ErrWriteResponse -> 500
//     Internal Server Error: all three are SERVER/WIRING or transport faults,
//     never the client's fault — this adapter always stamps a non-empty
//     correlation id, so a missing correlation key can only be a bug in this
//     package or its caller's wiring, not a malformed request.
//   - an oversize body (*http.MaxBytesError, via errors.As) -> 413 Request
//     Entity Too Large. Checked BEFORE ErrDecodeRequest, which also wraps it.
//   - any other DecodeRequest read/decode failure (ErrDecodeRequest) -> 400
//     Bad Request.
//   - anything else, including a nil error -> 500, the safe default for a
//     cause this package does not recognize.
func DefaultErrorStatus(err error) int {
	switch {
	case errors.Is(err, msgin.ErrReplyTimeout):
		return http.StatusGatewayTimeout
	case errors.Is(err, msgin.ErrGatewayClosed):
		return http.StatusServiceUnavailable
	case errors.Is(err, msgin.ErrDuplicateCorrelation):
		return http.StatusConflict
	case errors.Is(err, msgin.ErrNoCorrelation),
		errors.Is(err, ErrUnsupportedPayload),
		errors.Is(err, ErrWriteResponse):
		return http.StatusInternalServerError
	}

	var maxBytesErr *http.MaxBytesError
	if errors.As(err, &maxBytesErr) {
		return http.StatusRequestEntityTooLarge
	}

	if errors.Is(err, ErrDecodeRequest) {
		return http.StatusBadRequest
	}

	return http.StatusInternalServerError
}
