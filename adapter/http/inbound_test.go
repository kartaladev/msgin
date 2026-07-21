package msghttp_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kartaladev/msgin"
	msghttp "github.com/kartaladev/msgin/adapter/http"
)

// errBoom is a plain, non-sentinel error simulating a downstream
// target.Send failure — distinct from any msgin.* sentinel so it exercises
// defaultErrorStatus's unclassified-error arm (500) unless a custom
// WithErrorStatus mapper overrides it.
var errBoom = errors.New("boom: send failed")

// fakeExchange is a minimal msgin.RequestReplyExchange test double that
// returns a canned reply/err from every Exchange call, used to drive
// ServeGateway's error->status mapping for each gateway sentinel without
// wiring a full ChannelExchange round-trip per case.
type fakeExchange struct {
	reply msgin.Message[any]
	err   error
}

func (f fakeExchange) Exchange(context.Context, msgin.Message[any]) (msgin.Message[any], error) {
	return f.reply, f.err
}

// panicExchange is a msgin.RequestReplyExchange whose Exchange panics if
// called — used to prove ServeGateway short-circuits on a DecodeRequest
// failure and never reaches the exchange.
type panicExchange struct{}

func (panicExchange) Exchange(context.Context, msgin.Message[any]) (msgin.Message[any], error) {
	panic("ServeGateway must not call Exchange on a decode error")
}

func TestServeAsync(t *testing.T) {
	t.Parallel()

	type testCase struct {
		name       string
		opts       []msghttp.Option
		request    func() *http.Request
		handlerErr error
		assert     func(t *testing.T, rec *httptest.ResponseRecorder, sendCalled bool, captured msgin.Message[any])
	}

	cases := []testCase{
		{
			name: "success returns default 202 and target receives the decoded message",
			request: func() *http.Request {
				return httptest.NewRequest(http.MethodPost, "/", strings.NewReader("hello world"))
			},
			assert: func(t *testing.T, rec *httptest.ResponseRecorder, sendCalled bool, captured msgin.Message[any]) {
				assert.Equal(t, http.StatusAccepted, rec.Code)
				require.True(t, sendCalled)
				assert.Equal(t, []byte("hello world"), captured.Payload())
				cid, ok := captured.Header(msgin.HeaderCorrelationID)
				require.True(t, ok)
				assert.NotEmpty(t, cid)
			},
		},
		{
			name: "WithSuccessStatus(201) success returns 201",
			opts: []msghttp.Option{msghttp.WithSuccessStatus(http.StatusCreated)},
			request: func() *http.Request {
				return httptest.NewRequest(http.MethodPost, "/", strings.NewReader("x"))
			},
			assert: func(t *testing.T, rec *httptest.ResponseRecorder, sendCalled bool, _ msgin.Message[any]) {
				assert.Equal(t, http.StatusCreated, rec.Code)
				assert.True(t, sendCalled)
			},
		},
		{
			name:       "Send error returns 500",
			handlerErr: errBoom,
			request: func() *http.Request {
				return httptest.NewRequest(http.MethodPost, "/", strings.NewReader("x"))
			},
			assert: func(t *testing.T, rec *httptest.ResponseRecorder, sendCalled bool, _ msgin.Message[any]) {
				assert.Equal(t, http.StatusInternalServerError, rec.Code)
				assert.True(t, sendCalled)
			},
		},
		{
			name:       "WithErrorStatus custom mapper overrides a Send error to 418",
			opts:       []msghttp.Option{msghttp.WithErrorStatus(func(error) int { return http.StatusTeapot })},
			handlerErr: errBoom,
			request: func() *http.Request {
				return httptest.NewRequest(http.MethodPost, "/", strings.NewReader("x"))
			},
			assert: func(t *testing.T, rec *httptest.ResponseRecorder, sendCalled bool, _ msgin.Message[any]) {
				assert.Equal(t, http.StatusTeapot, rec.Code)
				assert.True(t, sendCalled)
			},
		},
		{
			name: "WithErrorStatus(nil) after a real mapper does not clobber it",
			opts: []msghttp.Option{
				msghttp.WithErrorStatus(func(error) int { return http.StatusTeapot }),
				msghttp.WithErrorStatus(nil),
			},
			handlerErr: errBoom,
			request: func() *http.Request {
				return httptest.NewRequest(http.MethodPost, "/", strings.NewReader("x"))
			},
			assert: func(t *testing.T, rec *httptest.ResponseRecorder, sendCalled bool, _ msgin.Message[any]) {
				assert.Equal(t, http.StatusTeapot, rec.Code, "a later WithErrorStatus(nil) must be a no-op, not clobber the earlier mapper")
				assert.True(t, sendCalled)
			},
		},
		{
			name: "oversize body returns 413 and never reaches the target",
			opts: []msghttp.Option{msghttp.WithMaxBodyBytes(4)},
			request: func() *http.Request {
				return httptest.NewRequest(http.MethodPost, "/", strings.NewReader("hello"))
			},
			assert: func(t *testing.T, rec *httptest.ResponseRecorder, sendCalled bool, _ msgin.Message[any]) {
				assert.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
				assert.False(t, sendCalled)
			},
		},
		{
			name: "a non-oversize decode error returns 400 and never reaches the target",
			request: func() *http.Request {
				return httptest.NewRequest(http.MethodPost, "/", errReader{})
			},
			assert: func(t *testing.T, rec *httptest.ResponseRecorder, sendCalled bool, _ msgin.Message[any]) {
				assert.Equal(t, http.StatusBadRequest, rec.Code)
				assert.False(t, sendCalled)
			},
		},
		{
			name: "the received message carries the request Content-Type",
			request: func() *http.Request {
				req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("{}"))
				req.Header.Set("Content-Type", "application/json")
				return req
			},
			assert: func(t *testing.T, rec *httptest.ResponseRecorder, sendCalled bool, captured msgin.Message[any]) {
				assert.Equal(t, http.StatusAccepted, rec.Code)
				require.True(t, sendCalled)
				ct, ok := captured.Headers().String(msgin.HeaderContentType)
				require.True(t, ok)
				assert.Equal(t, "application/json", ct)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cfg, err := msghttp.NewConfig(tc.opts...)
			require.NoError(t, err)

			target := msgin.NewDirectChannel()
			var (
				sendCalled bool
				captured   msgin.Message[any]
			)
			require.NoError(t, target.Subscribe(msgin.HandlerFunc(func(_ context.Context, msg msgin.Message[any]) error {
				sendCalled = true
				captured = msg
				return tc.handlerErr
			})))

			rec := httptest.NewRecorder()
			msghttp.ServeAsync(rec, tc.request(), target, cfg)

			tc.assert(t, rec, sendCalled, captured)
		})
	}
}

