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

	// ErrUnsupportedPayload is returned by EncodeResponse when a reply
	// message's payload is neither []byte nor string. The adapter is
	// type-agnostic (ADR 0001): it never encodes/decodes a domain type T,
	// only forwards the flow's own wire-shaped payload, so a non-bytes reply
	// payload is a WIRING fault, not a client input fault — hence its default
	// HTTP mapping is 500, not 400.
	ErrUnsupportedPayload = errors.New("msghttp: reply payload is not []byte or string")
)
