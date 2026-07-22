package msghttp_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unicode"

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

// --- Plan 024 Task 4: O1 NewOutbound / Send ---------------------------------
//
// Every branch (1-17, incl. 4b) has a mutation-sensitive case. Permanence and
// transient-wait behaviour are asserted through their OBSERVABLE consequence on
// a msgin.Producer (assertPermanent / a two-phase fake-clock advance), never via
// errors.Is on a marker (Plan 023 D6). HTTP is fully hermetic: httptest.Server,
// hand-built responses via a roundTripperFunc, no real network.

// capturedRequest records what an httptest handler saw, passed back to the test
// goroutine over a buffered channel so the -race detector sees the handoff.
type capturedRequest struct {
	method string
	body   string
	header http.Header
}

// TestNewOutbound_construction covers branch 1 (URL validation via the four
// sentinels, now reached through NewOutbound — Task 1 rows 9-12), branch 2 (a
// bad Option propagates NewConfig's error) and branch 14 (the caller's client is
// never mutated: its CheckRedirect stays nil).
func TestNewOutbound_construction(t *testing.T) {
	t.Parallel()

	type testCase struct {
		name   string
		url    string
		opts   []msghttp.Option
		assert func(t *testing.T, out *msghttp.Outbound, err error)
	}

	cases := []testCase{
		{ // branch 1: empty
			name: "empty URL -> ErrEmptyURL",
			url:  "",
			assert: func(t *testing.T, out *msghttp.Outbound, err error) {
				assert.Nil(t, out)
				assert.ErrorIs(t, err, msghttp.ErrEmptyURL)
			},
		},
		{ // branch 1: whitespace
			name: "whitespace URL -> ErrEmptyURL",
			url:  "   ",
			assert: func(t *testing.T, out *msghttp.Outbound, err error) {
				assert.Nil(t, out)
				assert.ErrorIs(t, err, msghttp.ErrEmptyURL)
			},
		},
		{ // branch 1: parse failure
			name: "unparseable URL -> ErrInvalidURL",
			url:  "://bad",
			assert: func(t *testing.T, out *msghttp.Outbound, err error) {
				assert.Nil(t, out)
				assert.ErrorIs(t, err, msghttp.ErrInvalidURL)
			},
		},
		{ // branch 1: bad scheme
			name: "non-http scheme -> ErrInvalidURL",
			url:  "file:///etc/passwd",
			assert: func(t *testing.T, out *msghttp.Outbound, err error) {
				assert.Nil(t, out)
				assert.ErrorIs(t, err, msghttp.ErrInvalidURL)
			},
		},
		{ // branch 1: empty host
			name: "empty host -> ErrInvalidURL",
			url:  "http:///path",
			assert: func(t *testing.T, out *msghttp.Outbound, err error) {
				assert.Nil(t, out)
				assert.ErrorIs(t, err, msghttp.ErrInvalidURL)
			},
		},
		{ // branch 2: a bad Option propagates NewConfig's error
			name: "a bad Option propagates NewConfig's error",
			url:  "https://example.test/hook",
			opts: []msghttp.Option{msghttp.WithMaxBodyBytes(0)},
			assert: func(t *testing.T, out *msghttp.Outbound, err error) {
				assert.Nil(t, out)
				assert.ErrorIs(t, err, msghttp.ErrInvalidMaxBodyBytes)
			},
		},
		{ // valid
			name: "a valid URL and options construct an Outbound",
			url:  "https://example.test/hook",
			assert: func(t *testing.T, out *msghttp.Outbound, err error) {
				require.NoError(t, err)
				assert.NotNil(t, out)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			out, err := msghttp.NewOutbound(tc.url, tc.opts...)
			tc.assert(t, out, err)
		})
	}

	// branch 14: the caller's *http.Client is shallow-copied, never mutated.
	t.Run("branch 14: NewOutbound does not mutate the caller's client", func(t *testing.T) {
		t.Parallel()
		client := &http.Client{}
		_, err := msghttp.NewOutbound("https://example.test/hook", msghttp.WithHTTPClient(client))
		require.NoError(t, err)
		assert.Nil(t, client.CheckRedirect,
			"the no-follow policy must be installed on a copy, leaving the caller's client untouched")
	})
}

