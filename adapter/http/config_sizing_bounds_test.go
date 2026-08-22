package msghttp_test

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kartaladev/msgin"
	msghttp "github.com/kartaladev/msgin/adapter/http"
)

// TestNewConfig_SizingBoundsRejects proves WithConnectionBuffer,
// WithMaxConnections and WithReplayBuffer reject a value outside their
// documented [lo, hi] range with the EXISTING sentinel, non-Permanent (Spec
// 016 §3.1 R1, ADR 0029 D-M), and a rendered message that is true at BOTH
// ends of the range (Spec 016 §6 AC-2b) — not merely an errors.Is match. The
// lower-bound (0) case is mandatory: it is the one that would have caught a
// format rendering "0 exceeds 65536" — false, since 0 exceeds nothing (Spec
// 016 §3.1's rejection of revision 2's "%d exceeds %d" shape).
func TestNewConfig_SizingBoundsRejects(t *testing.T) {
	tests := []struct {
		name   string
		opt    msghttp.Option
		assert func(t *testing.T, err error)
	}{
		{
			name: "WithConnectionBuffer ceiling+1 is rejected",
			opt:  msghttp.WithConnectionBuffer(1<<16 + 1),
			assert: func(t *testing.T, err error) {
				require.ErrorIs(t, err, msghttp.ErrInvalidConnectionBuffer)
				assert.False(t, msgin.IsPermanent(err), "R1 constructor error must stay bare (ADR 0029 D-M)")
				assert.EqualError(t, err,
					"msghttp: connection buffer out of range: msghttp.WithConnectionBuffer: 65537 not in [1, 65536]")
			},
		},
		{
			name: "WithConnectionBuffer lower bound 0 is rejected",
			opt:  msghttp.WithConnectionBuffer(0),
			assert: func(t *testing.T, err error) {
				require.ErrorIs(t, err, msghttp.ErrInvalidConnectionBuffer)
				assert.False(t, msgin.IsPermanent(err), "R1 constructor error must stay bare (ADR 0029 D-M)")
				assert.EqualError(t, err,
					"msghttp: connection buffer out of range: msghttp.WithConnectionBuffer: 0 not in [1, 65536]")
			},
		},
		{
			name: "WithMaxConnections ceiling+1 is rejected",
			opt:  msghttp.WithMaxConnections(1<<16 + 1),
			assert: func(t *testing.T, err error) {
				require.ErrorIs(t, err, msghttp.ErrInvalidMaxConnections)
				assert.False(t, msgin.IsPermanent(err), "R1 constructor error must stay bare (ADR 0029 D-M)")
				assert.EqualError(t, err,
					"msghttp: max connections out of range: msghttp.WithMaxConnections: 65537 not in [1, 65536]")
			},
		},
		{
			name: "WithMaxConnections lower bound 0 is rejected",
			opt:  msghttp.WithMaxConnections(0),
			assert: func(t *testing.T, err error) {
				require.ErrorIs(t, err, msghttp.ErrInvalidMaxConnections)
				assert.False(t, msgin.IsPermanent(err), "R1 constructor error must stay bare (ADR 0029 D-M)")
				assert.EqualError(t, err,
					"msghttp: max connections out of range: msghttp.WithMaxConnections: 0 not in [1, 65536]")
			},
		},
		{
			name: "WithReplayBuffer ceiling+1 is rejected",
			opt:  msghttp.WithReplayBuffer(1<<16 + 1),
			assert: func(t *testing.T, err error) {
				require.ErrorIs(t, err, msghttp.ErrInvalidReplayBuffer)
				assert.False(t, msgin.IsPermanent(err), "R1 constructor error must stay bare (ADR 0029 D-M)")
				assert.EqualError(t, err,
					"msghttp: replay buffer out of range: msghttp.WithReplayBuffer: 65537 not in [1, 65536]")
			},
		},
		{
			name: "WithReplayBuffer lower bound 0 is rejected",
			opt:  msghttp.WithReplayBuffer(0),
			assert: func(t *testing.T, err error) {
				require.ErrorIs(t, err, msghttp.ErrInvalidReplayBuffer)
				assert.False(t, msgin.IsPermanent(err), "R1 constructor error must stay bare (ADR 0029 D-M)")
				assert.EqualError(t, err,
					"msghttp: replay buffer out of range: msghttp.WithReplayBuffer: 0 not in [1, 65536]")
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := msghttp.NewConfig(tc.opt)
			tc.assert(t, err)
		})
	}
}

// TestNewSSEServer_ConnectionBufferCeilingAccepts_ServeHTTP proves
// WithConnectionBuffer(ceiling) is accepted AND the hazardous path — sizing
// the per-connection frames channel's backing array to the ceiling —
// actually runs inside ServeHTTP (Spec 016 §6 AC-1's "allocating" family: the
// allocation IS the hazard, so a constructor-only assertion does not prove
// the panic is gone).
//
// EXPECT A MEMORY SPIKE: at the ceiling this allocates ~1.5 MiB for the one
// connection (measured; 1<<16 * 24 = 1,572,864 B, sizeof([]byte) slice header
// = 24 on a 64-bit platform). This is the ceiling's intended, documented cost
// (Spec 016 §3.4) — not a leak.
func TestNewSSEServer_ConnectionBufferCeilingAccepts_ServeHTTP(t *testing.T) {
	t.Parallel()

	server, err := msghttp.NewSSEServer(msghttp.WithConnectionBuffer(1 << 16))
	require.NoError(t, err)

	w := newCaptureWriter()
	cancel, done := serveInBackground(t, server, w)
	<-w.flushed // headers flushed — the ceiling-sized make(chan []byte, n) did not panic

	cancel()
	<-done
}

