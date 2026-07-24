package msghttp

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/kartaladev/msgin"
)

// sseHeartbeat is the raw SSE COMMENT frame the writer emits every
// WithHeartbeat interval to keep an otherwise-idle stream's socket active. A
// line beginning with ':' is a comment in the WHATWG text/event-stream grammar
// — it is not an event and dispatches nothing on the client — so it is written
// verbatim rather than through EncodeSSEEvent (which frames events). The
// trailing blank line makes it a complete, self-contained frame.
var sseHeartbeat = []byte(": ping\n\n")

// SSEServer is a Server-Sent Events fan-out hub: an http.Handler that registers
// each GET request as a long-lived streaming connection and, via Send,
// broadcasts messages to every connection. Build one with NewSSEServer.
//
// A hand-built &SSEServer{} is UNSUPPORTED and nil-dereferences on first use:
// its cfg and connection map are nil, exactly as a hand-built &Outbound{}
// nil-derefs. Always construct through NewSSEServer, which resolves a validated
// Config and initializes the hub.
//
// SSEServer is safe for concurrent use: ServeHTTP, Close, and Send may be
// called from many goroutines at once.
type SSEServer struct {
	cfg *Config

	mu     sync.Mutex
	conns  map[*sseConn]struct{}
	closed bool
	// ring is the replay buffer: the most recent id-bearing event frames, in
	// send order, capped at WithReplayBuffer entries (empty when replay is off).
	// A reconnecting client that supplies a Last-Event-ID naming a ring entry has
	// the frames after it snapshotted into its sseConn.replay at register time —
	// all under mu, atomically with the registration, so no Send can interleave
	// between the snapshot and the insert (INV-S5). Guarded by mu.
	ring []ringEntry
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
	// frames carries live event frames from Send to this connection's writer;
	// capacity is WithConnectionBuffer.
	frames chan []byte
	// cancel cancels this connection's derived context, unblocking the writer's
	// select so Close and the SlowClientDisconnect policy can reap it.
	cancel context.CancelFunc
	// done is closed by the handler on exit, AFTER it has unregistered — Close
	// waits on it to join the writer.
	done chan struct{}
	// replay is the snapshot of ring frames (after the client's Last-Event-ID)
	// taken atomically at register time; the writer writes them, in order, before
	// entering its live-frame select loop. nil/empty means live-only. It is set
	// once under mu at registration and thereafter read only by this connection's
	// own writer goroutine, so it needs no further synchronization.
	replay [][]byte
	// remote is the connection's RemoteAddr, captured at register for the
	// slow-client drop WARN. Read only after registration; no synchronization.
	remote string
	// warned latches the one-time SlowClientDrop WARN for this connection, and
	// dropped counts how many events have been dropped for it (surfaced at DEBUG
	// on each further drop). Both are guarded by SSEServer.mu.
	warned  bool
	dropped int
}

// ringEntry is one replay-buffer slot: an event's id and its fully-encoded SSE
// frame. Only id-bearing events are ringed (an id-less frame can never be
// resumed to), so id is always non-empty.
type ringEntry struct {
	id    string
	frame []byte
}

// NewSSEServer builds an SSEServer from opts, validating them through NewConfig
// (an invalid option — e.g. WithMaxConnections(0) or WithWriteTimeout(-1) —
// returns its typed construction error and a nil server).
//
// # Delivery and topology
//
// Delivery is AT-MOST-ONCE: a frame is written to whichever connections are
// registered when it is sent, with no acknowledgement, redelivery, or durable
// buffering beyond the optional per-process replay ring.
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