// TestOutbound_Send_server drives Send against an httptest.Server: branch 3 (2xx
// -> nil, exactly one POST with the expected body), branch 4 (non-2xx -> the
// classified *StatusError), branch 4b (WithErrorBodyExcerpt surfaces a non-empty
// sanitized excerpt, proving classify-before-drain) and branch 15 (allow-listed
// headers reach the server, others do not).
func TestOutbound_Send_server(t *testing.T) {
	t.Parallel()

	t.Run("branch 3: 2xx returns nil after exactly one POST with the body", func(t *testing.T) {
		t.Parallel()
		var hits atomic.Int64
		capCh := make(chan capturedRequest, 1)
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			hits.Add(1)
			b, _ := io.ReadAll(r.Body)
			capCh <- capturedRequest{method: r.Method, body: string(b), header: r.Header.Clone()}
			w.WriteHeader(http.StatusOK)
		}))
		defer srv.Close()

		out, err := msghttp.NewOutbound(srv.URL)
		require.NoError(t, err)

		require.NoError(t, out.Send(t.Context(), msgin.New[any]([]byte("hello-webhook"))))
		assert.Equal(t, int64(1), hits.Load(), "exactly one POST is sent")
		cap := <-capCh
		assert.Equal(t, http.MethodPost, cap.method)
		assert.Equal(t, "hello-webhook", cap.body)
	})

	t.Run("branch 4: a non-2xx status surfaces the classified *StatusError", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		defer srv.Close()

		out, err := msghttp.NewOutbound(srv.URL)
		require.NoError(t, err)

		sendErr := out.Send(t.Context(), msgin.New[any]([]byte("x")))
		var se *msghttp.StatusError
		require.ErrorAs(t, sendErr, &se)
		assert.Equal(t, http.StatusNotFound, se.Code)
	})

	t.Run("branch 4b: WithErrorBodyExcerpt surfaces a non-empty sanitized excerpt", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = io.WriteString(w, "upstream boom: check code 42")
		}))
		defer srv.Close()

		out, err := msghttp.NewOutbound(srv.URL, msghttp.WithErrorBodyExcerpt())
		require.NoError(t, err)

		sendErr := out.Send(t.Context(), msgin.New[any]([]byte("x")))
		var se *msghttp.StatusError
		require.ErrorAs(t, sendErr, &se)
		// Non-empty ONLY because ClassifyResponse read the excerpt BEFORE
		// drainBounded consumed the body — draining first would leave "".
		assert.NotEmpty(t, se.Excerpt, "classify-before-drain must capture the error body excerpt")
		for _, r := range se.Excerpt {
			assert.True(t, unicode.IsPrint(r), "every excerpt rune must be printable, got %U", r)
		}
	})

	t.Run("branch 15: allow-listed request headers reach the server, others do not", func(t *testing.T) {
		t.Parallel()
		capCh := make(chan capturedRequest, 1)
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			capCh <- capturedRequest{header: r.Header.Clone()}
			w.WriteHeader(http.StatusOK)
		}))
		defer srv.Close()

		out, err := msghttp.NewOutbound(srv.URL, msghttp.WithOutboundHeaders("X-Allowed"))
		require.NoError(t, err)

		msg := msgin.New[any]([]byte("x")).
			WithHeader("X-Allowed", "yes").
			WithHeader("X-Secret", "leak-me")
		require.NoError(t, out.Send(t.Context(), msg))

		cap := <-capCh
		assert.Equal(t, "yes", cap.header.Get("X-Allowed"))
		assert.Empty(t, cap.header.Get("X-Secret"), "a non-allow-listed header must not be sent")
	})
}

