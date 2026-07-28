package msghttp_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jonboulle/clockwork"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kartaladev/msgin"
	msghttp "github.com/kartaladev/msgin/adapter/http"
	"github.com/kartaladev/msgin/endpoint"
)

// =============================================================================
// Plan 026 Task 4 — the cross-phase e2e the two SSE plans (025 server, 026
// client) exist for: a real SSEServer feeding a real SSEClient over a real
// httptest.Server, proving Last-Event-ID resume end to end (INV-S5 x INV-C4).
//
// Helpers below (connSignalWriter, sseConnHandle, newKillableSSEHandler,
// recvClientEventIDs) are local to this file; idMsg, parkOnBackoff,
// sseAttempt, and newSSEStubServer are reused from sse_server_test.go /
// sseclient_test.go (same package).
// =============================================================================

// connSignalWriter wraps a real http.ResponseWriter (from an httptest.Server
// connection) so a test can observe the moment its FIRST Flush happens.
// SSEServer's runWriter writes and flushes the SSE response headers BEFORE
// entering its writer loop (and before any replay frame), so a closed
// registered channel here means "this connection is now registered on the
// server, and any replay snapshot has already been taken." Unwrap lets
// http.ResponseController see through to the real writer's own
// Flusher/SetWriteDeadline support (net/http's server response implements
// both directly), so the per-write OS deadline still applies exactly as it
// would with no wrapper.
type connSignalWriter struct {
	http.ResponseWriter
	once       sync.Once
	registered chan struct{}
}

func (w *connSignalWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
	w.once.Do(func() { close(w.registered) })
}

