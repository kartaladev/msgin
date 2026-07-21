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
// observability (e.g. a router keying off the original HTTP method/path).
// They deliberately do NOT carry the reserved "msgin." prefix, so the
// defensive reserved-header strip (below) never touches them.
const (
	headerHTTPMethod = "http.method"
	headerHTTPPath   = "http.path"
	headerHTTPQuery  = "http.query"
)

// reservedHeaderPrefix is msgin's reserved header namespace: every core
// header constant (msgin.HeaderID, msgin.HeaderTimestamp,
// msgin.HeaderContentType, msgin.HeaderCorrelationID,
// msgin.HeaderDeliveryCount, msgin.HeaderSequenceNumber/Size, and any future
// one) starts with it. DecodeRequest strips any allow-listed request header
// whose name carries this prefix so a client can never forge a core header.
const reservedHeaderPrefix = "msgin."

// decodeError wraps a request-body read/decode failure returned by
// DecodeRequest. It exists so defaultErrorStatus can distinguish a
// non-oversize read/decode fault (400) from an unclassified downstream error
// (500) without over-claiming meaning for an arbitrary error it did not
// originate — the wrap preserves the underlying cause (a plain read error, or
// *http.MaxBytesError on overflow) via Unwrap, so errors.As/errors.Is still
// see through it.
type decodeError struct {
	err error
}

func (e *decodeError) Error() string {
	return fmt.Sprintf("msghttp: decode request: %v", e.err)
}

func (e *decodeError) Unwrap() error { return e.err }

// DecodeRequest reads and caps r's body (cfg's WithMaxBodyBytes, default 1
// MiB) and converts the request into a msgin.Message[any]: the body becomes
// the payload ([]byte); allow-listed request headers (cfg's
// WithRequestHeaders) and the Content-Type are copied onto the message's
// headers (Content-Type under msgin.HeaderContentType); http.method/
// http.path/http.query are added for observability. Any copied key carrying
// the reserved "msgin." prefix is defensively stripped BEFORE the message is
// built, so a client can never forge a core header (e.g.
// msgin.delivery-count) even via a misconfigured allow-list entry.
//
// The message is built with msgin.New — never msgin.NewMessage — because a
// decoded HTTP request is freshly-produced input entering the system, not a
// message reconstructed from a persisted/previously-framed one; New always
// stamps a fresh msgin.id and msgin.timestamp.
//
// The correlation id (msgin.HeaderCorrelationID) defaults to the message's
// own server-minted ID(<>) unless cfg's WithCorrelationID resolves a
// non-empty override from r — DecodeRequest never trusts a client-supplied
// correlation value by default (CLAUDE.md untrusted-input boundary).
//
// A body read/decode failure (including an oversize body) returns a non-nil
// error and a zero-value Message; the caller maps it to an HTTP status via
// defaultErrorStatus or a custom cfg.WithErrorStatus mapper — an oversize
// body (*http.MaxBytesError) maps to 413, any other read/decode error to 400.
func DecodeRequest(r *http.Request, cfg *Config) (msgin.Message[any], error) {
	body, err := io.ReadAll(http.MaxBytesReader(nil, r.Body, cfg.maxBodyBytes))
	if err != nil {
		return msgin.Message[any]{}, &decodeError{err: err}
	}

	headers := make(map[string]any, len(cfg.requestHeaders)+4)
	for _, name := range cfg.requestHeaders {
		if v := r.Header.Get(name); v != "" {
			headers[name] = v
		}
	}
	for k := range headers {
		if strings.HasPrefix(k, reservedHeaderPrefix) {
			delete(headers, k)
		}
	}

	if ct := r.Header.Get("Content-Type"); ct != "" {
		headers[msgin.HeaderContentType] = ct
	}
	headers[headerHTTPMethod] = r.Method
	headers[headerHTTPPath] = r.URL.Path
	headers[headerHTTPQuery] = r.URL.RawQuery

	msg := msgin.New[any](body, msgin.WithHeaders(headers))

	cid := ""
	if cfg.correlationID != nil {
		cid = cfg.correlationID(r)
	}
	if cid == "" {
		cid = msg.ID()
	}
	msg = msg.WithHeader(msgin.HeaderCorrelationID, cid)

	return msg, nil
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
// On successful extraction: allow-listed response headers (cfg's
// WithResponseHeaders) are emitted with their values CRLF-sanitized (\r and
// \n stripped, defending against HTTP response splitting from a header value
// sourced from message metadata) — an allow-listed name whose header value is
// not a string is silently skipped (a header value formatting policy is the
// caller's concern, not this package's to guess); Content-Type is set from
// msgin.HeaderContentType when present; the status is ALWAYS http.StatusOK
// (200) — a request-reply response body is a synchronous reply, never the
// async inbound handler's 202. cfg's WithSuccessStatus does NOT apply here;
// see its godoc.
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

	for _, name := range cfg.responseHeaders {
		if v, ok := msg.Headers().String(name); ok {
			w.Header().Set(name, sanitizeHeaderValue(v))
		}
	}
	if ct, ok := msg.Headers().String(msgin.HeaderContentType); ok {
		w.Header().Set("Content-Type", sanitizeHeaderValue(ct))
	}

	w.WriteHeader(http.StatusOK)
	_, err := w.Write(body)
	return err
}

