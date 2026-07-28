package msghttp

import (
	"context"
	"io"
	"mime"
	"net/http"
	"time"

	"github.com/kartaladev/msgin"
)

// SSEClient is the S-in msgin.EventDrivenSource (Spec 011 §3.5, ADR 0023
// Addendum C): it connects to a caller-configured text/event-stream
// endpoint, emits each parsed event as a msgin.Delivery, and auto-reconnects
// under a clamped exponential backoff (WithReconnectBackoff), carrying
// Last-Event-ID across reconnects on a best-effort basis. Stream — the
// actual connect/parse/reconnect loop — lands in a later task; this file
// declares only the type, its constructor, and its (fixed) NativeReliability
// answers.
//
// Delivery guarantee: AT-MOST-ONCE, best-effort resume. Resume works only
// against a server that honors a Last-Event-ID request header (e.g. this
// package's own NewSSEServer with WithReplayBuffer); a server that ignores
// it simply replays nothing extra. Ack/Nack on the delivered messages are
// no-ops — SSE has no acknowledgement protocol on the wire.
//
// MULTI-INSTANCE TOPOLOGY (CLAUDE.md "Production robustness —
// multi-instance awareness"): the resume position (the last-seen event id)
// is held ONLY in memory for the lifetime of a single Stream call — a
// restarted process, or a Stream call on a different instance, resumes from
// the live stream, not from the last id a prior instance saw. Competing
// consumers do not apply here: every SSEClient receives the full stream
// independently, there is no consumer-group partitioning.
//
// SSEClient carries no other mutable state: it is safe to call Stream
// again after it returns, and a single instance is not shared for
// concurrent Stream calls in the documented usage (mirrors the other
// adapters' one-Stream-caller-at-a-time contract).
type SSEClient struct {
	url    string
	client *http.Client
	cfg    *Config
}

var (
	_ msgin.NativeReliability = (*SSEClient)(nil)
	_ msgin.EventDrivenSource = (*SSEClient)(nil)
)

// NewSSEClient builds an SSEClient targeting url. It validates the options
// (NewConfig), then the URL (validateURL — non-empty, http/https, non-empty
// host, reused verbatim from the O1/O2 outbound adapters), then resolves the
// *http.Client ONCE via resolveSSEClient — deliberately NOT outbound's
// resolveClient, which would carry NewConfig's 30s defaultHTTPClientTimeout
// back-fill and abort every long-lived stream (Plan 026 audit MAJOR-1).
func NewSSEClient(url string, opts ...Option) (*SSEClient, error) {
	cfg, err := NewConfig(opts...)
	if err != nil {
		return nil, err
	}
	if err := validateURL(url); err != nil {
		return nil, err
	}
	return &SSEClient{url: url, client: resolveSSEClient(cfg), cfg: cfg}, nil
}

// NativeRedelivery reports false: an SSEClient's best-effort Last-Event-ID
// resume is not a redelivery guarantee — a server that discards its replay
// buffer, or a client that never resumes (no Last-Event-ID held yet), simply
// loses whatever was missed. Callers needing at-least-once delivery must
// layer it themselves (e.g. an idempotent consumer reading a durable
// upstream log the SSE endpoint fronts).
func (c *SSEClient) NativeRedelivery() bool { return false }

// NativeDeadLetter reports false: there is no dead-letter concept for a
// streamed SSE connection — an event that fails downstream processing is
// simply gone; the runtime's own dead-letter policy (RetryPolicy) is what a
// caller relies on instead.
func (c *SSEClient) NativeDeadLetter() bool { return false }

