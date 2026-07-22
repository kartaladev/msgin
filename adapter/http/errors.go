package msghttp

import "errors"

var (
	// ErrNilTarget is returned by the async inbound handler's constructor
	// (adapter/http/stdlib.NewInbound) when the target msgin.MessageChannel is
	// nil. It is a local sentinel rather than a reuse of msgin.ErrNilChannel:
	// that sentinel's wording ("request or reply channel is nil") describes
	// the Messaging Gateway's two-channel exchange, which reads wrong for the
	// async inbound handler's single send target (Plan 020 audit L3).
	ErrNilTarget = errors.New("msghttp: target channel is nil")

	// ErrInvalidMaxBodyBytes is returned by NewConfig when an explicit
	// WithMaxBodyBytes is <= 0. Leaving WithMaxBodyBytes unset takes the 1
	// MiB default instead of hitting this error — only an explicit
	// non-positive value is rejected (the set-flag pattern distinguishes
	// "unset" from "explicit invalid").
	ErrInvalidMaxBodyBytes = errors.New("msghttp: max body bytes must be > 0")

	// ErrInvalidStatusCode is returned by NewConfig when an explicit
	// WithSuccessStatus falls outside [100,599]. Leaving WithSuccessStatus
	// unset takes the 202 default instead of hitting this error.
	ErrInvalidStatusCode = errors.New("msghttp: status code must be in [100,599]")

	// ErrDecodeRequest wraps every request-body read/decode failure returned
	// by DecodeRequest. It exists so a status mapper — DefaultErrorStatus, or
	// a caller's own WithErrorStatus override — can tell a non-oversize
	// read/decode fault (400) from an unclassified downstream error (500)
	// without over-claiming meaning for an arbitrary error it did not
	// originate. The wrap preserves the underlying cause (a plain read error,
	// or *http.MaxBytesError on overflow), so errors.Is/errors.As still see
	// through it: check *http.MaxBytesError FIRST, since an oversize body
	// satisfies both.
	ErrDecodeRequest = errors.New("msghttp: decode request failed")

	// ErrWriteResponse wraps a failure of the response-body write in
	// EncodeResponse (typically a client that hung up mid-response). It is the
	// structural signal that the response has ALREADY been committed — the 200
	// status line went out before the body write failed — so a caller must
	// only log it and must NEVER follow it with a second WriteHeader (which
	// net/http reports as a "superfluous response.WriteHeader call"). Every
	// other EncodeResponse error (e.g. ErrUnsupportedPayload) is returned
	// before anything is written, leaving the ResponseWriter untouched.
	ErrWriteResponse = errors.New("msghttp: write response body failed")

	// ErrUnsupportedPayload is returned when a message payload is neither
	// []byte nor string: by EncodeResponse for a reply message, and by
	// EncodeRequest for an outbound request message. The adapter is
	// type-agnostic (ADR 0001): it never encodes/decodes a domain type T, it
	// only forwards the flow's own wire-shaped payload, so a non-bytes payload
	// is a WIRING fault, not a client input fault — hence EncodeResponse's
	// default HTTP mapping for it is 500 (via DefaultErrorStatus), not 400.
	ErrUnsupportedPayload = errors.New("msghttp: reply payload is not []byte or string")

	// ErrEmptyURL is returned by NewOutbound/NewExchange when the target URL
	// is empty or only whitespace.
	ErrEmptyURL = errors.New("msghttp: outbound URL is empty")

	// ErrInvalidURL is returned by NewOutbound/NewExchange when the target URL
	// fails to parse, does not use the http or https scheme, or has an empty
	// host.
	//
	// It rejects a malformed or non-HTTP target only. msgin performs NO
	// private-IP, link-local, loopback or metadata-endpoint filtering: a
	// caller-configured "http://169.254.169.254/…" is accepted and requested
	// verbatim. Because the URL is caller-configured and never derived from a
	// message payload, this prevents MESSAGE-DRIVEN SSRF only — it is not an
	// SSRF egress filter.
	ErrInvalidURL = errors.New("msghttp: outbound URL is invalid")

	// ErrInvalidMaxResponseBytes is returned by NewConfig when an explicit
	// WithMaxResponseBytes is <= 0. Leaving WithMaxResponseBytes unset takes
	// the 1 MiB default instead of hitting this error — the set-flag pattern
	// distinguishes "unset" from "explicit invalid", mirroring
	// ErrInvalidMaxBodyBytes.
	ErrInvalidMaxResponseBytes = errors.New("msghttp: max response bytes must be > 0")

	// ErrReplyTooLarge is returned by an Exchange when the remote response body
	// exceeds the WithMaxResponseBytes cap (default 1 MiB).
	ErrReplyTooLarge = errors.New("msghttp: reply body exceeds max response bytes")

	// ErrNilResponse is returned by ClassifyResponse when it is handed a nil
	// *http.Response. (*http.Client).Do never returns a nil response together
	// with a nil error, so on the live path this arm is unreachable; it is kept
	// (decision 3) as a typed guard so a caller feeding a hand-built nil gets a
	// clear error rather than a nil-dereference panic.
	ErrNilResponse = errors.New("msghttp: nil response")

	// ErrOutboundStatus is the sentinel a *StatusError unwraps to: it marks an
	// outbound request that completed but returned a non-2xx HTTP status.
	// ClassifyResponse may additionally wrap it in msgin.Permanent or
	// msgin.RetryAfter, but it never carries a payload-invalid or
	// decode-failure meaning inferred from the status (INV-3): it is only ever
	// "the remote returned status N".
	ErrOutboundStatus = errors.New("msghttp: outbound request returned an error status")

	// ErrOutboundTransport wraps a transport-level failure of an outbound
	// request (the error (*http.Client).Do returns). The wrap deliberately
	// discards the *url.Error's URL — whose own Error() redacts only the
	// password, leaking username, host, path and query (e.g. a webhook token in
	// the query string) — so no target URL reaches the error string (INV-5).
	// The underlying cause is preserved with %w, so errors.Is(err,
	// context.Canceled) and friends still hold on the cancellation arm.
	ErrOutboundTransport = errors.New("msghttp: outbound request transport error")
)
