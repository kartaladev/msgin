package msghttp_test

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jonboulle/clockwork"
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

// =============================================================================
// Task 2 — Stream: WHATWG triage, clamped reconnect pacing, Last-Event-ID
// resume. Test doubles/helpers below; test functions follow.
// =============================================================================

// sseEventStreamHeader is the Content-Type header a stub server attaches to a
// stream-triage-arm response.
var sseEventStreamHeader = http.Header{"Content-Type": {"text/event-stream"}}

// sseAttempt scripts ONE connection attempt's response for newSSEStubServer:
// status, optional extra headers (e.g. Content-Type), an optional raw body,
// and whether the handler should hold the connection open (blocking on the
// request's own context) after writing, to model a long-lived stream rather
// than one that ends immediately.
type sseAttempt struct {
	status int
	header http.Header
	body   string
	hold   bool
}

// newSSEStubServer serves attempts[0], attempts[1], ... on successive
// connections (1-indexed requests); once attempts is exhausted, the LAST
// attempt is repeated for any further request (a safety net — no test here
// depends on that repeat actually being reached). It returns the server's
// URL, a hits accessor (the number of requests served so far), a reqHeaders
// accessor (a clone of each request's headers, in arrival order), and a
// closeFn to tear the server down.
func newSSEStubServer(t *testing.T, attempts []sseAttempt) (url string, hits func() int32, reqHeaders func() []http.Header, closeFn func()) {
	t.Helper()

	var n atomic.Int32
	var mu sync.Mutex
	var headers []http.Header

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		idx := int(n.Add(1)) - 1

		mu.Lock()
		headers = append(headers, r.Header.Clone())
		mu.Unlock()

		if idx >= len(attempts) {
			idx = len(attempts) - 1
		}
		a := attempts[idx]

		for k, vs := range a.header {
			for _, v := range vs {
				w.Header().Add(k, v)
			}
		}
		w.WriteHeader(a.status)
		if a.body != "" {
			_, _ = io.WriteString(w, a.body)
		}
		if fl, ok := w.(http.Flusher); ok {
			fl.Flush()
		}
		if a.hold {
			<-r.Context().Done()
		}
	}))

	return srv.URL,
		func() int32 { return n.Load() },
		func() []http.Header {
			mu.Lock()
			defer mu.Unlock()
			return append([]http.Header(nil), headers...)
		},
		srv.Close
}

// parkOnBackoff blocks until Stream's single reconnect-pacing goroutine is
// parked on its backoff timer (cfg.streamClock().After), so a subsequent
// clk.Advance is guaranteed to land on that exact wait rather than racing it
// (mirrors classify_test.go's assertTransientWait / the pattern named in the
// task brief at classify_test.go:134-142).
func parkOnBackoff(t *testing.T, clk *clockwork.FakeClock) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	require.NoError(t, clk.BlockUntilContext(ctx, 1), "Stream never parked on its backoff timer")
}

// expectReconnectAfter is the INV-C4 INSTRUMENTED assertion (project rule:
// "measure interleaving tests, don't trust them" — a test that merely
// Advance(wait) and waits for the next hit would pass even if Stream
// reconnected early, since the clock has nothing else to race against). It
// proves BOTH halves: hits must NOT have advanced past before yet (parking
// alone proves nothing about timing), then wait-1ns must NOT trigger the
// reconnect (an under-wait would corrupt the pacing guarantee silently), and
// only the final 1ns closes it out. Mirrors classify_test.go's
// assertTransientWait two-phase advance.
func expectReconnectAfter(t *testing.T, clk *clockwork.FakeClock, hits func() int32, before int32, wait time.Duration) {
	t.Helper()

	parkOnBackoff(t, clk)
	require.EqualValues(t, before, hits(), "a reconnect attempt occurred BEFORE the expected backoff advance")

	if wait > time.Nanosecond {
		clk.Advance(wait - time.Nanosecond)
		time.Sleep(20 * time.Millisecond) // let a (buggy) early reconnect's goroutine run
		require.EqualValues(t, before, hits(),
			"reconnected before the full %s backoff delay elapsed (under-waited)", wait)
	}

	clk.Advance(time.Nanosecond)
	require.Eventually(t, func() bool { return hits() >= before+1 }, 3*time.Second, 5*time.Millisecond,
		"reconnect did not occur after the full %s backoff delay elapsed", wait)
}

