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

// boomExchange is a msgin.RequestReplyExchange that always panics, standing in
// for a panicking flow handler whose panic unwinds out of Exchange — the F1
// scenario ServeGateway's recover() must contain.
type boomExchange struct{}

func (boomExchange) Exchange(context.Context, msgin.Message[any]) (msgin.Message[any], error) {
	panic("flow handler exploded")
}

func TestServeAsync(t *testing.T) {
	t.Parallel()

	type testCase struct {
		name         string
		opts         []msghttp.Option
		request      func() *http.Request
		handlerErr   error
		handlerPanic bool
		assert       func(t *testing.T, rec *httptest.ResponseRecorder, sendCalled bool, captured msgin.Message[any])
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
			name: "the received message carries the request Content-Type under http.content-type",
			request: func() *http.Request {
				req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("{}"))
				req.Header.Set("Content-Type", "application/json")
				return req
			},
			assert: func(t *testing.T, rec *httptest.ResponseRecorder, sendCalled bool, captured msgin.Message[any]) {
				assert.Equal(t, http.StatusAccepted, rec.Code)
				require.True(t, sendCalled)
				ct, ok := captured.Headers().String("http.content-type")
				require.True(t, ok)
				assert.Equal(t, "application/json", ct)
			},
		},
		{
			name:         "a panicking flow handler is recovered and mapped to 500",
			handlerPanic: true,
			request: func() *http.Request {
				return httptest.NewRequest(http.MethodPost, "/", strings.NewReader("x"))
			},
			assert: func(t *testing.T, rec *httptest.ResponseRecorder, sendCalled bool, _ msgin.Message[any]) {
				assert.Equal(t, http.StatusInternalServerError, rec.Code)
				assert.True(t, sendCalled)
				assert.Zero(t, rec.Body.Len())
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
				if tc.handlerPanic {
					panic("flow handler exploded")
				}
				return tc.handlerErr
			})))

			rec := httptest.NewRecorder()
			require.NotPanics(t, func() {
				msghttp.ServeAsync(rec, tc.request(), target, cfg)
			})

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
		{
			name: "a panic unwinding out of Exchange is recovered and mapped to 500",
			exchange: func(*testing.T) msgin.RequestReplyExchange {
				return boomExchange{}
			},
			request: func() *http.Request {
				return httptest.NewRequest(http.MethodPost, "/", strings.NewReader("x"))
			},
			assert: func(t *testing.T, rec *httptest.ResponseRecorder) {
				assert.Equal(t, http.StatusInternalServerError, rec.Code)
				assert.Zero(t, rec.Body.Len())
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cfg, err := msghttp.NewConfig(tc.opts...)
			require.NoError(t, err)

			rec := httptest.NewRecorder()
			require.NotPanics(t, func() {
				msghttp.ServeGateway(rec, tc.request(), tc.exchange(t), cfg)
			})

			tc.assert(t, rec)
		})
	}
}

// TestServeGateway_panickingFlowIsContainedEndToEnd is the end-to-end
// regression test for the whole-branch review's F1: a panic raised by a flow
// handler unwinds out of msgin.ChannelExchange.Exchange and, without a
// recover() at this adapter's boundary, escapes into net/http — which aborts
// the connection, so the client sees a transport error instead of a response.
// With the recover in place every request gets a clean 500 and the server
// keeps serving the next one.
//
// It deliberately does NOT assert that no correlator slot leaks: the core's
// giveUp cleanup is not deferred, and making it panic-safe is a separate
// tracked increment (see recoverHandler's NOTE). Each request here takes a
// fresh server-minted correlation id, which is the default and the only shape
// this adapter guarantees.
func TestServeGateway_panickingFlowIsContainedEndToEnd(t *testing.T) {
	t.Parallel()

	request := msgin.NewDirectChannel()
	reply := msgin.NewDirectChannel()
	require.NoError(t, request.Subscribe(msgin.HandlerFunc(func(context.Context, msgin.Message[any]) error {
		panic("flow handler exploded")
	})))
	exchange, err := msgin.NewChannelExchange(request, reply)
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, exchange.Close()) })

	cfg, err := msghttp.NewConfig()
	require.NoError(t, err)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		msghttp.ServeGateway(w, r, exchange, cfg)
	}))
	defer srv.Close()

	for range 2 {
		resp, err := http.Post(srv.URL, "text/plain", strings.NewReader("x"))
		require.NoError(t, err, "an escaping panic aborts the connection and surfaces here as a transport error")
		assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
		require.NoError(t, resp.Body.Close())
	}
}

// TestServeGateway_writeFailureWritesTheStatusLineOnce is the regression test
// for F2: EncodeResponse writes 200 and then fails on w.Write; ServeGateway
// must NOT follow that with a second WriteHeader(500) (net/http's
// "superfluous response.WriteHeader call").
func TestServeGateway_writeFailureWritesTheStatusLineOnce(t *testing.T) {
	t.Parallel()

	cfg, err := msghttp.NewConfig()
	require.NoError(t, err)

	w := &failingWriter{}
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("x"))
	msghttp.ServeGateway(w, req, fakeExchange{reply: msgin.New[any]([]byte("reply"))}, cfg)

	assert.Equal(t, []int{http.StatusOK}, w.codes,
		"the status line must be written exactly once when the body write fails")
}

