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

	// ErrUnsupportedPayload is returned by EncodeResponse when a reply
	// message's payload is neither []byte nor string. The adapter is
	// type-agnostic (ADR 0001): it never encodes/decodes a domain type T,
	// only forwards the flow's own wire-shaped payload, so a non-bytes reply
	// payload is a WIRING fault, not a client input fault — hence its default
	// HTTP mapping is 500, not 400.
	ErrUnsupportedPayload = errors.New("msghttp: reply payload is not []byte or string")
)