// TestOutbound_Send_transport covers the INV-5 redaction helper via a
// roundTripperFunc: branch 5 (a transport error is redacted, transient, and
// carries no URL/credentials) and branch 6 (a cancellation is preserved through
// the wrap).
func TestOutbound_Send_transport(t *testing.T) {
	t.Parallel()

	const secretURL = "https://user:pw@example.invalid/hook?token=SECRET"

	t.Run("branch 5: transport error is redacted and stays transient", func(t *testing.T) {
		t.Parallel()
		client := &http.Client{Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("dial tcp: lookup failed")
		})}
		out, err := msghttp.NewOutbound(secretURL, msghttp.WithHTTPClient(client))
		require.NoError(t, err)

		sendErr := out.Send(t.Context(), msgin.New[any]([]byte("x")))
		require.Error(t, sendErr)
		assert.ErrorIs(t, sendErr, msghttp.ErrOutboundTransport)

		s := sendErr.Error()
		assert.NotContains(t, s, "SECRET", "the query token must not leak")
		assert.NotContains(t, s, "user", "the userinfo must not leak")
		assert.NotContains(t, s, "/hook", "the URL path must not leak")

		// Transient: NOT wrapped in msgin.Permanent, so the runtime retries it.
		assertTransientWait(t, sendErr, msgin.ExponentialBackoff{Initial: time.Second}, time.Second)
	})

	t.Run("branch 6: a cancellation is preserved through the redaction wrap", func(t *testing.T) {
		t.Parallel()
		client := &http.Client{Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
			return nil, context.Canceled
		})}
		out, err := msghttp.NewOutbound(secretURL, msghttp.WithHTTPClient(client))
		require.NoError(t, err)

		sendErr := out.Send(t.Context(), msgin.New[any]([]byte("x")))
		require.Error(t, sendErr)
		assert.ErrorIs(t, sendErr, context.Canceled, "the %w on ue.Err must preserve context.Canceled")
		assert.ErrorIs(t, sendErr, msghttp.ErrOutboundTransport)
		assert.NotContains(t, sendErr.Error(), "SECRET")
	})
}

// TestOutbound_Send_encodeFailure covers branch 7: a non-[]byte/string payload
// fails EncodeRequest, so NO request is sent (server count == 0) and the error
// is Permanent (attempt count == 1 through a Producer, via assertPermanent).
func TestOutbound_Send_encodeFailure(t *testing.T) {
	t.Parallel()

	var hits atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	out, err := msghttp.NewOutbound(srv.URL)
	require.NoError(t, err)

	sendErr := out.Send(t.Context(), msgin.New[any](42)) // int payload: unsupported
	require.Error(t, sendErr)
	assert.ErrorIs(t, sendErr, msghttp.ErrUnsupportedPayload)
	assert.Equal(t, int64(0), hits.Load(), "no request may be sent when encoding fails")
	assertPermanent(t, sendErr)
}

