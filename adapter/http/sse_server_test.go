package msghttp_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/jonboulle/clockwork"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kartaladev/msgin"
	msghttp "github.com/kartaladev/msgin/adapter/http"
)

// The writer's select arm `case frame := <-conn.frames` is fed by Send: the
// fan-out, slow-client, and replay tests further down (TestSSEServer_Send_*)
// drive it end to end, so the whole writer/lifecycle path is covered here.

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

// =============================================================================
// Task 5 — Send fan-out, slow-client policy, replay ring.
// =============================================================================

// wedgeWriter models a stalled subscriber whose Write stays WEDGED until the
// test explicitly releases it — so a Send's per-connection buffer deterministically
// fills and the slow-client policy arm fires (audit MAJOR-3/F4). Unlike
// blockingWriter it does NOT honor the write deadline: SetWriteDeadline returns
// nil (no INV-S8/F5 WARN to pollute the drop-WARN assertion) and Write ignores
// the deadline entirely, blocking on release. After release every Write returns
// success, so the writer drains its buffered frames and then exits via the
// connection ctx (which the disconnect policy cancels) — the observable that
// distinguishes SlowClientDisconnect from SlowClientDrop.
type wedgeWriter struct {
	hdr     http.Header
	flushed chan struct{}
	entered chan struct{} // pulsed when Write is entered (the writer is now wedged)
	release chan struct{} // closed to unwedge Write
}

func newWedgeWriter() *wedgeWriter {
	return &wedgeWriter{
		hdr:     make(http.Header),
		flushed: make(chan struct{}, 256),
		entered: make(chan struct{}, 256),
		release: make(chan struct{}),
	}
}

func (b *wedgeWriter) Header() http.Header              { return b.hdr }
func (b *wedgeWriter) WriteHeader(int)                  {}
func (b *wedgeWriter) SetWriteDeadline(time.Time) error { return nil }

func (b *wedgeWriter) Flush() {
	select {
	case b.flushed <- struct{}{}:
	default:
	}
}

func (b *wedgeWriter) Write(p []byte) (int, error) {
	select {
	case b.entered <- struct{}{}:
	default:
	}
	<-b.release
	return len(p), nil
}

// errBrokenPipe stands in for a client that hung up: Write returns it, exactly
// as a real socket surfaces a broken connection.
var errBrokenPipe = errors.New("test: broken pipe")

// failWriter is a Flusher whose frame Writes all fail with errBrokenPipe (a
// hung-up client). Headers go through WriteHeader/Flush, not Write, so the
// header flush still pulses flushed; the first frame — replay or live — trips
// the error and ends the writer. SetWriteDeadline is supported (no F5 WARN).
type failWriter struct {
	hdr     http.Header
	flushed chan struct{}
}

func newFailWriter() *failWriter {
	return &failWriter{hdr: make(http.Header), flushed: make(chan struct{}, 8)}
}

func (f *failWriter) Header() http.Header              { return f.hdr }
func (f *failWriter) WriteHeader(int)                  {}
func (f *failWriter) SetWriteDeadline(time.Time) error { return nil }
func (f *failWriter) Write([]byte) (int, error)        { return 0, errBrokenPipe }

func (f *failWriter) Flush() {
	select {
	case f.flushed <- struct{}{}:
	default:
	}
}

// serveWithHeader runs s.ServeHTTP on a background goroutine for a GET request
// carrying header:value (e.g. Last-Event-ID), returning a cancel and a done
// channel closed when the handler exits. The test MUST join done.
func serveWithHeader(s *msghttp.SSEServer, w http.ResponseWriter, header, value string) (context.CancelFunc, <-chan struct{}) {
	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/events", nil).WithContext(ctx)
	if header != "" {
		req.Header.Set(header, value)
	}
	d := make(chan struct{})
	go func() {
		s.ServeHTTP(w, req)
		close(d)
	}()
	return cancel, d
}

// idMsg builds a message whose SSE id: field is id (via HeaderMessageID).
func idMsg(id string, payload any) msgin.Message[any] {
	return msgin.New[any](payload, msgin.WithID(id))
}

// parseQuiet parses every complete SSE event in raw, stopping at the first
// error (a trailing partial frame or clean EOF) without failing the test.
func parseQuiet(raw []byte) []msghttp.SSEEvent {
	p, err := msghttp.NewSSEParser(bytes.NewReader(raw))
	if err != nil {
		return nil
	}
	var evs []msghttp.SSEEvent
	for {
		ev, perr := p.Next()
		if perr != nil {
			return evs
		}
		evs = append(evs, ev)
	}
}