// TestNewSSEServer_MaxConnectionsCeilingAccepts proves WithMaxConnections(ceiling)
// is accepted (Spec 016 §6 AC-1/AC-2's "growth" family: at the ceiling only
// construction + acceptance is asserted — running the admission check at
// 1<<16 would need 65,536 live SSE connections, which cannot be written; the
// cap-still-caps property is proven separately, at small n, by
// TestSSEServer_MaxConnections_CapStillCaps below).
func TestNewSSEServer_MaxConnectionsCeilingAccepts(t *testing.T) {
	t.Parallel()

	server, err := msghttp.NewSSEServer(msghttp.WithMaxConnections(1 << 16))
	require.NoError(t, err)
	require.NotNil(t, server)
}

// TestSSEServer_MaxConnections_CapStillCaps proves the cap still caps at a
// small n (Spec 016 §6's growth-knob split): with WithMaxConnections(1), the
// second concurrently-registered connection is rejected with 503 "SSE
// connection limit reached" (sse_server.go:182-184). This is the property the
// increment restores — an unbounded n silently stopped this cap from
// capping.
func TestSSEServer_MaxConnections_CapStillCaps(t *testing.T) {
	t.Parallel()

	server, err := msghttp.NewSSEServer(msghttp.WithMaxConnections(1))
	require.NoError(t, err)

	w1 := newCaptureWriter()
	cancel1, done1 := serveInBackground(t, server, w1)
	<-w1.flushed // first connection registered, at the cap

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/events", nil)
	server.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
	assert.Contains(t, rec.Body.String(), "SSE connection limit reached")

	cancel1()
	<-done1
}

// TestNewSSEServer_ReplayBufferCeilingAccepts proves WithReplayBuffer(ceiling)
// is accepted (Spec 016 §6 AC-1/AC-2's "growth" family: at the ceiling only
// construction + acceptance is asserted; the cap-still-caps property is
// proven separately, at small n, by TestSSEServer_ReplayBuffer_CapStillCaps
// below).
func TestNewSSEServer_ReplayBufferCeilingAccepts(t *testing.T) {
	t.Parallel()

	server, err := msghttp.NewSSEServer(msghttp.WithReplayBuffer(1 << 16))
	require.NoError(t, err)
	require.NotNil(t, server)
}

// TestSSEServer_ReplayBuffer_CapStillCaps proves the replay ring's cap still
// caps at a small n (Spec 016 §1.5 / §6's growth-knob split). "The ring holds
// 8" is NOT observable through the blackbox surface — SSEServer exports only
// ServeHTTP, Close and Send, with no ring accessor — so the property is
// observed through a Last-Event-ID replay request instead: with
// WithReplayBuffer(8) and 20 Sends (ids "1".."20"), the ring retains the last
// 8 (ids "13".."20"). Resuming from a RETAINED id (13) replays the 7 entries
// after it; resuming from an EVICTED id (1) replays 0 (live-only,
// best-effort) — exactly what an unbounded ring would NOT do at n=1<<30, the
// oversized value the root sizing class gate drives WithReplayBuffer with.
func TestSSEServer_ReplayBuffer_CapStillCaps(t *testing.T) {
	t.Parallel()

	server, err := msghttp.NewSSEServer(msghttp.WithReplayBuffer(8))
	require.NoError(t, err)

	// Populate the ring with 20 events, no subscriber yet (the ring grows with
	// zero connections — sse_server.go:429-431 precedes the fan-out loop).
	for i := 1; i <= 20; i++ {
		id := strconv.Itoa(i)
		require.NoError(t, server.Send(t.Context(), idMsg(id, []byte("d"+id))))
	}
	// Ring cap 8 ⇒ retains the last 8 sent: ids 13..20.

	// Resume from the RETAINED id 13: replays the 7 ring entries after it
	// (14..20), then whatever arrives live.
	retained := newCaptureWriter()
	cancelR, doneR := serveWithHeader(t, server, retained, "Last-Event-ID", "13")
	<-retained.flushed

	// Resume from the EVICTED id 1: live-only, 0 replay frames.
	evicted := newCaptureWriter()
	cancelE, doneE := serveWithHeader(t, server, evicted, "Last-Event-ID", "1")
	<-evicted.flushed

	require.NoError(t, server.Send(t.Context(), idMsg("21", []byte("d21"))))

	// retained: 7 replayed (14..20) + 1 live (21) = 8, ids in order.
	retainedEvents := waitEvents(t, retained, 8)
	assert.Equal(t, []string{"14", "15", "16", "17", "18", "19", "20", "21"}, eventIDs(retainedEvents))

	// evicted: 0 replayed + 1 live (21) = 1.
	evictedEvents := waitEvents(t, evicted, 1)
	assert.Equal(t, []string{"21"}, eventIDs(evictedEvents))

	cancelR()
	<-doneR
	cancelE()
	<-doneE
}