func (w *connSignalWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// sseConnHandle is one connection's kill/join handles, handed to the test
// over newKillableSSEHandler's handles channel as each request arrives.
type sseConnHandle struct {
	// cancel ends THIS connection only, for the first connection: it derives
	// a child of the request's own context, so cancelling it ends this one
	// ServeHTTP call (SSEServer's runWriter returns on ctx.Done()) without
	// touching the server, the replay ring, or any other connection — the
	// documented mechanism for "kill the connection without killing the
	// server" (SSEServer.Close would tear down every connection AND the
	// server, breaking replay). A no-op for every connection after the
	// first, since only the first is independently killable here.
	cancel context.CancelFunc
	// registered closes once the SSE response headers have been flushed.
	registered chan struct{}
	// done closes once ServeHTTP has returned for this connection (fully
	// unregistered from the server).
	done chan struct{}
	// lastEventID is the literal Last-Event-ID request header this
	// connection arrived with (empty for a first connection with no prior
	// stream to resume from).
	lastEventID string
}

// newKillableSSEHandler wraps server so the FIRST connection's context is
// independently cancellable, while every later connection is served on its
// own natural request context. One *sseConnHandle is sent on handles per
// incoming request, in arrival order.
func newKillableSSEHandler(server *msghttp.SSEServer, handles chan<- *sseConnHandle) http.HandlerFunc {
	var n atomic.Int32
	return func(w http.ResponseWriter, r *http.Request) {
		h := &sseConnHandle{
			registered:  make(chan struct{}),
			done:        make(chan struct{}),
			lastEventID: r.Header.Get("Last-Event-ID"),
		}
		sw := &connSignalWriter{ResponseWriter: w, registered: h.registered}

		reqCtx := r.Context()
		if n.Add(1) == 1 {
			cctx, cancel := context.WithCancel(reqCtx)
			h.cancel = cancel
			reqCtx = cctx
		} else {
			h.cancel = func() {}
		}
		handles <- h

		defer close(h.done)
		server.ServeHTTP(sw, r.WithContext(reqCtx))
	}
}

// recvClientEventIDs blocks until n msgin.Deliveries have arrived on out,
// returning their HeaderSSEEventID values in arrival order. It fails the test
// if any one delivery does not arrive within a bounded window.
func recvClientEventIDs(t *testing.T, out <-chan msgin.Delivery, n int) []string {
	t.Helper()
	ids := make([]string, 0, n)
	for i := 0; i < n; i++ {
		select {
		case d := <-out:
			id, _ := d.Msg.Headers().String(msghttp.HeaderSSEEventID)
			ids = append(ids, id)
		case <-time.After(3 * time.Second):
			t.Fatalf("timed out waiting for client event %d/%d", i+1, n)
		}
	}
	return ids
}

// runResumeScenario drives the shared resume-e2e script against an SSEServer
// built with serverOpts:
//
//  1. connect (conn #1), receive events 1,2,3;
//  2. kill conn #1 SERVER-SIDE (not the whole server) and wait for it to
//     fully unregister;
//  3. send a "gap" event (id 4) while NO connection is registered — with
//     replay ON this lands only in the ring; with replay OFF it is simply
//     gone (Send is fire-and-forget over zero subscribers);
//  4. release the client's fake-clock backoff wait so it reconnects,
//     carrying Last-Event-ID: 3;
//  5. once conn #2 has registered (its replay snapshot, if any, already
//     taken), send two more live events (5, 6);
//  6. read exactly afterCount events and return them alongside the 1,2,3
//     read from step 1, for the caller to assert contiguity (or its
//     documented absence) on, plus the literal Last-Event-ID header conn #2
//     arrived with (the reconnect request), so a resume regression can be
//     caught directly rather than only via the downstream contiguity check.
func runResumeScenario(t *testing.T, serverOpts []msghttp.Option, afterCount int) (before, after []string, conn2LastEventID string) {
	t.Helper()

	server, err := msghttp.NewSSEServer(serverOpts...)
	require.NoError(t, err)

	handles := make(chan *sseConnHandle, 4)
	ts := httptest.NewServer(newKillableSSEHandler(server, handles))
	defer ts.Close()

	clk := clockwork.NewFakeClock()
	client, err := msghttp.NewSSEClient(ts.URL, msghttp.WithSSEClock(clk))
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	out := make(chan msgin.Delivery, 16)
	done := make(chan error, 1)
	go func() { done <- client.Stream(ctx, out) }()

	conn1 := <-handles
	<-conn1.registered // headers flushed; conn1 registered

	for _, id := range []string{"1", "2", "3"} {
		require.NoError(t, server.Send(t.Context(), idMsg(id, []byte("d"+id))))
	}
	before = recvClientEventIDs(t, out, 3)

	// Kill connection #1 server-side, then wait for it to fully unregister
	// before the barrier Send, so id "4" cannot double-deliver to a
	// not-yet-torn-down conn1.
	conn1.cancel()
	<-conn1.done

	// The barrier event: sent while zero connections are registered.
	require.NoError(t, server.Send(t.Context(), idMsg("4", []byte("d4"))))

	// Release the client's backoff wait so it reconnects, carrying
	// Last-Event-ID: 3.
	parkOnBackoff(t, clk)
	clk.Advance(500 * time.Millisecond) // defaultReconnectMin

	conn2 := <-handles
	<-conn2.registered // conn2 registered; any replay snapshot already taken

	for _, id := range []string{"5", "6"} {
		require.NoError(t, server.Send(t.Context(), idMsg(id, []byte("d"+id))))
	}

	after = recvClientEventIDs(t, out, afterCount)

	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Stream did not return after cancel")
	}
	return before, after, conn2.lastEventID
}