// waitEvents blocks until w has captured at least n complete SSE events, then
// returns them (parsed). It fails the test if they never arrive.
func waitEvents(t *testing.T, w *captureWriter, n int) []msghttp.SSEEvent {
	t.Helper()
	var evs []msghttp.SSEEvent
	require.Eventually(t, func() bool {
		evs = parseQuiet(w.bytes())
		return len(evs) >= n
	}, 3*time.Second, 5*time.Millisecond)
	return evs
}

// eventIDs projects the id: field of each event, for contiguity assertions.
func eventIDs(evs []msghttp.SSEEvent) []string {
	ids := make([]string, len(evs))
	for i, ev := range evs {
		ids[i] = ev.ID
	}
	return ids
}

// eventData projects each event's data payload as a string. It is the reliable
// projection for an id-less event: the SSE parser carries the last-event-id
// forward across events (WHATWG), so an id-less frame parses with the PREVIOUS
// event's id — the payload, not the parsed id, is what identifies it on the wire.
func eventData(evs []msghttp.SSEEvent) []string {
	data := make([]string, len(evs))
	for i, ev := range evs {
		data[i] = string(ev.Data)
	}
	return data
}

// -----------------------------------------------------------------------------
// Interface guard + Row 1 (zero connections) + Row 7 (post-Close).
// -----------------------------------------------------------------------------

func TestSSEServer_isOutboundAdapter(t *testing.T) {
	t.Parallel()
	var _ msgin.OutboundAdapter = (*msghttp.SSEServer)(nil)
}

func TestSSEServer_Send_zeroConnections(t *testing.T) {
	t.Parallel()

	server, err := msghttp.NewSSEServer()
	require.NoError(t, err)

	// Fire-and-forget: a Send with no registered subscribers is a no-op nil.
	assert.NoError(t, server.Send(t.Context(), idMsg("1", []byte("x"))))
}

func TestSSEServer_Send_afterClose(t *testing.T) {
	t.Parallel()

	server, err := msghttp.NewSSEServer()
	require.NoError(t, err)
	require.NoError(t, server.Close(t.Context()))

	err = server.Send(t.Context(), idMsg("1", []byte("x")))
	assert.ErrorIs(t, err, msghttp.ErrSSEServerClosed)
	assertPermanent(t, err)
}

// -----------------------------------------------------------------------------
// Row 3 — payload []byte / string / other.
// -----------------------------------------------------------------------------

func TestSSEServer_Send_payloadTypes(t *testing.T) {
	t.Parallel()

	type testCase struct {
		name    string
		payload any
		assert  func(t *testing.T, sendErr error, delivered []msghttp.SSEEvent)
	}

	cases := []testCase{
		{
			name:    "byte slice payload is delivered verbatim",
			payload: []byte("bytes-payload"),
			assert: func(t *testing.T, sendErr error, delivered []msghttp.SSEEvent) {
				require.NoError(t, sendErr)
				require.Len(t, delivered, 1)
				assert.Equal(t, []byte("bytes-payload"), delivered[0].Data)
			},
		},
		{
			name:    "string payload is delivered as bytes",
			payload: "string-payload",
			assert: func(t *testing.T, sendErr error, delivered []msghttp.SSEEvent) {
				require.NoError(t, sendErr)
				require.Len(t, delivered, 1)
				assert.Equal(t, []byte("string-payload"), delivered[0].Data)
			},
		},
		{
			name:    "unsupported payload is a permanent error, nothing delivered",
			payload: 42,
			assert: func(t *testing.T, sendErr error, delivered []msghttp.SSEEvent) {
				assert.ErrorIs(t, sendErr, msghttp.ErrUnsupportedPayload)
				assertPermanent(t, sendErr)
				assert.Empty(t, delivered)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			server, err := msghttp.NewSSEServer()
			require.NoError(t, err)

			w := newCaptureWriter()
			cancel, done := serveInBackground(server, w)
			<-w.flushed // headers flushed; the subscriber is registered

			sendErr := server.Send(t.Context(), idMsg("1", tc.payload))

			var delivered []msghttp.SSEEvent
			if sendErr == nil {
				delivered = waitEvents(t, w, 1)
			}
			tc.assert(t, sendErr, delivered)

			cancel()
			<-done
		})
	}
}

