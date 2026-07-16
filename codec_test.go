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