// resolveSSEClient returns the *http.Client Stream uses for its per-connection
// requests, resolved ONCE at construction (mirrors resolveClient's
// once-at-construction discipline) but with a DELIBERATELY DIFFERENT default
// from outbound's resolveClient (Plan 026 audit MAJOR-1): the returned
// client's Timeout is never forced to a finite value.
//
// http.Client.Timeout bounds the ENTIRE request, including the response body
// read — exactly what a long-lived text/event-stream connection needs to
// stay open past. Outbound's resolveClient reuses cfg.httpClient, which
// NewConfig back-fills to a client with the 30s defaultHTTPClientTimeout
// when unset; reusing that here would silently abort every SSE stream after
// 30 seconds regardless of how healthy it is. resolveSSEClient therefore
// reads cfg.httpClientSet (set ONLY inside WithHTTPClient's `if c != nil`
// guard) rather than cfg.httpClient's resolved value:
//
//   - httpClientSet (the caller supplied a non-nil *http.Client): a SHALLOW
//     COPY is taken, preserving the caller's Timeout untouched (including a
//     caller who deliberately wants a finite one), and the no-follow
//     CheckRedirect (INV-C6, mirrors resolveClient) is installed on the copy
//     unless the caller opted into WithFollowRedirects or already set their
//     own CheckRedirect.
//   - unset (including an explicit WithHTTPClient(nil), which is a
//     documented no-op and therefore never sets the flag): a FRESH
//     &http.Client{} is built with Timeout left at its zero value (no
//     timeout) so http.DefaultTransport's net.Dialer{KeepAlive: 30s} is what
//     detects a dead peer (INV-C7) — WithReadTimeout is the opt-in, stronger
//     alternative for a caller who needs faster or guaranteed detection. The
//     no-follow CheckRedirect is installed unless WithFollowRedirects was set.
func resolveSSEClient(cfg *Config) *http.Client {
	if cfg.httpClientSet {
		c := cfg.httpClient
		if cfg.followRedirects || c.CheckRedirect != nil {
			return c
		}
		cp := *c
		cp.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
		return &cp
	}
	c := &http.Client{}
	if !cfg.followRedirects {
		c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	}
	return c
}

// Stream connects to c.url and streams Server-Sent Events onto out
// (msgin.EventDrivenSource), reconnecting under a clamped exponential backoff
// (WithReconnectBackoff) until ctx is cancelled or a TERMINAL condition is
// reached.
//
// Delivery guarantee: AT-MOST-ONCE, best-effort Last-Event-ID resume (see the
// SSEClient struct doc comment) — Ack/Nack on every delivered message are
// no-ops, since SSE carries no acknowledgement protocol on the wire.
//
// WHATWG triage (INV-C1) decides each connection's fate: a 200 response whose
// Content-Type is text/event-stream (optionally carrying a ";charset=..."
// parameter, parsed via mime.ParseMediaType) is streamed, each parsed
// SSEEvent becoming one Delivery whose payload is ev.Data and which carries
// HeaderSSEEventName/HeaderSSEEventID ONLY when ev.Name/ev.ID is non-empty —
// msg.ID() is always a freshly minted id (msgin.New), never the remote event
// id. A 204 is the server's deliberate stop and ends Stream with a nil error.
// A 200 with any OTHER (or missing) Content-Type is TERMINAL and ends Stream
// with ErrNotEventStream — almost always a misconfigured URL, which no amount
// of reconnecting fixes, so a non-2xx-or-204-or-200 body is NEVER handed to
// the parser. Any OTHER status (4xx, 5xx, a 3xx the no-follow client surfaces
// as a response, ...) is logged at WARN (the status CODE only — never a
// response body byte) and reconnected after a backoff wait. A transport-level
// failure (the request never got a response at all) is logged the same way,
// via redactTransport (no target URL reaches the log), and reconnected.
//
// MINOR-2: because every status outside {204, 200-with-wrong-CT} reconnects
// silently, a caller pointed at a wrong-but-still-answering URL (e.g. one that
// happens to return 404) sees Stream loop under backoff forever — visible
// ONLY in the WARN log, never in Stream's return value, until ctx is
// cancelled. Only a 204, a 200 with the wrong media type, or ctx cancellation
// ever ends Stream.
//
// Reconnect pacing (INV-C4, ADR 0023 Addendum C3): the wait before each
// reconnect starts at WithReconnectBackoff's min (default 500ms) and, absent
// an override below, doubles after every plain connection end (capped at
// max, default 30s — see doubleDelay). Two overrides apply to a given
// connection's end, in priority order: (1) a valid SSE "retry:" field seen
// on the ended connection REPLACES the wait (and the backoff baseline for
// every following round) with clamp(retry, min, max) instead of doubling —
// an explicit server reconnection-time directive is honored even on an
// otherwise-healthy connection, since the server is telling the client how
// hard it is willing to be hit; (2) absent that, a connection that emitted
// at least one event resets the wait to min, since a stream that was
// healthy before ending (but named no explicit retry) should not inherit an
// elevated delay from earlier failures. A connection that both emitted an
// event and carried a "retry:" field takes the retry-clamp, not the
// event-reset: rule (1) is checked first (user-decided 2026-07-24 — see
// ADR 0023 Addendum C).
//
// Resume (best-effort, WHATWG/C3): the last-seen "id:" is carried as
// Last-Event-ID on the next connection attempt; an empty "id:" line CLEARS
// it, so the following reconnect sends no Last-Event-ID header at all,
// exactly as the WHATWG last-event-id buffer does.
//
// MULTI-INSTANCE (CLAUDE.md "Production robustness — multi-instance
// awareness"): the resume position and the current backoff delay live ONLY
// in this call's local variables — see the SSEClient struct doc comment.
// They are never shared across Stream calls or process instances.
func (c *SSEClient) Stream(ctx context.Context, out chan<- msgin.Delivery) error {
	delay := c.cfg.reconnectMin
	lastEventID := ""

	for {
		res := c.connect(ctx, out, lastEventID)
		lastEventID = res.lastEventID
		if res.terminal {
			return res.terminalErr
		}

		wait := delay
		switch {
		case res.hasRetry:
			wait = clampDelay(res.retry, c.cfg.reconnectMin, c.cfg.reconnectMax)
		case res.gotEvent:
			wait = c.cfg.reconnectMin
		}

		if waitErr := c.backoffWait(ctx, wait); waitErr != nil {
			return waitErr
		}

		switch {
		case res.hasRetry:
			delay = wait
		case res.gotEvent:
			delay = c.cfg.reconnectMin
		default:
			delay = doubleDelay(wait, c.cfg.reconnectMax)
		}
	}
}

