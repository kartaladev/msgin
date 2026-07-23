package msghttp_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jonboulle/clockwork"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	msghttp "github.com/kartaladev/msgin/adapter/http"
)

// BEHAVIORAL-DEFERRAL NOTE (Plan 025, the Task-1 line-coverage rule): the
// writer's select arm `case frame := <-conn.frames` is present in Task 4's loop
// per the normative hub design, but the only producer that enqueues onto
// conn.frames is Send — a Task 5 deliverable. There is therefore no Task-4
// public path that exercises that arm, so it is the single line left uncovered
// here; Task 5's Send fan-out tests cover it when the feeder lands. Everything
// else on the writer/lifecycle path (all 11 Task-4 hot-path rows) is covered
// below. This file does not fabricate a private test hook to reach an arm no
// public Task-4 caller can drive.

// -----------------------------------------------------------------------------
// Test doubles (hermetic; goleak backstop is encode_test.go's TestMain).
// -----------------------------------------------------------------------------

// nonFlusherWriter wraps a http.ResponseWriter (a *httptest.ResponseRecorder in
// practice) but exposes only the http.ResponseWriter interface, so a
// w.(http.Flusher) assertion on it fails — the branch-3 guard fake. Because the
// embedded field's static type is the interface, the recorder's own Flush
// method is not promoted.
type nonFlusherWriter struct {
	http.ResponseWriter
}

// captureWriter is a hermetic http.ResponseWriter+Flusher whose SetWriteDeadline
// result is configurable (nil = supported, errors.ErrUnsupported = the F5 arm).
// Every Write is recorded under a mutex; every Flush pulses flushed so a test
// can synchronize on "headers flushed" / "frame flushed" without sleeping.
type captureWriter struct {
	hdr         http.Header
	flushed     chan struct{}
	deadlineErr error // returned by SetWriteDeadline

	mu        sync.Mutex
	written   bytes.Buffer
	deadlines int
}

func newCaptureWriter() *captureWriter {
	return &captureWriter{hdr: make(http.Header), flushed: make(chan struct{}, 64)}
}

func (c *captureWriter) Header() http.Header { return c.hdr }
func (c *captureWriter) WriteHeader(int)     {}

func (c *captureWriter) Write(p []byte) (int, error) {
	c.mu.Lock()
	c.written.Write(p)
	c.mu.Unlock()
	return len(p), nil
}

func (c *captureWriter) Flush() {
	select {
	case c.flushed <- struct{}{}:
	default:
	}
}

func (c *captureWriter) SetWriteDeadline(time.Time) error {
	c.mu.Lock()
	c.deadlines++
	err := c.deadlineErr
	c.mu.Unlock()
	return err
}

func (c *captureWriter) bytes() []byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]byte(nil), c.written.Bytes()...)
}

// blockingWriter models a stalled reader: its Write blocks until the real
// wall-clock deadline installed by SetWriteDeadline elapses, then returns
// os.ErrDeadlineExceeded — exactly as a kernel socket write does when the peer's
// TCP window is full and the write deadline trips. It is the INV-S8 fake.
type blockingWriter struct {
	hdr     http.Header
	flushed chan struct{}
	entered chan struct{} // pulsed when Write is entered (the writer is now wedged)

	mu       sync.Mutex
	deadline time.Time
}

func newBlockingWriter() *blockingWriter {
	return &blockingWriter{
		hdr:     make(http.Header),
		flushed: make(chan struct{}, 64),
		entered: make(chan struct{}, 64),
	}
}

func (b *blockingWriter) Header() http.Header { return b.hdr }
func (b *blockingWriter) WriteHeader(int)     {}

func (b *blockingWriter) Flush() {
	select {
	case b.flushed <- struct{}{}:
	default:
	}
}

func (b *blockingWriter) SetWriteDeadline(t time.Time) error {
	b.mu.Lock()
	b.deadline = t
	b.mu.Unlock()
	return nil
}

