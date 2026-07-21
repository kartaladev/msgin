package msgin_test

import (
	"context"
	"strings"
	"testing"

	"github.com/kartaladev/msgin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeExchange is a lightweight RequestReplyExchange double for the one-method
// SPI: no mockgen needed.
type fakeExchange struct {
	fn func(ctx context.Context, req msgin.Message[any]) (msgin.Message[any], error)
}

func (f fakeExchange) Exchange(ctx context.Context, req msgin.Message[any]) (msgin.Message[any], error) {
	return f.fn(ctx, req)
}

func TestNewGateway_nilExchange(t *testing.T) {
	gw, err := msgin.NewGateway[string, string](nil)

	assert.Nil(t, gw)
	assert.ErrorIs(t, err, msgin.ErrNilExchange)
}

func TestGateway_Request(t *testing.T) {
	tests := []struct {
		name   string
		fn     func(ctx context.Context, req msgin.Message[any]) (msgin.Message[any], error)
		assert func(t *testing.T, rep string, err error)
	}{
		{
			name: "happy path",
			fn: func(_ context.Context, req msgin.Message[any]) (msgin.Message[any], error) {
				payload, ok := req.Payload().(string)
				if !ok || payload != "hello" {
					t.Fatalf("want request payload %q, got %v (ok=%v)", "hello", req.Payload(), ok)
				}
				corrID, ok := req.Headers().String(msgin.HeaderCorrelationID)
				if !ok || corrID == "" {
					t.Fatalf("want a non-empty HeaderCorrelationID on the request, got %q (ok=%v)", corrID, ok)
				}
				return msgin.New[any]("world"), nil
			},
			assert: func(t *testing.T, rep string, err error) {
				require.NoError(t, err)
				assert.Equal(t, "world", rep)
			},
		},
		{
			name: "wrong reply type",
			fn: func(context.Context, msgin.Message[any]) (msgin.Message[any], error) {
				return msgin.New[any](42), nil
			},
			assert: func(t *testing.T, rep string, err error) {
				assert.ErrorIs(t, err, msgin.ErrPayloadType)
				assert.Empty(t, rep)
			},
		},
		{
			name: "exchange error propagates",
			fn: func(context.Context, msgin.Message[any]) (msgin.Message[any], error) {
				return msgin.Message[any]{}, context.DeadlineExceeded
			},
			assert: func(t *testing.T, rep string, err error) {
				assert.ErrorIs(t, err, context.DeadlineExceeded)
				assert.Empty(t, rep)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gw, err := msgin.NewGateway[string, string](fakeExchange{fn: tt.fn})
			require.NoError(t, err)

			rep, err := gw.Request(t.Context(), "hello")

			tt.assert(t, rep, err)
		})
	}
}

func TestGateway_Request_freshCorrelationIDPerRequest(t *testing.T) {
	var gotIDs []string
	ex := fakeExchange{fn: func(_ context.Context, req msgin.Message[any]) (msgin.Message[any], error) {
		id, _ := req.Headers().String(msgin.HeaderCorrelationID)
		gotIDs = append(gotIDs, id)
		return msgin.New[any]("ok"), nil
	}}
	gw, err := msgin.NewGateway[string, string](ex)
	require.NoError(t, err)

	_, err = gw.Request(t.Context(), "first")
	require.NoError(t, err)
	_, err = gw.Request(t.Context(), "second")
	require.NoError(t, err)

	require.Len(t, gotIDs, 2)
	assert.NotEmpty(t, gotIDs[0])
	assert.NotEmpty(t, gotIDs[1])
	assert.NotEqual(t, gotIDs[0], gotIDs[1])
}

// TestGateway_channelExchangeRoundTrip is the integration test: a real
// ChannelExchange over an echo flow that uppercases the payload, wired through
// Gateway.Request end to end.
func TestGateway_channelExchangeRoundTrip(t *testing.T) {
	request := msgin.NewDirectChannel()
	reply := msgin.NewDirectChannel()
	require.NoError(t, request.Subscribe(msgin.Chain(
		msgin.Activate(func(_ context.Context, m msgin.Message[string]) (msgin.Message[string], error) {
			return msgin.WithPayload(m, strings.ToUpper(m.Payload())), nil
		}),
		msgin.To(reply),
	)))
	ex, err := msgin.NewChannelExchange(request, reply)
	require.NoError(t, err)
	gw, err := msgin.NewGateway[string, string](ex)
	require.NoError(t, err)

	rep, err := gw.Request(t.Context(), "hello")

	require.NoError(t, err)
	assert.Equal(t, "HELLO", rep)
}