// TestOutbound_Send_bodyLifecycle covers branches 8/9 (the body is drained AND
// closed on both the 2xx and non-2xx paths) and branch 10 (the reuse-drain is
// bounded by WithMaxResponseBytes). A trackingBody injected via a roundTripperFunc
// records the reads and the single deferred Close.
func TestOutbound_Send_bodyLifecycle(t *testing.T) {
	t.Parallel()

	clientReturning := func(code int, body *trackingBody) *http.Client {
		return &http.Client{Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: code, Header: http.Header{}, Body: body}, nil
		})}
	}

	t.Run("branch 8: body drained and closed on the 2xx path", func(t *testing.T) {
		t.Parallel()
		body := &trackingBody{r: strings.NewReader("response-body")}
		out, err := msghttp.NewOutbound("https://example.test/hook",
			msghttp.WithHTTPClient(clientReturning(http.StatusOK, body)))
		require.NoError(t, err)

		require.NoError(t, out.Send(t.Context(), msgin.New[any]([]byte("x"))))
		assert.True(t, body.closed.Load(), "the deferred Close must run on the 2xx path")
		assert.Positive(t, body.read.Load(), "the body must be drained for connection reuse")
	})

	t.Run("branch 9: body drained and closed on the non-2xx path", func(t *testing.T) {
		t.Parallel()
		body := &trackingBody{r: strings.NewReader("error-body")}
		out, err := msghttp.NewOutbound("https://example.test/hook",
			msghttp.WithHTTPClient(clientReturning(http.StatusInternalServerError, body)))
		require.NoError(t, err)

		require.Error(t, out.Send(t.Context(), msgin.New[any]([]byte("x"))))
		assert.True(t, body.closed.Load(), "the deferred Close must run on the non-2xx path")
		assert.Positive(t, body.read.Load(), "the body must be drained even on an error status")
	})

	t.Run("branch 10: the reuse-drain is bounded by WithMaxResponseBytes", func(t *testing.T) {
		t.Parallel()
		body := &trackingBody{r: strings.NewReader(strings.Repeat("A", 4096))}
		out, err := msghttp.NewOutbound("https://example.test/hook",
			msghttp.WithHTTPClient(clientReturning(http.StatusOK, body)),
			msghttp.WithMaxResponseBytes(16))
		require.NoError(t, err)

		require.NoError(t, out.Send(t.Context(), msgin.New[any]([]byte("x"))))
		assert.LessOrEqual(t, body.read.Load(), int64(16),
			"drainBounded must read at most maxResponseBytes from an oversized body")
	})
}

// TestOutbound_Send_redirects is the INV-1 SSRF regression: branch 11 (the
// default no-follow policy stops a 302 -> the redirect target is never hit and
// the 302 classifies Permanent), branch 12 (WithFollowRedirects opts back in),
// and branch 13 (a caller client with its own CheckRedirect is honoured verbatim).
func TestOutbound_Send_redirects(t *testing.T) {
	t.Parallel()

	// newRedirectPair returns a first server that 302s to a second, and a counter
	// of hits on the second (the redirect target that MUST NOT be reached).
	newRedirectPair := func(t *testing.T) (firstURL string, secondHits *atomic.Int64) {
		t.Helper()
		var hits atomic.Int64
		second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			hits.Add(1)
			w.WriteHeader(http.StatusOK)
		}))
		t.Cleanup(second.Close)
		first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, second.URL, http.StatusFound)
		}))
		t.Cleanup(first.Close)
		return first.URL, &hits
	}

	t.Run("branch 11: the default policy does not follow a 302", func(t *testing.T) {
		t.Parallel()
		firstURL, secondHits := newRedirectPair(t)
		out, err := msghttp.NewOutbound(firstURL)
		require.NoError(t, err)

		sendErr := out.Send(t.Context(), msgin.New[any]([]byte("x")))
		var se *msghttp.StatusError
		require.ErrorAs(t, sendErr, &se)
		assert.Equal(t, http.StatusFound, se.Code, "the 302 itself is returned, not the target's 200")
		assert.Equal(t, int64(0), secondHits.Load(), "the redirect target MUST NOT be reached (SSRF)")
		assertPermanent(t, sendErr) // a 3xx classifies Permanent
	})

	t.Run("branch 12: WithFollowRedirects opts back into following", func(t *testing.T) {
		t.Parallel()
		firstURL, secondHits := newRedirectPair(t)
		out, err := msghttp.NewOutbound(firstURL, msghttp.WithFollowRedirects())
		require.NoError(t, err)

		require.NoError(t, out.Send(t.Context(), msgin.New[any]([]byte("x"))))
		assert.Equal(t, int64(1), secondHits.Load(), "opting in must follow the redirect to the target")
	})

	t.Run("branch 13: a caller client's own CheckRedirect is honoured verbatim", func(t *testing.T) {
		t.Parallel()
		firstURL, secondHits := newRedirectPair(t)
		var policyCalls atomic.Int64
		client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
			policyCalls.Add(1)
			return http.ErrUseLastResponse // the caller's own no-follow choice
		}}
		out, err := msghttp.NewOutbound(firstURL, msghttp.WithHTTPClient(client))
		require.NoError(t, err)

		sendErr := out.Send(t.Context(), msgin.New[any]([]byte("x")))
		var se *msghttp.StatusError
		require.ErrorAs(t, sendErr, &se)
		assert.Positive(t, policyCalls.Load(), "the caller's CheckRedirect must be invoked")
		assert.Equal(t, int64(0), secondHits.Load(), "the caller's policy (no-follow) is observed")
	})
}