func (b *blockingWriter) Write(p []byte) (int, error) {
	select {
	case b.entered <- struct{}{}:
	default:
	}
	b.mu.Lock()
	d := b.deadline
	b.mu.Unlock()
	if d.IsZero() {
		return 0, os.ErrDeadlineExceeded
	}
	timer := time.NewTimer(time.Until(d))
	defer timer.Stop()
	<-timer.C
	return 0, os.ErrDeadlineExceeded
}

// captureHandler is a slog.Handler that records every emitted record, so a test
// can assert a WARN was (or was not, or was exactly-once) emitted.
type captureHandler struct {
	mu      sync.Mutex
	records []slog.Record
}

func (h *captureHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *captureHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	h.records = append(h.records, r.Clone())
	h.mu.Unlock()
	return nil
}

func (h *captureHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *captureHandler) WithGroup(string) slog.Handler      { return h }

func (h *captureHandler) count(level slog.Level) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	n := 0
	for _, r := range h.records {
		if r.Level == level {
			n++
		}
	}
	return n
}

// serveInBackground runs s.ServeHTTP(w, req) on a goroutine cancellable via the
// returned cancel, and returns a done channel closed when the handler exits.
// The test MUST drain done (join) before returning so the goleak backstop is
// meaningful.
func serveInBackground(s *msghttp.SSEServer, w http.ResponseWriter) (cancel context.CancelFunc, done <-chan struct{}) {
	ctx, cancelFn := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/events", nil).WithContext(ctx)
	d := make(chan struct{})
	go func() {
		s.ServeHTTP(w, req)
		close(d)
	}()
	return cancelFn, d
}

// -----------------------------------------------------------------------------
// Construction.
// -----------------------------------------------------------------------------

func TestNewSSEServer_construction(t *testing.T) {
	t.Parallel()

	type testCase struct {
		name   string
		opts   []msghttp.Option
		assert func(t *testing.T, s *msghttp.SSEServer, err error)
	}

	cases := []testCase{
		{
			name: "defaults build a server",
			assert: func(t *testing.T, s *msghttp.SSEServer, err error) {
				require.NoError(t, err)
				require.NotNil(t, s)
			},
		},
		{
			name: "an invalid option is a construction error",
			opts: []msghttp.Option{msghttp.WithMaxConnections(0)},
			assert: func(t *testing.T, s *msghttp.SSEServer, err error) {
				assert.Nil(t, s)
				assert.ErrorIs(t, err, msghttp.ErrInvalidMaxConnections)
			},
		},
		{
			name: "an invalid write timeout is a construction error",
			opts: []msghttp.Option{msghttp.WithWriteTimeout(-1)},
			assert: func(t *testing.T, s *msghttp.SSEServer, err error) {
				assert.Nil(t, s)
				assert.ErrorIs(t, err, msghttp.ErrInvalidWriteTimeout)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s, err := msghttp.NewSSEServer(tc.opts...)
			tc.assert(t, s, err)
		})
	}
}

func TestSSEServer_isHTTPHandler(t *testing.T) {
	t.Parallel()
	var _ http.Handler = (*msghttp.SSEServer)(nil)
}

// -----------------------------------------------------------------------------
// Branch 1 — method != GET → 405 + Allow: GET.
// -----------------------------------------------------------------------------

func TestSSEServer_methodNotGet(t *testing.T) {
	t.Parallel()

	s, err := msghttp.NewSSEServer()
	require.NoError(t, err)

	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch} {
		t.Run(method, func(t *testing.T) {
			t.Parallel()
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(method, "/events", nil)
			s.ServeHTTP(rec, req)
			assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
			assert.Equal(t, http.MethodGet, rec.Header().Get("Allow"))
		})
	}
}

// -----------------------------------------------------------------------------
// Branch 2 — WithMaxConnections cap: at-cap → 503, then a freed slot → 200.
// -----------------------------------------------------------------------------