// -----------------------------------------------------------------------------
// Row 2 — fan-out to N healthy subscribers: each receives the identical frame.
// -----------------------------------------------------------------------------

func TestSSEServer_Send_fanout(t *testing.T) {
	t.Parallel()

	const n = 4
	server, err := msghttp.NewSSEServer()
	require.NoError(t, err)

	writers := make([]*captureWriter, n)
	cancels := make([]context.CancelFunc, n)
	dones := make([]<-chan struct{}, n)
	for i := range writers {
		writers[i] = newCaptureWriter()
		cancels[i], dones[i] = serveInBackground(server, writers[i])
		<-writers[i].flushed // each subscriber registered
	}

	require.NoError(t, server.Send(t.Context(), idMsg("evt-1", []byte("broadcast"))))

	for i := range writers {
		delivered := waitEvents(t, writers[i], 1)
		require.Len(t, delivered, 1)
		assert.Equal(t, "evt-1", delivered[0].ID)
		assert.Equal(t, []byte("broadcast"), delivered[0].Data)
		// Every subscriber received the byte-identical encoded frame.
		assert.Equal(t, writers[0].bytes(), writers[i].bytes())
	}

	for i := range writers {
		cancels[i]()
		<-dones[i]
	}
}

// -----------------------------------------------------------------------------
// Row 4 — hostile HeaderSSEEventName → Permanent(ErrInvalidEventField), zero
// subscriber bytes (INV-S1).
// -----------------------------------------------------------------------------

func TestSSEServer_Send_hostileHeader(t *testing.T) {
	t.Parallel()

	server, err := msghttp.NewSSEServer()
	require.NoError(t, err)

	w := newCaptureWriter()
	cancel, done := serveInBackground(server, w)
	<-w.flushed // headers flushed; subscriber registered

	hostile := idMsg("1", []byte("x")).WithHeader(msghttp.HeaderSSEEventName, "evil\nevent: forged")
	sendErr := server.Send(t.Context(), hostile)

	assert.ErrorIs(t, sendErr, msghttp.ErrInvalidEventField)
	assertPermanent(t, sendErr)

	// INV-S1: the injection is rejected BEFORE any fan-out, so zero event bytes
	// ever reached the subscriber. A follow-up good Send proves the subscriber
	// was live all along (not merely slow), and that its first bytes are the
	// good frame — never the hostile one.
	require.NoError(t, server.Send(t.Context(), idMsg("2", []byte("good"))))
	delivered := waitEvents(t, w, 1)
	require.Len(t, delivered, 1)
	assert.Equal(t, "2", delivered[0].ID)
	assert.Equal(t, []byte("good"), delivered[0].Data)

	cancel()
	<-done
}

// -----------------------------------------------------------------------------
// Row 5 — full buffer + SlowClientDrop: WARN latched once, healthy peer
// unaffected (INV-S2). Uses the wedge writer so the buffer deterministically
// fills; a real loopback would drain too fast (MAJOR-3).
// -----------------------------------------------------------------------------

func TestSSEServer_Send_dropPolicy(t *testing.T) {
	t.Parallel()

	const buffer = 2
	h := &captureHandler{}
	server, err := msghttp.NewSSEServer(
		msghttp.WithConnectionBuffer(buffer),
		msghttp.WithSlowClientPolicy(msghttp.SlowClientDrop),
		msghttp.WithLogger(slog.New(h)),
	)
	require.NoError(t, err)

	stalled := newWedgeWriter()
	_, stalledDone := serveInBackground(server, stalled)
	<-stalled.flushed // stalled headers flushed

	healthy := newCaptureWriter()
	healthyCancel, healthyDone := serveInBackground(server, healthy)
	<-healthy.flushed // healthy headers flushed

	// Send #1: the stalled writer pulls it and wedges inside Write; its buffer is
	// now empty and it can never drain again until released. Each Send's
	// enqueue into the stalled buffer is synchronous, so the stalled backlog is
	// fully determined by how many Sends have RETURNED. Between Sends we drain the
	// healthy peer (waitEvents) so it never itself backlogs — only the wedged
	// subscriber ever fills, keeping the arm under test unambiguous.
	const total = buffer + 4
	require.NoError(t, server.Send(t.Context(), idMsg("1", []byte("e1"))))
	<-stalled.entered
	waitEvents(t, healthy, 1)

	// Fill the stalled buffer to capacity, then overflow it: the first overflow
	// WARN-latches, the rest DEBUG. Every Send returns promptly (INV-S2: Send
	// never blocks on the wedged subscriber).
	for i := 2; i <= total; i++ {
		require.NoError(t, server.Send(t.Context(), idMsg(itoa(i), []byte("d"+itoa(i)))))
		waitEvents(t, healthy, i)
	}

	// The healthy peer received EVERY event — one slow subscriber never gaps
	// another (fault isolation).
	assert.Len(t, parseQuiet(healthy.bytes()), total)

	// Exactly one WARN latched for the stalled connection, regardless of how many
	// events were dropped. No other WARN can fire here: the wedge writer supports
	// SetWriteDeadline and is a Flusher, so neither the F5 nor the non-Flusher
	// WARN paths are taken.
	assert.Equal(t, 1, h.count(slog.LevelWarn))

	// Teardown: release the wedged writer, cancel everything, join both handlers.
	close(stalled.release)
	healthyCancel()
	require.NoError(t, server.Close(t.Context()))
	<-stalledDone
	<-healthyDone
}

