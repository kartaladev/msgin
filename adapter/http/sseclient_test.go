package msghttp_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kartaladev/msgin"
	msghttp "github.com/kartaladev/msgin/adapter/http"
)

// TestNewSSEClient_construction exercises Plan 026 Task 1's construction
// surface: URL validation (row 1, reused verbatim from the Plan 024 class —
// see TestNewOutbound_construction), WithReconnectBackoff's set-flag
// validation (row 2) and WithReadTimeout's set-flag validation (row 2b). Rows
// 3 (unset-backoff defaults), 4 (connect-headers actually-sent/reserved-name
// drop) and 6 (resolveSSEClient's behavioral no-Timeout effect) are NOT
// assertable from this blackbox construction-only test — Config is
// unexported with no getters, and the effect of "no Timeout" is only
// observable by holding a stream open — so they are DEFERRED to Task 2's e2e
// tests (the Plan 024 Task 1 deferral precedent: see outbound_test.go's
// TestNewConfig_outbound doc comment).
func TestNewSSEClient_construction(t *testing.T) {
	t.Parallel()

	type testCase struct {
		name   string
		url    string
		opts   []msghttp.Option
		assert func(t *testing.T, c *msghttp.SSEClient, err error)
	}

	valid := func(t *testing.T, c *msghttp.SSEClient, err error) {
		t.Helper()
		require.NoError(t, err)
		assert.NotNil(t, c)
	}

	cases := []testCase{
		{ // row 1: empty
			name: "empty URL -> ErrEmptyURL",
			url:  "",
			assert: func(t *testing.T, c *msghttp.SSEClient, err error) {
				assert.Nil(t, c)
				assert.ErrorIs(t, err, msghttp.ErrEmptyURL)
			},
		},
		{ // row 1: whitespace
			name: "whitespace URL -> ErrEmptyURL",
			url:  "   ",
			assert: func(t *testing.T, c *msghttp.SSEClient, err error) {
				assert.Nil(t, c)
				assert.ErrorIs(t, err, msghttp.ErrEmptyURL)
			},
		},
		{ // row 1: parse failure
			name: "unparseable URL -> ErrInvalidURL",
			url:  "://bad",
			assert: func(t *testing.T, c *msghttp.SSEClient, err error) {
				assert.Nil(t, c)
				assert.ErrorIs(t, err, msghttp.ErrInvalidURL)
			},
		},
		{ // row 1: bad scheme
			name: "non-http scheme -> ErrInvalidURL",
			url:  "file:///etc/passwd",
			assert: func(t *testing.T, c *msghttp.SSEClient, err error) {
				assert.Nil(t, c)
				assert.ErrorIs(t, err, msghttp.ErrInvalidURL)
			},
		},
		{ // row 1: empty host
			name: "empty host -> ErrInvalidURL",
			url:  "http:///path",
			assert: func(t *testing.T, c *msghttp.SSEClient, err error) {
				assert.Nil(t, c)
				assert.ErrorIs(t, err, msghttp.ErrInvalidURL)
			},
		},
		{ // row 1: valid
			name:   "a valid URL constructs an SSEClient",
			url:    "https://example.test/events",
			assert: valid,
		},
		{ // row 2: min <= 0
			name: "WithReconnectBackoff(0, x) -> ErrInvalidReconnectBackoff",
			url:  "https://example.test/events",
			opts: []msghttp.Option{msghttp.WithReconnectBackoff(0, time.Second)},
			assert: func(t *testing.T, c *msghttp.SSEClient, err error) {
				assert.Nil(t, c)
				assert.ErrorIs(t, err, msghttp.ErrInvalidReconnectBackoff)
			},
		},
		{ // row 2: min < 0
			name: "WithReconnectBackoff(-1, x) -> ErrInvalidReconnectBackoff",
			url:  "https://example.test/events",
			opts: []msghttp.Option{msghttp.WithReconnectBackoff(-1, time.Second)},
			assert: func(t *testing.T, c *msghttp.SSEClient, err error) {
				assert.Nil(t, c)
				assert.ErrorIs(t, err, msghttp.ErrInvalidReconnectBackoff)
			},
		},
		{ // row 2: max < min
			name: "WithReconnectBackoff(x, x-1) -> ErrInvalidReconnectBackoff",
			url:  "https://example.test/events",
			opts: []msghttp.Option{msghttp.WithReconnectBackoff(time.Second, time.Second-1)},
			assert: func(t *testing.T, c *msghttp.SSEClient, err error) {
				assert.Nil(t, c)
				assert.ErrorIs(t, err, msghttp.ErrInvalidReconnectBackoff)
			},
		},
		{ // row 2: accepted
			name:   "WithReconnectBackoff(1ns, 1ns) is accepted",
			url:    "https://example.test/events",
			opts:   []msghttp.Option{msghttp.WithReconnectBackoff(time.Nanosecond, time.Nanosecond)},
			assert: valid,
		},
		{ // row 2b: zero
			name: "WithReadTimeout(0) -> ErrInvalidReadTimeout",
			url:  "https://example.test/events",
			opts: []msghttp.Option{msghttp.WithReadTimeout(0)},
			assert: func(t *testing.T, c *msghttp.SSEClient, err error) {
				assert.Nil(t, c)
				assert.ErrorIs(t, err, msghttp.ErrInvalidReadTimeout)
			},
		},
		{ // row 2b: negative
			name: "WithReadTimeout(-1) -> ErrInvalidReadTimeout",
			url:  "https://example.test/events",
			opts: []msghttp.Option{msghttp.WithReadTimeout(-1)},
			assert: func(t *testing.T, c *msghttp.SSEClient, err error) {
				assert.Nil(t, c)
				assert.ErrorIs(t, err, msghttp.ErrInvalidReadTimeout)
			},
		},
		{ // row 2b: accepted
			name:   "WithReadTimeout(1ns) is accepted",
			url:    "https://example.test/events",
			opts:   []msghttp.Option{msghttp.WithReadTimeout(time.Nanosecond)},
			assert: valid,
		},
		{ // row 2b: unset -> off (INV-C7), not an error
			name:   "WithReadTimeout left unset is accepted (off by default)",
			url:    "https://example.test/events",
			assert: valid,
		},
		{ // WithConnectHeaders(nil) is a no-op (structural half of row 4)
			name:   "WithConnectHeaders(nil) is a no-op",
			url:    "https://example.test/events",
			opts:   []msghttp.Option{msghttp.WithConnectHeaders(nil)},
			assert: valid,
		},
		{ // WithConnectHeaders with a populated header is accepted (structural half of row 4)
			name: "WithConnectHeaders with a populated header is accepted",
			url:  "https://example.test/events",
			opts: []msghttp.Option{msghttp.WithConnectHeaders(http.Header{
				"X-Api-Key":     []string{"secret"},
				"Last-Event-ID": []string{"attacker-supplied"},
			})},
			assert: valid,
		},
		{ // resolveSSEClient structural coverage: caller-supplied client, no
			// CheckRedirect, default (no-follow) posture -> a shallow copy is
			// installed; construction must still succeed (the Timeout-preserved,
			// no-follow-installed BEHAVIOR is asserted in Task 2's e2e tests —
			// this Config is unexported with no getter to inspect the resolved
			// client from a blackbox test).
			name:   "WithHTTPClient with a plain custom client is accepted",
			url:    "https://example.test/events",
			opts:   []msghttp.Option{msghttp.WithHTTPClient(&http.Client{Timeout: 5 * time.Second})},
			assert: valid,
		},
		{ // resolveSSEClient structural coverage: caller-supplied client whose
			// own CheckRedirect is already set -> resolveSSEClient must return it
			// verbatim rather than overriding the caller's explicit choice.
			name: "WithHTTPClient with a client that sets its own CheckRedirect is accepted",
			url:  "https://example.test/events",
			opts: []msghttp.Option{msghttp.WithHTTPClient(&http.Client{
				CheckRedirect: func(*http.Request, []*http.Request) error { return nil },
			})},
			assert: valid,
		},
		{ // resolveSSEClient structural coverage: WithFollowRedirects opts out of
			// the no-follow install on a caller-supplied client.
			name:   "WithHTTPClient plus WithFollowRedirects is accepted",
			url:    "https://example.test/events",
			opts:   []msghttp.Option{msghttp.WithHTTPClient(&http.Client{}), msghttp.WithFollowRedirects()},
			assert: valid,
		},
		{ // resolveSSEClient structural coverage: WithFollowRedirects alone
			// (no WithHTTPClient) skips installing the no-follow CheckRedirect on
			// the fresh default client.
			name:   "WithFollowRedirects alone is accepted",
			url:    "https://example.test/events",
			opts:   []msghttp.Option{msghttp.WithFollowRedirects()},
			assert: valid,
		},
		{ // a bad Option propagates NewConfig's error
			name: "a bad Option propagates NewConfig's error",
			url:  "https://example.test/events",
			opts: []msghttp.Option{msghttp.WithMaxBodyBytes(0)},
			assert: func(t *testing.T, c *msghttp.SSEClient, err error) {
				assert.Nil(t, c)
				assert.ErrorIs(t, err, msghttp.ErrInvalidMaxBodyBytes)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c, err := msghttp.NewSSEClient(tc.url, tc.opts...)
			tc.assert(t, c, err)
		})
	}
}

// TestSSEClient_nativeReliability covers row 5: SSEClient's NativeReliability
// capability is always false/false — resume is best-effort, not a
// redelivery guarantee, and there is no dead-letter concept for a stream.
func TestSSEClient_nativeReliability(t *testing.T) {
	t.Parallel()

	c, err := msghttp.NewSSEClient("https://example.test/events")
	require.NoError(t, err)
	require.NotNil(t, c)

	assert.False(t, c.NativeRedelivery())
	assert.False(t, c.NativeDeadLetter())

	var _ msgin.NativeReliability = c
}
