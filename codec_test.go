package msgin_test

import (
	"testing"

	"github.com/kartaladev/msgin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type order struct {
	ID    string `json:"id"`
	Total int    `json:"total"`
}

func TestJSONPayloadCodec_RoundTrip(t *testing.T) {
	c := msgin.JSONPayloadCodec[order]{}

	tests := []struct {
		name   string
		in     order
		assert func(t *testing.T, out order, b []byte, err error)
	}{
		{"round trips", order{ID: "o1", Total: 5},
			func(t *testing.T, out order, b []byte, err error) {
				require.NoError(t, err)
				assert.Equal(t, order{ID: "o1", Total: 5}, out)
			}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			b, err := c.Encode(tc.in)
			require.NoError(t, err)
			out, err := c.Decode(b)
			tc.assert(t, out, b, err)
		})
	}
}

func TestJSONPayloadCodec_DecodeError(t *testing.T) {
	c := msgin.JSONPayloadCodec[order]{}
	_, err := c.Decode([]byte("{not json"))
	assert.Error(t, err)
}

func TestBytesPayloadCodec(t *testing.T) {
	t.Parallel()

	var codec msgin.BytesPayloadCodec

	tests := []struct {
		name   string
		assert func(t *testing.T)
	}{
		{
			name: "encode is identity, not JSON",
			assert: func(t *testing.T) {
				t.Helper()
				got, err := codec.Encode([]byte("payload"))
				require.NoError(t, err)
				assert.Equal(t, []byte("payload"), got)
				// The bug this codec exists to prevent: JSONPayloadCodec would
				// emit a quoted base64 string here.
				assert.NotEqual(t, []byte(`"cGF5bG9hZA=="`), got)
			},
		},
		{
			name: "decode is identity",
			assert: func(t *testing.T) {
				t.Helper()
				got, err := codec.Decode([]byte("payload"))
				require.NoError(t, err)
				assert.Equal(t, []byte("payload"), got)
			},
		},
		{
			name: "nil round-trips both ways",
			assert: func(t *testing.T) {
				t.Helper()
				enc, err := codec.Encode(nil)
				require.NoError(t, err)
				assert.Nil(t, enc, "Encode(nil) must return nil, not an empty slice: the documented NOT NULL consequence depends on it")
				dec, err := codec.Decode(nil)
				require.NoError(t, err)
				assert.Nil(t, dec, "Decode(nil) must return nil, not an empty slice")
			},
		},
		{
			name: "encode does not alias the caller's slice",
			assert: func(t *testing.T) {
				t.Helper()
				in := []byte("payload")
				got, err := codec.Encode(in)
				require.NoError(t, err)
				in[0] = 'X'
				assert.Equal(t, []byte("payload"), got,
					"mutating the caller's slice after Encode must not change the encoded bytes")
			},
		},
		{
			name: "decode does not alias the caller's slice",
			assert: func(t *testing.T) {
				t.Helper()
				in := []byte("payload")
				got, err := codec.Decode(in)
				require.NoError(t, err)
				in[0] = 'X'
				assert.Equal(t, []byte("payload"), got,
					"mutating the caller's slice after Decode must not change the decoded bytes")
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tc.assert(t)
		})
	}
}