// connResult reports the outcome of ONE Stream connection attempt: whether
// Stream must return now (terminal, with terminalErr — possibly nil for the
// 204 arm), the updated resume position (lastEventID, carried across
// reconnects regardless of terminal/non-terminal), and — for a non-terminal
// outcome — whether the connection emitted at least one event (gotEvent) and
// the most recent valid SSE "retry:" field it saw (retry/hasRetry). See
// Stream's doc comment for how these combine into the next reconnect delay.
type connResult struct {
	terminal    bool
	terminalErr error
	gotEvent    bool
	retry       time.Duration
	hasRetry    bool
	lastEventID string
}

// connect performs ONE connection attempt: build the GET request (the
// always-sent Accept/Cache-Control headers, Last-Event-ID when lastEventID is
// held, then cfg.connectHeaders), issue it, and triage the response per
// INV-C1. resp.Body is closed on EVERY path via a single deferred Close
// registered immediately after a nil-error Do (mirrors Outbound.Send's INV-7
// discipline) — drainBounded, used on every non-stream arm, never closes.
//
// ctx is the PARENT Stream ctx. connect derives its own cctx for the request
// so a future per-connection watchdog (Task 3's WithReadTimeout) can cancel
// just THIS attempt without tearing down Stream — but the terminal-vs-
// reconnect DECISION always keys on the parent ctx, never cctx: a cctx-only
// cancellation surfaces here as an ordinary transport or read error, which
// resolves to terminal-vs-reconnect through backoffWait's own select on the
// parent ctx (see Stream).
func (c *SSEClient) connect(ctx context.Context, out chan<- msgin.Delivery, lastEventID string) connResult {
	cctx, ccancel := context.WithCancel(ctx)
	defer ccancel()

	// http.NewRequestWithContext can only fail on a malformed method or an
	// unparseable url; the method here is the constant "GET" and c.url was
	// already validated by validateURL at NewSSEClient construction time
	// (and cctx is never nil), so this construction cannot error on any
	// supported path — the error is deliberately discarded rather than
	// guarded by an unreachable branch (Plan 026 Task 4, coverage-to-100%).
	req, _ := http.NewRequestWithContext(cctx, http.MethodGet, c.url, nil)

	for name, values := range c.cfg.connectHeaders {
		for _, v := range values {
			req.Header.Add(name, v)
		}
	}
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")
	if lastEventID != "" {
		req.Header.Set("Last-Event-ID", lastEventID)
	}

	// INV-C7 read-idle watchdog (Task 3, WithReadTimeout, guarded by
	// cfg.readTimeoutSet — default OFF). Its action IS ccancel: an expiry
	// cancels cctx exactly like a caller cancelling just this connection
	// attempt, aborting whatever client.Do/Read is currently blocked on. That
	// surfaces to Stream as an ordinary transport/read error and reconnects —
	// NEVER terminal (see Stream's doc comment: the terminal-vs-reconnect
	// decision keys on the PARENT ctx via backoffWait's select, never on
	// cctx, so a watchdog fire cannot be mistaken for a real shutdown).
	//
	// Armed BEFORE client.Do (audit F6): a peer that completes the TCP
	// handshake but never sends response headers is reaped too, not only a
	// peer that goes silent mid-body.
	//
	// REAL time (stdlib time.AfterFunc), deliberately NOT cfg.streamClock():
	// the watchdog must abort a REAL blocked socket read/Do, which a
	// fake/virtual clock cannot do — the same real-vs-virtual clock split as
	// the SSE server's WithWriteTimeout.
	var watchdog *time.Timer
	if c.cfg.readTimeoutSet {
		watchdog = time.AfterFunc(c.cfg.readTimeout, ccancel)
		defer watchdog.Stop()
	}

	resp, doErr := c.client.Do(req)
	if doErr != nil {
		c.cfg.log().WarnContext(ctx, "msghttp: sse client connect failed",
			"err", redactTransport(doErr))
		return connResult{lastEventID: lastEventID}
	}
	defer func() { _ = resp.Body.Close() }()

	switch classifySSEResponse(resp) {
	case sseTriageStream:
		body := io.Reader(resp.Body)
		if watchdog != nil {
			// Headers arrived: re-arm the full deadline for the message
			// phase, then hand readEvents a reader that resets the deadline
			// on every byte-yielding Read (per-READ, not per-parsed-event —
			// audit F2: an SSE comment heartbeat ("`: ping\n`") yields bytes
			// with no dispatched event, and must still keep the connection
			// alive).
			watchdog.Reset(c.cfg.readTimeout)
			body = &readTimeoutReader{r: resp.Body, w: watchdog, d: c.cfg.readTimeout}
		}
		return c.readEvents(ctx, out, body, lastEventID)
	case sseTriageDone:
		drainBounded(resp.Body, c.cfg.maxResponseBytes)
		return connResult{terminal: true, lastEventID: lastEventID}
	case sseTriageWrongType:
		drainBounded(resp.Body, c.cfg.maxResponseBytes)
		return connResult{terminal: true, terminalErr: ErrNotEventStream, lastEventID: lastEventID}
	default: // sseTriageReconnect
		drainBounded(resp.Body, c.cfg.maxResponseBytes)
		c.cfg.log().WarnContext(ctx, "msghttp: sse client received unexpected status",
			"status", resp.StatusCode)
		return connResult{lastEventID: lastEventID}
	}
}

