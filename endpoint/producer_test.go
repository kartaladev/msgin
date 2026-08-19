package endpoint_test

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/kartaladev/msgin"
	"github.com/kartaladev/msgin/adapter/memory"
	"github.com/kartaladev/msgin/endpoint"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// wireOut is a non-live-value (wire) outbound adapter capturing the last payload.
type wireOut struct{ last msgin.Message[any] }

func (w *wireOut) Send(_ context.Context, m msgin.Message[any]) error { w.last = m; return nil }

// errEncode is the sentinel returned by failingCodec.Encode, used to prove
// that producer.Send's encode-error path wraps without breaking errors.Is.
var errEncode = errors.New("boom")

// failingCodec is a PayloadCodec[order] whose Encode always fails.
type failingCodec struct{}

func (failingCodec) Encode(order) ([]byte, error) { return nil, errEncode }
func (failingCodec) Decode([]byte) (order, error) { return order{}, nil }

func TestNewProducer_CodecPairing(t *testing.T) {
	tests := []struct {
		name   string
		out    msgin.OutboundAdapter
		opts   []endpoint.ProducerOption[order]
		assert func(t *testing.T, p endpoint.Producer[order], err error)
	}{
		{"live-value adapter needs no codec", memory.New(), nil,
			func(t *testing.T, p endpoint.Producer[order], err error) {
				require.NoError(t, err)
			}},
		{"live-value adapter with codec is rejected", memory.New(),
			[]endpoint.ProducerOption[order]{endpoint.WithProducerCodec[order](msgin.JSONPayloadCodec[order]{})},
			func(t *testing.T, p endpoint.Producer[order], err error) {
				assert.ErrorIs(t, err, msgin.ErrUnexpectedCodec)
			}},
		{"nil adapter rejected", nil, nil,
			func(t *testing.T, p endpoint.Producer[order], err error) {
				assert.ErrorIs(t, err, msgin.ErrNilAdapter)
			}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p, err := endpoint.NewProducer[order](tc.out, tc.opts...)
			tc.assert(t, p, err)
		})
	}
}

// TestNewProducer_NilOptionElement proves a nil ELEMENT of opts is a bare
// ErrNilFunc naming the computed index, not a panic (Spec 015 §3.1, ADR 0031
// D-P/D-Q/D-R). out is a valid live-value adapter throughout — this is about
// the SECOND argument, opts, never about the "nil adapter rejected" case
// already covered by TestNewProducer_CodecPairing.
func TestNewProducer_NilOptionElement(t *testing.T) {
	tests := []struct {
		name   string
		opts   []endpoint.ProducerOption[order]
		assert func(t *testing.T, err error)
	}{
		{
			name: "nil element alone",
			opts: []endpoint.ProducerOption[order]{nil},
			assert: func(t *testing.T, err error) {
				require.ErrorIs(t, err, msgin.ErrNilFunc)
				assert.False(t, msgin.IsPermanent(err), "R1 nil-option error must stay bare")
				assert.Contains(t, err.Error(), "endpoint.NewProducer: nil option at index 0")
			},
		},
		{
			name: "nil element after a valid option asserts the COMPUTED index and the FULL position",
			opts: []endpoint.ProducerOption[order]{endpoint.WithProducerLogger[order](slog.Default()), nil},
			assert: func(t *testing.T, err error) {
				require.ErrorIs(t, err, msgin.ErrNilFunc)
				assert.Contains(t, err.Error(), "endpoint.NewProducer: nil option at index 1")
			},
		},
		{
			name: "first of two nils wins",
			opts: []endpoint.ProducerOption[order]{nil, nil},
			assert: func(t *testing.T, err error) {
				require.ErrorIs(t, err, msgin.ErrNilFunc)
				assert.Contains(t, err.Error(), "endpoint.NewProducer: nil option at index 0")
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := endpoint.NewProducer[order](memory.New(), tc.opts...)
			tc.assert(t, err)
		})
	}
}

// TestProducer_Send exercises p.Send across the three producer setups that
// share the same call shape: a wire adapter (default JSON codec), a
// live-value adapter (payload passes through unencoded), and a wire adapter
// paired with a codec whose Encode always fails.
func TestProducer_Send(t *testing.T) {
	tests := []struct {
		name    string
		newProd func(t *testing.T, w *wireOut) endpoint.Producer[order]
		assert  func(t *testing.T, w *wireOut, err error)
	}{
		{
			name: "wire adapter encodes to bytes via default JSON codec",
			newProd: func(t *testing.T, w *wireOut) endpoint.Producer[order] {
				p, err := endpoint.NewProducer[order](w) // wire adapter, default JSON codec
				require.NoError(t, err)
				return p
			},
			assert: func(t *testing.T, w *wireOut, err error) {
				require.NoError(t, err)
				b, ok := w.last.Payload().([]byte)
				require.True(t, ok, "wire payload must be []byte")
				assert.JSONEq(t, `{"id":"o1","total":3}`, string(b))
			},
		},
		{
			name: "live-value adapter passes payload through unencoded",
			newProd: func(t *testing.T, _ *wireOut) endpoint.Producer[order] {
				// memory is a LiveValueSource: the live order value passes through unencoded.
				m := memory.New(memory.WithBuffer(1))
				p, err := endpoint.NewProducer[order](m)
				require.NoError(t, err)
				return p
			},
			assert: func(t *testing.T, _ *wireOut, err error) {
				require.NoError(t, err)
			},
		},
		{
			name: "wire adapter encode failure propagates wrapped error",
			newProd: func(t *testing.T, w *wireOut) endpoint.Producer[order] {
				p, err := endpoint.NewProducer[order](w, endpoint.WithProducerCodec[order](failingCodec{}))
				require.NoError(t, err)
				return p
			},
			assert: func(t *testing.T, _ *wireOut, err error) {
				assert.ErrorIs(t, err, errEncode)
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w := &wireOut{}
			p := tc.newProd(t, w)
			err := p.Send(t.Context(), msgin.New(order{ID: "o1", Total: 3}))
			tc.assert(t, w, err)
		})
	}
}
