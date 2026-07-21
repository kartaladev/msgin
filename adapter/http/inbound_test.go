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