func TestSSEServer_maxConnections(t *testing.T) {
	t.Parallel()

	server, err := msghttp.NewSSEServer(msghttp.WithMaxConnections(2))
	require.NoError(t, err)

	srv := httptest.NewServer(server)
	client := srv.Client()

	// open opens a streaming GET; on 200 it returns a cancel+close cleanup.
	open := func() (*http.Response, func()) {
		ctx, cancel := context.WithCancel(context.Background())
		req, reqErr := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
		require.NoError(t, reqErr)
		resp, doErr := client.Do(req)
		require.NoError(t, doErr)
		return resp, func() { cancel(); _ = resp.Body.Close() }
	}

	resp1, close1 := open()
	require.Equal(t, http.StatusOK, resp1.StatusCode)
	resp2, close2 := open()
	require.Equal(t, http.StatusOK, resp2.StatusCode)

	// Third connection is over the cap.
	resp3, close3 := open()
	assert.Equal(t, http.StatusServiceUnavailable, resp3.StatusCode)
	close3()

	// Free a slot; a subsequent connection must succeed once the server has
	// observed the disconnect and unregistered.
	close1()
	require.Eventually(t, func() bool {
		resp, c := open()
		ok := resp.StatusCode == http.StatusOK
		c()
		return ok
	}, 3*time.Second, 10*time.Millisecond)

	// Tear down: cancel remaining streams, close the SSE server so every
	// handler returns, then close the httptest server.
	close2()
	require.NoError(t, server.Close(context.Background()))
	srv.Close()
}

// -----------------------------------------------------------------------------
// Branch 3 — non-Flusher ResponseWriter → 500 + WARN.
// -----------------------------------------------------------------------------

func TestSSEServer_nonFlusher(t *testing.T) {
	t.Parallel()

	h := &captureHandler{}
	server, err := msghttp.NewSSEServer(msghttp.WithLogger(slog.New(h)))
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	w := nonFlusherWriter{ResponseWriter: rec}
	req := httptest.NewRequest(http.MethodGet, "/events", nil)

	server.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Equal(t, 1, h.count(slog.LevelWarn))
}

// -----------------------------------------------------------------------------
// Branch 4 — happy path: the exact SSE response headers are set and flushed.
// -----------------------------------------------------------------------------

func TestSSEServer_happyHeaders(t *testing.T) {
	t.Parallel()

	server, err := msghttp.NewSSEServer()
	require.NoError(t, err)

	w := newCaptureWriter()
	cancel, done := serveInBackground(server, w)

	// The writer flushes the headers before entering its loop.
	<-w.flushed

	assert.Equal(t, "text/event-stream", w.Header().Get("Content-Type"))
	assert.Equal(t, "no-cache", w.Header().Get("Cache-Control"))
	assert.Equal(t, "keep-alive", w.Header().Get("Connection"))

	// The stream stays open until the client disconnects.
	cancel()
	<-done
}

// -----------------------------------------------------------------------------
// Branch 5 — client disconnect → handler returns, connection unregistered.
// -----------------------------------------------------------------------------

func TestSSEServer_clientDisconnect(t *testing.T) {
	t.Parallel()

	server, err := msghttp.NewSSEServer(msghttp.WithMaxConnections(1))
	require.NoError(t, err)

	srv := httptest.NewServer(server)
	defer srv.Close()
	client := srv.Client()

	ctx, cancel := context.WithCancel(context.Background())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	require.NoError(t, err)
	resp, err := client.Do(req)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	// Disconnect: cancelling the request ctx closes the client connection; the
	// server's derived ctx fires and the handler returns, unregistering the
	// only slot. A subsequent connection then succeeds.
	cancel()
	_ = resp.Body.Close()

	require.Eventually(t, func() bool {
		c2, cancel2 := context.WithCancel(context.Background())
		defer cancel2()
		r2, reqErr := http.NewRequestWithContext(c2, http.MethodGet, srv.URL, nil)
		require.NoError(t, reqErr)
		resp2, doErr := client.Do(r2)
		if doErr != nil {
			return false
		}
		ok := resp2.StatusCode == http.StatusOK
		_ = resp2.Body.Close()
		return ok
	}, 3*time.Second, 10*time.Millisecond)

	require.NoError(t, server.Close(context.Background()))
}