// -----------------------------------------------------------------------------
// Row 6 — full buffer + SlowClientDisconnect: the stalled connection is
// cancelled (its handler returns), healthy peer unaffected.
// -----------------------------------------------------------------------------

func TestSSEServer_Send_disconnectPolicy(t *testing.T) {
	t.Parallel()

	const buffer = 2
	server, err := msghttp.NewSSEServer(
		msghttp.WithConnectionBuffer(buffer),
		msghttp.WithSlowClientPolicy(msghttp.SlowClientDisconnect),
	)
	require.NoError(t, err)

	stalled := newWedgeWriter()
	_, stalledDone := serveInBackground(server, stalled)
	<-stalled.flushed

	healthy := newCaptureWriter()
	healthyCancel, healthyDone := serveInBackground(server, healthy)
	<-healthy.flushed

	// Wedge the stalled writer, fill its buffer, then overflow once: the policy
	// cancels the stalled connection. We drain the healthy peer between Sends so
	// it never backlogs (and so is never itself disconnected) — only the wedged
	// subscriber fills.
	require.NoError(t, server.Send(t.Context(), idMsg("1", []byte("e1"))))
	<-stalled.entered
	waitEvents(t, healthy, 1)
	for i := 2; i <= buffer+2; i++ { // fill to cap, then one overflow -> disconnect
		require.NoError(t, server.Send(t.Context(), idMsg(itoa(i), []byte("d"+itoa(i)))))
		waitEvents(t, healthy, i)
	}

	// Release the wedge with a SUCCESSFUL write: the writer flushes and returns
	// to its select loop, where the cancelled ctx (and only that) ends it. If the
	// policy had NOT cancelled the connection, the writer would drain its buffer
	// and block in select forever — so a returning handler proves the disconnect.
	close(stalled.release)
	select {
	case <-stalledDone:
	case <-time.After(3 * time.Second):
		t.Fatal("SlowClientDisconnect: the stalled handler never returned")
	}

	// The healthy peer received every event, unaffected by its peer's disconnect.
	assert.Len(t, parseQuiet(healthy.bytes()), buffer+2)

	healthyCancel()
	require.NoError(t, server.Close(t.Context()))
	<-healthyDone
}

// -----------------------------------------------------------------------------
// Row 8 — replay OFF + Last-Event-ID: live-only, no replay bytes.
// -----------------------------------------------------------------------------

func TestSSEServer_Send_replayOff(t *testing.T) {
	t.Parallel()

	server, err := msghttp.NewSSEServer() // replay off by default
	require.NoError(t, err)

	w := newCaptureWriter()
	cancel, done := serveWithHeader(server, w, "Last-Event-ID", "3")
	<-w.flushed

	require.NoError(t, server.Send(t.Context(), idMsg("4", []byte("d4"))))
	require.NoError(t, server.Send(t.Context(), idMsg("5", []byte("d5"))))

	delivered := waitEvents(t, w, 2)
	assert.Equal(t, []string{"4", "5"}, eventIDs(delivered)) // no replay of 1..3

	cancel()
	<-done
}

