package msghttp

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/kartaladev/msgin"
)

// HeaderSSEEventName is the non-reserved message header SSEEventFromMessage
// reads to name an SSE event's "event:" field. It wins over cfg's
// WithEventName default when present and non-empty; a flow sets it
// per-message via msg.WithHeader(msghttp.HeaderSSEEventName, "...").
//
// It deliberately does NOT carry the reserved "msgin." prefix (mirrors
// encode.go's http.* metadata keys): it is a msghttp-owned wire concept a
// flow sets directly, not a core msgin header a client could forge through
// the inbound allow-list strip.
const HeaderSSEEventName = "http.sse-event"

// HeaderSSEEventID is the non-reserved message header the SSE CLIENT (Plan
// 026, Phase 4) writes with the last event id it observed on a stream, so a
// flow reading a message built from a reconnect can resume from it. It is
// declared here — as a constant only — because Task 1 (the encode side) and
// the future client both need to agree on the same header name; nothing in
// this task reads or writes it.
const HeaderSSEEventID = "http.sse-event-id"

// SSEEvent is one Server-Sent Event frame: an optional id, an optional event
// name, and a data payload. EncodeSSEEvent writes it in the WHATWG SSE wire
// format.
type SSEEvent struct {
	// ID is the event's "id:" field. Empty means no id: line is written.
	ID string
	// Name is the event's "event:" field. Empty means no event: line is
	// written, and the client dispatches the frame as the SSE default event,
	// "message".
	Name string
	// Data is the event's payload. It is split on '\n' and framed one
	// "data: " line per element (see EncodeSSEEvent's doc comment). An empty
	// or nil Data still produces a single "data: \n" line.
	Data []byte
}

// EncodeSSEEvent writes ev to w in the WHATWG Server-Sent Events wire format:
//
//	id: <ID>\n           (only when ID != "")
//	event: <Name>\n      (only when Name != "")
//	data: <line>\n       (one per '\n'-split line of Data)
//	\n
//
// Every data line is framed uniformly as "data:" + one space + the line's
// bytes + "\n" — including an EMPTY (or nil) Data, which still produces
// exactly one line, "data: \n" (colon-space-newline). This is deliberate, not
// a special case: the SSE parsing algorithm strips exactly one leading space
// off a field's value, so the single framing space here is what makes the
// round trip lossless — a payload whose own first byte is itself a space
// survives as "data:  <rest>" (two spaces: the framing space, then the
// payload's own). Emitting nothing at all for an empty Data would produce an
// SSE event with no data line, which no conforming parser ever dispatches —
// silently turning a legitimate empty-payload send into a no-op.
//
// Before any byte is written, ID and Name are validated: either containing a
// CR, LF, or NUL returns ErrInvalidEventField and writes nothing (INV-S1). A
// raw newline in an id:/event: field would let it inject additional,
// unintended SSE fields into the frame; NUL is rejected alongside it as a
// defensive line-oriented-parser precaution. Data is NOT subject to this
// check: splitting it on '\n' into multiple data: lines is the documented,
// intended multi-line-data mechanism, not an injection.
//
// A write failure from w is returned exactly as w produced it (unwrapped):
// the caller already knows the write target, and EncodeSSEEvent adds no
// classification atop a plain I/O error.
func EncodeSSEEvent(w io.Writer, ev SSEEvent) error {
	if !validSSEField(ev.ID) || !validSSEField(ev.Name) {
		return ErrInvalidEventField
	}

	var b strings.Builder
	if ev.ID != "" {
		b.WriteString("id: ")
		b.WriteString(ev.ID)
		b.WriteByte('\n')
	}
	if ev.Name != "" {
		b.WriteString("event: ")
		b.WriteString(ev.Name)
		b.WriteByte('\n')
	}

	for _, line := range strings.Split(string(ev.Data), "\n") {
		b.WriteString("data: ")
		b.WriteString(line)
		b.WriteByte('\n')
	}
	b.WriteByte('\n')

	_, err := io.WriteString(w, b.String())
	return err
}