func TestServeGateway(t *testing.T) {
	t.Parallel()

	type testCase struct {
		name     string
		opts     []msghttp.Option
		exchange func(t *testing.T) msgin.RequestReplyExchange
		request  func() *http.Request
		assert   func(t *testing.T, rec *httptest.ResponseRecorder)
	}

	cases := []testCase{
		{
			name: "real ChannelExchange echo round-trip returns 200 with the request body",
			exchange: func(t *testing.T) msgin.RequestReplyExchange {
				t.Helper()
				request := msgin.NewDirectChannel()
				reply := msgin.NewDirectChannel()
				require.NoError(t, request.Subscribe(msgin.Chain(msgin.To(reply))))
				x, err := msgin.NewChannelExchange(request, reply)
				require.NoError(t, err)
				t.Cleanup(func() { assert.NoError(t, x.Close()) })
				return x
			},
			request: func() *http.Request {
				return httptest.NewRequest(http.MethodPost, "/", strings.NewReader("hello gateway"))
			},
			assert: func(t *testing.T, rec *httptest.ResponseRecorder) {
				assert.Equal(t, http.StatusOK, rec.Code)
				assert.Equal(t, "hello gateway", rec.Body.String())
			},
		},
		{
			name: "ErrReplyTimeout maps to 504",
			exchange: func(*testing.T) msgin.RequestReplyExchange {
				return fakeExchange{err: msgin.ErrReplyTimeout}
			},
			request: func() *http.Request {
				return httptest.NewRequest(http.MethodPost, "/", strings.NewReader("x"))
			},
			assert: func(t *testing.T, rec *httptest.ResponseRecorder) {
				assert.Equal(t, http.StatusGatewayTimeout, rec.Code)
			},
		},
		{
			name: "ErrGatewayClosed maps to 503",
			exchange: func(*testing.T) msgin.RequestReplyExchange {
				return fakeExchange{err: msgin.ErrGatewayClosed}
			},
			request: func() *http.Request {
				return httptest.NewRequest(http.MethodPost, "/", strings.NewReader("x"))
			},
			assert: func(t *testing.T, rec *httptest.ResponseRecorder) {
				assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
			},
		},
		{
			name: "ErrDuplicateCorrelation maps to 409",
			exchange: func(*testing.T) msgin.RequestReplyExchange {
				return fakeExchange{err: msgin.ErrDuplicateCorrelation}
			},
			request: func() *http.Request {
				return httptest.NewRequest(http.MethodPost, "/", strings.NewReader("x"))
			},
			assert: func(t *testing.T, rec *httptest.ResponseRecorder) {
				assert.Equal(t, http.StatusConflict, rec.Code)
			},
		},
		{
			name: "ErrNoCorrelation maps to 500",
			exchange: func(*testing.T) msgin.RequestReplyExchange {
				return fakeExchange{err: msgin.ErrNoCorrelation}
			},
			request: func() *http.Request {
				return httptest.NewRequest(http.MethodPost, "/", strings.NewReader("x"))
			},
			assert: func(t *testing.T, rec *httptest.ResponseRecorder) {
				assert.Equal(t, http.StatusInternalServerError, rec.Code)
			},
		},
		{
			name: "a generic exchange error maps to 500",
			exchange: func(*testing.T) msgin.RequestReplyExchange {
				return fakeExchange{err: errBoom}
			},
			request: func() *http.Request {
				return httptest.NewRequest(http.MethodPost, "/", strings.NewReader("x"))
			},
			assert: func(t *testing.T, rec *httptest.ResponseRecorder) {
				assert.Equal(t, http.StatusInternalServerError, rec.Code)
			},
		},
		{
			name: "a non-bytes reply payload maps to 500 with an empty body and no leaked headers",
			opts: []msghttp.Option{msghttp.WithResponseHeaders("X-Secret")},
			exchange: func(*testing.T) msgin.RequestReplyExchange {
				reply := msgin.New[any](42, msgin.WithHeaders(map[string]any{"X-Secret": "leak-me"}))
				return fakeExchange{reply: reply}
			},
			request: func() *http.Request {
				return httptest.NewRequest(http.MethodPost, "/", strings.NewReader("x"))
			},
			assert: func(t *testing.T, rec *httptest.ResponseRecorder) {
				assert.Equal(t, http.StatusInternalServerError, rec.Code)
				assert.Zero(t, rec.Body.Len())
				assert.Empty(t, rec.Header().Get("X-Secret"))
			},
		},
		{
			name: "WithErrorStatus custom mapper overrides an exchange error to 418",
			opts: []msghttp.Option{msghttp.WithErrorStatus(func(error) int { return http.StatusTeapot })},
			exchange: func(*testing.T) msgin.RequestReplyExchange {
				return fakeExchange{err: errBoom}
			},
			request: func() *http.Request {
				return httptest.NewRequest(http.MethodPost, "/", strings.NewReader("x"))
			},
			assert: func(t *testing.T, rec *httptest.ResponseRecorder) {
				assert.Equal(t, http.StatusTeapot, rec.Code)
			},
		},
		{
			name: "oversize body returns 413 and never reaches the exchange",
			opts: []msghttp.Option{msghttp.WithMaxBodyBytes(4)},
			exchange: func(*testing.T) msgin.RequestReplyExchange {
				return panicExchange{}
			},
			request: func() *http.Request {
				return httptest.NewRequest(http.MethodPost, "/", strings.NewReader("hello"))
			},
			assert: func(t *testing.T, rec *httptest.ResponseRecorder) {
				assert.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
			},
		},
		{
			name: "a non-oversize decode error returns 400 and never reaches the exchange",
			exchange: func(*testing.T) msgin.RequestReplyExchange {
				return panicExchange{}
			},
			request: func() *http.Request {
				return httptest.NewRequest(http.MethodPost, "/", errReader{})
			},
			assert: func(t *testing.T, rec *httptest.ResponseRecorder) {
				assert.Equal(t, http.StatusBadRequest, rec.Code)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cfg, err := msghttp.NewConfig(tc.opts...)
			require.NoError(t, err)

			rec := httptest.NewRecorder()
			msghttp.ServeGateway(rec, tc.request(), tc.exchange(t), cfg)

			tc.assert(t, rec)
		})
	}
}