// sanitizeHeaderValue strips \r and \n from v before it is written as an HTTP
// header value, defending against HTTP response splitting when v is sourced
// from message metadata (application-controlled, but not necessarily
// HTTP-header-safe) rather than a literal in this package.
func sanitizeHeaderValue(v string) string {
	v = strings.ReplaceAll(v, "\r", "")
	return strings.ReplaceAll(v, "\n", "")
}

// defaultErrorStatus is the default error->HTTP-status mapping used by the
// inbound handler cores (ServeAsync/ServeGateway) when WithErrorStatus is
// unset. It gives an HONEST status per failure cause (audit L2) rather than a
// blanket 500:
//
//   - msgin.ErrReplyTimeout -> 504 Gateway Timeout (no reply arrived in time).
//   - msgin.ErrGatewayClosed -> 503 Service Unavailable (shutting down).
//   - msgin.ErrDuplicateCorrelation -> 409 Conflict (only reachable when a
//     caller opts into trusting client correlation ids via
//     WithCorrelationID, and a client reuses one concurrently).
//   - msgin.ErrNoCorrelation, ErrUnsupportedPayload -> 500 Internal Server
//     Error: both are SERVER/WIRING faults, never the client's fault — this
//     adapter always mints a non-empty msg.ID() as the correlation id, so a
//     missing correlation key can only be a bug in this package or its
//     caller's wiring, not a malformed request.
//   - an oversize body (*http.MaxBytesError, via errors.As) -> 413 Request
//     Entity Too Large.
//   - any other DecodeRequest read/decode failure (via errors.As against the
//     local decodeError wrapper) -> 400 Bad Request.
//   - anything else (an unclassified downstream error) -> 500, the safe
//     default for a cause this package does not recognize.
func defaultErrorStatus(err error) int {
	switch {
	case errors.Is(err, msgin.ErrReplyTimeout):
		return http.StatusGatewayTimeout
	case errors.Is(err, msgin.ErrGatewayClosed):
		return http.StatusServiceUnavailable
	case errors.Is(err, msgin.ErrDuplicateCorrelation):
		return http.StatusConflict
	case errors.Is(err, msgin.ErrNoCorrelation), errors.Is(err, ErrUnsupportedPayload):
		return http.StatusInternalServerError
	}

	var maxBytesErr *http.MaxBytesError
	if errors.As(err, &maxBytesErr) {
		return http.StatusRequestEntityTooLarge
	}

	var de *decodeError
	if errors.As(err, &de) {
		return http.StatusBadRequest
	}

	return http.StatusInternalServerError
}