// SSEEventFromMessage converts msg into an SSEEvent: Data is msg's payload
// bytes (via the same payloadBytes extraction EncodeResponse/EncodeRequest
// use — a []byte payload verbatim, a string payload converted, any other
// payload type returns ErrUnsupportedPayload); ID is msg.ID(); and Name is
// the HeaderSSEEventName message header when present and non-empty, else
// cfg's WithEventName default (via eventNameOrDefault — "" when neither is
// set).
//
// ID and Name are then validated exactly as EncodeSSEEvent validates them
// (CR, LF, or NUL -> ErrInvalidEventField): the header value is
// flow-supplied, but a message built with msgin.NewMessage (reconstructing
// from a persisted envelope) can carry an arbitrary, previously-stored header
// value, so it must be checked here rather than trusted.
//
// A nil cfg, or one built by hand rather than by NewConfig, is tolerated
// (the Config nil-safety contract): the event name falls back to "".
func SSEEventFromMessage(msg msgin.Message[any], cfg *Config) (SSEEvent, error) {
	data, err := payloadBytes(msg)
	if err != nil {
		return SSEEvent{}, err
	}

	name := cfg.eventNameOrDefault()
	if v, ok := msg.Headers().String(HeaderSSEEventName); ok && v != "" {
		name = v
	}

	ev := SSEEvent{ID: msg.ID(), Name: name, Data: data}
	if !validSSEField(ev.ID) || !validSSEField(ev.Name) {
		return SSEEvent{}, ErrInvalidEventField
	}
	return ev, nil
}

// validSSEField reports whether s is safe to write as a single-line SSE
// field value (id: or event:): it must contain no CR, LF, or NUL. Shared by
// EncodeSSEEvent/SSEEventFromMessage and NewConfig's WithEventName
// validation, so the three enforce the exact same rule.
func validSSEField(s string) bool {
	return !strings.ContainsAny(s, "\r\n\x00")
}

// sseBOM is the UTF-8-encoded byte order mark (U+FEFF), stripped once at the
// start of a stream by NewSSEParser (WHATWG text/event-stream processing
// model) if present.
var sseBOM = []byte{0xEF, 0xBB, 0xBF}

// SSEParser reads a WHATWG text/event-stream (Server-Sent Events) byte
// stream and yields one SSEEvent per call to Next, implementing the full
// field-processing rules: a single leading BOM is stripped; CR, LF, and
// CRLF all end a line (a bare CR not followed by LF ends one too); a line
// starting with ':' is a comment and is ignored; "field:value" has at most
// one leading space of value stripped; a line with no colon is the whole
// field name with an empty value; "data" lines accumulate into the event's
// payload; "event" sets the event name; "id" sets the last-event-id unless
// its value contains NUL, and an empty "id:" clears it; "retry" (ASCII
// digits only) is retained for RetryDelay; any other field is ignored. A
// blank line dispatches the accumulated event, or — if no "data:" line
// contributed anything — dispatches nothing at all (only the event-name
// buffer resets; the last-event-id persists).
//
// SSEParser buffers at most WithMaxEventBytes (default 1 MiB) for a single
// event: see WithMaxEventBytes for the exact cap semantics and
// ErrEventTooLarge for the error it produces when exceeded.
//
// SSEParser is built over a bufio.Reader with explicit byte accounting
// rather than bufio.Scanner, whose token cap and line-splitting fit neither
// bare-CR line endings nor the per-event cap. It is not safe for concurrent
// use: a single goroutine must own the sequence of Next calls, exactly as it
// must own reads from the underlying io.Reader.
type SSEParser struct {
	r             *bufio.Reader
	maxEventBytes int64

	dataBuf  bytes.Buffer
	typeBuf  string
	idBuf    string
	retry    time.Duration
	hasRetry bool
}

// NewSSEParser builds an SSEParser reading from r. opts configures the same
// shared Config/Option surface as the rest of this package; only
// WithMaxEventBytes applies here. NewConfig's validation runs first, so an
// explicit WithMaxEventBytes(n<=0) returns ErrInvalidMaxEventBytes and a nil
// *SSEParser before r is touched.
//
// A single leading UTF-8 byte order mark (U+FEFF) on r is consumed here, at
// construction, if present — it is never treated as part of the stream's
// first line.
func NewSSEParser(r io.Reader, opts ...Option) (*SSEParser, error) {
	cfg, err := NewConfig(opts...)
	if err != nil {
		return nil, err
	}

	br := bufio.NewReader(r)
	if bom, peekErr := br.Peek(len(sseBOM)); peekErr == nil && bytes.Equal(bom, sseBOM) {
		_, _ = br.Discard(len(sseBOM))
	}

	return &SSEParser{r: br, maxEventBytes: cfg.maxEventBytes}, nil
}

