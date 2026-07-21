package msghttp_test

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"

	"github.com/kartaladev/msgin"
	msghttp "github.com/kartaladev/msgin/adapter/http"
)

func TestMain(m *testing.M) { goleak.VerifyTestMain(m) }

// errReader always fails on Read with a plain (non-*http.MaxBytesError) error,
// simulating a non-oversize body-read failure (e.g. a client that hangs up
// mid-body) distinct from the WithMaxBodyBytes overflow case.
type errReader struct{}

func (errReader) Read([]byte) (int, error) {
	return 0, errors.New("errReader: simulated read failure")
}

func TestNewConfig_validation(t *testing.T) {
	t.Parallel()

	type testCase struct {
		name   string
		opts   []msghttp.Option
		assert func(t *testing.T, cfg *msghttp.Config, err error)
	}

	cases := []testCase{
		{
			name: "default no opts is valid",
			assert: func(t *testing.T, cfg *msghttp.Config, err error) {
				require.NoError(t, err)
				require.NotNil(t, cfg)
			},
		},
		{
			name: "WithMaxBodyBytes(0) is invalid",
			opts: []msghttp.Option{msghttp.WithMaxBodyBytes(0)},
			assert: func(t *testing.T, cfg *msghttp.Config, err error) {
				assert.Nil(t, cfg)
				assert.ErrorIs(t, err, msghttp.ErrInvalidMaxBodyBytes)
			},
		},
		{
			name: "WithMaxBodyBytes(-1) is invalid",
			opts: []msghttp.Option{msghttp.WithMaxBodyBytes(-1)},
			assert: func(t *testing.T, cfg *msghttp.Config, err error) {
				assert.Nil(t, cfg)
				assert.ErrorIs(t, err, msghttp.ErrInvalidMaxBodyBytes)
			},
		},
		{
			name: "WithSuccessStatus(99) is invalid",
			opts: []msghttp.Option{msghttp.WithSuccessStatus(99)},
			assert: func(t *testing.T, cfg *msghttp.Config, err error) {
				assert.Nil(t, cfg)
				assert.ErrorIs(t, err, msghttp.ErrInvalidStatusCode)
			},
		},
		{
			name: "WithSuccessStatus(600) is invalid",
			opts: []msghttp.Option{msghttp.WithSuccessStatus(600)},
			assert: func(t *testing.T, cfg *msghttp.Config, err error) {
				assert.Nil(t, cfg)
				assert.ErrorIs(t, err, msghttp.ErrInvalidStatusCode)
			},
		},
		{
			name: "valid overrides are accepted",
			opts: []msghttp.Option{
				msghttp.WithMaxBodyBytes(2048),
				msghttp.WithSuccessStatus(http.StatusCreated),
				msghttp.WithRequestHeaders("X-Test"),
				msghttp.WithResponseHeaders("X-Reply"),
				msghttp.WithCorrelationID(func(*http.Request) string { return "" }),
				msghttp.WithErrorStatus(func(error) int { return http.StatusTeapot }),
				msghttp.WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))),
			},
			assert: func(t *testing.T, cfg *msghttp.Config, err error) {
				require.NoError(t, err)
				require.NotNil(t, cfg)
			},
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

