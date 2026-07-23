package msghttp

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"
)

// sseHeartbeat is the raw SSE COMMENT frame the writer emits every
// WithHeartbeat interval to keep an otherwise-idle stream's socket active. A
// line beginning with ':' is a comment in the WHATWG text/event-stream grammar
// — it is not an event and dispatches nothing on the client — so it is written
// verbatim rather than through EncodeSSEEvent (which frames events). The
// trailing blank line makes it a complete, self-contained frame.
var sseHeartbeat = []byte(": ping\n\n")

// SSEServer is a Server-Sent Events fan-out hub: an http.Handler that registers
// each GET request as a long-lived streaming connection and (from Task 5)
// broadcasts messages to every connection. Build one with NewSSEServer.
//
// A hand-built &SSEServer{} is UNSUPPORTED and nil-dereferences on first use:
// its cfg and connection map are nil, exactly as a hand-built &Outbound{}
// nil-derefs. Always construct through NewSSEServer, which resolves a validated
// Config and initializes the hub.
//
// SSEServer is safe for concurrent use: ServeHTTP, Close (and Send, Task 5) may
// be called from many goroutines at once.
type SSEServer struct {
	cfg *Config

	mu     sync.Mutex
	conns  map[*sseConn]struct{}
	closed bool
	// deadlineUnsupportedWarned latches the one-time WARN emitted when a
	// connection's ResponseWriter does not support SetWriteDeadline (INV-S8 /
	// audit F5): the per-write OS deadline is then unenforceable and the
	// stalled-reader reap is voided for this server, which the WARN makes
	// observable. Guarded by mu.
	deadlineUnsupportedWarned bool
}

// sseConn is one registered streaming connection. Its writer goroutine IS the
// request's handler goroutine (there is no extra goroutine per connection): the
// handler registers, then loops draining frames until the client disconnects,
// Close cancels it, or a write fails.
type sseConn struct {
	// frames carries live event frames from Send (Task 5) to this connection's
	// writer; capacity is WithConnectionBuffer. Unused until Task 5, but sized
	// here so the writer's select is complete.
	frames chan []byte
	// cancel cancels this connection's derived context, unblocking the writer's
	// select so Close (and, in Task 5, the disconnect policy) can reap it.
	cancel context.CancelFunc
	// done is closed by the handler on exit, AFTER it has unregistered — Close
	// waits on it to join the writer.
	done chan struct{}
}

// NewSSEServer builds an SSEServer from opts, validating them through NewConfig
// (an invalid option — e.g. WithMaxConnections(0) or WithWriteTimeout(-1) —
// returns its typed construction error and a nil server).
//
// # Delivery and topology
//
// Delivery is AT-MOST-ONCE: a frame is written to whichever connections are
// registered when it is sent, with no acknowledgement, redelivery, or durable
// buffering beyond the optional per-process replay ring (Task 5).
//
// Connected subscribers (and the replay ring) are PER-PROCESS in-memory state.
// A subscriber connected to instance A sees only messages that reach A; in a
// horizontally-scaled deployment (N instances behind a load balancer) a message
// published on instance B never reaches A's subscribers on its own. Cross-
// instance fan-out requires a shared pub/sub backbone (Redis, NATS, …) feeding
// every instance — this SSE server is the LAST hop from a process to its own
// connected browsers, not that backbone. WithMaxConnections is likewise a
// PROCESS-GLOBAL cap, not a cluster-wide or per-IP one.
//
// # Heartbeat
//
// Heartbeat is OFF by default. An idle stream behind a reverse proxy or load
// balancer with a 30–60s idle timeout is SILENTLY DROPPED without it; enable
// WithHeartbeat with an interval comfortably below the shortest idle timeout in
// the path if your deployment sits behind such a proxy (most do).
func NewSSEServer(opts ...Option) (*SSEServer, error) {
	cfg, err := NewConfig(opts...)
	if err != nil {
		return nil, err
	}
	return &SSEServer{
		cfg:   cfg,
		conns: make(map[*sseConn]struct{}),
	}, nil
}

// Interface guard: SSEServer is an http.Handler.
var _ http.Handler = (*SSEServer)(nil)

// ServeHTTP registers r as an SSE streaming connection and serves it until the
// client disconnects, Close is called, or a write fails. The guards run in a
// fixed order so their responses are deterministic:
//
//  1. a non-GET method → 405 with an Allow: GET header;
//  2. a closed server, or one already at WithMaxConnections → 503;
//  3. a ResponseWriter that is not an http.Flusher → 500 (SSE cannot stream
//     without flushing; a WARN records the misconfiguration).
//
// Once the guards pass the connection is registered and the SSE response
// headers are written and flushed — so the client sees an open stream before
// any event — after which the handler goroutine becomes this connection's
// writer loop.
func (s *SSEServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "SSE endpoint accepts GET only", http.StatusMethodNotAllowed)
		return
	}

	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		http.Error(w, "SSE server is closed", http.StatusServiceUnavailable)
		return
	}
	if len(s.conns) >= s.cfg.maxConns() {
		s.mu.Unlock()
		http.Error(w, "SSE connection limit reached", http.StatusServiceUnavailable)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		s.mu.Unlock()
		s.cfg.log().WarnContext(r.Context(),
			"msghttp: SSE ResponseWriter is not an http.Flusher; cannot stream")
		http.Error(w, "SSE streaming is unsupported by this server", http.StatusInternalServerError)
		return
	}

	// Register atomically with the cap check so the cap can never be exceeded by
	// a check-then-register race (INV-S3). The Flusher assertion above holds no
	// I/O, so doing it inside the critical section is safe.
	ctx, cancel := context.WithCancel(r.Context())
	conn := &sseConn{
		frames: make(chan []byte, s.cfg.connBuffer()),
		cancel: cancel,
		done:   make(chan struct{}),
	}
	s.conns[conn] = struct{}{}
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		delete(s.conns, conn)
		s.mu.Unlock()
		cancel()
		close(conn.done)
	}()

	s.runWriter(ctx, w, flusher, conn, r)
}