// -----------------------------------------------------------------------------
// Row 1 (+charset variant) & row 10 (Ack/Nack no-ops).
// -----------------------------------------------------------------------------

func TestSSEClient_streamsEvents(t *testing.T) {
	t.Parallel()

	type testCase struct {
		name        string
		contentType string
	}

	cases := []testCase{
		{name: "text/event-stream", contentType: "text/event-stream"},
		{name: "text/event-stream with a charset parameter", contentType: "text/event-stream; charset=utf-8"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			url, hits, _, closeSrv := newSSEStubServer(t, []sseAttempt{{
				status: http.StatusOK,
				header: http.Header{"Content-Type": {tc.contentType}},
				body:   "id: 1\nevent: greeting\ndata: hello\n\n",
				hold:   true,
			}})
			defer closeSrv()

			client, err := msghttp.NewSSEClient(url)
			require.NoError(t, err)

			ctx, cancel := context.WithCancel(t.Context())
			out := make(chan msgin.Delivery, 4)
			done := make(chan error, 1)
			go func() { done <- client.Stream(ctx, out) }()

			var d msgin.Delivery
			select {
			case d = <-out:
			case <-time.After(3 * time.Second):
				t.Fatal("no delivery received")
			}

			assert.Equal(t, []byte("hello"), d.Msg.Payload())
			name, ok := d.Msg.Headers().String(msghttp.HeaderSSEEventName)
			assert.True(t, ok)
			assert.Equal(t, "greeting", name)
			id, ok := d.Msg.Headers().String(msghttp.HeaderSSEEventID)
			assert.True(t, ok)
			assert.Equal(t, "1", id)
			assert.NotEqual(t, "1", d.Msg.ID(), "msg.ID() must be freshly minted, never the remote event id")

			// Row 10: Ack/Nack are no-ops.
			assert.NoError(t, d.Ack(t.Context()))
			assert.NoError(t, d.Nack(t.Context(), true, 0))

			cancel()
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				t.Fatal("Stream did not return after cancel")
			}
			assert.EqualValues(t, 1, hits())
		})
	}
}

// -----------------------------------------------------------------------------
// Rows 2, 3, 4 — INV-C1 terminal and reconnect status triage.
// -----------------------------------------------------------------------------