// TestSSEClient_resumesFromSSEServerAcrossReconnect is THE test the two SSE
// plans exist for (INV-S5 x INV-C4): a real SSEServer feeding a real
// SSEClient over a real httptest.Server, proving Last-Event-ID resume across
// a server-side connection kill and client reconnect.
func TestSSEClient_resumesFromSSEServerAcrossReconnect(t *testing.T) {
	t.Parallel()

	type testCase struct {
		name       string
		serverOpts []msghttp.Option
		afterCount int
		assert     func(t *testing.T, before, after []string, conn2LastEventID string)
	}

	cases := []testCase{
		{
			name:       "replay ON: the gap event is replayed, resume is contiguous (no gap, no dup)",
			serverOpts: []msghttp.Option{msghttp.WithReplayBuffer(8)},
			afterCount: 3,
			assert: func(t *testing.T, before, after []string, conn2LastEventID string) {
				assert.Equal(t, []string{"1", "2", "3"}, before)
				assert.Equal(t, []string{"4", "5", "6"}, after)
				// Direct, unambiguous assertion on the literal reconnect
				// header: a resume regression fails HERE, not just via the
				// downstream contiguity check above.
				assert.Equal(t, "3", conn2LastEventID,
					"the reconnect request must carry Last-Event-ID: 3, the last id delivered on conn1")
			},
		},
		{
			name:       "replay OFF: the gap event is lost, only post-reconnect events arrive",
			serverOpts: nil,
			afterCount: 2,
			assert: func(t *testing.T, before, after []string, _ string) {
				assert.Equal(t, []string{"1", "2", "3"}, before)
				assert.Equal(t, []string{"5", "6"}, after)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			before, after, conn2LastEventID := runResumeScenario(t, tc.serverOpts, tc.afterCount)
			tc.assert(t, before, after, conn2LastEventID)
		})
	}
}

// =============================================================================
// Consumer.Run integration smoke: NewConsumer over an SSEClient — the first
// NETWORKED/untrusted-remote StreamingSource wired to the runtime (audit
// MINOR-4; adapter/cron's Source is the earlier in-process one). SSEClient
// emits []byte payloads (a WIRE source, not a LiveValueSource), so it is
// paired with msgin.BytesPayloadCodec explicitly (mirrors outbound_test.go's
// WithProducerCodec[[]byte](msgin.BytesPayloadCodec{}) pairing) — otherwise
// the codec resolution would default to JSON, which would base64-wrap the
// raw event bytes.
// =============================================================================

func TestConsumer_overSSEClient_smoke(t *testing.T) {
	t.Parallel()

	// attempt 1: one event, clean end (no hold) -> Stream reconnects.
	// attempt 2: 204 -> the terminal-stop path; Stream (and so Consumer.Run)
	// ends with a nil error entirely on its own, with no ctx cancel needed.
	attempts := []sseAttempt{
		{status: http.StatusOK, header: sseEventStreamHeader, body: "data: hello\n\n"},
		{status: http.StatusNoContent},
	}
	url, _, _, closeSrv := newSSEStubServer(t, attempts)
	defer closeSrv()

	client, err := msghttp.NewSSEClient(url, msghttp.WithReconnectBackoff(time.Millisecond, time.Millisecond))
	require.NoError(t, err)

	var mu sync.Mutex
	var received [][]byte
	handler := func(_ context.Context, msg msgin.Message[[]byte]) error {
		mu.Lock()
		received = append(received, msg.Payload())
		mu.Unlock()
		return nil
	}

	consumer, err := endpoint.NewConsumer[[]byte](client, handler,
		endpoint.WithConsumerCodec[[]byte](msgin.BytesPayloadCodec{}))
	require.NoError(t, err)

	runErr := make(chan error, 1)
	go func() { runErr <- consumer.Run(t.Context()) }()

	select {
	case err := <-runErr:
		assert.NoError(t, err, "a 204 must end Consumer.Run with nil, the terminal-stop path")
	case <-time.After(5 * time.Second):
		t.Fatal("Consumer.Run did not return after the source's 204")
	}

	mu.Lock()
	got := append([][]byte(nil), received...)
	mu.Unlock()
	require.Len(t, got, 1, "the event from the source must have reached the handler")
	assert.Equal(t, []byte("hello"), got[0])
}