// -----------------------------------------------------------------------------
// Branch 6 — heartbeat on: advancing the fake clock emits ": ping\n\n".
// -----------------------------------------------------------------------------

func TestSSEServer_heartbeatOn(t *testing.T) {
	t.Parallel()

	fake := clockwork.NewFakeClock()
	const interval = 15 * time.Second
	server, err := msghttp.NewSSEServer(
		msghttp.WithHeartbeat(interval),
		msghttp.WithSSEClock(fake),
	)
	require.NoError(t, err)

	w := newCaptureWriter()
	cancel, done := serveInBackground(server, w)

	<-w.flushed // headers flushed; writer is about to register the ticker

	require.NoError(t, fake.BlockUntilContext(t.Context(), 1)) // ticker registered
	fake.Advance(interval)                                     // fire one heartbeat
	<-w.flushed                                                // ping flushed

	assert.Equal(t, []byte(": ping\n\n"), w.bytes())

	cancel()
	<-done
}

// -----------------------------------------------------------------------------
// Branch 7 — heartbeat off: advancing the clock produces no bytes.
// -----------------------------------------------------------------------------

func TestSSEServer_heartbeatOff(t *testing.T) {
	t.Parallel()

	fake := clockwork.NewFakeClock()
	server, err := msghttp.NewSSEServer(msghttp.WithSSEClock(fake))
	require.NoError(t, err)

	w := newCaptureWriter()
	cancel, done := serveInBackground(server, w)

	<-w.flushed // headers flushed

	// No ticker exists, so this advance is a no-op. Cancelling then joining the
	// writer gives a deterministic point at which "no ping was ever written"
	// can be asserted: the only thing the writer ever Writes is a frame or a
	// ping, and there were neither.
	fake.Advance(time.Hour)
	cancel()
	<-done

	assert.Empty(t, w.bytes())
}

// -----------------------------------------------------------------------------
// Branch 8 — Close joins every handler, is idempotent, and rejects afterward.
// -----------------------------------------------------------------------------

func TestSSEServer_close(t *testing.T) {
	t.Parallel()

	server, err := msghttp.NewSSEServer(msghttp.WithMaxConnections(4))
	require.NoError(t, err)

	srv := httptest.NewServer(server)
	defer srv.Close()
	client := srv.Client()

	var closers []func()
	for i := 0; i < 3; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		req, reqErr := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
		require.NoError(t, reqErr)
		resp, doErr := client.Do(req)
		require.NoError(t, doErr)
		require.Equal(t, http.StatusOK, resp.StatusCode)
		closers = append(closers, func() { cancel(); _ = resp.Body.Close() })
	}
	defer func() {
		for _, c := range closers {
			c()
		}
	}()

	// Close joins all three handlers (their derived ctx is cancelled).
	require.NoError(t, server.Close(context.Background()))

	// Idempotent: a second Close finds no connections and returns nil.
	require.NoError(t, server.Close(context.Background()))

	// Post-Close a new request is refused with 503.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	require.NoError(t, err)
	resp, err := client.Do(req)
	require.NoError(t, err)
	assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
	_ = resp.Body.Close()
}

// -----------------------------------------------------------------------------
// Branch 9 — Close with an already-expired ctx while a writer is wedged →
// ctx.Err(); the connection is still cancelled and eventually joins.
// -----------------------------------------------------------------------------