func TestDecodeRequest(t *testing.T) {
	t.Parallel()

	type testCase struct {
		name    string
		opts    []msghttp.Option
		request func() *http.Request
		assert  func(t *testing.T, msg msgin.Message[any], err error)
	}

	cases := []testCase{
		{
			name: "body becomes []byte payload",
			request: func() *http.Request {
				return httptest.NewRequest(http.MethodPost, "/", strings.NewReader("hello world"))
			},
			assert: func(t *testing.T, msg msgin.Message[any], err error) {
				require.NoError(t, err)
				assert.Equal(t, []byte("hello world"), msg.Payload())
			},
		},
		{
			name: "oversize body maps to *http.MaxBytesError",
			opts: []msghttp.Option{msghttp.WithMaxBodyBytes(4)},
			request: func() *http.Request {
				return httptest.NewRequest(http.MethodPost, "/", strings.NewReader("hello"))
			},
			assert: func(t *testing.T, _ msgin.Message[any], err error) {
				require.Error(t, err)
				var maxErr *http.MaxBytesError
				assert.True(t, errors.As(err, &maxErr), "expected err to wrap *http.MaxBytesError")
				assert.NotEmpty(t, err.Error())
			},
		},
		{
			name: "non-oversize read error is distinct from the oversize arm",
			request: func() *http.Request {
				return httptest.NewRequest(http.MethodPost, "/", errReader{})
			},
			assert: func(t *testing.T, _ msgin.Message[any], err error) {
				require.Error(t, err)
				var maxErr *http.MaxBytesError
				assert.False(t, errors.As(err, &maxErr), "a plain read error must NOT be classified as *http.MaxBytesError")
				assert.Contains(t, err.Error(), "errReader")
			},
		},
		{
			name: "Content-Type maps to the NON-reserved http.content-type header",
			request: func() *http.Request {
				req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("{}"))
				req.Header.Set("Content-Type", "application/json")
				return req
			},
			assert: func(t *testing.T, msg msgin.Message[any], err error) {
				require.NoError(t, err)
				ct, ok := msg.Headers().String("http.content-type")
				require.True(t, ok)
				assert.Equal(t, "application/json", ct)
				_, ok = msg.Header(msgin.HeaderContentType)
				assert.False(t, ok, "the client's Content-Type must never land in the reserved msgin. namespace")
			},
		},
		{
			name: "a nil request body decodes to an empty payload without panicking",
			request: func() *http.Request {
				req := httptest.NewRequest(http.MethodPost, "/", nil)
				req.Body = nil
				return req
			},
			assert: func(t *testing.T, msg msgin.Message[any], err error) {
				require.NoError(t, err)
				assert.Empty(t, msg.Payload())
			},
		},
		{
			name: "client msgin.delivery-count header is stripped even when allow-listed",
			opts: []msghttp.Option{msghttp.WithRequestHeaders(msgin.HeaderDeliveryCount)},
			request: func() *http.Request {
				req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("x"))
				req.Header.Set(msgin.HeaderDeliveryCount, "999")
				return req
			},
			assert: func(t *testing.T, msg msgin.Message[any], err error) {
				require.NoError(t, err)
				_, ok := msg.Header(msgin.HeaderDeliveryCount)
				assert.False(t, ok, "a client-forged reserved header must never survive into the message")
			},
		},
		{
			name: "allow-listed header copied, non-allow-listed header absent",
			opts: []msghttp.Option{msghttp.WithRequestHeaders("X-Custom")},
			request: func() *http.Request {
				req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("x"))
				req.Header.Set("X-Custom", "value1")
				req.Header.Set("X-Other", "value2")
				return req
			},
			assert: func(t *testing.T, msg msgin.Message[any], err error) {
				require.NoError(t, err)
				v, ok := msg.Header("X-Custom")
				require.True(t, ok)
				assert.Equal(t, "value1", v)
				_, ok = msg.Header("X-Other")
				assert.False(t, ok)
			},
		},
		{
			name: "default correlation id equals msg.ID()",
			request: func() *http.Request {
				return httptest.NewRequest(http.MethodPost, "/", strings.NewReader("x"))
			},
			assert: func(t *testing.T, msg msgin.Message[any], err error) {
				require.NoError(t, err)
				cid, ok := msg.Header(msgin.HeaderCorrelationID)
				require.True(t, ok)
				assert.Equal(t, msg.ID(), cid)
			},
		},
		{
			name: "WithCorrelationID populates http.correlation-id but NOT the exchange key",
			opts: []msghttp.Option{msghttp.WithCorrelationID(func(*http.Request) string { return "client-supplied" })},
			request: func() *http.Request {
				return httptest.NewRequest(http.MethodPost, "/", strings.NewReader("x"))
			},
			assert: func(t *testing.T, msg msgin.Message[any], err error) {
				require.NoError(t, err)
				cid, ok := msg.Header(msgin.HeaderCorrelationID)
				require.True(t, ok)
				assert.Equal(t, msg.ID(), cid, "the exchange correlation key must stay server-minted")
				advisory, ok := msg.Header("http.correlation-id")
				require.True(t, ok)
				assert.Equal(t, "client-supplied", advisory)
			},
		},
		{
			name: "WithCorrelationID(nil) after a real resolver does not clobber it",
			opts: []msghttp.Option{
				msghttp.WithCorrelationID(func(*http.Request) string { return "custom-id" }),
				msghttp.WithCorrelationID(nil),
			},
			request: func() *http.Request {
				return httptest.NewRequest(http.MethodPost, "/", strings.NewReader("x"))
			},
			assert: func(t *testing.T, msg msgin.Message[any], err error) {
				require.NoError(t, err)
				advisory, ok := msg.Header("http.correlation-id")
				require.True(t, ok)
				assert.Equal(t, "custom-id", advisory, "a later WithCorrelationID(nil) must be a no-op, not clobber the earlier resolver")
			},
		},
		{
			name: "WithCorrelationID returning empty adds no advisory header",
			opts: []msghttp.Option{msghttp.WithCorrelationID(func(*http.Request) string { return "" })},
			request: func() *http.Request {
				return httptest.NewRequest(http.MethodPost, "/", strings.NewReader("x"))
			},
			assert: func(t *testing.T, msg msgin.Message[any], err error) {
				require.NoError(t, err)
				cid, ok := msg.Header(msgin.HeaderCorrelationID)
				require.True(t, ok)
				assert.Equal(t, msg.ID(), cid)
				_, ok = msg.Header("http.correlation-id")
				assert.False(t, ok)
			},
		},
		{
			name: "WithTrustedCorrelationID makes the client value the exchange key",
			opts: []msghttp.Option{msghttp.WithTrustedCorrelationID(func(*http.Request) string { return "trusted-id" })},
			request: func() *http.Request {
				return httptest.NewRequest(http.MethodPost, "/", strings.NewReader("x"))
			},
			assert: func(t *testing.T, msg msgin.Message[any], err error) {
				require.NoError(t, err)
				cid, ok := msg.Header(msgin.HeaderCorrelationID)
				require.True(t, ok)
				assert.Equal(t, "trusted-id", cid)
			},
		},
		{
			name: "WithTrustedCorrelationID returning empty falls back to msg.ID()",
			opts: []msghttp.Option{msghttp.WithTrustedCorrelationID(func(*http.Request) string { return "" })},
			request: func() *http.Request {
				return httptest.NewRequest(http.MethodPost, "/", strings.NewReader("x"))
			},
			assert: func(t *testing.T, msg msgin.Message[any], err error) {
				require.NoError(t, err)
				cid, ok := msg.Header(msgin.HeaderCorrelationID)
				require.True(t, ok)
				assert.Equal(t, msg.ID(), cid)
			},
		},
		{
			name: "WithTrustedCorrelationID(nil) after a real resolver does not clobber it",
			opts: []msghttp.Option{
				msghttp.WithTrustedCorrelationID(func(*http.Request) string { return "trusted-id" }),
				msghttp.WithTrustedCorrelationID(nil),
			},
			request: func() *http.Request {
				return httptest.NewRequest(http.MethodPost, "/", strings.NewReader("x"))
			},
			assert: func(t *testing.T, msg msgin.Message[any], err error) {
				require.NoError(t, err)
				cid, ok := msg.Header(msgin.HeaderCorrelationID)
				require.True(t, ok)
				assert.Equal(t, "trusted-id", cid)
			},
		},
		{
			name: "both correlation options set are orthogonal: trusted keys the exchange, the other is advisory only",
			opts: []msghttp.Option{
				msghttp.WithCorrelationID(func(*http.Request) string { return "advisory-id" }),
				msghttp.WithTrustedCorrelationID(func(*http.Request) string { return "trusted-id" }),
			},
			request: func() *http.Request {
				return httptest.NewRequest(http.MethodPost, "/", strings.NewReader("x"))
			},
			assert: func(t *testing.T, msg msgin.Message[any], err error) {
				require.NoError(t, err)
				cid, ok := msg.Header(msgin.HeaderCorrelationID)
				require.True(t, ok)
				assert.Equal(t, "trusted-id", cid)
				advisory, ok := msg.Header("http.correlation-id")
				require.True(t, ok)
				assert.Equal(t, "advisory-id", advisory)
			},
		},
		{
			name: "a reserved allow-list entry is stripped case-INSENSITIVELY",
			opts: []msghttp.Option{msghttp.WithRequestHeaders("MSGIN.delivery-count")},
			request: func() *http.Request {
				req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("x"))
				req.Header.Set("MSGIN.delivery-count", "999")
				return req
			},
			assert: func(t *testing.T, msg msgin.Message[any], err error) {
				require.NoError(t, err)
				_, ok := msg.Header("MSGIN.delivery-count")
				assert.False(t, ok, "a differently-cased reserved allow-list entry must still be stripped")
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cfg, err := msghttp.NewConfig(tc.opts...)
			require.NoError(t, err)

			msg, err := msghttp.DecodeRequest(tc.request(), cfg)
			tc.assert(t, msg, err)
		})
	}
}

