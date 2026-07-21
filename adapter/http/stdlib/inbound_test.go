package stdlib_test

import (
	"bytes"
	"context"
	"fmt"
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

// TestNewInboundGateway_clientCannotChooseTheResponseContentType is the
// end-to-end regression test for the whole-branch security review's reflected
// XSS finding: the client's request Content-Type used to be copied into the
// reserved msgin.content-type header and echoed back unconditionally, so an
// echo flow served attacker bytes as text/html; with no request Content-Type
// at all, Go's sniffer inferred text/html from the body anyway.
func TestNewInboundGateway_clientCannotChooseTheResponseContentType(t *testing.T) {
	t.Parallel()

	type testCase struct {
		name        string
		contentType string
		assert      func(t *testing.T, resp *http.Response, body string)
	}

	const payload = "<script>alert(document.domain)</script>"

	cases := []testCase{
		{
			name:        "an explicit text/html request Content-Type is not reflected",
			contentType: "text/html",
			assert: func(t *testing.T, resp *http.Response, body string) {
				assert.Equal(t, http.StatusOK, resp.StatusCode)
				assert.Equal(t, payload, body, "the flow still echoes the bytes verbatim")
				assert.Equal(t, "application/octet-stream", resp.Header.Get("Content-Type"))
				assert.Equal(t, "nosniff", resp.Header.Get("X-Content-Type-Options"))
			},
		},
		{
			name:        "no request Content-Type still cannot be sniffed into text/html",
			contentType: "",
			assert: func(t *testing.T, resp *http.Response, body string) {
				assert.Equal(t, http.StatusOK, resp.StatusCode)
				assert.Equal(t, payload, body)
				assert.NotContains(t, resp.Header.Get("Content-Type"), "text/html")
				assert.Equal(t, "nosniff", resp.Header.Get("X-Content-Type-Options"))
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			h, err := stdlib.NewInboundGateway(echoExchange(t))
			require.NoError(t, err)

			srv := httptest.NewServer(h)
			defer srv.Close()

			req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, srv.URL, strings.NewReader(payload))
			require.NoError(t, err)
			if tc.contentType != "" {
				req.Header.Set("Content-Type", tc.contentType)
			}

			resp, err := http.DefaultClient.Do(req)
			require.NoError(t, err)
			defer resp.Body.Close()

			body, err := io.ReadAll(resp.Body)
			require.NoError(t, err)

			tc.assert(t, resp, string(body))
		})
	}
}

// ExampleNewInbound mounts the I1 async inbound handler on an httptest.Server
// and POSTs a request to it, printing only the response status — the
// fire-and-forget handoff has no reply body to show, so a deterministic 202
// is the whole observable outcome.
func ExampleNewInbound() {
	target := msgin.NewDirectChannel()
	if err := target.Subscribe(msgin.HandlerFunc(func(context.Context, msgin.Message[any]) error {
		return nil // accept every message
	})); err != nil {
		panic(err)
	}

	h, err := stdlib.NewInbound(target)
	if err != nil {
		panic(err)
	}

	srv := httptest.NewServer(h)
	defer srv.Close()

	resp, err := http.Post(srv.URL, "text/plain", strings.NewReader("hello"))
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()

	fmt.Println(resp.StatusCode)
	// Output: 202
}

// ExampleNewInboundGateway mounts the I2 sync inbound gateway handler on an
// httptest.Server, backed by a ChannelExchange whose flow uppercases the
// request body and forwards it to the reply channel. The activator uses
// msgin.WithPayload (never msgin.New) so the reply keeps the request's
// msgin.HeaderCorrelationID — dropping it would leave the reply unmatched to
// the waiting caller (Plan 019 G6).
func ExampleNewInboundGateway() {
	request := msgin.NewDirectChannel()
	reply := msgin.NewDirectChannel()

	flow := msgin.Chain(
		msgin.Activate(func(_ context.Context, m msgin.Message[[]byte]) (msgin.Message[[]byte], error) {
			return msgin.WithPayload(m, bytes.ToUpper(m.Payload())), nil
		}),
		msgin.To(reply),
	)
	if err := request.Subscribe(flow); err != nil {
		panic(err)
	}

	exchange, err := msgin.NewChannelExchange(request, reply)
	if err != nil {
		panic(err)
	}
	defer exchange.Close()

	h, err := stdlib.NewInboundGateway(exchange)
	if err != nil {
		panic(err)
	}

	srv := httptest.NewServer(h)
	defer srv.Close()

	resp, err := http.Post(srv.URL, "text/plain", strings.NewReader("hello"))
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		panic(err)
	}
	fmt.Println(string(body))
	// Output: HELLO
}