func TestSSEServer_closeExpiredCtx(t *testing.T) {
	t.Parallel()

	fake := clockwork.NewFakeClock()
	const interval = 5 * time.Second
	server, err := msghttp.NewSSEServer(
		msghttp.WithHeartbeat(interval),
		msghttp.WithSSEClock(fake),
		msghttp.WithWriteTimeout(150*time.Millisecond),
	)
	require.NoError(t, err)

	w := newBlockingWriter()
	_, done := serveInBackground(server, w)

	<-w.flushed                                                // headers flushed
	require.NoError(t, fake.BlockUntilContext(t.Context(), 1)) // ticker registered
	fake.Advance(interval)                                     // fire a ping
	<-w.entered                                                // the writer is now wedged in Write

	// Close with an already-expired ctx. The wedged Write cannot be interrupted
	// by the ctx cancel, so Close's join times out and returns ctx.Err().
	expired, cancelExpired := context.WithCancel(context.Background())
	cancelExpired()
	err = server.Close(expired)
	assert.ErrorIs(t, err, context.Canceled)

	// The connection was still cancelled: once its per-write OS deadline trips,
	// the writer exits and the handler returns — joined here so no goroutine
	// leaks.
	<-done

	// A subsequent Close now finds everything drained and returns nil.
	require.NoError(t, server.Close(context.Background()))
}

// -----------------------------------------------------------------------------
// Branch 10 — stalled reader: the per-write OS deadline reaps the wedged writer
// and Close joins it (INV-S8 / INV-S4 stalled-reader arm).
// -----------------------------------------------------------------------------

func TestSSEServer_stalledReaderDeadline(t *testing.T) {
	t.Parallel()

	fake := clockwork.NewFakeClock()
	const interval = 5 * time.Second
	server, err := msghttp.NewSSEServer(
		msghttp.WithHeartbeat(interval),
		msghttp.WithSSEClock(fake),
		msghttp.WithWriteTimeout(150*time.Millisecond),
	)
	require.NoError(t, err)

	w := newBlockingWriter()
	_, done := serveInBackground(server, w)

	<-w.flushed                                                // headers flushed
	require.NoError(t, fake.BlockUntilContext(t.Context(), 1)) // ticker registered
	fake.Advance(interval)                                     // fire a ping
	<-w.entered                                                // the writer is now wedged in Write

	// Close cancels the connection (no effect on the wedged Write) and waits.
	// The write deadline is what actually reaps the writer, letting Close join.
	start := time.Now()
	require.NoError(t, server.Close(context.Background()))
	assert.GreaterOrEqual(t, time.Since(start), 100*time.Millisecond,
		"Close should have blocked until the write deadline reaped the writer")

	<-done
}

// -----------------------------------------------------------------------------
// Branch 11 — SetWriteDeadline → errors.ErrUnsupported: a one-time latched WARN,
// and the writer proceeds without an enforced deadline (F5).
// -----------------------------------------------------------------------------

func TestSSEServer_writeDeadlineUnsupported(t *testing.T) {
	t.Parallel()

	fake := clockwork.NewFakeClock()
	const interval = 5 * time.Second
	h := &captureHandler{}
	server, err := msghttp.NewSSEServer(
		msghttp.WithHeartbeat(interval),
		msghttp.WithSSEClock(fake),
		msghttp.WithLogger(slog.New(h)),
	)
	require.NoError(t, err)

	w := newCaptureWriter()
	w.deadlineErr = errors.ErrUnsupported

	cancel, done := serveInBackground(server, w)

	<-w.flushed // headers flushed; the header write already tripped ErrUnsupported once

	// Two more heartbeats, each of which calls SetWriteDeadline again — the WARN
	// must still be emitted only once (latched).
	require.NoError(t, fake.BlockUntilContext(t.Context(), 1))
	fake.Advance(interval)
	<-w.flushed
	fake.Advance(interval)
	<-w.flushed

	cancel()
	<-done

	// The writer proceeded (pings were written) despite the unenforceable
	// deadline, and exactly one WARN was latched.
	assert.Equal(t, []byte(": ping\n\n: ping\n\n"), w.bytes())
	assert.Equal(t, 1, h.count(slog.LevelWarn))
}