func TestEncodeResponse(t *testing.T) {
	t.Parallel()

	type testCase struct {
		name   string
		opts   []msghttp.Option
		msg    func() msgin.Message[any]
		assert func(t *testing.T, rec *httptest.ResponseRecorder, err error)
	}

	cases := []testCase{
		{
			name: "[]byte payload writes body and 200",
			msg: func() msgin.Message[any] {
				return msgin.New[any]([]byte("hello"))
			},
			assert: func(t *testing.T, rec *httptest.ResponseRecorder, err error) {
				require.NoError(t, err)
				assert.Equal(t, http.StatusOK, rec.Code)
				assert.Equal(t, "hello", rec.Body.String())
			},
		},
		{
			name: "string payload writes body",
			msg: func() msgin.Message[any] {
				return msgin.New[any]("hello-string")
			},
			assert: func(t *testing.T, rec *httptest.ResponseRecorder, err error) {
				require.NoError(t, err)
				assert.Equal(t, http.StatusOK, rec.Code)
				assert.Equal(t, "hello-string", rec.Body.String())
			},
		},
		{
			name: "non-bytes payload errors and writes nothing",
			msg: func() msgin.Message[any] {
				return msgin.New[any](42)
			},
			assert: func(t *testing.T, rec *httptest.ResponseRecorder, err error) {
				assert.ErrorIs(t, err, msghttp.ErrUnsupportedPayload)
				assert.Empty(t, rec.Header(), "no header may be written before the payload is validated")
				assert.Empty(t, rec.Body.String(), "no body may be written on ErrUnsupportedPayload")
			},
		},
		{
			name: "allow-listed response header is emitted",
			opts: []msghttp.Option{msghttp.WithResponseHeaders("X-Reply")},
			msg: func() msgin.Message[any] {
				return msgin.New[any]([]byte("ok")).WithHeader("X-Reply", "value1")
			},
			assert: func(t *testing.T, rec *httptest.ResponseRecorder, err error) {
				require.NoError(t, err)
				assert.Equal(t, "value1", rec.Header().Get("X-Reply"))
			},
		},
		{
			name: "allow-listed response header with a non-string value is silently skipped",
			opts: []msghttp.Option{msghttp.WithResponseHeaders("X-Reply")},
			msg: func() msgin.Message[any] {
				return msgin.New[any]([]byte("ok")).WithHeader("X-Reply", 42)
			},
			assert: func(t *testing.T, rec *httptest.ResponseRecorder, err error) {
				require.NoError(t, err)
				assert.Empty(t, rec.Header().Get("X-Reply"))
				_, ok := rec.Header()["X-Reply"]
				assert.False(t, ok, "a non-string allow-listed header value must not be emitted at all")
			},
		},
		{
			name: "response header value is CRLF-sanitized",
			opts: []msghttp.Option{msghttp.WithResponseHeaders("X-Reply")},
			msg: func() msgin.Message[any] {
				return msgin.New[any]([]byte("ok")).WithHeader("X-Reply", "line1\r\nInjected: evil")
			},
			assert: func(t *testing.T, rec *httptest.ResponseRecorder, err error) {
				require.NoError(t, err)
				v := rec.Header().Get("X-Reply")
				assert.NotContains(t, v, "\r")
				assert.NotContains(t, v, "\n")
			},
		},
		{
			name: "msgin.HeaderContentType maps to Content-Type",
			msg: func() msgin.Message[any] {
				return msgin.New[any]([]byte("ok")).WithHeader(msgin.HeaderContentType, "application/json")
			},
			assert: func(t *testing.T, rec *httptest.ResponseRecorder, err error) {
				require.NoError(t, err)
				assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
				assert.Equal(t, "nosniff", rec.Header().Get("X-Content-Type-Options"))
			},
		},
		{
			name: "a reply with no msgin.content-type gets application/octet-stream plus nosniff",
			msg: func() msgin.Message[any] {
				return msgin.New[any]([]byte("<script>alert(1)</script>"))
			},
			assert: func(t *testing.T, rec *httptest.ResponseRecorder, err error) {
				require.NoError(t, err)
				assert.Equal(t, "application/octet-stream", rec.Header().Get("Content-Type"),
					"Go must never be allowed to sniff text/html out of a flow-controlled body")
				assert.Equal(t, "nosniff", rec.Header().Get("X-Content-Type-Options"))
			},
		},
		{
			name: "an empty msgin.content-type falls back to application/octet-stream",
			msg: func() msgin.Message[any] {
				return msgin.New[any]([]byte("ok")).WithHeader(msgin.HeaderContentType, "")
			},
			assert: func(t *testing.T, rec *httptest.ResponseRecorder, err error) {
				require.NoError(t, err)
				assert.Equal(t, "application/octet-stream", rec.Header().Get("Content-Type"))
			},
		},
		{
			name: "an allow-listed X-Content-Type-Options cannot override nosniff",
			opts: []msghttp.Option{msghttp.WithResponseHeaders("X-Content-Type-Options")},
			msg: func() msgin.Message[any] {
				return msgin.New[any]([]byte("ok")).WithHeader("X-Content-Type-Options", "sniff-me")
			},
			assert: func(t *testing.T, rec *httptest.ResponseRecorder, err error) {
				require.NoError(t, err)
				assert.Equal(t, "nosniff", rec.Header().Get("X-Content-Type-Options"))
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cfg, err := msghttp.NewConfig(tc.opts...)
			require.NoError(t, err)

			rec := httptest.NewRecorder()
			err = msghttp.EncodeResponse(rec, tc.msg(), cfg)
			tc.assert(t, rec, err)
		})
	}
}