func TestSSEClient_triageTerminalAndReconnect(t *testing.T) {
	t.Parallel()

	type testCase struct {
		name         string
		attempts     []sseAttempt
		needsAdvance bool
		assert       func(t *testing.T, streamErr error, hits int32)
	}

	cases := []testCase{
		{
			name:     "204 -> nil, single attempt (row 2)",
			attempts: []sseAttempt{{status: http.StatusNoContent}},
			assert: func(t *testing.T, streamErr error, hits int32) {
				assert.NoError(t, streamErr)
				assert.EqualValues(t, 1, hits)
			},
		},
		{
			name: "200 + application/json -> ErrNotEventStream, single attempt (row 3)",
			attempts: []sseAttempt{{
				status: http.StatusOK,
				header: http.Header{"Content-Type": {"application/json"}},
				body:   `{}`,
			}},
			assert: func(t *testing.T, streamErr error, hits int32) {
				assert.ErrorIs(t, streamErr, msghttp.ErrNotEventStream)
				assert.EqualValues(t, 1, hits)
			},
		},
		{
			name:     "200 + missing Content-Type -> ErrNotEventStream, single attempt (row 3)",
			attempts: []sseAttempt{{status: http.StatusOK}}, // no body written -> no Content-Type sniffing
			assert: func(t *testing.T, streamErr error, hits int32) {
				assert.ErrorIs(t, streamErr, msghttp.ErrNotEventStream)
				assert.EqualValues(t, 1, hits)
			},
		},
		{
			name: "500 -> reconnect after a backoff advance, then 204 terminal (row 4)",
			attempts: []sseAttempt{
				{status: http.StatusInternalServerError},
				{status: http.StatusNoContent},
			},
			needsAdvance: true,
			assert: func(t *testing.T, streamErr error, hits int32) {
				assert.NoError(t, streamErr)
				assert.EqualValues(t, 2, hits)
			},
		},
		{
			name: "404 -> reconnect after a backoff advance, then 204 terminal (row 4)",
			attempts: []sseAttempt{
				{status: http.StatusNotFound},
				{status: http.StatusNoContent},
			},
			needsAdvance: true,
			assert: func(t *testing.T, streamErr error, hits int32) {
				assert.NoError(t, streamErr)
				assert.EqualValues(t, 2, hits)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			url, hits, _, closeSrv := newSSEStubServer(t, tc.attempts)
			defer closeSrv()

			clk := clockwork.NewFakeClock()
			client, err := msghttp.NewSSEClient(url, msghttp.WithSSEClock(clk))
			require.NoError(t, err)

			out := make(chan msgin.Delivery, 4)
			done := make(chan error, 1)
			go func() { done <- client.Stream(t.Context(), out) }()

			if tc.needsAdvance {
				expectReconnectAfter(t, clk, hits, 1, 500*time.Millisecond) // defaultReconnectMin
			}

			var streamErr error
			select {
			case streamErr = <-done:
			case <-time.After(3 * time.Second):
				t.Fatal("Stream did not return")
			}
			tc.assert(t, streamErr, hits())
		})
	}
}

// -----------------------------------------------------------------------------
// Row 5 — transport refusal: redacted WARN, reconnect.
// -----------------------------------------------------------------------------

func TestSSEClient_transportRefusal(t *testing.T) {
	t.Parallel()

	// Bind then immediately close a loopback port: nothing else in this test
	// process races to rebind it, so connecting there reliably refuses fast —
	// a hermetic transport failure with no real network dependency.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := ln.Addr().String()
	require.NoError(t, ln.Close())

	const path = "/events?token=super-secret"
	h := &captureHandler{}
	clk := clockwork.NewFakeClock()
	client, err := msghttp.NewSSEClient("http://"+addr+path,
		msghttp.WithLogger(slog.New(h)),
		msghttp.WithSSEClock(clk),
	)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	out := make(chan msgin.Delivery, 1)
	done := make(chan error, 1)
	go func() { done <- client.Stream(ctx, out) }()

	// The client parks on its backoff timer after the refused connect — the
	// observable proof that Stream treated it as reconnectable, not terminal.
	parkOnBackoff(t, clk)

	assert.GreaterOrEqual(t, h.count(slog.LevelWarn), 1, "a transport refusal must log a WARN")
	var sawWrapped bool
	for _, s := range h.attrStrings() {
		if strings.Contains(s, "msghttp: outbound request transport error") {
			sawWrapped = true
		}
		// redactTransport's INV-5 guarantee is over the *url.Error it unwraps:
		// it discards ue.URL (host/path/query/userinfo) entirely rather than
		// re-deriving a redacted string from it — a query-string secret (or
		// the request path) must never reach the log this way. The
		// underlying *net.OpError's own Error() string legitimately still
		// names the dialed host:port (not a credential), which is why this
		// checks for the query/path, not the bare address.
		assert.NotContains(t, s, "token=super-secret", "the WARN must never carry the target URL's query")
		assert.NotContains(t, s, path, "the WARN must never carry the target URL's path")
	}
	assert.True(t, sawWrapped, "the transport error must be wrapped via redactTransport")

	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Stream did not return after cancel")
	}
}