// Interface guards: SSEServer is an http.Handler (its inbound streaming side)
// and a msgin.OutboundAdapter (its Send fan-out side).
var (
	_ http.Handler          = (*SSEServer)(nil)
	_ msgin.OutboundAdapter = (*SSEServer)(nil)
)

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
		remote: r.RemoteAddr,
	}
	// Snapshot the replay tail and register in ONE critical section (INV-S5): a
	// Send that appends to the ring is serialized against this whole block by mu,
	// so it either lands in the snapshot (its id is in the ring, visible here) or
	// it lands in conn.frames after the insert below — never lost in a gap
	// between the two. Replay is best-effort: an unset ring, an absent
	// Last-Event-ID, or an id already evicted all yield an empty (live-only)
	// snapshot.
	if s.cfg.replaySize() > 0 {
		if lastID := r.Header.Get("Last-Event-ID"); lastID != "" {
			conn.replay = s.replayAfter(lastID)
		}
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

	// Replay the ring tail (snapshotted at register, INV-S5) before any live
	// frame, so a resuming client sees a contiguous stream: the events after its
	// Last-Event-ID first, then whatever Send delivers next. Each replay frame
	// goes through the same deadline→write→flush→error-exit path as a live frame,
	// so a client that hangs up mid-replay is reaped identically.
	for _, frame := range conn.replay {
		if err := s.writeFrame(w, flusher, r, frame); err != nil {
			return
		}
	}

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
// The join is only possible because of the per-write OS deadline:
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

// sseSlowClientDropMsg is the message logged (WARN on the first drop for a
// connection, DEBUG on each subsequent one) when SlowClientDrop discards an
// event for a connection whose buffer is full.
const sseSlowClientDropMsg = "msghttp: SSE slow client; dropping event"

// dropNotice is a deferred slow-client-drop log record: the observability is
// decided under mu (which drop latches the WARN, its running count) but EMITTED
// after mu is released, so a slow slog handler can never stall the broadcast the
// way holding mu across the log call would.
type dropNotice struct {
	remote string
	count  int
	warn   bool // true = first drop for the connection (WARN); false = DEBUG
}

// Send fans msg out to every currently-registered connection as one SSE event,
// non-blocking per connection: it encodes the event ONCE, then does a
// non-blocking enqueue into each connection's bounded buffer, applying
// WithSlowClientPolicy to any connection whose buffer is full. It never blocks
// on, and never grows memory unboundedly for, a slow or dead subscriber, and
// returns promptly — including with zero connections, where it is a
// fire-and-forget no-op returning nil.
//
// Delivery is AT-MOST-ONCE: there is no acknowledgement or redelivery, and under
// the default SlowClientDrop a full-buffer event is dropped for that one
// connection (every other connection still receives it). ctx is used only for
// the drop-observability log records; the enqueue itself cannot block, so there
// is nothing for a cancelled ctx to interrupt.
//
// Errors are Permanent (a retry cannot fix them):
//
//   - a payload that is neither []byte nor string → Permanent(ErrUnsupportedPayload);
//   - an id or event name carrying CR/LF/NUL → Permanent(ErrInvalidEventField),
//     with ZERO bytes reaching any subscriber (the injection is rejected before
//     any fan-out; a CR/LF/CRLF in the payload is instead normalized into data:
//     framing by the encoder, so it too can never forge fields into a stream);
//   - a Send after Close → Permanent(ErrSSEServerClosed).
//
// When WithReplayBuffer is enabled, an event with a non-empty id is appended to
// the replay ring (evicting the oldest past the cap) so a reconnecting client
// can resume from it. An id-less event still fans out live but is NEVER ringed —
// it has no id a Last-Event-ID could name.
func (s *SSEServer) Send(ctx context.Context, msg msgin.Message[any]) error {
	ev, err := SSEEventFromMessage(msg, s.cfg)
	if err != nil {
		return msgin.Permanent(err)
	}
	// EncodeSSEEvent cannot fail here: SSEEventFromMessage already validated ev's
	// id and name (the sole ErrInvalidEventField source — Data is never
	// validated), and a bytes.Buffer write never errors. The error is
	// structurally nil, so it is deliberately dropped rather than guarded by a
	// branch no caller can reach.
	var buf bytes.Buffer
	_ = EncodeSSEEvent(&buf, ev)
	frame := buf.Bytes()

	var notices []dropNotice
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return msgin.Permanent(ErrSSEServerClosed)
	}
	if s.cfg.replaySize() > 0 && ev.ID != "" {
		s.appendRing(ringEntry{id: ev.ID, frame: frame})
	}
	for conn := range s.conns {
		select {
		case conn.frames <- frame:
			continue
		default:
		}
		// Buffer full: this connection is slow. Apply the policy WITHOUT ever
		// blocking on the channel — a blocking send here would let one slow client
		// stall the whole broadcast.
		if s.cfg.slowPolicy() == SlowClientDisconnect {
			conn.cancel()
			continue
		}
		// SlowClientDrop: drop this one event for this one connection, latching a
		// one-time WARN (running count at DEBUG thereafter). The log record is
		// emitted after mu is released.
		conn.dropped++
		notices = append(notices, dropNotice{remote: conn.remote, count: conn.dropped, warn: !conn.warned})
		conn.warned = true
	}
	s.mu.Unlock()

	for _, n := range notices {
		if n.warn {
			s.cfg.log().WarnContext(ctx, sseSlowClientDropMsg,
				slog.String("remote", n.remote), slog.Int("dropped", n.count))
			continue
		}
		s.cfg.log().DebugContext(ctx, sseSlowClientDropMsg,
			slog.String("remote", n.remote), slog.Int("dropped", n.count))
	}
	return nil
}

// appendRing appends e to the replay ring, evicting the oldest entries so the
// ring never exceeds WithReplayBuffer. It shifts surviving entries down to the
// front (rather than reslicing off the head) so the backing array does not
// retain evicted frames or grow its offset unboundedly. Caller holds mu.
func (s *SSEServer) appendRing(e ringEntry) {
	if max := s.cfg.replaySize(); len(s.ring) >= max {
		n := copy(s.ring, s.ring[len(s.ring)-max+1:])
		s.ring = s.ring[:n]
	}
	s.ring = append(s.ring, e)
}

// replayAfter returns copies of the ring frames AFTER the last entry whose id
// equals lastID — the events a client resuming from lastID has not yet seen. It
// scans for the LAST match so that, if a flow bug produced duplicate ids, resume
// picks the most recent occurrence (resume assumes unique HeaderMessageIDs). An
// unknown or already-evicted id yields nil (live-only, best-effort). Caller
// holds mu; the returned slice is a fresh snapshot the caller's connection owns,
// so later ring mutation cannot disturb it. Frames themselves are never mutated
// after creation, so copying the slice of references is sufficient.
func (s *SSEServer) replayAfter(lastID string) [][]byte {
	idx := -1
	for i := range s.ring {
		if s.ring[i].id == lastID {
			idx = i
		}
	}
	if idx < 0 {
		return nil
	}
	tail := s.ring[idx+1:]
	frames := make([][]byte, len(tail))
	for i := range tail {
		frames[i] = tail[i].frame
	}
	return frames
}
