package sql

import (
	"encoding/json"
	"fmt"
	"time"

	msgin "github.com/kartaladev/msgin"
)

// EncodeHeaders frames a message's Headers into the JSON bytes stored in the
// headers column (spec 001 §8 — envelope framing, type-agnostic).
//
// # On-wire header format (stability contract)
//
// This is public API: the JSON shape below is a stability contract that a
// custom LeaseDialect, an external tool, or a direct reader of the headers column
// may depend on. A single JSON object maps each header key to its value, with
// two reserved keys normalized so they round-trip losslessly:
//
//   - msgin.timestamp is written as an RFC3339 (nanosecond) string. A time.Time
//     would otherwise marshal to a string that DecodeHeaders could not restore
//     to a time.Time.
//   - msgin.delivery-count, msgin.sequence-number, and msgin.sequence-size are
//     each written as a JSON number and restored to an int by DecodeHeaders
//     (ADR 0021 §2 M-1).
//
// All other keys — the reserved string keys (msgin.id, msgin.content-type,
// msgin.correlation-id) and any custom keys — are marshaled as-is; a numeric
// custom value therefore returns as float64 (the standard encoding/json
// behavior). A value JSON cannot marshal (a channel, a func) surfaces as an
// error rather than being silently dropped.
//
// EncodeHeaders/DecodeHeaders are the only serialization the adapter performs.
func EncodeHeaders(h msgin.Headers) ([]byte, error) {
	m := make(map[string]any)
	for k, v := range h.All() {
		if k == msgin.HeaderTimestamp {
			if t, ok := v.(time.Time); ok {
				m[k] = t.Format(time.RFC3339Nano)
				continue
			}
		}
		m[k] = v
	}
	b, err := json.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("msgin/sql: encode headers: %w", err)
	}
	return b, nil
}

// DecodeHeaders reconstructs Headers from the framed JSON bytes written by
// EncodeHeaders, per the on-wire format documented on EncodeHeaders (the
// stability contract): msgin.timestamp is parsed back to a time.Time,
// msgin.delivery-count back to an int, and (ADR 0021 §2 M-1)
// msgin.sequence-number / msgin.sequence-size back to an int. Custom keys are
// returned with the types JSON produces (a numeric custom header comes back as
// float64 — the standard encoding/json behavior, so callers store immutable,
// JSON-round-trippable values). Empty input yields empty Headers; malformed
// JSON is an error.
func DecodeHeaders(b []byte) (msgin.Headers, error) {
	if len(b) == 0 {
		return msgin.NewHeaders(nil), nil
	}
	var raw map[string]any
	if err := json.Unmarshal(b, &raw); err != nil {
		return msgin.Headers{}, fmt.Errorf("msgin/sql: decode headers: %w", err)
	}
	for k, v := range raw {
		switch k {
		case msgin.HeaderTimestamp:
			if s, ok := v.(string); ok {
				if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
					raw[k] = t
				}
			}
		case msgin.HeaderDeliveryCount, msgin.HeaderSequenceNumber, msgin.HeaderSequenceSize:
			if f, ok := v.(float64); ok {
				raw[k] = int(f)
			}
		}
	}
	return msgin.NewHeaders(raw), nil
}