// attrStrings renders every slog.Attr value logged to h as a string, so a
// test can assert an untrusted value (e.g. a target URL/host) never reached
// the log (INV-5). Defined here (not in sse_server_test.go, which declares
// captureHandler) but legal: both files share package msghttp_test.
func (h *captureHandler) attrStrings() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []string
	for _, r := range h.records {
		r.Attrs(func(a slog.Attr) bool {
			out = append(out, a.Value.String())
			return true
		})
	}
	return out
}

// -----------------------------------------------------------------------------
// Rows 6, 7 — mid-stream EOF -> Last-Event-ID resume; an empty id: line clears
// it on the following reconnect (C3/WHATWG).
// -----------------------------------------------------------------------------

func TestSSEClient_resumeAndClear(t *testing.T) {
	t.Parallel()

	attempts := []sseAttempt{
		{status: http.StatusOK, header: sseEventStreamHeader, body: "id: 42\ndata: first\n\n"},
		{status: http.StatusOK, header: sseEventStreamHeader, body: "id: 42\ndata: second\n\nid:\ndata: third\n\n"},
		{status: http.StatusNoContent},
	}
	url, hits, reqHeaders, closeSrv := newSSEStubServer(t, attempts)
	defer closeSrv()

	clk := clockwork.NewFakeClock()
	client, err := msghttp.NewSSEClient(url, msghttp.WithSSEClock(clk))
	require.NoError(t, err)

	out := make(chan msgin.Delivery, 8)
	done := make(chan error, 1)
	go func() { done <- client.Stream(t.Context(), out) }()

	d1 := <-out
	assert.Equal(t, []byte("first"), d1.Msg.Payload())

	// Connection 1 ends (clean EOF) having emitted an event -> next wait
	// resets to min (500ms default) regardless of the prior state.
	expectReconnectAfter(t, clk, hits, 1, 500*time.Millisecond)

	d2 := <-out
	assert.Equal(t, []byte("second"), d2.Msg.Payload())
	d3 := <-out
	assert.Equal(t, []byte("third"), d3.Msg.Payload())

	expectReconnectAfter(t, clk, hits, 2, 500*time.Millisecond)

	var streamErr error
	select {
	case streamErr = <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Stream did not return")
	}
	assert.NoError(t, streamErr)
	assert.EqualValues(t, 3, hits())

	hdrs := reqHeaders()
	require.Len(t, hdrs, 3)
	assert.Empty(t, hdrs[0].Get("Last-Event-ID"), "the first connection holds no resume state yet")
	assert.Equal(t, "42", hdrs[1].Get("Last-Event-ID"), "row 6: resumed with the last-seen id")
	assert.Empty(t, hdrs[2].Get("Last-Event-ID"), "row 7: an empty id: line clears the resume buffer")
}

// -----------------------------------------------------------------------------
// Rows 8, 9 — INV-C4 clamped reconnect pacing (instrumented).
// -----------------------------------------------------------------------------

