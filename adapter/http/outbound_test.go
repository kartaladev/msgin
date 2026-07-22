package msghttp_test

import (
	"context"
	"io"
	"net/http"
	"testing"

	"github.com/jonboulle/clockwork"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kartaladev/msgin"
	msghttp "github.com/kartaladev/msgin/adapter/http"
)

// TestNewConfig_outbound exercises the outbound option surface added in Plan
// 024 Task 1. Config's outbound fields are unexported with no getters, so a
// blackbox (package msghttp_test) test can observe only NewConfig's returned
// error, never a resolved value. Only WithMaxResponseBytes(<=0) is therefore a
// genuine BEHAVIORAL assertion here (round-2 audit F3): it asserts through the
// ErrInvalidMaxResponseBytes error contract and can fail red-first. The
// remaining rows are line-covered only — they assert construction
// succeeds/fails as the error contract dictates, and are NOT dressed up as
// value checks. Their behavioral correctness (the 1 MiB default, the kept
// default client/clock, the cloned allow-lists) becomes observable, and is
// asserted, in Tasks 2/4/5 where the value is reachable.
func TestNewConfig_outbound(t *testing.T) {
	t.Parallel()

	type testCase struct {
		name   string
		opts   []msghttp.Option
		assert func(t *testing.T, cfg *msghttp.Config, err error)
	}

	valid := func(t *testing.T, cfg *msghttp.Config, err error) {
		t.Helper()
		require.NoError(t, err)
		require.NotNil(t, cfg)
	}

	cases := []testCase{
		{
			name:   "outbound defaults are valid",
			assert: valid,
		},
		{
			name: "WithMaxResponseBytes(0) is invalid",
			opts: []msghttp.Option{msghttp.WithMaxResponseBytes(0)},
			assert: func(t *testing.T, cfg *msghttp.Config, err error) {
				assert.Nil(t, cfg)
				assert.ErrorIs(t, err, msghttp.ErrInvalidMaxResponseBytes)
			},
		},
		{
			name: "WithMaxResponseBytes(-1) is invalid",
			opts: []msghttp.Option{msghttp.WithMaxResponseBytes(-1)},
			assert: func(t *testing.T, cfg *msghttp.Config, err error) {
				assert.Nil(t, cfg)
				assert.ErrorIs(t, err, msghttp.ErrInvalidMaxResponseBytes)
			},
		},
		{
			name:   "WithMaxResponseBytes(1) is accepted",
			opts:   []msghttp.Option{msghttp.WithMaxResponseBytes(1)},
			assert: valid,
		},
		{
			name:   "WithHTTPClient(nil) is a no-op",
			opts:   []msghttp.Option{msghttp.WithHTTPClient(nil)},
			assert: valid,
		},
		{
			name:   "WithHTTPClient with a custom client is accepted",
			opts:   []msghttp.Option{msghttp.WithHTTPClient(&http.Client{})},
			assert: valid,
		},
		{
			name:   "WithOutboundClock(nil) is a no-op",
			opts:   []msghttp.Option{msghttp.WithOutboundClock(nil)},
			assert: valid,
		},
		{
			name:   "WithOutboundClock with a fake clock is accepted",
			opts:   []msghttp.Option{msghttp.WithOutboundClock(clockwork.NewFakeClock())},
			assert: valid,
		},
		{
			name:   "WithFollowRedirects is accepted",
			opts:   []msghttp.Option{msghttp.WithFollowRedirects()},
			assert: valid,
		},
		{
			name:   "WithOutboundHeaders is accepted",
			opts:   []msghttp.Option{msghttp.WithOutboundHeaders("X-A", "X-B")},
			assert: valid,
		},
		{
			name:   "WithOutboundReplyHeaders is accepted",
			opts:   []msghttp.Option{msghttp.WithOutboundReplyHeaders("X-A", "X-B")},
			assert: valid,
		},
		{
			name:   "WithErrorBodyExcerpt is accepted",
			opts:   []msghttp.Option{msghttp.WithErrorBodyExcerpt()},
			assert: valid,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg, err := msghttp.NewConfig(tc.opts...)
			tc.assert(t, cfg, err)
		})
	}
}