// failingWriter is an http.ResponseWriter whose Write always fails (simulating
// a client that hangs up mid-response) and which records EVERY WriteHeader
// code it is handed, so a test can prove the response status line is written
// exactly once.
type failingWriter struct {
	headers http.Header
	codes   []int
}

func (f *failingWriter) Header() http.Header {
	if f.headers == nil {
		f.headers = http.Header{}
	}
	return f.headers
}

func (f *failingWriter) Write([]byte) (int, error) {
	return 0, errors.New("failingWriter: connection reset by peer")
}

func (f *failingWriter) WriteHeader(code int) { f.codes = append(f.codes, code) }

// panicOnWriteWriter is an http.ResponseWriter that panics from Write, i.e.
// AFTER the 200 status line has already gone out — the one shape in which the
// handler cores' recover() runs on an already-committed response and must
// therefore suppress its 500 rather than restate the status.
type panicOnWriteWriter struct {
	failingWriter
}

func (p *panicOnWriteWriter) Write([]byte) (int, error) {
	panic("panicOnWriteWriter: transport exploded mid-body")
}

func TestEncodeResponse_writeFailureWrapsErrWriteResponse(t *testing.T) {
	t.Parallel()

	cfg, err := msghttp.NewConfig()
	require.NoError(t, err)

	w := &failingWriter{}
	err = msghttp.EncodeResponse(w, msgin.New[any]([]byte("ok")), cfg)

	require.Error(t, err)
	assert.ErrorIs(t, err, msghttp.ErrWriteResponse,
		"a body-write failure must be distinguishable from a pre-write failure: the response is already committed")
	assert.NotErrorIs(t, err, msghttp.ErrUnsupportedPayload)
	assert.Equal(t, []int{http.StatusOK}, w.codes)
}