func TestSSEClient_backoffPacing(t *testing.T) {
	t.Parallel()

	immediateEOF := sseAttempt{status: http.StatusOK, header: sseEventStreamHeader}
	retryZero := sseAttempt{status: http.StatusOK, header: sseEventStreamHeader, body: "retry: 0\n\n"}
	retryHuge := sseAttempt{status: http.StatusOK, header: sseEventStreamHeader, body: "retry: 999999999999\n\n"}
	retryInBounds := sseAttempt{status: http.StatusOK, header: sseEventStreamHeader, body: "retry: 250\n\n"}
	oneEvent := sseAttempt{status: http.StatusOK, header: sseEventStreamHeader, body: "data: x\n\n"}
	eventAndRetry := sseAttempt{status: http.StatusOK, header: sseEventStreamHeader, body: "retry: 2000\ndata: x\n\n"}
	terminal := sseAttempt{status: http.StatusNoContent}

	type testCase struct {
		name     string
		min, max time.Duration
		attempts []sseAttempt
		waits    []time.Duration // expected wait before each subsequent attempt
	}

	cases := []testCase{
		{
			name: "doubling sequence min -> max cap",
			min:  100 * time.Millisecond, max: 400 * time.Millisecond,
			attempts: []sseAttempt{immediateEOF, immediateEOF, immediateEOF, immediateEOF, terminal},
			waits: []time.Duration{
				100 * time.Millisecond,
				200 * time.Millisecond,
				400 * time.Millisecond,
				400 * time.Millisecond, // capped at max
			},
		},
		{
			name: "retry: 0 clamps the next wait to min",
			min:  200 * time.Millisecond, max: 5 * time.Second,
			attempts: []sseAttempt{retryZero, terminal},
			waits:    []time.Duration{200 * time.Millisecond},
		},
		{
			name: "retry: huge clamps the next wait to max",
			min:  50 * time.Millisecond, max: 300 * time.Millisecond,
			attempts: []sseAttempt{retryHuge, terminal},
			waits:    []time.Duration{300 * time.Millisecond},
		},
		{
			name: "retry: already within [min,max] passes through unchanged",
			min:  50 * time.Millisecond, max: 1 * time.Second,
			attempts: []sseAttempt{retryInBounds, terminal},
			waits:    []time.Duration{250 * time.Millisecond},
		},
		{
			name: "a connection with >=1 event resets the next wait to min",
			min:  100 * time.Millisecond, max: 2 * time.Second,
			attempts: []sseAttempt{immediateEOF, immediateEOF, oneEvent, terminal},
			waits: []time.Duration{
				100 * time.Millisecond,
				200 * time.Millisecond,
				100 * time.Millisecond, // reset by the event, not doubled to 400ms
			},
		},
		{
			// User-decided precedence (2026-07-24): hasRetry > gotEvent >
			// doubling. A connection that BOTH delivers >=1 event AND carries
			// a valid "retry:" must honor the server's explicit retry value,
			// NOT reset to min. min=50ms, max=5s, retry=2000ms: clamp(2000ms)
			// is neither min (50ms) nor the plain-doubling value from min
			// (100ms), so the assertion is unambiguous — this case FAILS
			// under the old event-reset-wins precedence (which would expect
			// 50ms here instead).
			name: "a connection with >=1 event AND a retry: honors the retry (hasRetry wins over gotEvent)",
			min:  50 * time.Millisecond, max: 5 * time.Second,
			attempts: []sseAttempt{eventAndRetry, terminal},
			waits:    []time.Duration{2 * time.Second},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			url, hits, _, closeSrv := newSSEStubServer(t, tc.attempts)
			defer closeSrv()

			clk := clockwork.NewFakeClock()
			client, err := msghttp.NewSSEClient(url,
				msghttp.WithSSEClock(clk),
				msghttp.WithReconnectBackoff(tc.min, tc.max),
			)
			require.NoError(t, err)

			out := make(chan msgin.Delivery, 8)
			done := make(chan error, 1)
			go func() { done <- client.Stream(t.Context(), out) }()

			for i, wait := range tc.waits {
				expectReconnectAfter(t, clk, hits, int32(i+1), wait)
			}

			select {
			case streamErr := <-done:
				assert.NoError(t, streamErr)
			case <-time.After(3 * time.Second):
				t.Fatal("Stream did not terminate after the final attempt")
			}
			assert.EqualValues(t, len(tc.attempts), hits())
		})
	}
}

// -----------------------------------------------------------------------------
// Row 11 — connect headers actually sent; reserved names absent; clone proven.
// -----------------------------------------------------------------------------

