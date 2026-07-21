package stdlib_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"

	"github.com/kartaladev/msgin"
	msghttp "github.com/kartaladev/msgin/adapter/http"
	"github.com/kartaladev/msgin/adapter/http/stdlib"
)

func TestMain(m *testing.M) { goleak.VerifyTestMain(m) }

// acceptingTarget returns an msgin.MessageChannel with a single subscriber
// that accepts every message (returns a nil error).
func acceptingTarget(t *testing.T) msgin.MessageChannel {
	t.Helper()
	ch := msgin.NewDirectChannel()
	require.NoError(t, ch.Subscribe(msgin.HandlerFunc(func(context.Context, msgin.Message[any]) error {
		return nil
	})))
	return ch
}

// echoExchange returns an msgin.RequestReplyExchange whose request channel
// forwards straight to its reply channel (an identity echo). It is closed via
// t.Cleanup so no reply waiter lingers (goleak).
func echoExchange(t *testing.T) msgin.RequestReplyExchange {
	t.Helper()
	request := msgin.NewDirectChannel()
	reply := msgin.NewDirectChannel()
	require.NoError(t, request.Subscribe(msgin.Chain(msgin.To(reply))))
	x, err := msgin.NewChannelExchange(request, reply)
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, x.Close()) })
	return x
}

func TestNewInbound(t *testing.T) {
	t.Parallel()

	type testCase struct {
		name   string
		target func(t *testing.T) msgin.MessageChannel
		opts   []msghttp.Option
		assert func(t *testing.T, h http.Handler, err error)
	}

	cases := []testCase{
		{
			name:   "valid target serves a POST with the default 202",
			target: acceptingTarget,
			assert: func(t *testing.T, h http.Handler, err error) {
				require.NoError(t, err)
				require.NotNil(t, h)

				srv := httptest.NewServer(h)
				defer srv.Close()

				resp, err := http.Post(srv.URL, "text/plain", strings.NewReader("hello"))
				require.NoError(t, err)
				defer resp.Body.Close()

				assert.Equal(t, http.StatusAccepted, resp.StatusCode)
			},
		},
		{
			name:   "nil target returns ErrNilTarget",
			target: func(*testing.T) msgin.MessageChannel { return nil },
			assert: func(t *testing.T, h http.Handler, err error) {
				assert.Nil(t, h)
				assert.ErrorIs(t, err, msghttp.ErrNilTarget)
			},
		},
		{
			name:   "an invalid option surfaces the NewConfig error",
			target: acceptingTarget,
			opts:   []msghttp.Option{msghttp.WithMaxBodyBytes(0)},
			assert: func(t *testing.T, h http.Handler, err error) {
				assert.Nil(t, h)
				assert.ErrorIs(t, err, msghttp.ErrInvalidMaxBodyBytes)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			h, err := stdlib.NewInbound(tc.target(t), tc.opts...)
			tc.assert(t, h, err)
		})
	}
}

func TestNewInboundGateway(t *testing.T) {
	t.Parallel()

	type testCase struct {
		name     string
		exchange func(t *testing.T) msgin.RequestReplyExchange
		opts     []msghttp.Option
		assert   func(t *testing.T, h http.Handler, err error)
	}

	cases := []testCase{
		{
			name:     "valid exchange serves a POST and echoes the body with 200",
			exchange: echoExchange,
			assert: func(t *testing.T, h http.Handler, err error) {
				require.NoError(t, err)
				require.NotNil(t, h)

				srv := httptest.NewServer(h)
				defer srv.Close()

				resp, err := http.Post(srv.URL, "text/plain", strings.NewReader("hello"))
				require.NoError(t, err)
				defer resp.Body.Close()

				assert.Equal(t, http.StatusOK, resp.StatusCode)
				body, err := io.ReadAll(resp.Body)
				require.NoError(t, err)
				assert.Equal(t, "hello", string(body))
			},
		},
		{
			name:     "nil exchange returns ErrNilExchange",
			exchange: func(*testing.T) msgin.RequestReplyExchange { return nil },
			assert: func(t *testing.T, h http.Handler, err error) {
				assert.Nil(t, h)
				assert.ErrorIs(t, err, msgin.ErrNilExchange)
			},
		},
		{
			name:     "an invalid option surfaces the NewConfig error",
			exchange: echoExchange,
			opts:     []msghttp.Option{msghttp.WithMaxBodyBytes(0)},
			assert: func(t *testing.T, h http.Handler, err error) {
				assert.Nil(t, h)
				assert.ErrorIs(t, err, msghttp.ErrInvalidMaxBodyBytes)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			h, err := stdlib.NewInboundGateway(tc.exchange(t), tc.opts...)
			tc.assert(t, h, err)
		})
	}
}

func TestRegister(t *testing.T) {
	t.Parallel()

	h, err := stdlib.NewInboundGateway(echoExchange(t))
	require.NoError(t, err)

	mux := http.NewServeMux()
	stdlib.Register(mux, "/rr", h)

	srv := httptest.NewServer(mux)
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/rr", "text/plain", strings.NewReader("hello"))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, "hello", string(body))
}