func TestWithRequestResponseHeaders_clonesTheCallerSlice(t *testing.T) {
	t.Parallel()

	requestAllow := []string{"X-Allowed"}
	responseAllow := []string{"X-Reply"}

	cfg, err := msghttp.NewConfig(
		msghttp.WithRequestHeaders(requestAllow...),
		msghttp.WithResponseHeaders(responseAllow...),
	)
	require.NoError(t, err)

	// A caller mutating its own slice after NewConfig must not rewrite the
	// adapter's security allow-lists.
	requestAllow[0] = msgin.HeaderDeliveryCount
	responseAllow[0] = msgin.HeaderCorrelationID

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("x"))
	req.Header.Set("X-Allowed", "kept")
	msg, err := msghttp.DecodeRequest(req, cfg)
	require.NoError(t, err)
	v, ok := msg.Header("X-Allowed")
	require.True(t, ok)
	assert.Equal(t, "kept", v)

	rec := httptest.NewRecorder()
	reply := msgin.New[any]([]byte("ok")).WithHeader("X-Reply", "emitted")
	require.NoError(t, msghttp.EncodeResponse(rec, reply, cfg))
	assert.Equal(t, "emitted", rec.Header().Get("X-Reply"))
	assert.Empty(t, rec.Header().Get(msgin.HeaderCorrelationID))
}

