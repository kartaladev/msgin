package msgin

import "encoding/json"

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