// runWriter writes the SSE preamble and then loops, writing each live frame and
// heartbeat until the connection's context is cancelled or a write fails. It
// runs on the handler goroutine — there is no separate writer goroutine.
func (s *SSEServer) runWriter(ctx context.Context, w http.ResponseWriter, flusher http.Flusher, conn *sseConn, r *http.Request) {
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")

	// The client must see the open stream before any event, so flush the
	// headers now. The per-write deadline guards this flush like any other.
	s.applyWriteDeadline(w, r)
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	// A nil ticker channel (heartbeat off) makes its select arm block forever at
	// zero cost, so the loop needs no special-case for the off state.
	var tickC <-chan time.Time
	if d := s.cfg.heartbeatInterval(); d > 0 {
		ticker := s.cfg.streamClock().NewTicker(d)
		defer ticker.Stop()
		tickC = ticker.Chan()
	}

	for {
		select {
		case <-ctx.Done():
			return
		case frame := <-conn.frames:
			if err := s.writeFrame(w, flusher, r, frame); err != nil {
				return
			}
		case <-tickC:
			if err := s.writeFrame(w, flusher, r, sseHeartbeat); err != nil {
				return
			}
		}
	}
}

// writeFrame writes one frame and flushes it, guarding the write with the
// per-write OS deadline first. Any write error (a stalled reader tripping the
// deadline, or a client that hung up) is returned so runWriter ends the loop
// and the handler unregisters — this is the mechanism that lets Close join a
// writer wedged inside Write on a full TCP window.
func (s *SSEServer) writeFrame(w http.ResponseWriter, flusher http.Flusher, r *http.Request, frame []byte) error {
	s.applyWriteDeadline(w, r)
	if _, err := w.Write(frame); err != nil {
		return err
	}
	flusher.Flush()
	return nil
}

// applyWriteDeadline installs the per-write OS deadline (INV-S8) before a
// Write/Flush. The deadline is REAL wall-clock time (time.Now()), never the
// injectable streamClock: the kernel socket deadline cannot be faked, and it is
// what actually aborts a Write wedged on a stalled reader under the mandatory
// http.Server{WriteTimeout: 0}.
//
// A ResponseWriter that does not support SetWriteDeadline (an in-memory
// recorder, or middleware that wraps the writer without an Unwrap method)
// returns errors.ErrUnsupported; the writer then proceeds best-effort — the
// deadline is simply unenforceable — but latches a one-time WARN so the voided
// protection is observable rather than silent (F5). Any other SetWriteDeadline
// error is likewise tolerated best-effort.
func (s *SSEServer) applyWriteDeadline(w http.ResponseWriter, r *http.Request) {
	rc := http.NewResponseController(w)
	if err := rc.SetWriteDeadline(time.Now().Add(s.cfg.perWriteTimeout())); err != nil && errors.Is(err, errors.ErrUnsupported) {
		s.warnWriteDeadlineUnsupported(r)
	}
}

// warnWriteDeadlineUnsupported emits the INV-S8/F5 WARN at most once per server,
// latched under mu. It runs only on the ErrUnsupported path, so the common
// (deadline-supported) write path never touches the mutex.
func (s *SSEServer) warnWriteDeadlineUnsupported(r *http.Request) {
	s.mu.Lock()
	if s.deadlineUnsupportedWarned {
		s.mu.Unlock()
		return
	}
	s.deadlineUnsupportedWarned = true
	s.mu.Unlock()

	s.cfg.log().WarnContext(r.Context(),
		"msghttp: SSE ResponseWriter does not support SetWriteDeadline; "+
			"per-write timeout is unenforceable and a stalled reader cannot be reaped for this server")
}

// Close stops the server: it marks the server closed (so new requests get 503),
// cancels every registered connection, and joins each connection's writer,
// waiting up to ctx for the joins.
//
// The join is only possible because of the per-write OS deadline (INV-S8):
// cancelling a connection's context unblocks a writer sitting in select, but a
// writer wedged inside Write on a stalled reader is reaped by the deadline, not
// the cancel. If ctx expires before every writer has exited, Close returns
// ctx.Err() — the remaining connections have already been cancelled, so it is a
// best-effort join, not an abandonment.
//
// Close is idempotent: a second call finds no connections and returns nil.
func (s *SSEServer) Close(ctx context.Context) error {
	s.mu.Lock()
	s.closed = true
	conns := make([]*sseConn, 0, len(s.conns))
	for c := range s.conns {
		conns = append(conns, c)
	}
	s.mu.Unlock()

	for _, c := range conns {
		c.cancel()
	}
	for _, c := range conns {
		select {
		case <-c.done:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}