// TestEncodeRequest exercises Plan 024 Task 2's EncodeRequest and its shared
// payload/content-type extraction. Every branch (1-14, incl. 9b) is covered by
// a mutation-sensitive case: each would go red if the guarded line were
// removed.
func TestEncodeRequest(t *testing.T) {
	t.Parallel()

	const targetURL = "https://example.test/hook"

	type testCase struct {
		name      string
		method    string
		opts      []msghttp.Option
		nilConfig bool
		msg       func() msgin.Message[any]
		assert    func(t *testing.T, ctx context.Context, req *http.Request, err error)
	}

	readBody := func(t *testing.T, req *http.Request) string {
		t.Helper()
		require.NotNil(t, req.Body)
		b, err := io.ReadAll(req.Body)
		require.NoError(t, err)
		return string(b)
	}

	cases := []testCase{
		{ // branch 1
			name: "[]byte payload becomes the request body verbatim",
			msg:  func() msgin.Message[any] { return msgin.New[any]([]byte("payload-bytes")) },
			assert: func(t *testing.T, _ context.Context, req *http.Request, err error) {
				require.NoError(t, err)
				require.NotNil(t, req)
				assert.Equal(t, "payload-bytes", readBody(t, req))
			},
		},
		{ // branch 2
			name: "string payload becomes []byte(s) as the request body",
			msg:  func() msgin.Message[any] { return msgin.New[any]("payload-string") },
			assert: func(t *testing.T, _ context.Context, req *http.Request, err error) {
				require.NoError(t, err)
				require.NotNil(t, req)
				assert.Equal(t, "payload-string", readBody(t, req))
			},
		},
		{ // branch 3
			name: "non-bytes payload errors and builds no request",
			msg:  func() msgin.Message[any] { return msgin.New[any](42) },
			assert: func(t *testing.T, _ context.Context, req *http.Request, err error) {
				assert.ErrorIs(t, err, msghttp.ErrUnsupportedPayload)
				assert.Nil(t, req, "no request may be built when the payload is unsupported")
			},
		},
		{ // branch 4
			name: "present non-empty msgin.content-type is used as Content-Type",
			msg: func() msgin.Message[any] {
				return msgin.New[any]([]byte("x")).WithHeader(msgin.HeaderContentType, "application/json")
			},
			assert: func(t *testing.T, _ context.Context, req *http.Request, err error) {
				require.NoError(t, err)
				assert.Equal(t, "application/json", req.Header.Get("Content-Type"))
			},
		},
		{ // branch 5
			name: "absent msgin.content-type falls back to application/octet-stream",
			msg:  func() msgin.Message[any] { return msgin.New[any]([]byte("x")) },
			assert: func(t *testing.T, _ context.Context, req *http.Request, err error) {
				require.NoError(t, err)
				assert.Equal(t, "application/octet-stream", req.Header.Get("Content-Type"))
			},
		},
		{ // branch 6
			name: "non-string msgin.content-type falls back to the default",
			msg: func() msgin.Message[any] {
				return msgin.New[any]([]byte("x")).WithHeader(msgin.HeaderContentType, 42)
			},
			assert: func(t *testing.T, _ context.Context, req *http.Request, err error) {
				require.NoError(t, err)
				assert.Equal(t, "application/octet-stream", req.Header.Get("Content-Type"))
			},
		},
		{ // branch 7
			name: "msgin.content-type that sanitizes to empty falls back to the default",
			msg: func() msgin.Message[any] {
				return msgin.New[any]([]byte("x")).WithHeader(msgin.HeaderContentType, "\r\n")
			},
			assert: func(t *testing.T, _ context.Context, req *http.Request, err error) {
				require.NoError(t, err)
				assert.Equal(t, "application/octet-stream", req.Header.Get("Content-Type"))
			},
		},
		{ // branch 8
			name: "allow-listed message header is forwarded, CRLF-sanitized",
			opts: []msghttp.Option{msghttp.WithOutboundHeaders("X-Custom")},
			msg: func() msgin.Message[any] {
				return msgin.New[any]([]byte("x")).WithHeader("X-Custom", "line1\r\nInjected: evil")
			},
			assert: func(t *testing.T, _ context.Context, req *http.Request, err error) {
				require.NoError(t, err)
				v := req.Header.Get("X-Custom")
				assert.Equal(t, "line1Injected: evil", v)
				assert.NotContains(t, v, "\r")
				assert.NotContains(t, v, "\n")
			},
		},
		{ // branch 9
			name: "non-allow-listed message header is dropped",
			opts: []msghttp.Option{msghttp.WithOutboundHeaders("X-Allowed")},
			msg: func() msgin.Message[any] {
				return msgin.New[any]([]byte("x")).WithHeader("X-Secret", "leak-me")
			},
			assert: func(t *testing.T, _ context.Context, req *http.Request, err error) {
				require.NoError(t, err)
				assert.Empty(t, req.Header.Get("X-Secret"))
			},
		},
		{ // branch 9b — decision 4: reserved msgin.* names are dropped even when allow-listed
			name: "allow-listed reserved msgin.* names are dropped (case-insensitive guard, seeded values)",
			opts: []msghttp.Option{msghttp.WithOutboundHeaders("msgin.correlation-id", "MSGIN.Correlation-Id")},
			msg: func() msgin.Message[any] {
				// Both values must be SEEDED with the exact keys — the value lookup is
				// case-sensitive, and plain msgin.New stamps NEITHER — so that removing
				// the reserved-name guard would forward them and turn this test red.
				return msgin.New[any]([]byte("x")).
					WithHeader(msgin.HeaderCorrelationID, "corr-123").
					WithHeader("MSGIN.Correlation-Id", "corr-456")
			},
			assert: func(t *testing.T, _ context.Context, req *http.Request, err error) {
				require.NoError(t, err)
				assert.Empty(t, req.Header.Get("msgin.correlation-id"),
					"lowercase reserved name must be dropped")
				assert.Empty(t, req.Header.Get("MSGIN.Correlation-Id"),
					"upper-case reserved name must be dropped — the guard is case-insensitive")
			},
		},
		{ // branch 10
			name: "allow-listed header whose value is not a string is skipped",
			opts: []msghttp.Option{msghttp.WithOutboundHeaders("X-Num")},
			msg: func() msgin.Message[any] {
				return msgin.New[any]([]byte("x")).WithHeader("X-Num", 42)
			},
			assert: func(t *testing.T, _ context.Context, req *http.Request, err error) {
				require.NoError(t, err)
				assert.Empty(t, req.Header.Get("X-Num"))
			},
		},
		{ // branch 11
			name: "an allow-list entry naming Content-Type cannot override the resolved Content-Type",
			opts: []msghttp.Option{msghttp.WithOutboundHeaders("Content-Type")},
			msg: func() msgin.Message[any] {
				return msgin.New[any]([]byte("x")).
					WithHeader("Content-Type", "text/evil").
					WithHeader(msgin.HeaderContentType, "application/json")
			},
			assert: func(t *testing.T, _ context.Context, req *http.Request, err error) {
				require.NoError(t, err)
				assert.Equal(t, "application/json", req.Header.Get("Content-Type"),
					"Content-Type is written last, so an allow-list entry can never override it")
			},
		},
		{ // branch 12
			name:   "an invalid method fails NewRequestWithContext and builds no request",
			method: "BAD METHOD",
			msg:    func() msgin.Message[any] { return msgin.New[any]([]byte("x")) },
			assert: func(t *testing.T, _ context.Context, req *http.Request, err error) {
				require.Error(t, err)
				assert.Nil(t, req)
			},
		},
		{ // branch 13
			name:      "a nil *Config does not panic: empty allow-list and default Content-Type",
			nilConfig: true,
			msg: func() msgin.Message[any] {
				return msgin.New[any]([]byte("x")).WithHeader("X-Custom", "present")
			},
			assert: func(t *testing.T, _ context.Context, req *http.Request, err error) {
				require.NoError(t, err)
				require.NotNil(t, req)
				assert.Equal(t, "application/octet-stream", req.Header.Get("Content-Type"))
				assert.Empty(t, req.Header.Get("X-Custom"),
					"a nil Config has an empty allow-list, so no message header is forwarded")
			},
		},
		{ // branch 14
			name: "the returned request carries the caller's context",
			msg:  func() msgin.Message[any] { return msgin.New[any]([]byte("x")) },
			assert: func(t *testing.T, ctx context.Context, req *http.Request, err error) {
				require.NoError(t, err)
				require.NotNil(t, req)
				assert.Equal(t, ctx, req.Context(), "EncodeRequest must not swap the caller's context")
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			method := tc.method
			if method == "" {
				method = http.MethodPost
			}

			var cfg *msghttp.Config
			if !tc.nilConfig {
				var err error
				cfg, err = msghttp.NewConfig(tc.opts...)
				require.NoError(t, err)
			}

			ctx := t.Context()
			req, err := msghttp.EncodeRequest(ctx, method, targetURL, tc.msg(), cfg)
			tc.assert(t, ctx, req, err)
		})
	}
}