// Next reads and returns the next dispatched SSEEvent from the stream,
// skipping comments, no-op blank lines (an empty data buffer at a blank
// line dispatches nothing), and applying every field-processing rule
// documented on SSEParser.
//
// io.EOF is returned only for a CLEAN end-of-stream: the underlying reader
// is exhausted at a point where no event is in progress (no pending
// in-progress line, no accumulated data buffer, no pending event name). If
// the stream instead ends mid-event — a final unterminated line, or a
// complete field line with no blank line ever arriving to dispatch it —
// Next returns io.ErrUnexpectedEOF and discards whatever was accumulated
// for that event (the id buffer, which is stream-level state rather than
// per-event, is unaffected and survives).
//
// A single event exceeding WithMaxEventBytes returns ErrEventTooLarge; see
// its godoc for the exact cap semantics. When the cap trips on a single
// over-length line (the line-counter half of the cap), the oversized event
// is SKIPPED, not fatal: the abandoned line's remaining bytes are drained
// to its terminator before Next returns, so the next call to Next resumes
// parsing cleanly at the following event rather than reinterpreting the
// abandoned tail. Whatever the cause — a mid-event EOF, either half of the
// cap, or any other reader error — Next always resets its per-event data
// and type buffers before returning a non-nil error, exactly as it does
// after a successful dispatch (see resetEventBuffers): no data accumulated
// for a discarded event ever survives to corrupt a later one. The
// last-event-id and retry buffers are stream-level state and are
// unaffected by any of this.
//
// Any other error from the underlying reader is returned exactly as it was
// produced, unwrapped.
func (p *SSEParser) Next() (SSEEvent, error) {
	for {
		line, err := p.readLine()
		if err != nil {
			switch {
			case errors.Is(err, io.EOF):
				if p.dataBuf.Len() > 0 || p.typeBuf != "" {
					p.resetEventBuffers()
					return SSEEvent{}, io.ErrUnexpectedEOF
				}
				return SSEEvent{}, io.EOF
			case errors.Is(err, io.ErrUnexpectedEOF):
				p.resetEventBuffers()
				return SSEEvent{}, io.ErrUnexpectedEOF
			default:
				p.resetEventBuffers()
				return SSEEvent{}, err
			}
		}

		switch {
		case len(line) == 0:
			// Blank line: dispatch, unless the data buffer never received
			// anything — in which case only the event-name buffer resets
			// (WHATWG: the id buffer is not part of this reset; it
			// persists until overwritten or explicitly cleared).
			if p.dataBuf.Len() == 0 {
				p.typeBuf = ""
				continue
			}
			data := p.dataBuf.Bytes()
			data = data[:len(data)-1] // strip the single trailing '\n'
			ev := SSEEvent{ID: p.idBuf, Name: p.typeBuf, Data: append([]byte(nil), data...)}
			p.resetEventBuffers()
			return ev, nil
		case line[0] == ':':
			// Comment: ignored outright, never touches any buffer.
			continue
		default:
			if ferr := p.processField(line); ferr != nil {
				p.resetEventBuffers()
				return SSEEvent{}, ferr
			}
		}
	}
}

// RetryDelay returns the most recently processed valid "retry:" field, as a
// duration, and true — or (0, false) if no valid "retry:" field has been
// seen yet. Unlike the per-event data/type buffers, the retry value is
// stream-level state: it persists across events and is left untouched by an
// invalid (non-ASCII-digit) "retry:" value.
func (p *SSEParser) RetryDelay() (time.Duration, bool) {
	return p.retry, p.hasRetry
}

// LastEventID returns the current last-event-id buffer: the value most
// recently set by a valid "id:" field (one whose value contains no NUL), or
// "" if none has been seen yet or the buffer was last cleared by an empty
// "id:" line. Like RetryDelay, this is stream-level state that persists
// across events.
func (p *SSEParser) LastEventID() string {
	return p.idBuf
}

// resetEventBuffers resets the per-event data and type buffers — called
// after every dispatch, every no-op blank line, and every error return from
// Next that discards an in-progress event: a mid-event EOF, an over-cap
// event (whether the line-counter or the data-buffer counter tripped), or
// any other raw reader error. It never touches idBuf or retry: those are
// stream-level state, not per-event.
func (p *SSEParser) resetEventBuffers() {
	p.dataBuf.Reset()
	p.typeBuf = ""
}

