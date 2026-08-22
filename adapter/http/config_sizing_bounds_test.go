package msghttp_test

import (
	"math"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
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

// TestNewConfig_ByteCapRangeRejects is Spec 018 §6 AC-2/AC-2b: each of the
// three msghttp byte caps rejects a value outside [1, byteCapCeiling] with its
// OWN pre-existing sentinel (ADR 0034 D-AQ reuses them; errors.Is is
// unaffected by the message change), non-Permanent (ADR 0029 D-M), and the
// single rendered shape true at BOTH ends of the range.
//
// byteCapCeiling is unexported, so the literal is spelled here rather than
// named (Plan 032 Global constraint 2). The upper arm's 2147483648 is
// math.MaxInt32 + 1 written out: the option takes an int64, so the literal is
// in range on every GOARCH and renders the same decimal everywhere — which is
// exactly why the signature was NOT narrowed to int (ADR 0034 D-AP(a)).
//
// The 1<<62 rows are the literal the root sizing class gate drives these three
// knobs with (sizing_option_class_gate_test.go): they are what makes that
// gate's "fixed" arm rows non-vacuous, so they are asserted here too, in the
// package that owns the check.
func TestNewConfig_ByteCapRangeRejects(t *testing.T) {
	tests := []struct {
		name   string
		opt    msghttp.Option
		assert func(t *testing.T, err error)
	}{
		{
			name: "WithMaxBodyBytes lower bound 0 is rejected",
			opt:  msghttp.WithMaxBodyBytes(0),
			assert: func(t *testing.T, err error) {
				require.ErrorIs(t, err, msghttp.ErrInvalidMaxBodyBytes)
				assert.False(t, msgin.IsPermanent(err), "R1 constructor error stays bare (ADR 0029 D-M)")
				assert.EqualError(t, err,
					"msghttp: max body bytes out of range: msghttp.WithMaxBodyBytes: 0 not in [1, 2147483647]")
			},
		},
		{
			name: "WithMaxBodyBytes ceiling+1 is rejected",
			opt:  msghttp.WithMaxBodyBytes(2147483648),
			assert: func(t *testing.T, err error) {
				require.ErrorIs(t, err, msghttp.ErrInvalidMaxBodyBytes)
				assert.False(t, msgin.IsPermanent(err), "R1 constructor error stays bare (ADR 0029 D-M)")
				assert.EqualError(t, err,
					"msghttp: max body bytes out of range: msghttp.WithMaxBodyBytes: 2147483648 not in [1, 2147483647]")
			},
		},
		{
			name: "WithMaxBodyBytes at the class gate's 1<<62 is rejected",
			opt:  msghttp.WithMaxBodyBytes(1 << 62),
			assert: func(t *testing.T, err error) {
				require.ErrorIs(t, err, msghttp.ErrInvalidMaxBodyBytes)
				assert.False(t, msgin.IsPermanent(err), "R1 constructor error stays bare (ADR 0029 D-M)")
			},
		},
		{
			name: "WithMaxResponseBytes lower bound 0 is rejected",
			opt:  msghttp.WithMaxResponseBytes(0),
			assert: func(t *testing.T, err error) {
				require.ErrorIs(t, err, msghttp.ErrInvalidMaxResponseBytes)
				assert.False(t, msgin.IsPermanent(err), "R1 constructor error stays bare (ADR 0029 D-M)")
				assert.EqualError(t, err,
					"msghttp: max response bytes out of range: msghttp.WithMaxResponseBytes: 0 not in [1, 2147483647]")
			},
		},
		{
			name: "WithMaxResponseBytes ceiling+1 is rejected",
			opt:  msghttp.WithMaxResponseBytes(2147483648),
			assert: func(t *testing.T, err error) {
				require.ErrorIs(t, err, msghttp.ErrInvalidMaxResponseBytes)
				assert.False(t, msgin.IsPermanent(err), "R1 constructor error stays bare (ADR 0029 D-M)")
				assert.EqualError(t, err,
					"msghttp: max response bytes out of range: msghttp.WithMaxResponseBytes: 2147483648 not in [1, 2147483647]")
			},
		},
		{
			name: "WithMaxResponseBytes at the class gate's 1<<62 is rejected",
			opt:  msghttp.WithMaxResponseBytes(1 << 62),
			assert: func(t *testing.T, err error) {
				require.ErrorIs(t, err, msghttp.ErrInvalidMaxResponseBytes)
				assert.False(t, msgin.IsPermanent(err), "R1 constructor error stays bare (ADR 0029 D-M)")
			},
		},
		{
			name: "WithMaxEventBytes lower bound 0 is rejected",
			opt:  msghttp.WithMaxEventBytes(0),
			assert: func(t *testing.T, err error) {
				require.ErrorIs(t, err, msghttp.ErrInvalidMaxEventBytes)
				assert.False(t, msgin.IsPermanent(err), "R1 constructor error stays bare (ADR 0029 D-M)")
				assert.EqualError(t, err,
					"msghttp: max event bytes out of range: msghttp.WithMaxEventBytes: 0 not in [1, 2147483647]")
			},
		},
		{
			name: "WithMaxEventBytes ceiling+1 is rejected",
			opt:  msghttp.WithMaxEventBytes(2147483648),
			assert: func(t *testing.T, err error) {
				require.ErrorIs(t, err, msghttp.ErrInvalidMaxEventBytes)
				assert.False(t, msgin.IsPermanent(err), "R1 constructor error stays bare (ADR 0029 D-M)")
				assert.EqualError(t, err,
					"msghttp: max event bytes out of range: msghttp.WithMaxEventBytes: 2147483648 not in [1, 2147483647]")
			},
		},
		{
			name: "WithMaxEventBytes at the class gate's 1<<62 is rejected",
			opt:  msghttp.WithMaxEventBytes(1 << 62),
			assert: func(t *testing.T, err error) {
				require.ErrorIs(t, err, msghttp.ErrInvalidMaxEventBytes)
				assert.False(t, msgin.IsPermanent(err), "R1 constructor error stays bare (ADR 0029 D-M)")
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

// TestNewConfig_ByteCapCeilingAccepts is Spec 018 §6 AC-1's accepting arm: at
// the ceiling (and at the lower bound 1) NewConfig returns a usable *Config,
// so the bound is a BOUNDARY and not an off-by-one that shuts the knob down.
//
// "Its product is usable", NOT "the knob's effect is observable" (Spec 016 §6's
// phrase for a NewConfig-only key). The ceiling's effect is unobservable BY
// CONSTRUCTION and that is accepted, not a gap: Plan 032 Global constraint 6
// caps every fixture at 1 MiB + 1 while the ceiling is 2,147,483,647, so a
// small fixture succeeds identically at the ceiling, at the 1 MiB default, and
// with the option dropped. The ceiling is therefore proven at the CONSTRUCTOR
// only; the separate property "the cap caps" is proven at small n by
// TestDecodeRequest_defaultBodyCapIsOneMiB (body),
// TestExchange_defaultMaxResponseBytes (response) and TestSSEParser_Next's
// "default cap" rows (event).
//
// No accessor is asserted: maxBody() is unexported and maxResponseBytes /
// maxEventBytes have no accessor at all, so the value set is observed through
// the product instead (Spec 018 §6 AC-1, round-1 M-10).
func TestNewConfig_ByteCapCeilingAccepts(t *testing.T) {
	tests := []struct {
		name   string
		opt    msghttp.Option
		assert func(t *testing.T, cfg *msghttp.Config, err error)
	}{
		{
			name: "WithMaxBodyBytes at the ceiling is accepted and DecodeRequest still decodes",
			opt:  msghttp.WithMaxBodyBytes(math.MaxInt32),
			assert: func(t *testing.T, cfg *msghttp.Config, err error) {
				require.NoError(t, err)
				require.NotNil(t, cfg)

				req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("hello"))
				msg, decErr := msghttp.DecodeRequest(req, cfg)
				require.NoError(t, decErr)
				assert.Equal(t, []byte("hello"), msg.Payload())
			},
		},
		{
			name: "WithMaxBodyBytes at the lower bound 1 is accepted",
			opt:  msghttp.WithMaxBodyBytes(1),
			assert: func(t *testing.T, cfg *msghttp.Config, err error) {
				require.NoError(t, err)
				require.NotNil(t, cfg)
			},
		},
		{
			name: "WithMaxResponseBytes at the ceiling is accepted and an httptest round-trip still replies",
			opt:  msghttp.WithMaxResponseBytes(math.MaxInt32),
			assert: func(t *testing.T, cfg *msghttp.Config, err error) {
				require.NoError(t, err)
				require.NotNil(t, cfg)

				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					_, _ = w.Write([]byte("pong"))
				}))
				t.Cleanup(srv.Close)

				x, xErr := msghttp.NewExchange(srv.URL, msghttp.WithMaxResponseBytes(math.MaxInt32))
				require.NoError(t, xErr)
				reply, xErr := x.Exchange(t.Context(), msgin.New[any]([]byte("ping")))
				require.NoError(t, xErr)
				assert.Equal(t, []byte("pong"), reply.Payload())
			},
		},
		{
			name: "WithMaxResponseBytes at the lower bound 1 is accepted",
			opt:  msghttp.WithMaxResponseBytes(1),
			assert: func(t *testing.T, cfg *msghttp.Config, err error) {
				require.NoError(t, err)
				require.NotNil(t, cfg)
			},
		},
		{
			name: "WithMaxEventBytes at the ceiling is accepted and NewSSEParser still parses",
			opt:  msghttp.WithMaxEventBytes(math.MaxInt32),
			assert: func(t *testing.T, cfg *msghttp.Config, err error) {
				require.NoError(t, err)
				require.NotNil(t, cfg)

				p, pErr := msghttp.NewSSEParser(strings.NewReader("data: hi\n\n"),
					msghttp.WithMaxEventBytes(math.MaxInt32))
				require.NoError(t, pErr)
				ev, pErr := p.Next()
				require.NoError(t, pErr)
				assert.Equal(t, []byte("hi"), ev.Data)
			},
		},
		{
			name: "WithMaxEventBytes at the lower bound 1 is accepted",
			opt:  msghttp.WithMaxEventBytes(1),
			assert: func(t *testing.T, cfg *msghttp.Config, err error) {
				require.NoError(t, err)
				require.NotNil(t, cfg)
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg, err := msghttp.NewConfig(tc.opt)
			tc.assert(t, cfg, err)
		})
	}
}

// TestDecodeRequest_defaultBodyCapIsOneMiB proves the DEFAULT body cap
// behaviorally — the `!set` arm of the gate the ceiling rewrites, which no
// shipped test covered (the response cap's default has
// TestExchange_defaultMaxResponseBytes, the event cap's has
// TestSSEParser_Next's "default cap" rows; the body cap had none).
//
// The BOUNDARY PAIR, not a single oversized body: a body of exactly 1<<20 is
// accepted and 1<<20 + 1 is rejected, which pins the default at exactly
// 1,048,576 — the only value satisfying both arms — and lands on
// http.MaxBytesReader's exact boundary. A single "2 MiB is rejected" case
// would prove only that the default lies somewhere in (0, 2 MiB), and is
// killed by a one-byte body.
//
// This 1 MiB + 1 fixture is the largest in the increment, and it is what sets
// Plan 032 Global constraint 6's bound. A cap may be CONFIGURED at the ceiling
// (an int64 field costs eight bytes); a 2 GiB FIXTURE cannot be written, since
// io.ReadAll's doubling would peak near 4 GiB in a package whose sibling runs
// goleak.VerifyTestMain.
func TestDecodeRequest_defaultBodyCapIsOneMiB(t *testing.T) {
	t.Parallel()

	const defaultCap = 1 << 20 // the documented 1 MiB default

	tests := []struct {
		name   string
		body   string
		assert func(t *testing.T, msg msgin.Message[any], err error)
	}{
		{
			name: "a body of exactly the 1 MiB default decodes intact",
			body: strings.Repeat("A", defaultCap),
			assert: func(t *testing.T, msg msgin.Message[any], err error) {
				require.NoError(t, err)
				payload, ok := msg.Payload().([]byte)
				require.True(t, ok)
				assert.Len(t, payload, defaultCap)
			},
		},
		{
			name: "one byte over the 1 MiB default is rejected",
			body: strings.Repeat("A", defaultCap+1),
			assert: func(t *testing.T, _ msgin.Message[any], err error) {
				require.ErrorIs(t, err, msghttp.ErrDecodeRequest)
				var maxErr *http.MaxBytesError
				assert.ErrorAs(t, err, &maxErr, "the default cap must bind through http.MaxBytesReader")
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// No WithMaxBodyBytes: the default applies.
			cfg, err := msghttp.NewConfig()
			require.NoError(t, err)

			req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(tc.body))
			msg, err := msghttp.DecodeRequest(req, cfg)
			tc.assert(t, msg, err)
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