// sseTriageOutcome is WHATWG-triage's classification of a single connection's
// response (INV-C1) — see classifySSEResponse and Stream's doc comment.
type sseTriageOutcome int

const (
	// sseTriageStream: 200 + Content-Type text/event-stream -> hand the body
	// to the parser.
	sseTriageStream sseTriageOutcome = iota
	// sseTriageDone: 204 -> the server's deliberate terminal stop.
	sseTriageDone
	// sseTriageWrongType: 200 + any other (or missing) Content-Type ->
	// terminal ErrNotEventStream.
	sseTriageWrongType
	// sseTriageReconnect: any other status -> log and reconnect.
	sseTriageReconnect
)

// classifySSEResponse triages resp per INV-C1. A 200's media type is parsed
// with mime.ParseMediaType so a ";charset=..." (or any other) parameter does
// not defeat the text/event-stream match; an empty or unparseable
// Content-Type falls to sseTriageWrongType exactly like any other mismatched
// media type. 204 is checked independent of Content-Type, matching the
// server's own contract (Send has no body to type). Every other status,
// including a 3xx the no-follow client surfaces as an ordinary response,
// reconnects.
func classifySSEResponse(resp *http.Response) sseTriageOutcome {
	switch resp.StatusCode {
	case http.StatusOK:
		mt, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type"))
		if err == nil && mt == "text/event-stream" {
			return sseTriageStream
		}
		return sseTriageWrongType
	case http.StatusNoContent:
		return sseTriageDone
	default:
		return sseTriageReconnect
	}
}