func TestSSEClient_connectHeaders(t *testing.T) {
	t.Parallel()

	h := http.Header{
		"X-Api-Key":     {"secret"},
		"Last-Event-ID": {"attacker-supplied"}, // reserved: dropped at option time (Task 1)
	}
	url, hits, reqHeaders, closeSrv := newSSEStubServer(t, []sseAttempt{{status: http.StatusNoContent}})
	defer closeSrv()

	client, err := msghttp.NewSSEClient(url, msghttp.WithConnectHeaders(h))
	require.NoError(t, err)

	// Mutate the caller's own header AFTER construction: WithConnectHeaders
	// clones, so this must never leak into what is actually sent.
	h.Set("X-Api-Key", "mutated-after-construction")

	err = client.Stream(t.Context(), make(chan msgin.Delivery, 1))
	assert.NoError(t, err)
	assert.EqualValues(t, 1, hits())

	hdrs := reqHeaders()
	require.Len(t, hdrs, 1)
	assert.Equal(t, "secret", hdrs[0].Get("X-Api-Key"), "the connect header must be sent, not the post-construction mutation")
	assert.Empty(t, hdrs[0].Get("Last-Event-ID"), "the reserved name must be dropped")
	assert.Equal(t, "text/event-stream", hdrs[0].Get("Accept"))
	assert.Equal(t, "no-cache", hdrs[0].Get("Cache-Control"))
}

// -----------------------------------------------------------------------------
// Row 12 (MAJOR-1) — no-Timeout default lets a stream survive a real-time
// margin; an injected finite Timeout force-reconnects the same held-open
// stream. Discharges Task 1's deferred rows 3/4/6.
// -----------------------------------------------------------------------------

func sseHoldOpenHandler(hits *atomic.Int32) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		n := hits.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(w, "data: hello-%d\n\n", n)
		w.(http.Flusher).Flush()
		select {
		case <-time.After(2 * time.Second):
		case <-r.Context().Done():
		}
	}
}

func TestSSEClient_noTimeoutDefault(t *testing.T) {
	t.Parallel()

	t.Run("default client streams past a real-time margin, no abort", func(t *testing.T) {
		t.Parallel()

		var hits atomic.Int32
		srv := httptest.NewServer(sseHoldOpenHandler(&hits))
		defer srv.Close()

		client, err := msghttp.NewSSEClient(srv.URL)
		require.NoError(t, err)

		ctx, cancel := context.WithTimeout(t.Context(), 250*time.Millisecond)
		defer cancel()
		out := make(chan msgin.Delivery, 4)
		done := make(chan error, 1)
		go func() { done <- client.Stream(ctx, out) }()

		d := <-out
		assert.Equal(t, []byte("hello-1"), d.Msg.Payload())

		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Fatal("Stream did not return after the ctx timeout")
		}
		// The 250ms real-time margin comfortably exceeds what a wrongly-finite
		// default Timeout would have aborted at, yet only ONE attempt occurred.
		assert.EqualValues(t, 1, hits.Load())
	})

	t.Run("a finite WithHTTPClient Timeout force-reconnects the held-open stream", func(t *testing.T) {
		t.Parallel()

		var hits atomic.Int32
		srv := httptest.NewServer(sseHoldOpenHandler(&hits))
		defer srv.Close()

		client, err := msghttp.NewSSEClient(srv.URL,
			msghttp.WithHTTPClient(&http.Client{Timeout: 150 * time.Millisecond}),
			msghttp.WithReconnectBackoff(10*time.Millisecond, 10*time.Millisecond),
		)
		require.NoError(t, err)

		ctx, cancel := context.WithCancel(t.Context())
		out := make(chan msgin.Delivery, 8)
		done := make(chan error, 1)
		go func() { done <- client.Stream(ctx, out) }()

		require.Eventually(t, func() bool { return hits.Load() >= 2 }, 3*time.Second, 5*time.Millisecond,
			"a finite http.Client.Timeout must abort the held-open stream and force a reconnect")

		cancel()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Fatal("Stream did not return after cancel")
		}
	})
}