// TestOutbound_Send_endToEnd wires the real Outbound behind a msgin.Producer with
// WithProducerRetry: branch 16 (a 503 is retried then succeeds) and branch 17 (a
// 429 + Retry-After: 30 waits 30s because the server minimum exceeds the 1s
// computed backoff — max(computed, min(server, cap))). BytesPayloadCodec is
// paired explicitly: NewProducer[[]byte] defaults to JSON, which would base64 the
// body. Two-phase fake-clock advance detects under-waiting.
func TestOutbound_Send_endToEnd(t *testing.T) {
	t.Parallel()

	// retryThenOK builds a server whose FIRST hit returns firstCode (with optional
	// Retry-After) and every later hit returns 200; hits counts total requests.
	retryThenOK := func(t *testing.T, firstCode int, retryAfter string) (url string, hits *atomic.Int64) {
		t.Helper()
		var h atomic.Int64
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			if h.Add(1) == 1 {
				if retryAfter != "" {
					w.Header().Set("Retry-After", retryAfter)
				}
				w.WriteHeader(firstCode)
				return
			}
			w.WriteHeader(http.StatusOK)
		}))
		t.Cleanup(srv.Close)
		return srv.URL, &h
	}

	// runRetry sends through a retrying Producer on a fake clock and asserts the
	// effective wait via a two-phase advance (wantWait-1ns still parked, +1ns
	// returns), then that the second attempt succeeded.
	runRetry := func(t *testing.T, url string, hits *atomic.Int64, wantWait time.Duration) {
		t.Helper()
		clk := clockwork.NewFakeClock()
		out, err := msghttp.NewOutbound(url)
		require.NoError(t, err)
		policy := msgin.RetryPolicy{Backoff: msgin.ExponentialBackoff{Initial: time.Second}}
		prod, err := msgin.NewProducer[[]byte](out,
			msgin.WithProducerCodec[[]byte](msgin.BytesPayloadCodec{}),
			msgin.WithProducerRetry[[]byte](policy),
			msgin.WithProducerClock[[]byte](clk),
		)
		require.NoError(t, err)

		ctx := t.Context()
		done := make(chan error, 1)
		go func() { done <- prod.Send(ctx, msgin.New[[]byte]([]byte("event"))) }()

		blockCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		require.NoError(t, clk.BlockUntilContext(blockCtx, 1), "producer never parked on a timer")

		clk.Advance(wantWait - time.Nanosecond)
		select {
		case <-done:
			t.Fatalf("Send returned before the full %s wait (under-waited)", wantWait)
		case <-time.After(20 * time.Millisecond):
		}
		clk.Advance(time.Nanosecond)
		select {
		case sendErr := <-done:
			require.NoError(t, sendErr, "the retry succeeds on the second attempt")
		case <-time.After(5 * time.Second):
			t.Fatalf("Send did not return after the %s wait", wantWait)
		}
		assert.Equal(t, int64(2), hits.Load(), "the failing attempt is retried exactly once")
	}

	t.Run("branch 16: a 503 is retried after the computed backoff, then succeeds", func(t *testing.T) {
		t.Parallel()
		url, hits := retryThenOK(t, http.StatusServiceUnavailable, "")
		runRetry(t, url, hits, time.Second)
	})

	t.Run("branch 17: a 429 + Retry-After: 30 waits 30s (server minimum > 1s backoff)", func(t *testing.T) {
		t.Parallel()
		url, hits := retryThenOK(t, http.StatusTooManyRequests, "30")
		runRetry(t, url, hits, 30*time.Second)
	})
}