func TestDefaultErrorStatus(t *testing.T) {
	t.Parallel()

	type testCase struct {
		name   string
		err    error
		assert func(t *testing.T, status int)
	}

	cases := []testCase{
		{
			name:   "ErrReplyTimeout is 504",
			err:    msgin.ErrReplyTimeout,
			assert: func(t *testing.T, status int) { assert.Equal(t, http.StatusGatewayTimeout, status) },
		},
		{
			name:   "ErrGatewayClosed is 503",
			err:    msgin.ErrGatewayClosed,
			assert: func(t *testing.T, status int) { assert.Equal(t, http.StatusServiceUnavailable, status) },
		},
		{
			name:   "ErrDuplicateCorrelation is 409",
			err:    msgin.ErrDuplicateCorrelation,
			assert: func(t *testing.T, status int) { assert.Equal(t, http.StatusConflict, status) },
		},
		{
			name:   "ErrNoCorrelation is 500",
			err:    msgin.ErrNoCorrelation,
			assert: func(t *testing.T, status int) { assert.Equal(t, http.StatusInternalServerError, status) },
		},
		{
			name:   "ErrUnsupportedPayload is 500",
			err:    msghttp.ErrUnsupportedPayload,
			assert: func(t *testing.T, status int) { assert.Equal(t, http.StatusInternalServerError, status) },
		},
		{
			name:   "ErrWriteResponse is 500",
			err:    msghttp.ErrWriteResponse,
			assert: func(t *testing.T, status int) { assert.Equal(t, http.StatusInternalServerError, status) },
		},
		{
			name:   "an oversize body is 413",
			err:    &http.MaxBytesError{Limit: 4},
			assert: func(t *testing.T, status int) { assert.Equal(t, http.StatusRequestEntityTooLarge, status) },
		},
		{
			name:   "a decode failure is 400",
			err:    fmt.Errorf("%w: %w", msghttp.ErrDecodeRequest, errors.New("read: boom")),
			assert: func(t *testing.T, status int) { assert.Equal(t, http.StatusBadRequest, status) },
		},
		{
			name:   "an unclassified error is 500",
			err:    errors.New("unclassified"),
			assert: func(t *testing.T, status int) { assert.Equal(t, http.StatusInternalServerError, status) },
		},
		{
			name:   "a nil error is 500",
			err:    nil,
			assert: func(t *testing.T, status int) { assert.Equal(t, http.StatusInternalServerError, status) },
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tc.assert(t, msghttp.DefaultErrorStatus(tc.err))
		})
	}
}