// -----------------------------------------------------------------------------
// Row 9 — replay ON, id in ring → tail replayed then live, contiguous (INV-S5).
// Forced ordering: the barrier Send RETURNS before the reconnect snapshots the
// ring, so the appended event is provably present (audit MAJOR-2/F2 — assert
// contiguity, never a measured interleave count).
// -----------------------------------------------------------------------------

func TestSSEServer_Send_replayContiguous(t *testing.T) {
	t.Parallel()

	server, err := msghttp.NewSSEServer(msghttp.WithReplayBuffer(8))
	require.NoError(t, err)

	// Subscriber A observes 1..3, then disconnects.
	a := newCaptureWriter()
	cancelA, doneA := serveInBackground(server, a)
	<-a.flushed
	for _, id := range []string{"1", "2", "3"} {
		require.NoError(t, server.Send(t.Context(), idMsg(id, []byte("d"+id))))
	}
	got := waitEvents(t, a, 3)
	require.Equal(t, []string{"1", "2", "3"}, eventIDs(got))
	cancelA()
	<-doneA // A is fully unregistered

	// BARRIER: this Send RETURNS, so id "4" is provably in the ring before any
	// reconnect snapshot can run.
	require.NoError(t, server.Send(t.Context(), idMsg("4", []byte("d4"))))

	// B reconnects with Last-Event-ID=3: it must replay the ring tail after id 3
	// (i.e. id 4), then receive live 5,6 — contiguous 4,5,6, no gap, no dup.
	b := newCaptureWriter()
	cancelB, doneB := serveWithHeader(server, b, "Last-Event-ID", "3")
	<-b.flushed
	require.NoError(t, server.Send(t.Context(), idMsg("5", []byte("d5"))))
	require.NoError(t, server.Send(t.Context(), idMsg("6", []byte("d6"))))

	delivered := waitEvents(t, b, 3)
	assert.Equal(t, []string{"4", "5", "6"}, eventIDs(delivered))

	cancelB()
	<-doneB
}

// -----------------------------------------------------------------------------
// Row 10 — replay ON, unknown id → live-only (documented best-effort).
// -----------------------------------------------------------------------------

func TestSSEServer_Send_replayUnknownID(t *testing.T) {
	t.Parallel()

	server, err := msghttp.NewSSEServer(msghttp.WithReplayBuffer(8))
	require.NoError(t, err)

	// Populate the ring with 1,2 (no subscriber yet).
	require.NoError(t, server.Send(t.Context(), idMsg("1", []byte("d1"))))
	require.NoError(t, server.Send(t.Context(), idMsg("2", []byte("d2"))))

	// Reconnect naming an id that was never in the ring → no replay.
	w := newCaptureWriter()
	cancel, done := serveWithHeader(server, w, "Last-Event-ID", "999")
	<-w.flushed
	require.NoError(t, server.Send(t.Context(), idMsg("3", []byte("d3"))))

	delivered := waitEvents(t, w, 1)
	assert.Equal(t, []string{"3"}, eventIDs(delivered)) // live-only

	cancel()
	<-done
}

// -----------------------------------------------------------------------------
// Row 11 — replay ON, ring past cap → oldest evicted (resume from evicted id =
// live-only; resume from a retained id still replays).
// -----------------------------------------------------------------------------

func TestSSEServer_Send_replayEviction(t *testing.T) {
	t.Parallel()

	server, err := msghttp.NewSSEServer(msghttp.WithReplayBuffer(2))
	require.NoError(t, err)

	// Ring cap 2; sending 1,2,3 evicts the oldest (1). Ring == {2,3}.
	for _, id := range []string{"1", "2", "3"} {
		require.NoError(t, server.Send(t.Context(), idMsg(id, []byte("d"+id))))
	}

	// Resume from the EVICTED id 1 → live-only.
	evicted := newCaptureWriter()
	cancelE, doneE := serveWithHeader(server, evicted, "Last-Event-ID", "1")
	<-evicted.flushed

	// Resume from the RETAINED id 2 → replays the ring tail after 2 (id 3).
	retained := newCaptureWriter()
	cancelR, doneR := serveWithHeader(server, retained, "Last-Event-ID", "2")
	<-retained.flushed

	require.NoError(t, server.Send(t.Context(), idMsg("4", []byte("d4"))))

	assert.Equal(t, []string{"4"}, eventIDs(waitEvents(t, evicted, 1)))       // 1 gone
	assert.Equal(t, []string{"3", "4"}, eventIDs(waitEvents(t, retained, 2))) // 2,3 kept

	cancelE()
	<-doneE
	cancelR()
	<-doneR
}