// TestServeGateway_panicAfterCommitDoesNotRestateTheStatus covers the
// interaction of the two hardening fixes: a panic raised once the reply's 200
// is already on the wire must still be contained, but the recover must NOT
// then write a 500 over a committed response.
func TestServeGateway_panicAfterCommitDoesNotRestateTheStatus(t *testing.T) {
	t.Parallel()

	cfg, err := msghttp.NewConfig()
	require.NoError(t, err)

	w := &panicOnWriteWriter{}
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("x"))

	require.NotPanics(t, func() {
		msghttp.ServeGateway(w, req, fakeExchange{reply: msgin.New[any]([]byte("reply"))}, cfg)
	})

	assert.Equal(t, []int{http.StatusOK}, w.codes,
		"a post-commit panic may be logged, never answered with a second status")
}

// TestHandlerCores_toleratesAHandBuiltConfig is the regression test for F3:
// msghttp.Config is exported with unexported fields, so &msghttp.Config{} is
// constructible from any package and every consumer of it is exported. A
// zero-value (or nil) Config must degrade to the documented defaults, never
// panic on a nil logger / nil error mapper / zero body cap / zero status.
func TestHandlerCores_toleratesAHandBuiltConfig(t *testing.T) {
	t.Parallel()

	// Both cases run the identical assertion body against a differently
	// under-constructed Config, so the varying input is the Config alone.
	type testCase struct {
		name string
		cfg  *msghttp.Config
	}

	cases := []testCase{
		{
			name: "zero-value Config",
			cfg:  &msghttp.Config{},
		},
		{
			name: "nil Config",
			cfg:  nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			target := msgin.NewDirectChannel()
			require.NoError(t, target.Subscribe(msgin.HandlerFunc(func(context.Context, msgin.Message[any]) error {
				return nil
			})))

			// DecodeRequest: a zero maxBodyBytes must back-fill to the 1 MiB
			// default rather than rejecting every non-empty body.
			msg, err := msghttp.DecodeRequest(httptest.NewRequest(http.MethodPost, "/", strings.NewReader("hello")), tc.cfg)
			require.NoError(t, err)
			assert.Equal(t, []byte("hello"), msg.Payload())

			// EncodeResponse: no allow-list, no logger, still fine.
			rec := httptest.NewRecorder()
			require.NoError(t, msghttp.EncodeResponse(rec, msgin.New[any]([]byte("ok")), tc.cfg))
			assert.Equal(t, http.StatusOK, rec.Code)

			// ServeAsync success: a zero successStatus must back-fill to 202.
			rec = httptest.NewRecorder()
			require.NotPanics(t, func() {
				msghttp.ServeAsync(rec, httptest.NewRequest(http.MethodPost, "/", strings.NewReader("x")), target, tc.cfg)
			})
			assert.Equal(t, http.StatusAccepted, rec.Code)

			// ServeAsync failure: a nil logger and nil error mapper must not panic.
			failing := msgin.NewDirectChannel()
			require.NoError(t, failing.Subscribe(msgin.HandlerFunc(func(context.Context, msgin.Message[any]) error {
				return errBoom
			})))
			rec = httptest.NewRecorder()
			require.NotPanics(t, func() {
				msghttp.ServeAsync(rec, httptest.NewRequest(http.MethodPost, "/", strings.NewReader("x")), failing, tc.cfg)
			})
			assert.Equal(t, http.StatusInternalServerError, rec.Code)

			// ServeGateway: success and the nil-errorStatus mapping path.
			rec = httptest.NewRecorder()
			require.NotPanics(t, func() {
				msghttp.ServeGateway(rec, httptest.NewRequest(http.MethodPost, "/", strings.NewReader("x")),
					fakeExchange{reply: msgin.New[any]([]byte("reply"))}, tc.cfg)
			})
			assert.Equal(t, http.StatusOK, rec.Code)
			assert.Equal(t, "reply", rec.Body.String())

			rec = httptest.NewRecorder()
			require.NotPanics(t, func() {
				msghttp.ServeGateway(rec, httptest.NewRequest(http.MethodPost, "/", strings.NewReader("x")),
					fakeExchange{err: msgin.ErrReplyTimeout}, tc.cfg)
			})
			assert.Equal(t, http.StatusGatewayTimeout, rec.Code)

			// A decode failure through a hand-built Config.
			rec = httptest.NewRecorder()
			require.NotPanics(t, func() {
				msghttp.ServeGateway(rec, httptest.NewRequest(http.MethodPost, "/", errReader{}),
					panicExchange{}, tc.cfg)
			})
			assert.Equal(t, http.StatusBadRequest, rec.Code)
		})
	}
}