// processField parses line as a "field:value" (or bare field-name) SSE line
// and applies its effect, per the field-processing rules documented on
// SSEParser. It returns ErrEventTooLarge if appending a "data:" value pushes
// the accumulated data buffer over p.maxEventBytes.
func (p *SSEParser) processField(line []byte) error {
	var field, value string
	if idx := bytes.IndexByte(line, ':'); idx >= 0 {
		field = string(line[:idx])
		value = string(line[idx+1:])
		if len(value) > 0 && value[0] == ' ' {
			value = value[1:]
		}
	} else {
		field = string(line)
	}

	switch field {
	case "data":
		p.dataBuf.WriteString(value)
		p.dataBuf.WriteByte('\n')
		if int64(p.dataBuf.Len()) > p.maxEventBytes {
			return ErrEventTooLarge
		}
	case "event":
		p.typeBuf = value
	case "id":
		if strings.IndexByte(value, 0) < 0 {
			p.idBuf = value
		}
	case "retry":
		if ms, ok := parseRetryMillis(value); ok {
			p.retry = time.Duration(ms) * time.Millisecond
			p.hasRetry = true
		}
	default:
		// Unrecognized field: ignored per WHATWG.
	}
	return nil
}

// parseRetryMillis reports the integer value of an SSE "retry:" field, which
// is valid only when non-empty and composed entirely of ASCII digits
// (WHATWG: any other value is ignored, not clamped or truncated).
func parseRetryMillis(value string) (int64, bool) {
	if value == "" {
		return 0, false
	}
	for i := 0; i < len(value); i++ {
		if value[i] < '0' || value[i] > '9' {
			return 0, false
		}
	}
	ms, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, false
	}
	return ms, true
}

// readLine reads bytes from p.r up to and including the next line ending
// (LF, CRLF, or a bare CR), honoring the per-line byte cap (p.maxEventBytes)
// — the "current in-progress line" half of the INV-S6 cap. It returns:
//
//   - (line, nil) — a complete, terminated line; line excludes the
//     terminator.
//   - (nil, io.EOF) — a CLEAN end-of-stream: zero bytes were consumed
//     attempting to read this line.
//   - (line, io.ErrUnexpectedEOF) — the stream ended mid-line: line holds
//     whatever was read, but it has NO terminator and is never processed as
//     a field by the caller.
//   - (nil, ErrEventTooLarge) — the in-progress line exceeded
//     p.maxEventBytes before any terminator was found. Before returning,
//     the abandoned line's remaining bytes are drained (read and
//     discarded) up to and including its terminator, or up to EOF if none
//     is found — see drainLine. This leaves the underlying reader
//     positioned at the start of the NEXT line rather than mid-line, so a
//     subsequent readLine call resumes cleanly instead of reinterpreting
//     the abandoned tail as a new line.
//   - (nil, err) — any other read error from the underlying reader,
//     returned exactly as produced.
func (p *SSEParser) readLine() ([]byte, error) {
	var buf []byte
	for {
		b, err := p.r.ReadByte()
		if err != nil {
			if errors.Is(err, io.EOF) {
				if len(buf) == 0 {
					return nil, io.EOF
				}
				return buf, io.ErrUnexpectedEOF
			}
			return nil, err
		}

		if b == '\n' {
			return buf, nil
		}
		if b == '\r' {
			if next, peekErr := p.r.Peek(1); peekErr == nil && len(next) == 1 && next[0] == '\n' {
				_, _ = p.r.ReadByte() // consume the paired LF of a CRLF ending
			}
			return buf, nil
		}

		buf = append(buf, b)
		if int64(len(buf)) > p.maxEventBytes {
			p.drainLine()
			return nil, ErrEventTooLarge
		}
	}
}

// drainLine reads and discards bytes from p.r up to and including the next
// line terminator (LF, CRLF, or a bare CR), or up to EOF/a read error if no
// terminator is ever found. It exists solely for readLine's line-counter cap
// trip (INV-S6): once an in-progress line is abandoned as over-cap, its
// remaining, unread bytes must not stay in the reader — left there, the next
// readLine call would resume mid-line and reinterpret the abandoned tail as
// a brand-new line (WHATWG gives no guidance here; this is msghttp's own
// "continue past an oversized event" recovery policy — see Next's doc
// comment). Any error hit while draining, including EOF, simply ends the
// drain without surfacing it: a genuine underlying reader failure resurfaces
// on the very next read attempt regardless.
func (p *SSEParser) drainLine() {
	for {
		b, err := p.r.ReadByte()
		if err != nil {
			return
		}
		if b == '\n' {
			return
		}
		if b == '\r' {
			if next, peekErr := p.r.Peek(1); peekErr == nil && len(next) == 1 && next[0] == '\n' {
				_, _ = p.r.ReadByte() // consume the paired LF of a CRLF ending
			}
			return
		}
	}
}
