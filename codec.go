package msgin

import (
	"bytes"
	"encoding/json"
)

// PayloadCodec (de)serializes a typed payload to/from bytes. It lives in the
// typed runtime (which knows T); wire adapters never see T (spec §3, ADR 0001).
type PayloadCodec[T any] interface {
	Encode(T) ([]byte, error)
	Decode([]byte) (T, error)
}

// JSONPayloadCodec is the default PayloadCodec using encoding/json.
type JSONPayloadCodec[T any] struct{}

// Encode marshals v to its JSON representation.
func (JSONPayloadCodec[T]) Encode(v T) ([]byte, error) { return json.Marshal(v) }

// Decode unmarshals JSON bytes b into a value of type T.
func (JSONPayloadCodec[T]) Decode(b []byte) (T, error) {
	var v T
	err := json.Unmarshal(b, &v)
	return v, err
}

// BytesPayloadCodec is the identity PayloadCodec for []byte payloads: it passes
// the bytes through unchanged in both directions.
//
// Pair it EXPLICITLY whenever T is []byte and the adapter is a wire adapter:
//
//	msgin.NewProducer[[]byte](out, msgin.WithProducerCodec[[]byte](msgin.BytesPayloadCodec{}))
//
// Without it, the codec resolution defaults to JSONPayloadCodec, and
// json.Marshal of a []byte is a quoted base64 STRING — so a raw body would go on
// the wire as "cGF5bG9hZA==" rather than payload. That is almost never what a
// caller sending raw bytes intends.
//
// It is deliberately NOT the automatic default for T == []byte: adapters that
// already persist base64-encoded envelopes (database/sql, redis, nats) would
// silently change their on-wire format. The pairing is the caller's explicit
// choice.
//
// Both methods COPY, so neither the caller's slice nor the returned one can be
// mutated through the other. Messages are immutable by contract, and a message
// may be shared across a pub-sub channel; aliasing would break that.
//
// TWO RESIDUALS, both consequences of the pass-through being exact:
//
//   - It removes an ACCIDENTAL escaping layer. JSONPayloadCodec's quoting and
//     escaping neutralised some hostile bytes as a side effect — never as a
//     security control, but in practice. This codec emits the caller's bytes
//     verbatim, so sanitising or validating the payload for whatever the sink
//     interprets it as (SQL, a shell, HTML, a log) is wholly the caller's and the
//     adapter's responsibility.
//   - Encode(nil) returns nil, where JSONPayloadCodec returned the four bytes
//     null. A store with a NOT NULL payload column that accepted a nil payload
//     before will now reject it. Encode an explicit empty or sentinel payload if
//     the sink cannot take a NULL.
type BytesPayloadCodec struct{}

// Encode returns a copy of v, so the encoded bytes never alias the caller's
// slice. A nil v encodes to nil.
func (BytesPayloadCodec) Encode(v []byte) ([]byte, error) {
	return bytes.Clone(v), nil
}

// Decode returns a copy of b, so the decoded payload never aliases the adapter's
// buffer. A nil b decodes to nil.
func (BytesPayloadCodec) Decode(b []byte) ([]byte, error) {
	return bytes.Clone(b), nil
}