// -----------------------------------------------------------------------------
// Row 12 — an id-less event fans out live but NEVER enters the ring.
// -----------------------------------------------------------------------------

func TestSSEServer_Send_idlessNotRinged(t *testing.T) {
	t.Parallel()

	server, err := msghttp.NewSSEServer(msghttp.WithReplayBuffer(8))
	require.NoError(t, err)

	// A live subscriber to prove the id-less event still fans out.
	a := newCaptureWriter()
	cancelA, doneA := serveInBackground(server, a)
	<-a.flushed

	require.NoError(t, server.Send(t.Context(), idMsg("1", []byte("d1"))))
	// An id-less event: NewMessage with empty headers → msg.ID() == "".
	idless := msgin.NewMessage[any]([]byte("idless"), msgin.NewHeaders(nil))
	require.NoError(t, server.Send(t.Context(), idless))
	require.NoError(t, server.Send(t.Context(), idMsg("2", []byte("d2"))))

	live := waitEvents(t, a, 3)
	require.Len(t, live, 3)
	// The id-less event fanned out live (its payload is present between the two
	// id-bearing events). Its parsed id inherits the prior event's per SSE rules,
	// so the payload sequence — not the id sequence — is the on-the-wire proof.
	assert.Equal(t, []string{"d1", "idless", "d2"}, eventData(live))

	// B reconnects at id 1: the ring holds only the id-bearing events, so the
	// replay tail after 1 is [2] — the id-less event is absent from the ring.
	b := newCaptureWriter()
	cancelB, doneB := serveWithHeader(server, b, "Last-Event-ID", "1")
	<-b.flushed
	require.NoError(t, server.Send(t.Context(), idMsg("3", []byte("d3"))))

	delivered := waitEvents(t, b, 2)
	assert.Equal(t, []string{"2", "3"}, eventIDs(delivered)) // no id-less frame replayed

	cancelA()
	<-doneA
	cancelB()
	<-doneB
}

// -----------------------------------------------------------------------------
// Writer exits on a write error — for BOTH a replay frame and a live frame:
// a hung-up client's write failure ends the writer and unregisters it, so Close
// can join it (the same reap mechanism the live and replay paths share).
// -----------------------------------------------------------------------------

func TestSSEServer_writeErrorEndsWriter(t *testing.T) {
	t.Parallel()

	type testCase struct {
		name    string
		opts    []msghttp.Option
		lastID  string                                   // Last-Event-ID header; "" = none
		prelude func(t *testing.T, s *msghttp.SSEServer) // populate the ring before connect
		live    func(t *testing.T, s *msghttp.SSEServer) // send a live frame after connect
	}

	cases := []testCase{
		{
			name: "a live frame write error ends the writer",
			live: func(t *testing.T, s *msghttp.SSEServer) {
				require.NoError(t, s.Send(t.Context(), idMsg("1", []byte("x"))))
			},
		},
		{
			name:   "a replay frame write error ends the writer",
			opts:   []msghttp.Option{msghttp.WithReplayBuffer(4)},
			lastID: "1",
			prelude: func(t *testing.T, s *msghttp.SSEServer) {
				require.NoError(t, s.Send(t.Context(), idMsg("1", []byte("a"))))
				require.NoError(t, s.Send(t.Context(), idMsg("2", []byte("b"))))
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			server, err := msghttp.NewSSEServer(tc.opts...)
			require.NoError(t, err)
			if tc.prelude != nil {
				tc.prelude(t, server)
			}

			w := newFailWriter()
			var (
				cancel context.CancelFunc
				done   <-chan struct{}
			)
			if tc.lastID != "" {
				cancel, done = serveWithHeader(server, w, "Last-Event-ID", tc.lastID)
			} else {
				cancel, done = serveInBackground(server, w)
			}
			defer cancel()

			if tc.live != nil {
				<-w.flushed // headers flushed; the writer is in its select loop
				tc.live(t, server)
			}

			// The write failure ends the writer and its handler returns on its own,
			// no cancel needed.
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				t.Fatal("the writer did not exit after a frame write error")
			}
		})
	}
}

// itoa is a tiny int→decimal helper for building sequential event ids without
// pulling strconv into the assertions.
func itoa(n int) string { return strconv.Itoa(n) }