// readEvents parses body as an SSE stream, emitting each event onto out until
// the stream ends. Every connection-ending outcome from SSEParser.Next —
// io.EOF (clean end), io.ErrUnexpectedEOF (mid-event end), ErrEventTooLarge
// (an oversized event), or any other reader error — is treated identically
// here: the read loop simply ends and Stream reconnects after a backoff wait
// (every connection-end error is reconnectable; only ctx cancellation while
// blocked emitting is terminal, INV-C5).
func (c *SSEClient) readEvents(ctx context.Context, out chan<- msgin.Delivery, body io.Reader, lastEventID string) connResult {
	// newSSEParserWithCap takes c.cfg.maxEventBytes directly rather than going
	// through NewSSEParser(WithMaxEventBytes(...)): that value was already
	// validated (> 0) by NewConfig at NewSSEClient construction time, so
	// re-running NewConfig's validation here could never fail — there is no
	// error left to guard (Plan 026 Task 4, coverage-to-100%).
	parser := newSSEParserWithCap(body, c.cfg.maxEventBytes)

	gotEvent := false
	for {
		ev, nextErr := parser.Next()
		if nextErr != nil {
			break
		}

		msg := msgin.New[any](ev.Data, msgin.WithClock(c.cfg.streamClock()))
		if ev.Name != "" {
			msg = msg.WithHeader(HeaderSSEEventName, ev.Name)
		}
		if ev.ID != "" {
			msg = msg.WithHeader(HeaderSSEEventID, ev.ID)
		}

		select {
		case out <- msgin.Delivery{Msg: msg, Ack: sseNoopAck, Nack: sseNoopNack}:
			gotEvent = true
		case <-ctx.Done():
			return connResult{
				terminal:    true,
				terminalErr: ctx.Err(),
				gotEvent:    gotEvent,
				lastEventID: parser.LastEventID(),
			}
		}
	}

	res := connResult{gotEvent: gotEvent, lastEventID: parser.LastEventID()}
	res.retry, res.hasRetry = parser.RetryDelay()
	return res
}

// backoffWait waits d on cfg.streamClock() before the next reconnect attempt,
// or returns ctx.Err() if the PARENT ctx is cancelled first — this select is
// where every non-terminal connection outcome's terminal-vs-reconnect
// decision (INV-C5) actually resolves.
func (c *SSEClient) backoffWait(ctx context.Context, d time.Duration) error {
	select {
	case <-c.cfg.streamClock().After(d):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// clampDelay bounds d into [minD,maxD]. Used for a server-supplied SSE
// "retry:" hint (INV-C4). The d < minD arm is also the last line of defense
// against a "retry:" value that fits int64 milliseconds but overflows
// time.Duration on the *time.Millisecond conversion (≳9.3e12 ms), which wraps
// to a negative duration — safe (no hot loop, no hang) only because this
// floor catches it; do not remove or narrow this arm as an "optimization".
func clampDelay(d, minD, maxD time.Duration) time.Duration {
	switch {
	case d < minD:
		return minD
	case d > maxD:
		return maxD
	default:
		return d
	}
}

// doubleDelay doubles d, capping at maxD. next <= 0 also catches the (not
// expected on any supported configuration) integer-overflow case of d*2
// wrapping negative, falling back to maxD rather than letting a wrapped
// negative duration reach backoffWait's clk.After.
func doubleDelay(d, maxD time.Duration) time.Duration {
	next := d * 2
	if next <= 0 || next > maxD {
		return maxD
	}
	return next
}

// readTimeoutReader wraps an SSE connection's response body so every
// byte-yielding Read resets the INV-C7 idle watchdog (w) to its full
// duration (d) — this is what makes the watchdog measure READ idleness
// rather than EVENT idleness (audit F2): an SSE comment heartbeat is a Read
// that yields bytes but dispatches no event, and must still keep the
// watchdog from firing. Used only when cfg.readTimeoutSet; when the read
// timeout is off, connect passes resp.Body directly to readEvents with no
// wrapper and no timer, so the watchdog imposes zero overhead when unused.
type readTimeoutReader struct {
	r io.Reader
	w *time.Timer
	d time.Duration
}

// Read implements io.Reader, resetting rt.w to rt.d whenever the underlying
// read yields at least one byte (a zero-byte read, or an error with n==0,
// leaves the deadline running down — exactly what allows it to eventually
// expire on a genuinely idle connection).
func (rt *readTimeoutReader) Read(p []byte) (int, error) {
	n, err := rt.r.Read(p)
	if n > 0 {
		rt.w.Reset(rt.d)
	}
	return n, err
}

// sseNoopAck / sseNoopNack settle a delivered SSE event: SSE carries no
// acknowledgement protocol on the wire, so both are no-ops (named apart from
// adapter/cron's noopAck/noopNack, which are private to that package).
func sseNoopAck(context.Context) error                       { return nil }
func sseNoopNack(context.Context, bool, time.Duration) error { return nil }
