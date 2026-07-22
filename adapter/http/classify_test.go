package msghttp_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unicode"

	"github.com/jonboulle/clockwork"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kartaladev/msgin"
	msghttp "github.com/kartaladev/msgin/adapter/http"
)

// This file holds Plan 024 Task 3's tests: ClassifyResponse, StatusError and
// parseRetryAfter. Every branch (1-20, incl. 13a/13b, 18, 19/19b/19c/19d/19e,
// 20) has a mutation-sensitive case. Permanence and Retry-After delays are
// asserted through their OBSERVABLE consequence on a msgin.Producer, never via
// errors.Is on the marker (which proves nothing — Plan 023 D6).

// newResponse builds a hand-crafted *http.Response for ClassifyResponse. The
// body is a NopCloser over a string reader (Spec 011 hermetic — no network).
func newResponse(code int, body string, header http.Header) *http.Response {
	if header == nil {
		header = http.Header{}
	}
	return &http.Response{
		StatusCode: code,
		Header:     header,
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

// fixedResultAdapter is an OutboundAdapter that returns the same error on every
// Send and counts its calls. A msgin.Permanent classification is observable as
// exactly ONE call (sendRetrying short-circuits with no retry).
type fixedResultAdapter struct {
	err   error
	calls atomic.Int64
}

func (a *fixedResultAdapter) Send(context.Context, msgin.Message[any]) error {
	a.calls.Add(1)
	return a.err
}

// retryThenOKAdapter returns classified on the FIRST Send and nil afterwards, so
// a transient classification is observable as a two-attempt sequence: the
// producer parks for the classified wait, then the retry succeeds. Counting the
// calls (== 2) proves the error was retried, hence NOT permanent.
type retryThenOKAdapter struct {
	classified error
	calls      atomic.Int64
}

func (a *retryThenOKAdapter) Send(context.Context, msgin.Message[any]) error {
	if a.calls.Add(1) == 1 {
		return a.classified
	}
	return nil
}

// countingSink counts DeadLetter diverts.
type countingSink struct{ n atomic.Int64 }

func (s *countingSink) Send(context.Context, msgin.Message[any]) error {
	s.n.Add(1)
	return nil
}

// assertPermanent proves classified is a msgin.Permanent error by its OBSERVABLE
// consequence through a Producer: exactly ONE attempt, ZERO dead-letters, ZERO
// clock advance, returned synchronously with no retry wait and without
// ErrDeadLettered.
func assertPermanent(t *testing.T, classified error) {
	t.Helper()
	clk := clockwork.NewFakeClock()
	start := clk.Now()
	adapter := &fixedResultAdapter{err: classified}
	sink := &countingSink{}
	policy := msgin.RetryPolicy{
		MaxAttempts: 3,
		Backoff:     msgin.ExponentialBackoff{Initial: time.Second},
		DeadLetter:  sink,
	}
	prod, err := msgin.NewProducer[[]byte](adapter,
		msgin.WithProducerCodec[[]byte](msgin.BytesPayloadCodec{}),
		msgin.WithProducerRetry[[]byte](policy),
		msgin.WithProducerClock[[]byte](clk),
	)
	require.NoError(t, err)

	sendErr := prod.Send(t.Context(), msgin.New[[]byte]([]byte("x")))
	require.Error(t, sendErr)
	assert.Equal(t, int64(1), adapter.calls.Load(), "a permanent error must not be retried")
	assert.Equal(t, int64(0), sink.n.Load(), "a permanent error must not be dead-lettered")
	assert.Equal(t, start, clk.Now(), "a permanent error must not advance the retry clock")
	assert.False(t, errors.Is(sendErr, msgin.ErrDeadLettered))
}

// assertTransientWait proves classified is a transient error whose effective
// producer wait is wantWait, via a two-phase fake-clock advance
// (wantWait-1ns => still parked => +1ns => returns). Under-waiting is detected;
// a one-shot Advance(wantWait) cannot. The two-attempt success also proves the
// error is NOT permanent.
func assertTransientWait(t *testing.T, classified error, backoff msgin.BackoffStrategy, wantWait time.Duration) {
	t.Helper()
	clk := clockwork.NewFakeClock()
	adapter := &retryThenOKAdapter{classified: classified}
	policy := msgin.RetryPolicy{Backoff: backoff} // MaxAttempts 0: retry until success or budget
	prod, err := msgin.NewProducer[[]byte](adapter,
		msgin.WithProducerCodec[[]byte](msgin.BytesPayloadCodec{}),
		msgin.WithProducerRetry[[]byte](policy),
		msgin.WithProducerClock[[]byte](clk),
	)
	require.NoError(t, err)

	ctx := t.Context()
	done := make(chan error, 1)
	go func() { done <- prod.Send(ctx, msgin.New[[]byte]([]byte("x"))) }()

	// BOUNDED so a regression that never parks fails with a clear message rather
	// than hanging the whole binary on the package timeout.
	blockCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	require.NoError(t, clk.BlockUntilContext(blockCtx, 1), "producer never parked on a timer")

	clk.Advance(wantWait - time.Nanosecond)
	select {
	case <-done:
		t.Fatalf("Send returned before the full %s wait elapsed (under-waited)", wantWait)
	case <-time.After(20 * time.Millisecond):
	}
	clk.Advance(time.Nanosecond)
	select {
	case sendErr := <-done:
		require.NoError(t, sendErr, "the second attempt succeeds")
	case <-time.After(5 * time.Second):
		t.Fatalf("Send did not return after the %s wait elapsed", wantWait)
	}
	assert.Equal(t, int64(2), adapter.calls.Load(), "a transient error must be retried")
}

// TestClassifyResponse_direct covers the pure-output branches of ClassifyResponse
// and StatusError: nil response, 2xx, errors.As code extraction, errors.Is
// through both wrappers, INV-3 (no payload sentinel from a status), and INV-4
// (default no excerpt / opt-in sanitized excerpt).
func TestClassifyResponse_direct(t *testing.T) {
	t.Parallel()

	t.Run("nil response returns ErrNilResponse (branch 1)", func(t *testing.T) {
		t.Parallel()
		cfg, err := msghttp.NewConfig()
		require.NoError(t, err)
		got := msghttp.ClassifyResponse(nil, cfg)
		assert.ErrorIs(t, got, msghttp.ErrNilResponse)
	})

	t.Run("2xx returns nil (branch 2)", func(t *testing.T) {
		t.Parallel()
		cfg, err := msghttp.NewConfig()
		require.NoError(t, err)
		for _, code := range []int{200, 204, 299} {
			assert.NoError(t, msghttp.ClassifyResponse(newResponse(code, "", nil), cfg))
		}
	})

	t.Run("errors.As yields the status code (branch 16)", func(t *testing.T) {
		t.Parallel()
		cfg, err := msghttp.NewConfig()
		require.NoError(t, err)
		got := msghttp.ClassifyResponse(newResponse(400, "", nil), cfg)
		var se *msghttp.StatusError
		require.ErrorAs(t, got, &se)
		assert.Equal(t, 400, se.Code)
	})

	t.Run("errors.Is ErrOutboundStatus through both wrappers (branch 17)", func(t *testing.T) {
		t.Parallel()
		cfg, err := msghttp.NewConfig()
		require.NoError(t, err)

		perm := msghttp.ClassifyResponse(newResponse(404, "", nil), cfg)
		assert.ErrorIs(t, perm, msghttp.ErrOutboundStatus)

		h := http.Header{}
		h.Set("Retry-After", "30")
		ra := msghttp.ClassifyResponse(newResponse(429, "", h), cfg)
		assert.ErrorIs(t, ra, msghttp.ErrOutboundStatus)
	})

	t.Run("INV-3: a status never yields a payload sentinel (branch 18)", func(t *testing.T) {
		t.Parallel()
		cfg, err := msghttp.NewConfig()
		require.NoError(t, err)
		for _, code := range []int{400, 404, 413, 415, 422} {
			got := msghttp.ClassifyResponse(newResponse(code, "", nil), cfg)
			assert.False(t, errors.Is(got, msgin.ErrPayloadTooLarge), "code %d must not map to ErrPayloadTooLarge", code)
			assert.False(t, errors.Is(got, msgin.ErrPayloadType), "code %d must not map to ErrPayloadType", code)
			assert.True(t, errors.Is(got, msghttp.ErrOutboundStatus), "code %d must be ErrOutboundStatus", code)
		}
	})

	t.Run("INV-4 default: no excerpt, no remote bytes in the error string (branch 19)", func(t *testing.T) {
		t.Parallel()
		cfg, err := msghttp.NewConfig() // WithErrorBodyExcerpt NOT set
		require.NoError(t, err)
		hostileBody := "boom\r\nInjected: evil <script>"
		h := http.Header{}
		h.Set("Retry-After", "not-a-date\r\nX: y")
		got := msghttp.ClassifyResponse(newResponse(500, hostileBody, h), cfg)

		var se *msghttp.StatusError
		require.ErrorAs(t, got, &se)
		assert.Empty(t, se.Excerpt, "default posture carries no excerpt")

		msg := got.Error()
		assert.Contains(t, msg, "500")
		assert.NotContains(t, msg, "boom", "the remote body must not reach the error string")
		assert.NotContains(t, msg, "\r")
		assert.NotContains(t, msg, "\n")
	})

	t.Run("decision 5: opt-in excerpt is sanitized, printable survives (branch 19b)", func(t *testing.T) {
		t.Parallel()
		cfg, err := msghttp.NewConfig(msghttp.WithErrorBodyExcerpt())
		require.NoError(t, err)
		// Escape sequences ONLY (a literal BOM/bidi in Go source is a compile
		// error). À (A-grave) and 世 (CJK) are the printable survivors;
		// the rest are dangerous code points that must be escaped away.
		hostile := "\x1b[31m\r\n\t\x00\uFEFF\u202E\u0085\xff\xfeÀ世"
		got := msghttp.ClassifyResponse(newResponse(500, hostile, nil), cfg)

		var se *msghttp.StatusError
		require.ErrorAs(t, got, &se)

		// The Excerpt is strconv.Quote's output: a well-formed Go quoted string
		// (round-trips) whose EVERY rune is printable — the sanitization operates
		// at the RUNE level, so every non-printable is escaped to a printable
		// \uXXXX / \xNN sequence (a byte-strip mutation would leave raw controls
		// and fail this scan). NOTE: Unquote reverses the escaping back to the raw
		// controls, so the per-rune IsPrint scan is on the QUOTED form — which is
		// what actually reaches caller logs (plan branch 19b wording resolved: the
		// property "every rune IsPrint" holds on the Quote output, not after
		// unquoting).
		_, uqErr := strconv.Unquote(se.Excerpt)
		require.NoError(t, uqErr, "the excerpt must be a well-formed quoted string")

		for _, r := range se.Excerpt {
			assert.True(t, unicode.IsPrint(r), "every rune of the excerpt must be printable, got %U", r)
		}
		assert.True(t, strings.ContainsRune(se.Excerpt, 'À'), "the printable A-grave must survive")
		assert.True(t, strings.ContainsRune(se.Excerpt, '世'), "the printable CJK rune must survive")

		// StatusError.Error appends the (sanitized) excerpt when it is non-empty —
		// the else arm of Error(). The error string still contains no raw control
		// bytes, because the excerpt is already the quoted, printable form.
		errStr := got.Error()
		assert.Contains(t, errStr, se.Excerpt, "the non-empty excerpt is appended to the error string")
		assert.Contains(t, errStr, "500")
		assert.NotContains(t, errStr, "\r")
		assert.NotContains(t, errStr, "\n")
		for _, bad := range []rune{'\x1b', '\r', '\n', '\x00', '\uFEFF', '\u202E', '\u0085'} {
			assert.False(t, strings.ContainsRune(se.Excerpt, bad), "dangerous rune %U must be escaped/absent", bad)
		}
	})

	t.Run("decision 5: read is capped BEFORE reading (branch 19c)", func(t *testing.T) {
		t.Parallel()
		cfg, err := msghttp.NewConfig(msghttp.WithErrorBodyExcerpt())
		require.NoError(t, err)

		big := strings.Repeat("A", 1<<20) // 1 MiB
		counter := &countingReader{r: strings.NewReader(big)}
		resp := &http.Response{StatusCode: 500, Header: http.Header{}, Body: io.NopCloser(counter)}

		got := msghttp.ClassifyResponse(resp, cfg)
		var se *msghttp.StatusError
		require.ErrorAs(t, got, &se)
		assert.LessOrEqual(t, counter.n.Load(), int64(256),
			"at most errorBodyExcerptMax bytes may be CONSUMED from the body (cap-before-read)")
	})

	t.Run("review F1: a nil Body with the excerpt enabled classifies without panicking", func(t *testing.T) {
		t.Parallel()
		cfg, err := msghttp.NewConfig(msghttp.WithErrorBodyExcerpt())
		require.NoError(t, err)

		// A hand-built response may legally carry a nil Body (only a response
		// delivered by net/http backfills it); the excerpt read must be guarded,
		// never panic on caller input.
		got := msghttp.ClassifyResponse(&http.Response{StatusCode: 500, Header: http.Header{}}, cfg)

		var se *msghttp.StatusError
		require.ErrorAs(t, got, &se, "the status must classify, not panic")
		assert.Equal(t, 500, se.Code)
		assert.Empty(t, se.Excerpt, "no body means no excerpt")
	})

	t.Run("decision 5: a mid-read error is best-effort, the status still surfaces (branch 19d)", func(t *testing.T) {
		t.Parallel()
		cfg, err := msghttp.NewConfig(msghttp.WithErrorBodyExcerpt())
		require.NoError(t, err)

		body := io.NopCloser(io.MultiReader(strings.NewReader("partial"), &erroringReader{}))
		resp := &http.Response{StatusCode: 500, Header: http.Header{}, Body: body}

		got := msghttp.ClassifyResponse(resp, cfg)
		var se *msghttp.StatusError
		require.ErrorAs(t, got, &se, "a broken error body must not mask the status error")
	})
}

// countingReader counts the bytes actually consumed from the wrapped reader, to
// prove readExcerpt caps the READ (not merely the output length) — branch 19c.
type countingReader struct {
	r io.Reader
	n atomic.Int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n.Add(int64(n))
	return n, err
}

// erroringReader always fails, to inject a mid-read error — branch 19d.
type erroringReader struct{}

func (*erroringReader) Read([]byte) (int, error) { return 0, errors.New("boom") }

// TestClassifyResponse_permanence proves the Permanent-classified statuses via a
// Producer (branches 3, 4, 5): 400/404 (above-2xx), 100 (below-2xx), 301 (3xx).
func TestClassifyResponse_permanence(t *testing.T) {
	t.Parallel()

	cfg, err := msghttp.NewConfig()
	require.NoError(t, err)

	for _, code := range []int{400, 404, 100, 301} {
		t.Run(strconv.Itoa(code), func(t *testing.T) {
			t.Parallel()
			classified := msghttp.ClassifyResponse(newResponse(code, "", nil), cfg)
			assertPermanent(t, classified)
		})
	}
}

// TestClassifyResponse_transient proves the transient-classified statuses and
// their Retry-After handling via a Producer's two-phase fake-clock wait
// (branches 6-15, 20). The computed backoff is 1s throughout; the producer's
// Retry-After cap is the 60s default.
func TestClassifyResponse_transient(t *testing.T) {
	t.Parallel()

	const backoff = time.Second
	oneSecond := msgin.ExponentialBackoff{Initial: backoff} // Delay(0) == 1s

	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	dateCfg, err := msghttp.NewConfig(msghttp.WithOutboundClock(clockwork.NewFakeClockAt(base)))
	require.NoError(t, err)
	plainCfg, err := msghttp.NewConfig()
	require.NoError(t, err)

	header := func(k, v string) http.Header {
		h := http.Header{}
		if v != "" {
			h.Set(k, v)
		}
		return h
	}

	type testCase struct {
		name       string
		classified error
		wantWait   time.Duration
	}

	cases := []testCase{
		{ // branch 6
			name:       "408 is plain transient, no Retry-After honored",
			classified: msghttp.ClassifyResponse(newResponse(408, "", nil), plainCfg),
			wantWait:   backoff,
		},
		{ // branch 7
			name:       "429 without Retry-After is plain transient",
			classified: msghttp.ClassifyResponse(newResponse(429, "", nil), plainCfg),
			wantWait:   backoff,
		},
		{ // branch 8
			name:       "429 with Retry-After: 30 waits 30s",
			classified: msghttp.ClassifyResponse(newResponse(429, "", header("Retry-After", "30")), plainCfg),
			wantWait:   30 * time.Second,
		},
		{ // branch 9
			name:       "503 with an HTTP-date 45s ahead waits 45s",
			classified: msghttp.ClassifyResponse(newResponse(503, "", header("Retry-After", base.Add(45*time.Second).UTC().Format(http.TimeFormat))), dateCfg),
			wantWait:   45 * time.Second,
		},
		{ // branch 10
			name:       "503 with an unparseable Retry-After is plain transient",
			classified: msghttp.ClassifyResponse(newResponse(503, "", header("Retry-After", "soon-ish")), plainCfg),
			wantWait:   backoff,
		},
		{ // branch 11
			name:       "500 ignores Retry-After (not 429/503)",
			classified: msghttp.ClassifyResponse(newResponse(500, "", header("Retry-After", "30")), plainCfg),
			wantWait:   backoff,
		},
		{ // branch 12
			name:       "Retry-After: -5 is rejected, plain transient",
			classified: msghttp.ClassifyResponse(newResponse(429, "", header("Retry-After", "-5")), plainCfg),
			wantWait:   backoff,
		},
		{ // branch 13a: largest VALID delay-seconds (MaxInt64/1e9) accepted, clamped to the 60s cap
			name:       "Retry-After: 9223372036 accepted, clamped to the producer cap",
			classified: msghttp.ClassifyResponse(newResponse(429, "", header("Retry-After", "9223372036")), plainCfg),
			wantWait:   60 * time.Second, // min(~292y, 60s cap)
		},
		{ // branch 13b: first OVERFLOWING value rejected -> no marker -> computed backoff
			name:       "Retry-After: 9223372037 rejected (overflow), plain transient",
			classified: msghttp.ClassifyResponse(newResponse(429, "", header("Retry-After", "9223372037")), plainCfg),
			wantWait:   backoff,
		},
		{ // branch 14
			name:       "503 with an HTTP-date in the past degrades to the computed backoff",
			classified: msghttp.ClassifyResponse(newResponse(503, "", header("Retry-After", base.Add(-time.Hour).UTC().Format(http.TimeFormat))), dateCfg),
			wantWait:   backoff,
		},
		{ // branch 15
			name:       "empty/whitespace Retry-After is rejected",
			classified: msghttp.ClassifyResponse(newResponse(429, "", header("Retry-After", "   ")), plainCfg),
			wantWait:   backoff,
		},
		{ // branch 20: nil *Config on the 429+Retry-After path reaches clockOrDefault's nil branch
			name:       "nil Config does not panic on the Retry-After path",
			classified: msghttp.ClassifyResponse(newResponse(429, "", header("Retry-After", "30")), nil),
			wantWait:   30 * time.Second,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assertTransientWait(t, tc.classified, oneSecond, tc.wantWait)
		})
	}
}

// TestClassifyResponse_excerptPanicPropagates covers branch 19e / INV-7: when the
// error body's Read panics during readExcerpt, ClassifyResponse does NOT recover
// (the original panic value propagates), and a caller's deferred Close still
// runs. ClassifyResponse itself never closes the body — Send/Exchange (Tasks
// 4/5) register the sole deferred Close — so this harness mirrors that call site
// with its own deferred Close.
func TestClassifyResponse_excerptPanicPropagates(t *testing.T) {
	t.Parallel()

	cfg, err := msghttp.NewConfig(msghttp.WithErrorBodyExcerpt())
	require.NoError(t, err)

	panicVal := "read-boom"
	body := &panickingBody{panicVal: panicVal}

	recovered := func() (rec any) {
		defer func() { rec = recover() }()
		defer func() { _ = body.Close() }() // mirrors the Tasks 4/5 sole deferred Close
		_ = msghttp.ClassifyResponse(&http.Response{StatusCode: 500, Header: http.Header{}, Body: body}, cfg)
		return nil
	}()

	require.NotNil(t, recovered, "the panic must propagate, not be converted to an error")
	assert.Equal(t, panicVal, recovered, "the ORIGINAL panic value must be preserved")
	assert.True(t, body.closed.Load(), "the deferred Close must run on the panic unwind")
}

// panickingBody is a ReadCloser whose Read panics, tracking whether Close ran.
type panickingBody struct {
	panicVal any
	closed   atomic.Bool
}

func (b *panickingBody) Read([]byte) (int, error) { panic(b.panicVal) }

func (b *panickingBody) Close() error {
	b.closed.Store(true)
	return nil
}
