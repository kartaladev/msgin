package msghttp

import (
	"io"
	"strings"

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
